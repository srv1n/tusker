package main

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestGateLedgerToolchainIdentityAndLegacyConservatism(t *testing.T) {
	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	entry := GateLedgerEntry{ID: "go1", ProjectID: "app", TreeHash: "tree", Command: "go test ./...", Profile: "full", Toolchain: "go1"}
	if err := store.RecordGateLedger(entry); err != nil {
		t.Fatal(err)
	}
	hit, err := store.FindGateLedger("app", "tree", entry.Command, "full", "go1")
	if err != nil || hit == nil || hit.ID != entry.ID {
		t.Fatalf("same toolchain did not hit: %#v %v", hit, err)
	}
	hit, err = store.FindGateLedger("app", "tree", entry.Command, "full", "go2")
	if err != nil || hit != nil {
		t.Fatalf("changed toolchain reused green proof: %#v %v", hit, err)
	}
	if _, err := store.exec(`INSERT INTO gate_ledger (id, project_id, tree_hash, command, profile, toolchain, host, duration_ms, passed_at) VALUES ('legacy', 'app', 'legacy-tree', 'go test ./...', 'full', '', '', 0, '2026-07-25T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	hit, err = store.FindGateLedger("app", "legacy-tree", entry.Command, "full", "go1")
	if err != nil || hit != nil {
		t.Fatalf("legacy empty-toolchain row satisfied modern lookup: %#v %v", hit, err)
	}
}

func TestGateLedgerPersistsStructuredLifecycleProviderReceipt(t *testing.T) {
	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	receipt := &GateProviderReceipt{
		LifecycleID: "container:scope-1", ReceiptDigest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		RuntimeDigest: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", PolicyDigest: "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
		AttestationDigest: "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd", ImageOrVMID: "sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
	}
	entry := GateLedgerEntry{ID: "certified", ProjectID: "app", TreeHash: "tree", Command: "go test ./...", Profile: "full", Toolchain: "provider-v3", ProviderReceipt: receipt}
	if err := store.RecordGateLedger(entry); err != nil {
		t.Fatal(err)
	}
	hit, err := store.FindGateLedger(entry.ProjectID, entry.TreeHash, entry.Command, entry.Profile, entry.Toolchain)
	if err != nil || hit == nil || hit.ProviderReceipt == nil || *hit.ProviderReceipt != *receipt {
		t.Fatalf("structured lifecycle receipt was not durable: %#v %v", hit, err)
	}
}

func TestFullGateLedgerRejectsReceiptFromDifferentTrustedProfile(t *testing.T) {
	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	receipt := &GateProviderReceipt{
		LifecycleID: "container:scope-1", ReceiptDigest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		RuntimeDigest: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", PolicyDigest: "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
		AttestationDigest: "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd", ImageOrVMID: "sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
	}
	entry := GateLedgerEntry{ID: "wrong-policy", ProjectID: "app", TreeHash: "tree", Command: "go test ./...", Profile: "full", Toolchain: "provider-v3", ProviderReceipt: receipt}
	if err := store.RecordGateLedger(entry); err != nil {
		t.Fatal(err)
	}
	current := &v7ExternalFullGateProvider{runtimeDigest: receipt.RuntimeDigest, policyDigest: "sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff", attestationDigest: receipt.AttestationDigest, imageOrVMID: receipt.ImageOrVMID}
	ledger := v7CertifiedFullGateLedger{store: store, verifier: current}
	if hit, err := ledger.FindGateLedger(entry.ProjectID, entry.TreeHash, entry.Command, entry.Profile, entry.Toolchain); err != nil || hit != nil {
		t.Fatalf("different trusted provider policy reused green proof: %#v %v", hit, err)
	}
}

func TestGateLedgerMigratesOldUniqueConstraintWithoutReusingProof(t *testing.T) {
	root := t.TempDir()
	db, err := sql.Open("sqlite", runtimeStoreSQLiteDSN(filepath.Join(root, "daemon.db"), runtimeStoreBusyTimeout))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE gate_ledger (
		id TEXT PRIMARY KEY, project_id TEXT NOT NULL, tree_hash TEXT NOT NULL,
		command TEXT NOT NULL, profile TEXT NOT NULL DEFAULT '', host TEXT NOT NULL DEFAULT '',
		duration_ms INTEGER NOT NULL DEFAULT 0, passed_at TEXT NOT NULL,
		UNIQUE(project_id, tree_hash, command, profile))`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO gate_ledger VALUES ('old', 'app', 'tree', 'go test', 'full', '', 0, '2026-07-25T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := OpenRuntimeStore(root)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if hit, err := store.FindGateLedger("app", "tree", "go test", "full", "go1"); err != nil || hit != nil {
		t.Fatalf("migrated legacy row became reusable: %#v %v", hit, err)
	}
	if err := store.RecordGateLedger(GateLedgerEntry{ID: "new", ProjectID: "app", TreeHash: "tree", Command: "go test", Profile: "full", Toolchain: "go1"}); err != nil {
		t.Fatalf("new toolchain identity could not coexist with legacy row: %v", err)
	}
	if hit, err := store.FindGateLedger("app", "tree", "go test", "full", "go1"); err != nil || hit == nil || hit.ID != "new" {
		t.Fatalf("new migrated identity missing: %#v %v", hit, err)
	}
}

func TestPlannerLedgerHitRequiresEveryCommandAtSameToolchain(t *testing.T) {
	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	put := func(id, command, toolchain string) {
		if err := store.RecordGateLedger(GateLedgerEntry{ID: id, ProjectID: "app", TreeHash: "tree", Command: command, Profile: "full", Toolchain: toolchain}); err != nil {
			t.Fatal(err)
		}
	}
	put("one", "go test", "go1")
	if gateLedgerCommandsHit(store, "app", "tree", []string{"go test", "go vet"}, "full", "go1") {
		t.Fatal("partial multi-command proof passed")
	}
	put("two-wrong", "go vet", "go2")
	if gateLedgerCommandsHit(store, "app", "tree", []string{"go test", "go vet"}, "full", "go1") {
		t.Fatal("mixed-toolchain multi-command proof passed")
	}
	put("two-right", "go vet", "go1")
	if !gateLedgerCommandsHit(store, "app", "tree", []string{"go test", "go vet"}, "full", "go1") {
		t.Fatal("complete same-toolchain multi-command proof missed")
	}
}

func TestUnknownGateExecutorProducesNoReusableToolchainIdentity(t *testing.T) {
	if fingerprint := scheduledPromotionToolchainFingerprint(t.TempDir(), []string{"definitely-not-a-real-gate-executable --check"}); fingerprint != "" {
		t.Fatalf("unknown executor produced reusable fingerprint %q", fingerprint)
	}
	workspace := t.TempDir()
	script := filepath.Join(workspace, "gate-script")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if fingerprint := scheduledPromotionToolchainFingerprint(workspace, []string{"./gate-script"}); fingerprint == "" {
		t.Fatal("safely resolved repo-local script did not bind a toolchain identity")
	}
}

func TestUnknownToolchainGateRunsButDoesNotRecordReusableProof(t *testing.T) {
	exec := &recordingGateExec{outputs: map[string]string{"mystery-gate": "ok"}}
	rt := gateTestRuntime(exec)
	recorded := false
	rt.Toolchain = func(string, []string) string { return "" }
	rt.RecordPass = func(string, string, string, string, time.Duration) { recorded = true }
	result, err := runGateTier(GateTierPolicy{HarvestCommands: []string{"mystery-gate"}, AllowDirtyTree: true}, "", rt)
	if err != nil || result.Outcome != gateOutcomePassed || len(exec.ran) != 1 || recorded {
		t.Fatalf("unknown toolchain gate did not stay non-reusable: result=%#v ran=%#v recorded=%v err=%v", result, exec.ran, recorded, err)
	}
}
