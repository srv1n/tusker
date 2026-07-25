package main

import (
	"context"
	"path/filepath"
	"testing"
)

func reviewResultCommandFixture(t *testing.T) (string, Args) {
	t.Helper()
	vault := automationTestVault(t)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Typed review", "risk": "low", "priority": "p0", "v7": "true"}, newV7Task)
	setAutomationV7TaskFields(t, vault, "APP-T-0001", map[string]any{"status": "review", "source_sha": "abc123", "work_revision": 2, "proof_status": "satisfied"})
	state := filepath.Join(t.TempDir(), "state")
	t.Setenv("TUSKER_STATE_ROOT", state)
	store, err := OpenRuntimeStore(state)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SaveAttempt(RunAttempt{AttemptID: "review-1", ProjectID: v7ProjectID(vault), RecordID: "APP-T-0001", ItemID: "APP-T-0001", Runner: "codex", Lane: runLaneReview, WorkRevision: 2}); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertRun(RunStatus{ProjectID: v7ProjectID(vault), RecordID: "APP-T-0001", ItemID: "APP-T-0001", Runner: "codex", RunnerProfile: "review-fixture", Lane: runLaneReview, LeaseState: string(LeaseStateRunning), ActiveAttemptID: "review-1", WorkRevision: 2}); err != nil {
		t.Fatal(err)
	}
	note, err := resolveV7Note(vault, "APP-T-0001", "task")
	if err != nil {
		t.Fatal(err)
	}
	proof, gates, err := reviewObjectiveSnapshots(vault, note)
	if err != nil {
		t.Fatal(err)
	}
	return vault, Args{"vault": vault, "id": "APP-T-0001", "attempt": "review-1", "by": "reviewer:agent", "verdict": "changes_requested", "summary": "actionable", "finding": "fix acceptance", "task-rev": stringField(note.Data, "state_rev"), "source-sha": "abc123", "work-rev": "2", "proof-fingerprint": proof, "gate-fingerprint": gates}
}

func cloneReviewArgs(base Args) Args {
	clone := Args{}
	for key, value := range base {
		clone[key] = value
	}
	return clone
}

func refreshReviewArgs(t *testing.T, vault string, args Args) Args {
	t.Helper()
	note, err := resolveV7Note(vault, "APP-T-0001", "task")
	if err != nil {
		t.Fatal(err)
	}
	proof, gates, err := reviewObjectiveSnapshots(vault, note)
	if err != nil {
		t.Fatal(err)
	}
	args["task-rev"] = stringField(note.Data, "state_rev")
	args["proof-fingerprint"] = proof
	args["gate-fingerprint"] = gates
	return args
}

func validStoredReviewResult() ReviewResult {
	return ReviewResult{
		Schema:            reviewResultSchema,
		ProjectID:         "app",
		TaskID:            "APP-T-0001",
		TaskStateRev:      "sha256:task",
		WorkRevision:      1,
		ImplementationSHA: "abc123",
		AttemptID:         "review-1",
		Actor:             "reviewer:agent",
		Runner:            "codex",
		RunnerProfile:     "review",
		Covers:            []string{"A1"},
		ProofFingerprint:  "sha256:proof",
		GateFingerprint:   "sha256:gates",
		Verdict:           "blocked",
		Blocker:           "infrastructure",
		Summary:           "runner unavailable",
	}
}

func TestReviewResultProtocolLegacyFindingMigration(t *testing.T) {
	note := Note{Data: map[string]any{"id": "APP-T-0001", "state_rev": "sha256:state", "work_revision": 2}, Body: "## Verification\n\n| Covers | Check | Result | Notes |\n|---|---|---|---|\n| A1 | test | fail | " + reviewerFindingRowMarker("review-1") + " actionable regression |\n"}
	result, ok := legacyReviewerFindingResult(note, "review-1")
	if !ok || result.TaskID != "APP-T-0001" || result.AttemptID != "review-1" || result.Verdict != "changes_requested" || len(result.Findings) != 1 {
		t.Fatalf("legacy result=%#v ok=%v", result, ok)
	}
}

func TestReviewResultProtocolStoreReplayAndConflict(t *testing.T) {
	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	result := validStoredReviewResult()
	replay, err := store.SaveReviewResult(result)
	if err != nil || replay {
		t.Fatalf("first save replay=%v err=%v", replay, err)
	}
	replay, err = store.SaveReviewResult(result)
	if err != nil || !replay {
		t.Fatalf("exact replay replay=%v err=%v", replay, err)
	}
	result.Verdict = "pass"
	if _, err := store.SaveReviewResult(result); err == nil {
		t.Fatal("conflicting second verdict accepted")
	}
	result = validStoredReviewResult()
	result.ResultRevision = "sha256:forged"
	if _, err := store.SaveReviewResult(result); err == nil {
		t.Fatal("forged stable revision accepted")
	}
}

func TestReviewResultProtocolCommandValidation(t *testing.T) {
	for name, mutate := range map[string]func(Args){
		"invalid changes": func(a Args) { a["finding"] = "" },
		"invalid blocked": func(a Args) { a["verdict"] = "blocked"; a["blocker"] = "" },
		"wrong actor":     func(a Args) { a["by"] = "agent:implementer" },
		"stale task":      func(a Args) { a["task-rev"] = "sha256:stale" },
		"stale source":    func(a Args) { a["source-sha"] = "stale" },
		"stale work":      func(a Args) { a["attempt"] = "missing" },
		"stale proof":     func(a Args) { a["proof-fingerprint"] = "sha256:stale" },
		"stale gate":      func(a Args) { a["gate-fingerprint"] = "sha256:stale" },
	} {
		t.Run(name, func(t *testing.T) {
			_, base := reviewResultCommandFixture(t)
			a := cloneReviewArgs(base)
			mutate(a)
			if err := reviewSubmitCmd(a); err == nil {
				t.Fatal("accepted invalid review result")
			}
		})
	}
}

func TestReviewResultProtocolCommandAcceptsExactReplay(t *testing.T) {
	_, base := reviewResultCommandFixture(t)
	if err := reviewSubmitCmd(base); err != nil {
		t.Fatalf("accepted result rejected: %v", err)
	}
	if err := reviewSubmitCmd(base); err != nil {
		t.Fatalf("exact replay rejected: %v", err)
	}
}

func TestReviewResultProtocolRejectsVerdictPayloadMixing(t *testing.T) {
	for name, mutate := range map[string]func(Args){
		"pass with finding":    func(a Args) { a["verdict"] = "pass"; a["covers"] = "A1" },
		"blocked with finding": func(a Args) { a["verdict"] = "blocked"; a["blocker"] = "machine" },
	} {
		t.Run(name, func(t *testing.T) {
			_, base := reviewResultCommandFixture(t)
			a := cloneReviewArgs(base)
			mutate(a)
			if err := reviewSubmitCmd(a); err == nil {
				t.Fatal("accepted verdict payload mixing")
			}
		})
	}
}

func TestReviewResultProtocolRequiresGenuineHumanBlocker(t *testing.T) {
	vault, base := reviewResultCommandFixture(t)
	blocked := cloneReviewArgs(base)
	blocked["verdict"] = "blocked"
	blocked["finding"] = ""
	blocked["blocker"] = "human"
	if err := reviewSubmitCmd(blocked); err == nil {
		t.Fatal("human block without an open human gate was accepted")
	}
	if err := newV7Gate(Args{"vault": vault, "quiet": "true", "blocks": "APP-T-0001", "kind": "signoff", "owner": "human:product", "action": "Review the subjective artifact.", "verification": "Product owner records acceptance.", "why-agent-cannot": "The approved contract reserves subjective acceptance for the product owner.", "covers": "A1"}); err != nil {
		t.Fatal(err)
	}
	if err := reviewSubmitCmd(refreshReviewArgs(t, vault, blocked)); err != nil {
		t.Fatalf("genuine human block rejected: %v", err)
	}
}

func TestReviewResultProtocolRejectsInactiveReviewAuthority(t *testing.T) {
	vault, base := reviewResultCommandFixture(t)
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.UpsertRun(RunStatus{ProjectID: v7ProjectID(vault), RecordID: "APP-T-0001", ItemID: "APP-T-0001", Runner: "codex", RunnerProfile: "review-fixture", Lane: runLaneReview, LeaseState: string(LeaseStateRunning), ActiveAttemptID: "review-other", WorkRevision: 2}); err != nil {
		t.Fatal(err)
	}
	if err := reviewSubmitCmd(base); err == nil {
		t.Fatal("inactive reviewer attempt was accepted")
	}
}

func TestReviewResultProtocolReviewerExitRetriesThenCaps(t *testing.T) {
	vault := automationTestVault(t)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Typed review exit", "risk": "low", "priority": "p0", "v7": "true"}, newV7Task)
	setAutomationV7TaskFields(t, vault, "APP-T-0001", map[string]any{"status": "review", "readiness": "waiting_on_review", "next_owner": "reviewer", "source_sha": "abc123", "work_revision": 2})
	project := registerAutomationTestProject(t, vault)
	statusPath := filepath.Join(t.TempDir(), "reviewer.status.json")
	if err := writeRunnerStatusFile(statusPath, 0); err != nil {
		t.Fatal(err)
	}
	daemon, err := NewDaemon(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer daemon.Close()
	wfFile, err := loadWorkflow(vault)
	if err != nil {
		t.Fatal(err)
	}
	run := RunStatus{ProjectID: project.ProjectID, RecordID: "APP-T-0001", ItemID: "APP-T-0001", Runner: "codex", RunnerProfile: "review-fixture", Lane: runLaneReview, LeaseState: string(LeaseStateRunning), ActiveAttemptID: "review-1", WorkRevision: 2, StatusPath: statusPath, WorkspacePath: t.TempDir(), AttemptCount: 1}
	updated, changed, err := daemon.reconcileRun(context.Background(), project, wfFile, run)
	if err != nil {
		t.Fatal(err)
	}
	if !changed || updated.LeaseState != string(LeaseStateReleased) || updated.AttemptOutcome != string(AttemptOutcomeFailed) {
		t.Fatalf("reviewer exit did not release a retryable failed run: %#v", updated)
	}
	attempts, err := daemon.store.ListAttemptsForRun(project.ProjectID, "APP-T-0001")
	if err != nil {
		t.Fatal(err)
	}
	note, err := resolveV7Note(vault, "APP-T-0001", "task")
	if err != nil {
		t.Fatal(err)
	}
	if !reviewDispatchAllowed(vault, note, wfFile.Data, updated, reviewerAttemptCount(attempts)) {
		t.Fatal("reviewer exit without a result must be retryable before the configured cap")
	}
	wfFile.Data.Reviewer.MaxCycles = 1
	if reviewDispatchAllowed(vault, note, wfFile.Data, updated, reviewerAttemptCount(attempts)) {
		t.Fatal("reviewer exit must park at the configured review cycle cap")
	}
}

func TestReviewResultProtocolSavedResultSuppressesDuplicateReviewDispatch(t *testing.T) {
	vault := automationTestVault(t)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Typed review duplicate", "risk": "low", "priority": "p0", "v7": "true"}, newV7Task)
	setAutomationV7TaskFields(t, vault, "APP-T-0001", map[string]any{"status": "review", "readiness": "waiting_on_review", "next_owner": "reviewer", "source_sha": "abc123", "work_revision": 2})
	project := registerAutomationTestProject(t, vault)
	daemon, err := NewDaemon(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer daemon.Close()
	result := validStoredReviewResult()
	result.ProjectID = project.ProjectID
	result.WorkRevision = 2
	result.AttemptID = "review-1"
	if _, err := daemon.store.SaveReviewResult(result); err != nil {
		t.Fatal(err)
	}
	if err := daemon.PollOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	run := latestRunForRecord(t, daemon.store, project.ProjectID, "APP-T-0001")
	if run.Lane != runLaneReview || run.AttemptOutcome != string(AttemptOutcomeSucceeded) || run.LeaseState != string(LeaseStateReleased) || run.ActiveAttemptID != "" {
		t.Fatalf("saved typed result did not suppress review redispatch: %#v", run)
	}
	if run.LastError != "typed review result recorded; awaiting review reactor" {
		t.Fatalf("unexpected result hold reason: %q", run.LastError)
	}
	attempts, err := daemon.store.ListAttemptsForRun(project.ProjectID, "APP-T-0001")
	if err != nil {
		t.Fatal(err)
	}
	if len(attempts) != 0 {
		t.Fatalf("saved typed result dispatched an unnecessary reviewer: %#v", attempts)
	}
}

func TestReviewResultProtocolObjectiveSnapshotsDrift(t *testing.T) {
	vault, _ := reviewResultCommandFixture(t)
	note, err := resolveV7Note(vault, "APP-T-0001", "task")
	if err != nil {
		t.Fatal(err)
	}
	proofA, gateA, err := reviewObjectiveSnapshots(vault, note)
	if err != nil {
		t.Fatal(err)
	}
	// A real verification-row mutation changes proof without inventing gate
	// drift; frontmatter proof_status is correctly recomputed and ignored.
	if _, err := upsertV7Verification(vault, "APP-T-0001", v7VerificationRow{CoverText: "A1", Check: "objective snapshot", Result: "pass", Notes: "new objective proof"}, "agent:test"); err != nil {
		t.Fatal(err)
	}
	note, _ = resolveV7Note(vault, "APP-T-0001", "task")
	proofB, gateB, err := reviewObjectiveSnapshots(vault, note)
	if err != nil {
		t.Fatal(err)
	}
	if proofA == proofB || gateA != gateB {
		t.Fatalf("proof drift snapshots=%q/%q gates=%q/%q", proofA, proofB, gateA, gateB)
	}
	if err := newV7Gate(Args{"vault": vault, "quiet": "true", "blocks": "APP-T-0001", "kind": "signoff", "owner": "human:product", "action": "Review the subjective artifact.", "verification": "Product owner records acceptance.", "why-agent-cannot": "The approved contract reserves subjective acceptance for the product owner.", "covers": "A1"}); err != nil {
		t.Fatal(err)
	}
	note, _ = resolveV7Note(vault, "APP-T-0001", "task")
	proofC, gateC, err := reviewObjectiveSnapshots(vault, note)
	if err != nil {
		t.Fatal(err)
	}
	if proofB != proofC || gateB == gateC {
		t.Fatalf("gate drift snapshots=%q/%q gates=%q/%q", proofB, proofC, gateB, gateC)
	}
}
