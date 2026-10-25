package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestDogfoodGitignoreUpsert(t *testing.T) {
	for _, initial := range []string{"", "# tusker\nconfig.local.yaml\n_system/generated/\n.obsidian/workspace*\n.obsidian/cache\n.trash/\n", "# user rules\ncustom/\n!work/keep.md\n# tusker\nconfig.local.yaml"} {
		t.Run(initial, func(t *testing.T) {
			vault := t.TempDir()
			path := filepath.Join(vault, ".gitignore")
			if initial != "" {
				if err := os.WriteFile(path, []byte(initial), 0644); err != nil {
					t.Fatal(err)
				}
			}
			if err := upsertGitignore(vault); err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(string(got), initial) {
				t.Fatalf("user content changed: %q", got)
			}
			for _, line := range []string{"# tusker", "config.local.yaml", "_system/generated/", ".obsidian/workspace*", ".obsidian/cache", ".trash/", "events/**/*.json", "attempts/**/*.json", "attempts/**/*.jsonl", "attempts/**/*.log", "_generated/", "scratch/", "workspaces/", "_runtime/", "_system/locks/", "evidence/**/artifacts/"} {
				if !strings.Contains("\n"+string(got), "\n"+line+"\n") {
					t.Errorf("missing %q", line)
				}
			}
			if strings.Contains(string(got), "dashboards/") || strings.Contains(string(got), "\nwork/") {
				t.Fatal("durable records ignored")
			}
			before, _ := os.Stat(path)
			if err := upsertGitignore(vault); err != nil {
				t.Fatal(err)
			}
			again, _ := os.ReadFile(path)
			after, _ := os.Stat(path)
			if string(again) != string(got) || !before.ModTime().Equal(after.ModTime()) {
				t.Fatal("second run changed file")
			}
		})
	}
}

func TestDogfoodGitignoreCheckFix(t *testing.T) {
	repo := t.TempDir()
	vault := filepath.Join(repo, ".tusker")
	if err := os.MkdirAll(filepath.Join(vault, "work"), 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(vault, ".gitignore")
	if err := os.WriteFile(path, []byte("# tusker\nconfig.local.yaml\n"), 0644); err != nil {
		t.Fatal(err)
	}
	steps, err := checkPlan(Args{"only": "setup", "fix": "true"})
	if err != nil {
		t.Fatal(err)
	}
	var output string
	for _, step := range steps {
		if step.Name == "setup repair" {
			output = captureStdout(t, func() {
				_, err = step.Run(Args{"repo": repo, "state-root": filepath.Join(repo, "state"), "json": "true", "source": filepath.Join(repo, "missing")})
			})
		}
	}
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(missingTuskerGitignoreLines(string(got))) != 0 {
		t.Fatalf("upgrade not applied: %s", got)
	}
	if !strings.Contains(output, `"code":"vault_gitignore"`) || !strings.Contains(output, `"changed":true`) {
		t.Fatalf("repair not reported: %s", output)
	}
}

func TestDogfoodGitignoreTracked(t *testing.T) {
	repo := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
		return string(out)
	}
	git("init")
	vault := filepath.Join(repo, ".tusker")
	for _, rel := range []string{"_generated/indexes/a.json", "events/a.json", "attempts/a.log", "attempts/note.md", "work/task.md", "dashboards/a.md"} {
		path := filepath.Join(vault, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("test"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	git("add", ".tusker")
	before := git("ls-files")
	report, err := runSetupDoctor(setupDoctorInput{RepoRoot: repo, Source: filepath.Join(repo, "missing")}, false)
	if err != nil {
		t.Fatal(err)
	}
	found, action := false, ""
	for _, f := range report.Findings {
		if f.Code == "vault_gitignore" && !f.Repairable {
			t.Fatal("missing lines are not fixable")
		}
		if f.Code == "vault_generated_tracked" {
			found = true
			if !strings.Contains(f.Message, "3 generated") || !strings.HasPrefix(f.Action, "git rm -r --cached --quiet --ignore-unmatch -- ") {
				t.Fatalf("unexpected finding: %+v", f)
			}
			action = f.Action
		}
	}
	if !found {
		t.Fatal("tracked files not reported")
	}
	if git("ls-files") != before {
		t.Fatal("check changed index")
	}
	if _, err := os.Stat(filepath.Join(vault, ".gitignore")); !os.IsNotExist(err) {
		t.Fatalf("plain check wrote gitignore: %v", err)
	}
	// The suggested command untracks exactly the generated files.
	cmd := exec.Command("sh", "-c", action)
	cmd.Dir = repo
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s: %v: %s", action, err, out)
	}
	if left := strings.Fields(git("ls-files")); strings.Join(left, " ") != ".tusker/attempts/note.md .tusker/dashboards/a.md .tusker/work/task.md" {
		t.Fatalf("remedy untracked the wrong files: %v", left)
	}
}
