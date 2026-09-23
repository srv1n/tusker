package main

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

// selfServiceArmedABFixture authors a concurrency-1 armed A->B wave with
// canonical backlog/held member state and returns the wave authority.
func selfServiceArmedABFixture(t *testing.T) (vault string, store *RuntimeStore, project RegisteredProject, fingerprint, authorizedAt string) {
	t.Helper()
	vault, store, project = autonomousWaveFixture(t, []string{"APP-T-0001", "APP-T-0002"}, map[string]any{"concurrency": 1})
	writeDirectTask(t, vault, "APP-T-0001", "W-0001", nil)
	writeDirectTask(t, vault, "APP-T-0002", "W-0001", map[string]any{"dependencies": []any{"APP-T-0001:hard"}})
	if err := store.SetProjectEnabled(project.ProjectID, true); err != nil {
		t.Fatal(err)
	}
	result, err := directWaveStart(vault, store, "W-0001", "human:sarav")
	if err != nil {
		t.Fatal(err)
	}
	if result.Authorization != "authorized" || len(result.QueuedTaskIDs) != 1 || result.QueuedTaskIDs[0] != "APP-T-0001" {
		t.Fatalf("start did not queue exactly the armed root: %#v", result)
	}
	idx, err := loadV7Index(vault)
	if err != nil {
		t.Fatal(err)
	}
	wave := idx.Waves["W-0001"]
	return vault, store, project, stringField(wave.Data, "authorization_fingerprint"), stringField(wave.Data, "authorized_at")
}

func selfServiceClaimRoot(t *testing.T, store *RuntimeStore, project RegisteredProject, fingerprint, authorizedAt string) {
	t.Helper()
	root := RunStatus{ProjectID: project.ProjectID, RecordID: "APP-T-0001", ItemID: "APP-T-0001", Runner: string(RunnerCodexExec), Lane: runLaneExecute, LeaseState: string(LeaseStateUnclaimed)}
	if err := store.UpsertRun(root); err != nil {
		t.Fatal(err)
	}
	claimed, err := store.claimRunLeaseWithDirectiveAttempt(root, "attempt-root", 1, time.Minute, time.Now().UTC(),
		RuntimeLeaseClaimPrecondition{ExpectedLeaseState: LeaseStateUnclaimed},
		RunAuthorization{Source: "human_run_directive", Actor: "human:sarav", DirectiveWaveID: "W-0001", DirectiveAuthorizationFingerprint: fingerprint, DirectiveWaveAuthorizedAt: authorizedAt},
		RunAttempt{AttemptID: "attempt-root", Runner: string(RunnerCodexExec), Lane: runLaneExecute})
	if err != nil || !claimed {
		t.Fatalf("root directive claim did not consume: claimed=%t err=%v", claimed, err)
	}
}

func selfServiceReviewMember(t *testing.T, vault string, store *RuntimeStore, projectID, taskID string) directWaveReviewMember {
	t.Helper()
	review, err := buildDirectWaveReview(vault, store, projectID, "W-0001", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, member := range review.Members {
		if member.TaskID == taskID {
			return member
		}
	}
	t.Fatalf("review is missing member %s: %#v", taskID, review.Members)
	return directWaveReviewMember{}
}

func selfServicePlanExplanation(t *testing.T, vault string, store *RuntimeStore, taskID string) automationTaskExplanation {
	t.Helper()
	ctx, err := loadAutomationCommandContextWithStore(Args{"vault": vault}, DefaultStateRoot(), store)
	if err != nil {
		t.Fatal(err)
	}
	note, ok := ctx.NotesByID[taskID]
	if !ok {
		t.Fatalf("automation context is missing %s", taskID)
	}
	return ctx.explainTaskForRunnerMode(note, automationResolveRunner(note, ctx.Workflow.Data), nil, true, false)
}

func TestSelfServiceAdmission(t *testing.T) {
	t.Run("A1 queued root reservation reaches claim without self exclusion", func(t *testing.T) {
		vault, store, project, fingerprint, authorizedAt := selfServiceArmedABFixture(t)
		if fingerprint == "" || authorizedAt == "" {
			t.Fatal("armed wave has no durable authority")
		}

		directives := queuedDirectives(t, store, project.ProjectID)
		if len(directives) != 1 || directives[0].RecordID != "APP-T-0001" {
			t.Fatalf("unexpected directives: %#v", directives)
		}
		if got, err := queueAuthorizedWaveFrontier(vault, store, project.ProjectID, "W-0001", time.Now().UTC()); err != nil || len(got) != 0 {
			t.Fatalf("re-queue duplicated the root reservation: %v err=%v", got, err)
		}

		rootMember := selfServiceReviewMember(t, vault, store, project.ProjectID, "APP-T-0001")
		if rootMember.State == "blocked" || rootMember.State == "failed" {
			t.Fatalf("review failed the queued root: %#v", rootMember)
		}
		if rootMember.Responsible != "daemon" {
			t.Fatalf("review did not attribute the queued root to the daemon: %#v", rootMember)
		}
		dependentMember := selfServiceReviewMember(t, vault, store, project.ProjectID, "APP-T-0002")
		if dependentMember.State != "waiting" || !strings.Contains(dependentMember.WaitingReason, "APP-T-0001") {
			t.Fatalf("review did not hold the dependent on the root: %#v", dependentMember)
		}

		idx, err := loadV7Index(vault)
		if err != nil {
			t.Fatal(err)
		}
		// The member's own current-authorization reservation releases it from
		// canonical backlog authoring; a member without that backing stays put.
		reservation, reservationOK := selfServiceReservationPromotion(vault, store, project.ProjectID, idx.Tasks["APP-T-0001"], time.Now().UTC())
		if !reservationOK {
			t.Fatal("the queued root's own reservation did not release it from backlog authoring")
		}
		if status, readiness, owner := stringField(reservation.Data, "status"), stringField(reservation.Data, "readiness"), stringField(reservation.Data, "next_owner"); status != "ready" || readiness != "ready" || owner != "agent" {
			t.Fatalf("promotion left the queued root at %q/%q/%q", status, readiness, owner)
		}
		if _, dependentOK := selfServiceReservationPromotion(vault, store, project.ProjectID, idx.Tasks["APP-T-0002"], time.Now().UTC()); dependentOK {
			t.Fatal("the dependent without a reservation was promoted")
		}

		explanation := selfServicePlanExplanation(t, vault, store, "APP-T-0001")
		if !explanation.Dispatchable {
			t.Fatalf("automation plan refused the queued root: %#v", explanation.Blockers)
		}
		dependentExplanation := selfServicePlanExplanation(t, vault, store, "APP-T-0002")
		if dependentExplanation.Dispatchable {
			t.Fatal("automation plan admitted the dependent before root acceptance")
		}
		dependencyBlamed := false
		for _, blocker := range dependentExplanation.Blockers {
			dependencyBlamed = dependencyBlamed || strings.Contains(blocker, "APP-T-0001")
		}
		if !dependencyBlamed {
			t.Fatalf("plan did not blame the root for the dependent: %#v", dependentExplanation.Blockers)
		}

		rootFacts := AdmissionFacts{
			TaskID: "APP-T-0001", Status: stringField(reservation.Data, "status"),
			Readiness: stringField(reservation.Data, "readiness"), NextOwner: stringField(reservation.Data, "next_owner"),
			Lane: runLaneExecute, WaveID: "W-0001", ProjectID: project.ProjectID,
			ContractValid: true, RouteOK: true, ProofMapped: true,
			OwnerFree: true, DependenciesSatisfied: true,
			ProjectRegistered: true, ProjectEnabled: true,
			Authority: AdmissionAuthorityWaveArmed, AuthorityMatches: true, OwnReservation: true,
		}
		for _, stage := range []AdmissionStage{AdmissionStageAutomationPlan, AdmissionStageDaemonDispatch, AdmissionStageFinalClaim} {
			if verdict := EvaluateAdmissionForStage(rootFacts, stage); !verdict.Admit {
				t.Fatalf("shared admission refused the queued root at %s: %#v", stage, verdict.Blockers)
			}
		}
		dependentFacts := rootFacts
		dependentFacts.TaskID = "APP-T-0002"
		dependentFacts.Status = "backlog"
		dependentFacts.DependenciesSatisfied = false
		dependentFacts.BlockingDependency = "APP-T-0001"
		dependentFacts.OwnReservation = false
		dependentFacts.Authority = AdmissionAuthorityNone
		dependentFacts.AuthorityMatches = false
		dependentVerdict := EvaluateAdmissionForStage(dependentFacts, AdmissionStageDaemonDispatch)
		if dependentVerdict.Admit {
			t.Fatal("shared admission admitted the dependent before root acceptance")
		}
		dependencyBlamed = false
		for _, blocker := range dependentVerdict.Blockers {
			dependencyBlamed = dependencyBlamed || blocker.Code == AdmissionBlockerDependencyWaiting
		}
		if !dependencyBlamed {
			t.Fatalf("dependent refusal missed the dependency cause: %#v", dependentVerdict.Blockers)
		}

		daemon, err := NewDaemon(DefaultStateRoot())
		if err != nil {
			t.Fatal(err)
		}
		defer daemon.Close()
		daemon.dispatchRefusalReason = oneShotDispatchRefusal("tusker daemon run --once")
		if pollErr := daemon.PollOnce(context.Background()); pollErr != nil && !strings.Contains(pollErr.Error(), "cannot dispatch local runners") {
			t.Fatalf("one-shot poll returned an unexpected error: %v", pollErr)
		}
		if got := queuedDirectives(t, store, project.ProjectID); len(got) != 1 || got[0].RecordID != "APP-T-0001" {
			t.Fatalf("poll dropped or duplicated the root reservation: %#v", got)
		}

		selfServiceClaimRoot(t, store, project, fingerprint, authorizedAt)
		directive, err := store.RunDirective(project.ProjectID, "APP-T-0001")
		if err != nil || directive == nil || directive.State != "consumed" {
			t.Fatalf("root directive was not consumed by the claim: %#v err=%v", directive, err)
		}
		duplicate, err := store.claimRunLeaseWithDirectiveAttempt(
			RunStatus{ProjectID: project.ProjectID, RecordID: "APP-T-0001", ItemID: "APP-T-0001", Runner: string(RunnerCodexExec), Lane: runLaneExecute, LeaseState: string(LeaseStateUnclaimed)},
			"attempt-duplicate", 1, time.Minute, time.Now().UTC(),
			RuntimeLeaseClaimPrecondition{ExpectedLeaseState: LeaseStateUnclaimed},
			RunAuthorization{Source: "human_run_directive", Actor: "human:sarav", DirectiveWaveID: "W-0001", DirectiveAuthorizationFingerprint: fingerprint, DirectiveWaveAuthorizedAt: authorizedAt},
			RunAttempt{AttemptID: "attempt-duplicate", Runner: string(RunnerCodexExec), Lane: runLaneExecute})
		if err != nil {
			t.Fatalf("duplicate claim errored instead of refusing: %v", err)
		}
		if duplicate {
			t.Fatal("root claimed twice: one admission produced two attempts")
		}

		daemonAdvance := &Daemon{stateRoot: DefaultStateRoot(), store: store}
		if err := daemonAdvance.advanceAuthorizedWaveFrontiers(project); err != nil {
			t.Fatal(err)
		}
		if got := queuedDirectives(t, store, project.ProjectID); len(got) != 0 {
			t.Fatalf("wave oversubscribed while the root attempt was live: %#v", got)
		}
	})

	t.Run("A2 stages and lanes agree on code and actor", func(t *testing.T) {
		vault, store, project, _, _ := selfServiceArmedABFixture(t)

		planRoot := selfServicePlanExplanation(t, vault, store, "APP-T-0001")
		planDependent := selfServicePlanExplanation(t, vault, store, "APP-T-0002")
		if !planRoot.Dispatchable || planDependent.Dispatchable {
			t.Fatalf("plan disagrees with review: root=%t dependent=%t", planRoot.Dispatchable, planDependent.Dispatchable)
		}
		rootMember := selfServiceReviewMember(t, vault, store, project.ProjectID, "APP-T-0001")
		if rootMember.Responsible != "daemon" {
			t.Fatalf("review attributes the queued root to %q, plan dispatches through the daemon", rootMember.Responsible)
		}
		dependentMember := selfServiceReviewMember(t, vault, store, project.ProjectID, "APP-T-0002")
		if !strings.Contains(dependentMember.WaitingReason, "APP-T-0001") {
			t.Fatalf("review and plan blame different owners for the dependent: %q vs %#v", dependentMember.WaitingReason, planDependent.Blockers)
		}

		reviewFacts := AdmissionFacts{
			TaskID: "APP-T-0001", Status: "review", Lane: runLaneExecute,
			ContractValid: true, RouteOK: true, ProofMapped: true,
			OwnerFree: true, DependenciesSatisfied: true,
			ProjectRegistered: true, ProjectEnabled: true,
			Authority: AdmissionAuthorityWaveArmed, AuthorityMatches: true,
		}
		if verdict := EvaluateAdmissionForStage(reviewFacts, AdmissionStageTaskStart); verdict.Admit {
			t.Fatalf("execute start admitted review-lane work: %#v", verdict)
		} else if verdict.Blockers[0].Code != AdmissionBlockerTaskState {
			t.Fatalf("execute lane blamed the wrong cause for review work: %#v", verdict.Blockers)
		}
		if verdict := EvaluateAdmissionForStage(reviewFacts, AdmissionStageFinalClaim); verdict.Admit {
			t.Fatalf("final claim admitted review-lane work on the execute lane: %#v", verdict)
		}
		reviewFacts.Lane = runLaneReview
		startVerdict := EvaluateAdmissionForStage(reviewFacts, AdmissionStageTaskStart)
		if !startVerdict.Admit {
			t.Fatalf("review lane refused review work: %#v", startVerdict.Blockers)
		}

		// Documented interactive difference: disabling the project refuses
		// automation stages while a valid interactive claim stays valid.
		if err := store.SetProjectEnabled(project.ProjectID, false); err != nil {
			t.Fatal(err)
		}
		disabledFacts := AdmissionFacts{
			TaskID: "APP-T-0001", Status: "ready", Lane: runLaneExecute,
			ContractValid: true, RouteOK: true, ProofMapped: true,
			OwnerFree: true, DependenciesSatisfied: true,
			ProjectRegistered: true, ProjectEnabled: false,
			Authority: AdmissionAuthorityWaveArmed, AuthorityMatches: true, OwnReservation: true,
		}
		if verdict := EvaluateAdmissionForStage(disabledFacts, AdmissionStageAutomationPlan); verdict.Admit {
			t.Fatal("automation plan admitted work in a disabled project")
		} else if verdict.Blockers[0].Code != AdmissionBlockerProjectDisabled {
			t.Fatalf("plan blamed the wrong cause for a disabled project: %#v", verdict.Blockers)
		}
		if verdict := EvaluateAdmissionForStage(disabledFacts, AdmissionStageTaskStart); !verdict.Admit {
			t.Fatalf("disabled project refused direct interactive work: %#v", verdict.Blockers)
		}
	})

	t.Run("A3 duplicate start concurrent poll pause override and partial waves keep one attempt", func(t *testing.T) {
		vault, store, project, fingerprint, authorizedAt := selfServiceArmedABFixture(t)

		replay, err := directWaveStart(vault, store, "W-0001", "human:sarav")
		if err != nil {
			t.Fatalf("duplicate start errored instead of replaying: %v", err)
		}
		if !replay.Replayed {
			t.Fatalf("duplicate start was not marked as replay: %#v", replay)
		}
		if got := queuedDirectives(t, store, project.ProjectID); len(got) != 1 {
			t.Fatalf("duplicate start produced %d directives", len(got))
		}

		var wg sync.WaitGroup
		errs := make(chan error, 4)
		for i := 0; i < 4; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, callErr := queueAuthorizedWaveFrontier(vault, store, project.ProjectID, "W-0001", time.Now().UTC())
				errs <- callErr
			}()
		}
		wg.Wait()
		close(errs)
		for callErr := range errs {
			if callErr != nil {
				t.Fatal(callErr)
			}
		}
		if got := queuedDirectives(t, store, project.ProjectID); len(got) != 1 {
			t.Fatalf("concurrent polls produced %d directives", len(got))
		}

		pausedFacts := AdmissionFacts{
			TaskID: "APP-T-0001", Status: "ready", Lane: runLaneExecute, WaveID: "W-0001",
			ContractValid: true, RouteOK: true, ProofMapped: true,
			OwnerFree: true, DependenciesSatisfied: true,
			ProjectRegistered: true, ProjectEnabled: true,
			Authority: AdmissionAuthorityWaveArmed, AuthorityMatches: true, OwnReservation: true,
			WavePaused: true,
		}
		if verdict := EvaluateAdmissionForStage(pausedFacts, AdmissionStageDaemonDispatch); verdict.Admit {
			t.Fatal("daemon dispatch admitted new wave work while paused")
		} else if verdict.Blockers[0].Code != AdmissionBlockerWavePaused {
			t.Fatalf("paused wave blamed the wrong cause: %#v", verdict.Blockers)
		}
		pausedFacts.TaskStartOverride = true
		if verdict := EvaluateAdmissionForStage(pausedFacts, AdmissionStageDaemonDispatch); !verdict.Admit {
			t.Fatalf("explicit task-scoped start refused inside a paused wave: %#v", verdict.Blockers)
		}

		selfServiceClaimRoot(t, store, project, fingerprint, authorizedAt)
		markDirectTaskDone(t, vault, "APP-T-0001")
		releaseRoot := RunStatus{ProjectID: project.ProjectID, RecordID: "APP-T-0001", ItemID: "APP-T-0001", LeaseState: string(LeaseStateReleased), Terminal: true, AttemptOutcome: string(AttemptOutcomeSucceeded)}
		if err := store.UpsertRun(releaseRoot); err != nil {
			t.Fatal(err)
		}
		daemon := &Daemon{stateRoot: DefaultStateRoot(), store: store}
		if err := daemon.advanceAuthorizedWaveFrontiers(project); err != nil {
			t.Fatal(err)
		}
		directives := queuedDirectives(t, store, project.ProjectID)
		if len(directives) != 1 || directives[0].RecordID != "APP-T-0002" {
			t.Fatalf("dependent was not released exactly once after root acceptance: %#v", directives)
		}
		markDirectTaskDone(t, vault, "APP-T-0001")
		markDirectTaskDone(t, vault, "APP-T-0002")
		if got, err := queueAuthorizedWaveFrontier(vault, store, project.ProjectID, "W-0001", time.Now().UTC()); err != nil || len(got) != 0 {
			t.Fatalf("completed wave queued more work: %v err=%v", got, err)
		}
		// The dependent's still-valid queued row predates its completion and
		// expires by TTL; the frontier must not duplicate it or revive the root.
		if got := queuedDirectives(t, store, project.ProjectID); len(got) != 1 || got[0].RecordID != "APP-T-0002" {
			t.Fatalf("completed wave changed reservations: %#v", got)
		}
		completedReview, err := buildDirectWaveReview(vault, store, project.ProjectID, "W-0001", nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, member := range completedReview.Members {
			if member.State != "completed" {
				t.Fatalf("completed wave lost lifecycle state: %#v", member)
			}
		}
	})

	t.Run("A4 fences refuse without partial promotion", func(t *testing.T) {
		vault, store, project, fingerprint, authorizedAt := selfServiceArmedABFixture(t)

		dependent := RunStatus{ProjectID: project.ProjectID, RecordID: "APP-T-0002", ItemID: "APP-T-0002", Runner: string(RunnerCodexExec), Lane: runLaneExecute, LeaseState: string(LeaseStateUnclaimed)}
		if err := store.UpsertRun(dependent); err != nil {
			t.Fatal(err)
		}
		claimed, err := store.claimRunLeaseWithDirectiveAttempt(dependent, "attempt-dependent", 1, time.Minute, time.Now().UTC(),
			RuntimeLeaseClaimPrecondition{ExpectedLeaseState: LeaseStateUnclaimed},
			RunAuthorization{Source: "human_run_directive", Actor: "human:sarav", DirectiveWaveID: "W-0001", DirectiveAuthorizationFingerprint: fingerprint, DirectiveWaveAuthorizedAt: authorizedAt},
			RunAttempt{AttemptID: "attempt-dependent", Runner: string(RunnerCodexExec), Lane: runLaneExecute})
		if err == nil && claimed {
			t.Fatal("dependent claimed before root acceptance")
		}

		writeTaskFileOutOfBand(t, vault, "APP-T-0001", func(data map[string]any, body string) (map[string]any, string) {
			return data, body + "\nOut-of-band edit.\n"
		})
		editedIndex, err := loadV7Index(vault)
		if err != nil {
			t.Fatal(err)
		}
		edited := editedIndex.Tasks["APP-T-0001"]
		staleDirective, err := store.RunDirective(project.ProjectID, "APP-T-0001")
		if err != nil || staleDirective == nil {
			t.Fatalf("queued directive missing: %#v err=%v", staleDirective, err)
		}
		if runDirectiveMatchesTaskAuthority(vault, edited, staleDirective, time.Now().UTC()) {
			t.Fatal("drifted material still matches its queued authorization")
		}
		wfFile, err := loadWorkflow(vault)
		if err != nil {
			t.Fatal(err)
		}
		if got := armedWaveDispatchBlocker(vault, edited, wfFile.Data, nil); !strings.Contains(got, "contract") {
			t.Fatalf("changed material not refused at claim: %q", got)
		}
		if got, err := queueAuthorizedWaveFrontier(vault, store, project.ProjectID, "W-0001", time.Now().UTC()); err != nil || len(got) != 0 {
			t.Fatalf("drifted material queued more work: %v err=%v", got, err)
		}

		foreignFacts := AdmissionFacts{
			TaskID: "APP-T-0001", Status: "ready", Lane: runLaneExecute, WaveID: "W-0001",
			ContractValid: true, RouteOK: true, ProofMapped: true,
			OwnerFree: false, LiveOwner: "agent:other",
			DependenciesSatisfied: true,
			ProjectRegistered:     true, ProjectEnabled: true,
			Authority: AdmissionAuthorityWaveArmed, AuthorityMatches: true, OwnReservation: true,
		}
		if verdict := EvaluateAdmissionForStage(foreignFacts, AdmissionStageFinalClaim); verdict.Admit {
			t.Fatal("final claim admitted work held by a live owner")
		} else if verdict.Blockers[0].Code != AdmissionBlockerOwnerHeld {
			t.Fatalf("live ownership blamed the wrong cause: %#v", verdict.Blockers)
		}
	})
}
