package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

func TestServeReadOnlyAndLocalhost(t *testing.T) {
	addr, err := serveBindAddr(Args{})
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, defaultServeAddr, addr, "default serve addr")
	if _, err := serveBindAddr(Args{"addr": "0.0.0.0:7420"}); err == nil {
		t.Fatal("expected non-loopback bind to be rejected")
	}

	server := newServeFixture(t)
	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:7420/api/tasks", nil)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	assertEqual(t, http.StatusMethodNotAllowed, rec.Code, "mutating API route status")

	req = httptest.NewRequest(http.MethodGet, "/work/app", nil)
	rec = httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	assertEqual(t, http.StatusOK, rec.Code, "SPA fallback status")
	if !strings.Contains(rec.Body.String(), "serve fixture") {
		t.Fatalf("expected embedded index fallback, got %q", rec.Body.String())
	}
}

func TestServeNeedsSignals(t *testing.T) {
	server := newServeFixture(t)
	var needs []map[string]any
	serveDecode(t, server, "/api/needs", &needs)

	byID := map[string]map[string]any{}
	for _, need := range needs {
		byID[need["id"].(string)] = need
	}
	for _, id := range []string{
		"need-gate-APP-G-0001",
		"need-review-APP-T-0004",
		"need-failed-APP-T-0007",
	} {
		if _, ok := byID[id]; !ok {
			t.Fatalf("expected need %s in %#v", id, needs)
		}
	}
	if _, ok := byID["need-review-APP-T-0002"]; ok {
		t.Fatal("low-risk review task must not enter needs")
	}
	if _, ok := byID["need-review-APP-T-0001"]; ok {
		t.Fatal("risk alone must not create a human review need")
	}
	if _, ok := byID["need-failed-APP-T-0008"]; ok {
		t.Fatal("retry-queued failure must not enter needs")
	}
	var criticalTask serveTaskDetail
	serveDecode(t, server, "/api/tasks/APP-T-0001", &criticalTask)
	assertEqual(t, "critical", criticalTask.Risk, "critical risk passthrough")
	assertEqual(t, "provision", byID["need-gate-APP-G-0001"]["kind"], "gate need kind")
	assertEqual(t, "APP-G-0001", byID["need-gate-APP-G-0001"]["gateId"], "gate need identity")
	assertEqual(t, float64(2), byID["need-review-APP-T-0004"]["reworkCount"], "rework bounce count")
}

func TestServeNeedsEmptyReturnsArray(t *testing.T) {
	server := newServeEmptyNeedsFixture(t)
	req := httptest.NewRequest(http.MethodGet, "/api/needs", nil)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("/api/needs returned %d: %s", rec.Code, rec.Body.String())
	}
	if body := strings.TrimSpace(rec.Body.String()); body != "[]" {
		t.Fatalf("expected empty needs to marshal as [], got %q", body)
	}
	var needs []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &needs); err != nil {
		t.Fatalf("decode /api/needs: %v\n%s", err, rec.Body.String())
	}
	if needs == nil || len(needs) != 0 {
		t.Fatalf("expected decoded empty needs slice, got %#v", needs)
	}
}

func TestServeGateNeedAppearsOnceWithEveryBlockedTask(t *testing.T) {
	tasks := []Note{
		{Data: map[string]any{"kind": "task", "id": "APP-T-0001", "title": "First", "status": "ready", "priority": "p1", "risk": "medium"}},
		{Data: map[string]any{"kind": "task", "id": "APP-T-0002", "title": "Second", "status": "ready", "priority": "p1", "risk": "medium"}},
	}
	gate := Note{Data: map[string]any{
		"kind": "gate", "id": "APP-G-0001", "title": "Approve both", "gate_kind": "signoff",
		"status": "open", "owner": "human:sarav", "blocking": true,
		"blocks": []any{"APP-T-0001", "[[APP-T-0002]]"}, "action": "Review both tasks.",
	}}
	snap := serveSnapshot{projectID: "app", projectName: "App", tasks: tasks, gates: []Note{gate}, queue: map[string]automationTaskExplanation{}}
	needs := serveNeeds(snap, time.Now())
	if len(needs) != 1 {
		t.Fatalf("expected one need for one gate, got %#v", needs)
	}
	assertEqual(t, "APP-G-0001", needs[0]["gateId"], "first-class gate identity")
	assertEqual(t, []string{"APP-T-0001", "APP-T-0002"}, needs[0]["blockedTaskIds"], "all blocked tasks")
}

func TestServeDiscardPreflightAndDetachFlow(t *testing.T) {
	server := newServeFixture(t)
	var preview serveActionResult
	servePost(t, server, "/api/tasks/APP-T-0005/discard?project=app", `{"dryRun":true}`, &preview)
	if !preview.OK || preview.Discard == nil {
		t.Fatalf("expected discard impact, got %#v", preview)
	}
	assertEqual(t, true, preview.Discard.RequiresResolution, "serve discard dependency resolution")
	assertEqual(t, "APP-T-0006", preview.Discard.DirectDependents[0].ID, "serve discard dependent")

	var refused serveActionResult
	servePost(t, server, "/api/tasks/APP-T-0005/discard?project=app", `{"reason":"No longer planned."}`, &refused)
	if !refused.Refused {
		t.Fatalf("expected unresolved discard refusal, got %#v", refused)
	}

	var discarded serveActionResult
	servePost(t, server, "/api/tasks/APP-T-0005/discard?project=app", `{"reason":"No longer planned.","dependents":"detach"}`, &discarded)
	if !discarded.OK || discarded.Refused {
		t.Fatalf("expected discard success, got %#v", discarded)
	}
	data, _, err := parseFrontmatterMustRead(filepath.Join(server.vaultPath, "work", "tasks", "APP-T-0005.md"))
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "cancelled", stringField(data, "status"), "serve discarded task status")
	dependent, _, err := parseFrontmatterMustRead(filepath.Join(server.vaultPath, "work", "tasks", "APP-T-0006.md"))
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, []string{}, normalizeList(dependent["dependencies"]), "serve detached dependency")
}

func TestServeSummaryProjectsBadgeCounts(t *testing.T) {
	server := newServeFixture(t)
	var summary serveSummary
	serveDecode(t, server, "/api/summary", &summary)
	if summary.Attention == 0 || summary.Review == 0 {
		t.Fatalf("expected attention and review counts from fixture, got %#v", summary)
	}
	if summary.GeneratedAt == "" {
		t.Fatal("summary must include generated_at")
	}
	snap, err := server.loadSummarySnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.queue) != 0 {
		t.Fatalf("summary must not compute automation queue explanations, got %d", len(snap.queue))
	}
	full, err := server.loadSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if want := len(serveNeeds(full, server.now())); summary.Attention != want {
		t.Fatalf("summary attention=%d, want canonical needs count %d", summary.Attention, want)
	}
}

func TestServeFieldsRosterAndEpics(t *testing.T) {
	server := newServeFixture(t)

	var task serveTaskDetail
	serveDecode(t, server, "/api/tasks/APP-T-0003", &task)
	assertEqual(t, "app", task.ProjectID, "task project id")
	if len(task.Gates) != 1 {
		t.Fatalf("expected one gate, got %#v", task.Gates)
	}
	assertEqual(t, false, task.Gates[0].Satisfied, "gate satisfied flag")
	assertEqual(t, "provision", task.Gates[0].Kind, "gate kind payload")
	assertEqual(t, "Provision test credentials.", task.Gates[0].Ask, "gate ask")
	if len(task.OpenGates) != 1 || task.OpenGates[0].ID != "APP-G-0001" {
		t.Fatalf("expected open human gate capsule, got %#v", task.OpenGates)
	}
	assertEqual(t, "Provision test credentials.", task.OpenGates[0].Action, "gate action summary")
	if len(task.HumanActions) != 1 || task.HumanActions[0].GateID != "APP-G-0001" {
		t.Fatalf("expected every human action in task detail, got %#v", task.HumanActions)
	}

	var runs []map[string]any
	serveDecode(t, server, "/api/runs", &runs)
	var failed map[string]any
	var parked map[string]any
	for _, run := range runs {
		if run["taskId"] == "APP-T-0007" {
			failed = run
		}
		if run["taskId"] == "APP-T-0009" {
			parked = run
		}
	}
	if failed == nil {
		t.Fatal("expected APP-T-0007 run")
	}
	assertEqual(t, "boom", failed["error"], "terminal error text")
	assertEqual(t, false, failed["terminal"], "terminal flag")
	if failed["lastHeartbeatAt"] != nil {
		t.Fatalf("empty heartbeat must be explicit null: %#v", failed)
	}
	if parked == nil {
		t.Fatal("expected APP-T-0009 run")
	}
	assertEqual(t, "parked-no-progress", parked["outcome"], "parked run outcome")
	assertEqual(t, "parked_no_progress", parked["leaseStateRaw"], "parked raw lease")

	var epics []serveEpicSummary
	serveDecode(t, server, "/api/epics", &epics)
	seenTRC := false
	for _, epic := range epics {
		if epic.ID == "TRC" {
			seenTRC = true
			if epic.Counts["ready"] == 0 {
				t.Fatalf("expected TRC ready count from vault tasks: %#v", epic)
			}
		}
	}
	if !seenTRC {
		t.Fatalf("expected TRC epic derived from vault, got %#v", epics)
	}

	var roster map[string]any
	serveDecode(t, server, "/api/roster", &roster)
	rows := roster["rows"].([]any)
	if len(rows) == 0 {
		t.Fatal("expected roster rows from runs")
	}
	row := rows[0].(map[string]any)
	for _, key := range []string{"workingOn", "blockedOn", "handingOffTo"} {
		if _, ok := row[key]; !ok {
			t.Fatalf("roster row missing %s: %#v", key, row)
		}
	}
}

func TestServeHumanActionContractAndReviewProjection(t *testing.T) {
	server := newServeEmptyNeedsFixture(t)
	writeServeTask(t, server.vaultPath, serveTaskSeed{ID: "APP-T-0010", Epic: "APP", Title: "Panel review", Status: "backlog", Readiness: "waiting_on_human", Risk: "medium", Priority: "p1"})
	writeServeAcceptanceRows(t, server.vaultPath, "APP-T-0010")
	writeServeHumanVerificationGate(t, server.vaultPath, "APP-G-0010", "APP-T-0010", "A1,A3")

	var task serveTaskDetail
	serveDecode(t, server, "/api/tasks/APP-T-0010", &task)
	assertEqual(t, "review", task.Status, "human-wait projected status")
	assertEqual(t, "backlog", task.RawStatus, "human-wait raw status")
	assertEqual(t, "waiting_on_human", task.Readiness, "human-wait projected readiness")
	if task.HumanAction == nil {
		t.Fatal("expected server-derived human action")
	}
	assertEqual(t, "manual-verification", task.HumanAction.Kind, "human action kind")
	assertEqual(t, "verification", task.HumanAction.RawKind, "human action raw kind")
	assertEqual(t, "APP-G-0010", task.HumanAction.GateID, "human action gate id")
	assertEqual(t, "Exercise the panel.", task.HumanAction.Action, "human action text")
	assertEqual(t, "Requires visual macOS interaction.", task.HumanAction.WhyAgentCannot, "human boundary")
	assertEqual(t, "The panel behavior is confirmed.", task.HumanAction.CompletionCondition, "completion condition")
	assertEqual(t, []string{"A1", "A3"}, task.HumanAction.Covers, "covered acceptance ids")
	if len(task.HumanAction.Acceptance) != 2 || task.HumanAction.Acceptance[0].ID != "A1" || task.HumanAction.Acceptance[1].ID != "A3" {
		t.Fatalf("expected only covered acceptance rows, got %#v", task.HumanAction.Acceptance)
	}
	var review []serveTaskCapsule
	serveDecode(t, server, "/api/review/batch", &review)
	if len(review) != 1 || review[0].ID != "APP-T-0010" || review[0].Status != "review" {
		t.Fatalf("review batch must use the human-wait projection, got %#v", review)
	}

	var refused serveActionResult
	servePost(t, server, "/api/gates/APP-G-0010/satisfy", `{"projectId":"app","taskId":"APP-T-0010"}`, &refused)
	if !refused.Refused || refused.Task == nil || refused.Task.HumanAction == nil {
		t.Fatalf("refused completion must keep human action readback visible, got %#v", refused)
	}

	var completed serveActionResult
	servePost(t, server, "/api/gates/APP-G-0010/satisfy", `{"projectId":"app","taskId":"APP-T-0010","evidence":"Panel behavior confirmed."}`, &completed)
	if !completed.OK || completed.Refused || completed.Task == nil {
		t.Fatalf("expected successful human action completion readback, got %#v", completed)
	}
	if completed.Task.HumanAction != nil {
		t.Fatalf("human action must disappear after gate completion, got %#v", completed.Task.HumanAction)
	}
	assertEqual(t, "satisfied", completed.Gate.Status, "completed gate status")
}

func TestServeHumanActionKindMappingAndReworkProjection(t *testing.T) {
	cases := map[string]string{
		"verification": "manual-verification",
		"decision":     "decision",
		"signoff":      "signoff",
		"release":      "release",
		"provision":    "provision",
		"auth":         "provision",
		"env":          "provision",
		"unknown_kind": "human-action",
	}
	for raw, want := range cases {
		if got := serveHumanActionKind(raw); got != want {
			t.Fatalf("serveHumanActionKind(%q)=%q, want %q", raw, got, want)
		}
	}

	server := newServeEmptyNeedsFixture(t)
	writeServeTask(t, server.vaultPath, serveTaskSeed{ID: "APP-T-0011", Epic: "APP", Title: "Returned panel review", Status: "backlog", Readiness: "waiting_on_human", Risk: "medium", Priority: "p1"})
	writeServeHumanVerificationGate(t, server.vaultPath, "APP-G-0011", "APP-T-0011", "A1")

	var result serveActionResult
	servePost(t, server, "/api/tasks/APP-T-0011/status", `{"projectId":"app","status":"rework","reason":"Fix the panel spacing."}`, &result)
	if !result.OK || result.Refused || result.Task == nil {
		t.Fatalf("expected rework readback, got %#v", result)
	}
	assertEqual(t, "rework", result.Task.RawStatus, "rework raw status")
	assertEqual(t, "ready", result.Task.Status, "rework operator status")
	if result.Task.HumanAction != nil {
		t.Fatalf("human action must not remain after rework, got %#v", result.Task.HumanAction)
	}
}

func TestServeRunsHonest(t *testing.T) {
	server := newServeEmptyNeedsFixture(t)
	now := time.Date(2026, 7, 6, 6, 11, 0, 0, time.UTC)
	server.now = func() time.Time { return now }
	if err := server.store.UpsertRun(RunStatus{ProjectID: "app", RecordID: "APP-T-LIVE", ItemID: "APP-T-LIVE", Runner: string(RunnerCodexAppServer), Lane: runLaneExecute, LeaseState: string(LeaseStateRunning), AttemptOutcome: string(AttemptOutcomeNone), AttemptCount: 1, LastHeartbeatAt: now.Add(-10 * time.Second).Format(time.RFC3339), StartedAt: now.Add(-2 * time.Minute).Format(time.RFC3339), UpdatedAt: now.Add(-10 * time.Second).Format(time.RFC3339), LastEventAt: now.Add(-10 * time.Second).Format(time.RFC3339)}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"APP-T-EMPTY-1", "APP-T-EMPTY-2", "APP-T-EMPTY-3"} {
		if err := server.store.UpsertRun(RunStatus{ProjectID: "app", RecordID: id, ItemID: id, Runner: string(RunnerCodexAppServer), Lane: runLaneExecute, LeaseState: string(LeaseStateUnclaimed), AttemptOutcome: string(AttemptOutcomeNone), AttemptCount: 0, UpdatedAt: now.Format(time.RFC3339)}); err != nil {
			t.Fatal(err)
		}
	}

	var runs []map[string]any
	serveDecode(t, server, "/api/runs", &runs)
	assertEqual(t, 1, len(runs), "default runs omit placeholders")
	assertEqual(t, "APP-T-LIVE", runs[0]["taskId"], "live task id")
	assertEqual(t, "running", runs[0]["outcome"], "live run outcome")

	var all []map[string]any
	serveDecode(t, server, "/api/runs?all=true", &all)
	assertEqual(t, 4, len(all), "all runs include placeholders")
	var unclaimed map[string]any
	for _, run := range all {
		if run["taskId"] == "APP-T-EMPTY-1" {
			unclaimed = run
			break
		}
	}
	if unclaimed == nil {
		t.Fatalf("expected explicit unclaimed row in all=true payload: %#v", all)
	}
	assertEqual(t, "unclaimed", unclaimed["leaseState"], "unclaimed lease label")
	assertEqual(t, "idle", unclaimed["outcome"], "unclaimed outcome")
}

func TestServeRunsOutcomeLabels(t *testing.T) {
	server := newServeEmptyNeedsFixture(t)
	now := time.Date(2026, 7, 6, 6, 11, 0, 0, time.UTC)
	server.now = func() time.Time { return now }
	if err := server.store.UpsertRun(RunStatus{ProjectID: "app", RecordID: "APP-T-STALE", ItemID: "APP-T-STALE", Runner: string(RunnerCodexAppServer), Lane: runLaneExecute, LeaseState: string(LeaseStateRunning), AttemptOutcome: string(AttemptOutcomeNone), AttemptCount: 1, LastHeartbeatAt: now.Add(-5 * time.Minute).Format(time.RFC3339), StartedAt: now.Add(-10 * time.Minute).Format(time.RFC3339), UpdatedAt: now.Add(-5 * time.Minute).Format(time.RFC3339), LastEventAt: now.Add(-5 * time.Minute).Format(time.RFC3339)}); err != nil {
		t.Fatal(err)
	}
	if err := server.store.UpsertRun(RunStatus{ProjectID: "app", RecordID: "APP-T-DONE", ItemID: "APP-T-DONE", Runner: string(RunnerCodexAppServer), Lane: runLaneExecute, LeaseState: string(LeaseStateReleased), AttemptOutcome: string(AttemptOutcomeAbandoned), AttemptCount: 7, Terminal: true, UpdatedAt: now.Format(time.RFC3339)}); err != nil {
		t.Fatal(err)
	}

	var runs []map[string]any
	serveDecode(t, server, "/api/runs?all=true", &runs)
	byID := map[string]map[string]any{}
	for _, run := range runs {
		byID[run["taskId"].(string)] = run
	}
	assertEqual(t, "stale", byID["APP-T-STALE"]["outcome"], "stale held lease outcome")
	assertEqual(t, "held", byID["APP-T-STALE"]["leaseState"], "stale held lease state")
	assertEqual(t, "terminal", byID["APP-T-DONE"]["outcome"], "abandoned terminal outcome")
}

func TestServeRunDetailUsesCanonicalCompletedRunRow(t *testing.T) {
	server := newServeEmptyNeedsFixture(t)
	now := time.Date(2026, 7, 6, 6, 30, 0, 0, time.UTC)
	server.now = func() time.Time { return now }
	started := "2026-07-06T06:00:00Z"
	released := "2026-07-06T06:14:46Z"
	recent := now.Add(-6 * time.Second).Format(time.RFC3339)

	if err := server.store.UpsertRun(RunStatus{ProjectID: "app", RecordID: "APP-T-0001#worker", ItemID: "APP-T-0001", Runner: string(RunnerCodexAppServer), Lane: runLaneExecute, LeaseState: string(LeaseStateRunning), AttemptOutcome: string(AttemptOutcomeNone), AttemptCount: 1, LastHeartbeatAt: recent, StartedAt: started, UpdatedAt: recent, LastEventAt: recent}); err != nil {
		t.Fatal(err)
	}
	if err := server.store.UpsertRun(RunStatus{ProjectID: "app", RecordID: "APP-T-0001", ItemID: "APP-T-0001", Runner: string(RunnerCodexAppServer), Lane: runLaneExecute, LeaseState: string(LeaseStateUnclaimed), AttemptOutcome: string(AttemptOutcomeSucceeded), ActiveAttemptID: "attempt-done", AttemptCount: 1, WorkspacePath: "/tmp/app", StartedAt: started, UpdatedAt: released, LastEventAt: released, Terminal: true}); err != nil {
		t.Fatal(err)
	}
	if err := server.store.SaveAttempt(RunAttempt{AttemptID: "attempt-done", ProjectID: "app", RecordID: "APP-T-0001", ItemID: "APP-T-0001", Runner: string(RunnerCodexAppServer), Lane: runLaneExecute, Outcome: string(AttemptOutcomeSucceeded), StartedAt: started, FinishedAt: released}); err != nil {
		t.Fatal(err)
	}
	if err := server.store.SaveRunAuthorization(RunAuthorization{ProjectID: "app", RecordID: "APP-T-0001", LeaseGeneration: 1, Source: "tusker_cli", Actor: "operator", Trigger: "pickup", CreatedAt: started}); err != nil {
		t.Fatal(err)
	}
	if err := server.store.SaveRunIdentity(RunIdentityMetadata{ProjectID: "app", RecordID: "APP-T-0001", RepoRoot: "/repo/app", WorkspacePath: "/tmp/app", WorkspaceMode: "shared", Runner: string(RunnerCodexAppServer)}); err != nil {
		t.Fatal(err)
	}
	if err := server.store.SaveSession(RunnerSession{ProjectID: "app", RecordID: "APP-T-0001", Runner: string(RunnerCodexAppServer), SessionRef: "session-1", Resumable: true, State: "open", StartedAt: started, LastSeenAt: released}); err != nil {
		t.Fatal(err)
	}
	if err := server.store.SaveTurn(RunTurn{AttemptID: "attempt-done", ProjectID: "app", RecordID: "APP-T-0001", TurnID: "turn-1", TurnIndex: 0, Status: "completed", InputTokens: 120, OutputTokens: 34, TotalTokens: 154, StartedAt: started, CompletedAt: released, LastEventAt: released}); err != nil {
		t.Fatal(err)
	}
	if err := server.store.SaveTurn(RunTurn{AttemptID: "attempt-done", ProjectID: "app", RecordID: "APP-T-0001", TurnID: "turn-2", TurnIndex: 1, Status: "completed", InputTokens: 30, OutputTokens: 6, TotalTokens: 36, StartedAt: started, CompletedAt: released, LastEventAt: released}); err != nil {
		t.Fatal(err)
	}

	var detail serveRunDetail
	serveDecode(t, server, "/api/runs/APP-T-0001", &detail)
	assertEqual(t, "APP-T-0001", detail.TaskID, "detail task id")
	assertEqual(t, "succeeded", detail.Outcome, "completed detail outcome")
	assertEqual(t, "unclaimed", detail.LeaseState, "completed detail lease")
	assertEqual(t, 886, detail.ElapsedSec, "completed detail elapsed freezes at release")
	assertEqual(t, 914, detail.SinceLastEventSec, "detail did not use newer child liveness")
	assertEqual(t, 1, len(detail.Attempts), "detail attempts")
	assertEqual(t, "succeeded", detail.Attempts[0].Outcome, "attempt outcome")
	assertEqual(t, 886, detail.Attempts[0].DurationSec, "attempt elapsed freezes at finish")
	assertEqual(t, "tusker_cli", detail.Authorization.Source, "authorization source")
	assertEqual(t, "/repo/app", detail.Identity.RepoRoot, "registered repository")
	assertEqual(t, true, detail.Resume.Supported, "codex resume supported")
	assertEqual(t, "codex exec resume 'session-1'", detail.Resume.Command, "copyable resume command")
	assertEqual(t, "pending", detail.Delivery.ProofStatus, "delivery proof status")
	turns, err := server.store.ListTurnsForAttempt("attempt-done")
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, 2, len(turns), "raw usage rows remain available for forensic use")
}

func TestServeReadParityEndpointsSerialize(t *testing.T) {
	server := newServeFixture(t)
	writeServeEvidence(t, server.vaultPath)
	writeServeDecision(t, server.vaultPath)
	writeServeFeedback(t, server.vaultPath)
	if err := server.store.SaveAttempt(RunAttempt{AttemptID: "attempt-1", ProjectID: "app", RecordID: "APP-T-0007", ItemID: "APP-T-0007", Runner: string(RunnerCodexAppServer), Lane: runLaneExecute, Outcome: string(AttemptOutcomeFailed), StartedAt: "2026-07-06T06:00:00Z", FinishedAt: "2026-07-06T06:05:00Z", WorkspacePath: "/tmp/app", LastError: "boom"}); err != nil {
		t.Fatal(err)
	}
	if err := server.store.SaveTurn(RunTurn{AttemptID: "attempt-1", ProjectID: "app", RecordID: "APP-T-0007", TurnID: "turn-1", TurnIndex: 0, Status: "completed", InputTokens: 10, OutputTokens: 4, StartedAt: "2026-07-06T06:00:00Z", CompletedAt: "2026-07-06T06:05:00Z"}); err != nil {
		t.Fatal(err)
	}

	var gates []serveGateDetail
	serveDecode(t, server, "/api/gates?task=APP-T-0003", &gates)
	assertEqual(t, 1, len(gates), "gate list count")
	assertEqual(t, "APP-G-0001", gates[0].ID, "gate id")
	assertEqual(t, []string{"APP-T-0003"}, gates[0].Blocks, "gate blocks")

	var gate serveGateDetail
	serveDecode(t, server, "/api/gates/APP-G-0001", &gate)
	assertEqual(t, "open", gate.Status, "gate detail status")

	var evidence []serveEvidenceDoc
	serveDecode(t, server, "/api/evidence?task=APP-T-0001", &evidence)
	assertEqual(t, 1, len(evidence), "evidence list count")
	assertEqual(t, "accepted", evidence[0].Status, "evidence status")

	var evidenceDoc serveEvidenceDoc
	serveDecode(t, server, "/api/evidence/APP-T-0001-E-0001", &evidenceDoc)
	assertEqual(t, "Focused proof passed.", evidenceDoc.Summary, "evidence summary")

	var decisions []serveDecisionDoc
	serveDecode(t, server, "/api/decisions?epic=APP", &decisions)
	assertEqual(t, 1, len(decisions), "decision count")
	assertEqual(t, "Use serve parity.", decisions[0].Decision, "decision text")

	var feedback []serveFeedbackDoc
	serveDecode(t, server, "/api/feedback", &feedback)
	assertEqual(t, 1, len(feedback), "feedback count")
	assertEqual(t, "Serve lacks controls.", feedback[0].Friction, "feedback friction")

	var attempts []serveAttemptDetail
	serveDecode(t, server, "/api/attempts?task=APP-T-0007", &attempts)
	assertEqual(t, 1, len(attempts), "attempt count")
	assertEqual(t, "attempt-1", attempts[0].ID, "attempt id")

	var attempt serveAttemptDetail
	serveDecode(t, server, "/api/attempts/attempt-1", &attempt)
	assertEqual(t, "failed", attempt.Outcome, "attempt outcome")

	var daemon serveDaemonStatus
	serveDecode(t, server, "/api/daemon", &daemon)
	assertEqual(t, 2, daemon.MaxActiveRuns, "daemon active limit default")
}

func TestServeMutationRejectsCrossOrigin(t *testing.T) {
	server := newServeFixture(t)
	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:7420/api/tasks/APP-T-0001/status", bytes.NewBufferString(`{"status":"active"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://attacker.example")
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	assertEqual(t, http.StatusForbidden, rec.Code, "cross-origin mutation status")
	if !strings.Contains(rec.Body.String(), "refused cross-origin mutation") {
		t.Fatalf("expected cross-origin refusal, got %q", rec.Body.String())
	}
}

func TestServeMutationRejectsNonLoopbackHost(t *testing.T) {
	server := newServeFixture(t)
	req := httptest.NewRequest(http.MethodPost, "http://evil.example/api/tasks/APP-T-0001/status", bytes.NewBufferString(`{"status":"active"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	assertEqual(t, http.StatusForbidden, rec.Code, "non-loopback host mutation status")
	if !strings.Contains(rec.Body.String(), "non-loopback Host") {
		t.Fatalf("expected host refusal, got %q", rec.Body.String())
	}
}

func TestServeMutationEndpointsReturnVisibleRefusals(t *testing.T) {
	server := newServeFixture(t)
	cases := []struct {
		name       string
		path       string
		body       string
		wantReason string
	}{
		{name: "task status", path: "/api/tasks/APP-T-0001/status", body: `{"status":"done"}`, wantReason: "status cannot set done directly"},
		{name: "task close", path: "/api/tasks/APP-T-0002/close", body: `{}`, wantReason: "close blocked by placeholder acceptance"},
		{name: "task land", path: "/api/tasks/APP-T-0001/land", body: `{}`, wantReason: "is not in a wave"},
		{name: "wave land", path: "/api/waves/W-0001/land", body: `{}`, wantReason: "V7 wave not found"},
		{name: "gate satisfy", path: "/api/gates/APP-G-0001/satisfy", body: `{}`, wantReason: "satisfy requires --evidence"},
		{name: "gate waive", path: "/api/gates/APP-G-0001/waive", body: `{}`, wantReason: "waive requires --reason"},
		{name: "gate obsolete", path: "/api/gates/APP-G-0001/obsolete", body: `{}`, wantReason: "obsolete requires --reason"},
		{name: "evidence add", path: "/api/evidence", body: `{}`, wantReason: "Missing required argument --id"},
		{name: "feedback add", path: "/api/feedback", body: `{}`, wantReason: "feedback note missing required fields"},
		{name: "daemon limits", path: "/api/daemon/limits", body: `{"maxActiveRuns":0}`, wantReason: "--max-active-runs must be > 0"},
		{name: "daemon start malformed", path: "/api/daemon/start", body: `{`, wantReason: "invalid JSON body"},
		{name: "daemon stop malformed", path: "/api/daemon/stop", body: `{`, wantReason: "invalid JSON body"},
		{name: "daemon resume malformed", path: "/api/daemon/resume", body: `{`, wantReason: "invalid JSON body"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var result serveActionResult
			servePost(t, server, tc.path, tc.body, &result)
			if !result.Refused || result.OK {
				t.Fatalf("expected visible refusal, got %#v", result)
			}
			if !strings.Contains(result.Reason, tc.wantReason) {
				t.Fatalf("expected reason containing %q, got %#v", tc.wantReason, result)
			}
		})
	}
}

func TestServeCloseDefaultsReviewerActorForHighRisk(t *testing.T) {
	server := newServeEmptyNeedsFixture(t)
	writeServeCloseableReviewTask(t, server.vaultPath, "APP-T-0001", "high")

	var result serveActionResult
	servePost(t, server, "/api/tasks/APP-T-0001/close", `{}`, &result)
	if !result.OK || result.Refused {
		t.Fatalf("expected serve close to accept high-risk task as reviewer, got %#v", result)
	}

	data, _, err := parseFrontmatterMustRead(filepath.Join(server.vaultPath, "work", "tasks", "APP-T-0001.md"))
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "done", stringField(data, "status"), "closed task status")
	assertEqual(t, "reviewer:agent", stringField(data, "accepted_by"), "serve close default acceptor")
}

func newServeFixture(t *testing.T) *serveServer {
	t.Helper()
	root := t.TempDir()
	vault := filepath.Join(root, ".tusker")
	stateRoot := filepath.Join(root, "state")
	t.Setenv("TUSKER_STATE_ROOT", stateRoot)
	for _, dir := range []string{
		filepath.Join(vault, "work", "tasks"),
		filepath.Join(vault, "work", "epics"),
		filepath.Join(vault, "work", "gates"),
		filepath.Join(vault, "work", "decisions"),
		filepath.Join(vault, "evidence", "APP-T-0001"),
		filepath.Join(vault, "feedback", "agents"),
	} {
		if err := ensureDir(dir); err != nil {
			t.Fatal(err)
		}
	}
	if err := writeText(filepath.Join(root, "tusker.yaml"), "schema: tusker.config/v1\nproject_id: app\nstorage:\n  root: .tusker\nruntime:\n  mutation_mode: single_user_local\n"); err != nil {
		t.Fatal(err)
	}
	if err := writeText(workflowPath(vault), defaultWorkflowMarkdown()); err != nil {
		t.Fatal(err)
	}
	writeServeEpic(t, vault, "APP", "App")
	writeServeEpic(t, vault, "TRC", "Trace")
	writeServeTask(t, vault, serveTaskSeed{ID: "APP-T-0001", Epic: "APP", Title: "Critical review", Status: "review", Risk: "critical", Priority: "p0"})
	writeServeTask(t, vault, serveTaskSeed{ID: "APP-T-0002", Epic: "APP", Title: "Low review", Status: "review", Risk: "low", Priority: "p2"})
	writeServeTask(t, vault, serveTaskSeed{ID: "APP-T-0003", Epic: "APP", Title: "Human gate", Status: "ready", Risk: "medium", Priority: "p1"})
	writeServeTask(t, vault, serveTaskSeed{ID: "APP-T-0004", Epic: "APP", Title: "Rework loop", Status: "ready", Risk: "medium", Priority: "p1", ReworkTransitions: 2})
	writeServeTask(t, vault, serveTaskSeed{ID: "APP-T-0005", Epic: "APP", Title: "Dependent", Status: "ready", Risk: "medium", Priority: "p2", Dependencies: []string{"APP-T-0001"}})
	writeServeTask(t, vault, serveTaskSeed{ID: "APP-T-0006", Epic: "APP", Title: "Dependency blocked", Status: "ready", Readiness: "blocked_dependency", Risk: "medium", Priority: "p2", Dependencies: []string{"APP-T-0005"}})
	writeServeTask(t, vault, serveTaskSeed{ID: "APP-T-0007", Epic: "APP", Title: "Terminal failure", Status: "ready", Risk: "high", Priority: "p1"})
	writeServeTask(t, vault, serveTaskSeed{ID: "APP-T-0008", Epic: "TRC", Title: "Retrying failure", Status: "ready", Risk: "high", Priority: "p1"})
	writeServeTask(t, vault, serveTaskSeed{ID: "APP-T-0009", Epic: "TRC", Title: "Parked loop", Status: "ready", Risk: "high", Priority: "p1"})
	writeServeGate(t, vault)

	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	project := RegisteredProject{ProjectID: "app", ProjectKey: "app", Name: "app", RepoRoot: root, VaultRoot: vault, WorkflowPath: workflowPath(vault), Enabled: true, Health: projectHealthHealthy, LastPollAt: "2026-07-06T07:00:00Z"}
	if err := store.UpsertProject(project); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertRun(RunStatus{ProjectID: "app", RecordID: "APP-T-0007", ItemID: "APP-T-0007", Runner: string(RunnerCodexAppServer), Lane: runLaneExecute, LeaseState: string(LeaseStateReleased), AttemptOutcome: string(AttemptOutcomeFailed), AttemptCount: 3, LastError: "boom", StartedAt: "2026-07-06T06:00:00Z", UpdatedAt: "2026-07-06T06:10:00Z", LastEventAt: "2026-07-06T06:10:00Z"}); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertRun(RunStatus{ProjectID: "app", RecordID: "APP-T-0008", ItemID: "APP-T-0008", Runner: string(RunnerCodexAppServer), Lane: runLaneExecute, LeaseState: string(LeaseStateRetryQueued), AttemptOutcome: string(AttemptOutcomeFailed), AttemptCount: 1, LastError: "retry me", NextRetryAt: "2026-07-06T06:20:00Z", StartedAt: "2026-07-06T06:00:00Z", UpdatedAt: "2026-07-06T06:10:00Z", LastEventAt: "2026-07-06T06:10:00Z"}); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertRun(RunStatus{ProjectID: "app", RecordID: "APP-T-0009", ItemID: "APP-T-0009", Runner: string(RunnerCodexAppServer), Lane: runLaneExecute, LeaseState: string(LeaseStateParkedNoProgress), AttemptOutcome: string(AttemptOutcomeBlocked), AttemptCount: 3, LastError: "continuation retry cap reached", StartedAt: "2026-07-06T06:00:00Z", UpdatedAt: "2026-07-06T06:10:00Z", LastEventAt: "2026-07-06T06:10:00Z", Terminal: true}); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertRun(RunStatus{ProjectID: "app", RecordID: "APP-T-0003", ItemID: "APP-T-0003", Runner: string(RunnerCodexAppServer), Lane: runLaneExecute, LeaseState: string(LeaseStateRunning), AttemptOutcome: string(AttemptOutcomeNone), AttemptCount: 1, WorkspacePath: "/tmp/app", StartedAt: "2026-07-06T06:00:00Z", UpdatedAt: "2026-07-06T06:10:00Z", LastEventAt: "2026-07-06T06:10:00Z"}); err != nil {
		t.Fatal(err)
	}
	assets := fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("<html>serve fixture</html>"), ModTime: time.Now()}}
	server := newServeServer(vault, root, defaultServeAddr, store, assets)
	server.now = func() time.Time {
		return time.Date(2026, 7, 6, 6, 11, 0, 0, time.UTC)
	}
	return server
}

func newServeEmptyNeedsFixture(t *testing.T) *serveServer {
	t.Helper()
	root := t.TempDir()
	vault := filepath.Join(root, ".tusker")
	stateRoot := filepath.Join(root, "state")
	t.Setenv("TUSKER_STATE_ROOT", stateRoot)
	for _, dir := range []string{
		filepath.Join(vault, "work", "tasks"),
		filepath.Join(vault, "work", "epics"),
		filepath.Join(vault, "work", "gates"),
	} {
		if err := ensureDir(dir); err != nil {
			t.Fatal(err)
		}
	}
	if err := writeText(filepath.Join(root, "tusker.yaml"), "schema: tusker.config/v1\nproject_id: app\nstorage:\n  root: .tusker\nruntime:\n  mutation_mode: single_user_local\n"); err != nil {
		t.Fatal(err)
	}
	if err := writeText(workflowPath(vault), defaultWorkflowMarkdown()); err != nil {
		t.Fatal(err)
	}
	writeServeEpic(t, vault, "APP", "App")
	writeServeTask(t, vault, serveTaskSeed{ID: "APP-T-0001", Epic: "APP", Title: "Quiet ready task", Status: "ready", Risk: "medium", Priority: "p2"})

	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	project := RegisteredProject{ProjectID: "app", ProjectKey: "app", Name: "app", RepoRoot: root, VaultRoot: vault, WorkflowPath: workflowPath(vault), Enabled: true, Health: projectHealthHealthy, LastPollAt: "2026-07-06T07:00:00Z"}
	if err := store.UpsertProject(project); err != nil {
		t.Fatal(err)
	}
	assets := fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("<html>serve fixture</html>"), ModTime: time.Now()}}
	server := newServeServer(vault, root, defaultServeAddr, store, assets)
	server.now = func() time.Time {
		return time.Date(2026, 7, 6, 6, 11, 0, 0, time.UTC)
	}
	return server
}

type serveTaskSeed struct {
	ID                string
	Epic              string
	Title             string
	Status            string
	Readiness         string
	Risk              string
	Priority          string
	Dependencies      []string
	ReworkTransitions int
}

func writeServeEpic(t *testing.T, vault, id, title string) {
	t.Helper()
	body := "---\nschema: \"tusker.epic/v7\"\nkind: \"epic\"\nid: \"" + id + "\"\nproject: \"app\"\ntitle: \"" + title + "\"\nstatus: \"ready\"\nupdated_at: \"2026-07-06T06:00:00Z\"\n---\n\n# " + id + " - " + title + "\n"
	if err := writeText(filepath.Join(vault, "work", "epics", id+".md"), body); err != nil {
		t.Fatal(err)
	}
}

func writeServeTask(t *testing.T, vault string, seed serveTaskSeed) {
	t.Helper()
	readiness := firstNonEmpty(seed.Readiness, "ready")
	var deps string
	if len(seed.Dependencies) > 0 {
		deps = "dependencies:\n"
		for _, dep := range seed.Dependencies {
			deps += "  - \"" + dep + "\"\n"
		}
	} else {
		deps = "dependencies: []\n"
	}
	transitions := "transitions: []\n"
	if seed.ReworkTransitions > 0 {
		transitions = "transitions:\n"
		for i := 0; i < seed.ReworkTransitions; i++ {
			transitions += fmt.Sprintf("  - at: \"2026-07-06T06:%02d:00Z\"\n    kind: \"status\"\n    from: \"review\"\n    to: \"rework\"\n    actor: \"reviewer:agent\"\n    reason: \"needs changes\"\n", i)
		}
	}
	body := "---\nschema: \"tusker.task/v7\"\nkind: \"task\"\nid: \"" + seed.ID + "\"\nproject: \"app\"\ntitle: \"" + seed.Title + "\"\nepic: \"" + seed.Epic + "\"\nstatus: \"" + seed.Status + "\"\nreadiness: \"" + readiness + "\"\npriority: \"" + seed.Priority + "\"\nrisk: \"" + seed.Risk + "\"\nsize: \"m\"\nproof_mode: \"inline\"\nproof_status: \"pending\"\nproof_required:\n  - \"focused_test\"\n" + deps + "gates: []\ncreated_at: \"2026-07-06T06:00:00Z\"\nupdated_at: \"2026-07-06T06:00:00Z\"\nnext_owner: \"agent\"\nnext_action: \"Execute.\"\n" + transitions + "---\n\n# " + seed.ID + " - " + seed.Title + "\n\n## Intent\n\nDo the work.\n\n## Acceptance\n\n| ID | Outcome | Proof |\n|---|---|---|\n| A1 | Works. | Inline verification |\n\n## Non-goals\n\n- None.\n\n## Verification\n\n| Covers | Check | Result | Notes |\n|---|---|---|---|\n| A1 | go test ./cmd/tusker -run TestServe -count=1 | pending | Focused. |\n\n## Evidence\n\nAccepted:\n- None.\n\nPending:\n- None.\n\n## Knowledge delta\n\nNone expected.\n"
	writeServeTaskDocument(t, filepath.Join(vault, "work", "tasks", seed.ID+".md"), body)
}

func writeServeCloseableReviewTask(t *testing.T, vault, id, risk string) {
	t.Helper()
	body := "---\nschema: \"tusker.task/v7\"\nkind: \"task\"\nid: \"" + id + "\"\nproject: \"app\"\ntitle: \"Closeable high-risk review\"\nepic: \"APP\"\nstatus: \"review\"\nreadiness: \"waiting_on_review\"\npriority: \"p1\"\nrisk: \"" + risk + "\"\nsize: \"m\"\nproof_mode: \"inline\"\nproof_status: \"satisfied\"\nproof_required:\n  - \"focused_test\"\ndependencies: []\ngates: []\ncreated_at: \"2026-07-06T06:00:00Z\"\nupdated_at: \"2026-07-06T06:00:00Z\"\nnext_owner: \"human:sarav\"\nnext_source: \"close_policy\"\nnext_ref: \"close_policy:human_acceptor\"\nnext_action: \"Accept, waive, or return rework for close_policy:human_acceptor.\"\n---\n\n# " + id + " - Closeable high-risk review\n\n## Intent\n\nExercise the serve review close path for human-required risk.\n\n## Acceptance\n\n| ID | Outcome | Proof |\n|---|---|---|\n| A1 | The serve review action records a human acceptor when close policy requires human signoff. | focused_test |\n\n## Non-goals\n\n- No runner or daemon behavior.\n\n## Verification\n\n| Covers | Check | Result | Notes |\n|---|---|---|---|\n| A1 | go test ./cmd/tusker -run TestServeCloseDefaultsHumanActorForHumanRequiredRisk -count=1 | pass | Serve close actor proof. |\n\n## Evidence\n\nAccepted:\n- None.\n\nPending:\n- None.\n\n## Knowledge delta\n\nNone expected.\n"
	writeServeTaskDocument(t, filepath.Join(vault, "work", "tasks", id+".md"), body)
}

func writeServeTaskDocument(t *testing.T, path, source string) {
	t.Helper()
	data, body, err := parseFrontmatter(source)
	if err != nil {
		t.Fatal(err)
	}
	data["state_rev"] = v7StateRev(data, body)
	content, err := serializeDocument(data, body, v7FrontmatterOrder["task"])
	if err != nil {
		t.Fatal(err)
	}
	if err := writeText(path, content); err != nil {
		t.Fatal(err)
	}
}

func writeServeGate(t *testing.T, vault string) {
	t.Helper()
	body := "---\nschema: \"tusker.gate/v1\"\nkind: \"gate\"\nid: \"APP-G-0001\"\nproject: \"app\"\ntitle: \"Provision credentials\"\ngate_kind: \"provision\"\nstatus: \"open\"\nowner: \"human:sarav\"\nblocking: true\nblocks:\n  - \"APP-T-0003\"\naction: \"Provision test credentials.\"\n---\n\n# APP-G-0001 - Provision credentials\n\n## Action\n\nProvision test credentials.\n"
	if err := writeText(filepath.Join(vault, "work", "gates", "APP-G-0001.md"), body); err != nil {
		t.Fatal(err)
	}
}

func writeServeHumanVerificationGate(t *testing.T, vault, gateID, taskID, covers string) {
	t.Helper()
	coverLines := ""
	for _, cover := range strings.Split(covers, ",") {
		cover = strings.TrimSpace(cover)
		if cover != "" {
			coverLines += "  - \"" + cover + "\"\n"
		}
	}
	body := "---\nschema: \"tusker.gate/v1\"\nkind: \"gate\"\nid: \"" + gateID + "\"\nproject: \"app\"\ntitle: \"Panel verification\"\ngate_kind: \"verification\"\nstatus: \"open\"\nowner: \"human:sarav\"\nblocking: true\nblocks:\n  - \"" + taskID + "\"\ncovers:\n" + coverLines + "why_agent_cannot: \"Requires visual macOS interaction.\"\naction: \"Exercise the panel.\"\nverification: \"The panel behavior is confirmed.\"\ncreated_at: \"2026-07-06T06:00:00Z\"\nupdated_at: \"2026-07-06T06:00:00Z\"\n---\n\n# " + gateID + " · Panel verification\n\n## Why agent cannot do this\n\nRequires visual macOS interaction.\n\n## Action\n\nExercise the panel.\n\n## Verification\n\nThe panel behavior is confirmed.\n"
	if err := writeText(filepath.Join(vault, "work", "gates", gateID+".md"), body); err != nil {
		t.Fatal(err)
	}
}

func writeServeAcceptanceRows(t *testing.T, vault, taskID string) {
	t.Helper()
	path := filepath.Join(vault, "work", "tasks", taskID+".md")
	text, err := readText(path)
	if err != nil {
		t.Fatal(err)
	}
	text = strings.Replace(text, "| A1 | Works. | Inline verification |", "| A1 | Panel opens. | Inline verification |\n| A2 | Panel fits. | Inline verification |\n| A3 | Panel closes. | Inline verification |", 1)
	writeServeTaskDocument(t, path, text)
}

func writeServeEvidence(t *testing.T, vault string) {
	t.Helper()
	body := "---\nschema: \"tusker.evidence/v1\"\nkind: \"evidence\"\nid: \"APP-T-0001-E-0001\"\nproject: \"app\"\ntask: \"APP-T-0001\"\nepic: \"APP\"\nevidence_kind: \"automated_test\"\nstatus: \"accepted\"\ncovers:\n  - \"A1\"\nartifact_paths:\n  - \"external:https://ci.example.test/run/1\"\ncreated_by: \"agent:codex\"\ncreated_at: \"2026-07-06T06:00:00Z\"\naccepted_by: \"agent:codex\"\naccepted_at: \"2026-07-06T06:00:00Z\"\n---\n\n# APP-T-0001-E-0001 - evidence\n\n## Summary\n\nFocused proof passed.\n"
	if err := writeText(filepath.Join(vault, "evidence", "APP-T-0001", "APP-T-0001-E-0001.md"), body); err != nil {
		t.Fatal(err)
	}
}

func writeServeDecision(t *testing.T, vault string) {
	t.Helper()
	body := "---\nschema: \"tusker.decision/v1\"\nkind: \"decision\"\nid: \"APP-D-0001\"\nproject: \"app\"\nepic: \"APP\"\ntitle: \"Serve parity\"\nstatus: \"accepted\"\ndecision: \"Use serve parity.\"\ndecided_by: \"human:sarav\"\ndecided_at: \"2026-07-06T06:00:00Z\"\n---\n\n# APP-D-0001 - Serve parity\n\n## Decision\n\nUse serve parity.\n\n## Work streams\n\n- SRV\n"
	if err := writeText(filepath.Join(vault, "work", "decisions", "APP-D-0001.md"), body); err != nil {
		t.Fatal(err)
	}
}

func writeServeFeedback(t *testing.T, vault string) {
	t.Helper()
	body := "# Agent Feedback\n\n- context: Serve parity audit.\n- friction: Serve lacks controls.\n- product-idea: Add write actions.\n- impact: Operators stop shelling out.\n- related: SRV-T-0017\n- theme: serve\n- priority-hint: p1\n- affected-command: tusker serve\n"
	if err := writeText(filepath.Join(vault, "feedback", "agents", "2026-07-06-codex-serve-controls.md"), body); err != nil {
		t.Fatal(err)
	}
}

func serveDecode(t *testing.T, server *serveServer, path string, out any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("%s returned %d: %s", path, rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
		t.Fatalf("decode %s: %v\n%s", path, err, rec.Body.String())
	}
}

func servePost(t *testing.T, server *serveServer, path, body string, out any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:7420"+path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("%s returned %d: %s", path, rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
		t.Fatalf("decode %s: %v\n%s", path, err, rec.Body.String())
	}
}
