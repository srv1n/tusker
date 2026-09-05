package main

import "testing"

func TestResumeSessionProjectIdentity(t *testing.T) {
	for _, tc := range []struct {
		name                  string
		project, run, session string
		allowed               bool
	}{
		{"matching", "p", "p", "p", true},
		{"wrong run", "p", "other", "p", false},
		{"wrong session", "p", "p", "other", false},
		{"missing project", "", "p", "p", false},
		{"missing run", "p", "", "p", false},
		{"missing session", "p", "p", "", false},
		{"all missing", "", "", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			workspace := t.TempDir()
			project := RegisteredProject{ProjectID: tc.project}
			run := RunStatus{ProjectID: tc.run, RecordID: "T-1", Runner: string(RunnerCodexExec), WorkRevision: 1, WorkspacePath: workspace}
			session := &RunnerSession{ProjectID: tc.session, RecordID: run.RecordID, Runner: run.Runner, WorkRevision: 1, WorkspacePath: workspace, Resumable: true}
			reason := incompatibleResumeSessionReason(project, run, session)
			if (reason == "") != tc.allowed {
				t.Fatalf("allowed=%v, incompatibility=%q", tc.allowed, reason)
			}
		})
	}
}
