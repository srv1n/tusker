package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
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
	path            string
	rotate          bool
	allowExisting   bool
	rotated         bool
	onRotate        func(int64) error
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
	parentFD, base, err := openPrivatePathParent(path, true)
	if err != nil {
		return nil, err
	}
	defer unix.Close(parentFD)
	var existing unix.Stat_t
	statErr := unix.Fstatat(parentFD, base, &existing, unix.AT_SYMLINK_NOFOLLOW)
	exists := statErr == nil
	if statErr != nil && !errors.Is(statErr, unix.ENOENT) {
		return nil, statErr
	}
	if exists && !allowExisting {
		return nil, fmt.Errorf("fresh bounded raw log path already exists")
	}
	flags := unix.O_WRONLY | unix.O_APPEND | unix.O_CLOEXEC | unix.O_NOFOLLOW
	if !exists {
		flags |= unix.O_CREAT | unix.O_EXCL
	}
	fd, err := unix.Openat(parentFD, base, flags, 0o600)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	fail := func(err error) (*boundedRawLogWriter, error) {
		_ = file.Close()
		if !exists {
			_ = unix.Unlinkat(parentFD, base, 0)
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
	if openedInfo.Size() > max {
		return fail(fmt.Errorf("bounded raw log already exceeds %d bytes", max))
	}
	return &boundedRawLogWriter{file: file, path: path, max: max, written: openedInfo.Size(), allowExisting: allowExisting}, nil
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
	if w.rotate {
		n, err := w.writeRotating(p)
		w.mu.Unlock()
		return n, err
	}
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
		// The producer may be an exec stderr copier. Termination closes and
		// waits on the same child whose pipe writer is currently in this call;
		// invoke it asynchronously so an overflow can never self-deadlock the
		// copier before it returns the bounded error.
		go terminate()
	}
	if writeErr != nil {
		return n, writeErr
	}
	if exceeded {
		return n, errAuthoritativeRawLogOverflow
	}
	return n, nil
}

// writeRotating keeps the first segment for native session recovery, the
// current segment, and at most one previous segment. It is called under mu.
func (w *boundedRawLogWriter) writeRotating(p []byte) (int, error) {
	written := 0
	for len(p) > 0 {
		if w.written == w.max {
			if err := w.rotateSegment(); err != nil {
				return written, err
			}
		}
		size := int64(len(p))
		if size > w.max-w.written {
			size = w.max - w.written
		}
		n, err := w.file.Write(p[:int(size)])
		w.written += int64(n)
		written += n
		p = p[n:]
		if err != nil {
			return written, err
		}
		if int64(n) != size {
			return written, io.ErrShortWrite
		}
	}
	return written, nil
}

func (w *boundedRawLogWriter) rotateSegment() error {
	parentFD, base, err := openPrivatePathParent(w.path, false)
	if err != nil {
		return err
	}
	defer unix.Close(parentFD)
	var head unix.Stat_t
	headErr := unix.Fstatat(parentFD, base+".head", &head, unix.AT_SYMLINK_NOFOLLOW)
	if headErr != nil && !errors.Is(headErr, unix.ENOENT) {
		return headErr
	}
	headExists := headErr == nil
	if headExists && !w.allowExisting && !w.rotated {
		return fmt.Errorf("fresh raw log has an existing head segment")
	}
	if headExists && (head.Mode&unix.S_IFMT != unix.S_IFREG || head.Mode&0o777 != 0o600 || head.Nlink != 1) {
		return fmt.Errorf("raw log head segment is not exclusive owner-only regular file")
	}
	destination := base + ".head"
	var dropped int64
	if headExists {
		destination = base + ".prev"
		var previous unix.Stat_t
		if err := unix.Fstatat(parentFD, destination, &previous, unix.AT_SYMLINK_NOFOLLOW); err == nil {
			if previous.Mode&unix.S_IFMT != unix.S_IFREG || previous.Mode&0o777 != 0o600 || previous.Nlink != 1 {
				return fmt.Errorf("raw log previous segment is not exclusive owner-only regular file")
			}
			dropped = previous.Size
		} else if !errors.Is(err, unix.ENOENT) {
			return err
		}
	}
	if err := w.file.Close(); err != nil {
		return err
	}
	if err := unix.Renameat(parentFD, base, parentFD, destination); err != nil {
		return err
	}
	next, err := openBoundedRawLog(w.path, w.max, false)
	if err != nil {
		return err
	}
	w.file = next.file
	w.written = 0
	w.rotated = true
	if w.onRotate != nil {
		if err := w.onRotate(dropped); err != nil {
			return err
		}
	}
	return nil
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
		go terminate()
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

func monitorBoundedRunnerCommand(ctx context.Context, cmd *exec.Cmd, pgid int, wrapperContained bool, log *boundedRawLogWriter, runner RunnerName, req runnerExecRequest, eventLog runnerEventLog) {
	waitErr := cmd.Wait()
	// WaitDelay returns once the root has exited but descendants still hold the
	// stdout/stderr pipe open.  Fence that still-owned process group before
	// closing the authoritative log; otherwise an orphaned descendant can keep
	// the workspace busy indefinitely.  The group was created by this launch,
	// and the bounded delay is short enough that its numeric identity cannot be
	// safely treated as reusable until this cleanup completes.
	// A wrapper-contained child shares the wrapper's own process group: killing
	// it here would SIGKILL the wrapper before it can publish this terminal
	// status.  Contained descendants are fenced by the wrapper's post-status
	// containment reap instead, after the durable record exists.
	if errors.Is(waitErr, exec.ErrWaitDelay) && pgid > 0 && !wrapperContained {
		killRunnerProcessGroup(cmd, pgid)
	}
	closeErr := log.close()

	exitCode := 0
	if cmd.ProcessState != nil {
		exitCode = cmd.ProcessState.ExitCode()
	}
	// ErrWaitDelay means the root exited while a descendant held an output
	// pipe; ProcessState still carries the truthful root exit code, and the
	// descendant is fenced by the group reap above or the wrapper's post-status
	// containment cleanup.
	if exitCode < 0 || (waitErr != nil && !errors.Is(waitErr, exec.ErrWaitDelay) && exitCode == 0) {
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
	publishRunnerTerminalStatus(eventLog, runner, req, exitCode, outcome, reason, 0)
	// The monitor is not allowed to signal the group after Wait; a reused PGID
	// could belong to an unrelated process. The trusted launcher owns any
	// pre-Wait cancellation fence.
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
	parentFD, base, err := openPrivatePathParent(path, true)
	if err != nil {
		return false, err
	}
	defer unix.Close(parentFD)
	temp, tempName, err := privateTempFileAt(parentFD, base)
	if err != nil {
		return false, err
	}
	closed := false
	defer func() {
		if !closed {
			_ = temp.Close()
		}
		_ = unix.Unlinkat(parentFD, tempName, 0)
	}()
	if _, err := temp.Write(append(raw, '\n')); err != nil {
		return false, err
	}
	if err := temp.Sync(); err != nil {
		return false, err
	}
	if err := temp.Close(); err != nil {
		return false, err
	}
	closed = true
	if err := unix.Linkat(parentFD, tempName, parentFD, base, 0); err != nil {
		if errors.Is(err, unix.EEXIST) {
			if validateErr := validatePrivatePathEntryAt(parentFD, base, path); validateErr != nil {
				return false, validateErr
			}
			return false, nil
		}
		return false, err
	}
	if err := unix.Unlinkat(parentFD, tempName, 0); err != nil {
		return false, err
	}
	if err := validatePrivatePathEntryAt(parentFD, base, path); err != nil {
		return false, err
	}
	if err := unix.Fsync(parentFD); err != nil {
		return false, fmt.Errorf("sync runner status parent directory: %w", err)
	}
	return true, nil
}
