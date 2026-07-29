package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCanonicalSkillCompatibilityMatchesFactoryIntakeContract(t *testing.T) {
	root := filepath.Join("..", "..", "skills", "tusker")
	compatibility, err := readSkillCompatibilityContract(root)
	if err != nil {
		t.Fatal(err)
	}
	want, err := embeddedFactoryIntakeContractProvenance()
	if err != nil {
		t.Fatal(err)
	}
	got := compatibility.FactoryIntakeContract
	if got != want {
		t.Fatalf("canonical skill compatibility contract = %#v, want %#v", got, want)
	}
	raw, err := os.ReadFile(filepath.Join(root, "assets", "factory-intake-contract.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if got := factoryIntakeContractFingerprint(raw); got != want.Fingerprint {
		t.Fatalf("contract content fingerprint = %s, want %s", got, want.Fingerprint)
	}
}

func TestFactoryIntakeContractLoadsCanonicalTable(t *testing.T) {
	contract, err := loadFactoryIntakeContract(factoryIntakeContractPath(filepath.Join("..", "..")))
	if err != nil {
		t.Fatal(err)
	}
	if contract.ContractVersion != "1.1.0" {
		t.Fatalf("contract version = %q, want 1.1.0", contract.ContractVersion)
	}
}

func TestFactoryIntakeContractRoutesRepresentativeScenarios(t *testing.T) {
	contract, err := loadFactoryIntakeContract(factoryIntakeContractPath(filepath.Join("..", "..")))
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name      string
		request   FactoryIntakeRequest
		decision  string
		route     string
		authority string
	}{
		{"evaluation only is read-only", FactoryIntakeRequest{Intent: FactoryIntakeAnalysis}, "analysis", "read_only_analysis", "none"},
		{"one small singleton stays lightweight", FactoryIntakeRequest{Intent: FactoryIntakeSingletonRecord}, "singleton_record", "direct_singleton", "none"},
		{"backend and UI are structural multi-unit work", FactoryIntakeRequest{Intent: FactoryIntakeImplementation, Scope: FactoryIntakeScope{Domains: 2}}, "implementation_with_multi_unit_scope", "delivery_plan", "fingerprint_bound_start_delivery"},
		{"explicit tasking makes a held plan without Tusker vocabulary", FactoryIntakeRequest{Intent: FactoryIntakePlanningOrTasking}, "planned_or_structural_multi_unit", "delivery_plan", "none"},
		{"unattended singleton is a one-node delivery DAG", FactoryIntakeRequest{Intent: FactoryIntakeUnattendedDelivery}, "unattended", "delivery_plan", "fingerprint_bound_start_delivery"},
		{"ambiguous product scope remains a product question before routing", FactoryIntakeRequest{Intent: FactoryIntakeAnalysis}, "analysis", "read_only_analysis", "none"},
		{"stale plan cannot gain authority through routing", FactoryIntakeRequest{Intent: FactoryIntakePlanningOrTasking, Scope: FactoryIntakeScope{IndependentlyProvableOutcomes: 2}}, "planned_or_structural_multi_unit", "delivery_plan", "none"},
		{"one-node daemon DAG is explicit unattended intent", FactoryIntakeRequest{Intent: FactoryIntakeUnattendedDelivery, Scope: FactoryIntakeScope{IndependentlyProvableOutcomes: 1}}, "unattended", "delivery_plan", "fingerprint_bound_start_delivery"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decision, err := RouteFactoryIntake(contract, tt.request)
			if err != nil {
				t.Fatal(err)
			}
			if decision.ID != tt.decision || decision.Route != tt.route || decision.ExecutionAuthority != tt.authority {
				t.Fatalf("route = %#v, want decision=%q route=%q authority=%q", decision, tt.decision, tt.route, tt.authority)
			}
		})
	}
}

func TestFactoryIntakeContractStructuralScopeBeatsKeywords(t *testing.T) {
	contract, err := loadFactoryIntakeContract(factoryIntakeContractPath(filepath.Join("..", "..")))
	if err != nil {
		t.Fatal(err)
	}
	decision, err := RouteFactoryIntake(contract, FactoryIntakeRequest{Intent: FactoryIntakeImplementation, Scope: FactoryIntakeScope{ConcurrentLanes: true}})
	if err != nil {
		t.Fatal(err)
	}
	if decision.ID != "implementation_with_multi_unit_scope" {
		t.Fatalf("concurrent implementation route = %q, want multi-unit route", decision.ID)
	}
}

func TestFactoryIntakeContractRejectsIncompleteDecisionTable(t *testing.T) {
	contract, err := loadFactoryIntakeContract(factoryIntakeContractPath(filepath.Join("..", "..")))
	if err != nil {
		t.Fatal(err)
	}
	contract.DecisionTable = contract.DecisionTable[:len(contract.DecisionTable)-1]
	if err := validateFactoryIntakeContract(contract); err == nil {
		t.Fatal("expected incomplete contract to fail validation")
	}
}

func TestFactoryIntakeContractRejectsMissingStartFingerprintGuardrail(t *testing.T) {
	contract, err := loadFactoryIntakeContract(factoryIntakeContractPath(filepath.Join("..", "..")))
	if err != nil {
		t.Fatal(err)
	}
	contract.Guardrails = contract.Guardrails[:len(contract.Guardrails)-1]
	if err := validateFactoryIntakeContract(contract); err == nil {
		t.Fatal("expected missing start fingerprint guardrail to fail validation")
	}
}

func TestFactoryIntakeContractRejectsUnknownFields(t *testing.T) {
	source := factoryIntakeContractPath(filepath.Join("..", ".."))
	raw, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "contract.yaml")
	raw = append(raw, []byte("\nunknown_authority: true\n")...)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadFactoryIntakeContract(path); err == nil {
		t.Fatal("expected unknown contract field to fail strict decoding")
	}
}
