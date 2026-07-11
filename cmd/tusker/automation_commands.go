package main

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	automationExplainSchema = "tusker.automation.explain/v1"
	automationPlanSchema    = "tusker.automation.plan/v1"
	automationQueueSchema   = "tusker.automation.queue/v1"
	automationStatusSchema  = "tusker.automation.status/v1"
)

type automationCommandContext struct {
	StateRoot          string
	Store              *RuntimeStore
	Project            RegisteredProject
	ProjectRegistered  bool
	Workflow           WorkflowFile
	Notes              []Note
	NotesByID          map[string]Note
	NotesByRecordID    map[string]Note
	Runs               []RunStatus
	ProjectRuns        map[string]RunStatus
	NoteStatusByRecord map[string]string
	GlobalActiveRuns   int
	ProjectActiveRuns  int
	StateActiveRuns    map[string]int
	DispatchRefusal    string
}

type automationProjectSummary struct {
	ProjectID    string        `json:"project_id"`
	ProjectKey   string        `json:"project_key"`
	Name         string        `json:"name"`
	RepoRoot     string        `json:"repo_root"`
	VaultRoot    string        `json:"vault_root"`
	WorkflowPath string        `json:"workflow_path"`
	Registered   bool          `json:"registered"`
	Enabled      bool          `json:"enabled"`
	Health       ProjectHealth `json:"health"`
	LastPollAt   string        `json:"last_poll_at"`
	LastError    string        `json:"last_error"`
	ActiveRuns   int           `json:"active_runs"`
	QueuedRuns   int           `json:"queued_runs"`
}

type automationTaskExplanation struct {
	Schema            string                   `json:"schema"`
	ID                string                   `json:"id"`
	RecordID          string                   `json:"record_id"`
	Title             string                   `json:"title"`
	Path              string                   `json:"path"`
	Status            string                   `json:"status"`
	Readiness         string                   `json:"readiness"`
	NextOwner         string                   `json:"next_owner"`
	ProofMode         string                   `json:"proof_mode"`
	ProofStatus       string                   `json:"proof_status"`
	Risk              string                   `json:"risk"`
	Priority          string                   `json:"priority"`
	Dispatchable      bool                     `json:"dispatchable"`
	Blockers          []string                 `json:"blockers"`
	Project           automationProjectSummary `json:"project"`
	Runner            string                   `json:"runner"`
	Lane              string                   `json:"lane"`
	Command           string                   `json:"command"`
	WorkflowPath      string                   `json:"workflow_path"`
	WorkspacePath     string                   `json:"workspace_path"`
	WorkspaceStrategy string                   `json:"workspace_strategy"`
	Branch            string                   `json:"branch"`
	ApprovalPolicy    string                   `json:"approval_policy"`
	ThreadSandbox     string                   `json:"thread_sandbox"`
	TurnSandboxPolicy string                   `json:"turn_sandbox_policy"`
	RequiredApprovals []string                 `json:"required_approvals"`
	ApplyInputs       []RuntimeApplyInput      `json:"apply_inputs,omitempty"`
	Fanout            automationFanoutSummary  `json:"fanout"`
	ExistingRun       *RunStatus               `json:"existing_run,omitempty"`
}

type automationFanoutSummary struct {
	Enabled           bool     `json:"enabled"`
	MaxChildren       int      `json:"max_children"`
	ActiveChildren    int      `json:"active_children"`
	AllowedChildTypes []string `json:"allowed_child_types"`
	MergeRule         string   `json:"merge_rule"`
	Blockers          []string `json:"blockers,omitempty"`
}

type automationDispatchPlan struct {
	Schema            string                   `json:"schema"`
	Task              string                   `json:"task"`
	RecordID          string                   `json:"record_id"`
	Decision          string                   `json:"decision"`
	Lane              string                   `json:"lane"`
	Runner            string                   `json:"runner"`
	Command           string                   `json:"command"`
	WorkspacePath     string                   `json:"workspace_path"`
	WorkspaceStrategy string                   `json:"workspace_strategy"`
	Branch            string                   `json:"branch"`
	Blockers          []string                 `json:"blockers"`
	RequiredReads     []string                 `json:"required_reads"`
	ProofRequired     []string                 `json:"proof_required"`
	RequiredApprovals []string                 `json:"required_approvals"`
	Fanout            automationFanoutSummary  `json:"fanout"`
	Project           automationProjectSummary `json:"project"`
}

type automationQueueReport struct {
	Schema   string                      `json:"schema"`
	Project  automationProjectSummary    `json:"project"`
	Eligible []automationTaskExplanation `json:"eligible"`
	Blocked  []automationTaskExplanation `json:"blocked"`
	Count    int                         `json:"count"`
}

type automationStatusReport struct {
	Schema        string                     `json:"schema"`
	StateRoot     string                     `json:"state_root"`
	ProjectCount  int                        `json:"project_count"`
	ActiveRuns    int                        `json:"active_runs"`
	MaxActiveRuns int                        `json:"max_active_runs"`
	LimitSource   string                     `json:"limit_source"`
	Projects      []automationProjectSummary `json:"projects"`
}

func automationStatusCmd(args Args) error {
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		return err
	}
	defer store.Close()
	status, err := store.DaemonStatus()
	if err != nil {
		return err
	}
	projects, err := store.ListProjects()
	if err != nil {
		return err
	}
	runs, err := store.ListRuns()
	if err != nil {
		return err
	}
	report := automationStatusReport{
		Schema:        automationStatusSchema,
		StateRoot:     stringValue(status["state_root"]),
		ProjectCount:  intFromAny(status["projects"]),
		ActiveRuns:    intFromAny(status["activeRuns"]),
		MaxActiveRuns: intFromAny(status["max_active_runs"]),
		LimitSource:   stringValue(status["limit_source"]),
		Projects:      automationProjectSummaries(projects, runs),
	}
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": true, "status": report})
		return nil
	}
	printAutomationStatus(report)
	return nil
}

func automationQueueCmd(args Args) error {
	ctx, err := loadAutomationCommandContext(args)
	if err != nil {
		return err
	}
	defer ctx.Close()
	report := ctx.automationQueueReport()
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": true, "queue": report})
		return nil
	}
	printAutomationQueue(report)
	return nil
}

func automationExplainCmd(args Args) error {
	taskID, err := requireArg(args, "id")
	if err != nil {
		return err
	}
	ctx, err := loadAutomationCommandContext(args)
	if err != nil {
		return err
	}
	defer ctx.Close()
	note, err := ctx.findTask(taskID)
	if err != nil {
		return err
	}
	explanation := ctx.explainTask(note)
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": true, "explanation": explanation})
		return nil
	}
	printAutomationExplanation(explanation)
	return nil
}

func automationPlanCmd(args Args) error {
	taskID, err := requireArg(args, "id")
	if err != nil {
		return err
	}
	ctx, err := loadAutomationCommandContext(args)
	if err != nil {
		return err
	}
	defer ctx.Close()
	note, err := ctx.findTask(taskID)
	if err != nil {
		return err
	}
	explanation := ctx.explainTask(note)
	plan := automationPlanFromExplanation(ctx.Project.VaultRoot, note, explanation)
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": len(plan.Blockers) == 0, "plan": plan})
		return nil
	}
	printAutomationPlan(plan)
	return nil
}

func automationDispatchCmd(args Args) error {
	taskID, err := requireArg(args, "id")
	if err != nil {
		return err
	}
	ctx, err := loadAutomationCommandContext(args)
	if err != nil {
		return err
	}
	defer ctx.Close()
	note, err := ctx.findTask(taskID)
	if err != nil {
		return err
	}
	explanation := ctx.explainTask(note)
	if !explanation.Dispatchable {
		if args.Bool("json") {
			emitJSON(map[string]any{"ok": false, "explanation": explanation})
			return nil
		}
		printAutomationExplanation(explanation)
		return tuskerError(errorInvalidTransition, taskID+": automation dispatch blocked: "+strings.Join(explanation.Blockers, "; "), withContext(explanation))
	}
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": false, "explanation": explanation, "error": oneShotDispatchRefusal("tusker automation dispatch")})
		return nil
	}
	return tuskerError(errorInvalidTransition, oneShotDispatchRefusal("tusker automation dispatch"), withContext(explanation))
}

func loadAutomationCommandContext(args Args) (*automationCommandContext, error) {
	stateRoot := DefaultStateRoot()
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		return nil, err
	}
	ctx := &automationCommandContext{
		StateRoot:          stateRoot,
		Store:              store,
		NotesByID:          map[string]Note{},
		NotesByRecordID:    map[string]Note{},
		ProjectRuns:        map[string]RunStatus{},
		NoteStatusByRecord: map[string]string{},
		StateActiveRuns:    map[string]int{},
	}
	project, registered, projectErr := resolveAutomationRegisteredProject(store, args)
	if projectErr != nil && strings.TrimSpace(firstNonEmpty(args.String("project"), args.String("project-id"))) != "" {
		_ = store.Close()
		return nil, projectErr
	}
	if registered {
		ctx.Project = *project
		ctx.ProjectRegistered = true
	} else {
		vaultPath, err := resolveAutomationVaultPath(args)
		if err != nil {
			_ = store.Close()
			return nil, err
		}
		repoRoot := strings.TrimSpace(args.String("repo"))
		if repoRoot == "" {
			repoRoot = v7RepoRoot(vaultPath)
		}
		repoRoot, _ = filepath.Abs(repoRoot)
		ctx.Project = RegisteredProject{
			ProjectKey:   projectKeyFromPath(repoRoot),
			Name:         filepath.Base(repoRoot),
			RepoRoot:     repoRoot,
			VaultRoot:    vaultPath,
			WorkflowPath: workflowPath(vaultPath),
			Enabled:      false,
			Health:       projectHealthDisabled,
		}
	}
	ctx.Workflow, err = loadWorkflow(ctx.Project.VaultRoot)
	if err != nil {
		_ = store.Close()
		return nil, err
	}
	ctx.Notes, err = listAllNotes(ctx.Project.VaultRoot)
	if err != nil {
		_ = store.Close()
		return nil, err
	}
	lookup := buildNoteLookup(ctx.Notes)
	ctx.NotesByID = lookup.ByID
	ctx.NotesByRecordID = lookup.ByRecordID
	for _, note := range ctx.Notes {
		if daemonNoteKind(note) != "task" {
			continue
		}
		recordID := trackerRecordID(note)
		if recordID != "" {
			ctx.NoteStatusByRecord[recordID] = stringField(note.Data, "status")
		}
	}
	ctx.Runs, err = store.ListRuns()
	if err != nil {
		_ = store.Close()
		return nil, err
	}
	ctx.GlobalActiveRuns = countDispatchingRuns(ctx.Runs)
	for _, run := range ctx.Runs {
		if ctx.ProjectRegistered && run.ProjectID != ctx.Project.ProjectID {
			continue
		}
		if run.RecordID != "" {
			ctx.ProjectRuns[run.RecordID] = run
		}
	}
	ctx.ProjectActiveRuns = countDispatchingProjectRuns(ctx.ProjectRuns)
	ctx.StateActiveRuns = countDispatchingProjectRunsByState(ctx.ProjectRuns, ctx.NoteStatusByRecord)
	return ctx, nil
}

func (ctx *automationCommandContext) Close() {
	if ctx != nil && ctx.Store != nil {
		_ = ctx.Store.Close()
	}
}

func resolveAutomationRegisteredProject(store *RuntimeStore, args Args) (*RegisteredProject, bool, error) {
	projectArgs := Args{}
	if projectID := firstNonEmpty(args.String("project"), args.String("project-id")); projectID != "" {
		projectArgs["id"] = projectID
	}
	if repo := strings.TrimSpace(args.String("repo")); repo != "" {
		projectArgs["repo"] = repo
	}
	if vault := strings.TrimSpace(args.String("vault")); vault != "" {
		projectArgs["vault"] = vault
	}
	project, err := resolveRegisteredProject(store, projectArgs)
	if err != nil {
		return nil, false, err
	}
	return project, true, nil
}

func resolveAutomationVaultPath(args Args) (string, error) {
	if repo := strings.TrimSpace(args.String("repo")); repo != "" && strings.TrimSpace(args.String("vault")) == "" {
		if discovered, err := discoverVault(repo); err != nil {
			return "", err
		} else if discovered != "" {
			return discovered, nil
		}
	}
	return resolveVaultPath(args, false)
}

func (ctx *automationCommandContext) automationQueueReport() automationQueueReport {
	notes := append([]Note{}, ctx.Notes...)
	sortDispatchCandidates(notes)
	report := automationQueueReport{
		Schema:  automationQueueSchema,
		Project: automationSummarizeProject(ctx.Project, ctx.projectRunsSlice(), ctx.ProjectRegistered),
	}
	for _, note := range notes {
		if !automationQueueIncludes(note) {
			continue
		}
		explanation := ctx.explainTask(note)
		if explanation.Dispatchable {
			report.Eligible = append(report.Eligible, explanation)
		} else {
			report.Blocked = append(report.Blocked, explanation)
		}
	}
	report.Count = len(report.Eligible) + len(report.Blocked)
	return report
}

func automationQueueIncludes(note Note) bool {
	if daemonNoteKind(note) != "task" {
		return false
	}
	switch stringField(note.Data, "status") {
	case "done", "cancelled", "superseded":
		return false
	default:
		return true
	}
}

func (ctx *automationCommandContext) findTask(id string) (Note, error) {
	target := wikiTarget(id)
	if note, ok := ctx.NotesByID[target]; ok && daemonNoteKind(note) == "task" {
		return note, nil
	}
	if note, ok := ctx.NotesByRecordID[target]; ok && daemonNoteKind(note) == "task" {
		return note, nil
	}
	return Note{}, tuskerError(errorNotFound, "Task not found: "+target, withContext(map[string]any{"id": target}))
}

func (ctx *automationCommandContext) explainTask(note Note) automationTaskExplanation {
	return ctx.explainTaskForRunner(note, automationResolveRunner(note, ctx.Workflow.Data), nil)
}

func (ctx *automationCommandContext) explainTaskForRunner(note Note, runner string, runOverride *RunStatus) automationTaskExplanation {
	recordID := trackerRecordID(note)
	runner = firstNonEmpty(strings.TrimSpace(runner), automationResolveRunner(note, ctx.Workflow.Data))
	run := ctx.effectiveRunForTask(note, runner)
	if runOverride != nil {
		run = *runOverride
		run.ProjectID = firstNonEmpty(run.ProjectID, ctx.Project.ProjectID)
		run.RecordID = firstNonEmpty(run.RecordID, recordID)
		run.ItemID = firstNonEmpty(run.ItemID, stringField(note.Data, "id"))
		run.Runner = firstNonEmpty(strings.TrimSpace(runner), run.Runner, ctx.Workflow.Data.Agents.Default)
		run.Lane = firstNonEmpty(run.Lane, runLaneExecute)
	}
	command := ""
	requiredApprovals := []string{}
	var blockers []string
	if !ctx.ProjectRegistered {
		blockers = append(blockers, "project is not registered for automation")
	} else if !ctx.Project.Enabled {
		blockers = append(blockers, "project is disabled")
	}
	if isV7TaskNote(note) {
		blockers = append(blockers, v7TaskDispatchBlockers(ctx.Project.VaultRoot, note)...)
	} else {
		blockers = append(blockers, automationLegacyTaskBlockers(note, ctx.Workflow.Data)...)
	}
	if reason := dispatchEligibilityReason(note, ctx.NotesByID, ctx.NotesByRecordID); reason != "" {
		blockers = append(blockers, reason)
	}
	runnerObj, configuredCommand, err := runnerForName(runner, ctx.Workflow.Data)
	if err != nil {
		blockers = append(blockers, "runner config: "+err.Error())
	} else {
		command = configuredCommand
		if runnerObj.Capabilities().ExplicitApprovals {
			requiredApprovals = append(requiredApprovals, "runner explicit approvals")
		}
	}
	if runner != "" && !containsString(ctx.Workflow.Data.Agents.Enabled, runner) {
		blockers = append(blockers, "runner "+runner+" is not enabled in workflow")
	}
	applyInputs, applyInputErr := ctx.Store.ListApplyInputsForRun(ctx.Project.ProjectID, recordID)
	if applyInputErr != nil {
		blockers = append(blockers, "apply input store: "+applyInputErr.Error())
	} else if len(applyInputs) > 1 {
		blockers = append(blockers, "multiple external apply inputs require human selection")
	}
	if reason := automationRunBlocker(run, time.Now().UTC()); reason != "" {
		blockers = append(blockers, reason)
	}
	blockers = append(blockers, ctx.concurrencyBlockers(note)...)
	fanout := ctx.fanoutSummary(recordID)
	policy := codexPolicyFromWorkflow(ctx.Workflow.Data)
	if strings.TrimSpace(policy.ApprovalPolicy) != "" && policy.ApprovalPolicy != "never" {
		requiredApprovals = append(requiredApprovals, "codex approval_policy="+policy.ApprovalPolicy)
	}
	branch := ""
	if strings.TrimSpace(ctx.Project.RepoRoot) != "" {
		branch, _ = currentGitBranchIn(ctx.Project.RepoRoot)
	}
	existing := ctx.existingRunPointer(recordID)
	workspaceStrategy := workspaceStrategyForRun(ctx.Workflow.Data, ctx.Project, run, ctx.projectRunsSlice())
	blockers = uniqueStrings(blockers)
	return automationTaskExplanation{
		Schema:            automationExplainSchema,
		ID:                stringField(note.Data, "id"),
		RecordID:          recordID,
		Title:             stringField(note.Data, "title"),
		Path:              note.RelativePath,
		Status:            stringField(note.Data, "status"),
		Readiness:         stringField(note.Data, "readiness"),
		NextOwner:         stringField(note.Data, "next_owner"),
		ProofMode:         stringField(note.Data, "proof_mode"),
		ProofStatus:       stringField(note.Data, "proof_status"),
		Risk:              stringField(note.Data, "risk"),
		Priority:          stringField(note.Data, "priority"),
		Dispatchable:      len(blockers) == 0,
		Blockers:          blockers,
		Project:           automationSummarizeProject(ctx.Project, ctx.projectRunsSlice(), ctx.ProjectRegistered),
		Runner:            runner,
		Lane:              firstNonEmpty(run.Lane, runLaneExecute),
		Command:           command,
		WorkflowPath:      ctx.Workflow.Path,
		WorkspacePath:     automationWorkspacePath(ctx.StateRoot, ctx.Project, ctx.Workflow.Data, run, workspaceStrategy),
		WorkspaceStrategy: string(workspaceStrategy),
		Branch:            branch,
		ApprovalPolicy:    policy.ApprovalPolicy,
		ThreadSandbox:     policy.ThreadSandbox,
		TurnSandboxPolicy: policy.TurnSandboxPolicy,
		RequiredApprovals: uniqueStrings(requiredApprovals),
		ApplyInputs:       applyInputs,
		Fanout:            fanout,
		ExistingRun:       existing,
	}
}

func automationPlanFromExplanation(vaultPath string, note Note, explanation automationTaskExplanation) automationDispatchPlan {
	decision := "dispatch"
	if !explanation.Dispatchable {
		decision = "do_not_dispatch"
	}
	return automationDispatchPlan{
		Schema:            automationPlanSchema,
		Task:              explanation.ID,
		RecordID:          explanation.RecordID,
		Decision:          decision,
		Lane:              explanation.Lane,
		Runner:            explanation.Runner,
		Command:           explanation.Command,
		WorkspacePath:     explanation.WorkspacePath,
		WorkspaceStrategy: explanation.WorkspaceStrategy,
		Branch:            explanation.Branch,
		Blockers:          append([]string{}, explanation.Blockers...),
		RequiredReads:     automationPlanRequiredReads(vaultPath, note),
		ProofRequired:     automationPlanProofRequired(note),
		RequiredApprovals: append([]string{}, explanation.RequiredApprovals...),
		Fanout:            explanation.Fanout,
		Project:           explanation.Project,
	}
}

func automationPlanRequiredReads(vaultPath string, note Note) []string {
	vaultRoot := filepath.ToSlash(firstNonEmpty(relativeFromRepo(v7RepoRoot(vaultPath), vaultPath), defaultRepoVaultDir))
	reads := []string{
		filepath.ToSlash(filepath.Join(vaultRoot, "SKILL.md")),
	}
	if strings.TrimSpace(note.RelativePath) != "" {
		reads = append(reads, filepath.ToSlash(filepath.Join(vaultRoot, note.RelativePath)))
	}
	domains := normalizeList(note.Data["domains"])
	if len(domains) == 0 {
		domains = []string{"project"}
	}
	for _, domain := range domains {
		domain = strings.Trim(domain, "/")
		if domain == "" {
			continue
		}
		reads = append(reads, filepath.ToSlash(filepath.Join(vaultRoot, "knowledge", "domains", domain, "INDEX.md")))
		reads = append(reads, filepath.ToSlash(filepath.Join(vaultRoot, "knowledge", "domains", domain, "CANON.md")))
	}
	return uniqueStrings(reads)
}

func automationPlanProofRequired(note Note) []string {
	proof := normalizeList(note.Data["proof_required"])
	if len(proof) > 0 {
		return proof
	}
	return defaultV7ProofRequired(strings.TrimSpace(stringField(note.Data, "proof_mode")))
}

func (ctx *automationCommandContext) fanoutSummary(recordID string) automationFanoutSummary {
	policy := ctx.Workflow.Data.Fanout
	summary := automationFanoutSummary{
		Enabled:           policy.Enabled,
		MaxChildren:       policy.MaxChildren,
		AllowedChildTypes: append([]string{}, policy.AllowedChildTypes...),
		MergeRule:         policy.MergeRule,
	}
	if !policy.Enabled {
		summary.Blockers = append(summary.Blockers, "fanout disabled by automation policy")
		return summary
	}
	for _, run := range ctx.Runs {
		if ctx.Project.ProjectID != "" && run.ProjectID != ctx.Project.ProjectID {
			continue
		}
		if (run.RecordID == recordID || run.ItemID == recordID) && strings.HasPrefix(run.Lane, "fanout:") && isDispatchingLeaseState(run.LeaseState) {
			summary.ActiveChildren++
		}
	}
	summary.Blockers = append(summary.Blockers, activeFanoutWorkspaceConflicts(ctx.Runs, ctx.Project.ProjectID, recordID)...)
	if policy.MaxChildren > 0 && summary.ActiveChildren >= policy.MaxChildren {
		summary.Blockers = append(summary.Blockers, "fanout child cap reached")
	}
	return summary
}

func activeFanoutWorkspaceConflicts(runs []RunStatus, projectID, recordID string) []string {
	seen := map[string]string{}
	var conflicts []string
	for _, run := range runs {
		if projectID != "" && run.ProjectID != projectID {
			continue
		}
		if (run.RecordID != recordID && run.ItemID != recordID) || !strings.HasPrefix(run.Lane, "fanout:") || !isDispatchingLeaseState(run.LeaseState) {
			continue
		}
		workspace := strings.TrimSpace(run.WorkspacePath)
		if workspace == "" {
			continue
		}
		if previous := seen[workspace]; previous != "" {
			conflicts = append(conflicts, "fanout workspace conflict: "+workspace+" used by "+previous+" and "+run.ActiveAttemptID)
			continue
		}
		seen[workspace] = firstNonEmpty(run.ActiveAttemptID, run.ItemID, run.RecordID)
	}
	return conflicts
}

func automationLegacyTaskBlockers(note Note, wf Workflow) []string {
	var blockers []string
	if daemonNoteKind(note) != "task" {
		blockers = append(blockers, "kind is not task")
	}
	status := stringField(note.Data, "status")
	if !containsString(wf.Tracker.ActiveStates, status) {
		blockers = append(blockers, "status "+fallback(status, "(missing)")+" is not in workflow dispatch_states "+strings.Join(wf.Tracker.ActiveStates, ","))
	}
	return blockers
}

func automationResolveRunner(note Note, wf Workflow) string {
	nextOwner := strings.TrimSpace(stringField(note.Data, "next_owner"))
	if strings.HasPrefix(nextOwner, "agent:") {
		candidate := strings.TrimSpace(strings.TrimPrefix(nextOwner, "agent:"))
		if containsString(wf.Agents.Enabled, candidate) {
			return candidate
		}
	}
	return firstNonEmpty(resolveRunnerForNote(note, wf), wf.Agents.Default)
}

func (ctx *automationCommandContext) effectiveRunForTask(note Note, runner string) RunStatus {
	recordID := trackerRecordID(note)
	run := ctx.ProjectRuns[recordID]
	run.ProjectID = firstNonEmpty(run.ProjectID, ctx.Project.ProjectID)
	run.RecordID = recordID
	run.ItemID = firstNonEmpty(run.ItemID, stringField(note.Data, "id"))
	run.Runner = firstNonEmpty(runner, run.Runner, ctx.Workflow.Data.Agents.Default)
	run.Lane = firstNonEmpty(run.Lane, runLaneExecute)
	if run.LeaseState == "" {
		run.LeaseState = string(LeaseStateUnclaimed)
	}
	workRevision := intField(note.Data, "work_revision")
	if run.WorkRevision != workRevision {
		run.WorkRevision = workRevision
		run.AttemptCount = 0
		run.AttemptOutcome = string(AttemptOutcomeNone)
		run.LeaseState = string(LeaseStateUnclaimed)
		run.NextRetryAt = ""
		run.LastError = ""
		run.SessionRef = ""
		run.StartedAt = ""
		run.LastEventAt = ""
		run.Lane = runLaneExecute
		clearActiveExecution(&run)
	}
	if run.Lane == runLaneReview && LeaseState(run.LeaseState) == LeaseStateReleased {
		run = prepareRunForLaneDispatch(run, runLaneExecute, run.Runner)
	}
	return run
}

func automationRunBlocker(run RunStatus, now time.Time) string {
	switch LeaseState(strings.TrimSpace(run.LeaseState)) {
	case "", LeaseStateUnclaimed:
		return ""
	case LeaseStateRetryQueued:
		if strings.TrimSpace(run.NextRetryAt) == "" {
			return ""
		}
		due, err := time.Parse(time.RFC3339, run.NextRetryAt)
		if err == nil && !due.After(now) {
			return ""
		}
		return "existing run retry queued until " + run.NextRetryAt
	case LeaseStateClaimed, LeaseStateRunning:
		return "existing run is " + run.LeaseState
	case LeaseStateInterrupted:
		return "existing run is interrupted"
	case LeaseStateReleased:
		return "existing run is released; update the task revision before redispatch"
	default:
		return "existing run lease_state " + fallback(run.LeaseState, "(missing)") + " is not dispatchable"
	}
}

func (ctx *automationCommandContext) concurrencyBlockers(note Note) []string {
	if !ctx.ProjectRegistered {
		return nil
	}
	var blockers []string
	globalLimit, err := ctx.Store.GlobalActiveRunLimit()
	if err != nil {
		blockers = append(blockers, "global active run limit is invalid: "+err.Error())
	} else {
		if globalLimit <= 0 {
			globalLimit = 2
		}
		if globalLimit > 0 && ctx.GlobalActiveRuns >= globalLimit {
			blockers = append(blockers, fmt.Sprintf("global active run limit reached (%d/%d)", ctx.GlobalActiveRuns, globalLimit))
		}
	}
	projectLimit := projectActiveRunLimit(ctx.Workflow.Data)
	if ctx.ProjectActiveRuns >= projectLimit {
		blockers = append(blockers, fmt.Sprintf("project active run limit reached (%d/%d)", ctx.ProjectActiveRuns, projectLimit))
	}
	status := stringField(note.Data, "status")
	if stateDispatchCapReached(status, ctx.StateActiveRuns, ctx.Workflow.Data) {
		blockers = append(blockers, fmt.Sprintf("state %q concurrency cap reached", status))
	}
	return blockers
}

func automationWorkspacePath(stateRoot string, project RegisteredProject, wf Workflow, run RunStatus, strategy WorkspaceStrategy) string {
	req := WorkspacePrepareRequest{
		ProjectID:     project.ProjectID,
		ProjectKey:    project.ProjectKey,
		RecordID:      run.RecordID,
		ItemID:        run.ItemID,
		RepoRoot:      project.RepoRoot,
		StateRoot:     stateRoot,
		WorkspaceRoot: wf.Workspace.Root,
		Strategy:      strategy,
		WorkRevision:  run.WorkRevision,
	}
	workspacePath, _, err := workspacePathForRequest(req)
	if err != nil {
		return ""
	}
	return workspacePath
}

func (ctx *automationCommandContext) existingRunPointer(recordID string) *RunStatus {
	run, ok := ctx.ProjectRuns[recordID]
	if !ok {
		return nil
	}
	copy := run
	return &copy
}

func (ctx *automationCommandContext) projectRunsSlice() []RunStatus {
	runs := make([]RunStatus, 0, len(ctx.ProjectRuns))
	for _, run := range ctx.ProjectRuns {
		runs = append(runs, run)
	}
	return runs
}

func automationProjectSummaries(projects []RegisteredProject, runs []RunStatus) []automationProjectSummary {
	runsByProject := map[string][]RunStatus{}
	for _, run := range runs {
		runsByProject[run.ProjectID] = append(runsByProject[run.ProjectID], run)
	}
	out := make([]automationProjectSummary, 0, len(projects))
	for _, project := range projects {
		out = append(out, automationSummarizeProject(project, runsByProject[project.ProjectID], true))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name == out[j].Name {
			return out[i].ProjectID < out[j].ProjectID
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func automationSummarizeProject(project RegisteredProject, runs []RunStatus, registered bool) automationProjectSummary {
	summary := automationProjectSummary{
		ProjectID:    project.ProjectID,
		ProjectKey:   project.ProjectKey,
		Name:         project.Name,
		RepoRoot:     project.RepoRoot,
		VaultRoot:    project.VaultRoot,
		WorkflowPath: project.WorkflowPath,
		Registered:   registered,
		Enabled:      project.Enabled,
		Health:       project.Health,
		LastPollAt:   project.LastPollAt,
		LastError:    project.LastError,
	}
	for _, run := range runs {
		switch LeaseState(run.LeaseState) {
		case LeaseStateClaimed, LeaseStateRunning:
			summary.ActiveRuns++
		case LeaseStateRetryQueued:
			summary.QueuedRuns++
		}
	}
	return summary
}

func printAutomationStatus(report automationStatusReport) {
	fmt.Printf("Automation state root: %s\n", report.StateRoot)
	fmt.Printf("Registered projects: %d\n", report.ProjectCount)
	fmt.Printf("Active runs: %d / %d\n", report.ActiveRuns, report.MaxActiveRuns)
	if len(report.Projects) == 0 {
		fmt.Println("Projects: none")
		return
	}
	fmt.Println("Projects:")
	for _, project := range report.Projects {
		state := "disabled"
		if project.Enabled {
			state = "enabled"
		}
		fmt.Printf("  %-12s %-8s %-8s active=%d queued=%d %s\n", project.ProjectKey, state, project.Health, project.ActiveRuns, project.QueuedRuns, project.RepoRoot)
	}
}

func printAutomationQueue(report automationQueueReport) {
	fmt.Printf("Automation queue for %s\n", firstNonEmpty(report.Project.Name, report.Project.VaultRoot))
	fmt.Println("Eligible:")
	if len(report.Eligible) == 0 {
		fmt.Println("  none")
	} else {
		for _, item := range report.Eligible {
			fmt.Printf("  %-14s %-8s %-8s %s\n", item.ID, item.Status, item.Runner, item.Title)
		}
	}
	fmt.Println("Blocked:")
	if len(report.Blocked) == 0 {
		fmt.Println("  none")
		return
	}
	for _, item := range report.Blocked {
		fmt.Printf("  %-14s %-8s %s\n", item.ID, item.Status, strings.Join(item.Blockers, "; "))
	}
}

func printAutomationPlan(plan automationDispatchPlan) {
	fmt.Printf("%s automation plan: %s\n", plan.Task, plan.Decision)
	fmt.Printf("  runner=%s lane=%s workspace=%s\n", fallback(plan.Runner, "-"), fallback(plan.Lane, "-"), fallback(plan.WorkspacePath, "-"))
	if len(plan.RequiredReads) > 0 {
		fmt.Println("  required_reads:")
		for _, read := range plan.RequiredReads {
			fmt.Println("    - " + read)
		}
	}
	if len(plan.ProofRequired) > 0 {
		fmt.Printf("  proof_required=%s\n", strings.Join(plan.ProofRequired, ","))
	}
	if len(plan.Blockers) == 0 {
		fmt.Println("  blockers=none")
		return
	}
	fmt.Println("  blockers:")
	for _, blocker := range plan.Blockers {
		fmt.Println("    - " + blocker)
	}
}

func printAutomationExplanation(explanation automationTaskExplanation) {
	state := "blocked"
	if explanation.Dispatchable {
		state = "dispatchable"
	}
	fmt.Printf("%s automation: %s\n", explanation.ID, state)
	fmt.Printf("  status=%s readiness=%s next_owner=%s risk=%s proof=%s/%s\n",
		fallback(explanation.Status, "-"),
		fallback(explanation.Readiness, "-"),
		fallback(explanation.NextOwner, "-"),
		fallback(explanation.Risk, "-"),
		fallback(explanation.ProofMode, "-"),
		fallback(explanation.ProofStatus, "-"),
	)
	fmt.Printf("  runner=%s lane=%s workspace=%s\n", fallback(explanation.Runner, "-"), fallback(explanation.Lane, "-"), fallback(explanation.WorkspacePath, "-"))
	fmt.Printf("  fanout enabled=%t active=%d max=%d merge_rule=%s\n",
		explanation.Fanout.Enabled,
		explanation.Fanout.ActiveChildren,
		explanation.Fanout.MaxChildren,
		fallback(explanation.Fanout.MergeRule, "-"),
	)
	if len(explanation.Fanout.Blockers) > 0 {
		fmt.Printf("  fanout_blockers=%s\n", strings.Join(explanation.Fanout.Blockers, ", "))
	}
	if len(explanation.RequiredApprovals) > 0 {
		fmt.Printf("  approvals=%s\n", strings.Join(explanation.RequiredApprovals, ", "))
	}
	if len(explanation.ApplyInputs) > 0 {
		var paths []string
		for _, input := range explanation.ApplyInputs {
			paths = append(paths, firstNonEmpty(input.RelPath, input.Path))
		}
		fmt.Printf("  apply_inputs=%s\n", strings.Join(paths, ", "))
	}
	if len(explanation.Blockers) == 0 {
		fmt.Println("  blockers=none")
		return
	}
	fmt.Println("  blockers:")
	for _, blocker := range explanation.Blockers {
		fmt.Println("    - " + blocker)
	}
}

func intFromAny(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	default:
		return atoiSafe(stringValue(value))
	}
}
