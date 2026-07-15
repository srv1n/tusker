package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSkillExecutionOwnershipProtocol(t *testing.T) {
	source := filepath.Join("..", "..", "skills", "tusker", "SKILL.md")
	raw, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, required := range []string{
		"Never start `tusker daemon run`", "independently running resident daemon",
		"The daemon atomically claims the task before it creates the worker process",
		"it must not claim again", "tusker runs inspect <TASK-ID>",
		"runner harness owns session attachment, heartbeats, process monitoring",
		"heartbeat expiry and safe reclaim", "Never write `active` or", "`in_progress` into task frontmatter",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("canonical skill missing %q", required)
		}
	}
	if strings.Contains(text, "tusker runs claim <TASK-ID>") {
		t.Fatal("dispatched worker instructions must not repeat the daemon's pre-spawn claim")
	}
	for _, installed := range []string{filepath.Join("..", "..", ".agents", "skills", "tusker", "SKILL.md"), filepath.Join("..", "..", ".claude", "skills", "tusker", "SKILL.md")} {
		resolved, err := filepath.EvalSymlinks(installed)
		if err != nil {
			t.Fatal(err)
		}
		if resolved != filepath.Clean(source) {
			absResolved, _ := filepath.Abs(resolved)
			absSource, _ := filepath.Abs(source)
			if absResolved != absSource {
				t.Fatalf("generated install %s does not resolve to canonical skill", installed)
			}
		}
	}
}

func TestSkillReservesHumanApprovalForHumanOnlyBoundaries(t *testing.T) {
	source := filepath.Join("..", "..", "skills", "tusker", "SKILL.md")
	raw, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, required := range []string{
		"## Human Approval Boundary",
		"Everything already decided by the task, acceptance criteria, governing spec, or",
		"final human acceptance of screenshots, recordings, UX feel, brand quality",
		"Risk changes proof depth, reviewer strength, and landing safeguards; risk alone",
		"Independent reviewers may",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("canonical skill missing human-approval rule %q", required)
		}
	}
}
