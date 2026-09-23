package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// This fixture drives the real detached Codex exec adapter twice. The fake
// executable rejects misplaced resume flags and reports the native thread it
// received; the assertion uses the process log rather than supplied proof.
func TestDemoSessionCodexContinueFixture(t *testing.T) {
	root := t.TempDir()
	t.Setenv("TUSKER_STATE_ROOT", filepath.Join(root, "state"))
	installFakeCodexExec(t, root)
	workspace := filepath.Join(root, "workspace")
	if err := ensureDir(workspace); err != nil {
		t.Fatal(err)
	}
	prompt := filepath.Join(root, "prompt.md")
	if err := writeText(prompt, "Continue the standalone task.\n"); err != nil {
		t.Fatal(err)
	}
	start := StartRequest{
		ProjectID: "project-1", RecordID: "APP-T-0001", ItemID: "APP-T-0001", AttemptID: "attempt-1",
		Lane: runLaneExecute, WorkspacePath: workspace, RepoRoot: workspace, PromptPath: prompt,
		RawLogPath: filepath.Join(root, "first.log"), EventSinkPath: filepath.Join(root, "first.events"), StatusPath: filepath.Join(root, "first.status"),
		Command: defaultCodexExecCommand(),
	}
	if _, err := executeRunnerCommand(context.Background(), RunnerCodexExec, runnerExecRequest{
		ProjectID: start.ProjectID, RecordID: start.RecordID, ItemID: start.ItemID, AttemptID: start.AttemptID,
		Lane: start.Lane, WorkspacePath: workspace, RepoRoot: workspace, PromptPath: prompt,
		RawLogPath: start.RawLogPath, EventSinkPath: start.EventSinkPath, StatusPath: start.StatusPath, Command: start.Command,
	}, RunnerCapabilities{StructuredEvents: true, ResumeSession: true, MachineFinalStatus: true}); err != nil {
		t.Fatal(err)
	}
	waitForStatusFile(t, start.StatusPath)
	session := extractSessionRef(start.RawLogPath)
	if session == "" {
		t.Fatal("first detached attempt did not publish native session")
	}
	resumeCommand := codexExecResumeCommand(defaultCodexExecCommand())
	start.AttemptID = "attempt-2"
	start.RawLogPath = filepath.Join(root, "second.log")
	start.EventSinkPath = filepath.Join(root, "second.events")
	start.StatusPath = filepath.Join(root, "second.status")
	start.Command = resumeCommand
	if _, err := runnerWrapperStartChild(context.Background(), runnerWrapperRequest{
		Runner: string(RunnerCodexExec), Start: start,
		Resume: &ResumeRequest{SessionRef: session, Command: resumeCommand},
	}); err != nil {
		t.Fatal(err)
	}
	waitForStatusFile(t, start.StatusPath)
	if got := extractSessionRef(start.RawLogPath); got != session {
		t.Fatalf("native session changed across continuation: %q -> %q", session, got)
	}
	argv, err := readText(filepath.Join(root, "fake-codex-args.log"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(strings.TrimSpace(argv), "\n") != 1 || !strings.Contains(argv, "resume "+session+" -") {
		t.Fatalf("expected exactly two process attempts with native resume: %s", argv)
	}
}

// This checks proof validation only; the separate fake Serve and runner tests
// exercise controls. It is not end-to-end scenario qualification.
func TestDemoSessionScenariosFixture(t *testing.T) {
	for _, harness := range []string{"claude-code", "codex_exec", "muse", "devin"} {
		for _, scenario := range demoSessionScenarios {
			t.Run(harness+"/"+scenario, func(t *testing.T) {
				if reason := demoSessionUnsupported(harness, scenario); reason != "" {
					if scenario != "say-soft" {
						t.Fatalf("unexpected unsupported scenario: %s", reason)
					}
					return
				}
				id := "native-1"
				if scenario == "start-fresh" {
					id = "native-2"
				}
				attempts := []string{"attempt-1"}
				if scenario == "kill-worker" || scenario == "say-hard" || scenario == "stop-continue" || scenario == "start-fresh" {
					attempts = append(attempts, "attempt-2")
				}
				states := []string{"Working"}
				switch scenario {
				case "kill-worker":
					states = append(states, "Lost")
				case "ask-wait":
					states = append(states, "Waiting on you")
				case "ask-nowait", "stop-continue", "start-fresh":
					states = append(states, "Stopped")
				case "permission-deny":
					states = append(states, "Blocked")
				}
				var observed []demoSessionObservation
				for _, state := range states {
					observed = append(observed, demoSessionObservation{State: state, At: time.Now().UTC().Format(time.RFC3339Nano)})
				}
				fixture := demoSessionProof{Scenario: scenario, Harness: harness, NativeBefore: "native-1", NativeAfter: id, AttemptIDs: attempts, Observations: observed}
				raw, err := json.Marshal(fixture)
				if err != nil {
					t.Fatal(err)
				}
				binary := filepath.Join(t.TempDir(), "fake-harness")
				if err := os.WriteFile(binary, append([]byte("#!/bin/sh\ncat <<'JSON'\n"), append(raw, []byte("\nJSON\n")...)...), 0o755); err != nil {
					t.Fatal(err)
				}
				proof, err := demoRunSessionFixture(binary, harness, scenario)
				if err != nil || proof.Status != "passed" || proof.Label != "fixture" {
					t.Fatalf("fixture proof: %#v, %v", proof, err)
				}
				for _, check := range proof.Checks {
					if !check.Passed {
						t.Fatalf("failed check: %#v", check)
					}
				}
			})
		}
	}
}
