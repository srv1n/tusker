// Package acp contains the deliberately small, provider-neutral ACP v1
// client.  It owns transport safety only; task, lease, policy, and evidence
// authority remain in the caller.
package acp

import (
	"bufio"
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
)

const protocolVersion = 1

var (
	ErrClosed          = errors.New("acp client is closed")
	ErrPoisoned        = errors.New("acp client is poisoned")
	ErrProtocol        = errors.New("acp protocol failure")
	ErrDeliveryUnknown = errors.New("acp prompt delivery is unknown")
	ErrNotInitialized  = errors.New("acp client is not initialized")
	ErrNoSession       = errors.New("acp session is not created")
	ErrPromptActive    = errors.New("acp prompt already in flight")
	ErrNoPrompt        = errors.New("acp prompt is not in flight")
)

// Limits are finite safety ceilings. Zero values are replaced by defaults.
type Limits struct {
	MaxFrameBytes      int
	MaxPendingRequests int
	MaxUpdates         int
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
}

type InitializeResult struct {
	ProtocolVersion   int
	AgentInfo         AgentInfo
	AgentCapabilities AgentCapabilities
	AuthMethods       []AuthMethod
}

type Session struct{ ID string }

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
	Method string
	Params json.RawMessage
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
	Reason     string
	Options    []PermissionOption
	Raw        json.RawMessage
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
	id         string
	done       chan callResult
	settled    chan struct{}
	settleOnce sync.Once
	phaseMu    sync.Mutex
	phase      DeliveryPhase
	prompt     bool
}

func (p *pendingCall) settle() { p.settleOnce.Do(func() { close(p.settled) }) }

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

	writeMu               sync.Mutex
	initializeMu          sync.Mutex
	sessionMu             sync.Mutex
	promptMu              sync.Mutex
	mu                    sync.Mutex
	pending               map[string]*pendingCall
	inbound               map[string]struct{}
	nextID                int64
	initialized           bool
	session               *Session
	capabilities          AgentCapabilities
	activePrompt          *pendingCall
	protocolErr           error
	closed                bool
	readerDone            chan struct{}
	processDone           chan struct{}
	updates               chan Update
	finishOnce            sync.Once
	teardownOnce          sync.Once
	permissionSem         chan struct{}
	permissionCancel      map[string]context.CancelFunc
	permissionDone        map[string]chan struct{}
	permissionInvocations int
	updatesSeen           int
	cancelledPermissions  map[string]struct{}
	lastActivity          time.Time
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
		permissionCancel: make(map[string]context.CancelFunc), permissionDone: make(map[string]chan struct{}), cancelledPermissions: make(map[string]struct{}),
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
	caps := AgentCapabilities{Raw: append(json.RawMessage(nil), wire.AgentCapabilities...)}
	var capObj map[string]json.RawMessage
	if len(wire.AgentCapabilities) != 0 && json.Unmarshal(wire.AgentCapabilities, &capObj) == nil {
		caps.LoadSession = capabilityBool(capObj["loadSession"])
		caps.ResumeSession = capabilityBool(capObj["resumeSession"])
		// Some adapters nest these under session; preserve the exact raw object
		// while accepting the wire's documented nested capability form.
		var session map[string]json.RawMessage
		if json.Unmarshal(capObj["session"], &session) == nil {
			if raw, ok := session["loadSession"]; ok {
				caps.LoadSession = capabilityBool(raw)
			}
			if raw, ok := session["resumeSession"]; ok {
				caps.ResumeSession = capabilityBool(raw)
			}
		}
	}
	c.mu.Lock()
	c.initialized = true
	c.capabilities = caps
	c.mu.Unlock()
	return InitializeResult{ProtocolVersion: wire.ProtocolVersion, AgentInfo: wire.AgentInfo, AgentCapabilities: caps, AuthMethods: wire.AuthMethods}, nil
}

func capabilityBool(raw json.RawMessage) bool {
	var value bool
	return len(raw) > 0 && json.Unmarshal(raw, &value) == nil && value
}

func (c *Client) Capabilities() AgentCapabilities {
	c.mu.Lock()
	defer c.mu.Unlock()
	return AgentCapabilities{Raw: append(json.RawMessage(nil), c.capabilities.Raw...), LoadSession: c.capabilities.LoadSession, ResumeSession: c.capabilities.ResumeSession}
}

// NewSession creates the only fresh session supported by the first kernel.
func (c *Client) NewSession(ctx context.Context) (Session, error) {
	c.sessionMu.Lock()
	defer c.sessionMu.Unlock()
	c.mu.Lock()
	if !c.initialized {
		c.mu.Unlock()
		return Session{}, ErrNotInitialized
	}
	if c.session != nil {
		s := *c.session
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
		SessionID string `json:"sessionId"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		return Session{}, c.failCall(err, phase)
	}
	if strings.TrimSpace(wire.SessionID) == "" {
		return Session{}, c.failCall(errors.New("session/new returned empty sessionId"), phase)
	}
	c.mu.Lock()
	c.session = &Session{ID: wire.SessionID}
	s := *c.session
	c.mu.Unlock()
	return s, nil
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
	c.mu.Lock()
	c.activePrompt = call
	c.mu.Unlock()
	c.promptMu.Unlock()
	if err := c.writeMessage(msg, call); err != nil {
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
		c.cancelledPermissions[id] = struct{}{}
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
	if len(c.pending) >= c.cfg.Limits.MaxPendingRequests {
		err := c.poisonLocked(fmt.Errorf("%w: pending request limit %d exceeded", ErrProtocol, c.cfg.Limits.MaxPendingRequests))
		c.mu.Unlock()
		return nil, rpcMessage{}, err
	}
	c.nextID++
	id := strconv.FormatInt(c.nextID, 10)
	p := &pendingCall{id: id, done: make(chan callResult, 1), settled: make(chan struct{}), phase: DeliveryNotSent, prompt: prompt}
	c.pending[id] = p
	c.mu.Unlock()
	msg := rpcMessage{JSONRPC: "2.0", ID: json.RawMessage(id), Method: method, Params: paramBytes}
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
	b, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	if len(b)-1 > c.cfg.Limits.MaxFrameBytes {
		c.poison(fmt.Errorf("acp frame exceeds %d bytes", c.cfg.Limits.MaxFrameBytes))
		return ErrProtocol
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
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
			if !c.recordUpdateObservation() {
				return
			}
			c.enqueueUpdate(Update{Method: msg.Method, Params: append(json.RawMessage(nil), msg.Params...)})
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
		SessionID  string             `json:"sessionId"`
		ToolCallID string             `json:"toolCallId"`
		Reason     string             `json:"reason"`
		Options    []PermissionOption `json:"options"`
	}
	if err := json.Unmarshal(msg.Params, &p); err != nil {
		<-c.permissionSem
		if err := c.respond(id, nil, &rpcError{Code: -32602, Message: "invalid permission request"}); err != nil {
			c.poison(err)
		}
		return
	}
	c.mu.Lock()
	active := c.activePrompt != nil
	currentSession := ""
	if c.session != nil {
		currentSession = c.session.ID
	}
	c.mu.Unlock()
	if !active || currentSession == "" || p.SessionID != currentSession || strings.TrimSpace(p.ToolCallID) == "" {
		<-c.permissionSem
		_ = c.respond(id, nil, &rpcError{Code: -32602, Message: "permission request is not bound to the active session and turn"})
		c.poison(fmt.Errorf("%w: permission request is not bound to the active session and turn", ErrProtocol))
		return
	}
	req := PermissionRequest{SessionID: p.SessionID, ToolCallID: p.ToolCallID, Reason: p.Reason, Options: p.Options, Raw: append(json.RawMessage(nil), msg.Params...)}
	permissionCtx, permissionCancel := context.WithTimeout(context.Background(), c.cfg.Timeouts.Request)
	permissionDone := make(chan struct{})
	c.mu.Lock()
	c.permissionCancel[id] = permissionCancel
	c.permissionDone[id] = permissionDone
	c.mu.Unlock()
	go func() {
		defer func() {
			<-c.permissionSem
			permissionCancel()
			c.mu.Lock()
			delete(c.permissionCancel, id)
			delete(c.permissionDone, id)
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
		c.mu.Lock()
		_, cancelled := c.cancelledPermissions[id]
		delete(c.cancelledPermissions, id)
		c.mu.Unlock()
		if cancelled || permissionCtx.Err() != nil {
			decision = Cancelled
		}
		var result any
		if optionID, ok := allowOnceOption(req.Options); decision == AllowOnce && ok {
			result = map[string]any{"outcome": "selected", "optionId": optionID}
		} else {
			result = map[string]any{"outcome": "cancelled"}
		}
		if err := c.respond(id, result, nil); err != nil {
			c.poison(err)
		}
	}()
}

// recordUpdateObservation is a total per-attempt fuse, not merely a queue
// capacity check. A provider that emits and drains an unbounded stream must
// still poison this client rather than run indefinitely or consume unbounded
// supervisor attention.
func (c *Client) recordUpdateObservation() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed || c.protocolErr != nil {
		return false
	}
	c.updatesSeen++
	if c.updatesSeen > c.cfg.Limits.MaxUpdates {
		_ = c.poisonLocked(fmt.Errorf("%w: total session/update limit %d exceeded", ErrProtocol, c.cfg.Limits.MaxUpdates))
		return false
	}
	return true
}

func allowOnceOption(options []PermissionOption) (string, bool) {
	for _, option := range options {
		if option.ID != "" && (strings.EqualFold(option.Kind, "allow_once") || (option.Kind == "" && strings.EqualFold(option.ID, "allow_once"))) {
			return option.ID, true
		}
	}
	return "", false
}

func (c *Client) cancelPendingPermissions() []<-chan struct{} {
	c.mu.Lock()
	done := make([]<-chan struct{}, 0, len(c.permissionDone))
	for id, cancel := range c.permissionCancel {
		c.cancelledPermissions[id] = struct{}{}
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
		c.cancelledPermissions[id] = struct{}{}
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
