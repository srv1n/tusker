package main

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestV7SoftDependencyUnblocks(t *testing.T) {
	vault := automationTestVault(t)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Soft dependency", "risk": "low", "priority": "p0", "v7": "true"}, newV7Task)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Dependent", "risk": "low", "priority": "p0", "dependencies": "APP-T-0001", "v7": "true"}, newV7Task)
	makeV7TaskDispatchableForTest(t, vault, "APP-T-0002")
	registerAutomationTestProject(t, vault)

	cases := []struct {
		name      string
		status    string
		proof     string
		wantReady bool
	}{
		{name: "backlog with satisfied proof stays blocked", status: "backlog", proof: "satisfied"},
		{name: "ready with satisfied proof stays blocked", status: "ready", proof: "satisfied"},
		{name: "rework with satisfied proof stays blocked", status: "rework", proof: "satisfied"},
		{name: "review with pending proof stays blocked", status: "review", proof: "pending"},
		{name: "review with partial proof stays blocked", status: "review", proof: "partial"},
		{name: "review with satisfied proof unblocks", status: "review", proof: "satisfied", wantReady: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setAutomationV7TaskFields(t, vault, "APP-T-0001", map[string]any{
				"status":       tc.status,
				"proof_status": tc.proof,
			})
			mustRunPickupTest(t, Args{"vault": vault, "quiet": "true"}, reconcileV7Cmd)
			dependent := mustTaskData(t, vault, "APP-T-0002")
			if tc.wantReady {
				assertEqual(t, "ready", stringField(dependent, "readiness"), "soft dependency readiness")
				output := captureStdout(t, func() {
					if err := automationExplainCmd(Args{"vault": vault, "id": "APP-T-0002", "json": "true"}); err != nil {
						t.Fatal(err)
					}
				})
				var payload struct {
					Explanation automationTaskExplanation `json:"explanation"`
				}
				if err := json.Unmarshal([]byte(output), &payload); err != nil {
					t.Fatal(err)
				}
				assertEqual(t, true, payload.Explanation.Dispatchable, "automation explain dispatchable")
				return
			}
			assertEqual(t, "blocked_by_dependency", stringField(dependent, "readiness"), "soft dependency readiness")
			assertEqual(t, "APP-T-0001", stringField(dependent, "next_ref"), "blocked dependency ref")
		})
	}

	setAutomationV7TaskFields(t, vault, "APP-T-0002", map[string]any{
		"dependencies": []string{"APP-T-0001:hard"},
		"readiness":    "ready",
		"next_owner":   "agent",
	})
	setAutomationV7TaskFields(t, vault, "APP-T-0001", map[string]any{
		"status":       "review",
		"proof_status": "satisfied",
	})
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true"}, reconcileV7Cmd)
	assertEqual(t, "blocked_by_dependency", stringField(mustTaskData(t, vault, "APP-T-0002"), "readiness"), "hard dependency blocks review proof")

	setAutomationV7TaskFields(t, vault, "APP-T-0001", map[string]any{
		"status":       "done",
		"readiness":    "done",
		"proof_status": "satisfied",
	})
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true"}, reconcileV7Cmd)
	assertEqual(t, "ready", stringField(mustTaskData(t, vault, "APP-T-0002"), "readiness"), "hard dependency unblocks on done")
}

func TestV7DependencyHardnessDefaults(t *testing.T) {
	idx := v7Index{Tasks: map[string]Note{
		"LOW-T-0001":  {Data: map[string]any{"id": "LOW-T-0001", "risk": "low"}},
		"MED-T-0001":  {Data: map[string]any{"id": "MED-T-0001", "risk": "medium"}},
		"HIGH-T-0001": {Data: map[string]any{"id": "HIGH-T-0001", "risk": "high"}},
		"CRIT-T-0001": {Data: map[string]any{"id": "CRIT-T-0001", "risk": "critical"}},
		"BAD-T-0001":  {Data: map[string]any{"id": "BAD-T-0001", "risk": ""}},
	}}
	task := Note{Data: map[string]any{"dependencies": []string{
		"LOW-T-0001",
		"MED-T-0001",
		"HIGH-T-0001",
		"CRIT-T-0001",
		"BAD-T-0001",
		"HIGH-T-0001:soft",
		"LOW-T-0001:hard",
	}}}

	edges := v7TaskDependencyEdges(task, idx)
	got := map[string]string{}
	for _, edge := range edges {
		key := edge.Raw
		if edge.ExplicitHardness {
			key += ":explicit"
		}
		got[key] = edge.Hardness
	}
	assertEqual(t, "soft", got["LOW-T-0001"], "low default")
	assertEqual(t, "soft", got["MED-T-0001"], "medium default")
	assertEqual(t, "hard", got["HIGH-T-0001"], "high default")
	assertEqual(t, "hard", got["CRIT-T-0001"], "critical default")
	assertEqual(t, "hard", got["BAD-T-0001"], "unknown default")
	assertEqual(t, "soft", got["HIGH-T-0001:soft:explicit"], "explicit soft overrides high")
	assertEqual(t, "hard", got["LOW-T-0001:hard:explicit"], "explicit hard overrides low")
}

func TestV7DependencyParseCompat(t *testing.T) {
	raw := []string{"APP-T-0001", "APP-T-0002:soft", "APP-T-0003:hard"}
	task := Note{Data: map[string]any{"dependencies": raw}}
	assertEqual(t, raw, normalizeList(task.Data["dependencies"]), "frontmatter raw dependency strings stay unchanged")

	edges := v7TaskDependencyEdges(task, v7Index{Tasks: map[string]Note{
		"APP-T-0001": {Data: map[string]any{"id": "APP-T-0001", "risk": "medium"}},
		"APP-T-0002": {Data: map[string]any{"id": "APP-T-0002", "risk": "high"}},
		"APP-T-0003": {Data: map[string]any{"id": "APP-T-0003", "risk": "low"}},
	}})
	assertEqual(t, "APP-T-0001", edges[0].ID, "plain dependency id")
	assertEqual(t, false, edges[0].ExplicitHardness, "plain dependency is implicit")
	assertEqual(t, "APP-T-0002", edges[1].ID, "soft dependency id")
	assertEqual(t, "soft", edges[1].Hardness, "soft suffix")
	assertEqual(t, "APP-T-0003", edges[2].ID, "hard dependency id")
	assertEqual(t, "hard", edges[2].Hardness, "hard suffix")
}

func TestV7CloseOrderEnforced(t *testing.T) {
	vault := pickupV7TestVault(t)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Dependency", "risk": "low", "priority": "p0", "v7": "true"}, newV7Task)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Dependent", "risk": "low", "priority": "p0", "dependencies": "APP-T-0001:soft", "v7": "true"}, newV7Task)
	makeV7TaskDispatchableForTest(t, vault, "APP-T-0002")
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "_pos1": "APP-T-0002", "covers": "A1", "check": "go test ./cmd/tusker -run TestV7CloseOrderEnforced -count=1", "result": "pass", "note": "Dependent proof passed."}, verifyV7AddCmd)
	setAutomationV7TaskFields(t, vault, "APP-T-0001", map[string]any{
		"status":       "review",
		"proof_status": "satisfied",
	})
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true"}, reconcileV7Cmd)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "id": "APP-T-0002", "status": "review", "by": "agent:codex"}, statusV7Cmd)

	err := closeV7Cmd(Args{"vault": vault, "quiet": "true", "id": "APP-T-0002", "by": "reviewer:agent"})
	if err == nil || !strings.Contains(err.Error(), "unfinished dependency APP-T-0001") {
		t.Fatalf("expected unfinished dependency close error naming APP-T-0001, got %v", err)
	}

	setAutomationV7TaskFields(t, vault, "APP-T-0001", map[string]any{
		"status":       "done",
		"readiness":    "done",
		"proof_status": "satisfied",
	})
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "id": "APP-T-0002", "by": "reviewer:agent"}, closeV7Cmd)
	assertEqual(t, "done", stringField(mustTaskData(t, vault, "APP-T-0002"), "status"), "dependent close after dependency done")
}

func TestV7SoftDependencyReworkCascade(t *testing.T) {
	vault := automationTestVault(t)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Dependency", "risk": "low", "priority": "p0", "v7": "true"}, newV7Task)
	for _, title := range []string{"Non-started dependent", "Running dependent"} {
		mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": title, "risk": "low", "priority": "p0", "dependencies": "APP-T-0001:soft", "v7": "true"}, newV7Task)
	}
	makeV7TaskDispatchableForTest(t, vault, "APP-T-0002")
	makeV7TaskDispatchableForTest(t, vault, "APP-T-0003")
	setAutomationV7TaskFields(t, vault, "APP-T-0001", map[string]any{
		"status":       "review",
		"proof_status": "satisfied",
	})
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true"}, reconcileV7Cmd)
	assertEqual(t, "ready", stringField(mustTaskData(t, vault, "APP-T-0002"), "readiness"), "non-started dependent initially unblocked")
	assertEqual(t, "ready", stringField(mustTaskData(t, vault, "APP-T-0003"), "readiness"), "running dependent initially unblocked")

	project := registerAutomationTestProject(t, vault)
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertRun(RunStatus{
		ProjectID:       project.ProjectID,
		RecordID:        "APP-T-0003",
		ItemID:          "APP-T-0003",
		Runner:          string(RunnerCodexAppServer),
		Lane:            runLaneExecute,
		LeaseState:      string(LeaseStateRunning),
		ActiveAttemptID: "APP-T-0003-A-0001",
		SessionRef:      "session-running-dependent",
		AttemptCount:    1,
		UpdatedAt:       "2026-07-07T00:00:00Z",
	}); err != nil {
		t.Fatal(err)
	}
	_ = store.Close()

	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "status": "rework", "by": "reviewer:agent", "reason": "Soft dependency rejected in review."}, statusV7Cmd)
	for _, taskID := range []string{"APP-T-0002", "APP-T-0003"} {
		data := mustTaskData(t, vault, taskID)
		assertEqual(t, "blocked_by_dependency", stringField(data, "readiness"), taskID+" readiness after dependency rework")
		assertEqual(t, "APP-T-0001", stringField(data, "next_ref"), taskID+" dependency ref after rework")
	}

	daemon, err := NewDaemon(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer daemon.Close()
	if err := daemon.PollOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	run := latestRunForRecord(t, daemon.store, project.ProjectID, "APP-T-0003")
	assertEqual(t, string(LeaseStateReleased), run.LeaseState, "running dependent released after eligibility evaporates")
	if !strings.Contains(run.LastError, "readiness is blocked_by_dependency") {
		t.Fatalf("expected dependency-blocked stop reason, got %#v", run)
	}

	packet := v7Packet(vault, mustV7Task(t, vault, "APP-T-0001"), mustIndex(t, vault), "reviewer")
	assertContainsIndexTest(t, packet, "## Soft dependency blast radius")
	assertContainsIndexTest(t, packet, "`APP-T-0002`")
	assertContainsIndexTest(t, packet, "`APP-T-0003`")
}

func mustTaskData(t *testing.T, vault, taskID string) map[string]any {
	t.Helper()
	data, _, err := parseFrontmatterMustRead(filepath.Join(vault, "work", "tasks", taskID+".md"))
	if err != nil {
		t.Fatal(err)
	}
	return data
}
