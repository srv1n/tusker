package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestLoadDocsMapSupportsDiataxisMappingNodes(t *testing.T) {
	vault := t.TempDir()
	mapPath := filepath.Join(vault, "_config", "docs-map.yaml")
	content := `schema: tusker.docs-map/v5
domains:
  docs-system:
    label: Docs
    description: Docs system.
nodes:
  tusker/docs-system:
    title: Documentation freshness system
    path: docs/concepts/docs-freshness.md
    domain: docs-system
    mode: explanation
    audience: developer
    agent_layer: capsule
    role: canon
    covers: [docs-system]
    source_of_truth: [cmd/tusker/docs_map.go]
    stale_when:
      paths: [cmd/tusker/docs_*.go]
    evals: [docs-impact-staleness]
`
	if err := writeText(mapPath, content); err != nil {
		t.Fatal(err)
	}
	docsMap, err := loadDocsMap(vault)
	if err != nil {
		t.Fatal(err)
	}
	node, ok := docsMap.Node("tusker/docs-system")
	if !ok {
		t.Fatal("expected mapping-form node to load")
	}
	assertEqual(t, "docs/concepts/docs-freshness.md", node.SourcePath(), "source path")
	assertEqual(t, "docs-system", node.Domain, "domain")
	assertEqual(t, "explanation", node.Mode, "mode")
	assertEqual(t, "developer", node.Audience, "audience")
	assertEqual(t, "capsule", node.AgentLayer, "agent layer")
	assertEqual(t, []string{"docs-system"}, node.Covers, "covers")
	assertEqual(t, []string{"cmd/tusker/docs_map.go"}, node.SourceOfTruth, "source of truth")
	assertEqual(t, []string{"cmd/tusker/docs_*.go"}, node.StaleWhen.Paths, "stale paths")
	assertEqual(t, []string{"docs-impact-staleness"}, node.Evals, "evals")
	if issues := validateDocsMapConfig(docsMap); len(issues) != 0 {
		t.Fatalf("expected valid docs-map, got %#v", issues)
	}
}

func TestValidateDocsMapConfigRequiresExplicitDiataxisMetadata(t *testing.T) {
	var docsMap DocsMap
	raw := `schema: tusker.docs-map/v5
domains:
  docs:
    label: Docs
    description: Docs.
nodes:
  - id: reference/cli
    page: docs/reference/cli.md
    audience: developer
    kind: reference
    publish_path: reference/cli
`
	if err := yaml.Unmarshal([]byte(raw), &docsMap); err != nil {
		t.Fatal(err)
	}
	issues := validateDocsMapConfig(&docsMap)
	for _, expected := range []string{"domain", "mode", "agent_layer", "source_of_truth", "stale_when.paths"} {
		found := false
		for _, issue := range issues {
			if strings.Contains(issue.Message, expected) {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected issue for %s, got %#v", expected, issues)
		}
	}
}

func TestDefaultDocsMapYAMLIsValidDiataxisCatalog(t *testing.T) {
	var docsMap DocsMap
	if err := yaml.Unmarshal([]byte(defaultDocsMapYAML("2026-04-29")), &docsMap); err != nil {
		t.Fatal(err)
	}
	if issues := validateDocsMapConfig(&docsMap); len(issues) != 0 {
		t.Fatalf("default docs-map should validate, got %#v", issues)
	}
	for _, node := range docsMap.Nodes {
		if node.Mode == "" || node.Audience == "" || node.AgentLayer == "" || node.SourcePath() == "" || len(node.SourceOfTruth) == 0 || len(node.StaleWhen.Paths) == 0 {
			t.Fatalf("default node missing Diataxis metadata: %#v", node)
		}
	}
}

func TestReindexDocsIndexMergesDocsMapMetadataAndCatalog(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	if err := bootstrapLegacy(Args{"vault": vault, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	if err := reindex(Args{"vault": vault, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	raw, err := readText(filepath.Join(vault, "_system", "generated", "docs.index.json"))
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Items   []map[string]any `json:"items"`
		Catalog []map[string]any `json:"catalog"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatal(err)
	}
	var docsPipeline map[string]any
	for _, item := range payload.Items {
		if stringValue(item["node"]) == "tusker/docs-system" {
			docsPipeline = item
			break
		}
	}
	if docsPipeline == nil {
		t.Fatal("expected tusker/docs-system in docs index")
	}
	assertEqual(t, "explanation", stringValue(docsPipeline["mode"]), "mode")
	assertEqual(t, "capsule", stringValue(docsPipeline["agent_layer"]), "agent layer")
	if len(normalizeList(docsPipeline["source_of_truth"])) == 0 {
		t.Fatal("expected source_of_truth in docs index")
	}
	var agentEntry map[string]any
	for _, item := range payload.Catalog {
		if stringValue(item["doc_node"]) == "agents/tusker-skill" {
			agentEntry = item
			break
		}
	}
	if agentEntry == nil {
		t.Fatal("expected agents/tusker-skill in docs catalog")
	}
	assertEqual(t, "For agents", stringValue(agentEntry["section"]), "agent section")

	catalog, err := readText(filepath.Join(vault, "Docs.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, heading := range []string{"## Guides", "## Reference", "## Concepts", "## For agents"} {
		if !strings.Contains(catalog, heading) {
			t.Fatalf("expected catalog heading %s in Docs.md:\n%s", heading, catalog)
		}
	}
	for _, rawMode := range []string{"## tutorial", "## how-to", "## reference", "## explanation"} {
		if strings.Contains(catalog, rawMode) {
			t.Fatalf("catalog should not use raw Diataxis heading %s", rawMode)
		}
	}
}

func TestRenderLLMSTextUsesDocsMapOrderAndFullBody(t *testing.T) {
	sources := []docsSourceDocument{
		{Title: "Agent recipe: using Tusker", RouteURL: "/agents/use-tusker/", RoutePath: "agents/use-tusker", Audience: "agent", Mode: "how-to", AgentLayer: "standalone", SourcePath: "docs/agents/use-tusker.md", Body: "# Agent recipe: using Tusker", DocsMapOrder: 3},
		{Title: "Tusker v5 CLI surface", RouteURL: "/reference/cli/", RoutePath: "reference/cli", Audience: "developer", Mode: "reference", SourcePath: "docs/reference/cli.md", Body: "# Tusker v5 CLI surface", DocsMapOrder: 2},
		{Title: "Tusker v5 implementation spec", RouteURL: "/spec/v5-overview/", RoutePath: "spec/v5-overview", Audience: "developer", Mode: "explanation", SourcePath: "docs/spec/v5-overview.md", Body: "# Tusker v5 implementation spec", DocsMapOrder: 1},
	}
	docsSortSources(sources)
	compact := renderLLMSText(sources, false)
	overview := strings.Index(compact, "Tusker v5 implementation spec")
	cli := strings.Index(compact, "Tusker v5 CLI surface")
	agent := strings.Index(compact, "Agent recipe: using Tusker")
	if overview < 0 || cli < 0 || agent < 0 {
		t.Fatalf("expected overview, cli, and agent docs in llms.txt:\n%s", compact)
	}
	if !(overview < cli && cli < agent) {
		t.Fatalf("expected docs-map order in llms.txt; overview=%d cli=%d agent=%d\n%s", overview, cli, agent, compact)
	}
	if strings.Contains(compact, "source:") {
		t.Fatal("compact llms.txt should not include source lines")
	}
	full := renderLLMSText(sources, true)
	if !strings.Contains(full, "source:") || !strings.Contains(full, "# Agent recipe: using Tusker") {
		t.Fatalf("full llms output should include source lines and markdown body:\n%s", full)
	}
}

func TestKnowledgeDeltaParserRoutesDocNodes(t *testing.T) {
	body := `## Knowledge delta

| Change type | Topic | Before | After | Audience | Target doc nodes | Mode | Status |
|---|---|---|---|---|---|---|---|
| changed | Docs model | Docs routing was implicit. | Docs routing is controlled by docs-map metadata. | developer | tusker/docs-system, agents/tusker-skill | explanation | pending |
`
	rows := parseKnowledgeDeltaRows(body)
	if len(rows) != 1 {
		t.Fatalf("expected one row, got %#v", rows)
	}
	assertEqual(t, "changed", rows[0].ChangeType, "change type")
	assertEqual(t, "Docs model", rows[0].Topic, "topic")
	assertEqual(t, "developer", rows[0].Audience, "audience")
	assertEqual(t, "explanation", rows[0].Mode, "mode")
	assertEqual(t, []string{"tusker/docs-system", "agents/tusker-skill"}, rows[0].DocNodes, "doc nodes")
	if !hasValidKnowledgeDelta(body) {
		t.Fatal("expected non-tautological knowledge delta")
	}
}

func TestBuildDocsCatalogIncludesFreshnessTaskLinksAndWaivers(t *testing.T) {
	docsMap := &DocsMap{Nodes: []DocsMapNode{{
		ID:            "tusker/docs-system",
		Title:         "Documentation freshness system",
		Page:          "docs/reference/docs-pipeline.md",
		Domain:        "docs-system",
		Mode:          "explanation",
		Audience:      "developer",
		AgentLayer:    "capsule",
		SourceOfTruth: []string{"cmd/tusker/docs_map.go"},
		StaleWhen:     DocsMapStaleWhen{Paths: []string{"cmd/tusker/docs_*.go"}},
	}}}
	tasks := []map[string]any{{
		"id":        "DOC-T-0008",
		"doc_nodes": []string{"tusker/docs-system"},
		"docs_resolution": []any{map[string]any{
			"node":   "tusker/docs-system",
			"status": "waived",
			"actor":  "agent",
			"date":   "2026-04-29",
			"reason": "catalog fixture",
		}},
	}}
	catalog := buildDocsCatalog(docsMap, nil, tasks)
	if len(catalog) != 1 {
		t.Fatalf("expected one catalog entry, got %#v", catalog)
	}
	assertEqual(t, "waived", stringValue(catalog[0]["freshness"]), "freshness")
	assertEqual(t, []string{"DOC-T-0008"}, normalizeList(catalog[0]["linked_tasks"]), "linked tasks")
	waivers := anySlice(catalog[0]["waivers"])
	if len(waivers) != 1 {
		t.Fatalf("expected one waiver, got %#v", catalog[0]["waivers"])
	}
	assertEqual(t, "DOC-T-0008", stringValue(waivers[0].(map[string]any)["task"]), "waiver task")
}
