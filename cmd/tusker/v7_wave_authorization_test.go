package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestWaveIntegrationBaseClean(t *testing.T) {
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

func authorizedWaveTestVault(t *testing.T) string {
	t.Helper()
	vault := v7DirectTestVault(t)
	if err := writeText(filepath.Join(v7RepoRoot(vault), "docs", "specs", "delivery.md"), "# Delivery spec\n"); err != nil {
		t.Fatal(err)
	}
	specRefs := []any{"docs/specs/delivery.md"}
	writeDirectTask(t, vault, "APP-T-0001", "W-0001", map[string]any{
		"status": "ready", "readiness": "ready", "spec_refs": specRefs,
		"owned_paths": []any{"cmd/tusker/app-t-0001.go"},
	})
	writeDirectTask(t, vault, "APP-T-0002", "W-0001", map[string]any{
		"status": "ready", "readiness": "ready",
		"owned_paths":       []any{"cmd/tusker/app-t-0002.go"},
		"dependencies":      []any{"APP-T-0001:hard"},
		"artifact_contract": map[string]any{"kind": "diff_summary", "path": "cmd/tusker/app-t-0002.go", "summary": "Second task."},
	})
	writeDirectWave(t, vault, "W-0001", []string{"APP-T-0001", "APP-T-0002"}, map[string]any{"spec_refs": specRefs})
	return vault
}

func TestWaveArmAuthorizesCriticalMemberDispatch(t *testing.T) {
	vault := v7DirectTestVault(t)
	specDoc := "---\nsubject: direct-authoring-test\ntitle: \"Direct authoring test spec\"\nstatus: canonical\n---\n\n# Direct authoring test spec\n"
	if err := writeText(filepath.Join(vault, "specs", "direct-authoring-test.md"), specDoc); err != nil {
		t.Fatal(err)
	}
	writeDirectTaskBody(t, vault, "APP-T-0001", "W-0001", map[string]any{"status": "ready", "readiness": "ready", "risk": "critical", "spec_refs": []any{".tusker/specs/direct-authoring-test.md"}}, directDispatchableTaskBody("APP-T-0001"))
	writeDirectTaskBody(t, vault, "APP-T-0002", "W-0001", map[string]any{"status": "ready", "readiness": "ready", "dependencies": []any{"APP-T-0001:hard"}}, directDispatchableTaskBody("APP-T-0002"))
	writeDirectWave(t, vault, "W-0001", []string{"APP-T-0001", "APP-T-0002"}, nil)
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

func TestWaveAuthorizationIgnoresAppendedProofRows(t *testing.T) {
	vault := authorizedWaveTestVault(t)
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
	body = strings.TrimRight(body, "\n") + "\n| A1 | typed review attempt-123 | pass | [tusker-review-result:sha256:receipt] accepted |\n"
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
		t.Fatalf("proof-only verification row invalidated authorization: before=%s after=%s issues=%#v", before, after, issues)
	}
}

func TestWaveMaterialTableIgnoresDerivedReviewReceipt(t *testing.T) {
	contract := `| Covers | Check | Result | Notes |
|---|---|---|---|
| A1 | command: test -s artifact.json | pending | Planned proof. |`
	withReceipt := contract + "\n| A1 | typed review attempt-123 | pass | [tusker-review-result:sha256:receipt] accepted |"
	if got, want := waveMaterialTable(withReceipt, []int{0, 1}), waveMaterialTable(contract, []int{0, 1}); !reflect.DeepEqual(got, want) {
		t.Fatalf("derived review receipt changed wave material: got=%#v want=%#v", got, want)
	}
	changed := strings.Replace(contract, "artifact.json", "different.json", 1)
	if reflect.DeepEqual(waveMaterialTable(changed, []int{0, 1}), waveMaterialTable(contract, []int{0, 1})) {
		t.Fatal("an authored verification command change escaped wave material")
	}
}

func TestWaveMaterialFingerprintBindsTaskComplexity(t *testing.T) {
	vault := authorizedWaveTestVault(t)
	setAutomationV7TaskFields(t, vault, "APP-T-0001", map[string]any{"complexity": "routine"})
	idx, err := loadV7Index(vault)
	if err != nil {
		t.Fatal(err)
	}
	before, issues := waveMaterialFingerprint(vault, idx, idx.Waves["W-0001"])
	if len(issues) != 0 {
		t.Fatalf("fingerprint issues: %#v", issues)
	}
	setAutomationV7TaskFields(t, vault, "APP-T-0001", map[string]any{"complexity": "frontier"})
	idx, err = loadV7Index(vault)
	if err != nil {
		t.Fatal(err)
	}
	after, issues := waveMaterialFingerprint(vault, idx, idx.Waves["W-0001"])
	if len(issues) != 0 || after == before {
		t.Fatalf("complexity did not invalidate material: before=%s after=%s issues=%#v", before, after, issues)
	}
}

func TestWaveMaterialFingerprintBindsTaskRouteSelection(t *testing.T) {
	vault := authorizedWaveTestVault(t)
	idx, err := loadV7Index(vault)
	if err != nil {
		t.Fatal(err)
	}
	before, issues := waveMaterialFingerprint(vault, idx, idx.Waves["W-0001"])
	if len(issues) != 0 {
		t.Fatalf("fingerprint issues: %#v", issues)
	}
	for field, value := range map[string]any{"work_level": "demanding", "review_level": "light", "review_reason": "bounded review", "execute_profile": "execute-frontier", "review_profile": "review-independent"} {
		setAutomationV7TaskFields(t, vault, "APP-T-0001", map[string]any{field: value})
		idx, err = loadV7Index(vault)
		if err != nil {
			t.Fatal(err)
		}
		after, nextIssues := waveMaterialFingerprint(vault, idx, idx.Waves["W-0001"])
		if len(nextIssues) != 0 || after == before {
			t.Fatalf("%s did not invalidate wave authority: before=%s after=%s issues=%#v", field, before, after, nextIssues)
		}
		before = after
	}
}

func TestWavePause(t *testing.T) {
	vault, store, _ := authorityFixture(t)
	writeDirectTask(t, vault, "APP-T-0001", "W-0001", map[string]any{"status": "ready", "readiness": "ready"})
	writeDirectTask(t, vault, "APP-T-0002", "W-0001", map[string]any{"status": "ready", "readiness": "ready", "dependencies": []any{"APP-T-0001:hard"}})
	writeDirectWave(t, vault, "W-0001", []string{"APP-T-0001", "APP-T-0002"}, nil)
	armWaveForTest(t, vault)
	if _, err := directWavePause(vault, store, "W-0001", "human:test"); err != nil {
		t.Fatal(err)
	}
	if _, err := directWavePause(vault, store, "W-0001", "human:test"); err != nil {
		t.Fatal("pause not idempotent:", err)
	}
	idx, _ := loadV7Index(vault)
	task := idx.Tasks["APP-T-0001"]
	blockers := v7TaskDispatchBlockers(vault, task)
	if !strings.Contains(strings.Join(blockers, " "), "paused") {
		t.Fatalf("pause did not stop future claims: %#v", blockers)
	}
	if _, err := directWaveResume(vault, store, "W-0001", "human:test"); err != nil {
		t.Fatal(err)
	}
	idx, _ = loadV7Index(vault)
	if got := stringField(waveAuthorizationProjection(vault, idx, idx.Waves["W-0001"]), "state"); got != "armed" {
		t.Fatalf("resume=%s", got)
	}
}

func TestWavePauseAndStalePreserveLiveDaemonRuns(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(t *testing.T, vault string, store *RuntimeStore)
	}{
		{name: "paused", mutate: func(t *testing.T, vault string, store *RuntimeStore) {
			if _, err := directWavePause(vault, store, "W-0001", "human:test"); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "stale", mutate: func(t *testing.T, vault string, store *RuntimeStore) {
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
			vault, store, project := authorityFixture(t)
			writeDirectTaskBody(t, vault, "APP-T-0001", "W-0001", map[string]any{"status": "ready", "readiness": "ready"}, directDispatchableTaskBody("APP-T-0001"))
			writeDirectTaskBody(t, vault, "APP-T-0002", "W-0001", map[string]any{"status": "ready", "readiness": "ready", "dependencies": []any{"APP-T-0001:hard"}}, directDispatchableTaskBody("APP-T-0002"))
			writeDirectWave(t, vault, "W-0001", []string{"APP-T-0001", "APP-T-0002"}, nil)
			armWaveForTest(t, vault)
			tc.mutate(t, vault, store)
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
			daemon := &Daemon{stateRoot: DefaultStateRoot(), store: store, processIdentityProbe: func(RunStatus) bool { return true }}
			for _, lease := range []LeaseState{LeaseStateClaimed, LeaseStateRunning} {
				now := time.Now().UTC().Format(time.RFC3339)
				live := RunStatus{ProjectID: project.ProjectID, RecordID: "APP-T-0001", ItemID: "APP-T-0001", Runner: wfFile.Data.Agents.Default, Lane: runLaneExecute, LeaseState: string(lease), LeaseOwner: "attempt-live", LeaseGeneration: 7, ActiveAttemptID: "attempt-live", AttemptCount: 3, ProcessPID: 1234, ProcessPGID: 1234, ProcessStartedAt: now, StartedAt: now, FirstEventAt: now, LastEventAt: now, LastHeartbeatAt: now}
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
			retry := RunStatus{ProjectID: project.ProjectID, RecordID: "APP-T-0001", ItemID: "APP-T-0001", Runner: wfFile.Data.Agents.Default, Lane: runLaneExecute, LeaseState: string(LeaseStateRetryQueued), NextRetryAt: time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)}
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
	vault, store, project := authorityFixture(t)
	writeDirectTask(t, vault, "APP-T-0001", "W-0001", map[string]any{"status": "ready", "readiness": "ready"})
	writeDirectWave(t, vault, "W-0001", []string{"APP-T-0001"}, nil)
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
	daemon := &Daemon{stateRoot: DefaultStateRoot(), store: store}
	live := RunStatus{ProjectID: project.ProjectID, RecordID: "APP-T-0001", ItemID: "APP-T-0001", Runner: wfFile.Data.Agents.Default, Lane: runLaneExecute, LeaseState: string(LeaseStateClaimed), LeaseOwner: "attempt-live", LeaseGeneration: 7, ActiveAttemptID: "attempt-live", AttemptCount: 1}
	updated, changed, err := daemon.reconcileRunWithTracker(context.Background(), project, wfFile, live, note, lookup.ByID, lookup.ByRecordID)
	if err != nil {
		t.Fatal(err)
	}
	if !changed || updated.LeaseState != string(LeaseStateReleased) || !updated.Terminal || !strings.Contains(updated.LastError, "canonical status backlog") {
		t.Fatalf("genuine task ineligibility did not release live run: %#v changed=%t", updated, changed)
	}
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
	body = strings.Replace(body, "| A1 | command: go test ./x | pending |", "| A1 | command: go test ./x | pass |", 1)
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
	if !strings.Contains(stringField(auth, "action"), "wave start") {
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
	for _, want := range []string{"Interactive sessions implement work through interactive claims", "never launch a daemon or nested worker", "TUSKER_ATTEMPT_ID"} {
		if !strings.Contains(text, want) {
			t.Fatalf("interactive contract missing %q", want)
		}
	}
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
