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
	if err := newV7Task(Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Finished work", "risk": "low", "priority": "p1", "proof-mode": "inline", "proof-required": "focused_test", "v7": "true"}); err != nil {
		t.Fatal(err)
	}
	return vault, "APP-T-0001"
}

func acceptTestGreenProof(t *testing.T, vault, id string) {
	t.Helper()
	if err := verifyV7AddCmd(Args{"vault": vault, "quiet": "true", "_pos1": id, "by": "agent:worker", "covers": "A1", "check": "command: go test ./cmd/tusker -run TestAccept -count=1", "result": "pass", "note": "Proof passed."}); err != nil {
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
