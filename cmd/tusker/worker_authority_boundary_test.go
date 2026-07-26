package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestWorkerEnvironmentStripsDaemonAuthority(t *testing.T) {
	env := workerEnvironment([]string{
		"PATH=/bin", "TUSKER_STATE_ROOT=/daemon", "TUSKER_FIXTURE_STATE_ROOT=/fixture",
		"TUSKER_DAEMON_TOKEN=secret", "TUSKER_COMPLETION_KEY=secret", "SAFE=value",
	})
	for _, forbidden := range []string{"TUSKER_STATE_ROOT=", "TUSKER_FIXTURE_STATE_ROOT=", "TUSKER_DAEMON_", "TUSKER_COMPLETION_"} {
		for _, entry := range env {
			if len(entry) >= len(forbidden) && entry[:len(forbidden)] == forbidden {
				t.Fatalf("worker inherited daemon authority %q", entry)
			}
		}
	}
}

func TestCompletionWorkerSafetyRejectsUnsafeProfiles(t *testing.T) {
	state, workspace := filepath.Join(t.TempDir(), "state"), filepath.Join(t.TempDir(), "workspace")
	unsafe := ResolvedRunnerProfile{Name: "unsafe", Definition: RunnerProfileDefinition{Harness: string(RunnerCodexExec), PermissionPreset: "danger-full-access", Sandbox: RunnerSandboxDefinition{Mode: "danger-full-access"}}}
	if err := completionWorkerSafety(state, workspace, unsafe); err == nil {
		t.Fatal("danger-full-access profile must be rejected")
	}
	implementation := ResolvedRunnerProfile{Name: "implementation-terra", Definition: RunnerProfileDefinition{Harness: string(RunnerCodexExec), Sandbox: RunnerSandboxDefinition{Mode: "workspace-write"}}}
	if err := completionWorkerSafety(state, workspace, implementation); err != nil {
		t.Fatalf("codex_exec workspace-write must remain admissible: %v", err)
	}
	for _, profile := range []ResolvedRunnerProfile{
		{Name: "reviewer-terra", Definition: RunnerProfileDefinition{Harness: string(RunnerClaude), Sandbox: RunnerSandboxDefinition{Mode: "read-only"}}},
		{Name: "codex-app", Definition: RunnerProfileDefinition{Harness: string(RunnerCodexAppServer), Sandbox: RunnerSandboxDefinition{Mode: "read-only"}}},
		{Name: "codex-generic", Definition: RunnerProfileDefinition{Harness: string(RunnerCodex), Sandbox: RunnerSandboxDefinition{Mode: "read-only"}}},
	} {
		if err := completionWorkerSafety(state, workspace, profile); err == nil {
			t.Fatalf("metadata-only sandbox on %s must not authorize completion", profile.Name)
		}
	}
	if err := completionWorkerSafety(filepath.Join(workspace, "state"), workspace, ResolvedRunnerProfile{Name: "inside", Definition: RunnerProfileDefinition{Harness: string(RunnerCodexExec), Sandbox: RunnerSandboxDefinition{Mode: "workspace-write"}}}); err == nil {
		t.Fatal("state root nested in a workspace must be rejected")
	}
}

func TestReviewProposalRequiresCompleteSingleRawLogMarker(t *testing.T) {
	p := reviewProposal{Schema: reviewProposalSchema, AttemptID: "a"}
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, found, err := reviewProposalFromRawLog([]byte(reviewProposalMarker + string(raw))); err != nil || found {
		t.Fatalf("partial marker must not be harvested: found=%t err=%v", found, err)
	}
	if _, found, err := reviewProposalFromRawLog([]byte(reviewProposalMarker + string(raw) + "\n" + reviewProposalMarker + `{"schema":"other"}` + "\n")); err == nil || found {
		t.Fatalf("conflicting markers must be rejected: found=%t err=%v", found, err)
	}
	got, found, err := reviewProposalFromRawLog([]byte(reviewProposalMarker + string(raw) + "\n" + reviewProposalMarker + string(raw) + "\n"))
	if err != nil || !found || got.AttemptID != p.AttemptID {
		t.Fatalf("identical replay markers must be idempotent: %#v found=%t err=%v", got, found, err)
	}
}

func TestFrozenReviewProposalLogTreatsAbsenceAsNoProposalAndRejectsSymlink(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.raw.log")
	if raw, present, err := readFrozenReviewProposalLog(missing); err != nil || present || raw != nil {
		t.Fatalf("missing raw log must be no proposal: raw=%q present=%t err=%v", raw, present, err)
	}
	path := filepath.Join(t.TempDir(), "attempt.raw.log")
	if err := os.Symlink(missing, path); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, _, err := readFrozenReviewProposalLog(path); err == nil {
		t.Fatal("symlinked raw log must be rejected")
	}
}

func TestNilStoreCannotAuthenticateCompletionAuthority(t *testing.T) {
	if verifyCompletionReceiptAuthorityWithStore(t.TempDir(), completionReceipt{}, nil, true) {
		t.Fatal("caller environment must not select a completion trust store")
	}
}
