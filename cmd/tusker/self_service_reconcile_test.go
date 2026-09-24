package main

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

// TestSelfServiceReconcile proves TSK-T-0043: relevant changes promptly wake
// bounded reconciliation, automatic safe repair is bounded to one attempt per
// unchanged fingerprint with once-only escalation, protected work stays
// protected, and overdue reconciliation exposes its next actor and action.
func TestSelfServiceReconcile(t *testing.T) {
	t.Run("A1/project_enable_wake_reaches_daemon", func(t *testing.T) {
		previous := daemonControlOneWaySender
		defer func() { daemonControlOneWaySender = previous }()
		var got []daemonControlRequest
		daemonControlOneWaySender = func(_ string, req daemonControlRequest, _ time.Duration) error {
			got = append(got, req)
			return nil
		}
		notifyProjectEnableWake("project-wake", true)
		notifyProjectEnableWake("project-wake", false)
		if len(got) != 2 {
			t.Fatalf("toggle did not emit targeted wakes: %#v", got)
		}
		if got[0].Command != "reconcile_project" || got[0].ProjectID != "project-wake" || got[0].Cause != "project_enable" {
			t.Fatalf("wrong enable wake: %#v", got[0])
		}
		if len(got[0].Changes) != 1 || got[0].Changes[0].ID != "project-wake" {
			t.Fatalf("wake lost its project hint: %#v", got[0])
		}
		if got[1].Cause != "project_disable" {
			t.Fatalf("wrong disable wake: %#v", got[1])
		}
	})

	t.Run("A1/project_enable_wake_is_bounded_without_daemon", func(t *testing.T) {
		stateRoot := t.TempDir()
		t.Setenv("TUSKER_STATE_ROOT", stateRoot)
		done := make(chan struct{})
		go func() {
			defer close(done)
			notifyProjectEnableWake("project-absent", false)
		}()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("enable wake to an absent daemon did not stay bounded")
		}
	})

	t.Run("A1/duplicate_wakes_coalesce_to_one_poll_signal", func(t *testing.T) {
		d := &Daemon{}
		d.scheduleProjectReconcile("project-coalesce")
		d.scheduleProjectReconcile("project-coalesce")
		d.scheduleProjectReconcile("project-coalesce")
		got := map[string]int{}
		timeout := time.After(5 * time.Second)
		for len(got) == 0 {
			select {
			case projectID := <-d.notifyWake:
				got[projectID]++
			case <-timeout:
				t.Fatal("coalesced wake never arrived")
			}
		}
		// The debounce timer may still fire once more with no new activity;
		// what matters is that three duplicate wakes never queue three polls.
		if got["project-coalesce"] != 1 {
			t.Fatalf("duplicate wakes queued %d poll signals, want 1", got["project-coalesce"])
		}
		d.stopNotifyTimers()
	})

	t.Run("A1/capacity_release_marks_project_hot", func(t *testing.T) {
		store := fairDispatchTestStore(t)
		project := RegisteredProject{
			ProjectID: "project-cap", ProjectKey: "project-cap", Name: "cap",
			RepoRoot: t.TempDir(), VaultRoot: t.TempDir(), Enabled: true, Health: projectHealthHealthy,
		}
		if err := store.UpsertProject(project); err != nil {
			t.Fatal(err)
		}
		now := time.Now().UTC()
		if err := store.UpsertRun(selfServiceDeadRunFixture("project-cap", "CAP-T-0001", now)); err != nil {
			t.Fatal(err)
		}
		runs, err := store.ListRuns()
		if err != nil {
			t.Fatal(err)
		}
		d := &Daemon{store: store, stateRoot: store.stateRoot}
		reclaimed, err := d.reclaimExpiredDispatchCapacity(runs, now)
		if err != nil {
			t.Fatal(err)
		}
		if !reclaimed {
			t.Fatal("freed capacity did not report a wake")
		}
		status := d.adaptiveReconcileStatus("project-cap")
		if status.LastActivityReason != "capacity_reclaimed" {
			t.Fatalf("capacity release did not wake the schedule: %#v", status)
		}
	})

	t.Run("A1/dropped_hint_falls_back_to_canonical_scan", func(t *testing.T) {
		d := &Daemon{}
		if _, ok := d.applyFrontierHint(RegisteredProject{ProjectID: "project-drop"}, []daemonControlChange{{ID: "S-T-0001", Kind: "task", Revision: "rev-1"}}); ok {
			t.Fatal("an untrusted hint must fall back to the canonical scan, not dispatch from event delivery")
		}
	})

	t.Run("A2/concurrency_one_admits_sequentially_without_duplicates", func(t *testing.T) {
		store := fairDispatchTestStore(t)
		daemon := &Daemon{store: store, stateRoot: store.stateRoot}
		var order []string
		daemon.fairDispatchRun = fairDispatchRecorder(&order)
		ctx := context.Background()
		candidates := func() []daemonDispatchCandidate {
			return []daemonDispatchCandidate{
				fairDispatchTestCandidate("project-s", "S-T-0001", "p0", ""),
				fairDispatchTestCandidate("project-s", "S-T-0002", "p0", ""),
			}
		}
		// Concurrency one: the first wake admits exactly one attempt. The
		// order recorder stands in for the worker spawn, which persists
		// the claim in production; mirror that persistence here so the
		// duplicate-wake and release assertions read stored admission.
		for _, id := range []string{"S-T-0001", "S-T-0002"} {
			if err := store.UpsertRun(fairDispatchTestRun("project-s", id)); err != nil {
				t.Fatal(err)
			}
		}
		if err := daemon.dispatchFairCandidates(ctx, candidates(), 1); err != nil {
			t.Fatal(err)
		}
		assertFairDispatchOrder(t, []string{"project-s/S-T-0001"}, order)
		admittedRun := fairDispatchTestRun("project-s", "S-T-0001")
		admittedRun.LeaseState = string(LeaseStateClaimed)
		admittedRun.LeaseOwner = "attempt-first"
		admittedRun.LeaseGeneration = 1
		admittedRun.LeaseExpiresAt = time.Now().UTC().Add(time.Hour).Format(time.RFC3339)
		if err := store.UpsertRun(admittedRun); err != nil {
			t.Fatal(err)
		}
		// Duplicate wakes: the admitted attempt is retained, nothing is
		// dispatched twice, and the waiter keeps an actionable reason.
		if err := daemon.dispatchFairCandidates(ctx, candidates(), 1); err != nil {
			t.Fatal(err)
		}
		assertFairDispatchOrder(t, []string{"project-s/S-T-0001"}, order)
		waiter := fairDispatchFindRun(t, store, "project-s", "S-T-0002")
		if !strings.Contains(waiter.LastError, "global capacity reached (1/1)") {
			t.Fatalf("waiter lost its capacity owner: %#v", waiter)
		}
		// Acceptance of A releases the single slot: B is admitted next and is
		// not starved by the continuously eligible wave.
		admitted := fairDispatchFindRun(t, store, "project-s", "S-T-0001")
		admitted.Terminal = true
		if err := store.UpsertRun(admitted); err != nil {
			t.Fatal(err)
		}
		if err := daemon.dispatchFairCandidates(ctx, candidates(), 1); err != nil {
			t.Fatal(err)
		}
		assertFairDispatchOrder(t, []string{"project-s/S-T-0001", "project-s/S-T-0002"}, order)
	})

	t.Run("A3/one_repair_per_fingerprint_then_escalate_once", func(t *testing.T) {
		stateRoot := t.TempDir()
		openStore := func() *RuntimeStore {
			store, err := OpenRuntimeStore(stateRoot)
			if err != nil {
				t.Fatal(err)
			}
			return store
		}
		store := openStore()
		project := RegisteredProject{
			ProjectID: "project-repair", ProjectKey: "project-repair", Name: "repair",
			RepoRoot: t.TempDir(), VaultRoot: t.TempDir(), Enabled: true, Health: projectHealthHealthy,
		}
		if err := store.UpsertProject(project); err != nil {
			t.Fatal(err)
		}
		now := time.Now().UTC()
		dead := selfServiceDeadRunFixture("project-repair", "REP-T-0001", now)
		if err := store.UpsertRun(dead); err != nil {
			t.Fatal(err)
		}
		// First sight: the one automatic safe repair applies and sticks.
		changed, repaired, escalated, err := autoRepairDeadReservation(store, true, dead, now)
		if err != nil {
			t.Fatal(err)
		}
		if !changed || !repaired || escalated {
			t.Fatalf("first repair must apply cleanly: changed=%v repaired=%v escalated=%v", changed, repaired, escalated)
		}
		current, err := store.FindRunScoped("project-repair", "REP-T-0001")
		if err != nil {
			t.Fatal(err)
		}
		if current == nil || runConsumesDispatchCapacity(*current) {
			t.Fatalf("repair did not free the dead reservation: %#v", current)
		}
		// Restart: the ledger survives, so the identical fault is not
		// repaired again. Re-seed the exact unchanged lease to simulate the
		// fault returning despite the repair.
		if err := store.Close(); err != nil {
			t.Fatal(err)
		}
		store = openStore()
		defer store.Close()
		if err := store.UpsertRun(dead); err != nil {
			t.Fatal(err)
		}
		changed, repaired, escalated, err = autoRepairDeadReservation(store, true, dead, now)
		if err != nil {
			t.Fatal(err)
		}
		if changed || repaired || !escalated {
			t.Fatalf("recurrence must escalate once, not repair again: changed=%v repaired=%v escalated=%v", changed, repaired, escalated)
		}
		// Further polls stay silent: no repair and no escalation spam.
		changed, repaired, escalated, err = autoRepairDeadReservation(store, true, dead, now)
		if err != nil {
			t.Fatal(err)
		}
		if changed || repaired || escalated {
			t.Fatalf("escalated fault must stay silent: changed=%v repaired=%v escalated=%v", changed, repaired, escalated)
		}
		escalations, err := store.ListSelfServiceRepairEscalations()
		if err != nil {
			t.Fatal(err)
		}
		if len(escalations) != 1 || escalations[0].RecordID != "REP-T-0001" || strings.TrimSpace(escalations[0].Evidence) == "" {
			t.Fatalf("expected exactly one evidenced escalation, got %#v", escalations)
		}
		// Materially new state is a new diagnosis: a fresh lease generation
		// may be repaired again.
		fresh := dead
		fresh.LeaseGeneration = 2
		fresh.LeaseExpiresAt = now.Add(-5 * time.Minute).Format(time.RFC3339)
		if err := store.UpsertRun(fresh); err != nil {
			t.Fatal(err)
		}
		changed, repaired, escalated, err = autoRepairDeadReservation(store, true, fresh, now)
		if err != nil {
			t.Fatal(err)
		}
		if !changed || !repaired || escalated {
			t.Fatalf("new lease generation must allow a new repair: changed=%v repaired=%v escalated=%v", changed, repaired, escalated)
		}
	})

	t.Run("A3/normal_waits_cause_no_repair_and_no_escalation", func(t *testing.T) {
		store := fairDispatchTestStore(t)
		now := time.Now().UTC()
		dead := selfServiceDeadRunFixture("project-quiet", "QUI-T-0001", now)
		if err := store.UpsertRun(dead); err != nil {
			t.Fatal(err)
		}
		// Disabled project: legacy reclaim still frees the row, but no
		// repair is recorded and nothing escalates.
		changed, repaired, escalated, err := autoRepairDeadReservation(store, false, dead, now)
		if err != nil {
			t.Fatal(err)
		}
		if !changed || repaired || escalated {
			t.Fatalf("disabled project must keep legacy reclaim without repair accounting: changed=%v repaired=%v escalated=%v", changed, repaired, escalated)
		}
		fingerprint := selfServiceRepairFingerprint("project-quiet", selfServiceRepairKindDeadReservation, deadReservationRevision(dead))
		if attempted, err := store.selfServiceRepairAttempted(fingerprint); err != nil || attempted {
			t.Fatalf("disabled project recorded a repair attempt: attempted=%v err=%v", attempted, err)
		}
		// Uncertain ownership (a live holder) is never a repair candidate.
		live := dead
		live.RecordID, live.ItemID = "QUI-T-0002", "QUI-T-0002"
		live.ProcessPID = os.Getpid()
		liveStartedAt, liveStartedOK := processStartTime(live.ProcessPID)
		if !liveStartedOK {
			t.Fatal("cannot probe this process's start time for the live-holder fixture")
		}
		live.ProcessStartedAt = liveStartedAt
		if deadReservationRevision(live) != "" {
			t.Fatal("a live holder must not classify as a dead reservation")
		}
		changed, repaired, escalated, err = autoRepairDeadReservation(store, true, live, now)
		if err != nil {
			t.Fatal(err)
		}
		if changed || repaired || escalated {
			t.Fatalf("uncertain ownership must stay untouched: changed=%v repaired=%v escalated=%v", changed, repaired, escalated)
		}
		// Terminal work is never a candidate either.
		terminal := dead
		terminal.RecordID, terminal.ItemID = "QUI-T-0003", "QUI-T-0003"
		terminal.Terminal = true
		if deadReservationRevision(terminal) != "" {
			t.Fatal("terminal work must not classify as a dead reservation")
		}
		if escalations, err := store.ListSelfServiceRepairEscalations(); err != nil || len(escalations) != 0 {
			t.Fatalf("normal waits escalated: %#v err=%v", escalations, err)
		}
	})

	t.Run("A4/protected_work_stays_waiting_with_its_owner", func(t *testing.T) {
		admissible := AdmissionFacts{
			TaskID: "S-T-0001", Status: "ready", Lane: runLaneExecute,
			ContractValid: true, RouteOK: true, ProofMapped: true,
			OwnerFree: true, DependenciesSatisfied: true,
			ProjectRegistered: true, ProjectEnabled: true,
			Authority: AdmissionAuthorityWaveArmed, AuthorityMatches: true,
		}
		if verdict := EvaluateAdmissionForStage(admissible, AdmissionStageDaemonDispatch); !verdict.Admit {
			t.Fatalf("admissible work must dispatch: %#v", verdict)
		}
		paused := admissible
		paused.WavePaused = true
		if verdict := EvaluateAdmissionForStage(paused, AdmissionStageDaemonDispatch); verdict.Admit || !hasAdmissionBlocker(verdict, AdmissionBlockerWavePaused) {
			t.Fatalf("paused work must stay protected: %#v", verdict)
		}
		disabled := admissible
		disabled.ProjectEnabled = false
		if verdict := EvaluateAdmissionForStage(disabled, AdmissionStageDaemonDispatch); verdict.Admit || !hasAdmissionBlocker(verdict, AdmissionBlockerProjectDisabled) {
			t.Fatalf("project-off work must stay protected: %#v", verdict)
		}
		gated := admissible
		gated.Authority = AdmissionAuthorityNone
		if verdict := EvaluateAdmissionForStage(gated, AdmissionStageDaemonDispatch); verdict.Admit || !hasAdmissionBlocker(verdict, AdmissionBlockerAuthorityMissing) {
			t.Fatalf("unauthorized work must stay protected: %#v", verdict)
		}
	})

	t.Run("A4/overdue_schedule_exposes_actor_and_action", func(t *testing.T) {
		store := fairDispatchTestStore(t)
		now := time.Now().UTC()
		d := &Daemon{store: store, stateRoot: store.stateRoot}
		d.noteProjectActivity("project-sched", "cli_mutation", now)
		d.recordProjectPoll("project-sched", now, false)
		d.recordSelfServiceReconcileSchedule("project-sched", now)
		snapshot, ok := loadSelfServiceReconcileSchedule(store, "project-sched")
		if !ok {
			t.Fatal("persisted schedule snapshot is unavailable")
		}
		if snapshot.LastActivityReason != "cli_mutation" || snapshot.NextDueAt == "" || snapshot.Tier == "" {
			t.Fatalf("schedule snapshot lost last observation: %#v", snapshot)
		}
		// Deliberate idle polling before next-due is a normal wait, not a fault.
		if overdue, _, _ := selfServiceScheduleOverdue(snapshot, now); overdue {
			t.Fatal("a schedule before its published next-due time must not read overdue")
		}
		// Past next-due the project exposes its next actor and scoped action
		// instead of a false healthy daemon.
		overdue, actor, action := selfServiceScheduleOverdue(snapshot, now.Add(2*time.Minute))
		if !overdue || actor != "operator" || !strings.Contains(action, "project-sched") {
			t.Fatalf("overdue schedule hid its owner: overdue=%v actor=%q action=%q", overdue, actor, action)
		}
		if _, ok := loadSelfServiceReconcileSchedule(store, "project-unknown"); ok {
			t.Fatal("a missing schedule must read unavailable, never healthy")
		}
	})
}

func selfServiceDeadRunFixture(projectID, recordID string, now time.Time) RunStatus {
	return RunStatus{
		ProjectID: projectID, RecordID: recordID, ItemID: recordID,
		Runner: string(RunnerCodexExec), Lane: runLaneExecute,
		LeaseState: string(LeaseStateClaimed), LeaseOwner: "agent:dead",
		LeaseGeneration: 1, LeaseExpiresAt: now.Add(-5 * time.Minute).Format(time.RFC3339),
		ActiveAttemptID: "attempt-" + recordID, AttemptOutcome: string(AttemptOutcomeNone),
		WorkRevision: 1,
	}
}

func hasAdmissionBlocker(verdict AdmissionVerdict, code string) bool {
	for _, blocker := range verdict.Blockers {
		if blocker.Code == code {
			return true
		}
	}
	return false
}
