package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestV7NewTaskDefaultsToBacklogHeldForCLI(t *testing.T) {
	vault := v7DispatchTestVault(t)

	if err := newV7Task(Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Draft task"}); err != nil {
		t.Fatal(err)
	}

	data, _, err := parseFrontmatterMustRead(filepath.Join(vault, "work", "tasks", "APP-T-0001.md"))
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "backlog", stringField(data, "status"), "default task status")
	assertEqual(t, "held", stringField(data, "readiness"), "default task readiness")
}

func TestV7ReadyTaskWithStubAcceptanceRejected(t *testing.T) {
	vault := v7DispatchTestVault(t)

	err := newV7Task(Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Bad ready task", "ready": "true"})
	if err == nil {
		t.Fatal("expected ready placeholder task to be rejected")
	}
	if !strings.Contains(err.Error(), "not dispatchable") {
		t.Fatalf("expected dispatchable error, got %v", err)
	}
}

func TestV7NextSkipsStubReadyTask(t *testing.T) {
	vault := v7DispatchTestVault(t)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Stub ready", "v7": "true"}, newV7Task)
	forceV7DispatchPlaceholderAcceptance(t, vault, "APP-T-0001")

	if _, ok := pickV7Next(vault, "APP", ""); ok {
		t.Fatal("expected next to skip ready task with placeholder acceptance")
	}
}

func TestV7PacketAgentRequiresForceForUndispatchableStub(t *testing.T) {
	vault := v7DispatchTestVault(t)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Stub packet", "v7": "true"}, newV7Task)
	forceV7DispatchPlaceholderAcceptance(t, vault, "APP-T-0001")

	err := packetV7Cmd(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "for": "agent"})
	if err == nil {
		t.Fatal("expected agent packet to reject undispatchable stub")
	}
	if !strings.Contains(err.Error(), "not dispatchable") {
		t.Fatalf("expected dispatchability error, got %v", err)
	}
	output := captureStdout(t, func() {
		if err := packetV7Cmd(Args{"vault": vault, "id": "APP-T-0001", "for": "agent", "force": "true"}); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(output, "Packet warnings") || !strings.Contains(output, "placeholder") {
		t.Fatalf("expected forced packet to include placeholder warning, got:\n%s", output)
	}
}

func TestV7ExplainerPacketBuildsUnderstandingScaffold(t *testing.T) {
	vault := v7DispatchTestVault(t)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Explainer packets", "risk": "high", "domains": "project", "v7": "true"}, newV7Task)
	replaceV7TaskSection(t, vault, "APP-T-0001", "## Intent", "Help humans understand agent-authored changes before review.")
	replaceV7TaskSection(t, vault, "APP-T-0001", "## Acceptance", "| ID | Outcome | Proof |\n|---|---|---|\n| A1 | Explainer packets render background, intuition, proof, and quiz sections. | Focused CLI test |")
	replaceV7TaskSection(t, vault, "APP-T-0001", "## Knowledge delta", "| Change type | Topic | Before | After |\n|---|---|---|---|\n| added | Understanding packets | Review packets were proof-focused. | Explainer packets build human mental models. |")

	taskPath := filepath.Join(vault, "work", "tasks", "APP-T-0001.md")
	taskData, taskBody, err := parseFrontmatterMustRead(taskPath)
	if err != nil {
		t.Fatal(err)
	}
	packet := v7Packet(vault, Note{AbsolutePath: taskPath, Data: taskData, Body: taskBody}, mustIndex(t, vault), "explainer")
	for _, expected := range []string{
		"# APP-T-0001 explainer packet",
		"Purpose: help a human understand and participate in this change.",
		"Boundary: this packet is not proof, approval, or a replacement for code review.",
		"## Background",
		"## Intuition",
		"## Task Walkthrough",
		"## Literate Diff Guide",
		"## Proof Map",
		"## Review Focus",
		"## Comprehension Check",
		"Explainer packets build human mental models.",
		"Does this explainer packet count as evidence or approval?",
		"No. It is an understanding aid.",
	} {
		assertContainsIndexTest(t, packet, expected)
	}
}

func TestV7ExplainerPacketWriteUsesGeneratedPacketPath(t *testing.T) {
	vault := v7DispatchTestVault(t)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Write explainer packet", "domains": "project", "v7": "true"}, newV7Task)

	if err := packetV7Cmd(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "for": "explainer", "write": "true"}); err != nil {
		t.Fatal(err)
	}
	packetPath := filepath.Join(vault, "_generated", "packets", "APP-T-0001.explainer.md")
	assertExists(t, packetPath)
	assertContainsIndexTest(t, mustReadIndexTest(t, packetPath), "# APP-T-0001 explainer packet")
}

func TestV7ProofAndReviewRejectStubAcceptance(t *testing.T) {
	vault := v7DispatchTestVault(t)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Stub proof", "proof-mode": "inline", "v7": "true"}, newV7Task)
	forceV7DispatchPlaceholderAcceptance(t, vault, "APP-T-0001")
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "_pos1": "APP-T-0001", "covers": "A1", "check": "go test ./cmd/tusker -run TestStub -count=1", "result": "pass", "note": "Focused proof passed."}, v7TestVerificationMutation)

	data, _, err := parseFrontmatterMustRead(filepath.Join(vault, "work", "tasks", "APP-T-0001.md"))
	if err != nil {
		t.Fatal(err)
	}
	if stringField(data, "proof_status") == "satisfied" {
		t.Fatalf("stub acceptance must not satisfy proof: %#v", data)
	}

	err = statusV7Cmd(Args{"vault": vault, "quiet": "true", "local": "true", "id": "APP-T-0001", "status": "review", "by": "agent:codex"})
	if err == nil || !strings.Contains(err.Error(), "placeholder acceptance") {
		t.Fatalf("expected review to reject placeholder acceptance, got %v", err)
	}
	err = finishV7Cmd(Args{"vault": vault, "quiet": "true", "local": "true", "id": "APP-T-0001", "summary": "Done."})
	if err == nil || !strings.Contains(err.Error(), "placeholder acceptance") {
		t.Fatalf("expected finish to reject placeholder acceptance, got %v", err)
	}
}

func TestV7ValidationErrorsOnSatisfiedStubAcceptance(t *testing.T) {
	vault := v7DispatchTestVault(t)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Satisfied stub", "v7": "true"}, newV7Task)
	forceV7DispatchPlaceholderAcceptance(t, vault, "APP-T-0001")
	taskPath := filepath.Join(vault, "work", "tasks", "APP-T-0001.md")
	data, body, err := parseFrontmatterMustRead(taskPath)
	if err != nil {
		t.Fatal(err)
	}
	data["proof_status"] = "satisfied"
	data["state_rev"] = v7StateRev(data, body)
	content, err := serializeDocument(data, body, v7FrontmatterOrder["task"])
	if err != nil {
		t.Fatal(err)
	}
	if err := writeText(taskPath, content); err != nil {
		t.Fatal(err)
	}

	code, err := validateCmd(Args{"vault": vault, "json": "true"})
	if err != nil {
		t.Fatal(err)
	}
	if code == 0 {
		t.Fatal("expected validation to fail for satisfied placeholder acceptance")
	}
}

func v7DispatchTestVault(t *testing.T) string {
	t.Helper()
	vault := filepath.Join(t.TempDir(), "vault")
	mustV7Proof(t, Args{"vault": vault, "quiet": "true"}, bootstrap)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App V7", "summary": "Dispatch policy.", "v7": "true"}, newV7Epic)
	return vault
}

func forceV7DispatchPlaceholderAcceptance(t *testing.T, vault, taskID string) {
	t.Helper()
	taskPath := filepath.Join(vault, "work", "tasks", taskID+".md")
	data, body, err := parseFrontmatterMustRead(taskPath)
	if err != nil {
		t.Fatal(err)
	}
	body = replaceSection(body, "## Acceptance", "| ID | Outcome | Proof |\n|---|---|---|\n| A1 | Define the accepted outcome. | Inline verification, evidence, gate, or waiver |")
	data["status"] = "ready"
	data["readiness"] = "ready"
	data["next_owner"] = "agent"
	data["next_action"] = "Execute the task contract and satisfy proof mode."
	data["state_rev"] = v7StateRev(data, body)
	content, err := serializeDocument(data, body, v7FrontmatterOrder["task"])
	if err != nil {
		t.Fatal(err)
	}
	if err := writeText(taskPath, content); err != nil {
		t.Fatal(err)
	}
}

func replaceV7TaskSection(t *testing.T, vault, taskID, heading, newContent string) {
	t.Helper()
	taskPath := filepath.Join(vault, "work", "tasks", taskID+".md")
	data, body, err := parseFrontmatterMustRead(taskPath)
	if err != nil {
		t.Fatal(err)
	}
	body = replaceSection(body, heading, newContent)
	data["state_rev"] = v7StateRev(data, body)
	content, err := serializeDocument(data, body, v7FrontmatterOrder["task"])
	if err != nil {
		t.Fatal(err)
	}
	if err := writeText(taskPath, content); err != nil {
		t.Fatal(err)
	}
}
