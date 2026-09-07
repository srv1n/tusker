package main

import (
	"os"
	"path/filepath"
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
