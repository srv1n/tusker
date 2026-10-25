package main

import (
	"strings"
	"testing"
	"time"
)

// An agent recording a member of an unarmed wave gets the next step, not the
// internal reason the daemon could not queue the review.
func TestDogfoodReviewQueueUnarmedWaveNamesNextStep(t *testing.T) {
	vault, store, project := adoptFixture(t, "APP-T-0001", map[string]any{"wave": "W-0001"})
	writeDirectWave(t, vault, "W-0001", []string{"APP-T-0001"}, nil)
	now := time.Now().UTC()
	if err := store.UpsertRun(RunStatus{
		ProjectID: project.ProjectID, RecordID: "APP-T-0001", ItemID: "APP-T-0001", Lane: runLaneExecute,
		LeaseState: string(LeaseStateReleased), LeaseOwner: "agent:test",
		LeaseExpiresAt: now.Add(time.Hour).Format(time.RFC3339), UpdatedAt: now.Format(time.RFC3339),
	}); err != nil {
		t.Fatal(err)
	}
	result := adoptRun(t, vault, store, project.ProjectID, "APP-T-0001", "agent:test")
	if !result.OK || stringField(adoptStatus(t, vault, "APP-T-0001"), "status") != "review" {
		t.Fatalf("unexpected result %+v", result)
	}
	for _, want := range []string{"W-0001 is not armed", "tusker review APP-T-0001", "tusker done APP-T-0001 --by human:"} {
		if !strings.Contains(result.Reason, want) {
			t.Fatalf("reason %q lacks %q", result.Reason, want)
		}
	}
	if strings.Contains(result.Reason, "execution window") {
		t.Fatalf("reason still shows the internal refusal: %q", result.Reason)
	}
}
