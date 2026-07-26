package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"gopkg.in/yaml.v3"
)

// This is intentionally a provider boundary, not a Darwin process-tree
// implementation. Darwin exposes useful identity facts (audit PID versions,
// start times, SessionCreate), but no public unprivileged API that can
// enumerate and terminate every process in a launch/audit scope after a
// double-fork/reparent. A provider must own a container or VM and make its
// lifecycle scope disappear before it reports a command complete.
const (
	v7FullGateIsolationContract = "tusker.full-gate-isolation/lifecycle-provider/v3"
	v7FullGateProviderSchema    = "tusker.full-gate-provider/v1"
	v7FullGateCapabilitySchema  = "tusker.full-gate-capabilities/v1"
	v7KnownFullGateProvider     = "tusker.lifecycle-provider/v1"
	v7FullGateCleanTimeout      = 2 * time.Second
	v7FullGateRequestMaxBytes   = 64 << 10
	v7FullGateResultMaxBytes    = 1 << 20
	v7FullGateOutputMaxBytes    = 1 << 20
	v7FullGateRecoveryMaxScopes = 128
)

var errV7FullGateProvider = errors.New("full-gate lifecycle provider")

var runV7FullGateProviderCleanup = func(ctx context.Context, providerPath, requestPath string, env []string, output io.Writer) error {
	cmd := exec.CommandContext(ctx, providerPath, "--tusker-full-gate-cleanup", requestPath)
	cmd.Env = env
	cmd.Stdout, cmd.Stderr = output, output
	return cmd.Run()
}

type v7FullGateProviderRequest struct {
	Schema               string   `json:"schema"`
	Contract             string   `json:"contract"`
	RunID                string   `json:"run_id"`
	Workspace            string   `json:"workspace"`
	Command              string   `json:"command"`
	ResultPath           string   `json:"result_path"`
	ProviderKind         string   `json:"provider_kind"`
	ProviderID           string   `json:"provider_id"`
	ProviderPath         string   `json:"provider_path"`
	ExecutableID         string   `json:"executable_id"`
	RequestDigest        string   `json:"request_digest"`
	CandidateReadOnly    bool     `json:"candidate_read_only"`
	NetworkDenied        bool     `json:"network_denied"`
	ControlEnvDenied     bool     `json:"control_env_denied"`
	RuntimeDigest        string   `json:"runtime_digest"`
	PolicyDigest         string   `json:"policy_digest"`
	AttestationDigest    string   `json:"attestation_digest"`
	RequiredCapabilities []string `json:"required_capabilities"`
	ImplementationID     string   `json:"implementation_id"`
	CapabilitySchema     string   `json:"capability_schema"`
	ExpectedImageOrVMID  string   `json:"expected_image_or_vm_id"`
}

// v7FullGateProviderResult is a receipt from the provider, not a hint. The
// provider may return only after every process in its container/VM scope has
// stopped; lifecycle_id ties both normal completion and recovery to that
// immutable provider-side scope.
type v7FullGateProviderResult struct {
	Schema                    string   `json:"schema"`
	Contract                  string   `json:"contract"`
	RunID                     string   `json:"run_id"`
	LifecycleID               string   `json:"lifecycle_id"`
	State                     string   `json:"state"`
	Output                    string   `json:"output,omitempty"`
	Error                     string   `json:"error,omitempty"`
	ProviderID                string   `json:"provider_id"`
	RequestDigest             string   `json:"request_digest"`
	RuntimeDigest             string   `json:"runtime_digest"`
	PolicyDigest              string   `json:"policy_digest"`
	AttestationDigest         string   `json:"attestation_digest"`
	Capabilities              []string `json:"capabilities"`
	ReceiptDigest             string   `json:"receipt_digest"`
	ImplementationID          string   `json:"implementation_id"`
	CapabilitySchema          string   `json:"capability_schema"`
	CandidateReadOnlyMeasured bool     `json:"candidate_read_only_measured"`
	NetworkMode               string   `json:"network_mode"`
	ControlEnvAbsent          bool     `json:"control_env_absent"`
	ControlMountsAbsent       bool     `json:"control_mounts_absent"`
	ImageOrVMID               string   `json:"image_or_vm_id"`
}

type v7FullGateProvider interface {
	Run(context.Context, string, string) ([]byte, error)
	Close() error
}

type v7GateBoundedOutput struct {
	b      bytes.Buffer
	max    int
	cutoff bool
}

func (b *v7GateBoundedOutput) Write(data []byte) (int, error) {
	if b.max <= 0 {
		return len(data), nil
	}
	left := b.max - b.b.Len()
	if left > 0 {
		if len(data) > left {
			_, _ = b.b.Write(data[:left])
			b.cutoff = true
		} else {
			_, _ = b.b.Write(data)
		}
	} else {
		b.cutoff = true
	}
	return len(data), nil
}

func (b *v7GateBoundedOutput) Bytes() []byte {
	if b.cutoff {
		return append(append([]byte(nil), b.b.Bytes()...), []byte("\n[provider output truncated]\n")...)
	}
	return append([]byte(nil), b.b.Bytes()...)
}

type v7TrustedFullGateProvider struct {
	Kind              string   `yaml:"kind"`
	Command           string   `yaml:"command"`
	Version           string   `yaml:"version"`
	RuntimeDigest     string   `yaml:"runtime_digest"`
	PolicyDigest      string   `yaml:"policy_digest"`
	AttestationDigest string   `yaml:"attestation_digest"`
	Capabilities      []string `yaml:"capabilities"`
	ImplementationID  string   `yaml:"implementation_id"`
	CapabilitySchema  string   `yaml:"capability_schema"`
	ImageOrVMID       string   `yaml:"image_or_vm_id"`
}

type v7FullGateProviderRegistry struct {
	Schema    string                               `yaml:"schema"`
	Providers map[string]v7TrustedFullGateProvider `yaml:"providers"`
}

var v7FullGateProviderRegistryPath = func(stateRoot string) string {
	return filepath.Join(stateRoot, "full-gate-providers.yaml")
}

var newV7FullGateProvider = func(profile, repoRoot, stateRoot string) (v7FullGateProvider, error) {
	trusted, path, identity, executableIdentity, err := resolveV7TrustedFullGateProvider(profile, stateRoot)
	if err != nil {
		return nil, err
	}
	if v7PathWithin(repoRoot, path) {
		return nil, fmt.Errorf("%w: trusted provider executable must not be repository-local", errV7FullGateProvider)
	}
	return &v7ExternalFullGateProvider{path: path, kind: trusted.Kind, identity: identity, executableIdentity: executableIdentity, runtimeDigest: trusted.RuntimeDigest, policyDigest: trusted.PolicyDigest, attestationDigest: trusted.AttestationDigest, capabilities: append([]string(nil), trusted.Capabilities...), implementationID: trusted.ImplementationID, capabilitySchema: trusted.CapabilitySchema, imageOrVMID: trusted.ImageOrVMID, recoveryRoot: filepath.Join(stateRoot, "full-gate-recovery")}, nil
}

type v7ExternalFullGateProvider struct {
	path               string
	kind               string
	identity           string
	executableIdentity string
	runtimeDigest      string
	policyDigest       string
	attestationDigest  string
	capabilities       []string
	implementationID   string
	capabilitySchema   string
	imageOrVMID        string
	recoveryRoot       string
	mu                 sync.Mutex
	active             *v7FullGateProviderScope
	lastReceipt        v7FullGateProviderAudit
	closed             bool
}

type v7FullGateProviderScope struct {
	request     v7FullGateProviderRequest
	requestPath string
	// Close and Run can race during daemon cancellation. Cache one cleanup
	// result for this immutable scope so the provider receives exactly one
	// lifecycle-destruction command; a later daemon recovery owns retries.
	cleanupMu     sync.Mutex
	cleanupCalled bool
	cleanupResult v7FullGateProviderResult
	cleanupErr    error
}

type v7FullGateProviderAudit struct {
	LifecycleID       string
	ReceiptDigest     string
	RuntimeDigest     string
	PolicyDigest      string
	AttestationDigest string
	ImageOrVMID       string
}

func (p *v7ExternalFullGateProvider) LastReceipt() v7FullGateProviderAudit {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.lastReceipt
}

// MatchesGateProviderReceipt binds ledger reuse to this provider's currently
// trusted immutable profile. A syntactically valid receipt from another
// runtime, policy, attestation, or image is not promotion proof here.
func (p *v7ExternalFullGateProvider) MatchesGateProviderReceipt(receipt *GateProviderReceipt) bool {
	return v7CertifiedGateProviderReceipt(receipt) && receipt.RuntimeDigest == p.runtimeDigest && receipt.PolicyDigest == p.policyDigest && receipt.AttestationDigest == p.attestationDigest && receipt.ImageOrVMID == p.imageOrVMID
}

func (p *v7ExternalFullGateProvider) recordReceipt(result v7FullGateProviderResult) {
	p.mu.Lock()
	p.lastReceipt = v7FullGateProviderAudit{LifecycleID: result.LifecycleID, ReceiptDigest: result.ReceiptDigest, RuntimeDigest: result.RuntimeDigest, PolicyDigest: result.PolicyDigest, AttestationDigest: result.AttestationDigest, ImageOrVMID: result.ImageOrVMID}
	p.mu.Unlock()
}

func resolveV7TrustedFullGateProvider(profile, stateRoot string) (v7TrustedFullGateProvider, string, string, string, error) {
	profile = strings.TrimSpace(profile)
	if profile == "" {
		return v7TrustedFullGateProvider{}, "", "", "", fmt.Errorf("%w: scheduled full promotion requires a configured lifecycle-safe container/VM isolation_provider profile", errV7FullGateProvider)
	}
	if err := v7TrustedProviderStateRoot(stateRoot); err != nil {
		return v7TrustedFullGateProvider{}, "", "", "", err
	}
	registryPath := v7FullGateProviderRegistryPath(stateRoot)
	raw, err := readV7TrustedRegularFile(registryPath, v7FullGateRequestMaxBytes, false)
	if err != nil {
		return v7TrustedFullGateProvider{}, "", "", "", fmt.Errorf("%w: trusted provider registry %s is unavailable: %v", errV7FullGateProvider, registryPath, err)
	}
	var registry v7FullGateProviderRegistry
	if err := yaml.Unmarshal(raw, &registry); err != nil {
		return v7TrustedFullGateProvider{}, "", "", "", fmt.Errorf("%w: parse trusted provider registry: %v", errV7FullGateProvider, err)
	}
	trusted, ok := registry.Providers[profile]
	if registry.Schema != v7FullGateProviderSchema || !ok || trusted.Kind != "container" && trusted.Kind != "vm" || strings.TrimSpace(trusted.Version) == "" || trusted.ImplementationID != v7KnownFullGateProvider || trusted.CapabilitySchema != v7FullGateCapabilitySchema || !v7FullGateDigest(trusted.RuntimeDigest) || !v7FullGateDigest(trusted.PolicyDigest) || !v7FullGateDigest(trusted.AttestationDigest) || !v7FullGateDigest(trusted.ImageOrVMID) || !v7FullGateCapabilities(trusted.Capabilities) {
		return v7TrustedFullGateProvider{}, "", "", "", fmt.Errorf("%w: trusted provider profile %q is not a valid container/VM lifecycle provider", errV7FullGateProvider, profile)
	}
	path, identity, err := verifyV7TrustedProviderExecutable(trusted.Command)
	if err != nil {
		return v7TrustedFullGateProvider{}, "", "", "", err
	}
	return trusted, path, departureFingerprint(append([]string{profile, trusted.Kind, trusted.Version, trusted.ImplementationID, trusted.CapabilitySchema, identity, trusted.RuntimeDigest, trusted.PolicyDigest, trusted.AttestationDigest, trusted.ImageOrVMID}, trusted.Capabilities...)...), identity, nil
}

func v7TrustedProviderStateRoot(stateRoot string) error {
	info, err := os.Lstat(stateRoot)
	if err != nil || !info.IsDir() || info.Mode()&0o022 != 0 {
		return fmt.Errorf("%w: daemon state root for provider registry must be a non-group/world-writable directory", errV7FullGateProvider)
	}
	return nil
}

func verifyV7TrustedProviderExecutable(path string) (string, string, error) {
	if !filepath.IsAbs(path) || strings.ContainsAny(path, "\n\r\t ") || filepath.Base(path) == "sandbox-exec" {
		return "", "", fmt.Errorf("%w: provider executable must be an absolute non-sandbox executable path", errV7FullGateProvider)
	}
	raw, err := readV7TrustedRegularFile(path, 64<<20, true)
	if err != nil {
		return "", "", fmt.Errorf("%w: read trusted provider executable: %v", errV7FullGateProvider, err)
	}
	if bytes.HasPrefix(raw, []byte("#!")) {
		return "", "", fmt.Errorf("%w: provider executable must be a native immutable binary, not an interpreter-selected script", errV7FullGateProvider)
	}
	sum := sha256.Sum256(raw)
	return path, fmt.Sprintf("sha256:%x", sum[:]), nil
}

func v7FullGateDigest(value string) bool {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "sha256:") || len(value) != len("sha256:")+64 {
		return false
	}
	for _, r := range value[len("sha256:"):] {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}

func v7FullGateCapabilities(values []string) bool {
	required := map[string]bool{"candidate_read_only": false, "network_denied": false, "control_env_denied": false}
	for _, value := range values {
		if _, ok := required[value]; !ok || required[value] {
			return false
		}
		required[value] = true
	}
	for _, present := range required {
		if !present {
			return false
		}
	}
	return true
}

// readV7TrustedRegularFile binds validation and read to one descriptor. The
// pre-open lstat rejects symlinks; SameFile after open detects replacement;
// the fixed size check rejects a file that changes during the descriptor read.
func readV7TrustedRegularFile(path string, max int64, executable bool) ([]byte, error) {
	before, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !before.Mode().IsRegular() || before.Mode()&0o022 != 0 || executable && before.Mode()&0o111 == 0 {
		return nil, fmt.Errorf("must be a non-group/world-writable regular%s file", map[bool]string{true: " executable", false: ""}[executable])
	}
	if before.Size() < 0 || before.Size() > max {
		return nil, fmt.Errorf("file size exceeds %d-byte bound", max)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	afterOpen, err := f.Stat()
	if err != nil || !os.SameFile(before, afterOpen) || afterOpen.Size() != before.Size() {
		return nil, fmt.Errorf("file identity changed while opening")
	}
	raw, err := io.ReadAll(io.LimitReader(f, max+1))
	if err != nil || int64(len(raw)) != before.Size() || int64(len(raw)) > max {
		return nil, fmt.Errorf("file changed or exceeded bound while reading")
	}
	afterRead, err := f.Stat()
	if err != nil || !os.SameFile(afterOpen, afterRead) || afterRead.Size() != before.Size() {
		return nil, fmt.Errorf("file identity changed while reading")
	}
	return raw, nil
}

func (p *v7ExternalFullGateProvider) verifyIdentity() error {
	_, identity, err := verifyV7TrustedProviderExecutable(p.path)
	if err != nil {
		return err
	}
	if p.executableIdentity != identity {
		return fmt.Errorf("%w: trusted provider executable identity changed after profile resolution", errV7FullGateProvider)
	}
	return nil
}

func (p *v7ExternalFullGateProvider) Run(ctx context.Context, workspace, command string) ([]byte, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := p.verifyIdentity(); err != nil {
		return nil, err
	}
	request, requestPath, err := p.newRequest(workspace, command)
	if err != nil {
		return nil, err
	}
	scope := &v7FullGateProviderScope{request: request, requestPath: requestPath}
	// CommandContext guarantees that a cancelled daemon context cannot leave
	// the wrapper process awaited forever. Cancel asks it to terminate first;
	// WaitDelay then force-reaps it if it ignores SIGTERM.
	cmd := exec.CommandContext(ctx, request.ProviderPath, "--tusker-full-gate-run", requestPath)
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
	cmd.WaitDelay = v7FullGateCleanTimeout
	var providerOutput v7GateBoundedOutput
	providerOutput.max = v7FullGateOutputMaxBytes
	cmd.Env = v7FullGateProviderEnv()
	cmd.Stdout, cmd.Stderr = &providerOutput, &providerOutput
	if err := cmd.Start(); err != nil {
		// Start failed before the provider existed, so no lifecycle can need
		// recovery and retaining this record would be a false fail-closed block.
		_ = p.completeScope(scope)
		return nil, fmt.Errorf("%w: start provider: %v", errV7FullGateProvider, err)
	}
	p.mu.Lock()
	if p.closed || p.active != nil {
		p.mu.Unlock()
		_, cleanupErr := p.cleanup(scope)
		return providerOutput.Bytes(), v7JoinProviderErrors(fmt.Errorf("%w: provider was closed before scope registration", errV7FullGateProvider), cleanupErr)
	}
	p.active = scope
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		if p.active == scope {
			p.active = nil
		}
		p.mu.Unlock()
	}()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case runErr := <-done:
		result, receiptErr := readV7FullGateProviderResult(request)
		if receiptErr != nil {
			_, cleanupErr := p.cleanup(scope)
			return providerOutput.Bytes(), v7JoinProviderErrors(receiptErr, cleanupErr)
		}
		if result.State != "root_exited" && result.State != "cleaned" {
			_, cleanupErr := p.cleanup(scope)
			return providerOutput.Bytes(), v7JoinProviderErrors(fmt.Errorf("%w: provider returned invalid run state %q", errV7FullGateProvider, result.State), cleanupErr)
		}
		cleanupResult, cleanupErr := p.cleanup(scope)
		if cleanupErr != nil {
			return append(providerOutput.Bytes(), []byte(result.Output)...), v7JoinProviderErrors(v7ProviderRunError(runErr), cleanupErr)
		}
		if cleanupResult.LifecycleID != result.LifecycleID {
			return providerOutput.Bytes(), fmt.Errorf("%w: provider changed lifecycle identity between run and cleanup", errV7FullGateProvider)
		}
		p.recordReceipt(cleanupResult)
		if err := p.completeScope(scope); err != nil {
			return providerOutput.Bytes(), err
		}
		if runErr != nil {
			return append(providerOutput.Bytes(), []byte(result.Output)...), v7ProviderRunError(runErr)
		}
		return []byte(result.Output), nil
	case <-ctx.Done():
		// CommandContext's cancel/WaitDelay path terminates and reaps the
		// wrapper. Do not race a second signal/kill here.
		<-done
		_, cleanupErr := p.cleanup(scope)
		if cleanupErr != nil {
			return providerOutput.Bytes(), cleanupErr
		}
		if err := p.completeScope(scope); err != nil {
			return providerOutput.Bytes(), err
		}
		return providerOutput.Bytes(), ctx.Err()
	}
}

func (p *v7ExternalFullGateProvider) Close() error {
	p.mu.Lock()
	p.closed = true
	active := p.active
	p.mu.Unlock()
	if active == nil {
		return nil
	}
	_, err := p.cleanup(active)
	if err != nil {
		return err
	}
	// The active Run still owns process wait and record retirement. Keeping the
	// certified record until it observes cleanup makes Close/restart idempotent
	// instead of racing deletion against a still-running provider wrapper.
	return nil
}

func (p *v7ExternalFullGateProvider) newRequest(workspace, command string) (v7FullGateProviderRequest, string, error) {
	recoveryRoot := p.recoveryRoot
	if strings.TrimSpace(recoveryRoot) == "" {
		recoveryRoot = filepath.Join(DefaultStateRoot(), "full-gate-recovery")
	}
	if err := os.MkdirAll(recoveryRoot, 0o700); err != nil {
		return v7FullGateProviderRequest{}, "", err
	}
	control, err := os.MkdirTemp(recoveryRoot, "scope-")
	if err != nil {
		return v7FullGateProviderRequest{}, "", err
	}
	providerSnapshot := filepath.Join(control, "provider")
	if err := snapshotV7TrustedProvider(p.path, p.executableIdentity, providerSnapshot); err != nil {
		// No provider was launched, so this record is not a recovery scope.
		_ = os.RemoveAll(control)
		return v7FullGateProviderRequest{}, "", err
	}
	workspace, err = sandboxCanonicalPath(workspace)
	if err != nil {
		_ = os.RemoveAll(control)
		return v7FullGateProviderRequest{}, "", err
	}
	request := v7FullGateProviderRequest{
		Schema: v7FullGateProviderSchema, Contract: v7FullGateIsolationContract,
		RunID: strings.ToLower(newRecordID()), Workspace: workspace, Command: command,
		ResultPath: filepath.Join(control, "result.json"), ProviderKind: p.kind, ProviderID: p.identity, ProviderPath: providerSnapshot, ExecutableID: p.executableIdentity,
		CandidateReadOnly: true, NetworkDenied: true, ControlEnvDenied: true,
		RuntimeDigest: p.runtimeDigest, PolicyDigest: p.policyDigest, AttestationDigest: p.attestationDigest, RequiredCapabilities: append([]string(nil), p.capabilities...),
		ImplementationID: p.implementationID, CapabilitySchema: p.capabilitySchema, ExpectedImageOrVMID: p.imageOrVMID,
	}
	request.RequestDigest = v7FullGateRequestDigest(request)
	requestPath := filepath.Join(control, "request.json")
	raw, err := json.Marshal(request)
	if err != nil {
		_ = os.RemoveAll(control)
		return v7FullGateProviderRequest{}, "", err
	}
	if len(raw) > v7FullGateRequestMaxBytes {
		_ = os.RemoveAll(control)
		return v7FullGateProviderRequest{}, "", fmt.Errorf("%w: provider request exceeds size bound", errV7FullGateProvider)
	}
	if err := os.WriteFile(requestPath, append(raw, '\n'), 0o600); err != nil {
		_ = os.RemoveAll(control)
		return v7FullGateProviderRequest{}, "", err
	}
	return request, requestPath, nil
}

func snapshotV7TrustedProvider(source, identity, destination string) error {
	raw, err := readV7TrustedRegularFile(source, 64<<20, true)
	if err != nil {
		return fmt.Errorf("%w: snapshot provider: %v", errV7FullGateProvider, err)
	}
	sum := sha256.Sum256(raw)
	if identity != fmt.Sprintf("sha256:%x", sum[:]) {
		return fmt.Errorf("%w: provider executable changed before immutable snapshot", errV7FullGateProvider)
	}
	if err := os.WriteFile(destination, raw, 0o700); err != nil {
		return err
	}
	_, copiedIdentity, err := verifyV7TrustedProviderExecutable(destination)
	if err != nil || copiedIdentity != identity {
		return fmt.Errorf("%w: immutable provider snapshot identity mismatch", errV7FullGateProvider)
	}
	return nil
}

func (p *v7ExternalFullGateProvider) cleanup(scope *v7FullGateProviderScope) (v7FullGateProviderResult, error) {
	if scope == nil || strings.TrimSpace(scope.requestPath) == "" {
		return v7FullGateProviderResult{}, fmt.Errorf("%w: provider recovery cannot locate its trusted request", errV7FullGateProvider)
	}
	scope.cleanupMu.Lock()
	defer scope.cleanupMu.Unlock()
	if scope.cleanupCalled {
		return scope.cleanupResult, scope.cleanupErr
	}
	result, err := p.cleanupScope(scope)
	scope.cleanupCalled, scope.cleanupResult, scope.cleanupErr = true, result, err
	return result, err
}

func (p *v7ExternalFullGateProvider) cleanupScope(scope *v7FullGateProviderScope) (v7FullGateProviderResult, error) {
	_, identity, err := verifyV7TrustedProviderExecutable(scope.request.ProviderPath)
	if err != nil || identity != scope.request.ExecutableID {
		return v7FullGateProviderResult{}, fmt.Errorf("%w: provider recovery executable identity changed", errV7FullGateProvider)
	}
	ctx, cancel := context.WithTimeout(context.Background(), v7FullGateCleanTimeout)
	defer cancel()
	var output v7GateBoundedOutput
	output.max = v7FullGateOutputMaxBytes
	if err := runV7FullGateProviderCleanup(ctx, scope.request.ProviderPath, scope.requestPath, v7FullGateProviderEnv(), &output); err != nil {
		return v7FullGateProviderResult{}, fmt.Errorf("%w: provider cleanup for run %s failed: %v: %s", errV7FullGateProvider, scope.request.RunID, err, strings.TrimSpace(string(output.Bytes())))
	}
	result, err := readV7FullGateProviderResult(scope.request)
	if err != nil {
		return v7FullGateProviderResult{}, err
	}
	if result.State != "cleaned" {
		return v7FullGateProviderResult{}, fmt.Errorf("%w: provider cleanup for run %s was not certified", errV7FullGateProvider, scope.request.RunID)
	}
	return result, nil
}

func (p *v7ExternalFullGateProvider) completeScope(scope *v7FullGateProviderScope) error {
	if scope == nil || strings.TrimSpace(scope.requestPath) == "" {
		return fmt.Errorf("%w: missing cleanup scope", errV7FullGateProvider)
	}
	if err := os.RemoveAll(filepath.Dir(scope.requestPath)); err != nil {
		return fmt.Errorf("%w: remove certified provider scope: %v", errV7FullGateProvider, err)
	}
	return nil
}

func readV7FullGateProviderResult(request v7FullGateProviderRequest) (v7FullGateProviderResult, error) {
	raw, err := readV7TrustedRegularFile(request.ResultPath, v7FullGateResultMaxBytes, false)
	if err != nil {
		return v7FullGateProviderResult{}, fmt.Errorf("%w: provider did not produce a cleanup receipt: %v", errV7FullGateProvider, err)
	}
	var result v7FullGateProviderResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return v7FullGateProviderResult{}, fmt.Errorf("%w: invalid provider cleanup receipt: %v", errV7FullGateProvider, err)
	}
	if result.Schema != v7FullGateProviderSchema || result.Contract != v7FullGateIsolationContract || result.RunID != request.RunID || result.ProviderID != request.ProviderID || result.RequestDigest != request.RequestDigest || result.RuntimeDigest != request.RuntimeDigest || result.PolicyDigest != request.PolicyDigest || result.AttestationDigest != request.AttestationDigest || result.ImplementationID != request.ImplementationID || result.CapabilitySchema != request.CapabilitySchema || result.ImageOrVMID != request.ExpectedImageOrVMID || !sameDepartureStrings(result.Capabilities, request.RequiredCapabilities) || !result.CandidateReadOnlyMeasured || result.NetworkMode != "none" || !result.ControlEnvAbsent || !result.ControlMountsAbsent || !v7FullGateDigest(result.ImageOrVMID) || strings.TrimSpace(result.LifecycleID) == "" || result.ReceiptDigest != v7FullGateProviderResultDigest(result) {
		return v7FullGateProviderResult{}, fmt.Errorf("%w: provider cleanup receipt does not bind the requested lifecycle scope", errV7FullGateProvider)
	}
	return result, nil
}

func v7FullGateProviderEnv() []string {
	// A trusted provider receives only the data in its request. In particular,
	// it cannot accidentally inherit the daemon control socket, state root,
	// signing agent, or user-defined loader/path hooks.
	return []string{"PATH=/usr/bin:/bin:/usr/sbin:/sbin", "LANG=C", "LC_ALL=C", "HOME=/var/empty", "TMPDIR=/tmp"}
}

func v7ProviderRunError(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%w: provider run: %w", errV7FullGateProvider, err)
}

func v7JoinProviderErrors(primary, cleanup error) error {
	if primary == nil {
		return cleanup
	}
	if cleanup == nil {
		return primary
	}
	return errors.Join(primary, cleanup)
}

func v7FullGateRequestDigest(request v7FullGateProviderRequest) string {
	parts := append([]string{request.Schema, request.Contract, request.RunID, request.Workspace, request.Command, request.ResultPath, request.ProviderKind, request.ProviderID, request.ProviderPath, request.ExecutableID, fmt.Sprint(request.CandidateReadOnly), fmt.Sprint(request.NetworkDenied), fmt.Sprint(request.ControlEnvDenied), request.RuntimeDigest, request.PolicyDigest, request.AttestationDigest, request.ImplementationID, request.CapabilitySchema, request.ExpectedImageOrVMID}, request.RequiredCapabilities...)
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return fmt.Sprintf("sha256:%x", sum[:])
}

func v7FullGateProviderResultDigest(result v7FullGateProviderResult) string {
	parts := append([]string{result.Schema, result.Contract, result.RunID, result.LifecycleID, result.State, result.Output, result.Error, result.ProviderID, result.RequestDigest, result.RuntimeDigest, result.PolicyDigest, result.AttestationDigest, result.ImplementationID, result.CapabilitySchema, fmt.Sprint(result.CandidateReadOnlyMeasured), result.NetworkMode, fmt.Sprint(result.ControlEnvAbsent), fmt.Sprint(result.ControlMountsAbsent), result.ImageOrVMID}, result.Capabilities...)
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return fmt.Sprintf("sha256:%x", sum[:])
}

// recoverV7FullGateProviderScopes is run before a daemon accepts work. A
// crash can interrupt the parent between provider start and cleanup; durable
// request records let the next daemon invoke the provider's exact cleanup
// operation rather than inferring safety from a dead root PID.
func recoverV7FullGateProviderScopes(stateRoot string) error {
	root := filepath.Join(stateRoot, "full-gate-recovery")
	entries, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("%w: read provider recovery records: %v", errV7FullGateProvider, err)
	}
	if len(entries) > v7FullGateRecoveryMaxScopes {
		return fmt.Errorf("%w: provider recovery scope count exceeds %d", errV7FullGateProvider, v7FullGateRecoveryMaxScopes)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			return fmt.Errorf("%w: invalid provider recovery entry %q", errV7FullGateProvider, entry.Name())
		}
		dir := filepath.Join(root, entry.Name())
		requestPath := filepath.Join(dir, "request.json")
		raw, readErr := readV7TrustedRegularFile(requestPath, v7FullGateRequestMaxBytes, false)
		if readErr != nil {
			return fmt.Errorf("%w: read provider recovery request: %v", errV7FullGateProvider, readErr)
		}
		var request v7FullGateProviderRequest
		if err := json.Unmarshal(raw, &request); err != nil || request.RequestDigest != v7FullGateRequestDigest(request) || request.Schema != v7FullGateProviderSchema || request.Contract != v7FullGateIsolationContract {
			return fmt.Errorf("%w: invalid provider recovery request %q", errV7FullGateProvider, entry.Name())
		}
		_, executableID, verifyErr := verifyV7TrustedProviderExecutable(request.ProviderPath)
		if verifyErr != nil || executableID != request.ExecutableID {
			return fmt.Errorf("%w: provider recovery executable identity is unavailable or changed for %q", errV7FullGateProvider, entry.Name())
		}
		provider := &v7ExternalFullGateProvider{path: request.ProviderPath, executableIdentity: request.ExecutableID}
		scope := &v7FullGateProviderScope{request: request, requestPath: requestPath}
		if _, err := provider.cleanup(scope); err != nil {
			return err
		}
		if err := provider.completeScope(scope); err != nil {
			return err
		}
	}
	return nil
}

func v7PathWithin(root, path string) bool {
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		return false
	}
	path, err = filepath.EvalSymlinks(path)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
