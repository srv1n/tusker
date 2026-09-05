package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestTrustPreflight exercises the public mutation/read paths against the
// same contract defect. It is intentionally not a wrapper around validators:
// status, next, daemon dispatch, and delivery Start each use their production
// entry point and read-only refusals must leave their input untouched.
func TestTrustPreflight(t *testing.T) {
	t.Run("status ready next and dispatch reject a placeholder contract", func(t *testing.T) {
		vault := v7DispatchTestVault(t)
		mustV7Proof(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Preflight fixture decision", "decision": "Use the current preflight contract."}, newV7Decision)
		mustV7Proof(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Incomplete contract"}, newV7Task)
		path := filepath.Join(vault, "work", "tasks", "APP-T-0001.md")
		data, body, err := parseFrontmatterMustRead(path)
		if err != nil {
			t.Fatal(err)
		}
		data["spec_refs"] = []string{"APP-D-0001"}
		data["state_rev"] = v7StateRev(data, body)
		content, err := serializeDocument(data, body, v7FrontmatterOrder["task"])
		if err != nil {
			t.Fatal(err)
		}
		if err := writeText(path, content); err != nil {
			t.Fatal(err)
		}
		before := mustReadIndexTest(t, path)

		err = statusV7Cmd(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "status": "ready", "by": "agent:test"})
		if err == nil || !strings.Contains(err.Error(), "not dispatchable") || !strings.Contains(err.Error(), "placeholder acceptance") {
			t.Fatalf("status ready accepted incomplete contract: %v", err)
		}
		assertEqual(t, before, mustReadIndexTest(t, path), "status-ready refusal must not write")
		if err := nextCmd(Args{"vault": vault, "quiet": "true"}); err == nil || errorToIssue(err).Code != errorNotFound {
			t.Fatalf("next must reject incomplete contract: %v", err)
		}
		note := mustV7Task(t, vault, "APP-T-0001")
		idx, err := loadV7Index(vault)
		if err != nil {
			t.Fatal(err)
		}
		if reason := daemonDispatchBlockedReason(vault, note, idx.Tasks, idx.Tasks); !strings.Contains(reason, "placeholder acceptance") {
			t.Fatalf("dispatch did not preserve contract refusal: %q", reason)
		}
	})

	t.Run("dependency wait is valid but not pickable", func(t *testing.T) {
		vault := v7DispatchTestVault(t)
		mustV7Proof(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Upstream"}, newV7Task)
		mustV7Proof(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Dependent"}, newV7Task)
		makeV7TaskDispatchableForTest(t, vault, "APP-T-0001")
		makeV7TaskDispatchableForTest(t, vault, "APP-T-0002")
		path := filepath.Join(vault, "work", "tasks", "APP-T-0002.md")
		data, body, err := parseFrontmatterMustRead(path)
		if err != nil {
			t.Fatal(err)
		}
		data["dependencies"] = []string{"APP-T-0001"}
		data["readiness"] = "blocked_by_dependency"
		data["state_rev"] = v7StateRev(data, body)
		content, err := serializeDocument(data, body, v7FrontmatterOrder["task"])
		if err != nil {
			t.Fatal(err)
		}
		if err := writeText(path, content); err != nil {
			t.Fatal(err)
		}
		note := mustV7Task(t, vault, "APP-T-0002")
		errs, _ := validateV7Note(note, validationContext{VaultPath: vault, RelativePath: note.RelativePath}, note.RelativePath)
		if len(errs) != 0 {
			t.Fatalf("dependency wait corrupted contract validity: %#v", errs)
		}
		if picked, ok := pickV7Next(vault, "APP", ""); !ok || stringField(picked.Data, "id") != "APP-T-0001" {
			t.Fatalf("must pick the runnable upstream, not its blocked dependent: picked=%q ok=%v", stringField(picked.Data, "id"), ok)
		}
	})

	t.Run("delivery start refuses stale confirmation without writes", func(t *testing.T) {
		vault := deliveryTestVault(t)
		plan := validDeliveryPlanV2()
		plan.HumanGates = nil
		path := writeDeliveryV2TestPlan(t, vault, plan)
		before := snapshotDeliveryRecords(t, vault)
		_, err := deliveryStart(Args{"vault": vault, "plan": path, "by": "human:test", "confirm": "sha256:stale", "quiet": "true"}, nil)
		if err == nil || !strings.Contains(err.Error(), "confirmed plan fingerprint differs") {
			t.Fatalf("delivery Start accepted stale confirmation: %v", err)
		}
		assertEqual(t, before, snapshotDeliveryRecords(t, vault), "stale delivery start must not write")
	})
}
