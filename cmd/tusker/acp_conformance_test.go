package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// This harness speaks only the small ACP v1 wire subset needed by the fake
// agent.  Keeping it provider-free makes protocol and process failures
// reproducible on a clean checkout (and keeps the future internal/acp client
// free to choose its own public types).
type acpTestMessage struct {
	JSONRPC string          `json:"jsonrpc,omitempty"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   json.RawMessage `json:"error,omitempty"`
}

type acpLine struct {
	data []byte
	err  error
}

type fakeACPProcess struct {
	cmd      *exec.Cmd
	stdin    io.WriteCloser
	stdout   <-chan acpLine
	stderr   <-chan acpLine
	done     <-chan struct{}
	waitErr  error
	stopOnce sync.Once
}

var fakeACPBuild struct {
	sync.Once
	path string
	err  error
}

func fakeACPBinary(t *testing.T) string {
	t.Helper()
	fakeACPBuild.Do(func() {
		_, file, _, ok := runtime.Caller(0)
		if !ok {
			fakeACPBuild.err = errors.New("runtime.Caller failed")
			return
		}
		root := filepath.Dir(filepath.Dir(filepath.Dir(file)))
		tmp, err := os.MkdirTemp("", "tusker-fake-acp-")
		if err != nil {
			fakeACPBuild.err = err
			return
		}
		fakeACPBuild.path = filepath.Join(tmp, "fake-acp")
		cmd := exec.Command("go", "build", "-trimpath", "-o", fakeACPBuild.path, "./cmd/tusker/testdata/fake_acp")
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
		if out, err := cmd.CombinedOutput(); err != nil {
			fakeACPBuild.err = fmt.Errorf("build fake ACP: %w\n%s", err, out)
		}
	})
	if fakeACPBuild.err != nil {
		t.Fatal(fakeACPBuild.err)
	}
	return fakeACPBuild.path
}

func startFakeACP(t *testing.T, mode string) *fakeACPProcess {
	t.Helper()
	cmd := exec.Command(fakeACPBinary(t), "--mode", mode)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, stdoutWriter, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stderr, stderrWriter, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stdout = stdoutWriter
	cmd.Stderr = stderrWriter
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	// Keep the read ends in the harness.  Unlike os/exec's StdoutPipe,
	// explicit pipes remain open when the parent exits, so a descendant that
	// inherited the write end is observable until it releases the descriptor.
	_ = stdoutWriter.Close()
	_ = stderrWriter.Close()

	out := make(chan acpLine, 16)
	errOut := make(chan acpLine, 16)
	go readACPLines(stdout, out)
	go readACPLines(stderr, errOut)
	done := make(chan struct{})
	p := &fakeACPProcess{cmd: cmd, stdin: stdin, stdout: out, stderr: errOut, done: done}
	go func() {
		p.waitErr = cmd.Wait()
		close(done)
	}()
	t.Cleanup(func() { p.stop(t) })
	return p
}

func readACPLines(r io.Reader, dst chan<- acpLine) {
	defer close(dst)
	reader := bufio.NewReader(r)
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) != 0 {
			dst <- acpLine{data: line}
		}
		if err != nil {
			dst <- acpLine{err: err}
			return
		}
	}
}

func (p *fakeACPProcess) send(t *testing.T, id int, method string, params any) {
	t.Helper()
	var raw json.RawMessage
	if params != nil {
		var err error
		raw, err = json.Marshal(params)
		if err != nil {
			t.Fatal(err)
		}
	}
	msg := acpTestMessage{JSONRPC: "2.0", ID: json.RawMessage(strconv.Itoa(id)), Method: method, Params: raw}
	b, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	b = append(b, '\n')
	if _, err := p.stdin.Write(b); err != nil {
		t.Fatal(err)
	}
}

func (p *fakeACPProcess) sendRaw(t *testing.T, msg any) {
	t.Helper()
	b, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.stdin.Write(append(b, '\n')); err != nil {
		t.Fatal(err)
	}
}

func (p *fakeACPProcess) sendNotification(t *testing.T, method string, params any) {
	t.Helper()
	b, err := json.Marshal(acpTestMessage{JSONRPC: "2.0", Method: method, Params: acpRawJSON(params)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.stdin.Write(append(b, '\n')); err != nil {
		t.Fatal(err)
	}
}

func acpRawJSON(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

func (p *fakeACPProcess) next(t *testing.T, timeout time.Duration) ([]byte, error) {
	t.Helper()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case line, ok := <-p.stdout:
		if !ok {
			return nil, io.EOF
		}
		if line.err != nil {
			return line.data, line.err
		}
		return line.data, nil
	case <-timer.C:
		return nil, fmt.Errorf("timeout after %s", timeout)
	}
}

func (p *fakeACPProcess) nextMessage(t *testing.T, timeout time.Duration) acpTestMessage {
	t.Helper()
	line, err := p.next(t, timeout)
	if err != nil {
		t.Fatal(err)
	}
	var msg acpTestMessage
	if err := json.Unmarshal(bytes.TrimSpace(line), &msg); err != nil {
		t.Fatalf("invalid ACP message %q: %v", line, err)
	}
	return msg
}

func (p *fakeACPProcess) nextStderr(t *testing.T, timeout time.Duration) string {
	t.Helper()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case line, ok := <-p.stderr:
		if !ok {
			return ""
		}
		return string(line.data)
	case <-timer.C:
		t.Fatalf("timeout waiting for fake ACP stderr")
		return ""
	}
}

func (p *fakeACPProcess) waitFor(t *testing.T, timeout time.Duration) error {
	t.Helper()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-p.done:
		return p.waitErr
	case <-timer.C:
		return fmt.Errorf("fake ACP did not exit within %s", timeout)
	}
}

func (p *fakeACPProcess) stop(t *testing.T) {
	t.Helper()
	p.stopOnce.Do(func() {
		_ = p.stdin.Close()
		if p.cmd.Process != nil {
			_ = p.cmd.Process.Kill()
		}
		select {
		case <-p.done:
		case <-time.After(3 * time.Second):
			t.Errorf("fake ACP process did not stop")
		}
	})
}

func handshake(t *testing.T, p *fakeACPProcess) {
	t.Helper()
	p.send(t, 1, "initialize", map[string]any{"protocolVersion": 1})
	init := p.nextMessage(t, time.Second)
	if string(init.ID) != "1" {
		t.Fatalf("initialize response id=%s, want 1", init.ID)
	}
	var result struct {
		ProtocolVersion int `json:"protocolVersion"`
	}
	if err := json.Unmarshal(init.Result, &result); err != nil {
		t.Fatal(err)
	}
	if result.ProtocolVersion != 1 {
		t.Fatalf("protocol version=%d, want 1", result.ProtocolVersion)
	}
	p.send(t, 2, "session/new", map[string]any{"cwd": t.TempDir()})
	created := p.nextMessage(t, time.Second)
	if string(created.ID) != "2" {
		t.Fatalf("session/new response id=%s, want 2", created.ID)
	}
}

func prompt(t *testing.T, p *fakeACPProcess) {
	t.Helper()
	p.send(t, 3, "session/prompt", map[string]any{
		"sessionId": "fake-session",
		"prompt":    []map[string]string{{"type": "text", "text": "hello"}},
	})
}

func TestACPFakeAgentHappyFlow(t *testing.T) {
	p := startFakeACP(t, "happy")
	handshake(t, p)
	prompt(t, p)
	update := p.nextMessage(t, time.Second)
	if update.Method != "session/update" {
		t.Fatalf("first prompt message method=%q, want session/update", update.Method)
	}
	terminal := p.nextMessage(t, time.Second)
	if string(terminal.ID) != "3" || terminal.Result == nil {
		t.Fatalf("terminal response=%#v", terminal)
	}
}

func TestACPFakeAgentAdversarialFrames(t *testing.T) {
	t.Run("silence", func(t *testing.T) {
		p := startFakeACP(t, "silence")
		p.send(t, 1, "initialize", map[string]any{"protocolVersion": 1})
		if line, err := p.next(t, 100*time.Millisecond); err == nil {
			t.Fatalf("silence fixture emitted initialize response %q", line)
		}
	})
	t.Run("malformed", func(t *testing.T) {
		p := startFakeACP(t, "malformed")
		line, err := p.next(t, time.Second)
		if err != nil {
			t.Fatal(err)
		}
		if json.Valid(bytes.TrimSpace(line)) {
			t.Fatalf("malformed fixture emitted valid JSON: %q", line)
		}
	})
	t.Run("oversize", func(t *testing.T) {
		p := startFakeACP(t, "oversize")
		line, err := p.next(t, 2*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		if len(line) <= 4*1024*1024 {
			t.Fatalf("oversize frame length=%d, want > 4 MiB", len(line))
		}
	})
	t.Run("wrong version", func(t *testing.T) {
		p := startFakeACP(t, "wrong-version")
		p.send(t, 1, "initialize", map[string]any{"protocolVersion": 1})
		msg := p.nextMessage(t, time.Second)
		var result struct {
			ProtocolVersion int `json:"protocolVersion"`
		}
		if err := json.Unmarshal(msg.Result, &result); err != nil {
			t.Fatal(err)
		}
		if result.ProtocolVersion == 1 {
			t.Fatalf("wrong-version fixture negotiated protocol 1: %#v", msg)
		}
	})
}

func TestACPFakeAgentPermissionAndIdentifiers(t *testing.T) {
	t.Run("relative cwd rejected", func(t *testing.T) {
		p := startFakeACP(t, "happy")
		p.send(t, 1, "initialize", map[string]any{"protocolVersion": 1})
		_ = p.nextMessage(t, time.Second)
		p.send(t, 2, "session/new", map[string]any{"cwd": "relative/workspace"})
		msg := p.nextMessage(t, time.Second)
		if string(msg.ID) != "2" || msg.Error == nil {
			t.Fatalf("relative cwd response=%#v, want id=2 error", msg)
		}
	})
	t.Run("permission request", func(t *testing.T) {
		p := startFakeACP(t, "permission-request")
		handshake(t, p)
		prompt(t, p)
		permission := p.nextMessage(t, time.Second)
		if permission.Method != "session/request_permission" || string(permission.ID) != "9001" {
			t.Fatalf("permission request=%#v", permission)
		}
		p.sendRaw(t, map[string]any{"jsonrpc": "2.0", "id": 9001, "result": map[string]string{"outcome": "allow_once"}})
		terminal := p.nextMessage(t, time.Second)
		if string(terminal.ID) != "3" {
			t.Fatalf("permission terminal id=%s, want 3", terminal.ID)
		}
	})
	t.Run("unknown id", func(t *testing.T) {
		p := startFakeACP(t, "unknown-id")
		p.send(t, 1, "initialize", map[string]any{"protocolVersion": 1})
		unknown := p.nextMessage(t, time.Second)
		if string(unknown.ID) != "999" {
			t.Fatalf("first response id=%s, want unknown 999", unknown.ID)
		}
		matching := p.nextMessage(t, time.Second)
		if string(matching.ID) != "1" {
			t.Fatalf("matching response id=%s, want 1", matching.ID)
		}
	})
	t.Run("duplicate id", func(t *testing.T) {
		p := startFakeACP(t, "duplicate-id")
		p.send(t, 1, "initialize", map[string]any{"protocolVersion": 1})
		_ = p.nextMessage(t, time.Second) // unknown id
		_ = p.nextMessage(t, time.Second) // matching id
		p.send(t, 2, "session/new", map[string]any{"cwd": t.TempDir()})
		first := p.nextMessage(t, time.Second)
		second := p.nextMessage(t, time.Second)
		if string(first.ID) != "2" || string(second.ID) != "2" {
			t.Fatalf("duplicate session IDs: %s and %s", first.ID, second.ID)
		}
	})
}

func TestACPFakeAgentFloodKeepsTerminalObservable(t *testing.T) {
	for _, mode := range []string{"update-flood", "terminal-behind-flood"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("FAKE_ACP_UPDATE_COUNT", "512")
			p := startFakeACP(t, mode)
			handshake(t, p)
			prompt(t, p)
			updates := 0
			for {
				msg := p.nextMessage(t, 3*time.Second)
				if msg.Method == "session/update" {
					updates++
					continue
				}
				if string(msg.ID) != "3" || msg.Result == nil {
					t.Fatalf("unexpected control message after %d updates: %#v", updates, msg)
				}
				break
			}
			if updates != 512 {
				t.Fatalf("updates=%d, want 512", updates)
			}
		})
	}
}

func TestACPFakeAgentDeliveryAndCancellationCases(t *testing.T) {
	t.Run("EOF after prompt", func(t *testing.T) {
		p := startFakeACP(t, "eof-after-prompt")
		handshake(t, p)
		prompt(t, p)
		if _, err := p.next(t, time.Second); !errors.Is(err, io.EOF) {
			t.Fatalf("prompt EOF error=%v, want EOF", err)
		}
	})
	t.Run("ignored cancel", func(t *testing.T) {
		t.Setenv("FAKE_ACP_HOLD_MS", "1000")
		p := startFakeACP(t, "ignore-cancel")
		handshake(t, p)
		prompt(t, p)
		if msg := p.nextMessage(t, time.Second); msg.Method != "session/update" {
			t.Fatalf("initial ignore-cancel message=%#v", msg)
		}
		p.sendNotification(t, "session/cancel", map[string]string{"sessionId": "fake-session"})
		if line, err := p.next(t, 100*time.Millisecond); err == nil {
			t.Fatalf("ignored cancel unexpectedly produced output %q", line)
		}
	})
}

func TestACPFakeAgentDescendantHoldingPipe(t *testing.T) {
	t.Setenv("FAKE_ACP_HOLD_MS", "150")
	p := startFakeACP(t, "child-holds-pipe")
	handshake(t, p)
	prompt(t, p)
	_ = p.nextMessage(t, time.Second)
	terminal := p.nextMessage(t, time.Second)
	if string(terminal.ID) != "3" {
		t.Fatalf("terminal id=%s, want 3", terminal.ID)
	}
	diagnostic := p.nextStderr(t, time.Second)
	if !strings.Contains(diagnostic, "child_pid=") {
		t.Fatalf("child diagnostic=%q, want child_pid", diagnostic)
	}
	// Close stdin so the parent exits; the descendant keeps stdout open for a
	// bounded interval.  Wait must complete after the child releases the pipe.
	started := time.Now()
	if err := p.stdin.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-p.done:
		// The parent is allowed to exit; the descendant is what must keep the
		// inherited stdout pipe open until its bounded hold expires.
	case <-time.After(time.Second):
		t.Fatal("fake ACP parent did not exit after stdin close")
	}
	if _, err := p.next(t, 40*time.Millisecond); err == nil || !strings.Contains(err.Error(), "timeout") {
		t.Fatalf("inherited stdout closed before descendant hold elapsed: %v", err)
	}
	if _, err := p.next(t, 3*time.Second); !errors.Is(err, io.EOF) {
		t.Fatalf("stdout did not close after descendant exit: %v", err)
	}
	if elapsed := time.Since(started); elapsed < 100*time.Millisecond {
		t.Fatalf("inherited stdout closed in %s; expected bounded child pipe hold", elapsed)
	}
}
