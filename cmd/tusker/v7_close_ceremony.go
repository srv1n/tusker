package main

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// v7ClosePreflightRequest identifies the small amount of caller-specific
// context around the canonical close ceremony. DependencyRef is used by the
// completion reactor to name the exact integration ref whose dependency state
// will be protected by the Git CAS. Interactive close/accept leave it empty
// and use the reviewer-integrated view selected by their control authority.
type v7ClosePreflightRequest struct {
	Args              Args
	Actor             string
	Action            string
	RequireReview     bool
	Force             bool
	DependencyRef     string
	ExpectedStateRev  string
	ExpectedTaskID    string
	ExpectedTaskState string
}

// saveV7CloseProjectionCAS repeats the task identity/revision integrity check
// while holding the same lock that spans the replacement. Preflight's initial
// read is only a hint; without this check a raw edit that retained state_rev
// could move the integration ref and then wedge canonical projection.
func saveV7CloseProjectionCAS(path string, data map[string]any, body string, baseRev, expectedID string) (string, error) {
	lock, err := acquireV7DocumentLock(path, v7DocumentLockTimeout)
	if err != nil {
		return "", err
	}
	defer func() { _ = lock.Close() }()
	current, currentBody, err := parseFrontmatterMustRead(path)
	if err != nil {
		return "", err
	}
	if stringField(current, "id") != expectedID {
		return "", tuskerError(errorInvalidTransition, "close task identity drifted under lock")
	}
	currentRev := stringField(current, "state_rev")
	if currentRev == "" || !v7StateRevMatches(current, currentBody, currentRev) {
		return "", tuskerError("CAS_CONFLICT", "close task content changed without a refreshed state_rev", withPath(path))
	}
	if currentRev != baseRev {
		return "", tuskerError("CAS_CONFLICT", "close task revision drifted under lock", withPath(path))
	}
	next := cloneMap(data)
	next["updated_at"] = firstNonEmpty(stringField(next, "updated_at"), time.Now().UTC().Format(time.RFC3339))
	nextRev := v7StateRev(next, body)
	next["state_rev"] = nextRev
	content, err := serializeDocument(next, body, v7FrontmatterOrder["task"])
	if err != nil {
		return "", err
	}
	if err := atomicReplaceV7Document(path, content); err != nil {
		return "", err
	}
	data["state_rev"] = nextRev
	invalidateCachedNote(path)
	return nextRev, nil
}

type v7CloseDependencyAuthority struct {
	ID       string `json:"id"`
	Hardness string `json:"hardness"`
	Status   string `json:"status"`
	StateRev string `json:"state_rev"`
}

type v7ClosePreflightResult struct {
	Task                Note
	Index               v7Index
	Policy              v7ClosePolicy
	Risk                string
	RequiredEvidence    []string
	RequiredGateKinds   []string
	DependencyAuthority []v7CloseDependencyAuthority
}

// v7ClosePreflight is the one close-eligibility implementation shared by
// close, accept, and deterministic review completion. In particular,
// readiness semantics never leak into close semantics: a proof-green soft
// dependency may unblock execution, but v7UnclosedDependency still requires
// that dependency to be done before this ceremony can close the dependent.
func v7ClosePreflight(vaultPath string, task Note, idx v7Index, request v7ClosePreflightRequest) (v7ClosePreflightResult, error) {
	id := stringField(task.Data, "id")
	if request.ExpectedTaskID != "" && id != request.ExpectedTaskID {
		return v7ClosePreflightResult{}, tuskerError(errorInvalidTransition, "close preflight task identity drifted")
	}
	data, body, err := parseFrontmatterMustRead(task.AbsolutePath)
	if err != nil {
		return v7ClosePreflightResult{}, err
	}
	task.Data, task.Body = data, body
	id = stringField(data, "id")
	if request.ExpectedTaskID != "" && id != request.ExpectedTaskID {
		return v7ClosePreflightResult{}, tuskerError(errorInvalidTransition, "close preflight task identity drifted")
	}
	if rev := stringField(data, "state_rev"); rev == "" || !v7StateRevMatches(data, body, rev) {
		return v7ClosePreflightResult{}, tuskerError("CAS_CONFLICT", id+": close preflight task bytes do not match state_rev")
	}
	if request.ExpectedStateRev != "" && stringField(data, "state_rev") != request.ExpectedStateRev {
		return v7ClosePreflightResult{}, tuskerError(errorInvalidTransition, id+": close preflight task revision drifted")
	}
	if request.ExpectedTaskState != "" && stringField(data, "status") != request.ExpectedTaskState {
		return v7ClosePreflightResult{}, tuskerError(errorInvalidTransition, id+": close preflight task state drifted")
	}
	if request.RequireReview {
		if status := stringField(data, "status"); status != "review" && !request.Force {
			return v7ClosePreflightResult{}, tuskerError(
				errorInvalidTransition,
				id+": close requires status review",
				withHint("run `tusker status "+id+" review` on a control/local branch, or `tusker propose status "+id+" --status review` from an implementation branch"),
				withContext(map[string]any{"status": status}),
			)
		}
	}

	idx.Tasks = cloneNoteMap(idx.Tasks)
	idx.Tasks[id] = task
	if request.DependencyRef != "" {
		idx, err = v7CloseDependencyIndexAtRef(vaultPath, request.DependencyRef, task, idx)
		if err != nil {
			return v7ClosePreflightResult{}, err
		}
	} else {
		idx = v7ReviewerIntegratedDependencyIndex(vaultPath, request.Args, task, idx)
	}

	for _, gate := range idx.Gates {
		if stringField(gate.Data, "status") == "open" && boolField(gate.Data, "blocking") && containsString(normalizeList(gate.Data["blocks"]), id) {
			return v7ClosePreflightResult{}, tuskerError(errorInvalidTransition, v7ClosePreflightMessage(request.Action, id, "blocked by open gate "+stringField(gate.Data, "id")))
		}
	}
	if dep, blocked := v7UnclosedDependency(task, idx); blocked {
		return v7ClosePreflightResult{}, tuskerError(errorInvalidTransition, v7ClosePreflightMessage(request.Action, id, "blocked by unfinished dependency "+dep.ID))
	}
	if tuskerTier(vaultPath) >= 2 {
		if missing := missingRequiredEvidence(vaultPath, id, normalizeList(task.Data["evidence_required"])); len(missing) > 0 {
			return v7ClosePreflightResult{}, tuskerError(errorEvidenceGate, v7ClosePreflightMessage(request.Action, id, "missing required evidence: "+strings.Join(missing, ", ")))
		}
	}
	if err := enforceV7ClosePolicy(vaultPath, task, idx, request.Actor); err != nil {
		return v7ClosePreflightResult{}, err
	}
	if tuskerTier(vaultPath) >= 2 {
		if err := enforceV7AcceptanceClose(vaultPath, task, idx); err != nil {
			return v7ClosePreflightResult{}, err
		}
	}

	risk := strings.ToLower(fallback(stringField(task.Data, "risk"), "medium"))
	policy, err := v7ClosePolicyFor(vaultPath, risk)
	if err != nil {
		return v7ClosePreflightResult{}, err
	}
	requiredEvidence := mergeUniqueStrings(normalizeList(task.Data["evidence_required"]), policy.RequiredEvidence)
	requiredGates := append([]string{}, policy.RequiredGates...)
	sort.Strings(requiredEvidence)
	sort.Strings(requiredGates)
	dependencies := make([]v7CloseDependencyAuthority, 0, len(v7TaskDependencyEdges(task, idx)))
	for _, edge := range v7TaskDependencyEdges(task, idx) {
		dependency := idx.Tasks[edge.ID]
		dependencies = append(dependencies, v7CloseDependencyAuthority{
			ID: edge.ID, Hardness: edge.Hardness,
			Status: stringField(dependency.Data, "status"), StateRev: stringField(dependency.Data, "state_rev"),
		})
	}
	sort.Slice(dependencies, func(i, j int) bool {
		if dependencies[i].ID != dependencies[j].ID {
			return dependencies[i].ID < dependencies[j].ID
		}
		return dependencies[i].Hardness < dependencies[j].Hardness
	})
	return v7ClosePreflightResult{
		Task: task, Index: idx, Policy: policy, Risk: risk,
		RequiredEvidence: requiredEvidence, RequiredGateKinds: requiredGates,
		DependencyAuthority: dependencies,
	}, nil
}

func v7ClosePreflightMessage(action, id, detail string) string {
	if strings.EqualFold(strings.TrimSpace(action), "accept") {
		return id + ": accept refused, close " + detail
	}
	return id + ": close " + detail
}

func v7CloseDependencyIndexAtRef(vaultPath, ref string, task Note, idx v7Index) (v7Index, error) {
	repoRoot := v7RepoRoot(vaultPath)
	if !v7GitRepo(repoRoot) {
		return idx, tuskerError(errorInvalidTransition, "frozen close dependency ref is unavailable: "+ref)
	}
	if _, err := gitOutputTrim(repoRoot, "rev-parse", "--verify", ref+"^{commit}"); err != nil {
		return idx, tuskerError(errorInvalidTransition, "frozen close dependency ref is unavailable: "+ref)
	}
	idx.Tasks = cloneNoteMap(idx.Tasks)
	for _, raw := range normalizeList(task.Data["dependencies"]) {
		edge := parseV7DependencyEdge(raw)
		if edge.ID == "" || (edge.ExplicitHardness && edge.Hardness != v7DependencyHardnessHard && edge.Hardness != v7DependencyHardnessSoft) {
			return idx, tuskerError(errorInvalidField, "frozen close dependency is malformed: "+raw)
		}
		dependency, ok := idx.Tasks[edge.ID]
		if !ok {
			return idx, tuskerError(errorNotFound, "frozen close dependency is missing from canonical index: "+edge.ID)
		}
		rel, err := filepath.Rel(repoRoot, dependency.AbsolutePath)
		if err != nil || filepath.IsAbs(rel) || strings.HasPrefix(filepath.Clean(rel), "..") {
			return idx, fmt.Errorf("frozen close dependency path escapes repository: %s", edge.ID)
		}
		integrated, ok, err := v7GitNoteAtRef(repoRoot, ref, filepath.ToSlash(rel))
		if err != nil || !ok {
			if err != nil {
				return idx, err
			}
			return idx, tuskerError(errorNotFound, "frozen close dependency is missing from integration ref: "+edge.ID)
		}
		if effectiveV7Kind(integrated.Data) != "task" || stringField(integrated.Data, "id") != edge.ID {
			return idx, tuskerError(errorInvalidField, "frozen close dependency identity mismatch: "+edge.ID)
		}
		state := stringField(integrated.Data, "state_rev")
		if state == "" || !v7StateRevMatches(integrated.Data, integrated.Body, state) {
			return idx, tuskerError(errorInvalidTransition, "frozen close dependency state revision is missing or invalid: "+edge.ID)
		}
		if strings.ToLower(strings.TrimSpace(stringField(integrated.Data, "status"))) != "done" {
			// Keep the shared close contract stable: callers distinguish a normal
			// unfinished dependency from malformed/missing frozen evidence. This
			// is still fail-closed—the live task is never consulted.
			return idx, tuskerError(errorInvalidTransition, v7ClosePreflightMessage("close", stringField(task.Data, "id"), "blocked by unfinished dependency "+edge.ID))
		}
		integrated.AbsolutePath = dependency.AbsolutePath
		idx.Tasks[edge.ID] = integrated
	}
	return idx, nil
}

func applyV7TaskCloseProjection(data map[string]any, actor, now string, authority map[string]any) {
	data["status"] = "done"
	data["readiness"] = "done"
	if stringField(data, "proof_status") != "waived" {
		data["proof_status"] = "satisfied"
	}
	data["next_owner"] = "none"
	data["next_source"] = "status"
	data["next_ref"] = ""
	data["next_action"] = ""
	data["agent_action"] = ""
	data["machine_status"] = ""
	data["human_status"] = ""
	data["closeout_status"] = ""
	data["accepted_by"] = actor
	data["accepted_at"] = now
	data["closed_at"] = now
	data["updated_at"] = now
	data["updated_by"] = actor
	if authority == nil {
		delete(data, "close_authority")
	} else {
		data["close_authority"] = authority
	}
}
