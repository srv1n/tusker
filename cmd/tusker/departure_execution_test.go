package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type departureExecutionFixture struct {
	repo      string
	vault     string
	stateRoot string
	project   RegisteredProject
	wf        Workflow
	plan      func(RegisteredProject, Workflow) (DepartureDecision, error)
}

func TestDepartureExecution(t *testing.T) {
	t.Run("stage keeps main fixed and replay does not duplicate audit", func(t *testing.T) {
		fixture := newUnstagedDepartureExecutionFixture(t, scheduledPromotionStage, "stage-executor.txt")
		store, err := OpenRuntimeStore(fixture.stateRoot)
		if err != nil {
			t.Fatal(err)
		}
		defer store.Close()
		run := createDepartureExecutionRun(t, store, fixture)
		mainBefore := gitRevisionForTest(t, fixture.repo, "main")
		integrationBefore := gitRevisionForTest(t, fixture.repo, "integration/W-0001")
		auditBefore := departureLandingAuditCount(t, fixture.vault, "W-0001", "APP-T-0001")
		sourceSHA := run.Candidate.TaskSourceSHAs["APP-T-0001"]
		advancedSHA := advanceDepartureTaskBranch(t, fixture.repo, "task/APP-T-0001", map[string]string{"after-freeze.txt": "must stay off integration\n"})
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
		if integrationAfter == integrationBefore ||
			!gitMergeBaseAncestor(fixture.repo, sourceSHA, integrationAfter) ||
			gitMergeBaseAncestor(fixture.repo, advancedSHA, integrationAfter) {
			t.Fatalf("eligible cargo was not staged at the frozen source: before=%s after=%s source=%s advanced=%s", integrationBefore, integrationAfter, sourceSHA, advancedSHA)
		}
		if got := gitShowFile(t, fixture.repo, "integration/W-0001", "stage-executor.txt"); got != "departure\n" {
			t.Fatalf("staged content = %q", got)
		}
		if gitShowFileOK(fixture.repo, "integration/W-0001", "after-freeze.txt") {
			t.Fatal("scheduled staging boarded work committed after the frozen source")
		}
		if branchHead := gitRevisionForTest(t, fixture.repo, "task/APP-T-0001"); branchHead != advancedSHA {
			t.Fatalf("scheduled staging moved the user task branch: got=%s want=%s", branchHead, advancedSHA)
		}
		auditAfter := departureLandingAuditCount(t, fixture.vault, "W-0001", "APP-T-0001")
		if auditAfter != auditBefore+1 {
			t.Fatalf("stage audit count = %d, want %d", auditAfter, auditBefore+1)
		}
		assertDepartureLandingSource(t, fixture.vault, "W-0001", "APP-T-0001", sourceSHA)
		removeDepartureTaskLandingAudit(t, fixture.vault, "W-0001", "APP-T-0001", sourceSHA)
		current := mustDepartureRun(t, store, run.ID)
		if changed, err := store.TransitionDepartureRun(withDepartureState(current, DepartureStateStaging), current.StateRevision); err != nil || !changed {
			t.Fatalf("prepare exact staging replay: changed=%v err=%v", changed, err)
		}
		if err := d.executeDeparture(context.Background(), fixture.project, fixture.wf, run.ID); err != nil {
			t.Fatal(err)
		}
		if after := gitRevisionForTest(t, fixture.repo, "integration/W-0001"); after != integrationAfter {
			t.Fatalf("exact staging replay changed integration: before=%s after=%s", integrationAfter, after)
		}
		if after := departureLandingAuditCount(t, fixture.vault, "W-0001", "APP-T-0001"); after != auditAfter {
			t.Fatalf("stage replay audit count: before=%d after=%d", auditAfter, after)
		}
		assertDepartureLandingSource(t, fixture.vault, "W-0001", "APP-T-0001", sourceSHA)
		clearDepartureTaskSourceForTest(t, fixture.vault, "APP-T-0001")
		idx, err := loadV7Index(fixture.vault)
		if err != nil {
			t.Fatal(err)
		}
		recovered, err := scheduledPromotionTaskSourceSHAWithStore(fixture.repo, integrationAfter, "integration/W-0001", idx.Waves["W-0001"], idx.Tasks["APP-T-0001"], store)
		if err != nil || recovered != sourceSHA {
			t.Fatalf("exact audit did not fence mutable branch fallback: recovered=%s want=%s err=%v", recovered, sourceSHA, err)
		}
	})

	t.Run("stage batches frozen sources across multiple waves", func(t *testing.T) {
		fixture := newMultiWaveDepartureExecutionFixture(t)
		store, err := OpenRuntimeStore(fixture.stateRoot)
		if err != nil {
			t.Fatal(err)
		}
		defer store.Close()
		run := createDepartureExecutionRun(t, store, fixture)
		if len(run.Candidate.CargoTaskIDs) != 4 || !sameDepartureStrings(run.Candidate.WaveIDs, []string{"W-0001", "W-0002"}) {
			t.Fatalf("multi-wave candidate = %#v", run.Candidate)
		}
		mainBefore := gitRevisionForTest(t, fixture.repo, "main")
		integrationBefore := map[string]string{
			"W-0001": gitRevisionForTest(t, fixture.repo, "integration/W-0001"),
			"W-0002": gitRevisionForTest(t, fixture.repo, "integration/W-0002"),
		}
		advanced := map[string]string{}
		for _, taskID := range run.Candidate.CargoTaskIDs {
			advanced[taskID] = advanceDepartureTaskBranch(t, fixture.repo, "task/"+taskID, map[string]string{
				"after-freeze-" + strings.ToLower(taskID) + ".txt": "must stay off integration\n",
			})
		}
		d := &Daemon{store: store, departurePlan: fixture.plan}
		if err := d.executeDeparture(context.Background(), fixture.project, fixture.wf, run.ID); err != nil {
			t.Fatal(err)
		}
		if got := mustDepartureRun(t, store, run.ID); got.State != DepartureStatePassed {
			t.Fatalf("multi-wave stage state = %s", got.State)
		}
		if after := gitRevisionForTest(t, fixture.repo, "main"); after != mainBefore {
			t.Fatalf("multi-wave stage moved main: before=%s after=%s", mainBefore, after)
		}
		integrationAfter := map[string]string{}
		for _, waveID := range []string{"W-0001", "W-0002"} {
			integrationAfter[waveID] = gitRevisionForTest(t, fixture.repo, "integration/"+waveID)
			if integrationAfter[waveID] == integrationBefore[waveID] {
				t.Fatalf("%s integration did not advance", waveID)
			}
		}
		for i, taskID := range run.Candidate.CargoTaskIDs {
			waveID := "W-0001"
			if i >= 2 {
				waveID = "W-0002"
			}
			sourceSHA := run.Candidate.TaskSourceSHAs[taskID]
			if !gitMergeBaseAncestor(fixture.repo, sourceSHA, integrationAfter[waveID]) ||
				gitMergeBaseAncestor(fixture.repo, advanced[taskID], integrationAfter[waveID]) {
				t.Fatalf("%s staged source drifted in %s: frozen=%s advanced=%s", taskID, waveID, sourceSHA, advanced[taskID])
			}
			if branchHead := gitRevisionForTest(t, fixture.repo, "task/"+taskID); branchHead != advanced[taskID] {
				t.Fatalf("%s user branch moved: got=%s want=%s", taskID, branchHead, advanced[taskID])
			}
			assertDepartureLandingSource(t, fixture.vault, waveID, taskID, sourceSHA)
		}

		removeDepartureTaskLandingAudit(t, fixture.vault, "W-0001", "APP-T-0001", run.Candidate.TaskSourceSHAs["APP-T-0001"])
		removeDepartureTaskLandingAudit(t, fixture.vault, "W-0002", "APP-T-0003", run.Candidate.TaskSourceSHAs["APP-T-0003"])
		current := mustDepartureRun(t, store, run.ID)
		if changed, err := store.TransitionDepartureRun(withDepartureState(current, DepartureStateStaging), current.StateRevision); err != nil || !changed {
			t.Fatalf("prepare multi-wave replay: changed=%v err=%v", changed, err)
		}
		if err := d.executeDeparture(context.Background(), fixture.project, fixture.wf, run.ID); err != nil {
			t.Fatal(err)
		}
		for _, waveID := range []string{"W-0001", "W-0002"} {
			if after := gitRevisionForTest(t, fixture.repo, "integration/"+waveID); after != integrationAfter[waveID] {
				t.Fatalf("%s replay changed integration: before=%s after=%s", waveID, integrationAfter[waveID], after)
			}
		}
		for i, taskID := range run.Candidate.CargoTaskIDs {
			waveID := "W-0001"
			if i >= 2 {
				waveID = "W-0002"
			}
			assertDepartureLandingSource(t, fixture.vault, waveID, taskID, run.Candidate.TaskSourceSHAs[taskID])
			if count := departureLandingAuditCount(t, fixture.vault, waveID, taskID); count != 1 {
				t.Fatalf("%s replay audit count = %d, want 1", taskID, count)
			}
		}
	})

	t.Run("stage refuses a mutable source ref with named drift", func(t *testing.T) {
		fixture := newUnstagedDepartureExecutionFixture(t, scheduledPromotionStage, "mutable-source-ref.txt")
		store, err := OpenRuntimeStore(fixture.stateRoot)
		if err != nil {
			t.Fatal(err)
		}
		defer store.Close()
		run := createDepartureExecutionRun(t, store, fixture)
		next := run
		next.State = DepartureStateStaging
		next.Candidate.TaskSourceSHAs = map[string]string{"APP-T-0001": "task/APP-T-0001"}
		if changed, err := store.TransitionDepartureRun(next, run.StateRevision); err != nil || !changed {
			t.Fatalf("prepare mutable-source refusal: changed=%v err=%v", changed, err)
		}
		mainBefore := gitRevisionForTest(t, fixture.repo, "main")
		integrationBefore := gitRevisionForTest(t, fixture.repo, "integration/W-0001")
		branchBefore := gitRevisionForTest(t, fixture.repo, "task/APP-T-0001")
		d := &Daemon{store: store, departurePlan: fixture.plan}
		if err := d.executeDeparture(context.Background(), fixture.project, fixture.wf, run.ID); err != nil {
			t.Fatal(err)
		}
		blocked := mustDepartureRun(t, store, run.ID)
		if blocked.State != DepartureStateBlocked ||
			!strings.Contains(blocked.BlockReason, "task_source_not_immutable:APP-T-0001") ||
			!strings.Contains(blocked.BlockReason, "full immutable commit SHA") {
			t.Fatalf("mutable source did not produce an actionable immutable-source refusal: %#v", blocked)
		}
		if gitRevisionForTest(t, fixture.repo, "main") != mainBefore ||
			gitRevisionForTest(t, fixture.repo, "integration/W-0001") != integrationBefore ||
			gitRevisionForTest(t, fixture.repo, "task/APP-T-0001") != branchBefore {
			t.Fatal("mutable-source refusal changed a Git ref")
		}
	})

	t.Run("promote resumes in a fresh daemon and replays promoted audit", func(t *testing.T) {
		fixture := newDepartureExecutionFixture(t, scheduledPromotionPromote, "promote-executor.txt")
		stateRoot := fixture.stateRoot
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
		store, err := OpenRuntimeStore(fixture.stateRoot)
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
		if firstAuditBefore != 1 {
			t.Fatalf("already-boarded member exact-source audit count = %d, want 1", firstAuditBefore)
		}
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
			t.Fatalf("already-boarded member duplicated its exact-source audit: before=%d after=%d", firstAuditBefore, after)
		}
		if after := departureLandingAuditCount(t, fixture.vault, "W-0001", "APP-T-0002"); after != 1 {
			t.Fatalf("missing member landing audit count = %d, want 1", after)
		}
		assertDepartureLandingSource(t, fixture.vault, "W-0001", "APP-T-0001", sourceOne)
		assertDepartureLandingSource(t, fixture.vault, "W-0001", "APP-T-0002", sourceTwo)
	})

	for _, mode := range []string{scheduledPromotionStage, scheduledPromotionPromote} {
		t.Run(mode+" blocks a frozen wave when an accepted sibling regresses", func(t *testing.T) {
			fixture := newMultiMemberDepartureExecutionFixture(t)
			fixture.wf.ScheduledPromotion.Effective = scheduledPromotionProjection(ScheduledPromotionPolicy{Mode: mode}, true, "test")
			// Keep both exact sources explicit so APP-T-0001 selects the wave
			// after APP-T-0002 regresses while retaining its own source_sha.
			setDepartureTaskSourceForTest(t, fixture.vault, "APP-T-0001", gitRevisionForTest(t, fixture.repo, "task/APP-T-0001"))
			armScheduledPromotionWaveForTest(t, fixture.vault, "W-0001")
			store, err := OpenRuntimeStore(fixture.stateRoot)
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			run := createDepartureExecutionRun(t, store, fixture)
			if !sameDepartureStrings(run.Candidate.CargoTaskIDs, []string{"APP-T-0001", "APP-T-0002"}) {
				t.Fatalf("fully accepted wave was not frozen atomically: %#v", run.Candidate)
			}
			if changed, err := store.TransitionDepartureRun(withDepartureState(run, DepartureStateStaging), run.StateRevision); err != nil || !changed {
				t.Fatalf("prepare staging replay: changed=%v err=%v", changed, err)
			}
			mainBefore := gitRevisionForTest(t, fixture.repo, "main")
			integrationBefore := gitRevisionForTest(t, fixture.repo, "integration/W-0001")
			setWaveTaskState(t, fixture.vault, "APP-T-0002", "ready", "ready", "")

			idx, err := loadV7Index(fixture.vault)
			if err != nil {
				t.Fatal(err)
			}
			if stringField(idx.Tasks["APP-T-0002"].Data, "source_sha") == "" {
				t.Fatal("regression fixture lost the active sibling's exact source_sha")
			}
			decision, err := fixture.plan(fixture.project, fixture.wf)
			if err != nil {
				t.Fatal(err)
			}
			if decision.Disposition != "blocked" ||
				len(decision.Reasons) == 0 ||
				decision.Reasons[0].Code != "wave_member_not_done" ||
				containsString(decision.Candidate.CargoTaskIDs, "APP-T-0002") {
				t.Fatalf("planner widened cargo from membership after sibling regression: %#v", decision)
			}

			d := &Daemon{store: store, departurePlan: fixture.plan}
			if err := d.executeDeparture(context.Background(), fixture.project, fixture.wf, run.ID); err != nil {
				t.Fatal(err)
			}
			blocked := mustDepartureRun(t, store, run.ID)
			if blocked.State != DepartureStateBlocked || !strings.Contains(blocked.BlockReason, "wave_member_not_done:APP-T-0002") {
				t.Fatalf("staging replay did not enforce accepted full-wave authority: %#v", blocked)
			}
			if after := gitRevisionForTest(t, fixture.repo, "integration/W-0001"); after != integrationBefore {
				t.Fatalf("%s staged a regressed sibling: before=%s after=%s", mode, integrationBefore, after)
			}
			if after := gitRevisionForTest(t, fixture.repo, "main"); after != mainBefore {
				t.Fatalf("%s promoted a wave with an active sibling: before=%s after=%s", mode, mainBefore, after)
			}
			if count := departureLandingAuditCount(t, fixture.vault, "W-0001", "APP-T-0002"); count != 0 {
				t.Fatalf("%s recorded a landing for the regressed sibling: %d", mode, count)
			}
		})
	}

	t.Run("promote blocks a frozen done sibling without a reviewer acceptor", func(t *testing.T) {
		fixture := newMultiMemberDepartureExecutionFixture(t)
		setDepartureTaskSourceForTest(t, fixture.vault, "APP-T-0001", gitRevisionForTest(t, fixture.repo, "task/APP-T-0001"))
		armScheduledPromotionWaveForTest(t, fixture.vault, "W-0001")
		store, err := OpenRuntimeStore(fixture.stateRoot)
		if err != nil {
			t.Fatal(err)
		}
		defer store.Close()
		run := createDepartureExecutionRun(t, store, fixture)
		if changed, err := store.TransitionDepartureRun(withDepartureState(run, DepartureStateStaging), run.StateRevision); err != nil || !changed {
			t.Fatalf("prepare staging replay: changed=%v err=%v", changed, err)
		}
		mainBefore := gitRevisionForTest(t, fixture.repo, "main")
		integrationBefore := gitRevisionForTest(t, fixture.repo, "integration/W-0001")
		setDepartureTaskAcceptorForTest(t, fixture.vault, "APP-T-0002", "agent:worker")

		decision, err := fixture.plan(fixture.project, fixture.wf)
		if err != nil {
			t.Fatal(err)
		}
		if decision.Disposition != "blocked" ||
			len(decision.Reasons) == 0 ||
			decision.Reasons[0].Code != "wave_member_review_provenance_invalid" {
			t.Fatalf("planner accepted an unreviewed done sibling: %#v", decision)
		}
		d := &Daemon{store: store, departurePlan: fixture.plan}
		if err := d.executeDeparture(context.Background(), fixture.project, fixture.wf, run.ID); err != nil {
			t.Fatal(err)
		}
		blocked := mustDepartureRun(t, store, run.ID)
		if blocked.State != DepartureStateBlocked || !strings.Contains(blocked.BlockReason, "task_review_acceptor_invalid:APP-T-0002") {
			t.Fatalf("executor accepted an unreviewed durable sibling: %#v", blocked)
		}
		if after := gitRevisionForTest(t, fixture.repo, "integration/W-0001"); after != integrationBefore {
			t.Fatalf("unreviewed sibling moved integration: before=%s after=%s", integrationBefore, after)
		}
		if after := gitRevisionForTest(t, fixture.repo, "main"); after != mainBefore {
			t.Fatalf("unreviewed sibling moved main: before=%s after=%s", mainBefore, after)
		}
		if count := departureLandingAuditCount(t, fixture.vault, "W-0001", "APP-T-0002"); count != 0 {
			t.Fatalf("unreviewed sibling gained a landing audit: %d", count)
		}
	})

	t.Run("hold after evaluation blocks before side effects", func(t *testing.T) {
		fixture := newDepartureExecutionFixture(t, scheduledPromotionPromote, "held-executor.txt")
		store, err := OpenRuntimeStore(fixture.stateRoot)
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

	t.Run("hold after default preparation restores canonical bytes", func(t *testing.T) {
		fixture := newDepartureExecutionFixture(t, scheduledPromotionPromote, "final-hold-executor.txt")
		store, err := OpenRuntimeStore(fixture.stateRoot)
		if err != nil {
			t.Fatal(err)
		}
		defer store.Close()
		run := createDepartureExecutionRun(t, store, fixture)
		mainBefore := gitRevisionForTest(t, fixture.repo, "main")
		var canonicalBefore map[string]string
		var canonicalModesBefore map[string]os.FileMode
		originalBeforeHook := scheduledPromotionBeforeDefaultPrepare
		scheduledPromotionBeforeDefaultPrepare = func() error {
			canonicalBefore = departureWaveMemberBytes(t, fixture.vault, "W-0001", "APP-T-0001")
			canonicalModesBefore = departureWaveMemberModes(t, canonicalBefore)
			return nil
		}
		originalAfterHook := scheduledPromotionAfterDefaultPrepare
		hookCalled := false
		scheduledPromotionAfterDefaultPrepare = func() error {
			hookCalled = true
			_, err := store.SetDepartureHold("app", false, "last-second operator hold", "human:sara", time.Now().UTC())
			return err
		}
		defer func() {
			scheduledPromotionBeforeDefaultPrepare = originalBeforeHook
			scheduledPromotionAfterDefaultPrepare = originalAfterHook
		}()
		d := &Daemon{store: store, departurePlan: fixture.plan}
		if err := d.executeDeparture(context.Background(), fixture.project, fixture.wf, run.ID); err != nil {
			t.Fatal(err)
		}
		blocked := mustDepartureRun(t, store, run.ID)
		if !hookCalled || blocked.State != DepartureStateBlocked ||
			blocked.Promotion.AttemptedAt == "" ||
			!strings.Contains(blocked.BlockReason, "last-second operator hold") {
			t.Fatalf("post-intent hold fence was not durable: hook=%v run=%#v", hookCalled, blocked)
		}
		if after := gitRevisionForTest(t, fixture.repo, "main"); after != mainBefore {
			t.Fatalf("final hold fence moved main: before=%s after=%s", mainBefore, after)
		}
		assertDepartureWaveMemberBytes(t, fixture.vault, canonicalBefore, canonicalModesBefore)
	})

	t.Run("late disarm is revalidated before preparation", func(t *testing.T) {
		fixture := newDepartureExecutionFixture(t, scheduledPromotionPromote, "final-disarm-executor.txt")
		store, err := OpenRuntimeStore(fixture.stateRoot)
		if err != nil {
			t.Fatal(err)
		}
		defer store.Close()
		run := createDepartureExecutionRun(t, store, fixture)
		mainBefore := gitRevisionForTest(t, fixture.repo, "main")
		originalHook := scheduledPromotionBeforeDefaultPrepare
		hookCalled := false
		var disarmedBytes map[string]string
		scheduledPromotionBeforeDefaultPrepare = func() error {
			hookCalled = true
			if err := mutateWaveAuthorization(Args{
				"vault": fixture.vault, "_pos0": "W-0001",
				"by": "human:sara", "quiet": "true",
			}, "disarmed", nil); err != nil {
				return err
			}
			disarmedBytes = departureWaveMemberBytes(t, fixture.vault, "W-0001", "APP-T-0001")
			return nil
		}
		defer func() { scheduledPromotionBeforeDefaultPrepare = originalHook }()
		d := &Daemon{store: store, departurePlan: fixture.plan}
		if err := d.executeDeparture(context.Background(), fixture.project, fixture.wf, run.ID); err != nil {
			t.Fatal(err)
		}
		blocked := mustDepartureRun(t, store, run.ID)
		if !hookCalled || blocked.State != DepartureStateBlocked ||
			blocked.Promotion.AttemptedAt != "" ||
			!strings.Contains(strings.ToLower(blocked.BlockReason), "authorization") {
			t.Fatalf("late disarm was not fenced before promotion intent: hook=%v run=%#v", hookCalled, blocked)
		}
		if after := gitRevisionForTest(t, fixture.repo, "main"); after != mainBefore {
			t.Fatalf("late disarm moved main: before=%s after=%s", mainBefore, after)
		}
		assertDepartureWaveMemberBytes(t, fixture.vault, disarmedBytes)
	})

	t.Run("resource contention waits wakes and retries", func(t *testing.T) {
		fixture := newDepartureExecutionFixture(t, scheduledPromotionPromote, "contended-executor.txt")
		store, err := OpenRuntimeStore(fixture.stateRoot)
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

	t.Run("daemon close cancels a blocking full gate before waiting", func(t *testing.T) {
		if _, err := landingGateSandboxPath(); err != nil {
			t.Skip("isolated full gate unavailable: " + err.Error())
		}
		stateRoot := t.TempDir()
		repo, vault := newLandReadyForMainAdvanceTestInStateRoot(t, "cancel-gate.txt", "departure\n", stateRoot)
		sourceSHA := gitRevisionForTest(t, repo, "task/APP-T-0001")
		setDepartureTaskSourceForTest(t, vault, "APP-T-0001", sourceSHA)
		fixture := configureDepartureExecutionFixture(t, repo, vault, stateRoot, scheduledPromotionPromote, []string{
			"sleep 300",
		})
		gateStarted := make(chan struct{})
		oldGateHook := scheduledPromotionBeforeFullGateCommand
		scheduledPromotionBeforeFullGateCommand = func(string) { close(gateStarted) }
		defer func() { scheduledPromotionBeforeFullGateCommand = oldGateHook }()
		d, err := NewDaemon(stateRoot)
		if err != nil {
			t.Fatal(err)
		}
		closed := false
		defer func() {
			if !closed {
				_ = d.Close()
			}
		}()
		d.departurePlan = fixture.plan
		run := createDepartureExecutionRun(t, d.store, fixture)
		if err := d.startPendingDepartureExecutions(context.Background(), fixture.project, fixture.wf); err != nil {
			t.Fatal(err)
		}
		select {
		case <-gateStarted:
		case <-time.After(15 * time.Second):
			t.Fatal("blocking full gate did not start")
		}
		startedClose := time.Now()
		closeResult := make(chan error, 1)
		go func() { closeResult <- d.Close() }()
		select {
		case err := <-closeResult:
			if err != nil {
				t.Fatal(err)
			}
			closed = true
		case <-time.After(5 * time.Second):
			t.Fatal("daemon Close waited on a full-gate process without cancelling it")
		}
		if elapsed := time.Since(startedClose); elapsed >= 5*time.Second {
			t.Fatalf("daemon Close cancellation took %s", elapsed)
		}
		d.departureMu.Lock()
		active := len(d.departureActive)
		d.departureMu.Unlock()
		if active != 0 {
			t.Fatalf("departure worker remained active after Close: %d", active)
		}
		reopened, err := OpenRuntimeStore(stateRoot)
		if err != nil {
			t.Fatal(err)
		}
		defer reopened.Close()
		durable := mustDepartureRun(t, reopened, run.ID)
		if durable.State != DepartureStateGating ||
			durable.Promotion.AttemptedAt != "" ||
			durable.Gate.Failure.Identity != "" {
			t.Fatalf("cancelled full gate was misclassified as a product failure: %#v", durable)
		}
	})

	t.Run("daemon close preserves a promoted row cancelled during replay", func(t *testing.T) {
		fixture := newDepartureExecutionFixture(t, scheduledPromotionPromote, "cancel-promoted-replay.txt")
		stateRoot := fixture.stateRoot
		d, err := NewDaemon(stateRoot)
		if err != nil {
			t.Fatal(err)
		}
		run := createDepartureExecutionRun(t, d.store, fixture)
		promoted := run
		promoted.State = DepartureStatePromoted
		promoted.Promotion = DeparturePromotion{
			CommittedRef: "main",
			CommittedSHA: gitRevisionForTest(t, fixture.repo, "main"),
			CommittedAt:  time.Now().UTC().Format(time.RFC3339Nano),
		}
		if changed, err := d.store.TransitionDepartureRun(promoted, run.StateRevision); err != nil || !changed {
			t.Fatalf("prepare promoted replay: changed=%v err=%v", changed, err)
		}
		replayStarted := make(chan struct{})
		oldHook := scheduledPromotionBeforeCommittedReplay
		scheduledPromotionBeforeCommittedReplay = func(ctx context.Context) error {
			close(replayStarted)
			<-ctx.Done()
			return ctx.Err()
		}
		if err := d.startPendingDepartureExecutions(context.Background(), fixture.project, fixture.wf); err != nil {
			scheduledPromotionBeforeCommittedReplay = oldHook
			t.Fatal(err)
		}
		select {
		case <-replayStarted:
		case <-time.After(10 * time.Second):
			scheduledPromotionBeforeCommittedReplay = oldHook
			_ = d.Close()
			t.Fatal("promoted replay did not enter its cancellable boundary")
		}
		closeResult := make(chan error, 1)
		go func() { closeResult <- d.Close() }()
		select {
		case err := <-closeResult:
			if err != nil {
				scheduledPromotionBeforeCommittedReplay = oldHook
				t.Fatal(err)
			}
		case <-time.After(5 * time.Second):
			scheduledPromotionBeforeCommittedReplay = oldHook
			t.Fatal("Close did not cancel the promoted replay")
		}
		scheduledPromotionBeforeCommittedReplay = oldHook

		reopened, err := OpenRuntimeStore(stateRoot)
		if err != nil {
			t.Fatal(err)
		}
		cancelled := mustDepartureRun(t, reopened, run.ID)
		if cancelled.State != DepartureStatePromoted ||
			cancelled.BlockReason != "" ||
			cancelled.Gate.Failure.Identity != "" {
			_ = reopened.Close()
			t.Fatalf("cancelled promoted replay was terminalized: %#v", cancelled)
		}
		replayDaemon := &Daemon{store: reopened}
		if err := replayDaemon.executeDeparture(context.Background(), fixture.project, fixture.wf, run.ID); err != nil {
			_ = reopened.Close()
			t.Fatalf("promoted row was not replayable after cancellation: %v", err)
		}
		replayed := mustDepartureRun(t, reopened, run.ID)
		if replayed.State != DepartureStatePassed {
			_ = reopened.Close()
			t.Fatalf("promoted cancellation did not remain pass-replayable: %#v", replayed)
		}
		if err := reopened.Close(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("async executor errors are bounded durable and resumable", func(t *testing.T) {
		fixture := newDepartureExecutionFixture(t, scheduledPromotionShadow, "async-error.txt")
		d, err := NewDaemon(fixture.stateRoot)
		if err != nil {
			t.Fatal(err)
		}
		defer d.Close()
		run := createDepartureExecutionRun(t, d.store, fixture)
		rawFailure := "secret=supersecret " + strings.Repeat("x", departureExecutionErrorLimit*3)
		d.departurePlan = func(RegisteredProject, Workflow) (DepartureDecision, error) {
			return DepartureDecision{}, errors.New(rawFailure)
		}
		logged := make(chan string, 1)
		oldLogger := departureExecutionLog
		departureExecutionLog = func(projectID, runID, state, message string) {
			logged <- strings.Join([]string{projectID, runID, state, message}, "|")
		}
		defer func() { departureExecutionLog = oldLogger }()
		if err := d.startPendingDepartureExecutions(context.Background(), fixture.project, fixture.wf); err != nil {
			t.Fatal(err)
		}
		select {
		case line := <-logged:
			if !strings.Contains(line, fixture.project.ProjectID+"|"+run.ID+"|evaluating|") ||
				strings.Contains(line, "supersecret") {
				t.Fatalf("async executor log was missing identity or leaked a secret: %q", line)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("async executor error was not logged")
		}
		failed := mustDepartureRun(t, d.store, run.ID)
		if failed.State != DepartureStateEvaluating ||
			failed.ExecutionLastError == "" ||
			len(failed.ExecutionLastError) > departureExecutionErrorLimit+3 ||
			strings.Contains(failed.ExecutionLastError, "supersecret") ||
			failed.ExecutionLastErrorAt == "" ||
			failed.ExecutionErrorCount != 1 {
			t.Fatalf("async executor error was not bounded and durable: %#v", failed)
		}
		recovery, _ := classifyDepartureRecoveryForProject(fixture.project, failed)
		if recovery.Disposition != DepartureRecoveryResumable || recovery.ResumeState != DepartureStateEvaluating {
			t.Fatalf("safe async failure was not resumable: %#v", recovery)
		}
		deadline := time.Now().Add(5 * time.Second)
		for len(d.activeDepartureExecutionIDs()) != 0 {
			if time.Now().After(deadline) {
				t.Fatal("failed departure worker did not release its active claim")
			}
			time.Sleep(time.Millisecond)
		}
		d.departurePlan = fixture.plan
		if err := d.startPendingDepartureExecutions(context.Background(), fixture.project, fixture.wf); err != nil {
			t.Fatal(err)
		}
		deadline = time.Now().Add(10 * time.Second)
		for {
			replayed := mustDepartureRun(t, d.store, run.ID)
			if replayed.State == DepartureStatePassed && replayed.ExecutionLastError == "" && replayed.ExecutionLastErrorAt == "" {
				if replayed.ExecutionErrorCount != 1 {
					t.Fatalf("successful retry lost bounded error count: %#v", replayed)
				}
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("safe async failure did not resume and clear: %#v", replayed)
			}
			time.Sleep(10 * time.Millisecond)
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

func TestDepartureExecutionFinalAuthorityAfterPreparation(t *testing.T) {
	for _, tc := range []struct {
		name, kind, wantReason string
		mutate                 func(map[string]any)
	}{
		{
			name: "disarm", kind: "wave", wantReason: "wave_authorization_not_armed:W-0001",
			mutate: func(data map[string]any) {
				data["authorization"] = "disarmed"
				delete(data, "authorization_fingerprint")
			},
		},
		{
			name: "task rework", kind: "task", wantReason: "wave_member_not_done:APP-T-0001",
			mutate: func(data map[string]any) {
				data["status"] = "rework"
				data["readiness"] = "ready"
			},
		},
		{
			name: "proof revocation", kind: "task", wantReason: "task_review_provenance_missing:APP-T-0001",
			mutate: func(data map[string]any) {
				data["proof_status"] = "pending"
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newDepartureExecutionFixture(t, scheduledPromotionPromote, "final-authority-"+strings.ReplaceAll(tc.name, " ", "-")+".txt")
			store, err := OpenRuntimeStore(fixture.stateRoot)
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			run := createDepartureExecutionRun(t, store, fixture)
			mainBefore := gitRevisionForTest(t, fixture.repo, "main")
			targetID := "APP-T-0001"
			targetDir := "tasks"
			if tc.kind == "wave" {
				targetID, targetDir = "W-0001", "waves"
			}
			targetPath := filepath.Join(fixture.vault, "work", targetDir, targetID+".md")
			var livePreimage, operatorBytes string
			var operatorMode os.FileMode
			hookCalls := 0
			originalBeforeHook := scheduledPromotionBeforeDefaultPrepare
			scheduledPromotionBeforeDefaultPrepare = func() error {
				livePreimage = mustReadIndexTest(t, targetPath)
				return nil
			}
			originalAfterHook := scheduledPromotionAfterDefaultPrepare
			scheduledPromotionAfterDefaultPrepare = func() error {
				hookCalls++
				data, body, parseErr := parseFrontmatter(livePreimage)
				if parseErr != nil {
					return parseErr
				}
				current, _, parseErr := parseFrontmatterMustRead(targetPath)
				if parseErr != nil {
					return parseErr
				}
				tc.mutate(data)
				data["updated_by"] = "human:operator"
				data["updated_at"] = "2026-07-25T23:59:00Z"
				// This deliberately models an unmanaged/raw edit after
				// preparation. Managed writers take the material epoch and are
				// linearized by the dedicated barrier race below.
				if _, saveErr := saveV7DocumentCASUnderMaterialLock(targetPath, data, body, v7FrontmatterOrder[tc.kind], stringField(current, "state_rev")); saveErr != nil {
					return saveErr
				}
				operatorBytes = mustReadIndexTest(t, targetPath)
				info, statErr := os.Stat(targetPath)
				if statErr != nil {
					return statErr
				}
				operatorMode = info.Mode().Perm()
				return nil
			}
			defer func() {
				scheduledPromotionBeforeDefaultPrepare = originalBeforeHook
				scheduledPromotionAfterDefaultPrepare = originalAfterHook
			}()

			d := &Daemon{store: store, departurePlan: fixture.plan}
			if err := d.executeDeparture(context.Background(), fixture.project, fixture.wf, run.ID); err != nil {
				t.Fatal(err)
			}
			blocked := mustDepartureRun(t, store, run.ID)
			if hookCalls != 1 ||
				blocked.State != DepartureStateBlocked ||
				blocked.Promotion.AttemptedAt == "" ||
				!strings.Contains(blocked.BlockReason, tc.wantReason) {
				t.Fatalf("final %s authority was not fenced: hooks=%d run=%#v", tc.name, hookCalls, blocked)
			}
			if after := gitRevisionForTest(t, fixture.repo, "main"); after != mainBefore {
				t.Fatalf("final %s authority moved main: before=%s after=%s", tc.name, mainBefore, after)
			}
			if after := mustReadIndexTest(t, targetPath); after != operatorBytes {
				t.Fatalf("final %s authority overwrote operator bytes", tc.name)
			}
			info, err := os.Stat(targetPath)
			if err != nil {
				t.Fatal(err)
			}
			if info.Mode().Perm() != operatorMode {
				t.Fatalf("final %s authority overwrote operator mode: got=%v want=%v", tc.name, info.Mode().Perm(), operatorMode)
			}
		})
	}
}

func newDepartureExecutionFixture(t *testing.T, mode, fileName string) departureExecutionFixture {
	t.Helper()
	stateRoot := t.TempDir()
	repo, vault := newLandReadyForMainAdvanceTestInStateRoot(t, fileName, "departure\n", stateRoot)
	sourceSHA := gitRevisionForTest(t, repo, "task/APP-T-0001")
	setDepartureTaskSourceForTest(t, vault, "APP-T-0001", sourceSHA)
	return configureDepartureExecutionFixture(t, repo, vault, stateRoot, mode, []string{"go version >/dev/null && test -f " + yamlQuoteForShellTest(fileName)})
}

func newUnstagedDepartureExecutionFixture(t *testing.T, mode, fileName string) departureExecutionFixture {
	t.Helper()
	stateRoot := t.TempDir()
	repo, vault := newLandTestRepo(t, 1, "test -f "+yamlQuoteForShellTest(fileName))
	sourceSHA := commitLandBranch(t, repo, "task/APP-T-0001", "integration/W-0001", map[string]string{fileName: "departure\n"})
	setWaveTaskState(t, vault, "APP-T-0001", "done", "done", "2026-07-25T00:00:00Z")
	setDepartureTaskSourceForTest(t, vault, "APP-T-0001", sourceSHA)
	return configureDepartureExecutionFixture(t, repo, vault, stateRoot, mode, []string{"go version >/dev/null && test -f " + yamlQuoteForShellTest(fileName)})
}

func newMultiMemberDepartureExecutionFixture(t *testing.T) departureExecutionFixture {
	t.Helper()
	stateRoot := t.TempDir()
	repo, vault := newLandTestRepo(t, 2, "test -f member-one.txt")
	sourceOne := commitLandBranch(t, repo, "task/APP-T-0001", "integration/W-0001", map[string]string{"member-one.txt": "one\n"})
	setDepartureTaskSourceForTest(t, vault, "APP-T-0001", sourceOne)
	if err := landFrozenSourcesAsIssuedDepartureInStateRoot(t, repo, vault,
		Args{"vault": vault, "quiet": "true", "actor": "daemon:departure:fixture-one", "_pos0": "APP-T-0001"},
		map[string]string{"APP-T-0001": sourceOne},
		stateRoot,
	); err != nil {
		t.Fatal(err)
	}
	assertDepartureLandingSource(t, vault, "W-0001", "APP-T-0001", sourceOne)
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
	return configureDepartureExecutionFixture(t, repo, vault, stateRoot, scheduledPromotionPromote, []string{
		"go version >/dev/null && test -f member-one.txt && test -f member-two.txt",
	})
}

func newMultiWaveDepartureExecutionFixture(t *testing.T) departureExecutionFixture {
	t.Helper()
	stateRoot := t.TempDir()
	repo := t.TempDir()
	runGitDir(t, repo, "init", "-b", "main")
	runGitDir(t, repo, "config", "user.email", "test@example.com")
	runGitDir(t, repo, "config", "user.name", "Test User")
	if err := writeText(managedTuskerConfigPath(filepath.Join(repo, defaultRepoVaultDir)), "schema: tusker.config/v1\nproject_id: app\nbranches:\n  default_branch: main\n  control:\n    - main\nruntime:\n  mutation_mode: single_user_local\nautomation:\n  validation:\n    commands:\n      - \"true\"\n"); err != nil {
		t.Fatal(err)
	}
	vault := filepath.Join(repo, ".tusker")
	mustWave(t, Args{"vault": vault, "quiet": "true"}, bootstrap)
	mustWave(t, Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App", "summary": "Departure source-fence tests.", "v7": "true"}, newV7Epic)
	for i := 1; i <= 4; i++ {
		mustWave(t, Args{
			"vault": vault, "quiet": "true", "epic": "APP",
			"title": "Task " + padNumber(i), "risk": "low", "priority": "p2", "v7": "true",
		}, newV7Task)
	}
	if err := writeText(filepath.Join(repo, "README.md"), "seed\n"); err != nil {
		t.Fatal(err)
	}
	runGitDir(t, repo, "add", ".")
	runGitDir(t, repo, "commit", "-m", "seed")
	mustWave(t, Args{"vault": vault, "quiet": "true", "_pos0": "First departure", "_pos1": "APP-T-0001", "_pos2": "APP-T-0002"}, waveV7CreateCmd)
	mustWave(t, Args{"vault": vault, "quiet": "true", "_pos0": "Second departure", "_pos1": "APP-T-0003", "_pos2": "APP-T-0004"}, waveV7CreateCmd)
	runGitDir(t, repo, "add", "-A")
	runGitDir(t, repo, "commit", "-m", "record departure waves")
	runGitDir(t, repo, "branch", "-f", "integration/W-0001", "main")
	runGitDir(t, repo, "branch", "-f", "integration/W-0002", "main")

	for i := 1; i <= 4; i++ {
		taskID := "APP-T-" + padNumber(i)
		waveID := "W-0001"
		if i > 2 {
			waveID = "W-0002"
		}
		sourceSHA := commitLandBranch(t, repo, "task/"+taskID, "integration/"+waveID, map[string]string{
			"frozen-" + strings.ToLower(taskID) + ".txt": "frozen\n",
		})
		setWaveTaskState(t, vault, taskID, "done", "done", "2026-07-25T00:00:00Z")
		setDepartureTaskSourceForTest(t, vault, taskID, sourceSHA)
	}
	setScheduledPromotionPolicyForTest(t, vault, scheduledPromotionStage)
	wf := setScheduledPromotionGateForTest(t, vault, []string{"go version >/dev/null"}, "full")
	armScheduledPromotionWaveForTest(t, vault, "W-0001")
	armScheduledPromotionWaveForTest(t, vault, "W-0002")
	commitScheduledPromotionWorkflowForWavesTest(t, repo, vault, []string{"W-0001", "W-0002"})
	return departureExecutionFixtureForRepo(t, repo, vault, stateRoot, wf)
}

func configureDepartureExecutionFixture(t *testing.T, repo, vault, stateRoot, mode string, gateCommands []string) departureExecutionFixture {
	t.Helper()
	setScheduledPromotionPolicyForTest(t, vault, mode)
	wf := setScheduledPromotionGateForTest(t, vault, gateCommands, "full")
	armScheduledPromotionWaveForTest(t, vault, "W-0001")
	commitScheduledPromotionWorkflowForTest(t, repo, vault)
	return departureExecutionFixtureForRepo(t, repo, vault, stateRoot, wf)
}

func departureExecutionFixtureForRepo(t *testing.T, repo, vault, stateRoot string, wf Workflow) departureExecutionFixture {
	t.Helper()
	remote := filepath.Join(t.TempDir(), "origin.git")
	runGitDir(t, filepath.Dir(remote), "init", "--bare", remote)
	runGitDir(t, repo, "remote", "add", "origin", remote)
	runGitDir(t, repo, "push", "-u", "origin", "main")
	project := RegisteredProject{ProjectID: "app", RepoRoot: repo, VaultRoot: vault}
	planner := defaultDeparturePlanner()
	receiptStore, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = receiptStore.Close() })
	planner.receiptStore = receiptStore
	planner.gateLookup = func(string, string, []string, string, string) bool { return false }
	return departureExecutionFixture{
		repo: repo, vault: vault, stateRoot: stateRoot, project: project, wf: wf,
		plan: func(project RegisteredProject, wf Workflow) (DepartureDecision, error) {
			return planner.PlanDeparture(project.VaultRoot, project.ProjectID, WorkflowFile{Data: wf})
		},
	}
}

func commitScheduledPromotionWorkflowForWavesTest(t *testing.T, repo, vault string, waveIDs []string) {
	t.Helper()
	workflow := filepath.ToSlash(filepath.Join(".tusker", "WORKFLOW.md"))
	runGitDir(t, repo, "add", "--", workflow)
	runGitDir(t, repo, "commit", "-m", "configure scheduled promotion")
	for _, waveID := range waveIDs {
		worktree := filepath.Join(t.TempDir(), "scheduled-promotion-"+strings.ToLower(waveID))
		branch := "integration/" + waveID
		runGitDir(t, repo, "worktree", "add", "--detach", worktree, branch)
		if err := writeText(filepath.Join(worktree, workflow), mustReadIndexTest(t, filepath.Join(vault, "WORKFLOW.md"))); err != nil {
			t.Fatal(err)
		}
		runGitDir(t, worktree, "add", "--", workflow)
		runGitDir(t, worktree, "commit", "-m", "configure scheduled promotion")
		next := strings.TrimSpace(gitDirOutput(t, worktree, "rev-parse", "HEAD"))
		runGitDir(t, repo, "worktree", "remove", "--force", worktree)
		runGitDir(t, repo, "update-ref", "refs/heads/"+branch, next)
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

func setDepartureTaskAcceptorForTest(t *testing.T, vault, taskID, actor string) {
	t.Helper()
	path := filepath.Join(vault, "work", "tasks", taskID+".md")
	data, body, err := parseFrontmatterMustRead(path)
	if err != nil {
		t.Fatal(err)
	}
	baseRev := stringField(data, "state_rev")
	data["accepted_by"] = actor
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

func departureWaveMemberBytes(t *testing.T, vault, waveID string, taskIDs ...string) map[string]string {
	t.Helper()
	snapshot := make(map[string]string, len(taskIDs)+1)
	for _, taskID := range taskIDs {
		path := filepath.Join(vault, "work", "tasks", taskID+".md")
		snapshot[path] = mustReadIndexTest(t, path)
	}
	wavePath := filepath.Join(vault, "work", "waves", waveID+".md")
	snapshot[wavePath] = mustReadIndexTest(t, wavePath)
	return snapshot
}

func departureWaveMemberModes(t *testing.T, files map[string]string) map[string]os.FileMode {
	t.Helper()
	modes := make(map[string]os.FileMode, len(files))
	for path := range files {
		info, err := os.Lstat(path)
		if err != nil {
			t.Fatal(err)
		}
		modes[path] = info.Mode()
	}
	return modes
}

func assertDepartureWaveMemberBytes(t *testing.T, vault string, expected map[string]string, expectedModes ...map[string]os.FileMode) {
	t.Helper()
	if len(expected) == 0 {
		t.Fatal("canonical wave-member snapshot is empty")
	}
	for path, want := range expected {
		got := mustReadIndexTest(t, path)
		wantMode, gotMode := os.FileMode(0), os.FileMode(0)
		if len(expectedModes) > 0 {
			wantMode = expectedModes[0][path]
		}
		if info, statErr := os.Lstat(path); statErr == nil {
			gotMode = info.Mode()
		}
		modeChanged := len(expectedModes) > 0 && gotMode != wantMode
		if got != want || modeChanged {
			rel, err := filepath.Rel(vault, path)
			if err != nil {
				rel = path
			}
			offset, line, wantByte, gotByte, wantLine, gotLine := departureFirstDifference(want, got)
			t.Fatalf("canonical preimage changed for %s: first difference byte=%d line=%d want_byte=%#x got_byte=%#x want_len=%d got_len=%d want_mode=%s got_mode=%s want_line=%q got_line=%q",
				rel, offset, line, wantByte, gotByte, len(want), len(got), wantMode, gotMode, wantLine, gotLine)
		}
	}
}

func departureFirstDifference(want, got string) (offset, line, wantByte, gotByte int, wantLine, gotLine string) {
	limit := len(want)
	if len(got) < limit {
		limit = len(got)
	}
	for offset < limit && want[offset] == got[offset] {
		offset++
	}
	line = strings.Count(want[:offset], "\n") + 1
	wantByte, gotByte = -1, -1
	if offset < len(want) {
		wantByte = int(want[offset])
	}
	if offset < len(got) {
		gotByte = int(got[offset])
	}
	lineAt := func(value string, at int) string {
		start := strings.LastIndex(value[:at], "\n") + 1
		end := strings.Index(value[at:], "\n")
		if end < 0 {
			end = len(value)
		} else {
			end += at
		}
		return value[start:end]
	}
	return offset, line, wantByte, gotByte, lineAt(want, min(offset, len(want))), lineAt(got, min(offset, len(got)))
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

func assertDepartureLandingSource(t *testing.T, vault, waveID, taskID, sourceSHA string) {
	t.Helper()
	data, _, err := parseFrontmatterMustRead(filepath.Join(vault, "work", "waves", waveID+".md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range normalizeLandingAudit(data["landings"]) {
		if stringField(row, "task") == taskID &&
			stringField(row, "gate_result") == "pass" &&
			stringField(row, "source_sha") == sourceSHA {
			if branch := stringField(row, "branch"); branch != "task/"+taskID {
				t.Fatalf("%s exact landing audit branch = %q", taskID, branch)
			}
			actor := stringField(row, "actor")
			fingerprint := stringField(row, "receipt_fingerprint")
			if stringField(row, "commit") == "" ||
				stringField(row, "tree") == "" ||
				stringField(row, "base_sha") == "" ||
				stringField(row, "merge_commit") == "" ||
				stringField(row, "source_provenance") == "" ||
				stringField(row, "gate_fingerprint") == "" ||
				fingerprint == "" ||
				stringField(row, "control_authority") == "" ||
				!trustedV7LandingControlAuthority(stringField(row, "control_authority"), actor) ||
				stringField(row, "provenance") != v7LandingAuditProvenance {
				t.Fatalf("%s exact landing audit is not authenticated: %#v", taskID, row)
			}
			receipt, ok := loadV7LandingReceipt(vault, fingerprint)
			if !ok ||
				receipt.Actor != actor ||
				receipt.ControlAuthority != stringField(row, "control_authority") ||
				receipt.Fingerprint != fingerprint {
				t.Fatalf("%s exact landing audit lost its signed receipt identity: row=%#v receipt=%#v loaded=%v", taskID, row, receipt, ok)
			}
			return
		}
	}
	t.Fatalf("%s/%s has no pass audit bound to source %s", waveID, taskID, sourceSHA)
}

func removeDepartureTaskLandingAudit(t *testing.T, vault, waveID, taskID, sourceSHA string) {
	t.Helper()
	path := filepath.Join(vault, "work", "waves", waveID+".md")
	data, body, err := parseFrontmatterMustRead(path)
	if err != nil {
		t.Fatal(err)
	}
	baseRev := stringField(data, "state_rev")
	var kept []map[string]any
	for _, row := range normalizeLandingAudit(data["landings"]) {
		if stringField(row, "task") == taskID && stringField(row, "source_sha") == sourceSHA {
			continue
		}
		kept = append(kept, row)
	}
	data["landings"] = kept
	if _, err := saveV7DocumentCAS(path, data, body, v7FrontmatterOrder["wave"], baseRev); err != nil {
		t.Fatal(err)
	}
}

func advanceDepartureTaskBranch(t *testing.T, repo, branch string, files map[string]string) string {
	t.Helper()
	worktree := filepath.Join(t.TempDir(), "advanced-task")
	runGitDir(t, repo, "worktree", "add", worktree, branch)
	for path, content := range files {
		full := filepath.Join(worktree, path)
		if err := ensureDir(filepath.Dir(full)); err != nil {
			t.Fatal(err)
		}
		if err := writeText(full, content); err != nil {
			t.Fatal(err)
		}
	}
	runGitDir(t, worktree, "add", ".")
	runGitDir(t, worktree, "commit", "-m", "advance "+branch+" after departure freeze")
	advanced := gitRevisionForTest(t, worktree, "HEAD")
	runGitDir(t, repo, "worktree", "remove", "--force", worktree)
	return advanced
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
