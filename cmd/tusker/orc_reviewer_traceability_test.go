package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestReviewerCanReviewRiskExcludedFromAutoClose(t *testing.T) {
	policy := defaultWorkflow().Reviewer
	policy.AutoCloseRisks = []string{"low"}
	if !reviewerPolicyCoversRisk(policy, "high") {
		t.Fatal("reviewer should still inspect high-risk work when auto-close is excluded")
	}
	if reviewerMayAutoCloseRisk(policy, "high") {
		t.Fatal("high-risk work should not be auto-closed when excluded")
	}
	if err := validateReviewerPolicy(policy, defaultWorkflow().Agents.Enabled, "WORKFLOW.md"); err != nil {
		t.Fatalf("partial auto-close policy should remain valid: %v", err)
	}
}

func TestReviewerProfileWarningsExposeFallbackAndSameVendorModel(t *testing.T) {
	wf := defaultWorkflow()
	wf.RunnerProfiles = map[string]RunnerProfileDefinition{
		"execute": {Harness: string(RunnerCodexExec), Model: "gpt-5.x", Sandbox: RunnerSandboxDefinition{Mode: "workspace-write"}},
		"review":  {Harness: string(RunnerCodexACP), Model: "gpt-5.x", Sandbox: RunnerSandboxDefinition{Mode: "read-only"}},
	}
	wf.RunnerLaneProfiles = map[string]string{runLaneExecute: "execute", runLaneReview: "review"}
	note := Note{Data: map[string]any{"id": "APP-T-0001", "title": "same vendor", "risk": "medium"}}
	selected, err := resolveRunProfileForLane(note, wf, runLaneReview, "")
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(selected.Warnings, "\n")
	if !strings.Contains(joined, "reviewer vendor") || !strings.Contains(joined, "matches implementer vendor") {
		t.Fatalf("same-vendor warning missing: %#v", selected.Warnings)
	}
	if !strings.Contains(joined, "reviewer model") {
		t.Fatalf("same-model warning missing: %#v", selected.Warnings)
	}
	wf.Reviewer.FallbackWarning = `configured reviewer runner "claude-code" is unavailable; fell back to agents.default "codex_exec"`
	selected, err = resolveRunProfileForLane(note, wf, runLaneReview, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(selected.Warnings, "\n"), "fell back to agents.default") {
		t.Fatalf("fallback warning missing: %#v", selected.Warnings)
	}
}

func TestDemandingReadyTaskRequiresSpecRefInValidationAndStatus(t *testing.T) {
	vault := filepath.Join(t.TempDir(), ".tusker")
	if err := bootstrap(Args{"vault": vault, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	if err := newV7Epic(Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App", "summary": "App", "v7": "true"}); err != nil {
		t.Fatal(err)
	}
	if err := newV7Task(Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Needs spec", "risk": "medium", "priority": "p2"}); err != nil {
		t.Fatal(err)
	}
	task, err := resolveV7Note(vault, "APP-T-0001", "task")
	if err != nil {
		t.Fatal(err)
	}
	task.Data["status"], task.Data["readiness"] = "ready", "ready"
	errs, _ := validateV7Note(task, validationContext{VaultPath: vault, RelativePath: task.RelativePath}, task.RelativePath)
	if !issuesContainCode(errs, "TASK_SPEC_REF_REQUIRED") {
		t.Fatalf("validation did not block demanding ready task: %#v", errs)
	}
	err = statusV7Cmd(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "status": "ready", "by": "human:test", "local": "true"})
	if err == nil || !strings.Contains(err.Error(), "spec_refs") {
		t.Fatalf("status ready should refuse missing spec ref, got %v", err)
	}
}
