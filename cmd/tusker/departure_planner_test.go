package main

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestDeparturePlannerDisabledHasNoFetch(t *testing.T) {
	wf := WorkflowFile{Data: defaultWorkflow()}
	fetched := false
	planner := defaultDeparturePlanner()
	planner.fetch = func(context.Context, string, string, string) error { fetched = true; return nil }
	decision, err := planner.PlanDeparture(t.TempDir(), "project", wf)
	if err != nil || fetched || decision.Disposition != "disabled" {
		t.Fatalf("disabled decision=%#v fetched=%v err=%v", decision, fetched, err)
	}
}

func TestDepartureCLIEmitsStableDisabledJSON(t *testing.T) {
	vault := departurePlannerTestVault(t)
	output := captureStdout(t, func() {
		if err := departureCheckCmd(Args{"vault": vault, "project": "project", "json": "true"}); err != nil {
			t.Fatal(err)
		}
	})
	var payload struct {
		OK       bool              `json:"ok"`
		Decision DepartureDecision `json:"decision"`
	}
	if err := json.Unmarshal([]byte(output), &payload); err != nil {
		t.Fatal(err)
	}
	if !payload.OK || payload.Decision.Schema != departurePlannerSchema || payload.Decision.Disposition != "disabled" || payload.Decision.Fetch.Attempted {
		t.Fatalf("payload=%s", output)
	}
	if !strings.Contains(captureStdout(t, func() { _ = departureCheckCmd(Args{"vault": vault, "project": "project"}) }), "Scheduled promotion is off") {
		t.Fatal("terminal output did not explain disabled mode")
	}
}

func TestDepartureCLIStatusAndBoundedHistoryJSON(t *testing.T) {
	stateRoot := t.TempDir()
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	for _, window := range []string{"2026-07-25T20:00:00Z", "2026-07-25T19:00:00Z", "2026-07-25T18:00:00Z"} {
		if _, _, err := store.GetOrCreateDepartureRun(DepartureRun{ProjectID: "project", PolicyID: "policy", ScheduledWindow: window, State: DepartureStatePassed}); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	status := captureStdout(t, func() {
		if err := departureStatusCmd(Args{"project": "project", "state-root": stateRoot, "json": "true"}); err != nil {
			t.Fatal(err)
		}
	})
	history := captureStdout(t, func() {
		if err := departureHistoryCmd(Args{"project": "project", "state-root": stateRoot, "limit": "2", "json": "true"}); err != nil {
			t.Fatal(err)
		}
	})
	var statusPayload struct {
		OK     bool `json:"ok"`
		Status struct {
			Count int `json:"count"`
		} `json:"status"`
	}
	var historyPayload struct {
		OK      bool           `json:"ok"`
		History []DepartureRun `json:"history"`
		Limit   int            `json:"limit"`
	}
	if err := json.Unmarshal([]byte(status), &statusPayload); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(history), &historyPayload); err != nil {
		t.Fatal(err)
	}
	if !statusPayload.OK || statusPayload.Status.Count != 3 || !historyPayload.OK || historyPayload.Limit != 2 || len(historyPayload.History) != 2 {
		t.Fatalf("status=%s history=%s", status, history)
	}
}

func TestDeparturePlannerFetchFailureIsIndeterminate(t *testing.T) {
	vault := departurePlannerTestVault(t)
	wf, err := loadWorkflow(vault)
	if err != nil {
		t.Fatal(err)
	}
	wf.Data.ScheduledPromotion.Effective = scheduledPromotionProjection(ScheduledPromotionPolicy{Mode: scheduledPromotionShadow}, true, "test")
	planner := defaultDeparturePlanner()
	planner.remote = func(string) (string, bool) { return "origin", true }
	planner.fetch = func(context.Context, string, string, string) error { return errors.New("offline") }
	decision, err := planner.PlanDeparture(vault, "project", wf)
	if err != nil || decision.Disposition != "indeterminate" || len(decision.Reasons) == 0 || decision.Reasons[0].Code != "remote_refresh_failed" {
		t.Fatalf("decision=%#v err=%v", decision, err)
	}
}

func TestDeparturePlannerBehaviorMatrix(t *testing.T) {
	for _, tc := range []struct {
		name, setup, want string
		gateHit           bool
		noRemote          bool
	}{
		{name: "empty", want: "empty"},
		{name: "cargo", setup: "task", want: "ready"},
		{name: "held", setup: "task+held", want: "blocked"},
		{name: "no remote", setup: "task", noRemote: true, want: "indeterminate"},
		{name: "stale authorization", setup: "stale", want: "blocked"},
		{name: "already gated", setup: "task", gateHit: true, want: "already_gated"},
		{name: "release ineligible", setup: "task", want: "ready"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			vault := departurePlannerTestVault(t)
			if strings.Contains(tc.setup, "task") {
				wave := ""
				if tc.setup == "task+held" {
					wave = "W-0001"
				}
				writeDepartureTestTask(t, vault, "APP-T-0001", wave)
			}
			if tc.setup == "task+held" {
				writeDepartureTestWave(t, vault, "W-0001", "paused", "")
			}
			if tc.setup == "stale" {
				writeDepartureTestTask(t, vault, "APP-T-0001", "W-0001")
				writeDepartureTestWave(t, vault, "W-0001", "armed", "sha256:stale")
			}
			wf, err := loadWorkflow(vault)
			if err != nil {
				t.Fatal(err)
			}
			wf.Data.ScheduledPromotion.Effective = scheduledPromotionProjection(ScheduledPromotionPolicy{Mode: scheduledPromotionShadow}, true, "test")
			wf.Data.Orchestration.Gate.HarvestCommands = []string{"go test ./..."}
			wf.Data.Orchestration.Gate.Profile = "full"
			planner := departurePlannerTestPlanner(tc.noRemote, tc.gateHit)
			decision, err := planner.PlanDeparture(vault, "project", wf)
			if err != nil || decision.Disposition != tc.want {
				t.Fatalf("decision=%#v err=%v", decision, err)
			}
			if decision.DefaultRef.Name != "main" || (!tc.noRemote && decision.DefaultRef.SHA != "main-sha") {
				t.Fatalf("default ref not pinned: %#v", decision.DefaultRef)
			}
			if len(decision.Waves) > 0 && (len(decision.IntegrationRefs) != 1 || decision.IntegrationRefs[0].Name != "integration/W-0001" || decision.IntegrationRefs[0].SHA != "integration-sha") {
				t.Fatalf("integration refs not pinned: %#v", decision.IntegrationRefs)
			}
			if tc.name == "release ineligible" && decision.ReleaseEligible {
				t.Fatalf("shadow mode must not imply release eligibility: %#v", decision)
			}
		})
	}
}

func TestDeparturePlannerIgnoresUnrelatedHeldAndStaleWaves(t *testing.T) {
	vault := departurePlannerTestVault(t)
	writeDepartureTestWave(t, vault, "W-0001", "paused", "")
	writeDepartureTestWave(t, vault, "W-0002", "armed", "sha256:stale")
	wf, err := loadWorkflow(vault)
	if err != nil {
		t.Fatal(err)
	}
	wf.Data.ScheduledPromotion.Effective = scheduledPromotionProjection(ScheduledPromotionPolicy{Mode: scheduledPromotionShadow}, true, "test")
	decision, err := departurePlannerTestPlanner(false, false).PlanDeparture(vault, "project", wf)
	if err != nil || decision.Disposition != "empty" {
		t.Fatalf("unrelated historical waves blocked an empty departure: %#v err=%v", decision, err)
	}
}

func departurePlannerTestPlanner(noRemote, gateHit bool) departurePlanner {
	planner := defaultDeparturePlanner()
	planner.remote = func(string) (string, bool) { return "origin", !noRemote }
	planner.fetch = func(context.Context, string, string, string) error { return nil }
	planner.rev = func(_ string, ref string) (string, bool) {
		if strings.Contains(ref, "main") {
			return "main-sha", true
		}
		if strings.HasPrefix(ref, "integration/") {
			return "integration-sha", true
		}
		return "", false
	}
	planner.gateLookup = func(string, string, []string, string, string) bool { return gateHit }
	return planner
}

func writeDepartureTestTask(t *testing.T, vault, id, wave string) {
	t.Helper()
	if err := ensureDir(filepath.Join(vault, "work", "tasks")); err != nil {
		t.Fatal(err)
	}
	text := "---\nschema: tusker.task/v7\nkind: task\nid: " + id + "\nstatus: done\nstate_rev: sha256:task\nsource_sha: source-sha\n"
	if wave != "" {
		text += "wave: " + wave + "\n"
	}
	if err := writeText(filepath.Join(vault, "work", "tasks", id+".md"), text+"---\n"); err != nil {
		t.Fatal(err)
	}
}

func writeDepartureTestWave(t *testing.T, vault, id, authorization, fingerprint string) {
	t.Helper()
	if err := ensureDir(filepath.Join(vault, "work", "waves")); err != nil {
		t.Fatal(err)
	}
	text := "---\nschema: tusker.wave/v7\nkind: wave\nid: " + id + "\nauthorization: " + authorization + "\n"
	if fingerprint != "" {
		text += "authorization_fingerprint: " + fingerprint + "\n"
	}
	if err := writeText(filepath.Join(vault, "work", "waves", id+".md"), text+"---\n"); err != nil {
		t.Fatal(err)
	}
}

func departurePlannerTestVault(t *testing.T) string {
	t.Helper()
	vault := t.TempDir()
	if err := writeDefaultWorkflow(vault); err != nil {
		t.Fatal(err)
	}
	return vault
}
