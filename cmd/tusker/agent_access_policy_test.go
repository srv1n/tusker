package main

import (
	"os/exec"
	"path/filepath"
	"testing"

	runnercore "tusker/internal/runner"
)

func TestAccessProfilePublishesFixedCommandPolicy(t *testing.T) {
	workspace := t.TempDir()
	profile := RunnerProfileDefinition{Access: &AgentAccessV1{
		Schema: agentAccessSchemaV1, Mode: accessModeProjects, Network: true,
		DestructiveActions: "ask", Folders: []AgentAccessFolder{}, PrivateFolders: []string{},
	}}
	resolved, err := resolveAccess(profile, nil, AgentAccessResolutionContext{
		Workspace: workspace,
		Controls: []runnercore.ControlSupport{
			{Control: runnercore.AccessWorkspaceWrite, Mechanism: runnercore.AccessNativeSetting},
			{Control: runnercore.AccessNetwork, Mechanism: runnercore.AccessNativeSetting},
			{Control: runnercore.AccessDestructiveApproval, Mechanism: runnercore.AccessNativeHook},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.CommandPolicy == nil {
		t.Fatal("access resolution omitted command policy")
	}
	if got := resolved.CommandPolicy.Destructive; got != runnercore.CommandAsk {
		t.Fatalf("destructive behavior=%q, want ask", got)
	}
	if got := resolved.CommandPolicy.Routine; got != runnercore.CommandAutomatic {
		t.Fatalf("routine behavior=%q, want automatic", got)
	}
	if got := resolved.CommandPolicy.Catastrophic; got != runnercore.CommandBlock {
		t.Fatalf("catastrophic behavior=%q, want block", got)
	}
	if len(resolved.CommandPolicy.Rules) != 6 {
		t.Fatalf("fixed policy rules=%d, want 6", len(resolved.CommandPolicy.Rules))
	}

	review := profile
	review.Access = &AgentAccessV1{Schema: agentAccessSchemaV1, Mode: accessModeReview, Network: true, DestructiveActions: "ask", Folders: []AgentAccessFolder{}, PrivateFolders: []string{filepath.Join(workspace, "private")}}
	reviewResolved, err := resolveAccess(review, nil, AgentAccessResolutionContext{
		Workspace: workspace,
		Controls: []runnercore.ControlSupport{
			{Control: runnercore.AccessWorkspaceWrite, Mechanism: runnercore.AccessNativeSetting},
			{Control: runnercore.AccessNetwork, Mechanism: runnercore.AccessNativeSetting},
			{Control: runnercore.AccessDestructiveApproval, Mechanism: runnercore.AccessNativeHook},
			{Control: runnercore.AccessPrivateReadDeny, Mechanism: runnercore.AccessNativeHook},
			{Control: runnercore.AccessPrivateWriteDeny, Mechanism: runnercore.AccessNativeHook},
			{Control: runnercore.AccessReviewOnly, Mechanism: runnercore.AccessNativeSetting},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if reviewResolved.State != "unsupported" || len(reviewResolved.Issues) == 0 {
		t.Fatalf("review-only network was silently changed: %#v", reviewResolved)
	}
	if got := runnercore.EvaluateCommandPolicy(*reviewResolved.CommandPolicy, runnercore.CommandPolicyRequest{Mutating: true, ReviewOnly: true}); got.Behavior != runnercore.CommandBlock {
		t.Fatalf("review write behavior=%#v, want block", got)
	}
}

func TestAccessCommandPolicyBlocksDestructiveCallbackBeforeApproval(t *testing.T) {
	workspace := t.TempDir()
	policy := CodexPolicy{
		ApprovalPolicy: "on-request", ThreadSandbox: "workspace-write", TurnSandboxPolicy: "workspace-write",
		CommandPolicy: runnercore.NewCommandPolicy(false, "deny"),
	}
	handle := &codexLiveHandle{cmd: commandPolicyTestCommand(workspace), policy: policy}
	decision, handled := handle.awaitCodexAgentAccessApproval("execCommandApproval", "request-1", []byte(`{"command":"git reset --hard HEAD","cwd":"`+workspace+`"}`))
	if !handled || decision.Decision != "reject" || decision.Reason == "" {
		t.Fatalf("destructive block bypassed shared policy: handled=%v decision=%#v", handled, decision)
	}
}

func TestAgentAccessEditorNoPrompt(t *testing.T) {
	access := newAgentAccessDefaults()
	policy := CodexPolicy{
		ApprovalPolicy: "on-request", ThreadSandbox: "workspace-write", TurnSandboxPolicy: "workspace-write",
		CommandPolicy: runnercore.NewCommandPolicy(false, access.DestructiveActions),
	}
	handle := &codexLiveHandle{cmd: commandPolicyTestCommand(t.TempDir()), policy: policy}
	decision, handled := handle.awaitCodexAgentAccessApproval("execCommandApproval", "default-deny", []byte(`{"command":"git reset --hard HEAD"}`))
	if !handled || decision.Decision != "reject" || handle.runtimeStore != nil {
		t.Fatalf("default destructive request prompted or persisted: handled=%v decision=%#v store=%v", handled, decision, handle.runtimeStore)
	}
}

func commandPolicyTestCommand(workspace string) *exec.Cmd {
	return &exec.Cmd{Dir: workspace}
}
