package runner

import (
	"strings"
	"testing"
)

func TestAgentAccessNative(t *testing.T) {
	workspace := t.TempDir()
	access := &AgentAccessV1{Schema: "tusker.agent-access/v1", Mode: "work_in_projects", Network: true, DestructiveActions: "ask"}
	effective := EffectivePolicy{Preset: PresetWorkspaceNetwork, Filesystem: "workspace-write", Network: true, Approvals: "ask", Workspace: workspace}
	report := &ResolvedAccess{Requested: *access, Effective: effective, State: "ready", Controls: []ControlSupport{{Control: AccessWorkspaceWrite, Mechanism: AccessNativeSetting}}, Fingerprint: "sha256:test"}
	policy, argv, err := compilePolicy(HarnessDefinition{ID: "codex", Provider: "codex", Transport: TransportCLI, Dialect: "codex", Executable: "codex", Args: []string{"exec"}}, RunInput{Workspace: workspace, Preset: PresetWorkspaceNetwork, Access: access, ResolvedAccess: report})
	if err != nil {
		t.Fatal(err)
	}
	if policy != effective || !containsPair(argv, "--sandbox", "workspace-write") || !contains(argv, `approval_policy="on-request"`) || !contains(argv, "sandbox_workspace_write.network_access=true") {
		t.Fatalf("Codex native mapping = policy=%#v argv=%#v", policy, argv)
	}

	musePolicy, museArgv, err := compilePolicy(HarnessDefinition{ID: "muse", Provider: "muse", Transport: TransportCLI, Dialect: "muse", Executable: "muse", Args: []string{"exec"}}, RunInput{Workspace: workspace, Preset: PresetReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	if musePolicy.Filesystem != "read-only" || !containsPair(museArgv, "--workspace", workspace) || !contains(museArgv, "--disable-write") || !contains(museArgv, "--disable-shell") || !contains(museArgv, "--sandbox-network") {
		t.Fatalf("Muse native mapping = policy=%#v argv=%#v", musePolicy, museArgv)
	}
}

func TestAgentAccessDestructive(t *testing.T) {
	workspace := t.TempDir()
	d := HarnessDefinition{ID: "codex", Provider: "codex", Transport: TransportCLI, Dialect: "codex", Executable: "codex", Args: []string{"exec"}}
	_, argv, err := compilePolicy(d, RunInput{Workspace: workspace, Preset: PresetWorkspaceOffline, ResolvedAccess: &ResolvedAccess{State: "ready", Effective: EffectivePolicy{Preset: PresetWorkspaceOffline, Filesystem: "workspace-write", Approvals: "ask", Workspace: workspace}}})
	if err != nil {
		t.Fatal(err)
	}
	if !contains(argv, `approval_policy="on-request"`) {
		t.Fatalf("ask policy was silently changed to deny: %#v", argv)
	}
	_, _, err = compilePolicy(d, RunInput{Workspace: workspace, Preset: PresetWorkspaceOffline, ResolvedAccess: &ResolvedAccess{State: "unsupported", Effective: EffectivePolicy{Preset: PresetWorkspaceOffline, Filesystem: "workspace-write"}}})
	if err == nil || !strings.Contains(err.Error(), "policy_unenforceable") {
		t.Fatalf("unsupported access was admitted: %v", err)
	}

	if got, reason, session := classifyCLIResult("muse", `{"stream":{"id":"sess"},"payload_type":"run.terminal.cancelled"}`); got != EventCancelled || reason == "" || session != "sess" {
		t.Fatalf("Muse cancellation = %v %q %q", got, reason, session)
	}
}

func TestDevinACPPolicyCompiler(t *testing.T) {
	workspace := t.TempDir()
	definition := HarnessDefinition{ID: "devin", Provider: "devin", Transport: TransportACP, Executable: "devin", Args: []string{"acp"}, NativeContainment: true}
	policy, argv, err := compilePolicy(definition, RunInput{Workspace: workspace, Preset: PresetWorkspaceNetwork, Model: "swe-1-6-slow"})
	if err != nil || !policy.Network || !containsPair(argv, "--model", "swe-1-6-slow") || len(argv) < 2 || argv[0] != "--sandbox" || argv[1] != "acp" {
		t.Fatalf("Devin mapping policy=%#v argv=%#v err=%v", policy, argv, err)
	}
	for _, preset := range []PermissionPreset{PresetReadOnly, PresetWorkspaceOffline, PresetDangerFullAccess} {
		if _, _, err := compilePolicy(definition, RunInput{Workspace: workspace, Preset: preset, Model: "swe-1-6-slow"}); err == nil || !strings.Contains(err.Error(), "policy_unenforceable") {
			t.Fatalf("Devin admitted unsupported preset %s: %v", preset, err)
		}
	}
}

func TestMusePolicyArgsCannotOverrideRequestedPolicy(t *testing.T) {
	workspace := t.TempDir()
	for _, tc := range []struct {
		name string
		args []string
	}{
		{name: "yolo", args: []string{"--yolo"}},
		{name: "yolo equals", args: []string{"--yolo=true"}},
		{name: "workspace separate", args: []string{"--workspace", "/outside"}},
		{name: "workspace equals", args: []string{"--workspace=/outside"}},
		{name: "approval separate", args: []string{"--approval-mode", "on-request"}},
		{name: "approval equals", args: []string{"--approval-mode=on-request"}},
		{name: "sandbox network separate", args: []string{"--sandbox-network", "enabled"}},
		{name: "sandbox network equals", args: []string{"--sandbox-network=enabled"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := HarnessDefinition{ID: "muse", Provider: "muse", Transport: TransportCLI, Dialect: "muse", Executable: "muse", Args: append([]string{"exec"}, tc.args...)}
			_, _, err := compilePolicy(d, RunInput{Workspace: workspace, Preset: PresetReadOnly})
			if err == nil || !strings.Contains(err.Error(), "policy_conflict") {
				t.Fatalf("Muse policy override was admitted: %v", err)
			}
		})
	}
}

func TestMuseOfflinePolicyCannotEnableNetwork(t *testing.T) {
	workspace := t.TempDir()
	d := HarnessDefinition{ID: "muse", Provider: "muse", Transport: TransportCLI, Dialect: "muse", Executable: "muse", Args: []string{"exec"}}
	_, argv, err := compilePolicy(d, RunInput{Workspace: workspace, Preset: PresetWorkspaceOffline})
	if err != nil {
		t.Fatal(err)
	}
	if !containsPair(argv, "--sandbox-network", "restricted") || containsPair(argv, "--sandbox-network", "enabled") {
		t.Fatalf("Muse offline policy network mapping = %#v", argv)
	}
}

func containsPair(values []string, flag, value string) bool {
	for i := 0; i+1 < len(values); i++ {
		if values[i] == flag && values[i+1] == value {
			return true
		}
	}
	return false
}
