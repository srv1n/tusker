package main

import (
	"os"
	"path/filepath"
	"testing"
)

// initDispatchGitRepoForTest gives daemon dispatch fixtures a real repository
// with a commit. Their default workspace strategy is Git-backed, so an empty
// temporary directory is not a valid project fixture.
func initDispatchGitRepoForTest(t *testing.T, repo string) {
	t.Helper()
	runGitDir(t, repo, "init")
	runGitDir(t, repo, "config", "user.email", "tusker-tests@example.com")
	runGitDir(t, repo, "config", "user.name", "Tusker Tests")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("dispatch fixture\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitDir(t, repo, "add", "README.md")
	runGitDir(t, repo, "commit", "-m", "seed dispatch fixture")
}
