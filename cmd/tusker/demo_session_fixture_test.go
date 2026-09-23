package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The fake provider supplies only native events. Session identity comes from
// the adapter's raw logs and attempt completion comes from its status files.
func TestDemoSessionFakeCodexNativeLineage(t *testing.T) {
	root := t.TempDir()
	t.Setenv("TUSKER_STATE_ROOT", filepath.Join(root, "state"))
	binDir := filepath.Join(root, "bin")
	if err := os.Mkdir(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	provider := `#!/usr/bin/env python3
import json, os, sys
root = os.environ["TUSKER_FAKE_CODEX_ROOT"]
argv = sys.argv[1:]
with open(os.path.join(root, "argv.log"), "a") as f:
    f.write(" ".join(argv) + "\n")
if "resume" in argv:
    index = argv.index("resume")
    if argv[index + 2:] != ["-"]:
        sys.exit("invalid resume command")
    session = argv[index + 1]
else:
    counter = os.path.join(root, "starts")
    n = int(open(counter).read()) if os.path.exists(counter) else 0
    n += 1
    open(counter, "w").write(str(n))
    session = "native-" + str(n)
sys.stdin.read()
for event in ({"type":"thread.started","session_id":session},
              {"type":"turn.started","session_id":session,"turn_id":"turn-1"},
              {"type":"turn.completed","session_id":session,"turn_id":"turn-1","token_usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}):
    print(json.dumps(event), flush=True)
`
	if err := os.WriteFile(filepath.Join(binDir, "codex"), []byte(provider), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TUSKER_FAKE_CODEX_ROOT", root)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	workspace := filepath.Join(root, "workspace")
	if err := os.Mkdir(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	prompt := filepath.Join(root, "prompt.md")
	if err := os.WriteFile(prompt, []byte("Do the session fixture task.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	start := StartRequest{
		ProjectID: "fixture", RecordID: "APP-T-0001", ItemID: "APP-T-0001", Lane: runLaneExecute,
		WorkspacePath: workspace, RepoRoot: workspace, PromptPath: prompt,
		Command: defaultCodexExecCommand(),
	}
	launch := func(id string, resume string) string {
		t.Helper()
		start.AttemptID = id
		start.RawLogPath = filepath.Join(root, id+".log")
		start.EventSinkPath = filepath.Join(root, id+".events")
		start.StatusPath = filepath.Join(root, id+".status")
		if resume == "" {
			start.Command = defaultCodexExecCommand()
			_, err := executeRunnerCommand(context.Background(), RunnerCodexExec, runnerExecRequest{
				ProjectID: start.ProjectID, RecordID: start.RecordID, ItemID: start.ItemID, AttemptID: id,
				Lane: start.Lane, WorkspacePath: workspace, RepoRoot: workspace, PromptPath: prompt,
				RawLogPath: start.RawLogPath, EventSinkPath: start.EventSinkPath, StatusPath: start.StatusPath, Command: start.Command,
			}, RunnerCapabilities{StructuredEvents: true, ResumeSession: true, MachineFinalStatus: true})
			if err != nil {
				t.Fatal(err)
			}
		} else {
			start.Command = codexExecResumeCommand(defaultCodexExecCommand())
			if _, err := runnerWrapperStartChild(context.Background(), runnerWrapperRequest{
				Runner: string(RunnerCodexExec), Start: start,
				Resume: &ResumeRequest{SessionRef: resume, Command: start.Command},
			}); err != nil {
				t.Fatal(err)
			}
		}
		waitForStatusFile(t, start.StatusPath)
		session := extractSessionRef(start.RawLogPath)
		if session == "" {
			t.Fatalf("attempt %s has no native session in provider log", id)
		}
		return session
	}
	first := launch("attempt-1", "")
	continued := launch("attempt-2", first)
	fresh := launch("attempt-3", "")
	if first != "native-1" || continued != first || fresh != "native-2" {
		t.Fatalf("observed native lineage: first=%q continued=%q fresh=%q", first, continued, fresh)
	}
	argv, err := os.ReadFile(filepath.Join(root, "argv.log"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(argv)), "\n")
	if len(lines) != 3 || strings.Contains(lines[0], "resume") || !strings.Contains(lines[1], "resume "+first+" -") || strings.Contains(lines[2], "resume") {
		t.Fatalf("provider saw wrong start/resume/fresh sequence: %q", lines)
	}
}
