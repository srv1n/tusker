package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestTrustContextReuse(t *testing.T) {
	vault := v7DispatchTestVault(t)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Stable context", "domains": "project", "v7": "true"}, newV7Task)
	task := mustV7Task(t, vault, "APP-T-0001")
	task.Data = cloneMap(task.Data)
	task.Data["source_sha"] = "sha256:" + strings.Repeat("d", 64)
	idx := mustIndex(t, vault)

	// A restarted reader reconstructs the same packet from durable task and
	// route revisions. Stable context must not grow or drift between reads.
	cold := v7Packet(vault, task, idx, "agent")
	warm := v7Packet(vault, task, mustIndex(t, vault), "agent")
	if cold != warm {
		t.Fatalf("unchanged packet context drifted across reads\ncold:\n%s\nwarm:\n%s", cold, warm)
	}
	if !strings.Contains(cold, strings.TrimSpace(task.Body)) {
		t.Fatal("stable packet omitted the complete task contract")
	}

	// A material task edit changes the durable revision and must force the
	// complete contract back into the next packet rather than an unexplained
	// delta.
	edited := task
	edited.Data = cloneMap(task.Data)
	edited.Data["state_rev"] = "sha256:edited-context"
	edited.Body += "\n## Context refresh\n\nThe edited requirement must be visible after reuse.\n"
	refreshed := v7Packet(vault, edited, idx, "agent")
	if refreshed == cold || !strings.Contains(refreshed, "The edited requirement must be visible after reuse.") {
		t.Fatal("material task edit did not refresh complete packet context")
	}

	workspace := t.TempDir()
	project := RegisteredProject{
		ProjectID: "project-1", ProjectKey: "project-1", Name: "Trust project",
		RepoRoot: filepath.Dir(vault), VaultRoot: vault,
	}
	workflow := WorkflowFile{Path: filepath.Join(vault, "WORKFLOW.md"), Body: "Continue {{ note.id }} in {{ workspace.path }}."}
	workRevision := intField(task.Data, "work_revision")
	run := RunStatus{
		ProjectID: project.ProjectID, RecordID: "APP-T-0001", ItemID: "APP-T-0001",
		Runner: string(RunnerCodexExec), RunnerProfile: "execute-standard", RunnerHarness: string(RunnerCodexExec),
		RunnerModel: "gpt-5.x", RunnerEffort: "medium", WorkerPolicyFP: "sha256:" + strings.Repeat("a", 64),
		ExecutePolicyFP: "sha256:" + strings.Repeat("b", 64), Lane: runLaneExecute,
		WorkRevision: workRevision, WorkspacePath: workspace, ActiveAttemptID: "attempt-new", AttemptCount: 2,
	}
	previousRun := run
	previousRun.ActiveAttemptID = "attempt-old"
	previousRun.AttemptCount = 1
	previousRun.SessionRef = "native-session"
	previousRun.AttemptOutcome = string(AttemptOutcomeFailed)
	previousRun.LastError = "previous attempt stopped on a recoverable provider error"
	fullPrompt, err := renderAttemptPrompt(project, workflow, task, workspace, 2, run.ActiveAttemptID, runLaneExecute, run, previousRun, nil)
	if err != nil {
		t.Fatal(err)
	}
	if fingerprint := resumeContextFingerprintFromPrompt(fullPrompt); fingerprint == "" {
		t.Fatalf("full prompt did not persist a valid resume context fingerprint:\n%s", fullPrompt)
	}
	compactPrompt, err := renderAttemptPromptForResume(project, workflow, task, workspace, 2, run.ActiveAttemptID, runLaneExecute, run, previousRun, nil, resolvedResumeSession{
		SessionRef: "native-session", MessageRef: "native-message", DecisionKind: string(SupervisorDecisionResumeSession),
		Reason: "resuming compatible stored session", ParentAttemptID: previousRun.ActiveAttemptID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(compactPrompt) >= len(fullPrompt) {
		t.Fatalf("verified resume prompt did not shrink: full=%d resumed=%d bytes", len(fullPrompt), len(compactPrompt))
	}
	if !strings.Contains(compactPrompt, "## Tusker Verified Native Resume") ||
		!strings.Contains(compactPrompt, "Task state revision") ||
		!strings.Contains(compactPrompt, "Runner adapter/harness") ||
		!strings.Contains(compactPrompt, "Current error context") {
		t.Fatalf("resume prompt omitted current identity or error delta:\n%s", compactPrompt)
	}
	if strings.Contains(compactPrompt, strings.TrimSpace(task.Body)) || strings.Contains(compactPrompt, "## Task Packet") {
		t.Fatalf("resume prompt resent the complete task packet:\n%s", compactPrompt)
	}

	// An empty resolved session is the same safe branch used after an
	// invalidation or a lost provider session: retain the complete contract.
	fallbackPrompt, err := renderAttemptPromptForResume(project, workflow, task, workspace, 2, run.ActiveAttemptID, runLaneExecute, run, previousRun, nil, resolvedResumeSession{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(fallbackPrompt, strings.TrimSpace(task.Body)) || !strings.Contains(fallbackPrompt, "## Task Packet") {
		t.Fatal("full fallback omitted the complete task contract")
	}
	missingIdentity := run
	missingIdentity.RunnerModel = ""
	missingContextPrompt, err := renderAttemptPromptForResume(project, workflow, task, workspace, 2, run.ActiveAttemptID, runLaneExecute, missingIdentity, previousRun, nil, resolvedResumeSession{SessionRef: "native-session", Reason: "resuming compatible stored session"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(missingContextPrompt, strings.TrimSpace(task.Body)) || !strings.Contains(missingContextPrompt, "## Task Packet") {
		t.Fatal("missing resume identity did not retain the full fallback contract")
	}

	session := &RunnerSession{
		ProjectID:     project.ProjectID,
		RecordID:      run.RecordID,
		Runner:        run.Runner,
		WorkspacePath: workspace,
		WorkRevision:  run.WorkRevision,
		Resumable:     true,
	}
	if reason := incompatibleResumeSessionReason(project, run, session); reason != "" {
		t.Fatalf("matching durable session was rejected: %s", reason)
	}

	invalidations := []struct {
		name   string
		mutate func(*RunStatus, *RunnerSession)
	}{
		{name: "project identity", mutate: func(run *RunStatus, _ *RunnerSession) { run.ProjectID = "other-project" }},
		{name: "record identity", mutate: func(run *RunStatus, _ *RunnerSession) { run.RecordID = "other-record" }},
		{name: "runner identity", mutate: func(run *RunStatus, _ *RunnerSession) { run.Runner = string(RunnerClaude) }},
		{name: "task revision", mutate: func(run *RunStatus, _ *RunnerSession) { run.WorkRevision++ }},
		{name: "workspace", mutate: func(_ *RunStatus, session *RunnerSession) { session.WorkspacePath = filepath.Join(workspace, "moved") }},
		{name: "lost resumability", mutate: func(_ *RunStatus, session *RunnerSession) { session.Resumable = false }},
	}
	for _, tc := range invalidations {
		t.Run(tc.name, func(t *testing.T) {
			candidateRun := run
			candidateSession := *session
			tc.mutate(&candidateRun, &candidateSession)
			if reason := incompatibleResumeSessionReason(project, candidateRun, &candidateSession); reason == "" {
				t.Fatal("incompatible session was accepted for reuse")
			}
			fallbackPrompt, err := renderAttemptPromptForResume(project, workflow, task, candidateRun.WorkspacePath, 2, candidateRun.ActiveAttemptID, runLaneExecute, candidateRun, previousRun, nil, resolvedResumeSession{})
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(fallbackPrompt, strings.TrimSpace(task.Body)) || !strings.Contains(fallbackPrompt, "## Task Packet") {
				t.Fatalf("%s invalidation did not retain the full fallback contract", tc.name)
			}
		})
	}
	if reason := incompatibleResumeSessionReason(project, run, nil); reason == "" {
		t.Fatal("lost session was accepted for reuse")
	}

	// Exercise the real resolver against the durable attempt prompt and session
	// rows. The compact branch is accepted only when the current full prompt has
	// the exact same stable context as the prompt that established the session.
	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	previousPromptPath := filepath.Join(t.TempDir(), "previous.prompt.md")
	currentPromptPath := filepath.Join(t.TempDir(), "current.prompt.md")
	if err := writeText(previousPromptPath, fullPrompt); err != nil {
		t.Fatal(err)
	}
	if err := writeText(currentPromptPath, fullPrompt); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveAttempt(RunAttempt{
		AttemptID: previousRun.ActiveAttemptID, ProjectID: project.ProjectID, RecordID: run.RecordID,
		ItemID: run.ItemID, Runner: run.Runner, Lane: run.Lane, WorkerPolicyFP: run.WorkerPolicyFP,
		WorkRevision: run.WorkRevision, WorkspacePath: workspace, SessionRef: previousRun.SessionRef,
		PromptPath: previousPromptPath,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveSession(RunnerSession{
		ProjectID: project.ProjectID, RecordID: run.RecordID, Runner: run.Runner,
		SessionRef: previousRun.SessionRef, LastMessageRef: "native-message", WorkspacePath: workspace,
		CurrentItemID: run.ItemID, WorkRevision: run.WorkRevision, LastAttemptID: previousRun.ActiveAttemptID,
		State: "open", Resumable: true,
	}); err != nil {
		t.Fatal(err)
	}
	d := &Daemon{store: store}
	resolverRun := run
	resolverRun.SessionRef = previousRun.SessionRef
	resolverRun.PromptPath = currentPromptPath
	resolved, err := d.resolveResumeSession(project, task, resolverRun)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.SessionRef != previousRun.SessionRef {
		t.Fatalf("unchanged durable context was not accepted for native resume: %#v", resolved)
	}

	assertResolverRejects := func(name string, candidateProject RegisteredProject, candidateWorkflow WorkflowFile, candidateTask Note, candidateRun RunStatus) {
		t.Helper()
		candidatePrompt, renderErr := renderAttemptPrompt(candidateProject, candidateWorkflow, candidateTask, workspace, 2, run.ActiveAttemptID, runLaneExecute, candidateRun, previousRun, nil)
		if renderErr != nil {
			t.Fatalf("%s render: %v", name, renderErr)
		}
		if writeErr := writeText(currentPromptPath, candidatePrompt); writeErr != nil {
			t.Fatalf("%s prompt: %v", name, writeErr)
		}
		candidateRun.SessionRef = previousRun.SessionRef
		candidateRun.PromptPath = currentPromptPath
		got, resolveErr := d.resolveResumeSession(candidateProject, candidateTask, candidateRun)
		if resolveErr != nil {
			t.Fatalf("%s resolve: %v", name, resolveErr)
		}
		if got.SessionRef != "" {
			t.Fatalf("%s drift was accepted for native resume: %#v", name, got)
		}
	}
	bodyDrift := task
	bodyDrift.Body += "\nBody drift must force a complete prompt.\n"
	assertResolverRejects("task body", project, workflow, bodyDrift, run)
	workflowDrift := workflow
	workflowDrift.Body += "\nWorkflow drift must force a complete prompt.\n"
	assertResolverRejects("workflow body", project, workflowDrift, task, run)
	harnessDrift := run
	harnessDrift.RunnerHarness = "changed-harness"
	assertResolverRejects("runner harness", project, workflow, task, harnessDrift)
	profileDrift := run
	profileDrift.RunnerProfile = "changed-profile"
	assertResolverRejects("runner profile", project, workflow, task, profileDrift)
	modelDrift := run
	modelDrift.RunnerModel = "changed-model"
	assertResolverRejects("runner model", project, workflow, task, modelDrift)
	effortDrift := run
	effortDrift.RunnerEffort = "low"
	assertResolverRejects("runner effort", project, workflow, task, effortDrift)
	policyDrift := run
	policyDrift.WorkerPolicyFP = "sha256:" + strings.Repeat("c", 64)
	assertResolverRejects("worker policy", project, workflow, task, policyDrift)
	if err := writeText(currentPromptPath, fullPrompt); err != nil {
		t.Fatal(err)
	}
	resolverRun.PromptPath = currentPromptPath
	resolved, err = d.resolveResumeSession(project, task, resolverRun)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.SessionRef != previousRun.SessionRef {
		t.Fatalf("restored durable context was not accepted for native resume: %#v", resolved)
	}
	if err := writeText(currentPromptPath, "full prompt without a persisted context marker\n"); err != nil {
		t.Fatal(err)
	}
	resolved, err = d.resolveResumeSession(project, task, resolverRun)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.SessionRef != "" {
		t.Fatalf("missing current marker was accepted for native resume: %#v", resolved)
	}
}
