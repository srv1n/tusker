package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

type runnerWrapperRequest struct {
	Runner string         `json:"runner"`
	Start  StartRequest   `json:"start"`
	Resume *ResumeRequest `json:"resume,omitempty"`
}

func startDetachedRunnerWrapper(ctx context.Context, runner RunnerName, req StartRequest, resume *ResumeRequest, capabilities RunnerCapabilities) (*StartResult, error) {
	_ = ctx
	if err := ensureDir(filepath.Dir(req.StatusPath)); err != nil {
		return nil, err
	}
	requestPath := req.StatusPath + ".wrapper-request.json"
	raw, err := json.MarshalIndent(runnerWrapperRequest{Runner: string(runner), Start: req, Resume: resume}, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := writeText(requestPath, string(raw)+"\n"); err != nil {
		return nil, err
	}
	exe, err := runnerWrapperExecutable()
	if err != nil {
		return nil, err
	}
	wrapperLogPath := runnerWrapperLogPath(req)
	if err := ensureDir(filepath.Dir(wrapperLogPath)); err != nil {
		return nil, err
	}
	cmd := exec.Command(exe, "runner-wrapper", "--request", requestPath)
	if cwd, err := runnerWorkspaceCWD(runner, req.WorkspacePath); err == nil {
		cmd.Dir = cwd
	}
	cmd.Env = os.Environ()
	if stateRoot := strings.TrimSpace(DefaultStateRoot()); stateRoot != "" {
		cmd.Env = append(cmd.Env, "TUSKER_STATE_ROOT="+stateRoot)
	}
	closeStdin := attachDevNullStdin(cmd)
	defer closeStdin()
	if file, err := os.OpenFile(wrapperLogPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644); err == nil {
		defer file.Close()
		cmd.Stdout = file
		cmd.Stderr = file
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	pid := cmd.Process.Pid
	pgid := processGroupID(pid)
	processStartedAt := recordedProcessStartTime(pid, time.Now().UTC().Format(time.RFC3339))
	_ = cmd.Process.Release()
	_ = NewEventLog(req.EventSinkPath).Append("attempt_wrapper_spawned", req.AttemptID, runner, map[string]any{
		"pid":          pid,
		"pgid":         pgid,
		"request_path": requestPath,
		"log_path":     wrapperLogPath,
		"status_path":  req.StatusPath,
	})
	return &StartResult{
		StartedAt:    processStartedAt,
		PID:          pid,
		PGID:         pgid,
		ProcessStart: processStartedAt,
		StatusPath:   req.StatusPath,
		Capabilities: capabilities,
		Completed:    false,
		Outcome:      AttemptOutcomeNone,
	}, nil
}

func runnerWrapperLogPath(req StartRequest) string {
	if strings.TrimSpace(req.VaultPath) != "" && strings.TrimSpace(req.ItemID) != "" {
		return filepath.Join(req.VaultPath, "scratch", req.ItemID, "runner-wrapper.log")
	}
	return req.RawLogPath + ".wrapper.log"
}

func runnerWrapperCmd(args Args) error {
	requestPath, err := requireArg(args, "request")
	if err != nil {
		return err
	}
	req, err := readRunnerWrapperRequest(requestPath)
	if err != nil {
		return err
	}
	return runRunnerWrapper(context.Background(), req)
}

func runnerWrapperExecutable() (string, error) {
	if exe := strings.TrimSpace(os.Getenv("TUSKER_WRAPPER_EXE")); exe != "" {
		return exe, nil
	}
	return os.Executable()
}

func runRunnerWrapper(parent context.Context, req runnerWrapperRequest) error {
	ctx, stopSignals := signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
	defer stopSignals()
	stopHeartbeat := make(chan string, 1)
	heartbeatCtx, cancelHeartbeat := context.WithCancel(ctx)
	defer cancelHeartbeat()
	go runnerWrapperHeartbeat(heartbeatCtx, req.Start, stopHeartbeat)

	result, err := runnerWrapperStartChild(ctx, req)
	if err != nil {
		_ = writeRunnerStatusFile(req.Start.StatusPath, 1)
		return err
	}
	return runnerWrapperWait(ctx, req, result, stopHeartbeat)
}

func runnerWrapperStartChild(ctx context.Context, req runnerWrapperRequest) (*StartResult, error) {
	runner := RunnerName(strings.TrimSpace(req.Runner))
	switch runner {
	case RunnerCodexAppServer:
		if req.Resume != nil {
			return startLiveCodex(ctx, req.Start, req.Resume)
		}
		return startLiveCodex(ctx, req.Start, nil)
	case RunnerCodexExec:
		return executeRunnerCommand(ctx, runner, runnerExecRequest{
			ProjectID: req.Start.ProjectID, RecordID: req.Start.RecordID, ItemID: req.Start.ItemID, AttemptID: req.Start.AttemptID,
			Lane: req.Start.Lane, WorkRevision: req.Start.WorkRevision, LeaseGeneration: req.Start.LeaseGeneration, WorkingDir: req.Start.WorkingDir, WorkspacePath: req.Start.WorkspacePath,
			RepoRoot: req.Start.RepoRoot, PromptPath: req.Start.PromptPath, EventSinkPath: req.Start.EventSinkPath, RawLogPath: req.Start.RawLogPath, StatusPath: req.Start.StatusPath,
			Command: req.Start.Command, NotePath: req.Start.NotePath, VaultPath: req.Start.VaultPath, CodexPolicy: req.Start.CodexPolicy, ExternalLoop: req.Start.ExternalLoop,
		}, RunnerCapabilities{StructuredEvents: true, MachineFinalStatus: true, UsageMetrics: true})
	default:
		return nil, tuskerError(errorConfigInvalid, "runner wrapper does not support runner "+string(runner))
	}
}

func runnerWrapperWait(ctx context.Context, req runnerWrapperRequest, result *StartResult, stopHeartbeat <-chan string) error {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		if fileExists(req.Start.StatusPath) {
			return nil
		}
		select {
		case reason := <-stopHeartbeat:
			runnerWrapperInterrupt(req, result, reason)
			return runnerWrapperWaitForStatus(req.Start.StatusPath, runnerWrapperStopTimeout())
		case <-ctx.Done():
			runnerWrapperInterrupt(req, result, ctx.Err().Error())
			return runnerWrapperWaitForStatus(req.Start.StatusPath, runnerWrapperStopTimeout())
		case <-ticker.C:
		}
	}
}

func runnerWrapperWaitForStatus(statusPath string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if fileExists(statusPath) {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	_ = writeRunnerStatusFile(statusPath, 130)
	return nil
}

func runnerWrapperInterrupt(req runnerWrapperRequest, result *StartResult, reason string) {
	_ = appendRawLogLine(req.Start.RawLogPath, "runner wrapper stopping: "+reason)
	if handle := liveRegistry.Find(req.Start.AttemptID); handle != nil {
		_ = handle.Interrupt(context.Background())
		return
	}
	if result != nil && result.PGID > 0 {
		_ = syscall.Kill(-result.PGID, syscall.SIGINT)
		for i := 0; i < 20 && result.PID > 0 && processExists(result.PID); i++ {
			time.Sleep(100 * time.Millisecond)
		}
		if result.PID > 0 && processExists(result.PID) {
			_ = syscall.Kill(-result.PGID, syscall.SIGTERM)
		}
	}
}

func runnerWrapperHeartbeat(ctx context.Context, req StartRequest, stop chan<- string) {
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		nonBlockingWrapperStop(stop, err.Error())
		return
	}
	defer store.Close()
	interval := runnerWrapperHeartbeatInterval()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if ok := runnerWrapperBeat(store, req); !ok {
			nonBlockingWrapperStop(stop, "lease heartbeat stopped")
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func runnerWrapperBeat(store *RuntimeStore, req StartRequest) bool {
	if store == nil {
		return false
	}
	dispatchable := runnerWrapperDispatchable(req)
	renewed, err := store.RenewRunLease(RuntimeLeaseRenewal{
		ProjectID:      req.ProjectID,
		RecordID:       req.RecordID,
		Owner:          req.AttemptID,
		Generation:     req.LeaseGeneration,
		TTL:            defaultRunLeaseTTL,
		Now:            time.Now().UTC(),
		Dispatchable:   dispatchable,
		ProcessPID:     os.Getpid(),
		ProcessPGID:    processGroupID(os.Getpid()),
		ProcessStarted: recordedProcessStartTime(os.Getpid(), time.Now().UTC().Format(time.RFC3339)),
	})
	return err == nil && renewed
}

func runnerWrapperDispatchable(req StartRequest) bool {
	if strings.TrimSpace(req.NotePath) == "" {
		return true
	}
	data, _, err := parseFrontmatterMustRead(req.NotePath)
	if err != nil {
		return false
	}
	status := stringField(data, "status")
	if strings.TrimSpace(req.Lane) == runLaneReview {
		return status == "review"
	}
	return containsString(req.ActiveStates, status)
}

func nonBlockingWrapperStop(stop chan<- string, reason string) {
	select {
	case stop <- reason:
	default:
	}
}

func runnerWrapperHeartbeatInterval() time.Duration {
	if raw := strings.TrimSpace(os.Getenv("TUSKER_WRAPPER_HEARTBEAT_MS")); raw != "" {
		if ms := atoiSafe(raw); ms > 0 {
			return time.Duration(ms) * time.Millisecond
		}
	}
	return defaultRunHeartbeatInterval
}

func runnerWrapperStopTimeout() time.Duration {
	if raw := strings.TrimSpace(os.Getenv("TUSKER_WRAPPER_STOP_TIMEOUT_MS")); raw != "" {
		if ms := atoiSafe(raw); ms > 0 {
			return time.Duration(ms) * time.Millisecond
		}
	}
	return 10 * time.Second
}

func waitForWrapperDrain(store *RuntimeStore, timeout time.Duration) (bool, error) {
	if store == nil {
		return true, nil
	}
	deadline := time.Now().UTC().Add(timeout)
	for {
		running := 0
		runs, err := store.ListRuns()
		if err != nil {
			return false, err
		}
		for _, run := range runs {
			if isDispatchingLeaseState(run.LeaseState) && run.ProcessPID > 0 && processIdentityMatches(run) {
				running++
			}
		}
		if running == 0 {
			return true, nil
		}
		if time.Now().UTC().After(deadline) {
			return false, nil
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func wrapperStatusPathForTest(dir string) string {
	return filepath.Join(dir, "runner.status.json")
}

func runnerWrapperRequestForTest(dir string) (runnerWrapperRequest, error) {
	promptPath := filepath.Join(dir, "prompt.md")
	eventPath := filepath.Join(dir, "events.jsonl")
	rawLogPath := filepath.Join(dir, "raw.log")
	statusPath := wrapperStatusPathForTest(dir)
	notePath := filepath.Join(dir, "task.md")
	if err := writeText(promptPath, "test prompt\n"); err != nil {
		return runnerWrapperRequest{}, err
	}
	if err := writeText(notePath, "---\nid: APP-T-0001\nstatus: ready\n---\n"); err != nil {
		return runnerWrapperRequest{}, err
	}
	return runnerWrapperRequest{
		Runner: string(RunnerCodexExec),
		Start: StartRequest{
			ProjectID: "project-1", RecordID: "APP-T-0001", ItemID: "APP-T-0001", AttemptID: "attempt-wrapper",
			Lane: runLaneExecute, WorkRevision: 1, LeaseGeneration: 1, ActiveStates: []string{"ready", "rework"},
			WorkingDir: dir, WorkspacePath: dir, RepoRoot: dir, PromptPath: promptPath, EventSinkPath: eventPath,
			RawLogPath: rawLogPath, StatusPath: statusPath, Command: "sh -c 'sleep 5'", NotePath: notePath, VaultPath: dir,
		},
	}, nil
}

func writeRunnerWrapperRequest(path string, req runnerWrapperRequest) error {
	raw, err := json.MarshalIndent(req, "", "  ")
	if err != nil {
		return err
	}
	return writeText(path, string(raw)+"\n")
}

func readRunnerWrapperRequest(path string) (runnerWrapperRequest, error) {
	raw, err := readText(path)
	if err != nil {
		return runnerWrapperRequest{}, err
	}
	var req runnerWrapperRequest
	if err := json.Unmarshal([]byte(raw), &req); err != nil {
		return runnerWrapperRequest{}, err
	}
	if strings.TrimSpace(req.Start.AttemptID) == "" {
		return runnerWrapperRequest{}, fmt.Errorf("wrapper request missing attempt id")
	}
	return req, nil
}
