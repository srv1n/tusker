package main

import (
	"fmt"
	"os"
	"syscall"
	"time"
)

// demoKillSessionWorker kills only the verified process group for this demo
// task and attempt. It leaves the lease intact so the daemon can observe Lost.
func demoKillSessionWorker(repo, projectID, taskID, attemptID string) error {
	store, err := OpenRuntimeStoreReadOnly(DefaultStateRoot())
	if err != nil {
		return err
	}
	defer store.Close()
	run, err := store.FindRunScoped(projectID, taskID)
	if err != nil {
		return err
	}
	if run == nil || run.ProjectID != projectID || run.RecordID != taskID || run.ActiveAttemptID != attemptID {
		return fmt.Errorf("demo worker lease changed; refusing to signal a process")
	}
	if run.LeaseState != string(LeaseStateRunning) || !pathWithin(repo, run.WorkspacePath) {
		return fmt.Errorf("run is not an active worker in the demo workspace")
	}
	if run.ProcessPID <= 0 || run.ProcessPGID != run.ProcessPID || !processIdentityMatches(*run) {
		return fmt.Errorf("demo worker process identity is not verified")
	}
	if run.ProcessPGID == processGroupID(os.Getpid()) {
		return fmt.Errorf("demo worker shares this command's process group")
	}
	if daemon := readDaemonLiveness(DefaultStateRoot(), time.Now().UTC()); daemon.Alive && (run.ProcessPID == daemon.PID || run.ProcessPGID == processGroupID(daemon.PID)) {
		return fmt.Errorf("demo worker shares the resident daemon's process group")
	}
	return syscall.Kill(-run.ProcessPGID, syscall.SIGKILL)
}
