package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestRunSessionJourney is the provider-free end-to-end qualification seam for
// TSK-T-0054. It uses the real ACP fixture adapter, EventLog reader and
// RuntimeStore, then closes and reopens the store at each recovery boundary.
// Installed-app and live-provider qualification remain separate report rows.
func TestRunSessionJourney(t *testing.T) {
	t.Run("A1/messages_survive_reopen_without_private_content", func(t *testing.T) {
		store, req := setupACPRunnerRuntime(t, "activity")
		started, err := runnerWrapperStartChild(context.Background(), req)
		if err != nil {
			t.Fatal(err)
		}
		waitForStatusFile(t, req.Start.StatusPath)
		if !strings.HasPrefix(started.SessionRef, "acp:v1:fake-acp:") {
			t.Fatalf("fixture session was not namespaced: %q", started.SessionRef)
		}

		events := serveRunEvents(RunStatus{EventSinkPath: req.Start.EventSinkPath, RawLogPath: req.Start.RawLogPath}, nil)
		var activity []string
		for _, event := range events {
			if event.Activity {
				activity = append(activity, event.Text)
			}
		}
		joined := strings.Join(activity, "\n")
		for _, want := range []string{"Running fixture tests.", "Tests finished."} {
			if !strings.Contains(joined, want) {
				t.Fatalf("message/tool activity omitted %q: %#v", want, activity)
			}
		}
		if !strings.Contains(joined, "2 passed") && !strings.Contains(joined, "cargo test") {
			t.Fatalf("tool activity omitted its bounded result: %#v", activity)
		}
		for _, forbidden := range []string{"private-value", "private reasoning", "password=private-value"} {
			if strings.Contains(joined, forbidden) {
				t.Fatalf("private fixture content leaked into public activity: %q", forbidden)
			}
		}

		// Persist the provider session exactly as a daemon handoff would. Generic
		// ACP is intentionally fresh-only, so its readback must stay explicit.
		if err := store.SaveSession(RunnerSession{
			ProjectID: req.Start.ProjectID, RecordID: req.Start.RecordID, Runner: string(RunnerACP),
			SessionRef: started.SessionRef, WorkspacePath: req.Start.WorkspacePath,
			WorkRevision: req.Start.WorkRevision, LastAttemptID: req.Start.AttemptID,
			State: "ended", Resumable: false, StartedAt: started.StartedAt,
			LastSeenAt: time.Now().UTC().Format(time.RFC3339Nano),
			LastError:  "generic ACP adapter does not support native resume",
		}); err != nil {
			t.Fatal(err)
		}
		if err := store.Close(); err != nil {
			t.Fatal(err)
		}
		reopened, err := OpenRuntimeStore(DefaultStateRoot())
		if err != nil {
			t.Fatal(err)
		}
		defer reopened.Close()
		session, err := reopened.LatestSession(req.Start.ProjectID, req.Start.RecordID, string(RunnerACP))
		if err != nil || session == nil {
			t.Fatalf("reopened session missing: %#v err=%v", session, err)
		}
		if session.SessionRef != started.SessionRef || session.Resumable {
			t.Fatalf("reopened session identity/capability changed: %#v", session)
		}
		if capability := resumeCapability(&RunStatus{Runner: string(RunnerACP)}, session); capability.Supported || capability.Reason == "" {
			t.Fatalf("generic ACP resume was not explicit: %#v", capability)
		}
		reopenedEvents := serveRunEvents(RunStatus{EventSinkPath: req.Start.EventSinkPath, RawLogPath: req.Start.RawLogPath}, nil)
		if len(reopenedEvents) != len(events) || !strings.Contains(strings.Join(eventTexts(reopenedEvents), "\n"), "Tests finished.") {
			t.Fatalf("reopened activity changed: before=%#v after=%#v", events, reopenedEvents)
		}
	})

	t.Run("A1/crash_restart_preserves_delivery_unknown", func(t *testing.T) {
		store, req := setupACPRunnerRuntime(t, "eof-after-prompt")
		started, err := runnerWrapperStartChild(context.Background(), req)
		if err != nil {
			t.Fatal(err)
		}
		waitForStatusFile(t, req.Start.StatusPath)
		status, err := readRunnerProcessStatus(req.Start.StatusPath)
		if err != nil {
			t.Fatal(err)
		}
		if AttemptOutcome(status.Outcome) != AttemptOutcomeUnknown || !strings.Contains(status.Reason, "delivery") {
			t.Fatalf("EOF after prompt was not classified as delivery unknown: %#v", status)
		}
		if err := store.SaveSession(RunnerSession{
			ProjectID: req.Start.ProjectID, RecordID: req.Start.RecordID, Runner: string(RunnerACP),
			SessionRef: started.SessionRef, WorkspacePath: req.Start.WorkspacePath,
			WorkRevision: req.Start.WorkRevision, LastAttemptID: req.Start.AttemptID,
			State: "ended", Resumable: false, StartedAt: started.StartedAt,
			LastSeenAt: time.Now().UTC().Format(time.RFC3339Nano), LastError: status.Reason,
		}); err != nil {
			t.Fatal(err)
		}
		if err := store.Close(); err != nil {
			t.Fatal(err)
		}
		reopened, err := OpenRuntimeStore(DefaultStateRoot())
		if err != nil {
			t.Fatal(err)
		}
		defer reopened.Close()
		if session, sessionErr := reopened.LatestSession(req.Start.ProjectID, req.Start.RecordID, string(RunnerACP)); sessionErr != nil || session == nil || session.LastError != status.Reason {
			t.Fatalf("crash/restart lost delivery uncertainty: session=%#v err=%v", session, sessionErr)
		}
		unknown := RunStatus{AttemptOutcome: string(AttemptOutcomeUnknown), LeaseState: string(LeaseStateReleased), Terminal: true}
		if refused, reason := serveRedriveRefusal("ready", unknown); !refused || reason == "" {
			t.Fatalf("unknown external effect was not refused explicitly: refused=%t reason=%q", refused, reason)
		}
	})

	t.Run("A1/duplicate_actions_are_fenced_after_restart", func(t *testing.T) {
		dir := t.TempDir()
		t.Setenv("TUSKER_STATE_ROOT", filepath.Join(dir, "state"))
		store, err := OpenRuntimeStore(DefaultStateRoot())
		if err != nil {
			t.Fatal(err)
		}
		run := RunStatus{
			ProjectID: "isolated-project", RecordID: "TSK-T-0054-journey", ItemID: "TSK-T-0054-journey",
			Runner: string(RunnerDevin), Lane: runLaneExecute, LeaseState: string(LeaseStateUnclaimed),
			AttemptOutcome: string(AttemptOutcomeNone), WorkRevision: 1,
		}
		if err := store.UpsertRun(run); err != nil {
			t.Fatal(err)
		}
		now := time.Now().UTC()
		auth := RunAuthorization{Source: "test", Actor: "human:journey", Trigger: "reconnect"}
		first, err := store.claimRunLeaseWithDaemonAttempt(run, "attempt-first", 1, time.Minute, now,
			RuntimeLeaseClaimPrecondition{ExpectedLeaseState: LeaseStateUnclaimed, ExpectedWorkRevision: run.WorkRevision}, auth,
			RunAttempt{AttemptID: "attempt-first", ProjectID: run.ProjectID, RecordID: run.RecordID, ItemID: run.ItemID, Runner: run.Runner, Lane: run.Lane})
		if err != nil || !first {
			t.Fatalf("first action was not admitted: claimed=%t err=%v", first, err)
		}
		second, err := store.claimRunLeaseWithDaemonAttempt(run, "attempt-second", 1, time.Minute, now.Add(time.Millisecond),
			RuntimeLeaseClaimPrecondition{ExpectedLeaseState: LeaseStateUnclaimed, ExpectedWorkRevision: run.WorkRevision}, auth,
			RunAttempt{AttemptID: "attempt-second", ProjectID: run.ProjectID, RecordID: run.RecordID, ItemID: run.ItemID, Runner: run.Runner, Lane: run.Lane})
		if err != nil || second {
			t.Fatalf("duplicate action bypassed the lease fence: claimed=%t err=%v", second, err)
		}
		claimed, err := store.FindRunScoped(run.ProjectID, run.RecordID)
		if err != nil || claimed == nil || claimed.ActiveAttemptID != "attempt-first" || claimed.LeaseGeneration != 1 {
			t.Fatalf("canonical claim changed after duplicate action: %#v err=%v", claimed, err)
		}
		attempts, err := store.ListAttemptsForRun(run.ProjectID, run.RecordID)
		if err != nil || len(attempts) != 1 || attempts[0].AttemptID != "attempt-first" {
			t.Fatalf("duplicate action created a second worker attempt: %#v err=%v", attempts, err)
		}
		if err := store.SaveSession(RunnerSession{
			ProjectID: run.ProjectID, RecordID: run.RecordID, Runner: run.Runner,
			SessionRef: acpStoredSessionRef("devin", "native-session-1"), WorkspacePath: dir,
			WorkRevision: 1, LastAttemptID: "attempt-first", State: "open", Resumable: true,
			StartedAt: now.Format(time.RFC3339Nano), LastSeenAt: now.Format(time.RFC3339Nano),
		}); err != nil {
			t.Fatal(err)
		}
		if err := store.Close(); err != nil {
			t.Fatal(err)
		}
		reopened, err := OpenRuntimeStore(DefaultStateRoot())
		if err != nil {
			t.Fatal(err)
		}
		defer reopened.Close()
		latest, err := reopened.FindRunScoped(run.ProjectID, run.RecordID)
		if err != nil || latest == nil {
			t.Fatalf("reopen lost canonical run: %#v err=%v", latest, err)
		}
		latestSession, err := reopened.LatestSession(run.ProjectID, run.RecordID, run.Runner)
		if err != nil || latestSession == nil {
			t.Fatalf("reopen lost native session: %#v err=%v", latestSession, err)
		}
		if capability := resumeCapability(latest, latestSession); !capability.Supported || !strings.Contains(capability.Reason, "load") {
			t.Fatalf("native resume capability was not explicit: %#v", capability)
		}
	})

	t.Run("A1/live_owner_reconnect_does_not_spawn_after_reopen", func(t *testing.T) {
		stateRoot := t.TempDir()
		store, run, identity := seedRunSessionReconcile(t, stateRoot, true)
		// Reopen is the boundary under test. The fixture owns one live attempt
		// before the close; reconnect must only reconstruct that same identity.
		if err := store.Close(); err != nil {
			t.Fatal(err)
		}
		reopened, err := OpenRuntimeStore(stateRoot)
		if err != nil {
			t.Fatal(err)
		}
		defer reopened.Close()
		if err := reopened.SaveAttempt(RunAttempt{
			AttemptID: identity.AttemptID, ProjectID: run.ProjectID, RecordID: run.RecordID,
			ItemID: run.ItemID, Runner: run.Runner, Lane: run.Lane, WorkRevision: run.WorkRevision,
			WorkspacePath: run.WorkspacePath, SessionRef: identity.NativeSessionID,
			Outcome: string(AttemptOutcomeNone), StartedAt: run.StartedAt,
		}); err != nil {
			t.Fatal(err)
		}
		before, err := reopened.ListAttemptsForRun(run.ProjectID, run.RecordID)
		if err != nil {
			t.Fatal(err)
		}
		if len(before) != 1 || before[0].AttemptID != identity.AttemptID {
			t.Fatalf("reopen changed live attempt history before reconnect: %#v", before)
		}
		reconnected, err := reopened.ReconcileRunSessionWithProbe(run, func(observed RunStatus) bool {
			return observed.ProcessPID == os.Getpid() && observed.ActiveAttemptID == identity.AttemptID
		})
		if err != nil {
			t.Fatal(err)
		}
		if !reconnected.Reconnectable || reconnected.Identity == nil || reconnected.Identity.AttemptID != identity.AttemptID {
			t.Fatalf("live owner was not reconnectable after reopen: %#v", reconnected)
		}
		after, err := reopened.ListAttemptsForRun(run.ProjectID, run.RecordID)
		if err != nil {
			t.Fatal(err)
		}
		if len(after) != len(before) || after[0].AttemptID != before[0].AttemptID {
			t.Fatalf("read-only reconnect created or replaced a worker attempt: before=%#v after=%#v", before, after)
		}
	})

	t.Run("A1/completion_before_reopen_is_terminal", func(t *testing.T) {
		stateRoot := t.TempDir()
		store, err := OpenRuntimeStore(stateRoot)
		if err != nil {
			t.Fatal(err)
		}
		at := time.Date(2026, time.September, 22, 12, 0, 0, 0, time.UTC)
		run := RunStatus{
			ProjectID: "isolated-project", RecordID: "TSK-T-0054-complete", ItemID: "TSK-T-0054-complete",
			Runner: string(RunnerACP), Lane: runLaneExecute, LeaseState: string(LeaseStateReleased),
			LeaseGeneration: 1, AttemptOutcome: string(AttemptOutcomeSucceeded), ActiveAttemptID: "",
			AttemptCount: 1, FinalSummary: "fixture completed", Terminal: true,
			StartedAt: at.Add(-time.Minute).Format(time.RFC3339Nano), UpdatedAt: at.Format(time.RFC3339Nano),
		}
		if err := store.UpsertRun(run); err != nil {
			t.Fatal(err)
		}
		if err := store.SaveAttempt(RunAttempt{
			AttemptID: "attempt-complete", ProjectID: run.ProjectID, RecordID: run.RecordID, ItemID: run.ItemID,
			Runner: run.Runner, Lane: run.Lane, WorkRevision: run.WorkRevision, Outcome: string(AttemptOutcomeSucceeded),
			FinalSummary: run.FinalSummary, StartedAt: run.StartedAt, FinishedAt: at.Format(time.RFC3339Nano),
		}); err != nil {
			t.Fatal(err)
		}
		if err := store.SaveSession(RunnerSession{
			ProjectID: run.ProjectID, RecordID: run.RecordID, Runner: run.Runner,
			SessionRef: acpStoredSessionRef("fake-acp", "completed"), State: "ended", Resumable: false,
			WorkRevision: run.WorkRevision, LastAttemptID: "attempt-complete", StartedAt: run.StartedAt,
			LastSeenAt: at.Format(time.RFC3339Nano), EndedAt: at.Format(time.RFC3339Nano),
			LastError: "completed receipt persisted",
		}); err != nil {
			t.Fatal(err)
		}
		if err := store.Close(); err != nil {
			t.Fatal(err)
		}
		reopened, err := OpenRuntimeStore(stateRoot)
		if err != nil {
			t.Fatal(err)
		}
		defer reopened.Close()
		canonical, err := reopened.FindRunScoped(run.ProjectID, run.RecordID)
		if err != nil || canonical == nil {
			t.Fatalf("completed run was not retained across reopen: %#v err=%v", canonical, err)
		}
		if !canonical.Terminal || canonical.AttemptOutcome != string(AttemptOutcomeSucceeded) || canonical.FinalSummary != run.FinalSummary {
			t.Fatalf("completed receipt became resumable or lost its outcome: %#v", canonical)
		}
		if outcome := serveRunOutcome(*canonical, at.Add(time.Second)); outcome != "succeeded" {
			t.Fatalf("reopened terminal receipt projected as %q", outcome)
		}
		attempts, err := reopened.ListAttemptsForRun(run.ProjectID, run.RecordID)
		if err != nil || len(attempts) != 1 || attempts[0].AttemptID != "attempt-complete" || attempts[0].FinishedAt == "" {
			t.Fatalf("completed attempt receipt changed across reopen: %#v err=%v", attempts, err)
		}
		session, err := reopened.LatestSession(run.ProjectID, run.RecordID, run.Runner)
		if err != nil || session == nil || session.Resumable {
			t.Fatalf("completed session capability was not preserved as fresh-only: %#v err=%v", session, err)
		}
	})

	t.Run("A1/context_recovery_does_not_reuse_historical_session", func(t *testing.T) {
		stateRoot := t.TempDir()
		store, err := OpenRuntimeStore(stateRoot)
		if err != nil {
			t.Fatal(err)
		}
		defer store.Close()
		at := time.Date(2026, time.September, 22, 12, 0, 0, 0, time.UTC)
		run := RunStatus{
			ProjectID: "isolated-project", RecordID: "TSK-T-0054-context", ItemID: "TSK-T-0054-context",
			Runner: string(RunnerCodexExec), Lane: runLaneExecute, LeaseState: string(LeaseStateReleased),
			LeaseGeneration: 2, AttemptOutcome: string(AttemptOutcomeUnknown), ActiveAttemptID: "attempt-uncertain",
			SessionRef: "session-old", AttemptCount: 2, WorkRevision: 3, Terminal: true,
			StartedAt: at.Add(-time.Minute).Format(time.RFC3339Nano), UpdatedAt: at.Format(time.RFC3339Nano),
		}
		if err := store.UpsertRun(run); err != nil {
			t.Fatal(err)
		}
		if err := store.SaveAttempt(RunAttempt{
			AttemptID: run.ActiveAttemptID, ProjectID: run.ProjectID, RecordID: run.RecordID, ItemID: run.ItemID,
			Runner: run.Runner, Lane: run.Lane, WorkRevision: run.WorkRevision, SessionRef: run.SessionRef,
			Outcome: string(AttemptOutcomeUnknown), StartedAt: run.StartedAt,
		}); err != nil {
			t.Fatal(err)
		}
		for _, session := range []RunnerSession{
			{ProjectID: run.ProjectID, RecordID: run.RecordID, Runner: run.Runner, SessionRef: "session-old", WorkRevision: run.WorkRevision, LastAttemptID: run.ActiveAttemptID, State: "ended", Resumable: true, StartedAt: run.StartedAt, LastSeenAt: at.Format(time.RFC3339Nano)},
			{ProjectID: run.ProjectID, RecordID: run.RecordID, Runner: run.Runner, SessionRef: "session-newer", WorkRevision: run.WorkRevision, LastAttemptID: "attempt-newer", State: "ended", Resumable: true, StartedAt: at.Format(time.RFC3339Nano), LastSeenAt: at.Add(time.Minute).Format(time.RFC3339Nano)},
		} {
			if err := store.SaveSession(session); err != nil {
				t.Fatal(err)
			}
		}
		wave := Note{Data: map[string]any{
			"id": "W-0039", "authorization": "armed", "authorization_fingerprint": "sha256:wave",
			"authorized_at": at.Add(-time.Minute).Format(time.RFC3339Nano),
		}}
		result, err := queueOutcomeUnknownContextRecovery(store, Note{Data: map[string]any{"id": run.ItemID}}, wave, run, "human:journey", at)
		if err != nil {
			t.Fatal(err)
		}
		if !result.OK || !result.Admitted {
			t.Fatalf("context recovery was not admitted: %#v", result)
		}
		queued, err := store.FindRunScoped(run.ProjectID, run.RecordID)
		if err != nil || queued == nil {
			t.Fatalf("read queued context recovery: %#v err=%v", queued, err)
		}
		if queued.SessionRef != "" || queued.ActiveAttemptID != "" || queued.AttemptCount != run.AttemptCount || queued.LeaseState != string(LeaseStateRetryQueued) {
			t.Fatalf("context recovery retained current execution identity or changed budget: %#v", queued)
		}
		if directive, err := store.RunDirective(run.ProjectID, run.RecordID); err != nil || directive == nil || directive.State != "queued" {
			t.Fatalf("context recovery directive=%#v err=%v", directive, err)
		}
		decisionRows, err := store.ListRuntimeSupervisorDecisionsForRun(run.ProjectID, run.RecordID)
		if err != nil || len(decisionRows) != 1 || decisionRows[0].ParentSessionRef != run.SessionRef || decisionRows[0].TargetSessionRef != "" {
			t.Fatalf("context recovery reused or lost parent session lineage: %#v err=%v", decisionRows, err)
		}
		if err := store.Close(); err != nil {
			t.Fatal(err)
		}
		reopened, err := OpenRuntimeStore(stateRoot)
		if err != nil {
			t.Fatal(err)
		}
		defer reopened.Close()
		canonical, err := reopened.FindRunScoped(run.ProjectID, run.RecordID)
		if err != nil || canonical == nil {
			t.Fatalf("reopen lost queued context recovery: %#v err=%v", canonical, err)
		}
		reconciled, err := reopened.ReconcileRunSessionWithProbe(*canonical, func(RunStatus) bool { return false })
		if err != nil {
			t.Fatal(err)
		}
		if reconciled.Session != nil {
			t.Fatalf("reopened context recovery reused LatestSession %q", reconciled.Session.SessionRef)
		}
	})

	report, err := os.ReadFile(filepath.Join(repoRootForFreshCloneTest(t), "docs/reports/run-session-continuity-qualification.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, marker := range []string{"A1", "A2", "A3", "installed", "live-provider", "NOT RUN"} {
		if !strings.Contains(string(report), marker) {
			t.Fatalf("qualification report is missing required boundary marker %q", marker)
		}
	}
}

func eventTexts(events []serveRunEvent) []string {
	texts := make([]string, 0, len(events))
	for _, event := range events {
		texts = append(texts, event.Text)
	}
	return texts
}
