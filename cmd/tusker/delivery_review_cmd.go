package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const deliveryReviewSchema = "tusker.delivery-review/v1"

// deliveryReview is deliberately a product projection. It contains no task
// frontmatter, runner instructions, logs, or lifecycle authority.
type deliveryReview struct {
	Schema    string                   `json:"schema"`
	ReadOnly  bool                     `json:"readOnly"`
	Ready     bool                     `json:"ready"`
	What      []deliveryReviewOutcome  `json:"whatWillBeDelivered"`
	Proof     []deliveryReviewProof    `json:"howItWillBeProven"`
	Flow      deliveryReviewFlow       `json:"howWorkFlows"`
	Decisions []deliveryReviewDecision `json:"whatNeedsYourDecision"`
	Start     deliveryReviewStart      `json:"startBoundary"`
	NonGoals  []string                 `json:"nonGoals"`
}
type deliveryReviewOutcome struct {
	Requirement string               `json:"requirement"`
	Outcome     string               `json:"outcome"`
	NonGoals    []string             `json:"nonGoals"`
	Links       []deliveryReviewLink `json:"links"`
}
type deliveryReviewProof struct {
	Requirements []string                 `json:"requirements"`
	Outcome      string                   `json:"outcome"`
	Acceptance   []string                 `json:"acceptance"`
	Tests        []string                 `json:"tests"`
	Artifacts    []string                 `json:"artifacts"`
	SourceKey    string                   `json:"sourceKey"`
	TaskID       string                   `json:"taskId,omitempty"`
	TaskHref     string                   `json:"taskHref,omitempty"`
	Checks       []deliveryReviewCheck    `json:"checks"`
	ArtifactRefs []deliveryReviewArtifact `json:"artifactRefs"`
	ResourceRefs []string                 `json:"resourceRefs"`
}
type deliveryReviewCheck struct {
	Covers string `json:"covers"`
	Check  string `json:"check"`
	Notes  string `json:"notes,omitempty"`
	Href   string `json:"href,omitempty"`
}
type deliveryReviewArtifact struct {
	Kind          string   `json:"kind"`
	Path          string   `json:"path"`
	Summary       string   `json:"summary"`
	AcceptanceIDs []string `json:"acceptanceIds"`
	Href          string   `json:"href,omitempty"`
}
type deliveryReviewLink struct {
	Label string `json:"label"`
	Href  string `json:"href"`
}
type deliveryReviewFlow struct {
	Frontiers           [][]string               `json:"frontiers"`
	ExpectedConcurrency int                      `json:"expectedConcurrency"`
	Integration         string                   `json:"integration"`
	SharedResources     []deliveryReviewResource `json:"sharedResources"`
	Warnings            []string                 `json:"warnings"`
	WaveID              string                   `json:"waveId,omitempty"`
	WaveHref            string                   `json:"waveHref,omitempty"`
}
type deliveryReviewResource struct {
	SourceKey      string               `json:"sourceKey"`
	Kind           string               `json:"kind"`
	Capacity       *int                 `json:"capacity,omitempty"`
	CapacityStatus string               `json:"capacityStatus"`
	Constraints    []string             `json:"constraints"`
	ReferencedBy   []string             `json:"referencedBy"`
	TaskLinks      []deliveryReviewLink `json:"taskLinks"`
}
type deliveryReviewDecision struct {
	Title         string   `json:"title"`
	Action        string   `json:"action"`
	Why           string   `json:"why"`
	SourceKey     string   `json:"sourceKey,omitempty"`
	GateID        string   `json:"gateId,omitempty"`
	GateHref      string   `json:"gateHref,omitempty"`
	TaskSourceKey string   `json:"taskSourceKey,omitempty"`
	TaskID        string   `json:"taskId,omitempty"`
	AcceptanceIDs []string `json:"acceptanceIds"`
	Verification  string   `json:"verification,omitempty"`
}
type deliveryReviewStart struct {
	PlanFingerprint    string   `json:"planFingerprint"`
	ContextFingerprint string   `json:"contextFingerprint,omitempty"`
	Authorization      string   `json:"authorization"`
	Readiness          string   `json:"readiness"`
	Blockers           []string `json:"blockers"`
	NextAction         string   `json:"nextAction"`
	State              string   `json:"state"`
	StateLabel         string   `json:"stateLabel"`
	ActionHref         string   `json:"actionHref,omitempty"`
}

func deliveryReviewCmd(args Args) error {
	vault, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	path := strings.TrimSpace(firstNonEmpty(args.String("plan"), args.String("_pos0")))
	if path == "" {
		return tuskerError(errorMissingArg, "Usage: tusker delivery review --plan <plan.yaml> [--json]")
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(v7RepoRoot(vault), path)
	}
	review, err := buildDeliveryReview(vault, path)
	if err != nil {
		return err
	}
	if args.Bool("json") {
		emitJSON(review)
	} else {
		fmt.Print(renderDeliveryReview(review))
	}
	if !review.Ready {
		return tuskerError(errorInvalidTransition, "delivery review is not ready: "+review.Start.NextAction, withContext(map[string]any{"delivery_review": review}))
	}
	return nil
}

func buildDeliveryReview(vault, path string) (deliveryReview, error) {
	return buildDeliveryReviewWithInspector(vault, path, inspectWavePreflightEnvironmentReadOnly)
}

func buildDeliveryReviewWithInspector(vault, path string, inspector wavePreflightEnvironmentInspector) (deliveryReview, error) {
	plan, raw, preparationIssues, err := readDeliveryReviewPlan(vault, path)
	if err != nil {
		return deliveryReview{}, err
	}
	issues, frontiers := validateDeliveryPlan(vault, plan)
	issues = uniqueStrings(append(preparationIssues, issues...))
	sort.Strings(issues)
	r := deliveryReview{Schema: deliveryReviewSchema, ReadOnly: true, What: []deliveryReviewOutcome{}, Proof: []deliveryReviewProof{}, Decisions: []deliveryReviewDecision{}, NonGoals: []string{}, Flow: deliveryReviewFlow{Frontiers: deliveryReviewFrontiers(plan, frontiers), ExpectedConcurrency: deliveryExpectedConcurrency(plan, frontiers), Integration: "Reviewed work joins one serialized integration phase before landing.", SharedResources: []deliveryReviewResource{}, Warnings: []string{}}, Start: deliveryReviewStart{PlanFingerprint: deliveryFingerprint(raw), Authorization: "not imported", Readiness: "review only", Blockers: []string{}, State: "held", StateLabel: "Held for review"}}
	integrationBaseSHA := ""
	for _, issue := range issues {
		r.Start.Blockers = append(r.Start.Blockers, issue)
	}
	if plan.v2 != nil {
		findings := deliveryReviewDoctorFindings(vault, path)
		for _, finding := range findings {
			r.Start.Blockers = append(r.Start.Blockers, finding.Message)
			if strings.Contains(finding.Code, "CONFLICT") || strings.Contains(finding.Code, "RESOURCE") {
				r.Flow.Warnings = append(r.Flow.Warnings, finding.Message)
			}
		}
		r.Flow.SharedResources = deliveryReviewSharedResources(plan.v2.SharedResources, plan.Tasks, findings)
		context, contextErr := buildDeliveryPlanningContextForScope(vault, strings.Join(plan.SpecRefs, ","), deliveryPlanScope(plan))
		if contextErr != nil {
			r.Start.Blockers = append(r.Start.Blockers, "planning context could not be recomputed: "+contextErr.Error())
		} else {
			integrationBaseSHA = context.IntegrationBase.SHA
			r.Start.ContextFingerprint = plan.v2.ContextFingerprint
			if context.ContextFingerprint != plan.v2.ContextFingerprint {
				r.Start.Blockers = append(r.Start.Blockers, "planning context fingerprint differs; regenerate the plan from current delivery context")
				r.Start.State = "changed"
				r.Start.StateLabel = "Delivery changed"
				r.Start.NextAction = "Regenerate the plan from current delivery context, then review its new exact fingerprint."
			}
		}
	}
	if integrationBaseSHA == "" {
		var integrationBaseErr error
		integrationBaseSHA, integrationBaseErr = deliveryIntegrationBaseSHA(vault)
		if integrationBaseErr != nil {
			r.Start.Blockers = append(r.Start.Blockers, "configured integration base could not be inspected: "+integrationBaseErr.Error())
		}
	}
	projectID := v7ProjectID(vault)
	for _, task := range plan.Tasks {
		proof := deliveryReviewProof{
			Requirements: append([]string(nil), task.RequirementRefs...),
			Outcome:      task.Outcome, Acceptance: []string{}, Tests: []string{}, Artifacts: []string{},
			SourceKey: task.SourceKey, Checks: []deliveryReviewCheck{}, ArtifactRefs: []deliveryReviewArtifact{},
			ResourceRefs: append([]string(nil), task.ResourceRefs...),
		}
		for _, a := range task.Acceptance {
			proof.Acceptance = append(proof.Acceptance, a.Outcome)
		}
		for _, v := range task.Verification {
			check := strings.TrimSpace(strings.TrimPrefix(v.Check, "command: "))
			proof.Tests = append(proof.Tests, check)
			proof.Checks = append(proof.Checks, deliveryReviewCheck{Covers: v.Covers, Check: check, Notes: v.Notes})
		}
		if task.Artifact.Summary != "" {
			proof.Artifacts = append(proof.Artifacts, task.Artifact.Summary)
			artifact := deliveryReviewArtifact{
				Kind: task.Artifact.Kind, Path: task.Artifact.Path, Summary: task.Artifact.Summary,
				AcceptanceIDs: append([]string(nil), task.Artifact.AcceptanceIDs...),
			}
			if deliveryReviewRepoPathExists(vault, task.Artifact.Path) {
				artifact.Href = docDeepLink(projectID, task.Artifact.Path)
			}
			proof.ArtifactRefs = append(proof.ArtifactRefs, artifact)
		}
		r.Proof = append(r.Proof, proof)
	}
	if plan.v2 != nil {
		for _, req := range plan.v2.Requirements {
			links := []deliveryReviewLink{}
			for _, specRef := range plan.SpecRefs {
				if deliverySpecRefExists(vault, specRef) {
					links = append(links, deliveryReviewLink{Label: specRef, Href: docDeepLink(projectID, specRef)})
				}
			}
			r.What = append(r.What, deliveryReviewOutcome{Requirement: req.ID, Outcome: req.Outcome, NonGoals: []string{}, Links: links})
		}
		r.NonGoals = deliveryContextCleanStrings(plan.v2.NonGoals)
		sort.Strings(r.NonGoals)
		for _, gate := range plan.v2.HumanGates {
			r.Decisions = append(r.Decisions, deliveryReviewDecision{
				Title: gate.Title, Action: gate.Action, Why: gate.WhyAgentCannot,
				SourceKey: gate.SourceKey, TaskSourceKey: gate.TaskSourceKey,
				AcceptanceIDs: append([]string(nil), gate.AcceptanceIDs...), Verification: gate.Verification,
			})
		}
		for _, decision := range plan.v2.UnresolvedDecisions {
			r.Decisions = append(r.Decisions, deliveryReviewDecision{Title: "Product decision", Action: decision.Question, Why: "The plan marks this as unresolved.", SourceKey: decision.SourceKey, AcceptanceIDs: []string{}})
		}
	} else {
		for _, task := range plan.Tasks {
			r.What = append(r.What, deliveryReviewOutcome{Outcome: task.Outcome, NonGoals: []string{}, Links: []deliveryReviewLink{}})
		}
	}
	if len(r.Start.Blockers) > 0 && r.Start.State == "held" {
		r.Start.State = "invalid"
		r.Start.StateLabel = "Delivery plan is invalid"
		r.Start.NextAction = "Resolve the first blocker: " + r.Start.Blockers[0]
	}
	deliveryReviewCanonical(vault, plan, integrationBaseSHA, inspector, &r)
	r.Start.Blockers = uniqueStrings(r.Start.Blockers)
	sort.Strings(r.Start.Blockers)
	r.Flow.Warnings = uniqueStrings(r.Flow.Warnings)
	sort.Strings(r.Flow.Warnings)
	if len(r.Decisions) == 0 {
		r.Decisions = []deliveryReviewDecision{}
	}
	if len(r.Start.Blockers) > 0 {
		if r.Start.State == "held" {
			r.Start.State = "invalid"
			r.Start.StateLabel = "Delivery plan is invalid"
		}
		r.Start.Readiness = "blocked"
		if r.Start.NextAction != "" {
			// Canonical state projection already selected one truthful remedy.
		} else if strings.Contains(r.Start.Blockers[0], "canonical import drift") {
			r.Start.NextAction = "Regenerate delivery review, then rerun Start delivery with the exact reviewed fingerprint."
		} else {
			r.Start.NextAction = "Resolve the first blocker: " + r.Start.Blockers[0]
		}
		return r, nil
	}
	r.Ready = true
	if r.Start.State == "held" {
		r.Start.Readiness = "ready to start delivery"
		r.Start.StateLabel = "Ready to start"
		r.Start.NextAction = deliveryReviewStartCommand(vault, path, r.Start.PlanFingerprint)
	} else if r.Start.Readiness == "review only" {
		r.Start.Readiness = r.Start.StateLabel
	}
	return r, nil
}

func deliveryReviewStartCommand(vault, path, fingerprint string) string {
	return "tusker delivery start --plan " + deliveryReviewPlanArg(vault, path) + " --confirm " + fingerprint + " --by human:<name>"
}

func deliveryReviewSharedResources(resources []deliverySharedResource, tasks []deliveryPlanTask, findings []deliveryDoctorFinding) []deliveryReviewResource {
	out := make([]deliveryReviewResource, 0, len(resources))
	for _, resource := range resources {
		row := deliveryReviewResource{SourceKey: resource.SourceKey, Kind: resource.Kind, CapacityStatus: "not declared", Constraints: []string{}, ReferencedBy: []string{}, TaskLinks: []deliveryReviewLink{}}
		if resource.Capacity > 0 {
			capacity := resource.Capacity
			row.Capacity = &capacity
			row.CapacityStatus = "declared"
		}
		for _, task := range tasks {
			if containsString(task.ResourceRefs, resource.SourceKey) {
				row.ReferencedBy = append(row.ReferencedBy, task.SourceKey)
			}
		}
		for _, finding := range findings {
			if !strings.Contains(finding.Code, "RESOURCE") && !strings.Contains(finding.Code, "CONFLICT") {
				continue
			}
			if containsString(finding.SourceKeys, resource.SourceKey) {
				row.Constraints = append(row.Constraints, finding.Message)
			}
		}
		row.Constraints = uniqueStrings(row.Constraints)
		sort.Strings(row.Constraints)
		row.ReferencedBy = uniqueStrings(row.ReferencedBy)
		sort.Strings(row.ReferencedBy)
		out = append(out, row)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].SourceKey != out[j].SourceKey {
			return out[i].SourceKey < out[j].SourceKey
		}
		return out[i].Kind < out[j].Kind
	})
	return out
}

func deliveryReviewRepoPathExists(vault, path string) bool {
	full, ok := safeRepoPath(v7RepoRoot(vault), filepath.ToSlash(strings.TrimSpace(path)))
	return ok && fileExists(full)
}

func deliveryReviewFrontiers(plan deliveryPlan, frontiers [][]string) [][]string {
	outcomes := map[string]string{}
	for _, task := range plan.Tasks {
		outcomes[task.SourceKey] = task.Outcome
	}
	result := make([][]string, 0, len(frontiers))
	for _, frontier := range frontiers {
		row := make([]string, 0, len(frontier))
		for _, key := range frontier {
			row = append(row, fallback(outcomes[key], "Planned outcome"))
		}
		result = append(result, row)
	}
	return result
}

func readDeliveryReviewPlan(vault, path string) (deliveryPlan, []byte, []string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return deliveryPlan{}, nil, nil, err
	}
	if schema, err := deliveryPlanSchemaAt(path); err != nil {
		return deliveryPlan{}, raw, nil, err
	} else if schema == deliveryPlanV2Schema {
		var v2 deliveryPlanV2
		d := yaml.NewDecoder(bytes.NewReader(raw))
		d.KnownFields(true)
		if err := d.Decode(&v2); err != nil {
			return deliveryPlan{}, raw, nil, tuskerError(errorInvalidArg, "invalid V2 delivery plan YAML: "+err.Error())
		}
		plan, issues := deliveryV2Prepare(vault, v2)
		return plan, raw, issues, nil
	}
	plan, _, err := readDeliveryPlan(path)
	return plan, raw, nil, err
}

func deliveryReviewDoctorFindings(vault, path string) []deliveryDoctorFinding {
	report, err := deliveryPlanDoctor(vault, path)
	if err != nil {
		return []deliveryDoctorFinding{{Code: "PLAN_UNREADABLE", Message: err.Error()}}
	}
	return report.Findings
}

func deliveryReviewCanonical(vault string, plan deliveryPlan, integrationBaseSHA string, inspector wavePreflightEnvironmentInspector, r *deliveryReview) {
	idx, err := loadV7Index(vault)
	if err != nil {
		r.Start.Blockers = append(r.Start.Blockers, err.Error())
		return
	}
	var waves []Note
	for _, wave := range idx.Waves {
		if stringField(wave.Data, "delivery_plan_scope") == deliveryPlanScope(plan) {
			waves = append(waves, wave)
		}
	}
	if len(waves) == 0 {
		if r.Start.State == "invalid" || r.Start.State == "changed" {
			return
		}
		prospective := deliveryReviewProspectiveWave(vault, plan, r.Start.PlanFingerprint, integrationBaseSHA, r.Flow.ExpectedConcurrency)
		env := deliveryReviewInspectEnvironment(vault, prospective, inspector)
		if state, blocked := deliveryReviewEnvironmentState(env, ""); blocked {
			deliveryReviewApplyState(r, state)
			r.Start.Blockers = append(r.Start.Blockers, "operational preflight: "+state.Label)
		}
		return
	}
	if len(waves) != 1 {
		r.Start.State = "changed"
		r.Start.StateLabel = "Delivery changed"
		r.Start.NextAction = "Resolve duplicate plan-scope ownership, then regenerate delivery review."
		r.Start.Blockers = append(r.Start.Blockers, "canonical import drift: more than one wave owns this plan scope; re-import the reviewed plan after resolving duplicate scope ownership")
		return
	}
	wave := waves[0]
	projectID := firstNonEmpty(stringField(wave.Data, "project"), v7ProjectID(vault))
	waveID := stringField(wave.Data, "id")
	if waveID != "" {
		r.Flow.WaveID = waveID
		r.Flow.WaveHref = waveDeepLink(projectID, waveID)
	}
	r.Start.Authorization = fallback(stringField(wave.Data, "authorization"), "disarmed")
	if stringField(wave.Data, "delivery_plan_fingerprint") != r.Start.PlanFingerprint {
		r.Start.State = "changed"
		r.Start.StateLabel = "Delivery changed"
		r.Start.NextAction = "Regenerate delivery review, then confirm the new exact plan fingerprint."
		r.Start.Blockers = append(r.Start.Blockers, "canonical import drift: the plan fingerprint differs; regenerate delivery review, then Start the exact reviewed plan")
		return
	}
	members := map[string]bool{}
	for _, id := range normalizeList(wave.Data["members"]) {
		members[id] = true
	}
	for _, task := range plan.Tasks {
		found := ""
		for id, note := range idx.Tasks {
			if stringField(note.Data, "delivery_plan_scope") == deliveryPlanScope(plan) && stringField(note.Data, "delivery_source_key") == task.SourceKey {
				found = id
				break
			}
		}
		if found == "" || !members[found] {
			r.Start.State = "changed"
			r.Start.StateLabel = "Delivery changed"
			r.Start.NextAction = "Regenerate delivery review, then confirm the new exact plan fingerprint."
			r.Start.Blockers = append(r.Start.Blockers, "canonical import drift: a planned outcome is missing from the imported wave; regenerate delivery review, then Start the exact reviewed plan")
			return
		}
		taskHref := taskDeepLink(projectID, found)
		for i := range r.Proof {
			if r.Proof[i].SourceKey == task.SourceKey {
				r.Proof[i].TaskID = found
				r.Proof[i].TaskHref = taskHref
				for checkIndex := range r.Proof[i].Checks {
					r.Proof[i].Checks[checkIndex].Href = taskHref
				}
			}
		}
		for i := range r.Flow.SharedResources {
			if containsString(task.ResourceRefs, r.Flow.SharedResources[i].SourceKey) {
				r.Flow.SharedResources[i].TaskLinks = append(r.Flow.SharedResources[i].TaskLinks, deliveryReviewLink{Label: found, Href: taskHref})
			}
		}
	}
	for _, gate := range idx.Gates {
		if stringField(gate.Data, "delivery_plan_scope") != deliveryPlanScope(plan) {
			continue
		}
		sourceKey := stringField(gate.Data, "delivery_source_key")
		gateID := stringField(gate.Data, "id")
		blocked := normalizeList(gate.Data["blocks"])
		taskID := ""
		if len(blocked) > 0 {
			taskID = blocked[0]
		}
		for i := range r.Decisions {
			if r.Decisions[i].SourceKey != sourceKey {
				continue
			}
			r.Decisions[i].GateID = gateID
			r.Decisions[i].TaskID = taskID
			if gateID != "" {
				if _, ok := idx.Tasks[taskID]; !ok {
					continue
				}
				r.Decisions[i].GateHref = gateDeepLink(projectID, taskID, gateID)
			}
		}
	}
	env := deliveryReviewInspectEnvironment(vault, wave, inspector)
	preflight := buildWavePreflight(vault, idx, wave, env)
	if r.Start.State != "invalid" && r.Start.State != "changed" {
		deliveryReviewProjectState(vault, idx, wave, env, preflight, r)
	}
	if preflight.AuthorizationStale {
		r.Start.Blockers = append(r.Start.Blockers, "operational preflight: wave authorization fingerprint is stale")
	}
	for _, blocker := range preflight.Blockers {
		if r.Start.State == "completed" && deliveryReviewEnvironmentPreflightBlocker(blocker) {
			continue
		}
		r.Start.Blockers = append(r.Start.Blockers, "operational preflight: "+blocker)
	}
}

type deliveryReviewState struct {
	State  string
	Label  string
	Action string
	Href   string
}

func deliveryReviewProspectiveWave(vault string, plan deliveryPlan, planFingerprint, integrationBaseSHA string, concurrency int) Note {
	materialID := strings.TrimPrefix(strings.TrimSpace(planFingerprint), "sha256:")
	if len(materialID) > 12 {
		materialID = materialID[:12]
	}
	prospectiveID := "PROSPECTIVE-" + strings.ToUpper(materialID)
	data := map[string]any{
		"schema":                    "tusker.wave/v7",
		"kind":                      "wave",
		"id":                        prospectiveID,
		"project":                   v7ProjectID(vault),
		"delivery_plan_schema":      plan.Schema,
		"delivery_plan_scope":       deliveryPlanScope(plan),
		"delivery_plan_fingerprint": planFingerprint,
		"runner_profile":            plan.RunnerProfile,
		"concurrency":               maxInt(1, concurrency),
		"integration_branch":        v7IntegrationBranchName(prospectiveID),
	}
	if integrationBaseSHA != "" {
		data["integration_base_sha"] = integrationBaseSHA
	}
	// This record is inspection material only. In particular, it deliberately
	// carries no authorization claim and is never persisted or linked.
	return Note{Data: data}
}

func deliveryReviewInspectEnvironment(vault string, wave Note, inspector wavePreflightEnvironmentInspector) wavePreflightEnvironment {
	if inspector != nil {
		return inspector(vault, wave)
	}
	return inspectWavePreflightEnvironmentReadOnly(vault, wave)
}

func deliveryReviewEnvironmentState(env wavePreflightEnvironment, statusHref string) (deliveryReviewState, bool) {
	switch {
	case !env.ProjectRegistered:
		return deliveryReviewState{"disabled", "Project is not registered", "Register this project in Project Settings, then review the delivery again.", ""}, true
	case !env.ProjectEnabled:
		return deliveryReviewState{"disabled", "Project automation is off", "Enable this project's automation in Project Settings, then review the delivery again.", ""}, true
	case !env.ProjectHealthy:
		return deliveryReviewState{"disabled", "Project health is blocked", "Repair the project's reported health issue, then review the delivery again.", ""}, true
	case !env.WorkflowCompatible:
		return deliveryReviewState{"invalid", "Workflow is incompatible", "Repair the project workflow version and tracker schema, then review the delivery again.", ""}, true
	case !env.SkillCompatible:
		return deliveryReviewState{"invalid", "Project skill is incompatible", "Install or repair the compatible Tusker project skill, then review the delivery again.", ""}, true
	case !env.DaemonAlive:
		return deliveryReviewState{"daemon-off", "Resident daemon is off", "Start the resident daemon, then review the delivery again.", ""}, true
	case !env.DaemonReconciling:
		return deliveryReviewState{"daemon-off", "Resident daemon is not reconciling", "Repair the resident daemon's project polling, then review the delivery again.", ""}, true
	case !env.RunnerCompatible:
		return deliveryReviewState{"runner-blocked", "Runner is incompatible", "Configure a supported unattended runner for this wave, then review again.", ""}, true
	case !env.ApprovalFree:
		return deliveryReviewState{"runner-blocked", "Runner requires approval", "Configure this runner for approval-free unattended execution, then review again.", ""}, true
	case !env.IsolatedWorkspace:
		return deliveryReviewState{"shared-workspace", "Workspace is shared", "Select an isolated workspace strategy in Project Settings, then review again.", ""}, true
	case !env.IntegrationClean:
		return deliveryReviewState{"shared-workspace", "Integration lane is not clean", "Repair the wave integration lane, then review again.", statusHref}, true
	}
	return deliveryReviewState{}, false
}

func deliveryReviewApplyState(r *deliveryReview, state deliveryReviewState) {
	r.Start.State = state.State
	r.Start.StateLabel = state.Label
	r.Start.NextAction = state.Action
	r.Start.ActionHref = state.Href
}

func deliveryReviewEnvironmentPreflightBlocker(blocker string) bool {
	for _, key := range []string{"project", "daemon", "runner", "skill", "workflow", "approvalPolicy", "workspaceIsolation"} {
		if blocker == waveEnvironmentBlocker(key) {
			return true
		}
	}
	return false
}

func deliveryReviewProjectState(vault string, idx v7Index, wave Note, env wavePreflightEnvironment, preflight wavePreflightReport, r *deliveryReview) {
	projectID := firstNonEmpty(stringField(wave.Data, "project"), v7ProjectID(vault))
	waveID := stringField(wave.Data, "id")
	statusHref := ""
	if waveID != "" {
		statusHref = waveDeepLink(projectID, waveID)
	}
	set := func(state, label, action, href string) {
		deliveryReviewApplyState(r, deliveryReviewState{State: state, Label: label, Action: action, Href: href})
	}
	if preflight.AuthorizationStale {
		set("changed", "Delivery changed", "Regenerate delivery review, then confirm and Start the new exact fingerprint.", "")
		return
	}
	members := normalizeList(wave.Data["members"])
	allDone := len(members) > 0
	for _, memberID := range members {
		task, ok := idx.Tasks[memberID]
		allDone = allDone && ok && stringField(task.Data, "status") == "done"
	}
	if armedWaveIntegrated(wave) || allDone {
		set("completed", "Delivery completed", "Review the delivered artifacts and integration outcome.", statusHref)
		return
	}
	if state, blocked := deliveryReviewEnvironmentState(env, statusHref); blocked {
		deliveryReviewApplyState(r, state)
		return
	}

	runs := deliveryReviewRuntimeRuns(vault, projectID)
	snapshot := buildArmedWaveSnapshot(vault, idx, wave, runs, time.Now())
	hasRunning, hasParked := false, false
	hasGate := preflight.Authorization == "armed" && len(preflight.HumanGates) > 0
	firstGateID := ""
	if hasGate {
		firstGateID = stringField(preflight.HumanGates[0], "id")
	}
	firstParked := ""
	for _, member := range snapshot.Members {
		switch member.State {
		case armedWaveRunning:
			hasRunning = true
		case armedWaveMachineParked:
			hasParked = true
			if firstParked == "" {
				firstParked = member.Reason
			}
		case armedWaveHumanBlocked:
			hasGate = true
		}
	}
	switch {
	case hasParked:
		set("parked", "Delivery parked", "Resolve the first parked outcome: "+fallback(firstParked, "inspect the wave status for its recovery action"), statusHref)
	case hasGate:
		action := "Open the blocking human gate, record the decision, then resume delivery."
		if firstGateID != "" {
			action = "Open " + firstGateID + ", record the decision, then resume delivery."
		}
		set("gated", "Waiting on a human decision", action, statusHref)
		for _, decision := range r.Decisions {
			if decision.GateHref != "" && (firstGateID == "" || decision.GateID == firstGateID) {
				r.Start.ActionHref = decision.GateHref
				break
			}
		}
	case hasRunning:
		set("running", "Delivery running", "Observe the current wave; no additional Start action is needed.", statusHref)
	case preflight.Authorization == "armed":
		set("armed", "Delivery armed", "Observe the armed wave; the resident daemon owns the next runnable frontier.", statusHref)
	default:
		set("held", "Held for review", "", "")
		r.Start.ActionHref = ""
	}
}

func deliveryReviewRuntimeRuns(vault, projectID string) map[string]RunStatus {
	out := map[string]RunStatus{}
	store, err := OpenRuntimeStoreReadOnly(DefaultStateRoot())
	if err != nil {
		return out
	}
	defer store.Close()
	if projects, projectErr := store.ListProjects(); projectErr == nil {
		selectedProjectID := ""
		for _, project := range projects {
			vaultMatch := sameCanonicalProjectPath(project.VaultRoot, vault)
			repoMatch := sameCanonicalProjectPath(project.RepoRoot, v7RepoRoot(vault))
			if !vaultMatch && !repoMatch {
				continue
			}
			if projectID != "" && project.ProjectID == projectID {
				selectedProjectID = project.ProjectID
				break
			}
			if selectedProjectID == "" && vaultMatch {
				selectedProjectID = project.ProjectID
			}
			if selectedProjectID == "" && repoMatch {
				selectedProjectID = project.ProjectID
			}
		}
		if selectedProjectID != "" {
			projectID = selectedProjectID
		}
	}
	runs, err := store.ListRuns()
	if err != nil {
		return out
	}
	for _, run := range runs {
		if projectID == "" || run.ProjectID == projectID {
			out[firstNonEmpty(run.ItemID, run.RecordID)] = run
		}
	}
	return out
}

func deliveryReviewPlanArg(vault, path string) string {
	if rel, err := filepath.Rel(v7RepoRoot(vault), path); err == nil && !strings.HasPrefix(rel, "..") {
		return filepath.ToSlash(rel)
	}
	return "<plan.yaml>"
}

func renderDeliveryReview(r deliveryReview) string {
	var b strings.Builder
	b.WriteString("Delivery review (read-only; does not start delivery)\n\nWhat will be delivered\n")
	if len(r.What) == 0 {
		b.WriteString("- No requirement outcomes declared.\n")
	}
	for _, v := range r.What {
		b.WriteString("- " + v.Outcome + "\n")
	}
	if len(r.NonGoals) == 0 {
		b.WriteString("- Non-goals: None declared.\n")
	} else {
		b.WriteString("- Non-goals: " + strings.Join(r.NonGoals, "; ") + "\n")
	}
	b.WriteString("\nHow it will be proven\n")
	if len(r.Proof) == 0 {
		b.WriteString("- No proof coverage declared.\n")
	}
	for _, p := range r.Proof {
		b.WriteString("- " + p.Outcome + "\n")
	}
	b.WriteString("\nHow work flows\n")
	for i, f := range r.Flow.Frontiers {
		b.WriteString(fmt.Sprintf("- Phase %d: %s\n", i+1, strings.Join(f, ", ")))
	}
	b.WriteString(fmt.Sprintf("- Expected parallel work: %d; %s\n", r.Flow.ExpectedConcurrency, r.Flow.Integration))
	for _, resource := range r.Flow.SharedResources {
		capacity := "capacity not declared"
		if resource.Capacity != nil {
			capacity = fmt.Sprintf("capacity %d", *resource.Capacity)
		}
		b.WriteString(fmt.Sprintf("- Shared resource %s (%s): %s.\n", resource.SourceKey, resource.Kind, capacity))
	}
	b.WriteString("\nWhat needs your decision\n")
	if len(r.Decisions) == 0 {
		b.WriteString("- Nothing.\n")
	}
	for _, d := range r.Decisions {
		b.WriteString("- " + d.Title + ": " + d.Action + "\n")
	}
	b.WriteString("\nStart boundary\n")
	b.WriteString("- Plan: " + r.Start.PlanFingerprint)
	if r.Start.ContextFingerprint != "" {
		b.WriteString("; context: " + r.Start.ContextFingerprint)
	}
	b.WriteString("; authorization: " + r.Start.Authorization + "; readiness: " + r.Start.Readiness + "\n- Next action: " + r.Start.NextAction + "\n")
	return b.String()
}
