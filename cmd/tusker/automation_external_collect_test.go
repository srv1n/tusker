package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type externalCollectJSONPayload struct {
	OK         bool                  `json:"ok"`
	Collection externalCollectReport `json:"collection"`
}

func TestAutomationCollectExternalStoresPatchAndReviewEvidence(t *testing.T) {
	vault := automationTestVault(t)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "External apply", "risk": "low", "priority": "p0", "v7": "true"}, newV7Task)
	makeV7TaskDispatchableForTest(t, vault, "APP-T-0001")
	project := registerAutomationTestProject(t, vault)

	sourceDir := writeExternalFetchFiles(t, map[string]string{
		"fix.patch":  "diff --git a/README.md b/README.md\n--- a/README.md\n+++ b/README.md\n@@ -1 +1 @@\n-old\n+new\n",
		"notes.md":   "# Review notes\n\nCollected from ChatGPT Pro.\n",
		"bundle.zip": "fake bundle",
	})
	installFakeExternalCollectFetcher(t, sourceDir, []string{"fix.patch", "notes.md", "bundle.zip"})

	payload := runCollectExternalJSON(t, vault, Args{"id": "APP-T-0001", "runner": "chatgpt-browser", "job": "cgpt_test", "covers": "A1"})
	assertEqual(t, true, payload.OK, "json ok")
	assertEqual(t, "apply_patch", payload.Collection.NextAction, "next action")
	assertEqual(t, "architect/APP-T-0001", payload.Collection.ArtifactDir, "artifact dir")
	assertEqual(t, []string{"architect/APP-T-0001/fix.patch"}, payload.Collection.Patches, "patches")
	assertEqual(t, []string{"architect/APP-T-0001/notes.md"}, payload.Collection.ReviewPackets, "review packets")
	assertEqual(t, []string{"architect/APP-T-0001/bundle.zip"}, payload.Collection.Bundles, "bundles")
	if len(payload.Collection.EvidenceAdded) != 1 {
		t.Fatalf("expected one evidence record, got %#v", payload.Collection.EvidenceAdded)
	}
	assertExists(t, filepath.Join(project.RepoRoot, "architect", "APP-T-0001", "fix.patch"))

	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	inputs, err := store.ListApplyInputsForRun(project.ProjectID, "APP-T-0001")
	if err != nil {
		t.Fatal(err)
	}
	if len(inputs) != 1 || inputs[0].RelPath != "architect/APP-T-0001/fix.patch" || inputs[0].Kind != "patch" {
		t.Fatalf("expected one patch apply input, got %#v", inputs)
	}

	evidencePath := filepath.Join(vault, "evidence", "APP-T-0001", payload.Collection.EvidenceAdded[0]+".md")
	data, body, err := parseFrontmatterMustRead(evidencePath)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "review_packet", stringField(data, "evidence_kind"), "evidence kind")
	if !strings.Contains(body, "external_job=cgpt_test") || !strings.Contains(body, "artifact_sha256=") {
		t.Fatalf("expected external identity in evidence body:\n%s", body)
	}
}

func TestAutomationCollectExternalIsIdempotent(t *testing.T) {
	vault := automationTestVault(t)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "External apply", "risk": "low", "priority": "p0", "v7": "true"}, newV7Task)
	makeV7TaskDispatchableForTest(t, vault, "APP-T-0001")
	project := registerAutomationTestProject(t, vault)

	sourceDir := writeExternalFetchFiles(t, map[string]string{
		"fix.patch": "diff --git a/README.md b/README.md\n--- a/README.md\n+++ b/README.md\n@@ -1 +1 @@\n-old\n+new\n",
		"notes.md":  "# Review notes\n\nCollected from ChatGPT Pro.\n",
	})
	installFakeExternalCollectFetcher(t, sourceDir, []string{"fix.patch", "notes.md"})

	first := runCollectExternalJSON(t, vault, Args{"id": "APP-T-0001", "runner": "chatgpt-browser", "job": "cgpt_same", "covers": "A1"})
	second := runCollectExternalJSON(t, vault, Args{"id": "APP-T-0001", "runner": "chatgpt-browser", "job": "cgpt_same", "covers": "A1"})
	if len(first.Collection.EvidenceAdded) != 1 {
		t.Fatalf("expected first run to add evidence, got %#v", first.Collection.EvidenceAdded)
	}
	if len(second.Collection.EvidenceAdded) != 0 {
		t.Fatalf("expected second run not to add duplicate evidence, got %#v", second.Collection.EvidenceAdded)
	}
	matches, err := filepath.Glob(filepath.Join(vault, "evidence", "APP-T-0001", "*.md"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 {
		t.Fatalf("expected one evidence file after repeat collection, got %d: %#v", len(matches), matches)
	}

	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	inputs, err := store.ListApplyInputsForRun(project.ProjectID, "APP-T-0001")
	if err != nil {
		t.Fatal(err)
	}
	if len(inputs) != 1 {
		t.Fatalf("expected one apply input after repeat collection, got %#v", inputs)
	}
}

func TestAutomationCollectExternalNotesOnlyRecordsResearchArtifact(t *testing.T) {
	vault := automationTestVault(t)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "External notes", "risk": "low", "priority": "p0", "v7": "true"}, newV7Task)
	makeV7TaskDispatchableForTest(t, vault, "APP-T-0001")
	registerAutomationTestProject(t, vault)

	sourceDir := writeExternalFetchFiles(t, map[string]string{"notes.md": "# Review notes\n"})
	installFakeExternalCollectFetcher(t, sourceDir, []string{"notes.md"})

	payload := runCollectExternalJSON(t, vault, Args{"id": "APP-T-0001", "runner": "chatgpt-browser", "job": "cgpt_notes", "covers": "A1"})
	assertEqual(t, "record_research_artifact", payload.Collection.NextAction, "next action")
	assertEqual(t, []string{"architect/APP-T-0001/notes.md"}, payload.Collection.ReviewPackets, "review packets")
	if payload.Collection.Dispatchable {
		t.Fatalf("notes-only collection should not dispatch directly")
	}
	if len(payload.Collection.EvidenceAdded) != 1 {
		t.Fatalf("expected notes evidence, got %#v", payload.Collection.EvidenceAdded)
	}
}

func TestAutomationCollectExternalMultiplePatchesEscalates(t *testing.T) {
	vault := automationTestVault(t)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "External apply", "risk": "low", "priority": "p0", "v7": "true"}, newV7Task)
	makeV7TaskDispatchableForTest(t, vault, "APP-T-0001")
	registerAutomationTestProject(t, vault)

	sourceDir := writeExternalFetchFiles(t, map[string]string{"one.patch": "diff --git a/a b/a\n", "two.diff": "diff --git a/b b/b\n"})
	installFakeExternalCollectFetcher(t, sourceDir, []string{"one.patch", "two.diff"})

	payload := runCollectExternalJSON(t, vault, Args{"id": "APP-T-0001", "runner": "chatgpt-browser", "job": "cgpt_multi"})
	assertEqual(t, false, payload.OK, "json ok")
	assertEqual(t, "escalate_human", payload.Collection.NextAction, "next action")
	if !containsString(payload.Collection.Blockers, "multiple patch artifacts require human selection") {
		t.Fatalf("expected multiple patch blocker, got %#v", payload.Collection.Blockers)
	}
}

func TestAutomationCollectExternalNoArtifactsEscalates(t *testing.T) {
	vault := automationTestVault(t)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "External empty", "risk": "low", "priority": "p0", "v7": "true"}, newV7Task)
	makeV7TaskDispatchableForTest(t, vault, "APP-T-0001")
	registerAutomationTestProject(t, vault)

	sourceDir := writeExternalFetchFiles(t, map[string]string{})
	installFakeExternalCollectFetcher(t, sourceDir, nil)

	payload := runCollectExternalJSON(t, vault, Args{"id": "APP-T-0001", "runner": "chatgpt-browser", "job": "cgpt_empty"})
	assertEqual(t, false, payload.OK, "json ok")
	assertEqual(t, "escalate_human", payload.Collection.NextAction, "next action")
	if len(payload.Collection.Blockers) == 0 || !strings.Contains(payload.Collection.Blockers[0], "no artifacts fetched") {
		t.Fatalf("expected no artifacts blocker, got %#v", payload.Collection.Blockers)
	}
}

func TestMirrorApplyInputsIntoWorkspaceCopiesTaskArtifactDir(t *testing.T) {
	vault := automationTestVault(t)
	project := registerAutomationTestProject(t, vault)
	artifactPath := filepath.Join(project.RepoRoot, "architect", "APP-T-0001", "fix.patch")
	if err := writeText(artifactPath, "diff --git a/README.md b/README.md\n"); err != nil {
		t.Fatal(err)
	}
	hash, err := sha256Path(artifactPath)
	if err != nil {
		t.Fatal(err)
	}
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	_, err = store.UpsertApplyInput(RuntimeApplyInput{ProjectID: project.ProjectID, RecordID: "APP-T-0001", ItemID: "APP-T-0001", Runner: "chatgpt-browser", JobID: "cgpt", Path: artifactPath, RelPath: "architect/APP-T-0001/fix.patch", Sha256: hash, Kind: "patch"})
	if err != nil {
		t.Fatal(err)
	}
	workspace := filepath.Join(t.TempDir(), "workspace")
	if err := mirrorApplyInputsIntoWorkspace(store, project, RunStatus{ProjectID: project.ProjectID, RecordID: "APP-T-0001"}, workspace); err != nil {
		t.Fatal(err)
	}
	assertExists(t, filepath.Join(workspace, "architect", "APP-T-0001", "fix.patch"))
}

func runCollectExternalJSON(t *testing.T, vault string, args Args) externalCollectJSONPayload {
	t.Helper()
	args["vault"] = vault
	args["json"] = "true"
	output := captureStdout(t, func() {
		if err := automationCollectExternalCmd(args); err != nil {
			t.Fatal(err)
		}
	})
	var payload externalCollectJSONPayload
	if err := json.Unmarshal([]byte(output), &payload); err != nil {
		t.Fatalf("parse collect JSON: %v\n%s", err, output)
	}
	return payload
}

func installFakeExternalCollectFetcher(t *testing.T, artifactDir string, files []string) {
	t.Helper()
	previous := runExternalCollectFetch
	runExternalCollectFetch = func(ctx context.Context, req externalFetchRequest) (externalFetchResult, error) {
		return externalFetchResult{JobID: req.JobID, ArtifactDir: artifactDir, Files: files}, nil
	}
	t.Cleanup(func() { runExternalCollectFetch = previous })
}

func writeExternalFetchFiles(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}
