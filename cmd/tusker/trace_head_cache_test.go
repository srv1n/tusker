package main

import (
	"strings"
	"testing"
)

func TestTraceCodeSHAHeadCacheAvoidsGitExecWhenHeadIsUnchanged(t *testing.T) {
	repo := traceHeadCacheTestRepo(t)
	execs := 0
	cache := newTraceGitHeadCache(func(root string) string {
		execs++
		return resolveGitHeadWithCommand(root)
	})
	first := cache.resolve(repo)
	if first == "unknown" {
		t.Fatal("initial HEAD was not resolved")
	}
	for range 5 {
		if got := cache.resolve(repo); got != first {
			t.Fatalf("unchanged HEAD changed from %q to %q", first, got)
		}
	}
	if execs != 1 {
		t.Fatalf("unchanged HEAD spawned %d git commands, want one cold fill and zero warm execs", execs)
	}
}

func TestTraceCodeSHAHeadCacheInvalidatesImmediatelyAfterCommit(t *testing.T) {
	repo := traceHeadCacheTestRepo(t)
	execs := 0
	cache := newTraceGitHeadCache(func(root string) string {
		execs++
		return resolveGitHeadWithCommand(root)
	})
	before := cache.resolve(repo)
	if err := writeText(repo+"/fixture.txt", "two\n"); err != nil {
		t.Fatal(err)
	}
	runGitDir(t, repo, "add", "fixture.txt")
	runGitDir(t, repo, "commit", "-m", "second")
	after := cache.resolve(repo)
	if before == after || after == "unknown" {
		t.Fatalf("HEAD cache did not invalidate: before=%q after=%q", before, after)
	}
	if execs != 2 {
		t.Fatalf("HEAD change spawned %d git commands, want one per distinct ref version", execs)
	}
	if want := strings.TrimSpace(gitDirOutput(t, repo, "rev-parse", "HEAD")); after != want {
		t.Fatalf("cached HEAD=%q, git HEAD=%q", after, want)
	}
}

func traceHeadCacheTestRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	runGitDir(t, repo, "init", "-b", "main")
	runGitDir(t, repo, "config", "user.email", "test@example.com")
	runGitDir(t, repo, "config", "user.name", "Test User")
	if err := writeText(repo+"/fixture.txt", "one\n"); err != nil {
		t.Fatal(err)
	}
	runGitDir(t, repo, "add", "fixture.txt")
	runGitDir(t, repo, "commit", "-m", "first")
	return repo
}
