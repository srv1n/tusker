package main

import (
	"strings"
)

// completionReactorMode is an authority projection only. No code should use
// this type to start the reactor until the deterministic completion transaction
// is implemented and explicitly made a consumer of authoritative mode.
type completionReactorMode string

const (
	completionReactorModeDisabled      completionReactorMode = "disabled"
	completionReactorModeShadow        completionReactorMode = "shadow"
	completionReactorModeAuthoritative completionReactorMode = "authoritative"
	completionReactorModeLegacy        completionReactorMode = "legacy"
)

type completionReactorModeProjection struct {
	Configured string `json:"configured,omitempty"`
	Effective  string `json:"effective"`
	Provenance string `json:"provenance"`
	Warning    string `json:"warning,omitempty"`
	Repair     string `json:"repair,omitempty"`
}

const legacyCompletionReactorModeWarning = "automation.completion_reactor.mode is absent on an enabled project; preserving legacy completion authority"
const legacyCompletionReactorModeRepair = "set automation.completion_reactor.mode: disabled, shadow, or authoritative"

func defaultCompletionReactorMode() completionReactorModeProjection {
	return completionReactorModeProjection{
		Configured: string(completionReactorModeDisabled),
		Effective:  string(completionReactorModeDisabled),
		Provenance: "fresh default",
	}
}

// completionReactorModeFromLayer reads raw YAML rather than the merged schema
// value so an absent mode is distinguishable from an explicit mode in a lower
// precedence layer. That distinction is the whole compatibility contract.
func completionReactorModeFromLayer(layer tuskerConfigLayer) (string, bool, error) {
	automationRaw, ok := layer.Raw["automation"]
	if !ok {
		return "", false, nil
	}
	automation, ok := automationRaw.(map[string]any)
	if !ok {
		return "", false, tuskerError(errorConfigInvalid, "automation must be a mapping", withPath(layer.Path))
	}
	reactorRaw, ok := automation["completion_reactor"]
	if !ok {
		return "", false, nil
	}
	reactor, ok := reactorRaw.(map[string]any)
	if !ok {
		return "", false, tuskerError(errorConfigInvalid, "automation.completion_reactor must be a mapping", withPath(layer.Path), withHint("set automation.completion_reactor.mode to disabled, shadow, or authoritative"))
	}
	rawMode, ok := reactor["mode"]
	if !ok {
		return "", false, tuskerError(errorConfigInvalid, "automation.completion_reactor requires mode", withPath(layer.Path), withHint("set automation.completion_reactor.mode to disabled, shadow, or authoritative"))
	}
	mode, ok := rawMode.(string)
	if !ok {
		return "", false, tuskerError(errorConfigInvalid, "automation.completion_reactor.mode must be disabled, shadow, or authoritative", withPath(layer.Path))
	}
	mode = strings.TrimSpace(mode)
	switch completionReactorMode(mode) {
	case completionReactorModeDisabled, completionReactorModeShadow, completionReactorModeAuthoritative:
		return mode, true, nil
	default:
		return "", false, tuskerError(errorConfigInvalid, "automation.completion_reactor.mode must be disabled, shadow, or authoritative", withPath(layer.Path), withHint("set automation.completion_reactor.mode: disabled until shadow comparison is ready"))
	}
}

func validateCompletionReactorModeLayer(layer tuskerConfigLayer) error {
	_, _, err := completionReactorModeFromLayer(layer)
	return err
}

func resolveCompletionReactorMode(resolved resolvedTuskerConfig, automationEnabled bool) (completionReactorModeProjection, error) {
	for i := len(resolved.Layers) - 1; i >= 0; i-- {
		layer := resolved.Layers[i]
		if !layer.Present {
			continue
		}
		mode, configured, err := completionReactorModeFromLayer(layer)
		if err != nil {
			return completionReactorModeProjection{}, err
		}
		if configured {
			return completionReactorModeProjection{Configured: mode, Effective: mode, Provenance: layer.Name}, nil
		}
	}
	if automationEnabled {
		return completionReactorModeProjection{
			Effective:  string(completionReactorModeLegacy),
			Provenance: "legacy enabled config without completion_reactor.mode",
			Warning:    legacyCompletionReactorModeWarning,
			Repair:     legacyCompletionReactorModeRepair,
		}, nil
	}
	return defaultCompletionReactorMode(), nil
}
