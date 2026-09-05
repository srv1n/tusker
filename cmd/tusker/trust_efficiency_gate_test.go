package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestTrustEfficiencyGate(t *testing.T) {
	repo, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(repo, "scripts", "measure-agent-workflows.py")
	baseline := filepath.Join(repo, "docs", "reports", "agent-efficiency", "token-baseline.json")
	fixtures := filepath.Join(repo, "docs", "reports", "agent-efficiency", "fixtures-v2.json")
	for _, path := range []string{script, baseline, fixtures} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("efficiency gate artifact %s: %v", path, err)
		}
	}

	output, err := runTrustEfficiencyCheck(repo, script, baseline, fixtures)
	if err != nil {
		t.Fatalf("baseline self-check failed: %v\n%s", err, output)
	}
	if !strings.Contains(output, "PASS: FLW-T-0008 token baseline self-check") {
		t.Fatalf("baseline self-check did not report PASS: %s", output)
	}

	raw, err := os.ReadFile(baseline)
	if err != nil {
		t.Fatal(err)
	}
	mutated := strings.Replace(string(raw), `"fixture_manifest_sha256": "sha256:`, `"fixture_manifest_sha256": "sha256:0000`, 1)
	if mutated == string(raw) {
		t.Fatal("could not create intentional manifest regression")
	}
	badBaseline := filepath.Join(t.TempDir(), "token-baseline.json")
	if err := os.WriteFile(badBaseline, []byte(mutated), 0o600); err != nil {
		t.Fatal(err)
	}
	output, err = runTrustEfficiencyCheck(repo, script, badBaseline, fixtures)
	if err == nil {
		t.Fatalf("manifest regression unexpectedly passed: %s", output)
	}
	if !strings.Contains(output, "FAIL:") || !strings.Contains(output, "fixture manifest") {
		t.Fatalf("manifest regression reported the wrong failure: %s", output)
	}
}

func runTrustEfficiencyCheck(repo, script, baseline, fixtures string) (string, error) {
	command := exec.Command("python3", script, "--check", "--baseline", baseline, "--fixtures", fixtures)
	command.Dir = repo
	output, err := command.CombinedOutput()
	return string(output), err
}
