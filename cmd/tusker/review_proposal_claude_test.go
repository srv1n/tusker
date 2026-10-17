package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestScanReviewProposalLogReadsClaudeToolResults(t *testing.T) {
	claudeToolResult := func(text string) string {
		event := map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": []any{
			map[string]any{"type": "tool_result", "tool_use_id": "toolu_1", "content": text},
		}}}
		raw, err := json.Marshal(event)
		if err != nil {
			t.Fatal(err)
		}
		return string(raw) + "\n"
	}
	proposal := reviewProposal{Schema: reviewProposalSchema, AttemptID: "01ATTEMPT", Result: ReviewResult{Verdict: "pass"}}
	canonical, err := json.Marshal(proposal)
	if err != nil {
		t.Fatal(err)
	}

	// The reviewer read the Go source that declares the marker constant.
	source := claudeToolResult("30\t\treviewProposalMarker = \"" + reviewProposalMarker + "\"\n")
	if _, found, err := scanReviewProposalLog(strings.NewReader(source)); err != nil || found {
		t.Fatalf("source mentioning the marker must be ignored: found=%v err=%v", found, err)
	}

	// `tusker review` printed the proposal through the Bash tool.
	printed := claudeToolResult("recorded review\n" + reviewProposalMarker + string(canonical) + "\n")
	got, found, err := scanReviewProposalLog(strings.NewReader(source + printed))
	if err != nil || !found || got.AttemptID != "01ATTEMPT" {
		t.Fatalf("Claude tool_result proposal not recovered: got=%#v found=%v err=%v", got, found, err)
	}
}
