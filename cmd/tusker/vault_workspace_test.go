package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWorkspaceVaultMountUnmountAndRepair(t *testing.T) {
	tempRoot := t.TempDir()
	t.Setenv("TUSKER_STATE_ROOT", filepath.Join(tempRoot, "state"))

	repo := filepath.Join(tempRoot, "my-app")
	tracker := filepath.Join(repo, ".tusker")
	obsidian := filepath.Join(tempRoot, "obsidian-work")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	oldWD, _ := os.Getwd()
	defer os.Chdir(oldWD)
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}

	if err := bootstrap(Args{"vault": tracker, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	if err := vaultSetCmd(Args{"path": obsidian}); err != nil {
		t.Fatal(err)
	}
	if err := vaultMountCmd(Args{"quiet": "true"}); err != nil {
		t.Fatal(err)
	}

	mountPath := filepath.Join(obsidian, "my-app")
	assertSymlinkTarget(t, mountPath, tracker)
	assertExists(t, filepath.Join(tracker, "_system", "project.yaml"))

	cfg, err := loadWorkspaceVaultConfig()
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, obsidian, cfg.ObsidianVault, "configured Obsidian vault")
	assertEqual(t, 1, len(cfg.Projects), "mounted project count")
	assertEqual(t, "my-app", cfg.Projects[0].MountName, "mount name")

	if err := os.Remove(mountPath); err != nil {
		t.Fatal(err)
	}
	if err := vaultRepairCmd(Args{}); err != nil {
		t.Fatal(err)
	}
	assertSymlinkTarget(t, mountPath, tracker)

	if err := vaultUnmountCmd(Args{"name": "my-app"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(mountPath); !os.IsNotExist(err) {
		t.Fatalf("expected mount symlink removed, got err=%v", err)
	}
	assertExists(t, tracker)
}

func TestInitMountsTrackerIntoConfiguredWorkspaceVault(t *testing.T) {
	tempRoot := t.TempDir()
	t.Setenv("TUSKER_STATE_ROOT", filepath.Join(tempRoot, "state"))

	repo := filepath.Join(tempRoot, "repo")
	obsidian := filepath.Join(tempRoot, "work")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	oldWD, _ := os.Getwd()
	defer os.Chdir(oldWD)
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}

	if err := vaultSetCmd(Args{"path": obsidian}); err != nil {
		t.Fatal(err)
	}
	if err := initCmd(Args{"yes": "true", "mount": "true"}); err != nil {
		t.Fatal(err)
	}

	tracker := filepath.Join(repo, ".tusker")
	assertSymlinkTarget(t, filepath.Join(obsidian, "repo"), tracker)
	assertExists(t, filepath.Join(tracker, "WORKFLOW.md"))
	assertExists(t, filepath.Join(tracker, "_system", "project.yaml"))
}

func TestEnsureWorkspaceMountRepairsHistoricalTuskerSymlink(t *testing.T) {
	tempRoot := t.TempDir()
	repo := filepath.Join(tempRoot, "repo")
	tracker := filepath.Join(repo, ".tusker")
	mountPath := filepath.Join(tempRoot, "work", "repo")
	if err := os.MkdirAll(tracker, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(mountPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(repo, "tusker"), mountPath); err != nil {
		t.Fatal(err)
	}

	if err := ensureWorkspaceMount(mountPath, tracker, false); err != nil {
		t.Fatal(err)
	}

	assertSymlinkTarget(t, mountPath, tracker)
}

func TestWorkspaceVaultMountRepoDiscoversRepoTracker(t *testing.T) {
	tempRoot := t.TempDir()
	t.Setenv("TUSKER_STATE_ROOT", filepath.Join(tempRoot, "state"))

	repo := filepath.Join(tempRoot, "repo")
	tracker := filepath.Join(repo, "tusker")
	obsidian := filepath.Join(tempRoot, "work")
	other := filepath.Join(tempRoot, "other")
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := bootstrap(Args{"vault": tracker, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	if err := vaultSetCmd(Args{"path": obsidian}); err != nil {
		t.Fatal(err)
	}
	oldWD, _ := os.Getwd()
	defer os.Chdir(oldWD)
	if err := os.Chdir(other); err != nil {
		t.Fatal(err)
	}

	if err := vaultMountCmd(Args{"repo": repo, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	assertSymlinkTarget(t, filepath.Join(obsidian, "repo"), tracker)
}

func TestWorkspaceVaultMoveRemountsProjectsAtNewRoot(t *testing.T) {
	tempRoot := t.TempDir()
	t.Setenv("TUSKER_STATE_ROOT", filepath.Join(tempRoot, "state"))

	repo := filepath.Join(tempRoot, "client-mobile")
	tracker := filepath.Join(repo, "tusker")
	obsidian := filepath.Join(tempRoot, "work")
	nextObsidian := filepath.Join(tempRoot, "work-moved")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := bootstrap(Args{"vault": tracker, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	if err := vaultSetCmd(Args{"path": obsidian}); err != nil {
		t.Fatal(err)
	}
	if err := vaultMountCmd(Args{"repo": repo, "vault": tracker, "name": "mobile", "quiet": "true"}); err != nil {
		t.Fatal(err)
	}

	if err := vaultMoveCmd(Args{"to": nextObsidian}); err != nil {
		t.Fatal(err)
	}
	if fileExists(obsidian) {
		t.Fatalf("expected old Obsidian vault path to move away: %s", obsidian)
	}
	assertSymlinkTarget(t, filepath.Join(nextObsidian, "mobile"), tracker)

	cfg, err := loadWorkspaceVaultConfig()
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, nextObsidian, cfg.ObsidianVault, "moved Obsidian vault")
	assertEqual(t, filepath.Join(nextObsidian, "mobile"), cfg.Projects[0].MountPath, "moved mount path")
}

func TestWorkspaceVaultMountRefusesNonSymlinkCollision(t *testing.T) {
	tempRoot := t.TempDir()
	t.Setenv("TUSKER_STATE_ROOT", filepath.Join(tempRoot, "state"))

	repo := filepath.Join(tempRoot, "repo")
	tracker := filepath.Join(repo, "tusker")
	obsidian := filepath.Join(tempRoot, "work")
	if err := os.MkdirAll(filepath.Join(obsidian, "repo"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := bootstrap(Args{"vault": tracker, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	if err := vaultSetCmd(Args{"path": obsidian}); err != nil {
		t.Fatal(err)
	}
	err := vaultMountCmd(Args{"repo": repo, "vault": tracker, "quiet": "true"})
	if err == nil {
		t.Fatal("expected non-symlink mount collision to fail")
	}
	issue := errorToIssue(err)
	assertEqual(t, errorAlreadyExists, issue.Code, "collision error code")
}

func assertSymlinkTarget(t *testing.T, linkPath, targetPath string) {
	t.Helper()
	info, err := os.Lstat(linkPath)
	if err != nil {
		t.Fatalf("expected symlink at %s: %v", linkPath, err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("expected symlink at %s", linkPath)
	}
	target, err := os.Readlink(linkPath)
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(linkPath), target)
	}
	assertEqual(t, canonicalPath(targetPath), canonicalPath(target), "symlink target")
}
