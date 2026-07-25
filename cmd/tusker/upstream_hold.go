package main

import "strings"

// Dependency-scoped hold for a red shared build-and-test.
//
// When a piece of work fails its shared build-and-test it carries a
// `build_failed: true` marker. Any task that depends on that piece must be
// held (quarantined) instead of handed out, so the breakage is not copied into
// fresh work. The hold is targeted: only the failed piece's own dependents are
// held, and the hold names the failed upstream piece. When the upstream piece
// goes green (its marker is cleared) the held work becomes pickable again.
//
// This is the dependency-scoped cousin of the project-wide quarantine
// (projectQuarantinedError): that one freezes a whole project; this one holds
// only the dependents of a single failed piece.

const (
	buildFailedField        = "build_failed"
	buildFailedCommandField = "build_failed_command"
	buildFailedProfileField = "build_failed_profile"
)

// v7BuildFailed reports whether a task's most recent shared build-and-test came
// back red. A green (or never-run) piece carries no marker.
func v7BuildFailed(dep Note) bool {
	return boolField(dep.Data, buildFailedField)
}

// v7UpstreamMarkerIsDead reports whether a dependency is in a terminal dead
// state (cancelled or superseded, which is how a discarded task is recorded). A
// red build marker on such a piece is never going to be cleared by a green run,
// so honoring it would quarantine dependents forever. "done" is deliberately
// NOT dead: a done piece can be green-on-status yet red-on-build, and that hold
// is exactly the case we want to keep.
func v7UpstreamMarkerIsDead(dep Note) bool {
	switch strings.ToLower(strings.TrimSpace(stringField(dep.Data, "status"))) {
	case "cancelled", "superseded":
		return true
	default:
		return false
	}
}

// v7HeldByFailedUpstream returns the id of the piece whose shared build-and-test
// is red and that therefore holds this task, plus whether the task is held. A
// task is held when either (a) one of its dependencies still carries a red build
// marker, or (b) the task's own wave carries the red command from a shared gate
// run — the latter is how a genuine shared failure reaches dependents even when
// the failing piece is a fresh repair task with no dependents of its own. Dead
// (cancelled/superseded) dependencies are ignored so their markers cannot pin a
// dependent forever.
func v7HeldByFailedUpstream(task Note, idx v7Index) (string, bool) {
	for _, edge := range v7TaskDependencyEdges(task, idx) {
		if edge.Hardness != v7DependencyHardnessHard {
			continue
		}
		dep, ok := idx.Tasks[edge.ID]
		if !ok {
			continue
		}
		if v7UpstreamMarkerIsDead(dep) {
			continue
		}
		if v7BuildFailed(dep) {
			return edge.ID, true
		}
	}
	if waveID := strings.TrimSpace(stringField(task.Data, "wave")); waveID != "" {
		if wave, ok := idx.Waves[waveID]; ok {
			if strings.TrimSpace(stringField(wave.Data, buildFailedCommandField)) != "" {
				return waveID, true
			}
		}
	}
	return "", false
}

// v7UpstreamFailureHoldReason renders the dispatch blocker string used by the
// pick/eligibility path when a task is held for an upstream failure. Keeping it
// in one place lets tests and the eligibility check agree on the wording.
func v7UpstreamFailureHoldReason(upstreamID string) string {
	return "held for upstream failure: " + upstreamID + " failed its build-and-test"
}
