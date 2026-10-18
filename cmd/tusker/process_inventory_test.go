package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func openInventoryTestStore(t *testing.T) *RuntimeStore {
	t.Helper()
	t.Setenv("TUSKER_STATE_ROOT", t.TempDir())
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func inventoryRow(t *testing.T, store *RuntimeStore, owner string) ownedProcess {
	t.Helper()
	rows, err := store.ownedProcesses()
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.Owner == owner {
			return row
		}
	}
	t.Fatalf("no inventory row for owner %q in %+v", owner, rows)
	return ownedProcess{}
}

func waitProcessGone(pid int, within time.Duration) bool {
	for deadline := time.Now().Add(within); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if !processExists(pid) {
			return true
		}
	}
	return false
}

func TestProcessInventoryRecordsGateCheck(t *testing.T) {
	store := openInventoryTestStore(t)
	dir := t.TempDir()
	output, err := runV7OwnedLandingGateCommand(context.Background(), "landing_gate:lane-1", dir, `echo "$$ $(ps -o pgid= -p $$) $TUSKER_PROCESS_OWNER"`, false)
	if err != nil {
		t.Fatalf("gate: %v\n%s", err, output)
	}
	fields := strings.Fields(string(output))
	if len(fields) != 3 {
		t.Fatalf("unexpected output %q", output)
	}
	row := inventoryRow(t, store, "landing_gate:lane-1")
	if row.Kind != processKindCheck || strconv.Itoa(row.PID) != fields[0] || strconv.Itoa(row.PGID) != fields[1] || row.PID != row.PGID {
		t.Fatalf("row %+v does not match own-group check pid/pgid %v", row, fields)
	}
	if fields[2] != "landing_gate:lane-1" {
		t.Fatalf("owner identity missing from environment: %q", fields[2])
	}
	if row.ProcessStart == "" || row.Starter != currentProcessStarter() || row.StartedAt == "" || row.EndedAt == "" || row.EndReason != "passed" {
		t.Fatalf("incomplete inventory row %+v", row)
	}
}

func TestProcessInventoryCancelKillsTree(t *testing.T) {
	store := openInventoryTestStore(t)
	pidFile := filepath.Join(t.TempDir(), "grandchild.pid")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, err := runOwnedCheck(ctx, "cancel-tree", exec.Command("sh", "-c", "sleep 30 & echo $! > "+pidFile+"; wait"), time.Minute)
		result <- err
	}()
	var grandchild int
	for deadline := time.Now().Add(3 * time.Second); grandchild == 0 && time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		data, _ := os.ReadFile(pidFile)
		grandchild, _ = strconv.Atoi(strings.TrimSpace(string(data)))
	}
	if grandchild == 0 {
		t.Fatal("grandchild never started")
	}
	t.Cleanup(func() { _ = syscall.Kill(grandchild, syscall.SIGKILL) })
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("want context.Canceled, got %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("cancelled check did not return")
	}
	if !waitProcessGone(grandchild, 2*time.Second) {
		t.Fatalf("grandchild %d survived cancellation", grandchild)
	}
	if row := inventoryRow(t, store, "cancel-tree"); row.EndReason != "cancelled" {
		t.Fatalf("end reason %q, want cancelled", row.EndReason)
	}
}

func TestProcessInventorySilentCheckStuckStopped(t *testing.T) {
	store := openInventoryTestStore(t)
	started := time.Now()
	_, err := runOwnedCheck(context.Background(), "silent", exec.Command("sh", "-c", "echo start; sleep 30"), 300*time.Millisecond)
	if !errors.Is(err, errCheckStuckStopped) {
		t.Fatalf("want stuck-stopped error, got %v", err)
	}
	if elapsed := time.Since(started); elapsed > 3*time.Second {
		t.Fatalf("silent check stopped too late: %s", elapsed)
	}
	if row := inventoryRow(t, store, "silent"); row.EndReason != "stuck_stopped" {
		t.Fatalf("end reason %q, want stuck_stopped", row.EndReason)
	}
}

func TestProcessInventorySilentCheckChattyNotStopped(t *testing.T) {
	store := openInventoryTestStore(t)
	// Runs ~1.2s, three times the silence limit, but prints every 100ms.
	output, err := runOwnedCheck(context.Background(), "chatty", exec.Command("sh", "-c", "i=0; while [ $i -lt 12 ]; do echo tick; sleep 0.1; i=$((i+1)); done"), 400*time.Millisecond)
	if err != nil {
		t.Fatalf("chatty check stopped: %v", err)
	}
	if strings.Count(string(output), "tick") != 12 {
		t.Fatalf("output truncated: %q", output)
	}
	row := inventoryRow(t, store, "chatty")
	if row.EndReason != "passed" || row.LastSignOfLife <= row.StartedAt {
		t.Fatalf("chatty row %+v", row)
	}
}

// go test prints nothing until a package ends; a quiet check whose group keeps
// burning CPU is working, not stuck.
func TestProcessInventoryQuietBusyCheckNotStopped(t *testing.T) {
	store := openInventoryTestStore(t)
	_, err := runOwnedCheck(context.Background(), "quiet-busy", exec.Command("sh", "-c", "yes >/dev/null & p=$!; sleep 2.5; kill $p"), 1500*time.Millisecond)
	if err != nil {
		t.Fatalf("quiet busy check stopped: %v", err)
	}
	if row := inventoryRow(t, store, "quiet-busy"); row.EndReason != "passed" {
		t.Fatalf("row %+v", row)
	}
}

func TestProcessInventoryStopByStarter(t *testing.T) {
	store := openInventoryTestStore(t)
	start := func(starter string) *exec.Cmd {
		cmd := exec.Command("sh", "-c", "sleep 30")
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); _ = cmd.Wait() })
		processStart, _ := processStartTime(cmd.Process.Pid)
		if _, err := store.recordOwnedProcess("check-"+starter, processKindCheck, cmd.Process.Pid, cmd.Process.Pid, processStart, starter); err != nil {
			t.Fatal(err)
		}
		return cmd
	}
	mine, other := start("starter-a"), start("starter-b")
	// A row for starter-a whose start time does not match must be left alone.
	if _, err := store.recordOwnedProcess("check-reused", processKindCheck, other.Process.Pid, other.Process.Pid, "1999-01-01T00:00:00Z", "starter-a"); err != nil {
		t.Fatal(err)
	}
	stopped, err := stopOwnedChecksByStarter(store, "starter-a")
	if err != nil || stopped != 1 {
		t.Fatalf("stopped=%d err=%v, want 1", stopped, err)
	}
	waited := make(chan error, 1)
	go func() { waited <- mine.Wait() }()
	select {
	case <-waited:
	case <-time.After(2 * time.Second):
		t.Fatal("starter-a check still running")
	}
	if syscall.Kill(other.Process.Pid, 0) != nil {
		t.Fatal("starter-b check was stopped")
	}
	if row := inventoryRow(t, store, "check-starter-a"); row.EndReason != "starter_stopped" {
		t.Fatalf("end reason %q", row.EndReason)
	}
	if row := inventoryRow(t, store, "check-reused"); row.EndedAt != "" {
		t.Fatalf("mismatched identity row was touched: %+v", row)
	}
}
