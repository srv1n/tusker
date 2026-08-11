// Package acp contains the deliberately small, provider-neutral ACP v1
// client.  It owns transport safety only; task, lease, policy, and evidence
// authority remain in the caller.
package acp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
)

const (
	protocolVersion          = 1
	maxACPIdentifierBytes    = 512
	maxACPAuthMethods        = 64
	maxACPConfigOptions      = 64
	maxACPConfigValues       = 256
	maxACPPermissionOptions  = 32
	maxACPPermissionRawInput = 256 << 10
)

var (
	ErrClosed          = errors.New("acp client is closed")
	ErrPoisoned        = errors.New("acp client is poisoned")
	ErrProtocol        = errors.New("acp protocol failure")
	ErrDeliveryUnknown = errors.New("acp prompt delivery is unknown")
	ErrNotInitialized  = errors.New("acp client is not initialized")
	ErrNoSession       = errors.New("acp session is not created")
	// ErrLoadSessionUnsupported and ErrResumeSessionUnsupported are distinct
	// because ACP v1 advertises these lifecycle operations independently.
	// Callers must choose an explicit recovery policy; the client never falls
	// back from either operation to session/new.
	ErrLoadSessionUnsupported   = errors.New("acp session/load capability is not negotiated")
	ErrResumeSessionUnsupported = errors.New("acp session/resume capability is not negotiated")
	ErrPromptActive             = errors.New("acp prompt already in flight")
	ErrNoPrompt                 = errors.New("acp prompt is not in flight")
)

// Limits are finite safety ceilings. Zero values are replaced by defaults.
type Limits struct {
	MaxFrameBytes      int
	MaxPendingRequests int
	// MaxUpdates bounds queued, unread session/update notifications. It is not
	// a lifetime event count: valid adapters may stream many small deltas.
	MaxUpdates     int
	MaxUpdateBytes int
}

// Timeouts are finite deadlines. Zero values are replaced by defaults.
type Timeouts struct {
	Initialize  time.Duration
	Request     time.Duration
	Prompt      time.Duration
	Stall       time.Duration
	CancelDrain time.Duration
}

// PermissionHandler handles one provider permission request. It must be a
// bounded, context-compliant policy evaluation with no provider I/O. Returning
// a decision other than AllowOnce is fail-closed. A nil handler rejects.
type PermissionHandler func(context.Context, PermissionRequest) (PermissionDecision, error)

// ProcessValidator runs immediately after the ACP adapter starts and before
// the client exposes it to callers. It lets a process supervisor verify that
// the adapter stayed inside its already-established containment boundary.
// The validator must not perform provider I/O or mutate task authority.
type ProcessValidator func(pid int) error

// Config describes one direct, preinstalled ACP adapter process.
type Config struct {
	// Argv is passed directly to exec.Command. Shells and package installers are
	// intentionally not involved. Argv[0] and CWD must be absolute paths.
	Argv []string
	CWD  string
	// Env replaces the process environment when non-nil. An empty, non-nil
	// environment is therefore a useful allowlist.
	Env []string
	// Stderr is a caller-owned bounded/redacting diagnostic sink. ACP stdout
	// remains protocol-only and is never mixed with diagnostics.
	Stderr            io.Writer
	Limits            Limits
	Timeouts          Timeouts
	PermissionHandler PermissionHandler
	// ValidateProcess is an optional containment check invoked synchronously
	// after exec starts. A rejection kills and reaps the child before Start
	// returns, so no unverified ACP process can become live.
	ValidateProcess ProcessValidator
}

func (c Config) withDefaults() Config {
	if c.Limits.MaxFrameBytes <= 0 {
		c.Limits.MaxFrameBytes = 4 << 20
	}
	if c.Limits.MaxPendingRequests <= 0 {
		c.Limits.MaxPendingRequests = 64
	}
	if c.Limits.MaxUpdates <= 0 {
		c.Limits.MaxUpdates = 256
	}
	if c.Limits.MaxUpdateBytes <= 0 {
		c.Limits.MaxUpdateBytes = 32 << 20
	}
	if c.Timeouts.Initialize <= 0 {
		c.Timeouts.Initialize = 30 * time.Second
	}
	if c.Timeouts.Request <= 0 {
		c.Timeouts.Request = 30 * time.Second
	}
	if c.Timeouts.Prompt <= 0 {
		c.Timeouts.Prompt = 10 * time.Minute
	}
	if c.Timeouts.Stall <= 0 {
		c.Timeouts.Stall = 2 * time.Minute
	}
	if c.Timeouts.CancelDrain <= 0 {
		c.Timeouts.CancelDrain = 5 * time.Second
	}
	return c
}

// DeliveryPhase is a monotonic prompt transmission ledger.
type DeliveryPhase string

const (
	DeliveryNotSent          DeliveryPhase = "not_sent"
	DeliveryWriteStarted     DeliveryPhase = "write_started"
	DeliveryWriteComplete    DeliveryPhase = "write_complete"
	DeliveryResponseSeen     DeliveryPhase = "response_seen"
	DeliveryTerminalReceived DeliveryPhase = "terminal_received"
)

// Outcome is the transport-level result of one ACP prompt turn.
type Outcome string

const (
	OutcomeCompleted        Outcome = "completed"
	OutcomeBudgetExceeded   Outcome = "budget_exceeded"
	OutcomeTurnCapExhausted Outcome = "turn_cap_exhausted"
	OutcomeRefused          Outcome = "refused"
	OutcomeCancelled        Outcome = "cancelled"
	OutcomeProtocolFailed   Outcome = "protocol_failed"
	OutcomeTimedOut         Outcome = "timed_out"
	OutcomeDeliveryUnknown  Outcome = "delivery_unknown"
	OutcomePoisoned         Outcome = "poisoned"
)

// OutcomeError preserves the delivery-aware outcome for callers and errors.Is
// remains useful for the common unknown-delivery and poisoned cases.
type OutcomeError struct {
	Outcome  Outcome
	Delivery DeliveryPhase
	Err      error
}

func (e *OutcomeError) Error() string {
	if e == nil {
		return ""
	}
	if e.Err == nil {
		return fmt.Sprintf("acp outcome %s (%s)", e.Outcome, e.Delivery)
	}
	return fmt.Sprintf("acp outcome %s (%s): %v", e.Outcome, e.Delivery, e.Err)
}

func (e *OutcomeError) Unwrap() error { return e.Err }

// Is reports a matching typed sentinel for OutcomeError.
func (e *OutcomeError) Is(target error) bool {
	if target == ErrDeliveryUnknown {
		return e.Outcome == OutcomeDeliveryUnknown
	}
	if target == ErrPoisoned {
		return e.Outcome == OutcomePoisoned
	}
	return false
}

type ClientCapabilities struct{}

type AgentInfo struct {
	Name    string `json:"name,omitempty"`
	Title   string `json:"title,omitempty"`
	Version string `json:"version,omitempty"`
}

// AgentCapabilities retains the exact negotiated JSON and exposes conservative
// normalized feature checks without conflating load and resume.
type AgentCapabilities struct {
	Raw           json.RawMessage
	LoadSession   bool
	ResumeSession bool
}

type AuthMethod struct {
	ID          string `json:"id,omitempty"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	Type        string `json:"type,omitempty"`
}

type AuthenticationReceipt struct {
	MethodID   string
	MethodType string
}

type InitializeResult struct {
	ProtocolVersion   int
	AgentInfo         AgentInfo
	AgentCapabilities AgentCapabilities
	AuthMethods       []AuthMethod
}

type ConfigOptionValue struct {
	Value       string `json:"value"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// ConfigOption is the ungrouped-select phase-one projection. Boolean, grouped
// select, and future option variants are skipped locally without poisoning;
// ClientCapabilities currently makes no config-option support claim.
type ConfigOption struct {
	ID           string              `json:"id"`
	Name         string              `json:"name"`
	Description  string              `json:"description,omitempty"`
	Category     string              `json:"category,omitempty"`
	Type         string              `json:"type"`
	CurrentValue string              `json:"currentValue"`
	Options      []ConfigOptionValue `json:"options"`
}

type Session struct {
	ID            string
	ConfigOptions []ConfigOption
}

type PromptResult struct {
	Outcome    Outcome
	StopReason string
	Delivery   DeliveryPhase
	SessionID  string
	TurnID     string
	Raw        json.RawMessage
}

// Update is an execution observation. It has no task or authorization power.
type Update struct {
	Sequence uint64
	Method   string
	Params   json.RawMessage
}

type PermissionDecision string

const (
	AllowOnce PermissionDecision = "allow_once"
	Reject    PermissionDecision = "reject"
	Cancelled PermissionDecision = "cancelled"
)

type PermissionOption struct {
	ID   string `json:"optionId,omitempty"`
	Kind string `json:"kind,omitempty"`
	Name string `json:"name,omitempty"`
}

type PermissionRequest struct {
	SessionID  string
	ToolCallID string
	ToolKind   string
	RawInput   json.RawMessage
	// Reason is retained for source compatibility but ACP v1 does not define a
	// top-level permission reason. Provider-specific inference is forbidden.
	Reason  string
	Options []PermissionOption
	// Raw is the complete already-frame-bounded permission request.
	Raw json.RawMessage
}

type rpcError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *rpcError) Error() string { return fmt.Sprintf("acp rpc error %d: %s", e.Code, e.Message) }

type rpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type callResult struct {
	result json.RawMessage
	err    error
}

type pendingCall struct {
	id                     string
	done                   chan callResult
	settled                chan struct{}
	settleOnce             sync.Once
	writeDone              chan struct{}
	writeOnce              sync.Once
	phaseMu                sync.Mutex
	phase                  DeliveryPhase
	responseUpdateSequence uint64
	prompt                 bool
}

// pendingRestore binds session observations to the exact in-flight lifecycle
// request. responseSeen is set by the reader before it settles that request so
// session/resume can reject pre-response history without racing the caller.
type pendingRestore struct {
	method        string
	sessionID     string
	requestID     string
	responseSeen  bool
	configOptions []ConfigOption
}

func (p *pendingCall) settle() { p.settleOnce.Do(func() { close(p.settled) }) }

func (p *pendingCall) finishWrite() { p.writeOnce.Do(func() { close(p.writeDone) }) }

func (p *pendingCall) setPhase(v DeliveryPhase) {
	p.phaseMu.Lock()
	if phaseRank(v) > phaseRank(p.phase) {
		p.phase = v
	}
	p.phaseMu.Unlock()
}

func (p *pendingCall) getPhase() DeliveryPhase {
	p.phaseMu.Lock()
	defer p.phaseMu.Unlock()
	return p.phase
}

func (p *pendingCall) setResponseUpdateSequence(sequence uint64) {
	p.phaseMu.Lock()
	p.responseUpdateSequence = sequence
	p.phaseMu.Unlock()
}

func (p *pendingCall) getResponseUpdateSequence() uint64 {
	p.phaseMu.Lock()
	defer p.phaseMu.Unlock()
	return p.responseUpdateSequence
}

type permissionRequestState struct {
	mu        sync.Mutex
	cancelled bool
	settled   bool
}

func (s *permissionRequestState) cancel() {
	s.mu.Lock()
	if !s.settled {
		s.cancelled = true
	}
	s.mu.Unlock()
}

func phaseRank(p DeliveryPhase) int {
	switch p {
	case DeliveryWriteStarted:
		return 1
	case DeliveryWriteComplete:
		return 2
	case DeliveryResponseSeen:
		return 3
	case DeliveryTerminalReceived:
		return 4
	default:
		return 0
	}
}

// Client is one process/session boundary. It is not safe to reuse a Client
// after Close or after a protocol failure.
type Client struct {
	cfg    Config
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser

	writeMu                 sync.Mutex
	initializeMu            sync.Mutex
	sessionMu               sync.Mutex
	configMu                sync.Mutex
	promptMu                sync.Mutex
	mu                      sync.Mutex
	pending                 map[string]*pendingCall
	inbound                 map[string]struct{}
	nextID                  int64
	initialized             bool
	session                 *Session
	restore                 *pendingRestore
	capabilities            AgentCapabilities
	authMethods             map[string]AuthMethod
	activePrompt            *pendingCall
	protocolErr             error
	closed                  bool
	readerDone              chan struct{}
	processDone             chan struct{}
	updates                 chan Update
	finishOnce              sync.Once
	teardownOnce            sync.Once
	permissionSem           chan struct{}
	permissionCancel        map[string]context.CancelFunc
	permissionDone          map[string]chan struct{}
	permissionState         map[string]*permissionRequestState
	permissionInvocations   int
	updateBytes             int
	lastActivity            time.Time
	updateSequence          uint64
	configSequence          uint64
	beforePermissionRespond func()
	beforeConfigCommit      func()
	beforePromptWrite       func()
	beforeCancelWriteWait   func()
}

// Start launches exactly one direct subprocess and starts its bounded reader.
func Start(ctx context.Context, cfg Config) (*Client, error) {
	cfg = cfg.withDefaults()
	if len(cfg.Argv) == 0 || strings.TrimSpace(cfg.Argv[0]) == "" {
		return nil, fmt.Errorf("acp argv is required")
	}
	if !filepath.IsAbs(cfg.Argv[0]) {
		return nil, fmt.Errorf("acp executable must be absolute: %q", cfg.Argv[0])
	}
	if !filepath.IsAbs(cfg.CWD) {
		return nil, fmt.Errorf("acp cwd must be absolute: %q", cfg.CWD)
	}
	if cfg.Env == nil {
		return nil, errors.New("acp environment allowlist is required")
	}
	if cfg.Stderr == nil {
		return nil, errors.New("acp bounded stderr sink is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	cmd := exec.Command(cfg.Argv[0], cfg.Argv[1:]...)
	cmd.Dir = cfg.CWD
	if cfg.Env != nil {
		cmd.Env = append([]string(nil), cfg.Env...)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("acp stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("acp stdout: %w", err)
	}
	// Provider diagnostics must never corrupt stdout protocol framing.
	cmd.Stderr = cfg.Stderr
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		return nil, fmt.Errorf("start acp adapter: %w", err)
	}
	if cfg.ValidateProcess != nil {
		if err := cfg.ValidateProcess(cmd.Process.Pid); err != nil {
			_ = stdin.Close()
			_ = stdout.Close()
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			return nil, fmt.Errorf("validate acp adapter process: %w", err)
		}
	}
	c := &Client{
		cfg: cfg, cmd: cmd, stdin: stdin, stdout: stdout,
		pending: make(map[string]*pendingCall), inbound: make(map[string]struct{}),
		readerDone: make(chan struct{}), processDone: make(chan struct{}), updates: make(chan Update, cfg.Limits.MaxUpdates),
		permissionSem:    make(chan struct{}, cfg.Limits.MaxPendingRequests),
		permissionCancel: make(map[string]context.CancelFunc), permissionDone: make(map[string]chan struct{}), permissionState: make(map[string]*permissionRequestState),
		lastActivity: time.Now(),
	}
	go c.readLoop()
	go c.waitLoop()
	return c, nil
}

// ProcessID returns the supervised ACP adapter PID. It is observational
// process identity only; it conveys no Tusker task, lease, or session
// authority. A zero result means the client was not started.
func (c *Client) ProcessID() int {
	if c == nil || c.cmd == nil || c.cmd.Process == nil {
		return 0
	}
	return c.cmd.Process.Pid
}

func (c *Client) waitLoop() {
	defer close(c.processDone)
	err := c.cmd.Wait()
	c.mu.Lock()
	closed := c.closed
	c.mu.Unlock()
	if !closed && err != nil {
		c.poison(fmt.Errorf("acp adapter exited: %w", err))
	}
}

// Updates returns the bounded stream of session/update observations.
func (c *Client) Updates() <-chan Update { return c.updates }

// Initialize negotiates ACP v1 exactly once.
func (c *Client) Initialize(ctx context.Context) (InitializeResult, error) {
	c.initializeMu.Lock()
	defer c.initializeMu.Unlock()
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return InitializeResult{}, ErrClosed
	}
	if c.protocolErr != nil {
		err := c.protocolErr
		c.mu.Unlock()
		return InitializeResult{}, err
	}
	if c.initialized {
		c.mu.Unlock()
		return InitializeResult{}, fmt.Errorf("acp initialize called more than once")
	}
	c.mu.Unlock()

	ctx, cancel := withDeadline(ctx, c.cfg.Timeouts.Initialize)
	defer cancel()
	params := map[string]any{
		"protocolVersion":    protocolVersion,
		"clientInfo":         AgentInfo{Name: "tusker", Version: "1"},
		"clientCapabilities": ClientCapabilities{},
	}
	raw, phase, err := c.call(ctx, "initialize", params, false)
	if err != nil {
		return InitializeResult{}, err
	}
	var wire struct {
		ProtocolVersion   int             `json:"protocolVersion"`
		AgentInfo         AgentInfo       `json:"agentInfo"`
		AgentCapabilities json.RawMessage `json:"agentCapabilities"`
		AuthMethods       []AuthMethod    `json:"authMethods"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		return InitializeResult{}, c.failCall(err, phase)
	}
	if wire.ProtocolVersion != protocolVersion {
		return InitializeResult{}, c.failCall(fmt.Errorf("unsupported negotiated ACP protocol version %d", wire.ProtocolVersion), phase)
	}
	authMethods, authIndex, err := normalizeAuthMethods(wire.AuthMethods)
	if err != nil {
		return InitializeResult{}, c.failCall(fmt.Errorf("invalid ACP authMethods: %w", err), phase)
	}
	caps := AgentCapabilities{Raw: append(json.RawMessage(nil), wire.AgentCapabilities...)}
	var capObj map[string]json.RawMessage
	if len(wire.AgentCapabilities) != 0 && json.Unmarshal(wire.AgentCapabilities, &capObj) == nil {
		caps.LoadSession = capabilityBool(capObj["loadSession"])
		// ACP v1 specifies session/resume under
		// agentCapabilities.sessionCapabilities.resume. It is an object
		// capability: omitted, null, booleans, and lookalike legacy fields do
		// not authorize a resume call.
		var sessionCapabilities map[string]json.RawMessage
		if json.Unmarshal(capObj["sessionCapabilities"], &sessionCapabilities) == nil && sessionCapabilities != nil {
			caps.ResumeSession = capabilityObject(sessionCapabilities["resume"])
		}
	}
	c.mu.Lock()
	c.initialized = true
	c.capabilities = caps
	c.authMethods = authIndex
	c.mu.Unlock()
	return InitializeResult{ProtocolVersion: wire.ProtocolVersion, AgentInfo: wire.AgentInfo, AgentCapabilities: caps, AuthMethods: authMethods}, nil
}

func normalizeAuthMethods(methods []AuthMethod) ([]AuthMethod, map[string]AuthMethod, error) {
	if len(methods) > maxACPAuthMethods {
		return nil, nil, fmt.Errorf("auth method count %d exceeds limit %d", len(methods), maxACPAuthMethods)
	}
	out := make([]AuthMethod, 0, len(methods))
	index := make(map[string]AuthMethod, len(methods))
	for _, method := range methods {
		if err := validateIdentifier("auth method id", method.ID); err != nil {
			return nil, nil, err
		}
		if strings.TrimSpace(method.Name) == "" {
			return nil, nil, fmt.Errorf("auth method %q has a blank name", method.ID)
		}
		if _, duplicate := index[method.ID]; duplicate {
			return nil, nil, fmt.Errorf("duplicate auth method id %q", method.ID)
		}
		if method.Type == "" {
			method.Type = "agent"
		}
		if method.Type != "agent" {
			return nil, nil, fmt.Errorf("auth method %q has unsupported type %q", method.ID, method.Type)
		}
		index[method.ID] = method
		out = append(out, method)
	}
	return out, index, nil
}

func validateIdentifier(kind, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s is blank", kind)
	}
	if len(value) > maxACPIdentifierBytes {
		return fmt.Errorf("%s exceeds %d bytes", kind, maxACPIdentifierBytes)
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return fmt.Errorf("%s contains a control character", kind)
		}
	}
	return nil
}

func capabilityBool(raw json.RawMessage) bool {
	var value bool
	return len(raw) > 0 && json.Unmarshal(raw, &value) == nil && value
}

func capabilityObject(raw json.RawMessage) bool {
	var value map[string]json.RawMessage
	return len(raw) > 0 && json.Unmarshal(raw, &value) == nil && value != nil
}

func (c *Client) Capabilities() AgentCapabilities {
	c.mu.Lock()
	defer c.mu.Unlock()
	return AgentCapabilities{Raw: append(json.RawMessage(nil), c.capabilities.Raw...), LoadSession: c.capabilities.LoadSession, ResumeSession: c.capabilities.ResumeSession}
}

// Authenticate invokes one exact method advertised during initialization and
// returns a non-secret operational receipt. It never auto-selects a method or
// retries with another method.
func (c *Client) Authenticate(ctx context.Context, methodID string) (AuthenticationReceipt, error) {
	c.sessionMu.Lock()
	defer c.sessionMu.Unlock()
	if err := validateIdentifier("auth method id", methodID); err != nil {
		return AuthenticationReceipt{}, err
	}
	c.mu.Lock()
	if !c.initialized {
		c.mu.Unlock()
		return AuthenticationReceipt{}, ErrNotInitialized
	}
	if c.session != nil || c.restore != nil {
		c.mu.Unlock()
		return AuthenticationReceipt{}, errors.New("ACP authentication is not allowed after session setup")
	}
	method, advertised := c.authMethods[methodID]
	c.mu.Unlock()
	if !advertised {
		return AuthenticationReceipt{}, fmt.Errorf("ACP auth method %q was not advertised", methodID)
	}
	ctx, cancel := withDeadline(ctx, c.cfg.Timeouts.Request)
	defer cancel()
	raw, phase, err := c.call(ctx, "authenticate", map[string]any{"methodId": methodID}, false)
	if err != nil {
		return AuthenticationReceipt{}, err
	}
	var result map[string]json.RawMessage
	if err := json.Unmarshal(raw, &result); err != nil || result == nil {
		if err == nil {
			err = errors.New("expected a non-null result object")
		}
		return AuthenticationReceipt{}, c.failCall(fmt.Errorf("invalid authenticate result: %w", err), phase)
	}
	return AuthenticationReceipt{MethodID: method.ID, MethodType: method.Type}, nil
}

// NewSession creates a fresh ACP session. It does not load or resume an
// existing provider session.
func (c *Client) NewSession(ctx context.Context) (Session, error) {
	c.sessionMu.Lock()
	defer c.sessionMu.Unlock()
	c.mu.Lock()
	if !c.initialized {
		c.mu.Unlock()
		return Session{}, ErrNotInitialized
	}
	if c.session != nil {
		s := cloneSession(*c.session)
		c.mu.Unlock()
		return s, fmt.Errorf("acp session already created: %s", s.ID)
	}
	c.mu.Unlock()
	ctx, cancel := withDeadline(ctx, c.cfg.Timeouts.Request)
	defer cancel()
	raw, phase, err := c.call(ctx, "session/new", map[string]any{"cwd": c.cfg.CWD, "mcpServers": []any{}}, false)
	if err != nil {
		return Session{}, err
	}
	var wire struct {
		SessionID     string          `json:"sessionId"`
		ConfigOptions json.RawMessage `json:"configOptions"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		return Session{}, c.failCall(err, phase)
	}
	if err := c.validateSessionID(wire.SessionID); err != nil {
		return Session{}, c.failCall(fmt.Errorf("invalid session/new sessionId: %w", err), phase)
	}
	configOptions, err := parseConfigOptions(wire.ConfigOptions)
	if err != nil {
		return Session{}, c.failCall(fmt.Errorf("invalid session/new configOptions: %w", err), phase)
	}
	c.mu.Lock()
	c.session = &Session{ID: wire.SessionID, ConfigOptions: cloneConfigOptions(configOptions)}
	s := cloneSession(*c.session)
	c.mu.Unlock()
	return s, nil
}

// LoadSession restores an existing ACP session and replays its history through
// Updates. It is permitted only after initialize negotiated loadSession. The
// supplied ID remains opaque; Tusker authorization of that stored reference is
// deliberately the caller's responsibility.
func (c *Client) LoadSession(ctx context.Context, sessionID string) (Session, error) {
	return c.restoreSession(ctx, "session/load", sessionID, ErrLoadSessionUnsupported)
}

// ResumeSession reconnects to an existing ACP session without requesting a
// history replay. It is permitted only after initialize negotiated the exact
// sessionCapabilities.resume capability; loadSession alone is insufficient.
func (c *Client) ResumeSession(ctx context.Context, sessionID string) (Session, error) {
	return c.restoreSession(ctx, "session/resume", sessionID, ErrResumeSessionUnsupported)
}

func (c *Client) restoreSession(ctx context.Context, method, sessionID string, unsupported error) (Session, error) {
	c.sessionMu.Lock()
	defer c.sessionMu.Unlock()

	c.mu.Lock()
	if !c.initialized {
		c.mu.Unlock()
		return Session{}, ErrNotInitialized
	}
	supported := c.capabilities.LoadSession
	if method == "session/resume" {
		supported = c.capabilities.ResumeSession
	}
	if !supported {
		c.mu.Unlock()
		return Session{}, unsupported
	}
	if c.session != nil {
		s := cloneSession(*c.session)
		c.mu.Unlock()
		return s, fmt.Errorf("acp session already created or restored: %s", s.ID)
	}
	c.mu.Unlock()
	if err := c.validateSessionID(sessionID); err != nil {
		return Session{}, fmt.Errorf("invalid %s sessionId: %w", method, err)
	}

	ctx, cancel := withDeadline(ctx, c.cfg.Timeouts.Request)
	defer cancel()
	params := map[string]any{"sessionId": sessionID, "cwd": c.cfg.CWD, "mcpServers": []any{}}
	call, msg, err := c.prepareCall(method, params, false)
	if err != nil {
		return Session{}, err
	}
	restore := &pendingRestore{method: method, sessionID: sessionID, requestID: call.id}
	c.mu.Lock()
	c.restore = restore
	c.mu.Unlock()
	defer c.clearRestore(restore)
	if err := c.writeMessage(msg, call); err != nil {
		c.mu.Lock()
		delete(c.pending, call.id)
		c.mu.Unlock()
		call.settle()
		return Session{}, err
	}
	raw, err := c.awaitCall(ctx, call)
	phase := call.getPhase()
	if err != nil {
		return Session{}, err
	}
	var configOptions []ConfigOption
	if method == "session/load" {
		trimmed := bytes.TrimSpace(raw)
		if bytes.Equal(trimmed, []byte("null")) {
			configOptions = cloneConfigOptions(restore.configOptions)
		} else {
			// ACP v1 documents a null load result after history replay. The
			// pinned codex-acp adapter returns its legacy session state object,
			// including configOptions, models, and modes. Accept both exact
			// shapes without weakening malformed-response handling.
			var result map[string]json.RawMessage
			if err := json.Unmarshal(trimmed, &result); err != nil || result == nil {
				if err == nil {
					err = errors.New("expected null or a non-null object")
				}
				return Session{}, c.failCall(fmt.Errorf("invalid session/load result: %w", err), phase)
			}
			for _, field := range []string{"models", "modes"} {
				if value, present := result[field]; present && !bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
					var object map[string]json.RawMessage
					if err := json.Unmarshal(value, &object); err != nil || object == nil {
						return Session{}, c.failCall(fmt.Errorf("invalid session/load %s state", field), phase)
					}
				}
			}
			if value, present := result["configOptions"]; present {
				var err error
				configOptions, err = parseConfigOptions(value)
				if err != nil {
					return Session{}, c.failCall(fmt.Errorf("invalid session/load configOptions: %w", err), phase)
				}
			} else {
				configOptions = cloneConfigOptions(restore.configOptions)
			}
		}
	} else {
		var result map[string]json.RawMessage
		if err := json.Unmarshal(raw, &result); err != nil || result == nil {
			if err == nil {
				err = errors.New("expected a non-null object")
			}
			return Session{}, c.failCall(fmt.Errorf("invalid session/resume result: %w", err), phase)
		}
		configOptions, err = parseConfigOptions(result["configOptions"])
		if err != nil {
			return Session{}, c.failCall(fmt.Errorf("invalid session/resume configOptions: %w", err), phase)
		}
	}
	c.mu.Lock()
	if c.restore != restore {
		c.mu.Unlock()
		return Session{}, c.failCall(errors.New("ACP restore lifecycle binding was lost"), phase)
	}
	c.session = &Session{ID: sessionID, ConfigOptions: cloneConfigOptions(configOptions)}
	c.configSequence = call.getResponseUpdateSequence()
	c.restore = nil
	s := cloneSession(*c.session)
	c.mu.Unlock()
	return s, nil
}

func parseConfigOptions(raw json.RawMessage) ([]ConfigOption, error) {
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, nil
	}
	var wire []struct {
		ID           string          `json:"id"`
		Name         string          `json:"name"`
		Description  string          `json:"description"`
		Category     string          `json:"category"`
		Type         string          `json:"type"`
		CurrentValue json.RawMessage `json:"currentValue"`
		Options      json.RawMessage `json:"options"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil || wire == nil {
		if err == nil {
			err = errors.New("configOptions must be an array")
		}
		return nil, err
	}
	if len(wire) > maxACPConfigOptions {
		return nil, fmt.Errorf("config option count %d exceeds limit %d", len(wire), maxACPConfigOptions)
	}
	seen := make(map[string]struct{}, len(wire))
	out := make([]ConfigOption, 0, len(wire))
	for _, option := range wire {
		if option.Type != "select" {
			continue
		}
		var rawValues []json.RawMessage
		if err := json.Unmarshal(option.Options, &rawValues); err != nil || rawValues == nil {
			if err == nil {
				err = errors.New("options must be an array")
			}
			return nil, fmt.Errorf("config option %q: %w", option.ID, err)
		}
		grouped := false
		for _, rawValue := range rawValues {
			var shape map[string]json.RawMessage
			if json.Unmarshal(rawValue, &shape) == nil && shape != nil && shape["group"] != nil && shape["options"] != nil {
				grouped = true
				break
			}
		}
		if grouped {
			continue
		}
		if err := validateIdentifier("config option id", option.ID); err != nil {
			return nil, err
		}
		if _, duplicate := seen[option.ID]; duplicate {
			return nil, fmt.Errorf("duplicate config option id %q", option.ID)
		}
		seen[option.ID] = struct{}{}
		if strings.TrimSpace(option.Name) == "" {
			return nil, fmt.Errorf("config option %q has a blank name", option.ID)
		}
		if len(rawValues) == 0 || len(rawValues) > maxACPConfigValues {
			return nil, fmt.Errorf("config option %q has invalid value count %d", option.ID, len(rawValues))
		}
		valuesWire := make([]ConfigOptionValue, len(rawValues))
		for i, rawValue := range rawValues {
			if err := json.Unmarshal(rawValue, &valuesWire[i]); err != nil {
				return nil, fmt.Errorf("config option %q has malformed value: %w", option.ID, err)
			}
		}
		var current string
		if err := json.Unmarshal(option.CurrentValue, &current); err != nil {
			return nil, fmt.Errorf("config option %q currentValue must be a string", option.ID)
		}
		values := make(map[string]struct{}, len(valuesWire))
		for _, value := range valuesWire {
			if err := validateIdentifier("config option value", value.Value); err != nil {
				return nil, fmt.Errorf("config option %q: %w", option.ID, err)
			}
			if strings.TrimSpace(value.Name) == "" {
				return nil, fmt.Errorf("config option %q value %q has a blank name", option.ID, value.Value)
			}
			if _, duplicate := values[value.Value]; duplicate {
				return nil, fmt.Errorf("config option %q has duplicate value %q", option.ID, value.Value)
			}
			values[value.Value] = struct{}{}
		}
		if _, valid := values[current]; !valid {
			return nil, fmt.Errorf("config option %q currentValue %q is not advertised", option.ID, current)
		}
		out = append(out, ConfigOption{
			ID: option.ID, Name: option.Name, Description: option.Description, Category: option.Category,
			Type: option.Type, CurrentValue: current, Options: append([]ConfigOptionValue(nil), valuesWire...),
		})
	}
	return out, nil
}

func cloneConfigOptions(options []ConfigOption) []ConfigOption {
	if options == nil {
		return nil
	}
	out := make([]ConfigOption, len(options))
	copy(out, options)
	for i := range out {
		out[i].Options = append([]ConfigOptionValue(nil), options[i].Options...)
	}
	return out
}

func cloneSession(session Session) Session {
	session.ConfigOptions = cloneConfigOptions(session.ConfigOptions)
	return session
}

// SetConfigOption changes one exact select value advertised for the active
// session. The complete returned state replaces the local snapshot only after
// it validates and confirms the requested selection.
func (c *Client) SetConfigOption(ctx context.Context, configID, value string) (Session, error) {
	c.configMu.Lock()
	defer c.configMu.Unlock()
	if err := validateIdentifier("config option id", configID); err != nil {
		return Session{}, err
	}
	if err := validateIdentifier("config option value", value); err != nil {
		return Session{}, err
	}
	c.mu.Lock()
	if !c.initialized {
		c.mu.Unlock()
		return Session{}, ErrNotInitialized
	}
	if c.session == nil {
		c.mu.Unlock()
		return Session{}, ErrNoSession
	}
	sessionID := c.session.ID
	var selected *ConfigOption
	for i := range c.session.ConfigOptions {
		if c.session.ConfigOptions[i].ID == configID {
			copy := c.session.ConfigOptions[i]
			selected = &copy
			break
		}
	}
	c.mu.Unlock()
	if selected == nil {
		return Session{}, fmt.Errorf("ACP config option %q was not advertised", configID)
	}
	allowed := false
	for _, option := range selected.Options {
		if option.Value == value {
			allowed = true
			break
		}
	}
	if !allowed {
		return Session{}, fmt.Errorf("ACP config option %q did not advertise value %q", configID, value)
	}
	ctx, cancel := withDeadline(ctx, c.cfg.Timeouts.Request)
	defer cancel()
	call, err := c.beginCall("session/set_config_option", map[string]any{
		"sessionId": sessionID, "configId": configID, "value": value,
	}, false)
	if err != nil {
		return Session{}, err
	}
	raw, err := c.awaitCall(ctx, call)
	phase := call.getPhase()
	responseSequence := call.getResponseUpdateSequence()
	if err != nil {
		return Session{}, err
	}
	var result map[string]json.RawMessage
	if err := json.Unmarshal(raw, &result); err != nil || result == nil {
		if err == nil {
			err = errors.New("expected a non-null object")
		}
		return Session{}, c.failCall(fmt.Errorf("invalid session/set_config_option result: %w", err), phase)
	}
	configRaw, present := result["configOptions"]
	if !present || bytes.Equal(bytes.TrimSpace(configRaw), []byte("null")) {
		return Session{}, c.failCall(errors.New("session/set_config_option omitted complete configOptions"), phase)
	}
	options, err := parseConfigOptions(configRaw)
	if err != nil {
		return Session{}, c.failCall(fmt.Errorf("invalid session/set_config_option configOptions: %w", err), phase)
	}
	matched := false
	for _, option := range options {
		if option.ID == configID && option.CurrentValue == value {
			matched = true
			break
		}
	}
	if !matched {
		return Session{}, c.failCall(fmt.Errorf("session/set_config_option did not confirm %s=%s", configID, value), phase)
	}
	c.mu.Lock()
	hook := c.beforeConfigCommit
	c.mu.Unlock()
	if hook != nil {
		hook()
	}
	c.mu.Lock()
	if c.session == nil || c.session.ID != sessionID {
		c.mu.Unlock()
		return Session{}, c.failCall(errors.New("ACP session changed during config update"), phase)
	}
	if c.configSequence <= responseSequence {
		c.session.ConfigOptions = cloneConfigOptions(options)
		c.configSequence = responseSequence
	}
	session := cloneSession(*c.session)
	c.mu.Unlock()
	return session, nil
}

func (c *Client) clearRestore(restore *pendingRestore) {
	c.mu.Lock()
	if c.restore == restore {
		c.restore = nil
	}
	c.mu.Unlock()
}

func (c *Client) validateUpdateEnvelope(params json.RawMessage, sequence uint64) error {
	var envelope struct {
		SessionID string          `json:"sessionId"`
		Update    json.RawMessage `json:"update"`
	}
	if err := json.Unmarshal(params, &envelope); err != nil {
		return fmt.Errorf("invalid session/update envelope: %w", err)
	}
	if err := c.validateSessionID(envelope.SessionID); err != nil {
		return fmt.Errorf("invalid session/update sessionId: %w", err)
	}
	var update map[string]json.RawMessage
	if err := json.Unmarshal(envelope.Update, &update); err != nil || update == nil {
		if err == nil {
			err = errors.New("update must be a non-null object")
		}
		return fmt.Errorf("invalid session/update payload: %w", err)
	}
	var discriminator string
	if err := json.Unmarshal(update["sessionUpdate"], &discriminator); err != nil || strings.TrimSpace(discriminator) == "" {
		if err == nil {
			err = errors.New("sessionUpdate discriminator is blank")
		}
		return fmt.Errorf("invalid session/update discriminator: %w", err)
	}
	var configOptions []ConfigOption
	if discriminator == "config_option_update" {
		configRaw, present := update["configOptions"]
		if !present || bytes.Equal(bytes.TrimSpace(configRaw), []byte("null")) {
			return errors.New("config_option_update omitted complete configOptions")
		}
		var err error
		configOptions, err = parseConfigOptions(configRaw)
		if err != nil {
			return fmt.Errorf("invalid config_option_update configOptions: %w", err)
		}
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	restore := c.restore
	if restore != nil {
		if envelope.SessionID != restore.sessionID {
			return fmt.Errorf("session/update sessionId %q does not match in-flight %s sessionId %q", envelope.SessionID, restore.method, restore.sessionID)
		}
		if restore.responseSeen {
			return errors.New("session/update arrived after restore response but before session commit")
		}
		if restore.method == "session/resume" {
			return errors.New("session/update arrived before session/resume response")
		}
		if discriminator == "config_option_update" {
			restore.configOptions = cloneConfigOptions(configOptions)
		}
		return nil
	}
	if c.session == nil {
		return errors.New("session/update arrived without an active or restoring session")
	}
	if envelope.SessionID != c.session.ID {
		return fmt.Errorf("session/update sessionId %q does not match active sessionId %q", envelope.SessionID, c.session.ID)
	}
	if discriminator == "config_option_update" {
		c.session.ConfigOptions = cloneConfigOptions(configOptions)
		c.configSequence = sequence
	}
	return nil
}

func (c *Client) validateSessionID(sessionID string) error {
	if strings.TrimSpace(sessionID) == "" {
		return errors.New("sessionId is blank")
	}
	for _, r := range sessionID {
		if unicode.IsControl(r) {
			return errors.New("sessionId contains a control character")
		}
	}
	return nil
}

// Prompt sends one text prompt. It never retries, including after ambiguous
// delivery. For structured content, adapters can encode the text as ACP data.
func (c *Client) Prompt(ctx context.Context, prompt string) (PromptResult, error) {
	if len(prompt) > c.cfg.Limits.MaxFrameBytes-512 {
		return PromptResult{}, fmt.Errorf("%w: prompt exceeds bounded frame budget", ErrProtocol)
	}
	c.promptMu.Lock()
	c.mu.Lock()
	if !c.initialized {
		c.mu.Unlock()
		c.promptMu.Unlock()
		return PromptResult{}, ErrNotInitialized
	}
	if c.session == nil {
		c.mu.Unlock()
		c.promptMu.Unlock()
		return PromptResult{}, ErrNoSession
	}
	if c.activePrompt != nil {
		c.mu.Unlock()
		c.promptMu.Unlock()
		return PromptResult{}, ErrPromptActive
	}
	sessionID := c.session.ID
	c.mu.Unlock()
	ctx, cancel := withDeadline(ctx, c.cfg.Timeouts.Prompt)
	defer cancel()
	params := map[string]any{"sessionId": sessionID, "prompt": []any{map[string]any{"type": "text", "text": prompt}}}
	call, msg, err := c.prepareCall("session/prompt", params, true)
	if err != nil {
		c.promptMu.Unlock()
		return PromptResult{}, err
	}
	c.promptMu.Unlock()
	if hook := c.beforePromptWrite; hook != nil {
		hook()
	}
	err = c.writeMessage(msg, call)
	if err != nil {
		c.mu.Lock()
		delete(c.pending, call.id)
		if c.activePrompt == call {
			c.activePrompt = nil
		}
		c.mu.Unlock()
		call.settle()
		return PromptResult{Outcome: classifyErrorOutcome(err, call.getPhase()), Delivery: call.getPhase(), SessionID: sessionID}, wrapPromptError(err, call.getPhase())
	}
	defer func() {
		c.mu.Lock()
		if c.activePrompt == call {
			c.activePrompt = nil
		}
		c.mu.Unlock()
	}()
	result, err := c.awaitCall(ctx, call)
	phase := call.getPhase()
	if err != nil {
		return PromptResult{Outcome: classifyErrorOutcome(err, phase), Delivery: phase, SessionID: sessionID}, wrapPromptError(err, phase)
	}
	call.setPhase(DeliveryTerminalReceived)
	var wire struct {
		StopReason string `json:"stopReason"`
		TurnID     string `json:"turnId"`
	}
	if err := json.Unmarshal(result, &wire); err != nil {
		c.poison(err)
		return PromptResult{Outcome: OutcomeProtocolFailed, Delivery: call.getPhase(), SessionID: sessionID}, &OutcomeError{Outcome: OutcomeProtocolFailed, Delivery: call.getPhase(), Err: err}
	}
	outcome := outcomeForStopReason(wire.StopReason)
	if outcome == OutcomeProtocolFailed {
		protocolErr := fmt.Errorf("%w: unknown ACP stop reason %q", ErrProtocol, wire.StopReason)
		c.poison(protocolErr)
		return PromptResult{Outcome: OutcomeProtocolFailed, Delivery: call.getPhase(), SessionID: sessionID, TurnID: wire.TurnID}, &OutcomeError{Outcome: OutcomeProtocolFailed, Delivery: call.getPhase(), Err: protocolErr}
	}
	return PromptResult{Outcome: outcome, StopReason: wire.StopReason, Delivery: call.getPhase(), SessionID: sessionID, TurnID: wire.TurnID, Raw: append(json.RawMessage(nil), result...)}, nil
}

// Cancel requests cancellation of the active prompt and drains its response.
// Failure to drain poisons the process; it is never reused.
func (c *Client) Cancel(ctx context.Context) error {
	c.mu.Lock()
	call := c.activePrompt
	session := c.session
	c.mu.Unlock()
	if call == nil {
		return ErrNoPrompt
	}
	if session == nil {
		return ErrNoSession
	}
	ctx, cancel := withDeadline(ctx, c.cfg.Timeouts.CancelDrain)
	defer cancel()
	permissionDone := c.cancelPendingPermissions()
	if hook := c.beforeCancelWriteWait; hook != nil {
		hook()
	}
	select {
	case <-call.writeDone:
		if phaseRank(call.getPhase()) < phaseRank(DeliveryWriteComplete) {
			c.poison(ErrDeliveryUnknown)
			return &OutcomeError{Outcome: OutcomePoisoned, Delivery: call.getPhase(), Err: ErrDeliveryUnknown}
		}
	case <-ctx.Done():
		c.poison(ctx.Err())
		return &OutcomeError{Outcome: OutcomePoisoned, Delivery: call.getPhase(), Err: ctx.Err()}
	case <-c.readerDone:
		return ErrPoisoned
	}
	notifyDone := make(chan error, 1)
	go func() { notifyDone <- c.notify("session/cancel", map[string]any{"sessionId": session.ID}) }()
	select {
	case err := <-notifyDone:
		if err != nil {
			c.poison(err)
			return err
		}
	case <-ctx.Done():
		c.poison(ctx.Err())
		return &OutcomeError{Outcome: OutcomePoisoned, Delivery: call.getPhase(), Err: ctx.Err()}
	case <-c.readerDone:
		return ErrPoisoned
	}
	select {
	case <-call.settled:
	case <-ctx.Done():
		c.poison(ctx.Err())
		return &OutcomeError{Outcome: OutcomePoisoned, Delivery: call.getPhase(), Err: ctx.Err()}
	case <-c.readerDone:
		return ErrPoisoned
	}
	for _, done := range permissionDone {
		select {
		case <-done:
		case <-ctx.Done():
			c.poison(ctx.Err())
			return &OutcomeError{Outcome: OutcomePoisoned, Delivery: call.getPhase(), Err: ctx.Err()}
		case <-c.readerDone:
			return ErrPoisoned
		}
	}
	return nil
}

// Close terminates the direct child and waits for reader cleanup.
func (c *Client) Close() error {
	c.mu.Lock()
	c.closed = true
	for id, cancel := range c.permissionCancel {
		if state := c.permissionState[id]; state != nil {
			state.cancel()
		}
		cancel()
	}
	c.mu.Unlock()
	c.teardownTransport()
	<-c.readerDone
	<-c.processDone
	return nil
}

func (c *Client) teardownTransport() {
	c.teardownOnce.Do(func() {
		_ = c.stdin.Close()
		if c.cmd != nil && c.cmd.Process != nil {
			_ = c.cmd.Process.Kill()
		}
		_ = c.stdout.Close()
	})
}

func withDeadline(ctx context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	if d <= 0 {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, d)
}

func (c *Client) prepareCall(method string, params any, prompt bool) (*pendingCall, rpcMessage, error) {
	paramBytes, err := json.Marshal(params)
	if err != nil {
		return nil, rpcMessage{}, err
	}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, rpcMessage{}, ErrClosed
	}
	if c.protocolErr != nil {
		err := c.protocolErr
		c.mu.Unlock()
		return nil, rpcMessage{}, err
	}
	if prompt && c.activePrompt != nil {
		c.mu.Unlock()
		return nil, rpcMessage{}, ErrPromptActive
	}
	if len(c.pending) >= c.cfg.Limits.MaxPendingRequests {
		err := c.poisonLocked(fmt.Errorf("%w: pending request limit %d exceeded", ErrProtocol, c.cfg.Limits.MaxPendingRequests))
		c.mu.Unlock()
		return nil, rpcMessage{}, err
	}
	nextID := c.nextID + 1
	id := strconv.FormatInt(nextID, 10)
	msg := rpcMessage{JSONRPC: "2.0", ID: json.RawMessage(id), Method: method, Params: paramBytes}
	frame, err := json.Marshal(msg)
	if err != nil {
		c.mu.Unlock()
		return nil, rpcMessage{}, err
	}
	if len(frame) > c.cfg.Limits.MaxFrameBytes {
		c.mu.Unlock()
		return nil, rpcMessage{}, fmt.Errorf("%w: ACP request frame for %s is %d bytes, limit %d", ErrProtocol, method, len(frame), c.cfg.Limits.MaxFrameBytes)
	}
	c.nextID = nextID
	p := &pendingCall{id: id, done: make(chan callResult, 1), settled: make(chan struct{}), writeDone: make(chan struct{}), phase: DeliveryNotSent, prompt: prompt}
	c.pending[id] = p
	if prompt {
		c.activePrompt = p
		c.updateBytes = 0
	}
	c.mu.Unlock()
	return p, msg, nil
}

func (c *Client) beginCall(method string, params any, prompt bool) (*pendingCall, error) {
	p, msg, err := c.prepareCall(method, params, prompt)
	if err != nil {
		return nil, err
	}
	if err := c.writeMessage(msg, p); err != nil {
		c.mu.Lock()
		delete(c.pending, p.id)
		c.mu.Unlock()
		p.settle()
		return nil, err
	}
	return p, nil
}

func (c *Client) call(ctx context.Context, method string, params any, prompt bool) (json.RawMessage, DeliveryPhase, error) {
	p, err := c.beginCall(method, params, prompt)
	if err != nil {
		return nil, DeliveryNotSent, err
	}
	res, err := c.awaitCall(ctx, p)
	return res, p.getPhase(), err
}

func (c *Client) awaitCall(ctx context.Context, p *pendingCall) (json.RawMessage, error) {
	stall := c.cfg.Timeouts.Stall
	var ticker *time.Ticker
	var stallC <-chan time.Time
	if stall > 0 {
		interval := stall / 4
		if interval < 10*time.Millisecond {
			interval = 10 * time.Millisecond
		}
		ticker = time.NewTicker(interval)
		defer ticker.Stop()
		stallC = ticker.C
	}
	for {
		select {
		case result := <-p.done:
			if result.err != nil {
				return nil, result.err
			}
			p.setPhase(DeliveryResponseSeen)
			return result.result, nil
		case <-ctx.Done():
			c.mu.Lock()
			if _, ok := c.pending[p.id]; ok {
				delete(c.pending, p.id)
			}
			c.mu.Unlock()
			p.settle()
			if phaseRank(p.getPhase()) >= phaseRank(DeliveryWriteStarted) {
				c.poison(ErrDeliveryUnknown)
				return nil, &OutcomeError{Outcome: OutcomeDeliveryUnknown, Delivery: p.getPhase(), Err: ErrDeliveryUnknown}
			}
			return nil, ctx.Err()
		case <-stallC:
			c.mu.Lock()
			idle := time.Since(c.lastActivity)
			if _, ok := c.pending[p.id]; ok && idle >= stall {
				delete(c.pending, p.id)
			}
			c.mu.Unlock()
			if idle >= stall {
				p.settle()
				if phaseRank(p.getPhase()) >= phaseRank(DeliveryWriteStarted) {
					c.poison(ErrDeliveryUnknown)
					return nil, &OutcomeError{Outcome: OutcomeDeliveryUnknown, Delivery: p.getPhase(), Err: ErrDeliveryUnknown}
				}
				c.poison(fmt.Errorf("%w: adapter stalled for %s", ErrProtocol, idle.Round(time.Millisecond)))
				return nil, ErrProtocol
			}
		case <-c.readerDone:
			if phaseRank(p.getPhase()) >= phaseRank(DeliveryWriteStarted) {
				return nil, &OutcomeError{Outcome: OutcomeDeliveryUnknown, Delivery: p.getPhase(), Err: ErrPoisoned}
			}
			return nil, ErrPoisoned
		}
	}
}

func (c *Client) writeMessage(msg rpcMessage, p *pendingCall) error {
	defer p.finishWrite()
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.writeMessageLocked(msg, p)
}

func (c *Client) writeMessageLocked(msg rpcMessage, p *pendingCall) error {
	b, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	if len(b)-1 > c.cfg.Limits.MaxFrameBytes {
		c.poison(fmt.Errorf("acp frame exceeds %d bytes", c.cfg.Limits.MaxFrameBytes))
		return ErrProtocol
	}
	p.setPhase(DeliveryWriteStarted)
	for len(b) > 0 {
		n, err := c.stdin.Write(b)
		if n > 0 {
			b = b[n:]
		}
		if err != nil {
			c.poison(err)
			return err
		}
		if n == 0 {
			c.poison(io.ErrShortWrite)
			return io.ErrShortWrite
		}
	}
	p.setPhase(DeliveryWriteComplete)
	c.mu.Lock()
	c.lastActivity = time.Now()
	c.mu.Unlock()
	return nil
}

func (c *Client) notify(method string, params any) error {
	b, err := json.Marshal(rpcMessage{JSONRPC: "2.0", Method: method, Params: mustJSON(params)})
	if err != nil {
		return err
	}
	if len(b) > c.cfg.Limits.MaxFrameBytes {
		return fmt.Errorf("acp frame exceeds %d bytes", c.cfg.Limits.MaxFrameBytes)
	}
	b = append(b, '\n')
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return writeAll(c.stdin, b)
}

func mustJSON(v any) json.RawMessage { b, _ := json.Marshal(v); return b }

func (c *Client) readLoop() {
	defer close(c.readerDone)
	defer c.finishOnce.Do(func() { close(c.updates) })
	r := bufio.NewReaderSize(c.stdout, 64<<10)
	for {
		frame, err := readFrame(r, c.cfg.Limits.MaxFrameBytes)
		if err != nil {
			if errors.Is(err, io.EOF) {
				c.poison(fmt.Errorf("%w: adapter stdout closed", ErrProtocol))
			} else {
				c.poison(err)
			}
			return
		}
		c.mu.Lock()
		c.lastActivity = time.Now()
		c.mu.Unlock()
		var msg rpcMessage
		if err := json.Unmarshal(frame, &msg); err != nil || msg.JSONRPC != "2.0" {
			if err == nil {
				err = errors.New("missing or invalid jsonrpc version")
			}
			c.poison(fmt.Errorf("%w: %v", ErrProtocol, err))
			return
		}
		if len(msg.ID) != 0 && msg.Method == "" {
			c.handleResponse(msg)
			continue
		}
		if msg.Method != "" {
			c.handleRequest(msg)
			continue
		}
		c.poison(fmt.Errorf("%w: invalid message shape", ErrProtocol))
		return
	}
}

func readFrame(r *bufio.Reader, max int) ([]byte, error) {
	var frame []byte
	for {
		part, err := r.ReadSlice('\n')
		if len(frame)+len(part) > max+1 {
			return nil, fmt.Errorf("%w: frame exceeds %d bytes", ErrProtocol, max)
		}
		frame = append(frame, part...)
		if err == nil {
			frame = frame[:len(frame)-1]
			if len(frame) > 0 && frame[len(frame)-1] == '\r' {
				frame = frame[:len(frame)-1]
			}
			if len(frame) == 0 {
				continue
			}
			return frame, nil
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		if errors.Is(err, io.EOF) {
			if len(frame) == 0 {
				return nil, io.EOF
			}
			if len(frame) > max {
				return nil, fmt.Errorf("%w: unterminated frame exceeds %d bytes", ErrProtocol, max)
			}
			return frame, nil
		}
		return nil, err
	}
}

func (c *Client) handleResponse(msg rpcMessage) {
	id, ok := rpcID(msg.ID)
	if !ok {
		c.poison(fmt.Errorf("%w: invalid response id", ErrProtocol))
		return
	}
	c.mu.Lock()
	p, exists := c.pending[id]
	if exists {
		delete(c.pending, id)
		p.setResponseUpdateSequence(c.updateSequence)
		if c.restore != nil && c.restore.requestID == id {
			c.restore.responseSeen = true
		}
	}
	c.mu.Unlock()
	if !exists {
		c.poison(fmt.Errorf("%w: unknown or duplicate response id %s", ErrProtocol, id))
		return
	}
	p.setPhase(DeliveryResponseSeen)
	if msg.Error != nil {
		if msg.Result != nil {
			err := fmt.Errorf("%w: response %s contains both result and error", ErrProtocol, id)
			p.done <- callResult{err: err}
			p.settle()
			c.poison(err)
			return
		}
		p.done <- callResult{err: msg.Error}
		p.settle()
		return
	}
	if msg.Result == nil {
		err := fmt.Errorf("%w: response %s has no result", ErrProtocol, id)
		p.done <- callResult{err: err}
		p.settle()
		c.poison(err)
		return
	}
	p.done <- callResult{result: append(json.RawMessage(nil), msg.Result...)}
	p.settle()
}

func (c *Client) handleRequest(msg rpcMessage) {
	if len(msg.ID) == 0 {
		if msg.Method == "session/update" {
			sequence, ok := c.recordUpdateObservation(len(msg.Params))
			if !ok {
				return
			}
			if err := c.validateUpdateEnvelope(msg.Params, sequence); err != nil {
				c.poison(fmt.Errorf("%w: %v", ErrProtocol, err))
				return
			}
			c.enqueueUpdate(Update{Sequence: sequence, Method: msg.Method, Params: append(json.RawMessage(nil), msg.Params...)})
			return
		}
		// Unknown notifications have no response channel. Fail closed rather
		// than executing or forwarding a provider extension implicitly.
		c.poison(fmt.Errorf("%w: unknown notification %q", ErrProtocol, msg.Method))
		return
	}
	id, ok := rpcID(msg.ID)
	if !ok {
		c.poison(fmt.Errorf("%w: server request has invalid id", ErrProtocol))
		return
	}
	c.mu.Lock()
	if _, duplicate := c.inbound[id]; duplicate {
		c.mu.Unlock()
		c.poison(fmt.Errorf("%w: duplicate server request id %s", ErrProtocol, id))
		return
	}
	c.inbound[id] = struct{}{}
	c.mu.Unlock()
	if msg.Method != "session/request_permission" {
		if err := c.respond(id, nil, &rpcError{Code: -32601, Message: "method not found"}); err != nil {
			c.poison(err)
		}
		return
	}
	c.mu.Lock()
	if c.permissionInvocations >= c.cfg.Limits.MaxPendingRequests {
		c.mu.Unlock()
		c.poison(fmt.Errorf("%w: total permission request limit exceeded", ErrProtocol))
		return
	}
	c.permissionInvocations++
	c.mu.Unlock()
	select {
	case c.permissionSem <- struct{}{}:
	default:
		c.poison(fmt.Errorf("%w: permission request limit exceeded", ErrProtocol))
		return
	}
	var p struct {
		SessionID        string             `json:"sessionId"`
		ToolCall         json.RawMessage    `json:"toolCall"`
		TopLevelToolCall json.RawMessage    `json:"toolCallId"`
		Options          []PermissionOption `json:"options"`
	}
	if err := json.Unmarshal(msg.Params, &p); err != nil {
		<-c.permissionSem
		if err := c.respond(id, nil, &rpcError{Code: -32602, Message: "invalid permission request"}); err != nil {
			c.poison(err)
		}
		return
	}
	var toolCall struct {
		ToolCallID string          `json:"toolCallId"`
		Kind       string          `json:"kind"`
		RawInput   json.RawMessage `json:"rawInput"`
	}
	if len(p.ToolCall) == 0 || bytes.Equal(bytes.TrimSpace(p.ToolCall), []byte("null")) || json.Unmarshal(p.ToolCall, &toolCall) != nil {
		<-c.permissionSem
		_ = c.respond(id, nil, &rpcError{Code: -32602, Message: "permission request has malformed toolCall"})
		c.poison(fmt.Errorf("%w: permission request has malformed toolCall", ErrProtocol))
		return
	}
	if err := validateIdentifier("permission toolCallId", toolCall.ToolCallID); err != nil {
		<-c.permissionSem
		_ = c.respond(id, nil, &rpcError{Code: -32602, Message: "permission request has invalid toolCallId"})
		c.poison(fmt.Errorf("%w: %v", ErrProtocol, err))
		return
	}
	if len(p.TopLevelToolCall) != 0 {
		var topLevel string
		if json.Unmarshal(p.TopLevelToolCall, &topLevel) != nil || topLevel != toolCall.ToolCallID {
			<-c.permissionSem
			_ = c.respond(id, nil, &rpcError{Code: -32602, Message: "permission request toolCallId nesting mismatch"})
			c.poison(fmt.Errorf("%w: permission request toolCallId nesting mismatch", ErrProtocol))
			return
		}
	}
	if !isOfficialToolKind(toolCall.Kind) {
		toolCall.Kind = "other"
	}
	rawLimit := c.cfg.Limits.MaxFrameBytes
	if rawLimit > maxACPPermissionRawInput {
		rawLimit = maxACPPermissionRawInput
	}
	rawInput := bytes.TrimSpace(toolCall.RawInput)
	if len(rawInput) != 0 && !bytes.Equal(rawInput, []byte("null")) && (len(toolCall.RawInput) > rawLimit || !json.Valid(toolCall.RawInput)) {
		<-c.permissionSem
		_ = c.respond(id, nil, &rpcError{Code: -32602, Message: "permission request has invalid rawInput"})
		c.poison(fmt.Errorf("%w: permission request rawInput is malformed or exceeds %d bytes", ErrProtocol, rawLimit))
		return
	}
	if err := validatePermissionOptions(p.Options); err != nil {
		<-c.permissionSem
		_ = c.respond(id, nil, &rpcError{Code: -32602, Message: "permission request has invalid options"})
		c.poison(fmt.Errorf("%w: %v", ErrProtocol, err))
		return
	}
	c.mu.Lock()
	active := c.activePrompt != nil
	currentSession := ""
	if c.session != nil {
		currentSession = c.session.ID
	}
	c.mu.Unlock()
	if !active || currentSession == "" || p.SessionID != currentSession {
		<-c.permissionSem
		_ = c.respond(id, nil, &rpcError{Code: -32602, Message: "permission request is not bound to the active session and turn"})
		c.poison(fmt.Errorf("%w: permission request is not bound to the active session and turn", ErrProtocol))
		return
	}
	req := PermissionRequest{
		SessionID: p.SessionID, ToolCallID: toolCall.ToolCallID, ToolKind: toolCall.Kind,
		RawInput: append(json.RawMessage(nil), toolCall.RawInput...), Options: p.Options,
		Raw: append(json.RawMessage(nil), msg.Params...),
	}
	permissionCtx, permissionCancel := context.WithTimeout(context.Background(), c.cfg.Timeouts.Request)
	permissionDone := make(chan struct{})
	state := &permissionRequestState{}
	c.mu.Lock()
	c.permissionCancel[id] = permissionCancel
	c.permissionDone[id] = permissionDone
	c.permissionState[id] = state
	c.mu.Unlock()
	go func() {
		defer func() {
			<-c.permissionSem
			permissionCancel()
			c.mu.Lock()
			delete(c.permissionCancel, id)
			delete(c.permissionDone, id)
			delete(c.permissionState, id)
			c.mu.Unlock()
			close(permissionDone)
		}()
		decision := Reject
		if c.cfg.PermissionHandler != nil {
			type handlerResult struct {
				decision PermissionDecision
				err      error
			}
			resultCh := make(chan handlerResult, 1)
			go func() {
				d, err := c.cfg.PermissionHandler(permissionCtx, req)
				resultCh <- handlerResult{decision: d, err: err}
			}()
			select {
			case result := <-resultCh:
				if result.err == nil && permissionCtx.Err() == nil {
					decision = result.decision
				}
			case <-permissionCtx.Done():
				decision = Cancelled
			}
		}
		if permissionCtx.Err() != nil {
			decision = Cancelled
		}
		c.mu.Lock()
		hook := c.beforePermissionRespond
		c.mu.Unlock()
		if hook != nil {
			hook()
		}
		if err := c.respondPermission(id, state, decision, req.Options); err != nil {
			c.poison(err)
		}
	}()
}

// recordUpdateObservation bounds cumulative update bytes for the active
// prompt. Queue occupancy is enforced separately by enqueueUpdate, so normal
// provider chunking is not mistaken for abuse.
func (c *Client) recordUpdateObservation(size int) (uint64, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed || c.protocolErr != nil {
		return 0, false
	}
	if size < 0 || size > c.cfg.Limits.MaxUpdateBytes-c.updateBytes {
		_ = c.poisonLocked(fmt.Errorf("%w: session/update byte limit %d exceeded", ErrProtocol, c.cfg.Limits.MaxUpdateBytes))
		return 0, false
	}
	c.updateBytes += size
	c.updateSequence++
	return c.updateSequence, true
}

func allowOnceOption(options []PermissionOption) (string, bool) {
	for _, option := range options {
		if option.Kind == "allow_once" {
			return option.ID, true
		}
	}
	return "", false
}

func rejectOnceOption(options []PermissionOption) (string, bool) {
	for _, option := range options {
		if option.Kind == "reject_once" {
			return option.ID, true
		}
	}
	return "", false
}

func validatePermissionOptions(options []PermissionOption) error {
	if len(options) == 0 || len(options) > maxACPPermissionOptions {
		return fmt.Errorf("permission option count %d is outside 1..%d", len(options), maxACPPermissionOptions)
	}
	seen := make(map[string]struct{}, len(options))
	for _, option := range options {
		if err := validateIdentifier("permission option id", option.ID); err != nil {
			return err
		}
		if _, duplicate := seen[option.ID]; duplicate {
			return fmt.Errorf("duplicate permission option id %q", option.ID)
		}
		seen[option.ID] = struct{}{}
		if strings.TrimSpace(option.Name) == "" {
			return fmt.Errorf("permission option %q has a blank name", option.ID)
		}
		switch option.Kind {
		case "allow_once", "allow_always", "reject_once", "reject_always":
		default:
			return fmt.Errorf("permission option %q has unsupported kind %q", option.ID, option.Kind)
		}
	}
	return nil
}

func isOfficialToolKind(kind string) bool {
	switch kind {
	case "read", "edit", "delete", "move", "search", "execute", "think", "fetch", "other":
		return true
	default:
		return false
	}
}

func (c *Client) cancelPendingPermissions() []<-chan struct{} {
	c.mu.Lock()
	done := make([]<-chan struct{}, 0, len(c.permissionDone))
	for id, cancel := range c.permissionCancel {
		if state := c.permissionState[id]; state != nil {
			state.cancel()
		}
		cancel()
		if ch := c.permissionDone[id]; ch != nil {
			done = append(done, ch)
		}
	}
	c.mu.Unlock()
	return done
}

func (c *Client) respond(id string, result any, rpcErr *rpcError) error {
	c.mu.Lock()
	delete(c.inbound, id)
	c.mu.Unlock()
	msg := rpcMessage{JSONRPC: "2.0", ID: json.RawMessage(id), Error: rpcErr}
	if rpcErr == nil {
		b, err := json.Marshal(result)
		if err != nil {
			return err
		}
		msg.Result = b
	}
	return c.writeRaw(msg)
}

// respondPermission linearizes cancellation at the transport write. A cancel
// that reaches the per-request state before this method owns both the writer
// and state lock changes the response to cancelled; once those locks are held,
// the permission response is the operation that wins the wire race.
func (c *Client) respondPermission(id string, state *permissionRequestState, decision PermissionDecision, options []PermissionOption) error {
	c.mu.Lock()
	delete(c.inbound, id)
	c.mu.Unlock()

	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.cancelled {
		decision = Cancelled
	}
	var result any
	if optionID, ok := allowOnceOption(options); decision == AllowOnce && ok {
		result = map[string]any{"outcome": map[string]any{"outcome": "selected", "optionId": optionID}}
	} else if optionID, ok := rejectOnceOption(options); decision == Reject && ok {
		result = map[string]any{"outcome": map[string]any{"outcome": "selected", "optionId": optionID}}
	} else {
		result = map[string]any{"outcome": map[string]any{"outcome": "cancelled"}}
	}
	resultBytes, err := json.Marshal(result)
	if err != nil {
		return err
	}
	msg := rpcMessage{JSONRPC: "2.0", ID: json.RawMessage(id), Result: resultBytes}
	b, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	if len(b) > c.cfg.Limits.MaxFrameBytes {
		return fmt.Errorf("acp frame exceeds %d bytes", c.cfg.Limits.MaxFrameBytes)
	}
	err = writeAll(c.stdin, append(b, '\n'))
	state.settled = true
	return err
}

func (c *Client) writeRaw(msg rpcMessage) error {
	b, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	if len(b) > c.cfg.Limits.MaxFrameBytes {
		return fmt.Errorf("acp frame exceeds %d bytes", c.cfg.Limits.MaxFrameBytes)
	}
	b = append(b, '\n')
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return writeAll(c.stdin, b)
}

func writeAll(w io.Writer, b []byte) error {
	for len(b) > 0 {
		n, err := w.Write(b)
		if n > 0 {
			b = b[n:]
		}
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
	}
	return nil
}

func (c *Client) enqueueUpdate(update Update) {
	select {
	case c.updates <- update:
		c.mu.Lock()
		c.lastActivity = time.Now()
		c.mu.Unlock()
	default:
		c.poison(fmt.Errorf("%w: session update queue limit %d exceeded", ErrProtocol, c.cfg.Limits.MaxUpdates))
	}
}

func rpcID(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", false
	}
	var s string
	if json.Unmarshal(raw, &s) == nil && s != "" {
		return s, true
	}
	var n json.Number
	if json.Unmarshal(raw, &n) == nil && n.String() != "" {
		return n.String(), true
	}
	return "", false
}

func (c *Client) poison(err error) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.poisonLocked(err)
}

func (c *Client) poisonLocked(err error) error {
	if c.protocolErr == nil {
		c.protocolErr = err
	}
	c.closed = true
	for id, p := range c.pending {
		delete(c.pending, id)
		p.done <- callResult{err: c.protocolErr}
		p.settle()
	}
	for id, cancel := range c.permissionCancel {
		if state := c.permissionState[id]; state != nil {
			state.cancel()
		}
		cancel()
	}
	c.teardownTransport()
	return c.protocolErr
}

func (c *Client) failCall(err error, phase DeliveryPhase) error {
	_ = phase
	c.poison(err)
	return err
}

func classifyErrorOutcome(err error, phase DeliveryPhase) Outcome {
	var oe *OutcomeError
	if errors.As(err, &oe) {
		return oe.Outcome
	}
	if phaseRank(phase) >= phaseRank(DeliveryResponseSeen) {
		return OutcomeProtocolFailed
	}
	if phaseRank(phase) >= phaseRank(DeliveryWriteStarted) {
		return OutcomeDeliveryUnknown
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return OutcomeTimedOut
	}
	return OutcomeProtocolFailed
}

func wrapPromptError(err error, phase DeliveryPhase) error {
	var oe *OutcomeError
	if errors.As(err, &oe) {
		return err
	}
	return &OutcomeError{Outcome: classifyErrorOutcome(err, phase), Delivery: phase, Err: err}
}

func outcomeForStopReason(reason string) Outcome {
	switch strings.ToLower(reason) {
	case "end_turn":
		return OutcomeCompleted
	case "max_tokens":
		return OutcomeBudgetExceeded
	case "max_turn_requests":
		return OutcomeTurnCapExhausted
	case "refusal":
		return OutcomeRefused
	case "cancelled":
		return OutcomeCancelled
	default:
		return OutcomeProtocolFailed
	}
}
