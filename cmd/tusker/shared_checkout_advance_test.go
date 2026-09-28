package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAdvanceSharedCheckoutInPlace(t *testing.T) {
	type fixture struct{ dir, old, new string }
	run := func(t *testing.T, dir string, args ...string) string {
		t.Helper()
		out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	write := func(t *testing.T, dir, rel, body string, mode os.FileMode) {
		t.Helper()
		path := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), mode); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, mode); err != nil {
			t.Fatal(err)
		}
	}
	// setup builds main=O, then commits a submission N from a private index
	// the way shared-checkout submissions do, leaving the landed files dirty
	// on disk plus unrelated staged, unstaged and untracked work.
	setup := func(t *testing.T) fixture {
		dir := t.TempDir()
		run(t, dir, "init", "-q", "-b", "main")
		run(t, dir, "config", "user.email", "t@example.com")
		run(t, dir, "config", "user.name", "t")
		write(t, dir, "edit.txt", "old\n", 0o644)
		write(t, dir, "gone.txt", "bye\n", 0o644)
		write(t, dir, "moved/from.txt", "rename me\n", 0o644)
		write(t, dir, "tool.sh", "echo\n", 0o644)
		write(t, dir, "staged.txt", "s0\n", 0o644)
		write(t, dir, "unstaged.txt", "u0\n", 0o644)
		write(t, dir, ".tusker/task.md", "status: todo\n", 0o644)
		run(t, dir, "add", "-A")
		run(t, dir, "commit", "-q", "-m", "O")
		old := run(t, dir, "rev-parse", "HEAD")

		// Submitted work on disk.
		write(t, dir, "edit.txt", "new\n", 0o644)
		write(t, dir, "added.txt", "fresh\n", 0o644)
		write(t, dir, "moved/to.txt", "rename me\n", 0o644)
		write(t, dir, "tool.sh", "echo\n", 0o755)
		write(t, dir, ".tusker/task.md", "status: review\n", 0o644)
		os.Remove(filepath.Join(dir, "gone.txt"))
		os.Remove(filepath.Join(dir, "moved/from.txt"))
		private := filepath.Join(t.TempDir(), "index")
		env := append(os.Environ(), "GIT_INDEX_FILE="+private)
		for _, args := range [][]string{
			{"read-tree", old},
			{"add", "edit.txt", "added.txt", "moved/to.txt", "tool.sh", ".tusker/task.md"},
			{"rm", "-q", "--cached", "gone.txt", "moved/from.txt"},
		} {
			cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
			cmd.Env = env
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("git %v: %v\n%s", args, err, out)
			}
		}
		cmd := exec.Command("git", "-C", dir, "write-tree")
		cmd.Env = env
		tree, err := cmd.Output()
		if err != nil {
			t.Fatal(err)
		}
		next := run(t, dir, "commit-tree", strings.TrimSpace(string(tree)), "-p", old, "-m", "N")

		// Unrelated work from other agents.
		write(t, dir, "staged.txt", "s1\n", 0o644)
		run(t, dir, "add", "staged.txt")
		write(t, dir, "unstaged.txt", "u1\n", 0o644)
		write(t, dir, "untracked.txt", "mine\n", 0o644)
		// Tusker's live record has moved on past the landed commit.
		write(t, dir, ".tusker/task.md", "status: done\n", 0o644)
		return fixture{dir, old, next}
	}
	type snapshot struct {
		body  string
		mtime time.Time
	}
	snap := func(t *testing.T, dir string, rels ...string) map[string]snapshot {
		out := map[string]snapshot{}
		for _, rel := range rels {
			path := filepath.Join(dir, rel)
			info, err := os.Stat(path)
			if err != nil {
				out[rel] = snapshot{}
				continue
			}
			body, _ := os.ReadFile(path)
			out[rel] = snapshot{string(body), info.ModTime()}
		}
		return out
	}
	all := []string{"edit.txt", "added.txt", "moved/to.txt", "moved/from.txt", "gone.txt", "tool.sh", "staged.txt", "unstaged.txt", "untracked.txt", ".tusker/task.md"}

	cases := []struct {
		name    string
		mutate  func(t *testing.T, f fixture)
		wantErr string
	}{
		{name: "lands", mutate: func(*testing.T, fixture) {}},
		{name: "edited after submit", mutate: func(t *testing.T, f fixture) {
			write(t, f.dir, "edit.txt", "newer\n", 0o644)
		}, wantErr: "edit.txt"},
		{name: "third staged version", mutate: func(t *testing.T, f fixture) {
			write(t, f.dir, "edit.txt", "third\n", 0o644)
			run(t, f.dir, "add", "edit.txt")
			write(t, f.dir, "edit.txt", "new\n", 0o644)
		}, wantErr: "neither the old nor the landed commit: edit.txt"},
		{name: "main moved", mutate: func(t *testing.T, f fixture) {
			run(t, f.dir, "commit", "-q", "--allow-empty", "-m", "race")
		}, wantErr: "main moved"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := setup(t)
			tc.mutate(t, f)
			headBefore := run(t, f.dir, "rev-parse", "main")
			indexBefore := run(t, f.dir, "ls-files", "-s")
			before := snap(t, f.dir, all...)
			err := advanceSharedCheckoutInPlace(f.dir, "main", f.new, f.old)
			after := snap(t, f.dir, all...)
			for _, rel := range all {
				if before[rel] != after[rel] {
					t.Fatalf("working tree file %s changed: %+v -> %+v", rel, before[rel], after[rel])
				}
			}
			if _, statErr := os.Stat(filepath.Join(f.dir, ".git", "index.lock")); !os.IsNotExist(statErr) {
				t.Fatalf("index.lock left behind: %v", statErr)
			}
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("want refusal containing %q, got %v", tc.wantErr, err)
				}
				if got := run(t, f.dir, "rev-parse", "main"); got != headBefore {
					t.Fatalf("ref moved on refusal")
				}
				if got := run(t, f.dir, "ls-files", "-s"); got != indexBefore {
					t.Fatalf("index changed on refusal:\n%s\n---\n%s", indexBefore, got)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got := run(t, f.dir, "rev-parse", "main"); got != f.new {
				t.Fatalf("main = %s, want %s", got, f.new)
			}
			status := run(t, f.dir, "status", "--porcelain=v1", "--untracked-files=all")
			want := strings.TrimSpace(" M .tusker/task.md\nM  staged.txt\n M unstaged.txt\n?? untracked.txt") // run() trims output
			if status != want {
				t.Fatalf("status after landing:\n%s\nwant:\n%s", status, want)
			}
		})
	}
}
