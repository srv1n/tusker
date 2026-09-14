package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// Temporary verification: every real-vault task record must verify under the
// current contract canon and state_rev rules. Run with:
//   go test ./cmd/tusker -run TestRealVaultContractDrift -count=1 -v
func TestRealVaultContractDrift(t *testing.T) {
	vault := filepath.Join("..", "..", ".tusker")
	matches, err := filepath.Glob(filepath.Join(vault, "work", "tasks", "*.md"))
	if err != nil || len(matches) == 0 {
		t.Fatalf("no real-vault tasks found: %v", err)
	}
	drifted := 0
	for _, path := range matches {
		data, body, err := parseFrontmatterMustRead(path)
		if err != nil {
			t.Errorf("%s: %v", filepath.Base(path), err)
			continue
		}
		note := Note{AbsolutePath: path, Data: data, Body: body}
		if reason := directWaveTaskContractStaleReason(note); reason != "" {
			drifted++
			stored := stringField(data, "contract_fingerprint")
			recomputed := directWaveTaskContractFingerprint(data, body)
			t.Logf("%s: %s\n  stored=%s recomputed=%s status=%s wave=%s",
				filepath.Base(path), reason, stored, recomputed,
				stringField(data, "status"), stringField(data, "wave"))
		}
	}
	t.Logf("checked %d tasks, %d drifted", len(matches), drifted)
	// Also check waves for authorization projection consistency.
	waveMatches, _ := filepath.Glob(filepath.Join(vault, "work", "waves", "*.md"))
	for _, path := range waveMatches {
		data, body, err := parseFrontmatterMustRead(path)
		if err != nil {
			continue
		}
		if !v7StateRevMatches(data, body, stringField(data, "state_rev")) {
			t.Logf("%s: wave state_rev mismatch", filepath.Base(path))
		}
	}
	_ = strings.TrimSpace
}
