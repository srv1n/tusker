package main

import "testing"

func TestTrustEnforcementLintRejectsUnknownProofRequirement(t *testing.T) {
	vault := v7DispatchTestVault(t)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Valid proof policy"}, newV7Task)
	err := newV7Task(Args{
		"vault": vault, "quiet": "true", "epic": "APP", "title": "Unknown proof", "proof-required": "fabricated_proof", "v7": "true",
	})
	if err == nil || errorToIssue(err).Code != errorInvalidField {
		t.Fatalf("new task accepted an unknown proof requirement: %v", err)
	}

	task := mustV7Task(t, vault, "APP-T-0001")
	task.Data["proof_required"] = []string{"fabricated_proof"}
	var errors, warnings []Issue
	validateV7Task(task, validationContext{}, "work/tasks/APP-T-0001.md", &errors, &warnings)
	if !issuesContainCode(errors, "TASK_PROOF_REQUIRED_UNKNOWN") {
		t.Fatalf("validation did not report the unknown proof class: errors=%#v warnings=%#v", errors, warnings)
	}
}

func TestTrustEnforcementLintRejectsMalformedEvidenceIdentity(t *testing.T) {
	note := Note{Data: map[string]any{
		"schema": "tusker.evidence/v1", "kind": "evidence", "id": "APP-T-0001-E-0001", "project": "app", "task": "APP-T-0001",
		"evidence_kind": "screenshot", "status": "accepted", "covers": []string{"A1"}, "created_by": "agent:worker", "created_at": "2026-09-05T00:00:00Z",
		"artifact_durability": "copied", "artifact_fingerprint": "not-a-fingerprint", "source_revision": "", "proof_category": "visual", "proof_facts": map[string]string{"baseline": "before_after"},
	}}
	var errors, warnings []Issue
	validateV7Evidence(note, validationContext{}, "evidence/APP-T-0001/APP-T-0001-E-0001.md", &errors, &warnings)
	for _, code := range []string{"EVIDENCE_ARTIFACT_FINGERPRINT_INVALID", "EVIDENCE_SOURCE_REVISION_INVALID", "EVIDENCE_PROOF_FACTS_INVALID", "EVIDENCE_ARTIFACT_IDENTITY_MISSING"} {
		if !issuesContainCode(errors, code) {
			t.Fatalf("missing %s: errors=%#v warnings=%#v", code, errors, warnings)
		}
	}
}
