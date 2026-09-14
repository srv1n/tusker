package main

import (
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func directIntakeTaskBody(title, outcome string) string {
	return "# " + title + "\n\n## Outcome\n\n" + outcome + "\n\n## Acceptance\n\n| ID | Outcome |\n| --- | --- |\n| A1 | " + outcome + " |\n"
}

func writeDirectIntakeRequest(t *testing.T, vault string, req map[string]any) string {
	t.Helper()
	raw, err := yaml.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(v7RepoRoot(vault), ".tusker", "scratch", "intake-wave.yaml")
	if err := writeText(path, string(raw)); err != nil {
		t.Fatal(err)
	}
	return path
}

func directIntakeRequest(tasks []map[string]any) map[string]any {
	return map[string]any{
		"schema":      "tusker.wave-authoring/v1",
		"request_key": "intake",
		"title":       "Intake",
		"outcome":     "Direct intake classification.",
		"spec_refs":   []string{".tusker/specs/delivery.md"},
		"tasks":       tasks,
	}
}

func directIntakeTask(key, level string) map[string]any {
	return map[string]any{
		"key":        key,
		"title":      "Task " + key,
		"work_level": level,
		"body":       directIntakeTaskBody("Task "+key, "Outcome "+key),
	}
}

func TestTaskAuthoringIntakeClassifiesAndReturnsLinks(t *testing.T) {
	vault := v7DirectTestVault(t)
	if err := newV7Epic(Args{"vault": vault, "quiet": "true", "acronym": "VTP", "title": "Intake"}); err != nil {
		t.Fatal(err)
	}
	task := directIntakeTask("only", "standard")
	task["epic"] = "VTP"
	task["spec_refs"] = []string{".tusker/specs/delivery.md"}
	path := writeDirectIntakeRequest(t, vault, directIntakeRequest([]map[string]any{task}))

	if err := waveV7CreateCmd(Args{"vault": vault, "file": path, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	taskPath := filepath.Join(vault, "work", "tasks", "VTP-T-0001.md")
	taskData, body, err := parseFrontmatterMustRead(taskPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := stringField(taskData, "work_level"); got != "standard" {
		t.Fatalf("authoring lost work_level: %q", got)
	}
	if got := normalizeList(taskData["spec_refs"]); len(got) != 1 || got[0] != ".tusker/specs/delivery.md" {
		t.Fatalf("authoring changed exact spec anchor: %#v", taskData["spec_refs"])
	}
	if !v7StateRevMatches(taskData, body, stringField(taskData, "state_rev")) {
		t.Fatal("authored task state_rev does not match content")
	}

	packet := v7Packet(vault, Note{Data: taskData, Body: body}, mustIndex(t, vault), "agent")
	for _, want := range []string{"Work level: `standard`", "Review level: `standard` (inherited)", ".tusker/specs/delivery.md"} {
		if !strings.Contains(packet, want) {
			t.Fatalf("agent packet missing %q:\n%s", want, packet)
		}
	}

	wavePath := filepath.Join(vault, "work", "waves", "W-0001.md")
	waveData, waveBody, err := parseFrontmatterMustRead(wavePath)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"## Intended result", "## Members", "## Ordered work", "## Blockers and human actions", "## Closure criteria", "[VTP-T-0001](../tasks/VTP-T-0001.md)"} {
		if !strings.Contains(waveBody, want) {
			t.Fatalf("wave brief missing %q:\n%s", want, waveBody)
		}
	}
	waveBody += "\n## Operator notes\n\nKeep the architect informed of any changed product decision.\n"
	waveData["state_rev"] = v7StateRev(waveData, waveBody)
	waveContent, err := serializeDocument(waveData, waveBody, v7FrontmatterOrder["wave"])
	if err != nil {
		t.Fatal(err)
	}
	if err := writeText(wavePath, waveContent); err != nil {
		t.Fatal(err)
	}

	// Re-creating with the same request key replays the durable receipt without
	// rewriting the wave record or its authored prose.
	if err := waveV7CreateCmd(Args{"vault": vault, "file": path, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	_, replayedBody, err := parseFrontmatterMustRead(wavePath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(replayedBody, "## Operator notes") || !strings.Contains(replayedBody, "Keep the architect informed") {
		t.Fatalf("receipt replay lost authored wave prose:\n%s", replayedBody)
	}
	if strings.Count(replayedBody, "## Intended result") != 1 {
		t.Fatalf("receipt replay duplicated managed wave brief:\n%s", replayedBody)
	}
}

func TestTaskAuthoringIntakeRejectsMissingOrNamedWorkLevel(t *testing.T) {
	tests := []struct {
		name  string
		level string
		want  string
	}{
		{name: "missing", want: "work_level is required for agent work"},
		{name: "named model", level: "Sol Medium", want: "invalid work_level Sol Medium"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vault := v7DirectTestVault(t)
			path := writeDirectIntakeRequest(t, vault, directIntakeRequest([]map[string]any{directIntakeTask("only", tt.level)}))
			if err := waveV7CreateCmd(Args{"vault": vault, "file": path, "quiet": "true"}); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("authoring error=%v, want %q", err, tt.want)
			}
		})
	}
}

func TestTaskAuthoringEntryPointsRequireExplicitWorkLevel(t *testing.T) {
	vault := pickupV7TestVault(t)
	base := Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Direct task"}
	if err := newAuthoredV7Task(base); err == nil || !strings.Contains(err.Error(), "--work-level is required") {
		t.Fatalf("direct authoring error=%v", err)
	}
	if _, err := applyV7CreateTaskProposal(vault, "APP", "epic", map[string]any{"title": "Proposed task"}, "agent:architect"); err == nil || !strings.Contains(err.Error(), "--work-level is required") {
		t.Fatalf("proposal authoring error=%v", err)
	}
	base["work-level"] = "standard"
	bodyPath := filepath.Join(v7RepoRoot(vault), ".tusker", "scratch", "direct-body.md")
	if err := writeText(bodyPath, "# Direct task\n\nSubstantive body.\n"); err != nil {
		t.Fatal(err)
	}
	base["body-file"] = bodyPath
	if err := newAuthoredV7Task(base); err != nil {
		t.Fatal(err)
	}
}

func TestTaskAuthoringIntakeRejectsIncompleteHumanGate(t *testing.T) {
	vault := v7DirectTestVault(t)
	req := directIntakeRequest([]map[string]any{directIntakeTask("only", "standard")})
	req["human_actions"] = []map[string]any{{
		"key": "approve", "task": "only", "action": "Approve", "verification": "v", "why_agent_cannot": "w",
	}}
	path := writeDirectIntakeRequest(t, vault, req)
	if err := waveV7CreateCmd(Args{"vault": vault, "file": path, "quiet": "true"}); err == nil || !strings.Contains(err.Error(), "owner is required") {
		t.Fatalf("authoring accepted incomplete human action: %v", err)
	}
}

func TestTaskAuthoringIntakeRejectsModelProfilesAndUnreasonedReviewOverride(t *testing.T) {
	tests := []struct {
		name string
		edit func(map[string]any)
		want string
	}{
		{name: "model profile", edit: func(task map[string]any) { task["execute_profile"] = "execute-standard" }, want: "invalid wave authoring request"},
		{name: "review override without reason", edit: func(task map[string]any) { task["review_level"] = "demanding" }, want: "review_level override requires review_reason"},
		{name: "reason without review override", edit: func(task map[string]any) { task["review_reason"] = "Security-sensitive review is required." }, want: "review_reason requires an explicit review_level override"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vault := v7DirectTestVault(t)
			task := directIntakeTask("only", "standard")
			tt.edit(task)
			path := writeDirectIntakeRequest(t, vault, directIntakeRequest([]map[string]any{task}))
			if err := waveV7CreateCmd(Args{"vault": vault, "file": path, "quiet": "true"}); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("authoring error=%v, want %q", err, tt.want)
			}
		})
	}
}

func TestTaskAuthoringIntakeAcceptsRedundantEqualReviewLevel(t *testing.T) {
	vault := v7DirectTestVault(t)
	task := directIntakeTask("only", "standard")
	task["review_level"] = "standard"
	path := writeDirectIntakeRequest(t, vault, directIntakeRequest([]map[string]any{task}))
	if err := waveV7CreateCmd(Args{"vault": vault, "file": path, "quiet": "true"}); err != nil {
		t.Fatalf("authoring rejected equal review and work levels: %v", err)
	}
}
