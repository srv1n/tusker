package runner

import (
	"encoding/json"
	"testing"
)

func TestAgentAccessContract(t *testing.T) {
	access := AgentAccessV1{Schema: "tusker.agent-access/v1", Mode: "work_in_projects", Network: true, DestructiveActions: "ask", Folders: []AccessFolder{{Path: "/workspace", Access: "write"}}}
	policy := EffectivePolicy{Preset: PresetWorkspaceNetwork, Filesystem: "workspace-write", Network: true, Approvals: "ask", Workspace: "/workspace", AccessFingerprint: "sha256:access"}
	report := ResolvedAccess{Requested: access, Effective: policy, Controls: []ControlSupport{{Control: AccessWorkspaceWrite, Mechanism: AccessNativeSetting, Coverage: "workspace tools", Evidence: []string{"fixture.workspace-write"}}}, State: "ready", Issues: []AccessIssue{}, Fingerprint: "sha256:access"}
	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	var roundTrip ResolvedAccess
	if err := json.Unmarshal(encoded, &roundTrip); err != nil {
		t.Fatal(err)
	}
	if roundTrip.Effective != policy || roundTrip.State != "ready" || roundTrip.Fingerprint == "" {
		t.Fatalf("round-trip changed contract: %#v", roundTrip)
	}
	if len(roundTrip.Controls) != 1 || roundTrip.Controls[0].Mechanism != AccessNativeSetting {
		t.Fatalf("support coverage changed: %#v", roundTrip.Controls)
	}
}

func TestAgentAccessMigration(t *testing.T) {
	legacy := EffectivePolicy{Preset: PresetDangerFullAccess, Filesystem: "danger-full-access", Network: true, Approvals: "never"}
	if legacy.Preset != PresetDangerFullAccess || legacy.Filesystem != "danger-full-access" || !legacy.Network || legacy.Approvals != "never" {
		t.Fatalf("legacy full-access admission changed: %#v", legacy)
	}
	// Adding the optional fields must not make old policies non-comparable or
	// change their JSON shape when no access object is requested.
	encoded, err := json.Marshal(PreparedLaunch{RequestedPreset: PresetDangerFullAccess, EffectivePolicy: legacy})
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) == "" {
		t.Fatal("empty prepared launch")
	}
}

func TestCommandPolicyPrecedence(t *testing.T) {
	policy := NewCommandPolicy(false, "ask")
	checks := []struct {
		name         string
		request      CommandPolicyRequest
		wantBehavior CommandBehavior
		wantRule     string
	}{
		{name: "routine", request: CommandPolicyRequest{}, wantBehavior: CommandAutomatic, wantRule: "routine"},
		{name: "destructive asks", request: CommandPolicyRequest{Mutating: true, Destructive: true}, wantBehavior: CommandAsk, wantRule: "destructive"},
		{name: "private beats approval", request: CommandPolicyRequest{Mutating: true, Destructive: true, Private: true}, wantBehavior: CommandBlock, wantRule: "private"},
		{name: "outside beats approval", request: CommandPolicyRequest{Mutating: true, Destructive: true, OutsideWorkspace: true}, wantBehavior: CommandBlock, wantRule: "outside_workspace"},
		{name: "catastrophic wins", request: CommandPolicyRequest{Mutating: true, Destructive: true, Catastrophic: true}, wantBehavior: CommandBlock, wantRule: "catastrophic"},
		{name: "review blocks routine write", request: CommandPolicyRequest{Mutating: true, ReviewOnly: true}, wantBehavior: CommandBlock, wantRule: "review_write"},
	}
	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			got := EvaluateCommandPolicy(policy, check.request)
			if got.Behavior != check.wantBehavior || got.Rule != check.wantRule {
				t.Fatalf("decision=%#v, want behavior=%q rule=%q", got, check.wantBehavior, check.wantRule)
			}
		})
	}
	if got := EvaluateCommandPolicy(NewCommandPolicy(false, "deny"), CommandPolicyRequest{Mutating: true, Destructive: true}); got.Behavior != CommandBlock {
		t.Fatalf("deny destructive policy was not terminal: %#v", got)
	}
}
