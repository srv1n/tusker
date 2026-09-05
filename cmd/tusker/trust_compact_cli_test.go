package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestTrustCompactCli(t *testing.T) {
	vault := t.TempDir()
	if err := bootstrapV7Profile(vault, "v7"); err != nil {
		t.Fatal(err)
	}
	if err := newV7Epic(Args{"vault": vault, "quiet": "true", "acronym": "CMP", "title": "Compact CLI", "summary": "Compact CLI fixture.", "v7": "true"}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 10; i++ {
		if err := newV7Task(Args{
			"vault": vault,
			"quiet": "true",
			"epic":  "CMP",
			"title": "Task " + string(rune('A'+i)),
			"v7":    "true",
		}); err != nil {
			t.Fatal(err)
		}
	}

	listOutput := captureStdout(t, func() {
		if err := listCmd(Args{"vault": vault, "json": "true", "type": "task", "limit": "3"}); err != nil {
			t.Fatal(err)
		}
	})
	var listPayload struct {
		Count     int   `json:"count"`
		Total     int   `json:"total"`
		Truncated int   `json:"truncated"`
		Items     []any `json:"items"`
	}
	if err := json.Unmarshal([]byte(listOutput), &listPayload); err != nil {
		t.Fatalf("list output is not JSON: %v\n%s", err, listOutput)
	}
	if listPayload.Count != 3 || len(listPayload.Items) != 3 || listPayload.Total != 10 || listPayload.Truncated != 7 {
		t.Fatalf("bounded list projection = %#v", listPayload)
	}

	showOutput := captureStdout(t, func() {
		if err := showCmd(Args{"vault": vault, "_pos0": "CMP-T-0001", "json": "true"}); err != nil {
			t.Fatal(err)
		}
	})
	var showPayload struct {
		ID      string `json:"id"`
		Status  string `json:"status"`
		Capsule string `json:"capsule"`
	}
	if err := json.Unmarshal([]byte(showOutput), &showPayload); err != nil {
		t.Fatalf("show output is not JSON: %v\n%s", err, showOutput)
	}
	if showPayload.ID != "CMP-T-0001" || showPayload.Status != "backlog" {
		t.Fatalf("show projection lost stable identity/state: %#v", showPayload)
	}
	if got := len(strings.Fields(showPayload.Capsule)); got > 500 {
		t.Fatalf("routine capsule has %d tokens, want <= 500: %s", got, showPayload.Capsule)
	}

	for i := 1; i <= 5; i++ {
		domain := "compact-" + string(rune('0'+i))
		writeTrustRoute(t, vault, domain, "INDEX.md", "domain")
		writeTrustRoute(t, vault, domain, "CANON.md", "domain_canon")
	}
	long := strings.TrimSpace(strings.Repeat("route-context ", defaultV7CapsuleTokenBudget+20))
	legacyLine := capsuleOneLine(Note{Data: map[string]any{
		"id":      "CMP-T-0001",
		"capsule": capsuleBlock(long, nil, nil),
	}})
	if got := len(strings.Fields(legacyLine)); got > defaultCapsuleTokenBudget {
		t.Fatalf("legacy capsule line has %d tokens, want <= %d: %s", got, defaultCapsuleTokenBudget, legacyLine)
	}
	if !strings.Contains(legacyLine, "capsule shortened") || !strings.Contains(legacyLine, "tusker show CMP-T-0001 --capsule") {
		t.Fatalf("legacy capsule line lacks navigable truncation: %s", legacyLine)
	}
	// Replace the generated route capsules with intentionally over-budget text
	// to prove supporting context is bounded without changing task contracts.
	for i := 1; i <= 5; i++ {
		domain := "compact-" + string(rune('0'+i))
		writeTrustRouteWithCapsule(t, vault, domain, "INDEX.md", "domain", long)
		writeTrustRouteWithCapsule(t, vault, domain, "CANON.md", "domain_canon", long)
	}
	var domains []string
	for i := 1; i <= 5; i++ {
		domains = append(domains, "compact-"+string(rune('0'+i)))
	}
	routed := v7RoutedFileCapsules(vault, Note{Data: map[string]any{"domains": domains}})
	if !strings.Contains(routed, "routed file capsules omitted") {
		t.Fatalf("large route set lacks explicit remainder: %s", routed)
	}
	if !strings.Contains(routed, "knowledge/domains/compact-5/INDEX.md") || !strings.Contains(routed, "knowledge/domains/compact-5/CANON.md") {
		t.Fatalf("remainder is not navigable: %s", routed)
	}
	if !strings.Contains(routed, "Capsule truncated; read the complete route") {
		t.Fatalf("over-budget route capsule lacks truncation marker: %s", routed)
	}
}

func writeTrustRoute(t *testing.T, vault, domain, name, kind string) {
	t.Helper()
	writeTrustRouteWithCapsule(t, vault, domain, name, kind, "Compact route capsule.")
}

func writeTrustRouteWithCapsule(t *testing.T, vault, domain, name, kind, what string) {
	t.Helper()
	rel := filepath.ToSlash(filepath.Join("knowledge", "domains", domain, name))
	path := filepath.Join(vault, filepath.FromSlash(rel))
	if err := ensureDir(filepath.Dir(path)); err != nil {
		t.Fatal(err)
	}
	id := domain
	order := "domain"
	schema := "tusker.domain/v7"
	if kind == "domain_canon" {
		id += "/canon"
		order = "domain_canon"
		schema = "tusker.domain-canon/v7"
	}
	body := "# " + domain + " " + name + "\n"
	data := map[string]any{
		"schema":  schema,
		"kind":    kind,
		"id":      id,
		"project": "tusker",
		"domain":  domain,
		"title":   domain + " " + name,
		"status":  "current",
		"summary": "Compact route fixture.",
		"capsule": v7CapsuleOrdered(what, "Use this route.", "Skip unrelated work."),
	}
	data["state_rev"] = v7StateRev(data, body)
	content, err := serializeDocument(data, body, v7FrontmatterOrder[order])
	if err != nil {
		t.Fatal(err)
	}
	if err := writeText(path, content); err != nil {
		t.Fatal(err)
	}
}
