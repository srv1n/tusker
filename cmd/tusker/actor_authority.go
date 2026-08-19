package main

import "strings"

type v7ActorPolicy struct {
	AllowedKinds map[string]bool
	DefaultAgent bool
}

// v7InternalActor is deliberately not representable through Args/CLI flags.
// Only trusted in-process seams may construct one, and construction accepts
// daemon:/ or tusker:/ provenance exclusively.
type v7InternalActor struct{ value string }

func newV7InternalActor(raw string) (v7InternalActor, error) {
	parts := strings.SplitN(strings.TrimSpace(raw), ":", 2)
	if len(parts) != 2 {
		return v7InternalActor{}, tuskerError(errorInvalidField, "internal actor must be daemon:<name> or tusker:<name>")
	}
	kind, name := strings.ToLower(strings.TrimSpace(parts[0])), strings.TrimSpace(parts[1])
	if name == "" || (kind != "daemon" && kind != "tusker") {
		return v7InternalActor{}, tuskerError(errorInvalidField, "internal actor must be daemon:<name> or tusker:<name>")
	}
	return v7InternalActor{value: kind + ":" + name}, nil
}

func statusV7CmdAsInternalActor(args Args, raw string) error {
	actor, err := newV7InternalActor(raw)
	if err != nil {
		return err
	}
	return statusV7CmdWithInternalActor(args, &actor)
}

// resolveV7Actor is the single attribution boundary for durable mutations.
// It canonicalizes actor kinds, rejects unknown namespaces, and prevents an
// agent session from claiming human authority. Force/local flags deliberately
// do not participate: they control mutation routing, not identity.
func resolveV7Actor(args Args, operation string, policy v7ActorPolicy) (string, error) {
	raw := strings.TrimSpace(firstNonEmpty(args.String("by"), args.String("actor")))
	if raw == "" && policy.DefaultAgent {
		raw = "agent:" + defaultActorName()
	}
	if raw == "" {
		return "", tuskerError(errorMissingArg, operation+" requires an explicit qualified actor",
			withHint("pass --by human:<name>, reviewer:<name>, or agent:<name>; actor identity is never inferred as human"))
	}
	actor, ok := normalizeV7ProposalActor(raw)
	if !ok {
		return "", tuskerError(errorInvalidField, operation+" requires actor kind human, reviewer, or agent with a non-blank name, got "+raw)
	}
	kind := strings.SplitN(actor, ":", 2)[0]
	if len(policy.AllowedKinds) > 0 && !policy.AllowedKinds[kind] {
		return "", tuskerError(errorInvalidField, operation+" requires an actor of kind "+strings.Join(sortedStrings(mapKeys(policy.AllowedKinds)), " or ")+", got "+actor)
	}
	if kind == "human" && agentSessionKind() != "" {
		return "", tuskerError(errorInvalidTransition,
			operation+" cannot use human actor "+actor+" from "+agentSessionKind(),
			withHint("run the mutation from a human terminal with explicit --by human:<name>; no agent break-glass contract exists"),
			withContext(map[string]any{"execution_role": agentSessionKind(), "actor": actor, "operation": operation}))
	}
	return actor, nil
}

func v7HumanActor(args Args, operation string) (string, error) {
	return resolveV7Actor(args, operation, v7ActorPolicy{AllowedKinds: map[string]bool{"human": true}})
}

func v7ReviewerOrHumanActor(args Args, operation string) (string, error) {
	return resolveV7Actor(args, operation, v7ActorPolicy{AllowedKinds: map[string]bool{"human": true, "reviewer": true}})
}

func v7AgentDefaultActor(args Args, operation string) (string, error) {
	return resolveV7Actor(args, operation, v7ActorPolicy{AllowedKinds: map[string]bool{"agent": true, "human": true, "reviewer": true}, DefaultAgent: true})
}

func mapKeys(values map[string]bool) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	return keys
}

func normalizeV7ProposalActor(raw string) (string, bool) {
	parts := strings.SplitN(strings.TrimSpace(raw), ":", 2)
	if len(parts) != 2 {
		return "", false
	}
	kind := strings.ToLower(strings.TrimSpace(parts[0]))
	name := strings.TrimSpace(parts[1])
	if name == "" || (kind != "human" && kind != "reviewer" && kind != "agent") {
		return "", false
	}
	return kind + ":" + name, true
}
