package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type sharedCheckoutFixture struct{ dir, old, new string }

func sharedCheckoutGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func sharedCheckoutWrite(t *testing.T, dir, rel, body string, mode os.FileMode) {
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

// Status after a landing: only unrelated work and Tusker's live record.
const sharedCheckoutLandedStatus = " M .tusker/task.md\nM  staged.txt\n M unstaged.txt\n?? untracked.txt"

// setupSharedCheckout builds main=O, then commits a submission N from a
// private index the way shared-checkout submissions do, leaving the landed
// files dirty on disk plus unrelated staged, unstaged and untracked work. N
// also swaps a file for a directory (config) and a directory for a file
// (data).
func setupSharedCheckout(t *testing.T) sharedCheckoutFixture {
	run, write := sharedCheckoutGit, sharedCheckoutWrite
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
	write(t, dir, "config", "flat\n", 0o644)
	write(t, dir, "data/x.txt", "x\n", 0o644)
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
	os.Remove(filepath.Join(dir, "config"))
	write(t, dir, "config/settings.json", "{}\n", 0o644)
	os.RemoveAll(filepath.Join(dir, "data"))
	write(t, dir, "data", "now a file\n", 0o644)
	private := filepath.Join(t.TempDir(), "index")
	env := append(os.Environ(), "GIT_INDEX_FILE="+private)
	for _, args := range [][]string{
		{"read-tree", old},
		{"rm", "-q", "--cached", "gone.txt", "moved/from.txt", "config", "data/x.txt"},
		{"add", "edit.txt", "added.txt", "moved/to.txt", "tool.sh", ".tusker/task.md", "config/settings.json", "data"},
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
	return sharedCheckoutFixture{dir, old, next}
}

func TestAdvanceSharedCheckoutInPlace(t *testing.T) {
	type fixture = sharedCheckoutFixture
	run, write, setup := sharedCheckoutGit, sharedCheckoutWrite, setupSharedCheckout
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
	all := []string{"edit.txt", "added.txt", "moved/to.txt", "moved/from.txt", "gone.txt", "tool.sh", "staged.txt", "unstaged.txt", "untracked.txt", ".tusker/task.md", "config", "config/settings.json", "data", "data/x.txt"}

	cases := []struct {
		name    string
		mutate  func(t *testing.T, f fixture)
		writes  bool // the landing writes N's files over an untouched O checkout
		wantErr string
	}{
		{name: "lands", mutate: func(*testing.T, fixture) {}},
		{name: "clean checkout lands and writes landed files", writes: true, mutate: func(t *testing.T, f fixture) {
			// Put every landed path back to O on disk, as if nothing was
			// submitted here; Tusker's live record stays ahead.
			for _, rel := range []string{"added.txt", "moved/to.txt", "config", "data"} {
				if err := os.RemoveAll(filepath.Join(f.dir, rel)); err != nil {
					t.Fatal(err)
				}
			}
			run(t, f.dir, "checkout", f.old, "--", "edit.txt", "gone.txt", "moved/from.txt", "tool.sh", "config", "data/x.txt")
		}},
		{name: "differs from old and new", mutate: func(t *testing.T, f fixture) {
			write(t, f.dir, "edit.txt", "newer\n", 0o644)
		}, wantErr: "match neither the old nor the landed commit (edited after submit?): edit.txt"},
		{name: "third staged version", mutate: func(t *testing.T, f fixture) {
			write(t, f.dir, "edit.txt", "third\n", 0o644)
			run(t, f.dir, "add", "edit.txt")
			write(t, f.dir, "edit.txt", "new\n", 0o644)
		}, wantErr: "neither the old nor the landed commit: edit.txt"},
		{name: "bookkeeping third staged version", mutate: func(t *testing.T, f fixture) {
			write(t, f.dir, ".tusker/task.md", "status: bogus\n", 0o644)
			run(t, f.dir, "add", ".tusker/task.md")
			write(t, f.dir, ".tusker/task.md", "status: done\n", 0o644)
		}, wantErr: "neither the old nor the landed commit: .tusker/task.md"},
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
				if !tc.writes && before[rel] != after[rel] {
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
			want := strings.TrimSpace(sharedCheckoutLandedStatus) // run() trims output
			if tc.writes {
				for _, rel := range []string{"staged.txt", "unstaged.txt", "untracked.txt", ".tusker/task.md"} {
					if before[rel] != after[rel] {
						t.Fatalf("unrelated file %s changed", rel)
					}
				}
			}
			if status != want {
				t.Fatalf("status after landing:\n%s\nwant:\n%s", status, want)
			}
		})
	}
}

func TestAdvanceSharedCheckoutRepairAfterFailedIndexInstall(t *testing.T) {
	f := setupSharedCheckout(t)
	lock := filepath.Join(f.dir, ".git", "index.lock")
	sharedCheckoutRename = func(string, string) error { return os.ErrPermission }
	err := advanceSharedCheckoutInPlace(f.dir, "main", f.new, f.old)
	sharedCheckoutRename = os.Rename
	if err == nil || !strings.Contains(err.Error(), lock) {
		t.Fatalf("want error naming %s, got %v", lock, err)
	}
	if got := sharedCheckoutGit(t, f.dir, "rev-parse", "main"); got != f.new {
		t.Fatalf("main = %s, want %s", got, f.new)
	}
	if _, statErr := os.Stat(lock); statErr != nil {
		t.Fatalf("prepared index was deleted after the ref moved: %v", statErr)
	}
	// With the stale lock still there, repair reports it and leaves it.
	if err := repairSharedCheckoutIndexAfterAdvance(f.dir, f.old, f.new); err == nil || !strings.Contains(err.Error(), lock+" exists") {
		t.Fatalf("want repair to report the stale lock, got %v", err)
	}
	if _, statErr := os.Stat(lock); statErr != nil {
		t.Fatalf("repair removed the stale lock: %v", statErr)
	}
	if err := os.Remove(lock); err != nil { // the operator clears it
		t.Fatal(err)
	}
	if err := repairSharedCheckoutIndexAfterAdvance(f.dir, f.old, f.new); err != nil {
		t.Fatal(err)
	}
	status := sharedCheckoutGit(t, f.dir, "status", "--porcelain=v1", "--untracked-files=all")
	if want := strings.TrimSpace(sharedCheckoutLandedStatus); status != want {
		t.Fatalf("status after repair:\n%s\nwant:\n%s", status, want)
	}
}
