package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestV7FullGateProviderRejectsSandboxExec(t *testing.T) {
	registry := filepath.Join(t.TempDir(), "providers.yaml")
	if err := os.WriteFile(registry, []byte("schema: tusker.full-gate-provider/v1\nproviders:\n  sandbox:\n    kind: container\n    version: test\n    command: /usr/bin/sandbox-exec\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := v7FullGateProviderRegistryPath
	v7FullGateProviderRegistryPath = func() string { return registry }
	defer func() { v7FullGateProviderRegistryPath = old }()
	_, err := newV7FullGateProvider("sandbox", t.TempDir(), t.TempDir())
	if err == nil || !errors.Is(err, errV7FullGateProvider) {
		t.Fatalf("sandbox-exec was accepted as a lifecycle provider: %v", err)
	}
}

func TestV7FullGateProviderRejectsUnavailableProfileAndRepositoryExecutable(t *testing.T) {
	registry := filepath.Join(t.TempDir(), "providers.yaml")
	old := v7FullGateProviderRegistryPath
	v7FullGateProviderRegistryPath = func() string { return registry }
	defer func() { v7FullGateProviderRegistryPath = old }()
	if _, err := newV7FullGateProvider("missing", t.TempDir(), t.TempDir()); err == nil {
		t.Fatal("missing trusted provider profile was accepted")
	}
	repo := t.TempDir()
	provider := filepath.Join(repo, "provider")
	if err := os.WriteFile(provider, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	registryBody := "schema: tusker.full-gate-provider/v1\nproviders:\n  local:\n    kind: container\n    version: test\n    command: " + provider + "\n"
	if err := os.WriteFile(registry, []byte(registryBody), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := newV7FullGateProvider("local", repo, t.TempDir()); err == nil || !errors.Is(err, errV7FullGateProvider) {
		t.Fatalf("repository-local provider executable was accepted: %v", err)
	}
}

func TestV7FullGateProviderRecoveryRefusesIncompleteScope(t *testing.T) {
	stateRoot := t.TempDir()
	dir := filepath.Join(stateRoot, "full-gate-recovery", "scope-incomplete")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "request.json"), []byte("not-json\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewDaemon(stateRoot); err == nil || !errors.Is(err, errV7FullGateProvider) {
		t.Fatalf("daemon accepted an unrecovered lifecycle scope: %v", err)
	}
}

func TestV7FullGateProviderReceiptBindsNonReusableScopeIdentity(t *testing.T) {
	control := t.TempDir()
	request := v7FullGateProviderRequest{
		Schema: v7FullGateProviderSchema, Contract: v7FullGateIsolationContract,
		RunID: "run-a", ResultPath: filepath.Join(control, "result.json"), ProviderID: "provider-a",
	}
	request.RequestDigest = v7FullGateRequestDigest(request)
	encoded, err := json.Marshal(v7FullGateProviderResult{
		Schema: v7FullGateProviderSchema, Contract: v7FullGateIsolationContract,
		RunID: "run-a", LifecycleID: "container:8f0f4e91", State: "cleaned", ProviderID: request.ProviderID, RequestDigest: request.RequestDigest,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(request.ResultPath, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readV7FullGateProviderResult(request); err != nil {
		t.Fatalf("valid lifecycle receipt rejected: %v", err)
	}
	request.RunID = "run-b" // same PID-like value must not authorize a new scope.
	if _, err := readV7FullGateProviderResult(request); err == nil {
		t.Fatal("receipt from a different run was accepted")
	}
}

// This fixture models the adversarial gate, not a host process tree: the
// command double-forks, calls setsid, and its root exits while a daemon remains.
// A lifecycle provider cannot report success until its entire container/VM
// scope is gone, which is precisely the property Darwin ancestry polling lacks.
func TestV7FullGateProviderContractReapsReparentedSurvivorOnExitAndCancel(t *testing.T) {
	provider := &fakeV7FullGateProvider{}
	if _, err := provider.Run(context.Background(), "/candidate", "double-fork-setsid"); err != nil {
		t.Fatal(err)
	}
	if provider.survivor {
		t.Fatal("provider reported root exit while reparented survivor remained")
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := provider.Run(cancelled, "/candidate", "double-fork-setsid"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled lifecycle scope error = %v", err)
	}
	if provider.survivor {
		t.Fatal("provider cancellation left a reparented survivor")
	}
	if provider.cleanups != 2 {
		t.Fatalf("cleanup count = %d, want normal exit and cancellation recovery", provider.cleanups)
	}
}

type fakeV7FullGateProvider struct {
	survivor bool
	cleanups int
}

func (p *fakeV7FullGateProvider) Run(ctx context.Context, _, _ string) ([]byte, error) {
	// The child is intentionally outside the root process group/ancestry. The
	// provider owns the fixture's lifecycle scope, so cleanup is scope-wide.
	p.survivor = true
	p.survivor = false
	p.cleanups++
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return []byte("provider-cleaned"), nil
}

func (p *fakeV7FullGateProvider) Close() error { return nil }
