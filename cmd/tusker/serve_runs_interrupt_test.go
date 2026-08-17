package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestServeRunInterruptUsesRunsInterruptSemanticsAndCanonicalReadback(t *testing.T) {
	server := newServeEmptyNeedsFixture(t)

	cmd := exec.Command("sleep", "30")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	pid := cmd.Process.Pid
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	t.Cleanup(func() { _ = syscall.Kill(-pid, syscall.SIGKILL) })

	if err := server.store.UpsertRun(RunStatus{
		ProjectID:        "app",
		RecordID:         "APP-T-0001",
		ItemID:           "APP-T-0001",
		Runner:           string(RunnerCodexExec),
		Lane:             runLaneExecute,
		LeaseState:       string(LeaseStateRunning),
		LeaseOwner:       "attempt-live",
		ActiveAttemptID:  "attempt-live",
		ProcessPID:       pid,
		ProcessPGID:      pid,
		ProcessStartedAt: recordedProcessStartTime(pid, time.Now().UTC().Format(time.RFC3339)),
		AttemptCount:     1,
		StartedAt:        "2026-07-06T06:00:00Z",
		UpdatedAt:        "2026-07-06T06:10:00Z",
		LastEventAt:      "2026-07-06T06:10:00Z",
		LastHeartbeatAt:  "2026-07-06T06:10:30Z",
	}); err != nil {
		t.Fatal(err)
	}

	var result serveInterruptResult
	servePost(t, server, "/api/runs/APP-T-0001/interrupt?project=app", `{}`, &result)
	if !result.OK || result.Refused || !result.Interrupted {
		t.Fatalf("expected confirmed interrupt, got %#v", result)
	}
	assertEqual(t, string(LeaseStateInterrupted), result.LeaseStateRaw, "interrupt raw lease")
	assertEqual(t, "released", result.LeaseState, "interrupt display lease")
	assertEqual(t, false, result.ProcessRunning, "interrupt process readback")
	if processExists(pid) {
		t.Fatalf("interrupt response claimed a stopped process while pid %d was still alive", pid)
	}

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("interrupt endpoint did not stop the live process")
	}

	stored, err := server.store.FindRun("APP-T-0001")
	if err != nil {
		t.Fatal(err)
	}
	if stored == nil {
		t.Fatal("interrupted run missing from runtime store")
	}
	assertEqual(t, string(LeaseStateInterrupted), stored.LeaseState, "stored interrupt lease")
	assertEqual(t, string(AttemptOutcomeCancelled), stored.AttemptOutcome, "stored interrupt outcome")
	assertEqual(t, 0, stored.ProcessPID, "stored interrupt pid")

	var detail serveRunDetail
	serveDecode(t, server, "/api/runs/APP-T-0001", &detail)
	assertEqual(t, string(LeaseStateInterrupted), detail.LeaseStateRaw, "detail interrupt raw lease")
	assertEqual(t, false, detail.ProcessRunning, "detail interrupt process")
}

func TestServeRunInterruptStopsProcessGroupDescendantsAfterLeaderExits(t *testing.T) {
	server := newServeEmptyNeedsFixture(t)
	childPIDPath := filepath.Join(t.TempDir(), "child.pid")
	script := `
trap 'exit 0' INT
sh -c 'trap "" INT TERM; while :; do sleep 1; done' &
child=$!
echo "$child" > "$CHILD_PID_PATH"
wait "$child"
`
	cmd := exec.Command("sh", "-c", script)
	cmd.Env = append(os.Environ(), "CHILD_PID_PATH="+childPIDPath)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	pid := cmd.Process.Pid
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	childPID := waitForPIDFile(t, childPIDPath)
	t.Cleanup(func() {
		_ = syscall.Kill(-pid, syscall.SIGKILL)
		_ = syscall.Kill(childPID, syscall.SIGKILL)
	})
	if err := server.store.UpsertRun(RunStatus{
		ProjectID: "app", RecordID: "APP-T-0001", ItemID: "APP-T-0001",
		Runner: string(RunnerCodexExec), Lane: runLaneExecute,
		LeaseState: string(LeaseStateRunning), LeaseOwner: "attempt-live", LeaseGeneration: 1,
		ActiveAttemptID: "attempt-live", ProcessPID: pid, ProcessPGID: pid,
		ProcessStartedAt: recordedProcessStartTime(pid, time.Now().UTC().Format(time.RFC3339)),
		StartedAt:        time.Now().UTC().Format(time.RFC3339), UpdatedAt: time.Now().UTC().Format(time.RFC3339),
	}); err != nil {
		t.Fatal(err)
	}

	var result serveInterruptResult
	servePost(t, server, "/api/runs/APP-T-0001/interrupt?project=app", `{}`, &result)
	if !result.OK || result.ProcessRunning {
		t.Fatalf("expected confirmed group interrupt, got %#v", result)
	}
	if processExists(childPID) || processGroupExists(pid) {
		t.Fatalf("interrupt left process-group descendant alive: leader=%d child=%d", pid, childPID)
	}
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("process-group leader was not reaped")
	}
}

func TestServeRunInterruptRefusesUnverifiedRecordedGroupWhenLeaderAlreadyExited(t *testing.T) {
	server := newServeEmptyNeedsFixture(t)
	childPIDPath := filepath.Join(t.TempDir(), "child.pid")
	script := `
sh -c 'trap "" INT TERM; while :; do sleep 1; done' &
child=$!
echo "$child" > "$CHILD_PID_PATH"
exit 0
`
	cmd := exec.Command("sh", "-c", script)
	cmd.Env = append(os.Environ(), "CHILD_PID_PATH="+childPIDPath)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	leaderPID := cmd.Process.Pid
	leaderStart := recordedProcessStartTime(leaderPID, time.Now().UTC().Format(time.RFC3339))
	childPID := waitForPIDFile(t, childPIDPath)
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	if processExists(leaderPID) {
		t.Fatalf("leader %d must be reaped before interrupt", leaderPID)
	}
	if processGroupID(childPID) != leaderPID || !processGroupExists(leaderPID) {
		t.Fatalf("expected live descendant in recorded group: leader=%d child=%d child_pgid=%d", leaderPID, childPID, processGroupID(childPID))
	}
	t.Cleanup(func() {
		_ = syscall.Kill(-leaderPID, syscall.SIGKILL)
		_ = syscall.Kill(childPID, syscall.SIGKILL)
	})
	original := RunStatus{
		ProjectID: "app", RecordID: "APP-T-0001", ItemID: "APP-T-0001",
		Runner: string(RunnerCodexExec), Lane: runLaneExecute,
		LeaseState: string(LeaseStateRunning), LeaseOwner: "attempt-orphaned", LeaseGeneration: 1,
		ActiveAttemptID: "attempt-orphaned", ProcessPID: leaderPID, ProcessPGID: leaderPID,
		ProcessStartedAt: leaderStart, StartedAt: time.Now().UTC().Format(time.RFC3339), UpdatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	if err := server.store.UpsertRun(original); err != nil {
		t.Fatal(err)
	}

	var result serveInterruptResult
	servePost(t, server, "/api/runs/APP-T-0001/interrupt?project=app", `{}`, &result)
	if result.OK || !result.Refused || !strings.Contains(result.Reason, "ownership cannot be verified") || !strings.Contains(result.Reason, "manually") {
		t.Fatalf("expected actionable orphaned-group refusal, got %#v", result)
	}
	if !processExists(childPID) || !processGroupExists(leaderPID) {
		t.Fatalf("unsafe refusal must leave the unverified group untouched: leader=%d child=%d", leaderPID, childPID)
	}
	stored, err := server.store.FindRun(original.RecordID)
	if err != nil || stored == nil {
		t.Fatalf("load refused run: %#v %v", stored, err)
	}
	assertEqual(t, original.LeaseState, stored.LeaseState, "refusal leaves lease state unchanged")
	assertEqual(t, original.LeaseOwner, stored.LeaseOwner, "refusal leaves lease owner unchanged")
	assertEqual(t, original.ProcessPID, stored.ProcessPID, "refusal leaves process pid unchanged")
	assertEqual(t, original.ProcessPGID, stored.ProcessPGID, "refusal leaves process group unchanged")
}

func TestDaemonInterruptSignalsOrphanedGroupWithMatchingLiveHandle(t *testing.T) {
	server := newServeEmptyNeedsFixture(t)
	childPIDPath := filepath.Join(t.TempDir(), "child.pid")
	script := `
sh -c 'trap "" INT TERM; while :; do sleep 1; done' &
child=$!
echo "$child" > "$CHILD_PID_PATH"
exit 0
`
	cmd := exec.Command("sh", "-c", script)
	cmd.Env = append(os.Environ(), "CHILD_PID_PATH="+childPIDPath)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	leaderPID := cmd.Process.Pid
	leaderStart := recordedProcessStartTime(leaderPID, time.Now().UTC().Format(time.RFC3339))
	childPID := waitForPIDFile(t, childPIDPath)
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	if processExists(leaderPID) {
		t.Fatalf("leader %d must be reaped before interrupt", leaderPID)
	}
	if processGroupID(childPID) != leaderPID || !processGroupExists(leaderPID) {
		t.Fatalf("expected live descendant in recorded group: leader=%d child=%d child_pgid=%d", leaderPID, childPID, processGroupID(childPID))
	}
	t.Cleanup(func() {
		_ = syscall.Kill(-leaderPID, syscall.SIGKILL)
		_ = syscall.Kill(childPID, syscall.SIGKILL)
	})

	original := RunStatus{
		ProjectID: "app", RecordID: "APP-T-0001", ItemID: "APP-T-0001",
		Runner: string(RunnerCodexExec), Lane: runLaneExecute,
		LeaseState: string(LeaseStateRunning), LeaseOwner: "attempt-verified-orphan", LeaseGeneration: 1,
		ActiveAttemptID: "attempt-verified-orphan", ProcessPID: leaderPID, ProcessPGID: leaderPID,
		ProcessStartedAt: leaderStart, StartedAt: time.Now().UTC().Format(time.RFC3339), UpdatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	if err := server.store.UpsertRun(original); err != nil {
		t.Fatal(err)
	}
	handle := &interruptTestLiveHandle{
		attemptID: original.ActiveAttemptID,
		projectID: original.ProjectID,
		recordID:  original.RecordID,
		itemID:    original.ItemID,
	}
	liveRegistry.Register(handle)
	t.Cleanup(func() { liveRegistry.Unregister(handle.attemptID) })

	daemon := &Daemon{store: server.store}
	if err := daemon.InterruptRun(context.Background(), original.RecordID); err != nil {
		t.Fatal(err)
	}
	assertEqual(t, 1, handle.interrupts, "matching live handle receives interrupt")
	if processExists(childPID) || processGroupExists(leaderPID) {
		t.Fatalf("verified interrupt left recorded process group alive: leader=%d child=%d", leaderPID, childPID)
	}
	stored, err := server.store.FindRun(original.RecordID)
	if err != nil || stored == nil {
		t.Fatalf("load interrupted run: %#v %v", stored, err)
	}
	assertEqual(t, string(LeaseStateInterrupted), stored.LeaseState, "verified handle permits interruption")
	assertEqual(t, 0, stored.ProcessPID, "verified interrupt clears process pid")
	assertEqual(t, 0, stored.ProcessPGID, "verified interrupt clears process group")
}

type interruptTestLiveHandle struct {
	attemptID  string
	projectID  string
	recordID   string
	itemID     string
	interrupts int
}

func (h *interruptTestLiveHandle) AttemptID() string  { return h.attemptID }
func (h *interruptTestLiveHandle) ProjectID() string  { return h.projectID }
func (h *interruptTestLiveHandle) RecordID() string   { return h.recordID }
func (h *interruptTestLiveHandle) ItemID() string     { return h.itemID }
func (h *interruptTestLiveHandle) Runner() RunnerName { return RunnerCodexExec }
func (h *interruptTestLiveHandle) Interrupt(context.Context) error {
	h.interrupts++
	return nil
}

func TestRunsInterruptRuntimeRunDoesNotOverwriteConcurrentLeaseClaim(t *testing.T) {
	server := newServeEmptyNeedsFixture(t)
	original := RunStatus{
		ProjectID: "app", RecordID: "APP-T-0001", ItemID: "APP-T-0001",
		Runner: string(RunnerCodexExec), Lane: runLaneExecute,
		LeaseState: string(LeaseStateUnclaimed), WorkRevision: 3,
		StartedAt: time.Now().UTC().Format(time.RFC3339), UpdatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	if err := server.store.UpsertRun(original); err != nil {
		t.Fatal(err)
	}
	var claimErr error
	_, viaDaemon, err := interruptRuntimeRunWithHook(DefaultStateRoot(), server.store, original.RecordID, func() {
		var claimed bool
		claimed, claimErr = server.store.ClaimRunLease(original.ProjectID, original.RecordID, "new-attempt", 1, defaultRunLeaseTTL, time.Now().UTC(), true, false, RuntimeLeaseClaimPrecondition{
			ExpectedLeaseState: LeaseStateUnclaimed, ExpectedOwner: "", ExpectedLeaseGeneration: 0, ExpectedWorkRevision: original.WorkRevision,
		})
		if claimErr == nil && !claimed {
			claimErr = errors.New("concurrent claim did not match original snapshot")
		}
	})
	if claimErr != nil {
		t.Fatal(claimErr)
	}
	if viaDaemon {
		t.Fatal("isolated runtime interrupt unexpectedly used daemon control")
	}
	var typed *TuskerError
	if !errors.As(err, &typed) || typed.Code != "CAS_CONFLICT" {
		t.Fatalf("expected stale interrupt CAS conflict, got %v", err)
	}
	stored, err := server.store.FindRun(original.RecordID)
	if err != nil || stored == nil {
		t.Fatalf("load claimed run: %#v %v", stored, err)
	}
	assertEqual(t, string(LeaseStateClaimed), stored.LeaseState, "concurrent claim lease state")
	assertEqual(t, "new-attempt", stored.LeaseOwner, "concurrent claim owner")
	assertEqual(t, 1, stored.LeaseGeneration, "concurrent claim generation")
}

func TestRunsInterruptSharedStorePathInterruptsDeadProcessRow(t *testing.T) {
	server := newServeEmptyNeedsFixture(t)
	if err := server.store.UpsertRun(RunStatus{
		ProjectID:      "app",
		RecordID:       "APP-T-0001",
		ItemID:         "APP-T-0001",
		Runner:         string(RunnerCodexExec),
		Lane:           runLaneExecute,
		LeaseState:     string(LeaseStateRetryQueued),
		AttemptCount:   1,
		StartedAt:      "2026-07-06T06:00:00Z",
		UpdatedAt:      "2026-07-06T06:10:00Z",
		LastEventAt:    "2026-07-06T06:10:00Z",
		NextRetryAt:    "2026-07-06T06:20:00Z",
		AttemptOutcome: string(AttemptOutcomeNone),
	}); err != nil {
		t.Fatal(err)
	}

	if err := runsInterruptCmd(Args{"id": "APP-T-0001", "json": "true"}); err != nil {
		t.Fatal(err)
	}
	stored, err := server.store.FindRun("APP-T-0001")
	if err != nil {
		t.Fatal(err)
	}
	if stored == nil {
		t.Fatal("CLI interrupt removed runtime row")
	}
	assertEqual(t, string(LeaseStateInterrupted), stored.LeaseState, "CLI interrupt lease")
	assertEqual(t, 0, stored.ProcessPID, "CLI interrupt pid")
}

func TestServeRunInterruptRefusalAndLocalhostGuard(t *testing.T) {
	server := newServeEmptyNeedsFixture(t)

	missingReq := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:7420/api/runs/APP-T-9999/interrupt?project=app", bytes.NewBufferString(`{}`))
	missingReq.Header.Set("Content-Type", "application/json")
	missingRec := httptest.NewRecorder()
	server.ServeHTTP(missingRec, missingReq)
	assertEqual(t, http.StatusNotFound, missingRec.Code, "interrupt missing-run status")
	var missing serveInterruptResult
	if err := json.Unmarshal(missingRec.Body.Bytes(), &missing); err != nil {
		t.Fatalf("decode missing-run refusal: %v\n%s", err, missingRec.Body.String())
	}
	if missing.OK || !missing.Refused || !strings.Contains(missing.Reason, "run not found") {
		t.Fatalf("expected visible missing-run refusal, got %#v", missing)
	}

	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:7420/api/runs/APP-T-0001/interrupt?project=app", bytes.NewBufferString(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://attacker.example")
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	assertEqual(t, http.StatusForbidden, rec.Code, "interrupt cross-origin status")
	if !strings.Contains(rec.Body.String(), "refused cross-origin mutation") {
		t.Fatalf("expected interrupt origin refusal, got %q", rec.Body.String())
	}
}

func TestServeRunInterruptQueuedDaemonDownReadbackIsFrozen(t *testing.T) {
	server := newServeEmptyNeedsFixture(t)
	if err := server.store.UpsertRun(RunStatus{
		ProjectID:    "app",
		RecordID:     "APP-T-0001",
		ItemID:       "APP-T-0001",
		Runner:       string(RunnerCodexExec),
		Lane:         runLaneExecute,
		LeaseState:   string(LeaseStateRetryQueued),
		AttemptCount: 1,
		StartedAt:    "2026-07-06T06:00:00Z",
		UpdatedAt:    "2026-07-06T06:10:00Z",
		LastEventAt:  "2026-07-06T06:10:00Z",
	}); err != nil {
		t.Fatal(err)
	}

	first := server.runSummary(serveSnapshot{projectID: "app", notesByID: map[string]Note{}}, RunStatus{
		ProjectID: "app", RecordID: "APP-T-0001", ItemID: "APP-T-0001",
		LeaseState: string(LeaseStateRetryQueued), StartedAt: "2026-07-06T06:00:00Z", UpdatedAt: "2026-07-06T06:10:00Z",
	})
	server.now = func() time.Time { return time.Date(2026, 7, 6, 9, 0, 0, 0, time.UTC) }
	second := server.runSummary(serveSnapshot{projectID: "app", notesByID: map[string]Note{}}, RunStatus{
		ProjectID: "app", RecordID: "APP-T-0001", ItemID: "APP-T-0001",
		LeaseState: string(LeaseStateRetryQueued), StartedAt: "2026-07-06T06:00:00Z", UpdatedAt: "2026-07-06T06:10:00Z",
	})
	assertEqual(t, "unclaimed", first.LeaseState, "queued display lease")
	assertEqual(t, first.ElapsedSec, second.ElapsedSec, "queued elapsed remains frozen")

	var daemon serveDaemonStatus
	serveDecode(t, server, "/api/daemon", &daemon)
	assertEqual(t, false, daemon.DaemonAlive, "fixture daemon liveness")
	if reason := strings.ToLower(strings.TrimSpace(toString(daemon.DaemonDownReason))); !strings.Contains(reason, "start the daemon") {
		t.Fatalf("expected actionable daemon-down reason, got %#v", daemon.DaemonDownReason)
	}
}
