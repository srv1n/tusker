package main

import (
	"path/filepath"
	"testing"
)

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := writeText(path, content); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestMigrateLegacyVaultToV5ConvertsStoriesBugsAndDocs(t *testing.T) {
	vault := t.TempDir()
	mustWrite(t, filepath.Join(vault, "WORKFLOW.md"), "---\ntracker_schema_version: 2\n---\n")
	mustWrite(t, filepath.Join(vault, "epics", "ABC", "index.md"), `---
schema_version: 2
record_id: "01ABC"
id: "ABC"
title: "Alpha"
type: "epic"
status: "active"
owner: "sarav"
summary: "Legacy epic"
created: "2026-04-01"
updated: "2026-04-01"
docs:
  - "[[ABC-D-0001]]"
---

# ABC · Alpha

## Problem

Legacy epic body.
`)
	mustWrite(t, filepath.Join(vault, "epics", "ABC", "ABC-S-0001.md"), `---
schema_version: 2
record_id: "01STORY"
id: "ABC-S-0001"
title: "Ship the feature"
type: "story"
status: "in_review"
change_type: "feature"
epic: "[[ABC]]"
risk: "medium"
size: "s"
priority: "p1"
blocks:
  - "[[ABC-B-0001]]"
created: "2026-04-01"
updated: "2026-04-01"
---

# ABC-S-0001 · Ship the feature

## Problem

Legacy story body.
`)
	mustWrite(t, filepath.Join(vault, "epics", "ABC", "ABC-B-0001.md"), `---
schema_version: 2
record_id: "01BUG"
id: "ABC-B-0001"
title: "Fix the bug"
type: "bug"
status: "active"
change_type: "bug"
epic: "[[ABC]]"
risk: "medium"
size: "s"
priority: "p2"
blocked_by:
  - "[[ABC-S-0001]]"
created: "2026-04-01"
updated: "2026-04-01"
---

# ABC-B-0001 · Fix the bug

## Summary

Legacy bug body.
`)
	mustWrite(t, filepath.Join(vault, "epics", "ABC", "ABC-D-0001.md"), `---
schema_version: 2
record_id: "01DOC"
id: "ABC-D-0001"
title: "Alpha Canon"
type: "doc"
status: "approved"
epic: "[[ABC]]"
canonical: true
canonical_status: "approved"
publish: true
publish_path: "developer/alpha"
publish_description: "Alpha canon."
story_record_id: "01STORY"
created: "2026-04-01"
updated: "2026-04-01"
---

# Alpha Canon
`)
	if err := bootstrap(Args{"vault": vault, "quiet": "true"}); err != nil {
		t.Fatalf("bootstrap failed: %v", err)
	}
	report, err := migrateLegacyVaultToV5(Args{"vault": vault, "no-backup": "true"})
	if err != nil {
		t.Fatalf("migration failed: %v", err)
	}
	if report.FilesMoved != 3 {
		t.Fatalf("expected 3 moved files, got %d", report.FilesMoved)
	}
	assertExists(t, filepath.Join(vault, "epics", "ABC", "ABC.md"))
	assertExists(t, filepath.Join(vault, "epics", "ABC", "ABC-T-0001.md"))
	assertExists(t, filepath.Join(vault, "epics", "ABC", "ABC-T-0002.md"))
	storyData, _, err := parseFrontmatterMustRead(filepath.Join(vault, "epics", "ABC", "ABC-T-0001.md"))
	if err != nil {
		t.Fatalf("read migrated story: %v", err)
	}
	assertEqual(t, "tusker.task/v5", stringField(storyData, "schema"), "story schema")
	assertEqual(t, "task", stringField(storyData, "type"), "story type")
	assertEqual(t, "feature", stringField(storyData, "kind"), "story kind")
	assertEqual(t, "review", stringField(storyData, "status"), "story status")
	assertEqual(t, "", stringField(storyData, "record_id"), "legacy record id should be removed")
	assertEqual(t, "", stringField(storyData, "schema_version"), "legacy schema version should be removed")
	assertEqual(t, "ABC-T-0002", wikiTarget(normalizeList(storyData["blocks"])[0]), "story block link")
	bugData, _, err := parseFrontmatterMustRead(filepath.Join(vault, "epics", "ABC", "ABC-T-0002.md"))
	if err != nil {
		t.Fatalf("read migrated bug: %v", err)
	}
	assertEqual(t, "bug", stringField(bugData, "kind"), "bug kind")
	assertEqual(t, "ABC-T-0001", wikiTarget(normalizeList(bugData["blocked_by"])[0]), "bug blocked_by link")
	docsMap, err := loadDocsMap(vault)
	if err != nil {
		t.Fatalf("load docs map: %v", err)
	}
	if _, ok := docsMap.Node("developer/alpha"); !ok {
		t.Fatalf("expected migrated published doc in docs map")
	}
	docData, _, err := parseFrontmatterMustRead(filepath.Join(vault, "epics", "ABC", "ABC-D-0001.md"))
	if err != nil {
		t.Fatalf("read migrated doc: %v", err)
	}
	assertEqual(t, "", stringField(docData, "story_record_id"), "legacy story record id should be removed")
	if code, err := validateCmd(Args{"vault": vault, "json": "true"}); err != nil || code != 0 {
		t.Fatalf("validate failed after migration: code=%d err=%v", code, err)
	}
}
