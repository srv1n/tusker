package main

import ()

const (
	ReadinessContractSchema  = "tusker.readiness/v1"
	ReadinessContractVersion = 1
)

// ReadinessState deliberately describes one readiness dimension. It is not a
// project-wide verdict: callers choose the dimensions relevant to their action.
type ReadinessState string

const (
	ReadinessStateReady         ReadinessState = "ready"
	ReadinessStateBlocked       ReadinessState = "blocked"
	ReadinessStateWaiting       ReadinessState = "waiting"
	ReadinessStateUnavailable   ReadinessState = "unavailable"
	ReadinessStateNotApplicable ReadinessState = "not_applicable"
)

type ReadinessDimensionKind string

const (
	ReadinessDimensionContract            ReadinessDimensionKind = "contract"
	ReadinessDimensionImport              ReadinessDimensionKind = "import"
	ReadinessDimensionInteractive         ReadinessDimensionKind = "interactive"
	ReadinessDimensionAutomation          ReadinessDimensionKind = "automation"
	ReadinessDimensionAuthorization       ReadinessDimensionKind = "authorization"
	ReadinessDimensionRuntime             ReadinessDimensionKind = "runtime"
	ReadinessDimensionOptionalIntegration ReadinessDimensionKind = "optional_integration"
)

type ReadinessAuthorityDomain string

const (
	ReadinessAuthorityContract      ReadinessAuthorityDomain = "contract"
	ReadinessAuthorityImport        ReadinessAuthorityDomain = "import"
	ReadinessAuthorityInteractive   ReadinessAuthorityDomain = "interactive"
	ReadinessAuthorityAutomation    ReadinessAuthorityDomain = "automation"
	ReadinessAuthorityAuthorization ReadinessAuthorityDomain = "authorization"
	ReadinessAuthorityRuntime       ReadinessAuthorityDomain = "runtime"
	ReadinessAuthorityIntegration   ReadinessAuthorityDomain = "integration"
	ReadinessAuthorityHuman         ReadinessAuthorityDomain = "human"
)

type ReadinessBlockerKind string

const (
	ReadinessBlockerContractInvalid            ReadinessBlockerKind = "contract_invalid"
	ReadinessBlockerImportMissing              ReadinessBlockerKind = "import_missing"
	ReadinessBlockerInteractiveOwner           ReadinessBlockerKind = "interactive_owner"
	ReadinessBlockerAutomationDisabled         ReadinessBlockerKind = "automation_disabled"
	ReadinessBlockerAuthorizationMissing       ReadinessBlockerKind = "authorization_missing"
	ReadinessBlockerRuntimeUnavailable         ReadinessBlockerKind = "runtime_unavailable"
	ReadinessBlockerOptionalIntegrationMissing ReadinessBlockerKind = "optional_integration_unavailable"
	ReadinessBlockerDependencyIncomplete       ReadinessBlockerKind = "dependency_incomplete"
	ReadinessBlockerHumanGateOpen              ReadinessBlockerKind = "human_gate_open"
	ReadinessBlockerTaskNotReady               ReadinessBlockerKind = "task_not_ready"
	ReadinessBlockerTaskTerminal               ReadinessBlockerKind = "task_terminal"
	ReadinessBlockerWorkspaceUnsafe            ReadinessBlockerKind = "workspace_unsafe"
	ReadinessBlockerOwnedPathConflict          ReadinessBlockerKind = "owned_path_conflict"
	ReadinessBlockerWorkRevisionStale          ReadinessBlockerKind = "work_revision_stale"
	ReadinessBlockerIntegrationUnavailable     ReadinessBlockerKind = "integration_unavailable"
)

// ReadinessProvenance tells consumers which read-only input produced a
// dimension. Revision is intentionally caller supplied: construction never
// reads or writes tracker, runtime, provider, Git, or account state.
type ReadinessProvenance struct {
	Source   string `json:"source"`
	Revision string `json:"revision"`
}

type ReadinessDimension struct {
	State      ReadinessState      `json:"state"`
	Provenance ReadinessProvenance `json:"provenance"`
}

type ReadinessDimensions struct {
	Contract            ReadinessDimension `json:"contract"`
	Import              ReadinessDimension `json:"import"`
	Interactive         ReadinessDimension `json:"interactive"`
	Automation          ReadinessDimension `json:"automation"`
	Authorization       ReadinessDimension `json:"authorization"`
	Runtime             ReadinessDimension `json:"runtime"`
	OptionalIntegration ReadinessDimension `json:"optional_integration"`
}

// ReadinessBlocker is a structured refusal. Reason and Remedy are bounded so
// the contract stays safe to expose through CLI and Serve projections.
type ReadinessBlocker struct {
	ID                string                   `json:"id"`
	Kind              ReadinessBlockerKind     `json:"kind"`
	Authority         ReadinessAuthorityDomain `json:"authority"`
	Affects           []ReadinessDimensionKind `json:"affects"`
	TaskID            string                   `json:"task_id,omitempty"`
	DependencyTaskID  string                   `json:"dependency_task_id,omitempty"`
	ConflictingTaskID string                   `json:"conflicting_task_id,omitempty"`
	Owner             string                   `json:"owner,omitempty"`
	GateID            string                   `json:"gate_id,omitempty"`
	WaveID            string                   `json:"wave_id,omitempty"`
	ProjectID         string                   `json:"project_id,omitempty"`
	IntegrationID     string                   `json:"integration_id,omitempty"`
	Reason            string                   `json:"reason"`
	Remedy            string                   `json:"remedy"`
}

// ReadinessLegacyProjection retains the existing CLI and Serve field names
// while those surfaces migrate. Its Dispatchable value is compatibility data,
// not a new all-purpose readiness decision.
type ReadinessLegacyProjection struct {
	Readiness    string   `json:"readiness"`
	Dispatchable bool     `json:"dispatchable"`
	Blockers     []string `json:"blockers"`
}

type ReadinessInput struct {
	Dimensions ReadinessDimensions `json:"dimensions"`
	Blockers   []ReadinessBlocker  `json:"blockers"`
}

// ReadinessContract is the versioned, read-only source of readiness facts.
// It intentionally has no Ready/ProjectReady/Dispatchable field: consumers
// must select the dimensions that authorize their specific operation.
type ReadinessContract struct {
	Schema     string              `json:"schema"`
	Version    int                 `json:"version"`
	Dimensions ReadinessDimensions `json:"dimensions"`
	Blockers   []ReadinessBlocker  `json:"blockers"`
}

// ReadinessLegacyAdapter explicitly selects the dimensions that drive legacy
// fields. This prevents old readiness/dispatchable consumers from silently
// treating every independent fact as one universal project verdict.
type ReadinessLegacyAdapter struct {
	ReadinessDimension        ReadinessDimensionKind    `json:"readiness_dimension"`
	ReadinessByState          map[ReadinessState]string `json:"readiness_by_state"`
	DispatchabilityDimensions []ReadinessDimensionKind  `json:"dispatchability_dimensions"`
	BlockerDimensions         []ReadinessDimensionKind  `json:"blocker_dimensions"`
}

// NewDependencyReadinessBlocker and NewHumanGateReadinessBlocker encode their
// causes from exact IDs. They never classify free-form reason text.
func NewDependencyReadinessBlocker(id, taskID, dependencyID, reason, remedy string) ReadinessBlocker {
	return ReadinessBlocker{
		ID: id, Kind: ReadinessBlockerDependencyIncomplete, Authority: ReadinessAuthorityContract,
		Affects: []ReadinessDimensionKind{ReadinessDimensionContract}, TaskID: taskID,
		DependencyTaskID: dependencyID, Reason: reason, Remedy: remedy,
	}
}
