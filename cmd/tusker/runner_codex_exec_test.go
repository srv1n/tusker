package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func TestCodexExecAttemptRecordsJSONLTurnsAndSession(t *testing.T) {
	tempRoot := t.TempDir()
	t.Setenv("TUSKER_STATE_ROOT", filepath.Join(tempRoot, "state"))
	installFakeCodexExec(t, tempRoot)

	workspace := filepath.Join(tempRoot, "workspace")
	if err := ensureDir(workspace); err != nil {
		t.Fatal(err)
	}
	promptPath := filepath.Join(tempRoot, "prompt.md")
	rawLogPath := filepath.Join(tempRoot, "codex.raw.log")
	eventSinkPath := filepath.Join(tempRoot, "events.jsonl")
	statusPath := filepath.Join(tempRoot, "status.json")
	if err := writeText(promptPath, "Do the task.\n"); err != nil {
		t.Fatal(err)
	}

	result, err := executeRunnerCommand(context.Background(), RunnerCodexExec, runnerExecRequest{
		ProjectID: "project-1", RecordID: "APP-T-0001", ItemID: "APP-T-0001", AttemptID: "attempt-exec",
		Lane: runLaneExecute, WorkspacePath: workspace, RepoRoot: workspace, PromptPath: promptPath,
		EventSinkPath: eventSinkPath, RawLogPath: rawLogPath, StatusPath: statusPath, Command: defaultCodexExecCommand(),
	}, RunnerCapabilities{StructuredEvents: true, ResumeSession: true, MachineFinalStatus: true, UsageMetrics: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.PID <= 0 {
		t.Fatalf("expected codex exec process pid, got %#v", result)
	}
	waitForStatusFile(t, statusPath)
	status, err := readRunnerProcessStatus(statusPath)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, 0, status.ExitCode, "codex exec exit")

	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	run := RunStatus{
		ProjectID:       "project-1",
		RecordID:        "APP-T-0001",
		ItemID:          "APP-T-0001",
		Runner:          string(RunnerCodexExec),
		Lane:            runLaneExecute,
		LeaseState:      string(LeaseStateRunning),
		ActiveAttemptID: "attempt-exec",
		SessionRef:      extractSessionRef(rawLogPath),
		RawLogPath:      rawLogPath,
		EventSinkPath:   eventSinkPath,
		StatusPath:      statusPath,
		WorkspacePath:   workspace,
		AttemptCount:    1,
	}
	if err := store.SaveAttempt(RunAttempt{AttemptID: run.ActiveAttemptID, ProjectID: run.ProjectID, RecordID: run.RecordID, ItemID: run.ItemID, Runner: run.Runner, Lane: run.Lane, RawLogPath: rawLogPath, EventSinkPath: eventSinkPath, StatusPath: statusPath}); err != nil {
		t.Fatal(err)
	}
	daemon := &Daemon{stateRoot: DefaultStateRoot(), store: store}
	if changed, err := daemon.ingestCodexExecRawLog(run); err != nil {
		t.Fatal(err)
	} else if !changed {
		t.Fatal("expected codex exec raw log ingestion to record events")
	}
	if changed, err := daemon.ingestCodexExecRawLog(run); err != nil {
		t.Fatal(err)
	} else if changed {
		t.Fatal("expected second codex exec raw log ingestion to be idempotent")
	}
	eventText, err := readText(eventSinkPath)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, 1, strings.Count(eventText, `"kind":"thread_started"`), "thread_started event count")
	turns, err := store.ListTurnsForAttempt(run.ActiveAttemptID)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, 2, len(turns), "codex exec turns")
	assertEqual(t, "session-start", turns[0].SessionRef, "turn session")
	assertEqual(t, "completed", turns[1].Status, "second turn status")
	assertEqual(t, 11, turns[1].InputTokens, "second turn input tokens")
	updateRunAttemptFromRun(store, run, AttemptOutcomeSucceeded, 0, "", time.Now().UTC().Format(time.RFC3339))
	attempts, err := store.ListAttemptsForRun(run.ProjectID, run.RecordID)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, 2, attempts[0].TurnsUsed, "attempt turns used")
	assertEqual(t, "session-start", extractSessionRef(rawLogPath), "captured session id")

	argsLog, err := readText(filepath.Join(tempRoot, "fake-codex-args.log"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(strings.TrimSpace(argsLog), "\n")+1 != 1 || !strings.Contains(argsLog, "exec --json --skip-git-repo-check -") {
		t.Fatalf("expected one codex exec process invocation, got:\n%s", argsLog)
	}
}

func TestCodexExecContinuationRedispatchResumesRecordedSession(t *testing.T) {
	tempRoot := t.TempDir()
	t.Setenv("TUSKER_STATE_ROOT", filepath.Join(tempRoot, "state"))
	installFakeCodexExec(t, tempRoot)
	workspace := filepath.Join(tempRoot, "workspace")
	if err := ensureDir(workspace); err != nil {
		t.Fatal(err)
	}
	promptPath := filepath.Join(tempRoot, "prompt.md")
	if err := writeText(promptPath, "Continue.\n"); err != nil {
		t.Fatal(err)
	}
	statusPath := filepath.Join(tempRoot, "resume.status.json")
	resumeCommand := codexExecResumeCommand(defaultCodexExecCommand())
	if !strings.Contains(resumeCommand, "codex exec resume --json --skip-git-repo-check {{session_ref}} -") {
		t.Fatalf("unexpected resume command: %s", resumeCommand)
	}
	_, err := runnerWrapperStartChild(context.Background(), runnerWrapperRequest{
		Runner: string(RunnerCodexExec),
		Start: StartRequest{
			ProjectID: "project-1", RecordID: "APP-T-0001", ItemID: "APP-T-0001", AttemptID: "attempt-resume",
			Lane: runLaneExecute, WorkspacePath: workspace, RepoRoot: workspace, PromptPath: promptPath,
			RawLogPath: filepath.Join(tempRoot, "resume.raw.log"), EventSinkPath: filepath.Join(tempRoot, "resume.events.jsonl"), StatusPath: statusPath,
			Command: resumeCommand,
		},
		Resume: &ResumeRequest{SessionRef: "session-existing", Command: resumeCommand},
	})
	if err != nil {
		t.Fatal(err)
	}
	waitForStatusFile(t, statusPath)
	argsLog, err := readText(filepath.Join(tempRoot, "fake-codex-args.log"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(argsLog, "exec resume --json --skip-git-repo-check session-existing -") {
		t.Fatalf("expected codex exec resume invocation, got:\n%s", argsLog)
	}

	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	daemon := &Daemon{stateRoot: DefaultStateRoot(), store: store}
	project := RegisteredProject{ProjectID: "project-1"}
	note := Note{Data: map[string]any{"status": "ready"}}
	noSession, err := daemon.resolveResumeSession(project, note, RunStatus{ProjectID: "project-1", RecordID: "APP-T-0001", Runner: string(RunnerCodexExec), LeaseState: string(LeaseStateRetryQueued), WorkspacePath: workspace})
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "", noSession.SessionRef, "fresh fallback without stored session")
	if err := store.SaveSession(RunnerSession{ProjectID: "project-1", RecordID: "APP-T-0001", Runner: string(RunnerCodexExec), SessionRef: "session-existing", WorkspacePath: workspace, WorkRevision: 0, Resumable: true, LastAttemptID: "attempt-old"}); err != nil {
		t.Fatal(err)
	}
	budgetKilled, err := daemon.resolveResumeSession(project, note, RunStatus{ProjectID: "project-1", RecordID: "APP-T-0001", Runner: string(RunnerCodexExec), LeaseState: string(LeaseStateRetryQueued), AttemptOutcome: string(AttemptOutcomeBudgetExceeded), SessionRef: "session-existing", WorkspacePath: workspace})
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "", budgetKilled.SessionRef, "fresh fallback after budget kill")
}

func TestCodexExecBudgetAcrossJSONLTurnsAndTurnCap(t *testing.T) {
	t.Run("budget", func(t *testing.T) {
		store, daemon, run, project := codexExecGovernorFixture(t)
		defer store.Close()
		note := Note{Data: map[string]any{"id": run.ItemID}}
		wf := defaultWorkflow()
		wf.Runtime.Budget.PerAttemptInputTokens = 10
		wf.Runtime.Budget.PerAttemptOutputTokens = 100
		if changed, err := daemon.ingestCodexExecRawLog(run); err != nil {
			t.Fatal(err)
		} else if !changed {
			t.Fatal("expected raw log ingestion")
		}
		updated, changed, err := daemon.enforceBudgetForRun(context.Background(), project, wf, note, run)
		if err != nil {
			t.Fatal(err)
		}
		assertEqual(t, false, changed, "usage telemetry does not stop the runner")
		assertEqual(t, string(AttemptOutcomeNone), updated.AttemptOutcome, "usage does not set a budget outcome")
		attempts, err := store.ListAttemptsForRun(run.ProjectID, run.RecordID)
		if err != nil {
			t.Fatal(err)
		}
		if attempts[0].Outcome == string(AttemptOutcomeBudgetExceeded) {
			t.Fatalf("usage telemetry must not set a budget outcome: %#v", attempts[0])
		}
		turns, err := store.ListTurnsForAttempt(run.ActiveAttemptID)
		if err != nil {
			t.Fatal(err)
		}
		assertEqual(t, 2, len(turns), "turns remain recorded diagnostically")
	})

	t.Run("turn cap", func(t *testing.T) {
		store, daemon, run, project := codexExecGovernorFixture(t)
		defer store.Close()
		wf := defaultWorkflow()
		wf.Codex.MaxTurns = 1
		if changed, err := daemon.ingestCodexExecRawLog(run); err != nil {
			t.Fatal(err)
		} else if !changed {
			t.Fatal("expected raw log ingestion")
		}
		updated, changed, err := daemon.enforceTurnCapForRun(context.Background(), project, wf, run)
		if err != nil {
			t.Fatal(err)
		}
		assertEqual(t, true, changed, "turn cap changed")
		assertEqual(t, string(AttemptOutcomeTurnCapExhausted), updated.AttemptOutcome, "turn cap outcome")
		attempts, err := store.ListAttemptsForRun(run.ProjectID, run.RecordID)
		if err != nil {
			t.Fatal(err)
		}
		assertEqual(t, string(AttemptOutcomeTurnCapExhausted), attempts[0].Outcome, "turn cap attempt outcome")
		if attempts[0].Outcome == string(AttemptOutcomeEarlyExit) {
			t.Fatalf("turn cap must not record early_exit: %#v", attempts[0])
		}
	})
}

func TestUsageTelemetryIsOptionalAndCannotPauseCodexExec(t *testing.T) {
	store, daemon, run, project := codexExecGovernorFixture(t)
	defer store.Close()
	if err := writeText(run.RawLogPath, strings.Join([]string{
		`not json`,
		`{"type":"turn.completed","turn_id":"nested","token_usage":{"total":{"input_tokens":5,"output_tokens":2,"total_tokens":7}}}`,
		`{"type":"turn.completed","turn_id":"oversized","token_usage":{"input_tokens":1e100,"output_tokens":1e100,"total_tokens":1e100}}`,
		``,
	}, "\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := daemon.ingestCodexExecRawLog(run); err != nil {
		t.Fatalf("usage telemetry must not fail a valid run: %v", err)
	}
	updated, changed, err := daemon.enforceBudgetForRun(context.Background(), project, defaultWorkflow(), Note{}, run)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, false, changed, "optional usage telemetry does not pause the run")
	assertEqual(t, run.LeaseState, updated.LeaseState, "run remains live with malformed or oversized usage")
	turns, err := store.ListTurnsForAttempt(run.ActiveAttemptID)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, 2, len(turns), "nested and oversized usage remain diagnostic rows")
}

func TestCodexExecIngestReplayKeepsCompletedTurns(t *testing.T) {
	store, daemon, run, _ := codexExecGovernorFixture(t)
	defer store.Close()
	if changed, err := daemon.ingestCodexExecRawLog(run); err != nil {
		t.Fatal(err)
	} else if !changed {
		t.Fatal("expected first codex exec raw log ingestion to record events")
	}
	if changed, err := daemon.ingestCodexExecRawLog(run); err != nil {
		t.Fatal(err)
	} else if changed {
		t.Fatal("expected replay of the same raw log to be idempotent")
	}
	turns, err := store.ListTurnsForAttempt(run.ActiveAttemptID)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, 2, len(turns), "codex exec turns after replay")
	for _, turn := range turns {
		assertEqual(t, "completed", turn.Status, "turn status after replay "+turn.TurnID)
	}
	assertEqual(t, 5, turns[0].InputTokens, "first turn input tokens after replay")
	assertEqual(t, 1, turns[0].OutputTokens, "first turn output tokens after replay")
	assertEqual(t, 6, turns[0].TotalTokens, "first turn total tokens after replay")
	assertEqual(t, 11, turns[1].InputTokens, "second turn input tokens after replay")
	assertEqual(t, 2, turns[1].OutputTokens, "second turn output tokens after replay")
	assertEqual(t, 13, turns[1].TotalTokens, "second turn total tokens after replay")
	eventText, err := readText(run.EventSinkPath)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, 2, strings.Count(eventText, `"kind":"turn_started"`), "turn_started event count after replay")
	assertEqual(t, 2, strings.Count(eventText, `"kind":"turn_completed"`), "turn_completed event count after replay")
}

func TestCodexExecIngestSurfacesEventLogAppendFailure(t *testing.T) {
	store, daemon, run, _ := codexExecGovernorFixture(t)
	defer store.Close()
	if err := writeText(run.EventSinkPath, `{"seq":1}`); err != nil {
		t.Fatal(err)
	}
	changed, err := daemon.ingestCodexExecRawLog(run)
	if err == nil || !strings.Contains(err.Error(), "validate codex-exec event log before replay") || !strings.Contains(err.Error(), "partial trailing record") {
		t.Fatalf("expected surfaced codex-exec event-log failure, got changed=%t err=%v", changed, err)
	}
}

func TestCodexExecIngestValidatesTailBeforeDedupMatch(t *testing.T) {
	store, daemon, run, _ := codexExecGovernorFixture(t)
	defer store.Close()
	if err := writeText(run.RawLogPath, `{"type":"thread.started","thread_id":"session-start"}`+"\n"); err != nil {
		t.Fatal(err)
	}
	eventLog := NewEventLog(run.EventSinkPath)
	if err := eventLog.Append("thread_started", run.ActiveAttemptID, RunnerCodexExec, map[string]any{"session_ref": "session-start"}); err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(run.EventSinkPath, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(`{"seq":2`); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	changed, err := daemon.ingestCodexExecRawLog(run)
	if err == nil || !strings.Contains(err.Error(), "validate codex-exec event log before replay") || !strings.Contains(err.Error(), "partial trailing record") {
		t.Fatalf("expected malformed tail to fail before prefix dedup, got changed=%t err=%v", changed, err)
	}
	if changed {
		t.Fatal("malformed event log must not mutate replay state")
	}
}

func TestCodexExecInFlightSilenceUsesCommandCap(t *testing.T) {
	started := time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC)
	wf := defaultWorkflow()
	wf.Codex.TurnTimeoutMS = int((10 * time.Minute) / time.Millisecond)
	for _, tc := range []struct {
		name string
		raw  string
	}{
		{
			name: "command execution item",
			raw:  `{"method":"item/started","timestamp":"` + started.Format(time.RFC3339) + `","params":{"item":{"id":"cmd-1","type":"commandExecution","command":"go test ./..."}}}` + "\n",
		},
		{
			name: "response function call",
			raw:  `{"type":"response_item","payload":{"type":"function_call","name":"exec_command","arguments":"{\"cmd\":\"go test ./...\"}","call_id":"call-1"}}` + "\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rawLogPath := filepath.Join(t.TempDir(), "codex.raw.log")
			if err := writeText(rawLogPath, tc.raw); err != nil {
				t.Fatal(err)
			}
			run := codexExecHeartbeatRunForTest(rawLogPath, started)
			stalled, reason := runStallReason(run, wf, started.Add(daemonHeartbeatDeadThreshold+time.Second))
			assertEqual(t, false, stalled, "in-flight silence stalled")
			assertEqual(t, "", reason, "in-flight silence reason")
		})
	}
}

func TestCodexExecInFlightCommandCap(t *testing.T) {
	started := time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC)
	rawLogPath := filepath.Join(t.TempDir(), "codex.raw.log")
	if err := writeText(rawLogPath, `{"method":"item/started","timestamp":"`+started.Format(time.RFC3339)+`","params":{"item":{"id":"cmd-1","type":"commandExecution","command":"go test ./..."}}}`+"\n"); err != nil {
		t.Fatal(err)
	}
	wf := defaultWorkflow()
	wf.Codex.TurnTimeoutMS = int((3 * time.Minute) / time.Millisecond)
	stalled, reason := runStallReason(codexExecHeartbeatRunForTest(rawLogPath, started), wf, started.Add(3*time.Minute+time.Second))
	assertEqual(t, true, stalled, "in-flight cap stalled")
	if !strings.Contains(reason, "runner in-flight command exceeded cap") || !strings.Contains(reason, "command started 2026-07-08T12:00:00Z") {
		t.Fatalf("expected in-flight cap reason, got %q", reason)
	}
}

func TestCodexExecIdleHeartbeatReason(t *testing.T) {
	started := time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC)
	rawLogPath := filepath.Join(t.TempDir(), "codex.raw.log")
	if err := writeText(rawLogPath, strings.Join([]string{
		`{"method":"item/started","timestamp":"` + started.Format(time.RFC3339) + `","params":{"item":{"id":"cmd-1","type":"commandExecution","command":"go test ./..."}}}`,
		`{"method":"item/completed","timestamp":"` + started.Add(time.Second).Format(time.RFC3339) + `","params":{"item":{"id":"cmd-1","type":"commandExecution","status":"completed","exitCode":0}}}`,
		"",
	}, "\n")); err != nil {
		t.Fatal(err)
	}
	stalled, reason := runStallReason(codexExecHeartbeatRunForTest(rawLogPath, started), defaultWorkflow(), started.Add(daemonHeartbeatDeadThreshold+time.Second))
	assertEqual(t, true, stalled, "idle silence stalled")
	if !strings.Contains(reason, "runner heartbeat dead (idle)") || strings.Contains(reason, "in-flight") {
		t.Fatalf("expected idle heartbeat reason, got %q", reason)
	}
}

func TestFirstEventDeadlineToleratesFreshWrapperHeartbeat(t *testing.T) {
	started := time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC)
	run := RunStatus{
		Runner:           string(RunnerCodexExec),
		StartedAt:        started.Format(time.RFC3339),
		ProcessStartedAt: started.Format(time.RFC3339),
		LastHeartbeatAt:  started.Add(10 * time.Minute).Format(time.RFC3339),
	}
	stalled, reason := runStallReason(run, defaultWorkflow(), started.Add(10*time.Minute+time.Second))
	assertEqual(t, false, stalled, "fresh wrapper heartbeat without first event stalled")
	assertEqual(t, "", reason, "fresh wrapper heartbeat without first event reason")
}

func TestFirstEventDeadlineReportsDeadWrapperHeartbeat(t *testing.T) {
	started := time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC)
	heartbeatAt := started.Add(daemonFirstEventDeadline + time.Minute)
	run := RunStatus{
		Runner:           string(RunnerCodexExec),
		StartedAt:        started.Format(time.RFC3339),
		ProcessStartedAt: started.Format(time.RFC3339),
		LastHeartbeatAt:  heartbeatAt.Format(time.RFC3339),
	}
	stalled, reason := runStallReason(run, defaultWorkflow(), heartbeatAt.Add(daemonHeartbeatDeadThreshold+time.Second))
	assertEqual(t, true, stalled, "dead wrapper heartbeat before first event stalled")
	if !strings.Contains(reason, "runner heartbeat dead before first event") {
		t.Fatalf("expected dead heartbeat reason, got %q", reason)
	}
}

func TestCodexExecCompletionRecordsSucceeded(t *testing.T) {
	vault := automationTestVault(t)
	disableReviewerForTest(t, vault)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Codex exec completion", "risk": "low", "priority": "p0", "v7": "true"}, newV7Task)
	makeV7TaskDispatchableForTest(t, vault, "APP-T-0001")
	setAutomationV7TaskFields(t, vault, "APP-T-0001", map[string]any{"status": "review", "readiness": "waiting_on_review", "next_owner": "reviewer"})
	wfFile, err := loadWorkflow(vault)
	if err != nil {
		t.Fatal(err)
	}
	wfFile.Data.Codex.MaxTurns = 2
	rawWorkflow, err := yaml.Marshal(wfFile.Data)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeText(workflowPath(vault), "---\n"+strings.TrimSpace(string(rawWorkflow))+"\n---\n"+wfFile.Body); err != nil {
		t.Fatal(err)
	}
	project := registerAutomationTestProject(t, vault)
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	rawLogPath := filepath.Join(t.TempDir(), "codex.raw.log")
	statusPath := filepath.Join(t.TempDir(), "status.json")
	eventSinkPath := filepath.Join(t.TempDir(), "events.jsonl")
	if err := writeText(rawLogPath, fakeCodexExecJSONL("session-complete")); err != nil {
		t.Fatal(err)
	}
	if err := writeRunnerStatusFile(statusPath, 0); err != nil {
		t.Fatal(err)
	}
	run := RunStatus{
		ProjectID:       project.ProjectID,
		RecordID:        "APP-T-0001",
		ItemID:          "APP-T-0001",
		Runner:          string(RunnerCodexExec),
		Lane:            runLaneExecute,
		LeaseState:      string(LeaseStateRunning),
		AttemptOutcome:  string(AttemptOutcomeNone),
		ActiveAttemptID: "attempt-complete",
		SessionRef:      "session-complete",
		RawLogPath:      rawLogPath,
		EventSinkPath:   eventSinkPath,
		StatusPath:      statusPath,
		WorkspacePath:   t.TempDir(),
		AttemptCount:    1,
		UpdatedAt:       "2026-07-06T00:00:00Z",
	}
	if err := store.UpsertRun(run); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveAttempt(RunAttempt{AttemptID: run.ActiveAttemptID, ProjectID: run.ProjectID, RecordID: run.RecordID, ItemID: run.ItemID, Runner: run.Runner, Lane: run.Lane, RawLogPath: rawLogPath, EventSinkPath: eventSinkPath, StatusPath: statusPath}); err != nil {
		t.Fatal(err)
	}
	_ = store.Close()

	daemon, err := NewDaemon(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer daemon.Close()
	if err := daemon.PollOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	updated := latestRunForRecord(t, daemon.store, project.ProjectID, "APP-T-0001")
	assertEqual(t, string(LeaseStateReleased), updated.LeaseState, "completion lease")
	assertEqual(t, string(AttemptOutcomeSucceeded), updated.AttemptOutcome, "completion outcome")
	attempts, err := daemon.store.ListAttemptsForRun(project.ProjectID, "APP-T-0001")
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, string(AttemptOutcomeSucceeded), attempts[0].Outcome, "completion attempt outcome")
	assertEqual(t, 2, attempts[0].TurnsUsed, "completion turns used")
}

func codexExecHeartbeatRunForTest(rawLogPath string, at time.Time) RunStatus {
	return RunStatus{
		Runner:          string(RunnerCodexExec),
		RawLogPath:      rawLogPath,
		FirstEventAt:    at.Add(-time.Second).Format(time.RFC3339),
		LastEventAt:     at.Format(time.RFC3339),
		LastHeartbeatAt: at.Format(time.RFC3339),
	}
}

func codexExecGovernorFixture(t *testing.T) (*RuntimeStore, *Daemon, RunStatus, RegisteredProject) {
	t.Helper()
	stateRoot := filepath.Join(t.TempDir(), "state")
	t.Setenv("TUSKER_STATE_ROOT", stateRoot)
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	rawLogPath := filepath.Join(t.TempDir(), "codex.raw.log")
	if err := writeText(rawLogPath, fakeCodexExecJSONL("session-governor")); err != nil {
		t.Fatal(err)
	}
	run := RunStatus{
		ProjectID:       "project-1",
		RecordID:        "APP-T-0001",
		ItemID:          "APP-T-0001",
		Runner:          string(RunnerCodexExec),
		Lane:            runLaneExecute,
		LeaseState:      string(LeaseStateRunning),
		AttemptOutcome:  string(AttemptOutcomeNone),
		ActiveAttemptID: "attempt-governor",
		SessionRef:      "session-governor",
		RawLogPath:      rawLogPath,
		EventSinkPath:   filepath.Join(t.TempDir(), "events.jsonl"),
		WorkspacePath:   t.TempDir(),
		AttemptCount:    1,
	}
	if err := store.UpsertRun(run); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveAttempt(RunAttempt{AttemptID: run.ActiveAttemptID, ProjectID: run.ProjectID, RecordID: run.RecordID, ItemID: run.ItemID, Runner: run.Runner, Lane: run.Lane, RawLogPath: run.RawLogPath, EventSinkPath: run.EventSinkPath}); err != nil {
		t.Fatal(err)
	}
	return store, &Daemon{stateRoot: stateRoot, store: store}, run, RegisteredProject{ProjectID: "project-1"}
}

func installFakeCodexExec(t *testing.T, root string) {
	t.Helper()
	bin := filepath.Join(root, "bin")
	if err := ensureDir(bin); err != nil {
		t.Fatal(err)
	}
	argsLog := filepath.Join(root, "fake-codex-args.log")
	scriptPath := filepath.Join(bin, "codex")
	script := `#!/usr/bin/env python3
import json, os, sys
with open("` + filepath.ToSlash(argsLog) + `", "a", encoding="utf-8") as f:
    f.write(" ".join(sys.argv[1:]) + "\n")
session = "session-start"
if len(sys.argv) > 3 and sys.argv[1] == "exec" and sys.argv[2] == "resume":
    for arg in sys.argv[3:]:
        if arg and not arg.startswith("-"):
            session = arg
            break
sys.stdin.read()
for line in [
    {"type":"thread.started","session_id":session},
    {"type":"turn.started","session_id":session,"turn_id":"turn-1"},
    {"type":"turn.completed","session_id":session,"turn_id":"turn-1","token_usage":{"input_tokens":5,"output_tokens":1,"total_tokens":6}},
    {"type":"turn.started","session_id":session,"turn_id":"turn-2"},
    {"type":"turn.completed","session_id":session,"turn_id":"turn-2","token_usage":{"input_tokens":11,"output_tokens":2,"total_tokens":13}},
]:
    print(json.dumps(line), flush=True)
`
	if err := writeText(scriptPath, script); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(scriptPath, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func fakeCodexExecJSONL(session string) string {
	return strings.Join([]string{
		`{"type":"thread.started","session_id":"` + session + `"}`,
		`{"type":"turn.started","session_id":"` + session + `","turn_id":"turn-1"}`,
		`{"type":"turn.completed","session_id":"` + session + `","turn_id":"turn-1","token_usage":{"input_tokens":5,"output_tokens":1,"total_tokens":6}}`,
		`{"type":"turn.started","session_id":"` + session + `","turn_id":"turn-2"}`,
		`{"type":"turn.completed","session_id":"` + session + `","turn_id":"turn-2","token_usage":{"input_tokens":11,"output_tokens":2,"total_tokens":13}}`,
		"",
	}, "\n")
}
