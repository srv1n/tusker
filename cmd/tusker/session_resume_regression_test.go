package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMuseHardSayQueuedContinuationSurvivesRetryCap(t *testing.T) {
	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	d := &Daemon{store: store}
	now := time.Now().UTC()
	run := RunStatus{ProjectID: "p", RecordID: "P-T-0001", Runner: string(RunnerMuse), Lane: runLaneExecute,
		LeaseState: string(LeaseStateRetryQueued), SessionRef: "muse-session", AttemptCount: 4,
		LastError: "native continuation requested by human:test: muse-session"}
	if queued, err := store.QueueRunDirective(RunDirective{ProjectID: run.ProjectID, RecordID: run.RecordID,
		Actor: "human:test", CreatedAt: now.Format(time.RFC3339Nano), ExpiresAt: now.Add(time.Minute).Format(time.RFC3339Nano)}); err != nil || !queued {
		t.Fatalf("queue Say directive: queued=%t err=%v", queued, err)
	}
	if got, parked := d.parkRetryQueuedRunAtAttemptCap(RegisteredProject{ProjectID: "p"}, Workflow{}, run, "retry cap"); parked || got.LeaseState != string(LeaseStateRetryQueued) {
		t.Fatalf("operator Say was parked: %+v", got)
	}
	run.LastError = "automation plan do_not_dispatch: queued directive still owns continuation"
	if _, parked := d.parkRetryQueuedRunAtAttemptCap(RegisteredProject{ProjectID: "p"}, Workflow{}, run, "retry cap"); parked {
		t.Fatal("plan reconciliation parked the queued Say")
	}
}

func TestMuseFailedLaunchSessionRequiresProviderConfirmation(t *testing.T) {
	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	d := &Daemon{store: store}
	path := filepath.Join(t.TempDir(), "muse.raw.log")
	if err := os.WriteFile(path, []byte("missing meta credentials\n"), 0600); err != nil {
		t.Fatal(err)
	}
	run := RunStatus{ProjectID: "p", Runner: string(RunnerMuse), SessionRef: "proposed-muse", RawLogPath: path}
	if resumable, err := d.confirmedSessionResumable(Workflow{}, run); err != nil || resumable {
		t.Fatal("preassigned Muse ID was confirmed without a session stream")
	}
	if err := store.SaveSession(RunnerSession{ProjectID: "p", RecordID: "task", Runner: run.Runner, SessionRef: run.SessionRef, Resumable: false}); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkSessionState("p", run.SessionRef, "failed", "", "credentials missing", true); err != nil {
		t.Fatal(err)
	}
	stored, err := store.FindSessionByRef("p", run.SessionRef)
	if err != nil || stored == nil || stored.Resumable {
		t.Fatalf("failed launch became resumable: %+v, %v", stored, err)
	}
	if err := os.WriteFile(path, []byte(`{"stream":{"kind":"session","id":"proposed-muse"},"payload_type":"runtime.command.accepted"}`+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if got := extractSessionRef(path); got != run.SessionRef {
		t.Fatalf("Muse session stream identity = %q", got)
	}
	if resumable, err := d.confirmedSessionResumable(Workflow{}, run); err != nil || !resumable {
		t.Fatal("Muse session stream did not confirm its session")
	}
	if stored, err := store.FindSessionByRef("p", run.SessionRef); err != nil || stored == nil || !stored.Resumable {
		t.Fatalf("confirmed Muse session was not promoted for later resume: %+v, %v", stored, err)
	}
}

func TestClaudeFailedLaunchSessionRequiresProviderConfirmation(t *testing.T) {
	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	d := &Daemon{store: store}
	path := filepath.Join(t.TempDir(), "claude.raw.log")
	if err := os.WriteFile(path, []byte("No conversation found\n"), 0600); err != nil {
		t.Fatal(err)
	}
	run := RunStatus{ProjectID: "p", Runner: string(RunnerClaude), SessionRef: "proposed-claude", RawLogPath: path}
	if resumable, err := d.confirmedSessionResumable(Workflow{}, run); err != nil || resumable {
		t.Fatal("preassigned Claude ID was confirmed without init")
	}
	if err := store.SaveSession(RunnerSession{ProjectID: "p", RecordID: "task", Runner: run.Runner, SessionRef: run.SessionRef, Resumable: false}); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkSessionState("p", run.SessionRef, "failed", "", "No conversation found", true); err != nil {
		t.Fatal(err)
	}
	stored, err := store.FindSessionByRef("p", run.SessionRef)
	if err != nil || stored == nil || stored.Resumable {
		t.Fatalf("failed Claude launch became resumable: %+v, %v", stored, err)
	}
	if err := os.WriteFile(path, []byte(`{"type":"assistant","session_id":"proposed-claude"}`+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if resumable, err := d.confirmedSessionResumable(Workflow{}, run); err != nil || resumable {
		t.Fatal("Claude non-init event confirmed a failed launch")
	}
	if err := os.WriteFile(path, []byte(`{"type":"system","subtype":"init","session_id":"proposed-claude"}`+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if resumable, err := d.confirmedSessionResumable(Workflow{}, run); err != nil || !resumable {
		t.Fatal("Claude init did not confirm its session")
	}
}
