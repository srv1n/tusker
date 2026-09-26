package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

// doctorDaemonStaleAfter bounds how long a daemon may go without reconciling
// before queued work treats it as not reconciling on schedule. The lease and
// poll intervals reconcile far more often; ten minutes is staircase evidence,
// not a race with normal polling.
const doctorDaemonStaleAfter = 10 * time.Minute

// doctorMaxFindings keeps wave-level diagnoses bounded on large waves.
const doctorMaxFindings = 100

// doctorReport is the versioned doctor outcome: the full diagnostic envelope
// plus the process exit code its primary classification requires.
type doctorReport struct {
	Schema           string    `json:"schema"`
	SubjectType      string    `json:"subject_type"`
	SubjectID        string    `json:"subject_id"`
	Title            string    `json:"title"`
	Diagnosis        Diagnosis `json:"diagnosis"`
	ExitCode         int       `json:"exit_code"`
	BuildComparison  string    `json:"build_comparison"`
	PrimaryNextSteps []string  `json:"primary_next_steps,omitempty"`
}

func doctorExitForClassification(classification DiagnosticClassification) int {
	switch classification {
	case DiagnosticHealthy, DiagnosticComplete, DiagnosticNormalWait:
		return 0
	case DiagnosticRecoverableFault, DiagnosticHumanDecision, DiagnosticProductDefect:
		return 1
	default:
		return 2
	}
}

// doctorObservationTime anchors a diagnosis to the subject's recorded state
// time so repeated reads of unchanged state emit identical reports. When the
// subject carries no readable timestamp the read time is used.
func doctorObservationTime(subject Note, now time.Time) string {
	for _, key := range []string{"updated_at", "updated", "created_at", "created"} {
		if parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(stringField(subject.Data, key))); err == nil {
			return parsed.UTC().Format(time.RFC3339)
		}
	}
	return now.UTC().Format(time.RFC3339)
}

func diagnoseTaskForDoctorWithRuntime(vault string, store *RuntimeStore, runtimeErr error, projectID, taskID string, now time.Time) (Diagnosis, error) {
	idx, err := loadV7Index(vault)
	if err != nil {
		return Diagnosis{}, err
	}
	task, ok := idx.Tasks[taskID]
	if !ok {
		return Diagnosis{}, tuskerError(errorNotFound, "doctor task is missing: "+taskID)
	}
	scope := DiagnosticScope{Project: projectID, Record: trackerRecordID(task)}
	if waveID := strings.TrimSpace(stringField(task.Data, "wave")); waveID != "" {
		scope.Wave = waveID
	}
	revision := firstNonEmpty(stringField(task.Data, "state_rev"), "unavailable")
	observed := doctorObservationTime(task, now)
	var findings []DiagnosticFinding

	dispatchBlockers := v7TaskDispatchBlockers(vault, task)
	memberOfWave := strings.TrimSpace(stringField(task.Data, "wave")) != ""
	for _, blocker := range dispatchBlockers {
		// Canonical wave-member authoring (backlog/held) is the state arming
		// and the member's own reservation promote past it. It is never
		// itself a contract defect; the review-member findings below carry
		// the member's actual wait or fault.
		if memberOfWave && (blocker == "status is backlog" || blocker == "readiness is held") {
			continue
		}
		findings = append(findings, doctorContractBlockerFinding(scope, taskID, blocker, observed, revision))
		if len(findings) >= doctorMaxFindings {
			break
		}
	}

	if waveID := strings.TrimSpace(stringField(task.Data, "wave")); waveID != "" {
		if wave, ok := idx.Waves[waveID]; ok {
			findings = append(findings, doctorWaveMemberFindings(vault, store, runtimeErr, idx, wave, task, scope, projectID, observed, now)...)
		} else {
			findings = append(findings, DiagnosticFinding{
				Code: "doctor-wave-missing", Scope: scope, Classification: DiagnosticProductDefect,
				Affects: []string{taskID}, NextActor: DiagnosticAuthorityOperator,
				Evidence: DiagnosticEvidence{Source: "wave", Revision: "unavailable", ObservedAt: observed, Detail: "Task names wave " + waveID + " which does not resolve."},
				Action: &RecoveryAction{
					ID: "doctor-wave-missing", Type: RecoveryActionNoSupportedFix,
					RequiredAuthority: DiagnosticAuthorityOperator,
					ExpectedEffect:    "The dangling wave reference is corrected by the task author.",
					Postcondition:     "Task " + taskID + " names a wave that resolves.",
				},
			})
		}
	}

	findings = append(findings, doctorHumanGateFindings(idx, task, scope, observed, revision)...)

	if store != nil {
		if directive, directiveErr := store.RunDirective(projectID, trackerRecordID(task)); directiveErr == nil && directive != nil {
			findings = append(findings, doctorDirectiveFinding(vault, idx, task, directive, scope, projectID, observed, now))
		}
		runs, runsErr := store.ListRuns()
		if runsErr == nil {
			findings = append(findings, doctorCapacityFindings(vault, store, projectID, runs, scope, taskID, observed)...)
		}
	} else {
		findings = append(findings, DiagnosticFinding{
			Code: "doctor-runtime-unavailable", Scope: scope, Classification: DiagnosticUnavailable,
			Affects: []string{taskID}, NextActor: DiagnosticAuthorityOperator,
			Evidence: DiagnosticEvidence{Source: "runtime", Revision: "unavailable", ObservedAt: observed, Detail: "Runtime state is unavailable; reservations, capacity, and daemon freshness cannot be verified."},
		})
	}

	if len(findings) == 0 {
		findings = append(findings, DiagnosticFinding{
			Code: "doctor-healthy", Scope: scope, Classification: DiagnosticHealthy,
			Affects: []string{taskID}, NextActor: DiagnosticAuthorityNone,
			Evidence: DiagnosticEvidence{Source: "task", Revision: revision, ObservedAt: observed, Detail: "No contract, authorization, reservation, capacity, or gate blocker was observed."},
		})
	}
	if len(findings) > doctorMaxFindings {
		findings = findings[:doctorMaxFindings]
	}
	return NewDiagnosis(DiagnosisInput{
		Dimensions:      doctorDimensions(task, revision, store != nil),
		SourceRevisions: map[string]string{"task": revision},
		Findings:        doctorDedupeFindingCodes(findings),
	})
}

// doctorDedupeFindingCodes keeps every independent finding while guaranteeing
// stable unique codes: repeated raw blockers that map to one finding kind
// keep the first code and take deterministic -2, -3 suffixes after it.
func doctorDedupeFindingCodes(findings []DiagnosticFinding) []DiagnosticFinding {
	seen := map[string]int{}
	for index := range findings {
		code := findings[index].Code
		if count := seen[code]; count > 0 {
			findings[index].Code = fmt.Sprintf("%s-%d", code, count+1)
		}
		seen[code]++
	}
	return findings
}

func diagnoseWaveForDoctorWithRuntime(vault string, store *RuntimeStore, runtimeErr error, projectID, waveID string, now time.Time) (Diagnosis, error) {
	idx, err := loadV7Index(vault)
	if err != nil {
		return Diagnosis{}, err
	}
	wave, ok := idx.Waves[waveID]
	if !ok {
		return Diagnosis{}, tuskerError(errorNotFound, "doctor wave is missing: "+waveID)
	}
	scope := DiagnosticScope{Project: projectID, Wave: waveID}
	observed := doctorObservationTime(wave, now)
	revision := firstNonEmpty(stringField(wave.Data, "authorization_fingerprint"), stringField(wave.Data, "state_rev"))
	if revision == "" {
		revision = "unavailable"
	}
	var findings []DiagnosticFinding

	review, err := buildDirectWaveReview(vault, store, projectID, waveID, runtimeErr)
	if err != nil {
		return Diagnosis{}, err
	}
	findings = append(findings, doctorAuthorizationFindings(wave, scope, observed, revision)...)
	for _, blocker := range review.Blockers {
		findings = append(findings, doctorReviewBlockerFinding(blocker, scope, observed, revision))
		if len(findings) >= doctorMaxFindings {
			break
		}
	}
	for _, member := range review.Members {
		finding, include := doctorReviewMemberFinding(member, scope, observed, revision)
		if !include {
			continue
		}
		findings = append(findings, finding)
		if len(findings) >= doctorMaxFindings {
			break
		}
	}
	if store == nil {
		findings = append(findings, DiagnosticFinding{
			Code: "doctor-runtime-unavailable", Scope: scope, Classification: DiagnosticUnavailable,
			NextActor: DiagnosticAuthorityOperator,
			Evidence:  DiagnosticEvidence{Source: "runtime", Revision: "unavailable", ObservedAt: observed, Detail: "Runtime state is unavailable; reservations, capacity, and daemon freshness cannot be verified."},
		})
	} else {
		findings = append(findings, doctorDaemonFreshnessFindings(store, scope, true, observed, now)...)
	}
	if len(findings) == 0 {
		findings = append(findings, DiagnosticFinding{
			Code: "doctor-healthy", Scope: scope, Classification: DiagnosticHealthy,
			NextActor: DiagnosticAuthorityNone,
			Evidence:  DiagnosticEvidence{Source: "wave", Revision: revision, ObservedAt: observed, Detail: "No authorization, member, reservation, or runtime blocker was observed."},
		})
	}
	if len(findings) > doctorMaxFindings {
		findings = findings[:doctorMaxFindings]
	}
	return NewDiagnosis(DiagnosisInput{
		Dimensions:      doctorWaveDimensions(wave, revision, store != nil),
		SourceRevisions: map[string]string{"wave": revision},
		Findings:        doctorDedupeFindingCodes(findings),
	})
}

func doctorDimensions(task Note, revision string, runtimeAvailable bool) ReadinessDimensions {
	provenance := func(source, rev string) ReadinessDimension {
		return ReadinessDimension{State: ReadinessStateReady, Provenance: ReadinessProvenance{Source: source, Revision: rev}}
	}
	dimensions := ReadinessDimensions{
		Contract:            provenance("task", revision),
		Import:              provenance("import", revision),
		Interactive:         provenance("work-session", revision),
		Automation:          provenance("automation", revision),
		Authorization:       provenance("wave", revision),
		Runtime:             provenance("runtime", revision),
		OptionalIntegration: provenance("provider", revision),
	}
	if !runtimeAvailable {
		dimensions.Runtime = ReadinessDimension{State: ReadinessStateUnavailable, Provenance: ReadinessProvenance{Source: "runtime", Revision: "unavailable"}}
	}
	return dimensions
}

func doctorWaveDimensions(wave Note, revision string, runtimeAvailable bool) ReadinessDimensions {
	provenance := func(source, rev string) ReadinessDimension {
		return ReadinessDimension{State: ReadinessStateReady, Provenance: ReadinessProvenance{Source: source, Revision: rev}}
	}
	dimensions := ReadinessDimensions{
		Contract:            provenance("wave", revision),
		Import:              provenance("import", revision),
		Interactive:         provenance("work-session", revision),
		Automation:          provenance("automation", revision),
		Authorization:       provenance("wave", revision),
		Runtime:             provenance("runtime", revision),
		OptionalIntegration: provenance("provider", revision),
	}
	if !runtimeAvailable {
		dimensions.Runtime = ReadinessDimension{State: ReadinessStateUnavailable, Provenance: ReadinessProvenance{Source: "runtime", Revision: "unavailable"}}
	}
	_ = wave
	return dimensions
}

// doctorContractBlockerFinding maps one raw dispatch blocker to a stable
// classified finding. Dependency and capacity waits carry no repair action:
// they identify the owner and the next check instead.
func doctorContractBlockerFinding(scope DiagnosticScope, taskID, blocker, observed, revision string) DiagnosticFinding {
	lowered := strings.ToLower(blocker)
	finding := DiagnosticFinding{
		Scope: scope, Affects: []string{taskID}, NextActor: DiagnosticAuthorityOperator,
		Evidence: DiagnosticEvidence{Source: "task", Revision: revision, ObservedAt: observed, Detail: blocker},
	}
	switch {
	case strings.Contains(lowered, "dependenc"):
		finding.Code = "doctor-dependency-waiting"
		finding.Classification = DiagnosticNormalWait
		finding.NextActor = DiagnosticAuthorityAgent
		finding.RetryAfter = "1m"
	case strings.Contains(lowered, "capacity") || strings.Contains(lowered, "concurrency") || strings.Contains(lowered, "limit reached"):
		finding.Code = "doctor-capacity-waiting"
		finding.Classification = DiagnosticNormalWait
		finding.NextActor = DiagnosticAuthorityDaemon
		finding.RetryAfter = "1m"
	case strings.Contains(lowered, "authorization is disarmed") || strings.Contains(lowered, "not durably armed"):
		finding.Code = "doctor-authority-missing"
		finding.Classification = DiagnosticRecoverableFault
		finding.NextActor = DiagnosticAuthorityHuman
		finding.Action = &RecoveryAction{
			ID: "doctor-authority-missing", Type: RecoveryActionWaveStart,
			Argv:              []string{"tusker", "wave", "start", scope.Wave, "--mode", "background"},
			Preconditions:     []string{"a human operator supplies --by human:<name> for the exact current material"},
			RequiredAuthority: DiagnosticAuthorityHuman,
			ExpectedEffect:    "The wave is armed for the exact current material and eligible roots queue.",
			Postcondition:     "tusker wave review " + scope.Wave + " --check reports Start enabled.",
		}
	case strings.Contains(lowered, "paused"):
		finding.Code = "doctor-wave-paused"
		finding.Classification = DiagnosticRecoverableFault
		finding.NextActor = DiagnosticAuthorityOperator
		finding.Action = &RecoveryAction{
			ID: "doctor-wave-paused", Type: RecoveryActionWaveResume,
			Argv:              []string{"tusker", "wave", "resume", scope.Wave, "--by", "human:<name>"},
			Preconditions:     []string{"wave material matches the stored authorization fingerprint"},
			RequiredAuthority: DiagnosticAuthorityHuman,
			ExpectedEffect:    "The wave resumes without renewing drifted authority.",
			Postcondition:     "Wave " + scope.Wave + " reports armed authorization.",
		}
	default:
		finding.Code = "doctor-contract-defect"
		finding.Classification = DiagnosticProductDefect
		finding.Action = &RecoveryAction{
			ID: "doctor-contract-defect", Type: RecoveryActionNoSupportedFix,
			RequiredAuthority: DiagnosticAuthorityOperator,
			ExpectedEffect:    "The task author corrects the contract; no single command repairs authoring defects.",
			Postcondition:     "The contract blocker is absent from a fresh diagnosis.",
		}
	}
	return finding
}

func doctorWaveMemberFindings(vault string, store *RuntimeStore, runtimeErr error, idx v7Index, wave, task Note, scope DiagnosticScope, projectID, observed string, now time.Time) []DiagnosticFinding {
	taskID := stringField(task.Data, "id")
	review, err := buildDirectWaveReview(vault, store, projectID, stringField(wave.Data, "id"), runtimeErr)
	if err != nil {
		return []DiagnosticFinding{{
			Code: "doctor-review-unavailable", Scope: scope, Classification: DiagnosticUnavailable,
			Affects: []string{taskID}, NextActor: DiagnosticAuthorityOperator,
			Evidence: DiagnosticEvidence{Source: "wave", Revision: firstNonEmpty(stringField(wave.Data, "authorization_fingerprint"), "unavailable"), ObservedAt: observed, Detail: "Wave review is unavailable: " + err.Error()},
		}}
	}
	var member *directWaveReviewMember
	for index := range review.Members {
		if review.Members[index].TaskID == taskID {
			candidate := review.Members[index]
			member = &candidate
			break
		}
	}
	if member == nil {
		return nil
	}
	finding, include := doctorReviewMemberFinding(*member, scope, observed, firstNonEmpty(stringField(wave.Data, "authorization_fingerprint"), "unavailable"))
	if !include {
		return nil
	}
	return []DiagnosticFinding{finding}
}

// doctorReviewMemberFinding maps one review member state to a finding.
// Ready members need no finding; queued members wait on the daemon's next
// claim; dependency waits name their owner without suggesting repair.
func doctorReviewMemberFinding(member directWaveReviewMember, scope DiagnosticScope, observed, revision string) (DiagnosticFinding, bool) {
	base := DiagnosticFinding{
		Scope: scope, Affects: []string{member.TaskID}, NextActor: DiagnosticAuthorityAgent,
		Evidence: DiagnosticEvidence{Source: "wave", Revision: revision, ObservedAt: observed, Detail: firstNonEmpty(member.WaitingReason, "member state "+member.State)},
	}
	switch member.State {
	case "ready", "completed", "reviewing", "running":
		return DiagnosticFinding{}, false
	case "waiting":
		reason := strings.ToLower(member.WaitingReason)
		switch {
		case strings.Contains(reason, "waiting for dependency"):
			base.Code = "doctor-dependency-waiting"
			base.Classification = DiagnosticNormalWait
			base.NextActor = DiagnosticAuthorityOperator
			base.RetryAfter = "1m"
			return base, true
		case strings.Contains(reason, "queued for dispatch") || strings.Contains(reason, "queued under wave authorization"):
			base.Code = "doctor-queued-for-dispatch"
			base.Classification = DiagnosticNormalWait
			base.NextActor = DiagnosticAuthorityDaemon
			base.RetryAfter = "1m"
			return base, true
		case strings.Contains(reason, "paus"):
			base.Code = "doctor-wave-paused"
			base.Classification = DiagnosticRecoverableFault
			base.NextActor = DiagnosticAuthorityOperator
			base.Action = &RecoveryAction{
				ID: "doctor-wave-paused", Type: RecoveryActionWaveResume,
				Argv:              []string{"tusker", "wave", "resume", scope.Wave, "--by", "human:<name>"},
				Preconditions:     []string{"wave material matches the stored authorization fingerprint"},
				RequiredAuthority: DiagnosticAuthorityHuman,
				ExpectedEffect:    "The wave resumes without renewing drifted authority.",
				Postcondition:     "Wave " + scope.Wave + " reports armed authorization.",
			}
			return base, true
		default:
			base.Code = "doctor-member-waiting"
			base.Classification = DiagnosticNormalWait
			base.NextActor = DiagnosticAuthority(firstNonEmpty(member.Responsible, "operator"))
			if !validDiagnosticAuthority(base.NextActor) {
				base.NextActor = DiagnosticAuthorityOperator
			}
			return base, true
		}
	case "blocked", "failed":
		base.Code = "doctor-member-blocked"
		base.Classification = DiagnosticRecoverableFault
		base.NextActor = DiagnosticAuthorityOperator
		base.Action = &RecoveryAction{
			ID: "doctor-member-blocked", Type: RecoveryActionNoSupportedFix,
			RequiredAuthority: DiagnosticAuthorityOperator,
			ExpectedEffect:    "The operator inspects the member failure and applies the review's recorded repair.",
			Postcondition:     "The member failure is absent from a fresh diagnosis.",
		}
		return base, true
	default:
		base.Code = "doctor-member-" + member.State
		base.Classification = DiagnosticUnavailable
		base.NextActor = DiagnosticAuthorityOperator
		return base, true
	}
}

func doctorAuthorizationFindings(wave Note, scope DiagnosticScope, observed, revision string) []DiagnosticFinding {
	waveID := stringField(wave.Data, "id")
	state := strings.ToLower(strings.TrimSpace(stringField(wave.Data, "authorization")))
	if state == "" {
		state = "disarmed"
	}
	authorizedBy := strings.TrimSpace(stringField(wave.Data, "authorized_by"))
	switch state {
	case "armed":
		return nil
	case "paused":
		action := &RecoveryAction{
			ID: "doctor-wave-paused", Type: RecoveryActionWaveResume,
			Argv:              []string{"tusker", "wave", "resume", waveID, "--by", firstNonEmpty(authorizedBy, "human:<name>")},
			Preconditions:     []string{"wave material matches the stored authorization fingerprint"},
			RequiredAuthority: DiagnosticAuthorityHuman,
			ExpectedEffect:    "The wave resumes without renewing drifted authority.",
			Postcondition:     "Wave " + waveID + " reports armed authorization.",
		}
		return []DiagnosticFinding{{
			Code: "doctor-wave-paused", Scope: scope, Classification: DiagnosticRecoverableFault,
			NextActor: DiagnosticAuthorityOperator,
			Evidence:  DiagnosticEvidence{Source: "wave", Revision: revision, ObservedAt: observed, Detail: "Wave authorization is paused; admitted attempts finish while new admissions wait."},
			Action:    action,
		}}
	default:
		return []DiagnosticFinding{{
			Code: "doctor-authority-missing", Scope: scope, Classification: DiagnosticRecoverableFault,
			NextActor: DiagnosticAuthorityHuman,
			Evidence:  DiagnosticEvidence{Source: "wave", Revision: revision, ObservedAt: observed, Detail: "Wave authorization is " + state + "; nothing runs until an explicit Start."},
			Action: &RecoveryAction{
				ID: "doctor-authority-missing", Type: RecoveryActionWaveStart,
				Argv:              []string{"tusker", "wave", "start", waveID, "--mode", "background"},
				Preconditions:     []string{"a human operator supplies --by human:<name> for the exact current material"},
				RequiredAuthority: DiagnosticAuthorityHuman,
				ExpectedEffect:    "The wave is armed for the exact current material and eligible roots queue.",
				Postcondition:     "tusker wave review " + waveID + " --check reports Start enabled.",
			},
		}}
	}
}

func doctorReviewBlockerFinding(blocker directStartBlocker, scope DiagnosticScope, observed, revision string) DiagnosticFinding {
	finding := DiagnosticFinding{
		Scope: scope, NextActor: DiagnosticAuthorityOperator,
		Evidence: DiagnosticEvidence{Source: "wave", Revision: revision, ObservedAt: observed, Detail: blocker.Reason},
	}
	if blocker.TaskID != "" {
		finding.Affects = []string{blocker.TaskID}
	}
	switch blocker.Code {
	case "HUMAN_GATE_OPEN":
		finding.Code = "doctor-human-gate-open"
		finding.Classification = DiagnosticHumanDecision
		finding.NextActor = DiagnosticAuthorityHuman
		finding.Action = &RecoveryAction{
			ID: "doctor-human-gate-open", Type: RecoveryActionGateSatisfy,
			Argv:              []string{"tusker", "gate", "satisfy", blocker.GateID},
			Preconditions:     []string{"the named human owns gate " + blocker.GateID},
			RequiredAuthority: DiagnosticAuthorityHuman,
			ExpectedEffect:    "Human gate " + blocker.GateID + " is satisfied.",
			Postcondition:     "Gate " + blocker.GateID + " no longer blocks " + blocker.TaskID + ".",
		}
	case "DEPENDENCY_CONTRACT_INVALID", "CONTRACT_FINGERPRINT_STALE", "ROUTE_INVALID":
		finding.Code = "doctor-contract-" + strings.ToLower(strings.ReplaceAll(blocker.Code, "_", "-"))
		finding.Classification = DiagnosticProductDefect
		finding.Action = &RecoveryAction{
			ID: finding.Code, Type: RecoveryActionNoSupportedFix,
			RequiredAuthority: DiagnosticAuthorityOperator,
			ExpectedEffect:    "The task author corrects the contract; no single command repairs authoring defects.",
			Postcondition:     "The contract blocker is absent from a fresh diagnosis.",
		}
	case "RUNTIME_UNAVAILABLE":
		finding.Code = "doctor-runtime-unavailable"
		finding.Classification = DiagnosticUnavailable
		finding.Evidence.Source = "runtime"
		finding.Evidence.Revision = "unavailable"
	case "OUTCOME_UNKNOWN":
		finding.Code = "doctor-outcome-unknown"
		finding.Classification = DiagnosticProductDefect
		finding.Action = &RecoveryAction{
			ID: "doctor-outcome-unknown", Type: RecoveryActionNoSupportedFix,
			RequiredAuthority: DiagnosticAuthorityOperator,
			ExpectedEffect:    "The operator verifies existing work and continues the task; uncertain effects are never retried blindly.",
			Postcondition:     "The unknown outcome is resolved to a trustworthy terminal outcome.",
		}
	case "ACTIVE_OWNER":
		finding.Code = "doctor-owner-held"
		finding.Classification = DiagnosticNormalWait
		finding.NextActor = DiagnosticAuthorityAgent
		finding.RetryAfter = "1m"
	default:
		finding.Code = "doctor-review-" + strings.ToLower(strings.ReplaceAll(blocker.Code, "_", "-"))
		finding.Classification = DiagnosticRecoverableFault
		finding.Action = &RecoveryAction{
			ID: finding.Code, Type: RecoveryActionNoSupportedFix,
			RequiredAuthority: DiagnosticAuthorityOperator,
			ExpectedEffect:    "The operator applies the review's recorded repair: " + blocker.Action,
			Postcondition:     "The review blocker is absent from a fresh diagnosis.",
		}
	}
	return finding
}

func doctorHumanGateFindings(idx v7Index, task Note, scope DiagnosticScope, observed, revision string) []DiagnosticFinding {
	taskID := stringField(task.Data, "id")
	var findings []DiagnosticFinding
	for _, gate := range sortedV7Gates(idx) {
		if !v7GateTouchesTask(gate, taskID) || !boolField(gate.Data, "blocking") || !strings.EqualFold(stringField(gate.Data, "status"), "open") {
			continue
		}
		if v7ProofOwnerClass(stringField(gate.Data, "owner")) != "human" {
			continue
		}
		gateID := stringField(gate.Data, "id")
		findings = append(findings, DiagnosticFinding{
			Code: "doctor-human-gate-open", Scope: scope, Classification: DiagnosticHumanDecision,
			Affects: []string{taskID}, NextActor: DiagnosticAuthorityHuman,
			Evidence: DiagnosticEvidence{Source: "gate", Revision: revision, ObservedAt: observed, Detail: "Human gate " + gateID + " is open on " + taskID + "."},
			Action: &RecoveryAction{
				ID: "doctor-human-gate-" + gateID, Type: RecoveryActionGateSatisfy,
				Argv:              []string{"tusker", "gate", "satisfy", gateID},
				Preconditions:     []string{"the named human owns gate " + gateID},
				RequiredAuthority: DiagnosticAuthorityHuman,
				ExpectedEffect:    "Human gate " + gateID + " is satisfied.",
				Postcondition:     "Gate " + gateID + " no longer blocks " + taskID + ".",
			},
		})
		if len(findings) >= doctorMaxFindings {
			break
		}
	}
	return findings
}

// doctorDirectiveFinding names the owning reservation behind queued work: its
// bound wave, authority fingerprint, and actor. A reservation bound to stale
// authority can never match again and is the fault, not capacity.
func doctorDirectiveFinding(vault string, idx v7Index, task Note, directive *RunDirective, scope DiagnosticScope, projectID, observed string, now time.Time) DiagnosticFinding {
	taskID := stringField(task.Data, "id")
	recordID := trackerRecordID(task)
	revision := firstNonEmpty(stringField(task.Data, "state_rev"), "unavailable")
	detail := "Queued reservation for " + recordID + " in state " + directive.State + "."
	if directive.WaveID != "" {
		detail += " Owned by wave " + directive.WaveID + " authorization."
		if wave, ok := idx.Waves[directive.WaveID]; ok {
			current := stringField(wave.Data, "authorization_fingerprint")
			currentAt := stringField(wave.Data, "authorized_at")
			if current == "" || current != directive.AuthorizationFingerprint || currentAt != directive.WaveAuthorizedAt {
				return DiagnosticFinding{
					Code: "doctor-stale-reservation", Scope: scope, Classification: DiagnosticRecoverableFault,
					Affects: []string{taskID}, NextActor: DiagnosticAuthorityOperator,
					Evidence: DiagnosticEvidence{Source: "runtime", Revision: revision, ObservedAt: observed, Detail: "Queued reservation for " + recordID + " is bound to superseded wave authorization and can never match again."},
					Action: &RecoveryAction{
						ID: "doctor-stale-reservation", Type: RecoveryActionWaveStart,
						Argv:              []string{"tusker", "wave", "start", directive.WaveID, "--mode", "background"},
						Preconditions:     []string{"a human operator supplies --by human:<name> for the exact current material"},
						RequiredAuthority: DiagnosticAuthorityHuman,
						ExpectedEffect:    "The wave re-arms on current material and re-binds the eligible frontier.",
						Postcondition:     "A fresh reservation bound to current wave authority exists for " + recordID + ".",
					},
				}
			}
			detail += " Bound to current wave authority."
		}
		if actor := strings.TrimSpace(directive.Actor); actor != "" {
			detail += " Queued by " + actor + "."
		}
	} else {
		detail += " Task-scoped reservation."
		if actor := strings.TrimSpace(directive.Actor); actor != "" {
			detail += " Queued by " + actor + "."
		}
	}
	_ = projectID
	_ = now
	return DiagnosticFinding{
		Code: "doctor-queued-reservation", Scope: scope, Classification: DiagnosticNormalWait,
		Affects: []string{taskID}, NextActor: DiagnosticAuthorityDaemon, RetryAfter: "1m",
		Evidence: DiagnosticEvidence{Source: "runtime", Revision: revision, ObservedAt: observed, Detail: detail},
	}
}

func doctorCapacityFindings(vault string, store *RuntimeStore, projectID string, runs []RunStatus, scope DiagnosticScope, taskID, observed string) []DiagnosticFinding {
	globalActive := 0
	projectActive := 0
	for _, run := range runs {
		if !isDispatchCapacityLeaseState(run.LeaseState) {
			continue
		}
		globalActive++
		if run.ProjectID == projectID {
			projectActive++
		}
	}
	globalLimit := 2
	if report, err := configResolve(vault, "runtime.max_active_runs"); err == nil {
		if limit := intFromAny(report.Value); limit > 0 {
			globalLimit = limit
		}
	}
	projectLimit := 0
	if wf, err := loadWorkflow(vault); err == nil {
		projectLimit = projectActiveRunLimit(wf.Data)
	}
	var findings []DiagnosticFinding
	if globalLimit > 0 && globalActive >= globalLimit {
		findings = append(findings, DiagnosticFinding{
			Code: "doctor-global-capacity", Scope: scope, Classification: DiagnosticNormalWait,
			Affects: []string{taskID}, NextActor: DiagnosticAuthorityDaemon, RetryAfter: "1m",
			Evidence: DiagnosticEvidence{Source: "runtime", Revision: "live", ObservedAt: observed, Detail: fmt.Sprintf("Global execution capacity is full (%d/%d); no project is singled out.", globalActive, globalLimit)},
		})
	}
	if projectLimit > 0 && projectActive >= projectLimit {
		findings = append(findings, DiagnosticFinding{
			Code: "doctor-project-capacity", Scope: scope, Classification: DiagnosticNormalWait,
			Affects: []string{taskID}, NextActor: DiagnosticAuthorityDaemon, RetryAfter: "1m",
			Evidence: DiagnosticEvidence{Source: "runtime", Revision: "live", ObservedAt: observed, Detail: fmt.Sprintf("Project execution capacity is full (%d/%d).", projectActive, projectLimit)},
		})
	}
	return findings
}

// doctorDaemonFreshnessFindings reports a missing or stale daemon only when
// the subject actually waits on daemon work. A quiet project with no queued
// or admitted work gets no daemon finding at all.
func doctorDaemonFreshnessFindings(store *RuntimeStore, scope DiagnosticScope, daemonWaiting bool, observed string, now time.Time) []DiagnosticFinding {
	if !daemonWaiting {
		return nil
	}
	lastPoll, err := store.GetSetting("daemon_last_poll_at")
	if err != nil || strings.TrimSpace(lastPoll) == "" {
		return []DiagnosticFinding{{
			Code: "doctor-daemon-absent", Scope: scope, Classification: DiagnosticRecoverableFault,
			NextActor: DiagnosticAuthorityOperator,
			Evidence:  DiagnosticEvidence{Source: "runtime", Revision: "unavailable", ObservedAt: observed, Detail: "Work waits on daemon reconciliation but no daemon poll was recorded."},
			Action: &RecoveryAction{
				ID: "doctor-daemon-absent", Type: RecoveryActionNoSupportedFix,
				RequiredAuthority: DiagnosticAuthorityOperator,
				ExpectedEffect:    "The operator starts the resident daemon or enables Background work so queued work reconciles.",
				Postcondition:     "A fresh daemon poll is recorded after the change.",
			},
		}}
	}
	pollAt, pollErr := time.Parse(time.RFC3339, strings.TrimSpace(lastPoll))
	if pollErr != nil {
		if pollAtNano, nanoErr := time.Parse(time.RFC3339Nano, strings.TrimSpace(lastPoll)); nanoErr == nil {
			pollAt, pollErr = pollAtNano, nil
		}
	}
	if pollErr != nil {
		return []DiagnosticFinding{{
			Code: "doctor-daemon-unreadable", Scope: scope, Classification: DiagnosticUnavailable,
			NextActor: DiagnosticAuthorityOperator,
			Evidence:  DiagnosticEvidence{Source: "runtime", Revision: "unavailable", ObservedAt: observed, Detail: "The recorded daemon poll time is unreadable."},
		}}
	}
	if now.UTC().Sub(pollAt) > doctorDaemonStaleAfter {
		return []DiagnosticFinding{{
			Code: "doctor-daemon-stale", Scope: scope, Classification: DiagnosticRecoverableFault,
			NextActor: DiagnosticAuthorityOperator,
			Evidence:  DiagnosticEvidence{Source: "runtime", Revision: "live", ObservedAt: observed, Detail: "Work waits on daemon reconciliation but the last recorded poll was " + strings.TrimSpace(lastPoll) + "."},
			Action: &RecoveryAction{
				ID: "doctor-daemon-stale", Type: RecoveryActionNoSupportedFix,
				RequiredAuthority: DiagnosticAuthorityOperator,
				ExpectedEffect:    "The operator restores daemon reconciliation so queued work advances.",
				Postcondition:     "A fresh daemon poll is recorded after the change.",
			},
		}}
	}
	return nil
}

func doctorBuildComparison(store *RuntimeStore) string {
	if store == nil {
		return "unknown"
	}
	// No daemon build identity is recorded yet, so a mismatch cannot be
	// observed. Unknown is explicit: the doctor never reports a match it did
	// not measure.
	if recorded, err := store.GetSetting("daemon_build_version"); err == nil && strings.TrimSpace(recorded) != "" {
		return "recorded:" + strings.TrimSpace(recorded)
	}
	return "unknown"
}

func renderDoctorHuman(report doctorReport) string {
	var lines []string
	lines = append(lines, report.SubjectType+" "+report.SubjectID+" — "+report.Diagnosis.PrimaryCode+" ("+string(report.Diagnosis.PrimaryClassification)+")")
	if report.Diagnosis.PrimaryClassification == DiagnosticHealthy || report.Diagnosis.PrimaryClassification == DiagnosticComplete {
		lines = append(lines, "No action required.")
		return strings.Join(lines, "\n")
	}
	primary := report.Diagnosis.Findings[0]
	for _, finding := range report.Diagnosis.Findings {
		if finding.Code == report.Diagnosis.PrimaryCode {
			primary = finding
			break
		}
	}
	if primary.Evidence.Detail != "" {
		lines = append(lines, "Cause: "+primary.Evidence.Detail)
	}
	lines = append(lines, "Next actor: "+string(primary.NextActor))
	if primary.Action != nil {
		if primary.Action.Type == RecoveryActionNoSupportedFix {
			lines = append(lines, "No supported repair: "+primary.Action.ExpectedEffect)
		} else if len(primary.Action.Argv) > 0 {
			lines = append(lines, "Next action: "+strings.Join(primary.Action.Argv, " "))
		}
		if primary.Action.Postcondition != "" {
			lines = append(lines, "Verify: "+primary.Action.Postcondition)
		}
	}
	consequences := 0
	for _, finding := range report.Diagnosis.Findings {
		if finding.Code == primary.Code {
			continue
		}
		consequences++
	}
	if consequences > 0 {
		lines = append(lines, fmt.Sprintf("Also visible: %d further finding(s); see --json for the complete result.", consequences))
	}
	if report.BuildComparison == "unknown" {
		lines = append(lines, "Daemon build identity is unavailable; no build comparison was made.")
	}
	return strings.Join(lines, "\n")
}

func executionDoctorCmd(args Args) (int, error) {
	target := strings.ToUpper(strings.TrimSpace(firstNonEmpty(args.String("id"), args.String("_pos0"))))
	if target == "" {
		return 2, tuskerError(errorMissingArg, "Usage: tusker doctor <TASK-ID|WAVE-ID> [--json] [--output <new-path>]")
	}
	vault, err := resolveVaultPath(args, false)
	if err != nil {
		return 2, err
	}
	store, runtimeErr := directWaveReviewRuntimeStore()
	if store != nil {
		defer store.Close()
	}
	projectID, _ := resolveV7ProjectID(vault)
	if store != nil {
		if registeredID, registered, regErr := registeredProjectIDForVault(store, vault); regErr == nil && registered && strings.TrimSpace(registeredID) != "" {
			projectID = registeredID
		}
	}
	now := time.Now().UTC()
	idx, err := loadV7Index(vault)
	if err != nil {
		return 2, err
	}
	report := doctorReport{Schema: "tusker.doctor/v1", SubjectID: target}
	var diagnosis Diagnosis
	var diagnosisErr error
	if _, isTask := idx.Tasks[target]; isTask {
		report.SubjectType = "task"
		diagnosis, diagnosisErr = diagnoseTaskForDoctorWithRuntime(vault, store, runtimeErr, projectID, target, now)
	} else if _, isWave := idx.Waves[target]; isWave {
		report.SubjectType = "wave"
		diagnosis, diagnosisErr = diagnoseWaveForDoctorWithRuntime(vault, store, runtimeErr, projectID, target, now)
	} else {
		return 2, tuskerError(errorNotFound, "doctor target is missing: "+target)
	}
	if diagnosisErr != nil {
		return 2, diagnosisErr
	}
	report.Title = doctorReportTitle(idx, report.SubjectType, target)
	report.Diagnosis = diagnosis
	report.ExitCode = doctorExitForClassification(diagnosis.PrimaryClassification)
	report.BuildComparison = doctorBuildComparison(store)
	for _, finding := range diagnosis.Findings {
		if finding.Code != diagnosis.PrimaryCode || finding.Action == nil || len(finding.Action.Argv) == 0 {
			continue
		}
		report.PrimaryNextSteps = append([]string(nil), finding.Action.Argv...)
		break
	}
	if output := strings.TrimSpace(args.String("output")); output != "" {
		if _, statErr := os.Stat(output); statErr == nil {
			return 1, tuskerError(errorInvalidTransition, "doctor export refuses to overwrite existing file: "+output)
		} else if !os.IsNotExist(statErr) {
			return 2, tuskerError(errorInvalidTransition, "doctor export cannot inspect output path: "+statErr.Error())
		}
		encoded, encodeErr := doctorReportJSON(report)
		if encodeErr != nil {
			return 2, encodeErr
		}
		if writeErr := os.WriteFile(output, append(encoded, '\n'), 0o644); writeErr != nil {
			return 2, tuskerError(errorInvalidTransition, "doctor export failed: "+writeErr.Error())
		}
	}
	if args.Bool("json") {
		encoded, encodeErr := doctorReportJSON(report)
		if encodeErr != nil {
			return 2, encodeErr
		}
		fmt.Printf("%s\n", encoded)
		return report.ExitCode, nil
	}
	fmt.Println(renderDoctorHuman(report))
	return report.ExitCode, nil
}

func doctorReportTitle(idx v7Index, subjectType, target string) string {
	if subjectType == "task" {
		if note, ok := idx.Tasks[target]; ok {
			return stringField(note.Data, "title")
		}
	}
	if note, ok := idx.Waves[target]; ok {
		return stringField(note.Data, "title")
	}
	return ""
}

func doctorReportJSON(report doctorReport) ([]byte, error) {
	return json.Marshal(report)
}
