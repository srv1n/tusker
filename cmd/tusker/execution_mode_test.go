package main

import (
	"strings"
	"testing"
)

func clearAgentSessionEnvForTest(t *testing.T) {
	t.Helper()
	for _, key := range []string{"TUSKER_ATTEMPT_ID", "CODEX_SHELL", "CODEX_THREAD_ID", "CLAUDECODE", "CLAUDE_CODE_ENTRYPOINT"} {
		t.Setenv(key, "")
	}
}

func TestAgentSessionRejectsNestedRunnerLaunches(t *testing.T) {
	clearAgentSessionEnvForTest(t)
	t.Setenv("CODEX_SHELL", "1")
	for _, command := range []string{"tusker daemon run", "tusker automation dispatch", "tusker daemon service start"} {
		err := rejectAgentSpawn(command)
		if err == nil || !strings.Contains(err.Error(), "interactive Codex session") || !strings.Contains(err.Error(), "cannot run") {
			t.Fatalf("%s: expected nested-launch refusal, got %v", command, err)
		}
	}
}

func TestAgentSessionClassifiesDispatchedWorkerBeforeHostAgent(t *testing.T) {
	t.Setenv("CODEX_SHELL", "1")
	t.Setenv("TUSKER_ATTEMPT_ID", "attempt-1")
	if got := agentSessionKind(); got != "dispatched Tusker worker" {
		t.Fatalf("kind=%q", got)
	}
}

func TestAgentSessionGuardLeavesHumanTerminalCommandsAvailable(t *testing.T) {
	clearAgentSessionEnvForTest(t)
	if err := rejectAgentSpawn("tusker daemon run"); err != nil {
		t.Fatalf("human terminal should remain available: %v", err)
	}
}

func TestExecutionModeInstructionsForbidNestedRunners(t *testing.T) {
	for _, path := range []string{"../../AGENTS.md", "../../CLAUDE.md", "../../docs/specs/reliable-execution-lifecycle.md"} {
		body, err := readText(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, phrase := range []string{"resident daemon", "never"} {
			if !strings.Contains(strings.ToLower(body), phrase) {
				t.Fatalf("%s missing %q execution-mode contract", path, phrase)
			}
		}
	}
	skill, err := readText("../../skills/tusker/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"implements the requested work itself", "through the CLI", "work only the claimed task"} {
		if !strings.Contains(skill, required) {
			t.Fatalf("task-only skill missing %q", required)
		}
	}
}

func TestReviewDispatchStopsAtConfiguredCycleCap(t *testing.T) {
	wf := defaultWorkflow()
	note := Note{Data: map[string]any{"kind": "task", "status": "review", "risk": "medium"}}
	if !reviewDispatchAllowed(t.TempDir(), note, wf, RunStatus{}, wf.Reviewer.MaxCycles-1) {
		t.Fatal("review before cap should be dispatchable")
	}
	if reviewDispatchAllowed(t.TempDir(), note, wf, RunStatus{}, wf.Reviewer.MaxCycles) {
		t.Fatal("review at cap must require operator intervention")
	}
	attempts := []RunAttempt{{Lane: runLaneExecute}, {Lane: runLaneReview}, {Lane: runLaneReview}}
	if got := reviewerAttemptCount(attempts); got != 2 {
		t.Fatalf("review attempts=%d", got)
	}
}

func TestReviewerPolicyRejectsUnboundedCycleConfiguration(t *testing.T) {
	wf := defaultWorkflow()
	wf.Reviewer.MaxCycles = 0
	if err := validateReviewerPolicy(wf.Reviewer, wf.Agents.Enabled, "WORKFLOW.md"); err == nil || !strings.Contains(err.Error(), "max_cycles") {
		t.Fatalf("expected reviewer cycle validation, got %v", err)
	}
}
