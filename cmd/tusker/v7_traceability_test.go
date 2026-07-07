package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestV7SpecRefsFrontmatterAndValidation(t *testing.T) {
	vault := v7TraceabilityTestVault(t)
	writeTraceabilitySpec(t, vault, "docs/specs/linked.md", "# Linked spec\n")
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Trace decision", "decision": "Use linked canon."}, newV7Decision)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Linked task", "spec-refs": "docs/specs/linked.md,APP-D-0001,docs/specs/missing.md"}, newV7Task)

	taskData, _, err := parseFrontmatterMustRead(filepath.Join(vault, "work", "tasks", "APP-T-0001.md"))
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, []string{"docs/specs/linked.md", "APP-D-0001", "docs/specs/missing.md"}, normalizeList(taskData["spec_refs"]), "task spec_refs")

	epicPath := filepath.Join(vault, "work", "epics", "APP.md")
	epicData, epicBody, err := parseFrontmatterMustRead(epicPath)
	if err != nil {
		t.Fatal(err)
	}
	epicData["spec_refs"] = []string{"docs/design/missing-epic.md"}
	epicData["state_rev"] = v7StateRev(epicData, epicBody)
	content, err := serializeDocument(epicData, epicBody, v7FrontmatterOrder["epic"])
	if err != nil {
		t.Fatal(err)
	}
	if err := writeText(epicPath, content); err != nil {
		t.Fatal(err)
	}

	notes, err := listAllNotes(vault)
	if err != nil {
		t.Fatal(err)
	}
	warnings := validateV7SpecTraceability(vault, notes)
	if !issuesContainCode(warnings, "SPEC_REF_DANGLING") {
		t.Fatalf("expected dangling spec ref warnings, got %#v", warnings)
	}
	assertIssueMessageContains(t, warnings, "docs/specs/missing.md")
	assertIssueMessageContains(t, warnings, "docs/design/missing-epic.md")
}

func TestV7WorkStreamsValidationWarnsOnUnknownWorkIDs(t *testing.T) {
	vault := v7TraceabilityTestVault(t)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Known task"}, newV7Task)
	writeTraceabilitySpec(t, vault, "docs/design/linked-work.md", strings.Join([]string{
		"# Linked work",
		"",
		"## Work streams",
		"",
		"- [[APP-T-0001]] is valid.",
		"- [[APP-T-9999]] is stale.",
		"- [[ZZZ]] is not a known epic.",
		"",
	}, "\n"))

	notes, err := listAllNotes(vault)
	if err != nil {
		t.Fatal(err)
	}
	warnings := validateV7SpecTraceability(vault, notes)
	if !issuesContainCode(warnings, "WORK_STREAM_REF_DANGLING") {
		t.Fatalf("expected dangling Work streams warnings, got %#v", warnings)
	}
	assertIssueMessageContains(t, warnings, "APP-T-9999")
	assertIssueMessageContains(t, warnings, "ZZZ")
	if issueMessageContains(warnings, "APP-T-0001") {
		t.Fatalf("known task should not warn, got %#v", warnings)
	}
}

func TestV7SpecRefsSurfaceInCapsulePacketAndAutomationPlan(t *testing.T) {
	vault := v7TraceabilityTestVault(t)
	writeTraceabilitySpec(t, vault, "docs/specs/linked.md", "# Linked spec\n")
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Trace decision", "decision": "Use linked canon."}, newV7Decision)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Linked task", "spec-refs": "docs/specs/linked.md,APP-D-0001"}, newV7Task)

	capsule := captureStdout(t, func() {
		if err := showCmd(Args{"vault": vault, "_pos0": "APP-T-0001", "capsule": "true"}); err != nil {
			t.Fatal(err)
		}
	})
	assertContainsIndexTest(t, capsule, "Read next spec refs")
	assertContainsIndexTest(t, capsule, "docs/specs/linked.md")
	assertContainsIndexTest(t, capsule, "APP-D-0001")

	packet := captureStdout(t, func() {
		if err := packetV7Cmd(Args{"vault": vault, "id": "APP-T-0001", "for": "agent", "force": "true"}); err != nil {
			t.Fatal(err)
		}
	})
	assertContainsIndexTest(t, packet, "## Governing specs / decisions")
	assertContainsIndexTest(t, packet, "Read these governing specs/decisions before implementation")
	assertContainsIndexTest(t, packet, "docs/specs/linked.md")
	assertContainsIndexTest(t, packet, "APP-D-0001")
	assertContainsIndexTest(t, packet, ".tusker/work/decisions/APP-D-0001.md")

	taskData, taskBody, err := parseFrontmatterMustRead(filepath.Join(vault, "work", "tasks", "APP-T-0001.md"))
	if err != nil {
		t.Fatal(err)
	}
	reads := automationPlanRequiredReads(vault, Note{Data: taskData, Body: taskBody, RelativePath: "work/tasks/APP-T-0001.md"})
	if !containsString(reads, "docs/specs/linked.md") || !containsString(reads, ".tusker/work/decisions/APP-D-0001.md") {
		t.Fatalf("expected spec refs in automation reads, got %#v", reads)
	}
}

func v7TraceabilityTestVault(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	vault := filepath.Join(repo, ".tusker")
	mustV7Proof(t, Args{"vault": vault, "quiet": "true"}, bootstrap)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App V7", "summary": "Traceability policy.", "v7": "true"}, newV7Epic)
	return vault
}

func writeTraceabilitySpec(t *testing.T, vault, rel, content string) {
	t.Helper()
	path := filepath.Join(v7RepoRoot(vault), filepath.FromSlash(rel))
	if err := writeText(path, content); err != nil {
		t.Fatal(err)
	}
}

func assertIssueMessageContains(t *testing.T, issues []Issue, needle string) {
	t.Helper()
	if !issueMessageContains(issues, needle) {
		t.Fatalf("expected issue message containing %q, got %#v", needle, issues)
	}
}

func issueMessageContains(issues []Issue, needle string) bool {
	for _, issue := range issues {
		if strings.Contains(issue.Message, needle) {
			return true
		}
	}
	return false
}
