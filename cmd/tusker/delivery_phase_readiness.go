package main

import (
	"fmt"
	"sort"
)

// finalizeDeliveryReviewReadiness turns the three deliberately separate
// delivery phases into the shared CLI/Serve contract. It is pure: review and
// Serve use it without importing, arming, polling, or otherwise changing
// operational state.
func finalizeDeliveryReviewReadiness(r *deliveryReview, vault string, plan deliveryPlan, path string) error {
	r.contractIssues = uniqueStrings(r.contractIssues)
	r.importIssues = uniqueStrings(r.importIssues)
	r.startIssues = uniqueStrings(r.startIssues)
	sort.Strings(r.contractIssues)
	sort.Strings(r.importIssues)
	sort.Strings(r.startIssues)

	projectID := v7ProjectID(vault)
	waveID := r.Flow.WaveID
	provenance := ReadinessProvenance{Source: "delivery_review", Revision: r.Start.PlanFingerprint}
	blockers := make([]ReadinessBlocker, 0, len(r.contractIssues)+len(r.importIssues)+len(r.startBlockers))
	for i, issue := range r.contractIssues {
		blockers = append(blockers, deliveryReadinessBlocker(fmt.Sprintf("delivery-contract-%03d", i+1), ReadinessBlockerContractInvalid, ReadinessAuthorityContract, []ReadinessDimensionKind{ReadinessDimensionContract}, projectID, waveID, issue, "repair the delivery contract or regenerate its bounded context"))
	}
	for i, issue := range r.importIssues {
		blockers = append(blockers, deliveryReadinessBlocker(fmt.Sprintf("delivery-import-%03d", i+1), ReadinessBlockerImportMissing, ReadinessAuthorityImport, []ReadinessDimensionKind{ReadinessDimensionImport}, projectID, waveID, issue, "repair the held import lineage or atomic write safety before importing"))
	}
	blockers = append(blockers, r.startBlockers...)

	contractState := deliveryPhaseState(len(r.contractIssues) == 0)
	importState := deliveryPhaseState(len(r.importIssues) == 0)
	automationState := deliveryReadinessDimensionState(blockers, ReadinessDimensionAutomation)
	authorizationState := deliveryReadinessDimensionState(blockers, ReadinessDimensionAuthorization)
	runtimeState := deliveryReadinessDimensionState(blockers, ReadinessDimensionRuntime)
	contract, err := NewReadinessContract(ReadinessInput{Dimensions: ReadinessDimensions{
		Contract:            ReadinessDimension{State: contractState, Provenance: provenance},
		Import:              ReadinessDimension{State: importState, Provenance: provenance},
		Interactive:         ReadinessDimension{State: ReadinessStateNotApplicable, Provenance: provenance},
		Automation:          ReadinessDimension{State: automationState, Provenance: provenance},
		Authorization:       ReadinessDimension{State: authorizationState, Provenance: provenance},
		Runtime:             ReadinessDimension{State: runtimeState, Provenance: provenance},
		OptionalIntegration: ReadinessDimension{State: ReadinessStateNotApplicable, Provenance: provenance},
	}, Blockers: blockers})
	if err != nil {
		return err
	}
	r.Readiness = contract
	r.PlanValid = contractState == ReadinessStateReady
	r.ImportReady = r.PlanValid && importState == ReadinessStateReady
	r.StartReady = r.ImportReady && automationState == ReadinessStateReady && authorizationState == ReadinessStateReady && runtimeState == ReadinessStateReady
	compatibility, err := ProjectLegacyReadiness(contract, ReadinessLegacyAdapter{
		ReadinessDimension:        ReadinessDimensionRuntime,
		ReadinessByState:          map[ReadinessState]string{ReadinessStateReady: "ready to start delivery", ReadinessStateBlocked: "blocked"},
		DispatchabilityDimensions: []ReadinessDimensionKind{ReadinessDimensionContract, ReadinessDimensionImport, ReadinessDimensionAutomation, ReadinessDimensionAuthorization, ReadinessDimensionRuntime},
		BlockerDimensions:         []ReadinessDimensionKind{ReadinessDimensionContract, ReadinessDimensionImport, ReadinessDimensionAutomation, ReadinessDimensionAuthorization, ReadinessDimensionRuntime},
	})
	if err != nil {
		return err
	}
	if !r.StartReady {
		// Legacy Start.Readiness historically meant the whole Start boundary,
		// while the generic adapter names one source dimension. Keep that field
		// truthful when automation or authorization—not runtime—blocks Start.
		compatibility.Readiness = "blocked"
	}
	r.Compatibility = compatibility
	// Existing clients keep receiving ready/startBoundary fields. They are an
	// explicit Start projection, while planValid now governs review success.
	r.Ready = r.StartReady
	r.Start.Readiness = compatibility.Readiness
	r.Start.Blockers = compatibility.Blockers
	if r.PlanValid && r.ImportReady && r.StartReady && r.Start.State == "held" {
		r.Start.StateLabel = "Ready to start"
		r.Start.NextAction = deliveryReviewStartCommand(vault, path, r.Start.PlanFingerprint)
	} else if r.Start.NextAction == "" && !r.PlanValid {
		r.Start.State = "invalid"
		r.Start.StateLabel = "Delivery plan is invalid"
		r.Start.NextAction = "Resolve the first blocker: " + r.contractIssues[0]
	} else if r.Start.NextAction == "" && !r.ImportReady {
		r.Start.State = "changed"
		r.Start.StateLabel = "Held import is blocked"
		r.Start.NextAction = "Resolve the first held-import blocker: " + r.importIssues[0]
	} else if r.Start.NextAction == "" && !r.StartReady {
		r.Start.NextAction = "Resolve the first Start blocker: " + r.startIssues[0]
	}
	return nil
}

func deliveryPhaseState(ready bool) ReadinessState {
	if ready {
		return ReadinessStateReady
	}
	return ReadinessStateBlocked
}

func deliveryReadinessBlocker(id string, kind ReadinessBlockerKind, authority ReadinessAuthorityDomain, affects []ReadinessDimensionKind, projectID, waveID, reason, remedy string) ReadinessBlocker {
	return ReadinessBlocker{ID: id, Kind: kind, Authority: authority, Affects: affects, ProjectID: projectID, WaveID: waveID, Reason: reason, Remedy: remedy}
}

func deliveryReadinessDimensionState(blockers []ReadinessBlocker, dimension ReadinessDimensionKind) ReadinessState {
	for _, blocker := range blockers {
		if readinessBlockerAffects(blocker, dimension) {
			return ReadinessStateBlocked
		}
	}
	return ReadinessStateReady
}

func deliveryReviewAddStartBlocker(r *deliveryReview, blocker ReadinessBlocker) {
	for _, existing := range r.startBlockers {
		if existing.ID == blocker.ID {
			return
		}
	}
	r.startBlockers = append(r.startBlockers, blocker)
	r.startIssues = append(r.startIssues, blocker.Reason)
}

// Environment facts are already typed booleans. Keep their phase mapping
// here instead of reverse-engineering a cause from a preflight sentence.
func deliveryReviewAddEnvironmentStartBlockers(r *deliveryReview, env wavePreflightEnvironment, projectID, waveID string) {
	add := func(id string, kind ReadinessBlockerKind, authority ReadinessAuthorityDomain, affects []ReadinessDimensionKind, reason, remedy string) {
		deliveryReviewAddStartBlocker(r, deliveryReadinessBlocker(id, kind, authority, affects, projectID, waveID, reason, remedy))
	}
	if !env.ProjectRegistered {
		add("delivery-start-project-registration", ReadinessBlockerRuntimeUnavailable, ReadinessAuthorityRuntime, []ReadinessDimensionKind{ReadinessDimensionRuntime}, "Project is not registered", "register the project before Start")
	}
	if !env.ProjectEnabled {
		add("delivery-start-automation-disabled", ReadinessBlockerAutomationDisabled, ReadinessAuthorityAutomation, []ReadinessDimensionKind{ReadinessDimensionAutomation}, "Project automation is off", "enable project automation before Start")
	}
	if !env.ProjectHealthy {
		add("delivery-start-project-health", ReadinessBlockerRuntimeUnavailable, ReadinessAuthorityRuntime, []ReadinessDimensionKind{ReadinessDimensionRuntime}, "Project health is blocked", "repair project health before Start")
	}
	if !env.WorkflowCompatible {
		add("delivery-start-workflow", ReadinessBlockerRuntimeUnavailable, ReadinessAuthorityRuntime, []ReadinessDimensionKind{ReadinessDimensionRuntime}, "Workflow is incompatible", "repair workflow compatibility before Start")
	}
	if !env.SkillCompatible {
		add("delivery-start-skill", ReadinessBlockerRuntimeUnavailable, ReadinessAuthorityRuntime, []ReadinessDimensionKind{ReadinessDimensionRuntime}, "Project skill is incompatible", "repair the project skill before Start")
	}
	if !env.DaemonAlive {
		add("delivery-start-daemon", ReadinessBlockerRuntimeUnavailable, ReadinessAuthorityRuntime, []ReadinessDimensionKind{ReadinessDimensionRuntime}, "Resident daemon is off", "start the resident daemon before Start")
	}
	if !env.DaemonReconciling {
		add("delivery-start-daemon-reconciling", ReadinessBlockerRuntimeUnavailable, ReadinessAuthorityRuntime, []ReadinessDimensionKind{ReadinessDimensionRuntime}, "Resident daemon is not reconciling", "repair daemon reconciliation before Start")
	}
	if !env.RunnerCompatible {
		add("delivery-start-runner", ReadinessBlockerRuntimeUnavailable, ReadinessAuthorityRuntime, []ReadinessDimensionKind{ReadinessDimensionRuntime}, "Runner is incompatible", "configure a supported unattended runner before Start")
	}
	if !env.ApprovalFree {
		add("delivery-start-approval", ReadinessBlockerRuntimeUnavailable, ReadinessAuthorityRuntime, []ReadinessDimensionKind{ReadinessDimensionRuntime}, "Runner requires approval", "configure approval-free execution before Start")
	}
	if !env.IsolatedWorkspace {
		add("delivery-start-workspace", ReadinessBlockerRuntimeUnavailable, ReadinessAuthorityRuntime, []ReadinessDimensionKind{ReadinessDimensionRuntime}, "Workspace is shared", "select an isolated workspace strategy before Start")
	}
	if !env.IntegrationClean {
		add("delivery-start-integration", ReadinessBlockerIntegrationUnavailable, ReadinessAuthorityIntegration, []ReadinessDimensionKind{ReadinessDimensionRuntime}, "Integration lane is not clean", "repair the integration lane before Start")
	}
}

func deliveryReviewAddPreflightPhaseBlockers(r *deliveryReview, env wavePreflightEnvironment, report wavePreflightReport, projectID, waveID string) {
	deliveryReviewAddEnvironmentStartBlockers(r, env, projectID, waveID)
	if report.AuthorizationStale || report.Authorization == "disarmed" || report.Authorization == "stale" {
		deliveryReviewAddStartBlocker(r, deliveryReadinessBlocker("delivery-start-authorization", ReadinessBlockerAuthorizationMissing, ReadinessAuthorityAuthorization, []ReadinessDimensionKind{ReadinessDimensionAuthorization}, projectID, waveID, "Wave authorization is not current", "confirm the exact reviewed fingerprint with Start"))
	}
	// These are material validations of the existing held lineage, not runtime
	// observations. They must still make review/import fail closed.
	for _, name := range []string{"specDag", "taskContracts", "artifacts"} {
		if !report.Checks[name] {
			r.importIssues = append(r.importIssues, "held import material failed "+name+" validation")
		}
	}
}
