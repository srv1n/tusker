package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkRecoverContinuesFromValidChecksInVaultProjectOnly(t *testing.T) {
	vault, store, project := authorityFixture(t)
	writeDirectTask(t, vault, "APP-T-0001", "W-0001", map[string]any{
		"source_sha": "source-1", "work_revision": 7, "owned_paths": []string{"submitted.txt"},
	})
	writeDirectWave(t, vault, "W-0001", []string{"APP-T-0001"}, nil)
	rewriteTaskFile(t, vault, "APP-T-0001", func(data map[string]any, body string) (map[string]any, string) {
		body = directDispatchableTaskBody("APP-T-0001")
		rows := parseV7VerificationRows(body)
		rows[0].Check, rows[0].Result, rows[0].Notes = "command: test -f submitted.txt", "pending", ""
		data["proof_status"] = "pending"
		return data, replaceSection(body, "## Verification", renderV7VerificationTable(rows))
	})
	// Arm the wave over the amended task contract so the recovery path
	// exercises a current authorization rather than a stale one.
	armedIdx, err := loadV7Index(vault)
	if err != nil {
		t.Fatal(err)
	}
	armedWave, err := resolveV7Note(vault, "W-0001", "wave")
	if err != nil {
		t.Fatal(err)
	}
	armedFingerprint, armedIssues := waveMaterialFingerprint(vault, armedIdx, armedWave)
	if len(armedIssues) > 0 {
		t.Fatalf("wave material fingerprint issues: %#v", armedIssues)
	}
	rewriteWaveBody(t, vault, "W-0001", func(data map[string]any, body string) (map[string]any, string) {
		data["authorization"] = "armed"
		data["authorization_fingerprint"] = armedFingerprint
		data["authorized_at"] = "2026-01-02T00:00:00Z"
		return data, body
	})

	submitted := filepath.Join(t.TempDir(), "submitted")
	if err := ensureDir(submitted); err != nil {
		t.Fatal(err)
	}
	runGitDir(t, submitted, "init", "-q")
	runGitDir(t, submitted, "config", "user.email", "test@example.com")
	runGitDir(t, submitted, "config", "user.name", "Recovery Test")
	if err := writeText(filepath.Join(submitted, "submitted.txt"), "durable implementation\n"); err != nil {
		t.Fatal(err)
	}
	runGitDir(t, submitted, "add", "submitted.txt")
	runGitDir(t, submitted, "commit", "-m", "submitted implementation")
	material, err := workspaceTreeStateHashForPaths(submitted, []string{"submitted.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveAttempt(RunAttempt{
		AttemptID: "execute-submitted", ProjectID: project.ProjectID, RecordID: "APP-T-0001", ItemID: "APP-T-0001",
		Lane: runLaneExecute, WorkRevision: 7, WorkspacePath: submitted, Outcome: string(AttemptOutcomeSucceeded),
		EndState: RunEndState{Schema: "tusker.run-end-state/v2", HeadSHA: "source-1", WorktreePath: submitted, MaterialFingerprint: material, MaterialScope: []string{"submitted.txt"}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertRun(RunStatus{ProjectID: project.ProjectID, RecordID: "APP-T-0001", ItemID: "APP-T-0001", Lane: runLaneExecute, WorkRevision: 7, AttemptCount: 2, LeaseState: string(LeaseStateReleased), AttemptOutcome: string(AttemptOutcomeFailed), Terminal: true}); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertRun(RunStatus{ProjectID: "other-project", RecordID: "APP-T-0001", ItemID: "APP-T-0001", Lane: runLaneExecute, AttemptCount: 9, LeaseState: string(LeaseStateReleased), AttemptOutcome: string(AttemptOutcomeFailed), Terminal: true}); err != nil {
		t.Fatal(err)
	}

	wrongProject := Args{"vault": vault, "id": "APP-T-0001", "action": "rerun_checks", "by": "operator:sarav", "project": "other-project", "json": "true"}
	if err := workRecoverCmd(wrongProject); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("wrong project error = %v", err)
	}
	if foreign, err := store.FindRunScoped("other-project", "APP-T-0001"); err != nil || foreign.AttemptCount != 9 || foreign.Lane != runLaneExecute {
		t.Fatalf("cross-project run changed after refusal: %#v %v", foreign, err)
	}
	// A crash after recording checks but before queuing review must be resumable.
	taskBeforeRecovery := walkthroughNote(t, vault, "APP-T-0001")
	workspace, err := recoveryCommandVerificationWorkspace(store, vault, taskBeforeRecovery, RunStatus{ProjectID: project.ProjectID, RecordID: "APP-T-0001", WorkRevision: 7})
	if err != nil {
		t.Fatal(err)
	}
	if _, report, failures, err := executeV7CommandVerificationRowsInWorkspace(vault, taskBeforeRecovery, Args{"rerun-invalid": "true"}, "operator:sarav", true, workspace); err != nil || len(failures) != 0 || report.Status != "satisfied" {
		t.Fatalf("record valid checks before resumed recovery: report=%#v failures=%#v err=%v", report, failures, err)
	}

	command, args := parseCLI([]string{"tusker", "work", "recover", "APP-T-0001", "--action", "rerun_checks", "--by", "operator:sarav", "--vault", vault, "--json"})
	if code, err := runInner(command, args); err != nil || code != 0 {
		t.Fatalf("work recover CLI = code %d, err %v", code, err)
	}
	task := walkthroughNote(t, vault, "APP-T-0001")
	if stringField(task.Data, "status") != "review" || parseV7VerificationRows(task.Body)[0].Result != "pass" {
		t.Fatalf("recovery did not reopen reviewed proof: %#v", task.Data)
	}
	if cause := v7VerificationReceiptInvalidationForWorkspace(vault, task, workspace); cause != nil {
		t.Fatalf("recovery status transition invalidated its fresh receipt: %#v", cause)
	}
	if missing := v7VerificationReceiptRequirementMissing(vault, task); missing != "" {
		t.Fatalf("canonical proof status ignored submitted material: %s", missing)
	}
	if err := statusV7CmdAsInternalActor(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "status": "backlog", "task-rev": "sha256:stale"}, "tusker:recovery"); err == nil || !strings.Contains(err.Error(), "snapshot changed") {
		t.Fatalf("stale recovery status mutation was not fenced: %v", err)
	}
	recovered, err := store.FindRunScoped(project.ProjectID, "APP-T-0001")
	if err != nil || recovered.AttemptCount != 2 || recovered.Lane != runLaneReview || recovered.LeaseState != string(LeaseStateUnclaimed) || recovered.Terminal {
		t.Fatalf("recovered run = %#v err=%v", recovered, err)
	}
	directive, err := store.RunDirective(project.ProjectID, "APP-T-0001")
	if err != nil || directive == nil || directive.Actor != "operator:sarav" {
		t.Fatalf("independent review was not queued in vault project: %#v %v", directive, err)
	}
	attempts, err := store.ListAttemptsForRun(project.ProjectID, "APP-T-0001")
	if err != nil || len(attempts) != 1 || attempts[0].AttemptID != "execute-submitted" {
		t.Fatalf("execute attempt history changed: %#v %v", attempts, err)
	}
	if foreign, err := store.FindRunScoped("other-project", "APP-T-0001"); err != nil || foreign.AttemptCount != 9 || foreign.Lane != runLaneExecute {
		t.Fatalf("cross-project run changed: %#v %v", foreign, err)
	}
}
