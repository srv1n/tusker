package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestProjectsRebindPreservesProjectRuntimeHistoryAndSupportsReverseRetry(t *testing.T) {
	stateRoot := filepath.Join(t.TempDir(), "state")
	t.Setenv("TUSKER_STATE_ROOT", stateRoot)
	oldRepo, oldVault := projectRebindFixtureRepo(t, "old")
	newRepo, newVault := projectRebindFixtureRepo(t, "new")
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	project := newRegisteredProject(oldRepo, oldVault)
	project.ProjectID = "project-rebind"
	project.Enabled, project.Health = false, projectHealthDisabled
	if err := store.UpsertProject(project); err != nil {
		t.Fatal(err)
	}
	run := RunStatus{ProjectID: project.ProjectID, RecordID: "APP-T-0001", ItemID: "APP-T-0001", Runner: string(RunnerCodexExec), Lane: runLaneExecute, LeaseState: string(LeaseStateParkedNoProgress), AttemptOutcome: string(AttemptOutcomeBlocked), AttemptCount: 3, Terminal: true, UpdatedAt: time.Now().UTC().Format(time.RFC3339)}
	if err := store.UpsertRun(run); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveAttempt(RunAttempt{AttemptID: "parked-attempt", ProjectID: project.ProjectID, RecordID: run.RecordID, ItemID: run.ItemID, Runner: run.Runner, Lane: run.Lane, Outcome: string(AttemptOutcomeBlocked), StartedAt: run.UpdatedAt}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveSession(RunnerSession{ProjectID: project.ProjectID, RecordID: run.RecordID, Runner: run.Runner, SessionRef: "parked-session", State: "closed", StartedAt: run.UpdatedAt}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	dryRunOutput := captureStdout(t, func() {
		err = projectsRebindCmd(Args{"id": project.ProjectID, "repo": newRepo, "vault": newVault, "dry-run": "true", "json": "true"})
	})
	if err != nil || !strings.Contains(dryRunOutput, `"dry_run":true`) || !strings.Contains(dryRunOutput, oldRepo) || !strings.Contains(dryRunOutput, newRepo) {
		t.Fatalf("dry run=%s err=%v", dryRunOutput, err)
	}
	output := captureStdout(t, func() {
		err = projectsRebindCmd(Args{"id": project.ProjectID, "repo": newRepo, "vault": newVault, "json": "true"})
	})
	if err != nil || !strings.Contains(output, `"changed":true`) || !strings.Contains(output, `"before"`) || !strings.Contains(output, `"after"`) {
		t.Fatalf("rebind=%s err=%v", output, err)
	}
	store, err = OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	bound, err := projectByID(store, project.ProjectID)
	if err != nil || bound.RepoRoot != newRepo || bound.VaultRoot != newVault || bound.Enabled {
		t.Fatalf("rebound project=%#v err=%v", bound, err)
	}
	runs, err := store.ListRuns()
	if err != nil || len(runs) != 1 || runs[0].ProjectID != project.ProjectID || runs[0].LeaseState != string(LeaseStateParkedNoProgress) {
		t.Fatalf("runs not preserved: %#v err=%v", runs, err)
	}
	attempts, err := store.ListAttemptsForRun(project.ProjectID, run.RecordID)
	if err != nil || len(attempts) != 1 || attempts[0].AttemptID != "parked-attempt" {
		t.Fatalf("attempts not preserved: %#v err=%v", attempts, err)
	}
	sessions, err := store.ListSessionsForRun(project.ProjectID, run.RecordID, run.Runner)
	if err != nil || len(sessions) != 1 || sessions[0].SessionRef != "parked-session" {
		t.Fatalf("sessions not preserved: %#v err=%v", sessions, err)
	}
	audit, err := store.ListProjectRebindAudit(project.ProjectID)
	if err != nil || len(audit) != 1 || audit[0].Before.RepoRoot != oldRepo || audit[0].After.RepoRoot != newRepo {
		t.Fatalf("audit=%#v err=%v", audit, err)
	}

	if _, _, changed, err := store.RebindProjectRegistration(project.ProjectID, newRepo, newVault); err != nil || changed {
		t.Fatalf("retry changed=%v err=%v", changed, err)
	}
	if _, _, changed, err := store.RebindProjectRegistration(project.ProjectID, oldRepo, oldVault); err != nil || !changed {
		t.Fatalf("reverse changed=%v err=%v", changed, err)
	}
}

func TestProjectRebindFailsClosedAndRollsBackPersistenceFailure(t *testing.T) {
	stateRoot := filepath.Join(t.TempDir(), "state")
	t.Setenv("TUSKER_STATE_ROOT", stateRoot)
	oldRepo, oldVault := projectRebindFixtureRepo(t, "old")
	newRepo, newVault := projectRebindFixtureRepo(t, "new")
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	project := newRegisteredProject(oldRepo, oldVault)
	project.ProjectID, project.Enabled, project.Health = "project-rebind", false, projectHealthDisabled
	if err := store.UpsertProject(project); err != nil {
		t.Fatal(err)
	}
	store.rebindProjectAfterUpdate = func(*sql.Tx) error { return errors.New("injected persistence failure") }
	if _, _, _, err := store.RebindProjectRegistration(project.ProjectID, newRepo, newVault); err == nil || !strings.Contains(err.Error(), "injected") {
		t.Fatalf("persistence failure err=%v", err)
	}
	bound, err := projectByID(store, project.ProjectID)
	if err != nil || bound.RepoRoot != oldRepo || bound.VaultRoot != oldVault {
		t.Fatalf("rollback project=%#v err=%v", bound, err)
	}
	audit, err := store.ListProjectRebindAudit(project.ProjectID)
	if err != nil || len(audit) != 0 {
		t.Fatalf("rollback audit=%#v err=%v", audit, err)
	}
	store.rebindProjectAfterUpdate = nil
	if err := store.UpsertRun(RunStatus{ProjectID: project.ProjectID, RecordID: "live", ItemID: "live", LeaseState: string(LeaseStateUnclaimed), UpdatedAt: time.Now().UTC().Format(time.RFC3339)}); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := store.RebindProjectRegistration(project.ProjectID, newRepo, newVault); err == nil || !strings.Contains(err.Error(), "non-terminal") {
		t.Fatalf("live rebind err=%v", err)
	}
	if _, _, _, err := store.RebindProjectRegistration("", newRepo, newVault); err == nil {
		t.Fatal("missing project ID must fail")
	}
}

func TestProjectRebindReportsScopedStableNonTerminalRunIdentities(t *testing.T) {
	stateRoot := filepath.Join(t.TempDir(), "state")
	t.Setenv("TUSKER_STATE_ROOT", stateRoot)
	oldRepo, oldVault := projectRebindFixtureRepo(t, "old")
	newRepo, newVault := projectRebindFixtureRepo(t, "new")
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	project := newRegisteredProject(oldRepo, oldVault)
	project.ProjectID, project.Enabled, project.Health = "project-rebind", false, projectHealthDisabled
	if err := store.UpsertProject(project); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	fixtures := []RunStatus{
		{ProjectID: project.ProjectID, RecordID: "APP-T-0002", ItemID: "APP-T-0002", Lane: runLaneReview, LeaseState: string(LeaseStateParkedNoProgress), AttemptOutcome: string(AttemptOutcomeBlocked), ActiveAttemptID: "attempt-2", WorkspacePath: "/secret/workspace", RawLogPath: "/secret/raw.log", Terminal: false, UpdatedAt: now},
		{ProjectID: "other-project", RecordID: "OTHER-T-0001", ItemID: "OTHER-T-0001", Lane: runLaneExecute, LeaseState: string(LeaseStateUnclaimed), AttemptOutcome: string(AttemptOutcomeNone), ActiveAttemptID: "other-attempt", Terminal: false, UpdatedAt: now},
		{ProjectID: project.ProjectID, RecordID: "APP-T-0001", ItemID: "", Lane: runLaneExecute, LeaseState: string(LeaseStateUnclaimed), AttemptOutcome: string(AttemptOutcomeNone), ActiveAttemptID: "", Terminal: false, UpdatedAt: now},
		{ProjectID: project.ProjectID, RecordID: "APP-T-0003", ItemID: "APP-T-0003", Lane: runLaneExecute, LeaseState: string(LeaseStateReleased), AttemptOutcome: string(AttemptOutcomeAbandoned), ActiveAttemptID: "terminal-attempt", Terminal: true, UpdatedAt: now},
	}
	for _, run := range fixtures {
		if err := store.UpsertRun(run); err != nil {
			t.Fatal(err)
		}
	}

	err = projectsRebindCmd(Args{"id": project.ProjectID, "repo": newRepo, "vault": newVault, "dry-run": "true", "json": "true"})
	if err == nil {
		t.Fatal("dry-run must remain fenced by non-terminal runs")
	}
	issue := errorToIssue(err)
	if issue.Code != errorInvalidTransition || !strings.Contains(issue.Hint, "runs inspect <run-id>") || !strings.Contains(issue.Hint, "runs retire <run-id>") {
		t.Fatalf("issue=%#v", issue)
	}
	context, ok := issue.Context.(map[string]any)
	if !ok {
		t.Fatalf("context type=%T", issue.Context)
	}
	blocking, ok := context["blocking_runs"].([]ProjectNonTerminalRun)
	if !ok || len(blocking) != 2 {
		t.Fatalf("blocking=%#v", context["blocking_runs"])
	}
	if blocking[0].RunID != "APP-T-0001" || blocking[0].TaskID != "APP-T-0001" || blocking[0].ProjectID != project.ProjectID || blocking[0].Status != string(LeaseStateUnclaimed) {
		t.Fatalf("first blocking run=%#v", blocking[0])
	}
	if blocking[1].RunID != "APP-T-0002" || blocking[1].AttemptID != "attempt-2" || blocking[1].Lane != runLaneReview || blocking[1].Outcome != string(AttemptOutcomeBlocked) {
		t.Fatalf("second blocking run=%#v", blocking[1])
	}
	encoded, err := json.Marshal(issue)
	if err != nil {
		t.Fatal(err)
	}
	wire := string(encoded)
	for _, forbidden := range []string{"OTHER-T-0001", "other-attempt", "APP-T-0003", "terminal-attempt", "/secret/workspace", "/secret/raw.log"} {
		if strings.Contains(wire, forbidden) {
			t.Fatalf("diagnostic leaked %q: %s", forbidden, wire)
		}
	}

	_, _, _, err = store.RebindProjectRegistration(project.ProjectID, newRepo, newVault)
	if err == nil {
		t.Fatal("live rebind must remain fenced by non-terminal runs")
	}
	liveContext := errorToIssue(err).Context.(map[string]any)
	liveBlocking := liveContext["blocking_runs"].([]ProjectNonTerminalRun)
	if len(liveBlocking) != 2 || liveBlocking[0].RunID != "APP-T-0001" || liveBlocking[1].RunID != "APP-T-0002" {
		t.Fatalf("live blocking runs=%#v", liveBlocking)
	}
}

func TestProjectRebindRefusesWorkspaceMountAndPartialSamePath(t *testing.T) {
	stateRoot := filepath.Join(t.TempDir(), "state")
	t.Setenv("TUSKER_STATE_ROOT", stateRoot)
	oldRepo, oldVault := projectRebindFixtureRepo(t, "old")
	newRepo, newVault := projectRebindFixtureRepo(t, "new")
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	project := newRegisteredProject(oldRepo, oldVault)
	project.ProjectID, project.Enabled, project.Health = "project-rebind", false, projectHealthDisabled
	if err := store.UpsertProject(project); err != nil {
		t.Fatal(err)
	}
	if err := saveWorkspaceVaultConfig(WorkspaceVaultConfig{Projects: []WorkspaceProject{{ProjectID: project.ProjectID, TrackerRoot: oldVault, MountPath: filepath.Join(t.TempDir(), "mount")}}}); err != nil {
		t.Fatal(err)
	}
	if err := projectsRebindCmd(Args{"id": project.ProjectID, "repo": newRepo, "vault": newVault}); err == nil || !strings.Contains(err.Error(), "workspace") {
		t.Fatalf("workspace mount rebind err=%v", err)
	}
	if err := os.Remove(workspaceConfigPath()); err != nil {
		t.Fatal(err)
	}
	if err := saveWorkspaceVaultConfig(WorkspaceVaultConfig{Projects: []WorkspaceProject{{ProjectID: "other-project", TrackerRoot: newVault, MountPath: filepath.Join(t.TempDir(), "other-mount")}}}); err != nil {
		t.Fatal(err)
	}
	if err := projectsRebindCmd(Args{"id": project.ProjectID, "repo": newRepo, "vault": newVault}); err == nil || !strings.Contains(err.Error(), "another project") {
		t.Fatalf("target workspace collision err=%v", err)
	}
	if err := os.Remove(workspaceConfigPath()); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := store.RebindProjectRegistration(project.ProjectID, oldRepo, newVault); err == nil || !strings.Contains(err.Error(), "together") {
		t.Fatalf("partial same path err=%v", err)
	}
	claimed := newRegisteredProject(newRepo, newVault)
	claimed.ProjectID = "other-project"
	if err := store.UpsertProject(claimed); err != nil {
		t.Fatal(err)
	}
	thirdRepo, thirdVault := projectRebindFixtureRepo(t, "third")
	if _, _, _, err := store.RebindProjectRegistration(project.ProjectID, newRepo, newVault); err == nil || !strings.Contains(err.Error(), "claimed") {
		t.Fatalf("target collision err=%v", err)
	}
	if _, _, changed, err := store.RebindProjectRegistration(project.ProjectID, thirdRepo, thirdVault); err != nil || !changed {
		t.Fatalf("unclaimed target changed=%v err=%v", changed, err)
	}
}

func TestDisabledProjectCannotAcquireAutomatedLeaseKinds(t *testing.T) {
	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	oldRepo, oldVault := t.TempDir(), t.TempDir()
	newRepo, newVault := t.TempDir(), t.TempDir()
	project := RegisteredProject{ProjectID: "disabled", ProjectKey: "disabled", Name: "disabled", RepoRoot: oldRepo, VaultRoot: oldVault, WorkflowPath: "workflow", Enabled: false, Health: projectHealthDisabled}
	if err := store.UpsertProject(project); err != nil {
		t.Fatal(err)
	}
	if _, rebound, changed, err := store.RebindProjectRegistration(project.ProjectID, newRepo, newVault); err != nil || !changed || rebound.Enabled {
		t.Fatalf("rebind before stale claim changed=%v project=%#v err=%v", changed, rebound, err)
	}
	project.RepoRoot, project.VaultRoot = newRepo, newVault
	now := time.Now().UTC()
	for _, recordID := range []string{"run", "directive", "interactive"} {
		if err := store.UpsertRun(RunStatus{ProjectID: project.ProjectID, RecordID: recordID, ItemID: recordID, Runner: string(RunnerCodexExec), Lane: runLaneExecute, LeaseState: string(LeaseStateUnclaimed), UpdatedAt: now.Format(time.RFC3339)}); err != nil {
			t.Fatal(err)
		}
	}
	if claimed, err := store.ClaimRunLease(project.ProjectID, "run", "owner", 1, time.Minute, now, true, false, RuntimeLeaseClaimPrecondition{ExpectedLeaseState: LeaseStateUnclaimed}); err != nil || claimed {
		t.Fatalf("ordinary disabled claim=%v err=%v", claimed, err)
	}
	if queued, err := store.QueueRunDirective(RunDirective{ProjectID: project.ProjectID, RecordID: "directive", Actor: "human:test", CreatedAt: now.Format(time.RFC3339Nano), ExpiresAt: now.Add(time.Hour).Format(time.RFC3339Nano)}); err != nil || !queued {
		t.Fatalf("directive queue=%v err=%v", queued, err)
	}
	directiveRun, _ := store.FindRun("directive")
	if claimed, err := store.claimRunLeaseWithDirectiveAttempt(*directiveRun, "directive-attempt", 1, time.Minute, now, RuntimeLeaseClaimPrecondition{ExpectedLeaseState: LeaseStateUnclaimed}, RunAuthorization{Source: "human_run_directive", Actor: "human:test"}, RunAttempt{AttemptID: "directive-attempt", Runner: string(RunnerCodexExec), Lane: runLaneExecute}); err != nil || claimed {
		t.Fatalf("directive disabled claim=%v err=%v", claimed, err)
	}
	interactiveRun, _ := store.FindRun("interactive")
	identity := RunIdentityMetadata{ProjectID: project.ProjectID, RecordID: "interactive", RepoRoot: project.RepoRoot, WorkspacePath: t.TempDir(), WorkspaceMode: "in_place", Runner: string(RunnerCodexExec)}
	if claimed, err := store.claimRunLeaseWithWorkSessionAttempt(*interactiveRun, "interactive-attempt", 1, time.Minute, now, RuntimeLeaseClaimPrecondition{ExpectedLeaseState: LeaseStateUnclaimed}, RunAuthorization{Source: "tusker_cli", Actor: "agent:test"}, RunAttempt{AttemptID: "interactive-attempt", Runner: string(RunnerCodexExec), Lane: runLaneExecute}, identity); err != nil || !claimed {
		t.Fatalf("interactive work must remain available with automation disabled: claim=%v err=%v", claimed, err)
	}
}

func projectRebindFixtureRepo(t *testing.T, name string) (string, string) {
	t.Helper()
	repo := filepath.Join(t.TempDir(), name)
	vault := filepath.Join(repo, ".tusker")
	if err := os.MkdirAll(vault, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeDefaultWorkflow(vault); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init", "-q"}, {"config", "user.email", "test@example.com"}, {"config", "user.name", "Tusker Test"}, {"add", "."}, {"commit", "-qm", "fixture"}} {
		if out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return canonicalProjectPath(repo), canonicalProjectPath(vault)
}
