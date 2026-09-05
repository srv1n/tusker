package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDirectExecutionRegistration(t *testing.T) {
	state := filepath.Join(t.TempDir(), "state")
	store, err := OpenRuntimeStore(state)
	if err != nil {
		t.Fatal(err)
	}
	direct, err := store.CreateDirectExecution(DirectExecutionInput{ProjectID: "project-1", DisplayName: "Lease audit", Source: "direct_codex", Provider: "codex", AgentType: "reviewer", Creator: "operator"})
	if err != nil {
		t.Fatal(err)
	}
	if direct.ExecutionID == "" || direct.ExecutionID != direct.RootExecutionID || direct.TaskID != "" || direct.WaveID != "" {
		t.Fatalf("direct registration lost separate identity: %#v", direct)
	}
	view, err := store.ExecutionView(direct.ExecutionID)
	if err != nil || view.EffectiveDisplayName != "Lease audit" || view.ProofEligible {
		t.Fatalf("unbound registration view=%#v err=%v", view, err)
	}
	attached, created, err := store.AttachExecution(ExecutionAttachmentInput{ProjectID: "project-1", ExecutionID: direct.ExecutionID, Provider: "codex", ProviderSessionID: "thread-1", SessionRef: "local-1", Source: "direct_codex", Actor: "operator"})
	if err != nil || !created || attached.ProviderSessionID != "thread-1" {
		t.Fatalf("attach=%#v created=%v err=%v", attached, created, err)
	}
	replayed, created, err := store.AttachExecution(ExecutionAttachmentInput{ProjectID: "project-1", ExecutionID: direct.ExecutionID, Provider: "codex", ProviderSessionID: "thread-1", SessionRef: "different-local-ref", Actor: "operator"})
	if err != nil || created || replayed.ExecutionID != direct.ExecutionID {
		t.Fatalf("attach replay=%#v created=%v err=%v", replayed, created, err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = OpenRuntimeStore(state)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	view, err = store.ExecutionView(direct.ExecutionID)
	if err != nil || view.ProviderSessionID != "thread-1" || view.SessionRef != "local-1" {
		t.Fatalf("restart lost attachment: %#v err=%v", view, err)
	}
	inbox, err := store.ListUnboundDirectExecutions("project-1")
	if err != nil || len(inbox) != 1 || inbox[0].ExecutionID != direct.ExecutionID {
		t.Fatalf("unbound inbox=%#v err=%v", inbox, err)
	}
	other, err := store.CreateDirectExecution(DirectExecutionInput{ProjectID: "project-1", Source: "codex_cloud"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.AttachExecution(ExecutionAttachmentInput{ProjectID: "project-1", ExecutionID: other.ExecutionID, Provider: "codex", ProviderSessionID: "thread-1"}); err == nil {
		t.Fatal("provider identity moved between executions")
	}
}

func TestExecutionBindingAuthority(t *testing.T) {
	store := executionLedgerStore(t)
	direct, err := store.CreateDirectExecution(DirectExecutionInput{ProjectID: "project-1", Source: "direct_claude"})
	if err != nil {
		t.Fatal(err)
	}
	bound, err := store.BindExecution(ExecutionBindingInput{ProjectID: "project-1", ExecutionID: direct.ExecutionID, TaskID: "ORC-T-0067", WaveID: "W-0007", Actor: "operator"}, "bind")
	if err != nil || !bound.ProofEligible || bound.BindingGeneration != 1 || bound.BoundTaskID != "ORC-T-0067" {
		t.Fatalf("bind=%#v err=%v", bound, err)
	}
	if direct.TaskID != "" || direct.WaveID != "" {
		t.Fatalf("binding rewrote immutable record: %#v", direct)
	}
	renamed, err := store.RenameExecution("project-1", direct.ExecutionID, "Human readable", "operator")
	if err != nil || renamed.EffectiveDisplayName != "Human readable" || renamed.BindingGeneration != 1 {
		t.Fatalf("rename=%#v err=%v", renamed, err)
	}
	rebound, err := store.BindExecution(ExecutionBindingInput{ProjectID: "project-1", ExecutionID: direct.ExecutionID, TaskID: "ORC-T-0068", WaveID: "W-0007", Actor: "operator"}, "rebind")
	if err != nil || rebound.BindingGeneration != 2 || rebound.BoundTaskID != "ORC-T-0068" {
		t.Fatalf("rebind=%#v err=%v", rebound, err)
	}
	detached, err := store.BindExecution(ExecutionBindingInput{ProjectID: "project-1", ExecutionID: direct.ExecutionID, Actor: "operator"}, "detach")
	if err != nil || detached.ProofEligible || detached.BindingGeneration != 3 || detached.BoundTaskID != "" {
		t.Fatalf("detach=%#v err=%v", detached, err)
	}
	if err := store.UpsertRun(RunStatus{ProjectID: "project-1", RecordID: "ORC-T-0069", ItemID: "ORC-T-0069", LeaseState: string(LeaseStateRunning)}); err != nil {
		t.Fatal(err)
	}
	other, err := store.CreateDirectExecution(DirectExecutionInput{ProjectID: "project-1", Source: "direct_codex"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.BindExecution(ExecutionBindingInput{ProjectID: "project-1", ExecutionID: other.ExecutionID, TaskID: "ORC-T-0069", WaveID: "W-0007"}, "bind"); err == nil {
		t.Fatal("binding stole a live task owner")
	}
	var bindings, runs int
	if err := store.queryRowScan(`SELECT COUNT(*) FROM execution_binding_events WHERE execution_id = ?`, []any{direct.ExecutionID}, &bindings); err != nil || bindings != 3 {
		t.Fatalf("binding audit rows=%d err=%v", bindings, err)
	}
	if err := store.queryRowScan(`SELECT COUNT(*) FROM runs`, nil, &runs); err != nil || runs != 1 {
		t.Fatalf("binding minted runtime authority: runs=%d err=%v", runs, err)
	}
	foreign, err := store.CreateDirectExecution(DirectExecutionInput{ProjectID: "project-2", Source: "direct_codex"})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertRun(RunStatus{ProjectID: "project-2", RecordID: "ORC-T-0070", ItemID: "ORC-T-0070", LeaseState: string(LeaseStateRunning)}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.BindExecution(ExecutionBindingInput{ProjectID: "project-1", ExecutionID: foreign.ExecutionID, TaskID: "ORC-T-0070", WaveID: "W-0007"}, "bind"); err == nil {
		t.Fatal("cross-project binding bypassed selected project scope")
	}
	var foreignBindings int
	if err := store.queryRowScan(`SELECT COUNT(*) FROM execution_binding_events WHERE execution_id = ?`, []any{foreign.ExecutionID}, &foreignBindings); err != nil || foreignBindings != 0 {
		t.Fatalf("cross-project binding wrote audit: %d %v", foreignBindings, err)
	}
	if _, err := store.RenameExecution("project-1", "missing", "nope", "operator"); err == nil {
		t.Fatal("rename accepted nonexistent execution")
	}
	if _, err := store.exec(`INSERT INTO execution_name_events(event_id, execution_id, display_name, search_label, actor, created_at) VALUES ('orphan-name','missing','name','name','operator','2026-01-01T00:00:00Z')`); err == nil {
		t.Fatal("orphan name event bypassed trigger")
	}
	if _, err := store.exec(`INSERT INTO execution_attachment_events(event_id, execution_id, project_id, provider, provider_session_id, actor, created_at) VALUES ('orphan-attach','missing','project-1','codex','thread-missing','operator','2026-01-01T00:00:00Z')`); err == nil {
		t.Fatal("orphan attachment event bypassed trigger")
	}
	if _, err := store.exec(`INSERT INTO execution_binding_events(event_id, execution_id, generation, action, actor, created_at) VALUES ('orphan-bind','missing',1,'detach','operator','2026-01-01T00:00:00Z')`); err == nil {
		t.Fatal("orphan binding event bypassed trigger")
	}
	blank := ExecutionBindingInput{ExecutionID: direct.ExecutionID, TaskID: "ORC-T-0099", WaveID: "W-0007", Actor: "operator"}
	if _, err := store.RenameExecution("", direct.ExecutionID, "scope bypass", "operator"); err == nil {
		t.Fatal("blank project rename was accepted")
	}
	if _, _, err := store.AttachExecution(ExecutionAttachmentInput{ExecutionID: direct.ExecutionID, Provider: "claude", ProviderSessionID: "blank-scope"}); err == nil {
		t.Fatal("blank project attach was accepted")
	}
	if _, err := store.BindExecution(blank, "bind"); err == nil {
		t.Fatal("blank project bind was accepted")
	}
	if _, err := store.BindExecution(ExecutionBindingInput{ExecutionID: direct.ExecutionID, Actor: "operator"}, "detach"); err == nil {
		t.Fatal("blank project detach was accepted")
	}
	var names, attachments, bindingEvents int
	if err := store.queryRowScan(`SELECT COUNT(*) FROM execution_name_events WHERE execution_id = ?`, []any{direct.ExecutionID}, &names); err != nil || names != 1 {
		t.Fatalf("blank-scope rename wrote audit: %d %v", names, err)
	}
	if err := store.queryRowScan(`SELECT COUNT(*) FROM execution_attachment_events WHERE execution_id = ?`, []any{direct.ExecutionID}, &attachments); err != nil || attachments != 0 {
		t.Fatalf("blank-scope attach wrote audit: %d %v", attachments, err)
	}
	if err := store.queryRowScan(`SELECT COUNT(*) FROM execution_binding_events WHERE execution_id = ?`, []any{direct.ExecutionID}, &bindingEvents); err != nil || bindingEvents != 3 {
		t.Fatalf("blank-scope binding wrote audit: %d %v", bindingEvents, err)
	}
}

func TestDirectExecutionLaunchGuard(t *testing.T) {
	clearAgentSessionEnvForTest(t)
	t.Setenv("TUSKER_ATTEMPT_ID", "attempt-1")
	if err := rejectAgentSpawn("execution launch"); err == nil {
		t.Fatal("dispatched worker was allowed to launch direct execution")
	}
	os.Unsetenv("TUSKER_ATTEMPT_ID")
	t.Setenv("CODEX_THREAD_ID", "thread-1")
	if err := rejectAgentSpawn("execution launch"); err == nil {
		t.Fatal("nested Codex session was allowed to launch direct execution")
	}
	os.Unsetenv("CODEX_THREAD_ID")
	t.Setenv("CLAUDECODE", "1")
	if err := rejectAgentSpawn("execution launch"); err == nil {
		t.Fatal("nested Claude session was allowed to launch direct execution")
	}
	os.Unsetenv("CLAUDECODE")
	stateRoot := filepath.Join(t.TempDir(), "unopened-state")
	t.Setenv("TUSKER_ATTEMPT_ID", "attempt-2")
	if err := executionCmd(Args{"vault": filepath.Join(t.TempDir(), "missing-vault"), "state-root": stateRoot, "id": "exec_missing"}, "launch"); err == nil {
		t.Fatal("launch command accepted dispatched worker")
	}
	if entries, err := os.ReadDir(stateRoot); err == nil && len(entries) != 0 {
		t.Fatalf("launch guard opened state before refusal: %#v", entries)
	}
	os.Unsetenv("TUSKER_ATTEMPT_ID")
	repo := t.TempDir()
	vault := filepath.Join(repo, ".tusker")
	if err := os.MkdirAll(vault, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(managedTuskerConfigPath(filepath.Join(repo, defaultRepoVaultDir)), []byte("project_id: project-cloud\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cloudState := filepath.Join(t.TempDir(), "cloud-state")
	store, err := OpenRuntimeStore(cloudState)
	if err != nil {
		t.Fatal(err)
	}
	cloud, err := store.CreateDirectExecution(DirectExecutionInput{ProjectID: "project-cloud", Source: "codex_cloud"})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := executionCmd(Args{"vault": vault, "state-root": cloudState, "id": cloud.ExecutionID, "pid": "99"}, "launch"); err == nil {
		t.Fatal("cloud launch accepted arbitrary local pid")
	}
	if err := executionCmd(Args{"vault": vault, "state-root": cloudState, "id": cloud.ExecutionID, "json": "true"}, "launch"); err != nil {
		t.Fatalf("cloud launch unavailable-process projection: %v", err)
	}
}
