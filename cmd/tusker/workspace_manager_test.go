package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkspaceManagerRejectsMismatchedExistingMetadata(t *testing.T) {
	stateRoot := t.TempDir()
	manager := NewWorkspaceManager()
	req := WorkspacePrepareRequest{
		ProjectID: "project-1", ProjectKey: "MEM", RecordID: "record-1", ItemID: "MEM-T-0001",
		RepoRoot: t.TempDir(), StateRoot: stateRoot, Strategy: WorkspaceStrategyCopy, WorkRevision: 0,
	}
	if _, err := manager.Prepare(req); err != nil {
		t.Fatal(err)
	}
	metadataPath := filepath.Join(stateRoot, "workspaces", "MEM", "record-1", ".tusker", "workspace.json")
	if err := writeText(metadataPath, `{"project_id":"project-1","record_id":"other-record","created_at":"2026-04-28T00:00:00Z"}`+"\n"); err != nil {
		t.Fatal(err)
	}
	_, err := manager.Prepare(req)
	if err == nil || !strings.Contains(err.Error(), "record_id does not match") {
		t.Fatalf("expected metadata mismatch error, got %v", err)
	}
}

func TestWorkspaceManagerReplacesStaleProjectWorkspace(t *testing.T) {
	manager := NewWorkspaceManager()
	req := WorkspacePrepareRequest{
		ProjectID: "project-a", ProjectKey: "MEM", RecordID: "record-1", ItemID: "MEM-T-0001",
		RepoRoot: t.TempDir(), StateRoot: t.TempDir(), Strategy: WorkspaceStrategyCopy,
	}
	first, err := manager.Prepare(req)
	if err != nil {
		t.Fatal(err)
	}
	req.ProjectID = "project-b"
	second, err := manager.Prepare(req)
	if err != nil {
		t.Fatal(err)
	}
	if second.Path != first.Path || !second.NewlyMaterialized {
		t.Fatalf("stale workspace was not replaced: first=%q second=%+v", first.Path, second)
	}
	raw, err := readText(filepath.Join(second.Path, ".tusker", "workspace.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(raw, `"project_id": "project-b"`) {
		t.Fatalf("replacement metadata has wrong project: %s", raw)
	}
}

func TestAssertWorkspaceWithinRootRejectsEscapes(t *testing.T) {
	root := filepath.Join(t.TempDir(), "workspaces", "MEM")
	err := assertWorkspaceWithinRoot(filepath.Join(root, "..", "OTHER", "record-1"), root)
	if err == nil || !strings.Contains(err.Error(), "escapes workspace root") {
		t.Fatalf("expected workspace root escape error, got %v", err)
	}
}

func TestWorkspaceRootRejectsSharedRuntimeEscapes(t *testing.T) {
	stateRoot := filepath.Join(t.TempDir(), "state")
	for _, configured := range []string{"../repo-worktrees", filepath.Join(t.TempDir(), "absolute-worktrees"), "workspaces-old"} {
		_, _, err := workspacePathForRequest(WorkspacePrepareRequest{
			ProjectKey: "APP", RecordID: "APP-T-0001", RepoRoot: t.TempDir(), StateRoot: stateRoot,
			WorkspaceRoot: configured, Strategy: WorkspaceStrategyWorktree,
		})
		if err == nil || !strings.Contains(err.Error(), "shared runtime workspace directory") {
			t.Fatalf("workspace root %q should be rejected: %v", configured, err)
		}
	}
	path, root, err := workspacePathForRequest(WorkspacePrepareRequest{
		ProjectKey: "APP", RecordID: "APP-T-0001", RepoRoot: t.TempDir(), StateRoot: stateRoot,
		WorkspaceRoot: filepath.Join("workspaces", "team"), Strategy: WorkspaceStrategyWorktree,
	})
	if err != nil {
		t.Fatal(err)
	}
	wantRoot := filepath.Join(stateRoot, "workspaces", "team", "APP")
	if root != wantRoot || path != filepath.Join(wantRoot, "APP-T-0001") {
		t.Fatalf("workspace path/root = %q / %q, want %q", path, root, wantRoot)
	}
}

func TestSharedCheckoutWorkspacePrepareAcceptsOwnedAndSubmittedDirt(t *testing.T) {
	vault, project := workSessionFixture(t, 2)
	manager := NewWorkspaceManager()
	req := WorkspacePrepareRequest{ProjectID: project.ProjectID, ProjectKey: project.ProjectKey, RecordID: "APP-T-0001", ItemID: "APP-T-0001", RepoRoot: project.RepoRoot, StateRoot: DefaultStateRoot(), Strategy: WorkspaceStrategyShared}
	if _, err := manager.Prepare(req); err != nil {
		t.Fatal(err)
	}
	if err := startWorkSessionTest(t, vault, req.RecordID, "agent:one"); err != nil {
		t.Fatal(err)
	}
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	run, err := store.FindRunScoped(project.ProjectID, req.RecordID)
	if err != nil || run == nil {
		t.Fatalf("active run unavailable: %v", err)
	}
	run.WorkspacePath = project.RepoRoot
	run.Lane = runLaneExecute
	run.LeaseState = string(LeaseStateRunning)
	if err := store.UpsertRun(*run); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(project.RepoRoot, "owned", req.RecordID, "own.txt")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeText(path, "owned"); err != nil {
		t.Fatal(err)
	}
	req.RecordID, req.ItemID = "APP-T-0002", "APP-T-0002"
	if _, err := manager.Prepare(req); err != nil {
		t.Fatalf("disjoint second run refused owned dirt: %v", err)
	}
	// Untracked files never block; a modified tracked file nobody owns does.
	unknown := filepath.Join(project.RepoRoot, "unknown.txt")
	if err := writeText(unknown, "unknown"); err != nil {
		t.Fatal(err)
	}
	runGitDir(t, project.RepoRoot, "add", "unknown.txt")
	runGitDir(t, project.RepoRoot, "commit", "-q", "-m", "track unknown")
	if err := writeText(unknown, "changed"); err != nil {
		t.Fatal(err)
	}
	req.RecordID, req.ItemID = "APP-T-0003", "APP-T-0003"
	if _, err := manager.Prepare(req); err == nil || !strings.Contains(err.Error(), "unknown.txt") {
		t.Fatalf("unknown dirt was accepted: %v", err)
	}
	runGitDir(t, project.RepoRoot, "checkout", "--", "unknown.txt")
	source, err := materializeWorkerSubmissionCommit(*run, []string{"owned/APP-T-0001/own.txt"})
	if err != nil {
		t.Fatal(err)
	}
	setAutomationV7TaskFields(t, vault, run.ItemID, map[string]any{"status": "review", "source_sha": source})
	run.LeaseState = string(LeaseStateReleased)
	if err := store.UpsertRun(*run); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Prepare(req); err != nil {
		t.Fatalf("submitted identical dirt was refused: %v", err)
	}
}

func TestWorkspaceRootRejectsSymlinkEscapeWithMissingTail(t *testing.T) {
	stateRoot := t.TempDir()
	sharedRoot := filepath.Join(stateRoot, "workspaces")
	if err := os.MkdirAll(sharedRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(sharedRoot, "linked")); err != nil {
		t.Fatal(err)
	}
	_, _, err := workspacePathForRequest(WorkspacePrepareRequest{
		ProjectKey: "APP", RecordID: "APP-T-0001", RepoRoot: t.TempDir(), StateRoot: stateRoot,
		WorkspaceRoot: filepath.Join("workspaces", "linked", "not-created-yet"), Strategy: WorkspaceStrategyWorktree,
	})
	if err == nil || !strings.Contains(err.Error(), "shared runtime workspace directory") {
		t.Fatalf("expected missing-tail workspace symlink escape rejection, got %v", err)
	}
}

func TestWorkspaceManagerSeparatesBranchLineageForSameRecord(t *testing.T) {
	stateRoot := t.TempDir()
	manager := NewWorkspaceManager()
	baseReq := WorkspacePrepareRequest{
		ProjectID: "project-1", ProjectKey: "MEM", RecordID: "record-1", ItemID: "MEM-T-0001",
		RepoRoot: t.TempDir(), StateRoot: stateRoot, Strategy: WorkspaceStrategyCopy, WorkRevision: 0,
	}
	base, err := manager.Prepare(baseReq)
	if err != nil {
		t.Fatal(err)
	}
	branchReq := baseReq
	branchReq.BranchName = "try/parser rewrite"
	branch, err := manager.Prepare(branchReq)
	if err != nil {
		t.Fatal(err)
	}
	if base.Path == branch.Path {
		t.Fatalf("expected branch workspace to use a separate path, got %s", base.Path)
	}
	if !strings.Contains(branch.Path, "record-1__try-parser-rewrite") {
		t.Fatalf("expected sanitized branch workspace key, got %s", branch.Path)
	}
	assertEqual(t, "try/parser rewrite", branch.Metadata.BranchName, "branch metadata")
}

func TestWorkspacePrepareReportsOnlyFirstMaterialization(t *testing.T) {
	manager := NewWorkspaceManager()
	req := WorkspacePrepareRequest{
		ProjectID: "project-1", ProjectKey: "MEM", RecordID: "record-1", ItemID: "MEM-T-0001",
		RepoRoot: t.TempDir(), StateRoot: t.TempDir(), Strategy: WorkspaceStrategyCopy,
	}
	first, err := manager.Prepare(req)
	if err != nil {
		t.Fatal(err)
	}
	if !first.NewlyMaterialized {
		t.Fatal("first workspace preparation must report a new materialization")
	}
	second, err := manager.Prepare(req)
	if err != nil {
		t.Fatal(err)
	}
	if second.NewlyMaterialized {
		t.Fatal("reused workspace must not report a new materialization")
	}
}

func TestWorkspaceManagerSeparatesProjectsWithSameKey(t *testing.T) {
	stateRoot := t.TempDir()
	manager := NewWorkspaceManager()
	first, err := manager.Prepare(WorkspacePrepareRequest{
		ProjectID: "project-1", ProjectKey: "repo", RecordID: "record-1", ItemID: "APP-T-0001",
		RepoRoot: t.TempDir(), StateRoot: stateRoot, Strategy: WorkspaceStrategyCopy,
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := manager.Prepare(WorkspacePrepareRequest{
		ProjectID: "project-2", ProjectKey: "repo", RecordID: "record-1", ItemID: "APP-T-0001",
		RepoRoot: t.TempDir(), StateRoot: stateRoot, Strategy: WorkspaceStrategyCopy,
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.Path == second.Path || !strings.Contains(second.Path, "repo__project-2") {
		t.Fatalf("same-key projects reused one workspace: first=%q second=%q", first.Path, second.Path)
	}
}

func TestWorkspaceManagerRejectsMismatchedBranchMetadata(t *testing.T) {
	stateRoot := t.TempDir()
	manager := NewWorkspaceManager()
	req := WorkspacePrepareRequest{
		ProjectID: "project-1", ProjectKey: "MEM", RecordID: "record-1", ItemID: "MEM-T-0001",
		BranchName: "branch-a", RepoRoot: t.TempDir(), StateRoot: stateRoot, Strategy: WorkspaceStrategyCopy, WorkRevision: 0,
	}
	if _, err := manager.Prepare(req); err != nil {
		t.Fatal(err)
	}
	metadataPath := filepath.Join(stateRoot, "workspaces", "MEM", "record-1__branch-a", ".tusker", "workspace.json")
	if err := writeText(metadataPath, `{"project_id":"project-1","record_id":"record-1","branch_name":"other","created_at":"2026-04-28T00:00:00Z"}`+"\n"); err != nil {
		t.Fatal(err)
	}
	_, err := manager.Prepare(req)
	if err == nil || !strings.Contains(err.Error(), "branch_name does not match") {
		t.Fatalf("expected branch metadata mismatch error, got %v", err)
	}
}

func TestWorkspaceStrategyForRunKeepsInFlightWorkspaceKind(t *testing.T) {
	repo := t.TempDir()
	project := RegisteredProject{RepoRoot: repo}
	var shared, worktree Workflow
	shared.Workspace.Strategy, worktree.Workspace.Strategy = "shared", "worktree"
	cases := []struct {
		wf   Workflow
		path string
		want WorkspaceStrategy
	}{
		{shared, "", WorkspaceStrategyShared},
		{shared, filepath.Join(repo, "..", "worktrees", "APP-T-0001"), WorkspaceStrategyWorktree},
		{worktree, repo, WorkspaceStrategyShared},
		{worktree, "", WorkspaceStrategyWorktree},
	}
	for _, c := range cases {
		if got := workspaceStrategyForRun(c.wf, project, RunStatus{WorkspacePath: c.path}, nil); got != c.want {
			t.Fatalf("strategy %q path %q = %q, want %q", c.wf.Workspace.Strategy, c.path, got, c.want)
		}
	}
}

func TestSharedCheckoutRunHoldsWorkUntilSubmittedWorkLands(t *testing.T) {
	cases := []struct {
		run  RunStatus
		want bool
	}{
		{RunStatus{Lane: runLaneExecute, LeaseState: string(LeaseStateRunning)}, true},
		{RunStatus{Lane: runLaneExecute, LeaseState: string(LeaseStateUnclaimed)}, false},               // queued or retired
		{RunStatus{Lane: runLaneReview, LeaseState: string(LeaseStateRetryQueued)}, true},               // submitted, not landed
		{RunStatus{Lane: runLaneReview, LeaseState: string(LeaseStateReleased), Terminal: true}, false}, // finished
	}
	for _, c := range cases {
		if got := sharedCheckoutRunHoldsWork(c.run); got != c.want {
			t.Fatalf("sharedCheckoutRunHoldsWork(%+v) = %v, want %v", c.run, got, c.want)
		}
	}
}
