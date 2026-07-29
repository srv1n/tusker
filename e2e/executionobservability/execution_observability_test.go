//go:build !windows

// Package executionobservability exercises the execution-observability
// product boundary in one hermetic process. The unit suites own the detailed
// fixtures; this gate makes the recovery contract explicit and prevents a
// future refactor from silently dropping one failure class from the release
// proof.
package executionobservability_test

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"
)

// executionObservabilityFixture is deliberately stable, compact input to the
// broader factory regression. It records the operator-facing facts this
// focused suite proves, while the invoked tests keep the raw graph, timeline,
// and lifecycle fixtures next to the code they exercise.
type executionObservabilityFixture struct {
	Name       string
	Acceptance string
	Facts      []string
	Tests      []string
}

func TestExecutionObservability(t *testing.T) {
	fixtures := []executionObservabilityFixture{
		{
			Name:       "migration_and_compatibility",
			Acceptance: "A1",
			Facts:      []string{"identity", "lineage", "bindings", "ownership", "restart_safe_backfill"},
			Tests: []string{
				"TestExecutionLedgerMigratesFirstRevisionSchema",
				"TestExecutionBackfillRejectsAnyImmutableMetadataConflictAtomically",
				"TestExecutionGraphMigration",
				"TestWaveExecutionRootGenerationIsIdempotentAcrossRestart",
			},
		},
		{
			Name:       "authoritative_replay_and_cursor_convergence",
			Acceptance: "A2",
			Facts:      []string{"complete", "partial", "reset", "gap", "stale_cursor", "authoritative_tail"},
			Tests: []string{
				"TestProviderExecutionEventReplay",
				"TestProviderExecutionEventAtomicity",
				"TestExecutionTimelineRecovery",
				"TestExecutionTimelineVectorCursorConvergesAcrossSources",
				"TestExecutionTimelineGapNewSourceAndRestartRecovery",
			},
		},
		{
			Name:       "cross_provider_fanout",
			Acceptance: "A3",
			Facts:      []string{"wave_root", "managed_attempt", "provider_child", "codex", "claude", "resume_identity"},
			Tests: []string{
				"TestExecutionGraphIdentity",
				"TestCodexExecutionAdapterLocalThreadAndChildPreserveResumeIdentity",
				"TestCodexExecutionAdapterRunPayloadWiresExecAndCloudPolls",
				"TestClaudeExecutionAdapterNamedSessionAndNativeChildPreserveResumeIdentity",
				"TestClaudeExecutionAdapterRunPayloadIsReplaySafeAndWiredToLiveIngress",
			},
		},
		{
			Name:       "restart_cancellation_and_authority",
			Acceptance: "A4",
			Facts:      []string{"no_duplicate_root", "parent_does_not_kill_child", "provider_outage", "idempotent_cancel", "pid_fence", "no_proof_leakage"},
			Tests: []string{
				"TestClaudeExecutionAdapterParentTerminalReconcilesUnstoppedChildOnceAcrossRestart",
				"TestClaudeLiveFinalizeProcessExitWithoutResultReconcilesChild",
				"TestExecutionLifecycleRecovery",
				"TestExecutionCancellationManagedPIDFence",
				"TestExecutionCancellationEvidenceIsIdempotentAndProviderSafe",
				"TestProviderObservationAuthorityRefusals",
				"TestExecutionBindingAuthority",
			},
		},
		{
			Name:       "operator_fixture_shape",
			Acceptance: "A5",
			Facts:      []string{"graph", "lifecycle_dimensions", "cursor_tail", "unbound_authority_refusal", "attention"},
			Tests: []string{
				"TestExecutionObservabilityDogfoodFixture",
				"TestExecutionGraphProjection",
				"TestExecutionGraphFiltersAndReadOnly",
				"TestDirectExecutionRegistration",
				"TestExecutionLifecycleDimensions",
				"TestExecutionTimelineProjection",
			},
		},
	}

	all := make([]string, 0, 32)
	seen := map[string]bool{}
	for _, fixture := range fixtures {
		if fixture.Name == "" || fixture.Acceptance == "" || len(fixture.Facts) == 0 || len(fixture.Tests) == 0 {
			t.Fatalf("invalid stable execution observability fixture: %#v", fixture)
		}
		for _, name := range fixture.Tests {
			if seen[name] {
				t.Fatalf("fixture test appears twice: %s", name)
			}
			seen[name] = true
			all = append(all, name)
		}
	}
	sort.Strings(all)

	repo := executionObservabilityRepositoryRoot(t)
	goBinary, err := exec.LookPath("go")
	if err != nil {
		t.Fatal("locate go:", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, goBinary, "test", "./cmd/tusker", "-run", "^("+strings.Join(all, "|")+")$", "-count=1", "-v")
	cmd.Dir = repo
	cmd.Env = executionObservabilityEnvironment(t)
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	if err := cmd.Run(); err != nil {
		t.Fatalf("execution observability gate failed: %v\n%s", err, output.String())
	}
	if ctx.Err() != nil {
		t.Fatalf("execution observability gate timed out: %v\n%s", ctx.Err(), output.String())
	}
	for _, name := range all {
		if !strings.Contains(output.String(), "=== RUN   "+name) {
			t.Fatalf("execution observability gate silently skipped %s\n%s", name, output.String())
		}
	}
}

func executionObservabilityRepositoryRoot(t *testing.T) string {
	t.Helper()
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test source")
	}
	repo, err := filepath.Abs(filepath.Join(filepath.Dir(source), "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(repo, "go.mod")); err != nil {
		t.Fatalf("resolve repository root: %v", err)
	}
	return repo
}

func executionObservabilityEnvironment(t *testing.T) []string {
	t.Helper()
	sandbox := t.TempDir()
	home := filepath.Join(sandbox, "home")
	state := filepath.Join(sandbox, "state")
	tmp := filepath.Join(sandbox, "tmp")
	for _, path := range []string{home, state, tmp} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	env := make([]string, 0, len(os.Environ())+10)
	for _, entry := range os.Environ() {
		if strings.HasPrefix(entry, "HOME=") || strings.HasPrefix(entry, "TMPDIR=") || strings.HasPrefix(entry, "TUSKER_STATE_ROOT=") || strings.HasPrefix(entry, "TUSKER_ATTEMPT_ID=") || strings.HasPrefix(entry, "CODEX_") || strings.HasPrefix(entry, "CLAUDE") || strings.HasPrefix(entry, "ANTHROPIC_") || strings.HasPrefix(entry, "OPENAI_") {
			continue
		}
		env = append(env, entry)
	}
	return append(env,
		"HOME="+home,
		"TMPDIR="+tmp,
		"TUSKER_STATE_ROOT="+state,
		"GIT_CONFIG_GLOBAL="+os.DevNull,
		"GIT_CONFIG_SYSTEM="+os.DevNull,
		"GIT_TERMINAL_PROMPT=0",
		"GOTOOLCHAIN=local",
	)
}
