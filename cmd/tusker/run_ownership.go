package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
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
	vaultPath               string
	candidateNote           Note
	notesByID               map[string]Note
}

type runClaimResult struct {
	OK            bool              `json:"ok"`
	Claimed       bool              `json:"claimed"`
	Run           *RunStatus        `json:"run,omitempty"`
	OwnerRun      *RunStatus        `json:"owner_run,omitempty"`
	Freshness     string            `json:"freshness,omitempty"`
	Authorization *RunAuthorization `json:"authorization,omitempty"`
}

// holderLiveness reports the liveness verdict named in a claim refusal and
// whether that holder still blocks the claim.  Lease age alone is not the
// verdict: an aged-out lease does not release the files its holder is still
// editing, so a stale lease whose process is provably alive keeps blocking and
// says so.  Only a holder that is both aged out and unprovable is treated as
// dead, which is the sole case lease self-heal may take over.
func holderLiveness(run RunStatus, now time.Time) (string, bool) {
	switch runFreshness(&run, now) {
	case "fresh":
		return "fresh", true
	case "stale":
		if run.ProcessPID > 0 && processIdentityMatches(run) {
			return "lease_expired_process_alive", true
		}
		return "dead", false
	default:
		return "released", false
	}
}

// ownedPathConflict is deliberately evaluated before the lease CAS.  A lease
// protects a task row; it does not protect the files that two tasks intend to
// edit.  Treating prefix paths as overlapping also catches a lane claiming a
// directory while another claims a file beneath it.
func ownedPathConflict(candidate Note, notes map[string]Note, runs []RunStatus, now time.Time) (map[string]any, bool) {
	wanted := normalizeOwnedPaths(normalizeList(candidate.Data["owned_paths"]))
	if len(wanted) == 0 {
		return nil, false
	}
	for _, run := range runs {
		liveness, blocking := holderLiveness(run, now)
		if run.ItemID == stringField(candidate.Data, "id") || !blocking {
			continue
		}
		holder, ok := notes[run.ItemID]
		if !ok {
			continue
		}
		for _, mine := range wanted {
			for _, theirs := range normalizeOwnedPaths(normalizeList(holder.Data["owned_paths"])) {
				if ownedPathsOverlap(mine, theirs) {
					age := "unknown"
					if started, err := time.Parse(time.RFC3339, firstNonEmpty(run.StartedAt, run.UpdatedAt)); err == nil {
						age = now.Sub(started).Round(time.Second).String()
					}
					return map[string]any{"code": "OWNED_PATH_CONFLICT", "holder": run.LeaseOwner, "task_id": run.ItemID, "lease_age": age, "liveness": liveness, "candidate_path": mine, "holder_path": theirs}, true
				}
			}
		}
	}
	return nil, false
}

func normalizeOwnedPaths(paths []string) []string {
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		path = strings.Trim(strings.TrimSpace(path), "/")
		if path != "" {
			out = append(out, path)
		}
	}
	return out
}

func orchestrationOwnedPathNotes(notes map[string]Note, wf Workflow) map[string]Note {
	out := make(map[string]Note, len(notes))
	for id, note := range notes {
		copyNote := note
		copyNote.Data = cloneMap(note.Data)
		if stringField(copyNote.Data, "work_kind") == "integrator" {
			copyNote.Data["owned_paths"] = uniqueStrings(append(normalizeList(copyNote.Data["owned_paths"]), wf.Orchestration.SharedNamespaces...))
		}
		out[id] = copyNote
	}
	return out
}

func ownedPathsOverlap(a, b string) bool {
	return a == b || strings.HasPrefix(a, b+"/") || strings.HasPrefix(b, a+"/")
}

func reclaimDeadOwnedPathHolders(store *RuntimeStore, vaultPath string, candidate Note, notes map[string]Note, runs []RunStatus, now time.Time) ([]string, error) {
	wanted := normalizeOwnedPaths(normalizeList(candidate.Data["owned_paths"]))
	if len(wanted) == 0 {
		return nil, nil
	}
	taken := []string{}
	for _, run := range runs {
		if run.ItemID == stringField(candidate.Data, "id") || runFreshness(&run, now) != "stale" {
			continue
		}
		if run.ProcessPID > 0 && processIdentityMatches(run) {
			continue
		}
		holder, ok := notes[run.ItemID]
		if !ok {
			continue
		}
		for _, mine := range wanted {
			for _, theirs := range normalizeOwnedPaths(normalizeList(holder.Data["owned_paths"])) {
				if !ownedPathsOverlap(mine, theirs) {
					continue
				}
				ok, err := store.ReclaimExpiredRunLease(run.ProjectID, run.RecordID, now.Add(2*defaultRunLeaseTTL), defaultRunLeaseTTL, "owned-path takeover after expired heartbeat")
				if err != nil {
					return nil, err
				}
				if ok {
					taken = append(taken, run.ItemID)
					if vaultPath != "" {
						_ = emitV7Event(vaultPath, stringField(candidate.Data, "id"), "task", "claimed", "tusker:claim", map[string]any{"takeover_from": run.ItemID, "dead_holder": run.LeaseOwner, "reason": "expired heartbeat and failed liveness probe", "candidate_path": mine, "holder_path": theirs})
					}
				}
			}
		}
	}
	return uniqueStrings(taken), nil
}

func newRunOwnershipService(store *RuntimeStore) *runOwnershipService {
	return &runOwnershipService{store: store, now: func() time.Time { return time.Now().UTC() }, projectConcurrencyLimit: 1}
}

func (s *runOwnershipService) withOwnedPathContext(vaultPath string, candidate Note, notes map[string]Note) *runOwnershipService {
	s.vaultPath, s.candidateNote, s.notesByID = vaultPath, candidate, notes
	return s
}

func (s *runOwnershipService) guardOwnedPathClaim() error {
	if len(s.notesByID) == 0 || stringField(s.candidateNote.Data, "id") == "" {
		return nil
	}
	runs, err := s.store.ListRuns()
	if err != nil {
		return err
	}
	now := s.now()
	if _, err := reclaimDeadOwnedPathHolders(s.store, s.vaultPath, s.candidateNote, s.notesByID, runs, now); err != nil {
		return err
	}
	runs, err = s.store.ListRuns()
	if err != nil {
		return err
	}
	if conflict, found := ownedPathConflict(s.candidateNote, s.notesByID, runs, now); found {
		return tuskerError("OWNED_PATH_CONFLICT", fmt.Sprintf("claim refused: %s holds %s for %s (lease age %s; liveness %s)", conflict["holder"], conflict["holder_path"], conflict["task_id"], conflict["lease_age"], conflict["liveness"]), withContext(conflict))
	}
	return nil
}

func (s *runOwnershipService) lockOwnedPathClaims() (func(), error) {
	if len(s.notesByID) == 0 {
		return func() {}, nil
	}
	lockDir := filepath.Join(s.store.stateRoot, "locks")
	if err := ensureDir(lockDir); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(filepath.Join(lockDir, "owned-path-claims.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX); err != nil {
		_ = file.Close()
		return nil, err
	}
	return func() { _ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN); _ = file.Close() }, nil
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
	unlock, err := s.lockOwnedPathClaims()
	if err != nil {
		return runClaimResult{}, err
	}
	defer unlock()
	if err := s.guardOwnedPathClaim(); err != nil {
		return runClaimResult{}, err
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
	claimed, err := s.store.ClaimRunLease(current.ProjectID, current.RecordID, owner, generation, defaultRunLeaseTTL, now, true, claimIsHandRun(), RuntimeLeaseClaimPrecondition{
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

// claimWorkSessionWithAuthorization is the only interactive claim entry.  In
// contrast with the older claim API, it atomically creates the lease, its
// authorization evidence, and a unique runtime attempt intent.
func (s *runOwnershipService) claimWorkSessionWithAuthorization(run RunStatus, owner string, auth RunAuthorization, identity RunIdentityMetadata) (runClaimResult, error) {
	if s == nil || s.store == nil {
		return runClaimResult{}, tuskerError(errorConfigInvalid, "run ownership store is unavailable")
	}
	if strings.TrimSpace(owner) == "" || run.ProjectID == "" || run.RecordID == "" || run.Runner == "" || run.WorkspacePath == "" {
		return runClaimResult{}, tuskerError(errorInvalidArg, "work start requires project, task, runner, workspace, and owner")
	}
	unlock, err := s.lockOwnedPathClaims()
	if err != nil {
		return runClaimResult{}, err
	}
	defer unlock()
	if err := s.guardOwnedPathClaim(); err != nil {
		return runClaimResult{}, err
	}
	if run.LeaseState == "" {
		run.LeaseState = string(LeaseStateUnclaimed)
	}
	if err := s.store.UpsertRunPreservingLease(run); err != nil {
		return runClaimResult{}, err
	}
	current, err := s.store.FindRun(run.RecordID)
	if err != nil || current == nil {
		return runClaimResult{}, firstNonNil(err, tuskerError(errorNotFound, "run not found after work-session preparation"))
	}
	now, generation := s.now(), current.LeaseGeneration+1
	identity.ProjectID, identity.RecordID = current.ProjectID, current.RecordID
	identity.WorkspacePath, identity.Runner = current.WorkspacePath, current.Runner
	attempt := RunAttempt{AttemptID: "work-" + newRecordID(), ProjectID: current.ProjectID, RecordID: current.RecordID, ItemID: current.ItemID, Runner: current.Runner, Lane: current.Lane, WorkRevision: current.WorkRevision, WorkspacePath: current.WorkspacePath, BranchName: identity.Branch, Outcome: string(AttemptOutcomeNone), StartedAt: now.Format(time.RFC3339Nano)}
	claimed, err := s.store.claimRunLeaseWithWorkSessionAttempt(*current, owner, generation, defaultRunLeaseTTL, now, RuntimeLeaseClaimPrecondition{ExpectedLeaseState: LeaseState(current.LeaseState), ExpectedOwner: current.LeaseOwner, ExpectedLeaseGeneration: current.LeaseGeneration, ExpectedWorkRevision: current.WorkRevision, ProjectConcurrencyLimit: s.projectConcurrencyLimit}, auth, attempt, identity)
	if err != nil {
		return runClaimResult{}, err
	}
	latest, err := s.store.FindRun(current.RecordID)
	if err != nil {
		return runClaimResult{}, err
	}
	if !claimed {
		existingAuth, _ := s.store.LatestRunAuthorization(current.ProjectID, current.RecordID)
		return runClaimResult{OK: false, OwnerRun: latest, Freshness: runFreshness(latest, now), Authorization: existingAuth}, nil
	}
	auth.ProjectID, auth.RecordID, auth.LeaseGeneration = current.ProjectID, current.RecordID, generation
	auth.Actor, auth.Source, auth.CreatedAt = firstNonEmpty(strings.TrimSpace(auth.Actor), owner), firstNonEmpty(strings.TrimSpace(auth.Source), "tusker_cli"), now.Format(time.RFC3339Nano)
	return runClaimResult{OK: true, Claimed: true, Run: latest, Freshness: "fresh", Authorization: &auth}, nil
}

// claimExistingWithAuthorization is the daemon path: the supplied row is the
// planning snapshot, so its CAS preconditions must not be refreshed after a
// concurrent operator mutation.
func (s *runOwnershipService) claimExistingWithAuthorization(run RunStatus, owner string, auth RunAuthorization) (runClaimResult, error) {
	if s == nil || s.store == nil {
		return runClaimResult{}, tuskerError(errorConfigInvalid, "run ownership store is unavailable")
	}
	unlock, err := s.lockOwnedPathClaims()
	if err != nil {
		return runClaimResult{}, err
	}
	defer unlock()
	if err := s.guardOwnedPathClaim(); err != nil {
		return runClaimResult{}, err
	}
	now := s.now()
	generation := run.LeaseGeneration + 1
	// The daemon claim path is, by definition, machine-dispatched: this claim was
	// not made by hand in a live session, so it stamps hand_run=false and clears
	// any hand-run origin left by an earlier hand claim of the same task.
	claimed, err := s.store.ClaimRunLease(run.ProjectID, run.RecordID, owner, generation, defaultRunLeaseTTL, now, true, false, RuntimeLeaseClaimPrecondition{
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

func (s *runOwnershipService) claimExistingWithDirective(run RunStatus, owner string, auth RunAuthorization, attempt RunAttempt) (runClaimResult, error) {
	if s == nil || s.store == nil {
		return runClaimResult{}, tuskerError(errorConfigInvalid, "run ownership store is unavailable")
	}
	unlock, err := s.lockOwnedPathClaims()
	if err != nil {
		return runClaimResult{}, err
	}
	defer unlock()
	if err := s.guardOwnedPathClaim(); err != nil {
		return runClaimResult{}, err
	}
	now := s.now()
	generation := run.LeaseGeneration + 1
	claimed, err := s.store.claimRunLeaseWithDirectiveAttempt(run, owner, generation, defaultRunLeaseTTL, now, RuntimeLeaseClaimPrecondition{
		ExpectedLeaseState: LeaseState(run.LeaseState), ExpectedOwner: run.LeaseOwner,
		ExpectedLeaseGeneration: run.LeaseGeneration, ExpectedWorkRevision: run.WorkRevision,
		ProjectConcurrencyLimit: s.projectConcurrencyLimit,
	}, auth, attempt)
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
	auth.Actor, auth.Source, auth.CreatedAt = firstNonEmpty(auth.Actor, owner), firstNonEmpty(auth.Source, "human_run_directive"), now.Format(time.RFC3339Nano)
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
	return s.finishWithEndState(identity, owner, outcome, summary, verification, reason, nil)
}

func (s *runOwnershipService) finishWithEndState(identity, owner string, outcome AttemptOutcome, summary, verification, reason string, endState *RunEndState) (*RunStatus, error) {
	run, err := s.ownedRun(identity, owner)
	if err != nil {
		return nil, err
	}
	if outcome == AttemptOutcomeSucceeded && (strings.TrimSpace(summary) == "" || strings.TrimSpace(verification) == "") {
		return nil, tuskerError(errorInvalidArg, "successful submission requires deliverable and acceptance-mapped verification summaries")
	}
	// A missing end state is the same defect as an incomplete one: an absent
	// record is exactly how a lane reports success with no branch, no SHA and
	// no verdicts.  Refuse both, naming every field the submitter still owes.
	if outcome == AttemptOutcomeSucceeded {
		state := RunEndState{}
		if endState != nil {
			state = *endState
		}
		if missing := missingRunEndStateFields(state); len(missing) > 0 {
			return nil, tuskerError("END_STATE_REQUIRED", "run submission is missing end-state fields: "+strings.Join(missing, ", "), withContext(map[string]any{"missing_fields": missing}))
		}
	}
	now := s.now().Format(time.RFC3339)
	attemptID := firstNonEmpty(run.ActiveAttemptID, owner)
	attempt := RunAttempt{AttemptID: attemptID, ProjectID: run.ProjectID, RecordID: run.RecordID, ItemID: run.ItemID, Runner: run.Runner, Lane: run.Lane, WorkRevision: run.WorkRevision, WorkspacePath: run.WorkspacePath, SessionRef: run.SessionRef, Outcome: string(outcome), FinalSummary: summary, LastError: reason, StartedAt: run.StartedAt, FinishedAt: now}
	if endState != nil {
		attempt.EndState = *endState
		attempt.BranchName = endState.Branch
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
	// The outcome and release are one ownership transition.  Persisting the
	// attempt first used to leave a stale terminal attempt behind if a reclaim
	// won the lease before the separate release CAS.
	if ok, err := s.store.FinalizeRunLease(*run, attempt, owner, run.LeaseGeneration); err != nil || !ok {
		return nil, firstNonNil(err, tuskerError("CAS_CONFLICT", "run ownership changed before terminal handoff"))
	}
	return s.store.FindRun(identity)
}

func captureRunEndState(workspace, gateVerdicts, reportedBranch, reportedSHA string, now time.Time) (RunEndState, error) {
	verdicts, err := parseGateVerdicts(gateVerdicts)
	if err != nil {
		return RunEndState{}, err
	}
	if len(verdicts) == 0 {
		return RunEndState{}, tuskerError("END_STATE_REQUIRED", "run submission is missing end-state fields: gate_verdicts", withContext(map[string]any{"missing_fields": []string{"gate_verdicts"}}))
	}
	facts, err := captureGitBranchFacts(workspace, "", now)
	if err != nil {
		return RunEndState{}, tuskerError("END_STATE_CAPTURE_FAILED", "cannot capture authoritative workspace end state: "+err.Error(), withContext(map[string]any{"worktree_path": workspace}))
	}
	state := RunEndState{Schema: "tusker.run-end-state/v1", Branch: facts.Branch, HeadSHA: facts.Head, WorktreePath: workspace, Dirty: facts.Dirty, GateVerdicts: verdicts, ReportedBranch: strings.TrimSpace(reportedBranch), ReportedHeadSHA: strings.TrimSpace(reportedSHA), CapturedAt: now.UTC().Format(time.RFC3339)}
	if state.ReportedBranch != "" && state.ReportedBranch != state.Branch {
		state.Discrepancies = append(state.Discrepancies, fmt.Sprintf("reported branch %s differs from harness branch %s", state.ReportedBranch, state.Branch))
	}
	if state.ReportedHeadSHA != "" && state.ReportedHeadSHA != state.HeadSHA {
		state.Discrepancies = append(state.Discrepancies, fmt.Sprintf("reported HEAD %s differs from harness HEAD %s", state.ReportedHeadSHA, state.HeadSHA))
	}
	return state, nil
}

func parseGateVerdicts(raw string) (map[string]string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	result := map[string]string{}
	if strings.HasPrefix(raw, "{") {
		if err := json.Unmarshal([]byte(raw), &result); err != nil {
			return nil, tuskerError(errorInvalidArg, "--gate-verdicts must be JSON or comma-separated ID=verdict pairs")
		}
	} else {
		for _, pair := range strings.Split(raw, ",") {
			parts := strings.SplitN(strings.TrimSpace(pair), "=", 2)
			if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
				return nil, tuskerError(errorInvalidArg, "--gate-verdicts must be JSON or comma-separated ID=verdict pairs")
			}
			result[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
		}
	}
	return result, nil
}

func gateVerdictsFromTask(note Note) map[string]string {
	verdicts := map[string]string{}
	for _, row := range parseV7VerificationRows(note.Body) {
		if strings.TrimSpace(row.CoverText) == "" || strings.EqualFold(row.Result, "pending") {
			continue
		}
		verdicts[row.CoverText] = row.Result
	}
	return verdicts
}

func saveAttemptEndStateForRun(store *RuntimeStore, run RunStatus, state RunEndState) error {
	attempts, err := store.ListAttemptsForRun(run.ProjectID, run.RecordID)
	if err != nil {
		return err
	}
	for _, attempt := range attempts {
		if attempt.AttemptID != run.ActiveAttemptID {
			continue
		}
		attempt.EndState = state
		attempt.EndStateJSON = ""
		attempt.BranchName = state.Branch
		return store.SaveAttempt(attempt)
	}
	return tuskerError(errorNotFound, "active attempt missing while recording end state: "+run.ActiveAttemptID)
}

func missingRunEndStateFields(state RunEndState) []string {
	missing := []string{}
	if strings.TrimSpace(state.Branch) == "" {
		missing = append(missing, "branch")
	}
	if strings.TrimSpace(state.HeadSHA) == "" {
		missing = append(missing, "head_sha")
	}
	if strings.TrimSpace(state.WorktreePath) == "" {
		missing = append(missing, "worktree_path")
	}
	if len(state.GateVerdicts) == 0 {
		missing = append(missing, "gate_verdicts")
	}
	return missing
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
	// Compatibility surface: do not create a second notion of an active run.
	return workSessionStartCmd(args)
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
		current, findErr := store.FindRun(id)
		if findErr != nil || current == nil {
			return firstNonNil(findErr, tuskerError(errorNotFound, "run not found: "+id))
		}
		endState, captureErr := captureRunEndState(current.WorkspacePath, firstNonEmpty(args.String("gate-verdicts"), args.String("gates")), args.String("branch"), firstNonEmpty(args.String("head-sha"), args.String("sha")), time.Now().UTC())
		if captureErr != nil {
			return captureErr
		}
		run, err = service.finishWithEndState(id, owner, AttemptOutcomeSucceeded, args.String("deliverable"), args.String("verification"), "", &endState)
	case "fail":
		run, err = service.finish(id, owner, AttemptOutcomeFailed, "", "", firstNonEmpty(args.String("reason"), "run failed"))
	case "release":
		run, err = service.finish(id, owner, AttemptOutcomeInterrupted, "", "", firstNonEmpty(args.String("reason"), "work session released"))
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
	_ = refreshStreamBoardForProject(store, run.ProjectID)
	if action == "submit" {
		statusArgs := Args{"id": run.ItemID, "status": "review", "actor": firstNonEmpty(args.String("actor"), owner), "reason": "normalized run submission", "normalized-work-submit": "true", "lease-generation": fmt.Sprintf("%d", run.LeaseGeneration)}
		loaded, projectErr := loadRegisteredProjects(store, registeredProjectLoadOptions{LoadDisabled: true, ProjectID: run.ProjectID})
		if projectErr != nil {
			return projectErr
		}
		if len(loaded) != 1 || loaded[0].LoadError != nil {
			return tuskerError(errorNotFound, "registered project for submitted run was not found: "+run.ProjectID)
		}
		statusArgs["vault"] = loaded[0].Project.VaultRoot
		if err := statusCmd(statusArgs); err != nil {
			return err
		}
	}
	emitJSON(map[string]any{"ok": true, "action": action, "run": run})
	return nil
}
