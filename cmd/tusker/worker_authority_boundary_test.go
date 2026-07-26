package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkerEnvironmentStripsDaemonAuthority(t *testing.T) {
	env := workerEnvironment([]string{
		"PATH=/bin", "TUSKER_STATE_ROOT=/daemon", "TUSKER_FIXTURE_STATE_ROOT=/fixture",
		"TUSKER_DAEMON_TOKEN=secret", "TUSKER_COMPLETION_KEY=secret", "SAFE=value",
	})
	for _, forbidden := range []string{"TUSKER_STATE_ROOT=", "TUSKER_FIXTURE_STATE_ROOT=", "TUSKER_DAEMON_", "TUSKER_COMPLETION_"} {
		for _, entry := range env {
			if len(entry) >= len(forbidden) && entry[:len(forbidden)] == forbidden {
				t.Fatalf("worker inherited daemon authority %q", entry)
			}
		}
	}
}

func TestCompletionWorkerSafetyRejectsUnsafeProfiles(t *testing.T) {
	state, workspace := filepath.Join(t.TempDir(), "state"), filepath.Join(t.TempDir(), "workspace")
	unsafe := ResolvedRunnerProfile{Name: "unsafe", Definition: RunnerProfileDefinition{Harness: string(RunnerCodexExec), PermissionPreset: "danger-full-access", Sandbox: RunnerSandboxDefinition{Mode: "danger-full-access"}}}
	if err := completionWorkerSafety(state, workspace, unsafe); err == nil {
		t.Fatal("danger-full-access profile must be rejected")
	}
	implementation := ResolvedRunnerProfile{Name: "implementation-terra", Definition: RunnerProfileDefinition{Harness: string(RunnerCodexExec), Sandbox: RunnerSandboxDefinition{Mode: "workspace-write", Network: boolPtr(false)}}}
	if err := completionWorkerSafety(state, workspace, implementation); err != nil {
		t.Fatalf("codex_exec workspace-write must remain admissible: %v", err)
	}
	for _, profile := range []ResolvedRunnerProfile{
		{Name: "reviewer-terra", Definition: RunnerProfileDefinition{Harness: string(RunnerClaude), Sandbox: RunnerSandboxDefinition{Mode: "read-only", Network: boolPtr(false)}}},
		{Name: "codex-app", Definition: RunnerProfileDefinition{Harness: string(RunnerCodexAppServer), Sandbox: RunnerSandboxDefinition{Mode: "read-only", Network: boolPtr(false)}}},
		{Name: "codex-generic", Definition: RunnerProfileDefinition{Harness: string(RunnerCodex), Sandbox: RunnerSandboxDefinition{Mode: "read-only", Network: boolPtr(false)}}},
	} {
		if err := completionWorkerSafety(state, workspace, profile); err == nil {
			t.Fatalf("metadata-only sandbox on %s must not authorize completion", profile.Name)
		}
	}
	networked := implementation
	networked.Name = "networked"
	networked.Definition.Sandbox.Network = boolPtr(true)
	if err := completionWorkerSafety(state, workspace, networked); err == nil {
		t.Fatal("network-enabled worker must not authorize completion through localhost control surfaces")
	}
	if err := completionWorkerSafety(filepath.Join(workspace, "state"), workspace, ResolvedRunnerProfile{Name: "inside", Definition: RunnerProfileDefinition{Harness: string(RunnerCodexExec), Sandbox: RunnerSandboxDefinition{Mode: "workspace-write", Network: boolPtr(false)}}}); err == nil {
		t.Fatal("state root nested in a workspace must be rejected")
	}
}

func TestCompletionWorkerSafetyRejectsProfileShellInjection(t *testing.T) {
	profile := ResolvedRunnerProfile{Name: "reviewer", Source: configSourceProject, Definition: RunnerProfileDefinition{Harness: string(RunnerCodexExec), Sandbox: RunnerSandboxDefinition{Mode: "read-only", Network: boolPtr(false)}}}
	for _, command := range []string{
		"codex exec --sandbox read-only -; touch /tmp/pwned",
		"codex exec --sandbox read-only - > /tmp/pwned",
		"codex exec --sandbox read-only - $(touch /tmp/pwned)",
	} {
		if err := completionWorkerSafetyForLane(t.TempDir(), t.TempDir(), runLaneReview, command, profile); err == nil {
			t.Fatalf("shell-bearing command authorized completion: %q", command)
		}
	}
	profile.Definition.Command = "codex exec --json -"
	if err := completionWorkerSafetyForLane(t.TempDir(), t.TempDir(), runLaneReview, defaultCodexExecCommand(), profile); err == nil {
		t.Fatal("project-defined profile command authorized completion")
	}
	profile.Definition.Command = ""
	argv, err := completionAuthoritativeCodexExecArgv(defaultCodexExecCommand(), runLaneReview, profile)
	if err != nil {
		t.Fatalf("canonical authoritative argv rejected: %v", err)
	}
	if got := strings.Join(argv, "\x00"); !strings.Contains(got, "sandbox_mode=\"read-only\"") || !strings.Contains(got, "sandbox_workspace_write.network_access=false") {
		t.Fatalf("canonical argv did not mechanically enforce sandbox/network: %#v", argv)
	}
	got := strings.Join(argv, "\x00")
	for _, required := range []string{"--ignore-user-config", "--ignore-rules", "--disable\x00hooks", `projects.` + completionWorkspaceTrustKeyToken + `.trust_level="untrusted"`} {
		if !strings.Contains(got, required) {
			t.Fatalf("canonical argv did not isolate project launch policy %q: %#v", required, argv)
		}
	}
}

func TestCompletionCodexBindingUsesPhysicalExecutableWithoutLoginShell(t *testing.T) {
	root := t.TempDir()
	binDir := filepath.Join(root, "trusted-bin")
	repo := filepath.Join(root, "repo")
	workspace := filepath.Join(root, "workspace")
	if err := ensureDir(repo); err != nil {
		t.Fatal(err)
	}
	if err := ensureDir(workspace); err != nil {
		t.Fatal(err)
	}
	writeRunnerPreflightScript(t, binDir, "codex", "#!/bin/sh\nprintf 'codex-test 1.0\\n'\n")
	t.Setenv("PATH", binDir)
	t.Setenv(runnerPathPrefixEnv, "")
	previousLoginPath := runnerLoginShellPath
	runnerLoginShellPath = func() string {
		t.Fatal("authoritative binding sourced a login shell PATH")
		return ""
	}
	t.Cleanup(func() { runnerLoginShellPath = previousLoginPath })

	profile := ResolvedRunnerProfile{
		Name:   "reviewer",
		Source: configSourceProject,
		Definition: RunnerProfileDefinition{
			Harness: string(RunnerCodexExec),
			Sandbox: RunnerSandboxDefinition{Mode: "read-only", Network: boolPtr(false)},
		},
	}
	template, err := completionAuthoritativeCodexExecArgv(defaultCodexExecCommand(), runLaneReview, profile)
	if err != nil {
		t.Fatal(err)
	}
	argv, executableFP, searchPath, err := completionBindAuthoritativeCodexExec(defaultCodexExecCommand(), template, workspace, repo)
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(argv[0]) || filepath.Base(argv[0]) != "codex" {
		t.Fatalf("authoritative argv did not freeze the physical executable: %#v", argv)
	}
	if !v7CloseAuthorityDigest(executableFP, "sha256:") || strings.TrimSpace(searchPath) == "" {
		t.Fatalf("authoritative executable binding was incomplete: fp=%q path=%q", executableFP, searchPath)
	}
	joined := strings.Join(argv, "\x00")
	if strings.Contains(joined, completionWorkspaceTrustKeyToken) || !strings.Contains(joined, canonicalPath(workspace)) {
		t.Fatalf("workspace trust override was not materialized: %#v", argv)
	}
	if err := completionVerifyExecutableIdentity(argv[0], executableFP, searchPath); err != nil {
		t.Fatalf("fresh executable identity did not verify: %v", err)
	}
}

func TestReviewProposalRequiresCompleteSingleRawLogMarker(t *testing.T) {
	p := reviewProposal{Schema: reviewProposalSchema, AttemptID: "a"}
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, found, err := reviewProposalFromRawLog([]byte(reviewProposalMarker + string(raw))); err != nil || found {
		t.Fatalf("partial marker must not be harvested: found=%t err=%v", found, err)
	}
	if _, found, err := reviewProposalFromRawLog([]byte(reviewProposalMarker + string(raw) + "\n" + reviewProposalMarker + `{"schema":"other"}` + "\n")); err == nil || found {
		t.Fatalf("conflicting markers must be rejected: found=%t err=%v", found, err)
	}
	if _, found, err := scanReviewProposalLog(strings.NewReader(reviewProposalMarker + string(raw) + "\n" + reviewProposalMarker + `{"schema":"other"}`)); err == nil || found {
		t.Fatalf("unterminated trailing marker must reject the complete earlier marker: found=%t err=%v", found, err)
	}
	got, found, err := reviewProposalFromRawLog([]byte(reviewProposalMarker + string(raw) + "\n" + reviewProposalMarker + string(raw) + "\n"))
	if err != nil || !found || got.AttemptID != p.AttemptID {
		t.Fatalf("identical replay markers must be idempotent: %#v found=%t err=%v", got, found, err)
	}
}

func TestFrozenReviewProposalLogTreatsAbsenceAsNoProposalAndRejectsSymlink(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.raw.log")
	if proposal, present, err := readFrozenReviewProposalLog(missing); err != nil || present || proposal.Schema != "" {
		t.Fatalf("missing raw log must be no proposal: proposal=%#v present=%t err=%v", proposal, present, err)
	}
	path := filepath.Join(t.TempDir(), "attempt.raw.log")
	if err := os.Symlink(missing, path); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, _, err := readFrozenReviewProposalLog(path); err == nil {
		t.Fatal("symlinked raw log must be rejected")
	}
}

func TestNilStoreCannotAuthenticateCompletionAuthority(t *testing.T) {
	if verifyCompletionReceiptAuthorityWithStore(t.TempDir(), completionReceipt{}, completionGitTreeEntry{}, nil, true) {
		t.Fatal("caller environment must not select a completion trust store")
	}
}

func TestWorkerReviewSubmitEmitsProposalWithoutOpeningRuntimeStore(t *testing.T) {
	vault := automationTestVault(t)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Worker proposal", "risk": "low", "priority": "p0", "v7": "true"}, newV7Task)
	setAutomationV7TaskFields(t, vault, "APP-T-0001", map[string]any{"status": "review", "readiness": "waiting_on_review", "next_owner": "reviewer", "source_sha": "abc123", "work_revision": 2})
	note, err := resolveV7Note(vault, "APP-T-0001", "task")
	if err != nil {
		t.Fatal(err)
	}
	proof, gates, err := reviewObjectiveSnapshots(vault, note)
	if err != nil {
		t.Fatal(err)
	}
	wfFile, err := loadWorkflow(vault)
	if err != nil {
		t.Fatal(err)
	}
	stateRoot := filepath.Join(t.TempDir(), "worker-must-not-create-state")
	t.Setenv("TUSKER_STATE_ROOT", stateRoot)
	t.Setenv("TUSKER_ATTEMPT_ID", "review-worker")
	output := captureStdout(t, func() {
		err := reviewSubmitCmd(Args{
			"vault": vault, "id": "APP-T-0001", "attempt": "review-worker",
			"by":      reviewerActorForNote(wfFile.Data.Reviewer.Actor, note),
			"verdict": "changes_requested", "summary": "actionable", "finding": "fix acceptance",
			"task-rev": stringField(note.Data, "state_rev"), "source-sha": "abc123",
			"proof-fingerprint": proof, "gate-fingerprint": gates,
		})
		if err != nil {
			t.Fatal(err)
		}
	})
	if !strings.HasPrefix(output, reviewProposalMarker) {
		t.Fatalf("worker review submit did not emit the bounded proposal marker: %q", output)
	}
	if fileExists(runtimeStoreDBPath(stateRoot)) {
		t.Fatal("worker review submit opened or created the daemon runtime store")
	}
}

func TestReviewProposalDaemonLifecycleBoundary(t *testing.T) {
	t.Run("live marker is not durable", func(t *testing.T) {
		project, daemon, wfFile, run := reviewProposalDaemonFixture(t)
		defer daemon.Close()
		if _, _, err := daemon.reconcileRun(context.Background(), project, wfFile, run); err != nil {
			t.Fatal(err)
		}
		assertReviewResultCount(t, daemon.store, project.ProjectID, 0)
	})

	t.Run("zero exit saves exactly once", func(t *testing.T) {
		project, daemon, wfFile, run := reviewProposalDaemonFixture(t)
		defer daemon.Close()
		if err := writeRunnerStatusFile(run.StatusPath, 0); err != nil {
			t.Fatal(err)
		}
		updated, changed, err := daemon.reconcileRun(context.Background(), project, wfFile, run)
		if err != nil {
			t.Fatal(err)
		}
		if !changed || updated.LeaseState != string(LeaseStateReleased) || updated.AttemptOutcome != string(AttemptOutcomeSucceeded) {
			t.Fatalf("terminal proposal did not release a successful review run: %#v", updated)
		}
		assertReviewResultCount(t, daemon.store, project.ProjectID, 1)
		if _, _, err := daemon.reconcileRun(context.Background(), project, wfFile, run); err != nil {
			t.Fatal(err)
		}
		assertReviewResultCount(t, daemon.store, project.ProjectID, 1)
	})

	t.Run("nonzero exit never saves", func(t *testing.T) {
		project, daemon, wfFile, run := reviewProposalDaemonFixture(t)
		defer daemon.Close()
		if err := writeRunnerStatusFile(run.StatusPath, 7); err != nil {
			t.Fatal(err)
		}
		if _, _, err := daemon.reconcileRun(context.Background(), project, wfFile, run); err != nil {
			t.Fatal(err)
		}
		assertReviewResultCount(t, daemon.store, project.ProjectID, 0)
	})

	t.Run("conflicting markers park only this run", func(t *testing.T) {
		project, daemon, wfFile, run := reviewProposalDaemonFixture(t)
		defer daemon.Close()
		raw, err := readText(run.RawLogPath)
		if err != nil {
			t.Fatal(err)
		}
		var first reviewProposal
		payload := strings.TrimPrefix(strings.TrimSpace(raw), reviewProposalMarker)
		if err := json.Unmarshal([]byte(payload), &first); err != nil {
			t.Fatal(err)
		}
		second := first
		second.Result.Summary = "different authority"
		secondRaw, err := json.Marshal(second)
		if err != nil {
			t.Fatal(err)
		}
		if err := writeText(run.RawLogPath, raw+reviewProposalMarker+string(secondRaw)+"\n"); err != nil {
			t.Fatal(err)
		}
		if err := writeRunnerStatusFile(run.StatusPath, 0); err != nil {
			t.Fatal(err)
		}
		updated, changed, err := daemon.reconcileRun(context.Background(), project, wfFile, run)
		if err != nil {
			t.Fatal(err)
		}
		if !changed || updated.LeaseState != string(LeaseStateParkedNoProgress) || updated.AttemptOutcome != string(AttemptOutcomeBlocked) {
			t.Fatalf("conflicting proposal was not contained to a local park: %#v", updated)
		}
		assertReviewResultCount(t, daemon.store, project.ProjectID, 0)
	})
}

func reviewProposalDaemonFixture(t *testing.T) (RegisteredProject, *Daemon, WorkflowFile, RunStatus) {
	t.Helper()
	vault := automationTestVault(t)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Review proposal boundary", "risk": "low", "priority": "p0", "v7": "true"}, newV7Task)
	setAutomationV7TaskFields(t, vault, "APP-T-0001", map[string]any{"status": "review", "readiness": "waiting_on_review", "next_owner": "reviewer", "source_sha": "abc123", "work_revision": 2})
	project := registerAutomationTestProject(t, vault)
	configureCompletionWorkerProfilesForTest(t, vault)
	wfFile, err := loadWorkflow(vault)
	if err != nil {
		t.Fatal(err)
	}
	note, err := resolveV7Note(vault, "APP-T-0001", "task")
	if err != nil {
		t.Fatal(err)
	}
	executeProfile, _, executePolicyFP, err := completionLaneWorkerPolicy(wfFile.Data, note, runLaneExecute)
	if err != nil {
		t.Fatal(err)
	}
	reviewProfile, _, reviewPolicyFP, err := completionLaneWorkerPolicy(wfFile.Data, note, runLaneReview)
	if err != nil {
		t.Fatal(err)
	}
	if executeProfile.Name == reviewProfile.Name {
		t.Fatal("fixture must use independent execute and review profiles")
	}
	proof, gates, err := reviewObjectiveSnapshots(vault, note)
	if err != nil {
		t.Fatal(err)
	}
	daemon, err := NewDaemon(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	attemptID := "review-boundary"
	runDir := filepath.Join(daemon.stateRoot, "runs", project.ProjectKey, "APP-T-0001")
	if err := ensureDir(runDir); err != nil {
		daemon.Close()
		t.Fatal(err)
	}
	rawLogPath := filepath.Join(runDir, "review-boundary.raw.log")
	statusPath := filepath.Join(runDir, "review-boundary.status.json")
	run := RunStatus{
		ProjectID: project.ProjectID, RecordID: "APP-T-0001", ItemID: "APP-T-0001",
		Runner: string(RunnerCodexExec), RunnerProfile: reviewProfile.Name, RunnerHarness: string(RunnerCodexExec),
		WorkerPolicyFP: reviewPolicyFP, ExecutePolicyFP: executePolicyFP,
		Lane: runLaneReview, LeaseState: string(LeaseStateRunning), LeaseOwner: attemptID,
		ActiveAttemptID: attemptID, WorkRevision: 2, WorkspacePath: t.TempDir(),
		RawLogPath: rawLogPath, StatusPath: statusPath, AttemptCount: 1,
	}
	if err := daemon.store.SaveAttempt(RunAttempt{AttemptID: attemptID, ProjectID: project.ProjectID, RecordID: run.RecordID, ItemID: run.ItemID, Runner: run.Runner, Lane: runLaneReview, WorkerPolicyFP: reviewPolicyFP, WorkRevision: run.WorkRevision, WorkspacePath: run.WorkspacePath}); err != nil {
		daemon.Close()
		t.Fatal(err)
	}
	if err := daemon.store.UpsertRun(run); err != nil {
		daemon.Close()
		t.Fatal(err)
	}
	result := ReviewResult{
		Schema: reviewResultSchema, ProjectID: project.ProjectID, TaskID: run.RecordID,
		TaskStateRev: stringField(note.Data, "state_rev"), WorkRevision: run.WorkRevision,
		ImplementationSHA: "abc123", AttemptID: attemptID,
		Actor:  reviewerActorForNote(wfFile.Data.Reviewer.Actor, note),
		Covers: []string{}, ProofFingerprint: proof, GateFingerprint: gates,
		Verdict: "changes_requested", Summary: "actionable", Findings: []string{"fix acceptance"},
		CreatedAt: "2026-07-25T10:00:00Z",
	}
	raw, err := json.Marshal(reviewProposal{Schema: reviewProposalSchema, AttemptID: attemptID, Result: result})
	if err != nil {
		daemon.Close()
		t.Fatal(err)
	}
	if err := writeText(rawLogPath, reviewProposalMarker+string(raw)+"\n"); err != nil {
		daemon.Close()
		t.Fatal(err)
	}
	return project, daemon, wfFile, run
}

func configureCompletionWorkerProfilesForTest(t *testing.T, vault string) {
	t.Helper()
	profiles := map[string]any{
		"implementation-terra": map[string]any{
			"harness": "codex_exec", "model": "gpt-5.x", "effort": "medium", "permission_preset": "workspace-write-offline",
			"sandbox":   map[string]any{"mode": "workspace-write", "network": false},
			"subagents": map[string]any{"allowed": false, "max_concurrent": 0},
		},
		"reviewer-terra": map[string]any{
			"harness": "codex_exec", "model": "gpt-5.x", "effort": "high", "permission_preset": "read-only",
			"sandbox":   map[string]any{"mode": "read-only", "network": false},
			"subagents": map[string]any{"allowed": false, "max_concurrent": 0},
		},
	}
	for _, setting := range []struct {
		key   string
		value any
	}{
		{"automation.profiles", profiles},
		{"automation.default_profile", "implementation-terra"},
		{"automation.lane_profiles", map[string]any{runLaneExecute: "implementation-terra", runLaneReview: "reviewer-terra"}},
		{"automation.completion_reactor.mode", string(completionReactorModeAuthoritative)},
	} {
		if _, err := setProjectLocalConfigWithReadback(vault, setting.key, setting.value); err != nil {
			t.Fatal(err)
		}
	}
}

func assertReviewResultCount(t *testing.T, store *RuntimeStore, projectID string, want int) {
	t.Helper()
	rows, err := store.ListReviewResults(projectID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != want {
		t.Fatalf("review result count=%d want=%d rows=%#v", len(rows), want, rows)
	}
}
