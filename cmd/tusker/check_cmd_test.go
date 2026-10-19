package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func checkPlanNames(t *testing.T, args Args) []string {
	t.Helper()
	steps, err := checkPlan(args)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, step := range steps {
		names = append(names, step.Part+"/"+step.Name)
	}
	return names
}

func TestCheckPlanRouting(t *testing.T) {
	cases := []struct {
		args Args
		want []string
	}{
		{Args{}, []string{"setup/setup doctor", "setup/vault status", "setup/workspace mode", "validate/validate", "skills/skill doctor", "skills/skill audit-agent-guidance", "docs/docs check", "docs/docs status --stale"}},
		{Args{"only": "docs", "since": "origin/main"}, []string{"docs/docs check", "docs/docs status --stale", "docs/docs touch"}},
		{Args{"only": "validate,acp"}, []string{"validate/validate", "acp/acp doctor"}},
		{Args{"fix": "true", "only": "setup,validate,docs"}, []string{"setup/setup repair", "setup/vault repair", "setup/workspace mode", "validate/reconcile", "validate/reindex", "docs/docs map", "validate/validate", "docs/docs check", "docs/docs status --stale"}},
	}
	for _, tc := range cases {
		if got := checkPlanNames(t, tc.args); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("checkPlan(%v) = %v, want %v", tc.args, got, tc.want)
		}
	}
	if _, err := checkPlan(Args{"only": "validate,bogus"}); err == nil || !strings.Contains(err.Error(), "bogus") {
		t.Fatalf("unknown part must be rejected, got %v", err)
	}
}

func TestCheckWorstExitCodeAndJSON(t *testing.T) {
	var seen []Args
	steps := []checkStep{
		{Part: "setup", Name: "pass", Run: func(a Args) (int, error) {
			seen = append(seen, a)
			emitJSON(map[string]any{"ok": true})
			return 0, nil
		}},
		{Part: "validate", Name: "fail", Run: func(Args) (int, error) { return 2, nil }},
		{Part: "docs", Name: "docs failure", Run: func(Args) (int, error) { return 0, &docsCheckFailure{Count: 1} }},
		{Part: "docs", Name: "error", Run: func(Args) (int, error) { return 0, errors.New("boom") }},
	}
	var code int
	out := captureStdout(t, func() {
		code = runCheckSteps(Args{"json": "true", "only": "x", "fix": "true", "_pos0": "p", "vault": "v"}, steps)
	})
	if code != 2 {
		t.Fatalf("worst exit code = %d, want 2", code)
	}
	if got := seen[0]; got["vault"] != "v" || got["json"] != "true" || got["only"] != "" || got["fix"] != "" || got["_pos0"] != "" {
		t.Fatalf("step args not scrubbed: %v", got)
	}
	var report struct {
		OK       bool                       `json:"ok"`
		ExitCode int                        `json:"exit_code"`
		Parts    map[string]checkPartResult `json:"parts"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("one JSON object expected: %v\n%s", err, out)
	}
	if report.OK || report.ExitCode != 2 || !report.Parts["setup"].OK || report.Parts["validate"].OK || report.Parts["docs"].OK {
		t.Fatalf("unexpected report: %+v", report)
	}
	if string(report.Parts["setup"].Steps[0].Report) != `{"ok":true}` {
		t.Fatalf("step JSON not embedded: %s", report.Parts["setup"].Steps[0].Report)
	}
	docs := report.Parts["docs"].Steps
	if docs[0].ExitCode != 1 || docs[0].Error != nil || docs[1].ExitCode != 1 || docs[1].Error == nil {
		t.Fatalf("docs steps: %+v", docs)
	}
}

func TestWorkflowWorktreeModeDefaultsWorkspaceRoot(t *testing.T) {
	vault := filepath.Join(t.TempDir(), ".tusker")
	text := strings.Replace(defaultWorkflowMarkdown(), "strategy: shared", "strategy: worktree", 1)
	if !strings.Contains(text, "root: .") || !strings.Contains(text, "strategy: worktree") {
		t.Fatalf("init workflow lacks the expected workspace block:\n%s", text)
	}
	if err := os.MkdirAll(vault, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(workflowPath(vault), []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	wf, err := loadWorkflow(vault)
	if err != nil {
		t.Fatal(err)
	}
	if wf.Data.Workspace.Strategy != "worktree" || wf.Data.Workspace.Root != "workspaces" {
		t.Fatalf("workspace = %+v, want worktree under workspaces", wf.Data.Workspace)
	}
}
