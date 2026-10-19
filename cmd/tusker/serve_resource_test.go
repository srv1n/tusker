package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestServeWarmRunReadsSkipVaultAndDocsBuilds(t *testing.T) {
	server := newServeEmptyNeedsFixture(t)
	run := RunStatus{ProjectID: "app", RecordID: "APP-T-0001", ItemID: "APP-T-0001", Runner: string(RunnerCodexExec), Lane: runLaneExecute, LeaseState: string(LeaseStateReleased), AttemptOutcome: string(AttemptOutcomeFailed), Terminal: true}
	if err := server.store.UpsertRun(run); err != nil {
		t.Fatal(err)
	}
	if _, err := server.loadSnapshotForProject("app"); err != nil {
		t.Fatal(err)
	}
	indexLoads, docsWalks := 0, 0
	loadV7IndexObserver = func() { indexLoads++ }
	serveDocWalkObserver = func(string) { docsWalks++ }
	t.Cleanup(func() { loadV7IndexObserver, serveDocWalkObserver = nil, nil })
	for range 4 {
		var detail serveRunDetail
		serveDecode(t, server, "/api/runs/APP-T-0001?project=app", &detail)
		var list []serveRunSummary
		serveDecode(t, server, "/api/runs?project=app&all=true", &list)
		if len(list) != 1 || detail.TaskID != run.ItemID {
			t.Fatalf("run reads lost rows: detail=%#v list=%#v", detail, list)
		}
	}
	if indexLoads != 0 || docsWalks != 0 || server.snapshots["app"].buildCount != 1 {
		t.Fatalf("warm reads reloaded: index=%d docs=%d builds=%d", indexLoads, docsWalks, server.snapshots["app"].buildCount)
	}

	// No broad invalidation is required to observe control settlement/current
	// lifecycle facts, including a task edited outside Serve.
	intent := runSessionControlIntent{Action: runSessionControlStop, State: runSessionControlPending, ProjectID: "app", RecordID: run.RecordID}
	if err := saveRunSessionControlIntent(server.store, intent); err != nil {
		t.Fatal(err)
	}
	var detail serveRunDetail
	serveDecode(t, server, "/api/runs/APP-T-0001?project=app", &detail)
	if detail.Controls.Pending == nil {
		t.Fatal("current control intent missing from warm detail")
	}
	intent.State = runSessionControlSettledState
	if err := saveRunSessionControlIntent(server.store, intent); err != nil {
		t.Fatal(err)
	}
	run.LeaseState, run.AttemptOutcome = string(LeaseStateInterrupted), string(AttemptOutcomeCancelled)
	if err := server.store.UpsertRun(run); err != nil {
		t.Fatal(err)
	}
	writeServeTask(t, server.vaultPath, serveTaskSeed{ID: run.ItemID, Epic: "APP", Title: "Current task", Status: "done", Risk: "medium", Priority: "p2"})
	detail = serveRunDetail{}
	serveDecode(t, server, "/api/runs/APP-T-0001?project=app", &detail)
	if detail.Controls.Pending != nil || detail.LeaseStateRaw != string(LeaseStateInterrupted) || detail.OperatorState.State != "finished" || detail.TaskTitle != "Current task" {
		t.Fatalf("warm detail missed current facts: %#v", detail)
	}
	if server.snapshots["app"].buildCount != 1 || indexLoads != 0 || docsWalks != 0 {
		t.Fatal("targeted readback rebuilt the full projection")
	}
	if got := stringField(server.snapshots["app"].snapshot.notesByID[run.ItemID].Data, "status"); got != "ready" {
		t.Fatalf("request freshness mutated shared note snapshot: %s", got)
	}
}

func TestServeRuntimeInvalidationKeepsNotesDocsProjection(t *testing.T) {
	server := newServeEmptyNeedsFixture(t)
	if _, err := server.loadSnapshotForProject("app"); err != nil {
		t.Fatal(err)
	}
	indexLoads, docsWalks := 0, 0
	loadV7IndexObserver = func() { indexLoads++ }
	serveDocWalkObserver = func(string) { docsWalks++ }
	t.Cleanup(func() { loadV7IndexObserver, serveDocWalkObserver = nil, nil })
	run := RunStatus{ProjectID: "app", RecordID: "APP-T-0001", ItemID: "APP-T-0001", LeaseState: string(LeaseStateInterrupted), AttemptOutcome: string(AttemptOutcomeCancelled), Terminal: true}
	if err := server.store.UpsertRun(run); err != nil {
		t.Fatal(err)
	}
	server.invalidateProjectRuntimeSnapshot("app")
	snap, err := server.loadFreshSnapshotForProject("app")
	if err != nil {
		t.Fatal(err)
	}
	entry := server.snapshots["app"]
	if len(snap.runs) != 1 || entry.buildCount != 1 || entry.runtimeBuildCount != 1 || indexLoads != 0 || docsWalks != 0 {
		t.Fatalf("runtime update rebuilt content: runs=%d builds=%d runtime=%d index=%d docs=%d", len(snap.runs), entry.buildCount, entry.runtimeBuildCount, indexLoads, docsWalks)
	}
	if entry.contentHash != serveSnapshotContentHash(snap) {
		t.Fatal("incremental runtime hash disagrees with fresh projection hash")
	}
}

func TestServeForgetProjectDropsProjectionAndNoteCache(t *testing.T) {
	server := newServeEmptyNeedsFixture(t)
	if _, err := server.loadSnapshotForProject("app"); err != nil {
		t.Fatal(err)
	}
	if err := server.store.RemoveProject("app"); err != nil {
		t.Fatal(err)
	}
	server.forgetProjectSnapshot("app")
	if len(server.snapshots) != 0 {
		t.Fatalf("removed projection retained: %#v", server.snapshots)
	}
	sharedVaultNoteCaches.mu.Lock()
	_, retained := sharedVaultNoteCaches.vaults[filepath.Clean(server.vaultPath)]
	sharedVaultNoteCaches.mu.Unlock()
	if retained {
		t.Fatal("removed project's parser cache retained")
	}
	if _, err := server.loadSnapshotForProject("app"); err == nil {
		t.Fatal("removed project remained available through cache")
	}
}

func TestServeWaveRequestSharesMaterialAndNextRequestReadsChanges(t *testing.T) {
	server := newServeEmptyNeedsFixture(t)
	writeServeWave(t, server.vaultPath, "W-0001", "One", []string{"APP-T-0001"})
	writeServeWave(t, server.vaultPath, "W-0002", "Two", []string{"APP-T-0001"})
	snap, err := server.loadSnapshotForProject("app")
	if err != nil {
		t.Fatal(err)
	}
	context := newWaveMaterialReadContext()
	out := server.attachWaveRecovery(serveWaves(snap, context), snap, context)
	if len(out) != 2 || context.corpusLoads != 1 || len(context.specs) != 1 || len(context.fingerprints) != 2 {
		t.Fatalf("request material work was repeated: waves=%d corpus=%d specs=%d fingerprints=%d", len(out), context.corpusLoads, len(context.specs), len(context.fingerprints))
	}
	old := stringField(out[0].Authorization, "fingerprint")
	// Spec text is not material (M5), but whether a cited spec resolves is.
	if err := os.Remove(filepath.Join(server.repoRoot, "docs", "specs", "test-fixture.md")); err != nil {
		t.Fatal(err)
	}
	fresh := newWaveMaterialReadContext()
	changed := server.attachWaveRecovery(serveWaves(snap, fresh), snap, fresh)
	if stringField(changed[0].Authorization, "fingerprint") == old || fresh.corpusLoads != 1 {
		t.Fatal("request material cache leaked stale authority into next request")
	}
}

func TestServeRunListActivitySharesByteBudget(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	if err := writeText(path, strings.Repeat(`{"kind":"message","payload":{"text":"activity"}}`+"\n", 2000)); err != nil {
		t.Fatal(err)
	}
	budget := int64(4096)
	first := loadRunStateTailsWithBudget(RunStatus{EventSinkPath: path, RawLogPath: path}, nil, &budget, 50)
	if budget != 0 || len(first.events) > 50 || len(first.log) != 0 {
		t.Fatalf("first row exceeded shared budget: remaining=%d events=%d log=%d", budget, len(first.events), len(first.log))
	}
	later := loadRunStateTailsWithBudget(RunStatus{EventSinkPath: path}, nil, &budget, 50)
	if len(later.events) != 0 || budget != 0 {
		t.Fatal("later rows read after request activity budget exhausted")
	}
}

func TestServeWarmRecoveryPreflightUsesRequestIndex(t *testing.T) {
	server := newServeEmptyNeedsFixture(t)
	writeServeWave(t, server.vaultPath, "W-0001", "Recovery", []string{"APP-T-0001"})
	taskPath := filepath.Join(server.vaultPath, "work", "tasks", "APP-T-0001.md")
	data, body, err := parseFrontmatterMustRead(taskPath)
	if err != nil {
		t.Fatal(err)
	}
	data["wave"] = "W-0001"
	text, err := serializeDocument(data, body, v7FrontmatterOrder["task"])
	if err != nil {
		t.Fatal(err)
	}
	if err := writeText(taskPath, text); err != nil {
		t.Fatal(err)
	}
	idx, err := loadV7Index(server.vaultPath)
	if err != nil {
		t.Fatal(err)
	}
	wave := idx.Waves["W-0001"]
	fingerprint, _ := waveMaterialFingerprint(server.vaultPath, idx, wave)
	wave.Data["authorization"], wave.Data["authorization_fingerprint"], wave.Data["authorized_at"] = "armed", fingerprint, "2026-10-15T00:00:00Z"
	text, err = serializeDocument(wave.Data, wave.Body, v7FrontmatterOrder["wave"])
	if err != nil {
		t.Fatal(err)
	}
	if err := writeText(wave.AbsolutePath, text); err != nil {
		t.Fatal(err)
	}
	run := RunStatus{ProjectID: "app", RecordID: "APP-T-0001", ItemID: "APP-T-0001", Runner: string(RunnerCodexExec), Lane: runLaneExecute, LeaseState: string(LeaseStateReleased), AttemptOutcome: string(AttemptOutcomeUnknown), Terminal: true}
	if err := server.store.UpsertRun(run); err != nil {
		t.Fatal(err)
	}
	if _, err := server.loadSnapshotForProject("app"); err != nil {
		t.Fatal(err)
	}
	loads := 0
	loadV7IndexObserver = func() { loads++ }
	t.Cleanup(func() { loadV7IndexObserver = nil })
	var detail serveRunDetail
	serveDecode(t, server, "/api/runs/APP-T-0001?project=app", &detail)
	if loads != 0 {
		t.Fatalf("warm recovery controls loaded the vault %d times", loads)
	}
	// Spec text is not material (M5); a member's promised scope is.
	data["owned_paths"] = []any{"src/changed-scope"}
	if text, err = serializeDocument(data, body, v7FrontmatterOrder["task"]); err != nil {
		t.Fatal(err)
	}
	if err := writeText(taskPath, text); err != nil {
		t.Fatal(err)
	}
	// Actual mutation admission still reads fresh material, even after a warm
	// display preflight has reused the request's projection.
	_, _, reason := contextRecoveryPreflight(server.store, wave, run)
	if !strings.Contains(reason, "wave material changed") {
		t.Fatalf("mutation preflight reused display authority: %s", reason)
	}
}

func TestServeRunDetailWaitsForMetadataRefresh(t *testing.T) {
	server := newServeEmptyNeedsFixture(t)
	run := RunStatus{ProjectID: "app", RecordID: "APP-T-0001", ItemID: "APP-T-0001", LeaseState: string(LeaseStateReleased), AttemptOutcome: string(AttemptOutcomeFailed), Terminal: true}
	if err := server.store.UpsertRun(run); err != nil {
		t.Fatal(err)
	}
	if _, err := server.loadSnapshotForProject("app"); err != nil {
		t.Fatal(err)
	}
	server.snapshotMu.Lock()
	entry := server.snapshots["app"]
	entry.building = true
	entry.done = make(chan struct{})
	server.snapshotMu.Unlock()
	unblock := func() {
		server.snapshotMu.Lock()
		if entry.building {
			entry.building = false
			close(entry.done)
		}
		server.snapshotMu.Unlock()
	}
	defer unblock()
	rec := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		server.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/runs/APP-T-0001?project=app", nil))
		close(done)
	}()
	select {
	case <-done:
		t.Fatal("detail controls returned the old member/gate projection during metadata refresh")
	case <-time.After(50 * time.Millisecond):
	}
	unblock()
	select {
	case <-done:
		if rec.Code != http.StatusOK {
			t.Fatalf("detail failed after metadata refresh: %d %s", rec.Code, rec.Body.String())
		}
	case <-time.After(time.Second):
		t.Fatal("detail did not resume when metadata refresh settled")
	}
}

func TestServeRuntimeEventDoesNotDiscardMetadataRefresh(t *testing.T) {
	server := newServeEmptyNeedsFixture(t)
	if _, err := server.loadSnapshotForProject("app"); err != nil {
		t.Fatal(err)
	}
	writeServeTask(t, server.vaultPath, serveTaskSeed{ID: "APP-T-0002", Epic: "APP", Title: "New metadata", Status: "ready", Risk: "medium", Priority: "p2"})
	interrupted := false
	serveDocWalkObserver = func(string) {
		if !interrupted {
			interrupted = true
			server.invalidateProjectRuntimeSnapshot("app")
		}
	}
	t.Cleanup(func() { serveDocWalkObserver = nil })
	server.invalidateProjectSnapshot("app")
	snap, err := server.loadFreshSnapshotForProject("app")
	if err != nil {
		t.Fatal(err)
	}
	if !interrupted || len(snap.tasks) != 2 || server.snapshots["app"].buildCount != 2 {
		t.Fatalf("runtime notification lost metadata rebuild: interrupted=%v tasks=%d builds=%d", interrupted, len(snap.tasks), server.snapshots["app"].buildCount)
	}
}

func TestServeFactoryAndReviewShareSnapshotMaterialReads(t *testing.T) {
	server := newServeEmptyNeedsFixture(t)
	writeServeTask(t, server.vaultPath, serveTaskSeed{ID: "APP-T-0001", Epic: "APP", Title: "First review", Status: "review", Risk: "medium", Priority: "p2"})
	writeServeTask(t, server.vaultPath, serveTaskSeed{ID: "APP-T-0002", Epic: "APP", Title: "Second review", Status: "review", Risk: "medium", Priority: "p2"})
	writeServeWave(t, server.vaultPath, "W-0001", "One", []string{"APP-T-0001"})
	writeServeWave(t, server.vaultPath, "W-0002", "Two", []string{"APP-T-0002"})
	snap, err := server.loadSnapshotForProject("app")
	if err != nil {
		t.Fatal(err)
	}
	material := newWaveMaterialReadContext()
	factory, err := buildFactoryOperationsWithIndex(server.vaultPath, snap.project, snap.workflow, server.store, server.now(), serveSnapshotIndex(snap), material)
	if err != nil {
		t.Fatal(err)
	}
	if len(factory.Authority.Waves) != 2 || material.corpusLoads != 1 || len(material.specs) != 1 {
		t.Fatalf("factory repeated wave material reads: waves=%d corpus=%d specs=%d", len(factory.Authority.Waves), material.corpusLoads, len(material.specs))
	}
	reviewMaterial := newWaveMaterialReadContext()
	batch := serveReviewBatchFor(snap, reviewMaterial)
	if len(batch.Waves) != 2 || reviewMaterial.corpusLoads != 1 || len(reviewMaterial.specs) != 1 {
		t.Fatalf("review repeated wave material reads: waves=%d corpus=%d specs=%d", len(batch.Waves), reviewMaterial.corpusLoads, len(reviewMaterial.specs))
	}
	indexLoads, docsWalks := 0, 0
	loadV7IndexObserver = func() { indexLoads++ }
	serveDocWalkObserver = func(string) { docsWalks++ }
	t.Cleanup(func() { loadV7IndexObserver, serveDocWalkObserver = nil, nil })
	for range 2 {
		serveDecode(t, server, "/api/factory-operations?project=app", &factory)
		serveDecode(t, server, "/api/review/batch?project=app", &batch)
	}
	now := server.now()
	run := RunStatus{ProjectID: "app", RecordID: "APP-T-0001", ItemID: "APP-T-0001", LeaseState: string(LeaseStateRunning), LastHeartbeatAt: now.Format(time.RFC3339), LeaseExpiresAt: now.Add(time.Hour).Format(time.RFC3339)}
	if err := server.store.UpsertRun(run); err != nil {
		t.Fatal(err)
	}
	server.invalidateProjectRuntimeSnapshot("app")
	serveDecode(t, server, "/api/factory-operations?project=app", &factory)
	serveDecode(t, server, "/api/review/batch?project=app", &batch)
	if factory.Capacity.Project.Active != 1 || len(batch.Waves) != 2 || batch.Waves[0].ReadyForReview {
		t.Fatalf("warm reads missed runtime change: capacity=%#v batch=%#v", factory.Capacity.Project, batch)
	}
	if indexLoads != 0 || docsWalks != 0 || server.snapshots["app"].buildCount != 1 {
		t.Fatalf("runtime summaries rebuilt metadata: index=%d docs=%d builds=%d", indexLoads, docsWalks, server.snapshots["app"].buildCount)
	}
	oldFingerprint := factory.Authority.Waves[0].CurrentFingerprint
	// Spec text is not material (M5), but whether a cited spec resolves is,
	// and that changes without a metadata rebuild.
	if err := os.Remove(filepath.Join(server.repoRoot, "docs", "specs", "test-fixture.md")); err != nil {
		t.Fatal(err)
	}
	serveDecode(t, server, "/api/factory-operations?project=app", &factory)
	if factory.Authority.Waves[0].CurrentFingerprint == oldFingerprint {
		t.Fatal("request material cache retained old authorization across requests")
	}
}
