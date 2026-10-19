package main

import (
	"os"
	"path/filepath"
	"testing"
)

// countMaterialHashes counts full workspace material hashes until cleanup.
func countMaterialHashes(t *testing.T) *int {
	t.Helper()
	n := 0
	workspaceMaterialHashObserver = func(string) { n++ }
	t.Cleanup(func() { workspaceMaterialHashObserver = nil })
	return &n
}

// submittedWorkspace commits files into a fresh repository and returns it,
// its HEAD and the material fingerprint execute would have submitted.
func submittedWorkspace(t *testing.T, files map[string]string, scope, generated []string) (string, string, string) {
	t.Helper()
	dir := t.TempDir()
	runGitDir(t, dir, "init", "-q")
	runGitDir(t, dir, "config", "user.email", "t@example.com")
	runGitDir(t, dir, "config", "user.name", "t")
	for rel, body := range files {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, rel)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := writeText(filepath.Join(dir, rel), body); err != nil {
			t.Fatal(err)
		}
	}
	runGitDir(t, dir, "add", "-A")
	runGitDir(t, dir, "commit", "-q", "-m", "submission")
	sha, err := gitOutputTrim(dir, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	material, err := workspaceTreeStateHashForPaths(dir, scope, generated)
	if err != nil {
		t.Fatal(err)
	}
	return dir, sha, material
}

func TestProofMaterialUsesGitCommitAndCleanScope(t *testing.T) {
	vault, store, project := authorityFixture(t)
	scope := []string{"submitted.txt"}
	ws, sha, material := submittedWorkspace(t, map[string]string{"submitted.txt": "v1\n"}, scope, nil)
	writeDirectTask(t, vault, "APP-T-0001", "W-0001", map[string]any{"source_sha": sha, "work_revision": 7, "owned_paths": scope})
	if err := store.SaveAttempt(RunAttempt{
		AttemptID: "execute-submitted", ProjectID: project.ProjectID, RecordID: "APP-T-0001", ItemID: "APP-T-0001",
		Lane: runLaneExecute, WorkRevision: 7, WorkspacePath: ws, Outcome: string(AttemptOutcomeSucceeded),
		EndState: RunEndState{Schema: "tusker.run-end-state/v2", HeadSHA: sha, WorktreePath: ws, MaterialFingerprint: material, MaterialScope: scope},
	}); err != nil {
		t.Fatal(err)
	}
	run := RunStatus{ProjectID: project.ProjectID, RecordID: "APP-T-0001", ItemID: "APP-T-0001", WorkRevision: 7}
	hashes := countMaterialHashes(t)
	workspace, err := recoveryCommandVerificationWorkspace(store, vault, walkthroughNote(t, vault, "APP-T-0001"), run)
	if err != nil {
		t.Fatal(err)
	}
	if *hashes != 1 || workspace.MaterialFingerprint != material {
		t.Fatalf("build hashed %d times, material %q; want once, %q", *hashes, workspace.MaterialFingerprint, material)
	}
	fresh := func(why string) {
		t.Helper()
		*hashes = 0
		if err := workspace.Verify(); err != nil {
			t.Fatalf("%s: proof went stale: %v", why, err)
		}
		if *hashes != 0 {
			t.Fatalf("%s: git could decide but the workspace was hashed %d times", why, *hashes)
		}
	}
	stale := func(why string) {
		t.Helper()
		if err := workspace.Verify(); err == nil {
			t.Fatalf("%s: proof still counted for changed code", why)
		}
	}
	fresh("unchanged submission")

	if err := writeText(filepath.Join(ws, "submitted.txt"), "edited\n"); err != nil {
		t.Fatal(err)
	}
	stale("uncommitted edit in scope")
	runGitDir(t, ws, "checkout", "--", "submitted.txt")
	fresh("edit reverted")

	if err := writeText(filepath.Join(ws, "unrelated.txt"), "x\n"); err != nil {
		t.Fatal(err)
	}
	runGitDir(t, ws, "add", "unrelated.txt")
	runGitDir(t, ws, "commit", "-q", "-m", "outside scope")
	fresh("later commit outside scope")

	if err := writeText(filepath.Join(ws, "submitted.txt"), "v2\n"); err != nil {
		t.Fatal(err)
	}
	runGitDir(t, ws, "commit", "-q", "-am", "inside scope")
	stale("later commit in scope")

	// Generated outputs git ignores keep a content check.
	genScope := []string{"out", "src.txt"}
	gen, genSHA, genMaterial := submittedWorkspace(t, map[string]string{".gitignore": "out/\n", "src.txt": "s\n", "out/bin": "built\n"}, genScope, []string{"out"})
	writeDirectTask(t, vault, "APP-T-0002", "W-0001", map[string]any{"source_sha": genSHA, "work_revision": 1, "owned_paths": []string{"src.txt"}, "generated_outputs": []string{"out"}})
	if err := store.SaveAttempt(RunAttempt{
		AttemptID: "execute-generated", ProjectID: project.ProjectID, RecordID: "APP-T-0002", ItemID: "APP-T-0002",
		Lane: runLaneExecute, WorkRevision: 1, WorkspacePath: gen, Outcome: string(AttemptOutcomeSucceeded),
		EndState: RunEndState{Schema: "tusker.run-end-state/v2", HeadSHA: genSHA, WorktreePath: gen, MaterialFingerprint: genMaterial, MaterialScope: genScope, GeneratedOutputScope: []string{"out"}},
	}); err != nil {
		t.Fatal(err)
	}
	workspace, err = recoveryCommandVerificationWorkspace(store, vault, walkthroughNote(t, vault, "APP-T-0002"), RunStatus{ProjectID: project.ProjectID, RecordID: "APP-T-0002", WorkRevision: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := workspace.Verify(); err != nil {
		t.Fatalf("unchanged generated output went stale: %v", err)
	}
	if err := writeText(filepath.Join(gen, "out", "bin"), "rebuilt differently\n"); err != nil {
		t.Fatal(err)
	}
	stale("ignored generated output changed")
}

func TestReviewWorkspaceHashesAtMostOnce(t *testing.T) {
	vault, store, project := authorityFixture(t)
	scope := []string{"submitted.txt"}
	ws, sha, material := submittedWorkspace(t, map[string]string{"submitted.txt": "v1\n"}, scope, nil)
	writeDirectTask(t, vault, "APP-T-0001", "W-0001", map[string]any{"source_sha": sha, "work_revision": 7, "owned_paths": scope, "status": "review"})
	for _, attempt := range []RunAttempt{
		{AttemptID: "EXEC-1", ProjectID: project.ProjectID, RecordID: "APP-T-0001", ItemID: "APP-T-0001", Runner: "codex",
			Lane: runLaneExecute, WorkRevision: 7, WorkspacePath: ws, Outcome: string(AttemptOutcomeSucceeded),
			EndState: RunEndState{Schema: "tusker.run-end-state/v2", HeadSHA: sha, WorktreePath: ws, MaterialFingerprint: material, MaterialScope: scope}},
		{AttemptID: "REV-1", ProjectID: project.ProjectID, RecordID: "APP-T-0001", ItemID: "APP-T-0001", Runner: "codex",
			Lane: runLaneReview, WorkRevision: 7, WorkspacePath: ws, ParentAttemptID: "EXEC-1"},
	} {
		if err := store.SaveAttempt(attempt); err != nil {
			t.Fatal(err)
		}
	}
	run := RunStatus{ProjectID: project.ProjectID, RecordID: "APP-T-0001", ItemID: "APP-T-0001", Runner: "codex", Lane: runLaneReview,
		WorkRevision: 7, ActiveAttemptID: "REV-1", WorkspacePath: ws, LeaseState: string(LeaseStateClaimed), LeaseOwner: "reviewer:test", LeaseGeneration: 1}
	if err := store.UpsertRun(run); err != nil {
		t.Fatal(err)
	}
	note := walkthroughNote(t, vault, "APP-T-0001")
	hashes := countMaterialHashes(t)

	// No close event: the review task's workspace is never read.
	if closed, err := externalLoopCloseTaskRecordedForCurrentRun(store, project.ProjectID, "APP-T-0001", note, run); err != nil || closed {
		t.Fatalf("close recorded=%v err=%v", closed, err)
	}
	if *hashes != 0 {
		t.Fatalf("review task without a close event hashed its workspace %d times", *hashes)
	}

	workspace, err := reviewCommandVerificationWorkspace(store, vault, note, run)
	if err != nil {
		t.Fatal(err)
	}
	if *hashes > 1 {
		t.Fatalf("building the review workspace hashed it %d times", *hashes)
	}
	*hashes = 0
	if err := workspace.Verify(); err != nil || *hashes != 0 {
		t.Fatalf("verify err=%v hashes=%d; want git to decide without hashing", err, *hashes)
	}
}
