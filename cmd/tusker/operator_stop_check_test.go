package main

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// Match the Processes Stop endpoint: persist intent before stopping the group.
func stopRunningCheckForTest(t *testing.T, store *RuntimeStore) {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		rows, err := store.ownedProcesses()
		if err != nil {
			t.Fatal(err)
		}
		for _, row := range rows {
			if row.Kind != processKindCheck || row.EndedAt != "" {
				continue
			}
			if err := store.endOwnedProcess(row.ID, "operator_stopped"); err != nil {
				t.Fatal(err)
			}
			stopProcessGroup(row.PGID)
			return
		}
	}
	t.Fatal("check did not start")
}

func TestOperatorStoppedCheckOutcome(t *testing.T) {
	t.Run("operator", func(t *testing.T) {
		store := openInventoryTestStore(t)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		done := make(chan error, 1)
		go func() {
			_, err := runOwnedCheck(ctx, "operator-check", exec.Command("sleep", "30"), time.Minute)
			done <- err
		}()
		stopRunningCheckForTest(t, store)
		select {
		case err := <-done:
			if !errors.Is(err, errCheckOperatorStopped) || errors.Is(err, errCheckStuckStopped) {
				t.Fatalf("want operator stop, got %v", err)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("stopped check did not return")
		}
		if row := inventoryRow(t, store, "operator-check"); row.EndReason != "operator_stopped" {
			t.Fatalf("lost operator intent: %+v", row)
		}
	})
	t.Run("failure", func(t *testing.T) {
		openInventoryTestStore(t)
		_, err := runOwnedCheck(context.Background(), "failed-check", exec.Command("sh", "-c", "exit 7"), time.Minute)
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != 7 {
			t.Fatalf("want exit 7, got %v", err)
		}
	})
	t.Run("silence", TestProcessInventorySilentCheckStuckStopped)
	t.Run("closeout", func(t *testing.T) {
		store := openInventoryTestStore(t)
		vault := t.TempDir()
		done := make(chan struct{})
		var result map[string]any
		var err error
		go func() {
			defer close(done)
			result, err = runV7CloseoutValidation(vault, "closeout:operator", "sleep 30")
		}()
		stopRunningCheckForTest(t, store)
		select {
		case <-done:
			if err == nil || result["result"] != "operator_stopped" || !strings.Contains(err.Error(), "stopped by operator") {
				t.Fatalf("result=%v err=%v", result, err)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("closeout did not stop")
		}
	})
}

func TestOperatorStoppedGateNotRetried(t *testing.T) {
	store := openInventoryTestStore(t)
	repo, vault := newLandTestRepo(t, 2, "sleep 30")
	commitLandBranch(t, repo, "task/APP-T-0001", "integration/W-0001", map[string]string{"a.txt": "a\n"})
	commitLandBranch(t, repo, "task/APP-T-0002", "integration/W-0001", map[string]string{"b.txt": "b\n"})
	before := gitDirOutput(t, repo, "rev-parse", "integration/W-0001")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	acc := &v7BatchAccumulator{}
	tasks := []v7LandTask{
		{ID: "APP-T-0001", Branch: "task/APP-T-0001", SourceSHA: strings.TrimSpace(gitDirOutput(t, repo, "rev-parse", "task/APP-T-0001"))},
		{ID: "APP-T-0002", Branch: "task/APP-T-0002", SourceSHA: strings.TrimSpace(gitDirOutput(t, repo, "rev-parse", "task/APP-T-0002"))},
	}
	go func() {
		done <- landV7BatchRecursive(ctx, vault, repo, "W-0001", "integration/W-0001", tasks, "human:test", "", nil, acc)
	}()
	stopRunningCheckForTest(t, store)
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "gate stopped by operator") {
			t.Fatalf("want stopped gate, got %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("landing retried after operator stop")
	}
	if len(acc.Failed) != 0 || len(acc.Landed) != 0 {
		t.Fatalf("stopped batch recorded landings or failures: %+v", acc)
	}
	rows, err := store.ownedProcesses()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].EndReason != "operator_stopped" {
		t.Fatalf("expected one stopped gate without retries: %+v", rows)
	}
	if after := gitDirOutput(t, repo, "rev-parse", "integration/W-0001"); after != before {
		t.Fatal("stopped gate advanced integration")
	}
}
