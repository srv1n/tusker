package main

import (
	"context"
	"errors"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

func serveProcessRow(t *testing.T, server *serveServer, id int64) (serveProcess, bool) {
	t.Helper()
	var list serveProcessList
	serveDecode(t, server, "/api/processes?project=app", &list)
	for _, p := range list.Processes {
		if p.ID == id {
			return p, true
		}
	}
	return serveProcess{}, false
}

// A1/A2: the operator sees a running check and an agent session with both
// clocks and owner links, and Stop ends the whole group with operator as the
// reason. A row whose identity no longer matches is refused and left alone.
func TestServeProcessStopKillsGroup(t *testing.T) {
	server := newServeEmptyNeedsFixture(t)
	store := server.store

	check := startJanitorTestGroup(t)
	checkRow := recordJanitorTestRow(t, store, "closeout:APP-T-0001", processKindCheck, check, "", currentProcessStarter())

	agent := startJanitorTestGroup(t)
	if err := store.UpsertRun(RunStatus{ProjectID: "app", RecordID: "APP-T-0001", ItemID: "APP-T-0001", ActiveAttemptID: "att-1", LeaseState: string(LeaseStateRunning), LastEventAt: time.Now().UTC().Format(time.RFC3339Nano)}); err != nil {
		t.Fatal(err)
	}
	agentRow := recordJanitorTestRow(t, store, "att-1", processKindAgentSession, agent, "", currentProcessStarter())

	got, ok := serveProcessRow(t, server, checkRow.ID)
	if !ok || got.State != "running" || !got.CanStop || got.PGID != check.pid || len(got.TaskIDs) != 1 || got.TaskIDs[0] != "APP-T-0001" || !strings.Contains(got.What, "Quiet ready task") {
		t.Fatalf("check row = %+v", got)
	}
	if got.StartedAt == "" || got.LastSignOfLife == "" {
		t.Fatalf("check row lacks its two clocks: %+v", got)
	}
	gotAgent, ok := serveProcessRow(t, server, agentRow.ID)
	if !ok || gotAgent.Kind != processKindAgentSession || gotAgent.ProjectID != "app" || gotAgent.AttemptID != "att-1" || gotAgent.State != "running" {
		t.Fatalf("agent row = %+v", gotAgent)
	}

	var stop serveActionResult
	servePost(t, server, "/api/processes/stop", `{"id":`+strconv.FormatInt(checkRow.ID, 10)+`,"projectId":"app"}`, &stop)
	if !stop.OK || stop.Refused {
		t.Fatalf("stop = %+v", stop)
	}
	if !check.stopped(3 * time.Second) {
		t.Fatal("stop left the check's process group running")
	}
	got, ok = serveProcessRow(t, server, checkRow.ID)
	if !ok || got.State != "stopped" || got.Rule != ruleOperator || !strings.Contains(got.Reason, "human:test-operator") || got.CanStop {
		t.Fatalf("stopped row = %+v", got)
	}
	if events := processEventsFor(t, store, checkRow.Owner); len(events) != 1 || events[0].Rule != ruleOperator {
		t.Fatalf("operator stop events = %+v", events)
	}

	// Identity mismatch: a recorded start time that is not the live process's.
	other := startJanitorTestGroup(t)
	stale := recordJanitorTestRow(t, store, "closeout:APP-T-0002", processKindCheck, other, "2000-01-01T00:00:00Z", currentProcessStarter())
	var refused serveActionResult
	servePost(t, server, "/api/processes/stop", `{"id":`+strconv.FormatInt(stale.ID, 10)+`}`, &refused)
	if refused.OK || !refused.Refused || refused.Issue == nil || refused.Issue.Code != "PROCESS_IDENTITY_UNCONFIRMED" {
		t.Fatalf("mismatched identity stop = %+v", refused)
	}
	if other.stopped(300 * time.Millisecond) {
		t.Fatal("a refused stop killed the process")
	}
	if row := inventoryRow(t, store, stale.Owner); row.EndedAt != "" {
		t.Fatalf("a refused stop ended the row: %+v", row)
	}
}

// A3: both silence limits are project settings, validated, and honored by the
// janitor (agent flag) and by owned checks (check limit).
func TestServeProcessSilenceSettings(t *testing.T) {
	server := newServeEmptyNeedsFixture(t)
	store := server.store
	projects, err := store.ListProjects()
	if err != nil || len(projects) != 1 {
		t.Fatalf("fixture project: %v %+v", err, projects)
	}
	vault := projects[0].VaultRoot

	if check, agent := processSilenceLimits(vault); check != defaultCheckSilenceLimit || agent != defaultAgentSilenceLimit {
		t.Fatalf("defaults = %s/%s", check, agent)
	}
	for _, body := range []string{`{"key":"processes.check_silence_minutes","value":15}`, `{"key":"processes.agent_silence_minutes","value":45}`} {
		var result serveActionResult
		servePost(t, server, "/api/projects/app/settings", body, &result)
		if !result.OK {
			t.Fatalf("setting %s: %+v", body, result)
		}
	}
	var bad serveActionResult
	servePost(t, server, "/api/projects/app/settings", `{"key":"processes.check_silence_minutes","value":0}`, &bad)
	if bad.OK || !bad.Refused {
		t.Fatalf("zero minutes accepted: %+v", bad)
	}
	if check, agent := processSilenceLimits(vault); check != 15*time.Minute || agent != 45*time.Minute {
		t.Fatalf("saved limits = %s/%s", check, agent)
	}

	// The daemon's janitor reads the agent flag per project: 30 minutes of
	// silence is under this project's 45 and must not be flagged.
	proc := startJanitorTestGroup(t)
	now := time.Now().UTC()
	quietSince := now.Add(-30 * time.Minute).Format(time.RFC3339Nano)
	if err := store.UpsertRun(RunStatus{ProjectID: "app", RecordID: "APP-T-0001", ItemID: "APP-T-0001", ActiveAttemptID: "att-quiet", LeaseState: string(LeaseStateRunning), LastEventAt: quietSince}); err != nil {
		t.Fatal(err)
	}
	row := recordJanitorTestRow(t, store, "att-quiet", processKindAgentSession, proc, "", currentProcessStarter())
	if _, err := store.exec(`UPDATE process_inventory SET started_at = ?, last_sign_of_life = ? WHERE id = ?`, quietSince, quietSince, row.ID); err != nil {
		t.Fatal(err)
	}
	if err := runProcessJanitorFor(store, projectAgentSilenceLimits(store), now); err != nil {
		t.Fatal(err)
	}
	for _, e := range processEventsFor(t, store, "att-quiet") {
		if e.Kind == "silent" {
			t.Fatalf("flagged silent under the project's 45-minute limit: %+v", e)
		}
	}
	if err := runProcessJanitor(store, 20*time.Minute, now); err != nil {
		t.Fatal(err)
	}
	flagged := false
	for _, e := range processEventsFor(t, store, "att-quiet") {
		flagged = flagged || e.Kind == "silent"
	}
	if !flagged {
		t.Fatal("control: a 20-minute limit did not flag 30 minutes of silence")
	}

	// A check run with no explicit limit takes the project's limit from ctx.
	ctx := withCheckSilenceLimit(context.Background(), 300*time.Millisecond)
	started := time.Now()
	_, err = runOwnedCheck(ctx, "closeout:silence", exec.Command("sleep", "20"), 0)
	if !errors.Is(err, errCheckStuckStopped) || time.Since(started) > 10*time.Second {
		t.Fatalf("check silence from ctx: err=%v after %s", err, time.Since(started))
	}
}
