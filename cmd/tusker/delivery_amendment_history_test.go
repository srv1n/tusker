package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDeliveryAmendmentRetainedHistory(t *testing.T) {
	for _, scenario := range []string{"empty registry", "unclaimed observation", "other project", "execute attempt", "review child attempt", "review result", "run history", "corrupt review result"} {
		t.Run(scenario, func(t *testing.T) {
			t.Setenv("TUSKER_STATE_ROOT", filepath.Join(t.TempDir(), "state"))
			vault := deliveryTestVault(t)
			plan, path, _ := deliveryAmendmentPlan(t, vault)
			if err := deliveryImportCmd(Args{"vault": vault, "plan": path, "quiet": "true"}); err != nil {
				t.Fatal(err)
			}
			wave := deliveryWaveForScope(t, vault, plan.Scope)
			members := normalizeList(wave.Data["members"])
			if len(members) == 0 {
				t.Fatal("fixture imported no members")
			}
			taskID := members[0]
			store, err := OpenRuntimeStore(DefaultStateRoot())
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = store.Close() })
			project := newRegisteredProject(v7RepoRoot(vault), vault)
			// Disabled registrations still own durable execution history.
			if err := store.UpsertProject(project); err != nil {
				t.Fatal(err)
			}
			refuse := true
			switch scenario {
			case "empty registry":
				refuse = false
			case "unclaimed observation":
				refuse = false
				err = store.UpsertRun(RunStatus{ProjectID: project.ProjectID, RecordID: taskID, ItemID: taskID, LeaseState: string(LeaseStateUnclaimed)})
			case "other project", "execute attempt", "review child attempt":
				attempt := RunAttempt{AttemptID: "retained-attempt", ProjectID: project.ProjectID, RecordID: taskID, ItemID: taskID, Lane: runLaneExecute, Outcome: string(AttemptOutcomeFailed)}
				if scenario == "other project" {
					refuse = false
					attempt.ProjectID = "unrelated-project"
				}
				if scenario == "review child attempt" {
					attempt.RecordID += "#review"
					attempt.Lane = runLaneReview
				}
				err = store.SaveAttempt(attempt)
			case "review result":
				result := validStoredReviewResult()
				result.ProjectID, result.TaskID = project.ProjectID, taskID
				_, err = store.SaveReviewResult(result)
			case "run history":
				err = store.UpsertRun(RunStatus{ProjectID: project.ProjectID, RecordID: taskID, ItemID: taskID, AttemptCount: 1, LeaseGeneration: 1, LeaseState: string(LeaseStateReleased)})
			case "corrupt review result":
				// Malformed retained authority is not permission to rewrite it.
				_, err = store.exec(`INSERT INTO review_results(project_id,task_id,work_revision,attempt_id,result_json) VALUES(?,?,?,?,?)`, project.ProjectID, taskID, 1, "corrupt-review", "{")
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			plan.Summary = "Amend a held plan without rewriting any execution history."
			plan.ContextFingerprint = ""
			path = writeDeliveryV2TestPlan(t, vault, plan)
			before := snapshotDeliveryRecords(t, vault)
			for _, dryRun := range []string{"true", "false"} {
				err := deliveryImportCmd(Args{"vault": vault, "plan": path, "quiet": "true", "dry-run": dryRun})
				if refuse {
					if err == nil || !strings.Contains(err.Error(), "new plan scope/wave") {
						t.Fatalf("dry-run=%s rewrote retained history or refused for the wrong reason: %v", dryRun, err)
					}
					assertEqual(t, before, snapshotDeliveryRecords(t, vault), "refused amendment must leave canonical records untouched")
				} else if err != nil {
					t.Fatalf("dry-run=%s empty/unrelated history incorrectly froze a held plan: %v", dryRun, err)
				}
			}
		})
	}
}

func TestDeliveryAmendmentUnreadableHistoryFailsClosed(t *testing.T) {
	t.Setenv("TUSKER_STATE_ROOT", filepath.Join(t.TempDir(), "state"))
	vault := deliveryTestVault(t)
	plan, path, _ := deliveryAmendmentPlan(t, vault)
	if err := deliveryImportCmd(Args{"vault": vault, "plan": path, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertProject(newRegisteredProject(v7RepoRoot(vault), vault)); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	plan.Summary = "Must not infer no history from an unreadable database."
	plan.ContextFingerprint = ""
	path = writeDeliveryV2TestPlan(t, vault, plan)
	if err := os.WriteFile(runtimeStoreDBPath(DefaultStateRoot()), []byte("not a database"), 0o600); err != nil {
		t.Fatal(err)
	}
	before := snapshotDeliveryRecords(t, vault)
	if err := deliveryImportCmd(Args{"vault": vault, "plan": path, "quiet": "true"}); err == nil {
		t.Fatal("unreadable runtime history must not authorize an amendment")
	}
	assertEqual(t, before, snapshotDeliveryRecords(t, vault), "unreadable history refusal must preserve canonical records")
}

func TestDeliveryAmendmentWithoutRuntimeDoesNotCreateStore(t *testing.T) {
	state := filepath.Join(t.TempDir(), "state")
	t.Setenv("TUSKER_STATE_ROOT", state)
	vault := deliveryTestVault(t)
	plan, path, _ := deliveryAmendmentPlan(t, vault)
	if err := deliveryImportCmd(Args{"vault": vault, "plan": path, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	plan.Summary = "A new held plan does not require a runtime database."
	plan.ContextFingerprint = ""
	path = writeDeliveryV2TestPlan(t, vault, plan)
	if err := deliveryImportCmd(Args{"vault": vault, "plan": path, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(runtimeStoreDBPath(state)); !os.IsNotExist(err) {
		t.Fatalf("amendment created a runtime store or could not establish its absence: %v", err)
	}
}
