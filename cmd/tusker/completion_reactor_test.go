package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// This matrix stays intentionally small but exercises the authority boundary:
// legacy/disabled do not consume an immutable result, shadow persists only a
// plan, authoritative returns findings without a model, and an exact reviewed
// SHA can be staged and CASed into integration.
func TestDeterministicReviewCompletion(t *testing.T) {
	t.Run("mode compatibility", func(t *testing.T) {
		for _, tc := range []struct {
			mode      completionReactorMode
			wantPhase string
		}{
			{completionReactorModeLegacy, ""},
			{completionReactorModeDisabled, ""},
			{completionReactorModeShadow, completionPhasePlanned},
		} {
			t.Run(string(tc.mode), func(t *testing.T) {
				vault, project, daemon, result := completionReactorFixture(t, false)
				defer daemon.Close()
				if _, err := daemon.store.SaveReviewResult(result); err != nil {
					t.Fatal(err)
				}
				if err := daemon.reconcileReviewCompletion(project, Workflow{CompletionReactor: completionReactorModeProjection{Effective: string(tc.mode)}}); err != nil {
					t.Fatal(err)
				}
				note, err := resolveV7Note(vault, result.TaskID, "task")
				if err != nil {
					t.Fatal(err)
				}
				if tc.wantPhase == "" {
					if stringField(note.Data, "status") != "review" {
						t.Fatalf("%s mutated review task", tc.mode)
					}
					return
				}
				wave, _, ok := armedWaveForTask(vault, note)
				if !ok {
					t.Fatal("fixture lost wave")
				}
				base, _ := gitOutputTrim(project.RepoRoot, "rev-parse", "refs/heads/"+v7WaveIntegrationBranch(wave))
				transaction, err := daemon.store.CompletionTransaction(completionTransactionID(project.ProjectID, result, base))
				if err != nil || transaction == nil || transaction.Phase != tc.wantPhase {
					t.Fatalf("transaction=%#v err=%v", transaction, err)
				}
			})
		}
	})

	t.Run("changes requested handback is idempotent", func(t *testing.T) {
		vault, project, daemon, result := completionReactorFixture(t, false)
		defer daemon.Close()
		result.Verdict, result.Blocker, result.Findings = "changes_requested", "", []string{"fix the exact acceptance regression"}
		result.ResultRevision = reviewResultFingerprint(result)
		if _, err := daemon.store.SaveReviewResult(result); err != nil {
			t.Fatal(err)
		}
		wf := Workflow{CompletionReactor: completionReactorModeProjection{Effective: string(completionReactorModeAuthoritative)}}
		if err := daemon.reconcileReviewCompletion(project, wf); err != nil {
			t.Fatal(err)
		}
		if err := daemon.reconcileReviewCompletion(project, wf); err != nil {
			t.Fatal(err)
		}
		note, err := resolveV7Note(vault, result.TaskID, "task")
		if err != nil {
			t.Fatal(err)
		}
		if stringField(note.Data, "status") != "rework" || !strings.Contains(generatedReviewerFindingContent(note.Body), "fix the exact acceptance regression") {
			t.Fatalf("finding was not durably handed back: %#v", note.Data)
		}
	})

	t.Run("pass freezes exact sha and integration CAS", func(t *testing.T) {
		vault, project, daemon, result := completionReactorFixture(t, true)
		defer daemon.Close()
		if _, err := daemon.store.SaveReviewResult(result); err != nil {
			t.Fatal(err)
		}
		wf := Workflow{CompletionReactor: completionReactorModeProjection{Effective: string(completionReactorModeAuthoritative)}}
		if err := daemon.reconcileReviewCompletion(project, wf); err != nil {
			t.Fatal(err)
		}
		note, err := resolveV7Note(vault, result.TaskID, "task")
		if err != nil {
			t.Fatal(err)
		}
		wave, _, _ := armedWaveForTask(vault, note)
		integration := "refs/heads/" + v7WaveIntegrationBranch(wave)
		got, err := gitOutputTrim(project.RepoRoot, "rev-parse", integration)
		if err != nil {
			t.Fatal(err)
		}
		if !gitMergeBaseAncestor(project.RepoRoot, result.ImplementationSHA, got) {
			t.Fatalf("integration did not merge exact reviewed sha %s", result.ImplementationSHA)
		}
		data, ok, err := v7GitFrontmatterAtRef(project.RepoRoot, strings.TrimPrefix(integration, "refs/heads/"), filepath.ToSlash(filepath.Join(".tusker", "work", "tasks", result.TaskID+".md")))
		if err != nil || !ok || stringField(data, "status") != "done" {
			t.Fatalf("staged close missing data=%#v err=%v", data, err)
		}
		if err := daemon.reconcileReviewCompletion(project, wf); err != nil {
			t.Fatal(err)
		}
		if replay, _ := gitOutputTrim(project.RepoRoot, "rev-parse", integration); replay != got {
			t.Fatalf("replay moved integration %s -> %s", got, replay)
		}
	})
}

func completionReactorFixture(t *testing.T, exactSource bool) (string, RegisteredProject, *Daemon, ReviewResult) {
	t.Helper()
	repo, vault := newLandTestRepo(t, 1, "true")
	state := filepath.Join(t.TempDir(), "state")
	t.Setenv("TUSKER_STATE_ROOT", state)
	project := newRegisteredProject(repo, vault)
	daemon, err := NewDaemon(state)
	if err != nil {
		t.Fatal(err)
	}
	if exactSource {
		sha := commitLandBranch(t, repo, "source/APP-T-0001", "integration/W-0001", map[string]string{"reviewed.txt": "exact\n"})
		setAutomationV7TaskFields(t, vault, "APP-T-0001", map[string]any{"status": "review", "readiness": "waiting_on_review", "proof_status": "satisfied", "source_sha": sha, "work_revision": 1})
	} else {
		setAutomationV7TaskFields(t, vault, "APP-T-0001", map[string]any{"status": "review", "readiness": "waiting_on_review", "proof_status": "satisfied", "source_sha": "deadbeef", "work_revision": 1})
	}
	note, err := resolveV7Note(vault, "APP-T-0001", "task")
	if err != nil {
		t.Fatal(err)
	}
	proof, gates, err := reviewObjectiveSnapshots(vault, note)
	if err != nil {
		t.Fatal(err)
	}
	result := ReviewResult{Schema: reviewResultSchema, ProjectID: project.ProjectID, TaskID: "APP-T-0001", TaskStateRev: stringField(note.Data, "state_rev"), WorkRevision: 1, ImplementationSHA: stringField(note.Data, "source_sha"), AttemptID: "review-1", Actor: "reviewer:agent", Runner: "codex", RunnerProfile: "review", Covers: []string{"A1"}, ProofFingerprint: proof, GateFingerprint: gates, Verdict: "pass", Summary: "objective pass"}
	result.ResultRevision = reviewResultFingerprint(result)
	return vault, project, daemon, result
}
