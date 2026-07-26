package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestV7GateSandboxCancellationContainsSetsidChild(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Darwin sandbox-exec containment contract")
	}
	sandbox, err := newV7GateSandbox(t.TempDir(), false)
	if err != nil {
		t.Skip("sandbox-exec unavailable: " + err.Error())
	}
	defer sandbox.Close()
	marker := filepath.Join(sandbox.scratchPath, "setsid-child.pid")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	inner := fmt.Sprintf(
		`$p=fork(); if ($p) { waitpid($p, 0); } else { POSIX::setsid(); open(my $f, ">", %q) or die $!; print $f $$; close($f); sleep 300; }`,
		marker,
	)
	go func() {
		output, runErr := sandbox.Run(ctx, "/usr/bin/perl -MPOSIX -e "+shellQuoteForSandboxTest(inner))
		if runErr != nil {
			runErr = fmt.Errorf("%w: %s", runErr, strings.TrimSpace(string(output)))
		}
		done <- runErr
	}()
	childPID := waitForV7GatePID(t, marker, done)
	if got := processGroupID(childPID); got != childPID {
		t.Fatalf("setsid child did not escape its original process group: pid=%d pgid=%d", childPID, got)
	}
	cancel()
	select {
	case runErr := <-done:
		if !errors.Is(runErr, context.Canceled) {
			t.Fatalf("cancelled sandbox gate error = %v, want context cancellation", runErr)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled sandbox gate did not reap")
	}
	if processExists(childPID) {
		t.Fatalf("setsid child survived gate cancellation: pid=%d pgid=%d", childPID, processGroupID(childPID))
	}
}

func TestV7GateSandboxCancellationFailsClosedWhenContainmentCannotSnapshot(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Darwin sandbox-exec containment contract")
	}
	sandbox, err := newV7GateSandbox(t.TempDir(), false)
	if err != nil {
		t.Skip("sandbox-exec unavailable: " + err.Error())
	}
	defer sandbox.Close()
	marker := filepath.Join(sandbox.scratchPath, "root-child.pid")
	oldSnapshot := v7GateProcessSnapshot
	v7GateProcessSnapshot = func() ([]v7GateProcess, error) { return nil, fmt.Errorf("forced snapshot failure") }
	defer func() { v7GateProcessSnapshot = oldSnapshot }()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, runErr := sandbox.Run(ctx, "echo $$ > "+shellQuoteForSandboxTest(marker)+"; sleep 300")
		done <- runErr
	}()
	childPID := waitForV7GatePID(t, marker, done)
	cancel()
	select {
	case runErr := <-done:
		if runErr == nil || !strings.Contains(runErr.Error(), "cannot snapshot descendants") {
			t.Fatalf("unproven containment must fail closed: %v", runErr)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("failed containment did not stop and reap the original process group")
	}
	if processExists(childPID) {
		t.Fatalf("fallback did not kill original gate group child: pid=%d", childPID)
	}
}

func waitForV7GatePID(t *testing.T, marker string, done <-chan error) int {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if raw, err := os.ReadFile(marker); err == nil {
			pid, parseErr := strconv.Atoi(strings.TrimSpace(string(raw)))
			if parseErr == nil && pid > 0 {
				return pid
			}
		}
		select {
		case runErr := <-done:
			t.Fatalf("gate exited before writing child PID marker %s: %v", marker, runErr)
		default:
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("gate did not write child PID marker %s", marker)
	return 0
}
