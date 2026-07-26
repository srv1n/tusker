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
	v7FullGateCommandMaxBytes   = 16 << 10
	v7FullGateRuntimeMax        = 2 * time.Hour
	v7FullGateArtifactMaxBytes  = 16 << 20
	v7FullGateRecoveryMaxScopes = 128
	v7FullGateCloseWaitTimeout  = 2 * v7FullGateCleanTimeout
)

// Kept as a variable solely so the deterministic fake-provider deadline test
// can use milliseconds; production never configures this from repository data.
var v7FullGateRuntimeLimit = v7FullGateRuntimeMax

var errV7FullGateProvider = errors.New("full-gate lifecycle provider")

// v7FullGateOutcome is deliberately provider-neutral. A failed repository
// command is not a broken provider, and neither cancellation nor a timeout may
// quietly become a green reusable result.
type v7FullGateOutcome string

const (
	v7FullGateOutcomePassed   v7FullGateOutcome = "gate_passed"
	v7FullGateOutcomeFailed   v7FullGateOutcome = "gate_failed"
	v7FullGateOutcomeProvider v7FullGateOutcome = "provider_failed"
	v7FullGateOutcomeCanceled v7FullGateOutcome = "cancelled"
	v7FullGateOutcomeTimedOut v7FullGateOutcome = "timed_out"
)

func (outcome v7FullGateOutcome) valid() bool {
	switch outcome {
	case v7FullGateOutcomePassed, v7FullGateOutcomeFailed, v7FullGateOutcomeProvider, v7FullGateOutcomeCanceled, v7FullGateOutcomeTimedOut:
		return true
	default:
		return false
	}
}

type v7FullGateOutcomeError struct {
	Outcome v7FullGateOutcome
	Cause   error
}

func (e *v7FullGateOutcomeError) Error() string {
	if e == nil || e.Cause == nil {
		return string(e.Outcome)
	}
	return string(e.Outcome) + ": " + e.Cause.Error()
}

func (e *v7FullGateOutcomeError) Unwrap() error { return e.Cause }

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
	ProjectID            string   `json:"project_id"`
	DepartureID          string   `json:"departure_id"`
	CandidateDigest      string   `json:"candidate_digest"`
	Profile              string   `json:"profile"`
	ProviderProfile      string   `json:"provider_profile"`
	Toolchain            string   `json:"toolchain"`
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
	ClientDigest         string   `json:"client_digest"`
	PolicyDigest         string   `json:"policy_digest"`
	AttestationDigest    string   `json:"attestation_digest"`
	RequiredCapabilities []string `json:"required_capabilities"`
	ImplementationID     string   `json:"implementation_id"`
	CapabilitySchema     string   `json:"capability_schema"`
	ExpectedImageOrVMID  string   `json:"expected_image_or_vm_id"`
	MaxCommandBytes      int      `json:"max_command_bytes"`
	MaxOutputBytes       int      `json:"max_output_bytes"`
	MaxRuntimeMS         int64    `json:"max_runtime_ms"`
	MaxArtifactBytes     int64    `json:"max_artifact_bytes"`
}

// v7FullGateProviderResult is a receipt from the provider, not a hint. The
// provider may return only after every process in its container/VM scope has
// stopped; lifecycle_id ties both normal completion and recovery to that
// immutable provider-side scope.
type v7FullGateProviderResult struct {
	Schema                    string            `json:"schema"`
	Contract                  string            `json:"contract"`
	RunID                     string            `json:"run_id"`
	LifecycleID               string            `json:"lifecycle_id"`
	State                     string            `json:"state"`
	Outcome                   v7FullGateOutcome `json:"outcome"`
	Output                    string            `json:"output,omitempty"`
	Error                     string            `json:"error,omitempty"`
	ProviderID                string            `json:"provider_id"`
	RequestDigest             string            `json:"request_digest"`
	RuntimeDigest             string            `json:"runtime_digest"`
	PolicyDigest              string            `json:"policy_digest"`
	AttestationDigest         string            `json:"attestation_digest"`
	Capabilities              []string          `json:"capabilities"`
	ReceiptDigest             string            `json:"receipt_digest"`
	ImplementationID          string            `json:"implementation_id"`
	CapabilitySchema          string            `json:"capability_schema"`
	CandidateReadOnlyMeasured bool              `json:"candidate_read_only_measured"`
	NetworkMode               string            `json:"network_mode"`
	ControlEnvAbsent          bool              `json:"control_env_absent"`
	ControlMountsAbsent       bool              `json:"control_mounts_absent"`
	ImageOrVMID               string            `json:"image_or_vm_id"`
	RuntimeMS                 int64             `json:"runtime_ms"`
	ArtifactBytes             int64             `json:"artifact_bytes"`
	OutputDigest              string            `json:"output_digest"`
	ResultDigest              string            `json:"result_digest"`
}

type v7FullGateProvider interface {
	Run(context.Context, string, string) ([]byte, error)
	Close() error
}

type v7GateBoundedOutput struct {
	b                bytes.Buffer
	max              int
	cutoff           bool
	truncationNotice string
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
		notice := b.truncationNotice
		if notice == "" {
			notice = "\n[provider output truncated]\n"
		}
		return append(append([]byte(nil), b.b.Bytes()...), []byte(notice)...)
	}
	return append([]byte(nil), b.b.Bytes()...)
}

type v7TrustedFullGateProvider struct {
	Kind              string   `yaml:"kind"`
	Command           string   `yaml:"command"`
	Version           string   `yaml:"version"`
	RuntimeDigest     string   `yaml:"runtime_digest"`
	ClientDigest      string   `yaml:"client_digest"`
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
	return &v7ExternalFullGateProvider{path: path, kind: trusted.Kind, identity: identity, executableIdentity: executableIdentity, runtimeDigest: trusted.RuntimeDigest, clientDigest: trusted.ClientDigest, policyDigest: trusted.PolicyDigest, attestationDigest: trusted.AttestationDigest, capabilities: append([]string(nil), trusted.Capabilities...), implementationID: trusted.ImplementationID, capabilitySchema: trusted.CapabilitySchema, imageOrVMID: trusted.ImageOrVMID, profile: profile, recoveryRoot: filepath.Join(stateRoot, "full-gate-recovery")}, nil
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
	clientDigest       string
	profile            string
	binding            v7FullGateProviderBinding
	recoveryRoot       string
	mu                 sync.Mutex
	active             *v7FullGateProviderScope
	pending            map[string]*v7FullGateProviderScope
	lastReceipt        v7FullGateProviderAudit
	closed             bool
}

type v7FullGateProviderScope struct {
	request     v7FullGateProviderRequest
	requestPath string
	// runCancel and wrapperDone are installed before the wrapper is started.
	// Close therefore either observes no wrapper at all or can cancel and wait
	// for the exact wrapper which owns this durable scope.
	runCancel   context.CancelFunc
	wrapperDone chan struct{}
	wrapperMu   sync.Mutex
	wrapperErr  error
	// Close and Run can race during daemon cancellation. Cache one cleanup
	// result for this immutable scope so the provider receives exactly one
	// lifecycle-destruction command; a later daemon recovery owns retries.
	cleanupMu     sync.Mutex
	cleanupCalled bool
	cleanupResult v7FullGateProviderResult
	cleanupErr    error
}

// v7FullGateProviderBinding is supplied by the trusted promotion boundary,
// never repository configuration. It makes the receipt useful for replay
// without allowing a backend to invent project or candidate identity.
type v7FullGateProviderBinding struct {
	ProjectID       string
	DepartureID     string
	CandidateDigest string
	GateProfile     string
	ProviderProfile string
	Toolchain       string
}

type v7FullGateProviderBinder interface {
	BindFullGateProvider(v7FullGateProviderBinding) error
}

// A provider scope is retired only after the caller has durably recorded the
// corresponding receipt. This separates lifecycle certification from proof
// persistence and closes the cleanup-to-ledger crash window.
type v7FullGateProviderFinalizer interface {
	FinalizeFullGateProviderOutcome(GateProviderReceipt) error
}

type v7FullGateDepartureContextKey struct{}

func withV7FullGateDeparture(ctx context.Context, departureID string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, v7FullGateDepartureContextKey{}, strings.TrimSpace(departureID))
}

func v7FullGateDepartureID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	value, _ := ctx.Value(v7FullGateDepartureContextKey{}).(string)
	return strings.TrimSpace(value)
}

func (s *v7FullGateProviderScope) finishWrapper(err error) {
	if s == nil || s.wrapperDone == nil {
		return
	}
	s.wrapperMu.Lock()
	s.wrapperErr = err
	close(s.wrapperDone)
	s.wrapperMu.Unlock()
}

func (s *v7FullGateProviderScope) waitWrapper(timeout time.Duration) (error, error) {
	if s == nil || s.wrapperDone == nil {
		return nil, nil
	}
	if timeout <= 0 {
		<-s.wrapperDone
	} else {
		select {
		case <-s.wrapperDone:
		case <-time.After(timeout):
			return nil, fmt.Errorf("%w: provider wrapper did not exit within %s", errV7FullGateProvider, timeout)
		}
	}
	s.wrapperMu.Lock()
	defer s.wrapperMu.Unlock()
	return s.wrapperErr, nil
}

type v7FullGateProviderAudit struct {
	Receipt           GateProviderReceipt
	Outcome           v7FullGateOutcome
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
	return v7CertifiedGateProviderReceipt(receipt) && receipt.ProviderDigest == p.executableIdentity && receipt.ClientDigest == p.clientDigest && receipt.RuntimeDigest == p.runtimeDigest && receipt.PolicyDigest == p.policyDigest && receipt.AttestationDigest == p.attestationDigest && receipt.ImageOrVMID == p.imageOrVMID && receipt.ProviderProfile == p.profile
}

func (p *v7ExternalFullGateProvider) BindFullGateProvider(binding v7FullGateProviderBinding) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.active != nil || p.closed {
		return fmt.Errorf("%w: cannot bind an active or closed provider", errV7FullGateProvider)
	}
	if strings.TrimSpace(binding.ProjectID) == "" || strings.TrimSpace(binding.DepartureID) == "" || strings.TrimSpace(binding.CandidateDigest) == "" || strings.TrimSpace(binding.GateProfile) == "" || strings.TrimSpace(binding.ProviderProfile) == "" || strings.TrimSpace(binding.Toolchain) == "" || binding.ProviderProfile != p.profile {
		return fmt.Errorf("%w: full-gate binding lacks the current project, departure, candidate, gate/provider profile, or toolchain identity", errV7FullGateProvider)
	}
	p.binding = binding
	return nil
}

func (p *v7ExternalFullGateProvider) recordReceipt(scope *v7FullGateProviderScope, request v7FullGateProviderRequest, result v7FullGateProviderResult) {
	receipt := GateProviderReceipt{
		Schema: v7FullGateProviderSchema, Outcome: string(result.Outcome), ProjectID: request.ProjectID, DepartureID: request.DepartureID,
		RequestDigest: request.RequestDigest, CandidateDigest: request.CandidateDigest, CommandDigest: v7FullGateTextDigest(request.Command), Profile: request.Profile, ProviderProfile: request.ProviderProfile, Toolchain: request.Toolchain,
		ProviderDigest: request.ExecutableID, ClientDigest: request.ClientDigest, LifecycleID: result.LifecycleID, ReceiptDigest: result.ReceiptDigest,
		RuntimeDigest: result.RuntimeDigest, PolicyDigest: result.PolicyDigest, AttestationDigest: result.AttestationDigest, ImageOrVMID: result.ImageOrVMID,
		CapabilitiesDigest: v7FullGateStringsDigest(result.Capabilities), ContainmentDigest: v7FullGateContainmentDigest(result), CleanupDigest: v7FullGateCleanupDigest(result), ResultDigest: result.ResultDigest, OutputDigest: result.OutputDigest, CleanupCertified: result.State == "cleaned",
	}
	p.mu.Lock()
	p.lastReceipt = v7FullGateProviderAudit{Receipt: receipt, Outcome: result.Outcome, LifecycleID: result.LifecycleID, ReceiptDigest: result.ReceiptDigest, RuntimeDigest: result.RuntimeDigest, PolicyDigest: result.PolicyDigest, AttestationDigest: result.AttestationDigest, ImageOrVMID: result.ImageOrVMID}
	if scope != nil {
		if p.pending == nil {
			p.pending = make(map[string]*v7FullGateProviderScope)
		}
		p.pending[request.RequestDigest] = scope
	}
	p.mu.Unlock()
}

func (p *v7ExternalFullGateProvider) FinalizeFullGateProviderOutcome(receipt GateProviderReceipt) error {
	p.mu.Lock()
	scope := p.pending[receipt.RequestDigest]
	if scope == nil || scope.request.RequestDigest != receipt.RequestDigest {
		p.mu.Unlock()
		return fmt.Errorf("%w: provider receipt is not awaiting persistence acknowledgement", errV7FullGateProvider)
	}
	delete(p.pending, receipt.RequestDigest)
	p.mu.Unlock()
	if err := p.completeScope(scope); err != nil {
		p.mu.Lock()
		if p.pending == nil {
			p.pending = make(map[string]*v7FullGateProviderScope)
		}
		p.pending[receipt.RequestDigest] = scope
		p.mu.Unlock()
		return err
	}
	return nil
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
	if registry.Schema != v7FullGateProviderSchema || !ok || trusted.Kind != "container" && trusted.Kind != "vm" || strings.TrimSpace(trusted.Version) == "" || trusted.ImplementationID != v7KnownFullGateProvider || trusted.CapabilitySchema != v7FullGateCapabilitySchema || !v7FullGateDigest(trusted.RuntimeDigest) || !v7FullGateDigest(trusted.ClientDigest) || !v7FullGateDigest(trusted.PolicyDigest) || !v7FullGateDigest(trusted.AttestationDigest) || !v7FullGateDigest(trusted.ImageOrVMID) || !v7FullGateCapabilities(trusted.Capabilities) {
		return v7TrustedFullGateProvider{}, "", "", "", fmt.Errorf("%w: trusted provider profile %q is not a valid container/VM lifecycle provider", errV7FullGateProvider, profile)
	}
	path, identity, err := verifyV7TrustedProviderExecutable(trusted.Command)
	if err != nil {
		return v7TrustedFullGateProvider{}, "", "", "", err
	}
	return trusted, path, departureFingerprint(append([]string{profile, trusted.Kind, trusted.Version, trusted.ImplementationID, trusted.CapabilitySchema, identity, trusted.ClientDigest, trusted.RuntimeDigest, trusted.PolicyDigest, trusted.AttestationDigest, trusted.ImageOrVMID}, trusted.Capabilities...)...), identity, nil
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
	runCtx, runCancel := v7FullGateProviderDeadline(ctx, v7FullGateRuntimeLimit)
	defer runCancel()
	scope := &v7FullGateProviderScope{request: request, requestPath: requestPath, runCancel: runCancel, wrapperDone: make(chan struct{})}
	started := time.Now()
	// CommandContext guarantees that a cancelled daemon context cannot leave
	// the wrapper process awaited forever. Cancel asks it to terminate first;
	// WaitDelay then force-reaps it if it ignores SIGTERM.
	cmd := exec.CommandContext(runCtx, request.ProviderPath, "--tusker-full-gate-run", requestPath)
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
	cmd.WaitDelay = v7FullGateCleanTimeout
	var providerOutput v7GateBoundedOutput
	providerOutput.max = v7FullGateOutputMaxBytes
	cmd.Env = v7FullGateProviderEnv()
	cmd.Stdout, cmd.Stderr = &providerOutput, &providerOutput
	// Hold the provider lock across Start and active-scope publication. Close can
	// no longer observe the old "wrapper exists but active is nil" gap.
	p.mu.Lock()
	if p.closed || p.active != nil {
		p.mu.Unlock()
		_ = p.completeScope(scope)
		return nil, fmt.Errorf("%w: provider is closed or already owns an active scope", errV7FullGateProvider)
	}
	if err := cmd.Start(); err != nil {
		p.mu.Unlock()
		// Start failed before the provider existed, so no lifecycle can need
		// recovery and retaining this record would be a false fail-closed block.
		_ = p.completeScope(scope)
		return nil, fmt.Errorf("%w: start provider: %v", errV7FullGateProvider, err)
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
	go func() { scope.finishWrapper(cmd.Wait()) }()
	select {
	case <-scope.wrapperDone:
		runErr, waitErr := scope.waitWrapper(0)
		if waitErr != nil {
			return providerOutput.Bytes(), waitErr
		}
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
		if time.Since(started) > time.Duration(request.MaxRuntimeMS)*time.Millisecond {
			return []byte(cleanupResult.Output), fmt.Errorf("%w: daemon-measured provider runtime exceeded %s", errV7FullGateProvider, time.Duration(request.MaxRuntimeMS)*time.Millisecond)
		}
		p.recordReceipt(scope, request, cleanupResult)
		if cleanupResult.Outcome == v7FullGateOutcomePassed && runErr == nil {
			return []byte(cleanupResult.Output), nil
		}
		return []byte(cleanupResult.Output), v7FullGateResultError(cleanupResult, runErr, nil)
	case <-runCtx.Done():
		// CommandContext's cancel/WaitDelay path terminates and reaps the
		// wrapper. Do not race a second signal/kill here. The derived context
		// makes the same guarantee when Close races cancellation.
		runCancel()
		if _, waitErr := scope.waitWrapper(v7FullGateCloseWaitTimeout); waitErr != nil {
			return providerOutput.Bytes(), waitErr
		}
		cleanupResult, cleanupErr := p.cleanup(scope)
		if cleanupErr != nil {
			return providerOutput.Bytes(), cleanupErr
		}
		if cleanupResult.Outcome != v7FullGateOutcomeCanceled && cleanupResult.Outcome != v7FullGateOutcomeTimedOut {
			return []byte(cleanupResult.Output), fmt.Errorf("%w: provider did not certify cancellation or timeout cleanup", errV7FullGateProvider)
		}
		p.recordReceipt(scope, request, cleanupResult)
		return []byte(cleanupResult.Output), v7FullGateResultError(cleanupResult, nil, runCtx.Err())
	}
}

func v7FullGateProviderDeadline(parent context.Context, limit time.Duration) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	if limit <= 0 || limit > v7FullGateRuntimeMax {
		limit = v7FullGateRuntimeMax
	}
	return context.WithTimeout(parent, limit)
}

func (p *v7ExternalFullGateProvider) Close() error {
	p.mu.Lock()
	p.closed = true
	active := p.active
	p.mu.Unlock()
	if active == nil {
		return nil
	}
	if active.runCancel != nil {
		active.runCancel()
	}
	if _, err := active.waitWrapper(v7FullGateCloseWaitTimeout); err != nil {
		return err
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
	if len(command) == 0 || len(command) > v7FullGateCommandMaxBytes {
		return v7FullGateProviderRequest{}, "", fmt.Errorf("%w: provider command exceeds %d-byte bound", errV7FullGateProvider, v7FullGateCommandMaxBytes)
	}
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
	p.mu.Lock()
	binding := p.binding
	p.mu.Unlock()
	if strings.TrimSpace(binding.ProjectID) == "" || strings.TrimSpace(binding.DepartureID) == "" || strings.TrimSpace(binding.CandidateDigest) == "" || strings.TrimSpace(binding.GateProfile) == "" || strings.TrimSpace(binding.ProviderProfile) == "" || strings.TrimSpace(binding.Toolchain) == "" {
		_ = os.RemoveAll(control)
		return v7FullGateProviderRequest{}, "", fmt.Errorf("%w: provider request is not bound to the current full-gate contract", errV7FullGateProvider)
	}
	request := v7FullGateProviderRequest{
		Schema: v7FullGateProviderSchema, Contract: v7FullGateIsolationContract,
		RunID: strings.ToLower(newRecordID()), Workspace: workspace, Command: command, ProjectID: binding.ProjectID, DepartureID: binding.DepartureID, CandidateDigest: binding.CandidateDigest, Profile: binding.GateProfile, ProviderProfile: binding.ProviderProfile, Toolchain: binding.Toolchain,
		ResultPath: filepath.Join(control, "result.json"), ProviderKind: p.kind, ProviderID: p.identity, ProviderPath: providerSnapshot, ExecutableID: p.executableIdentity,
		CandidateReadOnly: true, NetworkDenied: true, ControlEnvDenied: true,
		RuntimeDigest: p.runtimeDigest, ClientDigest: p.clientDigest, PolicyDigest: p.policyDigest, AttestationDigest: p.attestationDigest, RequiredCapabilities: append([]string(nil), p.capabilities...),
		ImplementationID: p.implementationID, CapabilitySchema: p.capabilitySchema, ExpectedImageOrVMID: p.imageOrVMID,
		MaxCommandBytes: v7FullGateCommandMaxBytes, MaxOutputBytes: v7FullGateOutputMaxBytes, MaxRuntimeMS: v7FullGateRuntimeLimit.Milliseconds(), MaxArtifactBytes: v7FullGateArtifactMaxBytes,
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
	if err := writeV7FullGateReservation(requestPath, append(raw, '\n')); err != nil {
		_ = os.RemoveAll(control)
		return v7FullGateProviderRequest{}, "", err
	}
	return request, requestPath, nil
}

// writeV7FullGateReservation makes the recovery record durable before a
// provider can create its scope. A crash after this returns is recoverable; a
// crash before it returns has not been granted create authority.
func writeV7FullGateReservation(path string, raw []byte) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(raw); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
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
	if result.Schema != v7FullGateProviderSchema || result.Contract != v7FullGateIsolationContract || result.RunID != request.RunID || result.ProviderID != request.ProviderID || result.RequestDigest != request.RequestDigest || result.RuntimeDigest != request.RuntimeDigest || result.PolicyDigest != request.PolicyDigest || result.AttestationDigest != request.AttestationDigest || result.ImplementationID != request.ImplementationID || result.CapabilitySchema != request.CapabilitySchema || result.ImageOrVMID != request.ExpectedImageOrVMID || !sameDepartureStrings(result.Capabilities, request.RequiredCapabilities) || !result.CandidateReadOnlyMeasured || result.NetworkMode != "none" || !result.ControlEnvAbsent || !result.ControlMountsAbsent || !v7FullGateDigest(result.ImageOrVMID) || strings.TrimSpace(result.LifecycleID) == "" || !result.Outcome.valid() || len(result.Output) > request.MaxOutputBytes || result.RuntimeMS < 0 || result.RuntimeMS > request.MaxRuntimeMS || result.ArtifactBytes < 0 || result.ArtifactBytes > request.MaxArtifactBytes || result.OutputDigest != v7FullGateTextDigest(result.Output) || result.ResultDigest != v7FullGateProviderResultDigest(result) || result.ReceiptDigest != v7FullGateReceiptDigest(request, result) {
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

func v7FullGateResultError(result v7FullGateProviderResult, runErr, cause error) error {
	if result.Outcome == v7FullGateOutcomePassed {
		return fmt.Errorf("%w: provider wrapper failed after a claimed gate pass: %v", errV7FullGateProvider, runErr)
	}
	if result.Outcome == v7FullGateOutcomeProvider {
		return fmt.Errorf("%w: provider reported failure: %s", errV7FullGateProvider, strings.TrimSpace(result.Error))
	}
	if cause != nil {
		return &v7FullGateOutcomeError{Outcome: result.Outcome, Cause: cause}
	}
	if runErr != nil {
		return &v7FullGateOutcomeError{Outcome: result.Outcome, Cause: runErr}
	}
	return &v7FullGateOutcomeError{Outcome: result.Outcome, Cause: errors.New(strings.TrimSpace(result.Error))}
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
	parts := append([]string{request.Schema, request.Contract, request.RunID, request.Workspace, request.Command, request.ProjectID, request.DepartureID, request.CandidateDigest, request.Profile, request.ProviderProfile, request.Toolchain, request.ResultPath, request.ProviderKind, request.ProviderID, request.ProviderPath, request.ExecutableID, fmt.Sprint(request.CandidateReadOnly), fmt.Sprint(request.NetworkDenied), fmt.Sprint(request.ControlEnvDenied), request.RuntimeDigest, request.ClientDigest, request.PolicyDigest, request.AttestationDigest, request.ImplementationID, request.CapabilitySchema, request.ExpectedImageOrVMID, fmt.Sprint(request.MaxCommandBytes), fmt.Sprint(request.MaxOutputBytes), fmt.Sprint(request.MaxRuntimeMS), fmt.Sprint(request.MaxArtifactBytes)}, request.RequiredCapabilities...)
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return fmt.Sprintf("sha256:%x", sum[:])
}

func v7FullGateProviderResultDigest(result v7FullGateProviderResult) string {
	parts := append([]string{result.Schema, result.Contract, result.RunID, result.LifecycleID, result.State, string(result.Outcome), result.Output, result.Error, result.ProviderID, result.RequestDigest, result.RuntimeDigest, result.PolicyDigest, result.AttestationDigest, result.ImplementationID, result.CapabilitySchema, fmt.Sprint(result.CandidateReadOnlyMeasured), result.NetworkMode, fmt.Sprint(result.ControlEnvAbsent), fmt.Sprint(result.ControlMountsAbsent), result.ImageOrVMID, fmt.Sprint(result.RuntimeMS), fmt.Sprint(result.ArtifactBytes), result.OutputDigest}, result.Capabilities...)
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return fmt.Sprintf("sha256:%x", sum[:])
}

// v7SealFullGateProviderResult is the canonical receipt finalizer for backend
// adapters. Backends fill measured facts first; this function makes the output,
// result, and request-bound receipt digests agree without trusting transport
// formatting or caller-provided digest strings.
func v7SealFullGateProviderResult(request v7FullGateProviderRequest, result *v7FullGateProviderResult) {
	if result == nil {
		return
	}
	result.OutputDigest = v7FullGateTextDigest(result.Output)
	result.ResultDigest = v7FullGateProviderResultDigest(*result)
	result.ReceiptDigest = v7FullGateReceiptDigest(request, *result)
}

func v7FullGateReceiptDigest(request v7FullGateProviderRequest, result v7FullGateProviderResult) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{request.RequestDigest, result.ResultDigest, result.LifecycleID, result.State, string(result.Outcome), v7FullGateCleanupDigest(result)}, "\x00")))
	return fmt.Sprintf("sha256:%x", sum[:])
}

func v7FullGateTextDigest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return fmt.Sprintf("sha256:%x", sum[:])
}

func v7FullGateStringsDigest(values []string) string {
	return v7FullGateTextDigest(strings.Join(values, "\x00"))
}

func v7FullGateContainmentDigest(result v7FullGateProviderResult) string {
	return v7FullGateTextDigest(strings.Join([]string{fmt.Sprint(result.CandidateReadOnlyMeasured), result.NetworkMode, fmt.Sprint(result.ControlEnvAbsent), fmt.Sprint(result.ControlMountsAbsent)}, "\x00"))
}

func v7FullGateCleanupDigest(result v7FullGateProviderResult) string {
	return v7FullGateTextDigest(strings.Join([]string{result.LifecycleID, result.State, string(result.Outcome), fmt.Sprint(result.RuntimeMS), fmt.Sprint(result.ArtifactBytes)}, "\x00"))
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
		if result, resultErr := readV7FullGateProviderResult(request); resultErr == nil && result.State == "cleaned" {
			return fmt.Errorf("%w: certified provider scope %q awaits durable outcome acknowledgement (%s)", errV7FullGateProvider, entry.Name(), result.Outcome)
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
		return fmt.Errorf("%w: recovered provider scope %q awaits durable outcome acknowledgement", errV7FullGateProvider, entry.Name())
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
