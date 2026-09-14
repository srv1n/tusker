package main

import (
	"strings"

	"gopkg.in/yaml.v3"
)

// legacyDirectWaveTruncatedContractBody reproduces the contract canon that
// wrote stored fingerprints before ledger columns were dropped by header name:
// it cut every Acceptance/Verification row to its first two cells, which also
// dropped authored columns such as Acceptance Proof. It exists only so
// reconcile can prove a stored pin was written under the old algorithm (an
// era rebase) rather than treating every mismatch as contract tampering.
func legacyDirectWaveTruncatedContractBody(body string) string {
	lines := strings.Split(v7CanonicalBody(body), "\n")
	filtered := make([]string, 0, len(lines))
	dropping := false
	inFence := false
	for _, line := range lines {
		delimiter := isFenceDelimiter(line)
		if delimiter {
			inFence = !inFence
		}
		if !delimiter && !inFence && strings.HasPrefix(strings.TrimSpace(line), "## ") {
			dropping = directWaveContractLedgerSections[strings.ToLower(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "## ")))]
			if dropping {
				continue
			}
		} else if dropping {
			continue
		}
		filtered = append(filtered, line)
	}
	if start, end := generatedReviewerFindingBounds(filtered); start != -1 {
		filtered = append(filtered[:start], filtered[end:]...)
	}
	var out []string
	section := ""
	inFence = false
	for _, line := range filtered {
		trimmed := strings.TrimSpace(line)
		if isFenceDelimiter(line) {
			inFence = !inFence
			out = append(out, line)
			continue
		}
		if inFence {
			out = append(out, line)
			continue
		}
		if strings.HasPrefix(trimmed, "## ") {
			section = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(trimmed, "## ")))
			out = append(out, line)
			continue
		}
		if !strings.HasPrefix(trimmed, "|") || (section != "acceptance" && section != "verification") {
			out = append(out, line)
			continue
		}
		cells := v7MarkdownTableCells(trimmed)
		if waveMaterialLedgerRow(cells) {
			continue
		}
		if len(cells) > 2 {
			cells = cells[:2]
		}
		out = append(out, "| "+strings.Join(cells, " | ")+" |")
	}
	return strings.Trim(strings.Join(out, "\n"), "\n")
}

// legacyDirectWaveTaskContractFingerprint is the stored-pin algorithm prior to
// the authored-column canon fix. A stored pin matching this value for the
// current bytes proves the record was pinned under the old algorithm, so
// reconcile may rebase it to the current canon without certifying tampered
// content.
func legacyDirectWaveTaskContractFingerprint(data map[string]any, body string) string {
	canon := map[string]any{
		"title":                stringField(data, "title"),
		"body":                 legacyDirectWaveTruncatedContractBody(body),
		"artifact_contract":    data["artifact_contract"],
		"proof_mode":           stringField(data, "proof_mode"),
		"proof_required":       sortedStrings(normalizeList(data["proof_required"])),
		"proof_required_owner": data["proof_required_owner"],
		"evidence_budget":      intField(data, "evidence_budget"),
		"evidence_required":    sortedStrings(normalizeList(data["evidence_required"])),
		"dependencies":         sortedStrings(normalizeList(data["dependencies"])),
		"gates":                sortedStrings(normalizeList(data["gates"])),
		"work_level":           stringField(data, "work_level"),
		"review_level":         stringField(data, "review_level"),
		"review_reason":        stringField(data, "review_reason"),
		"owned_paths":          sortedStrings(normalizeList(data["owned_paths"])),
		"generated_outputs":    sortedStrings(normalizeList(data["generated_outputs"])),
	}
	raw, _ := yaml.Marshal(canon)
	return v7Fingerprint(raw)
}
