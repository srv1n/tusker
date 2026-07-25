package main

import (
	"errors"
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
		{"infrastructure", "", "dial tcp: connection reset", promotionFailureInfrastructure, false},
		{"flake", "", "flaky test; rerun passed", promotionFailureFlake, false},
		{"isolated", "APP-T-0001", "assertion failed", promotionFailureIsolated, false},
		{"ambiguous", "", "compiler failed", promotionFailureAmbiguous, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			route := classifyPromotionFailure(PromotionFailurePacket{GateCommand: "go test", OwningTaskID: tc.owner, Defects: []GateDefect{{Target: "T", Excerpt: tc.excerpt}}})
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
