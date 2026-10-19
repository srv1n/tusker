package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDemoSeedCommitsTrackerRecords(t *testing.T) {
	bin := demoTestBinary(t)
	demoTestEnv(t, bin)
	repo := t.TempDir()
	if code := demoRunInner(t, "demo seed", Args{"repo": repo, "scenario": demoScenario}); code != demoExitOK {
		t.Fatalf("seed exit %d", code)
	}
	manifest := demoManifestForTest(t, repo)
	tracked, err := demoGit(repo, "ls-files", ".tusker/work")
	if err != nil {
		t.Fatal(err)
	}
	for _, rec := range manifest.Tasks {
		if !strings.Contains(tracked, ".tusker/work/tasks/"+rec.TaskID+".md") {
			t.Fatalf("task record %s not committed:\n%s", rec.TaskID, tracked)
		}
	}
	for _, wave := range manifest.Waves {
		for _, want := range []string{".tusker/work/waves/" + wave.WaveID + ".md", ".tusker/work/epics/" + wave.Epic + ".md"} {
			if !strings.Contains(tracked, want) {
				t.Fatalf("%s not committed:\n%s", want, tracked)
			}
		}
		head, _ := demoGit(repo, "rev-parse", "HEAD")
		branch, err := demoGit(repo, "rev-parse", "integration/"+wave.WaveID)
		if err != nil || branch != head {
			t.Fatalf("integration/%s should point at the records commit: %q vs %q (%v)", wave.WaveID, branch, head, err)
		}
	}
	// Task work dirs are ignored only in the main checkout (offline lane);
	// the committed .gitignore must not hide them from agent worktrees.
	if raw, err := os.ReadFile(filepath.Join(repo, ".gitignore")); err == nil && strings.Contains(string(raw), "sample/alpha/") {
		t.Fatalf("committed .gitignore hides task-owned dirs:\n%s", raw)
	}
	if _, err := demoGit(repo, "check-ignore", "-q", "sample/alpha/part-1.txt"); err != nil {
		t.Fatalf("main checkout should ignore offline-lane artifacts: %v", err)
	}
	wt := filepath.Join(t.TempDir(), "wt")
	if _, err := demoGit(repo, "worktree", "add", "-q", "--detach", wt); err != nil {
		t.Fatal(err)
	}
	if out, err := demoGit(wt, "check-ignore", "-v", "sample/alpha/part-1.txt"); err == nil {
		t.Fatalf("agent worktree must be able to commit task-owned files: %s", out)
	}
}

func TestDocsOnlyTaskAcceptsCommandProof(t *testing.T) {
	body := "## Verification\n| Covers | Check | Result | Notes |\n| --- | --- | --- | --- |\n" +
		"| A1 | command: ls docs/system/a.md | pass | |\n"
	task := func(owned ...string) Note {
		return Note{Data: map[string]any{"id": "T-1", "proof_required": []any{"focused_test"}, "owned_paths": toAnySlice(owned)}, Body: body}
	}
	docs := task("docs/system/a.md", "docs/system/b.md")
	for _, required := range v7TaskProofRequired(docs) {
		if !v7ProofRequiredClassSatisfied("T-1", required, docs, v7Index{}) {
			t.Fatalf("docs-only task: %s not satisfied by passing command check", required)
		}
	}
	code := task("docs/system/a.md", "cmd/tusker/main.go")
	for _, required := range v7TaskProofRequired(code) {
		if v7ProofRequiredClassSatisfied("T-1", required, code, v7Index{}) {
			t.Fatalf("code task: %s must still require a test command", required)
		}
	}
}

func toAnySlice(in []string) []any {
	out := make([]any, len(in))
	for i, s := range in {
		out[i] = s
	}
	return out
}
