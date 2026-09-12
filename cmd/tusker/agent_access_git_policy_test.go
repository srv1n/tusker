package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"testing"

	"tusker/internal/acp"
)

func TestGitPolicyCallbacksSeparateWorkspaceWritesFromDestructiveApproval(t *testing.T) {
	workspace := t.TempDir()
	routine := []string{"git add result.txt", "git commit -m save"}
	redirected := []string{
		"git -C /other-repo reset --hard HEAD",
		"git --git-dir=/other-repo/.git --work-tree=/other-repo reset --hard HEAD",
		"git --work-tree /other-repo add result.txt",
	}
	modes := []struct {
		name       string
		policy     CodexPolicy
		codexMode  CodexACPMode
		allowWrite bool
	}{
		{name: "project-write", policy: CodexPolicy{ApprovalPolicy: "on-request", ThreadSandbox: "workspace-write", TurnSandboxPolicy: "workspace-write"}, codexMode: CodexACPModeWorkspaceWrite, allowWrite: true},
		{name: "review", policy: CodexPolicy{ApprovalPolicy: "never", ThreadSandbox: "read-only", TurnSandboxPolicy: "read-only"}, codexMode: CodexACPModeReadOnly},
	}

	for _, mode := range modes {
		t.Run("codex/"+mode.name, func(t *testing.T) {
			handle := &codexLiveHandle{cmd: &exec.Cmd{Dir: workspace}, policy: mode.policy, pending: map[string]chan codexRPCResponse{}}
			for index, command := range routine {
				params := []byte(fmt.Sprintf(`{"cwd":%q,"command":%q}`, workspace, command))
				decision := handle.evaluateCommandApproval(params)
				if decision.Mutating {
					t.Fatalf("routine Git write classified as destructive: command=%q decision=%#v", command, decision)
				}
				handle.stdin = &recordingWriteCloser{}
				handle.handleServerRequest("execCommandApproval", fmt.Sprintf("routine-%d", index), params)
				result := lastCodexCallbackResult(t, handle.stdin.(*recordingWriteCloser))
				if mode.allowWrite {
					if result["decision"] != "approved" {
						t.Fatalf("project-write rejected routine Git write: command=%q result=%#v", command, result)
					}
				} else if result["decision"] != "denied" {
					t.Fatalf("review accepted routine Git write: command=%q result=%#v", command, result)
				}
			}
			for index, command := range redirected {
				params := []byte(fmt.Sprintf(`{"cwd":%q,"command":%q}`, workspace, command))
				handle.stdin = &recordingWriteCloser{}
				handle.handleServerRequest("execCommandApproval", fmt.Sprintf("redirected-%d", index), params)
				result := lastCodexCallbackResult(t, handle.stdin.(*recordingWriteCloser))
				if result["decision"] != "denied" {
					t.Fatalf("redirected Git repository reached callback approval: command=%q result=%#v", command, result)
				}
			}
		})

		t.Run("claude/"+mode.name, func(t *testing.T) {
			for index, command := range append(append([]string{}, routine...), redirected...) {
				handle := &claudeLiveHandle{cmd: &exec.Cmd{Dir: workspace}, policy: mode.policy}
				handle.stdin = &recordingWriteCloser{}
				handle.handleControlRequest(map[string]any{
					"request_id": fmt.Sprintf("claude-%d", index),
					"request":    map[string]any{"subtype": "can_use_tool", "name": "Bash", "input": map[string]any{"command": command, "cwd": workspace}},
				})
				response := lastClaudeCallbackResponse(t, handle.stdin.(*recordingWriteCloser))
				if response["behavior"] != "allow" && response["behavior"] != "deny" {
					t.Fatalf("invalid Claude callback response: command=%q response=%#v", command, response)
				}
				if index >= len(routine) && response["behavior"] != "deny" {
					t.Fatalf("redirected Claude Git repository reached callback approval: command=%q response=%#v", command, response)
				}
				if index < len(routine) && !mode.allowWrite && response["behavior"] != "deny" {
					t.Fatalf("review accepted routine Claude Git write: command=%q response=%#v", command, response)
				}
			}
		})

		t.Run("codex-acp/"+mode.name, func(t *testing.T) {
			for index, command := range append(append([]string{}, routine...), redirected...) {
				request := gitACPCallbackRequest(t, workspace, command, fmt.Sprintf("acp-%d", index))
				decision, err := evaluateCodexACPTransportPermission(context.Background(), nil, acpAttemptProvenance{AttemptID: "attempt-git-policy"}, request, workspace, mode.codexMode, mode.policy)
				if err != nil && index < len(routine) && mode.allowWrite {
					t.Fatalf("project-write ACP routine Git write errored: command=%q err=%v", command, err)
				}
				if index < len(routine) && mode.allowWrite && decision != acp.AllowOnce {
					t.Fatalf("project-write ACP routine Git write was not allowed: command=%q decision=%q err=%v", command, decision, err)
				}
				if index < len(routine) && !mode.allowWrite && decision != acp.Reject {
					t.Fatalf("review ACP routine Git write was allowed: command=%q decision=%q err=%v", command, decision, err)
				}
				if index >= len(routine) && decision != acp.Reject {
					t.Fatalf("redirected ACP Git repository reached callback approval: command=%q decision=%q err=%v", command, decision, err)
				}
			}
		})
	}
}

func lastCodexCallbackResult(t *testing.T, writer *recordingWriteCloser) map[string]any {
	t.Helper()
	if len(writer.writes) == 0 {
		t.Fatal("Codex callback emitted no response")
	}
	var envelope struct {
		Result map[string]any `json:"result"`
	}
	if err := json.Unmarshal(writer.writes[len(writer.writes)-1], &envelope); err != nil {
		t.Fatalf("decode Codex callback response: %v", err)
	}
	return envelope.Result
}

func lastClaudeCallbackResponse(t *testing.T, writer *recordingWriteCloser) map[string]any {
	t.Helper()
	if len(writer.writes) == 0 {
		t.Fatal("Claude callback emitted no response")
	}
	var envelope struct {
		Response struct {
			Response map[string]any `json:"response"`
		} `json:"response"`
	}
	if err := json.Unmarshal(writer.writes[len(writer.writes)-1], &envelope); err != nil {
		t.Fatalf("decode Claude callback response: %v", err)
	}
	return envelope.Response.Response
}

func gitACPCallbackRequest(t *testing.T, workspace, command, id string) acp.PermissionRequest {
	t.Helper()
	rawInput, err := json.Marshal(map[string]any{"command": command, "cwd": workspace})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(map[string]any{
		"sessionId": "session-git-policy",
		"toolCall":  map[string]any{"toolCallId": id, "kind": "execute", "rawInput": json.RawMessage(rawInput)},
		"options":   []map[string]any{{"optionId": "allow-once", "kind": "allow_once"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return acp.PermissionRequest{
		SessionID: "session-git-policy", ToolCallID: id, ToolKind: "execute",
		RawInput: rawInput, Raw: raw,
		Options: []acp.PermissionOption{{ID: "allow-once", Kind: "allow_once"}},
	}
}
