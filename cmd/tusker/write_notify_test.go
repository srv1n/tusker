package main

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestWriteNotifyTargetsProjectDebouncesBurstAndStreamsProjectKey(t *testing.T) {
	root := shortDaemonServeTempDir(t)
	stateRoot := filepath.Join(root, "state")
	vault := filepath.Join(root, "app", ".tusker")
	t.Setenv("TUSKER_STATE_ROOT", stateRoot)
	bootstrapWriteNotifyProject(t, vault)
	projectID := v7ProjectID(vault)

	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	project := RegisteredProject{
		ProjectID: projectID, ProjectKey: "app-key", Name: "App",
		RepoRoot: filepath.Dir(vault), VaultRoot: vault, WorkflowPath: workflowPath(vault),
		Enabled: true, Health: projectHealthHealthy,
	}
	if err := store.UpsertProject(project); err != nil {
		t.Fatal(err)
	}

	daemon := &Daemon{
		stateRoot:   stateRoot,
		store:       store,
		stream:      newServeStreamBroker(),
		writeNotify: make(chan string, daemonWriteNotifyBuffer),
	}
	if err := daemon.reconcileProjectOnce(context.Background(), projectID); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go daemon.runWriteNotifyLoop(ctx)
	control, err := startDaemonControlServer(stateRoot, func(reqCtx context.Context, req daemonControlRequest) daemonControlResponse {
		return daemon.handleControlRequest(reqCtx, req, cancel)
	})
	if err != nil {
		t.Fatal(err)
	}
	defer control.Close()

	events, unsubscribe, ok := daemon.stream.Subscribe()
	if !ok {
		t.Fatal("subscribe to daemon stream")
	}
	defer unsubscribe()
	started := time.Now()
	for i := 0; i < 4; i++ {
		if err := statusV7Cmd(Args{
			"vault": vault, "quiet": "true", "local": "true",
			"id": "APP-T-0001", "status": "cancelled", "by": "agent:test",
		}); err != nil {
			t.Fatalf("CLI mutation %d failed: %v", i+1, err)
		}
	}
	if err := reconcileV7Cmd(Args{"vault": vault, "quiet": "true", "local": "true"}); err != nil {
		t.Fatalf("CLI reconcile mutation failed: %v", err)
	}
	// Insert this after the CLI's own terminal-state retirement lookup but
	// before the notification debounce expires. A full-registry reconcile on
	// the notify path would quarantine the deliberately missing vault.
	other := RegisteredProject{
		ProjectID: "other-project", ProjectKey: "other-key", Name: "Other",
		RepoRoot: t.TempDir(), VaultRoot: filepath.Join(t.TempDir(), "missing-vault"),
		WorkflowPath: filepath.Join(t.TempDir(), "missing-vault", "WORKFLOW.md"),
		Enabled:      true, Health: projectHealthHealthy,
	}
	if err := store.UpsertProject(other); err != nil {
		t.Fatal(err)
	}

	reconciles := 0
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	for reconciles == 0 {
		select {
		case event := <-events:
			if event.Kind != serveStreamKindProjectReconcile {
				continue
			}
			reconciles++
			if event.Project != project.ProjectKey {
				t.Fatalf("stream project = %q, want project key %q", event.Project, project.ProjectKey)
			}
		case <-deadline.C:
			t.Fatal("CLI mutation did not trigger targeted reconcile within one second")
		}
	}
	if elapsed := time.Since(started); elapsed >= time.Second {
		t.Fatalf("targeted reconcile took %s", elapsed)
	}

	quiet := time.NewTimer(daemonWriteNotifyDebounce + 150*time.Millisecond)
	defer quiet.Stop()
	for {
		select {
		case event := <-events:
			if event.Kind == serveStreamKindProjectReconcile {
				reconciles++
			}
		case <-quiet.C:
			if reconciles != 1 {
				t.Fatalf("burst produced %d reconciles, want 1", reconciles)
			}
			goto checked
		}
	}

checked:
	if got := daemon.pollTaskStatuses[projectID+"\x00APP-T-0001"]; got != "cancelled" {
		t.Fatalf("daemon task state = %q, want cancelled", got)
	}
	untouched, err := store.GetProject(other.ProjectID)
	if err != nil || untouched == nil {
		t.Fatalf("load unrelated project: project=%#v err=%v", untouched, err)
	}
	if untouched.Health != projectHealthHealthy || untouched.LastError != "" || untouched.LastPollAt != "" {
		t.Fatalf("unrelated project was loaded or reconciled: %#v", *untouched)
	}
}

func TestWriteNotifyDaemonDownDoesNotFailOrHangCLI(t *testing.T) {
	root := t.TempDir()
	vault := filepath.Join(root, ".tusker")
	t.Setenv("TUSKER_STATE_ROOT", filepath.Join(root, "daemon-down"))
	bootstrapWriteNotifyProject(t, vault)

	started := time.Now()
	err := statusV7Cmd(Args{
		"vault": vault, "quiet": "true", "local": "true",
		"id": "APP-T-0001", "status": "cancelled", "by": "agent:test",
	})
	if err != nil {
		t.Fatalf("daemon-down CLI mutation failed: %v", err)
	}
	if elapsed := time.Since(started); elapsed >= time.Second {
		t.Fatalf("daemon-down notification delayed CLI for %s", elapsed)
	}
}

func bootstrapWriteNotifyProject(t *testing.T, vault string) {
	t.Helper()
	for _, step := range []struct {
		args Args
		fn   func(Args) error
	}{
		{Args{"vault": vault, "quiet": "true"}, bootstrap},
		{Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App", "summary": "Write notification fixture.", "v7": "true"}, newV7Epic},
		{Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Notify daemon", "risk": "low", "priority": "p2", "v7": "true"}, newV7Task},
	} {
		if err := step.fn(step.args); err != nil {
			t.Fatal(err)
		}
	}
	writeDaemonServeWorkflow(t, vault, false, defaultServeAddr)
}
