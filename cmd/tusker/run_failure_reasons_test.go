package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRunFailureReasonCodes(t *testing.T) {
	want := map[RunFailureReasonCode]string{
		RunFailureUsageLimit: "blocked", RunFailureAuthExpired: "blocked", RunFailurePermissionDenied: "blocked",
		RunFailureSandboxDenied: "blocked", RunFailureNetworkDenied: "blocked", RunFailureMissingAccess: "blocked",
		RunFailureContextWindow: "failed", RunFailureMaxTurns: "failed", RunFailureMaxBudget: "blocked",
		RunFailureConfigInvalid: "blocked", RunFailureProviderError: "failed", RunFailureProcessLost: "lost",
		RunFailureOutcomeUnknown: "lost", RunFailureCancelled: "failed", RunFailureUnknown: "failed",
	}
	if len(runFailureReasons) != len(want) {
		t.Fatalf("reason count = %d, want %d", len(runFailureReasons), len(want))
	}
	for code, class := range want {
		spec, ok := runFailureReason(code)
		if !ok || spec.Class != class || spec.Outcome == "" || spec.Guidance == "" {
			t.Fatalf("reason %q = %#v, found %v", code, spec, ok)
		}
		got := classifyRetryFailure("configuration in unrelated output", code)
		if got.outcome != spec.Outcome || got.retryable != spec.Retryable || got.code != code || got.source != "driver" {
			t.Fatalf("typed retry %q = %#v", code, got)
		}
	}
	if _, ok := runFailureReason("made_up"); ok {
		t.Fatal("unknown code accepted")
	}
	legacy := classifyRetryFailure("invalid token")
	if legacy.retryable || legacy.outcome != AttemptOutcomeBlocked || legacy.code != RunFailureUnknown || legacy.source != "legacy_text" {
		t.Fatalf("legacy classification = %#v", legacy)
	}
	for _, marker := range []string{
		"auth", "authentication", "authorization", "unauthorized", "forbidden", "permission denied",
		"api key", "token expired", "invalid token", "login required", "not logged in",
		"sandbox", "approval denied", "approval rejected", "requires approval", "human approval",
		"config", "configuration", "unsupported runner", "command is empty", "invalid workflow",
		"deterministic", "validation failed", "invalid request", "bad request",
		"budget", "quota", "spend limit", "rate limit budget",
		"context window", "context-window", "context length", "maximum context", "context limit",
	} {
		got := classifyRetryFailure(marker)
		if got.retryable || got.outcome != AttemptOutcomeBlocked || got.code != RunFailureUnknown || got.source != "legacy_text" {
			t.Fatalf("legacy %q = %#v", marker, got)
		}
	}
	for _, test := range []struct {
		reason string
		want   AttemptOutcome
	}{
		{"turn cap exhausted", AttemptOutcomeTurnCapExhausted},
		{"runner process no longer matches recorded identity", AttemptOutcomeCancelled},
		{"delivery_unknown", AttemptOutcomeUnknown},
		{"delivery unknown", AttemptOutcomeUnknown},
		{"prompt delivery is unknown", AttemptOutcomeUnknown},
		{"child exited without terminal status", AttemptOutcomeUnknown},
	} {
		got := classifyRetryFailure(test.reason)
		if got.outcome != test.want || got.code != RunFailureUnknown || got.source != "legacy_text" {
			t.Fatalf("legacy %q = %#v", test.reason, got)
		}
	}
	if caps := (RunnerCapabilities{}); caps.SoftSay || caps.HardSay || caps.PreassignSessionID || caps.ResumeAfterDeath {
		t.Fatalf("undeclared capabilities = %#v", caps)
	}
	statusPath := filepath.Join(t.TempDir(), "status.json")
	if _, err := writeRunnerStatusFileIfAbsentWithOutcome(statusPath, 1, AttemptOutcomeFailed, "usage limit", 0, RunFailureUsageLimit); err != nil {
		t.Fatal(err)
	}
	status, err := readRunnerProcessStatus(statusPath)
	if err != nil || status.ReasonCode != string(RunFailureUsageLimit) {
		t.Fatalf("status = %#v, %v", status, err)
	}
	classified := classifyRunnerProcessExit(RunStatus{}, status, Note{Data: map[string]any{"status": "ready"}}, "", nil)
	if classified.outcome != AttemptOutcomeBlocked || classified.reasonCode != status.ReasonCode {
		t.Fatalf("classified status = %#v", classified)
	}
	if _, err := writeRunnerStatusFileIfAbsentWithOutcome(filepath.Join(t.TempDir(), "invalid.json"), 1, AttemptOutcomeFailed, "", 0, "made_up"); err == nil {
		t.Fatal("unknown status code accepted")
	}
	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	run := RunStatus{ProjectID: "p", RecordID: "t", ReasonCode: string(RunFailureUsageLimit)}
	if err := store.UpsertRun(run); err != nil {
		t.Fatal(err)
	}
	runs, err := store.ListRuns()
	if err != nil || len(runs) != 1 || runs[0].ReasonCode != run.ReasonCode {
		t.Fatalf("persisted runs = %#v, %v", runs, err)
	}
	attempt := RunAttempt{AttemptID: "a", ProjectID: "p", RecordID: "t", ReasonCode: run.ReasonCode}
	if err := store.SaveAttempt(attempt); err != nil {
		t.Fatal(err)
	}
	attempts, err := store.ListAttemptsForRun("p", "t")
	if err != nil || len(attempts) != 1 || attempts[0].ReasonCode != attempt.ReasonCode {
		t.Fatalf("persisted attempts = %#v, %v", attempts, err)
	}
	run.ReasonCode = "made_up"
	if err := store.UpsertRun(run); err == nil {
		t.Fatal("unknown run code stored")
	}
}

func TestCodexExecFailureCode(t *testing.T) {
	for _, test := range []struct {
		name, message string
		want          RunFailureReasonCode
	}{
		{"usage", "You've hit your usage limit", RunFailureUsageLimit},
		{"auth", "401 Unauthorized: please login", RunFailureAuthExpired},
		{"generic", "stream disconnected before completion", RunFailureProviderError},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "raw.jsonl")
			if err := os.WriteFile(path, []byte(`{"type":"error","message":"`+test.message+`"}`+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			got, _ := codexExecFailureFromLog(path)
			if got != test.want {
				t.Fatalf("code = %q, want %q", got, test.want)
			}
		})
	}
}
