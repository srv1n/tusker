package main

import (
	"strings"
	"testing"
)

// TestNewTaskBodyHasTwoLayers proves a freshly created task body opens with a
// plain-language top section and carries a separate builder appendix, with the
// plain top appearing before the appendix.
func TestNewTaskBodyHasTwoLayers(t *testing.T) {
	body := v7TaskBody("APP-T-0001", "Two-layer template")

	intent := strings.TrimSpace(sectionContent(body, "## Intent"))
	appendix := strings.TrimSpace(sectionContent(body, "## Implementation notes"))
	if intent == "" {
		t.Fatal("new task body must include a plain-language Intent section")
	}
	if appendix == "" {
		t.Fatal("new task body must include a builder appendix (Implementation notes)")
	}

	intentAt := strings.Index(body, "## Intent")
	appendixAt := strings.Index(body, "## Implementation notes")
	if intentAt < 0 || appendixAt < 0 || intentAt >= appendixAt {
		t.Fatalf("plain top must come before the builder appendix; Intent at %d, appendix at %d", intentAt, appendixAt)
	}
}

// TestNewTaskAppendixSeparateFromIntent proves the appendix is a distinct
// section clearly marked as builder-only detail, and that the builder marking
// does not bleed into the plain Intent layer.
func TestNewTaskAppendixSeparateFromIntent(t *testing.T) {
	body := v7TaskBody("APP-T-0001", "Marked appendix")

	intent := strings.ToLower(sectionContent(body, "## Intent"))
	appendix := strings.ToLower(sectionContent(body, "## Implementation notes"))

	if !strings.Contains(appendix, "builder") {
		t.Fatalf("appendix must be marked as builder-only, got:\n%s", appendix)
	}
	if strings.Contains(intent, "builder appendix") {
		t.Fatalf("the builder-only marking must stay out of the plain Intent layer, got:\n%s", intent)
	}
	if strings.TrimSpace(appendix) == strings.TrimSpace(intent) {
		t.Fatal("appendix and Intent must be distinct sections")
	}
}

// TestNewTaskAcceptanceReadsAsOutcomes proves the generated "done" scaffold is
// phrased as a visible outcome rather than as code internals.
func TestNewTaskAcceptanceReadsAsOutcomes(t *testing.T) {
	body := v7TaskBody("APP-T-0001", "Outcome acceptance")

	acceptance := strings.ToLower(sectionContent(body, "## Acceptance"))
	if strings.TrimSpace(acceptance) == "" {
		t.Fatal("new task body must include an Acceptance section")
	}
	if !strings.Contains(acceptance, "see") {
		t.Fatalf("acceptance scaffold must read as a visible outcome, got:\n%s", acceptance)
	}
	if strings.Contains(acceptance, "complete the task contract") {
		t.Fatalf("acceptance scaffold must not fall back to a generic non-outcome, got:\n%s", acceptance)
	}
}
