package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPollOnceSkipsOnlyFailingProjectAndReturnsGlobalConfigError(t *testing.T) {
	vaults := []string{automationTestVault(t), pickupV7TestVault(t)}
	if err := writeDefaultWorkflow(vaults[1]); err != nil {
		t.Fatal(err)
	}
	registered := []RegisteredProject{
		registerAutomationTestProject(t, vaults[0]),
		registerAutomationTestProject(t, vaults[1]),
	}

	daemon, err := NewDaemon(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer daemon.Close()
	projects, err := daemon.store.ListProjects()
	if err != nil || len(projects) != 2 {
		t.Fatalf("list registered projects: %#v %v", projects, err)
	}
	if projects[0].ProjectID == registered[1].ProjectID {
		vaults[0], vaults[1] = vaults[1], vaults[0]
	}
	mustRunPickupTest(t, Args{"vault": vaults[0], "quiet": "true", "epic": "APP", "title": "Broken poll", "risk": "low", "priority": "p0", "owned-paths": "src", "v7": "true"}, newV7Task)
	makeV7TaskDispatchableForTest(t, vaults[0], "APP-T-0001")
	setAllEligibleDispatchScopeForAutomationTest(t, vaults[0])
	if err := daemon.store.UpsertRun(RunStatus{
		ProjectID: projects[0].ProjectID, RecordID: "APP-T-0001", ItemID: "APP-T-0001",
		Runner: string(RunnerCodexExec), Lane: runLaneExecute,
		LeaseState: string(LeaseStateUnclaimed), UpdatedAt: time.Now().UTC().Format(time.RFC3339),
	}); err != nil {
		t.Fatal(err)
	}
	mutated := false
	daemon.beforePollRunPersist = func(before, _ RunStatus) {
		if mutated || before.ProjectID != projects[0].ProjectID {
			return
		}
		mutated = true
		concurrent := before
		concurrent.LastError = "concurrent change"
		concurrent.UpdatedAt = time.Now().UTC().Add(time.Second).Format(time.RFC3339Nano)
		if err := daemon.store.UpsertRun(concurrent); err != nil {
			t.Fatal(err)
		}
	}
	var logs bytes.Buffer
	previousLogOutput := log.Writer()
	log.SetOutput(&logs)
	defer log.SetOutput(previousLogOutput)
	if err := daemon.PollOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	// The injected concurrent change makes the first project's poll lose a
	// compare-and-swap race inside its body. That is isolated to the project and
	// retried silently (F77); the other project is still polled below.
	if !mutated || strings.Contains(logs.String(), "skipped: run changed") {
		t.Fatalf("first project did not lose its CAS race silently: mutated=%t logs=%s", mutated, logs.String())
	}
	polled, err := daemon.store.ListProjects()
	if err != nil || len(polled) != 2 {
		t.Fatalf("list polled projects: %#v %v", polled, err)
	}
	for _, project := range polled {
		if project.LastPollAt == "" {
			t.Fatalf("project %s was not polled or backed off", project.ProjectID)
		}
	}

	config := filepath.Join(t.TempDir(), "config.yaml")
	t.Setenv("TUSKER_CONFIG", config)
	if err := os.WriteFile(config, []byte("automation:\n  concurrency:\n    max_active_runs: -1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	err = daemon.runPoll(context.Background(), "")
	var typed *TuskerError
	if !errors.As(err, &typed) || typed.Code != errorConfigInvalid {
		t.Fatalf("global CONFIG_INVALID must stop the daemon: %v", err)
	}
}

func TestPollProjectErrorRequiresOnlyTypedLeaves(t *testing.T) {
	projection := fmt.Errorf("armed-wave integration projection: %w", tuskerError(errorNotFound, "task missing"))
	if !daemonProjectPollErrorIsSkippable(projection) {
		t.Fatal("typed NOT_FOUND from a project body should be isolated")
	}
	if daemonProjectPollErrorIsSkippable(errors.Join(projection, errors.New("storage failure"))) {
		t.Fatal("joined storage failure must stop the daemon")
	}
}
