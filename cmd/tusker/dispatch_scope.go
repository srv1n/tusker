package main

import (
	"fmt"
	"strings"
)

// This is a workflow projection, not a second dispatch policy. Every daemon
// implementation and review claim already enters armedWaveDispatchBlocker.
type automationDispatchScope string

const (
	automationDispatchScopeArmedWaves  automationDispatchScope = "armed_waves"
	automationDispatchScopeAllEligible automationDispatchScope = "all_eligible"
)

type automationDispatchScopeProjection struct {
	Configured string `json:"configured,omitempty"`
	Effective  string `json:"effective"`
	Provenance string `json:"provenance"`
	Warning    string `json:"warning,omitempty"`
	Repair     string `json:"repair,omitempty"`
}

const legacyDispatchScopeWarning = "automation.dispatch_scope is absent on an enabled project; preserving legacy all_eligible authority"
const legacyDispatchScopeRepair = "set automation.dispatch_scope: all_eligible to acknowledge legacy broad dispatch, or armed_waves to require an armed wave"

func defaultAutomationDispatchScope() automationDispatchScopeProjection {
	return automationDispatchScopeProjection{Configured: string(automationDispatchScopeArmedWaves), Effective: string(automationDispatchScopeArmedWaves), Provenance: "fresh default"}
}

func (p automationDispatchScopeProjection) isArmedWaves() bool {
	return p.Effective == string(automationDispatchScopeArmedWaves)
}

func (p automationDispatchScopeProjection) preservesLegacyWaveConstraints() bool {
	return p.Configured == "" &&
		p.Effective == string(automationDispatchScopeAllEligible) &&
		p.Warning == legacyDispatchScopeWarning
}

func resolveAutomationDispatchScope(resolved resolvedTuskerConfig, automationEnabled bool) (automationDispatchScopeProjection, error) {
	for i := len(resolved.Layers) - 1; i >= 0; i-- {
		layer := resolved.Layers[i]
		if !layer.Present {
			continue
		}
		automation, ok := layer.Raw["automation"].(map[string]any)
		if !ok {
			continue
		}
		raw, ok := automation["dispatch_scope"]
		if !ok {
			continue
		}
		scope := automationDispatchScope(strings.TrimSpace(fmt.Sprint(raw)))
		switch scope {
		case automationDispatchScopeArmedWaves, automationDispatchScopeAllEligible:
			return automationDispatchScopeProjection{Configured: string(scope), Effective: string(scope), Provenance: layer.Name}, nil
		default:
			return automationDispatchScopeProjection{}, tuskerError(errorConfigInvalid, "automation.dispatch_scope must be armed_waves or all_eligible", withPath(layer.Path), withHint("set automation.dispatch_scope: armed_waves for wave-scoped dispatch"))
		}
	}
	if automationEnabled {
		return automationDispatchScopeProjection{Effective: string(automationDispatchScopeAllEligible), Provenance: "legacy enabled config without dispatch_scope", Warning: legacyDispatchScopeWarning, Repair: legacyDispatchScopeRepair}, nil
	}
	return defaultAutomationDispatchScope(), nil
}

func automationDispatchScopeBlocker(vaultPath string, task Note, wf Workflow, runs map[string]RunStatus) string {
	if automationDispatchScopeContinuation(task, runs) {
		// Scope is an admission rule for a fresh piece of implementation work.
		// A retry, external apply handoff, or review continuation already has a
		// durable run/attempt provenance; re-evaluating it as a fresh task would
		// strand it merely because a project later selected armed_waves.
		return ""
	}
	if wf.DispatchScope.preservesLegacyWaveConstraints() {
		// Before dispatch_scope existed, tasks outside a wave were eligible while
		// tasks that named a wave still had to satisfy that wave's authorization.
		// Keep that exact hybrid behavior for enabled legacy projects so adding
		// the projection cannot silently widen their daemon authority.
		return armedWaveDispatchBlockerForArmedScope(vaultPath, task, wf, runs)
	}
	if !wf.DispatchScope.isArmedWaves() {
		return ""
	}
	if strings.TrimSpace(stringField(task.Data, "wave")) == "" {
		return "dispatch scope armed_waves requires task membership in a currently armed wave"
	}
	return armedWaveDispatchBlockerForArmedScope(vaultPath, task, wf, runs)
}

func automationDispatchScopeContinuation(task Note, runs map[string]RunStatus) bool {
	if len(runs) == 0 {
		return false
	}
	run, ok := runs[stringField(task.Data, "id")]
	if !ok {
		return false
	}
	switch LeaseState(strings.TrimSpace(run.LeaseState)) {
	case LeaseStateRetryQueued, LeaseStateClaimed, LeaseStateRunning:
		return true
	}
	return run.AttemptCount > 0 && strings.TrimSpace(run.Lane) != ""
}
