package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestServeRefreshTargetsProjectAndCollapsesRapidRequests(t *testing.T) {
	server := newServeEmptyNeedsFixture(t)
	if _, err := server.loadSnapshotForProject("app"); err != nil {
		t.Fatal(err)
	}
	streamEvents, unsubscribe, ok := server.stream.SubscribeProject("app")
	if !ok {
		t.Fatal("expected scoped stream subscription")
	}
	defer unsubscribe()
	stateRoot, err := os.MkdirTemp("/tmp", "tusker-refresh-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(stateRoot) })
	t.Setenv("TUSKER_STATE_ROOT", stateRoot)
	daemon := &Daemon{
		stateRoot: stateRoot, store: server.store, serve: server, stream: server.stream,
		notifyWake: make(chan string, 8),
	}
	defer daemon.stopNotifyTimers()
	received := make(chan daemonControlRequest, 4)
	control, err := startDaemonControlServer(stateRoot, func(_ context.Context, req daemonControlRequest) daemonControlResponse {
		received <- req
		daemon.scheduleProjectReconcile(req.ProjectID)
		return daemonControlResponse{OK: true}
	})
	if err != nil {
		t.Fatal(err)
	}
	defer control.Close()

	postRefresh := func() serveActionResult {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:7420/api/projects/app/refresh", nil)
		rec := httptest.NewRecorder()
		server.ServeHTTP(rec, req)
		if rec.Code != http.StatusAccepted {
			t.Fatalf("refresh returned %d: %s", rec.Code, rec.Body.String())
		}
		var result serveActionResult
		if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	first := postRefresh()
	second := postRefresh()
	if !first.OK || first.ProjectID != "app" || !second.OK || second.ProjectID != "app" {
		t.Fatalf("unexpected refresh responses: first=%#v second=%#v", first, second)
	}
	if !strings.Contains(second.Reason, "coalesced") {
		t.Fatalf("rapid refresh did not report coalescing: %#v", second)
	}
	select {
	case req := <-received:
		if req.Command != "reconcile_project" || req.ProjectID != "app" {
			t.Fatalf("refresh used wrong control path: %#v", req)
		}
	case <-time.After(time.Second):
		t.Fatal("refresh did not reach targeted reconcile control path")
	}
	select {
	case duplicate := <-received:
		t.Fatalf("rapid refresh escaped server coalescing: %#v", duplicate)
	case <-time.After(250 * time.Millisecond):
	}
	writeServeTask(t, server.vaultPath, serveTaskSeed{ID: "APP-T-0002", Epic: "APP", Title: "Out-of-band refresh", Status: "backlog", Risk: "medium", Priority: "p2"})
	select {
	case projectID := <-daemon.notifyWake:
		if projectID != "app" {
			t.Fatalf("debounced refresh targeted %q", projectID)
		}
		if err := daemon.PollProjectOnce(context.Background(), projectID); err != nil {
			t.Fatal(err)
		}
		attempts, err := daemon.store.ListAttemptsForRun("app", "APP-T-0001")
		if err != nil {
			t.Fatal(err)
		}
		if len(attempts) != 0 {
			t.Fatalf("manual refresh dispatched default-off project work: %#v", attempts)
		}
	case <-time.After(time.Second):
		t.Fatal("manual refresh did not enter daemon reconcile loop")
	}
	select {
	case event := <-streamEvents:
		if event.Kind != "projection_refreshed" || event.Project != "app" {
			t.Fatalf("manual refresh emitted unscoped event: %#v", event)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("manual refresh was not visible on scoped stream within 2s")
	}
	snap, err := server.loadSnapshotForProject("app")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := snap.notesByID["APP-T-0002"]; !ok {
		t.Fatal("manual refresh snapshot did not include out-of-band task")
	}
}

func TestServeRefreshRejectsMissingDaemonWithoutMutatingSnapshot(t *testing.T) {
	server := newServeEmptyNeedsFixture(t)
	stateRoot, err := os.MkdirTemp("/tmp", "tusker-refresh-down-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(stateRoot) })
	t.Setenv("TUSKER_STATE_ROOT", stateRoot)
	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:7420/api/projects/app/refresh", nil)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("daemon-down refresh returned %d: %s", rec.Code, rec.Body.String())
	}
}
