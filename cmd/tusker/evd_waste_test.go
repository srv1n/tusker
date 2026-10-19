package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// interactionPoll sends one UI interaction wake and runs the poll it queues.
func interactionPoll(t *testing.T, d *Daemon, projectID string) {
	t.Helper()
	d.controlReconcileProject(daemonControlRequest{Command: "reconcile_project", ProjectID: projectID, Cause: serveInteractionCause})
	for len(d.notifyWake) > 0 {
		id := <-d.notifyWake
		d.takeProjectReconcile(id)
		if err := d.runPoll(context.Background(), id); err != nil {
			t.Fatal(err)
		}
	}
}

func TestWasteInteractionWakeWithoutChangeSkipsFrontier(t *testing.T) {
	d, project, vault := evdProject(t, false)
	if err := d.runPoll(context.Background(), project.ProjectID); err != nil {
		t.Fatal(err)
	}
	advances := 0
	waveFrontierAdvanceObserver = func(string) { advances++ }
	t.Cleanup(func() { waveFrontierAdvanceObserver = nil })

	interactionPoll(t, d, project.ProjectID)
	if advances != 0 || d.pollFrontierChanged {
		t.Fatalf("unchanged interaction read: advances=%d snapshot rebuild=%v", advances, d.pollFrontierChanged)
	}

	// A hand edit found by the interaction read still advances the frontier.
	writeReconcileResourceTask(t, vault, "APP-T-0099", "backlog")
	interactionPoll(t, d, project.ProjectID)
	if advances != 1 || !d.pollFrontierChanged {
		t.Fatalf("hand edit on interaction read: advances=%d snapshot rebuild=%v", advances, d.pollFrontierChanged)
	}
}

func TestWasteJanitorOncePerTick(t *testing.T) {
	d, project, _ := evdProject(t, false)
	ctx := context.Background()
	if err := d.heartbeat(ctx, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	// A row whose process is gone: any janitor pass closes it.
	id, err := d.store.recordOwnedProcess("check:waste", processKindCheck, 999999, 999999, "x", "pid:1@x")
	if err != nil {
		t.Fatal(err)
	}
	isOpen := func() bool {
		open, err := d.store.openOwnedProcesses()
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range open {
			if p.ID == id {
				return true
			}
		}
		return false
	}
	if err := d.runPoll(ctx, project.ProjectID); err != nil {
		t.Fatal(err)
	}
	if !isOpen() {
		t.Fatal("a poll in the heartbeat's tick ran a second janitor pass")
	}
	d.runProcessJanitor(time.Now().UTC().Add(reconcileLiveCadence))
	if isOpen() {
		t.Fatal("the next tick's janitor pass did not run")
	}
}

func TestWasteJanitorQueryIgnoresClosedRows(t *testing.T) {
	store := openInventoryTestStore(t)
	closed, err := store.recordOwnedProcess("check:closed", processKindCheck, 999998, 999998, "x", "pid:1@x")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.endOwnedProcess(closed, "exited"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.recordOwnedProcess("check:open", processKindCheck, 999997, 999997, "x", "pid:1@x"); err != nil {
		t.Fatal(err)
	}
	open, err := store.openOwnedProcesses()
	if err != nil || len(open) != 1 || open[0].Owner != "check:open" {
		t.Fatalf("open rows = %+v err=%v", open, err)
	}
	if all, err := store.ownedProcesses(); err != nil || len(all) != 2 {
		t.Fatalf("history rows = %+v err=%v", all, err)
	}
}

func TestWasteSilenceLimitCachedUntilConfigChanges(t *testing.T) {
	vault := t.TempDir()
	agentSilenceByVault.Store(vault, agentSilenceEntry{stamp: tuskerConfigStamp(vault), limit: 7 * time.Minute})
	t.Cleanup(func() { agentSilenceByVault.Delete(vault) })
	if got := cachedAgentSilenceLimit(vault); got != 7*time.Minute {
		t.Fatalf("unchanged config reread: %s", got)
	}
	path := managedTuskerConfigPath(vault)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := cachedAgentSilenceLimit(vault); got != defaultAgentSilenceLimit {
		t.Fatalf("changed config not reread: %s", got)
	}
}
