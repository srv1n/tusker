package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func workSessionFixture(t *testing.T, count int) (string, RegisteredProject) {
	t.Helper()
	vault := automationTestVault(t)
	for i := 0; i < count; i++ {
		mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": fmt.Sprintf("Work %d", i+1), "risk": "low", "priority": "p0", "v7": "true"}, newV7Task)
		id := fmt.Sprintf("APP-T-%04d", i+1)
		makeV7TaskDispatchableForTest(t, vault, id)
		setAutomationV7TaskFields(t, vault, id, map[string]any{"owned_paths": []string{"owned/" + id}})
	}
	initializeOrchestrationGitRepo(t, filepath.Dir(vault))
	project := registerAutomationTestProject(t, vault)
	configPath := managedTuskerConfigPath(vault)
	config, err := readText(configPath)
	if err != nil {
		t.Fatal(err)
	}
	config = strings.Replace(config, "mutation_mode: single_user_local", "mutation_mode: control_branch", 1)
	if err := writeText(configPath, config); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(vault), "tusker.yaml")); !os.IsNotExist(err) {
		t.Fatalf("work-session fixture created legacy root config: %v", err)
	}
	return vault, project
}

func startWorkSessionTest(t *testing.T, vault, id, owner string) error {
	t.Helper()
	var err error
	captureStdout(t, func() { err = workSessionStartCmd(Args{"vault": vault, "id": id, "by": owner, "source": "codex"}) })
	return err
}

func configureWorkSessionMaterialScope(t *testing.T, vault string) {
	t.Helper()
	setAutomationV7TaskFields(t, vault, "APP-T-0001", map[string]any{"work_revision": 1, "owned_paths": []string{"owned"}})
	owned := filepath.Join(filepath.Dir(vault), "owned")
	if err := os.MkdirAll(owned, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(owned, "implementation.go"), []byte("package owned\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := gitFactOutput(filepath.Dir(vault), "add", "owned/implementation.go"); err != nil {
		t.Fatal(err)
	}
	if _, err := gitFactOutput(filepath.Dir(vault), "commit", "-m", "seed owned implementation"); err != nil {
		t.Fatal(err)
	}
}

func workSessionErrorCode(err error) string {
	if issue, ok := err.(*TuskerError); ok {
		return issue.Code
	}
	return ""
}

func TestWorkSessionInteractiveStartWithAutomationDisabled(t *testing.T) {
	vault := automationTestVault(t)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Manual work", "risk": "low", "priority": "p0", "v7": "true"}, newV7Task)
	makeV7TaskDispatchableForTest(t, vault, "APP-T-0001")
	initializeOrchestrationGitRepo(t, filepath.Dir(vault))
	project := registerAutomationTestProject(t, vault)
	if _, err := setProjectLocalConfigWithReadback(vault, "automation.enabled", false); err != nil {
		t.Fatal(err)
	}
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	project.Enabled, project.Health = false, projectHealthDisabled
	if err := store.UpsertProject(project); err != nil {
		t.Fatal(err)
	}
	_ = store.Close()

	output := captureStdout(t, func() {
		if err := workSessionStartCmd(Args{"vault": vault, "id": "APP-T-0001", "by": "agent:codex", "source": "codex"}); err != nil {
			t.Fatal(err)
		}
	})
	var packet workSessionPacket
	if err := json.Unmarshal([]byte(output), &packet); err != nil {
		t.Fatal(err)
	}
	store, err = OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	run, err := store.FindRun("APP-T-0001")
	if err != nil {
		t.Fatal(err)
	}
	if run == nil || run.LeaseOwner != "agent:codex" || !run.HandRun {
		t.Fatalf("interactive work start did not create a hand-owned lease: %#v", run)
	}
	auth, err := store.LatestRunAuthorization(project.ProjectID, "APP-T-0001")
	if err != nil {
		t.Fatal(err)
	}
	if auth == nil || auth.ProjectAutomationEnabled || auth.Source != "codex" {
		t.Fatalf("interactive work start widened automation authority: %#v", auth)
	}
	identity, err := store.RunIdentity(project.ProjectID, "APP-T-0001")
	if err != nil {
		t.Fatal(err)
	}
	if identity == nil || identity.Branch == "" || identity.Head == "" || identity.WorkspacePath != run.WorkspacePath {
		t.Fatalf("interactive work start did not bind source identity atomically: %#v", identity)
	}
	if packet.Branch != identity.Branch || packet.Head != identity.Head || !strings.Contains(packet.Packet, "# APP-T-0001 agent packet") {
		t.Fatalf("returned packet was not bound to atomic identity: packet=%#v identity=%#v", packet, identity)
	}
	statusOutput := captureStdout(t, func() {
		if err := workSessionStatusCmd(Args{"id": "APP-T-0001"}); err != nil {
			t.Fatal(err)
		}
	})
	var status workSessionPacket
	if err := json.Unmarshal([]byte(statusOutput), &status); err != nil {
		t.Fatal(err)
	}
	if status.Branch != identity.Branch || status.Head != identity.Head || status.Authorization == nil || status.Authorization.Source != "codex" || !strings.Contains(status.Next, "heartbeat") {
		t.Fatalf("status lost session identity or next action: %#v", status)
	}
	attempts, err := store.ListAttemptsForRun(project.ProjectID, "APP-T-0001")
	if err != nil {
		t.Fatal(err)
	}
	if len(attempts) != 1 || attempts[0].WorkRevision != run.WorkRevision || attempts[0].BranchName != identity.Branch {
		t.Fatalf("interactive work start attempt binding = %#v", attempts)
	}
}

func TestWorkSessionUnregisteredRepoSupportsAgentReviewPath(t *testing.T) {
	vault := automationTestVault(t)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Manual unregistered work", "risk": "low", "priority": "p0", "v7": "true"}, newV7Task)
	makeV7TaskDispatchableForTest(t, vault, "APP-T-0001")
	initializeOrchestrationGitRepo(t, filepath.Dir(vault))

	if err := startWorkSessionTest(t, vault, "APP-T-0001", "agent:sarav"); err != nil {
		t.Fatal(err)
	}
	if err := statusV7Cmd(Args{"vault": vault, "id": "APP-T-0001", "status": "review", "by": "agent:sarav", "local": "true", "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	data, _, err := parseFrontmatterMustRead(filepath.Join(vault, "work", "tasks", "APP-T-0001.md"))
	if err != nil {
		t.Fatal(err)
	}
	if stringField(data, "status") != "review" {
		t.Fatalf("agent review path status = %q", stringField(data, "status"))
	}
}

func TestWorkSessionRejectsDaemonImpersonationAndRetiresOnce(t *testing.T) {
	store, run := ownershipStoreFixture(t, "APP-T-WORK-0001")
	defer store.Close()
	if err := workSessionStartCmd(Args{"id": run.RecordID, "by": "agent:codex", "source": "daemon_auto"}); err == nil || !strings.Contains(err.Error(), "source") {
		t.Fatalf("daemon impersonation must be refused, got %v", err)
	}
	service := newRunOwnershipService(store)
	claimed, err := service.claimWithAuthorization(run, "agent:codex", RunAuthorization{Source: "codex", Actor: "agent:codex"})
	if err != nil || !claimed.Claimed {
		t.Fatalf("claim: %#v %v", claimed, err)
	}
	if _, err := service.finish(run.RecordID, "agent:codex", AttemptOutcomeFailed, "", "", "done"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.finish(run.RecordID, "agent:codex", AttemptOutcomeFailed, "", "", "again"); err == nil {
		t.Fatal("second terminal mutation must fail its owner CAS")
	}
}

func TestWorkSessionIdentityWriteFailureRollsBackClaim(t *testing.T) {
	store, run := ownershipStoreFixture(t, "APP-T-WORK-ROLLBACK")
	defer store.Close()
	if _, err := store.db.Exec(`CREATE TRIGGER reject_work_identity BEFORE INSERT ON run_identity_metadata
		BEGIN SELECT RAISE(ABORT, 'identity refused'); END`); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	identity := RunIdentityMetadata{
		ProjectID: run.ProjectID, RecordID: run.RecordID, RepoRoot: "/tmp/repo",
		WorkspacePath: run.WorkspacePath, WorkspaceMode: string(WorkspaceStrategyCopy),
		Runner: run.Runner, Branch: "task/" + run.RecordID, Head: strings.Repeat("a", 40),
	}
	attempt := RunAttempt{
		AttemptID: "work-rollback", ProjectID: run.ProjectID, RecordID: run.RecordID,
		ItemID: run.ItemID, Runner: run.Runner, Lane: run.Lane, WorkRevision: run.WorkRevision,
		WorkspacePath: run.WorkspacePath, BranchName: identity.Branch,
	}
	claimed, err := store.claimRunLeaseWithWorkSessionAttempt(
		run, "agent:codex", 1, defaultRunLeaseTTL, now,
		RuntimeLeaseClaimPrecondition{
			ExpectedLeaseState: LeaseStateUnclaimed, ExpectedLeaseGeneration: 0,
			ExpectedWorkRevision: run.WorkRevision,
		},
		RunAuthorization{Source: "codex", Actor: "agent:codex", Trigger: "work_start"},
		attempt, identity,
	)
	if err == nil || claimed {
		t.Fatalf("identity failure must refuse the whole claim: claimed=%v err=%v", claimed, err)
	}
	latest, findErr := store.FindRun(run.RecordID)
	if findErr != nil {
		t.Fatal(findErr)
	}
	if latest == nil || LeaseState(latest.LeaseState) != LeaseStateUnclaimed || latest.LeaseOwner != "" || latest.ActiveAttemptID != "" {
		t.Fatalf("failed identity write left a claimed run: %#v", latest)
	}
	if auth, authErr := store.LatestRunAuthorization(run.ProjectID, run.RecordID); authErr != nil || auth != nil {
		t.Fatalf("failed identity write left authorization: %#v err=%v", auth, authErr)
	}
	if attempts, attemptsErr := store.ListAttemptsForRun(run.ProjectID, run.RecordID); attemptsErr != nil || len(attempts) != 0 {
		t.Fatalf("failed identity write left attempt: %#v err=%v", attempts, attemptsErr)
	}
	if stored, identityErr := store.RunIdentity(run.ProjectID, run.RecordID); identityErr != nil || stored != nil {
		t.Fatalf("failed identity write left source binding: %#v err=%v", stored, identityErr)
	}
}

func TestWorkSessionStartRefusalMatrix(t *testing.T) {
	t.Run("dependency", func(t *testing.T) {
		vault, _ := workSessionFixture(t, 2)
		setAutomationV7TaskFields(t, vault, "APP-T-0002", map[string]any{"dependencies": []string{"APP-T-0001:hard"}})
		if err := startWorkSessionTest(t, vault, "APP-T-0002", "agent:b"); workSessionErrorCode(err) != "WORK_SESSION_DEPENDENCY_BLOCKED" {
			t.Fatalf("dependency refusal = %v", err)
		}
	})
	t.Run("human_gate", func(t *testing.T) {
		vault, _ := workSessionFixture(t, 1)
		if err := newV7Gate(Args{"vault": vault, "quiet": "true", "blocks": "APP-T-0001", "kind": "auth", "owner": "human:sarav", "action": "Complete account authorization.", "verification": "The account authorization is recorded.", "why-agent-cannot": "Only the account owner can authorize this integration."}); err != nil {
			t.Fatal(err)
		}
		if err := startWorkSessionTest(t, vault, "APP-T-0001", "agent:a"); workSessionErrorCode(err) != "WORK_SESSION_HUMAN_GATE" {
			t.Fatalf("human refusal = %v", err)
		}
	})
	t.Run("terminal", func(t *testing.T) {
		vault, _ := workSessionFixture(t, 1)
		setAutomationV7TaskFields(t, vault, "APP-T-0001", map[string]any{"status": "done", "readiness": "done"})
		if err := startWorkSessionTest(t, vault, "APP-T-0001", "agent:a"); workSessionErrorCode(err) != "WORK_SESSION_TERMINAL" {
			t.Fatalf("terminal refusal = %v", err)
		}
	})
	t.Run("healthy_owner", func(t *testing.T) {
		vault, _ := workSessionFixture(t, 1)
		if err := startWorkSessionTest(t, vault, "APP-T-0001", "agent:a"); err != nil {
			t.Fatal(err)
		}
		if err := startWorkSessionTest(t, vault, "APP-T-0001", "agent:b"); workSessionErrorCode(err) != "WORK_SESSION_HEALTHY_OWNER" {
			t.Fatalf("owner refusal = %v", err)
		}
	})
	t.Run("owned_path", func(t *testing.T) {
		vault, _ := workSessionFixture(t, 2)
		setAutomationV7TaskFields(t, vault, "APP-T-0001", map[string]any{"owned_paths": []string{"cmd/tusker"}})
		setAutomationV7TaskFields(t, vault, "APP-T-0002", map[string]any{"owned_paths": []string{"cmd/tusker/work.go"}})
		if _, err := setProjectLocalConfigWithReadback(vault, "runtime.max_active_runs_per_project", 2); err != nil {
			t.Fatal(err)
		}
		if err := startWorkSessionTest(t, vault, "APP-T-0001", "agent:a"); err != nil {
			t.Fatal(err)
		}
		if err := startWorkSessionTest(t, vault, "APP-T-0002", "agent:b"); workSessionErrorCode(err) != "OWNED_PATH_CONFLICT" {
			t.Fatalf("path refusal = %v", err)
		}
	})
	t.Run("unsafe_workspace", func(t *testing.T) {
		vault, _ := workSessionFixture(t, 1)
		wfFile, err := loadWorkflow(vault)
		if err != nil {
			t.Fatal(err)
		}
		wfFile.Data.Workspace.Strategy = string(WorkspaceStrategyShared)
		writeWorkflowForPreflightTest(t, vault, wfFile.Data, wfFile.Body)
		if _, err := setProjectLocalConfigWithReadback(vault, "workspace.strategy", "shared"); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(filepath.Dir(vault), "tracked.txt"), []byte("dirty\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := startWorkSessionTest(t, vault, "APP-T-0001", "agent:a"); workSessionErrorCode(err) != "WORK_SESSION_UNSAFE_WORKSPACE" {
			t.Fatalf("workspace refusal = %v", err)
		}
	})
}

func TestWorkSessionDeadExpiredHolderReclaims(t *testing.T) {
	vault, _ := workSessionFixture(t, 1)
	if err := startWorkSessionTest(t, vault, "APP-T-0001", "agent:old"); err != nil {
		t.Fatal(err)
	}
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	old := time.Now().UTC().Add(-4 * defaultRunLeaseTTL).Format(time.RFC3339)
	if _, err := store.db.Exec(`UPDATE runs SET lease_expires_at=?, process_pid=0, process_pgid=0 WHERE record_id=?`, old, "APP-T-0001"); err != nil {
		t.Fatal(err)
	}
	_ = store.Close()
	if err := startWorkSessionTest(t, vault, "APP-T-0001", "agent:new"); err != nil {
		t.Fatal(err)
	}
	store, _ = OpenRuntimeStore(DefaultStateRoot())
	defer store.Close()
	run, _ := store.FindRun("APP-T-0001")
	if run == nil || run.LeaseOwner != "agent:new" || run.LeaseGeneration != 2 {
		t.Fatalf("reclaimed run = %#v", run)
	}
}

func TestWorkSessionLifecycleCASAndExactOnce(t *testing.T) {
	for _, action := range []string{"fail", "release"} {
		t.Run(action, func(t *testing.T) {
			vault, _ := workSessionFixture(t, 1)
			if err := startWorkSessionTest(t, vault, "APP-T-0001", "agent:a"); err != nil {
				t.Fatal(err)
			}
			if err := workSessionLifecycleCmd(Args{"id": "APP-T-0001", "by": "agent:wrong", "reason": action}, action); err == nil {
				t.Fatal("wrong owner mutation succeeded")
			}
			if err := workSessionLifecycleCmd(Args{"id": "APP-T-0001", "by": "agent:a", "revision": "999", "reason": action}, action); err == nil {
				t.Fatal("wrong revision mutation succeeded")
			}
			captureStdout(t, func() {
				if err := workSessionLifecycleCmd(Args{"id": "APP-T-0001", "by": "agent:a", "reason": action}, action); err != nil {
					t.Fatal(err)
				}
			})
			if err := workSessionLifecycleCmd(Args{"id": "APP-T-0001", "by": "agent:a", "reason": "again"}, action); err == nil {
				t.Fatal("second terminal mutation succeeded")
			}
			store, _ := OpenRuntimeStore(DefaultStateRoot())
			defer store.Close()
			run, _ := store.FindRun("APP-T-0001")
			attempts, _ := store.ListAttemptsForRun(run.ProjectID, run.RecordID)
			if run.LeaseState != string(LeaseStateReleased) || len(attempts) != 1 {
				t.Fatalf("terminal state run=%#v attempts=%#v", run, attempts)
			}
		})
	}
}

func TestRunsReleaseCannotBypassInteractiveWorkSession(t *testing.T) {
	vault, _ := workSessionFixture(t, 1)
	if err := startWorkSessionTest(t, vault, "APP-T-0001", "agent:a"); err != nil {
		t.Fatal(err)
	}
	if err := runsReleaseCmd(Args{"id": "APP-T-0001", "reason": "bare"}); workSessionErrorCode(err) != errorMissingArg {
		t.Fatalf("bare legacy release = %v", err)
	}
	if err := runsReleaseCmd(Args{"id": "APP-T-0001", "by": "agent:other", "revision": "0", "reason": "foreign"}); err == nil {
		t.Fatal("foreign legacy release bypassed interactive owner")
	}
	if err := runsReleaseCmd(Args{"id": "APP-T-0001", "by": "agent:a", "reason": "missing revision"}); workSessionErrorCode(err) != errorMissingArg {
		t.Fatalf("legacy release without revision = %v", err)
	}
	captureStdout(t, func() {
		if err := runsReleaseCmd(Args{"id": "APP-T-0001", "by": "agent:a", "revision": "0", "reason": "done"}); err != nil {
			t.Fatal(err)
		}
	})
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	run, err := store.FindRun("APP-T-0001")
	if err != nil || run == nil || LeaseState(run.LeaseState) != LeaseStateReleased || run.LeaseOwner != "" {
		t.Fatalf("exact owner release did not retire session: run=%#v err=%v", run, err)
	}
}

func TestRunsReleaseBreakGlassPersistsActorAndFencesStaleSnapshot(t *testing.T) {
	vault, _ := workSessionFixture(t, 1)
	if err := startWorkSessionTest(t, vault, "APP-T-0001", "agent:a"); err != nil {
		t.Fatal(err)
	}
	output := captureStdout(t, func() {
		if err := runsReleaseCmd(Args{"id": "APP-T-0001", "break-glass": "true", "by": "human:sarav", "reason": "incident recovery", "json": "true"}); err != nil {
			t.Fatal(err)
		}
	})
	var response struct {
		Actor string `json:"actor"`
	}
	if err := json.Unmarshal([]byte(output), &response); err != nil || response.Actor != "human:sarav" {
		t.Fatalf("break-glass response attribution = %#v err=%v", response, err)
	}
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	run, err := store.FindRun("APP-T-0001")
	if err != nil || run == nil || !strings.Contains(run.LastError, "break_glass actor=human:sarav reason=incident recovery") {
		_ = store.Close()
		t.Fatalf("durable break-glass attribution = %#v err=%v", run, err)
	}
	_ = store.Close()

	store, stale := ownershipStoreFixture(t, "APP-T-BREAK-GLASS-CAS")
	stale.HandRun = true
	stale.LeaseState, stale.LeaseOwner, stale.LeaseGeneration = string(LeaseStateClaimed), "agent:old", 1
	stale.LeaseExpiresAt = time.Now().UTC().Add(time.Minute).Format(time.RFC3339)
	if err := store.UpsertRun(stale); err != nil {
		t.Fatal(err)
	}
	takeover := stale
	takeover.LeaseOwner, takeover.LeaseGeneration = "agent:new", 2
	if ok, err := store.UpdateRunIfLease(takeover, "agent:old", 1); err != nil || !ok {
		t.Fatalf("takeover: ok=%v err=%v", ok, err)
	}
	if err := finishRuntimeRunIfSnapshot(store, &stale, LeaseStateReleased, AttemptOutcomeAbandoned, 0, "break_glass actor=human:sarav reason=late", false); workSessionErrorCode(err) != "CAS_CONFLICT" {
		t.Fatalf("stale break-glass snapshot = %v", err)
	}
	live, err := store.FindRun(stale.RecordID)
	if err != nil || live == nil || live.LeaseOwner != "agent:new" || live.LeaseGeneration != 2 {
		t.Fatalf("stale break-glass overwrote takeover: %#v err=%v", live, err)
	}
}

func TestWorkSessionHeartbeatAndSubmitCAS(t *testing.T) {
	vault, _ := workSessionFixture(t, 1)
	if err := startWorkSessionTest(t, vault, "APP-T-0001", "agent:a"); err != nil {
		t.Fatal(err)
	}
	if err := workSessionLifecycleCmd(Args{"id": "APP-T-0001", "by": "agent:wrong"}, "heartbeat"); err == nil {
		t.Fatal("wrong owner heartbeat succeeded")
	}
	captureStdout(t, func() {
		if err := workSessionLifecycleCmd(Args{"id": "APP-T-0001", "by": "agent:a"}, "heartbeat"); err != nil {
			t.Fatal(err)
		}
	})
	captureStdout(t, func() {
		if err := workSessionLifecycleCmd(Args{"id": "APP-T-0001", "by": "agent:a", "deliverable": "done", "verification": "A1 pass", "gate-verdicts": "A1=pass"}, "submit"); err != nil {
			t.Fatal(err)
		}
	})
	if err := workSessionLifecycleCmd(Args{"id": "APP-T-0001", "by": "agent:a", "deliverable": "again", "verification": "A1 pass", "gate-verdicts": "A1=pass"}, "submit"); err == nil {
		t.Fatal("second submit succeeded")
	}
	store, _ := OpenRuntimeStore(DefaultStateRoot())
	defer store.Close()
	run, _ := store.FindRun("APP-T-0001")
	attempts, _ := store.ListAttemptsForRun(run.ProjectID, run.RecordID)
	data, _, _ := parseFrontmatterMustRead(filepath.Join(vault, "work", "tasks", "APP-T-0001.md"))
	if run.LeaseState != string(LeaseStateReleased) || run.AttemptOutcome != string(AttemptOutcomeSucceeded) || len(attempts) != 1 || stringField(data, "status") != "review" {
		t.Fatalf("submit did not retire exactly once into review: run=%#v attempts=%#v status=%s", run, attempts, stringField(data, "status"))
	}
}

func TestWorkSessionInteractiveReviewReceiptBindsImplementer(t *testing.T) {
	vault, _ := workSessionFixture(t, 1)
	configureWorkSessionMaterialScope(t, vault)
	if err := startWorkSessionTest(t, vault, "APP-T-0001", "agent:implementer"); err != nil {
		t.Fatal(err)
	}
	captureStdout(t, func() {
		if err := workSessionLifecycleCmd(Args{"id": "APP-T-0001", "by": "agent:implementer", "deliverable": "implementation", "verification": "A1 pass", "gate-verdicts": "A1=pass"}, "submit"); err != nil {
			t.Fatal(err)
		}
	})
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	run, err := store.FindRun("APP-T-0001")
	if err != nil || run == nil {
		t.Fatalf("completed implementation run: %#v err=%v", run, err)
	}
	attempts, err := store.ListAttemptsForRun(run.ProjectID, run.RecordID)
	if err != nil || len(attempts) != 1 || attempts[0].EndState.HeadSHA == "" {
		t.Fatalf("completed implementation attempt: %#v err=%v", attempts, err)
	}
	setAutomationV7TaskFields(t, vault, "APP-T-0001", map[string]any{"source_sha": attempts[0].EndState.HeadSHA})
	output := captureStdout(t, func() {
		if err := workSessionReviewCmd(Args{"vault": vault, "id": "APP-T-0001", "by": "reviewer:agent", "source": "codex"}); err != nil {
			t.Fatal(err)
		}
	})
	var packet workSessionPacket
	if err := json.Unmarshal([]byte(output), &packet); err != nil {
		t.Fatal(err)
	}
	if packet.Action != "review" || packet.Run == nil || packet.Run.Lane != runLaneReview || packet.Run.ActiveAttemptID == "" || packet.ImplementationAttempt != attempts[0].AttemptID || packet.Workspace != attempts[0].WorkspacePath || packet.ImplementationActor != "agent:implementer" || packet.ProofFingerprint == "" || packet.GateFingerprint == "" || packet.MaterialFingerprint == "" || !strings.Contains(packet.Next, "--material-fingerprint "+packet.MaterialFingerprint) {
		t.Fatalf("review packet lacks native provenance: %#v", packet)
	}
	current, err := resolveV7Note(vault, "APP-T-0001", "task")
	if err != nil {
		t.Fatal(err)
	}
	if err := reviewSubmitCmd(Args{"vault": vault, "id": "APP-T-0001", "attempt": packet.Run.ActiveAttemptID, "by": "reviewer:agent", "verdict": "changes_requested", "summary": "needs one correction", "finding": "repair A1", "task-rev": stringField(current.Data, "state_rev"), "source-sha": stringField(current.Data, "source_sha"), "work-rev": strconv.Itoa(packet.Revision), "proof-fingerprint": packet.ProofFingerprint, "gate-fingerprint": packet.GateFingerprint, "material-fingerprint": packet.MaterialFingerprint}); err != nil {
		t.Fatal(err)
	}
	results, err := store.ListReviewResults(run.ProjectID)
	if err != nil || len(results) != 1 || results[0].Result.MaterialFingerprint != packet.MaterialFingerprint {
		t.Fatalf("durable review receipt material identity = %#v err=%v", results, err)
	}
}

func TestWorkSessionInteractiveReviewRejectsMaterialDrift(t *testing.T) {
	vault, _ := workSessionFixture(t, 1)
	configureWorkSessionMaterialScope(t, vault)
	if err := startWorkSessionTest(t, vault, "APP-T-0001", "agent:implementer"); err != nil {
		t.Fatal(err)
	}
	captureStdout(t, func() {
		if err := workSessionLifecycleCmd(Args{"id": "APP-T-0001", "by": "agent:implementer", "deliverable": "implementation", "verification": "A1 pass", "gate-verdicts": "A1=pass"}, "submit"); err != nil {
			t.Fatal(err)
		}
	})
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	run, err := store.FindRun("APP-T-0001")
	if err != nil || run == nil {
		t.Fatalf("completed implementation run: %#v err=%v", run, err)
	}
	attempts, err := store.ListAttemptsForRun(run.ProjectID, run.RecordID)
	if err != nil || len(attempts) != 1 || attempts[0].EndState.MaterialFingerprint == "" {
		t.Fatalf("completed material fingerprint: %#v err=%v", attempts, err)
	}
	setAutomationV7TaskFields(t, vault, "APP-T-0001", map[string]any{"source_sha": attempts[0].EndState.HeadSHA})
	if err := os.WriteFile(filepath.Join(attempts[0].WorkspacePath, "owned", "review-material-drift.go"), []byte("untracked source changed after execute\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := workSessionReviewCmd(Args{"vault": vault, "id": "APP-T-0001", "by": "reviewer:agent", "source": "codex"}); err == nil || !strings.Contains(err.Error(), "workspace material changed") {
		t.Fatalf("review accepted changed untracked implementation material: %v", err)
	}
}

func TestWorkSessionInteractiveReviewRejectsTrackedMaterialDrift(t *testing.T) {
	vault, _ := workSessionFixture(t, 1)
	configureWorkSessionMaterialScope(t, vault)
	if err := startWorkSessionTest(t, vault, "APP-T-0001", "agent:implementer"); err != nil {
		t.Fatal(err)
	}
	captureStdout(t, func() {
		if err := workSessionLifecycleCmd(Args{"id": "APP-T-0001", "by": "agent:implementer", "deliverable": "implementation", "verification": "A1 pass", "gate-verdicts": "A1=pass"}, "submit"); err != nil {
			t.Fatal(err)
		}
	})
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	run, err := store.FindRun("APP-T-0001")
	if err != nil || run == nil {
		t.Fatalf("completed implementation run: %#v err=%v", run, err)
	}
	attempts, err := store.ListAttemptsForRun(run.ProjectID, run.RecordID)
	if err != nil || len(attempts) != 1 {
		t.Fatalf("completed implementation attempt: %#v err=%v", attempts, err)
	}
	setAutomationV7TaskFields(t, vault, "APP-T-0001", map[string]any{"source_sha": attempts[0].EndState.HeadSHA})
	if err := os.WriteFile(filepath.Join(attempts[0].WorkspacePath, "owned", "implementation.go"), []byte("package owned\n// changed after execute\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := workSessionReviewCmd(Args{"vault": vault, "id": "APP-T-0001", "by": "reviewer:agent", "source": "codex"}); err == nil || !strings.Contains(err.Error(), "workspace material changed") {
		t.Fatalf("review accepted changed tracked implementation material: %v", err)
	}
}

func TestWorkSessionInteractiveReviewRejectsMaterialScopeDrift(t *testing.T) {
	vault, _ := workSessionFixture(t, 1)
	configureWorkSessionMaterialScope(t, vault)
	if err := startWorkSessionTest(t, vault, "APP-T-0001", "agent:implementer"); err != nil {
		t.Fatal(err)
	}
	captureStdout(t, func() {
		if err := workSessionLifecycleCmd(Args{"id": "APP-T-0001", "by": "agent:implementer", "deliverable": "implementation", "verification": "A1 pass", "gate-verdicts": "A1=pass"}, "submit"); err != nil {
			t.Fatal(err)
		}
	})
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	run, err := store.FindRun("APP-T-0001")
	if err != nil || run == nil {
		t.Fatalf("completed implementation run: %#v err=%v", run, err)
	}
	attempts, err := store.ListAttemptsForRun(run.ProjectID, run.RecordID)
	if err != nil || len(attempts) != 1 {
		t.Fatalf("completed implementation attempt: %#v err=%v", attempts, err)
	}
	setAutomationV7TaskFields(t, vault, "APP-T-0001", map[string]any{"source_sha": attempts[0].EndState.HeadSHA, "owned_paths": []string{"narrowed"}})
	if err := workSessionReviewCmd(Args{"vault": vault, "id": "APP-T-0001", "by": "reviewer:agent", "source": "codex"}); err == nil || !strings.Contains(err.Error(), "material scope changed") {
		t.Fatalf("review accepted narrowed material scope: %v", err)
	}
}

func TestWorkSessionInteractiveReviewMaterialScopeIgnoresUnrelatedAndControlWrites(t *testing.T) {
	vault, _ := workSessionFixture(t, 1)
	configureWorkSessionMaterialScope(t, vault)
	if err := startWorkSessionTest(t, vault, "APP-T-0001", "agent:implementer"); err != nil {
		t.Fatal(err)
	}
	captureStdout(t, func() {
		if err := workSessionLifecycleCmd(Args{"id": "APP-T-0001", "by": "agent:implementer", "deliverable": "implementation", "verification": "A1 pass", "gate-verdicts": "A1=pass"}, "submit"); err != nil {
			t.Fatal(err)
		}
	})
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	run, err := store.FindRun("APP-T-0001")
	if err != nil || run == nil {
		t.Fatalf("completed implementation run: %#v err=%v", run, err)
	}
	attempts, err := store.ListAttemptsForRun(run.ProjectID, run.RecordID)
	if err != nil || len(attempts) != 1 || strings.Join(attempts[0].EndState.MaterialScope, ",") != ".tusker/specs/test-fixture.md,owned" {
		t.Fatalf("declared material scope not persisted: %#v err=%v", attempts, err)
	}
	setAutomationV7TaskFields(t, vault, "APP-T-0001", map[string]any{"source_sha": attempts[0].EndState.HeadSHA})
	if err := os.WriteFile(filepath.Join(attempts[0].WorkspacePath, "other-task.go"), []byte("package other\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(attempts[0].WorkspacePath, ".tusker", "evidence"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(attempts[0].WorkspacePath, ".tusker", "evidence", "review.md"), []byte("operational evidence\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := workSessionReviewCmd(Args{"vault": vault, "id": "APP-T-0001", "by": "reviewer:agent", "source": "codex"}); err != nil {
		t.Fatalf("unrelated or control-plane write invalidated review: %v", err)
	}
}

func TestWorkSessionTaskRevisionDriftRefusesMutation(t *testing.T) {
	vault, _ := workSessionFixture(t, 1)
	if err := startWorkSessionTest(t, vault, "APP-T-0001", "agent:a"); err != nil {
		t.Fatal(err)
	}
	setAutomationV7TaskFields(t, vault, "APP-T-0001", map[string]any{"work_revision": 2})
	if err := workSessionLifecycleCmd(Args{"id": "APP-T-0001", "by": "agent:a", "reason": "stale"}, "fail"); workSessionErrorCode(err) != "WORK_SESSION_STALE" {
		t.Fatalf("stale task revision mutation = %v", err)
	}
}

func TestWorkSessionAgentGuardAndHumanBreakGlass(t *testing.T) {
	vault, _ := workSessionFixture(t, 1)
	if err := requireAgentWorkSession(vault, "APP-T-0001", "agent:a", Args{}); workSessionErrorCode(err) != "WORK_SESSION_REQUIRED" {
		t.Fatalf("direct review guard = %v", err)
	} else if hint := errorToIssue(err).Hint; !strings.Contains(hint, "--vault "+strconv.Quote(vault)) {
		t.Fatalf("work-session hint is not self-contained: %q", hint)
	}
	if err := statusV7Cmd(Args{"vault": vault, "id": "APP-T-0001", "status": "review", "by": "agent:a", "local": "true", "quiet": "true"}); workSessionErrorCode(err) != "WORK_SESSION_REQUIRED" {
		t.Fatalf("status --local bypassed configured work-session guard: %v", err)
	}
	if err := statusV7Cmd(Args{"vault": vault, "id": "APP-T-0001", "status": "review", "by": "human:sarav", "break-glass": "true", "reason": "incident recovery", "local": "true", "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	data, _, _ := parseFrontmatterMustRead(filepath.Join(vault, "work", "tasks", "APP-T-0001.md"))
	if stringField(data, "updated_by") != "human:sarav" {
		t.Fatalf("break-glass actor = %q", stringField(data, "updated_by"))
	}
	t.Run("live_session_review", func(t *testing.T) {
		liveVault, _ := workSessionFixture(t, 1)
		if err := startWorkSessionTest(t, liveVault, "APP-T-0001", "agent:a"); err != nil {
			t.Fatal(err)
		}
		if err := requireAgentWorkSession(liveVault, "APP-T-0001", "agent:a", Args{}); err != nil {
			t.Fatal(err)
		}
		if err := statusV7Cmd(Args{"vault": liveVault, "id": "APP-T-0001", "status": "review", "by": "agent:a", "local": "true", "quiet": "true"}); err != nil {
			t.Fatal(err)
		}
	})
}

func TestWorkSessionAcceptsOnlyExactDaemonAttemptCapability(t *testing.T) {
	vault, project := workSessionFixture(t, 1)
	task, err := resolveV7Note(vault, "APP-T-0001", "task")
	if err != nil {
		t.Fatal(err)
	}
	workRevision := intField(task.Data, "work_revision")
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	run := RunStatus{
		ProjectID: project.ProjectID, RecordID: "APP-T-0001", ItemID: "APP-T-0001",
		Runner: string(RunnerCodexExec), Lane: runLaneExecute, WorkRevision: workRevision,
		WorkspacePath: project.RepoRoot, LeaseState: string(LeaseStateUnclaimed),
	}
	if err := store.UpsertRunPreservingLease(run); err != nil {
		t.Fatal(err)
	}
	attemptID := "01KYDAEMONWORKSESSION00000000"
	claim, err := newRunOwnershipService(store).claimExistingWithAuthorization(run, attemptID, RunAuthorization{
		Source: "daemon_auto", Actor: "daemon", Trigger: "poll", ProjectAutomationEnabled: true,
	}, RunAttempt{AttemptID: attemptID, WorkspacePath: project.RepoRoot})
	if err != nil || !claim.Claimed || claim.Run == nil {
		t.Fatalf("daemon claim failed: claim=%#v err=%v", claim, err)
	}
	claimed := *claim.Run
	claimed.ActiveAttemptID = attemptID
	claimed.LeaseState = string(LeaseStateClaimed)
	persisted, err := store.UpdateRunIfLease(claimed, attemptID, claimed.LeaseGeneration)
	if err != nil || !persisted {
		t.Fatalf("daemon attempt identity update failed: persisted=%v err=%v", persisted, err)
	}
	updated, err := store.FindRun("APP-T-0001")
	if err != nil || updated == nil {
		t.Fatalf("daemon attempt identity reload failed: run=%#v err=%v", updated, err)
	}
	claimed = *updated
	if err := attemptV7StartCmd(Args{
		"vault": vault, "quiet": "true", "id": "APP-T-0001",
		"attempt-id": "APP-T-0001-A-0001", "runtime-attempt-id": attemptID,
		"lane": runLaneExecute, "runner": string(RunnerCodexExec),
		"workspace-kind": "git_worktree", "workspace-path": project.RepoRoot, "branch": "task/APP-T-0001",
	}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TUSKER_ATTEMPT_ID", attemptID)
	t.Setenv("TUSKER_PROJECT_ID", project.ProjectID)
	t.Setenv("TUSKER_RECORD_ID", "APP-T-0001")
	t.Setenv("TUSKER_LEASE_GENERATION", fmt.Sprintf("%d", claimed.LeaseGeneration))
	t.Setenv("TUSKER_WORK_REVISION", fmt.Sprintf("%d", workRevision))
	t.Setenv("TUSKER_WORKSPACE", project.RepoRoot)
	t.Setenv("TUSKER_REPO_ROOT", project.RepoRoot)
	t.Setenv("TUSKER_VAULT", vault)
	t.Setenv("TUSKER_STATUS_PATH", filepath.Join(t.TempDir(), "active.status.json"))
	t.Setenv("TUSKER_RUN_LANE", runLaneExecute)
	if err := requireAgentWorkSession(vault, "APP-T-0001", "agent:worker", Args{}); err != nil {
		t.Fatalf("exact daemon attempt capability was refused: %v", err)
	}
	attemptPath := filepath.Join(vault, "attempts", "APP-T-0001", "APP-T-0001-A-0001.md")
	attemptData, attemptBody, err := parseFrontmatterMustRead(attemptPath)
	if err != nil {
		t.Fatal(err)
	}
	attemptData["status"] = "handoff"
	if _, err := saveV7DocumentCAS(attemptPath, attemptData, attemptBody, v7FrontmatterOrder["attempt"], stringField(attemptData, "state_rev")); err != nil {
		t.Fatal(err)
	}
	if err := requireAgentWorkSession(vault, "APP-T-0001", "agent:worker", Args{}); err != nil {
		t.Fatalf("handoff daemon attempt capability was refused: %v", err)
	}
	exactEnv := map[string]string{
		"TUSKER_ATTEMPT_ID":       attemptID,
		"TUSKER_PROJECT_ID":       project.ProjectID,
		"TUSKER_RECORD_ID":        "APP-T-0001",
		"TUSKER_LEASE_GENERATION": fmt.Sprintf("%d", claimed.LeaseGeneration),
		"TUSKER_WORK_REVISION":    fmt.Sprintf("%d", workRevision),
		"TUSKER_WORKSPACE":        project.RepoRoot,
		"TUSKER_REPO_ROOT":        project.RepoRoot,
		"TUSKER_VAULT":            vault,
		"TUSKER_STATUS_PATH":      os.Getenv("TUSKER_STATUS_PATH"),
		"TUSKER_RUN_LANE":         runLaneExecute,
	}
	finishedStatusPath := filepath.Join(t.TempDir(), "finished.status.json")
	if err := os.WriteFile(finishedStatusPath, []byte(`{"exit_code":0}`), 0o600); err != nil {
		t.Fatal(err)
	}
	mismatches := map[string]string{
		"TUSKER_ATTEMPT_ID":       attemptID + "-other",
		"TUSKER_PROJECT_ID":       project.ProjectID + "-other",
		"TUSKER_RECORD_ID":        "APP-T-9999",
		"TUSKER_LEASE_GENERATION": fmt.Sprintf("%d", claimed.LeaseGeneration+1),
		"TUSKER_WORK_REVISION":    fmt.Sprintf("%d", workRevision+1),
		"TUSKER_WORKSPACE":        filepath.Join(project.RepoRoot, "other"),
		"TUSKER_REPO_ROOT":        filepath.Join(project.RepoRoot, "other"),
		"TUSKER_VAULT":            filepath.Join(vault, "other"),
		"TUSKER_STATUS_PATH":      finishedStatusPath,
		"TUSKER_RUN_LANE":         runLaneReview,
	}
	for key, mismatch := range mismatches {
		t.Run(key, func(t *testing.T) {
			for envKey, exact := range exactEnv {
				t.Setenv(envKey, exact)
			}
			t.Setenv(key, mismatch)
			if err := requireAgentWorkSession(vault, "APP-T-0001", "agent:worker", Args{}); workSessionErrorCode(err) != "WORK_SESSION_REQUIRED" {
				t.Fatalf("mismatched daemon capability was accepted: %v", err)
			}
		})
	}
}

func TestWorkSessionLegacyEntryPointsDelegate(t *testing.T) {
	for _, entry := range []string{"attempt", "claim"} {
		t.Run(entry, func(t *testing.T) {
			vault, project := workSessionFixture(t, 1)
			captureStdout(t, func() {
				var err error
				if entry == "attempt" {
					err = attemptV7Cmd(Args{"vault": vault, "_pos0": "start", "_pos1": "APP-T-0001", "by": "agent:a"})
				} else {
					err = claimCmd(Args{"vault": vault, "id": "APP-T-0001", "owner": "agent:a"})
				}
				if err != nil {
					t.Fatal(err)
				}
			})
			store, _ := OpenRuntimeStore(DefaultStateRoot())
			defer store.Close()
			run, _ := store.FindRun("APP-T-0001")
			attempts, _ := store.ListAttemptsForRun(project.ProjectID, "APP-T-0001")
			if run == nil || !strings.HasPrefix(run.ActiveAttemptID, "work-") || len(attempts) != 1 || attempts[0].AttemptID != run.ActiveAttemptID {
				t.Fatalf("legacy %s created contradictory state: run=%#v attempts=%#v", entry, run, attempts)
			}
		})
	}
}

func TestWorkSessionNotificationIsExactRunHintAndDoesNotSpawn(t *testing.T) {
	shortState, err := os.MkdirTemp("/tmp", "tws-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(shortState) })
	t.Setenv("TUSKER_STATE_ROOT", shortState)
	vault := pickupV7TestVault(t)
	if err := writeDefaultWorkflow(vault); err != nil {
		t.Fatal(err)
	}
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Notify", "risk": "low", "priority": "p0", "v7": "true"}, newV7Task)
	makeV7TaskDispatchableForTest(t, vault, "APP-T-0001")
	initializeOrchestrationGitRepo(t, filepath.Dir(vault))
	registerAutomationTestProject(t, vault)
	requests := make(chan daemonControlRequest, 2)
	server, err := startDaemonControlServer(DefaultStateRoot(), func(_ context.Context, req daemonControlRequest) daemonControlResponse {
		requests <- req
		return daemonControlResponse{OK: true}
	})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	if err := startWorkSessionTest(t, vault, "APP-T-0001", "agent:a"); err != nil {
		t.Fatal(err)
	}
	select {
	case req := <-requests:
		if req.Command != "reconcile_project" || req.Cause != "work_session" || len(req.Changes) != 1 || req.Changes[0].ID != "APP-T-0001" || req.Changes[0].Kind != "run" || !strings.HasPrefix(req.Changes[0].Revision, "lease:") {
			t.Fatalf("notification = %#v", req)
		}
	case <-time.After(time.Second):
		t.Fatal("missing work-session notification")
	}
	store, _ := OpenRuntimeStore(DefaultStateRoot())
	defer store.Close()
	runs, _ := store.ListRuns()
	if len(runs) != 1 || runs[0].ProcessPID != 0 || runs[0].ProcessPGID != 0 {
		t.Fatalf("interactive start spawned work: %#v", runs)
	}
}
