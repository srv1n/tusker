package main

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// Resume must hand the wrapper the same policy inputs Start does.
func TestRunnerResumeCarriesStartPolicy(t *testing.T) {
	for _, tc := range []struct {
		name   string
		runner Runner
		argv   []string
	}{
		{"codex_exec", &CodexExecRunner{}, []string{"codex", "exec", "--json", "-"}},
		{"muse", &MuseRunner{}, []string{"muse", "exec", "--json"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			base, err := runnerWrapperRequestForTest(dir)
			if err != nil {
				t.Fatal(err)
			}
			stub := filepath.Join(dir, "fake-wrapper")
			if err := os.WriteFile(stub, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("TUSKER_WRAPPER_EXE", stub)
			start := base.Start
			start.Command = tc.argv[0] + " exec --json"
			start.CommandArgv = tc.argv
			start.PrivateFolders = []string{"/private/secrets", "/private/keys"}
			start.Actor, start.Principal = "actor:operator", "principal:operator"
			start.CodexPolicy = CodexPolicy{ApprovalPolicy: "never", ThreadSandbox: "workspace-write"}
			requestPath := start.StatusPath + ".wrapper-request.json"

			if _, err := tc.runner.Start(context.Background(), start); err != nil {
				t.Fatal(err)
			}
			started, err := readRunnerWrapperRequest(requestPath)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := tc.runner.Resume(context.Background(), ResumeRequest{
				ProjectID: start.ProjectID, RecordID: start.RecordID, ItemID: start.ItemID, AttemptID: start.AttemptID,
				Lane: start.Lane, WorkRevision: start.WorkRevision, LeaseGeneration: start.LeaseGeneration,
				SessionRef: "session-1", WorkingDir: start.WorkingDir, WorkspacePath: start.WorkspacePath, RepoRoot: start.RepoRoot,
				PromptPath: start.PromptPath, EventSinkPath: start.EventSinkPath, RawLogPath: start.RawLogPath, StatusPath: start.StatusPath,
				Command: start.Command, CommandArgv: tc.argv, PrivateFolders: start.PrivateFolders,
				NotePath: start.NotePath, VaultPath: start.VaultPath, CodexPolicy: start.CodexPolicy,
				Principal: start.Principal, Actor: start.Actor,
			}); err != nil {
				t.Fatal(err)
			}
			resumed, err := readRunnerWrapperRequest(requestPath)
			if err != nil {
				t.Fatal(err)
			}
			if resumed.Resume == nil {
				t.Fatal("resume request not recorded")
			}
			if !reflect.DeepEqual(resumed.Start.PrivateFolders, started.Start.PrivateFolders) || len(resumed.Start.PrivateFolders) != 2 {
				t.Fatalf("private folders: start %v resume %v", started.Start.PrivateFolders, resumed.Start.PrivateFolders)
			}
			if resumed.Start.Actor != started.Start.Actor || resumed.Start.Principal != started.Start.Principal || resumed.Start.Actor == "" {
				t.Fatalf("actor/principal: start %q/%q resume %q/%q", started.Start.Actor, started.Start.Principal, resumed.Start.Actor, resumed.Start.Principal)
			}
			if !reflect.DeepEqual(resumed.Start.CodexPolicy, started.Start.CodexPolicy) {
				t.Fatalf("codex policy: start %#v resume %#v", started.Start.CodexPolicy, resumed.Start.CodexPolicy)
			}
		})
	}
}
