package main

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

func selfServiceDimensionsFixture() ReadinessDimensions {
	provenance := func(source, revision string) ReadinessDimension {
		return ReadinessDimension{State: ReadinessStateReady, Provenance: ReadinessProvenance{Source: source, Revision: revision}}
	}
	return ReadinessDimensions{
		Contract:            provenance("task", "task-r1"),
		Import:              provenance("import", "import-r1"),
		Interactive:         provenance("work-session", "work-r1"),
		Automation:          provenance("automation", "automation-r1"),
		Authorization:       provenance("wave", "wave-r1"),
		Runtime:             provenance("runtime", "run-r1"),
		OptionalIntegration: provenance("provider", "provider-r1"),
	}
}

func selfServiceEvidence(source, revision string) DiagnosticEvidence {
	return DiagnosticEvidence{
		Source:     source,
		Revision:   revision,
		ObservedAt: "2026-09-22T10:00:00Z",
		Detail:     "Observed during authoring review.",
	}
}

func TestSelfServiceDiagnosticContract(t *testing.T) {
	t.Parallel()

	t.Run("A1 serialization preserves dimensions and rejects malformed input", func(t *testing.T) {
		t.Parallel()
		dimensions := selfServiceDimensionsFixture()
		input := DiagnosisInput{
			Dimensions:      dimensions,
			SourceRevisions: map[string]string{"task": "task-r1", "runtime": "run-r1", "wave": "wave-r1"},
			Findings: []DiagnosticFinding{
				{
					Code:           "dependency-incomplete",
					Scope:          DiagnosticScope{Project: "tusker", Record: "TSK-T-0037", Wave: "W-0035"},
					Classification: DiagnosticNormalWait,
					Affects:        []string{"TSK-T-0037"},
					NextActor:      DiagnosticAuthorityOperator,
					RetryAfter:     "5m",
					Evidence:       selfServiceEvidence("task", "task-r1"),
				},
				{
					Code:           "stale-authorization",
					Scope:          DiagnosticScope{Project: "tusker", Wave: "W-0035"},
					Classification: DiagnosticRecoverableFault,
					Affects:        []string{"TSK-T-0036", "TSK-T-0037"},
					NextActor:      DiagnosticAuthorityHuman,
					Evidence:       selfServiceEvidence("wave", "wave-r1"),
					Action: &RecoveryAction{
						ID:                "reconcile-authorization",
						Type:              RecoveryActionWaveStart,
						Argv:              []string{"tusker", "wave", "start", "W-0035", "--mode", "background", "--by", "human:sarav"},
						Preconditions:     []string{"wave material matches the stored authorization fingerprint"},
						RequiredAuthority: DiagnosticAuthorityHuman,
						ExpectedEffect:    "Wave authorization is armed for the exact current material.",
						Postcondition:     "tusker wave review W-0035 --check reports Start enabled.",
					},
				},
				{
					Code:           "gate-open",
					Scope:          DiagnosticScope{Project: "tusker", Record: "TSK-T-0001"},
					Classification: DiagnosticHumanDecision,
					Affects:        []string{"TSK-T-0001"},
					NextActor:      DiagnosticAuthorityHuman,
					Evidence:       selfServiceEvidence("task", "task-r1"),
					Action: &RecoveryAction{
						ID:                "confirm-gate",
						Type:              RecoveryActionGateSatisfy,
						Argv:              []string{"tusker", "gate", "satisfy", "G-0001"},
						RequiredAuthority: DiagnosticAuthorityHuman,
						ExpectedEffect:    "Human gate G-0001 is satisfied.",
						Postcondition:     "Gate G-0001 reports satisfied in the task contract.",
					},
				},
				{
					Code:           "healthy-member",
					Scope:          DiagnosticScope{Project: "tusker", Record: "TSK-T-0036"},
					Classification: DiagnosticHealthy,
					NextActor:      DiagnosticAuthorityNone,
					Evidence:       selfServiceEvidence("task", "task-r1"),
				},
				{
					Code:           "completed-member",
					Scope:          DiagnosticScope{Project: "tusker", Record: "TSK-T-0035"},
					Classification: DiagnosticComplete,
					NextActor:      DiagnosticAuthorityNone,
					Evidence:       selfServiceEvidence("task", "task-r1"),
				},
				{
					Code:           "corrupt-projection",
					Scope:          DiagnosticScope{Project: "tusker", Wave: "W-0035"},
					Classification: DiagnosticProductDefect,
					Affects:        []string{"TSK-T-0038"},
					NextActor:      DiagnosticAuthorityOperator,
					Evidence:       selfServiceEvidence("runtime", "run-r1"),
					Action: &RecoveryAction{
						ID:                "report-defect",
						Type:              RecoveryActionNoSupportedFix,
						RequiredAuthority: DiagnosticAuthorityOperator,
						ExpectedEffect:    "Defect is reported with bounded evidence.",
						Postcondition:     "A product-defect report exists with the diagnosis attached.",
					},
				},
				{
					Code:           "runtime-unreachable",
					Scope:          DiagnosticScope{Project: "tusker"},
					Classification: DiagnosticUnavailable,
					NextActor:      DiagnosticAuthorityOperator,
					Evidence:       DiagnosticEvidence{Source: "runtime", Revision: "run-r1", ObservedAt: "2026-09-22T10:00:00Z", Detail: "Runtime store did not answer."},
				},
			},
		}
		diagnosis, err := NewDiagnosis(input)
		if err != nil {
			t.Fatalf("NewDiagnosis: %v", err)
		}
		if !reflect.DeepEqual(diagnosis.Dimensions, dimensions) {
			t.Fatalf("diagnosis did not preserve readiness dimensions: %#v", diagnosis.Dimensions)
		}
		encoded, err := json.Marshal(diagnosis)
		if err != nil {
			t.Fatalf("marshal diagnosis: %v", err)
		}
		var decoded Diagnosis
		if err := json.Unmarshal(encoded, &decoded); err != nil {
			t.Fatalf("unmarshal diagnosis: %v", err)
		}
		if !reflect.DeepEqual(decoded.Dimensions, dimensions) {
			t.Fatalf("round trip lost readiness dimensions: %#v", decoded.Dimensions)
		}
		if got := decoded.FindingCodes(); len(got) != 7 {
			t.Fatalf("round trip lost findings: %v", got)
		}
		if decoded.PrimaryCode != "stale-authorization" || decoded.PrimaryClassification != DiagnosticRecoverableFault {
			t.Fatalf("primary = %s/%s, want stale-authorization/recoverable_fault", decoded.PrimaryCode, decoded.PrimaryClassification)
		}

		malformed := input
		malformed.Findings = append([]DiagnosticFinding(nil), input.Findings...)
		broken := malformed.Findings[0]
		broken.Scope = DiagnosticScope{}
		malformed.Findings[0] = broken
		if _, err := NewDiagnosis(malformed); err == nil {
			t.Fatal("malformed scope was accepted")
		}

		unknownAction := input
		unknownAction.Findings = append([]DiagnosticFinding(nil), input.Findings...)
		broken = unknownAction.Findings[1]
		action := *broken.Action
		action.Type = RecoveryActionType("run_shell")
		broken.Action = &action
		unknownAction.Findings[1] = broken
		if _, err := NewDiagnosis(unknownAction); err == nil {
			t.Fatal("unknown recovery action type was accepted")
		}

		revisionMismatch := input
		revisionMismatch.Findings = append([]DiagnosticFinding(nil), input.Findings...)
		broken = revisionMismatch.Findings[0]
		broken.Evidence.Revision = "task-r2"
		revisionMismatch.Findings[0] = broken
		if _, err := NewDiagnosis(revisionMismatch); err == nil {
			t.Fatal("mismatched evidence revision was accepted")
		}
	})

	t.Run("A2 dependency capacity and authorization findings elect one cause", func(t *testing.T) {
		t.Parallel()
		input := DiagnosisInput{
			Dimensions: selfServiceDimensionsFixture(),
			Findings: []DiagnosticFinding{
				{
					Code: "authorization-missing", Scope: DiagnosticScope{Project: "tusker", Wave: "W-0035"},
					Classification: DiagnosticRecoverableFault, Affects: []string{"TSK-T-0036"},
					NextActor: DiagnosticAuthorityHuman, Evidence: selfServiceEvidence("wave", "wave-r1"),
					Action: &RecoveryAction{
						ID: "arm-wave", Type: RecoveryActionWaveStart,
						Argv:              []string{"tusker", "wave", "start", "W-0035", "--mode", "background", "--by", "human:sarav"},
						RequiredAuthority: DiagnosticAuthorityHuman, ExpectedEffect: "Wave is armed.",
						Postcondition: "Wave W-0035 reports armed authorization.",
					},
				},
				{
					Code: "capacity-reached", Scope: DiagnosticScope{Project: "tusker"},
					Classification: DiagnosticNormalWait, Causes: []string{"authorization-missing"},
					Affects:   []string{"TSK-T-0036", "TSK-T-0037"},
					NextActor: DiagnosticAuthorityDaemon, RetryAfter: "1m",
					Evidence: selfServiceEvidence("runtime", "run-r1"),
				},
				{
					Code: "dependency-waiting", Scope: DiagnosticScope{Project: "tusker", Record: "TSK-T-0037"},
					Classification: DiagnosticNormalWait, Affects: []string{"TSK-T-0037"},
					NextActor: DiagnosticAuthorityOperator, Evidence: selfServiceEvidence("task", "task-r1"),
				},
			},
		}
		first, err := NewDiagnosis(input)
		if err != nil {
			t.Fatalf("NewDiagnosis: %v", err)
		}
		// Reorder the findings: the primary cause must not depend on input order.
		reordered := input
		reordered.Findings = []DiagnosticFinding{input.Findings[2], input.Findings[0], input.Findings[1]}
		second, err := NewDiagnosis(reordered)
		if err != nil {
			t.Fatalf("NewDiagnosis reordered: %v", err)
		}
		if first.PrimaryCode != "authorization-missing" || second.PrimaryCode != "authorization-missing" {
			t.Fatalf("primary not deterministic: %s vs %s", first.PrimaryCode, second.PrimaryCode)
		}
		encoded, err := json.Marshal(first)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		var decoded Diagnosis
		if err := json.Unmarshal(encoded, &decoded); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if len(decoded.Findings) != 3 {
			t.Fatalf("JSON dropped independent blockers: %v", decoded.FindingCodes())
		}
		affected := map[string]bool{}
		for _, finding := range decoded.Findings {
			for _, id := range finding.Affects {
				affected[id] = true
			}
		}
		if !affected["TSK-T-0036"] || !affected["TSK-T-0037"] {
			t.Fatalf("JSON lost affected tasks: %v", affected)
		}

		cyclic := input
		cyclic.Findings = append([]DiagnosticFinding(nil), input.Findings...)
		loop := cyclic.Findings[0]
		loop.Causes = []string{"capacity-reached"}
		cyclic.Findings[0] = loop
		if _, err := NewDiagnosis(cyclic); err == nil {
			t.Fatal("causal cycle was accepted")
		}
		unknownCause := input
		unknownCause.Findings = append([]DiagnosticFinding(nil), input.Findings...)
		dangling := unknownCause.Findings[2]
		dangling.Causes = []string{"missing-finding"}
		unknownCause.Findings[2] = dangling
		if _, err := NewDiagnosis(unknownCause); err == nil {
			t.Fatal("unknown cause was accepted")
		}
	})

	t.Run("A3 unavailable input disables unsafe actions without side effects", func(t *testing.T) {
		t.Parallel()
		dimensions := selfServiceDimensionsFixture()
		before, err := json.Marshal(DiagnosisInput{
			Dimensions: dimensions,
			Findings: []DiagnosticFinding{{
				Code: "stale-runtime", Scope: DiagnosticScope{Project: "tusker"},
				Classification: DiagnosticNormalWait, NextActor: DiagnosticAuthorityDaemon,
				Evidence: DiagnosticEvidence{Source: "runtime", Revision: "run-r0", ObservedAt: "2026-09-22T10:00:00Z", Detail: "Runtime facts are stale.", Stale: true},
			}},
		})
		if err != nil {
			t.Fatalf("marshal input: %v", err)
		}
		var input DiagnosisInput
		if err := json.Unmarshal(before, &input); err != nil {
			t.Fatalf("unmarshal input: %v", err)
		}
		diagnosis, err := NewDiagnosis(input)
		if err != nil {
			t.Fatalf("NewDiagnosis: %v", err)
		}
		if diagnosis.PrimaryClassification != DiagnosticUnavailable || diagnosis.Findings[0].Classification != DiagnosticUnavailable || !diagnosis.Findings[0].Evidence.Stale {
			t.Fatalf("stale input was not classified unavailable: %#v", diagnosis)
		}
		if got := diagnosis.SafeActions(); len(got) != 0 {
			t.Fatalf("stale evidence produced safe actions: %#v", got)
		}
		unknown := input
		unknown.Findings = append([]DiagnosticFinding(nil), input.Findings...)
		unknown.Findings[0].Classification = DiagnosticClassification("unknown")
		if _, err := NewDiagnosis(unknown); err == nil {
			t.Fatal("stale evidence hid an unknown classification")
		}
		after, err := json.Marshal(input)
		if err != nil {
			t.Fatalf("marshal input after: %v", err)
		}
		if string(before) != string(after) {
			t.Fatal("diagnosis mutated its read-only input")
		}

		unavailable, err := DiagnoseUnavailable(dimensions, DiagnosticScope{Project: "tusker"}, "runtime", "Runtime store did not answer.")
		if err != nil {
			t.Fatalf("DiagnoseUnavailable: %v", err)
		}
		if unavailable.PrimaryClassification != DiagnosticUnavailable || len(unavailable.SafeActions()) != 0 {
			t.Fatalf("unavailable diagnosis is not action-free: %#v", unavailable)
		}

		armed := DiagnosisInput{
			Dimensions: dimensions,
			Findings: []DiagnosticFinding{{
				Code: "stale-with-action", Scope: DiagnosticScope{Project: "tusker"},
				Classification: DiagnosticRecoverableFault, NextActor: DiagnosticAuthorityOperator,
				Evidence: DiagnosticEvidence{Source: "runtime", Revision: "run-r0", ObservedAt: "2026-09-22T10:00:00Z", Stale: true},
				Action: &RecoveryAction{
					ID: "retry", Type: RecoveryActionRunRetry, Argv: []string{"tusker", "runs", "retry"},
					RequiredAuthority: DiagnosticAuthorityOperator, ExpectedEffect: "Run is retried.",
					Postcondition: "Run reports a fresh attempt.",
				},
			}},
		}
		if _, err := NewDiagnosis(armed); err == nil {
			t.Fatal("action on stale evidence was accepted")
		}
	})

	t.Run("A4 recovery descriptors are typed and existing callers keep behavior", func(t *testing.T) {
		t.Parallel()
		diagnosis, err := NewDiagnosis(DiagnosisInput{
			Dimensions: selfServiceDimensionsFixture(),
			Findings: []DiagnosticFinding{{
				Code: "paused-wave", Scope: DiagnosticScope{Project: "tusker", Wave: "W-0035"},
				Classification: DiagnosticRecoverableFault, NextActor: DiagnosticAuthorityHuman,
				Evidence: selfServiceEvidence("wave", "wave-r1"),
				Action: &RecoveryAction{
					ID: "resume-wave", Type: RecoveryActionWaveResume,
					Argv:              []string{"tusker", "wave", "resume", "W-0035", "--by", "human:sarav"},
					Preconditions:     []string{"wave material is unchanged"},
					RequiredAuthority: DiagnosticAuthorityHuman, ExpectedEffect: "Wave resumes.",
					Postcondition: "Wave W-0035 reports armed authorization.",
				},
			}},
		})
		if err != nil {
			t.Fatalf("NewDiagnosis: %v", err)
		}
		actions := diagnosis.SafeActions()
		if len(actions) != 1 {
			t.Fatalf("safe actions = %#v", actions)
		}
		action := actions[0]
		if len(action.Argv) == 0 || action.RequiredAuthority == "" || action.Postcondition == "" || action.ExpectedEffect == "" {
			t.Fatalf("recovery descriptor is missing typed fields: %#v", action)
		}
		encoded, err := json.Marshal(diagnosis)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		for _, forbidden := range []string{`"shell"`, `"script"`, `"command_string"`, `"remedy_text"`, `"remedy"`} {
			if strings.Contains(string(encoded), forbidden) {
				t.Fatalf("diagnosis embeds executable remedy protocol %s: %s", forbidden, encoded)
			}
		}
		shellAction := action
		shellAction.ID = "shell-escape"
		shellAction.Argv = []string{"sh", "-c", "rm -rf /tmp/x; echo done"}
		shellInput := DiagnosisInput{
			Dimensions: selfServiceDimensionsFixture(),
			Findings: []DiagnosticFinding{{
				Code: "shell-fault", Scope: DiagnosticScope{Project: "tusker"},
				Classification: DiagnosticRecoverableFault, NextActor: DiagnosticAuthorityOperator,
				Evidence: selfServiceEvidence("runtime", "run-r1"), Action: &shellAction,
			}},
		}
		if _, err := NewDiagnosis(shellInput); err == nil {
			t.Fatal("embedded shell in argv was accepted")
		}

		// Existing readiness and legacy-adapter callers retain their behavior.
		legacyDimensions := selfServiceDimensionsFixture()
		legacyDimensions.Authorization = ReadinessDimension{State: ReadinessStateBlocked, Provenance: ReadinessProvenance{Source: "wave", Revision: "wave-r1"}}
		contract, err := NewReadinessContract(ReadinessInput{
			Dimensions: legacyDimensions,
			Blockers: []ReadinessBlocker{
				{
					ID: "authorization", Kind: ReadinessBlockerAuthorizationMissing, Authority: ReadinessAuthorityAuthorization,
					Affects: []ReadinessDimensionKind{ReadinessDimensionAuthorization}, WaveID: "W-0035",
					Reason: "Wave authorization is absent.", Remedy: "Have the authorized operator arm the exact wave.",
				},
			},
		})
		if err != nil {
			t.Fatalf("NewReadinessContract: %v", err)
		}
		legacy, err := ProjectLegacyReadiness(contract, ReadinessLegacyAdapter{
			ReadinessDimension:        ReadinessDimensionAuthorization,
			DispatchabilityDimensions: []ReadinessDimensionKind{ReadinessDimensionAuthorization},
			BlockerDimensions:         []ReadinessDimensionKind{ReadinessDimensionAuthorization},
		})
		if err != nil {
			t.Fatalf("ProjectLegacyReadiness: %v", err)
		}
		if legacy.Dispatchable || legacy.Readiness != string(ReadinessStateBlocked) {
			t.Fatalf("legacy projection changed: %#v", legacy)
		}
		if len(legacy.Blockers) != 1 || !strings.Contains(legacy.Blockers[0], "Wave authorization is absent.") {
			t.Fatalf("legacy blockers changed: %#v", legacy.Blockers)
		}
		_ = time.Now
	})
}
