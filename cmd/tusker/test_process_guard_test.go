package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// processGuardSleeperEnv makes the test binary sleep instead of running the
// suite; TestProcessGuardDetectsLeakedChild uses it as a stand-in leak.
const processGuardSleeperEnv = "TUSKER_PROCESS_GUARD_SLEEPER"

// processGuardRootEnv carries the top-level suite's pid. Test binaries that
// tests spawn inherit it and skip the leak guard: they share the binary path
// with their siblings and may deliberately outlive-and-clean their children.
const processGuardRootEnv = "TUSKER_TEST_GUARD_ROOT"

// claimSuiteGuard reports whether this process is the top-level suite, and
// marks it so nested test-binary children skip the guard.
func claimSuiteGuard() bool {
	if os.Getenv(processGuardRootEnv) != "" {
		return false
	}
	_ = os.Setenv(processGuardRootEnv, strconv.Itoa(os.Getpid()))
	return true
}

type suiteProcess struct {
	PID, PPID, PGID int
	Command         string
}

// listProcesses snapshots every live (non-zombie) process except the ps
// command itself. Works with BSD and procps ps.
func listProcesses() ([]suiteProcess, error) {
	var out bytes.Buffer
	cmd := exec.Command("ps", "-A", "-ww", "-o", "pid=,ppid=,pgid=,stat=,command=")
	cmd.Stdout = &out
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	psPID := cmd.Process.Pid
	if err := cmd.Wait(); err != nil {
		return nil, err
	}
	var procs []suiteProcess
	for _, line := range strings.Split(out.String(), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 5 || strings.HasPrefix(fields[3], "Z") {
			continue
		}
		pid, err1 := strconv.Atoi(fields[0])
		ppid, err2 := strconv.Atoi(fields[1])
		pgid, err3 := strconv.Atoi(fields[2])
		if err1 != nil || err2 != nil || err3 != nil || pid == psPID {
			continue
		}
		procs = append(procs, suiteProcess{PID: pid, PPID: ppid, PGID: pgid, Command: strings.Join(fields[4:], " ")})
	}
	return procs, nil
}

// suiteLeaks returns live descendants of root plus, when binary is set, any
// process running that exact binary. Detached wrappers keep the test binary as
// parent until it exits; the binary match also catches orphaned grandchildren.
func suiteLeaks(procs []suiteProcess, root int, binary string) []suiteProcess {
	children := map[int][]suiteProcess{}
	for _, p := range procs {
		children[p.PPID] = append(children[p.PPID], p)
	}
	seen := map[int]bool{root: true}
	var leaks []suiteProcess
	queue := []int{root}
	for len(queue) > 0 {
		parent := queue[0]
		queue = queue[1:]
		for _, child := range children[parent] {
			if !seen[child.PID] {
				seen[child.PID] = true
				leaks = append(leaks, child)
				queue = append(queue, child.PID)
			}
		}
	}
	if binary != "" {
		// A nested run of the same binary must not kill the suite that started it.
		parentOf := map[int]int{}
		for _, p := range procs {
			parentOf[p.PID] = p.PPID
		}
		for pid := parentOf[root]; pid > 1 && !seen[pid]; pid = parentOf[pid] {
			seen[pid] = true
		}
		for _, p := range procs {
			if !seen[p.PID] && (p.Command == binary || strings.HasPrefix(p.Command, binary+" ")) {
				seen[p.PID] = true
				leaks = append(leaks, p)
			}
		}
	}
	return leaks
}

// killSuiteProcess SIGKILLs the process, and its whole group when it leads one
// that is not the suite's own group.
// ponytail: no start-time check between ps and kill; PID reuse in that window is
// accepted for a test harness.
func killSuiteProcess(p suiteProcess) {
	if p.PGID == p.PID && p.PGID != syscall.Getpgrp() {
		_ = syscall.Kill(-p.PGID, syscall.SIGKILL)
	}
	_ = syscall.Kill(p.PID, syscall.SIGKILL)
}

// killSuiteLeaks runs after m.Run: it kills and names every process the suite
// left alive and reports false when there were any.
func killSuiteLeaks() bool {
	procs, err := listProcesses()
	if err != nil {
		fmt.Fprintf(os.Stderr, "cmd/tusker test suite: leak check could not list processes: %v\n", err)
		return false
	}
	binary, _ := os.Executable()
	leaks := suiteLeaks(procs, os.Getpid(), binary)
	if len(leaks) == 0 {
		return true
	}
	fmt.Fprintf(os.Stderr, "cmd/tusker test suite: FAIL: %d leaked process(es) killed:\n", len(leaks))
	for _, p := range leaks {
		killSuiteProcess(p)
		fmt.Fprintf(os.Stderr, "  pid %d pgid %d: %s\n", p.PID, p.PGID, p.Command)
	}
	return false
}

// reportStaleTestWrappers names wrappers left by an earlier, killed suite run.
// It only reports: a concurrent suite may own them.
func reportStaleTestWrappers() {
	procs, err := listProcesses()
	if err != nil {
		return
	}
	self, _ := os.Executable()
	for _, p := range procs {
		exe, rest, _ := strings.Cut(p.Command, " ")
		if strings.HasSuffix(exe, ".test") && exe != self && strings.HasPrefix(rest, "runner-wrapper") {
			fmt.Fprintf(os.Stderr, "cmd/tusker test suite: WARNING: leftover test wrapper from an earlier run: pid %d: %s\n", p.PID, p.Command)
		}
	}
}

func TestProcessGuardWrapperArgsDoNotRunSuite(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, "runner-wrapper", "--request", t.TempDir()+"/missing-request.json")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	// If the guard regresses this child runs the whole suite; kill its group.
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	output, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("test binary started as runner-wrapper ran the suite instead of the wrapper:\n%s", output)
	}
	if err == nil {
		t.Fatalf("runner-wrapper with a missing request succeeded:\n%s", output)
	}
	if text := string(output); strings.Contains(text, "PASS") || strings.Contains(text, "--- FAIL") || !strings.Contains(text, "tusker runner-wrapper --help") {
		t.Fatalf("runner-wrapper output is not the wrapper's request error:\n%s", text)
	}
}

// TestProcessGuardLeakProbe leaks a detached sleeper on purpose, only when run
// as a child by TestProcessGuardDetectsLeakedChild.
func TestProcessGuardLeakProbe(t *testing.T) {
	if os.Getenv("TUSKER_PROCESS_GUARD_LEAK_PROBE") != "1" {
		return
	}
	exe, _ := os.Executable()
	cmd := exec.Command(exe)
	cmd.Env = append(os.Environ(), processGuardSleeperEnv+"=1")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	fmt.Printf("leak-probe-pid=%d\n", cmd.Process.Pid)
}

func TestProcessGuardDetectsLeakedChild(t *testing.T) {
	t.Run("suite fails and names the leak", func(t *testing.T) {
		exe, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(exe, "-test.run=^TestProcessGuardLeakProbe$", "-test.count=1", "-test.v")
		// Clear the guard root so this child runs the guard as a top-level suite.
		cmd.Env = append(os.Environ(), "TUSKER_PROCESS_GUARD_LEAK_PROBE=1", processGuardRootEnv+"=")
		output, err := cmd.CombinedOutput()
		text := string(output)
		_, rest, ok := strings.Cut(text, "leak-probe-pid=")
		if !ok {
			t.Fatalf("probe did not report its child:\n%s", text)
		}
		pid, _ := strconv.Atoi(strings.Fields(rest)[0])
		if pid <= 0 {
			t.Fatalf("probe pid unreadable:\n%s", text)
		}
		t.Cleanup(func() { _ = syscall.Kill(-pid, syscall.SIGKILL) })
		if err == nil || !strings.Contains(text, "leaked process") || !strings.Contains(text, fmt.Sprintf("pid %d ", pid)) {
			t.Fatalf("suite with a leaked child did not fail naming pid %d (err %v):\n%s", pid, err, text)
		}
		deadline := time.Now().Add(5 * time.Second)
		for syscall.Kill(pid, 0) == nil {
			if time.Now().After(deadline) {
				t.Fatalf("leaked pid %d still alive after the suite's leak check", pid)
			}
			time.Sleep(20 * time.Millisecond)
		}
	})

	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe)
	cmd.Env = append(os.Environ(), processGuardSleeperEnv+"=1")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	pid := cmd.Process.Pid
	t.Cleanup(func() { _ = syscall.Kill(-pid, syscall.SIGKILL); _ = cmd.Wait() })

	procs, err := listProcesses()
	if err != nil {
		t.Fatal(err)
	}
	var leaked *suiteProcess
	for _, p := range suiteLeaks(procs, os.Getpid(), "") {
		if p.PID == pid {
			leaked = &p
		}
	}
	if leaked == nil {
		t.Fatalf("leak check did not find detached child pid %d", pid)
	}
	if leaked.PGID != pid || !strings.HasPrefix(leaked.Command, exe) {
		t.Fatalf("leaked child = %+v, want its own group running %s", *leaked, exe)
	}

	killSuiteProcess(*leaked)
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if status, ok := cmd.ProcessState.Sys().(syscall.WaitStatus); !ok || status.Signal() != syscall.SIGKILL {
			t.Fatalf("leaked child exit = %v, want SIGKILL", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("leaked child pid %d survived the kill", pid)
	}
}
