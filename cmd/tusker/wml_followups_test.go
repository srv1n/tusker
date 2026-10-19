package main

import (
	"strings"
	"testing"
	"time"
)

// F1: stop --all records an operator stop on the run whose agent it killed,
// so a restarted daemon treats it as stopped, not crashed.
func TestStopAllMarksRunsOperatorStopped(t *testing.T) {
	root := t.TempDir()
	t.Setenv("TUSKER_STATE_ROOT", root)
	store, err := OpenRuntimeStore(root)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	agent := startJanitorTestGroup(t)
	start, _ := processStartTime(agent.pid)
	run := RunStatus{ProjectID: "app", RecordID: "APP-T-0001", ItemID: "APP-T-0001", Lane: runLaneExecute,
		ActiveAttemptID: "att-1", LeaseOwner: "att-1", LeaseGeneration: 1, LeaseState: string(LeaseStateRunning),
		ProcessPID: agent.pid, ProcessPGID: agent.pid, ProcessStartedAt: start, LastEventAt: time.Now().UTC().Format(time.RFC3339Nano)}
	if err := store.UpsertRun(run); err != nil {
		t.Fatal(err)
	}
	recordJanitorTestRow(t, store, "att-1", processKindAgentSession, agent, "", currentProcessStarter())

	if err := stopCmd(Args{"all": "true", "json": "true"}); err != nil {
		t.Fatal(err)
	}
	if !agent.stopped(3 * time.Second) {
		t.Fatal("agent group still running after stop --all")
	}
	intent, err := loadRunSessionControlIntent(store, "app", "APP-T-0001")
	if err != nil || intent == nil || intent.Action != runSessionControlStop || intent.LeaseGeneration != 1 {
		t.Fatalf("stop intent = %+v, %v; want an operator stop for generation 1", intent, err)
	}
	if got := serveProcessStopReasons[endReasonOperatorStoppedAll]; got == "" {
		t.Fatal("operator_stopped_all has no Serve label")
	}
}

// F2: a changes_requested result reviewed against an older task revision is
// not applied again; the task waits in review for its new review.
func TestReviewReactorIgnoresStaleResult(t *testing.T) {
	env := newReviewPassEnv(t, map[string]string{"reviewed.txt": "reviewed\n"}, []string{"reviewed.txt"})
	env.submitProposal(t, "changes_requested")
	// A contract change after the review (as an owned_paths widening makes).
	setAutomationV7TaskFields(t, env.vault, "APP-T-0001", map[string]any{"owned_paths": []string{"reviewed.txt", "more.txt"}})
	env.passHandler(t)
	task := env.task(t)
	if stringField(task.Data, "status") != "review" || strings.Contains(task.Body, "Fix the greeting.") {
		t.Fatalf("stale result was applied: status=%q", stringField(task.Data, "status"))
	}
}

// F3: a review-sanctioned owned_paths widening re-authorizes the member's
// wave as the governor; a wave already stale for another reason is left for
// a human.
func TestWideningKeepsWaveAuthorized(t *testing.T) {
	arm := func(t *testing.T, vault string, stale bool) {
		t.Helper()
		setAutomationV7TaskFields(t, vault, "APP-T-0001", map[string]any{"wave": "W-0001"})
		writeDirectWave(t, vault, "W-0001", []string{"APP-T-0001"}, map[string]any{"status": "active"})
		idx, err := loadV7Index(vault)
		if err != nil {
			t.Fatal(err)
		}
		fingerprint, issues := waveMaterialFingerprint(vault, idx, idx.Waves["W-0001"])
		if len(issues) > 0 {
			t.Fatal(issues)
		}
		if stale {
			fingerprint = "sha256:stale"
		}
		rewriteWaveBody(t, vault, "W-0001", func(data map[string]any, body string) (map[string]any, string) {
			data["authorization"], data["authorization_fingerprint"] = "armed", fingerprint
			data["authorized_by"], data["authorized_at"] = "human:sarav", "2026-01-01T00:00:00Z"
			return data, body
		})
	}

	t.Run("sanctioned widening re-authorizes", func(t *testing.T) {
		vault, finding := wmlWideningFixture(t)
		arm(t, vault, false)
		if err := returnReviewerFindingToImplementer(vault, "APP-T-0001", finding, "daemon:review-pass"); err != nil {
			t.Fatal(err)
		}
		if got := strings.Join(normalizeList(wmlTask(t, vault, "APP-T-0001").Data["owned_paths"]), ","); got != "lib/caller.go,src" {
			t.Fatalf("owned_paths = %q", got)
		}
		idx, err := loadV7Index(vault)
		if err != nil {
			t.Fatal(err)
		}
		wave := idx.Waves["W-0001"]
		fingerprint, _ := waveMaterialFingerprint(vault, idx, wave)
		if !directWaveAuthorizationCurrent(wave.Data, "armed", fingerprint) {
			t.Fatalf("wave not re-authorized: %#v", wave.Data)
		}
		if stringField(wave.Data, "authorized_by") != ownedPathWideningEditActor || !strings.Contains(stringField(wave.Data, "authorization_reason"), "lib/caller.go") {
			t.Fatalf("authorization not recorded as the governor with a reason: %#v", wave.Data)
		}
	})

	t.Run("already stale wave still needs a human", func(t *testing.T) {
		vault, finding := wmlWideningFixture(t)
		arm(t, vault, true)
		if err := returnReviewerFindingToImplementer(vault, "APP-T-0001", finding, "daemon:review-pass"); err != nil {
			t.Fatal(err)
		}
		wave := wmlWave(t, vault)
		if stringField(wave.Data, "authorization_fingerprint") != "sha256:stale" || stringField(wave.Data, "authorized_by") != "human:sarav" {
			t.Fatalf("stale wave was re-authorized: %#v", wave.Data)
		}
	})
}

func wmlWave(t *testing.T, vault string) Note {
	t.Helper()
	wave, err := resolveV7Note(vault, "W-0001", "wave")
	if err != nil {
		t.Fatal(err)
	}
	return wave
}
