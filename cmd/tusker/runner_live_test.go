package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

const runnerLiveTestWait = 15 * time.Second

func TestMaxTurnsReachesRunnerSession(t *testing.T) {
	tempRoot := t.TempDir()
	t.Setenv("TUSKER_STATE_ROOT", filepath.Join(tempRoot, "state"))
	workspaceRoot := filepath.Join(tempRoot, "workspace")
	if err := ensureDir(workspaceRoot); err != nil {
		t.Fatal(err)
	}
	if err := writeText(workflowPath(tempRoot), "---\ncodex:\n  max_turns: 30\n  turn_timeout_ms: 5000\n  read_timeout_ms: 5000\n  stall_timeout_ms: 5000\n---\n\n## Routing\n\nTest.\n\n## Prompt\n\nTest prompt.\n\n## Retry policy\n\nRetry transient failures.\n\n## Human override policy\n\nHumans may override.\n"); err != nil {
		t.Fatal(err)
	}
	wfFile, err := loadWorkflow(tempRoot)
	if err != nil {
		t.Fatal(err)
	}
	policy := codexPolicyFromWorkflow(wfFile.Data)
	assertEqual(t, 30, policy.MaxTurns, "workflow max_turns")

	rawLogPath := filepath.Join(tempRoot, "codex.log")
	statusPath := filepath.Join(tempRoot, "codex.status.json")
	eventSinkPath := filepath.Join(tempRoot, "events.jsonl")
	promptPath := filepath.Join(tempRoot, "prompt.txt")
	scriptPath := filepath.Join(tempRoot, "fake-codex.py")
	if err := writeText(promptPath, "Check policy.\n"); err != nil {
		t.Fatal(err)
	}
	script := `#!/usr/bin/env python3
import json,os,sys
for line in sys.stdin:
    msg=json.loads(line)
    if msg.get("method")=="initialize":
        assert os.environ["TUSKER_CODEX_MAX_TURNS"]=="30", os.environ.get("TUSKER_CODEX_MAX_TURNS")
        print(json.dumps({"jsonrpc":"2.0","id":msg["id"],"result":{"serverInfo":{"name":"fake-codex"}}}), flush=True)
    elif msg.get("method")=="thread/start":
        assert "maxTurns" not in msg["params"], msg["params"]
        print(json.dumps({"jsonrpc":"2.0","id":msg["id"],"result":{"thread":{"id":"thread-max-turns"}}}), flush=True)
    elif msg.get("method")=="turn/start":
        print(json.dumps({"jsonrpc":"2.0","id":msg["id"],"result":{"turn":{"id":"turn-max-turns","status":"inProgress","items":[]}}}), flush=True)
        print(json.dumps({"jsonrpc":"2.0","method":"turn/completed","params":{"threadId":"thread-max-turns","turn":{"id":"turn-max-turns","status":"completed","items":[]}}}), flush=True)
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
		AttemptID:     "attempt-max-turns",
		WorkRevision:  0,
		WorkspacePath: workspaceRoot,
		PromptPath:    promptPath,
		EventSinkPath: eventSinkPath,
		RawLogPath:    rawLogPath,
		StatusPath:    statusPath,
		Command:       scriptPath,
		VaultPath:     tempRoot,
		CodexPolicy:   policy,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "thread-max-turns", result.SessionRef, "codex live session ref")
	waitForStatusFile(t, statusPath)
	status, err := readRunnerProcessStatus(statusPath)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, 0, status.ExitCode, "codex policy exit code")
	logText, err := readText(rawLogPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(logText, "codex app-server dispatch policy: max_turns=30") {
		t.Fatalf("expected max_turns dispatch log, got:\n%s", logText)
	}
}

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
        assert "turnTimeoutMs" not in msg["params"], msg["params"]
        assert "readTimeoutMs" not in msg["params"], msg["params"]
        assert "stallTimeoutMs" not in msg["params"], msg["params"]
        assert "maxTurns" not in msg["params"], msg["params"]
        assert msg["params"]["sandboxPolicy"]=={
            "type":"workspaceWrite",
            "writableRoots":[os.environ["TUSKER_WORKSPACE"]],
            "networkAccess":False,
            "excludeTmpdirEnvVar":False,
            "excludeSlashTmp":False,
        }, msg["params"]
        assert msg["params"]["input"][0]["type"]=="text", msg["params"]
        assert msg["params"]["input"][0]["text"]=="Ship it.\n", msg["params"]
        assert msg["params"]["input"][0]["text_elements"]==[], msg["params"]
        print(json.dumps({"jsonrpc":"2.0","id":msg["id"],"result":{"turn":{"id":"turn-123","status":"inProgress","items":[]}}}), flush=True)
        print(json.dumps({"jsonrpc":"2.0","method":"item/commandExecution/requestApproval","id":"approve-command","params":{"threadId":"thread-123","turnId":"turn-123","itemId":"item-1","command":"go test ./cmd/tusker","cwd":os.environ["TUSKER_WORKSPACE"]}}), flush=True)
    elif msg.get("id")=="approve-command":
        assert msg["result"]["decision"]=="accept"
        print(json.dumps({"jsonrpc":"2.0","method":"item/fileChange/requestApproval","id":"approve-file","params":{"threadId":"thread-123","turnId":"turn-123","itemId":"item-2","changes":[{"path":os.path.join(os.environ["TUSKER_WORKSPACE"],"result.txt")}],"reason":"write result"}}), flush=True)
    elif msg.get("id")=="approve-file":
        assert msg["result"]["decision"]=="accept"
        print(json.dumps({"jsonrpc":"2.0","method":"item/permissions/requestApproval","id":"approve-permissions","params":{"threadId":"thread-123","turnId":"turn-123","itemId":"item-3","cwd":os.environ["TUSKER_WORKSPACE"],"reason":"needs current workspace","permissions":{}}}), flush=True)
    elif msg.get("id")=="approve-permissions":
        assert "error" in msg, msg
        assert "permission approval requests" in msg["error"]["message"], msg
        print(json.dumps({"jsonrpc":"2.0","method":"item/tool/requestUserInput","id":"user-input","params":{"threadId":"thread-123","turnId":"turn-123","itemId":"item-4","questions":[{"id":"choice","header":"Pick","question":"Choose","isOther":False,"isSecret":False,"options":None}]}}), flush=True)
    elif msg.get("id")=="user-input":
        assert msg["result"]["answers"]["choice"]["answers"]==[], msg
        print(json.dumps({"jsonrpc":"2.0","method":"mcpServer/elicitation/request","id":"mcp-elicit","params":{"server":"demo","message":"Need input","requestedSchema":{}}}), flush=True)
    elif msg.get("id")=="mcp-elicit":
        assert msg["result"]["action"]=="cancel", msg
        assert msg["result"]["content"] is None, msg
        assert msg["result"]["_meta"] is None, msg
        print(json.dumps({"jsonrpc":"2.0","method":"turn/started","params":{"threadId":"thread-123","turn":{"id":"turn-123","status":"inProgress","items":[]}}}), flush=True)
        print(json.dumps({"jsonrpc":"2.0","method":"thread/tokenUsage/updated","params":{"threadId":"thread-123","turnId":"turn-123","tokenUsage":{"total":{"inputTokens":11,"cachedInputTokens":0,"outputTokens":7,"reasoningOutputTokens":0,"totalTokens":18},"last":{"inputTokens":11,"cachedInputTokens":0,"outputTokens":7,"reasoningOutputTokens":0,"totalTokens":18},"modelContextWindow":200000}}}), flush=True)
        print(json.dumps({"jsonrpc":"2.0","method":"item/agentMessage/delta","params":{"threadId":"thread-123","item":{"id":"msg-codex-1"}}}), flush=True)
        print(json.dumps({"jsonrpc":"2.0","method":"turn/completed","params":{"threadId":"thread-123","turn":{"id":"turn-123","status":"completed","items":[]}}}), flush=True)
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
	store := openRuntimeStoreEventually(t)
	defer store.Close()
	turns := waitForStoredTurnStatus(t, store, "project-1", "record-1", "completed")
	assertEqual(t, 1, len(turns), "stored codex turn count")
	assertEqual(t, "completed", turns[0].Status, "stored codex turn status")
	assertEqual(t, 11, turns[0].InputTokens, "stored codex input tokens")
	assertEqual(t, 7, turns[0].OutputTokens, "stored codex output tokens")
	assertEqual(t, 18, turns[0].TotalTokens, "stored codex total tokens")
}

func TestCodexLiveEventLogFailureStopsRunner(t *testing.T) {
	tempRoot := t.TempDir()
	eventSinkPath := filepath.Join(tempRoot, "events.jsonl")
	rawLogPath := filepath.Join(tempRoot, "raw.log")
	statusPath := filepath.Join(tempRoot, "status.json")
	if err := writeText(eventSinkPath, `{"seq":1}`); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("sh", "-c", "sleep 30")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	handle := &codexLiveHandle{
		attemptID: "attempt-live-event-failure", eventSinkPath: eventSinkPath,
		rawLogPath: rawLogPath, statusPath: statusPath, runner: RunnerCodex,
		eventLog: NewEventLog(eventSinkPath), cmd: cmd,
	}
	if handle.appendCriticalEvent("turn_started", map[string]any{"turn_id": "turn-1"}) {
		t.Fatal("critical append unexpectedly succeeded")
	}
	if !handle.eventFailed.Load() {
		t.Fatal("live handle did not record event-log failure")
	}
	waitForStatusFile(t, statusPath)
	status, err := readRunnerProcessStatus(statusPath)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, 1, status.ExitCode, "event-log failure exit code")
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		t.Fatal("live runner survived critical event-log failure")
	}
	raw, err := readText(rawLogPath)
	if err != nil || !strings.Contains(raw, "critical event log failure") {
		t.Fatalf("event-log failure was not surfaced in raw log: %q err=%v", raw, err)
	}
}

func TestCodexLiveScannerFailureStopsAndReapsRunner(t *testing.T) {
	tempRoot := t.TempDir()
	cmd := exec.Command("sh", "-c", "sleep 30")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	stdout, writer := io.Pipe()
	handle := &codexLiveHandle{
		attemptID: "attempt-codex-scan-failure", rawLogPath: filepath.Join(tempRoot, "raw.log"),
		statusPath: filepath.Join(tempRoot, "status.json"), runner: RunnerCodex, cmd: cmd, stdout: stdout,
	}
	handle.ioWG.Add(1)
	go handle.readStdout()
	go handle.waitForExit()
	writeDone := make(chan struct{})
	go func() {
		_, _ = writer.Write(bytes.Repeat([]byte{'x'}, 4*1024*1024+1))
		_ = writer.Close()
		close(writeDone)
	}()

	waitForStatusFile(t, handle.statusPath)
	status, err := readRunnerProcessStatus(handle.statusPath)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, 1, status.ExitCode, "codex scanner failure exit code")
	waitForProcessGone(t, cmd.Process.Pid, "codex scanner failure")
	select {
	case <-writeDone:
	case <-time.After(3 * time.Second):
		t.Fatal("oversized codex stdout writer remained blocked")
	}
	raw, err := readText(handle.rawLogPath)
	if err != nil || !strings.Contains(raw, "stdout scan failed") {
		t.Fatalf("codex scanner failure was not surfaced: %q err=%v", raw, err)
	}
}

func TestApprovalPolicyOnRequestAllowsWorkspaceVerificationCommands(t *testing.T) {
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
	scriptPath := filepath.Join(tempRoot, "fake-codex.py")
	if err := writeText(promptPath, "Run verification.\n"); err != nil {
		t.Fatal(err)
	}
	script := `#!/usr/bin/env python3
import json,os,sys
for line in sys.stdin:
    msg=json.loads(line)
    if msg.get("method")=="initialize":
        print(json.dumps({"jsonrpc":"2.0","id":msg["id"],"result":{"serverInfo":{"name":"fake-codex"}}}), flush=True)
    elif msg.get("method")=="thread/start":
        assert msg["params"]["approvalPolicy"]=="on-request", msg["params"]
        assert msg["params"]["sandbox"]=="workspace-write", msg["params"]
        print(json.dumps({"jsonrpc":"2.0","id":msg["id"],"result":{"thread":{"id":"thread-policy"}}}), flush=True)
    elif msg.get("method")=="turn/start":
        assert msg["params"]["approvalPolicy"]=="on-request", msg["params"]
        assert msg["params"]["sandboxPolicy"]["type"]=="workspaceWrite", msg["params"]
        print(json.dumps({"jsonrpc":"2.0","id":msg["id"],"result":{"turn":{"id":"turn-policy","status":"inProgress","items":[]}}}), flush=True)
        print(json.dumps({"jsonrpc":"2.0","method":"item/commandExecution/requestApproval","id":"approve-command","params":{"threadId":"thread-policy","turnId":"turn-policy","itemId":"item-1","command":"rtk go test ./cmd/tusker -run TestStub -count=1","cwd":os.environ["TUSKER_WORKSPACE"]}}), flush=True)
    elif msg.get("id")=="approve-command":
        assert msg["result"]["decision"]=="accept", msg
        print(json.dumps({"jsonrpc":"2.0","method":"item/fileChange/requestApproval","id":"approve-file","params":{"threadId":"thread-policy","turnId":"turn-policy","itemId":"item-2","changes":[{"path":"changed.txt"}],"cwd":os.environ["TUSKER_WORKSPACE"],"reason":"write result"}}), flush=True)
    elif msg.get("id")=="approve-file":
        assert msg["result"]["decision"]=="accept", msg
        print(json.dumps({"jsonrpc":"2.0","method":"turn/completed","params":{"threadId":"thread-policy","turn":{"id":"turn-policy","status":"completed","items":[]}}}), flush=True)
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
		AttemptID:     "attempt-policy",
		WorkRevision:  0,
		WorkspacePath: workspaceRoot,
		PromptPath:    promptPath,
		EventSinkPath: eventSinkPath,
		RawLogPath:    rawLogPath,
		StatusPath:    statusPath,
		Command:       scriptPath,
		VaultPath:     tempRoot,
		CodexPolicy: CodexPolicy{
			ApprovalPolicy:    "on-request",
			ThreadSandbox:     "workspace-write",
			TurnSandboxPolicy: "workspace-write",
			TurnTimeoutMS:     5000,
			ReadTimeoutMS:     5000,
			StallTimeoutMS:    5000,
			MaxTurns:          1,
		},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "thread-policy", result.SessionRef, "codex policy session ref")
	waitForStatusFile(t, statusPath)
	status, err := readRunnerProcessStatus(statusPath)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, 0, status.ExitCode, "codex policy exit code")
	eventsText, err := readText(eventSinkPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(eventsText, "\"kind\":\"codex_approval_decision\"") != 2 {
		t.Fatalf("expected two approval decision events, got:\n%s", eventsText)
	}
	if strings.Count(eventsText, "\"decision\":\"accept\"") != 2 || !strings.Contains(eventsText, "\"approval_policy\":\"on-request\"") {
		t.Fatalf("expected accepted on-request approval events, got:\n%s", eventsText)
	}
}

func TestCodexLiveRunnerReviewerLaneForcesReadOnlyAndRejectsMutatingApprovals(t *testing.T) {
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
	scriptPath := filepath.Join(tempRoot, "fake-reviewer-codex.py")
	if err := writeText(promptPath, "Review only.\n"); err != nil {
		t.Fatal(err)
	}
	script := `#!/usr/bin/env python3
import json,os,sys
for line in sys.stdin:
    msg=json.loads(line)
    if msg.get("method")=="initialize":
        assert os.environ["TUSKER_RUN_LANE"]=="review", os.environ.get("TUSKER_RUN_LANE")
        assert os.environ["TUSKER_CODEX_APPROVAL_POLICY"]=="never", os.environ.get("TUSKER_CODEX_APPROVAL_POLICY")
        assert os.environ["TUSKER_CODEX_TURN_SANDBOX_POLICY"]=="read-only", os.environ.get("TUSKER_CODEX_TURN_SANDBOX_POLICY")
        print(json.dumps({"jsonrpc":"2.0","id":msg["id"],"result":{"serverInfo":{"name":"fake-codex"}}}), flush=True)
    elif msg.get("method")=="thread/start":
        assert msg["params"]["approvalPolicy"]=="never", msg["params"]
        assert msg["params"]["sandbox"]=="read-only", msg["params"]
        print(json.dumps({"jsonrpc":"2.0","id":msg["id"],"result":{"thread":{"id":"thread-review"}}}), flush=True)
    elif msg.get("method")=="turn/start":
        assert msg["params"]["approvalPolicy"]=="never", msg["params"]
        assert msg["params"]["sandboxPolicy"]=={"type":"readOnly","networkAccess":False}, msg["params"]
        print(json.dumps({"jsonrpc":"2.0","id":msg["id"],"result":{"turn":{"id":"turn-review","status":"inProgress","items":[]}}}), flush=True)
        print(json.dumps({"jsonrpc":"2.0","method":"item/commandExecution/requestApproval","id":"approve-command","params":{"threadId":"thread-review","turnId":"turn-review","itemId":"item-1","command":"touch reviewed.txt","cwd":os.environ["TUSKER_WORKSPACE"]}}), flush=True)
    elif msg.get("id")=="approve-command":
        assert msg["result"]["decision"]=="reject", msg
        assert "read-only sandbox" in msg["result"]["reason"], msg
        print(json.dumps({"jsonrpc":"2.0","method":"item/fileChange/requestApproval","id":"approve-file","params":{"threadId":"thread-review","turnId":"turn-review","itemId":"item-2","changes":[{"path":"reviewed.txt"}],"cwd":os.environ["TUSKER_WORKSPACE"]}}), flush=True)
    elif msg.get("id")=="approve-file":
        assert msg["result"]["decision"]=="reject", msg
        print(json.dumps({"jsonrpc":"2.0","method":"item/permissions/requestApproval","id":"approve-permissions","params":{"threadId":"thread-review","turnId":"turn-review","itemId":"item-3","cwd":os.environ["TUSKER_WORKSPACE"],"reason":"needs write permissions","permissions":{}}}), flush=True)
    elif msg.get("id")=="approve-permissions":
        assert "error" in msg, msg
        assert "permission approval requests" in msg["error"]["message"], msg
        print(json.dumps({"jsonrpc":"2.0","method":"applyPatchApproval","id":"approve-patch","params":{"cwd":os.environ["TUSKER_WORKSPACE"],"changes":[{"path":"reviewed.txt"}]}}), flush=True)
    elif msg.get("id")=="approve-patch":
        assert msg["result"]["decision"]=="denied", msg
        print(json.dumps({"jsonrpc":"2.0","method":"execCommandApproval","id":"approve-exec","params":{"command":"touch other.txt","cwd":os.environ["TUSKER_WORKSPACE"]}}), flush=True)
    elif msg.get("id")=="approve-exec":
        assert msg["result"]["decision"]=="denied", msg
        print(json.dumps({"jsonrpc":"2.0","method":"turn/completed","params":{"threadId":"thread-review","turn":{"id":"turn-review","status":"completed","items":[]}}}), flush=True)
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
		AttemptID:     "attempt-review",
		Lane:          runLaneReview,
		WorkRevision:  0,
		WorkspacePath: workspaceRoot,
		PromptPath:    promptPath,
		EventSinkPath: eventSinkPath,
		RawLogPath:    rawLogPath,
		StatusPath:    statusPath,
		Command:       scriptPath,
		VaultPath:     tempRoot,
		CodexPolicy: CodexPolicy{
			ApprovalPolicy:    "on-failure",
			ThreadSandbox:     "workspace-write",
			TurnSandboxPolicy: "workspace-write",
			TurnTimeoutMS:     5000,
			ReadTimeoutMS:     5000,
			StallTimeoutMS:    5000,
			MaxTurns:          1,
		},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "thread-review", result.SessionRef, "codex review session ref")
	waitForStatusFile(t, statusPath)
	status, err := readRunnerProcessStatus(statusPath)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, 0, status.ExitCode, "codex review exit code")
	eventsText, err := readText(eventSinkPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(eventsText, "\"kind\":\"codex_approval_decision\"") != 5 {
		t.Fatalf("expected five reviewer approval decision events, got:\n%s", eventsText)
	}
	if !strings.Contains(eventsText, "\"approval_policy\":\"never\"") || !strings.Contains(eventsText, "\"turn_sandbox_policy\":\"read-only\"") {
		t.Fatalf("expected reviewer approval events to expose enforced policy, got:\n%s", eventsText)
	}
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
	if err := writeText(notePath, "---\nid: ITEM-1\nrecord_id: record-1\ntype: task\ntitle: Continue me\nstatus: active\n---\n\n"); err != nil {
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
        assert "maxTurns" not in msg["params"], msg["params"]
        assert msg["params"]["sandboxPolicy"]["type"]=="workspaceWrite", msg["params"]
        text=msg["params"]["input"][0]["text"]
        assert msg["params"]["input"][0]["text_elements"]==[], msg["params"]
        if turn_count == 1:
            assert "First rendered prompt." in text, text
        if turn_count == 2:
            assert "Continue on the same Codex thread" in text, text
            assert "First rendered prompt." not in text, text
        turn_id=f"turn-{turn_count}"
        print(json.dumps({"jsonrpc":"2.0","id":msg["id"],"result":{"turn":{"id":turn_id,"status":"inProgress","items":[]}}}), flush=True)
        print(json.dumps({"jsonrpc":"2.0","method":"turn/started","params":{"threadId":"thread-continue","turn":{"id":turn_id,"status":"inProgress","items":[]}}}), flush=True)
        print(json.dumps({"jsonrpc":"2.0","method":"turn/completed","params":{"threadId":"thread-continue","turn":{"id":turn_id,"status":"completed","items":[]}}}), flush=True)
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
	store := openRuntimeStoreEventually(t)
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

func TestCodexApprovalPolicyRejectsWorkspaceEscapesSecretsAndGitMutations(t *testing.T) {
	workspaceRoot := t.TempDir()
	handle := &codexLiveHandle{
		cmd: &exec.Cmd{Dir: workspaceRoot},
		policy: codexPolicyForLane(CodexPolicy{
			ApprovalPolicy:    "on-failure",
			ThreadSandbox:     "workspace-write",
			TurnSandboxPolicy: "workspace-write",
		}, runLaneExecute),
	}

	outside := handle.evaluateFileChangeApproval([]byte(`{"cwd":"` + filepath.ToSlash(workspaceRoot) + `","changes":[{"path":"../outside.txt"}]}`))
	if outside.Decision != "reject" || !strings.Contains(outside.Reason, "escapes the prepared workspace") {
		t.Fatalf("expected outside workspace write rejection, got %#v", outside)
	}

	secret := handle.evaluateFileChangeApproval([]byte(`{"cwd":"` + filepath.ToSlash(workspaceRoot) + `","changes":[{"path":".env"}]}`))
	if secret.Decision != "reject" || !strings.Contains(secret.Reason, "secret path") {
		t.Fatalf("expected secret path rejection, got %#v", secret)
	}

	gitMutation := handle.evaluateCommandApproval([]byte(`{"cwd":"` + filepath.ToSlash(workspaceRoot) + `","command":"git reset --hard HEAD"}`))
	if gitMutation.Decision != "reject" || !strings.Contains(gitMutation.Reason, "unsafe git state mutation") {
		t.Fatalf("expected unsafe git mutation rejection, got %#v", gitMutation)
	}

	inside := handle.evaluateFileChangeApproval([]byte(`{"cwd":"` + filepath.ToSlash(workspaceRoot) + `","changes":[{"path":"result.txt"}]}`))
	if inside.Decision != "accept" {
		t.Fatalf("expected in-workspace file change approval, got %#v", inside)
	}
}

func TestCodexLiveRequestCleansPendingOnExitPaths(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		stdin := &recordingWriteCloser{}
		handle := &codexLiveHandle{
			stdin:   stdin,
			pending: map[string]chan codexRPCResponse{},
		}
		done := make(chan struct{})
		go func() {
			defer close(done)
			waitForCodexPendingRequest(t, handle, "1")
			handle.handleStdoutLine(`{"jsonrpc":"2.0","id":"1","result":{"ok":true}}`)
		}()
		var out struct {
			OK bool `json:"ok"`
		}
		if err := handle.request(context.Background(), "initialize", map[string]any{}, &out); err != nil {
			t.Fatal(err)
		}
		<-done
		if !out.OK {
			t.Fatal("expected response to unmarshal")
		}
		assertCodexPendingEmpty(t, handle)
	})

	t.Run("cancel", func(t *testing.T) {
		stdin := &recordingWriteCloser{}
		handle := &codexLiveHandle{
			stdin:   stdin,
			pending: map[string]chan codexRPCResponse{},
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		err := handle.request(ctx, "thread/start", map[string]any{}, nil)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context canceled, got %v", err)
		}
		assertCodexPendingEmpty(t, handle)
		handle.handleStdoutLine(`{"jsonrpc":"2.0","id":"1","result":{"thread":{"id":"late"}}}`)
		assertCodexPendingEmpty(t, handle)
	})

	t.Run("write error", func(t *testing.T) {
		errWrite := errors.New("write failed")
		stdin := &recordingWriteCloser{err: errWrite}
		handle := &codexLiveHandle{
			stdin:   stdin,
			pending: map[string]chan codexRPCResponse{},
		}
		err := handle.request(context.Background(), "thread/start", map[string]any{}, nil)
		if !errors.Is(err, errWrite) {
			t.Fatalf("expected write error, got %v", err)
		}
		assertCodexPendingEmpty(t, handle)
	})
}

type recordingWriteCloser struct {
	err    error
	writes [][]byte
}

func (w *recordingWriteCloser) Write(p []byte) (int, error) {
	if w.err != nil {
		return 0, w.err
	}
	w.writes = append(w.writes, append([]byte(nil), p...))
	return len(p), nil
}

func (w *recordingWriteCloser) Close() error {
	return nil
}

func waitForCodexPendingRequest(t *testing.T, handle *codexLiveHandle, id string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		handle.pendingMu.Lock()
		_, ok := handle.pending[id]
		handle.pendingMu.Unlock()
		if ok {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for pending request %s", id)
}

func assertCodexPendingEmpty(t *testing.T, handle *codexLiveHandle) {
	t.Helper()
	handle.pendingMu.Lock()
	defer handle.pendingMu.Unlock()
	if len(handle.pending) != 0 {
		t.Fatalf("expected no pending requests, got %d", len(handle.pending))
	}
}

func TestCodexLiveRunnerStartsFreshThreadAfterRestart(t *testing.T) {
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
	scriptPath := filepath.Join(tempRoot, "fake-codex.py")
	if err := writeText(promptPath, "Resume prompt.\n"); err != nil {
		t.Fatal(err)
	}
	script := `#!/usr/bin/env python3
import json,os,sys
for line in sys.stdin:
    msg=json.loads(line)
    if msg.get("method")=="initialize":
        print(json.dumps({"jsonrpc":"2.0","id":msg["id"],"result":{"serverInfo":{"name":"fake-codex"}}}), flush=True)
    elif msg.get("method")=="thread/start":
        assert os.environ.get("TUSKER_SESSION_REF","")=="", os.environ.get("TUSKER_SESSION_REF")
        assert os.environ.get("TUSKER_MESSAGE_REF","")=="", os.environ.get("TUSKER_MESSAGE_REF")
        assert msg["params"]["cwd"]==os.environ["TUSKER_WORKSPACE"], msg["params"]
        assert msg["params"]["approvalPolicy"]=="never", msg["params"]
        assert msg["params"]["sandbox"]=="read-only", msg["params"]
        print(json.dumps({"jsonrpc":"2.0","id":msg["id"],"result":{"thread":{"id":"thread-after-restart"}}}), flush=True)
    elif msg.get("method")=="thread/fork":
        raise AssertionError("restart recovery must not fork predecessor thread")
    elif msg.get("method")=="thread/resume":
        raise AssertionError("restart recovery must not resume predecessor thread")
    elif msg.get("method")=="turn/start":
        assert msg["params"]["threadId"]=="thread-after-restart", msg["params"]
        assert msg["params"]["sandboxPolicy"]=={"type":"readOnly","networkAccess":False}, msg["params"]
        assert msg["params"]["input"][0]["text"]=="Resume prompt.\n", msg["params"]
        assert msg["params"]["input"][0]["text_elements"]==[], msg["params"]
        print(json.dumps({"jsonrpc":"2.0","id":msg["id"],"result":{"turn":{"id":"turn-after-restart","status":"inProgress","items":[]}}}), flush=True)
        print(json.dumps({"jsonrpc":"2.0","method":"turn/started","params":{"threadId":"thread-after-restart","turn":{"id":"turn-after-restart","status":"inProgress","items":[]}}}), flush=True)
        print(json.dumps({"jsonrpc":"2.0","method":"turn/completed","params":{"threadId":"thread-after-restart","turn":{"id":"turn-after-restart","status":"completed","items":[]}}}), flush=True)
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
		AttemptID:     "attempt-2",
		WorkRevision:  0,
		WorkspacePath: workspaceRoot,
		PromptPath:    promptPath,
		EventSinkPath: eventSinkPath,
		RawLogPath:    rawLogPath,
		StatusPath:    statusPath,
		Command:       scriptPath,
		VaultPath:     tempRoot,
		CodexPolicy: CodexPolicy{
			ApprovalPolicy:    "never",
			ThreadSandbox:     "read-only",
			TurnSandboxPolicy: "read-only",
			TurnTimeoutMS:     5000,
			ReadTimeoutMS:     5000,
			StallTimeoutMS:    5000,
			MaxTurns:          1,
		},
	}, &ResumeRequest{SessionRef: "thread-before-restart"})
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "thread-after-restart", result.SessionRef, "fresh codex session ref after restart")
	waitForStatusFile(t, statusPath)
	status, err := readRunnerProcessStatus(statusPath)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, 0, status.ExitCode, "codex fresh restart exit code")
	store := openRuntimeStoreEventually(t)
	defer store.Close()
	turns := waitForStoredTurnCount(t, store, "project-1", "record-1", 1)
	assertEqual(t, "turn-after-restart", turns[0].TurnID, "fresh restart turn id")
	assertEqual(t, "thread-after-restart", turns[0].SessionRef, "fresh restart turn session ref")
}

func TestClaudeLiveRunnerCapabilityMatrixMatchesCodexAppServer(t *testing.T) {
	codex := (&CodexAppServerRunner{}).Capabilities()
	claude := (&ClaudeRunner{}).Capabilities()
	if codex.StructuredEvents != claude.StructuredEvents ||
		codex.ResumeSession != claude.ResumeSession ||
		codex.ExplicitApprovals != claude.ExplicitApprovals ||
		codex.Heartbeats != claude.Heartbeats ||
		codex.MachineFinalStatus != claude.MachineFinalStatus ||
		codex.UsageMetrics != claude.UsageMetrics {
		t.Fatalf("claude-code capability matrix must match codex_app_server for daemon-used capabilities: codex=%#v claude=%#v", codex, claude)
	}
}

func TestClaudeLiveRunnerCompletesFixtureThroughReviewLane(t *testing.T) {
	vault := automationTestVault(t)
	project := registerAutomationTestProject(t, vault)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Claude fixture", "risk": "low", "priority": "p0", "v7": "true"}, newV7Task)
	makeV7TaskDispatchableForTest(t, vault, "APP-T-0001")
	setAutomationV7TaskFields(t, vault, "APP-T-0001", map[string]any{"work_revision": 1})
	if _, err := upsertV7Verification(vault, "APP-T-0001", v7VerificationRow{CoverText: "A1", Check: "command: true", Result: "pass", Notes: "fixture proof"}, "agent:test"); err != nil {
		t.Fatal(err)
	}
	initializeOrchestrationGitRepo(t, filepath.Dir(vault))

	scriptPath := filepath.Join(filepath.Dir(vault), "fake-claude.py")
	script := `#!/usr/bin/env python3
import datetime, json, os, pathlib, re, subprocess, sys
def promote_note_to_review():
    path = pathlib.Path(os.environ["TUSKER_NOTE_PATH"])
    text = path.read_text()
    text = text.replace('status: "ready"', 'status: "review"')
    text = text.replace('readiness: "ready"', 'readiness: "ready"')
    text = text.replace('next_owner: "agent"', 'next_owner: "reviewer:agent"')
    path.write_text(text)
def prompt_arg(prompt, name):
    match = re.search(r"--" + re.escape(name) + r"\s+(\S+)", prompt)
    assert match, (name, prompt)
    return match.group(1)
def emit_review_proposal(msg):
    prompt = msg["message"]["content"]
    attempt_id = os.environ["TUSKER_ATTEMPT_ID"]
    actor_match = re.search(r"(?m)^- Reviewer actor:\s*(\S+)\s*$", prompt)
    assert actor_match, prompt
    assert prompt_arg(prompt, "attempt") == attempt_id
    work_revision = int(prompt_arg(prompt, "work-rev"))
    assert work_revision == int(os.environ["TUSKER_WORK_REVISION"])
    result = {
        "schema": "tusker.review-result/v2",
        "project_id": os.environ["TUSKER_CANONICAL_PROJECT_ID"],
        "task_id": os.environ["TUSKER_RECORD_ID"],
        "task_state_rev": prompt_arg(prompt, "task-rev"),
        "work_revision": work_revision,
        "implementation_sha": prompt_arg(prompt, "source-sha"),
        "attempt_id": attempt_id,
        "actor": actor_match.group(1),
        "runner": "",
        "runner_profile": "",
        "worker_policy_fingerprint": "",
        "covers": [],
        "proof_fingerprint": prompt_arg(prompt, "proof-fingerprint"),
        "gate_fingerprint": prompt_arg(prompt, "gate-fingerprint"),
        "verdict": "changes_requested",
        "summary": "generic Claude typed review",
        "findings": ["fixture finding"],
        "result_revision": "",
        "created_at": datetime.datetime.now(datetime.timezone.utc).replace(microsecond=0).isoformat().replace("+00:00", "Z"),
    }
    proposal = {
        "schema": "tusker.review-proposal/v1",
        "attempt_id": attempt_id,
        "result": result,
    }
    print("TUSKER_REVIEW_PROPOSAL_V1 " + json.dumps(proposal, separators=(",", ":")), flush=True)
for line in sys.stdin:
    msg=json.loads(line)
    if msg.get("type")=="control_request":
        continue
    if msg.get("type")=="user":
        lane=os.environ["TUSKER_RUN_LANE"]
        assert os.path.isdir(os.environ["TUSKER_WORKSPACE"]), os.environ["TUSKER_WORKSPACE"]
        if lane=="execute":
            workspace=os.environ["TUSKER_WORKSPACE"]
            subprocess.run(["git","init","-b","main"],cwd=workspace,check=True,stdout=subprocess.DEVNULL)
            subprocess.run(["git","config","user.email","test@example.com"],cwd=workspace,check=True)
            subprocess.run(["git","config","user.name","Tusker Test"],cwd=workspace,check=True)
            pathlib.Path(workspace,".gitignore").write_text(".tusker/workspace.json\n")
            subprocess.run(["git","rm","--cached","--ignore-unmatch",".tusker/workspace.json"],cwd=workspace,check=True,stdout=subprocess.DEVNULL)
            subprocess.run(["git","add","-A"],cwd=workspace,check=True)
            subprocess.run(["git","commit","-m","fixture end state"],cwd=workspace,check=True,stdout=subprocess.DEVNULL)
            promote_note_to_review()
        elif lane=="review":
            assert 'status: "review"' in pathlib.Path(os.environ["TUSKER_NOTE_PATH"]).read_text()
            emit_review_proposal(msg)
        else:
            raise AssertionError(lane)
        print(json.dumps({"type":"assistant","session_id":"claude-fixture-session","message":{"id":"msg-"+lane,"role":"assistant","content":[{"type":"text","text":"done"}],"usage":{"input_tokens":4,"output_tokens":2}}}), flush=True)
        print(json.dumps({"type":"result","subtype":"success","is_error":False,"session_id":"claude-fixture-session","usage":{"input_tokens":4,"output_tokens":2}}), flush=True)
        break
`
	if err := writeText(scriptPath, script); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(scriptPath, 0o755); err != nil {
		t.Fatal(err)
	}

	wf := defaultWorkflow()
	// This fixture dispatches through the daemon. Fresh workflows default the
	// daemon opt-in off, so the test must make that authorization explicit.
	wf.AutomationEnabled = true
	wf.Agents.Default = string(RunnerClaude)
	wf.Agents.Enabled = []string{string(RunnerClaude), string(RunnerCodexAppServer)}
	wf.Workspace.Strategy = string(WorkspaceStrategyCopy)
	wf.Claude.Command = scriptPath + " --input-format stream-json"
	wf.Runners[string(RunnerClaude)] = RunnerDefinition{Kind: string(RunnerClaude), Command: wf.Claude.Command}
	wf.Codex.ApprovalPolicy = "on-failure"
	wf.Codex.ThreadSandbox = "workspace-write"
	wf.Codex.TurnSandboxPolicy = "workspace-write"
	wf.Codex.ReadTimeoutMS = 5000
	wf.Codex.TurnTimeoutMS = 5000
	wfFile := WorkflowFile{Path: workflowPath(vault), Data: wf}

	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	daemon := &Daemon{stateRoot: DefaultStateRoot(), store: store}
	executeRun := RunStatus{
		ProjectID:    project.ProjectID,
		RecordID:     "APP-T-0001",
		ItemID:       "APP-T-0001",
		Runner:       string(RunnerClaude),
		LeaseState:   string(LeaseStateUnclaimed),
		WorkRevision: 1,
	}
	mustUpsertRun(t, store, executeRun)
	note, err := resolveNote(vault, "APP-T-0001")
	if err != nil {
		t.Fatal(err)
	}
	executeRun, _, err = daemon.dispatchRun(context.Background(), project, wfFile, note, executeRun, runLaneExecute)
	if err != nil {
		t.Fatal(err)
	}
	executeRun = finishDaemonRunForTest(t, daemon, project, wfFile, executeRun)
	if executeRun.LeaseState != string(LeaseStateReleased) {
		t.Fatalf("execute completion was not released: state=%s error=%s", executeRun.LeaseState, executeRun.LastError)
	}
	assertEqual(t, string(LeaseStateReleased), executeRun.LeaseState, "execute lease")
	assertEqual(t, string(AttemptOutcomeSucceeded), executeRun.AttemptOutcome, "execute outcome")
	implementationSHA := strings.TrimSpace(gitOutput(t, "-C", executeRun.WorkspacePath, "rev-parse", "HEAD"))
	// Copy-mode workspaces have an independent object database. Import the
	// exact implementation object before asking the review lane to resolve the
	// recorded source; no branch or canonical working-tree content is moved.
	runGitDir(t, filepath.Dir(vault), "fetch", executeRun.WorkspacePath, implementationSHA)
	setAutomationV7TaskFields(t, vault, "APP-T-0001", map[string]any{
		"source_sha":    implementationSHA,
		"work_revision": 1,
	})
	updatedNote, err := resolveNote(vault, "APP-T-0001")
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "review", stringField(updatedNote.Data, "status"), "fixture promoted to review")

	reviewRun, _, err := daemon.dispatchRun(context.Background(), project, wfFile, updatedNote, executeRun, runLaneReview)
	if err != nil {
		t.Fatal(err)
	}
	reviewRawLogPath := reviewRun.RawLogPath
	reviewRun = finishDaemonRunForTest(t, daemon, project, wfFile, reviewRun)
	if reviewRun.AttemptOutcome != string(AttemptOutcomeSucceeded) {
		raw, _ := readText(reviewRawLogPath)
		if raw == "" {
			_ = filepath.WalkDir(DefaultStateRoot(), func(path string, entry os.DirEntry, walkErr error) error {
				if walkErr == nil && !entry.IsDir() && strings.HasSuffix(path, ".raw.log") {
					if candidate, readErr := readText(path); readErr == nil {
						raw += path + ":\n" + candidate + "\n"
					}
				}
				return nil
			})
		}
		t.Fatalf("review completion failed: state=%s outcome=%s error=%s\nraw log:\n%s", reviewRun.LeaseState, reviewRun.AttemptOutcome, reviewRun.LastError, raw)
	}
	assertEqual(t, runLaneReview, reviewRun.Lane, "review lane")
	assertEqual(t, string(LeaseStateReleased), reviewRun.LeaseState, "review lease")
	assertEqual(t, string(AttemptOutcomeSucceeded), reviewRun.AttemptOutcome, "review outcome")
	results, err := store.ListReviewResults(project.ProjectID)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Result.Schema != reviewResultSchemaV2 || results[0].Result.WorkerPolicyFP != "" {
		t.Fatalf("generic Claude review did not persist authority-less typed transport: %#v", results)
	}
	turns, err := store.ListTurnsForRun(project.ProjectID, "APP-T-0001")
	if err != nil {
		t.Fatal(err)
	}
	if len(turns) < 2 {
		t.Fatalf("expected execute and review turns, got %#v", turns)
	}
}

func TestClaudeResumeLiveRunnerUsesSessionRefAfterRestart(t *testing.T) {
	tempRoot := t.TempDir()
	t.Setenv("TUSKER_STATE_ROOT", filepath.Join(tempRoot, "state"))
	workspaceRoot := filepath.Join(tempRoot, "workspace")
	if err := ensureDir(workspaceRoot); err != nil {
		t.Fatal(err)
	}
	rawLogPath := filepath.Join(tempRoot, "claude.log")
	statusPath := filepath.Join(tempRoot, "claude.status.json")
	eventSinkPath := filepath.Join(tempRoot, "events.jsonl")
	promptPath := filepath.Join(tempRoot, "prompt.txt")
	scriptPath := filepath.Join(tempRoot, "fake-claude.py")
	if err := writeText(promptPath, "Resume prompt.\n"); err != nil {
		t.Fatal(err)
	}
	script := `#!/usr/bin/env python3
import json,os,sys
assert "--resume claude-session-before-restart" in " ".join(sys.argv), sys.argv
for line in sys.stdin:
    msg=json.loads(line)
    if msg.get("type")=="control_request":
        continue
    if msg.get("type")=="user":
        assert os.environ["TUSKER_SESSION_REF"]=="claude-session-before-restart", os.environ.get("TUSKER_SESSION_REF")
        print(json.dumps({"type":"assistant","session_id":"claude-session-before-restart","message":{"id":"msg-resumed","role":"assistant","content":[{"type":"text","text":"resumed"}],"usage":{"input_tokens":5,"output_tokens":3}}}), flush=True)
        print(json.dumps({"type":"result","subtype":"success","is_error":False,"session_id":"claude-session-before-restart","usage":{"input_tokens":5,"output_tokens":3}}), flush=True)
        break
`
	if err := writeText(scriptPath, script); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(scriptPath, 0o755); err != nil {
		t.Fatal(err)
	}

	result, err := (&ClaudeRunner{}).Resume(context.Background(), ResumeRequest{
		ProjectID:     "project-1",
		RecordID:      "record-1",
		ItemID:        "ITEM-1",
		AttemptID:     "attempt-resume",
		WorkRevision:  0,
		SessionRef:    "claude-session-before-restart",
		WorkspacePath: workspaceRoot,
		PromptPath:    promptPath,
		EventSinkPath: eventSinkPath,
		RawLogPath:    rawLogPath,
		StatusPath:    statusPath,
		Command:       scriptPath + " --input-format stream-json --resume {{session_ref}}",
		VaultPath:     tempRoot,
		CodexPolicy:   CodexPolicy{ApprovalPolicy: "never", ThreadSandbox: "read-only", TurnSandboxPolicy: "read-only"},
	})
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "claude-session-before-restart", result.SessionRef, "resumed claude session ref")
	waitForStatusFile(t, statusPath)
	status, err := readRunnerProcessStatus(statusPath)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, 0, status.ExitCode, "claude resumed exit code")
	store := openRuntimeStoreEventually(t)
	defer store.Close()
	turns := waitForStoredTurnStatus(t, store, "project-1", "record-1", "completed")
	assertEqual(t, "claude-session-before-restart", turns[0].SessionRef, "resumed turn session ref")
	reconciled, err := (&ClaudeRunner{}).Reconcile(context.Background(), ReconcileRequest{SessionRef: "claude-session-before-restart"})
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, LeaseStateRetryQueued, reconciled.LeaseState, "reconcile lease")
	assertEqual(t, AttemptOutcomeNone, reconciled.Outcome, "reconcile outcome")
	assertEqual(t, "session is resumable", reconciled.Reason, "reconcile reason")
}

func TestClaudeUsageLiveRunnerRecordsTokensAndReviewPacket(t *testing.T) {
	tempRoot := t.TempDir()
	t.Setenv("TUSKER_STATE_ROOT", filepath.Join(tempRoot, "state"))
	workspaceRoot := filepath.Join(tempRoot, "workspace")
	if err := ensureDir(workspaceRoot); err != nil {
		t.Fatal(err)
	}
	rawLogPath := filepath.Join(tempRoot, "claude.log")
	statusPath := filepath.Join(tempRoot, "claude.status.json")
	eventSinkPath := filepath.Join(tempRoot, "events.jsonl")
	promptPath := filepath.Join(tempRoot, "prompt.txt")
	scriptPath := filepath.Join(tempRoot, "fake-claude.py")
	if err := writeText(promptPath, "Count tokens.\n"); err != nil {
		t.Fatal(err)
	}
	script := `#!/usr/bin/env python3
import json,sys
for line in sys.stdin:
    msg=json.loads(line)
    if msg.get("type")=="control_request":
        continue
    if msg.get("type")=="user":
        print(json.dumps({"type":"assistant","session_id":"claude-usage-session","message":{"id":"msg-usage","role":"assistant","content":[{"type":"text","text":"usage"}],"usage":{"input_tokens":17,"output_tokens":9}}}), flush=True)
        print(json.dumps({"type":"result","subtype":"success","is_error":False,"session_id":"claude-usage-session","usage":{"input_tokens":17,"output_tokens":9}}), flush=True)
        break
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
		AttemptID:     "attempt-usage",
		WorkRevision:  0,
		WorkspacePath: workspaceRoot,
		PromptPath:    promptPath,
		EventSinkPath: eventSinkPath,
		RawLogPath:    rawLogPath,
		StatusPath:    statusPath,
		Command:       scriptPath,
		VaultPath:     tempRoot,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "claude-usage-session", result.SessionRef, "claude usage session ref")
	waitForStatusFile(t, statusPath)
	store := openRuntimeStoreEventually(t)
	defer store.Close()
	turns := waitForStoredTurnStatus(t, store, "project-1", "record-1", "completed")
	assertEqual(t, 17, turns[0].InputTokens, "stored claude input tokens")
	assertEqual(t, 9, turns[0].OutputTokens, "stored claude output tokens")
	assertEqual(t, 26, turns[0].TotalTokens, "stored claude total tokens")
	eventsText, err := readText(eventSinkPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"\"kind\":\"turn_started\"", "\"kind\":\"turn_usage_updated\"", "\"kind\":\"turn_completed\""} {
		if !strings.Contains(eventsText, expected) {
			t.Fatalf("expected normalized claude events to include %s, got:\n%s", expected, eventsText)
		}
	}
	packet := renderReviewPacket(Note{Data: map[string]any{"id": "ITEM-1", "title": "Claude usage"}}, RunStatus{
		ProjectID:       "project-1",
		RecordID:        "record-1",
		ItemID:          "ITEM-1",
		Runner:          string(RunnerClaude),
		Lane:            runLaneExecute,
		ActiveAttemptID: "attempt-usage",
		WorkspacePath:   workspaceRoot,
		SessionRef:      "claude-usage-session",
		EventSinkPath:   eventSinkPath,
		RawLogPath:      rawLogPath,
		StatusPath:      statusPath,
	}, turns, nil, collectReviewPacketFacts(RunStatus{EventSinkPath: eventSinkPath, WorkspacePath: workspaceRoot, StatusPath: statusPath}))
	if !strings.Contains(packet, "Usage telemetry: raw diagnostic data only; it is neither billable nor an exact aggregate.") {
		t.Fatalf("expected review packet diagnostic usage disclaimer, got:\n%s", packet)
	}
}

func TestClaudeInterruptLiveRunnerSupportsInterrupt(t *testing.T) {
	tempRoot := t.TempDir()
	t.Setenv("TUSKER_STATE_ROOT", filepath.Join(tempRoot, "state"))
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
	store := openRuntimeStoreEventually(t)
	defer store.Close()
	turns := waitForStoredTurnStatus(t, store, "project-1", "record-1", "interrupted")
	assertEqual(t, "interrupted", turns[0].Status, "stored claude interrupted turn")
}

func TestClaudeLiveWaitDrainsFinalResultBeforeProcessExit(t *testing.T) {
	tempRoot := t.TempDir()
	cmd := exec.Command("sh", "-c", "exit 7")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	stdout, writer := io.Pipe()
	handle := &claudeLiveHandle{
		attemptID: "attempt-claude-drain", rawLogPath: filepath.Join(tempRoot, "raw.log"),
		statusPath: filepath.Join(tempRoot, "status.json"), runner: RunnerClaude, cmd: cmd, stdout: stdout,
	}
	handle.ioWG.Add(1)
	go handle.readStdout()
	go handle.waitForExit()
	time.Sleep(100 * time.Millisecond)
	if _, err := os.Stat(handle.statusPath); !os.IsNotExist(err) {
		t.Fatalf("process exit was classified before stdout drained: %v", err)
	}
	if _, err := writer.Write([]byte(`{"type":"result","subtype":"success","is_error":false}` + "\n")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	waitForStatusFile(t, handle.statusPath)
	status, err := readRunnerProcessStatus(handle.statusPath)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, 0, status.ExitCode, "final buffered Claude result wins over process exit")
	waitForProcessGone(t, cmd.Process.Pid, "claude drain")
}

func TestClaudeLiveScannerFailureStopsAndReapsRunner(t *testing.T) {
	tempRoot := t.TempDir()
	cmd := exec.Command("sh", "-c", "sleep 30")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	stdout, writer := io.Pipe()
	handle := &claudeLiveHandle{
		attemptID: "attempt-claude-scan-failure", rawLogPath: filepath.Join(tempRoot, "raw.log"),
		statusPath: filepath.Join(tempRoot, "status.json"), runner: RunnerClaude, cmd: cmd, stdout: stdout,
	}
	handle.ioWG.Add(1)
	go handle.readStdout()
	go handle.waitForExit()
	writeDone := make(chan struct{})
	go func() {
		_, _ = writer.Write(bytes.Repeat([]byte{'x'}, 4*1024*1024+1))
		_ = writer.Close()
		close(writeDone)
	}()

	waitForStatusFile(t, handle.statusPath)
	status, err := readRunnerProcessStatus(handle.statusPath)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, 1, status.ExitCode, "claude scanner failure exit code")
	waitForProcessGone(t, cmd.Process.Pid, "claude scanner failure")
	select {
	case <-writeDone:
	case <-time.After(3 * time.Second):
		t.Fatal("oversized Claude stdout writer remained blocked")
	}
	raw, err := readText(handle.rawLogPath)
	if err != nil || !strings.Contains(raw, "stdout scan failed") {
		t.Fatalf("Claude scanner failure was not surfaced: %q err=%v", raw, err)
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
	scriptPath := filepath.Join(tempRoot, "record-workspace.py")
	if err := writeText(scriptPath, "import os,pathlib\npathlib.Path(os.environ[\"TUSKER_WORKSPACE\"], \"cwd.txt\").write_text(os.getcwd()+\"\\n\"+os.environ.get(\"TUSKER_REPO_ROOT\",\"\"))\n"); err != nil {
		t.Fatal(err)
	}
	command := "python3 " + scriptPath
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
	deadline := time.Now().Add(runnerLiveTestWait)
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
	deadline := time.Now().Add(runnerLiveTestWait)
	lastText := ""
	lastErr := error(nil)
	for time.Now().Before(deadline) {
		text, err := readText(path)
		lastText, lastErr = text, err
		if err == nil && strings.Contains(text, needle) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %q in %s (last error: %v)\nlast contents:\n%s", needle, path, lastErr, lastText)
}

func waitForStoredTurnStatus(t *testing.T, store *RuntimeStore, projectID, recordID, status string) []RunTurn {
	t.Helper()
	deadline := time.Now().Add(runnerLiveTestWait)
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

func openRuntimeStoreEventually(t *testing.T) *RuntimeStore {
	t.Helper()
	deadline := time.Now().Add(runnerLiveTestWait)
	var lastErr error
	for time.Now().Before(deadline) {
		store, err := OpenRuntimeStore(DefaultStateRoot())
		if err == nil {
			return store
		}
		lastErr = err
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out opening runtime store: %v", lastErr)
	return nil
}

func waitForStoredTurnCount(t *testing.T, store *RuntimeStore, projectID, recordID string, count int) []RunTurn {
	t.Helper()
	deadline := time.Now().Add(runnerLiveTestWait)
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

func finishDaemonRunForTest(t *testing.T, daemon *Daemon, project RegisteredProject, wfFile WorkflowFile, run RunStatus) RunStatus {
	t.Helper()
	if strings.TrimSpace(run.StatusPath) == "" {
		if LeaseState(run.LeaseState) == LeaseStateReleased || LeaseState(run.LeaseState) == LeaseStateRetryQueued {
			return run
		}
		t.Fatalf("run has no status path and is not terminal: %#v", run)
	}
	waitForStatusFile(t, run.StatusPath)
	reconciled, _, err := daemon.reconcileRun(context.Background(), project, wfFile, run)
	if err != nil {
		t.Fatal(err)
	}
	if err := daemon.store.UpsertRun(reconciled); err != nil {
		t.Fatal(err)
	}
	return reconciled
}
