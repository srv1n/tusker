package main

import (
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

	// A change touching nothing any scope owns runs nothing at all and still
	// passes: there is no in-scope area to check.
	exec = &recordingGateExec{outputs: map[string]string{}}
	rt = scopedGateRuntime(exec, []string{"docs/README.md"})
	result, err := runSelectiveGateTier(policy, twoAreaScopes(), "main", "", rt)
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != gateOutcomePassed || len(exec.ran) != 0 {
		t.Fatalf("out-of-scope change should skip every command and pass: %#v ran=%#v", result, exec.ran)
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
