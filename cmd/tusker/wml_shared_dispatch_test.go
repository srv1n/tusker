package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// sharedDispatchFixture registers a git project in shared mode whose tasks
// own the given paths, with a codex shim that records its working directory.
func sharedDispatchFixture(t *testing.T, owned ...string) (string, RegisteredProject, WorkflowFile, string) {
	t.Helper()
	vault := automationTestVault(t)
	disableReviewerForTest(t, vault)
	for i, path := range owned {
		mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": fmt.Sprintf("Shared %d", i+1), "risk": "low", "priority": "p0", "owned-paths": "src", "v7": "true"}, newV7Task)
		id := fmt.Sprintf("APP-T-%04d", i+1)
		makeV7TaskDispatchableForTest(t, vault, id)
		setAutomationV7TaskFields(t, vault, id, map[string]any{"owned_paths": []string{path}})
	}
	// The project setting lives in config.yaml, which overlays WORKFLOW.md.
	if _, err := setProjectLocalConfigWithReadback(vault, "automation.workspace.strategy", "shared"); err != nil {
		t.Fatal(err)
	}
	initializeOrchestrationGitRepo(t, filepath.Dir(vault))
	project := registerAutomationTestProject(t, vault)
	wfFile, err := loadWorkflow(vault)
	if err != nil {
		t.Fatal(err)
	}
	if got := workspaceStrategyFromWorkflow(wfFile.Data.Workspace.Strategy); got != WorkspaceStrategyShared {
		t.Fatalf("fixture strategy = %q, want shared", got)
	}
	wfFile.Data.Runtime.MaxActiveRunsPerProject = len(owned)
	wfFile.Data.Agents.Default = "test-shared"
	wfFile.Data.Agents.Enabled = append(wfFile.Data.Agents.Enabled, "test-shared")
	wfFile.Data.Runners["test-shared"] = RunnerDefinition{Kind: string(RunnerCodexExec), Command: defaultCodexExecCommand()}

	cwdLog := filepath.Join(t.TempDir(), "cwd.log")
	binDir := filepath.Join(t.TempDir(), "bin")
	if err := ensureDir(binDir); err != nil {
		t.Fatal(err)
	}
	shim := "#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then\n  echo codex-test-shim\n  exit 0\nfi\npwd -P >> " + cwdLog + "\nexec sleep 5\n"
	if err := writeText(filepath.Join(binDir, "codex"), shim); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(binDir, "codex"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return vault, project, wfFile, cwdLog
}

func dispatchSharedTask(t *testing.T, daemon *Daemon, project RegisteredProject, wfFile WorkflowFile, vault, id string) (RunStatus, error) {
	t.Helper()
	note, err := resolveNote(vault, id)
	if err != nil {
		t.Fatal(err)
	}
	run := RunStatus{ProjectID: project.ProjectID, RecordID: id, ItemID: id, Runner: "test-shared", Lane: runLaneExecute,
		LeaseState: string(LeaseStateUnclaimed), AttemptOutcome: string(AttemptOutcomeNone), WorkRevision: intField(note.Data, "work_revision")}
	if err := daemon.store.UpsertRun(run); err != nil {
		t.Fatal(err)
	}
	dispatched, _, err := daemon.dispatchRun(context.Background(), project, wfFile, note, run, runLaneExecute)
	if pgid := dispatched.ProcessPGID; pgid > 0 {
		t.Cleanup(func() { _ = syscall.Kill(-pgid, syscall.SIGKILL) })
	} else if pid := dispatched.ProcessPID; pid > 0 {
		t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })
	}
	return dispatched, err
}

func openSharedDispatchDaemon(t *testing.T) *Daemon {
	t.Helper()
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	return &Daemon{stateRoot: DefaultStateRoot(), store: store}
}

func TestSharedModeDispatchUsesCheckout(t *testing.T) {
	vault, project, wfFile, cwdLog := sharedDispatchFixture(t, "owned/APP-T-0001")
	repo := canonicalPath(project.RepoRoot)
	refsBefore, _ := gitOutputTrim(repo, "for-each-ref", "--format=%(refname)", "refs/heads")
	daemon := openSharedDispatchDaemon(t)

	dispatched, err := dispatchSharedTask(t, daemon, project, wfFile, vault, "APP-T-0001")
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, string(LeaseStateRunning), dispatched.LeaseState, "shared dispatch state")
	assertEqual(t, repo, canonicalPath(dispatched.WorkspacePath), "run workspace")
	attempts, err := daemon.store.ListAttemptsForRun(project.ProjectID, "APP-T-0001")
	if err != nil || len(attempts) != 1 {
		t.Fatalf("attempts = %#v err=%v", attempts, err)
	}
	assertEqual(t, repo, canonicalPath(attempts[0].WorkspacePath), "attempt workspace")
	if branch, _ := currentGitBranchIn(repo); attempts[0].BranchName != branch {
		t.Fatalf("attempt branch = %q, want the checkout's own branch %q", attempts[0].BranchName, branch)
	}
	if list, _ := gitOutputTrim(repo, "worktree", "list", "--porcelain"); strings.Count(list, "worktree ") != 1 {
		t.Fatalf("shared dispatch created a worktree:\n%s", list)
	}
	if refs, _ := gitOutputTrim(repo, "for-each-ref", "--format=%(refname)", "refs/heads"); refs != refsBefore {
		t.Fatalf("shared dispatch created branches: before=%q after=%q", refsBefore, refs)
	}
	if fileExists(filepath.Join(DefaultStateRoot(), "workspaces", project.ProjectKey)) {
		t.Fatal("shared dispatch materialized a per-task workspace")
	}
	waitForSharedDispatchCwd(t, cwdLog, 1)
	// The shim exits after five seconds and the daemon may start a
	// continuation, so every start it logged must be in the checkout.
	got, _ := readText(cwdLog)
	for _, cwd := range strings.Fields(got) {
		if canonicalPath(cwd) != repo {
			t.Fatalf("agent cwd = %q, want repo root %q", cwd, repo)
		}
	}
}

func TestSharedModeOverlappingTasksDoNotRunTogether(t *testing.T) {
	vault, project, wfFile, _ := sharedDispatchFixture(t, "src/api", "src", "docs")
	daemon := openSharedDispatchDaemon(t)
	repo := canonicalPath(project.RepoRoot)

	first, err := dispatchSharedTask(t, daemon, project, wfFile, vault, "APP-T-0001")
	if err != nil || first.LeaseState != string(LeaseStateRunning) {
		t.Fatalf("first shared task did not start: %#v err=%v", first, err)
	}
	overlapping, err := dispatchSharedTask(t, daemon, project, wfFile, vault, "APP-T-0002")
	if err == nil && isDispatchingLeaseState(overlapping.LeaseState) {
		t.Fatalf("overlapping task started in the same checkout: %#v", overlapping)
	}
	if reason := fmt.Sprint(err, overlapping.LastError); !strings.Contains(reason, "OWNED_PATH_CONFLICT") && !strings.Contains(reason, "claim refused") {
		t.Fatalf("overlapping task refused for the wrong reason: %s", reason)
	}
	if attempts, _ := daemon.store.ListAttemptsForRun(project.ProjectID, "APP-T-0002"); len(attempts) != 0 {
		t.Fatalf("overlapping task got an attempt: %#v", attempts)
	}
	disjoint, err := dispatchSharedTask(t, daemon, project, wfFile, vault, "APP-T-0003")
	if err != nil || disjoint.LeaseState != string(LeaseStateRunning) {
		t.Fatalf("disjoint task did not run alongside: %#v err=%v", disjoint, err)
	}
	assertEqual(t, repo, canonicalPath(disjoint.WorkspacePath), "disjoint task workspace")
}

// A shared-checkout submission commits only the task's owned files. Dirty
// files nobody owns are reported and stay uncommitted; another task's files
// are never staged.
func TestSharedModeCommitsOnlyOwnedFiles(t *testing.T) {
	vault, project := workSessionFixture(t, 2)
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	runs := map[string]RunStatus{}
	for _, id := range []string{"APP-T-0001", "APP-T-0002"} {
		if err := startWorkSessionTest(t, vault, id, "agent:"+id); err != nil {
			t.Fatal(err)
		}
		run, err := store.FindRunScoped(project.ProjectID, id)
		if err != nil || run == nil {
			t.Fatalf("missing run %s: %v", id, err)
		}
		run.WorkspacePath, run.ActiveAttemptID, run.HandRun = project.RepoRoot, "attempt-"+id, false
		run.Lane, run.LeaseState = runLaneExecute, string(LeaseStateRunning)
		if err := store.UpsertRun(*run); err != nil {
			t.Fatal(err)
		}
		runs[id] = *run
	}
	if err := writeText(filepath.Join(vault, "workspace.json"), `{"strategy":"shared"}`); err != nil {
		t.Fatal(err)
	}
	repo := project.RepoRoot
	if err := writeText(filepath.Join(repo, "tracked-stray.txt"), "base\n"); err != nil {
		t.Fatal(err)
	}
	runGitDir(t, repo, "add", "tracked-stray.txt")
	runGitDir(t, repo, "commit", "-qm", "seed tracked stray")
	head, _ := gitRevParse(repo, "HEAD^{commit}")
	for path, content := range map[string]string{
		"owned/APP-T-0001/own.txt": "own", "owned/APP-T-0002/other.txt": "other",
		"stray.txt": "stray", "tracked-stray.txt": "edited\n",
	} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(repo, path)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := writeText(filepath.Join(repo, path), content); err != nil {
			t.Fatal(err)
		}
	}

	endState, err := captureSubmissionEndState(store, runs["APP-T-0001"], `{"A1":"pass"}`, "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if endState.HeadSHA == "" || endState.HeadSHA == head {
		t.Fatalf("shared submission was not committed: head=%s end=%s", head, endState.HeadSHA)
	}
	changed, err := gitOutputTrim(repo, "diff", "--name-only", head, endState.HeadSHA)
	if err != nil {
		t.Fatal(err)
	}
	if changed != "owned/APP-T-0001/own.txt" {
		t.Fatalf("submission commit touches %q, want only the owned file", changed)
	}
	for _, stray := range []string{"stray.txt", "tracked-stray.txt"} {
		if !containsString(endState.StrayPaths, stray) {
			t.Fatalf("unowned edit %s not reported: %v", stray, endState.StrayPaths)
		}
		if containsString(endState.MaterialScope, stray) {
			t.Fatalf("unowned edit %s joined the submitted material: %v", stray, endState.MaterialScope)
		}
	}
	if endState.Overlaps["owned/APP-T-0002/other.txt"] != "APP-T-0002" {
		t.Fatalf("other task's file not reported as its own: %v", endState.Overlaps)
	}
	if got, _ := readText(filepath.Join(repo, "tracked-stray.txt")); got != "edited\n" {
		t.Fatalf("unowned edit was discarded: %q", got)
	}
	if now, _ := gitRevParse(repo, "HEAD^{commit}"); now != head {
		t.Fatalf("submission moved the checkout HEAD: %s -> %s", head, now)
	}
	declared, _, err := canonicalRunMaterialScopeWithGeneratedOutputs(store, runs["APP-T-0001"])
	if err != nil {
		t.Fatal(err)
	}
	if !submittedMaterialScopeMatches(declared, RunAttempt{EndState: endState}) {
		t.Fatalf("review binding refuses the owned-only submission: %v", endState.MaterialScope)
	}
}

func waitForSharedDispatchCwd(t *testing.T, path string, lines int) {
	t.Helper()
	for i := 0; i < 100; i++ {
		if got, _ := readText(path); strings.Count(got, "\n") >= lines {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("agent never recorded its working directory in %s", path)
}
