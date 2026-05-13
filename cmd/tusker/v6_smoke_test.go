package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestV6ProfileInitCreatesKnowledgeVault(t *testing.T) {
	previousWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(previousWD); err != nil {
			t.Fatalf("restore working directory: %v", err)
		}
	})

	repo := filepath.Join(t.TempDir(), "repo")
	if err := ensureDir(repo); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}
	vault := filepath.Join(repo, "tusker")
	if err := initCmd(Args{"vault": vault, "yes": "true", "vault-only": "true", "no-mount": "true", "profile": "generic"}); err != nil {
		t.Fatal(err)
	}

	assertExists(t, filepath.Join(vault, "SKILL.md"))
	assertExists(t, filepath.Join(vault, "domains", "codebase", "INDEX.md"))
	assertExists(t, filepath.Join(vault, "domains", "codebase", "CANON.md"))
	assertExists(t, filepath.Join(vault, "domains", "product", "INDEX.md"))
	assertExists(t, filepath.Join(vault, "_config", "knowledge-policy.yaml"))
	assertExists(t, filepath.Join(vault, "_system", "generated", "knowledge-map.json"))
	if _, err := os.Stat(filepath.Join(vault, "docs")); !os.IsNotExist(err) {
		t.Fatalf("fresh V6 init must not create authored docs source folder: %v", err)
	}
	if _, err := os.Stat(filepath.Join(vault, "_config", "docs-map.yaml")); !os.IsNotExist(err) {
		t.Fatalf("fresh V6 init must not create authored docs-map registry: %v", err)
	}
	data, _, err := parseFrontmatterMustRead(filepath.Join(vault, "WORKFLOW.md"))
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, 6, intField(data, "tracker_schema_version"), "workflow schema version")
	if code, err := validateCmd(Args{"vault": vault, "json": "true"}); err != nil || code != 0 {
		t.Fatalf("V6 validate failed: code=%d err=%v", code, err)
	}
}

func TestV6KnowledgeRouteAndCapsule(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	if err := bootstrapV6(Args{"vault": vault, "quiet": "true", "profile": "tusker"}); err != nil {
		t.Fatal(err)
	}
	output := captureStdout(t, func() {
		if err := knowledgeRouteCmd(Args{"vault": vault, "query": "change CLI flag"}); err != nil {
			t.Fatal(err)
		}
	})
	assertContainsIndexTest(t, output, "cli/canon")
	assertContainsIndexTest(t, output, "tusker knowledge show cli/canon --capsule")

	capsule := captureStdout(t, func() {
		if err := domainShowCmd(Args{"vault": vault, "id": "runtime", "capsule": "true"}); err != nil {
			t.Fatal(err)
		}
	})
	assertContainsIndexTest(t, capsule, "Domain: runtime")
	assertContainsIndexTest(t, capsule, "Current canon")
}

func TestV6KnowledgeResolutionRecordsFingerprint(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	if err := bootstrapV6(Args{"vault": vault, "quiet": "true", "profile": "generic"}); err != nil {
		t.Fatal(err)
	}
	epicDir := filepath.Join(vault, "epics", "APP")
	if err := ensureDir(epicDir); err != nil {
		t.Fatal(err)
	}
	epic := `---
schema: "tusker.epic/v6"
id: "APP"
title: "App"
status: "ready"
primary_domains:
  - "codebase"
knowledge_nodes:
  - "codebase/canon"
owner: "sarav"
created_at: "2026-05-12"
updated_at: "2026-05-12"
---

# APP - App

## Canon

Current truth lives in [[codebase/CANON]].
`
	if err := writeText(filepath.Join(epicDir, "APP.md"), epic); err != nil {
		t.Fatal(err)
	}
	task := `---
schema: "tusker.task/v6"
id: "APP-T-0001"
title: "Touch codebase canon"
epic: "APP"
kind: "feature"
status: "ready"
risk: "medium"
size: "m"
priority: "p1"
primary_domain: "codebase"
domains:
  - "codebase"
knowledge_change: true
knowledge_nodes:
  - "codebase/canon"
ai_assistance: "assisted"
created_at: "2026-05-12"
updated_at: "2026-05-12"
---

# APP-T-0001 - Touch codebase canon

## Intent

Update codebase canon.

## Read this when

Read this for proof.

## Acceptance

- Canon is checked.

## Verification plan

- Run validate.

## Verification log

- Not verified yet.

## Evidence

- Fixture evidence.

## Knowledge delta

| Topic | Before | After | Audience | Target knowledge nodes |
|---|---|---|---|---|
| Codebase canon | Unchecked | Checked | developer | codebase/canon |
`
	if err := writeText(filepath.Join(epicDir, "APP-T-0001.md"), task); err != nil {
		t.Fatal(err)
	}
	if err := knowledgeApplyCmd(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "node": "codebase/canon", "by": "agent", "reason": "Checked codebase canon."}); err != nil {
		t.Fatal(err)
	}
	data, _, err := parseFrontmatterMustRead(filepath.Join(epicDir, "APP-T-0001.md"))
	if err != nil {
		t.Fatal(err)
	}
	rows := anySlice(data["knowledge_resolution"])
	if len(rows) != 1 {
		t.Fatalf("expected one knowledge resolution, got %#v", rows)
	}
	row := rows[0].(map[string]any)
	assertEqual(t, "codebase/canon", stringValue(row["node"]), "resolution node")
	assertEqual(t, "applied", stringValue(row["status"]), "resolution status")
	if strings.TrimSpace(stringValue(row["source_fingerprint"])) == "" {
		t.Fatalf("expected source_fingerprint in resolution: %#v", row)
	}
	if code, err := validateCmd(Args{"vault": vault, "json": "true"}); err != nil || code != 0 {
		t.Fatalf("V6 validate with resolution failed: code=%d err=%v", code, err)
	}
}

func TestV6FreshnessBlocksStaleCloseAndDoneValidation(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	if err := bootstrapV6(Args{"vault": vault, "quiet": "true", "profile": "generic"}); err != nil {
		t.Fatal(err)
	}
	writeV6EpicFixture(t, vault, "APP")
	taskPath := writeV6TaskFixture(t, vault, v6TaskFixture{
		ID:             "APP-T-0001",
		Epic:           "APP",
		Status:         "ready",
		KnowledgeNodes: []string{"codebase/canon"},
		Acceptance:     "- Closing this task requires the source fingerprint recorded by the knowledge resolution to still match the current source.",
		Evidence:       "- Fixture evidence records enough proof text to satisfy the done-task evidence gate before freshness is checked.",
	})
	if err := knowledgeApplyCmd(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "node": "codebase/canon", "by": "agent", "reason": "Current source reviewed."}); err != nil {
		t.Fatal(err)
	}
	if err := setStatus(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "status": "review", "actor": "agent"}); err != nil {
		t.Fatal(err)
	}
	if err := verifyV5Cmd(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "by": "reviewer", "summary": "Acceptance and evidence checked."}); err != nil {
		t.Fatal(err)
	}
	skillPath := filepath.Join(vault, "SKILL.md")
	skill, err := readText(skillPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeText(skillPath, skill+"\nFreshness mutation after review.\n"); err != nil {
		t.Fatal(err)
	}
	err = closeV5Cmd(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "by": "reviewer"})
	if err == nil {
		t.Fatalf("expected stale source fingerprint to block close")
	}
	if !strings.Contains(err.Error(), "fingerprint changed") && !strings.Contains(err.Error(), "stale") {
		t.Fatalf("expected freshness error, got %v", err)
	}
	data, body, err := parseFrontmatterMustRead(taskPath)
	if err != nil {
		t.Fatal(err)
	}
	data["status"] = "done"
	data["closed_at"] = "2026-05-12T00:00:00Z"
	content, err := serializeDocument(data, body, v6FrontmatterOrder["task"])
	if err != nil {
		t.Fatal(err)
	}
	if err := writeText(taskPath, content); err != nil {
		t.Fatal(err)
	}
	code, err := validateCmd(Args{"vault": vault, "json": "true"})
	if err != nil {
		t.Fatal(err)
	}
	if code == 0 {
		t.Fatalf("expected validate to fail for done task with stale knowledge fingerprint")
	}
}

func TestV6TaskValidationRejectsInvalidSchemaValues(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	if err := bootstrapV6(Args{"vault": vault, "quiet": "true", "profile": "generic"}); err != nil {
		t.Fatal(err)
	}
	taskPath := writeV6TaskFixture(t, vault, v6TaskFixture{
		ID:            "APP-T-0001",
		Epic:          "MISSING",
		Status:        "banana",
		Kind:          "dream",
		Risk:          "chaos",
		Size:          "planet",
		Priority:      "tomorrow",
		PrimaryDomain: "missing-domain",
	})
	if taskPath == "" {
		t.Fatal("expected task path")
	}
	code, err := validateCmd(Args{"vault": vault, "json": "true"})
	if err != nil {
		t.Fatal(err)
	}
	if code == 0 {
		t.Fatalf("expected validate to fail invalid V6 task enum, epic, and primary_domain values")
	}
}

func TestV6DoneTaskValidationRejectsShallowProof(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	if err := bootstrapV6(Args{"vault": vault, "quiet": "true", "profile": "generic"}); err != nil {
		t.Fatal(err)
	}
	writeV6EpicFixture(t, vault, "APP")
	writeV6TaskFixture(t, vault, v6TaskFixture{
		ID:                  "APP-T-0001",
		Epic:                "APP",
		Status:              "done",
		Acceptance:          "- TODO",
		Evidence:            "- TBD",
		VerificationSummary: "",
		VerifiedAt:          "2026-05-12T00:00:00Z",
	})
	code, err := validateCmd(Args{"vault": vault, "json": "true"})
	if err != nil {
		t.Fatal(err)
	}
	if code == 0 {
		t.Fatalf("expected validate to fail done V6 task with shallow proof")
	}
}

func TestV6ValidationRejectsUnresolvedWikiLinks(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	if err := bootstrapV6(Args{"vault": vault, "quiet": "true", "profile": "generic"}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(vault, "domains", "product", "CANON.md")
	text, err := readText(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeText(path, text+"\n\n## Broken link fixture\n\n- [[missing/node]]\n"); err != nil {
		t.Fatal(err)
	}
	code, err := validateCmd(Args{"vault": vault, "json": "true"})
	if err != nil {
		t.Fatal(err)
	}
	if code == 0 {
		t.Fatalf("expected validate to fail unresolved V6 wikilink")
	}
}

func TestV6PublishExportDeletesStaleProjectionFiles(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	site := filepath.Join(t.TempDir(), "site")
	if err := bootstrapV6(Args{"vault": vault, "quiet": "true", "profile": "generic"}); err != nil {
		t.Fatal(err)
	}
	if err := knowledgeNewCmd(Args{"vault": vault, "quiet": "true", "node": "product/reference/temporary", "title": "Temporary node", "audience": "developer", "source": "tusker/SKILL.md"}); err != nil {
		t.Fatal(err)
	}
	if err := publishExportCmd(Args{"vault": vault, "site": site, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	projected := filepath.Join(site, "src", "content", "docs", "internal", "product", "reference", "temporary.md")
	assertExists(t, projected)
	if err := os.Remove(filepath.Join(vault, "domains", "product", "reference", "temporary.md")); err != nil {
		t.Fatal(err)
	}
	if err := publishExportCmd(Args{"vault": vault, "site": site, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(projected); !os.IsNotExist(err) {
		t.Fatalf("expected stale V6 projection file to be removed, stat err=%v", err)
	}
}

func TestV6PublishExportPreservesSharedRemovedRoutesReport(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	site := filepath.Join(t.TempDir(), "site")
	if err := bootstrapV6(Args{"vault": vault, "quiet": "true", "profile": "generic"}); err != nil {
		t.Fatal(err)
	}
	sharedReport := docsRemovedRoutesReport{
		SchemaVersion: docsManifestSchemaVersion,
		GeneratedAt:   "2026-05-12T00:00:00Z",
		Removed: []docsRemovedRoute{{
			Title:      "Legacy V5 route",
			SourceKind: "vault_doc",
			SourceID:   "LEG-D-0001",
			SourcePath: "docs/legacy.md",
			Route:      "internal/legacy",
			RouteURL:   "/internal/legacy/",
			OutputPath: "src/content/docs/internal/legacy.md",
		}},
	}
	if err := writeJSON(filepath.Join(site, docsRoutesRemovedRelative), sharedReport); err != nil {
		t.Fatal(err)
	}
	before := mustReadIndexTest(t, filepath.Join(site, docsRoutesRemovedRelative))
	if err := publishExportCmd(Args{"vault": vault, "site": site, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	after := mustReadIndexTest(t, filepath.Join(site, docsRoutesRemovedRelative))
	assertEqual(t, before, after, "shared removed-routes report")
	assertExists(t, filepath.Join(site, v6PublishRoutesRemovedRelative))
}

func TestV6PublishExportRejectsUnresolvedSingleSegmentWikiLink(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	site := filepath.Join(t.TempDir(), "site")
	if err := bootstrapV6(Args{"vault": vault, "quiet": "true", "profile": "generic"}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(vault, "domains", "product", "CANON.md")
	text, err := readText(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeText(path, text+"\n\n## Broken publish link fixture\n\n- [[missing-node]]\n"); err != nil {
		t.Fatal(err)
	}
	err = publishExportCmd(Args{"vault": vault, "site": site, "quiet": "true"})
	if err == nil {
		t.Fatalf("expected publish export to fail unresolved single-segment wikilink")
	}
	if !strings.Contains(err.Error(), "missing-node") {
		t.Fatalf("expected missing-node in publish error, got %v", err)
	}
}

func TestV6PublishLLMSLanesFilterInternalAndHistorical(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	site := filepath.Join(t.TempDir(), "site")
	if err := bootstrapV6(Args{"vault": vault, "quiet": "true", "profile": "generic"}); err != nil {
		t.Fatal(err)
	}
	if err := knowledgeNewCmd(Args{"vault": vault, "quiet": "true", "node": "product/reference/public-node", "title": "Public node", "audience": "developer", "source": "tusker/SKILL.md"}); err != nil {
		t.Fatal(err)
	}
	if err := knowledgeNewCmd(Args{"vault": vault, "quiet": "true", "node": "product/reference/internal-node", "title": "Internal node", "audience": "internal", "source": "tusker/SKILL.md"}); err != nil {
		t.Fatal(err)
	}
	internalPath := filepath.Join(vault, "domains", "product", "reference", "internal-node.md")
	data, body, err := parseFrontmatterMustRead(internalPath)
	if err != nil {
		t.Fatal(err)
	}
	data["canonical_status"] = "historical"
	content, err := serializeDocument(data, body, v6FrontmatterOrder["knowledge"])
	if err != nil {
		t.Fatal(err)
	}
	if err := writeText(internalPath, content); err != nil {
		t.Fatal(err)
	}
	if err := publishLLMSCmd(Args{"vault": vault, "site": site, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	defaultLLMS := mustReadIndexTest(t, filepath.Join(site, "public", "llms.txt"))
	assertContainsIndexTest(t, defaultLLMS, "Public node")
	assertNotContainsIndexTest(t, defaultLLMS, "Internal node")
	historical := mustReadIndexTest(t, filepath.Join(site, "public", "llms-historical.txt"))
	assertContainsIndexTest(t, historical, "Internal node")
}

func TestV6MigrationDryRunReportsMovesAndFieldRewrites(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	if err := bootstrap(Args{"vault": vault, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	if err := newV5Epic(Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App", "summary": "App work."}); err != nil {
		t.Fatal(err)
	}
	if err := newV5Task(Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Docs task", "risk": "low", "size": "s", "doc-nodes": "reference/cli"}, "feature"); err != nil {
		t.Fatal(err)
	}
	report, err := migrateV5VaultToV6(Args{"vault": vault, "dry-run": "true"})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Moves) == 0 {
		t.Fatalf("expected V5 docs move plan: %#v", report)
	}
	foundTaskRewrite := false
	for _, rewrite := range report.FieldRewrites {
		if rewrite.Path == "epics/APP/APP-T-0001.md" && rewrite.From == "doc_nodes" && rewrite.To == "knowledge_nodes" {
			foundTaskRewrite = true
		}
	}
	if !foundTaskRewrite {
		t.Fatalf("expected task doc_nodes rewrite in report: %#v", report.FieldRewrites)
	}
	if !strings.Contains(report.Compatibility, "clean break") {
		t.Fatalf("expected clean-break compatibility stance, got %q", report.Compatibility)
	}
}

type v6TaskFixture struct {
	ID                  string
	Epic                string
	Status              string
	Kind                string
	Risk                string
	Size                string
	Priority            string
	PrimaryDomain       string
	KnowledgeNodes      []string
	Acceptance          string
	Evidence            string
	VerificationSummary string
	VerifiedAt          string
}

func writeV6EpicFixture(t *testing.T, vault, id string) string {
	t.Helper()
	epicDir := filepath.Join(vault, "epics", id)
	if err := ensureDir(epicDir); err != nil {
		t.Fatal(err)
	}
	data := map[string]any{
		"schema":          "tusker.epic/v6",
		"id":              id,
		"title":           id + " epic",
		"status":          "ready",
		"primary_domains": []string{"codebase"},
		"knowledge_nodes": []string{"codebase/canon"},
		"owner":           "test",
		"created_at":      "2026-05-12",
		"updated_at":      "2026-05-12",
	}
	body := "# " + id + " - " + id + " epic\n\n## Canon\n\nCurrent truth lives in [[codebase/CANON]].\n"
	content, err := serializeDocument(data, body, v6FrontmatterOrder["epic"])
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(epicDir, id+".md")
	if err := writeText(path, content); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeV6TaskFixture(t *testing.T, vault string, fixture v6TaskFixture) string {
	t.Helper()
	id := firstNonEmpty(fixture.ID, "APP-T-0001")
	epic := firstNonEmpty(fixture.Epic, "APP")
	parsed := parseID(id)
	acronym := epic
	if parsed != nil {
		acronym = parsed.Acronym
	}
	epicDir := filepath.Join(vault, "epics", acronym)
	if err := ensureDir(epicDir); err != nil {
		t.Fatal(err)
	}
	status := firstNonEmpty(fixture.Status, "ready")
	kind := firstNonEmpty(fixture.Kind, "feature")
	risk := firstNonEmpty(fixture.Risk, "medium")
	size := firstNonEmpty(fixture.Size, "m")
	priority := firstNonEmpty(fixture.Priority, "p1")
	primaryDomain := firstNonEmpty(fixture.PrimaryDomain, "codebase")
	acceptance := firstNonEmpty(fixture.Acceptance, "- Acceptance proof confirms the fixture records a concrete outcome with enough detail to be meaningful.")
	evidence := firstNonEmpty(fixture.Evidence, "- Evidence proof confirms the fixture has a durable validation artifact with enough detail to be meaningful.")
	verificationLog := "- Not verified yet."
	if fixture.VerifiedAt != "" {
		verificationLog = "- 2026-05-12 - reviewer - fixture verification recorded."
	}
	data := map[string]any{
		"schema":               "tusker.task/v6",
		"id":                   id,
		"title":                "Fixture task",
		"epic":                 epic,
		"kind":                 kind,
		"status":               status,
		"risk":                 risk,
		"size":                 size,
		"priority":             priority,
		"primary_domain":       primaryDomain,
		"domains":              []string{primaryDomain},
		"knowledge_change":     len(fixture.KnowledgeNodes) > 0,
		"knowledge_nodes":      fixture.KnowledgeNodes,
		"knowledge_resolution": []any{},
		"ai_assistance":        "assisted",
		"ai_tools":             []string{},
		"created_at":           "2026-05-12",
		"updated_at":           "2026-05-12",
	}
	if fixture.VerifiedAt != "" {
		data["verified_by"] = "reviewer"
		data["verified_at"] = fixture.VerifiedAt
	}
	if fixture.VerificationSummary != "" {
		data["verification_summary"] = fixture.VerificationSummary
	}
	body := "# " + id + " - Fixture task\n\n" +
		"## Intent\n\nExercise V6 validation fixtures.\n\n" +
		"## Read this when\n\nRead this for task proof or implementation context.\n\n" +
		"## Acceptance\n\n" + acceptance + "\n\n" +
		"## Verification plan\n\n- Run validate.\n\n" +
		"## Verification log\n\n" + verificationLog + "\n\n" +
		"## Evidence\n\n" + evidence + "\n\n" +
		"## Knowledge delta\n\n" +
		"| Topic | Before | After | Audience | Target knowledge nodes |\n" +
		"|---|---|---|---|---|\n" +
		"| Fixture | Before | After | developer | " + strings.Join(fixture.KnowledgeNodes, ", ") + " |\n"
	content, err := serializeDocument(data, body, v6FrontmatterOrder["task"])
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(epicDir, id+".md")
	if err := writeText(path, content); err != nil {
		t.Fatal(err)
	}
	return path
}
