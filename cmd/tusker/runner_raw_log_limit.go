package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	completionAuthoritativeRawLogOverflowExitCode = 74
	boundedRunnerWaitDelay                        = time.Second
)

var errAuthoritativeRawLogOverflow = errors.New("completion-authoritative raw log byte limit exceeded")

// boundedRawLogWriter is the trusted producer boundary for output that can
// carry completion authority. It owns the already-open daemon log file and
// never writes more than max bytes in total, including bytes from an earlier
// wrapper invocation of the same attempt.
type boundedRawLogWriter struct {
	mu              sync.Mutex
	file            *os.File
	max             int64
	written         int64
	overflow        bool
	terminate       func()
	terminationSent bool
}

func openBoundedRawLog(path string, max int64, allowExisting bool) (*boundedRawLogWriter, error) {
	if max <= 0 {
		return nil, fmt.Errorf("bounded raw log requires a positive byte limit")
	}
	pathInfo, err := os.Lstat(path)
	exists := err == nil
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if exists {
		if !allowExisting {
			return nil, fmt.Errorf("fresh bounded raw log path already exists")
		}
		if err := validateExclusiveRawLog(pathInfo); err != nil {
			return nil, err
		}
		if pathInfo.Size() > max {
			return nil, fmt.Errorf("bounded raw log already exceeds %d bytes", max)
		}
	}

	flags := os.O_WRONLY | os.O_APPEND | syscall.O_NOFOLLOW
	if !exists {
		flags |= os.O_CREATE | os.O_EXCL
	}
	file, err := os.OpenFile(path, flags, 0o600)
	if err != nil {
		return nil, err
	}
	fail := func(err error) (*boundedRawLogWriter, error) {
		_ = file.Close()
		if !exists {
			_ = os.Remove(path)
		}
		return nil, err
	}
	if !exists {
		if err := file.Chmod(0o600); err != nil {
			return fail(fmt.Errorf("secure fresh bounded raw log: %w", err))
		}
	}
	openedInfo, err := file.Stat()
	if err != nil {
		return fail(fmt.Errorf("bounded raw log changed while opening"))
	}
	if err := validateExclusiveRawLog(openedInfo); err != nil {
		return fail(err)
	}
	if exists && !os.SameFile(pathInfo, openedInfo) {
		return fail(fmt.Errorf("bounded raw log changed while opening"))
	}
	currentInfo, err := os.Lstat(path)
	if err != nil || !os.SameFile(openedInfo, currentInfo) {
		return fail(fmt.Errorf("bounded raw log changed while opening"))
	}
	if err := validateExclusiveRawLog(currentInfo); err != nil {
		return fail(err)
	}
	if openedInfo.Size() > max {
		return fail(fmt.Errorf("bounded raw log already exceeds %d bytes", max))
	}
	return &boundedRawLogWriter{file: file, max: max, written: openedInfo.Size()}, nil
}

func validateExclusiveRawLog(info os.FileInfo) error {
	if info == nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("bounded raw log is not a regular file")
	}
	if info.Mode().Perm() != 0o600 {
		return fmt.Errorf("bounded raw log must have owner-only 0600 permissions")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Nlink != 1 {
		return fmt.Errorf("bounded raw log must have exactly one filesystem link")
	}
	return nil
}

func (w *boundedRawLogWriter) Write(p []byte) (int, error) {
	if w == nil {
		return 0, os.ErrInvalid
	}
	w.mu.Lock()
	if w.overflow {
		w.mu.Unlock()
		return 0, errAuthoritativeRawLogOverflow
	}
	remaining := w.max - w.written
	if remaining < 0 {
		remaining = 0
	}
	allowed := len(p)
	if int64(allowed) > remaining {
		allowed = int(remaining)
	}
	n := 0
	var writeErr error
	if allowed > 0 {
		n, writeErr = w.file.Write(p[:allowed])
		w.written += int64(n)
		if writeErr == nil && n != allowed {
			writeErr = io.ErrShortWrite
		}
	}
	exceeded := len(p) > allowed
	var terminate func()
	if exceeded {
		w.overflow = true
		if w.terminate != nil && !w.terminationSent {
			w.terminationSent = true
			terminate = w.terminate
		}
	}
	w.mu.Unlock()
	if terminate != nil {
		terminate()
	}
	if writeErr != nil {
		return n, writeErr
	}
	if exceeded {
		return n, errAuthoritativeRawLogOverflow
	}
	return n, nil
}

// bindTerminator is called only after attempt_spawned has durably published
// the PID. Output can reach the cap before then, but the kill cannot race the
// daemon's process registration.
func (w *boundedRawLogWriter) bindTerminator(terminate func()) {
	if w == nil || terminate == nil {
		return
	}
	w.mu.Lock()
	w.terminate = terminate
	overflowed := w.overflow
	if overflowed && !w.terminationSent {
		w.terminationSent = true
	} else {
		overflowed = false
	}
	w.mu.Unlock()
	if overflowed {
		terminate()
	}
}

func (w *boundedRawLogWriter) overflowed() bool {
	if w == nil {
		return false
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.overflow
}

func (w *boundedRawLogWriter) close() error {
	if w == nil || w.file == nil {
		return nil
	}
	return w.file.Close()
}

func killRunnerProcessGroup(cmd *exec.Cmd, pgid int) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	if pgid > 0 {
		if err := syscall.Kill(-pgid, syscall.SIGKILL); err == nil || errors.Is(err, syscall.ESRCH) {
			return
		}
	}
	_ = cmd.Process.Kill()
}

func killContainedRunnerCommand(cmd *exec.Cmd, pgid int, wrapperContained bool) {
	if wrapperContained {
		if cmd != nil && cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		return
	}
	killRunnerProcessGroup(cmd, pgid)
}

func monitorBoundedRunnerCommand(ctx context.Context, cmd *exec.Cmd, pgid int, wrapperContained bool, log *boundedRawLogWriter, statusPath string) {
	waitDone := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			killContainedRunnerCommand(cmd, pgid, wrapperContained)
		case <-waitDone:
		}
	}()

	waitErr := cmd.Wait()
	close(waitDone)
	// The shell root can exit after leaving a descendant in its process group.
	// WaitDelay bounds inherited output pipes; this final fence prevents that
	// descendant from outliving the trusted wrapper after terminal status.
	if !wrapperContained {
		killRunnerProcessGroup(cmd, pgid)
	}
	closeErr := log.close()

	exitCode := 0
	if cmd.ProcessState != nil {
		exitCode = cmd.ProcessState.ExitCode()
	}
	if exitCode < 0 || (waitErr != nil && exitCode == 0) {
		exitCode = 1
	}
	outcome := AttemptOutcomeNone
	reason := ""
	if log.overflowed() {
		exitCode = completionAuthoritativeRawLogOverflowExitCode
		outcome = AttemptOutcomeFailed
		reason = fmt.Sprintf("completion-authoritative raw log exceeded %d-byte limit", log.max)
	} else if ctx.Err() != nil {
		exitCode = 130
		outcome = AttemptOutcomeInterrupted
		reason = "completion-authoritative runner cancelled: " + ctx.Err().Error()
	} else if closeErr != nil {
		exitCode = 1
		outcome = AttemptOutcomeFailed
		reason = "close completion-authoritative raw log: " + closeErr.Error()
	} else if exitCode != 0 {
		outcome = AttemptOutcomeFailed
		reason = fmt.Sprintf("runner exited with code %d", exitCode)
	}
	_, _ = writeRunnerStatusFileIfAbsentWithOutcome(statusPath, exitCode, outcome, reason, 0)
	if wrapperContained && pgid > 0 {
		// Persist the terminal status before fencing the containing wrapper group:
		// SIGKILL necessarily includes this monitor because it runs inside that
		// wrapper. The daemon consumes the durable status after the group exits.
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
	}
}

func writeRunnerStatusFileIfAbsentWithOutcome(path string, exitCode int, outcome AttemptOutcome, reason string, turnsUsed int) (bool, error) {
	payload := runnerProcessStatus{
		ExitCode:    exitCode,
		CompletedAt: time.Now().UTC().Format(time.RFC3339),
	}
	if outcome != "" && outcome != AttemptOutcomeNone {
		payload.Outcome = string(outcome)
	}
	if reason = strings.TrimSpace(reason); reason != "" {
		payload.Reason = reason
	}
	if turnsUsed > 0 {
		payload.TurnsUsed = turnsUsed
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return false, err
	}
	if err := ensureDir(filepath.Dir(path)); err != nil {
		return false, err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".terminal-*")
	if err != nil {
		return false, err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		return false, err
	}
	if _, err := temp.Write(append(raw, '\n')); err != nil {
		_ = temp.Close()
		return false, err
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return false, err
	}
	if err := temp.Close(); err != nil {
		return false, err
	}
	if err := os.Link(tempPath, path); err != nil {
		if errors.Is(err, os.ErrExist) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}
