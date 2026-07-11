package main

import (
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func addServeProjectFixture(t *testing.T, server *serveServer, projectID, taskID, title string) RegisteredProject {
	t.Helper()
	root := t.TempDir()
	vault := filepath.Join(root, ".tusker")
	for _, dir := range []string{filepath.Join(vault, "work", "tasks"), filepath.Join(vault, "work", "epics")} {
		if err := ensureDir(dir); err != nil {
			t.Fatal(err)
		}
	}
	if err := writeText(filepath.Join(root, "tusker.yaml"), "schema: tusker.config/v1\nproject_id: "+projectID+"\nstorage:\n  root: .tusker\nruntime:\n  mutation_mode: single_user_local\n"); err != nil {
		t.Fatal(err)
	}
	if err := writeText(workflowPath(vault), defaultWorkflowMarkdown()); err != nil {
		t.Fatal(err)
	}
	writeServeEpic(t, vault, "ALT", "Alternate")
	writeServeTask(t, vault, serveTaskSeed{ID: taskID, Epic: "ALT", Title: title, Status: "ready", Risk: "medium", Priority: "p1"})
	project := RegisteredProject{ProjectID: projectID, ProjectKey: projectID, Name: projectID, RepoRoot: root, VaultRoot: vault, WorkflowPath: workflowPath(vault), Enabled: true, Health: projectHealthHealthy}
	if err := server.store.UpsertProject(project); err != nil {
		t.Fatal(err)
	}
	return project
}

func TestServeMultiProjectRoutesOwnVaultProjection(t *testing.T) {
	server := newServeEmptyNeedsFixture(t)
	other := addServeProjectFixture(t, server, "backend", "BCK-T-0001", "Backend task")

	var primary, backend []serveTaskCapsule
	serveDecode(t, server, "/api/tasks?project=app", &primary)
	serveDecode(t, server, "/api/tasks?project=backend", &backend)
	if len(primary) != 1 || primary[0].ID != "APP-T-0001" {
		t.Fatalf("primary projection contaminated: %#v", primary)
	}
	if len(backend) != 1 || backend[0].ID != "BCK-T-0001" || backend[0].Title != "Backend task" {
		t.Fatalf("backend projection missing: %#v", backend)
	}

	var epics []serveEpicSummary
	serveDecode(t, server, "/api/epics?project=backend", &epics)
	if len(epics) != 1 || epics[0].ID != "ALT" {
		t.Fatalf("backend epics missing: %#v", epics)
	}
	var docs []serveDocListEntry
	serveDecode(t, server, "/api/docs?project=backend", &docs)
	if len(docs) == 0 {
		t.Fatal("backend docs projection is empty")
	}

	var detail serveTaskDetail
	serveDecode(t, server, "/api/tasks/BCK-T-0001?project=backend", &detail)
	if detail.ID != "BCK-T-0001" || !sameCleanPath(other.VaultRoot, filepath.Join(other.RepoRoot, ".tusker")) {
		t.Fatalf("backend detail resolved from wrong project: %#v", detail)
	}
}

func TestServeSnapshotCacheReusesBuildAndInvalidatesPerProject(t *testing.T) {
	server := newServeEmptyNeedsFixture(t)
	other := addServeProjectFixture(t, server, "backend", "BCK-T-0001", "Backend task")

	const readers = 12
	var wg sync.WaitGroup
	errs := make(chan error, readers)
	for range readers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := server.loadSnapshotForProject("backend")
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}

	server.snapshotMu.Lock()
	entry := server.snapshots[serveSnapshotKey(other)]
	builds := entry.buildCount
	server.snapshotMu.Unlock()
	if builds != 1 {
		t.Fatalf("concurrent cold reads built projection %d times, want 1", builds)
	}
	for range 5 {
		if _, err := server.loadSnapshotForProject("backend"); err != nil {
			t.Fatal(err)
		}
	}
	server.snapshotMu.Lock()
	builds = entry.buildCount
	server.snapshotMu.Unlock()
	if builds != 1 {
		t.Fatalf("warm reads rebuilt projection: %d", builds)
	}

	server.snapshotMu.Lock()
	entry.invalid = true
	entry.building = true
	entry.done = make(chan struct{})
	server.snapshotMu.Unlock()
	served := make(chan error, 1)
	go func() {
		_, err := server.loadSnapshotForProject("backend")
		served <- err
	}()
	select {
	case err := <-served:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("warm reader blocked behind an in-flight refresh")
	}
	server.snapshotMu.Lock()
	entry.invalid = false
	entry.building = false
	close(entry.done)
	server.snapshotMu.Unlock()

	writeServeTask(t, other.VaultRoot, serveTaskSeed{ID: "BCK-T-0002", Epic: "ALT", Title: "Fresh task", Status: "ready", Risk: "medium", Priority: "p1"})
	server.invalidateProjectSnapshot("backend")
	snap, err := server.loadFreshSnapshotForProject("backend")
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.tasks) != 2 {
		t.Fatalf("invalidated projection stayed stale: %d tasks", len(snap.tasks))
	}
	server.snapshotMu.Lock()
	backendBuilds := entry.buildCount
	primaryEntry := server.snapshots["app"]
	server.snapshotMu.Unlock()
	if backendBuilds != 2 {
		t.Fatalf("backend invalidation builds=%d, want 2", backendBuilds)
	}
	if primaryEntry != nil && primaryEntry.invalid {
		t.Fatal("backend invalidation dirtied primary projection")
	}
}

func TestServeSnapshotCacheEagerlyWarmsRegisteredProjects(t *testing.T) {
	server := newServeEmptyNeedsFixture(t)
	other := addServeProjectFixture(t, server, "backend", "BCK-T-0001", "Backend task")
	server.warmRegisteredProjectSnapshots()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		server.snapshotMu.Lock()
		primary := server.snapshots["app"]
		backend := server.snapshots[serveSnapshotKey(other)]
		ready := primary != nil && primary.ready && backend != nil && backend.ready
		server.snapshotMu.Unlock()
		if ready {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("registered project projections did not warm")
}
