package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExecutionObservabilityDocumentation(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	required := map[string][]string{
		"docs/system/execution-observability.md": {
			"immutable Tusker execution ID", "unbound inbox", "managed child",
			"provider-native child", "authoritative fetch", "stale_cursor",
			"Codex Cloud", "Claude Code", "hooks, JSON streams, and cloud status are untrusted",
			"ORC-T-0046", "ORC-T-0047",
		},
		"docs/runbooks/execution-observability.md": {
			"Backfill and compatibility", "Restart and cursor recovery",
			"Binding conflicts and unbound work", "Cancellation settlement",
			"Bounded disable/rollback response",
		},
		"docs/system/cli.md":                         {"execution register", "execution inbox"},
		"docs/system/orchestration.md":               {"Execution visibility is not dispatch authority"},
		"docs/system/serve-ui.md":                    {"Execution Operations"},
		".tusker/knowledge/domains/project/INDEX.md": {"Execution identity, direct-work visibility, provider children, or timeline recovery"},
		".tusker/knowledge/domains/project/CANON.md": {"Execution observability has its canonical contract"},
		"skills/tusker/references/OPERATE.md":        {"docs/runbooks/execution-observability.md", "Provider observations remain authority-neutral"},
	}
	for rel, fragments := range required {
		raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		text := string(raw)
		for _, fragment := range fragments {
			if !strings.Contains(text, fragment) {
				t.Errorf("%s missing %q", rel, fragment)
			}
		}
	}
}
