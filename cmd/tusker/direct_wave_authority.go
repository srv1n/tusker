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
	Reason string `json:"reason" yaml:"reason"`
	Action string `json:"action" yaml:"action"`
}

type directStartControl struct {
	Action  string `json:"action" yaml:"action"`
	Enabled bool   `json:"enabled" yaml:"enabled"`
	Scope   string `json:"scope" yaml:"scope"`
	Reason  string `json:"reason,omitempty" yaml:"reason,omitempty"`
}

type directWaveReviewMember struct {
	TaskID        string   `json:"taskId" yaml:"taskId"`
	Title         string   `json:"title" yaml:"title"`
	State         string   `json:"state" yaml:"state"`
	WaitingReason string   `json:"waitingReason,omitempty" yaml:"waitingReason,omitempty"`
	Dependencies  []string `json:"dependencies,omitempty" yaml:"dependencies,omitempty"`
	ExecuteRoute  string   `json:"executeRoute,omitempty" yaml:"executeRoute,omitempty"`
	ReviewRoute   string   `json:"reviewRoute,omitempty" yaml:"reviewRoute,omitempty"`
	Acceptance    []string `json:"acceptance,omitempty" yaml:"acceptance,omitempty"`
	Verification  []string `json:"verification,omitempty" yaml:"verification,omitempty"`
	Instructions  string   `json:"instructions,omitempty" yaml:"instructions,omitempty"`
}

type directWaveReview struct {
	Schema              string                   `json:"schema" yaml:"schema"`
	WaveID              string                   `json:"waveId" yaml:"waveId"`
	Title               string                   `json:"title" yaml:"title"`
	Outcome             string                   `json:"outcome" yaml:"outcome"`
	State               string                   `json:"state" yaml:"state"`
	Authorization       string                   `json:"authorization" yaml:"authorization"`
	MaterialFingerprint string                   `json:"materialFingerprint" yaml:"materialFingerprint"`
	Members             []directWaveReviewMember `json:"members" yaml:"members"`
	Frontiers           [][]string               `json:"frontiers" yaml:"frontiers"`
	Blockers            []directStartBlocker     `json:"blockers,omitempty" yaml:"blockers,omitempty"`
	Controls            []directStartControl     `json:"controls" yaml:"controls"`
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

func directWaveLiveOwner(runs map[string]RunStatus, recordID string) string {
	run, ok := runs[recordID]
	if !ok {
		return ""
	}
	if isDispatchingLeaseState(run.LeaseState) || LeaseState(strings.TrimSpace(run.LeaseState)) == LeaseStateRunning {
		return firstNonEmpty(run.LeaseOwner, "active run")
	}
	return ""
}

func directWaveReviewRuntimeStore() (*RuntimeStore, error) {
	stateRoot := DefaultStateRoot()
	if !fileExists(runtimeStoreDBPath(stateRoot)) {
		return nil, nil
	}
	return OpenRuntimeStoreReadOnly(stateRoot)
}

func directWaveRunAdmittedByWave(store *RuntimeStore, projectID, recordID, waveID, fingerprint, authorizedAt string) bool {
	if store == nil || fingerprint == "" {
		return false
	}
	if directive, err := store.RunDirective(projectID, recordID); err == nil && directive != nil && directive.WaveID == waveID && directive.AuthorizationFingerprint == fingerprint && directive.WaveAuthorizedAt == authorizedAt {
		return true
	}
	if auth, err := store.LatestRunAuthorization(projectID, recordID); err == nil && auth != nil && auth.DirectiveWaveID == waveID && auth.DirectiveAuthorizationFingerprint == fingerprint && auth.DirectiveWaveAuthorizedAt == authorizedAt {
		return true
	}
	return false
}

func buildDirectWaveReview(vaultPath string, store *RuntimeStore, projectID, waveID string, runtimeErr error) (directWaveReview, error) {
	review := directWaveReview{Schema: directWaveReviewSchema, WaveID: waveID, Controls: []directStartControl{}}
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
	members := uniqueStrings(normalizeList(wave.Data["members"]))
	sort.Strings(members)
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
	runs := map[string]RunStatus{}
	if runtimeErr != nil {
		review.Blockers = append(review.Blockers, directStartBlocker{Code: "RUNTIME_UNAVAILABLE", Reason: "runtime state is unavailable: " + runtimeErr.Error(), Action: "restore the runtime store and rerun wave review"})
	} else if store != nil {
		if list, listErr := store.ListRuns(); listErr != nil {
			review.Blockers = append(review.Blockers, directStartBlocker{Code: "RUNTIME_UNAVAILABLE", Reason: "runtime state is unavailable: " + listErr.Error(), Action: "restore the runtime store and rerun wave review"})
		} else {
			for _, run := range list {
				if projectID != "" && run.ProjectID != projectID {
					continue
				}
				key := firstNonEmpty(run.ItemID, run.RecordID)
				if key != "" {
					runs[key] = run
				}
			}
		}
	}
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
		if reason := directWaveStrictBlocker(vaultPath, idx, task, wave); reason != "" {
			review.Blockers = append(review.Blockers, directStartBlocker{Code: "STRICT_PROOF_STALE", TaskID: id, Reason: reason, Action: "re-run current command proof for " + id})
		}
		recordID := trackerRecordID(task)
		owner := directWaveLiveOwner(runs, firstNonEmpty(recordID, id))
		admitted := owner != "" && !stale && (authState == "armed" || authState == "paused") && directWaveRunAdmittedByWave(store, projectID, recordID, waveID, armedFingerprint, armedAt)
		status := strings.ToLower(stringField(task.Data, "status"))
		depWait := directWaveMemberDependencyWait(task, idx)
		switch {
		// Contract staleness dominates lifecycle status: a done member whose
		// stored pin no longer covers its bytes cannot certify the wave as
		// complete, and a stale member is never startable. Live admitted work
		// still reports running — the recorded blocker flags the drift.
		case taskStale == "" && (landed[id] && status == "done" || status == "done"):
			member.State = "completed"
		case status == "review":
			member.State = "waiting"
			member.WaitingReason = firstNonEmpty(member.WaitingReason, "awaiting independent review")
		case owner != "" && admitted:
			member.State = "running"
			member.WaitingReason = "active owner " + owner
		case taskStale != "":
			member.State = "waiting"
			member.WaitingReason = "task contract drifted from its stored fingerprint; rebind required"
		case member.State == "waiting":
		case depWait != "":
			member.State = "waiting"
			member.WaitingReason = "waiting for dependency " + depWait
			review.Blockers = append(review.Blockers, directStartBlocker{Code: "DEPENDENCY_WAITING", TaskID: id, Reason: "dependency " + depWait + " is unfinished", Action: "complete " + depWait + " first"})
		case armedWaveTaskHumanBlocked(idx, task):
			member.State = "waiting"
			member.WaitingReason = "open blocking human gate"
			review.Blockers = append(review.Blockers, directStartBlocker{Code: "HUMAN_GATE_OPEN", TaskID: id, Reason: "an open human gate blocks " + id, Action: "complete the human gate for " + id})
		default:
			member.State = "ready"
		}
		if owner != "" && !admitted {
			member.State = "waiting"
			member.WaitingReason = firstNonEmpty(member.WaitingReason, "task is held by "+owner)
			review.Blockers = append(review.Blockers, directStartBlocker{Code: "ACTIVE_OWNER", TaskID: id, Reason: "task is held by " + owner + " outside this wave's authorization", Action: "wait for the owner to release or reclaim the lease"})
		}
		review.Members = append(review.Members, member)
	}
	anyRunning, allDone := false, len(members) > 0
	for _, member := range review.Members {
		if member.State == "running" {
			anyRunning = true
		}
		if member.State != "completed" {
			allDone = false
		}
	}
	switch {
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
		control := directStartControl{Action: "task start", Enabled: member.State == "ready" || member.State == "planned", Scope: member.TaskID}
		if !control.Enabled {
			control.Reason = firstNonEmpty(member.WaitingReason, "task is not eligible")
		} else if review.State == "Paused" {
			control.Reason = "wave " + waveID + " remains paused; this start is task-scoped only"
		}
		review.Controls = append(review.Controls, control)
	}
	return review, nil
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
		case "ROUTE_INVALID", "STRICT_PROOF_STALE", "DEPENDENCY_CONTRACT_INVALID", "CONTRACT_FINGERPRINT_STALE", "ACTIVE_OWNER":
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
	result.Reason = "Authorized — waiting for runtime"
	if directWaveStartInjectCrashBeforeQueue != nil && directWaveStartInjectCrashBeforeQueue() {
		return result, nil
	}
	queued, err := queueAuthorizedWaveFrontierUnderMaterialLock(vault, store, project.Project.ProjectID, waveID, time.Now().UTC())
	if err != nil {
		return result, err
	}
	result.QueuedTaskIDs = queued
	result.Replayed = alreadyArmed && len(queued) == 0
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
	var wave Note
	if waveID := stringField(task.Data, "wave"); waveID != "" {
		wave = idx.Waves[waveID]
	}
	if reason := directWaveStrictBlocker(vault, idx, task, wave); reason != "" {
		return result, tuskerError(errorInvalidTransition, "STRICT_PROOF_STALE "+taskID+": "+reason)
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
			occupied[firstNonEmpty(run.ItemID, run.RecordID)] = true
		}
	}
	recordToTask := map[string]string{}
	for _, member := range members {
		if task, ok := idx.Tasks[member]; ok {
			recordToTask[trackerRecordID(task)] = member
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
	queued, _, err := store.QueueWaveRunDirectives(projectID, waveID, fingerprint, authorizedAt, authorizedBy, candidates, now, directRunDirectiveTTL, false)
	_ = waveLock.Close()
	if err != nil {
		return nil, err
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
	projectID, _ := resolveV7ProjectID(vault)
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
	result.Reason = "Authorized — resumed; waiting for runtime"
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
