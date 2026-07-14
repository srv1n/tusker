package main

import (
	"fmt"
	"os/exec"
	"strings"
	"time"
)

func runIdentityForClaim(run RunStatus, repoRoot, workspace, mode, branch string) RunIdentityMetadata {
	head := ""
	if output, err := exec.Command("git", "-C", workspace, "rev-parse", "HEAD").Output(); err == nil {
		head = strings.TrimSpace(string(output))
	}
	if branch == "" {
		if output, err := exec.Command("git", "-C", workspace, "branch", "--show-current").Output(); err == nil {
			branch = strings.TrimSpace(string(output))
		}
	}
	return RunIdentityMetadata{ProjectID: run.ProjectID, RecordID: run.RecordID, RepoRoot: repoRoot, WorkspacePath: workspace, WorkspaceMode: mode, Runner: run.Runner, Branch: branch, Head: head}
}

type runOwnershipService struct {
	store                   *RuntimeStore
	now                     func() time.Time
	projectConcurrencyLimit int
}

type runClaimResult struct {
	OK            bool              `json:"ok"`
	Claimed       bool              `json:"claimed"`
	Run           *RunStatus        `json:"run,omitempty"`
	OwnerRun      *RunStatus        `json:"owner_run,omitempty"`
	Freshness     string            `json:"freshness,omitempty"`
	Authorization *RunAuthorization `json:"authorization,omitempty"`
}

func newRunOwnershipService(store *RuntimeStore) *runOwnershipService {
	return &runOwnershipService{store: store, now: func() time.Time { return time.Now().UTC() }, projectConcurrencyLimit: 1}
}

func (s *runOwnershipService) claim(run RunStatus, owner string) (runClaimResult, error) {
	return s.claimWithAuthorization(run, owner, RunAuthorization{Source: "tusker_cli", Actor: owner, Trigger: "manual_claim"})
}

func (s *runOwnershipService) claimWithAuthorization(run RunStatus, owner string, auth RunAuthorization) (runClaimResult, error) {
	if s == nil || s.store == nil {
		return runClaimResult{}, tuskerError(errorConfigInvalid, "run ownership store is unavailable")
	}
	owner = strings.TrimSpace(owner)
	if owner == "" {
		owner = newRecordID()
	}
	if run.ProjectID == "" || run.RecordID == "" || run.Runner == "" || run.WorkspacePath == "" {
		return runClaimResult{}, tuskerError(errorInvalidArg, "claim requires project, task, runner, and workspace")
	}
	if run.LeaseState == "" {
		run.LeaseState = string(LeaseStateUnclaimed)
	}
	if err := s.store.UpsertRunPreservingLease(run); err != nil {
		return runClaimResult{}, err
	}
	current, err := s.store.FindRun(run.RecordID)
	if err != nil || current == nil {
		return runClaimResult{}, firstNonNil(err, tuskerError(errorNotFound, "run not found after claim preparation"))
	}
	now := s.now()
	generation := current.LeaseGeneration + 1
	claimed, err := s.store.ClaimRunLease(current.ProjectID, current.RecordID, owner, generation, defaultRunLeaseTTL, now, true, RuntimeLeaseClaimPrecondition{
		ExpectedLeaseState: LeaseState(current.LeaseState), ExpectedOwner: current.LeaseOwner,
		ExpectedLeaseGeneration: current.LeaseGeneration, ExpectedWorkRevision: current.WorkRevision,
		ProjectConcurrencyLimit: s.projectConcurrencyLimit,
	})
	if err != nil {
		return runClaimResult{}, err
	}
	latest, err := s.store.FindRun(current.RecordID)
	if err != nil {
		return runClaimResult{}, err
	}
	if !claimed {
		existingAuth, _ := s.store.LatestRunAuthorization(current.ProjectID, current.RecordID)
		return runClaimResult{OK: false, Claimed: false, OwnerRun: latest, Freshness: runFreshness(latest, now), Authorization: existingAuth}, nil
	}
	auth.ProjectID, auth.RecordID, auth.LeaseGeneration = current.ProjectID, current.RecordID, generation
	auth.Actor = firstNonEmpty(strings.TrimSpace(auth.Actor), owner)
	auth.Source = firstNonEmpty(strings.TrimSpace(auth.Source), "tusker_cli")
	auth.CreatedAt = now.Format(time.RFC3339)
	if err := s.store.SaveRunAuthorization(auth); err != nil {
		return runClaimResult{}, err
	}
	return runClaimResult{OK: true, Claimed: true, Run: latest, Freshness: "fresh", Authorization: &auth}, nil
}

// claimExistingWithAuthorization is the daemon path: the supplied row is the
// planning snapshot, so its CAS preconditions must not be refreshed after a
// concurrent operator mutation.
func (s *runOwnershipService) claimExistingWithAuthorization(run RunStatus, owner string, auth RunAuthorization) (runClaimResult, error) {
	if s == nil || s.store == nil {
		return runClaimResult{}, tuskerError(errorConfigInvalid, "run ownership store is unavailable")
	}
	now := s.now()
	generation := run.LeaseGeneration + 1
	claimed, err := s.store.ClaimRunLease(run.ProjectID, run.RecordID, owner, generation, defaultRunLeaseTTL, now, true, RuntimeLeaseClaimPrecondition{
		ExpectedLeaseState: LeaseState(run.LeaseState), ExpectedOwner: run.LeaseOwner,
		ExpectedLeaseGeneration: run.LeaseGeneration, ExpectedWorkRevision: run.WorkRevision,
		ProjectConcurrencyLimit: s.projectConcurrencyLimit,
	})
	if err != nil {
		return runClaimResult{}, err
	}
	latest, err := s.store.FindRun(run.RecordID)
	if err != nil {
		return runClaimResult{}, err
	}
	if !claimed {
		return runClaimResult{OwnerRun: latest, Freshness: runFreshness(latest, now)}, nil
	}
	auth.ProjectID, auth.RecordID, auth.LeaseGeneration = run.ProjectID, run.RecordID, generation
	auth.Actor, auth.Source, auth.CreatedAt = firstNonEmpty(auth.Actor, owner), firstNonEmpty(auth.Source, "daemon_auto"), now.Format(time.RFC3339)
	if err := s.store.SaveRunAuthorization(auth); err != nil {
		return runClaimResult{}, err
	}
	return runClaimResult{OK: true, Claimed: true, Run: latest, Freshness: "fresh", Authorization: &auth}, nil
}

func firstNonNil(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

func runFreshness(run *RunStatus, now time.Time) string {
	if run == nil {
		return "missing"
	}
	if LeaseState(run.LeaseState) != LeaseStateClaimed && LeaseState(run.LeaseState) != LeaseStateRunning {
		return "released"
	}
	expires, err := time.Parse(time.RFC3339, run.LeaseExpiresAt)
	if err != nil || !expires.After(now) {
		return "stale"
	}
	return "fresh"
}

func (s *runOwnershipService) start(identity, owner, session string, pid, pgid int) (*RunStatus, error) {
	run, err := s.ownedRun(identity, owner)
	if err != nil {
		return nil, err
	}
	now := s.now()
	ok, err := s.store.RenewRunLease(RuntimeLeaseRenewal{ProjectID: run.ProjectID, RecordID: run.RecordID, Owner: owner, Generation: run.LeaseGeneration, TTL: defaultRunLeaseTTL, Now: now, Dispatchable: true, ProcessPID: pid, ProcessPGID: pgid, ProcessStarted: now.Format(time.RFC3339)})
	if err != nil || !ok {
		return nil, firstNonNil(err, tuskerError("CAS_CONFLICT", "run ownership changed before start"))
	}
	run, _ = s.store.FindRun(identity)
	if session != "" {
		run.SessionRef = session
		run.UpdatedAt = now.Format(time.RFC3339)
		if ok, err := s.store.UpdateRunIfLease(*run, owner, run.LeaseGeneration); err != nil || !ok {
			return nil, firstNonNil(err, tuskerError("CAS_CONFLICT", "run ownership changed before session attach"))
		}
		_ = s.store.SaveSession(RunnerSession{ProjectID: run.ProjectID, RecordID: run.RecordID, Runner: run.Runner, SessionRef: session, WorkspacePath: run.WorkspacePath, CurrentItemID: run.ItemID, WorkRevision: run.WorkRevision, LastAttemptID: run.ActiveAttemptID, State: "open", Resumable: true, StartedAt: now.Format(time.RFC3339), LastSeenAt: now.Format(time.RFC3339)})
	}
	return s.store.FindRun(identity)
}

func (s *runOwnershipService) heartbeat(identity, owner string) (*RunStatus, error) {
	run, err := s.ownedRun(identity, owner)
	if err != nil {
		return nil, err
	}
	ok, err := s.store.RenewRunLease(RuntimeLeaseRenewal{ProjectID: run.ProjectID, RecordID: run.RecordID, Owner: owner, Generation: run.LeaseGeneration, TTL: defaultRunLeaseTTL, Now: s.now(), Dispatchable: true})
	if err != nil || !ok {
		return nil, firstNonNil(err, tuskerError("CAS_CONFLICT", "run ownership changed before heartbeat"))
	}
	return s.store.FindRun(identity)
}

func (s *runOwnershipService) finish(identity, owner string, outcome AttemptOutcome, summary, verification, reason string) (*RunStatus, error) {
	run, err := s.ownedRun(identity, owner)
	if err != nil {
		return nil, err
	}
	if outcome == AttemptOutcomeSucceeded && (strings.TrimSpace(summary) == "" || strings.TrimSpace(verification) == "") {
		return nil, tuskerError(errorInvalidArg, "successful submission requires deliverable and acceptance-mapped verification summaries")
	}
	now := s.now().Format(time.RFC3339)
	attemptID := firstNonEmpty(run.ActiveAttemptID, owner)
	attempt := RunAttempt{AttemptID: attemptID, ProjectID: run.ProjectID, RecordID: run.RecordID, ItemID: run.ItemID, Runner: run.Runner, Lane: run.Lane, WorkRevision: run.WorkRevision, WorkspacePath: run.WorkspacePath, SessionRef: run.SessionRef, Outcome: string(outcome), FinalSummary: summary, LastError: reason, StartedAt: run.StartedAt, FinishedAt: now}
	if ok, err := s.store.SaveAttemptIfRunLease(attempt, owner, run.LeaseGeneration); err != nil || !ok {
		return nil, firstNonNil(err, tuskerError("CAS_CONFLICT", "run ownership changed before outcome write"))
	}
	run.LeaseState = string(LeaseStateReleased)
	run.LeaseOwner = ""
	run.LeaseExpiresAt = ""
	run.AttemptOutcome = string(outcome)
	run.ActiveAttemptID = ""
	run.FinalSummary = summary
	run.LogsSummary = verification
	run.LastError = reason
	run.LastEventAt = now
	run.UpdatedAt = now
	run.ProcessPID, run.ProcessPGID, run.ProcessStartedAt = 0, 0, ""
	if ok, err := s.store.UpdateRunIfLease(*run, owner, run.LeaseGeneration); err != nil || !ok {
		return nil, firstNonNil(err, tuskerError("CAS_CONFLICT", "run ownership changed before release"))
	}
	return s.store.FindRun(identity)
}

func (s *runOwnershipService) ownedRun(identity, owner string) (*RunStatus, error) {
	run, err := s.store.FindRun(identity)
	if err != nil {
		return nil, err
	}
	if run == nil {
		return nil, tuskerError(errorNotFound, "run not found: "+identity)
	}
	if run.LeaseOwner != owner || (LeaseState(run.LeaseState) != LeaseStateClaimed && LeaseState(run.LeaseState) != LeaseStateRunning) {
		return nil, tuskerError("CAS_CONFLICT", fmt.Sprintf("run is owned by %s", firstNonEmpty(run.LeaseOwner, "nobody")))
	}
	return run, nil
}

func runsClaimCmd(args Args) error {
	id, err := requireArg(args, "id")
	if err != nil {
		return err
	}
	ctx, err := loadAutomationCommandContext(args)
	if err != nil {
		return err
	}
	defer ctx.Close()
	note, err := ctx.findTask(id)
	if err != nil {
		return err
	}
	explain := ctx.explainTask(note)
	if !explain.Dispatchable {
		return tuskerError("CAS_CONFLICT", "claim blocked: "+strings.Join(explain.Blockers, "; "))
	}
	run := ctx.effectiveRunForTask(note, explain.Runner)
	run.ProjectID, run.Runner, run.WorkspacePath = ctx.Project.ProjectID, explain.Runner, explain.WorkspacePath
	owner := firstNonEmpty(args.String("owner"), args.String("actor"))
	source := firstNonEmpty(args.String("source"), "tusker_cli")
	if source != "tusker_cli" && source != "codex" && source != "claude" {
		return tuskerError(errorInvalidArg, "manual claim source must be tusker_cli, codex, or claude")
	}
	service := newRunOwnershipService(ctx.Store)
	service.projectConcurrencyLimit = ctx.Workflow.Data.Runtime.MaxActiveRunsPerProject
	result, err := service.claimWithAuthorization(run, owner, RunAuthorization{Source: source, Actor: owner, Trigger: firstNonEmpty(args.String("trigger"), "manual_claim"), ProjectAutomationEnabled: ctx.Workflow.Data.AutomationEnabled})
	if err != nil {
		return err
	}
	if result.Claimed {
		identity := runIdentityForClaim(*result.Run, ctx.Project.RepoRoot, result.Run.WorkspacePath, string(workspaceStrategyFromWorkflow(ctx.Workflow.Data.Workspace.Strategy)), "")
		if err := ctx.Store.SaveRunIdentity(identity); err != nil {
			return err
		}
	}
	emitJSON(result)
	if !result.Claimed {
		return tuskerError("CAS_CONFLICT", "task already has a live owner")
	}
	return nil
}

func runsLifecycleCmd(args Args, action string) error {
	id, err := requireArg(args, "id")
	if err != nil {
		return err
	}
	owner, err := requireArg(args, "owner")
	if err != nil {
		return err
	}
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		return err
	}
	defer store.Close()
	service := newRunOwnershipService(store)
	var run *RunStatus
	switch action {
	case "start":
		run, err = service.start(id, owner, args.String("session"), intArg(args, "pid"), intArg(args, "pgid"))
	case "heartbeat":
		run, err = service.heartbeat(id, owner)
	case "submit":
		run, err = service.finish(id, owner, AttemptOutcomeSucceeded, args.String("deliverable"), args.String("verification"), "")
	case "fail":
		run, err = service.finish(id, owner, AttemptOutcomeFailed, "", "", firstNonEmpty(args.String("reason"), "run failed"))
	case "reclaim":
		current, findErr := store.FindRun(id)
		if findErr != nil || current == nil {
			return firstNonNil(findErr, tuskerError(errorNotFound, "run not found: "+id))
		}
		ok, reclaimErr := store.ReclaimExpiredRunLease(current.ProjectID, current.RecordID, time.Now().UTC(), defaultRunLeaseTTL, args.String("reason"))
		err = reclaimErr
		if err == nil && !ok {
			err = tuskerError("CAS_CONFLICT", "run is not safely reclaimable")
		}
		run, _ = store.FindRun(id)
	}
	if err != nil {
		return err
	}
	if action == "submit" {
		statusArgs := Args{"id": run.ItemID, "status": "review", "actor": firstNonEmpty(args.String("actor"), owner), "reason": "normalized run submission"}
		if err := statusCmd(statusArgs); err != nil {
			return err
		}
	}
	emitJSON(map[string]any{"ok": true, "action": action, "run": run})
	return nil
}
