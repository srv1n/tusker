package main

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"tusker/internal/acp"
)

// The fake ACP fixture advertises mode/model config options only when
// CODEX_CONFIG and INITIAL_AGENT_MODE are set.
func newDevinConfigFixtureClient(t *testing.T, mode string) (*acp.Client, acp.Session) {
	t.Helper()
	client, err := acp.Start(context.Background(), acp.Config{
		Argv: []string{fakeACPBinary(t)}, CWD: t.TempDir(), Stderr: io.Discard,
		Env: []string{
			`CODEX_CONFIG={"model":"fixture-model","model_reasoning_effort":"high"}`,
			"INITIAL_AGENT_MODE=" + mode,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	if _, err := client.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	session, err := client.NewSession(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return client, session
}

func TestDevinACPModeForPolicyAcceptsReviewReadOnly(t *testing.T) {
	policy := codexPolicyForLane(CodexPolicy{
		ApprovalPolicy:     "never",
		ThreadSandbox:      "workspace-write",
		TurnSandboxPolicy:  "workspace-write",
		TurnSandboxNetwork: boolPtr(true),
	}, runLaneReview)
	if policy.ThreadSandbox != "read-only" || policy.TurnSandboxPolicy != "read-only" {
		t.Fatalf("review lane did not resolve read-only: %#v", policy)
	}
	mode, err := devinACPModeForPolicy(policy)
	if err != nil || mode != "plan" {
		t.Fatalf("review policy mode=%q err=%v, want plan", mode, err)
	}
}

func TestDevinACPModeForPolicyKeepsExecuteSmart(t *testing.T) {
	mode, err := devinACPModeForPolicy(CodexPolicy{
		ApprovalPolicy:     "never",
		ThreadSandbox:      "workspace-write",
		TurnSandboxPolicy:  "workspace-write",
		TurnSandboxNetwork: boolPtr(true),
	})
	if err != nil || mode != "smart" {
		t.Fatalf("execute policy mode=%q err=%v, want smart", mode, err)
	}
}

func TestDevinACPModeForPolicyFullAccess(t *testing.T) {
	mode, err := devinACPModeForPolicy(CodexPolicy{TurnSandboxPolicy: "danger-full-access"})
	if err != nil || mode != "bypass" {
		t.Fatalf("full access mode=%q err=%v, want bypass", mode, err)
	}
}

func TestDevinACPModeForPolicyStillRefusesUnsupportedPolicies(t *testing.T) {
	for _, tc := range []struct {
		name   string
		policy CodexPolicy
		want   string
	}{
		{name: "offline workspace-write", policy: CodexPolicy{ThreadSandbox: "workspace-write", TurnSandboxPolicy: "workspace-write", TurnSandboxNetwork: boolPtr(false)}, want: "network"},
		{name: "workspace-write missing network bit", policy: CodexPolicy{ThreadSandbox: "workspace-write", TurnSandboxPolicy: "workspace-write"}, want: "network"},
		{name: "blank sandbox", policy: CodexPolicy{}, want: "read-only, networked workspace-write"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mode, err := devinACPModeForPolicy(tc.policy)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("mode=%q err=%v, want refusal containing %q", mode, err, tc.want)
			}
		})
	}
}

func TestConfigureDevinSessionAcceptsReadOnlyReviewPolicy(t *testing.T) {
	client, session := newDevinConfigFixtureClient(t, "plan")
	policy := codexPolicyForLane(CodexPolicy{
		ApprovalPolicy:     "never",
		ThreadSandbox:      "workspace-write",
		TurnSandboxPolicy:  "workspace-write",
		TurnSandboxNetwork: boolPtr(true),
	}, runLaneReview)
	if err := configureDevinSession(context.Background(), client, session, policy, "fixture-model", "high"); err != nil {
		t.Fatalf("Devin review session refused the read-only lane policy: %v", err)
	}
}

func TestConfigureDevinSessionAcceptsNetworkedExecutePolicy(t *testing.T) {
	client, session := newDevinConfigFixtureClient(t, "smart")
	policy := CodexPolicy{
		ApprovalPolicy:     "never",
		ThreadSandbox:      "workspace-write",
		TurnSandboxPolicy:  "workspace-write",
		TurnSandboxNetwork: boolPtr(true),
	}
	if err := configureDevinSession(context.Background(), client, session, policy, "fixture-model", "high"); err != nil {
		t.Fatalf("Devin execute session refused the networked workspace-write policy: %v", err)
	}
}

func TestDevinACPMapsSWETwoMaxToAdvertisedModelAndThought(t *testing.T) {
	// Recorded from a prompt-free Devin ACP session/new handshake.
	var payload struct {
		ConfigOptions []acp.ConfigOption `json:"configOptions"`
	}
	if err := json.Unmarshal([]byte(`{"configOptions":[{"id":"model","name":"Model","type":"select","currentValue":"swe-2-high","options":[{"value":"adaptive","name":"Adaptive"},{"value":"swe-2-high","name":"SWE-2"}]},{"id":"thought_level","name":"Thinking","type":"select","currentValue":"medium","options":[{"value":"medium","name":"Medium"},{"value":"high","name":"High"},{"value":"max","name":"Max"}]}]}`), &payload); err != nil {
		t.Fatal(err)
	}
	model, thought := devinACPModelAndThought(acp.Session{ConfigOptions: payload.ConfigOptions}, "swe-2-max", "max")
	if model != "swe-2-high" || thought != "max" {
		t.Fatalf("model=%q thought=%q", model, thought)
	}
	model, thought = devinACPModelAndThought(acp.Session{}, "swe-2-max", "max")
	if model != "swe-2-max" || thought != "" {
		t.Fatalf("unadvertised model mapped: model=%q thought=%q", model, thought)
	}
}
