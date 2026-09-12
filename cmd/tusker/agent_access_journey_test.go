package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	runnercore "tusker/internal/runner"
)

// TestAgentAccessProviderFreeJourney keeps the whole access journey local:
// legacy profile upgrade, routine callback decisions, a bounded approval,
// mandatory private-folder denial, and an unavailable provider route.
func TestAgentAccessProviderFreeJourney(t *testing.T) {
	vault := automationTestVault(t)
	legacyConfig := `automation:
  profiles:
    legacy-full:
      display_name: Legacy Full access
      harness: codex_exec
      model: gpt-fixture
      effort: medium
      permission_preset: danger-full-access
      sandbox:
        mode: danger-full-access
        network: true
      eligible_tiers: [light, standard]
`
	if err := writeConfigTextAtomically(managedTuskerLocalConfigPath(vault), legacyConfig); err != nil {
		t.Fatal(err)
	}
	before, err := modelLevelsRead(vault)
	if err != nil {
		t.Fatal(err)
	}
	legacy, ok := before.Profiles["legacy-full"]
	if !ok || legacy.PermissionPreset != "danger-full-access" || legacy.Access != nil {
		t.Fatalf("fixture did not start as a legacy Full access profile: %#v", legacy)
	}

	workspace := filepath.Join(t.TempDir(), "project")
	private := filepath.Join(workspace, "private")
	build := filepath.Join(workspace, "build")
	if err := os.MkdirAll(private, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(build, 0o755); err != nil {
		t.Fatal(err)
	}

	access := &AgentAccessV1{
		Schema:             agentAccessSchemaV1,
		Mode:               accessModeProjects,
		Network:            true,
		DestructiveActions: "ask",
		Folders:            []AgentAccessFolder{},
		PrivateFolders:     []string{private},
	}
	encodedAccess, err := json.Marshal(access)
	if err != nil {
		t.Fatal(err)
	}
	if err := modelsProfileSetCmd(Args{
		"vault":          vault,
		"scope":          "project",
		"name":           "legacy-full",
		"display-name":   legacy.DisplayName,
		"harness":        legacy.Harness,
		"model":          legacy.Model,
		"effort":         legacy.Effort,
		"eligible-tiers": strings.Join(legacy.EligibleTiers, ","),
		"access":         string(encodedAccess),
		"if-revision":    before.Revision,
		"_no-output":     "true",
	}); err != nil {
		t.Fatal(err)
	}
	after, err := modelLevelsRead(vault)
	if err != nil {
		t.Fatal(err)
	}
	upgraded, ok := after.Profiles["legacy-full"]
	if !ok || upgraded.Access == nil || upgraded.PermissionPreset != "" {
		t.Fatalf("Use project access did not replace legacy authority: %#v", upgraded)
	}
	if upgraded.DisplayName != legacy.DisplayName || upgraded.Model != legacy.Model || upgraded.Effort != legacy.Effort || strings.Join(upgraded.EligibleTiers, ",") != strings.Join(legacy.EligibleTiers, ",") {
		t.Fatalf("profile identity or tier references changed during upgrade: before=%#v after=%#v", legacy, upgraded)
	}

	// The fixture route declares all requested controls affirmatively. The
	// unsupported case below uses the real Codex native mapping, which has no
	// private-folder hook, and must remain unavailable.
	controls := []runnercore.ControlSupport{
		{Control: runnercore.AccessWorkspaceWrite, Mechanism: runnercore.AccessNativeSetting},
		{Control: runnercore.AccessNetwork, Mechanism: runnercore.AccessNativeSetting},
		{Control: runnercore.AccessDestructiveApproval, Mechanism: runnercore.AccessNativeSetting},
		{Control: runnercore.AccessPrivateReadDeny, Mechanism: runnercore.AccessNativeHook},
		{Control: runnercore.AccessPrivateWriteDeny, Mechanism: runnercore.AccessNativeHook},
	}
	resolved, err := resolveAccess(RunnerProfileDefinition{Harness: string(RunnerCodexExec), Model: upgraded.Model, Effort: upgraded.Effort, Access: upgraded.Access}, nil, AgentAccessResolutionContext{
		Workspace: workspace,
		Controls:  controls,
		Route:     "codex-fixture",
		Transport: "cli",
	})
	if err != nil || resolved.State != "ready" {
		t.Fatalf("upgraded project access did not resolve: %#v err=%v", resolved, err)
	}

	stateRoot := filepath.Join(t.TempDir(), "state")
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	policy := CodexPolicy{ApprovalPolicy: "on-request", ThreadSandbox: "workspace-write", TurnSandboxPolicy: "workspace-write"}
	handle := &codexLiveHandle{
		attemptID:      "journey-attempt",
		projectID:      "journey-project",
		itemID:         "journey-task",
		policy:         policy,
		privateFolders: []string{private},
		cmd:            &exec.Cmd{Dir: workspace},
		runtimeStore:   store,
		pending:        map[string]chan codexRPCResponse{},
	}

	// Routine work is accepted by the native callback without creating an
	// approval row. Staging is a workspace write, but not a destructive approval.
	for index, command := range []string{"git status --short", "git add result.txt"} {
		writer := &recordingWriteCloser{}
		handle.stdin = writer
		handle.handleServerRequest("execCommandApproval", fmt.Sprintf("routine-%d", index), journeyCommandParams(t, workspace, command))
		if result := lastCodexCallbackResult(t, writer); result["decision"] != "approved" {
			t.Fatalf("routine command was prompted or denied: command=%q result=%#v", command, result)
		}
	}
	rows, err := store.ListAgentAccessApprovals("journey-project", "journey-task")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("routine commands created approval rows: %#v", rows)
	}

	// A bounded destructive action creates exactly one immutable request and
	// proceeds only after the human-scoped allow-once decision settles it.
	approvalWriter := &recordingWriteCloser{}
	handle.stdin = approvalWriter
	done := make(chan struct{})
	go func() {
		handle.handleServerRequest("execCommandApproval", "bounded-destructive", journeyCommandParams(t, workspace, "rm -rf build"))
		close(done)
	}()
	var pending AgentAccessApproval
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		rows, listErr := store.ListAgentAccessApprovals("journey-project", "journey-task")
		if listErr != nil {
			t.Fatal(listErr)
		}
		if len(rows) == 1 {
			pending = rows[0]
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if pending.State != AgentAccessApprovalPending || pending.NativeOptionKind != AgentAccessApprovalAllowOnce {
		t.Fatalf("bounded action did not offer a bound allow-once request: %#v", pending)
	}
	if _, err := store.SettleAgentAccessApproval(AgentAccessApprovalResponse{RequestID: pending.RequestID, ExpectedRevision: pending.StateRevision, Decision: AgentAccessApprovalAllowOnce, Actor: "human:fixture"}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("bounded destructive callback did not resume after allow-once")
	}
	if result := lastCodexCallbackResult(t, approvalWriter); result["decision"] != "approved" {
		t.Fatalf("bounded destructive action was not approved after allow-once: %#v", result)
	}

	// Mandatory private-folder denial happens before persistence, so approval
	// cannot override the configured exclusion.
	privateWriter := &recordingWriteCloser{}
	handle.stdin = privateWriter
	handle.handleServerRequest("execCommandApproval", "private-denial", journeyCommandParams(t, workspace, "rm -rf private"))
	if result := lastCodexCallbackResult(t, privateWriter); result["decision"] != "denied" || !strings.Contains(fmt.Sprint(result["reason"]), "private folder") {
		t.Fatalf("private-folder request was approvable: %#v", result)
	}
	rows, err = store.ListAgentAccessApprovals("journey-project", "journey-task")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("private-folder denial created an approval row: %#v", rows)
	}

	definition := runnercore.HarnessDefinition{ID: string(RunnerCodexExec), Dialect: "codex", Transport: runnercore.TransportCLI}
	unsupported, err := resolveAccess(RunnerProfileDefinition{Harness: string(RunnerCodexExec), Access: access}, nil, AgentAccessResolutionContext{Workspace: workspace, Controls: nativeAccessControls(definition, access), Route: string(RunnerCodexExec), Transport: "cli"})
	if err != nil || unsupported.State != "unsupported" || !containsAccessIssue(unsupported.Issues, runnercore.AccessPrivateReadDeny) {
		t.Fatalf("unsupported provider route was presented as available: %#v err=%v", unsupported, err)
	}
}

func journeyCommandParams(t *testing.T, workspace, command string) []byte {
	t.Helper()
	params, err := json.Marshal(map[string]string{"cwd": workspace, "command": command})
	if err != nil {
		t.Fatal(err)
	}
	return params
}
