package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestWavePreflight(t *testing.T) {
	vault := authorizedWaveTestVault(t)
	before := snapshotDeliveryRecords(t, vault)
	_, wave, idx, err := loadWaveAuthorizationTarget(Args{"vault": vault, "_pos0": "W-0001"})
	if err != nil {
		t.Fatal(err)
	}
	green := greenWaveEnvironment()
	report := buildWavePreflight(vault, idx, wave, green)
	if !report.OK || !report.ReadOnly || len(report.Frontiers) != 2 || len(report.Artifacts) != 2 {
		t.Fatalf("unexpected green preflight: %#v", report)
	}
	if report.DispatchScope.Effective != string(automationDispatchScopeArmedWaves) || report.DispatchScope.Provenance == "" {
		t.Fatalf("preflight omitted dispatch scope projection: %#v", report.DispatchScope)
	}
	if after := snapshotDeliveryRecords(t, vault); !mapsEqualString(before, after) {
		t.Fatal("preflight mutated records")
	}

	cases := []struct {
		name   string
		mutate func(*wavePreflightEnvironment)
		want   string
	}{
		{"project", func(e *wavePreflightEnvironment) { e.ProjectRegistered = false }, "project"},
		{"daemon", func(e *wavePreflightEnvironment) { e.DaemonAlive = false }, "daemon"},
		{"runner", func(e *wavePreflightEnvironment) { e.RunnerCompatible = false }, "runner"},
		{"skill", func(e *wavePreflightEnvironment) { e.SkillCompatible = false }, "operator skill"},
		{"workflow", func(e *wavePreflightEnvironment) { e.WorkflowCompatible = false }, "workflow"},
		{"approval", func(e *wavePreflightEnvironment) { e.ApprovalFree = false }, "approval"},
		{"workspace", func(e *wavePreflightEnvironment) { e.IsolatedWorkspace = false }, "workspace"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := green
			tc.mutate(&env)
			got := buildWavePreflight(vault, idx, wave, env)
			if got.OK || !strings.Contains(strings.Join(got.Blockers, " "), tc.want) {
				t.Fatalf("missing %s blocker: %#v", tc.want, got.Blockers)
			}
		})
	}

	brokenIdx := idx
	brokenIdx.Tasks = cloneNoteMap(idx.Tasks)
	broken := brokenIdx.Tasks["APP-T-0001"]
	broken.Data = cloneMap(broken.Data)
	delete(broken.Data, "artifact_contract")
	brokenIdx.Tasks["APP-T-0001"] = broken
	if got := buildWavePreflight(vault, brokenIdx, wave, green); got.OK || !hasWaveBlocker(got.Blockers, "artifact contract") {
		t.Fatalf("missing artifact blocker: %#v", got.Blockers)
	}
	broken = brokenIdx.Tasks["APP-T-0001"]
	broken.Data["artifact_contract"] = idx.Tasks["APP-T-0001"].Data["artifact_contract"]
	broken.Data["dependencies"] = []string{"APP-T-0002:hard"}
	brokenIdx.Tasks["APP-T-0001"] = broken
	if got := buildWavePreflight(vault, brokenIdx, wave, green); got.OK || !hasWaveBlocker(got.Blockers, "cycle") {
		t.Fatalf("missing cycle blocker: %#v", got.Blockers)
	}

	artifactIdx := idx
	artifactIdx.Tasks = cloneNoteMap(idx.Tasks)
	artifactTask := artifactIdx.Tasks["APP-T-0001"]
	artifactTask.Data = cloneMap(artifactTask.Data)
	artifactTask.Data["artifact_contract"] = map[string]any{"kind": "log", "path": ".tusker/scratch/output.log", "summary": "Invalid scratch artifact."}
	artifactIdx.Tasks["APP-T-0001"] = artifactTask
	if got := buildWavePreflight(vault, artifactIdx, wave, green); got.OK || !hasWaveBlocker(got.Blockers, "artifact contract") {
		t.Fatalf("scratch artifact escaped canonical validation: %#v", got.Blockers)
	}

	specIdx := idx
	specIdx.Tasks = cloneNoteMap(idx.Tasks)
	specTask := specIdx.Tasks["APP-T-0001"]
	specTask.Data = cloneMap(specTask.Data)
	specTask.Data["spec_refs"] = []string{"docs/specs/missing.md"}
	specIdx.Tasks["APP-T-0001"] = specTask
	if got := buildWavePreflight(vault, specIdx, wave, green); got.OK || !hasWaveBlocker(got.Blockers, "spec_ref does not resolve") {
		t.Fatalf("missing member spec_ref was not rejected: %#v", got.Blockers)
	}

	externalIdx := idx
	externalIdx.Tasks = cloneNoteMap(idx.Tasks)
	external := externalIdx.Tasks["APP-T-0002"]
	external.Data = cloneMap(external.Data)
	external.Data["id"], external.Data["status"], external.Data["readiness"] = "APP-T-0003", "backlog", "held"
	externalIdx.Tasks["APP-T-0003"] = external
	externalTask := externalIdx.Tasks["APP-T-0001"]
	externalTask.Data = cloneMap(externalTask.Data)
	externalTask.Data["dependencies"] = []string{"APP-T-0003:hard"}
	externalIdx.Tasks["APP-T-0001"] = externalTask
	gotExternal := buildWavePreflight(vault, externalIdx, wave, green)
	if gotExternal.OK || gotExternal.ExternalDependencies["APP-T-0001"] == nil || !hasWaveBlocker(gotExternal.Blockers, "external dependency APP-T-0003") {
		t.Fatalf("external dependency was not explained and blocked: %#v %#v", gotExternal.ExternalDependencies, gotExternal.Blockers)
	}
}

func TestWavePreflightSkillAndIntegrationCompatibility(t *testing.T) {
	vault := deliveryTestVault(t)
	repo := v7RepoRoot(vault)
	operatorPath := filepath.Join(repo, "skill", "SKILL.md")
	provenance, err := embeddedFactoryIntakeContractProvenance()
	if err != nil {
		t.Fatal(err)
	}
	compatibleSkill := fmt.Sprintf("---\nname: tusker\ndescription: test\nmetadata:\n  wave_authorization_schema: tusker.wave-authorization/v1\n  workflow_version: 1\n  tracker_schema_version: 7\n  factory_intake_contract_schema: %q\n  factory_intake_contract_version: %q\n  factory_intake_contract_fingerprint: %q\n---\n", provenance.Schema, provenance.Version, provenance.Fingerprint)
	if err := writeText(operatorPath, compatibleSkill); err != nil {
		t.Fatal(err)
	}
	if !waveSkillCompatible(vault) {
		t.Fatal("current wave-aware operator skill was rejected")
	}
	if err := writeText(operatorPath, strings.Replace(compatibleSkill, "tusker.wave-authorization/v1", "tusker.wave-authorization/v0", 1)); err != nil {
		t.Fatal(err)
	}
	if waveSkillCompatible(vault) {
		t.Fatal("stale operator skill was accepted")
	}

	gitRepo, gitVault := newLandTestRepo(t, 1, "true")
	gitIdx, _ := loadV7Index(gitVault)
	gitWave := gitIdx.Waves["W-0001"]
	if !waveIntegrationBaseClean(gitVault, gitWave) {
		t.Fatal("clean integration base was rejected")
	}
	worktree := filepath.Join(t.TempDir(), "integration")
	runGitDir(t, gitRepo, "worktree", "add", worktree, "integration/W-0001")
	t.Cleanup(func() { _ = exec.Command("git", "-C", gitRepo, "worktree", "remove", "--force", worktree).Run() })
	if err := writeText(filepath.Join(worktree, "dirty.txt"), "dirty\n"); err != nil {
		t.Fatal(err)
	}
	if waveIntegrationBaseClean(gitVault, gitWave) {
		t.Fatal("dirty integration worktree was accepted")
	}
	_ = os.Remove(filepath.Join(worktree, "dirty.txt"))
	runGitDir(t, gitRepo, "worktree", "remove", "--force", worktree)
	commitLandBranch(t, gitRepo, "task/unrelated", "integration/W-0001", map[string]string{"ahead.txt": "ahead\n"})
	runGitDir(t, gitRepo, "branch", "-f", "integration/W-0001", "task/unrelated")
	if waveIntegrationBaseClean(gitVault, gitWave) {
		t.Fatal("integration branch ahead of its clean base was accepted")
	}
	gitWave.Data = cloneMap(gitWave.Data)
	gitWave.Data["authorized_at"] = "2026-07-14T00:00:00Z"
	if !waveIntegrationBaseClean(gitVault, gitWave) {
		t.Fatal("clean progressed integration branch could not resume")
	}
}

func TestWavePreflightProfileApprovalPolicy(t *testing.T) {
	wf := defaultWorkflow()
	wf.RunnerProfiles = map[string]RunnerProfileDefinition{
		"implementation-terra": {
			Harness:          string(RunnerCodexExec),
			Model:            "gpt-5.6-terra",
			Effort:           "high",
			PermissionPreset: "workspace-write-network",
			Sandbox:          RunnerSandboxDefinition{Mode: "workspace-write", Network: boolPtr(true)},
		},
	}
	wave := Note{Data: map[string]any{"runner_profile": "implementation-terra"}}
	for _, policy := range []string{"never", "bypass"} {
		wf.Codex.ApprovalPolicy = policy
		env := wavePreflightEnvironment{}
		applyWaveWorkflowEnvironment(&env, wave, wf)
		if !env.RunnerCompatible || !env.ApprovalFree {
			t.Fatalf("approval-free effective Codex policy %q was rejected for workspace-write-network: %#v", policy, env)
		}
	}

	wf.Codex.ApprovalPolicy = "on-request"
	env := wavePreflightEnvironment{}
	applyWaveWorkflowEnvironment(&env, wave, wf)
	if env.ApprovalFree {
		t.Fatalf("workspace-write-network incorrectly bypassed an interactive effective policy: %#v", env)
	}
}

func TestWaveArm(t *testing.T) {
	vault := authorizedWaveTestVault(t)
	unrelatedPath := filepath.Join(vault, "work", "tasks", "APP-T-0003.md")
	unrelatedData, unrelatedBody, err := parseFrontmatterMustRead(filepath.Join(vault, "work", "tasks", "APP-T-0001.md"))
	if err != nil {
		t.Fatal(err)
	}
	unrelatedData = cloneMap(unrelatedData)
	unrelatedData["id"], unrelatedData["title"], unrelatedData["status"], unrelatedData["readiness"] = "APP-T-0003", "Unrelated ready work", "ready", "ready"
	delete(unrelatedData, "wave")
	unrelatedData["state_rev"] = v7StateRev(unrelatedData, unrelatedBody)
	unrelatedContent, err := serializeDocument(unrelatedData, unrelatedBody, v7FrontmatterOrder["task"])
	if err != nil {
		t.Fatal(err)
	}
	if err := writeText(unrelatedPath, unrelatedContent); err != nil {
		t.Fatal(err)
	}
	args := Args{"vault": vault, "_pos0": "W-0001", "by": "human:test", "quiet": "true"}
	green := greenWaveEnvironment()
	before := snapshotDeliveryRecords(t, vault)
	failing := cloneArgs(args)
	failing["fail-after-first-write"] = "true"
	if err := mutateWaveAuthorization(failing, "armed", &green); err == nil {
		t.Fatal("expected forced arm failure")
	}
	if after := snapshotDeliveryRecords(t, vault); !mapsEqualString(before, after) {
		t.Fatal("arm failure did not roll back")
	}

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, actor := range []string{"human:one", "human:two"} {
		wg.Add(1)
		go func(actor string) {
			defer wg.Done()
			call := cloneArgs(args)
			call["by"] = actor
			errs <- mutateWaveAuthorization(call, "armed", &green)
		}(actor)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	waveData, _, err := parseFrontmatterMustRead(filepath.Join(vault, "work", "waves", "W-0001.md"))
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "armed", stringField(waveData, "authorization"), "authorization")
	if stringField(waveData, "authorization_fingerprint") == "" || stringField(waveData, "authorized_by") == "" {
		t.Fatal("arm omitted durable identity")
	}
	for _, id := range []string{"APP-T-0001", "APP-T-0002"} {
		data, _, e := parseFrontmatterMustRead(filepath.Join(vault, "work", "tasks", id+".md"))
		if e != nil {
			t.Fatal(e)
		}
		assertEqual(t, "ready", stringField(data, "status"), id+" promoted")
	}
	if got := mustReadIndexTest(t, unrelatedPath); got != unrelatedContent {
		t.Fatal("arm changed unrelated ready task")
	}
}

func TestWaveArmReusesHeldMemberLocks(t *testing.T) {
	t.Setenv("TUSKER_STATE_ROOT", filepath.Join(t.TempDir(), "state"))
	vault := deliveryTestVault(t)
	path := writeDeliveryTestPlan(t, vault, validDeliveryPlan())
	if err := deliveryImportCmd(Args{
		"vault": vault, "plan": path, "wave": "Public arm regression", "quiet": "true",
	}); err != nil {
		t.Fatal(err)
	}
	green := greenWaveEnvironment()
	if err := mutateWaveAuthorization(
		Args{"vault": vault, "_pos0": "W-0001", "by": "human:fixture", "quiet": "true"},
		"armed",
		&green,
	); err != nil {
		t.Fatalf("public arm path recursively reacquired its member locks: %v", err)
	}
	idx, err := loadV7Index(vault)
	if err != nil {
		t.Fatal(err)
	}
	wave := idx.Waves["W-0001"]
	material, issues := waveMaterialFingerprint(vault, idx, wave)
	if len(issues) != 0 ||
		stringField(wave.Data, "authorization") != "armed" ||
		stringField(wave.Data, "authorization_fingerprint") != material {
		t.Fatalf("public arm did not persist an exact material snapshot: wave=%#v material=%q issues=%#v", wave.Data, material, issues)
	}
}

func TestWaveArmAuthorizesCriticalMemberDispatch(t *testing.T) {
	vault := deliveryTestVault(t)
	plan := validDeliveryPlan()
	plan.Tasks[0].Risk = "critical"
	planPath := writeDeliveryTestPlan(t, vault, plan)
	if err := deliveryImportCmd(Args{"vault": vault, "plan": planPath, "wave": "Critical", "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	armWaveForTest(t, vault)
	idx, err := loadV7Index(vault)
	if err != nil {
		t.Fatal(err)
	}
	notes, err := listAllNotes(vault)
	if err != nil {
		t.Fatal(err)
	}
	lookup := buildNoteLookup(notes)
	task := idx.Tasks["APP-T-0001"]
	if blocker := daemonDispatchBlockedReason(vault, task, lookup.ByID, lookup.ByRecordID); blocker != "" {
		t.Fatalf("armed critical member still requires a second authorization: %s", blocker)
	}
	impostor := task
	impostor.Data = cloneMap(task.Data)
	impostor.Data["id"] = "APP-T-9999"
	if armedWaveExplicitlyAuthorizes(impostor, lookup.ByID) {
		t.Fatal("armed wave authorized a critical task outside its exact member set")
	}
}

func TestImportedWaveAuthorizationIgnoresAppendedProofRows(t *testing.T) {
	vault := deliveryTestVault(t)
	planPath := writeDeliveryTestPlan(t, vault, validDeliveryPlan())
	if err := deliveryImportCmd(Args{"vault": vault, "plan": planPath, "wave": "Proof ledger", "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	armWaveForTest(t, vault)
	idx, err := loadV7Index(vault)
	if err != nil {
		t.Fatal(err)
	}
	before, issues := waveMaterialFingerprint(vault, idx, idx.Waves["W-0001"])
	if len(issues) != 0 {
		t.Fatalf("unexpected fingerprint issues: %#v", issues)
	}
	path := filepath.Join(vault, "work", "tasks", "APP-T-0001.md")
	data, body, err := parseFrontmatterMustRead(path)
	if err != nil {
		t.Fatal(err)
	}
	body = strings.TrimRight(body, "\n") + "\n| A1 | review: independent acceptance | pass | reviewer proof |\n"
	content, err := serializeDocument(data, body, v7FrontmatterOrder["task"])
	if err != nil {
		t.Fatal(err)
	}
	if err := writeText(path, content); err != nil {
		t.Fatal(err)
	}
	idx, err = loadV7Index(vault)
	if err != nil {
		t.Fatal(err)
	}
	after, issues := waveMaterialFingerprint(vault, idx, idx.Waves["W-0001"])
	if len(issues) != 0 || after != before {
		t.Fatalf("proof-only verification row invalidated imported authorization: before=%s after=%s issues=%#v", before, after, issues)
	}
}

func TestWavePause(t *testing.T) {
	vault := authorizedWaveTestVault(t)
	armWaveForTest(t, vault)
	args := Args{"vault": vault, "_pos0": "W-0001", "by": "human:test", "reason": "maintenance", "quiet": "true"}
	if err := waveV7PauseCmd(args); err != nil {
		t.Fatal(err)
	}
	if err := waveV7PauseCmd(args); err != nil {
		t.Fatal("pause not idempotent:", err)
	}
	idx, _ := loadV7Index(vault)
	task := idx.Tasks["APP-T-0001"]
	blockers := v7TaskDispatchBlockers(vault, task)
	if !strings.Contains(strings.Join(blockers, " "), "paused") {
		t.Fatalf("pause did not stop future claims: %#v", blockers)
	}
	green := greenWaveEnvironment()
	if err := mutateWaveAuthorization(args, "armed", &green); err != nil {
		t.Fatal(err)
	}
	idx, _ = loadV7Index(vault)
	if got := stringField(waveAuthorizationProjection(vault, idx, idx.Waves["W-0001"]), "state"); got != "armed" {
		t.Fatalf("resume=%s", got)
	}
}

func TestWavePauseAndDisarmPreserveLiveDaemonRuns(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(t *testing.T, vault string)
	}{
		{name: "paused", mutate: func(t *testing.T, vault string) {
			args := Args{"vault": vault, "_pos0": "W-0001", "by": "human:test", "reason": "lifecycle test", "quiet": "true"}
			if err := mutateWaveAuthorization(args, "paused", nil); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "disarmed", mutate: func(t *testing.T, vault string) {
			args := Args{"vault": vault, "_pos0": "W-0001", "by": "human:test", "reason": "lifecycle test", "quiet": "true"}
			if err := mutateWaveAuthorization(args, "disarmed", nil); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "stale", mutate: func(t *testing.T, vault string) {
			path := filepath.Join(vault, "work", "tasks", "APP-T-0001.md")
			data, body, err := parseFrontmatterMustRead(path)
			if err != nil {
				t.Fatal(err)
			}
			data["title"] = "Materially changed after authorization"
			data["state_rev"] = v7StateRev(data, body)
			content, err := serializeDocument(data, body, v7FrontmatterOrder["task"])
			if err != nil {
				t.Fatal(err)
			}
			if err := writeText(path, content); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("TUSKER_STATE_ROOT", t.TempDir())
			vault := authorizedWaveTestVault(t)
			armWaveForTest(t, vault)
			tc.mutate(t, vault)
			wfFile := WorkflowFile{Path: workflowPath(vault), Data: defaultWorkflow()}
			notes, err := listAllNotes(vault)
			if err != nil {
				t.Fatal(err)
			}
			lookup := buildNoteLookup(notes)
			note, err := resolveNote(vault, "APP-T-0001")
			if err != nil {
				t.Fatal(err)
			}
			store, err := OpenRuntimeStore(DefaultStateRoot())
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			daemon := &Daemon{stateRoot: DefaultStateRoot(), store: store, processIdentityProbe: func(RunStatus) bool { return true }}
			project := RegisteredProject{ProjectID: "app", ProjectKey: "app", Name: "app", RepoRoot: v7RepoRoot(vault), VaultRoot: vault, Enabled: true, Health: projectHealthHealthy}
			for _, lease := range []LeaseState{LeaseStateClaimed, LeaseStateRunning} {
				now := time.Now().UTC().Format(time.RFC3339)
				live := RunStatus{ProjectID: "app", RecordID: "APP-T-0001", ItemID: "APP-T-0001", Runner: wfFile.Data.Agents.Default, Lane: runLaneExecute, LeaseState: string(lease), LeaseOwner: "attempt-live", LeaseGeneration: 7, ActiveAttemptID: "attempt-live", AttemptCount: 3, ProcessPID: 1234, ProcessPGID: 1234, ProcessStartedAt: now, StartedAt: now, FirstEventAt: now, LastEventAt: now, LastHeartbeatAt: now}
				updated, persisted, err := daemon.reconcileExecuteRunWithPlan(context.Background(), project, wfFile, notes, note, live)
				if err != nil {
					t.Fatal(err)
				}
				updated, trackerPersisted, err := daemon.reconcileRunWithTracker(context.Background(), project, wfFile, updated, note, lookup.ByID, lookup.ByRecordID)
				if err != nil {
					t.Fatal(err)
				}
				if persisted || updated.LeaseState == string(LeaseStateReleased) || updated.LeaseState == string(LeaseStateInterrupted) || updated.LeaseOwner != live.LeaseOwner || updated.LeaseGeneration != live.LeaseGeneration || updated.ActiveAttemptID != live.ActiveAttemptID || updated.AttemptCount != live.AttemptCount || updated.ProcessPID != live.ProcessPID || updated.ProcessPGID != live.ProcessPGID || updated.ProcessStartedAt != live.ProcessStartedAt {
					t.Fatalf("%s forged a terminal/released live run: before=%#v after=%#v plan_persisted=%t tracker_persisted=%t", tc.name, live, updated, persisted, trackerPersisted)
				}
			}
			retry := RunStatus{ProjectID: "app", RecordID: "APP-T-0001", ItemID: "APP-T-0001", Runner: wfFile.Data.Agents.Default, Lane: runLaneExecute, LeaseState: string(LeaseStateRetryQueued), NextRetryAt: time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)}
			updated, persisted, err := daemon.reconcileExecuteRunWithPlan(context.Background(), project, wfFile, notes, note, retry)
			if err != nil {
				t.Fatal(err)
			}
			updated, _, err = daemon.reconcileRunWithTracker(context.Background(), project, wfFile, updated, note, lookup.ByID, lookup.ByRecordID)
			if err != nil {
				t.Fatal(err)
			}
			if !persisted || updated.LeaseState != string(LeaseStateUnclaimed) || !strings.Contains(updated.LastError, "wave W-0001 authorization") {
				t.Fatalf("%s did not suppress the future retry: %#v persisted=%t", tc.name, updated, persisted)
			}
		})
	}
}

func TestLiveExecuteTrackerStillReleasesTaskIneligibility(t *testing.T) {
	t.Setenv("TUSKER_STATE_ROOT", t.TempDir())
	vault := authorizedWaveTestVault(t)
	armWaveForTest(t, vault)
	wfFile := WorkflowFile{Path: workflowPath(vault), Data: defaultWorkflow()}
	notes, err := listAllNotes(vault)
	if err != nil {
		t.Fatal(err)
	}
	lookup := buildNoteLookup(notes)
	note, err := resolveNote(vault, "APP-T-0001")
	if err != nil {
		t.Fatal(err)
	}
	note.Data = cloneMap(note.Data)
	note.Data["status"] = "backlog"
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	daemon := &Daemon{stateRoot: DefaultStateRoot(), store: store}
	project := RegisteredProject{ProjectID: "app", ProjectKey: "app", Name: "app", RepoRoot: v7RepoRoot(vault), VaultRoot: vault, Enabled: true, Health: projectHealthHealthy}
	live := RunStatus{ProjectID: "app", RecordID: "APP-T-0001", ItemID: "APP-T-0001", Runner: wfFile.Data.Agents.Default, Lane: runLaneExecute, LeaseState: string(LeaseStateClaimed), LeaseOwner: "attempt-live", LeaseGeneration: 7, ActiveAttemptID: "attempt-live", AttemptCount: 1}
	updated, changed, err := daemon.reconcileRunWithTracker(context.Background(), project, wfFile, live, note, lookup.ByID, lookup.ByRecordID)
	if err != nil {
		t.Fatal(err)
	}
	if !changed || updated.LeaseState != string(LeaseStateReleased) || !updated.Terminal || !strings.Contains(updated.LastError, "canonical status backlog") {
		t.Fatalf("genuine task ineligibility did not release live run: %#v changed=%t", updated, changed)
	}
}

func TestWaveDisarm(t *testing.T) {
	vault := authorizedWaveTestVault(t)
	armWaveForTest(t, vault)
	args := Args{"vault": vault, "_pos0": "W-0001", "by": "human:test", "reason": "scope withdrawn", "quiet": "true"}
	if err := waveV7DisarmCmd(args); err != nil {
		t.Fatal(err)
	}
	if err := waveV7DisarmCmd(args); err != nil {
		t.Fatal("disarm not idempotent:", err)
	}
	data, _, _ := parseFrontmatterMustRead(filepath.Join(vault, "work", "waves", "W-0001.md"))
	assertEqual(t, "disarmed", stringField(data, "authorization"), "disarmed")
	assertEqual(t, "", stringField(data, "authorization_fingerprint"), "fingerprint cleared")
}

func TestWaveAuthorizationFingerprint(t *testing.T) {
	vault := authorizedWaveTestVault(t)
	armWaveForTest(t, vault)
	idx, _ := loadV7Index(vault)
	wave := idx.Waves["W-0001"]
	original, _ := waveMaterialFingerprint(vault, idx, wave)
	path := filepath.Join(vault, "work", "tasks", "APP-T-0001.md")
	data, body, err := parseFrontmatterMustRead(path)
	if err != nil {
		t.Fatal(err)
	}
	data["proof_status"] = "satisfied"
	body = strings.Replace(body, "| A1 | command: go test ./cmd/tusker -run TestDeliveryPlanSchemaRoundTrip -count=1 | pending |", "| A1 | command: go test ./cmd/tusker -run TestDeliveryPlanSchemaRoundTrip -count=1 | pass |", 1)
	data["state_rev"] = v7StateRev(data, body)
	content, _ := serializeDocument(data, body, v7FrontmatterOrder["task"])
	if err := writeText(path, content); err != nil {
		t.Fatal(err)
	}
	idx, _ = loadV7Index(vault)
	progress, _ := waveMaterialFingerprint(vault, idx, idx.Waves["W-0001"])
	assertEqual(t, original, progress, "proof progress fingerprint")
	taskChanged := idx
	taskChanged.Tasks = cloneNoteMap(idx.Tasks)
	changedTask := taskChanged.Tasks["APP-T-0001"]
	changedTask.Data = cloneMap(changedTask.Data)
	changedTask.Data["title"] = "Materially changed task intent"
	taskChanged.Tasks["APP-T-0001"] = changedTask
	assertWaveFingerprintChanged(t, vault, taskChanged, taskChanged.Waves["W-0001"], original, "task")
	proofContractChanged := idx
	proofContractChanged.Tasks = cloneNoteMap(idx.Tasks)
	proofTask := proofContractChanged.Tasks["APP-T-0001"]
	proofTask.Data = cloneMap(proofTask.Data)
	proofTask.Data["proof_mode"] = "card"
	proofTask.Data["proof_required"] = []string{"focused_test", "artifact"}
	proofTask.Data["proof_required_owner"] = []string{"agent", "reviewer"}
	proofTask.Data["evidence_budget"] = 2
	proofContractChanged.Tasks["APP-T-0001"] = proofTask
	assertWaveFingerprintChanged(t, vault, proofContractChanged, proofContractChanged.Waves["W-0001"], original, "proof contract")
	memberWave := idx.Waves["W-0001"]
	memberWave.Data = cloneMap(memberWave.Data)
	memberWave.Data["members"] = []string{"APP-T-0001"}
	assertWaveFingerprintChanged(t, vault, idx, memberWave, original, "member set")
	gateChanged := idx
	gateChanged.Gates = map[string]Note{"APP-G-0001": {Data: map[string]any{"id": "APP-G-0001", "status": "open", "owner": "human:test", "blocking": true, "blocks": []string{"APP-T-0001"}, "action": "Supply a credential.", "verification": "Credential works.", "why_agent_cannot": "The credential is unavailable to agents."}}}
	assertWaveFingerprintChanged(t, vault, gateChanged, gateChanged.Waves["W-0001"], original, "gate")
	gateAuthorityChanged := gateChanged
	gateAuthorityChanged.Gates = cloneNoteMap(gateChanged.Gates)
	authorityGate := gateAuthorityChanged.Gates["APP-G-0001"]
	authorityGate.Data = cloneMap(authorityGate.Data)
	authorityGate.Data["gate_kind"] = "decision"
	authorityGate.Data["suggestion"] = "Use the non-destructive option."
	authorityGate.Data["why_agent_cannot"] = "The approved spec leaves two incompatible product choices."
	gateAuthorityChanged.Gates["APP-G-0001"] = authorityGate
	gateFingerprint, _ := waveMaterialFingerprint(vault, gateChanged, gateChanged.Waves["W-0001"])
	assertWaveFingerprintChanged(t, vault, gateAuthorityChanged, gateAuthorityChanged.Waves["W-0001"], gateFingerprint, "gate authority boundary")
	specPath := filepath.Join(v7RepoRoot(vault), "docs", "specs", "delivery.md")
	specBefore := mustReadIndexTest(t, specPath)
	if err := writeText(specPath, specBefore+"\nMaterial intent change.\n"); err != nil {
		t.Fatal(err)
	}
	assertWaveFingerprintChanged(t, vault, idx, idx.Waves["W-0001"], original, "spec")
	if err := writeText(specPath, specBefore); err != nil {
		t.Fatal(err)
	}
	memberSpecPath := filepath.Join(v7RepoRoot(vault), "docs", "specs", "member-only.md")
	if err := writeText(memberSpecPath, "# Member contract\n"); err != nil {
		t.Fatal(err)
	}
	memberSpecIdx := idx
	memberSpecIdx.Tasks = cloneNoteMap(idx.Tasks)
	memberSpecTask := memberSpecIdx.Tasks["APP-T-0001"]
	memberSpecTask.Data = cloneMap(memberSpecTask.Data)
	memberSpecTask.Data["spec_refs"] = append(normalizeList(memberSpecTask.Data["spec_refs"]), "docs/specs/member-only.md")
	memberSpecIdx.Tasks["APP-T-0001"] = memberSpecTask
	memberSpecFingerprint, _ := waveMaterialFingerprint(vault, memberSpecIdx, memberSpecIdx.Waves["W-0001"])
	if err := writeText(memberSpecPath, "# Member contract\n\nChanged intent.\n"); err != nil {
		t.Fatal(err)
	}
	assertWaveFingerprintChanged(t, vault, memberSpecIdx, memberSpecIdx.Waves["W-0001"], memberSpecFingerprint, "member task spec")
	data, body, _ = parseFrontmatterMustRead(path)
	data["dependencies"] = []string{"APP-T-0002:hard"}
	data["state_rev"] = v7StateRev(data, body)
	content, _ = serializeDocument(data, body, v7FrontmatterOrder["task"])
	_ = writeText(path, content)
	idx, _ = loadV7Index(vault)
	projection := waveAuthorizationProjection(vault, idx, idx.Waves["W-0001"])
	assertEqual(t, "stale", stringField(projection, "state"), "material change stales auth")
}

func assertWaveFingerprintChanged(t *testing.T, vault string, idx v7Index, wave Note, original, class string) {
	t.Helper()
	changed, _ := waveMaterialFingerprint(vault, idx, wave)
	if changed == original {
		t.Fatalf("%s change preserved authorization fingerprint", class)
	}
}

func TestWaveAuthorizationProjection(t *testing.T) {
	vault := authorizedWaveTestVault(t)
	idx, _ := loadV7Index(vault)
	wave := idx.Waves["W-0001"]
	payload := v7WavePayload(vault, idx, wave)
	auth := payload["authorization"].(map[string]any)
	assertEqual(t, "disarmed", stringField(auth, "state"), "CLI projection")
	if !strings.Contains(stringField(auth, "action"), "preflight") {
		t.Fatal("projection omitted action")
	}
	snap := serveSnapshot{project: RegisteredProject{VaultRoot: vault}, tasks: sortedV7Tasks(idx), waves: sortedV7Waves(idx), notesByID: map[string]Note{}}
	summary := serveWaveSummaryFor(snap, wave)
	assertEqual(t, "disarmed", stringField(summary.Authorization, "state"), "Serve/Mac projection")
}

func TestInteractiveExecutionContract(t *testing.T) {
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root = filepath.Clean(filepath.Join(root, "..", ".."))
	raw, err := os.ReadFile(filepath.Join(root, "skill", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Join(strings.Fields(string(raw)), " ")
	for _, want := range []string{"implements the requested work itself", "does not require daemon enablement or a daemon lifecycle claim", "Never start `tusker daemon run`"} {
		if !strings.Contains(text, want) {
			t.Fatalf("interactive contract missing %q", want)
		}
	}
}

func authorizedWaveTestVault(t *testing.T) string {
	t.Helper()
	vault := deliveryTestVault(t)
	plan := writeDeliveryTestPlan(t, vault, validDeliveryPlan())
	if err := deliveryImportCmd(Args{"vault": vault, "plan": plan, "wave": "Authorized", "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	return vault
}
func armWaveForTest(t *testing.T, vault string) {
	t.Helper()
	green := greenWaveEnvironment()
	if err := mutateWaveAuthorization(Args{"vault": vault, "_pos0": "W-0001", "by": "human:test", "quiet": "true"}, "armed", &green); err != nil {
		t.Fatal(err)
	}
}

func greenWaveEnvironment() wavePreflightEnvironment {
	return wavePreflightEnvironment{ProjectRegistered: true, ProjectEnabled: true, ProjectHealthy: true, DaemonAlive: true, DaemonReconciling: true, RunnerCompatible: true, SkillCompatible: true, WorkflowCompatible: true, ApprovalFree: true, IsolatedWorkspace: true, IntegrationClean: true}
}
func mapsEqualString(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}
func cloneArgs(in Args) Args {
	out := Args{}
	for k, v := range in {
		out[k] = v
	}
	return out
}
