package runner

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"tusker/internal/acp"
)

func TestACPCappedStderrFixture(t *testing.T) {
	if os.Getenv("TUSKER_ACP_STDERR_FIXTURE") != "1" {
		return
	}
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var request struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		if json.Unmarshal(scanner.Bytes(), &request) != nil {
			os.Exit(2)
		}
		var result any
		switch request.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": 1, "agentCapabilities": map[string]any{}, "authMethods": []any{}}
		case "session/new":
			result = map[string]any{"sessionId": "bounded-session"}
		case "session/prompt":
			_, _ = os.Stderr.WriteString(strings.Repeat("stderr-payload", 32*1024))
			result = map[string]any{"stopReason": "end_turn"}
		default:
			continue
		}
		response, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": result})
		_, _ = os.Stdout.Write(append(response, '\n'))
	}
	os.Exit(0)
}

func TestExecuteACPBoundsStderrWhileWriting(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := executeACP(context.Background(), PreparedLaunch{
		Argv: []string{exe, "-test.run=^TestACPCappedStderrFixture$"}, CWD: t.TempDir(),
		Environment:  append(os.Environ(), "TUSKER_ACP_STDERR_FIXTURE=1"),
		Capabilities: map[string]bool{"native_containment": true}, Deadline: 10 * time.Second,
		OutputLimit: 4096,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Outcome != EventCompleted || len(receipt.Stderr) != 4096 || !receipt.StderrTruncated {
		t.Fatalf("stderr receipt: outcome=%s bytes=%d truncated=%t", receipt.Outcome, len(receipt.Stderr), receipt.StderrTruncated)
	}
}

// The live canary must negotiate the same Devin model mapping as dispatch:
// swe-2-max is the profile ID but the ACP session advertises swe-2-high plus a
// separate thought_level option.
func TestDevinACPLiveCheckSharesModelMapping(t *testing.T) {
	var payload struct {
		ConfigOptions []acp.ConfigOption `json:"configOptions"`
	}
	if err := json.Unmarshal([]byte(`{"configOptions":[{"id":"model","name":"Model","type":"select","currentValue":"swe-2-high","options":[{"value":"adaptive","name":"Adaptive"},{"value":"swe-2-high","name":"SWE-2"}]},{"id":"thought_level","name":"Thinking","type":"select","currentValue":"medium","options":[{"value":"medium","name":"Medium"},{"value":"high","name":"High"},{"value":"max","name":"Max"}]}]}`), &payload); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name        string
		session     acp.Session
		model       string
		effort      string
		wantModel   string
		wantThought string
	}{
		{name: "swe-2-max maps to advertised model and thought", session: acp.Session{ConfigOptions: payload.ConfigOptions}, model: "swe-2-max", effort: "max", wantModel: "swe-2-high", wantThought: "max"},
		{name: "unadvertised swe-2-high passes the raw model through", session: acp.Session{}, model: "swe-2-max", effort: "max", wantModel: "swe-2-max", wantThought: ""},
		{name: "non-max effort passes through", session: acp.Session{ConfigOptions: payload.ConfigOptions}, model: "swe-2-max", effort: "high", wantModel: "swe-2-max", wantThought: ""},
		{name: "other models pass through", session: acp.Session{ConfigOptions: payload.ConfigOptions}, model: "swe-2-high", effort: "max", wantModel: "swe-2-high", wantThought: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			model, thought := DevinACPModelAndThought(tc.session, tc.model, tc.effort)
			if model != tc.wantModel || thought != tc.wantThought {
				t.Fatalf("model=%q thought=%q, want %q %q", model, thought, tc.wantModel, tc.wantThought)
			}
		})
	}
}
