package main

import "testing"

func TestV7ProofCategoryRequiresTypedEvidenceKind(t *testing.T) {
	cases := []struct {
		required string
		kind     string
		artifact string
	}{
		{required: "screenshot", kind: "automated_test", artifact: "proof.png"},
		{required: "video", kind: "automated_test", artifact: "proof.mp4"},
		{required: "trace", kind: "automated_test", artifact: "proof.trace"},
	}

	for _, tc := range cases {
		ev := Note{Data: map[string]any{
			"evidence_kind":  tc.kind,
			"artifact_paths": []string{tc.artifact},
		}}
		if v7EvidenceSatisfiesProofRequired(tc.required, ev) {
			t.Errorf("%s proof must not be satisfied by %s evidence with %s artifact", tc.required, tc.kind, tc.artifact)
		}

		ev.Data["evidence_kind"] = tc.required
		if !v7EvidenceSatisfiesProofRequired(tc.required, ev) {
			t.Errorf("typed %s evidence should satisfy %s proof", tc.required, tc.required)
		}
	}
}
