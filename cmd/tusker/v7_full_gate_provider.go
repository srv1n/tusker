package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"debug/macho"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
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
var v7FullGateProviderPipeWaitDelay = v7FullGateCleanTimeout

// The lifecycle proof currently relies on Darwin's descriptor-bound ACL
// inspection and inherited descriptor transport. Keep the platform lookup
// injectable so the refusal path is testable without a second host OS.
var v7FullGateProviderGOOS = func() string { return runtime.GOOS }

// Deterministic crash/durability and adversary seams. Production leaves them
// nil.
var (
	v7FullGateDurabilityHook func(string) error
	v7FullGateRecoveryHook   func(string) error
	// Test-only adversary and launch-observation seams. Production never
	// replaces executable verification or the launcher.
	v7FullGateAfterExecutableDigestVerified func(string) error
	v7FullGateBeforeProviderLaunch          func(string, string) error
)

var errV7FullGateProvider = errors.New("full-gate lifecycle provider")
var errV7FullGateLedgerConflict = errors.New("full-gate lifecycle provider ledger conflict")

const v7FullGateProviderUnsupportedPlatformCode = "GATE_PROVIDER_UNSUPPORTED_PLATFORM"

func v7FullGateProviderUnsupportedPlatformError(platform, detail string) error {
	platform = strings.TrimSpace(platform)
	if platform == "" {
		platform = "unknown"
	}
	return tuskerError(
		v7FullGateProviderUnsupportedPlatformCode,
		v7FullGateProviderUnsupportedPlatformCode+": full-gate lifecycle provider cannot run on "+platform+": "+detail,
		withHint("run the full gate on macOS with the configured lifecycle provider; do not treat this refusal as a gate pass"),
		withContext(map[string]any{"goos": platform, "supported": []string{"darwin"}}),
	)
}

func v7FullGateProviderPlatformError() error {
	platform := v7FullGateProviderGOOS()
	if platform == "darwin" {
		return nil
	}
	return v7FullGateProviderUnsupportedPlatformError(platform, "Darwin descriptor transport and immutable provider authority are unavailable; refusing pathname-based fallback")
}

func isV7FullGateProviderError(err error) bool {
	var typed *TuskerError
	return errors.Is(err, errV7FullGateProvider) || (errors.As(err, &typed) && typed != nil && typed.Code == v7FullGateProviderUnsupportedPlatformCode)
}

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

var runV7FullGateProviderCleanup = launchV7FullGateProviderCleanup

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
	ArtifactRef          string   `json:"artifact_ref"`
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
	if err := v7FullGateProviderPlatformError(); err != nil {
		return nil, err
	}
	state, err := openV7FullGateStateRoot(stateRoot)
	if err != nil {
		return nil, err
	}
	trusted, path, identity, executableIdentity, err := resolveV7TrustedFullGateProviderAtRoot(profile, state)
	if err != nil {
		_ = state.Close()
		return nil, err
	}
	if v7PathWithin(repoRoot, path) {
		_ = state.Close()
		return nil, fmt.Errorf("%w: trusted provider executable must not be repository-local", errV7FullGateProvider)
	}
	return &v7ExternalFullGateProvider{path: path, kind: trusted.Kind, identity: identity, executableIdentity: executableIdentity, runtimeDigest: trusted.RuntimeDigest, clientDigest: trusted.ClientDigest, policyDigest: trusted.PolicyDigest, attestationDigest: trusted.AttestationDigest, capabilities: append([]string(nil), trusted.Capabilities...), implementationID: trusted.ImplementationID, capabilitySchema: trusted.CapabilitySchema, imageOrVMID: trusted.ImageOrVMID, profile: profile, stateRoot: state.path, state: state, recoveryRoot: filepath.Join(state.path, "full-gate-recovery")}, nil
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
	state              *v7FullGateStateRoot
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
	state       *v7FullGateStateRoot
	scopeRel    string
	requestRel  string
	resultRel   string
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
	cleanupMu      sync.Mutex
	cleanupCalled  bool
	cleanupResult  v7FullGateProviderResult
	cleanupErr     error
	finalizeMu     sync.Mutex
	transportMu    sync.Mutex
	resultIdentity os.FileInfo
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
	ArtifactRef     string
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

type v7FullGateProviderArtifactBinder interface {
	BindFullGateProviderArtifact(string, []GateProviderReceipt) error
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

func (p *v7ExternalFullGateProvider) stateHandle(scope *v7FullGateProviderScope) (*v7FullGateStateRoot, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.state != nil && p.state.root != nil {
		if scope != nil && scope.state == nil {
			scope.state = p.state
		}
		return p.state, nil
	}
	rootPath := strings.TrimSpace(p.stateRoot)
	if rootPath == "" && scope != nil && filepath.IsAbs(scope.requestPath) {
		rootPath = filepath.Dir(filepath.Dir(filepath.Dir(scope.requestPath)))
	}
	if rootPath == "" {
		return nil, fmt.Errorf("%w: provider state root is unavailable", errV7FullGateProvider)
	}
	state, err := openV7FullGateStateRoot(rootPath)
	if err != nil {
		return nil, err
	}
	p.state, p.stateRoot = state, state.path
	if scope != nil {
		scope.state = state
	}
	return state, nil
}

func (scope *v7FullGateProviderScope) bindState(state *v7FullGateStateRoot) error {
	if scope == nil || state == nil {
		return fmt.Errorf("%w: provider scope state is unavailable", errV7FullGateProvider)
	}
	if scope.requestRel == "" {
		rel, err := state.relative(scope.requestPath)
		if err != nil {
			return err
		}
		scope.requestRel = rel
	}
	if scope.scopeRel == "" {
		scope.scopeRel = filepath.Dir(scope.requestRel)
	}
	if scope.resultRel == "" {
		scope.resultRel = filepath.Join(scope.scopeRel, "result.json")
	}
	scope.state = state
	return nil
}

func (scope *v7FullGateProviderScope) openTransport() (*os.File, *os.File, *os.File, error) {
	if scope == nil || scope.state == nil {
		return nil, nil, nil, fmt.Errorf("%w: provider descriptor transport has no state-root authority", errV7FullGateProvider)
	}
	requestFile, requestOpened, err := scope.state.openRegular(scope.requestRel, v7FullGateRequestMaxBytes, false)
	if err != nil {
		return nil, nil, nil, err
	}
	requestRaw, readErr := io.ReadAll(io.LimitReader(requestFile, v7FullGateRequestMaxBytes+1))
	requestAfter, statErr := requestFile.Stat()
	openedRequest, decodeErr := decodeV7FullGateProviderRequest(requestRaw, readErr)
	if statErr != nil || !os.SameFile(requestOpened, requestAfter) || requestAfter.Size() != requestOpened.Size() || int64(len(requestRaw)) != requestOpened.Size() || int64(len(requestRaw)) > v7FullGateRequestMaxBytes || decodeErr != nil || !reflect.DeepEqual(openedRequest, scope.request) {
		_ = requestFile.Close()
		return nil, nil, nil, fmt.Errorf("%w: inherited request descriptor does not exactly bind the prepared request", errV7FullGateProvider)
	}
	if _, err := requestFile.Seek(0, io.SeekStart); err != nil {
		_ = requestFile.Close()
		return nil, nil, nil, fmt.Errorf("%w: rewind inherited request descriptor: %v", errV7FullGateProvider, err)
	}
	before, err := scope.state.lstat(scope.scopeRel)
	if err != nil || !before.IsDir() || before.Mode()&os.ModeSymlink != 0 || before.Mode()&0o022 != 0 {
		_ = requestFile.Close()
		return nil, nil, nil, fmt.Errorf("%w: provider descriptor scope is invalid", errV7FullGateProvider)
	}
	scopeDir, err := scope.state.open(scope.scopeRel)
	if err != nil {
		_ = requestFile.Close()
		return nil, nil, nil, err
	}
	opened, err := scopeDir.Stat()
	if err != nil || !os.SameFile(before, opened) {
		_ = requestFile.Close()
		_ = scopeDir.Close()
		return nil, nil, nil, fmt.Errorf("%w: provider descriptor scope identity changed while opening", errV7FullGateProvider)
	}
	resultBefore, err := scope.state.lstat(scope.resultRel)
	if err != nil || !resultBefore.Mode().IsRegular() || resultBefore.Mode()&os.ModeSymlink != 0 || resultBefore.Mode()&0o022 != 0 {
		_ = requestFile.Close()
		_ = scopeDir.Close()
		return nil, nil, nil, fmt.Errorf("%w: provider result reservation is invalid", errV7FullGateProvider)
	}
	resultFile, err := scope.state.root.OpenFile(scope.resultRel, os.O_RDWR, 0)
	if err != nil {
		_ = requestFile.Close()
		_ = scopeDir.Close()
		return nil, nil, nil, err
	}
	resultOpened, err := resultFile.Stat()
	if err != nil || !os.SameFile(resultBefore, resultOpened) {
		_ = requestFile.Close()
		_ = scopeDir.Close()
		_ = resultFile.Close()
		return nil, nil, nil, fmt.Errorf("%w: provider result reservation identity changed while opening", errV7FullGateProvider)
	}
	scope.transportMu.Lock()
	scope.resultIdentity = resultOpened
	scope.transportMu.Unlock()
	// scopeDir remains open only in Tusker. It pins the exact scope while the
	// child runs but is deliberately not inherited: a backend with directory
	// authority could use openat to mutate recovery records.
	return requestFile, resultFile, scopeDir, nil
}

func (scope *v7FullGateProviderScope) readRequest() (v7FullGateProviderRequest, error) {
	if scope == nil || scope.state == nil {
		return v7FullGateProviderRequest{}, fmt.Errorf("%w: provider request state authority is unavailable", errV7FullGateProvider)
	}
	return readV7FullGateProviderRequestAtRoot(scope.state, scope.requestRel)
}

func (scope *v7FullGateProviderScope) readResult() (v7FullGateProviderResult, error) {
	if scope == nil || scope.state == nil {
		return v7FullGateProviderResult{}, fmt.Errorf("%w: provider result state authority is unavailable", errV7FullGateProvider)
	}
	resultFile, opened, err := scope.state.openRegular(scope.resultRel, v7FullGateResultMaxBytes, false)
	if err != nil {
		return decodeV7FullGateProviderResult(nil, err, scope.request)
	}
	defer resultFile.Close()
	scope.transportMu.Lock()
	expected := scope.resultIdentity
	scope.transportMu.Unlock()
	if expected != nil && !os.SameFile(expected, opened) {
		return v7FullGateProviderResult{}, fmt.Errorf("%w: provider result reservation was replaced after launch", errV7FullGateProvider)
	}
	raw, readErr := io.ReadAll(io.LimitReader(resultFile, v7FullGateResultMaxBytes+1))
	after, statErr := resultFile.Stat()
	if readErr != nil || statErr != nil || !os.SameFile(opened, after) || after.Size() != opened.Size() || int64(len(raw)) != opened.Size() || int64(len(raw)) > v7FullGateResultMaxBytes {
		return v7FullGateProviderResult{}, fmt.Errorf("%w: provider result changed or exceeded its bound while reading", errV7FullGateProvider)
	}
	return decodeV7FullGateProviderResult(raw, nil, scope.request)
}

func (scope *v7FullGateProviderScope) persistResult(result v7FullGateProviderResult) error {
	if scope == nil || scope.state == nil {
		return fmt.Errorf("%w: provider result state authority is unavailable", errV7FullGateProvider)
	}
	scope.transportMu.Lock()
	expected := scope.resultIdentity
	scope.transportMu.Unlock()
	return persistV7FullGateProviderResultAtRoot(scope.state, scope.resultRel, result, expected)
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
	Schema         string                    `json:"schema"`
	DepartureID    string                    `json:"departure_id"`
	RequestDigest  string                    `json:"request_digest"`
	ScopePath      string                    `json:"scope_path"`
	Request        v7FullGateProviderRequest `json:"request"`
	Result         v7FullGateProviderResult  `json:"result"`
	Receipt        GateProviderReceipt       `json:"receipt"`
	ArtifactRef    string                    `json:"artifact_ref,omitempty"`
	ArtifactDigest string                    `json:"artifact_digest,omitempty"`
	Reconciled     bool                      `json:"reconciled,omitempty"`
	Action         string                    `json:"action,omitempty"`
	JournalDigest  string                    `json:"journal_digest"`
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
	if strings.TrimSpace(binding.ProjectID) == "" || strings.TrimSpace(binding.DepartureID) == "" || strings.TrimSpace(binding.CandidateDigest) == "" || strings.TrimSpace(binding.GateProfile) == "" || strings.TrimSpace(binding.ProviderProfile) == "" || strings.TrimSpace(binding.Toolchain) == "" || strings.TrimSpace(binding.ArtifactRef) == "" || binding.ProviderProfile != p.profile {
		return fmt.Errorf("%w: full-gate binding lacks the current project, departure, candidate, gate/provider profile, toolchain, or artifact identity", errV7FullGateProvider)
	}
	if p.state == nil {
		return fmt.Errorf("%w: full-gate binding has no trusted state-root handle", errV7FullGateProvider)
	}
	artifactRel, err := p.state.relative(binding.ArtifactRef)
	if err != nil || filepath.Dir(artifactRel) != filepath.Join("artifacts", "promotion-gates") || filepath.Ext(artifactRel) != ".log" {
		return fmt.Errorf("%w: full-gate artifact target is outside the trusted promotion artifact directory", errV7FullGateProvider)
	}
	for _, pending := range p.pending {
		if pending != nil && filepath.Clean(pending.request.ArtifactRef) == filepath.Clean(binding.ArtifactRef) {
			return fmt.Errorf("%w: each provider command requires a unique journal-bound artifact", errV7FullGateProvider)
		}
	}
	p.binding = binding
	return nil
}

func (p *v7ExternalFullGateProvider) recordReceipt(scope *v7FullGateProviderScope, request v7FullGateProviderRequest, result v7FullGateProviderResult) (GateProviderReceipt, error) {
	receipt := v7GateProviderReceiptForResult(request, result)
	if scope != nil {
		state, err := p.stateHandle(scope)
		if err != nil {
			return receipt, err
		}
		if err := persistV7FullGateProviderOutcomeJournalAtRoot(state, scope, request, result, receipt); err != nil {
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
	p.mu.Unlock()
	scope.finalizeMu.Lock()
	defer scope.finalizeMu.Unlock()
	p.mu.Lock()
	if p.pending[receipt.RequestDigest] != scope {
		p.mu.Unlock()
		return fmt.Errorf("%w: provider receipt is not awaiting persistence acknowledgement", errV7FullGateProvider)
	}
	state := p.state
	p.mu.Unlock()
	if strings.TrimSpace(scope.request.ArtifactRef) != "" {
		if state == nil {
			return fmt.Errorf("%w: provider outcome artifact authority is unavailable", errV7FullGateProvider)
		}
		journal, err := readV7FullGateProviderOutcomeJournalAtRoot(state, v7FullGateProviderOutcomeJournalRelPath(receipt.DepartureID, receipt.RequestDigest))
		if err != nil {
			return err
		}
		if journal.ArtifactRef == "" || journal.ArtifactDigest == "" {
			return fmt.Errorf("%w: provider outcome cannot retire before its durable artifact is bound", errV7FullGateProvider)
		}
		if err := ensureV7FullGateJournalArtifactAtRoot(state, &journal); err != nil {
			return err
		}
	}
	if err := p.completeScope(scope); err != nil {
		return err
	}
	if err := removeV7FullGateProviderOutcomeJournalAtRoot(state, receipt); err != nil {
		return err
	}
	p.mu.Lock()
	delete(p.pending, receipt.RequestDigest)
	p.mu.Unlock()
	if err := runV7FullGateDurabilityHook("outcome_retired"); err != nil {
		return err
	}
	return p.closeStateIfIdle()
}

func (p *v7ExternalFullGateProvider) BindFullGateProviderArtifact(artifactRef string, receipts []GateProviderReceipt) error {
	state, err := p.stateHandle(nil)
	if err != nil {
		return err
	}
	p.mu.Lock()
	boundArtifact := p.binding.ArtifactRef
	p.mu.Unlock()
	if boundArtifact != artifactRef {
		return fmt.Errorf("%w: durable artifact does not match the provider request binding", errV7FullGateProvider)
	}
	for _, receipt := range receipts {
		p.mu.Lock()
		scope := p.pending[receipt.RequestDigest]
		p.mu.Unlock()
		if scope == nil {
			continue
		}
		scope.finalizeMu.Lock()
		rel := v7FullGateProviderOutcomeJournalRelPath(receipt.DepartureID, receipt.RequestDigest)
		journal, readErr := readV7FullGateProviderOutcomeJournalAtRoot(state, rel)
		if readErr != nil {
			scope.finalizeMu.Unlock()
			return readErr
		}
		if err := ensureV7FullGateJournalArtifactAtRoot(state, &journal); err != nil {
			scope.finalizeMu.Unlock()
			return err
		}
		scope.finalizeMu.Unlock()
	}
	return nil
}

func resolveV7TrustedFullGateProvider(profile, stateRoot string) (v7TrustedFullGateProvider, string, string, string, error) {
	state, err := openV7FullGateStateRoot(stateRoot)
	if err != nil {
		return v7TrustedFullGateProvider{}, "", "", "", err
	}
	defer state.Close()
	return resolveV7TrustedFullGateProviderAtRoot(profile, state)
}

func resolveV7TrustedFullGateProviderAtRoot(profile string, state *v7FullGateStateRoot) (v7TrustedFullGateProvider, string, string, string, error) {
	profile = strings.TrimSpace(profile)
	if profile == "" {
		return v7TrustedFullGateProvider{}, "", "", "", fmt.Errorf("%w: scheduled full promotion requires a configured lifecycle-safe container/VM isolation_provider profile", errV7FullGateProvider)
	}
	if state == nil || state.root == nil {
		return v7TrustedFullGateProvider{}, "", "", "", fmt.Errorf("%w: trusted provider state root is unavailable", errV7FullGateProvider)
	}
	registryPath := v7FullGateProviderRegistryPath(state.path)
	registryRel, err := state.relative(registryPath)
	if err != nil {
		return v7TrustedFullGateProvider{}, "", "", "", fmt.Errorf("%w: trusted provider registry must be inside the daemon state root", errV7FullGateProvider)
	}
	raw, err := state.readRegular(registryRel, v7FullGateRequestMaxBytes, false)
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
	state, err := openV7FullGateStateRoot(stateRoot)
	if err != nil {
		return err
	}
	return state.Close()
}

func verifyV7TrustedProviderExecutable(path string) (string, string, error) {
	if err := v7FullGateProviderPlatformError(); err != nil {
		return "", "", err
	}
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
	codeClosure, err := validateV7MachOProvider(raw)
	if err != nil {
		return "", "", err
	}
	sum := sha256.Sum256(raw)
	mainIdentity := fmt.Sprintf("sha256:%x", sum[:])
	identity := departureFingerprint(append([]string{"tusker.mach-o-provider-closure/v1", mainIdentity}, codeClosure...)...)
	if v7FullGateAfterExecutableDigestVerified != nil {
		if err := v7FullGateAfterExecutableDigestVerified(path); err != nil {
			return "", "", err
		}
	}
	if err := verifyV7ImmutableProviderAuthority(path); err != nil {
		return "", "", err
	}
	// The immutable-authority proof is the launch boundary; this second
	// descriptor read catches an attempted replacement during verification.
	after, err := readV7TrustedRegularFile(path, 64<<20, true)
	if err != nil {
		return "", "", fmt.Errorf("%w: revalidate immutable provider executable: %v", errV7FullGateProvider, err)
	}
	afterSum := sha256.Sum256(after)
	if mainIdentity != fmt.Sprintf("sha256:%x", afterSum[:]) {
		return "", "", fmt.Errorf("%w: provider executable changed across immutable launch verification", errV7FullGateProvider)
	}
	return path, identity, nil
}

func validateV7MachOProvider(raw []byte) ([]string, error) {
	reader := bytes.NewReader(raw)
	if thin, err := macho.NewFile(reader); err == nil {
		defer thin.Close()
		return validateV7MachOProviderImage(thin)
	}
	fat, err := macho.NewFatFile(reader)
	if err != nil {
		return nil, fmt.Errorf("%w: provider executable must be a native Mach-O binary", errV7FullGateProvider)
	}
	defer fat.Close()
	accepted := make(map[string]struct{})
	for _, arch := range fat.Arches {
		if arch.File == nil {
			return nil, fmt.Errorf("%w: provider universal Mach-O contains an invalid image", errV7FullGateProvider)
		}
		paths, validateErr := validateV7MachOProviderImage(arch.File)
		if validateErr != nil {
			return nil, validateErr
		}
		for _, path := range paths {
			accepted[path] = struct{}{}
		}
	}
	return sortedV7MachOClosure(accepted), nil
}

const (
	v7MachOLoadFixedVMLibrary = uint32(0x6)
	v7MachOLoadFVMFile        = uint32(0x9)
	v7MachOLoadDylib          = uint32(0xc)
	v7MachOIDLibrary          = uint32(0xd)
	v7MachOLoadDylinker       = uint32(0xe)
	v7MachOIDDynamicLinker    = uint32(0xf)
	v7MachOPreboundDylib      = uint32(0x10)
	v7MachOLoadWeakDylib      = uint32(0x80000018)
	v7MachORPath              = uint32(0x8000001c)
	v7MachOReexportDylib      = uint32(0x8000001f)
	v7MachOLazyLoadDylib      = uint32(0x20)
	v7MachOLoadUpwardDylib    = uint32(0x80000023)
	v7MachODynamicEnvironment = uint32(0x27)
)

func validateV7MachOProviderImage(file *macho.File) ([]string, error) {
	if file == nil || file.Type != macho.TypeExec {
		return nil, fmt.Errorf("%w: provider Mach-O is not an executable image", errV7FullGateProvider)
	}
	accepted := make(map[string]struct{})
	for _, load := range file.Loads {
		raw := load.Raw()
		if len(raw) < 8 {
			return nil, fmt.Errorf("%w: provider Mach-O contains a truncated load command", errV7FullGateProvider)
		}
		command := file.ByteOrder.Uint32(raw[:4])
		switch command {
		case v7MachOLoadDylib, v7MachOLoadWeakDylib, v7MachOReexportDylib, v7MachOLazyLoadDylib, v7MachOLoadUpwardDylib:
			path, err := v7MachOLoadCommandPath(file, raw, 24)
			if err != nil || !v7SealedSystemLibraryPath(path) {
				return nil, fmt.Errorf("%w: provider Mach-O imports an unresolved or mutable library %q", errV7FullGateProvider, path)
			}
			accepted["dylib:"+path] = struct{}{}
		case v7MachOLoadDylinker:
			path, err := v7MachOLoadCommandPath(file, raw, 12)
			if err != nil || path != "/usr/lib/dyld" {
				return nil, fmt.Errorf("%w: provider Mach-O selects an unresolved or mutable dynamic linker %q", errV7FullGateProvider, path)
			}
			accepted["dylinker:"+path] = struct{}{}
		case v7MachORPath:
			return nil, fmt.Errorf("%w: provider Mach-O LC_RPATH is not permitted", errV7FullGateProvider)
		case v7MachODynamicEnvironment:
			return nil, fmt.Errorf("%w: provider Mach-O LC_DYLD_ENVIRONMENT is not permitted", errV7FullGateProvider)
		case v7MachOLoadFixedVMLibrary, v7MachOLoadFVMFile, v7MachOIDLibrary, v7MachOIDDynamicLinker, v7MachOPreboundDylib:
			return nil, fmt.Errorf("%w: provider Mach-O contains unsupported code-loading command %#x", errV7FullGateProvider, command)
		case
			0x1,        // LC_SEGMENT
			0x2,        // LC_SYMTAB
			0x4,        // LC_THREAD
			0x5,        // LC_UNIXTHREAD
			0xb,        // LC_DYSYMTAB
			0x11,       // LC_ROUTINES
			0x16,       // LC_TWOLEVEL_HINTS
			0x17,       // LC_PREBIND_CKSUM
			0x19,       // LC_SEGMENT_64
			0x1a,       // LC_ROUTINES_64
			0x1b,       // LC_UUID
			0x1d,       // LC_CODE_SIGNATURE
			0x1e,       // LC_SEGMENT_SPLIT_INFO
			0x21,       // LC_ENCRYPTION_INFO
			0x22,       // LC_DYLD_INFO
			0x80000022, // LC_DYLD_INFO_ONLY
			0x24,       // LC_VERSION_MIN_MACOSX
			0x25,       // LC_VERSION_MIN_IPHONEOS
			0x26,       // LC_FUNCTION_STARTS
			0x80000028, // LC_MAIN
			0x29,       // LC_DATA_IN_CODE
			0x2a,       // LC_SOURCE_VERSION
			0x2b,       // LC_DYLIB_CODE_SIGN_DRS
			0x2c,       // LC_ENCRYPTION_INFO_64
			0x2e,       // LC_LINKER_OPTIMIZATION_HINT
			0x2f,       // LC_VERSION_MIN_TVOS
			0x30,       // LC_VERSION_MIN_WATCHOS
			0x31,       // LC_NOTE
			0x32,       // LC_BUILD_VERSION
			0x80000033, // LC_DYLD_EXPORTS_TRIE
			0x80000034, // LC_DYLD_CHAINED_FIXUPS
			0x36,       // LC_ATOM_INFO
			0x37,       // LC_FUNCTION_VARIANTS
			0x38,       // LC_FUNCTION_VARIANT_FIXUPS
			0x39,       // LC_TARGET_TRIPLE
			0x3a:       // LC_LAZY_LOAD_DYLIB_INFO
			continue
		default:
			return nil, fmt.Errorf("%w: provider Mach-O contains an unrecognized load command %#x", errV7FullGateProvider, command)
		}
	}
	return sortedV7MachOClosure(accepted), nil
}

func v7MachOLoadCommandPath(file *macho.File, raw []byte, minimumOffset uint32) (string, error) {
	if file == nil || len(raw) < 12 {
		return "", errors.New("truncated path load command")
	}
	offset := file.ByteOrder.Uint32(raw[8:12])
	if offset < minimumOffset || offset >= uint32(len(raw)) {
		return "", errors.New("invalid path load command")
	}
	tail := raw[offset:]
	end := bytes.IndexByte(tail, 0)
	if end < 0 {
		return "", errors.New("unterminated path load command")
	}
	path := string(tail[:end])
	if path == "" || strings.TrimSpace(path) != path || strings.ContainsRune(path, 0) {
		return "", errors.New("invalid path load command")
	}
	return path, nil
}

func v7SealedSystemLibraryPath(path string) bool {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return false
	}
	return strings.HasPrefix(path, "/usr/lib/") || strings.HasPrefix(path, "/System/Library/")
}

func sortedV7MachOClosure(values map[string]struct{}) []string {
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

const v7ImmutableProviderSetupPrerequisite = "provider executable setup prerequisite: install a native root-owned binary beneath root-owned non-group/world-writable directories"

func verifyV7ImmutableProviderAuthority(path string) error {
	if err := v7FullGateProviderPlatformError(); err != nil {
		return err
	}
	current := filepath.Clean(path)
	for {
		info, err := os.Lstat(current)
		uid, hasUID := v7FileOwnerUID(info)
		if err != nil || info.Mode()&os.ModeSymlink != 0 || !hasUID || uid != 0 || info.Mode()&0o022 != 0 {
			return fmt.Errorf("%w: %s (%s)", errV7FullGateProvider, v7ImmutableProviderSetupPrerequisite, current)
		}
		file, err := os.Open(current)
		if err != nil {
			return fmt.Errorf("%w: %s (%s)", errV7FullGateProvider, v7ImmutableProviderSetupPrerequisite, current)
		}
		opened, statErr := file.Stat()
		mutationACL, aclErr := v7DarwinDescriptorHasMutationACL(file)
		afterACL, afterErr := file.Stat()
		closeErr := file.Close()
		if aclErr != nil && errorToIssue(aclErr).Code == v7FullGateProviderUnsupportedPlatformCode {
			return aclErr
		}
		if statErr != nil || !os.SameFile(info, opened) || aclErr != nil || mutationACL || afterErr != nil || !os.SameFile(opened, afterACL) || closeErr != nil {
			return fmt.Errorf("%w: %s; descriptor ACL/identity validation failed for %s", errV7FullGateProvider, v7ImmutableProviderSetupPrerequisite, current)
		}
		if current == string(filepath.Separator) {
			break
		}
		parent := filepath.Dir(current)
		if parent == current {
			return fmt.Errorf("%w: %s", errV7FullGateProvider, v7ImmutableProviderSetupPrerequisite)
		}
		current = parent
	}
	return nil
}

func v7FileOwnerUID(info os.FileInfo) (uint64, bool) {
	if info == nil || info.Sys() == nil {
		return 0, false
	}
	value := reflect.ValueOf(info.Sys())
	if value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return 0, false
		}
		value = value.Elem()
	}
	if value.Kind() != reflect.Struct {
		return 0, false
	}
	field := value.FieldByName("Uid")
	switch field.Kind() {
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return field.Uint(), true
	default:
		return 0, false
	}
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
	state, err := p.stateHandle(scope)
	if err != nil {
		return v7FullGateProviderInvocation{Outcome: v7FullGateOutcomeProvider}, err
	}
	if err := scope.bindState(state); err != nil {
		return v7FullGateProviderInvocation{Outcome: v7FullGateOutcomeProvider}, err
	}
	started := time.Now()
	if v7FullGateBeforeProviderLaunch != nil {
		if err := v7FullGateBeforeProviderLaunch("run", p.path); err != nil {
			_ = p.completeScope(scope)
			return v7FullGateProviderInvocation{Outcome: v7FullGateOutcomeProvider}, err
		}
	}
	if err := p.verifyIdentity(); err != nil {
		_ = p.completeScope(scope)
		return v7FullGateProviderInvocation{Outcome: v7FullGateOutcomeProvider}, err
	}
	// CommandContext guarantees that a cancelled daemon context cannot leave
	// the wrapper process awaited forever. Cancel asks it to terminate first;
	// WaitDelay then force-reaps it if it ignores SIGTERM.
	cmd, closeTransport, err := v7FullGateProviderCommand(runCtx, request.ProviderPath, "--tusker-full-gate-run", scope)
	if err != nil {
		_ = p.completeScope(scope)
		return v7FullGateProviderInvocation{Outcome: v7FullGateOutcomeProvider}, err
	}
	defer closeTransport()
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
	cmd.WaitDelay = v7FullGateProviderPipeWaitDelay
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
		result, receiptErr := scope.readResult()
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
			if err := scope.persistResult(cleanupResult); err != nil {
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
			if err := scope.persistResult(cleanupResult); err != nil {
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
	if persistErr := scope.persistResult(result); persistErr != nil {
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
		return p.closeStateIfIdle()
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

func (p *v7ExternalFullGateProvider) closeStateIfIdle() error {
	p.mu.Lock()
	if !p.closed || p.active != nil || len(p.pending) != 0 || p.state == nil {
		p.mu.Unlock()
		return nil
	}
	state := p.state
	p.state = nil
	p.mu.Unlock()
	return state.Close()
}

func (p *v7ExternalFullGateProvider) newRequest(workspace, command string) (v7FullGateProviderRequest, string, error) {
	if len(command) == 0 || len(command) > v7FullGateCommandMaxBytes {
		return v7FullGateProviderRequest{}, "", fmt.Errorf("%w: provider command exceeds %d-byte bound", errV7FullGateProvider, v7FullGateCommandMaxBytes)
	}
	state, err := p.stateHandle(nil)
	if err != nil {
		return v7FullGateProviderRequest{}, "", err
	}
	recoveryRoot := p.recoveryRoot
	if strings.TrimSpace(recoveryRoot) == "" {
		recoveryRoot = filepath.Join(state.path, "full-gate-recovery")
	}
	recoveryRel, err := state.relative(recoveryRoot)
	if err != nil || recoveryRel != "full-gate-recovery" {
		return v7FullGateProviderRequest{}, "", fmt.Errorf("%w: provider recovery root is not the trusted state-root recovery directory", errV7FullGateProvider)
	}
	created, err := state.ensureDir(recoveryRel, 0o700)
	if err != nil {
		return v7FullGateProviderRequest{}, "", err
	}
	if created {
		if err := runV7FullGateDurabilityHook("recovery_root_created_synced"); err != nil {
			return v7FullGateProviderRequest{}, "", err
		}
	}
	preparationRel := "full-gate-preparing"
	created, err = state.ensureDir(preparationRel, 0o700)
	if err != nil {
		return v7FullGateProviderRequest{}, "", err
	}
	if created {
		if err := runV7FullGateDurabilityHook("preparation_root_created_synced"); err != nil {
			return v7FullGateProviderRequest{}, "", err
		}
	}
	var controlRel string
	for range 8 {
		controlRel = filepath.Join(preparationRel, "scope-"+strings.ToLower(newRecordID()))
		if err := state.root.Mkdir(controlRel, 0o700); err == nil {
			break
		} else if !errors.Is(err, os.ErrExist) {
			return v7FullGateProviderRequest{}, "", err
		}
		controlRel = ""
	}
	if controlRel == "" {
		return v7FullGateProviderRequest{}, "", fmt.Errorf("%w: allocate unique provider preparation scope", errV7FullGateProvider)
	}
	publishedRel := filepath.Join(recoveryRel, filepath.Base(controlRel))
	if err := state.syncDir(controlRel); err != nil {
		_ = state.removeAll(controlRel)
		return v7FullGateProviderRequest{}, "", err
	}
	if err := state.syncDir(preparationRel); err != nil {
		_ = state.removeAll(controlRel)
		return v7FullGateProviderRequest{}, "", err
	}
	if err := runV7FullGateDurabilityHook("preparation_dentry_synced"); err != nil {
		return v7FullGateProviderRequest{}, "", err
	}
	workspace, err = sandboxCanonicalPath(workspace)
	if err != nil {
		_ = state.removeAll(controlRel)
		return v7FullGateProviderRequest{}, "", err
	}
	p.mu.Lock()
	binding := p.binding
	p.mu.Unlock()
	if strings.TrimSpace(binding.ProjectID) == "" || strings.TrimSpace(binding.DepartureID) == "" || strings.TrimSpace(binding.CandidateDigest) == "" || strings.TrimSpace(binding.GateProfile) == "" || strings.TrimSpace(binding.ProviderProfile) == "" || strings.TrimSpace(binding.Toolchain) == "" || strings.TrimSpace(binding.ArtifactRef) == "" {
		_ = state.removeAll(controlRel)
		return v7FullGateProviderRequest{}, "", fmt.Errorf("%w: provider request is not bound to the current full-gate contract", errV7FullGateProvider)
	}
	artifactRel, err := state.relative(binding.ArtifactRef)
	if err != nil || filepath.Dir(artifactRel) != filepath.Join("artifacts", "promotion-gates") {
		_ = state.removeAll(controlRel)
		return v7FullGateProviderRequest{}, "", fmt.Errorf("%w: provider request artifact target escapes the trusted promotion artifact directory", errV7FullGateProvider)
	}
	request := v7FullGateProviderRequest{
		Schema: v7FullGateProviderSchema, Contract: v7FullGateIsolationContract,
		RunID: strings.ToLower(newRecordID()), Workspace: workspace, Command: command, ProjectID: binding.ProjectID, DepartureID: binding.DepartureID, CandidateDigest: binding.CandidateDigest, Profile: binding.GateProfile, ProviderProfile: binding.ProviderProfile, Toolchain: binding.Toolchain,
		ArtifactRef: binding.ArtifactRef, ResultPath: "/dev/fd/4", ProviderKind: p.kind, ProviderID: p.identity, ProviderPath: p.path, ExecutableID: p.executableIdentity,
		CandidateReadOnly: true, NetworkDenied: true, ControlEnvDenied: true,
		RuntimeDigest: p.runtimeDigest, ClientDigest: p.clientDigest, PolicyDigest: p.policyDigest, AttestationDigest: p.attestationDigest, RequiredCapabilities: append([]string(nil), p.capabilities...),
		ImplementationID: p.implementationID, CapabilitySchema: p.capabilitySchema, ExpectedImageOrVMID: p.imageOrVMID,
		MaxCommandBytes: v7FullGateCommandMaxBytes, MaxOutputBytes: v7FullGateOutputMaxBytes, MaxRuntimeMS: v7FullGateRuntimeLimit.Milliseconds(), MaxArtifactBytes: v7FullGateArtifactMaxBytes,
	}
	request.RequestDigest = v7FullGateRequestDigest(request)
	if err := state.writeDurable(filepath.Join(controlRel, "result.json"), nil, 0o600, false); err != nil {
		_ = state.removeAll(controlRel)
		return v7FullGateProviderRequest{}, "", err
	}
	if err := runV7FullGateDurabilityHook("result_reservation_synced"); err != nil {
		return v7FullGateProviderRequest{}, "", err
	}
	requestRel := filepath.Join(controlRel, "request.json")
	raw, err := json.Marshal(request)
	if err != nil {
		_ = state.removeAll(controlRel)
		return v7FullGateProviderRequest{}, "", err
	}
	if len(raw) > v7FullGateRequestMaxBytes {
		_ = state.removeAll(controlRel)
		return v7FullGateProviderRequest{}, "", fmt.Errorf("%w: provider request exceeds size bound", errV7FullGateProvider)
	}
	if err := state.writeDurable(requestRel, append(raw, '\n'), 0o600, false); err != nil {
		_ = state.removeAll(controlRel)
		return v7FullGateProviderRequest{}, "", err
	}
	if err := runV7FullGateDurabilityHook("reservation_synced"); err != nil {
		return v7FullGateProviderRequest{}, "", err
	}
	if err := state.rename(controlRel, publishedRel); err != nil {
		_ = state.removeAll(controlRel)
		return v7FullGateProviderRequest{}, "", err
	}
	if err := state.syncDir(recoveryRel); err != nil {
		return v7FullGateProviderRequest{}, "", err
	}
	if err := state.syncDir(preparationRel); err != nil {
		return v7FullGateProviderRequest{}, "", err
	}
	if err := runV7FullGateDurabilityHook("scope_published_synced"); err != nil {
		return v7FullGateProviderRequest{}, "", err
	}
	requestPath, err := state.absolute(filepath.Join(publishedRel, "request.json"))
	if err != nil {
		return v7FullGateProviderRequest{}, "", err
	}
	return request, requestPath, nil
}

// writeV7FullGateReservation makes the recovery record durable before a
// provider can create its scope. A crash after this returns is recoverable; a
// crash before it returns has not been granted create authority.
func writeV7FullGateReservation(path string, raw []byte) error {
	state, err := openV7FullGateStateRoot(filepath.Dir(filepath.Dir(path)))
	if err != nil {
		return err
	}
	defer state.Close()
	rel, err := state.relative(path)
	if err != nil {
		return err
	}
	return state.writeDurable(rel, raw, 0o600, false)
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
	state, err := p.stateHandle(scope)
	if err != nil {
		return v7FullGateProviderResult{}, err
	}
	if err := scope.bindState(state); err != nil {
		return v7FullGateProviderResult{}, err
	}
	if v7FullGateBeforeProviderLaunch != nil {
		if err := v7FullGateBeforeProviderLaunch("cleanup", scope.request.ProviderPath); err != nil {
			return v7FullGateProviderResult{}, err
		}
	}
	_, identity, err := verifyV7TrustedProviderExecutable(scope.request.ProviderPath)
	if err != nil {
		return v7FullGateProviderResult{}, err
	}
	if identity != scope.request.ExecutableID {
		return v7FullGateProviderResult{}, fmt.Errorf("%w: provider recovery executable identity changed", errV7FullGateProvider)
	}
	ctx, cancel := context.WithTimeout(context.Background(), v7FullGateCleanTimeout)
	defer cancel()
	var output v7GateBoundedOutput
	output.max = v7FullGateOutputMaxBytes
	if err := runV7FullGateProviderCleanup(ctx, scope.request.ProviderPath, scope, v7FullGateProviderEnv(), &output); err != nil {
		return v7FullGateProviderResult{}, fmt.Errorf("%w: provider cleanup for run %s failed: %v: %s", errV7FullGateProvider, scope.request.RunID, err, strings.TrimSpace(string(output.Bytes())))
	}
	result, err := scope.readResult()
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
	state, err := p.stateHandle(scope)
	if err != nil {
		return err
	}
	if err := scope.bindState(state); err != nil {
		return err
	}
	parent := filepath.Dir(scope.scopeRel)
	if err := state.removeAll(scope.scopeRel); err != nil {
		return fmt.Errorf("%w: remove certified provider scope: %v", errV7FullGateProvider, err)
	}
	if err := state.syncDir(parent); err != nil {
		return fmt.Errorf("%w: sync retired provider scope: %v", errV7FullGateProvider, err)
	}
	return runV7FullGateDurabilityHook("scope_retired_synced")
}

func v7FullGateProviderCommand(ctx context.Context, providerPath, operation string, scope *v7FullGateProviderScope) (*exec.Cmd, func(), error) {
	if err := v7FullGateProviderPlatformError(); err != nil {
		return nil, func() {}, err
	}
	requestFile, resultFile, scopeDir, err := scope.openTransport()
	if err != nil {
		return nil, func() {}, err
	}
	closeTransport := func() {
		_ = requestFile.Close()
		_ = scopeDir.Close()
		_ = resultFile.Close()
	}
	cmd := exec.CommandContext(ctx, providerPath, operation, "/dev/fd/3")
	cmd.ExtraFiles = []*os.File{requestFile, resultFile}
	return cmd, closeTransport, nil
}

func launchV7FullGateProviderCleanup(ctx context.Context, providerPath string, scope *v7FullGateProviderScope, env []string, output io.Writer) error {
	cmd, closeTransport, err := v7FullGateProviderCommand(ctx, providerPath, "--tusker-full-gate-cleanup", scope)
	if err != nil {
		return err
	}
	defer closeTransport()
	cmd.Env = env
	cmd.Stdout, cmd.Stderr = output, output
	// A cleanup wrapper can exit while a reparented descendant still owns the
	// inherited pipe. Bound that pipe drain as well as the wrapper process.
	cmd.WaitDelay = v7FullGateProviderPipeWaitDelay
	return cmd.Run()
}

func readV7FullGateProviderResult(request v7FullGateProviderRequest) (v7FullGateProviderResult, error) {
	state, err := openV7FullGateStateRoot(filepath.Dir(request.ResultPath))
	if err != nil {
		return v7FullGateProviderResult{}, err
	}
	defer state.Close()
	raw, err := state.readRegular(filepath.Base(request.ResultPath), v7FullGateResultMaxBytes, false)
	return decodeV7FullGateProviderResult(raw, err, request)
}

func readV7FullGateProviderResultAtRoot(state *v7FullGateStateRoot, resultRel string, request v7FullGateProviderRequest) (v7FullGateProviderResult, error) {
	raw, err := state.readRegular(resultRel, v7FullGateResultMaxBytes, false)
	return decodeV7FullGateProviderResult(raw, err, request)
}

func decodeV7FullGateProviderResult(raw []byte, readErr error, request v7FullGateProviderRequest) (v7FullGateProviderResult, error) {
	err := readErr
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
	parts := append([]string{request.Schema, request.Contract, request.RunID, request.Workspace, request.Command, request.ProjectID, request.DepartureID, request.CandidateDigest, request.Profile, request.ProviderProfile, request.Toolchain, request.ArtifactRef, request.ResultPath, request.ProviderKind, request.ProviderID, request.ProviderPath, request.ExecutableID, fmt.Sprint(request.CandidateReadOnly), fmt.Sprint(request.NetworkDenied), fmt.Sprint(request.ControlEnvDenied), request.RuntimeDigest, request.ClientDigest, request.PolicyDigest, request.AttestationDigest, request.ImplementationID, request.CapabilitySchema, request.ExpectedImageOrVMID, fmt.Sprint(request.MaxCommandBytes), fmt.Sprint(request.MaxOutputBytes), fmt.Sprint(request.MaxRuntimeMS), fmt.Sprint(request.MaxArtifactBytes)}, request.RequiredCapabilities...)
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

func v7FullGateProviderOutcomeJournalRelPath(departureID, requestDigest string) string {
	key := strings.TrimPrefix(requestDigest, "sha256:")
	departure := strings.TrimPrefix(v7FullGateTextDigest(departureID), "sha256:")[:16]
	return filepath.Join("full-gate-outcomes", departure+"-"+key+".json")
}

func v7FullGateProviderOutcomeJournalPath(stateRoot, departureID, requestDigest string) string {
	return filepath.Join(stateRoot, v7FullGateProviderOutcomeJournalRelPath(departureID, requestDigest))
}

func v7FullGateProviderOutcomeJournalDigest(journal v7FullGateProviderOutcomeJournal) string {
	receiptRaw, _ := json.Marshal(journal.Receipt)
	return v7FullGateTextDigest(strings.Join([]string{journal.Schema, journal.DepartureID, journal.RequestDigest, journal.ScopePath, journal.Request.RequestDigest, journal.Result.ResultDigest, string(receiptRaw), journal.ArtifactRef, journal.ArtifactDigest, fmt.Sprint(journal.Reconciled), journal.Action}, "\x00"))
}

func persistV7FullGateProviderOutcomeJournal(stateRoot string, scope *v7FullGateProviderScope, request v7FullGateProviderRequest, result v7FullGateProviderResult, receipt GateProviderReceipt) error {
	if strings.TrimSpace(stateRoot) == "" {
		stateRoot = filepath.Dir(filepath.Dir(filepath.Dir(scope.requestPath)))
	}
	state, err := openV7FullGateStateRoot(stateRoot)
	if err != nil {
		return err
	}
	defer state.Close()
	return persistV7FullGateProviderOutcomeJournalAtRoot(state, scope, request, result, receipt)
}

func persistV7FullGateProviderOutcomeJournalAtRoot(state *v7FullGateStateRoot, scope *v7FullGateProviderScope, request v7FullGateProviderRequest, result v7FullGateProviderResult, receipt GateProviderReceipt) error {
	if err := scope.bindState(state); err != nil {
		return err
	}
	created, err := state.ensureDir("full-gate-outcomes", 0o700)
	if err != nil {
		return err
	}
	if created {
		if err := runV7FullGateDurabilityHook("outcome_journal_root_created_synced"); err != nil {
			return err
		}
	}
	scopePath, err := state.absolute(scope.scopeRel)
	if err != nil {
		return err
	}
	journal := v7FullGateProviderOutcomeJournal{Schema: v7FullGateOutcomeJournalSchema, DepartureID: request.DepartureID, RequestDigest: request.RequestDigest, ScopePath: scopePath, Request: request, Result: result, Receipt: receipt, ArtifactRef: request.ArtifactRef}
	journal.JournalDigest = v7FullGateProviderOutcomeJournalDigest(journal)
	raw, err := json.Marshal(journal)
	if err != nil || len(raw) > v7FullGateResultMaxBytes {
		return fmt.Errorf("%w: provider outcome journal exceeds bound: %v", errV7FullGateProvider, err)
	}
	rel := v7FullGateProviderOutcomeJournalRelPath(request.DepartureID, request.RequestDigest)
	if existing, readErr := readV7FullGateProviderOutcomeJournalAtRoot(state, rel); readErr == nil {
		if existing.JournalDigest != journal.JournalDigest {
			return fmt.Errorf("%w: provider outcome journal identity conflict", errV7FullGateProvider)
		}
		return nil
	} else if !errors.Is(readErr, os.ErrNotExist) {
		return readErr
	}
	if err := state.writeJSON(rel, raw, false); err != nil {
		return err
	}
	return runV7FullGateDurabilityHook("outcome_journal_synced")
}

func readV7FullGateProviderOutcomeJournal(path string) (v7FullGateProviderOutcomeJournal, error) {
	stateRoot := filepath.Dir(filepath.Dir(path))
	state, err := openV7FullGateStateRoot(stateRoot)
	if err != nil {
		return v7FullGateProviderOutcomeJournal{}, err
	}
	defer state.Close()
	rel, err := state.relative(path)
	if err != nil {
		return v7FullGateProviderOutcomeJournal{}, err
	}
	return readV7FullGateProviderOutcomeJournalAtRoot(state, rel)
}

func readV7FullGateProviderOutcomeJournalAtRoot(state *v7FullGateStateRoot, rel string) (v7FullGateProviderOutcomeJournal, error) {
	raw, err := state.readRegular(rel, v7FullGateResultMaxBytes, false)
	if err != nil {
		return v7FullGateProviderOutcomeJournal{}, err
	}
	var journal v7FullGateProviderOutcomeJournal
	if err := json.Unmarshal(raw, &journal); err != nil {
		return v7FullGateProviderOutcomeJournal{}, fmt.Errorf("%w: invalid provider outcome journal", errV7FullGateProvider)
	}
	expectedReceipt := v7GateProviderReceiptForResult(journal.Request, journal.Result)
	artifactInvalid := journal.ArtifactRef != journal.Request.ArtifactRef || journal.ArtifactDigest != "" && (!v7FullGateDigest(journal.ArtifactDigest) || journal.ArtifactRef == "")
	if journal.Schema != v7FullGateOutcomeJournalSchema || journal.JournalDigest != v7FullGateProviderOutcomeJournalDigest(journal) || journal.RequestDigest != journal.Request.RequestDigest || journal.RequestDigest != v7FullGateRequestDigest(journal.Request) || journal.DepartureID != journal.Request.DepartureID || journal.Result.RequestDigest != journal.RequestDigest || journal.Result.ResultDigest != v7FullGateProviderResultDigest(journal.Result) || journal.Result.ReceiptDigest != v7FullGateReceiptDigest(journal.Request, journal.Result) || journal.Receipt != expectedReceipt || artifactInvalid {
		return v7FullGateProviderOutcomeJournal{}, fmt.Errorf("%w: invalid provider outcome journal", errV7FullGateProvider)
	}
	return journal, nil
}

func removeV7FullGateProviderOutcomeJournal(stateRoot string, receipt GateProviderReceipt) error {
	state, err := openV7FullGateStateRoot(stateRoot)
	if err != nil {
		return err
	}
	defer state.Close()
	return removeV7FullGateProviderOutcomeJournalAtRoot(state, receipt)
}

func removeV7FullGateProviderOutcomeJournalAtRoot(state *v7FullGateStateRoot, receipt GateProviderReceipt) error {
	rel := v7FullGateProviderOutcomeJournalRelPath(receipt.DepartureID, receipt.RequestDigest)
	if err := runV7FullGateDurabilityHook("before_outcome_journal_remove"); err != nil {
		return err
	}
	if err := state.remove(rel); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return state.syncDir(filepath.Dir(rel))
}

func persistV7FullGateProviderResultAtRoot(state *v7FullGateStateRoot, resultRel string, result v7FullGateProviderResult, expected os.FileInfo) error {
	raw, err := json.Marshal(result)
	if err != nil || len(raw) > v7FullGateResultMaxBytes {
		return fmt.Errorf("%w: normalized provider result exceeds bound: %v", errV7FullGateProvider, err)
	}
	payload := append(append([]byte(nil), raw...), '\n')
	if err := state.overwriteRegular(resultRel, payload, v7FullGateResultMaxBytes, expected); err != nil {
		if expected == nil && errors.Is(err, os.ErrNotExist) {
			return state.writeDurable(resultRel, payload, 0o600, false)
		}
		return err
	}
	return nil
}

func writeV7DurablePromotionArtifact(path string, raw []byte) error {
	stateRoot := filepath.Dir(filepath.Dir(filepath.Dir(path)))
	state, err := openV7FullGateStateRoot(stateRoot)
	if err != nil {
		return err
	}
	defer state.Close()
	return writeV7DurablePromotionArtifactAtRoot(state, path, raw)
}

func writeV7DurablePromotionArtifactAtRoot(state *v7FullGateStateRoot, path string, raw []byte) error {
	rel, err := state.relative(path)
	if err != nil || filepath.Dir(rel) != filepath.Join("artifacts", "promotion-gates") || filepath.Ext(rel) != ".log" {
		return fmt.Errorf("%w: promotion artifact target escapes the trusted artifact directory", errV7FullGateProvider)
	}
	if _, err := state.ensureDir(filepath.Join("artifacts", "promotion-gates"), 0o755); err != nil {
		return err
	}
	if err := state.writeDurable(rel, raw, 0o600, false); err != nil {
		return err
	}
	return runV7FullGateDurabilityHook("promotion_gate_artifact_synced")
}

func removeV7FullGatePromotionArtifact(stateRoot, path string) error {
	state, err := openV7FullGateStateRoot(stateRoot)
	if err != nil {
		return err
	}
	defer state.Close()
	rel, err := state.relative(path)
	if err != nil || filepath.Dir(rel) != filepath.Join("artifacts", "promotion-gates") || filepath.Ext(rel) != ".log" {
		return fmt.Errorf("%w: promotion artifact removal escapes the trusted artifact directory", errV7FullGateProvider)
	}
	if err := state.remove(rel); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return state.syncDir(filepath.Dir(rel))
}

// removeV7ProvablyUnboundPromotionArtifacts is deliberately conservative.
// Cancellation may race the service-owned outcome journal, so a file is a
// disposable temporary only when no valid journal or published recovery
// request claims it. Any malformed authority record preserves every file.
func removeV7ProvablyUnboundPromotionArtifacts(stateRoot string, paths []string) error {
	state, err := openV7FullGateStateRoot(stateRoot)
	if err != nil {
		return err
	}
	defer state.Close()
	claimed := make(map[string]struct{})
	journalEntries, err := state.readDir("full-gate-outcomes")
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if len(journalEntries) > v7FullGateJournalMaxEntries {
		return fmt.Errorf("%w: provider outcome journal count exceeds %d", errV7FullGateProvider, v7FullGateJournalMaxEntries)
	}
	for _, entry := range journalEntries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			return fmt.Errorf("%w: cannot prove artifacts unbound with invalid outcome journal %q", errV7FullGateProvider, entry.Name())
		}
		journal, readErr := readV7FullGateProviderOutcomeJournalAtRoot(state, filepath.Join("full-gate-outcomes", entry.Name()))
		if readErr != nil {
			return readErr
		}
		if ref := strings.TrimSpace(journal.Request.ArtifactRef); ref != "" {
			claimed[filepath.Clean(ref)] = struct{}{}
		}
	}
	scopeEntries, err := state.readDir("full-gate-recovery")
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if len(scopeEntries) > v7FullGateRecoveryMaxScopes {
		return fmt.Errorf("%w: provider recovery scope count exceeds %d", errV7FullGateProvider, v7FullGateRecoveryMaxScopes)
	}
	for _, entry := range scopeEntries {
		scopeRel := filepath.Join("full-gate-recovery", entry.Name())
		info, statErr := state.lstat(scopeRel)
		if statErr != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode()&0o022 != 0 || !strings.HasPrefix(entry.Name(), "scope-") {
			return fmt.Errorf("%w: cannot prove artifacts unbound with invalid recovery scope %q", errV7FullGateProvider, entry.Name())
		}
		request, readErr := readV7FullGateProviderRequestAtRoot(state, filepath.Join(scopeRel, "request.json"))
		if readErr != nil {
			return readErr
		}
		if ref := strings.TrimSpace(request.ArtifactRef); ref != "" {
			claimed[filepath.Clean(ref)] = struct{}{}
		}
	}
	syncArtifactDir := false
	seen := make(map[string]struct{})
	for _, path := range paths {
		path = filepath.Clean(strings.TrimSpace(path))
		if path == "." || path == "" {
			continue
		}
		if _, duplicate := seen[path]; duplicate {
			continue
		}
		seen[path] = struct{}{}
		if _, bound := claimed[path]; bound {
			continue
		}
		rel, relErr := state.relative(path)
		if relErr != nil || filepath.Dir(rel) != filepath.Join("artifacts", "promotion-gates") || filepath.Ext(rel) != ".log" {
			return fmt.Errorf("%w: promotion artifact removal escapes the trusted artifact directory", errV7FullGateProvider)
		}
		if removeErr := state.remove(rel); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			return removeErr
		}
		syncArtifactDir = true
	}
	if syncArtifactDir {
		return state.syncDir(filepath.Join("artifacts", "promotion-gates"))
	}
	return nil
}

func ensureV7FullGateJournalArtifactAtRoot(state *v7FullGateStateRoot, journal *v7FullGateProviderOutcomeJournal) error {
	if state == nil || journal == nil {
		return fmt.Errorf("%w: provider outcome artifact binding is unavailable", errV7FullGateProvider)
	}
	artifactRef := strings.TrimSpace(journal.Request.ArtifactRef)
	if artifactRef == "" {
		// Legacy fixtures and already-actionable journals predate artifact
		// binding. New requests cannot omit it.
		return nil
	}
	artifactRel, err := state.relative(artifactRef)
	if err != nil || filepath.Dir(artifactRel) != filepath.Join("artifacts", "promotion-gates") || filepath.Ext(artifactRel) != ".log" {
		return fmt.Errorf("%w: provider outcome artifact escapes the trusted artifact directory", errV7FullGateProvider)
	}
	raw, err := state.readRegular(artifactRel, v7FullGateArtifactMaxBytes, false)
	if errors.Is(err, os.ErrNotExist) {
		var recovered bytes.Buffer
		fmt.Fprintf(&recovered, "# recovered_provider_outcome=%s\n# request_digest=%s\n", journal.Result.Outcome, journal.RequestDigest)
		recovered.WriteString(journal.Result.Output)
		if journal.Result.Error != "" {
			fmt.Fprintf(&recovered, "\n# provider_error=%s\n", safePacketText(journal.Result.Error, 4096))
		}
		if writeErr := writeV7DurablePromotionArtifactAtRoot(state, artifactRef, recovered.Bytes()); writeErr != nil {
			return writeErr
		}
		raw, err = state.readRegular(artifactRel, v7FullGateArtifactMaxBytes, false)
	}
	if err != nil {
		return fmt.Errorf("%w: read bound provider outcome artifact: %v", errV7FullGateProvider, err)
	}
	digest := v7FullGateTextDigest(string(raw))
	if journal.ArtifactDigest != "" {
		if journal.ArtifactRef != artifactRef || journal.ArtifactDigest != digest {
			return fmt.Errorf("%w: provider outcome artifact binding conflict", errV7FullGateProvider)
		}
		return nil
	}
	if journal.ArtifactRef != artifactRef {
		return fmt.Errorf("%w: provider outcome artifact target conflict", errV7FullGateProvider)
	}
	journal.ArtifactDigest = digest
	journal.JournalDigest = v7FullGateProviderOutcomeJournalDigest(*journal)
	encoded, err := json.Marshal(journal)
	if err != nil || len(encoded) > v7FullGateResultMaxBytes {
		return fmt.Errorf("%w: provider outcome artifact journal exceeds bound: %v", errV7FullGateProvider, err)
	}
	rel := v7FullGateProviderOutcomeJournalRelPath(journal.DepartureID, journal.RequestDigest)
	if err := state.writeJSON(rel, encoded, true); err != nil {
		return err
	}
	return runV7FullGateDurabilityHook("outcome_artifact_bound")
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
	state, err := openV7FullGateStateRoot(stateRoot)
	if err != nil {
		return err
	}
	defer state.Close()
	recoveryRel := "full-gate-recovery"
	preparationRel := "full-gate-preparing"
	if err := recoverV7FullGateProviderPreparationsAtRoot(state, preparationRel); err != nil {
		return err
	}
	journalRel := "full-gate-outcomes"
	journalEntries, err := state.readDir(journalRel)
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
				rel := filepath.Join(journalRel, entry.Name())
				info, statErr := state.lstat(rel)
				if statErr != nil || !info.Mode().IsRegular() || info.Mode()&0o022 != 0 {
					return fmt.Errorf("%w: invalid provider outcome journal temporary %q", errV7FullGateProvider, entry.Name())
				}
				if err := state.remove(rel); err != nil {
					return fmt.Errorf("%w: retire provider outcome journal temporary %q: %v", errV7FullGateProvider, entry.Name(), err)
				}
				removedTemp = true
				continue
			}
			filtered = append(filtered, entry)
		}
		if removedTemp {
			if err := state.syncDir(journalRel); err != nil {
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
		pathRel := filepath.Join(journalRel, entry.Name())
		journal, readErr := readV7FullGateProviderOutcomeJournalAtRoot(state, pathRel)
		if readErr != nil {
			return readErr
		}
		if _, duplicate := journals[journal.RequestDigest]; duplicate {
			return fmt.Errorf("%w: duplicate provider outcome journal request digest", errV7FullGateProvider)
		}
		canonicalRel := v7FullGateProviderOutcomeJournalRelPath(journal.DepartureID, journal.RequestDigest)
		if filepath.Clean(pathRel) != filepath.Clean(canonicalRel) {
			return fmt.Errorf("%w: non-canonical provider outcome journal filename", errV7FullGateProvider)
		}
		if strings.TrimSpace(journal.ScopePath) == "" {
			if !journal.Reconciled {
				return fmt.Errorf("%w: unreconciled provider outcome journal has no recovery scope", errV7FullGateProvider)
			}
		} else if !validV7FullGateRecoveryScopePathAtRoot(state, recoveryRel, journal.ScopePath) {
			return fmt.Errorf("%w: journal scope escapes recovery root", errV7FullGateProvider)
		}
		journals[journal.RequestDigest] = journal
		journalPaths[journal.RequestDigest] = pathRel
		journalOrder = append(journalOrder, journal.RequestDigest)
	}
	scopeEntries, err := state.readDir(recoveryRel)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("%w: read provider recovery records: %v", errV7FullGateProvider, err)
	}
	if len(scopeEntries) > v7FullGateRecoveryMaxScopes {
		return fmt.Errorf("%w: provider recovery scope count exceeds %d", errV7FullGateProvider, v7FullGateRecoveryMaxScopes)
	}
	for _, entry := range scopeEntries {
		dirRel := filepath.Join(recoveryRel, entry.Name())
		info, statErr := state.lstat(dirRel)
		if statErr != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode()&0o022 != 0 || !strings.HasPrefix(entry.Name(), "scope-") {
			return fmt.Errorf("%w: invalid provider recovery entry %q", errV7FullGateProvider, entry.Name())
		}
		dir, absErr := state.absolute(dirRel)
		if absErr != nil {
			return absErr
		}
		requestRel := filepath.Join(dirRel, "request.json")
		requestPath, absErr := state.absolute(requestRel)
		if absErr != nil {
			return absErr
		}
		request, readErr := readV7FullGateProviderRequestAtRoot(state, requestRel)
		if readErr != nil {
			return readErr
		}
		scope := &v7FullGateProviderScope{request: request, requestPath: requestPath, state: state, scopeRel: dirRel, requestRel: requestRel, resultRel: filepath.Join(dirRel, "result.json")}
		journal, ok := journals[request.RequestDigest]
		if !ok {
			result, resultErr := scope.readResult()
			if resultErr != nil || result.State != "cleaned" {
				_, executableID, verifyErr := verifyV7TrustedProviderExecutable(request.ProviderPath)
				if verifyErr != nil {
					return verifyErr
				}
				if executableID != request.ExecutableID {
					return fmt.Errorf("%w: provider recovery executable unavailable for %q", errV7FullGateProvider, entry.Name())
				}
				provider := &v7ExternalFullGateProvider{path: request.ProviderPath, executableIdentity: request.ExecutableID, stateRoot: state.path, state: state}
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
			if err := persistV7FullGateProviderOutcomeJournalAtRoot(state, scope, request, result, receipt); err != nil {
				return err
			}
			if normalized {
				if err := scope.persistResult(result); err != nil {
					return err
				}
			}
			journalPath := v7FullGateProviderOutcomeJournalRelPath(request.DepartureID, request.RequestDigest)
			journal, readErr = readV7FullGateProviderOutcomeJournalAtRoot(state, journalPath)
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
	artifactOwners := make(map[string]string)
	for digest, journal := range journals {
		ref := strings.TrimSpace(journal.Request.ArtifactRef)
		if ref == "" {
			continue
		}
		ref = filepath.Clean(ref)
		if owner, duplicate := artifactOwners[ref]; duplicate && owner != digest {
			return fmt.Errorf("%w: multiple provider outcome journals share one artifact target", errV7FullGateProvider)
		}
		artifactOwners[ref] = digest
	}
	sort.Strings(journalOrder)
	for _, digest := range journalOrder {
		journal := journals[digest]
		if journal.Reconciled {
			if strings.TrimSpace(journal.ScopePath) == "" {
				continue
			}
			if err := retireV7FullGateRecoveredScopeAtRoot(state, journal); err != nil {
				return err
			}
			journal.ScopePath = ""
			journal.JournalDigest = v7FullGateProviderOutcomeJournalDigest(journal)
			raw, marshalErr := json.Marshal(journal)
			if marshalErr != nil {
				return marshalErr
			}
			if err := state.writeJSON(journalPaths[digest], raw, true); err != nil {
				return err
			}
			continue
		}
		if err := runV7FullGateRecoveryHook("outcome_journal_ready"); err != nil {
			return err
		}
		if err := ensureV7FullGateJournalArtifactAtRoot(state, &journal); err != nil {
			return err
		}
		if err := reconcileV7FullGateProviderOutcome(state, store, &journal); err != nil {
			return err
		}
		if err := ensureV7FullGateJournalArtifactAtRoot(state, &journal); err != nil {
			return err
		}
		if journal.Reconciled {
			journal.JournalDigest = v7FullGateProviderOutcomeJournalDigest(journal)
			raw, marshalErr := json.Marshal(journal)
			if marshalErr != nil {
				return marshalErr
			}
			if err := state.writeJSON(journalPaths[digest], raw, true); err != nil {
				return err
			}
			if err := runV7FullGateRecoveryHook("outcome_reconciled_synced"); err != nil {
				return err
			}
			if err := retireV7FullGateRecoveredScopeAtRoot(state, journal); err != nil {
				return err
			}
			journal.ScopePath = ""
			journal.JournalDigest = v7FullGateProviderOutcomeJournalDigest(journal)
			raw, marshalErr = json.Marshal(journal)
			if marshalErr != nil {
				return marshalErr
			}
			if err := state.writeJSON(journalPaths[digest], raw, true); err != nil {
				return err
			}
			continue
		}
		if err := retireV7FullGateRecoveredOutcomeAtRoot(state, journal); err != nil {
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
	stateRoot := filepath.Dir(root)
	state, err := openV7FullGateStateRoot(stateRoot)
	if err != nil {
		return err
	}
	defer state.Close()
	rel, err := state.relative(root)
	if err != nil {
		return err
	}
	return recoverV7FullGateProviderPreparationsAtRoot(state, rel)
}

func recoverV7FullGateProviderPreparationsAtRoot(state *v7FullGateStateRoot, rootRel string) error {
	entries, err := state.readDir(rootRel)
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
		pathRel := filepath.Join(rootRel, entry.Name())
		info, statErr := state.lstat(pathRel)
		if statErr != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode()&0o022 != 0 || !strings.HasPrefix(entry.Name(), "scope-") {
			return fmt.Errorf("%w: invalid provider preparation %q", errV7FullGateProvider, entry.Name())
		}
		if err := state.removeAll(pathRel); err != nil {
			return fmt.Errorf("%w: retire provider preparation %q: %v", errV7FullGateProvider, entry.Name(), err)
		}
	}
	if err := state.syncDir(rootRel); err != nil {
		return fmt.Errorf("%w: sync retired provider preparations: %v", errV7FullGateProvider, err)
	}
	if len(entries) == 0 {
		return nil
	}
	return runV7FullGateRecoveryHook("preparation_retired_synced")
}

func readV7FullGateProviderRequestAtRoot(state *v7FullGateStateRoot, requestRel string) (v7FullGateProviderRequest, error) {
	raw, err := state.readRegular(requestRel, v7FullGateRequestMaxBytes, false)
	return decodeV7FullGateProviderRequest(raw, err)
}

func decodeV7FullGateProviderRequest(raw []byte, readErr error) (v7FullGateProviderRequest, error) {
	err := readErr
	if err != nil {
		return v7FullGateProviderRequest{}, fmt.Errorf("%w: read provider recovery request: %v", errV7FullGateProvider, err)
	}
	var request v7FullGateProviderRequest
	if err := json.Unmarshal(raw, &request); err != nil || request.RequestDigest != v7FullGateRequestDigest(request) || request.Schema != v7FullGateProviderSchema || request.Contract != v7FullGateIsolationContract {
		return v7FullGateProviderRequest{}, fmt.Errorf("%w: invalid provider recovery request", errV7FullGateProvider)
	}
	return request, nil
}

func reconcileV7FullGateProviderOutcome(state *v7FullGateStateRoot, store *RuntimeStore, journal *v7FullGateProviderOutcomeJournal) error {
	if store == nil {
		return fmt.Errorf("%w: provider recovery store unavailable", errV7FullGateProvider)
	}
	run, err := store.FindDepartureRun(journal.DepartureID)
	if err != nil {
		return err
	}
	if run != nil {
		published, publishedErr := v7DepartureHasPublishedProviderOutcome(*run, *journal)
		if publishedErr != nil {
			return publishedErr
		}
		if published {
			// The departure CAS already won. Recovery owns only retirement now;
			// re-adjudicating proof or ledger state here would let later
			// contract drift wedge startup or destroy an already-published
			// ordinary-red repair or successful flake rerun.
			return runV7FullGateRecoveryHook("outcome_target_persisted")
		}
	}
	if journal.Result.Outcome == v7FullGateOutcomePassed {
		if run != nil {
			if err := validateV7FullGateJournalGreen(state, store, *run, *journal); err == nil {
				if ledgerErr := reconcileV7FullGateGreenLedger(store, *journal); ledgerErr != nil {
					if errors.Is(ledgerErr, errV7FullGateLedgerConflict) {
						return blockV7FullGateJournalOutcome(store, run, journal, "conflicting_green_ledger")
					}
					return ledgerErr
				}
				return runV7FullGateRecoveryHook("outcome_target_persisted")
			}
		}
		return blockV7FullGateJournalOutcome(store, run, journal, "green_outcome_no_longer_matches_current_contract")
	}
	return blockV7FullGateJournalOutcome(store, run, journal, "recovered_"+string(journal.Result.Outcome))
}

func reconcileV7FullGateGreenLedger(store *RuntimeStore, journal v7FullGateProviderOutcomeJournal) error {
	existing, err := store.FindGateLedger(journal.Receipt.ProjectID, journal.Receipt.CandidateDigest, journal.Request.Command, journal.Receipt.Profile, journal.Receipt.Toolchain)
	if err != nil {
		return err
	}
	if existing != nil {
		if existing.ProviderReceipt == nil || *existing.ProviderReceipt != journal.Receipt {
			return fmt.Errorf("%w: conflicting green ledger receipt", errV7FullGateLedgerConflict)
		}
		return nil
	}
	receipt := journal.Receipt
	return store.RecordGateLedger(GateLedgerEntry{ProjectID: receipt.ProjectID, TreeHash: receipt.CandidateDigest, Command: journal.Request.Command, Profile: receipt.Profile, Toolchain: receipt.Toolchain, Host: runtimeLeaseHost(), DurationMS: journal.Result.RuntimeMS, ProviderReceipt: &receipt})
}

func v7DepartureHasPublishedProviderOutcome(run DepartureRun, journal v7FullGateProviderOutcomeJournal) (bool, error) {
	published := false
	for _, receipt := range run.Gate.ProviderOutcomes {
		if receipt == journal.Receipt {
			published = true
			break
		}
	}
	if !published {
		return false, nil
	}
	artifactRef := strings.TrimSpace(journal.ArtifactRef)
	if artifactRef == "" {
		return true, nil
	}
	for _, ref := range appendV7UniqueArtifactRefs(appendV7UniqueArtifactRefs(append([]string(nil), run.Gate.ArtifactRefs...), run.Gate.ArtifactRef), run.Gate.Failure.ArtifactRefs...) {
		if filepath.Clean(ref) == filepath.Clean(artifactRef) {
			return true, nil
		}
	}
	return false, fmt.Errorf("%w: published provider outcome omits its journal-bound artifact", errV7FullGateProvider)
}

func appendV7UniqueArtifactRefs(refs []string, values ...string) []string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		duplicate := false
		for _, existing := range refs {
			if filepath.Clean(existing) == filepath.Clean(value) {
				duplicate = true
				break
			}
		}
		if !duplicate {
			refs = append(refs, value)
		}
	}
	return refs
}

func validateV7FullGateJournalGreen(state *v7FullGateStateRoot, store *RuntimeStore, run DepartureRun, journal v7FullGateProviderOutcomeJournal) error {
	if run.ProjectID != journal.Receipt.ProjectID || run.ID != journal.DepartureID || run.Candidate.CandidateSHA == "" {
		return errors.New("departure identity mismatch")
	}
	loadedProjects, err := loadRegisteredProjects(store, registeredProjectLoadOptions{
		LoadDisabled: true,
		ProjectID:    run.ProjectID,
	})
	if err != nil {
		return err
	}
	if len(loadedProjects) != 1 {
		return errors.New("registered project unavailable")
	}
	loadedProject := loadedProjects[0]
	if loadedProject.LoadError != nil {
		return loadedProject.LoadError
	}
	project := loadedProject.Project
	wf := loadedProject.Workflow
	policy, err := scheduledPromotionGatePolicy(project.VaultRoot, wf.Data)
	if err != nil || policy.Profile != journal.Receipt.Profile || policy.IsolationProvider != journal.Receipt.ProviderProfile {
		return errors.New("current gate profile or toolchain mismatch")
	}
	trusted, path, identity, executableIdentity, err := resolveV7TrustedFullGateProviderAtRoot(policy.IsolationProvider, state)
	baseToolchain := scheduledPromotionToolchainFingerprint(project.RepoRoot, policy.HarvestCommands)
	currentToolchain := departureFingerprint(v7FullGateIsolationContract, identity, baseToolchain)
	if err != nil || baseToolchain == "" || v7PathWithin(project.RepoRoot, path) || currentToolchain != journal.Receipt.Toolchain {
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
	provider := &v7ExternalFullGateProvider{
		path: path, kind: trusted.Kind, identity: identity, executableIdentity: executableIdentity,
		runtimeDigest: trusted.RuntimeDigest, clientDigest: trusted.ClientDigest, policyDigest: trusted.PolicyDigest,
		attestationDigest: trusted.AttestationDigest, imageOrVMID: trusted.ImageOrVMID, profile: policy.IsolationProvider,
	}
	if !provider.MatchesGateProviderReceipt(&journal.Receipt) {
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
	receiptPresent := false
	for _, receipt := range run.Gate.ProviderOutcomes {
		if receipt == journal.Receipt {
			receiptPresent = true
			break
		}
	}
	if !receiptPresent && len(run.Gate.ProviderOutcomes) >= v7PromotionGateMaxReceipts {
		return fmt.Errorf("%w: recovered provider outcome count exceeds bound", errV7FullGateProvider)
	}
	next := *run
	next.Gate.ArtifactRefs = append([]string(nil), run.Gate.ArtifactRefs...)
	next.Gate.ProviderOutcomes = append([]GateProviderReceipt(nil), run.Gate.ProviderOutcomes...)
	if !receiptPresent {
		next.Gate.ProviderOutcomes = append(next.Gate.ProviderOutcomes, journal.Receipt)
	}
	next.Gate.Status = string(journal.Result.Outcome)
	artifactRefs := append([]string(nil), run.Gate.Failure.ArtifactRefs...)
	if journal.ArtifactRef != "" {
		next.Gate.ArtifactRef = journal.ArtifactRef
		next.Gate.ArtifactRefs = appendV7UniqueArtifactRefs(next.Gate.ArtifactRefs, journal.ArtifactRef)
		artifactRefs = appendV7UniqueArtifactRefs(artifactRefs, journal.ArtifactRef)
	}
	next.Gate.Failure = DepartureFailure{Class: "provider", Identity: safePacketText(reason, 256), Action: "operator_inspect_provider_outcome", ArtifactRefs: artifactRefs}
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
		if latest != nil {
			published, publishErr := v7DepartureHasPublishedProviderOutcome(*latest, *journal)
			if publishErr != nil {
				return publishErr
			}
			if published {
				return nil
			}
		}
		return fmt.Errorf("%w: departure outcome recovery CAS conflict", errV7FullGateProvider)
	}
	return runV7FullGateRecoveryHook("outcome_target_persisted")
}

func retireV7FullGateRecoveredOutcome(stateRoot string, journal v7FullGateProviderOutcomeJournal) error {
	state, err := openV7FullGateStateRoot(stateRoot)
	if err != nil {
		return err
	}
	defer state.Close()
	return retireV7FullGateRecoveredOutcomeAtRoot(state, journal)
}

func retireV7FullGateRecoveredOutcomeAtRoot(state *v7FullGateStateRoot, journal v7FullGateProviderOutcomeJournal) error {
	if err := retireV7FullGateRecoveredScopeAtRoot(state, journal); err != nil {
		return err
	}
	if err := removeV7FullGateProviderOutcomeJournalAtRoot(state, journal.Receipt); err != nil {
		return err
	}
	return runV7FullGateRecoveryHook("outcome_scope_retired")
}

func retireV7FullGateRecoveredScope(stateRoot string, journal v7FullGateProviderOutcomeJournal) error {
	state, err := openV7FullGateStateRoot(stateRoot)
	if err != nil {
		return err
	}
	defer state.Close()
	return retireV7FullGateRecoveredScopeAtRoot(state, journal)
}

func retireV7FullGateRecoveredScopeAtRoot(state *v7FullGateStateRoot, journal v7FullGateProviderOutcomeJournal) error {
	if strings.TrimSpace(journal.ScopePath) != "" {
		if !validV7FullGateRecoveryScopePathAtRoot(state, "full-gate-recovery", journal.ScopePath) {
			return fmt.Errorf("%w: journal scope escapes recovery root", errV7FullGateProvider)
		}
		scopeRel, err := state.relative(journal.ScopePath)
		if err != nil {
			return err
		}
		requestRel := filepath.Join(scopeRel, "request.json")
		if _, statErr := state.lstat(requestRel); statErr == nil {
			requestPath, absErr := state.absolute(requestRel)
			if absErr != nil {
				return absErr
			}
			provider := &v7ExternalFullGateProvider{stateRoot: state.path, state: state}
			if err := provider.completeScope(&v7FullGateProviderScope{request: journal.Request, requestPath: requestPath, state: state, scopeRel: scopeRel, requestRel: requestRel, resultRel: filepath.Join(scopeRel, "result.json")}); err != nil {
				return err
			}
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return statErr
		}
	}
	return nil
}

func validV7FullGateRecoveryScopePath(root, scope string) bool {
	rootAbs, err := filepath.Abs(filepath.Clean(root))
	if err != nil {
		return false
	}
	state, err := openV7FullGateStateRoot(filepath.Dir(rootAbs))
	if err != nil {
		return false
	}
	defer state.Close()
	recoveryRel, err := state.relative(rootAbs)
	return err == nil && validV7FullGateRecoveryScopePathAtRoot(state, recoveryRel, scope)
}

func validV7FullGateRecoveryScopePathAtRoot(state *v7FullGateStateRoot, recoveryRel, scope string) bool {
	if state == nil || !filepath.IsAbs(scope) || filepath.Clean(scope) != scope {
		return false
	}
	scopeRel, err := state.relative(scope)
	if err != nil || filepath.Dir(scopeRel) != recoveryRel || !strings.HasPrefix(filepath.Base(scopeRel), "scope-") {
		return false
	}
	info, statErr := state.lstat(scopeRel)
	if statErr == nil {
		return info.IsDir() && info.Mode()&os.ModeSymlink == 0 && info.Mode()&0o022 == 0
	}
	return errors.Is(statErr, os.ErrNotExist)
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
