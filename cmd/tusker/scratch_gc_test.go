package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// newScratchVault builds the minimum layout vaultAuthorizesDeletion accepts.
func newScratchVault(t *testing.T) string {
	t.Helper()
	vault := filepath.Join(t.TempDir(), "vault")
	if err := ensureDir(filepath.Join(vault, "work", "tasks")); err != nil {
		t.Fatal(err)
	}
	if err := writeText(filepath.Join(vault, "WORKFLOW.md"), "# workflow\n"); err != nil {
		t.Fatal(err)
	}
	return vault
}

// backdateTree stamps every path beneath root (children first) so the entry's
// newest mtime is `age` old.
func backdateTree(t *testing.T, root string, age time.Duration) {
	t.Helper()
	stamp := time.Now().Add(-age)
	var paths []string
	if err := filepath.WalkDir(root, func(p string, _ fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		paths = append(paths, p)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for i := len(paths) - 1; i >= 0; i-- {
		if err := os.Chtimes(paths[i], stamp, stamp); err != nil {
			t.Fatal(err)
		}
	}
}

// seedAgedScratch creates <vault>/scratch/<name>/blob.bin with the given body
// and backdates the entry.
func seedAgedScratch(t *testing.T, vault, name string, age time.Duration) string {
	t.Helper()
	return seedAgedScratchBody(t, vault, name, "scratch exhaust\n", age)
}

func seedAgedScratchBody(t *testing.T, vault, name, body string, age time.Duration) string {
	t.Helper()
	dir := filepath.Join(vault, "scratch", name)
	if err := ensureDir(dir); err != nil {
		t.Fatal(err)
	}
	if err := writeText(filepath.Join(dir, "blob.bin"), body); err != nil {
		t.Fatal(err)
	}
	backdateTree(t, dir, age)
	return dir
}

func TestScratchGCDryRunListsStaleEntries(t *testing.T) {
	vault := newScratchVault(t)
	stale := seedAgedScratch(t, vault, "rejected_renders", 30*24*time.Hour)

	entries, err := planScratchGC(vault, defaultScratchTTLDays*24*time.Hour, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name != "rejected_renders" {
		t.Fatalf("expected the stale entry in the plan, got %+v", entries)
	}
	if err := scratchGCCmd(Args{"vault": vault, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stale); err != nil {
		t.Fatalf("dry run must not delete anything: %v", err)
	}
}

func TestScratchGCAppliesWithYes(t *testing.T) {
	vault := newScratchVault(t)
	stale := seedAgedScratch(t, vault, "APP-T-0001", 30*24*time.Hour)

	if err := scratchGCCmd(Args{"vault": vault, "quiet": "true", "yes": "true"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("expected %s to be deleted, got err=%v", stale, err)
	}
}

func TestScratchGCSparesFreshEntries(t *testing.T) {
	vault := newScratchVault(t)
	freshTask := seedAgedScratch(t, vault, "APP-T-0002", time.Hour)
	freshNamed := seedAgedScratch(t, vault, "orig-piano", 13*24*time.Hour)
	stale := seedAgedScratch(t, vault, "old-render", 30*24*time.Hour)

	if err := scratchGCCmd(Args{"vault": vault, "quiet": "true", "yes": "true"}); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{freshTask, freshNamed} {
		if _, err := os.Stat(dir); err != nil {
			t.Fatalf("expected %s to survive: %v", dir, err)
		}
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("expected %s to be deleted, got err=%v", stale, err)
	}

	// --ttl overrides the 14-day default and now takes the 13-day entry.
	if err := scratchGCCmd(Args{"vault": vault, "quiet": "true", "yes": "true", "ttl": "7"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(freshNamed); !os.IsNotExist(err) {
		t.Fatalf("expected %s to be deleted under --ttl 7, got err=%v", freshNamed, err)
	}
	if _, err := os.Stat(freshTask); err != nil {
		t.Fatalf("expected %s to survive --ttl 7: %v", freshTask, err)
	}
}

func TestScratchGCRejectsOverflowTTL(t *testing.T) {
	vault := newScratchVault(t)
	fresh := seedAgedScratch(t, vault, "APP-T-0003", time.Hour)

	// 106752 days overflows an int64 nanosecond Duration and would wrap the
	// cutoff into the future, making every entry look stale.
	if err := scratchGCCmd(Args{"vault": vault, "quiet": "true", "yes": "true", "ttl": "106752"}); err == nil {
		t.Fatal("expected an overflowing --ttl to be rejected")
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Fatalf("expected %s to survive a rejected --ttl: %v", fresh, err)
	}
}

func TestScratchGCRejectsMalformedYes(t *testing.T) {
	vault := newScratchVault(t)
	stale := seedAgedScratch(t, vault, "old-render", 30*24*time.Hour)

	if err := scratchGCCmd(Args{"vault": vault, "quiet": "true", "yes": "definitely-not"}); err == nil {
		t.Fatal("expected a malformed --yes to be rejected")
	}
	if _, err := os.Stat(stale); err != nil {
		t.Fatalf("expected %s to survive a malformed --yes: %v", stale, err)
	}
}

func TestScratchGCRefusesNonVault(t *testing.T) {
	dir := t.TempDir()
	if err := ensureDir(filepath.Join(dir, "work")); err != nil {
		t.Fatal(err)
	}
	stale := seedAgedScratchBody(t, dir, "old-render", "payload\n", 30*24*time.Hour)

	err := scratchGCCmd(Args{"vault": dir, "quiet": "true", "yes": "true"})
	if err == nil || !strings.Contains(err.Error(), "not a recognized Tusker vault") {
		t.Fatalf("expected a non-vault refusal, got %v", err)
	}
	if _, err := os.Stat(stale); err != nil {
		t.Fatalf("expected %s to survive outside a vault: %v", stale, err)
	}
}

func TestScratchGCReportsPartialFailure(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	vault := newScratchVault(t)
	big := seedAgedScratchBody(t, vault, "big-render", strings.Repeat("x", 8192), 30*24*time.Hour)
	locked := seedAgedScratchBody(t, vault, "locked-render", "small\n", 30*24*time.Hour)

	inner := filepath.Join(locked, "inner")
	if err := ensureDir(inner); err != nil {
		t.Fatal(err)
	}
	if err := writeText(filepath.Join(inner, "pinned.bin"), "pinned\n"); err != nil {
		t.Fatal(err)
	}
	backdateTree(t, locked, 30*24*time.Hour)
	// Read+execute only: the tree can still be walked, but nothing inside can
	// be unlinked, so RemoveAll fails partway through the sweep.
	if err := os.Chmod(inner, 0o500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(inner, 0o700)

	err := scratchGCCmd(Args{"vault": vault, "quiet": "true", "yes": "true"})
	if err == nil {
		t.Fatal("expected the failed removal to surface as an error")
	}
	typed, ok := err.(*TuskerError)
	if !ok {
		t.Fatalf("expected a TuskerError, got %T", err)
	}
	if !strings.Contains(typed.Path, "locked-render") {
		t.Fatalf("expected the failing path to be reported, got %q", typed.Path)
	}
	context, ok := typed.Context.(map[string]any)
	if !ok {
		t.Fatalf("expected outcome context, got %T", typed.Context)
	}
	deleted, ok := context["deleted"].([]map[string]any)
	if !ok || len(deleted) != 1 || deleted[0]["name"] != "big-render" {
		t.Fatalf("expected big-render reported as deleted, got %v", context["deleted"])
	}
	if _, err := os.Stat(big); !os.IsNotExist(err) {
		t.Fatalf("expected %s to be deleted before the failure, got err=%v", big, err)
	}
	if _, err := os.Stat(locked); err != nil {
		t.Fatalf("expected %s to survive the failed removal: %v", locked, err)
	}
}
