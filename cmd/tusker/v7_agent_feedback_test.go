package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestV7NewTaskRejectsLegacyTaskIDBeforeWriting(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	if err := bootstrap(Args{"vault": vault, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	if err := newV7Epic(Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App V7", "summary": "V7 tracker smoke.", "v7": "true"}); err != nil {
		t.Fatal(err)
	}
	legacyPath := filepath.Join(vault, "epics", "APP", "APP-T-0001.md")
	if err := writeText(legacyPath, "---\nid: APP-T-0001\ntype: task\nstatus: active\ntitle: Legacy task\n---\n\n# Legacy task\n"); err != nil {
		t.Fatal(err)
	}

	err := newV7Task(Args{"vault": vault, "quiet": "true", "epic": "APP", "id": "APP-T-0001", "title": "Colliding V7 task", "risk": "low", "priority": "p2", "v7": "true"})
	if err == nil {
		t.Fatal("expected legacy/V7 task id collision")
	}
	message := err.Error()
	assertContainsIndexTest(t, message, "APP-T-0001")
	assertContainsIndexTest(t, message, "work/tasks/APP-T-0001.md")
	assertContainsIndexTest(t, message, "epics/APP/APP-T-0001.md")
	assertContainsIndexTest(t, message, "next safe candidate: APP-T-0002")
	if fileExists(filepath.Join(vault, "work", "tasks", "APP-T-0001.md")) {
		t.Fatal("colliding V7 task was written")
	}
	matches, err := filepath.Glob(filepath.Join(vault, "events", "*", "*", "APP-T-0001--*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("colliding task emitted events: %#v", matches)
	}
}

func TestV7GeneratedTaskIDSkipsConfiguredLegacyRootCollision(t *testing.T) {
	repo := t.TempDir()
	vault := filepath.Join(repo, "tusker")
	if err := writeText(managedTuskerConfigPath(filepath.Join(repo, defaultRepoVaultDir)), "legacy_task_roots:\n  - legacy/tasks\n"); err != nil {
		t.Fatal(err)
	}
	if err := bootstrap(Args{"vault": vault, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	if err := newV7Epic(Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App V7", "summary": "V7 tracker smoke.", "v7": "true"}); err != nil {
		t.Fatal(err)
	}
	if err := writeText(filepath.Join(vault, "legacy", "tasks", "renamed-legacy-task.md"), "---\nid: APP-T-0001\ntype: task\nstatus: active\ntitle: Legacy task\n---\n\n# Legacy task\n"); err != nil {
		t.Fatal(err)
	}

	if err := newV7Task(Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Safe generated task", "risk": "low", "priority": "p2", "v7": "true"}); err != nil {
		t.Fatal(err)
	}
	assertExists(t, filepath.Join(vault, "work", "tasks", "APP-T-0002.md"))
}

func TestV7ValidateReportsMixedLayoutTaskCollisionRepair(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	if err := bootstrap(Args{"vault": vault, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	if err := newV7Epic(Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App V7", "summary": "V7 tracker smoke.", "v7": "true"}); err != nil {
		t.Fatal(err)
	}
	if err := newV7Task(Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Current V7 task", "risk": "low", "priority": "p2", "v7": "true"}); err != nil {
		t.Fatal(err)
	}
	if err := writeText(filepath.Join(vault, "epics", "APP", "APP-T-0001.md"), "---\nid: APP-T-0001\ntype: task\nstatus: active\ntitle: Legacy task\n---\n\n# Legacy task\n"); err != nil {
		t.Fatal(err)
	}

	var output string
	code := 0
	var cmdErr error
	output = captureStdout(t, func() {
		code, cmdErr = validateCmd(Args{"vault": vault, "json": "true"})
	})
	if cmdErr != nil {
		t.Fatal(cmdErr)
	}
	if code == 0 {
		t.Fatal("expected validate to fail for mixed-layout task id collision")
	}
	var payload struct {
		Errors []Issue `json:"errors"`
	}
	if err := json.Unmarshal([]byte(output), &payload); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, issue := range payload.Errors {
		if issue.Code != errorIDCollision {
			continue
		}
		found = true
		assertContainsIndexTest(t, issue.Message, "APP-T-0001")
		assertContainsIndexTest(t, issue.Hint, "mixed V5/V7 task collision")
		assertContainsIndexTest(t, issue.Hint, ".tusker/work/tasks/APP-T-0001.md")
		if strings.Contains(issue.Hint, "<id>") {
			t.Fatalf("repair hint still uses placeholder id: %s", issue.Hint)
		}
	}
	if !found {
		t.Fatalf("expected ID_COLLISION issue, got %#v", payload.Errors)
	}
}

func TestV7ProtectedActiveStatusExplainsAttemptFlowAndCapsuleRuntime(t *testing.T) {
	previousWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(previousWD); err != nil {
			t.Fatalf("restore working directory: %v", err)
		}
	})

	repo := t.TempDir()
	vault := filepath.Join(repo, "tusker")
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}
	runGit(t, "init", "-b", "main")
	runGit(t, "config", "user.email", "test@example.com")
	runGit(t, "config", "user.name", "Tusker Test")
	if err := writeText(managedTuskerConfigPath(filepath.Join(repo, defaultRepoVaultDir)), "branches:\n  control:\n    - main\n"); err != nil {
		t.Fatal(err)
	}
	if err := bootstrap(Args{"vault": vault, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	if err := newV7Epic(Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App V7", "summary": "V7 tracker smoke.", "v7": "true"}); err != nil {
		t.Fatal(err)
	}
	if err := newV7Task(Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Protected branch task", "risk": "low", "priority": "p2", "v7": "true"}); err != nil {
		t.Fatal(err)
	}
	runGit(t, "add", ".")
	runGit(t, "commit", "-m", "seed")
	runGit(t, "checkout", "-b", "agent/APP-T-0001")

	err = statusV7Cmd(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "status": "active"})
	if err == nil {
		t.Fatal("expected protected active status transition to fail")
	}
	typed, ok := err.(*TuskerError)
	if !ok {
		t.Fatalf("expected TuskerError, got %T", err)
	}
	for _, expected := range []string{"attempt start APP-T-0001", "verify add APP-T-0001", "attempt handoff APP-T-0001", "finish APP-T-0001 --request-review"} {
		assertContainsIndexTest(t, typed.Hint, expected)
	}
	assertNotContainsIndexTest(t, typed.Hint, "status APP-T-0001 active")

	if err := attemptV7StartCmd(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "runner": "codex"}); err != nil {
		t.Fatal(err)
	}
	capsule := captureStdout(t, func() {
		if err := showCmd(Args{"vault": vault, "_pos0": "APP-T-0001", "capsule": "true"}); err != nil {
			t.Fatal(err)
		}
	})
	assertContainsIndexTest(t, capsule, "Next action: Execute the task contract and satisfy proof mode.")
	assertContainsIndexTest(t, capsule, "Attempt: APP-T-0001-A-0001 started")

	brief := captureStdout(t, func() {
		if err := briefV7Cmd(Args{"vault": vault, "_pos0": "APP-T-0001"}); err != nil {
			t.Fatal(err)
		}
	})
	assertContainsIndexTest(t, brief, "Attempt: APP-T-0001-A-0001 started")

	makeV7TaskDispatchableForTest(t, vault, "APP-T-0001")
	next := captureStdout(t, func() {
		if err := nextCmd(Args{"vault": vault}); err != nil {
			t.Fatal(err)
		}
	})
	assertContainsIndexTest(t, next, "APP-T-0001-A-0001 started")
}

func TestV7SkillAuditAgentGuidanceClassifiesAndWritesDraft(t *testing.T) {
	repo := t.TempDir()
	vault := filepath.Join(repo, "tusker")
	if err := ensureDir(vault); err != nil {
		t.Fatal(err)
	}
	managed := renderTuskerPointerBlock("tusker/README.md")
	if err := writeText(filepath.Join(repo, "AGENTS.md"), managed+"\n\nRun `rtk go test ./cmd/tusker` before handoff.\n"); err != nil {
		t.Fatal(err)
	}
	if err := writeText(filepath.Join(repo, "CLAUDE.md"), "You are carrying old persona prompt baggage.\n"); err != nil {
		t.Fatal(err)
	}
	if err := writeText(filepath.Join(repo, "service", "AGENTS.md"), "Architecture canon lives in docs/system.md.\n"); err != nil {
		t.Fatal(err)
	}

	audit, err := auditV7AgentGuidance(repo, vault)
	if err != nil {
		t.Fatal(err)
	}
	if len(audit.Findings) != 3 {
		t.Fatalf("expected 3 guidance findings, got %#v", audit.Findings)
	}
	classes := map[string]string{}
	for _, finding := range audit.Findings {
		classes[finding.Path] = finding.Classification
		if strings.Contains(finding.Content, tuskerPointerBegin) {
			t.Fatalf("managed Tusker block leaked into finding: %#v", finding)
		}
	}
	assertEqual(t, "verification_recipe", classes["AGENTS.md"], "root AGENTS classification")
	assertEqual(t, "stale_prompt_baggage", classes["CLAUDE.md"], "root CLAUDE classification")
	assertEqual(t, "project_knowledge", classes["service/AGENTS.md"], "nested AGENTS classification")
	if len(audit.Warnings) != 1 || audit.Warnings[0].Code != "PROJECT_SKILL_MISSING_FOR_AGENT_GUIDANCE" {
		t.Fatalf("expected missing project skill warning, got %#v", audit.Warnings)
	}

	code := 0
	var cmdErr error
	output := captureStdout(t, func() {
		code, cmdErr = skillV7AuditAgentGuidanceCmd(Args{"repo": repo, "vault": vault, "write": "true", "target": "feedback"})
	})
	if cmdErr != nil {
		t.Fatal(cmdErr)
	}
	if code == 0 {
		t.Fatal("expected audit command to return non-zero when findings exist")
	}
	assertContainsIndexTest(t, output, "Migration draft:")
	matches, err := filepath.Glob(filepath.Join(vault, "feedback", "agents", "*-agent-guidance-migration-draft.md"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 {
		t.Fatalf("expected one migration draft, got %#v", matches)
	}
	draft := mustReadIndexTest(t, matches[0])
	assertContainsIndexTest(t, draft, "verification_recipe")
	assertContainsIndexTest(t, draft, "stale_prompt_baggage")
	assertContainsIndexTest(t, draft, "project_knowledge")
}

func TestV7SkillAuditAgentGuidanceRefreshesStaleBootstrap(t *testing.T) {
	repo := t.TempDir()
	vault := filepath.Join(repo, "tusker")
	if err := ensureDir(vault); err != nil {
		t.Fatal(err)
	}
	stalePointer := strings.Join([]string{
		tuskerPointerBegin,
		"## Progressive Tusker context",
		"",
		"Start with `tusker list --type epic` to see the short epic roster.",
		tuskerPointerEnd,
	}, "\n")
	if err := writeText(filepath.Join(repo, "AGENTS.md"), stalePointer+"\n"); err != nil {
		t.Fatal(err)
	}

	audit, err := auditV7AgentGuidance(repo, vault)
	if err != nil {
		t.Fatal(err)
	}
	if !issuesContainCode(audit.Warnings, "TUSKER_BOOTSTRAP_STALE") {
		t.Fatalf("expected stale bootstrap warning, got %#v", audit.Warnings)
	}

	code := 0
	var cmdErr error
	output := captureStdout(t, func() {
		code, cmdErr = skillV7AuditAgentGuidanceCmd(Args{"repo": repo, "vault": vault, "write": "true"})
	})
	if cmdErr != nil {
		t.Fatal(cmdErr)
	}
	if code != 0 {
		t.Fatalf("expected write to refresh bootstrap cleanly, code=%d output=\n%s", code, output)
	}
	assertContainsIndexTest(t, output, "Updated bootstrap:")
	agents := mustReadIndexTest(t, filepath.Join(repo, "AGENTS.md"))
	assertContainsIndexTest(t, agents, "Keep proof compact")
	assertContainsIndexTest(t, agents, "command + PASS/FAIL summaries")

	after, err := auditV7AgentGuidance(repo, vault)
	if err != nil {
		t.Fatal(err)
	}
	if issuesContainCode(after.Warnings, "TUSKER_BOOTSTRAP_STALE") || issuesContainCode(after.Warnings, "TUSKER_BOOTSTRAP_MISSING") {
		t.Fatalf("expected bootstrap warning to be resolved, got %#v", after.Warnings)
	}
}

func TestV7PacketWarnsOnMissingRoutesAndStubAcceptance(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	if err := bootstrap(Args{"vault": vault, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(vault, "SKILL.md")); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if err := newV7Epic(Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App V7", "summary": "V7 tracker smoke.", "v7": "true"}); err != nil {
		t.Fatal(err)
	}
	if err := newV7Task(Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Missing packet routes", "risk": "low", "priority": "p2", "domains": "missing", "v7": "true"}); err != nil {
		t.Fatal(err)
	}
	forceV7DispatchPlaceholderAcceptance(t, vault, "APP-T-0001")
	task := mustV7Task(t, vault, "APP-T-0001")
	packet := v7Packet(vault, task, mustIndex(t, vault), "agent")

	assertContainsIndexTest(t, packet, "## Packet warnings")
	assertContainsIndexTest(t, packet, "`vault/SKILL.md` is missing")
	assertContainsIndexTest(t, packet, "`missing` domain route missing")
	assertContainsIndexTest(t, packet, "Acceptance looks vague or placeholder: Define the accepted outcome")
	assertContainsIndexTest(t, packet, "`missing`: domain route is missing")
	assertNotContainsIndexTest(t, packet, "`missing`: read `knowledge/domains/missing/INDEX.md`")
}
