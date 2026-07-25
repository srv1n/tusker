package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type fakeGateLedger struct {
	hits  map[string]*GateLedgerEntry
	calls int
}

func (f *fakeGateLedger) FindGateLedger(projectID, treeHash, command, profile, toolchain string) (*GateLedgerEntry, error) {
	f.calls++
	return f.hits[projectID+"|"+treeHash+"|"+command+"|"+profile+"|"+toolchain], nil
}

type recordingGateExec struct {
	ran     []string
	outputs map[string]string
	fail    map[string]bool
}

func (r *recordingGateExec) run(_ string, command string) (string, error) {
	r.ran = append(r.ran, command)
	if r.fail[command] {
		return r.outputs[command], errors.New("exit status 1")
	}
	return r.outputs[command], nil
}

func gateTestRuntime(exec *recordingGateExec) gateTierRuntime {
	return gateTierRuntime{
		ProjectID:  "app",
		Workspace:  "/repo",
		TreeHash:   func(string) (string, error) { return "tree-1", nil },
		Toolchain:  func(string, []string) string { return "toolchain-1" },
		FreeDiskGB: func(string) (float64, error) { return 500, nil },
		SlotHolder: func(string, []string) (string, bool) { return "", false },
		TreeStatus: func(string) (string, error) { return "", nil },
		Exec:       exec.run,
		Now:        func() time.Time { return time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC) },
	}
}

func TestGateHarvestModeCollectsEveryFailureInOnePass(t *testing.T) {
	exec := &recordingGateExec{
		outputs: map[string]string{
			"first":  "error: first blew up",
			"second": "error: second blew up",
			"third":  "ok",
		},
		fail: map[string]bool{"first": true, "second": true},
	}
	policy := GateTierPolicy{HarvestCommands: []string{"first", "second", "third"}, AllowDirtyTree: true}
	result, err := runGateTier(policy, "", gateTestRuntime(exec))
	if err != nil {
		t.Fatal(err)
	}
	if result.Mode != gateTierModeHarvest || result.Outcome != gateOutcomeFailed {
		t.Fatalf("gate did not report a harvested failure: %#v", result)
	}
	if len(exec.ran) != 3 {
		t.Fatalf("gate stopped early instead of harvesting: ran %#v", exec.ran)
	}
	if len(result.Defects) != 2 {
		t.Fatalf("expected the complete failure set, got %#v", result.Defects)
	}
	if result.Defects[0].Command != "first" || result.Defects[1].Command != "second" {
		t.Fatalf("failure set is not attributable to its commands: %#v", result.Defects)
	}
}

func TestGateHarvestModePassRecordsLedger(t *testing.T) {
	exec := &recordingGateExec{outputs: map[string]string{"all-green": "ok"}}
	rt := gateTestRuntime(exec)
	var recorded []string
	rt.RecordPass = func(command, treeHash, profile, toolchain string, _ time.Duration) {
		recorded = append(recorded, command+"@"+treeHash+"#"+profile+"$"+toolchain)
	}
	policy := GateTierPolicy{HarvestCommands: []string{"all-green"}, Profile: "canonical", AllowDirtyTree: true}
	result, err := runGateTier(policy, "", rt)
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != gateOutcomePassed || len(recorded) != 1 || recorded[0] != "all-green@tree-1#canonical$toolchain-1" {
		t.Fatalf("green gate was not ledgered for the next run: %#v %#v", result, recorded)
	}
}

func TestGatePreflightRefusalNamesCauseAndRemedy(t *testing.T) {
	cases := []struct {
		name    string
		policy  GateTierPolicy
		profile string
		mutate  func(*gateTierRuntime)
		cause   string
		detail  string
	}{
		{
			name:   "disk headroom",
			policy: GateTierPolicy{HarvestCommands: []string{"gate"}, MinFreeDiskGB: 25},
			mutate: func(rt *gateTierRuntime) {
				rt.FreeDiskGB = func(string) (float64, error) { return 3.5, nil }
			},
			cause:  gateRefusalDiskHeadroom,
			detail: "below the configured 25.0 GB floor",
		},
		{
			name:   "build slot held",
			policy: GateTierPolicy{HarvestCommands: []string{"gate"}, BuildSlotLocks: []string{".cargo-build.lock"}},
			mutate: func(rt *gateTierRuntime) {
				rt.SlotHolder = func(string, []string) (string, bool) { return ".cargo-build.lock", true }
			},
			cause:  gateRefusalBuildSlotHeld,
			detail: ".cargo-build.lock",
		},
		{
			name:    "profile parity",
			policy:  GateTierPolicy{HarvestCommands: []string{"gate"}, Profile: "cloud-index"},
			profile: "minimal",
			cause:   gateRefusalProfileParity,
			detail:  "cloud-index",
		},
		{
			name:   "tree not frozen",
			policy: GateTierPolicy{HarvestCommands: []string{"gate"}},
			mutate: func(rt *gateTierRuntime) {
				rt.TreeStatus = func(string) (string, error) { return " M cmd/tusker/cli.go\n?? scratch.go\n", nil }
			},
			cause:  gateRefusalTreeNotFrozen,
			detail: "cmd/tusker/cli.go",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			exec := &recordingGateExec{outputs: map[string]string{}}
			rt := gateTestRuntime(exec)
			if testCase.mutate != nil {
				testCase.mutate(&rt)
			}
			policy := testCase.policy
			if testCase.cause != gateRefusalTreeNotFrozen {
				policy.AllowDirtyTree = true
			}
			result, err := runGateTier(policy, testCase.profile, rt)
			if err != nil {
				t.Fatal(err)
			}
			if result.Outcome != gateOutcomeRefused || result.Refusal == nil {
				t.Fatalf("preflight did not refuse: %#v", result)
			}
			if result.Refusal.Cause != testCase.cause {
				t.Fatalf("wrong cause: %#v", result.Refusal)
			}
			if !strings.Contains(result.Refusal.Detail, testCase.detail) {
				t.Fatalf("detail does not name the cause: %#v", result.Refusal)
			}
			if strings.TrimSpace(result.Refusal.Remedy) == "" {
				t.Fatalf("refusal carries no remedy: %#v", result.Refusal)
			}
			if len(exec.ran) != 0 {
				t.Fatalf("refusal happened after expensive work: %#v", exec.ran)
			}
		})
	}
}

func TestGatePreflightRefusalOrderPrefersCheapestCause(t *testing.T) {
	exec := &recordingGateExec{outputs: map[string]string{}}
	rt := gateTestRuntime(exec)
	rt.FreeDiskGB = func(string) (float64, error) { return 1, nil }
	rt.SlotHolder = func(string, []string) (string, bool) { return "lock", true }
	rt.TreeStatus = func(string) (string, error) { return " M a.go\n", nil }
	policy := GateTierPolicy{HarvestCommands: []string{"gate"}, MinFreeDiskGB: 25, BuildSlotLocks: []string{"lock"}, Profile: "canonical"}
	result, err := runGateTier(policy, "other", rt)
	if err != nil {
		t.Fatal(err)
	}
	if result.Refusal == nil || result.Refusal.Cause != gateRefusalDiskHeadroom {
		t.Fatalf("preflight order is not doctrine order: %#v", result.Refusal)
	}
}

func TestGatePreflightBuildSlotDetectsRealLockFile(t *testing.T) {
	workspace := t.TempDir()
	if _, held := heldBuildSlot(workspace, []string{"build.lock"}); held {
		t.Fatal("absent lock reported as held")
	}
	if err := os.WriteFile(filepath.Join(workspace, "build.lock"), []byte("pid 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	lock, held := heldBuildSlot(workspace, []string{"build.lock"})
	if !held || lock != "build.lock" {
		t.Fatalf("present lock not detected: %q %v", lock, held)
	}
}

func TestGateDefectListCarriesCommandTargetAndDiagnostic(t *testing.T) {
	output := strings.Join([]string{
		"running 3 tests",
		"--- FAIL: TestAlpha (0.01s)",
		"    alpha_test.go:12: expected 3, got 4",
		"--- FAIL: TestBeta (0.02s)",
		"    beta_test.go:44: nil pointer dereference",
		"FAIL",
	}, "\n")
	exec := &recordingGateExec{outputs: map[string]string{"go test ./...": output}, fail: map[string]bool{"go test ./...": true}}
	policy := GateTierPolicy{
		HarvestCommands:   []string{"go test ./..."},
		DefectTargetRegex: `^--- FAIL: (\S+)`,
		AllowDirtyTree:    true,
	}
	result, err := runGateTier(policy, "", gateTestRuntime(exec))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Defects) != 2 {
		t.Fatalf("defect list did not split by target: %#v", result.Defects)
	}
	if result.Defects[0].Target != "TestAlpha" || result.Defects[1].Target != "TestBeta" {
		t.Fatalf("targets not harvested: %#v", result.Defects)
	}
	if !strings.Contains(result.Defects[0].Excerpt, "alpha_test.go:12") || !strings.Contains(result.Defects[1].Excerpt, "beta_test.go:44") {
		t.Fatalf("excerpt lost the first actionable diagnostic: %#v", result.Defects)
	}
	for _, defect := range result.Defects {
		if defect.Command != "go test ./..." {
			t.Fatalf("defect is not attributable to its command: %#v", defect)
		}
	}
}

func TestGateDefectListWithoutConfiguredMarkerFallsBackToCommand(t *testing.T) {
	exec := &recordingGateExec{
		outputs: map[string]string{"make check": "noise\nerror: link failed\n"},
		fail:    map[string]bool{"make check": true},
	}
	policy := GateTierPolicy{HarvestCommands: []string{"make check"}, AllowDirtyTree: true}
	result, err := runGateTier(policy, "", gateTestRuntime(exec))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Defects) != 1 || result.Defects[0].Target != "make check" {
		t.Fatalf("unmarked failure lost its target: %#v", result.Defects)
	}
	if !strings.Contains(result.Defects[0].Excerpt, "error: link failed") || strings.Contains(result.Defects[0].Excerpt, "noise") {
		t.Fatalf("excerpt is not the actionable diagnostic: %#v", result.Defects[0])
	}
}

func TestGateDefectListCapsExcerptLines(t *testing.T) {
	lines := []string{"--- FAIL: TestBig"}
	for i := 0; i < 40; i++ {
		lines = append(lines, "detail line")
	}
	defects := harvestGateDefects("cmd", strings.Join(lines, "\n"), errors.New("exit 1"), GateTierPolicy{DefectTargetRegex: `^--- FAIL: (\S+)`, DefectLineLimit: 4})
	if len(defects) != 1 || strings.Count(defects[0].Excerpt, "\n")+1 != 4 {
		t.Fatalf("excerpt not capped: %#v", defects)
	}
}

func TestGateLedgerShortCircuitOnMatchingTreeState(t *testing.T) {
	exec := &recordingGateExec{outputs: map[string]string{}}
	rt := gateTestRuntime(exec)
	ledger := &fakeGateLedger{hits: map[string]*GateLedgerEntry{
		"app|tree-1|make test|canonical|toolchain-1": {ID: "gate-abc", Command: "make test", Profile: "canonical", Toolchain: "toolchain-1", TreeHash: "tree-1"},
	}}
	rt.Ledger = ledger
	policy := GateTierPolicy{HarvestCommands: []string{"make test"}, Profile: "canonical"}
	result, err := runGateTier(policy, "", rt)
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != gateOutcomeLedgerHit {
		t.Fatalf("passing tree state did not short-circuit: %#v", result)
	}
	if len(result.LedgerHits) != 1 || result.LedgerHits[0] != "make test" {
		t.Fatalf("hit not reported: %#v", result)
	}
	if len(exec.ran) != 0 {
		t.Fatalf("ledger hit still re-ran the gate: %#v", exec.ran)
	}
}

func TestGateLedgerShortCircuitSkipsOnlyHitCommands(t *testing.T) {
	exec := &recordingGateExec{outputs: map[string]string{"make vet": "ok"}}
	rt := gateTestRuntime(exec)
	rt.Ledger = &fakeGateLedger{hits: map[string]*GateLedgerEntry{
		"app|tree-1|make test||toolchain-1": {ID: "gate-abc", Command: "make test", Toolchain: "toolchain-1", TreeHash: "tree-1"},
	}}
	policy := GateTierPolicy{HarvestCommands: []string{"make test", "make vet"}, AllowDirtyTree: true}
	result, err := runGateTier(policy, "", rt)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.LedgerHits) != 1 || len(exec.ran) != 1 || exec.ran[0] != "make vet" {
		t.Fatalf("partial ledger hit mis-scheduled the pass: %#v %#v", result, exec.ran)
	}
	if result.Outcome != gateOutcomePassed {
		t.Fatalf("unexpected outcome: %#v", result)
	}
}

func TestGateLedgerShortCircuitUsesRuntimeStore(t *testing.T) {
	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	// The seam is satisfied by the real ORC-T-0005 ledger, not only by fakes.
	var reader gateLedgerReader = store
	if err := store.RecordGateLedger(GateLedgerEntry{ID: "gate-real", ProjectID: "app", TreeHash: "tree-1", Command: "make test", Profile: "canonical", Toolchain: "toolchain-1"}); err != nil {
		t.Fatal(err)
	}
	exec := &recordingGateExec{outputs: map[string]string{}}
	rt := gateTestRuntime(exec)
	rt.Ledger = reader
	result, err := runGateTier(GateTierPolicy{HarvestCommands: []string{"make test"}, Profile: "canonical"}, "", rt)
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != gateOutcomeLedgerHit || len(exec.ran) != 0 {
		t.Fatalf("real ledger did not short-circuit: %#v", result)
	}
}

func TestGateTierToolchainChangeMissesSameTreeLedger(t *testing.T) {
	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.RecordGateLedger(GateLedgerEntry{ID: "old", ProjectID: "app", TreeHash: "tree-1", Command: "make test", Profile: "canonical", Toolchain: "go1"}); err != nil {
		t.Fatal(err)
	}
	exec := &recordingGateExec{outputs: map[string]string{"make test": "ok"}}
	rt := gateTestRuntime(exec)
	rt.Ledger = store
	rt.Toolchain = func(string, []string) string { return "go2" }
	result, err := runGateTier(GateTierPolicy{HarvestCommands: []string{"make test"}, Profile: "canonical", AllowDirtyTree: true}, "", rt)
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != gateOutcomePassed || result.Toolchain != "go2" || len(exec.ran) != 1 {
		t.Fatalf("changed toolchain reused stale proof: result=%#v ran=%#v", result, exec.ran)
	}
}

func TestGateTierPolicyFallsBackToBatchGateCommands(t *testing.T) {
	wf := defaultWorkflow()
	wf.Orchestration.BatchGate.Commands = []string{"make test"}
	wf.Orchestration.BatchGate.FeatureProfile = "canonical"
	policy := resolveGateTierPolicy(wf)
	if len(policy.HarvestCommands) != 1 || policy.HarvestCommands[0] != "make test" || policy.Profile != "canonical" {
		t.Fatalf("project declares its harvest command once: %#v", policy)
	}
	wf.Orchestration.Gate = GateTierPolicy{HarvestCommands: []string{"cargo nextest run --no-fail-fast"}, Profile: "cloud-index"}
	policy = resolveGateTierPolicy(wf)
	if policy.HarvestCommands[0] != "cargo nextest run --no-fail-fast" || policy.Profile != "cloud-index" {
		t.Fatalf("explicit gate policy did not win: %#v", policy)
	}
}

func TestGateTierRequiresConfiguredHarvestCommand(t *testing.T) {
	exec := &recordingGateExec{outputs: map[string]string{}}
	if _, err := runGateTier(GateTierPolicy{}, "", gateTestRuntime(exec)); err == nil || !strings.Contains(err.Error(), "harvest_commands") {
		t.Fatalf("unconfigured gate was not actionable: %v", err)
	}
}

func TestGateTierFrozenTreeAgainstRealRepo(t *testing.T) {
	repo := orchestrationGitRepo(t)
	exec := &recordingGateExec{outputs: map[string]string{"true": ""}}
	rt := defaultGateTierRuntime(nil, "app", repo)
	rt.Exec = exec.run
	policy := GateTierPolicy{HarvestCommands: []string{"true"}}
	result, err := runGateTier(policy, "", rt)
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != gateOutcomePassed {
		t.Fatalf("clean repo refused: %#v", result.Refusal)
	}
	if err := os.WriteFile(filepath.Join(repo, "tracked.txt"), []byte("dirty\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err = runGateTier(policy, "", rt)
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != gateOutcomeRefused || result.Refusal.Cause != gateRefusalTreeNotFrozen {
		t.Fatalf("dirty repo was gated anyway: %#v", result)
	}
}
