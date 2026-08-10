package acp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

type helperMessage struct {
	JSONRPC string          `json:"jsonrpc,omitempty"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   any             `json:"error,omitempty"`
}

func TestACPHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_ACP_HELPER") != "1" {
		return
	}
	mode := os.Getenv("ACP_HELPER_MODE")
	if mode == "malformed" {
		_, _ = os.Stdout.WriteString("{not-json\n")
		os.Exit(0)
	}
	if mode == "oversize" {
		_, _ = os.Stdout.WriteString(strings.Repeat("x", 4097) + "\n")
		os.Exit(0)
	}
	if mode == "silence" {
		_, _ = bufio.NewReader(os.Stdin).ReadByte()
		time.Sleep(10 * time.Second)
		os.Exit(0)
	}

	scanner := bufio.NewScanner(os.Stdin)
	var promptID json.RawMessage
	for scanner.Scan() {
		var msg helperMessage
		if json.Unmarshal(scanner.Bytes(), &msg) != nil {
			os.Exit(2)
		}
		switch msg.Method {
		case "initialize":
			if mode == "unknown-id" {
				writeHelper(helperMessage{JSONRPC: "2.0", ID: json.RawMessage("999"), Result: map[string]any{"protocolVersion": 1}})
			}
			version := 1
			if mode == "wrong-version" {
				version = 2
			}
			writeHelper(helperMessage{JSONRPC: "2.0", ID: msg.ID, Result: map[string]any{
				"protocolVersion": version,
				"agentInfo":       map[string]string{"name": "test-agent", "version": "1"},
				"agentCapabilities": map[string]any{
					"loadSession": false,
					"session":     map[string]bool{"resumeSession": false},
				},
			}})
			if mode == "duplicate-response" {
				writeHelper(helperMessage{JSONRPC: "2.0", ID: msg.ID, Result: map[string]any{"protocolVersion": version}})
			}
		case "session/new":
			var params map[string]any
			_ = json.Unmarshal(msg.Params, &params)
			mcp, present := params["mcpServers"].([]any)
			if !present || len(mcp) != 0 {
				writeHelper(helperMessage{JSONRPC: "2.0", ID: msg.ID, Error: map[string]any{"code": -32602, "message": "mcpServers must be empty"}})
				continue
			}
			writeHelper(helperMessage{JSONRPC: "2.0", ID: msg.ID, Result: map[string]string{"sessionId": "session-1"}})
			if mode == "no-read-after-session" {
				time.Sleep(10 * time.Second)
				os.Exit(0)
			}
		case "session/prompt":
			promptID = append(json.RawMessage(nil), msg.ID...)
			switch mode {
			case "eof-after-prompt":
				os.Exit(0)
			case "unknown-stop":
				writeHelper(helperMessage{JSONRPC: "2.0", ID: msg.ID, Result: map[string]string{"stopReason": "mystery"}})
			case "prompt-rpc-error":
				writeHelper(helperMessage{JSONRPC: "2.0", ID: msg.ID, Error: map[string]any{"code": -32000, "message": "refused"}})
			case "missing-result":
				writeHelper(helperMessage{JSONRPC: "2.0", ID: msg.ID})
			case "permission":
				writeHelper(helperMessage{JSONRPC: "2.0", ID: json.RawMessage("91"), Method: "session/request_permission", Params: mustTestJSON(map[string]any{
					"sessionId": "session-1", "toolCallId": "tool-1",
					"options": []map[string]string{{"optionId": "allow_always", "kind": "allow_always"}, {"optionId": "allow_once", "kind": "allow_once"}},
				})})
			case "permission-mismatch":
				writeHelper(helperMessage{JSONRPC: "2.0", ID: json.RawMessage("91"), Method: "session/request_permission", Params: mustTestJSON(map[string]any{
					"sessionId": "other-session", "toolCallId": "tool-1",
					"options": []map[string]string{{"optionId": "allow_once", "kind": "allow_once"}},
				})})
			case "flood":
				for i := 0; i < 4; i++ {
					writeUpdateForTest(i)
				}
				writeHelper(helperMessage{JSONRPC: "2.0", ID: msg.ID, Result: map[string]string{"stopReason": "end_turn"}})
			case "hold-prompt":
				writeUpdateForTest(0)
				time.Sleep(250 * time.Millisecond)
				writeHelper(helperMessage{JSONRPC: "2.0", ID: msg.ID, Result: map[string]string{"stopReason": "end_turn"}})
			case "ignore-cancel":
				writeUpdateForTest(0)
			default:
				writeUpdateForTest(0)
				writeHelper(helperMessage{JSONRPC: "2.0", ID: msg.ID, Result: map[string]string{"stopReason": "end_turn", "turnId": "turn-1"}})
			}
		case "session/cancel":
			// ignore-cancel deliberately never settles the prompt.
		case "":
			if string(msg.ID) == "91" && promptID != nil {
				var result struct {
					Outcome  string `json:"outcome"`
					OptionID string `json:"optionId"`
				}
				_ = json.Unmarshal(mustTestJSON(msg.Result), &result)
				stop := "refusal"
				if result.Outcome == "selected" && result.OptionID == "allow_once" {
					stop = "end_turn"
				}
				writeHelper(helperMessage{JSONRPC: "2.0", ID: promptID, Result: map[string]string{"stopReason": stop}})
			}
		}
	}
	os.Exit(0)
}

func writeUpdateForTest(n int) {
	writeHelper(helperMessage{JSONRPC: "2.0", Method: "session/update", Params: mustTestJSON(map[string]any{
		"sessionId": "session-1", "update": map[string]any{"kind": "agent_message_chunk", "text": fmt.Sprintf("u%d", n)},
	})})
}

func writeHelper(msg helperMessage) {
	b, _ := json.Marshal(msg)
	_, _ = os.Stdout.Write(append(b, '\n'))
}

func mustTestJSON(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

func startTestClient(t *testing.T, mode string, mutate func(*Config)) *Client {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cfg := Config{
		Argv:     []string{exe, "-test.run=^TestACPHelperProcess$"},
		CWD:      t.TempDir(),
		Env:      []string{"GO_WANT_ACP_HELPER=1", "ACP_HELPER_MODE=" + mode},
		Stderr:   io.Discard,
		Limits:   Limits{MaxFrameBytes: 4096, MaxPendingRequests: 8, MaxUpdates: 8},
		Timeouts: Timeouts{Initialize: time.Second, Request: time.Second, Prompt: time.Second, Stall: 500 * time.Millisecond, CancelDrain: 75 * time.Millisecond},
	}
	if mutate != nil {
		mutate(&cfg)
	}
	c, err := Start(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func initializeAndSession(t *testing.T, c *Client) InitializeResult {
	t.Helper()
	init, err := c.Initialize(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.NewSession(context.Background()); err != nil {
		t.Fatal(err)
	}
	return init
}

func TestACPStartRequiresAbsolutePathsAndEnvironmentAllowlist(t *testing.T) {
	valid := Config{Argv: []string{"/bin/echo"}, CWD: t.TempDir(), Env: []string{}, Stderr: io.Discard}
	for name, cfg := range map[string]Config{
		"relative executable": {Argv: []string{"echo"}, CWD: valid.CWD, Env: []string{}, Stderr: io.Discard},
		"relative cwd":        {Argv: valid.Argv, CWD: "relative", Env: []string{}, Stderr: io.Discard},
		"inherited env":       {Argv: valid.Argv, CWD: valid.CWD, Stderr: io.Discard},
		"missing stderr sink": {Argv: valid.Argv, CWD: valid.CWD, Env: []string{}},
	} {
		t.Run(name, func(t *testing.T) {
			if c, err := Start(context.Background(), cfg); err == nil {
				_ = c.Close()
				t.Fatal("unsafe start configuration was accepted")
			}
		})
	}
}

func TestACPStartValidatesAndReapsProcessBeforeExposure(t *testing.T) {
	called := false
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cfg := Config{
		Argv:   []string{exe, "-test.run=^TestACPHelperProcess$"},
		CWD:    t.TempDir(),
		Env:    []string{"GO_WANT_ACP_HELPER=1", "ACP_HELPER_MODE=silence"},
		Stderr: io.Discard,
		ValidateProcess: func(pid int) error {
			called = pid > 0
			return errors.New("outside containment")
		},
	}
	if client, err := Start(context.Background(), cfg); err == nil || client != nil {
		t.Fatalf("unsafe process was exposed: client=%#v err=%v", client, err)
	}
	if !called {
		t.Fatal("process validator did not receive a live PID")
	}
}

func TestACPHappyFlowAdvertisesNoOptionalClientSurface(t *testing.T) {
	c := startTestClient(t, "happy", nil)
	init := initializeAndSession(t, c)
	if init.ProtocolVersion != 1 || init.AgentCapabilities.LoadSession || init.AgentCapabilities.ResumeSession {
		t.Fatalf("unexpected negotiation: %#v", init)
	}
	resultCh := make(chan PromptResult, 1)
	errCh := make(chan error, 1)
	go func() {
		result, err := c.Prompt(context.Background(), "hello")
		resultCh <- result
		errCh <- err
	}()
	select {
	case update := <-c.Updates():
		if update.Method != "session/update" {
			t.Fatalf("update method=%q", update.Method)
		}
	case <-time.After(time.Second):
		t.Fatal("session update not observed")
	}
	if err := <-errCh; err != nil {
		t.Fatal(err)
	}
	result := <-resultCh
	if result.Outcome != OutcomeCompleted || result.Delivery != DeliveryTerminalReceived || result.TurnID != "turn-1" {
		t.Fatalf("prompt result=%#v", result)
	}
}

func TestACPProtocolFailuresPoisonBeforeContinuation(t *testing.T) {
	for _, mode := range []string{"wrong-version", "malformed", "oversize", "unknown-id"} {
		t.Run(mode, func(t *testing.T) {
			c := startTestClient(t, mode, nil)
			if _, err := c.Initialize(context.Background()); err == nil {
				t.Fatal("adversarial initialize unexpectedly succeeded")
			}
			if _, err := c.NewSession(context.Background()); err == nil {
				t.Fatal("poisoned client accepted session creation")
			}
		})
	}
}

func TestACPDuplicateResponsePoisonsClient(t *testing.T) {
	c := startTestClient(t, "duplicate-response", nil)
	_, _ = c.Initialize(context.Background())
	deadline := time.Now().Add(time.Second)
	for {
		c.mu.Lock()
		err := c.protocolErr
		c.mu.Unlock()
		if err != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("duplicate response did not poison client")
		}
		time.Sleep(time.Millisecond)
	}
	if _, err := c.NewSession(context.Background()); err == nil {
		t.Fatal("poisoned client accepted session creation")
	}
}

func TestACPMatchedErrorResponsesAreNotDeliveryUnknown(t *testing.T) {
	for _, mode := range []string{"prompt-rpc-error", "missing-result"} {
		t.Run(mode, func(t *testing.T) {
			c := startTestClient(t, mode, nil)
			initializeAndSession(t, c)
			result, err := c.Prompt(context.Background(), "known response")
			if err == nil || errors.Is(err, ErrDeliveryUnknown) || result.Outcome != OutcomeProtocolFailed || result.Delivery != DeliveryResponseSeen {
				t.Fatalf("result=%#v err=%v, want known protocol failure", result, err)
			}
			if mode == "missing-result" {
				if _, err := c.Prompt(context.Background(), "must not continue"); err == nil {
					t.Fatal("malformed matched response left client reusable")
				}
			}
		})
	}
}

func TestACPEOFAfterPromptIsDeliveryUnknown(t *testing.T) {
	c := startTestClient(t, "eof-after-prompt", nil)
	initializeAndSession(t, c)
	result, err := c.Prompt(context.Background(), "ambiguous")
	if !errors.Is(err, ErrDeliveryUnknown) || result.Outcome != OutcomeDeliveryUnknown {
		t.Fatalf("result=%#v err=%v, want delivery_unknown", result, err)
	}
}

func TestACPUpdateOverflowPoisonsInsteadOfHidingTerminal(t *testing.T) {
	c := startTestClient(t, "flood", func(cfg *Config) { cfg.Limits.MaxUpdates = 2 })
	initializeAndSession(t, c)
	result, err := c.Prompt(context.Background(), "flood")
	if err == nil || (result.Outcome != OutcomeDeliveryUnknown && result.Outcome != OutcomePoisoned) {
		t.Fatalf("result=%#v err=%v, want explicit poisoned/unknown outcome", result, err)
	}
}

func TestACPPermissionDefaultsRejectAndNeverSelectsAllowAlways(t *testing.T) {
	t.Run("nil handler rejects", func(t *testing.T) {
		c := startTestClient(t, "permission", nil)
		initializeAndSession(t, c)
		result, err := c.Prompt(context.Background(), "permission")
		if err != nil || result.Outcome != OutcomeRefused {
			t.Fatalf("result=%#v err=%v", result, err)
		}
	})
	t.Run("allow once chooses only allow_once option", func(t *testing.T) {
		c := startTestClient(t, "permission", func(cfg *Config) {
			cfg.PermissionHandler = func(context.Context, PermissionRequest) (PermissionDecision, error) { return AllowOnce, nil }
		})
		initializeAndSession(t, c)
		result, err := c.Prompt(context.Background(), "permission")
		if err != nil || result.Outcome != OutcomeCompleted {
			t.Fatalf("result=%#v err=%v", result, err)
		}
	})
}

func TestACPPermissionMustMatchActiveSession(t *testing.T) {
	called := false
	c := startTestClient(t, "permission-mismatch", func(cfg *Config) {
		cfg.PermissionHandler = func(context.Context, PermissionRequest) (PermissionDecision, error) {
			called = true
			return AllowOnce, nil
		}
	})
	initializeAndSession(t, c)
	_, err := c.Prompt(context.Background(), "mismatch")
	if err == nil {
		t.Fatal("mismatched permission request did not fail the turn")
	}
	if called {
		t.Fatal("policy handler ran for mismatched session")
	}
}

func TestACPCancelResolvesBlockedPermissionAsCancelled(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	c := startTestClient(t, "permission", func(cfg *Config) {
		cfg.PermissionHandler = func(context.Context, PermissionRequest) (PermissionDecision, error) {
			close(started)
			<-release
			return AllowOnce, nil
		}
	})
	initializeAndSession(t, c)
	type promptAnswer struct {
		result PromptResult
		err    error
	}
	promptDone := make(chan promptAnswer, 1)
	go func() {
		result, err := c.Prompt(context.Background(), "permission")
		promptDone <- promptAnswer{result: result, err: err}
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("permission handler did not start")
	}
	if err := c.Cancel(context.Background()); err != nil {
		t.Fatalf("cancel did not drain cancelled permission: %v", err)
	}
	c.mu.Lock()
	pendingPermissions := len(c.permissionCancel) + len(c.permissionDone) + len(c.inbound)
	c.mu.Unlock()
	if pendingPermissions != 0 {
		t.Fatalf("cancel returned with %d pending permission records", pendingPermissions)
	}
	answer := <-promptDone
	close(release)
	if answer.err != nil || answer.result.Outcome != OutcomeRefused {
		t.Fatalf("prompt=%#v err=%v, want cancelled permission refusal", answer.result, answer.err)
	}
}

func TestACPCancelDrainDoesNotStealPromptResponse(t *testing.T) {
	c := startTestClient(t, "ignore-cancel", nil)
	initializeAndSession(t, c)
	promptErr := make(chan error, 1)
	go func() { _, err := c.Prompt(context.Background(), "cancel"); promptErr <- err }()
	select {
	case <-c.Updates():
	case <-time.After(time.Second):
		t.Fatal("prompt never became active")
	}
	err := c.Cancel(context.Background())
	var outcome *OutcomeError
	if !errors.As(err, &outcome) || outcome.Outcome != OutcomePoisoned {
		t.Fatalf("cancel err=%v, want poisoned cancellation", err)
	}
	select {
	case err := <-promptErr:
		if err == nil {
			t.Fatal("prompt succeeded after ignored cancellation")
		}
	case <-time.After(time.Second):
		t.Fatal("prompt did not settle after poisoned cancellation")
	}
}

func TestACPConcurrentPromptIsRejected(t *testing.T) {
	c := startTestClient(t, "hold-prompt", nil)
	initializeAndSession(t, c)
	first := make(chan error, 1)
	go func() { _, err := c.Prompt(context.Background(), "first"); first <- err }()
	select {
	case <-c.Updates():
	case <-time.After(time.Second):
		t.Fatal("first prompt did not start")
	}
	if _, err := c.Prompt(context.Background(), "second"); !errors.Is(err, ErrPromptActive) {
		t.Fatalf("second prompt err=%v, want ErrPromptActive", err)
	}
	if err := <-first; err != nil {
		t.Fatal(err)
	}
}

func TestACPUnknownStopReasonIsProtocolFailure(t *testing.T) {
	c := startTestClient(t, "unknown-stop", nil)
	initializeAndSession(t, c)
	result, err := c.Prompt(context.Background(), "unknown")
	if result.Outcome != OutcomeProtocolFailed || !errors.Is(err, ErrProtocol) {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}

func TestACPCancelDeadlineCoversBlockedPromptWrite(t *testing.T) {
	c := startTestClient(t, "no-read-after-session", func(cfg *Config) {
		cfg.Limits.MaxFrameBytes = 4 << 20
		cfg.Timeouts.CancelDrain = 75 * time.Millisecond
	})
	initializeAndSession(t, c)
	promptDone := make(chan error, 1)
	go func() {
		_, err := c.Prompt(context.Background(), strings.Repeat("x", 2<<20))
		promptDone <- err
	}()
	deadline := time.Now().Add(time.Second)
	for {
		c.mu.Lock()
		active := c.activePrompt != nil
		c.mu.Unlock()
		if active {
			break
		}
		select {
		case err := <-promptDone:
			t.Fatalf("prompt failed before blocked-write state: %v", err)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("prompt was not published before blocked write")
		}
		time.Sleep(time.Millisecond)
	}
	started := time.Now()
	if err := c.Cancel(context.Background()); err == nil {
		t.Fatal("blocked cancel unexpectedly succeeded")
	}
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("cancel exceeded bounded deadline: %s", elapsed)
	}
	select {
	case err := <-promptDone:
		if err == nil {
			t.Fatal("blocked prompt succeeded after cancellation")
		}
	case <-time.After(time.Second):
		t.Fatal("blocked prompt did not unblock after transport poison")
	}
}

func TestACPPendingRequestOverflowPoisons(t *testing.T) {
	c := startTestClient(t, "silence", func(cfg *Config) {
		cfg.Limits.MaxPendingRequests = 1
		cfg.Timeouts.Stall = time.Second
	})
	first, err := c.beginCall("initialize", map[string]any{"protocolVersion": 1}, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.beginCall("session/new", map[string]any{"cwd": c.cfg.CWD}, false); err == nil {
		t.Fatal("pending request overflow was accepted")
	}
	select {
	case result := <-first.done:
		if result.err == nil {
			t.Fatal("overflow did not fail incumbent request")
		}
	case <-time.After(time.Second):
		t.Fatal("overflow did not settle incumbent request")
	}
}
