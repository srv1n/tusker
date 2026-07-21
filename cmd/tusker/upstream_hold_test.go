package main

import (
	"strings"
	"testing"
)

// setupUpstreamHoldVault builds a dependency APP-T-0001 and a dispatchable
// dependent APP-T-0002 that hard-depends on it, then marks the dependency as a
// closed piece whose shared build-and-test came back red.
func setupUpstreamHoldVault(t *testing.T) string {
	t.Helper()
	vault := automationTestVault(t)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Upstream piece", "risk": "low", "priority": "p0", "v7": "true"}, newV7Task)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Dependent", "risk": "low", "priority": "p0", "dependencies": "APP-T-0001:hard", "v7": "true"}, newV7Task)
	makeV7TaskDispatchableForTest(t, vault, "APP-T-0002")
	// Upstream piece is closed/green on status but its shared build-and-test failed.
	setAutomationV7TaskFields(t, vault, "APP-T-0001", map[string]any{
		"status":       "done",
		"readiness":    "done",
		"proof_status": "satisfied",
		"build_failed": true,
	})
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true"}, reconcileV7Cmd)
	return vault
}

func upstreamHoldTaskNote(t *testing.T, vault, id string) Note {
	t.Helper()
	idx, err := loadV7Index(vault)
	if err != nil {
		t.Fatal(err)
	}
	note, ok := idx.Tasks[id]
	if !ok {
		t.Fatalf("task %s not found in index", id)
	}
	return note
}

// A1: a dependent of a piece whose shared build-and-test is red is held, not dispatched.
func TestRedGateHoldsDependents(t *testing.T) {
	vault := setupUpstreamHoldVault(t)

	dependent := mustTaskData(t, vault, "APP-T-0002")
	assertEqual(t, "held", stringField(dependent, "readiness"), "dependent held for upstream failure")

	note := upstreamHoldTaskNote(t, vault, "APP-T-0002")
	if isV7DispatchableAgentTask(vault, note) {
		t.Fatalf("expected held dependent to be non-dispatchable")
	}
	if _, ok := pickV7Next(vault, "", "agent"); ok {
		t.Fatalf("expected no dispatchable task while dependent is held")
	}
}

// A2: the hold names the upstream piece that failed.
func TestHeldDependentNamesUpstream(t *testing.T) {
	vault := setupUpstreamHoldVault(t)

	dependent := mustTaskData(t, vault, "APP-T-0002")
	assertEqual(t, "APP-T-0001", stringField(dependent, "next_ref"), "hold names failed upstream")
	if action := stringField(dependent, "next_action"); !strings.Contains(action, "APP-T-0001") {
		t.Fatalf("expected hold action to name APP-T-0001, got %q", action)
	}

	note := upstreamHoldTaskNote(t, vault, "APP-T-0002")
	blockers := v7TaskDispatchBlockers(vault, note)
	found := false
	for _, reason := range blockers {
		if strings.Contains(reason, "APP-T-0001") && strings.Contains(reason, "upstream failure") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected dispatch blocker naming upstream failure, got %#v", blockers)
	}
}

// A3: when the upstream piece goes green, the held work becomes pickable again.
func TestGreenUpstreamReleasesDependent(t *testing.T) {
	vault := setupUpstreamHoldVault(t)
	assertEqual(t, "held", stringField(mustTaskData(t, vault, "APP-T-0002"), "readiness"), "dependent starts held")

	// Upstream build-and-test goes green: clear the red marker.
	setAutomationV7TaskFields(t, vault, "APP-T-0001", map[string]any{
		"build_failed": false,
	})
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true"}, reconcileV7Cmd)

	dependent := mustTaskData(t, vault, "APP-T-0002")
	assertEqual(t, "ready", stringField(dependent, "readiness"), "dependent released after upstream green")

	note := upstreamHoldTaskNote(t, vault, "APP-T-0002")
	if !isV7DispatchableAgentTask(vault, note) {
		t.Fatalf("expected released dependent to be dispatchable, blockers: %#v", v7TaskDispatchBlockers(vault, note))
	}
}

func holdTestTask(id, status string, extra map[string]any) Note {
	data := map[string]any{"schema": "tusker.task/v7", "kind": "task", "id": id, "project": "app", "status": status}
	for k, v := range extra {
		data[k] = v
	}
	return Note{Data: data}
}

// Finding 3: a shared red gate holds a dependent through its own wave even when
// the piece carrying the marker is a fresh repair task with zero dependents.
func TestRedGateHoldsWaveMemberViaFreshRepairTask(t *testing.T) {
	repair := holdTestTask("BGR-T-0001", "ready", map[string]any{
		"build_failed":         true,
		"build_failed_command": "go test ./...",
	})
	member := holdTestTask("APP-T-0002", "ready", map[string]any{"wave": "W-0001"})
	unrelated := holdTestTask("APP-T-0003", "ready", nil)
	wave := Note{Data: map[string]any{"schema": "tusker.wave/v7", "kind": "wave", "id": "W-0001",
		"members": []string{"APP-T-0002"}, "build_failed_command": "go test ./..."}}
	idx := v7Index{
		Tasks: map[string]Note{"BGR-T-0001": repair, "APP-T-0002": member, "APP-T-0003": unrelated},
		Waves: map[string]Note{"W-0001": wave},
	}

	if _, held := v7HeldByFailedUpstream(member, idx); !held {
		t.Fatalf("expected wave member to be held by shared red gate even though the marker sits on a dependent-less repair task")
	}
	if _, held := v7HeldByFailedUpstream(unrelated, idx); held {
		t.Fatalf("expected a task outside the red wave (and with no red dependency) to stay free")
	}
	if _, held := v7HeldByFailedUpstream(repair, idx); held {
		t.Fatalf("the repair task itself carries no wave marker and depends on nothing, so it must not hold itself")
	}
}

// Finding 6: a red marker on a dead (cancelled/superseded) dependency must be
// ignored, since no green run will ever clear it; a done dependency still holds.
func TestDeadUpstreamMarkerDoesNotHoldForever(t *testing.T) {
	dependent := holdTestTask("APP-T-0002", "ready", map[string]any{"dependencies": []string{"APP-T-0001:hard"}})

	for _, dead := range []string{"cancelled", "superseded"} {
		dep := holdTestTask("APP-T-0001", dead, map[string]any{"build_failed": true})
		idx := v7Index{Tasks: map[string]Note{"APP-T-0001": dep, "APP-T-0002": dependent}, Waves: map[string]Note{}}
		if _, held := v7HeldByFailedUpstream(dependent, idx); held {
			t.Fatalf("expected %s upstream's red marker to be ignored, got dependent held forever", dead)
		}
	}

	live := holdTestTask("APP-T-0001", "done", map[string]any{"build_failed": true})
	idx := v7Index{Tasks: map[string]Note{"APP-T-0001": live, "APP-T-0002": dependent}, Waves: map[string]Note{}}
	if _, held := v7HeldByFailedUpstream(dependent, idx); !held {
		t.Fatalf("a done-but-red-on-build dependency must still hold its dependent")
	}
}

// Finding 5: a task already in review must stay in the review queue and not be
// pulled out by an upstream build hold.
func TestReviewPrecedesUpstreamHold(t *testing.T) {
	vault := setupUpstreamHoldVault(t)
	setAutomationV7TaskFields(t, vault, "APP-T-0002", map[string]any{"status": "review"})
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true"}, reconcileV7Cmd)

	dependent := mustTaskData(t, vault, "APP-T-0002")
	assertEqual(t, "waiting_on_review", stringField(dependent, "readiness"), "task in review stays in review, not held")
}
