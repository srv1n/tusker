package main

import (
	"strings"
	"testing"
)

func TestDogfoodTreeChange(t *testing.T) {
	for _, tc := range []struct {
		name, command, result string
		changed               bool
	}{
		{"failed with new file", "printf x > stray.txt; exit 1", "pending", true},
		{"failed without changes", "exit 1", "fail", false},
		{"passed with new file", "printf x > stray.txt", "pass", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			vault, store, project := adoptFixture(t, "APP-T-0001", nil)
			dogfoodSetChecks(t, vault, "APP-T-0001", tc.command)
			result := adoptRun(t, vault, store, project.ProjectID, "APP-T-0001", "agent:builder")
			rows := parseV7VerificationRows(walkthroughNote(t, vault, "APP-T-0001").Body)
			if len(rows) != 1 || rows[0].Result != tc.result {
				t.Fatalf("rows = %#v; refusal: %s", rows, result.Reason)
			}
			if result.OK != (tc.result == "pass") {
				t.Fatalf("unexpected outcome: %+v", result)
			}
			if tc.changed {
				if !strings.Contains(result.Reason, "changed while it ran") || !strings.Contains(result.Reason, "stray.txt") {
					t.Fatalf("missing tree change refusal: %s", result.Reason)
				}
				if v7VerificationReceiptFields(rows[0]) != nil {
					t.Fatal("pending row has a receipt")
				}
			} else if strings.Contains(result.Reason, "changed while it ran") {
				t.Fatalf("unexpected tree change refusal: %s", result.Reason)
			}
		})
	}
}
