package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunnerStatusUnknownCode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "status.json")
	if err := os.WriteFile(path, []byte(`{"exit_code":1,"outcome":"failed","reason":"provider failed","reason_code":"future_code"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	status, err := readRunnerProcessStatus(path)
	if err != nil {
		t.Fatal(err)
	}
	if status.ReasonCode != string(RunFailureUnknown) || !strings.Contains(status.Reason, "future_code") || status.ExitCode != 1 {
		t.Fatalf("status=%+v", status)
	}
}
