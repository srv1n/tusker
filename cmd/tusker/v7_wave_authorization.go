package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const waveAuthorizationSchema = "tusker.wave-authorization/v1"

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

func applyWaveWorkflowEnvironment(env *wavePreflightEnvironment, wave Note, wf Workflow) {
	env.WorkflowCompatible = wf.WorkflowVersion == 1 && wf.TrackerSchemaVersion == 7
	profileName := stringField(wave.Data, "runner_profile")
	if profileName != "" {
		profile, ok := wf.RunnerProfiles[profileName]
		env.RunnerCompatible = ok && profile.Harness != ""
		env.ApprovalFree = ok && profile.PermissionPreset == "danger-full-access"
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
		if readErr == nil && stringField(metadata, "wave_authorization_schema") == waveAuthorizationSchema && intField(metadata, "workflow_version") == 1 && intField(metadata, "tracker_schema_version") == 7 {
			return true
		}
	}
	return false
}

func waveIntegrationBaseClean(vaultPath string, wave Note) bool {
	repoRoot := v7RepoRoot(vaultPath)
	branch := firstNonEmpty(stringField(wave.Data, "integration_branch"), v7IntegrationBranchName(stringField(wave.Data, "id")))
	if !v7GitRepo(repoRoot) || !gitRefExists(repoRoot, "refs/heads/"+branch) {
		return false
	}
	defaultBranch := v7DefaultBranch(vaultPath)
	integrationRev, integrationErr := gitCombined(repoRoot, "rev-parse", "refs/heads/"+branch)
	baseRev, baseErr := gitCombined(repoRoot, "rev-parse", "refs/heads/"+defaultBranch)
	if integrationErr != nil || baseErr != nil {
		return false
	}
	if stringField(wave.Data, "authorized_at") == "" {
		if strings.TrimSpace(integrationRev) != strings.TrimSpace(baseRev) {
			return false
		}
	} else if _, err := gitCombined(repoRoot, "merge-base", "--is-ancestor", strings.TrimSpace(baseRev), strings.TrimSpace(integrationRev)); err != nil {
		return false
	}
	branchRef := "refs/heads/" + branch
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
		ValidationLane: "serialized integration validation", IntegrationBranch: stringField(wave.Data, "integration_branch"),
		LandingPolicy: "task branches -> wave integration branch -> configured default branch",
		DispatchScope: dispatchScope,
		Checks:        map[string]bool{"specDag": true, "taskContracts": true, "artifacts": true, "project": env.ProjectRegistered && env.ProjectEnabled && env.ProjectHealthy, "daemon": env.DaemonAlive && env.DaemonReconciling, "runner": env.RunnerCompatible, "skill": env.SkillCompatible, "workflow": env.WorkflowCompatible, "approvalPolicy": env.ApprovalFree, "workspaceIsolation": env.IsolatedWorkspace && env.IntegrationClean},
	}
	report.Blockers = append(report.Blockers, fpIssues...)
	graph := map[string][]string{}
	memberSet := makeSet(members...)
	for _, id := range members {
		task, ok := idx.Tasks[id]
		if !ok {
			report.Blockers = append(report.Blockers, "member task does not resolve: "+id)
			continue
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
				report.ExternalDependencies[id] = appendAny(report.ExternalDependencies[id], map[string]any{"id": depID, "hardness": edge.Hardness, "status": stringField(depNote.Data, "status"), "proof": stringField(depNote.Data, "proof_status"), "satisfied": satisfied})
				if !satisfied {
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
		if stringField(gate.Data, "status") != "open" || v7ProofOwnerClass(stringField(gate.Data, "owner")) != "human" {
			continue
		}
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
	material := map[string]any{"schema": waveAuthorizationSchema, "wave": stringField(wave.Data, "id"), "members": []any{}, "specs": map[string]any{}}
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
			"owned_paths": sortedStrings(normalizeList(task.Data["owned_paths"])), "runner_profile": stringField(task.Data, "runner_profile"),
			"proof_contract": map[string]any{"mode": stringField(task.Data, "proof_mode"), "required": sortedStrings(normalizeList(task.Data["proof_required"])), "required_owner": task.Data["proof_required_owner"], "evidence_budget": intField(task.Data, "evidence_budget"), "evidence_required": sortedStrings(normalizeList(task.Data["evidence_required"]))},
			"gates":          waveMaterialGates(idx, id),
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

func waveMaterialGates(idx v7Index, taskID string) []any {
	var out []any
	for _, gate := range sortedV7Gates(idx) {
		if !containsString(normalizeList(gate.Data["blocks"]), taskID) && !containsString(normalizeList(idx.Tasks[taskID].Data["gates"]), stringField(gate.Data, "id")) {
			continue
		}
		out = append(out, map[string]any{"id": stringField(gate.Data, "id"), "status": stringField(gate.Data, "status"), "gate_kind": stringField(gate.Data, "gate_kind"), "owner": stringField(gate.Data, "owner"), "blocking": boolField(gate.Data, "blocking"), "blocks": sortedStrings(normalizeList(gate.Data["blocks"])), "covers": sortedStrings(normalizeList(gate.Data["covers"])), "action": stringField(gate.Data, "action"), "verification": stringField(gate.Data, "verification"), "why_agent_cannot": v7GateBoundaryText(gate), "suggestion": v7GateSuggestionText(gate)})
	}
	return out
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
	vaultPath, wave, _, err := loadWaveAuthorizationTarget(args)
	if err != nil {
		return err
	}
	if err := ensureV7ControlMutation(vaultPath, args); err != nil {
		return err
	}
	lock, err := acquireV7DocumentLock(wave.AbsolutePath, v7DocumentLockTimeout)
	if err != nil {
		return err
	}
	defer lock.Close()
	idx, err := loadV7Index(vaultPath)
	if err != nil {
		return err
	}
	wave = idx.Waves[stringField(wave.Data, "id")]
	current := fallback(stringField(wave.Data, "authorization"), "disarmed")
	if target == "paused" && current == "disarmed" {
		return tuskerError(errorInvalidTransition, stringField(wave.Data, "id")+": cannot pause a disarmed wave")
	}
	if target == "armed" {
		taskLocks, lockErr := lockWaveMemberTasks(idx, wave)
		if lockErr != nil {
			return lockErr
		}
		defer closeV7DocumentLocks(taskLocks)
		idx, err = loadV7Index(vaultPath)
		if err != nil {
			return err
		}
		wave = idx.Waves[stringField(wave.Data, "id")]
		env := inspectWavePreflightEnvironment(vaultPath, wave)
		if environment != nil {
			env = *environment
		}
		report := buildWavePreflight(vaultPath, idx, wave, env)
		if !report.OK {
			return tuskerError(errorInvalidTransition, report.WaveID+": wave arm blocked: "+strings.Join(report.Blockers, "; "), withContext(report))
		}
		if current == "armed" && report.StoredFingerprint == report.Fingerprint {
			return emitWaveAuthorizationResult(args, report.WaveID, current, report)
		}
		return armWaveAtomically(vaultPath, idx, wave, report, args)
	}
	if current == target {
		report := buildWavePreflight(vaultPath, idx, wave, wavePreflightEnvironment{})
		return emitWaveAuthorizationResult(args, report.WaveID, current, report)
	}
	return updateWaveAuthorization(vaultPath, wave, target, "", args)
}

func lockWaveMemberTasks(idx v7Index, wave Note) ([]*v7DocumentLock, error) {
	memberIDs := sortedStrings(normalizeList(wave.Data["members"]))
	locks := make([]*v7DocumentLock, 0, len(memberIDs))
	for _, memberID := range memberIDs {
		task, ok := idx.Tasks[memberID]
		if !ok {
			continue
		}
		lock, err := acquireV7DocumentLock(task.AbsolutePath, v7DocumentLockTimeout)
		if err != nil {
			closeV7DocumentLocks(locks)
			return nil, err
		}
		locks = append(locks, lock)
	}
	return locks, nil
}

func closeV7DocumentLocks(locks []*v7DocumentLock) {
	for index := len(locks) - 1; index >= 0; index-- {
		_ = locks[index].Close()
	}
}

func armWaveAtomically(vaultPath string, idx v7Index, wave Note, report wavePreflightReport, args Args) error {
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
	if err := commitDeliveryWrites(writes, failAfter); err != nil {
		return err
	}
	_ = emitV7Event(vaultPath, report.WaveID, "wave", "updated", actor, map[string]any{"authorization": "armed", "fingerprint": report.Fingerprint, "members": report.Members})
	return emitWaveAuthorizationResult(args, report.WaveID, "armed", report)
}

func updateWaveAuthorization(vaultPath string, wave Note, target, fingerprint string, args Args) error {
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
	if err := commitDeliveryWrites(map[string]string{wave.AbsolutePath: content}, 0); err != nil {
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
