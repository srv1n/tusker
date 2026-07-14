package v7policy

import "strings"

type ClosePolicy struct {
	RequiredAcceptor string
	RequiredEvidence []string
	RequiredGates    []string
}

type ClosePolicyConfigFile struct {
	ClosePolicy map[string]ClosePolicyConfigRule `yaml:"close_policy"`
}

type ClosePolicyConfigRule struct {
	RequiredAcceptor string    `yaml:"required_acceptor"`
	RequiredEvidence *[]string `yaml:"required_evidence"`
	RequiredGates    *[]string `yaml:"required_gates"`
}

func AcceptorAllowed(actor, requiredAcceptor string) bool {
	switch requiredAcceptor {
	case "human":
		return strings.HasPrefix(actor, "human:")
	case "reviewer_agent":
		return strings.HasPrefix(actor, "reviewer:") || strings.HasPrefix(actor, "human:")
	default:
		return true
	}
}

func DefaultClosePolicy(risk string) ClosePolicy {
	return ClosePolicy{
		RequiredAcceptor: "reviewer_agent",
		RequiredEvidence: RequiredEvidence(risk),
		RequiredGates:    RequiredGateKinds(risk),
	}
}

func RequiredEvidence(risk string) []string {
	return nil
}

func RequiredGateKinds(risk string) []string {
	return nil
}
