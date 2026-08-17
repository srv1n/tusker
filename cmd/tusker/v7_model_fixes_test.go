package main

import (
	"path/filepath"
	"testing"
)

func TestV7KindKeyedFrontmatterCleanup(t *testing.T) {
	// Pruning must stay legacy-`type`-keyed: V7 records compute state_rev over
	// full frontmatter before serialization, so pruning V7 kinds desyncs the
	// stored rev from the written file (see pruneEmptyOptionalFrontmatter).
	tests := []struct {
		name   string
		data   map[string]any
		key    string
		pruned bool
	}{
		{name: "legacy type", data: map[string]any{"type": "task", "domains": []any{}}, key: "domains", pruned: true},
		{name: "v7 kind", data: map[string]any{"schema": "tusker.task/v7", "kind": "task", "domains": []any{}}, key: "domains", pruned: false},
		{name: "unmanaged kind", data: map[string]any{"schema": "tusker.gate/v1", "kind": "gate", "tags": []any{}}, key: "tags", pruned: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			removed := pruneEmptyOptionalFrontmatter(tt.data)
			if tt.pruned != (len(removed) > 0) {
				t.Fatalf("removed=%v, want pruned=%t", removed, tt.pruned)
			}
			_, present := tt.data[tt.key]
			if present == tt.pruned {
				t.Fatalf("%s present=%t after pruning, want %t", tt.key, present, !tt.pruned)
			}
		})
	}
}

func TestV7KindKeyedCanonicalSanitization(t *testing.T) {
	legacyFields := map[string]any{
		"dispatch_state":  "claimed",
		"claimed_by":      "agent",
		"claimed_at":      "today",
		"run_attempts":    2,
		"last_attempt_at": "today",
		"failure_class":   "timeout",
	}
	for _, tt := range []struct {
		name                  string
		data                  map[string]any
		legacyFieldsRemaining bool
	}{
		{name: "legacy type", data: map[string]any{"type": "task"}, legacyFieldsRemaining: false},
		{name: "v7 kind", data: map[string]any{"schema": "tusker.task/v7", "kind": "task"}, legacyFieldsRemaining: false},
		{name: "unmanaged kind", data: map[string]any{"schema": "tusker.gate/v1", "kind": "gate"}, legacyFieldsRemaining: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			for key, value := range legacyFields {
				tt.data[key] = value
			}
			sanitizeCanonicalNoteData(tt.data)
			for key := range legacyFields {
				_, present := tt.data[key]
				if present != tt.legacyFieldsRemaining {
					t.Errorf("%s present=%t, want %t", key, present, tt.legacyFieldsRemaining)
				}
			}
		})
	}
}

func TestV7CapsuleValidationUsesCanonicalPolicy(t *testing.T) {
	for _, tt := range []struct {
		name     string
		schema   string
		kind     string
		required bool
	}{
		{name: "doc", schema: "tusker.doc/v7", kind: "doc", required: true},
		{name: "spec", schema: "tusker.spec/v7", kind: "spec", required: true},
		{name: "task", schema: "tusker.task/v7", kind: "task", required: false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			note := Note{Data: map[string]any{"schema": tt.schema, "kind": tt.kind}}
			var errors, warnings []Issue
			validateV7Capsule(note, validationContext{}, tt.name, &errors, &warnings)
			if got := issuesContainCode(warnings, "CAPSULE_MISSING"); got != tt.required {
				t.Fatalf("CAPSULE_MISSING=%t, want %t; errors=%#v warnings=%#v", got, tt.required, errors, warnings)
			}
		})
	}

	arrayNote := Note{Data: map[string]any{
		"schema": "tusker.knowledge/v7",
		"kind":   "runbook",
		"capsule": map[string]any{
			"what":      "one",
			"use_when":  []string{"two", "three"},
			"skip_when": "four",
		},
	}}
	capsule, present, valid := v7CapsuleFromData(arrayNote.Data)
	if !present || !valid || capsule.UseWhen != "two; three" {
		t.Fatalf("array capsule parsing: present=%t valid=%t capsule=%#v", present, valid, capsule)
	}

	boundary := Note{Data: map[string]any{
		"schema":  "tusker.knowledge/v7",
		"kind":    "runbook",
		"capsule": v7CapsuleOrdered(longTokenString(160), "", ""),
	}}
	var errors, warnings []Issue
	validateV7Capsule(boundary, validationContext{}, "knowledge/runbook.md", &errors, &warnings)
	if issuesContainCode(errors, "CAPSULE_TOKEN_BUDGET_EXCEEDED") || !issuesContainCode(warnings, "CAPSULE_TOKEN_BUDGET_WARN") {
		t.Fatalf("boundary should warn without failing: errors=%#v warnings=%#v", errors, warnings)
	}

	v7Errors, v7Warnings := validateV7Note(v7CapsuleKnowledgeNote(v7CapsuleOrdered(longTokenString(81), "", "")), validationContext{}, "knowledge/domains/project/runbooks/capsule.md")
	canonicalWarnings := 0
	for _, current := range v7Warnings {
		if current.Code == "CAPSULE_TOKEN_BUDGET_WARN" {
			canonicalWarnings++
		}
	}
	if canonicalWarnings != 1 || issuesContainCode(v7Warnings, "CAPSULE_LONG") || issuesContainCode(v7Errors, "CAPSULE_TOO_LONG") {
		t.Fatalf("V7 validation should use only canonical capsule issues: errors=%#v warnings=%#v", v7Errors, v7Warnings)
	}

	var legacyErrors, legacyWarnings []Issue
	validateCapsule(Note{Data: map[string]any{"schema": "tusker.doc/v7", "kind": "doc"}}, "", "docs/spec.md", true, &legacyErrors, &legacyWarnings)
	if !issuesContainCode(legacyWarnings, "CAPSULE_MISSING") || issuesContainCode(legacyWarnings, "CAPSULE_LONG") {
		t.Fatalf("legacy entry point should delegate V7 capsule validation: errors=%#v warnings=%#v", legacyErrors, legacyWarnings)
	}
}

func TestV7AbsentEvidenceBudgetHasNoCap(t *testing.T) {
	vault := t.TempDir()
	evidencePath := filepath.Join(vault, "evidence", "APP-T-0001-E-0001.md")
	if err := ensureDir(filepath.Dir(evidencePath)); err != nil {
		t.Fatal(err)
	}
	if err := writeText(evidencePath, "---\nschema: tusker.evidence/v1\nkind: evidence\nid: APP-T-0001-E-0001\ntask: APP-T-0001\nstatus: pending\nevidence_kind: diff_summary\ncovers: [A1]\n---\n"); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name       string
		configured bool
		exceeded   bool
	}{
		{name: "absent", configured: false, exceeded: false},
		{name: "zero cap", configured: true, exceeded: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			data := map[string]any{
				"schema":         "tusker.task/v7",
				"kind":           "task",
				"id":             "APP-T-0001",
				"status":         "ready",
				"proof_mode":     "card",
				"proof_status":   "pending",
				"proof_required": []string{"focused_test"},
			}
			if tt.configured {
				data["evidence_budget"] = 0
			}
			var errors, warnings []Issue
			validateV7TaskProofPolicy(Note{Data: data}, validationContext{VaultPath: vault}, "work/tasks/APP-T-0001.md", &errors, &warnings)
			got := issuesContainCode(errors, "EVIDENCE_BUDGET_EXCEEDED") || issuesContainCode(warnings, "EVIDENCE_BUDGET_EXCEEDED")
			if got != tt.exceeded {
				t.Fatalf("EVIDENCE_BUDGET_EXCEEDED=%t, want %t: errors=%#v warnings=%#v", got, tt.exceeded, errors, warnings)
			}
		})
	}
}

func TestV7BlockingGatesAndAcceptanceWarnings(t *testing.T) {
	vault := t.TempDir()
	gatePath := filepath.Join(vault, "work", "gates", "FIX-G-0001.md")
	if err := ensureDir(filepath.Dir(gatePath)); err != nil {
		t.Fatal(err)
	}
	if err := writeText(gatePath, "---\nschema: tusker.gate/v1\nkind: gate\nid: FIX-G-0001\nstatus: open\nblocking: false\nblocks: [APP-T-0001]\n---\n"); err != nil {
		t.Fatal(err)
	}
	if gates := findV7BlockingGates(vault, "APP-T-0001", []string{"FIX-G-0001"}); len(gates) != 1 {
		t.Fatalf("attached gate missing from lookup: %#v", gates)
	}

	note := Note{Data: map[string]any{
		"schema":         "tusker.task/v7",
		"kind":           "task",
		"id":             "APP-T-0001",
		"project":        "app",
		"title":          "Acceptance warning",
		"status":         "cancelled",
		"readiness":      "cancelled",
		"risk":           "low",
		"priority":       "p2",
		"gates":          []string{"FIX-G-0001"},
		"discarded_by":   "agent:test",
		"discarded_at":   "2026-08-17T00:00:00Z",
		"discard_reason": "No longer needed.",
	}, Body: "## Intent\n\nTest.\n\n## Acceptance\n\n- [ ] Ship the fix.\n\n## Verification\n\ncommand: go test ./cmd/tusker -run TestV7Model -count=1\n"}
	var errors, warnings []Issue
	validateV7Task(note, validationContext{VaultPath: vault}, "work/tasks/APP-T-0001.md", &errors, &warnings)
	if !issuesContainCode(errors, "DISCARDED_TASK_OPEN_GATE") {
		t.Fatalf("discarded task with open non-blocking gate should fail: errors=%#v warnings=%#v", errors, warnings)
	}
	if !issuesContainCode(warnings, "ACCEPTANCE_ID_MISSING") {
		t.Fatalf("bullet acceptance without A<n> should warn: errors=%#v warnings=%#v", errors, warnings)
	}
}

func TestV7ModelTaskWriteRoundTripKeepsStateRev(t *testing.T) {
	vault := pickupV7TestVault(t)
	if err := newV7Task(Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "State revision task", "risk": "low", "priority": "p2", "v7": "true"}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(vault, "work", "tasks", "APP-T-0001.md")
	data, body, err := parseFrontmatterMustRead(path)
	if err != nil {
		t.Fatal(err)
	}
	baseRev := stringField(data, "state_rev")
	if !v7StateRevMatches(data, body, baseRev) {
		t.Fatal("created task state_rev does not match its content")
	}
	data["title"] = "Updated state revision task"
	if _, err := saveV7DocumentCAS(path, data, body, v7FrontmatterOrder["task"], baseRev); err != nil {
		t.Fatal(err)
	}
	rereadData, rereadBody, err := parseFrontmatterMustRead(path)
	if err != nil {
		t.Fatal(err)
	}
	if !v7StateRevMatches(rereadData, rereadBody, stringField(rereadData, "state_rev")) {
		t.Fatal("saved task state_rev does not match its re-read content")
	}
}
