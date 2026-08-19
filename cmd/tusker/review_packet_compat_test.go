package main

import (
	"crypto/sha256"
	"fmt"
	"testing"
)

func TestReviewPacketMatchesPreExtractionOutput(t *testing.T) {
	packet := renderReviewPacket(
		Note{Data: map[string]any{"id": "APP-T-0001", "title": "Golden packet"}},
		RunStatus{RecordID: "APP-T-0001", ActiveAttemptID: "attempt-1", Runner: "codex", RunnerProfile: "default", RunnerHarness: "codex_acp", RunnerModel: "gpt-5", RunnerEffort: "medium", Lane: "execute", WorkRevision: 3, WorkspacePath: "/workspace", SessionRef: "session-1", StartedAt: "2026-08-12T00:00:00Z", LastEventAt: "2026-08-12T00:01:00Z", PromptPath: "prompt.md", EventSinkPath: "events.jsonl", RawLogPath: "raw.log", StatusPath: "status.json"},
		[]RunTurn{{TurnIndex: 1, TurnID: "turn-1", SessionRef: "session-1", Status: "complete", LastEventAt: "2026-08-12T00:01:00Z"}},
		[]RuntimeSupervisorDecision{{Kind: "continue_attempt", Reason: "continue", ParentAttemptID: "attempt-0", ParentSessionRef: "session-0", BranchName: "task/APP-T-0001", WorkspacePath: "/workspace", ContextSignal: "healthy", TotalTokens: 42, CreatedAt: "2026-08-12T00:00:30Z"}},
		reviewPacketFacts{ChangedFiles: []string{"`main.go` (M)"}, DiffSummary: []string{"`main.go | 2 +-`"}, CommandSummaries: []string{"`go test ./...` kind=test result=pass"}, VerificationCommands: []string{"`go test ./...` result=pass"}, ValidationSummaries: []string{"`tusker validate` result=pass"}, SessionRefs: []string{"session-1"}, TurnIDs: []string{"turn-1"}, RuntimeSummaries: []string{"lease=released outcome=succeeded"}, OpenRisks: []string{"review pending"}, SoftDependencyDependents: []string{"- APP-T-0002"}},
	)
	got := fmt.Sprintf("%x", sha256.Sum256([]byte(packet)))
	const want = "53d529542511913cff2e6017bc152977489f0612e07194a9c9da92d0d767fec9"
	if got != want {
		t.Fatalf("review packet changed across extraction: got %s, want %s\n%s", got, want, packet)
	}
}
