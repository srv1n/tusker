package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCodexCloudRunnerStartStoresCloudRefsWithoutSessionRef(t *testing.T) {
	tempRoot := t.TempDir()
	workspaceRoot := filepath.Join(tempRoot, "workspace")
	if err := ensureDir(workspaceRoot); err != nil {
		t.Fatal(err)
	}
	promptPath := filepath.Join(tempRoot, "prompt.md")
	rawLogPath := filepath.Join(tempRoot, "cloud.raw.log")
	eventSinkPath := filepath.Join(tempRoot, "cloud.events.jsonl")
	if err := writeText(promptPath, "Run cloud task.\n"); err != nil {
		t.Fatal(err)
	}
	exec := &fakeCodexCloudExecutor{outputs: [][]byte{[]byte(`{
		"task":{"id":"cloud-task-123","status":"queued","environment_id":"env-prod","attempt_number":2},
		"pull_request":{"url":"https://github.example/acme/repo/pull/7"},
		"apply":{"id":"apply-456"},
		"logs_summary":"queued on Codex Cloud",
		"final_summary":"pending"
	}`)}}
	runner := &CodexCloudRunner{
		Config:   codexCloudTestConfig("manual", "none"),
		Executor: exec,
	}

	result, err := runner.Start(context.Background(), StartRequest{
		ProjectID:     "project-1",
		RecordID:      "record-1",
		ItemID:        "APP-T-0001",
		AttemptID:     "attempt-1",
		WorkRevision:  3,
		WorkspacePath: workspaceRoot,
		PromptPath:    promptPath,
		RawLogPath:    rawLogPath,
		EventSinkPath: eventSinkPath,
		StatusPath:    filepath.Join(tempRoot, "cloud.status.json"),
	})
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "", result.SessionRef, "cloud task is not a local session ref")
	assertEqual(t, "cloud-task-123", result.CloudTaskID, "cloud task id")
	assertEqual(t, "queued", result.CloudStatus, "remote status")
	assertEqual(t, "env-prod", result.CloudEnvironmentID, "environment id")
	assertEqual(t, 2, result.CloudAttemptNumber, "cloud attempt")
	assertEqual(t, "apply-456", result.ApplyRef, "apply ref")
	assertEqual(t, "https://github.example/acme/repo/pull/7", result.PullRequestURL, "pull request url")
	assertEqual(t, false, result.Completed, "queued cloud task is not completed")
	assertEqual(t, AttemptOutcomeNone, result.Outcome, "queued outcome")
	assertEqual(t, "Run cloud task.\n", exec.requests[0].Stdin, "cloud start stdin")
	assertContainsEnv(t, exec.requests[0].Env, "TUSKER_SESSION_REF=")
	assertContainsEnv(t, exec.requests[0].Env, "TUSKER_CODEX_CLOUD_ENVIRONMENT_ID=env-prod")
	assertContainsEnv(t, exec.requests[0].Env, "TUSKER_CODEX_CLOUD_APPLY_MODE=manual")
	assertContainsEnv(t, exec.requests[0].Env, "TUSKER_CODEX_CLOUD_PR_MODE=none")
	logText, err := readText(rawLogPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(logText, "cloud-task-123") {
		t.Fatalf("expected raw log to retain cloud output, got:\n%s", logText)
	}
	eventText, err := readText(eventSinkPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(eventText, "codex_cloud_task_started") || !strings.Contains(eventText, "cloud-task-123") {
		t.Fatalf("expected cloud start event, got:\n%s", eventText)
	}
}

func TestCodexCloudRunnerStartSurfacesEventLogFailure(t *testing.T) {
	tempRoot := t.TempDir()
	workspaceRoot := filepath.Join(tempRoot, "workspace")
	if err := ensureDir(workspaceRoot); err != nil {
		t.Fatal(err)
	}
	promptPath := filepath.Join(tempRoot, "prompt.md")
	eventSinkPath := filepath.Join(tempRoot, "events.jsonl")
	if err := writeText(promptPath, "Run cloud task.\n"); err != nil {
		t.Fatal(err)
	}
	if err := writeText(eventSinkPath, `{"seq":1}`); err != nil {
		t.Fatal(err)
	}
	executor := &fakeCodexCloudExecutor{outputs: [][]byte{
		[]byte(`{"task_id":"cloud-task-123","status":"queued"}`),
	}}
	runner := &CodexCloudRunner{
		Config:   codexCloudTestConfig("manual", "none"),
		Executor: executor,
	}
	result, err := runner.Start(context.Background(), StartRequest{
		ProjectID: "project-1", RecordID: "record-1", ItemID: "APP-T-0001", AttemptID: "attempt-1",
		WorkspacePath: workspaceRoot, PromptPath: promptPath,
		RawLogPath: filepath.Join(tempRoot, "raw.log"), EventSinkPath: eventSinkPath,
	})
	if err == nil || !strings.Contains(err.Error(), "preflight codex cloud event sink") || !strings.Contains(err.Error(), "partial trailing record") {
		t.Fatalf("expected surfaced cloud event-log failure, got result=%#v err=%v", result, err)
	}
	assertEqual(t, 0, len(executor.requests), "cloud launch count after failed event preflight")
}

func TestCodexCloudRunnerPreservesTaskIDWhenPostLaunchEventAppendFails(t *testing.T) {
	tempRoot := t.TempDir()
	workspaceRoot := filepath.Join(tempRoot, "workspace")
	if err := ensureDir(workspaceRoot); err != nil {
		t.Fatal(err)
	}
	promptPath := filepath.Join(tempRoot, "prompt.md")
	eventSinkPath := filepath.Join(tempRoot, "events.jsonl")
	rawLogPath := filepath.Join(tempRoot, "raw.log")
	if err := writeText(promptPath, "Run cloud task.\n"); err != nil {
		t.Fatal(err)
	}
	executor := &fakeCodexCloudExecutor{runFn: func(codexCloudExecRequest) ([]byte, error) {
		if err := writeText(eventSinkPath, `{"seq":1}`); err != nil {
			return nil, err
		}
		return []byte(`{"task_id":"cloud-task-preserved","status":"queued"}`), nil
	}}
	runner := &CodexCloudRunner{Config: codexCloudTestConfig("manual", "none"), Executor: executor}

	result, err := runner.Start(context.Background(), StartRequest{
		ProjectID: "project-1", RecordID: "record-1", ItemID: "APP-T-0001", AttemptID: "attempt-1",
		WorkspacePath: workspaceRoot, PromptPath: promptPath, RawLogPath: rawLogPath, EventSinkPath: eventSinkPath,
	})
	if err == nil || !strings.Contains(err.Error(), "cloud-task-preserved") || !strings.Contains(err.Error(), rawLogPath) {
		t.Fatalf("expected durable recovery details for post-launch event failure, got result=%#v err=%v", result, err)
	}
	raw, readErr := readText(rawLogPath)
	if readErr != nil || !strings.Contains(raw, "cloud-task-preserved") {
		t.Fatalf("cloud task id was not preserved in raw log: %q err=%v", raw, readErr)
	}
}

func TestCodexCloudRunnerTracksTaskWhenRawLogFailsAfterLaunch(t *testing.T) {
	tempRoot := t.TempDir()
	workspaceRoot := filepath.Join(tempRoot, "workspace")
	if err := ensureDir(workspaceRoot); err != nil {
		t.Fatal(err)
	}
	promptPath := filepath.Join(tempRoot, "prompt.md")
	eventSinkPath := filepath.Join(tempRoot, "events.jsonl")
	rawLogPath := filepath.Join(tempRoot, "raw.log")
	if err := writeText(promptPath, "Run cloud task.\n"); err != nil {
		t.Fatal(err)
	}
	executor := &fakeCodexCloudExecutor{runFn: func(codexCloudExecRequest) ([]byte, error) {
		if err := os.Remove(rawLogPath); err != nil {
			return nil, err
		}
		if err := os.Mkdir(rawLogPath, 0o700); err != nil {
			return nil, err
		}
		return []byte(`{"task_id":"cloud-task-event-tracked","status":"queued"}`), nil
	}}
	runner := &CodexCloudRunner{Config: codexCloudTestConfig("manual", "none"), Executor: executor}

	result, err := runner.Start(context.Background(), StartRequest{
		ProjectID: "project-1", RecordID: "record-1", ItemID: "APP-T-0001", AttemptID: "attempt-1",
		WorkspacePath: workspaceRoot, PromptPath: promptPath, RawLogPath: rawLogPath, EventSinkPath: eventSinkPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "cloud-task-event-tracked", result.CloudTaskID, "cloud task tracked after raw log failure")
	events, readErr := readText(eventSinkPath)
	if readErr != nil || !strings.Contains(events, "cloud-task-event-tracked") || !strings.Contains(events, "raw_log_error") {
		t.Fatalf("event log did not surface raw-log failure with task id: %q err=%v", events, readErr)
	}
}

func TestCodexCloudRunnerReconcileMapsRemoteStatuses(t *testing.T) {
	tests := []struct {
		name    string
		status  string
		lease   LeaseState
		outcome AttemptOutcome
	}{
		{name: "queued", status: "queued", lease: LeaseStateClaimed, outcome: AttemptOutcomeNone},
		{name: "running", status: "running", lease: LeaseStateRunning, outcome: AttemptOutcomeNone},
		{name: "completed", status: "completed", lease: LeaseStateReleased, outcome: AttemptOutcomeSucceeded},
		{name: "failed", status: "failed", lease: LeaseStateReleased, outcome: AttemptOutcomeFailed},
		{name: "needs-input", status: "needs-input", lease: LeaseStateReleased, outcome: AttemptOutcomeWaitingForHuman},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			runner := &CodexCloudRunner{
				Config:   codexCloudTestConfig("pull_request", "draft"),
				Executor: &fakeCodexCloudExecutor{outputs: [][]byte{[]byte(`{"task_id":"cloud-task-123","status":"` + tc.status + `","environment_id":"env-prod","attempt_number":4}`)}},
			}
			result, err := runner.Reconcile(context.Background(), ReconcileRequest{CloudTaskID: "cloud-task-123"})
			if err != nil {
				t.Fatal(err)
			}
			assertEqual(t, tc.lease, result.LeaseState, "lease state")
			assertEqual(t, tc.outcome, result.Outcome, "attempt outcome")
			assertEqual(t, "cloud-task-123", result.CloudTaskID, "cloud task id")
			assertEqual(t, tc.status, result.CloudStatus, "remote status")
			assertEqual(t, 4, result.CloudAttemptNumber, "cloud attempt")
		})
	}
}

func TestCodexCloudRunnerManualApplyRecordsApplyRefWithoutApplying(t *testing.T) {
	exec := &fakeCodexCloudExecutor{outputs: [][]byte{
		[]byte(`{"task_id":"cloud-task-123","status":"completed","apply":{"command":"codex apply apply-789"},"summary":"ready to apply"}`),
		[]byte(`{"task_id":"cloud-task-123","status":"completed","apply":{"command":"codex apply apply-789"},"pull_request":{"url":"https://github.example/acme/repo/pull/9"},"logs_summary":"done","summary":"ready to apply"}`),
	}}
	runner := &CodexCloudRunner{
		Config:   codexCloudTestConfig("manual", "none"),
		Executor: exec,
	}
	result, err := runner.Reconcile(context.Background(), ReconcileRequest{CloudTaskID: "cloud-task-123"})
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, LeaseStateReleased, result.LeaseState, "manual apply release")
	assertEqual(t, AttemptOutcomeWaitingForHuman, result.Outcome, "manual apply waits for operator")
	assertEqual(t, "codex apply apply-789", result.ApplyRef, "apply ref")
	if !strings.Contains(result.Reason, "manual apply required") {
		t.Fatalf("expected manual apply reason, got %q", result.Reason)
	}
	collect, err := runner.Collect(context.Background(), CollectRequest{CloudTaskID: "cloud-task-123"})
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "codex apply apply-789", collect.Artifacts["apply_ref"], "collected apply ref")
	assertEqual(t, "https://github.example/acme/repo/pull/9", collect.Artifacts["pull_request_url"], "collected PR url")
	for _, req := range exec.requests {
		if strings.Contains(req.Command, "codex apply") {
			t.Fatalf("manual apply path must not run apply command, got %q", req.Command)
		}
	}
}

func TestCodexCloudRunnerExternalCollectDoesNotWaitForHumanOnManualApplyRef(t *testing.T) {
	runner := &CodexCloudRunner{
		Config: CodexCloudConfig{
			EnvironmentID:   "chatgpt-browser",
			ApplyMode:       "manual",
			PRMode:          "none",
			ExternalCollect: true,
			Command:         "chatgpt-handoff tusker-start --json",
			StatusCommand:   "chatgpt-handoff tusker-status --job {{cloud_task_id}} --json",
			CollectCommand:  "chatgpt-handoff tusker-collect --job {{cloud_task_id}} --json",
		},
		Executor: &fakeCodexCloudExecutor{outputs: [][]byte{[]byte(`{"task_id":"cgpt-123","status":"completed","apply_ref":"architect/cgpt-123/fix.patch","summary":"ready to collect"}`)}},
	}
	result, err := runner.Reconcile(context.Background(), ReconcileRequest{CloudTaskID: "cgpt-123"})
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, LeaseStateReleased, result.LeaseState, "external collect release")
	assertEqual(t, AttemptOutcomeSucceeded, result.Outcome, "external collect is a Tusker collect step, not human wait")
	assertEqual(t, "architect/cgpt-123/fix.patch", result.ApplyRef, "apply ref")
	if !strings.Contains(result.Reason, "external collect required") {
		t.Fatalf("expected external collect reason, got %q", result.Reason)
	}
}

func TestParseCodexCloudJSONLFixtureMergesStatusAndArtifacts(t *testing.T) {
	snapshot, err := parseCodexCloudOutput([]byte(strings.Join([]string{
		`{"task":{"id":"cloud-task-123","status":"queued","environment_id":"env-prod","attempt_number":1}}`,
		`{"task":{"id":"cloud-task-123","status":"completed"},"pull_request":{"html_url":"https://github.example/acme/repo/pull/7"},"patch":{"id":"patch-321"},"logs":{"summary":"tests passed"},"result":{"summary":"implemented"}}`,
	}, "\n")))
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "cloud-task-123", snapshot.TaskID, "task id")
	assertEqual(t, "completed", snapshot.Status, "latest status")
	assertEqual(t, "env-prod", snapshot.EnvironmentID, "environment id")
	assertEqual(t, 1, snapshot.AttemptNumber, "attempt number")
	assertEqual(t, "https://github.example/acme/repo/pull/7", snapshot.PullRequestURL, "pr url")
	assertEqual(t, "patch-321", snapshot.ApplyRef, "apply ref")
	assertEqual(t, "tests passed", snapshot.LogsSummary, "logs summary")
	assertEqual(t, "implemented", snapshot.FinalSummary, "final summary")
}

func TestValidateWorkflowCodexCloudConfig(t *testing.T) {
	wfFile := WorkflowFile{Path: "WORKFLOW.md", Body: defaultWorkflowMarkdown()}
	wfFile.Data = defaultWorkflow()
	wfFile.Data.Agents.Default = string(RunnerCodexCloud)
	wfFile.Data.Agents.Enabled = append(wfFile.Data.Agents.Enabled, string(RunnerCodexCloud))
	err := validateWorkflowFile(wfFile)
	if err == nil || !strings.Contains(err.Error(), "codex_cloud.environment_id is required") {
		t.Fatalf("expected missing cloud environment error, got %v", err)
	}
	wfFile.Data.CodexCloud = codexCloudTestConfig("manual", "none")
	if err := validateWorkflowFile(wfFile); err != nil {
		t.Fatalf("expected valid cloud workflow, got %v", err)
	}
	wfFile.Data.CodexCloud.ApprovalPolicy = "on-request"
	err = validateWorkflowFile(wfFile)
	if err == nil || !strings.Contains(err.Error(), "codex_cloud.approval_policy is local app-server-only") {
		t.Fatalf("expected app-server-only rejection, got %v", err)
	}
}

func TestRunnerForNameWiresCodexCloudRunner(t *testing.T) {
	wf := defaultWorkflow()
	wf.CodexCloud = codexCloudTestConfig("pull_request", "draft")

	runner, command, err := runnerForName(string(RunnerCodexCloud), wf)
	if err != nil {
		t.Fatal(err)
	}
	cloud, ok := runner.(*CodexCloudRunner)
	if !ok {
		t.Fatalf("expected CodexCloudRunner, got %T", runner)
	}
	assertEqual(t, string(RunnerCodexCloud), string(cloud.Name()), "runner name")
	assertEqual(t, wf.CodexCloud.Command, command, "configured command")
	assertEqual(t, "env-prod", cloud.Config.EnvironmentID, "cloud environment")
	assertEqual(t, "pull_request", cloud.Config.ApplyMode, "apply mode")
}

func TestRunnerForNameUsesExplicitCodexAdapters(t *testing.T) {
	wf := defaultWorkflow()
	wf.Runners["local-review"] = RunnerDefinition{Kind: string(RunnerCodexAppServer), Command: "codex app-server"}
	wf.Runners["local-oneshot"] = RunnerDefinition{Kind: string(RunnerCodexExec), Command: "codex exec --skip-git-repo-check -"}

	appServer, appCommand, err := runnerForName("local-review", wf)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := appServer.(*CodexAppServerRunner); !ok {
		t.Fatalf("expected CodexAppServerRunner, got %T", appServer)
	}
	assertEqual(t, "codex app-server", appCommand, "app-server command")

	execRunner, execCommand, err := runnerForName("local-oneshot", wf)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := execRunner.(*CodexExecRunner); !ok {
		t.Fatalf("expected CodexExecRunner, got %T", execRunner)
	}
	assertEqual(t, "codex exec --skip-git-repo-check -", execCommand, "exec command")
}

func TestCodexCloudRunnerSurfacesExecutorErrors(t *testing.T) {
	runner := &CodexCloudRunner{
		Config:   codexCloudTestConfig("manual", "none"),
		Executor: &fakeCodexCloudExecutor{err: errors.New("boom")},
	}
	_, err := runner.Reconcile(context.Background(), ReconcileRequest{CloudTaskID: "cloud-task-123"})
	if err == nil || !strings.Contains(err.Error(), "codex cloud status failed") {
		t.Fatalf("expected executor error, got %v", err)
	}
}

type fakeCodexCloudExecutor struct {
	outputs  [][]byte
	err      error
	runFn    func(codexCloudExecRequest) ([]byte, error)
	requests []codexCloudExecRequest
}

func (f *fakeCodexCloudExecutor) RunCodexCloud(ctx context.Context, req codexCloudExecRequest) ([]byte, error) {
	f.requests = append(f.requests, req)
	if f.runFn != nil {
		return f.runFn(req)
	}
	if f.err != nil {
		return nil, f.err
	}
	if len(f.outputs) == 0 {
		return nil, nil
	}
	output := f.outputs[0]
	f.outputs = f.outputs[1:]
	return output, nil
}

func codexCloudTestConfig(applyMode, prMode string) CodexCloudConfig {
	return CodexCloudConfig{
		EnvironmentID:  "env-prod",
		ApplyMode:      applyMode,
		PRMode:         prMode,
		Command:        "codex cloud start --json --environment {{environment_id}} --apply-mode {{apply_mode}} --pr-mode {{pr_mode}}",
		StatusCommand:  "codex cloud status --json {{cloud_task_id}}",
		CollectCommand: "codex cloud status --json {{cloud_task_id}}",
	}
}

func assertContainsEnv(t *testing.T, env []string, expected string) {
	t.Helper()
	for _, value := range env {
		if value == expected {
			return
		}
	}
	t.Fatalf("expected env %q in %#v", expected, env)
}
