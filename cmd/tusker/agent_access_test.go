package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	runnercore "tusker/internal/runner"
)

func TestAgentAccessContract(t *testing.T) {
	t.Run("defaults and wire shape", func(t *testing.T) {
		access := newAgentAccessDefaults()
		if access.Schema != agentAccessSchemaV1 || access.Mode != accessModeProjects || !access.Network || access.DestructiveActions != "deny" {
			t.Fatalf("defaults = %#v", access)
		}
		encoded, err := json.Marshal(access)
		if err != nil || !strings.Contains(string(encoded), `"schema":"tusker.agent-access/v1"`) {
			t.Fatalf("wire shape = %s err=%v", encoded, err)
		}
	})

	workspace := t.TempDir()
	reference := filepath.Join(workspace, "reference")
	if err := os.Mkdir(reference, 0o755); err != nil {
		t.Fatal(err)
	}
	profile := RunnerProfileDefinition{Harness: string(RunnerCodexExec), Model: "gpt-test", Access: &AgentAccessV1{
		Schema: agentAccessSchemaV1, Mode: accessModeProjects, Network: true, DestructiveActions: "ask",
		Folders: []AgentAccessFolder{{Path: reference, Access: "read"}},
	}}
	controls := []runnercore.ControlSupport{
		{Control: runnercore.AccessWorkspaceWrite, Mechanism: runnercore.AccessNativeSetting},
		{Control: runnercore.AccessReferenceRead, Mechanism: runnercore.AccessNativeSetting},
		{Control: runnercore.AccessNetwork, Mechanism: runnercore.AccessNativeSetting},
		{Control: runnercore.AccessDestructiveApproval, Mechanism: runnercore.AccessNativeSetting},
	}
	resolved, err := resolveAccess(profile, nil, AgentAccessResolutionContext{Workspace: workspace, References: []string{reference}, Controls: controls, Route: "codex/cli", ExecutableIdentity: "sha256:test", Transport: "cli", Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.State != "ready" || resolved.Effective.Filesystem != "workspace-write" || !resolved.Effective.Network || resolved.Effective.Approvals != "ask" {
		t.Fatalf("resolved = %#v", resolved)
	}
	if len(resolved.Folders) < 2 || len(resolved.References) != 1 || resolved.Fingerprint == "" {
		t.Fatalf("derived scope = %#v", resolved)
	}

	unsupported, err := resolveAccess(profile, nil, AgentAccessResolutionContext{Workspace: workspace, Controls: []runnercore.ControlSupport{{Control: runnercore.AccessNetwork, Mechanism: runnercore.AccessAdvisory, Coverage: "prompt only"}}})
	if err != nil {
		t.Fatal(err)
	}
	if unsupported.State != "unsupported" || !containsAccessIssue(unsupported.Issues, runnercore.AccessNetwork) {
		t.Fatalf("unsupported = %#v", unsupported)
	}

	if err := validateAgentAccessRaw(map[string]any{"automation": map[string]any{"profiles": map[string]any{"p": map[string]any{"access": map[string]any{"schema": agentAccessSchemaV1, "future": true}}}}}, "config.yaml"); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("unknown access field accepted: %v", err)
	}
}

func TestAgentAccessEditor(t *testing.T) {
	access := newAgentAccessDefaults()
	if access.Mode != accessModeProjects || !access.Network || access.DestructiveActions != "deny" || len(access.Folders) != 0 || len(access.PrivateFolders) != 0 {
		t.Fatalf("new editor defaults = %#v", access)
	}
	if err := validateAgentAccessDefinition("new-profile", access, "config.yaml"); err != nil {
		t.Fatalf("new editor defaults are not saveable: %v", err)
	}
}

func TestAgentAccessBoundaries(t *testing.T) {
	workspace := t.TempDir()
	private := filepath.Join(workspace, "private")
	if err := os.Mkdir(private, 0o755); err != nil {
		t.Fatal(err)
	}
	profile := RunnerProfileDefinition{Access: &AgentAccessV1{
		Schema: agentAccessSchemaV1, Mode: accessModeProjects, Network: true, DestructiveActions: "ask",
		PrivateFolders: []string{private},
	}}
	controls := []runnercore.ControlSupport{
		{Control: runnercore.AccessWorkspaceWrite, Mechanism: runnercore.AccessNativeSetting},
		{Control: runnercore.AccessNetwork, Mechanism: runnercore.AccessNativeSetting},
		{Control: runnercore.AccessDestructiveApproval, Mechanism: runnercore.AccessNativeSetting},
		{Control: runnercore.AccessPrivateReadDeny, Mechanism: runnercore.AccessNativeHook},
		{Control: runnercore.AccessPrivateWriteDeny, Mechanism: runnercore.AccessNativeHook},
	}
	resolved, err := resolveAccess(profile, nil, AgentAccessResolutionContext{Workspace: workspace, Controls: controls})
	if err != nil || resolved.State != "ready" {
		t.Fatalf("private descendant should be allowed: %#v err=%v", resolved, err)
	}

	for _, conflict := range []string{workspace, filepath.Dir(workspace)} {
		bad := profile
		bad.Access = &AgentAccessV1{Schema: agentAccessSchemaV1, Mode: accessModeProjects, Network: true, DestructiveActions: "ask", PrivateFolders: []string{conflict}}
		if _, err := resolveAccess(bad, nil, AgentAccessResolutionContext{Workspace: workspace, Controls: controls}); err == nil || !strings.Contains(err.Error(), "workspace_private_conflict") {
			t.Fatalf("private ancestor/equal workspace accepted for %q: %v", conflict, err)
		}
	}

	missing := profile
	missing.Access = &AgentAccessV1{Schema: agentAccessSchemaV1, Mode: accessModeProjects, Network: true, DestructiveActions: "ask"}
	missingResolved, err := resolveAccess(missing, nil, AgentAccessResolutionContext{Workspace: workspace})
	if err != nil || missingResolved.State != "unsupported" || !containsAccessIssue(missingResolved.Issues, runnercore.AccessWorkspaceWrite) || !containsAccessIssue(missingResolved.Issues, runnercore.AccessNetwork) || !containsAccessIssue(missingResolved.Issues, runnercore.AccessDestructiveApproval) {
		t.Fatalf("missing capability declarations were accepted: %#v err=%v", missingResolved, err)
	}
}

func TestAgentAccessMigration(t *testing.T) {
	vault := automationTestVault(t)
	t.Setenv("TUSKER_CONFIG", filepath.Join(t.TempDir(), "config.yaml"))
	workspace := t.TempDir()
	legacyAccess := `automation:
  profiles:
    access-profile:
      harness: codex_exec
      model: gpt-access
      effort: medium
      access:
        schema: tusker.agent-access/v1
        mode: work_in_projects
        network: true
        destructive_actions: ask
        folders: []
        private_folders: []
`
	writeGlobalConfigForTest(t, legacyAccess)
	report, err := modelLevelsRead(vault)
	if err != nil {
		t.Fatal(err)
	}
	profile := report.Profiles["access-profile"]
	if profile.Access == nil || profile.Access.Network != true || profile.PermissionPreset != "" {
		t.Fatalf("access profile was not lossless: %#v", profile)
	}
	edit := Args{"vault": vault, "scope": "global", "name": "access-profile", "display-name": "renamed", "harness": "codex_exec", "model": "gpt-access-2", "effort": "high", "if-revision": report.Revision, "_no-output": "true"}
	if err := modelsProfileSetCmd(edit); err != nil {
		t.Fatal(err)
	}
	after, err := modelLevelsRead(vault)
	if err != nil {
		t.Fatal(err)
	}
	if after.Profiles["access-profile"].Access == nil || !after.Profiles["access-profile"].Access.Network || after.Profiles["access-profile"].PermissionPreset != "" {
		t.Fatalf("model-only edit widened or migrated access: %#v", after.Profiles["access-profile"])
	}
	if after.Profiles["access-profile"].Model != "gpt-access-2" || after.Profiles["access-profile"].DisplayName != "renamed" {
		t.Fatalf("model edit did not apply: %#v", after.Profiles["access-profile"])
	}

	private := filepath.Join(workspace, "private")
	if err := os.Mkdir(private, 0o755); err != nil {
		t.Fatal(err)
	}
	conflict := profile
	conflict.Access = &AgentAccessV1{Schema: agentAccessSchemaV1, Mode: accessModeProjects, Network: true, DestructiveActions: "ask"}
	if _, err := resolveAccess(conflict, []string{private}, AgentAccessResolutionContext{Workspace: private}); err == nil || !strings.Contains(err.Error(), "workspace_private_conflict") {
		t.Fatalf("private workspace conflict accepted: %v", err)
	}
}
