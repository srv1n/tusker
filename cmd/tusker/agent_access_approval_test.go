package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"tusker/internal/acp"
)

func testAgentAccessApprovalRequest(id, attempt string, expires time.Time) AgentAccessApprovalRequest {
	return AgentAccessApprovalRequest{
		RequestID:         id,
		ProjectID:         "project",
		TaskID:            "task",
		AttemptID:         attempt,
		ExecutionID:       "execution-1",
		SessionID:         "session-1",
		NativeRequestID:   "native-" + id,
		Route:             "codex_cli",
		PolicyFingerprint: "sha256:policy",
		Tool:              "shell",
		Arguments:         "{\"command\":\"git clean -fd\",\"cwd\":\"/workspace\"}",
		RedactedArguments: []byte("{\"command\":\"git clean -fd\",\"cwd\":\"/workspace\"}"),
		WorkingDirectory:  "/workspace",
		Targets:           []string{"/workspace"},
		Reason:            "recognized destructive action",
		NativeOptionID:    "allow-once-option",
		NativeOptionKind:  AgentAccessApprovalAllowOnce,
		ExpiresAt:         expires.Format(time.RFC3339Nano),
		LiveUntil:         expires.Format(time.RFC3339Nano),
	}
}

func TestAgentAccessApproval(t *testing.T) {
	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	expires := time.Now().UTC().Add(time.Minute)
	request := testAgentAccessApprovalRequest("approval-1", "attempt-1", expires)
	first, created, err := store.CreateAgentAccessApproval(request)
	if err != nil || !created || first.State != AgentAccessApprovalPending || first.StateRevision != 1 {
		t.Fatalf("create = %#v created=%v err=%v", first, created, err)
	}
	duplicate, created, err := store.CreateAgentAccessApproval(request)
	if err != nil || created || duplicate.RequestID != first.RequestID {
		t.Fatalf("duplicate = %#v created=%v err=%v", duplicate, created, err)
	}
	request.Arguments = "{\"command\":\"git reset --hard\",\"cwd\":\"/workspace\"}"
	if _, _, err := store.CreateAgentAccessApproval(request); err == nil || !strings.Contains(err.Error(), "conflicts") {
		t.Fatalf("changed arguments were accepted: %v", err)
	}
	if _, err := store.SettleAgentAccessApproval(AgentAccessApprovalResponse{RequestID: first.RequestID, ExpectedRevision: 1, Decision: AgentAccessApprovalAllowOnce, Actor: "agent:worker"}); err == nil {
		t.Fatal("worker self-approval was accepted")
	}
	allowed, err := store.SettleAgentAccessApproval(AgentAccessApprovalResponse{RequestID: first.RequestID, ExpectedRevision: 1, Decision: AgentAccessApprovalAllowOnce, Actor: "human:operator"})
	if err != nil || allowed.State != AgentAccessApprovalAllowed || allowed.Decision != AgentAccessApprovalAllowOnce || allowed.StateRevision != 2 {
		t.Fatalf("allow once = %#v err=%v", allowed, err)
	}
	duplicateDecision, err := store.SettleAgentAccessApproval(AgentAccessApprovalResponse{RequestID: first.RequestID, ExpectedRevision: 1, Decision: AgentAccessApprovalAllowOnce, Actor: "human:operator"})
	if err != nil || duplicateDecision.State != AgentAccessApprovalAllowed {
		t.Fatalf("duplicate decision = %#v err=%v", duplicateDecision, err)
	}
	if _, err := store.SettleAgentAccessApproval(AgentAccessApprovalResponse{RequestID: first.RequestID, ExpectedRevision: 1, Decision: AgentAccessApprovalDeny, Actor: "human:operator"}); err == nil {
		t.Fatal("conflicting terminal decision was accepted")
	}

	second := testAgentAccessApprovalRequest("approval-2", "attempt-1", expires)
	if _, _, err := store.CreateAgentAccessApproval(second); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SettleAgentAccessApproval(AgentAccessApprovalResponse{RequestID: second.RequestID, ExpectedRevision: 1, Decision: AgentAccessApprovalDeny, Actor: "human:operator"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SettleAgentAccessApproval(AgentAccessApprovalResponse{RequestID: second.RequestID, ExpectedRevision: 1, Decision: AgentAccessApprovalAllowOnce, Actor: "human:operator"}); err == nil {
		t.Fatal("stale/conflicting response was accepted")
	}
}

func TestAgentAccessApprovalRecovery(t *testing.T) {
	stateRoot := t.TempDir()
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	request := testAgentAccessApprovalRequest("recovery-1", "attempt-1", time.Now().UTC().Add(10*time.Minute))
	created, ok, err := store.CreateAgentAccessApproval(request)
	if err != nil || !ok || created.State != AgentAccessApprovalPending {
		t.Fatalf("create = %#v ok=%v err=%v", created, ok, err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	restarted, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	// Opening a store is not a restart boundary: another process may own the
	// live native callback. Reconciliation is explicit at the authoritative
	// lifecycle boundary.
	if expired, err := restarted.ReconcileAgentAccessApprovals(time.Now().UTC(), nil); err != nil || expired != 1 {
		t.Fatalf("explicit restart reconciliation expired=%d err=%v", expired, err)
	}
	recovered, err := restarted.AgentAccessApproval(request.RequestID)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.State != AgentAccessApprovalExpired || recovered.StateRevision != 2 {
		t.Fatalf("restart did not expire unverifiable request: %#v", recovered)
	}
	if _, err := restarted.SettleAgentAccessApproval(AgentAccessApprovalResponse{RequestID: request.RequestID, ExpectedRevision: 1, Decision: AgentAccessApprovalAllowOnce, Actor: "human:operator"}); err == nil {
		t.Fatal("expired request was approved after restart")
	}

	newAttempt := request
	newAttempt.RequestID = "recovery-2"
	newAttempt.AttemptID = "attempt-2"
	newAttempt.NativeRequestID = "native-recovery-2"
	if next, ok, err := restarted.CreateAgentAccessApproval(newAttempt); err != nil || !ok || next.State != AgentAccessApprovalPending {
		t.Fatalf("new attempt could not create a fresh request: %#v ok=%v err=%v", next, ok, err)
	}
}

func TestAgentAccessApprovalStoreOpenPreservesLivePendingRequest(t *testing.T) {
	stateRoot := t.TempDir()
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	request := testAgentAccessApprovalRequest("second-store-1", "attempt-1", time.Now().UTC().Add(10*time.Minute))
	created, ok, err := store.CreateAgentAccessApproval(request)
	if err != nil || !ok || created.State != AgentAccessApprovalPending {
		t.Fatalf("create = %#v ok=%v err=%v", created, ok, err)
	}

	second, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	opened, err := second.AgentAccessApproval(request.RequestID)
	if err != nil {
		t.Fatal(err)
	}
	if opened.State != AgentAccessApprovalPending || opened.StateRevision != 1 {
		t.Fatalf("opening second store changed pending request: %#v", opened)
	}
}

func TestDaemonStartupReconcilesAgentAccessApprovals(t *testing.T) {
	stateRoot := t.TempDir()
	t.Setenv("TUSKER_STATE_ROOT", stateRoot)
	clearAgentSessionEnvForTest(t)

	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	request := testAgentAccessApprovalRequest("daemon-startup-1", "attempt-daemon-startup", time.Now().UTC().Add(10*time.Minute))
	if _, created, err := store.CreateAgentAccessApproval(request); err != nil || !created {
		t.Fatalf("seed approval created=%v err=%v", created, err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	if err := daemonRunCmd(Args{"once": "true"}); err != nil {
		t.Fatal(err)
	}

	store, err = OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	approval, err := store.AgentAccessApproval(request.RequestID)
	if err != nil {
		t.Fatal(err)
	}
	if approval.State != AgentAccessApprovalExpired || approval.StateRevision != 2 {
		t.Fatalf("daemon startup did not expire unverifiable approval: %#v", approval)
	}
}

func TestDaemonStartupReconciliationPreservesRegisteredLiveAttempt(t *testing.T) {
	stateRoot := t.TempDir()
	t.Setenv("TUSKER_STATE_ROOT", stateRoot)
	clearAgentSessionEnvForTest(t)
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}

	request := testAgentAccessApprovalRequest("daemon-live-1", "attempt-daemon-live", time.Now().UTC().Add(10*time.Minute))
	if _, created, err := store.CreateAgentAccessApproval(request); err != nil || !created {
		t.Fatalf("seed approval created=%v err=%v", created, err)
	}
	handle := &interruptTestLiveHandle{attemptID: request.AttemptID, projectID: request.ProjectID, itemID: request.TaskID}
	liveRegistry.Register(handle)
	defer liveRegistry.Unregister(handle.attemptID)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	if err := daemonRunCmd(Args{"once": "true"}); err != nil {
		t.Fatal(err)
	}
	store, err = OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	approval, err := store.AgentAccessApproval(request.RequestID)
	if err != nil {
		t.Fatal(err)
	}
	if approval.State != AgentAccessApprovalPending || approval.StateRevision != 1 {
		t.Fatalf("live registered approval was reconciled away: %#v", approval)
	}
}

func TestAgentAccessApprovalNativeOptionAndCancellation(t *testing.T) {
	stateRoot := t.TempDir()
	t.Setenv("TUSKER_STATE_ROOT", stateRoot)
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	provenance := acpAttemptProvenance{ProjectID: "project", TaskID: "task", AttemptID: "attempt-acp", RuntimeStore: store}
	request := acp.PermissionRequest{
		SessionID:  "session-acp",
		ToolCallID: "call-acp",
		ToolKind:   "execute",
		RawInput:   []byte("{\"command\":\"git clean -fd\",\"cwd\":\"/workspace\"}"),
		Raw:        []byte("{\"sessionId\":\"session-acp\",\"toolCall\":{\"toolCallId\":\"call-acp\",\"kind\":\"execute\",\"rawInput\":{\"command\":\"git clean -fd\",\"cwd\":\"/workspace\"}},\"options\":[{\"optionId\":\"allow-option\",\"kind\":\"allow_once\"},{\"optionId\":\"reject-option\",\"kind\":\"reject_once\"}]}"),
		Options:    []acp.PermissionOption{{ID: "allow-option", Kind: "allow_once"}, {ID: "reject-option", Kind: "reject_once"}},
	}
	policy := CodexPolicy{ApprovalPolicy: "on-request", TurnSandboxPolicy: "workspace-write"}
	resultCh := make(chan struct {
		decision acp.PermissionDecision
		err      error
	}, 1)
	go func() {
		decision, _, waitErr := awaitCodexACPAgentAccessApproval(context.Background(), provenance, request, "/workspace", policy)
		resultCh <- struct {
			decision acp.PermissionDecision
			err      error
		}{decision, waitErr}
	}()
	var approval AgentAccessApproval
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		rows, listErr := store.ListAgentAccessApprovals("project", "task")
		if listErr != nil {
			t.Fatal(listErr)
		}
		if len(rows) == 1 {
			approval = rows[0]
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if approval.RequestID == "" || approval.NativeOptionID != "allow-option" {
		t.Fatalf("native allow-once option was not bound exactly: %#v", approval)
	}
	if _, err := store.SettleAgentAccessApproval(AgentAccessApprovalResponse{RequestID: approval.RequestID, ExpectedRevision: approval.StateRevision, Decision: AgentAccessApprovalAllowOnce, Actor: "human:operator"}); err != nil {
		t.Fatal(err)
	}
	select {
	case result := <-resultCh:
		if result.err != nil || result.decision != acp.AllowOnce {
			t.Fatalf("native option result = %#v", result)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ACP approval did not return after exact option settlement")
	}

	cancelCtx, cancel := context.WithCancel(context.Background())
	cancelRequest := request
	cancelRequest.SessionID, cancelRequest.ToolCallID = "session-cancel", "call-cancel"
	cancelRequest.RawInput = []byte("{\"command\":\"rm -rf build\",\"cwd\":\"/workspace\"}")
	cancelRequest.Raw = []byte("{\"sessionId\":\"session-cancel\",\"toolCall\":{\"toolCallId\":\"call-cancel\",\"kind\":\"execute\",\"rawInput\":{\"command\":\"rm -rf build\",\"cwd\":\"/workspace\"}},\"options\":[{\"optionId\":\"allow-cancel\",\"kind\":\"allow_once\"}]}")
	cancelRequest.Options = []acp.PermissionOption{{ID: "allow-cancel", Kind: "allow_once"}}
	cancelCh := make(chan struct {
		decision acp.PermissionDecision
		err      error
	}, 1)
	go func() {
		decision, _, waitErr := awaitCodexACPAgentAccessApproval(cancelCtx, provenance, cancelRequest, "/workspace", policy)
		cancelCh <- struct {
			decision acp.PermissionDecision
			err      error
		}{decision, waitErr}
	}()
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		rows, listErr := store.ListAgentAccessApprovals("project", "task")
		if listErr != nil {
			t.Fatal(listErr)
		}
		if len(rows) == 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case result := <-cancelCh:
		if result.decision != acp.Cancelled || result.err == nil {
			t.Fatalf("cancel result = %#v", result)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancelled ACP approval did not return")
	}
	rows, err := store.ListAgentAccessApprovals("project", "task")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[1].State != AgentAccessApprovalCancelled {
		t.Fatalf("cancellation state = %#v", rows)
	}
}

func TestAgentAccessApprovalPreflightDeniesBeforePersistence(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	store, err := OpenRuntimeStore(filepath.Join(root, "state"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	privateFolders := []string{filepath.Join(workspace, "private")}
	policy := CodexPolicy{ApprovalPolicy: "on-request", TurnSandboxPolicy: "workspace-write"}

	codex := &codexLiveHandle{attemptID: "attempt-codex", policy: policy, privateFolders: privateFolders, cmd: &exec.Cmd{Dir: workspace}, runtimeStore: store}
	decision, handled := codex.awaitCodexAgentAccessApproval("item/commandExecution/requestApproval", "call-codex", []byte(fmt.Sprintf(`{"cwd":%q,"command":"rm -rf /"}`, workspace)))
	if !handled || decision.Decision != "reject" || !strings.Contains(decision.Reason, "filesystem root") {
		t.Fatalf("Codex catastrophic request was approvable: handled=%v decision=%#v", handled, decision)
	}
	for _, command := range []string{
		"diskutil eraseDisk APFS Tusker /dev/disk2",
		"mkfs.ext4 /dev/disk2",
	} {
		command := command
		t.Run("codex "+command, func(t *testing.T) {
			decision, handled := codex.awaitCodexAgentAccessApproval("item/commandExecution/requestApproval", "call-codex-device", []byte(fmt.Sprintf(`{"cwd":%q,"command":%q}`, workspace, command)))
			if !handled || decision.Decision != "reject" || !strings.Contains(decision.Reason, "protected raw device") {
				t.Fatalf("Codex device request reached an approvable path: handled=%v decision=%#v", handled, decision)
			}
		})
	}

	claude := &claudeLiveHandle{attemptID: "attempt-claude", policy: policy, privateFolders: privateFolders, cmd: &exec.Cmd{Dir: workspace}, runtimeStore: store}
	claudeDecision, handled := claude.awaitClaudeAgentAccessApproval("call-claude", map[string]any{
		"name":  "Bash",
		"input": map[string]any{"command": "rm -rf /", "cwd": workspace},
	})
	if !handled || claudeDecision.Decision != "reject" || !strings.Contains(claudeDecision.Reason, "filesystem root") {
		t.Fatalf("Claude catastrophic request was approvable: handled=%v decision=%#v", handled, claudeDecision)
	}
	for _, command := range []string{
		"diskutil eraseDisk APFS Tusker /dev/disk2",
		"mkfs.ext4 /dev/disk2",
	} {
		command := command
		t.Run("claude "+command, func(t *testing.T) {
			decision, handled := claude.awaitClaudeAgentAccessApproval("call-claude-device", map[string]any{
				"name": "Bash", "input": map[string]any{"command": command, "cwd": workspace},
			})
			if !handled || decision.Decision != "reject" || !strings.Contains(decision.Reason, "protected raw device") {
				t.Fatalf("Claude device request reached an approvable path: handled=%v decision=%#v", handled, decision)
			}
		})
	}

	acpRequest := acp.PermissionRequest{
		SessionID:  "session-acp",
		ToolCallID: "call-acp",
		ToolKind:   "execute",
		RawInput:   []byte(fmt.Sprintf(`{"command":"rm -rf /","cwd":%q}`, workspace)),
		Raw:        []byte(fmt.Sprintf(`{"sessionId":"session-acp","toolCall":{"toolCallId":"call-acp","kind":"execute","rawInput":{"command":"rm -rf /","cwd":%q}},"options":[{"optionId":"allow-option","kind":"allow_once"}]}`, workspace)),
		Options:    []acp.PermissionOption{{ID: "allow-option", Kind: "allow_once"}},
	}
	acpDecision, handled, acpErr := awaitCodexACPAgentAccessApproval(context.Background(), acpAttemptProvenance{ProjectID: "project", TaskID: "task", AttemptID: "attempt-acp", RuntimeStore: store, PrivateFolders: privateFolders}, acpRequest, workspace, policy)
	if !handled || acpDecision != acp.Reject || acpErr == nil || !strings.Contains(acpErr.Error(), "filesystem root") {
		t.Fatalf("ACP catastrophic request was approvable: handled=%v decision=%v err=%v", handled, acpDecision, acpErr)
	}
	for _, command := range []string{
		"diskutil eraseDisk APFS Tusker /dev/disk2",
		"mkfs.ext4 /dev/disk2",
	} {
		command := command
		t.Run("ACP "+command, func(t *testing.T) {
			rawInput := []byte(fmt.Sprintf(`{"command":%q,"cwd":%q}`, command, workspace))
			raw := []byte(fmt.Sprintf(`{"sessionId":"session-device","toolCall":{"toolCallId":"call-device","kind":"execute","rawInput":{"command":%q,"cwd":%q}},"options":[{"optionId":"allow-device","kind":"allow_once"}]}`, command, workspace))
			request := acp.PermissionRequest{SessionID: "session-device", ToolCallID: "call-device", ToolKind: "execute", RawInput: rawInput, Raw: raw, Options: []acp.PermissionOption{{ID: "allow-device", Kind: "allow_once"}}}
			decision, handled, err := awaitCodexACPAgentAccessApproval(context.Background(), acpAttemptProvenance{ProjectID: "project", TaskID: "task", AttemptID: "attempt-device", RuntimeStore: store, PrivateFolders: privateFolders}, request, workspace, policy)
			if !handled || decision != acp.Reject || err == nil || !strings.Contains(err.Error(), "protected raw device") {
				t.Fatalf("ACP device request reached an approvable path: handled=%v decision=%v err=%v", handled, decision, err)
			}
		})
	}
	rows, err := store.ListAgentAccessApprovals("project")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("mandatory denials created approval rows: %#v", rows)
	}

	home := filepath.Join(root, "home")
	outside := filepath.Join(root, "outside")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	for _, test := range []struct {
		name, command, want string
		private             []string
	}{
		{name: "project root", command: "rm -rf .", want: "project/worktree root", private: nil},
		{name: "home root", command: "rm -rf " + home, want: "home directory", private: nil},
		{name: "private folder", command: "rm -rf " + filepath.Join(workspace, "private"), want: "private folder", private: privateFolders},
		{name: "outside workspace", command: "rm -rf " + outside, want: "outside the prepared workspace", private: nil},
		{name: "ambiguous variable", command: `rm -rf "$TARGET"`, want: "ambiguous target", private: nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			if reason := destructiveApprovalBoundaryReason(test.command, workspace, workspace, nil, test.private); !strings.Contains(reason, test.want) {
				t.Fatalf("boundary reason=%q, want %q", reason, test.want)
			}
		})
	}
}
