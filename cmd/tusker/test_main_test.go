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

var validationTestLockRetireHook func(string) error

func TestMain(m *testing.M) {
	// Keep ordinary daemon fixtures independent of the developer machine's
	// free-space budget. Dedicated disk-pressure tests inject d.diskStat and
	// therefore still exercise the real refusal/recovery paths.
	runtimeDiskStat = func(string) (diskFilesystemStat, error) {
		return diskFilesystemStat{Blocks: 100, AvailableBlocks: 90, BlockSize: 1 << 30}, nil
	}
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
	if err := os.Setenv("TUSKER_STATE_ROOT", stateRoot); err != nil {
		fmt.Fprintf(os.Stderr, "cmd/tusker test suite: isolate state root: %v\n", err)
		_ = os.RemoveAll(stateRoot)
		os.Exit(1)
	}
	// Repository fixtures must not inherit developer-machine hooks, signing
	// requirements, credential helpers, or interactive prompts. Those settings
	// made otherwise deterministic git commits execute the host Tusker hook and
	// fail because the temporary repository intentionally has no vault.
	for key, value := range map[string]string{
		"GIT_CONFIG_GLOBAL":   os.DevNull,
		"GIT_CONFIG_SYSTEM":   os.DevNull,
		"GIT_TERMINAL_PROMPT": "0",
		"GCM_INTERACTIVE":     "Never",
	} {
		if err := os.Setenv(key, value); err != nil {
			fmt.Fprintf(os.Stderr, "cmd/tusker test suite: isolate git setting %s: %v\n", key, err)
			_ = os.RemoveAll(stateRoot)
			os.Exit(1)
		}
	}

	cleanupNotifications, err := installEscalationTestNotifications()
	if err != nil {
		fmt.Fprintf(os.Stderr, "cmd/tusker test suite: configure escalation notifications: %v\n", err)
		_ = os.RemoveAll(stateRoot)
		os.Exit(1)
	}

	if os.Getenv(validationLockHeldEnv) != "" {
		code := m.Run()
		cleanupNotifications()
		if err := os.RemoveAll(stateRoot); err != nil {
			fmt.Fprintf(os.Stderr, "cmd/tusker test suite: remove isolated state root: %v\n", err)
			if code == 0 {
				code = 1
			}
		}
		os.Exit(code)
	}

	release, err := acquireValidationTestLock()
	if err != nil {
		fmt.Fprintf(os.Stderr, "cmd/tusker test suite: %v\n", err)
		cleanupNotifications()
		_ = os.RemoveAll(stateRoot)
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
	if err := os.RemoveAll(stateRoot); err != nil {
		fmt.Fprintf(os.Stderr, "cmd/tusker test suite: remove isolated state root: %v\n", err)
		if code == 0 {
			code = 1
		}
	}
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
	initializationGrace, err := validationTestLockInitializationGrace()
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
					releaseErr = retireValidationTestLock(lockDir, "released")
				})
				return releaseErr
			}, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, fmt.Errorf("create validation lock %s: %w", lockDir, err)
		}

		lockIdentity, identityErr := os.Lstat(lockDir)
		ownerPID := validationTestLockOwnerPID(lockDir)
		if ownerPID > 0 && !validationTestProcessAlive(ownerPID) {
			if identityErr == nil {
				if err := recoverValidationTestLock(lockDir, "stale", lockIdentity, initializationGrace); err == nil {
					fmt.Fprintf(os.Stderr, "cmd/tusker test suite: recovered stale validation owner pid %d\n", ownerPID)
					continue
				}
			}
		}
		if ownerPID == 0 && identityErr == nil && validationTestLockOlderThan(lockIdentity, initializationGrace) {
			if err := recoverValidationTestLock(lockDir, "abandoned", lockIdentity, initializationGrace); err == nil {
				fmt.Fprintln(os.Stderr, "cmd/tusker test suite: recovered abandoned validation lock initialization")
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

func validationTestLockInitializationGrace() (time.Duration, error) {
	value := os.Getenv("TUSKER_VALIDATION_LOCK_INITIALIZATION_GRACE_SECONDS")
	if value == "" {
		return 5 * time.Second, nil
	}
	grace, err := time.ParseDuration(value + "s")
	if err != nil || grace <= 0 {
		return 0, fmt.Errorf("invalid TUSKER_VALIDATION_LOCK_INITIALIZATION_GRACE_SECONDS %q", value)
	}
	return grace, nil
}

func writeValidationTestLockMetadata(lockDir, token string) error {
	// Publish the owner PID first. A process killed during initialization then
	// leaves either a recoverable dead owner or, only in the mkdir-to-first-write
	// window, an ownerless directory recovered after the initialization grace.
	metadata := []struct {
		name  string
		value string
	}{
		{name: "pid", value: strconv.Itoa(os.Getpid()) + "\n"},
		{name: "token", value: token + "\n"},
		{name: "cwd", value: mustValidationTestCWD() + "\n"},
		{name: "started_at", value: time.Now().UTC().Format(time.RFC3339) + "\n"},
	}
	for _, item := range metadata {
		if err := os.WriteFile(filepath.Join(lockDir, item.name), []byte(item.value), 0o600); err != nil {
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
	return validationTestLockOwnerPIDFromPath(filepath.Join(lockDir, "pid"))
}

func validationTestLockOwnerPIDFromPath(path string) int {
	raw, err := os.ReadFile(path)
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
	return validationTestProcessAlivePlatform(pid)
}

func validationTestLockOlderThan(info os.FileInfo, age time.Duration) bool {
	return info != nil && time.Since(info.ModTime()) >= age
}

func recoverValidationTestLock(lockDir, reason string, expected os.FileInfo, claimGrace time.Duration) error {
	current, err := os.Lstat(lockDir)
	if err != nil || expected == nil || !os.SameFile(expected, current) {
		return fmt.Errorf("validation lock identity changed before %s recovery", reason)
	}
	claimPath := filepath.Join(lockDir, "recovery")
	if err := claimValidationTestLockRecovery(lockDir, claimPath, expected, claimGrace); err != nil {
		return err
	}
	current, err = os.Lstat(lockDir)
	if err != nil || !os.SameFile(expected, current) {
		return fmt.Errorf("validation lock identity changed while claiming %s recovery", reason)
	}
	return retireValidationTestLock(lockDir, reason)
}

func claimValidationTestLockRecovery(lockDir, claimPath string, expected os.FileInfo, grace time.Duration) error {
	for attempt := 0; attempt < 3; attempt++ {
		current, err := os.Lstat(lockDir)
		if err != nil || !os.SameFile(expected, current) {
			return errors.New("validation lock identity changed before recovery claim")
		}
		claim, err := os.OpenFile(claimPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			if _, err = fmt.Fprintf(claim, "%d\n", os.Getpid()); err == nil {
				err = claim.Close()
			} else {
				_ = claim.Close()
			}
			if err != nil {
				removeValidationTestRecoveryClaim(lockDir, claimPath, expected)
			}
			return err
		}
		if !errors.Is(err, os.ErrExist) {
			return err
		}
		info, statErr := os.Lstat(claimPath)
		ownerPID := validationTestLockOwnerPIDFromPath(claimPath)
		if statErr != nil || !info.Mode().IsRegular() ||
			ownerPID > 0 && validationTestProcessAlive(ownerPID) ||
			ownerPID == 0 && !validationTestLockOlderThan(info, grace) {
			return errors.New("validation lock recovery is already claimed")
		}
		if !removeValidationTestRecoveryClaim(lockDir, claimPath, expected) {
			return errors.New("validation lock recovery claim changed")
		}
	}
	return errors.New("validation lock recovery claim did not converge")
}

func removeValidationTestRecoveryClaim(lockDir, claimPath string, expected os.FileInfo) bool {
	current, err := os.Lstat(lockDir)
	if err != nil || !os.SameFile(expected, current) {
		return false
	}
	return os.Remove(claimPath) == nil
}

func retireValidationTestLock(lockDir, reason string) error {
	retiredDir := fmt.Sprintf("%s.%s.%d.%d", lockDir, reason, os.Getpid(), time.Now().UnixNano())
	if err := os.Rename(lockDir, retiredDir); err != nil {
		return err
	}
	// The canonical lock name is already free. A kill during cleanup can leave
	// only an inert, uniquely named tombstone; it cannot block another suite.
	if validationTestLockRetireHook != nil {
		if err := validationTestLockRetireHook(retiredDir); err != nil {
			return err
		}
	}
	return removeValidationTestLock(retiredDir)
}

func removeValidationTestLock(lockDir string) error {
	for _, name := range []string{"pid", "token", "cwd", "started_at", "recovery"} {
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

func TestValidationSuiteLockRecoversAbandonedInitialization(t *testing.T) {
	lockDir := filepath.Join(t.TempDir(), "validation.lock")
	if err := os.Mkdir(lockDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(lockDir, "recovery"), []byte("99999999\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Minute)
	if err := os.Chtimes(lockDir, old, old); err != nil {
		t.Fatal(err)
	}

	restoreValidationLockEnv(t)
	t.Setenv("TUSKER_VALIDATION_LOCK_DIR", lockDir)
	t.Setenv("TUSKER_VALIDATION_LOCK_POLL_SECONDS", "0.01")
	t.Setenv("TUSKER_VALIDATION_LOCK_TIMEOUT_SECONDS", "1")
	t.Setenv("TUSKER_VALIDATION_LOCK_INITIALIZATION_GRACE_SECONDS", "0.01")
	release, err := acquireValidationTestLock()
	if err != nil {
		t.Fatal(err)
	}
	if owner := validationTestLockOwnerPID(lockDir); owner != os.Getpid() {
		t.Fatalf("recovered lock owner pid = %d, want %d", owner, os.Getpid())
	}
	if err := release(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(lockDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("released canonical lock remains: %v", err)
	}
}

func TestValidationSuiteLockReleaseFreesCanonicalNameBeforeCleanup(t *testing.T) {
	lockDir := filepath.Join(t.TempDir(), "validation.lock")
	restoreValidationLockEnv(t)
	t.Setenv("TUSKER_VALIDATION_LOCK_DIR", lockDir)
	t.Setenv("TUSKER_VALIDATION_LOCK_POLL_SECONDS", "0.01")
	t.Setenv("TUSKER_VALIDATION_LOCK_TIMEOUT_SECONDS", "1")
	release, err := acquireValidationTestLock()
	if err != nil {
		t.Fatal(err)
	}

	injected := errors.New("injected retirement interruption")
	t.Cleanup(func() { validationTestLockRetireHook = nil })
	validationTestLockRetireHook = func(string) error { return injected }
	if err := release(); !errors.Is(err, injected) {
		t.Fatalf("release error = %v, want injected interruption", err)
	}
	validationTestLockRetireHook = nil
	if _, err := os.Stat(lockDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("canonical lock remains after interrupted tombstone cleanup: %v", err)
	}

	secondRelease, err := acquireValidationTestLock()
	if err != nil {
		t.Fatalf("second suite could not acquire canonical name: %v", err)
	}
	if err := secondRelease(); err != nil {
		t.Fatal(err)
	}
}

func TestValidationSuiteLockRecoveryCannotRetireReplacementOwner(t *testing.T) {
	root := t.TempDir()
	lockDir := filepath.Join(root, "validation.lock")
	if err := os.Mkdir(lockDir, 0o755); err != nil {
		t.Fatal(err)
	}
	staleIdentity, err := os.Lstat(lockDir)
	if err != nil {
		t.Fatal(err)
	}
	retired := filepath.Join(root, "old.lock")
	if err := os.Rename(lockDir, retired); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(lockDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(lockDir, "pid"), []byte(strconv.Itoa(os.Getpid())+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := recoverValidationTestLock(lockDir, "stale", staleIdentity, time.Millisecond); err == nil {
		t.Fatal("stale recovery retired a replacement owner's lock")
	}
	if owner := validationTestLockOwnerPID(lockDir); owner != os.Getpid() {
		t.Fatalf("replacement lock owner = %d, want %d", owner, os.Getpid())
	}
}

func restoreValidationLockEnv(t *testing.T) {
	t.Helper()
	original, present := os.LookupEnv(validationLockHeldEnv)
	t.Cleanup(func() {
		if present {
			_ = os.Setenv(validationLockHeldEnv, original)
		} else {
			_ = os.Unsetenv(validationLockHeldEnv)
		}
	})
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
