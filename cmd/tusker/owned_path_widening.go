package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Owned-path widening (WML M6). A reviewer that finds the work correct but
// touching files outside the task's owned_paths records a proof-scope blocking
// finding naming those files. Bouncing that to the implementer cannot fix it
// (the code is right; the contract is narrow), so the task burned its review
// cycles and parked. Tusker widens owned_paths itself when it is safe, and
// otherwise opens a human decision gate so the request reaches the operator.
const (
	ownedPathWideningEditActor   = "agent:tusker-governor"
	ownedPathWideningStatusActor = "tusker:governor"
	ownedPathWideningLogPrefix   = "tusker-governor widened owned_paths: "
	ownedPathWideningGateMarker  = "[tusker-governor:owned-path-widening]"
)

// widenOwnedPathsForReview runs before a changes_requested review is returned
// to the implementer. handled=true means the bounce must not happen: the
// owned paths were widened and review re-requested, an earlier widening
// already answered this same result, or the request was routed to the operator.
//
// Detection is structural: every blocking finding must be repair_scope
// "proof" (the material is correct) and name, in its evidence, at least one
// file the submission changed outside owned_paths. Prose is not consulted.
func widenOwnedPathsForReview(vaultPath, taskID, findingText string) (bool, error) {
	var findings []reviewerFindingRecord
	for _, line := range strings.Split(findingText, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		finding, err := parseReviewerFinding(line)
		if err != nil {
			return false, nil
		}
		if finding.Kind == "blocking" {
			findings = append(findings, finding)
		}
	}
	if len(findings) == 0 {
		return false, nil
	}
	note, err := resolveV7Note(vaultPath, taskID, "task")
	if err != nil || stringField(note.Data, "status") != "review" {
		return false, err
	}
	changed, ok := v7DocTouchChangedPaths(vaultPath, note)
	if !ok {
		return false, nil
	}
	scope := append(normalizeOwnedPaths(normalizeList(note.Data["owned_paths"])), normalizeOwnedPaths(normalizeList(note.Data["generated_outputs"]))...)
	widened := ownedPathWidenedByGovernor(note.Body)
	var want []string
	for _, finding := range findings {
		if finding.RepairScope != reviewerFindingRepairScopeProof {
			return false, nil
		}
		// A finding is a widening request when it names a changed file outside
		// owned_paths, or one an earlier governor widening already added (the
		// same result seen again before its fresh review runs).
		hit := false
		for _, path := range ownedPathWideningNamedFiles(finding.Evidence, changed) {
			if !workspaceMaterialScopeContains(scope, path) {
				want = append(want, path)
				hit = true
			} else if widened[path] {
				hit = true
			}
		}
		if !hit {
			return false, nil
		}
	}
	want = sortedUniqueStrings(want)
	if len(want) == 0 {
		// The current result was already answered by a widening; wait for the
		// fresh review instead of bouncing the stale verdict.
		return true, nil
	}
	if reason := ownedPathWideningRefusal(vaultPath, note, want); reason != "" {
		return ownedPathWideningRouteToOperator(vaultPath, taskID, want, reason)
	}
	return true, applyOwnedPathWidening(vaultPath, note, want)
}

// ownedPathWideningNamedFiles returns the changed files a finding's evidence
// names. Evidence entries may list several files and carry ":line" suffixes;
// a bare file name matches a changed path by its trailing path segments.
func ownedPathWideningNamedFiles(evidence, changed []string) []string {
	var out []string
	for _, entry := range evidence {
		for _, token := range strings.FieldsFunc(entry, func(r rune) bool { return r == ',' || r == ' ' || r == ';' || r == '\t' || r == '\n' }) {
			token = strings.Trim(strings.TrimSpace(token), "`'\"()[]")
			if i := strings.Index(token, ":"); i > 0 {
				token = token[:i]
			}
			token = strings.Trim(filepath.ToSlash(token), "/")
			if token == "" {
				continue
			}
			for _, path := range changed {
				if path == token || strings.HasSuffix(path, "/"+token) {
					out = append(out, path)
				}
			}
		}
	}
	return sortedUniqueStrings(out)
}

func ownedPathWidenedByGovernor(body string) map[string]bool {
	out := map[string]bool{}
	for _, line := range strings.Split(body, "\n") {
		if i := strings.Index(line, ownedPathWideningLogPrefix); i >= 0 {
			rest := line[i+len(ownedPathWideningLogPrefix):]
			if j := strings.Index(rest, " ("); j >= 0 {
				rest = rest[:j]
			}
			for _, path := range strings.Split(rest, ",") {
				if path = strings.Trim(strings.TrimSpace(path), "`"); path != "" {
					out[path] = true
				}
			}
		}
	}
	return out
}

// ownedPathWideningRefusal names why a widening is unsafe, or "". A file is
// unsafe when another active task already claims it (in the vault, or through
// a live run via ownedPathConflict), or when it matches a do-not-touch list:
// `do_not_touch` in the task frontmatter or in WORKFLOW.md frontmatter.
func ownedPathWideningRefusal(vaultPath string, task Note, want []string) string {
	taskID := stringField(task.Data, "id")
	for _, source := range ownedPathDoNotTouchLists(vaultPath, task) {
		for _, path := range want {
			for _, entry := range source.paths {
				if ownedPathsOverlap(path, entry) {
					return fmt.Sprintf("%s matches do-not-touch entry %s in %s", path, entry, source.name)
				}
			}
		}
	}
	idx, err := loadV7Index(vaultPath)
	if err != nil {
		return "task index could not be loaded: " + err.Error()
	}
	candidate := Note{Data: map[string]any{"id": taskID, "owned_paths": want}}
	wanted := taskOwnershipClaims(candidate)
	for id, other := range idx.Tasks {
		if id == taskID {
			continue
		}
		switch strings.ToLower(stringField(other.Data, "status")) {
		case "done", "superseded", "cancelled", "idea":
			continue
		}
		if mine, theirs, overlaps := firstTaskOwnershipOverlap(wanted, taskOwnershipClaims(other)); overlaps {
			return fmt.Sprintf("%s is owned by active task %s (%s)", mine.Value, id, theirs.Value)
		}
	}
	if store, err := directWaveReviewRuntimeStore(); err == nil {
		defer store.Close()
		if runs, err := store.ListRuns(); err == nil {
			if conflict, found := ownedPathConflict(candidate, idx.Tasks, runs, time.Now().UTC()); found {
				return fmt.Sprintf("%v is held by live task %v", conflict["candidate_path"], conflict["task_id"])
			}
		}
	}
	return ""
}

type ownedPathDoNotTouch struct {
	name  string
	paths []string
}

func ownedPathDoNotTouchLists(vaultPath string, task Note) []ownedPathDoNotTouch {
	out := []ownedPathDoNotTouch{{name: "task frontmatter do_not_touch", paths: normalizeOwnedPaths(normalizeList(task.Data["do_not_touch"]))}}
	if text, err := os.ReadFile(workflowPath(vaultPath)); err == nil {
		if data, _, err := parseFrontmatter(string(text)); err == nil {
			out = append(out, ownedPathDoNotTouch{name: "WORKFLOW.md do_not_touch", paths: normalizeOwnedPaths(normalizeList(data["do_not_touch"]))})
		}
	}
	return out
}

// applyOwnedPathWidening widens through the same path as
// `tusker edit --owned-paths`, records why in the work log, puts the task back
// in review, and starts a fresh review-cycle window so the re-review is not
// charged against the cap.
func applyOwnedPathWidening(vaultPath string, note Note, want []string) error {
	taskID := stringField(note.Data, "id")
	before, beforeErr := loadV7Index(vaultPath)
	owned := sortedUniqueStrings(append(normalizeList(note.Data["owned_paths"]), want...))
	if err := updateV7TaskCmd(Args{
		"vault": vaultPath, "id": taskID, "if-revision": stringField(note.Data, "state_rev"),
		"owned-paths": strings.Join(owned, ","), "by": ownedPathWideningEditActor, "quiet": "true", "local": "true",
	}); err != nil {
		return err
	}
	fresh, err := resolveV7Note(vaultPath, taskID, "task")
	if err != nil {
		return err
	}
	data, body := fresh.Data, fresh.Body
	if !strings.Contains(body, "\n## Work log") && !strings.HasPrefix(body, "## Work log") {
		body = strings.TrimRight(body, "\n") + "\n\n## Work log\n"
	}
	now := time.Now().UTC().Format(time.RFC3339)
	body = appendWorkLogBullet(body, now+" "+ownedPathWideningLogPrefix+strings.Join(want, ", ")+" (the reviewer found the work correct but outside owned_paths; no other active task or do-not-touch list claims these files; review re-requested)")
	data["updated_at"] = now
	data["updated_by"] = ownedPathWideningEditActor
	if _, err := saveV7DocumentCAS(fresh.AbsolutePath, data, body, v7FrontmatterOrder["task"], stringField(data, "state_rev")); err != nil {
		return err
	}
	if err := statusV7CmdAsInternalActor(Args{
		"vault": vaultPath, "quiet": "true", "local": "true", "id": taskID, "status": "review",
		"reason": "owned_paths widened after review; review re-requested",
	}, ownedPathWideningStatusActor); err != nil {
		return err
	}
	if beforeErr == nil {
		if err := reauthorizeWaveAfterWidening(vaultPath, before, taskID, want); err != nil {
			return err
		}
	}
	return resetReviewCycleWindowForWidening(taskID)
}

// reauthorizeWaveAfterWidening keeps the task's wave authorized across the
// widening (WML M5/M6). owned_paths are part of a member's promise, so the
// widening stales the wave's authorization; the reviewer sanctioned this
// change, so the governor records the new authorization itself. It acts only
// when the authorization covered the wave just before the widening and the
// widened task is the only material that changed since; any other promise
// change still waits for a human `tusker start`.
func reauthorizeWaveAfterWidening(vaultPath string, before v7Index, taskID string, want []string) error {
	task, ok := before.Tasks[taskID]
	if !ok {
		return nil
	}
	waveID := stringField(task.Data, "wave")
	wave, ok := before.Waves[waveID]
	if !ok {
		return nil
	}
	state := stringField(wave.Data, "authorization")
	prior, issues := waveMaterialFingerprint(vaultPath, before, wave)
	if len(issues) > 0 || !waveAuthorizationFingerprintActive(state) || !directWaveAuthorizationCurrent(wave.Data, state, prior) {
		return nil
	}
	materialLock, err := acquireV7MaterialEpochLock(vaultPath)
	if err != nil {
		return err
	}
	defer materialLock.Close()
	idx, err := loadV7Index(vaultPath)
	if err != nil {
		return err
	}
	widened, taskOK := idx.Tasks[taskID]
	current, waveOK := idx.Waves[waveID]
	if !taskOK || !waveOK {
		return nil
	}
	fingerprint, issues := waveMaterialFingerprint(vaultPath, idx, current)
	expectedIdx := before
	expectedIdx.Tasks = cloneNoteMap(before.Tasks)
	expectedIdx.Tasks[taskID] = widened
	expected, _ := waveMaterialFingerprint(vaultPath, expectedIdx, wave)
	if len(issues) > 0 || fingerprint == prior || fingerprint != expected {
		return nil
	}
	waveLock, err := acquireV7DocumentLock(current.AbsolutePath, v7DocumentLockTimeout)
	if err != nil {
		return err
	}
	defer waveLock.Close()
	data, body, err := parseFrontmatterMustRead(current.AbsolutePath)
	if err != nil {
		return err
	}
	if !directWaveAuthorizationCurrent(data, state, prior) {
		return nil
	}
	now := time.Now().UTC()
	reason := fmt.Sprintf("owned_paths of %s widened to include %s after review; re-authorized by Tusker", taskID, strings.Join(want, ", "))
	data["authorization_fingerprint"] = fingerprint
	data["authorized_by"] = ownedPathWideningEditActor
	data["authorized_at"] = now.Format(time.RFC3339Nano)
	data["authorization_reason"] = reason
	data["updated_at"] = now.Format(time.RFC3339)
	data["updated_by"] = ownedPathWideningEditActor
	data["state_rev"] = v7StateRev(data, body)
	content, err := serializeDocument(data, body, v7FrontmatterOrder["wave"])
	if err != nil {
		return err
	}
	payload := map[string]any{"authorization": state, "fingerprint": fingerprint, "replaced_fingerprint": prior, "reason": reason}
	eventPath, eventContent, err := prepareV7Event(vaultPath, waveID, "wave", "updated", ownedPathWideningEditActor, payload, now)
	if err != nil {
		return err
	}
	if err := ensureDir(filepath.Dir(eventPath)); err != nil {
		return err
	}
	return commitV7DocumentWritesWithLocks(map[string]string{current.AbsolutePath: content, eventPath: eventContent}, 0, []*v7DocumentLock{waveLock})
}

// resetReviewCycleWindowForWidening records a budget redrive for the task's
// run: the review cap counts attempts since the last redrive, so the review a
// widening triggers starts a fresh window. No runtime store means no daemon
// state to reset.
func resetReviewCycleWindowForWidening(taskID string) error {
	stateRoot := DefaultStateRoot()
	if !fileExists(runtimeStoreDBPath(stateRoot)) {
		return nil
	}
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		return err
	}
	defer store.Close()
	runs, err := store.ListRuns()
	if err != nil {
		return err
	}
	for _, run := range runs {
		if run.ItemID == taskID || run.RecordID == taskID {
			if _, err := store.RecordBudgetRedrive(run.ProjectID, run.RecordID, ownedPathWideningEditActor, "owned_paths widened after review; review re-requested", time.Now()); err != nil {
				return err
			}
		}
	}
	return nil
}

// ownedPathWideningRouteToOperator opens one human decision gate per refused
// request; the task stays in review (a rework task hides human gates from the
// needs queue). Once the operator resolves that gate, the same result falls
// through to the ordinary bounce.
func ownedPathWideningRouteToOperator(vaultPath, taskID string, want []string, reason string) (bool, error) {
	action := fmt.Sprintf("Decide whether %s may own %s: %s. %s", taskID, strings.Join(want, ", "), reason, ownedPathWideningGateMarker)
	idx, err := loadV7Index(vaultPath)
	if err != nil {
		return false, err
	}
	ids := make([]string, 0, len(idx.Gates))
	for id := range idx.Gates {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		gate := idx.Gates[id]
		if v7GateTouchesTask(gate, taskID) && stringField(gate.Data, "action") == action {
			return stringField(gate.Data, "status") == "open", nil
		}
	}
	err = newV7Gate(Args{
		"vault": vaultPath, "blocks": taskID, "kind": "decision", "owner": "human:operator", "quiet": "true", "local": "true",
		"by":               ownedPathWideningEditActor,
		"action":           action,
		"verification":     "The operator widens owned_paths with tusker edit --owned-paths, or resolves this gate to send the finding back to the implementer.",
		"why-agent-cannot": "Ownership conflict: Tusker only widens owned files nobody else claims; this file is claimed or protected, so the ownership choice belongs to the operator.",
		"suggestion":       "Move the change into a task that owns the file, or widen this task if the other claim is stale.",
	})
	return err == nil, err
}
