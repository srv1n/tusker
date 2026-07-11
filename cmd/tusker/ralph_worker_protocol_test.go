package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFreshSessionPerAttemptCodexStartsNewThread(t *testing.T) {
	tempRoot := t.TempDir()
	t.Setenv("TUSKER_STATE_ROOT", filepath.Join(tempRoot, "state"))
	workspaceRoot := filepath.Join(tempRoot, "workspace")
	if err := ensureDir(workspaceRoot); err != nil {
		t.Fatal(err)
	}
	promptPath := filepath.Join(tempRoot, "prompt.md")
	if err := writeText(promptPath, "Fresh prompt.\n"); err != nil {
		t.Fatal(err)
	}
	scriptPath := filepath.Join(tempRoot, "fake-app-server.py")
	script := `#!/usr/bin/env python3
import json,sys
for line in sys.stdin:
    msg=json.loads(line)
    if msg.get("method")=="initialize":
        print(json.dumps({"jsonrpc":"2.0","id":msg["id"],"result":{"serverInfo":{"name":"fake-codex"}}}), flush=True)
    elif msg.get("method")=="thread/resume":
        raise AssertionError("fresh attempts must not resume predecessor threads")
    elif msg.get("method")=="thread/start":
        print(json.dumps({"jsonrpc":"2.0","id":msg["id"],"result":{"thread":{"id":"thread-fresh"}}}), flush=True)
    elif msg.get("method")=="turn/start":
        assert msg["params"]["threadId"]=="thread-fresh", msg["params"]
        assert msg["params"]["input"][0]["text"]=="Fresh prompt.\n", msg["params"]
        print(json.dumps({"jsonrpc":"2.0","id":msg["id"],"result":{"turn":{"id":"turn-fresh","status":"inProgress","items":[]}}}), flush=True)
        print(json.dumps({"jsonrpc":"2.0","method":"turn/completed","params":{"threadId":"thread-fresh","turn":{"id":"turn-fresh","status":"completed","items":[]}}}), flush=True)
        break
`
	if err := writeText(scriptPath, script); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(scriptPath, 0o755); err != nil {
		t.Fatal(err)
	}

	result, err := startLiveCodex(context.Background(), StartRequest{
		ProjectID:     "project-1",
		RecordID:      "APP-T-0001",
		ItemID:        "APP-T-0001",
		AttemptID:     "attempt-fresh",
		WorkspacePath: workspaceRoot,
		PromptPath:    promptPath,
		EventSinkPath: filepath.Join(tempRoot, "events.jsonl"),
		RawLogPath:    filepath.Join(tempRoot, "codex.raw.log"),
		StatusPath:    filepath.Join(tempRoot, "codex.status.json"),
		Command:       scriptPath,
		VaultPath:     tempRoot,
		CodexPolicy:   CodexPolicy{MaxTurns: 1, TurnTimeoutMS: 5000, ReadTimeoutMS: 5000, StallTimeoutMS: 5000},
	}, &ResumeRequest{SessionRef: "thread-predecessor", MessageRef: "msg-predecessor"})
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "thread-fresh", result.SessionRef, "fresh codex thread")
	waitForStatusFile(t, filepath.Join(tempRoot, "codex.status.json"))
}

func TestFreshSessionPerAttemptClaudeStartsNewSession(t *testing.T) {
	tempRoot := t.TempDir()
	workspaceRoot := filepath.Join(tempRoot, "workspace")
	if err := ensureDir(workspaceRoot); err != nil {
		t.Fatal(err)
	}
	promptPath := filepath.Join(tempRoot, "prompt.md")
	if err := writeText(promptPath, "Fresh claude prompt.\n"); err != nil {
		t.Fatal(err)
	}
	scriptPath := filepath.Join(tempRoot, "fake-claude.py")
	script := `#!/usr/bin/env python3
import json,os,sys
assert "--resume" not in sys.argv, sys.argv
for line in sys.stdin:
    msg=json.loads(line)
    if msg.get("type")=="control_request":
        continue
    if msg.get("type")=="user":
        assert os.environ.get("TUSKER_SESSION_REF","")=="", os.environ.get("TUSKER_SESSION_REF")
        print(json.dumps({"type":"assistant","session_id":"claude-fresh","uuid":"claude-msg-fresh","message":{"id":"claude-msg-fresh","role":"assistant","content":[{"type":"text","text":"fresh"}]}}), flush=True)
        break
`
	if err := writeText(scriptPath, script); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(scriptPath, 0o755); err != nil {
		t.Fatal(err)
	}

	result, err := (&ClaudeRunner{}).Resume(context.Background(), ResumeRequest{
		ProjectID:     "project-1",
		RecordID:      "APP-T-0001",
		ItemID:        "APP-T-0001",
		AttemptID:     "attempt-claude-fresh",
		WorkspacePath: workspaceRoot,
		PromptPath:    promptPath,
		EventSinkPath: filepath.Join(tempRoot, "events.jsonl"),
		RawLogPath:    filepath.Join(tempRoot, "claude.raw.log"),
		StatusPath:    filepath.Join(tempRoot, "claude.status.json"),
		Command:       scriptPath + " --input-format stream-json",
		VaultPath:     tempRoot,
		SessionRef:    "claude-predecessor",
		MessageRef:    "claude-message",
	})
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "claude-fresh", result.SessionRef, "fresh claude session")
	waitForStatusFile(t, filepath.Join(tempRoot, "claude.status.json"))
}

func TestPlanFileLifecycle(t *testing.T) {
	vault := automationTestVault(t)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Plan lifecycle", "risk": "low", "priority": "p1", "v7": "true"}, newV7Task)
	makeV7TaskDispatchableForTest(t, vault, "APP-T-0001")

	plan, err := ensureTaskPlanFile(vault, "APP-T-0001", "Plan lifecycle")
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Created {
		t.Fatal("expected first plan creation")
	}
	assertExists(t, plan.Path)
	if err := writeText(plan.Path, "# Custom Plan\n\n- [x] keep this between attempts\n"); err != nil {
		t.Fatal(err)
	}
	again, err := ensureTaskPlanFile(vault, "APP-T-0001", "Plan lifecycle")
	if err != nil {
		t.Fatal(err)
	}
	if again.Created || !strings.Contains(again.Contents, "keep this between attempts") {
		t.Fatalf("expected existing plan to survive, got created=%t contents=%q", again.Created, again.Contents)
	}

	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "_pos1": "APP-T-0001", "covers": "A1", "check": "go test ./cmd/tusker -run TestPlanFileLifecycle -count=1", "result": "pass", "note": "Plan lifecycle proof passed."}, verifyV7AddCmd)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "by": "reviewer:agent", "force": "true", "local": "true"}, closeV7Cmd)
	if fileExists(plan.Path) {
		t.Fatalf("expected close to remove plan file %s", plan.Path)
	}
	task := mustV7Task(t, vault, "APP-T-0001")
	if strings.Contains(task.Body, "PLAN.md") || strings.Contains(task.Body, ".tusker/scratch") {
		t.Fatalf("task markdown must not reference disposable plan file:\n%s", task.Body)
	}
}

func TestPromptBackpressureAndSigns(t *testing.T) {
	vault := ralphPromptTestVault(t)
	signLines := make([]string, 0, tuskerSignsWarnLineLimit+1)
	for i := 0; i < tuskerSignsWarnLineLimit+1; i++ {
		signLines = append(signLines, fmt.Sprintf("- sign %02d", i+1))
	}
	if err := writeText(filepath.Join(vault, "signs.md"), strings.Join(signLines, "\n")+"\n"); err != nil {
		t.Fatal(err)
	}

	prompt := renderRalphPromptForTest(t, vault, RunStatus{
		ActiveAttemptID: "attempt-prev",
		LeaseState:      string(LeaseStateParkedNoProgress),
		AttemptOutcome:  string(AttemptOutcomeBlocked),
		LastError:       "parked because validation stayed red",
	})
	for _, expected := range []string{
		"## Tusker Attempt Context",
		"# APP-T-0001 agent packet",
		".tusker/scratch/APP-T-0001/PLAN.md",
		"Previous reason: parked because validation stayed red",
		"Source: `tusker.yaml` automation.validation.commands",
		"go test ./cmd/tusker -run TestPromptBackpressureAndSigns -count=1",
		"Search before implementing",
		"placeholder, stub",
		"Warning: `.tusker/signs.md` has 61 lines",
		"- sign 61",
	} {
		if !strings.Contains(prompt, expected) {
			t.Fatalf("expected prompt to contain %q, got:\n%s", expected, prompt)
		}
	}
	_, warnings := validateV7SkillKnowledge(vault)
	if !issuesContainCode(warnings, "SIGNS_FILE_BLOATED") {
		t.Fatalf("expected signs bloat warning, got %#v", warnings)
	}
}

func TestAttemptInputFlat(t *testing.T) {
	vault := ralphPromptTestVault(t)
	var minLen, maxLen int
	for i := 1; i <= 5; i++ {
		prompt := renderRalphPromptForTest(t, vault, RunStatus{
			ActiveAttemptID: fmt.Sprintf("attempt-%d", i),
			LeaseState:      string(LeaseStateRetryQueued),
			AttemptOutcome:  string(AttemptOutcomeNone),
			LastError:       strings.Repeat("raw predecessor transcript chunk ", i*400),
		})
		length := len(prompt)
		if i == 1 || length < minLen {
			minLen = length
		}
		if length > maxLen {
			maxLen = length
		}
		if strings.Contains(prompt, strings.Repeat("raw predecessor transcript chunk ", 20)) {
			t.Fatalf("prompt included unbounded predecessor text")
		}
	}
	if maxLen-minLen > 700 {
		t.Fatalf("expected roughly flat prompt sizes across continuations; min=%d max=%d", minLen, maxLen)
	}
}

func ralphPromptTestVault(t *testing.T) string {
	t.Helper()
	vault := automationTestVault(t)
	repo := filepath.Dir(vault)
	if err := writeText(filepath.Join(repo, "tusker.yaml"), strings.TrimSpace(`
automation:
  validation:
    commands:
      - go test ./cmd/tusker -run TestPromptBackpressureAndSigns -count=1
      - go vet ./cmd/tusker
`)+"\n"); err != nil {
		t.Fatal(err)
	}
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Prompt context", "risk": "low", "priority": "p1", "v7": "true"}, newV7Task)
	makeV7TaskDispatchableForTest(t, vault, "APP-T-0001")
	return vault
}

func renderRalphPromptForTest(t *testing.T, vault string, previousRun RunStatus) string {
	t.Helper()
	wfFile, err := loadWorkflow(vault)
	if err != nil {
		t.Fatal(err)
	}
	project := newRegisteredProject(filepath.Dir(vault), vault)
	note := mustV7Task(t, vault, "APP-T-0001")
	prompt, err := renderAttemptPrompt(project, wfFile, note, filepath.Dir(vault), 2, "attempt-current", runLaneExecute, RunStatus{}, previousRun, nil)
	if err != nil {
		t.Fatal(err)
	}
	return prompt
}
