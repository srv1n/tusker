package main

import (
	"path/filepath"
	"testing"
)

func TestImplicitSingletonPolicyMatrixAndReplay(t *testing.T) {
	for _, tc := range []struct {
		mode string
		want bool
	}{
		{scheduledPromotionDisabled, false},
		{scheduledPromotionShadow, false},
		{scheduledPromotionStage, true},
		{scheduledPromotionPromote, true},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			_, vault := newLandTestRepo(t, 1, "true")
			clearWaveBackpointer(t, vault, "APP-T-0001")
			setSingletonPromotionMode(t, vault, tc.mode)
			if tc.want {
				setWaveTaskState(t, vault, "APP-T-0001", "review", "review", "")
			}

			unit, created, err := ensureV7ImplicitSingletonDeliveryUnit(vault, "APP-T-0001", Args{"vault": vault, "quiet": "true"})
			if err != nil {
				t.Fatal(err)
			}
			if created != tc.want {
				t.Fatalf("created=%v, want %v", created, tc.want)
			}
			if !tc.want {
				if unit != "" || fileExists(filepath.Join(vault, "work", "waves", "W-0002.md")) {
					t.Fatalf("%s must not materialize a delivery unit: unit=%q", tc.mode, unit)
				}
				return
			}
			if unit != "W-0002" {
				t.Fatalf("unexpected unit %q", unit)
			}
			data, _, err := parseFrontmatterMustRead(filepath.Join(vault, "work", "waves", unit+".md"))
			if err != nil {
				t.Fatal(err)
			}
			if !v7ImplicitDeliveryUnit(Note{Data: data}) || stringField(data, "authorization") != "disarmed" || boolFromAny(data["release_authorized"]) {
				t.Fatalf("implicit unit widened authority: %#v", data)
			}
			again, recreated, err := ensureV7ImplicitSingletonDeliveryUnit(vault, "APP-T-0001", Args{"vault": vault, "quiet": "true"})
			if err != nil || recreated || again != unit {
				t.Fatalf("replay must adopt exact unit: unit=%q created=%v err=%v", again, recreated, err)
			}
			eventIssues, _, _ := validateV7Events(vault)
			if len(eventIssues) != 0 {
				t.Fatalf("implicit delivery-unit creation event must validate: %#v", eventIssues)
			}
		})
	}
}

func TestImplicitSingletonDoesNotWrapUnfinishedTask(t *testing.T) {
	repo, vault := newLandTestRepo(t, 1, "true")
	clearWaveBackpointer(t, vault, "APP-T-0001")
	setSingletonPromotionMode(t, vault, scheduledPromotionStage)

	_, created, err := ensureV7ImplicitSingletonDeliveryUnit(vault, "APP-T-0001", Args{"vault": vault, "quiet": "true"})
	if err == nil || created {
		t.Fatalf("unfinished task must not be wrapped: created=%v err=%v", created, err)
	}
	if fileExists(filepath.Join(vault, "work", "waves", "W-0002.md")) || gitBranchExists(repo, "integration/W-0002") {
		t.Fatal("refused singleton wrapping must not leave a wave record or integration branch")
	}
	task, _, readErr := parseFrontmatterMustRead(filepath.Join(vault, "work", "tasks", "APP-T-0001.md"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	if stringField(task, "wave") != "" {
		t.Fatalf("refused singleton wrapping changed task membership: %#v", task["wave"])
	}
}

func TestImplicitSingletonLandAdoptsReviewedTaskWithoutWaveCeremony(t *testing.T) {
	repo, vault := newLandTestRepo(t, 1, "test -f singleton.txt")
	clearWaveBackpointer(t, vault, "APP-T-0001")
	setSingletonPromotionMode(t, vault, scheduledPromotionStage)
	setWaveTaskState(t, vault, "APP-T-0001", "review", "review", "")
	commitLandBranch(t, repo, "task/APP-T-0001", "integration/W-0001", map[string]string{"singleton.txt": "ok\n"})

	if err := landV7Cmd(Args{"vault": vault, "quiet": "true", "_pos0": "APP-T-0001"}); err != nil {
		t.Fatalf("standalone reviewed task should stage: %v", err)
	}
	task, _, err := parseFrontmatterMustRead(filepath.Join(vault, "work", "tasks", "APP-T-0001.md"))
	if err != nil {
		t.Fatal(err)
	}
	unit := stringField(task, "wave")
	if unit == "" || !fileExists(filepath.Join(vault, "work", "waves", unit+".md")) {
		t.Fatalf("landing did not adopt a singleton delivery unit: %#v", task)
	}
	assertEqual(t, "ok\n", gitShowFile(t, repo, v7IntegrationBranchName(unit), "singleton.txt"), "staged singleton work")
}

func clearWaveBackpointer(t *testing.T, vault, taskID string) {
	t.Helper()
	path := filepath.Join(vault, "work", "tasks", taskID+".md")
	data, body, err := parseFrontmatterMustRead(path)
	if err != nil {
		t.Fatal(err)
	}
	delete(data, "wave")
	if _, err := saveV7DocumentCAS(path, data, body, v7FrontmatterOrder["task"], stringField(data, "state_rev")); err != nil {
		t.Fatal(err)
	}
}

func setSingletonPromotionMode(t *testing.T, vault, mode string) {
	t.Helper()
	if err := writeDefaultWorkflow(vault); err != nil {
		t.Fatal(err)
	}
	path := workflowPath(vault)
	data, body, err := parseFrontmatterMustRead(path)
	if err != nil {
		t.Fatal(err)
	}
	data["scheduled_promotion"] = map[string]any{"version": 1, "mode": mode}
	content, err := serializeDocument(data, body, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeText(path, content); err != nil {
		t.Fatal(err)
	}
}
