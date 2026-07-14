package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEventLogAllowsConcurrentRunnerTelemetryGrowth(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	log := NewEventLog(path)
	log.writeFn = func(file *os.File, raw []byte) (int, error) {
		n, err := file.Write(raw)
		if err != nil {
			return n, err
		}
		external, openErr := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
		if openErr != nil {
			return n, openErr
		}
		_, appendErr := external.WriteString("{\"kind\":\"runner_heartbeat\"}\n")
		closeErr := external.Close()
		if appendErr != nil {
			return n, appendErr
		}
		return n, closeErr
	}
	if err := log.Append("attempt_spawned", "attempt-1", RunnerCodexExec, nil); err != nil {
		t.Fatal(err)
	}
	if err := log.Validate(); err != nil {
		t.Fatal(err)
	}
}
