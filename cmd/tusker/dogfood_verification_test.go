package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// Regression tests for the 2026-10-24 rzn backend dogfood report: checks
// that ran out a shared budget, lost their output, ignored repo test
// wrappers, dropped build routing, or reran work for nothing.

func dogfoodSetWorkflowVerification(t *testing.T, vault, yaml string) {
	t.Helper()
	path := workflowPath(vault)
	text, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	rest, ok := strings.CutPrefix(string(text), "---\n")
	if !ok {
		t.Fatalf("WORKFLOW.md has no frontmatter: %s", path)
	}
	if err := os.WriteFile(path, []byte("---\nverification:\n"+yaml+rest), 0o644); err != nil {
		t.Fatal(err)
	}
}

func dogfoodSetChecks(t *testing.T, vault, id string, checks ...string) {
	t.Helper()
	rewriteTaskFile(t, vault, id, func(data map[string]any, body string) (map[string]any, string) {
		body = directDispatchableTaskBody(id)
		var rows []v7VerificationRow
		for _, check := range checks {
			rows = append(rows, v7VerificationRow{CoverText: "A1", Check: "command: " + check, Result: "pending"})
		}
		data["proof_status"] = "pending"
		return data, replaceSection(body, "## Verification", renderV7VerificationTable(rows))
	})
}

func TestDogfoodEachCheckGetsTheFullTimeout(t *testing.T) {
	vault, store, project := adoptFixture(t, "APP-T-0001", nil)
	dogfoodSetWorkflowVerification(t, vault, "  timeout: 1500ms\n")
	// Together the two checks outlast one timeout; each fits inside it.
	dogfoodSetChecks(t, vault, "APP-T-0001", "sleep 1 && test -f submitted.txt", "sleep 1.0 && test -f submitted.txt")
	if result := adoptRun(t, vault, store, project.ProjectID, "APP-T-0001", "agent:builder"); !result.OK {
		t.Fatalf("second check ran out a shared budget: %s", result.Reason)
	}
}

func TestDogfoodIdenticalChecksRunOnce(t *testing.T) {
	vault, store, project := adoptFixture(t, "APP-T-0001", nil)
	counter := filepath.Join(t.TempDir(), "runs")
	check := "test -f submitted.txt && printf x >> " + shellSingleQuote(counter)
	dogfoodSetChecks(t, vault, "APP-T-0001", check, check, check)
	if result := adoptRun(t, vault, store, project.ProjectID, "APP-T-0001", "agent:builder"); !result.OK {
		t.Fatal(result.Reason)
	}
	if got, _ := os.ReadFile(counter); string(got) != "x" {
		t.Fatalf("identical check ran %d times", len(got))
	}
	for _, row := range parseV7VerificationRows(walkthroughNote(t, vault, "APP-T-0001").Body) {
		if row.Result != "pass" || v7VerificationReceiptFields(row) == nil {
			t.Fatalf("row without its own receipt: %#v", row)
		}
	}
}

func TestDogfoodChecksReceiveDeclaredEnvironment(t *testing.T) {
	vault, store, project := adoptFixture(t, "APP-T-0001", nil)
	t.Setenv("TUSKER_DOGFOOD_SLOT", "air")
	dogfoodSetChecks(t, vault, "APP-T-0001", `test "$TUSKER_DOGFOOD_SLOT" = air && test -f submitted.txt`)
	if result := adoptRun(t, vault, store, project.ProjectID, "APP-T-0001", "agent:builder"); result.OK {
		t.Fatal("an undeclared variable reached the check")
	}
	dogfoodSetWorkflowVerification(t, vault, "  env:\n    - TUSKER_DOGFOOD_*\n")
	if result := adoptRun(t, vault, store, project.ProjectID, "APP-T-0001", "agent:builder"); !result.OK {
		t.Fatalf("declared variable did not reach the check: %s", result.Reason)
	}
	if slices.Contains(v7VerificationCommandEnv([]string{"*"}, false), "TUSKER_DOGFOOD_SLOT=air") {
		t.Fatal("a bare * passed the whole shell environment")
	}
	wf := defaultWorkflow()
	wf.Verification.Env = []string{"*"}
	if err := validateWorkflow(wf, "WORKFLOW.md", ""); err == nil || !strings.Contains(err.Error(), "verification.env") {
		t.Fatalf("WORKFLOW.md accepted verification.env: [*]: %v", err)
	}
}

func TestDogfoodFailedCheckKeepsItsOutput(t *testing.T) {
	vault, store, project := adoptFixture(t, "APP-T-0001", nil)
	// The octal escapes print "zqx-cause" without the check's text naming it.
	dogfoodSetChecks(t, vault, "APP-T-0001", `echo compiling; printf '\172\161\170\055\143\141\165\163\145\n'; exit 3`)
	result := adoptRun(t, vault, store, project.ProjectID, "APP-T-0001", "agent:builder")
	_, saved, ok := strings.Cut(result.Reason, "output saved to ")
	if result.OK || !ok {
		t.Fatalf("refusal does not point at the output: %s", result.Reason)
	}
	saved, _, _ = strings.Cut(saved, ")")
	got, err := os.ReadFile(filepath.Join(v7RepoRoot(vault), saved))
	if err != nil || !strings.Contains(string(got), "zqx-cause") {
		t.Fatalf("saved output %q: %v", got, err)
	}
	if strings.Contains(walkthroughNote(t, vault, "APP-T-0001").Body, "zqx-cause") {
		t.Fatal("raw output was written to the task")
	}
}

func TestDogfoodRepoTestWrapperCountsAsTest(t *testing.T) {
	vault, store, project := adoptFixture(t, "APP-T-0001", nil)
	if err := os.WriteFile(filepath.Join(v7RepoRoot(vault), "Makefile"), []byte("cargo-any:\n\t@true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dogfoodSetChecks(t, vault, "APP-T-0001", "make cargo-any CARGO_ARGS='test -p core'")
	result := adoptRun(t, vault, store, project.ProjectID, "APP-T-0001", "agent:builder")
	if result.OK || !strings.Contains(result.Reason, "not a recognised test runner") || !strings.Contains(result.Reason, "verification.test_commands") {
		t.Fatalf("refusal does not say why the passing check did not count: %s", result.Reason)
	}
	dogfoodSetWorkflowVerification(t, vault, "  test_commands:\n    - \"make cargo-any CARGO_ARGS=.?(test|nextest)\"\n")
	if result := adoptRun(t, vault, store, project.ProjectID, "APP-T-0001", "agent:builder"); !result.OK {
		t.Fatalf("declared test wrapper did not count: %s", result.Reason)
	}
	if !v7CommandInvokesTest("cargo nextest run -p core") {
		t.Fatal("cargo nextest is not a test runner")
	}
}

func TestDogfoodProofPolicyChangeKeepsReceipts(t *testing.T) {
	vault, _, _ := adoptFixture(t, "APP-T-0001", nil)
	task := walkthroughNote(t, vault, "APP-T-0001")
	if _, _, failures, err := executeV7CommandVerificationRows(vault, task, Args{"quiet": "true"}, "agent:builder", true); err != nil || len(failures) > 0 {
		t.Fatalf("checks: %v %#v", err, failures)
	}
	if err := proofV7SetModeCmd(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "mode": "inline", "required": "command", "by": "agent:builder"}); err != nil {
		t.Fatal(err)
	}
	task = walkthroughNote(t, vault, "APP-T-0001")
	if rows := parseV7VerificationRows(task.Body); len(rows) != 1 || rows[0].Result != "pass" {
		t.Fatalf("policy change reset the passed check: %#v", rows)
	}
	if cause := v7VerificationReceiptInvalidation(vault, task); cause != nil {
		t.Fatalf("receipt went stale on a policy change: %#v", cause)
	}
}

func TestDogfoodDoneSkipsDependentsOfARefusedTask(t *testing.T) {
	vault, _, _ := adoptFixture(t, "APP-T-0001", nil)
	first := walkthroughNote(t, vault, "APP-T-0001")
	writeDirectTask(t, vault, "APP-T-0002", "", map[string]any{
		"status": "ready", "readiness": "ready", "source_sha": stringField(first.Data, "source_sha"),
		"work_revision": 1, "owned_paths": []string{"submitted.txt"}, "dependencies": []string{"APP-T-0001"},
	})
	counter := filepath.Join(t.TempDir(), "runs")
	dogfoodSetChecks(t, vault, "APP-T-0001", "false")
	dogfoodSetChecks(t, vault, "APP-T-0002", "test -f submitted.txt && printf x >> "+shellSingleQuote(counter))
	out := captureStdout(t, func() {
		_ = doneCmd(Args{"vault": vault, "_pos": "APP-T-0001 APP-T-0002", "by": "agent:builder", "json": "true"})
	})
	if !strings.Contains(out, "skipped: depends on APP-T-0001") {
		t.Fatalf("dependent was not reported as skipped: %s", out)
	}
	if _, err := os.Stat(counter); err == nil {
		t.Fatal("dependent ran its checks although its dependency was refused")
	}
}

func TestDogfoodVerifyRemoveTakesSeveralRows(t *testing.T) {
	vault, _, _ := adoptFixture(t, "APP-T-0001", nil)
	dogfoodSetChecks(t, vault, "APP-T-0001", "test -f one", "test -f two", "test -f three")
	if err := verifyV7RemoveCmd(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "index": "3,1", "by": "agent:builder"}); err != nil {
		t.Fatal(err)
	}
	rows := parseV7VerificationRows(walkthroughNote(t, vault, "APP-T-0001").Body)
	if len(rows) != 1 || rows[0].Check != "command: test -f two" {
		t.Fatalf("positions did not all refer to the original table: %#v", rows)
	}
}
