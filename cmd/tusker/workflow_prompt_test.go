package main

import (
	"path/filepath"
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

func TestRenderAttemptPromptExplainsFreshRecoverySession(t *testing.T) {
	project := RegisteredProject{ProjectID: "project-123", Name: "Memory"}
	wfFile := WorkflowFile{Path: "/vault/WORKFLOW.md", Body: "Original task {{ note.id }}"}
	note := Note{Data: map[string]any{"id": "MEM-T-0001"}}
	previous := RunStatus{LastError: "redriven by human:test: " + outcomeUnknownRecoveryReasonPrefix + "attempt-lost"}
	prompt, err := renderAttemptPrompt(project, wfFile, note, "/workspace", 2, "attempt-recovery", runLaneExecute, RunStatus{}, previous, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Original task MEM-T-0001", "fresh recovery session", "attempt-lost", "inspect the current task-owned material and Git diff", "repair or complete only what remains"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("recovery prompt missing %q:\n%s", want, prompt)
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
	project, wfFile, note := reviewerPromptFixture(t)

	prompt, err := renderAttemptPrompt(project, wfFile, note, "/workspace/path", 4, "attempt-review", runLaneReview, RunStatus{}, RunStatus{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"independent Tusker reviewer",
		"ID: APP-T-0001",
		"Risk alone does not justify a human gate",
		"tusker review submit APP-T-0001 --attempt attempt-review --task-rev",
		"--source-sha",
		"--work-rev",
		"--proof-fingerprint",
		"--gate-fingerprint",
		"--verdict pass|changes_requested|blocked",
		`--finding '[{"schema":"tusker.reviewer-finding/v1"`,
		"Set repair_scope to material",
		"set it to proof",
		`--closure '[{"schema":"tusker.reviewer-finding-closure/v1"`,
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

func TestGeneratedReviewerFindingArraySurvivesCLIParsing(t *testing.T) {
	material := "sha256:" + strings.Repeat("b", 64)
	next := workSessionReviewNext(
		RunStatus{ItemID: "APP-T-0001", ActiveAttemptID: "review-1", LeaseOwner: "reviewer:agent", WorkRevision: 2},
		Note{Data: map[string]any{"state_rev": "sha256:task", "source_sha": "abc123"}},
		workSessionPacket{ProofFingerprint: "sha256:proof", GateFingerprint: "sha256:gates", MaterialFingerprint: material},
	)
	prefix := "--finding '"
	start := strings.Index(next, prefix)
	if start < 0 {
		t.Fatalf("generated review command lacks structured finding syntax: %s", next)
	}
	start += len(prefix)
	end := strings.Index(next[start:], "'")
	if end < 0 {
		t.Fatalf("generated finding array is not shell quoted: %s", next)
	}
	raw := next[start : start+end]
	_, args := parseCLI([]string{"tusker", "review", "submit", "APP-T-0001", "--finding", raw})
	findings, err := parseReviewFindingArgs(args.String("finding"))
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 2 {
		t.Fatalf("generated CLI lost a finding: %#v", findings)
	}
	for _, finding := range findings {
		if _, err := parseReviewerFinding(finding); err != nil {
			t.Fatalf("generated finding does not satisfy the parser: %v\n%s", err, finding)
		}
	}
	if err := validateReviewResultFindings(ReviewResult{Verdict: "changes_requested", Findings: findings}, false); err != nil {
		t.Fatalf("generated finding array does not satisfy review validation: %v", err)
	}

	duplicateRaw := strings.Replace(raw, `"id":"F-002"`, `"id":"F-001"`, 1)
	duplicates, err := parseReviewFindingArgs(duplicateRaw)
	if err != nil || len(duplicates) != 2 {
		t.Fatalf("duplicate records disappeared during parsing: %#v err=%v", duplicates, err)
	}
	if err := validateReviewResultFindings(ReviewResult{Verdict: "changes_requested", Findings: duplicates}, false); err == nil || !strings.Contains(err.Error(), "duplicated") {
		t.Fatalf("duplicate finding ID was silently discarded: %v", err)
	}

	help := captureStdout(t, printReviewHelp)
	for _, required := range []string{reviewerFindingSchema, reviewerFindingClosureSchema, "Use one JSON array", "Never repeat --finding"} {
		if !strings.Contains(help, required) {
			t.Fatalf("review help lacks %q:\n%s", required, help)
		}
	}
}

func TestRenderAttemptPromptUsesV7ReviewerActorShape(t *testing.T) {
	project, wfFile, note := reviewerPromptFixture(t)

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

func reviewerPromptFixture(t *testing.T) (RegisteredProject, WorkflowFile, Note) {
	t.Helper()
	vault := automationTestVault(t)
	mustRunPickupTest(t, Args{
		"vault": vault, "quiet": "true", "epic": "APP", "title": "Add provider harness",
		"risk": "medium", "priority": "p0", "v7": "true",
	}, newV7Task)
	setAutomationV7TaskFields(t, vault, "APP-T-0001", map[string]any{
		"status":        "review",
		"source_sha":    "abc123",
		"work_revision": 2,
	})
	note, err := resolveV7Note(vault, "APP-T-0001", "task")
	if err != nil {
		t.Fatal(err)
	}
	workflow, err := loadWorkflow(vault)
	if err != nil {
		t.Fatal(err)
	}
	project := newRegisteredProject(filepath.Dir(vault), vault)
	return project, workflow, note
}
