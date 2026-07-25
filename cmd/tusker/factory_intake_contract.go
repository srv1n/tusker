package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const factoryIntakeContractSchema = "tusker.factory-intake-contract/v1"

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

// FactoryIntakeRequest is semantic intent plus facts discovered during
// repository inspection. It deliberately contains no prompt text: language
// understanding happens before routing, and routing must not be keyword based.
type FactoryIntakeRequest struct {
	Intent FactoryIntakeIntent
	Scope  FactoryIntakeScope
}

type FactoryIntakeIntent string

const (
	FactoryIntakeAnalysis           FactoryIntakeIntent = "analysis"
	FactoryIntakeUnattendedDelivery FactoryIntakeIntent = "unattended_delivery"
	FactoryIntakePlanningOrTasking  FactoryIntakeIntent = "planning_or_tasking"
	FactoryIntakeSingletonRecord    FactoryIntakeIntent = "singleton_record"
	FactoryIntakeImplementation     FactoryIntakeIntent = "implementation"
)

type FactoryIntakeScope struct {
	IndependentlyProvableOutcomes int
	Domains                       int
	ConcurrentLanes               bool
	SharedScarceResource          bool
	RolloutOrRecoveryPhase        bool
	ImplementationBranches        int
	ReviewerPackets               int
}

func (s FactoryIntakeScope) multiUnit() bool {
	return s.IndependentlyProvableOutcomes >= 2 || s.Domains >= 2 || s.ConcurrentLanes ||
		s.SharedScarceResource || s.RolloutOrRecoveryPhase || s.ImplementationBranches >= 2 || s.ReviewerPackets >= 2
}

// RouteFactoryIntake applies the canonical decision table after a caller has
// identified the request's intent and inspected its structural scope.
func RouteFactoryIntake(contract factoryIntakeContract, request FactoryIntakeRequest) (factoryIntakeDecision, error) {
	if err := validateFactoryIntakeContract(contract); err != nil {
		return factoryIntakeDecision{}, err
	}
	scope := "singleton"
	if request.Scope.multiUnit() {
		scope = "multi_unit"
	}
	for _, decision := range contract.DecisionTable {
		if decision.Intent != string(request.Intent) {
			continue
		}
		if decision.Scope == "" || decision.Scope == scope {
			return decision, nil
		}
	}
	return factoryIntakeDecision{}, fmt.Errorf("factory intake contract has no route for intent %q and scope %q", request.Intent, scope)
}

func loadFactoryIntakeContract(path string) (factoryIntakeContract, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return factoryIntakeContract{}, fmt.Errorf("read factory intake contract: %w", err)
	}
	var contract factoryIntakeContract
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	decoder.KnownFields(true)
	if err := decoder.Decode(&contract); err != nil {
		return factoryIntakeContract{}, fmt.Errorf("parse factory intake contract: %w", err)
	}
	if err := validateFactoryIntakeContract(contract); err != nil {
		return factoryIntakeContract{}, err
	}
	return contract, nil
}

func factoryIntakeContractPath(repoRoot string) string {
	return filepath.Join(repoRoot, "skills", "tusker", "assets", "factory-intake-contract.yaml")
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
		"analysis_is_read_only", "import_is_inert", "start_does_not_enable_project_automation",
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
