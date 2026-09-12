package main

import runnercore "tusker/internal/runner"

// commandPolicyDecision is the command-policy authority used by every native
// callback. Adapters remain responsible for classifying their tool request;
// this function owns the fixed automatic/ask/block precedence.
func commandPolicyDecision(policy CodexPolicy, request runnercore.CommandPolicyRequest) runnercore.CommandPolicyDecision {
	commandPolicy := policy.CommandPolicy
	if commandPolicy.IsZero() {
		// Legacy permission_preset profiles retain their historical callback
		// behavior. Their existing sandbox fields still provide the review-only
		// write guard, while recognized destructive requests remain approvable.
		commandPolicy = runnercore.NewCommandPolicy(activeCodexPolicyIsReviewOnly(policy), "ask")
	}
	return runnercore.EvaluateCommandPolicy(commandPolicy, request)
}

func activeCodexPolicyIsReviewOnly(policy CodexPolicy) bool {
	return firstNonEmpty(policy.TurnSandboxPolicy, policy.ThreadSandbox) == "read-only"
}

func commandPolicyRejectReason(policy CodexPolicy, request runnercore.CommandPolicyRequest) string {
	decision := commandPolicyDecision(policy, request)
	if decision.Behavior == runnercore.CommandBlock {
		return decision.Reason
	}
	return ""
}
