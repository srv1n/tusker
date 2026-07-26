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
	"sort"
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
	v7FullGateJournalMaxEntries = 128
	v7FullGateCloseWaitTimeout  = 2 * v7FullGateCleanTimeout
)

// Kept as a variable solely so the deterministic fake-provider deadline test
// can use milliseconds; production never configures this from repository data.
var v7FullGateRuntimeLimit = v7FullGateRuntimeMax

// Deterministic crash/durability seams. Production leaves both nil.
var (
	v7FullGateDurabilityHook func(string) error
	v7FullGateRecoveryHook   func(string) error
)

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

type v7FullGateProviderInvocation struct {
	Output  []byte
	Outcome v7FullGateOutcome
	Receipt GateProviderReceipt
}

type v7FullGateProvider interface {
	Run(context.Context, string, string) (v7FullGateProviderInvocation, error)
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
	return &v7ExternalFullGateProvider{path: path, kind: trusted.Kind, identity: identity, executableIdentity: executableIdentity, runtimeDigest: trusted.RuntimeDigest, clientDigest: trusted.ClientDigest, policyDigest: trusted.PolicyDigest, attestationDigest: trusted.AttestationDigest, capabilities: append([]string(nil), trusted.Capabilities...), implementationID: trusted.ImplementationID, capabilitySchema: trusted.CapabilitySchema, imageOrVMID: trusted.ImageOrVMID, profile: profile, stateRoot: stateRoot, recoveryRoot: filepath.Join(stateRoot, "full-gate-recovery")}, nil
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
	stateRoot          string
	binding            v7FullGateProviderBinding
	recoveryRoot       string
	mu                 sync.Mutex
	active             *v7FullGateProviderScope
	pending            map[string]*v7FullGateProviderScope
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

const v7FullGateOutcomeJournalSchema = "tusker.full-gate-provider-outcome/v1"

type v7FullGateProviderOutcomeJournal struct {
	Schema        string                    `json:"schema"`
	DepartureID   string                    `json:"departure_id"`
	RequestDigest string                    `json:"request_digest"`
	ScopePath     string                    `json:"scope_path"`
	Request       v7FullGateProviderRequest `json:"request"`
	Result        v7FullGateProviderResult  `json:"result"`
	Receipt       GateProviderReceipt       `json:"receipt"`
	Reconciled    bool                      `json:"reconciled,omitempty"`
	Action        string                    `json:"action,omitempty"`
	JournalDigest string                    `json:"journal_digest"`
}

// MatchesGateProviderReceipt binds ledger reuse to this provider's currently
// trusted immutable profile. A syntactically valid receipt from another
// runtime, policy, attestation, or image is not promotion proof here.
func (p *v7ExternalFullGateProvider) MatchesGateProviderReceipt(receipt *GateProviderReceipt) bool {
	return v7CertifiedGateProviderReceipt(receipt) && receipt.ProviderDigest == p.executableIdentity && receipt.ProviderClosureDigest == p.identity && receipt.ClientDigest == p.clientDigest && receipt.RuntimeDigest == p.runtimeDigest && receipt.PolicyDigest == p.policyDigest && receipt.AttestationDigest == p.attestationDigest && receipt.ImageOrVMID == p.imageOrVMID && receipt.ProviderProfile == p.profile
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

func (p *v7ExternalFullGateProvider) recordReceipt(scope *v7FullGateProviderScope, request v7FullGateProviderRequest, result v7FullGateProviderResult) (GateProviderReceipt, error) {
	receipt := v7GateProviderReceiptForResult(request, result)
	if scope != nil {
		if err := persistV7FullGateProviderOutcomeJournal(p.stateRoot, scope, request, result, receipt); err != nil {
			return receipt, err
		}
	}
	p.mu.Lock()
	if scope != nil {
		if p.pending == nil {
			p.pending = make(map[string]*v7FullGateProviderScope)
		}
		p.pending[request.RequestDigest] = scope
	}
	p.mu.Unlock()
	return receipt, nil
}

func v7GateProviderReceiptForResult(request v7FullGateProviderRequest, result v7FullGateProviderResult) GateProviderReceipt {
	return GateProviderReceipt{
		Schema: v7FullGateProviderSchema, Outcome: string(result.Outcome), ProjectID: request.ProjectID, DepartureID: request.DepartureID,
		RequestDigest: request.RequestDigest, CandidateDigest: request.CandidateDigest, CommandDigest: v7FullGateTextDigest(request.Command), Profile: request.Profile, ProviderProfile: request.ProviderProfile, Toolchain: request.Toolchain,
		ProviderDigest: request.ExecutableID, ProviderClosureDigest: request.ProviderID, ClientDigest: request.ClientDigest, LifecycleID: result.LifecycleID, ReceiptDigest: result.ReceiptDigest,
		RuntimeDigest: result.RuntimeDigest, PolicyDigest: result.PolicyDigest, AttestationDigest: result.AttestationDigest, ImageOrVMID: result.ImageOrVMID,
		CapabilitiesDigest: v7FullGateStringsDigest(result.Capabilities), ContainmentDigest: v7FullGateContainmentDigest(result), CleanupDigest: v7FullGateCleanupDigest(result), ResultDigest: result.ResultDigest, OutputDigest: result.OutputDigest, CleanupCertified: result.State == "cleaned",
	}
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
	if err := removeV7FullGateProviderOutcomeJournal(p.stateRoot, receipt); err != nil {
		p.mu.Lock()
		if p.pending == nil {
			p.pending = make(map[string]*v7FullGateProviderScope)
		}
		p.pending[receipt.RequestDigest] = scope
		p.mu.Unlock()
		return err
	}
	return runV7FullGateDurabilityHook("outcome_retired")
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

func (p *v7ExternalFullGateProvider) Run(ctx context.Context, workspace, command string) (v7FullGateProviderInvocation, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := p.verifyIdentity(); err != nil {
		return v7FullGateProviderInvocation{Outcome: v7FullGateOutcomeProvider}, err
	}
	request, requestPath, err := p.newRequest(workspace, command)
	if err != nil {
		return v7FullGateProviderInvocation{Outcome: v7FullGateOutcomeProvider}, err
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
		return v7FullGateProviderInvocation{Outcome: v7FullGateOutcomeProvider}, fmt.Errorf("%w: provider is closed or already owns an active scope", errV7FullGateProvider)
	}
	if err := cmd.Start(); err != nil {
		p.mu.Unlock()
		// Start failed before the provider existed, so no lifecycle can need
		// recovery and retaining this record would be a false fail-closed block.
		_ = p.completeScope(scope)
		return v7FullGateProviderInvocation{Outcome: v7FullGateOutcomeProvider}, fmt.Errorf("%w: start provider: %v", errV7FullGateProvider, err)
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
			return v7FullGateProviderInvocation{Output: providerOutput.Bytes(), Outcome: v7FullGateOutcomeProvider}, waitErr
		}
		result, receiptErr := readV7FullGateProviderResult(request)
		if receiptErr != nil {
			cleanupResult, cleanupErr := p.cleanup(scope)
			if cleanupErr != nil {
				return v7FullGateProviderInvocation{Output: providerOutput.Bytes(), Outcome: v7FullGateOutcomeProvider}, v7JoinProviderErrors(receiptErr, cleanupErr)
			}
			return p.recordNormalizedProviderFailure(scope, request, cleanupResult, providerOutput.Bytes(), "invalid_run_result_before_certified_cleanup", receiptErr)
		}
		if result.State != "root_exited" && result.State != "cleaned" {
			stateErr := fmt.Errorf("%w: provider returned invalid run state %q", errV7FullGateProvider, result.State)
			cleanupResult, cleanupErr := p.cleanup(scope)
			if cleanupErr != nil {
				return v7FullGateProviderInvocation{Output: providerOutput.Bytes(), Outcome: v7FullGateOutcomeProvider}, v7JoinProviderErrors(stateErr, cleanupErr)
			}
			return p.recordNormalizedProviderFailure(scope, request, cleanupResult, providerOutput.Bytes(), "invalid_run_state_before_certified_cleanup", stateErr)
		}
		cleanupResult, cleanupErr := p.cleanup(scope)
		if cleanupErr != nil {
			return v7FullGateProviderInvocation{Output: append(providerOutput.Bytes(), []byte(result.Output)...), Outcome: v7FullGateOutcomeProvider}, v7JoinProviderErrors(v7ProviderRunError(runErr), cleanupErr)
		}
		if cleanupResult.LifecycleID != result.LifecycleID {
			identityErr := fmt.Errorf("%w: provider changed lifecycle identity between run and cleanup", errV7FullGateProvider)
			return p.recordNormalizedProviderFailure(scope, request, cleanupResult, providerOutput.Bytes(), "cleanup_lifecycle_identity_mismatch", identityErr)
		}
		normalized := false
		if time.Since(started) > time.Duration(request.MaxRuntimeMS)*time.Millisecond {
			cleanupResult = normalizedV7FullGateProviderFailure(request, cleanupResult, "daemon_measured_runtime_exceeded")
			normalized = true
		} else if cleanupResult.Outcome == v7FullGateOutcomePassed && runErr != nil {
			cleanupResult = normalizedV7FullGateProviderFailure(request, cleanupResult, "wrapper_failed_after_gate_pass")
			normalized = true
		}
		receipt, journalErr := p.recordReceipt(scope, request, cleanupResult)
		invocation := v7FullGateProviderInvocation{Output: []byte(cleanupResult.Output), Outcome: cleanupResult.Outcome, Receipt: receipt}
		if journalErr != nil {
			invocation.Outcome = v7FullGateOutcomeProvider
			return invocation, journalErr
		}
		// The journal is the crash-recovery authority. Write it before replacing
		// a backend's contradictory result so a crash can never resurrect a
		// measured overrun or wrapper/pass mismatch as reusable green proof.
		if normalized {
			if err := persistV7FullGateProviderResult(request.ResultPath, cleanupResult); err != nil {
				return invocation, err
			}
		}
		if cleanupResult.Outcome == v7FullGateOutcomePassed && runErr == nil {
			return invocation, nil
		}
		return invocation, v7FullGateResultError(cleanupResult, runErr, nil)
	case <-runCtx.Done():
		// CommandContext's cancel/WaitDelay path terminates and reaps the
		// wrapper. Do not race a second signal/kill here. The derived context
		// makes the same guarantee when Close races cancellation.
		runCancel()
		if _, waitErr := scope.waitWrapper(v7FullGateCloseWaitTimeout); waitErr != nil {
			return v7FullGateProviderInvocation{Output: providerOutput.Bytes(), Outcome: v7FullGateOutcomeProvider}, waitErr
		}
		cleanupResult, cleanupErr := p.cleanup(scope)
		if cleanupErr != nil {
			return v7FullGateProviderInvocation{Output: providerOutput.Bytes(), Outcome: v7FullGateOutcomeProvider}, cleanupErr
		}
		expected := v7FullGateOutcomeCanceled
		if errors.Is(runCtx.Err(), context.DeadlineExceeded) && ctx.Err() == nil {
			expected = v7FullGateOutcomeTimedOut
		}
		normalized := false
		if cleanupResult.Outcome != expected {
			cleanupResult = normalizedV7FullGateProviderFailure(request, cleanupResult, "cleanup_outcome_mismatch_expected_"+string(expected))
			normalized = true
		}
		receipt, journalErr := p.recordReceipt(scope, request, cleanupResult)
		invocation := v7FullGateProviderInvocation{Output: []byte(cleanupResult.Output), Outcome: cleanupResult.Outcome, Receipt: receipt}
		if journalErr != nil {
			invocation.Outcome = v7FullGateOutcomeProvider
			return invocation, journalErr
		}
		if normalized {
			if err := persistV7FullGateProviderResult(request.ResultPath, cleanupResult); err != nil {
				return invocation, err
			}
		}
		return invocation, v7FullGateResultError(cleanupResult, nil, runCtx.Err())
	}
}

func (p *v7ExternalFullGateProvider) recordNormalizedProviderFailure(scope *v7FullGateProviderScope, request v7FullGateProviderRequest, result v7FullGateProviderResult, output []byte, reason string, cause error) (v7FullGateProviderInvocation, error) {
	result = normalizedV7FullGateProviderFailure(request, result, reason)
	receipt, journalErr := p.recordReceipt(scope, request, result)
	invocation := v7FullGateProviderInvocation{Output: append(output, []byte(result.Output)...), Outcome: v7FullGateOutcomeProvider, Receipt: receipt}
	if journalErr != nil {
		return invocation, v7JoinProviderErrors(cause, journalErr)
	}
	if persistErr := persistV7FullGateProviderResult(request.ResultPath, result); persistErr != nil {
		return invocation, v7JoinProviderErrors(cause, persistErr)
	}
	return invocation, v7FullGateResultError(result, nil, cause)
}

func normalizedV7FullGateProviderFailure(request v7FullGateProviderRequest, result v7FullGateProviderResult, reason string) v7FullGateProviderResult {
	result.Outcome = v7FullGateOutcomeProvider
	result.Error = safePacketText(reason, 512)
	v7SealFullGateProviderResult(request, &result)
	return result
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
	created, err := ensureV7DurableDirectory(recoveryRoot, 0o700)
	if err != nil {
		return v7FullGateProviderRequest{}, "", err
	}
	if created {
		if err := runV7FullGateDurabilityHook("recovery_root_created_synced"); err != nil {
			return v7FullGateProviderRequest{}, "", err
		}
	}
	preparationRoot := filepath.Join(filepath.Dir(recoveryRoot), "full-gate-preparing")
	created, err = ensureV7DurableDirectory(preparationRoot, 0o700)
	if err != nil {
		return v7FullGateProviderRequest{}, "", err
	}
	if created {
		if err := runV7FullGateDurabilityHook("preparation_root_created_synced"); err != nil {
			return v7FullGateProviderRequest{}, "", err
		}
	}
	control, err := os.MkdirTemp(preparationRoot, "scope-")
	if err != nil {
		return v7FullGateProviderRequest{}, "", err
	}
	publishedControl := filepath.Join(recoveryRoot, filepath.Base(control))
	if err := syncV7Directory(control); err != nil {
		_ = os.RemoveAll(control)
		return v7FullGateProviderRequest{}, "", err
	}
	if err := syncV7Directory(preparationRoot); err != nil {
		_ = os.RemoveAll(control)
		return v7FullGateProviderRequest{}, "", err
	}
	if err := runV7FullGateDurabilityHook("preparation_dentry_synced"); err != nil {
		return v7FullGateProviderRequest{}, "", err
	}
	providerSnapshot := filepath.Join(control, "provider")
	if err := snapshotV7TrustedProvider(p.path, p.executableIdentity, providerSnapshot); err != nil {
		// No provider was launched, so this record is not a recovery scope.
		_ = os.RemoveAll(control)
		return v7FullGateProviderRequest{}, "", err
	}
	if err := runV7FullGateDurabilityHook("provider_snapshot_synced"); err != nil {
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
		ResultPath: filepath.Join(publishedControl, "result.json"), ProviderKind: p.kind, ProviderID: p.identity, ProviderPath: filepath.Join(publishedControl, "provider"), ExecutableID: p.executableIdentity,
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
	if err := runV7FullGateDurabilityHook("reservation_synced"); err != nil {
		return v7FullGateProviderRequest{}, "", err
	}
	if err := os.Rename(control, publishedControl); err != nil {
		return v7FullGateProviderRequest{}, "", err
	}
	if err := syncV7Directory(recoveryRoot); err != nil {
		return v7FullGateProviderRequest{}, "", err
	}
	if err := syncV7Directory(preparationRoot); err != nil {
		return v7FullGateProviderRequest{}, "", err
	}
	if err := runV7FullGateDurabilityHook("scope_published_synced"); err != nil {
		return v7FullGateProviderRequest{}, "", err
	}
	return request, filepath.Join(publishedControl, "request.json"), nil
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
	f, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o700)
	if err != nil {
		return err
	}
	if _, err = f.Write(raw); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err := syncV7Directory(filepath.Dir(destination)); err != nil {
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
	dir := filepath.Dir(scope.requestPath)
	parent := filepath.Dir(dir)
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("%w: remove certified provider scope: %v", errV7FullGateProvider, err)
	}
	if err := syncV7Directory(parent); err != nil {
		return fmt.Errorf("%w: sync retired provider scope: %v", errV7FullGateProvider, err)
	}
	return runV7FullGateDurabilityHook("scope_retired_synced")
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

func v7FullGateProviderOutcomeJournalRoot(stateRoot string) string {
	return filepath.Join(stateRoot, "full-gate-outcomes")
}

func v7FullGateProviderOutcomeJournalPath(stateRoot, departureID, requestDigest string) string {
	key := strings.TrimPrefix(requestDigest, "sha256:")
	departure := strings.TrimPrefix(v7FullGateTextDigest(departureID), "sha256:")[:16]
	return filepath.Join(v7FullGateProviderOutcomeJournalRoot(stateRoot), departure+"-"+key+".json")
}

func v7FullGateProviderOutcomeJournalDigest(journal v7FullGateProviderOutcomeJournal) string {
	receiptRaw, _ := json.Marshal(journal.Receipt)
	return v7FullGateTextDigest(strings.Join([]string{journal.Schema, journal.DepartureID, journal.RequestDigest, journal.ScopePath, journal.Request.RequestDigest, journal.Result.ResultDigest, string(receiptRaw), fmt.Sprint(journal.Reconciled), journal.Action}, "\x00"))
}

func persistV7FullGateProviderOutcomeJournal(stateRoot string, scope *v7FullGateProviderScope, request v7FullGateProviderRequest, result v7FullGateProviderResult, receipt GateProviderReceipt) error {
	if strings.TrimSpace(stateRoot) == "" {
		stateRoot = filepath.Dir(filepath.Dir(scope.requestPath))
	}
	root := v7FullGateProviderOutcomeJournalRoot(stateRoot)
	created, err := ensureV7DurableDirectory(root, 0o700)
	if err != nil {
		return err
	}
	if created {
		if err := runV7FullGateDurabilityHook("outcome_journal_root_created_synced"); err != nil {
			return err
		}
	}
	journal := v7FullGateProviderOutcomeJournal{Schema: v7FullGateOutcomeJournalSchema, DepartureID: request.DepartureID, RequestDigest: request.RequestDigest, ScopePath: filepath.Dir(scope.requestPath), Request: request, Result: result, Receipt: receipt}
	journal.JournalDigest = v7FullGateProviderOutcomeJournalDigest(journal)
	raw, err := json.Marshal(journal)
	if err != nil || len(raw) > v7FullGateResultMaxBytes {
		return fmt.Errorf("%w: provider outcome journal exceeds bound: %v", errV7FullGateProvider, err)
	}
	path := v7FullGateProviderOutcomeJournalPath(stateRoot, request.DepartureID, request.RequestDigest)
	if existing, readErr := readV7FullGateProviderOutcomeJournal(path); readErr == nil {
		if existing.JournalDigest != journal.JournalDigest {
			return fmt.Errorf("%w: provider outcome journal identity conflict", errV7FullGateProvider)
		}
		return nil
	} else if !errors.Is(readErr, os.ErrNotExist) {
		return readErr
	}
	if err := writeV7DurableJSON(path, raw, false); err != nil {
		return err
	}
	return runV7FullGateDurabilityHook("outcome_journal_synced")
}

func readV7FullGateProviderOutcomeJournal(path string) (v7FullGateProviderOutcomeJournal, error) {
	raw, err := readV7TrustedRegularFile(path, v7FullGateResultMaxBytes, false)
	if err != nil {
		return v7FullGateProviderOutcomeJournal{}, err
	}
	var journal v7FullGateProviderOutcomeJournal
	if err := json.Unmarshal(raw, &journal); err != nil {
		return v7FullGateProviderOutcomeJournal{}, fmt.Errorf("%w: invalid provider outcome journal", errV7FullGateProvider)
	}
	expectedReceipt := v7GateProviderReceiptForResult(journal.Request, journal.Result)
	if journal.Schema != v7FullGateOutcomeJournalSchema || journal.JournalDigest != v7FullGateProviderOutcomeJournalDigest(journal) || journal.RequestDigest != journal.Request.RequestDigest || journal.RequestDigest != v7FullGateRequestDigest(journal.Request) || journal.DepartureID != journal.Request.DepartureID || journal.Result.RequestDigest != journal.RequestDigest || journal.Result.ResultDigest != v7FullGateProviderResultDigest(journal.Result) || journal.Result.ReceiptDigest != v7FullGateReceiptDigest(journal.Request, journal.Result) || journal.Receipt != expectedReceipt {
		return v7FullGateProviderOutcomeJournal{}, fmt.Errorf("%w: invalid provider outcome journal", errV7FullGateProvider)
	}
	return journal, nil
}

func removeV7FullGateProviderOutcomeJournal(stateRoot string, receipt GateProviderReceipt) error {
	path := v7FullGateProviderOutcomeJournalPath(stateRoot, receipt.DepartureID, receipt.RequestDigest)
	if err := runV7FullGateDurabilityHook("before_outcome_journal_remove"); err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return syncV7Directory(filepath.Dir(path))
}

func persistV7FullGateProviderResult(path string, result v7FullGateProviderResult) error {
	raw, err := json.Marshal(result)
	if err != nil || len(raw) > v7FullGateResultMaxBytes {
		return fmt.Errorf("%w: normalized provider result exceeds bound: %v", errV7FullGateProvider, err)
	}
	return writeV7DurableJSON(path, raw, true)
}

func writeV7DurableJSON(path string, raw []byte, replace bool) error {
	payload := append(append([]byte(nil), raw...), '\n')
	return writeV7DurableFile(path, payload, 0o600, replace)
}

func writeV7DurableFile(path string, raw []byte, mode os.FileMode, replace bool) error {
	tmp := path + ".tmp-" + strings.ToLower(newRecordID())
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	if _, err = f.Write(raw); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return closeErr
	}
	if !replace {
		if err := os.Link(tmp, path); err != nil {
			_ = os.Remove(tmp)
			return err
		}
		if err := os.Remove(tmp); err != nil {
			return err
		}
	} else if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return syncV7Directory(filepath.Dir(path))
}

func writeV7DurablePromotionArtifact(path string, raw []byte) error {
	if _, err := ensureV7DurableDirectory(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := writeV7DurableFile(path, raw, 0o600, false); err != nil {
		return err
	}
	return runV7FullGateDurabilityHook("promotion_gate_artifact_synced")
}

func syncV7Directory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

// ensureV7DurableDirectory creates a service-owned directory and fsyncs every
// newly created directory followed by the nearest pre-existing parent. This is
// stronger than MkdirAll alone: after a power loss, the directory entries
// needed to discover recovery scopes and outcome journals are durable too.
func ensureV7DurableDirectory(path string, mode os.FileMode) (bool, error) {
	path = filepath.Clean(path)
	missing := make([]string, 0, 2)
	current := path
	for {
		info, err := os.Lstat(current)
		if err == nil {
			if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				return false, fmt.Errorf("%w: durable directory path is not a directory", errV7FullGateProvider)
			}
			break
		}
		if !errors.Is(err, os.ErrNotExist) {
			return false, err
		}
		missing = append(missing, current)
		parent := filepath.Dir(current)
		if parent == current {
			return false, fmt.Errorf("%w: durable directory has no existing parent", errV7FullGateProvider)
		}
		current = parent
	}
	if len(missing) == 0 {
		return false, nil
	}
	if err := os.MkdirAll(path, mode); err != nil {
		return false, err
	}
	for _, created := range missing {
		if err := syncV7Directory(created); err != nil {
			return false, err
		}
	}
	if err := syncV7Directory(current); err != nil {
		return false, err
	}
	return true, nil
}

func runV7FullGateDurabilityHook(stage string) error {
	if v7FullGateDurabilityHook != nil {
		return v7FullGateDurabilityHook(stage)
	}
	return nil
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
func recoverV7FullGateProviderScopes(stateRoot string, store *RuntimeStore) error {
	recoveryRoot := filepath.Join(stateRoot, "full-gate-recovery")
	if err := recoverV7FullGateProviderPreparations(filepath.Join(stateRoot, "full-gate-preparing")); err != nil {
		return err
	}
	journalRoot := v7FullGateProviderOutcomeJournalRoot(stateRoot)
	journalEntries, err := os.ReadDir(journalRoot)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("%w: read provider outcome journals: %v", errV7FullGateProvider, err)
	}
	if len(journalEntries) > v7FullGateJournalMaxEntries {
		return fmt.Errorf("%w: provider outcome journal count exceeds %d", errV7FullGateProvider, v7FullGateJournalMaxEntries)
	}
	if len(journalEntries) > 0 {
		filtered := make([]os.DirEntry, 0, len(journalEntries))
		removedTemp := false
		for _, entry := range journalEntries {
			if v7FullGateOutcomeJournalTemporaryName(entry.Name()) {
				path := filepath.Join(journalRoot, entry.Name())
				info, statErr := os.Lstat(path)
				if statErr != nil || !info.Mode().IsRegular() || info.Mode()&0o022 != 0 {
					return fmt.Errorf("%w: invalid provider outcome journal temporary %q", errV7FullGateProvider, entry.Name())
				}
				if err := os.Remove(path); err != nil {
					return fmt.Errorf("%w: retire provider outcome journal temporary %q: %v", errV7FullGateProvider, entry.Name(), err)
				}
				removedTemp = true
				continue
			}
			filtered = append(filtered, entry)
		}
		if removedTemp {
			if err := syncV7Directory(journalRoot); err != nil {
				return fmt.Errorf("%w: sync retired provider outcome journal temporaries: %v", errV7FullGateProvider, err)
			}
			if err := runV7FullGateRecoveryHook("outcome_journal_temporaries_retired"); err != nil {
				return err
			}
		}
		journalEntries = filtered
	}
	journals := make(map[string]v7FullGateProviderOutcomeJournal)
	journalPaths := make(map[string]string)
	journalOrder := make([]string, 0, len(journalEntries))
	for _, entry := range journalEntries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			return fmt.Errorf("%w: invalid provider outcome journal %q", errV7FullGateProvider, entry.Name())
		}
		path := filepath.Join(journalRoot, entry.Name())
		journal, readErr := readV7FullGateProviderOutcomeJournal(path)
		if readErr != nil {
			return readErr
		}
		if _, duplicate := journals[journal.RequestDigest]; duplicate {
			return fmt.Errorf("%w: duplicate provider outcome journal request digest", errV7FullGateProvider)
		}
		canonicalPath := v7FullGateProviderOutcomeJournalPath(stateRoot, journal.DepartureID, journal.RequestDigest)
		if filepath.Clean(path) != filepath.Clean(canonicalPath) {
			return fmt.Errorf("%w: non-canonical provider outcome journal filename", errV7FullGateProvider)
		}
		if strings.TrimSpace(journal.ScopePath) == "" {
			if !journal.Reconciled {
				return fmt.Errorf("%w: unreconciled provider outcome journal has no recovery scope", errV7FullGateProvider)
			}
		} else if !validV7FullGateRecoveryScopePath(recoveryRoot, journal.ScopePath) {
			return fmt.Errorf("%w: journal scope escapes recovery root", errV7FullGateProvider)
		}
		journals[journal.RequestDigest] = journal
		journalPaths[journal.RequestDigest] = path
		journalOrder = append(journalOrder, journal.RequestDigest)
	}
	scopeEntries, err := os.ReadDir(recoveryRoot)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("%w: read provider recovery records: %v", errV7FullGateProvider, err)
	}
	if len(scopeEntries) > v7FullGateRecoveryMaxScopes {
		return fmt.Errorf("%w: provider recovery scope count exceeds %d", errV7FullGateProvider, v7FullGateRecoveryMaxScopes)
	}
	for _, entry := range scopeEntries {
		if !entry.IsDir() {
			return fmt.Errorf("%w: invalid provider recovery entry %q", errV7FullGateProvider, entry.Name())
		}
		dir := filepath.Join(recoveryRoot, entry.Name())
		requestPath := filepath.Join(dir, "request.json")
		request, readErr := readV7FullGateProviderRequest(requestPath)
		if readErr != nil {
			return readErr
		}
		journal, ok := journals[request.RequestDigest]
		if !ok {
			result, resultErr := readV7FullGateProviderResult(request)
			if resultErr != nil || result.State != "cleaned" {
				_, executableID, verifyErr := verifyV7TrustedProviderExecutable(request.ProviderPath)
				if verifyErr != nil || executableID != request.ExecutableID {
					return fmt.Errorf("%w: provider recovery executable unavailable for %q", errV7FullGateProvider, entry.Name())
				}
				provider := &v7ExternalFullGateProvider{path: request.ProviderPath, executableIdentity: request.ExecutableID}
				scope := &v7FullGateProviderScope{request: request, requestPath: requestPath}
				result, resultErr = provider.cleanup(scope)
				if resultErr != nil {
					return resultErr
				}
			}
			normalized := false
			if result.Outcome == v7FullGateOutcomePassed {
				// A backend result alone cannot prove that the daemon accepted
				// its wall-clock runtime, wrapper exit, and outcome agreement.
				// Only the service-owned journal closes that acceptance window.
				result = normalizedV7FullGateProviderFailure(request, result, "recovered_cleaned_result_without_service_journal")
				normalized = true
			}
			receipt := v7GateProviderReceiptForResult(request, result)
			scope := &v7FullGateProviderScope{request: request, requestPath: requestPath}
			if err := persistV7FullGateProviderOutcomeJournal(stateRoot, scope, request, result, receipt); err != nil {
				return err
			}
			if normalized {
				if err := persistV7FullGateProviderResult(request.ResultPath, result); err != nil {
					return err
				}
			}
			journalPath := v7FullGateProviderOutcomeJournalPath(stateRoot, request.DepartureID, request.RequestDigest)
			journal, readErr = readV7FullGateProviderOutcomeJournal(journalPath)
			if readErr != nil {
				return readErr
			}
			journals[request.RequestDigest] = journal
			journalPaths[request.RequestDigest] = journalPath
			journalOrder = append(journalOrder, request.RequestDigest)
		}
		if journal.ScopePath != dir {
			return fmt.Errorf("%w: provider journal scope mismatch", errV7FullGateProvider)
		}
	}
	sort.Strings(journalOrder)
	for _, digest := range journalOrder {
		journal := journals[digest]
		if journal.Reconciled {
			if strings.TrimSpace(journal.ScopePath) == "" {
				continue
			}
			if err := retireV7FullGateRecoveredScope(stateRoot, journal); err != nil {
				return err
			}
			journal.ScopePath = ""
			journal.JournalDigest = v7FullGateProviderOutcomeJournalDigest(journal)
			raw, marshalErr := json.Marshal(journal)
			if marshalErr != nil {
				return marshalErr
			}
			if err := writeV7DurableJSON(journalPaths[digest], raw, true); err != nil {
				return err
			}
			continue
		}
		if err := runV7FullGateRecoveryHook("outcome_journal_ready"); err != nil {
			return err
		}
		if err := reconcileV7FullGateProviderOutcome(stateRoot, store, &journal); err != nil {
			return err
		}
		if journal.Reconciled {
			journal.JournalDigest = v7FullGateProviderOutcomeJournalDigest(journal)
			raw, marshalErr := json.Marshal(journal)
			if marshalErr != nil {
				return marshalErr
			}
			if err := writeV7DurableJSON(journalPaths[digest], raw, true); err != nil {
				return err
			}
			if err := runV7FullGateRecoveryHook("outcome_reconciled_synced"); err != nil {
				return err
			}
			if err := retireV7FullGateRecoveredScope(stateRoot, journal); err != nil {
				return err
			}
			journal.ScopePath = ""
			journal.JournalDigest = v7FullGateProviderOutcomeJournalDigest(journal)
			raw, marshalErr = json.Marshal(journal)
			if marshalErr != nil {
				return marshalErr
			}
			if err := writeV7DurableJSON(journalPaths[digest], raw, true); err != nil {
				return err
			}
			continue
		}
		if err := retireV7FullGateRecoveredOutcome(stateRoot, journal); err != nil {
			return err
		}
	}
	return nil
}

func v7FullGateOutcomeJournalTemporaryName(name string) bool {
	const baseLen = 16 + 1 + 64 + len(".json")
	marker := strings.LastIndex(name, ".tmp-")
	if marker != baseLen || marker+len(".tmp-") == len(name) {
		return false
	}
	base := name[:marker]
	if len(base) != baseLen || base[16] != '-' || base[len(base)-len(".json"):] != ".json" {
		return false
	}
	for index, r := range base[:len(base)-len(".json")] {
		if index == 16 {
			continue
		}
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}

func recoverV7FullGateProviderPreparations(root string) error {
	entries, err := os.ReadDir(root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("%w: read provider preparations: %v", errV7FullGateProvider, err)
	}
	if len(entries) > v7FullGateRecoveryMaxScopes {
		return fmt.Errorf("%w: provider preparation count exceeds %d", errV7FullGateProvider, v7FullGateRecoveryMaxScopes)
	}
	for _, entry := range entries {
		path := filepath.Join(root, entry.Name())
		info, statErr := os.Lstat(path)
		if statErr != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || !strings.HasPrefix(entry.Name(), "scope-") {
			return fmt.Errorf("%w: invalid provider preparation %q", errV7FullGateProvider, entry.Name())
		}
		if err := os.RemoveAll(path); err != nil {
			return fmt.Errorf("%w: retire provider preparation %q: %v", errV7FullGateProvider, entry.Name(), err)
		}
	}
	if err := syncV7Directory(root); err != nil {
		return fmt.Errorf("%w: sync retired provider preparations: %v", errV7FullGateProvider, err)
	}
	if len(entries) == 0 {
		return nil
	}
	return runV7FullGateRecoveryHook("preparation_retired_synced")
}

func readV7FullGateProviderRequest(path string) (v7FullGateProviderRequest, error) {
	raw, err := readV7TrustedRegularFile(path, v7FullGateRequestMaxBytes, false)
	if err != nil {
		return v7FullGateProviderRequest{}, fmt.Errorf("%w: read provider recovery request: %v", errV7FullGateProvider, err)
	}
	var request v7FullGateProviderRequest
	if err := json.Unmarshal(raw, &request); err != nil || request.RequestDigest != v7FullGateRequestDigest(request) || request.Schema != v7FullGateProviderSchema || request.Contract != v7FullGateIsolationContract {
		return v7FullGateProviderRequest{}, fmt.Errorf("%w: invalid provider recovery request", errV7FullGateProvider)
	}
	return request, nil
}

func reconcileV7FullGateProviderOutcome(stateRoot string, store *RuntimeStore, journal *v7FullGateProviderOutcomeJournal) error {
	if store == nil {
		return fmt.Errorf("%w: provider recovery store unavailable", errV7FullGateProvider)
	}
	run, err := store.FindDepartureRun(journal.DepartureID)
	if err != nil {
		return err
	}
	if journal.Result.Outcome == v7FullGateOutcomePassed {
		if run != nil {
			if err := validateV7FullGateJournalGreen(stateRoot, store, *run, *journal); err == nil {
				existing, lookupErr := store.FindGateLedger(journal.Receipt.ProjectID, journal.Receipt.CandidateDigest, journal.Request.Command, journal.Receipt.Profile, journal.Receipt.Toolchain)
				if lookupErr != nil {
					return lookupErr
				}
				if existing != nil && (existing.ProviderReceipt == nil || *existing.ProviderReceipt != journal.Receipt) {
					return blockV7FullGateJournalOutcome(store, run, journal, "conflicting_green_ledger")
				}
				if existing == nil {
					receipt := journal.Receipt
					if err := store.RecordGateLedger(GateLedgerEntry{ProjectID: receipt.ProjectID, TreeHash: receipt.CandidateDigest, Command: journal.Request.Command, Profile: receipt.Profile, Toolchain: receipt.Toolchain, Host: runtimeLeaseHost(), DurationMS: journal.Result.RuntimeMS, ProviderReceipt: &receipt}); err != nil {
						return err
					}
				}
				return runV7FullGateRecoveryHook("outcome_target_persisted")
			}
		}
		return blockV7FullGateJournalOutcome(store, run, journal, "green_outcome_no_longer_matches_current_contract")
	}
	return blockV7FullGateJournalOutcome(store, run, journal, "recovered_"+string(journal.Result.Outcome))
}

func validateV7FullGateJournalGreen(stateRoot string, store *RuntimeStore, run DepartureRun, journal v7FullGateProviderOutcomeJournal) error {
	if run.ProjectID != journal.Receipt.ProjectID || run.ID != journal.DepartureID || run.Candidate.CandidateSHA == "" {
		return errors.New("departure identity mismatch")
	}
	projects, err := store.ListProjects()
	if err != nil {
		return err
	}
	var project *RegisteredProject
	for i := range projects {
		if projects[i].ProjectID == run.ProjectID {
			project = &projects[i]
			break
		}
	}
	if project == nil {
		return errors.New("registered project unavailable")
	}
	wf, err := loadWorkflow(project.VaultRoot)
	if err != nil {
		return err
	}
	policy, err := scheduledPromotionGatePolicy(project.VaultRoot, wf.Data)
	if err != nil || policy.Profile != journal.Receipt.Profile || policy.IsolationProvider != journal.Receipt.ProviderProfile || scheduledPromotionFullGateToolchainFingerprint(project.RepoRoot, policy.HarvestCommands, policy.IsolationProvider, stateRoot) != journal.Receipt.Toolchain {
		return errors.New("current gate profile or toolchain mismatch")
	}
	commandPresent := false
	for _, command := range policy.HarvestCommands {
		commandPresent = commandPresent || command == journal.Request.Command
	}
	if !commandPresent || journal.Receipt.CommandDigest != v7FullGateTextDigest(journal.Request.Command) {
		return errors.New("current command mismatch")
	}
	tree, err := scheduledPromotionRecoveryLedgerTreeHash(project.RepoRoot, run.Candidate.CandidateSHA)
	if err != nil || tree != journal.Receipt.CandidateDigest {
		return errors.New("current candidate mismatch")
	}
	provider, err := newV7FullGateProvider(policy.IsolationProvider, project.RepoRoot, stateRoot)
	if err != nil {
		return err
	}
	defer provider.Close()
	verifier, ok := provider.(v7FullGateReceiptVerifier)
	if !ok || !verifier.MatchesGateProviderReceipt(&journal.Receipt) {
		return errors.New("current provider closure mismatch")
	}
	return nil
}

func blockV7FullGateJournalOutcome(store *RuntimeStore, run *DepartureRun, journal *v7FullGateProviderOutcomeJournal, reason string) error {
	if run == nil {
		journal.Reconciled = true
		journal.Action = safePacketText(reason+": departure "+journal.DepartureID+" requires operator inspection", 512)
		return runV7FullGateRecoveryHook("outcome_actionable_recorded")
	}
	for _, receipt := range run.Gate.ProviderOutcomes {
		if receipt == journal.Receipt {
			return nil
		}
	}
	if len(run.Gate.ProviderOutcomes) >= v7PromotionGateMaxReceipts {
		return fmt.Errorf("%w: recovered provider outcome count exceeds bound", errV7FullGateProvider)
	}
	next := *run
	next.Gate.ProviderOutcomes = append(append([]GateProviderReceipt(nil), run.Gate.ProviderOutcomes...), journal.Receipt)
	next.Gate.Status = string(journal.Result.Outcome)
	next.Gate.Failure = DepartureFailure{Class: "provider", Identity: safePacketText(reason, 256), Action: "operator_inspect_provider_outcome"}
	next.State = DepartureStateBlocked
	next.BlockReason = "provider outcome recovery: " + safePacketText(reason, 256)
	changed, err := store.TransitionDepartureRun(next, run.StateRevision)
	if err != nil {
		return err
	}
	if !changed {
		latest, readErr := store.FindDepartureRun(run.ID)
		if readErr != nil {
			return readErr
		}
		for _, receipt := range latest.Gate.ProviderOutcomes {
			if receipt == journal.Receipt {
				return nil
			}
		}
		return fmt.Errorf("%w: departure outcome recovery CAS conflict", errV7FullGateProvider)
	}
	return runV7FullGateRecoveryHook("outcome_target_persisted")
}

func retireV7FullGateRecoveredOutcome(stateRoot string, journal v7FullGateProviderOutcomeJournal) error {
	if err := retireV7FullGateRecoveredScope(stateRoot, journal); err != nil {
		return err
	}
	if err := removeV7FullGateProviderOutcomeJournal(stateRoot, journal.Receipt); err != nil {
		return err
	}
	return runV7FullGateRecoveryHook("outcome_scope_retired")
}

func retireV7FullGateRecoveredScope(stateRoot string, journal v7FullGateProviderOutcomeJournal) error {
	if strings.TrimSpace(journal.ScopePath) != "" {
		recoveryRoot := filepath.Join(stateRoot, "full-gate-recovery")
		if !validV7FullGateRecoveryScopePath(recoveryRoot, journal.ScopePath) {
			return fmt.Errorf("%w: journal scope escapes recovery root", errV7FullGateProvider)
		}
		requestPath := filepath.Join(journal.ScopePath, "request.json")
		if fileExists(requestPath) {
			provider := &v7ExternalFullGateProvider{}
			if err := provider.completeScope(&v7FullGateProviderScope{request: journal.Request, requestPath: requestPath}); err != nil {
				return err
			}
		}
	}
	return nil
}

func validV7FullGateRecoveryScopePath(root, scope string) bool {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	scopeAbs, err := filepath.Abs(scope)
	if err != nil || !filepath.IsAbs(scope) || filepath.Clean(scope) != scope || filepath.Clean(filepath.Dir(scopeAbs)) != filepath.Clean(rootAbs) || !strings.HasPrefix(filepath.Base(scopeAbs), "scope-") {
		return false
	}
	if info, statErr := os.Lstat(scopeAbs); statErr == nil {
		return info.IsDir() && info.Mode()&os.ModeSymlink == 0
	} else {
		return errors.Is(statErr, os.ErrNotExist)
	}
}

func runV7FullGateRecoveryHook(stage string) error {
	if v7FullGateRecoveryHook != nil {
		return v7FullGateRecoveryHook(stage)
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
