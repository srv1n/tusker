package main

import (
	"strings"
	"testing"
)

func TestRenderAttemptPromptUsesWorkflowBodyTemplate(t *testing.T) {
	project := RegisteredProject{
		ProjectID:  "project-123",
		ProjectKey: "MEM",
		Name:       "Memory",
		RepoRoot:   "/repo/root",
		VaultRoot:  "/vault/root",
	}
	wfFile := WorkflowFile{
		Path: "/vault/root/WORKFLOW.md",
		Body: "Project {{ project.name }} ({{ project.key }}/{{ project.id }})\nWorkspace {{ workspace.path }}\nRepo {{ repo.root }}\nVault {{ vault.path }}\nWorkflow {{ workflow.path }}\nNote {{ note.id }} {{ note.record_id }} {{ note.title }} {{ note.status }} {{ note.type }}\nAttempt {{ attempt.number }} {{ attempt.id }}\n",
	}
	note := Note{Data: map[string]any{
		"id":        "MEM-T-0001",
		"record_id": "rec-1",
		"title":     "Add memory backend",
		"status":    "active",
		"type":      "task",
	}}

	prompt, err := renderAttemptPrompt(project, wfFile, note, "/workspace/path", 3, "attempt-123", runLaneExecute, RunStatus{}, RunStatus{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"Project Memory (MEM/project-123)",
		"Workspace /workspace/path",
		"Repo /repo/root",
		"Vault /vault/root",
		"Workflow /vault/root/WORKFLOW.md",
		"Note MEM-T-0001 rec-1 Add memory backend active task",
		"Attempt 3 attempt-123",
	} {
		if !strings.Contains(prompt, expected) {
			t.Fatalf("expected rendered prompt to contain %q, got:\n%s", expected, prompt)
		}
	}
}

func TestRenderAttemptPromptRejectsUnknownPlaceholder(t *testing.T) {
	_, err := renderAttemptPrompt(
		RegisteredProject{Name: "Memory"},
		WorkflowFile{Path: "/vault/WORKFLOW.md", Body: "Unknown {{ note.nope }}\n"},
		Note{Data: map[string]any{}},
		"/workspace",
		1,
		"attempt-1",
		runLaneExecute,
		RunStatus{},
		RunStatus{},
		nil,
	)
	if err == nil || !strings.Contains(err.Error(), "unknown placeholder") {
		t.Fatalf("expected unknown placeholder error, got %v", err)
	}
}

func TestRenderAttemptPromptUsesReviewerTemplateForReviewLane(t *testing.T) {
	project := RegisteredProject{
		ProjectID:  "project-123",
		ProjectKey: "MEM",
		Name:       "Memory",
		RepoRoot:   "/repo/root",
		VaultRoot:  "/vault/root",
	}
	wf := defaultWorkflow()
	wfFile := WorkflowFile{
		Path: "/vault/root/WORKFLOW.md",
		Body: "worker prompt {{ note.id }}",
		Data: wf,
	}
	note := Note{Data: map[string]any{
		"id":     "MEM-T-0001",
		"title":  "Add memory backend",
		"risk":   "medium",
		"status": "review",
		"type":   "task",
	}}

	prompt, err := renderAttemptPrompt(project, wfFile, note, "/workspace/path", 4, "attempt-review", runLaneReview, RunStatus{}, RunStatus{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"independent Tusker reviewer",
		"ID: MEM-T-0001",
		"Risk alone does not justify a human gate",
		"tusker review submit MEM-T-0001 --attempt attempt-review --task-rev",
		"--source-sha",
		"--work-rev",
		"--proof-fingerprint",
		"--gate-fingerprint",
		"--verdict pass|changes_requested|blocked",
	} {
		if !strings.Contains(prompt, expected) {
			t.Fatalf("expected reviewer prompt to contain %q, got:\n%s", expected, prompt)
		}
	}
	for _, forbidden := range []string{"auto-close", "tusker status", "tusker merge", "tusker land", "tusker close", "tusker rework", "git update-ref", "git checkout"} {
		if strings.Contains(strings.ToLower(prompt), forbidden) {
			t.Fatalf("reviewer prompt retained forbidden authority %q:\n%s", forbidden, prompt)
		}
	}
}

func TestRenderAttemptPromptUsesV7ReviewerActorShape(t *testing.T) {
	project := RegisteredProject{
		ProjectID:  "project-123",
		ProjectKey: "APP",
		Name:       "App",
		RepoRoot:   "/repo/root",
		VaultRoot:  "/vault/root",
	}
	wf := defaultWorkflow()
	wfFile := WorkflowFile{
		Path: "/vault/root/WORKFLOW.md",
		Body: "worker prompt {{ note.id }}",
		Data: wf,
	}
	note := Note{
		Data: map[string]any{
			"schema": "tusker.task/v7",
			"kind":   "task",
			"id":     "APP-T-0001",
			"title":  "Add provider harness",
			"risk":   "medium",
			"status": "review",
		},
		Body: "## Acceptance\n\n| ID | Outcome | Proof |\n|---|---|---|\n| A1 | First. | Review |\n| A2 | Second. | Review |\n",
	}

	prompt, err := renderAttemptPrompt(project, wfFile, note, "/workspace/path", 4, "attempt-review", runLaneReview, RunStatus{}, RunStatus{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt, "Reviewer actor: reviewer:agent") {
		t.Fatalf("expected V7 reviewer actor, got:\n%s", prompt)
	}
	if !strings.Contains(prompt, "tusker review submit APP-T-0001 --attempt attempt-review") {
		t.Fatalf("expected V7 reviewer prompt to use typed result submission, got:\n%s", prompt)
	}
	for _, forbidden := range []string{"tusker status", "tusker merge", "tusker land", "tusker close", "tusker rework", "git update-ref", "git checkout"} {
		if strings.Contains(strings.ToLower(prompt), forbidden) {
			t.Fatalf("V7 reviewer prompt retained forbidden authority %q:\n%s", forbidden, prompt)
		}
	}
}
