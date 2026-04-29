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
