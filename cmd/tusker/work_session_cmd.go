package main

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// workSessionPacket is deliberately small: it is the hand-off contract for a
// caller, not a second runtime projection.  The run row remains the sole
// authority for owner, generation, workspace, branch and liveness.
type workSessionPacket struct {
	Schema                string            `json:"schema"`
	Action                string            `json:"action"`
	TaskID                string            `json:"task_id"`
	Owner                 string            `json:"owner,omitempty"`
	Revision              int               `json:"work_revision,omitempty"`
	Workspace             string            `json:"workspace,omitempty"`
	Branch                string            `json:"branch,omitempty"`
	Head                  string            `json:"head,omitempty"`
	LeaseExpiry           string            `json:"lease_expires_at,omitempty"`
	Packet                string            `json:"packet,omitempty"`
	Next                  string            `json:"next"`
	Run                   *RunStatus        `json:"run,omitempty"`
	Authorization         *RunAuthorization `json:"authorization,omitempty"`
	ProofFingerprint      string            `json:"proof_fingerprint,omitempty"`
	GateFingerprint       string            `json:"gate_fingerprint,omitempty"`
	ImplementationAttempt string            `json:"implementation_attempt_id,omitempty"`
	ImplementationActor   string            `json:"implementation_actor,omitempty"`
	MaterialFingerprint   string            `json:"material_fingerprint,omitempty"`
}

func workSessionCmd(args Args, action string) error {
	args["id"] = firstNonEmpty(args.String("id"), args.String("_pos0"))
	switch action {
	case "start":
		return workSessionStartCmd(args)
	case "review":
		return workSessionReviewCmd(args)
	case "status":
		return workSessionStatusCmd(args)
	case "heartbeat":
		return workSessionLifecycleCmd(args, "heartbeat")
	case "submit":
		return workSessionLifecycleCmd(args, "submit")
	case "fail":
		return workSessionLifecycleCmd(args, "fail")
	case "release":
		return workSessionLifecycleCmd(args, "release")
	default:
		return tuskerError(errorInvalidArg, "unknown work action: "+action)
	}
}

func workSessionReviewCmd(args Args) error {
	vault, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	id, err := requireArg(args, "id")
	if err != nil {
		return err
	}
	note, err := resolveV7Note(vault, id, "task")
	if err != nil {
		return err
	}
	wf, err := loadWorkflow(vault)
	if err != nil {
		return err
	}
	configured := reviewerActorForNote(wf.Data.Reviewer.Actor, note)
	actor, err := resolveReviewResultActor(args, configured, note)
	if err != nil {
		return err
	}
	canonical, ok := normalizeV7ProposalActor(configured)
	if !ok || actor != canonical {
		return tuskerError(errorInvalidTransition, "reviewer actor is not authorized for this task")
	}
	args["lane"], args["owner"], args["by"] = runLaneReview, actor, actor
	return workSessionStartCmd(args)
}

func workSessionStartCmd(args Args) error {
	// This entry point is for a user-directed owner.  The daemon has its own
	// snapshot-bound claim path; accepting daemon_auto here would let an
	// interactive shell impersonate a dispatcher.
	source := firstNonEmpty(args.String("source"), "tusker_cli")
	if source != "tusker_cli" && source != "codex" && source != "claude" {
		return tuskerError(errorInvalidArg, "work start source must be tusker_cli, codex, or claude")
	}
	args["source"] = source
	if args.String("owner") == "" {
		args["owner"] = firstNonEmpty(args.String("by"), args.String("actor"), "agent:"+defaultActorName())
	}
	result, ctx, err := claimWorkSession(args)
	if err != nil {
		return workSessionClaimRefusal(args.String("id"), err)
	}
	defer ctx.Close()
	if !result.Claimed || result.Run == nil {
		owner := ""
		if result.Run != nil {
			owner = result.Run.LeaseOwner
		}
		return workSessionStartBlocker(workSessionOwnerBlocker(args.String("id"), owner, "A healthy work-session owner already holds this task."))
	}
	branch, head := "", ""
	if identity, identityErr := ctx.Store.RunIdentity(result.Run.ProjectID, result.Run.RecordID); identityErr == nil && identity != nil {
		branch = identity.Branch
		head = identity.Head
	}
	note, _ := ctx.findTask(result.Run.ItemID)
	packet := workSessionPacket{
		Schema: "tusker.work-session/v1", Action: firstNonEmpty(args.String("lane"), "start"), TaskID: result.Run.ItemID,
		Owner: result.Run.LeaseOwner, Revision: result.Run.WorkRevision,
		Workspace: result.Run.WorkspacePath, Branch: branch, Head: head, LeaseExpiry: result.Run.LeaseExpiresAt,
		Packet: v7Packet(ctx.Project.VaultRoot, note, workSessionV7Index(ctx.Notes), "agent"),
		Next:   workSessionNext(*result.Run),
		Run:    result.Run, Authorization: result.Authorization,
	}
	if result.Run.Lane == runLaneReview {
		packet.ProofFingerprint = args.String("review-proof-fingerprint")
		packet.GateFingerprint = args.String("review-gate-fingerprint")
		packet.ImplementationAttempt = args.String("implementation-attempt")
		packet.ImplementationActor = args.String("implementation-actor")
		packet.MaterialFingerprint = args.String("review-material-fingerprint")
		packet.Next = workSessionReviewNext(*result.Run, note, packet)
	}
	workSessionNotify(ctx.Project.VaultRoot, result.Run)
	if !args.Bool("embedded") {
		emitJSON(packet)
	}
	return nil
}

func workSessionV7Index(notes []Note) v7Index {
	idx := v7Index{Tasks: map[string]Note{}, Gates: map[string]Note{}, Waves: map[string]Note{}, Escalations: map[string]Note{}, Evidence: map[string][]Note{}, Attempts: map[string][]Note{}, Decisions: map[string]Note{}, Epics: map[string]Note{}, Proposals: map[string]Note{}, Closeouts: map[string][]Note{}}
	for _, note := range notes {
		id := stringField(note.Data, "id")
		switch effectiveV7Kind(note.Data) {
		case "task":
			idx.Tasks[id] = note
		case "gate":
			idx.Gates[id] = note
		case "wave":
			idx.Waves[id] = note
		case "evidence":
			idx.Evidence[stringField(note.Data, "task")] = append(idx.Evidence[stringField(note.Data, "task")], note)
		case "attempt":
			idx.Attempts[stringField(note.Data, "task")] = append(idx.Attempts[stringField(note.Data, "task")], note)
		}
	}
	return idx
}

func claimWorkSession(args Args) (runClaimResult, *automationCommandContext, error) {
	id, err := requireArg(args, "id")
	if err != nil {
		return runClaimResult{}, nil, err
	}
	ctx, err := loadAutomationCommandContext(args)
	if err != nil {
		return runClaimResult{}, nil, err
	}
	closeOnError := true
	defer func() {
		if closeOnError {
			ctx.Close()
		}
	}()
	note, err := ctx.findTask(id)
	if err != nil {
		return runClaimResult{}, nil, err
	}
	lane := strings.TrimSpace(args.String("lane"))
	if lane == "" {
		lane = runLaneExecute
	}
	if lane != runLaneExecute && lane != runLaneReview {
		return runClaimResult{}, nil, tuskerError(errorInvalidArg, "work start lane must be execute or review")
	}
	// A same-task holder is reclaimable only after both independent facts hold:
	// its heartbeat is past the reclaim grace and its recorded process identity
	// no longer exists.  Never let an expired timestamp alone steal a live
	// interactive workspace.
	if current, findErr := ctx.Store.FindRunScoped(ctx.Project.ProjectID, trackerRecordID(note)); findErr != nil {
		return runClaimResult{}, nil, findErr
	} else if current != nil && runFreshness(current, time.Now().UTC()) == "stale" {
		if _, blocking := holderLiveness(*current, time.Now().UTC()); blocking {
			return runClaimResult{}, nil, workSessionStartBlocker(workSessionOwnerBlocker(stringField(note.Data, "id"), current.LeaseOwner, "The expired holder process is still alive."))
		}
		ok, reclaimErr := ctx.Store.ReclaimExpiredRunLease(current.ProjectID, current.RecordID, time.Now().UTC(), defaultRunLeaseTTL, "work-session reclaim after expired heartbeat and failed liveness probe")
		if reclaimErr != nil {
			return runClaimResult{}, nil, reclaimErr
		}
		if !ok {
			return runClaimResult{}, nil, workSessionStartBlocker(workSessionOwnerBlocker(stringField(note.Data, "id"), current.LeaseOwner, "The expired holder has not passed reclaim grace."))
		}
		ctx.Runs, _ = ctx.Store.ListRuns()
		if ctx.ProjectActiveRuns > 0 {
			ctx.ProjectActiveRuns--
		}
		if ctx.GlobalActiveRuns > 0 {
			ctx.GlobalActiveRuns--
		}
		if ctx.StateActiveRuns[current.LeaseState] > 0 {
			ctx.StateActiveRuns[current.LeaseState]--
		}
		if latest, latestErr := ctx.Store.FindRunScoped(ctx.Project.ProjectID, trackerRecordID(note)); latestErr == nil && latest != nil {
			projected := *latest
			projected.LeaseState = string(LeaseStateUnclaimed)
			ctx.ProjectRuns[trackerRecordID(note)] = projected
		}
	}
	if blockers := workSessionAdmissionBlockersForLane(note, workSessionV7Index(ctx.Notes), ctx.NotesByID, ctx.NotesByRecordID, lane); len(blockers) > 0 {
		return runClaimResult{}, nil, workSessionStartBlocker(blockers[0])
	}
	run := ctx.effectiveRunForTask(note, automationResolveRunner(note, ctx.Workflow.Data))
	run.ProjectID = ctx.Project.ProjectID
	var reviewBinding *reviewImplementation
	if lane == runLaneReview {
		binding, bindingErr := reviewImplementationBinding(ctx.Store, run, note, ctx.Project.VaultRoot)
		if bindingErr != nil {
			return runClaimResult{}, nil, bindingErr
		}
		if strings.TrimSpace(args.String("owner")) != binding.ReviewerActor {
			return runClaimResult{}, nil, tuskerError(errorInvalidTransition, "review session owner does not match the configured reviewer")
		}
		if binding.ImplementationActor == binding.ReviewerActor {
			return runClaimResult{}, nil, tuskerError(errorInvalidTransition, "implementing session cannot masquerade as independent review")
		}
		selected, profileErr := resolveRunProfileForLane(note, ctx.Workflow.Data, runLaneReview, firstNonEmpty(ctx.Workflow.Data.Reviewer.Runner, ctx.Workflow.Data.Agents.Default))
		if profileErr != nil {
			return runClaimResult{}, nil, profileErr
		}
		run = prepareRunForLaneDispatch(run, runLaneReview, firstNonEmpty(selected.Definition.Harness, ctx.Workflow.Data.Reviewer.Runner, run.Runner))
		run = applyResolvedProfileToRun(run, selected)
		proof, gates, snapshotErr := reviewObjectiveSnapshots(ctx.Project.VaultRoot, note)
		if snapshotErr != nil {
			return runClaimResult{}, nil, snapshotErr
		}
		reviewBinding = &binding
		args["implementation-attempt"] = binding.AttemptID
		args["implementation-actor"] = binding.ImplementationActor
		args["review-material-fingerprint"] = binding.MaterialFingerprint
		args["review-proof-fingerprint"], args["review-gate-fingerprint"] = proof, gates
	}
	workspaceStrategy := workspaceStrategyForRun(ctx.Workflow.Data, ctx.Project, run, ctx.projectRunsSlice())
	branchName := ""
	if reviewBinding != nil {
		// Review the same immutable-at-claim material the implementation submitted.
		// A new worktree at HEAD would erase an uncommitted implementation diff.
		run.WorkspacePath, branchName = reviewBinding.WorkspacePath, reviewBinding.Branch
	} else {
		branchBase := ""
		var workspaceErr error
		branchName, branchBase, workspaceErr = v7WorkspaceBranchForLane(ctx.Project.VaultRoot, note, run.Lane)
		if workspaceErr != nil {
			return runClaimResult{}, nil, workspaceErr
		}
		if branchName == "" && v7GitRepo(ctx.Project.RepoRoot) {
			branchName = v7TaskBranchName(trackerRecordID(note))
		}
		workspace, prepareErr := NewWorkspaceManager().Prepare(WorkspacePrepareRequest{ProjectID: ctx.Project.ProjectID, ProjectKey: ctx.Project.ProjectKey, RecordID: run.RecordID, ItemID: run.ItemID, BranchName: branchName, BranchBase: branchBase, RepoRoot: ctx.Project.RepoRoot, StateRoot: ctx.StateRoot, WorkspaceRoot: ctx.Workflow.Data.Workspace.Root, Strategy: workspaceStrategy, WorkRevision: run.WorkRevision, MaxLiveWorktrees: ctx.Workflow.Data.Workspace.MaxLiveWorktrees})
		if prepareErr != nil {
			return runClaimResult{}, nil, workSessionStartBlocker(workSessionUnsafeWorkspaceBlocker(stringField(note.Data, "id"), prepareErr.Error()))
		}
		run.WorkspacePath = workspace.Path
	}
	owner := args.String("owner")
	service := newRunOwnershipService(ctx.Store)
	claimNotes := orchestrationOwnedPathNotes(ctx.NotesByID, ctx.Workflow.Data)
	service.withOwnedPathContext(ctx.Project.VaultRoot, claimNotes[stringField(note.Data, "id")], claimNotes)
	// Interactive work owns a user-directed session, not an unattended dispatch
	// slot. Same-task and owned-path safety stay in the ownership service.
	service.projectConcurrencyLimit = 0
	identity := runIdentityForClaim(run, ctx.Project.RepoRoot, run.WorkspacePath, string(workspaceStrategy), branchName)
	trigger := "work_start"
	if lane == runLaneReview {
		trigger = "work_review"
	}
	result, err := service.claimWorkSessionWithAuthorizationWithParent(run, owner, RunAuthorization{Source: args.String("source"), Actor: owner, Trigger: trigger, ProjectAutomationEnabled: ctx.Workflow.Data.AutomationEnabled}, identity, args.String("implementation-attempt"))
	if err != nil {
		return runClaimResult{}, nil, err
	}
	if result.Claimed && result.Run != nil {
		_ = refreshStreamBoardForProject(ctx.Store, result.Run.ProjectID)
	}
	closeOnError = false
	return result, ctx, nil
}

type reviewImplementation struct {
	AttemptID           string
	ImplementationActor string
	ReviewerActor       string
	MaterialFingerprint string
	WorkspacePath       string
	Branch              string
}

func reviewImplementationBinding(store *RuntimeStore, run RunStatus, note Note, vault string) (reviewImplementation, error) {
	if stringField(note.Data, "status") != "review" {
		return reviewImplementation{}, tuskerError(errorInvalidTransition, "review session requires task status review")
	}
	if run.Lane != runLaneExecute || LeaseState(run.LeaseState) != LeaseStateReleased || AttemptOutcome(run.AttemptOutcome) != AttemptOutcomeSucceeded {
		return reviewImplementation{}, tuskerError(errorInvalidTransition, "review session requires a completed execute work session")
	}
	workRevision := intField(note.Data, "work_revision")
	source := firstNonEmpty(stringField(note.Data, "source_sha"), stringField(note.Data, "source_commit"))
	if workRevision == 0 || source == "" || run.WorkRevision != workRevision {
		return reviewImplementation{}, tuskerError(errorInvalidTransition, fmt.Sprintf("review session requires the current implementation revision and source identity (task revision %d, run revision %d, source %q)", workRevision, run.WorkRevision, source))
	}
	parent, material, materialErr := reviewImplementationParent(store, vault, run.ProjectID, run.RecordID, workRevision, source, note)
	if materialErr != nil {
		return reviewImplementation{}, materialErr
	}
	var actor string
	err := store.queryRowScan(`SELECT actor FROM run_authorizations WHERE project_id=? AND record_id=? AND lease_generation=?`, []any{run.ProjectID, run.RecordID, run.LeaseGeneration}, &actor)
	if err != nil || strings.TrimSpace(actor) == "" {
		return reviewImplementation{}, firstNonNil(err, tuskerError(errorInvalidTransition, "review session requires durable implementation session provenance"))
	}
	wf, err := loadWorkflow(vault)
	if err != nil {
		return reviewImplementation{}, err
	}
	return reviewImplementation{AttemptID: parent.AttemptID, ImplementationActor: actor, ReviewerActor: reviewerActorForNote(wf.Data.Reviewer.Actor, note), MaterialFingerprint: material, WorkspacePath: parent.WorkspacePath, Branch: firstNonEmpty(parent.EndState.Branch, parent.BranchName)}, nil
}

// reviewImplementationParent is the single durable binding used before both
// interactive and daemon review. It rejects a changed declared scope before a
// reviewer can certify a narrower or different implementation than execute
// submitted.
func reviewImplementationParent(store *RuntimeStore, vault, projectID, recordID string, workRevision int, source string, note Note) (RunAttempt, string, error) {
	attempts, err := store.ListAttemptsForRun(projectID, recordID)
	if err != nil {
		return RunAttempt{}, "", err
	}
	for _, parent := range attempts {
		if parent.Lane != runLaneExecute || parent.WorkRevision != workRevision || parent.Outcome != string(AttemptOutcomeSucceeded) || parent.EndState.HeadSHA != source {
			continue
		}
		currentScope, scopeErr := canonicalTaskMaterialScope(vault, note)
		if scopeErr != nil || strings.Join(currentScope, "\x00") != strings.Join(parent.EndState.MaterialScope, "\x00") {
			return RunAttempt{}, "", tuskerError(errorInvalidTransition, "review refused: declared implementation material scope changed after execute submission")
		}
		material, materialErr := verifiedImplementationWorkspaceMaterial(parent)
		if materialErr != nil {
			return RunAttempt{}, "", materialErr
		}
		return parent, material, nil
	}
	return RunAttempt{}, "", tuskerError(errorInvalidTransition, "review requires a successful execute attempt bound to the current source")
}

func verifiedImplementationWorkspaceMaterial(parent RunAttempt) (string, error) {
	if strings.TrimSpace(parent.EndState.MaterialFingerprint) == "" || len(parent.EndState.MaterialScope) == 0 {
		return "", tuskerError(errorInvalidTransition, "review session requires a declared implementation material scope and fingerprint")
	}
	material, err := workspaceTreeStateHashForPaths(parent.WorkspacePath, parent.EndState.MaterialScope)
	if err != nil {
		return "", tuskerError(errorInvalidTransition, "review session cannot read implementation workspace material: "+err.Error())
	}
	if material != parent.EndState.MaterialFingerprint {
		return "", tuskerError(errorInvalidTransition, "review session refused: implementation workspace material changed after execute submission")
	}
	return material, nil
}

func workSessionReviewNext(run RunStatus, note Note, packet workSessionPacket) string {
	return "tusker review submit " + run.ItemID + " --attempt " + run.ActiveAttemptID + " --by " + run.LeaseOwner + " --task-rev " + stringField(note.Data, "state_rev") + " --source-sha " + firstNonEmpty(stringField(note.Data, "source_sha"), stringField(note.Data, "source_commit")) + " --work-rev " + strconv.Itoa(run.WorkRevision) + " --proof-fingerprint " + packet.ProofFingerprint + " --gate-fingerprint " + packet.GateFingerprint + " --material-fingerprint " + packet.MaterialFingerprint + " --verdict pass|changes_requested|blocked --covers <acceptance-ids> --summary \"<review summary>\""
}

func workSessionStartBlocker(blocker ReadinessBlocker) error {
	code := "WORK_SESSION_NOT_READY"
	switch blocker.Kind {
	case ReadinessBlockerInteractiveOwner:
		code = "WORK_SESSION_HEALTHY_OWNER"
	case ReadinessBlockerHumanGateOpen:
		code = "WORK_SESSION_HUMAN_GATE"
	case ReadinessBlockerTaskTerminal:
		code = "WORK_SESSION_TERMINAL"
	case ReadinessBlockerDependencyIncomplete:
		code = "WORK_SESSION_DEPENDENCY_BLOCKED"
	case ReadinessBlockerWorkspaceUnsafe:
		code = "WORK_SESSION_UNSAFE_WORKSPACE"
	case ReadinessBlockerOwnedPathConflict:
		code = "OWNED_PATH_CONFLICT"
	}
	return tuskerError(code, "work start refused: "+blocker.Reason, withContext(map[string]any{"readiness_blocker": blocker}))
}

func workSessionClaimRefusal(taskID string, err error) error {
	issue := errorToIssue(err)
	if issue.Code != "OWNED_PATH_CONFLICT" {
		return err
	}
	conflictingTaskID, owner := "", ""
	if context, ok := issue.Context.(map[string]any); ok {
		conflictingTaskID, _ = context["task_id"].(string)
		owner, _ = context["holder"].(string)
	}
	return workSessionStartBlocker(ReadinessBlocker{
		ID: "interactive-owned-path:" + taskID + ":" + conflictingTaskID, Kind: ReadinessBlockerOwnedPathConflict, Authority: ReadinessAuthorityInteractive,
		Affects: []ReadinessDimensionKind{ReadinessDimensionInteractive}, TaskID: taskID, ConflictingTaskID: conflictingTaskID, Owner: owner,
		Reason: issue.Message, Remedy: "Wait for the conflicting owner to release its owned path or choose non-overlapping work.",
	})
}

func workSessionStatusCmd(args Args) error {
	id, err := requireArg(args, "id")
	if err != nil {
		return err
	}
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		return err
	}
	defer store.Close()
	run, err := findRunScopedOrAmbiguous(store, args.String("project"), id)
	if err != nil {
		return err
	}
	if run == nil {
		return tuskerError(errorNotFound, "work session not found: "+id)
	}
	branch, head := "", ""
	if identity, identityErr := store.RunIdentity(run.ProjectID, run.RecordID); identityErr == nil && identity != nil {
		branch, head = identity.Branch, identity.Head
	}
	authorization, _ := store.LatestRunAuthorization(run.ProjectID, run.RecordID)
	emitJSON(workSessionPacket{Schema: "tusker.work-session/v1", Action: "status", TaskID: run.ItemID, Owner: run.LeaseOwner, Revision: run.WorkRevision, Workspace: run.WorkspacePath, Branch: branch, Head: head, LeaseExpiry: run.LeaseExpiresAt, Next: workSessionNext(*run), Run: run, Authorization: authorization})
	return nil
}

func workSessionNext(run RunStatus) string {
	if LeaseState(run.LeaseState) == LeaseStateClaimed || LeaseState(run.LeaseState) == LeaseStateRunning {
		return "tusker work heartbeat " + run.ItemID + " --by " + run.LeaseOwner
	}
	return "tusker work start " + run.ItemID + " --by <agent>"
}

func workSessionLifecycleCmd(args Args, action string) error {
	if args.String("owner") == "" {
		args["owner"] = firstNonEmpty(args.String("by"), args.String("actor"))
	}
	if strings.TrimSpace(args.String("owner")) == "" {
		return tuskerError(errorMissingArg, "work "+action+" requires --by")
	}
	if action == "release" {
		args["reason"] = firstNonEmpty(args.String("reason"), "work session released")
	}
	if err := requireWorkSessionRevision(args); err != nil {
		return err
	}
	if err := runsLifecycleCmd(args, action); err != nil {
		return err
	}
	workSessionNotifyRun(args.String("id"))
	return nil
}

func canonicalTaskMaterialScope(vault string, note Note) ([]string, error) {
	scope, err := taskMaterialScope(note)
	if err != nil || stringField(note.Data, "work_kind") != "integrator" {
		return scope, err
	}
	wf, err := loadWorkflow(vault)
	if err != nil {
		return nil, err
	}
	return normalizeWorkspaceMaterialScope(append(scope, wf.Data.Orchestration.SharedNamespaces...))
}

func taskMaterialScope(note Note) ([]string, error) {
	paths := append([]string{}, normalizeList(note.Data["owned_paths"])...)
	paths = append(paths, normalizeList(note.Data["generated_outputs"])...)
	paths = append(paths, normalizeList(note.Data["knowledge_nodes"])...)
	for _, ref := range normalizeList(note.Data["spec_refs"]) {
		ref = firstNonEmpty(wikiTarget(ref), ref)
		if strings.Contains(ref, "://") || filepath.IsAbs(ref) {
			continue
		}
		paths = append(paths, ref)
	}
	return normalizeWorkspaceMaterialScope(paths)
}

func requireWorkSessionRevision(args Args) error {
	expectedRevision, err := workSessionRevisionArg(args)
	if err != nil {
		return err
	}
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		return err
	}
	defer store.Close()
	run, err := findRunScopedOrAmbiguous(store, args.String("project"), args.String("id"))
	if err != nil {
		return err
	}
	if run == nil {
		return tuskerError(errorNotFound, "work session not found: "+args.String("id"))
	}
	if expectedRevision != nil && *expectedRevision != run.WorkRevision {
		return workSessionStaleRevisionError("CAS_CONFLICT", args.String("id"), *expectedRevision, run.WorkRevision)
	}
	loaded, err := loadRegisteredProjects(store, registeredProjectLoadOptions{LoadDisabled: true, ProjectID: run.ProjectID})
	if err != nil {
		return err
	}
	if len(loaded) == 1 && loaded[0].LoadError == nil {
		note, noteErr := resolveV7Note(loaded[0].Project.VaultRoot, run.ItemID, "task")
		if noteErr != nil {
			return noteErr
		}
		if revision := intField(note.Data, "work_revision"); revision != run.WorkRevision {
			return workSessionStaleRevisionError("WORK_SESSION_STALE", args.String("id"), run.WorkRevision, revision)
		}
	}
	return nil
}

func workSessionRevisionArg(args Args) (*int, error) {
	raw := strings.TrimSpace(args.String("revision"))
	if raw == "" {
		return nil, nil
	}
	revision, err := strconv.Atoi(raw)
	if err != nil || revision < 0 {
		return nil, tuskerError(errorInvalidArg, "work revision must be a non-negative integer")
	}
	return &revision, nil
}

func workSessionStaleRevisionError(code, taskID string, expected, current int) error {
	blocker := ReadinessBlocker{
		ID: "interactive-revision:" + taskID, Kind: ReadinessBlockerWorkRevisionStale, Authority: ReadinessAuthorityInteractive,
		Affects: []ReadinessDimensionKind{ReadinessDimensionInteractive}, TaskID: taskID,
		Reason: fmt.Sprintf("Work revision changed: expected %d, current %d.", expected, current), Remedy: "Restart the work session against the current task revision.",
	}
	return tuskerError(code, blocker.Reason, withContext(map[string]any{"readiness_blocker": blocker}))
}

func workSessionNotifyRun(id string) {
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		return
	}
	defer store.Close()
	run, err := store.FindRun(id)
	if err != nil || run == nil {
		return
	}
	_ = sendDaemonControlOneWay(DefaultStateRoot(), daemonControlRequest{
		Command: "reconcile_project", ProjectID: run.ProjectID, Cause: "work_session",
		Changes: []daemonControlChange{{ID: run.ItemID, Kind: "run", Revision: fmt.Sprintf("lease:%d", run.LeaseGeneration), Eligibility: []string{"runtime", "ownership"}}},
	}, 250*time.Millisecond)
}

func workSessionNotify(vaultPath string, run *RunStatus) {
	// Notifications are one-way reconciliation hints.  They do not start the
	// daemon, alter automation.enabled, or create a worker.
	projectID := ""
	if store, err := OpenRuntimeStore(DefaultStateRoot()); err == nil {
		if loaded, listErr := loadRegisteredProjects(store, registeredProjectLoadOptions{MetadataOnly: true, LoadDisabled: true}); listErr == nil {
			for _, project := range loadedRegisteredProjects(loaded) {
				if sameCanonicalProjectPath(project.VaultRoot, vaultPath) {
					projectID = project.ProjectID
					break
				}
			}
		}
		_ = store.Close()
	}
	if projectID == "" {
		projectID, _ = resolveV7ProjectID(vaultPath)
	}
	if projectID == "" || run == nil {
		return
	}
	_ = sendDaemonControlOneWay(DefaultStateRoot(), daemonControlRequest{
		Command: "reconcile_project", ProjectID: projectID, Cause: "work_session",
		Changes: []daemonControlChange{{ID: run.ItemID, Kind: "run", Revision: fmt.Sprintf("lease:%d", run.LeaseGeneration), Eligibility: []string{"runtime", "ownership"}}},
	}, 250*time.Millisecond)
}
