package main

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestSelfServiceServeDiagnosis proves TSK-T-0045 (server half): wave
// list/detail and task surfaces share one recovery diagnosis with the
// actual blocking cause, queued directives never read as running, safe
// repair renders as unsupported by server capability, and settled task
// reads carry the same projection as post-action refreshes.
func TestSelfServiceServeDiagnosis(t *testing.T) {
	t.Run("A1/armed_queued_wave_shows_cause", func(t *testing.T) {
		store := fairDispatchTestStore(t)
		now := time.Now().UTC()
		if err := store.SetSetting("daemon_last_poll_at", now.Format(time.RFC3339)); err != nil {
			t.Fatal(err)
		}
		if _, err := store.QueueRunDirective(RunDirective{ProjectID: "project-srv", RecordID: "S-T-0001", Actor: "human:test", CreatedAt: now.Format(time.RFC3339Nano), ExpiresAt: now.Add(time.Hour).Format(time.RFC3339Nano)}); err != nil {
			t.Fatal(err)
		}
		snap := selfServiceServeSnapshot("project-srv", true)
		server := &serveServer{store: store}
		summaries := server.attachWaveRecovery(serveWaves(snap), snap)
		if len(summaries) != 1 {
			t.Fatalf("expected one wave summary, got %d", len(summaries))
		}
		recovery := summaries[0].Recovery
		if recovery == nil {
			t.Fatal("wave detail lost its recovery diagnosis")
		}
		if recovery.Authorization != "armed" || !recovery.Queued {
			t.Fatalf("armed+queued wave misrendered: %#v", recovery)
		}
		if recovery.CauseCode != "queued" || !strings.HasPrefix(recovery.BlockingCause, "Queued.") {
			t.Fatalf("queued wave hid its cause: %#v", recovery)
		}
		if recovery.NextActor != string(DiagnosticAuthorityDaemon) {
			t.Fatalf("queued wait named the wrong actor: %#v", recovery)
		}
		list := server.attachWaveListRecovery(serveWaveList(snap), snap)
		if len(list) != 1 || list[0].Recovery == nil || list[0].Recovery.CauseCode != "queued" {
			t.Fatalf("wave list lost the queued cause: %#v", list)
		}
		capsules := server.attachTaskRecovery([]serveTaskCapsule{serveTaskCapsuleFor(snap, snap.tasks[0])}, snap)
		taskRecovery := capsules[0].Recovery
		if taskRecovery == nil || taskRecovery.CauseCode != "queued" {
			t.Fatalf("task surface lost the queued cause: %#v", taskRecovery)
		}
		if capsules[0].Status == "in_progress" || capsules[0].Status == "running" {
			t.Fatalf("queued directive inferred running: %#v", capsules[0])
		}
	})

	t.Run("A1/project_off_daemon_stale_capacity_stay_distinct", func(t *testing.T) {
		store := fairDispatchTestStore(t)
		now := time.Now().UTC()
		server := &serveServer{store: store}
		// Project off.
		snap := selfServiceServeSnapshot("project-srv", false)
		recovery := server.attachWaveRecovery(serveWaves(snap), snap)[0].Recovery
		if recovery.CauseCode != "project_disabled" || recovery.NextActor != string(DiagnosticAuthorityOperator) {
			t.Fatalf("project-off misrendered: %#v", recovery)
		}
		// Daemon unavailable: no poll ever recorded, work waiting.
		snap = selfServiceServeSnapshot("project-srv", true)
		recovery = server.attachWaveRecovery(serveWaves(snap), snap)[0].Recovery
		if recovery.CauseCode != "doctor-daemon-absent" {
			t.Fatalf("absent daemon misrendered: %#v", recovery)
		}
		// Daemon stale: old poll recorded, work waiting.
		if err := store.SetSetting("daemon_last_poll_at", now.Add(-time.Hour).Format(time.RFC3339)); err != nil {
			t.Fatal(err)
		}
		recovery = server.attachWaveRecovery(serveWaves(snap), snap)[0].Recovery
		if recovery.CauseCode != "doctor-daemon-stale" || !strings.Contains(recovery.BlockingCause, "last recorded poll") {
			t.Fatalf("stale daemon misrendered: %#v", recovery)
		}
		// Capacity owner: a live run holds the slot while a sibling waits.
		if err := store.SetSetting("daemon_last_poll_at", now.Format(time.RFC3339)); err != nil {
			t.Fatal(err)
		}
		snap = selfServiceServeSnapshot("project-srv", true)
		snap.runs = append(snap.runs, RunStatus{
			ProjectID: "project-srv", RecordID: "S-T-0001", ItemID: "S-T-0001",
			LeaseState: string(LeaseStateRunning), LeaseOwner: "attempt-live", LeaseGeneration: 1,
			LeaseExpiresAt: now.Add(time.Hour).Format(time.RFC3339),
		})
		recovery = server.attachWaveRecovery(serveWaves(snap), snap)[0].Recovery
		if recovery.CauseCode != "doctor-project-capacity" || !strings.Contains(recovery.BlockingCause, "S-T-0001") {
			t.Fatalf("capacity wait hid its owner: %#v", recovery)
		}
		if recovery.NextActor != string(DiagnosticAuthorityDaemon) {
			t.Fatalf("capacity wait named the wrong actor: %#v", recovery)
		}
	})

	t.Run("A2/safe_repair_capability_reflects_server", func(t *testing.T) {
		store := fairDispatchTestStore(t)
		now := time.Now().UTC()
		if err := store.SetSetting("daemon_last_poll_at", now.Format(time.RFC3339)); err != nil {
			t.Fatal(err)
		}
		snap := selfServiceServeSnapshot("project-srv", true)
		server := &serveServer{store: store}
		for _, recovery := range []*serveRecovery{
			server.attachWaveRecovery(serveWaves(snap), snap)[0].Recovery,
			server.attachWaveListRecovery(serveWaveList(snap), snap)[0].Recovery,
			server.attachTaskRecovery([]serveTaskCapsule{serveTaskCapsuleFor(snap, snap.tasks[0])}, snap)[0].Recovery,
		} {
			if recovery == nil {
				t.Fatal("surface lost its recovery projection")
			}
			if recovery.Capabilities.SafeRepair || strings.TrimSpace(recovery.Capabilities.SafeRepairReason) == "" {
				t.Fatalf("server overstated repair capability: %#v", recovery.Capabilities)
			}
		}
	})

	t.Run("A3/settled_reads_match_post_action_readback", func(t *testing.T) {
		store := fairDispatchTestStore(t)
		now := time.Now().UTC()
		if err := store.SetSetting("daemon_last_poll_at", now.Format(time.RFC3339)); err != nil {
			t.Fatal(err)
		}
		if _, err := store.QueueRunDirective(RunDirective{ProjectID: "project-srv", RecordID: "S-T-0001", Actor: "human:test", CreatedAt: now.Format(time.RFC3339Nano), ExpiresAt: now.Add(time.Hour).Format(time.RFC3339Nano)}); err != nil {
			t.Fatal(err)
		}
		snap := selfServiceServeSnapshot("project-srv", true)
		server := &serveServer{store: store}
		capsule := server.attachTaskRecovery([]serveTaskCapsule{serveTaskCapsuleFor(snap, snap.tasks[0])}, snap)[0]
		detail := server.taskDetailFor(snap, snap.tasks[0])
		if detail.Recovery == nil || capsule.Recovery == nil {
			t.Fatal("task read lost its recovery projection")
		}
		if detail.Recovery.CauseCode != capsule.Recovery.CauseCode || detail.Recovery.BlockingCause != capsule.Recovery.BlockingCause {
			t.Fatalf("settled read disagrees with list read: detail=%#v capsule=%#v", detail.Recovery, capsule.Recovery)
		}
	})

	t.Run("A4/background_scope_read_arms_nothing", func(t *testing.T) {
		vault, store, project := authorityFixture(t)
		writeDirectTask(t, vault, "SC-T-0001", "W-SC", nil)
		writeDirectWave(t, vault, "W-SC", []string{"SC-T-0001"}, nil)
		if _, err := directWaveStart(vault, store, "W-SC", "human:test"); err != nil {
			t.Fatal(err)
		}
		beforeWaves := snapshotWaveAuthorizations(t, vault, []string{"W-SC"})
		beforeTasks := snapshotTaskStatuses(t, vault, []string{"SC-T-0001"})
		server := &serveServer{store: store}
		recorder := httptest.NewRecorder()
		server.handleProjectAutomationScope(recorder, project.ProjectID)
		var body struct {
			OK         bool                     `json:"ok"`
			Enabled    bool                     `json:"enabled"`
			Automation *projectAutomationReport `json:"automation"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode scope readback: %v\n%s", err, recorder.Body.String())
		}
		if !body.OK || body.Automation == nil {
			t.Fatalf("scope readback failed: %s", recorder.Body.String())
		}
		if len(body.Automation.Scope.Waves) != 1 || body.Automation.Scope.Waves[0].WaveID != "W-SC" {
			t.Fatalf("scope readback lost the armed wave: %s", recorder.Body.String())
		}
		if after := snapshotWaveAuthorizations(t, vault, []string{"W-SC"}); after != beforeWaves {
			t.Fatalf("scope read armed work: %s -> %s", beforeWaves, after)
		}
		if after := snapshotTaskStatuses(t, vault, []string{"SC-T-0001"}); after != beforeTasks {
			t.Fatalf("scope read edited tasks: %s -> %s", beforeTasks, after)
		}
	})
}

func selfServiceServeSnapshot(projectID string, enabled bool) serveSnapshot {
	task := Note{Data: map[string]any{
		"id": "S-T-0001", "kind": "task", "title": "Serve fixture", "status": "ready",
		"readiness": "ready", "next_owner": "agent", "wave": "W-SRV",
	}}
	second := Note{Data: map[string]any{
		"id": "S-T-0002", "kind": "task", "title": "Serve fixture sibling", "status": "ready",
		"readiness": "ready", "next_owner": "agent", "wave": "W-SRV",
	}}
	wave := Note{Data: map[string]any{
		"id": "W-SRV", "kind": "wave", "title": "Serve wave", "status": "open",
		"authorization": "armed", "members": []string{"S-T-0001", "S-T-0002"},
	}}
	return serveSnapshot{
		projectID: projectID,
		project:   RegisteredProject{ProjectID: projectID, ProjectKey: projectID, Name: projectID, Enabled: enabled, Health: projectHealthHealthy},
		tasks:     []Note{task, second},
		waves:     []Note{wave},
		notesByID: map[string]Note{"S-T-0001": task, "S-T-0002": second, "W-SRV": wave},
		queue:     map[string]automationTaskExplanation{},
	}
}
