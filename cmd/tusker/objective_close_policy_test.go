package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestObjectiveClosePolicy(t *testing.T) {
	vault := filepath.Join(t.TempDir(), ".tusker")
	if err := bootstrap(Args{"vault": vault, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	if err := newV7Epic(Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "Objective close", "summary": "Risk is proof depth.", "v7": "true"}); err != nil {
		t.Fatal(err)
	}
	for i, risk := range []string{"low", "medium", "high", "critical"} {
		if err := newV7Task(Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Close " + risk, "risk": risk, "priority": "p1", "proof-mode": "inline", "proof-required": "focused_test", "v7": "true"}); err != nil {
			t.Fatalf("create %s: %v", risk, err)
		}
		id := "APP-T-000" + string(rune('1'+i))
		if err := verifyV7AddCmd(Args{"vault": vault, "quiet": "true", "_pos1": id, "by": "reviewer:independent", "covers": "A1", "check": "command: go test ./cmd/tusker -run TestObjectiveClosePolicy -count=1", "result": "pass", "note": "Objective proof passed."}); err != nil {
			t.Fatalf("verify %s: %v", risk, err)
		}
		if err := statusV7Cmd(Args{"vault": vault, "quiet": "true", "id": id, "status": "review", "by": "agent:worker"}); err != nil {
			t.Fatalf("review %s: %v", risk, err)
		}
		if err := closeV7Cmd(Args{"vault": vault, "quiet": "true", "id": id, "by": "reviewer:independent"}); err != nil {
			t.Fatalf("independent reviewer close %s: %v", risk, err)
		}
	}
	if got := defaultV7ProofMode("critical"); got != "audit" {
		t.Fatalf("critical risk should retain audit proof depth, got %q", got)
	}
	required := defaultV7ProofRequired("audit")
	if !containsString(required, "independent_review") || containsString(required, "human_signoff") {
		t.Fatalf("critical proof should strengthen objective review without human signoff: %#v", required)
	}
}

func TestHumanGateBoundary(t *testing.T) {
	valid := []struct{ kind, action, verification, boundary, suggestion string }{
		{"auth", "Provide OAuth credentials.", "Credential probe succeeds.", "The agent lacks the account owner's OAuth secret.", ""},
		{"security", "Grant security approval.", "Security authority records approval.", "Only the security authority can approve this release.", ""},
		{"privacy", "Approve the privacy exception.", "Privacy authority records approval.", "Only the privacy authority can approve the exception.", ""},
		{"legal", "Approve the legal terms.", "Legal authority records approval.", "Only legal authority can accept these terms.", ""},
		{"billing", "Approve the charge.", "Billing authority records approval.", "Only billing authority can approve account spend.", ""},
		{"release", "Authorize the production release.", "Release authority records approval.", "Only the production release authority can deploy.", ""},
		{"destructive_external_action", "Authorize destructive external deletion.", "External records are deleted.", "Only the account owner may authorize destructive external action.", ""},
		{"env", "Run the physical-device check.", "Device result is attached.", "The physical device is unavailable to the agent.", ""},
		{"decision", "Choose the retention rule.", "The decision is recorded.", "The approved specs contradict each other on retention.", "Prefer the shorter retention period."},
		{"subjective_acceptance", "Judge whether the final artifact feels on-brand.", "Subjective brand acceptance is recorded.", "The contract explicitly reserves brand quality for subjective acceptance.", ""},
	}
	for _, tc := range valid {
		if err := validateV7GateCreationPolicy(tc.kind, "human:owner", true, tc.action, tc.verification, tc.boundary, tc.suggestion); err != nil {
			t.Errorf("valid %s gate rejected: %v", tc.kind, err)
		}
	}
	invalid := []string{
		"Perform code review and approve the diff.",
		"Inspect basic test logs.",
		"Approve the objective screenshot and screen recording.",
		"Interpret the benchmark result.",
		"Choose implementation already settled by the spec.",
	}
	for _, action := range invalid {
		err := validateV7GateCreationPolicy("signoff", "human:owner", true, action, "Human confirms implementation.", "This is high risk and a human should approve it.", "")
		if err == nil || !strings.Contains(err.Error(), "agent-capable review work") {
			t.Errorf("agent-capable gate accepted for %q: %v", action, err)
		}
	}

	vault := filepath.Join(t.TempDir(), ".tusker")
	if err := bootstrap(Args{"vault": vault, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	if err := newV7Epic(Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "Gate matrix", "summary": "Gate boundary.", "v7": "true"}); err != nil {
		t.Fatal(err)
	}
	if err := newV7Task(Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Gate target", "risk": "low", "priority": "p2", "v7": "true"}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range valid {
		if err := newV7Gate(Args{"vault": vault, "quiet": "true", "blocks": "APP-T-0001", "kind": tc.kind, "owner": "human:owner", "action": tc.action, "verification": tc.verification, "why-agent-cannot": tc.boundary, "suggestion": tc.suggestion, "covers": "A1"}); err != nil {
			t.Fatalf("persist valid %s gate: %v", tc.kind, err)
		}
	}
	reloaded, err := loadV7Index(vault)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.Gates) != len(valid) {
		t.Fatalf("valid gates did not survive reload: got %d want %d", len(reloaded.Gates), len(valid))
	}
	badFields := map[string]any{"kind": "signoff", "owner": "human:owner", "action": "Approve the objective screenshot.", "verification": "Human confirms screenshot.", "why_agent_cannot": "This is high risk."}
	if _, err := applyV7CreateGateProposal(vault, "APP-T-0001", "task", badFields, "reviewer:agent"); err == nil || !strings.Contains(err.Error(), "agent-capable") {
		t.Fatalf("proposal path accepted invalid gate: %v", err)
	}
	if err := newV7Gate(Args{"vault": vault, "quiet": "true", "blocks": "APP-T-0001", "kind": "signoff", "owner": "human:owner", "action": "Interpret the benchmark result.", "verification": "Human confirms benchmark.", "why-agent-cannot": "This is high risk."}); err == nil || !strings.Contains(err.Error(), "agent-capable") {
		t.Fatalf("CLI path accepted invalid gate: %v", err)
	}
	note := Note{Data: map[string]any{"schema": "tusker.gate/v1", "kind": "gate", "id": "APP-G-0001", "project": "app", "title": "Bad", "gate_kind": "signoff", "status": "open", "owner": "human:owner", "blocking": true, "blocks": []string{"APP-T-0001"}, "covers": []string{"A1"}, "action": "Review code changes.", "verification": "Human approves diff.", "why_agent_cannot": "This is high risk."}}
	var errs, warnings []Issue
	validateV7Gate(note, "work/gates/APP-G-0001.md", &errs, &warnings)
	if !issuesContainCode(errs, "GATE_HUMAN_OWNS_AGENT_CAPABLE_WORK") {
		t.Fatalf("validation path missed invalid gate: %#v", errs)
	}
	// Delivery-plan import uses this same policy function before materializing a gate.
	if err := validateV7GateCreationPolicy("signoff", "human:owner", true, "Approve implementation already settled by the spec.", "Human approves.", "High risk.", ""); err == nil {
		t.Fatal("import-policy path accepted an invalid human gate")
	}
}

func TestObjectiveClosePolicyContract(t *testing.T) {
	wf := defaultWorkflowMarkdown()
	for _, residue := range []string{"human_required_risks", "Human close required", "high/critical risks stay in `review`"} {
		if strings.Contains(wf, residue) {
			t.Fatalf("generated workflow retains risk-based human authority %q", residue)
		}
	}
	for _, path := range []string{"workflow.go", "serve_needs.go", "serve_command.go"} {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		text := string(raw)
		if strings.Contains(text, `risk == "high" || risk == "critical"`) && strings.Contains(text, "human") {
			t.Fatalf("%s retains a high/critical human heuristic", path)
		}
	}
}

func TestClosePolicyMigration(t *testing.T) {
	root := t.TempDir()
	vault := filepath.Join(root, ".tusker")
	if err := bootstrap(Args{"vault": vault, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	data, body, err := parseFrontmatter(defaultWorkflowMarkdown())
	if err != nil {
		t.Fatal(err)
	}
	reviewer := data["reviewer"].(map[string]any)
	reviewer["auto_close_risks"] = []string{"low", "medium"}
	reviewer["human_required_risks"] = []string{"high", "critical"}
	reviewer["prompt"] = "Human close required: {{ reviewer.human_required }}"
	data["reviewer"] = reviewer
	fm, err := stringifyFrontmatter(data, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeText(workflowPath(vault), fm+"\n"+body); err != nil {
		t.Fatal(err)
	}
	if err := writeText(filepath.Join(root, "tusker.yaml"), "close_policy:\n  high:\n    required_acceptor: human\n  critical:\n    required_acceptor: human\n    required_gates: [release, security]\n"); err != nil {
		t.Fatal(err)
	}
	gatePath := filepath.Join(vault, "work", "gates", "APP-G-0001.md")
	gate := "explicit genuine gate remains byte-for-byte\n"
	if err := writeText(gatePath, gate); err != nil {
		t.Fatal(err)
	}
	if _, err := loadWorkflow(vault); err == nil || !strings.Contains(err.Error(), "treats risk as human authority") {
		t.Fatalf("expected explicit legacy diagnostic, got %v", err)
	}
	if err := migrateClosePolicyCmd(Args{"vault": vault, "quiet": "true", "write": "true"}); err != nil {
		t.Fatal(err)
	}
	wf, err := loadWorkflow(vault)
	if err != nil {
		t.Fatal(err)
	}
	for _, risk := range []string{"low", "medium", "high", "critical"} {
		if !reviewerMayAutoCloseRisk(wf.Data.Reviewer, risk) {
			t.Fatalf("migration missed %s", risk)
		}
	}
	if raw, err := os.ReadFile(gatePath); err != nil || string(raw) != gate {
		t.Fatalf("migration changed explicit gate: %q, %v", raw, err)
	}
	if changed, _, err := migratedObjectiveWorkflow(workflowPath(vault)); err != nil || changed {
		t.Fatalf("workflow migration is not idempotent: changed=%v err=%v", changed, err)
	}
	if changed, _, err := migratedObjectiveCloseConfig(filepath.Join(root, "tusker.yaml")); err != nil || changed {
		t.Fatalf("config migration is not idempotent: changed=%v err=%v", changed, err)
	}
}

func TestWorkflowGeneratedContract(t *testing.T) {
	wf := defaultWorkflow()
	if len(wf.Reviewer.HumanRequiredRisks) != 0 {
		t.Fatalf("generated workflow synthesized human risks: %#v", wf.Reviewer.HumanRequiredRisks)
	}
	for _, risk := range []string{"low", "medium", "high", "critical"} {
		if !reviewerMayAutoCloseRisk(wf.Reviewer, risk) {
			t.Fatalf("generated workflow omitted %s", risk)
		}
	}
	if !strings.Contains(wf.Reviewer.Prompt, "every check passes") || !strings.Contains(wf.Reviewer.Prompt, "Explicit blocking gates") {
		t.Fatalf("reviewer prompt lost objective close/gate contract: %q", wf.Reviewer.Prompt)
	}
}
