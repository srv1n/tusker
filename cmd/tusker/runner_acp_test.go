package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"tusker/internal/acp"
)

func TestRunnerACPFencedWrapperFlowRecordsOnlyObservations(t *testing.T) {
	store, req := setupACPRunnerRuntime(t, "happy")
	result, err := runnerWrapperStartChild(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if result.PID <= 0 || result.PGID != req.ContainmentPGID {
		t.Fatalf("ACP child receipt did not retain wrapper containment: %#v", result)
	}
	if !strings.HasPrefix(result.SessionRef, "acp:v1:fake-acp:") {
		t.Fatalf("ACP session ref lacks protocol and adapter namespace: %q", result.SessionRef)
	}
	waitForStatusFile(t, req.Start.StatusPath)
	status, err := readRunnerProcessStatus(req.Start.StatusPath)
	if err != nil {
		t.Fatal(err)
	}
	if status.ExitCode != 0 || status.Outcome != "" {
		t.Fatalf("unexpected ACP terminal status: %#v", status)
	}

	run, err := store.FindRun(req.Start.RecordID)
	if err != nil {
		t.Fatal(err)
	}
	if run == nil || run.LeaseOwner != req.Start.AttemptID || run.LeaseGeneration != req.Start.LeaseGeneration || run.AttemptOutcome != string(AttemptOutcomeNone) {
		t.Fatalf("ACP transport changed task or lease authority: %#v", run)
	}
	events, err := readText(req.Start.EventSinkPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"acp_protocol_negotiated", "acp_session_bound", "acp_session_update_observed", "acp_turn_terminal", "observation_only"} {
		if !strings.Contains(events, want) {
			t.Fatalf("missing ACP observation %q in events:\n%s", want, events)
		}
	}
	for _, forbidden := range []string{"claim_work", "renew_lease", "accept_evidence", "pass_gate", "complete_task"} {
		if strings.Contains(events, forbidden) {
			t.Fatalf("ACP event carried forbidden authority action %q:\n%s", forbidden, events)
		}
	}
	rawLog, err := readText(req.Start.RawLogPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(rawLog, "test prompt") {
		t.Fatalf("ACP raw log retained the prompt: %q", rawLog)
	}
}

func TestRunnerACPRejectsUnfencedOrUnfingerprintedLaunch(t *testing.T) {
	_, req := setupACPRunnerRuntime(t, "happy")
	req.Start.ContainmentPGID = 0
	if result, err := startLiveACP(context.Background(), req.Start); err == nil || result != nil || !strings.Contains(err.Error(), "detached runner wrapper") {
		t.Fatalf("unfenced ACP launch was accepted: result=%#v err=%v", result, err)
	}

	req.Start.CommandExecutableFP = ""
	if result, err := (&ACPRunner{}).Start(context.Background(), req.Start); err == nil || result != nil || !strings.Contains(err.Error(), "fingerprint") {
		t.Fatalf("unfingerprinted ACP launch was accepted: result=%#v err=%v", result, err)
	}
}

func TestACPAuthorityAndCloudBoundariesRemainSeparate(t *testing.T) {
	runner := &ACPRunner{}
	if runner.Capabilities().ResumeSession {
		t.Fatal("generic ACP transport advertised resumability without a negotiated provider adapter")
	}
	if _, err := runner.Resume(context.Background(), ResumeRequest{SessionRef: "acp:v1:fake:session"}); err == nil {
		t.Fatal("generic ACP transport silently resumed a persisted provider session")
	}
	reconciled, err := runner.Reconcile(context.Background(), ReconcileRequest{SessionRef: "acp:v1:fake:session"})
	if err != nil {
		t.Fatal(err)
	}
	if reconciled.LeaseState != LeaseStateReleased || reconciled.Outcome != AttemptOutcomeAbandoned || !strings.Contains(reconciled.Reason, "no automatic resume") {
		t.Fatalf("unexpected ACP reconciliation: %#v", reconciled)
	}
	collected, err := runner.Collect(context.Background(), CollectRequest{AttemptID: "attempt-1"})
	if err != nil || len(collected.Artifacts) != 0 {
		t.Fatalf("ACP collect must remain transport-only: result=%#v err=%v", collected, err)
	}

	cloud := &CodexCloudRunner{Config: codexCloudTestConfig("manual", "none"), Executor: &fakeCodexCloudExecutor{}}
	if cloud.Name() != RunnerCodexCloud || cloud.Name() == RunnerACP {
		t.Fatalf("codex_cloud was aliased to ACP: cloud=%s acp=%s", cloud.Name(), runner.Name())
	}
}

func TestACPAuthorityPermissionObservationFailsClosed(t *testing.T) {
	dir := t.TempDir()
	provenance := acpAttemptProvenance{
		Principal: "actor-derived:human:operator", Actor: "human:operator", AttemptID: "attempt-acp-1",
		Adapter: "fake-acp", ProcessID: 42, SessionID: acpStoredSessionRef("fake-acp", "session-1"),
	}
	decision, err := evaluateACPTransportPermission(context.Background(), NewEventLog(filepath.Join(dir, "events.jsonl")), provenance, acp.PermissionRequest{
		SessionID: "session-1", ToolCallID: "tool-unsafe", Options: []acp.PermissionOption{{ID: "allow_once", Kind: "allow_once"}},
	})
	if err != nil || decision != acp.Reject {
		t.Fatalf("provider permission widened generic ACP authority: decision=%s err=%v", decision, err)
	}
	events, err := readText(filepath.Join(dir, "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"acp_permission_decided", "tool-unsafe", "invalid_request", "observation_only"} {
		if !strings.Contains(events, want) {
			t.Fatalf("permission provenance omitted %q: %s", want, events)
		}
	}
	if strings.Contains(events, "allow_once") {
		t.Fatalf("permission decision retained or selected provider approval data: %s", events)
	}
}

func TestACPRuntimeEnvironmentIsPositiveAndControlPlaneFree(t *testing.T) {
	t.Setenv("TUSKER_STATE_ROOT", "/should-not-cross-boundary")
	t.Setenv("TUSKER_RUNNER_PATH_PREFIX", "/attacker/bin")
	t.Setenv("OPENAI_API_KEY", "fixture-key")
	env := acpRunnerEnvironment(StartRequest{RunnerPathPrefix: "/attacker/bin"}, "/workspace", CodexPolicy{})
	seen := make(map[string]string, len(env))
	for _, entry := range env {
		key, value, ok := strings.Cut(entry, "=")
		if !ok {
			t.Fatalf("malformed ACP environment entry %q", entry)
		}
		if strings.HasPrefix(key, "TUSKER_") {
			t.Fatalf("ACP environment leaked control-plane variable %q", entry)
		}
		seen[key] = value
	}
	if seen["OPENAI_API_KEY"] != "fixture-key" {
		t.Fatalf("explicit provider credential was not retained: %q", seen["OPENAI_API_KEY"])
	}
	if strings.Contains(seen["PATH"], "/attacker/bin") || seen["PATH"] == "" {
		t.Fatalf("ACP PATH was not fixed and prefix-free: %q", seen["PATH"])
	}
	if _, ok := seen["TUSKER_STATE_ROOT"]; ok {
		t.Fatal("ACP environment contains TUSKER_STATE_ROOT")
	}
}

func TestACPRuntimeDetectsReapedChildWithoutTerminalStatus(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe, "-test.run=^TestACPReapedChildHelper$")
	cmd.Env = append(os.Environ(), "TUSKER_ACP_REAPED_CHILD_HELPER=1")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	if !acpChildExitedWithoutStatus(&StartResult{PID: cmd.Process.Pid}, filepath.Join(t.TempDir(), "missing.status")) {
		t.Fatal("reaped ACP child without status was not detected")
	}
}

func TestRunnerACPWrapperPublishesFailureForReapedChildWithoutStatus(t *testing.T) {
	_, req := setupRunnerWrapperRuntime(t)
	req.Runner = string(RunnerACP)
	req.ContainmentPGID = 0
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe, "-test.run=^TestACPReapedChildHelper$")
	cmd.Env = append(os.Environ(), "TUSKER_ACP_REAPED_CHILD_HELPER=1")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	if err := runnerWrapperWait(context.Background(), func() {}, req, &StartResult{PID: cmd.Process.Pid}, make(chan string)); err != nil {
		t.Fatalf("wrapper fallback returned error: %v", err)
	}
	status, err := readRunnerProcessStatus(req.Start.StatusPath)
	if err != nil {
		t.Fatal(err)
	}
	if status.ExitCode != 1 || AttemptOutcome(status.Outcome) != AttemptOutcomeFailed || !strings.Contains(status.Reason, "without terminal status") {
		t.Fatalf("wrapper did not publish bounded ACP failure: %#v", status)
	}
}

func TestRunnerACPOverflowTerminatorCannotBlockProducer(t *testing.T) {
	writer, err := openBoundedRawLog(filepath.Join(t.TempDir(), "overflow.log"), 1, false)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.close()
	started := make(chan struct{})
	release := make(chan struct{})
	writer.bindTerminator(func() {
		close(started)
		<-release
	})
	startedAt := time.Now()
	if _, err := writer.Write([]byte("overflow")); !errors.Is(err, errAuthoritativeRawLogOverflow) {
		t.Fatalf("overflow write error=%v", err)
	}
	if elapsed := time.Since(startedAt); elapsed > 200*time.Millisecond {
		t.Fatalf("overflow producer blocked for %s", elapsed)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("asynchronous overflow terminator did not start")
	}
	close(release)
}

func TestRunnerACPRejectsShebangBeforeLaunch(t *testing.T) {
	_, req := setupACPRunnerRuntime(t, "happy")
	scriptDir := t.TempDir()
	script := filepath.Join(scriptDir, "adapter.sh")
	sideEffect := filepath.Join(scriptDir, "executed")
	if err := os.WriteFile(script, []byte("#!/bin/sh\ntouch "+sideEffect+"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	fingerprint, err := acpExecutableFingerprint(script)
	if err != nil {
		t.Fatal(err)
	}
	req.Start.CommandArgv = []string{script}
	req.Start.CommandExecutableFP = fingerprint
	if _, _, _, err := resolveACPRunnerLaunch(req.Start); err == nil || !strings.Contains(err.Error(), "shebang") {
		t.Fatalf("shebang adapter was accepted: %v", err)
	}
	if fileExists(sideEffect) {
		t.Fatal("shebang adapter executed before generic ACP rejection")
	}
}

func TestACPReapedChildHelper(t *testing.T) {
	if os.Getenv("TUSKER_ACP_REAPED_CHILD_HELPER") == "1" {
		return
	}
}

func TestRunnerACPCancelledStartReapsAfterPublishingStatus(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repoRootForFreshCloneTest(t), "cmd", "tusker", "runner_wrapper.go"))
	if err != nil {
		t.Fatal(err)
	}
	source := string(raw)
	const contract = `runner wrapper cancelled before child terminal status",
			)
			runnerWrapperReapACPContainmentAfterStatus(req)`
	if !strings.Contains(source, contract) {
		t.Fatal("cancelled ACP start does not reap containment after publishing its bounded status")
	}
}

func setupACPRunnerRuntime(t *testing.T, mode string) (*RuntimeStore, runnerWrapperRequest) {
	t.Helper()
	store, req := setupRunnerWrapperRuntime(t)
	adapter := fakeACPBinary(t)
	fingerprint, err := acpExecutableFingerprint(adapter)
	if err != nil {
		t.Fatal(err)
	}
	req.Runner = string(RunnerACP)
	req.ContainmentPGID = processGroupID(os.Getpid())
	req.Start.Command = ""
	req.Start.CommandArgv = []string{adapter, "--mode", mode}
	req.Start.CommandExecutableFP = fingerprint
	req.Start.RawLogMaxBytes = 64 * 1024

	run, err := store.FindRun(req.Start.RecordID)
	if err != nil || run == nil {
		t.Fatalf("find ACP test run: run=%#v err=%v", run, err)
	}
	run.Runner = string(RunnerACP)
	if err := store.UpsertRun(*run); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveRunAuthorization(RunAuthorization{
		ProjectID: req.Start.ProjectID, RecordID: req.Start.RecordID, LeaseGeneration: req.Start.LeaseGeneration,
		Source: "test", Actor: "human:runner-acp-test", Trigger: "test", CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}); err != nil {
		t.Fatal(err)
	}
	return store, req
}

func TestRunnerACPStoredSessionNamespaceIsStable(t *testing.T) {
	got := acpStoredSessionRef("fake-acp", "session/with spaces")
	if got != "acp:v1:fake-acp:c2Vzc2lvbi93aXRoIHNwYWNlcw" {
		t.Fatalf("stored ACP session namespace=%q", got)
	}
	if filepath.IsAbs(got) {
		t.Fatalf("stored ACP session became a filesystem path: %q", got)
	}
}

func TestRunnerACPWrapperReapsItsContainedDescendantAfterStatus(t *testing.T) {
	dir := t.TempDir()
	statusPath := filepath.Join(dir, "terminal.status.json")
	pidPath := filepath.Join(dir, "descendant.pid")
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe, "-test.run=^TestRunnerACPWrapperReapHelper$")
	cmd.Env = append(os.Environ(),
		"TUSKER_ACP_REAP_HELPER=1",
		"TUSKER_ACP_REAP_STATUS="+statusPath,
		"TUSKER_ACP_REAP_DESCENDANT_PID="+pidPath,
	)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	err = cmd.Run()
	if err == nil {
		t.Fatal("contained ACP wrapper helper survived its terminal group reap")
	}
	waitForStatusFile(t, statusPath)
	pidRaw, err := readText(pidPath)
	if err != nil {
		t.Fatal(err)
	}
	pid := atoiSafe(strings.TrimSpace(pidRaw))
	if pid <= 0 {
		t.Fatalf("invalid contained descendant pid %q", pidRaw)
	}
	deadline := time.Now().Add(time.Second)
	for processExists(pid) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if processExists(pid) {
		_ = syscall.Kill(pid, syscall.SIGKILL)
		t.Fatalf("ACP contained descendant %d survived terminal group reap", pid)
	}
}

func TestRunnerACPWrapperReapHelper(t *testing.T) {
	if os.Getenv("TUSKER_ACP_REAP_HELPER") != "1" {
		return
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	descendant := exec.Command(exe, "-test.run=^TestRunnerACPWrapperReapDescendant$")
	descendant.Env = append(os.Environ(), "TUSKER_ACP_REAP_DESCENDANT=1")
	if err := descendant.Start(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(os.Getenv("TUSKER_ACP_REAP_DESCENDANT_PID"), []byte(strconv.Itoa(descendant.Process.Pid)), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeRunnerStatusFile(os.Getenv("TUSKER_ACP_REAP_STATUS"), 0); err != nil {
		t.Fatal(err)
	}
	runnerWrapperReapACPContainmentAfterStatus(runnerWrapperRequest{
		Runner:          string(RunnerACP),
		ContainmentPGID: os.Getpid(),
	})
	t.Fatal("ACP wrapper group reap returned without terminating its containment group")
}

func TestRunnerACPWrapperReapDescendant(t *testing.T) {
	if os.Getenv("TUSKER_ACP_REAP_DESCENDANT") != "1" {
		return
	}
	select {}
}
