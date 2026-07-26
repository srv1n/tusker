package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

const validationLockHeldEnv = "TUSKER_VALIDATION_LOCK_HELD"

func TestMain(m *testing.M) {
	switch os.Getenv("TUSKER_V7_PIPE_HOLDER") {
	case "parent":
		child := exec.Command(os.Args[0])
		child.Env = []string{"TUSKER_V7_PIPE_HOLDER=child"}
		child.Stdout, child.Stderr = os.Stdout, os.Stderr
		if err := child.Start(); err != nil {
			os.Exit(2)
		}
		os.Exit(0)
	case "child":
		time.Sleep(2 * time.Second)
		os.Exit(0)
	}
	if os.Getenv("TUSKER_V7_FD_TRANSPORT_HELPER") == "1" {
		os.Exit(runV7FDTransportProcessHelper())
	}
	stateRoot, err := os.MkdirTemp("", "tusker-test-state-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "cmd/tusker test suite: create isolated state root: %v\n", err)
		os.Exit(1)
	}
	defer os.RemoveAll(stateRoot)
	if err := os.Setenv("TUSKER_STATE_ROOT", stateRoot); err != nil {
		fmt.Fprintf(os.Stderr, "cmd/tusker test suite: isolate state root: %v\n", err)
		os.Exit(1)
	}

	cleanupNotifications, err := installEscalationTestNotifications()
	if err != nil {
		fmt.Fprintf(os.Stderr, "cmd/tusker test suite: configure escalation notifications: %v\n", err)
		os.Exit(1)
	}

	if os.Getenv(validationLockHeldEnv) != "" {
		code := m.Run()
		cleanupNotifications()
		os.Exit(code)
	}

	release, err := acquireValidationTestLock()
	if err != nil {
		fmt.Fprintf(os.Stderr, "cmd/tusker test suite: %v\n", err)
		os.Exit(1)
	}
	stopSignalCleanup := installValidationTestLockSignalCleanup(release)
	code := m.Run()
	if err := release(); err != nil {
		fmt.Fprintf(os.Stderr, "cmd/tusker test suite: release validation lock: %v\n", err)
		if code == 0 {
			code = 1
		}
	}
	stopSignalCleanup()
	cleanupNotifications()
	os.Exit(code)
}

func runV7FDTransportProcessHelper() int {
	if len(os.Args) != 3 || os.Args[1] != "--tusker-full-gate-run" && os.Args[1] != "--tusker-full-gate-cleanup" || os.Args[2] != "/dev/fd/3" {
		return 91
	}
	raw, err := os.ReadFile(os.Args[2])
	if err != nil {
		return 92
	}
	var request v7FullGateProviderRequest
	if json.Unmarshal(raw, &request) != nil || request.ResultPath != "/dev/fd/4" {
		return 93
	}
	if writable, err := os.OpenFile("/dev/fd/3", os.O_WRONLY, 0); err == nil {
		_ = writable.Close()
		return 96
	}
	for fd := 5; fd < 64; fd++ {
		if info, err := os.Stat("/dev/fd/" + strconv.Itoa(fd)); err == nil && info.IsDir() {
			return 95
		}
	}
	if err := os.WriteFile(request.ResultPath, []byte("darwin descriptor transport "+os.Args[1]+"\n"), 0o600); err != nil {
		fmt.Fprintf(os.Stderr, "write inherited result descriptor: %v\n", err)
		return 94
	}
	return 0
}

func installValidationTestLockSignalCleanup(release func() error) func() {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	go func() {
		sig := <-signals
		if err := release(); err != nil {
			fmt.Fprintf(os.Stderr, "cmd/tusker test suite: release validation lock after %s: %v\n", sig, err)
		}
		signal.Stop(signals)
		signal.Reset(sig)
		if err := syscall.Kill(os.Getpid(), sig.(syscall.Signal)); err != nil {
			fmt.Fprintf(os.Stderr, "cmd/tusker test suite: re-deliver %s after validation lock cleanup: %v\n", sig, err)
			os.Exit(1)
		}
	}()
	return func() { signal.Stop(signals) }
}

func acquireValidationTestLock() (func() error, error) {
	lockDir, err := validationTestLockDir()
	if err != nil {
		return nil, err
	}
	poll, err := validationTestLockPoll()
	if err != nil {
		return nil, err
	}
	timeout, err := validationTestLockTimeout()
	if err != nil {
		return nil, err
	}

	token := fmt.Sprintf("%d-%d-%d", os.Getpid(), time.Now().UnixNano(), os.Getppid())
	started := time.Now()
	waitReported := false
	for {
		err := os.Mkdir(lockDir, 0o755)
		if err == nil {
			if err := writeValidationTestLockMetadata(lockDir, token); err != nil {
				_ = removeValidationTestLock(lockDir)
				return nil, fmt.Errorf("initialize validation lock %s: %w", lockDir, err)
			}
			if err := os.Setenv(validationLockHeldEnv, token); err != nil {
				_ = removeValidationTestLock(lockDir)
				return nil, fmt.Errorf("mark validation lock held: %w", err)
			}
			if waitReported {
				fmt.Fprintf(os.Stderr, "cmd/tusker test suite: acquired validation lock after %s\n", time.Since(started).Round(time.Millisecond))
			}
			var releaseOnce sync.Once
			var releaseErr error
			return func() error {
				releaseOnce.Do(func() {
					_ = os.Unsetenv(validationLockHeldEnv)
					ownerToken, _ := os.ReadFile(filepath.Join(lockDir, "token"))
					if strings.TrimSpace(string(ownerToken)) != token {
						return
					}
					releaseErr = removeValidationTestLock(lockDir)
				})
				return releaseErr
			}, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, fmt.Errorf("create validation lock %s: %w", lockDir, err)
		}

		ownerPID := validationTestLockOwnerPID(lockDir)
		if ownerPID > 0 && !validationTestProcessAlive(ownerPID) {
			staleDir := fmt.Sprintf("%s.stale.%d.%d", lockDir, os.Getpid(), time.Now().UnixNano())
			if err := os.Rename(lockDir, staleDir); err == nil {
				fmt.Fprintf(os.Stderr, "cmd/tusker test suite: recovered stale validation owner pid %d\n", ownerPID)
				if err := removeValidationTestLock(staleDir); err != nil {
					return nil, fmt.Errorf("remove stale validation lock: %w", err)
				}
				continue
			}
		}

		if !waitReported {
			owner := "initializing"
			if ownerPID > 0 {
				owner = strconv.Itoa(ownerPID)
			}
			fmt.Fprintf(os.Stderr, "cmd/tusker test suite: validation gate busy (owner pid %s); waiting\n", owner)
			waitReported = true
		}
		if timeout > 0 && time.Since(started) >= timeout {
			return nil, fmt.Errorf("timed out after %s waiting for validation lock %s", timeout, lockDir)
		}
		time.Sleep(poll)
	}
}

func validationTestLockDir() (string, error) {
	if configured := os.Getenv("TUSKER_VALIDATION_LOCK_DIR"); configured != "" {
		return configured, nil
	}
	output, err := exec.Command("git", "rev-parse", "--git-common-dir").Output()
	if err != nil {
		cwd, cwdErr := os.Getwd()
		if cwdErr != nil {
			return "", fmt.Errorf("resolve cwd after git-common-dir failure: %w", cwdErr)
		}
		return filepath.Join(os.TempDir(), fmt.Sprintf("tusker-validation-%x.lock", []byte(cwd))), nil
	}
	commonDir := strings.TrimSpace(string(output))
	if !filepath.IsAbs(commonDir) {
		commonDir, err = filepath.Abs(commonDir)
		if err != nil {
			return "", fmt.Errorf("resolve git common dir: %w", err)
		}
	}
	if physical, evalErr := filepath.EvalSymlinks(commonDir); evalErr == nil {
		commonDir = physical
	}
	return filepath.Join(commonDir, "tusker-validation.lock"), nil
}

func validationTestLockPoll() (time.Duration, error) {
	value := os.Getenv("TUSKER_VALIDATION_LOCK_POLL_SECONDS")
	if value == "" {
		return time.Second, nil
	}
	poll, err := time.ParseDuration(value + "s")
	if err != nil || poll <= 0 {
		return 0, fmt.Errorf("invalid TUSKER_VALIDATION_LOCK_POLL_SECONDS %q", value)
	}
	return poll, nil
}

func validationTestLockTimeout() (time.Duration, error) {
	value := os.Getenv("TUSKER_VALIDATION_LOCK_TIMEOUT_SECONDS")
	if value == "" {
		return 30 * time.Minute, nil
	}
	seconds, err := strconv.Atoi(value)
	if err != nil || seconds < 0 {
		return 0, fmt.Errorf("invalid TUSKER_VALIDATION_LOCK_TIMEOUT_SECONDS %q", value)
	}
	return time.Duration(seconds) * time.Second, nil
}

func writeValidationTestLockMetadata(lockDir, token string) error {
	metadata := map[string]string{
		"token":      token + "\n",
		"pid":        strconv.Itoa(os.Getpid()) + "\n",
		"cwd":        mustValidationTestCWD() + "\n",
		"started_at": time.Now().UTC().Format(time.RFC3339) + "\n",
	}
	for name, value := range metadata {
		if err := os.WriteFile(filepath.Join(lockDir, name), []byte(value), 0o600); err != nil {
			return err
		}
	}
	return nil
}

func mustValidationTestCWD() string {
	cwd, err := os.Getwd()
	if err != nil {
		return "unknown"
	}
	return cwd
}

func validationTestLockOwnerPID(lockDir string) int {
	raw, err := os.ReadFile(filepath.Join(lockDir, "pid"))
	if err != nil {
		return 0
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(raw)))
	return pid
}

func validationTestProcessAlive(pid int) bool {
	if pid == os.Getpid() {
		return true
	}
	if runtime.GOOS == "windows" {
		return true
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = process.Signal(syscall.Signal(0))
	return err == nil || errors.Is(err, syscall.EPERM)
}

func removeValidationTestLock(lockDir string) error {
	for _, name := range []string{"pid", "token", "cwd", "started_at"} {
		if err := os.Remove(filepath.Join(lockDir, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	if err := os.Remove(lockDir); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func TestValidationSuiteLockProbe(t *testing.T) {
	eventsPath := os.Getenv("TUSKER_TEST_SUITE_PROBE_EVENTS")
	if eventsPath == "" {
		return
	}
	holdMillis, err := strconv.Atoi(os.Getenv("TUSKER_TEST_SUITE_PROBE_HOLD_MS"))
	if err != nil || holdMillis < 0 {
		t.Fatalf("invalid probe hold milliseconds: %q", os.Getenv("TUSKER_TEST_SUITE_PROBE_HOLD_MS"))
	}
	appendProbeEvent(t, eventsPath, "start")
	time.Sleep(time.Duration(holdMillis) * time.Millisecond)
	appendProbeEvent(t, eventsPath, "end")
}

func appendProbeEvent(t *testing.T, path, kind string) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fmt.Fprintf(file, "%s %d\n", kind, os.Getpid()); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestValidationSuiteLockReleasedOnTERM(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("TERM is not a process-termination signal on Windows")
	}

	tempDir := t.TempDir()
	testBinary := filepath.Join(tempDir, "tusker.test")
	build := exec.Command("go", "test", "-c", "-o", testBinary, ".")
	build.Dir = mustValidationTestCWD()
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build isolated test binary: %v\n%s", err, output)
	}

	lockDir := filepath.Join(tempDir, "validation.lock")
	eventsPath := filepath.Join(tempDir, "events")
	first := validationTestBinaryCommand(testBinary, lockDir, eventsPath, 30_000)
	if err := first.Start(); err != nil {
		t.Fatalf("start lock holder: %v", err)
	}
	firstWaited := false
	t.Cleanup(func() {
		if first.Process != nil && !firstWaited {
			_ = first.Process.Kill()
			_ = first.Wait()
		}
	})

	waitForValidationTestPath(t, filepath.Join(lockDir, "pid"))
	waitForValidationTestPath(t, eventsPath)
	if err := first.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("send TERM to lock holder: %v", err)
	}
	err := first.Wait()
	firstWaited = true
	assertValidationTestTerminatedBySignal(t, err, syscall.SIGTERM)
	if _, err := os.Stat(lockDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("validation lock remains after TERM: %v", err)
	}

	second := validationTestBinaryCommand(testBinary, lockDir, filepath.Join(tempDir, "second-events"), 0)
	if output, err := second.CombinedOutput(); err != nil {
		t.Fatalf("second lock holder could not acquire lock: %v\n%s", err, output)
	}
}

func validationTestBinaryCommand(testBinary, lockDir, eventsPath string, holdMillis int) *exec.Cmd {
	command := exec.Command(testBinary, "-test.run=^TestValidationSuiteLockProbe$", "-test.count=1")
	command.Env = validationTestBinaryEnv(map[string]string{
		"TUSKER_VALIDATION_LOCK_DIR":          lockDir,
		"TUSKER_VALIDATION_LOCK_POLL_SECONDS": "0.01",
		"TUSKER_TEST_SUITE_PROBE_EVENTS":      eventsPath,
		"TUSKER_TEST_SUITE_PROBE_HOLD_MS":     strconv.Itoa(holdMillis),
	})
	return command
}

func validationTestBinaryEnv(overrides map[string]string) []string {
	env := make([]string, 0, len(os.Environ())+len(overrides))
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if key == validationLockHeldEnv {
			continue
		}
		if _, overridden := overrides[key]; !overridden {
			env = append(env, entry)
		}
	}
	for key, value := range overrides {
		env = append(env, key+"="+value)
	}
	return env
}

func waitForValidationTestPath(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", path)
}

func assertValidationTestTerminatedBySignal(t *testing.T, err error, signal syscall.Signal) {
	t.Helper()
	if err == nil {
		t.Fatal("lock holder exited successfully after TERM")
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("wait for lock holder: %v", err)
	}
	status, ok := exitErr.Sys().(syscall.WaitStatus)
	if !ok || !status.Signaled() || status.Signal() != signal {
		t.Fatalf("lock holder exit status = %#v, want signal %s", exitErr.Sys(), signal)
	}
}
