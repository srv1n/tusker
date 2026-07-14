package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestWaveCreateMembership(t *testing.T) {
	vault := newWaveTestVault(t, 3)

	mustWave(t, Args{"vault": vault, "quiet": "true", "_pos0": "Launch batch", "_pos1": "APP-T-0001", "_pos2": "APP-T-0002"}, waveV7CreateCmd)

	waveData, _, err := parseFrontmatterMustRead(filepath.Join(vault, "work", "waves", "W-0001.md"))
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "wave", stringField(waveData, "kind"), "kind")
	assertEqual(t, []string{"APP-T-0001", "APP-T-0002"}, normalizeList(waveData["members"]), "members")

	task1, _, err := parseFrontmatterMustRead(filepath.Join(vault, "work", "tasks", "APP-T-0001.md"))
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "W-0001", stringField(task1, "wave"), "task wave back-pointer")

	err = waveV7CreateCmd(Args{"vault": vault, "quiet": "true", "_pos0": "Duplicate", "_pos1": "APP-T-0003", "_pos2": "APP-T-0003"})
	if err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("expected duplicate membership rejection, got %v", err)
	}
	err = waveV7CreateCmd(Args{"vault": vault, "quiet": "true", "_pos0": "Unknown", "_pos1": "APP-T-9999"})
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected unknown task rejection, got %v", err)
	}
	err = waveV7CreateCmd(Args{"vault": vault, "quiet": "true", "_pos0": "Conflict", "_pos1": "APP-T-0001"})
	if err == nil || !strings.Contains(err.Error(), "already belongs") {
		t.Fatalf("expected open-wave conflict rejection, got %v", err)
	}

	mustWave(t, Args{"vault": vault, "quiet": "true", "_pos0": "W-0001", "_pos1": "APP-T-0002"}, waveV7RemoveCmd)
	task2, _, err := parseFrontmatterMustRead(filepath.Join(vault, "work", "tasks", "APP-T-0002.md"))
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "", stringField(task2, "wave"), "removed task wave back-pointer")

	mustWave(t, Args{"vault": vault, "quiet": "true", "_pos0": "W-0001", "_pos1": "APP-T-0003"}, waveV7AddCmd)
	task3, _, err := parseFrontmatterMustRead(filepath.Join(vault, "work", "tasks", "APP-T-0003.md"))
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "W-0001", stringField(task3, "wave"), "added task wave back-pointer")
}

func TestWaveDerivedState(t *testing.T) {
	vault := newWaveTestVault(t, 3)
	mustWave(t, Args{"vault": vault, "quiet": "true", "_pos0": "Landing batch", "_pos1": "APP-T-0001", "_pos2": "APP-T-0002"}, waveV7CreateCmd)

	setWaveTaskState(t, vault, "APP-T-0001", "done", "done", "2026-07-07T01:00:00Z")
	mustWave(t, Args{"vault": vault, "quiet": "true"}, reconcileV7Cmd)
	waveData, _, err := parseFrontmatterMustRead(filepath.Join(vault, "work", "waves", "W-0001.md"))
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "open", stringField(waveData, "status"), "partially done wave status")
	assertEqual(t, "", stringField(waveData, "landed_at"), "partially done landed_at")

	setWaveTaskState(t, vault, "APP-T-0002", "done", "done", "2026-07-07T02:00:00Z")
	mustWave(t, Args{"vault": vault, "quiet": "true"}, reconcileV7Cmd)
	waveData, waveBody, err := parseFrontmatterMustRead(filepath.Join(vault, "work", "waves", "W-0001.md"))
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "landed", stringField(waveData, "status"), "landed wave status")
	assertEqual(t, "2026-07-07T02:00:00Z", stringField(waveData, "landed_at"), "landed timestamp")

	baseRev := stringField(waveData, "state_rev")
	waveData["status"] = "open"
	waveData["landed_at"] = "2000-01-01T00:00:00Z"
	if _, err := saveV7DocumentCAS(filepath.Join(vault, "work", "waves", "W-0001.md"), waveData, waveBody, v7FrontmatterOrder["wave"], baseRev); err != nil {
		t.Fatal(err)
	}
	mustWave(t, Args{"vault": vault, "quiet": "true"}, reconcileV7Cmd)
	waveData, _, err = parseFrontmatterMustRead(filepath.Join(vault, "work", "waves", "W-0001.md"))
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "landed", stringField(waveData, "status"), "reconciled wave status")
	assertEqual(t, "2026-07-07T02:00:00Z", stringField(waveData, "landed_at"), "reconciled landed timestamp")

	mustWave(t, Args{"vault": vault, "quiet": "true", "_pos0": "W-0001", "_pos1": "APP-T-0003"}, waveV7AddCmd)
	waveData, _, err = parseFrontmatterMustRead(filepath.Join(vault, "work", "waves", "W-0001.md"))
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "open", stringField(waveData, "status"), "reopened wave status")
	assertEqual(t, "", stringField(waveData, "landed_at"), "reopened landed_at")
}

func TestWaveShowAndFilters(t *testing.T) {
	vault := newWaveTestVault(t, 4)
	mustWave(t, Args{"vault": vault, "quiet": "true", "_pos0": "Review batch", "_pos1": "APP-T-0001", "_pos2": "APP-T-0002", "_pos3": "APP-T-0003"}, waveV7CreateCmd)
	setWaveTaskState(t, vault, "APP-T-0001", "done", "done", "2026-07-07T01:00:00Z")
	setWaveTaskState(t, vault, "APP-T-0002", "review", "waiting_on_review", "")
	setWaveTaskState(t, vault, "APP-T-0003", "ready", "blocked_by_dependency", "")
	mustWave(t, Args{"vault": vault, "quiet": "true"}, reconcileV7Cmd)

	show := captureStdout(t, func() {
		if err := waveV7ShowCmd(Args{"vault": vault, "_pos0": "W-0001"}); err != nil {
			t.Fatal(err)
		}
	})
	for _, want := range []string{"## done", "APP-T-0001", "## review", "APP-T-0002", "## blocked", "APP-T-0003", "proof:"} {
		if !strings.Contains(show, want) {
			t.Fatalf("wave show missing %q:\n%s", want, show)
		}
	}

	listOutput := captureStdout(t, func() {
		if err := listCmd(Args{"vault": vault, "wave": "W-0001", "format": "ids"}); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(listOutput, "APP-T-0001") || !strings.Contains(listOutput, "APP-T-0002") || !strings.Contains(listOutput, "APP-T-0003") {
		t.Fatalf("wave list filter missing members:\n%s", listOutput)
	}
	if strings.Contains(listOutput, "APP-T-0004") {
		t.Fatalf("wave list filter included non-member:\n%s", listOutput)
	}

	mustWave(t, Args{"vault": vault, "quiet": "true"}, dashboardV7Cmd)
	dashboard := mustReadIndexTest(t, filepath.Join(vault, "dashboards", "review-queue.md"))
	if !strings.Contains(dashboard, "| Task | Wave | Risk | Next action |") || !strings.Contains(dashboard, "W-0001") {
		t.Fatalf("review dashboard missing wave filter surface:\n%s", dashboard)
	}
}

func TestServeWaves(t *testing.T) {
	server := newServeEmptyNeedsFixture(t)
	var empty []serveWaveSummary
	serveDecode(t, server, "/api/waves", &empty)
	if len(empty) != 0 {
		t.Fatalf("expected empty waves array, got %#v", empty)
	}

	writeServeWave(t, server.vaultPath, "W-0001", "Morning batch", []string{"APP-T-0001"})
	setServeTaskWave(t, server.vaultPath, "APP-T-0001", "W-0001")
	server.invalidateSnapshotCaches()
	server.warmSnapshot("")

	var waves []serveWaveSummary
	serveDecode(t, server, "/api/waves", &waves)
	if len(waves) != 1 {
		t.Fatalf("expected one wave, got %#v", waves)
	}
	assertEqual(t, "W-0001", waves[0].ID, "wave id")
	assertEqual(t, []string{"APP-T-0001"}, waves[0].MemberIDs, "wave members")

	var detail serveWaveSummary
	serveDecode(t, server, "/api/waves/W-0001", &detail)
	assertEqual(t, "Morning batch", detail.Title, "wave detail title")
	assertEqual(t, waveBriefSchema, detail.Brief.Schema, "serve shared brief schema")
	assertEqual(t, waveBriefSectionOrder, detail.Brief.SectionOrder, "serve brief section order")
	if len(detail.Members) != 1 || detail.Members[0].ID != "APP-T-0001" {
		t.Fatalf("expected wave detail member, got %#v", detail.Members)
	}

	var task serveTaskDetail
	serveDecode(t, server, "/api/tasks/APP-T-0001", &task)
	assertEqual(t, "W-0001", task.WaveID, "task wave id")
	assertEqual(t, "Morning batch", task.WaveTitle, "task wave title")
}

func newWaveTestVault(t *testing.T, tasks int) string {
	t.Helper()
	vault := filepath.Join(t.TempDir(), "vault")
	mustWave(t, Args{"vault": vault, "quiet": "true"}, bootstrap)
	mustWave(t, Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App", "summary": "Wave tests.", "v7": "true"}, newV7Epic)
	for i := 1; i <= tasks; i++ {
		mustWave(t, Args{
			"vault":    vault,
			"quiet":    "true",
			"epic":     "APP",
			"title":    "Task " + padNumber(i),
			"risk":     "low",
			"priority": "p2",
			"v7":       "true",
		}, newV7Task)
	}
	return vault
}

func mustWave(t *testing.T, args Args, fn func(Args) error) {
	t.Helper()
	if err := fn(args); err != nil {
		t.Fatal(err)
	}
}

func setWaveTaskState(t *testing.T, vault, taskID, status, readiness, closedAt string) {
	t.Helper()
	path := filepath.Join(vault, "work", "tasks", taskID+".md")
	data, body, err := parseFrontmatterMustRead(path)
	if err != nil {
		t.Fatal(err)
	}
	data["status"] = status
	data["readiness"] = readiness
	data["updated_at"] = firstNonEmpty(closedAt, "2026-07-07T00:00:00Z")
	data["updated_by"] = "test"
	switch status {
	case "done":
		data["proof_status"] = "satisfied"
		data["next_owner"] = "none"
		data["next_source"] = "status"
		data["next_ref"] = ""
		data["next_action"] = ""
		data["accepted_by"] = "reviewer:agent"
		data["accepted_at"] = closedAt
		data["closed_at"] = closedAt
	case "review":
		data["next_owner"] = "reviewer"
		data["next_source"] = "review_policy"
		data["next_ref"] = ""
		data["next_action"] = "Review evidence and close or return to rework."
	case "ready":
		data["next_owner"] = "agent"
		data["next_source"] = "task"
		data["next_ref"] = taskID
		data["next_action"] = "Execute."
	}
	data["state_rev"] = v7StateRev(data, body)
	content, err := serializeDocument(data, body, v7FrontmatterOrder["task"])
	if err != nil {
		t.Fatal(err)
	}
	if err := writeText(path, content); err != nil {
		t.Fatal(err)
	}
}

func writeServeWave(t *testing.T, vault, id, title string, members []string) {
	t.Helper()
	data := map[string]any{
		"schema":     "tusker.wave/v7",
		"kind":       "wave",
		"id":         id,
		"project":    "app",
		"title":      title,
		"status":     "open",
		"members":    members,
		"created_at": "2026-07-06T06:00:00Z",
		"created_by": "agent:test",
		"updated_at": "2026-07-06T06:00:00Z",
		"updated_by": "agent:test",
	}
	body := "# " + id + " - " + title + "\n"
	data["state_rev"] = v7StateRev(data, body)
	content, err := serializeDocument(data, body, v7FrontmatterOrder["wave"])
	if err != nil {
		t.Fatal(err)
	}
	if err := writeText(filepath.Join(vault, "work", "waves", id+".md"), content); err != nil {
		t.Fatal(err)
	}
}

func setServeTaskWave(t *testing.T, vault, taskID, waveID string) {
	t.Helper()
	path := filepath.Join(vault, "work", "tasks", taskID+".md")
	data, body, err := parseFrontmatterMustRead(path)
	if err != nil {
		t.Fatal(err)
	}
	data["wave"] = waveID
	data["state_rev"] = v7StateRev(data, body)
	content, err := serializeDocument(data, body, v7FrontmatterOrder["task"])
	if err != nil {
		t.Fatal(err)
	}
	if err := writeText(path, content); err != nil {
		t.Fatal(err)
	}
}
