package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestV7TargetedReconcileRepairsOnlySelectedTask(t *testing.T) {
	vault := targetedReconcileFixture(t)
	firstPath := filepath.Join(vault, "work", "tasks", "APP-T-0001.md")
	secondPath := filepath.Join(vault, "work", "tasks", "APP-T-0002.md")
	firstBeforeRev := makeTargetedReconcileTaskStale(t, firstPath, "\n## CRP lock detail\n\nPinned SDK and bootstrap semantics remain unchanged.\n")
	secondBeforeRev := makeTargetedReconcileTaskStale(t, secondPath, "\n## Other stale detail\n\nThis object must remain untouched.\n")
	beforeDryRun := snapshotTargetedReconcileVault(t, vault)

	output := captureTargetedReconcileStdout(t, func() {
		if err := reconcileV7Cmd(Args{"vault": vault, "id": "APP-T-0001", "dry-run": "true", "json": "true"}); err != nil {
			t.Fatal(err)
		}
	})
	var report v7TargetedReconcileReport
	if err := json.Unmarshal([]byte(output), &report); err != nil {
		t.Fatalf("decode dry-run report: %v\n%s", err, output)
	}
	if report.Schema != "tusker.targeted-reconcile/v1" || !report.OK || !report.DryRun || !report.Changed {
		t.Fatalf("unexpected dry-run report: %#v", report)
	}
	if report.ID != "APP-T-0001" || report.Kind != "task" || report.Path != "work/tasks/APP-T-0001.md" {
		t.Fatalf("dry-run targeted the wrong object: %#v", report)
	}
	if report.BeforeStateRev != firstBeforeRev || report.AfterStateRev == "" || report.AfterStateRev == firstBeforeRev {
		t.Fatalf("dry-run revision facts are incomplete: %#v", report)
	}
	firstDryRun := report
	nextSecond := time.Now().Truncate(time.Second).Add(time.Second)
	time.Sleep(time.Until(nextSecond) + 20*time.Millisecond)
	output = captureTargetedReconcileStdout(t, func() {
		if err := reconcileV7Cmd(Args{"vault": vault, "id": "APP-T-0001", "dry-run": "true", "json": "true"}); err != nil {
			t.Fatal(err)
		}
	})
	var repeatedDryRun v7TargetedReconcileReport
	if err := json.Unmarshal([]byte(output), &repeatedDryRun); err != nil {
		t.Fatalf("decode repeated dry-run report: %v\n%s", err, output)
	}
	if repeatedDryRun.BeforeStateRev != firstDryRun.BeforeStateRev || repeatedDryRun.AfterStateRev != firstDryRun.AfterStateRev {
		t.Fatalf("identical task content produced clock-dependent dry-run revisions: first=%#v repeated=%#v", firstDryRun, repeatedDryRun)
	}
	assertTargetedReconcileSnapshotsEqual(t, beforeDryRun, snapshotTargetedReconcileVault(t, vault))

	beforeRepair := snapshotTargetedReconcileVault(t, vault)
	beforeData, _, err := parseFrontmatterMustRead(firstPath)
	if err != nil {
		t.Fatal(err)
	}
	beforeUpdatedAt := stringField(beforeData, "updated_at")
	beforeUpdatedBy := stringField(beforeData, "updated_by")
	output = captureTargetedReconcileStdout(t, func() {
		if err := reconcileV7Cmd(Args{"vault": vault, "id": "APP-T-0001", "json": "true"}); err != nil {
			t.Fatal(err)
		}
	})
	if err := json.Unmarshal([]byte(output), &report); err != nil {
		t.Fatalf("decode repair report: %v\n%s", err, output)
	}
	if !report.OK || report.DryRun || !report.Changed || report.ID != "APP-T-0001" {
		t.Fatalf("unexpected repair report: %#v", report)
	}
	if report.BeforeStateRev != firstDryRun.BeforeStateRev || report.AfterStateRev != firstDryRun.AfterStateRev {
		t.Fatalf("apply did not commit the stable dry-run revision: dry_run=%#v apply=%#v", firstDryRun, report)
	}

	firstData, firstBody, err := parseFrontmatterMustRead(firstPath)
	if err != nil {
		t.Fatal(err)
	}
	if stringField(firstData, "state_rev") != v7StateRev(firstData, firstBody) {
		t.Fatal("selected task state_rev was not repaired from its current content")
	}
	if stringField(firstData, "state_rev") != firstDryRun.AfterStateRev {
		t.Fatalf("committed revision=%s, want dry-run revision=%s", stringField(firstData, "state_rev"), firstDryRun.AfterStateRev)
	}
	if stringField(firstData, "updated_at") != beforeUpdatedAt || stringField(firstData, "updated_by") != beforeUpdatedBy {
		t.Fatalf("targeted state_rev repair changed task semantics: updated_at %q -> %q, updated_by %q -> %q", beforeUpdatedAt, stringField(firstData, "updated_at"), beforeUpdatedBy, stringField(firstData, "updated_by"))
	}
	if !strings.Contains(firstBody, "Pinned SDK and bootstrap semantics remain unchanged.") {
		t.Fatal("selected task body was not preserved")
	}
	secondData, secondBody, err := parseFrontmatterMustRead(secondPath)
	if err != nil {
		t.Fatal(err)
	}
	if stringField(secondData, "state_rev") != secondBeforeRev || v7StateRevMatches(secondData, secondBody, secondBeforeRev) {
		t.Fatal("unselected stale task was modified or unexpectedly certified")
	}

	afterRepair := snapshotTargetedReconcileVault(t, vault)
	changed, added, removed := targetedReconcileSnapshotDiff(beforeRepair, afterRepair)
	if len(changed) != 1 || changed[0] != "work/tasks/APP-T-0001.md" || len(removed) != 0 || len(added) != 1 || !strings.HasPrefix(added[0], "events/") {
		t.Fatalf("targeted reconcile mutated unrelated files: changed=%v added=%v removed=%v", changed, added, removed)
	}
	rawEvent := afterRepair[added[0]]
	var event map[string]any
	if err := json.Unmarshal(rawEvent, &event); err != nil {
		t.Fatal(err)
	}
	if stringField(event, "object") != "APP-T-0001" || stringField(event, "event_kind") != "updated" {
		t.Fatalf("wrong targeted repair event: %#v", event)
	}
	payload, _ := event["payload"].(map[string]any)
	if stringField(payload, "source") != "state_rev_repair" || stringField(payload, "previous_state_rev") != firstBeforeRev || stringField(payload, "state_rev") != stringField(firstData, "state_rev") {
		t.Fatalf("targeted repair event omitted revision audit facts: %#v", event)
	}

	if code, err := validateCmd(Args{"vault": vault, "json": "true"}); err != nil || code != 0 {
		t.Fatalf("repaired CRP-like vault failed import-grade validation: code=%d err=%v", code, err)
	}
}

func TestV7TargetedReconcileUsesCurrentBodyDespiteStaleCache(t *testing.T) {
	vault := targetedReconcileFixture(t)
	taskPath := filepath.Join(vault, "work", "tasks", "APP-T-0001.md")
	stamp, err := os.Stat(taskPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := listAllNotes(vault); err != nil {
		t.Fatal(err)
	}
	data, body, err := parseFrontmatterMustRead(taskPath)
	if err != nil {
		t.Fatal(err)
	}
	changedBody := strings.Replace(body, "First CRP task", "Fresh CRP task", 1)
	if changedBody == body || len(changedBody) != len(body) {
		t.Fatal("same-size cached-body fixture is invalid")
	}
	raw, err := serializeDocument(data, changedBody, v7FrontmatterOrder["task"])
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(taskPath, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(taskPath, stamp.ModTime(), stamp.ModTime()); err != nil {
		t.Fatal(err)
	}

	if err := reconcileV7Cmd(Args{"vault": vault, "id": "APP-T-0001", "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	afterData, afterBody, err := parseFrontmatterMustRead(taskPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(afterBody, "Fresh CRP task") {
		t.Fatal("targeted reconcile overwrote the current body with cached content")
	}
	if stringField(afterData, "state_rev") != v7StateRev(afterData, afterBody) {
		t.Fatal("targeted reconcile did not certify the current body")
	}
}

func TestV7TargetedReconcileRefusesTerminalRewind(t *testing.T) {
	repo := t.TempDir()
	runGitDir(t, repo, "init", "-b", "main")
	runGitDir(t, repo, "config", "user.email", "test@example.com")
	runGitDir(t, repo, "config", "user.name", "Test User")
	if err := os.WriteFile(filepath.Join(repo, "tusker.yaml"), []byte("schema: tusker.config/v1\nproject_id: app\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	vault := filepath.Join(repo, ".tusker")
	if err := bootstrap(Args{"vault": vault, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	if err := newV7Epic(Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App", "summary": "Terminal guard.", "v7": "true"}); err != nil {
		t.Fatal(err)
	}
	if err := newV7Task(Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Guarded task", "risk": "low", "priority": "p0", "v7": "true"}); err != nil {
		t.Fatal(err)
	}
	taskPath := filepath.Join(vault, "work", "tasks", "APP-T-0001.md")
	staleData, staleBody, err := parseFrontmatterMustRead(taskPath)
	if err != nil {
		t.Fatal(err)
	}
	staleData["status"] = "review"
	staleData["readiness"] = "waiting_on_review"
	staleData["state_rev"] = v7StateRev(staleData, staleBody)

	setWaveTaskState(t, vault, "APP-T-0001", "done", "done", "2026-07-29T01:00:00Z")
	runGitDir(t, repo, "add", ".")
	runGitDir(t, repo, "commit", "-m", "close task")

	staleData["next_action"] = "Stale review copy."
	staleRaw, err := serializeDocument(staleData, staleBody, v7FrontmatterOrder["task"])
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(taskPath, []byte(staleRaw), 0o644); err != nil {
		t.Fatal(err)
	}
	before := snapshotTargetedReconcileVault(t, vault)
	for _, args := range []Args{
		{"vault": vault, "id": "APP-T-0001", "dry-run": "true", "json": "true"},
		{"vault": vault, "id": "APP-T-0001", "local": "true", "json": "true"},
	} {
		err := reconcileV7Cmd(args)
		if err == nil || !strings.Contains(err.Error(), "terminal task state rewind refused") {
			t.Fatalf("expected terminal rewind refusal, got %v", err)
		}
		assertTargetedReconcileSnapshotsEqual(t, before, snapshotTargetedReconcileVault(t, vault))
	}
}

func TestV7TargetedReconcileRefusesInvalidAndMissingIDs(t *testing.T) {
	vault := targetedReconcileFixture(t)
	for _, tc := range []struct {
		id   string
		code string
	}{
		{id: "not-a-task", code: errorIDScheme},
		{id: "APP-T-9999", code: errorNotFound},
	} {
		err := reconcileV7Cmd(Args{"vault": vault, "id": tc.id, "dry-run": "true", "json": "true"})
		if err == nil {
			t.Fatalf("expected %s to be refused", tc.id)
		}
		if issue := errorToIssue(err); issue.Code != tc.code {
			t.Fatalf("%s error code=%s, want %s: %v", tc.id, issue.Code, tc.code, err)
		}
	}
}

func TestV7ReconcileDryRunEnumeratesInvalidStateRevsWithoutWrites(t *testing.T) {
	vault := targetedReconcileFixture(t)
	epicPath := filepath.Join(vault, "work", "epics", "APP.md")
	terminalTaskPath := filepath.Join(vault, "work", "tasks", "APP-T-0001.md")

	epicBeforeRev := makeTargetedReconcileObjectStale(t, epicPath, "epic", "\n## Stale epic detail\n\nEnumerate this epic.\n")
	taskData, taskBody, err := parseFrontmatterMustRead(terminalTaskPath)
	if err != nil {
		t.Fatal(err)
	}
	taskData["status"] = "done"
	taskData["readiness"] = "done"
	taskData["state_rev"] = v7StateRev(taskData, taskBody)
	raw, err := serializeDocument(taskData, taskBody+"\n## Stale terminal detail\n\nReport without repair.\n", v7FrontmatterOrder["task"])
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(terminalTaskPath, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	terminalBeforeRev := stringField(taskData, "state_rev")
	before := snapshotTargetedReconcileVault(t, vault)

	scan := func() v7StateRevScanReport {
		t.Helper()
		output := captureTargetedReconcileStdout(t, func() {
			if err := reconcileV7Cmd(Args{"vault": vault, "dry-run": "true", "json": "true"}); err != nil {
				t.Fatal(err)
			}
		})
		var report v7StateRevScanReport
		if err := json.Unmarshal([]byte(output), &report); err != nil {
			t.Fatalf("decode state_rev scan: %v\n%s", err, output)
		}
		return report
	}

	first := scan()
	if first.Schema != "tusker.state-rev-scan/v1" || !first.OK || !first.DryRun || first.Count != len(first.Rows) || first.Count < 2 {
		t.Fatalf("unexpected state_rev scan report: %#v", first)
	}
	if !sort.SliceIsSorted(first.Rows, func(i, j int) bool {
		if first.Rows[i].ID != first.Rows[j].ID {
			return first.Rows[i].ID < first.Rows[j].ID
		}
		if first.Rows[i].Type != first.Rows[j].Type {
			return first.Rows[i].Type < first.Rows[j].Type
		}
		return first.Rows[i].Path < first.Rows[j].Path
	}) {
		t.Fatalf("state_rev scan rows are not stably sorted: %#v", first.Rows)
	}
	rowsByID := map[string]v7StateRevScanRow{}
	for _, row := range first.Rows {
		rowsByID[row.ID] = row
	}
	epicRow := rowsByID["APP"]
	if epicRow.Type != "epic" || epicRow.Path != "work/epics/APP.md" ||
		epicRow.BeforeStateRev != epicBeforeRev || epicRow.AfterStateRev == "" || epicRow.AfterStateRev == epicBeforeRev {
		t.Fatalf("stale epic row is incomplete: %#v", first.Rows)
	}
	terminalRow := rowsByID["APP-T-0001"]
	if terminalRow.Type != "task" || terminalRow.Path != "work/tasks/APP-T-0001.md" ||
		terminalRow.BeforeStateRev != terminalBeforeRev || terminalRow.AfterStateRev == "" || terminalRow.AfterStateRev == terminalBeforeRev {
		t.Fatalf("stale terminal task row is incomplete: %#v", first.Rows)
	}
	if _, ok := rowsByID["APP-T-0002"]; ok {
		t.Fatalf("valid task was included in invalid state_rev scan: %#v", first.Rows)
	}
	assertTargetedReconcileSnapshotsEqual(t, before, snapshotTargetedReconcileVault(t, vault))

	nextSecond := time.Now().Truncate(time.Second).Add(time.Second)
	time.Sleep(time.Until(nextSecond) + 20*time.Millisecond)
	second := scan()
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("identical vault produced clock-dependent scan: first=%#v second=%#v", first, second)
	}
	assertTargetedReconcileSnapshotsEqual(t, before, snapshotTargetedReconcileVault(t, vault))
}

func TestV7TargetedReconcileRefusesDuplicateTaskID(t *testing.T) {
	vault := targetedReconcileFixture(t)
	originalPath := filepath.Join(vault, "work", "tasks", "APP-T-0001.md")
	duplicatePath := filepath.Join(vault, "work", "archive", "APP-T-0001-duplicate.md")
	raw, err := os.ReadFile(originalPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(duplicatePath, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	makeTargetedReconcileTaskStale(t, originalPath, "\n## Stale duplicate fixture\n\nNeither duplicate may be selected.\n")
	before := snapshotTargetedReconcileVault(t, vault)

	err = reconcileV7Cmd(Args{"vault": vault, "id": "APP-T-0001", "dry-run": "true", "json": "true"})
	if err == nil {
		t.Fatal("expected duplicate task ID to be refused")
	}
	issue := errorToIssue(err)
	if issue.Code != errorIDCollision {
		t.Fatalf("duplicate task ID error code=%s, want %s: %v", issue.Code, errorIDCollision, err)
	}
	context, ok := issue.Context.(map[string]any)
	if !ok || toString(context["id"]) != "APP-T-0001" {
		t.Fatalf("duplicate refusal omitted target identity: %#v", issue)
	}
	paths, ok := context["paths"].([]string)
	if !ok || len(paths) != 2 || paths[0] != "work/archive/APP-T-0001-duplicate.md" || paths[1] != "work/tasks/APP-T-0001.md" {
		t.Fatalf("duplicate refusal paths are not stable: %#v", issue.Context)
	}
	assertTargetedReconcileSnapshotsEqual(t, before, snapshotTargetedReconcileVault(t, vault))
}

func targetedReconcileFixture(t *testing.T) string {
	t.Helper()
	vault := filepath.Join(t.TempDir(), "vault")
	if err := bootstrap(Args{"vault": vault, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	if err := newV7Epic(Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App", "summary": "Targeted reconcile.", "v7": "true"}); err != nil {
		t.Fatal(err)
	}
	for _, title := range []string{"First CRP task", "Second CRP task"} {
		if err := newV7Task(Args{"vault": vault, "quiet": "true", "epic": "APP", "title": title, "risk": "low", "priority": "p0", "v7": "true"}); err != nil {
			t.Fatal(err)
		}
	}
	return vault
}

func makeTargetedReconcileTaskStale(t *testing.T, path, suffix string) string {
	t.Helper()
	return makeTargetedReconcileObjectStale(t, path, "task", suffix)
}

func makeTargetedReconcileObjectStale(t *testing.T, path, kind, suffix string) string {
	t.Helper()
	data, body, err := parseFrontmatterMustRead(path)
	if err != nil {
		t.Fatal(err)
	}
	beforeRev := stringField(data, "state_rev")
	raw, err := serializeDocument(data, body+suffix, v7FrontmatterOrder[kind])
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	return beforeRev
}

func snapshotTargetedReconcileVault(t *testing.T, vault string) map[string][]byte {
	t.Helper()
	result := map[string][]byte{}
	err := filepath.WalkDir(vault, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(vault, path)
		if err != nil {
			return err
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		result[filepath.ToSlash(rel)] = raw
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func targetedReconcileSnapshotDiff(before, after map[string][]byte) (changed, added, removed []string) {
	for path, beforeRaw := range before {
		afterRaw, ok := after[path]
		if !ok {
			removed = append(removed, path)
		} else if string(beforeRaw) != string(afterRaw) {
			changed = append(changed, path)
		}
	}
	for path := range after {
		if _, ok := before[path]; !ok {
			added = append(added, path)
		}
	}
	sort.Strings(changed)
	sort.Strings(added)
	sort.Strings(removed)
	return changed, added, removed
}

func assertTargetedReconcileSnapshotsEqual(t *testing.T, before, after map[string][]byte) {
	t.Helper()
	changed, added, removed := targetedReconcileSnapshotDiff(before, after)
	if len(changed) != 0 || len(added) != 0 || len(removed) != 0 {
		t.Fatalf("unexpected filesystem mutation: changed=%v added=%v removed=%v", changed, added, removed)
	}
}

func captureTargetedReconcileStdout(t *testing.T, fn func()) string {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	previous := os.Stdout
	os.Stdout = writer
	defer func() { os.Stdout = previous }()
	fn()
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(raw))
}
