package main

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// These pipes belong to the live reader, rather than exec.Cmd. Wait can reap
// the root process without closing unread final output or waiting for an
// inherited pipe that a descendant still has open.
type nativeRunnerPipes struct {
	stdout, stderr           *os.File
	stdoutWrite, stderrWrite *os.File
}

func openNativeRunnerPipes(cmd *exec.Cmd) (*nativeRunnerPipes, error) {
	p := &nativeRunnerPipes{}
	var err error
	p.stdout, p.stdoutWrite, err = os.Pipe()
	if err != nil {
		return nil, err
	}
	p.stderr, p.stderrWrite, err = os.Pipe()
	if err != nil {
		p.close()
		return nil, err
	}
	cmd.Stdout, cmd.Stderr = p.stdoutWrite, p.stderrWrite
	return p, nil
}

func (p *nativeRunnerPipes) closeWriters() {
	if p.stdoutWrite != nil {
		_ = p.stdoutWrite.Close()
	}
	if p.stderrWrite != nil {
		_ = p.stderrWrite.Close()
	}
}

func (p *nativeRunnerPipes) close() {
	p.closeWriters()
	if p.stdout != nil {
		_ = p.stdout.Close()
	}
	if p.stderr != nil {
		_ = p.stderr.Close()
	}
}

func waitForNativeRunnerExit(cmd *exec.Cmd, wg *sync.WaitGroup, stdout, stderr io.ReadCloser, closing *atomic.Bool) (error, bool) {
	err := cmd.Wait()
	drained := make(chan struct{})
	go func() { wg.Wait(); close(drained) }()
	timer := time.NewTimer(boundedRunnerWaitDelay)
	defer timer.Stop()
	select {
	case <-drained:
		return err, true
	case <-timer.C:
		closing.Store(true)
		if stdout != nil {
			_ = stdout.Close()
		}
		if stderr != nil {
			_ = stderr.Close()
		}
		<-drained
		return err, false
	}
}

func openNativeRunnerRawLog(req StartRequest, resume bool) (*boundedRawLogWriter, error) {
	limit := req.RawLogMaxBytes
	if limit < 0 {
		return nil, errors.New("raw log byte limit cannot be negative")
	}
	if limit == 0 {
		limit = completionAuthoritativeRawLogMaxBytes
	}
	return openBoundedRawLog(req.RawLogPath, limit, resume)
}

// Production handles open the log before launching. Lazy opening preserves
// manually constructed handles used by callers and tests, with the same cap.
func appendNativeRunnerLog(mu *sync.Mutex, log **boundedRawLogWriter, path, line string) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	mu.Lock()
	defer mu.Unlock()
	if *log == nil {
		var err error
		*log, err = openNativeRunnerRawLog(StartRequest{RawLogPath: path}, true)
		if err != nil {
			return err
		}
	}
	_, err := (*log).Write([]byte(line + "\n"))
	return err
}

func closeNativeRunnerLog(mu *sync.Mutex, log *boundedRawLogWriter) error {
	mu.Lock()
	defer mu.Unlock()
	return log.close()
}

func nativeRunnerFailureExitCode(err error) int {
	if errors.Is(err, errAuthoritativeRawLogOverflow) {
		return completionAuthoritativeRawLogOverflowExitCode
	}
	return 1
}

func reapNativeRunnerDescendants(cmd *exec.Cmd) {
	if cmd != nil && cmd.Process != nil && cmd.SysProcAttr != nil && cmd.SysProcAttr.Setpgid && cmd.Process.Pid != processGroupID(os.Getpid()) {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}

func killNativeRunnerProcess(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	// A contained child shares its wrapper's group. Only the wrapper may kill
	// that group, after recording the terminal status and its ownership outcome.
	if cmd.SysProcAttr != nil && cmd.SysProcAttr.Setpgid {
		killRunnerProcessGroup(cmd, cmd.Process.Pid)
	} else {
		_ = cmd.Process.Kill()
	}
}

func stopNativeRunnerAfterResult(cmd *exec.Cmd, stdin io.WriteCloser) {
	if stdin != nil {
		_ = stdin.Close()
	}
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Signal(syscall.SIGTERM)
		// Process.Kill refuses a process already reaped by Wait. Do not use a
		// delayed numeric group signal that could hit a reused process group.
		time.AfterFunc(boundedRunnerWaitDelay, func() { _ = cmd.Process.Kill() })
	}
}
