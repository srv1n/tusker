package main

import (
	"os"
	"path/filepath"
	"regexp"
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
		if _, err := upsertV7Verification(vault, id, v7VerificationRow{CoverText: "A1", Check: "command: go test ./cmd/tusker -run TestObjectiveClosePolicy -count=1", Result: "pass", Notes: "Existing gate receipt."}, "reviewer:gate"); err != nil {
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

func TestHumanGateBoundaryAdversarialVocabulary(t *testing.T) {
	invalid := []struct{ kind, action, verification, boundary string }{
		{"release", "Review code changes and authorize the production release.", "Release authority records approval.", "Only the production release authority can deploy."},
		{"security", "Inspect logs and grant security approval.", "Security authority records approval.", "Only the security authority can approve."},
		{"privacy", "Interpret the benchmark and approve the privacy exception.", "Privacy authority records approval.", "Only the privacy authority can approve."},
		{"billing", "Review the diff before approving the charge.", "Billing authority records approval.", "Only billing authority controls account spend."},
		{"subjective_acceptance", "Inspect the objective screenshot.", "Human confirms the screenshot.", "The contract mentions subjective brand quality."},
		{"env", "Capture an objective screen recording.", "The recording is attached.", "The physical device is unavailable to the agent."},
	}
	for _, tc := range invalid {
		if err := validateV7GateCreationPolicy(tc.kind, "human:owner", true, tc.action, tc.verification, tc.boundary, ""); err == nil || !strings.Contains(err.Error(), "agent-capable") {
			t.Errorf("authority vocabulary laundered %s action %q: %v", tc.kind, tc.action, err)
		}
	}
	valid := []struct{ kind, action, verification, boundary string }{
		{"release", "Authorize the production release.", "Release authority records approval.", "Only the release authority can deploy; an independent agent completed code review."},
		{"security", "Grant security approval.", "Security authority records approval.", "Only the security authority can approve; an agent already inspected logs."},
		{"subjective_acceptance", "Judge whether the screenshot feels on-brand.", "Subjective brand acceptance is recorded.", "The contract explicitly reserves this look and feel judgment."},
		{"env", "Use the unavailable physical device to exercise the app.", "The device result is attached.", "The physical device is unavailable to the agent."},
	}
	for _, tc := range valid {
		if err := validateV7GateCreationPolicy(tc.kind, "human:owner", true, tc.action, tc.verification, tc.boundary, ""); err != nil {
			t.Errorf("genuine %s boundary rejected: %v", tc.kind, err)
		}
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
	forbidden := []*regexp.Regexp{
		regexp.MustCompile(`(?i)high(?:\s+and|\s+or|/)?\s*critical[^\n]{0,100}(?:human verification and close|requires? (?:a )?human acceptor|stay[^\n]{0,30}review|route[^\n]{0,20}human)`),
		regexp.MustCompile(`(?i)risk[^\n]{0,80}requires?[^\n]{0,20}human (?:acceptor|acceptance|close|verification|review)`),
		regexp.MustCompile(`(?i)usually human acceptance`),
		regexp.MustCompile(`(?i)human_required_risks\s*:`),
	}
	paths := []string{"../../skill", "../../docs", "../../internal/serve/ui", "../../.tusker/WORKFLOW.md", "../../.tusker/knowledge/domains/project/CANON.md", "../../HANDOFF-dispatch-land-hardening.md", "../../tusker.yaml"}
	for _, root := range paths {
		info, err := os.Stat(root)
		if err != nil {
			t.Fatal(err)
		}
		check := func(path string) error {
			if strings.Contains(filepath.ToSlash(path), "/dist/") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for _, pattern := range forbidden {
				if match := pattern.Find(raw); match != nil {
					t.Errorf("%s retains risk-based human authority: %q", path, match)
				}
			}
			return nil
		}
		if !info.IsDir() {
			if err := check(root); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				if containsString([]string{"dist", "artifacts", "node_modules", ".git", "_generated", "coverage"}, info.Name()) {
					return filepath.SkipDir
				}
				return nil
			}
			if !info.Mode().IsRegular() {
				return nil
			}
			return check(path)
		}); err != nil {
			t.Fatal(err)
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
	legacyConfigPath := filepath.Join(root, "tusker.yaml")
	legacyConfig := "close_policy:\n  high:\n    required_acceptor: human\n  critical:\n    required_acceptor: human\n    required_gates: [release, security]\n"
	if err := writeText(legacyConfigPath, legacyConfig); err != nil {
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
	if got, err := readText(legacyConfigPath); err != nil || got != legacyConfig {
		t.Fatalf("migration rewrote legacy compatibility config: got=%q err=%v", got, err)
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
	if changed, _, err := migratedObjectiveCloseConfig(filepath.Join(vault, "config.yaml")); err != nil || changed {
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
	if !strings.Contains(wf.Reviewer.Prompt, "tusker review submit") || !strings.Contains(wf.Reviewer.Prompt, "Explicit blocking gates") {
		t.Fatalf("reviewer prompt lost typed-result/gate contract: %q", wf.Reviewer.Prompt)
	}
	for _, forbidden := range []string{"tusker status", "tusker merge", "tusker land", "tusker close", "git update-ref", "git checkout"} {
		if strings.Contains(strings.ToLower(wf.Reviewer.Prompt), forbidden) {
			t.Fatalf("generated reviewer prompt retained forbidden authority %q: %q", forbidden, wf.Reviewer.Prompt)
		}
	}
}
