package main

import "testing"

func TestWorkspaceMaterialScopeRejectsPathspecEscapes(t *testing.T) {
	for _, bad := range []string{"cmd/..", "cmd/../x", "a/./b", ":(top)", ":/", "..", "a//b"} {
		if _, err := normalizeWorkspaceMaterialScope([]string{bad}); err == nil {
			t.Errorf("scope %q accepted", bad)
		}
	}
	if got, err := normalizeWorkspaceMaterialScope([]string{"/crates/alpha/", "cmd/tusker"}); err != nil || len(got) != 2 {
		t.Fatalf("clean scope refused: %v %v", got, err)
	}
}
