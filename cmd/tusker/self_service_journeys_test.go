package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestSelfServiceJourneys proves TSK-T-0047: a focused lifecycle matrix over
// the existing direct-wave fixtures joins actual authoring/Start,
// poll/claim/fixture-runner attempt, verification/review/acceptance,
// diagnosis and safe repair, and shows downstream progression through the
// canonical acceptance path. A manual done mutation is exercised as the
// forbidden shortcut: it leaves proof debt, records no attempt or acceptor,
// wedges the frontier on its unconsumed directive, and is refused by accept.
//
// The suite owns independent integration assertions and fixture corrections
// only; broad production refactoring stays with the owning prerequisites.
// Discovered production gaps are reported against those prerequisites in the
// task report, not silently absorbed here.
func TestSelfServiceJourneys(t *testing.T) {
	t.Run("A1/canonical_path_dispatches_next_dependency", func(t *testing.T) {
		vault, store, project := journeysPendingABFixture(t)
		if repo := v7RepoRoot(vault); !v7GitRepo(repo) {
			runGit(t, "-C", repo, "init", "-q")
		}
		if err := store.SetProjectEnabled(project.ProjectID, true); err != nil {
			t.Fatal(err)
		}
		start, err := directWaveStart(vault, store, "W-0001", "human:journey")
		if err != nil {
			t.Fatal(err)
		}
		if start.Authorization != "authorized" {
			t.Fatalf("start did not authorize: %#v", start)
		}
		if len(start.QueuedTaskIDs) != 1 || start.QueuedTaskIDs[0] != "APP-T-0001" {
			t.Fatalf("start queued the wrong frontier: %#v", start.QueuedTaskIDs)
		}
		if again, err := queueAuthorizedWaveFrontier(vault, store, project.ProjectID, "W-0001", time.Now().UTC()); err != nil || len(again) != 0 {
			t.Fatalf("re-queue duplicated the root reservation: %v err=%v", again, err)
		}

		// One real fixture-runner attempt through the actual directive claim
		// path: the run row, lease, authorization and attempt are persisted,
		// and the queued directive is consumed.
		root := RunStatus{ProjectID: project.ProjectID, RecordID: "APP-T-0001", ItemID: "APP-T-0001", Runner: string(RunnerCodexExec), Lane: runLaneExecute, LeaseState: string(LeaseStateUnclaimed)}
		if err := store.UpsertRun(root); err != nil {
			t.Fatal(err)
		}
		idx, err := loadV7Index(vault)
		if err != nil {
			t.Fatal(err)
		}
		wave := idx.Waves["W-0001"]
		fingerprint, authorizedAt := stringField(wave.Data, "authorization_fingerprint"), stringField(wave.Data, "authorized_at")
		now := time.Now().UTC()
		claimed, err := store.claimRunLeaseWithDirectiveAttempt(root, "attempt-journey-root", 1, time.Minute, now,
			RuntimeLeaseClaimPrecondition{ExpectedLeaseState: LeaseStateUnclaimed},
			RunAuthorization{Source: "human_run_directive", Actor: "human:journey", DirectiveWaveID: "W-0001", DirectiveAuthorizationFingerprint: fingerprint, DirectiveWaveAuthorizedAt: authorizedAt},
			RunAttempt{AttemptID: "attempt-journey-root", Runner: string(RunnerCodexExec), Lane: runLaneExecute})
		if err != nil || !claimed {
			t.Fatalf("root directive claim did not consume: claimed=%t err=%v", claimed, err)
		}
		directive, err := store.RunDirective(project.ProjectID, "APP-T-0001")
		if err != nil || directive == nil || directive.State != "consumed" {
			t.Fatalf("root directive was not consumed by the claim: %#v err=%v", directive, err)
		}

		// The fixture runner succeeds: terminal outcome, then proof recorded
		// against the current material, then reviewer acceptance closes the
		// task through the canonical accept ceremony.
		finished := root
		finished.LeaseState = string(LeaseStateReleased)
		finished.Terminal = true
		finished.AttemptOutcome = string(AttemptOutcomeSucceeded)
		if err := store.UpsertRun(finished); err != nil {
			t.Fatal(err)
		}
		if err := v7TestVerificationMutation(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "by": "reviewer:journey",
			"covers": "A1", "check": "command: go test ./... -run '^TestProofExists$' -count=1", "result": "pass", "note": "Journey proof receipt."}); err != nil {
			t.Fatalf("record journey proof: %v", err)
		}
		if err := statusV7Cmd(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "status": "ready", "by": "human:journey"}); err != nil {
			t.Fatalf("admit root to ready: %v", err)
		}
		if err := acceptV7Cmd(Args{"vault": vault, "quiet": "true", "_pos0": "APP-T-0001", "by": "reviewer:independent"}); err != nil {
			t.Fatalf("accept root: %v", err)
		}

		// Acceptance dispatches the next dependency automatically under the
		// exact still-current wave authority.
		daemon := &Daemon{store: store, stateRoot: store.stateRoot}
		if err := daemon.advanceAuthorizedWaveFrontiers(project); err != nil {
			t.Fatal(err)
		}
		next := queuedDirectives(t, store, project.ProjectID)
		if len(next) != 1 || next[0].RecordID != "APP-T-0002" {
			t.Fatalf("acceptance did not dispatch the dependent: %#v", next)
		}
		if next[0].WaveID != "W-0001" || next[0].AuthorizationFingerprint != fingerprint || next[0].WaveAuthorizedAt != authorizedAt {
			t.Fatalf("dependent directive is not bound to exact wave authority: %#v", next[0])
		}

		// Exactly one scoped attempt exists for the root: the canonical path
		// never retries the external effect.
		attempts, err := store.ListAttemptsForRun(project.ProjectID, "APP-T-0001")
		if err != nil {
			t.Fatal(err)
		}
		if len(attempts) != 1 || attempts[0].AttemptID != "attempt-journey-root" {
			t.Fatalf("canonical path left %d attempts, want exactly the fixture-runner attempt: %#v", len(attempts), attempts)
		}

		// The accepted root carries proof, acceptor and completion with no
		// residual proof debt on the wave.
		after, err := loadV7Index(vault)
		if err != nil {
			t.Fatal(err)
		}
		rootTask := after.Tasks["APP-T-0001"]
		if statusField := stringField(rootTask.Data, "status"); statusField != "done" {
			t.Fatalf("accepted root status=%q, want done", statusField)
		}
		if got := stringField(rootTask.Data, "proof_status"); got != "satisfied" {
			t.Fatalf("accepted root proof_status=%q, want satisfied", got)
		}
		if got := stringField(rootTask.Data, "accepted_by"); got != "reviewer:independent" {
			t.Fatalf("accepted root lost its reviewer: accepted_by=%q", got)
		}
		review, err := buildDirectWaveReview(vault, store, project.ProjectID, "W-0001", nil)
		if err != nil {
			t.Fatal(err)
		}
		if member := journeysReviewMember(t, review, "APP-T-0001"); member.State != "completed" {
			t.Fatalf("accepted root did not project completed: %#v", member)
		}
		for _, blocker := range review.Blockers {
			if strings.HasPrefix(blocker.Code, "STRICT_PROOF_") {
				t.Fatalf("accepted wave still carries proof debt: %#v", blocker)
			}
		}
	})

	t.Run("A1/manual_done_is_not_a_substitute", func(t *testing.T) {
		vault, store, project := journeysPendingABFixture(t)
		if err := store.SetProjectEnabled(project.ProjectID, true); err != nil {
			t.Fatal(err)
		}
		if _, err := directWaveStart(vault, store, "W-0001", "human:journey"); err != nil {
			t.Fatal(err)
		}
		// The forbidden shortcut: mark done without an attempt, proof, or
		// review acceptance.
		markDirectTaskDone(t, vault, "APP-T-0001")

		review, err := buildDirectWaveReview(vault, store, project.ProjectID, "W-0001", nil)
		if err != nil {
			t.Fatal(err)
		}
		member := journeysReviewMember(t, review, "APP-T-0001")
		if member.State != "waiting" || member.Phase != "proof_blocked" {
			t.Fatalf("manual done did not strand the root on proof: %#v", member)
		}
		strictProof := false
		for _, blocker := range review.Blockers {
			if blocker.Code == "STRICT_PROOF_MISSING" && blocker.TaskID == "APP-T-0001" {
				strictProof = true
			}
		}
		if !strictProof {
			t.Fatalf("manual done produced no STRICT_PROOF_MISSING debt: %#v", review.Blockers)
		}
		if err := acceptV7Cmd(Args{"vault": vault, "quiet": "true", "_pos0": "APP-T-0001", "by": "reviewer:independent"}); err == nil {
			t.Fatal("accept closed a manually marked task with no proof")
		}
		after, err := loadV7Index(vault)
		if err != nil {
			t.Fatal(err)
		}
		if got := stringField(after.Tasks["APP-T-0001"].Data, "accepted_by"); got != "" {
			t.Fatalf("shortcut recorded an acceptor: %q", got)
		}
		if attempts, err := store.ListAttemptsForRun(project.ProjectID, "APP-T-0001"); err != nil || len(attempts) != 0 {
			t.Fatalf("shortcut ran %d attempts, want zero: %#v err=%v", len(attempts), attempts, err)
		}
		// The unconsumed directive still occupies the single wave slot, so
		// the shortcut wedges the frontier instead of advancing it.
		daemon := &Daemon{store: store, stateRoot: store.stateRoot}
		if err := daemon.advanceAuthorizedWaveFrontiers(project); err != nil {
			t.Fatal(err)
		}
		if next := queuedDirectives(t, store, project.ProjectID); len(next) != 1 || next[0].RecordID != "APP-T-0001" {
			t.Fatalf("shortcut released or duplicated the frontier: %#v", next)
		}
	})
}

// journeysPendingABFixture authors a concurrency-1 armed A->B wave whose root
// carries pending (unproven) verification, so the canonical proof/acceptance
// transitions happen inside the test instead of at authoring time.
func journeysPendingABFixture(t *testing.T) (string, *RuntimeStore, RegisteredProject) {
	t.Helper()
	vault, store, project := autonomousWaveFixture(t, []string{"APP-T-0001", "APP-T-0002"}, map[string]any{"concurrency": 1})
	writePendingDirectTask(t, vault, "APP-T-0001", "W-0001", nil)
	writeDirectTask(t, vault, "APP-T-0002", "W-0001", map[string]any{"dependencies": []any{"APP-T-0001:hard"}})
	return vault, store, project
}

func journeysReviewMember(t *testing.T, review directWaveReview, taskID string) directWaveReviewMember {
	t.Helper()
	for _, member := range review.Members {
		if member.TaskID == taskID {
			return member
		}
	}
	t.Fatalf("review is missing member %s: %#v", taskID, review.Members)
	return directWaveReviewMember{}
}

// journeysCLIWaveReview runs the receiving `wave review` command through the
// actual CLI parser and dispatch against the fixture state root, proving the
// CLI renders the shared projection instead of its own diagnosis.
func TestSelfServiceJourneysA2(t *testing.T) {
	// Each fault/wait branch pins one tuple across the receiving CLI, the
	// shared wave-review projection, and actual admission: the CLI renders
	// the projection's own reason (never a second diagnosis), admission
	// carries the same code family with a stable actor, repair permission
	// agrees on whether a supported repair exists, and the postcondition
	// holds on real store state. The (code, actor, repair, postcondition)
	// pair is pinned per branch so drift in any surface fails the test.
	t.Run("A2/chain_wait_names_dependency_everywhere", func(t *testing.T) {
		vault, store, project := journeysArmedAB(t)
		review, err := buildDirectWaveReview(vault, store, project.ProjectID, "W-0001", nil)
		if err != nil {
			t.Fatal(err)
		}
		member := journeysReviewMember(t, review, "APP-T-0002")
		if member.State != "waiting" || member.WaitingReason != "waiting for dependency APP-T-0001" {
			t.Fatalf("dependent lost its wait: %#v", member)
		}
		if member.Responsible != "daemon" {
			t.Fatalf("dependency wait named the wrong resolver: %#v", member)
		}
		if member.Recovery != nil {
			t.Fatalf("a normal dependency wait must offer no repair: %#v", member.Recovery)
		}
		output, _ := journeysCLIWaveReview(t, vault, "W-0001")
		if !strings.Contains(output, "APP-T-0002 waiting (waiting for dependency APP-T-0001)") {
			t.Fatalf("CLI did not render the shared wait reason:\n%s", output)
		}
		if _, err := journeysCLITaskStart(t, vault, "APP-T-0002"); err == nil || !strings.Contains(err.Error(), "DEPENDENCY_WAITING") || !strings.Contains(err.Error(), "APP-T-0001") {
			t.Fatalf("CLI task start did not refuse with the dependency code: %v", err)
		}
		verdict := EvaluateAdmissionForStage(AdmissionFacts{
			TaskID: "APP-T-0002", Status: "backlog", Readiness: "held", NextOwner: "agent", Lane: runLaneExecute,
			ContractValid: true, RouteOK: true, ProofMapped: true, OwnerFree: true,
			DependenciesSatisfied: false, BlockingDependency: "APP-T-0001",
			ProjectRegistered: true, ProjectEnabled: true,
		}, AdmissionStageDaemonDispatch)
		if verdict.Admit || !hasAdmissionBlocker(verdict, AdmissionBlockerDependencyWaiting) {
			t.Fatalf("admission admitted the dependent: %#v", verdict)
		}
		for _, blocker := range verdict.Blockers {
			if blocker.Code == AdmissionBlockerDependencyWaiting && len(blocker.Repair) != 0 {
				t.Fatalf("dependency wait offers no supported repair: %#v", blocker)
			}
		}
		if again, err := queueAuthorizedWaveFrontier(vault, store, project.ProjectID, "W-0001", time.Now().UTC()); err != nil || len(again) != 0 {
			t.Fatalf("frontier released the dependent early: %v err=%v", again, err)
		}
		if directive, err := store.RunDirective(project.ProjectID, "APP-T-0002"); err != nil || directive != nil {
			t.Fatalf("dependent holds a directive before acceptance: %#v err=%v", directive, err)
		}
	})

	t.Run("A2/duplicate_start_replays_without_duplicates", func(t *testing.T) {
		vault, store, project := journeysArmedAB(t)
		before := queuedDirectives(t, store, project.ProjectID)
		second, err := directWaveStart(vault, store, "W-0001", "human:journey")
		if err != nil {
			t.Fatal(err)
		}
		if second.Authorization != "authorized" || !second.Replayed {
			t.Fatalf("duplicate start did not replay the live authorization: %#v", second)
		}
		after := queuedDirectives(t, store, project.ProjectID)
		if len(before) != 1 || len(after) != 1 || after[0].RecordID != "APP-T-0001" {
			t.Fatalf("duplicate start duplicated the reservation: before=%#v after=%#v", before, after)
		}
		if after[0].AuthorizationFingerprint != before[0].AuthorizationFingerprint {
			t.Fatalf("duplicate start rebound live authority: %#v", after[0])
		}
		output, _ := journeysCLIWaveReview(t, vault, "W-0001")
		if !strings.Contains(output, "authorization authorized") || !strings.Contains(output, "APP-T-0001") {
			t.Fatalf("CLI lost the replayed authorization:\n%s", output)
		}
	})

	t.Run("A2/stale_authority_refuses_reuse_everywhere", func(t *testing.T) {
		vault, store, project := journeysArmedAB(t)
		stale := queuedDirectives(t, store, project.ProjectID)[0]
		journeysDriftTaskTitle(t, vault, "APP-T-0001", "Direct APP-T-0001 (drifted)")
		review, err := buildDirectWaveReview(vault, store, project.ProjectID, "W-0001", nil)
		if err != nil {
			t.Fatal(err)
		}
		if review.Authorization != "stale" {
			t.Fatalf("edited wave did not project stale: %s", review.Authorization)
		}
		idx, err := loadV7Index(vault)
		if err != nil {
			t.Fatal(err)
		}
		if runDirectiveMatchesTaskAuthority(vault, idx.Tasks["APP-T-0001"], &stale, time.Now().UTC()) {
			t.Fatal("the pre-edit directive still matches after the wave drifted")
		}
		if _, ok := selfServiceReservationPromotion(vault, store, project.ProjectID, idx.Tasks["APP-T-0001"], time.Now().UTC()); ok {
			t.Fatal("a stale reservation promoted its holder")
		}
		output, _ := journeysCLIWaveReview(t, vault, "W-0001")
		if !strings.Contains(output, "authorization stale") {
			t.Fatalf("CLI hid the stale authorization:\n%s", output)
		}
		verdict := EvaluateAdmissionForStage(AdmissionFacts{
			TaskID: "APP-T-0001", Status: "ready", Readiness: "ready", NextOwner: "agent", Lane: runLaneExecute,
			ContractValid: true, RouteOK: true, ProofMapped: true, OwnerFree: true,
			DependenciesSatisfied: true, ProjectRegistered: true, ProjectEnabled: true,
			Authority: AdmissionAuthorityWaveArmed, AuthorityMatches: false, OwnReservation: true,
		}, AdmissionStageFinalClaim)
		if verdict.Admit || !hasAdmissionBlocker(verdict, AdmissionBlockerAuthorityStale) {
			t.Fatalf("admission honored stale authority: %#v", verdict)
		}
		for _, blocker := range verdict.Blockers {
			if blocker.Code == AdmissionBlockerAuthorityStale {
				if blocker.Actor != "operator" || len(blocker.Repair) != 0 {
					t.Fatalf("stale authority misattributed repair: %#v", blocker)
				}
			}
		}
		if queued, err := queueAuthorizedWaveFrontier(vault, store, project.ProjectID, "W-0001", time.Now().UTC()); err != nil || len(queued) != 0 {
			t.Fatalf("stale wave queued frontier work: %v err=%v", queued, err)
		}
		// Recovery is three sanctioned CLI steps: targeted reconcile
		// repairs the record revision, `task update` rebinds the drifted
		// pin, then Start replaces the stale wave authorization under the
		// material lock instead of reusing it.
		reconcileCommand, reconcileArgs := parseCLI([]string{"tusker", "reconcile", "APP-T-0001", "--vault", vault})
		if reconcileCommand != "reconcile" {
			t.Fatalf("parseCLI routed reconcile to %q", reconcileCommand)
		}
		captureStdout(t, func() {
			if code, err := runInner(reconcileCommand, reconcileArgs); err != nil || code != 0 {
				t.Fatalf("reconcile did not repair the drifted revision: code=%d err=%v", code, err)
			}
		})
		reboundIdx, err := loadV7Index(vault)
		if err != nil {
			t.Fatal(err)
		}
		updateCommand, updateArgs := parseCLI([]string{"tusker", "task", "update", "APP-T-0001",
			"--if-revision", stringField(reboundIdx.Tasks["APP-T-0001"].Data, "state_rev"),
			"--rebind-contract", "--by", "human:journey", "--vault", vault})
		if updateCommand != "task update" {
			t.Fatalf("parseCLI routed task update to %q", updateCommand)
		}
		captureStdout(t, func() {
			if code, err := runInner(updateCommand, updateArgs); err != nil || code != 0 {
				t.Fatalf("task update did not rebind the drifted pin: code=%d err=%v", code, err)
			}
		})
		renewed, err := directWaveStart(vault, store, "W-0001", "human:journey")
		if err != nil {
			t.Fatalf("start did not recover rebound material: %v", err)
		}
		if renewed.Authorization != "authorized" || renewed.MaterialFingerprint == stale.AuthorizationFingerprint {
			t.Fatalf("renewal kept the stale fingerprint: %#v", renewed)
		}
		if len(renewed.QueuedTaskIDs) != 1 || renewed.QueuedTaskIDs[0] != "APP-T-0001" {
			t.Fatalf("renewal did not re-queue the root: %#v", renewed.QueuedTaskIDs)
		}
		current := queuedDirectives(t, store, project.ProjectID)
		if len(current) != 1 || current[0].AuthorizationFingerprint != renewed.MaterialFingerprint {
			t.Fatalf("renewal left a stale or duplicated reservation: %#v", current)
		}
	})

	t.Run("A2/expired_directive_renews_instead_of_reuse", func(t *testing.T) {
		vault, store, project := journeysArmedAB(t)
		now := time.Now().UTC()
		if _, err := store.exec(`UPDATE run_directives SET expires_at=? WHERE project_id=? AND record_id=?`, now.Add(-time.Hour).Format(time.RFC3339Nano), project.ProjectID, "APP-T-0001"); err != nil {
			t.Fatal(err)
		}
		expired, err := store.RunDirective(project.ProjectID, "APP-T-0001")
		if err != nil || expired == nil {
			t.Fatalf("expired directive row vanished: %#v err=%v", expired, err)
		}
		idx, err := loadV7Index(vault)
		if err != nil {
			t.Fatal(err)
		}
		if runDirectiveMatchesTaskAuthority(vault, idx.Tasks["APP-T-0001"], expired, now) {
			t.Fatal("an expired directive still matches authority")
		}
		if _, ok := selfServiceReservationPromotion(vault, store, project.ProjectID, idx.Tasks["APP-T-0001"], now); ok {
			t.Fatal("an expired reservation promoted its holder")
		}
		verdict := EvaluateAdmissionForStage(AdmissionFacts{
			TaskID: "APP-T-0001", Status: "ready", Readiness: "ready", NextOwner: "agent", Lane: runLaneExecute,
			ContractValid: true, RouteOK: true, ProofMapped: true, OwnerFree: true,
			DependenciesSatisfied: true, ProjectRegistered: true, ProjectEnabled: true,
			Authority: AdmissionAuthorityWaveArmed, AuthorityMatches: false, OwnReservation: true,
		}, AdmissionStageFinalClaim)
		if verdict.Admit || !hasAdmissionBlocker(verdict, AdmissionBlockerAuthorityStale) {
			t.Fatalf("admission honored an expired directive: %#v", verdict)
		}
		// Renewal is explicit: a task-scoped start issues a fresh directive
		// bound to current material instead of reviving the expired row.
		if code, err := journeysCLITaskStart(t, vault, "APP-T-0001"); err != nil || code != 0 {
			t.Fatalf("task start did not renew the lapsed directive: code=%d err=%v", code, err)
		}
		renewedDirective, err := store.RunDirective(project.ProjectID, "APP-T-0001")
		if err != nil || renewedDirective == nil || !runDirectiveActive(renewedDirective, time.Now().UTC()) {
			t.Fatalf("renewal left no active directive: %#v err=%v", renewedDirective, err)
		}
		if renewedDirective.WaveID != "" {
			t.Fatalf("renewal did not issue a task-scoped directive: %#v", renewedDirective)
		}
		idxAfter, err := loadV7Index(vault)
		if err != nil {
			t.Fatal(err)
		}
		if !runDirectiveMatchesTaskAuthority(vault, idxAfter.Tasks["APP-T-0001"], renewedDirective, time.Now().UTC()) {
			t.Fatal("the renewed directive does not match current authority")
		}
	})

	t.Run("A2/paused_wave_keeps_task_override", func(t *testing.T) {
		vault, store, project := journeysArmedAB(t)
		if _, err := directWavePause(vault, store, "W-0001", "human:journey"); err != nil {
			t.Fatal(err)
		}
		review, err := buildDirectWaveReview(vault, store, project.ProjectID, "W-0001", nil)
		if err != nil {
			t.Fatal(err)
		}
		member := journeysReviewMember(t, review, "APP-T-0001")
		if member.State != "ready" || member.Phase != "paused" || member.Responsible != "operator" {
			t.Fatalf("paused member lost its override cue: %#v", member)
		}
		output, _ := journeysCLIWaveReview(t, vault, "W-0001")
		if !strings.Contains(output, "authorization paused") || !strings.Contains(output, "APP-T-0001 ready") {
			t.Fatalf("CLI hid the paused-but-overridable member:\n%s", output)
		}
		paused := AdmissionFacts{
			TaskID: "APP-T-0001", Status: "ready", Readiness: "ready", NextOwner: "agent", Lane: runLaneExecute, WaveID: "W-0001",
			ContractValid: true, RouteOK: true, ProofMapped: true, OwnerFree: true,
			DependenciesSatisfied: true, ProjectRegistered: true, ProjectEnabled: true,
			Authority: AdmissionAuthorityWaveArmed, AuthorityMatches: true, OwnReservation: true, WavePaused: true,
		}
		verdict := EvaluateAdmissionForStage(paused, AdmissionStageDaemonDispatch)
		if verdict.Admit || !hasAdmissionBlocker(verdict, AdmissionBlockerWavePaused) {
			t.Fatalf("daemon dispatch ignored the pause: %#v", verdict)
		}
		for _, blocker := range verdict.Blockers {
			if blocker.Code == AdmissionBlockerWavePaused {
				if blocker.Actor != "operator" {
					t.Fatalf("pause named the wrong actor: %#v", blocker)
				}
				if len(blocker.Repair) == 0 || blocker.Repair[0] != "tusker" {
					t.Fatalf("pause hid its resume repair: %#v", blocker)
				}
			}
		}
		overridden := paused
		overridden.TaskStartOverride = true
		if verdict := EvaluateAdmissionForStage(overridden, AdmissionStageDaemonDispatch); !verdict.Admit {
			t.Fatalf("task-scoped override did not survive the pause: %#v", verdict)
		}
		if queued, err := queueAuthorizedWaveFrontier(vault, store, project.ProjectID, "W-0001", time.Now().UTC()); err != nil || len(queued) != 0 {
			t.Fatalf("paused wave admitted new frontier work: %v err=%v", queued, err)
		}
		// The override is real: a task-scoped start replaces the paused wave
		// directive with a task directive and leaves the wave paused.
		if code, err := journeysCLITaskStart(t, vault, "APP-T-0001"); err != nil || code != 0 {
			t.Fatalf("task-scoped start failed inside the pause: code=%d err=%v", code, err)
		}
		replaced, err := store.RunDirective(project.ProjectID, "APP-T-0001")
		if err != nil || replaced == nil || replaced.WaveID != "" {
			t.Fatalf("override did not replace the wave directive with a task directive: %#v err=%v", replaced, err)
		}
		pausedReview, err := buildDirectWaveReview(vault, store, project.ProjectID, "W-0001", nil)
		if err != nil {
			t.Fatal(err)
		}
		if pausedReview.Authorization != "paused" {
			t.Fatalf("override resumed the wave: %s", pausedReview.Authorization)
		}
	})

	t.Run("A2/contract_drift_blocks_every_surface", func(t *testing.T) {
		vault, store, project := journeysArmedAB(t)
		// Ledger-section bytes are not contract canon, so this out-of-band
		// write stales only state_rev: the task must wait while the wave
		// authorization stays current.
		journeysRawAppend(t, vault, "tasks", "APP-T-0001", "\n## Evidence\n\nJourney ledger note.\n")
		review, err := buildDirectWaveReview(vault, store, project.ProjectID, "W-0001", nil)
		if err != nil {
			t.Fatal(err)
		}
		if review.Authorization != "authorized" {
			t.Fatalf("ledger-only drift must not stale the wave: %s", review.Authorization)
		}
		member := journeysReviewMember(t, review, "APP-T-0001")
		// The projection reports every record-level staleness (pin or
		// revision) with one message; the precise cause is available from
		// the stale-reason predicate the admission paths evaluate.
		if member.State != "waiting" || !strings.Contains(member.WaitingReason, "task contract drifted from its stored fingerprint; rebind required") {
			t.Fatalf("drifted record did not wait on its revision: %#v", member)
		}
		contractBlocker := false
		for _, blocker := range review.Blockers {
			contractBlocker = contractBlocker || (blocker.Code == "CONTRACT_FINGERPRINT_STALE" && blocker.TaskID == "APP-T-0001")
		}
		if !contractBlocker {
			t.Fatalf("drifted contract raised no stale-fingerprint blocker: %#v", review.Blockers)
		}
		if member.Recovery != nil {
			t.Fatalf("drift offers no supported repair action: %#v", member.Recovery)
		}
		output, _ := journeysCLIWaveReview(t, vault, "W-0001")
		if !strings.Contains(output, "CONTRACT_FINGERPRINT_STALE") {
			t.Fatalf("CLI hid the contract blocker:\n%s", output)
		}
		if _, err := journeysCLITaskStart(t, vault, "APP-T-0001"); err == nil || !strings.Contains(err.Error(), "CONTRACT_FINGERPRINT_STALE") {
			t.Fatalf("CLI task start admitted a drifted contract: %v", err)
		}
		verdict := EvaluateAdmissionForStage(AdmissionFacts{
			TaskID: "APP-T-0001", Status: "ready", Readiness: "ready", NextOwner: "agent", Lane: runLaneExecute,
			ContractValid: true, ContractStale: true, RouteOK: true, ProofMapped: true, OwnerFree: true,
			DependenciesSatisfied: true, ProjectRegistered: true, ProjectEnabled: true,
			Authority: AdmissionAuthorityWaveArmed, AuthorityMatches: true, OwnReservation: true,
		}, AdmissionStageFinalClaim)
		if verdict.Admit || !hasAdmissionBlocker(verdict, AdmissionBlockerContractStale) {
			t.Fatalf("admission honored a drifted contract: %#v", verdict)
		}
		for _, blocker := range verdict.Blockers {
			if blocker.Code == AdmissionBlockerContractStale && len(blocker.Repair) != 0 {
				t.Fatalf("contract drift must name rebind out of band, not a repair argv: %#v", blocker)
			}
		}
	})

	t.Run("A2/summary_rejects_partial_green", func(t *testing.T) {
		if !journeysAllGreen(map[string]string{"chain_wait": "PASS", "duplicate_start": "PASS", "stale_authority": "PASS"}) {
			t.Fatal("an all-pass matrix must summarize green")
		}
		if journeysAllGreen(map[string]string{"chain_wait": "PASS", "duplicate_start": "FAIL", "stale_authority": "PASS"}) {
			t.Fatal("a matrix with a failed scenario summarized green")
		}
		if journeysAllGreen(map[string]string{"chain_wait": "PASS", "duplicate_start": "BLOCKED", "stale_authority": "PASS"}) {
			t.Fatal("a matrix with a blocked scenario summarized green")
		}
		if journeysAllGreen(nil) {
			t.Fatal("an empty matrix summarized green")
		}
	})
}

func TestSelfServiceJourneysA3(t *testing.T) {
	// Every injected crash or race retains exactly one scoped attempt or an
	// explicit uncertain outcome: reservations survive restart without
	// duplication, owner fences refuse competing claims, cancellation is
	// bounded and preserves attempt identity, terminal work is never
	// repaired, and uncertain outcomes require explicit recovery instead of
	// an automatic external-effect retry.
	t.Run("A3/duplicate_claim_keeps_one_scoped_attempt", func(t *testing.T) {
		vault, store, project := journeysArmedAB(t)
		claimed := journeysClaimRoot(t, vault, store, project, "W-0001", "APP-T-0001", "attempt-race-1")
		if !claimed {
			t.Fatal("first claim did not consume")
		}
		root := RunStatus{ProjectID: project.ProjectID, RecordID: "APP-T-0001", ItemID: "APP-T-0001", Runner: string(RunnerCodexExec), Lane: runLaneExecute, LeaseState: string(LeaseStateUnclaimed)}
		duplicate, err := store.claimRunLeaseWithDirectiveAttempt(root, "attempt-race-2", 1, time.Minute, time.Now().UTC(),
			RuntimeLeaseClaimPrecondition{ExpectedLeaseState: LeaseStateUnclaimed},
			RunAuthorization{Source: "human_run_directive", Actor: "human:journey", DirectiveWaveID: "W-0001"},
			RunAttempt{AttemptID: "attempt-race-2", Runner: string(RunnerCodexExec), Lane: runLaneExecute})
		if err != nil {
			t.Fatalf("duplicate claim errored instead of refusing: %v", err)
		}
		if duplicate {
			t.Fatal("a competing claim stole the live reservation")
		}
		attempts, err := store.ListAttemptsForRun(project.ProjectID, "APP-T-0001")
		if err != nil || len(attempts) != 1 || attempts[0].AttemptID != "attempt-race-1" {
			t.Fatalf("race left %d attempts: %#v err=%v", len(attempts), attempts, err)
		}
		directive, err := store.RunDirective(project.ProjectID, "APP-T-0001")
		if err != nil || directive == nil || directive.State != "consumed" {
			t.Fatalf("race revived the consumed directive: %#v err=%v", directive, err)
		}
		// Submission identity is preserved: the recorded authorization
		// names the actual claimant, generation, and attempt.
		auth, err := store.LatestRunAuthorization(project.ProjectID, "APP-T-0001")
		if err != nil || auth == nil {
			t.Fatalf("claim left no authorization: %#v err=%v", auth, err)
		}
		if auth.Actor != "human:journey" || auth.AttemptID != "attempt-race-1" || auth.LeaseGeneration != 1 {
			t.Fatalf("claim recorded the wrong submission identity: %#v", auth)
		}
	})

	t.Run("A3/crash_preserves_attempt_across_restart_without_retry", func(t *testing.T) {
		vault, store, project := journeysArmedAB(t)
		if !journeysClaimRoot(t, vault, store, project, "W-0001", "APP-T-0001", "attempt-crash-1") {
			t.Fatal("claim did not consume")
		}
		// Crash between claim and terminal outcome: the lease lapses with
		// no outcome recorded and no live owner behind it.
		crashed := RunStatus{ProjectID: project.ProjectID, RecordID: "APP-T-0001", ItemID: "APP-T-0001",
			Runner: string(RunnerCodexExec), Lane: runLaneExecute, LeaseState: string(LeaseStateClaimed),
			LeaseOwner: "attempt-crash-1", LeaseGeneration: 1, LeaseExpiresAt: time.Now().UTC().Add(-time.Hour).Format(time.RFC3339),
			ActiveAttemptID: "attempt-crash-1", AttemptOutcome: string(AttemptOutcomeNone), WorkRevision: 1}
		if err := store.UpsertRun(crashed); err != nil {
			t.Fatal(err)
		}
		if revision := deadReservationRevision(crashed); revision == "" {
			t.Fatal("the crashed lease did not classify as a dead reservation")
		}
		// A live holder is uncertainty, never a repair candidate.
		live := crashed
		live.RecordID, live.ItemID = "APP-T-0002", "APP-T-0002"
		live.ProcessPID = os.Getpid()
		liveStartedAt, liveStartedOK := processStartTime(live.ProcessPID)
		if !liveStartedOK {
			t.Fatal("cannot probe this process's start time for the live-holder fixture")
		}
		live.ProcessStartedAt = liveStartedAt
		if deadReservationRevision(live) != "" {
			t.Fatal("a live holder classified as a dead reservation")
		}
		// Restart at the repair boundary: close and reopen the store, then
		// prove the single scoped attempt survived with nothing duplicated.
		stateRoot := store.stateRoot
		if err := store.Close(); err != nil {
			t.Fatal(err)
		}
		reopened, err := OpenRuntimeStore(stateRoot)
		if err != nil {
			t.Fatal(err)
		}
		store = reopened
		t.Cleanup(func() { _ = store.Close() })
		attempts, err := store.ListAttemptsForRun(project.ProjectID, "APP-T-0001")
		if err != nil || len(attempts) != 1 || attempts[0].AttemptID != "attempt-crash-1" {
			t.Fatalf("restart lost or duplicated the scoped attempt: %#v err=%v", attempts, err)
		}
		current, err := store.FindRunScoped(project.ProjectID, "APP-T-0001")
		if err != nil || current == nil || current.ActiveAttemptID != "attempt-crash-1" || current.Terminal {
			t.Fatalf("restart rewrote the crashed run: %#v err=%v", current, err)
		}
		// No automatic retry: the frontier does not queue fresh work for
		// the crashed member and no second attempt appears.
		daemon := &Daemon{store: store, stateRoot: store.stateRoot}
		if err := daemon.advanceAuthorizedWaveFrontiers(project); err != nil {
			t.Fatal(err)
		}
		if attempts, err := store.ListAttemptsForRun(project.ProjectID, "APP-T-0001"); err != nil || len(attempts) != 1 {
			t.Fatalf("poll retried the crashed attempt: %#v err=%v", attempts, err)
		}
	})

	t.Run("A3/unknown_outcome_is_explicit_never_retried", func(t *testing.T) {
		vault, store, project := journeysArmedAB(t)
		if !journeysClaimRoot(t, vault, store, project, "W-0001", "APP-T-0001", "attempt-unknown-1") {
			t.Fatal("claim did not consume")
		}
		uncertain := RunStatus{ProjectID: project.ProjectID, RecordID: "APP-T-0001", ItemID: "APP-T-0001",
			Runner: string(RunnerCodexExec), Lane: runLaneExecute, LeaseState: string(LeaseStateReleased),
			LeaseOwner: "attempt-unknown-1", LeaseGeneration: 1,
			ActiveAttemptID: "attempt-unknown-1", AttemptOutcome: string(AttemptOutcomeUnknown),
			Terminal: true, LastError: "worker vanished mid-flight", WorkRevision: 1}
		if err := store.UpsertRun(uncertain); err != nil {
			t.Fatal(err)
		}
		review, err := buildDirectWaveReview(vault, store, project.ProjectID, "W-0001", nil)
		if err != nil {
			t.Fatal(err)
		}
		member := journeysReviewMember(t, review, "APP-T-0001")
		if member.State != "blocked" || member.Phase != "outcome_unknown" || member.Responsible != "operator" {
			t.Fatalf("uncertain outcome did not route to its operator: %#v", member)
		}
		unknownBlocker := false
		for _, blocker := range review.Blockers {
			unknownBlocker = unknownBlocker || blocker.Code == "OUTCOME_UNKNOWN"
		}
		if !unknownBlocker {
			t.Fatalf("uncertain outcome raised no OUTCOME_UNKNOWN blocker: %#v", review.Blockers)
		}
		if member.Recovery == nil || member.Recovery.Action != "recover_unknown" || !member.Recovery.Enabled {
			t.Fatalf("uncertain outcome offers no enabled typed recovery: %#v", member.Recovery)
		}
		// Uncertain work must not be redriven as a routine retry: the
		// refusal names Verify-and-continue as the one supported path.
		refused, reason := serveRedriveRefusal("backlog", uncertain)
		if !refused || !strings.Contains(reason, "Verify and continue") {
			t.Fatalf("uncertain work redrove without explicit recovery: refused=%v reason=%q", refused, reason)
		}
		daemon := &Daemon{store: store, stateRoot: store.stateRoot}
		if err := daemon.advanceAuthorizedWaveFrontiers(project); err != nil {
			t.Fatal(err)
		}
		if attempts, err := store.ListAttemptsForRun(project.ProjectID, "APP-T-0001"); err != nil || len(attempts) != 1 {
			t.Fatalf("poll retried the uncertain attempt: %#v err=%v", attempts, err)
		}
	})

	t.Run("A3/cancellation_is_bounded_and_preserves_attempt", func(t *testing.T) {
		vault, store, project := journeysArmedAB(t)
		if !journeysClaimRoot(t, vault, store, project, "W-0001", "APP-T-0001", "attempt-cancel-1") {
			t.Fatal("claim did not consume")
		}
		after, changed, err := interruptRuntimeRun(store.stateRoot, store, "APP-T-0001")
		if err != nil {
			t.Fatalf("interrupt failed on a live fixture run: %v", err)
		}
		if changed {
			t.Fatal("interrupt delegated to a daemon that does not exist in the fixture")
		}
		// Bounded teardown releases the live owner and records the
		// cancellation, while the durable attempt row survives untouched.
		if after == nil || string(after.AttemptOutcome) != string(AttemptOutcomeCancelled) || after.Terminal {
			t.Fatalf("interrupt did not record a bounded cancellation: %#v", after)
		}
		if !strings.Contains(after.LastError, "interrupt requested") {
			t.Fatalf("interrupt lost its attribution: %#v", after)
		}
		if attempts, err := store.ListAttemptsForRun(project.ProjectID, "APP-T-0001"); err != nil || len(attempts) != 1 || attempts[0].AttemptID != "attempt-cancel-1" {
			t.Fatalf("cancellation duplicated or dropped the attempt: %#v err=%v", attempts, err)
		}
		// Terminal work is never a repair candidate, however it ended.
		terminal := RunStatus{ProjectID: project.ProjectID, RecordID: "APP-T-0001", ItemID: "APP-T-0001",
			Runner: string(RunnerCodexExec), Lane: runLaneExecute, LeaseState: string(LeaseStateReleased),
			ActiveAttemptID: "attempt-cancel-1", AttemptOutcome: string(AttemptOutcomeSucceeded), Terminal: true}
		if deadReservationRevision(terminal) != "" {
			t.Fatal("terminal work classified as a dead reservation")
		}
		if changed, repaired, escalated, err := autoRepairDeadReservation(store, true, terminal, time.Now().UTC()); err != nil || changed || repaired || escalated {
			t.Fatalf("repair touched terminal work: changed=%v repaired=%v escalated=%v err=%v", changed, repaired, escalated, err)
		}
	})
}

// journeysClaimRoot claims the queued root directive with a real
// fixture-runner attempt and reports whether the claim consumed.
func journeysClaimRoot(t *testing.T, vault string, store *RuntimeStore, project RegisteredProject, waveID, recordID, attemptID string) bool {
	t.Helper()
	root := RunStatus{ProjectID: project.ProjectID, RecordID: recordID, ItemID: recordID, Runner: string(RunnerCodexExec), Lane: runLaneExecute, LeaseState: string(LeaseStateUnclaimed)}
	if err := store.UpsertRun(root); err != nil {
		t.Fatal(err)
	}
	idx, err := loadV7Index(vault)
	if err != nil {
		t.Fatal(err)
	}
	wave := idx.Waves[waveID]
	claimed, err := store.claimRunLeaseWithDirectiveAttempt(root, attemptID, 1, time.Minute, time.Now().UTC(),
		RuntimeLeaseClaimPrecondition{ExpectedLeaseState: LeaseStateUnclaimed},
		RunAuthorization{Source: "human_run_directive", Actor: "human:journey", DirectiveWaveID: waveID,
			DirectiveAuthorizationFingerprint: stringField(wave.Data, "authorization_fingerprint"), DirectiveWaveAuthorizedAt: stringField(wave.Data, "authorized_at")},
		RunAttempt{AttemptID: attemptID, Runner: string(RunnerCodexExec), Lane: runLaneExecute})
	if err != nil {
		t.Fatal(err)
	}
	return claimed
}

// journeysArmedAB authors the standard concurrency-1 armed A->B wave with
// proven verification on both members and returns the live frontier.
func journeysArmedAB(t *testing.T) (string, *RuntimeStore, RegisteredProject) {
	t.Helper()
	vault, store, project, _, _ := selfServiceArmedABFixture(t)
	return vault, store, project
}

// journeysCLITaskStart runs the receiving `task start --mode background`
// command through the actual CLI parser and dispatch, returning the CLI exit
// code and error. Callers assert follow-up store state for postconditions.
func journeysCLITaskStart(t *testing.T, vault, taskID string) (int, error) {
	t.Helper()
	command, args := parseCLI([]string{"tusker", "task", "start", taskID, "--mode", "background", "--by", "operator:journey", "--vault", vault})
	if command != "task start" {
		t.Fatalf("parseCLI routed task start to %q", command)
	}
	var code int
	var err error
	captureStdout(t, func() {
		code, err = runInner(command, args)
	})
	return code, err
}

// journeysRawAppend appends raw bytes to a task or wave file without
// rebinding pins or revisions, simulating an out-of-band edit outside the
// lifecycle. Canon-covered bytes stale both the task pin and the wave
// material fingerprint; ledger-section bytes stale only state_rev, leaving
// the wave authorization intact.
func journeysRawAppend(t *testing.T, vault, kind, id, text string) {
	t.Helper()
	path := filepath.Join(vault, "work", kind, id+".md")
	raw, err := readText(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeText(path, raw+text); err != nil {
		t.Fatal(err)
	}
}

// journeysDriftTaskTitle rewrites a task's frontmatter title outside the
// lifecycle: the title is contract canon and wave material, so both the
// stored task pin and the stored wave authorization stop covering the bytes.
func journeysDriftTaskTitle(t *testing.T, vault, id, title string) {
	t.Helper()
	path := filepath.Join(vault, "work", "tasks", id+".md")
	raw, err := readText(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(raw, "\n")
	replaced := false
	for i, line := range lines {
		if strings.HasPrefix(line, "title: ") {
			lines[i] = "title: " + title
			replaced = true
			break
		}
	}
	if !replaced {
		t.Fatalf("task %s has no frontmatter title line", id)
	}
	if err := writeText(path, strings.Join(lines, "\n")); err != nil {
		t.Fatal(err)
	}
}

// journeysAllGreen is the matrix summary rule: green requires at least one
// scenario and every scenario PASS. Any FAIL, BLOCKED, or NOT RUN verdict —
// or an empty matrix — is not green, so a partial matrix can never report an
// all-green summary.
func journeysAllGreen(results map[string]string) bool {
	if len(results) == 0 {
		return false
	}
	for _, verdict := range results {
		if verdict != "PASS" {
			return false
		}
	}
	return true
}

func journeysCLIWaveReview(t *testing.T, vault, waveID string) (string, int) {
	t.Helper()
	command, args := parseCLI([]string{"tusker", "wave", "review", waveID, "--vault", vault})
	if command != "wave review" {
		t.Fatalf("parseCLI routed wave review to %q", command)
	}
	var code int
	output := captureStdout(t, func() {
		var err error
		code, err = runInner(command, args)
		if err != nil {
			t.Fatalf("wave review command failed: %v", err)
		}
	})
	return output, code
}

func TestSelfServiceJourneysA4(t *testing.T) {
	// Competing waves, older armed work, and gates never silently expand
	// authority and never starve continuously eligible work: each wave
	// advances only under its own fingerprint, drifted authorizations queue
	// nothing until rebound and re-armed, gates refuse acceptance without
	// touching authority, and safe repair retries stay bounded across
	// restart with attributable evidence.
	t.Run("A4/competing_waves_advance_under_own_authority", func(t *testing.T) {
		vault, store, project := authorityFixture(t)
		writeDirectWave(t, vault, "W-0001", []string{"APP-T-0001"}, map[string]any{"concurrency": 1})
		writeDirectWave(t, vault, "W-0002", []string{"APP-T-0002"}, map[string]any{"concurrency": 1})
		writeDirectTask(t, vault, "APP-T-0001", "W-0001", nil)
		writeDirectTask(t, vault, "APP-T-0002", "W-0002", nil)
		if err := store.SetProjectEnabled(project.ProjectID, true); err != nil {
			t.Fatal(err)
		}
		first, err := directWaveStart(vault, store, "W-0001", "human:journey")
		if err != nil {
			t.Fatal(err)
		}
		second, err := directWaveStart(vault, store, "W-0002", "human:journey")
		if err != nil {
			t.Fatal(err)
		}
		if first.MaterialFingerprint == "" || second.MaterialFingerprint == "" || first.MaterialFingerprint == second.MaterialFingerprint {
			t.Fatalf("competing waves share authority: %#v %#v", first, second)
		}
		directives := queuedDirectives(t, store, project.ProjectID)
		if len(directives) != 2 {
			t.Fatalf("competing waves did not both queue their root: %#v", directives)
		}
		binding := map[string]RunDirective{}
		for _, directive := range directives {
			binding[directive.RecordID] = directive
		}
		if binding["APP-T-0001"].WaveID != "W-0001" || binding["APP-T-0001"].AuthorizationFingerprint != first.MaterialFingerprint {
			t.Fatalf("first wave lost its binding: %#v", binding["APP-T-0001"])
		}
		if binding["APP-T-0002"].WaveID != "W-0002" || binding["APP-T-0002"].AuthorizationFingerprint != second.MaterialFingerprint {
			t.Fatalf("second wave lost its binding: %#v", binding["APP-T-0002"])
		}
		// The second Start did not disturb the first wave's reservation.
		if binding["APP-T-0001"].WaveAuthorizedAt == "" {
			t.Fatal("first reservation lost its authorization timestamp")
		}
		// A live claim in one wave does not starve the other: the sibling
		// frontier still holds its queued directive and both waves project
		// authorized.
		if !journeysClaimRoot(t, vault, store, project, "W-0001", "APP-T-0001", "attempt-compete-1") {
			t.Fatal("claim in the first wave did not consume")
		}
		daemon := &Daemon{store: store, stateRoot: store.stateRoot}
		if err := daemon.advanceAuthorizedWaveFrontiers(project); err != nil {
			t.Fatal(err)
		}
		sibling, err := store.RunDirective(project.ProjectID, "APP-T-0002")
		if err != nil || sibling == nil || sibling.State != "queued" || sibling.AuthorizationFingerprint != second.MaterialFingerprint {
			t.Fatalf("sibling wave starved while the first wave executed: %#v err=%v", sibling, err)
		}
		for _, waveID := range []string{"W-0001", "W-0002"} {
			review, err := buildDirectWaveReview(vault, store, project.ProjectID, waveID, nil)
			if err != nil {
				t.Fatal(err)
			}
			if review.Authorization != "authorized" {
				t.Fatalf("%s lost authorization: %s", waveID, review.Authorization)
			}
		}
	})

	t.Run("A4/drifted_wave_holds_dependent_until_rearmed", func(t *testing.T) {
		vault, store, project := journeysArmedAB(t)
		staleFP := queuedDirectives(t, store, project.ProjectID)[0].AuthorizationFingerprint
		journeysDriftTaskTitle(t, vault, "APP-T-0002", "Direct APP-T-0002 (drifted)")
		review, err := buildDirectWaveReview(vault, store, project.ProjectID, "W-0001", nil)
		if err != nil {
			t.Fatal(err)
		}
		if review.Authorization != "stale" {
			t.Fatalf("drifted wave did not project stale: %s", review.Authorization)
		}
		if queued, err := queueAuthorizedWaveFrontier(vault, store, project.ProjectID, "W-0001", time.Now().UTC()); err != nil || len(queued) != 0 {
			t.Fatalf("drifted wave queued frontier work: %v err=%v", queued, err)
		}
		// The sanctioned recovery rebinds the drifted member, then Start
		// issues fresh authority: the root re-queues under it while the
		// dependent stays held on its unfinished dependency.
		reconcileCommand, reconcileArgs := parseCLI([]string{"tusker", "reconcile", "APP-T-0002", "--vault", vault})
		captureStdout(t, func() {
			if code, err := runInner(reconcileCommand, reconcileArgs); err != nil || code != 0 {
				t.Fatalf("reconcile did not repair the drifted member: code=%d err=%v", code, err)
			}
		})
		reboundNow, err := loadV7Index(vault)
		if err != nil {
			t.Fatal(err)
		}
		updateCommand, updateArgs := parseCLI([]string{"tusker", "task", "update", "APP-T-0002",
			"--if-revision", stringField(reboundNow.Tasks["APP-T-0002"].Data, "state_rev"),
			"--rebind-contract", "--by", "human:journey", "--vault", vault})
		captureStdout(t, func() {
			if code, err := runInner(updateCommand, updateArgs); err != nil || code != 0 {
				t.Fatalf("task update did not rebind the drifted member: code=%d err=%v", code, err)
			}
		})
		renewed, err := directWaveStart(vault, store, "W-0001", "human:journey")
		if err != nil {
			t.Fatalf("start did not re-arm rebound material: %v", err)
		}
		if renewed.MaterialFingerprint == staleFP {
			t.Fatalf("re-arm kept the drifted fingerprint: %#v", renewed)
		}
		if len(renewed.QueuedTaskIDs) != 1 || renewed.QueuedTaskIDs[0] != "APP-T-0001" {
			t.Fatalf("re-arm queued the wrong frontier: %#v", renewed.QueuedTaskIDs)
		}
		dependent, err := buildDirectWaveReview(vault, store, project.ProjectID, "W-0001", nil)
		if err != nil {
			t.Fatal(err)
		}
		if member := journeysReviewMember(t, dependent, "APP-T-0002"); member.State != "waiting" || !strings.Contains(member.WaitingReason, "APP-T-0001") {
			t.Fatalf("re-arm silently released the dependent: %#v", member)
		}
	})

	t.Run("A4/open_gate_blocks_accept_without_touching_authority", func(t *testing.T) {
		vault, store, project := journeysArmedAB(t)
		if repo := v7RepoRoot(vault); !v7GitRepo(repo) {
			runGit(t, "-C", repo, "init", "-q")
		}
		if !journeysClaimRoot(t, vault, store, project, "W-0001", "APP-T-0001", "attempt-gate-1") {
			t.Fatal("claim did not consume")
		}
		finished := RunStatus{ProjectID: project.ProjectID, RecordID: "APP-T-0001", ItemID: "APP-T-0001",
			Runner: string(RunnerCodexExec), Lane: runLaneExecute, LeaseState: string(LeaseStateReleased),
			Terminal: true, AttemptOutcome: string(AttemptOutcomeSucceeded)}
		if err := store.UpsertRun(finished); err != nil {
			t.Fatal(err)
		}
		if err := v7TestVerificationMutation(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "by": "reviewer:journey",
			"covers": "A1", "check": "command: go test ./x", "result": "pass", "note": "Gate-branch proof receipt."}); err != nil {
			t.Fatalf("record gate-branch proof: %v", err)
		}
		if err := statusV7Cmd(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "status": "ready", "by": "human:journey"}); err != nil {
			t.Fatalf("admit root to ready: %v", err)
		}
		writeHumanGate(t, vault, "JRN-G-0001", "APP-T-0001")
		beforeFP := journeysWaveFingerprint(t, vault, "W-0001")
		err := acceptV7Cmd(Args{"vault": vault, "quiet": "true", "_pos0": "APP-T-0001", "by": "reviewer:independent"})
		if err == nil || !strings.Contains(err.Error(), "open gate") {
			t.Fatalf("accept ignored the open blocking gate: %v", err)
		}
		after, err := loadV7Index(vault)
		if err != nil {
			t.Fatal(err)
		}
		if status := stringField(after.Tasks["APP-T-0001"].Data, "status"); status != "ready" {
			t.Fatalf("gated refusal stranded the task at %q", status)
		}
		if got := stringField(after.Tasks["APP-T-0001"].Data, "accepted_by"); got != "" {
			t.Fatalf("gated refusal recorded an acceptor: %q", got)
		}
		if fp := journeysWaveFingerprint(t, vault, "W-0001"); fp != beforeFP {
			t.Fatal("gated refusal changed wave authority")
		}
	})

	t.Run("A4/safe_repair_bounded_across_restart_with_evidence", func(t *testing.T) {
		stateRoot := t.TempDir()
		openStore := func() *RuntimeStore {
			store, err := OpenRuntimeStore(stateRoot)
			if err != nil {
				t.Fatal(err)
			}
			return store
		}
		store := openStore()
		now := time.Now().UTC()
		dead := RunStatus{
			ProjectID: "project-journey", RecordID: "JNY-T-0001", ItemID: "JNY-T-0001",
			Runner: string(RunnerCodexExec), Lane: runLaneExecute,
			LeaseState: string(LeaseStateClaimed), LeaseOwner: "agent:dead",
			LeaseGeneration: 1, LeaseExpiresAt: now.Add(-5 * time.Minute).Format(time.RFC3339),
			ActiveAttemptID: "attempt-journey-dead", AttemptOutcome: string(AttemptOutcomeNone), WorkRevision: 1,
		}
		if err := store.UpsertRun(dead); err != nil {
			t.Fatal(err)
		}
		changed, repaired, escalated, err := autoRepairDeadReservation(store, true, dead, now)
		if err != nil || !changed || !repaired || escalated {
			t.Fatalf("first repair must apply cleanly: changed=%v repaired=%v escalated=%v err=%v", changed, repaired, escalated, err)
		}
		// Restart at the repair boundary: the ledger survives, so the
		// identical fault escalates once with evidence instead of
		// repairing again, then stays silent.
		if err := store.Close(); err != nil {
			t.Fatal(err)
		}
		store = openStore()
		defer store.Close()
		if err := store.UpsertRun(dead); err != nil {
			t.Fatal(err)
		}
		changed, repaired, escalated, err = autoRepairDeadReservation(store, true, dead, now)
		if err != nil || changed || repaired || !escalated {
			t.Fatalf("recurrence must escalate once: changed=%v repaired=%v escalated=%v err=%v", changed, repaired, escalated, err)
		}
		changed, repaired, escalated, err = autoRepairDeadReservation(store, true, dead, now)
		if err != nil || changed || repaired || escalated {
			t.Fatalf("escalated fault must stay silent: changed=%v repaired=%v escalated=%v err=%v", changed, repaired, escalated, err)
		}
		escalations, err := store.ListSelfServiceRepairEscalations()
		if err != nil || len(escalations) != 1 {
			t.Fatalf("expected exactly one escalation: %#v err=%v", escalations, err)
		}
		if escalations[0].RecordID != "JNY-T-0001" || strings.TrimSpace(escalations[0].Evidence) == "" {
			t.Fatalf("escalation lacks attributable evidence: %#v", escalations[0])
		}
	})
}

// journeysWaveFingerprint reads the stored wave authorization fingerprint.
func journeysWaveFingerprint(t *testing.T, vault, waveID string) string {
	t.Helper()
	idx, err := loadV7Index(vault)
	if err != nil {
		t.Fatal(err)
	}
	return stringField(idx.Waves[waveID].Data, "authorization_fingerprint")
}
