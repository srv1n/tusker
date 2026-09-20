package main

import (
	"fmt"
	"net/http"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const directWaveReviewSchema = "tusker.wave-review/v1"
const directStartSchema = "tusker.direct-start/v1"

var directWaveStartInjectCrashBeforeQueue func() bool
var directWaveStartInjectAfterReview func()
var directWaveStartInjectCommitFailAfter int
var directTaskStartInjectBeforeLock func()

type directStartBlocker struct {
	Code   string `json:"code" yaml:"code"`
	TaskID string `json:"taskId,omitempty" yaml:"taskId,omitempty"`
	GateID string `json:"gateId,omitempty" yaml:"gateId,omitempty"`
	Reason string `json:"reason" yaml:"reason"`
	Action string `json:"action" yaml:"action"`
}

type directStartControl struct {
	Action  string `json:"action" yaml:"action"`
	Enabled bool   `json:"enabled" yaml:"enabled"`
	Scope   string `json:"scope" yaml:"scope"`
	Reason  string `json:"reason,omitempty" yaml:"reason,omitempty"`
}

// directWaveMemberRecovery names the single supported recovery action for a
// failed or proof-blocked member, mirroring the eligibility checks the
// recovery endpoints apply: a projected enabled action must actually admit.
// Actions map to existing operator surfaces: retry_task (run redrive),
// rerun_checks (verification recovery), retry_review (review lane recovery).
type directWaveMemberRecovery struct {
	Action      string `json:"action" yaml:"action"`
	Enabled     bool   `json:"enabled" yaml:"enabled"`
	Reason      string `json:"reason,omitempty" yaml:"reason,omitempty"`
	Attempts    int    `json:"attempts,omitempty" yaml:"attempts,omitempty"`
	MaxAttempts int    `json:"maxAttempts,omitempty" yaml:"maxAttempts,omitempty"`
}

type directWaveReviewMember struct {
	TaskID string `json:"taskId" yaml:"taskId"`
	Title  string `json:"title" yaml:"title"`
	State  string `json:"state" yaml:"state"`
	Phase  string `json:"phase,omitempty" yaml:"phase,omitempty"`
	Lane   string `json:"lane,omitempty" yaml:"lane,omitempty"`
	// Responsible names the actor that must act next: a lease owner for live
	// work, a gate owner for human-blocked work, "operator" when recovery needs
	// a human/operator decision, and "daemon" for waits the scheduler resolves.
	Responsible string `json:"responsible,omitempty" yaml:"responsible,omitempty"`
	// CompletionReported is deliberately separate from State. A task can have
	// reported completion but still lack current acceptance evidence.
	CompletionReported bool                        `json:"completionReported,omitempty" yaml:"completionReported,omitempty"`
	WaitingReason      string                      `json:"waitingReason,omitempty" yaml:"waitingReason,omitempty"`
	Recovery           *directWaveMemberRecovery   `json:"recovery,omitempty" yaml:"recovery,omitempty"`
	Dependencies       []string                    `json:"dependencies,omitempty" yaml:"dependencies,omitempty"`
	ExecuteRoute       string                      `json:"executeRoute,omitempty" yaml:"executeRoute,omitempty"`
	ReviewRoute        string                      `json:"reviewRoute,omitempty" yaml:"reviewRoute,omitempty"`
	Acceptance         []string                    `json:"acceptance,omitempty" yaml:"acceptance,omitempty"`
	Verification       []string                    `json:"verification,omitempty" yaml:"verification,omitempty"`
	Instructions       string                      `json:"instructions,omitempty" yaml:"instructions,omitempty"`
	ProofInvalidation  *v7VerificationInvalidation `json:"proofInvalidation,omitempty" yaml:"proofInvalidation,omitempty"`
}

type directWaveReviewHumanAction struct {
	TaskID    string           `json:"taskId" yaml:"taskId"`
	TaskTitle string           `json:"taskTitle" yaml:"taskTitle"`
	Action    serveHumanAction `json:"action" yaml:"action"`
}

type directWaveReview struct {
	Schema              string                        `json:"schema" yaml:"schema"`
	WaveID              string                        `json:"waveId" yaml:"waveId"`
	Title               string                        `json:"title" yaml:"title"`
	Outcome             string                        `json:"outcome" yaml:"outcome"`
	State               string                        `json:"state" yaml:"state"`
	Authorization       string                        `json:"authorization" yaml:"authorization"`
	MaterialFingerprint string                        `json:"materialFingerprint" yaml:"materialFingerprint"`
	Members             []directWaveReviewMember      `json:"members" yaml:"members"`
	Frontiers           [][]string                    `json:"frontiers" yaml:"frontiers"`
	HumanActions        []directWaveReviewHumanAction `json:"humanActions,omitempty" yaml:"humanActions,omitempty"`
	Blockers            []directStartBlocker          `json:"blockers" yaml:"blockers"`
	Controls            []directStartControl          `json:"controls" yaml:"controls"`
}

func directWaveHumanActionProjection(idx v7Index, members []string) []directWaveReviewHumanAction {
	snap := serveSnapshot{gates: make([]Note, 0, len(idx.Gates))}
	for _, gate := range idx.Gates {
		snap.gates = append(snap.gates, gate)
	}
	seen := map[string]bool{}
	actions := []directWaveReviewHumanAction{}
	for _, id := range members {
		task, ok := idx.Tasks[id]
		if !ok {
			continue
		}
		for _, action := range serveHumanActionsForTask(snap, task) {
			if seen[action.GateID] {
				continue
			}
			seen[action.GateID] = true
			actions = append(actions, directWaveReviewHumanAction{TaskID: id, TaskTitle: stringField(task.Data, "title"), Action: action})
		}
	}
	sort.Slice(actions, func(i, j int) bool { return actions[i].Action.GateID < actions[j].Action.GateID })
	return actions
}

type directStartResult struct {
	Schema              string               `json:"schema" yaml:"schema"`
	Subject             string               `json:"subject" yaml:"subject"`
	Scope               string               `json:"scope" yaml:"scope"`
	State               string               `json:"state" yaml:"state"`
	Authorization       string               `json:"authorization" yaml:"authorization"`
	MaterialFingerprint string               `json:"materialFingerprint,omitempty" yaml:"materialFingerprint,omitempty"`
	Reason              string               `json:"reason,omitempty" yaml:"reason,omitempty"`
	QueuedTaskIDs       []string             `json:"queuedTaskIds,omitempty" yaml:"queuedTaskIds,omitempty"`
	ClaimedTaskIDs      []string             `json:"claimedTaskIds,omitempty" yaml:"claimedTaskIds,omitempty"`
	Replayed            bool                 `json:"replayed" yaml:"replayed"`
	Blockers            []directStartBlocker `json:"blockers,omitempty" yaml:"blockers,omitempty"`
	Controls            []directStartControl `json:"controls,omitempty" yaml:"controls,omitempty"`
}

func directWaveTaskContract(task Note) string {
	// Always recompute from the current document: directive admission must bind
	// the contract bytes that are actually present, not a stored pin that an
	// out-of-band edit may have left behind. The canonical body excludes
	// lifecycle ledger content (evidence, work log, generated reviewer
	// findings, verification receipts), so sanctioned lifecycle writes keep the
	// same contract while authored edits drift it.
	return directWaveTaskContractFingerprint(task.Data, task.Body)
}

// directWaveTaskContractStaleReason reports why a task record must not be
// admitted under a previously queued or consumed authorization: its stored
// contract pin no longer matches the current contract material, or the
// document was rewritten outside the CAS ledger. It returns "" when the record
// is internally consistent.
func directWaveTaskContractStaleReason(task Note) string {
	id := stringField(task.Data, "id")
	if stored := strings.TrimSpace(stringField(task.Data, "contract_fingerprint")); stored != "" && stored != directWaveTaskContractFingerprint(task.Data, task.Body) {
		return "task " + id + " contract drifted from its stored contract_fingerprint; rebind with `tusker task update` or restore the authored bytes"
	}
	if !v7StateRevMatches(task.Data, task.Body, stringField(task.Data, "state_rev")) {
		return "task " + id + " state_rev does not match current bytes; reconcile the record before dispatch"
	}
	return ""
}

// directWaveAuthorizationCurrent reports whether the wave record's stored
// authorization is in the expected state and still covers the recomputed
// material fingerprint.
func directWaveAuthorizationCurrent(data map[string]any, state, fingerprint string) bool {
	return stringField(data, "authorization") == state &&
		stringField(data, "authorization_fingerprint") != "" &&
		stringField(data, "authorization_fingerprint") == fingerprint &&
		stringField(data, "authorized_at") != ""
}

func directWaveReviewRoutes(task Note, wf Workflow) (execute, review string, execErr, reviewErr error) {
	exec := routePreviewForNote(task, wf, runLaneExecute)
	if len(exec.Blockers) > 0 {
		execErr = fmt.Errorf("%s", strings.Join(exec.Blockers, "; "))
	} else {
		execute = firstNonEmpty(exec.Profile, exec.Harness)
	}
	rev := routePreviewForNote(task, wf, runLaneReview)
	if len(rev.Blockers) > 0 {
		reviewErr = fmt.Errorf("%s", strings.Join(rev.Blockers, "; "))
	} else {
		review = firstNonEmpty(rev.Profile, rev.Harness)
	}
	return execute, review, execErr, reviewErr
}

func directWaveMemberAcceptance(task Note) []string {
	var rows []string
	for _, row := range serveAcceptanceRows(task) {
		rows = append(rows, strings.TrimSpace(row.ID+" "+row.Text))
	}
	return rows
}

func directWaveMemberVerification(task Note) []string {
	var rows []string
	for _, row := range parseV7VerificationRows(task.Body) {
		rows = append(rows, strings.TrimSpace(row.CoverText+" "+row.Check))
	}
	return rows
}

func directWaveFrontiers(members []string, idx v7Index) ([][]string, []string) {
	memberSet := makeSet(members...)
	remaining := makeSet(members...)
	done := map[string]bool{}
	var layers [][]string
	for len(remaining) > 0 {
		var layer []string
		for _, id := range members {
			if _, ok := remaining[id]; !ok {
				continue
			}
			ready := true
			if task, ok := idx.Tasks[id]; ok {
				for _, raw := range normalizeList(task.Data["dependencies"]) {
					edge := parseV7DependencyEdge(raw)
					_, inWave := memberSet[edge.ID]
					if inWave && !done[edge.ID] {
						ready = false
						break
					}
				}
			}
			if ready {
				layer = append(layer, id)
			}
		}
		if len(layer) == 0 {
			var cycle []string
			for id := range remaining {
				cycle = append(cycle, id)
			}
			sort.Strings(cycle)
			return nil, cycle
		}
		sort.Strings(layer)
		for _, id := range layer {
			done[id] = true
			delete(remaining, id)
		}
		layers = append(layers, layer)
	}
	return layers, nil
}

func directWaveMemberDependencyWait(task Note, idx v7Index) string {
	if edge, blocked := v7BlockingDependencyForReadiness(task, idx); blocked {
		return edge.ID
	}
	return ""
}

func directWaveStrictBlocker(vaultPath string, idx v7Index, task Note, wave Note) string {
	return v7VerificationReceiptRequirementMissing(vaultPath, task)
}

func directWaveProofBlocker(vaultPath string, task Note) (code, reason string) {
	cause := v7VerificationReceiptInvalidation(vaultPath, task)
	if cause == nil {
		return "", ""
	}
	reason = cause.Explanation
	switch cause.Kind {
	case "missing":
		code = "STRICT_PROOF_MISSING"
	case "failed":
		code = "STRICT_PROOF_FAILED"
	case "unavailable":
		code = "STRICT_PROOF_UNAVAILABLE"
	default:
		code = "STRICT_PROOF_STALE"
	}
	return code, reason
}

func directWaveDependencyContractBlocker(task Note, idx v7Index) string {
	contracts, _ := task.Data["dependency_contracts"].([]any)
	edges := map[string]string{}
	for _, raw := range normalizeList(task.Data["dependencies"]) {
		edge := parseV7DependencyEdge(raw)
		edges[edge.ID] = fallback(edge.Hardness, "hard")
	}
	seen := map[string]int{}
	for _, raw := range contracts {
		entry, ok := raw.(map[string]any)
		if !ok {
			return "dependency_contracts contains a malformed entry"
		}
		target := stringField(entry, "task_id")
		kind := fallback(strings.ToLower(stringField(entry, "kind")), "hard")
		if edges[target] == "" {
			return "dependency_contract " + target + " has no matching dependency edge"
		}
		if edges[target] != kind {
			return "dependency_contract " + target + " kind " + kind + " does not match edge " + edges[target]
		}
		targetTask, ok := idx.Tasks[target]
		if !ok {
			return "dependency_contract target " + target + " does not resolve"
		}
		if directWaveTaskContract(targetTask) != stringField(entry, "target_contract_fingerprint") {
			return "dependency_contract " + target + " target fingerprint drifted; rerun migration or rebind the dependency"
		}
		seen[target]++
	}
	for target, count := range seen {
		if count > 1 {
			return "duplicate dependency_contract entries for " + target
		}
	}
	consumerWave := stringField(task.Data, "wave")
	for target := range edges {
		targetTask, ok := idx.Tasks[target]
		if !ok {
			continue
		}
		if stringField(targetTask.Data, "wave") != consumerWave && seen[target] == 0 {
			return "cross-wave dependency " + target + " requires a dependency_contract entry"
		}
	}
	return ""
}

func directWaveLiveRun(runs map[string]RunStatus, recordID string, now time.Time) *RunStatus {
	run, ok := runs[recordID]
	if !ok || run.Terminal || runFreshness(&run, now) != "fresh" {
		return nil
	}
	if isDispatchingLeaseState(run.LeaseState) || LeaseState(strings.TrimSpace(run.LeaseState)) == LeaseStateRunning {
		return &run
	}
	return nil
}

func directWaveReviewRuntimeStore() (*RuntimeStore, error) {
	stateRoot := DefaultStateRoot()
	if !fileExists(runtimeStoreDBPath(stateRoot)) {
		return nil, fmt.Errorf("runtime store does not exist")
	}
	return OpenRuntimeStoreReadOnly(stateRoot)
}

func directWaveRunAdmittedByWave(store *RuntimeStore, run RunStatus, waveID, fingerprint, authorizedAt string) bool {
	if store == nil || fingerprint == "" || run.ProjectID == "" || run.RecordID == "" || run.LeaseGeneration <= 0 || run.ActiveAttemptID == "" {
		return false
	}
	directive, err := store.RunDirective(run.ProjectID, run.RecordID)
	if err != nil || directive == nil || directive.WaveID != waveID || directive.AuthorizationFingerprint != fingerprint || directive.WaveAuthorizedAt != authorizedAt {
		return false
	}
	auth, err := store.LatestRunAuthorization(run.ProjectID, run.RecordID)
	if err != nil || auth == nil {
		return false
	}
	return auth.LeaseGeneration == run.LeaseGeneration && auth.AttemptID == run.ActiveAttemptID && auth.Source == "human_run_directive"
}

func buildDirectWaveReview(vaultPath string, store *RuntimeStore, projectID, waveID string, runtimeErr error) (directWaveReview, error) {
	review := directWaveReview{
		Schema:       directWaveReviewSchema,
		WaveID:       waveID,
		Members:      []directWaveReviewMember{},
		Frontiers:    [][]string{},
		HumanActions: []directWaveReviewHumanAction{},
		Blockers:     []directStartBlocker{},
		Controls:     []directStartControl{},
	}
	idx, err := loadV7Index(vaultPath)
	if err != nil {
		return review, err
	}
	wave, ok := idx.Waves[waveID]
	if !ok {
		review.Blockers = append(review.Blockers, directStartBlocker{Code: "WAVE_MISSING", Reason: "wave " + waveID + " does not resolve", Action: "choose an existing wave id"})
		return review, nil
	}
	review.Title = stringField(wave.Data, "title")
	review.Outcome = firstNonEmpty(stringField(wave.Data, "outcome"), stringField(wave.Data, "summary"))
	waveStatus := strings.ToLower(stringField(wave.Data, "status"))
	waveTerminal := waveStatus == "cancelled" || waveStatus == "superseded"
	if waveTerminal {
		review.Blockers = append(review.Blockers, directStartBlocker{Code: "WAVE_TERMINAL", Reason: "wave " + waveID + " is " + waveStatus, Action: "create a new wave for new work"})
	}
	members := uniqueStrings(normalizeList(wave.Data["members"]))
	sort.Strings(members)
	review.HumanActions = directWaveHumanActionProjection(idx, members)
	fingerprint, issues := waveMaterialFingerprint(vaultPath, idx, wave)
	review.MaterialFingerprint = fingerprint
	for _, issue := range issues {
		review.Blockers = append(review.Blockers, directStartBlocker{Code: "MATERIAL_INVALID", Reason: issue, Action: "repair the wave material and rerun wave review"})
	}
	authorization := waveAuthorizationProjection(vaultPath, idx, wave)
	authState := stringField(authorization, "state")
	stale := boolFromAny(authorization["stale"])
	armedFingerprint := stringField(authorization, "authorizedFingerprint")
	armedAt := stringField(authorization, "at")
	switch {
	case stale:
		review.Authorization = "stale"
	case authState == "armed":
		review.Authorization = "authorized"
	case authState == "paused":
		review.Authorization = "paused"
	default:
		review.Authorization = "inert"
	}
	var wf Workflow
	var wfErr error
	if wfFile, loadErr := loadWorkflow(vaultPath); loadErr == nil {
		wf = wfFile.Data
	} else {
		wfErr = loadErr
	}
	// Runtime rows are keyed by the registered project id, which may differ from
	// the authored project field in .tusker/config.yaml (resolveV7ProjectID).
	// Normalize to the registered id so run/directive lookups never silently
	// miss every row.
	if store != nil {
		if registeredID, registered, regErr := registeredProjectIDForVault(store, vaultPath); regErr == nil && registered && strings.TrimSpace(registeredID) != "" {
			projectID = registeredID
		}
	}
	runs := map[string]RunStatus{}
	var projectRuns []RunStatus
	var directives []RunDirective
	if runtimeErr != nil {
		review.Blockers = append(review.Blockers, directStartBlocker{Code: "RUNTIME_UNAVAILABLE", Reason: "runtime state is unavailable: " + runtimeErr.Error(), Action: "restore the runtime store and rerun wave review"})
	} else if store == nil {
		review.Blockers = append(review.Blockers, directStartBlocker{Code: "RUNTIME_UNAVAILABLE", Reason: "runtime state is unavailable: runtime store is missing", Action: "restore the runtime store and rerun wave review"})
	} else {
		if list, listErr := store.ListRuns(); listErr != nil {
			review.Blockers = append(review.Blockers, directStartBlocker{Code: "RUNTIME_UNAVAILABLE", Reason: "runtime state is unavailable: " + listErr.Error(), Action: "restore the runtime store and rerun wave review"})
		} else {
			for _, run := range list {
				if projectID != "" && run.ProjectID != projectID {
					continue
				}
				projectRuns = append(projectRuns, run)
				// Runtime rows are canonically scoped by project + tracker record.
				// ItemID is display identity and may differ from the persisted key.
				key := firstNonEmpty(run.RecordID, run.ItemID)
				if key != "" {
					runs[key] = run
				}
			}
		}
		if list, directiveErr := store.ListActiveRunDirectives(projectID, time.Now().UTC()); directiveErr == nil {
			directives = list
		}
	}
	// Frontier occupancy mirrors queueAuthorizedWaveFrontier: a member holding
	// a dispatch-capacity lease (claimed/running/retry_queued) or a queued
	// directive bound to the current authorization occupies a wave slot, while
	// only claimed/running rows consume the project's active-run budget. A
	// dispatchable member past either ceiling is capacity-waiting, not failed.
	recordToMember := map[string]string{}
	for _, memberID := range members {
		if memberTask, ok := idx.Tasks[memberID]; ok {
			recordToMember[trackerRecordID(memberTask)] = memberID
		}
	}
	occupied := map[string]bool{}
	// occupiedWaveBound marks members whose only occupancy is a queued
	// wave-bound directive: that directive cannot dispatch while the wave is
	// paused (it was never consumed, so admitted-lease continuity does not
	// apply), but a task-scoped start replaces it and dispatches immediately.
	occupiedWaveBound := map[string]bool{}
	for _, run := range projectRuns {
		if taskID, ok := recordToMember[firstNonEmpty(run.RecordID, run.ItemID)]; ok && isDispatchCapacityLeaseState(run.LeaseState) {
			occupied[taskID] = true
		}
	}
	for _, directive := range directives {
		taskID, ok := recordToMember[directive.RecordID]
		if !ok {
			continue
		}
		// A directive bound to this wave's superseded authorization can never
		// match again and is rebound on queue, so it holds no slot.
		if directive.WaveID == waveID && (directive.AuthorizationFingerprint != fingerprint || directive.WaveAuthorizedAt != armedAt) {
			continue
		}
		occupied[taskID] = true
		if directive.WaveID == waveID {
			occupiedWaveBound[taskID] = true
		}
	}
	waveLimit := intField(wave.Data, "concurrency")
	if waveLimit <= 0 {
		waveLimit = maxInt(1, len(members))
	}
	waveOccupied := 0
	for _, id := range members {
		if occupied[id] {
			waveOccupied++
		}
	}
	projectActive := 0
	for _, run := range projectRuns {
		if runConsumesDispatchCapacity(run) {
			projectActive++
		}
	}
	projectCap := projectActiveRunLimit(wf)
	landed := armedWaveLandedMembers(wave)
	frontiers, cycle := directWaveFrontiers(members, idx)
	if cycle != nil {
		review.Blockers = append(review.Blockers, directStartBlocker{Code: "MATERIAL_CYCLIC", Reason: "member dependency cycle: " + strings.Join(cycle, ", "), Action: "remove the cyclic dependency edge"})
	}
	review.Frontiers = frontiers
	routeUnavailable := wfErr != nil
	if routeUnavailable {
		review.Blockers = append(review.Blockers, directStartBlocker{Code: "ROUTE_UNAVAILABLE", Reason: "route resolution unavailable: " + runnerRouteBlocker(wfErr), Action: "repair WORKFLOW.md route configuration"})
	}
	for _, id := range members {
		member := directWaveReviewMember{TaskID: id}
		task, ok := idx.Tasks[id]
		if !ok {
			member.State = "waiting"
			member.WaitingReason = "task record is missing"
			review.Blockers = append(review.Blockers, directStartBlocker{Code: "MATERIAL_INVALID", TaskID: id, Reason: "member task does not resolve", Action: "restore the task record or remove it from the wave"})
			review.Members = append(review.Members, member)
			continue
		}
		member.Title = stringField(task.Data, "title")
		member.Dependencies = normalizeList(task.Data["dependencies"])
		member.Instructions = strings.TrimSpace(task.Body)
		member.Acceptance = directWaveMemberAcceptance(task)
		member.Verification = directWaveMemberVerification(task)
		if !routeUnavailable {
			exec, rev, execErr, revErr := directWaveReviewRoutes(task, wf)
			member.ExecuteRoute, member.ReviewRoute = exec, rev
			if execErr != nil || revErr != nil {
				reason := "route blocked"
				if execErr != nil {
					reason = "execute route blocked: " + execErr.Error()
				} else {
					reason = "review route blocked: " + revErr.Error()
				}
				member.State = "waiting"
				member.WaitingReason = reason
				review.Blockers = append(review.Blockers, directStartBlocker{Code: "ROUTE_INVALID", TaskID: id, Reason: reason, Action: "repair the execute/review route configuration for " + id})
			}
		}
		taskStale := directWaveTaskContractStaleReason(task)
		if taskStale != "" {
			review.Blockers = append(review.Blockers, directStartBlocker{Code: "CONTRACT_FINGERPRINT_STALE", TaskID: id, Reason: taskStale, Action: "rebind the task contract fingerprint with tusker task update"})
		}
		if reason := directWaveDependencyContractBlocker(task, idx); reason != "" {
			review.Blockers = append(review.Blockers, directStartBlocker{Code: "DEPENDENCY_CONTRACT_INVALID", TaskID: id, Reason: reason, Action: "rebind the dependency contract for " + id})
		}
		recordID := trackerRecordID(task)
		storedRun, hasStoredRun := runs[firstNonEmpty(recordID, id)]
		member.ProofInvalidation = v7VerificationReceiptInvalidation(vaultPath, task)
		workspaceErr := error(nil)
		if store != nil && hasStoredRun {
			var workspace *v7VerificationWorkspace
			if workspace, workspaceErr = recoveryCommandVerificationWorkspace(store, vaultPath, task, storedRun); workspaceErr == nil {
				member.ProofInvalidation = v7VerificationReceiptInvalidationForWorkspace(vaultPath, task, workspace)
			}
		}
		proofCode, proofStale := "", ""
		if member.ProofInvalidation != nil {
			proofStale = member.ProofInvalidation.Explanation
			switch member.ProofInvalidation.Kind {
			case "missing":
				proofCode = "STRICT_PROOF_MISSING"
			case "failed":
				proofCode = "STRICT_PROOF_FAILED"
			case "unavailable":
				proofCode = "STRICT_PROOF_UNAVAILABLE"
			default:
				proofCode = "STRICT_PROOF_STALE"
			}
		}
		if proofStale != "" && strings.EqualFold(stringField(task.Data, "status"), "done") {
			review.Blockers = append(review.Blockers, directStartBlocker{Code: proofCode, TaskID: id, Reason: proofStale, Action: "re-run current command proof for " + id})
		}
		liveRun := directWaveLiveRun(runs, firstNonEmpty(recordID, id), time.Now().UTC())
		owner := ""
		if liveRun != nil {
			owner = firstNonEmpty(liveRun.LeaseOwner, "active run")
			member.Lane = liveRun.Lane
		}
		admitted := liveRun != nil && !stale && (authState == "armed" || authState == "paused") && directWaveRunAdmittedByWave(store, *liveRun, waveID, armedFingerprint, armedAt)
		status := strings.ToLower(stringField(task.Data, "status"))
		member.CompletionReported = status == "done"
		currentReviewResult := false
		if store != nil && hasStoredRun && storedRun.Lane == runLaneReview {
			currentReviewResult, _ = store.HasReviewResultForWork(projectID, recordID, intField(task.Data, "work_revision"), stringField(task.Data, "state_rev"))
		}
		depWait := directWaveMemberDependencyWait(task, idx)
		switch {
		// Contract staleness dominates lifecycle status: a done member whose
		// stored pin no longer covers its bytes cannot certify the wave as
		// complete, and a stale member is never startable. Live admitted work
		// still reports running — the recorded blocker flags the drift.
		case taskStale == "" && proofStale == "" && (landed[id] && status == "done" || status == "done"):
			member.State = "completed"
			member.Phase = "completed"
		case owner != "" && admitted && liveRun.Lane == runLaneReview:
			member.State = "reviewing"
			member.Phase = "reviewing"
			member.WaitingReason = "active reviewer " + owner
		case hasStoredRun && storedRun.Lane == runLaneReview && status != "rework" && AttemptOutcome(storedRun.AttemptOutcome) != AttemptOutcomeNone && AttemptOutcome(storedRun.AttemptOutcome) != AttemptOutcomeSucceeded && strings.TrimSpace(storedRun.AttemptOutcome) != "":
			member.State = "blocked"
			member.Phase = "failed"
			member.Lane = storedRun.Lane
			member.WaitingReason = firstNonEmpty(storedRun.LastError, "runtime "+storedRun.AttemptOutcome)
			review.Blockers = append(review.Blockers, directStartBlocker{Code: "RUNTIME_FAILED", TaskID: id, Reason: member.WaitingReason, Action: "inspect the failed review attempt for " + id})
		case status == "review" && hasStoredRun && storedRun.Terminal && storedRun.Lane == runLaneReview && !currentReviewResult:
			member.State = "blocked"
			member.Phase = "failed"
			member.Lane = storedRun.Lane
			member.WaitingReason = "the previous review result does not match the current task snapshot"
			review.Blockers = append(review.Blockers, directStartBlocker{Code: "REVIEW_SNAPSHOT_STALE", TaskID: id, Reason: member.WaitingReason, Action: "retry independent review for " + id})
		case status == "review" && proofStale != "":
			// A submitted member whose recorded checks no longer cover current
			// material is blocked on proof, not on a reviewer: re-running the
			// checks is the one supported action. Review-lane failures above
			// keep their own diagnosis and recovery.
			member.State = "waiting"
			member.Phase = "proof_blocked"
			member.WaitingReason = proofStale
		case status == "review":
			member.State = "waiting"
			member.Phase = "awaiting_review"
			member.WaitingReason = firstNonEmpty(member.WaitingReason, "awaiting independent review")
		case owner != "" && admitted:
			member.State = "running"
			member.Phase = "executing"
			member.WaitingReason = "active owner " + owner
		case status == "done" && proofStale != "":
			member.State = "waiting"
			member.Phase = "proof_blocked"
			member.WaitingReason = proofStale
		case hasStoredRun && storedRun.Terminal && AttemptOutcome(storedRun.AttemptOutcome) != AttemptOutcomeNone && AttemptOutcome(storedRun.AttemptOutcome) != AttemptOutcomeSucceeded && strings.TrimSpace(storedRun.AttemptOutcome) != "" && !(status == "rework" && storedRun.Lane == runLaneReview):
			member.State = "blocked"
			member.Phase = "failed"
			member.Lane = storedRun.Lane
			member.WaitingReason = firstNonEmpty(storedRun.LastError, "runtime "+storedRun.AttemptOutcome)
			review.Blockers = append(review.Blockers, directStartBlocker{Code: "RUNTIME_FAILED", TaskID: id, Reason: member.WaitingReason, Action: "inspect the failed " + firstNonEmpty(storedRun.Lane, "runtime") + " attempt for " + id})
		case taskStale != "":
			member.State = "waiting"
			member.WaitingReason = "task contract drifted from its stored fingerprint; rebind required"
		case member.State == "waiting":
		case status == "rework":
			// A reviewer requested changes: this is ordinary implementation
			// state, not an infrastructure or wave-setup failure. The execute
			// lane re-admits it through the normal frontier.
			member.State = "ready"
			member.Phase = "rework"
			member.WaitingReason = "review requested changes; another implementation attempt is dispatchable"
		case depWait != "":
			// Dependency waiting is expected DAG behavior: a member state, not
			// a diagnostic blocker.
			member.State = "waiting"
			member.WaitingReason = "waiting for dependency " + depWait
		case armedWaveTaskHumanBlocked(idx, task):
			member.State = "waiting"
			member.WaitingReason = "open blocking human gate"
			gateID := ""
			for _, action := range review.HumanActions {
				if containsString(action.Action.BlockedTaskIDs, id) {
					gateID = action.Action.GateID
					break
				}
			}
			review.Blockers = append(review.Blockers, directStartBlocker{Code: "HUMAN_GATE_OPEN", TaskID: id, GateID: gateID, Reason: "open human gate " + gateID + " blocks " + id, Action: "open " + id + " in the Tusker Mac app and confirm " + gateID})
		default:
			member.State = "ready"
		}
		if owner != "" && !admitted {
			member.State = "waiting"
			member.WaitingReason = firstNonEmpty(member.WaitingReason, "task is held by "+owner)
			review.Blockers = append(review.Blockers, directStartBlocker{Code: "ACTIVE_OWNER", TaskID: id, Reason: "task is held by " + owner + " outside this wave's authorization", Action: "wait for the owner to release or reclaim the lease"})
		}
		// Capacity annotation: a dispatchable member that already holds a
		// frontier slot (queued directive or held lease) is waiting on the
		// daemon's next claim, and a dispatchable member past the wave
		// concurrency ceiling is waiting on a sibling slot. Neither is a
		// failure or a setup problem — the scheduler resolves them.
		if member.State == "ready" {
			switch {
			case review.Authorization == "paused":
				// A paused wave admits no new frontier work, but every member
				// stays dispatchable through a task-scoped start — the one
				// supported pause bypass. Keep the member ready so that
				// control stays enabled; the phase and reason carry the pause.
				member.Phase = "paused"
				member.Responsible = "operator"
				switch {
				case occupiedWaveBound[id]:
					member.WaitingReason = "queued under wave authorization; dispatches when the wave resumes, or sooner through a task-scoped start"
				case occupied[id]:
					member.Phase = "queued"
					member.Responsible = "daemon"
					member.WaitingReason = "queued for dispatch"
				default:
					member.WaitingReason = "wave is paused; a task-scoped start dispatches without resuming, and resume re-admits queued work but does not retry failures"
				}
			case occupied[id]:
				member.State = "waiting"
				member.Responsible = "daemon"
				if member.Phase == "" {
					member.Phase = "queued"
				}
				if projectCap > 0 && projectActive >= projectCap {
					if member.Phase == "queued" {
						member.Phase = "capacity_wait"
					}
					slotReason := fmt.Sprintf("waiting for an execution slot — project capacity %d/%d in use", projectActive, projectCap)
					if member.WaitingReason != "" {
						member.WaitingReason += "; " + slotReason
					} else {
						member.WaitingReason = slotReason
					}
				} else {
					member.WaitingReason = firstNonEmpty(member.WaitingReason, "queued for dispatch")
				}
			case waveOccupied >= waveLimit && (review.Authorization == "authorized" || review.Authorization == "stale"):
				// Past the wave concurrency ceiling nothing is queued for this
				// member, so a task-scoped start still dispatches — keep the
				// member ready and let the phase carry the capacity wait.
				if member.Phase == "" {
					member.Phase = "capacity_wait"
				}
				member.Responsible = "daemon"
				slotReason := fmt.Sprintf("waiting for a wave execution slot — concurrency %d/%d in use", waveOccupied, waveLimit)
				if member.WaitingReason != "" {
					member.WaitingReason += "; " + slotReason
				} else {
					member.WaitingReason = slotReason
				}
			}
		}
		member.Recovery = directWaveMemberRecoveryFor(task, wave, storedRun, hasStoredRun, workspaceErr, member, wf.Retry.MaxAttempts)
		review.Members = append(review.Members, member)
	}
	// Dependency waits resolve through the dependency's own lifecycle: when the
	// dependency is progressing the daemon owns the wait; when it failed or is
	// missing the operator acts on the dependency's recovery, not this member.
	memberByID := make(map[string]*directWaveReviewMember, len(review.Members))
	for i := range review.Members {
		memberByID[review.Members[i].TaskID] = &review.Members[i]
	}
	for i := range review.Members {
		m := &review.Members[i]
		if m.Responsible != "" || !strings.HasPrefix(m.WaitingReason, "waiting for dependency ") {
			continue
		}
		depID := strings.TrimSpace(strings.TrimPrefix(m.WaitingReason, "waiting for dependency "))
		if dep, ok := memberByID[depID]; ok {
			switch {
			case dep.State == "running" || dep.State == "reviewing" || dep.Phase == "queued" || dep.Phase == "capacity_wait":
				m.Responsible = "daemon"
			default:
				m.Responsible = "operator"
			}
		}
	}
	anyRunning, allDone := false, len(members) > 0
	for _, member := range review.Members {
		if member.State == "running" || member.State == "reviewing" {
			anyRunning = true
		}
		if member.State != "completed" {
			allDone = false
		}
	}
	switch {
	case waveTerminal:
		review.State = "Cancelled"
	case allDone:
		review.State = "Completed"
	case review.Authorization == "paused":
		review.State = "Paused"
	case anyRunning:
		review.State = "Running"
	case review.Authorization == "authorized" || review.Authorization == "stale":
		review.State = "Waiting"
	default:
		review.State = "Planned"
	}
	globalBlocked := false
	for _, blocker := range review.Blockers {
		if blocker.TaskID == "" {
			globalBlocked = true
		}
	}
	switch {
	case review.State == "Cancelled":
		review.Controls = append(review.Controls, directStartControl{Action: "wave start", Enabled: false, Scope: waveID, Reason: "wave is " + waveStatus})
	case review.State == "Completed":
		review.Controls = append(review.Controls, directStartControl{Action: "wave start", Enabled: false, Scope: waveID, Reason: "wave is already complete"})
	case review.State == "Paused":
		review.Controls = append(review.Controls, directStartControl{Action: "wave resume", Enabled: true, Scope: waveID, Reason: "wave is paused; resume restores the exact stored authorization"})
	case review.Authorization == "authorized":
		review.Controls = append(review.Controls, directStartControl{Action: "wave pause", Enabled: true, Scope: waveID, Reason: "admitted attempts may finish; no new wave-owned workers or reviewers will start"})
	default:
		startReason := ""
		if globalBlocked {
			startReason = "resolve global blockers before wave start"
		}
		review.Controls = append(review.Controls, directStartControl{Action: "wave start", Enabled: !globalBlocked, Scope: waveID, Reason: startReason})
	}
	for _, member := range review.Members {
		control := directStartControl{Action: "task start", Enabled: !waveTerminal && (member.State == "ready" || member.State == "planned"), Scope: member.TaskID}
		if !control.Enabled {
			control.Reason = firstNonEmpty(member.WaitingReason, "task is not eligible")
			if waveTerminal {
				control.Reason = "wave is " + waveStatus
			}
		} else if review.State == "Paused" {
			control.Reason = "wave " + waveID + " remains paused; this start is task-scoped only"
		}
		review.Controls = append(review.Controls, control)
	}
	return review, nil
}

// directWaveMemberRecoveryFor names the one supported recovery action for a
// failed or proof-blocked member and mirrors the same eligibility the recovery
// endpoints enforce, so a projected enabled action actually admits. Actions
// map to existing operator surfaces: retry_task is `tusker redrive` (or the
// serve redrive endpoint), rerun_checks is verification recovery, and
// retry_review is the review-lane recovery queue.
func directWaveMemberRecoveryFor(task Note, wave Note, run RunStatus, hasRun bool, workspaceErr error, member directWaveReviewMember, maxAttempts int) *directWaveMemberRecovery {
	status := strings.ToLower(strings.TrimSpace(stringField(task.Data, "status")))
	recovery := &directWaveMemberRecovery{Attempts: run.AttemptCount, MaxAttempts: maxAttempts}
	switch member.Phase {
	case "proof_blocked":
		recovery.Action = "rerun_checks"
		switch {
		case !hasRun:
			recovery.Reason = "no submitted run workspace to re-verify"
		case workspaceErr != nil:
			recovery.Reason = workspaceErr.Error()
		default:
			// rerun_checks ends by queueing the review lane, so the review
			// window's operational blockers are rerun_checks blockers too.
			if blocker := reviewRecoveryOperationalBlocker(wave, run, maxAttempts); blocker != "" {
				recovery.Reason = blocker
			} else {
				recovery.Enabled = true
			}
		}
	case "failed":
		if member.Lane == runLaneReview {
			recovery.Action = "retry_review"
			switch {
			case !hasRun || run.Lane != runLaneReview:
				recovery.Reason = "no failed review lane to retry"
			case status != "review":
				recovery.Reason = "task is not awaiting review (status " + fallback(status, "missing") + ")"
			default:
				if blocker := reviewRecoveryOperationalBlocker(wave, run, maxAttempts); blocker != "" {
					recovery.Reason = blocker
				} else {
					recovery.Enabled = true
				}
			}
		} else {
			recovery.Action = "retry_task"
			switch refused, reason := serveRedriveRefusal(status, run); {
			case !hasRun:
				recovery.Reason = "no run to retry"
			case directWaveTaskContractStaleReason(task) != "":
				recovery.Reason = "task contract drifted from its stored fingerprint; rebind before retry"
			case refused:
				recovery.Reason = reason
			default:
				recovery.Enabled = true
				if strings.EqualFold(stringField(wave.Data, "authorization"), "paused") {
					recovery.Reason = "wave is paused; the retried attempt dispatches on its already-admitted authorization"
				}
			}
		}
	default:
		return nil
	}
	return recovery
}

func waveReviewCmd(args Args) error {
	vault, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	waveID := strings.ToUpper(strings.TrimSpace(firstNonEmpty(args.String("id"), args.String("_pos0"))))
	if waveID == "" {
		return tuskerError(errorMissingArg, "Usage: tusker wave review <WAVE-ID> [--json]")
	}
	store, runtimeErr := directWaveReviewRuntimeStore()
	if store != nil {
		defer store.Close()
	}
	projectID, _ := resolveV7ProjectID(vault)
	review, err := buildDirectWaveReview(vault, store, projectID, waveID, runtimeErr)
	if err != nil {
		return err
	}
	if args.Bool("json") {
		emitJSON(review)
		return nil
	}
	if args.Bool("quiet") {
		return nil
	}
	fmt.Printf("%s %s — %s (authorization %s)\n", review.WaveID, review.Title, review.State, review.Authorization)
	for _, member := range review.Members {
		line := "  " + member.TaskID + " " + member.State
		if member.WaitingReason != "" {
			line += " (" + member.WaitingReason + ")"
		}
		fmt.Println(line)
	}
	for _, blocker := range review.Blockers {
		fmt.Printf("  blocker %s %s: %s — %s\n", blocker.Code, blocker.TaskID, blocker.Reason, blocker.Action)
	}
	return nil
}

func directStartActor(args Args, operation string) (string, error) {
	actor := strings.TrimSpace(firstNonEmpty(args.String("by"), args.String("actor")))
	if actor == "" {
		return "", tuskerError(errorMissingArg, operation+" requires --by human:<name> or operator:<name>")
	}
	name := ""
	if strings.HasPrefix(actor, "human:") {
		name = strings.TrimPrefix(actor, "human:")
	} else if strings.HasPrefix(actor, "operator:") {
		name = strings.TrimPrefix(actor, "operator:")
	}
	if name == "" || strings.TrimSpace(name) == "" {
		return "", tuskerError(errorInvalidArg, operation+" requires --by human:<name> or operator:<name>")
	}
	return actor, nil
}

func waveStartCmd(args Args) error {
	vault, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	mode := strings.ToLower(strings.TrimSpace(args.String("mode")))
	if mode != "background" {
		return tuskerError(errorInvalidArg, "wave start requires --mode background")
	}
	actor, err := directStartActor(args, "wave start")
	if err != nil {
		return err
	}
	waveID := strings.ToUpper(strings.TrimSpace(firstNonEmpty(args.String("id"), args.String("_pos0"))))
	if waveID == "" {
		return tuskerError(errorMissingArg, "Usage: tusker wave start <WAVE-ID> --mode background --by human:<name>|operator:<name> [--json]")
	}
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		return err
	}
	defer store.Close()
	result, err := directWaveStart(vault, store, waveID, actor)
	if err != nil {
		return err
	}
	if args.Bool("json") {
		emitJSON(result)
	} else if !args.Bool("quiet") {
		fmt.Printf("%s: %s (%s) — %s\n", result.Subject, result.State, result.Authorization, result.Reason)
	}
	return nil
}

func directWaveStartRefusal(review directWaveReview) error {
	for _, blocker := range review.Blockers {
		if blocker.TaskID == "" {
			return tuskerError(errorInvalidTransition, "wave start refused: "+blocker.Code+" "+blocker.Reason)
		}
	}
	for _, blocker := range review.Blockers {
		switch blocker.Code {
		case "WAVE_TERMINAL", "ROUTE_INVALID", "DEPENDENCY_CONTRACT_INVALID", "CONTRACT_FINGERPRINT_STALE", "ACTIVE_OWNER":
			return tuskerError(errorInvalidTransition, "wave start refused: "+blocker.Code+" "+blocker.TaskID+" "+blocker.Reason)
		}
	}
	return nil
}

func directWaveStart(vault string, store *RuntimeStore, waveID, actor string) (directStartResult, error) {
	result := directStartResult{Schema: directStartSchema, Subject: waveID, Scope: "wave", State: "Waiting", Authorization: "inert"}
	project, registered, err := resolveAutomationRegisteredProject(store, Args{"vault": vault})
	if err != nil {
		return result, err
	}
	if !registered || project == nil {
		return result, tuskerError(errorInvalidTransition, "wave start requires a registered project; run tusker projects add first")
	}
	review, err := buildDirectWaveReview(vault, store, project.Project.ProjectID, waveID, nil)
	if err != nil {
		return result, err
	}
	if refusal := directWaveStartRefusal(review); refusal != nil {
		return result, refusal
	}
	if directWaveStartInjectAfterReview != nil {
		directWaveStartInjectAfterReview()
	}
	materialLock, err := acquireV7MaterialEpochLock(vault)
	if err != nil {
		return result, err
	}
	defer materialLock.Close()
	lockedReview, err := buildDirectWaveReview(vault, store, project.Project.ProjectID, waveID, nil)
	if err != nil {
		return result, err
	}
	if lockedReview.MaterialFingerprint != review.MaterialFingerprint {
		return result, tuskerError(errorInvalidTransition, "wave material changed between review and authorization; rerun wave review")
	}
	if refusal := directWaveStartRefusal(lockedReview); refusal != nil {
		return result, refusal
	}
	idx, err := loadV7Index(vault)
	if err != nil {
		return result, err
	}
	wave, ok := idx.Waves[waveID]
	if !ok {
		return result, tuskerError(errorNotFound, "wave "+waveID+" does not resolve")
	}
	fingerprint := lockedReview.MaterialFingerprint
	var eligible []string
	for _, member := range lockedReview.Members {
		if member.State == "ready" {
			eligible = append(eligible, member.TaskID)
		}
	}
	if limit := intField(wave.Data, "concurrency"); limit > 0 && len(eligible) > limit {
		eligible = eligible[:limit]
	}
	if routeBlockers := directRouteBlockers(vault, idx, eligible); len(routeBlockers) > 0 {
		return result, tuskerError(errorInvalidTransition, "wave route admission blocked: "+strings.Join(routeBlockers, "; "))
	}
	now := time.Now().UTC()
	// A paused wave whose stored authorization still covers current material
	// belongs to Resume. Once material has drifted, Start is the supported
	// recovery: it replaces the stale paused authorization under the same
	// material lock rather than deadlocking the operator between the two.
	if directWaveAuthorizationCurrent(wave.Data, "paused", fingerprint) {
		return result, tuskerError(errorInvalidTransition, "wave "+waveID+" is paused; use wave resume")
	}
	alreadyArmed := directWaveAuthorizationCurrent(wave.Data, "armed", fingerprint)
	waveLock, err := acquireV7DocumentLock(wave.AbsolutePath, v7DocumentLockTimeout)
	if err != nil {
		return result, err
	}
	data, body, err := parseFrontmatterMustRead(wave.AbsolutePath)
	if err != nil {
		_ = waveLock.Close()
		return result, err
	}
	switch {
	case directWaveAuthorizationCurrent(data, "armed", fingerprint):
		alreadyArmed = true
		_ = waveLock.Close()
	case directWaveAuthorizationCurrent(data, "paused", fingerprint):
		_ = waveLock.Close()
		return result, tuskerError(errorInvalidTransition, "wave "+waveID+" is paused; use wave resume")
	default:
		previous := stringField(data, "authorization")
		data["authorization"] = "armed"
		data["authorization_fingerprint"] = fingerprint
		data["authorized_by"] = actor
		data["authorized_at"] = now.Format(time.RFC3339Nano)
		data["updated_at"] = now.Format(time.RFC3339)
		data["updated_by"] = actor
		data["state_rev"] = v7StateRev(data, body)
		content, err := serializeDocument(data, body, v7FrontmatterOrder["wave"])
		if err != nil {
			_ = waveLock.Close()
			return result, err
		}
		payload := map[string]any{"authorization": "armed", "fingerprint": fingerprint}
		if previous == "armed" || previous == "paused" {
			payload["replaced_authorization"] = previous
		}
		eventPath, eventContent, err := prepareV7Event(vault, waveID, "wave", "updated", actor, payload, now)
		if err != nil {
			_ = waveLock.Close()
			return result, err
		}
		if err := ensureDir(filepath.Dir(eventPath)); err != nil {
			_ = waveLock.Close()
			return result, err
		}
		if err := commitV7DocumentWritesWithLocks(map[string]string{wave.AbsolutePath: content, eventPath: eventContent}, directWaveStartInjectCommitFailAfter, []*v7DocumentLock{waveLock}); err != nil {
			_ = waveLock.Close()
			return result, err
		}
		_ = waveLock.Close()
	}
	result.Authorization = "authorized"
	result.MaterialFingerprint = fingerprint
	result.Reason = "Authorized — waiting for prerequisites"
	if directWaveStartInjectCrashBeforeQueue != nil && directWaveStartInjectCrashBeforeQueue() {
		return result, nil
	}
	queued, err := queueAuthorizedWaveFrontierUnderMaterialLock(vault, store, project.Project.ProjectID, waveID, time.Now().UTC())
	if err != nil {
		return result, err
	}
	result.QueuedTaskIDs = queued
	result.Replayed = alreadyArmed
	if len(eligible) > 0 || len(queued) > 0 {
		_ = sendDaemonControlOneWay(DefaultStateRoot(), daemonControlRequest{Command: "reconcile_project", ProjectID: project.Project.ProjectID, Cause: "wave_start", Changes: []daemonControlChange{{ID: waveID, Kind: "wave", Eligibility: []string{"runtime", "authorization"}}}}, 250*time.Millisecond)
	}
	return result, nil
}

func taskStartCmd(args Args) error {
	mode := strings.ToLower(strings.TrimSpace(args.String("mode")))
	switch mode {
	case "interactive":
		if !args.Bool("current-workspace") {
			return tuskerError(errorInvalidArg, "task start --mode interactive requires --current-workspace")
		}
		if strings.TrimSpace(firstNonEmpty(args.String("by"), args.String("actor"))) == "" {
			return tuskerError(errorMissingArg, "task start --mode interactive requires an explicit --by <actor>")
		}
		args["id"] = firstNonEmpty(args.String("id"), args.String("_pos0"))
		args["lane"] = runLaneExecute
		args["explicit-start"] = "true"
		return workSessionStartCmd(args)
	case "background":
		vault, err := resolveVaultPath(args, false)
		if err != nil {
			return err
		}
		actor, err := directStartActor(args, "task start")
		if err != nil {
			return err
		}
		taskID := strings.ToUpper(strings.TrimSpace(firstNonEmpty(args.String("id"), args.String("_pos0"))))
		if taskID == "" {
			return tuskerError(errorMissingArg, "Usage: tusker task start <TASK-ID> --mode interactive|background --by <actor> [--current-workspace] [--json]")
		}
		store, err := OpenRuntimeStore(DefaultStateRoot())
		if err != nil {
			return err
		}
		defer store.Close()
		result, err := directTaskBackgroundStart(vault, store, taskID, actor)
		if err != nil {
			return err
		}
		if args.Bool("json") {
			emitJSON(result)
		} else if !args.Bool("quiet") {
			fmt.Printf("%s: %s (%s) — %s\n", result.Subject, result.State, result.Authorization, result.Reason)
		}
		return nil
	default:
		return tuskerError(errorInvalidArg, "task start requires --mode interactive|background")
	}
}

func directTaskBackgroundStart(vault string, store *RuntimeStore, taskID, actor string) (directStartResult, error) {
	result := directStartResult{Schema: directStartSchema, Subject: taskID, Scope: "task", State: "Waiting", Authorization: "inert"}
	project, registered, err := resolveAutomationRegisteredProject(store, Args{"vault": vault})
	if err != nil {
		return result, err
	}
	if !registered || project == nil {
		return result, tuskerError(errorInvalidTransition, "task start --mode background requires a registered project; run tusker projects add first")
	}
	if directTaskStartInjectBeforeLock != nil {
		directTaskStartInjectBeforeLock()
	}
	materialLock, err := acquireV7MaterialEpochLock(vault)
	if err != nil {
		return result, err
	}
	defer materialLock.Close()
	idx, err := loadV7Index(vault)
	if err != nil {
		return result, err
	}
	task, ok := idx.Tasks[taskID]
	if !ok {
		return result, tuskerError(errorNotFound, "task "+taskID+" does not resolve")
	}
	if waveID := stringField(task.Data, "wave"); waveID != "" {
		if wave, ok := idx.Waves[waveID]; ok {
			waveStatus := strings.ToLower(stringField(wave.Data, "status"))
			if waveStatus == "cancelled" || waveStatus == "superseded" {
				return result, tuskerError(errorInvalidTransition, "WAVE_TERMINAL "+waveID+": wave is "+waveStatus)
			}
		}
	}
	if reason := directWaveTaskContractStaleReason(task); reason != "" {
		return result, tuskerError(errorInvalidTransition, "CONTRACT_FINGERPRINT_STALE "+reason)
	}
	status := strings.ToLower(stringField(task.Data, "status"))
	if status == "done" || status == "cancelled" || status == "superseded" {
		return result, tuskerError(errorInvalidTransition, "task "+taskID+" is terminal (status "+status+")")
	}
	if status != "backlog" && status != "ready" && status != "rework" {
		return result, tuskerError(errorInvalidTransition, "task "+taskID+" is "+strings.ReplaceAll(status, "_", " ")+"; task start accepts backlog, ready, or rework")
	}
	if edge, blocked := v7BlockingDependencyForReadiness(task, idx); blocked {
		return result, tuskerError(errorInvalidTransition, "DEPENDENCY_WAITING "+taskID+": dependency "+edge.ID+" is unfinished")
	}
	if blocker := workSessionOpenHumanGateBlocker(task, idx); blocker != nil {
		return result, tuskerError(errorInvalidTransition, "HUMAN_GATE_OPEN "+taskID+": "+blocker.Reason)
	}
	if current, findErr := store.FindRunScoped(project.Project.ProjectID, trackerRecordID(task)); findErr != nil {
		return result, findErr
	} else if current != nil && isDispatchingLeaseState(current.LeaseState) {
		return result, tuskerError(errorInvalidTransition, "ACTIVE_OWNER "+taskID+": held by "+current.LeaseOwner)
	}
	if reason := directWaveDependencyContractBlocker(task, idx); reason != "" {
		return result, tuskerError(errorInvalidTransition, "DEPENDENCY_CONTRACT_INVALID "+taskID+": "+reason)
	}
	if routeBlockers := directRouteBlockers(vault, idx, []string{taskID}); len(routeBlockers) > 0 {
		return result, tuskerError(errorInvalidTransition, "ROUTE_INVALID "+strings.Join(routeBlockers, "; "))
	}
	contractFP := directWaveTaskContract(task)
	if existing, err := store.RunDirective(project.Project.ProjectID, trackerRecordID(task)); err != nil {
		return result, err
	} else if existing != nil && existing.State == "queued" && runDirectiveActive(existing, time.Now().UTC()) {
		if existing.WaveID == "" && existing.AuthorizationFingerprint == contractFP {
			result.Authorization = "authorized"
			result.MaterialFingerprint = contractFP
			result.Reason = "Authorized — waiting for runtime"
			result.Replayed = true
			return result, nil
		}
		if existing.WaveID != "" && existing.WaveID == stringField(task.Data, "wave") {
			if pausedWave, ok := idx.Waves[existing.WaveID]; ok &&
				stringField(pausedWave.Data, "authorization") == "paused" &&
				stringField(pausedWave.Data, "authorization_fingerprint") == existing.AuthorizationFingerprint &&
				stringField(pausedWave.Data, "authorized_at") == existing.WaveAuthorizedAt {
				if waveFP, _ := waveMaterialFingerprint(vault, idx, pausedWave); waveFP == existing.AuthorizationFingerprint {
					replaced, repErr := store.ReplacePausedWaveDirectiveWithTaskDirective(project.Project.ProjectID, trackerRecordID(task), existing.WaveID, existing.AuthorizationFingerprint, existing.WaveAuthorizedAt, actor, contractFP, time.Now().UTC(), directRunDirectiveTTL)
					if repErr != nil {
						return result, repErr
					}
					if !replaced {
						return result, tuskerError(errorInvalidTransition, "task "+taskID+" wave directive changed during task start; retry the operation")
					}
					result.Authorization = "authorized"
					result.MaterialFingerprint = contractFP
					result.QueuedTaskIDs = []string{taskID}
					result.Reason = "Authorized — task-scoped start inside paused wave " + existing.WaveID + "; waiting for runtime"
					_ = sendDaemonControlOneWay(DefaultStateRoot(), daemonControlRequest{Command: "reconcile_project", ProjectID: project.Project.ProjectID, Cause: "task_start", Changes: []daemonControlChange{{ID: trackerRecordID(task), Kind: "run", Eligibility: []string{"runtime", "authorization"}}}}, 250*time.Millisecond)
					return result, nil
				}
			}
		}
		return result, tuskerError(errorInvalidTransition, "task "+taskID+" has a stale queued directive for different material; renew it after the current directive lapses or is consumed")
	}
	now := time.Now().UTC()
	queued, err := store.QueueRunDirective(RunDirective{ProjectID: project.Project.ProjectID, RecordID: trackerRecordID(task), Actor: actor, CreatedAt: now.Format(time.RFC3339Nano), ExpiresAt: now.Add(directRunDirectiveTTL).Format(time.RFC3339Nano), WaveID: "", AuthorizationFingerprint: contractFP})
	if err != nil {
		return result, err
	}
	if !queued {
		return result, tuskerError(errorInvalidTransition, "task "+taskID+" could not be queued; it already has a live run or directive")
	}
	result.Authorization = "authorized"
	result.MaterialFingerprint = contractFP
	result.QueuedTaskIDs = []string{taskID}
	result.Reason = "Authorized — waiting for runtime"
	_ = sendDaemonControlOneWay(DefaultStateRoot(), daemonControlRequest{Command: "reconcile_project", ProjectID: project.Project.ProjectID, Cause: "task_start", Changes: []daemonControlChange{{ID: trackerRecordID(task), Kind: "run", Eligibility: []string{"runtime", "authorization"}}}}, 250*time.Millisecond)
	return result, nil
}

func queueAuthorizedWaveFrontier(vault string, store *RuntimeStore, projectID, waveID string, now time.Time) ([]string, error) {
	materialLock, err := acquireV7MaterialEpochLock(vault)
	if err != nil {
		return nil, err
	}
	defer materialLock.Close()
	return queueAuthorizedWaveFrontierUnderMaterialLock(vault, store, projectID, waveID, now)
}

func queueAuthorizedWaveFrontierUnderMaterialLock(vault string, store *RuntimeStore, projectID, waveID string, now time.Time) ([]string, error) {
	idx, err := loadV7Index(vault)
	if err != nil {
		return nil, err
	}
	wave, ok := idx.Waves[waveID]
	if !ok {
		return nil, nil
	}
	waveLock, err := acquireV7DocumentLock(wave.AbsolutePath, v7DocumentLockTimeout)
	if err != nil {
		return nil, err
	}
	data, body, err := parseFrontmatterMustRead(wave.AbsolutePath)
	if err != nil {
		_ = waveLock.Close()
		return nil, err
	}
	wave.Data = data
	wave.Body = body
	idx.Waves[waveID] = wave
	status := strings.ToLower(stringField(data, "status"))
	if status == "cancelled" || status == "superseded" {
		_ = waveLock.Close()
		return nil, nil
	}
	fingerprint, issues := waveMaterialFingerprint(vault, idx, wave)
	stored := stringField(data, "authorization_fingerprint")
	authorizedAt := stringField(data, "authorized_at")
	authorizedBy := stringField(data, "authorized_by")
	armed := stringField(data, "authorization") == "armed" && len(issues) == 0 && stored != "" && stored == fingerprint && authorizedAt != "" && authorizedBy != ""
	if !armed {
		_ = waveLock.Close()
		return nil, nil
	}
	review, err := buildDirectWaveReview(vault, store, projectID, waveID, nil)
	if err != nil {
		_ = waveLock.Close()
		return nil, err
	}
	members := uniqueStrings(normalizeList(data["members"]))
	limit := intField(data, "concurrency")
	if limit <= 0 {
		limit = len(members)
	}
	if limit < 1 {
		limit = 1
	}
	occupied := map[string]bool{}
	recordToTask := map[string]string{}
	for _, member := range members {
		if task, ok := idx.Tasks[member]; ok {
			recordToTask[trackerRecordID(task)] = member
		}
	}
	runs, err := store.ListRuns()
	if err != nil {
		_ = waveLock.Close()
		return nil, err
	}
	for _, run := range runs {
		if run.ProjectID != projectID {
			continue
		}
		if isDispatchCapacityLeaseState(run.LeaseState) || isDispatchingLeaseState(run.LeaseState) {
			if taskID, ok := recordToTask[firstNonEmpty(run.RecordID, run.ItemID)]; ok {
				occupied[taskID] = true
			}
		}
	}
	directives, err := store.ListActiveRunDirectives(projectID, now)
	if err != nil {
		_ = waveLock.Close()
		return nil, err
	}
	for _, directive := range directives {
		if taskID, ok := recordToTask[directive.RecordID]; ok {
			// A queued directive bound to this wave's superseded authorization
			// can never match again; QueueWaveRunDirectives re-binds it to the
			// current fingerprint. Only a directive bound to the current (or a
			// foreign) authorization occupies the frontier slot.
			if directive.WaveID == waveID && (directive.AuthorizationFingerprint != fingerprint || directive.WaveAuthorizedAt != authorizedAt) {
				continue
			}
			occupied[taskID] = true
		}
	}
	ready := map[string]bool{}
	for _, member := range review.Members {
		if member.State == "ready" {
			ready[member.TaskID] = true
		}
	}
	remaining := limit
	for _, member := range members {
		if occupied[member] {
			remaining--
		}
	}
	var candidates []string
	for _, frontier := range review.Frontiers {
		for _, id := range frontier {
			if ready[id] && !occupied[id] {
				candidates = append(candidates, id)
			}
		}
	}
	if remaining <= 0 || len(candidates) == 0 {
		_ = waveLock.Close()
		return nil, nil
	}
	if len(candidates) > remaining {
		candidates = candidates[:remaining]
	}
	if routeBlockers := directRouteBlockers(vault, idx, candidates); len(routeBlockers) > 0 {
		_ = waveLock.Close()
		return nil, tuskerError(errorInvalidTransition, "wave route admission blocked: "+strings.Join(routeBlockers, "; "))
	}
	candidateRecords := make([]string, 0, len(candidates))
	for _, taskID := range candidates {
		candidateRecords = append(candidateRecords, trackerRecordID(idx.Tasks[taskID]))
	}
	queuedRecords, _, err := store.QueueWaveRunDirectives(projectID, waveID, fingerprint, authorizedAt, authorizedBy, candidateRecords, now, directRunDirectiveTTL, false)
	_ = waveLock.Close()
	if err != nil {
		return nil, err
	}
	queued := make([]string, 0, len(queuedRecords))
	for _, recordID := range queuedRecords {
		queued = append(queued, firstNonEmpty(recordToTask[recordID], recordID))
	}
	return queued, nil
}

func directWavePause(vault string, store *RuntimeStore, waveID, actor string) (directStartResult, error) {
	result := directStartResult{Schema: directStartSchema, Subject: waveID, Scope: "wave", State: "Paused", Authorization: "paused"}
	materialLock, err := acquireV7MaterialEpochLock(vault)
	if err != nil {
		return result, err
	}
	defer materialLock.Close()
	idx, err := loadV7Index(vault)
	if err != nil {
		return result, err
	}
	wave, ok := idx.Waves[waveID]
	if !ok {
		return result, tuskerError(errorNotFound, "wave "+waveID+" does not resolve")
	}
	waveLock, err := acquireV7DocumentLock(wave.AbsolutePath, v7DocumentLockTimeout)
	if err != nil {
		return result, err
	}
	defer func() { _ = waveLock.Close() }()
	data, body, err := parseFrontmatterMustRead(wave.AbsolutePath)
	if err != nil {
		return result, err
	}
	wave.Data = data
	wave.Body = body
	idx.Waves[waveID] = wave
	fingerprint, _ := waveMaterialFingerprint(vault, idx, wave)
	stored := stringField(data, "authorization_fingerprint")
	authorizedAt := stringField(data, "authorized_at")
	switch stringField(data, "authorization") {
	case "paused":
		result.MaterialFingerprint = fingerprint
		result.Replayed = true
		result.Reason = "Paused — admitted attempts may finish; no new wave-owned workers or reviewers will start."
		return result, nil
	case "armed":
		if stored == "" || authorizedAt == "" || stored != fingerprint {
			return result, tuskerError(errorInvalidTransition, "wave "+waveID+" authorization is stale; rerun wave review and re-authorize current material")
		}
	default:
		return result, tuskerError(errorInvalidTransition, "wave "+waveID+" is not armed; only an armed wave can be paused")
	}
	data["authorization"] = "paused"
	data["updated_at"] = time.Now().UTC().Format(time.RFC3339)
	data["updated_by"] = actor
	data["state_rev"] = v7StateRev(data, body)
	content, err := serializeDocument(data, body, v7FrontmatterOrder["wave"])
	if err != nil {
		return result, err
	}
	eventPath, eventContent, err := prepareV7Event(vault, waveID, "wave", "updated", actor, map[string]any{"authorization": "paused", "fingerprint": fingerprint}, time.Now().UTC())
	if err != nil {
		return result, err
	}
	if err := ensureDir(filepath.Dir(eventPath)); err != nil {
		return result, err
	}
	if err := commitV7DocumentWritesWithLocks(map[string]string{wave.AbsolutePath: content, eventPath: eventContent}, 0, []*v7DocumentLock{waveLock}); err != nil {
		return result, err
	}
	result.MaterialFingerprint = fingerprint
	result.Reason = "Paused — admitted attempts may finish; no new wave-owned workers or reviewers will start."
	return result, nil
}

func directWaveResume(vault string, store *RuntimeStore, waveID, actor string) (directStartResult, error) {
	result := directStartResult{Schema: directStartSchema, Subject: waveID, Scope: "wave", State: "Waiting", Authorization: "inert"}
	materialLock, err := acquireV7MaterialEpochLock(vault)
	if err != nil {
		return result, err
	}
	defer materialLock.Close()
	idx, err := loadV7Index(vault)
	if err != nil {
		return result, err
	}
	wave, ok := idx.Waves[waveID]
	if !ok {
		return result, tuskerError(errorNotFound, "wave "+waveID+" does not resolve")
	}
	waveLock, err := acquireV7DocumentLock(wave.AbsolutePath, v7DocumentLockTimeout)
	if err != nil {
		return result, err
	}
	data, body, err := parseFrontmatterMustRead(wave.AbsolutePath)
	if err != nil {
		_ = waveLock.Close()
		return result, err
	}
	wave.Data = data
	wave.Body = body
	idx.Waves[waveID] = wave
	fingerprint, _ := waveMaterialFingerprint(vault, idx, wave)
	stored := stringField(data, "authorization_fingerprint")
	authorizedAt := stringField(data, "authorized_at")
	replayed := false
	switch stringField(data, "authorization") {
	case "paused":
		if stored == "" || authorizedAt == "" || stored != fingerprint {
			_ = waveLock.Close()
			return result, tuskerError(errorInvalidTransition, "wave "+waveID+" material changed while paused; rerun wave review and re-authorize current material")
		}
	case "armed":
		if stored == "" || authorizedAt == "" || stored != fingerprint {
			_ = waveLock.Close()
			return result, tuskerError(errorInvalidTransition, "wave "+waveID+" authorization is stale; rerun wave review and re-authorize current material")
		}
		replayed = true
	default:
		_ = waveLock.Close()
		return result, tuskerError(errorInvalidTransition, "wave "+waveID+" is not paused; only a paused wave can be resumed")
	}
	// Directives and run rows are keyed by the registered project id, not the
	// authored project field: a resume that queues under the authored key would
	// write directives the daemon's registered project can never see.
	projectID, _ := resolveV7ProjectID(vault)
	if registeredID, registered, regErr := registeredProjectIDForVault(store, vault); regErr == nil && registered && strings.TrimSpace(registeredID) != "" {
		projectID = registeredID
	}
	review, err := buildDirectWaveReview(vault, store, projectID, waveID, nil)
	if err != nil {
		_ = waveLock.Close()
		return result, err
	}
	if refusal := directWaveStartRefusal(review); refusal != nil {
		_ = waveLock.Close()
		return result, refusal
	}
	if !replayed {
		data["authorization"] = "armed"
		data["updated_at"] = time.Now().UTC().Format(time.RFC3339)
		data["updated_by"] = actor
		data["state_rev"] = v7StateRev(data, body)
		content, err := serializeDocument(data, body, v7FrontmatterOrder["wave"])
		if err != nil {
			_ = waveLock.Close()
			return result, err
		}
		eventPath, eventContent, err := prepareV7Event(vault, waveID, "wave", "updated", actor, map[string]any{"authorization": "armed", "fingerprint": fingerprint}, time.Now().UTC())
		if err != nil {
			_ = waveLock.Close()
			return result, err
		}
		if err := ensureDir(filepath.Dir(eventPath)); err != nil {
			_ = waveLock.Close()
			return result, err
		}
		if err := commitV7DocumentWritesWithLocks(map[string]string{wave.AbsolutePath: content, eventPath: eventContent}, 0, []*v7DocumentLock{waveLock}); err != nil {
			_ = waveLock.Close()
			return result, err
		}
	}
	_ = waveLock.Close()
	queued, err := queueAuthorizedWaveFrontierUnderMaterialLock(vault, store, projectID, waveID, time.Now().UTC())
	if err != nil {
		return result, err
	}
	result.Authorization = "authorized"
	result.MaterialFingerprint = fingerprint
	result.QueuedTaskIDs = queued
	result.Replayed = replayed && len(queued) == 0
	for _, member := range review.Members {
		if member.State == "running" {
			result.State = "Running"
			break
		}
	}
	result.Reason = "Resumed — the stored authorization is restored and queued work re-admits for dispatch; failed attempts are not retried automatically"
	_ = sendDaemonControlOneWay(DefaultStateRoot(), daemonControlRequest{Command: "reconcile_project", ProjectID: projectID, Cause: "wave_resume", Changes: []daemonControlChange{{ID: waveID, Kind: "wave", Eligibility: []string{"runtime", "authorization"}}}}, 250*time.Millisecond)
	return result, nil
}

func directWaveControlCmd(args Args, operation string, fn func(string, *RuntimeStore, string, string) (directStartResult, error)) error {
	vault, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	actor, err := directStartActor(args, operation)
	if err != nil {
		return err
	}
	waveID := strings.ToUpper(strings.TrimSpace(firstNonEmpty(args.String("id"), args.String("_pos0"))))
	if waveID == "" {
		return tuskerError(errorMissingArg, "Usage: tusker "+operation+" <WAVE-ID> --by human:<name>|operator:<name> [--json]")
	}
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		return err
	}
	defer store.Close()
	result, err := fn(vault, store, waveID, actor)
	if err != nil {
		return err
	}
	if args.Bool("json") {
		emitJSON(result)
	} else if !args.Bool("quiet") {
		fmt.Printf("%s: %s (%s) — %s\n", result.Subject, result.State, result.Authorization, result.Reason)
	}
	return nil
}

func (s *serveServer) handleWaveReviewAPI(w http.ResponseWriter, projectID, waveID string) {
	project, err := s.projectForSnapshot(projectID)
	if err != nil {
		serveJSON(w, http.StatusOK, serveActionResult{Refused: true, Reason: errorToIssue(err).Message})
		return
	}
	review, err := buildDirectWaveReview(project.VaultRoot, s.store, project.ProjectID, strings.ToUpper(strings.TrimSpace(waveID)), nil)
	if err != nil {
		serveJSON(w, http.StatusOK, serveActionResult{Refused: true, Reason: errorToIssue(err).Message})
		return
	}
	serveJSON(w, http.StatusOK, review)
}

func (s *serveServer) handleWaveStartAction(w http.ResponseWriter, projectID, waveID string, body serveActionBody) {
	project, err := s.projectForSnapshot(projectID)
	if err != nil {
		serveJSON(w, http.StatusOK, serveActionResult{Refused: true, Reason: errorToIssue(err).Message})
		return
	}
	actor, err := s.serveOperatorActor(body, "serve wave start")
	if err != nil {
		status, result := serveOperatorActorResult("serve wave start", err)
		serveJSON(w, status, result)
		return
	}
	mode := strings.ToLower(strings.TrimSpace(body.string("mode")))
	if mode != "" && mode != "background" {
		serveJSON(w, http.StatusOK, serveActionResult{Refused: true, Reason: "wave start supports mode background only"})
		return
	}
	result, err := directWaveStart(project.VaultRoot, s.store, strings.ToUpper(strings.TrimSpace(waveID)), actor)
	if err != nil {
		serveJSON(w, http.StatusOK, serveActionResult{Refused: true, Reason: errorToIssue(err).Message})
		return
	}
	s.invalidateProjectSnapshot(project.ProjectID)
	serveJSON(w, http.StatusOK, result)
}

func (s *serveServer) handleWavePauseAction(w http.ResponseWriter, projectID, waveID string, body serveActionBody) {
	project, err := s.projectForSnapshot(projectID)
	if err != nil {
		serveJSON(w, http.StatusOK, serveActionResult{Refused: true, Reason: errorToIssue(err).Message})
		return
	}
	actor, err := s.serveOperatorActor(body, "serve wave pause")
	if err != nil {
		status, result := serveOperatorActorResult("serve wave pause", err)
		serveJSON(w, status, result)
		return
	}
	result, err := directWavePause(project.VaultRoot, s.store, strings.ToUpper(strings.TrimSpace(waveID)), actor)
	if err != nil {
		serveJSON(w, http.StatusOK, serveActionResult{Refused: true, Reason: errorToIssue(err).Message})
		return
	}
	s.invalidateProjectSnapshot(project.ProjectID)
	serveJSON(w, http.StatusOK, result)
}

func (s *serveServer) handleWaveResumeAction(w http.ResponseWriter, projectID, waveID string, body serveActionBody) {
	project, err := s.projectForSnapshot(projectID)
	if err != nil {
		serveJSON(w, http.StatusOK, serveActionResult{Refused: true, Reason: errorToIssue(err).Message})
		return
	}
	actor, err := s.serveOperatorActor(body, "serve wave resume")
	if err != nil {
		status, result := serveOperatorActorResult("serve wave resume", err)
		serveJSON(w, status, result)
		return
	}
	result, err := directWaveResume(project.VaultRoot, s.store, strings.ToUpper(strings.TrimSpace(waveID)), actor)
	if err != nil {
		serveJSON(w, http.StatusOK, serveActionResult{Refused: true, Reason: errorToIssue(err).Message})
		return
	}
	s.invalidateProjectSnapshot(project.ProjectID)
	serveJSON(w, http.StatusOK, result)
}

func (s *serveServer) handleTaskStartAction(w http.ResponseWriter, projectID, taskID string, body serveActionBody) {
	project, err := s.projectForSnapshot(projectID)
	if err != nil {
		serveJSON(w, http.StatusOK, serveActionResult{Refused: true, Reason: errorToIssue(err).Message})
		return
	}
	actor, err := s.serveOperatorActor(body, "serve task start")
	if err != nil {
		status, result := serveOperatorActorResult("serve task start", err)
		serveJSON(w, status, result)
		return
	}
	mode := strings.ToLower(strings.TrimSpace(body.string("mode")))
	if mode != "" && mode != "background" {
		serveJSON(w, http.StatusOK, serveActionResult{Refused: true, Reason: "task start over Serve supports mode background only"})
		return
	}
	result, err := directTaskBackgroundStart(project.VaultRoot, s.store, strings.ToUpper(strings.TrimSpace(taskID)), actor)
	if err != nil {
		serveJSON(w, http.StatusOK, serveActionResult{Refused: true, Reason: errorToIssue(err).Message})
		return
	}
	s.invalidateProjectSnapshot(project.ProjectID)
	serveJSON(w, http.StatusOK, result)
}
