package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type departureExecutionFixture struct {
	repo    string
	vault   string
	project RegisteredProject
	wf      Workflow
	plan    func(RegisteredProject, Workflow) (DepartureDecision, error)
}

func TestDepartureExecution(t *testing.T) {
	t.Run("stage keeps main fixed and replay does not duplicate audit", func(t *testing.T) {
		fixture := newUnstagedDepartureExecutionFixture(t, scheduledPromotionStage, "stage-executor.txt")
		store, err := OpenRuntimeStore(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		defer store.Close()
		run := createDepartureExecutionRun(t, store, fixture)
		mainBefore := gitRevisionForTest(t, fixture.repo, "main")
		integrationBefore := gitRevisionForTest(t, fixture.repo, "integration/W-0001")
		auditBefore := departureLandingAuditCount(t, fixture.vault, "W-0001", "APP-T-0001")
		d := &Daemon{store: store, departurePlan: fixture.plan}
		if err := d.executeDeparture(context.Background(), fixture.project, fixture.wf, run.ID); err != nil {
			t.Fatal(err)
		}
		got := mustDepartureRun(t, store, run.ID)
		if got.State != DepartureStatePassed {
			t.Fatalf("stage departure state = %s, want passed: %#v", got.State, got)
		}
		if after := gitRevisionForTest(t, fixture.repo, "main"); after != mainBefore {
			t.Fatalf("stage departure moved main: before=%s after=%s", mainBefore, after)
		}
		integrationAfter := gitRevisionForTest(t, fixture.repo, "integration/W-0001")
		sourceSHA := run.Candidate.TaskSourceSHAs["APP-T-0001"]
		if integrationAfter == integrationBefore || !gitMergeBaseAncestor(fixture.repo, sourceSHA, integrationAfter) {
			t.Fatalf("eligible cargo was not staged exactly: before=%s after=%s source=%s", integrationBefore, integrationAfter, sourceSHA)
		}
		if got := gitShowFile(t, fixture.repo, "integration/W-0001", "stage-executor.txt"); got != "departure\n" {
			t.Fatalf("staged content = %q", got)
		}
		auditAfter := departureLandingAuditCount(t, fixture.vault, "W-0001", "APP-T-0001")
		if auditAfter != auditBefore+1 {
			t.Fatalf("stage audit count = %d, want %d", auditAfter, auditBefore+1)
		}
		if err := d.executeDeparture(context.Background(), fixture.project, fixture.wf, run.ID); err != nil {
			t.Fatal(err)
		}
		if after := departureLandingAuditCount(t, fixture.vault, "W-0001", "APP-T-0001"); after != auditAfter {
			t.Fatalf("stage replay duplicated landing audit: before=%d after=%d", auditAfter, after)
		}
	})

	t.Run("promote resumes in a fresh daemon and replays promoted audit", func(t *testing.T) {
		fixture := newDepartureExecutionFixture(t, scheduledPromotionPromote, "promote-executor.txt")
		stateRoot := t.TempDir()
		store, err := OpenRuntimeStore(stateRoot)
		if err != nil {
			t.Fatal(err)
		}
		run := createDepartureExecutionRun(t, store, fixture)
		mainBefore := gitRevisionForTest(t, fixture.repo, "main")
		if err := store.Close(); err != nil {
			t.Fatal(err)
		}

		restarted, err := NewDaemon(stateRoot)
		if err != nil {
			t.Fatal(err)
		}
		defer restarted.Close()
		restarted.departurePlan = fixture.plan
		originalAfterRefUpdate := scheduledPromotionAfterRefUpdate
		refUpdated := make(chan struct{})
		finishPromotion := make(chan struct{})
		scheduledPromotionAfterRefUpdate = func() error {
			close(refUpdated)
			<-finishPromotion
			return nil
		}
		defer func() {
			select {
			case <-finishPromotion:
			default:
				close(finishPromotion)
			}
			scheduledPromotionAfterRefUpdate = originalAfterRefUpdate
		}()
		if err := restarted.startPendingDepartureExecutions(context.Background(), fixture.project, fixture.wf); err != nil {
			t.Fatal(err)
		}
		select {
		case <-refUpdated:
		case <-time.After(15 * time.Second):
			t.Fatal("promotion did not reach the post-ref restart boundary")
		}
		if err := restarted.startPendingDepartureExecutions(context.Background(), fixture.project, fixture.wf); err != nil {
			t.Fatal(err)
		}
		inFlight := mustDepartureRun(t, restarted.store, run.ID)
		if inFlight.State != DepartureStateGating || inFlight.Promotion.AttemptedAt == "" {
			t.Fatalf("active promotion was not preserved at the restart boundary: %#v", inFlight)
		}
		close(finishPromotion)
		passed := waitDepartureState(t, restarted.store, run.ID, DepartureStatePassed)
		mainAfter := gitRevisionForTest(t, fixture.repo, "main")
		if mainAfter == mainBefore || passed.Promotion.CommittedSHA != mainAfter ||
			passed.Promotion.CommittedRef != "main" || passed.Gate.Status != "passed" ||
			passed.Gate.Command == "" || passed.Gate.Profile != "full" || passed.Gate.Toolchain == "" {
			t.Fatalf("promotion facts/main mismatch: before=%s after=%s run=%#v", mainBefore, mainAfter, passed)
		}
		lease, err := restarted.store.FindResourceLease("gate:full")
		if err != nil || lease == nil || lease.State != resourceLeaseReleased || lease.ReleaseReason != "promotion passed" {
			t.Fatalf("promotion lease was not released: %#v err=%v", lease, err)
		}
		if count := departureLandingAuditCount(t, fixture.vault, "W-0001", "wave"); count != 1 {
			t.Fatalf("promotion audit count = %d, want 1", count)
		}

		removeDeparturePromotionAudit(t, fixture.vault, "W-0001", mainAfter)
		current := mustDepartureRun(t, restarted.store, run.ID)
		if changed, err := restarted.store.TransitionDepartureRun(withDepartureState(current, DepartureStateGating), current.StateRevision); err != nil || !changed {
			t.Fatalf("prepare committed gating restart: changed=%v err=%v", changed, err)
		}
		if err := restarted.executeDeparture(context.Background(), fixture.project, fixture.wf, run.ID); err != nil {
			t.Fatal(err)
		}
		replayed := mustDepartureRun(t, restarted.store, run.ID)
		if replayed.State != DepartureStatePassed || gitRevisionForTest(t, fixture.repo, "main") != mainAfter {
			t.Fatalf("committed gating restart changed main or missed terminal state: %#v", replayed)
		}
		if count := departureLandingAuditCount(t, fixture.vault, "W-0001", "wave"); count != 1 {
			t.Fatalf("committed gating restart did not repair exactly one audit row: %d", count)
		}
		current = mustDepartureRun(t, restarted.store, run.ID)
		if changed, err := restarted.store.TransitionDepartureRun(withDepartureState(current, DepartureStatePromoted), current.StateRevision); err != nil || !changed {
			t.Fatalf("prepare promoted replay: changed=%v err=%v", changed, err)
		}
		if err := restarted.executeDeparture(context.Background(), fixture.project, fixture.wf, run.ID); err != nil {
			t.Fatal(err)
		}
		if count := departureLandingAuditCount(t, fixture.vault, "W-0001", "wave"); count != 1 {
			t.Fatalf("promoted replay duplicated the audit row: %d", count)
		}
	})

	t.Run("promote freezes the full wave and stages only its missing member", func(t *testing.T) {
		fixture := newMultiMemberDepartureExecutionFixture(t)
		store, err := OpenRuntimeStore(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		defer store.Close()
		run := createDepartureExecutionRun(t, store, fixture)
		if !sameDepartureStrings(run.Candidate.CargoTaskIDs, []string{"APP-T-0001", "APP-T-0002"}) ||
			run.Candidate.TaskSourceSHAs["APP-T-0001"] == "" {
			t.Fatalf("planner did not freeze the full selected wave: %#v", run.Candidate)
		}
		integrationBefore := gitRevisionForTest(t, fixture.repo, "integration/W-0001")
		sourceOne := run.Candidate.TaskSourceSHAs["APP-T-0001"]
		sourceTwo := run.Candidate.TaskSourceSHAs["APP-T-0002"]
		if !gitMergeBaseAncestor(fixture.repo, sourceOne, integrationBefore) ||
			gitMergeBaseAncestor(fixture.repo, sourceTwo, integrationBefore) {
			t.Fatalf("fixture ancestry is not one boarded/one waiting: integration=%s source1=%s source2=%s", integrationBefore, sourceOne, sourceTwo)
		}
		firstAuditBefore := departureLandingAuditCount(t, fixture.vault, "W-0001", "APP-T-0001")
		d := &Daemon{store: store, departurePlan: fixture.plan}
		if err := d.executeDeparture(context.Background(), fixture.project, fixture.wf, run.ID); err != nil {
			t.Fatal(err)
		}
		passed := mustDepartureRun(t, store, run.ID)
		if passed.State != DepartureStatePassed ||
			!gitMergeBaseAncestor(fixture.repo, sourceOne, passed.Promotion.CommittedSHA) ||
			!gitMergeBaseAncestor(fixture.repo, sourceTwo, passed.Promotion.CommittedSHA) {
			t.Fatalf("multi-member wave did not promote its exact full candidate: %#v", passed)
		}
		if gitShowFile(t, fixture.repo, "main", "member-one.txt") != "one\n" ||
			gitShowFile(t, fixture.repo, "main", "member-two.txt") != "two\n" {
			t.Fatal("multi-member promotion did not contain both members")
		}
		if after := departureLandingAuditCount(t, fixture.vault, "W-0001", "APP-T-0001"); after != firstAuditBefore {
			t.Fatalf("already-boarded member audit duplicated: before=%d after=%d", firstAuditBefore, after)
		}
		if after := departureLandingAuditCount(t, fixture.vault, "W-0001", "APP-T-0002"); after != 1 {
			t.Fatalf("missing member landing audit count = %d, want 1", after)
		}
	})

	t.Run("hold after evaluation blocks before side effects", func(t *testing.T) {
		fixture := newDepartureExecutionFixture(t, scheduledPromotionPromote, "held-executor.txt")
		store, err := OpenRuntimeStore(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		defer store.Close()
		run := createDepartureExecutionRun(t, store, fixture)
		mainBefore := gitRevisionForTest(t, fixture.repo, "main")
		integrationBefore := gitRevisionForTest(t, fixture.repo, "integration/W-0001")
		if _, err := store.SetDepartureHold("app", false, "operator maintenance", "human:sara", time.Now().UTC()); err != nil {
			t.Fatal(err)
		}
		d := &Daemon{store: store, departurePlan: fixture.plan}
		if err := d.executeDeparture(context.Background(), fixture.project, fixture.wf, run.ID); err != nil {
			t.Fatal(err)
		}
		blocked := mustDepartureRun(t, store, run.ID)
		if blocked.State != DepartureStateBlocked ||
			!strings.Contains(blocked.BlockReason, "human:sara") ||
			!strings.Contains(blocked.BlockReason, "tusker departure resume --project app --by <actor>") {
			t.Fatalf("hold was not durable/actionable: %#v", blocked)
		}
		if gitRevisionForTest(t, fixture.repo, "main") != mainBefore ||
			gitRevisionForTest(t, fixture.repo, "integration/W-0001") != integrationBefore {
			t.Fatal("held departure changed a Git ref")
		}
		if lease, err := store.FindResourceLease("gate:full"); err != nil || lease != nil {
			t.Fatalf("held departure touched full-gate resource: %#v err=%v", lease, err)
		}
	})

	t.Run("hold after default preparation blocks promotion intent", func(t *testing.T) {
		fixture := newDepartureExecutionFixture(t, scheduledPromotionPromote, "final-hold-executor.txt")
		store, err := OpenRuntimeStore(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		defer store.Close()
		run := createDepartureExecutionRun(t, store, fixture)
		mainBefore := gitRevisionForTest(t, fixture.repo, "main")
		originalHook := scheduledPromotionAfterDefaultPrepare
		hookCalled := false
		scheduledPromotionAfterDefaultPrepare = func() error {
			hookCalled = true
			_, err := store.SetDepartureHold("app", false, "last-second operator hold", "human:sara", time.Now().UTC())
			return err
		}
		defer func() { scheduledPromotionAfterDefaultPrepare = originalHook }()
		d := &Daemon{store: store, departurePlan: fixture.plan}
		if err := d.executeDeparture(context.Background(), fixture.project, fixture.wf, run.ID); err != nil {
			t.Fatal(err)
		}
		blocked := mustDepartureRun(t, store, run.ID)
		if !hookCalled || blocked.State != DepartureStateBlocked ||
			blocked.Promotion.AttemptedAt != "" ||
			!strings.Contains(blocked.BlockReason, "last-second operator hold") {
			t.Fatalf("final hold fence did not block before promotion intent: hook=%v run=%#v", hookCalled, blocked)
		}
		if after := gitRevisionForTest(t, fixture.repo, "main"); after != mainBefore {
			t.Fatalf("final hold fence moved main: before=%s after=%s", mainBefore, after)
		}
	})

	t.Run("resource contention waits wakes and retries", func(t *testing.T) {
		fixture := newDepartureExecutionFixture(t, scheduledPromotionPromote, "contended-executor.txt")
		store, err := OpenRuntimeStore(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		defer store.Close()
		run := createDepartureExecutionRun(t, store, fixture)
		mainBefore := gitRevisionForTest(t, fixture.repo, "main")
		holder, acquired, err := store.AcquireResourceLease(ResourceLeaseAcquireInput{
			Name: "gate:full", Owner: "other-departure", Purpose: "other full gate",
			ProjectID: "other-project", DepartureID: "other-run", TTL: 10 * time.Minute,
		})
		if err != nil || !acquired {
			t.Fatalf("contending holder acquire: %#v acquired=%v err=%v", holder, acquired, err)
		}
		var wakes []string
		resourceLeaseWaiterState.Lock()
		originalNotify := resourceLeaseWaiterState.notify
		resourceLeaseWaiterState.notify = func(_ string, resource, projectID string) {
			wakes = append(wakes, resource+"|"+projectID)
		}
		resourceLeaseWaiterState.Unlock()
		t.Cleanup(func() {
			resourceLeaseWaiterState.Lock()
			resourceLeaseWaiterState.notify = originalNotify
			resourceLeaseWaiterState.Unlock()
		})

		d := &Daemon{store: store, departurePlan: fixture.plan}
		if err := d.executeDeparture(context.Background(), fixture.project, fixture.wf, run.ID); err != nil {
			t.Fatal(err)
		}
		waiting := mustDepartureRun(t, store, run.ID)
		if waiting.State != DepartureStateGating || gitRevisionForTest(t, fixture.repo, "main") != mainBefore {
			t.Fatalf("contended departure did not remain resumable: %#v", waiting)
		}
		waiters, err := store.resourceLeaseWaitersLocked("gate:full")
		if err != nil || len(waiters) != 1 || waiters[0] != "app" {
			t.Fatalf("gate waiter not registered: %#v err=%v", waiters, err)
		}
		if released, err := store.ReleaseResourceLease(holder.Name, holder.Owner, holder.Generation, "other gate finished", time.Now().UTC()); err != nil || !released {
			t.Fatalf("release contending holder: released=%v err=%v", released, err)
		}
		if len(wakes) != 1 || wakes[0] != "gate:full|app" {
			t.Fatalf("resource release wake = %#v", wakes)
		}
		if err := d.executeDeparture(context.Background(), fixture.project, fixture.wf, run.ID); err != nil {
			t.Fatal(err)
		}
		passed := mustDepartureRun(t, store, run.ID)
		if passed.State != DepartureStatePassed || passed.Promotion.CommittedSHA != gitRevisionForTest(t, fixture.repo, "main") {
			t.Fatalf("resource retry did not promote: %#v", passed)
		}
	})

	t.Run("legacy candidate preserves every pinned drift fence", func(t *testing.T) {
		legacy := DepartureRun{
			Candidate: DepartureCandidate{
				TaskStateRevisions:       map[string]string{"APP-T-0001": "state-1"},
				TaskSourceSHAs:           map[string]string{"APP-T-0001": "source-1"},
				IntegrationBaseSHA:       "integration-1",
				ExpectedDefaultBranchSHA: "main-1",
			},
			Gate: DepartureGate{Command: "go test ./...", Profile: "full", Toolchain: "go-1", TreeHash: "tree-1"},
		}
		current := DepartureDecision{
			Candidate: DepartureCandidate{
				CargoTaskIDs:             []string{"APP-T-0001"},
				WaveIDs:                  []string{"W-0001"},
				TaskStateRevisions:       map[string]string{"APP-T-0001": "state-1"},
				TaskSourceSHAs:           map[string]string{"APP-T-0001": "source-1"},
				IntegrationBaseSHA:       "integration-1",
				ExpectedDefaultBranchSHA: "main-1",
			},
			GateIntent: legacy.Gate,
		}
		if drift := departurePlanningDrift(legacy, current); drift != "" {
			t.Fatalf("unchanged legacy pins drifted: %s", drift)
		}
		changedTask := current
		changedTask.Candidate.TaskStateRevisions = map[string]string{"APP-T-0001": "state-2"}
		if drift := departurePlanningDrift(legacy, changedTask); drift != "task" {
			t.Fatalf("legacy task drift = %q, want task", drift)
		}
		changedSource := current
		changedSource.Candidate.TaskSourceSHAs = map[string]string{"APP-T-0001": "source-2"}
		if drift := departurePlanningDrift(legacy, changedSource); drift != "source" {
			t.Fatalf("legacy source drift = %q, want source", drift)
		}
	})

	t.Run("daemon close waits for active departure workers", func(t *testing.T) {
		d, err := NewDaemon(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		if !d.claimDepartureExecution("dep-close-test") {
			t.Fatal("failed to register active departure worker")
		}
		closed := make(chan error, 1)
		go func() { closed <- d.Close() }()
		deadline := time.Now().Add(5 * time.Second)
		for {
			d.departureMu.Lock()
			closing := d.departureClosing
			d.departureMu.Unlock()
			if closing {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("daemon close did not enter departure shutdown")
			}
			time.Sleep(time.Millisecond)
		}
		select {
		case err := <-closed:
			t.Fatalf("daemon closed its store while a departure was active: %v", err)
		default:
		}
		d.releaseDepartureExecution("dep-close-test")
		select {
		case err := <-closed:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("daemon close did not finish after the departure worker exited")
		}
		if d.claimDepartureExecution("dep-after-close") {
			t.Fatal("daemon accepted a departure worker after close began")
		}
	})

	t.Run("configured promotion fences legacy and absent policy stays compatible", func(t *testing.T) {
		configuredRepo, configuredVault := newLandReadyForMainAdvanceTest(t, "configured-fence.txt", "configured\n")
		setScheduledPromotionPolicyForTest(t, configuredVault, scheduledPromotionPromote)
		configuredBefore := gitRevisionForTest(t, configuredRepo, "main")
		err := landV7Cmd(Args{"vault": configuredVault, "quiet": "true", "_pos0": "W-0001"})
		if err == nil || !strings.Contains(err.Error(), "configured departures own main promotion") {
			t.Fatalf("configured manual landing was not fenced: %v", err)
		}
		if gitRevisionForTest(t, configuredRepo, "main") != configuredBefore {
			t.Fatal("configured legacy landing moved main")
		}

		legacyRepo, legacyVault := newLandReadyForMainAdvanceTest(t, "legacy-compatible.txt", "legacy\n")
		legacyBefore := gitRevisionForTest(t, legacyRepo, "main")
		if err := landV7Cmd(Args{"vault": legacyVault, "quiet": "true", "_pos0": "W-0001"}); err != nil {
			t.Fatalf("absent policy broke legacy landing: %v", err)
		}
		if gitRevisionForTest(t, legacyRepo, "main") == legacyBefore {
			t.Fatal("absent policy no longer preserves legacy main advance")
		}
	})
}

func newDepartureExecutionFixture(t *testing.T, mode, fileName string) departureExecutionFixture {
	t.Helper()
	repo, vault := newLandReadyForMainAdvanceTest(t, fileName, "departure\n")
	sourceSHA := gitRevisionForTest(t, repo, "task/APP-T-0001")
	setDepartureTaskSourceForTest(t, vault, "APP-T-0001", sourceSHA)
	return configureDepartureExecutionFixture(t, repo, vault, mode, []string{"go version >/dev/null && test -f " + yamlQuoteForShellTest(fileName)})
}

func newUnstagedDepartureExecutionFixture(t *testing.T, mode, fileName string) departureExecutionFixture {
	t.Helper()
	repo, vault := newLandTestRepo(t, 1, "test -f "+yamlQuoteForShellTest(fileName))
	sourceSHA := commitLandBranch(t, repo, "task/APP-T-0001", "integration/W-0001", map[string]string{fileName: "departure\n"})
	setWaveTaskState(t, vault, "APP-T-0001", "done", "done", "2026-07-25T00:00:00Z")
	setDepartureTaskSourceForTest(t, vault, "APP-T-0001", sourceSHA)
	return configureDepartureExecutionFixture(t, repo, vault, mode, []string{"go version >/dev/null && test -f " + yamlQuoteForShellTest(fileName)})
}

func newMultiMemberDepartureExecutionFixture(t *testing.T) departureExecutionFixture {
	t.Helper()
	repo, vault := newLandTestRepo(t, 2, "test -f member-one.txt")
	sourceOne := commitLandBranch(t, repo, "task/APP-T-0001", "integration/W-0001", map[string]string{"member-one.txt": "one\n"})
	if err := landV7Cmd(Args{"vault": vault, "quiet": "true", "_pos0": "APP-T-0001"}); err != nil {
		t.Fatal(err)
	}
	setWaveTaskState(t, vault, "APP-T-0001", "done", "done", "2026-07-25T00:00:00Z")
	setDepartureTaskSourceForTest(t, vault, "APP-T-0001", sourceOne)
	commitCanonicalTaskStateToIntegration(t, repo, vault, "APP-T-0001")
	clearDepartureTaskSourceForTest(t, vault, "APP-T-0001")

	setWaveTaskState(t, vault, "APP-T-0002", "done", "done", "2026-07-25T00:01:00Z")
	taskTwoRel := filepath.ToSlash(filepath.Join(".tusker", "work", "tasks", "APP-T-0002.md"))
	sourceTwo := commitLandBranch(t, repo, "task/APP-T-0002", "integration/W-0001", map[string]string{
		"member-two.txt": "two\n",
		taskTwoRel:       mustReadIndexTest(t, filepath.Join(vault, "work", "tasks", "APP-T-0002.md")),
	})
	setDepartureTaskSourceForTest(t, vault, "APP-T-0002", sourceTwo)
	return configureDepartureExecutionFixture(t, repo, vault, scheduledPromotionPromote, []string{
		"go version >/dev/null && test -f member-one.txt && test -f member-two.txt",
	})
}

func configureDepartureExecutionFixture(t *testing.T, repo, vault, mode string, gateCommands []string) departureExecutionFixture {
	t.Helper()
	setScheduledPromotionPolicyForTest(t, vault, mode)
	wf := setScheduledPromotionGateForTest(t, vault, gateCommands, "full")
	armScheduledPromotionWaveForTest(t, vault, "W-0001")
	commitScheduledPromotionWorkflowForTest(t, repo, vault)
	remote := filepath.Join(t.TempDir(), "origin.git")
	runGitDir(t, filepath.Dir(remote), "init", "--bare", remote)
	runGitDir(t, repo, "remote", "add", "origin", remote)
	runGitDir(t, repo, "push", "-u", "origin", "main")
	project := RegisteredProject{ProjectID: "app", RepoRoot: repo, VaultRoot: vault}
	planner := defaultDeparturePlanner()
	planner.gateLookup = func(string, string, []string, string, string) bool { return false }
	return departureExecutionFixture{
		repo: repo, vault: vault, project: project, wf: wf,
		plan: func(project RegisteredProject, wf Workflow) (DepartureDecision, error) {
			return planner.PlanDeparture(project.VaultRoot, project.ProjectID, WorkflowFile{Data: wf})
		},
	}
}

func setDepartureTaskSourceForTest(t *testing.T, vault, taskID, sourceSHA string) {
	t.Helper()
	path := filepath.Join(vault, "work", "tasks", taskID+".md")
	data, body, err := parseFrontmatterMustRead(path)
	if err != nil {
		t.Fatal(err)
	}
	baseRev := stringField(data, "state_rev")
	data["source_sha"] = sourceSHA
	if _, err := saveV7DocumentCAS(path, data, body, v7FrontmatterOrder["task"], baseRev); err != nil {
		t.Fatal(err)
	}
}

func clearDepartureTaskSourceForTest(t *testing.T, vault, taskID string) {
	t.Helper()
	path := filepath.Join(vault, "work", "tasks", taskID+".md")
	data, body, err := parseFrontmatterMustRead(path)
	if err != nil {
		t.Fatal(err)
	}
	baseRev := stringField(data, "state_rev")
	delete(data, "source_sha")
	delete(data, "source_commit")
	delete(data, "source_branch_sha")
	if _, err := saveV7DocumentCAS(path, data, body, v7FrontmatterOrder["task"], baseRev); err != nil {
		t.Fatal(err)
	}
}

func createDepartureExecutionRun(t *testing.T, store *RuntimeStore, fixture departureExecutionFixture) DepartureRun {
	t.Helper()
	decision, err := fixture.plan(fixture.project, fixture.wf)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Disposition != "ready" && decision.Disposition != "already_gated" {
		t.Fatalf("fixture departure decision = %#v", decision)
	}
	run, created, err := store.GetOrCreateDepartureRun(DepartureRun{
		ProjectID: "app", PolicyID: departurePolicyID(fixture.wf.ScheduledPromotion.Effective),
		ScheduledWindow: time.Now().UTC().Format(time.RFC3339Nano),
		State:           DepartureStateEvaluating, Candidate: decision.Candidate, Gate: decision.GateIntent,
	})
	if err != nil || !created {
		t.Fatalf("create departure run: %#v created=%v err=%v", run, created, err)
	}
	return run
}

func mustDepartureRun(t *testing.T, store *RuntimeStore, runID string) DepartureRun {
	t.Helper()
	run, err := store.FindDepartureRun(runID)
	if err != nil || run == nil {
		t.Fatalf("find departure %s: %#v err=%v", runID, run, err)
	}
	return *run
}

func waitDepartureState(t *testing.T, store *RuntimeStore, runID string, state DepartureState) DepartureRun {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		run := mustDepartureRun(t, store, runID)
		if run.State == state {
			return run
		}
		if departureTerminal(run.State) && run.State != state {
			t.Fatalf("departure reached %s while waiting for %s: %#v", run.State, state, run)
		}
		time.Sleep(20 * time.Millisecond)
	}
	run := mustDepartureRun(t, store, runID)
	t.Fatalf("departure state = %s after timeout, want %s: %#v", run.State, state, run)
	return DepartureRun{}
}

func gitRevisionForTest(t *testing.T, repo, ref string) string {
	t.Helper()
	return strings.TrimSpace(gitDirOutput(t, repo, "rev-parse", ref))
}

func departureLandingAuditCount(t *testing.T, vault, waveID, taskID string) int {
	t.Helper()
	data, _, err := parseFrontmatterMustRead(filepath.Join(vault, "work", "waves", waveID+".md"))
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, row := range normalizeLandingAudit(data["landings"]) {
		if stringField(row, "task") == taskID {
			count++
		}
	}
	return count
}

func removeDeparturePromotionAudit(t *testing.T, vault, waveID, commit string) {
	t.Helper()
	path := filepath.Join(vault, "work", "waves", waveID+".md")
	data, body, err := parseFrontmatterMustRead(path)
	if err != nil {
		t.Fatal(err)
	}
	baseRev := stringField(data, "state_rev")
	var kept []map[string]any
	for _, row := range normalizeLandingAudit(data["landings"]) {
		if stringField(row, "task") == "wave" && stringField(row, "commit") == commit {
			continue
		}
		kept = append(kept, row)
	}
	data["landings"] = kept
	if _, err := saveV7DocumentCAS(path, data, body, v7FrontmatterOrder["wave"], baseRev); err != nil {
		t.Fatal(err)
	}
}

func withDepartureState(run DepartureRun, state DepartureState) DepartureRun {
	run.State = state
	return run
}
