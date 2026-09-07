package runner

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestHarnessRegistrationConformance(t *testing.T) {
	workspace, bin := fakeCodex(t, "")
	definition := HarnessDefinition{ID: "first", Provider: "codex", Transport: TransportCLI, Dialect: "codex", Executable: "codex", Args: []string{"exec", "--json", "-"}, SchemaVersion: 1}
	first, err := Prepare(context.Background(), definition, RunInput{Workspace: workspace, Preset: PresetReadOnly, SearchPath: bin})
	if err != nil {
		t.Fatal(err)
	}
	definition.ID = "second-compatible"
	second, err := Prepare(context.Background(), definition, RunInput{Workspace: workspace, Preset: PresetReadOnly, SearchPath: bin})
	if err != nil {
		t.Fatal(err)
	}
	if first.Argv[0] != second.Argv[0] || first.EffectivePolicy != second.EffectivePolicy {
		t.Fatalf("compatible registration changed execution behavior: %#v %#v", first, second)
	}
	definition.Dialect = "unknown"
	if _, err := Prepare(context.Background(), definition, RunInput{Workspace: workspace, Preset: PresetReadOnly, SearchPath: bin}); err == nil {
		t.Fatal("unknown CLI dialect was admitted")
	}
}

func TestHarnessAdmissionConformance(t *testing.T) {
	workspace := t.TempDir()
	definition := HarnessDefinition{ID: "codex", Provider: "codex", Transport: TransportCLI, Dialect: "codex", Executable: "codex", Args: []string{"exec", "--json", "-"}, SchemaVersion: 1}
	_, err := Prepare(context.Background(), definition, RunInput{Workspace: workspace, Preset: PresetReadOnly, SearchPath: t.TempDir()})
	var admissionErr *AdmissionError
	if !errors.As(err, &admissionErr) || admissionErr.Code != "runtime_missing" {
		t.Fatalf("missing executable result = %#v, %v", admissionErr, err)
	}

	badDir := t.TempDir()
	writeExecutable(t, filepath.Join(badDir, "codex"), "#!/bin/sh\nexit 7\n")
	_, goodDir := fakeCodex(t, "")
	prepared, err := Prepare(context.Background(), definition, RunInput{Workspace: workspace, Preset: PresetReadOnly, SearchPath: badDir + string(os.PathListSeparator) + goodDir})
	if err != nil {
		t.Fatal(err)
	}
	resolvedGoodDir, _ := filepath.EvalSymlinks(goodDir)
	if filepath.Dir(prepared.Executable) != resolvedGoodDir {
		t.Fatalf("selected %s instead of healthy second candidate", prepared.Executable)
	}

	definition.Args = append(definition.Args, "--sandbox", "danger-full-access")
	_, err = Prepare(context.Background(), definition, RunInput{Workspace: workspace, Preset: PresetReadOnly, SearchPath: goodDir})
	if !errors.As(err, &admissionErr) || admissionErr.Code != "policy_conflict" {
		t.Fatalf("policy conflict = %#v, %v", admissionErr, err)
	}
}

func TestHarnessPolicyConformance(t *testing.T) {
	workspace, bin := fakeCodex(t, "")
	definition := HarnessDefinition{ID: "codex", Provider: "codex", Transport: TransportCLI, Dialect: "codex", Executable: "codex", Args: []string{"exec", "--json", "-"}, SchemaVersion: 1}
	for _, tc := range []struct {
		preset           PermissionPreset
		sandbox, network string
	}{
		{PresetReadOnly, "read-only", ""},
		{PresetWorkspaceOffline, "workspace-write", "sandbox_workspace_write.network_access=false"},
		{PresetWorkspaceNetwork, "workspace-write", "sandbox_workspace_write.network_access=true"},
	} {
		prepared, err := Prepare(context.Background(), definition, RunInput{Workspace: workspace, Preset: tc.preset, SearchPath: bin})
		if err != nil {
			t.Fatal(err)
		}
		joined := strings.Join(prepared.Argv, " ")
		for _, required := range []string{"--ignore-user-config", `approval_policy="never"`, "--sandbox " + tc.sandbox} {
			if !strings.Contains(joined, required) {
				t.Fatalf("%s argv omitted %q: %s", tc.preset, required, joined)
			}
		}
		if tc.network != "" && !strings.Contains(joined, tc.network) {
			t.Fatalf("%s argv omitted network policy: %s", tc.preset, joined)
		}
	}

	claude := HarnessDefinition{ID: "claude", Provider: "claude", Transport: TransportCLI, Dialect: "claude", Executable: fakeClaude(t), SchemaVersion: 1}
	readOnly, err := Prepare(context.Background(), claude, RunInput{Workspace: workspace, Preset: PresetReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(readOnly.Argv, " ")
	if strings.Contains(joined, "bypassPermissions") || !strings.Contains(joined, "--permission-mode plan") || !strings.Contains(joined, "--permission-prompts none") {
		t.Fatalf("bounded Claude argv is unsafe: %s", joined)
	}
	_, err = Prepare(context.Background(), claude, RunInput{Workspace: workspace, Preset: PresetWorkspaceOffline})
	var admissionErr *AdmissionError
	if !errors.As(err, &admissionErr) || admissionErr.Code != "policy_unenforceable" {
		t.Fatalf("bounded Claude workspace policy = %#v, %v", admissionErr, err)
	}
}

func TestHarnessEventConformance(t *testing.T) {
	kind, reason, session := classifyCLIResult("codex", `{"type":"thread.started","thread_id":"s1"}`+"\n"+`{"type":"turn.completed"}`)
	if kind != EventCompleted || reason != "" || session != "s1" {
		t.Fatalf("completed result = %s %q %q", kind, reason, session)
	}
	if kind, _, _ := classifyCLIResult("codex", `{"type":"item.completed"}`); kind != EventFailed {
		t.Fatalf("missing final = %s", kind)
	}
	if kind, _, _ := classifyCLIResult("claude", `{"type":"result","is_error":true}`); kind != EventFailed {
		t.Fatalf("Claude error = %s", kind)
	}
	if kind, reason, _ := classifyCLIResult("codex", strings.Repeat("x", maxProtocolFrame+1)); kind != EventFailed || reason != "protocol_frame_too_large" {
		t.Fatalf("oversized frame = %s %q", kind, reason)
	}
}

func TestHarnessSupervisionConformance(t *testing.T) {
	workspace, bin := fakeCodex(t, "sleep 30 & wait")
	definition := HarnessDefinition{ID: "codex", Provider: "codex", Transport: TransportCLI, Dialect: "codex", Executable: "codex", Args: []string{"exec", "--json", "-"}, SchemaVersion: 1}
	prepared, err := Prepare(context.Background(), definition, RunInput{Workspace: workspace, Preset: PresetReadOnly, SearchPath: bin + string(os.PathListSeparator) + "/bin", Deadline: 100 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	receipt, err := Execute(context.Background(), prepared, nil)
	if err == nil || receipt.Reason != "timeout" {
		t.Fatalf("timeout receipt = %#v, %v", receipt, err)
	}
	if time.Since(started) > 3*time.Second {
		t.Fatalf("process tree cleanup exceeded grace: %s", time.Since(started))
	}

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	prepared.Deadline = time.Second
	receipt, err = Execute(cancelled, prepared, nil)
	if err == nil || receipt.Outcome != EventCancelled {
		t.Fatalf("pre-cancel receipt = %#v, %v", receipt, err)
	}
}

func TestHarnessConformanceReport(t *testing.T) {
	workspace, bin := fakeCodex(t, "")
	definition := HarnessDefinition{ID: "codex", Provider: "codex", Transport: TransportCLI, Dialect: "codex", Executable: "codex", Args: []string{"exec", "--json", "-"}, SchemaVersion: 1}
	report, err := Conformance(context.Background(), definition, RunInput{Workspace: workspace, Preset: PresetReadOnly, SearchPath: bin}, false)
	if err != nil {
		t.Fatal(err)
	}
	if report.Schema != ConformanceSchema || report.Ready || report.Live || report.Cases[len(report.Cases)-1].Result != CaseNotRun {
		t.Fatalf("offline report = %#v", report)
	}
	report, err = Conformance(context.Background(), definition, RunInput{Workspace: workspace, Preset: PresetReadOnly, SearchPath: bin}, true)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Ready || report.ValidUntil == nil || report.HostOS != runtime.GOOS {
		t.Fatalf("live report = %#v", report)
	}
}

func TestHarnessFunctionalExercises(t *testing.T) {
	workspace, bin := fakeCodex(t, `printf '%s\n' '{"type":"item.completed","item":{"text":"TUSKER_EXERCISE_TIMER"}}' '{"type":"turn.completed"}'`)
	definition := HarnessDefinition{ID: "codex", Provider: "codex", Transport: TransportCLI, Dialect: "codex", Executable: "codex", Args: []string{"exec", "--json", "-"}, SchemaVersion: 1}
	report, err := Conformance(context.Background(), definition, RunInput{Workspace: workspace, Preset: PresetReadOnly, SearchPath: bin, Exercise: "timer"}, true)
	if err != nil || !report.Ready {
		t.Fatalf("timer exercise: report=%#v err=%v", report, err)
	}
	if got := report.Cases[len(report.Cases)-1]; got.ID != "functional_exercise" || got.Result != CasePass {
		t.Fatalf("functional exercise case = %#v", got)
	}
	if shellQuote("a'b") != `'a'"'"'b'` {
		t.Fatalf("unsafe script quoting: %s", shellQuote("a'b"))
	}
}

func fakeCodex(t *testing.T, body string) (string, string) {
	t.Helper()
	workspace, bin := t.TempDir(), t.TempDir()
	if body == "" {
		body = `printf '%s\n' '{"type":"thread.started","thread_id":"test-session"}' '{"type":"turn.completed"}'`
	}
	writeExecutable(t, filepath.Join(bin, "codex"), "#!/bin/sh\nif [ \"$1\" = --version ]; then echo 'codex-test 1'; exit 0; fi\nif [ \"$1\" = login ]; then echo 'Logged in'; exit 0; fi\n"+body+"\n")
	return workspace, bin
}

func fakeClaude(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "claude")
	writeExecutable(t, path, "#!/bin/sh\nif [ \"$1\" = --version ]; then echo 'claude-test 1'; exit 0; fi\nif [ \"$1\" = auth ]; then echo '{\"loggedIn\":true}'; exit 0; fi\nprintf '%s\\n' '{\"type\":\"result\",\"is_error\":false}'\n")
	return path
}

func writeExecutable(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o755); err != nil {
		t.Fatal(err)
	}
}
