package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	scheduledPromotionResourceLeaseTTL        = defaultResourceLeaseTTL
	scheduledPromotionAfterRefIntent          = func() error { return nil }
	scheduledPromotionAfterRefUpdate          = func() error { return nil }
	scheduledPromotionBeforeDefaultPrepare    = func() error { return nil }
	scheduledPromotionAfterDefaultPrepare     = func() error { return nil }
	scheduledPromotionAfterFinalAuthority     = func() error { return nil }
	scheduledPromotionAfterFailureIntent      = func() error { return nil }
	scheduledPromotionBeforeCommittedReplay   = func(context.Context) error { return nil }
	scheduledPromotionBeforeFailureCompletion = func() error { return nil }
	scheduledPromotionBeforeFullGateCommand   = func(string) {}
)

const (
	scheduledPromotionIntentPreRef    = "pre_ref"
	scheduledPromotionIntentCommitted = "committed"
	scheduledPromotionIntentDrifted   = "drifted"
)

// scheduledPromotionCandidateSnapshot is the immutable input contract for a
// promotion attempt.  It deliberately contains the facts that are otherwise
// easy to accidentally re-read after a long gate has completed.
type scheduledPromotionCandidateSnapshot struct {
	WaveID        string
	Candidate     DepartureCandidate
	Gate          DepartureGate
	DefaultBranch string
}

// scheduledPromotionAllowsDefaultAdvance preserves the legacy/manual path only
// for repositories that did not opt in. Once configured, the durable departure
// executor is the sole authority allowed to move the default branch.
func scheduledPromotionAllowsDefaultAdvance(vaultPath string) (bool, error) {
	// Older V7 repositories predate WORKFLOW.md.  Absence is the same legacy
	// opt-out as an absent scheduled_promotion stanza: preserve their current
	// explicit `tusker land` behaviour rather than turning an upgrade into a
	// new blocker.
	if !fileExists(workflowPath(vaultPath)) {
		return true, nil
	}
	wf, err := loadWorkflow(vaultPath)
	if err != nil {
		return false, err
	}
	policy := wf.Data.ScheduledPromotion.Effective
	if !policy.Configured {
		return true, nil
	}
	return false, nil
}

// stageScheduledTasks intentionally delegates to tusker land.  That keeps
// serialized staging, the isolated staging worktree, bisection, gate cache,
// and landing audit in one engine instead of growing a second "departure"
// merge path.  In stage mode the policy choke point above leaves main alone.
func stageScheduledTasks(vaultPath string, taskIDs []string, sourceSHAs map[string]string, actor string) error {
	return stageScheduledTasksWithAuthority(vaultPath, taskIDs, sourceSHAs, actor, nil)
}

func stageScheduledTasksWithAuthority(vaultPath string, taskIDs []string, sourceSHAs map[string]string, actor string, authority *v7LandingAuthority) error {
	wf, err := loadWorkflow(vaultPath)
	if err != nil {
		return err
	}
	if !wf.Data.ScheduledPromotion.Effective.Stage {
		return tuskerError(errorInvalidTransition, "scheduled staging refusal: mode "+wf.Data.ScheduledPromotion.Effective.Mode+" cannot stage reviewed work")
	}
	args := Args{"vault": vaultPath, "quiet": "true", "actor": actor}
	for i, id := range taskIDs {
		args[fmt.Sprintf("_pos%d", i)] = id
	}
	if authority == nil {
		return tuskerError(errorInvalidTransition, "scheduled staging refusal: daemon authority capability is required")
	}
	return landV7CmdWithDepartureAuthority(args, sourceSHAs, authority)
}

func scheduledPromotionGatePolicy(vaultPath string, wf Workflow) (GateTierPolicy, error) {
	policy := resolveGateTierPolicy(wf)
	data, _, err := parseFrontmatterMustRead(workflowPath(vaultPath))
	if err != nil {
		return GateTierPolicy{}, err
	}
	orchestration := mapStringAny(data["orchestration"])
	gate := mapStringAny(orchestration["gate"])
	batch := mapStringAny(orchestration["batch_gate"])
	declaredCommands := normalizeList(gate["harvest_commands"])
	if len(declaredCommands) == 0 {
		declaredCommands = normalizeList(batch["commands"])
	}
	if len(declaredCommands) == 0 {
		policy.HarvestCommands = backpressureCommands(vaultPath)
	} else {
		policy.HarvestCommands = declaredCommands
	}
	if strings.TrimSpace(stringField(gate, "profile")) == "" && strings.TrimSpace(stringField(batch, "feature_profile")) == "" {
		policy.Profile = ""
	}
	return policy, nil
}

func scheduledPromotionSnapshot(vaultPath, projectID, waveID string, wf Workflow) (scheduledPromotionCandidateSnapshot, error) {
	return scheduledPromotionSnapshotWithStore(vaultPath, projectID, waveID, wf, nil)
}

func scheduledPromotionSnapshotWithStore(vaultPath, projectID, waveID string, wf Workflow, trustedStore *RuntimeStore) (scheduledPromotionCandidateSnapshot, error) {
	if strings.TrimSpace(projectID) == "" {
		return scheduledPromotionCandidateSnapshot{}, tuskerError(errorInvalidArg, "scheduled promotion requires a project identity")
	}
	idx, err := loadV7Index(vaultPath)
	if err != nil {
		return scheduledPromotionCandidateSnapshot{}, err
	}
	wave, ok := idx.Waves[waveID]
	if !ok {
		return scheduledPromotionCandidateSnapshot{}, tuskerError(errorNotFound, "V7 wave not found: "+waveID)
	}
	repoRoot := v7RepoRoot(vaultPath)
	integrationBranch := v7WaveIntegrationBranch(wave)
	candidateSHA, err := gitOutputTrim(repoRoot, "rev-parse", integrationBranch)
	if err != nil {
		return scheduledPromotionCandidateSnapshot{}, tuskerError(errorInvalidTransition, "promotion candidate is unavailable: "+integrationBranch)
	}
	treeHash, err := gitOutputTrim(repoRoot, "rev-parse", candidateSHA+"^{tree}")
	if err != nil {
		return scheduledPromotionCandidateSnapshot{}, err
	}
	defaultBranch := firstNonEmpty(strings.TrimSpace(wf.Orchestration.DefaultBranch), v7DefaultBranch(vaultPath))
	defaultSHA, err := gitOutputTrim(repoRoot, "rev-parse", defaultBranch)
	if err != nil {
		return scheduledPromotionCandidateSnapshot{}, err
	}
	candidate := DepartureCandidate{
		WaveIDs:                  []string{waveID},
		TaskStateRevisions:       map[string]string{},
		TaskSourceSHAs:           map[string]string{},
		IntegrationBaseSHA:       candidateSHA,
		CandidateSHA:             candidateSHA,
		CandidateTreeHash:        treeHash,
		ExpectedDefaultBranchSHA: defaultSHA,
	}
	members := normalizeList(wave.Data["members"])
	implicitSingleton := v7ImplicitDeliveryUnit(wave)
	deliveryTask := ""
	if implicitSingleton {
		deliveryTask = strings.ToUpper(strings.TrimSpace(stringField(wave.Data, "delivery_task")))
		if len(members) != 1 || deliveryTask == "" || members[0] != deliveryTask {
			return scheduledPromotionCandidateSnapshot{}, tuskerError(errorInvalidTransition, "promotion candidate refusal: implicit_singleton_membership:"+waveID)
		}
		if boolFromAny(wave.Data["release_authorized"]) {
			return scheduledPromotionCandidateSnapshot{}, tuskerError(errorInvalidTransition, "promotion candidate refusal: implicit_singleton_release_authority:"+waveID)
		}
	}
	memberFacts := make([]string, 0, len(members))
	for _, id := range members {
		task, ok := idx.Tasks[id]
		if !ok {
			return scheduledPromotionCandidateSnapshot{}, tuskerError(errorInvalidTransition, "promotion candidate drift: task_missing:"+id)
		}
		assignment := strings.TrimSpace(stringField(task.Data, "wave"))
		if (!implicitSingleton && assignment != waveID) ||
			(implicitSingleton && assignment != "" && assignment != waveID) {
			return scheduledPromotionCandidateSnapshot{}, tuskerError(errorInvalidTransition, "promotion candidate refusal: wave_member_assignment_mismatch:"+id)
		}
		if err := scheduledPromotionTaskAcceptedReview(vaultPath, task); err != nil {
			return scheduledPromotionCandidateSnapshot{}, err
		}
		stateRev := stringField(task.Data, "state_rev")
		sourceSHA, err := scheduledPromotionTaskSourceSHAWithStore(repoRoot, candidateSHA, integrationBranch, wave, task, trustedStore)
		if err != nil {
			return scheduledPromotionCandidateSnapshot{}, err
		}
		candidate.TaskStateRevisions[id] = stateRev
		candidate.TaskSourceSHAs[id] = sourceSHA
		candidate.CargoTaskIDs = append(candidate.CargoTaskIDs, id)
		memberFacts = append(memberFacts, id+"@"+stateRev+"@"+sourceSHA)
	}
	candidate.CargoTaskIDs = uniqueDepartureStrings(candidate.CargoTaskIDs)
	sort.Strings(memberFacts)
	if implicitSingleton {
		task := idx.Tasks[deliveryTask]
		if strings.ToLower(stringField(task.Data, "status")) != "done" ||
			candidate.TaskSourceSHAs[deliveryTask] == "" {
			return scheduledPromotionCandidateSnapshot{}, tuskerError(errorInvalidTransition, "promotion candidate refusal: implicit_singleton_not_finished:"+deliveryTask)
		}
		// An implicit delivery unit is intentionally disarmed because it is not
		// dispatch authority. Its landing authority is the exact finished task
		// provenance plus the repository's explicit promote policy.
		candidate.WaveAuthorization = departureFingerprint(append([]string{
			projectID,
			waveID,
			v7ImplicitSingletonDeliveryUnit,
			deliveryTask,
			stringField(wave.Data, "delivery_source_state_rev"),
			stringField(wave.Data, "execution_provenance"),
			wf.ScheduledPromotion.Effective.Mode,
		}, memberFacts...)...)
	} else {
		auth := waveAuthorizationProjection(vaultPath, idx, wave)
		if stringField(auth, "state") != "armed" || boolFromAny(auth["stale"]) {
			return scheduledPromotionCandidateSnapshot{}, tuskerError(errorInvalidTransition, "promotion candidate refusal: wave_authorization_not_armed:"+waveID)
		}
		candidate.WaveAuthorization = departureFingerprint(append([]string{projectID, waveID, stringField(auth, "state"), stringField(auth, "fingerprint")}, memberFacts...)...)
	}
	gatePolicy, err := scheduledPromotionGatePolicy(vaultPath, wf)
	if err != nil {
		return scheduledPromotionCandidateSnapshot{}, err
	}
	gate := DepartureGate{
		Command:  strings.Join(gatePolicy.HarvestCommands, " && "),
		Profile:  gatePolicy.Profile,
		TreeHash: treeHash,
		Status:   "required",
	}
	if gate.Command == "" {
		return scheduledPromotionCandidateSnapshot{}, tuskerError(errorInvalidTransition, "promotion candidate refusal: full_gate_missing")
	}
	providerStateRoot := DefaultStateRoot()
	if trustedStore != nil && strings.TrimSpace(trustedStore.stateRoot) != "" {
		providerStateRoot = trustedStore.stateRoot
	}
	gate.Toolchain = scheduledPromotionFullGateToolchainFingerprint(repoRoot, gatePolicy.HarvestCommands, gatePolicy.IsolationProvider, providerStateRoot)
	return scheduledPromotionCandidateSnapshot{WaveID: waveID, Candidate: candidate, Gate: gate, DefaultBranch: defaultBranch}, nil
}

func scheduledPromotionTaskSourceSHA(repoRoot, candidateSHA, integrationBranch string, wave, task Note) (string, error) {
	return scheduledPromotionTaskSourceSHAWithStore(repoRoot, candidateSHA, integrationBranch, wave, task, nil)
}

func scheduledPromotionTaskSourceSHAWithStore(repoRoot, candidateSHA, integrationBranch string, wave, task Note, trustedStore *RuntimeStore) (string, error) {
	sourceSHA, err := scheduledPromotionExactTaskSourceSHAWithStore(repoRoot, integrationBranch, wave, task, gitRevParse, trustedStore)
	if err != nil {
		return "", err
	}
	taskID := stringField(task.Data, "id")
	if !gitMergeBaseAncestor(repoRoot, sourceSHA, candidateSHA) {
		return "", tuskerError(errorInvalidTransition, "promotion candidate refusal: task_source_not_integrated:"+taskID)
	}
	return sourceSHA, nil
}

func validateDepartureDurableCargo(vaultPath string, candidate DepartureCandidate) error {
	return validateDepartureDurableCargoWithStore(vaultPath, candidate, nil)
}

func validateDepartureDurableCargoWithStore(vaultPath string, candidate DepartureCandidate, trustedStore *RuntimeStore) error {
	idx, err := loadV7Index(vaultPath)
	if err != nil {
		return err
	}
	return validateDepartureDurableCargoIndexWithStore(vaultPath, idx, candidate, trustedStore)
}

func validateDepartureDurableCargoIndex(vaultPath string, idx v7Index, candidate DepartureCandidate) error {
	return validateDepartureDurableCargoIndexWithStore(vaultPath, idx, candidate, nil)
}

func validateDepartureDurableCargoIndexWithStore(vaultPath string, idx v7Index, candidate DepartureCandidate, trustedStore *RuntimeStore) error {
	cargo := uniqueDepartureStrings(normalizeList(candidate.CargoTaskIDs))
	if len(cargo) == 0 {
		return tuskerError(errorInvalidTransition, "departure recompute required: cargo_missing")
	}
	expectedWaves := uniqueDepartureStrings(normalizeList(candidate.WaveIDs))
	implicitWaveByTask := map[string]string{}
	for _, waveID := range expectedWaves {
		wave, ok := idx.Waves[waveID]
		if !ok || !v7ImplicitDeliveryUnit(wave) {
			continue
		}
		members := uniqueDepartureStrings(normalizeList(wave.Data["members"]))
		deliveryTask := strings.ToUpper(strings.TrimSpace(stringField(wave.Data, "delivery_task")))
		if len(members) == 1 && deliveryTask != "" && members[0] == deliveryTask {
			implicitWaveByTask[deliveryTask] = waveID
		}
	}
	cargoByWave := map[string][]string{}
	for _, taskID := range cargo {
		task, ok := idx.Tasks[taskID]
		if !ok {
			return tuskerError(errorInvalidTransition, "departure recompute required: task_missing:"+taskID)
		}
		waveID := strings.TrimSpace(stringField(task.Data, "wave"))
		if waveID == "" {
			waveID = implicitWaveByTask[taskID]
		}
		cargoByWave[waveID] = append(cargoByWave[waveID], taskID)
	}
	actualWaves := make([]string, 0, len(cargoByWave))
	for waveID := range cargoByWave {
		if waveID != "" {
			actualWaves = append(actualWaves, waveID)
		}
	}
	sort.Strings(expectedWaves)
	sort.Strings(actualWaves)
	if !sameDepartureStrings(expectedWaves, actualWaves) {
		return tuskerError(errorInvalidTransition, "departure recompute required: wave_membership_drift")
	}

	repoRoot := v7RepoRoot(vaultPath)
	validationWaves := append([]string(nil), actualWaves...)
	if _, ok := cargoByWave[""]; ok {
		validationWaves = append([]string{""}, validationWaves...)
	}
	for _, waveID := range validationWaves {
		taskIDs := cargoByWave[waveID]
		wave := Note{}
		integrationBranch := ""
		if waveID != "" {
			var ok bool
			wave, ok = idx.Waves[waveID]
			if !ok {
				return tuskerError(errorInvalidTransition, "departure recompute required: wave_missing:"+waveID)
			}
			members := uniqueDepartureStrings(normalizeList(wave.Data["members"]))
			sort.Strings(members)
			sort.Strings(taskIDs)
			if len(members) == 0 || !sameDepartureStrings(members, taskIDs) {
				return tuskerError(errorInvalidTransition, "departure recompute required: wave_membership_drift:"+waveID)
			}
			integrationBranch = v7WaveIntegrationBranch(wave)
		}
		for _, taskID := range taskIDs {
			task := idx.Tasks[taskID]
			if err := scheduledPromotionTaskAcceptedReview(vaultPath, task); err != nil {
				return err
			}
			if expected := candidate.TaskStateRevisions[taskID]; expected == "" || expected != stringField(task.Data, "state_rev") {
				return tuskerError(errorInvalidTransition, "departure recompute required: task_state_drift:"+taskID)
			}
			expectedSource := strings.TrimSpace(candidate.TaskSourceSHAs[taskID])
			resolvedExpected, ok := gitRevParse(repoRoot, expectedSource+"^{commit}")
			if !ok {
				return tuskerError(errorInvalidTransition, "departure recompute required: task_source_unavailable:"+taskID+": durable task source must name an available full immutable commit SHA")
			}
			if expectedSource == "" || !strings.EqualFold(expectedSource, resolvedExpected) {
				return tuskerError(errorInvalidTransition, "departure recompute required: task_source_not_immutable:"+taskID+": durable task source must be a full immutable commit SHA")
			}
			sourceSHA, err := scheduledPromotionExactTaskSourceSHAWithStore(repoRoot, integrationBranch, wave, task, gitRevParse, trustedStore)
			if err != nil {
				return err
			}
			if !strings.EqualFold(expectedSource, sourceSHA) {
				return tuskerError(errorInvalidTransition, "departure recompute required: task_source_drift:"+taskID)
			}
		}
	}
	return nil
}

func validateScheduledPromotionDurableCargo(vaultPath, waveID string, candidate DepartureCandidate) error {
	return validateScheduledPromotionDurableCargoWithStore(vaultPath, waveID, candidate, nil)
}

func validateScheduledPromotionDurableCargoWithStore(vaultPath, waveID string, candidate DepartureCandidate, trustedStore *RuntimeStore) error {
	idx, err := loadV7Index(vaultPath)
	if err != nil {
		return err
	}
	return validateScheduledPromotionDurableCargoIndexWithStore(vaultPath, idx, waveID, candidate, trustedStore)
}

func validateScheduledPromotionDurableCargoIndex(vaultPath string, idx v7Index, waveID string, candidate DepartureCandidate) error {
	return validateScheduledPromotionDurableCargoIndexWithStore(vaultPath, idx, waveID, candidate, nil)
}

func validateScheduledPromotionDurableCargoIndexWithStore(vaultPath string, idx v7Index, waveID string, candidate DepartureCandidate, trustedStore *RuntimeStore) error {
	if !sameDepartureStrings(uniqueDepartureStrings(normalizeList(candidate.WaveIDs)), []string{waveID}) {
		return tuskerError(errorInvalidTransition, "promotion recompute required: wave_membership_drift")
	}
	if err := validateDepartureDurableCargoIndexWithStore(vaultPath, idx, candidate, trustedStore); err != nil {
		return err
	}
	repoRoot := v7RepoRoot(vaultPath)
	for _, taskID := range candidate.CargoTaskIDs {
		sourceSHA := strings.TrimSpace(candidate.TaskSourceSHAs[taskID])
		if sourceSHA == "" || !gitMergeBaseAncestor(repoRoot, sourceSHA, candidate.CandidateSHA) {
			return tuskerError(errorInvalidTransition, "promotion candidate refusal: task_source_not_integrated:"+taskID)
		}
	}
	return nil
}

func validateScheduledPromotionWaveAuthority(vaultPath, waveID string) error {
	return validateScheduledPromotionWaveAuthorityWithStore(vaultPath, waveID, nil)
}

func validateScheduledPromotionWaveAuthorityWithStore(vaultPath, waveID string, trustedStore *RuntimeStore) error {
	idx, err := loadV7Index(vaultPath)
	if err != nil {
		return err
	}
	return validateScheduledPromotionWaveAuthorityIndexWithStore(vaultPath, idx, waveID, trustedStore)
}

func validateScheduledPromotionWaveAuthorityIndex(vaultPath string, idx v7Index, waveID string) error {
	return validateScheduledPromotionWaveAuthorityIndexWithStore(vaultPath, idx, waveID, nil)
}

func validateScheduledPromotionWaveAuthorityIndexWithStore(vaultPath string, idx v7Index, waveID string, trustedStore *RuntimeStore) error {
	wave, ok := idx.Waves[waveID]
	if !ok {
		return tuskerError(errorNotFound, "V7 wave not found: "+waveID)
	}
	members := uniqueDepartureStrings(normalizeList(wave.Data["members"]))
	if len(members) == 0 {
		return tuskerError(errorInvalidTransition, "promotion candidate refusal: wave_members_missing:"+waveID)
	}
	implicitSingleton := v7ImplicitDeliveryUnit(wave)
	if implicitSingleton {
		deliveryTask := strings.ToUpper(strings.TrimSpace(stringField(wave.Data, "delivery_task")))
		if len(members) != 1 || deliveryTask == "" || members[0] != deliveryTask {
			return tuskerError(errorInvalidTransition, "promotion candidate refusal: implicit_singleton_membership:"+waveID)
		}
		if boolFromAny(wave.Data["release_authorized"]) {
			return tuskerError(errorInvalidTransition, "promotion candidate refusal: implicit_singleton_release_authority:"+waveID)
		}
	} else {
		auth := waveAuthorizationProjection(vaultPath, idx, wave)
		if stringField(auth, "state") != "armed" || boolFromAny(auth["stale"]) {
			return tuskerError(errorInvalidTransition, "promotion candidate refusal: wave_authorization_not_armed:"+waveID)
		}
	}
	repoRoot := v7RepoRoot(vaultPath)
	integrationBranch := v7WaveIntegrationBranch(wave)
	for _, taskID := range members {
		task, ok := idx.Tasks[taskID]
		if !ok {
			return tuskerError(errorInvalidTransition, "promotion candidate refusal: task_missing:"+taskID)
		}
		assignment := strings.TrimSpace(stringField(task.Data, "wave"))
		if (!implicitSingleton && assignment != waveID) ||
			(implicitSingleton && assignment != "" && assignment != waveID) {
			return tuskerError(errorInvalidTransition, "promotion candidate refusal: wave_member_assignment_mismatch:"+taskID)
		}
		if err := scheduledPromotionTaskAcceptedReview(vaultPath, task); err != nil {
			return err
		}
		if _, err := scheduledPromotionExactTaskSourceSHAWithStore(repoRoot, integrationBranch, wave, task, gitRevParse, trustedStore); err != nil {
			return err
		}
	}
	return nil
}

func validateScheduledPromotionPreparedAuthority(vaultPath, waveID string, candidate DepartureCandidate, preparation *v7WaveMemberPreparation) error {
	return validateScheduledPromotionPreparedAuthorityWithStore(vaultPath, waveID, candidate, preparation, nil)
}

func validateScheduledPromotionPreparedAuthorityWithStore(vaultPath, waveID string, candidate DepartureCandidate, preparation *v7WaveMemberPreparation, trustedStore *RuntimeStore) error {
	idx, changed, err := scheduledPromotionPreparedAuthorityIndex(vaultPath, preparation)
	if err != nil {
		return err
	}
	if err := validateScheduledPromotionWaveAuthorityIndexWithStore(vaultPath, idx, waveID, trustedStore); err != nil {
		return err
	}
	if err := validateScheduledPromotionDurableCargoIndexWithStore(vaultPath, idx, waveID, candidate, trustedStore); err != nil {
		return err
	}
	if changed {
		return tuskerError(errorInvalidTransition, "promotion recompute required: wave_control_changed_after_default_prepare")
	}
	return nil
}

// Default-advance preparation temporarily replaces canonical task/wave files
// with their checked-in representations. For final authority checks, an
// unchanged prepared file still means its captured live preimage; a file
// changed after preparation is a concurrent operator write and must win.
func scheduledPromotionPreparedAuthorityIndex(vaultPath string, preparation *v7WaveMemberPreparation) (v7Index, bool, error) {
	if preparation == nil || preparation.finished {
		return v7Index{}, false, tuskerError(errorInvalidTransition, "promotion refusal: default preparation authority is unavailable")
	}
	idx, err := loadV7Index(vaultPath)
	if err != nil {
		return v7Index{}, false, err
	}
	repoRoot := v7RepoRoot(vaultPath)
	vaultRelative, err := filepath.Rel(repoRoot, vaultPath)
	if err != nil || filepath.IsAbs(vaultRelative) || strings.HasPrefix(filepath.Clean(vaultRelative), "..") {
		return v7Index{}, false, tuskerError(errorInvalidTransition, "promotion refusal: canonical vault path is unavailable after default preparation")
	}
	changed := false
	for _, path := range preparation.paths {
		indexState, tracked, stateErr := v7PreparedIndexState(path.WorkDir, path.Relative)
		if stateErr != nil {
			return v7Index{}, false, stateErr
		}
		if !tracked {
			indexState = v7PreparedWaveMemberState{}
		}
		if !sameV7PreparedWaveMemberIndexState(indexState, path.PreparedIndex) {
			return v7Index{}, false, tuskerError(errorInvalidTransition, "promotion recompute required: wave_control_index_changed_after_default_prepare")
		}
		current, stateErr := v7PreparedWorktreeState(path.Absolute)
		if stateErr != nil {
			return v7Index{}, false, stateErr
		}
		authority := path.Before
		if !sameV7PreparedWaveMemberExactState(current, path.PreparedWorktree) {
			authority = current
			changed = true
		}
		// Git reports physical worktree paths on macOS (for example,
		// /private/var/...) while callers may retain their equivalent logical
		// path (/var/...). Compare resolved worktree roots, then derive the
		// control path from Git's repo-relative identity.
		if !workspacePathsCompatible(path.WorkDir, repoRoot) {
			continue
		}
		relative, relErr := filepath.Rel(filepath.FromSlash(vaultRelative), filepath.FromSlash(path.Relative))
		if relErr != nil || filepath.IsAbs(relative) || strings.HasPrefix(filepath.Clean(relative), "..") {
			return v7Index{}, false, tuskerError(errorInvalidTransition, "promotion recompute required: unexpected_wave_control_path_after_default_prepare:"+path.Relative)
		}
		if !authority.Exists {
			return v7Index{}, false, tuskerError(errorInvalidTransition, "promotion recompute required: wave_control_missing_after_default_prepare")
		}
		data, body, parseErr := parseFrontmatter(string(authority.Bytes))
		if parseErr != nil {
			return v7Index{}, false, parseErr
		}
		id := strings.TrimSuffix(filepath.Base(relative), filepath.Ext(relative))
		kind := strings.TrimSuffix(filepath.Base(filepath.Dir(relative)), "s")
		if effectiveV7Kind(data) != kind || stringField(data, "id") != id {
			return v7Index{}, false, tuskerError(errorInvalidTransition, "promotion recompute required: wave_control_identity_changed_after_default_prepare:"+id)
		}
		note := Note{
			AbsolutePath: filepath.Join(vaultPath, relative),
			RelativePath: filepath.ToSlash(relative),
			Data:         data,
			Body:         body,
		}
		switch kind {
		case "task":
			idx.Tasks[id] = note
		case "wave":
			idx.Waves[id] = note
		default:
			return v7Index{}, false, tuskerError(errorInvalidTransition, "promotion recompute required: unexpected_wave_control_kind:"+kind)
		}
	}
	return idx, changed, nil
}

// scheduledPromotionAdvanceRefUnderMaterialEpoch is the linearization point
// between mutable control authority and the irreversible default-ref CAS.
// Managed task, proof, and authorization writers acquire the same epoch first,
// so they are observed by the pre-prepare validation or happen after the ref.
func scheduledPromotionAdvanceRefUnderMaterialEpoch(
	ctx context.Context,
	vaultPath, projectID, waveID string,
	store *RuntimeStore,
	lease ResourceLease,
	candidate DepartureCandidate,
	wave Note,
	defaultBranch, expectedSHA, intendedSHA string,
	recovery bool,
) (checkouts []v7Worktree, err error) {
	materialLock, err := acquireV7MaterialEpochLock(vaultPath)
	if err != nil {
		return nil, err
	}
	defer func() {
		err = errors.Join(err, materialLock.Close())
	}()

	if err := validateScheduledPromotionWaveAuthorityWithStore(vaultPath, waveID, store); err != nil {
		return nil, err
	}
	if err := validateScheduledPromotionDurableCargoWithStore(vaultPath, waveID, candidate, store); err != nil {
		return nil, err
	}
	repoRoot := v7RepoRoot(vaultPath)
	preparation, err := prepareV7WaveMembersForDefaultAdvance(repoRoot, vaultPath, defaultBranch, wave)
	if err != nil {
		return nil, err
	}
	restore := func(cause error) error {
		return errors.Join(cause, preparation.finishAfterRefAttempt(repoRoot, defaultBranch, expectedSHA, intendedSHA))
	}
	if err := scheduledPromotionAfterDefaultPrepare(); err != nil {
		return nil, restore(err)
	}
	if hold, err := store.departureHold(projectID, false); err != nil {
		return nil, restore(err)
	} else if hold != nil {
		return nil, restore(departureHoldError(hold))
	}
	if matched, err := store.ResourceLeaseMatches(lease.Name, lease.Owner, lease.Generation); err != nil || !matched {
		return nil, restore(tuskerError(errorInvalidTransition, "promotion refusal: full_gate_lease_fenced_at_ref_update"))
	}
	if err := ctx.Err(); err != nil {
		return nil, restore(err)
	}
	if err := validateScheduledPromotionPreparedAuthorityWithStore(vaultPath, waveID, candidate, preparation, store); err != nil {
		return nil, restore(err)
	}
	if err := scheduledPromotionAfterFinalAuthority(); err != nil {
		return nil, restore(err)
	}
	checkouts, err = advanceV7DefaultBranchRef(repoRoot, defaultBranch, intendedSHA, expectedSHA)
	if err != nil {
		message := "promotion refusal: default_ref_drift: " + err.Error()
		if recovery {
			message = "promotion recovery blocked: default_ref_drift: " + err.Error()
		}
		return nil, restore(tuskerError(errorInvalidTransition, message))
	}
	preparation.commit()
	return checkouts, nil
}

func scheduledPromotionTaskAcceptedReview(vaultPath string, task Note) error {
	taskID := stringField(task.Data, "id")
	if strings.ToLower(strings.TrimSpace(stringField(task.Data, "status"))) != "done" {
		return tuskerError(errorInvalidTransition, "promotion candidate refusal: wave_member_not_done:"+taskID)
	}
	proofStatus := strings.ToLower(strings.TrimSpace(stringField(task.Data, "proof_status")))
	acceptedBy := strings.TrimSpace(stringField(task.Data, "accepted_by"))
	if (proofStatus != "satisfied" && proofStatus != "waived") ||
		acceptedBy == "" ||
		strings.TrimSpace(stringField(task.Data, "accepted_at")) == "" ||
		strings.TrimSpace(stringField(task.Data, "closed_at")) == "" {
		return tuskerError(errorInvalidTransition, "promotion candidate refusal: task_review_provenance_missing:"+taskID)
	}
	policy, err := v7ClosePolicyFor(vaultPath, strings.ToLower(fallback(stringField(task.Data, "risk"), "medium")))
	if err != nil {
		return err
	}
	if !v7CloseAcceptorAllowed(acceptedBy, policy.RequiredAcceptor) {
		return tuskerError(errorInvalidTransition, "promotion candidate refusal: task_review_acceptor_invalid:"+taskID)
	}
	return nil
}

func scheduledPromotionExactTaskSourceSHA(repoRoot, integrationBranch string, wave, task Note, resolve func(string, string) (string, bool)) (string, error) {
	return scheduledPromotionExactTaskSourceSHAWithStore(repoRoot, integrationBranch, wave, task, resolve, nil)
}

func scheduledPromotionExactTaskSourceSHAWithStore(repoRoot, integrationBranch string, wave, task Note, resolve func(string, string) (string, bool), trustedStore *RuntimeStore) (string, error) {
	taskID := stringField(task.Data, "id")
	sourceSHA := firstNonEmpty(stringField(task.Data, "source_sha"), stringField(task.Data, "source_commit"), stringField(task.Data, "source_branch_sha"))
	if sourceSHA == "" {
		if exact, ok := authenticatedV7LandingAuditSourceWithStore(repoRoot, integrationBranch, wave, taskID, resolve, trustedStore); ok {
			sourceSHA = exact
		}
	}
	if sourceSHA == "" {
		return "", tuskerError(errorInvalidTransition, "promotion candidate refusal: task_source_provenance_missing:"+taskID)
	}
	if resolve == nil {
		resolve = gitRevParse
	}
	resolved, ok := resolve(repoRoot, sourceSHA+"^{commit}")
	if !ok {
		return "", tuskerError(errorInvalidTransition, "promotion candidate refusal: task_source_unavailable:"+taskID)
	}
	if !strings.EqualFold(strings.TrimSpace(sourceSHA), strings.TrimSpace(resolved)) {
		return "", tuskerError(errorInvalidTransition, "promotion candidate refusal: task_source_not_immutable:"+taskID+": source provenance must be a full immutable commit SHA")
	}
	return resolved, nil
}

// authenticatedV7LandingAuditSource accepts Markdown only as an index into a
// user-global Tusker receipt. The receipt names the exact bounded batch
// segment, direct merge parents, task-owned source, gate/toolchain facts, and
// final tree. Recovery never searches historical Git for a plausible merge.
func authenticatedV7LandingAuditSource(repoRoot, integrationBranch string, wave Note, taskID string, resolve func(string, string) (string, bool)) (string, bool) {
	return authenticatedV7LandingAuditSourceWithStore(repoRoot, integrationBranch, wave, taskID, resolve, nil)
}

func authenticatedV7LandingAuditSourceWithStore(repoRoot, integrationBranch string, wave Note, taskID string, resolve func(string, string) (string, bool), trustedStore *RuntimeStore) (string, bool) {
	if strings.TrimSpace(integrationBranch) == "" {
		return "", false
	}
	if resolve == nil {
		resolve = gitRevParse
	}
	integrationSHA, ok := resolve(repoRoot, integrationBranch+"^{commit}")
	if !ok || integrationSHA == "" {
		return "", false
	}
	vaultPath := v7VaultPathForLandingAudit(wave)
	if vaultPath == "" {
		return "", false
	}
	landings := normalizeLandingAudit(wave.Data["landings"])
	for index := len(landings) - 1; index >= 0; index-- {
		row := landings[index]
		if stringField(row, "task") != taskID ||
			stringField(row, "branch") != v7TaskBranchName(taskID) ||
			stringField(row, "target") != integrationBranch ||
			stringField(row, "gate_result") != "pass" ||
			stringField(row, "provenance") != v7LandingAuditProvenance ||
			!trustedV7LandingControlAuthority(stringField(row, "control_authority"), stringField(row, "actor")) {
			continue
		}
		if _, err := time.Parse(time.RFC3339, stringField(row, "timestamp")); err != nil {
			continue
		}
		fingerprint := stringField(row, "receipt_fingerprint")
		if fingerprint == "" || stringField(row, "gate_fingerprint") == "" {
			continue
		}
		receipt, ok := loadV7LandingReceipt(vaultPath, fingerprint)
		if !ok ||
			receipt.Actor != stringField(row, "actor") ||
			receipt.ControlAuthority != stringField(row, "control_authority") ||
			receipt.GateSummary != stringField(row, "gate_summary") ||
			receipt.GateFingerprint != stringField(row, "gate_fingerprint") ||
			receipt.BatchBaseSHA == "" ||
			receipt.BatchHeadSHA != stringField(row, "commit") ||
			receipt.BatchTreeSHA != stringField(row, "tree") {
			continue
		}
		proof, ok := verifiedV7LandingReceiptTaskWithStore(repoRoot, integrationBranch, receipt, taskID, trustedStore)
		if !ok ||
			proof.SourceSHA != stringField(row, "source_sha") ||
			proof.SourceProvenance != stringField(row, "source_provenance") ||
			proof.BaseSHA != stringField(row, "base_sha") ||
			proof.MergeCommit != stringField(row, "merge_commit") ||
			!gitMergeBaseAncestor(repoRoot, receipt.BatchHeadSHA, integrationSHA) {
			continue
		}
		resolvedSource, sourceOK := resolve(repoRoot, proof.SourceSHA+"^{commit}")
		if !sourceOK || !strings.EqualFold(proof.SourceSHA, resolvedSource) {
			continue
		}
		return resolvedSource, true
	}
	return "", false
}

func v7VaultPathForLandingAudit(wave Note) string {
	if strings.TrimSpace(wave.AbsolutePath) == "" {
		return ""
	}
	vaultPath := filepath.Clean(filepath.Join(filepath.Dir(wave.AbsolutePath), "..", ".."))
	if !fileExists(filepath.Join(vaultPath, "SKILL.md")) {
		return ""
	}
	return vaultPath
}

func scheduledPromotionToolchainFingerprint(repoRoot string, commands []string) string {
	toolchains := v7LandingToolchainFingerprints(repoRoot, commands)
	if len(toolchains) == 0 {
		return ""
	}
	parts := make([]string, 0, len(toolchains))
	for name, value := range toolchains {
		parts = append(parts, name+"="+value)
	}
	sort.Strings(parts)
	return departureFingerprint(parts...)
}

// Full-gate ledger proof is valid only under the configured provider contract
// that produced it. This prevents an old sandbox-exec or unrestricted pass
// from bypassing the lifecycle boundary before a full promotion.
func scheduledPromotionFullGateToolchainFingerprint(repoRoot string, commands []string, provider, stateRoot string) string {
	base := scheduledPromotionToolchainFingerprint(repoRoot, commands)
	if base == "" {
		return ""
	}
	_, _, identity, _, err := resolveV7TrustedFullGateProvider(provider, stateRoot)
	if err != nil {
		return departureFingerprint(v7FullGateIsolationContract, "unavailable", strings.TrimSpace(provider), base)
	}
	return departureFingerprint(v7FullGateIsolationContract, identity, base)
}

func scheduledPromotionSnapshotDrift(before, after scheduledPromotionCandidateSnapshot) string {
	if before.WaveID != after.WaveID {
		return "wave"
	}
	if !sameDepartureStrings(before.Candidate.WaveIDs, after.Candidate.WaveIDs) {
		return "wave"
	}
	if !sameDepartureStrings(before.Candidate.CargoTaskIDs, after.Candidate.CargoTaskIDs) {
		return "cargo"
	}
	if before.Candidate.CandidateSHA != after.Candidate.CandidateSHA || before.Candidate.CandidateTreeHash != after.Candidate.CandidateTreeHash {
		return "candidate"
	}
	if before.Candidate.IntegrationBaseSHA != after.Candidate.IntegrationBaseSHA {
		return "integration"
	}
	if before.Candidate.ExpectedDefaultBranchSHA != after.Candidate.ExpectedDefaultBranchSHA {
		return "default_ref"
	}
	if before.Candidate.WaveAuthorization != after.Candidate.WaveAuthorization {
		return "authorization"
	}
	if !sameStringMap(before.Candidate.TaskStateRevisions, after.Candidate.TaskStateRevisions) || !sameStringMap(before.Candidate.TaskSourceSHAs, after.Candidate.TaskSourceSHAs) {
		return "task"
	}
	if before.Gate.Command != after.Gate.Command || before.Gate.Profile != after.Gate.Profile || before.Gate.Toolchain != after.Gate.Toolchain || before.Gate.TreeHash != after.Gate.TreeHash {
		return "gate"
	}
	return ""
}

func sameStringMap(left, right map[string]string) bool {
	if len(left) != len(right) {
		return false
	}
	for key, value := range left {
		if right[key] != value {
			return false
		}
	}
	return true
}

// startScheduledPromotionLeaseHeartbeat keeps a long full-gate run fenced.
// Stopping waits for any in-flight renewal, so the caller can safely perform
// the final fence check without racing its own heartbeat.
func startScheduledPromotionLeaseHeartbeat(store *RuntimeStore, lease ResourceLease, ttl time.Duration) func() error {
	interval := ttl / 3
	if interval < time.Millisecond {
		interval = time.Millisecond
	}
	stop := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				done <- nil
				return
			case <-ticker.C:
				renewed, err := store.RenewResourceLease(ResourceLeaseRenewal{
					Name: lease.Name, Owner: lease.Owner, Generation: lease.Generation, TTL: ttl,
				})
				if err != nil {
					done <- err
					return
				}
				if !renewed {
					done <- tuskerError(errorInvalidTransition, "promotion refusal: full_gate_lease_heartbeat_fenced")
					return
				}
			}
		}
	}()
	var once sync.Once
	var heartbeatErr error
	return func() error {
		once.Do(func() {
			close(stop)
			heartbeatErr = <-done
		})
		return heartbeatErr
	}
}

// resumePromotionFailureRouting finishes a red-gate route only after its
// durable departure intent exists. Every mutation below is idempotent, so a
// crash or CAS loss cannot leave a reworked task without an attributable
// failure packet, and a later reconciliation can safely resume this phase.
func resumePromotionFailureRouting(vaultPath string, store *RuntimeStore, run *DepartureRun) error {
	if store == nil || run == nil || run.State != DepartureStateRepairing || run.Gate.Failure.Identity == "" {
		return tuskerError(errorInvalidTransition, "promotion repair routing requires a durable repairing departure")
	}
	failure := run.Gate.Failure
	if failure.Class == string(promotionFailureIsolated) && failure.OwningTaskID != "" {
		affected := failure.AffectedTaskIDs
		if len(affected) == 0 {
			affected = promotionFailureHardClosure(vaultPath, failure.OwningTaskID)
		}
		for _, id := range affected {
			if err := statusV7Cmd(Args{"vault": vaultPath, "quiet": "true", "id": id, "status": "rework", "by": "tusker:scheduled-promotion", "reason": "promotion gate red: " + failure.Identity}); err != nil {
				return err
			}
		}
	} else {
		excerpt := "see the bounded promotion failure packet"
		if len(failure.Packet.Defects) > 0 {
			excerpt = failure.Packet.Defects[0].Excerpt
		}
		artifact := ""
		if len(failure.Packet.ArtifactRefs) > 0 {
			artifact = failure.Packet.ArtifactRefs[len(failure.Packet.ArtifactRefs)-1]
		}
		// This task is deliberately held. A red promotion must not manufacture an
		// autonomous execution lane outside the project's configured wave/dispatch
		// authority; the repair packet is ready for the existing explicit release
		// path to bind it safely.
		if err := createPromotionFailureRepairTask(vaultPath, run.ID, failure.Packet.GateCommand, excerpt, failure.Packet.GateProfile, failure.Identity, failure.OwningTaskID, artifact, true); err != nil {
			return err
		}
		failure.RepairTaskID = promotionFailureRepairTaskID(vaultPath, failure.Identity)
	}
	next := *run
	next.Gate.Failure = failure
	next.State = DepartureStateFailed
	if err := scheduledPromotionBeforeFailureCompletion(); err != nil {
		return err
	}
	changed, err := store.TransitionDepartureRun(next, run.StateRevision)
	if err != nil {
		return err
	}
	if !changed {
		latest, readErr := store.FindDepartureRun(run.ID)
		if readErr != nil {
			return readErr
		}
		if latest != nil && latest.State == DepartureStateFailed && latest.Gate.Failure.Identity == failure.Identity {
			*run = *latest
			return nil
		}
		return tuskerError(errorInvalidTransition, "promotion repair routing lost its departure CAS; retry reconciliation")
	}
	*run = next
	run.StateRevision++
	return nil
}

// withPromotionGateResult preserves packet-harvested fallback defects for
// setup/execution failures, where a GateTierResult has no command-level
// defects. When present, the structured result is authoritative because it
// retains each failed command's identity.
func withPromotionGateResult(packet PromotionFailurePacket, result GateTierResult) PromotionFailurePacket {
	packet.GateResult = result
	if len(result.Defects) > 0 {
		packet.Defects = append([]GateDefect(nil), result.Defects...)
	}
	return packet
}

func appendScheduledPromotionAudit(vaultPath, waveID, defaultBranch, commit, gateSummary, actor string) error {
	idx, err := loadV7Index(vaultPath)
	if err != nil {
		return err
	}
	wave, ok := idx.Waves[waveID]
	if !ok {
		return tuskerError(errorNotFound, "V7 wave not found: "+waveID)
	}
	return appendV7WaveLandingAudit(vaultPath, waveID, []v7LandingAuditEntry{{
		Task: "wave", Branch: v7WaveIntegrationBranch(wave), Target: defaultBranch,
		GateResult: "pass", GateSummary: gateSummary, Commit: commit,
		Actor: actor, Timestamp: time.Now().UTC().Format(time.RFC3339),
	}}, actor)
}

func inspectScheduledPromotionIntent(repoRoot string, promotion DeparturePromotion) (string, string, error) {
	if strings.TrimSpace(promotion.IntendedSHA) == "" {
		return "", "", fmt.Errorf("legacy promotion intent lacks intended_sha")
	}
	if strings.TrimSpace(promotion.ExpectedRef) == "" || strings.TrimSpace(promotion.ExpectedSHA) == "" {
		return "", "", fmt.Errorf("promotion intent lacks expected ref identity")
	}
	for _, frozen := range []struct {
		label string
		sha   string
	}{
		{label: "expected_sha", sha: promotion.ExpectedSHA},
		{label: "intended_sha", sha: promotion.IntendedSHA},
	} {
		resolved, err := gitOutputTrim(repoRoot, "rev-parse", frozen.sha+"^{commit}")
		if err != nil {
			return "", "", fmt.Errorf("%s is unavailable", frozen.label)
		}
		if !strings.EqualFold(strings.TrimSpace(frozen.sha), resolved) {
			return "", "", fmt.Errorf("%s must be a full immutable commit SHA", frozen.label)
		}
	}
	current, err := gitOutputTrim(repoRoot, "rev-parse", promotion.ExpectedRef+"^{commit}")
	if err != nil {
		return "", "", fmt.Errorf("expected ref %s is unavailable", promotion.ExpectedRef)
	}
	switch {
	case strings.EqualFold(current, promotion.ExpectedSHA):
		return scheduledPromotionIntentPreRef, current, nil
	case strings.EqualFold(current, promotion.IntendedSHA):
		return scheduledPromotionIntentCommitted, current, nil
	default:
		return scheduledPromotionIntentDrifted, current, nil
	}
}

func validateScheduledPromotionIntendedCommit(repoRoot string, run DepartureRun) error {
	promotion := run.Promotion
	candidateSHA := strings.TrimSpace(run.Candidate.CandidateSHA)
	expectedSHA := strings.TrimSpace(run.Candidate.ExpectedDefaultBranchSHA)
	if candidateSHA == "" || expectedSHA == "" || !strings.EqualFold(expectedSHA, promotion.ExpectedSHA) {
		return fmt.Errorf("intended_sha provenance does not match the durable candidate")
	}
	resolvedCandidate, err := gitOutputTrim(repoRoot, "rev-parse", candidateSHA+"^{commit}")
	if err != nil || !strings.EqualFold(candidateSHA, resolvedCandidate) {
		return fmt.Errorf("durable candidate SHA is unavailable or mutable")
	}
	intendedTree, err := gitOutputTrim(repoRoot, "rev-parse", promotion.IntendedSHA+"^{tree}")
	if err != nil {
		return fmt.Errorf("intended_sha tree is unavailable")
	}
	candidateTree, err := gitOutputTrim(repoRoot, "rev-parse", candidateSHA+"^{tree}")
	if err != nil || intendedTree != candidateTree {
		return fmt.Errorf("intended_sha tree does not match the durable candidate")
	}
	parents, err := gitOutputTrim(repoRoot, "show", "-s", "--format=%P", promotion.IntendedSHA)
	if err != nil {
		return fmt.Errorf("intended_sha parents are unavailable")
	}
	actualParents := strings.Fields(parents)
	if len(actualParents) != 2 ||
		!strings.EqualFold(actualParents[0], promotion.ExpectedSHA) ||
		!strings.EqualFold(actualParents[1], candidateSHA) {
		return fmt.Errorf("intended_sha parents do not match the durable promotion")
	}
	return nil
}

func persistScheduledPromotionCommit(store *RuntimeStore, run *DepartureRun, ref, sha string) error {
	if store == nil || run == nil {
		return fmt.Errorf("persist promotion commit requires a durable departure")
	}
	if intended := strings.TrimSpace(run.Promotion.IntendedSHA); intended != "" && !strings.EqualFold(intended, sha) {
		return tuskerError(errorInvalidTransition, "promotion recovery blocked: committed SHA differs from intended_sha")
	}
	next := *run
	next.Promotion.CommittedRef = ref
	next.Promotion.CommittedSHA = sha
	next.Promotion.CommittedAt = departureNow()
	next.State = DepartureStatePromoted
	next.BlockReason = ""
	changed, err := store.TransitionDepartureRun(next, run.StateRevision)
	if err != nil {
		return err
	}
	if !changed {
		return tuskerError(errorInvalidTransition, "promotion committed but departure row changed concurrently; recover from committed ref "+sha)
	}
	*run = next
	run.StateRevision++
	return nil
}

func resumeScheduledPromotionIntent(ctx context.Context, vaultPath, projectID, waveID string, store *RuntimeStore, run *DepartureRun, actor string) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if run.ProjectID != projectID {
		return "", tuskerError(errorInvalidTransition, "promotion recovery blocked: departure belongs to another project")
	}
	repoRoot := v7RepoRoot(vaultPath)
	state, current, err := inspectScheduledPromotionIntent(repoRoot, run.Promotion)
	if err == nil {
		err = validateScheduledPromotionIntendedCommit(repoRoot, *run)
	}
	if err != nil {
		return "", tuskerError(errorInvalidTransition, "promotion recovery blocked: "+err.Error())
	}
	if state == scheduledPromotionIntentDrifted {
		return "", tuskerError(errorInvalidTransition, "promotion recovery blocked: default_ref_drift current="+current+" expected="+run.Promotion.ExpectedSHA+" intended="+run.Promotion.IntendedSHA)
	}
	complete := func() (string, error) {
		if err := persistScheduledPromotionCommit(store, run, run.Promotion.ExpectedRef, run.Promotion.IntendedSHA); err != nil {
			return "", err
		}
		if err := appendScheduledPromotionAudit(vaultPath, waveID, run.Promotion.CommittedRef, run.Promotion.CommittedSHA, "durable promotion intent replay: "+run.Gate.Command, actor); err != nil {
			return "", err
		}
		return run.Promotion.CommittedSHA, nil
	}
	if state == scheduledPromotionIntentCommitted {
		if err := finishV7DefaultBranchAdvance(v7DefaultBranchCheckouts(repoRoot, run.Promotion.ExpectedRef)); err != nil {
			return "", err
		}
		return complete()
	}
	if hold, err := store.departureHold(projectID, false); err != nil {
		return "", err
	} else if hold != nil {
		return "", departureHoldError(hold)
	}
	releaseLanding, err := acquireV7LandingLock(vaultPath)
	if err != nil {
		return "", err
	}
	defer releaseLanding()
	idx, err := loadV7Index(vaultPath)
	if err != nil {
		return "", err
	}
	wave, ok := idx.Waves[waveID]
	if !ok {
		return "", tuskerError(errorNotFound, "V7 wave not found: "+waveID)
	}
	state, current, err = inspectScheduledPromotionIntent(repoRoot, run.Promotion)
	if err != nil {
		return "", tuskerError(errorInvalidTransition, "promotion recovery blocked: "+err.Error())
	}
	if state == scheduledPromotionIntentCommitted {
		if err := finishV7DefaultBranchAdvance(v7DefaultBranchCheckouts(repoRoot, run.Promotion.ExpectedRef)); err != nil {
			return "", err
		}
		return complete()
	}
	if state != scheduledPromotionIntentPreRef {
		return "", tuskerError(errorInvalidTransition, "promotion recovery blocked: default_ref_drift current="+current+" expected="+run.Promotion.ExpectedSHA+" intended="+run.Promotion.IntendedSHA)
	}
	if err := validateScheduledPromotionWaveAuthorityWithStore(vaultPath, waveID, store); err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	lease, acquired, err := store.AcquireResourceLease(ResourceLeaseAcquireInput{
		Name: "gate:full", Owner: "departure:" + run.ID, Purpose: scheduledPromotionResourcePurpose,
		ProjectID: projectID, DepartureID: run.ID, TTL: scheduledPromotionResourceLeaseTTL,
	})
	if err != nil {
		return "", err
	}
	if !acquired {
		return "", tuskerError(errorInvalidTransition, "promotion refusal: full_gate_lease_unavailable")
	}
	leaseOutcome := "promotion intent replay aborted before ref update"
	defer func() {
		_, _ = store.ReleaseResourceLease(lease.Name, lease.Owner, lease.Generation, leaseOutcome, time.Now().UTC())
	}()
	if hold, err := store.departureHold(projectID, false); err != nil {
		return "", err
	} else if hold != nil {
		return "", departureHoldError(hold)
	}
	if matched, err := store.ResourceLeaseMatches(lease.Name, lease.Owner, lease.Generation); err != nil || !matched {
		return "", tuskerError(errorInvalidTransition, "promotion refusal: full_gate_lease_fenced_at_ref_update")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	checkouts, err := scheduledPromotionAdvanceRefUnderMaterialEpoch(
		ctx, vaultPath, projectID, waveID, store, lease, run.Candidate, wave,
		run.Promotion.ExpectedRef, run.Promotion.ExpectedSHA, run.Promotion.IntendedSHA, true,
	)
	if err != nil {
		if state, _, inspectErr := inspectScheduledPromotionIntent(repoRoot, run.Promotion); inspectErr == nil && state == scheduledPromotionIntentCommitted {
			if finishErr := finishV7DefaultBranchAdvance(v7DefaultBranchCheckouts(repoRoot, run.Promotion.ExpectedRef)); finishErr != nil {
				return "", finishErr
			}
			leaseOutcome = "promotion intent replay observed committed ref"
			return complete()
		}
		return "", err
	}
	if err := finishV7DefaultBranchAdvance(checkouts); err != nil {
		return "", err
	}
	if err := scheduledPromotionAfterRefUpdate(); err != nil {
		return "", err
	}
	commit, err := complete()
	if err != nil {
		return "", err
	}
	leaseOutcome = "promotion passed"
	return commit, nil
}

// promoteScheduledWave performs the irreversible half of a departure.  It
// uses the normal staging worktree/gate implementation; this wrapper adds the
// immutable candidate contract and the ref CAS that scheduled promotion needs.
func promoteScheduledWave(vaultPath, projectID, waveID string, wf Workflow, store *RuntimeStore, run *DepartureRun, actor string) (string, error) {
	return promoteScheduledWaveContext(context.Background(), vaultPath, projectID, waveID, wf, store, run, actor)
}

func promoteScheduledWaveContext(ctx context.Context, vaultPath, projectID, waveID string, wf Workflow, store *RuntimeStore, run *DepartureRun, actor string) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if !wf.ScheduledPromotion.Effective.Promote {
		return "", tuskerError(errorInvalidTransition, "scheduled promotion refusal: mode "+wf.ScheduledPromotion.Effective.Mode+" cannot move the default branch")
	}
	if store == nil || run == nil {
		return "", fmt.Errorf("scheduled promotion requires a departure runtime row")
	}
	if run.State == DepartureStateRepairing && run.Gate.Failure.Identity != "" {
		if err := resumePromotionFailureRouting(vaultPath, store, run); err != nil {
			return "", err
		}
		return "", tuskerError(errorInvalidTransition, "promotion gate red: "+run.Gate.Failure.Identity)
	}
	if run.Promotion.CommittedRef != "" && run.Promotion.CommittedSHA != "" {
		if err := scheduledPromotionBeforeCommittedReplay(ctx); err != nil {
			return "", err
		}
		repoRoot := v7RepoRoot(vaultPath)
		if current, err := gitOutputTrim(repoRoot, "rev-parse", run.Promotion.CommittedRef+"^{commit}"); err == nil && current == run.Promotion.CommittedSHA {
			if err := appendScheduledPromotionAudit(vaultPath, waveID, run.Promotion.CommittedRef, current, "durable promotion replay: "+run.Gate.Command, actor); err != nil {
				return "", err
			}
			return current, nil
		}
		return "", tuskerError(errorInvalidTransition, "promotion recovery blocked: committed ref no longer matches its durable SHA")
	}
	if run.Promotion.AttemptedAt != "" {
		return resumeScheduledPromotionIntent(ctx, vaultPath, projectID, waveID, store, run, actor)
	}
	if hold, err := store.departureHold(projectID, false); err != nil {
		return "", err
	} else if hold != nil {
		return "", departureHoldError(hold)
	}
	release, err := acquireV7LandingLock(vaultPath)
	if err != nil {
		return "", err
	}
	defer release()
	idx, err := loadV7Index(vaultPath)
	if err != nil {
		return "", err
	}
	wave, ok := idx.Waves[waveID]
	if !ok {
		return "", tuskerError(errorNotFound, "V7 wave not found: "+waveID)
	}
	if err := validateScheduledPromotionWaveAuthorityWithStore(vaultPath, waveID, store); err != nil {
		return "", err
	}
	if len(run.Candidate.CargoTaskIDs) > 0 {
		if err := validateScheduledPromotionDurableCargoWithStore(vaultPath, waveID, run.Candidate, store); err != nil {
			return "", err
		}
	}
	integrationBranch := v7WaveIntegrationBranch(wave)
	// The integration-side task/wave state is part of the tree that will be
	// gated, so synchronize it before freezing the candidate rather than
	// quietly changing it between the gate and the CAS.
	if err := syncV7WaveControlStateToIntegration(vaultPath, wave, integrationBranch); err != nil {
		return "", err
	}
	before, err := scheduledPromotionSnapshotWithStore(vaultPath, projectID, waveID, wf, store)
	if err != nil {
		return "", err
	}
	if run.Candidate.CandidateSHA != "" {
		expected := scheduledPromotionCandidateSnapshot{WaveID: waveID, Candidate: run.Candidate, Gate: run.Gate, DefaultBranch: before.DefaultBranch}
		if drift := scheduledPromotionSnapshotDrift(expected, before); drift != "" {
			return "", tuskerError(errorInvalidTransition, "promotion recompute required: durable_"+drift+"_drift")
		}
	}
	leaseOwner := "departure:" + run.ID
	lease, acquired, err := store.AcquireResourceLease(ResourceLeaseAcquireInput{
		Name: "gate:full", Owner: leaseOwner, Purpose: scheduledPromotionResourcePurpose,
		ProjectID: projectID, DepartureID: run.ID, TTL: scheduledPromotionResourceLeaseTTL,
	})
	if err != nil {
		return "", err
	}
	if !acquired {
		return "", tuskerError(errorInvalidTransition, "promotion refusal: full_gate_lease_unavailable")
	}
	leaseOutcome := "promotion aborted before ref update"
	defer func() {
		_, _ = store.ReleaseResourceLease(lease.Name, lease.Owner, lease.Generation, leaseOutcome, time.Now().UTC())
	}()
	if matched, err := store.ResourceLeaseMatches(lease.Name, lease.Owner, lease.Generation); err != nil || !matched {
		return "", tuskerError(errorInvalidTransition, "promotion refusal: full_gate_lease_fenced")
	}
	gatePolicy, err := scheduledPromotionGatePolicy(vaultPath, wf)
	if err != nil {
		return "", err
	}
	if strings.Join(gatePolicy.HarvestCommands, " && ") != before.Gate.Command || gatePolicy.Profile != before.Gate.Profile {
		return "", tuskerError(errorInvalidTransition, "promotion recompute required: gate_contract_drift")
	}
	gateStarted := time.Now().UTC()
	stopHeartbeat := startScheduledPromotionLeaseHeartbeat(store, lease, scheduledPromotionResourceLeaseTTL)
	defer func() { _ = stopHeartbeat() }()
	execution := runV7GateTierOnRefContext(ctx, vaultPath, v7RepoRoot(vaultPath), before.Candidate.CandidateSHA, projectID, gatePolicy, store)
	if err := ctx.Err(); err != nil {
		if errors.Is(execution.Err, errV7FullGateProvider) {
			return "", execution.Err
		}
		for _, ref := range execution.ArtifactRefs {
			_ = os.Remove(ref)
		}
		return "", err
	}
	// A configured flake rerun is deliberately one-shot. Its second result
	// replaces the gate attempt; a second red falls through to quarantine.
	if execution.Err == nil && execution.Result.Outcome == gateOutcomeFailed && strings.TrimSpace(gatePolicy.FlakeFailureAction) == "rerun" {
		raw, _ := os.ReadFile(execution.ArtifactRef)
		probe := promotionFailurePacket(before.Candidate, before.Gate, actor, string(raw), nil, gatePolicy, "unknown", "not_run", promotionFailureOwner(before.Candidate), nil, []string{execution.ArtifactRef})
		if classifyPromotionFailure(probe, gatePolicy).Class == promotionFailureFlake {
			firstRefs := append([]string(nil), execution.ArtifactRefs...)
			execution = runV7GateTierOnRefContext(ctx, vaultPath, v7RepoRoot(vaultPath), before.Candidate.CandidateSHA, projectID, gatePolicy, store)
			execution.ArtifactRefs = append(firstRefs, execution.ArtifactRefs...)
		}
	}
	if err := ctx.Err(); err != nil {
		for _, ref := range execution.ArtifactRefs {
			_ = os.Remove(ref)
		}
		return "", err
	}
	gateSummary := string(execution.Result.Outcome)
	gateFinished := time.Now().UTC()
	if execution.Err != nil || (execution.Result.Outcome != gateOutcomePassed && execution.Result.Outcome != gateOutcomeLedgerHit) {
		if heartbeatErr := stopHeartbeat(); heartbeatErr != nil {
			return "", heartbeatErr
		}
		// Red gates never advance main. Persist a compact, replay-safe packet so
		// triage has the frozen candidate and a durable raw-log reference rather
		// than rediscovering the failure from mutable branches.
		artifactRefs := append([]string(nil), execution.ArtifactRefs...)
		if len(artifactRefs) == 0 {
			artifactRefs = []string{execution.ArtifactRef}
		}
		gateOutput, _ := os.ReadFile(artifactRefs[len(artifactRefs)-1])
		owner := promotionFailureOwner(before.Candidate)
		touched, touchStatus := []string(nil), "unavailable"
		if paths, pathErr := gitCombined(v7RepoRoot(vaultPath), "diff", "--name-only", "-z", before.Candidate.ExpectedDefaultBranchSHA+".."+before.Candidate.CandidateSHA); pathErr == nil {
			touched = promotionTouchedPathsFromNUL(paths)
			touchStatus = "proven"
		}
		lastGreen := ""
		lastStatus := "unavailable:no_matching_entry"
		if entry, ledgerErr := store.LatestCompleteGateLedgerBefore(projectID, gatePolicy.HarvestCommands, before.Gate.Profile, before.Gate.Toolchain, gateStarted.Format(time.RFC3339Nano)); ledgerErr == nil && entry != nil {
			lastGreen, lastStatus = entry.ID+"@"+entry.TreeHash+"@"+entry.PassedAt, "proven"
		}
		packet := promotionFailurePacket(before.Candidate, before.Gate, actor, string(gateOutput), execution.Err, gatePolicy, lastGreen, "", owner, touched, artifactRefs)
		packet.LastGreenStatus, packet.BisectionStatus, packet.TouchedPathsStatus = lastStatus, "not_run:independent_patch_boundaries_unavailable", touchStatus
		packet = withPromotionGateResult(packet, execution.Result)
		route := classifyPromotionFailure(packet, gatePolicy)
		repairTaskID, action := "", "ambiguous_repair"
		affected := promotionFailureHardClosure(vaultPath, owner)
		if route.Class == promotionFailureIsolated && owner != "" {
			action = "owner_rework"
		} else {
			if route.Class == promotionFailureInfrastructure {
				action = "infrastructure_repair"
			}
			if route.Class == promotionFailureFlake {
				action = "flake_quarantine"
			}
		}
		intent := *run
		intent.Candidate, intent.Gate = before.Candidate, before.Gate
		intent.Gate.Status = "failed"
		intent.Gate.StartedAt, intent.Gate.FinishedAt, intent.Gate.ArtifactRef = gateStarted.Format(time.RFC3339Nano), gateFinished.Format(time.RFC3339Nano), artifactRefs[len(artifactRefs)-1]
		intent.Gate.ProviderReceipts = append([]GateProviderReceipt(nil), execution.ProviderReceipts...)
		intent.Gate.Failure = DepartureFailure{Class: string(route.Class), Identity: route.StableIdentity, OwningTaskID: packet.OwningTaskID, BisectionRef: packet.BisectionRef, ArtifactRefs: packet.ArtifactRefs, RepairTaskID: repairTaskID, ModelTriage: route.ModelTriage, Packet: packet, Action: action, AffectedTaskIDs: affected}
		intent.State = DepartureStateRepairing
		intent.BlockReason = "promotion gate red: " + route.StableIdentity
		if changed, persistErr := store.TransitionDepartureRun(intent, run.StateRevision); persistErr != nil {
			return "", persistErr
		} else if !changed {
			return "", tuskerError(errorInvalidTransition, "promotion refusal: departure row changed while recording red gate")
		}
		*run = intent
		run.StateRevision++
		if err := scheduledPromotionAfterFailureIntent(); err != nil {
			return "", err
		}
		if err := resumePromotionFailureRouting(vaultPath, store, run); err != nil {
			return "", err
		}
		return "", tuskerError(errorInvalidTransition, "promotion gate red: "+scheduledPromotionGateFailureDetail(execution.Err, string(gateOutput)))
	}
	if execution.Err == nil && (execution.Result.Outcome == gateOutcomePassed || execution.Result.Outcome == gateOutcomeLedgerHit) {
		for _, ref := range execution.ArtifactRefs {
			_ = os.Remove(ref)
		}
	}
	if hold, err := store.departureHold(projectID, false); err != nil {
		return "", err
	} else if hold != nil {
		return "", departureHoldError(hold)
	}
	after, err := scheduledPromotionSnapshotWithStore(vaultPath, projectID, waveID, wf, store)
	if err != nil {
		return "", err
	}
	if drift := scheduledPromotionSnapshotDrift(before, after); drift != "" {
		return "", tuskerError(errorInvalidTransition, "promotion recompute required: "+drift+"_drift")
	}
	repoRoot := v7RepoRoot(vaultPath)
	message := fmt.Sprintf("Scheduled promotion %s", waveID)
	mergeCommit, err := gitOutputTrim(repoRoot, "commit-tree", before.Candidate.CandidateSHA+"^{tree}", "-p", before.Candidate.ExpectedDefaultBranchSHA, "-p", before.Candidate.CandidateSHA, "-m", message)
	if err != nil {
		return "", err
	}
	// A final snapshot makes every mutable input explicit. update-ref supplies
	// the final default-ref CAS even if another writer races after this read.
	final, err := scheduledPromotionSnapshotWithStore(vaultPath, projectID, waveID, wf, store)
	if err != nil {
		return "", err
	}
	if drift := scheduledPromotionSnapshotDrift(before, final); drift != "" {
		return "", tuskerError(errorInvalidTransition, "promotion recompute required: "+drift+"_drift")
	}
	if hold, err := store.departureHold(projectID, false); err != nil {
		return "", err
	} else if hold != nil {
		return "", departureHoldError(hold)
	}
	if err := scheduledPromotionBeforeDefaultPrepare(); err != nil {
		return "", err
	}
	late, err := scheduledPromotionSnapshotWithStore(vaultPath, projectID, waveID, wf, store)
	if err != nil {
		return "", err
	}
	if drift := scheduledPromotionSnapshotDrift(before, late); drift != "" {
		return "", tuskerError(errorInvalidTransition, "promotion recompute required: "+drift+"_drift")
	}
	if hold, err := store.departureHold(projectID, false); err != nil {
		return "", err
	} else if hold != nil {
		return "", departureHoldError(hold)
	}
	if matched, err := store.ResourceLeaseMatches(lease.Name, lease.Owner, lease.Generation); err != nil || !matched {
		return "", tuskerError(errorInvalidTransition, "promotion refusal: full_gate_lease_fenced_before_ref_update")
	}
	// Persist the irreversible-action intent before touching the ref. A crash
	// after this CAS is deliberately classified as ambiguous and blocks replay
	// until reconciliation observes the actual ref.
	intent := *run
	intent.Candidate, intent.Gate = before.Candidate, before.Gate
	intent.Gate.Status = "passed"
	intent.Gate.StartedAt = gateStarted.Format(time.RFC3339Nano)
	intent.Gate.FinishedAt = gateFinished.Format(time.RFC3339Nano)
	intent.Gate.ProviderReceipts = append([]GateProviderReceipt(nil), execution.ProviderReceipts...)
	intent.Promotion = DeparturePromotion{
		ExpectedRef: before.DefaultBranch, ExpectedSHA: before.Candidate.ExpectedDefaultBranchSHA,
		IntendedSHA: mergeCommit,
		AttemptedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
	intent.State = DepartureStateGating
	if hold, err := store.departureHold(projectID, false); err != nil {
		return "", err
	} else if hold != nil {
		return "", departureHoldError(hold)
	}
	changed, err := store.TransitionDepartureRun(intent, run.StateRevision)
	if err != nil {
		return "", err
	}
	if !changed {
		return "", tuskerError(errorInvalidTransition, "promotion refusal: departure row changed before ref update")
	}
	*run = intent
	run.StateRevision++
	if err := scheduledPromotionAfterRefIntent(); err != nil {
		return "", err
	}
	if hold, err := store.departureHold(projectID, false); err != nil {
		return "", err
	} else if hold != nil {
		return "", departureHoldError(hold)
	}
	if matched, err := store.ResourceLeaseMatches(lease.Name, lease.Owner, lease.Generation); err != nil || !matched {
		return "", tuskerError(errorInvalidTransition, "promotion refusal: full_gate_lease_fenced_at_ref_update")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	checkouts, err := scheduledPromotionAdvanceRefUnderMaterialEpoch(
		ctx, vaultPath, projectID, waveID, store, lease, before.Candidate, wave,
		before.DefaultBranch, before.Candidate.ExpectedDefaultBranchSHA, mergeCommit, false,
	)
	if err != nil {
		return "", err
	}
	if err := finishV7DefaultBranchAdvance(checkouts); err != nil {
		return "", err
	}
	if heartbeatErr := stopHeartbeat(); heartbeatErr != nil {
		return "", heartbeatErr
	}
	if err := scheduledPromotionAfterRefUpdate(); err != nil {
		return "", err
	}
	// Only durable success after the ref update counts as a promoted revision.
	if err := persistScheduledPromotionCommit(store, run, before.DefaultBranch, mergeCommit); err != nil {
		return "", err
	}
	leaseOutcome = "promotion passed"
	if err := appendScheduledPromotionAudit(vaultPath, waveID, before.DefaultBranch, mergeCommit, gateSummary, actor); err != nil {
		return "", err
	}
	return mergeCommit, nil
}

// scheduledPromotionGateFailureDetail keeps the causal execution failure
// visible to callers. The raw artifact remains the forensic record, but a
// leading provider receipt can otherwise consume the old 320-byte excerpt and
// hide the fail-closed ledger or lifecycle reason entirely.
func scheduledPromotionGateFailureDetail(executionErr error, gateOutput string) string {
	if executionErr != nil {
		return safePacketText(executionErr.Error(), 640)
	}
	return safePacketText(gateOutput, 320)
}
