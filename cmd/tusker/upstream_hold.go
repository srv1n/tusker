package main

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

const buildFailedField = "build_failed"

// v7BuildFailed reports whether a task's most recent shared build-and-test came
// back red. A green (or never-run) piece carries no marker.
func v7BuildFailed(dep Note) bool {
	return boolField(dep.Data, buildFailedField)
}

// v7HeldByFailedUpstream returns the id of the first dependency whose shared
// build-and-test is red, and whether the task is therefore held. The failed
// upstream is named so the hold can explain itself.
func v7HeldByFailedUpstream(task Note, idx v7Index) (string, bool) {
	for _, edge := range v7TaskDependencyEdges(task, idx) {
		dep, ok := idx.Tasks[edge.ID]
		if !ok {
			continue
		}
		if v7BuildFailed(dep) {
			return edge.ID, true
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
