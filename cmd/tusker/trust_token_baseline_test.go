package main

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestTrustTokenBaseline(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	repo := filepath.Clean(filepath.Join(filepath.Dir(file), "../.."))
	script := filepath.Join(repo, "scripts", "measure-agent-workflows.py")
	baseline := filepath.Join(repo, "docs", "reports", "agent-efficiency", "token-baseline.json")
	fixtures := filepath.Join(repo, "docs", "reports", "agent-efficiency", "fixtures-v2.json")
	command := exec.Command("python3", script, "--check", "--baseline", baseline, "--fixtures", fixtures)
	command.Dir = repo
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("token baseline self-check failed: %v\n%s", err, output)
	}
	if !strings.Contains(string(output), "PASS: FLW-T-0008 token baseline self-check") {
		t.Fatalf("token baseline self-check did not report PASS: %s", output)
	}
}
