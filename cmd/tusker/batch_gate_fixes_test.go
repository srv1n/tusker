package main

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBatchGateLedgerRejectsMidRunTreeMutation(t *testing.T) {
	repo := orchestrationGitRepo(t)
	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	project := RegisteredProject{ProjectID: "app", RepoRoot: repo, VaultRoot: t.TempDir()}
	policy := BatchGatePolicy{Commands: []string{"/bin/sh -c 'printf changed >> tracked.txt'"}, FeatureProfile: "canonical"}
	before, err := workspaceTreeStateHash(repo)
	if err != nil {
		t.Fatal(err)
	}

	(&Daemon{store: store}).executeBatchGate(project, policy, BatchGateRun{ID: "batch-mutated", ProjectID: project.ProjectID, StartedAt: time.Now().UTC().Format(time.RFC3339)})

	after, err := workspaceTreeStateHash(repo)
	if err != nil {
		t.Fatal(err)
	}
	if before == after {
		t.Fatal("gate command did not mutate the tree fixture")
	}
	toolchain := scheduledPromotionToolchainFingerprint(repo, policy.Commands)
	for _, tree := range []string{before, after} {
		hit, findErr := store.FindGateLedger(project.ProjectID, tree, policy.Commands[0], policy.FeatureProfile, toolchain)
		if findErr != nil {
			t.Fatal(findErr)
		}
		if hit != nil {
			t.Fatalf("mutated-tree command minted a ledger pass for tree %s: %#v", tree, hit)
		}
	}
	run, err := store.latestBatchGateRun(project.ProjectID)
	if err != nil {
		t.Fatal(err)
	}
	if run == nil || run.Status != "passed" {
		t.Fatalf("green command result was not reported honestly: %#v", run)
	}
}

func TestBatchGateScheduleUsesFinishedAtForMalformedStartedAt(t *testing.T) {
	now := time.Date(2026, 8, 17, 14, 0, 0, 0, time.Local)
	for _, tc := range []struct {
		name   string
		policy BatchGatePolicy
	}{
		{name: "window", policy: BatchGatePolicy{Enabled: true, Windows: []string{"13:00"}, Commands: []string{"true"}}},
		{name: "period", policy: BatchGatePolicy{Enabled: true, PeriodHours: 1, Commands: []string{"true"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := orchestrationGitRepo(t)
			store, err := OpenRuntimeStore(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			if err := store.saveBatchGateRun(BatchGateRun{
				ID: "batch-malformed-" + tc.name, ProjectID: "app", Status: "passed",
				StartedAt: "not-a-timestamp", FinishedAt: now.Add(-30 * time.Minute).Format(time.RFC3339),
			}); err != nil {
				t.Fatal(err)
			}

			wf := defaultWorkflow()
			wf.Orchestration.BatchGate = tc.policy
			if err := (&Daemon{store: store}).scheduleBatchGateIfDue(RegisteredProject{ProjectID: "app", RepoRoot: repo}, wf, now); err != nil {
				t.Fatal(err)
			}
			var count int
			if err := store.queryRowScan(`SELECT COUNT(*) FROM batch_gate_runs WHERE project_id = ?`, []any{"app"}, &count); err != nil {
				t.Fatal(err)
			}
			if count != 1 {
				t.Fatalf("malformed StartedAt ignored FinishedAt and spawned a batch gate: %d runs", count)
			}
		})
	}
}

func TestBatchGateStampsAllFailingCommandsOnActiveWaves(t *testing.T) {
	repo := orchestrationGitRepo(t)
	vault := newWaveTestVault(t, 6)
	mustWave(t, Args{"vault": vault, "quiet": "true", "_pos0": "Active wave 1", "_pos1": "APP-T-0001", "_pos2": "APP-T-0002"}, waveV7CreateCmd)
	mustWave(t, Args{"vault": vault, "quiet": "true", "_pos0": "Active wave 2", "_pos1": "APP-T-0003", "_pos2": "APP-T-0004"}, waveV7CreateCmd)
	mustWave(t, Args{"vault": vault, "quiet": "true", "_pos0": "Inactive wave", "_pos1": "APP-T-0005", "_pos2": "APP-T-0006"}, waveV7CreateCmd)
	setWaveTaskState(t, vault, "APP-T-0005", "done", "done", "2026-08-17T12:00:00Z")
	setWaveTaskState(t, vault, "APP-T-0006", "done", "done", "2026-08-17T12:00:00Z")
	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	commands := []string{
		"/bin/sh -c 'echo FAIL first; exit 1'",
		"/bin/sh -c 'echo FAIL second; exit 1'",
	}
	(&Daemon{store: store}).executeBatchGate(
		RegisteredProject{ProjectID: "app", RepoRoot: repo, VaultRoot: vault},
		BatchGatePolicy{Commands: commands, FeatureProfile: "canonical"},
		BatchGateRun{ID: "batch-two-failures", ProjectID: "app", StartedAt: time.Now().UTC().Format(time.RFC3339)},
	)

	want := strings.Join(commands, "\n")
	for _, waveID := range []string{"W-0001", "W-0002"} {
		data, _, readErr := parseFrontmatterMustRead(filepath.Join(vault, "work", "waves", waveID+".md"))
		if readErr != nil {
			t.Fatal(readErr)
		}
		if got := stringField(data, buildFailedCommandField); got != want {
			t.Fatalf("%s marker did not contain the complete failing command set: got %q want %q", waveID, got, want)
		}
	}
	inactive, _, err := parseFrontmatterMustRead(filepath.Join(vault, "work", "waves", "W-0003.md"))
	if err != nil {
		t.Fatal(err)
	}
	if got := stringField(inactive, buildFailedCommandField); got != "" {
		t.Fatalf("inactive wave received a red marker: %q", got)
	}
}

func TestBatchGateMultilineMarkerClearsOnNextGreenRun(t *testing.T) {
	repo := orchestrationGitRepo(t)
	vault := newWaveTestVault(t, 2)
	mustWave(t, Args{"vault": vault, "quiet": "true", "_pos0": "Round trip", "_pos1": "APP-T-0001", "_pos2": "APP-T-0002"}, waveV7CreateCmd)
	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	command := "if [ -e gate-pass ]; then\n  exit 0\nfi\ntouch gate-pass\nprintf 'FAIL first\\n'\nexit 1"
	project := RegisteredProject{ProjectID: "app", RepoRoot: repo, VaultRoot: vault}
	policy := BatchGatePolicy{Commands: []string{command}, FeatureProfile: "canonical"}
	d := &Daemon{store: store}
	d.executeBatchGate(project, policy, BatchGateRun{ID: "batch-red", ProjectID: "app", StartedAt: time.Now().UTC().Format(time.RFC3339)})
	path := filepath.Join(vault, "work", "waves", "W-0001.md")
	data, _, err := parseFrontmatterMustRead(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := stringField(data, buildFailedCommandField); got != command {
		t.Fatalf("multi-line failure marker was not preserved: got %q want %q", got, command)
	}

	d.executeBatchGate(project, policy, BatchGateRun{ID: "batch-green", ProjectID: "app", StartedAt: time.Now().UTC().Format(time.RFC3339)})
	data, _, err = parseFrontmatterMustRead(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := stringField(data, buildFailedCommandField); got != "" || boolField(data, buildFailedField) {
		t.Fatalf("next green run did not clear the multi-line marker: %#v", data)
	}
}

func TestClearBuildFailedMarkersClearsLegacyAfterFullRun(t *testing.T) {
	vault := newWaveTestVault(t, 1)
	taskPath := filepath.Join(vault, "work", "tasks", "APP-T-0001.md")
	if _, _, err := mutateV7DocumentLocked(taskPath, v7FrontmatterOrder["task"], func(data map[string]any, body string) (map[string]any, string, bool, error) {
		data[buildFailedField] = true
		delete(data, buildFailedCommandField)
		delete(data, buildFailedProfileField)
		return data, body, true, nil
	}); err != nil {
		t.Fatal(err)
	}

	if err := clearBuildFailedMarkers(vault, nil, "canonical"); err != nil {
		t.Fatal(err)
	}
	data, _, err := parseFrontmatterMustRead(taskPath)
	if err != nil {
		t.Fatal(err)
	}
	if !boolField(data, buildFailedField) {
		t.Fatal("partial green run cleared a legacy marker")
	}
	if err := clearBuildFailedMarkers(vault, []string{"true"}, "canonical"); err != nil {
		t.Fatal(err)
	}
	data, _, err = parseFrontmatterMustRead(taskPath)
	if err != nil {
		t.Fatal(err)
	}
	if boolField(data, buildFailedField) {
		t.Fatal("full green run did not clear a legacy marker")
	}
}

func TestRunGateTierSkipsPassOnTreeMutationAndChainsHashes(t *testing.T) {
	exec := &recordingGateExec{outputs: map[string]string{"first": "ok", "second": "ok"}}
	rt := gateTestRuntime(exec)
	hashes := []string{"tree-1", "tree-2", "tree-2"}
	var hashCalls int
	rt.TreeHash = func(string) (string, error) {
		if hashCalls >= len(hashes) {
			t.Fatalf("tree hash called too many times: %d", hashCalls+1)
		}
		value := hashes[hashCalls]
		hashCalls++
		return value, nil
	}
	var recorded []string
	rt.RecordPass = func(command, treeHash, _ string, _ string, _ time.Duration) {
		recorded = append(recorded, command+"@"+treeHash)
	}
	result, err := runGateTier(GateTierPolicy{HarvestCommands: []string{"first", "second"}, AllowDirtyTree: true}, "", rt)
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != gateOutcomePassed || hashCalls != 3 || len(recorded) != 1 || recorded[0] != "second@tree-2" {
		t.Fatalf("tree mutation was reusable or hash chain doubled work: result=%#v calls=%d recorded=%#v", result, hashCalls, recorded)
	}
}
