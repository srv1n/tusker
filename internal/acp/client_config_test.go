package acp

import (
	"encoding/json"
	"fmt"
	"testing"
)

func TestParseConfigOptionsSkipsDynamicSelectWithoutValues(t *testing.T) {
	options, err := parseConfigOptions([]byte(`[{"id":"model","name":"Model","type":"select","currentValue":"swe-fast","options":[]},{"id":"mode","name":"Mode","type":"select","currentValue":"ask","options":[{"value":"ask","name":"Ask"}]}]`))
	if err != nil {
		t.Fatal(err)
	}
	if len(options) != 1 || options[0].ID != "mode" {
		t.Fatalf("options=%#v", options)
	}
}

func TestParseConfigOptionsAcceptsLargeProviderModelCatalog(t *testing.T) {
	values := make([]map[string]string, 385)
	for i := range values {
		values[i] = map[string]string{"value": fmt.Sprintf("model-%d", i), "name": fmt.Sprintf("Model %d", i)}
	}
	raw, err := json.Marshal([]map[string]any{{"id": "model", "name": "Model", "type": "select", "currentValue": "model-0", "options": values}})
	if err != nil {
		t.Fatal(err)
	}
	options, err := parseConfigOptions(raw)
	if err != nil || len(options) != 1 || len(options[0].Options) != len(values) {
		t.Fatalf("options=%d values=%d err=%v", len(options), len(values), err)
	}
}

func TestDevinNotificationsAreInformational(t *testing.T) {
	for _, method := range []string{"_cognition.ai/mcp/serversChanged", "_cognition.ai/output", "_cognition.ai/thinking_complete", "_cognition.ai/plugins/changed"} {
		c := &Client{}
		c.handleRequest(rpcMessage{JSONRPC: "2.0", Method: method, Params: json.RawMessage(`{}`)})
		if c.protocolErr != nil {
			t.Fatalf("%s notification poisoned client: %v", method, c.protocolErr)
		}
	}
}

func TestSessionUpdateDuringNewSessionIsAccepted(t *testing.T) {
	c := &Client{pending: map[string]*pendingCall{"1": {method: "session/new"}}}
	err := c.validateUpdateEnvelope(json.RawMessage(`{"sessionId":"new-session","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"starting"}}}`), 1)
	if err != nil {
		t.Fatal(err)
	}
}
