package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const departurePlannerTestSourceSHA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

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

func TestDeparturePlannerImplicitSingletonPublishesAtomically(t *testing.T) {
	vault := departurePlannerTestVault(t)
	writeDepartureTestTask(t, vault, "APP-T-0001", "W-0001")
	writeDepartureTestWave(t, vault, "W-0001", "disarmed", "")
	markDepartureTestWaveImplicitSingleton(t, vault, "W-0001", "APP-T-0001")
	wf, err := loadWorkflow(vault)
	if err != nil {
		t.Fatal(err)
	}
	wf.Data.ScheduledPromotion.Effective = scheduledPromotionProjection(ScheduledPromotionPolicy{Mode: scheduledPromotionShadow}, true, "test")
	planner := departurePlannerTestPlanner(false, false)

	decision, err := planner.PlanDeparture(vault, "project", wf)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Disposition != "ready" ||
		!sameDepartureStrings(decision.Candidate.WaveIDs, []string{"W-0001"}) ||
		!sameDepartureStrings(decision.Candidate.CargoTaskIDs, []string{"APP-T-0001"}) ||
		len(decision.Candidate.TaskSourceSHAs) != 1 ||
		decision.Candidate.TaskSourceSHAs["APP-T-0001"] != departurePlannerTestSourceSHA {
		t.Fatalf("valid implicit singleton did not publish exactly one atomic candidate: %#v", decision)
	}

	setDepartureTaskSourceForTest(t, vault, "APP-T-0001", strings.Repeat("b", 40))
	blocked, err := planner.PlanDeparture(vault, "project", wf)
	if err != nil {
		t.Fatal(err)
	}
	if blocked.Disposition != "blocked" ||
		len(blocked.Reasons) == 0 ||
		blocked.Reasons[0].Code != "wave_member_source_unavailable" ||
		len(blocked.Candidate.WaveIDs) != 0 ||
		len(blocked.Candidate.CargoTaskIDs) != 0 ||
		len(blocked.Candidate.TaskSourceSHAs) != 0 {
		t.Fatalf("invalid implicit singleton leaked a partial candidate: %#v", blocked)
	}
}

func TestDeparturePlannerDiscoversCompletedWaveFromExactLandingAudits(t *testing.T) {
	fixture := newMultiMemberDepartureExecutionFixture(t)
	idx, err := loadV7Index(fixture.vault)
	if err != nil {
		t.Fatal(err)
	}
	sourceOne := gitRevisionForTest(t, fixture.repo, "task/APP-T-0001")
	sourceTwo := stringField(idx.Tasks["APP-T-0002"].Data, "source_sha")
	if sourceTwo == "" {
		t.Fatal("fixture is missing APP-T-0002 exact source")
	}
	if err := landFrozenSourcesAsIssuedDeparture(t, fixture.repo, fixture.vault,
		Args{"vault": fixture.vault, "quiet": "true", "actor": "daemon:departure:fixture-two", "_pos0": "APP-T-0002"},
		map[string]string{"APP-T-0002": sourceTwo},
	); err != nil {
		t.Fatal(err)
	}
	clearDepartureTaskSourceForTest(t, fixture.vault, "APP-T-0002")
	armScheduledPromotionWaveForTest(t, fixture.vault, "W-0001")

	decision, err := fixture.plan(fixture.project, fixture.wf)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Disposition != "ready" ||
		!sameDepartureStrings(decision.Candidate.WaveIDs, []string{"W-0001"}) ||
		!sameDepartureStrings(decision.Candidate.CargoTaskIDs, []string{"APP-T-0001", "APP-T-0002"}) ||
		decision.Candidate.TaskSourceSHAs["APP-T-0001"] != sourceOne ||
		decision.Candidate.TaskSourceSHAs["APP-T-0002"] != sourceTwo ||
		!sameDepartureStrings(decision.ResourceNeeds, []string{"gate:full"}) {
		t.Fatalf("exact-audit-only completed wave was not discoverable: %#v", decision)
	}
	for _, task := range decision.Tasks {
		if task.EligibleForCargo {
			t.Fatalf("test did not exercise audit-only wave discovery: %#v", task)
		}
	}
}

func TestDeparturePlannerRejectsForgedLandingAuditProvenance(t *testing.T) {
	fixture := newMultiMemberDepartureExecutionFixture(t)
	wavePath := filepath.Join(fixture.vault, "work", "waves", "W-0001.md")
	data, body, err := parseFrontmatterMustRead(wavePath)
	if err != nil {
		t.Fatal(err)
	}
	var historical map[string]any
	for _, row := range normalizeLandingAudit(data["landings"]) {
		if stringField(row, "task") == "APP-T-0001" {
			historical = row
			break
		}
	}
	if historical == nil || stringField(historical, "receipt_fingerprint") == "" {
		t.Fatalf("fixture lacks a legitimate historical receipt-backed row: %#v", data["landings"])
	}
	forged := make(map[string]any, len(historical))
	for key, value := range historical {
		forged[key] = value
	}
	forged["task"] = "APP-T-0002"
	forged["branch"] = "task/APP-T-0002"
	baseRev := stringField(data, "state_rev")
	data["landings"] = append(normalizeLandingAudit(data["landings"]), forged)
	if _, err := saveV7DocumentCAS(wavePath, data, body, v7FrontmatterOrder["wave"], baseRev); err != nil {
		t.Fatal(err)
	}
	clearDepartureTaskSourceForTest(t, fixture.vault, "APP-T-0002")
	armScheduledPromotionWaveForTest(t, fixture.vault, "W-0001")

	decision, err := fixture.plan(fixture.project, fixture.wf)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Disposition != "empty" ||
		len(decision.Candidate.CargoTaskIDs) != 0 ||
		len(decision.Candidate.TaskSourceSHAs) != 0 {
		t.Fatalf("forged Markdown audit resurrected source-less cargo: %#v", decision)
	}
}

func TestLandingActorLabelCannotMintControlPlaneReceipt(t *testing.T) {
	repo, vault := newLandTestRepo(t, 1, "true")
	source := commitLandBranch(t, repo, "task/APP-T-0001", "integration/W-0001", map[string]string{"spoofed-actor.txt": "untrusted\n"})
	setDepartureTaskSourceForTest(t, vault, "APP-T-0001", source)
	if err := landV7Cmd(Args{
		"vault": vault, "quiet": "true", "actor": "daemon:departure:typed-by-a-user",
		"_pos0": "APP-T-0001",
	}); err != nil {
		t.Fatal(err)
	}
	idx, err := loadV7Index(vault)
	if err != nil {
		t.Fatal(err)
	}
	wave := idx.Waves["W-0001"]
	saw := false
	for _, row := range normalizeLandingAudit(wave.Data["landings"]) {
		if stringField(row, "task") != "APP-T-0001" {
			continue
		}
		saw = true
		if stringField(row, "control_authority") != "" ||
			stringField(row, "provenance") == v7LandingAuditProvenance {
			t.Fatalf("an actor label minted control-plane authority: %#v", row)
		}
	}
	if !saw {
		t.Fatal("spoofed actor fixture did not land")
	}
	if recovered, ok := authenticatedV7LandingAuditSource(repo, "integration/W-0001", wave, "APP-T-0001", gitRevParse); ok || recovered != "" {
		t.Fatalf("actor-label-only landing authenticated: source=%s ok=%v", recovered, ok)
	}
}

func TestDeparturePlannerLandingAuditReceiptFailsClosed(t *testing.T) {
	for _, tc := range []struct {
		name   string
		tamper func(*testing.T, string)
	}{
		{
			name: "missing receipt",
			tamper: func(t *testing.T, path string) {
				raw, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = os.WriteFile(path, raw, 0o600) })
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "mismatched final tree",
			tamper: func(t *testing.T, path string) {
				raw, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = os.WriteFile(path, raw, 0o600) })
				var record v7LandingGateCacheRecord
				if err := json.Unmarshal(raw, &record); err != nil || record.Receipt == nil {
					t.Fatalf("decode receipt: %v %#v", err, record)
				}
				record.Receipt.BatchTreeSHA = strings.Repeat("0", 40)
				changed, err := json.MarshalIndent(record, "", "  ")
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, append(changed, '\n'), 0o600); err != nil {
					t.Fatal(err)
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newMultiMemberDepartureExecutionFixture(t)
			wave, _, err := parseFrontmatterMustRead(filepath.Join(fixture.vault, "work", "waves", "W-0001.md"))
			if err != nil {
				t.Fatal(err)
			}
			fingerprint := ""
			for _, row := range normalizeLandingAudit(wave["landings"]) {
				if stringField(row, "task") == "APP-T-0001" {
					fingerprint = stringField(row, "receipt_fingerprint")
					break
				}
			}
			if fingerprint == "" {
				t.Fatal("fixture is missing APP-T-0001 receipt fingerprint")
			}
			tc.tamper(t, v7LandingGateCachePath(fixture.vault, fingerprint))

			decision, err := fixture.plan(fixture.project, fixture.wf)
			if err != nil {
				t.Fatal(err)
			}
			if decision.Disposition != "blocked" ||
				len(decision.Reasons) == 0 ||
				decision.Reasons[0].Code != "wave_member_source_unavailable" ||
				len(decision.Candidate.WaveIDs) != 0 ||
				len(decision.Candidate.CargoTaskIDs) != 0 ||
				len(decision.Candidate.TaskSourceSHAs) != 0 {
				t.Fatalf("unverifiable landing receipt resurrected source-less cargo: %#v", decision)
			}
		})
	}
}

func departurePlannerTestPlanner(noRemote, gateHit bool) departurePlanner {
	planner := defaultDeparturePlanner()
	planner.remote = func(string) (string, bool) { return "origin", !noRemote }
	planner.fetch = func(context.Context, string, string, string) error { return nil }
	planner.rev = func(_ string, ref string) (string, bool) {
		if strings.HasPrefix(ref, departurePlannerTestSourceSHA) {
			return departurePlannerTestSourceSHA, true
		}
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
	body := ""
	data := map[string]any{
		"schema":       "tusker.task/v7",
		"kind":         "task",
		"id":           id,
		"status":       "done",
		"readiness":    "done",
		"proof_status": "satisfied",
		"accepted_by":  "reviewer:planner",
		"accepted_at":  "2026-07-25T00:00:00Z",
		"closed_at":    "2026-07-25T00:00:00Z",
		"source_sha":   departurePlannerTestSourceSHA,
	}
	if wave != "" {
		data["wave"] = wave
	}
	data["state_rev"] = v7StateRev(data, body)
	content, err := serializeDocument(data, body, v7FrontmatterOrder["task"])
	if err != nil {
		t.Fatal(err)
	}
	if err := writeText(filepath.Join(vault, "work", "tasks", id+".md"), content); err != nil {
		t.Fatal(err)
	}
}

func writeDepartureTestWave(t *testing.T, vault, id, authorization, fingerprint string) {
	t.Helper()
	if err := ensureDir(filepath.Join(vault, "work", "waves")); err != nil {
		t.Fatal(err)
	}
	text := "---\nschema: tusker.wave/v7\nkind: wave\nid: " + id + "\nauthorization: " + authorization + "\n"
	text += "members:\n  - APP-T-0001\n"
	if fingerprint != "" {
		text += "authorization_fingerprint: " + fingerprint + "\n"
	}
	if err := writeText(filepath.Join(vault, "work", "waves", id+".md"), text+"---\n"); err != nil {
		t.Fatal(err)
	}
}

func markDepartureTestWaveImplicitSingleton(t *testing.T, vault, waveID, taskID string) {
	t.Helper()
	path := filepath.Join(vault, "work", "waves", waveID+".md")
	data, body, err := parseFrontmatterMustRead(path)
	if err != nil {
		t.Fatal(err)
	}
	baseRev := stringField(data, "state_rev")
	data["delivery_unit"] = v7ImplicitSingletonDeliveryUnit
	data["delivery_task"] = taskID
	data["release_authorized"] = false
	if _, err := saveV7DocumentCAS(path, data, body, v7FrontmatterOrder["wave"], baseRev); err != nil {
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
