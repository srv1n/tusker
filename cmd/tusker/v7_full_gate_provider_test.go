package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"debug/macho"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestV7FullGateProviderMachOClosureRejectsMutableLoadPaths(t *testing.T) {
	sealed := v7MachOFixture(t,
		v7MachOPathLoad(t, v7MachOLoadDylinker, 12, "/usr/lib/dyld"),
		v7MachOPathLoad(t, v7MachOLoadDylib, 24, "/usr/lib/libSystem.B.dylib"),
	)
	closure, err := validateV7MachOProvider(sealed)
	if err != nil {
		t.Fatalf("sealed native fixture rejected: %v", err)
	}
	if got := strings.Join(closure, ","); got != "dylib:/usr/lib/libSystem.B.dylib,dylinker:/usr/lib/dyld" {
		t.Fatalf("sealed closure = %q", got)
	}
	tests := []struct {
		name string
		load []byte
	}{
		{name: "relative", load: v7MachOPathLoad(t, v7MachOLoadDylib, 24, "libmutable.dylib")},
		{name: "rpath import", load: v7MachOPathLoad(t, v7MachOLoadDylib, 24, "@rpath/libmutable.dylib")},
		{name: "loader path", load: v7MachOPathLoad(t, v7MachOLoadDylib, 24, "@loader_path/libmutable.dylib")},
		{name: "executable path", load: v7MachOPathLoad(t, v7MachOLoadDylib, 24, "@executable_path/libmutable.dylib")},
		{name: "mutable absolute", load: v7MachOPathLoad(t, v7MachOLoadDylib, 24, "/opt/provider/libmutable.dylib")},
		{name: "rpath command", load: v7MachOPathLoad(t, v7MachORPath, 12, "/opt/provider")},
		{name: "dyld environment", load: v7MachOPathLoad(t, v7MachODynamicEnvironment, 12, "DYLD_LIBRARY_PATH=/opt/provider")},
		{name: "unknown load command", load: v7MachORawLoad(0x7ffffffe)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := validateV7MachOProvider(v7MachOFixture(t, tc.load)); err == nil {
				t.Fatal("mutable Mach-O code-loading path was accepted")
			}
		})
	}
}

func TestV7FullGateProviderDarwinNativeClosureAndACL(t *testing.T) {
	if runtime.GOOS != "darwin" {
		file, err := os.Open(os.Args[0])
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()
		if _, err := v7DarwinDescriptorHasMutationACL(file); err == nil {
			t.Fatal("non-Darwin ACL fallback did not fail closed")
		}
		return
	}
	path := "/usr/bin/true"
	systemFile, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	_, aclSupportErr := v7DarwinDescriptorHasMutationACL(systemFile)
	_ = systemFile.Close()
	if aclSupportErr != nil {
		if _, _, verifyErr := verifyV7TrustedProviderExecutable(path); verifyErr == nil {
			t.Fatal("Darwin build without descriptor ACL support did not fail closed")
		}
		return
	}
	_, identity, err := verifyV7TrustedProviderExecutable(path)
	if err != nil || !v7FullGateDigest(identity) {
		t.Fatalf("sealed Darwin native helper rejected: identity=%q err=%v", identity, err)
	}
	systemRaw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	systemSum := sha256.Sum256(systemRaw)
	if identity == fmt.Sprintf("sha256:%x", systemSum[:]) {
		t.Fatal("provider digest binds only the main Mach-O, not its accepted dependency closure")
	}
	aclPath := filepath.Join(t.TempDir(), "native-helper")
	if err := os.WriteFile(aclPath, systemRaw, 0o700); err != nil {
		t.Fatal(err)
	}
	plain, err := os.Open(aclPath)
	if err != nil {
		t.Fatal(err)
	}
	mutation, aclErr := v7DarwinDescriptorHasMutationACL(plain)
	_ = plain.Close()
	if aclErr != nil {
		t.Fatal(aclErr)
	}
	if mutation {
		t.Fatal("plain native helper unexpectedly has a mutation ACL")
	}
	if output, err := exec.Command("/bin/chmod", "+a", "everyone allow write,delete", aclPath).CombinedOutput(); err != nil {
		t.Skipf("Darwin fixture filesystem does not support extended ACLs: %v: %s", err, output)
	}
	withACL, err := os.Open(aclPath)
	if err != nil {
		t.Fatal(err)
	}
	mutation, aclErr = v7DarwinDescriptorHasMutationACL(withACL)
	_ = withACL.Close()
	if aclErr != nil || !mutation {
		t.Fatalf("descriptor ACL mutation grant accepted: mutation=%t err=%v", mutation, aclErr)
	}
}

func v7MachOFixture(t *testing.T, loads ...[]byte) []byte {
	t.Helper()
	var commands bytes.Buffer
	for _, load := range loads {
		commands.Write(load)
	}
	var raw bytes.Buffer
	header := []uint32{
		uint32(macho.Magic64),
		uint32(macho.CpuArm64),
		0,
		uint32(macho.TypeExec),
		uint32(len(loads)),
		uint32(commands.Len()),
		0,
		0,
	}
	for _, value := range header {
		if err := binary.Write(&raw, binary.LittleEndian, value); err != nil {
			t.Fatal(err)
		}
	}
	raw.Write(commands.Bytes())
	return raw.Bytes()
}

func v7MachOPathLoad(t *testing.T, command, offset uint32, path string) []byte {
	t.Helper()
	size := int(offset) + len(path) + 1
	size = (size + 7) &^ 7
	raw := make([]byte, size)
	binary.LittleEndian.PutUint32(raw[0:4], command)
	binary.LittleEndian.PutUint32(raw[4:8], uint32(size))
	binary.LittleEndian.PutUint32(raw[8:12], offset)
	copy(raw[offset:], path)
	return raw
}

func v7MachORawLoad(command uint32) []byte {
	raw := make([]byte, 8)
	binary.LittleEndian.PutUint32(raw[0:4], command)
	binary.LittleEndian.PutUint32(raw[4:8], uint32(len(raw)))
	return raw
}

func TestV7FullGateProviderRejectsSandboxExec(t *testing.T) {
	registry := filepath.Join(t.TempDir(), "providers.yaml")
	if err := os.WriteFile(registry, []byte("schema: tusker.full-gate-provider/v1\nproviders:\n  sandbox:\n    kind: container\n    version: test\n    command: /usr/bin/sandbox-exec\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := v7FullGateProviderRegistryPath
	v7FullGateProviderRegistryPath = func(string) string { return registry }
	defer func() { v7FullGateProviderRegistryPath = old }()
	_, err := newV7FullGateProvider("sandbox", t.TempDir(), t.TempDir())
	if err == nil || !errors.Is(err, errV7FullGateProvider) {
		t.Fatalf("sandbox-exec was accepted as a lifecycle provider: %v", err)
	}
}

func TestV7FullGateProviderRejectsUnavailableProfileAndRepositoryExecutable(t *testing.T) {
	registry := filepath.Join(t.TempDir(), "providers.yaml")
	old := v7FullGateProviderRegistryPath
	v7FullGateProviderRegistryPath = func(string) string { return registry }
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

func TestV7FullGateProviderRetainsFailedCleanupRecordUntilRecoverySucceeds(t *testing.T) {
	stateRoot := t.TempDir()
	requestPath, request := writeV7ProviderRecoveryRequest(t, stateRoot)
	old := runV7FullGateProviderCleanup
	defer func() { runV7FullGateProviderCleanup = old }()
	runV7FullGateProviderCleanup = func(context.Context, string, *v7FullGateProviderScope, []string, io.Writer) error {
		return errors.New("fixture cleanup unavailable")
	}
	provider := &v7ExternalFullGateProvider{path: request.ProviderPath, executableIdentity: request.ExecutableID}
	if _, err := provider.cleanup(&v7FullGateProviderScope{request: request, requestPath: requestPath}); err == nil {
		t.Fatal("fixture cleanup unexpectedly succeeded")
	}
	if _, err := os.Stat(requestPath); err != nil {
		t.Fatalf("failed cleanup erased recovery record: %v", err)
	}
	if err := os.WriteFile(request.ResultPath, []byte("{malformed"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewDaemon(stateRoot); err == nil || !errors.Is(err, errV7FullGateProvider) {
		t.Fatalf("daemon accepted failed cleanup scope: %v", err)
	}
	runV7FullGateProviderCleanup = func(_ context.Context, _ string, scope *v7FullGateProviderScope, _ []string, _ io.Writer) error {
		result := v7FullGateProviderResult{Schema: v7FullGateProviderSchema, Contract: v7FullGateIsolationContract, RunID: request.RunID, LifecycleID: "fixture-scope", State: "cleaned", ProviderID: request.ProviderID, RequestDigest: request.RequestDigest, RuntimeDigest: request.RuntimeDigest, PolicyDigest: request.PolicyDigest, AttestationDigest: request.AttestationDigest, Capabilities: request.RequiredCapabilities, ImplementationID: request.ImplementationID, CapabilitySchema: request.CapabilitySchema, CandidateReadOnlyMeasured: true, NetworkMode: "none", ControlEnvAbsent: true, ControlMountsAbsent: true, ImageOrVMID: request.ExpectedImageOrVMID}
		result.Outcome = v7FullGateOutcomeCanceled
		v7SealFullGateProviderResult(request, &result)
		return scope.persistResult(result)
	}
	if daemon, err := NewDaemon(stateRoot); err != nil {
		t.Fatalf("certified cleanup did not converge at daemon startup: %v", err)
	} else {
		_ = daemon.Close()
	}
	if _, err := os.Stat(filepath.Dir(requestPath)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("reconciled cleanup scope remained: %v", err)
	}
	journalPath := v7FullGateProviderOutcomeJournalPath(stateRoot, request.DepartureID, request.RequestDigest)
	journal, err := readV7FullGateProviderOutcomeJournal(journalPath)
	if err != nil {
		t.Fatalf("read bounded actionable cleanup outcome: %v", err)
	}
	if !journal.Reconciled || journal.ScopePath != "" || journal.Action == "" {
		t.Fatalf("cleanup outcome was not retained as bounded actionable evidence: %#v", journal)
	}
	if daemon, err := NewDaemon(stateRoot); err != nil {
		t.Fatalf("second startup did not converge over actionable outcome: %v", err)
	} else {
		_ = daemon.Close()
	}
}

func TestV7FullGateProviderCloseUsesActiveRequestPath(t *testing.T) {
	stateRoot := t.TempDir()
	requestPath, request := writeV7ProviderRecoveryRequest(t, stateRoot)
	old := runV7FullGateProviderCleanup
	defer func() { runV7FullGateProviderCleanup = old }()
	seen, cleanups := "", 0
	runV7FullGateProviderCleanup = func(_ context.Context, _ string, scope *v7FullGateProviderScope, _ []string, _ io.Writer) error {
		cleanups++
		seen = "/dev/fd/3"
		result := v7FullGateProviderResult{Schema: v7FullGateProviderSchema, Contract: v7FullGateIsolationContract, RunID: request.RunID, LifecycleID: "fixture-scope", State: "cleaned", ProviderID: request.ProviderID, RequestDigest: request.RequestDigest, RuntimeDigest: request.RuntimeDigest, PolicyDigest: request.PolicyDigest, AttestationDigest: request.AttestationDigest, Capabilities: request.RequiredCapabilities, ImplementationID: request.ImplementationID, CapabilitySchema: request.CapabilitySchema, CandidateReadOnlyMeasured: true, NetworkMode: "none", ControlEnvAbsent: true, ControlMountsAbsent: true, ImageOrVMID: request.ExpectedImageOrVMID}
		result.Outcome = v7FullGateOutcomeCanceled
		v7SealFullGateProviderResult(request, &result)
		return scope.persistResult(result)
	}
	provider := &v7ExternalFullGateProvider{path: request.ProviderPath, executableIdentity: request.ExecutableID, active: &v7FullGateProviderScope{request: request, requestPath: requestPath}}
	if err := provider.Close(); err != nil {
		t.Fatal(err)
	}
	if err := provider.Close(); err != nil {
		t.Fatal(err)
	}
	if seen != "/dev/fd/3" {
		t.Fatalf("Close cleanup transport = %q, want inherited request descriptor", seen)
	}
	if cleanups != 1 {
		t.Fatalf("Close issued %d cleanup calls for one scope, want 1", cleanups)
	}
	if _, err := os.Stat(requestPath); err != nil {
		t.Fatalf("Close removed active scope before Run can observe cleanup: %v", err)
	}
}

func TestV7FullGateProviderCloseWaitsForPublishedWrapperBeforeCleanup(t *testing.T) {
	stateRoot := t.TempDir()
	requestPath, request := writeV7ProviderRecoveryRequest(t, stateRoot)
	old := runV7FullGateProviderCleanup
	defer func() { runV7FullGateProviderCleanup = old }()
	cleanups := 0
	runV7FullGateProviderCleanup = func(_ context.Context, _ string, scope *v7FullGateProviderScope, _ []string, _ io.Writer) error {
		cleanups++
		result := v7FullGateProviderResult{Schema: v7FullGateProviderSchema, Contract: v7FullGateIsolationContract, RunID: request.RunID, LifecycleID: "fixture-scope", State: "cleaned", ProviderID: request.ProviderID, RequestDigest: request.RequestDigest, RuntimeDigest: request.RuntimeDigest, PolicyDigest: request.PolicyDigest, AttestationDigest: request.AttestationDigest, Capabilities: request.RequiredCapabilities, ImplementationID: request.ImplementationID, CapabilitySchema: request.CapabilitySchema, CandidateReadOnlyMeasured: true, NetworkMode: "none", ControlEnvAbsent: true, ControlMountsAbsent: true, ImageOrVMID: request.ExpectedImageOrVMID}
		result.Outcome = v7FullGateOutcomeCanceled
		v7SealFullGateProviderResult(request, &result)
		return scope.persistResult(result)
	}
	cancelled := make(chan struct{})
	scope := &v7FullGateProviderScope{request: request, requestPath: requestPath, wrapperDone: make(chan struct{}), runCancel: func() { close(cancelled) }}
	provider := &v7ExternalFullGateProvider{path: request.ProviderPath, executableIdentity: request.ExecutableID, active: scope}
	done := make(chan error, 1)
	go func() { done <- provider.Close() }()
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("Close did not cancel the published wrapper")
	}
	select {
	case err := <-done:
		t.Fatalf("Close returned before wrapper exit: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	scope.finishWrapper(nil)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Close after wrapper exit: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Close did not finish after wrapper exit")
	}
	if cleanups != 1 {
		t.Fatalf("cleanup count = %d, want 1", cleanups)
	}
}

func writeV7ProviderRecoveryRequest(t *testing.T, stateRoot string) (string, v7FullGateProviderRequest) {
	t.Helper()
	providerPath, err := exec.LookPath("true")
	if err != nil {
		t.Fatal(err)
	}
	rawProvider, err := os.ReadFile(providerPath)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(rawProvider)
	executableID := fmt.Sprintf("sha256:%x", sum[:])
	if _, verified, verifyErr := verifyV7TrustedProviderExecutable(providerPath); verifyErr == nil {
		executableID = verified
	}
	dir := filepath.Join(stateRoot, "full-gate-recovery", "scope-fixture")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	request := v7FullGateProviderRequest{Schema: v7FullGateProviderSchema, Contract: v7FullGateIsolationContract, RunID: "fixture-run", Workspace: stateRoot, Command: "fixture", ResultPath: filepath.Join(dir, "result.json"), ProviderKind: "container", ProviderID: "fixture-provider", ProviderPath: providerPath, ExecutableID: executableID, CandidateReadOnly: true, NetworkDenied: true, ControlEnvDenied: true, RuntimeDigest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", PolicyDigest: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", AttestationDigest: "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc", RequiredCapabilities: []string{"candidate_read_only", "network_denied", "control_env_denied"}, ImplementationID: v7KnownFullGateProvider, CapabilitySchema: v7FullGateCapabilitySchema, ExpectedImageOrVMID: "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"}
	request.RequestDigest = v7FullGateRequestDigest(request)
	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	requestPath := filepath.Join(dir, "request.json")
	if err := os.WriteFile(requestPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return requestPath, request
}

func TestV7FullGateProviderReceiptBindsNonReusableScopeIdentity(t *testing.T) {
	control := t.TempDir()
	request := v7FullGateProviderRequest{
		Schema: v7FullGateProviderSchema, Contract: v7FullGateIsolationContract,
		RunID: "run-a", ResultPath: filepath.Join(control, "result.json"), ProviderID: "provider-a",
		RuntimeDigest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", PolicyDigest: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", AttestationDigest: "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
		RequiredCapabilities: []string{"candidate_read_only", "network_denied", "control_env_denied"},
		ImplementationID:     v7KnownFullGateProvider, CapabilitySchema: v7FullGateCapabilitySchema, ExpectedImageOrVMID: "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
	}
	request.RequestDigest = v7FullGateRequestDigest(request)
	encoded, err := json.Marshal(v7FullGateProviderResult{
		Schema: v7FullGateProviderSchema, Contract: v7FullGateIsolationContract,
		RunID: "run-a", LifecycleID: "container:8f0f4e91", State: "cleaned", ProviderID: request.ProviderID, RequestDigest: request.RequestDigest, RuntimeDigest: request.RuntimeDigest, PolicyDigest: request.PolicyDigest, AttestationDigest: request.AttestationDigest, Capabilities: request.RequiredCapabilities, ImplementationID: request.ImplementationID, CapabilitySchema: request.CapabilitySchema, CandidateReadOnlyMeasured: true, NetworkMode: "none", ControlEnvAbsent: true, ControlMountsAbsent: true, ImageOrVMID: request.ExpectedImageOrVMID,
	})
	if err != nil {
		t.Fatal(err)
	}
	var result v7FullGateProviderResult
	if err := json.Unmarshal(encoded, &result); err != nil {
		t.Fatal(err)
	}
	result.Outcome = v7FullGateOutcomePassed
	v7SealFullGateProviderResult(request, &result)
	encoded, err = json.Marshal(result)
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

func TestV7FullGateProviderTypedOutcomesGateLedgerCertification(t *testing.T) {
	for _, outcome := range []v7FullGateOutcome{v7FullGateOutcomePassed, v7FullGateOutcomeFailed, v7FullGateOutcomeProvider, v7FullGateOutcomeCanceled, v7FullGateOutcomeTimedOut} {
		t.Run(string(outcome), func(t *testing.T) {
			control := t.TempDir()
			request := v7FullGateProviderRequest{
				Schema: v7FullGateProviderSchema, Contract: v7FullGateIsolationContract, RunID: "run-" + string(outcome), Workspace: control, Command: "go test ./...",
				ProjectID: "project", DepartureID: "departure", CandidateDigest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Profile: "full", ProviderProfile: "fixture", Toolchain: "toolchain",
				ResultPath: filepath.Join(control, "result.json"), ProviderID: "sha256:abababababababababababababababababababababababababababababababab", ExecutableID: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", RuntimeDigest: "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc", ClientDigest: "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd", PolicyDigest: "sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee", AttestationDigest: "sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
				RequiredCapabilities: []string{"candidate_read_only", "network_denied", "control_env_denied"}, ImplementationID: v7KnownFullGateProvider, CapabilitySchema: v7FullGateCapabilitySchema, ExpectedImageOrVMID: "sha256:1111111111111111111111111111111111111111111111111111111111111111",
				MaxCommandBytes: v7FullGateCommandMaxBytes, MaxOutputBytes: 256, MaxRuntimeMS: 1_000, MaxArtifactBytes: 1_024,
			}
			request.RequestDigest = v7FullGateRequestDigest(request)
			result := v7FullGateProviderResult{Schema: v7FullGateProviderSchema, Contract: v7FullGateIsolationContract, RunID: request.RunID, LifecycleID: "scope-" + string(outcome), State: "cleaned", Outcome: outcome, Output: "bounded", ProviderID: request.ProviderID, RequestDigest: request.RequestDigest, RuntimeDigest: request.RuntimeDigest, PolicyDigest: request.PolicyDigest, AttestationDigest: request.AttestationDigest, Capabilities: request.RequiredCapabilities, ImplementationID: request.ImplementationID, CapabilitySchema: request.CapabilitySchema, CandidateReadOnlyMeasured: true, NetworkMode: "none", ControlEnvAbsent: true, ControlMountsAbsent: true, ImageOrVMID: request.ExpectedImageOrVMID, RuntimeMS: 12, ArtifactBytes: 16}
			v7SealFullGateProviderResult(request, &result)
			raw, err := json.Marshal(result)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(request.ResultPath, raw, 0o600); err != nil {
				t.Fatal(err)
			}
			got, err := readV7FullGateProviderResult(request)
			if err != nil {
				t.Fatalf("read typed result: %v", err)
			}
			provider := &v7ExternalFullGateProvider{executableIdentity: request.ExecutableID, clientDigest: request.ClientDigest, runtimeDigest: request.RuntimeDigest, policyDigest: request.PolicyDigest, attestationDigest: request.AttestationDigest, imageOrVMID: request.ExpectedImageOrVMID, profile: request.ProviderProfile}
			receipt, err := provider.recordReceipt(nil, request, got)
			if err != nil {
				t.Fatal(err)
			}
			certified := v7CertifiedGateProviderReceipt(&receipt)
			if certified != (outcome == v7FullGateOutcomePassed) {
				t.Fatalf("outcome %q certified=%t", outcome, certified)
			}
		})
	}
}

func TestV7FullGateProviderBindingRejectsProfileConfusion(t *testing.T) {
	stateRoot := t.TempDir()
	state, err := openV7FullGateStateRoot(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	artifact := filepath.Join(stateRoot, "artifacts", "promotion-gates", "binding.log")
	provider := &v7ExternalFullGateProvider{profile: "provider-profile", stateRoot: stateRoot, state: state}
	if err := provider.BindFullGateProvider(v7FullGateProviderBinding{ProjectID: "project", DepartureID: "departure", CandidateDigest: "candidate", GateProfile: "gate-profile", ProviderProfile: "other-provider", Toolchain: "toolchain", ArtifactRef: artifact}); err == nil {
		t.Fatal("provider accepted a gate/provider profile confusion")
	}
	if err := provider.BindFullGateProvider(v7FullGateProviderBinding{ProjectID: "project", DepartureID: "departure", CandidateDigest: "candidate", GateProfile: "gate-profile", ProviderProfile: "provider-profile", Toolchain: "toolchain", ArtifactRef: artifact}); err != nil {
		t.Fatalf("provider rejected exact gate/provider binding: %v", err)
	}
}

func TestV7FullGateProviderTimeoutFixture(t *testing.T) {
	oldLimit := v7FullGateRuntimeLimit
	v7FullGateRuntimeLimit = 5 * time.Millisecond
	defer func() { v7FullGateRuntimeLimit = oldLimit }()
	ctx, cancel := v7FullGateProviderDeadline(context.Background(), v7FullGateRuntimeLimit)
	defer cancel()
	select {
	case <-ctx.Done():
		if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
			t.Fatalf("deadline outcome = %v", ctx.Err())
		}
	case <-time.After(time.Second):
		t.Fatal("fake provider deadline did not fire")
	}
}

func TestV7FullGateProviderCleanupBoundsInheritedPipeDrain(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("descriptor transport is Darwin-only")
	}
	stateRoot := t.TempDir()
	requestPath, request := writeV7ProviderRecoveryRequest(t, stateRoot)
	if err := os.WriteFile(request.ResultPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	scope := &v7FullGateProviderScope{request: request, requestPath: requestPath}
	state, err := openV7FullGateStateRoot(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	if err := scope.bindState(state); err != nil {
		t.Fatal(err)
	}
	previous := v7FullGateProviderPipeWaitDelay
	v7FullGateProviderPipeWaitDelay = 25 * time.Millisecond
	defer func() { v7FullGateProviderPipeWaitDelay = previous }()
	var output bytes.Buffer
	started := time.Now()
	err = launchV7FullGateProviderCleanup(context.Background(), os.Args[0], scope, []string{"TUSKER_V7_PIPE_HOLDER=parent"}, &output)
	elapsed := time.Since(started)
	if !errors.Is(err, exec.ErrWaitDelay) {
		t.Fatalf("cleanup with descendant-held pipes = %v, want exec.ErrWaitDelay", err)
	}
	if elapsed > time.Second {
		t.Fatalf("cleanup pipe drain exceeded bound: %s", elapsed)
	}
}

func TestV7FullGateProviderReservationIsWrittenBeforeLaunch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "scope", "request.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := writeV7FullGateReservation(path, []byte("reserved\n")); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != "reserved\n" {
		t.Fatalf("durable reservation = %q, %v", got, err)
	}
	if err := writeV7FullGateReservation(path, []byte("replacement")); !errors.Is(err, os.ErrExist) {
		t.Fatalf("reservation replacement error = %v, want os.ErrExist", err)
	}
}

func TestV7FullGateProviderDarwinDescriptorTransportUsesOnlyRequestAndResult(t *testing.T) {
	stateRoot := t.TempDir()
	state, err := openV7FullGateStateRoot(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	if _, err := state.ensureDir(filepath.Join("full-gate-recovery", "scope-fd-transport"), 0o700); err != nil {
		t.Fatal(err)
	}
	scopeRel := filepath.Join("full-gate-recovery", "scope-fd-transport")
	requestRel := filepath.Join(scopeRel, "request.json")
	requestPath, err := state.absolute(requestRel)
	if err != nil {
		t.Fatal(err)
	}
	request := v7FullGateProviderRequest{Schema: v7FullGateProviderSchema, Contract: v7FullGateIsolationContract, RunID: "fd-transport", ResultPath: "/dev/fd/4"}
	request.RequestDigest = v7FullGateRequestDigest(request)
	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	if err := state.writeJSON(requestRel, raw, false); err != nil {
		t.Fatal(err)
	}
	if err := state.writeDurable(filepath.Join(scopeRel, "result.json"), nil, 0o600, false); err != nil {
		t.Fatal(err)
	}
	scope := &v7FullGateProviderScope{request: request, requestPath: requestPath, state: state, scopeRel: scopeRel, requestRel: requestRel, resultRel: filepath.Join(scopeRel, "result.json")}
	preparedRequest := scope.request
	scope.request.RunID = "different-in-memory-request"
	requestFile, resultFile, scopeDir, err := scope.openTransport()
	if err == nil {
		_ = requestFile.Close()
		_ = resultFile.Close()
		_ = scopeDir.Close()
		t.Fatal("descriptor transport accepted a request different from the prepared request")
	}
	if !strings.Contains(err.Error(), "does not exactly bind") {
		t.Fatalf("different prepared request error = %v", err)
	}
	scope.request = preparedRequest
	if runtime.GOOS != "darwin" {
		_, closeTransport, err := v7FullGateProviderCommand(context.Background(), os.Args[0], "--tusker-full-gate-run", scope)
		if err == nil {
			closeTransport()
			t.Fatal("unsupported host silently accepted pathname-based descriptor fallback")
		}
		if !strings.Contains(err.Error(), "refusing pathname-based fallback") {
			t.Fatalf("unsupported-host transport error = %v", err)
		}
		return
	}
	for _, operation := range []string{"--tusker-full-gate-run", "--tusker-full-gate-cleanup"} {
		cmd, closeTransport, err := v7FullGateProviderCommand(context.Background(), os.Args[0], operation, scope)
		if err != nil {
			t.Fatalf("construct Darwin inherited-descriptor %s launch: %v", operation, err)
		}
		cmd.Env = append(v7FullGateProviderEnv(), "TUSKER_V7_FD_TRANSPORT_HELPER=1")
		output, runErr := cmd.CombinedOutput()
		closeTransport()
		if runErr != nil {
			t.Fatalf("compiled descriptor fixture %s: %v: %s", operation, runErr, output)
		}
		got, readErr := state.readRegular(scope.resultRel, 1024, false)
		want := "darwin descriptor transport " + operation + "\n"
		if readErr != nil || string(got) != want {
			t.Fatalf("/dev/fd/4 result for %s = %q, %v", operation, got, readErr)
		}
	}
	if err := state.remove(scope.resultRel); err != nil {
		t.Fatal(err)
	}
	if err := state.writeDurable(scope.resultRel, []byte("{}"), 0o600, false); err != nil {
		t.Fatal(err)
	}
	if _, err := scope.readResult(); err == nil || !strings.Contains(err.Error(), "result reservation was replaced after launch") {
		t.Fatalf("replacement result reservation error = %v", err)
	}
}

func TestV7FullGateProviderRejectsVerifyToExecReplacementBeforeLaunch(t *testing.T) {
	stateRoot := t.TempDir()
	binary := filepath.Join(t.TempDir(), "provider")
	original, err := os.ReadFile(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(binary, original, 0o700); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(t.TempDir(), "wrong-provider-ran")
	replacement := binary + ".replacement"
	if err := os.WriteFile(replacement, []byte("#!/bin/sh\nprintf wrong > "+yamlQuoteForShellTest(sentinel)+"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	provider := newExternalV7ProviderFixture(t, binary, stateRoot)
	oldVerified, oldLaunch := v7FullGateAfterExecutableDigestVerified, v7FullGateBeforeProviderLaunch
	defer func() {
		v7FullGateAfterExecutableDigestVerified = oldVerified
		v7FullGateBeforeProviderLaunch = oldLaunch
	}()
	replaced, launches := false, 0
	v7FullGateAfterExecutableDigestVerified = func(path string) error {
		if path == binary && !replaced {
			replaced = true
			return os.Rename(replacement, binary)
		}
		return nil
	}
	v7FullGateBeforeProviderLaunch = func(string, string) error {
		launches++
		return nil
	}
	invocation, err := provider.Run(context.Background(), t.TempDir(), "fixture")
	if err == nil || invocation.Outcome != v7FullGateOutcomeProvider || !replaced {
		t.Fatalf("replacement attempt = %#v, %v, replaced=%t", invocation, err, replaced)
	}
	if launches != 0 {
		t.Fatalf("replacement reached launch seam %d times", launches)
	}
	if _, statErr := os.Stat(sentinel); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("wrong replacement binary ran: %v", statErr)
	}
	if !strings.Contains(err.Error(), v7ImmutableProviderSetupPrerequisite) {
		t.Fatalf("mutable executable rejection hid setup prerequisite: %v", err)
	}
}

func TestV7FullGateProviderRootAndScopeSwapCannotTouchOutsideState(t *testing.T) {
	parent := t.TempDir()
	stateRoot := filepath.Join(parent, "state")
	if err := os.Mkdir(stateRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(parent, "outside")
	if err := os.Mkdir(outside, 0o700); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(outside, "sentinel")
	if err := os.WriteFile(sentinel, []byte("outside stays exact\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	provider := newExternalV7ProviderFixture(t, os.Args[0], stateRoot)
	request, requestPath, err := provider.newRequest(t.TempDir(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	scope := &v7FullGateProviderScope{request: request, requestPath: requestPath}
	if err := scope.bindState(provider.state); err != nil {
		t.Fatal(err)
	}
	heldRoot := filepath.Join(parent, "held-state")
	if err := os.Rename(stateRoot, heldRoot); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, stateRoot); err != nil {
		t.Fatal(err)
	}
	actualScope := filepath.Join(heldRoot, scope.scopeRel)
	if err := os.RemoveAll(actualScope); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, actualScope); err != nil {
		t.Fatal(err)
	}
	result := v7CertifiedProviderResultFixture(request, v7FullGateOutcomeProvider)
	receipt, err := provider.recordReceipt(scope, request, result)
	if err != nil {
		t.Fatalf("journal through retained state handle: %v", err)
	}
	if err := provider.FinalizeFullGateProviderOutcome(receipt); err == nil || !strings.Contains(err.Error(), "cannot retire before") {
		t.Fatalf("unbound artifact unexpectedly allowed retirement: %v", err)
	}
	if err := writeV7DurablePromotionArtifactAtRoot(provider.state, request.ArtifactRef, []byte("rooted evidence\n")); err != nil {
		t.Fatal(err)
	}
	if err := provider.BindFullGateProviderArtifact(request.ArtifactRef, []GateProviderReceipt{receipt}); err != nil {
		t.Fatal(err)
	}
	if err := provider.FinalizeFullGateProviderOutcome(receipt); err != nil {
		t.Fatalf("retire swapped scope through retained state handle: %v", err)
	}
	if got, err := os.ReadFile(sentinel); err != nil || string(got) != "outside stays exact\n" {
		t.Fatalf("outside sentinel = %q, %v", got, err)
	}
	entries, err := os.ReadDir(outside)
	if err != nil || len(entries) != 1 || entries[0].Name() != "sentinel" {
		t.Fatalf("outside tree was mutated: %#v, %v", entries, err)
	}
	if _, err := os.Lstat(actualScope); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("swapped scope link remained: %v", err)
	}
	if err := provider.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestV7FullGateProviderRetiresOnlyAfterOutcomeAcknowledgement(t *testing.T) {
	requestPath, request := writeV7ProviderRecoveryRequest(t, t.TempDir())
	result := v7FullGateProviderResult{Schema: v7FullGateProviderSchema, Contract: v7FullGateIsolationContract, RunID: request.RunID, LifecycleID: "ack-scope", State: "cleaned", Outcome: v7FullGateOutcomePassed, ProviderID: request.ProviderID, RequestDigest: request.RequestDigest, RuntimeDigest: request.RuntimeDigest, PolicyDigest: request.PolicyDigest, AttestationDigest: request.AttestationDigest, Capabilities: request.RequiredCapabilities, ImplementationID: request.ImplementationID, CapabilitySchema: request.CapabilitySchema, CandidateReadOnlyMeasured: true, NetworkMode: "none", ControlEnvAbsent: true, ControlMountsAbsent: true, ImageOrVMID: request.ExpectedImageOrVMID}
	v7SealFullGateProviderResult(request, &result)
	provider := &v7ExternalFullGateProvider{stateRoot: filepath.Dir(filepath.Dir(filepath.Dir(requestPath)))}
	scope := &v7FullGateProviderScope{request: request, requestPath: requestPath}
	receipt, err := provider.recordReceipt(scope, request, result)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(requestPath); err != nil {
		t.Fatalf("scope vanished before acknowledgement: %v", err)
	}
	if err := provider.FinalizeFullGateProviderOutcome(receipt); err != nil {
		t.Fatalf("acknowledge certified outcome: %v", err)
	}
	if _, err := os.Stat(filepath.Dir(requestPath)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("acknowledged scope remained: %v", err)
	}
}

func TestV7FullGateProviderOutcomeJournalRejectsTampering(t *testing.T) {
	t.Run("full receipt", func(t *testing.T) {
		stateRoot := t.TempDir()
		scope, request, result, receipt := writeV7ProviderOutcomeFixture(t, stateRoot, "departure-receipt", v7FullGateOutcomePassed)
		if err := persistV7FullGateProviderOutcomeJournal(stateRoot, scope, request, result, receipt); err != nil {
			t.Fatal(err)
		}
		path := v7FullGateProviderOutcomeJournalPath(stateRoot, request.DepartureID, request.RequestDigest)
		journal, err := readV7FullGateProviderOutcomeJournal(path)
		if err != nil {
			t.Fatal(err)
		}
		journal.Receipt.Toolchain = "tampered-toolchain"
		journal.JournalDigest = v7FullGateProviderOutcomeJournalDigest(journal)
		writeV7RawJournalFixture(t, path, journal)
		if _, err := readV7FullGateProviderOutcomeJournal(path); err == nil {
			t.Fatal("journal accepted a receipt that no longer reconstructs exactly from request/result")
		}
	})

	t.Run("scope escape", func(t *testing.T) {
		stateRoot := t.TempDir()
		scope, request, result, receipt := writeV7ProviderOutcomeFixture(t, stateRoot, "departure-escape", v7FullGateOutcomeProvider)
		if err := persistV7FullGateProviderOutcomeJournal(stateRoot, scope, request, result, receipt); err != nil {
			t.Fatal(err)
		}
		path := v7FullGateProviderOutcomeJournalPath(stateRoot, request.DepartureID, request.RequestDigest)
		journal, err := readV7FullGateProviderOutcomeJournal(path)
		if err != nil {
			t.Fatal(err)
		}
		journal.ScopePath = filepath.Join(t.TempDir(), "scope-escape")
		journal.JournalDigest = v7FullGateProviderOutcomeJournalDigest(journal)
		writeV7RawJournalFixture(t, path, journal)
		if err := recoverV7FullGateProviderScopes(stateRoot, nil); err == nil || !strings.Contains(err.Error(), "scope escapes") {
			t.Fatalf("scope escape recovery error = %v", err)
		}
	})

	t.Run("canonical filename", func(t *testing.T) {
		stateRoot := t.TempDir()
		scope, request, result, receipt := writeV7ProviderOutcomeFixture(t, stateRoot, "departure-name", v7FullGateOutcomeProvider)
		if err := persistV7FullGateProviderOutcomeJournal(stateRoot, scope, request, result, receipt); err != nil {
			t.Fatal(err)
		}
		path := v7FullGateProviderOutcomeJournalPath(stateRoot, request.DepartureID, request.RequestDigest)
		other := filepath.Join(filepath.Dir(path), "not-canonical.json")
		if err := os.Rename(path, other); err != nil {
			t.Fatal(err)
		}
		if err := recoverV7FullGateProviderScopes(stateRoot, nil); err == nil || !strings.Contains(err.Error(), "non-canonical") {
			t.Fatalf("non-canonical journal recovery error = %v", err)
		}
	})

	t.Run("duplicate request digest", func(t *testing.T) {
		stateRoot := t.TempDir()
		scope, request, result, receipt := writeV7ProviderOutcomeFixture(t, stateRoot, "departure-duplicate", v7FullGateOutcomeProvider)
		if err := persistV7FullGateProviderOutcomeJournal(stateRoot, scope, request, result, receipt); err != nil {
			t.Fatal(err)
		}
		path := v7FullGateProviderOutcomeJournalPath(stateRoot, request.DepartureID, request.RequestDigest)
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(filepath.Dir(path), "zz-duplicate.json"), raw, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := recoverV7FullGateProviderScopes(stateRoot, nil); err == nil || !strings.Contains(err.Error(), "duplicate") {
			t.Fatalf("duplicate journal recovery error = %v", err)
		}
	})

	t.Run("atomic write temporary", func(t *testing.T) {
		stateRoot := t.TempDir()
		scope, request, result, receipt := writeV7ProviderOutcomeFixture(t, stateRoot, "departure-temporary", v7FullGateOutcomeProvider)
		if err := persistV7FullGateProviderOutcomeJournal(stateRoot, scope, request, result, receipt); err != nil {
			t.Fatal(err)
		}
		path := v7FullGateProviderOutcomeJournalPath(stateRoot, request.DepartureID, request.RequestDigest)
		journal, err := readV7FullGateProviderOutcomeJournal(path)
		if err != nil {
			t.Fatal(err)
		}
		journal.Reconciled = true
		journal.Action = "fixture actionable outcome"
		journal.ScopePath = ""
		journal.JournalDigest = v7FullGateProviderOutcomeJournalDigest(journal)
		writeV7RawJournalFixture(t, path, journal)
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		temporary := path + ".tmp-fixture"
		if err := os.WriteFile(temporary, raw, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.RemoveAll(filepath.Dir(scope.requestPath)); err != nil {
			t.Fatal(err)
		}
		if err := recoverV7FullGateProviderScopes(stateRoot, nil); err != nil {
			t.Fatalf("recover linked journal temporary: %v", err)
		}
		if _, err := os.Stat(temporary); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("journal temporary remained: %v", err)
		}
	})
}

func TestV7FullGateProviderDurabilityOrdering(t *testing.T) {
	stateRoot := t.TempDir()
	rawProvider, err := os.ReadFile(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	providerSum := sha256.Sum256(rawProvider)
	digest := func(ch byte) string { return "sha256:" + strings.Repeat(string(ch), 64) }
	provider := &v7ExternalFullGateProvider{
		path: os.Args[0], kind: "container", identity: digest('a'), executableIdentity: fmt.Sprintf("sha256:%x", providerSum[:]),
		runtimeDigest: digest('b'), clientDigest: digest('c'), policyDigest: digest('d'), attestationDigest: digest('e'),
		capabilities: []string{"candidate_read_only", "network_denied", "control_env_denied"}, implementationID: v7KnownFullGateProvider,
		capabilitySchema: v7FullGateCapabilitySchema, imageOrVMID: digest('f'), profile: "fixture", stateRoot: stateRoot,
		recoveryRoot: filepath.Join(stateRoot, "full-gate-recovery"),
		binding:      v7FullGateProviderBinding{ProjectID: "project", DepartureID: "departure-order", CandidateDigest: digest('1'), GateProfile: "full", ProviderProfile: "fixture", Toolchain: "toolchain", ArtifactRef: filepath.Join(stateRoot, "artifacts", "promotion-gates", "ordering.log")},
	}
	var stages []string
	previous := v7FullGateDurabilityHook
	v7FullGateDurabilityHook = func(stage string) error {
		stages = append(stages, stage)
		return nil
	}
	defer func() { v7FullGateDurabilityHook = previous }()
	request, requestPath, err := provider.newRequest(t.TempDir(), "fixture-command")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"recovery_root_created_synced", "preparation_root_created_synced", "preparation_dentry_synced", "result_reservation_synced", "reservation_synced", "scope_published_synced"}
	if strings.Join(stages, ",") != strings.Join(want, ",") {
		t.Fatalf("request durability order = %v, want %v", stages, want)
	}
	stages = nil
	result := v7CertifiedProviderResultFixture(request, v7FullGateOutcomeProvider)
	scope := &v7FullGateProviderScope{request: request, requestPath: requestPath}
	receipt := v7GateProviderReceiptForResult(request, result)
	if err := persistV7FullGateProviderOutcomeJournal(stateRoot, scope, request, result, receipt); err != nil {
		t.Fatal(err)
	}
	journal, err := readV7FullGateProviderOutcomeJournal(v7FullGateProviderOutcomeJournalPath(stateRoot, request.DepartureID, request.RequestDigest))
	if err != nil || journal.ArtifactRef != request.ArtifactRef || journal.ArtifactDigest != "" {
		t.Fatalf("journal did not retain the preallocated unbound artifact target: %#v, %v", journal, err)
	}
	want = []string{"outcome_journal_root_created_synced", "outcome_journal_synced"}
	if strings.Join(stages, ",") != strings.Join(want, ",") {
		t.Fatalf("journal durability order = %v, want %v", stages, want)
	}
}

func TestV7FullGateProviderPreparationCrashSeamsConverge(t *testing.T) {
	stages := []struct {
		name      string
		stage     string
		published bool
	}{
		{name: "mkdir before request", stage: "preparation_dentry_synced"},
		{name: "result before request", stage: "result_reservation_synced"},
		{name: "request before publish", stage: "reservation_synced"},
		{name: "publish before wrapper", stage: "scope_published_synced", published: true},
	}
	for _, tc := range stages {
		t.Run(tc.name, func(t *testing.T) {
			stateRoot := t.TempDir()
			store, run := openV7ProviderRecoveryRun(t, stateRoot, "preparation-"+tc.stage)
			defer store.Close()
			providerPath, lookupErr := exec.LookPath("true")
			if lookupErr != nil {
				t.Fatal(lookupErr)
			}
			provider := newExternalV7ProviderFixture(t, providerPath, stateRoot)
			provider.binding.DepartureID = run.ID
			previousDurability := v7FullGateDurabilityHook
			v7FullGateDurabilityHook = func(stage string) error {
				if stage == tc.stage {
					return errors.New("fixture crash at " + stage)
				}
				return nil
			}
			_, _, err := provider.newRequest(t.TempDir(), "fixture")
			v7FullGateDurabilityHook = previousDurability
			if err == nil {
				t.Fatal("injected preparation crash unexpectedly returned launch authority")
			}
			preparations, readErr := os.ReadDir(filepath.Join(stateRoot, "full-gate-preparing"))
			if readErr != nil {
				t.Fatal(readErr)
			}
			recovery, readErr := os.ReadDir(filepath.Join(stateRoot, "full-gate-recovery"))
			if readErr != nil {
				t.Fatal(readErr)
			}
			if tc.published {
				if len(preparations) != 0 || len(recovery) != 1 {
					t.Fatalf("published crash directories: preparing=%d recovery=%d", len(preparations), len(recovery))
				}
			} else if len(preparations) != 1 || len(recovery) != 0 {
				t.Fatalf("unpublished crash directories: preparing=%d recovery=%d", len(preparations), len(recovery))
			}
			previousCleanup := runV7FullGateProviderCleanup
			runV7FullGateProviderCleanup = func(_ context.Context, _ string, scope *v7FullGateProviderScope, _ []string, _ io.Writer) error {
				request, err := scope.readRequest()
				if err != nil {
					return err
				}
				result := v7CertifiedProviderResultFixture(request, v7FullGateOutcomeCanceled)
				return scope.persistResult(result)
			}
			defer func() { runV7FullGateProviderCleanup = previousCleanup }()
			if err := recoverV7FullGateProviderScopes(stateRoot, store); err != nil {
				t.Fatalf("first restart recovery: %v", err)
			}
			if err := recoverV7FullGateProviderScopes(stateRoot, store); err != nil {
				t.Fatalf("second restart recovery: %v", err)
			}
			preparations, _ = os.ReadDir(filepath.Join(stateRoot, "full-gate-preparing"))
			recovery, _ = os.ReadDir(filepath.Join(stateRoot, "full-gate-recovery"))
			if len(preparations) != 0 || len(recovery) != 0 {
				t.Fatalf("recovery did not retire crash state: preparing=%d recovery=%d", len(preparations), len(recovery))
			}
			durable, findErr := store.FindDepartureRun(run.ID)
			if findErr != nil || durable == nil {
				t.Fatalf("read departure: %#v, %v", durable, findErr)
			}
			wantOutcomes := 0
			if tc.published {
				wantOutcomes = 1
			}
			if len(durable.Gate.ProviderOutcomes) != wantOutcomes {
				t.Fatalf("published=%t outcomes=%#v", tc.published, durable.Gate.ProviderOutcomes)
			}
		})
	}
}

func TestV7FullGateProviderRecoveryCrashSeamsConverge(t *testing.T) {
	seams := []struct {
		name       string
		recovery   string
		durability string
	}{
		{name: "journal synced before target transition", recovery: "outcome_journal_ready"},
		{name: "target persisted before scope retirement", recovery: "outcome_target_persisted"},
		{name: "scope retired before journal deletion", durability: "before_outcome_journal_remove"},
	}
	for _, seam := range seams {
		t.Run(seam.name, func(t *testing.T) {
			stateRoot := t.TempDir()
			store, run := openV7ProviderRecoveryRun(t, stateRoot, seam.name)
			defer store.Close()
			scope, request, result, receipt := writeV7ProviderOutcomeFixture(t, stateRoot, run.ID, v7FullGateOutcomeProvider)
			if err := persistV7FullGateProviderOutcomeJournal(stateRoot, scope, request, result, receipt); err != nil {
				t.Fatal(err)
			}
			journalPath := v7FullGateProviderOutcomeJournalPath(stateRoot, request.DepartureID, request.RequestDigest)
			previousRecovery, previousDurability := v7FullGateRecoveryHook, v7FullGateDurabilityHook
			v7FullGateRecoveryHook = func(stage string) error {
				if stage == seam.recovery {
					return errors.New("fixture crash at " + stage)
				}
				return nil
			}
			v7FullGateDurabilityHook = func(stage string) error {
				if stage == seam.durability {
					return errors.New("fixture crash at " + stage)
				}
				return nil
			}
			err := recoverV7FullGateProviderScopes(stateRoot, store)
			v7FullGateRecoveryHook, v7FullGateDurabilityHook = previousRecovery, previousDurability
			if err == nil {
				t.Fatal("injected crash seam unexpectedly completed")
			}
			if _, statErr := os.Stat(journalPath); statErr != nil {
				t.Fatalf("crash seam lost outcome journal: %v", statErr)
			}
			if err := recoverV7FullGateProviderScopes(stateRoot, store); err != nil {
				t.Fatalf("first restart recovery: %v", err)
			}
			if err := recoverV7FullGateProviderScopes(stateRoot, store); err != nil {
				t.Fatalf("second restart recovery: %v", err)
			}
			if _, statErr := os.Stat(filepath.Dir(scope.requestPath)); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("scope remained after convergent recovery: %v", statErr)
			}
			if _, statErr := os.Stat(journalPath); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("journal remained after convergent recovery: %v", statErr)
			}
			durable, findErr := store.FindDepartureRun(run.ID)
			if findErr != nil || durable == nil || durable.State != DepartureStateBlocked || len(durable.Gate.ProviderOutcomes) != 1 {
				t.Fatalf("durable recovered outcome = %#v, %v", durable, findErr)
			}
		})
	}
}

func TestV7FullGateProviderCancellationRemovesOnlyUnboundArtifacts(t *testing.T) {
	stateRoot := t.TempDir()
	scope, request, result, receipt := writeV7ProviderOutcomeFixture(t, stateRoot, "departure-cancel-artifact", v7FullGateOutcomeFailed)
	if err := persistV7FullGateProviderOutcomeJournal(stateRoot, scope, request, result, receipt); err != nil {
		t.Fatal(err)
	}
	unbound := filepath.Join(stateRoot, "artifacts", "promotion-gates", "unbound-summary.log")
	if err := os.WriteFile(unbound, []byte("unbound summary\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := removeV7ProvablyUnboundPromotionArtifacts(stateRoot, []string{request.ArtifactRef, unbound}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(request.ArtifactRef); err != nil {
		t.Fatalf("cancellation removed journal-bound evidence: %v", err)
	}
	if _, err := os.Stat(unbound); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("provably unbound cancellation artifact remained: %v", err)
	}
}

func TestV7FullGateProviderRecoveryRejectsSharedJournalArtifact(t *testing.T) {
	stateRoot := t.TempDir()
	store, run := openV7ProviderRecoveryRun(t, stateRoot, "shared-journal-artifact")
	defer store.Close()
	firstScope, firstRequest, firstResult, firstReceipt := writeV7ProviderOutcomeFixture(t, stateRoot, run.ID, v7FullGateOutcomeFailed)
	if err := persistV7FullGateProviderOutcomeJournal(stateRoot, firstScope, firstRequest, firstResult, firstReceipt); err != nil {
		t.Fatal(err)
	}
	secondDir := filepath.Join(stateRoot, "full-gate-recovery", "scope-second")
	if err := os.MkdirAll(secondDir, 0o700); err != nil {
		t.Fatal(err)
	}
	secondRequest := firstRequest
	secondRequest.RunID = "fixture-run-second"
	secondRequest.Command = "fixture-second"
	secondRequest.ResultPath = filepath.Join(secondDir, "result.json")
	secondRequest.RequestDigest = v7FullGateRequestDigest(secondRequest)
	secondResult := v7CertifiedProviderResultFixture(secondRequest, v7FullGateOutcomePassed)
	secondReceipt := v7GateProviderReceiptForResult(secondRequest, secondResult)
	requestRaw, _ := json.Marshal(secondRequest)
	resultRaw, _ := json.Marshal(secondResult)
	secondRequestPath := filepath.Join(secondDir, "request.json")
	if err := os.WriteFile(secondRequestPath, requestRaw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(secondRequest.ResultPath, resultRaw, 0o600); err != nil {
		t.Fatal(err)
	}
	secondScope := &v7FullGateProviderScope{request: secondRequest, requestPath: secondRequestPath}
	if err := persistV7FullGateProviderOutcomeJournal(stateRoot, secondScope, secondRequest, secondResult, secondReceipt); err != nil {
		t.Fatal(err)
	}
	if err := recoverV7FullGateProviderScopes(stateRoot, store); err == nil || !strings.Contains(err.Error(), "share one artifact target") {
		t.Fatalf("shared journal artifact recovery = %v", err)
	}
	durable, err := store.FindDepartureRun(run.ID)
	if err != nil || durable == nil || durable.State == DepartureStateBlocked {
		t.Fatalf("ambiguous shared evidence mutated departure: %#v, %v", durable, err)
	}
}

func TestV7FullGateProviderRecoveryRetiresPublishedOutcomeWithoutRewritingDeparture(t *testing.T) {
	stateRoot := t.TempDir()
	store, run := openV7ProviderRecoveryRun(t, stateRoot, "published-ordinary-red")
	defer store.Close()
	scope, request, result, receipt := writeV7ProviderOutcomeFixture(t, stateRoot, run.ID, v7FullGateOutcomeFailed)
	if err := persistV7FullGateProviderOutcomeJournal(stateRoot, scope, request, result, receipt); err != nil {
		t.Fatal(err)
	}
	state, err := openV7FullGateStateRoot(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	journalRel := v7FullGateProviderOutcomeJournalRelPath(request.DepartureID, request.RequestDigest)
	journal, err := readV7FullGateProviderOutcomeJournalAtRoot(state, journalRel)
	if err == nil {
		err = ensureV7FullGateJournalArtifactAtRoot(state, &journal)
	}
	if closeErr := state.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		t.Fatal(err)
	}
	intent := run
	intent.State = DepartureStateRepairing
	intent.Gate.Status = "failed"
	intent.Gate.ArtifactRef = request.ArtifactRef
	intent.Gate.ArtifactRefs = []string{request.ArtifactRef}
	intent.Gate.ProviderOutcomes = []GateProviderReceipt{receipt}
	intent.Gate.Failure = DepartureFailure{
		Class: "isolated", Identity: "stable-defect", OwningTaskID: "APP-T-0001", Action: "owner_rework",
		ArtifactRefs: []string{request.ArtifactRef},
	}
	changed, err := store.TransitionDepartureRun(intent, run.StateRevision)
	if err != nil || !changed {
		t.Fatalf("persist ordinary-red departure: changed=%t err=%v", changed, err)
	}
	before, err := store.FindDepartureRun(run.ID)
	if err != nil || before == nil {
		t.Fatal(err)
	}
	beforeGate, _ := json.Marshal(before.Gate)
	if err := recoverV7FullGateProviderScopes(stateRoot, store); err != nil {
		t.Fatalf("recover exact published outcome: %v", err)
	}
	after, err := store.FindDepartureRun(run.ID)
	if err != nil || after == nil {
		t.Fatal(err)
	}
	afterGate, _ := json.Marshal(after.Gate)
	if after.State != before.State || after.StateRevision != before.StateRevision || !bytes.Equal(afterGate, beforeGate) || after.BlockReason != before.BlockReason {
		t.Fatalf("recovery rewrote published departure:\nbefore=%#v\nafter=%#v", before, after)
	}
	if _, err := os.Stat(filepath.Dir(scope.requestPath)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("published outcome scope remained: %v", err)
	}
	if _, err := os.Stat(v7FullGateProviderOutcomeJournalPath(stateRoot, request.DepartureID, request.RequestDigest)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("published outcome journal remained: %v", err)
	}
}

func TestV7FullGateProviderArtifactBindingCrashRecoversExactRedEvidence(t *testing.T) {
	stateRoot := t.TempDir()
	store, run := openV7ProviderRecoveryRun(t, stateRoot, "artifact-bound-before-departure-cas")
	defer store.Close()
	scope, request, result, receipt := writeV7ProviderOutcomeFixture(t, stateRoot, run.ID, v7FullGateOutcomeProvider)
	exact := []byte("exact red provider evidence\nwith stable bytes\n")
	if err := os.Remove(request.ArtifactRef); err != nil {
		t.Fatal(err)
	}
	state, err := openV7FullGateStateRoot(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeV7DurablePromotionArtifactAtRoot(state, request.ArtifactRef, exact); err != nil {
		_ = state.Close()
		t.Fatal(err)
	}
	if err := state.Close(); err != nil {
		t.Fatal(err)
	}
	if err := persistV7FullGateProviderOutcomeJournal(stateRoot, scope, request, result, receipt); err != nil {
		t.Fatal(err)
	}
	injected := errors.New("fixture crash after fsynced artifact journal binding")
	previous := v7FullGateDurabilityHook
	v7FullGateDurabilityHook = func(stage string) error {
		if stage == "outcome_artifact_bound" {
			return injected
		}
		return nil
	}
	err = recoverV7FullGateProviderScopes(stateRoot, store)
	v7FullGateDurabilityHook = previous
	if !errors.Is(err, injected) {
		t.Fatalf("artifact-binding crash seam = %v", err)
	}
	durable, err := store.FindDepartureRun(run.ID)
	if err != nil || durable == nil || durable.State == DepartureStateBlocked {
		t.Fatalf("departure CAS happened before bound-journal seam: %#v, %v", durable, err)
	}
	journalPath := v7FullGateProviderOutcomeJournalPath(stateRoot, request.DepartureID, request.RequestDigest)
	journal, err := readV7FullGateProviderOutcomeJournal(journalPath)
	if err != nil || journal.ArtifactRef != request.ArtifactRef || journal.ArtifactDigest != v7FullGateTextDigest(string(exact)) {
		t.Fatalf("durable artifact binding = %#v, %v", journal, err)
	}
	if err := recoverV7FullGateProviderScopes(stateRoot, store); err != nil {
		t.Fatalf("restart artifact recovery: %v", err)
	}
	durable, err = store.FindDepartureRun(run.ID)
	if err != nil || durable == nil || durable.State != DepartureStateBlocked || durable.Gate.ArtifactRef != request.ArtifactRef || len(durable.Gate.Failure.ArtifactRefs) != 1 || durable.Gate.Failure.ArtifactRefs[0] != request.ArtifactRef {
		t.Fatalf("recovered departure evidence = %#v, %v", durable, err)
	}
	got, err := os.ReadFile(durable.Gate.ArtifactRef)
	if err != nil || !bytes.Equal(got, exact) {
		t.Fatalf("recovered artifact content = %q, %v", got, err)
	}
	if _, err := os.Stat(filepath.Dir(scope.requestPath)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("artifact-bound recovery scope remained: %v", err)
	}
	if _, err := os.Stat(journalPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("artifact-bound recovery journal remained: %v", err)
	}
}

func TestV7FullGateProviderRecoveryFailsUnjournaledGreenClosed(t *testing.T) {
	stateRoot := t.TempDir()
	store, run := openV7ProviderRecoveryRun(t, stateRoot, "unjournaled-green")
	defer store.Close()
	scope, request, _, _ := writeV7ProviderOutcomeFixture(t, stateRoot, run.ID, v7FullGateOutcomePassed)
	if err := recoverV7FullGateProviderScopes(stateRoot, store); err != nil {
		t.Fatal(err)
	}
	if err := recoverV7FullGateProviderScopes(stateRoot, store); err != nil {
		t.Fatalf("second recovery: %v", err)
	}
	durable, err := store.FindDepartureRun(run.ID)
	if err != nil || durable == nil || durable.Gate.Status != string(v7FullGateOutcomeProvider) || len(durable.Gate.ProviderOutcomes) != 1 || durable.Gate.ProviderOutcomes[0].Outcome != string(v7FullGateOutcomeProvider) {
		t.Fatalf("unjournaled green was not routed fail-closed: %#v, %v", durable, err)
	}
	if entry, err := store.FindGateLedger(request.ProjectID, request.CandidateDigest, request.Command, request.Profile, request.Toolchain); err != nil || entry != nil {
		t.Fatalf("unjournaled green entered ledger: %#v, %v", entry, err)
	}
	if _, err := os.Stat(filepath.Dir(scope.requestPath)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unjournaled green scope remained: %v", err)
	}
}

func TestV7FullGateProviderRecoveryCleansDurableRequestBeforeWrapper(t *testing.T) {
	stateRoot := t.TempDir()
	store, run := openV7ProviderRecoveryRun(t, stateRoot, "request-before-wrapper")
	defer store.Close()
	scope, request, _, _ := writeV7ProviderOutcomeFixture(t, stateRoot, run.ID, v7FullGateOutcomePassed)
	if err := os.Remove(request.ResultPath); err != nil {
		t.Fatal(err)
	}
	previous := runV7FullGateProviderCleanup
	runV7FullGateProviderCleanup = func(_ context.Context, _ string, scope *v7FullGateProviderScope, _ []string, _ io.Writer) error {
		result := v7CertifiedProviderResultFixture(request, v7FullGateOutcomeCanceled)
		return scope.persistResult(result)
	}
	defer func() { runV7FullGateProviderCleanup = previous }()
	if err := recoverV7FullGateProviderScopes(stateRoot, store); err != nil {
		t.Fatal(err)
	}
	if err := recoverV7FullGateProviderScopes(stateRoot, store); err != nil {
		t.Fatalf("second recovery: %v", err)
	}
	durable, err := store.FindDepartureRun(run.ID)
	if err != nil || durable == nil || len(durable.Gate.ProviderOutcomes) != 1 || durable.Gate.ProviderOutcomes[0].Outcome != string(v7FullGateOutcomeCanceled) {
		t.Fatalf("pre-wrapper recovery outcome = %#v, %v", durable, err)
	}
	if _, err := os.Stat(filepath.Dir(scope.requestPath)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("pre-wrapper scope remained: %v", err)
	}
}

func TestV7FullGateProviderNormalizesOverrunAndOutcomeMismatch(t *testing.T) {
	_, request, passed, _ := writeV7ProviderOutcomeFixture(t, t.TempDir(), "departure-normalize", v7FullGateOutcomePassed)
	for _, reason := range []string{"daemon_measured_runtime_exceeded", "cleanup_outcome_mismatch_expected_timed_out", "wrapper_failed_after_gate_pass"} {
		got := normalizedV7FullGateProviderFailure(request, passed, reason)
		if got.Outcome != v7FullGateOutcomeProvider || got.Error != reason || got.ResultDigest != v7FullGateProviderResultDigest(got) || got.ReceiptDigest != v7FullGateReceiptDigest(request, got) {
			t.Fatalf("normalization %q = %#v", reason, got)
		}
	}
}

func TestV7FullGateProviderContractFailuresAreTyped(t *testing.T) {
	t.Run("identity", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "provider")
		if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
			t.Fatal(err)
		}
		provider := newExternalV7ProviderFixture(t, path, t.TempDir())
		provider.executableIdentity = "sha256:" + strings.Repeat("0", 64)
		invocation, err := provider.Run(context.Background(), t.TempDir(), "fixture")
		if err == nil || invocation.Outcome != v7FullGateOutcomeProvider || invocation.Receipt.RequestDigest != "" {
			t.Fatalf("identity failure invocation = %#v, %v", invocation, err)
		}
	})

	t.Run("start", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "provider")
		if err := os.WriteFile(path, []byte("not-an-executable-format\n"), 0o700); err != nil {
			t.Fatal(err)
		}
		provider := newExternalV7ProviderFixture(t, path, t.TempDir())
		invocation, err := provider.Run(context.Background(), t.TempDir(), "fixture")
		if err == nil || invocation.Outcome != v7FullGateOutcomeProvider || invocation.Receipt.RequestDigest != "" {
			t.Fatalf("start failure invocation = %#v, %v", invocation, err)
		}
	})

	t.Run("invalid result with certified cleanup", func(t *testing.T) {
		stateRoot := t.TempDir()
		path, err := exec.LookPath("true")
		if err != nil {
			t.Fatal(err)
		}
		provider := newExternalV7ProviderFixture(t, path, stateRoot)
		previous := runV7FullGateProviderCleanup
		runV7FullGateProviderCleanup = func(_ context.Context, _ string, scope *v7FullGateProviderScope, _ []string, _ io.Writer) error {
			request, err := scope.readRequest()
			if err != nil {
				return err
			}
			result := v7CertifiedProviderResultFixture(request, v7FullGateOutcomePassed)
			return scope.persistResult(result)
		}
		defer func() { runV7FullGateProviderCleanup = previous }()
		invocation, err := provider.Run(context.Background(), t.TempDir(), "fixture")
		if err == nil || invocation.Outcome != v7FullGateOutcomeProvider || invocation.Receipt.Outcome != string(v7FullGateOutcomeProvider) || !v7FullGateDigest(invocation.Receipt.RequestDigest) {
			t.Fatalf("invalid-result failure invocation = %#v, %v", invocation, err)
		}
		journalPath := v7FullGateProviderOutcomeJournalPath(stateRoot, invocation.Receipt.DepartureID, invocation.Receipt.RequestDigest)
		journal, readErr := readV7FullGateProviderOutcomeJournal(journalPath)
		if readErr != nil || journal.Result.Outcome != v7FullGateOutcomeProvider || journal.Result.Error != "invalid_run_result_before_certified_cleanup" {
			t.Fatalf("invalid-result journal = %#v, %v", journal, readErr)
		}
		if err := writeV7DurablePromotionArtifactAtRoot(provider.state, provider.binding.ArtifactRef, []byte("invalid result evidence\n")); err != nil {
			t.Fatal(err)
		}
		if err := provider.BindFullGateProviderArtifact(provider.binding.ArtifactRef, []GateProviderReceipt{invocation.Receipt}); err != nil {
			t.Fatal(err)
		}
		if err := provider.FinalizeFullGateProviderOutcome(invocation.Receipt); err != nil {
			t.Fatalf("finalize invalid-result outcome: %v", err)
		}
	})
}

func TestV7FullGateProviderClosureDriftInvalidatesLedgerReceipt(t *testing.T) {
	receipt := testV7FullGateProviderReceipt
	provider := &v7ExternalFullGateProvider{
		identity: receipt.ProviderClosureDigest, executableIdentity: receipt.ProviderDigest, clientDigest: receipt.ClientDigest,
		runtimeDigest: receipt.RuntimeDigest, policyDigest: receipt.PolicyDigest, attestationDigest: receipt.AttestationDigest,
		imageOrVMID: receipt.ImageOrVMID, profile: receipt.ProviderProfile,
	}
	if !provider.MatchesGateProviderReceipt(&receipt) {
		t.Fatal("exact provider closure rejected its receipt")
	}
	provider.identity = "sha256:" + strings.Repeat("0", 64)
	if provider.MatchesGateProviderReceipt(&receipt) {
		t.Fatal("provider closure drift reused a stale ledger receipt")
	}
}

func newExternalV7ProviderFixture(t *testing.T, path, stateRoot string) *v7ExternalFullGateProvider {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	executableID := fmt.Sprintf("sha256:%x", sum[:])
	if _, verified, verifyErr := verifyV7TrustedProviderExecutable(path); verifyErr == nil {
		executableID = verified
	}
	digest := func(ch byte) string { return "sha256:" + strings.Repeat(string(ch), 64) }
	return &v7ExternalFullGateProvider{
		path: path, kind: "container", identity: digest('a'), executableIdentity: executableID,
		runtimeDigest: digest('b'), clientDigest: digest('c'), policyDigest: digest('d'), attestationDigest: digest('e'),
		capabilities: []string{"candidate_read_only", "network_denied", "control_env_denied"}, implementationID: v7KnownFullGateProvider,
		capabilitySchema: v7FullGateCapabilitySchema, imageOrVMID: digest('f'), profile: "fixture", stateRoot: stateRoot,
		recoveryRoot: filepath.Join(stateRoot, "full-gate-recovery"),
		binding:      v7FullGateProviderBinding{ProjectID: "project", DepartureID: "departure-contract-failure", CandidateDigest: digest('1'), GateProfile: "full", ProviderProfile: "fixture", Toolchain: "toolchain", ArtifactRef: filepath.Join(stateRoot, "artifacts", "promotion-gates", "fixture.log")},
	}
}

func writeV7ProviderOutcomeFixture(t *testing.T, stateRoot, departureID string, outcome v7FullGateOutcome) (*v7FullGateProviderScope, v7FullGateProviderRequest, v7FullGateProviderResult, GateProviderReceipt) {
	t.Helper()
	requestPath, request := writeV7ProviderRecoveryRequest(t, stateRoot)
	digest := func(ch byte) string { return "sha256:" + strings.Repeat(string(ch), 64) }
	request.ProjectID = "project"
	request.DepartureID = departureID
	request.CandidateDigest = digest('1')
	request.Profile = "full"
	request.ProviderProfile = "fixture"
	request.Toolchain = "fixture-toolchain"
	request.ArtifactRef = filepath.Join(stateRoot, "artifacts", "promotion-gates", "fixture-"+strings.ToLower(newRecordID())+".log")
	request.ProviderID = digest('2')
	request.ClientDigest = digest('3')
	request.MaxCommandBytes = v7FullGateCommandMaxBytes
	request.MaxOutputBytes = 1_024
	request.MaxRuntimeMS = 1_000
	request.MaxArtifactBytes = 1_024
	request.RequestDigest = v7FullGateRequestDigest(request)
	if err := os.MkdirAll(filepath.Dir(request.ArtifactRef), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(request.ArtifactRef, []byte("exact provider evidence\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(requestPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	result := v7CertifiedProviderResultFixture(request, outcome)
	raw, err = json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(request.ResultPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	scope := &v7FullGateProviderScope{request: request, requestPath: requestPath}
	return scope, request, result, v7GateProviderReceiptForResult(request, result)
}

func v7CertifiedProviderResultFixture(request v7FullGateProviderRequest, outcome v7FullGateOutcome) v7FullGateProviderResult {
	result := v7FullGateProviderResult{
		Schema: v7FullGateProviderSchema, Contract: v7FullGateIsolationContract, RunID: request.RunID, LifecycleID: "fixture-scope", State: "cleaned", Outcome: outcome,
		Output: "fixture-output", ProviderID: request.ProviderID, RequestDigest: request.RequestDigest, RuntimeDigest: request.RuntimeDigest, PolicyDigest: request.PolicyDigest,
		AttestationDigest: request.AttestationDigest, Capabilities: append([]string(nil), request.RequiredCapabilities...), ImplementationID: request.ImplementationID,
		CapabilitySchema: request.CapabilitySchema, CandidateReadOnlyMeasured: true, NetworkMode: "none", ControlEnvAbsent: true, ControlMountsAbsent: true,
		ImageOrVMID: request.ExpectedImageOrVMID, RuntimeMS: 10, ArtifactBytes: 16,
	}
	v7SealFullGateProviderResult(request, &result)
	return result
}

func writeV7RawJournalFixture(t *testing.T, path string, journal v7FullGateProviderOutcomeJournal) {
	t.Helper()
	raw, err := json.Marshal(journal)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

func openV7ProviderRecoveryRun(t *testing.T, stateRoot, suffix string) (*RuntimeStore, DepartureRun) {
	t.Helper()
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	run, _, err := store.GetOrCreateDepartureRun(DepartureRun{ProjectID: "project", PolicyID: "provider-recovery-" + strings.ReplaceAll(suffix, " ", "-"), ScheduledWindow: "2026-07-26T00:00:00Z", State: DepartureStateGating})
	if err != nil {
		store.Close()
		t.Fatal(err)
	}
	return store, run
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

func (p *fakeV7FullGateProvider) Run(ctx context.Context, _, _ string) (v7FullGateProviderInvocation, error) {
	// The child is intentionally outside the root process group/ancestry. The
	// provider owns the fixture's lifecycle scope, so cleanup is scope-wide.
	p.survivor = true
	p.survivor = false
	p.cleanups++
	if err := ctx.Err(); err != nil {
		return v7FullGateProviderInvocation{Outcome: v7FullGateOutcomeCanceled}, err
	}
	return v7FullGateProviderInvocation{Output: []byte("provider-cleaned"), Outcome: v7FullGateOutcomePassed}, nil
}

func (p *fakeV7FullGateProvider) Close() error { return nil }
