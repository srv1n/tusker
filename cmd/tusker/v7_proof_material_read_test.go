package main

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestV7ProofReportReadsMaterialOnlyForPassingReceipt(t *testing.T) {
	for _, tc := range []struct {
		name     string
		check    string
		result   string
		receipt  bool
		wantRead bool
	}{
		{name: "pending with history", result: "pending", receipt: true},
		{name: "failed with history", result: "fail", receipt: true},
		{name: "blocked with history", result: "blocked", receipt: true},
		{name: "skipped with history", result: "skipped", receipt: true},
		{name: "waived with history", result: "waived", receipt: true},
		{name: "pass without receipt", result: "pass"},
		{name: "manual pass", check: "Manual smoke passed.", result: "pass", receipt: true},
		{name: "pass with receipt", result: "pass", receipt: true, wantRead: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := t.TempDir()
			bin := t.TempDir()
			calls := filepath.Join(bin, "git.calls")
			if err := os.WriteFile(filepath.Join(bin, "git"), []byte("#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$TUSKER_PROOF_TEST_GIT_CALLS\"\nexit 1\n"), 0o755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", bin)
			t.Setenv("TUSKER_PROOF_TEST_GIT_CALLS", calls)
			t.Setenv("TUSKER_STATE_ROOT", filepath.Join(repo, "state"))
			row := v7VerificationRow{CoverText: "A1", Check: fallback(tc.check, "command: true"), Result: tc.result}
			if tc.receipt {
				row.Notes = v7VerificationReceiptSchema + " material=recorded"
			}
			task := Note{
				Data: map[string]any{"id": "APP-T-0001", "proof_mode": "inline"},
				Body: "# Task\n\n## Acceptance\n\n| ID | Outcome |\n| --- | --- |\n| A1 | Works. |\n\n## Verification\n\n" + renderV7VerificationTable([]v7VerificationRow{row}),
			}
			vault := filepath.Join(repo, "vault")
			report := computeV7ProofReport(vault, task, v7Index{})
			_, err := os.Stat(calls)
			if read := err == nil; read != tc.wantRead {
				t.Fatalf("current material read=%v, want %v (stat: %v)", read, tc.wantRead, err)
			}
			want := computeV7ProofReportForMaterial(vault, task, v7Index{}, "", errors.New("material unavailable"))
			if !reflect.DeepEqual(report, want) {
				t.Fatalf("lazy material read changed the proof report:\ngot: %#v\nwant: %#v", report, want)
			}
		})
	}
}
