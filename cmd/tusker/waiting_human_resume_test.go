package main

import (
	"context"
	"os"
	"path/filepath"
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

	project, _ := resumeFixtureProject(t, store)
	workspace := t.TempDir()
	files := t.TempDir()

	now := time.Now().UTC()
	statusPath := filepath.Join(files, "status.json")
	if err := os.WriteFile(statusPath, []byte(`{"exit_code":0,"completed_at":"`+now.Format(time.RFC3339)+`","outcome":"succeeded"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	promptPath := filepath.Join(files, "prompt.md")
	run := RunStatus{
		ProjectID: "app", RecordID: "APP-T-0001", ItemID: "APP-T-0001",
		Runner: string(RunnerCodexExec), RunnerProfile: "codex_exec-gpt-6-luna", RunnerHarness: "codex_exec",
		RunnerModel: "gpt-6-luna", RunnerEffort: "xhigh", Lane: runLaneExecute,
		LeaseState: string(LeaseStateRunning), LeaseOwner: "attempt-1", LeaseGeneration: 1,
		AttemptOutcome: string(AttemptOutcomeNone), ActiveAttemptID: "attempt-1",
		WorkRevision: 0, AttemptCount: 1, SessionRef: "thread-1",
		WorkspacePath: workspace, PromptPath: promptPath, StatusPath: statusPath,
		StartedAt: now.Format(time.RFC3339), UpdatedAt: now.Format(time.RFC3339),
	}
	loaded, err := loadProjectContents(store, project, false)
	if err != nil {
		t.Fatal(err)
	}
	note, err := resolveV7Note(project.VaultRoot, "APP-T-0001", "task")
	if err != nil {
		t.Fatal(err)
	}
	prompt, err := renderAttemptPrompt(loaded.Project, loaded.Workflow, note, workspace, 1, "attempt-1", runLaneExecute, run, RunStatus{}, store)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeText(promptPath, prompt); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertRun(run); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.PutAgentMessage(AgentMessage{
		ProjectID: "app", IdempotencyKey: "q", Sender: "task:APP-T-0001",
		Recipient: AgentAddress{Kind: "operator", ID: "operator"}, OriginTaskID: "APP-T-0001",
		WorkRevision: 0, RouteGeneration: 1, Kind: "question", Body: "Pick A or B",
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

// resumeFixtureProject registers a real vault (WORKFLOW.md plus a fresh V7
// task without work_revision or source_sha) so the continue preflight can
// recompute the resume context through the same loaders dispatch uses.
func resumeFixtureProject(t *testing.T, store *RuntimeStore) (RegisteredProject, string) {
	t.Helper()
	vault := t.TempDir()
	if err := writeText(filepath.Join(vault, "WORKFLOW.md"), defaultWorkflowMarkdown()); err != nil {
		t.Fatal(err)
	}
	taskPath := filepath.Join(vault, "work", "tasks", "APP-T-0001.md")
	if err := writeText(taskPath, "---\nschema: tusker.task/v7\nkind: task\nid: APP-T-0001\ntitle: First\nstatus: ready\nstate_rev: sha256:x\n---\nBody\n"); err != nil {
		t.Fatal(err)
	}
	project := RegisteredProject{ProjectID: "app", ProjectKey: "app", Name: "app", RepoRoot: t.TempDir(), VaultRoot: vault, Enabled: true, Health: projectHealthHealthy}
	if err := store.UpsertProject(project); err != nil {
		t.Fatal(err)
	}
	return project, taskPath
}

// After `runs interrupt`, the run row keeps the native session but its
// prompt_path is cleared. `runs continue` must recompute the current resume
// context and compare it with the stopped attempt's retained prompt. The run
// is shaped like a real V7 dispatch: work revision 0 and empty worker/execute
// policy fingerprints (those are only set under completion authority).
func TestInterruptedRunContinuePreflightRecomputesResumeContext(t *testing.T) {
	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	project, taskPath := resumeFixtureProject(t, store)
	workspace, files := t.TempDir(), t.TempDir()
	run := RunStatus{
		ProjectID: "app", RecordID: "APP-T-0001", ItemID: "APP-T-0001", Runner: string(RunnerCodexExec), RunnerProfile: "codex_exec-gpt-6-luna",
		RunnerHarness: "codex_exec", RunnerModel: "gpt-6-luna", RunnerEffort: "xhigh", Lane: runLaneExecute,
		LeaseState: string(LeaseStateInterrupted), LeaseGeneration: 7, AttemptOutcome: string(AttemptOutcomeFailed),
		WorkRevision: 0, AttemptCount: 1, SessionRef: "thread-1", WorkspacePath: workspace,
	}
	loaded, err := loadProjectContents(store, project, false)
	if err != nil {
		t.Fatal(err)
	}
	note, err := resolveV7Note(project.VaultRoot, "APP-T-0001", "task")
	if err != nil {
		t.Fatal(err)
	}
	prompt, err := renderAttemptPrompt(loaded.Project, loaded.Workflow, note, workspace, 1, "attempt-1", runLaneExecute, run, RunStatus{}, store)
	if err != nil {
		t.Fatal(err)
	}
	if resumeContextFingerprintFromPrompt(prompt) == "" {
		t.Fatalf("V7 execute prompt lacks a resume context marker:\n%s", prompt)
	}
	promptPath := filepath.Join(files, "rev-00-execute-attempt-0001.prompt.md")
	if err := writeText(promptPath, prompt); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertRun(run); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveAttempt(RunAttempt{AttemptID: "attempt-1", ProjectID: "app", RecordID: "APP-T-0001", ItemID: "APP-T-0001",
		Runner: string(RunnerCodexExec), Lane: runLaneExecute, SessionRef: "thread-1", PromptPath: promptPath, Outcome: string(AttemptOutcomeFailed)}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveSession(RunnerSession{ProjectID: "app", RecordID: "APP-T-0001", Runner: string(RunnerCodexExec), SessionRef: "thread-1",
		CurrentItemID: "APP-T-0001", LastAttemptID: "attempt-1", WorkspacePath: workspace, State: "open", Resumable: true}); err != nil {
		t.Fatal(err)
	}
	wave := Note{Data: map[string]any{"id": "W-1", "authorization": "armed", "authorization_fingerprint": "fp", "authorized_at": time.Now().UTC().Format(time.RFC3339)}}
	session, preflightErr, reason := nativeContinuationPreflight(store, project, wave, run)
	if preflightErr != nil || reason != "" || session == nil || session.SessionRef != "thread-1" {
		t.Fatalf("continue preflight: session=%#v reason=%q err=%v", session, reason, preflightErr)
	}
	// A task change after the session was established must be refused here,
	// not accepted and then rejected by dispatch.
	if err := writeText(taskPath, "---\nschema: tusker.task/v7\nkind: task\nid: APP-T-0001\ntitle: Changed\nstatus: ready\nstate_rev: sha256:y\n---\nBody\n"); err != nil {
		t.Fatal(err)
	}
	if _, _, reason := nativeContinuationPreflight(store, project, wave, run); reason != "stored native session prompt context fingerprint changed" {
		t.Fatalf("changed task must refuse continuation, reason=%q", reason)
	}
}
