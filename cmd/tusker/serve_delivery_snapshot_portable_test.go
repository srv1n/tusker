//go:build !darwin && !linux

package main

import (
	"runtime"
	"strings"
	"testing"
)

func TestServeDeliveryPlanSnapshotFailsClosedOnUnsupportedPlatform(t *testing.T) {
	snapshot, err := serveDeliveryPlanSnapshotAt(RegisteredProject{RepoRoot: t.TempDir()}, "plan.yaml", nil)
	if snapshot != nil || err == nil {
		t.Fatalf("unsupported %s snapshot was not refused: snapshot=%#v err=%v", runtime.GOOS, snapshot, err)
	}
	issue := errorToIssue(err)
	if issue.Code != errorInvalidTransition || !strings.Contains(issue.Message, "unavailable") || issue.Hint == "" {
		t.Fatalf("unsupported-platform refusal lost typed repair guidance: %#v", issue)
	}
}
