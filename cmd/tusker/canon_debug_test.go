package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// Canon variants to identify which produced the stored pins.
func canonFullBody(body string) string { return v7CanonicalBody(body) }

func canonNamedCols(body string) string {
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
	var ledgerCols map[int]bool
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
			ledgerCols = nil
			out = append(out, line)
			continue
		}
		if !strings.HasPrefix(trimmed, "|") || (section != "acceptance" && section != "verification") {
			out = append(out, line)
			continue
		}
		cells := v7MarkdownTableCells(trimmed)
		isSep := strings.Contains(trimmed, "---")
		if ledgerCols == nil && !isSep {
			ledgerCols = map[int]bool{}
			for i, h := range cells {
				switch strings.ToLower(strings.TrimSpace(h)) {
				case "result", "notes", "blocked by":
					ledgerCols[i] = true
				}
			}
		}
		if waveMaterialLedgerRow(cells) {
			continue
		}
		var kept []string
		for i, c := range cells {
			if !ledgerCols[i] {
				kept = append(kept, c)
			}
		}
		out = append(out, "| "+strings.Join(kept, " | ")+" |")
	}
	return strings.Trim(strings.Join(out, "\n"), "\n")
}

func fpWith(data map[string]any, bodyFn func(string) string, body string) string {
	canon := map[string]any{
		"title":                stringField(data, "title"),
		"body":                 bodyFn(body),
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

func TestCanonVariants(t *testing.T) {
	for _, id := range []string{"FLW-T-0027", "FLW-T-0028", "ORC-T-0084", "ORC-T-0055", "WUX-T-0014", "FLW-T-0043", "FLW-T-0044", "FLW-T-0020"} {
		path := filepath.Join("..", "..", ".tusker", "work", "tasks", id+".md")
		data, body, err := parseFrontmatterMustRead(path)
		if err != nil {
			t.Fatal(err)
		}
		stored := stringField(data, "contract_fingerprint")
		full := fpWith(data, canonFullBody, body)
		named := fpWith(data, canonNamedCols, body)
		trunc := directWaveTaskContractFingerprint(data, body)
		match := func(v string) string {
			if v == stored {
				return "==STORED"
			}
			return ""
		}
		fmt.Printf("%s stored=%s\n  full=%s %s\n  named=%s %s\n  trunc=%s %s\n", id, stored[:20], full[:20], match(full), named[:20], match(named), trunc[:20], match(trunc))
	}
}
