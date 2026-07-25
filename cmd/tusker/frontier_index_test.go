package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sync/atomic"
	"testing"
	"time"
)

func frontierTestNote(id, kind string, fields map[string]any) Note {
	data := map[string]any{"id": id, "kind": kind}
	for key, value := range fields {
		data[key] = value
	}
	return Note{RelativePath: "work/" + id + ".md", Data: data}
}

func TestIncrementalFrontierIndexEquivalenceAcrossTransitions(t *testing.T) {
	notes := []Note{
		frontierTestNote("W-1", "wave", map[string]any{"authorization": "armed", "members": []any{"A", "B", "C"}}),
		frontierTestNote("A", "task", map[string]any{"status": "review", "proof_status": "satisfied", "wave": "W-1"}),
		frontierTestNote("B", "task", map[string]any{"status": "ready", "dependencies": []any{"A:soft"}, "wave": "W-1"}),
		frontierTestNote("C", "task", map[string]any{"status": "ready", "dependencies": []any{"B:hard"}, "wave": "W-1"}),
	}
	warm := newProjectFrontierIndex("app")
	warm.rebuild(notes)
	if !reflect.DeepEqual(warm.Frontier, []string{"B"}) {
		t.Fatalf("soft frontier = %v, want B", warm.Frontier)
	}

	// A rework relocks B, and the reverse closure relocks C as well.
	changed := frontierTestNote("A", "task", map[string]any{"status": "backlog", "proof_status": "", "wave": "W-1"})
	counters := warm.apply([]Note{changed})
	if counters.GraphRecomputed != 3 || len(warm.Frontier) != 0 {
		t.Fatalf("incremental closure = %#v frontier=%v", counters, warm.Frontier)
	}

	cold := newProjectFrontierIndex("app")
	cold.rebuild([]Note{notes[0], changed, notes[2], notes[3]})
	if !reflect.DeepEqual(warm.Eligibility, cold.Eligibility) || !reflect.DeepEqual(warm.Frontier, cold.Frontier) {
		t.Fatalf("incremental and cold projections differ:\nincremental=%#v/%v\ncold=%#v/%v", warm.Eligibility, warm.Frontier, cold.Eligibility, cold.Frontier)
	}

	// Gate, stale authorization, and terminal states all remain in the same
	// projection semantics after an incremental replacement.
	warm.apply([]Note{frontierTestNote("W-1", "wave", map[string]any{"authorization": "stale"})})
	if len(warm.Frontier) != 0 {
		t.Fatalf("stale wave frontier = %v", warm.Frontier)
	}
	warm.apply([]Note{frontierTestNote("W-1", "wave", map[string]any{"authorization": "armed", "members": []any{"A", "B", "C"}}), frontierTestNote("A", "task", map[string]any{"status": "done", "wave": "W-1"}), frontierTestNote("B", "task", map[string]any{"status": "done", "wave": "W-1", "dependencies": []any{"A:soft"}})})
	if !reflect.DeepEqual(warm.Frontier, []string{"C"}) {
		t.Fatalf("terminal transition frontier = %v, want C", warm.Frontier)
	}

	gate := frontierTestNote("G-1", "gate", map[string]any{"status": "open", "blocking": true, "owner": "agent", "blocks": []any{"C"}})
	warm.apply([]Note{gate})
	cold.rebuild(append(warm.notes()[:0:0], warm.notes()...))
	if !reflect.DeepEqual(warm.Eligibility, cold.Eligibility) || len(warm.Frontier) != 0 {
		t.Fatalf("blocking gate projection differs: incremental=%#v/%v cold=%#v/%v", warm.Eligibility, warm.Frontier, cold.Eligibility, cold.Frontier)
	}

	gate.Data["status"] = "satisfied"
	warm.apply([]Note{gate})
	if !reflect.DeepEqual(warm.Frontier, []string{"C"}) {
		t.Fatalf("satisfied gate frontier = %v, want C", warm.Frontier)
	}
}

func TestIncrementalFrontierIndexRecomputesOldAndNewWaveAndGateClosures(t *testing.T) {
	notes := []Note{
		frontierTestNote("W-1", "wave", map[string]any{"authorization": "armed", "members": []any{"A", "B"}}),
		frontierTestNote("W-2", "wave", map[string]any{"authorization": "armed", "members": []any{"C"}}),
		frontierTestNote("A", "task", map[string]any{"status": "ready", "wave": "W-1"}),
		frontierTestNote("B", "task", map[string]any{"status": "ready", "wave": "W-1"}),
		frontierTestNote("C", "task", map[string]any{"status": "ready", "wave": "W-2"}),
		frontierTestNote("G-1", "gate", map[string]any{"status": "open", "blocking": true, "blocks": []any{"A"}}),
	}
	warm := newProjectFrontierIndex("app")
	warm.rebuild(notes)
	if !reflect.DeepEqual(warm.Frontier, []string{"B", "C"}) {
		t.Fatalf("initial frontier = %v", warm.Frontier)
	}

	changedWave := frontierTestNote("W-1", "wave", map[string]any{"authorization": "disarmed", "members": []any{"A", "B"}})
	retargetedGate := frontierTestNote("G-1", "gate", map[string]any{"status": "open", "blocking": true, "blocks": []any{"C"}})
	counters := warm.apply([]Note{changedWave, retargetedGate})
	if counters.GraphRecomputed != 3 {
		t.Fatalf("closure recomputed %d tasks, want 3", counters.GraphRecomputed)
	}
	if len(warm.Frontier) != 0 {
		t.Fatalf("retargeted closure frontier = %v, want empty", warm.Frontier)
	}

	cold := newProjectFrontierIndex("app")
	cold.rebuild([]Note{changedWave, notes[1], notes[2], notes[3], notes[4], retargetedGate})
	if !reflect.DeepEqual(warm.Eligibility, cold.Eligibility) || !reflect.DeepEqual(warm.Frontier, cold.Frontier) {
		t.Fatalf("incremental and cold closure projections differ:\nincremental=%#v/%v\ncold=%#v/%v", warm.Eligibility, warm.Frontier, cold.Eligibility, cold.Frontier)
	}
}

func TestIncrementalFrontierIndexTaskOwnedProofRecordTouchesTaskClosure(t *testing.T) {
	notes := []Note{
		frontierTestNote("A", "task", map[string]any{"status": "ready"}),
		frontierTestNote("B", "task", map[string]any{"status": "ready", "dependencies": []any{"A:hard"}}),
		frontierTestNote("A-E-0001", "evidence", map[string]any{"task": "A", "status": "proposed"}),
	}
	index := newProjectFrontierIndex("app")
	index.rebuild(notes)
	changedEvidence := frontierTestNote("A-E-0001", "evidence", map[string]any{"task": "A", "status": "accepted"})
	counters := index.apply([]Note{changedEvidence})
	if counters.GraphRecomputed != 2 {
		t.Fatalf("task-owned proof closure recomputed %d tasks, want 2", counters.GraphRecomputed)
	}
}

func TestIncrementalFrontierIndexBoundedTenThousandTaskMutation(t *testing.T) {
	notes := make([]Note, 0, 10000)
	for i := 0; i < 10000; i++ {
		id := fmt.Sprintf("T-%05d", i)
		fields := map[string]any{"status": "backlog"}
		if i == 1 {
			fields = map[string]any{"status": "ready", "dependencies": []any{"T-00000:hard"}}
		}
		notes = append(notes, frontierTestNote(id, "task", fields))
	}
	index := newProjectFrontierIndex("alpha")
	index.rebuild(notes)
	counters := index.apply([]Note{frontierTestNote("T-00000", "task", map[string]any{"status": "done"})})
	if counters.RecordsRead != 1 || counters.RecordsParsed != 1 || counters.GraphRecomputed != 2 {
		t.Fatalf("warm mutation touched too much: %#v", counters)
	}
	if !reflect.DeepEqual(index.Frontier, []string{"T-00001"}) {
		t.Fatalf("frontier = %v, want T-00001", index.Frontier)
	}
}

func TestIncrementalFrontierIndexRichControlNotificationRemainsOptional(t *testing.T) {
	stateRoot, err := os.MkdirTemp("/tmp", "tusker-frontier-notify-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(stateRoot) })
	t.Setenv("TUSKER_STATE_ROOT", stateRoot)
	vault := pickupV7TestVault(t)
	projectID, err := resolveV7ProjectID(vault)
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

	beginCLIVaultMutationTracking()
	path := filepath.Join(vault, "work", "tasks", "APP-T-0001.md")
	if err := os.WriteFile(path, []byte("---\nid: APP-T-0001\nkind: task\nstate_rev: r2\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	recordCLIVaultMutation(path)
	finishCLIVaultMutationTracking()
	notifyDaemonForVaultPath(vault)
	select {
	case req := <-received:
		if req.ProjectID != projectID || req.Cause != "cli_mutation" || len(req.Changes) != 1 || req.Changes[0].ID != "APP-T-0001" {
			t.Fatalf("rich notification = %#v", req)
		}
	case <-time.After(time.Second):
		t.Fatal("missing notification")
	}
}

func TestIncrementalFrontierIndexDaemonWarmHintAndFallback(t *testing.T) {
	vaultA, vaultB := pickupV7TestVault(t), pickupV7TestVault(t)
	mustRunPickupTest(t, Args{"vault": vaultA, "quiet": "true", "epic": "APP", "title": "Frontier target", "risk": "low", "priority": "p0", "v7": "true"}, newV7Task)
	notes, err := listOperationalNotes(vaultA)
	if err != nil {
		t.Fatal(err)
	}
	d := &Daemon{frontiers: map[string]*projectFrontierIndex{}, frontierHints: map[string][]daemonControlChange{}}
	d.rebuildFrontier("alpha", notes)
	var reads, parses atomic.Int64
	noteCacheReadObserver = func() { reads.Add(1) }
	noteCacheParseObserver = func() { parses.Add(1) }
	t.Cleanup(func() { noteCacheReadObserver, noteCacheParseObserver = nil, nil })

	path, taskID := "", ""
	for _, note := range notes {
		if effectiveV7Kind(note.Data) == "task" && stringField(note.Data, "id") != "" {
			path, taskID = note.AbsolutePath, stringField(note.Data, "id")
			break
		}
	}
	if path == "" || taskID == "" {
		t.Fatal("task fixture path missing")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	} // changes stat version without changing semantics
	invalidateCachedNote(path)
	project := RegisteredProject{ProjectID: "alpha", VaultRoot: vaultA}
	updated, ok := d.applyFrontierHint(project, []daemonControlChange{{ID: taskID, Kind: "task"}})
	if !ok || len(updated) != len(notes) || reads.Load() != 1 || parses.Load() != 1 {
		t.Fatalf("warm target update ok=%v notes=%d reads=%d parses=%d", ok, len(updated), reads.Load(), parses.Load())
	}
	updated, ok = d.applyFrontierHint(project, []daemonControlChange{{ID: taskID, Kind: "run", Revision: "lease:2", Eligibility: []string{"runtime", "ownership"}}})
	if !ok || len(updated) != len(notes) || reads.Load() != 1 || parses.Load() != 1 {
		t.Fatalf("runtime-only target touch parsed Markdown: ok=%v notes=%d reads=%d parses=%d", ok, len(updated), reads.Load(), parses.Load())
	}
	if _, ok := d.applyFrontierHint(project, []daemonControlChange{{ID: "APP-T-UNKNOWN", Kind: "run", Revision: "lease:1"}}); ok {
		t.Fatal("unknown runtime task must use adaptive fallback")
	}
	if writeNotifyNoteCacheExists(vaultB) || reads.Load() != 1 || parses.Load() != 1 {
		t.Fatalf("unrelated project was read by target hint: reads=%d parses=%d", reads.Load(), parses.Load())
	}
	if _, ok := d.applyFrontierHint(project, []daemonControlChange{{ID: taskID, Revision: "wrong"}}); ok {
		t.Fatal("revision mismatch must use adaptive fallback")
	}
	d.clearFrontiers()
	if _, ok := d.applyFrontierHint(project, []daemonControlChange{{ID: taskID}}); ok {
		t.Fatal("restart without projection must cold rebuild")
	}
}
