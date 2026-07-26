package main

import (
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// deliveryProofContractSchema is the immutable, source-derived authority for a
// strict V2 task. Results live in a separate projection so a changed note or
// result cannot rewrite what was required.
const deliveryProofContractSchema = "tusker.delivery-proof-contract/v1"
const deliveryProofResultsSchema = "tusker.delivery-proof-results/v1"
const deliveryStrictImportLineageSchema = "tusker.delivery-strict-import-lineage/v1"

type deliveryStrictAuthorityState string

const (
	deliveryStrictAuthorityLegacy  deliveryStrictAuthorityState = "legacy"
	deliveryStrictAuthorityCurrent deliveryStrictAuthorityState = "strict_current"
	deliveryStrictAuthorityCorrupt deliveryStrictAuthorityState = "strict_corrupt_or_missing"
)

type deliveryStrictImportLineage struct {
	Schema                  string   `yaml:"schema"`
	Scope                   string   `yaml:"scope"`
	SourceKey               string   `yaml:"source_key"`
	DeliveryTaskFingerprint string   `yaml:"delivery_task_fingerprint"`
	RequiredCapabilities    []string `yaml:"required_capabilities"`
	Fingerprint             string   `yaml:"fingerprint"`
}

type deliveryStrictWaveLineage struct {
	Schema      string                                 `yaml:"schema"`
	Tasks       map[string]deliveryStrictImportLineage `yaml:"tasks"`
	Fingerprint string                                 `yaml:"fingerprint"`
}

type deliveryCanonicalAcceptance struct {
	ID      string `yaml:"id"`
	Outcome string `yaml:"outcome"`
}

type deliveryCanonicalVerification struct {
	Covers              []string `yaml:"covers"`
	Type                string   `yaml:"type"`
	Text                string   `yaml:"text"`
	ManualGateSourceKey string   `yaml:"manual_gate_source_key,omitempty"`
}

type deliveryProofContract struct {
	Schema                  string                          `yaml:"schema"`
	Scope                   string                          `yaml:"scope"`
	SourceKey               string                          `yaml:"source_key"`
	DeliveryTaskFingerprint string                          `yaml:"delivery_task_fingerprint"`
	Acceptance              []deliveryCanonicalAcceptance   `yaml:"acceptance"`
	Verification            []deliveryCanonicalVerification `yaml:"verification"`
	Fingerprint             string                          `yaml:"fingerprint"`
}

type deliveryProofResultRow struct {
	VerificationFingerprint string `yaml:"verification_fingerprint"`
	Result                  string `yaml:"result"`
	Notes                   string `yaml:"notes,omitempty"`
}

type deliveryProofResultsProjection struct {
	Schema              string                   `yaml:"schema"`
	ContractFingerprint string                   `yaml:"contract_fingerprint"`
	Rows                []deliveryProofResultRow `yaml:"rows"`
}

func deliveryPlanRequiresStrictProofAuthority(plan deliveryPlan) bool {
	if plan.v2 == nil {
		return false
	}
	for _, capability := range plan.v2.RequiredCapabilities {
		if capability == strictV2ProofAuthorityCapability {
			return true
		}
	}
	return false
}

func deliveryStrictLineageFor(plan deliveryPlanV2, task deliveryPlanTask) (deliveryStrictImportLineage, error) {
	lineage := deliveryStrictImportLineage{Schema: deliveryStrictImportLineageSchema, Scope: strings.TrimSpace(plan.Scope), SourceKey: strings.TrimSpace(task.SourceKey), DeliveryTaskFingerprint: deliveryV2TaskFingerprint(task, plan.HumanGates), RequiredCapabilities: append([]string(nil), plan.RequiredCapabilities...)}
	if lineage.Scope == "" || lineage.SourceKey == "" || lineage.DeliveryTaskFingerprint == "" {
		return deliveryStrictImportLineage{}, fmt.Errorf("strict import lineage is missing source identity")
	}
	lineage.Fingerprint = ""
	raw, err := yaml.Marshal(lineage)
	if err != nil {
		return deliveryStrictImportLineage{}, err
	}
	lineage.Fingerprint = deliveryFingerprint(raw)
	return lineage, nil
}

func deliveryStrictWaveLineageFor(plan deliveryPlan, report deliveryImportReport) (deliveryStrictWaveLineage, error) {
	lineage := deliveryStrictWaveLineage{Schema: deliveryStrictImportLineageSchema, Tasks: map[string]deliveryStrictImportLineage{}}
	for _, task := range plan.Tasks {
		entry, err := deliveryStrictLineageFor(*plan.v2, task)
		if err != nil {
			return deliveryStrictWaveLineage{}, err
		}
		lineage.Tasks[report.TaskMapping[task.SourceKey]] = entry
	}
	lineage.Fingerprint = ""
	raw, err := yaml.Marshal(lineage)
	if err != nil {
		return deliveryStrictWaveLineage{}, err
	}
	lineage.Fingerprint = deliveryFingerprint(raw)
	return lineage, nil
}

func deliveryCanonicalProofContract(plan deliveryPlanV2, task deliveryPlanTask) (deliveryProofContract, error) {
	if strings.TrimSpace(plan.Scope) == "" || strings.TrimSpace(task.SourceKey) == "" {
		return deliveryProofContract{}, fmt.Errorf("strict proof contract requires non-empty scope and source_key")
	}
	acceptance := make([]deliveryCanonicalAcceptance, 0, len(task.Acceptance))
	declared := map[string]bool{}
	for _, row := range task.Acceptance {
		id := strings.ToUpper(strings.TrimSpace(row.ID))
		outcome := strings.TrimSpace(row.Outcome)
		if id == "" || outcome == "" || declared[id] {
			return deliveryProofContract{}, fmt.Errorf("strict proof contract has invalid acceptance row %q", row.ID)
		}
		declared[id] = true
		acceptance = append(acceptance, deliveryCanonicalAcceptance{ID: id, Outcome: outcome})
	}
	sort.Slice(acceptance, func(i, j int) bool { return acceptance[i].ID < acceptance[j].ID })
	verification := make([]deliveryCanonicalVerification, 0, len(task.Verification))
	covered := map[string]bool{}
	for _, row := range task.Verification {
		covers, err := deliveryCanonicalCoverage(row.Covers, declared)
		if err != nil {
			return deliveryProofContract{}, err
		}
		kind, text, err := deliveryCanonicalCheck(row.Check)
		if err != nil {
			return deliveryProofContract{}, err
		}
		entry := deliveryCanonicalVerification{Covers: covers, Type: kind, Text: text}
		if kind == "manual" {
			gate, err := deliveryCanonicalManualGate(plan.HumanGates, task.SourceKey, covers)
			if err != nil {
				return deliveryProofContract{}, err
			}
			entry.ManualGateSourceKey = gate
		}
		for _, id := range covers {
			covered[id] = true
		}
		verification = append(verification, entry)
	}
	for id := range declared {
		if !covered[id] {
			return deliveryProofContract{}, fmt.Errorf("strict proof contract leaves acceptance %s without verification coverage", id)
		}
	}
	sort.Slice(verification, func(i, j int) bool {
		left, right := strings.Join(verification[i].Covers, ","), strings.Join(verification[j].Covers, ",")
		if left != right {
			return left < right
		}
		if verification[i].Type != verification[j].Type {
			return verification[i].Type < verification[j].Type
		}
		if verification[i].Text != verification[j].Text {
			return verification[i].Text < verification[j].Text
		}
		return verification[i].ManualGateSourceKey < verification[j].ManualGateSourceKey
	})
	contract := deliveryProofContract{Schema: deliveryProofContractSchema, Scope: strings.TrimSpace(plan.Scope), SourceKey: strings.TrimSpace(task.SourceKey), DeliveryTaskFingerprint: deliveryV2TaskFingerprint(task, plan.HumanGates), Acceptance: acceptance, Verification: verification}
	return deliveryFinalizeProofContract(contract)
}

func deliveryCanonicalCoverage(raw string, declared map[string]bool) ([]string, error) {
	seen := map[string]bool{}
	var out []string
	for _, part := range strings.Split(raw, ",") {
		id := strings.ToUpper(strings.TrimSpace(part))
		if id == "" || !declared[id] || seen[id] {
			return nil, fmt.Errorf("strict proof contract has invalid verification coverage %q", raw)
		}
		seen[id] = true
		out = append(out, id)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("strict proof contract has empty verification coverage")
	}
	sort.Strings(out)
	return out, nil
}

func deliveryCanonicalCheck(raw string) (string, string, error) {
	raw = strings.TrimSpace(raw)
	for _, candidate := range []struct{ prefix, kind string }{{"command:", "command"}, {"manual proof:", "manual"}} {
		if strings.HasPrefix(raw, candidate.prefix) {
			text := strings.TrimSpace(strings.TrimPrefix(raw, candidate.prefix))
			if text == "" {
				return "", "", fmt.Errorf("strict proof contract has empty %s check", candidate.kind)
			}
			return candidate.kind, text, nil
		}
	}
	return "", "", fmt.Errorf("strict proof contract check must be command: or manual proof: %q", raw)
}

func deliveryCanonicalManualGate(gates []deliveryHumanGate, taskKey string, covers []string) (string, error) {
	var match string
	for _, gate := range gates {
		if strings.TrimSpace(gate.TaskSourceKey) != strings.TrimSpace(taskKey) {
			continue
		}
		ids, err := deliveryCanonicalCoverage(strings.Join(gate.AcceptanceIDs, ","), mapFromStrings(covers))
		if err != nil || strings.Join(ids, ",") != strings.Join(covers, ",") {
			continue
		}
		if match != "" {
			return "", fmt.Errorf("strict proof contract manual verification has ambiguous source-keyed human gate for %s", taskKey)
		}
		match = strings.TrimSpace(gate.SourceKey)
	}
	if match == "" {
		return "", fmt.Errorf("strict proof contract manual verification has no exact source-keyed human gate for %s", taskKey)
	}
	return match, nil
}

func mapFromStrings(values []string) map[string]bool {
	out := map[string]bool{}
	for _, value := range values {
		out[value] = true
	}
	return out
}

func deliveryFinalizeProofContract(contract deliveryProofContract) (deliveryProofContract, error) {
	contract.Fingerprint = ""
	raw, err := yaml.Marshal(contract)
	if err != nil {
		return deliveryProofContract{}, err
	}
	contract.Fingerprint = deliveryFingerprint(raw)
	return contract, nil
}

func deliveryValidateProofContract(contract deliveryProofContract) error {
	if contract.Schema != deliveryProofContractSchema || contract.Scope == "" || contract.SourceKey == "" || contract.DeliveryTaskFingerprint == "" || contract.Fingerprint == "" {
		return fmt.Errorf("strict proof contract is missing authoritative material")
	}
	want, err := deliveryFinalizeProofContract(contract)
	if err != nil || want.Fingerprint != contract.Fingerprint {
		return fmt.Errorf("strict proof contract fingerprint drift; reopen/rework from reviewed source material")
	}
	return nil
}

func deliveryProofResultsFor(contract deliveryProofContract) deliveryProofResultsProjection {
	rows := make([]deliveryProofResultRow, 0, len(contract.Verification))
	for _, verification := range contract.Verification {
		raw, _ := yaml.Marshal(verification)
		rows = append(rows, deliveryProofResultRow{VerificationFingerprint: deliveryFingerprint(raw), Result: "pending"})
	}
	return deliveryProofResultsProjection{Schema: deliveryProofResultsSchema, ContractFingerprint: contract.Fingerprint, Rows: rows}
}

// deliveryInstallStrictProofContract is intentionally an unexported import
// seam. It does not make the unavailable capability available; public import
// is still rejected before it can reach this function.
func deliveryInstallStrictProofContract(data, existing map[string]any, held bool, contract deliveryProofContract) error {
	if err := deliveryValidateProofContract(contract); err != nil {
		return err
	}
	if existing != nil && !held {
		persisted, err := deliveryProofContractFromData(existing)
		if err != nil {
			return fmt.Errorf("strict adoption requires reopen/rework: %w", err)
		}
		if persisted.Fingerprint != contract.Fingerprint {
			return fmt.Errorf("strict adoption requires reopen/rework from reviewed source material")
		}
	}
	data["delivery_proof_contract"] = contract
	data["delivery_proof_contract_fingerprint"] = contract.Fingerprint
	if existing != nil {
		if persisted, err := deliveryProofContractFromData(existing); err == nil && persisted.Fingerprint == contract.Fingerprint {
			results, ok := existing["delivery_proof_results"]
			if !ok {
				data["delivery_proof_results"] = deliveryProofResultsFor(contract)
				return nil
			}
			data["delivery_proof_results"] = results
			return nil
		}
	}
	data["delivery_proof_results"] = deliveryProofResultsFor(contract)
	return nil
}

func deliveryProofContractFromData(data map[string]any) (deliveryProofContract, error) {
	var contract deliveryProofContract
	raw, err := yaml.Marshal(data["delivery_proof_contract"])
	if err != nil {
		return deliveryProofContract{}, fmt.Errorf("strict proof contract projection is absent or malformed")
	}
	if err := yaml.Unmarshal(raw, &contract); err != nil {
		return deliveryProofContract{}, fmt.Errorf("strict proof contract projection is absent or malformed")
	}
	if stringField(data, "delivery_proof_contract_fingerprint") != contract.Fingerprint {
		return deliveryProofContract{}, fmt.Errorf("strict proof contract projection fingerprint is absent or drifted")
	}
	if err := deliveryValidateProofContract(contract); err != nil {
		return deliveryProofContract{}, err
	}
	return contract, nil
}

func deliveryStrictLineageFromData(data map[string]any) (deliveryStrictImportLineage, bool, error) {
	_, hasLineage := data["delivery_strict_import_lineage"]
	_, hasFingerprint := data["delivery_strict_import_lineage_fingerprint"]
	if !hasLineage && !hasFingerprint {
		return deliveryStrictImportLineage{}, false, nil
	}
	var lineage deliveryStrictImportLineage
	raw, err := yaml.Marshal(data["delivery_strict_import_lineage"])
	if err != nil || yaml.Unmarshal(raw, &lineage) != nil || lineage.Schema != deliveryStrictImportLineageSchema || lineage.Fingerprint == "" || stringField(data, "delivery_strict_import_lineage_fingerprint") != lineage.Fingerprint {
		return deliveryStrictImportLineage{}, true, fmt.Errorf("strict import lineage is absent or malformed")
	}
	if lineage.Scope == "" || lineage.SourceKey == "" || lineage.DeliveryTaskFingerprint == "" {
		return deliveryStrictImportLineage{}, true, fmt.Errorf("strict import lineage is missing source identity")
	}
	copy := lineage
	copy.Fingerprint = ""
	canonical, marshalErr := yaml.Marshal(copy)
	if marshalErr != nil || deliveryFingerprint(canonical) != lineage.Fingerprint {
		return deliveryStrictImportLineage{}, true, fmt.Errorf("strict import lineage fingerprint drift")
	}
	return lineage, true, nil
}

func deliveryStrictWaveLineageFromData(data map[string]any) (deliveryStrictWaveLineage, bool, error) {
	_, hasLineage := data["delivery_strict_import_lineage"]
	_, hasFingerprint := data["delivery_strict_import_lineage_fingerprint"]
	if !hasLineage && !hasFingerprint {
		return deliveryStrictWaveLineage{}, false, nil
	}
	var lineage deliveryStrictWaveLineage
	raw, err := yaml.Marshal(data["delivery_strict_import_lineage"])
	if err != nil || yaml.Unmarshal(raw, &lineage) != nil || lineage.Schema != deliveryStrictImportLineageSchema || lineage.Fingerprint == "" || stringField(data, "delivery_strict_import_lineage_fingerprint") != lineage.Fingerprint {
		return deliveryStrictWaveLineage{}, true, fmt.Errorf("strict wave import lineage is absent or malformed")
	}
	copy := lineage
	copy.Fingerprint = ""
	canonical, marshalErr := yaml.Marshal(copy)
	if marshalErr != nil || deliveryFingerprint(canonical) != lineage.Fingerprint {
		return deliveryStrictWaveLineage{}, true, fmt.Errorf("strict wave import lineage fingerprint drift")
	}
	return lineage, true, nil
}

// deliveryStrictAuthorityFor resolves strictness from mirrored import lineage,
// never from the presence of a mutable proof field. One missing or substituted
// side is corrupt; only records with no strict lineage on either side are
// historical/legacy.
func deliveryStrictAuthorityFor(task Note, wave Note) (deliveryStrictAuthorityState, error) {
	taskLineage, taskPresent, taskErr := deliveryStrictLineageFromData(task.Data)
	waveLineage, wavePresent, waveErr := deliveryStrictWaveLineageFromData(wave.Data)
	if taskErr != nil || waveErr != nil {
		return deliveryStrictAuthorityCorrupt, fmt.Errorf("strict import lineage corrupt or missing")
	}
	if !taskPresent && !wavePresent {
		return deliveryStrictAuthorityLegacy, nil
	}
	if !taskPresent || !wavePresent {
		return deliveryStrictAuthorityCorrupt, fmt.Errorf("strict import lineage is missing one authoritative projection; reopen/rework")
	}
	waveTask, ok := waveLineage.Tasks[stringField(task.Data, "id")]
	if !ok || waveTask.Fingerprint != taskLineage.Fingerprint {
		return deliveryStrictAuthorityCorrupt, fmt.Errorf("strict import lineage task/wave mismatch; reopen/rework")
	}
	contract, err := deliveryProofContractFromData(task.Data)
	if err != nil || contract.Scope != taskLineage.Scope || contract.SourceKey != taskLineage.SourceKey || contract.DeliveryTaskFingerprint != taskLineage.DeliveryTaskFingerprint {
		return deliveryStrictAuthorityCorrupt, fmt.Errorf("strict proof contract corrupt or missing; reopen/rework")
	}
	return deliveryStrictAuthorityCurrent, nil
}
