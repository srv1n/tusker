package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

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

func TestAgentProfileTestIdentity(t *testing.T) {
	vault := automationTestVault(t)
	bin := t.TempDir()
	argsPath := filepath.Join(t.TempDir(), "muse-argv")
	command := filepath.Join(bin, "codex")
	script := fmt.Sprintf("#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then echo 'codex-muse-test 1'; exit 0; fi\nprintf '%%s\\n' \"$@\" > %q\nprintf '%%s\\n' '{\"type\":\"turn.completed\"}' '.tusker-conformance-write .tusker-conformance-outside'\n", argsPath)
	if err := writeText(command, script); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(command, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := setProjectLocalConfigWithReadback(vault, "automation.profiles.muse-profile", map[string]any{
		"harness": "muse", "model": "muse-manual", "effort": "high", "permission_preset": "read-only",
		"command": command + " --profile muse exec --json -", "sandbox": map[string]any{"mode": "read-only", "network": false}, "subagents": map[string]any{"allowed": false, "max_concurrent": 0},
	}); err != nil {
		t.Fatal(err)
	}
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
	if err != nil || !strings.Contains(string(argv), "muse-manual") || !strings.Contains(string(argv), "--profile") {
		t.Fatalf("selected Muse profile was not invoked exactly: %q %v", argv, err)
	}
	if _, err := setProjectLocalConfigWithReadback(vault, "automation.profiles.muse-profile.model", "muse-edited"); err != nil {
		t.Fatal(err)
	}
	levels, err = modelLevelsRead(vault)
	if err != nil || levels.ProfileStates["muse-profile"] != "configured_unverified" {
		t.Fatalf("profile edit left a prior test current: %#v %v", levels.ProfileStates, err)
	}
	code, report, err = runRunnerConformance(Args{"vault": vault, "harness": "muse-profile", "preset": "read-only", "live": "true"})
	if err != nil || code != 0 || report.Model != "muse-edited" || report.ConfigurationHash == firstFingerprint {
		t.Fatalf("profile edit did not invalidate test identity: code=%d report=%#v err=%v", code, report, err)
	}
}
