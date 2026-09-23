package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRunsSayMessageInputAndKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "message.txt")
	if err := os.WriteFile(path, []byte("exact\nmessage\n"), 0600); err != nil {
		t.Fatal(err)
	}
	message, err := runOperatorMessage(Args{"message-file": path}, true)
	if err != nil || message != "exact\nmessage\n" {
		t.Fatalf("message=%q err=%v", message, err)
	}
	if _, err := runOperatorMessage(Args{"message": "x", "message-file": path}, true); err == nil {
		t.Fatal("accepted two message sources")
	}
	if _, err := runOperatorMessage(Args{}, true); err == nil {
		t.Fatal("accepted missing Say message")
	}
	if _, err := runOperatorMessage(Args{"message": strings.Repeat("x", workerDeliveryBodyLimit+1)}, true); err == nil {
		t.Fatal("accepted oversized message")
	}
	identity := WorkerAttemptIdentity{AttemptID: "attempt", AttemptGeneration: 3}
	key := runSayIdempotencyKey("human:one", message, identity)
	if !strings.HasPrefix(key, "say:") || key != runSayIdempotencyKey("human:one", message, identity) {
		t.Fatalf("unstable idempotency key %q", key)
	}
	if key == runSayIdempotencyKey("human:two", message, identity) || key == runSayIdempotencyKey("human:one", "different", identity) {
		t.Fatal("distinct requests shared an idempotency key")
	}
}

func TestRunsContinueRefusesLiveAndCompletedAttempts(t *testing.T) {
	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	run := RunStatus{ProjectID: "p", RecordID: "P-T-0001", ItemID: "P-T-0001", Runner: string(RunnerCodexExec),
		Lane: runLaneExecute, LeaseState: string(LeaseStateRunning), ActiveAttemptID: "a", LeaseGeneration: 1, WorkRevision: 1}
	if err := store.UpsertRun(run); err != nil {
		t.Fatal(err)
	}
	_, err = continueRuntimeRun(store, RegisteredProject{}, Note{}, Note{}, run, "human:test", "message", time.Now().UTC())
	if err == nil || !strings.Contains(err.Error(), "live owner") {
		t.Fatalf("live Continue was not refused: %v", err)
	}
	run.LeaseState = string(LeaseStateReleased)
	run.AttemptOutcome = string(AttemptOutcomeSucceeded)
	run.Terminal = true
	if err := store.UpsertRun(run); err != nil {
		t.Fatal(err)
	}
	_, err = continueRuntimeRun(store, RegisteredProject{}, Note{}, Note{}, run, "human:test", "message", time.Now().UTC())
	if err == nil || !strings.Contains(err.Error(), "completed attempt") {
		t.Fatalf("completed Continue was not refused: %v", err)
	}
}

func TestRunsSayPendingDeliveryOrderAndReceipt(t *testing.T) {
	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	identity := WorkerAttemptIdentity{
		ProjectID: "p", TaskID: "P-T-0001", AttemptID: "parent", AttemptGeneration: 2,
		WorkRevision: 1, Provider: "codex", NativeSessionID: "thread-1",
	}
	if err := store.SaveAttempt(RunAttempt{AttemptID: identity.AttemptID, ProjectID: identity.ProjectID, RecordID: identity.TaskID,
		ItemID: identity.TaskID, Runner: string(RunnerCodexExec), Lane: runLaneExecute, WorkRevision: identity.WorkRevision,
		SessionRef: "thread-1"}); err != nil {
		t.Fatal(err)
	}
	first, _, err := store.PutWorkerDelivery(WorkerDelivery{Identity: identity, Kind: "instruction", Body: "First exact\nline", IdempotencyKey: "say:one", StoredAt: "2026-09-23T00:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := store.PutWorkerDelivery(WorkerDelivery{Identity: identity, Kind: "instruction", Body: "Second exact line", IdempotencyKey: "say:two", StoredAt: "2026-09-23T00:00:01Z"})
	if err != nil {
		t.Fatal(err)
	}
	run := RunStatus{ProjectID: identity.ProjectID, RecordID: identity.TaskID, ItemID: identity.TaskID, WorkRevision: identity.WorkRevision}
	pending, err := pendingRunContinuationDeliveries(store, run, identity.AttemptID, "thread-1")
	if err != nil || len(pending) != 2 || pending[0].DeliveryID != first.DeliveryID || pending[1].DeliveryID != second.DeliveryID {
		t.Fatalf("pending=%#v err=%v", pending, err)
	}
	if err := acceptRunContinuationDeliveries(store, pending, "child"); err != nil {
		t.Fatal(err)
	}
	for _, delivery := range pending {
		accepted, found, err := store.WorkerDelivery(delivery.DeliveryID)
		if err != nil || !found || accepted.State != "accepted" || accepted.ProviderReceipt["child_attempt_id"] != "child" {
			t.Fatalf("accepted=%#v found=%v err=%v", accepted, found, err)
		}
	}
	pending, err = pendingRunContinuationDeliveries(store, run, identity.AttemptID, "thread-1")
	if err != nil || len(pending) != 0 {
		t.Fatalf("accepted deliveries reappeared: %#v %v", pending, err)
	}
	late := identity
	late.AttemptID = "earlier-parent"
	if err := store.SaveAttempt(RunAttempt{AttemptID: late.AttemptID, ProjectID: late.ProjectID, RecordID: late.TaskID,
		ItemID: late.TaskID, Runner: string(RunnerCodexExec), Lane: runLaneExecute, WorkRevision: late.WorkRevision,
		SessionRef: "thread-1"}); err != nil {
		t.Fatal(err)
	}
	lateMessage, _, err := store.PutWorkerDelivery(WorkerDelivery{Identity: late, Kind: "instruction", Body: "Arrived after claim", IdempotencyKey: "say:late"})
	if err != nil {
		t.Fatal(err)
	}
	other := identity
	other.AttemptID = "other-thread-parent"
	other.NativeSessionID = "thread-2"
	if err := store.SaveAttempt(RunAttempt{AttemptID: other.AttemptID, ProjectID: other.ProjectID, RecordID: other.TaskID,
		ItemID: other.TaskID, Runner: string(RunnerCodexExec), Lane: runLaneExecute, WorkRevision: other.WorkRevision,
		SessionRef: "thread-2"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.PutWorkerDelivery(WorkerDelivery{Identity: other, Kind: "instruction", Body: "Different native thread", IdempotencyKey: "say:other"}); err != nil {
		t.Fatal(err)
	}
	pending, err = pendingRunContinuationDeliveries(store, run, "child", "thread-1")
	if err != nil || len(pending) != 1 || pending[0].DeliveryID != lateMessage.DeliveryID {
		t.Fatalf("late message lost before next continuation: %#v %v", pending, err)
	}
}

func TestRunsSayResumePromptContainsExactOperatorMessages(t *testing.T) {
	vault := v7DispatchTestVault(t)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Say prompt", "domains": "project", "v7": "true"}, newV7Task)
	task := mustV7Task(t, vault, "APP-T-0001")
	task.Data = cloneMap(task.Data)
	task.Data["work_revision"] = 1
	workspace := t.TempDir()
	project := RegisteredProject{ProjectID: "project-1", ProjectKey: "project-1", RepoRoot: filepath.Dir(vault), VaultRoot: vault}
	workflow := WorkflowFile{Path: filepath.Join(vault, "WORKFLOW.md"), Body: "Continue {{ note.id }} in {{ workspace.path }}."}
	run := RunStatus{
		ProjectID: project.ProjectID, RecordID: "APP-T-0001", ItemID: "APP-T-0001", Runner: string(RunnerCodexExec),
		RunnerProfile: "execute", RunnerHarness: string(RunnerCodexExec), RunnerModel: "gpt-test", RunnerEffort: "medium",
		WorkerPolicyFP: "sha256:" + strings.Repeat("a", 64), ExecutePolicyFP: "sha256:" + strings.Repeat("b", 64),
		Lane: runLaneExecute, WorkRevision: 1, WorkspacePath: workspace, ActiveAttemptID: "child", AttemptCount: 2,
	}
	previous := run
	previous.ActiveAttemptID = "parent"
	previous.SessionRef = "native-thread"
	previous.LastError = "native continuation requested\nPrevious failure:\nCode: usage_limit\nGuidance: Wait for reset"
	first := WorkerDelivery{Body: "First exact\nline"}
	second := WorkerDelivery{Body: "Second exact line"}
	prompt, err := renderAttemptPromptForResume(project, workflow, task, workspace, 2, "child", runLaneExecute, run, previous, nil,
		resolvedResumeSession{SessionRef: "native-thread", ParentAttemptID: "parent", Reason: "compatible session"}, first, second)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(prompt, "### Operator message") != 2 || !strings.Contains(prompt, "### Operator message\n\nFirst exact\nline") ||
		!strings.Contains(prompt, "### Operator message\n\nSecond exact line") || !strings.Contains(prompt, "Code: usage_limit") {
		t.Fatalf("resume prompt lost operator messages or typed failure:\n%s", prompt)
	}
	if strings.Index(prompt, first.Body) > strings.Index(prompt, second.Body) {
		t.Fatalf("resume prompt changed message order:\n%s", prompt)
	}
}
