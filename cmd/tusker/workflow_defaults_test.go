package main

import (
	"strings"
	"testing"
)

// seedWorkflow writes the default (fresh-init) WORKFLOW into a temp vault and
// returns the raw file text.
func seedWorkflow(t *testing.T) (string, string) {
	t.Helper()
	vault := t.TempDir()
	if err := writeDefaultWorkflow(vault); err != nil {
		t.Fatalf("writeDefaultWorkflow: %v", err)
	}
	text, err := readText(workflowPath(vault))
	if err != nil {
		t.Fatalf("readText: %v", err)
	}
	return vault, text
}

func TestInitSeedsProofDefaults(t *testing.T) {
	_, text := seedWorkflow(t)

	data, _, err := parseFrontmatter(text)
	if err != nil {
		t.Fatalf("parseFrontmatter: %v", err)
	}
	proof, ok := data["proof"].(map[string]any)
	if !ok {
		t.Fatalf("expected a proof policy block, got %T", data["proof"])
	}
	if got := proof["proof_mode"]; got != "inline" {
		t.Fatalf("proof_mode = %v, want inline", got)
	}
	// The block must state the actual critical -> audit carve-out.
	if got := proof["proof_mode_critical"]; got != "audit" {
		t.Fatalf("proof_mode_critical = %v, want audit", got)
	}
	if got, ok := proof["evidence_budget"].(int); !ok || got != 0 {
		t.Fatalf("evidence_budget = %v, want 0", proof["evidence_budget"])
	}
	modesRaw, ok := proof["evidence_bearing_modes"].([]any)
	if !ok {
		t.Fatalf("expected evidence_bearing_modes list, got %T", proof["evidence_bearing_modes"])
	}
	modes := map[string]bool{}
	for _, m := range modesRaw {
		modes[strings.TrimSpace(m.(string))] = true
	}
	for _, want := range []string{"card", "artifact", "audit"} {
		if !modes[want] {
			t.Fatalf("evidence_bearing_modes missing %q; got %v", want, modesRaw)
		}
	}
	if len(modes) != 3 {
		t.Fatalf("evidence_bearing_modes should be exactly {card, artifact, audit}; got %v", modesRaw)
	}
}

func TestInitSeedsGateStanzaWithMeasuredDiskComment(t *testing.T) {
	_, text := seedWorkflow(t)

	data, _, err := parseFrontmatter(text)
	if err != nil {
		t.Fatalf("parseFrontmatter: %v", err)
	}
	orch, ok := data["orchestration"].(map[string]any)
	if !ok {
		t.Fatalf("expected orchestration block, got %T", data["orchestration"])
	}
	gate, ok := orch["gate"].(map[string]any)
	if !ok {
		t.Fatalf("expected orchestration.gate stanza, got %T", orch["gate"])
	}
	if got := gate["profile"]; got != "default" {
		t.Fatalf("gate.profile placeholder = %v, want default", got)
	}
	// min_free_disk_gb must ship COMMENTED OUT: no active, unmeasured floor is
	// inherited.
	if _, present := gate["min_free_disk_gb"]; present {
		t.Fatalf("gate.min_free_disk_gb must be a commented placeholder, not an active value: %v", gate["min_free_disk_gb"])
	}
	if !strings.Contains(text, "# min_free_disk_gb: <measured-peak-build-gb>") {
		t.Fatal("gate stanza missing commented-out min_free_disk_gb placeholder")
	}

	// The min_free_disk_gb comment must demand a MEASURED value and cite the
	// 2026-07-20 unmeasured-15 GB doomed run.
	for _, want := range []string{"MEASURED", "min_free_disk_gb", "2026-07-20", "15 GB", "full disk", "build cache"} {
		if !strings.Contains(text, want) {
			t.Fatalf("gate stanza missing required comment text %q", want)
		}
	}
}

func TestReinitDoesNotOverwriteExistingWorkflow(t *testing.T) {
	vault := t.TempDir()
	if err := writeDefaultWorkflow(vault); err != nil {
		t.Fatalf("writeDefaultWorkflow: %v", err)
	}
	path := workflowPath(vault)

	// Simulate a repo that has hand-edited its proof/gate declarations.
	original, err := readText(path)
	if err != nil {
		t.Fatalf("readText: %v", err)
	}
	edited := strings.NewReplacer(
		"proof_mode: inline", "proof_mode: card",
		"profile: default", "profile: canonical",
	).Replace(original)
	if edited == original {
		t.Fatal("test setup failed to alter proof/gate declarations")
	}
	if err := writeText(path, edited); err != nil {
		t.Fatalf("writeText: %v", err)
	}

	// Re-running init/sync without an explicit force/overwrite must be inert.
	if err := writeDefaultWorkflow(vault); err != nil {
		t.Fatalf("re-init writeDefaultWorkflow: %v", err)
	}
	after, err := readText(path)
	if err != nil {
		t.Fatalf("readText after re-init: %v", err)
	}
	if after != edited {
		t.Fatalf("re-init overwrote existing WORKFLOW proof/gate declarations")
	}
	if !strings.Contains(after, "proof_mode: card") || !strings.Contains(after, "profile: canonical") {
		t.Fatal("re-init did not preserve the repo's edited proof/gate declarations")
	}
}
