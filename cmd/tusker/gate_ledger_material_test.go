package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func ignoredMaterialRepo(t *testing.T, patterns string) string {
	t.Helper()
	repo := orchestrationGitRepo(t)
	if err := os.WriteFile(filepath.Join(repo, ".gitignore"), []byte(patterns), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitDir(t, repo, "add", ".gitignore")
	runGitDir(t, repo, "commit", "-m", "ignore generated material")
	return repo
}

func TestWorkspaceTreeStateHashIncludesIgnoredGeneratedOutput(t *testing.T) {
	repo := ignoredMaterialRepo(t, "dist/\n")
	dist := filepath.Join(repo, "dist")
	if err := os.MkdirAll(dist, 0o755); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(dist, "bundle.js")
	if err := os.WriteFile(output, []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	withoutGenerated, err := workspaceTreeStateHashForPaths(repo, []string{"dist"})
	if err != nil {
		t.Fatal(err)
	}
	before, err := workspaceTreeStateHashForPaths(repo, []string{"dist"}, []string{"dist"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(output, []byte("two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	withoutGeneratedAfter, err := workspaceTreeStateHashForPaths(repo, []string{"dist"})
	if err != nil {
		t.Fatal(err)
	}
	if withoutGenerated != withoutGeneratedAfter {
		t.Fatal("source-only scope started hashing ignored generated output")
	}
	after, err := workspaceTreeStateHashForPaths(repo, []string{"dist"}, []string{"dist"})
	if err != nil {
		t.Fatal(err)
	}
	if before == after {
		t.Fatal("ignored generated output drift did not invalidate material hash")
	}
}

func TestWorkspaceTreeStateHashIncludesExecutableMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not expose Git executable mode through chmod")
	}
	repo := orchestrationGitRepo(t)
	path := filepath.Join(repo, "tracked.txt")
	before, err := workspaceTreeStateHashForPaths(repo, []string{"tracked.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatal(err)
	}
	after, err := workspaceTreeStateHashForPaths(repo, []string{"tracked.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if before == after {
		t.Fatal("executable-bit drift did not invalidate material hash")
	}
}

func TestWorkspaceTreeStateHashIncludesGeneratedSymlinkType(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows symlink support is not guaranteed")
	}
	repo := ignoredMaterialRepo(t, "dist/\n")
	dist := filepath.Join(repo, "dist")
	if err := os.MkdirAll(dist, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dist, "tool")
	if err := os.WriteFile(path, []byte("target"), 0o644); err != nil {
		t.Fatal(err)
	}
	regular, err := workspaceTreeStateHashForPaths(repo, []string{"dist"}, []string{"dist"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("target", path); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	symlink, err := workspaceTreeStateHashForPaths(repo, []string{"dist"}, []string{"dist"})
	if err != nil {
		t.Fatal(err)
	}
	if regular == symlink {
		t.Fatal("regular-file to symlink drift did not invalidate material hash")
	}
}

func TestWorkspaceTreeStateHashExcludesUnrelatedIgnoredFiles(t *testing.T) {
	repo := ignoredMaterialRepo(t, "dist/\ndist-old/\nsecrets/\nsrc/generated/\n")
	for _, rel := range []string{"dist", "dist-old", "secrets", "src/generated"} {
		if err := os.MkdirAll(filepath.Join(repo, rel), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string]string{
		"dist/bundle.js":      "dist-one\n",
		"dist-old/bundle.js":  "old-one\n",
		"secrets/token":       "secret-one\n",
		"src/generated/cache": "source-one\n",
	}
	for rel, content := range files {
		if err := os.WriteFile(filepath.Join(repo, rel), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	scope := []string{"dist", "dist-old", "secrets", "src"}
	generated := []string{"dist"}
	before, err := workspaceTreeStateHashForPaths(repo, scope, generated)
	if err != nil {
		t.Fatal(err)
	}
	for rel, content := range map[string]string{
		"dist-old/bundle.js":  "old-two\n",
		"secrets/token":       "secret-two\n",
		"src/generated/cache": "source-two\n",
	} {
		if err := os.WriteFile(filepath.Join(repo, rel), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	afterUnrelated, err := workspaceTreeStateHashForPaths(repo, scope, generated)
	if err != nil {
		t.Fatal(err)
	}
	if before != afterUnrelated {
		t.Fatal("ignored files outside the exact generated-output root changed material hash")
	}
	if err := os.WriteFile(filepath.Join(repo, "dist/bundle.js"), []byte("dist-two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	afterGenerated, err := workspaceTreeStateHashForPaths(repo, scope, generated)
	if err != nil {
		t.Fatal(err)
	}
	if before == afterGenerated {
		t.Fatal("ignored file under generated-output root did not change material hash")
	}
}
