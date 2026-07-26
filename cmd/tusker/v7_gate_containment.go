package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// A gate may deliberately call setsid(2), so its process group is a useful
// first stop but not a containment boundary.  The supervisor snapshots the
// process tree while the original group is frozen, freezes each newly found
// descendant, and only then kills the recorded identities.  This is kept at
// the sandbox boundary: normal local gate commands do not get this privilege.
type v7GateProcess struct {
	PID       int
	PPID      int
	StartedAt string
}

type v7GateOutput struct {
	mu sync.Mutex
	b  strings.Builder
}

func (b *v7GateOutput) Write(data []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(data)
}

func (b *v7GateOutput) Bytes() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	return []byte(b.b.String())
}

var v7GateProcessSnapshot = snapshotV7GateProcesses

var errV7GateContainment = errors.New("gate containment")

const (
	v7GateContainmentScans = 16
	v7GateContainmentWait  = 2 * time.Second
)

func runV7ContainedGateCommand(ctx context.Context, cmd *exec.Cmd) ([]byte, error) {
	if cmd == nil {
		return nil, fmt.Errorf("gate containment: nil command")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var output v7GateOutput
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Start(); err != nil {
		return output.Bytes(), err
	}
	root, err := v7GateProcessIdentity(cmd.Process.Pid)
	if err != nil {
		// The command is ours, but without a stable identity it is not safe to
		// address a reused PID.  Best-effort the just-created group and refuse.
		v7KillGateRootGroup(v7GateProcess{PID: cmd.Process.Pid})
		waitV7GateCommand(cmd, v7GateContainmentWait)
		return output.Bytes(), fmt.Errorf("%w: cannot establish root identity: %v", errV7GateContainment, err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		return output.Bytes(), err
	case <-ctx.Done():
		containErr := containV7GateTree(root)
		if containErr != nil {
			// Do not leave the original group running merely because the tree
			// snapshot failed.  This fallback is identity-checked; the returned
			// error deliberately keeps the containment failure visible.
			v7KillGateRootGroup(root)
		}
		if !waitV7GateCommandDone(done, v7GateContainmentWait) && containErr == nil {
			containErr = fmt.Errorf("%w: root process did not reap after termination", errV7GateContainment)
		}
		if containErr != nil {
			return output.Bytes(), containErr
		}
		return output.Bytes(), ctx.Err()
	}
}

func waitV7GateCommand(cmd *exec.Cmd, timeout time.Duration) {
	done := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(timeout):
	}
}

func waitV7GateCommandDone(done <-chan error, timeout time.Duration) bool {
	select {
	case <-done:
		return true
	case <-time.After(timeout):
		return false
	}
}

func containV7GateTree(root v7GateProcess) error {
	if !v7GateProcessStillMatches(root) {
		return fmt.Errorf("%w: root PID %d no longer matches its recorded identity", errV7GateContainment, root.PID)
	}
	if err := syscall.Kill(-root.PID, syscall.SIGSTOP); err != nil && !errors.Is(err, syscall.ESRCH) {
		return fmt.Errorf("%w: cannot freeze root process group %d: %v", errV7GateContainment, root.PID, err)
	}

	recorded := map[int]v7GateProcess{root.PID: root}
	stable := false
	for scan := 0; scan < v7GateContainmentScans; scan++ {
		snapshot, err := v7GateProcessSnapshot()
		if err != nil {
			return fmt.Errorf("%w: cannot snapshot descendants: %v", errV7GateContainment, err)
		}
		tree, ok := v7GateDescendants(root, snapshot)
		if !ok {
			return fmt.Errorf("%w: root PID %d disappeared before descendant tree was proven", errV7GateContainment, root.PID)
		}
		added := false
		for _, process := range tree {
			if prior, exists := recorded[process.PID]; exists {
				if prior.StartedAt != process.StartedAt {
					return fmt.Errorf("%w: PID %d was reused during descendant discovery", errV7GateContainment, process.PID)
				}
				continue
			}
			recorded[process.PID] = process
			added = true
		}
		for _, process := range recorded {
			if !v7GateProcessStillMatches(process) {
				// A vanished process cannot fork after it is gone. A changed
				// identity is PID reuse and must never be signalled.
				if processExists(process.PID) {
					return fmt.Errorf("%w: PID %d identity changed while freezing descendants", errV7GateContainment, process.PID)
				}
				continue
			}
			if err := syscall.Kill(process.PID, syscall.SIGSTOP); err != nil && !errors.Is(err, syscall.ESRCH) {
				return fmt.Errorf("%w: cannot freeze descendant PID %d: %v", errV7GateContainment, process.PID, err)
			}
		}
		if !added {
			// Every process seen in a complete ancestry walk is stopped. A final
			// identical walk closes the race where an escaped child forked while
			// the preceding scan was in progress.
			verified, verifyErr := v7GateProcessSnapshot()
			if verifyErr != nil {
				return fmt.Errorf("%w: cannot verify frozen descendants: %v", errV7GateContainment, verifyErr)
			}
			if finalTree, found := v7GateDescendants(root, verified); found && v7GateTreeRecorded(finalTree, recorded) {
				stable = true
				break
			}
		}
	}
	if !stable {
		return fmt.Errorf("%w: descendant tree did not stabilize before termination", errV7GateContainment)
	}

	for _, process := range recorded {
		v7SignalGateProcess(process, syscall.SIGTERM)
	}
	for _, process := range recorded {
		v7SignalGateProcess(process, syscall.SIGKILL)
	}
	deadline := time.Now().Add(v7GateContainmentWait)
	for {
		allGone := true
		for _, process := range recorded {
			if !v7GateProcessStillMatches(process) {
				continue
			}
			allGone = false
			break
		}
		if allGone {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("%w: recorded descendants remained after SIGKILL", errV7GateContainment)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func v7KillGateRootGroup(root v7GateProcess) {
	if root.PID <= 0 || (root.StartedAt != "" && !v7GateProcessStillMatches(root)) {
		return
	}
	_ = syscall.Kill(-root.PID, syscall.SIGKILL)
}

func v7SignalGateProcess(process v7GateProcess, signal syscall.Signal) {
	if v7GateProcessStillMatches(process) {
		_ = syscall.Kill(process.PID, signal)
	}
}

func v7GateDescendants(root v7GateProcess, processes []v7GateProcess) ([]v7GateProcess, bool) {
	byParent := make(map[int][]v7GateProcess, len(processes))
	rootFound := false
	for _, process := range processes {
		if process.PID == root.PID && process.StartedAt == root.StartedAt {
			rootFound = true
		}
		byParent[process.PPID] = append(byParent[process.PPID], process)
	}
	if !rootFound {
		return nil, false
	}
	result := []v7GateProcess{root}
	seen := map[int]string{root.PID: root.StartedAt}
	for current := 0; current < len(result); current++ {
		for _, child := range byParent[result[current].PID] {
			if started, exists := seen[child.PID]; exists {
				if started != child.StartedAt {
					return nil, false
				}
				continue
			}
			seen[child.PID] = child.StartedAt
			result = append(result, child)
		}
	}
	return result, true
}

func v7GateTreeRecorded(tree []v7GateProcess, recorded map[int]v7GateProcess) bool {
	for _, process := range tree {
		recordedProcess, ok := recorded[process.PID]
		if !ok || recordedProcess.StartedAt != process.StartedAt {
			return false
		}
	}
	return true
}

func v7GateProcessIdentity(pid int) (v7GateProcess, error) {
	started, ok := processStartTime(pid)
	if !ok || strings.TrimSpace(started) == "" {
		return v7GateProcess{}, fmt.Errorf("PID %d has no readable start time", pid)
	}
	return v7GateProcess{PID: pid, PPID: os.Getpid(), StartedAt: started}, nil
}

func v7GateProcessStillMatches(process v7GateProcess) bool {
	if process.PID <= 0 || !processExists(process.PID) {
		return false
	}
	started, ok := processStartTime(process.PID)
	return ok && started == process.StartedAt
}

func snapshotV7GateProcesses() ([]v7GateProcess, error) {
	out, err := exec.Command("ps", "-axo", "pid=,ppid=,lstart=").Output()
	if err != nil {
		return nil, err
	}
	processes := []v7GateProcess{}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 7 {
			continue
		}
		pid, pidErr := strconv.Atoi(fields[0])
		ppid, ppidErr := strconv.Atoi(fields[1])
		if pidErr != nil || ppidErr != nil || pid <= 0 {
			continue
		}
		startedText := strings.Join(fields[2:], " ")
		started, parseErr := time.ParseInLocation("Mon Jan _2 15:04:05 2006", startedText, time.Local)
		if parseErr != nil {
			continue
		}
		processes = append(processes, v7GateProcess{PID: pid, PPID: ppid, StartedAt: started.UTC().Format(time.RFC3339)})
	}
	if len(processes) == 0 {
		return nil, fmt.Errorf("ps returned no parseable process identities")
	}
	return processes, nil
}
