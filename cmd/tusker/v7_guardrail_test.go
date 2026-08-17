package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestV7GuardrailNoDiataxisInRuntimePolicyOrProjectSkill(t *testing.T) {
	repo := repoRootForFreshCloneTest(t)
	for _, rel := range []string{
		"cmd/tusker/commands_v7.go",
		"cmd/tusker/v7_validation.go",
		".tusker/SKILL.md",
		".tusker/knowledge/domains/project/CANON.md",
	} {
		text := mustReadGuardrailFile(t, filepath.Join(repo, filepath.FromSlash(rel)))
		lower := strings.ToLower(text)
		if strings.Contains(lower, "diataxis") || strings.Contains(lower, "diátaxis") {
			t.Fatalf("%s mentions Diataxis; V7 runtime policy and project skill text should use concrete audience/source/route language instead", rel)
		}
	}
}

func TestV7GuardrailTopLevelHelpAndBootstrapDoNotExposeLegacyDocsTaxonomy(t *testing.T) {
	for name, output := range map[string]string{
		"main": captureStdout(t, printHelp),
		"init": captureStdout(t, printInitHelp),
		"v7":   captureStdout(t, printV7Help),
	} {
		lower := strings.ToLower(output)
		for _, forbidden := range []string{"diataxis", "diátaxis", "tutorial", "how-to", "explanation", "_config/docs-map.yaml"} {
			if strings.Contains(lower, forbidden) {
				t.Fatalf("%s help exposes legacy docs taxonomy %q:\n%s", name, forbidden, output)
			}
		}
	}

	vault := filepath.Join(t.TempDir(), "tusker")
	if err := bootstrap(Args{"vault": vault, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"_config/docs-map.yaml", "docs/spec", "docs/reference", "_system/templates", "_system/views"} {
		if fileExists(filepath.Join(vault, filepath.FromSlash(rel))) {
			t.Fatalf("default V7 bootstrap created legacy path %s", rel)
		}
	}
}

func TestV7GuardrailDurableTaskStatusesStayLifecycleOnly(t *testing.T) {
	for _, forbidden := range []string{"active", "blocked"} {
		if _, ok := v7TaskStatuses[forbidden]; ok {
			t.Fatalf("V7 durable task statuses must not include %q; use attempts/leases for runtime activity and gates/dependencies for blockers", forbidden)
		}
	}
}

func TestV7GuardrailFailureHintsAreAgentLegible(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "tusker")
	if err := bootstrap(Args{"vault": vault, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	body := strings.Join([]string{
		"## Acceptance",
		"",
		"| ID | Outcome | Proof |",
		"|---|---|---|",
		"| A1 | Define the outcome. | Evidence record |",
		"",
		"## Work Log",
		"",
		"FAIL one",
		"FAIL two",
		"FAIL three",
		"FAIL four",
		"FAIL five",
	}, "\n")
	note := Note{
		Data: map[string]any{
			"schema":      "tusker.task/v7",
			"kind":        "task",
			"id":          "APP-T-0001",
			"project":     "tusker",
			"title":       "Guardrail hints",
			"epic":        "APP",
			"status":      "ready",
			"readiness":   "ready",
			"priority":    "p2",
			"risk":        "low",
			"next_owner":  "agent",
			"next_action": "Execute the task contract.",
		},
		Body:         body,
		RelativePath: "work/tasks/APP-T-0001.md",
	}
	errs, _ := validateV7Note(note, validationContext{VaultPath: vault, RelativePath: note.RelativePath}, note.RelativePath)
	for _, code := range []string{"TASK_WORK_LOG_SECTION", "TASK_RAW_LOG_IN_BODY"} {
		issue, ok := findIssueByCode(errs, code)
		if !ok {
			t.Fatalf("expected %s, got %#v", code, errs)
		}
		if !strings.Contains(issue.Hint, "evidence") && !strings.Contains(issue.Hint, "attempt") {
			t.Fatalf("%s hint should tell an agent where to move the content, got %q", code, issue.Hint)
		}
	}
}

func TestV7GuardrailVerificationTablePassRowsAreStructuredProof(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "tusker")
	if err := bootstrap(Args{"vault": vault, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	body := strings.Join([]string{
		"## Acceptance",
		"",
		"| ID | Outcome | Proof |",
		"|---|---|---|",
		"| A1 | Define the outcome. | Inline verification |",
		"",
		"## Verification",
		"",
		"| Covers | Check | Result | Notes |",
		"|---|---|---|---|",
		"| A1 | go test ./cmd/tusker -run TestOne -count=1 | pass | Focused proof passed. |",
		"| A1 | go test ./cmd/tusker -run TestTwo -count=1 | pass | Focused proof passed. |",
		"| A1 | go test ./cmd/tusker -run TestThree -count=1 | pass | Focused proof passed. |",
		"| A1 | go test ./cmd/tusker -run TestFour -count=1 | pass | Focused proof passed. |",
		"| A1 | go test ./cmd/tusker -run TestFive -count=1 | pass | Focused proof passed. |",
	}, "\n")
	note := Note{
		Data: map[string]any{
			"schema":      "tusker.task/v7",
			"kind":        "task",
			"id":          "APP-T-0001",
			"project":     "tusker",
			"title":       "Structured proof rows",
			"epic":        "APP",
			"status":      "ready",
			"readiness":   "ready",
			"priority":    "p2",
			"risk":        "low",
			"next_owner":  "agent",
			"next_action": "Execute the task contract.",
		},
		Body:         body,
		RelativePath: "work/tasks/APP-T-0001.md",
	}
	errs, warns := validateV7Note(note, validationContext{VaultPath: vault, RelativePath: note.RelativePath}, note.RelativePath)
	if issuesContainCode(errs, "TASK_RAW_LOG_IN_BODY") {
		t.Fatalf("verification table pass rows should not count as raw logs, got %#v", errs)
	}
	if issuesContainCode(warns, "VERIFICATION_PROOF_MISSING") {
		t.Fatalf("command-shaped verification rows should satisfy exact proof guidance, got %#v", warns)
	}
}

func TestV7GuardrailSkillPackageEnforcesHardStopCloseoutContract(t *testing.T) {
	repo := repoRootForFreshCloneTest(t)
	required := map[string][]string{
		"skills/tusker/SKILL.md": {
			"Hard stop rule",
			"tusker closeout status <TASK-ID> --json",
			"tusker proof status <TASK-ID>",
			"agent_action: stop_until_human_response",
			"readiness: waiting_on_human",
			"Do not manufacture proof",
			"When Tusker is used, mutate its records through the CLI",
			"tracker failure is not a source-code failure",
		},
		"skills/tusker/references/WORK.md": {
			"idea -> backlog -> ready -> review -> done",
			"Runtime activity is not a durable task status",
			"If the task reports `readiness: waiting_on_human`",
			"agent_action: stop_until_human_response",
			"Report what is blocked, the exact human action",
		},
	}
	for rel, snippets := range required {
		text := strings.Join(strings.Fields(mustReadGuardrailFile(t, filepath.Join(repo, filepath.FromSlash(rel)))), " ")
		for _, snippet := range snippets {
			if !strings.Contains(text, strings.Join(strings.Fields(snippet), " ")) {
				t.Fatalf("%s is missing finish-contract guardrail %q", rel, snippet)
			}
		}
	}
}

func TestV7BodyBudgetObjectTypesHaveDistinctLimits(t *testing.T) {
	cases := map[string][2]int{
		"task":             {120, 220},
		"gate":             {80, 160},
		"index":            {180, 300},
		"canon":            {220, 400},
		"runbook":          {240, 500},
		"evidence_summary": {80, 160},
	}
	for objectType, want := range cases {
		warn, fail := v7BodyLineLimitsFor("", objectType)
		if warn != want[0] || fail != want[1] {
			t.Fatalf("%s body budget = %d/%d, want %d/%d", objectType, warn, fail, want[0], want[1])
		}
	}
}

func mustReadGuardrailFile(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func findIssueByCode(issues []Issue, code string) (Issue, bool) {
	for _, issue := range issues {
		if issue.Code == code {
			return issue, true
		}
	}
	return Issue{}, false
}
