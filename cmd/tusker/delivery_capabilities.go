package main

import (
	"fmt"
	"sort"
	"strings"
)

const strictV2ProofAuthorityCapability = "strict_v2_proof_authority/v1"

const unavailableCapabilityRemedy = "install or select a binary that enforces the exact required capability"

type unavailableCapabilityContext struct {
	MissingCapabilities []string          `json:"missing_capabilities"`
	Installed           VersionProjection `json:"installed"`
	Remedy              string            `json:"remedy"`
}

// deliveryRequiredCapabilities is deliberately small and explicit. A plan may
// only request capabilities this binary can actually enforce; accepting an
// opaque string would turn the field into decorative metadata.
func deliveryRequiredCapabilities(raw []string) ([]string, error) {
	seen := map[string]bool{}
	out := make([]string, 0, len(raw))
	for _, capability := range raw {
		capability = strings.TrimSpace(capability)
		if capability == "" {
			return nil, fmt.Errorf("required_capabilities cannot contain an empty capability")
		}
		if seen[capability] {
			continue
		}
		seen[capability] = true
		out = append(out, capability)
	}
	sort.Strings(out)
	return out, nil
}

func deliveryCapabilityAvailable(capability string) bool {
	switch strings.TrimSpace(capability) {
	case strictV2ProofAuthorityCapability:
		// K0 only parses and refuses. K1-K5 must first land the canonical
		// contract, exact proof, strict close, typed completion, and adversarial
		// kernel proof under an automation-off independent review boundary.
		return false
	default:
		return false
	}
}

func deliveryUnavailableCapabilities(required []string) ([]string, error) {
	normalized, err := deliveryRequiredCapabilities(required)
	if err != nil {
		return nil, err
	}
	var unavailable []string
	for _, capability := range normalized {
		if !deliveryCapabilityAvailable(capability) {
			unavailable = append(unavailable, capability)
		}
	}
	return unavailable, nil
}

func deliveryRequireCapabilities(required []string) error {
	unavailable, err := deliveryUnavailableCapabilities(required)
	if err != nil {
		return err
	}
	if len(unavailable) > 0 {
		return fmt.Errorf("required capability unavailable: %s", strings.Join(unavailable, ", "))
	}
	return nil
}

func unavailableDeliveryCapabilityError(unavailable []string) error {
	return tuskerError(
		errorInvalidArg,
		"required capability unavailable: "+strings.Join(unavailable, ", "),
		withHint(unavailableCapabilityRemedy),
		withContext(unavailableCapabilityContext{
			MissingCapabilities: append([]string(nil), unavailable...),
			Installed:           installedVersionProjection(),
			Remedy:              unavailableCapabilityRemedy,
		}),
	)
}
