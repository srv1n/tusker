package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestACPPermissionBroker(t *testing.T) {
	workspace := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(workspace, "outside-link")); err != nil {
		t.Fatalf("create escape symlink: %v", err)
	}

	allowRead := map[string]bool{"read": true}
	allowReadWrite := map[string]bool{"read": true, "write": true}
	base := ACPPermissionRequest{
		AttemptID:      "attempt-1",
		BoundAttemptID: "attempt-1",
		SessionID:      "session-1",
		BoundSessionID: "session-1",
		Workspace:      workspace,
		Target:         "safe/file.txt",
		ToolKind:       "read",
		Options:        []ACPPermissionOption{{OptionID: "allow_once", Kind: "allow_once"}},
	}

	tests := []struct {
		name       string
		mutate     func(*ACPPermissionRequest, *ACPPermissionPolicy)
		want       ACPPermissionOutcome
		wantReason string
		wantOption string
	}{
		{name: "attempt mismatch", mutate: func(r *ACPPermissionRequest, _ *ACPPermissionPolicy) { r.AttemptID = "other" }, want: ACPPermissionReject, wantReason: ACPPermissionReasonAttemptMismatch},
		{name: "session mismatch", mutate: func(r *ACPPermissionRequest, _ *ACPPermissionPolicy) { r.SessionID = "other" }, want: ACPPermissionReject, wantReason: ACPPermissionReasonSessionMismatch},
		{name: "lexical traversal", mutate: func(r *ACPPermissionRequest, _ *ACPPermissionPolicy) { r.Target = "../outside" }, want: ACPPermissionReject, wantReason: ACPPermissionReasonTargetOutsideWorkspace},
		{name: "symlink escape", mutate: func(r *ACPPermissionRequest, _ *ACPPermissionPolicy) { r.Target = "outside-link/secret.txt" }, want: ACPPermissionReject, wantReason: ACPPermissionReasonTargetOutsideWorkspace},
		{name: "unknown tool", mutate: func(r *ACPPermissionRequest, _ *ACPPermissionPolicy) { r.ToolKind = "shell" }, want: ACPPermissionReject, wantReason: ACPPermissionReasonUnknownTool},
		{name: "budget exhausted", mutate: func(_ *ACPPermissionRequest, p *ACPPermissionPolicy) { p.BudgetAuthorized = false }, want: ACPPermissionReject, wantReason: ACPPermissionReasonBudgetExceeded},
		{name: "network denied", mutate: func(r *ACPPermissionRequest, p *ACPPermissionPolicy) {
			r.ToolKind, r.Target = "network", "https://example.test"
			p.AllowedToolKinds = map[string]bool{"network": true}
		}, want: ACPPermissionReject, wantReason: ACPPermissionReasonNetworkNotAllowed},
		{name: "write denied by read only", mutate: func(r *ACPPermissionRequest, p *ACPPermissionPolicy) {
			r.ToolKind = "write"
			p.AllowedToolKinds, p.ReadOnly, p.AllowWorkspaceWrite = allowReadWrite, true, true
		}, want: ACPPermissionReject, wantReason: ACPPermissionReasonReadOnly},
		{name: "write denied by policy", mutate: func(r *ACPPermissionRequest, p *ACPPermissionPolicy) {
			r.ToolKind = "write"
			p.AllowedToolKinds = allowReadWrite
		}, want: ACPPermissionReject, wantReason: ACPPermissionReasonWorkspaceWriteNotAllowed},
		{name: "missing allow once", mutate: func(r *ACPPermissionRequest, _ *ACPPermissionPolicy) {
			r.Options = []ACPPermissionOption{{OptionID: "reject_once"}}
		}, want: ACPPermissionReject, wantReason: ACPPermissionReasonAllowOnceUnavailable},
		{name: "allow always only", mutate: func(r *ACPPermissionRequest, _ *ACPPermissionPolicy) {
			r.Options = []ACPPermissionOption{{OptionID: "allow_always", Kind: "allow_always"}}
		}, want: ACPPermissionReject, wantReason: ACPPermissionReasonAllowOnceUnavailable},
		{name: "allow once id cannot disguise allow always kind", mutate: func(r *ACPPermissionRequest, _ *ACPPermissionPolicy) {
			r.Options = []ACPPermissionOption{{OptionID: "allow_once", Kind: "allow_always"}}
		}, want: ACPPermissionReject, wantReason: ACPPermissionReasonAllowOnceUnavailable},
		{name: "valid read", mutate: func(_ *ACPPermissionRequest, _ *ACPPermissionPolicy) {}, want: ACPPermissionAllowOnce, wantReason: ACPPermissionReasonAllowed, wantOption: "allow_once"},
		{name: "valid write", mutate: func(r *ACPPermissionRequest, p *ACPPermissionPolicy) {
			r.ToolKind = "write"
			p.AllowedToolKinds, p.AllowWorkspaceWrite = allowReadWrite, true
		}, want: ACPPermissionAllowOnce, wantReason: ACPPermissionReasonAllowed, wantOption: "allow_once"},
		{name: "cancelled", mutate: func(r *ACPPermissionRequest, _ *ACPPermissionPolicy) { r.Cancelled = true }, want: ACPPermissionCancelled, wantReason: ACPPermissionReasonCancelled},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := base
			req.Options = append([]ACPPermissionOption(nil), base.Options...)
			policy := ACPPermissionPolicy{AllowedToolKinds: allowRead, BudgetAuthorized: true}
			tt.mutate(&req, &policy)

			got := EvaluateACPPermission(req, policy)
			if got.Outcome != tt.want || got.ReasonCode != tt.wantReason || got.OptionID != tt.wantOption {
				t.Fatalf("decision = %#v, want outcome=%q reason=%q option=%q", got, tt.want, tt.wantReason, tt.wantOption)
			}
			if got.Audit.OperationClass == "" || got.Audit.PolicyRule == "" || got.Audit.Outcome != string(got.Outcome) || got.Audit.ReasonCode != got.ReasonCode {
				t.Fatalf("invalid audit: %#v", got.Audit)
			}
			if got.Audit.TargetDigest == req.Target || got.Audit.TargetDigest == "" {
				t.Fatalf("audit leaked or omitted target digest: %#v", got.Audit)
			}
		})
	}
}

func TestACPPermissionBrokerAuditIsDeterministicAndRedacted(t *testing.T) {
	workspace := t.TempDir()
	req := ACPPermissionRequest{
		AttemptID: "attempt-1", BoundAttemptID: "attempt-1", SessionID: "session-1", BoundSessionID: "session-1",
		Workspace: workspace, Target: "private/token=not-for-a-log", ToolKind: "read",
		Options: []ACPPermissionOption{{OptionID: "allow_once", Kind: "allow_once"}},
	}
	policy := ACPPermissionPolicy{AllowedToolKinds: map[string]bool{"read": true}, BudgetAuthorized: true}
	first, second := EvaluateACPPermission(req, policy), EvaluateACPPermission(req, policy)
	if first.Audit != second.Audit {
		t.Fatalf("audit is not deterministic: %#v != %#v", first.Audit, second.Audit)
	}
	if first.Audit.TargetDigest == req.Target || first.Audit.TargetDigest == "" {
		t.Fatalf("target was retained in audit: %#v", first.Audit)
	}
}
