package main

import (
	"fmt"
	"path/filepath"
	"syscall"
	"time"
)

const endReasonOperatorStoppedAll = "operator_stopped_all"

// stopAllCmd is `tusker stop --all`: stop the daemon first so it cannot start
// new work, then stop every process group the inventory still holds open
// (agent sessions and checks). It works when the daemon is already dead.
func stopAllCmd(args Args) error {
	root := DefaultStateRoot()
	daemon := stopDaemonForAll(root)
	store, err := OpenRuntimeStore(root)
	if err != nil {
		return err
	}
	defer store.Close()
	open, err := store.openOwnedProcesses()
	if err != nil {
		return err
	}
	stopped, skipped := []map[string]any{}, []map[string]any{}
	for _, p := range open {
		row := map[string]any{"id": p.ID, "owner": p.Owner, "kind": p.Kind, "pid": p.PID, "pgid": p.PGID}
		if !processIdentityMatches(RunStatus{ProcessPID: p.PID, ProcessPGID: p.PGID, ProcessStartedAt: p.ProcessStart}) {
			// Not provably ours: never signal it. Close the row only when
			// nothing runs at that PID or in that group any more.
			if !processAlive(p.PID) && syscall.Kill(-p.PGID, 0) != nil {
				_ = store.endOwnedProcess(p.ID, "exited")
				continue
			}
			row["reason"] = "PID, process group and start time do not all match the inventory; not stopped"
			_ = store.recordProcessEvent(p, "reported", row["reason"].(string), ruleIdentityMustMatch)
			skipped = append(skipped, row)
			continue
		}
		// End the row first so the operator reason wins over the exit the kill causes.
		if err := store.endOwnedProcess(p.ID, endReasonOperatorStoppedAll); err != nil {
			return err
		}
		_ = store.recordProcessEvent(p, "stopped", "tusker stop --all", ruleOperator)
		stopProcessGroup(p.PGID)
		stopped = append(stopped, row)
	}
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": len(skipped) == 0, "daemon": daemon, "stopped": stopped, "skipped": skipped})
		return nil
	}
	fmt.Println("Daemon: " + daemon["message"].(string))
	for _, row := range stopped {
		fmt.Printf("Stopped %s %v (pid %v, group %v)\n", row["kind"], row["owner"], row["pid"], row["pgid"])
	}
	for _, row := range skipped {
		fmt.Printf("Left running %s %v (pid %v): %v\n", row["kind"], row["owner"], row["pid"], row["reason"])
	}
	if len(stopped) == 0 && len(skipped) == 0 {
		fmt.Println("No Tusker-started processes were running")
	}
	return nil
}

// stopDaemonForAll asks the daemon to stop over its control socket and waits.
// If it does not answer or does not exit, it signals the PID from the receipt,
// but only when that PID's start time matches the receipt (no reused PID).
func stopDaemonForAll(root string) map[string]any {
	before := readDaemonLiveness(root, time.Now().UTC())
	if !before.Alive {
		return map[string]any{"stopped": false, "message": "not running"}
	}
	pid := before.PID
	gone := func(within time.Duration) bool {
		for deadline := time.Now().Add(within); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
			if !processAlive(pid) {
				return true
			}
		}
		return !processAlive(pid)
	}
	if resp, err := sendDaemonControlWithTimeout(root, daemonControlRequest{Command: "stop"}, 3*time.Second); err == nil && resp.OK && gone(10*time.Second) {
		return map[string]any{"stopped": true, "pid": pid, "message": fmt.Sprintf("stopped pid %d", pid)}
	}
	if !daemonReceiptMatchesProcess(root, pid) {
		return map[string]any{"stopped": false, "pid": pid, "message": fmt.Sprintf("pid %d did not stop and its start time does not match the daemon receipt; not signalled", pid)}
	}
	_ = syscall.Kill(pid, syscall.SIGTERM)
	if gone(5 * time.Second) {
		return map[string]any{"stopped": true, "pid": pid, "message": fmt.Sprintf("stopped pid %d (SIGTERM)", pid)}
	}
	_ = syscall.Kill(pid, syscall.SIGKILL)
	gone(2 * time.Second)
	return map[string]any{"stopped": true, "pid": pid, "message": fmt.Sprintf("killed pid %d (SIGKILL)", pid)}
}

// daemonReceiptMatchesProcess: the daemon writes its receipt just after it
// starts, so the kernel start time must fall shortly before started_at.
func daemonReceiptMatchesProcess(root string, pid int) bool {
	receipt, ok, err := readDaemonPIDFile(filepath.Join(root, daemonPIDFileName))
	if err != nil || !ok || receipt.PID != pid {
		return false
	}
	written, err := time.Parse(time.RFC3339Nano, receipt.StartedAt)
	if err != nil {
		return false
	}
	text, ok := processStartTime(pid)
	if !ok {
		return false
	}
	started, err := time.Parse(time.RFC3339, text)
	if err != nil {
		return false
	}
	return !started.After(written.Add(time.Second)) && written.Sub(started) < 5*time.Minute
}
