package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestCapsuleTemplatesCreateScaffolds(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	if err := bootstrapV7Profile(vault, "v7"); err != nil {
		t.Fatal(err)
	}
	if err := newV7Epic(Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App", "summary": "App work.", "v7": "true"}); err != nil {
		t.Fatal(err)
	}
	if err := domainNewCmd(Args{"vault": vault, "quiet": "true", "v7": "true", "id": "providers", "title": "Providers", "summary": "Provider integrations."}); err != nil {
		t.Fatal(err)
	}
	if err := knowledgeNewCmd(Args{"vault": vault, "quiet": "true", "v7": "true", "node": "providers/runbooks/oauth-refresh", "kind": "runbook", "title": "OAuth refresh"}); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{
		"work/epics/APP.md",
		"knowledge/domains/providers/INDEX.md",
		"knowledge/domains/providers/CANON.md",
		"knowledge/domains/providers/runbooks/oauth-refresh.md",
		"SKILL.md",
	} {
		data, _, err := parseFrontmatterMustRead(filepath.Join(vault, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		capsule := frontmatterCapsuleFor(Note{Data: data})
		if !capsule.Present {
			t.Fatalf("%s missing capsule scaffold", rel)
		}
		if rel != "SKILL.md" && !capsuleHasContent(capsule) {
			t.Fatalf("%s capsule should provide useful routing, got %#v", rel, capsule)
		}
	}
}

func TestCapsuleValidationWarnsAndFailsByBudget(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "repo", ".tusker")
	if err := ensureDir(vault); err != nil {
		t.Fatal(err)
	}
	if err := writeText(filepath.Join(filepath.Dir(vault), "tusker.yaml"), "validation:\n  capsule_token_budget: 5\n"); err != nil {
		t.Fatal(err)
	}
	note := Note{Data: map[string]any{
		"schema":  "tusker.domain/v7",
		"kind":    "domain",
		"id":      "project",
		"capsule": capsuleBlock("one two three four five six", nil, nil),
	}}
	var errs, warns []Issue
	validateCapsule(note, vault, "knowledge/domains/project/INDEX.md", true, &errs, &warns)
	if len(errs) != 0 || !issuesContainCode(warns, "CAPSULE_LONG") {
		t.Fatalf("expected budget warning only, errs=%#v warns=%#v", errs, warns)
	}
	note.Data["capsule"] = capsuleBlock("one two three four five six seven eight nine ten", nil, nil)
	errs, warns = nil, nil
	validateCapsule(note, vault, "knowledge/domains/project/INDEX.md", true, &errs, &warns)
	if !issuesContainCode(errs, "CAPSULE_TOO_LONG") {
		t.Fatalf("expected hard budget error, errs=%#v warns=%#v", errs, warns)
	}
	delete(note.Data, "capsule")
	errs, warns = nil, nil
	validateCapsule(note, vault, "knowledge/domains/project/INDEX.md", true, &errs, &warns)
	if !issuesContainCode(warns, "CAPSULE_MISSING") {
		t.Fatalf("expected missing capsule warning, errs=%#v warns=%#v", errs, warns)
	}
}

func TestCapsuleSpecValidationScansDocsSpecs(t *testing.T) {
	repo := t.TempDir()
	vault := filepath.Join(repo, ".tusker")
	if err := ensureDir(filepath.Join(repo, "docs", "specs")); err != nil {
		t.Fatal(err)
	}
	if err := writeText(filepath.Join(repo, "docs", "specs", "missing.md"), "# Missing capsule\n"); err != nil {
		t.Fatal(err)
	}
	errs, warns := validateSpecCapsules(vault)
	if len(errs) != 0 || !issuesContainCode(warns, "CAPSULE_MISSING") {
		t.Fatalf("expected missing spec capsule warning, errs=%#v warns=%#v", errs, warns)
	}
}

func TestCapsuleTriageSurfacesAndPackets(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	if err := bootstrapV7Profile(vault, "v7"); err != nil {
		t.Fatal(err)
	}
	if err := newV7Epic(Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App", "summary": "App work.", "v7": "true"}); err != nil {
		t.Fatal(err)
	}
	if err := domainNewCmd(Args{"vault": vault, "quiet": "true", "v7": "true", "id": "providers", "title": "Providers", "summary": "Provider integrations."}); err != nil {
		t.Fatal(err)
	}
	setCapsuleForTest(t, filepath.Join(vault, "knowledge", "domains", "providers", "INDEX.md"), v7FrontmatterOrder["domain"], capsuleBlock(
		"Provider routes for external integrations.",
		[]string{"triaging provider domain work"},
		[]string{"task proof or generated packets"},
	))
	setCapsuleForTest(t, filepath.Join(vault, "knowledge", "domains", "providers", "CANON.md"), v7FrontmatterOrder["domain_canon"], capsuleBlock(
		"Provider truths and invariants.",
		[]string{"changing provider integration behavior"},
		[]string{"only checking task status"},
	))
	if err := newV7Task(Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Provider task", "risk": "low", "priority": "p2", "domains": "providers", "v7": "true"}); err != nil {
		t.Fatal(err)
	}
	makeV7TaskDispatchableForTest(t, vault, "APP-T-0001")

	listJSON := captureStdout(t, func() {
		if err := listCmd(Args{"vault": vault, "json": "true"}); err != nil {
			t.Fatal(err)
		}
	})
	assertContainsIndexTest(t, listJSON, `"capsule"`)
	assertContainsIndexTest(t, listJSON, "Provider routes for external integrations.")

	searchJSON := captureStdout(t, func() {
		if err := searchCmd(Args{"vault": vault, "query": "triaging provider domain work", "json": "true"}); err != nil {
			t.Fatal(err)
		}
	})
	assertContainsIndexTest(t, searchJSON, "Provider routes for external integrations.")

	showOutput := captureStdout(t, func() {
		if err := showCmd(Args{"vault": vault, "_pos0": "providers", "capsule": "true"}); err != nil {
			t.Fatal(err)
		}
	})
	assertContainsIndexTest(t, showOutput, "What: Provider routes for external integrations.")

	task, err := resolveNote(vault, "APP-T-0001")
	if err != nil {
		t.Fatal(err)
	}
	idx, err := loadV7Index(vault)
	if err != nil {
		t.Fatal(err)
	}
	packet := v7Packet(vault, task, idx, "agent")
	assertContainsIndexTest(t, packet, "`providers` INDEX capsule: Provider routes for external integrations.")
	assertContainsIndexTest(t, packet, "- CANON: Provider truths and invariants.")

	var decoded struct {
		Items []struct {
			ID      string         `json:"id"`
			Capsule map[string]any `json:"capsule"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(listJSON), &decoded); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range decoded.Items {
		if item.ID == "providers" && strings.Contains(stringValue(item.Capsule["what"]), "Provider routes") {
			found = true
		}
	}
	if !found {
		t.Fatalf("list JSON did not expose providers capsule: %s", listJSON)
	}
}

func setCapsuleForTest(t *testing.T, path string, order []string, capsule map[string]any) {
	t.Helper()
	data, body, err := parseFrontmatterMustRead(path)
	if err != nil {
		t.Fatal(err)
	}
	data["capsule"] = capsule
	data["state_rev"] = v7StateRev(data, body)
	content, err := serializeDocument(data, body, order)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeText(path, content); err != nil {
		t.Fatal(err)
	}
}
