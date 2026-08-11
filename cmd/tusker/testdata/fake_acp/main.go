// Command fake_acp is a deterministic, provider-free ACP v1 fixture.
//
// It deliberately has no dependencies outside the standard library.  The
// conformance tests launch it as a child process and select one of the modes
// below with --mode.  Stdout is protocol-only; the child-process mode writes
// its diagnostic PID to stderr so a caller can prove that an inherited pipe
// is eventually closed.
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	protocolVersion = 1
	codexACPVersion = "1.1.14"
	defaultSession  = "fake-session"
	defaultUpdates  = 1024
	defaultHoldMS   = 2000
	defaultOversize = 4*1024*1024 + 1
)

type message struct {
	JSONRPC string          `json:"jsonrpc,omitempty"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  any             `json:"params,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   any             `json:"error,omitempty"`
}

var stdoutMu sync.Mutex

func main() {
	mode := flag.String("mode", "happy", "fixture mode")
	flag.Parse()

	// These environment overrides make the adversarial cases deterministic
	// without making the test harness depend on a shell or a scripting runtime.
	updates := envInt("FAKE_ACP_UPDATE_COUNT", defaultUpdates)
	holdMS := envInt("FAKE_ACP_HOLD_MS", defaultHoldMS)
	oversizeBytes := envInt("FAKE_ACP_OVERSIZE_BYTES", defaultOversize)
	if updates < 0 {
		updates = 0
	}
	if holdMS < 0 {
		holdMS = 0
	}
	if oversizeBytes < 1 {
		oversizeBytes = defaultOversize
	}

	s := &server{
		mode:         *mode,
		updates:      updates,
		hold:         time.Duration(holdMS) * time.Millisecond,
		oversizeByte: oversizeBytes,
		in:           bufio.NewReader(os.Stdin),
	}
	if s.mode == "pipe-holder" {
		time.Sleep(s.hold)
		return
	}
	s.serve()
}

func envInt(name string, fallback int) int {
	n, err := strconv.Atoi(os.Getenv(name))
	if err != nil {
		return fallback
	}
	return n
}

type server struct {
	mode         string
	updates      int
	hold         time.Duration
	oversizeByte int
	in           *bufio.Reader
	session      string
	codexConfig  map[string]string
}

func (s *server) serve() {
	s.session = defaultSession
	if s.mode == "malformed" {
		// A truncated JSON object is a protocol error, not a JSON-RPC error.
		writeRaw([]byte(`{"jsonrpc":"2.0","id":1,"result":`))
		return
	}
	if s.mode == "oversize" {
		writeRaw(bytes.Repeat([]byte("x"), s.oversizeByte))
		return
	}
	if s.mode == "silence" {
		// Keep the process alive until the client closes stdin.  This exercises
		// initialization and turn deadlines without a sleeping test process.
		_, _ = io.Copy(io.Discard, s.in)
		return
	}

	var initialized bool
	for {
		line, err := s.in.ReadBytes('\n')
		if len(line) != 0 {
			var req message
			if json.Unmarshal(bytes.TrimSpace(line), &req) != nil {
				// The fake never tries to recover from malformed client input.
				return
			}
			switch req.Method {
			case "initialize":
				if initialized {
					return
				}
				initialized = true
				s.initialize(req)
			case "session/new", "session/load", "session/resume":
				if !initialized {
					return
				}
				s.newSession(req)
			case "session/set_config_option":
				if !initialized {
					return
				}
				s.setConfigOption(req)
			case "session/prompt":
				if !initialized {
					return
				}
				s.prompt(req)
			case "session/cancel":
				if s.mode == "ignore-cancel" {
					// Intentionally do nothing.  The supervisor must poison and
					// reap this process after its cancellation drain deadline.
					continue
				}
			default:
				// Notifications and unknown methods are observations for this
				// fixture.  Never execute arbitrary method names.
			}
		}
		if err != nil {
			return
		}
	}
}

func (s *server) initialize(req message) {
	if s.mode == "unknown-id" || s.mode == "duplicate-id" {
		// A client must ignore an otherwise well-formed response whose id is
		// not pending.  It must still process the matching response below.
		write(message{JSONRPC: "2.0", ID: json.RawMessage("999"), Result: map[string]any{
			"protocolVersion": protocolVersion,
		}})
	}
	version := protocolVersion
	if s.mode == "wrong-version" {
		version = 2
	}
	agentName, agentVersion := "fake-acp", "1"
	if s.codexConfigMode() {
		agentName = "@agentclientprotocol/codex-acp"
		agentVersion = codexACPVersion
	}
	write(message{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]any{
			"protocolVersion": version,
			"agentInfo":       map[string]string{"name": agentName, "version": agentVersion},
			"agentCapabilities": map[string]any{
				"loadSession":   false,
				"resumeSession": false,
			},
		},
	})
}

func (s *server) codexConfigMode() bool {
	if strings.TrimSpace(os.Getenv("CODEX_CONFIG")) == "" || strings.TrimSpace(os.Getenv("INITIAL_AGENT_MODE")) == "" {
		return false
	}
	var config struct {
		Model string `json:"model"`
	}
	return json.Unmarshal([]byte(os.Getenv("CODEX_CONFIG")), &config) == nil && strings.TrimSpace(config.Model) != ""
}

func (s *server) newSession(req message) {
	params := map[string]any{}
	paramsBytes, _ := json.Marshal(req.Params)
	_ = json.Unmarshal(paramsBytes, &params)
	if cwd, ok := params["cwd"].(string); ok && !strings.HasPrefix(cwd, "/") {
		write(message{JSONRPC: "2.0", ID: req.ID, Error: map[string]any{
			"code": -32602, "message": "cwd must be absolute",
		}})
		return
	}
	result := map[string]any{"sessionId": s.session}
	if s.configureCodexSession() {
		result["configOptions"] = s.codexConfigOptions()
	}
	write(message{JSONRPC: "2.0", ID: req.ID, Result: result})
	if s.mode == "duplicate-id" {
		// Duplicate responses are adversarial input; a client must not treat
		// the second response as a second session transition.
		write(message{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{"sessionId": s.session}})
	}
}

func (s *server) configureCodexSession() bool {
	raw := os.Getenv("CODEX_CONFIG")
	if raw == "" {
		return false
	}
	var config struct {
		Model  string `json:"model"`
		Effort string `json:"model_reasoning_effort"`
	}
	if json.Unmarshal([]byte(raw), &config) != nil || strings.TrimSpace(config.Model) == "" {
		return false
	}
	mode := strings.TrimSpace(os.Getenv("INITIAL_AGENT_MODE"))
	if mode == "" {
		return false
	}
	s.codexConfig = map[string]string{"model": config.Model, "reasoning_effort": config.Effort, "mode": mode}
	return true
}

func (s *server) setConfigOption(req message) {
	if s.codexConfig == nil {
		write(message{JSONRPC: "2.0", ID: req.ID, Error: map[string]any{"code": -32601, "message": "config unsupported"}})
		return
	}
	params := map[string]any{}
	paramsBytes, _ := json.Marshal(req.Params)
	_ = json.Unmarshal(paramsBytes, &params)
	id, _ := params["configId"].(string)
	value, _ := params["value"].(string)
	if _, exists := s.codexConfig[id]; !exists || value != s.codexConfig[id] {
		write(message{JSONRPC: "2.0", ID: req.ID, Error: map[string]any{"code": -32602, "message": "unexpected config"}})
		return
	}
	write(message{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{"configOptions": s.codexConfigOptions()}})
}

func (s *server) codexConfigOptions() []map[string]any {
	if s.codexConfig == nil {
		return nil
	}
	return []map[string]any{
		{"id": "mode", "name": "Mode", "type": "select", "currentValue": s.codexConfig["mode"], "options": []map[string]string{{"value": s.codexConfig["mode"], "name": s.codexConfig["mode"]}}},
		{"id": "model", "name": "Model", "type": "select", "currentValue": s.codexConfig["model"], "options": []map[string]string{{"value": s.codexConfig["model"], "name": s.codexConfig["model"]}}},
		{"id": "reasoning_effort", "name": "Reasoning effort", "type": "select", "currentValue": s.codexConfig["reasoning_effort"], "options": []map[string]string{{"value": s.codexConfig["reasoning_effort"], "name": s.codexConfig["reasoning_effort"]}}},
	}
}

func (s *server) prompt(req message) {
	switch s.mode {
	case "eof-after-prompt":
		// Exit immediately after accepting the prompt.  This is the precise
		// ambiguous-delivery fixture: the client has written a turn but cannot
		// observe a terminal response.
		os.Exit(0)
	case "ignore-cancel":
		writeUpdate(s.session, "working")
		// Keep reading cancel notifications while the timer is pending.  A
		// real supervisor should terminate us before this timer expires.
		timer := time.NewTimer(s.hold)
		defer timer.Stop()
		for {
			result := make(chan []byte, 1)
			go func() {
				line, _ := s.in.ReadBytes('\n')
				result <- line
			}()
			select {
			case <-timer.C:
				writeTerminal(req.ID)
				return
			case line := <-result:
				if len(line) == 0 {
					return
				}
				var next message
				if json.Unmarshal(bytes.TrimSpace(line), &next) != nil {
					return
				}
				// Deliberately ignore session/cancel and any other message.
			}
		}
	case "permission-request":
		write(message{JSONRPC: "2.0", ID: json.RawMessage("9001"), Method: "session/request_permission", Params: map[string]any{
			"sessionId": s.session,
			"options":   []map[string]string{{"optionId": "allow_once", "name": "Allow once"}},
			"toolCall":  map[string]any{"kind": "read", "target": "workspace/file.txt"},
		}})
		// Wait for the matching permission response.  Unknown IDs and unrelated
		// notifications are intentionally ignored.
		for {
			line, err := s.in.ReadBytes('\n')
			if err != nil {
				return
			}
			var reply message
			if json.Unmarshal(bytes.TrimSpace(line), &reply) != nil {
				return
			}
			if string(reply.ID) == "9001" {
				writeTerminal(req.ID)
				return
			}
		}
	case "update-flood", "terminal-behind-flood":
		for i := 0; i < s.updates; i++ {
			writeUpdate(s.session, fmt.Sprintf("update-%d", i))
		}
		writeTerminal(req.ID)
	case "unknown-id":
		writeUpdate(s.session, "unknown-id-before-terminal")
		writeTerminal(req.ID)
	case "duplicate-id":
		writeTerminal(req.ID)
		writeTerminal(req.ID)
	default:
		writeUpdate(s.session, "working")
		writeTerminal(req.ID)
	}
	if s.mode == "child-holds-pipe" {
		// This mode is intentionally after terminal delivery.  The process tree
		// still owns stdout until the descendant exits, which catches supervisors
		// that wait on a pipe instead of enforcing a bounded reap.
		child := exec.Command(os.Args[0], "--mode", "pipe-holder")
		child.Stdout = os.Stdout
		child.Stderr = os.Stderr
		if err := child.Start(); err == nil {
			_, _ = fmt.Fprintf(os.Stderr, "child_pid=%d\n", child.Process.Pid)
		}
	}
}

func writeUpdate(session, text string) {
	write(message{JSONRPC: "2.0", Method: "session/update", Params: map[string]any{
		"sessionId": session,
		"update": map[string]any{
			"sessionUpdate": "agent_message_chunk",
			"content":       map[string]string{"type": "text", "text": text},
		},
	}})
}

func writeTerminal(id json.RawMessage) {
	write(message{JSONRPC: "2.0", ID: id, Result: map[string]string{"stopReason": "end_turn"}})
}

func write(v message) {
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	writeRaw(append(b, '\n'))
}

func writeRaw(b []byte) {
	stdoutMu.Lock()
	defer stdoutMu.Unlock()
	_, _ = os.Stdout.Write(b)
}
