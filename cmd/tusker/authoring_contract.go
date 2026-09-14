package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	skillbundle "tusker/skills/tusker"

	"gopkg.in/yaml.v3"
)

const authoringContractSchema = "tusker.authoring-contract/v1"

// authoringContractFingerprint deliberately binds the one canonical input
// contract, rather than a whole skill tree. Hashing the whole package would
// make the advertised metadata self-referential once it contains this value.
func authoringContractFingerprint(raw []byte) string {
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}

type authoringContractProvenance struct {
	Schema      string `yaml:"schema" json:"schema"`
	Version     string `yaml:"version" json:"version"`
	Fingerprint string `yaml:"fingerprint" json:"fingerprint"`
}

func embeddedAuthoringContractProvenance() (authoringContractProvenance, error) {
	raw, err := skillbundle.GetAsset("authoring-contract.yaml")
	if err != nil {
		return authoringContractProvenance{}, err
	}
	return authoringContractProvenanceFromRaw([]byte(raw))
}

func authoringContractProvenanceFromRaw(raw []byte) (authoringContractProvenance, error) {
	var contract authoringContract
	if err := yaml.Unmarshal(raw, &contract); err != nil {
		return authoringContractProvenance{}, fmt.Errorf("parse embedded authoring contract: %w", err)
	}
	if err := validateAuthoringContract(contract); err != nil {
		return authoringContractProvenance{}, err
	}
	return authoringContractProvenance{Schema: contract.Schema, Version: contract.ContractVersion, Fingerprint: authoringContractFingerprint(raw)}, nil
}

type authoringContract struct {
	Schema                     string                   `yaml:"schema"`
	ContractVersion            string                   `yaml:"contract_version"`
	Title                      string                   `yaml:"title"`
	Description                string                   `yaml:"description"`
	ProductQuestions           []string                 `yaml:"product_questions"`
	AuthoringMechanics         []string                 `yaml:"authoring_mechanics"`
	DecisionTable              []authoringContractRoute `yaml:"decision_table"`
	StructuralMultiUnitSignals []string                 `yaml:"structural_multi_unit_signals"`
	Guardrails                 []string                 `yaml:"guardrails"`
}

type authoringContractRoute struct {
	ID                 string `yaml:"id"`
	Intent             string `yaml:"intent"`
	Scope              string `yaml:"scope"`
	Route              string `yaml:"route"`
	DurableMutation    string `yaml:"durable_mutation"`
	ExecutionAuthority string `yaml:"execution_authority"`
	Remedy             string `yaml:"remedy"`
}

func validateAuthoringContract(contract authoringContract) error {
	if contract.Schema != authoringContractSchema {
		return fmt.Errorf("authoring contract schema must be %q", authoringContractSchema)
	}
	if strings.TrimSpace(contract.ContractVersion) == "" || strings.TrimSpace(contract.Title) == "" || strings.TrimSpace(contract.Description) == "" {
		return fmt.Errorf("authoring contract requires contract_version, title, and description")
	}
	if len(contract.ProductQuestions) == 0 || len(contract.AuthoringMechanics) == 0 || len(contract.StructuralMultiUnitSignals) == 0 || len(contract.Guardrails) == 0 {
		return fmt.Errorf("authoring contract requires product_questions, authoring_mechanics, structural_multi_unit_signals, and guardrails")
	}
	expected := map[string]authoringContractRoute{
		"analysis":                             {ID: "analysis", Intent: "analysis", Route: "read_only_analysis", DurableMutation: "none", ExecutionAuthority: "none"},
		"unattended":                           {ID: "unattended", Intent: "unattended_delivery", Route: "direct_wave_authoring", DurableMutation: "inert_wave_authoring_receipt", ExecutionAuthority: "scoped_wave_start"},
		"planned_or_structural_multi_unit":     {ID: "planned_or_structural_multi_unit", Intent: "planning_or_tasking", Route: "direct_wave_authoring", DurableMutation: "inert_wave_authoring_receipt", ExecutionAuthority: "none"},
		"singleton_record":                     {ID: "singleton_record", Intent: "singleton_record", Scope: "singleton", Route: "direct_task", DurableMutation: "held_or_backlog_singleton", ExecutionAuthority: "none"},
		"bounded_direct_implementation":        {ID: "bounded_direct_implementation", Intent: "implementation", Scope: "singleton", Route: "direct_interactive", DurableMutation: "singleton_contract_only", ExecutionAuthority: "explicit_direct_request"},
		"implementation_with_multi_unit_scope": {ID: "implementation_with_multi_unit_scope", Intent: "implementation", Scope: "multi_unit", Route: "direct_wave_authoring", DurableMutation: "inert_wave_authoring_receipt", ExecutionAuthority: "scoped_wave_start"},
	}
	seen := map[string]bool{}
	for _, decision := range contract.DecisionTable {
		if seen[decision.ID] {
			return fmt.Errorf("authoring contract has duplicate decision %q", decision.ID)
		}
		seen[decision.ID] = true
		expectedDecision, ok := expected[decision.ID]
		if !ok {
			return fmt.Errorf("authoring contract has unknown decision %q", decision.ID)
		}
		expected[decision.ID] = authoringContractRoute{}
		if strings.TrimSpace(decision.Intent) == "" || strings.TrimSpace(decision.Route) == "" || strings.TrimSpace(decision.DurableMutation) == "" || strings.TrimSpace(decision.ExecutionAuthority) == "" || strings.TrimSpace(decision.Remedy) == "" {
			return fmt.Errorf("authoring decision %q requires intent, route, durable_mutation, execution_authority, and remedy", decision.ID)
		}
		if decision.Intent != expectedDecision.Intent || decision.Scope != expectedDecision.Scope || decision.Route != expectedDecision.Route || decision.DurableMutation != expectedDecision.DurableMutation || decision.ExecutionAuthority != expectedDecision.ExecutionAuthority {
			return fmt.Errorf("authoring decision %q does not match the canonical route", decision.ID)
		}
	}
	for id, expectedDecision := range expected {
		if expectedDecision.ID != "" {
			return fmt.Errorf("authoring contract is missing required decision %q", id)
		}
	}
	requiredGuardrails := []string{
		"analysis_is_read_only", "creation_is_inert", "tracked_modifying_work_requires_work_start",
		"dispatched_worker_verifies_existing_claim", "reviewer_submits_typed_result_only",
		"deterministic_handlers_own_close_and_successor_wake",
		"epic_is_never_execution_authority",
		"project_automation_is_separate_explicit_opt_in", "fresh_dispatch_scope_is_authorized_waves",
		"start_does_not_enable_project_automation",
		"start_does_not_start_or_install_daemon", "start_does_not_authorize_release_or_paid_work",
		"start_does_not_satisfy_human_gates", "start_does_not_include_unrelated_work", "start_authorizes_only_the_current_material",
	}
	guardrails := map[string]bool{}
	for _, guardrail := range contract.Guardrails {
		guardrails[guardrail] = true
	}
	for _, guardrail := range requiredGuardrails {
		if !guardrails[guardrail] {
			return fmt.Errorf("authoring contract is missing required guardrail %q", guardrail)
		}
	}
	return nil
}
