package main

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestWriteNotifyCLIUsesRepoIdentityAndCompletesSocketWrite(t *testing.T) {
	stateRoot, err := os.MkdirTemp("/tmp", "tusker-notify-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(stateRoot) })
	t.Setenv("TUSKER_STATE_ROOT", stateRoot)
	vault := pickupV7TestVault(t)
	wantProjectID, err := resolveV7ProjectID(vault)
	if err != nil {
		t.Fatal(err)
	}

	received := make(chan daemonControlRequest, 1)
	server, err := startDaemonControlServer(stateRoot, func(_ context.Context, req daemonControlRequest) daemonControlResponse {
		received <- req
		return daemonControlResponse{OK: true}
	})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	notifyDaemonForVault(Args{"vault": vault})
	select {
	case req := <-received:
		if req.Command != "reconcile_project" || req.ProjectID != wantProjectID {
			t.Fatalf("unexpected notification: %#v", req)
		}
	case <-time.After(time.Second):
		t.Fatal("CLI notification was not delivered before the command returned")
	}
}

func TestWriteNotifyDaemonDownIsBoundedAndIgnored(t *testing.T) {
	stateRoot := t.TempDir()
	started := time.Now()
	if err := sendDaemonControlOneWay(stateRoot, daemonControlRequest{Command: "reconcile_project", ProjectID: "app"}, 100*time.Millisecond); err == nil {
		t.Fatal("expected a missing daemon socket error")
	}
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("daemon-down notification took too long: %s", elapsed)
	}
}

func TestWriteNotifyDebouncesPerProjectWithoutConcurrentPolling(t *testing.T) {
	d := &Daemon{notifyWake: make(chan string, 8)}
	defer d.stopNotifyTimers()
	for range 5 {
		d.scheduleProjectReconcile("alpha")
	}
	d.scheduleProjectReconcile("beta")

	seen := map[string]int{}
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	for len(seen) < 2 {
		select {
		case projectID := <-d.notifyWake:
			seen[projectID]++
		case <-deadline.C:
			t.Fatalf("timed out waiting for debounced notifications: %#v", seen)
		}
	}
	select {
	case projectID := <-d.notifyWake:
		t.Fatalf("duplicate notification escaped debounce for %q", projectID)
	case <-time.After(450 * time.Millisecond):
	}
	if seen["alpha"] != 1 || seen["beta"] != 1 {
		t.Fatalf("expected one notification per project, got %#v", seen)
	}
}

func TestWriteNotifyTargetedLoadDoesNotReadOtherProjects(t *testing.T) {
	stateRoot := t.TempDir()
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	vaultA := pickupV7TestVault(t)
	vaultB := pickupV7TestVault(t)
	for _, vault := range []string{vaultA, vaultB} {
		if err := writeText(workflowPath(vault), defaultWorkflowMarkdown()); err != nil {
			t.Fatal(err)
		}
	}
	projects := []RegisteredProject{
		{ProjectID: "alpha", ProjectKey: "alpha", Name: "alpha", RepoRoot: filepath.Dir(vaultA), VaultRoot: vaultA, WorkflowPath: workflowPath(vaultA), Enabled: true, Health: projectHealthHealthy},
		{ProjectID: "beta", ProjectKey: "beta", Name: "beta", RepoRoot: filepath.Dir(vaultB), VaultRoot: vaultB, WorkflowPath: workflowPath(vaultB), Enabled: true, Health: projectHealthHealthy},
	}
	for _, project := range projects {
		if err := store.UpsertProject(project); err != nil {
			t.Fatal(err)
		}
	}
	forgetNoteCacheForWriteNotifyTest(vaultA)
	forgetNoteCacheForWriteNotifyTest(vaultB)
	var reads atomic.Int64
	noteCacheReadObserver = func() { reads.Add(1) }
	t.Cleanup(func() {
		noteCacheReadObserver = nil
	})

	loaded, err := loadRegisteredProjects(store, registeredProjectLoadOptions{Notes: true, FrontmatterOnly: true, ProjectID: "alpha"})
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 1 || loaded[0].Project.ProjectID != "alpha" || loaded[0].LoadError != nil || len(loaded[0].Notes) == 0 {
		t.Fatalf("targeted load returned %#v", loaded)
	}
	if reads.Load() == 0 {
		t.Fatal("target project notes were not read")
	}
	if !writeNotifyNoteCacheExists(vaultA) {
		t.Fatal("target project did not populate its note cache")
	}
	if writeNotifyNoteCacheExists(vaultB) {
		t.Fatal("non-target project note cache was populated")
	}
}

func forgetNoteCacheForWriteNotifyTest(vault string) {
	abs, err := filepath.Abs(vault)
	if err != nil {
		return
	}
	sharedVaultNoteCaches.mu.Lock()
	delete(sharedVaultNoteCaches.vaults, filepath.Clean(abs))
	sharedVaultNoteCaches.mu.Unlock()
}

func writeNotifyNoteCacheExists(vault string) bool {
	abs, err := filepath.Abs(vault)
	if err != nil {
		return false
	}
	sharedVaultNoteCaches.mu.Lock()
	defer sharedVaultNoteCaches.mu.Unlock()
	_, ok := sharedVaultNoteCaches.vaults[filepath.Clean(abs)]
	return ok
}
