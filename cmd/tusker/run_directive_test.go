package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestRunDirectiveRecorded(t *testing.T) {
	t.Setenv("USER", "test-operator")
	server := newServeEmptyNeedsFixture(t)
	makeServeTaskDispatchable(t, server, "APP-T-0001")
	guard, err := acquireDaemonGuard(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer guard.Close()

	var result serveActionResult
	servePost(t, server, "/api/tasks/APP-T-0001/run?project=app", `{"actor":"human:forged"}`, &result)
	if !result.OK || result.Refused {
		t.Fatalf("expected queued directive, got %#v", result)
	}
	directive, err := server.store.RunDirective("app", "APP-T-0001")
	if err != nil {
		t.Fatal(err)
	}
	if directive == nil {
		t.Fatal("expected stored run directive")
	}
	assertEqual(t, "queued", directive.State, "directive state")
	assertEqual(t, "human:test-operator", directive.Actor, "trusted directive actor")

	var detail serveTaskDetail
	serveDecode(t, server, "/api/tasks/APP-T-0001?project=app", &detail)
	if detail.RunDirective == nil {
		t.Fatal("expected directive in task detail")
	}
	assertEqual(t, "queued", detail.RunDirective.State, "task detail directive state")

	var duplicate serveActionResult
	servePost(t, server, "/api/tasks/APP-T-0001/run?project=app", `{"actor":"human:other"}`, &duplicate)
	if !duplicate.Refused || !strings.Contains(duplicate.Reason, "already queued") {
		t.Fatalf("expected duplicate refusal, got %#v", duplicate)
	}
	directive, err = server.store.RunDirective("app", "APP-T-0001")
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "human:test-operator", directive.Actor, "duplicate must not overwrite actor")
}

func TestRunDirectiveBypassableBlocker(t *testing.T) {
	tests := []struct {
		name    string
		blocker string
		want    bool
	}{
		{name: "automation disabled", blocker: "project automation is disabled in its configuration", want: true},
		{name: "default armed wave membership", blocker: "dispatch scope armed_waves requires task membership in a currently armed wave", want: false},
		{name: "explicit wave is not armed", blocker: "wave is not durably armed", want: false},
		{name: "dependency", blocker: "dependency APP-T-0002 is not done", want: false},
		{name: "empty", blocker: "", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := runDirectiveBypassableBlocker(tt.blocker); got != tt.want {
				t.Fatalf("runDirectiveBypassableBlocker(%q) = %v, want %v", tt.blocker, got, tt.want)
			}
		})
	}
}

func TestDaemonHonorsDirectiveWithAutomationOff(t *testing.T) {
	vault := automationTestVault(t)
	setAllEligibleDispatchScopeForAutomationTest(t, vault)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Directed", "risk": "low", "priority": "p0", "v7": "true"}, newV7Task)
	makeV7TaskDispatchableForTest(t, vault, "APP-T-0001")
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Not directed", "risk": "low", "priority": "p1", "v7": "true"}, newV7Task)
	makeV7TaskDispatchableForTest(t, vault, "APP-T-0002")
	initializeOrchestrationGitRepo(t, filepath.Dir(vault))
	installFakeCodexExec(t, filepath.Dir(vault))
	project := registerAutomationTestProject(t, vault)
	if _, err := setProjectLocalConfigWithReadback(vault, "automation.enabled", false); err != nil {
		t.Fatal(err)
	}
	daemon, err := NewDaemon(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer daemon.Close()
	diskConfig := defaultDiskPressureConfig()
	diskConfig.Enabled = false
	if err := daemon.store.SetDiskPressureConfig(diskConfig); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	queued, err := daemon.store.QueueRunDirective(RunDirective{
		ProjectID: project.ProjectID,
		RecordID:  "APP-T-0001",
		Actor:     "human:test",
		CreatedAt: now.Format(time.RFC3339Nano),
		ExpiresAt: now.Add(time.Minute).Format(time.RFC3339Nano),
		State:     "queued",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !queued {
		t.Fatal("expected directive to be queued")
	}

	if err := daemon.PollOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	run := latestRunForRecord(t, daemon.store, project.ProjectID, "APP-T-0001")
	if run.AttemptCount != 1 || run.ActiveAttemptID == "" {
		t.Fatalf("expected one normal attempt, got %#v", run)
	}
	if run.ProcessPGID > 0 {
		t.Cleanup(func() { _ = syscall.Kill(-run.ProcessPGID, syscall.SIGKILL) })
	}
	directive, err := daemon.store.RunDirective(project.ProjectID, "APP-T-0001")
	if err != nil {
		t.Fatal(err)
	}
	if directive == nil || directive.State != "consumed" {
		t.Fatalf("expected consumed directive, got %#v", directive)
	}
	auth, err := daemon.store.LatestRunAuthorization(project.ProjectID, "APP-T-0001")
	if err != nil {
		t.Fatal(err)
	}
	if auth == nil {
		t.Fatal("expected run authorization")
	}
	assertEqual(t, "human_run_directive", auth.Source, "authorization source")
	assertEqual(t, "human:test", auth.Actor, "authorization actor")
	if err := daemon.PollOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	attempts, err := daemon.store.ListAttemptsForRun(project.ProjectID, "APP-T-0001")
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, 1, len(attempts), "directive attempt count after repeat poll")
	otherAttempts, err := daemon.store.ListAttemptsForRun(project.ProjectID, "APP-T-0002")
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, 0, len(otherAttempts), "non-directed task attempt count")
}

func TestDaemonDirectiveCannotBypassArmedWaveScope(t *testing.T) {
	vault := automationTestVault(t)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Unrelated directive", "risk": "low", "priority": "p0", "v7": "true"}, newV7Task)
	makeV7TaskDispatchableForTest(t, vault, "APP-T-0001")
	initializeOrchestrationGitRepo(t, filepath.Dir(vault))
	installFakeCodexExec(t, filepath.Dir(vault))
	project := registerAutomationTestProject(t, vault)
	daemon, err := NewDaemon(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer daemon.Close()
	diskConfig := defaultDiskPressureConfig()
	diskConfig.Enabled = false
	if err := daemon.store.SetDiskPressureConfig(diskConfig); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	queued, err := daemon.store.QueueRunDirective(RunDirective{
		ProjectID: project.ProjectID, RecordID: "APP-T-0001", Actor: "human:test",
		CreatedAt: now.Format(time.RFC3339Nano), ExpiresAt: now.Add(time.Minute).Format(time.RFC3339Nano), State: "queued",
	})
	if err != nil || !queued {
		t.Fatalf("queue directive: queued=%t err=%v", queued, err)
	}
	if err := daemon.PollOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	run := latestRunForRecord(t, daemon.store, project.ProjectID, "APP-T-0001")
	assertEqual(t, 0, run.AttemptCount, "unrelated armed-waves directive attempt count")
	if !strings.Contains(run.LastError, "dispatch scope armed_waves requires task membership") {
		t.Fatalf("expected armed-wave scope refusal, got %#v", run)
	}
	directive, err := daemon.store.RunDirective(project.ProjectID, "APP-T-0001")
	if err != nil {
		t.Fatal(err)
	}
	if directive == nil || directive.State != "queued" {
		t.Fatalf("scope refusal must preserve queued directive, got %#v", directive)
	}
}

func TestRunDirectiveRefusals(t *testing.T) {
	t.Run("daemon down", func(t *testing.T) {
		server := newServeEmptyNeedsFixture(t)
		makeServeTaskDispatchable(t, server, "APP-T-0001")
		var result serveActionResult
		servePost(t, server, "/api/tasks/APP-T-0001/run?project=app", `{}`, &result)
		if !result.Refused || !strings.Contains(strings.ToLower(result.Reason), "daemon") {
			t.Fatalf("expected daemon-down refusal, got %#v", result)
		}
	})

	t.Run("task not runnable", func(t *testing.T) {
		server := newServeFixture(t)
		guard, err := acquireDaemonGuard(DefaultStateRoot())
		if err != nil {
			t.Fatal(err)
		}
		defer guard.Close()
		var result serveActionResult
		servePost(t, server, "/api/tasks/APP-T-0001/run?project=app", `{}`, &result)
		if !result.Refused || !strings.Contains(result.Reason, "not runnable") {
			t.Fatalf("expected non-runnable refusal, got %#v", result)
		}
	})

	t.Run("live run", func(t *testing.T) {
		server := newServeFixture(t)
		guard, err := acquireDaemonGuard(DefaultStateRoot())
		if err != nil {
			t.Fatal(err)
		}
		defer guard.Close()
		var result serveActionResult
		servePost(t, server, "/api/tasks/APP-T-0003/run?project=app", `{}`, &result)
		if !result.Refused || !strings.Contains(result.Reason, "live run") {
			t.Fatalf("expected live-run refusal, got %#v", result)
		}
	})

	t.Run("known dispatch blocker", func(t *testing.T) {
		server := newServeFixture(t)
		guard, err := acquireDaemonGuard(DefaultStateRoot())
		if err != nil {
			t.Fatal(err)
		}
		defer guard.Close()
		var result serveActionResult
		servePost(t, server, "/api/tasks/APP-T-0006/run?project=app", `{}`, &result)
		if !result.Refused || !strings.Contains(result.Reason, "cannot be dispatched") || !strings.Contains(result.Reason, "dependency") {
			t.Fatalf("expected canonical blocker refusal, got %#v", result)
		}
	})
}

func makeServeTaskDispatchable(t *testing.T, server *serveServer, taskID string) {
	t.Helper()
	path := filepath.Join(server.vaultPath, "work", "tasks", taskID+".md")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	updated := strings.Replace(string(raw), "| A1 | Works. | Inline verification |", "| A1 | The task completes its concrete fixture outcome. | Inline verification |", 1)
	if updated == string(raw) {
		t.Fatalf("fixture acceptance marker not found in %s", path)
	}
	if err := writeText(path, updated); err != nil {
		t.Fatal(err)
	}
	server.invalidateProjectSnapshot("app")
}

func TestRunDirectiveExpires(t *testing.T) {
	server := newServeEmptyNeedsFixture(t)
	now := time.Now().UTC()
	queued, err := server.store.QueueRunDirective(RunDirective{
		ProjectID: "app",
		RecordID:  "APP-T-0001",
		Actor:     "human:test",
		CreatedAt: now.Add(-2 * time.Minute).Format(time.RFC3339Nano),
		ExpiresAt: now.Add(-time.Minute).Format(time.RFC3339Nano),
		State:     "queued",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !queued {
		t.Fatal("expected directive to be queued")
	}
	directive, err := server.store.RunDirective("app", "APP-T-0001")
	if err != nil {
		t.Fatal(err)
	}
	if directive == nil || directive.State != "lapsed" {
		t.Fatalf("expected lapsed directive, got %#v", directive)
	}
	if !strings.Contains(directive.Reason, "expired") {
		t.Fatalf("expected explicit expiry reason, got %#v", directive)
	}
	withinSecond := time.Date(2026, 7, 22, 9, 0, 0, 100_000_000, time.UTC)
	if !runDirectiveActive(&RunDirective{State: "queued", ExpiresAt: withinSecond.Add(800 * time.Millisecond).Format(time.RFC3339Nano)}, withinSecond) {
		t.Fatal("sub-second future expiry must remain active")
	}
}

func TestRunDirectiveClaimIsAtomicWithAttemptIntent(t *testing.T) {
	server := newServeEmptyNeedsFixture(t)
	run := RunStatus{ProjectID: "app", RecordID: "APP-T-0001", ItemID: "APP-T-0001", Runner: string(RunnerCodexExec), Lane: runLaneExecute, LeaseState: string(LeaseStateUnclaimed)}
	if err := server.store.UpsertRun(run); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if queued, err := server.store.QueueRunDirective(RunDirective{ProjectID: "app", RecordID: "APP-T-0001", Actor: "human:test", CreatedAt: now.Format(time.RFC3339Nano), ExpiresAt: now.Add(time.Minute).Format(time.RFC3339Nano)}); err != nil || !queued {
		t.Fatalf("queue directive: queued=%t err=%v", queued, err)
	}
	if err := server.store.SaveAttempt(RunAttempt{AttemptID: "attempt-conflict", ProjectID: "other", RecordID: "other", ItemID: "other"}); err != nil {
		t.Fatal(err)
	}
	claimed, err := server.store.claimRunLeaseWithDirectiveAttempt(run, "attempt-conflict", 1, time.Minute, now, RuntimeLeaseClaimPrecondition{ExpectedLeaseState: LeaseStateUnclaimed}, RunAuthorization{Source: "human_run_directive", Actor: "human:test"}, RunAttempt{AttemptID: "attempt-conflict", Runner: string(RunnerCodexExec), Lane: runLaneExecute})
	if err == nil || claimed {
		t.Fatalf("expected atomic claim failure, claimed=%t err=%v", claimed, err)
	}
	directive, err := server.store.RunDirective("app", "APP-T-0001")
	if err != nil {
		t.Fatal(err)
	}
	if directive == nil || directive.State != "queued" {
		t.Fatalf("failed attempt insert must roll back directive consumption: %#v", directive)
	}
	latest, err := server.store.FindRun("APP-T-0001")
	if err != nil {
		t.Fatal(err)
	}
	if latest == nil || latest.LeaseState != string(LeaseStateUnclaimed) {
		t.Fatalf("failed attempt insert must roll back lease claim: %#v", latest)
	}
	auth, err := server.store.LatestRunAuthorization("app", "APP-T-0001")
	if err != nil {
		t.Fatal(err)
	}
	if auth != nil {
		t.Fatalf("failed attempt insert must roll back authorization: %#v", auth)
	}
}

func TestDirectedClaimRecoveryRequeuesUnstartedIntent(t *testing.T) {
	server := newServeEmptyNeedsFixture(t)
	makeServeTaskDispatchable(t, server, "APP-T-0001")
	initializeOrchestrationGitRepo(t, server.repoRoot)
	installFakeCodexExec(t, server.repoRoot)
	daemon, err := NewDaemon(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer daemon.Close()
	diskConfig := defaultDiskPressureConfig()
	diskConfig.Enabled = false
	if err := daemon.store.SetDiskPressureConfig(diskConfig); err != nil {
		t.Fatal(err)
	}
	projects, err := daemon.store.ListProjects()
	if err != nil || len(projects) != 1 {
		t.Fatalf("registered project: projects=%#v err=%v", projects, err)
	}
	project := projects[0]
	wfFile, err := loadWorkflow(project.VaultRoot)
	if err != nil {
		t.Fatal(err)
	}
	run := RunStatus{ProjectID: project.ProjectID, RecordID: "APP-T-0001", ItemID: "APP-T-0001", Runner: string(RunnerCodexExec), Lane: runLaneExecute, LeaseState: string(LeaseStateUnclaimed)}
	if err := daemon.store.UpsertRun(run); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if queued, err := daemon.store.QueueRunDirective(RunDirective{ProjectID: project.ProjectID, RecordID: run.RecordID, Actor: "human:test", CreatedAt: now.Format(time.RFC3339Nano), ExpiresAt: now.Add(time.Minute).Format(time.RFC3339Nano)}); err != nil || !queued {
		t.Fatalf("queue directive: queued=%t err=%v", queued, err)
	}
	claimed, err := daemon.store.claimRunLeaseWithDirectiveAttempt(run, "attempt-before-crash", 1, time.Minute, now, RuntimeLeaseClaimPrecondition{ExpectedLeaseState: LeaseStateUnclaimed}, RunAuthorization{Source: "human_run_directive", Actor: "human:test"}, RunAttempt{AttemptID: "attempt-before-crash", Runner: string(RunnerCodexExec), Lane: runLaneExecute})
	if err != nil || !claimed {
		t.Fatalf("commit crash-window intent: claimed=%t err=%v", claimed, err)
	}
	crashed, err := daemon.store.FindRun(run.RecordID)
	if err != nil || crashed == nil {
		t.Fatalf("load committed intent: run=%#v err=%v", crashed, err)
	}
	recovered, changed, err := daemon.reconcileRun(context.Background(), project, wfFile, *crashed)
	if err != nil || !changed {
		t.Fatalf("recover committed unstarted intent: changed=%t err=%v run=%#v", changed, err, recovered)
	}
	if recovered.LeaseState != string(LeaseStateUnclaimed) || recovered.ActiveAttemptID != "" || recovered.AttemptCount != 0 {
		t.Fatalf("expected requeued clean run, got %#v", recovered)
	}
	directive, err := daemon.store.RunDirective(project.ProjectID, run.RecordID)
	if err != nil || directive == nil || directive.State != "queued" {
		t.Fatalf("expected restored directive, got %#v err=%v", directive, err)
	}
	attempts, err := daemon.store.ListAttemptsForRun(project.ProjectID, run.RecordID)
	if err != nil || len(attempts) != 0 {
		t.Fatalf("placeholder attempt must be removed: attempts=%#v err=%v", attempts, err)
	}

	wfFile.Data.Workspace.Strategy = string(WorkspaceStrategyCopy)
	writeWorkflowForPreflightTest(t, project.VaultRoot, wfFile.Data, wfFile.Body)
	note, err := resolveNote(project.VaultRoot, run.RecordID)
	if err != nil {
		t.Fatal(err)
	}
	dispatched, _, err := daemon.dispatchRun(context.Background(), project, wfFile, note, recovered, runLaneExecute)
	if err != nil {
		t.Fatal(err)
	}
	if dispatched.ActiveAttemptID == "" || dispatched.ActiveAttemptID == "attempt-before-crash" || dispatched.AttemptCount != 1 {
		t.Fatalf("expected exactly one recovered dispatch attempt, got %#v", dispatched)
	}
	if dispatched.ProcessPGID > 0 {
		t.Cleanup(func() { _ = syscall.Kill(-dispatched.ProcessPGID, syscall.SIGKILL) })
	}
	attempts, err = daemon.store.ListAttemptsForRun(project.ProjectID, run.RecordID)
	if err != nil || len(attempts) != 1 {
		t.Fatalf("expected one recovered attempt: attempts=%#v err=%v", attempts, err)
	}
}

func TestDirectedClaimRecoveryDoesNotRequeueSpawnedIntent(t *testing.T) {
	server := newServeEmptyNeedsFixture(t)
	projects, err := server.store.ListProjects()
	if err != nil || len(projects) != 1 {
		t.Fatalf("registered project: projects=%#v err=%v", projects, err)
	}
	project := projects[0]
	run := RunStatus{ProjectID: project.ProjectID, RecordID: "APP-T-0001", ItemID: "APP-T-0001", Runner: string(RunnerCodexExec), Lane: runLaneExecute, LeaseState: string(LeaseStateUnclaimed)}
	if err := server.store.UpsertRun(run); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if queued, err := server.store.QueueRunDirective(RunDirective{ProjectID: project.ProjectID, RecordID: run.RecordID, Actor: "human:test", CreatedAt: now.Format(time.RFC3339Nano), ExpiresAt: now.Add(time.Minute).Format(time.RFC3339Nano)}); err != nil || !queued {
		t.Fatalf("queue directive: queued=%t err=%v", queued, err)
	}
	if claimed, err := server.store.claimRunLeaseWithDirectiveAttempt(run, "attempt-spawned", 1, time.Minute, now, RuntimeLeaseClaimPrecondition{ExpectedLeaseState: LeaseStateUnclaimed}, RunAuthorization{Source: "human_run_directive", Actor: "human:test"}, RunAttempt{AttemptID: "attempt-spawned", Runner: string(RunnerCodexExec), Lane: runLaneExecute}); err != nil || !claimed {
		t.Fatalf("commit intent: claimed=%t err=%v", claimed, err)
	}
	claimedRun, err := server.store.FindRun(run.RecordID)
	if err != nil || claimedRun == nil {
		t.Fatalf("load claimed run: run=%#v err=%v", claimedRun, err)
	}
	registered, err := server.store.registerRunnerWrapperSpawn(StartRequest{
		ProjectID: project.ProjectID, RecordID: run.RecordID, AttemptID: claimedRun.ActiveAttemptID, LeaseGeneration: claimedRun.LeaseGeneration,
	}, 999999, 999999, "dead-test-process")
	if err != nil || !registered {
		t.Fatalf("register wrapper spawn: registered=%t err=%v", registered, err)
	}
	requeued, err := server.store.requeueUnstartedDirectedClaim(*claimedRun, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if requeued {
		t.Fatal("durably registered wrapper must make recovery ineligible")
	}
	directive, err := server.store.RunDirective(project.ProjectID, run.RecordID)
	if err != nil || directive == nil || directive.State != "consumed" {
		t.Fatalf("registered directive must remain consumed: directive=%#v err=%v", directive, err)
	}
	attempts, err := server.store.ListAttemptsForRun(project.ProjectID, run.RecordID)
	if err != nil || len(attempts) != 1 || attempts[0].ProcessPID != 999999 {
		t.Fatalf("registered attempt must remain durable: attempts=%#v err=%v", attempts, err)
	}
}

func TestDirectedClaimSpawnRegistrationRacesRecovery(t *testing.T) {
	server := newServeEmptyNeedsFixture(t)
	projects, err := server.store.ListProjects()
	if err != nil || len(projects) != 1 {
		t.Fatalf("registered project: projects=%#v err=%v", projects, err)
	}
	project := projects[0]
	for i := 0; i < 12; i++ {
		recordID := "APP-T-RACE-" + time.Now().UTC().Format("150405.000000000") + "-" + string(rune('a'+i))
		attemptID := "attempt-race-" + string(rune('a'+i))
		run := RunStatus{ProjectID: project.ProjectID, RecordID: recordID, ItemID: recordID, Runner: string(RunnerCodexExec), Lane: runLaneExecute, LeaseState: string(LeaseStateUnclaimed)}
		if err := server.store.UpsertRun(run); err != nil {
			t.Fatal(err)
		}
		now := time.Now().UTC()
		if queued, err := server.store.QueueRunDirective(RunDirective{ProjectID: project.ProjectID, RecordID: recordID, Actor: "human:test", CreatedAt: now.Format(time.RFC3339Nano), ExpiresAt: now.Add(time.Minute).Format(time.RFC3339Nano)}); err != nil || !queued {
			t.Fatalf("queue directive %d: queued=%t err=%v", i, queued, err)
		}
		if claimed, err := server.store.claimRunLeaseWithDirectiveAttempt(run, attemptID, 1, time.Minute, now, RuntimeLeaseClaimPrecondition{ExpectedLeaseState: LeaseStateUnclaimed}, RunAuthorization{Source: "human_run_directive", Actor: "human:test"}, RunAttempt{AttemptID: attemptID, Runner: string(RunnerCodexExec), Lane: runLaneExecute}); err != nil || !claimed {
			t.Fatalf("claim intent %d: claimed=%t err=%v", i, claimed, err)
		}
		claimedRun, err := server.store.FindRun(recordID)
		if err != nil || claimedRun == nil {
			t.Fatalf("load claimed run %d: run=%#v err=%v", i, claimedRun, err)
		}

		type raceResult struct {
			registered bool
			requeued   bool
			err        error
		}
		start := make(chan struct{})
		results := make(chan raceResult, 2)
		go func() {
			<-start
			ok, err := server.store.registerRunnerWrapperSpawn(StartRequest{ProjectID: project.ProjectID, RecordID: recordID, AttemptID: attemptID, LeaseGeneration: claimedRun.LeaseGeneration}, 900000+i, 900000+i, "race-test-process")
			results <- raceResult{registered: ok, err: err}
		}()
		go func() {
			<-start
			ok, err := server.store.requeueUnstartedDirectedClaim(*claimedRun, now.Add(time.Second))
			results <- raceResult{requeued: ok, err: err}
		}()
		close(start)
		first, second := <-results, <-results
		if first.err != nil || second.err != nil {
			t.Fatalf("race %d failed: first=%#v second=%#v", i, first, second)
		}
		registered := first.registered || second.registered
		requeued := first.requeued || second.requeued
		if registered == requeued {
			t.Fatalf("race %d must have exactly one winner: first=%#v second=%#v", i, first, second)
		}
		directive, err := server.store.RunDirective(project.ProjectID, recordID)
		if err != nil || directive == nil {
			t.Fatalf("load directive after race %d: directive=%#v err=%v", i, directive, err)
		}
		attempts, err := server.store.ListAttemptsForRun(project.ProjectID, recordID)
		if err != nil {
			t.Fatal(err)
		}
		if registered && (directive.State != "consumed" || len(attempts) != 1 || attempts[0].ProcessPID == 0) {
			t.Fatalf("registration winner %d inconsistent: directive=%#v attempts=%#v", i, directive, attempts)
		}
		if requeued && (directive.State != "queued" || len(attempts) != 0) {
			t.Fatalf("recovery winner %d inconsistent: directive=%#v attempts=%#v", i, directive, attempts)
		}
	}
}

func TestDirectedClaimWrapperRegistersDaemonStampedPID(t *testing.T) {
	server := newServeEmptyNeedsFixture(t)
	projects, err := server.store.ListProjects()
	if err != nil || len(projects) != 1 {
		t.Fatalf("registered project: projects=%#v err=%v", projects, err)
	}
	project := projects[0]
	run := RunStatus{ProjectID: project.ProjectID, RecordID: "APP-T-0001", ItemID: "APP-T-0001", Runner: string(RunnerCodexExec), Lane: runLaneExecute, LeaseState: string(LeaseStateUnclaimed)}
	if err := server.store.UpsertRun(run); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if queued, err := server.store.QueueRunDirective(RunDirective{ProjectID: project.ProjectID, RecordID: run.RecordID, Actor: "human:test", CreatedAt: now.Format(time.RFC3339Nano), ExpiresAt: now.Add(time.Minute).Format(time.RFC3339Nano)}); err != nil || !queued {
		t.Fatalf("queue directive: queued=%t err=%v", queued, err)
	}
	if claimed, err := server.store.claimRunLeaseWithDirectiveAttempt(run, "attempt-stamped", 1, time.Minute, now, RuntimeLeaseClaimPrecondition{ExpectedLeaseState: LeaseStateUnclaimed}, RunAuthorization{Source: "human_run_directive", Actor: "human:test"}, RunAttempt{AttemptID: "attempt-stamped", Runner: string(RunnerCodexExec), Lane: runLaneExecute}); err != nil || !claimed {
		t.Fatalf("commit intent: claimed=%t err=%v", claimed, err)
	}
	claimedRun, err := server.store.FindRun(run.RecordID)
	if err != nil || claimedRun == nil {
		t.Fatalf("load claimed run: run=%#v err=%v", claimedRun, err)
	}

	// The daemon wins the write race and persists the wrapper's own PID before
	// the cold wrapper executable calls registerRunnerWrapperSpawn (daemon.go
	// stamps the attempt then flips the run to running). Reproduce both stamps.
	const daemonStampedPID = 424242
	placeholders, err := server.store.ListAttemptsForRun(project.ProjectID, run.RecordID)
	if err != nil || len(placeholders) != 1 {
		t.Fatalf("expected one placeholder attempt: attempts=%#v err=%v", placeholders, err)
	}
	stampedAttempt := placeholders[0]
	stampedAttempt.ProcessPID = daemonStampedPID
	if ok, err := server.store.SaveAttemptIfRunLease(stampedAttempt, claimedRun.ActiveAttemptID, claimedRun.LeaseGeneration); err != nil || !ok {
		t.Fatalf("daemon stamp attempt pid: ok=%t err=%v", ok, err)
	}
	stampedRun := *claimedRun
	stampedRun.ProcessPID = daemonStampedPID
	stampedRun.ProcessPGID = daemonStampedPID
	stampedRun.ProcessStartedAt = "daemon-stamped-process"
	stampedRun.LeaseState = string(LeaseStateRunning)
	if ok, err := server.store.UpdateRunIfLease(stampedRun, claimedRun.ActiveAttemptID, claimedRun.LeaseGeneration); err != nil || !ok {
		t.Fatalf("daemon stamp run pid: ok=%t err=%v", ok, err)
	}

	spawn := StartRequest{ProjectID: project.ProjectID, RecordID: run.RecordID, AttemptID: claimedRun.ActiveAttemptID, LeaseGeneration: claimedRun.LeaseGeneration}

	// A different PID reaching this stamped attempt is a genuine conflict and
	// must still be refused without clobbering the stored PID.
	if registered, err := server.store.registerRunnerWrapperSpawn(spawn, daemonStampedPID+1, daemonStampedPID+1, "wrapper-process"); err != nil || registered {
		t.Fatalf("different PID must be refused after daemon stamp: registered=%t err=%v", registered, err)
	}
	if guarded, err := server.store.ListAttemptsForRun(project.ProjectID, run.RecordID); err != nil || len(guarded) != 1 || guarded[0].ProcessPID != daemonStampedPID {
		t.Fatalf("refused registration must not clobber stored PID: attempts=%#v err=%v", guarded, err)
	}

	// The wrapper registering the identical PID the daemon already stored is the
	// benign race and must succeed idempotently.
	if registered, err := server.store.registerRunnerWrapperSpawn(spawn, daemonStampedPID, daemonStampedPID, "wrapper-process"); err != nil || !registered {
		t.Fatalf("wrapper registering the daemon-stamped PID must succeed: registered=%t err=%v", registered, err)
	}
	attempts, err := server.store.ListAttemptsForRun(project.ProjectID, run.RecordID)
	if err != nil || len(attempts) != 1 || attempts[0].ProcessPID != daemonStampedPID {
		t.Fatalf("registered attempt must retain daemon-stamped PID: attempts=%#v err=%v", attempts, err)
	}
	directive, err := server.store.RunDirective(project.ProjectID, run.RecordID)
	if err != nil || directive == nil || directive.State != "consumed" {
		t.Fatalf("registered directive must remain consumed: directive=%#v err=%v", directive, err)
	}
	final, err := server.store.FindRun(run.RecordID)
	if err != nil || final == nil || final.ProcessPID != daemonStampedPID {
		t.Fatalf("registered run must retain daemon-stamped PID: run=%#v err=%v", final, err)
	}
}

func TestInteractiveCannotDispatchDirective(t *testing.T) {
	vault := automationTestVault(t)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Interactive refusal", "risk": "low", "priority": "p0", "v7": "true"}, newV7Task)
	makeV7TaskDispatchableForTest(t, vault, "APP-T-0001")
	project := registerAutomationTestProject(t, vault)
	if _, err := setProjectLocalConfigWithReadback(vault, "automation.enabled", false); err != nil {
		t.Fatal(err)
	}
	daemon, err := NewDaemon(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer daemon.Close()
	daemon.dispatchRefusalReason = oneShotDispatchRefusal("tusker daemon run --once")
	now := time.Now().UTC()
	if queued, err := daemon.store.QueueRunDirective(RunDirective{ProjectID: project.ProjectID, RecordID: "APP-T-0001", Actor: "human:test", CreatedAt: now.Format(time.RFC3339Nano), ExpiresAt: now.Add(time.Minute).Format(time.RFC3339Nano)}); err != nil || !queued {
		t.Fatalf("queue directive: queued=%t err=%v", queued, err)
	}
	if err := daemon.PollOnce(context.Background()); err == nil || !strings.Contains(err.Error(), "cannot dispatch local runners") {
		t.Fatalf("expected one-shot dispatch refusal, got %v", err)
	}
	directive, err := daemon.store.RunDirective(project.ProjectID, "APP-T-0001")
	if err != nil {
		t.Fatal(err)
	}
	if directive == nil || directive.State != "queued" {
		t.Fatalf("one-shot process must not consume directive: %#v", directive)
	}
	attempts, err := daemon.store.ListAttemptsForRun(project.ProjectID, "APP-T-0001")
	if err != nil {
		t.Fatal(err)
	}
	if len(attempts) != 0 {
		t.Fatalf("one-shot process must not dispatch attempts: %#v", attempts)
	}
}
