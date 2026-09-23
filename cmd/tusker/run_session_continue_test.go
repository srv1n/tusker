package main

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestRunSessionContinueUnsupportedAdaptersFailClosed(t *testing.T) {
	request := ResumeRequest{SessionRef: "native-session", Command: "codex app-server"}
	for _, runner := range []Runner{&CodexRunner{}, &CodexAppServerRunner{}} {
		result, err := runner.Resume(context.Background(), request)
		if result != nil || err == nil || !strings.Contains(err.Error(), "does not support native session resume") {
			t.Fatalf("%s accepted unsupported native resume: result=%#v err=%v", runner.Name(), result, err)
		}
	}

	result, err := (&CodexExecRunner{}).Resume(context.Background(), ResumeRequest{
		SessionRef: "native-session", Command: defaultCodexExecCommand(),
		CommandArgv: []string{"sh", "-c", "echo fresh"},
	})
	if result != nil || err == nil || !strings.Contains(err.Error(), "direct codex exec command") {
		t.Fatalf("codex exec accepted a non-resume argv: result=%#v err=%v", result, err)
	}
}

func TestRunSessionContinueCodexExecBindsExactSession(t *testing.T) {
	argv := codexExecResumeArgv([]string{"codex", "exec", "--json", "--skip-git-repo-check", "-"}, "session-old")
	want := []string{"codex", "exec", "--json", "--skip-git-repo-check", "resume", "session-old", "-"}
	if len(argv) != len(want) {
		t.Fatalf("resume argv=%#v, want %#v", argv, want)
	}
	for i := range want {
		if argv[i] != want[i] {
			t.Fatalf("resume argv=%#v, want %#v", argv, want)
		}
	}
}

func TestRunSessionContinueExplicitContextRecoveryUsesFreshSession(t *testing.T) {
	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	now := time.Date(2026, time.September, 22, 12, 0, 0, 0, time.UTC)
	run := RunStatus{
		ProjectID: "project-1", RecordID: "APP-T-0001", ItemID: "APP-T-0001",
		Runner: string(RunnerCodexExec), Lane: runLaneExecute,
		LeaseState: string(LeaseStateReleased), AttemptOutcome: string(AttemptOutcomeUnknown),
		ActiveAttemptID: "attempt-old", SessionRef: "session-old", WorkRevision: 3,
		WorkspacePath: t.TempDir(), AttemptCount: 2, Terminal: true,
	}
	if err := store.UpsertRun(run); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveAttempt(RunAttempt{
		AttemptID: run.ActiveAttemptID, ProjectID: run.ProjectID, RecordID: run.RecordID, ItemID: run.ItemID,
		Runner: run.Runner, Lane: run.Lane, WorkRevision: run.WorkRevision, WorkspacePath: run.WorkspacePath,
		SessionRef: run.SessionRef, Outcome: string(AttemptOutcomeUnknown), StartedAt: now.Add(-time.Minute).Format(time.RFC3339),
	}); err != nil {
		t.Fatal(err)
	}
	task := Note{Data: map[string]any{"id": run.ItemID}}
	wave := Note{Data: map[string]any{
		"id": "W-0001", "authorization": "armed", "authorization_fingerprint": "sha256:wave",
		"authorized_at": now.Add(-time.Minute).Format(time.RFC3339),
	}}
	result, err := queueOutcomeUnknownContextRecovery(store, task, wave, run, "human:test", now)
	if err != nil {
		t.Fatal(err)
	}
	if !result.OK || !result.Admitted {
		t.Fatalf("context recovery was not admitted: %#v", result)
	}
	updated, err := store.FindRunScoped(run.ProjectID, run.RecordID)
	if err != nil || updated == nil {
		t.Fatalf("read recovered run: %#v %v", updated, err)
	}
	if updated.SessionRef != "" {
		t.Fatalf("context recovery retained native session %q", updated.SessionRef)
	}
	if updated.AttemptCount != run.AttemptCount {
		t.Fatalf("context recovery changed attempt budget: before=%d after=%d", run.AttemptCount, updated.AttemptCount)
	}
	if !strings.Contains(updated.LastError, run.ActiveAttemptID) {
		t.Fatalf("context recovery lost parent lineage: %q", updated.LastError)
	}
	if directive, err := store.RunDirective(run.ProjectID, run.RecordID); err != nil || directive == nil || directive.State != "queued" {
		t.Fatalf("context recovery directive=%#v err=%v", directive, err)
	}
	decisions, err := store.ListRuntimeSupervisorDecisionsForRun(run.ProjectID, run.RecordID)
	if err != nil {
		t.Fatal(err)
	}
	if len(decisions) != 1 || decisions[0].Kind != string(SupervisorDecisionForkThread) || decisions[0].ParentSessionRef != "session-old" {
		t.Fatalf("context recovery lineage=%#v", decisions)
	}
}

func TestRunSessionContinueContextRecoveryRefusesCompletedEffects(t *testing.T) {
	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	run := RunStatus{
		ProjectID: "project-1", RecordID: "APP-T-0001", ItemID: "APP-T-0001",
		Runner: string(RunnerCodexExec), LeaseState: string(LeaseStateReleased),
		AttemptOutcome: string(AttemptOutcomeSucceeded), Terminal: true,
	}
	if err := store.UpsertRun(run); err != nil {
		t.Fatal(err)
	}
	result, err := queueOutcomeUnknownContextRecovery(store, Note{Data: map[string]any{"id": run.ItemID}}, Note{}, run, "human:test", time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if !result.Refused || !strings.Contains(result.Reason, "unknown terminal outcome") {
		t.Fatalf("completed effect was replayable: %#v", result)
	}
}

func TestRunSessionContinueCodexExecFlagsPrecedeResume(t *testing.T) {
	args := codexExecResumeArgv([]string{"codex", "exec", "--json", "--sandbox", "workspace-write", "-c", `model="x"`, "--skip-git-repo-check", "-"}, "S")
	want := []string{"codex", "exec", "--json", "--sandbox", "workspace-write", "-c", `model="x"`, "--skip-git-repo-check", "resume", "S", "-"}
	if strings.Join(args, "|") != strings.Join(want, "|") {
		t.Fatalf("argv=%q want=%q", args, want)
	}
	if got := codexExecResumeCommand("codex exec --json --skip-git-repo-check"); got != "codex exec --json --skip-git-repo-check resume {{session_ref}} -" {
		t.Fatalf("resume template without start dash=%q", got)
	}
	command := codexExecResumeCommand(`codex exec --json --sandbox workspace-write -c 'model="x"' --skip-git-repo-check -`)
	if !strings.Contains(command, `--skip-git-repo-check resume {{session_ref}} -`) {
		t.Fatalf("command=%q", command)
	}
	bypass := codexExecResumeArgv([]string{"codex", "exec", "--dangerously-bypass-approvals-and-sandbox", "-"}, "S")
	if strings.Join(bypass, "|") != "codex|exec|--dangerously-bypass-approvals-and-sandbox|resume|S|-" {
		t.Fatalf("bypass=%q", bypass)
	}
}
