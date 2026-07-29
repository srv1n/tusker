package main

import (
	"path/filepath"
	"testing"
)

func timelineFixture(t *testing.T) (*RuntimeStore, ExecutionRecord) {
	t.Helper()
	store, root := providerObservationFixture(t, filepath.Join(t.TempDir(), "state"))
	t.Cleanup(func() { _ = store.Close() })
	return store, root
}

func TestExecutionTimelineCursor(t *testing.T) {
	store, root := timelineFixture(t)
	for i, id := range []string{"one", "two", "three"} {
		e := providerEvent(int64(i+1), id)
		e.ChildHandle = "" // root source provides an uncomplicated cursor walk.
		e.OccurredAt = "2026-07-29T12:00:0" + string(rune('0'+i)) + "Z"
		if _, err := store.ApplyProviderExecutionEvent(e); err != nil {
			t.Fatal(err)
		}
	}
	tail, err := store.ExecutionTimeline("project-1", root.ExecutionID, "", "tail", "", 2)
	if err != nil || len(tail.Rows) != 2 || !tail.HasOlder || tail.CommittedTail == "" || tail.NextCursor == "" {
		t.Fatalf("tail=%#v err=%v", tail, err)
	}
	after, err := store.ExecutionTimeline("project-1", root.ExecutionID, "", "after", tail.Rows[0].Cursor, 10)
	if err != nil || len(after.Rows) != 1 || after.Rows[0].SourceSequence != 3 {
		t.Fatalf("after=%#v err=%v", after, err)
	}
}

func TestExecutionTimelineRecovery(t *testing.T) {
	store, root := timelineFixture(t)
	e := providerEvent(1, "one")
	e.ChildHandle = ""
	if _, err := store.ApplyProviderExecutionEvent(e); err != nil {
		t.Fatal(err)
	}
	stale, err := store.ExecutionTimeline("project-1", root.ExecutionID, "", "after", "bad-cursor", 10)
	if err != nil || !stale.StaleCursor || !stale.Reset || stale.Recovery != "fetch_tail" {
		t.Fatalf("stale=%#v err=%v", stale, err)
	}
	page, err := store.ExecutionTimeline("project-1", root.ExecutionID, "", "tail", "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.exec(`UPDATE execution_timeline_sources SET epoch='replaced-epoch' WHERE execution_id=?`, root.ExecutionID); err != nil {
		t.Fatal(err)
	}
	replaced, err := store.ExecutionTimeline("project-1", root.ExecutionID, "", "after", page.Rows[0].Cursor, 10)
	if err != nil || !replaced.StaleCursor || !replaced.Reset {
		t.Fatalf("replacement=%#v err=%v", replaced, err)
	}
}

func TestExecutionTimelineProjection(t *testing.T) {
	store, root := timelineFixture(t)
	first := providerEvent(1, "parent")
	first.ChildHandle = ""
	if _, err := store.ApplyProviderExecutionEvent(first); err != nil {
		t.Fatal(err)
	}
	child := providerEvent(1, "child")
	child.ChildHandle = "child-1"
	child.OccurredAt = "2026-07-29T12:00:01Z"
	if _, err := store.ApplyProviderExecutionEvent(child); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ApplyProviderExecutionEvent(child); err != nil {
		t.Fatal(err)
	} // replay is not a second timeline row.
	page, err := store.ExecutionTimeline("project-1", root.ExecutionID, "", "tail", "", 10)
	if err != nil || len(page.Rows) != 2 {
		t.Fatalf("projection=%#v err=%v", page, err)
	}
	if page.Rows[0].SourceExecutionID == page.Rows[1].SourceExecutionID || !page.Rows[0].Authoritative || page.Rows[0].Provenance != "authoritative" {
		t.Fatalf("rows lost source/provenance: %#v", page.Rows)
	}
}

func TestExecutionTimelineVectorCursorConvergesAcrossSources(t *testing.T) {
	store, root := timelineFixture(t)
	parent := providerEvent(9, "parent-9")
	parent.ChildHandle = ""
	parent.OccurredAt = "2026-07-29T12:00:00Z"
	if _, err := store.ApplyProviderExecutionEvent(parent); err != nil {
		t.Fatal(err)
	}
	child := providerEvent(9, "child-9")
	child.ChildHandle, child.OccurredAt = "child-vector", "2026-07-29T12:00:01Z"
	if _, err := store.ApplyProviderExecutionEvent(child); err != nil {
		t.Fatal(err)
	}
	checkpoint, err := store.ExecutionTimeline("project-1", root.ExecutionID, "", "tail", "", 10)
	if err != nil || len(checkpoint.Rows) != 2 {
		t.Fatalf("checkpoint=%#v err=%v", checkpoint, err)
	}
	// The source sequence regresses, but accepted late provider facts receive a
	// new local sequence and are discoverable from the vector checkpoint.
	lateParent := providerEvent(1, "parent-late")
	lateParent.ChildHandle = ""
	lateParent.OccurredAt = "2026-07-29T12:00:02Z"
	lateChild := providerEvent(1, "child-late")
	lateChild.ChildHandle = "child-vector"
	lateChild.OccurredAt = "2026-07-29T12:00:03Z"
	if _, err := store.ApplyProviderExecutionEvent(lateParent); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ApplyProviderExecutionEvent(lateChild); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ApplyProviderExecutionEvent(lateChild); err != nil {
		t.Fatal(err)
	} // duplicate notification/event.
	after, err := store.ExecutionTimeline("project-1", root.ExecutionID, "", "after", checkpoint.NextCursor, 1)
	if err != nil || len(after.Rows) != 1 || !after.HasNewer {
		t.Fatalf("limited vector after=%#v err=%v", after, err)
	}
	after, err = store.ExecutionTimeline("project-1", root.ExecutionID, "", "after", after.NextCursor, 10)
	if err != nil || len(after.Rows) != 1 || after.CommittedTail == checkpoint.CommittedTail {
		t.Fatalf("vector after=%#v err=%v", after, err)
	}
	// A checkpoint returned by authoritative fetch has reached every current
	// source tail. Losing every stream notification changes nothing.
	settled, err := store.ExecutionTimeline("project-1", root.ExecutionID, "", "after", after.NextCursor, 10)
	if err != nil || len(settled.Rows) != 0 || settled.CommittedTail != after.NextCursor {
		t.Fatalf("settled=%#v err=%v", settled, err)
	}
	stateRoot := store.stateRoot
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	restarted, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	// Restart never rebuilds a checkpoint from stream memory: the same durable
	// vector still proves the committed tail.
	afterRestart, err := restarted.ExecutionTimeline("project-1", root.ExecutionID, "", "after", after.NextCursor, 10)
	if err != nil || len(afterRestart.Rows) != 0 || afterRestart.CommittedTail != after.NextCursor {
		t.Fatalf("restart=%#v err=%v", afterRestart, err)
	}
}

func TestExecutionTimelineFanInTailBeforePaging(t *testing.T) {
	store, root := timelineFixture(t)
	for i := 0; i < 4; i++ {
		e := providerEvent(int64(i+1), "fan-in-"+string(rune('a'+i)))
		if i%2 == 0 {
			e.ChildHandle = ""
		} else {
			e.ChildHandle = "fan-in-child"
		}
		e.OccurredAt = "2026-07-29T12:00:0" + string(rune('0'+i)) + "Z"
		if _, err := store.ApplyProviderExecutionEvent(e); err != nil {
			t.Fatal(err)
		}
	}
	tail, err := store.ExecutionTimeline("project-1", root.ExecutionID, "", "tail", "", 2)
	if err != nil || len(tail.Rows) != 2 || !tail.HasOlder {
		t.Fatalf("tail=%#v err=%v", tail, err)
	}
	if tail.Rows[0].Cursor == "" {
		t.Fatal("returned fan-in row cursor is empty")
	}
	rowResume, err := store.ExecutionTimeline("project-1", root.ExecutionID, "", "after", tail.Rows[0].Cursor, 10)
	if err != nil || rowResume.Reset || len(rowResume.Rows) != 1 || rowResume.Rows[0].ObservationID != tail.Rows[1].ObservationID {
		// This proves the checkpoint resumes at the next global row: no replay,
		// no skipped peer-source event, and no stale-vector reset.
		t.Fatalf("row cursor resume=%#v err=%v", rowResume, err)
	}
	previous, err := store.ExecutionTimeline("project-1", root.ExecutionID, "", "before", tail.PreviousCursor, 2)
	if err != nil || len(previous.Rows) != 2 || previous.Rows[1].OccurredAt >= tail.Rows[0].OccurredAt {
		t.Fatalf("before=%#v err=%v", previous, err)
	}
	if previous.HasOlder {
		t.Fatalf("unexpected extra older page: %#v", previous)
	}
	if !previous.HasNewer {
		t.Fatalf("before must expose known newer rows: %#v", previous)
	}
}

func TestExecutionTimelineGapNewSourceAndRestartRecovery(t *testing.T) {
	store, root := timelineFixture(t)
	first := providerEvent(1, "retained-1")
	first.ChildHandle = ""
	if _, err := store.ApplyProviderExecutionEvent(first); err != nil {
		t.Fatal(err)
	}
	page, err := store.ExecutionTimeline("project-1", root.ExecutionID, "", "tail", "", 10)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate retention/compaction: a checkpoint below the retained floor must
	// ask the client to recover rather than emitting a misleading empty page.
	if _, err := store.exec(`DELETE FROM execution_timeline_events WHERE execution_id=?`, root.ExecutionID); err != nil {
		t.Fatal(err)
	}
	second := providerEvent(2, "retained-2")
	second.ChildHandle = ""
	if _, err := store.ApplyProviderExecutionEvent(second); err != nil {
		t.Fatal(err)
	}
	retainedCursor, err := decodeExecutionTimelineCursor(page.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	for id, source := range retainedCursor.Sources {
		source.Sequence = 0
		retainedCursor.Sources[id] = source
	}
	gap, err := store.ExecutionTimeline("project-1", root.ExecutionID, "", "after", encodeExecutionTimelineCursor(retainedCursor), 10)
	if err != nil || !gap.Gap || !gap.Reset || gap.Recovery != "fetch_tail" {
		t.Fatalf("gap=%#v err=%v", gap, err)
	}
	// A new source after a vector checkpoint is likewise explicit; it cannot be
	// silently omitted from a claimed-converged projection.
	fresh, err := store.ExecutionTimeline("project-1", root.ExecutionID, "", "tail", "", 10)
	if err != nil {
		t.Fatal(err)
	}
	child := providerEvent(1, "new-source")
	child.ChildHandle = "new-child"
	if _, err := store.ApplyProviderExecutionEvent(child); err != nil {
		t.Fatal(err)
	}
	newSource, err := store.ExecutionTimeline("project-1", root.ExecutionID, "", "after", fresh.NextCursor, 10)
	if err != nil || !newSource.Reset || !newSource.Gap {
		t.Fatalf("new source=%#v err=%v", newSource, err)
	}
}
