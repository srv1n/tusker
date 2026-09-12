package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAgentTransportCodexSteersExactActiveTurn(t *testing.T) {
	root := t.TempDir()
	t.Setenv("TUSKER_STATE_ROOT", filepath.Join(root, "state"))
	workspace := filepath.Join(root, "workspace")
	if err := ensureDir(workspace); err != nil {
		t.Fatal(err)
	}
	prompt := filepath.Join(root, "prompt")
	if err := writeText(prompt, "begin"); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(root, "fake-codex.py")
	source := `#!/usr/bin/env python3
import json,sys
for line in sys.stdin:
 m=json.loads(line); method=m.get("method")
 if method=="initialize": print(json.dumps({"jsonrpc":"2.0","id":m["id"],"result":{}}),flush=True)
 elif method=="thread/start": print(json.dumps({"jsonrpc":"2.0","id":m["id"],"result":{"thread":{"id":"thread-1"}}}),flush=True)
 elif method=="turn/start": print(json.dumps({"jsonrpc":"2.0","id":m["id"],"result":{"turn":{"id":"turn-1"}}}),flush=True)
 elif method=="turn/steer":
  assert m["params"]["threadId"]=="thread-1" and m["params"]["expectedTurnId"]=="turn-1"
  assert m["params"]["clientUserMessageId"]=="msg-1" and m["params"]["input"][0]["text"]=="answer"
  print(json.dumps({"jsonrpc":"2.0","id":m["id"],"result":{"turnId":"turn-1"}}),flush=True)
  print(json.dumps({"jsonrpc":"2.0","method":"turn/completed","params":{"threadId":"thread-1","turn":{"id":"turn-1","status":"completed","items":[]}}}),flush=True); break
`
	if err := writeText(script, source); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(script, 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := startLiveCodex(context.Background(), StartRequest{ProjectID: "app", RecordID: "task4", ItemID: "task4", AttemptID: "attempt-1", WorkspacePath: workspace, PromptPath: prompt, EventSinkPath: filepath.Join(root, "events"), RawLogPath: filepath.Join(root, "raw"), StatusPath: filepath.Join(root, "status"), Command: script, VaultPath: root, CodexPolicy: CodexPolicy{ApprovalPolicy: "never", ThreadSandbox: "read-only", TurnSandboxPolicy: "read-only", ReadTimeoutMS: 5000, TurnTimeoutMS: 5000, StallTimeoutMS: 5000, MaxTurns: 1}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	store, err := OpenRuntimeStore(filepath.Join(root, "runtime"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.UpsertProject(RegisteredProject{ProjectID: "app", ProjectKey: "app", Name: "app", RepoRoot: workspace, VaultRoot: root, Enabled: true, Health: projectHealthHealthy}); err != nil {
		t.Fatal(err)
	}
	if err := store.insertExecutionRecord(ExecutionRecord{ExecutionID: "exec-1", RootExecutionID: "exec-1", ProjectID: "app", NodeKind: ExecutionNodeRoot, AttemptID: "attempt-1", CreatedAt: time.Now().UTC().Format(time.RFC3339)}); err != nil {
		t.Fatal(err)
	}
	message, _, err := store.PutAgentMessage(AgentMessage{ID: "msg-1", IdempotencyKey: "steer-1", ProjectID: "app", Sender: "task:sender", Recipient: AgentAddress{Kind: "execution", ID: "exec-1"}, Kind: "instruction", Body: "answer"})
	if err != nil || message.ID != "msg-1" {
		t.Fatal(err)
	}
	if err := (&Daemon{store: store}).processAgentWakeups("app"); err != nil {
		t.Fatal(err)
	}
	waitForStatusFile(t, filepath.Join(root, "status"))
}
