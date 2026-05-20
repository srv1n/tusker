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

	prompt, err := renderAttemptPrompt(project, wfFile, note, "/workspace/path", 3, "attempt-123", runLaneExecute)
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

	prompt, err := renderAttemptPrompt(project, wfFile, note, "/workspace/path", 4, "attempt-review", runLaneReview)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"independent Tusker reviewer",
		"ID: MEM-T-0001",
		"Auto-close allowed: yes",
		"Human close required: no",
		"tusker close MEM-T-0001 --by agent-reviewer",
	} {
		if !strings.Contains(prompt, expected) {
			t.Fatalf("expected reviewer prompt to contain %q, got:\n%s", expected, prompt)
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

	prompt, err := renderAttemptPrompt(project, wfFile, note, "/workspace/path", 4, "attempt-review", runLaneReview)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt, "tusker close APP-T-0001 --by reviewer:agent") {
		t.Fatalf("expected V7 reviewer prompt to use reviewer:agent, got:\n%s", prompt)
	}
	if !strings.Contains(prompt, "tusker verify add APP-T-0001 --by reviewer:agent --covers A1,A2") || strings.Contains(prompt, "tusker verify APP-T-0001") {
		t.Fatalf("expected V7 reviewer prompt to use verify add, got:\n%s", prompt)
	}
}
