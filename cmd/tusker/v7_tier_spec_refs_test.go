package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestTierOneDemandingReadySpecRefIsWarningOnly(t *testing.T) {
	vault := v7DispatchTestVault(t)
	if _, err := setProjectLocalConfigWithReadback(vault, "tier", 1); err != nil {
		t.Fatal(err)
	}
	note := Note{Data: map[string]any{
		"schema": "tusker.task/v7", "kind": "task", "id": "APP-T-0001",
		"project": "app", "title": "Demanding", "status": "ready", "readiness": "ready",
		"priority": "p2", "risk": "medium", "next_owner": "agent", "next_action": "Execute.",
	}, RelativePath: "work/tasks/APP-T-0001.md", Body: "# APP-T-0001\n"}

	err, warnings := validateV7Note(note, validationContext{VaultPath: vault, RelativePath: note.RelativePath}, note.RelativePath)
	if issuesContainCode(err, "TASK_SPEC_REF_REQUIRED") {
		t.Fatalf("tier 1 must not error on an absent spec ref: %#v", err)
	}
	if !issuesContainCode(warnings, "TASK_SPEC_REF_REQUIRED") {
		t.Fatalf("tier 1 must warn on an absent spec ref: %#v", warnings)
	}
	if finding, ok := v7DemandingTaskSpecRefIssue(vault, note, note.RelativePath); !ok || !strings.Contains(finding.Message, "should declare") {
		t.Fatalf("tier 1 helper result = %#v, ok=%v", finding, ok)
	}
}

func TestStrictDemandingReadyRequiresResolvableSpecRef(t *testing.T) {
	vault := v7DispatchTestVault(t)
	note := Note{Data: map[string]any{
		"schema": "tusker.task/v7", "kind": "task", "id": "APP-T-0001",
		"project": "app", "title": "Demanding", "status": "ready", "readiness": "ready",
		"priority": "p2", "risk": "medium", "next_owner": "agent", "next_action": "Execute.",
		"spec_refs": []string{"docs/specs/missing.md"},
	}, RelativePath: "work/tasks/APP-T-0001.md", Body: "# APP-T-0001\n"}

	finding, ok := v7DemandingTaskSpecRefIssue(vault, note, note.RelativePath)
	if !ok || !strings.Contains(finding.Message, "resolvable") {
		t.Fatalf("strict helper must reject dangling-only refs: %#v, ok=%v", finding, ok)
	}
	if err := newV7Task(Args{
		"vault": vault, "quiet": "true", "epic": "APP", "title": "Force ready without spec",
		"risk": "medium", "status": "ready", "force-ready": "true",
	}); err == nil || !strings.Contains(err.Error(), "spec_refs") {
		t.Fatalf("--force-ready must not bypass strict spec policy, got %v", err)
	}
}

func TestStrictDemandingReadyAcceptsResolvableRepoSpec(t *testing.T) {
	vault := v7DispatchTestVault(t)
	spec := filepath.Join(v7RepoRoot(vault), "docs", "specs", "linked.md")
	if err := writeText(spec, "# Governing spec\n"); err != nil {
		t.Fatal(err)
	}
	note := Note{Data: map[string]any{
		"schema": "tusker.task/v7", "kind": "task", "id": "APP-T-0001",
		"project": "app", "title": "Demanding", "status": "ready", "readiness": "ready",
		"priority": "p2", "risk": "medium", "next_owner": "agent", "next_action": "Execute.",
		"spec_refs": []string{"docs/specs/linked.md"},
	}, RelativePath: "work/tasks/APP-T-0001.md", Body: "# APP-T-0001\n"}
	if finding, ok := v7DemandingTaskSpecRefIssue(vault, note, note.RelativePath); ok {
		t.Fatalf("resolvable repo spec should satisfy strict helper: %#v", finding)
	}
}
