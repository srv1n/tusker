package main

import (
	"testing"
	"time"
)

func TestApprovalCLIListAndRespond(t *testing.T) {
	t.Setenv("TUSKER_STATE_ROOT", t.TempDir())
	for _, name := range []string{"TUSKER_ATTEMPT_ID", "CODEX_SHELL", "CODEX_THREAD_ID", "CODEX_SESSION_ID", "CLAUDECODE", "CLAUDE_CODE_ENTRYPOINT", "CHISEL_SESSION_DB"} {
		t.Setenv(name, "")
	}
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	request := testAgentAccessApprovalRequest("cli-approval", "attempt-1", time.Now().UTC().Add(time.Minute))
	if _, _, err := store.CreateAgentAccessApproval(request); err != nil {
		t.Fatal(err)
	}
	store.Close()
	if err := approvalsRespondCmd(Args{"_pos0": request.RequestID, "allow-once": "true", "block": "true", "by": "human:operator"}); err == nil {
		t.Fatal("ambiguous decision accepted")
	}
	t.Setenv("CODEX_SHELL", "1")
	if err := approvalsRespondCmd(Args{"_pos0": request.RequestID, "allow-once": "true", "by": "human:operator"}); err == nil {
		t.Fatal("agent session settled its own approval")
	}
	t.Setenv("CODEX_SHELL", "")
	if err := approvalsListCmd(Args{"project": "project", "json": "true"}); err != nil {
		t.Fatal(err)
	}
	if err := approvalsRespondCmd(Args{"_pos0": request.RequestID, "allow-once": "true", "by": "human:operator"}); err != nil {
		t.Fatal(err)
	}
	store, err = OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	allowed, err := store.AgentAccessApproval(request.RequestID)
	if err != nil || allowed.State != AgentAccessApprovalAllowed {
		t.Fatalf("allow once: %#v, %v", allowed, err)
	}
	request = testAgentAccessApprovalRequest("cli-block", "attempt-1", time.Now().UTC().Add(time.Minute))
	if _, _, err := store.CreateAgentAccessApproval(request); err != nil {
		t.Fatal(err)
	}
	store.Close()
	if err := approvalsRespondCmd(Args{"_pos0": request.RequestID, "block": "true", "by": "human:operator"}); err != nil {
		t.Fatal(err)
	}
	store, err = OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	blocked, err := store.AgentAccessApproval(request.RequestID)
	if err != nil || blocked.State != AgentAccessApprovalDenied {
		t.Fatalf("block: %#v, %v", blocked, err)
	}
}
