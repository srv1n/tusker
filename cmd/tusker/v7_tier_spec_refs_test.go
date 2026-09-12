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

func TestReadyCreateReportsAllContractBlockersAtOnce(t *testing.T) {
	vault := v7DispatchTestVault(t)
	err := newV7Task(Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Incomplete ready task", "risk": "medium", "status": "ready"})
	if err == nil {
		t.Fatal("incomplete ready task was accepted")
	}
	message := err.Error()
	for _, want := range []string{"spec_refs", "placeholder acceptance", "verification missing exact command"} {
		if !strings.Contains(message, want) {
			t.Fatalf("combined refusal missing %q: %v", want, err)
		}
	}
}

func TestStrictDemandingReadyAcceptsResolvableRepoSpec(t *testing.T) {
	vault := v7DispatchTestVault(t)
	spec := filepath.Join(v7RepoRoot(vault), ".tusker", "specs", "linked.md")
	if err := writeText(spec, "---\ntitle: Governing spec\nsubject: linked\nstatus: canonical\nread_when: Testing required references.\nskip_when: Never.\n---\n\n# Governing spec\n"); err != nil {
		t.Fatal(err)
	}
	note := Note{Data: map[string]any{
		"schema": "tusker.task/v7", "kind": "task", "id": "APP-T-0001",
		"project": "app", "title": "Demanding", "status": "ready", "readiness": "ready",
		"priority": "p2", "risk": "medium", "next_owner": "agent", "next_action": "Execute.",
		"spec_refs": []string{".tusker/specs/linked.md"},
	}, RelativePath: "work/tasks/APP-T-0001.md", Body: "# APP-T-0001\n"}
	if finding, ok := v7DemandingTaskSpecRefIssue(vault, note, note.RelativePath); ok {
		t.Fatalf("resolvable repo spec should satisfy strict helper: %#v", finding)
	}
}

func TestStrictDemandingReadyRequiresEverySpecRefAndExactAnchor(t *testing.T) {
	vault := v7DispatchTestVault(t)
	spec := filepath.Join(v7RepoRoot(vault), ".tusker", "specs", "linked.md")
	if err := writeText(spec, "---\ntitle: Governing spec\nsubject: linked\nstatus: canonical\nread_when: Testing exact references.\nskip_when: Never.\n---\n\n# Governing spec\n\n## Locked interface\n\nUse the existing contract.\n"); err != nil {
		t.Fatal(err)
	}
	note := Note{Data: map[string]any{
		"schema": "tusker.task/v7", "kind": "task", "id": "APP-T-0001",
		"project": "app", "title": "Demanding", "status": "ready", "readiness": "ready",
		"priority": "p2", "risk": "medium", "next_owner": "agent", "next_action": "Execute.",
		"spec_refs": []string{".tusker/specs/linked.md#Locked interface", ".tusker/specs/missing.md"},
	}, RelativePath: "work/tasks/APP-T-0001.md", Body: "# APP-T-0001\n"}
	if _, ok := v7DemandingTaskSpecRefIssue(vault, note, note.RelativePath); !ok {
		t.Fatal("one valid ref must not hide another missing mandatory ref")
	}

	note.Data["spec_refs"] = []string{".tusker/specs/linked.md#Missing interface"}
	if _, ok := v7DemandingTaskSpecRefIssue(vault, note, note.RelativePath); !ok {
		t.Fatal("missing section anchor must block strict readiness")
	}

	note.Data["spec_refs"] = []string{".tusker/specs/linked.md#Locked interface"}
	idx, err := loadV7Index(vault)
	if err != nil {
		t.Fatalf("fixture index: %v", err)
	}
	if !v7SpecRefExists(vault, ".tusker/specs/linked.md#Locked interface", idx.Decisions) {
		raw, _ := readText(spec)
		t.Fatalf("anchored ref did not resolve (section=%v)", v7SpecRefSectionExists(raw, "Locked interface"))
	}
	if finding, ok := v7DemandingTaskSpecRefIssue(vault, note, note.RelativePath); ok {
		t.Fatalf("exact existing anchor should resolve: %#v (normalized=%q clean=%q anchor=%q)", finding,
			v7NormalizeSpecRef(".tusker/specs/linked.md#Locked interface"),
			v7CleanSpecRef(".tusker/specs/linked.md#Locked interface"),
			v7SpecRefAnchor(".tusker/specs/linked.md#Locked interface"))
	}
	packetRefs := v7SpecRefsPacketSection(vault, note)
	if !strings.Contains(packetRefs, ".tusker/specs/linked.md#Locked interface") {
		t.Fatalf("packet lost exact section anchor: %s", packetRefs)
	}
}
