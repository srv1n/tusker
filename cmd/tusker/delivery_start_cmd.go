package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const deliveryStartSchema = "tusker.delivery-start/v1"

var deliveryStartBeforeArm func()
var deliveryStartAfterImportUnlock func()

// deliveryStartResult is intentionally a compact product result. It exposes
// the exact authorization and where to observe delivery, but not daemon
// control or runner permissions.
type deliveryStartResult struct {
	Schema                   string   `json:"schema"`
	WaveID                   string   `json:"waveId"`
	PlanFingerprint          string   `json:"planFingerprint"`
	ContextFingerprint       string   `json:"contextFingerprint"`
	AuthorizationFingerprint string   `json:"authorizationFingerprint"`
	FirstFrontier            []string `json:"firstFrontier"`
	ExpectedConcurrency      int      `json:"expectedConcurrency"`
	IntegrationLane          string   `json:"integrationLane"`
	StatusLink               string   `json:"statusLink"`
	Replayed                 bool     `json:"replayed"`
	NextAction               string   `json:"nextAction,omitempty"`
}

type deliveryStartAuthority struct {
	WaveID                    string
	Members                   []string
	MemberBaselines           map[string]string
	MemberReadiness           map[string]string
	AuthorizationFingerprint  string
	WaveAuthorization         string
	WaveAuthorizedFingerprint string
	WaveAuthorizedBy          string
	WaveAuthorizedAt          string
	PlanPath                  string
	PlanFingerprint           string
	PlanBytes                 []byte
	PlanBound                 bool
	PlanVerify                func() error
	ContextFingerprint        string
	IntegrationBaseSHA        string
}

type deliveryPlanSource struct {
	Path           string
	Raw            []byte
	BeforeMutation func()
	Verify         func() error
}

func deliveryStartCmd(args Args) error {
	result, err := deliveryStart(args, nil)
	if err != nil {
		return err
	}
	if args.Bool("json") {
		emitJSON(result)
	} else if !args.Bool("quiet") {
		fmt.Printf("Delivery started: %s\nAuthorization: %s\nFirst frontier: %s\nExpected concurrency: %d\nIntegration lane: %s\nStatus: %s\n", result.WaveID, result.AuthorizationFingerprint, strings.Join(result.FirstFrontier, ", "), result.ExpectedConcurrency, result.IntegrationLane, result.StatusLink)
	}
	return nil
}

// deliveryStart performs the two safe mutations in order: held import, then
// exact-wave arm. The importer and armer each use their existing rollback/CAS
// machinery; a preflight refusal intentionally leaves the imported records
// held and disarmed. Tests can inject a repeatable environment inspector
// without creating a daemon while production always inspects live state.
func deliveryStart(args Args, inspector wavePreflightEnvironmentInspector) (deliveryStartResult, error) {
	return deliveryStartWithPlanSource(args, inspector, nil)
}

func deliveryStartWithPlanSource(args Args, inspector wavePreflightEnvironmentInspector, source *deliveryPlanSource) (deliveryStartResult, error) {
	vault, err := resolveVaultPath(args, false)
	if err != nil {
		return deliveryStartResult{}, err
	}
	if source != nil && source.Verify == nil {
		return deliveryStartResult{}, tuskerError(errorInvalidTransition, "bound delivery plan snapshot has no path identity verifier")
	}
	if err := ensureV7ControlMutation(vault, args); err != nil {
		return deliveryStartResult{}, err
	}
	actor, err := deliveryStartActor(args)
	if err != nil {
		return deliveryStartResult{}, err
	}
	path, confirmed, plan, raw, err := deliveryStartPlanInput(vault, args, source)
	if err != nil {
		return deliveryStartResult{}, err
	}
	context, err := deliveryStartValidateContext(vault, plan)
	if err != nil {
		return deliveryStartResult{}, err
	}
	materialLock, err := acquireV7MaterialEpochLock(vault)
	if err != nil {
		return deliveryStartResult{}, err
	}
	// Revalidate every reviewed input while holding the same material boundary
	// as import/gate/task mutations. CLI callers re-read their path; Serve
	// reparses its descriptor-bound bytes and verifies the rooted path chain.
	pathLocked, confirmedLocked, planLocked, rawLocked, err := deliveryStartPlanInput(vault, args, source)
	if err == nil {
		context, err = deliveryStartValidateContext(vault, planLocked)
	}
	if err == nil && (!bytes.Equal(raw, rawLocked) || pathLocked != path || confirmedLocked != confirmed) {
		err = tuskerError(errorInvalidTransition, "delivery plan changed during Start; regenerate delivery review and confirm its exact plan fingerprint")
	}
	if err != nil {
		_ = materialLock.Close()
		return deliveryStartResult{}, err
	}
	if source != nil {
		if source.BeforeMutation != nil {
			source.BeforeMutation()
		}
		if source.Verify != nil {
			if verifyErr := source.Verify(); verifyErr != nil {
				_ = materialLock.Close()
				return deliveryStartResult{}, tuskerError(errorInvalidTransition, "delivery plan identity changed after review; review and confirm the current plan again", withContext(map[string]any{"cause": verifyErr.Error()}))
			}
		}
	}
	authority := &deliveryStartAuthority{
		PlanPath:           pathLocked,
		PlanFingerprint:    confirmedLocked,
		PlanBytes:          append([]byte(nil), rawLocked...),
		PlanBound:          source != nil,
		ContextFingerprint: context.ContextFingerprint,
		IntegrationBaseSHA: context.IntegrationBase.SHA,
	}
	if source != nil {
		authority.PlanVerify = source.Verify
	}

	// Reuse the V2 importer, but suppress its integration-branch bootstrap: a
	// Start request must not move refs as a side effect of authorization.
	importArgs := copyArgsForInternalMutation(args)
	importArgs["plan"] = authority.PlanPath
	importArgs["by"] = actor
	importArgs["quiet"] = "true"
	importArgs["json"] = "false"
	importArgs["skip-integration-branch"] = "true"
	importArgs["expected-plan-fingerprint"] = authority.PlanFingerprint
	importArgs["expected-integration-base-sha"] = authority.IntegrationBaseSHA
	importArgs["material-lock-held"] = "true"
	if err := deliveryV2ImportBytes(vault, authority.PlanPath, authority.PlanBytes, importArgs); err != nil {
		_ = materialLock.Close()
		return deliveryStartResult{}, err
	}
	importedIdx, err := loadV7Index(vault)
	if err != nil {
		_ = materialLock.Close()
		return deliveryStartResult{}, err
	}
	importedWave, err := deliveryStartWave(importedIdx, planLocked, authority.PlanFingerprint)
	if err != nil {
		_ = materialLock.Close()
		return deliveryStartResult{}, err
	}
	authorizationFingerprint, fingerprintIssues := waveMaterialFingerprint(vault, importedIdx, importedWave)
	authority.AuthorizationFingerprint = authorizationFingerprint
	if len(fingerprintIssues) > 0 {
		_ = materialLock.Close()
		return deliveryStartResult{}, tuskerError(errorInvalidTransition, "reviewed import has invalid authorization material: "+fingerprintIssues[0])
	}
	authority.WaveID = stringField(importedWave.Data, "id")
	authority.Members = sortedStrings(normalizeList(importedWave.Data["members"]))
	authority.MemberBaselines = map[string]string{}
	authority.MemberReadiness = map[string]string{}
	for _, memberID := range authority.Members {
		member, ok := importedIdx.Tasks[memberID]
		if !ok {
			_ = materialLock.Close()
			return deliveryStartResult{}, tuskerError(errorInvalidTransition, "reviewed import lost member "+memberID+" before authority capture")
		}
		authority.MemberBaselines[memberID] = deliveryStartMemberBaseline(member)
		authority.MemberReadiness[memberID] = stringField(member.Data, "readiness")
	}
	authority.WaveAuthorization = fallback(stringField(importedWave.Data, "authorization"), "disarmed")
	authority.WaveAuthorizedFingerprint = stringField(importedWave.Data, "authorization_fingerprint")
	authority.WaveAuthorizedBy = stringField(importedWave.Data, "authorized_by")
	authority.WaveAuthorizedAt = stringField(importedWave.Data, "authorized_at")
	if err := materialLock.Close(); err != nil {
		return deliveryStartResult{}, err
	}
	if deliveryStartAfterImportUnlock != nil {
		deliveryStartAfterImportUnlock()
	}

	// The plan and bounded context are sampled again after the write for an
	// early refusal. Serve keeps parsing the bound bytes; CLI callers re-read
	// their path. The final material lock repeats this authority check.
	_, confirmedAfter, planAfter, rawAfter, err := deliveryStartPlanInput(vault, Args{"plan": authority.PlanPath, "confirm": authority.PlanFingerprint}, source)
	if err != nil {
		return deliveryStartResult{}, refuseDeliveryStartBeforeArm(vault, authority, err)
	}
	if !bytes.Equal(authority.PlanBytes, rawAfter) || confirmedAfter != authority.PlanFingerprint {
		cause := tuskerError(errorInvalidTransition, "delivery plan changed during Start; regenerate delivery review and confirm its exact plan fingerprint")
		return deliveryStartResult{}, refuseDeliveryStartBeforeArm(vault, authority, cause)
	}
	if _, err := deliveryStartValidateContext(vault, planAfter); err != nil {
		return deliveryStartResult{}, refuseDeliveryStartBeforeArm(vault, authority, err)
	}

	idx, err := loadV7Index(vault)
	if err != nil {
		return deliveryStartResult{}, refuseDeliveryStartBeforeArm(vault, authority, err)
	}
	wave, err := deliveryStartWave(idx, planAfter, authority.PlanFingerprint)
	if err != nil {
		return deliveryStartResult{}, refuseDeliveryStartBeforeArm(vault, authority, err)
	}
	if stringField(wave.Data, "id") != authority.WaveID {
		cause := tuskerError(errorInvalidTransition, "reviewed import wave changed before preflight; rerun delivery review and Start")
		return deliveryStartResult{}, refuseDeliveryStartBeforeArm(vault, authority, cause)
	}
	currentFingerprint, currentIssues := waveMaterialFingerprint(vault, idx, wave)
	if len(currentIssues) > 0 || currentFingerprint != authority.AuthorizationFingerprint || strings.Join(sortedStrings(normalizeList(wave.Data["members"])), "\x00") != strings.Join(authority.Members, "\x00") {
		cause := tuskerError(errorInvalidTransition, "wave material changed after reviewed import; rerun delivery review and Start")
		return deliveryStartResult{}, refuseDeliveryStartBeforeArm(vault, authority, cause)
	}
	env := deliveryStartInspectEnvironment(vault, wave, inspector)
	preflight := buildWavePreflight(vault, idx, wave, env)
	result := deliveryStartProjection(wave, authority.PlanFingerprint, authority.ContextFingerprint, preflight, false)
	if !preflight.OK {
		result.NextAction = deliveryStartPreflightRemedy(preflight)
		cause := tuskerError(errorInvalidTransition, "delivery start blocked: "+result.NextAction, withContext(map[string]any{"delivery_start": result, "preflight": preflight}))
		return result, refuseDeliveryStartBeforeArm(vault, authority, cause)
	}
	if deliveryStartBeforeArm != nil {
		deliveryStartBeforeArm()
	}

	wasArmed := preflight.Authorization == "armed" && preflight.StoredFingerprint == preflight.Fingerprint
	armArgs := copyArgsForInternalMutation(args)
	armArgs["id"] = preflight.WaveID
	armArgs["by"] = actor
	armArgs["quiet"] = "true"
	armArgs["json"] = "false"
	final, err := mutateWaveAuthorizationWithInspector(armArgs, "armed", inspector, authority)
	if err != nil {
		if !deliveryStartRefusalAlreadyHandled(err) {
			err = refuseDeliveryStartBeforeArm(vault, authority, err)
		}
		return deliveryStartResult{}, err
	}
	return deliveryStartProjection(wave, authority.PlanFingerprint, authority.ContextFingerprint, final, wasArmed), nil
}

func deliveryStartInspectEnvironment(vault string, wave Note, inspector wavePreflightEnvironmentInspector) wavePreflightEnvironment {
	if inspector != nil {
		return inspector(vault, wave)
	}
	return inspectWavePreflightEnvironment(vault, wave)
}

func deliveryStartMemberBaseline(task Note) string {
	data := cloneMap(task.Data)
	for _, key := range []string{"readiness", "updated_at", "updated_by", "state_rev"} {
		delete(data, key)
	}
	return v7StateRev(data, task.Body)
}

func validateDeliveryStartAuthorityUnderLock(vault string, wave Note, authority *deliveryStartAuthority) error {
	if authority == nil {
		return nil
	}
	if authority.PlanBound && authority.PlanVerify != nil {
		if err := authority.PlanVerify(); err != nil {
			return tuskerError(errorInvalidTransition, "reviewed delivery plan identity changed before authorization; regenerate delivery review and Start")
		}
	}
	var (
		confirmed string
		plan      deliveryPlan
		raw       []byte
		err       error
	)
	if authority.PlanBound {
		_, confirmed, plan, raw, err = deliveryStartPlanBytes(vault, Args{"plan": authority.PlanPath, "confirm": authority.PlanFingerprint}, authority.PlanPath, authority.PlanBytes)
	} else {
		_, confirmed, plan, raw, err = deliveryStartPlan(vault, Args{"plan": authority.PlanPath, "confirm": authority.PlanFingerprint})
	}
	if err != nil {
		return err
	}
	if !bytes.Equal(raw, authority.PlanBytes) || confirmed != authority.PlanFingerprint || deliveryFingerprint(raw) != authority.PlanFingerprint || stringField(wave.Data, "delivery_plan_fingerprint") != authority.PlanFingerprint || deliveryPlanScope(plan) != stringField(wave.Data, "delivery_plan_scope") {
		return tuskerError(errorInvalidTransition, "reviewed delivery plan changed before authorization; regenerate delivery review and Start")
	}
	context, err := deliveryStartValidateContext(vault, plan)
	if err != nil {
		return err
	}
	if authority.ContextFingerprint == "" || context.ContextFingerprint != authority.ContextFingerprint || plan.v2.ContextFingerprint != authority.ContextFingerprint {
		return tuskerError(errorInvalidTransition, "reviewed delivery context changed before authorization; regenerate delivery review and Start")
	}
	if authority.IntegrationBaseSHA != "" && context.IntegrationBase.SHA != authority.IntegrationBaseSHA {
		return tuskerError(errorInvalidTransition, "configured default branch changed before authorization; regenerate delivery context and review")
	}
	return nil
}

func deliveryStartActor(args Args) (string, error) {
	actor := strings.TrimSpace(firstNonEmpty(args.String("by"), args.String("actor")))
	if !strings.HasPrefix(actor, "human:") || strings.TrimSpace(strings.TrimPrefix(actor, "human:")) == "" {
		return "", tuskerError(errorInvalidArg, "delivery start requires an attributable --by human:<name> actor")
	}
	return actor, nil
}

func deliveryStartPlan(vault string, args Args) (string, string, deliveryPlan, []byte, error) {
	path := strings.TrimSpace(firstNonEmpty(args.String("plan"), args.String("_pos0")))
	if path == "" {
		return "", "", deliveryPlan{}, nil, tuskerError(errorMissingArg, "Usage: tusker delivery start --plan <plan.yaml> --by human:<name> --confirm <fingerprint>")
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(v7RepoRoot(vault), path)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", "", deliveryPlan{}, nil, err
	}
	return deliveryStartPlanBytes(vault, args, path, raw)
}

func deliveryStartPlanInput(vault string, args Args, source *deliveryPlanSource) (string, string, deliveryPlan, []byte, error) {
	if source == nil {
		return deliveryStartPlan(vault, args)
	}
	path := strings.TrimSpace(firstNonEmpty(args.String("plan"), args.String("_pos0")))
	if !filepath.IsAbs(path) {
		path = filepath.Join(v7RepoRoot(vault), path)
	}
	if filepath.Clean(path) != filepath.Clean(source.Path) {
		return "", "", deliveryPlan{}, nil, tuskerError(errorInvalidTransition, "bound delivery plan path changed before Start; review the plan again")
	}
	return deliveryStartPlanBytes(vault, args, source.Path, source.Raw)
}

func deliveryStartPlanBytes(vault string, args Args, path string, raw []byte) (string, string, deliveryPlan, []byte, error) {
	confirmed := strings.TrimSpace(args.String("confirm"))
	if confirmed == "" {
		return "", "", deliveryPlan{}, nil, tuskerError(errorMissingArg, "delivery start requires --confirm <exact plan fingerprint>")
	}
	if deliveryFingerprint(raw) != confirmed {
		return "", "", deliveryPlan{}, nil, tuskerError(errorInvalidTransition, "confirmed plan fingerprint differs; rerun delivery review and confirm its exact plan fingerprint")
	}
	if schema, err := deliveryPlanSchemaBytes(raw); err != nil {
		return "", "", deliveryPlan{}, nil, err
	} else if schema != deliveryPlanV2Schema {
		return "", "", deliveryPlan{}, nil, tuskerError(errorInvalidArg, "delivery start requires a tusker.delivery-plan/v2 plan; regenerate the reviewed V2 plan")
	}
	var v2 deliveryPlanV2
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	decoder.KnownFields(true)
	if err := decoder.Decode(&v2); err != nil {
		return "", "", deliveryPlan{}, nil, tuskerError(errorInvalidArg, "invalid V2 delivery plan YAML: "+err.Error())
	}
	plan, issues := deliveryV2Prepare(vault, v2)
	baseIssues, _ := validateDeliveryPlan(vault, plan)
	issues = uniqueStrings(append(issues, baseIssues...))
	sort.Strings(issues)
	if len(issues) > 0 {
		return "", "", deliveryPlan{}, nil, tuskerError(errorInvalidArg, "delivery plan is invalid: "+issues[0])
	}
	doctor, err := deliveryPlanDoctorBytes(vault, path, raw)
	if err != nil {
		return "", "", deliveryPlan{}, nil, err
	}
	if !doctor.OK {
		findings := append([]deliveryDoctorFinding(nil), doctor.Findings...)
		sort.Slice(findings, func(i, j int) bool { return findings[i].Code < findings[j].Code })
		return "", "", deliveryPlan{}, nil, tuskerError(errorInvalidArg, "delivery plan is operationally unsafe: "+findings[0].Message)
	}
	return path, confirmed, plan, append([]byte(nil), raw...), nil
}

func deliveryStartValidateContext(vault string, plan deliveryPlan) (deliveryPlanningContext, error) {
	context, err := buildDeliveryPlanningContextForScope(vault, strings.Join(plan.SpecRefs, ","), deliveryPlanScope(plan))
	if err != nil {
		return deliveryPlanningContext{}, tuskerError(errorInvalidTransition, "planning context could not be recomputed; repair the cited context before Start")
	}
	if context.ContextFingerprint != plan.v2.ContextFingerprint {
		return deliveryPlanningContext{}, tuskerError(errorInvalidTransition, "planning context fingerprint differs; regenerate the plan from current delivery context")
	}
	return context, nil
}

func deliveryStartWave(idx v7Index, plan deliveryPlan, confirmed string) (Note, error) {
	var matches []Note
	for _, wave := range idx.Waves {
		if stringField(wave.Data, "delivery_plan_scope") == deliveryPlanScope(plan) {
			matches = append(matches, wave)
		}
	}
	if len(matches) != 1 {
		return Note{}, tuskerError(errorInvalidTransition, "delivery import did not converge on one exact wave; repair duplicate scope ownership and rerun Start")
	}
	wave := matches[0]
	if stringField(wave.Data, "delivery_plan_fingerprint") != confirmed {
		return Note{}, tuskerError(errorInvalidTransition, "imported wave does not match the confirmed plan; rerun delivery review and Start")
	}
	return wave, nil
}

func deliveryStartProjection(wave Note, planFingerprint, contextFingerprint string, report wavePreflightReport, replayed bool) deliveryStartResult {
	first := []string{}
	if len(report.Frontiers) > 0 {
		first = append(first, report.Frontiers[0]...)
	}
	project := stringField(wave.Data, "project")
	return deliveryStartResult{Schema: deliveryStartSchema, WaveID: report.WaveID, PlanFingerprint: planFingerprint, ContextFingerprint: contextFingerprint, AuthorizationFingerprint: report.Fingerprint, FirstFrontier: first, ExpectedConcurrency: report.ExpectedConcurrency, IntegrationLane: report.IntegrationBranch, StatusLink: waveDeepLink(project, report.WaveID), Replayed: replayed}
}

func deliveryStartPreflightRemedy(report wavePreflightReport) string {
	if len(report.Blockers) == 0 {
		return "rerun tusker delivery start after preflight succeeds"
	}
	return "fix preflight blocker: " + report.Blockers[0] + "; then rerun tusker delivery start"
}
