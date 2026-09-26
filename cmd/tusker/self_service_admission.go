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
