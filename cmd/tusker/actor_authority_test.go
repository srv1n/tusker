package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestV7ActorAuthorityFamilies(t *testing.T) {
	clearAgentSessionEnvForTest(t)
	tests := []struct {
		name      string
		resolve   func(Args) (string, error)
		args      Args
		want      string
		wantError string
	}{
		{name: "human-required missing", resolve: func(a Args) (string, error) { return v7HumanActor(a, "wave start") }, args: Args{}, wantError: "explicit qualified actor"},
		{name: "human-required canonical", resolve: func(a Args) (string, error) { return v7HumanActor(a, "wave start") }, args: Args{"by": " HUMAN: sarav "}, want: "human:sarav"},
		{name: "reviewer-required canonical", resolve: func(a Args) (string, error) { return v7ReviewerOrHumanActor(a, "accept") }, args: Args{"by": " REVIEWER: independent "}, want: "reviewer:independent"},
		{name: "agent-default", resolve: func(a Args) (string, error) { return v7AgentDefaultActor(a, "task status") }, args: Args{"USER": "ignored"}, want: "agent:" + defaultActorName()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.resolve(tt.args)
			if tt.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantError) {
					t.Fatalf("expected error containing %q, got actor=%q err=%v", tt.wantError, got, err)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Fatalf("actor=%q err=%v, want %q", got, err, tt.want)
			}
		})
	}
}

func TestV7ActorAuthorityAcceptsHumanFromEveryAgentSession(t *testing.T) {
	for _, key := range []string{"TUSKER_ATTEMPT_ID", "CODEX_SHELL", "CODEX_THREAD_ID", "CLAUDECODE", "CLAUDE_CODE_ENTRYPOINT"} {
		t.Run(key, func(t *testing.T) {
			clearAgentSessionEnvForTest(t)
			t.Setenv(key, "session")
			for _, resolve := range []func(Args) (string, error){
				func(a Args) (string, error) { return v7HumanActor(a, "wave start") },
				func(a Args) (string, error) { return v7ReviewerOrHumanActor(a, "close") },
				func(a Args) (string, error) { return v7AgentDefaultActor(a, "task status") },
			} {
				if got, err := resolve(Args{"by": "HuMaN:sarav", "force": "true", "local": "true"}); err != nil || got != "human:sarav" {
					t.Fatalf("human actor in %s session = %q, %v", key, got, err)
				}
			}
		})
	}
}

func TestServeOperatorActorUsesOnlyExplicitConfiguration(t *testing.T) {
	clearAgentSessionEnvForTest(t)
	t.Setenv("TUSKER_SERVE_OPERATOR", "")
	t.Setenv("TUSKER_ACTOR", "")
	if got := configuredServeOperatorActor(); got != "" {
		t.Fatalf("serve operator inherited an implicit actor %q", got)
	}
	t.Setenv("TUSKER_SERVE_OPERATOR", "human:operator")
	if got := configuredServeOperatorActor(); got != "human:operator" {
		t.Fatalf("configured serve operator=%q", got)
	}
	server := &serveServer{operatorActor: "human:operator"}
	if got, err := server.serveOperatorActor(serveActionBody{}, "serve task run"); err != nil || got != "human:operator" {
		t.Fatalf("configured serve operator resolution=%q err=%v", got, err)
	}
	for _, raw := range []string{"operator"} {
		t.Setenv("TUSKER_SERVE_OPERATOR", raw)
		if got := configuredServeOperatorActor(); got != "" {
			t.Fatalf("non-human serve actor %q was accepted as %q", raw, got)
		}
	}
	for _, raw := range []string{"reviewer:operator", "agent:operator"} {
		t.Setenv("TUSKER_SERVE_OPERATOR", raw)
		if got := configuredServeOperatorActor(); got != raw {
			t.Fatalf("qualified serve actor %q was normalized as %q", raw, got)
		}
	}
}

func TestServeOperatorActorAcceptsConfiguredHumanFromAgentSession(t *testing.T) {
	clearAgentSessionEnvForTest(t)
	server := &serveServer{operatorActor: "human:operator"}
	for _, key := range []string{"TUSKER_ATTEMPT_ID", "CODEX_THREAD_ID"} {
		t.Run(key, func(t *testing.T) {
			t.Setenv(key, "session")
			if got, err := server.serveOperatorActor(serveActionBody{}, "serve task run"); err != nil || got != "human:operator" {
				t.Fatalf("serve actor from %s = %q, %v", key, got, err)
			}
		})
	}
}

func TestInternalActorSeamSeparatesDaemonAndTuskerFromPublicFlags(t *testing.T) {
	clearAgentSessionEnvForTest(t)
	for _, raw := range []string{"daemon:completion-reactor", "tusker:batch-gate"} {
		if _, err := v7AgentDefaultActor(Args{"by": raw}, "task status"); err == nil {
			t.Fatalf("public actor resolver accepted internal actor %q", raw)
		}
		actor, err := newV7InternalActor(raw)
		if err != nil || actor.value != raw {
			t.Fatalf("internal actor %q = %#v err=%v", raw, actor, err)
		}
	}
	for _, raw := range []string{"daemon:", "tusker:", "evil:worker"} {
		if _, err := newV7InternalActor(raw); err == nil {
			t.Fatalf("invalid internal actor %q was accepted", raw)
		}
	}
	t.Setenv("CODEX_THREAD_ID", "thread-internal")
	if got, err := v7HumanActor(Args{"by": "human:operator"}, "task status"); err != nil || got != "human:operator" {
		t.Fatalf("human actor in agent session = %q, %v", got, err)
	}
}

func TestReviewResultActorHonorsConfiguredReviewerAndSession(t *testing.T) {
	note := Note{Data: map[string]any{"schema": "tusker.task/v7", "kind": "task"}}
	clearAgentSessionEnvForTest(t)
	if got, err := resolveReviewResultActor(Args{}, "reviewer:independent", note); err != nil || got != "reviewer:independent" {
		t.Fatalf("default configured reviewer=%q err=%v", got, err)
	}
	if got, err := resolveReviewResultActor(Args{"by": " REVIEWER:independent "}, "reviewer:independent", note); err != nil || got != "reviewer:independent" {
		t.Fatalf("canonical reviewer=%q err=%v", got, err)
	}
	for _, key := range []string{"CODEX_THREAD_ID", "TUSKER_ATTEMPT_ID"} {
		t.Run(key, func(t *testing.T) {
			t.Setenv(key, "review-session")
			if got, err := resolveReviewResultActor(Args{"by": "human:owner"}, "human:owner", note); err != nil || got != "human:owner" {
				t.Fatalf("configured human reviewer in %s session = %q, %v", key, got, err)
			}
		})
	}
}

func TestV7CreationActorsCanonicalizeAndAcceptAgentHuman(t *testing.T) {
	clearAgentSessionEnvForTest(t)
	vault := pickupV7TestVault(t)
	if err := newV7Task(Args{"vault": vault, "quiet": "true", "epic": "APP", "id": "APP-T-0099", "title": "Actor provenance", "risk": "low", "priority": "p1", "by": " AGENT:builder "}); err != nil {
		t.Fatal(err)
	}
	taskData, _, err := parseFrontmatterMustRead(filepath.Join(vault, "work", "tasks", "APP-T-0099.md"))
	if err != nil {
		t.Fatal(err)
	}
	if got := stringField(taskData, "created_by"); got != "agent:builder" {
		t.Fatalf("task created_by=%q, want canonical agent:builder", got)
	}
	if err := newV7Gate(Args{"vault": vault, "quiet": "true", "blocks": "APP-T-0099", "kind": "verification", "owner": "reviewer:independent", "action": "Review the task proof.", "verification": "Reviewer records the result.", "by": " REVIEWER:independent "}); err != nil {
		t.Fatal(err)
	}
	gateData, _, err := parseFrontmatterMustRead(filepath.Join(vault, "work", "gates", "APP-G-0001.md"))
	if err != nil {
		t.Fatal(err)
	}
	if got := stringField(gateData, "created_by"); got != "reviewer:independent" {
		t.Fatalf("gate created_by=%q, want canonical reviewer:independent", got)
	}
	if err := newV7Domain(Args{"vault": vault, "quiet": "true", "id": "actor-provenance", "title": "Actor provenance", "by": " HUMAN:operator "}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_THREAD_ID", "creation-session")
	if err := newV7Domain(Args{"vault": vault, "quiet": "true", "id": "agent-owned-domain", "by": "human:operator"}); err != nil {
		t.Fatalf("human actor from agent session: %v", err)
	}
}
