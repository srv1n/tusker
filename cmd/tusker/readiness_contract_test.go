package main

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestReadinessContract(t *testing.T) {
	t.Parallel()
	const fixture = `{
  "dimensions": {
    "contract": {"state":"blocked","provenance":{"source":"task","revision":"task-r1"}},
    "import": {"state":"blocked","provenance":{"source":"import","revision":"import-r1"}},
    "interactive": {"state":"waiting","provenance":{"source":"work-session","revision":"work-r1"}},
    "automation": {"state":"blocked","provenance":{"source":"automation","revision":"automation-r1"}},
    "authorization": {"state":"blocked","provenance":{"source":"wave","revision":"wave-r1"}},
    "runtime": {"state":"unavailable","provenance":{"source":"runtime","revision":"run-r1"}},
    "optional_integration": {"state":"blocked","provenance":{"source":"provider","revision":"provider-r1"}}
  },
  "blockers": [
    {"id":"contract","kind":"contract_invalid","authority":"contract","affects":["contract"],"task_id":"APP-T-0001","reason":"Task contract is invalid.","remedy":"Repair the task contract."},
    {"id":"import","kind":"import_missing","authority":"import","affects":["import"],"project_id":"app","reason":"Import is missing.","remedy":"Import the reviewed delivery plan."},
    {"id":"interactive","kind":"interactive_owner","authority":"interactive","affects":["interactive"],"task_id":"APP-T-0001","reason":"A work session owns the task.","remedy":"Wait for the owner to release it."},
    {"id":"automation","kind":"automation_disabled","authority":"automation","affects":["automation"],"project_id":"app","reason":"Automation is disabled.","remedy":"Enable automation through the approved control."},
    {"id":"authorization","kind":"authorization_missing","authority":"authorization","affects":["authorization"],"wave_id":"W-0001","reason":"Wave authorization is absent.","remedy":"Have the authorized operator arm the exact wave."},
    {"id":"runtime","kind":"runtime_unavailable","authority":"runtime","affects":["runtime"],"project_id":"app","reason":"No runtime capacity is available.","remedy":"Wait for capacity to become available."},
    {"id":"optional","kind":"optional_integration_unavailable","authority":"integration","affects":["optional_integration"],"integration_id":"github","reason":"Optional integration is unavailable.","remedy":"Reconnect the optional integration."},
    {"id":"dependency","kind":"dependency_incomplete","authority":"contract","affects":["contract"],"task_id":"APP-T-0001","dependency_task_id":"APP-T-0002","reason":"Dependency APP-T-0002 is incomplete.","remedy":"Complete APP-T-0002."},
    {"id":"gate","kind":"human_gate_open","authority":"human","affects":["interactive"],"task_id":"APP-T-0001","gate_id":"G-0001","reason":"Human approval is pending.","remedy":"Provide the requested approval on G-0001."},
    {"id":"integration","kind":"integration_unavailable","authority":"integration","affects":["optional_integration"],"integration_id":"slack","reason":"Integration is unavailable.","remedy":"Restore the integration connection."}
  ]
}`

	var input ReadinessInput
	if err := json.Unmarshal([]byte(fixture), &input); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	contract, err := NewReadinessContract(input)
	if err != nil {
		t.Fatalf("NewReadinessContract: %v", err)
	}
	if contract.Schema != ReadinessContractSchema || contract.Version != ReadinessContractVersion {
		t.Fatalf("version = %s@%d", contract.Schema, contract.Version)
	}
	if len(contract.Blockers) != 10 || len(contract.ReadinessBlockerIDs(ReadinessDimensionContract)) != 2 {
		t.Fatalf("blocker projection = %#v", contract.Blockers)
	}
	for _, kind := range []ReadinessBlockerKind{
		ReadinessBlockerContractInvalid, ReadinessBlockerImportMissing, ReadinessBlockerInteractiveOwner,
		ReadinessBlockerAutomationDisabled, ReadinessBlockerAuthorizationMissing, ReadinessBlockerRuntimeUnavailable,
		ReadinessBlockerOptionalIntegrationMissing, ReadinessBlockerDependencyIncomplete,
		ReadinessBlockerHumanGateOpen, ReadinessBlockerIntegrationUnavailable,
	} {
		found := false
		for _, blocker := range contract.Blockers {
			found = found || blocker.Kind == kind
		}
		if !found {
			t.Fatalf("fixture omitted blocker kind %q", kind)
		}
	}

	legacy, err := ProjectLegacyReadiness(contract, ReadinessLegacyAdapter{
		ReadinessDimension:        ReadinessDimensionContract,
		ReadinessByState:          map[ReadinessState]string{ReadinessStateBlocked: "blocked_dependency"},
		DispatchabilityDimensions: []ReadinessDimensionKind{ReadinessDimensionAutomation, ReadinessDimensionAuthorization},
		BlockerDimensions:         []ReadinessDimensionKind{ReadinessDimensionContract},
	})
	if err != nil {
		t.Fatalf("ProjectLegacyReadiness: %v", err)
	}
	wantLegacy := ReadinessLegacyProjection{Readiness: "blocked_dependency", Dispatchable: false, Blockers: []string{"Dependency APP-T-0002 is incomplete.", "Task contract is invalid."}}
	if !reflect.DeepEqual(legacy, wantLegacy) {
		t.Fatalf("legacy = %#v, want %#v", legacy, wantLegacy)
	}

	encoded, err := json.Marshal(contract)
	if err != nil {
		t.Fatalf("marshal contract: %v", err)
	}
	if strings.Contains(string(encoded), `"dispatchable"`) || strings.Contains(string(encoded), `"project_ready"`) {
		t.Fatalf("typed contract leaked aggregate readiness: %s", encoded)
	}

	input.Blockers[0].Reason = "mutated input"
	input.Blockers[0].Affects[0] = ReadinessDimensionRuntime
	if contract.Blockers[0].Reason == "mutated input" || contract.Blockers[0].Affects[0] != ReadinessDimensionContract {
		t.Fatal("construction retained mutable input state")
	}

	invalid := input
	invalid.Blockers = append([]ReadinessBlocker(nil), input.Blockers...)
	invalid.Blockers[7].DependencyTaskID = ""
	if _, err := NewReadinessContract(invalid); err == nil || errorToIssue(err).Code != errorReadinessContractInvalid {
		t.Fatalf("dependency identity refusal = %v", err)
	}
	invalid = input
	invalid.Blockers = append([]ReadinessBlocker(nil), input.Blockers...)
	invalid.Blockers[8].Authority = ReadinessAuthorityRuntime
	if _, err := NewReadinessContract(invalid); err == nil || errorToIssue(err).Code != errorReadinessContractInvalid {
		t.Fatalf("human gate authority refusal = %v", err)
	}
}
