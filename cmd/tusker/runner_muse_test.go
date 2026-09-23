package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	runnercore "tusker/internal/runner"
)

func TestMuseDriverParityPreassignSession(t *testing.T) {
	dir := t.TempDir()
	req, err := runnerWrapperRequestForTest(dir)
	if err != nil {
		t.Fatal(err)
	}
	stub := filepath.Join(dir, "fake-wrapper")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TUSKER_WRAPPER_EXE", stub)
	req.Start.Command = "muse exec --json"
	req.Start.CommandArgv = []string{"muse", "exec", "--json"}
	for _, explicit := range []string{"", "fixture-session"} {
		argv := append([]string(nil), req.Start.CommandArgv...)
		if explicit != "" {
			argv = append(argv, "--session-id", explicit)
		}
		req.Start.CommandArgv = argv
		result, err := (&MuseRunner{}).Start(context.Background(), req.Start)
		if err != nil {
			t.Fatal(err)
		}
		if result.SessionRef == "" || (explicit != "" && result.SessionRef != explicit) {
			t.Fatalf("session = %q", result.SessionRef)
		}
		stored, err := readRunnerWrapperRequest(req.Start.StatusPath + ".wrapper-request.json")
		if err != nil {
			t.Fatal(err)
		}
		if stored.Start.NativeSessionID != result.SessionRef || museCLIArgvSession(stored.Start.CommandArgv) != result.SessionRef {
			t.Fatalf("request lost session: %#v", stored.Start)
		}
	}
}

func TestMuseDriverParityTerminal(t *testing.T) {
	for _, tc := range []struct {
		raw     string
		outcome AttemptOutcome
		code    RunFailureReasonCode
	}{
		{`{"payload_type":"run.terminal.completed"}`, AttemptOutcomeSucceeded, ""},
		{`{"payload_type":"run.terminal.cancelled"}`, AttemptOutcomeCancelled, RunFailureCancelled},
		{`{"payload_type":"run.terminal.failed","reason":"rate limit exceeded"}`, AttemptOutcomeFailed, RunFailureUsageLimit},
		{`{"payload_type":"run.terminal.failed","reason":"login required"}`, AttemptOutcomeFailed, RunFailureAuthExpired},
		{`{"payload_type":"run.terminal.failed","reason":"approval denied"}`, AttemptOutcomeFailed, RunFailurePermissionDenied},
		{`{"payload_type":"run.terminal.failed","reason":"provider broke"}`, AttemptOutcomeFailed, RunFailureProviderError},
		{`{"payload_type":"unrecognized"}`, AttemptOutcomeUnknown, RunFailureOutcomeUnknown},
	} {
		outcome, reason, _ := classifyMuseCLIOutput(tc.raw)
		if outcome != tc.outcome || museFailureReasonCode(outcome, reason) != tc.code {
			t.Fatalf("%s: %s %s", tc.raw, outcome, reason)
		}
	}
}

func TestMuseDriverParityInterrupt(t *testing.T) {
	// Exercise the shared cancellation transition after a Muse child exits;
	// process-group signalling itself is covered by the shared runner tests.
	store, req := setupRunnerWrapperRuntime(t)
	run, err := store.FindRun(req.Start.RecordID)
	if err != nil || run == nil {
		t.Fatalf("run = %#v, %v", run, err)
	}
	run.Runner = string(RunnerMuse)
	run.SessionRef = "fixture-session"
	if err := store.UpsertRun(*run); err != nil {
		t.Fatal(err)
	}
	if err := interruptRunProcess(store, run, false); err != nil {
		t.Fatal(err)
	}
	if run.LeaseState != string(LeaseStateInterrupted) || run.AttemptOutcome != string(AttemptOutcomeCancelled) || run.SessionRef != "fixture-session" {
		t.Fatalf("interrupt = %#v", run)
	}
	argv := museCLIResumeArgv([]string{"muse", "exec", "--json"}, run.SessionRef)
	if museCLIArgvSession(argv) != run.SessionRef {
		t.Fatalf("resume argv = %#v", argv)
	}
}

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
