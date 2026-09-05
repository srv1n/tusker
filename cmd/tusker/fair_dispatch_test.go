package main

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestFairMultiProjectDispatch(t *testing.T) {
	t.Run("poll_once_interleaves_projects_instead_of_draining_registry_order", func(t *testing.T) {
		stateRoot := t.TempDir()
		t.Setenv("TUSKER_STATE_ROOT", stateRoot)
		t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "xdg"))
		store, err := OpenRuntimeStore(stateRoot)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = store.Close() })
		fairDispatchPollProject(t, store, "project-a", "AAA", 2)
		fairDispatchPollProject(t, store, "project-b", "BBB", 1)

		daemon := &Daemon{
			store: store, stateRoot: stateRoot, frontiers: map[string]*projectFrontierIndex{},
			frontierHints: map[string][]daemonControlChange{},
		}
		var order []string
		daemon.fairDispatchRun = fairDispatchRecorder(&order)
		if err := daemon.PollOnce(context.Background()); err != nil {
			t.Fatal(err)
		}
		assertFairDispatchOrder(t, []string{"project-a/AAA-T-0001", "project-b/BBB-T-0001"}, order)
	})

	t.Run("disabled_stale_wave_and_corrupt_siblings_do_not_block_a_healthy_project", func(t *testing.T) {
		stateRoot := t.TempDir()
		t.Setenv("TUSKER_STATE_ROOT", stateRoot)
		t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "xdg"))
		store, err := OpenRuntimeStore(stateRoot)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = store.Close() })
		fairDispatchPollProject(t, store, "project-healthy", "HLT", 1)
		_, staleVault := fairDispatchPollProject(t, store, "project-stale-wave", "STL", 1)
		if _, err := setProjectLocalConfigWithReadback(staleVault, "automation.dispatch_scope", "armed_waves"); err != nil {
			t.Fatal(err)
		}
		setAutomationV7TaskFields(t, staleVault, "STL-T-0001", map[string]any{"wave": "W-MISSING"})
		for _, project := range []RegisteredProject{
			{
				ProjectID: "project-disabled", ProjectKey: "project-disabled", Name: "disabled",
				RepoRoot: filepath.Join(t.TempDir(), "missing"), VaultRoot: filepath.Join(t.TempDir(), ".tusker"),
				Enabled: false, Health: projectHealthHealthy,
			},
			{
				ProjectID: "project-corrupt", ProjectKey: "project-corrupt", Name: "corrupt",
				RepoRoot: filepath.Join(t.TempDir(), "missing"), VaultRoot: filepath.Join(t.TempDir(), ".tusker"),
				Enabled: true, Health: projectHealthHealthy,
			},
		} {
			if err := store.UpsertProject(project); err != nil {
				t.Fatal(err)
			}
		}

		daemon := &Daemon{
			store: store, stateRoot: stateRoot, frontiers: map[string]*projectFrontierIndex{},
			frontierHints: map[string][]daemonControlChange{},
		}
		var order []string
		daemon.fairDispatchRun = fairDispatchRecorder(&order)
		if err := daemon.PollOnce(context.Background()); err != nil {
			t.Fatal(err)
		}
		assertFairDispatchOrder(t, []string{"project-healthy/HLT-T-0001"}, order)
		stale := fairDispatchFindRun(t, store, "project-stale-wave", "STL-T-0001")
		if !strings.Contains(stale.LastError, "wave is not durably armed") {
			t.Fatalf("stale wave did not retain an actionable reason: %#v", stale)
		}
		projects, err := store.ListProjects()
		if err != nil {
			t.Fatal(err)
		}
		health := map[string]ProjectHealth{}
		for _, project := range projects {
			health[project.ProjectID] = project.Health
		}
		if health["project-corrupt"] != projectHealthError || health["project-disabled"] != projectHealthHealthy {
			t.Fatalf("registration isolation failed: %#v", health)
		}
	})

	t.Run("targeted_poll_loads_and_dispatches_only_the_requested_project", func(t *testing.T) {
		stateRoot := t.TempDir()
		t.Setenv("TUSKER_STATE_ROOT", stateRoot)
		t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "xdg"))
		store, err := OpenRuntimeStore(stateRoot)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = store.Close() })
		fairDispatchPollProject(t, store, "project-a", "TAA", 1)
		fairDispatchPollProject(t, store, "project-b", "TBB", 1)
		daemon := &Daemon{
			store: store, stateRoot: stateRoot, frontiers: map[string]*projectFrontierIndex{},
			frontierHints: map[string][]daemonControlChange{},
		}
		var order []string
		daemon.fairDispatchRun = fairDispatchRecorder(&order)
		if err := daemon.PollProjectOnce(context.Background(), "project-b"); err != nil {
			t.Fatal(err)
		}
		assertFairDispatchOrder(t, []string{"project-b/TBB-T-0001"}, order)
		if run := fairDispatchFindOptionalRun(t, store, "project-a", "TAA-T-0001"); run != nil {
			t.Fatalf("targeted poll touched sibling runtime state: %#v", *run)
		}
	})

	t.Run("equal_priority_round_robin_survives_restart", func(t *testing.T) {
		store := fairDispatchTestStore(t)
		first := &Daemon{store: store, stateRoot: store.stateRoot}
		var order []string
		first.fairDispatchRun = fairDispatchRecorder(&order)
		candidates := []daemonDispatchCandidate{
			fairDispatchTestCandidate("project-a", "A-T-0001", "p1", ""),
			fairDispatchTestCandidate("project-a", "A-T-0002", "p1", ""),
			fairDispatchTestCandidate("project-b", "B-T-0001", "p1", ""),
		}
		if err := first.dispatchFairCandidates(context.Background(), candidates, 3); err != nil {
			t.Fatal(err)
		}
		assertFairDispatchOrder(t, []string{"project-a/A-T-0001", "project-b/B-T-0001", "project-a/A-T-0002"}, order)

		restarted := &Daemon{store: store, stateRoot: store.stateRoot}
		order = nil
		restarted.fairDispatchRun = fairDispatchRecorder(&order)
		if err := restarted.dispatchFairCandidates(context.Background(), []daemonDispatchCandidate{
			fairDispatchTestCandidate("project-a", "A-T-0003", "p1", ""),
			fairDispatchTestCandidate("project-b", "B-T-0002", "p1", ""),
		}, 2); err != nil {
			t.Fatal(err)
		}
		assertFairDispatchOrder(t, []string{"project-b/B-T-0002", "project-a/A-T-0003"}, order)
	})

	t.Run("three_projects_receive_a_turn_before_any_project_repeats", func(t *testing.T) {
		store := fairDispatchTestStore(t)
		daemon := &Daemon{store: store, stateRoot: store.stateRoot}
		var order []string
		daemon.fairDispatchRun = fairDispatchRecorder(&order)
		if err := daemon.dispatchFairCandidates(context.Background(), []daemonDispatchCandidate{
			fairDispatchTestCandidate("project-a", "A-T-0001", "p1", ""),
			fairDispatchTestCandidate("project-a", "A-T-0002", "p1", ""),
			fairDispatchTestCandidate("project-b", "B-T-0001", "p1", ""),
			fairDispatchTestCandidate("project-c", "C-T-0001", "p1", ""),
		}, 4); err != nil {
			t.Fatal(err)
		}
		assertFairDispatchOrder(t, []string{
			"project-a/A-T-0001", "project-b/B-T-0001",
			"project-c/C-T-0001", "project-a/A-T-0002",
		}, order)
	})

	t.Run("priority_precedes_fairness", func(t *testing.T) {
		store := fairDispatchTestStore(t)
		daemon := &Daemon{store: store, stateRoot: store.stateRoot}
		var order []string
		daemon.fairDispatchRun = fairDispatchRecorder(&order)
		if err := daemon.dispatchFairCandidates(context.Background(), []daemonDispatchCandidate{
			fairDispatchTestCandidate("project-a", "A-T-0001", "p2", ""),
			fairDispatchTestCandidate("project-b", "B-T-0001", "p0", ""),
			fairDispatchTestCandidate("project-b", "B-T-0002", "p0", ""),
		}, 3); err != nil {
			t.Fatal(err)
		}
		assertFairDispatchOrder(t, []string{"project-b/B-T-0001", "project-b/B-T-0002", "project-a/A-T-0001"}, order)
	})

	t.Run("runner_capacity_blocks_only_that_runner_with_stable_reason", func(t *testing.T) {
		store := fairDispatchTestStore(t)
		active := fairDispatchTestRun("project-holder", "H-T-0001")
		active.LeaseState, active.LeaseOwner = string(LeaseStateRunning), "holder"
		if err := store.UpsertRun(active); err != nil {
			t.Fatal(err)
		}
		codex := fairDispatchTestCandidate("project-a", "A-T-0001", "p1", "")
		codex.RunnerLimit = 1
		claude := fairDispatchTestCandidate("project-b", "B-T-0001", "p1", "")
		claude.Run.Runner, claude.RunnerLimit = string(RunnerClaude), 1
		for _, run := range []RunStatus{codex.Run, claude.Run} {
			if err := store.UpsertRun(run); err != nil {
				t.Fatal(err)
			}
		}
		daemon := &Daemon{store: store, stateRoot: store.stateRoot}
		var order []string
		daemon.fairDispatchRun = fairDispatchRecorder(&order)
		if err := daemon.dispatchFairCandidates(context.Background(), []daemonDispatchCandidate{codex, claude}, 4); err != nil {
			t.Fatal(err)
		}
		assertFairDispatchOrder(t, []string{"project-b/B-T-0001"}, order)
		blocked := fairDispatchFindRun(t, store, "project-a", "A-T-0001")
		want := "dispatch waiting: runner codex_exec capacity reached (1/1)"
		if blocked.LastError != want {
			t.Fatalf("runner blocker=%q, want %q", blocked.LastError, want)
		}
	})

	t.Run("capacity_reason_is_stable_and_does_not_churn", func(t *testing.T) {
		store := fairDispatchTestStore(t)
		active := fairDispatchTestRun("project-a", "A-T-0001")
		active.LeaseState = string(LeaseStateRunning)
		active.LeaseOwner = "attempt-a"
		if err := store.UpsertRun(active); err != nil {
			t.Fatal(err)
		}
		waiting := fairDispatchTestRun("project-a", "A-T-0002")
		if err := store.UpsertRun(waiting); err != nil {
			t.Fatal(err)
		}
		candidate := fairDispatchTestCandidate("project-a", "A-T-0002", "p1", "")
		candidate.ProjectLimit = 1
		writes := 0
		daemon := &Daemon{store: store, stateRoot: store.stateRoot, beforePollRunPersist: func(_, _ RunStatus) { writes++ }}
		if err := daemon.dispatchFairCandidates(context.Background(), []daemonDispatchCandidate{candidate}, 4); err != nil {
			t.Fatal(err)
		}
		if err := daemon.dispatchFairCandidates(context.Background(), []daemonDispatchCandidate{candidate}, 4); err != nil {
			t.Fatal(err)
		}
		if writes != 1 {
			t.Fatalf("capacity blocker writes=%d, want one stable write", writes)
		}
		run := fairDispatchFindRun(t, store, "project-a", "A-T-0002")
		want := "dispatch waiting: project capacity reached (1/1)"
		if run.LastError != want {
			t.Fatalf("capacity reason=%q, want %q", run.LastError, want)
		}
	})

	t.Run("free_named_resource_is_reserved_before_a_second_candidate_can_claim", func(t *testing.T) {
		store := fairDispatchTestStore(t)
		first := fairDispatchTestCandidate("project-a", "A-T-0001", "p1", "build-host")
		second := fairDispatchTestCandidate("project-b", "B-T-0001", "p1", "build-host")
		for _, run := range []RunStatus{first.Run, second.Run} {
			if err := store.UpsertRun(run); err != nil {
				t.Fatal(err)
			}
		}
		var wakes []string
		resourceLeaseWaiterState.Lock()
		previousNotify := resourceLeaseWaiterState.notify
		resourceLeaseWaiterState.notify = func(_ string, resource, project string) {
			wakes = append(wakes, resource+"/"+project)
		}
		resourceLeaseWaiterState.Unlock()
		t.Cleanup(func() {
			resourceLeaseWaiterState.Lock()
			resourceLeaseWaiterState.notify = previousNotify
			resourceLeaseWaiterState.Unlock()
		})

		daemon := &Daemon{store: store, stateRoot: store.stateRoot}
		var order []string
		daemon.fairDispatchRun = func(_ context.Context, project RegisteredProject, _ WorkflowFile, _ Note, run RunStatus, _, attemptID string) (RunStatus, bool, bool, error) {
			order = append(order, project.ProjectID+"/"+run.ItemID)
			run.LeaseState, run.LeaseOwner = string(LeaseStateClaimed), attemptID
			run.LeaseGeneration++
			run.LeaseExpiresAt = time.Now().UTC().Add(defaultRunLeaseTTL).Format(time.RFC3339)
			if err := store.UpsertRun(run); err != nil {
				return run, false, false, err
			}
			return run, true, true, nil
		}
		if err := daemon.dispatchFairCandidates(context.Background(), []daemonDispatchCandidate{first, second}, 2); err != nil {
			t.Fatal(err)
		}
		assertFairDispatchOrder(t, []string{"project-a/A-T-0001"}, order)
		blocked := fairDispatchFindRun(t, store, "project-b", "B-T-0001")
		if !strings.Contains(blocked.LastError, `named resource "build-host" is held by project project-a`) {
			t.Fatalf("second resource user was not blocked: %#v", blocked)
		}
		lease, err := store.FindResourceLease("build-host")
		if err != nil || lease == nil || lease.ProjectID != "project-a" || lease.State != resourceLeaseHeld {
			t.Fatalf("resource reservation missing after first claim: %#v %v", lease, err)
		}
		expiredAt := time.Now().UTC().Add(3 * time.Minute)
		if _, acquired, err := store.AcquireResourceLease(ResourceLeaseAcquireInput{
			Name: "build-host", Owner: "intruder", Purpose: "promotion",
			ProjectID: "project-intruder", Now: expiredAt,
		}); err == nil || acquired {
			t.Fatalf("expired task resource with a live run allowed takeover: acquired=%t err=%v", acquired, err)
		} else {
			var typed *TuskerError
			if !errors.As(err, &typed) || typed.Code != resourceLeaseRefusal {
				t.Fatalf("expired live resource returned wrong refusal: %v", err)
			}
		}
		generation := lease.Generation
		renewed, acquired, err := store.AcquireResourceLease(ResourceLeaseAcquireInput{
			Name: lease.Name, Owner: lease.Owner, Purpose: lease.Purpose,
			ProjectID: lease.ProjectID, DepartureID: lease.DepartureID,
			TTL: defaultResourceLeaseTTL, Now: expiredAt,
		})
		if err != nil || !acquired || renewed.Generation != generation {
			t.Fatalf("original live owner did not renew its fence: %#v acquired=%t err=%v", renewed, acquired, err)
		}
		recoveries, err := store.ReconcileExpiredResourceLeases(expiredAt.Add(3*time.Minute), nil)
		if err != nil || len(recoveries) != 1 || !strings.Contains(recoveries[0].Reason, "live run owner") {
			t.Fatalf("restart-style resource reconciliation lost the live task holder: %#v %v", recoveries, err)
		}
		lease, err = store.FindResourceLease("build-host")
		if err != nil || lease == nil || lease.ProjectID != "project-a" || lease.State != resourceLeaseHeld {
			t.Fatalf("live task resource was not fenced across restart: %#v %v", lease, err)
		}

		completed, err := newRunOwnershipService(store).finish(
			"A-T-0001", lease.Owner, AttemptOutcomeFailed, "", "", "test completion",
		)
		if err != nil || completed.LeaseState != string(LeaseStateReleased) {
			t.Fatalf("real run completion failed: %#v err=%v", completed, err)
		}
		assertFairDispatchOrder(t, []string{"build-host/project-b"}, wakes)
		lease, err = store.FindResourceLease("build-host")
		if err != nil || lease == nil || lease.State != resourceLeaseReleased ||
			lease.ReleaseReason != "run completion released dispatch capacity" {
			t.Fatalf("completion did not immediately release its fenced resource: %#v err=%v", lease, err)
		}
		if err := daemon.dispatchFairCandidates(context.Background(), []daemonDispatchCandidate{second}, 2); err != nil {
			t.Fatal(err)
		}
		assertFairDispatchOrder(t, []string{"project-a/A-T-0001", "project-b/B-T-0001"}, order)
	})

	t.Run("resource_release_wakes_exact_waiters_and_blocked_project_does_not_stall_sibling", func(t *testing.T) {
		store := fairDispatchTestStore(t)
		now := time.Now().UTC()
		lease, ok, err := store.AcquireResourceLease(ResourceLeaseAcquireInput{
			Name: "build-host", Owner: "departure:one", Purpose: "promotion",
			ProjectID: "holder", Now: now,
		})
		if err != nil || !ok {
			t.Fatalf("acquire resource: %#v %v %v", lease, ok, err)
		}
		daemon := &Daemon{store: store, stateRoot: store.stateRoot}
		var order []string
		daemon.fairDispatchRun = fairDispatchRecorder(&order)
		if err := daemon.dispatchFairCandidates(context.Background(), []daemonDispatchCandidate{
			fairDispatchTestCandidate("project-a", "A-T-0001", "p1", "build-host"),
			fairDispatchTestCandidate("project-b", "B-T-0001", "p1", ""),
		}, 2); err != nil {
			t.Fatal(err)
		}
		assertFairDispatchOrder(t, []string{"project-b/B-T-0001"}, order)
		if err := store.RegisterResourceLeaseWaiter("build-host", "project-c"); err != nil {
			t.Fatal(err)
		}
		if err := store.RegisterResourceLeaseWaiter("other-host", "project-d"); err != nil {
			t.Fatal(err)
		}

		var wakes []string
		resourceLeaseWaiterState.Lock()
		previousNotify := resourceLeaseWaiterState.notify
		resourceLeaseWaiterState.notify = func(_ string, resource, project string) {
			wakes = append(wakes, resource+"/"+project)
		}
		resourceLeaseWaiterState.Unlock()
		t.Cleanup(func() {
			resourceLeaseWaiterState.Lock()
			resourceLeaseWaiterState.notify = previousNotify
			resourceLeaseWaiterState.Unlock()
		})
		released, err := store.ReleaseResourceLease(lease.Name, lease.Owner, lease.Generation, "promotion complete", now.Add(time.Second))
		if err != nil || !released {
			t.Fatalf("release resource: released=%t err=%v", released, err)
		}
		sort.Strings(wakes)
		assertFairDispatchOrder(t, []string{"build-host/project-a", "build-host/project-c"}, wakes)
	})

	t.Run("daemon_reconciliation_releases_fenced_resource_immediately", func(t *testing.T) {
		store := fairDispatchTestStore(t)
		now := time.Now().UTC()
		live := RunStatus{
			ProjectID: "project-a", RecordID: "A-T-0001", ItemID: "A-T-0001",
			LeaseState: string(LeaseStateRunning), LeaseOwner: "attempt-a", LeaseGeneration: 1,
			LeaseExpiresAt: now.Add(defaultRunLeaseTTL).Format(time.RFC3339),
			AttemptOutcome: string(AttemptOutcomeNone), UpdatedAt: now.Format(time.RFC3339),
		}
		if err := store.UpsertRun(live); err != nil {
			t.Fatal(err)
		}
		lease, acquired, err := store.AcquireResourceLease(ResourceLeaseAcquireInput{
			Name: "reconcile-host", Owner: live.LeaseOwner,
			Purpose: fairDispatchResourcePurpose(live.RecordID), ProjectID: live.ProjectID,
			DepartureID: live.RecordID, TTL: defaultResourceLeaseTTL, Now: now,
		})
		if err != nil || !acquired {
			t.Fatalf("resource acquire: %#v acquired=%t err=%v", lease, acquired, err)
		}
		if err := store.RegisterResourceLeaseWaiter(lease.Name, "project-b"); err != nil {
			t.Fatal(err)
		}
		var wakes []string
		resourceLeaseWaiterState.Lock()
		previousNotify := resourceLeaseWaiterState.notify
		resourceLeaseWaiterState.notify = func(_ string, resource, project string) {
			wakes = append(wakes, resource+"/"+project)
		}
		resourceLeaseWaiterState.Unlock()
		t.Cleanup(func() {
			resourceLeaseWaiterState.Lock()
			resourceLeaseWaiterState.notify = previousNotify
			resourceLeaseWaiterState.Unlock()
		})

		released := live
		released.LeaseState, released.LeaseOwner, released.LeaseExpiresAt = string(LeaseStateReleased), "", ""
		released.AttemptOutcome = string(AttemptOutcomeSucceeded)
		released.UpdatedAt = now.Add(time.Second).Format(time.RFC3339)
		broker := newServeStreamBroker()
		t.Cleanup(broker.Close)
		daemon := &Daemon{store: store, stream: broker}
		if err := daemon.upsertRunWithStream(live, released); err != nil {
			t.Fatal(err)
		}
		assertFairDispatchOrder(t, []string{"reconcile-host/project-b"}, wakes)
		stored, err := store.FindResourceLease(lease.Name)
		if err != nil || stored == nil || stored.State != resourceLeaseReleased ||
			stored.ReleaseReason != "run reconciliation released dispatch capacity" {
			t.Fatalf("daemon reconciliation did not release resource: %#v err=%v", stored, err)
		}
	})

	t.Run("post-reactor soft dependency relock drops stale all-eligible candidate", func(t *testing.T) {
		vault := automationTestVault(t)
		setAllEligibleDispatchScopeForAutomationTest(t, vault)
		mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Reviewed premise", "risk": "low", "priority": "p0", "v7": "true"}, newV7Task)
		mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Soft dependent", "risk": "low", "priority": "p1", "dependencies": "APP-T-0001:soft", "v7": "true"}, newV7Task)
		for _, taskID := range []string{"APP-T-0001", "APP-T-0002"} {
			makeV7TaskDispatchableForTest(t, vault, taskID)
		}
		setAutomationV7TaskFields(t, vault, "APP-T-0001", map[string]any{
			"status": "review", "readiness": "waiting_on_review", "proof_status": "satisfied",
			"work_revision": 1, "source_sha": "reviewed-premise",
		})
		mustRunPickupTest(t, Args{"vault": vault, "quiet": "true"}, reconcileV7Cmd)
		stale, err := resolveV7Note(vault, "APP-T-0002", "task")
		if err != nil {
			t.Fatal(err)
		}
		if stringField(stale.Data, "readiness") != "ready" {
			t.Fatalf("soft dependent was not initially dispatchable: %#v", stale.Data)
		}
		staleNotes, err := listOperationalNotes(vault)
		if err != nil {
			t.Fatal(err)
		}
		staleByID, _ := daemonNoteMaps(staleNotes)
		project := registerAutomationTestProject(t, vault)
		store, err := OpenRuntimeStore(DefaultStateRoot())
		if err != nil {
			t.Fatal(err)
		}
		defer store.Close()
		run := fairDispatchTestRun(project.ProjectID, "APP-T-0002")
		run.WorkRevision = intField(stale.Data, "work_revision")
		if err := store.UpsertRun(run); err != nil {
			t.Fatal(err)
		}
		wfFile, err := loadWorkflow(vault)
		if err != nil {
			t.Fatal(err)
		}
		candidate := daemonDispatchCandidate{
			Project: project, Workflow: wfFile, Note: stale, NotesByID: staleByID,
			Run: run, Lane: runLaneExecute, Status: stringField(stale.Data, "status"),
			ProjectLimit: 4, RunnerLimit: fairDispatchRunnerLimit(wfFile.Data),
		}

		// This is the same transition the completion reactor performs after a
		// changes-requested result, deliberately after candidate capture.
		mustRunPickupTest(t, Args{
			"vault": vault, "quiet": "true", "id": "APP-T-0001",
			"status": "rework", "by": "reviewer:agent", "reason": "Review rejected the soft premise.",
		}, statusV7Cmd)
		relocked, err := resolveV7Note(vault, "APP-T-0002", "task")
		if err != nil {
			t.Fatal(err)
		}
		if stringField(relocked.Data, "readiness") != "blocked_by_dependency" {
			t.Fatalf("changes-requested transition did not relock dependent: %#v", relocked.Data)
		}

		daemon := &Daemon{store: store, stateRoot: store.stateRoot}
		var order []string
		daemon.fairDispatchRun = fairDispatchRecorder(&order)
		if err := daemon.dispatchFairCandidates(context.Background(), []daemonDispatchCandidate{candidate}, 2); err != nil {
			t.Fatal(err)
		}
		if len(order) != 0 {
			t.Fatalf("stale all-eligible candidate reached claim: %#v", order)
		}
		blocked := fairDispatchFindRun(t, store, project.ProjectID, "APP-T-0002")
		if !strings.Contains(blocked.LastError, "blocked_by_dependency") {
			t.Fatalf("stale candidate lacks post-reactor dependency reason: %#v", blocked)
		}
	})

	t.Run("armed_wave_concurrency_guard_is_stable_before_shared_selection", func(t *testing.T) {
		vault, idx, _ := armedWaveTestFixture(t)
		review := idx.Tasks["APP-T-0002"]
		review.Data = cloneMap(review.Data)
		review.Data["status"] = "review"
		wf := defaultWorkflow()
		wf.DispatchScope = defaultAutomationDispatchScope()
		wf.Workspace.Strategy = string(WorkspaceStrategyWorktree)
		runs := map[string]RunStatus{
			"APP-T-0001": {RecordID: "APP-T-0001", ItemID: "APP-T-0001", LeaseState: string(LeaseStateRunning)},
			"APP-T-0006": {RecordID: "APP-T-0006", ItemID: "APP-T-0006", LeaseState: string(LeaseStateRunning)},
		}
		first := armedWaveDispatchBlocker(vault, review, wf, runs)
		second := armedWaveDispatchBlocker(vault, review, wf, runs)
		if first != "armed wave concurrency ceiling reached" || second != first {
			t.Fatalf("wave concurrency blocker unstable: first=%q second=%q", first, second)
		}

		store := fairDispatchTestStore(t)
		project := RegisteredProject{
			ProjectID: "project-wave", ProjectKey: "project-wave",
			VaultRoot: vault, RepoRoot: filepath.Dir(vault), Enabled: true,
		}
		notesByID := map[string]Note{}
		for _, id := range []string{"APP-T-0001", "APP-T-0002", "APP-T-0006"} {
			note := idx.Tasks[id]
			note.Data = cloneMap(note.Data)
			delete(note.Data, "concurrency_group")
			delete(note.Data, "resource_refs")
			notesByID[id] = note
		}
		dynamicReview := notesByID["APP-T-0002"]
		dynamicReview.Data["status"] = "review"
		notesByID["APP-T-0002"] = dynamicReview
		wf.Agents.MaxConcurrentAgents = 0
		wfFile := WorkflowFile{Data: wf}
		candidates := make([]daemonDispatchCandidate, 0, 3)
		for _, id := range []string{"APP-T-0001", "APP-T-0002", "APP-T-0006"} {
			note := notesByID[id]
			run := fairDispatchTestRun(project.ProjectID, id)
			if err := store.UpsertRun(run); err != nil {
				t.Fatal(err)
			}
			lane := runLaneExecute
			if id == "APP-T-0002" {
				lane = runLaneReview
			}
			candidates = append(candidates, daemonDispatchCandidate{
				Project: project, Workflow: wfFile, Note: note, NotesByID: notesByID,
				Run: run, Lane: lane, Status: stringField(note.Data, "status"), ProjectLimit: 4,
			})
		}
		daemon := &Daemon{store: store, stateRoot: store.stateRoot}
		var order []string
		daemon.fairDispatchRun = fairDispatchRecorder(&order)
		if err := daemon.dispatchFairCandidates(context.Background(), candidates, 3); err != nil {
			t.Fatal(err)
		}
		assertFairDispatchOrder(t, []string{
			"project-wave/APP-T-0001", "project-wave/APP-T-0002",
		}, order)
		blocked := fairDispatchFindRun(t, store, project.ProjectID, "APP-T-0006")
		if !strings.Contains(blocked.LastError, "wave constraint") {
			t.Fatalf("deferred wave candidate escaped the recomputed cap: %#v", blocked)
		}
	})

	t.Run("owned_path_guard_refuses_the_daemon_claim_with_stable_identity", func(t *testing.T) {
		_, candidate, service := ownedPathClaimFixture(
			t,
			"APP-T-PATH-2", "APP-T-PATH-1",
			[]string{"migrations/0014.sql"}, []string{"migrations"}, nil,
		)
		var first string
		for attempt := 0; attempt < 2; attempt++ {
			_, err := service.claimExistingWithAuthorization(candidate, "scheduler-attempt", RunAuthorization{
				Source: "daemon_auto", Actor: "daemon", Trigger: "poll", ProjectAutomationEnabled: true,
			}, RunAttempt{})
			var typed *TuskerError
			if !errors.As(err, &typed) || typed.Code != "OWNED_PATH_CONFLICT" {
				t.Fatalf("owned path claim was not fenced: %v", err)
			}
			if attempt == 0 {
				first = err.Error()
			} else if err.Error() != first {
				t.Fatalf("owned path reason changed between polls: first=%q second=%q", first, err.Error())
			}
		}

		store := fairDispatchTestStore(t)
		notesByID := map[string]Note{
			"APP-T-OVERLAP-1": {Data: map[string]any{
				"id": "APP-T-OVERLAP-1", "priority": "p1", "owned_paths": []string{"migrations"},
			}},
			"APP-T-OVERLAP-2": {Data: map[string]any{
				"id": "APP-T-OVERLAP-2", "priority": "p1", "owned_paths": []string{"migrations/0014.sql"},
			}},
		}
		candidates := make([]daemonDispatchCandidate, 0, 2)
		for _, id := range []string{"APP-T-OVERLAP-1", "APP-T-OVERLAP-2"} {
			candidate := fairDispatchTestCandidate("project-overlap", id, "p1", "")
			candidate.Note, candidate.NotesByID = notesByID[id], notesByID
			if err := store.UpsertRun(candidate.Run); err != nil {
				t.Fatal(err)
			}
			candidates = append(candidates, candidate)
		}
		daemon := &Daemon{store: store, stateRoot: store.stateRoot}
		var order []string
		daemon.fairDispatchRun = fairDispatchRecorder(&order)
		if err := daemon.dispatchFairCandidates(context.Background(), candidates, 2); err != nil {
			t.Fatal(err)
		}
		assertFairDispatchOrder(t, []string{"project-overlap/APP-T-OVERLAP-1"}, order)
		blocked := fairDispatchFindRun(t, store, "project-overlap", "APP-T-OVERLAP-2")
		if !strings.Contains(blocked.LastError, "owned path conflict") {
			t.Fatalf("deferred overlapping path escaped the recomputed guard: %#v", blocked)
		}
	})

	t.Run("disk_pressure_stops_selection_before_claim_with_stable_reason", func(t *testing.T) {
		store := fairDispatchTestStore(t)
		candidate := fairDispatchTestCandidate("project-a", "A-T-0001", "p1", "")
		candidate.Project.RepoRoot = t.TempDir()
		candidate.Project.VaultRoot = t.TempDir()
		workflow := defaultWorkflow()
		workflow.AutomationEnabled = true
		workflow.DispatchScope = automationDispatchScopeProjection{
			Configured: string(automationDispatchScopeAllEligible),
			Effective:  string(automationDispatchScopeAllEligible),
			Provenance: "test",
		}
		workflow.Workspace.Strategy = string(WorkspaceStrategyCopy)
		workflow.Workspace.Root = "workspaces/fair-dispatch-disk"
		candidate.Workflow = WorkflowFile{Data: workflow}
		candidate.RunnerLimit = 0
		if err := store.UpsertRun(candidate.Run); err != nil {
			t.Fatal(err)
		}
		daemon := &Daemon{
			store: store, stateRoot: store.stateRoot,
			diskStat: func(string) (diskFilesystemStat, error) {
				return diskFilesystemStat{Blocks: 100, AvailableBlocks: 1, BlockSize: 1 << 30}, nil
			},
			beforeRunLeaseClaim: func(RunStatus) {
				t.Fatal("disk pressure reached the lease claim")
			},
		}
		for poll := 0; poll < 2; poll++ {
			if err := daemon.dispatchFairCandidates(context.Background(), []daemonDispatchCandidate{candidate}, 2); err != nil {
				t.Fatal(err)
			}
		}
		blocked := fairDispatchFindRun(t, store, "project-a", "A-T-0001")
		if !strings.Contains(blocked.LastError, "disk_pressure") || runConsumesDispatchCapacity(blocked) {
			t.Fatalf("disk pressure did not preserve an unclaimed stable blocker: %#v", blocked)
		}
	})

	t.Run("cas_loss_does_not_block_sibling_or_consume_turn", func(t *testing.T) {
		store := fairDispatchTestStore(t)
		daemon := &Daemon{store: store, stateRoot: store.stateRoot}
		var order []string
		daemon.fairDispatchRun = func(_ context.Context, project RegisteredProject, _ WorkflowFile, _ Note, run RunStatus, _, attemptID string) (RunStatus, bool, bool, error) {
			order = append(order, project.ProjectID+"/"+run.ItemID)
			if project.ProjectID == "project-a" {
				return run, true, false, tuskerError("CAS_CONFLICT", "concurrent owner won")
			}
			run.LeaseState = string(LeaseStateClaimed)
			run.LeaseOwner = attemptID
			run.LeaseGeneration++
			run.LeaseExpiresAt = time.Now().UTC().Add(defaultRunLeaseTTL).Format(time.RFC3339)
			return run, true, true, nil
		}
		if err := daemon.dispatchFairCandidates(context.Background(), []daemonDispatchCandidate{
			fairDispatchTestCandidate("project-a", "A-T-0001", "p1", ""),
			fairDispatchTestCandidate("project-b", "B-T-0001", "p1", ""),
		}, 2); err != nil {
			t.Fatal(err)
		}
		assertFairDispatchOrder(t, []string{"project-a/A-T-0001", "project-b/B-T-0001"}, order)
		ledger, err := loadFairDispatchLedger(store)
		if err != nil {
			t.Fatal(err)
		}
		if ledger.ProjectTurns["project-a"] != 0 || ledger.ProjectTurns["project-b"] == 0 {
			t.Fatalf("CAS loser consumed fairness turn: %#v", ledger.ProjectTurns)
		}
	})

	t.Run("duplicate_candidate_claims_once", func(t *testing.T) {
		store := fairDispatchTestStore(t)
		daemon := &Daemon{store: store, stateRoot: store.stateRoot}
		candidate := fairDispatchTestCandidate("project-a", "A-T-0001", "p1", "")
		var order []string
		daemon.fairDispatchRun = fairDispatchRecorder(&order)
		if err := daemon.dispatchFairCandidates(context.Background(), []daemonDispatchCandidate{candidate, candidate}, 2); err != nil {
			t.Fatal(err)
		}
		assertFairDispatchOrder(t, []string{"project-a/A-T-0001"}, order)
	})
}

func fairDispatchPollProject(t *testing.T, store *RuntimeStore, projectID, acronym string, tasks int) (RegisteredProject, string) {
	t.Helper()
	root := t.TempDir()
	vault := filepath.Join(root, ".tusker")
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true"}, bootstrap)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "acronym": acronym, "title": projectID, "summary": "Fair dispatch fixture.", "v7": "true"}, newV7Epic)
	for index := 0; index < tasks; index++ {
		mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": acronym, "title": "Work", "risk": "low", "priority": "p1", "v7": "true"}, newV7Task)
		makeV7TaskDispatchableForTest(t, vault, acronym+"-T-"+padNumber(index+1))
	}
	if err := writeDefaultWorkflow(vault); err != nil {
		t.Fatal(err)
	}
	setDirectEmergencyProfileForAutomationTest(t, vault)
	for key, value := range map[string]any{
		"automation.enabled":                  true,
		"automation.dispatch_scope":           "all_eligible",
		"runtime.max_active_runs_per_project": 3,
	} {
		if _, err := setProjectLocalConfigWithReadback(vault, key, value); err != nil {
			t.Fatal(err)
		}
	}
	project := RegisteredProject{
		ProjectID: projectID, ProjectKey: projectID, Name: projectID,
		RepoRoot: root, VaultRoot: vault, WorkflowPath: workflowPath(vault),
		Enabled: true, Health: projectHealthHealthy,
	}
	if err := store.UpsertProject(project); err != nil {
		t.Fatal(err)
	}
	return project, vault
}

func fairDispatchTestStore(t *testing.T) *RuntimeStore {
	t.Helper()
	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func fairDispatchTestRun(projectID, taskID string) RunStatus {
	return RunStatus{
		ProjectID: projectID, RecordID: taskID, ItemID: taskID,
		Runner: string(RunnerCodexExec), Lane: runLaneExecute,
		LeaseState: string(LeaseStateUnclaimed), AttemptOutcome: string(AttemptOutcomeNone),
	}
}

func fairDispatchTestCandidate(projectID, taskID, priority, resource string) daemonDispatchCandidate {
	run := fairDispatchTestRun(projectID, taskID)
	note := Note{Data: map[string]any{"id": taskID, "priority": priority}}
	if resource != "" {
		note.Data["concurrency_group"] = resource
	}
	var workflow WorkflowFile
	workflow.Data.Agents.MaxConcurrentAgents = 0
	return daemonDispatchCandidate{
		Project:  RegisteredProject{ProjectID: projectID, ProjectKey: projectID, Enabled: true},
		Workflow: workflow, Note: note, Run: run, Lane: runLaneExecute, Status: "ready",
		ProjectLimit: 4,
	}
}

func fairDispatchRecorder(order *[]string) func(context.Context, RegisteredProject, WorkflowFile, Note, RunStatus, string, string) (RunStatus, bool, bool, error) {
	return func(_ context.Context, project RegisteredProject, _ WorkflowFile, _ Note, run RunStatus, _, attemptID string) (RunStatus, bool, bool, error) {
		*order = append(*order, project.ProjectID+"/"+run.ItemID)
		run.LeaseState = string(LeaseStateClaimed)
		run.LeaseOwner = attemptID
		run.LeaseGeneration++
		run.LeaseExpiresAt = time.Now().UTC().Add(defaultRunLeaseTTL).Format(time.RFC3339)
		run.StartedAt = time.Now().UTC().Format(time.RFC3339)
		return run, true, true, nil
	}
}

func fairDispatchFindRun(t *testing.T, store *RuntimeStore, projectID, recordID string) RunStatus {
	t.Helper()
	runs, err := store.ListRuns()
	if err != nil {
		t.Fatal(err)
	}
	for _, run := range runs {
		if run.ProjectID == projectID && run.RecordID == recordID {
			return run
		}
	}
	t.Fatalf("run not found: %s/%s", projectID, recordID)
	return RunStatus{}
}

func fairDispatchFindOptionalRun(t *testing.T, store *RuntimeStore, projectID, recordID string) *RunStatus {
	t.Helper()
	runs, err := store.ListRuns()
	if err != nil {
		t.Fatal(err)
	}
	for _, run := range runs {
		if run.ProjectID == projectID && run.RecordID == recordID {
			copy := run
			return &copy
		}
	}
	return nil
}

func assertFairDispatchOrder(t *testing.T, want, got []string) {
	t.Helper()
	if !reflect.DeepEqual(want, got) {
		t.Fatalf("dispatch order=%#v, want %#v", got, want)
	}
}
