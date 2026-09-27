package runner

import (
	"encoding/json"
	"testing"

	"tusker/internal/acp"
)

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
