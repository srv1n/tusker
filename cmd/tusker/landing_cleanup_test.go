package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// closeLandedTaskForTest closes a landed task through the same close
// ceremony the daemon's review pass uses.
func closeLandedTaskForTest(t *testing.T, vault, taskID string) {
	t.Helper()
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "_pos1": taskID, "covers": "A1", "check": "true", "result": "pass", "note": "Landing cleanup fixture passed."}, v7TestVerificationMutation)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "id": taskID, "status": "review", "by": "agent:test"}, statusV7Cmd)
	if _, err := closeV7Task(vault, taskID, reviewPassActor, Args{"vault": vault, "quiet": "true", "local": "true", "by": reviewPassActor, "reason": "landed"}, true, ""); err != nil {
		t.Fatalf("close %s: %v", taskID, err)
	}
}

func waveDataForTest(t *testing.T, vault, waveID string) map[string]any {
	t.Helper()
	data, _, err := parseFrontmatterMustRead(filepath.Join(vault, "work", "waves", waveID+".md"))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestWaveAutoCompletesWhenMembersLand(t *testing.T) {
	repo, vault := wmlLandRepo(t, 2, "test -f README.md", string(WorkspaceStrategyWorktree))
	wavePath := filepath.Join(vault, "work", "waves", "W-0001.md")
	data, body, err := parseFrontmatterMustRead(wavePath)
	if err != nil {
		t.Fatal(err)
	}
	data["authorization"], data["authorization_fingerprint"] = "armed", "sha256:fixture"
	data["authorized_by"], data["authorized_at"] = "human:test", "2026-10-19T00:00:00Z"
	text, err := serializeDocument(data, body, v7FrontmatterOrder["wave"])
	if err != nil {
		t.Fatal(err)
	}
	if err := writeText(wavePath, text); err != nil {
		t.Fatal(err)
	}

	commitLandBranch(t, repo, "task/APP-T-0001", "main", map[string]string{"src/a.txt": "a\n"})
	if err := landV7Cmd(Args{"vault": vault, "quiet": "true", "_pos0": "APP-T-0001"}); err != nil {
		t.Fatal(err)
	}
	closeLandedTaskForTest(t, vault, "APP-T-0001")
	assertEqual(t, "open", stringField(waveDataForTest(t, vault, "W-0001"), "status"), "wave with an open member stays open")

	// The last member is cancelled: every member is terminal, so the wave completes.
	if err := discardV7Cmd(Args{"vault": vault, "quiet": "true", "id": "APP-T-0002", "reason": "No longer desired."}); err != nil {
		t.Fatal(err)
	}
	wave := waveDataForTest(t, vault, "W-0001")
	assertEqual(t, "landed", stringField(wave, "status"), "wave completes when every member is terminal")
	if stringField(wave, "landed_at") == "" {
		t.Fatal("completed wave has no landed_at")
	}
	assertEqual(t, "disarmed", stringField(wave, "authorization"), "a completed wave is no longer armed")
	idx, err := loadV7Index(vault)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, armed := armedWaveForTask(vault, idx.Tasks["APP-T-0001"]); armed {
		t.Fatal("armed-wave dispatch still treats the completed wave as armed")
	}
}

func TestLandingDeletesTaskAndReviewBranches(t *testing.T) {
	repo, vault := wmlLandRepo(t, 1, "test -f README.md", string(WorkspaceStrategyWorktree))
	source := commitLandBranch(t, repo, "task/APP-T-0001", "main", map[string]string{"src/a.txt": "a\n"})
	reviewed := v7ReviewBranchName("APP-T-0001", source)
	runGitDir(t, repo, "branch", reviewed, source)
	// A rejected review round whose commit never reached main.
	rejected := commitLandBranch(t, repo, "scratch", "main", map[string]string{"src/x.txt": "x\n"})
	runGitDir(t, repo, "branch", "-D", "scratch")
	unmerged := v7ReviewBranchName("APP-T-0001", rejected)
	runGitDir(t, repo, "branch", unmerged, rejected)

	// A clean Tusker-owned review worktree and a dirty one.
	addOwned := func(name, rev string, dirty bool) string {
		path := filepath.Join(t.TempDir(), name)
		runGitDir(t, repo, "worktree", "add", "--detach", path, rev)
		raw, _ := json.Marshal(WorkspaceMetadata{RecordID: "APP-T-0001", Strategy: string(WorkspaceStrategyWorktree), RepoRoot: repo})
		if err := writeText(filepath.Join(path, ".tusker", "workspace.json"), string(raw)); err != nil {
			t.Fatal(err)
		}
		if dirty {
			if err := writeText(filepath.Join(path, "wip.txt"), "uncommitted\n"); err != nil {
				t.Fatal(err)
			}
		}
		return path
	}
	clean := addOwned("clean", source, false)
	dirty := addOwned("dirty", source, true)

	if err := landV7Cmd(Args{"vault": vault, "quiet": "true", "_pos0": "APP-T-0001"}); err != nil {
		t.Fatal(err)
	}
	closeLandedTaskForTest(t, vault, "APP-T-0001")

	for _, branch := range []string{"task/APP-T-0001", reviewed} {
		if gitBranchExists(repo, branch) {
			t.Fatalf("landed branch %s was not deleted", branch)
		}
	}
	if !gitBranchExists(repo, unmerged) {
		t.Fatal("a review branch not on main was deleted")
	}
	if _, err := os.Stat(clean); !os.IsNotExist(err) {
		t.Fatalf("clean task worktree was not removed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dirty, "wip.txt")); err != nil {
		t.Fatalf("a worktree with uncommitted changes was removed: %v", err)
	}
	if !gitBranchExists(repo, "main") || !fileExists(filepath.Join(repo, "README.md")) {
		t.Fatal("the main checkout was touched")
	}
}

func TestLandingKeepsUnmergedIntegrationBranch(t *testing.T) {
	for _, tc := range []struct {
		name   string
		merged bool
	}{{"unmerged", false}, {"merged", true}} {
		t.Run(tc.name, func(t *testing.T) {
			repo, vault := wmlLandRepo(t, 1, "test -f README.md", string(WorkspaceStrategyWorktree))
			if tc.merged {
				runGitDir(t, repo, "branch", "integration/W-0001", "main")
			} else {
				commitLandBranch(t, repo, "integration/W-0001", "main", map[string]string{"src/staged.txt": "staged\n"})
			}
			commitLandBranch(t, repo, "task/APP-T-0001", "main", map[string]string{"src/a.txt": "a\n"})
			if err := landV7Cmd(Args{"vault": vault, "quiet": "true", "_pos0": "APP-T-0001"}); err != nil {
				t.Fatal(err)
			}
			closeLandedTaskForTest(t, vault, "APP-T-0001")
			assertEqual(t, "landed", stringField(waveDataForTest(t, vault, "W-0001"), "status"), "wave completes")
			if gitBranchExists(repo, "integration/W-0001") == tc.merged {
				t.Fatalf("integration branch exists=%v, want %v", !tc.merged, !tc.merged)
			}
			if !tc.merged {
				events, _ := filepath.Glob(filepath.Join(vault, "events", "*", "*", "W-0001--*.json"))
				found := false
				for _, path := range events {
					if text, err := readText(path); err == nil && strings.Contains(text, "not merged into main") {
						found = true
					}
				}
				if !found {
					t.Fatal("no event records why the integration branch was kept")
				}
			}
		})
	}
}
