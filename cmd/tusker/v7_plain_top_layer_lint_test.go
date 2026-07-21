package main

import (
	"strings"
	"testing"
)

// twoLayerBody builds a task body with a plain top layer (intent) and a builder
// appendix, so tests can vary the two independently.
func twoLayerBody(intent, appendix string) string {
	return strings.Join([]string{
		"# ABC-T-0001 · Sample",
		"",
		"## Intent",
		"",
		intent,
		"",
		"## Acceptance",
		"",
		"| ID | Outcome | Proof |",
		"|---|---|---|",
		"| A1 | Someone can see the result. | Inline verification |",
		"",
		"## Implementation notes",
		"",
		appendix,
		"",
		"## Verification",
		"",
		"| Covers | Check | Result | Notes |",
		"|---|---|---|---|",
		"| A1 | command: go test ./cmd/tusker -run TestFoo -count=1 | pending | - |",
		"",
	}, "\n")
}

func lintTop(body string, data map[string]any) (errs, warns []Issue) {
	if data == nil {
		data = map[string]any{}
	}
	lintV7PlainTopLayer(Note{Data: data, Body: body}, "task.md", &errs, &warns)
	return errs, warns
}

// A1: code words in the top layer are flagged, with the offending word named.
func TestTopLayerLintFlagsCodeWords(t *testing.T) {
	body := twoLayerBody(
		"The runner reads cmd/tusker/foo.go and calls the v7TaskBody helper via `renderTask`.",
		"Nothing here.",
	)
	errs, warns := lintTop(body, map[string]any{"priority": "p2", "risk": "low"})
	if len(errs) != 0 {
		t.Fatalf("non-demanding task should warn, not error: %#v", errs)
	}
	if len(warns) != 1 {
		t.Fatalf("expected one warning, got %#v", warns)
	}
	w := warns[0]
	if w.Code != "TASK_TOP_LAYER_CODE_WORDS" {
		t.Fatalf("unexpected code %q", w.Code)
	}
	for _, want := range []string{"cmd/tusker/foo.go", "v7TaskBody", "renderTask"} {
		if !strings.Contains(w.Message, want) {
			t.Fatalf("offending word %q not named in message %q", want, w.Message)
		}
	}
}

// A2: the builder appendix is exempt from the check.
func TestTopLayerLintExemptsAppendix(t *testing.T) {
	body := twoLayerBody(
		"A former product manager can read this cold: the feature simply works.",
		"File map: cmd/tusker/foo.go. Call v7TaskBody and `renderTask` in the loop.",
	)
	errs, warns := lintTop(body, map[string]any{"priority": "p1", "risk": "high", "readiness": "ready"})
	if len(errs) != 0 || len(warns) != 0 {
		t.Fatalf("appendix code words must not be flagged: errs=%#v warns=%#v", errs, warns)
	}
}

// A3: a clean plain-language top passes, and a demanding task cannot go ready
// while its top layer still reads like code.
func TestTopLayerLintCleanPasses(t *testing.T) {
	clean := twoLayerBody(
		"This lets a reviewer see, at a glance, whether a task is ready. It matters "+
			"because broken visibility forces manual work. Done looks like a clear list.",
		"Details: cmd/tusker/foo.go.",
	)
	errs, warns := lintTop(clean, map[string]any{"priority": "p1", "risk": "high", "readiness": "ready"})
	if len(errs) != 0 || len(warns) != 0 {
		t.Fatalf("clean plain top should pass quietly: errs=%#v warns=%#v", errs, warns)
	}

	dirty := twoLayerBody(
		"The check scans cmd/tusker/v7_validation.go for the v7TaskBody token.",
		"Nothing here.",
	)
	errs, warns = lintTop(dirty, map[string]any{"priority": "p1", "risk": "high", "readiness": "ready"})
	if len(errs) == 0 {
		t.Fatalf("demanding ready task with code words in top layer must be blocked (error); warns=%#v", warns)
	}

	// Same dirty top, but not yet ready: a warning, not a hard block.
	errs, warns = lintTop(dirty, map[string]any{"priority": "p1", "risk": "high", "readiness": "blocked_by_dependency"})
	if len(errs) != 0 {
		t.Fatalf("not-ready demanding task should warn, not error: %#v", errs)
	}
	if len(warns) == 0 {
		t.Fatalf("expected a warning for dirty top layer")
	}
}
