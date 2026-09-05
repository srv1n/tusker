package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTrustArtifactContract(t *testing.T) {
	vault := t.TempDir()
	task := Note{Data: map[string]any{
		"id": "APP-T-0001", "proof_mode": "inline", "proof_status": "pending", "proof_required": []string{"none"},
		"source_sha": "source-a", "artifact_contract": map[string]any{"kind": "screenshot_set", "acceptance_ids": []string{"A1"}},
	}, Body: trustProofAcceptanceBody()}
	evidence := trustProofEvidence(t, vault, task, "screenshot", []string{"A1"}, "before")
	idx := v7Index{Evidence: map[string][]Note{"APP-T-0001": {evidence}}, Gates: map[string]Note{}}

	if report := computeV7ProofReport(vault, task, idx); report.Status != "satisfied" {
		t.Fatalf("current matching artifact should satisfy proof: %#v", report)
	}

	for name, mutate := range map[string]func(*Note){
		"wrong kind":       func(ev *Note) { ev.Data["evidence_kind"] = "automated_test" },
		"wrong acceptance": func(ev *Note) { ev.Data["covers"] = []string{"A2"} },
		"legacy identity":  func(ev *Note) { delete(ev.Data, "artifact_fingerprint") },
	} {
		t.Run(name, func(t *testing.T) {
			broken := evidence
			broken.Data = cloneMap(evidence.Data)
			mutate(&broken)
			idx.Evidence["APP-T-0001"] = []Note{broken}
			if report := computeV7ProofReport(vault, task, idx); report.Status == "satisfied" || len(report.ModeMissing) == 0 {
				t.Fatalf("%s falsely satisfied artifact contract: %#v", name, report)
			}
		})
	}

	if err := os.WriteFile(filepath.Join(vault, "evidence", "APP-T-0001", "artifacts", "APP-T-0001-E-0001", "before"), []byte("replaced"), 0o644); err != nil {
		t.Fatal(err)
	}
	idx.Evidence["APP-T-0001"] = []Note{evidence}
	if report := computeV7ProofReport(vault, task, idx); report.Status == "satisfied" || len(report.ModeMissing) == 0 {
		t.Fatalf("replaced artifact falsely satisfied contract: %#v", report)
	}
}

func TestTrustProofCategories(t *testing.T) {
	cases := []struct {
		category string
		kind     string
		facts    map[string]string
		paths    []string
	}{
		{"visual", "screenshot", map[string]string{"baseline": "before_after"}, []string{"before.png", "after.png"}},
		{"performance", "benchmark", map[string]string{"before": "10", "after": "8", "before_workload": "100 requests", "after_workload": "100 requests", "units": "ms", "method": "same command", "environment": "local", "revision": "abc123"}, nil},
		{"backend", "integration_test", map[string]string{"observable": "POST returns 201", "negative": "invalid request returns 400"}, nil},
		{"migration", "integration_test", map[string]string{"preservation": "rows preserved", "interruption": "kill during migration", "recovery": "rollback restores rows"}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.category, func(t *testing.T) {
			ev := Note{Data: map[string]any{"evidence_kind": tc.kind, "proof_category": tc.category, "proof_facts": tc.facts, "artifact_paths": tc.paths}}
			if !v7EvidenceSatisfiesProofRequired(tc.category, ev) {
				t.Fatalf("complete %s evidence was rejected", tc.category)
			}
			broken := cloneMap(ev.Data)
			switch tc.category {
			case "visual":
				broken["artifact_paths"] = []string{"after.png"}
			case "performance":
				broken["proof_facts"] = map[string]string{"before": "10", "after": "8", "before_workload": "100 requests", "after_workload": "10 requests", "units": "ms", "method": "same command", "environment": "local", "revision": "abc123"}
			case "backend":
				broken["proof_facts"] = map[string]string{"observable": "POST returns 201"}
			case "migration":
				broken["proof_facts"] = map[string]string{"interruption": "kill during migration", "recovery": "rollback restores rows"}
			}
			if v7EvidenceSatisfiesProofRequired(tc.category, Note{Data: broken}) {
				t.Fatalf("materially misleading %s evidence was accepted", tc.category)
			}
		})
	}
}

func TestTrustReviewCloseout(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	mustV7Proof(t, Args{"vault": vault, "quiet": "true"}, bootstrap)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App", "summary": "Review freshness.", "v7": "true"}, newV7Epic)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Artifact review", "risk": "low", "priority": "p2", "proof-mode": "inline", "proof-required": "none", "v7": "true"}, newV7Task)

	taskPath := filepath.Join(vault, "work", "tasks", "APP-T-0001.md")
	data, body, err := parseFrontmatterMustRead(taskPath)
	if err != nil {
		t.Fatal(err)
	}
	data["artifact_contract"] = map[string]any{"kind": "screenshot_set", "path": "docs/result.md", "summary": "Result", "acceptance_ids": []string{"A1"}}
	if _, err := saveV7DocumentCAS(taskPath, data, body, v7FrontmatterOrder["task"], stringField(data, "state_rev")); err != nil {
		t.Fatal(err)
	}
	task, err := resolveV7Note(vault, "APP-T-0001", "task")
	if err != nil {
		t.Fatal(err)
	}
	evidence := trustProofEvidence(t, vault, task, "screenshot", []string{"A1"}, "review.png")
	writeTrustProofEvidence(t, vault, evidence)
	before, _, err := reviewObjectiveSnapshots(vault, task)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vault, "evidence", "APP-T-0001", "artifacts", "APP-T-0001-E-0001", "review.png"), []byte("changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	after, _, err := reviewObjectiveSnapshots(vault, task)
	if err != nil {
		t.Fatal(err)
	}
	if before == after {
		t.Fatal("artifact replacement must invalidate the review proof snapshot")
	}
}

func trustProofAcceptanceBody() string {
	return "## Acceptance\n\n| ID | Outcome | Proof |\n|---|---|---|\n| A1 | Result. | Proof. |\n"
}

func trustProofEvidence(t *testing.T, vault string, task Note, kind string, covers []string, name string) Note {
	t.Helper()
	id, taskID := "APP-T-0001-E-0001", stringField(task.Data, "id")
	path := filepath.Join(vault, "evidence", taskID, "artifacts", id, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	evidence := Note{Data: map[string]any{
		"id": id, "task": taskID, "evidence_kind": kind, "status": "accepted", "covers": covers,
		"artifact_paths":        []string{strings.TrimPrefix(filepath.ToSlash(strings.TrimPrefix(path, vault+string(filepath.Separator))), "/")},
		"screenshot_checked_by": "reviewer:test", "screenshot_checked_at": "2026-09-05T00:00:00Z",
		"source_revision": firstNonEmpty(stringField(task.Data, "source_sha"), stringField(task.Data, "source_commit")),
	}}
	fingerprint, ok := v7EvidenceArtifactFingerprint(vault, evidence)
	if !ok {
		t.Fatal("test evidence fingerprint unavailable")
	}
	evidence.Data["artifact_fingerprint"] = fingerprint
	return evidence
}

func writeTrustProofEvidence(t *testing.T, vault string, evidence Note) {
	t.Helper()
	evidence.Data["schema"] = "tusker.evidence/v1"
	evidence.Data["kind"] = "evidence"
	evidence.Data["project"] = v7ProjectID(vault)
	evidence.Data["epic"] = "APP"
	evidence.Data["created_by"] = "agent:test"
	evidence.Data["created_at"] = "2026-09-05T00:00:00Z"
	evidence.Data["accepted_by"] = "reviewer:test"
	evidence.Data["accepted_at"] = "2026-09-05T00:00:00Z"
	body := "# Evidence\n\n## Summary\n\nArtifact proof.\n"
	evidence.Data["state_rev"] = v7StateRev(evidence.Data, body)
	content, err := serializeDocument(evidence.Data, body, v7FrontmatterOrder["evidence"])
	if err != nil {
		t.Fatal(err)
	}
	if err := writeText(filepath.Join(vault, "evidence", stringField(evidence.Data, "task"), stringField(evidence.Data, "id")+".md"), content); err != nil {
		t.Fatal(err)
	}
}
