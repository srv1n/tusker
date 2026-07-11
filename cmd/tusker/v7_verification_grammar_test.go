package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestV7VerificationGrammarIsToolIndependent locks the canonical grammar: a
// Check cell is exact when it starts with a "command:" / "manual proof:" marker
// regardless of the tool named, so new toolchains (bun, swift, compound cd
// pipelines) do not require touching a per-tool allowlist.
func TestV7VerificationGrammarIsToolIndependent(t *testing.T) {
	exact := []string{
		"command: cd internal/serve/ui && bun test",
		"command: swift build -c release",
		"command: cd apps/mac/TuskerBar && swift build -c release && make -C ../../.. mac-app",
		"manual proof: launch the bundle and confirm the menu item opens the browser",
		"go test ./cmd/tusker -run TestFoo -count=1", // legacy bare prefix still honored
	}
	for _, check := range exact {
		if !v7VerificationCheckLooksExact(check) {
			t.Errorf("expected exact verification check: %q", check)
		}
	}

	notExact := []string{
		"command: <exact command that proves A1>",  // unfilled grammar placeholder
		"manual proof: <exact steps a human runs>", // unfilled grammar placeholder
		"TBD",
		"Define the smallest proof that proves acceptance.",
		"",
		"-",
	}
	for _, check := range notExact {
		if v7VerificationCheckLooksExact(check) {
			t.Errorf("expected NON-exact verification check: %q", check)
		}
	}
}

// TestV7VerificationIgnoresMarkersOutsideCheckCell guards the Notes-cell false
// positive: a marker mentioned in the Notes column (or guidance prose) must not
// make an otherwise-vague verification section count as real proof.
func TestV7VerificationIgnoresMarkersOutsideCheckCell(t *testing.T) {
	section := strings.Join([]string{
		"| Covers | Check | Result | Notes |",
		"|---|---|---|---|",
		"| A1 | Run the thing and eyeball it | pending | command: go test ./... would be better |",
	}, "\n")
	if v7TextHasExactVerificationProof(section) {
		t.Fatalf("marker in Notes column must not satisfy exact proof: %q", section)
	}

	filled := strings.Join([]string{
		"| Covers | Check | Result | Notes |",
		"|---|---|---|---|",
		"| A1 | command: go test ./cmd/tusker -run TestFoo -count=1 | pending | real command in Check |",
	}, "\n")
	if !v7TextHasExactVerificationProof(filled) {
		t.Fatalf("marker in Check column must satisfy exact proof: %q", filled)
	}
}

// TestV7TaskTemplateStaysNonExactUntilFilled proves a freshly created task
// cannot be promoted to ready on its placeholder verification row, yet becomes
// valid the moment the placeholder is replaced with a real command — no format
// knowledge required beyond the template itself.
func TestV7TaskTemplateStaysNonExactUntilFilled(t *testing.T) {
	body := v7TaskBody("APP-T-0001", "Template gate")
	section := sectionContent(body, "## Verification")
	if strings.TrimSpace(section) == "" {
		t.Fatal("template must include a Verification section")
	}
	if v7TextHasExactVerificationProof(section) {
		t.Fatalf("template verification must stay non-exact until filled, got exact for:\n%s", section)
	}
	if !strings.Contains(section, "command:") || !strings.Contains(section, "manual proof:") {
		t.Fatalf("template should teach both grammar markers, got:\n%s", section)
	}

	filled := strings.Replace(section,
		"command: <exact command that proves A1>",
		"command: go test ./cmd/tusker -run TestTemplate -count=1", 1)
	if !v7TextHasExactVerificationProof(filled) {
		t.Fatalf("replacing the placeholder with a real command must satisfy exact proof:\n%s", filled)
	}
}

// TestV7VerificationDiagnosticsCarryGrammar proves the failure surfaces the
// accepted grammar at both the validate-warning and dispatch-blocker sites, so
// an agent reading only the error can fix the row.
func TestV7VerificationDiagnosticsCarryGrammar(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "tusker")
	if err := bootstrap(Args{"vault": vault, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	data := map[string]any{
		"schema":                "tusker.task/v7",
		"kind":                  "task",
		"id":                    "APP-T-0001",
		"project":               "tusker",
		"title":                 "Grammar in diagnostics",
		"epic":                  "APP",
		"status":                "ready",
		"readiness":             "ready",
		"priority":              "p2",
		"risk":                  "low",
		"proof_mode":            "inline",
		"proof_status":          "pending",
		"proof_required":        []string{"focused_test"},
		"evidence_budget":       0,
		"raw_artifacts_allowed": false,
		"next_owner":            "agent",
		"next_action":           "Execute the task contract.",
	}
	placeholderBody := strings.Join([]string{
		"## Acceptance",
		"",
		"| ID | Outcome | Proof |",
		"|---|---|---|",
		"| A1 | The widget renders on load. | focused_test |",
		"",
		"## Verification",
		"",
		"| Covers | Check | Result | Notes |",
		"|---|---|---|---|",
		"| A1 | command: <exact command that proves A1> | pending | fill me in |",
	}, "\n")
	note := Note{Data: data, Body: placeholderBody, RelativePath: "work/tasks/APP-T-0001.md"}

	_, warns := validateV7Note(note, validationContext{VaultPath: vault, RelativePath: note.RelativePath}, note.RelativePath)
	var hint string
	for _, w := range warns {
		if w.Code == "VERIFICATION_PROOF_MISSING" {
			hint = w.Hint
		}
	}
	if hint != v7VerificationGrammarHint {
		t.Fatalf("verification warning should carry the grammar hint, got %q", hint)
	}

	blockers := v7TaskDispatchBlockers(vault, note)
	if !blockersContain(blockers, v7VerificationGrammarHint) {
		t.Fatalf("dispatch blocker should state the grammar, got %#v", blockers)
	}

	note.Body = strings.Replace(placeholderBody,
		"command: <exact command that proves A1>",
		"command: go test ./cmd/tusker -run TestWidget -count=1", 1)
	if blockersContain(v7TaskDispatchBlockers(vault, note), v7VerificationGrammarHint) {
		t.Fatalf("filled verification row must clear the verification blocker: %#v", v7TaskDispatchBlockers(vault, note))
	}
}

func blockersContain(blockers []string, substr string) bool {
	for _, b := range blockers {
		if strings.Contains(b, substr) {
			return true
		}
	}
	return false
}
