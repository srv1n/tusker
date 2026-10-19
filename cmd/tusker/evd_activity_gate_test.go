package main

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// evdProject registers a project with one task. With live set, the task
// holds a claimed lease, so the project has a live run.
func evdProject(t *testing.T, live bool) (*Daemon, RegisteredProject, string) {
	t.Helper()
	vault := automationTestVault(t)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Activity gate", "risk": "low", "priority": "p0", "owned-paths": "src", "v7": "true"}, newV7Task)
	makeV7TaskDispatchableForTest(t, vault, "APP-T-0001")
	project := registerAutomationTestProject(t, vault)
	// A plain .git directory, so the stat signature covers git refs.
	if err := os.MkdirAll(filepath.Join(project.RepoRoot, ".git", "refs", "heads"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project.RepoRoot, ".git", "HEAD"), []byte("ref: refs/heads/main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	d, err := NewDaemon(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if !live {
		return d, project, vault
	}
	now := time.Now().UTC()
	workRevision := intField(mustTaskData(t, vault, "APP-T-0001"), "work_revision")
	run := RunStatus{
		ProjectID: project.ProjectID, RecordID: "APP-T-0001", ItemID: "APP-T-0001",
		Runner: string(RunnerCodexExec), Lane: runLaneExecute,
		LeaseState: string(LeaseStateUnclaimed), WorkRevision: workRevision, UpdatedAt: now.Format(time.RFC3339),
	}
	if err := d.store.UpsertRun(run); err != nil {
		t.Fatal(err)
	}
	claimed, err := d.store.ClaimRunLease(project.ProjectID, run.RecordID, "agent:editor", 1, defaultRunLeaseTTL, now, true, true, RuntimeLeaseClaimPrecondition{
		ExpectedLeaseState: LeaseStateUnclaimed, ExpectedWorkRevision: workRevision,
	})
	if err != nil || !claimed {
		t.Fatalf("claim live lease: claimed=%v err=%v", claimed, err)
	}
	return d, project, vault
}

// evdWatchMaterialWork counts vault walks, task reads and git processes.
func evdWatchMaterialWork(t *testing.T) (walks, reads *atomic.Int64, gitRan func() bool) {
	t.Helper()
	walks, reads = &atomic.Int64{}, &atomic.Int64{}
	previousList, previousRead, previousParse := noteCacheListObserver, noteCacheReadObserver, noteCacheParseObserver
	noteCacheListObserver = func() { walks.Add(1) }
	noteCacheReadObserver = func() { reads.Add(1) }
	noteCacheParseObserver = func() { reads.Add(1) }
	t.Cleanup(func() {
		noteCacheListObserver, noteCacheReadObserver, noteCacheParseObserver = previousList, previousRead, previousParse
	})
	bin := t.TempDir()
	marker := filepath.Join(t.TempDir(), "git-ran")
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte("#!/bin/sh\ntouch '"+marker+"'\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return walks, reads, func() bool { _, err := os.Stat(marker); return err == nil }
}

func TestHeartbeatPassDoesNoMaterialWork(t *testing.T) {
	d, project, vault := evdProject(t, true)
	path := filepath.Join(vault, "work", "waves", "W-0001.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	wave, err := serializeDocument(map[string]any{
		"schema": "tusker.wave/v7", "kind": "wave", "id": "W-0001", "status": "active",
		"authorization": "armed", "members": []any{"APP-T-0001"},
	}, "\n# Wave\n", v7FrontmatterOrder["wave"])
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(wave), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := d.runPoll(ctx, project.ProjectID); err != nil {
		t.Fatal(err)
	}
	if !d.projectHasArmedWork(project.ProjectID) {
		t.Fatal("the full scan did not see the armed wave")
	}

	walks, reads, gitRan := evdWatchMaterialWork(t)
	var loads atomic.Int64
	registeredProjectLoadObserver = func(opts registeredProjectLoadOptions) {
		if !opts.MetadataOnly {
			loads.Add(1)
		}
	}
	t.Cleanup(func() { registeredProjectLoadObserver = nil })
	now := time.Now().UTC()
	for beat := range 3 {
		if err := d.heartbeat(ctx, now.Add(time.Duration(beat)*reconcileLiveCadence)); err != nil {
			t.Fatal(err)
		}
	}
	if status := d.adaptiveReconcileStatus(project.ProjectID); status.Tier != "active" {
		t.Fatalf("a live run and armed wave left the project %#v", status)
	}
	if walks.Load() != 0 || reads.Load() != 0 || loads.Load() != 0 || gitRan() {
		t.Fatalf("heartbeat did material work: walks=%d reads=%d project loads=%d git=%v", walks.Load(), reads.Load(), loads.Load(), gitRan())
	}
}

func TestIdleProjectWaitsForInteraction(t *testing.T) {
	d, project, vault := evdProject(t, false)
	ctx := context.Background()
	walks, reads, gitRan := evdWatchMaterialWork(t)
	now := time.Now().UTC()
	for _, at := range []time.Time{now, now.Add(5 * time.Minute), now.Add(time.Hour), now.Add(24 * time.Hour)} {
		if err := d.heartbeat(ctx, at); err != nil {
			t.Fatal(err)
		}
	}
	d.frontierMu.Lock()
	_, statted := d.projectSignatures[project.ProjectID]
	d.frontierMu.Unlock()
	if walks.Load() != 0 || reads.Load() != 0 || gitRan() || statted || d.adaptiveReconcileStatus(project.ProjectID).Tier != "idle" {
		t.Fatalf("idle project got a pass: walks=%d reads=%d git=%v statted=%v tier=%s", walks.Load(), reads.Load(), gitRan(), statted, d.adaptiveReconcileStatus(project.ProjectID).Tier)
	}

	// The operator opens the project and clicks around: one wake.
	previous := daemonControlOneWaySender
	t.Cleanup(func() { daemonControlOneWaySender = previous })
	var wakes []daemonControlRequest
	daemonControlOneWaySender = func(_ string, req daemonControlRequest, _ time.Duration) error {
		wakes = append(wakes, req)
		return nil
	}
	server := newServeServer(vault, project.RepoRoot, defaultServeAddr, d.store, nil)
	for range 3 {
		server.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/api/tasks?project="+project.ProjectID, nil))
	}
	if len(wakes) != 1 || wakes[0].Cause != serveInteractionCause || wakes[0].ProjectID != project.ProjectID {
		t.Fatalf("UI interaction wakes = %#v", wakes)
	}

	// The daemon turns that wake into exactly one rescan.
	walks.Store(0)
	d.controlReconcileProject(wakes[0])
	rescans := 0
	for len(d.notifyWake) > 0 {
		projectID := <-d.notifyWake
		d.takeProjectReconcile(projectID)
		if err := d.runPoll(ctx, projectID); err != nil {
			t.Fatal(err)
		}
		rescans++
	}
	if rescans != 1 || walks.Load() == 0 {
		t.Fatalf("one interaction caused %d rescans (walks=%d)", rescans, walks.Load())
	}
	walks.Store(0)
	for beat := range 3 {
		if err := d.heartbeat(ctx, time.Now().UTC().Add(time.Duration(beat)*reconcileLiveCadence)); err != nil {
			t.Fatal(err)
		}
	}
	if walks.Load() != 0 || d.adaptiveReconcileStatus(project.ProjectID).Tier != "active" {
		t.Fatalf("after its rescan the interacted project rescanned again: walks=%d", walks.Load())
	}
}

func TestActiveProjectNoticesHandEdit(t *testing.T) {
	d, project, vault := evdProject(t, true)
	ctx := context.Background()
	if err := d.runPoll(ctx, project.ProjectID); err != nil {
		t.Fatal(err)
	}
	if due, err := d.heartbeatProjectsDue(time.Now().UTC()); err != nil || len(due) != 0 {
		t.Fatalf("unchanged active project was due: %#v err=%v", due, err)
	}

	// A hand edit (say, in Obsidian) sends no CLI event.
	path := writeReconcileResourceTask(t, vault, "APP-T-0099", "backlog")
	if err := d.heartbeat(ctx, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if got := d.pollTaskMeta[project.ProjectID+"\x00APP-T-0099"]; got.TaskID != "APP-T-0099" {
		t.Fatalf("heartbeat did not rescan after the hand edit: %#v", got)
	}
	if due, err := d.heartbeatProjectsDue(time.Now().UTC()); err != nil || len(due) != 0 {
		t.Fatalf("the rescan did not settle the signature: %#v err=%v", due, err)
	}

	// Editing an existing file shows as a newer modification time.
	later := time.Now().Add(time.Minute)
	if err := os.Chtimes(path, later, later); err != nil {
		t.Fatal(err)
	}
	if due, err := d.heartbeatProjectsDue(time.Now().UTC()); err != nil || !due[project.ProjectID] {
		t.Fatalf("an edited task file did not make the project due for a full rescan: %#v err=%v", due, err)
	}
}
