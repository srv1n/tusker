package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestScheduledPromotionMorningBriefDisabledAndEmptyFixtures(t *testing.T) {
	night := time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC)
	now := time.Date(2026, 7, 25, 7, 30, 0, 0, time.UTC)

	t.Run("disabled ignores stale runtime state", func(t *testing.T) {
		wf := Workflow{}
		wf.ScheduledPromotion.Effective = ScheduledPromotionProjection{
			Mode: scheduledPromotionDisabled, Provenance: "migration default (scheduled_promotion absent)",
		}
		// A nonexistent vault proves the feature-off projection returns before
		// reading tasks, events, runtime rows, or proof.
		brief, err := buildScheduledPromotionMorningBrief(filepath.Join(t.TempDir(), "missing"), "app", wf, nil, night, now)
		if err != nil {
			t.Fatal(err)
		}
		if brief.Feature.Enabled || len(brief.LandedLastNight) != 0 || len(brief.BlockedOrRepairing) != 0 || len(brief.NeedsYourDecision) != 0 {
			t.Fatalf("feature-off projection raised activity: %#v", brief)
		}
		if !strings.Contains(brief.Feature.Summary, "off for this project") ||
			!strings.Contains(brief.EmptyStates.BlockedOrRepairing, "off") {
			t.Fatalf("feature-off language is not explicit: %#v", brief)
		}
	})

	t.Run("enabled empty projection has exactly three primary arrays", func(t *testing.T) {
		projection := scheduledPromotionTestProjection(scheduledPromotionShadow, false)
		brief := composeScheduledPromotionMorningBrief(
			newScheduledPromotionMorningBrief("app", projection, night, now),
			scheduledPromotionMorningBriefFacts{Index: scheduledPromotionEmptyIndex()},
			night,
		)
		raw, err := json.Marshal(brief)
		if err != nil {
			t.Fatal(err)
		}
		var object map[string]json.RawMessage
		if err := json.Unmarshal(raw, &object); err != nil {
			t.Fatal(err)
		}
		arrayKeys := []string{}
		for key, value := range object {
			if len(value) > 0 && value[0] == '[' {
				arrayKeys = append(arrayKeys, key)
			}
		}
		sort.Strings(arrayKeys)
		want := []string{"blockedOrRepairing", "landedLastNight", "needsYourDecision"}
		if !reflect.DeepEqual(arrayKeys, want) {
			t.Fatalf("primary arrays = %#v, want %#v\n%s", arrayKeys, want, raw)
		}
		for _, encoded := range []string{`"landedLastNight":[]`, `"blockedOrRepairing":[]`, `"needsYourDecision":[]`} {
			if !strings.Contains(string(raw), encoded) {
				t.Fatalf("empty list did not encode as [] (%s): %s", encoded, raw)
			}
		}
		if brief.EmptyStates.LandedLastNight == "" || brief.EmptyStates.BlockedOrRepairing == "" || brief.EmptyStates.NeedsYourDecision == "" {
			t.Fatalf("empty-state language is incomplete: %#v", brief.EmptyStates)
		}
	})
}

func TestScheduledPromotionMorningBriefLandedOnlyFixture(t *testing.T) {
	night, now := scheduledPromotionTestTimes()
	idx, wave := briefFixture()
	waveProjection := buildWaveBrief(idx, wave)
	run := scheduledPromotionTestDeparture("departure-landed", "2026-07-24T23:15:00Z")
	run.State = DepartureStatePassed
	run.Candidate.TaskSourceSHAs = map[string]string{"APP-T-0001": "source-1"}
	run.Candidate.TaskStateRevisions = map[string]string{"APP-T-0001": "state-1"}
	run.Promotion = DeparturePromotion{
		CommittedRef: "refs/heads/main", CommittedSHA: "promoted-sha-1", CommittedAt: "2026-07-24T23:15:00Z",
	}
	facts := scheduledPromotionMorningBriefFacts{
		Index: idx,
		Logbook: tuskerLogbook{Shipped: []logbookShipped{{
			TaskID: "APP-T-0001", Outcome: "Customers can now see the final interaction.",
		}}},
		Departures: []DepartureRun{run}, WaveBriefs: []waveBrief{waveProjection},
	}
	brief := composeScheduledPromotionMorningBrief(
		newScheduledPromotionMorningBrief("app", scheduledPromotionTestProjection(scheduledPromotionPromote, false), night, now),
		facts, night,
	)
	if len(brief.LandedLastNight) != 1 || len(brief.BlockedOrRepairing) != 0 {
		t.Fatalf("landed-only outcome was not isolated: %#v", brief)
	}
	landed := brief.LandedLastNight[0]
	if landed.PromotedSHA != "promoted-sha-1" || landed.PromotedRef != "refs/heads/main" || landed.ReleasedRevision != "" {
		t.Fatalf("promotion/release facts collapsed: %#v", landed)
	}
	if len(landed.AcceptedArtifacts) != 1 || landed.AcceptedArtifacts[0].EvidenceRef != "APP-T-0001-E-0001" {
		t.Fatalf("accepted wave artifact was not composed: %#v", landed.AcceptedArtifacts)
	}
	if !strings.Contains(landed.Summary, "Customers can now see") {
		t.Fatalf("logbook outcome was not composed: %#v", landed)
	}
	rendered := renderScheduledPromotionMorningBrief(brief)
	if strings.Contains(rendered, "released revision") {
		t.Fatalf("promotion without release was described as a release:\n%s", rendered)
	}
}

func TestScheduledPromotionMorningBriefRedRepairingFixture(t *testing.T) {
	night, now := scheduledPromotionTestTimes()
	run := scheduledPromotionTestDeparture("departure-red", "2026-07-20T02:00:00Z")
	run.State = DepartureStateRepairing
	run.Gate.Status = "failed"
	run.Gate.ArtifactRef = "artifact://gate-summary"
	run.Gate.Failure = DepartureFailure{
		Action: "infrastructure_repair", RepairTaskID: "BGR-T-0007",
		AffectedTaskIDs: []string{"APP-T-0002", "APP-T-0001"},
		ArtifactRefs:    []string{"artifact://raw-gate"},
		Packet: PromotionFailurePacket{Defects: []GateDefect{{
			Target: "TestPromotion", Excerpt: "token=do-not-leak timed out",
		}}},
	}
	brief := composeScheduledPromotionMorningBrief(
		newScheduledPromotionMorningBrief("app", scheduledPromotionTestProjection(scheduledPromotionPromote, false), night, now),
		scheduledPromotionMorningBriefFacts{Index: scheduledPromotionEmptyIndex(), Departures: []DepartureRun{run}},
		night,
	)
	if len(brief.BlockedOrRepairing) != 1 {
		t.Fatalf("repairing departure missing: %#v", brief.BlockedOrRepairing)
	}
	blocked := brief.BlockedOrRepairing[0]
	if blocked.FirstActionableCause == "" || len(blocked.AffectedScope) != 2 || blocked.AutomaticAction == "" || blocked.Href == "" {
		t.Fatalf("blocked item is missing its actionable contract: %#v", blocked)
	}
	if blocked.AffectedScope[0] != "APP-T-0001" || !strings.Contains(blocked.AutomaticAction, "infrastructure repair") {
		t.Fatalf("repair routing/scope mismatch: %#v", blocked)
	}
	if strings.Contains(blocked.FirstActionableCause, "do-not-leak") || !strings.Contains(blocked.FirstActionableCause, "[redacted]") {
		t.Fatalf("bounded cause leaked a secret: %q", blocked.FirstActionableCause)
	}
	if blocked.RepairTaskID != "BGR-T-0007" || !strings.Contains(blocked.Href, "BGR-T-0007") {
		t.Fatalf("repair deep link is not actionable: %#v", blocked)
	}
}

func TestScheduledPromotionMorningBriefNeedsRealHumanDecisionOnly(t *testing.T) {
	night, now := scheduledPromotionTestTimes()
	task := briefTask("APP-T-0001", "review", "satisfied")
	valid := briefGate(
		"APP-G-0002", "release",
		"Approve the production release in the account-owner console.",
		"The account owner records the release approval.",
		"Only the account owner has production release authority.",
	)
	machine := briefGate("APP-G-0001", "verification", "Run the focused test.", "The focused test passes.", "The agent can execute this work.")
	machine.Data["owner"] = "agent:operator"
	idx := scheduledPromotionEmptyIndex()
	idx.Tasks["APP-T-0001"] = task
	idx.Gates["APP-G-0002"] = valid
	idx.Gates["APP-G-0001"] = machine
	facts := scheduledPromotionMorningBriefFacts{
		Index: idx,
		// Review and escalation prose is deliberately not promoted to a human
		// decision; only the validated human-gate boundary is authoritative.
		Logbook: tuskerLogbook{NeedsHuman: []logbookNeed{
			{Kind: "review", Label: "Review code"},
			{Kind: "escalation", Label: "Machine failure"},
		}},
	}
	brief := composeScheduledPromotionMorningBrief(
		newScheduledPromotionMorningBrief("app", scheduledPromotionTestProjection(scheduledPromotionPromote, false), night, now),
		facts, night,
	)
	if len(brief.NeedsYourDecision) != 1 {
		t.Fatalf("machine work leaked into decisions: %#v", brief.NeedsYourDecision)
	}
	decision := brief.NeedsYourDecision[0]
	if decision.GateID != "APP-G-0002" || decision.WhyHuman == "" || decision.Verification == "" ||
		!reflect.DeepEqual(decision.AffectedScope, []string{"APP-T-0001"}) || !strings.Contains(decision.Href, "APP-G-0002") {
		t.Fatalf("human decision is incomplete: %#v", decision)
	}
}

func TestScheduledPromotionMorningBriefMixedPromotionAndReleaseFixture(t *testing.T) {
	night, now := scheduledPromotionTestTimes()
	failedRelease := scheduledPromotionTestDeparture("departure-release-failed", "2026-07-24T22:00:00Z")
	failedRelease.State = DepartureStateFailed
	failedRelease.Candidate.TaskSourceSHAs = map[string]string{"APP-T-0001": "source-a"}
	failedRelease.Promotion = DeparturePromotion{
		CommittedRef: "refs/heads/main", CommittedSHA: "promoted-still-live", CommittedAt: "2026-07-24T22:00:00Z",
	}
	failedRelease.Release = DepartureRelease{
		Profile: "production", Revision: "release-attempt-7", Status: "failed",
		AttemptedAt: "2026-07-24T22:05:00Z", CompletedAt: "2026-07-24T22:06:00Z",
	}
	released := scheduledPromotionTestDeparture("departure-released", "2026-07-24T23:00:00Z")
	released.State = DepartureStatePassed
	released.Candidate.TaskSourceSHAs = map[string]string{"APP-T-0002": "source-b"}
	released.Promotion = DeparturePromotion{
		CommittedRef: "refs/heads/main", CommittedSHA: "promoted-and-released", CommittedAt: "2026-07-24T23:00:00Z",
	}
	released.Release = DepartureRelease{
		Profile: "production", Revision: "release-revision-8", Status: "released",
		AttemptedAt: "2026-07-24T23:05:00Z", CompletedAt: "2026-07-24T23:06:00Z",
	}
	brief := composeScheduledPromotionMorningBrief(
		newScheduledPromotionMorningBrief("app", scheduledPromotionTestProjection(scheduledPromotionPromote, true), night, now),
		scheduledPromotionMorningBriefFacts{Index: scheduledPromotionEmptyIndex(), Departures: []DepartureRun{failedRelease, released}},
		night,
	)
	if len(brief.LandedLastNight) != 2 || len(brief.BlockedOrRepairing) != 1 {
		t.Fatalf("mixed promotion/release outcome was flattened: %#v", brief)
	}
	byID := map[string]scheduledPromotionMorningBriefLanded{}
	for _, landed := range brief.LandedLastNight {
		byID[landed.ID] = landed
	}
	if byID["departure-release-failed"].PromotedSHA != "promoted-still-live" ||
		byID["departure-release-failed"].ReleasedRevision != "" {
		t.Fatalf("failed release was presented as released: %#v", byID["departure-release-failed"])
	}
	if byID["departure-released"].ReleasedRevision != "release-revision-8" {
		t.Fatalf("successful released revision missing: %#v", byID["departure-released"])
	}
	blocked := brief.BlockedOrRepairing[0]
	if blocked.Kind != "release" || !strings.Contains(blocked.AutomaticAction, "promoted revision remains unchanged") {
		t.Fatalf("release failure did not preserve promotion truth: %#v", blocked)
	}
}

func TestScheduledPromotionMorningBriefRejectsNonSuccessReleaseStatuses(t *testing.T) {
	night, now := scheduledPromotionTestTimes()
	for _, status := range []string{"blocked", "pending", "cancelled"} {
		t.Run(status, func(t *testing.T) {
			run := scheduledPromotionTestDeparture("departure-"+status, "2026-07-24T23:00:00Z")
			run.State = DepartureStateBlocked
			run.Promotion = DeparturePromotion{
				CommittedRef: "refs/heads/main", CommittedSHA: "promoted-" + status, CommittedAt: "2026-07-24T23:00:00Z",
			}
			// Even malformed/noncanonical rows with apparently complete release
			// fields must not be upgraded to a successful release.
			run.Release = DepartureRelease{
				Profile: "production", Revision: "not-a-release", Status: status, CompletedAt: "2026-07-24T23:05:00Z",
			}
			brief := composeScheduledPromotionMorningBrief(
				newScheduledPromotionMorningBrief("app", scheduledPromotionTestProjection(scheduledPromotionPromote, true), night, now),
				scheduledPromotionMorningBriefFacts{Index: scheduledPromotionEmptyIndex(), Departures: []DepartureRun{run}},
				night,
			)
			if len(brief.LandedLastNight) != 1 || brief.LandedLastNight[0].ReleasedRevision != "" {
				t.Fatalf("%q release status was presented as released: %#v", status, brief.LandedLastNight)
			}
			if !strings.Contains(renderScheduledPromotionMorningBrief(brief), "Promoted") ||
				strings.Contains(renderScheduledPromotionMorningBrief(brief), "released revision") {
				t.Fatalf("%q status collapsed promotion into release:\n%s", status, renderScheduledPromotionMorningBrief(brief))
			}
		})
	}
}

func TestScheduledPromotionMorningBriefStableOrdering(t *testing.T) {
	night, now := scheduledPromotionTestTimes()
	earlier := scheduledPromotionTestDeparture("departure-z", "2026-07-24T21:00:00Z")
	earlier.State = DepartureStatePassed
	earlier.Promotion = DeparturePromotion{CommittedRef: "main", CommittedSHA: "sha-z", CommittedAt: "2026-07-24T21:00:00Z"}
	later := scheduledPromotionTestDeparture("departure-a", "2026-07-24T23:00:00Z")
	later.State = DepartureStatePassed
	later.Promotion = DeparturePromotion{CommittedRef: "main", CommittedSHA: "sha-a", CommittedAt: "2026-07-24T23:00:00Z"}
	blockedZ := scheduledPromotionTestDeparture("blocked-z", "2026-07-24T20:00:00Z")
	blockedZ.State, blockedZ.BlockReason = DepartureStateBlocked, "Z failure"
	blockedA := scheduledPromotionTestDeparture("blocked-a", "2026-07-24T20:00:00Z")
	blockedA.State, blockedA.BlockReason = DepartureStateBlocked, "A failure"
	idx := scheduledPromotionEmptyIndex()
	idx.Tasks["APP-T-0001"] = briefTask("APP-T-0001", "ready", "pending")
	for _, gateID := range []string{"APP-G-0002", "APP-G-0001"} {
		idx.Gates[gateID] = briefGate(
			gateID, "release", "Approve the production release in the account-owner console.",
			"The account owner records the release approval.", "Only the account owner has production release authority.",
		)
	}

	base := newScheduledPromotionMorningBrief("app", scheduledPromotionTestProjection(scheduledPromotionPromote, false), night, now)
	left := composeScheduledPromotionMorningBrief(base, scheduledPromotionMorningBriefFacts{
		Index: idx, Departures: []DepartureRun{earlier, blockedZ, later, blockedA},
	}, night)
	right := composeScheduledPromotionMorningBrief(base, scheduledPromotionMorningBriefFacts{
		Index: idx, Departures: []DepartureRun{blockedA, later, blockedZ, earlier},
	}, night)
	leftJSON, _ := json.Marshal(left)
	rightJSON, _ := json.Marshal(right)
	if string(leftJSON) != string(rightJSON) {
		t.Fatalf("input ordering changed the projection:\n%s\n%s", leftJSON, rightJSON)
	}
	if got := []string{left.LandedLastNight[0].ID, left.LandedLastNight[1].ID}; !reflect.DeepEqual(got, []string{"departure-a", "departure-z"}) {
		t.Fatalf("landed ordering = %#v", got)
	}
	if got := []string{left.BlockedOrRepairing[0].ID, left.BlockedOrRepairing[1].ID}; !reflect.DeepEqual(got, []string{"blocked-a", "blocked-z"}) {
		t.Fatalf("blocked ordering = %#v", got)
	}
	if got := []string{left.NeedsYourDecision[0].GateID, left.NeedsYourDecision[1].GateID}; !reflect.DeepEqual(got, []string{"APP-G-0001", "APP-G-0002"}) {
		t.Fatalf("decision ordering = %#v", got)
	}
}

func TestScheduledPromotionMorningBriefServeProjection(t *testing.T) {
	server := newServeEmptyNeedsFixture(t)
	data, body, err := parseFrontmatterMustRead(workflowPath(server.vaultPath))
	if err != nil {
		t.Fatal(err)
	}
	data["scheduled_promotion"] = map[string]any{"version": 1, "mode": scheduledPromotionShadow}
	workflow, err := serializeDocument(data, body, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeText(workflowPath(server.vaultPath), workflow); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/morning-brief?date=2026-07-05", nil)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("morning brief endpoint returned %d: %s", rec.Code, rec.Body.String())
	}
	var brief scheduledPromotionMorningBrief
	if err := json.Unmarshal(rec.Body.Bytes(), &brief); err != nil {
		t.Fatal(err)
	}
	if brief.Schema != scheduledPromotionMorningBriefSchema || brief.Night != "2026-07-05" || !brief.Feature.Enabled {
		t.Fatalf("Serve returned the wrong shared projection: %#v", brief)
	}
	if brief.LandedLastNight == nil || brief.BlockedOrRepairing == nil || brief.NeedsYourDecision == nil {
		t.Fatalf("Serve returned null primary lists: %s", rec.Body.String())
	}

	bad := httptest.NewRecorder()
	server.ServeHTTP(bad, httptest.NewRequest(http.MethodGet, "/api/morning-brief?date=yesterday", nil))
	if bad.Code != http.StatusBadRequest {
		t.Fatalf("invalid date returned %d: %s", bad.Code, bad.Body.String())
	}
}

func TestScheduledPromotionMorningBriefCLIIsReadOnly(t *testing.T) {
	stateRoot := filepath.Join(t.TempDir(), "runtime")
	t.Setenv("TUSKER_STATE_ROOT", stateRoot)
	vault := newWaveTestVault(t, 1)
	if err := writeDefaultWorkflow(vault); err != nil {
		t.Fatal(err)
	}
	output := captureStdout(t, func() {
		if err := logbookCmd(Args{
			"vault": vault, "scheduled-promotion": "true", "date": "2026-07-24", "json": "true",
		}); err != nil {
			t.Fatal(err)
		}
	})
	var brief scheduledPromotionMorningBrief
	if err := json.Unmarshal([]byte(output), &brief); err != nil {
		t.Fatalf("decode CLI morning brief: %v\n%s", err, output)
	}
	if brief.Schema != scheduledPromotionMorningBriefSchema || brief.Feature.Enabled {
		t.Fatalf("logbook CLI returned the wrong projection: %#v", brief)
	}
	if _, err := os.Stat(runtimeStoreDBPath(stateRoot)); !os.IsNotExist(err) {
		t.Fatalf("read-only feature-off CLI created a runtime database: %v", err)
	}
}

func scheduledPromotionTestTimes() (time.Time, time.Time) {
	return time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 7, 25, 7, 30, 0, 0, time.UTC)
}

func scheduledPromotionTestProjection(mode string, release bool) ScheduledPromotionProjection {
	projection := ScheduledPromotionProjection{
		Configured: true, Mode: mode, Provenance: "workflow", Observe: true,
		Stage:   mode == scheduledPromotionStage || mode == scheduledPromotionPromote,
		Promote: mode == scheduledPromotionPromote,
	}
	projection.Release = projection.Promote && release
	return projection
}

func scheduledPromotionTestDeparture(id, scheduledWindow string) DepartureRun {
	return DepartureRun{
		ID: id, ProjectID: "app", PolicyID: "nightly", ScheduledWindow: scheduledWindow,
		State: DepartureStateDue, Candidate: DepartureCandidate{
			TaskStateRevisions: map[string]string{}, TaskSourceSHAs: map[string]string{},
		},
		CreatedAt: scheduledWindow, UpdatedAt: scheduledWindow,
	}
}

func scheduledPromotionEmptyIndex() v7Index {
	return v7Index{
		Tasks: map[string]Note{}, Gates: map[string]Note{}, Waves: map[string]Note{},
		Escalations: map[string]Note{}, Evidence: map[string][]Note{}, Attempts: map[string][]Note{},
		Decisions: map[string]Note{}, Epics: map[string]Note{}, Proposals: map[string]Note{}, Closeouts: map[string][]Note{},
	}
}
