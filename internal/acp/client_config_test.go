package acp

import (
	"encoding/json"
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

func TestDevinMCPServersChangedNotificationIsInformational(t *testing.T) {
	for _, method := range []string{"_cognition.ai/mcp/serversChanged", "_cognition.ai/output"} {
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
