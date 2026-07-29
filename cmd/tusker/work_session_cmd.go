package main

import (
	"fmt"
	"strings"
	"time"
)

// workSessionPacket is deliberately small: it is the hand-off contract for a
// caller, not a second runtime projection.  The run row remains the sole
// authority for owner, generation, workspace, branch and liveness.
type workSessionPacket struct {
	Schema        string            `json:"schema"`
	Action        string            `json:"action"`
	TaskID        string            `json:"task_id"`
	Owner         string            `json:"owner,omitempty"`
	Revision      int               `json:"work_revision,omitempty"`
	Workspace     string            `json:"workspace,omitempty"`
	Branch        string            `json:"branch,omitempty"`
	Head          string            `json:"head,omitempty"`
	LeaseExpiry   string            `json:"lease_expires_at,omitempty"`
	Packet        string            `json:"packet,omitempty"`
	Next          string            `json:"next"`
	Run           *RunStatus        `json:"run,omitempty"`
	Authorization *RunAuthorization `json:"authorization,omitempty"`
}

func workSessionCmd(args Args, action string) error {
	args["id"] = firstNonEmpty(args.String("id"), args.String("_pos0"))
	switch action {
	case "start":
		return workSessionStartCmd(args)
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
		Schema: "tusker.work-session/v1", Action: "start", TaskID: result.Run.ItemID,
		Owner: result.Run.LeaseOwner, Revision: result.Run.WorkRevision,
		Workspace: result.Run.WorkspacePath, Branch: branch, Head: head, LeaseExpiry: result.Run.LeaseExpiresAt,
		Packet: v7Packet(ctx.Project.VaultRoot, note, workSessionV7Index(ctx.Notes), "agent"),
		Next:   workSessionNext(*result.Run),
		Run:    result.Run, Authorization: result.Authorization,
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
	// A same-task holder is reclaimable only after both independent facts hold:
	// its heartbeat is past the reclaim grace and its recorded process identity
	// no longer exists.  Never let an expired timestamp alone steal a live
	// interactive workspace.
	if current, findErr := ctx.Store.FindRun(trackerRecordID(note)); findErr != nil {
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
		if latest, latestErr := ctx.Store.FindRun(trackerRecordID(note)); latestErr == nil && latest != nil {
			projected := *latest
			projected.LeaseState = string(LeaseStateUnclaimed)
			ctx.ProjectRuns[trackerRecordID(note)] = projected
		}
	}
	if blockers := workSessionAdmissionBlockers(note, workSessionV7Index(ctx.Notes), ctx.NotesByID, ctx.NotesByRecordID); len(blockers) > 0 {
		return runClaimResult{}, nil, workSessionStartBlocker(blockers[0])
	}
	run := ctx.effectiveRunForTask(note, automationResolveRunner(note, ctx.Workflow.Data))
	run.ProjectID = ctx.Project.ProjectID
	workspaceStrategy := workspaceStrategyForRun(ctx.Workflow.Data, ctx.Project, run, ctx.projectRunsSlice())
	branchName, branchBase, err := v7WorkspaceBranchForLane(ctx.Project.VaultRoot, note, run.Lane)
	if err != nil {
		return runClaimResult{}, nil, err
	}
	if branchName == "" && v7GitRepo(ctx.Project.RepoRoot) {
		branchName = v7TaskBranchName(trackerRecordID(note))
	}
	workspace, err := NewWorkspaceManager().Prepare(WorkspacePrepareRequest{ProjectID: ctx.Project.ProjectID, ProjectKey: ctx.Project.ProjectKey, RecordID: run.RecordID, ItemID: run.ItemID, BranchName: branchName, BranchBase: branchBase, RepoRoot: ctx.Project.RepoRoot, StateRoot: ctx.StateRoot, WorkspaceRoot: ctx.Workflow.Data.Workspace.Root, Strategy: workspaceStrategy, WorkRevision: run.WorkRevision, MaxLiveWorktrees: ctx.Workflow.Data.Workspace.MaxLiveWorktrees})
	if err != nil {
		return runClaimResult{}, nil, workSessionStartBlocker(workSessionUnsafeWorkspaceBlocker(stringField(note.Data, "id"), err.Error()))
	}
	run.WorkspacePath = workspace.Path
	owner := args.String("owner")
	service := newRunOwnershipService(ctx.Store)
	claimNotes := orchestrationOwnedPathNotes(ctx.NotesByID, ctx.Workflow.Data)
	service.withOwnedPathContext(ctx.Project.VaultRoot, claimNotes[stringField(note.Data, "id")], claimNotes)
	// Interactive work owns a user-directed session, not an unattended dispatch
	// slot. Same-task and owned-path safety stay in the ownership service.
	service.projectConcurrencyLimit = 0
	identity := runIdentityForClaim(run, ctx.Project.RepoRoot, run.WorkspacePath, string(workspaceStrategy), branchName)
	result, err := service.claimWorkSessionWithAuthorization(run, owner, RunAuthorization{Source: args.String("source"), Actor: owner, Trigger: "work_start", ProjectAutomationEnabled: ctx.Workflow.Data.AutomationEnabled}, identity)
	if err != nil {
		return runClaimResult{}, nil, err
	}
	if result.Claimed && result.Run != nil {
		_ = refreshStreamBoardForProject(ctx.Store, result.Run.ProjectID)
	}
	closeOnError = false
	return result, ctx, nil
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
	run, err := store.FindRun(id)
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

func requireWorkSessionRevision(args Args) error {
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		return err
	}
	defer store.Close()
	run, err := store.FindRun(args.String("id"))
	if err != nil {
		return err
	}
	if run == nil {
		return tuskerError(errorNotFound, "work session not found: "+args.String("id"))
	}
	if expected := intArg(args, "revision"); expected > 0 && expected != run.WorkRevision {
		return workSessionStaleRevisionError("CAS_CONFLICT", args.String("id"), expected, run.WorkRevision)
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
