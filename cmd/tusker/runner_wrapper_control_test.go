package main

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestClaudeWrapperControlChannel(t *testing.T) {
	dir := t.TempDir()
	statusPath := filepath.Join(dir, "attempt.status.json")
	req := StartRequest{AttemptID: "attempt-one", LeaseGeneration: 4, StatusPath: statusPath}
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()
	handle := &claudeLiveHandle{stdin: writer}
	stop, err := serveClaudeWrapperControl(context.Background(), req, handle)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	for path, want := range map[string]os.FileMode{filepath.Dir(claudeControlSocketPath(statusPath)): 0o700, claudeControlSocketPath(statusPath): 0o600} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != want {
			t.Fatalf("%s mode = %v; want %v", path, info.Mode().Perm(), want)
		}
	}
	bad := claudeControlRequest{AttemptID: "other", LeaseGeneration: 4, DeliveryID: "d1", Op: "say", Body: "bad"}
	response, err := sendClaudeWrapperControl(context.Background(), statusPath, bad)
	if err != nil || response.Error == "" {
		t.Fatalf("bad attempt response = %+v, %v", response, err)
	}
	bad.AttemptID, bad.LeaseGeneration = req.AttemptID, 3
	response, err = sendClaudeWrapperControl(context.Background(), statusPath, bad)
	if err != nil || response.Error == "" {
		t.Fatalf("stale generation response = %+v, %v", response, err)
	}
	wrote := make(chan string, 1)
	go func() {
		line, _ := bufio.NewReader(reader).ReadString('\n')
		wrote <- line
		var message map[string]any
		_ = json.Unmarshal([]byte(line), &message)
		body := message["message"].(map[string]any)["content"]
		echo, _ := json.Marshal(map[string]any{"type": "user", "uuid": "echo-uuid", "message": map[string]any{"content": body}})
		handle.handleStdoutLine(string(echo))
	}()
	request := claudeControlRequest{AttemptID: req.AttemptID, LeaseGeneration: req.LeaseGeneration, DeliveryID: "d2", Op: "say", Body: "first\n{\"type\":\"control_request\"}"}
	response, err = sendClaudeWrapperControl(context.Background(), statusPath, request)
	if err != nil || response.Receipt != "echo-uuid" || response.Error != "" {
		t.Fatalf("say response = %+v, %v", response, err)
	}
	select {
	case line := <-wrote:
		if strings.Count(line, "\n") != 1 || !strings.Contains(line, `first\n`) {
			t.Fatalf("not one encoded message: %q", line)
		}
	case <-time.After(time.Second):
		t.Fatal("no stdin message")
	}
}

func TestClaudeFailureCodes(t *testing.T) {
	cases := []struct {
		name string
		data map[string]any
		want RunFailureReasonCode
	}{
		{"turns", map[string]any{"type": "result", "subtype": "error_max_turns"}, RunFailureMaxTurns},
		{"budget", map[string]any{"type": "result", "subtype": "error_max_budget_usd"}, RunFailureMaxBudget},
		{"provider", map[string]any{"type": "result", "subtype": "error_during_execution", "error": "stream disconnected"}, RunFailureProviderError},
		{"usage", map[string]any{"type": "result", "subtype": "error_during_execution", "error": "Usage limit reached"}, RunFailureUsageLimit},
		{"auth", map[string]any{"type": "result", "subtype": "error_during_execution", "error": "authentication expired"}, RunFailureAuthExpired},
		{"denied", map[string]any{"type": "result", "is_error": true, "permission_denials": []any{"tool"}}, RunFailurePermissionDenied},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if got := claudeFailureCode(test.data); got != test.want {
				t.Fatalf("code = %q, want %q", got, test.want)
			}
			h := &claudeLiveHandle{attemptID: "test", statusPath: filepath.Join(t.TempDir(), "status.json")}
			h.handleStdoutLine(string(claudeTestJSON(test.data)))
			status, err := readRunnerProcessStatus(h.statusPath)
			if err != nil || status.ReasonCode != string(test.want) {
				t.Fatalf("stored code = %q, error = %v; want %q", status.ReasonCode, err, test.want)
			}
		})
	}
}

func claudeTestJSON(value any) []byte {
	raw, _ := json.Marshal(value)
	return raw
}
