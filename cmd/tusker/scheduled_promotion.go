package main

import (
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	scheduledPromotionResourceLeaseTTL = defaultResourceLeaseTTL
	scheduledPromotionAfterRefUpdate   = func() error { return nil }
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

// scheduledPromotionAllowsDefaultAdvance is intentionally permissive for the
// legacy/manual landing path.  The new policy only takes authority away when a
// repository explicitly selected a non-promoting scheduled-promotion mode.
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
	return policy.Promote, nil
}

// stageScheduledTasks intentionally delegates to tusker land.  That keeps
// serialized staging, the isolated staging worktree, bisection, gate cache,
// and landing audit in one engine instead of growing a second "departure"
// merge path.  In stage mode the policy choke point above leaves main alone.
func stageScheduledTasks(vaultPath string, taskIDs []string, actor string) error {
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
	return landV7Cmd(args)
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
		TaskStateRevisions:       map[string]string{},
		TaskSourceSHAs:           map[string]string{},
		IntegrationBaseSHA:       candidateSHA,
		CandidateSHA:             candidateSHA,
		CandidateTreeHash:        treeHash,
		ExpectedDefaultBranchSHA: defaultSHA,
	}
	memberFacts := make([]string, 0, len(normalizeList(wave.Data["members"])))
	for _, id := range normalizeList(wave.Data["members"]) {
		task, ok := idx.Tasks[id]
		if !ok {
			return scheduledPromotionCandidateSnapshot{}, tuskerError(errorInvalidTransition, "promotion candidate drift: task_missing:"+id)
		}
		stateRev := stringField(task.Data, "state_rev")
		sourceSHA, err := scheduledPromotionTaskSourceSHA(repoRoot, candidateSHA, integrationBranch, wave, task)
		if err != nil {
			return scheduledPromotionCandidateSnapshot{}, err
		}
		candidate.TaskStateRevisions[id] = stateRev
		candidate.TaskSourceSHAs[id] = sourceSHA
		memberFacts = append(memberFacts, id+"@"+stateRev+"@"+sourceSHA)
	}
	sort.Strings(memberFacts)
	if v7ImplicitDeliveryUnit(wave) {
		members := normalizeList(wave.Data["members"])
		deliveryTask := strings.ToUpper(strings.TrimSpace(stringField(wave.Data, "delivery_task")))
		if len(members) != 1 || deliveryTask == "" || members[0] != deliveryTask {
			return scheduledPromotionCandidateSnapshot{}, tuskerError(errorInvalidTransition, "promotion candidate refusal: implicit_singleton_membership:"+waveID)
		}
		task := idx.Tasks[deliveryTask]
		if strings.ToLower(stringField(task.Data, "status")) != "done" ||
			stringField(task.Data, "wave") != waveID ||
			candidate.TaskSourceSHAs[deliveryTask] == "" {
			return scheduledPromotionCandidateSnapshot{}, tuskerError(errorInvalidTransition, "promotion candidate refusal: implicit_singleton_not_finished:"+deliveryTask)
		}
		if boolFromAny(wave.Data["release_authorized"]) {
			return scheduledPromotionCandidateSnapshot{}, tuskerError(errorInvalidTransition, "promotion candidate refusal: implicit_singleton_release_authority:"+waveID)
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
	gate.Toolchain = scheduledPromotionToolchainFingerprint(repoRoot, gatePolicy.HarvestCommands)
	return scheduledPromotionCandidateSnapshot{WaveID: waveID, Candidate: candidate, Gate: gate, DefaultBranch: defaultBranch}, nil
}

func scheduledPromotionTaskSourceSHA(repoRoot, candidateSHA, integrationBranch string, wave, task Note) (string, error) {
	taskID := stringField(task.Data, "id")
	sourceSHA := firstNonEmpty(stringField(task.Data, "source_sha"), stringField(task.Data, "source_commit"), stringField(task.Data, "source_branch_sha"))
	sourceRef := ""
	if sourceSHA == "" {
		landings := normalizeLandingAudit(wave.Data["landings"])
		for i := len(landings) - 1; i >= 0; i-- {
			row := landings[i]
			if stringField(row, "task") == taskID &&
				stringField(row, "target") == integrationBranch &&
				stringField(row, "gate_result") == "pass" {
				sourceRef = stringField(row, "branch")
				break
			}
		}
		if sourceRef == "" {
			return "", tuskerError(errorInvalidTransition, "promotion candidate refusal: task_source_provenance_missing:"+taskID)
		}
		var err error
		sourceSHA, err = gitOutputTrim(repoRoot, "rev-parse", sourceRef+"^{commit}")
		if err != nil {
			return "", tuskerError(errorInvalidTransition, "promotion candidate refusal: task_source_unavailable:"+taskID)
		}
	}
	if execErr := exec.Command("git", "-C", repoRoot, "merge-base", "--is-ancestor", sourceSHA, candidateSHA).Run(); execErr != nil {
		return "", tuskerError(errorInvalidTransition, "promotion candidate refusal: task_source_not_integrated:"+taskID)
	}
	return sourceSHA, nil
}

func scheduledPromotionToolchainFingerprint(repoRoot string, commands []string) string {
	toolchains := v7LandingToolchainFingerprints(repoRoot, commands)
	parts := make([]string, 0, len(toolchains))
	for name, value := range toolchains {
		parts = append(parts, name+"="+value)
	}
	sort.Strings(parts)
	return departureFingerprint(parts...)
}

func scheduledPromotionSnapshotDrift(before, after scheduledPromotionCandidateSnapshot) string {
	if before.WaveID != after.WaveID {
		return "wave"
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

// promoteScheduledWave performs the irreversible half of a departure.  It
// uses the normal staging worktree/gate implementation; this wrapper adds the
// immutable candidate contract and the ref CAS that scheduled promotion needs.
func promoteScheduledWave(vaultPath, projectID, waveID string, wf Workflow, store *RuntimeStore, run *DepartureRun, actor string) (string, error) {
	if !wf.ScheduledPromotion.Effective.Promote {
		return "", tuskerError(errorInvalidTransition, "scheduled promotion refusal: mode "+wf.ScheduledPromotion.Effective.Mode+" cannot move the default branch")
	}
	if store == nil || run == nil {
		return "", fmt.Errorf("scheduled promotion requires a departure runtime row")
	}
	if run.Promotion.CommittedRef != "" && run.Promotion.CommittedSHA != "" {
		repoRoot := v7RepoRoot(vaultPath)
		if current, err := gitOutputTrim(repoRoot, "rev-parse", run.Promotion.CommittedRef); err == nil && current == run.Promotion.CommittedSHA {
			if err := appendScheduledPromotionAudit(vaultPath, waveID, run.Promotion.CommittedRef, current, "durable promotion replay: "+run.Gate.Command, actor); err != nil {
				return "", err
			}
			return current, nil
		}
	}
	if run.Promotion.AttemptedAt != "" {
		return "", tuskerError(errorInvalidTransition, "promotion refusal: prior ref update outcome is ambiguous; reconcile the departure before retrying")
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
	integrationBranch := v7WaveIntegrationBranch(wave)
	// The integration-side task/wave state is part of the tree that will be
	// gated, so synchronize it before freezing the candidate rather than
	// quietly changing it between the gate and the CAS.
	if err := syncV7WaveControlStateToIntegration(vaultPath, wave, integrationBranch); err != nil {
		return "", err
	}
	before, err := scheduledPromotionSnapshot(vaultPath, projectID, waveID, wf)
	if err != nil {
		return "", err
	}
	leaseOwner := "departure:" + run.ID
	lease, acquired, err := store.AcquireResourceLease(ResourceLeaseAcquireInput{
		Name: "gate:full", Owner: leaseOwner, Purpose: "scheduled full promotion gate",
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
	pass, gateSummary := runV7GateTierOnRef(vaultPath, v7RepoRoot(vaultPath), before.Candidate.CandidateSHA, projectID, gatePolicy, store)
	gateFinished := time.Now().UTC()
	if !pass {
		if heartbeatErr := stopHeartbeat(); heartbeatErr != nil {
			return "", heartbeatErr
		}
		// Red gates never advance main. Persist a compact, replay-safe packet so
		// triage has the frozen candidate and a durable raw-log reference rather
		// than rediscovering the failure from mutable branches.
		artifactRef := "departure://" + run.ID + "/full-gate"
		packet := promotionFailurePacket(before.Candidate, before.Gate, actor, gateSummary, nil, gatePolicy, "", "", "", nil, []string{artifactRef})
		route := classifyPromotionFailure(packet)
		repairTaskID := ""
		if route.Repair {
			_ = createPromotionFailureRepairTask(vaultPath, run.ID, packet.GateCommand, actionableGateFailure(gateSummary, nil), packet.GateProfile, route.StableIdentity, packet.OwningTaskID, artifactRef)
			repairTaskID = promotionFailureRepairTaskID(vaultPath, route.StableIdentity)
		}
		failed := *run
		failed.Candidate, failed.Gate = before.Candidate, before.Gate
		failed.Gate.Status = "failed"
		failed.Gate.StartedAt, failed.Gate.FinishedAt, failed.Gate.ArtifactRef = gateStarted.Format(time.RFC3339Nano), gateFinished.Format(time.RFC3339Nano), artifactRef
		failed.Gate.Failure = DepartureFailure{Class: string(route.Class), Identity: route.StableIdentity, OwningTaskID: packet.OwningTaskID, BisectionRef: packet.BisectionRef, ArtifactRefs: packet.ArtifactRefs, RepairTaskID: repairTaskID, ModelTriage: route.ModelTriage}
		failed.State = DepartureStateFailed
		failed.BlockReason = "promotion gate red: " + route.StableIdentity
		if changed, persistErr := store.TransitionDepartureRun(failed, run.StateRevision); persistErr != nil {
			return "", persistErr
		} else if !changed {
			return "", tuskerError(errorInvalidTransition, "promotion refusal: departure row changed while recording red gate")
		}
		*run = failed
		run.StateRevision++
		return "", tuskerError(errorInvalidTransition, "promotion gate red: "+gateSummary)
	}
	after, err := scheduledPromotionSnapshot(vaultPath, projectID, waveID, wf)
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
	final, err := scheduledPromotionSnapshot(vaultPath, projectID, waveID, wf)
	if err != nil {
		return "", err
	}
	if drift := scheduledPromotionSnapshotDrift(before, final); drift != "" {
		return "", tuskerError(errorInvalidTransition, "promotion recompute required: "+drift+"_drift")
	}
	if err := prepareV7WaveMembersForDefaultAdvance(repoRoot, vaultPath, before.DefaultBranch, wave); err != nil {
		return "", err
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
	intent.Promotion = DeparturePromotion{
		ExpectedRef: before.DefaultBranch, ExpectedSHA: before.Candidate.ExpectedDefaultBranchSHA,
		AttemptedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
	intent.State = DepartureStateGating
	changed, err := store.TransitionDepartureRun(intent, run.StateRevision)
	if err != nil {
		return "", err
	}
	if !changed {
		return "", tuskerError(errorInvalidTransition, "promotion refusal: departure row changed before ref update")
	}
	*run = intent
	run.StateRevision++
	if matched, err := store.ResourceLeaseMatches(lease.Name, lease.Owner, lease.Generation); err != nil || !matched {
		return "", tuskerError(errorInvalidTransition, "promotion refusal: full_gate_lease_fenced_at_ref_update")
	}
	if err := advanceV7DefaultBranch(repoRoot, before.DefaultBranch, mergeCommit, before.Candidate.ExpectedDefaultBranchSHA); err != nil {
		return "", tuskerError(errorInvalidTransition, "promotion refusal: default_ref_drift: "+err.Error())
	}
	if heartbeatErr := stopHeartbeat(); heartbeatErr != nil {
		return "", heartbeatErr
	}
	if err := scheduledPromotionAfterRefUpdate(); err != nil {
		return "", err
	}
	// Only durable success after the ref update counts as a promoted revision.
	next := *run
	next.Promotion.CommittedRef = before.DefaultBranch
	next.Promotion.CommittedSHA = mergeCommit
	next.Promotion.CommittedAt = time.Now().UTC().Format(time.RFC3339Nano)
	next.State = DepartureStatePromoted
	changed, err = store.TransitionDepartureRun(next, run.StateRevision)
	if err != nil {
		return "", err
	}
	if !changed {
		return "", tuskerError(errorInvalidTransition, "promotion committed but departure row changed concurrently; recover from committed ref "+mergeCommit)
	}
	*run = next
	run.StateRevision++
	leaseOutcome = "promotion passed"
	if err := appendScheduledPromotionAudit(vaultPath, waveID, before.DefaultBranch, mergeCommit, gateSummary, actor); err != nil {
		return "", err
	}
	return mergeCommit, nil
}
