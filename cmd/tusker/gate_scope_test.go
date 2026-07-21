package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// scopedGateRuntime extends the shared gate test runtime with a diff boundary
// that reports a fixed set of touched paths.
func scopedGateRuntime(exec *recordingGateExec, touched []string) gateTierRuntime {
	rt := gateTestRuntime(exec)
	rt.DiffPaths = func(string, string) ([]string, error) { return touched, nil }
	return rt
}

func twoAreaScopes() []GateScope {
	return []GateScope{
		{Name: "api", Paths: []string{"internal/api"}, Commands: []string{"go test ./internal/api/..."}},
		{Name: "store", Paths: []string{"internal/store"}, Commands: []string{"go test ./internal/store/..."}},
	}
}

// A1: a per-change gate builds, styles, and tests only the areas the change
// touched. A change under internal/api runs the api scope's command and no
// other.
func TestSelectiveGateScopesToTouched(t *testing.T) {
	exec := &recordingGateExec{outputs: map[string]string{
		"go test ./internal/api/...":   "ok",
		"go test ./internal/store/...": "ok",
	}}
	rt := scopedGateRuntime(exec, []string{"internal/api/handler.go"})
	policy := GateTierPolicy{AllowDirtyTree: true}

	result, err := runSelectiveGateTier(policy, twoAreaScopes(), "main", "", rt)
	if err != nil {
		t.Fatal(err)
	}
	if result.Mode != gateTierModeSelective {
		t.Fatalf("per-change gate did not run in selective mode: %#v", result)
	}
	if result.Outcome != gateOutcomePassed {
		t.Fatalf("scoped gate did not pass: %#v", result)
	}
	if len(exec.ran) != 1 || exec.ran[0] != "go test ./internal/api/..." {
		t.Fatalf("selective gate did not run only the touched area's command: ran %#v", exec.ran)
	}
	if len(result.Scopes) != 1 || result.Scopes[0] != "api" {
		t.Fatalf("selected scope set is wrong: %#v", result.Scopes)
	}
}

// A2: areas the change did not touch are skipped. The store area's command must
// never run when the change is confined to internal/api.
func TestSelectiveGateSkipsUntouched(t *testing.T) {
	exec := &recordingGateExec{outputs: map[string]string{
		"go test ./internal/api/...":   "ok",
		"go test ./internal/store/...": "ok",
	}}
	rt := scopedGateRuntime(exec, []string{"internal/api/handler.go"})
	policy := GateTierPolicy{AllowDirtyTree: true}

	if _, err := runSelectiveGateTier(policy, twoAreaScopes(), "main", "", rt); err != nil {
		t.Fatal(err)
	}
	for _, command := range exec.ran {
		if command == "go test ./internal/store/..." {
			t.Fatalf("untouched store area was not skipped: ran %#v", exec.ran)
		}
	}

	// A change touching a path no scope owns must fail closed to the full harvest
	// set, not skip and pass: an unscoped path is a coverage gap.
	full := []string{"go build ./...", "go test ./..."}
	outputs := map[string]string{}
	for _, command := range full {
		outputs[command] = "ok"
	}
	exec = &recordingGateExec{outputs: outputs}
	rt = scopedGateRuntime(exec, []string{"docs/README.md"})
	fallbackPolicy := GateTierPolicy{HarvestCommands: full, AllowDirtyTree: true}
	result, err := runSelectiveGateTier(fallbackPolicy, twoAreaScopes(), "main", "", rt)
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != gateOutcomePassed {
		t.Fatalf("unscoped touched path full fallback did not pass: %#v", result)
	}
	if len(exec.ran) != len(full) {
		t.Fatalf("unscoped touched path should fall back to the full harvest set: ran %#v", exec.ran)
	}
	if result.Fallback == "" {
		t.Fatalf("full fallback must record why it ran the whole gate: %#v", result)
	}
}

// A3: the batch-landing (wave-end) gate still runs the full harvest set,
// unchanged by the per-change narrowing.
func TestWaveEndGateStillRunsAll(t *testing.T) {
	full := []string{
		"go test ./internal/api/...",
		"go test ./internal/store/...",
		"go vet ./...",
	}
	outputs := map[string]string{}
	for _, command := range full {
		outputs[command] = "ok"
	}
	exec := &recordingGateExec{outputs: outputs}
	policy := GateTierPolicy{HarvestCommands: full, AllowDirtyTree: true}

	result, err := runGateTier(policy, "", gateTestRuntime(exec))
	if err != nil {
		t.Fatal(err)
	}
	if result.Mode != gateTierModeHarvest {
		t.Fatalf("wave-end gate must stay the full harvest gate, got mode %q", result.Mode)
	}
	if len(exec.ran) != len(full) {
		t.Fatalf("wave-end gate did not run the whole set: ran %#v", exec.ran)
	}
	for i, command := range full {
		if exec.ran[i] != command {
			t.Fatalf("wave-end gate dropped or reordered %q: ran %#v", command, exec.ran)
		}
	}
}

func TestSelectScopedCommandsUnionsAndDedupes(t *testing.T) {
	scopes := []GateScope{
		{Name: "api", Paths: []string{"internal/api"}, Commands: []string{"build", "shared"}},
		{Name: "store", Paths: []string{"internal/store"}, Commands: []string{"shared", "store-test"}},
		{Name: "web", Paths: []string{"web"}, Commands: []string{"web-test"}},
	}
	commands, selected := selectScopedCommands(scopes, []string{"internal/api/a.go", "internal/store/b.go"})
	want := []string{"build", "shared", "store-test"}
	if len(commands) != len(want) {
		t.Fatalf("expected order-preserving de-duplicated union %#v, got %#v", want, commands)
	}
	for i := range want {
		if commands[i] != want[i] {
			t.Fatalf("union mismatch at %d: want %#v got %#v", i, want, commands)
		}
	}
	if len(selected) != 2 || selected[0] != "api" || selected[1] != "store" {
		t.Fatalf("selected scope set is wrong: %#v", selected)
	}
}

// Fail closed: when the runtime cannot compute which paths a change touched, the
// selective gate must REFUSE (named diff_unavailable), never pass on an empty
// diff that would run nothing.
func TestSelectiveGateRefusesWhenDiffUnavailable(t *testing.T) {
	exec := &recordingGateExec{outputs: map[string]string{}}
	rt := gateTestRuntime(exec)
	rt.DiffPaths = func(string, string) ([]string, error) {
		return nil, errors.New("git merge-base: unknown revision")
	}
	policy := GateTierPolicy{HarvestCommands: []string{"go test ./..."}, AllowDirtyTree: true}

	result, err := runSelectiveGateTier(policy, twoAreaScopes(), "main", "", rt)
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != gateOutcomeRefused {
		t.Fatalf("unavailable diff must refuse, not pass: %#v", result)
	}
	if result.Refusal == nil || result.Refusal.Cause != gateRefusalDiffUnavailable {
		t.Fatalf("refusal must name diff_unavailable: %#v", result.Refusal)
	}
	if len(exec.ran) != 0 {
		t.Fatalf("a refused gate must run nothing: ran %#v", exec.ran)
	}
}

// changedGatePaths fails closed when the committed-change base cannot be
// resolved (no default branch, no merge-base): the committed delta is
// load-bearing, so its absence is a refusal, not an empty change set.
func TestChangedGatePathsRefusesWithoutBase(t *testing.T) {
	repo := t.TempDir()
	// A repo whose branch is neither main nor master and has no origin: there is
	// no resolvable default branch to diff committed changes against.
	runGitDir(t, repo, "init", "-b", "feature")
	runGitDir(t, repo, "config", "user.email", "test@example.com")
	runGitDir(t, repo, "config", "user.name", "Tusker Test")
	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitDir(t, repo, "add", "a.txt")
	runGitDir(t, repo, "commit", "-m", "base")

	_, err := changedGatePaths(repo, "")
	if err == nil {
		t.Fatalf("missing base must fail closed, got nil error")
	}
	var te *TuskerError
	if !errors.As(err, &te) || te.Code != "GATE_DIFF_UNAVAILABLE" {
		t.Fatalf("missing base must refuse with GATE_DIFF_UNAVAILABLE, got %v", err)
	}
}

// Even a pass on an empty diff must stamp the tree hash and honor preflight, so a
// selective pass is attributable to one revision and cannot skip preconditions.
func TestSelectiveGatePassRunsPreflightAndStampsTreeHash(t *testing.T) {
	exec := &recordingGateExec{outputs: map[string]string{}}
	rt := scopedGateRuntime(exec, nil) // nothing touched at all
	policy := GateTierPolicy{HarvestCommands: []string{"go test ./..."}, AllowDirtyTree: true}

	result, err := runSelectiveGateTier(policy, twoAreaScopes(), "main", "", rt)
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != gateOutcomePassed {
		t.Fatalf("empty diff should pass: %#v", result)
	}
	if result.TreeHash == "" {
		t.Fatalf("selective pass must stamp the tree hash: %#v", result)
	}
	if len(exec.ran) != 0 {
		t.Fatalf("empty diff must run nothing: ran %#v", exec.ran)
	}

	// Preflight still bites on an empty diff: a disk-headroom floor below free
	// space refuses even the no-command pass.
	rt.FreeDiskGB = func(string) (float64, error) { return 1, nil }
	policy.MinFreeDiskGB = 100
	result, err = runSelectiveGateTier(policy, twoAreaScopes(), "main", "", rt)
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != gateOutcomeRefused || result.Refusal == nil || result.Refusal.Cause != gateRefusalDiskHeadroom {
		t.Fatalf("empty-diff pass path skipped preflight: %#v", result)
	}
	if result.TreeHash == "" {
		t.Fatalf("even a preflight refusal must stamp the tree hash: %#v", result)
	}
}

func TestValidateGateScopesRejectsIncompleteScopes(t *testing.T) {
	good := []GateScope{{Name: "api", Paths: []string{"internal/api"}, Commands: []string{"go test ./internal/api/..."}}}
	if err := validateGateScopes(good, "WORKFLOW.md"); err != nil {
		t.Fatalf("well-formed scope rejected: %v", err)
	}
	cases := map[string][]GateScope{
		"missing name":     {{Paths: []string{"x"}, Commands: []string{"c"}}},
		"missing paths":    {{Name: "api", Commands: []string{"c"}}},
		"missing commands": {{Name: "api", Paths: []string{"x"}}},
		"duplicate name":   {{Name: "api", Paths: []string{"x"}, Commands: []string{"c"}}, {Name: "api", Paths: []string{"y"}, Commands: []string{"d"}}},
	}
	for label, scopes := range cases {
		if err := validateGateScopes(scopes, "WORKFLOW.md"); err == nil {
			t.Fatalf("%s: incomplete scope config accepted", label)
		}
	}
}

// The seeded gate stanza and the parse round-trip must both carry scopes: a fresh
// init documents the selective gate, and configured scopes decode onto the policy.
func TestGateScopesParseAndSeed(t *testing.T) {
	block := defaultProofAndGateBlock(defaultWorkflow().Orchestration)
	if !strings.Contains(block, "scopes:") || !strings.Contains(block, "gate --changed") {
		t.Fatalf("seeded gate block does not document scopes: %s", block)
	}
	wfFile := WorkflowFile{Path: "WORKFLOW.md", Body: defaultWorkflowMarkdown()}
	wfFile.Data = defaultWorkflow()
	wfFile.Data.Orchestration.Gate = GateTierPolicy{
		HarvestCommands: []string{"go test ./..."},
		Scopes:          []GateScope{{Name: "api", Paths: []string{"internal/api"}, Commands: []string{"go test ./internal/api/..."}}},
	}
	if err := validateWorkflowFile(wfFile); err != nil {
		t.Fatalf("workflow with scopes failed validation: %v", err)
	}
	// A malformed scope is rejected through the same validation path.
	wfFile.Data.Orchestration.Gate.Scopes[0].Commands = nil
	if err := validateWorkflowFile(wfFile); err == nil {
		t.Fatalf("scope missing commands passed workflow validation")
	}
}

func TestScopeOwnsPathMatchesPrefixAndGlob(t *testing.T) {
	cases := []struct {
		scope   GateScope
		touched string
		want    bool
	}{
		{GateScope{Paths: []string{"internal/api"}}, "internal/api/handler.go", true},
		{GateScope{Paths: []string{"internal/api"}}, "internal/apiary/x.go", false},
		{GateScope{Paths: []string{"internal/api/"}}, "internal/api", true},
		{GateScope{Paths: []string{"*.rs"}}, "crates/core/src/lib.rs", true},
		{GateScope{Paths: []string{"*.rs"}}, "crates/core/src/lib.go", false},
		{GateScope{Paths: []string{"cmd/*/main.go"}}, "cmd/tusker/main.go", true},
	}
	for _, c := range cases {
		if got := scopeOwnsPath(c.scope, c.touched); got != c.want {
			t.Fatalf("scopeOwnsPath(%#v, %q) = %v, want %v", c.scope.Paths, c.touched, got, c.want)
		}
	}
}
