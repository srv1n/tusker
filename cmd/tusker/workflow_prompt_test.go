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
		Body: "Project {{ project.name }} ({{ project.key }}/{{ project.id }})\nWorkspace {{ workspace.path }}\nRepo {{ repo.root }}\nVault {{ vault.path }}\nWorkflow {{ workflow.path }}\nNote {{ note.id }} {{ note.record_id }} {{ note.title }} {{ note.status }} {{ note.type }}\nAttempt {{ attempt.number }}\n",
	}
	note := Note{Data: map[string]any{
		"id":        "MEM-S-0001",
		"record_id": "rec-1",
		"title":     "Add memory backend",
		"status":    "active",
		"type":      "story",
	}}

	prompt, err := renderAttemptPrompt(project, wfFile, note, "/workspace/path", 3)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"Project Memory (MEM/project-123)",
		"Workspace /workspace/path",
		"Repo /repo/root",
		"Vault /vault/root",
		"Workflow /vault/root/WORKFLOW.md",
		"Note MEM-S-0001 rec-1 Add memory backend active story",
		"Attempt 3",
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
	)
	if err == nil || !strings.Contains(err.Error(), "unknown placeholder") {
		t.Fatalf("expected unknown placeholder error, got %v", err)
	}
}
