package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const waveAuthorizationSchema = "tusker.wave-authorization/v1"

// closeV7AuthorizationLock is a narrow seam for exercising the transaction's
// release failure handling. v7DocumentLock.Close itself always attempts both
// unlock and file close; this seam must therefore only be used while releasing
// authorization transaction locks.
var closeV7AuthorizationLock = func(lock *v7DocumentLock) error {
	return lock.Close()
}

func waveV7PauseCmd(args Args) error {
	return directWaveControlCmd(args, "wave pause", directWavePause)
}

func waveV7ResumeCmd(args Args) error {
	return directWaveControlCmd(args, "wave resume", directWaveResume)
}

func waveMaterialFingerprint(vaultPath string, idx v7Index, wave Note) (string, []string) {
	material := map[string]any{"schema": waveAuthorizationSchema, "wave": stringField(wave.Data, "id"), "outcome": firstNonEmpty(stringField(wave.Data, "outcome"), stringField(wave.Data, "summary")), "integration_base_sha": stringField(wave.Data, "integration_base_sha"), "members": []any{}, "specs": map[string]any{}}
	var issues []string
	members := uniqueStrings(normalizeList(wave.Data["members"]))
	sort.Strings(members)
	rows := make([]any, 0, len(members))
	for _, id := range members {
		task, ok := idx.Tasks[id]
		if !ok {
			issues = append(issues, "member task does not resolve: "+id)
			continue
		}
		row := map[string]any{
			"id": id, "title": stringField(task.Data, "title"), "epic": stringField(task.Data, "epic"), "risk": stringField(task.Data, "risk"),
			"dependencies": sortedStrings(normalizeList(task.Data["dependencies"])), "spec_refs": sortedStrings(normalizeList(task.Data["spec_refs"])),
			"contract_fingerprint": directWaveTaskContract(task), "artifact_contract": task.Data["artifact_contract"],
			"owned_paths": sortedStrings(normalizeList(task.Data["owned_paths"])), "generated_outputs": sortedStrings(normalizeList(task.Data["generated_outputs"])), "runner_profile": stringField(task.Data, "runner_profile"), "complexity": stringField(task.Data, "complexity"),
			"work_level": stringField(task.Data, "work_level"), "review_level": stringField(task.Data, "review_level"), "review_reason": stringField(task.Data, "review_reason"),
			"execute_profile": stringField(task.Data, "execute_profile"), "review_profile": stringField(task.Data, "review_profile"),
			"proof_contract":        map[string]any{"mode": stringField(task.Data, "proof_mode"), "required": sortedStrings(normalizeList(task.Data["proof_required"])), "required_owner": task.Data["proof_required_owner"], "evidence_budget": intField(task.Data, "evidence_budget"), "evidence_required": sortedStrings(normalizeList(task.Data["evidence_required"]))},
			"gates":                 waveMaterialGates(idx, id),
			"dependency_contracts":  task.Data["dependency_contracts"],
			"close_policy_snapshot": task.Data["close_policy_snapshot"],
		}
		// Authored tasks carry an immutable contract fingerprint. Their live
		// Verification table is also the proof ledger, so reviewers may append
		// rows without invalidating authorization.
		if stringField(task.Data, "contract_fingerprint") == "" {
			row["acceptance"] = waveMaterialTable(sectionContent(task.Body, "## Acceptance"), []int{0, 1})
			row["verification"] = waveMaterialTable(sectionContent(task.Body, "## Verification"), []int{0, 1})
		}
		rows = append(rows, row)
	}
	material["members"] = rows
	allSpecRefs := append([]string{}, normalizeList(wave.Data["spec_refs"])...)
	for _, id := range members {
		if task, ok := idx.Tasks[id]; ok {
			allSpecRefs = append(allSpecRefs, normalizeList(task.Data["spec_refs"])...)
		}
	}
	for _, ref := range sortedStrings(allSpecRefs) {
		path := v7SpecRefPath(vaultPath, ref)
		if path == "" || !fileExists(path) {
			issues = append(issues, "spec_ref does not resolve: "+ref)
			continue
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			issues = append(issues, "spec_ref cannot be read: "+ref)
			continue
		}
		sum := sha256.Sum256(raw)
		material["specs"].(map[string]any)[ref] = hex.EncodeToString(sum[:])
	}
	raw, _ := yaml.Marshal(material)
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:]), uniqueStrings(issues)
}

func waveMaterialGates(idx v7Index, taskID string) []any {
	var out []any
	for _, gate := range sortedV7Gates(idx) {
		if !containsString(normalizeList(gate.Data["blocks"]), taskID) && !containsString(normalizeList(idx.Tasks[taskID].Data["gates"]), stringField(gate.Data, "id")) {
			continue
		}
		row := map[string]any{"id": stringField(gate.Data, "id"), "status": stringField(gate.Data, "status"), "gate_kind": stringField(gate.Data, "gate_kind"), "owner": stringField(gate.Data, "owner"), "blocking": boolField(gate.Data, "blocking"), "blocks": sortedStrings(normalizeList(gate.Data["blocks"])), "covers": sortedStrings(normalizeList(gate.Data["covers"])), "action": stringField(gate.Data, "action"), "verification": stringField(gate.Data, "verification"), "why_agent_cannot": v7GateBoundaryText(gate), "suggestion": v7GateSuggestionText(gate)}
		kind := strings.ToLower(stringField(gate.Data, "gate_kind"))
		status := stringField(gate.Data, "status")
		if (kind == "auth" || kind == "release") && (status == "satisfied" || status == "waived") {
			current, incomplete := v7GateHardClosureFingerprint(gate.Data, idx)
			row["dependency_material_fingerprint"] = stringField(gate.Data, "dependency_material_fingerprint")
			row["current_dependency_material_fingerprint"] = current
			row["dependency_material_incomplete"] = sortedStrings(incomplete)
		}
		out = append(out, row)
	}
	return out
}

// waveMaterialLedgerRow reports whether a Verification/Acceptance table row is
// a lifecycle receipt rather than authored contract. Completion appends the
// typed-review receipt after independent review; hashing it would stale a wave
// after its first successful landing. The same predicate keeps the receipt out
// of the pinned contract canon in directWaveCanonicalContractBody.
func waveMaterialLedgerRow(cells []string) bool {
	return len(cells) > 3 && strings.HasPrefix(strings.TrimSpace(cells[1]), "typed review ") && strings.Contains(cells[3], "[tusker-review-result:")
}

func waveMaterialTable(section string, columns []int) []string {
	var out []string
	for _, line := range strings.Split(section, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "|") || strings.Contains(line, "---") {
			continue
		}
		cells := v7MarkdownTableCells(line)
		if len(cells) == 0 || strings.EqualFold(strings.TrimSpace(cells[0]), "ID") || strings.EqualFold(strings.TrimSpace(cells[0]), "Covers") {
			continue
		}
		if waveMaterialLedgerRow(cells) {
			continue
		}
		var selected []string
		for _, column := range columns {
			if column < len(cells) {
				selected = append(selected, strings.TrimSpace(cells[column]))
			}
		}
		out = append(out, strings.Join(selected, "|"))
	}
	return out
}

func closeV7DocumentLocks(locks []*v7DocumentLock) error {
	var errs []error
	for index := len(locks) - 1; index >= 0; index-- {
		if err := closeV7AuthorizationLock(locks[index]); err != nil {
			errs = append(errs, fmt.Errorf("member authorization lock %d: %w", index, err))
		}
	}
	return errors.Join(errs...)
}

func closeV7AuthorizationLocks(materialLock, waveLock *v7DocumentLock, taskLocks []*v7DocumentLock) error {
	var errs []error
	if err := closeV7DocumentLocks(taskLocks); err != nil {
		errs = append(errs, err)
	}
	if waveLock != nil {
		if err := closeV7AuthorizationLock(waveLock); err != nil {
			errs = append(errs, fmt.Errorf("wave authorization lock: %w", err))
		}
	}
	if materialLock != nil {
		if err := closeV7AuthorizationLock(materialLock); err != nil {
			errs = append(errs, fmt.Errorf("material authorization lock: %w", err))
		}
	}
	return errors.Join(errs...)
}

func combineV7AuthorizationLockCloseError(cause, closeErr error) error {
	if closeErr == nil {
		return cause
	}
	if cause == nil {
		return fmt.Errorf("authorization lock release failed: %w", closeErr)
	}
	return errors.Join(cause, fmt.Errorf("authorization lock release also failed: %w", closeErr))
}

func waveAuthorizationProjection(vaultPath string, idx v7Index, wave Note) map[string]any {
	fingerprint, _ := waveMaterialFingerprint(vaultPath, idx, wave)
	stored, state := stringField(wave.Data, "authorization_fingerprint"), fallback(stringField(wave.Data, "authorization"), "disarmed")
	stale := waveAuthorizationFingerprintActive(state) && stored != "" && stored != fingerprint
	if stale {
		state = "stale"
	}
	return map[string]any{"state": state, "stale": stale, "fingerprint": fingerprint, "authorizedFingerprint": nullIfBlank(stored), "actor": nullIfBlank(stringField(wave.Data, "authorized_by")), "at": nullIfBlank(stringField(wave.Data, "authorized_at")), "action": waveAuthorizationProjectionAction(state)}
}

func waveAuthorizationFingerprintActive(state string) bool {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "armed", "paused":
		return true
	default:
		return false
	}
}

func waveAuthorizationProjectionAction(state string) string {
	switch state {
	case "armed":
		return "none"
	case "paused":
		return "tusker wave resume <wave-id>"
	case "stale":
		return "tusker wave start <wave-id> --by <actor>"
	default:
		return "tusker wave start <wave-id> --by <actor>"
	}
}

func waveIntegrationBaseClean(vaultPath string, wave Note) bool {
	repoRoot := v7RepoRoot(vaultPath)
	branch := firstNonEmpty(stringField(wave.Data, "integration_branch"), v7IntegrationBranchName(stringField(wave.Data, "id")))
	if !v7GitRepo(repoRoot) {
		return false
	}
	defaultBranch := v7DefaultBranch(vaultPath)
	baseRev, baseErr := gitCombined(repoRoot, "rev-parse", "refs/heads/"+defaultBranch)
	if baseErr != nil {
		return false
	}
	baseRev = strings.TrimSpace(baseRev)
	frozenBase := strings.TrimSpace(stringField(wave.Data, "integration_base_sha"))
	branchRef := "refs/heads/" + branch
	if !gitRefExists(repoRoot, branchRef) {
		// A newly authored wave intentionally has no integration ref. It is
		// clean only while its frozen base still is the configured default.
		if frozenBase == "" || frozenBase != baseRev {
			return false
		}
	} else {
		integrationRev, integrationErr := gitCombined(repoRoot, "rev-parse", branchRef)
		if integrationErr != nil {
			return false
		}
		integrationRev = strings.TrimSpace(integrationRev)
		expected := firstNonEmpty(frozenBase, baseRev)
		if stringField(wave.Data, "authorized_at") == "" {
			if integrationRev != expected {
				return false
			}
		} else {
			if _, err := gitCombined(repoRoot, "merge-base", "--is-ancestor", expected, integrationRev); err != nil {
				return false
			}
		}
	}
	for _, worktree := range v7ListWorktrees(repoRoot) {
		if worktree.Branch != branchRef {
			continue
		}
		dirty, err := inPlaceDirtyPaths(worktree.Path)
		if err != nil || len(dirty) > 0 {
			return false
		}
	}
	return true
}

func sortedStrings(values []string) []string {
	values = uniqueStrings(values)
	sort.Strings(values)
	return values
}

func cloneMap(src map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range src {
		out[k] = v
	}
	return out
}

func cloneNoteMap(src map[string]Note) map[string]Note {
	out := map[string]Note{}
	for k, v := range src {
		out[k] = v
	}
	return out
}
