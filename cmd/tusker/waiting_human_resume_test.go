package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A worker that yields on an unanswered yield question is waiting for the
// human, not finished: its stored native session must stay resumable so
// `runs continue` resolves the same session instead of refusing with
// "stored session is not resumable".
func TestWaitingHumanYieldKeepsSessionResumableForContinue(t *testing.T) {
	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	vault := t.TempDir()
	workspace := t.TempDir()
	files := t.TempDir()
	if err := writeText(filepath.Join(vault, "work", "tasks", "APP-T-0001.md"), "---\nid: APP-T-0001\nrecord_id: APP-T-0001\nstatus: ready\nwork_revision: 1\n---\n"); err != nil {
		t.Fatal(err)
	}
	project := RegisteredProject{ProjectID: "app", ProjectKey: "app", Name: "app", RepoRoot: t.TempDir(), VaultRoot: vault, Enabled: true, Health: projectHealthHealthy}
	if err := store.UpsertProject(project); err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC()
	statusPath := filepath.Join(files, "status.json")
	if err := os.WriteFile(statusPath, []byte(`{"exit_code":0,"completed_at":"`+now.Format(time.RFC3339)+`","outcome":"succeeded"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	promptPath := filepath.Join(files, "prompt.md")
	prompt := "attempt prompt body\n\n" + resumePromptContextHeader + "\n\n" + resumePromptContextMarkerPrefix + "sha256:" + strings.Repeat("ab", 32) + "`"
	if err := writeText(promptPath, prompt); err != nil {
		t.Fatal(err)
	}

	run := RunStatus{
		ProjectID: "app", RecordID: "APP-T-0001", ItemID: "APP-T-0001",
		Runner: string(RunnerCodexExec), Lane: runLaneExecute,
		LeaseState: string(LeaseStateRunning), LeaseOwner: "attempt-1", LeaseGeneration: 1,
		AttemptOutcome: string(AttemptOutcomeNone), ActiveAttemptID: "attempt-1",
		WorkRevision: 1, AttemptCount: 1, SessionRef: "thread-1",
		WorkspacePath: workspace, PromptPath: promptPath, StatusPath: statusPath,
		StartedAt: now.Format(time.RFC3339), UpdatedAt: now.Format(time.RFC3339),
	}
	if err := store.UpsertRun(run); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.PutAgentMessage(AgentMessage{
		ProjectID: "app", IdempotencyKey: "q", Sender: "task:APP-T-0001",
		Recipient: AgentAddress{Kind: "operator", ID: "operator"}, OriginTaskID: "APP-T-0001",
		WorkRevision: 1, RouteGeneration: 1, Kind: "question", Body: "Pick A or B",
		ReplyRequired: true, YieldSender: true,
	}); err != nil {
		t.Fatal(err)
	}

	daemon := &Daemon{store: store}
	reconciled, changed, err := daemon.reconcileRun(context.Background(), project, WorkflowFile{Data: defaultWorkflow()}, run)
	if err != nil {
		t.Fatal(err)
	}
	if !changed || reconciled.AttemptOutcome != string(AttemptOutcomeWaitingForHuman) || reconciled.LeaseState != string(LeaseStateReleased) {
		t.Fatalf("expected released waiting_for_human run, got changed=%t run=%#v", changed, reconciled)
	}
	if err := store.UpsertRun(reconciled); err != nil {
		t.Fatal(err)
	}

	session, err := store.FindSessionByRef("app", "thread-1")
	if err != nil || session == nil {
		t.Fatalf("stored session missing: session=%#v err=%v", session, err)
	}
	if !session.Resumable {
		t.Fatalf("waiting-for-human session must stay resumable, got %#v", session)
	}
	if session.State != "open" {
		t.Fatalf("waiting-for-human session must stay open, got %#v", session)
	}

	task := Note{Data: map[string]any{"id": "APP-T-0001", "status": "ready"}}
	wave := Note{Data: map[string]any{"id": "W-1", "authorization": "armed", "authorization_fingerprint": "fp", "authorized_at": now.Format(time.RFC3339)}}

	resume, err := daemon.resolveResumeSession(project, task, reconciled)
	if err != nil || resume.SessionRef != "thread-1" {
		t.Fatalf("dispatch resume resolution: resume=%#v err=%v", resume, err)
	}

	resolved, preflightErr, reason := nativeContinuationPreflight(store, project, wave, reconciled)
	if preflightErr != nil || reason != "" || resolved == nil {
		t.Fatalf("continue preflight: session=%#v reason=%q err=%v", resolved, reason, preflightErr)
	}
	if resolved.SessionRef != "thread-1" {
		t.Fatalf("continue resolved session %q, want thread-1", resolved.SessionRef)
	}

	result, err := continueRuntimeRun(store, project, task, wave, reconciled, "human:test", "Use A", now)
	if err != nil {
		t.Fatalf("runs continue: %v", err)
	}
	if !result.Continuation.OK || !result.Continuation.Admitted || result.Continuation.Refused {
		t.Fatalf("runs continue refused: %#v", result.Continuation)
	}
	continued, err := store.FindRunScoped("app", "APP-T-0001")
	if err != nil || continued == nil {
		t.Fatalf("reload run: %v", err)
	}
	if continued.LeaseState != string(LeaseStateRetryQueued) || continued.SessionRef != "thread-1" {
		t.Fatalf("expected retry_queued run on session thread-1, got %#v", continued)
	}
}
