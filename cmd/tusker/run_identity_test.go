package main

import (
	"strings"
	"testing"
)

func TestSessionMetadataWorkspaceIdentityResume(t *testing.T) {
	store, run := ownershipStoreFixture(t, "APP-T-IDENTITY")
	identity := RunIdentityMetadata{ProjectID: run.ProjectID, RecordID: run.RecordID, RepoRoot: "/registered/repo", WorkspacePath: "/physical/worktree", WorkspaceMode: "worktree", Runner: run.Runner, Branch: "task/APP-T-IDENTITY", Head: "abc123"}
	if err := store.SaveRunIdentity(identity); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveSession(RunnerSession{ProjectID: run.ProjectID, RecordID: run.RecordID, Runner: run.Runner, SessionRef: "session-1", WorkspacePath: identity.WorkspacePath, Resumable: true, State: "open"}); err != nil {
		t.Fatal(err)
	}
	for _, auth := range []RunAuthorization{
		{ProjectID: run.ProjectID, RecordID: run.RecordID, LeaseGeneration: 1, AttemptID: "attempt-worker", Source: "codex", Actor: "agent:worker", Trigger: "self_implementation;source=codex;conversation=worker;host=local"},
		{ProjectID: run.ProjectID, RecordID: run.RecordID, LeaseGeneration: 2, AttemptID: "attempt-reviewer", Source: "codex", Actor: "reviewer:agent", Trigger: "work_review;source=codex;conversation=reviewer;host=local"},
	} {
		if err := store.SaveRunAuthorization(auth); err != nil {
			t.Fatal(err)
		}
	}
	inspection, err := buildRunInspection(store, &run)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, identity.RepoRoot, inspection.Identity.RepoRoot, "registered repo")
	assertEqual(t, identity.WorkspacePath, inspection.Identity.WorkspacePath, "physical workspace")
	assertEqual(t, identity.WorkspaceMode, inspection.Identity.WorkspaceMode, "workspace mode")
	if !inspection.Resume.Supported || !strings.Contains(inspection.Resume.Command, "session-1") {
		t.Fatalf("resume: %#v", inspection.Resume)
	}
	if len(inspection.Authorizations) != 2 || inspection.Authorizations[0].LeaseGeneration != 1 || inspection.Authorizations[1].LeaseGeneration != 2 {
		t.Fatalf("authorization history: %#v", inspection.Authorizations)
	}
	assertEqual(t, "attempt-worker", inspection.Authorizations[0].AttemptID, "worker authorization attempt")
	assertEqual(t, "attempt-reviewer", inspection.Authorizations[1].AttemptID, "reviewer authorization attempt")
}

func TestCodexSessionIdentityIgnoresNestedSubagentAndMessageIDs(t *testing.T) {
	line := `{"type":"thread.started","thread_id":"operator-session","message":{"id":"message-id"},"subagent":{"session_id":"nested-session"}}`
	assertEqual(t, "operator-session", extractSessionRefFromJSON(line), "codex top-level session")
	assertEqual(t, "", extractSessionRefFromJSON(`{"type":"task_notification","payload":{"session_id":"notification-id"}}`), "nested notification ignored")
}

func TestClaudeSessionIdentityIgnoresNestedSubagentAndMessageIDs(t *testing.T) {
	line := `{"type":"assistant","session_id":"claude-operator","uuid":"message-uuid","subagent":{"session_id":"claude-child"}}`
	assertEqual(t, "claude-operator", extractSessionRefFromJSON(line), "claude top-level session")
	assertEqual(t, "", extractSessionRefFromJSON(`{"type":"assistant","message":{"id":"uuid-only"}}`), "message id ignored")
}

func TestResumeCommandEscapesSessionAndReportsUnsupported(t *testing.T) {
	session := &RunnerSession{SessionRef: "session with ' quote", Resumable: true}
	codex := resumeCapability(&RunStatus{Runner: string(RunnerCodexExec)}, session)
	assertEqual(t, "codex exec resume 'session with '\"'\"' quote'", codex.Command, "escaped codex resume")
	claude := resumeCapability(&RunStatus{Runner: string(RunnerClaude)}, session)
	assertEqual(t, "claude --resume 'session with '\"'\"' quote'", claude.Command, "escaped claude resume")
	unsupported := resumeCapability(&RunStatus{Runner: string(RunnerCodexCloud)}, session)
	if unsupported.Supported || unsupported.Reason == "" {
		t.Fatalf("unsupported resume: %#v", unsupported)
	}
	expired := resumeCapability(&RunStatus{Runner: string(RunnerCodexExec)}, &RunnerSession{SessionRef: "old", Resumable: false, LastError: "expired"})
	assertEqual(t, "expired", expired.Reason, "expired reason")
}
