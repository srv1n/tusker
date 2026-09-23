package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestServeWaveListTruth(t *testing.T) {
	t.Run("effective authorization, task counts, and live member", func(t *testing.T) {
		task := Note{Data: map[string]any{"kind": "task", "id": "APP-T-1", "status": "ready"}}
		wave := Note{Data: map[string]any{"kind": "wave", "id": "W-1", "status": "open", "authorization": "armed", "authorization_fingerprint": "superseded", "members": []string{"APP-T-1", "docs/spec.md"}}}
		snap := serveSnapshot{waves: []Note{wave}, tasks: []Note{task}, notesByID: map[string]Note{"W-1": wave, "APP-T-1": task}, runs: []RunStatus{{ItemID: "APP-T-1", LeaseState: string(LeaseStateRunning)}}}
		list := serveWaveList(snap)
		if len(list) != 1 || list[0].Authorization != "stale" || list[0].MemberCount != 1 || list[0].DoneCount != 0 || !list[0].LiveRun {
			t.Fatalf("list=%#v", list)
		}
		task.Data["status"] = "done"
		snap.notesByID["APP-T-1"] = task
		snap.tasks[0] = task
		list = serveWaveList(snap)
		if list[0].DoneCount != 1 || list[0].LiveRun {
			t.Fatalf("done member appears live: %#v", list[0])
		}
	})

	t.Run("unknown view is rejected", func(t *testing.T) {
		server := newServeEmptyNeedsFixture(t)
		rec := httptest.NewRecorder()
		server.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/waves?view=bogus", nil))
		if rec.Code != http.StatusBadRequest || rec.Body.String() != "{\"error\":\"unknown view\"}\n" {
			t.Fatalf("status=%d body=%q", rec.Code, rec.Body.String())
		}
	})

	t.Run("superseded directive and unrelated escalation stay off wave", func(t *testing.T) {
		store := fairDispatchTestStore(t)
		now := time.Now().UTC()
		if err := store.SetSetting("daemon_last_poll_at", now.Format(time.RFC3339)); err != nil {
			t.Fatal(err)
		}
		snap := selfServiceServeSnapshot("project-srv", true)
		wave := snap.waves[0]
		wave.Data["authorization_fingerprint"] = "current"
		wave.Data["authorized_at"] = "2026-09-23T00:00:00Z"
		snap.waves[0], snap.notesByID["W-SRV"] = wave, wave
		if _, err := store.QueueRunDirective(RunDirective{ProjectID: "project-srv", RecordID: "S-T-0001", Actor: "human:test", CreatedAt: now.Format(time.RFC3339Nano), ExpiresAt: now.Add(time.Hour).Format(time.RFC3339Nano), WaveID: "W-SRV", AuthorizationFingerprint: "old", WaveAuthorizedAt: "2026-09-22T00:00:00Z"}); err != nil {
			t.Fatal(err)
		}
		server := &serveServer{store: store}
		recovery := server.attachWaveListRecovery(serveWaveList(snap), snap)[0].Recovery
		if recovery == nil || recovery.Queued {
			t.Fatalf("superseded directive queued wave: %#v", recovery)
		}
		currentStore := fairDispatchTestStore(t)
		if _, err := currentStore.QueueRunDirective(RunDirective{ProjectID: "project-srv", RecordID: "S-T-0001", Actor: "human:test", CreatedAt: now.Format(time.RFC3339Nano), ExpiresAt: now.Add(time.Hour).Format(time.RFC3339Nano), WaveID: "W-SRV", AuthorizationFingerprint: "current", WaveAuthorizedAt: "2026-09-23T00:00:00Z"}); err != nil {
			t.Fatal(err)
		}
		current := (&serveServer{store: currentStore}).attachWaveListRecovery(serveWaveList(snap), snap)[0].Recovery
		if current == nil || !current.Queued {
			t.Fatalf("current directive did not queue wave: %#v", current)
		}
		rctx := serveRecoveryContext{escalations: []SelfServiceRepairEscalation{{RecordID: "S-T-0001"}, {RecordID: "OTHER"}}}
		idx := serveSnapshotIndex(snap)
		waveRecovery := server.recoveryForWave(wave, snap, idx, nil, rctx)
		taskRecovery := server.recoveryForTask(snap.tasks[0], snap, idx, nil, map[string]string{"W-SRV": "armed"}, rctx)
		if len(waveRecovery.Escalations) != 1 || len(taskRecovery.Escalations) != 1 || waveRecovery.Escalations[0].RecordID != "S-T-0001" {
			t.Fatalf("escalations wave=%#v task=%#v", waveRecovery.Escalations, taskRecovery.Escalations)
		}
	})
}
