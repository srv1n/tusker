package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestClaudeReviewAllowRulesIncludeSubmitAndTaskChecks(t *testing.T) {
	note := filepath.Join(t.TempDir(), "TSK-T-0001.md")
	body := "---\nid: TSK-T-0001\n---\n\n## Verification\n\n| Covers | Check | Result | Notes |\n|---|---|---|---|\n| A1 | command: go test ./cmd/tusker -run 'A\\|B' -count=1 | pending | |\n| A2 | manual: eyeball it | pending | |\n"
	if err := os.WriteFile(note, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	rules := claudeReviewAllowRules(note)
	for _, want := range []string{"Bash(tusker review *)", "Bash(git diff*)", "Bash(go test ./cmd/tusker -run 'A|B' -count=1)", "Bash(rtk git diff*)", "Bash(rtk go test ./cmd/tusker -run 'A|B' -count=1)"} {
		if !slices.Contains(rules, want) {
			t.Errorf("missing %q in %q", want, rules)
		}
	}
	for _, rule := range rules {
		if rule == "Bash(eyeball it)" || rule == "Bash(manual: eyeball it)" {
			t.Errorf("manual check must not become a Bash rule: %q", rule)
		}
	}
}
