package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func noteOperationTestVault(t *testing.T, count int) string {
	t.Helper()
	vault := v7DispatchTestVault(t)
	if err := writeText(filepath.Join(v7RepoRoot(vault), ".tusker", "specs", "linked.md"), "---\ntitle: Governing spec\nsubject: linked\nstatus: canonical\nread_when: Testing snapshot reads.\nskip_when: Never.\n---\n\n# Governing spec\n"); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= count; i++ {
		id := fmt.Sprintf("APP-T-%04d", i)
		data := map[string]any{
			"schema": "tusker.task/v7", "kind": "task", "id": id, "project": "app", "epic": "APP",
			"title": "Snapshot task", "status": "ready", "readiness": "ready", "risk": "medium", "priority": "p2",
			"next_owner": "agent", "next_action": "Complete the contract.", "proof_mode": "none", "proof_status": "pending",
			"spec_refs": []string{".tusker/specs/linked.md"}, "owned_paths": []string{"src"},
		}
		body := "## Intent\n\nShow the current task status.\n\n## Acceptance\n\n| ID | Outcome | Proof |\n|---|---|---|\n| A1 | The capsule shows the current task status. | command: go test ./... |\n\n## Verification\n\n| Check | Covers | Result |\n|---|---|---|\n| go test ./... | A1 | pending |\n"
		content, err := serializeDocument(data, body, v7FrontmatterOrder["task"])
		if err != nil {
			t.Fatal(err)
		}
		if err := writeText(filepath.Join(vault, "work", "tasks", id+".md"), content); err != nil {
			t.Fatal(err)
		}
		// Evidence validation resolves its task to inspect artifact policy.
		if err := writeText(filepath.Join(vault, "evidence", id, id+"-E-0001.md"), "---\nschema: tusker.evidence/v1\nkind: evidence\nid: "+id+"-E-0001\ntask: "+id+"\nevidence_kind: test_log\nstatus: proposed\ncovers: [A1]\n---\n"); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { disposeCachedNotes(vault) })
	return vault
}

func TestValidationUsesOneNoteSnapshotAsTasksGrow(t *testing.T) {
	for _, count := range []int{1, 24} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			vault := noteOperationTestVault(t, count)
			disposeCachedNotes(vault)
			var lists, indexes atomic.Int64
			noteCacheListObserver = func() { lists.Add(1) }
			loadV7IndexObserver = func() { indexes.Add(1) }
			t.Cleanup(func() { noteCacheListObserver, loadV7IndexObserver = nil, nil })
			var cmdErr error
			output := captureStdout(t, func() {
				_, cmdErr = validateCmd(Args{"vault": vault, "json": "true", "dispatchable": "true"})
			})
			if cmdErr != nil {
				t.Fatal(cmdErr)
			}
			if lists.Load() != 1 || indexes.Load() != 0 {
				t.Fatalf("%d tasks: note scans=%d index reloads=%d, want one scan and no reloads", count, lists.Load(), indexes.Load())
			}
			if !strings.Contains(output, fmt.Sprintf("APP-T-%04d", count)) {
				t.Fatalf("validation did not inspect the last task: %s", output)
			}
		})
	}
}

func TestShowCapsuleAndJSONReuseOneNoteSnapshot(t *testing.T) {
	vault := noteOperationTestVault(t, 1)
	t.Setenv("TUSKER_STATE_ROOT", t.TempDir())
	if err := writeText(filepath.Join(vault, "attempts", "APP-T-0001", "APP-T-0001-A-0001.md"), "---\nschema: tusker.attempt/v1\nkind: attempt\nid: APP-T-0001-A-0001\ntask: APP-T-0001\nstatus: started\nrunner: test\n---\n"); err != nil {
		t.Fatal(err)
	}
	for _, json := range []bool{false, true} {
		t.Run(fmt.Sprint(json), func(t *testing.T) {
			var lists, indexes atomic.Int64
			noteCacheListObserver = func() { lists.Add(1) }
			loadV7IndexObserver = func() { indexes.Add(1) }
			t.Cleanup(func() { noteCacheListObserver, loadV7IndexObserver = nil, nil })
			output := captureStdout(t, func() {
				if err := showNoteCmd(Args{"vault": vault, "id": "APP-T-0001", "json": fmt.Sprint(json)}); err != nil {
					t.Fatal(err)
				}
			})
			if lists.Load() != 1 || indexes.Load() != 0 {
				t.Fatalf("note scans=%d index reloads=%d, want one scan and no reloads", lists.Load(), indexes.Load())
			}
			if strings.Count(output, "- Attempt: APP-T-0001-A-0001 started runner=test") != 1 {
				t.Fatalf("capsule lost or duplicated attempt: %s", output)
			}
		})
	}
}

func TestValidationSnapshotIsolationAndChangedNoteVisibility(t *testing.T) {
	vault := noteOperationTestVault(t, 1)
	gatePath := filepath.Join(vault, "work", "gates", "G-0001.md")
	gate := "---\nschema: tusker.gate/v1\nkind: gate\nid: G-0001\nstatus: open\nblocking: true\nblocks: [APP-T-0001]\n---\n"
	if err := writeText(gatePath, gate); err != nil {
		t.Fatal(err)
	}
	before, err := loadV7Index(vault)
	if err != nil {
		t.Fatal(err)
	}
	task := before.Tasks["APP-T-0001"]
	task.Data["title"] = "Mutated snapshot"
	task.Data["owned_paths"].([]any)[0] = "unrelated"
	if err := writeText(gatePath, strings.Replace(gate, "status: open", "status: satisfied", 1)); err != nil {
		t.Fatal(err)
	}
	after, err := loadV7Index(vault)
	if err != nil {
		t.Fatal(err)
	}
	freshTask := after.Tasks["APP-T-0001"]
	if stringField(freshTask.Data, "title") != "Snapshot task" || strings.Join(normalizeList(freshTask.Data["owned_paths"]), ",") != "src" {
		t.Fatal("operation snapshot mutated shared cached data")
	}
	var beforeErrors, afterErrors, warnings []Issue
	validateV7TaskReadiness(task, validationContext{VaultPath: vault, V7Index: &before}, task.RelativePath, &beforeErrors, &warnings)
	validateV7TaskReadiness(freshTask, validationContext{VaultPath: vault, V7Index: &after}, freshTask.RelativePath, &afterErrors, &warnings)
	if !issuesContainCode(beforeErrors, "READINESS_STALE") || issuesContainCode(afterErrors, "READINESS_STALE") {
		t.Fatalf("snapshots did not retain old/read new gate state: before=%#v after=%#v", beforeErrors, afterErrors)
	}
}

func TestNoteCacheDisposalIsExactVaultAndReloads(t *testing.T) {
	vaultA, vaultB := filepath.Join(t.TempDir(), "vault"), filepath.Join(t.TempDir(), "vault")
	for _, vault := range []string{vaultA, vaultB} {
		if err := writeText(filepath.Join(vault, "note.md"), "---\nid: same-id\n---\nBody\n"); err != nil {
			t.Fatal(err)
		}
		if _, err := listAllNotes(vault); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { disposeCachedNotes(vault) })
	}
	cacheB, err := noteCacheForVault(vaultB)
	if err != nil {
		t.Fatal(err)
	}
	disposeCachedNotes(filepath.Join(vaultA, "."))
	if writeNotifyNoteCacheExists(vaultA) || !writeNotifyNoteCacheExists(vaultB) {
		t.Fatal("cache disposal crossed vault boundary or retained removed vault")
	}
	stillB, err := noteCacheForVault(vaultB)
	if err != nil || stillB != cacheB {
		t.Fatal("unrelated vault cache was replaced")
	}
	var reads atomic.Int64
	noteCacheReadObserver = func() { reads.Add(1) }
	t.Cleanup(func() { noteCacheReadObserver = nil })
	if _, err := listAllNotes(vaultA); err != nil {
		t.Fatal(err)
	}
	if reads.Load() != 1 {
		t.Fatalf("removed vault cache did not cold reload: reads=%d", reads.Load())
	}
}

func TestTargetedNoteCachePreservesBodyAndCoarseMtimeEdits(t *testing.T) {
	vault := t.TempDir()
	t.Cleanup(func() { disposeCachedNotes(vault) })
	path := filepath.Join(vault, "work", "tasks", "APP-T-0001.md")
	raw := "---\nschema: tusker.task/v7\nkind: task\nid: APP-T-0001\ntitle: Before\n---\nFull body\n"
	if err := writeText(path, raw); err != nil {
		t.Fatal(err)
	}
	mtime := time.Now().Truncate(time.Second)
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	if _, err := listOperationalNotesFrontmatter(vault); err != nil {
		t.Fatal(err)
	}
	first, err := loadOperationalNotesByPath(vault, []string{"work/tasks/APP-T-0001.md"})
	if err != nil || len(first) != 1 || first[0].Body != "Full body\n" {
		t.Fatalf("targeted load did not promote frontmatter cache: %#v err=%v", first, err)
	}
	second, err := loadOperationalNotesByPath(vault, []string{path})
	if err != nil || len(second) != 1 || second[0].Body != first[0].Body {
		t.Fatalf("warm targeted load dropped body: %#v err=%v", second, err)
	}
	// Same size and coarse timestamp: the existing freshness hash guard must
	// detect edits even though the stat version is unchanged.
	if err := os.WriteFile(path, []byte(strings.Replace(raw, "Before", "After!", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	changed, err := loadOperationalNotesByPath(vault, []string{path})
	if err != nil || len(changed) != 1 || stringField(changed[0].Data, "title") != "After!" {
		t.Fatalf("targeted load missed same-second edit: %#v err=%v", changed, err)
	}
}
