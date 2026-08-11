package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

func TestBoundedRawLogWriterExactCapAndConcurrentOverflow(t *testing.T) {
	t.Run("exact cap", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "exact.raw.log")
		writer, err := openBoundedRawLog(path, 64, false)
		if err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		errs := make(chan error, 2)
		for _, payload := range [][]byte{
			[]byte(strings.Repeat("a", 32)),
			[]byte(strings.Repeat("b", 32)),
		} {
			wg.Add(1)
			go func(payload []byte) {
				defer wg.Done()
				_, err := writer.Write(payload)
				errs <- err
			}(payload)
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			if err != nil {
				t.Fatalf("exact-cap write failed: %v", err)
			}
		}
		if writer.overflowed() {
			t.Fatal("exact-cap output was classified as overflow")
		}
		if err := writer.close(); err != nil {
			t.Fatal(err)
		}
		assertRawLogSize(t, path, 64)
	})

	t.Run("cap plus one split across streams", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "overflow.raw.log")
		writer, err := openBoundedRawLog(path, 64, false)
		if err != nil {
			t.Fatal(err)
		}
		var kills atomic.Int32
		terminationDone := make(chan struct{})
		writer.bindTerminator(func() {
			kills.Add(1)
			close(terminationDone)
		})
		var wg sync.WaitGroup
		errs := make(chan error, 2)
		for _, payload := range [][]byte{
			[]byte(strings.Repeat("a", 32)),
			[]byte(strings.Repeat("b", 33)),
		} {
			wg.Add(1)
			go func(payload []byte) {
				defer wg.Done()
				_, err := writer.Write(payload)
				errs <- err
			}(payload)
		}
		wg.Wait()
		close(errs)
		overflowErrors := 0
		for err := range errs {
			if errors.Is(err, errAuthoritativeRawLogOverflow) {
				overflowErrors++
			} else if err != nil {
				t.Fatalf("unexpected bounded-writer error: %v", err)
			}
		}
		if overflowErrors == 0 || !writer.overflowed() {
			t.Fatalf("cap+1 did not trigger one terminal overflow: errors=%d overflow=%t kills=%d", overflowErrors, writer.overflowed(), kills.Load())
		}
		select {
		case <-terminationDone:
		case <-time.After(time.Second):
			t.Fatalf("cap+1 did not trigger async termination: errors=%d overflow=%t kills=%d", overflowErrors, writer.overflowed(), kills.Load())
		}
		if kills.Load() != 1 {
			t.Fatalf("cap+1 triggered unexpected terminal termination count: %d", kills.Load())
		}
		if err := writer.close(); err != nil {
			t.Fatal(err)
		}
		assertRawLogSize(t, path, 64)
	})

	t.Run("preexisting unsafe targets", func(t *testing.T) {
		dir := t.TempDir()
		oversized := filepath.Join(dir, "oversized.raw.log")
		file, err := os.Create(oversized)
		if err != nil {
			t.Fatal(err)
		}
		if err := file.Truncate(65); err != nil {
			t.Fatal(err)
		}
		if err := file.Chmod(0o600); err != nil {
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
		if _, err := openBoundedRawLog(oversized, 64, true); err == nil {
			t.Fatal("producer opened a preexisting oversized raw log")
		}
		target := filepath.Join(dir, "target")
		if err := os.WriteFile(target, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		symlink := filepath.Join(dir, "symlink.raw.log")
		if err := os.Symlink(target, symlink); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		if _, err := openBoundedRawLog(symlink, 64, true); err == nil {
			t.Fatal("producer followed a preexisting symlinked raw log")
		}

		precreated := filepath.Join(dir, "precreated.raw.log")
		if err := os.WriteFile(precreated, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := openBoundedRawLog(precreated, 64, false); err == nil {
			t.Fatal("fresh producer accepted a precreated raw log")
		}

		writable := filepath.Join(dir, "group-writable.raw.log")
		if err := os.WriteFile(writable, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(writable, 0o660); err != nil {
			t.Fatal(err)
		}
		if _, err := openBoundedRawLog(writable, 64, true); err == nil {
			t.Fatal("resumed producer accepted a group-writable raw log")
		}

		hardlinked := filepath.Join(dir, "hardlinked.raw.log")
		if err := os.WriteFile(hardlinked, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Link(hardlinked, filepath.Join(dir, "raw-log-alias")); err != nil {
			t.Skipf("hard links unavailable: %v", err)
		}
		if _, err := openBoundedRawLog(hardlinked, 64, true); err == nil {
			t.Fatal("resumed producer accepted a hard-linked raw log")
		}
	})

	t.Run("resume accepts only exclusive owner-only log", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "resume.raw.log")
		if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
			t.Fatal(err)
		}
		writer, err := openBoundedRawLog(path, 64, true)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write([]byte(strings.Repeat("n", 61))); err != nil {
			t.Fatal(err)
		}
		if err := writer.close(); err != nil {
			t.Fatal(err)
		}
		assertRawLogSize(t, path, 64)
	})
}

func TestBoundedRunnerImmediateOverflowWinsCancellationAndPublishesSpawn(t *testing.T) {
	req := runnerExecEventRequestForTest(t)
	req.Command = ""
	req.CommandArgv = []string{"/bin/sh", "-c", `while :; do printf '0123456789abcdef'; done`}
	req.RawLogMaxBytes = 4096

	eventLog := NewEventLog(req.EventSinkPath)
	writeCount := 0
	eventLog.writeFn = func(file *os.File, raw []byte) (int, error) {
		writeCount++
		if writeCount == 2 {
			deadline := time.Now().Add(3 * time.Second)
			for time.Now().Before(deadline) {
				if info, err := os.Stat(req.RawLogPath); err == nil && info.Size() == req.RawLogMaxBytes {
					break
				}
				time.Sleep(5 * time.Millisecond)
			}
			assertRawLogSize(t, req.RawLogPath, req.RawLogMaxBytes)
		}
		return file.Write(raw)
	}

	ctx, cancel := context.WithCancel(context.Background())
	result, err := executeRunnerCommandWithEventLog(ctx, RunnerCodexExec, req, RunnerCapabilities{}, eventLog)
	if err != nil || result == nil || result.PID <= 0 {
		t.Fatalf("bounded runner did not return its published process: result=%#v err=%v", result, err)
	}
	cancel()
	waitForStatusFile(t, req.StatusPath)
	status, err := readRunnerProcessStatus(req.StatusPath)
	if err != nil {
		t.Fatal(err)
	}
	if status.ExitCode != completionAuthoritativeRawLogOverflowExitCode ||
		AttemptOutcome(status.Outcome) != AttemptOutcomeFailed ||
		!strings.Contains(status.Reason, "raw log exceeded") {
		t.Fatalf("cancellation overwrote immediate overflow status: %#v", status)
	}
	assertRawLogSize(t, req.RawLogPath, req.RawLogMaxBytes)
	events, err := os.ReadFile(req.EventSinkPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(events), `"kind":"attempt_spawned"`) {
		t.Fatalf("overflow killed the process before its PID was published: %s", events)
	}
}

func TestBoundedRunnerExactCapAndResumeOverflow(t *testing.T) {
	t.Run("exact cap succeeds", func(t *testing.T) {
		req := runnerExecEventRequestForTest(t)
		payload := filepath.Join(t.TempDir(), "payload")
		if err := os.WriteFile(payload, []byte(strings.Repeat("x", 1024)), 0o600); err != nil {
			t.Fatal(err)
		}
		req.Command = ""
		req.CommandArgv = []string{"/bin/cat", payload}
		req.RawLogMaxBytes = 1024
		if _, err := executeRunnerCommand(context.Background(), RunnerCodexExec, req, RunnerCapabilities{}); err != nil {
			t.Fatal(err)
		}
		waitForStatusFile(t, req.StatusPath)
		status, err := readRunnerProcessStatus(req.StatusPath)
		if err != nil || status.ExitCode != 0 {
			t.Fatalf("exact-cap process did not succeed: status=%#v err=%v", status, err)
		}
		assertRawLogSize(t, req.RawLogPath, req.RawLogMaxBytes)
	})

	t.Run("resume retains cap", func(t *testing.T) {
		req := runnerExecEventRequestForTest(t)
		payload := filepath.Join(t.TempDir(), "payload")
		if err := os.WriteFile(payload, []byte(strings.Repeat("x", 1025)), 0o600); err != nil {
			t.Fatal(err)
		}
		req.Command = ""
		req.CommandArgv = []string{"/bin/cat", payload}
		req.RawLogMaxBytes = 1024
		_, err := runnerWrapperStartChild(context.Background(), runnerWrapperRequest{
			Runner: string(RunnerCodexExec),
			Start: StartRequest{
				ProjectID: req.ProjectID, RecordID: req.RecordID, ItemID: req.ItemID, AttemptID: req.AttemptID,
				Lane: req.Lane, WorkspacePath: req.WorkspacePath, RepoRoot: req.RepoRoot,
				PromptPath: req.PromptPath, EventSinkPath: req.EventSinkPath,
				RawLogPath: req.RawLogPath, RawLogMaxBytes: req.RawLogMaxBytes, StatusPath: req.StatusPath,
				CommandArgv: append([]string(nil), req.CommandArgv...),
			},
			Resume: &ResumeRequest{SessionRef: "session-existing"},
		})
		if err != nil {
			t.Fatal(err)
		}
		waitForStatusFile(t, req.StatusPath)
		status, err := readRunnerProcessStatus(req.StatusPath)
		if err != nil || status.ExitCode != completionAuthoritativeRawLogOverflowExitCode {
			t.Fatalf("resume bypassed bounded output: status=%#v err=%v", status, err)
		}
		assertRawLogSize(t, req.RawLogPath, req.RawLogMaxBytes)
	})
}

func TestBoundedRunnerFencesDescendantAfterRootExit(t *testing.T) {
	req := runnerExecEventRequestForTest(t)
	pidPath := filepath.Join(req.WorkspacePath, "descendant.pid")
	req.Command = ""
	req.CommandArgv = []string{"/bin/sh", "-c", fmt.Sprintf(
		`sleep 30 & child=$!; printf '%%s\n' "$child" > %q; exit 0`,
		pidPath,
	)}
	req.RawLogMaxBytes = 1024
	started := time.Now()
	if _, err := executeRunnerCommand(context.Background(), RunnerCodexExec, req, RunnerCapabilities{}); err != nil {
		t.Fatal(err)
	}
	waitForStatusFile(t, req.StatusPath)
	if elapsed := time.Since(started); elapsed > 4*time.Second {
		t.Fatalf("descendant-held output pipe exceeded bounded cleanup: %s", elapsed)
	}
	rawPID, err := os.ReadFile(pidPath)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(rawPID)))
	if err != nil || pid <= 0 {
		t.Fatalf("invalid descendant pid %q: %v", rawPID, err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for processExists(pid) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if processExists(pid) {
		_ = syscall.Kill(pid, syscall.SIGKILL)
		t.Fatalf("descendant %d survived authoritative process-group fence", pid)
	}
}

func TestFrozenReviewProposalRejectsOversizeReplacementAndBoundsScanner(t *testing.T) {
	t.Run("oversize rejected before open", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "oversized.raw.log")
		file, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := file.Truncate(completionAuthoritativeRawLogMaxBytes + 1); err != nil {
			t.Fatal(err)
		}
		if err := file.Chmod(0o600); err != nil {
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
		opens := 0
		_, _, err = readFrozenReviewProposalLogWithOpen(path, func(path string) (*os.File, error) {
			opens++
			return os.Open(path)
		})
		if err == nil || opens != 0 {
			t.Fatalf("oversized log reached open/scan: opens=%d err=%v", opens, err)
		}
	})

	t.Run("symlink rejected before open", func(t *testing.T) {
		dir := t.TempDir()
		target := filepath.Join(dir, "target.raw.log")
		if err := os.WriteFile(target, []byte("ordinary\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, "attempt.raw.log")
		if err := os.Symlink(target, path); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		opens := 0
		_, _, err := readFrozenReviewProposalLogWithOpen(path, func(path string) (*os.File, error) {
			opens++
			return os.Open(path)
		})
		if err == nil || opens != 0 {
			t.Fatalf("symlinked log reached open/scan: opens=%d err=%v", opens, err)
		}
	})

	t.Run("writable and hard-linked logs rejected before open", func(t *testing.T) {
		dir := t.TempDir()
		writable := filepath.Join(dir, "writable.raw.log")
		if err := os.WriteFile(writable, []byte("ordinary\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(writable, 0o660); err != nil {
			t.Fatal(err)
		}
		opens := 0
		if _, _, err := readFrozenReviewProposalLogWithOpen(writable, func(path string) (*os.File, error) {
			opens++
			return os.Open(path)
		}); err == nil || opens != 0 {
			t.Fatalf("group-writable log reached open: opens=%d err=%v", opens, err)
		}

		hardlinked := filepath.Join(dir, "hardlinked.raw.log")
		if err := os.WriteFile(hardlinked, []byte("ordinary\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Link(hardlinked, filepath.Join(dir, "hardlink-alias")); err != nil {
			t.Skipf("hard links unavailable: %v", err)
		}
		opens = 0
		if _, _, err := readFrozenReviewProposalLogWithOpen(hardlinked, func(path string) (*os.File, error) {
			opens++
			return os.Open(path)
		}); err == nil || opens != 0 {
			t.Fatalf("hard-linked log reached open: opens=%d err=%v", opens, err)
		}
	})

	t.Run("replacement rejected before scan", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "attempt.raw.log")
		original := filepath.Join(dir, "original.raw.log")
		if err := os.WriteFile(path, []byte("ordinary\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		scanned := false
		_, _, err := readFrozenReviewProposalLogWithOpen(path, func(path string) (*os.File, error) {
			if err := os.Rename(path, original); err != nil {
				return nil, err
			}
			if err := os.WriteFile(path, []byte(reviewProposalMarker+`{"schema":"tusker.review-proposal/v1","attempt_id":"forged","result":{}}`+"\n"), 0o600); err != nil {
				return nil, err
			}
			scanned = true
			return os.Open(path)
		})
		if err == nil || !scanned || !strings.Contains(err.Error(), "changed while opening") {
			t.Fatalf("replacement was not rejected at the open identity check: scanned=%t err=%v", scanned, err)
		}
	})

	t.Run("scanner reads at most cap plus sentinel", func(t *testing.T) {
		source := &countingInfiniteReader{}
		_, _, err := scanReviewProposalLogWithLimit(source, 128)
		if err == nil || !strings.Contains(err.Error(), "exceeds") {
			t.Fatalf("unbounded source was not rejected: %v", err)
		}
		if source.read.Load() > 129 {
			t.Fatalf("scanner consumed %d bytes, want at most 129", source.read.Load())
		}
	})

	t.Run("exact cap proposal works", func(t *testing.T) {
		raw, err := json.Marshal(reviewProposal{Schema: reviewProposalSchema, AttemptID: "exact"})
		if err != nil {
			t.Fatal(err)
		}
		marker := reviewProposalMarker + string(raw) + "\n"
		const limit = int64(1024)
		padding := int(limit) - len(marker)
		input := strings.Repeat("x", padding-1) + "\n" + marker
		got, found, err := scanReviewProposalLogWithLimit(strings.NewReader(input), limit)
		if err != nil || !found || got.AttemptID != "exact" || int64(len(input)) != limit {
			t.Fatalf("exact-cap proposal rejected: proposal=%#v found=%t bytes=%d err=%v", got, found, len(input), err)
		}
	})
}

func TestCompletionAuthorityRejectsRawLogPolicyDriftBeforeExecutableUse(t *testing.T) {
	req := runnerExecEventRequestForTest(t)
	req.Command = ""
	req.CommandArgv = []string{"/nonexistent/codex", "exec", "-"}
	req.CommandExecutableFP = "sha256:" + strings.Repeat("a", 64)
	req.RawLogMaxBytes = completionAuthoritativeRawLogMaxBytes - 1
	if _, err := executeRunnerCommand(context.Background(), RunnerCodexExec, req, RunnerCapabilities{}); err == nil || !strings.Contains(err.Error(), "exact") {
		t.Fatalf("authoritative request accepted raw-log policy drift: %v", err)
	}
}

func TestBoundedRunnerTerminalStatusIsNoClobber(t *testing.T) {
	path := filepath.Join(t.TempDir(), "status.json")
	wrote, err := writeRunnerStatusFileIfAbsentWithOutcome(
		path,
		completionAuthoritativeRawLogOverflowExitCode,
		AttemptOutcomeFailed,
		"completion-authoritative raw log exceeded byte limit",
		0,
	)
	if err != nil || !wrote {
		t.Fatalf("publish overflow status: wrote=%t err=%v", wrote, err)
	}
	wrote, err = writeRunnerStatusFileIfAbsentWithOutcome(
		path,
		130,
		AttemptOutcomeInterrupted,
		"later cancellation",
		0,
	)
	if err != nil || wrote {
		t.Fatalf("later terminal writer replaced overflow: wrote=%t err=%v", wrote, err)
	}
	status, err := readRunnerProcessStatus(path)
	if err != nil ||
		status.ExitCode != completionAuthoritativeRawLogOverflowExitCode ||
		!strings.Contains(status.Reason, "raw log exceeded") {
		t.Fatalf("overflow status was not preserved: status=%#v err=%v", status, err)
	}
}

type countingInfiniteReader struct {
	read atomic.Int64
}

func (r *countingInfiniteReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 'x'
	}
	r.read.Add(int64(len(p)))
	return len(p), nil
}

func TestOpenBoundedRawLogRejectsSymlinkedParent(t *testing.T) {
	dir := t.TempDir()
	safe := filepath.Join(dir, "safe")
	outside := filepath.Join(dir, "outside")
	if err := os.Mkdir(safe, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(outside, 0o700); err != nil {
		t.Fatal(err)
	}
	redirect := filepath.Join(safe, "redirect")
	if err := os.Symlink(outside, redirect); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := openBoundedRawLog(filepath.Join(redirect, "raw.log"), 64, false); err == nil {
		t.Fatal("bounded raw log followed a symlinked parent")
	}
	entries, err := os.ReadDir(outside)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("symlink target received bounded raw log: %#v", entries)
	}
}

func assertRawLogSize(t *testing.T, path string, want int64) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() != want {
		t.Fatalf("raw log size=%d, want %d", info.Size(), want)
	}
}

var _ io.Reader = (*countingInfiniteReader)(nil)
