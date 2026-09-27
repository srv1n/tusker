package main

import (
	"strings"
	"testing"
)

func TestV7ProposalActorRequiresExplicitQualifiedIdentity(t *testing.T) {
	clearAgentSessionEnvForTest(t)
	if _, err := proposalV7Actor(Args{}, "proposal accepted", true); err == nil || !strings.Contains(err.Error(), "explicit") {
		t.Fatalf("missing proposal reviewer should refuse without an inferred human: %v", err)
	}
	if _, err := proposalV7Actor(Args{"by": "sarav"}, "proposal apply", false); err == nil || !strings.Contains(err.Error(), "qualified") {
		t.Fatalf("bare proposal actor should refuse: %v", err)
	}
}

func TestV7ProposalActorAllowsIndependentReviewer(t *testing.T) {
	clearAgentSessionEnvForTest(t)
	got, err := proposalV7Actor(Args{"by": "reviewer:independent"}, "proposal accepted", true)
	if err != nil || got != "reviewer:independent" {
		t.Fatalf("independent reviewer actor = %q, err=%v", got, err)
	}
}

func TestV7ProposalActorAllowsExplicitHumanOutsideAgentSession(t *testing.T) {
	clearAgentSessionEnvForTest(t)
	got, err := proposalV7Actor(Args{"by": "  HUMAN: sarav  ", "local": "true", "force": "true"}, "proposal apply", false)
	if err != nil || got != "human:sarav" {
		t.Fatalf("explicit human actor = %q, err=%v", got, err)
	}
}

func TestV7ProposalActorRejectsUnknownOrBlankActorKinds(t *testing.T) {
	clearAgentSessionEnvForTest(t)
	for _, raw := range []string{"evil:name", "human:", "reviewer:   ", "agent"} {
		if _, err := proposalV7Actor(Args{"by": raw}, "proposal apply", false); err == nil || !strings.Contains(err.Error(), "actor kind") && !strings.Contains(err.Error(), "qualified actor") {
			t.Fatalf("actor %q should be refused, got %v", raw, err)
		}
	}
}

func TestV7ProposalActorCanonicalizesMixedCaseHumanInAgentSession(t *testing.T) {
	clearAgentSessionEnvForTest(t)
	t.Setenv("CODEX_THREAD_ID", "thread-1")
	got, err := proposalV7Actor(Args{"by": "HuMaN:sarav"}, "proposal apply", false)
	if err != nil || got != "human:sarav" {
		t.Fatalf("mixed-case human actor = %q, %v", got, err)
	}
}

func TestV7ProposalActorAcceptsHumanInAgentSessionEvenWithForceOrLocal(t *testing.T) {
	for _, key := range []string{"CODEX_SHELL", "CODEX_THREAD_ID", "CLAUDECODE", "CLAUDE_CODE_ENTRYPOINT", "TUSKER_ATTEMPT_ID"} {
		t.Run(key, func(t *testing.T) {
			clearAgentSessionEnvForTest(t)
			t.Setenv(key, "agent-session")
			got, err := proposalV7Actor(Args{"by": "human:sarav", "local": "true", "force": "true"}, "proposal apply", false)
			if err != nil || got != "human:sarav" {
				t.Fatalf("human actor in %s session = %q, %v", key, got, err)
			}
		})
	}
}

func TestV7ProposalActorAllowsExplicitAgentForApply(t *testing.T) {
	clearAgentSessionEnvForTest(t)
	got, err := proposalV7Actor(Args{"by": "agent:codex"}, "proposal apply", false)
	if err != nil || got != "agent:codex" {
		t.Fatalf("explicit agent apply actor = %q, err=%v", got, err)
	}
}

func TestV7ProposalCreationActorDefaultsToAgent(t *testing.T) {
	clearAgentSessionEnvForTest(t)
	t.Setenv("TUSKER_ACTOR", "")
	t.Setenv("USER", "fixture-user")
	got, err := proposalV7CreationActor(Args{})
	if err != nil || got != "agent:fixture-user" {
		t.Fatalf("implicit proposal actor = %q, err=%v", got, err)
	}
}

func TestV7ProposalCreationActorAcceptsHumanFromAgentSession(t *testing.T) {
	clearAgentSessionEnvForTest(t)
	t.Setenv("CODEX_THREAD_ID", "thread-creation")
	got, err := proposalV7CreationActor(Args{"by": "HuMaN:fixture-user"})
	if err != nil || got != "human:fixture-user" {
		t.Fatalf("proposal creation human actor = %q, %v", got, err)
	}
}

func TestV7ProposalCreationActorCanonicalizesExplicitKind(t *testing.T) {
	clearAgentSessionEnvForTest(t)
	got, err := proposalV7CreationActor(Args{"by": " REVIEWER: fixture-reviewer "})
	if err != nil || got != "reviewer:fixture-reviewer" {
		t.Fatalf("canonical proposal actor = %q, err=%v", got, err)
	}
}
