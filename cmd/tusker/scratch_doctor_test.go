package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// newScratchDoctorVault builds the minimum layout vaultAuthorizesDeletion
// accepts: a v7 work tree plus one vault marker.
func newScratchDoctorVault(t *testing.T) (repo, vault string) {
	t.Helper()
	repo = t.TempDir()
	vault = filepath.Join(repo, ".tusker")
	for _, dir := range []string{filepath.Join(vault, "work", "tasks"), filepath.Join(vault, "_system")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return repo, vault
}

func ageScratchEntry(t *testing.T, dir string, age time.Duration) {
	t.Helper()
	stamp := time.Now().Add(-age)
	// scanScratchEntries takes the newest mtime across the whole subtree,
	// including directories; age everything or a directory's own mtime (set at
	// creation time) keeps the entry looking fresh.
	err := filepath.Walk(dir, func(path string, _ os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		return os.Chtimes(path, stamp, stamp)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func writeScratchEntry(t *testing.T, vault, name string, size int64, age time.Duration) {
	t.Helper()
	dir := filepath.Join(vault, "scratch", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "data.bin")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	// Sparse: FileInfo.Size reports the full length without writing the blocks.
	if err := os.Truncate(path, size); err != nil {
		t.Fatal(err)
	}
	ageScratchEntry(t, dir, age)
}

func TestScratchDoctorWarnsOverBudget(t *testing.T) {
	repo, vault := newScratchDoctorVault(t)

	under, err := runSetupDoctor(setupDoctorInput{RepoRoot: repo}, false)
	if err != nil {
		t.Fatal(err)
	}
	if findingByCode(under, "scratch_size") != nil {
		t.Fatalf("finding present under budget: %#v", under.Findings)
	}

	writeScratchEntry(t, vault, "big", defaultScratchBudgetBytes+(1<<20), 0)

	over, err := runSetupDoctor(setupDoctorInput{RepoRoot: repo}, false)
	if err != nil {
		t.Fatal(err)
	}
	assertSetupFinding(t, over, "scratch_size", false)
}

func TestScratchDoctorRepairReclaims(t *testing.T) {
	repo, vault := newScratchDoctorVault(t)

	// One stale entry (older than the TTL) and one fresh entry, together over budget.
	writeScratchEntry(t, vault, "stale", defaultScratchBudgetBytes, defaultScratchTTLDays*24*time.Hour+time.Hour)
	writeScratchEntry(t, vault, "fresh", 1<<20, 0)

	dry, err := runSetupDoctor(setupDoctorInput{RepoRoot: repo}, false)
	if err != nil {
		t.Fatal(err)
	}
	assertSetupFinding(t, dry, "scratch_size", false)
	if !dirExists(filepath.Join(vault, "scratch", "stale")) {
		t.Fatal("dry run must not delete stale scratch")
	}

	repaired, err := runSetupDoctor(setupDoctorInput{RepoRoot: repo}, true)
	if err != nil {
		t.Fatal(err)
	}
	assertSetupFinding(t, repaired, "scratch_size", true)
	if dirExists(filepath.Join(vault, "scratch", "stale")) {
		t.Fatal("repair must remove stale scratch entry")
	}
	if !dirExists(filepath.Join(vault, "scratch", "fresh")) {
		t.Fatal("repair must keep fresh scratch entry")
	}
}

func TestScratchDoctorReportsUninspectableScratch(t *testing.T) {
	repo, vault := newScratchDoctorVault(t)
	if err := os.Symlink(t.TempDir(), filepath.Join(vault, "scratch")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	report, err := runSetupDoctor(setupDoctorInput{RepoRoot: repo}, false)
	if err != nil {
		t.Fatal(err)
	}
	finding := findingByCode(report, "scratch_uninspectable")
	if finding == nil {
		t.Fatalf("symlinked scratch must be reported: %#v", report.Findings)
	}
	if finding.Repairable {
		t.Fatal("an uninspectable scratch root is not repairable by setup doctor")
	}
	if findingByCode(report, "scratch_size") != nil {
		t.Fatal("must not claim a size it could not measure")
	}
}

func TestScratchDoctorChangedOnEmptyStaleDirs(t *testing.T) {
	repo, vault := newScratchDoctorVault(t)

	writeScratchEntry(t, vault, "fresh", defaultScratchBudgetBytes+(1<<20), 0)
	empty := filepath.Join(vault, "scratch", "empty")
	if err := os.MkdirAll(empty, 0o755); err != nil {
		t.Fatal(err)
	}
	ageScratchEntry(t, empty, defaultScratchTTLDays*24*time.Hour+time.Hour)

	report, err := runSetupDoctor(setupDoctorInput{RepoRoot: repo}, true)
	if err != nil {
		t.Fatal(err)
	}
	// Zero bytes reclaimed, but a directory did disappear.
	assertSetupFinding(t, report, "scratch_size", true)
	if dirExists(empty) {
		t.Fatal("repair must remove the stale empty directory")
	}
}
