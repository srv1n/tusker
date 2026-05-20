package v7schema

import "testing"

func TestStateRevIgnoresStateRevField(t *testing.T) {
	data := map[string]any{"id": "VSD-T-0011", "state_rev": "sha256:old"}
	without := map[string]any{"id": "VSD-T-0011"}
	if StateRev(data, "body") != StateRev(without, "body") {
		t.Fatal("StateRev should ignore existing state_rev")
	}
}

func TestStateRevCanonicalizesNilSlicesLikeSerializedYAML(t *testing.T) {
	withNilSlice := map[string]any{"id": "VSD-T-0011", "domains": []string(nil)}
	withEmptySlice := map[string]any{"id": "VSD-T-0011", "domains": []any{}}
	if StateRev(withNilSlice, "body") != StateRev(withEmptySlice, "body") {
		t.Fatal("StateRev should treat nil slices as serialized empty sequences")
	}
}

func TestPureHelpers(t *testing.T) {
	if !TaskIDPattern.MatchString("VSD-T-0011") {
		t.Fatal("expected task ID pattern to match V7 task ID")
	}
	if EpicFromTaskID("VSD-T-0011") != "VSD" {
		t.Fatal("expected epic acronym from task ID")
	}
	if NormalizeMutationMode("Single User-Local") != "single_user_local" {
		t.Fatal("expected mutation mode normalization")
	}
	body := "# T\n\n## Acceptance\n\n| ID | Outcome | Proof |\n|---|---|---|\n| A1 | Move code | tests |\n- [ ] keep cli behavior\n\n## Other\n"
	if AcceptanceCount(body) != 2 {
		t.Fatalf("expected 2 acceptance items, got %d", AcceptanceCount(body))
	}
}
