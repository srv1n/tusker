package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A stand-in daemon (a real sleeping process with a receipt but no control
// socket) proves the dead-socket fallback; real sleeping groups stand in for
// an agent session and a check.
func TestStopAllStopsInventoryAndDaemon(t *testing.T) {
	root := t.TempDir()
	t.Setenv("TUSKER_STATE_ROOT", root)
	store, err := OpenRuntimeStore(root)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	daemon := startJanitorTestGroup(t)
	receipt, _ := json.Marshal(daemonPIDFile{PID: daemon.pid, StartedAt: time.Now().UTC().Format(time.RFC3339Nano)})
	if err := os.WriteFile(filepath.Join(root, daemonPIDFileName), receipt, 0o600); err != nil {
		t.Fatal(err)
	}
	agent := startJanitorTestGroup(t)
	check := startJanitorTestGroup(t)
	recordJanitorTestRow(t, store, "ATT-stop-all-agent", processKindAgentSession, agent, "", currentProcessStarter())
	recordJanitorTestRow(t, store, "stop-all-check", processKindCheck, check, "", currentProcessStarter())

	if err := stopCmd(Args{"all": "true", "json": "true"}); err != nil {
		t.Fatal(err)
	}
	for name, p := range map[string]janitorTestProc{"daemon": daemon, "agent": agent, "check": check} {
		if !p.stopped(3 * time.Second) {
			t.Fatalf("%s group still running after stop --all", name)
		}
	}
	for _, owner := range []string{"ATT-stop-all-agent", "stop-all-check"} {
		row := inventoryRow(t, store, owner)
		if row.EndedAt == "" || row.EndReason != endReasonOperatorStoppedAll {
			t.Fatalf("%s row = %+v, want ended %s", owner, row, endReasonOperatorStoppedAll)
		}
	}
	open, err := store.openOwnedProcesses()
	if err != nil || len(open) != 0 {
		t.Fatalf("open rows after stop --all = %v, %v", open, err)
	}

	// Already dead daemon and empty inventory: still succeeds.
	if err := stopCmd(Args{"all": "true"}); err != nil {
		t.Fatalf("second stop --all: %v", err)
	}
}
