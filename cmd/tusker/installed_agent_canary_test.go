package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestInstalledAgentCanaryLifecycle(t *testing.T) {
	if os.Getenv("TUSKER_RUN_LIVE_TESTS") != "1" {
		t.Skip("set TUSKER_RUN_LIVE_TESTS=1 to spend one disposable Codex turn")
	}
	if runtime.GOOS != "darwin" {
		t.Skip("installed Mac app canary")
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	vault := filepath.Join(cwd, "..", "..", ".tusker")
	code, report, err := runRunnerConformance(Args{"vault": vault, "harness": "codex_exec", "preset": "read-only", "live": "true"})
	if err != nil || code != 0 || !report.Ready {
		t.Fatalf("installed canary: code=%d err=%v report=%#v", code, err, report)
	}
}
