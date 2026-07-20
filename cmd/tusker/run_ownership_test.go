package main

import (
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestClaimOwnedPathConflict(t *testing.T) {
	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	candidate := Note{Data: map[string]any{"id": "APP-T-0002", "owned_paths": []string{"migrations/0014_add.sql"}}}
	notes := map[string]Note{"APP-T-0001": {Data: map[string]any{"id": "APP-T-0001", "owned_paths": []string{"migrations"}}}}
	runs := []RunStatus{{ItemID: "APP-T-0001", LeaseOwner: "lane-a", LeaseState: string(LeaseStateRunning), LeaseExpiresAt: now.Add(time.Minute).Format(time.RFC3339), StartedAt: now.Add(-time.Minute).Format(time.RFC3339)}}
	conflict, found := ownedPathConflict(candidate, notes, runs, now)
	if !found || conflict["task_id"] != "APP-T-0001" || conflict["liveness"] != "fresh" {
		t.Fatalf("unexpected conflict: %#v, %v", conflict, found)
	}
	if _, found := ownedPathConflict(candidate, notes, []RunStatus{{ItemID: "APP-T-0001", LeaseState: string(LeaseStateRunning), LeaseExpiresAt: now.Add(-time.Minute).Format(time.RFC3339)}}, now); found {
		t.Fatal("expired holder must not block a claim")
	}
}

// ownedPathClaimFixture wires a live holder and a candidate through the real
// claim entry point, because the incident this guards against was two lanes
// claiming successfully — not a helper returning the wrong verdict.
func ownedPathClaimFixture(t *testing.T, candidateID, holderID string, candidatePaths, holderPaths []string, mutate func(*RunStatus, time.Time)) (*RuntimeStore, RunStatus, *runOwnershipService) {
	t.Helper()
	store, candidateRun := ownershipStoreFixture(t, candidateID)
	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	holder := candidateRun
	holder.RecordID, holder.ItemID = holderID, holderID
	holder.LeaseState, holder.LeaseOwner = string(LeaseStateRunning), "lane-a"
	holder.LeaseExpiresAt = now.Add(time.Hour).Format(time.RFC3339)
	holder.StartedAt = now.Add(-time.Minute).Format(time.RFC3339)
	if mutate != nil {
		mutate(&holder, now)
	}
	if err := store.UpsertRun(holder); err != nil {
		t.Fatal(err)
	}
	candidate := Note{Data: map[string]any{"id": candidateID, "owned_paths": candidatePaths}}
	notes := map[string]Note{holderID: {Data: map[string]any{"id": holderID, "owned_paths": holderPaths}}, candidateID: candidate}
	service := newRunOwnershipService(store).withOwnedPathContext("", candidate, notes)
	service.now = func() time.Time { return now }
	service.projectConcurrencyLimit = 2
	return store, candidateRun, service
}

func TestClaimDisjointOwnedPathsSucceeds(t *testing.T) {
	_, candidateRun, service := ownedPathClaimFixture(t, "APP-T-DISJOINT-2", "APP-T-DISJOINT-1", []string{"cmd/tusker/run_ownership.go"}, []string{"internal/v7schema"}, nil)
	result, err := service.claim(candidateRun, "lane-b")
	if err != nil || !result.Claimed {
		t.Fatalf("disjoint claim was refused: %#v %v", result, err)
	}
}

// A lease that aged out does not release the files its holder is editing.  If
// the holder's process is provably alive, the claim is still a collision and
// must be refused with that liveness verdict instead of quietly proceeding.
func TestClaimStaleLeaseWithLiveProcessRefused(t *testing.T) {
	store, candidateRun, service := ownedPathClaimFixture(t, "APP-T-LIVE-2", "APP-T-LIVE-1", []string{"migrations/0014.sql"}, []string{"migrations"}, func(holder *RunStatus, now time.Time) {
		holder.LeaseExpiresAt = now.Add(-time.Minute).Format(time.RFC3339)
		holder.ProcessPID = os.Getpid()
	})
	result, err := service.claim(candidateRun, "lane-b")
	if err == nil || result.Claimed {
		t.Fatalf("live holder with an expired lease did not block the claim: %#v %v", result, err)
	}
	var typed *TuskerError
	if !errors.As(err, &typed) || typed.Code != "OWNED_PATH_CONFLICT" || !strings.Contains(typed.Message, "lease_expired_process_alive") {
		t.Fatalf("liveness verdict missing from refusal: %v", err)
	}
	holder, _ := store.FindRun("APP-T-LIVE-1")
	if holder.LeaseState != string(LeaseStateRunning) {
		t.Fatalf("live holder was taken over: %#v", holder)
	}
}

func TestConcurrentRunClaimExactlyOneOwner(t *testing.T) {
	store, run := ownershipStoreFixture(t, "APP-T-0001")
	service := newRunOwnershipService(store)
	service.now = func() time.Time { return time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC) }

	const contenders = 20
	results := make(chan runClaimResult, contenders)
	errs := make(chan error, contenders)
	var wg sync.WaitGroup
	for i := 0; i < contenders; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			result, err := service.claim(run, newRecordID())
			results <- result
			errs <- err
		}(i)
	}
	wg.Wait()
	close(results)
	close(errs)

	wins := 0
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	for result := range results {
		if result.Claimed {
			wins++
		} else if result.OwnerRun == nil || result.OwnerRun.LeaseOwner == "" || result.Freshness != "fresh" {
			t.Fatalf("loser did not receive fresh owner metadata: %#v", result)
		}
	}
	assertEqual(t, 1, wins, "exactly one claim winner")
}

func TestRunClaimStartHeartbeatSubmitFailInterruptReclaim(t *testing.T) {
	store, run := ownershipStoreFixture(t, "APP-T-0002")
	service := newRunOwnershipService(store)
	now := time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }

	claimed, err := service.claim(run, "actor-1")
	if err != nil || !claimed.Claimed {
		t.Fatalf("claim: %#v %v", claimed, err)
	}
	started, err := service.start(run.RecordID, "actor-1", "session-1", 123, 123)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, string(LeaseStateRunning), started.LeaseState, "started lease")
	assertEqual(t, "session-1", started.SessionRef, "attached session")
	now = now.Add(time.Minute)
	if _, err := service.heartbeat(run.RecordID, "actor-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.finish(run.RecordID, "actor-1", AttemptOutcomeSucceeded, "", "A1 pass", ""); err == nil {
		t.Fatal("submission without deliverable must fail")
	}
	submitted, err := service.finish(run.RecordID, "actor-1", AttemptOutcomeSucceeded, "diff: cmd/tusker", "A1,A2: pass", "")
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, string(LeaseStateReleased), submitted.LeaseState, "submitted release")
	assertEqual(t, string(AttemptOutcomeSucceeded), submitted.AttemptOutcome, "submitted outcome")

	failRun := run
	failRun.RecordID, failRun.ItemID = "APP-T-0003", "APP-T-0003"
	if err := store.UpsertRun(failRun); err != nil {
		t.Fatal(err)
	}
	if result, err := service.claim(failRun, "actor-2"); err != nil || !result.Claimed {
		t.Fatalf("fail claim: %#v %v", result, err)
	}
	failed, err := service.finish(failRun.RecordID, "actor-2", AttemptOutcomeFailed, "", "", "forced failure")
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, string(AttemptOutcomeFailed), failed.AttemptOutcome, "failed outcome")

	stale := run
	stale.RecordID, stale.ItemID = "APP-T-0004", "APP-T-0004"
	if err := store.UpsertRun(stale); err != nil {
		t.Fatal(err)
	}
	if result, err := service.claim(stale, "actor-stale"); err != nil || !result.Claimed {
		t.Fatalf("stale claim: %#v %v", result, err)
	}
	reclaimAt := now.Add(4 * defaultRunLeaseTTL)
	reclaimed, err := store.ReclaimExpiredRunLease(stale.ProjectID, stale.RecordID, reclaimAt, defaultRunLeaseTTL, "heartbeat expired")
	if err != nil || !reclaimed {
		t.Fatalf("reclaim: %v %v", reclaimed, err)
	}
	latest, _ := store.FindRun(stale.RecordID)
	assertEqual(t, string(LeaseStateInterrupted), latest.LeaseState, "reclaimed state")
	assertEqual(t, string(AttemptOutcomeInterrupted), latest.AttemptOutcome, "reclaimed outcome")
}

func TestHeartbeatLeaseExpiryStaleReclaimPreservesAttempt(t *testing.T) {
	store, run := ownershipStoreFixture(t, "APP-T-0005")
	service := newRunOwnershipService(store)
	now := time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }
	result, err := service.claim(run, "owner")
	if err != nil || !result.Claimed {
		t.Fatalf("claim: %#v %v", result, err)
	}
	if result.Run.LastHeartbeatAt == "" || result.Run.LeaseExpiresAt == "" {
		t.Fatalf("claim lacks liveness timestamps: %#v", result.Run)
	}
	if err := store.SaveAttempt(RunAttempt{AttemptID: "prior-attempt", ProjectID: run.ProjectID, RecordID: run.RecordID, ItemID: run.ItemID, Runner: run.Runner, Outcome: string(AttemptOutcomeFailed)}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.heartbeat(run.RecordID, "foreign"); err == nil {
		t.Fatal("foreign heartbeat must fail")
	}
	now = now.Add(time.Minute)
	first, err := service.heartbeat(run.RecordID, "owner")
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.heartbeat(run.RecordID, "owner")
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, first.LeaseExpiresAt, second.LeaseExpiresAt, "idempotent heartbeat")
	reclaimAt := now.Add(4 * defaultRunLeaseTTL)
	if ok, err := store.ReclaimExpiredRunLease(run.ProjectID, run.RecordID, reclaimAt, defaultRunLeaseTTL, "running heartbeat expired"); err != nil || !ok {
		t.Fatalf("reclaim: %v %v", ok, err)
	}
	if ok, err := store.ReclaimExpiredRunLease(run.ProjectID, run.RecordID, reclaimAt, defaultRunLeaseTTL, "again"); err != nil || ok {
		t.Fatalf("second reclaim must be no-op: %v %v", ok, err)
	}
	attempts, err := store.ListAttemptsForRun(run.ProjectID, run.RecordID)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, 1, len(attempts), "reclaim preserves prior attempt")
	if _, err := service.heartbeat(run.RecordID, "owner"); err == nil {
		t.Fatal("terminal heartbeat must fail")
	}
}

func TestHeartbeatCompletionRaceSingleTerminalOutcome(t *testing.T) {
	store, run := ownershipStoreFixture(t, "APP-T-0006")
	service := newRunOwnershipService(store)
	if result, err := service.claim(run, "owner"); err != nil || !result.Claimed {
		t.Fatalf("claim: %#v %v", result, err)
	}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); _, _ = service.heartbeat(run.RecordID, "owner") }()
	go func() {
		defer wg.Done()
		_, _ = service.finish(run.RecordID, "owner", AttemptOutcomeSucceeded, "diff", "A1 pass", "")
	}()
	wg.Wait()
	latest, err := store.FindRun(run.RecordID)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, string(LeaseStateReleased), latest.LeaseState, "one terminal release")
	assertEqual(t, string(AttemptOutcomeSucceeded), latest.AttemptOutcome, "one terminal outcome")
	attempts, err := store.ListAttemptsForRun(run.ProjectID, run.RecordID)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, 1, len(attempts), "one terminal attempt")
}

func TestAutomationManualAuthorizationClaim(t *testing.T) {
	for i, source := range []string{"daemon_auto", "tusker_cli", "codex", "claude"} {
		store, run := ownershipStoreFixture(t, "APP-T-AUTH-"+source)
		service := newRunOwnershipService(store)
		auth := RunAuthorization{Source: source, Actor: "actor-" + source, Trigger: "test", ProjectAutomationEnabled: source == "daemon_auto"}
		result, err := service.claimWithAuthorization(run, auth.Actor, auth)
		if err != nil || !result.Claimed {
			t.Fatalf("source %d %s: %#v %v", i, source, result, err)
		}
		stored, err := store.LatestRunAuthorization(run.ProjectID, run.RecordID)
		if err != nil {
			t.Fatal(err)
		}
		assertEqual(t, source, stored.Source, "authorization source")
		assertEqual(t, auth.Actor, stored.Actor, "authorization actor")
		assertEqual(t, auth.ProjectAutomationEnabled, stored.ProjectAutomationEnabled, "automation snapshot")
	}

	store, run := ownershipStoreFixture(t, "APP-T-MANUAL-DISABLED")
	service := newRunOwnershipService(store)
	manual, err := service.claimWithAuthorization(run, "manual", RunAuthorization{Source: "tusker_cli", Actor: "manual", Trigger: "pickup", ProjectAutomationEnabled: false})
	if err != nil || !manual.Claimed {
		t.Fatalf("manual claim while automation disabled: %#v %v", manual, err)
	}
}

func TestConcurrentProjectClaimEnforcesSharedCapacity(t *testing.T) {
	store, first := ownershipStoreFixture(t, "APP-T-CAP-1")
	second := first
	second.RecordID, second.ItemID = "APP-T-CAP-2", "APP-T-CAP-2"
	if err := store.UpsertRun(second); err != nil {
		t.Fatal(err)
	}
	service := newRunOwnershipService(store)
	service.projectConcurrencyLimit = 1
	var wg sync.WaitGroup
	results := make(chan runClaimResult, 2)
	for _, candidate := range []RunStatus{first, second} {
		candidate := candidate
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, _ := service.claim(candidate, "owner-"+candidate.RecordID)
			results <- result
		}()
	}
	wg.Wait()
	close(results)
	wins := 0
	for result := range results {
		if result.Claimed {
			wins++
		}
	}
	assertEqual(t, 1, wins, "shared project capacity claim winner")
}

func TestReliableExecutionLifecycleE2E(t *testing.T) {
	store, run := ownershipStoreFixture(t, "LIF-T-E2E")
	service := newRunOwnershipService(store)
	service.projectConcurrencyLimit = 1

	var wg sync.WaitGroup
	results := make(chan runClaimResult, 3)
	for _, source := range []string{"daemon_auto", "codex", "claude"} {
		source := source
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, _ := service.claimWithAuthorization(run, source, RunAuthorization{Source: source, Actor: source, Trigger: "race", ProjectAutomationEnabled: source == "daemon_auto"})
			results <- result
		}()
	}
	wg.Wait()
	close(results)
	winner := ""
	wins := 0
	for result := range results {
		if result.Claimed {
			wins++
			winner = result.Run.LeaseOwner
		}
	}
	assertEqual(t, 1, wins, "one race winner")

	identity := RunIdentityMetadata{ProjectID: run.ProjectID, RecordID: run.RecordID, RepoRoot: "/registered/repo", WorkspacePath: "/registered/repo", WorkspaceMode: "shared", Runner: run.Runner, Branch: "main", Head: "abc123"}
	if err := store.SaveRunIdentity(identity); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveSession(RunnerSession{ProjectID: run.ProjectID, RecordID: run.RecordID, Runner: run.Runner, SessionRef: "session-e2e", WorkspacePath: identity.WorkspacePath, State: "open", Resumable: true, StartedAt: time.Now().UTC().Format(time.RFC3339), LastSeenAt: time.Now().UTC().Format(time.RFC3339)}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.finish(run.RecordID, winner, AttemptOutcomeSucceeded, "", "A1 pass", ""); err == nil {
		t.Fatal("exit-zero without a deliverable must be rejected")
	}
	completed, err := service.finish(run.RecordID, winner, AttemptOutcomeSucceeded, "fixture.diff: one changed file", "A1-A6 pass", "")
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, string(LeaseStateReleased), completed.LeaseState, "success releases ownership")

	// Malformed/cumulative telemetry remains forensic data and cannot affect the
	// normalized delivery outcome or future claims.
	if err := store.SaveTurn(RunTurn{AttemptID: winner, ProjectID: run.ProjectID, RecordID: run.RecordID, TurnID: "malformed-usage", Status: "completed", InputTokens: -1, OutputTokens: 1 << 30, TotalTokens: 0}); err != nil {
		t.Fatal(err)
	}
	storedIdentity, _ := store.RunIdentity(run.ProjectID, run.RecordID)
	assertEqual(t, "shared", storedIdentity.WorkspaceMode, "shared workspace identity")
	auth, _ := store.LatestRunAuthorization(run.ProjectID, run.RecordID)
	assertEqual(t, winner, auth.Actor, "winning authorization retained")

	failureStore, failedRun := ownershipStoreFixture(t, "LIF-T-FAIL")
	failureService := newRunOwnershipService(failureStore)
	claimed, err := failureService.claim(failedRun, "failure-owner")
	if err != nil || !claimed.Claimed {
		t.Fatalf("failure claim: %#v %v", claimed, err)
	}
	failed, err := failureService.finish(failedRun.RecordID, "failure-owner", AttemptOutcomeFailed, "", "", "forced failure")
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, string(AttemptOutcomeFailed), failed.AttemptOutcome, "failure normalized")
	attempts, _ := failureStore.ListAttemptsForRun(failedRun.ProjectID, failedRun.RecordID)
	assertEqual(t, 1, len(attempts), "failed history inspectable")
}

func ownershipStoreFixture(t *testing.T, recordID string) (*RuntimeStore, RunStatus) {
	t.Helper()
	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	run := RunStatus{ProjectID: "project-1", RecordID: recordID, ItemID: recordID, Runner: string(RunnerCodexExec), Lane: runLaneExecute, LeaseState: string(LeaseStateUnclaimed), AttemptOutcome: string(AttemptOutcomeNone), WorkspacePath: "/tmp/repo with spaces", WorkRevision: 1}
	if err := store.UpsertRun(run); err != nil {
		t.Fatal(err)
	}
	return store, run
}
