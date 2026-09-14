package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestTrustPreflight exercises the public mutation/read paths against the
// same contract defect. It is intentionally not a wrapper around validators:
// status, next, daemon dispatch, and direct Start each use their production
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

	t.Run("wave resume refuses stale material without extra writes", func(t *testing.T) {
		vault, store, _ := authorityFixture(t)
		writeDirectTask(t, vault, "APP-T-0001", "W-0001", nil)
		writeDirectWave(t, vault, "W-0001", []string{"APP-T-0001"}, nil)
		if _, err := directWaveStart(vault, store, "W-0001", "human:test"); err != nil {
			t.Fatal(err)
		}
		if _, err := directWavePause(vault, store, "W-0001", "human:test"); err != nil {
			t.Fatal(err)
		}
		before := snapshotTrustVaultFiles(t, vault)
		taskPath := filepath.Join(vault, "work", "tasks", "APP-T-0001.md")
		data, body, err := parseFrontmatterMustRead(taskPath)
		if err != nil {
			t.Fatal(err)
		}
		data["title"] = "Amended while paused"
		data["contract_fingerprint"] = directWaveTaskContractFingerprint(data, body)
		data["state_rev"] = v7StateRev(data, body)
		content, err := serializeDocument(data, body, v7FrontmatterOrder["task"])
		if err != nil {
			t.Fatal(err)
		}
		if err := writeText(taskPath, content); err != nil {
			t.Fatal(err)
		}
		if _, err := directWaveResume(vault, store, "W-0001", "human:test"); err == nil {
			t.Fatal("wave resume accepted stale material")
		}
		after := snapshotTrustVaultFiles(t, vault)
		for path, content := range before {
			if path == taskPath || strings.HasPrefix(path, filepath.Join(vault, "events")+string(filepath.Separator)) {
				continue
			}
			if after[path] != content {
				t.Fatalf("stale resume wrote %s", path)
			}
		}
	})
}

func snapshotTrustVaultFiles(t *testing.T, vault string) map[string]string {
	t.Helper()
	out := map[string]string{}
	if err := filepath.WalkDir(vault, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		out[path] = mustReadIndexTest(t, path)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return out
}
