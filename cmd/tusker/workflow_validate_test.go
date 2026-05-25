package main

import (
	"strings"
	"testing"
)

func TestValidateWorkflowRejectsUnknownPerStateCap(t *testing.T) {
	wfFile := WorkflowFile{Path: "WORKFLOW.md", Body: defaultWorkflowMarkdown()}
	wfFile.Data = defaultWorkflow()
	wfFile.Data.Agents.MaxConcurrentAgentsByState = map[string]int{"made_up": 1}
	err := validateWorkflowFile(wfFile)
	if err == nil || !strings.Contains(err.Error(), "unknown tracker state") {
		t.Fatalf("expected unknown tracker state error, got %v", err)
	}
}

func TestValidateWorkflowRejectsReviewerRiskOverlap(t *testing.T) {
	wfFile := WorkflowFile{Path: "WORKFLOW.md", Body: defaultWorkflowMarkdown()}
	wfFile.Data = defaultWorkflow()
	wfFile.Data.Reviewer.AutoCloseRisks = []string{"low", "high"}
	wfFile.Data.Reviewer.HumanRequiredRisks = []string{"high", "critical"}
	err := validateWorkflowFile(wfFile)
	if err == nil || !strings.Contains(err.Error(), "appears in both") {
		t.Fatalf("expected reviewer risk overlap error, got %v", err)
	}
}

func TestValidateWorkflowRejectsReviewerRunnerOutsideEnabledAgents(t *testing.T) {
	wfFile := WorkflowFile{Path: "WORKFLOW.md", Body: defaultWorkflowMarkdown()}
	wfFile.Data = defaultWorkflow()
	wfFile.Data.Reviewer.Runner = "opencode"
	err := validateWorkflowFile(wfFile)
	if err == nil || !strings.Contains(err.Error(), "reviewer.runner must be listed in agents.enabled") {
		t.Fatalf("expected reviewer runner enabled-agents error, got %v", err)
	}
}

func TestDefaultWorkflowEnablesRiskAwareReviewer(t *testing.T) {
	wf := defaultWorkflow()
	if !wf.Reviewer.Enabled {
		t.Fatal("expected reviewer policy to be enabled by default")
	}
	if !reviewerMayAutoCloseRisk(wf.Reviewer, "medium") {
		t.Fatal("expected medium risk to be reviewer auto-closeable")
	}
	if !reviewerRequiresHumanRisk(wf.Reviewer, "high") {
		t.Fatal("expected high risk to require human close")
	}
	if wf.Reviewer.Actor != "agent-reviewer" {
		t.Fatalf("expected runner-neutral reviewer actor, got %q", wf.Reviewer.Actor)
	}
	if !strings.Contains(wf.Reviewer.Prompt, "independent Tusker reviewer") {
		t.Fatalf("expected reviewer prompt to be substantive, got %q", wf.Reviewer.Prompt)
	}
}

func TestDefaultWorkflowPromptIncludesCommandBudget(t *testing.T) {
	body := defaultWorkflowMarkdown()
	for _, expected := range []string{
		"## Command budget",
		"path-scoped status/search",
		"build-lock/status commands",
		"command + PASS/FAIL",
		"do not paste raw transcripts",
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("default workflow missing %q:\n%s", expected, body)
		}
	}
}
