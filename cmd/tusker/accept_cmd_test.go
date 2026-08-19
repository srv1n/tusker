package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func acceptTestVaultWithTask(t *testing.T) (string, string) {
	t.Helper()
	vault := filepath.Join(t.TempDir(), ".tusker")
	if err := bootstrap(Args{"vault": vault, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	if err := newV7Epic(Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "Accept", "summary": "One-step accept.", "v7": "true"}); err != nil {
		t.Fatal(err)
	}
	if err := writeText(filepath.Join(v7RepoRoot(vault), "test_accept.py"), "import unittest\nclass Proof(unittest.TestCase):\n    def test_accept(self):\n        self.assertTrue(True)\n"); err != nil {
		t.Fatal(err)
	}
	if err := newV7Task(Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Finished work", "risk": "low", "priority": "p1", "proof-mode": "inline", "proof-required": "focused_test", "v7": "true"}); err != nil {
		t.Fatal(err)
	}
	return vault, "APP-T-0001"
}

func acceptTestGreenProof(t *testing.T, vault, id string) {
	t.Helper()
	if _, err := upsertV7Verification(vault, id, v7VerificationRow{CoverText: "A1", Check: "command: python3 -m unittest discover -s .", Result: "pass", Notes: "Existing gate receipt."}, "reviewer:gate"); err != nil {
		t.Fatalf("verify: %v", err)
	}
}

func acceptTestTaskData(t *testing.T, vault, id string) map[string]any {
	t.Helper()
	idx, err := loadV7Index(vault)
	if err != nil {
		t.Fatal(err)
	}
	task, ok := idx.Tasks[id]
	if !ok {
		t.Fatalf("task %s not found after accept", id)
	}
	return task.Data
}

// A1: one command accepts, confirms proof, and closes a green task.
func TestAcceptClosesGreenTask(t *testing.T) {
	vault, id := acceptTestVaultWithTask(t)
	acceptTestGreenProof(t, vault, id)

	if err := acceptV7Cmd(Args{"vault": vault, "quiet": "true", "_pos0": id, "by": "reviewer:independent"}); err != nil {
		t.Fatalf("accept green task: %v", err)
	}
	data := acceptTestTaskData(t, vault, id)
	if got := stringField(data, "status"); got != "done" {
		t.Fatalf("green task did not close: status=%q", got)
	}
	if got := stringField(data, "proof_status"); got != "satisfied" {
		t.Fatalf("accept did not confirm proof: proof_status=%q", got)
	}
	if stringField(data, "closed_at") == "" {
		t.Fatalf("accept did not record closed_at")
	}
}

// A2: the reviewer who accepted is recorded on the closed task.
func TestAcceptRecordsReviewer(t *testing.T) {
	vault, id := acceptTestVaultWithTask(t)
	acceptTestGreenProof(t, vault, id)

	if err := acceptV7Cmd(Args{"vault": vault, "quiet": "true", "_pos0": id, "by": "reviewer:independent"}); err != nil {
		t.Fatalf("accept: %v", err)
	}
	data := acceptTestTaskData(t, vault, id)
	if got := stringField(data, "accepted_by"); got != "reviewer:independent" {
		t.Fatalf("acceptor not recorded: accepted_by=%q", got)
	}
	if stringField(data, "accepted_at") == "" {
		t.Fatalf("accept did not record accepted_at")
	}
}

// A3: a task with non-green or missing proof is refused and stays open, with a reason.
func TestAcceptRefusesUnprovenTask(t *testing.T) {
	vault, id := acceptTestVaultWithTask(t)
	// No verification rows: proof is not green.

	err := acceptV7Cmd(Args{"vault": vault, "quiet": "true", "_pos0": id, "by": "reviewer:independent"})
	if err == nil {
		t.Fatalf("accept rubber-stamped unproven work")
	}
	if !strings.Contains(err.Error(), "proof is not green") {
		t.Fatalf("refusal lacked a named reason: %v", err)
	}
	data := acceptTestTaskData(t, vault, id)
	if got := stringField(data, "status"); got == "done" {
		t.Fatalf("refused task was closed anyway: status=%q", got)
	}
	if stringField(data, "accepted_by") != "" {
		t.Fatalf("refused task recorded an acceptor")
	}
}

// A4: accept on an already-done task is refused by name and does not downgrade
// it back to review or wipe its acceptance metadata.
func TestAcceptRefusesDoneTask(t *testing.T) {
	vault, id := acceptTestVaultWithTask(t)
	acceptTestGreenProof(t, vault, id)

	if err := acceptV7Cmd(Args{"vault": vault, "quiet": "true", "_pos0": id, "by": "reviewer:independent"}); err != nil {
		t.Fatalf("first accept: %v", err)
	}
	before := acceptTestTaskData(t, vault, id)
	if got := stringField(before, "status"); got != "done" {
		t.Fatalf("precondition: task not done: status=%q", got)
	}

	err := acceptV7Cmd(Args{"vault": vault, "quiet": "true", "_pos0": id, "by": "reviewer:second"})
	if err == nil {
		t.Fatalf("accept re-accepted a done task")
	}
	if !strings.Contains(err.Error(), "already done") {
		t.Fatalf("refusal lacked a named reason: %v", err)
	}
	after := acceptTestTaskData(t, vault, id)
	if got := stringField(after, "status"); got != "done" {
		t.Fatalf("refused done task was downgraded: status=%q", got)
	}
	if got := stringField(after, "accepted_by"); got != "reviewer:independent" {
		t.Fatalf("refused done task lost its acceptor: accepted_by=%q", got)
	}
}

// A5: a close precondition that would refuse (an open blocking gate) is detected
// before any status write, so the task is left exactly where it was.
func TestAcceptPreflightLeavesStatusUntouched(t *testing.T) {
	vault, id := acceptTestVaultWithTask(t)
	acceptTestGreenProof(t, vault, id)

	if err := newV7Gate(Args{"vault": vault, "quiet": "true", "blocks": id, "kind": "release", "owner": "human:release", "action": "Authorize the production release.", "verification": "Release authority records approval.", "why-agent-cannot": "Only the production release authority can deploy."}); err != nil {
		t.Fatalf("create blocking gate: %v", err)
	}
	before := stringField(acceptTestTaskData(t, vault, id), "status")

	err := acceptV7Cmd(Args{"vault": vault, "quiet": "true", "_pos0": id, "by": "reviewer:independent"})
	if err == nil {
		t.Fatalf("accept ignored an open blocking gate")
	}
	if !strings.Contains(err.Error(), "open gate") {
		t.Fatalf("refusal lacked a named reason: %v", err)
	}
	after := stringField(acceptTestTaskData(t, vault, id), "status")
	if after != before {
		t.Fatalf("preflight refusal changed status: before=%q after=%q", before, after)
	}
	if after == "review" {
		t.Fatalf("preflight refusal stranded task in review")
	}
}

// A6: accept requires an explicit, namespaced reviewer identity; a missing or
// bare --by is refused with a message naming the expected format.
func TestAcceptRequiresExplicitReviewer(t *testing.T) {
	vault, id := acceptTestVaultWithTask(t)
	acceptTestGreenProof(t, vault, id)

	missing := acceptV7Cmd(Args{"vault": vault, "quiet": "true", "_pos0": id})
	if missing == nil {
		t.Fatalf("accept ran without an explicit acceptor")
	}
	if errorToIssue(missing).Code != errorMissingArg {
		t.Fatalf("missing --by refusal did not return missing-actor error: %v", missing)
	}

	bad := acceptV7Cmd(Args{"vault": vault, "quiet": "true", "_pos0": id, "by": "agent:worker"})
	if bad == nil {
		t.Fatalf("accept accepted a non-namespaced actor")
	}
	if errorToIssue(bad).Code != errorInvalidField {
		t.Fatalf("invalid --by refusal did not return actor-field error: %v", bad)
	}

	// The refused task must stay open with no recorded acceptor.
	data := acceptTestTaskData(t, vault, id)
	if got := stringField(data, "status"); got == "done" {
		t.Fatalf("task closed despite refused identity: status=%q", got)
	}
	if stringField(data, "accepted_by") != "" {
		t.Fatalf("refused task recorded an acceptor")
	}
}
