package main

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// adoptFixture builds the Devin-style environment the recovery action exists
// for: a registered checkout that is a real git repository, holding a task
// whose work already exists on disk, with no implementation claim and no
// native conversation ID anywhere in the environment.
func adoptFixture(t *testing.T, taskID string, extra map[string]any) (vault string, store *RuntimeStore, project RegisteredProject) {
	t.Helper()
	// Devin sessions have no native conversation id; adoption must not
	// require one. Clear every host marker the authoring context reads.
	for _, key := range []string{
		"CODEX_SESSION_ID", "CODEX_THREAD_ID", "CLAUDE_SESSION_ID",
		"CLAUDE_CODE_SESSION_ID", "CLAUDECODE", "CLAUDE_CODE_ENTRYPOINT",
		"CHISEL_SESSION_DB", "TUSKER_ATTEMPT_ID",
	} {
		t.Setenv(key, "")
	}
	vault, store, project = authorityFixture(t)
	repo := v7RepoRoot(vault)
	runGitDir(t, repo, "init", "-q")
	runGitDir(t, repo, "config", "user.email", "test@example.com")
	runGitDir(t, repo, "config", "user.name", "Adopt Test")
	if err := writeText(filepath.Join(repo, "submitted.txt"), "already-implemented\n"); err != nil {
		t.Fatal(err)
	}
	runGitDir(t, repo, "add", "submitted.txt")
	runGitDir(t, repo, "commit", "-m", "implementation already present")
	head, _ := gitRevParse(repo, "HEAD")

	fields := map[string]any{
		"status": "ready", "readiness": "ready",
		"source_sha": head, "work_revision": 1,
		"owned_paths": []string{"submitted.txt"},
	}
	for k, v := range extra {
		fields[k] = v
	}
	writeDirectTask(t, vault, taskID, stringField(fields, "wave"), fields)
	rewriteTaskFile(t, vault, taskID, func(data map[string]any, body string) (map[string]any, string) {
		body = directDispatchableTaskBody(taskID)
		rows := parseV7VerificationRows(body)
		rows[0].Check, rows[0].Result, rows[0].Notes = "command: test -f submitted.txt", "pending", ""
		data["proof_status"] = "pending"
		return data, replaceSection(body, "## Verification", renderV7VerificationTable(rows))
	})
	return vault, store, project
}

func adoptRun(t *testing.T, vault string, store *RuntimeStore, projectID, taskID, actor string) serveRecoveryResult {
	t.Helper()
	result, err := adoptCompletedWork(vault, store, projectID, taskID, actor, 3, time.Now().UTC())
	if err != nil {
		t.Fatalf("adoptCompletedWork(%s): %v", actor, err)
	}
	return result
}

func adoptStatus(t *testing.T, vault, taskID string) map[string]any {
	t.Helper()
	return walkthroughNote(t, vault, taskID).Data
}

func TestAdoptCompletedWorkObjectiveCloseDevinSession(t *testing.T) {
	vault, store, project := adoptFixture(t, "APP-T-0001", nil)

	result := adoptRun(t, vault, store, project.ProjectID, "APP-T-0001", "human:sarav")
	if !result.OK || !result.Admitted || result.Refused {
		t.Fatalf("unexpected result %+v", result)
	}
	if result.Action != "adopt_completed" || result.TaskID != "APP-T-0001" {
		t.Fatalf("unexpected result identity %+v", result)
	}
	if result.Lane != "" {
		t.Fatalf("objective close must leave lane empty, got %q", result.Lane)
	}
	if result.Reason != "Checks passed. The task is complete." {
		t.Fatalf("unexpected reason %q", result.Reason)
	}
	data := adoptStatus(t, vault, "APP-T-0001")
	if stringField(data, "status") != "done" {
		t.Fatalf("status = %q", stringField(data, "status"))
	}
	if stringField(data, "implementation_source") != "external/unknown" {
		t.Fatalf("implementation_source = %q", stringField(data, "implementation_source"))
	}
	if stringField(data, "adopted_by") != "human:sarav" || stringField(data, "adopted_at") == "" {
		t.Fatalf("adoption marker incomplete: %v", data)
	}
	// No fabricated provenance.
	for _, key := range []string{"provider", "conversation_id", "attempt_id", "implementer", "implementation_attempt"} {
		if _, ok := data[key]; ok {
			t.Fatalf("fabricated provenance field %s present", key)
		}
	}
	if parseV7VerificationRows(walkthroughNote(t, vault, "APP-T-0001").Body)[0].Result != "pass" {
		t.Fatal("verification row was not executed to pass")
	}
}

func TestAdoptCompletedWorkIndependentReviewQueued(t *testing.T) {
	vault, store, project := adoptFixture(t, "APP-T-0001", map[string]any{
		"proof_required": []any{"focused_test", "independent_review"},
	})

	result := adoptRun(t, vault, store, project.ProjectID, "APP-T-0001", "human:sarav")
	if !result.OK || !result.Admitted {
		t.Fatalf("unexpected result %+v", result)
	}
	if result.Lane != runLaneReview {
		t.Fatalf("lane = %q, want %q", result.Lane, runLaneReview)
	}
	if result.Reason != "Checks passed. Independent review is next." {
		t.Fatalf("unexpected reason %q", result.Reason)
	}
	data := adoptStatus(t, vault, "APP-T-0001")
	if stringField(data, "status") != "review" {
		t.Fatalf("status = %q, want review", stringField(data, "status"))
	}
	if stringField(data, "implementation_source") != "external/unknown" {
		t.Fatalf("implementation_source = %q", stringField(data, "implementation_source"))
	}
}

func TestAdoptCompletedWorkIdempotentReplay(t *testing.T) {
	vault, store, project := adoptFixture(t, "APP-T-0001", nil)

	first := adoptRun(t, vault, store, project.ProjectID, "APP-T-0001", "human:sarav")
	if !first.Admitted {
		t.Fatalf("first adoption must be admitted: %+v", first)
	}
	before := adoptStatus(t, vault, "APP-T-0001")

	second := adoptRun(t, vault, store, project.ProjectID, "APP-T-0001", "human:sarav")
	if !second.OK || second.Admitted || second.Refused {
		t.Fatalf("second adoption must be ok without admission: %+v", second)
	}
	if second.Reason != "Checks passed. The task is complete." {
		t.Fatalf("unexpected reason %q", second.Reason)
	}
	after := adoptStatus(t, vault, "APP-T-0001")
	if stringField(before, "state_rev") != stringField(after, "state_rev") {
		t.Fatal("idempotent replay wrote a new state_rev")
	}
	directive, err := store.RunDirective(project.ProjectID, "APP-T-0001")
	if err != nil {
		t.Fatal(err)
	}
	if directive != nil && directive.State == "queued" {
		t.Fatal("idempotent replay queued a duplicate directive")
	}
}

func TestAdoptCompletedWorkAuthorization(t *testing.T) {
	t.Run("inert agent refused", func(t *testing.T) {
		vault, store, project := adoptFixture(t, "APP-T-0001", nil)
		result := adoptRun(t, vault, store, project.ProjectID, "APP-T-0001", "agent:devin")
		if !result.Refused || result.OK {
			t.Fatalf("expected refusal, got %+v", result)
		}
		if stringField(adoptStatus(t, vault, "APP-T-0001"), "status") != "ready" {
			t.Fatal("refused adoption mutated status")
		}
	})
	t.Run("human allowed", func(t *testing.T) {
		vault, store, project := adoptFixture(t, "APP-T-0001", nil)
		result := adoptRun(t, vault, store, project.ProjectID, "APP-T-0001", "human:sarav")
		if !result.OK || result.Refused {
			t.Fatalf("expected success, got %+v", result)
		}
	})
	t.Run("wave armed agent allowed", func(t *testing.T) {
		vault, store, project := adoptFixture(t, "APP-T-0001", map[string]any{"wave": "W-0001"})
		writeDirectWave(t, vault, "W-0001", []string{"APP-T-0001"}, map[string]any{"status": "active"})
		idx, err := loadV7Index(vault)
		if err != nil {
			t.Fatal(err)
		}
		wave, err := resolveV7Note(vault, "W-0001", "wave")
		if err != nil {
			t.Fatal(err)
		}
		fingerprint, issues := waveMaterialFingerprint(vault, idx, wave)
		if len(issues) > 0 {
			t.Fatalf("wave material fingerprint issues: %#v", issues)
		}
		rewriteWaveBody(t, vault, "W-0001", func(data map[string]any, body string) (map[string]any, string) {
			data["authorization"] = "armed"
			data["authorization_fingerprint"] = fingerprint
			data["authorized_at"] = time.Now().UTC().Format(time.RFC3339)
			return data, body
		})
		result := adoptRun(t, vault, store, project.ProjectID, "APP-T-0001", "agent:devin")
		if !result.OK || result.Refused {
			t.Fatalf("expected success, got %+v", result)
		}
		if stringField(adoptStatus(t, vault, "APP-T-0001"), "status") != "done" {
			t.Fatalf("status = %q", stringField(adoptStatus(t, vault, "APP-T-0001"), "status"))
		}
	})
}

func TestAdoptCompletedWorkDriftAndUnsafeRefusals(t *testing.T) {
	t.Run("stale state_rev refuses atomically", func(t *testing.T) {
		vault, store, project := adoptFixture(t, "APP-T-0001", nil)
		taskPath := filepath.Join(vault, "work", "tasks", "APP-T-0001.md")
		data, body, err := parseFrontmatterMustRead(taskPath)
		if err != nil {
			t.Fatal(err)
		}
		data["state_rev"] = "sha256:bogus"
		content, err := serializeDocument(data, body, v7FrontmatterOrder["task"])
		if err != nil {
			t.Fatal(err)
		}
		if err := writeText(taskPath, content); err != nil {
			t.Fatal(err)
		}
		result := adoptRun(t, vault, store, project.ProjectID, "APP-T-0001", "human:sarav")
		if !result.Refused {
			t.Fatalf("expected refusal, got %+v", result)
		}
		after := adoptStatus(t, vault, "APP-T-0001")
		if stringField(after, "status") != "ready" || stringField(after, "implementation_source") != "" {
			t.Fatalf("refusal was not atomic: %v", after)
		}
	})
	t.Run("scope escaping repo refuses before checks", func(t *testing.T) {
		vault, store, project := adoptFixture(t, "APP-T-0001", nil)
		rewriteTaskFile(t, vault, "APP-T-0001", func(data map[string]any, body string) (map[string]any, string) {
			data["owned_paths"] = []string{"../escape.txt"}
			return data, body
		})
		result := adoptRun(t, vault, store, project.ProjectID, "APP-T-0001", "human:sarav")
		if !result.Refused {
			t.Fatalf("expected refusal, got %+v", result)
		}
		if stringField(adoptStatus(t, vault, "APP-T-0001"), "status") != "ready" {
			t.Fatal("unsafe scope mutated status")
		}
	})
	t.Run("unreadable scoped material refuses", func(t *testing.T) {
		vault, store, project := adoptFixture(t, "APP-T-0001", map[string]any{
			"owned_paths": []string{"missing/file.go"},
		})
		result := adoptRun(t, vault, store, project.ProjectID, "APP-T-0001", "human:sarav")
		if !result.Refused {
			t.Fatalf("expected refusal, got %+v", result)
		}
		if stringField(adoptStatus(t, vault, "APP-T-0001"), "status") != "ready" {
			t.Fatal("unreadable material mutated status")
		}
	})
}

func TestAdoptCompletedWorkFailedVerificationNoTransition(t *testing.T) {
	vault, store, project := adoptFixture(t, "APP-T-0001", nil)
	rewriteTaskFile(t, vault, "APP-T-0001", func(data map[string]any, body string) (map[string]any, string) {
		rows := parseV7VerificationRows(body)
		rows[0].Check, rows[0].Result, rows[0].Notes = "command: test -f does-not-exist.txt", "pending", ""
		data["proof_status"] = "pending"
		return data, replaceSection(body, "## Verification", renderV7VerificationTable(rows))
	})
	result := adoptRun(t, vault, store, project.ProjectID, "APP-T-0001", "human:sarav")
	if !result.Refused || result.OK || result.Admitted {
		t.Fatalf("expected refusal, got %+v", result)
	}
	data := adoptStatus(t, vault, "APP-T-0001")
	status := stringField(data, "status")
	if status == "review" || status == "done" {
		t.Fatalf("failed verification transitioned to %q", status)
	}
	if stringField(data, "implementation_source") != "" {
		t.Fatal("failed verification wrote adoption marker")
	}
}

func TestAdoptCompletedWorkPostVerificationGateRefusalLeavesNoMarker(t *testing.T) {
	vault, store, project := adoptFixture(t, "APP-T-0001", map[string]any{
		"proof_required": []any{"focused_test", "independent_review"},
	})
	adoptCompletedBeforeCommitHook = func() {
		adoptCompletedBeforeCommitHook = nil
		writeHumanGate(t, vault, "APP-G-0001", "APP-T-0001")
	}
	t.Cleanup(func() { adoptCompletedBeforeCommitHook = nil })

	result, err := adoptCompletedWork(vault, store, project.ProjectID, "APP-T-0001", "human:sarav", 3, time.Now().UTC())
	if err == nil && !result.Refused {
		t.Fatalf("expected post-verification refusal, got result=%+v err=%v", result, err)
	}
	data := adoptStatus(t, vault, "APP-T-0001")
	if stringField(data, "status") == "review" || stringField(data, "implementation_source") != "" {
		t.Fatalf("refused transition left partial adoption state: %v", data)
	}
}

func TestAdoptCompletedWorkPostVerificationDependencyDriftLeavesNoMarker(t *testing.T) {
	vault, store, project := adoptFixture(t, "APP-T-0001", map[string]any{"dependencies": []any{"APP-T-0002:hard"}})
	writeDirectTask(t, vault, "APP-T-0002", "", map[string]any{"status": "done", "readiness": "done"})
	adoptCompletedBeforeCommitHook = func() {
		adoptCompletedBeforeCommitHook = nil
		rewriteTaskFile(t, vault, "APP-T-0002", func(data map[string]any, body string) (map[string]any, string) {
			data["updated_by"] = "human:concurrent"
			return data, body
		})
	}
	t.Cleanup(func() { adoptCompletedBeforeCommitHook = nil })

	if result, err := adoptCompletedWork(vault, store, project.ProjectID, "APP-T-0001", "human:sarav", 3, time.Now().UTC()); err == nil && !result.Refused {
		t.Fatalf("expected dependency drift refusal, got result=%+v err=%v", result, err)
	}
	data := adoptStatus(t, vault, "APP-T-0001")
	if stringField(data, "status") == "done" || stringField(data, "implementation_source") != "" {
		t.Fatalf("dependency drift left partial adoption state: %v", data)
	}
}

func TestAdoptCompletedWorkMaterialMutationAtCommitRollsBack(t *testing.T) {
	vault, store, project := adoptFixture(t, "APP-T-0001", nil)
	adoptCompletedBeforeWriteHook = func() {
		adoptCompletedBeforeWriteHook = nil
		if err := writeText(filepath.Join(v7RepoRoot(vault), "submitted.txt"), "concurrent mutation\n"); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { adoptCompletedBeforeWriteHook = nil })

	if result, err := adoptCompletedWork(vault, store, project.ProjectID, "APP-T-0001", "human:sarav", 3, time.Now().UTC()); err == nil && !result.Refused {
		t.Fatalf("expected material drift refusal, got result=%+v err=%v", result, err)
	}
	data := adoptStatus(t, vault, "APP-T-0001")
	if stringField(data, "status") == "done" || stringField(data, "implementation_source") != "" {
		t.Fatalf("material drift left partial adoption state: %v", data)
	}
}

func TestAdoptCompletedWorkReviewProvenanceFence(t *testing.T) {
	vault, store, project := adoptFixture(t, "APP-T-0001", map[string]any{
		"proof_required": []any{"focused_test", "independent_review"},
	})
	result := adoptRun(t, vault, store, project.ProjectID, "APP-T-0001", "human:sarav")
	if !result.OK {
		t.Fatalf("adoption failed: %+v", result)
	}
	note, err := resolveV7Note(vault, "APP-T-0001", "task")
	if err != nil {
		t.Fatal(err)
	}

	// Unknown reviewer identity (no native conversation, no dispatch
	// attempt): binding must refuse rather than present external/unknown
	// provenance as proven independence.
	if _, err := reviewImplementationBinding(store, RunStatus{}, note, vault); err == nil {
		t.Fatal("unknown reviewer claimed independence on adopted work")
	} else if !strings.Contains(err.Error(), "external/unknown") {
		t.Fatalf("unexpected refusal: %v", err)
	}

	// Known reviewer identity binds truthfully: implementation provenance is
	// recorded as external/unknown and can never equal the reviewer actor.
	t.Setenv("CODEX_SESSION_ID", "conv-reviewer-1")
	binding, err := reviewImplementationBinding(store, RunStatus{}, note, vault)
	if err != nil {
		t.Fatalf("known reviewer binding failed: %v", err)
	}
	if binding.ImplementationActor != "external/unknown" {
		t.Fatalf("ImplementationActor = %q", binding.ImplementationActor)
	}
	if binding.ReviewerActor == "" || binding.ReviewerActor == binding.ImplementationActor {
		t.Fatalf("sentinel collided with reviewer actor: %+v", binding)
	}
	// A dispatched attempt identity qualifies only when durable runtime state
	// binds it to this project, task, and review lane.
	t.Setenv("CODEX_SESSION_ID", "")
	t.Setenv("TUSKER_ATTEMPT_ID", "attempt-reviewer-1")
	run := RunStatus{ProjectID: project.ProjectID, RecordID: "APP-T-0001", ItemID: "APP-T-0001", Lane: runLaneReview, LeaseState: string(LeaseStateClaimed), LeaseOwner: "reviewer:agent", LeaseGeneration: 1, LeaseExpiresAt: time.Now().UTC().Add(time.Hour).Format(time.RFC3339Nano), ActiveAttemptID: "attempt-reviewer-1"}
	if err := store.UpsertRun(run); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveAttempt(RunAttempt{AttemptID: "attempt-reviewer-1", ProjectID: project.ProjectID, RecordID: "APP-T-0001", ItemID: "APP-T-0001", Lane: runLaneReview}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveRunAuthorization(RunAuthorization{ProjectID: project.ProjectID, RecordID: "APP-T-0001", LeaseGeneration: 1, AttemptID: "attempt-reviewer-1", Source: "tusker_cli", Actor: "reviewer:agent"}); err != nil {
		t.Fatal(err)
	}
	if _, err := reviewImplementationBinding(store, run, note, vault); err != nil {
		t.Fatalf("dispatched reviewer binding failed: %v", err)
	}
	stale := run
	stale.ActiveAttemptID = "attempt-superseded"
	if _, err := reviewImplementationBinding(store, stale, note, vault); err == nil {
		t.Fatal("superseded ActiveAttemptID claimed adopted review")
	}
	released := run
	released.LeaseState = string(LeaseStateReleased)
	if _, err := reviewImplementationBinding(store, released, note, vault); err == nil {
		t.Fatal("released review lease claimed adopted review")
	}
	unauthorized := run
	unauthorized.ActiveAttemptID, unauthorized.LeaseGeneration = "attempt-no-auth", 2
	if err := store.UpsertRun(unauthorized); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveAttempt(RunAttempt{AttemptID: "attempt-no-auth", ProjectID: project.ProjectID, RecordID: "APP-T-0001", ItemID: "APP-T-0001", Lane: runLaneReview}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TUSKER_ATTEMPT_ID", "attempt-no-auth")
	if _, err := reviewImplementationBinding(store, unauthorized, note, vault); err == nil {
		t.Fatal("review attempt without current authorization claimed adopted review")
	}
	if err := store.UpsertRun(run); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, projectID, recordID, lane string
	}{
		{"wrong project", "other-project", "APP-T-0001", runLaneReview},
		{"wrong task", project.ProjectID, "APP-T-9999", runLaneReview},
		{"wrong lane", project.ProjectID, "APP-T-0001", runLaneExecute},
	} {
		t.Run(tc.name, func(t *testing.T) {
			attemptID := "attempt-" + strings.ReplaceAll(tc.name, " ", "-")
			if err := store.SaveAttempt(RunAttempt{AttemptID: attemptID, ProjectID: tc.projectID, RecordID: tc.recordID, ItemID: tc.recordID, Lane: tc.lane}); err != nil {
				t.Fatal(err)
			}
			t.Setenv("TUSKER_ATTEMPT_ID", attemptID)
			if _, err := reviewImplementationBinding(store, run, note, vault); err == nil {
				t.Fatal("unbound dispatched attempt claimed adopted review")
			}
		})
	}
}

func TestAdoptCompletedWorkClaimedLifecycleUnchanged(t *testing.T) {
	// A task in review that was NOT adopted still requires the strict
	// implementation-revision fence; nothing about rerun_checks/review
	// loosened for the ordinary claimed lifecycle.
	vault, store, project := adoptFixture(t, "APP-T-0001", nil)
	rewriteTaskFile(t, vault, "APP-T-0001", func(data map[string]any, body string) (map[string]any, string) {
		data["status"] = "review"
		data["work_revision"] = 0
		data["source_sha"] = ""
		return data, body
	})
	note, err := resolveV7Note(vault, "APP-T-0001", "task")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reviewImplementationBinding(store, RunStatus{}, note, vault); err == nil {
		t.Fatal("unadopted review task bound without implementation provenance")
	}
	result := adoptRun(t, vault, store, project.ProjectID, "APP-T-0001", "human:sarav")
	if !result.Refused {
		t.Fatalf("expected refusal on foreign review task, got %+v", result)
	}
}
