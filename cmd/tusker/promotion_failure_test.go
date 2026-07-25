package main

import (
	"errors"
	"os"
	"strings"
	"testing"
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
