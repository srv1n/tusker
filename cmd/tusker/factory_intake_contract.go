package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	skillbundle "tusker/skills/tusker"

	"gopkg.in/yaml.v3"
)

const factoryIntakeContractSchema = "tusker.factory-intake-contract/v1"

// factoryIntakeContractFingerprint deliberately binds the one canonical input
// contract, rather than a whole skill tree. Hashing the whole package would
// make the advertised metadata self-referential once it contains this value.
func factoryIntakeContractFingerprint(raw []byte) string {
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}

type factoryIntakeContractProvenance struct {
	Schema      string `yaml:"schema" json:"schema"`
	Version     string `yaml:"version" json:"version"`
	Fingerprint string `yaml:"fingerprint" json:"fingerprint"`
}

func embeddedFactoryIntakeContractProvenance() (factoryIntakeContractProvenance, error) {
	raw, err := skillbundle.GetAsset("factory-intake-contract.yaml")
	if err != nil {
		return factoryIntakeContractProvenance{}, err
	}
	return factoryIntakeContractProvenanceFromRaw([]byte(raw))
}

func factoryIntakeContractProvenanceFromRaw(raw []byte) (factoryIntakeContractProvenance, error) {
	var contract factoryIntakeContract
	if err := yaml.Unmarshal(raw, &contract); err != nil {
		return factoryIntakeContractProvenance{}, fmt.Errorf("parse embedded factory intake contract: %w", err)
	}
	if err := validateFactoryIntakeContract(contract); err != nil {
		return factoryIntakeContractProvenance{}, err
	}
	return factoryIntakeContractProvenance{Schema: contract.Schema, Version: contract.ContractVersion, Fingerprint: factoryIntakeContractFingerprint(raw)}, nil
}

type factoryIntakeContract struct {
	Schema                     string                  `yaml:"schema"`
	ContractVersion            string                  `yaml:"contract_version"`
	Title                      string                  `yaml:"title"`
	Description                string                  `yaml:"description"`
	ProductQuestions           []string                `yaml:"product_questions"`
	FactoryMechanics           []string                `yaml:"factory_mechanics"`
	DecisionTable              []factoryIntakeDecision `yaml:"decision_table"`
	StructuralMultiUnitSignals []string                `yaml:"structural_multi_unit_signals"`
	Guardrails                 []string                `yaml:"guardrails"`
}

type factoryIntakeDecision struct {
	ID                 string `yaml:"id"`
	Intent             string `yaml:"intent"`
	Scope              string `yaml:"scope"`
	Route              string `yaml:"route"`
	DurableMutation    string `yaml:"durable_mutation"`
	ExecutionAuthority string `yaml:"execution_authority"`
	Remedy             string `yaml:"remedy"`
}

func validateFactoryIntakeContract(contract factoryIntakeContract) error {
	if contract.Schema != factoryIntakeContractSchema {
		return fmt.Errorf("factory intake contract schema must be %q", factoryIntakeContractSchema)
	}
	if strings.TrimSpace(contract.ContractVersion) == "" || strings.TrimSpace(contract.Title) == "" || strings.TrimSpace(contract.Description) == "" {
		return fmt.Errorf("factory intake contract requires contract_version, title, and description")
	}
	if len(contract.ProductQuestions) == 0 || len(contract.FactoryMechanics) == 0 || len(contract.StructuralMultiUnitSignals) == 0 || len(contract.Guardrails) == 0 {
		return fmt.Errorf("factory intake contract requires product_questions, factory_mechanics, structural_multi_unit_signals, and guardrails")
	}
	expected := map[string]factoryIntakeDecision{
		"analysis":                             {ID: "analysis", Intent: "analysis", Route: "read_only_analysis", DurableMutation: "none", ExecutionAuthority: "none"},
		"unattended":                           {ID: "unattended", Intent: "unattended_delivery", Route: "delivery_plan", DurableMutation: "versioned_plan_and_held_import", ExecutionAuthority: "fingerprint_bound_start_delivery"},
		"planned_or_structural_multi_unit":     {ID: "planned_or_structural_multi_unit", Intent: "planning_or_tasking", Route: "delivery_plan", DurableMutation: "versioned_plan_and_held_import", ExecutionAuthority: "none"},
		"singleton_record":                     {ID: "singleton_record", Intent: "singleton_record", Scope: "singleton", Route: "direct_singleton", DurableMutation: "held_or_backlog_singleton", ExecutionAuthority: "none"},
		"bounded_direct_implementation":        {ID: "bounded_direct_implementation", Intent: "implementation", Scope: "singleton", Route: "direct_interactive", DurableMutation: "singleton_contract_only", ExecutionAuthority: "explicit_direct_request"},
		"implementation_with_multi_unit_scope": {ID: "implementation_with_multi_unit_scope", Intent: "implementation", Scope: "multi_unit", Route: "delivery_plan", DurableMutation: "versioned_plan_and_held_import", ExecutionAuthority: "fingerprint_bound_start_delivery"},
	}
	seen := map[string]bool{}
	for _, decision := range contract.DecisionTable {
		if seen[decision.ID] {
			return fmt.Errorf("factory intake contract has duplicate decision %q", decision.ID)
		}
		seen[decision.ID] = true
		expectedDecision, ok := expected[decision.ID]
		if !ok {
			return fmt.Errorf("factory intake contract has unknown decision %q", decision.ID)
		}
		expected[decision.ID] = factoryIntakeDecision{}
		if strings.TrimSpace(decision.Intent) == "" || strings.TrimSpace(decision.Route) == "" || strings.TrimSpace(decision.DurableMutation) == "" || strings.TrimSpace(decision.ExecutionAuthority) == "" || strings.TrimSpace(decision.Remedy) == "" {
			return fmt.Errorf("factory intake decision %q requires intent, route, durable_mutation, execution_authority, and remedy", decision.ID)
		}
		if decision.Intent != expectedDecision.Intent || decision.Scope != expectedDecision.Scope || decision.Route != expectedDecision.Route || decision.DurableMutation != expectedDecision.DurableMutation || decision.ExecutionAuthority != expectedDecision.ExecutionAuthority {
			return fmt.Errorf("factory intake decision %q does not match the canonical route", decision.ID)
		}
	}
	for id, expectedDecision := range expected {
		if expectedDecision.ID != "" {
			return fmt.Errorf("factory intake contract is missing required decision %q", id)
		}
	}
	requiredGuardrails := []string{
		"analysis_is_read_only", "import_is_inert", "tracked_modifying_work_requires_work_start",
		"dispatched_worker_verifies_existing_claim", "reviewer_submits_typed_result_only",
		"deterministic_handlers_own_close_and_successor_wake",
		"epic_is_never_execution_authority",
		"project_automation_is_separate_explicit_opt_in", "fresh_dispatch_scope_is_armed_waves",
		"start_does_not_enable_project_automation",
		"start_does_not_start_or_install_daemon", "start_does_not_authorize_release_or_paid_work",
		"start_does_not_satisfy_human_gates", "start_does_not_include_unrelated_work", "start_requires_current_plan_fingerprint",
	}
	guardrails := map[string]bool{}
	for _, guardrail := range contract.Guardrails {
		guardrails[guardrail] = true
	}
	for _, guardrail := range requiredGuardrails {
		if !guardrails[guardrail] {
			return fmt.Errorf("factory intake contract is missing required guardrail %q", guardrail)
		}
	}
	return nil
}
