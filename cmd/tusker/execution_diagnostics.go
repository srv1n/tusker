package main

import (
	"sort"
	"strings"
	"time"
)

const (
	// DiagnosticSchema versions the self-service diagnostic envelope. It
	// extends the existing readiness facts; it does not replace them.
	DiagnosticSchema  = "tusker.diagnosis/v1"
	DiagnosticVersion = 1
)

// DiagnosticClassification separates healthy, waiting, actionable, and
// unavailable outcomes. It is data, never an execution decision: producers
// diagnose read-only inputs and consumers choose stage-specific predicates.
type DiagnosticClassification string

const (
	DiagnosticHealthy          DiagnosticClassification = "healthy"
	DiagnosticComplete         DiagnosticClassification = "complete"
	DiagnosticNormalWait       DiagnosticClassification = "normal_wait"
	DiagnosticRecoverableFault DiagnosticClassification = "recoverable_fault"
	DiagnosticHumanDecision    DiagnosticClassification = "human_decision"
	DiagnosticProductDefect    DiagnosticClassification = "product_defect"
	DiagnosticUnavailable      DiagnosticClassification = "unavailable"
)

// DiagnosticAuthority names who may apply a recovery action. It mirrors the
// existing readiness authority domains without inventing new actors.
type DiagnosticAuthority string

const (
	DiagnosticAuthorityAgent      DiagnosticAuthority = "agent"
	DiagnosticAuthorityReviewer   DiagnosticAuthority = "reviewer"
	DiagnosticAuthorityHuman      DiagnosticAuthority = "human"
	DiagnosticAuthorityOperator   DiagnosticAuthority = "operator"
	DiagnosticAuthorityDaemon     DiagnosticAuthority = "daemon"
	DiagnosticAuthorityCommandUID DiagnosticAuthority = "command_executor"
	DiagnosticAuthorityNone       DiagnosticAuthority = "none"
)

// RecoveryActionType allowlists the typed recovery descriptors a diagnosis
// may carry. There is deliberately no free-form shell/command/script type:
// recovery is argv plus authority plus postcondition, never an executable
// protocol smuggled through a string field.
type RecoveryActionType string

const (
	RecoveryActionTaskStart      RecoveryActionType = "task_start"
	RecoveryActionWaveStart      RecoveryActionType = "wave_start"
	RecoveryActionWaveResume     RecoveryActionType = "wave_resume"
	RecoveryActionWavePause      RecoveryActionType = "wave_pause"
	RecoveryActionGateSatisfy    RecoveryActionType = "gate_satisfy"
	RecoveryActionProjectEnable  RecoveryActionType = "project_enable"
	RecoveryActionWorkReconcile  RecoveryActionType = "work_reconcile"
	RecoveryActionRunRetry       RecoveryActionType = "run_retry"
	RecoveryActionNoSupportedFix RecoveryActionType = "no_supported_repair"
)

// DiagnosticScope binds a finding to project, record, and wave identity.
// At least one scope field is required; scope is identity, not evidence.
type DiagnosticScope struct {
	Project string `json:"project,omitempty"`
	Record  string `json:"record,omitempty"`
	Wave    string `json:"wave,omitempty"`
}

// DiagnosticEvidence is the bounded, read-only observation behind a finding.
// Detail carries a short human note; it must never carry environment secrets
// or raw worker prompts. Unavailable or stale evidence disables unsafe
// recovery actions at construction time.
type DiagnosticEvidence struct {
	Source     string `json:"source"`
	Revision   string `json:"revision"`
	ObservedAt string `json:"observed_at"`
	Detail     string `json:"detail,omitempty"`
	Stale      bool   `json:"stale,omitempty"`
}

// RecoveryAction is a typed, non-executable recovery descriptor. Argv holds
// the exact supported command arguments; preconditions, authority, expected
// effect, and postcondition tell the actor when the action applies and how
// to prove it worked.
type RecoveryAction struct {
	ID                string              `json:"id"`
	Type              RecoveryActionType  `json:"type"`
	Argv              []string            `json:"argv"`
	Preconditions     []string            `json:"preconditions,omitempty"`
	RequiredAuthority DiagnosticAuthority `json:"required_authority"`
	ExpectedEffect    string              `json:"expected_effect"`
	Postcondition     string              `json:"postcondition"`
}

// DiagnosticFinding is one classified, scoped observation with its causal
// parents, affected tasks, next actor, and optional typed recovery.
type DiagnosticFinding struct {
	Code           string                   `json:"code"`
	Scope          DiagnosticScope          `json:"scope"`
	Classification DiagnosticClassification `json:"classification"`
	Causes         []string                 `json:"causes,omitempty"`
	Affects        []string                 `json:"affects,omitempty"`
	NextActor      DiagnosticAuthority      `json:"next_actor"`
	RetryAfter     string                   `json:"retry_after,omitempty"`
	Evidence       DiagnosticEvidence       `json:"evidence"`
	Action         *RecoveryAction          `json:"action,omitempty"`
}

// DiagnosisInput carries the read-only facts a diagnosis is built from. The
// constructor never reads or writes vault, DB, process, or provider state.
type DiagnosisInput struct {
	// Dimensions preserves the existing readiness dimensions so downstream
	// admission, doctor, repair, and UI consumers share one set of facts.
	Dimensions ReadinessDimensions `json:"dimensions"`
	// SourceRevisions pins the expected revision per evidence source.
	// Evidence naming a pinned source must carry that exact revision.
	SourceRevisions map[string]string   `json:"source_revisions,omitempty"`
	Findings        []DiagnosticFinding `json:"findings"`
}

// Diagnosis is the versioned diagnostic envelope: preserved readiness
// dimensions, every independent finding, and the deterministic primary cause.
type Diagnosis struct {
	Schema                string                   `json:"schema"`
	Version               int                      `json:"version"`
	Dimensions            ReadinessDimensions      `json:"dimensions"`
	Findings              []DiagnosticFinding      `json:"findings"`
	PrimaryCode           string                   `json:"primary_code"`
	PrimaryClassification DiagnosticClassification `json:"primary_classification"`
}

// NewDiagnosis validates and assembles a diagnosis from read-only inputs.
// It performs no I/O and mutates no shared state.
func NewDiagnosis(input DiagnosisInput) (Diagnosis, error) {
	diagnosis := Diagnosis{
		Schema:     DiagnosticSchema,
		Version:    DiagnosticVersion,
		Dimensions: input.Dimensions,
		Findings:   cloneDiagnosticFindings(input.Findings),
	}
	if err := validateDiagnosis(diagnosis, input.SourceRevisions); err != nil {
		return Diagnosis{}, err
	}
	primary := primaryDiagnosticFinding(diagnosis.Findings)
	diagnosis.PrimaryCode = primary.Code
	diagnosis.PrimaryClassification = primary.Classification
	return diagnosis, nil
}

// DiagnoseUnavailable builds a diagnosis whose only finding reports
// unavailable input. It carries no recovery action: unavailable state cannot
// be coerced to ready and unsafe actions stay disabled.
func DiagnoseUnavailable(dimensions ReadinessDimensions, scope DiagnosticScope, source, reason string) (Diagnosis, error) {
	evidence := DiagnosticEvidence{
		Source:     source,
		Revision:   "unavailable",
		ObservedAt: time.Now().UTC().Format(time.RFC3339),
		Detail:     reason,
	}
	return NewDiagnosis(DiagnosisInput{
		Dimensions: dimensions,
		Findings: []DiagnosticFinding{{
			Code:           "unavailable-input",
			Scope:          scope,
			Classification: DiagnosticUnavailable,
			NextActor:      DiagnosticAuthorityOperator,
			Evidence:       evidence,
		}},
	})
}

// SafeActions returns the typed recovery actions whose evidence is fresh and
// available. Actions on stale or unavailable evidence are never safe to apply.
func (diagnosis Diagnosis) SafeActions() []RecoveryAction {
	var actions []RecoveryAction
	for _, finding := range diagnosis.Findings {
		if finding.Action == nil {
			continue
		}
		if finding.Evidence.Stale || finding.Classification == DiagnosticUnavailable {
			continue
		}
		actions = append(actions, *finding.Action)
	}
	return actions
}

// FindingCodes returns the stable finding codes in deterministic order.
func (diagnosis Diagnosis) FindingCodes() []string {
	codes := make([]string, 0, len(diagnosis.Findings))
	for _, finding := range diagnosis.Findings {
		codes = append(codes, finding.Code)
	}
	sort.Strings(codes)
	return codes
}

func validateDiagnosis(diagnosis Diagnosis, sourceRevisions map[string]string) error {
	if diagnosis.Schema != DiagnosticSchema || diagnosis.Version != DiagnosticVersion {
		return diagnosticError("unsupported diagnostic envelope version")
	}
	if len(diagnosis.Findings) == 0 {
		return diagnosticError("diagnosis requires at least one finding")
	}
	codes := map[string]bool{}
	for _, finding := range diagnosis.Findings {
		if strings.TrimSpace(finding.Code) == "" {
			return diagnosticError("finding requires a stable code")
		}
		if codes[finding.Code] {
			return diagnosticError("duplicate finding code " + finding.Code)
		}
		codes[finding.Code] = true
		if err := validateDiagnosticScope(finding.Scope); err != nil {
			return diagnosticError("finding " + finding.Code + ": " + err.Error())
		}
		if !validDiagnosticClassification(finding.Classification) {
			return diagnosticError("finding " + finding.Code + " has an unknown classification")
		}
		if !validDiagnosticAuthority(finding.NextActor) {
			return diagnosticError("finding " + finding.Code + " has an unknown next actor")
		}
		if err := validateDiagnosticEvidence(finding.Code, finding.Evidence, sourceRevisions); err != nil {
			return err
		}
		for _, cause := range finding.Causes {
			if cause == finding.Code {
				return diagnosticError("finding " + finding.Code + " cannot cause itself")
			}
			if !causalContains(diagnosis.Findings, cause) {
				return diagnosticError("finding " + finding.Code + " names unknown cause " + cause)
			}
		}
		for _, affected := range finding.Affects {
			if !boundedDiagnosticText(affected) {
				return diagnosticError("finding " + finding.Code + " has an unbounded affected task id")
			}
		}
		if strings.TrimSpace(finding.RetryAfter) != "" {
			if finding.Classification != DiagnosticNormalWait && finding.Classification != DiagnosticUnavailable {
				return diagnosticError("finding " + finding.Code + " sets retry_after outside a wait")
			}
			if _, err := time.ParseDuration(strings.TrimSpace(finding.RetryAfter)); err != nil {
				if _, timeErr := time.Parse(time.RFC3339, strings.TrimSpace(finding.RetryAfter)); timeErr != nil {
					return diagnosticError("finding " + finding.Code + " has an unreadable retry_after")
				}
			}
		}
		if finding.Action != nil {
			if err := validateRecoveryAction(finding.Code, finding.Classification, finding.Evidence, finding.Action); err != nil {
				return err
			}
		} else if finding.Classification == DiagnosticRecoverableFault {
			return diagnosticError("finding " + finding.Code + " is a recoverable fault without a typed recovery action")
		}
	}
	if err := rejectDiagnosticCauseCycles(diagnosis.Findings); err != nil {
		return err
	}
	return nil
}

func causalContains(findings []DiagnosticFinding, code string) bool {
	for _, finding := range findings {
		if finding.Code == code {
			return true
		}
	}
	return false
}

func validateDiagnosticScope(scope DiagnosticScope) error {
	if strings.TrimSpace(scope.Project) == "" && strings.TrimSpace(scope.Record) == "" && strings.TrimSpace(scope.Wave) == "" {
		return diagnosticError("malformed scope: project, record, or wave is required")
	}
	for _, field := range []string{scope.Project, scope.Record, scope.Wave} {
		if field != "" && !boundedDiagnosticText(field) {
			return diagnosticError("malformed scope: scope fields are bounded identity text")
		}
	}
	return nil
}

func validateDiagnosticEvidence(code string, evidence DiagnosticEvidence, sourceRevisions map[string]string) error {
	if strings.TrimSpace(evidence.Source) == "" {
		return diagnosticError("finding " + code + " evidence requires a source")
	}
	if !boundedDiagnosticText(evidence.Source) {
		return diagnosticError("finding " + code + " evidence source is unbounded")
	}
	if strings.TrimSpace(evidence.Revision) == "" {
		return diagnosticError("finding " + code + " evidence requires a source revision")
	}
	if expected, pinned := sourceRevisions[evidence.Source]; pinned && evidence.Revision != expected {
		return diagnosticError("finding " + code + " evidence revision " + evidence.Revision + " mismatches pinned revision for source " + evidence.Source)
	}
	if strings.TrimSpace(evidence.ObservedAt) == "" {
		return diagnosticError("finding " + code + " evidence requires an observation time")
	}
	if _, err := time.Parse(time.RFC3339, strings.TrimSpace(evidence.ObservedAt)); err != nil {
		return diagnosticError("finding " + code + " evidence has an unreadable observation time")
	}
	if evidence.Detail != "" && !boundedDiagnosticText(evidence.Detail) {
		return diagnosticError("finding " + code + " evidence detail is unbounded")
	}
	return nil
}

func validateRecoveryAction(code string, classification DiagnosticClassification, evidence DiagnosticEvidence, action *RecoveryAction) error {
	if strings.TrimSpace(action.ID) == "" {
		return diagnosticError("finding " + code + " recovery action requires an id")
	}
	if !validRecoveryActionType(action.Type) {
		return diagnosticError("finding " + code + " recovery action has an unknown type")
	}
	if evidence.Stale || classification == DiagnosticUnavailable {
		return diagnosticError("finding " + code + " cannot carry a recovery action on stale or unavailable evidence")
	}
	if action.Type == RecoveryActionNoSupportedFix {
		if len(action.Argv) != 0 {
			return diagnosticError("finding " + code + " no-supported-repair action carries no argv")
		}
	} else if len(action.Argv) == 0 {
		return diagnosticError("finding " + code + " recovery action requires typed argv")
	}
	for _, arg := range action.Argv {
		if !boundedDiagnosticText(arg) {
			return diagnosticError("finding " + code + " recovery argv is unbounded")
		}
		if strings.ContainsAny(arg, ";|&$`()\"\n\\") {
			return diagnosticError("finding " + code + " recovery argv must be typed arguments, never an embedded shell")
		}
	}
	for _, precondition := range action.Preconditions {
		if !boundedDiagnosticText(precondition) {
			return diagnosticError("finding " + code + " recovery precondition is unbounded")
		}
	}
	if !validDiagnosticAuthority(action.RequiredAuthority) || action.RequiredAuthority == DiagnosticAuthorityNone {
		return diagnosticError("finding " + code + " recovery action requires an explicit authority")
	}
	if !boundedDiagnosticText(action.ExpectedEffect) || !boundedDiagnosticText(action.Postcondition) {
		return diagnosticError("finding " + code + " recovery action requires a bounded expected effect and verifiable postcondition")
	}
	return nil
}

// rejectDiagnosticCauseCycles refuses causal loops so a primary cause can
// never chase its own consequences. Independent blockers stay independent.
func rejectDiagnosticCauseCycles(findings []DiagnosticFinding) error {
	edges := map[string][]string{}
	for _, finding := range findings {
		edges[finding.Code] = append([]string(nil), finding.Causes...)
	}
	const (
		white = 0
		grey  = 1
		black = 2
	)
	color := map[string]int{}
	var visit func(code string, path []string) error
	visit = func(code string, path []string) error {
		switch color[code] {
		case black:
			return nil
		case grey:
			return diagnosticError("causal cycle through " + strings.Join(append(path, code), " -> "))
		}
		color[code] = grey
		for _, next := range edges[code] {
			if err := visit(next, append(path, code)); err != nil {
				return err
			}
		}
		color[code] = black
		return nil
	}
	codes := make([]string, 0, len(findings))
	for _, finding := range findings {
		codes = append(codes, finding.Code)
	}
	sort.Strings(codes)
	for _, code := range codes {
		if err := visit(code, nil); err != nil {
			return err
		}
	}
	_ = white
	return nil
}

// primaryDiagnosticFinding selects the deterministic human primary cause.
// Precedence ranks actionable faults first; ties break on stable code so the
// same findings always elect the same primary without suppressing the
// independent blockers retained beside it.
func primaryDiagnosticFinding(findings []DiagnosticFinding) DiagnosticFinding {
	ordered := append([]DiagnosticFinding(nil), findings...)
	sort.Slice(ordered, func(i, j int) bool {
		rankI, rankJ := diagnosticPrecedence(ordered[i].Classification), diagnosticPrecedence(ordered[j].Classification)
		if rankI != rankJ {
			return rankI < rankJ
		}
		return ordered[i].Code < ordered[j].Code
	})
	return ordered[0]
}

func diagnosticPrecedence(classification DiagnosticClassification) int {
	switch classification {
	case DiagnosticRecoverableFault:
		return 0
	case DiagnosticHumanDecision:
		return 1
	case DiagnosticProductDefect:
		return 2
	case DiagnosticUnavailable:
		return 3
	case DiagnosticNormalWait:
		return 4
	case DiagnosticHealthy, DiagnosticComplete:
		return 5
	default:
		return 6
	}
}

func validDiagnosticClassification(classification DiagnosticClassification) bool {
	switch classification {
	case DiagnosticHealthy, DiagnosticComplete, DiagnosticNormalWait,
		DiagnosticRecoverableFault, DiagnosticHumanDecision,
		DiagnosticProductDefect, DiagnosticUnavailable:
		return true
	default:
		return false
	}
}

func validDiagnosticAuthority(authority DiagnosticAuthority) bool {
	switch authority {
	case DiagnosticAuthorityAgent, DiagnosticAuthorityReviewer, DiagnosticAuthorityHuman,
		DiagnosticAuthorityOperator, DiagnosticAuthorityDaemon,
		DiagnosticAuthorityCommandUID, DiagnosticAuthorityNone:
		return true
	default:
		return false
	}
}

func validRecoveryActionType(actionType RecoveryActionType) bool {
	switch actionType {
	case RecoveryActionTaskStart, RecoveryActionWaveStart, RecoveryActionWaveResume,
		RecoveryActionWavePause, RecoveryActionGateSatisfy, RecoveryActionProjectEnable,
		RecoveryActionWorkReconcile, RecoveryActionRunRetry, RecoveryActionNoSupportedFix:
		return true
	default:
		return false
	}
}

func diagnosticError(message string) error {
	return tuskerError(errorReadinessContractInvalid, message)
}

func boundedDiagnosticText(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 320 {
		return false
	}
	for _, runeValue := range value {
		if runeValue < 0x20 || runeValue == 0x7f {
			return false
		}
	}
	return true
}

func cloneDiagnosticFindings(findings []DiagnosticFinding) []DiagnosticFinding {
	out := make([]DiagnosticFinding, len(findings))
	for index, finding := range findings {
		clone := finding
		clone.Causes = append([]string(nil), finding.Causes...)
		clone.Affects = append([]string(nil), finding.Affects...)
		if finding.Action != nil {
			action := *finding.Action
			action.Argv = append([]string(nil), finding.Action.Argv...)
			action.Preconditions = append([]string(nil), finding.Action.Preconditions...)
			clone.Action = &action
		}
		out[index] = clone
	}
	return out
}
