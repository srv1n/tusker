package main

import (
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
	state := map[string]armedWaveMember{}

	for _, id := range members {
		task, ok := idx.Tasks[id]
		member := armedWaveMember{ID: id, State: armedWaveRunnable, Reason: "eligible in current armed-wave frontier"}
		switch {
		case !ok:
			member.State, member.Reason = armedWaveMachineParked, "task record is missing"
		case landed[id]:
			member.State, member.Reason = armedWaveLanded, "landed on wave integration branch"
		case stale || authState != "armed":
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
		if stringField(gate.Data, "status") == "open" && v7ProofOwnerClass(stringField(gate.Data, "owner")) == "human" && stringField(gate.Data, "task") == taskID {
			return true
		}
	}
	return false
}

func armedWaveRunMachineParked(run RunStatus) bool {
	return LeaseState(strings.TrimSpace(run.LeaseState)) == LeaseStateParkedNoProgress
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

func armedWaveDispatchBlocker(vaultPath string, task Note, wf Workflow, runs map[string]RunStatus) string {
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
