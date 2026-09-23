package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/google/uuid"
)

func claudeFakeScript(t *testing.T, dir, body string) string {
	t.Helper()
	path := filepath.Join(dir, "fake-claude.py")
	if err := writeText(path, "#!/usr/bin/env python3\n"+body); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func claudeTestPolicy() CodexPolicy {
	return CodexPolicy{ApprovalPolicy: "never", ThreadSandbox: "read-only", TurnSandboxPolicy: "read-only"}
}

func waitForClaudePublishedStatus(t *testing.T, path string) runnerProcessStatus {
	t.Helper()
	deadline := time.Now().Add(runnerLiveTestWait)
	var lastErr error
	for time.Now().Before(deadline) {
		status, err := readRunnerProcessStatus(path)
		if err == nil {
			return status
		}
		lastErr = err
		// Publication links a synced temp file before removing its original
		// name, so the trusted reader can briefly observe two hard links.
		if !errors.Is(err, os.ErrNotExist) && !strings.Contains(err.Error(), "runner status has unexpected hard links") {
			t.Fatal(err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for trusted Claude status %s: %v", path, lastErr)
	return runnerProcessStatus{}
}

func TestClaudeDetachedSurvivesLauncherExit(t *testing.T) {
	if requestPath := os.Getenv("TUSKER_CLAUDE_LAUNCH_REQUEST"); requestPath != "" {
		if err := os.Setenv("TUSKER_STATE_ROOT", os.Getenv("TUSKER_CLAUDE_PARENT_STATE_ROOT")); err != nil {
			t.Fatal(err)
		}
		req, err := readRunnerWrapperRequest(requestPath)
		if err != nil {
			t.Fatal(err)
		}
		result, err := (&ClaudeRunner{}).Start(context.Background(), req.Start)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := json.Marshal(result)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(os.Getenv("TUSKER_CLAUDE_LAUNCH_RESULT"), raw, 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}
	store, req := setupRunnerWrapperRuntime(t)
	req.Runner = string(RunnerClaude)
	run, err := store.FindRun(req.Start.RecordID)
	if err != nil || run == nil {
		t.Fatalf("load wrapper fixture run: %#v, %v", run, err)
	}
	run.Runner = string(RunnerClaude)
	if err := store.UpsertRun(*run); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveAttempt(RunAttempt{AttemptID: req.Start.AttemptID, ProjectID: req.Start.ProjectID, RecordID: req.Start.RecordID, ItemID: req.Start.ItemID, Runner: string(RunnerClaude), Lane: req.Start.Lane, WorkRevision: req.Start.WorkRevision, WorkspacePath: req.Start.WorkspacePath, Outcome: string(AttemptOutcomeNone), StartedAt: time.Now().UTC().Format(time.RFC3339)}); err != nil {
		t.Fatal(err)
	}
	dir := req.Start.WorkspacePath
	script := claudeFakeScript(t, dir, `import json,os,sys,time
args=sys.argv[1:]
session=args[args.index("--session-id")+1]
with open(os.environ["TUSKER_CLAUDE_ARGV_PATH"],"w") as f: json.dump(args,f)
for line in sys.stdin:
    msg=json.loads(line)
    if msg.get("type")=="user":
        time.sleep(1)
        print(json.dumps({"type":"result","subtype":"success","is_error":False,"session_id":session}),flush=True)
        break
`)
	req.Start.Command = script + " --input-format stream-json --mcp-config fake-mcp.json --settings fake-settings.json"
	req.Start.CodexPolicy = claudeTestPolicy()
	t.Setenv("TUSKER_WRAPPER_EXE", demoTestBinary(t))
	argvPath := filepath.Join(dir, "argv.json")
	t.Setenv("TUSKER_CLAUDE_ARGV_PATH", argvPath)
	requestPath := filepath.Join(dir, "launcher-request.json")
	resultPath := filepath.Join(dir, "launcher-result.json")
	if err := writeRunnerWrapperRequest(requestPath, req); err != nil {
		t.Fatal(err)
	}
	launcher := exec.Command(os.Args[0], "-test.run=^TestClaudeDetachedSurvivesLauncherExit$")
	launcher.Env = append(os.Environ(), "TUSKER_CLAUDE_LAUNCH_REQUEST="+requestPath, "TUSKER_CLAUDE_LAUNCH_RESULT="+resultPath, "TUSKER_CLAUDE_PARENT_STATE_ROOT="+DefaultStateRoot())
	if out, err := launcher.CombinedOutput(); err != nil {
		t.Fatalf("launcher failed: %v\n%s", err, out)
	}
	raw, err := os.ReadFile(resultPath)
	if err != nil {
		t.Fatal(err)
	}
	var result StartResult
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	if _, err := uuid.Parse(result.SessionRef); err != nil {
		t.Fatalf("assigned session id %q: %v", result.SessionRef, err)
	}
	if result.PID <= 0 || !processExists(result.PID) {
		t.Fatalf("wrapper did not survive launcher exit: %#v", result)
	}
	t.Cleanup(func() {
		if processExists(result.PID) {
			_ = syscall.Kill(-result.PGID, syscall.SIGKILL)
		}
	})
	status := waitForClaudePublishedStatus(t, req.Start.StatusPath)
	if status.ExitCode != 0 {
		t.Fatalf("detached Claude status = %#v", status)
	}
	argvRaw, err := os.ReadFile(argvPath)
	if err != nil {
		t.Fatal(err)
	}
	var argv []string
	if err := json.Unmarshal(argvRaw, &argv); err != nil {
		t.Fatal(err)
	}
	if got := argv[len(argv)-1]; got != result.SessionRef {
		t.Fatalf("child session id %q != returned %q", got, result.SessionRef)
	}
	if run, err := store.FindRun(req.Start.RecordID); err != nil || run == nil {
		t.Fatalf("detached run missing: %#v, %v", run, err)
	}
}

func TestClaudeSessionIDAssignedAndResumed(t *testing.T) {
	start := []string{"/fake/claude", "-p", "--mcp-config", "mcp.json", "--settings", "settings.json", "--permission-mode", "plan"}
	id := uuid.NewString()
	fresh := claudeSessionArgv(start, id, nil)
	if !reflect.DeepEqual(fresh[len(fresh)-2:], []string{"--session-id", id}) {
		t.Fatalf("fresh argv missing assigned id: %v", fresh)
	}
	resumed := claudeSessionArgv(fresh, "", &ResumeRequest{SessionRef: id})
	want := append(append([]string{}, start...), "--resume", id)
	if !reflect.DeepEqual(resumed, want) {
		t.Fatalf("resume argv = %v; want %v", resumed, want)
	}
	if !(&ClaudeRunner{}).Capabilities().ResumeSession {
		t.Fatal("Claude native resume capability is false")
	}
	if err := validateClaudeSessionFlags("", fresh); err == nil {
		t.Fatal("profile-supplied session id was accepted")
	}
	dir := t.TempDir()
	t.Setenv("TUSKER_STATE_ROOT", filepath.Join(dir, "state"))
	argvPath := filepath.Join(dir, "argv.jsonl")
	t.Setenv("TUSKER_CLAUDE_ARGV_PATH", argvPath)
	script := claudeFakeScript(t, dir, `import json,os,sys
args=sys.argv[1:]
with open(os.environ["TUSKER_CLAUDE_ARGV_PATH"],"a") as f: f.write(json.dumps(args)+"\n")
session=args[args.index("--session-id")+1] if "--session-id" in args else args[args.index("--resume")+1]
for line in sys.stdin:
    if json.loads(line).get("type")=="user":
        print(json.dumps({"type":"result","subtype":"success","is_error":False,"session_id":session}),flush=True)
        break
`)
	req, err := runnerWrapperRequestForTest(dir)
	if err != nil {
		t.Fatal(err)
	}
	req.Start.Command = script + " --input-format stream-json --mcp-config mcp.json --settings settings.json --permission-mode plan"
	req.Start.CodexPolicy = claudeTestPolicy()
	req.Start.NativeSessionID = id
	started, err := startLiveClaude(context.Background(), req.Start, nil)
	if err != nil {
		t.Fatal(err)
	}
	if started.SessionRef != id {
		t.Fatalf("start returned session %q, want %q", started.SessionRef, id)
	}
	waitForClaudePublishedStatus(t, req.Start.StatusPath)
	req.Start.NativeSessionID = ""
	req.Start.AttemptID = "attempt-resume"
	req.Start.StatusPath = filepath.Join(dir, "resume.status.json")
	req.Start.RawLogPath = filepath.Join(dir, "resume.raw.log")
	resumedResult, err := startLiveClaude(context.Background(), req.Start, &ResumeRequest{SessionRef: id})
	if err != nil {
		t.Fatal(err)
	}
	if resumedResult.SessionRef != id {
		t.Fatalf("resume returned session %q, want %q", resumedResult.SessionRef, id)
	}
	waitForClaudePublishedStatus(t, req.Start.StatusPath)
	raw, err := os.ReadFile(argvPath)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) != 2 {
		t.Fatalf("fake Claude launches = %d, want 2", len(lines))
	}
	var observedStart, observedResume []string
	if err := json.Unmarshal([]byte(lines[0]), &observedStart); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(lines[1]), &observedResume); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(observedStart[len(observedStart)-2:], []string{"--session-id", id}) ||
		!reflect.DeepEqual(observedResume[len(observedResume)-2:], []string{"--resume", id}) {
		t.Fatalf("fake Claude session flags: start=%v resume=%v", observedStart, observedResume)
	}
	for _, flag := range []string{"--mcp-config", "mcp.json", "--settings", "settings.json", "--permission-mode", "plan"} {
		if !strings.Contains(strings.Join(observedStart, " "), flag) || !strings.Contains(strings.Join(observedResume, " "), flag) {
			t.Fatalf("flag %q was not carried across resume: start=%v resume=%v", flag, observedStart, observedResume)
		}
	}
	if strings.Contains(strings.Join(observedResume, " "), "--session-id") || strings.Contains(strings.Join(observedResume, " "), "--bare") {
		t.Fatalf("resume retained forbidden flags: %v", observedResume)
	}
}

func TestClaudeOversizedStdoutLine(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TUSKER_STATE_ROOT", filepath.Join(dir, "state"))
	script := claudeFakeScript(t, dir, `import json,sys
for line in sys.stdin:
    if json.loads(line).get("type")=="user":
        print("x"*(6*1024*1024),flush=True)
        print(json.dumps({"type":"result","subtype":"success","is_error":False,"session_id":"oversize-session"}),flush=True)
        break
`)
	req, err := runnerWrapperRequestForTest(dir)
	if err != nil {
		t.Fatal(err)
	}
	req.Start.Command = script + " --input-format stream-json"
	req.Start.CodexPolicy = claudeTestPolicy()
	result, err := startLiveClaude(context.Background(), req.Start, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.PID <= 0 {
		t.Fatal("fake Claude did not start")
	}
	status := waitForClaudePublishedStatus(t, req.Start.StatusPath)
	if status.ExitCode != 0 {
		t.Fatalf("oversized line killed run: %#v", status)
	}
	raw, err := readText(req.Start.RawLogPath)
	if err != nil || !strings.Contains(raw, "[truncated ") || strings.Contains(raw, strings.Repeat("x", 128*1024)) {
		t.Fatalf("raw log was not bounded: length=%d err=%v", len(raw), err)
	}
	events, err := readText(req.Start.EventSinkPath)
	if err != nil || !strings.Contains(events, "stdout_line_oversized") {
		t.Fatalf("missing oversized-line diagnostic: %v, %s", err, events)
	}
}
