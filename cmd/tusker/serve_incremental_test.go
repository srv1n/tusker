package main

import (
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func serveIncrementalFixture(t *testing.T) (*serveServer, <-chan serveStreamEvent) {
	t.Helper()
	server := newServeEmptyNeedsFixture(t)
	if _, err := server.loadSnapshotForProject("app"); err != nil {
		t.Fatal(err)
	}
	events, unsubscribe, ok := server.stream.SubscribeProject("app")
	if !ok {
		t.Fatal("expected stream subscription")
	}
	t.Cleanup(unsubscribe)
	server.stream.setProjector(server.queueProjectionDiff) // what handleStream attaches
	// The first diff has no baseline and must announce every key.
	server.refreshProjectSnapshot("app")
	if first := nextServeIncrementalEvent(t, events); len(first.Keys) < 10 {
		t.Fatalf("baseline refresh should announce every key, got %v", first.Keys)
	}
	// Runtime-only topics (roster, agent messages) get their first baseline here.
	server.refreshProjectRuntimeSnapshot("app")
	nextServeIncrementalEvent(t, events)
	return server, events
}

func nextServeIncrementalEvent(t *testing.T, events <-chan serveStreamEvent) serveStreamEvent {
	t.Helper()
	select {
	case event := <-events:
		return event
	case <-time.After(5 * time.Second):
		t.Fatal("expected a stream event")
		return serveStreamEvent{}
	}
}

func expectNoServeIncrementalEvent(t *testing.T, events <-chan serveStreamEvent) {
	t.Helper()
	select {
	case event := <-events:
		t.Fatalf("unchanged refresh broadcast %s %v", event.Kind, event.Keys)
	case <-time.After(400 * time.Millisecond):
	}
}

func TestServeIncrementalUnchangedRefreshBroadcastsNothing(t *testing.T) {
	server, events := serveIncrementalFixture(t)
	server.refreshProjectSnapshot("app")
	expectNoServeIncrementalEvent(t, events)
	server.refreshProjectRuntimeSnapshot("app")
	expectNoServeIncrementalEvent(t, events)
}

func TestServeIncrementalTaskChangeBroadcastsOnlyAffectedKeys(t *testing.T) {
	server, events := serveIncrementalFixture(t)
	vault := filepath.Join(server.repoRoot, ".tusker")
	writeServeTask(t, vault, serveTaskSeed{ID: "APP-T-0001", Epic: "APP", Title: "Renamed ready task", Status: "ready", Risk: "medium", Priority: "p2"})
	server.refreshProjectSnapshot("app")
	event := nextServeIncrementalEvent(t, events)
	if event.Kind != "projection_refreshed" || event.Project != "app" || !slices.Contains(event.Keys, "tasks") {
		t.Fatalf("task change event = %#v", event)
	}
	// The task file is also a docs-list entry (title, updatedAt), so docs is affected.
	allowed := []string{"tasks", "docs", "needs", "projects", "review:batch", "factory-operations"}
	for _, key := range event.Keys {
		if !slices.Contains(allowed, key) {
			t.Fatalf("task change broadcast unrelated key %q in %v", key, event.Keys)
		}
	}
	// The rebuild's own content-change broadcast must not echo the keys again.
	expectNoServeIncrementalEvent(t, events)
}
