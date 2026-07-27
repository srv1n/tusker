package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const waveAuthorizationSchema = "tusker.wave-authorization/v1"

var deliveryStartAfterArmCommit func() error

// closeV7AuthorizationLock is a narrow seam for exercising the transaction's
// release failure handling. v7DocumentLock.Close itself always attempts both
// unlock and file close; this seam must therefore only be used while releasing
// authorization transaction locks.
var closeV7AuthorizationLock = func(lock *v7DocumentLock) error {
	return lock.Close()
}

type wavePreflightEnvironment struct {
	ProjectRegistered  bool
	ProjectEnabled     bool
	ProjectHealthy     bool
	DaemonAlive        bool
	DaemonReconciling  bool
	RunnerCompatible   bool
	SkillCompatible    bool
	WorkflowCompatible bool
	ApprovalFree       bool
	IsolatedWorkspace  bool
	IntegrationClean   bool
}

type wavePreflightEnvironmentInspector func(vaultPath string, wave Note) wavePreflightEnvironment

type waveCrossScopeProjection = deliveryCrossScopeReviewProjection

type wavePreflightReport struct {
	Schema               string                            `json:"schema"`
	WaveID               string                            `json:"waveId"`
	OK                   bool                              `json:"ok"`
	ReadOnly             bool                              `json:"readOnly"`
	Fingerprint          string                            `json:"fingerprint"`
	StoredFingerprint    string                            `json:"storedFingerprint,omitempty"`
	Authorization        string                            `json:"authorization"`
	AuthorizationStale   bool                              `json:"authorizationStale"`
	Action               string                            `json:"action"`
	Members              []string                          `json:"members"`
	Frontiers            [][]string                        `json:"frontiers"`
	TaskProof            map[string]any                    `json:"taskProof"`
	Artifacts            map[string]any                    `json:"artifacts"`
	ExternalDependencies map[string]any                    `json:"externalDependencies"`
	CrossScopeReview     waveCrossScopeProjection          `json:"crossScopeDependencies"`
	HumanGates           []map[string]any                  `json:"humanGates"`
	ExpectedConcurrency  int                               `json:"expectedConcurrency"`
	ValidationLane       string                            `json:"validationLane"`
	IntegrationBranch    string                            `json:"integrationBranch"`
	LandingPolicy        string                            `json:"landingPolicy"`
	DispatchScope        automationDispatchScopeProjection `json:"dispatchScope"`
	Checks               map[string]bool                   `json:"checks"`
	Blockers             []string                          `json:"blockers"`
}

func waveV7PreflightCmd(args Args) error {
	vaultPath, wave, idx, err := loadWaveAuthorizationTarget(args)
	if err != nil {
		return err
	}
	env := inspectWavePreflightEnvironment(vaultPath, wave)
	report := buildWavePreflight(vaultPath, idx, wave, env)
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": report.OK, "preflight": report})
		return nil
	}
	fmt.Print(renderWavePreflight(report))
	if !report.OK {
		return tuskerError(errorInvalidTransition, stringField(wave.Data, "id")+": wave preflight blocked: "+strings.Join(report.Blockers, "; "), withContext(report))
	}
	return nil
}

func waveV7ArmCmd(args Args) error {
	return mutateWaveAuthorization(args, "armed", nil)
}

func waveV7PauseCmd(args Args) error {
	return mutateWaveAuthorization(args, "paused", nil)
}

func waveV7ResumeCmd(args Args) error {
	return mutateWaveAuthorization(args, "armed", nil)
}

func waveV7DisarmCmd(args Args) error {
	return mutateWaveAuthorization(args, "disarmed", nil)
}

func loadWaveAuthorizationTarget(args Args) (string, Note, v7Index, error) {
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return "", Note{}, v7Index{}, err
	}
	id := strings.ToUpper(strings.TrimSpace(firstNonEmpty(args.String("id"), args.String("_pos0"))))
	if id == "" {
		return "", Note{}, v7Index{}, tuskerError(errorMissingArg, "wave id is required")
	}
	idx, err := loadV7Index(vaultPath)
	if err != nil {
		return "", Note{}, idx, err
	}
	wave, ok := idx.Waves[id]
	if !ok {
		return "", Note{}, idx, tuskerError(errorNotFound, "V7 wave not found: "+id)
	}
	return vaultPath, wave, idx, nil
}

func inspectWavePreflightEnvironment(vaultPath string, wave Note) wavePreflightEnvironment {
	env := wavePreflightEnvironment{}
	stateRoot := DefaultStateRoot()
	if fileExists(runtimeStoreDBPath(stateRoot)) {
		store, storeErr := OpenRuntimeStore(stateRoot)
		if storeErr == nil {
			defer store.Close()
			if ctx, contextErr := loadAutomationCommandContextWithStore(Args{"vault": vaultPath}, stateRoot, store); contextErr == nil {
				env.ProjectRegistered = ctx.ProjectRegistered
				env.ProjectEnabled = ctx.ProjectRegistered && ctx.Project.Enabled && ctx.Workflow.Data.AutomationEnabled
				env.ProjectHealthy = ctx.ProjectRegistered && ctx.Project.Health == projectHealthHealthy
				applyWaveWorkflowEnvironment(&env, wave, ctx.Workflow.Data)
				if lastPoll, parseErr := time.Parse(time.RFC3339, ctx.Project.LastPollAt); parseErr == nil {
					env.DaemonReconciling = time.Since(lastPoll) <= daemonHeartbeatDeadThreshold
				}
			}
			if status, statusErr := store.DaemonStatus(); statusErr == nil {
				env.DaemonAlive = boolFromAny(status["daemon_alive"])
			}
			env.DaemonReconciling = env.DaemonAlive && env.DaemonReconciling
		}
	} else if wf, workflowErr := loadWorkflow(vaultPath); workflowErr == nil {
		applyWaveWorkflowEnvironment(&env, wave, wf.Data)
	}
	env.SkillCompatible = waveSkillCompatible(vaultPath)
	env.IntegrationClean = waveIntegrationBaseClean(vaultPath, wave)
	return env
}

// inspectWavePreflightEnvironmentReadOnly projects the same environment facts
// without opening the runtime store through its migration/reconciliation path.
// Delivery review and other diagnostic surfaces must use this observer.
func inspectWavePreflightEnvironmentReadOnly(vaultPath string, wave Note) wavePreflightEnvironment {
	env := wavePreflightEnvironment{}
	stateRoot := DefaultStateRoot()
	store, storeErr := OpenRuntimeStoreReadOnly(stateRoot)
	if storeErr == nil {
		defer store.Close()
		projects, projectsErr := loadRegisteredProjects(store, registeredProjectLoadOptions{MetadataOnly: true, LoadDisabled: true})
		if projectsErr == nil {
			repoRoot := v7RepoRoot(vaultPath)
			wantedProjectID := stringField(wave.Data, "project")
			selected := -1
			for i, loaded := range projects {
				project := loaded.Project
				vaultMatch := sameCanonicalProjectPath(project.VaultRoot, vaultPath)
				repoMatch := sameCanonicalProjectPath(project.RepoRoot, repoRoot)
				if !vaultMatch && !repoMatch {
					continue
				}
				switch {
				case wantedProjectID != "" && project.ProjectID == wantedProjectID:
					selected = i
				case selected < 0 && vaultMatch:
					selected = i
				case selected < 0 && repoMatch:
					selected = i
				}
			}
			if selected >= 0 {
				project := projects[selected].Project
				env.ProjectRegistered = true
				env.ProjectEnabled = project.Enabled
				env.ProjectHealthy = project.Health == projectHealthHealthy
				if loaded, workflowErr := loadProjectContents(store, project, false); workflowErr == nil && registeredProjectIdentityMatches(project, loaded.Project) {
					wf := loaded.Workflow
					env.ProjectEnabled = env.ProjectEnabled && wf.Data.AutomationEnabled
					applyWaveWorkflowEnvironment(&env, wave, wf.Data)
				}
				if lastPoll, parseErr := time.Parse(time.RFC3339, project.LastPollAt); parseErr == nil {
					env.DaemonReconciling = time.Since(lastPoll) <= daemonHeartbeatDeadThreshold
				}
			}
		}
		if status, statusErr := store.DaemonStatus(); statusErr == nil {
			env.DaemonAlive = boolFromAny(status["daemon_alive"])
		}
		env.DaemonReconciling = env.DaemonAlive && env.DaemonReconciling
		if !env.ProjectRegistered {
			if wf, workflowErr := loadWorkflow(vaultPath); workflowErr == nil {
				applyWaveWorkflowEnvironment(&env, wave, wf.Data)
			}
		}
	} else if errors.Is(storeErr, os.ErrNotExist) {
		if wf, workflowErr := loadWorkflow(vaultPath); workflowErr == nil {
			applyWaveWorkflowEnvironment(&env, wave, wf.Data)
		}
	}
	env.SkillCompatible = waveSkillCompatible(vaultPath)
	env.IntegrationClean = waveIntegrationBaseClean(vaultPath, wave)
	return env
}

func applyWaveWorkflowEnvironment(env *wavePreflightEnvironment, wave Note, wf Workflow) {
	env.WorkflowCompatible = wf.WorkflowVersion == 1 && wf.TrackerSchemaVersion == 7
	profileName := stringField(wave.Data, "runner_profile")
	if profileName != "" {
		profile, ok := wf.RunnerProfiles[profileName]
		env.RunnerCompatible = ok && profile.Harness != ""
		if ok {
			selected := ResolvedRunnerProfile{Name: profileName, Definition: profile}
			policy := codexPolicyForResolvedProfile(codexPolicyFromWorkflow(wf), runLaneExecute, selected)
			switch RunnerName(strings.TrimSpace(profile.Harness)) {
			case RunnerCodex, RunnerCodexAppServer, RunnerCodexExec:
				env.ApprovalFree = strings.EqualFold(policy.ApprovalPolicy, "never") || strings.EqualFold(policy.ApprovalPolicy, "bypass")
			default:
				// Preserve the legacy non-Codex signal until those harnesses
				// expose a normalized effective approval policy.
				env.ApprovalFree = profile.PermissionPreset == "danger-full-access"
			}
		}
	} else {
		env.RunnerCompatible = wf.Agents.Default != "" && containsString(wf.Agents.Enabled, wf.Agents.Default)
		env.ApprovalFree = strings.EqualFold(wf.Codex.ApprovalPolicy, "never") || strings.EqualFold(wf.Codex.ApprovalPolicy, "bypass")
	}
	env.IsolatedWorkspace = workspaceStrategyFromWorkflow(wf.Workspace.Strategy) != WorkspaceStrategyShared
}

func waveSkillCompatible(vaultPath string) bool {
	projectSkill, _, err := parseFrontmatterMustRead(filepath.Join(vaultPath, "SKILL.md"))
	if err != nil || stringField(projectSkill, "schema") != "tusker.project-skill/v7" || stringField(projectSkill, "operator_skill") != "tusker" {
		return false
	}
	repoRoot := v7RepoRoot(vaultPath)
	for _, path := range []string{filepath.Join(repoRoot, ".agents", "skills", "tusker", "SKILL.md"), filepath.Join(repoRoot, ".claude", "skills", "tusker", "SKILL.md"), filepath.Join(repoRoot, "skills", "tusker", "SKILL.md"), filepath.Join(repoRoot, "skill", "SKILL.md")} {
		data, _, readErr := parseFrontmatterMustRead(path)
		metadata := mapField(data, "metadata")
		want, provenanceErr := embeddedFactoryIntakeContractProvenance()
		if readErr == nil && provenanceErr == nil && stringField(metadata, "wave_authorization_schema") == waveAuthorizationSchema && intField(metadata, "workflow_version") == 1 && intField(metadata, "tracker_schema_version") == 7 && stringField(metadata, "factory_intake_contract_schema") == want.Schema && stringField(metadata, "factory_intake_contract_version") == want.Version && stringField(metadata, "factory_intake_contract_fingerprint") == want.Fingerprint {
			return true
		}
	}
	return false
}

func waveIntegrationBaseClean(vaultPath string, wave Note) bool {
	repoRoot := v7RepoRoot(vaultPath)
	branch := firstNonEmpty(stringField(wave.Data, "integration_branch"), v7IntegrationBranchName(stringField(wave.Data, "id")))
	if !v7GitRepo(repoRoot) {
		return false
	}
	defaultBranch := v7DefaultBranch(vaultPath)
	baseRev, baseErr := gitCombined(repoRoot, "rev-parse", "refs/heads/"+defaultBranch)
	if baseErr != nil {
		return false
	}
	baseRev = strings.TrimSpace(baseRev)
	frozenBase := strings.TrimSpace(stringField(wave.Data, "integration_base_sha"))
	branchRef := "refs/heads/" + branch
	if !gitRefExists(repoRoot, branchRef) {
		// A freshly imported delivery wave intentionally has no integration ref.
		// It is clean only while its frozen base still is the configured default.
		if frozenBase == "" || frozenBase != baseRev {
			return false
		}
	} else {
		integrationRev, integrationErr := gitCombined(repoRoot, "rev-parse", branchRef)
		if integrationErr != nil {
			return false
		}
		integrationRev = strings.TrimSpace(integrationRev)
		if stringField(wave.Data, "authorized_at") == "" {
			expected := firstNonEmpty(frozenBase, baseRev)
			if integrationRev != expected {
				return false
			}
		} else {
			expected := firstNonEmpty(frozenBase, baseRev)
			if _, err := gitCombined(repoRoot, "merge-base", "--is-ancestor", expected, integrationRev); err != nil {
				return false
			}
		}
	}
	for _, worktree := range v7ListWorktrees(repoRoot) {
		if worktree.Branch != branchRef {
			continue
		}
		dirty, err := inPlaceDirtyPaths(worktree.Path)
		if err != nil || len(dirty) > 0 {
			return false
		}
	}
	return true
}

func buildWavePreflight(vaultPath string, idx v7Index, wave Note, env wavePreflightEnvironment) wavePreflightReport {
	members := uniqueStrings(normalizeList(wave.Data["members"]))
	sort.Strings(members)
	fingerprint, fpIssues := waveMaterialFingerprint(vaultPath, idx, wave)
	dispatchScope := defaultAutomationDispatchScope()
	if wf, err := loadWorkflow(vaultPath); err == nil {
		dispatchScope = wf.Data.DispatchScope
	}
	report := wavePreflightReport{
		Schema: waveAuthorizationSchema, WaveID: stringField(wave.Data, "id"), ReadOnly: true,
		Fingerprint: fingerprint, StoredFingerprint: stringField(wave.Data, "authorization_fingerprint"),
		Authorization: fallback(stringField(wave.Data, "authorization"), "disarmed"), Members: members,
		TaskProof: map[string]any{}, Artifacts: map[string]any{}, ExternalDependencies: map[string]any{}, ExpectedConcurrency: maxInt(1, intField(wave.Data, "concurrency")),
		CrossScopeReview: deliveryCrossScopeReviewForTaskIDs(idx, members),
		ValidationLane:   "serialized integration validation", IntegrationBranch: stringField(wave.Data, "integration_branch"),
		LandingPolicy: "task branches -> wave integration branch -> configured default branch",
		DispatchScope: dispatchScope,
		Checks:        map[string]bool{"specDag": true, "taskContracts": true, "artifacts": true, "project": env.ProjectRegistered && env.ProjectEnabled && env.ProjectHealthy, "daemon": env.DaemonAlive && env.DaemonReconciling, "runner": env.RunnerCompatible, "skill": env.SkillCompatible, "workflow": env.WorkflowCompatible, "approvalPolicy": env.ApprovalFree, "workspaceIsolation": env.IsolatedWorkspace && env.IntegrationClean},
	}
	report.Blockers = append(report.Blockers, fpIssues...)
	report.Blockers = append(report.Blockers, waveFactoryIntakeContractBlockers(vaultPath, wave)...)
	graph := map[string][]string{}
	memberSet := makeSet(members...)
	for _, id := range members {
		task, ok := idx.Tasks[id]
		if !ok {
			report.Blockers = append(report.Blockers, "member task does not resolve: "+id)
			continue
		}
		if edge, blocked := v7CrossScopeIntegrityBlocker(task, idx); blocked {
			report.Blockers = append(report.Blockers, id+": cross-scope dependency integrity is stale at "+edge.ID)
		}
		contractBlockers := waveTaskContractBlockers(vaultPath, task)
		for _, blocker := range contractBlockers {
			report.Blockers = append(report.Blockers, id+": "+blocker)
		}
		report.TaskProof[id] = map[string]any{
			"acceptance":   waveMaterialTable(sectionContent(task.Body, "## Acceptance"), []int{0, 1}),
			"verification": waveMaterialTable(sectionContent(task.Body, "## Verification"), []int{0, 1}),
			"proofMode":    stringField(task.Data, "proof_mode"), "proofRequired": normalizeList(task.Data["proof_required"]),
		}
		artifact := mapField(task.Data, "artifact_contract")
		if deliveryPlaceholder(stringField(artifact, "kind")) || deliveryInvalidProductionPath(stringField(artifact, "path")) || deliveryPlaceholder(stringField(artifact, "summary")) {
			report.Blockers = append(report.Blockers, id+": artifact contract requires kind, path, and summary")
		} else {
			report.Artifacts[id] = artifact
		}
		for _, ref := range normalizeList(task.Data["spec_refs"]) {
			if !deliverySpecRefExists(vaultPath, ref) {
				report.Blockers = append(report.Blockers, id+": spec_ref does not resolve: "+ref)
			}
		}
		for _, edge := range v7TaskDependencyEdges(task, idx) {
			depID := edge.ID
			if depID == "" {
				continue
			}
			if _, exists := idx.Tasks[depID]; !exists {
				report.Blockers = append(report.Blockers, id+": dependency does not resolve: "+depID)
				continue
			}
			if _, inside := memberSet[depID]; inside {
				graph[id] = append(graph[id], depID)
			} else {
				depNote := idx.Tasks[depID]
				satisfied := v7DependencySatisfiedForReadiness(edge, depNote, true)
				qualified := false
				if projections, projectionErr := deliveryCrossScopeProjections(task); projectionErr == nil {
					for _, projection := range projections {
						if projection.TaskID == depID {
							qualified = true
							break
						}
					}
				}
				report.ExternalDependencies[id] = appendAny(report.ExternalDependencies[id], map[string]any{"id": depID, "hardness": edge.Hardness, "status": stringField(depNote.Data, "status"), "proof": stringField(depNote.Data, "proof_status"), "satisfied": satisfied, "qualified": qualified})
				if !satisfied && !qualified {
					report.Blockers = append(report.Blockers, id+": external dependency "+depID+" is not satisfied or authorized by this wave")
				}
			}
		}
	}
	report.Frontiers, _ = waveDependencyFrontiers(members, graph)
	if len(report.Frontiers) == 0 && len(members) > 0 {
		report.Blockers = append(report.Blockers, "member dependency graph contains a cycle")
	}
	for _, gate := range sortedV7Gates(idx) {
		var affected []string
		for _, id := range normalizeList(gate.Data["blocks"]) {
			if _, ok := memberSet[id]; ok {
				affected = append(affected, id)
			}
		}
		for _, id := range members {
			if containsString(normalizeList(idx.Tasks[id].Data["gates"]), stringField(gate.Data, "id")) {
				affected = append(affected, id)
			}
		}
		if len(affected) == 0 {
			continue
		}
		affected = waveAffectedClosure(uniqueStrings(affected), graph)
		if !v7GateAuthorityReceiptCurrent(gate, idx) {
			report.Blockers = append(report.Blockers, v7GateAuthorityReceiptStaleCode+" "+stringField(gate.Data, "id")+": auth/release authority receipt does not match the current completed hard dependency material")
		}
		if stringField(gate.Data, "status") != "open" || v7ProofOwnerClass(stringField(gate.Data, "owner")) != "human" {
			continue
		}
		if !v7GateHasAgentBoundary(gate) {
			report.Blockers = append(report.Blockers, stringField(gate.Data, "id")+": human gate does not explain why an agent cannot resolve it")
		}
		if v7HumanGateOwnsAgentCapableWork(stringField(gate.Data, "gate_kind"), stringField(gate.Data, "owner"), stringField(gate.Data, "action"), stringField(gate.Data, "verification"), v7GateBoundaryText(gate), v7GateSuggestionText(gate)) {
			report.Blockers = append(report.Blockers, stringField(gate.Data, "id")+": human gate owns agent-capable work")
		}
		report.HumanGates = append(report.HumanGates, map[string]any{"id": stringField(gate.Data, "id"), "action": stringField(gate.Data, "action"), "affected": affected})
	}
	for key, ok := range report.Checks {
		if !ok {
			report.Blockers = append(report.Blockers, waveEnvironmentBlocker(key))
		}
	}
	report.Blockers = uniqueStrings(filterStrings(report.Blockers))
	sort.Strings(report.Blockers)
	report.Checks["specDag"] = !hasWaveBlocker(report.Blockers, "spec", "dependency", "cycle", "member")
	report.Checks["taskContracts"] = !hasWaveBlocker(report.Blockers, "acceptance", "verification", "proof_mode", "proof_required")
	report.Checks["artifacts"] = !hasWaveBlocker(report.Blockers, "artifact contract")
	report.AuthorizationStale = report.StoredFingerprint != "" && report.StoredFingerprint != report.Fingerprint
	if report.AuthorizationStale {
		report.Authorization = "stale"
	}
	report.OK = len(report.Blockers) == 0
	report.Action = waveAuthorizationAction(report)
	return report
}

func waveTaskContractBlockers(vaultPath string, task Note) []string {
	copy := task
	copy.Data = cloneMap(task.Data)
	copy.Data["status"] = "ready"
	copy.Data["readiness"] = "ready"
	copy.Data["next_owner"] = "agent"
	var blockers []string
	for _, blocker := range v7TaskDispatchBlockers(vaultPath, copy) {
		if strings.HasPrefix(blocker, "wave ") {
			continue
		}
		blockers = append(blockers, blocker)
	}
	return blockers
}

func waveMaterialFingerprint(vaultPath string, idx v7Index, wave Note) (string, []string) {
	material := map[string]any{"schema": waveAuthorizationSchema, "wave": stringField(wave.Data, "id"), "delivery_plan_schema": stringField(wave.Data, "delivery_plan_schema"), "integration_base_sha": stringField(wave.Data, "integration_base_sha"), "factory_intake_contract": map[string]string{"schema": stringField(wave.Data, "factory_intake_contract_schema"), "version": stringField(wave.Data, "factory_intake_contract_version"), "fingerprint": stringField(wave.Data, "factory_intake_contract_fingerprint")}, "members": []any{}, "specs": map[string]any{}}
	var issues []string
	members := uniqueStrings(normalizeList(wave.Data["members"]))
	sort.Strings(members)
	rows := make([]any, 0, len(members))
	for _, id := range members {
		task, ok := idx.Tasks[id]
		if !ok {
			issues = append(issues, "member task does not resolve: "+id)
			continue
		}
		row := map[string]any{
			"id": id, "title": stringField(task.Data, "title"), "epic": stringField(task.Data, "epic"), "risk": stringField(task.Data, "risk"),
			"dependencies": sortedStrings(normalizeList(task.Data["dependencies"])), "spec_refs": sortedStrings(normalizeList(task.Data["spec_refs"])),
			"delivery_contract_fingerprint": stringField(task.Data, "delivery_contract_fingerprint"), "artifact_contract": task.Data["artifact_contract"],
			"owned_paths": sortedStrings(normalizeList(task.Data["owned_paths"])), "runner_profile": stringField(task.Data, "runner_profile"), "complexity": stringField(task.Data, "complexity"),
			"proof_contract":                    map[string]any{"mode": stringField(task.Data, "proof_mode"), "required": sortedStrings(normalizeList(task.Data["proof_required"])), "required_owner": task.Data["proof_required_owner"], "evidence_budget": intField(task.Data, "evidence_budget"), "evidence_required": sortedStrings(normalizeList(task.Data["evidence_required"]))},
			"gates":                             waveMaterialGates(idx, id),
			"delivery_cross_scope_dependencies": task.Data["delivery_cross_scope_dependencies"],
			"delivery_cross_scope_targets":      waveMaterialCrossScopeTargets(task, idx),
		}
		// Historical and non-strict tasks retain their existing wave material.
		// A strict projection is authority material, not a mutable status marker.
		if proofFingerprint := stringField(task.Data, "delivery_proof_contract_fingerprint"); proofFingerprint != "" {
			row["delivery_proof_contract"] = task.Data["delivery_proof_contract"]
			row["delivery_proof_contract_fingerprint"] = proofFingerprint
		}
		// Imported delivery tasks already carry the immutable source-contract
		// fingerprint. Their live Verification table is also the proof ledger, so
		// reviewers may append rows without invalidating one-arm authorization.
		if stringField(task.Data, "delivery_contract_fingerprint") == "" {
			row["acceptance"] = waveMaterialTable(sectionContent(task.Body, "## Acceptance"), []int{0, 1})
			row["verification"] = waveMaterialTable(sectionContent(task.Body, "## Verification"), []int{0, 1})
		}
		rows = append(rows, row)
	}
	material["members"] = rows
	if strictLineageFingerprint := stringField(wave.Data, "delivery_strict_import_lineage_fingerprint"); strictLineageFingerprint != "" {
		material["delivery_strict_import_lineage"] = wave.Data["delivery_strict_import_lineage"]
		material["delivery_strict_import_lineage_fingerprint"] = strictLineageFingerprint
	}
	allSpecRefs := append([]string{}, normalizeList(wave.Data["spec_refs"])...)
	for _, id := range members {
		if task, ok := idx.Tasks[id]; ok {
			allSpecRefs = append(allSpecRefs, normalizeList(task.Data["spec_refs"])...)
		}
	}
	for _, ref := range sortedStrings(allSpecRefs) {
		path := deliverySpecRefPath(vaultPath, ref)
		if path == "" || !fileExists(path) {
			issues = append(issues, "spec_ref does not resolve: "+ref)
			continue
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			issues = append(issues, "spec_ref cannot be read: "+ref)
			continue
		}
		sum := sha256.Sum256(raw)
		material["specs"].(map[string]any)[ref] = hex.EncodeToString(sum[:])
	}
	raw, _ := yaml.Marshal(material)
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:]), uniqueStrings(issues)
}

func waveFactoryIntakeContractBlockers(vaultPath string, wave Note) []string {
	planned := factoryIntakeContractProvenance{Schema: stringField(wave.Data, "factory_intake_contract_schema"), Version: stringField(wave.Data, "factory_intake_contract_version"), Fingerprint: stringField(wave.Data, "factory_intake_contract_fingerprint")}
	if planned.Schema == "" && planned.Version == "" && planned.Fingerprint == "" {
		if stringField(wave.Data, "delivery_plan_schema") == deliveryPlanV2Schema || stringField(wave.Data, "context_fingerprint") != "" {
			return []string{"factory-intake contract provenance is missing from a V2-derived wave; remedy: regenerate and re-import the V2 plan from tusker delivery context"}
		}
		// Manual and V1 waves retain compatibility because neither carries the
		// V2 schema nor its authored planning-context fingerprint.
		return nil
	}
	if planned.Schema == "" || planned.Version == "" || planned.Fingerprint == "" {
		return []string{"factory-intake contract provenance is incomplete on this wave; remedy: re-import the V2 plan with schema, version, and fingerprint"}
	}
	want, err := embeddedFactoryIntakeContractProvenance()
	if err != nil {
		return []string{"factory-intake contract cannot be read from this Tusker build; remedy: " + skillSyncRepairAction()}
	}
	if status, _ := factoryContractStatus(planned, want); status != "current" {
		return []string{"factory-intake plan contract is stale or contradictory; remedy: regenerate the V2 plan under the current factory contract"}
	}
	repoRoot := v7RepoRoot(vaultPath)
	// A claimed factory wave is intentionally stricter than generic skill
	// discovery: sync owns both managed agent surfaces, so a healthy sibling or
	// source checkout must not mask a stale/missing Claude or Codex package.
	var blockers []string
	for _, managed := range []struct{ name, path string }{
		{".agents", filepath.Join(repoRoot, ".agents", "skills", "tusker")},
		{".claude", filepath.Join(repoRoot, ".claude", "skills", "tusker")},
	} {
		state := inspectSkillMaterialization(managed.path)
		if state.Status != "current" {
			blockers = append(blockers, "factory-intake managed "+managed.name+" skill is "+state.Status+"; remedy: "+skillSyncRepairAction())
			continue
		}
		resolved := managed.path
		if target, resolveErr := filepath.EvalSymlinks(managed.path); resolveErr == nil {
			resolved = target
		}
		have, metadataErr := readSkillMetadata(resolved)
		if metadataErr != nil || have != planned {
			blockers = append(blockers, "factory-intake managed "+managed.name+" skill contradicts the planned contract; remedy: "+skillSyncRepairAction())
		}
	}
	return blockers
}

func waveMaterialGates(idx v7Index, taskID string) []any {
	var out []any
	for _, gate := range sortedV7Gates(idx) {
		if !containsString(normalizeList(gate.Data["blocks"]), taskID) && !containsString(normalizeList(idx.Tasks[taskID].Data["gates"]), stringField(gate.Data, "id")) {
			continue
		}
		row := map[string]any{"id": stringField(gate.Data, "id"), "status": stringField(gate.Data, "status"), "gate_kind": stringField(gate.Data, "gate_kind"), "owner": stringField(gate.Data, "owner"), "blocking": boolField(gate.Data, "blocking"), "blocks": sortedStrings(normalizeList(gate.Data["blocks"])), "covers": sortedStrings(normalizeList(gate.Data["covers"])), "action": stringField(gate.Data, "action"), "verification": stringField(gate.Data, "verification"), "why_agent_cannot": v7GateBoundaryText(gate), "suggestion": v7GateSuggestionText(gate)}
		kind := strings.ToLower(stringField(gate.Data, "gate_kind"))
		status := stringField(gate.Data, "status")
		if (kind == "auth" || kind == "release") && (status == "satisfied" || status == "waived") {
			current, incomplete := v7GateHardClosureFingerprint(gate.Data, idx)
			row["dependency_material_fingerprint"] = stringField(gate.Data, "dependency_material_fingerprint")
			row["current_dependency_material_fingerprint"] = current
			row["dependency_material_incomplete"] = sortedStrings(incomplete)
		}
		out = append(out, row)
	}
	return out
}

func waveMaterialCrossScopeTargets(task Note, idx v7Index) []any {
	projections, err := deliveryCrossScopeProjections(task)
	if err != nil {
		return []any{map[string]any{"invalid": true}}
	}
	rows := make([]any, 0, len(projections))
	for _, projection := range projections {
		row := map[string]any{
			"scope": projection.Scope, "task": projection.Task, "task_id": projection.TaskID,
			"kind": projection.Kind, "projected_contract_fingerprint": projection.TargetContractFingerprint,
		}
		producer, exists := idx.Tasks[projection.TaskID]
		row["target_exists"] = exists
		if exists {
			row["target_scope"] = stringField(producer.Data, "delivery_plan_scope")
			row["target_source_key"] = stringField(producer.Data, "delivery_source_key")
			row["target_contract_fingerprint"] = stringField(producer.Data, "delivery_contract_fingerprint")
			row["target_current"] = deliveryCrossScopeProducerCurrent(producer, idx)
		}
		rows = append(rows, row)
	}
	return rows
}

func waveMaterialTable(section string, columns []int) []string {
	var out []string
	for _, line := range strings.Split(section, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "|") || strings.Contains(line, "---") {
			continue
		}
		cells := strings.Split(strings.Trim(line, "|"), "|")
		if len(cells) == 0 || strings.EqualFold(strings.TrimSpace(cells[0]), "ID") || strings.EqualFold(strings.TrimSpace(cells[0]), "Covers") {
			continue
		}
		// Completion appends this receipt after independent review. It is a
		// derived lifecycle fact, not an authored verification obligation; hashing
		// it would stale a manual wave after its first successful landing.
		if len(cells) > 3 && strings.HasPrefix(strings.TrimSpace(cells[1]), "typed review ") && strings.Contains(cells[3], "[tusker-review-result:") {
			continue
		}
		var selected []string
		for _, column := range columns {
			if column < len(cells) {
				selected = append(selected, strings.TrimSpace(cells[column]))
			}
		}
		out = append(out, strings.Join(selected, "|"))
	}
	return out
}

func mutateWaveAuthorization(args Args, target string, environment *wavePreflightEnvironment) error {
	var inspector wavePreflightEnvironmentInspector
	if environment != nil {
		fixed := *environment
		inspector = func(string, Note) wavePreflightEnvironment { return fixed }
	}
	_, err := mutateWaveAuthorizationWithInspector(args, target, inspector, nil)
	return err
}

func mutateWaveAuthorizationWithInspector(args Args, target string, inspector wavePreflightEnvironmentInspector, authority *deliveryStartAuthority) (wavePreflightReport, error) {
	vaultPath, wave, _, err := loadWaveAuthorizationTarget(args)
	if err != nil {
		return wavePreflightReport{}, err
	}
	if err := ensureV7ControlMutation(vaultPath, args); err != nil {
		return wavePreflightReport{}, err
	}
	materialLock, err := acquireV7MaterialEpochLock(vaultPath)
	if err != nil {
		return wavePreflightReport{}, err
	}
	lock, err := acquireV7DocumentLock(wave.AbsolutePath, v7DocumentLockTimeout)
	if err != nil {
		return wavePreflightReport{}, combineV7AuthorizationLockCloseError(err, closeV7AuthorizationLocks(materialLock, nil, nil))
	}
	closeLocks := func(taskLocks []*v7DocumentLock) error {
		return closeV7AuthorizationLocks(materialLock, lock, taskLocks)
	}
	finishBeforeMemberLocks := func(report wavePreflightReport, cause error) (wavePreflightReport, error) {
		return report, combineV7AuthorizationLockCloseError(cause, closeLocks(nil))
	}
	idx, err := loadV7Index(vaultPath)
	if err != nil {
		return finishBeforeMemberLocks(wavePreflightReport{}, err)
	}
	wave = idx.Waves[stringField(wave.Data, "id")]
	current := fallback(stringField(wave.Data, "authorization"), "disarmed")
	if target == "paused" && current == "disarmed" {
		return finishBeforeMemberLocks(wavePreflightReport{}, tuskerError(errorInvalidTransition, stringField(wave.Data, "id")+": cannot pause a disarmed wave"))
	}
	if target == "armed" {
		taskLocks, lockErr := lockWaveMemberTasks(idx, wave, authority)
		if lockErr != nil {
			return finishBeforeMemberLocks(wavePreflightReport{}, lockErr)
		}
		finishFailure := func(report wavePreflightReport, cause error) (wavePreflightReport, error) {
			heldLocks := append([]*v7DocumentLock{lock}, taskLocks...)
			cause = rollbackDeliveryStartRefusalUnderLock(vaultPath, idx, authority, cause, heldLocks)
			return report, combineV7AuthorizationLockCloseError(cause, closeLocks(taskLocks))
		}
		finishSuccess := func(report wavePreflightReport, wroteAuthorization bool) (wavePreflightReport, error) {
			if closeErr := closeLocks(taskLocks); closeErr != nil {
				cause := tuskerError(
					errorInvalidTransition,
					"delivery Start authorization locks did not close cleanly; delivery is fail-closed pending repair",
					withHint("stop delivery, inspect the reported authorization locks and transaction paths, then regenerate delivery review"),
					withContext(map[string]any{"lock": closeErr.Error(), "paths": deliveryStartTransactionPaths(authority)}),
				)
				if authority != nil && authority.ImportCommit != nil {
					return report, refuseDeliveryStartBeforeArm(vaultPath, authority, cause)
				}
				return report, handledDeliveryStartRefusal(cause)
			}
			if wroteAuthorization && (authority == nil || !authority.PlanBound) {
				actor := fallback(firstNonEmpty(args.String("by"), args.String("actor")), "human:"+defaultActorName())
				_ = emitV7Event(vaultPath, report.WaveID, "wave", "updated", actor, map[string]any{"authorization": "armed", "fingerprint": report.Fingerprint, "members": report.Members})
			}
			return report, emitWaveAuthorizationResult(args, report.WaveID, report.Authorization, report)
		}
		idx, err = loadV7Index(vaultPath)
		if err != nil {
			return finishFailure(wavePreflightReport{}, err)
		}
		wave = idx.Waves[stringField(wave.Data, "id")]
		current = fallback(stringField(wave.Data, "authorization"), "disarmed")
		var env wavePreflightEnvironment
		if inspector == nil {
			env = inspectWavePreflightEnvironment(vaultPath, wave)
		} else {
			env = inspector(vaultPath, wave)
		}
		report := buildWavePreflight(vaultPath, idx, wave, env)
		if authority != nil && authority.WaveID != report.WaveID {
			cause := tuskerError(errorInvalidTransition, report.WaveID+": reviewed import wave identity changed before authorization; rerun delivery review and Start", withContext(report))
			return finishFailure(report, cause)
		}
		if authority != nil && authority.AuthorizationFingerprint != report.Fingerprint {
			cause := tuskerError(errorInvalidTransition, report.WaveID+": wave material changed after delivery preflight; rerun delivery review and Start", withContext(report))
			return finishFailure(report, cause)
		}
		if authority != nil && strings.Join(authority.Members, "\x00") != strings.Join(sortedStrings(normalizeList(wave.Data["members"])), "\x00") {
			cause := tuskerError(errorInvalidTransition, report.WaveID+": wave membership changed after reviewed import; rerun delivery review and Start", withContext(report))
			return finishFailure(report, cause)
		}
		if err := validateDeliveryStartLiveAuthority(idx, wave, authority); err != nil {
			return finishFailure(report, err)
		}
		if err := validateDeliveryStartAuthorityUnderLock(vaultPath, wave, authority); err != nil {
			return finishFailure(report, err)
		}
		if !report.OK {
			cause := tuskerError(errorInvalidTransition, report.WaveID+": wave arm blocked: "+strings.Join(report.Blockers, "; "), withContext(report))
			return finishFailure(report, cause)
		}
		if current == "armed" && report.StoredFingerprint == report.Fingerprint {
			return finishSuccess(report, false)
		}
		heldLocks := append([]*v7DocumentLock{lock}, taskLocks...)
		if err := armWaveAtomicallyGuarded(vaultPath, idx, wave, report, args, authority, heldLocks); err != nil {
			return finishFailure(report, err)
		}
		final := report
		final.Authorization = "armed"
		final.StoredFingerprint = report.Fingerprint
		final.AuthorizationStale = false
		final.Action = waveAuthorizationAction(final)
		return finishSuccess(final, true)
	}
	if current == target {
		report := buildWavePreflight(vaultPath, idx, wave, wavePreflightEnvironment{})
		report.Authorization = current
		return finishBeforeMemberLocks(report, emitWaveAuthorizationResult(args, report.WaveID, current, report))
	}
	return finishBeforeMemberLocks(wavePreflightReport{}, updateWaveAuthorization(vaultPath, wave, target, "", args, []*v7DocumentLock{lock}))
}

func lockWaveMemberTasks(idx v7Index, wave Note, authority *deliveryStartAuthority) ([]*v7DocumentLock, error) {
	memberIDs := append([]string{}, normalizeList(wave.Data["members"])...)
	if authority != nil {
		memberIDs = append(memberIDs, authority.Members...)
	}
	memberIDs = sortedStrings(memberIDs)
	locks := make([]*v7DocumentLock, 0, len(memberIDs))
	for _, memberID := range memberIDs {
		task, ok := idx.Tasks[memberID]
		if !ok {
			continue
		}
		lock, err := acquireV7DocumentLock(task.AbsolutePath, v7DocumentLockTimeout)
		if err != nil {
			return nil, combineV7AuthorizationLockCloseError(err, closeV7DocumentLocks(locks))
		}
		locks = append(locks, lock)
	}
	return locks, nil
}

func validateDeliveryStartLiveAuthority(idx v7Index, wave Note, authority *deliveryStartAuthority) error {
	if authority == nil {
		return nil
	}
	if fallback(stringField(wave.Data, "authorization"), "disarmed") != authority.WaveAuthorization ||
		stringField(wave.Data, "authorization_fingerprint") != authority.WaveAuthorizedFingerprint ||
		stringField(wave.Data, "authorized_by") != authority.WaveAuthorizedBy ||
		stringField(wave.Data, "authorized_at") != authority.WaveAuthorizedAt {
		return tuskerError(
			errorInvalidTransition,
			authority.WaveID+": authorization changed after reviewed import; preserve the current owner and explicitly review or repair it before retrying Start",
		)
	}
	for _, memberID := range authority.Members {
		task, ok := idx.Tasks[memberID]
		if !ok || deliveryStartMemberBaseline(task) != authority.MemberBaselines[memberID] {
			return tuskerError(
				errorInvalidTransition,
				memberID+": task state/work/source/owner changed after reviewed import; preserve the progressed task and explicitly review or repair it before retrying Start",
			)
		}
		if stringField(task.Data, "readiness") != authority.MemberReadiness[memberID] {
			return tuskerError(
				errorInvalidTransition,
				memberID+": readiness projection changed after reviewed import; restore the exact held baseline before retrying Start",
			)
		}
	}
	return nil
}

type deliveryStartRefusalHandledError struct {
	cause error
}

func (err *deliveryStartRefusalHandledError) Error() string {
	return err.cause.Error()
}

func (err *deliveryStartRefusalHandledError) Unwrap() error {
	return err.cause
}

func deliveryStartRefusalAlreadyHandled(err error) bool {
	var handled *deliveryStartRefusalHandledError
	return errors.As(err, &handled)
}

func handledDeliveryStartRefusal(err error) error {
	if err == nil || deliveryStartRefusalAlreadyHandled(err) {
		return err
	}
	return &deliveryStartRefusalHandledError{cause: err}
}

// rollbackDeliveryStartRefusalUnderLock restores a descriptor-bound Serve
// transaction exactly while the material epoch, wave, and union-member locks
// are held. Standalone CLI Start has no captured import commit, so it retains
// the historical readiness-only merge that preserves concurrent progress.
func rollbackDeliveryStartRefusalUnderLock(vaultPath string, idx v7Index, authority *deliveryStartAuthority, cause error, heldLocks []*v7DocumentLock) error {
	if authority == nil {
		return cause
	}
	if authority.ImportCommit != nil {
		return handledDeliveryStartRefusal(rollbackDeliveryStartTransaction(authority, cause))
	}
	now := time.Now().UTC().Format(time.RFC3339)
	const actor = "tusker:delivery-start-rollback"
	writes := map[string]string{}
	var preserved []string
	for _, memberID := range authority.Members {
		task, ok := idx.Tasks[memberID]
		if !ok {
			preserved = append(preserved, memberID+" (missing)")
			continue
		}
		baseline, exists := authority.MemberBaselines[memberID]
		if !exists ||
			authority.MemberReadiness[memberID] != "held" ||
			stringField(task.Data, "status") != "backlog" ||
			deliveryStartMemberBaseline(task) != baseline {
			preserved = append(preserved, memberID)
			continue
		}
		if stringField(task.Data, "readiness") == "held" {
			continue
		}
		data := cloneMap(task.Data)
		data["readiness"] = "held"
		data["updated_at"], data["updated_by"] = now, actor
		data["state_rev"] = v7StateRev(data, task.Body)
		content, err := serializeDocument(data, task.Body, v7FrontmatterOrder["task"])
		if err != nil {
			return handledDeliveryStartRefusal(fmt.Errorf("%w; delivery Start rollback could not serialize %s: %v", cause, memberID, err))
		}
		writes[task.AbsolutePath] = content
	}
	if len(writes) > 0 {
		if err := commitDeliveryWritesGuardedWithLocks(writes, 0, nil, heldLocks); err != nil {
			return handledDeliveryStartRefusal(fmt.Errorf("%w; delivery Start readiness rollback failed: %v", cause, err))
		}
		_ = emitV7Event(vaultPath, authority.WaveID, "wave", "updated", actor, map[string]any{
			"reason": "delivery Start authority changed before commit", "members_held": authority.Members,
		})
	}
	if len(preserved) > 0 {
		sort.Strings(preserved)
		return handledDeliveryStartRefusal(fmt.Errorf("%w; refusal preserved progressed imported member(s) %s; explicit review or repair is required", cause, strings.Join(preserved, ", ")))
	}
	return handledDeliveryStartRefusal(cause)
}

// refuseDeliveryStartBeforeArm reacquires the material epoch before cleanup.
// Serve can then restore its complete transaction preimages directly. CLI
// callers lock every surviving member and perform the narrower readiness merge.
func refuseDeliveryStartBeforeArm(vaultPath string, authority *deliveryStartAuthority, cause error) error {
	if authority == nil {
		return cause
	}
	materialLock, err := acquireV7MaterialEpochLock(vaultPath)
	if err != nil {
		if authority.ImportCommit != nil {
			rollbackFailure := tuskerError(
				errorInvalidTransition,
				"delivery Start was rejected but its transaction could not be locked for exact rollback; delivery is fail-closed pending repair",
				withHint("stop delivery, restore every reported path from version control or a verified backup, then regenerate delivery review"),
				withContext(map[string]any{"rollback": err.Error(), "paths": deliveryStartTransactionPaths(authority)}),
			)
			return errors.Join(cause, rollbackFailure)
		}
		return fmt.Errorf("%w; refusal could not lock the material epoch for safe readiness cleanup: %v", cause, err)
	}
	if authority.ImportCommit != nil {
		rolledBack := rollbackDeliveryStartTransaction(authority, cause)
		if closeErr := materialLock.Close(); closeErr != nil {
			closeFailure := tuskerError(
				errorInvalidTransition,
				"delivery Start rollback material lock did not close cleanly; delivery is fail-closed pending repair",
				withHint("stop delivery, inspect the material lock and reported transaction paths, then regenerate delivery review"),
				withContext(map[string]any{"lock": closeErr.Error(), "paths": deliveryStartTransactionPaths(authority)}),
			)
			return handledDeliveryStartRefusal(errors.Join(rolledBack, closeFailure))
		}
		return handledDeliveryStartRefusal(rolledBack)
	}
	var waveLock *v7DocumentLock
	var taskLocks []*v7DocumentLock
	closeLocks := func() error {
		return closeV7AuthorizationLocks(materialLock, waveLock, taskLocks)
	}
	finish := func(cause error) error {
		return combineV7AuthorizationLockCloseError(cause, closeLocks())
	}
	idx, err := loadV7Index(vaultPath)
	if err != nil {
		return finish(fmt.Errorf("%w; refusal could not load current state for safe readiness cleanup: %v", cause, err))
	}
	wave := idx.Waves[authority.WaveID]
	if wave.AbsolutePath != "" {
		waveLock, err = acquireV7DocumentLock(wave.AbsolutePath, v7DocumentLockTimeout)
		if err != nil {
			return finish(fmt.Errorf("%w; refusal could not lock current wave for safe readiness cleanup: %v", cause, err))
		}
		idx, err = loadV7Index(vaultPath)
		if err != nil {
			return finish(fmt.Errorf("%w; refusal could not reload current wave for safe readiness cleanup: %v", cause, err))
		}
		wave = idx.Waves[authority.WaveID]
	}
	taskLocks, err = lockWaveMemberTasks(idx, wave, authority)
	if err != nil {
		return finish(fmt.Errorf("%w; refusal could not lock imported members for safe readiness cleanup: %v", cause, err))
	}
	idx, err = loadV7Index(vaultPath)
	if err != nil {
		return finish(fmt.Errorf("%w; refusal could not reload locked members for safe readiness cleanup: %v", cause, err))
	}
	heldLocks := append([]*v7DocumentLock{}, taskLocks...)
	if waveLock != nil {
		heldLocks = append(heldLocks, waveLock)
	}
	return finish(rollbackDeliveryStartRefusalUnderLock(vaultPath, idx, authority, cause, heldLocks))
}

func closeV7DocumentLocks(locks []*v7DocumentLock) error {
	var errs []error
	for index := len(locks) - 1; index >= 0; index-- {
		if err := closeV7AuthorizationLock(locks[index]); err != nil {
			errs = append(errs, fmt.Errorf("member authorization lock %d: %w", index, err))
		}
	}
	return errors.Join(errs...)
}

func closeV7AuthorizationLocks(materialLock, waveLock *v7DocumentLock, taskLocks []*v7DocumentLock) error {
	var errs []error
	if err := closeV7DocumentLocks(taskLocks); err != nil {
		errs = append(errs, err)
	}
	if waveLock != nil {
		if err := closeV7AuthorizationLock(waveLock); err != nil {
			errs = append(errs, fmt.Errorf("wave authorization lock: %w", err))
		}
	}
	if materialLock != nil {
		if err := closeV7AuthorizationLock(materialLock); err != nil {
			errs = append(errs, fmt.Errorf("material authorization lock: %w", err))
		}
	}
	return errors.Join(errs...)
}

func combineV7AuthorizationLockCloseError(cause, closeErr error) error {
	if closeErr == nil {
		return cause
	}
	if cause == nil {
		return fmt.Errorf("authorization lock release failed: %w", closeErr)
	}
	return errors.Join(cause, fmt.Errorf("authorization lock release also failed: %w", closeErr))
}

func armWaveAtomically(vaultPath string, idx v7Index, wave Note, report wavePreflightReport, args Args) error {
	return armWaveAtomicallyGuarded(vaultPath, idx, wave, report, args, nil, nil)
}

func armWaveAtomicallyGuarded(vaultPath string, idx v7Index, wave Note, report wavePreflightReport, args Args, authority *deliveryStartAuthority, heldLocks []*v7DocumentLock) error {
	now := time.Now().UTC().Format(time.RFC3339)
	actor := fallback(firstNonEmpty(args.String("by"), args.String("actor")), "human:"+defaultActorName())
	writes := map[string]string{}
	for _, id := range report.Members {
		task := idx.Tasks[id]
		if stringField(task.Data, "status") != "backlog" || stringField(task.Data, "readiness") != "held" {
			continue
		}
		data := cloneMap(task.Data)
		data["status"] = "ready"
		projectedTask := task
		projectedTask.Data = data
		projectedIdx := idx
		projectedIdx.Tasks = cloneNoteMap(idx.Tasks)
		projectedIdx.Tasks[id] = projectedTask
		projection := v7ProjectedTaskState(vaultPath, projectedTask, projectedIdx)
		for key, value := range projection {
			if value == "" {
				delete(data, key)
			} else {
				data[key] = value
			}
		}
		data["updated_at"], data["updated_by"] = now, actor
		data["state_rev"] = v7StateRev(data, task.Body)
		content, err := serializeDocument(data, task.Body, v7FrontmatterOrder["task"])
		if err != nil {
			return err
		}
		writes[task.AbsolutePath] = content
	}
	data := cloneMap(wave.Data)
	data["authorization"], data["authorization_fingerprint"] = "armed", report.Fingerprint
	data["authorized_by"], data["authorized_at"] = actor, now
	data["authorization_updated_by"], data["authorization_updated_at"] = actor, now
	delete(data, "authorization_reason")
	data["updated_at"], data["updated_by"] = now, actor
	data["state_rev"] = v7StateRev(data, wave.Body)
	content, err := serializeDocument(data, wave.Body, v7FrontmatterOrder["wave"])
	if err != nil {
		return err
	}
	writes[wave.AbsolutePath] = content
	failAfter := 0
	if args.Bool("fail-after-first-write") {
		failAfter = 1
	}
	var guard *deliveryImportWriteGuard
	if authority != nil && authority.PlanBound {
		guard = &deliveryImportWriteGuard{
			Verify:                  authority.PlanVerify,
			DelayMutationVisibility: true,
		}
	}
	if err := commitDeliveryWritesGuardedWithLocks(writes, failAfter, guard, heldLocks); err != nil {
		if isDeliveryImportIdentityChanged(err) {
			return deliveryStartPlanAuthorityChanged(err)
		}
		return err
	}
	if guard != nil {
		authority.ArmCommit = guard.Commit
		if deliveryStartAfterArmCommit != nil {
			if err := deliveryStartAfterArmCommit(); err != nil {
				return err
			}
		}
	}
	return nil
}

func updateWaveAuthorization(vaultPath string, wave Note, target, fingerprint string, args Args, heldLocks []*v7DocumentLock) error {
	now := time.Now().UTC().Format(time.RFC3339)
	actor := fallback(firstNonEmpty(args.String("by"), args.String("actor")), "human:"+defaultActorName())
	reason := strings.TrimSpace(args.String("reason"))
	data := cloneMap(wave.Data)
	data["authorization"], data["authorization_updated_by"], data["authorization_updated_at"] = target, actor, now
	if target == "disarmed" {
		delete(data, "authorization_fingerprint")
	} else if fingerprint != "" {
		data["authorization_fingerprint"] = fingerprint
	}
	if reason != "" {
		data["authorization_reason"] = reason
	} else {
		delete(data, "authorization_reason")
	}
	data["updated_at"], data["updated_by"] = now, actor
	data["state_rev"] = v7StateRev(data, wave.Body)
	content, err := serializeDocument(data, wave.Body, v7FrontmatterOrder["wave"])
	if err != nil {
		return err
	}
	if err := commitDeliveryWritesGuardedWithLocks(map[string]string{wave.AbsolutePath: content}, 0, nil, heldLocks); err != nil {
		return err
	}
	_ = emitV7Event(vaultPath, stringField(data, "id"), "wave", "updated", actor, map[string]any{"authorization": target, "reason": reason})
	idx, _ := loadV7Index(vaultPath)
	next := idx.Waves[stringField(data, "id")]
	report := buildWavePreflight(vaultPath, idx, next, wavePreflightEnvironment{})
	return emitWaveAuthorizationResult(args, stringField(data, "id"), target, report)
}

func emitWaveAuthorizationResult(args Args, id, state string, report wavePreflightReport) error {
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": true, "waveId": id, "authorization": state, "preflight": report})
		return nil
	}
	if !args.Bool("quiet") {
		fmt.Printf("%s authorization: %s\n", id, state)
	}
	return nil
}

func waveAuthorizationProjection(vaultPath string, idx v7Index, wave Note) map[string]any {
	fingerprint, _ := waveMaterialFingerprint(vaultPath, idx, wave)
	stored, state := stringField(wave.Data, "authorization_fingerprint"), fallback(stringField(wave.Data, "authorization"), "disarmed")
	stale := stored != "" && stored != fingerprint
	if stale {
		state = "stale"
	}
	return map[string]any{"state": state, "stale": stale, "fingerprint": fingerprint, "authorizedFingerprint": nullIfBlank(stored), "actor": nullIfBlank(stringField(wave.Data, "authorized_by")), "at": nullIfBlank(stringField(wave.Data, "authorized_at")), "action": waveAuthorizationProjectionAction(state)}
}

func waveAuthorizationProjectionAction(state string) string {
	switch state {
	case "armed":
		return "none"
	case "paused":
		return "tusker wave resume <wave-id>"
	case "stale":
		return "tusker wave preflight <wave-id> && tusker wave arm <wave-id> --by <actor>"
	default:
		return "tusker wave preflight <wave-id> && tusker wave arm <wave-id> --by <actor>"
	}
}

func waveAuthorizationAction(report wavePreflightReport) string {
	if !report.OK {
		return "fix preflight blockers, then rerun tusker wave preflight " + report.WaveID
	}
	return waveAuthorizationProjectionAction(report.Authorization)
}

func waveEnvironmentBlocker(key string) string {
	return map[string]string{"project": "project must be registered, automation-enabled, and healthy", "daemon": "managed daemon must be alive and reconciling", "runner": "runner/profile must resolve to a supported unattended harness", "skill": "installed Tusker operator skill does not support wave authorization", "workflow": "workflow and tracker schema versions are incompatible with wave authorization", "approvalPolicy": "unattended runner approval policy must not pause for routine approvals", "workspaceIsolation": "multi-task wave requires isolated workspaces and a clean integration branch/worktree/base"}[key]
}
func hasWaveBlocker(blockers []string, needles ...string) bool {
	for _, b := range blockers {
		for _, n := range needles {
			if strings.Contains(strings.ToLower(b), strings.ToLower(n)) {
				return true
			}
		}
	}
	return false
}
func waveDependencyID(raw string) string {
	raw = strings.TrimSpace(wikiTarget(raw))
	if i := strings.LastIndex(raw, ":"); i > 0 {
		raw = raw[:i]
	}
	return strings.ToUpper(strings.TrimSpace(raw))
}
func sortedStrings(values []string) []string {
	values = uniqueStrings(values)
	sort.Strings(values)
	return values
}
func cloneMap(src map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range src {
		out[k] = v
	}
	return out
}
func cloneNoteMap(src map[string]Note) map[string]Note {
	out := map[string]Note{}
	for k, v := range src {
		out[k] = v
	}
	return out
}
func appendAny(current any, value any) []any {
	if values, ok := current.([]any); ok {
		return append(values, value)
	}
	return []any{value}
}
func waveDependencyFrontiers(members []string, deps map[string][]string) ([][]string, bool) {
	remaining := map[string]int{}
	next := map[string][]string{}
	for _, id := range members {
		remaining[id] = len(uniqueStrings(deps[id]))
		for _, dep := range uniqueStrings(deps[id]) {
			next[dep] = append(next[dep], id)
		}
	}
	var out [][]string
	seen := 0
	for len(remaining) > 0 {
		var frontier []string
		for id, n := range remaining {
			if n == 0 {
				frontier = append(frontier, id)
			}
		}
		sort.Strings(frontier)
		if len(frontier) == 0 {
			return nil, false
		}
		out = append(out, frontier)
		for _, id := range frontier {
			delete(remaining, id)
			seen++
			for _, child := range next[id] {
				remaining[child]--
			}
		}
	}
	return out, seen == len(members)
}

func waveAffectedClosure(seed []string, deps map[string][]string) []string {
	affected := makeSet(seed...)
	for changed := true; changed; {
		changed = false
		for taskID, taskDeps := range deps {
			if _, exists := affected[taskID]; exists {
				continue
			}
			for _, dependency := range taskDeps {
				if _, blocked := affected[dependency]; blocked {
					affected[taskID] = struct{}{}
					changed = true
					break
				}
			}
		}
	}
	out := make([]string, 0, len(affected))
	for id := range affected {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

func renderWavePreflight(report wavePreflightReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s wave preflight: %t (read-only)\n", report.WaveID, report.OK)
	fmt.Fprintf(&b, "  authorization=%s stale=%t action=%s\n", report.Authorization, report.AuthorizationStale, report.Action)
	fmt.Fprintf(&b, "  fingerprint=%s members=%s concurrency=%d\n", report.Fingerprint, strings.Join(report.Members, ","), report.ExpectedConcurrency)
	fmt.Fprintf(&b, "  integration=%s validation=%s landing=%s\n", report.IntegrationBranch, report.ValidationLane, report.LandingPolicy)
	fmt.Fprintf(&b, "  dispatch_scope configured=%s effective=%s provenance=%s\n", fallback(report.DispatchScope.Configured, "-"), report.DispatchScope.Effective, report.DispatchScope.Provenance)
	if report.DispatchScope.Warning != "" {
		fmt.Fprintf(&b, "  scope_warning=%s; repair=%s\n", report.DispatchScope.Warning, report.DispatchScope.Repair)
	}
	for i, frontier := range report.Frontiers {
		fmt.Fprintf(&b, "  frontier %d: %s\n", i+1, strings.Join(frontier, ","))
	}
	if rendered := renderDeliveryCrossScopeReview(report.CrossScopeReview.Dependencies); rendered != "" {
		for _, line := range strings.Split(strings.TrimSuffix(rendered, "\n"), "\n") {
			b.WriteString("  " + line + "\n")
		}
	}
	for _, gate := range report.HumanGates {
		fmt.Fprintf(&b, "  human gate: %s affected=%s action=%s\n", stringField(gate, "id"), strings.Join(normalizeList(gate["affected"]), ","), stringField(gate, "action"))
	}
	if len(report.Blockers) == 0 {
		b.WriteString("  blockers=none\n")
	} else {
		for _, blocker := range report.Blockers {
			b.WriteString("  blocker: " + blocker + "\n")
		}
	}
	return b.String()
}
