package main

import (
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Armed wave state is deliberately reconstructed from canonical wave/task
// records plus the runtime store.  There is no in-memory cursor to lose when
// the daemon restarts: every poll computes the same desired frontier.
const (
	armedWaveRunnable           = "runnable"
	armedWaveRunning            = "running"
	armedWaveReview             = "review"
	armedWaveLanded             = "landed"
	armedWaveMachineParked      = "machine-parked"
	armedWaveHumanBlocked       = "human-blocked"
	armedWaveStaleAuthorization = "stale-authorization"
	armedWaveDisarmed           = "disarmed"
	armedWaveDependencyWaiting  = "dependency-waiting"
)

type armedWaveMember struct {
	ID     string `json:"id"`
	State  string `json:"state"`
	Reason string `json:"reason,omitempty"`
}

type armedWaveSnapshot struct {
	WaveID        string            `json:"waveId"`
	Authorization string            `json:"authorization"`
	Concurrency   int               `json:"concurrency"`
	Members       []armedWaveMember `json:"members"`
	Frontier      []string          `json:"frontier"`
}

func buildArmedWaveSnapshot(vaultPath string, idx v7Index, wave Note, runs map[string]RunStatus, now time.Time) armedWaveSnapshot {
	members := normalizeList(wave.Data["members"])
	cap := intField(wave.Data, "concurrency")
	if cap <= 0 {
		cap = maxInt(1, len(members))
	}
	auth := waveAuthorizationProjection(vaultPath, idx, wave)
	authState := stringField(auth, "state")
	stale := boolFromAny(auth["stale"])
	landed := armedWaveLandedMembers(wave)
	idx, projectionErrors := armedWaveProjectedIndex(vaultPath, idx, wave)
	state := map[string]armedWaveMember{}

	for _, id := range members {
		task, ok := idx.Tasks[id]
		member := armedWaveMember{ID: id, State: armedWaveRunnable, Reason: "eligible in current armed-wave frontier"}
		switch {
		case !ok:
			member.State, member.Reason = armedWaveMachineParked, "task record is missing"
		case projectionErrors[id] != "":
			member.State, member.Reason = armedWaveMachineParked, "integration projection failed: "+projectionErrors[id]
		case landed[id] && stringField(task.Data, "status") == "done":
			member.State, member.Reason = armedWaveLanded, "landed on wave integration branch"
		case landed[id] && stringField(task.Data, "status") == "review":
			member.State, member.Reason = armedWaveReview, "awaiting objective review and landing"
		case stale:
			member.State, member.Reason = armedWaveStaleAuthorization, "wave authorization is "+fallback(authState, "disarmed")
		case authState == "disarmed":
			member.State, member.Reason = armedWaveDisarmed, "wave authorization is disarmed"
		case authState != "armed":
			// Paused is intentionally kept on the existing stale/blocked surface;
			// only an explicit disarmed wave gets the never-armed member state.
			member.State, member.Reason = armedWaveStaleAuthorization, "wave authorization is "+fallback(authState, "disarmed")
		case armedWaveTaskHumanBlocked(idx, task):
			member.State, member.Reason = armedWaveHumanBlocked, firstNonEmpty(stringField(task.Data, "next_action"), "human action required")
		case armedWaveRunMachineParked(runs[id]):
			member.State, member.Reason = armedWaveMachineParked, firstNonEmpty(runs[id].LastError, "attempt policy exhausted")
		case runConsumesDispatchCapacity(runs[id]):
			member.State, member.Reason = armedWaveRunning, "live implementation or review lease"
		case stringField(task.Data, "status") == "review" || stringField(task.Data, "status") == "done":
			member.State, member.Reason = armedWaveReview, "awaiting objective review and landing"
		case !isV7RunnableAgentTask(task):
			if edge, blocked := v7BlockingDependencyForReadiness(task, idx); blocked {
				member.State, member.Reason = armedWaveDependencyWaiting, "waiting for dependency "+edge.ID
			} else {
				member.State, member.Reason = armedWaveMachineParked, firstNonEmpty(stringField(task.Data, "next_action"), "task is not runnable")
			}
		}
		state[id] = member
	}

	// A parked prerequisite contains only its hard downstream closure. Soft
	// edges continue once their proof contract is machine-green.
	changed := true
	for changed {
		changed = false
		for _, id := range members {
			member := state[id]
			if member.State != armedWaveRunnable && member.State != armedWaveDependencyWaiting {
				continue
			}
			task := idx.Tasks[id]
			for _, edge := range v7TaskDependencyEdges(task, idx) {
				dep, inWave := state[edge.ID]
				if edge.Hardness != v7DependencyHardnessHard || !inWave {
					continue
				}
				if dep.State == armedWaveMachineParked || dep.State == armedWaveHumanBlocked {
					member.State = dep.State
					member.Reason = "hard dependency closure of " + edge.ID
					state[id] = member
					changed = true
					break
				}
			}
		}
	}

	active := 0
	for _, member := range state {
		if member.State == armedWaveRunning {
			active++
		}
	}
	remaining := maxInt(0, cap-active)
	snapshot := armedWaveSnapshot{WaveID: stringField(wave.Data, "id"), Authorization: authState, Concurrency: cap}
	for _, id := range members {
		member := state[id]
		if member.State == armedWaveRunnable {
			task := idx.Tasks[id]
			if edge, blocked := v7BlockingDependencyForReadiness(task, idx); blocked {
				member.State, member.Reason = armedWaveDependencyWaiting, "waiting for dependency "+edge.ID
			} else if len(snapshot.Frontier) < remaining {
				snapshot.Frontier = append(snapshot.Frontier, id)
			} else {
				member.Reason = "waiting for wave concurrency capacity"
			}
		}
		snapshot.Members = append(snapshot.Members, member)
	}
	sort.Strings(snapshot.Frontier)
	sort.Slice(snapshot.Members, func(i, j int) bool { return snapshot.Members[i].ID < snapshot.Members[j].ID })
	_ = now // reserved for lease-expiry-aware stores; RunStatus is already reconciled.
	return snapshot
}

func armedWaveProjectedIndex(vaultPath string, idx v7Index, wave Note) (v7Index, map[string]string) {
	if armedWaveIntegrated(wave) {
		return idx, map[string]string{}
	}
	members := normalizeList(wave.Data["members"])
	landed := armedWaveLandedMembers(wave)
	projectionErrors := map[string]string{}
	effectiveTasks := make(map[string]Note, len(idx.Tasks))
	for id, task := range idx.Tasks {
		effectiveTasks[id] = task
	}
	if v7GitRepo(v7RepoRoot(vaultPath)) {
		for _, id := range members {
			if !landed[id] {
				continue
			}
			task, ok := idx.Tasks[id]
			if !ok {
				continue
			}
			projected, projectedOK, err := armedWaveIntegrationTaskProjection(vaultPath, task)
			if err != nil {
				projectionErrors[id] = err.Error()
				continue
			}
			if !projectedOK {
				projectionErrors[id] = "landed task has no integration projection"
				continue
			}
			effectiveTasks[id] = projected
		}
	}
	idx.Tasks = effectiveTasks
	for _, id := range members {
		task, ok := idx.Tasks[id]
		if !ok || stringField(task.Data, "readiness") != "blocked_by_dependency" {
			continue
		}
		projectedState := v7ProjectedTaskState(vaultPath, task, idx)
		data := cloneNoteData(task.Data)
		for key, value := range projectedState {
			data[key] = value
		}
		task.Data = data
		idx.Tasks[id] = task
	}
	return idx, projectionErrors
}

func armedWaveLandedMembers(wave Note) map[string]bool {
	out := map[string]bool{}
	for _, row := range normalizeLandingAudit(wave.Data["landings"]) {
		if taskID := stringField(row, "task"); taskID != "" {
			out[taskID] = stringField(row, "gate_result") == "pass"
		}
	}
	return out
}

func armedWaveIntegrated(wave Note) bool {
	landed := false
	for _, row := range normalizeLandingAudit(wave.Data["landings"]) {
		if stringField(row, "task") == "wave" {
			landed = stringField(row, "gate_result") == "pass"
		}
	}
	return landed
}

func armedWaveTaskHumanBlocked(idx v7Index, task Note) bool {
	if strings.HasPrefix(stringField(task.Data, "readiness"), "waiting_on_human") {
		return true
	}
	taskID := stringField(task.Data, "id")
	for _, gate := range idx.Gates {
		if stringField(gate.Data, "status") == "open" && v7ProofOwnerClass(stringField(gate.Data, "owner")) == "human" && containsString(normalizeList(gate.Data["blocks"]), taskID) {
			return true
		}
	}
	return false
}

func armedWaveRunMachineParked(run RunStatus) bool {
	switch LeaseState(strings.TrimSpace(run.LeaseState)) {
	case LeaseStateParkedNoProgress, LeaseStateParkedBudget:
		return true
	default:
		return false
	}
}

func armedWaveForTask(vaultPath string, task Note) (Note, v7Index, bool) {
	waveID := stringField(task.Data, "wave")
	if waveID == "" {
		return Note{}, v7Index{}, false
	}
	idx, err := loadV7Index(vaultPath)
	if err != nil {
		return Note{}, v7Index{}, false
	}
	wave, ok := idx.Waves[waveID]
	if !ok {
		return Note{}, idx, false
	}
	auth := waveAuthorizationProjection(vaultPath, idx, wave)
	return wave, idx, stringField(auth, "state") == "armed" && !boolFromAny(auth["stale"])
}

func armedWaveIntegrationTaskProjection(vaultPath string, task Note) (Note, bool, error) {
	wave, _, armed := armedWaveForTask(vaultPath, task)
	if !armed || !armedWaveLandedMembers(wave)[stringField(task.Data, "id")] {
		return Note{}, false, nil
	}
	repoRoot := v7RepoRoot(vaultPath)
	rel, err := filepath.Rel(repoRoot, task.AbsolutePath)
	if err != nil || filepath.IsAbs(rel) || strings.HasPrefix(filepath.Clean(rel), "..") {
		return Note{}, false, tuskerError(errorInvalidTransition, "cannot project armed-wave task from integration: invalid canonical task path "+task.AbsolutePath)
	}
	integrationBranch := v7WaveIntegrationBranch(wave)
	projected, ok, err := v7GitNoteAtRef(repoRoot, integrationBranch, filepath.ToSlash(rel))
	if err != nil {
		return Note{}, false, err
	}
	if !ok {
		return Note{}, false, tuskerError(errorNotFound, "armed-wave integration task is missing: "+integrationBranch+":"+filepath.ToSlash(rel))
	}
	if effectiveV7Kind(projected.Data) != "task" || stringField(projected.Data, "id") != stringField(task.Data, "id") {
		return Note{}, false, tuskerError(errorInvalidField, "armed-wave integration task identity mismatch: "+integrationBranch+":"+filepath.ToSlash(rel))
	}
	projected.AbsolutePath = task.AbsolutePath
	return projected, true, nil
}

func v7GitNoteAtRef(repoRoot, ref, rel string) (Note, bool, error) {
	object := ref + ":" + rel
	if _, err := gitCombined(repoRoot, "cat-file", "-e", object); err != nil {
		return Note{}, false, nil
	}
	raw, err := gitCombined(repoRoot, "show", object)
	if err != nil {
		return Note{}, false, err
	}
	data, body, err := parseFrontmatter(raw)
	if err != nil {
		return Note{}, false, err
	}
	return Note{AbsolutePath: filepath.Join(repoRoot, filepath.FromSlash(rel)), RelativePath: filepath.ToSlash(rel), Data: data, Body: body}, true, nil
}

func armedWaveDispatchTaskProjection(vaultPath string, task Note) (Note, v7Index, bool, error) {
	wave, idx, armed := armedWaveForTask(vaultPath, task)
	if !armed {
		return task, idx, false, nil
	}
	projectedIdx, projectionErrors := armedWaveProjectedIndex(vaultPath, idx, wave)
	taskID := stringField(task.Data, "id")
	if reason := projectionErrors[taskID]; reason != "" {
		return task, projectedIdx, false, tuskerError(errorInvalidTransition, "armed-wave dispatch projection failed for "+taskID+": "+reason)
	}
	projected, ok := projectedIdx.Tasks[taskID]
	if !ok {
		return task, projectedIdx, false, tuskerError(errorNotFound, "armed-wave dispatch task is missing: "+taskID)
	}
	return projected, projectedIdx, true, nil
}

func armedWaveReviewDependencyBlocker(vaultPath string, task Note) string {
	wave, idx, armed := armedWaveForTask(vaultPath, task)
	if !armed {
		return ""
	}
	repoRoot := v7RepoRoot(vaultPath)
	integrationBranch := v7WaveIntegrationBranch(wave)
	for _, edge := range v7TaskDependencyEdges(task, idx) {
		dependencyID := edge.ID
		dependency, ok := idx.Tasks[dependencyID]
		if !ok {
			return "dependency " + dependencyID + " is missing"
		}
		rel, err := filepath.Rel(repoRoot, dependency.AbsolutePath)
		if err != nil || filepath.IsAbs(rel) || strings.HasPrefix(filepath.Clean(rel), "..") {
			return dependencyID + " integration state has an invalid task path"
		}
		integrated, ok, err := v7GitNoteAtRef(repoRoot, integrationBranch, filepath.ToSlash(rel))
		if err != nil || !ok {
			return dependencyID + " integration state is unavailable"
		}
		if status := strings.TrimSpace(stringField(integrated.Data, "status")); status != "done" {
			return "dependency " + dependencyID + " has not completed objective review (status " + fallback(status, "missing") + ")"
		}
	}
	return ""
}

func armedWaveDispatchBlocker(vaultPath string, task Note, wf Workflow, runs map[string]RunStatus) string {
	return automationDispatchScopeBlocker(vaultPath, task, wf, runs)
}

func armedWaveDispatchBlockerForArmedScope(vaultPath string, task Note, wf Workflow, runs map[string]RunStatus) string {
	wave, idx, armed := armedWaveForTask(vaultPath, task)
	if stringField(task.Data, "wave") == "" {
		return ""
	}
	if !armed {
		return "wave is not durably armed"
	}
	if workspaceStrategyFromWorkflow(wf.Workspace.Strategy) == WorkspaceStrategyShared {
		return "armed waves require an isolated worktree, clone, or copy workspace"
	}
	snapshot := buildArmedWaveSnapshot(vaultPath, idx, wave, runs, time.Now().UTC())
	if stringField(task.Data, "status") == "review" {
		active := 0
		for _, member := range snapshot.Members {
			if member.State == armedWaveRunning {
				active++
			}
		}
		if active < snapshot.Concurrency {
			return ""
		}
		return "armed wave concurrency ceiling reached"
	}
	for _, id := range snapshot.Frontier {
		if id == stringField(task.Data, "id") {
			return ""
		}
	}
	for _, member := range snapshot.Members {
		if member.ID == stringField(task.Data, "id") && member.State == armedWaveRunning {
			return ""
		}
	}
	return "task is outside the armed wave's current frontier or concurrency ceiling"
}
