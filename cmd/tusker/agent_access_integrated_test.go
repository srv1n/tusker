package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	runnercore "tusker/internal/runner"
)

func TestAgentAccessIntegrated(t *testing.T) {
	workspaceRoot := t.TempDir()
	workspace := filepath.Join(workspaceRoot, "workspace")
	if err := os.Mkdir(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	privateRoot := t.TempDir()
	private := filepath.Join(privateRoot, "private")
	if err := os.Mkdir(private, 0o700); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(private, "sentinel.txt")
	if err := os.WriteFile(sentinel, []byte("must-survive"), 0o600); err != nil {
		t.Fatal(err)
	}

	profile := RunnerProfileDefinition{Harness: string(RunnerMuseCLI), Model: "muse-fixture", Effort: "medium", Access: &AgentAccessV1{
		Schema: agentAccessSchemaV1, Mode: accessModeProjects, Network: true, DestructiveActions: "ask", Folders: []AgentAccessFolder{}, PrivateFolders: []string{},
	}}
	definition := runnercore.HarnessDefinition{ID: string(RunnerMuseCLI), Provider: "muse", Dialect: "muse", Transport: runnercore.TransportCLI, Executable: museFixtureExecutable(t), Args: []string{"exec"}, SchemaVersion: 1}
	controls := nativeAccessControls(definition, profile.Access)
	resolved, err := resolveAccess(profile, nil, AgentAccessResolutionContext{Workspace: workspace, Controls: controls, Route: string(RunnerMuseCLI), Transport: "cli", Version: "Muse Code 1.1.1"})
	if err != nil || resolved.State != "ready" {
		t.Fatalf("profile setup did not resolve: %#v err=%v", resolved, err)
	}

	prepared, err := runnercore.Prepare(context.Background(), definition, runnercore.RunInput{
		Workspace: workspace, Preset: resolved.Effective.Preset, Model: profile.Model, Effort: profile.Effort,
		Prompt: "provider-free access integration", ResolvedAccess: runnercoreResolvedAccess(&resolved), Access: profile.Access,
	})
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Access == nil || prepared.Access.Fingerprint == "" || !containsRunnerArgPair(prepared.Argv, "--workspace", prepared.Access.Effective.Workspace) || !containsRunnerArgPair(prepared.Argv, "--approval-mode", "on-request") {
		t.Fatalf("prepared access/argv incomplete: %#v", prepared)
	}
	receipt, err := runnercore.Execute(context.Background(), prepared, nil)
	if err != nil || receipt.Outcome != runnercore.EventCompleted || receipt.SessionID != "muse-session-fixture" {
		t.Fatalf("direct Muse fixture receipt = %#v err=%v", receipt, err)
	}

	legacy, err := resolveAccess(RunnerProfileDefinition{Harness: string(RunnerCodexExec), PermissionPreset: string(runnercore.PresetDangerFullAccess)}, nil, AgentAccessResolutionContext{Workspace: workspace})
	if err != nil || legacy.Requested != runnercore.PresetDangerFullAccess || legacy.Effective.Filesystem != "danger-full-access" {
		t.Fatalf("legacy access changed: %#v err=%v", legacy, err)
	}

	privateProfile := profile
	privateProfile.Access = &AgentAccessV1{Schema: agentAccessSchemaV1, Mode: accessModeProjects, Network: true, DestructiveActions: "ask", PrivateFolders: []string{private}}
	privateResolved, err := resolveAccess(privateProfile, nil, AgentAccessResolutionContext{Workspace: workspace, Controls: nativeAccessControls(definition, privateProfile.Access), Route: string(RunnerMuseCLI), Transport: "cli"})
	if err != nil || privateResolved.State != "unsupported" || !containsAccessIssue(privateResolved.Issues, runnercore.AccessPrivateReadDeny) {
		t.Fatalf("private-folder native gap was not surfaced: %#v err=%v", privateResolved, err)
	}
	if got, readErr := os.ReadFile(sentinel); readErr != nil || string(got) != "must-survive" {
		t.Fatalf("private sentinel changed: %q err=%v", got, readErr)
	}

	noNetwork := profile
	copyAccess := *profile.Access
	copyAccess.Network = false
	noNetwork.Access = &copyAccess
	changed, err := resolveAccess(noNetwork, nil, AgentAccessResolutionContext{Workspace: workspace, Controls: controls, Route: string(RunnerMuseCLI), Transport: "cli", Version: "Muse Code 1.1.1"})
	if err != nil || changed.Fingerprint == resolved.Fingerprint || changed.Effective.Network {
		t.Fatalf("policy revision did not invalidate effective access: before=%#v after=%#v err=%v", resolved, changed, err)
	}

	// Headless direct Muse has deterministic native denial, not a synthetic
	// approval callback or a piped "yes".
	denied := profile
	deniedAccess := *profile.Access
	deniedAccess.DestructiveActions = "deny"
	denied.Access = &deniedAccess
	deniedResolved, err := resolveAccess(denied, nil, AgentAccessResolutionContext{Workspace: workspace, Controls: controls, Route: string(RunnerMuseCLI), Transport: "cli"})
	if err != nil {
		t.Fatal(err)
	}
	deniedPrepared, err := runnercore.Prepare(context.Background(), definition, runnercore.RunInput{Workspace: workspace, Preset: deniedResolved.Effective.Preset, Access: denied.Access, ResolvedAccess: runnercoreResolvedAccess(&deniedResolved)})
	if err != nil || !containsRunnerArgPair(deniedPrepared.Argv, "--approval-mode", "never") {
		t.Fatalf("headless denial was not compiled natively: argv=%#v err=%v", deniedPrepared.Argv, err)
	}
}

func museFixtureExecutable(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "muse")
	script := `#!/bin/sh
if [ "$1" = "--version" ]; then printf '%s\n' 'Muse Code 1.1.1 (fixture)'; exit 0; fi
printf '%s\n' '{"schema_version":1,"stream":{"kind":"session","id":"muse-session-fixture"},"record_type":"reconciliation","payload_type":"run.terminal.completed","payload":{"text":"fixture complete"}}'
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func runnercoreResolvedAccess(access *ResolvedAccess) *runnercore.ResolvedAccess {
	if access == nil {
		return nil
	}
	converted := runnercore.ResolvedAccess(*access)
	return &converted
}

func containsRunnerArgPair(argv []string, flag, value string) bool {
	for i := 0; i+1 < len(argv); i++ {
		if argv[i] == flag && argv[i+1] == value {
			return true
		}
	}
	return false
}

func containsAccessIssue(issues []runnercore.AccessIssue, control runnercore.AccessControl) bool {
	for _, issue := range issues {
		if issue.Field == string(control) && strings.Contains(issue.Code, "unsupported") {
			return true
		}
	}
	return false
}
