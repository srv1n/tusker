package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCodexLiveRunnerHandlesAppServerFlow(t *testing.T) {
	tempRoot := t.TempDir()
	t.Setenv("TUSKER_STATE_ROOT", filepath.Join(tempRoot, "state"))
	repoRoot := filepath.Join(tempRoot, "repo")
	workspaceRoot := filepath.Join(tempRoot, "workspace")
	if err := ensureDir(repoRoot); err != nil {
		t.Fatal(err)
	}
	if err := ensureDir(workspaceRoot); err != nil {
		t.Fatal(err)
	}
	rawLogPath := filepath.Join(tempRoot, "codex.log")
	statusPath := filepath.Join(tempRoot, "codex.status.json")
	eventSinkPath := filepath.Join(tempRoot, "events.jsonl")
	promptPath := filepath.Join(tempRoot, "prompt.txt")
	scriptPath := filepath.Join(tempRoot, "fake-codex.py")
	if err := writeText(promptPath, "Ship it.\n"); err != nil {
		t.Fatal(err)
	}
	script := `#!/usr/bin/env python3
import json,os,sys
for line in sys.stdin:
    msg=json.loads(line)
    if msg.get("method")=="initialize":
        print(json.dumps({"jsonrpc":"2.0","id":msg["id"],"result":{"serverInfo":{"name":"fake-codex"}}}), flush=True)
    elif msg.get("method")=="thread/start":
        assert os.path.realpath(os.getcwd())==os.path.realpath(os.environ["TUSKER_WORKSPACE"]), (os.getcwd(), os.environ["TUSKER_WORKSPACE"])
        assert os.environ["TUSKER_REPO_ROOT"].endswith("/repo"), os.environ.get("TUSKER_REPO_ROOT")
        assert os.environ["TUSKER_EXTENSIONS_ENABLED"]=="false", os.environ.get("TUSKER_EXTENSIONS_ENABLED")
        assert os.environ["TUSKER_EXTENSION_ALLOW_TUSKER_READ_TOOLS"]=="false", os.environ.get("TUSKER_EXTENSION_ALLOW_TUSKER_READ_TOOLS")
        assert msg["params"]["cwd"]==os.environ["TUSKER_WORKSPACE"], msg["params"]
        assert msg["params"]["approvalPolicy"]=="on-failure", msg["params"]
        assert msg["params"]["sandbox"]=="read-only", msg["params"]
        print(json.dumps({"jsonrpc":"2.0","id":msg["id"],"result":{"thread":{"id":"thread-123"}}}), flush=True)
    elif msg.get("method")=="turn/start":
        assert msg["params"]["cwd"]==os.environ["TUSKER_WORKSPACE"], msg["params"]
        assert msg["params"]["approvalPolicy"]=="on-failure", msg["params"]
        assert msg["params"]["sandboxPolicy"]=="workspace-write", msg["params"]
        assert msg["params"]["turnTimeoutMs"]==12345, msg["params"]
        assert msg["params"]["readTimeoutMs"]==5000, msg["params"]
        assert msg["params"]["stallTimeoutMs"]==6789, msg["params"]
        assert msg["params"]["maxTurns"]==2, msg["params"]
        print(json.dumps({"jsonrpc":"2.0","id":msg["id"],"result":{"turn":{"id":"turn-123"}}}), flush=True)
        print(json.dumps({"jsonrpc":"2.0","method":"item/commandExecution/requestApproval","id":"approve-1","params":{"threadId":"thread-123","turnId":"turn-123","itemId":"item-1"}}), flush=True)
    elif msg.get("id")=="approve-1":
        assert msg["result"]["decision"]=="accept"
        print(json.dumps({"jsonrpc":"2.0","method":"turn/started","params":{"threadId":"thread-123","turn":{"id":"turn-123"}}}), flush=True)
        print(json.dumps({"jsonrpc":"2.0","method":"turn/usage","params":{"threadId":"thread-123","turnId":"turn-123","usage":{"input_tokens":11,"output_tokens":7}}}), flush=True)
        print(json.dumps({"jsonrpc":"2.0","method":"item/agentMessage/delta","params":{"threadId":"thread-123","item":{"id":"msg-codex-1"}}}), flush=True)
        print(json.dumps({"jsonrpc":"2.0","method":"turn/completed","params":{"threadId":"thread-123","turn":{"id":"turn-123","status":"completed"}}}), flush=True)
        break
`
	if err := writeText(scriptPath, script); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(scriptPath, 0o755); err != nil {
		t.Fatal(err)
	}

	result, err := startLiveCodex(context.Background(), StartRequest{
		ProjectID:     "project-1",
		RecordID:      "record-1",
		ItemID:        "ITEM-1",
		AttemptID:     "attempt-1",
		WorkRevision:  0,
		WorkspacePath: workspaceRoot,
		RepoRoot:      repoRoot,
		PromptPath:    promptPath,
		EventSinkPath: eventSinkPath,
		RawLogPath:    rawLogPath,
		StatusPath:    statusPath,
		Command:       scriptPath,
		VaultPath:     tempRoot,
		CodexPolicy: CodexPolicy{
			ApprovalPolicy:    "on-failure",
			ThreadSandbox:     "read-only",
			TurnSandboxPolicy: "workspace-write",
			TurnTimeoutMS:     12345,
			ReadTimeoutMS:     5000,
			StallTimeoutMS:    6789,
			MaxTurns:          2,
		},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "thread-123", result.SessionRef, "codex live session ref")
	waitForStatusFile(t, statusPath)
	status, err := readRunnerProcessStatus(statusPath)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, 0, status.ExitCode, "codex live exit code")
	waitForFileText(t, eventSinkPath, "\"kind\":\"turn_completed\"")
	eventsText, err := readText(eventSinkPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"\"kind\":\"turn_started\"", "\"kind\":\"turn_usage_updated\"", "\"kind\":\"turn_completed\""} {
		if !strings.Contains(eventsText, expected) {
			t.Fatalf("expected normalized codex events to include %s, got:\n%s", expected, eventsText)
		}
	}
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	turns := waitForStoredTurnStatus(t, store, "project-1", "record-1", "completed")
	assertEqual(t, 1, len(turns), "stored codex turn count")
	assertEqual(t, "completed", turns[0].Status, "stored codex turn status")
	assertEqual(t, 11, turns[0].InputTokens, "stored codex input tokens")
	assertEqual(t, 7, turns[0].OutputTokens, "stored codex output tokens")
	assertEqual(t, 18, turns[0].TotalTokens, "stored codex total tokens")
}

func TestCodexLiveRunnerContinuesSameThreadWhileNoteRemainsActive(t *testing.T) {
	tempRoot := t.TempDir()
	t.Setenv("TUSKER_STATE_ROOT", filepath.Join(tempRoot, "state"))
	workspaceRoot := filepath.Join(tempRoot, "workspace")
	if err := ensureDir(workspaceRoot); err != nil {
		t.Fatal(err)
	}
	rawLogPath := filepath.Join(tempRoot, "codex.log")
	statusPath := filepath.Join(tempRoot, "codex.status.json")
	eventSinkPath := filepath.Join(tempRoot, "events.jsonl")
	promptPath := filepath.Join(tempRoot, "prompt.txt")
	notePath := filepath.Join(tempRoot, "note.md")
	scriptPath := filepath.Join(tempRoot, "fake-codex.py")
	if err := writeText(promptPath, "First rendered prompt.\n"); err != nil {
		t.Fatal(err)
	}
	if err := writeText(notePath, "---\nid: ITEM-1\nrecord_id: record-1\ntype: story\ntitle: Continue me\nstatus: active\n---\n\n"); err != nil {
		t.Fatal(err)
	}
	script := `#!/usr/bin/env python3
import json,sys
turn_count=0
for line in sys.stdin:
    msg=json.loads(line)
    if msg.get("method")=="initialize":
        print(json.dumps({"jsonrpc":"2.0","id":msg["id"],"result":{"serverInfo":{"name":"fake-codex"}}}), flush=True)
    elif msg.get("method")=="thread/start":
        print(json.dumps({"jsonrpc":"2.0","id":msg["id"],"result":{"thread":{"id":"thread-continue"}}}), flush=True)
    elif msg.get("method")=="turn/start":
        turn_count += 1
        text=msg["params"]["input"][0]["text"]
        if turn_count == 1:
            assert "First rendered prompt." in text, text
        if turn_count == 2:
            assert "Continue on the same Codex thread" in text, text
            assert "First rendered prompt." not in text, text
        turn_id=f"turn-{turn_count}"
        print(json.dumps({"jsonrpc":"2.0","id":msg["id"],"result":{"turn":{"id":turn_id}}}), flush=True)
        print(json.dumps({"jsonrpc":"2.0","method":"turn/started","params":{"threadId":"thread-continue","turn":{"id":turn_id}}}), flush=True)
        print(json.dumps({"jsonrpc":"2.0","method":"turn/completed","params":{"threadId":"thread-continue","turn":{"id":turn_id,"status":"completed"}}}), flush=True)
        if turn_count == 2:
            break
`
	if err := writeText(scriptPath, script); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(scriptPath, 0o755); err != nil {
		t.Fatal(err)
	}

	result, err := startLiveCodex(context.Background(), StartRequest{
		ProjectID:     "project-1",
		RecordID:      "record-1",
		ItemID:        "ITEM-1",
		AttemptID:     "attempt-1",
		WorkRevision:  0,
		ActiveStates:  []string{"active"},
		WorkspacePath: workspaceRoot,
		PromptPath:    promptPath,
		EventSinkPath: eventSinkPath,
		RawLogPath:    rawLogPath,
		StatusPath:    statusPath,
		Command:       scriptPath,
		NotePath:      notePath,
		VaultPath:     tempRoot,
		CodexPolicy:   CodexPolicy{MaxTurns: 2, TurnTimeoutMS: 5000, ReadTimeoutMS: 5000, StallTimeoutMS: 5000},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "thread-continue", result.SessionRef, "codex live session ref")
	waitForStatusFile(t, statusPath)
	status, err := readRunnerProcessStatus(statusPath)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, 0, status.ExitCode, "codex continuation exit code")
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	turns := waitForStoredTurnCount(t, store, "project-1", "record-1", 2)
	assertEqual(t, "turn-1", turns[0].TurnID, "first turn id")
	assertEqual(t, "turn-2", turns[1].TurnID, "second turn id")
	assertEqual(t, 0, turns[0].TurnIndex, "first turn index")
	assertEqual(t, 1, turns[1].TurnIndex, "second turn index")
	logText, err := readText(rawLogPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(logText, "max turns reached") {
		t.Fatalf("expected max-turn exhaustion to be surfaced in raw log, got:\n%s", logText)
	}
}

func TestClaudeLiveRunnerSupportsInterrupt(t *testing.T) {
	tempRoot := t.TempDir()
	workspaceRoot := filepath.Join(tempRoot, "workspace")
	repoRoot := filepath.Join(tempRoot, "repo")
	if err := ensureDir(workspaceRoot); err != nil {
		t.Fatal(err)
	}
	if err := ensureDir(repoRoot); err != nil {
		t.Fatal(err)
	}
	rawLogPath := filepath.Join(tempRoot, "claude.log")
	statusPath := filepath.Join(tempRoot, "claude.status.json")
	promptPath := filepath.Join(tempRoot, "prompt.txt")
	scriptPath := filepath.Join(tempRoot, "fake-claude.py")
	if err := writeText(promptPath, "Fix the bug.\n"); err != nil {
		t.Fatal(err)
	}
	script := `#!/usr/bin/env python3
import json,os,sys,time,threading
assert os.path.realpath(os.getcwd())==os.path.realpath(os.environ["TUSKER_WORKSPACE"]), (os.getcwd(), os.environ["TUSKER_WORKSPACE"])
assert os.environ["TUSKER_REPO_ROOT"].endswith("/repo"), os.environ.get("TUSKER_REPO_ROOT")
running=True
started=False
def reader():
    global running,started
    for line in sys.stdin:
        msg=json.loads(line)
        if msg.get("type")=="control_request":
            subtype=msg["request"]["subtype"]
            if subtype=="initialize" or subtype=="set_permission_mode":
                continue
            if subtype=="interrupt":
                print(json.dumps({"type":"result","subtype":"interrupted","is_error":True,"session_id":"claude-session-1"}), flush=True)
                running=False
                return
        elif msg.get("type")=="user":
            started=True
            print(json.dumps({"type":"assistant","session_id":"claude-session-1","uuid":"claude-msg-1","message":{"id":"claude-msg-1","role":"assistant","content":[{"type":"text","text":"working"}]}}), flush=True)
threading.Thread(target=reader, daemon=True).start()
while running:
    time.sleep(0.05)
`
	if err := writeText(scriptPath, script); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(scriptPath, 0o755); err != nil {
		t.Fatal(err)
	}

	result, err := startLiveClaude(context.Background(), StartRequest{
		ProjectID:     "project-1",
		RecordID:      "record-1",
		ItemID:        "ITEM-1",
		AttemptID:     "attempt-1",
		WorkRevision:  0,
		WorkspacePath: workspaceRoot,
		RepoRoot:      repoRoot,
		PromptPath:    promptPath,
		EventSinkPath: filepath.Join(tempRoot, "events.jsonl"),
		RawLogPath:    rawLogPath,
		StatusPath:    statusPath,
		Command:       scriptPath,
		VaultPath:     tempRoot,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "claude-session-1", result.SessionRef, "claude live session ref")
	handle := liveRegistry.Find("attempt-1")
	if handle == nil {
		t.Fatal("expected live claude handle to be registered")
	}
	if err := handle.Interrupt(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitForStatusFile(t, statusPath)
	status, err := readRunnerProcessStatus(statusPath)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, 130, status.ExitCode, "claude interrupt exit code")
	logText, err := readText(rawLogPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(logText, "claude-msg-1") {
		t.Fatalf("expected claude raw log to include message id, got:\n%s", logText)
	}
}

func TestDetachedRunnerUsesWorkspaceCWDAndRepoRootEnv(t *testing.T) {
	tempRoot := t.TempDir()
	workspaceRoot := filepath.Join(tempRoot, "workspace")
	repoRoot := filepath.Join(tempRoot, "repo")
	if err := ensureDir(workspaceRoot); err != nil {
		t.Fatal(err)
	}
	if err := ensureDir(repoRoot); err != nil {
		t.Fatal(err)
	}
	promptPath := filepath.Join(tempRoot, "prompt.txt")
	rawLogPath := filepath.Join(tempRoot, "runner.log")
	statusPath := filepath.Join(tempRoot, "runner.status.json")
	if err := writeText(promptPath, "Run.\n"); err != nil {
		t.Fatal(err)
	}
	command := `python3 -c 'import os,pathlib; pathlib.Path(os.environ["TUSKER_WORKSPACE"], "cwd.txt").write_text(os.getcwd()+"\n"+os.environ.get("TUSKER_REPO_ROOT",""))'`
	result, err := executeRunnerCommand(context.Background(), RunnerCodex, runnerExecRequest{
		ProjectID: "project-1", RecordID: "record-1", ItemID: "ITEM-1", AttemptID: "attempt-1",
		WorkspacePath: workspaceRoot, RepoRoot: repoRoot, PromptPath: promptPath,
		EventSinkPath: filepath.Join(tempRoot, "events.jsonl"), RawLogPath: rawLogPath, StatusPath: statusPath,
		Command: command, VaultPath: tempRoot,
	}, RunnerCapabilities{})
	if err != nil {
		t.Fatal(err)
	}
	if result.PID <= 0 {
		t.Fatalf("expected spawned pid, got %d", result.PID)
	}
	waitForStatusFile(t, statusPath)
	status, err := readRunnerProcessStatus(statusPath)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, 0, status.ExitCode, "detached runner exit code")
	cwdText, err := readText(filepath.Join(workspaceRoot, "cwd.txt"))
	if err != nil {
		t.Fatal(err)
	}
	expectedWorkspace, err := filepath.EvalSymlinks(workspaceRoot)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, expectedWorkspace+"\n"+repoRoot, strings.TrimSpace(cwdText), "detached runner cwd/env")
	if fileExists(filepath.Join(repoRoot, "cwd.txt")) {
		t.Fatal("detached runner wrote cwd marker in repo root")
	}
}

func TestRunnerRejectsMissingWorkspacePath(t *testing.T) {
	tempRoot := t.TempDir()
	promptPath := filepath.Join(tempRoot, "prompt.txt")
	if err := writeText(promptPath, "Run.\n"); err != nil {
		t.Fatal(err)
	}
	_, err := executeRunnerCommand(context.Background(), RunnerCodex, runnerExecRequest{
		PromptPath: promptPath, RawLogPath: filepath.Join(tempRoot, "runner.log"),
		StatusPath: filepath.Join(tempRoot, "runner.status.json"), Command: "cat >/dev/null",
	}, RunnerCapabilities{})
	if err == nil || !strings.Contains(err.Error(), "requires workspace_path") {
		t.Fatalf("expected missing workspace_path error, got %v", err)
	}
}

func waitForStatusFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if fileExists(path) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for status file: %s", path)
}

func waitForFileText(t *testing.T, path, needle string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if text, err := readText(path); err == nil && strings.Contains(text, needle) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %q in %s", needle, path)
}

func waitForStoredTurnStatus(t *testing.T, store *RuntimeStore, projectID, recordID, status string) []RunTurn {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	var last []RunTurn
	for time.Now().Before(deadline) {
		turns, err := store.ListTurnsForRun(projectID, recordID)
		if err != nil {
			t.Fatal(err)
		}
		last = turns
		for _, turn := range turns {
			if turn.Status == status {
				return turns
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for stored turn status %q; latest=%v", status, last)
	return nil
}

func waitForStoredTurnCount(t *testing.T, store *RuntimeStore, projectID, recordID string, count int) []RunTurn {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	var last []RunTurn
	for time.Now().Before(deadline) {
		turns, err := store.ListTurnsForRun(projectID, recordID)
		if err != nil {
			t.Fatal(err)
		}
		last = turns
		if len(turns) >= count {
			return turns
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d stored turns; latest=%v", count, last)
	return nil
}
