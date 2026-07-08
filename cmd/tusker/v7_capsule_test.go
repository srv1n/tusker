package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestV7CapsuleValidationWarnsMissingAndBudgetsTokens(t *testing.T) {
	note := v7CapsuleKnowledgeNote(nil)
	_, warnings := validateV7Note(note, validationContext{}, note.RelativePath)
	assertIssueCode(t, warnings, "CAPSULE_MISSING")

	warnNote := v7CapsuleKnowledgeNote(v7CapsuleOrdered(longTokenString(81), "", ""))
	errors, warnings := validateV7Note(warnNote, validationContext{}, warnNote.RelativePath)
	assertNoIssueCode(t, errors, "CAPSULE_TOKEN_BUDGET_EXCEEDED")
	assertIssueCode(t, warnings, "CAPSULE_TOKEN_BUDGET_WARN")

	failNote := v7CapsuleKnowledgeNote(v7CapsuleOrdered(longTokenString(161), "", ""))
	errors, _ = validateV7Note(failNote, validationContext{}, failNote.RelativePath)
	assertIssueCode(t, errors, "CAPSULE_TOKEN_BUDGET_EXCEEDED")
}

func TestV7CapsuleValidateCommandWarnsAndFailsByBudget(t *testing.T) {
	root := t.TempDir()
	vault := filepath.Join(root, ".tusker")
	if err := bootstrapV7Profile(vault, "v7"); err != nil {
		t.Fatal(err)
	}
	if err := writeText(filepath.Join(root, "tusker.yaml"), "validation:\n  capsule_token_budget: 40\n"); err != nil {
		t.Fatal(err)
	}
	if err := knowledgeV7NewCmd(Args{"vault": vault, "quiet": "true", "node": "project/runbooks/capsule-route", "kind": "runbook", "title": "Capsule Route", "summary": "Route by capsule.", "v7": "true"}); err != nil {
		t.Fatal(err)
	}

	setCapsuleFixture(t, vault, nil)
	code, payload := validateCapsuleFixture(t, vault)
	if code != 0 {
		t.Fatalf("missing capsule exit code: expected 0, got %d; errors=%#v warnings=%#v", code, payload["errors"], payload["warnings"])
	}
	assertJSONIssueCode(t, payload["warnings"], "CAPSULE_MISSING")

	setCapsuleFixture(t, vault, v7CapsuleOrdered(longTokenString(41), "", ""))
	code, payload = validateCapsuleFixture(t, vault)
	if code != 0 {
		t.Fatalf("over budget warning exit code: expected 0, got %d; errors=%#v warnings=%#v", code, payload["errors"], payload["warnings"])
	}
	assertJSONIssueCode(t, payload["warnings"], "CAPSULE_TOKEN_BUDGET_WARN")
	assertNoJSONIssueCode(t, payload["errors"], "CAPSULE_TOKEN_BUDGET_EXCEEDED")

	setCapsuleFixture(t, vault, v7CapsuleOrdered(longTokenString(81), "", ""))
	code, payload = validateCapsuleFixture(t, vault)
	assertEqual(t, 1, code, "over budget failure exit code")
	assertJSONIssueCode(t, payload["errors"], "CAPSULE_TOKEN_BUDGET_EXCEEDED")
}

func TestV7CapsuleTemplatesAndGeneratedRecords(t *testing.T) {
	for _, rel := range []string{
		"skill/assets/templates/project-skill.md",
		"skill/assets/templates/domain-index.md",
		"skill/assets/templates/domain-canon.md",
		"skill/assets/templates/epic.md",
		"skill/assets/templates/doc.md",
		"skill/assets/templates/agent-doc.md",
	} {
		raw, err := readText(filepath.Join("..", "..", rel))
		if err != nil {
			t.Fatal(err)
		}
		assertContainsIndexTest(t, raw, "capsule:")
		assertContainsIndexTest(t, raw, "use_when:")
		assertContainsIndexTest(t, raw, "skip_when:")
	}

	vault := t.TempDir()
	if err := bootstrapV7Profile(vault, "v7"); err != nil {
		t.Fatal(err)
	}
	if err := newV7Epic(Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App V7", "summary": "V7 tracker smoke.", "v7": "true"}); err != nil {
		t.Fatal(err)
	}
	if err := knowledgeV7NewCmd(Args{"vault": vault, "quiet": "true", "node": "project/runbooks/capsule-route", "kind": "runbook", "title": "Capsule Route", "summary": "Route by capsule.", "v7": "true"}); err != nil {
		t.Fatal(err)
	}

	for _, rel := range []string{
		"SKILL.md",
		"knowledge/domains/project/INDEX.md",
		"knowledge/domains/project/CANON.md",
		"work/epics/APP.md",
		"knowledge/domains/project/runbooks/capsule-route.md",
	} {
		data, _, err := parseFrontmatterMustRead(filepath.Join(vault, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		if _, present, valid := v7CapsuleFromData(data); !present || !valid {
			t.Fatalf("%s missing valid capsule: %#v", rel, data["capsule"])
		}
	}
}

func TestV7CapsuleSurfacesInListSearchShowAndPacket(t *testing.T) {
	vault := t.TempDir()
	if err := bootstrapV7Profile(vault, "v7"); err != nil {
		t.Fatal(err)
	}
	if err := newV7Epic(Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App V7", "summary": "V7 tracker smoke.", "v7": "true"}); err != nil {
		t.Fatal(err)
	}
	if err := newV7Task(Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Domain packet", "risk": "low", "priority": "p2", "domains": "project", "v7": "true"}); err != nil {
		t.Fatal(err)
	}

	listOutput := captureStdout(t, func() {
		if err := listCmd(Args{"vault": vault, "json": "true", "type": "epic"}); err != nil {
			t.Fatal(err)
		}
	})
	listPayload := decodeJSONMap(t, listOutput)
	items := listPayload["items"].([]any)
	first := items[0].(map[string]any)
	capsule := first["capsule"].(map[string]any)
	assertContainsIndexTest(t, capsule["what"].(string), "APP epic")

	searchOutput := captureStdout(t, func() {
		if err := searchCmd(Args{"vault": vault, "json": "true", "query": "workstream"}); err != nil {
			t.Fatal(err)
		}
	})
	searchPayload := decodeJSONMap(t, searchOutput)
	searchItems := searchPayload["items"].([]any)
	searchCapsule := searchItems[0].(map[string]any)["capsule"].(map[string]any)
	assertContainsIndexTest(t, searchCapsule["use_when"].(string), "workstream")

	showOutput := captureStdout(t, func() {
		if err := showCmd(Args{"vault": vault, "_pos0": "project", "capsule": "true"}); err != nil {
			t.Fatal(err)
		}
	})
	assertContainsIndexTest(t, showOutput, "- What:")
	assertContainsIndexTest(t, showOutput, "Domain index for Project")

	taskPath := filepath.Join(vault, "work", "tasks", "APP-T-0001.md")
	taskData, taskBody, err := parseFrontmatterMustRead(taskPath)
	if err != nil {
		t.Fatal(err)
	}
	packet := v7Packet(vault, Note{AbsolutePath: taskPath, Data: taskData, Body: taskBody}, mustIndex(t, vault), "agent")
	assertContainsIndexTest(t, packet, "## Routed file capsules")
	assertContainsIndexTest(t, packet, "Domain index for Project")
	assertContainsIndexTest(t, packet, "Current durable truth")
}

func TestV7CapsulePacketDefaultsNoDomainTasksToProjectRoute(t *testing.T) {
	vault := t.TempDir()
	if err := bootstrapV7Profile(vault, "v7"); err != nil {
		t.Fatal(err)
	}
	if err := newV7Epic(Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App V7", "summary": "V7 tracker smoke.", "v7": "true"}); err != nil {
		t.Fatal(err)
	}
	if err := newV7Task(Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "No explicit domain", "risk": "low", "priority": "p2", "v7": "true"}); err != nil {
		t.Fatal(err)
	}

	taskPath := filepath.Join(vault, "work", "tasks", "APP-T-0001.md")
	taskData, taskBody, err := parseFrontmatterMustRead(taskPath)
	if err != nil {
		t.Fatal(err)
	}
	packet := v7Packet(vault, Note{AbsolutePath: taskPath, Data: taskData, Body: taskBody}, mustIndex(t, vault), "agent")
	assertContainsIndexTest(t, packet, "No task domains declared; default to the `project` domain route")
	assertContainsIndexTest(t, packet, "`knowledge/domains/project/INDEX.md`")
	assertContainsIndexTest(t, packet, "`knowledge/domains/project/CANON.md`")
	assertContainsIndexTest(t, packet, "Domain index for Project")
	assertContainsIndexTest(t, packet, "Current durable truth")
}

func v7CapsuleKnowledgeNote(capsule any) Note {
	data := map[string]any{
		"schema":          "tusker.knowledge/v7",
		"kind":            "runbook",
		"id":              "project/runbooks/capsule",
		"project":         "tusker",
		"domain":          "project",
		"title":           "Capsule",
		"status":          "current",
		"summary":         "Capsule validation.",
		"source_of_truth": []string{"knowledge/domains/project/CANON.md"},
	}
	if capsule != nil {
		data["capsule"] = capsule
	}
	return Note{
		RelativePath: "knowledge/domains/project/runbooks/capsule.md",
		Data:         data,
		Body:         "# Capsule\n",
	}
}

func longTokenString(count int) string {
	return strings.TrimSpace(strings.Repeat("token ", count))
}

func setCapsuleFixture(t *testing.T, vault string, capsule any) {
	t.Helper()
	path := filepath.Join(vault, "knowledge", "domains", "project", "runbooks", "capsule-route.md")
	data, body, err := parseFrontmatterMustRead(path)
	if err != nil {
		t.Fatal(err)
	}
	if capsule == nil {
		delete(data, "capsule")
	} else {
		data["capsule"] = capsule
	}
	data["state_rev"] = v7StateRev(data, body)
	content, err := serializeDocument(data, body, v7FrontmatterOrder["knowledge"])
	if err != nil {
		t.Fatal(err)
	}
	if err := writeText(path, content); err != nil {
		t.Fatal(err)
	}
}

func validateCapsuleFixture(t *testing.T, vault string) (int, map[string]any) {
	t.Helper()
	var code int
	output := captureStdout(t, func() {
		var err error
		code, err = validateCmd(Args{"vault": vault, "json": "true"})
		if err != nil {
			t.Fatal(err)
		}
	})
	return code, decodeJSONMap(t, output)
}

func assertIssueCode(t *testing.T, issues []Issue, code string) {
	t.Helper()
	for _, issue := range issues {
		if issue.Code == code {
			return
		}
	}
	t.Fatalf("expected issue %s in %#v", code, issues)
}

func assertNoIssueCode(t *testing.T, issues []Issue, code string) {
	t.Helper()
	for _, issue := range issues {
		if issue.Code == code {
			t.Fatalf("did not expect issue %s in %#v", code, issues)
		}
	}
}

func assertJSONIssueCode(t *testing.T, raw any, code string) {
	t.Helper()
	issues, ok := raw.([]any)
	if !ok {
		t.Fatalf("expected issue %s in %#v", code, raw)
	}
	for _, issue := range issues {
		if issue.(map[string]any)["code"] == code {
			return
		}
	}
	t.Fatalf("expected issue %s in %#v", code, raw)
}

func assertNoJSONIssueCode(t *testing.T, raw any, code string) {
	t.Helper()
	issues, ok := raw.([]any)
	if raw == nil {
		return
	}
	if !ok {
		t.Fatalf("expected issue list in %#v", raw)
	}
	for _, issue := range issues {
		if issue.(map[string]any)["code"] == code {
			t.Fatalf("did not expect issue %s in %#v", code, raw)
		}
	}
}

func decodeJSONMap(t *testing.T, raw string) map[string]any {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("invalid JSON %v:\n%s", err, raw)
	}
	return payload
}
