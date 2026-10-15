package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

func TestNativeRunnerExitDrainsFinalOutputWithInheritedPipes(t *testing.T) {
	for _, runner := range []RunnerName{RunnerClaude, RunnerCodexAppServer} {
		t.Run(string(runner), func(t *testing.T) {
			dir := t.TempDir()
			childPath := filepath.Join(dir, "child.pid")
			final := `{"type":"result","subtype":"success","is_error":false,"session_id":"final-session"}`
			if runner == RunnerCodexAppServer {
				final = `{"jsonrpc":"2.0","method":"turn/completed","params":{"threadId":"final-session","turn":{"id":"final-turn","status":"completed"}}}`
			}
			cmd := exec.Command("sh", "-c", "sleep 30 & echo $! > \"$1\"; printf '%s\\n' \"$2\"; exit 0", "fixture", childPath, final)
			cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
			pipes, err := openNativeRunnerPipes(cmd)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				pipes.close()
				if cmd.Process != nil {
					_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
				}
			})
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			pipes.closeWriters()
			rawPath, statusPath := filepath.Join(dir, "raw.log"), filepath.Join(dir, "status.json")
			done := make(chan struct{})
			if runner == RunnerClaude {
				h := &claudeLiveHandle{attemptID: "fixture-claude", runner: runner, cmd: cmd, stdout: pipes.stdout, stderr: pipes.stderr, rawLogPath: rawPath, statusPath: statusPath}
				h.ioWG.Add(2)
				go h.readStdout()
				go h.readStderr()
				go func() { h.waitForExit(); close(done) }()
			} else {
				h := &codexLiveHandle{attemptID: "fixture-codex", runner: runner, cmd: cmd, stdout: pipes.stdout, stderr: pipes.stderr, rawLogPath: rawPath, statusPath: statusPath}
				h.ioWG.Add(2)
				go h.readStdout()
				go h.readStderr()
				go func() { h.waitForExit(); close(done) }()
			}
			select {
			case <-done:
			case <-time.After(4 * time.Second):
				t.Fatal("inherited output pipe prevented root exit from completing")
			}
			raw, err := readText(rawPath)
			if err != nil || !strings.Contains(raw, "final-session") {
				t.Fatalf("final output lost: %q err=%v", raw, err)
			}
			status, err := readRunnerProcessStatus(statusPath)
			if err != nil || status.ExitCode != 0 {
				t.Fatalf("exit status: %#v err=%v", status, err)
			}
			pidText, err := readText(childPath)
			if err != nil {
				t.Fatal(err)
			}
			pid, err := strconv.Atoi(strings.TrimSpace(pidText))
			if err != nil {
				t.Fatal(err)
			}
			waitForProcessGone(t, pid, "inherited pipe descendant")
		})
	}
}

func TestNativeRunnerRawLogOverflowCannotPublishSuccess(t *testing.T) {
	for _, runner := range []RunnerName{RunnerClaude, RunnerCodexAppServer} {
		t.Run(string(runner), func(t *testing.T) {
			dir := t.TempDir()
			path, statusPath := filepath.Join(dir, "raw.log"), filepath.Join(dir, "status.json")
			log, err := openNativeRunnerRawLog(StartRequest{RawLogPath: path, RawLogMaxBytes: 64}, false)
			if err != nil {
				t.Fatal(err)
			}
			defer log.close()
			var appendLine func(string) error
			var finalize func(int)
			if runner == RunnerClaude {
				h := &claudeLiveHandle{runner: runner, rawLogPath: path, rawLog: log, statusPath: statusPath}
				appendLine, finalize = h.appendRawLogLine, h.finalize
			} else {
				h := &codexLiveHandle{runner: runner, rawLogPath: path, rawLog: log, statusPath: statusPath}
				appendLine, finalize = h.appendRawLogLine, h.finalize
			}
			var wg sync.WaitGroup
			for range 2 {
				wg.Add(1)
				go func() { defer wg.Done(); _ = appendLine(strings.Repeat("x", 40)) }()
			}
			wg.Wait()
			finalize(0)
			status, err := readRunnerProcessStatus(statusPath)
			if err != nil || status.ExitCode != completionAuthoritativeRawLogOverflowExitCode || status.Outcome != string(AttemptOutcomeFailed) {
				t.Fatalf("overflow became success: %#v err=%v", status, err)
			}
			assertRawLogSize(t, path, 64)
		})
	}
}

func TestNativeRunnerFinalResultWaitsForBoundedStderr(t *testing.T) {
	for _, runner := range []RunnerName{RunnerClaude, RunnerCodexAppServer} {
		t.Run(string(runner), func(t *testing.T) {
			dir := t.TempDir()
			path, statusPath := filepath.Join(dir, "raw.log"), filepath.Join(dir, "status.json")
			log, err := openNativeRunnerRawLog(StartRequest{RawLogPath: path, RawLogMaxBytes: 512}, false)
			if err != nil {
				t.Fatal(err)
			}
			defer log.close()
			final := `{"type":"result","subtype":"success","is_error":false}`
			if runner == RunnerCodexAppServer {
				final = `{"jsonrpc":"2.0","method":"turn/completed","params":{"turn":{"id":"final-turn","status":"completed"}}}`
			}
			cmd := exec.Command("sh", "-c", "trap '' TERM; printf '%s\\n' \"$1\"; sleep .1; printf '%s\\n' \"$2\" >&2; sleep 30", "fixture", final, strings.Repeat("x", 1024))
			cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
			pipes, err := openNativeRunnerPipes(cmd)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				pipes.close()
				if cmd.Process != nil {
					_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
				}
			})
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			pipes.closeWriters()
			done := make(chan struct{})
			if runner == RunnerClaude {
				h := &claudeLiveHandle{runner: runner, cmd: cmd, stdout: pipes.stdout, stderr: pipes.stderr, rawLogPath: path, rawLog: log, statusPath: statusPath}
				h.ioWG.Add(2)
				go h.readStdout()
				go h.readStderr()
				go func() { h.waitForExit(); close(done) }()
			} else {
				h := &codexLiveHandle{runner: runner, cmd: cmd, stdout: pipes.stdout, stderr: pipes.stderr, rawLogPath: path, rawLog: log, statusPath: statusPath}
				h.ioWG.Add(2)
				go h.readStdout()
				go h.readStderr()
				go func() { h.waitForExit(); close(done) }()
			}
			select {
			case <-done:
			case <-time.After(4 * time.Second):
				t.Fatal("completed result did not bound process shutdown")
			}
			status, err := readRunnerProcessStatus(statusPath)
			if err != nil || status.ExitCode != completionAuthoritativeRawLogOverflowExitCode || status.Outcome != string(AttemptOutcomeFailed) {
				t.Fatalf("late stderr overflow became success: %#v err=%v", status, err)
			}
			assertRawLogSize(t, path, 512)
		})
	}
}

func TestNativeRunnerResultEscalatesIgnoredTermination(t *testing.T) {
	dir := t.TempDir()
	cmd := exec.Command("sh", "-c", "trap '' TERM; printf '%s\\n' '{\"type\":\"result\",\"subtype\":\"success\",\"is_error\":false}'; while :; do sleep 30; done")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	pipes, err := openNativeRunnerPipes(cmd)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pipes.close()
		if cmd.Process != nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
	})
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	pipes.closeWriters()
	h := &claudeLiveHandle{runner: RunnerClaude, cmd: cmd, stdout: pipes.stdout, stderr: pipes.stderr, rawLogPath: filepath.Join(dir, "raw.log"), statusPath: filepath.Join(dir, "status.json")}
	h.ioWG.Add(2)
	go h.readStdout()
	go h.readStderr()
	done := make(chan struct{})
	go func() { h.waitForExit(); close(done) }()
	select {
	case <-done:
	case <-time.After(4 * time.Second):
		t.Fatal("provider ignored TERM and kept terminal status pending")
	}
	status, err := readRunnerProcessStatus(h.statusPath)
	if err != nil || status.ExitCode != 0 {
		t.Fatalf("typed final result was lost during escalation: %#v err=%v", status, err)
	}
}

func TestNativeRunnerRawLogCloseIsSafeAndPrivacyValidated(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "raw.log")
	log, err := openNativeRunnerRawLog(StartRequest{RawLogPath: path, RawLogMaxBytes: 64}, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := log.close(); err != nil {
		t.Fatal(err)
	}
	if err := log.close(); err != nil {
		t.Fatal(err)
	}
	if _, err := log.Write([]byte("after close")); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("write after close: %v", err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := openNativeRunnerRawLog(StartRequest{RawLogPath: path}, true); err == nil {
		t.Fatal("public resumed raw log accepted")
	}
}
