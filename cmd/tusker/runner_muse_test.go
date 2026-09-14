package main

import (
	"strings"
	"testing"

	runnercore "tusker/internal/runner"
)

func TestAgentAccessMuse(t *testing.T) {
	argv, err := museCLIArgv(defaultMuseCLICommand(), CodexPolicy{ApprovalPolicy: "on-request", TurnSandboxPolicy: "workspace-write", TurnSandboxNetwork: boolPtr(true)}, "/tmp/project", "muse-model", "high")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"muse", "exec", "--json", "--workspace", "/tmp/project", "--approval-mode", "on-request", "--sandbox-network", "enabled", "--model", "muse-model", "--reasoning-effort", "high"}
	for _, value := range want {
		if !containsExact(argv, value) {
			t.Fatalf("native Muse argv missing %q: %#v", value, argv)
		}
	}
	if containsExact(argv, "--disable-sandbox") || containsExact(argv, "--yolo") {
		t.Fatalf("bounded Muse route widened its policy: %#v", argv)
	}

	controls := nativeAccessControls(runnercore.HarnessDefinition{Provider: "muse", Dialect: "muse"}, &AgentAccessV1{})
	var private, destructive bool
	for _, control := range controls {
		private = private || control.Control == runnercore.AccessPrivateReadDeny && control.Mechanism == runnercore.AccessUnsupported
		destructive = destructive || control.Control == runnercore.AccessDestructiveApproval && control.Mechanism == runnercore.AccessNativeSetting
	}
	if !private || !destructive {
		t.Fatalf("Muse support matrix lost required gaps: %#v", controls)
	}
}

func TestMuseNativeRoute(t *testing.T) {
	runner, command, err := runnerForName(string(RunnerMuse), defaultWorkflow())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := runner.(*MuseRunner); !ok || command != defaultMuseCLICommand() {
		t.Fatalf("direct Muse route = %T %q", runner, command)
	}
	if museCLIResumeArgv([]string{"muse", "exec", "--json"}, "session-1")[len(museCLIResumeArgv([]string{"muse", "exec", "--json"}, "session-1"))-1] != "session-1" {
		t.Fatal("direct Muse resume did not bind the requested session")
	}
}

func TestMuseCLIRejectsConfiguredPolicyOverrides(t *testing.T) {
	for _, tc := range []struct {
		name    string
		command string
	}{
		{name: "yolo", command: "muse exec --yolo"},
		{name: "yolo equals", command: "muse exec --yolo=true"},
		{name: "workspace separate", command: "muse exec --workspace /outside"},
		{name: "workspace equals", command: "muse exec --workspace=/outside"},
		{name: "approval separate", command: "muse exec --approval-mode on-request"},
		{name: "approval equals", command: "muse exec --approval-mode=on-request"},
		{name: "sandbox network separate", command: "muse exec --sandbox-network enabled"},
		{name: "sandbox network equals", command: "muse exec --sandbox-network=enabled"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := museCLIArgv(tc.command, CodexPolicy{}, "/workspace", "", ""); err == nil || !strings.Contains(err.Error(), "cannot override") {
				t.Fatalf("configured Muse policy override was admitted: %v", err)
			}
		})
	}
}

func TestMuseCLIOfflinePolicyCannotEnableNetwork(t *testing.T) {
	for _, command := range []string{"muse exec --sandbox-network enabled", "muse exec --sandbox-network=enabled"} {
		if _, err := museCLIArgv(command, CodexPolicy{TurnSandboxPolicy: "workspace-write", TurnSandboxNetwork: boolPtr(false)}, "/workspace", "", ""); err == nil || !strings.Contains(err.Error(), "cannot override") {
			t.Fatalf("offline Muse policy admitted configured network enable for %q: %v", command, err)
		}
	}
	argv, err := museCLIArgv(defaultMuseCLICommand(), CodexPolicy{TurnSandboxPolicy: "workspace-write", TurnSandboxNetwork: boolPtr(false)}, "/workspace", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if !containsPair(argv, "--sandbox-network", "restricted") || containsPair(argv, "--sandbox-network", "enabled") {
		t.Fatalf("offline Muse policy network mapping = %#v", argv)
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

func containsExact(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
