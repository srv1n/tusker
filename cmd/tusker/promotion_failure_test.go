package main

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

func TestPromotionFailurePacketCarriesFrozenFactsAndBoundedDiagnostics(t *testing.T) {
	output := "noise\n--- FAIL: TestRed (0.01s)\n  expected green\n" + strings.Repeat("x", 4000)
	packet := promotionFailurePacket(DepartureCandidate{CandidateSHA: "candidate", CandidateTreeHash: "tree", ExpectedDefaultBranchSHA: "main"}, DepartureGate{Command: "go test ./...", Profile: "full", Toolchain: "go1.24"}, "runner:local", output, errors.New("exit 1"), GateTierPolicy{DefectTargetRegex: `^--- FAIL: (\S+)`}, "green", "bisect/42", "APP-T-0001", []string{"a.go", "a.go"}, []string{"runtime://gate.log", "runtime://gate.log"})
	if packet.CandidateSHA != "candidate" || packet.ExpectedMainSHA != "main" || packet.Runner != "runner:local" || packet.BisectionRef != "bisect/42" || packet.OwningTaskID != "APP-T-0001" {
		t.Fatalf("missing frozen facts: %#v", packet)
	}
	if len(packet.Defects) != 1 || packet.Defects[0].Target != "TestRed" || len(packet.ArtifactRefs) != 1 || len(packet.TouchedPaths) != 1 {
		t.Fatalf("packet is not bounded/attributable: %#v", packet)
	}
	if len(packet.Defects[0].Excerpt) > 500 {
		t.Fatalf("raw output leaked into packet: %q", packet.Defects[0].Excerpt)
	}
}

func TestPromotionFailureLatestCompleteGateLedgerMatrix(t *testing.T) {
	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	put := func(id, tree, command, toolchain, at string) {
		if err := store.RecordGateLedger(GateLedgerEntry{ID: id, ProjectID: "app", TreeHash: tree, Command: command, Profile: "full", Toolchain: toolchain, PassedAt: at}); err != nil {
			t.Fatal(err)
		}
	}
	put("a1", "tree-a", "one", "go1", "2026-07-25T00:00:00.1Z")
	put("a2", "tree-a", "two", "go1", "2026-07-25T00:00:00.2Z")
	put("split", "tree-b", "one", "go1", "2026-07-25T00:00:00.9Z")
	put("other", "tree-c", "two", "go1", "2026-07-25T00:00:00.9Z")
	put("mixed", "tree-e", "one", "go1", "2026-07-25T00:00:00.8Z")
	put("mixed-two", "tree-e", "two", "go2", "2026-07-25T00:00:00.85Z")
	put("current", "tree-d", "one", "go1", "2026-07-25T00:00:02Z")
	entry, err := store.LatestCompleteGateLedgerBefore("app", []string{"one", "two"}, "full", "go1", "2026-07-25T00:00:01Z")
	if err != nil || entry == nil || entry.TreeHash != "tree-a" {
		t.Fatalf("same-tree complete proof=%#v err=%v", entry, err)
	}
	entry, err = store.LatestCompleteGateLedgerBefore("app", []string{"one", "two"}, "full", "go1", "2026-07-25T00:00:00.15Z")
	if err != nil || entry != nil {
		t.Fatalf("partial/fractional prior pass incorrectly accepted: %#v %v", entry, err)
	}
	entry, err = store.LatestCompleteGateLedgerBefore("app", []string{"one", "two"}, "full", "go2", "2026-07-25T00:00:01Z")
	if err != nil || entry != nil {
		t.Fatalf("mixed-toolchain proof incorrectly accepted: %#v %v", entry, err)
	}
}

func TestPromotionFailureHardAndSoftSuccessorSemantics(t *testing.T) {
	failed := holdTestTask("APP-T-0001", "done", map[string]any{"build_failed": true})
	hard := holdTestTask("APP-T-0002", "ready", map[string]any{"dependencies": []string{"APP-T-0001:hard"}})
	soft := holdTestTask("APP-T-0003", "ready", map[string]any{"dependencies": []string{"APP-T-0001:soft"}})
	idx := v7Index{Tasks: map[string]Note{"APP-T-0001": failed, "APP-T-0002": hard, "APP-T-0003": soft}}
	if _, held := v7HeldByFailedUpstream(hard, idx); !held {
		t.Fatal("hard successor was not relocked")
	}
	if _, held := v7HeldByFailedUpstream(soft, idx); !held {
		t.Fatal("soft successor was not relocked on revoked premise")
	}
	_ = time.Now // retain time import proof that RFC timestamps are parsed by store path
}

func TestPromotionTouchedPathsPreservesLegalNewlineFilename(t *testing.T) {
	paths := promotionTouchedPathsFromNUL("zeta \x00dir/line\nbreak.go\x00 alpha\x00")
	if len(paths) != 3 || paths[0] != " alpha" || paths[1] != "dir/line\nbreak.go" || paths[2] != "zeta " {
		t.Fatalf("NUL path parsing split a legal filename: %#v", paths)
	}
}

func TestPromotionFailureClassificationEscalationLadder(t *testing.T) {
	for _, tc := range []struct {
		name, owner, excerpt string
		want                 promotionFailureClass
		model                bool
	}{
		{"infrastructure", "", "infra-fingerprint", promotionFailureInfrastructure, false},
		{"flake", "", "flake-fingerprint", promotionFailureFlake, false},
		{"isolated", "APP-T-0001", "assertion failed", promotionFailureIsolated, false},
		{"ambiguous", "", "compiler failed", promotionFailureAmbiguous, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			policy := GateTierPolicy{InfrastructureFailurePatterns: []string{"infra-fingerprint"}, FlakeFailurePatterns: []string{"flake-fingerprint"}}
			route := classifyPromotionFailure(PromotionFailurePacket{GateCommand: "go test", OwningTaskID: tc.owner, ClassificationText: tc.excerpt, Defects: []GateDefect{{Target: "T", Excerpt: tc.excerpt}}}, policy)
			if route.Class != tc.want || route.ModelTriage != tc.model {
				t.Fatalf("route=%#v", route)
			}
		})
	}
}

func TestPromotionFailureIdentityIsStableAcrossRuns(t *testing.T) {
	first := PromotionFailurePacket{GateCommand: "go test", GateProfile: "full", Toolchain: "go", OwningTaskID: "APP-T-0001", Defects: []GateDefect{{Target: "TestA", Excerpt: "first output"}}}
	second := first
	second.Defects[0].Excerpt = "different timestamp and raw output"
	if promotionFailureIdentity(first) != promotionFailureIdentity(second) {
		t.Fatalf("same defect opened a duplicate identity")
	}
}

func TestPromotionFailureTimeoutTextIsAmbiguousWithoutConfiguredPattern(t *testing.T) {
	route := classifyPromotionFailure(PromotionFailurePacket{ClassificationText: "assertion expected timeout value", Defects: []GateDefect{{Target: "TestTimeout"}}}, GateTierPolicy{})
	if route.Class != promotionFailureAmbiguous || !route.ModelTriage {
		t.Fatalf("incidental test text was misclassified: %#v", route)
	}
}

func TestPromotionFailureNotRunBisectionIsNotIsolationEvidence(t *testing.T) {
	route := classifyPromotionFailure(PromotionFailurePacket{BisectionRef: "not_run", BisectionStatus: "not_run"}, GateTierPolicy{})
	if route.Class != promotionFailureAmbiguous || !route.ModelTriage {
		t.Fatalf("sentinel became false isolation: %#v", route)
	}
}

func TestPromotionFailureRouteMatrix(t *testing.T) {
	cases := []struct {
		name      string
		candidate DepartureCandidate
		text      string
		policy    GateTierPolicy
		want      promotionFailureClass
		triage    bool
	}{
		{"infra held repair", DepartureCandidate{}, "INFRA", GateTierPolicy{InfrastructureFailurePatterns: []string{"INFRA"}}, promotionFailureInfrastructure, false},
		{"flake quarantine", DepartureCandidate{}, "FLAKE", GateTierPolicy{FlakeFailurePatterns: []string{"FLAKE"}, FlakeFailureAction: "quarantine"}, promotionFailureFlake, false},
		{"flake rerun", DepartureCandidate{}, "FLAKE", GateTierPolicy{FlakeFailurePatterns: []string{"FLAKE"}, FlakeFailureAction: "rerun"}, promotionFailureFlake, false},
		{"ambiguous dedup", DepartureCandidate{}, "unknown", GateTierPolicy{}, promotionFailureAmbiguous, true},
		{"multi task no owner", DepartureCandidate{TaskSourceSHAs: map[string]string{"A": "a", "B": "b"}, TaskStateRevisions: map[string]string{"A": "1", "B": "1"}}, "unknown", GateTierPolicy{}, promotionFailureAmbiguous, true},
		{"singleton owner", DepartureCandidate{TaskSourceSHAs: map[string]string{"A": "a"}, TaskStateRevisions: map[string]string{"A": "1"}}, "unknown", GateTierPolicy{}, promotionFailureIsolated, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			owner := promotionFailureOwner(tc.candidate)
			packet := PromotionFailurePacket{OwningTaskID: owner, ClassificationText: tc.text}
			route := classifyPromotionFailure(packet, tc.policy)
			if route.Class != tc.want || route.ModelTriage != tc.triage {
				t.Fatalf("route=%#v", route)
			}
			if tc.name == "multi task no owner" && owner != "" {
				t.Fatalf("false owner %q", owner)
			}
			if tc.name == "singleton owner" && owner != "A" {
				t.Fatalf("owner=%q", owner)
			}
		})
	}
}

func TestPromotionGateSetupFailureWritesArtifact(t *testing.T) {
	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	exec := runV7GateTierOnRef(t.TempDir(), t.TempDir(), "missing", "app", GateTierPolicy{HarvestCommands: []string{"true"}}, store)
	if exec.Err == nil || exec.ArtifactRef == "" {
		t.Fatalf("setup failure lacks artifact: %#v", exec)
	}
	raw, err := os.ReadFile(exec.ArtifactRef)
	if err != nil || len(raw) == 0 {
		t.Fatalf("artifact unavailable: %v %q", err, raw)
	}
}

func TestPromotionFailurePacketKeepsSetupFallbackDefect(t *testing.T) {
	packet := PromotionFailurePacket{Defects: []GateDefect{{Target: "fallback", Excerpt: "worktree setup failed"}}}
	got := withPromotionGateResult(packet, GateTierResult{Outcome: gateOutcomeFailed})
	if len(got.Defects) != 1 || got.Defects[0].Target != "fallback" {
		t.Fatalf("empty tier result erased setup fallback: %#v", got.Defects)
	}
	got = withPromotionGateResult(packet, GateTierResult{Outcome: gateOutcomeFailed, Defects: []GateDefect{{Target: "declared command", Excerpt: "red"}}})
	if len(got.Defects) != 1 || got.Defects[0].Target != "declared command" {
		t.Fatalf("structured tier defects were not authoritative: %#v", got.Defects)
	}
}
