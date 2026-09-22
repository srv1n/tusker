package main

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func selfServiceCheck(t *testing.T, vault, waveID string) error {
	t.Helper()
	var checkErr error
	captured := captureStdout(t, func() {
		checkErr = waveReviewCmd(Args{"vault": vault, "id": waveID, "check": "true"})
	})
	_ = captured
	return checkErr
}

func selfServiceWaveAuthorization(t *testing.T, vault, waveID string) string {
	t.Helper()
	idx, err := loadV7Index(vault)
	if err != nil {
		t.Fatal(err)
	}
	return stringField(idx.Waves[waveID].Data, "authorization")
}

func selfServiceAssertNoPublication(t *testing.T, vault string, store *RuntimeStore, project RegisteredProject, waveID string) {
	t.Helper()
	if authorization := selfServiceWaveAuthorization(t, vault, waveID); authorization == "armed" {
		t.Fatalf("invalid wave %s was authorized", waveID)
	}
	if got := queuedDirectives(t, store, project.ProjectID); len(got) != 0 {
		t.Fatalf("invalid wave %s published directives: %#v", waveID, got)
	}
}

func TestSelfServiceAuthoring(t *testing.T) {
	t.Run("A1 invalid later frontier member fails preflight without publication", func(t *testing.T) {
		vault, store, project := autonomousWaveFixture(t, []string{"APP-T-0001", "APP-T-0002"}, map[string]any{"concurrency": 1})
		writeDirectTask(t, vault, "APP-T-0001", "W-0001", nil)
		writeDirectTask(t, vault, "APP-T-0002", "W-0001", map[string]any{
			"dependencies":   []any{"APP-T-0001:hard"},
			"proof_required": []any{},
		})

		checkErr := selfServiceCheck(t, vault, "W-0001")
		if checkErr == nil {
			t.Fatal("preflight passed a later-frontier member without proof mapping")
		}
		for _, want := range []string{"APP-T-0002", "MEMBER_CONTRACT_INVALID", "repair:"} {
			if !strings.Contains(checkErr.Error(), want) {
				t.Fatalf("preflight failure %q misses %q", checkErr.Error(), want)
			}
		}
		review, err := buildDirectWaveReview(vault, store, project.ProjectID, "W-0001", nil)
		if err != nil {
			t.Fatal(err)
		}
		blocked := false
		for _, blocker := range review.Blockers {
			if blocker.TaskID == "APP-T-0002" && blocker.Code == "MEMBER_CONTRACT_INVALID" {
				blocked = true
				if strings.TrimSpace(blocker.Action) == "" {
					t.Fatalf("member blocker carries no repair action: %#v", blocker)
				}
			}
		}
		if !blocked {
			t.Fatalf("review has no contract blocker for the invalid member: %#v", review.Blockers)
		}
		if _, err := directWaveStart(vault, store, "W-0001", "human:sarav"); err == nil {
			t.Fatal("start authorized a wave with an invalid member")
		}
		selfServiceAssertNoPublication(t, vault, store, project, "W-0001")
	})

	t.Run("A2 missing refs cycles ownership and stale pins fail on the full graph", func(t *testing.T) {
		t.Run("missing member", func(t *testing.T) {
			vault, store, project := autonomousWaveFixture(t, []string{"APP-T-0001", "APP-T-0009"}, nil)
			writeDirectTask(t, vault, "APP-T-0001", "W-0001", nil)
			checkErr := selfServiceCheck(t, vault, "W-0001")
			if checkErr == nil || !strings.Contains(checkErr.Error(), "APP-T-0009") || !strings.Contains(checkErr.Error(), "MATERIAL_INVALID") {
				t.Fatalf("preflight did not name the missing member: %v", checkErr)
			}
			if _, err := directWaveStart(vault, store, "W-0001", "human:sarav"); err == nil {
				t.Fatal("start authorized a wave with a missing member")
			}
			selfServiceAssertNoPublication(t, vault, store, project, "W-0001")
		})

		t.Run("cross-wave cycle", func(t *testing.T) {
			vault, store, project := autonomousWaveFixture(t, []string{"APP-T-0001"}, nil)
			writeDirectTask(t, vault, "APP-T-0007", "", map[string]any{"dependencies": []any{"APP-T-0001:hard"}})
			idx, err := loadV7Index(vault)
			if err != nil {
				t.Fatal(err)
			}
			external, ok := idx.Tasks["APP-T-0007"]
			if !ok {
				t.Fatal("external task did not persist")
			}
			writeDirectTask(t, vault, "APP-T-0001", "W-0001", map[string]any{
				"dependencies": []any{"APP-T-0007:hard"},
				"dependency_contracts": []any{map[string]any{
					"task_id": "APP-T-0007", "kind": "hard",
					"target_contract_fingerprint": directWaveTaskContract(external),
				}},
			})
			checkErr := selfServiceCheck(t, vault, "W-0001")
			if checkErr == nil || !strings.Contains(checkErr.Error(), "MATERIAL_CYCLIC") {
				t.Fatalf("preflight missed the cross-wave cycle: %v", checkErr)
			}
			if !strings.Contains(checkErr.Error(), "APP-T-0007") || !strings.Contains(checkErr.Error(), "APP-T-0001") {
				t.Fatalf("cycle report does not name the full path: %v", checkErr)
			}
			if _, err := directWaveStart(vault, store, "W-0001", "human:sarav"); err == nil {
				t.Fatal("start authorized a cyclic wave")
			}
			selfServiceAssertNoPublication(t, vault, store, project, "W-0001")
		})

		t.Run("frontier ownership collision", func(t *testing.T) {
			vault, store, project := autonomousWaveFixture(t, []string{"APP-T-0001", "APP-T-0002"}, nil)
			writeDirectTask(t, vault, "APP-T-0001", "W-0001", map[string]any{"owned_paths": []any{"cmd/tusker/overlap"}})
			writeDirectTask(t, vault, "APP-T-0002", "W-0001", map[string]any{"owned_paths": []any{"cmd/tusker/overlap"}})
			checkErr := selfServiceCheck(t, vault, "W-0001")
			if checkErr == nil || !strings.Contains(checkErr.Error(), "OWNED_PATH_FRONTIER_CONFLICT") {
				t.Fatalf("preflight missed the ownership collision: %v", checkErr)
			}
			if !strings.Contains(checkErr.Error(), "APP-T-0001") || !strings.Contains(checkErr.Error(), "APP-T-0002") {
				t.Fatalf("collision report does not name both members: %v", checkErr)
			}
			if _, err := directWaveStart(vault, store, "W-0001", "human:sarav"); err == nil {
				t.Fatal("start authorized colliding ownership")
			}
			selfServiceAssertNoPublication(t, vault, store, project, "W-0001")
		})

		t.Run("stale external dependency pin", func(t *testing.T) {
			vault, store, project := autonomousWaveFixture(t, []string{"APP-T-0001", "APP-T-0002"}, nil)
			writeDirectTask(t, vault, "APP-T-0001", "W-0001", nil)
			writeDirectTask(t, vault, "APP-T-0007", "", nil)
			writeDirectTask(t, vault, "APP-T-0002", "W-0001", map[string]any{
				"dependencies": []any{"APP-T-0007:hard"},
				"dependency_contracts": []any{map[string]any{
					"task_id": "APP-T-0007", "kind": "hard", "target_contract_fingerprint": "stale-pin",
				}},
			})
			checkErr := selfServiceCheck(t, vault, "W-0001")
			if checkErr == nil || !strings.Contains(checkErr.Error(), "DEPENDENCY_CONTRACT_INVALID") || !strings.Contains(checkErr.Error(), "APP-T-0002") {
				t.Fatalf("preflight missed the stale external pin: %v", checkErr)
			}
			if _, err := directWaveStart(vault, store, "W-0001", "human:sarav"); err == nil {
				t.Fatal("start authorized a stale dependency pin")
			}
			selfServiceAssertNoPublication(t, vault, store, project, "W-0001")
		})
	})

	t.Run("A3 waiting inert project-off and unavailable runtime stay out of contract validity", func(t *testing.T) {
		vault, store, project := autonomousWaveFixture(t, []string{"APP-T-0001", "APP-T-0002"}, map[string]any{"concurrency": 1})
		writeDirectTask(t, vault, "APP-T-0001", "W-0001", nil)
		writeDirectTask(t, vault, "APP-T-0002", "W-0001", map[string]any{"dependencies": []any{"APP-T-0001:hard"}})

		// Well-formed dependency waiting and inert (disarmed) authorization are
		// not contract defects: preflight passes and Start stays enabled.
		if err := selfServiceCheck(t, vault, "W-0001"); err != nil {
			t.Fatalf("preflight failed well-formed waiting work: %v", err)
		}
		if err := store.SetProjectEnabled(project.ProjectID, false); err != nil {
			t.Fatal(err)
		}
		if err := selfServiceCheck(t, vault, "W-0001"); err != nil {
			t.Fatalf("preflight failed a well-formed wave with the project off: %v", err)
		}

		// Unavailable runtime is a runtime dimension, never a contract defect.
		review, err := buildDirectWaveReview(vault, nil, project.ProjectID, "W-0001", errors.New("runtime store is gone"))
		if err != nil {
			t.Fatal(err)
		}
		runtimeBlamed := false
		for _, blocker := range review.Blockers {
			if blocker.Code == "RUNTIME_UNAVAILABLE" {
				runtimeBlamed = true
			}
			if blocker.Code == "MEMBER_CONTRACT_INVALID" {
				t.Fatalf("unavailable runtime was reported as a contract defect: %#v", blocker)
			}
		}
		if !runtimeBlamed {
			t.Fatalf("review has no runtime-dimension blocker: %#v", review.Blockers)
		}
		if refusal := directWaveStartRefusal(review); refusal == nil || !strings.Contains(refusal.Error(), "RUNTIME_UNAVAILABLE") {
			t.Fatalf("start refusal did not carry the runtime dimension: %v", refusal)
		}
		diagnosis, err := diagnoseWaveForDoctorWithRuntime(vault, nil, errors.New("runtime store is gone"), project.ProjectID, "W-0001", time.Now().UTC())
		if err != nil {
			t.Fatal(err)
		}
		if diagnosis.PrimaryClassification != DiagnosticUnavailable {
			t.Fatalf("doctor primary = %s, want unavailable", diagnosis.PrimaryClassification)
		}
		if code := doctorExitForClassification(diagnosis.PrimaryClassification); code != 2 {
			t.Fatalf("doctor exit = %d, want 2", code)
		}
	})

	t.Run("A4 changed route contract and owner are rechecked and refused", func(t *testing.T) {
		vault, store, project := autonomousWaveFixture(t, []string{"APP-T-0001", "APP-T-0002"}, map[string]any{"concurrency": 1})
		writeDirectTask(t, vault, "APP-T-0001", "W-0001", nil)
		writeDirectTask(t, vault, "APP-T-0002", "W-0001", map[string]any{"dependencies": []any{"APP-T-0001:hard"}})
		if err := selfServiceCheck(t, vault, "W-0001"); err != nil {
			t.Fatalf("preflight failed valid work: %v", err)
		}

		writeTaskFileOutOfBand(t, vault, "APP-T-0001", func(data map[string]any, body string) (map[string]any, string) {
			return data, body + "\nOut-of-band edit.\n"
		})
		if _, err := directWaveStart(vault, store, "W-0001", "human:sarav"); err == nil {
			t.Fatal("start authorized drifted material after a passing preflight")
		}
		selfServiceAssertNoPublication(t, vault, store, project, "W-0001")

		rewriteTaskFile(t, vault, "APP-T-0002", func(data map[string]any, body string) (map[string]any, string) {
			data["work_level"] = "demanding"
			return data, body
		})
		review, err := buildDirectWaveReview(vault, store, project.ProjectID, "W-0001", nil)
		if err != nil {
			t.Fatal(err)
		}
		routeBlamed := false
		for _, blocker := range review.Blockers {
			if blocker.TaskID == "APP-T-0002" && blocker.Code == "ROUTE_INVALID" {
				routeBlamed = true
			}
		}
		if !routeBlamed {
			t.Fatalf("changed route was not refused: %#v", review.Blockers)
		}
		if refusal := directWaveStartRefusal(review); refusal == nil || !strings.Contains(refusal.Error(), "APP-T-0002") {
			t.Fatalf("route refusal did not name the member: %v", refusal)
		}

		rewriteTaskFile(t, vault, "APP-T-0002", func(data map[string]any, body string) (map[string]any, string) {
			delete(data, "execute_profile")
			data["next_owner"] = "human:other"
			return data, body
		})
		review, err = buildDirectWaveReview(vault, store, project.ProjectID, "W-0001", nil)
		if err != nil {
			t.Fatal(err)
		}
		ownerBlamed := false
		for _, blocker := range review.Blockers {
			if blocker.TaskID == "APP-T-0002" && blocker.Code == "MEMBER_CONTRACT_INVALID" {
				ownerBlamed = true
			}
		}
		if !ownerBlamed {
			t.Fatalf("changed owner was not refused: %#v", review.Blockers)
		}

	})

	t.Run("A4 partial done and review waves keep their lifecycle", func(t *testing.T) {
		vault, store, project, fingerprint, authorizedAt := selfServiceArmedABFixture(t)
		selfServiceClaimRoot(t, store, project, fingerprint, authorizedAt)
		markDirectTaskDone(t, vault, "APP-T-0001")
		releaseRoot := RunStatus{ProjectID: project.ProjectID, RecordID: "APP-T-0001", ItemID: "APP-T-0001", LeaseState: string(LeaseStateReleased), Terminal: true, AttemptOutcome: string(AttemptOutcomeSucceeded)}
		if err := store.UpsertRun(releaseRoot); err != nil {
			t.Fatal(err)
		}
		daemon := &Daemon{stateRoot: DefaultStateRoot(), store: store}
		if err := daemon.advanceAuthorizedWaveFrontiers(project); err != nil {
			t.Fatal(err)
		}
		directives := queuedDirectives(t, store, project.ProjectID)
		if len(directives) != 1 || directives[0].RecordID != "APP-T-0002" {
			t.Fatalf("partial wave did not release only the dependent: %#v", directives)
		}
		review, err := buildDirectWaveReview(vault, store, project.ProjectID, "W-0001", nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, member := range review.Members {
			if member.TaskID == "APP-T-0001" && member.State != "completed" {
				t.Fatalf("accepted implementation lost its lifecycle state: %#v", member)
			}
		}
		rewriteTaskFile(t, vault, "APP-T-0002", func(data map[string]any, body string) (map[string]any, string) {
			data["status"] = "review"
			data["readiness"] = "waiting_on_review"
			return data, body
		})
		if got, err := queueAuthorizedWaveFrontier(vault, store, project.ProjectID, "W-0001", time.Now().UTC()); err != nil || len(got) != 0 {
			t.Fatalf("review member re-queued: %v err=%v", got, err)
		}
	})
}
