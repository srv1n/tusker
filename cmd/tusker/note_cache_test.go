package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestNoteCacheUnchangedRescanDoesNotReadOrParse(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	path := filepath.Join(vault, "work", "tasks", "APP-T-0001.md")
	if err := writeText(path, "---\nid: APP-T-0001\nkind: task\n---\n\n# task\n"); err != nil {
		t.Fatal(err)
	}
	var reads, parses atomic.Int64
	noteCacheReadObserver = func() { reads.Add(1) }
	noteCacheParseObserver = func() { parses.Add(1) }
	t.Cleanup(func() {
		noteCacheReadObserver = nil
		noteCacheParseObserver = nil
	})
	if _, err := listAllNotesFrontmatter(vault); err != nil {
		t.Fatal(err)
	}
	if reads.Load() != 1 || parses.Load() != 1 {
		t.Fatalf("initial load reads=%d parses=%d, want 1 each", reads.Load(), parses.Load())
	}
	reads.Store(0)
	parses.Store(0)
	notes, err := listAllNotesFrontmatter(vault)
	if err != nil {
		t.Fatal(err)
	}
	if reads.Load() != 0 || parses.Load() != 0 {
		t.Fatalf("unchanged rescan reads=%d parses=%d, want 0 each", reads.Load(), parses.Load())
	}
	if len(notes) != 1 || notes[0].Body != "" || stringField(notes[0].Data, "id") != "APP-T-0001" {
		t.Fatalf("frontmatter-only note = %#v", notes)
	}
}

func TestNoteCacheFrontmatterWarmHitStillAllowsFullBodyLoad(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	path := filepath.Join(vault, "work", "tasks", "APP-T-0001.md")
	if err := writeText(path, "---\nid: APP-T-0001\nkind: task\n---\n\n# full body\n\nContract detail.\n"); err != nil {
		t.Fatal(err)
	}
	frontmatter, err := listAllNotesFrontmatter(vault)
	if err != nil {
		t.Fatal(err)
	}
	if len(frontmatter) != 1 || frontmatter[0].Body != "" {
		t.Fatalf("frontmatter-only load leaked body: %#v", frontmatter)
	}
	full, err := listAllNotes(vault)
	if err != nil {
		t.Fatal(err)
	}
	if len(full) != 1 || !strings.Contains(full[0].Body, "Contract detail.") {
		t.Fatalf("full-body load reused incomplete cache entry: %#v", full)
	}
}

func TestNoteCacheIdlePollDoesNotReadOrParseNotes(t *testing.T) {
	vault := automationTestVault(t)
	if err := writeText(filepath.Join(vault, "work", "notes", "fixture.md"), "---\nid: fixture\nkind: note\n---\n\n# fixture\n"); err != nil {
		t.Fatal(err)
	}
	registerAutomationTestProject(t, vault)
	daemon, err := NewDaemon(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer daemon.Close()
	if err := daemon.PollOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	var reads, parses atomic.Int64
	noteCacheReadObserver = func() { reads.Add(1) }
	noteCacheParseObserver = func() { parses.Add(1) }
	t.Cleanup(func() {
		noteCacheReadObserver = nil
		noteCacheParseObserver = nil
	})
	if err := daemon.PollOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if reads.Load() != 0 || parses.Load() != 0 {
		t.Fatalf("idle poll reads=%d parses=%d, want 0 each", reads.Load(), parses.Load())
	}
}

func TestNoteCacheInvalidatesChangedAndDeletedNotes(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	first := filepath.Join(vault, "work", "tasks", "APP-T-0001.md")
	second := filepath.Join(vault, "work", "tasks", "APP-T-0002.md")
	for path, id := range map[string]string{first: "APP-T-0001", second: "APP-T-0002"} {
		if err := writeText(path, "---\nid: "+id+"\nkind: task\n---\n\n# task\n"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := listAllNotesFrontmatter(vault); err != nil {
		t.Fatal(err)
	}
	if err := writeText(first, "---\nid: APP-T-0001\nkind: task\nchanged: true\n---\n\n# task changed\n"); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(second); err != nil {
		t.Fatal(err)
	}
	notes, err := listAllNotesFrontmatter(vault)
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 1 || !boolField(notes[0].Data, "changed") {
		t.Fatalf("cache did not apply change/delete: %#v", notes)
	}
}

func TestNoteCacheHashFallbackDetectsSameSizeCoarseMtimeEdit(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	path := filepath.Join(vault, "work", "tasks", "APP-T-0001.md")
	original := "---\nid: APP-T-0001\nkind: task\nflag: one\n---\n\n# task\n"
	changed := "---\nid: APP-T-0001\nkind: task\nflag: two\n---\n\n# task\n"
	if len(original) != len(changed) {
		t.Fatal("test fixture must preserve file size")
	}
	if err := writeText(path, original); err != nil {
		t.Fatal(err)
	}
	stamp := time.Now().Truncate(time.Second)
	if err := os.Chtimes(path, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	if _, err := listAllNotesFrontmatter(vault); err != nil {
		t.Fatal(err)
	}
	if err := writeText(path, changed); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, stamp, stamp); err != nil {
		t.Fatal(err)
	}

	var reads, parses atomic.Int64
	noteCacheReadObserver = func() { reads.Add(1) }
	noteCacheParseObserver = func() { parses.Add(1) }
	t.Cleanup(func() {
		noteCacheReadObserver = nil
		noteCacheParseObserver = nil
	})
	notes, err := listAllNotesFrontmatter(vault)
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 1 || stringField(notes[0].Data, "flag") != "two" {
		t.Fatalf("coarse-mtime cache returned stale note: %#v", notes)
	}
	if reads.Load() != 1 || parses.Load() != 1 {
		t.Fatalf("hash fallback reads=%d parses=%d, want one changed-file reload", reads.Load(), parses.Load())
	}
}

func TestReconcileUsesOneIndexForCleanVault(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	if err := bootstrapV7Profile(vault, "v7"); err != nil {
		t.Fatal(err)
	}
	var loads atomic.Int64
	loadV7IndexObserver = func() { loads.Add(1) }
	t.Cleanup(func() { loadV7IndexObserver = nil })
	if err := reconcileV7Cmd(Args{"vault": vault, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	if loads.Load() > 2 {
		t.Fatalf("reconcile index loads=%d, want initial load plus at most one post-mutation reload", loads.Load())
	}
}
