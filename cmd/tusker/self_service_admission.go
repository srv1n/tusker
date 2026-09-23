package main

import (
	"strings"
	"time"
)

// AdmissionStage identifies the decision point evaluating eligibility. One
// set of facts feeds every stage; each stage applies its own predicate so
// authoring validity and current dispatch eligibility stay separate.
type AdmissionStage string

const (
	// AdmissionStageAuthoring checks contract quality before handoff.
	// Future dependencies may wait; authorization is inert.
	AdmissionStageAuthoring AdmissionStage = "authoring"
	// AdmissionStageTaskStart checks direct interactive work. Automation
	// enablement, daemon presence, and wave authorization are a separate
	// authority domain and cannot refuse it.
	AdmissionStageTaskStart AdmissionStage = "task_start"
	// AdmissionStageAutomationPlan checks what the automation plan reports.
	AdmissionStageAutomationPlan AdmissionStage = "automation_plan"
	// AdmissionStageDaemonDispatch checks what daemon candidate selection admits.
	AdmissionStageDaemonDispatch AdmissionStage = "daemon_dispatch"
	// AdmissionStageFinalClaim rechecks every fence at claim time.
	AdmissionStageFinalClaim AdmissionStage = "final_claim"
)

// AdmissionAuthority names the authorization backing a claim.
type AdmissionAuthority string

const (
	AdmissionAuthorityNone                AdmissionAuthority = "none"
	AdmissionAuthorityStandaloneDirective AdmissionAuthority = "standalone_directive"
	AdmissionAuthorityWaveArmed           AdmissionAuthority = "wave_armed"
)

// AdmissionFacts carries the read-only facts every admission stage reads.
// Construction performs no I/O; callers project vault, store, and runtime
// state into facts before evaluating.
type AdmissionFacts struct {
	TaskID    string
	Status    string
	Readiness string
	NextOwner string
	Lane      string
	WaveID    string
	ProjectID string

	ContractValid bool
	ContractStale bool
	RouteOK       bool
	ProofMapped   bool

	// OwnerFree is false when a live conflicting owner or lease holds the task.
	OwnerFree bool
	LiveOwner string

	DependenciesSatisfied bool
	BlockingDependency    string

	ProjectRegistered bool
	ProjectEnabled    bool

	Authority AdmissionAuthority
	// AuthorityMatches reports the backing authorization still matches the
	// exact current material (fingerprint, timestamp, lease generation).
	AuthorityMatches bool
	AuthorityStale   bool

	WavePaused bool
	// TaskStartOverride permits an explicit task-scoped start inside a paused
	// wave without resuming the wave.
	TaskStartOverride bool

	// OwnReservation reports the task holds its own queued directive bound to
	// the current authorization. A reservation is authority for its own
	// claim; it never disqualifies it.
	OwnReservation bool

	MaterialChanged bool
}

// AdmissionBlocker is one stage-specific refusal with its responsible actor
// and typed repair argv. Empty Repair means no supported repair exists.
type AdmissionBlocker struct {
	Code   string         `json:"code"`
	TaskID string         `json:"task_id"`
	Stage  AdmissionStage `json:"stage"`
	Actor  string         `json:"actor"`
	Repair []string       `json:"repair,omitempty"`
}

// AdmissionVerdict is the stage predicate outcome.
type AdmissionVerdict struct {
	Admit    bool               `json:"admit"`
	Blockers []AdmissionBlocker `json:"blockers"`
}

// Stable admission blocker codes shared by every stage.
const (
	AdmissionBlockerContractInvalid     = "contract_invalid"
	AdmissionBlockerContractStale       = "contract_stale"
	AdmissionBlockerRouteBlocked        = "route_blocked"
	AdmissionBlockerProofUnmapped       = "proof_unmapped"
	AdmissionBlockerTaskState           = "task_state"
	AdmissionBlockerDependencyWaiting   = "dependency_waiting"
	AdmissionBlockerOwnerHeld           = "owner_held"
	AdmissionBlockerProjectUnregistered = "project_unregistered"
	AdmissionBlockerProjectDisabled     = "project_disabled"
	AdmissionBlockerAuthorityMissing    = "authority_missing"
	AdmissionBlockerAuthorityStale      = "authority_stale"
	AdmissionBlockerWavePaused          = "wave_paused"
	AdmissionBlockerMaterialChanged     = "material_changed"
)

// EvaluateAdmissionForStage applies the stage predicate to shared facts.
// Facts are never mutated. Callers keep their existing authority domains:
// the evaluator reports, it does not authorize.
func EvaluateAdmissionForStage(facts AdmissionFacts, stage AdmissionStage) AdmissionVerdict {
	switch stage {
	case AdmissionStageAuthoring:
		return evaluateAuthoringAdmission(facts)
	case AdmissionStageTaskStart:
		return evaluateTaskStartAdmission(facts)
	case AdmissionStageAutomationPlan:
		return evaluateAutomationPlanAdmission(facts)
	case AdmissionStageDaemonDispatch:
		return evaluateDaemonDispatchAdmission(facts)
	case AdmissionStageFinalClaim:
		return evaluateFinalClaimAdmission(facts)
	default:
		return AdmissionVerdict{Blockers: []AdmissionBlocker{{
			Code: AdmissionBlockerContractInvalid, TaskID: facts.TaskID, Stage: stage,
			Actor: "operator",
		}}}
	}
}

func evaluateAuthoringAdmission(facts AdmissionFacts) AdmissionVerdict {
	verdict := AdmissionVerdict{Admit: true}
	if !facts.ContractValid {
		verdict.Blockers = append(verdict.Blockers, AdmissionBlocker{
			Code: AdmissionBlockerContractInvalid, TaskID: facts.TaskID, Stage: AdmissionStageAuthoring,
			Actor: "author", Repair: []string{"tusker", "task", "update", facts.TaskID, "--if-revision", "<state_rev>"},
		})
	}
	if !facts.RouteOK {
		verdict.Blockers = append(verdict.Blockers, AdmissionBlocker{
			Code: AdmissionBlockerRouteBlocked, TaskID: facts.TaskID, Stage: AdmissionStageAuthoring,
			Actor: "author",
		})
	}
	if !facts.ProofMapped {
		verdict.Blockers = append(verdict.Blockers, AdmissionBlocker{
			Code: AdmissionBlockerProofUnmapped, TaskID: facts.TaskID, Stage: AdmissionStageAuthoring,
			Actor: "author",
		})
	}
	// Authoring validity is structural: future dependencies wait at execution
	// time, and inert authorization never fails a contract check.
	verdict.Admit = len(verdict.Blockers) == 0
	return verdict
}

func evaluateTaskStartAdmission(facts AdmissionFacts) AdmissionVerdict {
	verdict := AdmissionVerdict{Admit: true}
	if facts.ContractStale || !facts.ContractValid {
		code := AdmissionBlockerContractInvalid
		if facts.ContractStale {
			code = AdmissionBlockerContractStale
		}
		verdict.Blockers = append(verdict.Blockers, AdmissionBlocker{
			Code: code, TaskID: facts.TaskID, Stage: AdmissionStageTaskStart, Actor: "agent",
		})
	}
	if !admissionLaneStateOK(facts.Status, facts.Lane) {
		verdict.Blockers = append(verdict.Blockers, AdmissionBlocker{
			Code: AdmissionBlockerTaskState, TaskID: facts.TaskID, Stage: AdmissionStageTaskStart, Actor: "agent",
		})
	}
	if !facts.DependenciesSatisfied {
		verdict.Blockers = append(verdict.Blockers, AdmissionBlocker{
			Code: AdmissionBlockerDependencyWaiting, TaskID: facts.TaskID, Stage: AdmissionStageTaskStart, Actor: "agent",
		})
	}
	if !facts.OwnerFree {
		verdict.Blockers = append(verdict.Blockers, AdmissionBlocker{
			Code: AdmissionBlockerOwnerHeld, TaskID: facts.TaskID, Stage: AdmissionStageTaskStart, Actor: firstNonEmpty(facts.LiveOwner, "agent"),
		})
	}
	// Documented interactive difference: background switch, offline daemon,
	// project enablement, and wave authorization live in a separate authority
	// domain and never refuse direct interactive work.
	verdict.Admit = len(verdict.Blockers) == 0
	return verdict
}

func evaluateAutomationPlanAdmission(facts AdmissionFacts) AdmissionVerdict {
	verdict := AdmissionVerdict{Admit: true}
	verdict.Blockers = append(verdict.Blockers, admissionContractFences(facts, AdmissionStageAutomationPlan, "agent")...)
	verdict.Blockers = append(verdict.Blockers, admissionProjectFences(facts, AdmissionStageAutomationPlan)...)
	verdict.Blockers = append(verdict.Blockers, admissionAuthorityFences(facts, AdmissionStageAutomationPlan)...)
	verdict.Admit = len(verdict.Blockers) == 0
	return verdict
}

func evaluateDaemonDispatchAdmission(facts AdmissionFacts) AdmissionVerdict {
	verdict := evaluateAutomationPlanAdmission(facts)
	for index := range verdict.Blockers {
		verdict.Blockers[index].Stage = AdmissionStageDaemonDispatch
	}
	if facts.WavePaused && !facts.TaskStartOverride {
		verdict.Blockers = append(verdict.Blockers, AdmissionBlocker{
			Code: AdmissionBlockerWavePaused, TaskID: facts.TaskID, Stage: AdmissionStageDaemonDispatch,
			Actor: "operator", Repair: []string{"tusker", "wave", "resume", facts.WaveID, "--by", "human:<name>"},
		})
	}
	if facts.MaterialChanged {
		verdict.Blockers = append(verdict.Blockers, AdmissionBlocker{
			Code: AdmissionBlockerMaterialChanged, TaskID: facts.TaskID, Stage: AdmissionStageDaemonDispatch,
			Actor: "operator",
		})
	}
	verdict.Admit = len(verdict.Blockers) == 0
	return verdict
}

func evaluateFinalClaimAdmission(facts AdmissionFacts) AdmissionVerdict {
	verdict := AdmissionVerdict{Admit: true}
	verdict.Blockers = append(verdict.Blockers, admissionContractFences(facts, AdmissionStageFinalClaim, "agent")...)
	if !facts.RouteOK {
		verdict.Blockers = append(verdict.Blockers, AdmissionBlocker{
			Code: AdmissionBlockerRouteBlocked, TaskID: facts.TaskID, Stage: AdmissionStageFinalClaim, Actor: "agent",
		})
	}
	if !facts.ProofMapped {
		verdict.Blockers = append(verdict.Blockers, AdmissionBlocker{
			Code: AdmissionBlockerProofUnmapped, TaskID: facts.TaskID, Stage: AdmissionStageFinalClaim, Actor: "agent",
		})
	}
	if !facts.OwnerFree {
		verdict.Blockers = append(verdict.Blockers, AdmissionBlocker{
			Code: AdmissionBlockerOwnerHeld, TaskID: facts.TaskID, Stage: AdmissionStageFinalClaim, Actor: firstNonEmpty(facts.LiveOwner, "agent"),
		})
	}
	if !facts.DependenciesSatisfied {
		verdict.Blockers = append(verdict.Blockers, AdmissionBlocker{
			Code: AdmissionBlockerDependencyWaiting, TaskID: facts.TaskID, Stage: AdmissionStageFinalClaim, Actor: "agent",
		})
	}
	verdict.Blockers = append(verdict.Blockers, admissionAuthorityFences(facts, AdmissionStageFinalClaim)...)
	if facts.MaterialChanged {
		verdict.Blockers = append(verdict.Blockers, AdmissionBlocker{
			Code: AdmissionBlockerMaterialChanged, TaskID: facts.TaskID, Stage: AdmissionStageFinalClaim, Actor: "operator",
		})
	}
	if !facts.ProjectRegistered {
		verdict.Blockers = append(verdict.Blockers, AdmissionBlocker{
			Code: AdmissionBlockerProjectUnregistered, TaskID: facts.TaskID, Stage: AdmissionStageFinalClaim, Actor: "operator",
		})
	}
	verdict.Admit = len(verdict.Blockers) == 0
	return verdict
}

func admissionContractFences(facts AdmissionFacts, stage AdmissionStage, actor string) []AdmissionBlocker {
	var blockers []AdmissionBlocker
	if facts.ContractStale || !facts.ContractValid {
		code := AdmissionBlockerContractInvalid
		if facts.ContractStale {
			code = AdmissionBlockerContractStale
		}
		blockers = append(blockers, AdmissionBlocker{
			Code: code, TaskID: facts.TaskID, Stage: stage, Actor: actor,
		})
	}
	if !admissionLaneStateOK(facts.Status, facts.Lane) {
		blockers = append(blockers, AdmissionBlocker{
			Code: AdmissionBlockerTaskState, TaskID: facts.TaskID, Stage: stage, Actor: actor,
		})
	}
	if !facts.DependenciesSatisfied {
		blockers = append(blockers, AdmissionBlocker{
			Code: AdmissionBlockerDependencyWaiting, TaskID: facts.TaskID, Stage: stage, Actor: actor,
		})
	}
	if !facts.OwnerFree {
		blockers = append(blockers, AdmissionBlocker{
			Code: AdmissionBlockerOwnerHeld, TaskID: facts.TaskID, Stage: stage, Actor: firstNonEmpty(facts.LiveOwner, actor),
		})
	}
	return blockers
}

func admissionProjectFences(facts AdmissionFacts, stage AdmissionStage) []AdmissionBlocker {
	var blockers []AdmissionBlocker
	if !facts.ProjectRegistered {
		blockers = append(blockers, AdmissionBlocker{
			Code: AdmissionBlockerProjectUnregistered, TaskID: facts.TaskID, Stage: stage, Actor: "operator",
		})
		return blockers
	}
	if !facts.ProjectEnabled {
		blockers = append(blockers, AdmissionBlocker{
			Code: AdmissionBlockerProjectDisabled, TaskID: facts.TaskID, Stage: stage,
			Actor: "operator", Repair: []string{"tusker", "projects", "enable", facts.ProjectID},
		})
	}
	return blockers
}

func admissionAuthorityFences(facts AdmissionFacts, stage AdmissionStage) []AdmissionBlocker {
	// A task holding its own current-authorization reservation is authorized
	// for its own claim. The reservation must never disqualify the claim it
	// exists to back.
	if facts.OwnReservation && facts.AuthorityMatches && !facts.AuthorityStale {
		return nil
	}
	if facts.Authority == AdmissionAuthorityNone {
		return []AdmissionBlocker{{
			Code: AdmissionBlockerAuthorityMissing, TaskID: facts.TaskID, Stage: stage, Actor: "operator",
		}}
	}
	if facts.AuthorityStale || !facts.AuthorityMatches {
		return []AdmissionBlocker{{
			Code: AdmissionBlockerAuthorityStale, TaskID: facts.TaskID, Stage: stage, Actor: "operator",
		}}
	}
	return nil
}

// admissionLaneStateOK mirrors the work-session lane rule: execute admits
// ready and rework, review admits review. Armed-wave promotion projects
// backlog members before this predicate runs, so projection and claim agree.
func admissionLaneStateOK(status, lane string) bool {
	status = strings.ToLower(strings.TrimSpace(status))
	if strings.TrimSpace(lane) == runLaneReview {
		return status == "review"
	}
	return status == "ready" || status == "rework"
}

// selfServiceReservationPromotion projects an armed-wave member with canonical
// backlog authoring to ready when its own current-authorization reservation
// backs it. A reservation is authority for its own claim: it releases its
// holder from the authored hold instead of disqualifying it.
//
// The gate is deliberately narrow. Promotion requires a live reservation
// bound to still-current wave authority over an unedited contract
// (runDirectiveMatchesTaskAuthority already refuses stale, superseded, and
// paused authority), a member with no blocking dependency and no human block,
// and canonical backlog/held authoring. Anything else keeps waiting, and
// promotion is an in-memory view: no vault, directive, run, or provider
// state is written.
func selfServiceReservationPromotion(vaultPath string, store *RuntimeStore, projectID string, task Note, now time.Time) (Note, bool) {
	if store == nil {
		return task, false
	}
	if strings.ToLower(strings.TrimSpace(stringField(task.Data, "status"))) != "backlog" {
		return task, false
	}
	switch strings.TrimSpace(stringField(task.Data, "readiness")) {
	case "held", "ready":
	default:
		return task, false
	}
	if strings.TrimSpace(stringField(task.Data, "wave")) == "" {
		return task, false
	}
	recordID := trackerRecordID(task)
	if recordID == "" {
		return task, false
	}
	directive, err := store.RunDirective(projectID, recordID)
	if err != nil || directive == nil {
		return task, false
	}
	if !runDirectiveMatchesTaskAuthority(vaultPath, task, directive, now) {
		return task, false
	}
	idx, err := loadV7Index(vaultPath)
	if err != nil {
		return task, false
	}
	if _, blocked := v7BlockingDependencyForReadiness(task, idx); blocked {
		return task, false
	}
	if armedWaveTaskHumanBlocked(idx, task) {
		return task, false
	}
	// Promotion releases the authored hold; it never overrides a human owner.
	// A member explicitly owned outside the agent lane keeps waiting for its
	// owner even when its reservation is current.
	switch owner := strings.TrimSpace(stringField(task.Data, "next_owner")); owner {
	case "agent", "":
	default:
		if !strings.HasPrefix(owner, "agent:") {
			return task, false
		}
	}
	projected := task
	data := cloneNoteData(task.Data)
	data["status"] = "ready"
	data["readiness"] = "ready"
	data["next_owner"] = "agent"
	data["state_rev"] = v7StateRev(data, task.Body)
	projected.Data = data
	return projected, true
}
