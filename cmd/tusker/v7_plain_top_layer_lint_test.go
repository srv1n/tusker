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

// Prose times like "5 p.m." must not be mistaken for single-letter-extension
// filenames, while a real single-letter-extension file is still caught.
func TestTopLayerLintFilenameSingleLetterExt(t *testing.T) {
	clean := v7TopLayerCodeWords(twoLayerBody(
		"Ship the update by 5 p.m. tomorrow, and no earlier than 9 a.m. today.",
		"Nothing here.",
	))
	if len(clean) != 0 {
		t.Fatalf("prose times must not be flagged as filenames: %#v", clean)
	}
	// A genuine two-letter-stem single-letter-extension file is still a code word.
	real := v7TopLayerCodeWords(twoLayerBody(
		"The build compiles from main.c into the binary.",
		"Nothing here.",
	))
	if !containsString(real, "main.c") {
		t.Fatalf("real single-letter-extension file should be flagged: %#v", real)
	}
}

// Mixed-case words are only code words when corroborating evidence backs them.
// Marketing-style product names stay clean; genuine identifiers are flagged.
func TestTopLayerLintMixedCaseEvidence(t *testing.T) {
	cases := []struct {
		name   string
		intent string
		token  string
		flag   bool
	}{
		{"testflight", "Testers install the build through TestFlight before launch.", "TestFlight", false},
		{"paypal", "Customers can pay with PayPal or a card at checkout.", "PayPal", false},
		{"doordash", "The order is delivered through DoorDash within the hour.", "DoorDash", false},
		{"icloud", "Photos sync to iCloud automatically overnight.", "iCloud", false},
		{"linkedin", "Recruiters find the profile on LinkedIn easily.", "LinkedIn", false},
		{"lowercamel", "The loop runs scheduleBatchGateIfDue on each tick.", "scheduleBatchGateIfDue", true},
		{"digitcamel", "The helper reads v7TaskBody before rendering.", "v7TaskBody", true},
		{"callparen", "It then calls defaultWorkflow() to build the plan.", "defaultWorkflow", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			offenders := v7TopLayerCodeWords(twoLayerBody(tc.intent, "Nothing here."))
			got := containsString(offenders, tc.token)
			if got != tc.flag {
				t.Fatalf("token %q: flagged=%v want=%v (offenders=%#v)", tc.token, got, tc.flag, offenders)
			}
		})
	}
}

// A PascalCase word that carries no local evidence is clean, but the same word
// becomes a code word once it appears verbatim in the builder appendix.
func TestTopLayerLintAppendixEvidence(t *testing.T) {
	intent := "The DefaultWorkflow decides what runs first."
	clean := v7TopLayerCodeWords(twoLayerBody(intent, "Nothing here."))
	if containsString(clean, "DefaultWorkflow") {
		t.Fatalf("PascalCase word with no evidence must stay clean: %#v", clean)
	}
	flagged := v7TopLayerCodeWords(twoLayerBody(intent, "Call DefaultWorkflow in the dispatch loop."))
	if !containsString(flagged, "DefaultWorkflow") {
		t.Fatalf("word appearing in appendix should be flagged: %#v", flagged)
	}
}

// The appendix boundary is recognized case-insensitively and tolerates the
// heading level and trailing-word variations.
func TestTopLayerLintAppendixHeadingVariations(t *testing.T) {
	for _, heading := range []string{
		"## Implementation notes",
		"### Implementation Notes",
		"## implementation notes for the builder",
	} {
		body := strings.Join([]string{
			"## Intent",
			"",
			"A plain reader can follow this without any jargon at all.",
			"",
			heading,
			"",
			"The builder edits cmd/tusker/foo.go and calls v7TaskBody here.",
		}, "\n")
		if offenders := v7TopLayerCodeWords(body); len(offenders) != 0 {
			t.Fatalf("heading %q: appendix code words leaked: %#v", heading, offenders)
		}
	}
}

// A path and the bare filename it ends with occupy a single display slot.
func TestTopLayerLintDedupsByBaseName(t *testing.T) {
	offenders := v7TopLayerCodeWords(twoLayerBody(
		"The runner reads cmd/tusker/foo.go during startup.",
		"Nothing here.",
	))
	if !containsString(offenders, "cmd/tusker/foo.go") {
		t.Fatalf("expected the full path to be reported: %#v", offenders)
	}
	if containsString(offenders, "foo.go") {
		t.Fatalf("bare filename should be deduped against the path: %#v", offenders)
	}
}
