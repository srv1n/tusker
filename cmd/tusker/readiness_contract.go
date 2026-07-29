package main

import (
	"sort"
	"strings"
)

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

func NewReadinessContract(input ReadinessInput) (ReadinessContract, error) {
	contract := ReadinessContract{
		Schema:     ReadinessContractSchema,
		Version:    ReadinessContractVersion,
		Dimensions: input.Dimensions,
		Blockers:   cloneReadinessBlockers(input.Blockers),
	}
	if err := validateReadinessContract(contract); err != nil {
		return ReadinessContract{}, err
	}
	return contract, nil
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

// ProjectLegacyReadiness preserves old CLI and Serve field names from a
// caller-selected policy. It is a pure projection over the typed contract.
func ProjectLegacyReadiness(contract ReadinessContract, adapter ReadinessLegacyAdapter) (ReadinessLegacyProjection, error) {
	if !validReadinessDimensionKind(adapter.ReadinessDimension) {
		return ReadinessLegacyProjection{}, readinessContractError("legacy readiness adapter requires a readiness dimension")
	}
	dimensions := readinessDimensionsByKind(contract.Dimensions)
	state := dimensions[adapter.ReadinessDimension].State
	readiness := string(state)
	if mapped, ok := adapter.ReadinessByState[state]; ok {
		readiness = mapped
	}
	projection := ReadinessLegacyProjection{Readiness: readiness, Dispatchable: true, Blockers: []string{}}
	for _, kind := range adapter.DispatchabilityDimensions {
		if !validReadinessDimensionKind(kind) {
			return ReadinessLegacyProjection{}, readinessContractError("legacy readiness adapter has an invalid dispatchability dimension")
		}
		state := dimensions[kind].State
		if state != ReadinessStateReady && state != ReadinessStateNotApplicable {
			projection.Dispatchable = false
		}
	}
	for _, kind := range adapter.BlockerDimensions {
		if !validReadinessDimensionKind(kind) {
			return ReadinessLegacyProjection{}, readinessContractError("legacy readiness adapter has an invalid blocker dimension")
		}
		for _, blocker := range contract.Blockers {
			if readinessBlockerAffects(blocker, kind) {
				projection.Blockers = append(projection.Blockers, blocker.Reason)
			}
		}
	}
	projection.Blockers = uniqueStrings(projection.Blockers)
	sort.Strings(projection.Blockers)
	if projection.Blockers == nil {
		projection.Blockers = []string{}
	}
	return projection, nil
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

func NewHumanGateReadinessBlocker(id, taskID, gateID, reason, remedy string) ReadinessBlocker {
	return ReadinessBlocker{
		ID: id, Kind: ReadinessBlockerHumanGateOpen, Authority: ReadinessAuthorityHuman,
		Affects: []ReadinessDimensionKind{ReadinessDimensionInteractive}, TaskID: taskID,
		GateID: gateID, Reason: reason, Remedy: remedy,
	}
}

func validateReadinessContract(contract ReadinessContract) error {
	if contract.Schema != ReadinessContractSchema || contract.Version != ReadinessContractVersion {
		return readinessContractError("unsupported readiness contract version")
	}

	dimensions := readinessDimensionsByKind(contract.Dimensions)
	for kind, dimension := range dimensions {
		if !validReadinessState(dimension.State) {
			return readinessContractError("dimension " + string(kind) + " has an invalid state")
		}
		if strings.TrimSpace(dimension.Provenance.Source) == "" || strings.TrimSpace(dimension.Provenance.Revision) == "" {
			return readinessContractError("dimension " + string(kind) + " is missing provenance")
		}
	}

	blockerIDs := make(map[string]struct{}, len(contract.Blockers))
	blockedDimensions := map[ReadinessDimensionKind]bool{}
	for _, blocker := range contract.Blockers {
		if err := validateReadinessBlocker(blocker); err != nil {
			return err
		}
		if _, exists := blockerIDs[blocker.ID]; exists {
			return readinessContractError("duplicate blocker id " + blocker.ID)
		}
		blockerIDs[blocker.ID] = struct{}{}
		for _, kind := range blocker.Affects {
			blockedDimensions[kind] = true
		}
	}
	for kind, dimension := range dimensions {
		if (dimension.State == ReadinessStateBlocked || dimension.State == ReadinessStateWaiting || dimension.State == ReadinessStateUnavailable) && !blockedDimensions[kind] {
			return readinessContractError("dimension " + string(kind) + " is " + string(dimension.State) + " without a structured blocker")
		}
	}
	return nil
}

func validateReadinessBlocker(blocker ReadinessBlocker) error {
	if strings.TrimSpace(blocker.ID) == "" || !validReadinessBlockerKind(blocker.Kind) || !validReadinessAuthority(blocker.Authority) {
		return readinessContractError("blocker requires id, stable kind, and authority")
	}
	if len(blocker.Affects) == 0 {
		return readinessContractError("blocker " + blocker.ID + " must affect a readiness dimension")
	}
	seenDimensions := map[ReadinessDimensionKind]bool{}
	for _, kind := range blocker.Affects {
		if !validReadinessDimensionKind(kind) || seenDimensions[kind] {
			return readinessContractError("blocker " + blocker.ID + " has invalid affected dimensions")
		}
		seenDimensions[kind] = true
	}
	if strings.TrimSpace(blocker.TaskID) == "" && strings.TrimSpace(blocker.GateID) == "" && strings.TrimSpace(blocker.WaveID) == "" && strings.TrimSpace(blocker.ProjectID) == "" && strings.TrimSpace(blocker.IntegrationID) == "" {
		return readinessContractError("blocker " + blocker.ID + " must identify an affected task, gate, wave, project, or integration")
	}
	if !boundedReadinessText(blocker.Reason) || !boundedReadinessText(blocker.Remedy) {
		return readinessContractError("blocker " + blocker.ID + " requires bounded reason and remedy")
	}
	if blocker.Kind == ReadinessBlockerDependencyIncomplete && (strings.TrimSpace(blocker.TaskID) == "" || strings.TrimSpace(blocker.DependencyTaskID) == "") {
		return readinessContractError("dependency blocker " + blocker.ID + " requires task and dependency task ids")
	}
	if blocker.Kind == ReadinessBlockerHumanGateOpen && (blocker.Authority != ReadinessAuthorityHuman || strings.TrimSpace(blocker.GateID) == "") {
		return readinessContractError("human-gate blocker " + blocker.ID + " requires human authority and gate id")
	}
	return nil
}

func readinessContractError(message string) error {
	return tuskerError(errorReadinessContractInvalid, message)
}

func boundedReadinessText(value string) bool {
	value = strings.TrimSpace(value)
	return value != "" && len(value) <= 320
}

func readinessDimensionsByKind(dimensions ReadinessDimensions) map[ReadinessDimensionKind]ReadinessDimension {
	return map[ReadinessDimensionKind]ReadinessDimension{
		ReadinessDimensionContract:            dimensions.Contract,
		ReadinessDimensionImport:              dimensions.Import,
		ReadinessDimensionInteractive:         dimensions.Interactive,
		ReadinessDimensionAutomation:          dimensions.Automation,
		ReadinessDimensionAuthorization:       dimensions.Authorization,
		ReadinessDimensionRuntime:             dimensions.Runtime,
		ReadinessDimensionOptionalIntegration: dimensions.OptionalIntegration,
	}
}

func validReadinessState(state ReadinessState) bool {
	switch state {
	case ReadinessStateReady, ReadinessStateBlocked, ReadinessStateWaiting, ReadinessStateUnavailable, ReadinessStateNotApplicable:
		return true
	default:
		return false
	}
}

func validReadinessDimensionKind(kind ReadinessDimensionKind) bool {
	_, ok := readinessDimensionsByKind(ReadinessDimensions{})[kind]
	return ok
}

func validReadinessAuthority(authority ReadinessAuthorityDomain) bool {
	switch authority {
	case ReadinessAuthorityContract, ReadinessAuthorityImport, ReadinessAuthorityInteractive, ReadinessAuthorityAutomation, ReadinessAuthorityAuthorization, ReadinessAuthorityRuntime, ReadinessAuthorityIntegration, ReadinessAuthorityHuman:
		return true
	default:
		return false
	}
}

func validReadinessBlockerKind(kind ReadinessBlockerKind) bool {
	switch kind {
	case ReadinessBlockerContractInvalid, ReadinessBlockerImportMissing, ReadinessBlockerInteractiveOwner, ReadinessBlockerAutomationDisabled, ReadinessBlockerAuthorizationMissing, ReadinessBlockerRuntimeUnavailable, ReadinessBlockerOptionalIntegrationMissing, ReadinessBlockerDependencyIncomplete, ReadinessBlockerHumanGateOpen, ReadinessBlockerTaskNotReady, ReadinessBlockerTaskTerminal, ReadinessBlockerWorkspaceUnsafe, ReadinessBlockerOwnedPathConflict, ReadinessBlockerWorkRevisionStale, ReadinessBlockerIntegrationUnavailable:
		return true
	default:
		return false
	}
}

func cloneReadinessBlockers(blockers []ReadinessBlocker) []ReadinessBlocker {
	out := make([]ReadinessBlocker, len(blockers))
	copy(out, blockers)
	for index := range out {
		out[index].Affects = append([]ReadinessDimensionKind(nil), blockers[index].Affects...)
	}
	return out
}

// ReadinessBlockerIDs returns a deterministic, read-only lookup aid for
// consumers that need to correlate a dimension to its structured blockers.
func (contract ReadinessContract) ReadinessBlockerIDs(kind ReadinessDimensionKind) []string {
	var ids []string
	for _, blocker := range contract.Blockers {
		if readinessBlockerAffects(blocker, kind) {
			ids = append(ids, blocker.ID)
		}
	}
	sort.Strings(ids)
	return ids
}

func readinessBlockerAffects(blocker ReadinessBlocker, dimension ReadinessDimensionKind) bool {
	for _, affected := range blocker.Affects {
		if affected == dimension {
			return true
		}
	}
	return false
}
