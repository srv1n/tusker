package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	runnercore "tusker/internal/runner"
)

func TestHarnessConformanceReport(t *testing.T) {
	vault := automationTestVault(t)
	bin := t.TempDir()
	writeText(filepath.Join(bin, "codex"), "#!/bin/sh\ncase \"$1\" in --version) echo 'codex-test 1';; login) echo 'Logged in';; *) printf '%s\\n' '{\"type\":\"turn.completed\"}';; esac\n")
	if err := os.Chmod(filepath.Join(bin, "codex"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	code, report, err := runRunnerConformance(Args{"vault": vault, "harness": "codex_exec", "preset": "read-only"})
	if err != nil || code != 0 {
		t.Fatalf("conformance: code=%d err=%v report=%#v", code, err, report)
	}
	if report.Schema != runnercore.ConformanceSchema || report.Ready || len(report.Cases) == 0 {
		t.Fatalf("offline report = %#v", report)
	}
	if _, err := os.Stat(conformanceReportCachePath(DefaultStateRoot(), report)); err != nil {
		t.Fatalf("cached report: %v", err)
	}
	manifest, err := buildCapabilitiesManifest(nil, "")
	if err != nil {
		t.Fatal(err)
	}
	command, ok := capabilityCommandNamed(manifest.Commands, "runner conformance")
	if !ok || !containsString(command.Flags, "--harness") || !containsString(command.Flags, "--live") {
		t.Fatalf("runner conformance capability = %#v", command)
	}
}

func TestMuseConformanceAdmission(t *testing.T) {
	vault := automationTestVault(t)
	command := filepath.Join(t.TempDir(), "muse")
	if err := os.WriteFile(command, []byte("#!/bin/sh\necho 'Muse Code 1'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	setGlobalProfileForTest(t, "muse-admission", map[string]any{
		"harness": "muse", "model": "muse-manual", "effort": "high", "permission_preset": "danger-full-access",
		"command": command + " exec --json", "sandbox": map[string]any{"mode": "danger-full-access", "network": true},
		"subagents": map[string]any{"allowed": false, "max_concurrent": 0},
	})
	selected := ResolvedRunnerProfile{Name: "muse-admission", Definition: RunnerProfileDefinition{Harness: "muse", PermissionPreset: "danger-full-access"}}
	workspace := v7RepoRoot(vault)
	_, err := preparedRunnerForDispatch(vault, RunnerMuse, "", selected, CodexPolicy{}, workspace, runnerCommandSearchPath())
	var admission *runnercore.AdmissionError
	if !errors.As(err, &admission) || admission.Code != "live_check_missing" || !strings.Contains(admission.Remedy, "tusker runner test muse-admission --preset danger-full-access --external-containment --live") {
		t.Fatalf("missing live check = %#v", err)
	}
	definition, model, effort, err := conformanceHarnessDefinition(vault, selected.Name)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := runnercore.Prepare(context.Background(), definition, runnercore.RunInput{Workspace: workspace, Preset: runnercore.PresetDangerFullAccess, Model: model, Effort: effort, SearchPath: runnerCommandSearchPath(), VerifiedAuth: true})
	if err != nil {
		t.Fatal(err)
	}
	validUntil := time.Now().Add(time.Hour)
	if err := saveConformanceReport(DefaultStateRoot(), runnercore.ConformanceReport{HarnessID: selected.Name, Preset: runnercore.PresetDangerFullAccess, Live: true, Ready: true, ValidUntil: &validUntil, ExecutableIdentity: prepared.ExecutableIdentity, Cases: []runnercore.ConformanceCase{{ID: "deny_list", Result: runnercore.CasePass}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := preparedRunnerForDispatch(vault, RunnerMuse, "", selected, CodexPolicy{}, workspace, runnerCommandSearchPath()); err != nil {
		t.Fatalf("matching live check: %v", err)
	}
	if err := os.WriteFile(command, []byte("#!/bin/sh\necho 'Muse Code 2'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err = preparedRunnerForDispatch(vault, RunnerMuse, "", selected, CodexPolicy{}, workspace, runnerCommandSearchPath())
	if !errors.As(err, &admission) || admission.Code != "launch_changed" {
		t.Fatalf("changed executable = %#v", err)
	}
}

func TestRunnerTestDefaultsToProfilePreset(t *testing.T) {
	vault := automationTestVault(t)
	command := filepath.Join(t.TempDir(), "muse")
	if err := os.WriteFile(command, []byte("#!/bin/sh\necho 'Muse Code 1'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	setGlobalProfileForTest(t, "preset-default", map[string]any{
		"harness": "muse", "model": "muse-manual", "effort": "high", "permission_preset": "workspace-write-offline",
		"command":   command + " exec --json",
		"sandbox":   map[string]any{"mode": "workspace-write", "network": false},
		"subagents": map[string]any{"allowed": false, "max_concurrent": 0},
	})
	code, report, err := runRunnerConformance(Args{"vault": vault, "harness": "preset-default"})
	if err != nil || code != 0 || report.Preset != runnercore.PresetWorkspaceOffline {
		t.Fatalf("default preset = %q code=%d err=%v", report.Preset, code, err)
	}
}

func TestRunnerTestLiveAccessWrapping(t *testing.T) {
	definition := runnercore.HarnessDefinition{Provider: "muse", Transport: runnercore.TransportCLI, Dialect: "muse"}
	wrapper, err := liveConformanceArgvWrapper(definition, t.TempDir(), runnercore.PresetDangerFullAccess)
	if err != nil {
		t.Fatal(err)
	}
	argv, evidence := wrapper([]string{"/usr/bin/muse", "exec"})
	if runtime.GOOS == "darwin" {
		if len(argv) < 5 || argv[0] != "/usr/bin/sandbox-exec" || !strings.Contains(argv[2], "Keychains") || argv[3] != "/usr/bin/muse" {
			t.Fatalf("live deny-list argv = %q", argv)
		}
	} else if argv[0] != "/usr/bin/muse" || !strings.Contains(evidence, "unavailable") {
		t.Fatalf("unsupported host evidence = %q %q", argv, evidence)
	}
	definition.Transport = runnercore.TransportACP
	fallback, err := liveConformanceArgvWrapper(definition, t.TempDir(), runnercore.PresetDangerFullAccess)
	if err != nil {
		t.Fatal(err)
	}
	_, evidence = fallback([]string{"/usr/bin/acp"})
	if !strings.Contains(evidence, "not applied") {
		t.Fatalf("unsupported transport evidence = %q", evidence)
	}
}

func TestRunnerTestLiveUsesWrappedArgv(t *testing.T) {
	bin := t.TempDir()
	command := filepath.Join(bin, "codex")
	wrapper := filepath.Join(bin, "wrapper")
	marker := filepath.Join(bin, "wrapped")
	if err := os.WriteFile(command, []byte("#!/bin/sh\nif [ \"$1\" = --version ]; then echo codex-test; exit 0; fi\nif [ \"$1\" = login ]; then echo Logged; exit 0; fi\nprintf '%s\\n' '{\"type\":\"turn.completed\"}'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(wrapper, []byte(fmt.Sprintf("#!/bin/sh\nprintf wrapped > %q\nexec \"$@\"\n", marker)), 0o755); err != nil {
		t.Fatal(err)
	}
	report, err := runnercore.Conformance(context.Background(), runnercore.HarnessDefinition{ID: "wrapped", Provider: "openai", Transport: runnercore.TransportCLI, Dialect: "codex", Executable: command, SchemaVersion: 1}, runnercore.RunInput{
		Workspace: t.TempDir(), Preset: runnercore.PresetReadOnly, SearchPath: bin,
		LiveArgvWrapper: func(argv []string) ([]string, string) {
			return append([]string{wrapper}, argv...), "fixture wrapper applied"
		},
	}, true)
	if err != nil || !report.Ready {
		t.Fatalf("wrapped live check = %#v %v", report, err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("live launch bypassed wrapper: %v", err)
	}
}

func TestHarnessConformanceExerciseValidation(t *testing.T) {
	vault := automationTestVault(t)
	if code, _, err := runRunnerConformance(Args{"vault": vault, "harness": "codex_exec", "exercise": "timer"}); code != 2 || err == nil {
		t.Fatalf("offline exercise: code=%d err=%v", code, err)
	}
	if code, _, err := runRunnerConformance(Args{"vault": vault, "harness": "codex_exec", "live": "true", "exercise": "unknown"}); code != 2 || err == nil {
		t.Fatalf("unknown exercise: code=%d err=%v", code, err)
	}
}

func TestDraftConformanceUsesCurrentValuesWithoutProfileState(t *testing.T) {
	vault := automationTestVault(t)
	bin := t.TempDir()
	command := filepath.Join(bin, "codex")
	if err := writeText(command, "#!/bin/sh\nif [ \"$1\" = --version ]; then echo codex-test; exit 0; fi\nif [ \"$1\" = login ]; then echo Logged; exit 0; fi\nprintf '%s\\n' '{\"type\":\"turn.completed\"}'\n"); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(command, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	code, report, err := runRunnerConformance(Args{"vault": vault, "harness": "codex_exec", "draft": "true", "draft-id": "luna-draft", "model": "gpt-5.6-luna", "effort": "xhigh", "preset": "read-only"})
	if err != nil || code != 0 || report.HarnessID != "draft-luna-draft" || report.ProfileID != "" || report.Model != "gpt-5.6-luna" || report.Effort != "xhigh" {
		t.Fatalf("draft conformance = code=%d report=%#v err=%v", code, report, err)
	}
}

func TestRunnerTestUsesGlobalProfileWithoutVault(t *testing.T) {
	// Runner profiles are global, so `runner test` outside any repo must not
	// demand a vault: it resolves the user-global profile and the live policy
	// canary's protected path lands in a temp dir instead of the vault.
	t.Chdir(t.TempDir())
	t.Setenv("TUSKER_STATE_ROOT", t.TempDir())
	bin := t.TempDir()
	command := filepath.Join(bin, "codex")
	if err := writeText(command, "#!/bin/sh\nif [ \"$1\" = --version ]; then echo codex-test; exit 0; fi\nif [ \"$1\" = login ]; then echo Logged; exit 0; fi\nprintf '%s\\n' '{\"type\":\"item.completed\",\"item\":{\"text\":\"TUSKER_POLICY_CANARY_ATTEMPTED\"}}' '{\"type\":\"turn.completed\"}'\n"); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(command, 0o755); err != nil {
		t.Fatal(err)
	}
	setGlobalProfileForTest(t, "outside-runner", map[string]any{
		"harness": "codex_exec", "model": "gpt-6-sol", "effort": "low", "permission_preset": "read-only",
		"command":   command + " exec --json -",
		"sandbox":   map[string]any{"mode": "read-only", "network": false},
		"subagents": map[string]any{"allowed": false, "max_concurrent": 0},
	})
	code, report, err := runRunnerConformance(Args{"harness": "outside-runner", "preset": "read-only", "live": "true"})
	if err != nil || code != 0 || !report.Ready {
		t.Fatalf("vault-less live conformance: code=%d report=%#v err=%v", code, report, err)
	}
	if report.ProfileID != "outside-runner" || report.Model != "gpt-6-sol" || report.Effort != "low" || report.ProfileRevision == "" {
		t.Fatalf("global profile identity missing: %#v", report)
	}
	levels, err := modelLevelsRead("")
	if err != nil || levels.ProfileStates["outside-runner"] != "tested" {
		t.Fatalf("vault-less profile state: %#v %v", levels.ProfileStates, err)
	}
}

func TestAgentProfileTestIdentity(t *testing.T) {
	vault := automationTestVault(t)
	bin := t.TempDir()
	argsPath := filepath.Join(t.TempDir(), "muse-argv")
	command := filepath.Join(bin, "muse")
	script := fmt.Sprintf("#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then echo 'Muse Code 1.1.1'; exit 0; fi\nprintf '%%s\\n' \"$@\" > %q\nprintf '%%s\\n' '{\"schema_version\":1,\"stream\":{\"kind\":\"session\",\"id\":\"muse-session-fixture\"},\"record_type\":\"reconciliation\",\"payload_type\":\"run.terminal.completed\",\"payload\":{\"text\":\"TUSKER_POLICY_CANARY_ATTEMPTED\"}}'\n", argsPath)
	if err := writeText(command, script); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(command, 0o755); err != nil {
		t.Fatal(err)
	}
	setGlobalProfileForTest(t, "muse-profile", map[string]any{
		"harness": "muse", "model": "muse-manual", "effort": "high", "permission_preset": "read-only",
		"command": command + " exec --json", "sandbox": map[string]any{"mode": "read-only", "network": false}, "subagents": map[string]any{"allowed": false, "max_concurrent": 0},
	})
	code, report, err := runRunnerConformance(Args{"vault": vault, "harness": "muse-profile", "preset": "read-only", "live": "true"})
	if err != nil || code != 0 || !report.Ready || !report.Live {
		t.Fatalf("live profile test: code=%d report=%#v err=%v", code, report, err)
	}
	if report.ProfileID != "muse-profile" || report.HarnessID != "muse-profile" || report.Model != "muse-manual" || report.Effort != "high" || report.Transport != runnercore.TransportCLI {
		t.Fatalf("exact profile identity missing: %#v", report)
	}
	if report.ProfileRevision == "" {
		t.Fatalf("profile test did not record its saved revision: %#v", report)
	}
	levels, err := modelLevelsRead(vault)
	if err != nil || levels.ProfileStates["muse-profile"] != "tested" {
		t.Fatalf("matching saved test was not current: %#v %v", levels.ProfileStates, err)
	}
	firstFingerprint := report.ConfigurationHash
	argv, err := os.ReadFile(argsPath)
	if err != nil || !strings.Contains(string(argv), "muse-manual") || !strings.Contains(string(argv), "--model") {
		t.Fatalf("selected Muse profile was not invoked exactly: %q %v", argv, err)
	}
	setGlobalProfileForTest(t, "muse-profile.model", "muse-edited")
	levels, err = modelLevelsRead(vault)
	if err != nil || levels.ProfileStates["muse-profile"] != "configured_unverified" {
		t.Fatalf("profile edit left a prior test current: %#v %v", levels.ProfileStates, err)
	}
	code, report, err = runRunnerConformance(Args{"vault": vault, "harness": "muse-profile", "preset": "read-only", "live": "true"})
	if err != nil || code != 0 || report.Model != "muse-edited" || report.ConfigurationHash == firstFingerprint {
		t.Fatalf("profile edit did not invalidate test identity: code=%d report=%#v err=%v", code, report, err)
	}
}
