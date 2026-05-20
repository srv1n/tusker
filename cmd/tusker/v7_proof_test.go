package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestV7InlineProofClosesWithoutEvidenceFile(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	mustV7Proof(t, Args{"vault": vault, "quiet": "true"}, bootstrap)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App", "summary": "Proof policy.", "v7": "true"}, newV7Epic)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Inline proof close", "risk": "low", "priority": "p2", "proof-mode": "inline", "v7": "true"}, newV7Task)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "runner": "codex"}, attemptV7StartCmd)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "_pos1": "APP-T-0001", "covers": "A1", "check": "go test ./cmd/tusker -run TestProof -count=1", "result": "pass", "note": "Focused proof passed."}, verifyV7AddCmd)

	if err := finishV7Cmd(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "attempt": "APP-T-0001-A-0001", "summary": "Implementation complete.", "local": "true"}); err != nil {
		t.Fatal(err)
	}
	taskPath := filepath.Join(vault, "work", "tasks", "APP-T-0001.md")
	data, body, err := parseFrontmatterMustRead(taskPath)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "review", stringField(data, "status"), "finish moved task to review")
	assertEqual(t, "satisfied", stringField(data, "proof_status"), "proof status")
	if !strings.Contains(body, "| A1 | go test ./cmd/tusker -run TestProof -count=1 | pass | Focused proof passed. |") {
		t.Fatalf("verification row missing:\n%s", body)
	}
	if dirExists(filepath.Join(vault, "evidence", "APP-T-0001")) {
		t.Fatal("inline proof should not create an evidence directory")
	}
	if err := closeV7Cmd(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "by": "reviewer:agent", "reason": "inline proof accepted", "local": "true"}); err != nil {
		t.Fatal(err)
	}
	data, _, err = parseFrontmatterMustRead(taskPath)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "done", stringField(data, "status"), "closed status")
}

func TestV7VerifyAddParsesEscapedPipeCheck(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	mustV7Proof(t, Args{"vault": vault, "quiet": "true"}, bootstrap)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App", "summary": "Proof policy.", "v7": "true"}, newV7Epic)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Escaped pipe proof", "risk": "low", "priority": "p2", "proof-mode": "inline", "v7": "true"}, newV7Task)

	check := "go test ./... | tee /tmp/proof.log"
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "_pos1": "APP-T-0001", "covers": "A1", "check": check, "result": "pass", "note": "Focused proof passed."}, verifyV7AddCmd)

	taskPath := filepath.Join(vault, "work", "tasks", "APP-T-0001.md")
	data, body, err := parseFrontmatterMustRead(taskPath)
	if err != nil {
		t.Fatal(err)
	}
	rows := parseV7VerificationRows(body)
	if len(rows) != 1 {
		t.Fatalf("expected one verification row, got %#v", rows)
	}
	assertEqual(t, check, rows[0].Check, "verification check round-trip")
	assertEqual(t, "pass", rows[0].Result, "verification result")
	assertEqual(t, "satisfied", stringField(data, "proof_status"), "proof status")
}

func TestV7ProofModeNoneSkipsGeneratedAcceptance(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	mustV7Proof(t, Args{"vault": vault, "quiet": "true"}, bootstrap)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App", "summary": "Proof policy.", "v7": "true"}, newV7Epic)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Planning cleanup", "risk": "low", "priority": "p3", "proof-mode": "none", "v7": "true"}, newV7Task)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "status": "review", "by": "agent:codex", "local": "true"}, statusV7Cmd)

	if err := closeV7Cmd(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "by": "reviewer:agent", "reason": "non-executable work accepted", "local": "true"}); err != nil {
		t.Fatal(err)
	}
	data, _, err := parseFrontmatterMustRead(filepath.Join(vault, "work", "tasks", "APP-T-0001.md"))
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "done", stringField(data, "status"), "closed status")
	assertEqual(t, "satisfied", stringField(data, "proof_status"), "proof status")
}

func TestV7ProofStatusForNoneMarksAcceptanceNotRequired(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	mustV7Proof(t, Args{"vault": vault, "quiet": "true"}, bootstrap)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App", "summary": "Proof policy.", "v7": "true"}, newV7Epic)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Planning cleanup", "risk": "low", "priority": "p3", "proof-mode": "none", "v7": "true"}, newV7Task)

	output := captureStdout(t, func() {
		if err := proofV7StatusCmd(Args{"vault": vault, "id": "APP-T-0001"}); err != nil {
			t.Fatal(err)
		}
	})
	if strings.Contains(output, "pending, no proof") {
		t.Fatalf("proof status should not show pending proof for proof_mode=none:\n%s", output)
	}
	if !strings.Contains(output, "Proof status: satisfied") || !strings.Contains(output, "A1: not required") {
		t.Fatalf("expected proof_mode=none status output, got:\n%s", output)
	}
}

func TestV7ArtifactFinishRequiresEvidenceOrGate(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	mustV7Proof(t, Args{"vault": vault, "quiet": "true"}, bootstrap)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App", "summary": "Proof policy.", "v7": "true"}, newV7Epic)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Artifact proof", "risk": "high", "priority": "p1", "v7": "true"}, newV7Task)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "runner": "codex"}, attemptV7StartCmd)

	err := finishV7Cmd(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "attempt": "APP-T-0001-A-0001", "summary": "Implementation complete.", "local": "true"})
	if err == nil || !strings.Contains(err.Error(), "finish proof incomplete") {
		t.Fatalf("expected finish proof error, got %v", err)
	}

	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "blocks": "APP-T-0001", "kind": "verification", "owner": "human:sarav", "action": "Capture manual artifact proof.", "verification": "Attach screenshot/video proof for A1.", "covers": "A1"}, newV7Gate)
	if err := finishV7Cmd(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "attempt": "APP-T-0001-A-0001", "summary": "Implementation complete; manual proof gated.", "local": "true"}); err != nil {
		t.Fatal(err)
	}
	data, _, err := parseFrontmatterMustRead(filepath.Join(vault, "work", "tasks", "APP-T-0001.md"))
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "waiting_on_human", stringField(data, "readiness"), "artifact task remains gated")
	assertEqual(t, "partial", stringField(data, "proof_status"), "proof status with open gate")
}

func TestV7AttemptHandoffRequiresProofBeforeReview(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	mustV7Proof(t, Args{"vault": vault, "quiet": "true"}, bootstrap)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App", "summary": "Proof policy.", "v7": "true"}, newV7Epic)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Incomplete handoff", "risk": "low", "priority": "p2", "v7": "true"}, newV7Task)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "runner": "codex"}, attemptV7StartCmd)

	err := attemptV7HandoffCmd(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "summary": "Implementation claims ready."})
	if err == nil || !strings.Contains(err.Error(), "finish proof incomplete") {
		t.Fatalf("expected handoff proof error, got %v", err)
	}
	taskData, _, err := parseFrontmatterMustRead(filepath.Join(vault, "work", "tasks", "APP-T-0001.md"))
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "ready", stringField(taskData, "status"), "task status after rejected handoff")
	attemptData, _, err := parseFrontmatterMustRead(filepath.Join(vault, "attempts", "APP-T-0001", "APP-T-0001-A-0001.md"))
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "started", stringField(attemptData, "status"), "attempt status after rejected handoff")
}

func TestV7ValidateRejectsReviewAfterHandoffWithIncompleteProof(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	mustV7Proof(t, Args{"vault": vault, "quiet": "true"}, bootstrap)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App", "summary": "Proof policy.", "v7": "true"}, newV7Epic)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Corrupted review", "risk": "low", "priority": "p2", "v7": "true"}, newV7Task)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "runner": "codex"}, attemptV7StartCmd)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "status": "review", "by": "agent:codex", "local": "true"}, statusV7Cmd)

	attemptPath := filepath.Join(vault, "attempts", "APP-T-0001", "APP-T-0001-A-0001.md")
	data, body, err := parseFrontmatterMustRead(attemptPath)
	if err != nil {
		t.Fatal(err)
	}
	baseRev := stringField(data, "state_rev")
	data["status"] = "handoff"
	if _, err := saveV7DocumentCAS(attemptPath, data, body, v7FrontmatterOrder["attempt"], baseRev); err != nil {
		t.Fatal(err)
	}

	code, err := validateCmd(Args{"vault": vault, "json": "true"})
	if err != nil {
		t.Fatal(err)
	}
	if code == 0 {
		t.Fatal("expected validate to reject handoff-backed review with incomplete proof")
	}
}

func TestV7ValidateAllowsHandoffWaitingOnUnresolvedDependency(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	mustV7Proof(t, Args{"vault": vault, "quiet": "true"}, bootstrap)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App", "summary": "Proof policy.", "v7": "true"}, newV7Epic)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Dependency", "risk": "low", "priority": "p2", "proof-mode": "none", "v7": "true"}, newV7Task)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Blocked handoff", "risk": "low", "priority": "p2", "dependencies": "APP-T-0001", "v7": "true"}, newV7Task)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true"}, reconcileV7Cmd)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "_pos1": "APP-T-0002", "covers": "A1", "check": "go test ./cmd/tusker -run TestV7ValidateAllowsHandoff -count=1", "result": "pass", "note": "Dependent work proof passed."}, verifyV7AddCmd)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "id": "APP-T-0002", "runner": "codex"}, attemptV7StartCmd)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "id": "APP-T-0002", "summary": "Implementation done; dependency still open."}, attemptV7HandoffCmd)

	data, _, err := parseFrontmatterMustRead(filepath.Join(vault, "work", "tasks", "APP-T-0002.md"))
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "ready", stringField(data, "status"), "blocked task status")
	assertEqual(t, "blocked_by_dependency", stringField(data, "readiness"), "blocked task readiness")
	code, err := validateCmd(Args{"vault": vault, "json": "true"})
	if err != nil {
		t.Fatal(err)
	}
	if code != 0 {
		t.Fatal("expected dependency-blocked handoff to validate")
	}
}

func TestV7ProofRequiredClassesAreEnforced(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	mustV7Proof(t, Args{"vault": vault, "quiet": "true"}, bootstrap)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App", "summary": "Proof policy.", "v7": "true"}, newV7Epic)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Normative proof classes", "risk": "high", "priority": "p1", "proof-mode": "artifact", "proof-required": "screenshot,human_signoff", "v7": "true"}, newV7Task)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "runner": "codex"}, attemptV7StartCmd)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "kind": "video", "status": "accepted", "accepted-by": "reviewer:agent", "covers": "A1", "external-url": "https://example.test/proof.mov", "summary": "Video artifact accepted."}, evidenceV7AddCmd)

	err := finishV7Cmd(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "attempt": "APP-T-0001-A-0001", "summary": "Implementation complete.", "local": "true"})
	if err == nil {
		t.Fatal("expected finish to reject missing required proof classes")
	}
	if !strings.Contains(err.Error(), "proof_required:screenshot") || !strings.Contains(err.Error(), "proof_required:human_signoff") {
		t.Fatalf("expected missing proof_required classes, got %v", err)
	}

	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "kind": "screenshot", "status": "accepted", "checked-by": "human:sarav", "covers": "A1", "external-url": "https://example.test/proof.png", "summary": "Screenshot checked."}, evidenceV7AddCmd)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "kind": "human_review", "status": "accepted", "accepted-by": "human:sarav", "covers": "A1", "summary": "Human signoff complete."}, evidenceV7AddCmd)

	if err := finishV7Cmd(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "attempt": "APP-T-0001-A-0001", "summary": "Implementation complete.", "local": "true"}); err != nil {
		t.Fatal(err)
	}
	data, _, err := parseFrontmatterMustRead(filepath.Join(vault, "work", "tasks", "APP-T-0001.md"))
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "review", stringField(data, "status"), "finish moved task to review")
	assertEqual(t, "satisfied", stringField(data, "proof_status"), "proof status")
}

func TestV7ProofReportClassifiesHumanOnlyGapsAsTerminalWait(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	mustV7Proof(t, Args{"vault": vault, "quiet": "true"}, bootstrap)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App", "summary": "Proof policy.", "v7": "true"}, newV7Epic)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Human wait", "risk": "low", "priority": "p2", "proof-mode": "inline", "proof-required": "human_signoff", "v7": "true"}, newV7Task)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "_pos1": "APP-T-0001", "covers": "A1", "check": "go test ./cmd/tusker -run TestV7ProofReportClassifiesHumanOnlyGapsAsTerminalWait -count=1", "result": "pass", "note": "Machine proof passed."}, verifyV7AddCmd)

	report := computeV7ProofReport(vault, mustV7Task(t, vault, "APP-T-0001"), mustIndex(t, vault))
	assertEqual(t, true, report.TerminalWait, "terminal wait")
	assertEqual(t, "stop_until_human_response", report.AgentAction, "agent action")
	assertEqual(t, 0, len(report.MachineMissing), "machine gaps")
	assertEqual(t, []string{"proof_required:human_signoff"}, report.HumanMissing, "human gaps")
}

func TestV7ProofReportClassifiesManualSmokeOwner(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	mustV7Proof(t, Args{"vault": vault, "quiet": "true"}, bootstrap)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App", "summary": "Proof policy.", "v7": "true"}, newV7Epic)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Manual smoke", "risk": "low", "priority": "p2", "proof-mode": "inline", "proof-required": "manual_smoke", "proof-required-owner": "manual_smoke=human:sarav", "v7": "true"}, newV7Task)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "_pos1": "APP-T-0001", "covers": "A1", "check": "go test ./cmd/tusker -run TestV7HumanWaitOwner -count=1", "result": "pass", "note": "Machine proof passed."}, verifyV7AddCmd)

	report := computeV7ProofReport(vault, mustV7Task(t, vault, "APP-T-0001"), mustIndex(t, vault))
	assertEqual(t, true, report.TerminalWait, "terminal wait")
	assertEqual(t, []string{"proof_required:manual_smoke"}, report.HumanMissing, "human gaps")
}

func TestV7HumanOwnedProofRequiresHumanAcceptedArtifact(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	mustV7Proof(t, Args{"vault": vault, "quiet": "true"}, bootstrap)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App", "summary": "Proof policy.", "v7": "true"}, newV7Epic)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Human signoff", "risk": "low", "priority": "p2", "proof-mode": "inline", "proof-required": "human_signoff", "v7": "true"}, newV7Task)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "_pos1": "APP-T-0001", "covers": "A1", "check": "human signoff recorded by agent", "result": "pass", "note": "Agent summary should not satisfy human signoff."}, verifyV7AddCmd)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "kind": "human_review", "status": "accepted", "accepted-by": "reviewer:agent", "covers": "A1", "summary": "Reviewer accepted human signoff."}, evidenceV7AddCmd)

	report := computeV7ProofReport(vault, mustV7Task(t, vault, "APP-T-0001"), mustIndex(t, vault))
	assertEqual(t, []string{"proof_required:human_signoff"}, report.HumanMissing, "human proof gap")
	if report.Status == "satisfied" {
		t.Fatalf("reviewer/agent proof must not satisfy human-owned signoff: %#v", report)
	}

	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "kind": "human_review", "status": "accepted", "accepted-by": "human:sarav", "covers": "A1", "summary": "Human accepted signoff."}, evidenceV7AddCmd)
	report = computeV7ProofReport(vault, mustV7Task(t, vault, "APP-T-0001"), mustIndex(t, vault))
	assertEqual(t, "satisfied", report.Status, "human-accepted proof status")
}

func TestV7ProofReportKeepsMachineProofMissingActionable(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	mustV7Proof(t, Args{"vault": vault, "quiet": "true"}, bootstrap)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App", "summary": "Proof policy.", "v7": "true"}, newV7Epic)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Machine gap", "risk": "low", "priority": "p2", "proof-mode": "inline", "proof-required": "focused_test", "v7": "true"}, newV7Task)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "_pos1": "APP-T-0001", "covers": "A1", "check": "manual review", "result": "pass", "note": "Acceptance covered only."}, verifyV7AddCmd)

	report := computeV7ProofReport(vault, mustV7Task(t, vault, "APP-T-0001"), mustIndex(t, vault))
	assertEqual(t, false, report.TerminalWait, "terminal wait")
	assertEqual(t, []string{"proof_required:focused_test"}, report.MachineMissing, "machine gaps")
}

func TestV7CloseoutWritesHumanWaitCheckpoint(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	mustV7Proof(t, Args{"vault": vault, "quiet": "true"}, bootstrap)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App", "summary": "Proof policy.", "v7": "true"}, newV7Epic)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Closeout wait", "risk": "low", "priority": "p2", "proof-mode": "inline", "proof-required": "human_signoff", "v7": "true"}, newV7Task)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "_pos1": "APP-T-0001", "covers": "A1", "check": "go test ./cmd/tusker -run TestV7CloseoutWritesHumanWaitCheckpoint -count=1", "result": "pass", "note": "Machine proof passed."}, verifyV7AddCmd)

	if err := closeoutV7Cmd(Args{"vault": vault, "quiet": "true", "_pos0": "APP-T-0001", "emit-packet": "true", "validate": "printf validation-ok"}); err != nil {
		t.Fatal(err)
	}
	assertExists(t, filepath.Join(vault, "work", "closeouts", "APP-T-0001-C-0001.md"))
	assertExists(t, filepath.Join(vault, "_generated", "packets", "APP-T-0001.reviewer.md"))
	data, _, err := parseFrontmatterMustRead(filepath.Join(vault, "work", "tasks", "APP-T-0001.md"))
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "review", stringField(data, "status"), "status")
	assertEqual(t, "waiting_on_human", stringField(data, "readiness"), "readiness")
	assertEqual(t, "stop_until_human_response", stringField(data, "agent_action"), "agent action")
	assertEqual(t, "machine_complete_waiting_for_human", stringField(data, "closeout_status"), "closeout status")
	_, latest := latestV7Closeout(mustIndex(t, vault), "APP-T-0001")
	if !latest {
		t.Fatal("expected latest closeout")
	}
}

func TestV7CloseoutAllowsHumanClosePolicyCheckpoint(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	mustV7Proof(t, Args{"vault": vault, "quiet": "true"}, bootstrap)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App", "summary": "Proof policy.", "v7": "true"}, newV7Epic)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Human close policy", "risk": "high", "priority": "p1", "status": "review", "proof-mode": "inline", "proof-required": "focused_test", "v7": "true"}, newV7Task)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "_pos1": "APP-T-0001", "covers": "A1", "check": "go test ./cmd/tusker -run TestV7CloseoutAllowsHumanClosePolicyCheckpoint -count=1", "result": "pass", "note": "Machine proof passed."}, verifyV7AddCmd)

	if err := closeoutV7Cmd(Args{"vault": vault, "quiet": "true", "_pos0": "APP-T-0001", "emit-packet": "true", "validate": "printf validation-ok"}); err != nil {
		t.Fatal(err)
	}
	idx := mustIndex(t, vault)
	closeout, ok := latestV7Closeout(idx, "APP-T-0001")
	if !ok {
		t.Fatal("expected closeout")
	}
	assertEqual(t, []string{"close_policy:human_acceptor"}, normalizeList(closeout.Data["human_missing"]), "human close policy blocker")
	if !v7CloseoutCheckpointValid(vault, mustV7Task(t, vault, "APP-T-0001"), idx, closeout) {
		t.Fatal("expected human close policy checkpoint to be valid")
	}
}

func TestV7CloseoutRequiresValidationAndPacket(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	mustV7Proof(t, Args{"vault": vault, "quiet": "true"}, bootstrap)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App", "summary": "Proof policy.", "v7": "true"}, newV7Epic)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Closeout requirements", "risk": "low", "priority": "p2", "proof-mode": "inline", "proof-required": "human_signoff", "v7": "true"}, newV7Task)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "_pos1": "APP-T-0001", "covers": "A1", "check": "go test ./cmd/tusker -run TestV7CloseoutRequiresValidationAndPacket -count=1", "result": "pass", "note": "Machine proof passed."}, verifyV7AddCmd)

	err := closeoutV7Cmd(Args{"vault": vault, "quiet": "true", "_pos0": "APP-T-0001", "emit-packet": "true"})
	if err == nil || !strings.Contains(err.Error(), "requires --validate") {
		t.Fatalf("expected missing validation error, got %v", err)
	}
	err = closeoutV7Cmd(Args{"vault": vault, "quiet": "true", "_pos0": "APP-T-0001", "validate": "printf validation-ok"})
	if err == nil || !strings.Contains(err.Error(), "requires --emit-packet") {
		t.Fatalf("expected missing packet error, got %v", err)
	}
}

func TestV7CloseoutDoesNotAdvertiseHumanWaitWhenCheckpointWriteFails(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	mustV7Proof(t, Args{"vault": vault, "quiet": "true"}, bootstrap)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App", "summary": "Proof policy.", "v7": "true"}, newV7Epic)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Failed closeout write", "risk": "low", "priority": "p2", "proof-mode": "inline", "proof-required": "human_signoff", "v7": "true"}, newV7Task)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "_pos1": "APP-T-0001", "covers": "A1", "check": "go test ./cmd/tusker -run TestV7CloseoutDoesNotAdvertiseHumanWaitWhenCheckpointWriteFails -count=1", "result": "pass", "note": "Machine proof passed."}, verifyV7AddCmd)
	closeoutDir := filepath.Join(vault, "work", "closeouts")
	if err := os.RemoveAll(closeoutDir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(closeoutDir, []byte("not a directory\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := closeoutV7Cmd(Args{"vault": vault, "quiet": "true", "_pos0": "APP-T-0001", "emit-packet": "true", "validate": "printf validation-ok"})
	if err == nil {
		t.Fatal("expected closeout checkpoint write to fail")
	}
	data, _, readErr := parseFrontmatterMustRead(filepath.Join(vault, "work", "tasks", "APP-T-0001.md"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	if stringField(data, "closeout_status") != "" || stringField(data, "agent_action") == "stop_until_human_response" || strings.HasPrefix(stringField(data, "next_source"), "closeout:") {
		t.Fatalf("task advertised human wait despite failed checkpoint write: %#v", data)
	}
}

func TestV7CloseoutRechecksTerminalStateAfterValidation(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	mustV7Proof(t, Args{"vault": vault, "quiet": "true"}, bootstrap)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App", "summary": "Proof policy.", "v7": "true"}, newV7Epic)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Validation side effect", "risk": "low", "priority": "p2", "proof-mode": "inline", "proof-required": "human_signoff", "v7": "true"}, newV7Task)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "_pos1": "APP-T-0001", "covers": "A1", "check": "go test ./cmd/tusker -run TestV7CloseoutRechecksTerminalStateAfterValidation -count=1", "result": "pass", "note": "Machine proof passed."}, verifyV7AddCmd)
	mutate := `rm ` + filepath.Base(vault) + `/work/tasks/APP-T-0001.md`

	err := closeoutV7Cmd(Args{"vault": vault, "quiet": "true", "_pos0": "APP-T-0001", "emit-packet": "true", "validate": mutate})
	if err == nil || !strings.Contains(err.Error(), "not found after validation") {
		t.Fatalf("expected terminal recheck error, got %v", err)
	}
	if fileExists(filepath.Join(vault, "work", "closeouts", "APP-T-0001-C-0001.md")) {
		t.Fatal("closeout should not be written after validation changes terminal state")
	}
}

func TestV7CloseoutStatusIgnoresStaleCheckpointAgentAction(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	mustV7Proof(t, Args{"vault": vault, "quiet": "true"}, bootstrap)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App", "summary": "Proof policy.", "v7": "true"}, newV7Epic)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Stale closeout", "risk": "low", "priority": "p2", "proof-mode": "inline", "proof-required": "human_signoff", "v7": "true"}, newV7Task)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "_pos1": "APP-T-0001", "covers": "A1", "check": "go test ./cmd/tusker -run TestV7CloseoutStatusIgnoresStaleCheckpointAgentAction -count=1", "result": "pass", "note": "Machine proof passed."}, verifyV7AddCmd)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "_pos0": "APP-T-0001", "emit-packet": "true", "validate": "printf validation-ok"}, closeoutV7Cmd)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "status": "rework", "by": "human:sarav", "reason": "Needs rework.", "local": "true"}, statusV7Cmd)

	output := captureStdout(t, func() {
		if err := closeoutV7StatusCmd(Args{"vault": vault, "id": "APP-T-0001", "json": "true"}); err != nil {
			t.Fatal(err)
		}
	})
	var payload map[string]any
	if err := json.Unmarshal([]byte(output), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["agent_action"] == "stop_until_human_response" {
		t.Fatalf("stale closeout status must not report terminal agent_action: %#v", payload)
	}
	if payload["fingerprint_matches"] == true || payload["checkpoint_valid"] == true {
		t.Fatalf("expected stale closeout fingerprint to be invalid: %#v", payload)
	}
	projected := v7ProjectedTaskState(vault, mustV7Task(t, vault, "APP-T-0001"), mustIndex(t, vault))
	if stringField(projected, "agent_action") == "stop_until_human_response" {
		t.Fatalf("stale closeout projection must not stop agent: %#v", projected)
	}
	code, err := validateCmd(Args{"vault": vault, "json": "true"})
	if err != nil {
		t.Fatal(err)
	}
	if code == 0 {
		t.Fatal("expected validate to reject stale closeout")
	}
}

func TestV7CloseoutFingerprintInvalidatesGateChange(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	mustV7Proof(t, Args{"vault": vault, "quiet": "true"}, bootstrap)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App", "summary": "Proof policy.", "v7": "true"}, newV7Epic)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Gate stale closeout", "risk": "low", "priority": "p2", "proof-mode": "inline", "proof-required": "human_signoff", "v7": "true"}, newV7Task)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "_pos1": "APP-T-0001", "covers": "A1", "check": "go test ./cmd/tusker -run TestV7CloseoutFingerprintInvalidatesGateChange -count=1", "result": "pass", "note": "Machine proof passed."}, verifyV7AddCmd)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "blocks": "APP-T-0001", "kind": "signoff", "owner": "human:sarav", "action": "Sign off.", "verification": "Human signoff recorded.", "covers": "A1"}, newV7Gate)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "_pos0": "APP-T-0001", "emit-packet": "true", "validate": "printf validation-ok"}, closeoutV7Cmd)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "id": "APP-G-0001", "by": "human:sarav", "evidence": "Human signed off."}, func(args Args) error {
		return gateV7Transition(args, "satisfied")
	})

	idx := mustIndex(t, vault)
	task := mustV7Task(t, vault, "APP-T-0001")
	closeout, ok := latestV7Closeout(idx, "APP-T-0001")
	if !ok {
		t.Fatal("expected closeout")
	}
	if v7CloseoutCheckpointValid(vault, task, idx, closeout) {
		t.Fatal("gate state change should invalidate the closeout checkpoint")
	}
}

func TestV7CloseoutFingerprintHashesDirtyRepoContent(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	runGitV7ProofTest(t, repo, "init")
	runGitV7ProofTest(t, repo, "config", "user.email", "test@example.test")
	runGitV7ProofTest(t, repo, "config", "user.name", "Test User")
	if err := os.WriteFile(filepath.Join(repo, "app.txt"), []byte("clean\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitV7ProofTest(t, repo, "add", "app.txt")
	runGitV7ProofTest(t, repo, "commit", "-m", "initial")

	vault := filepath.Join(repo, "tusker")
	mustV7Proof(t, Args{"vault": vault, "quiet": "true"}, bootstrap)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App", "summary": "Proof policy.", "v7": "true"}, newV7Epic)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Dirty repo", "risk": "low", "priority": "p2", "proof-mode": "inline", "proof-required": "human_signoff", "v7": "true"}, newV7Task)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "_pos1": "APP-T-0001", "covers": "A1", "check": "go test ./cmd/tusker -run TestV7CloseoutFingerprintHashesDirtyRepoContent -count=1", "result": "pass", "note": "Machine proof passed."}, verifyV7AddCmd)
	if err := os.WriteFile(filepath.Join(repo, "app.txt"), []byte("dirty one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := closeoutV7Cmd(Args{"vault": vault, "quiet": "true", "_pos0": "APP-T-0001", "emit-packet": "true", "validate": "printf validation-ok"}); err != nil {
		t.Fatal(err)
	}
	idx := mustIndex(t, vault)
	closeout, ok := latestV7Closeout(idx, "APP-T-0001")
	if !ok {
		t.Fatal("expected closeout")
	}
	if !v7CloseoutCheckpointValid(vault, mustV7Task(t, vault, "APP-T-0001"), idx, closeout) {
		t.Fatal("expected closeout valid before dirty content changes again")
	}
	if err := os.WriteFile(filepath.Join(repo, "app.txt"), []byte("dirty two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if v7CloseoutCheckpointValid(vault, mustV7Task(t, vault, "APP-T-0001"), mustIndex(t, vault), closeout) {
		t.Fatal("dirty content change with same git status should invalidate closeout")
	}
}

func TestV7CloseoutRepoStateHashesUntrackedContent(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	runGitV7ProofTest(t, repo, "init")
	vault := filepath.Join(repo, "tusker")
	mustV7Proof(t, Args{"vault": vault, "quiet": "true"}, bootstrap)
	untracked := filepath.Join(repo, "scratch.txt")
	if err := os.WriteFile(untracked, []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	first := v7StateRev(v7CloseoutRepoState(vault), "")
	if err := os.WriteFile(untracked, []byte("two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	second := v7StateRev(v7CloseoutRepoState(vault), "")
	if first == second {
		t.Fatal("untracked content change should alter repo fingerprint state")
	}
}

func TestV7HighRiskReviewWithMachineGapsStaysReviewerOwned(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	mustV7Proof(t, Args{"vault": vault, "quiet": "true"}, bootstrap)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App", "summary": "Proof policy.", "v7": "true"}, newV7Epic)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "High review gaps", "risk": "high", "priority": "p1", "status": "review", "proof-mode": "artifact", "v7": "true"}, newV7Task)

	projected := v7ProjectedTaskState(vault, mustV7Task(t, vault, "APP-T-0001"), mustIndex(t, vault))
	assertEqual(t, "waiting_on_review", stringField(projected, "readiness"), "readiness")
	assertEqual(t, "reviewer", stringField(projected, "next_owner"), "next owner")
	if stringField(projected, "agent_action") == "stop_until_human_response" {
		t.Fatalf("machine gaps must not be hidden by human close policy: %#v", projected)
	}
}

func TestV7VerificationGateCanSatisfyManualProofRequirement(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	mustV7Proof(t, Args{"vault": vault, "quiet": "true"}, bootstrap)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App", "summary": "Proof policy.", "v7": "true"}, newV7Epic)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Verification gate smoke", "risk": "low", "priority": "p2", "proof-mode": "inline", "proof-required": "manual_smoke", "proof-required-owner": "manual_smoke=human:sarav", "v7": "true"}, newV7Task)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "blocks": "APP-T-0001", "kind": "verification", "owner": "human:sarav", "action": "Run manual smoke.", "verification": "Manual smoke passed.", "covers": "A1"}, newV7Gate)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "id": "APP-G-0001", "by": "human:sarav", "evidence": "Manual smoke passed."}, func(args Args) error {
		return gateV7Transition(args, "satisfied")
	})

	report := computeV7ProofReport(vault, mustV7Task(t, vault, "APP-T-0001"), mustIndex(t, vault))
	assertEqual(t, "satisfied", report.Status, "proof status")
	assertEqual(t, 0, len(report.HumanMissing), "human proof gaps")
}

func runGitV7ProofTest(t *testing.T, repo string, args ...string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func TestV7VerificationSummaryDoesNotAutoSatisfyDefaultCardProof(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	mustV7Proof(t, Args{"vault": vault, "quiet": "true"}, bootstrap)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App", "summary": "Proof policy.", "v7": "true"}, newV7Epic)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Docs-only summary", "risk": "low", "priority": "p2", "proof-mode": "card", "v7": "true"}, newV7Task)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "runner": "codex"}, attemptV7StartCmd)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "kind": "verification_summary", "status": "accepted", "accepted-by": "reviewer:agent", "covers": "A1", "summary": "Reviewed docs only."}, evidenceV7AddCmd)

	err := finishV7Cmd(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "attempt": "APP-T-0001-A-0001", "summary": "Implementation complete.", "local": "true"})
	if err == nil {
		t.Fatal("expected finish to reject docs-only verification summary")
	}
	if !strings.Contains(err.Error(), "proof_required:focused_test") || !strings.Contains(err.Error(), "proof_required:broad_test") {
		t.Fatalf("expected focused/broad proof gaps, got %v", err)
	}
}

func TestV7ValidatorRejectsSourceFileEvidenceArtifacts(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	mustV7Proof(t, Args{"vault": vault, "quiet": "true"}, bootstrap)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App", "summary": "Proof policy.", "v7": "true"}, newV7Epic)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Forbidden evidence", "risk": "medium", "priority": "p2", "proof-mode": "card", "v7": "true"}, newV7Task)
	sourcePath := filepath.Join(filepath.Dir(vault), "copied.go")
	if err := os.WriteFile(sourcePath, []byte("package copied\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "kind": "verification_summary", "covers": "A1", "summary": "Copied source should be rejected.", "path": sourcePath, "link-only": "true"}, evidenceV7AddCmd)

	code, err := validateCmd(Args{"vault": vault, "json": "true"})
	if err != nil {
		t.Fatal(err)
	}
	if code == 0 {
		t.Fatal("expected validate to reject source file evidence artifact")
	}
}

func TestV7EvidencePolicyMigrationHonorsEvidenceRequired(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	mustV7Proof(t, Args{"vault": vault, "quiet": "true"}, bootstrap)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App", "summary": "Proof policy.", "v7": "true"}, newV7Epic)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Evidence policy migration", "risk": "medium", "priority": "p2", "evidence-required": "automated_test", "v7": "true"}, newV7Task)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "kind": "automated_test", "covers": "A1", "summary": "Focused tests passed."}, evidenceV7AddCmd)

	taskPath := filepath.Join(vault, "work", "tasks", "APP-T-0001.md")
	data, body, err := parseFrontmatterMustRead(taskPath)
	if err != nil {
		t.Fatal(err)
	}
	baseRev := stringField(data, "state_rev")
	delete(data, "proof_mode")
	delete(data, "proof_status")
	delete(data, "proof_required")
	delete(data, "evidence_budget")
	delete(data, "raw_artifacts_allowed")
	if _, err := saveV7DocumentCAS(taskPath, data, body, v7FrontmatterOrder["task"], baseRev); err != nil {
		t.Fatal(err)
	}

	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "write": "true"}, migrateV7EvidencePolicyCmd)
	after, afterBody, err := parseFrontmatterMustRead(taskPath)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "card", stringField(after, "proof_mode"), "proof mode")
	assertEqual(t, 1, intField(after, "evidence_budget"), "evidence budget")
	assertEqual(t, "satisfied", stringField(after, "proof_status"), "proof status")
	assertContainsIndexTest(t, afterBody, "[[APP-T-0001-E-0001]] automated_test (A1) - Focused tests passed.")
}

func TestV7AttachmentsMigrateMovesLegacyFilesToScratch(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	mustV7Proof(t, Args{"vault": vault, "quiet": "true"}, bootstrap)
	legacyDir := filepath.Join(vault, "Attachments", "APP-T-0001")
	if err := ensureDir(legacyDir); err != nil {
		t.Fatal(err)
	}
	legacyLog := filepath.Join(legacyDir, "raw.log")
	if err := os.WriteFile(legacyLog, []byte("PASS\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := attachmentsV7MigrateCmd(Args{"vault": vault, "_pos0": "migrate", "write": "true", "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	if fileExists(legacyLog) {
		t.Fatal("expected legacy attachment to be moved")
	}
	if dirExists(filepath.Join(vault, "Attachments")) {
		t.Fatal("expected empty Attachments directory to be removed")
	}
	assertExists(t, filepath.Join(vault, ".tusker", "scratch", "APP-T-0001", "legacy-attachments", "raw.log"))
}

func TestV7NoteWalkerSkipsScratchMarkdown(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	mustV7Proof(t, Args{"vault": vault, "quiet": "true"}, bootstrap)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App", "summary": "Proof policy.", "v7": "true"}, newV7Epic)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Scratch duplicate", "risk": "low", "priority": "p2", "proof-mode": "none", "v7": "true"}, newV7Task)
	scratch := filepath.Join(vault, ".tusker", "scratch", "APP-T-0001", "legacy-attachments", "APP-T-0001.md")
	if err := writeText(scratch, "---\nid: APP-T-0001\nkind: task\n---\n\nscratch copy\n"); err != nil {
		t.Fatal(err)
	}

	code, err := validateCmd(Args{"vault": vault, "json": "true"})
	if err != nil {
		t.Fatal(err)
	}
	if code != 0 {
		t.Fatal("expected scratch markdown to be ignored by validation")
	}
}

func mustV7Proof(t *testing.T, args Args, fn func(Args) error) {
	t.Helper()
	if err := fn(args); err != nil {
		t.Fatal(err)
	}
}

func mustV7Task(t *testing.T, vault, taskID string) Note {
	t.Helper()
	note, err := resolveV7Note(vault, taskID, "task")
	if err != nil {
		t.Fatal(err)
	}
	return note
}
