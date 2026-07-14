package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestEscalationCreateRouting(t *testing.T) {
	vault := pickupV7TestVault(t)
	writeDigestTask(t, vault, "APP-T-0001", "Escalating task", "ready", "medium", "pending", "")
	writeEscalationNotificationsConfig(t, vault, true)

	var notifications []string
	oldNotify := notifyEscalationUser
	notifyEscalationUser = func(title, message string) error {
		notifications = append(notifications, title+"|"+message)
		return nil
	}
	t.Cleanup(func() { notifyEscalationUser = oldNotify })

	if err := escalationV7CreateCmd(Args{"vault": vault, "quiet": "true", "local": "true", "severity": "P2", "task": "APP-T-0001", "_pos0": "record-only escalation"}); err != nil {
		t.Fatal(err)
	}
	assertEqual(t, 0, len(notifications), "P2 notification count")

	if err := escalationV7CreateCmd(Args{"vault": vault, "quiet": "true", "local": "true", "_pos0": "-s", "_pos1": "P1", "_pos2": "notify escalation", "task": "APP-T-0001"}); err != nil {
		t.Fatal(err)
	}
	assertEqual(t, 1, len(notifications), "P1 notification count")

	if err := writeText(filepath.Join(filepath.Dir(vault), "tusker.yaml"), "escalation:\n  notifications_enabled: false\n"); err != nil {
		t.Fatal(err)
	}
	if err := escalationV7CreateCmd(Args{"vault": vault, "quiet": "true", "local": "true", "severity": "P1", "task": "APP-T-0001", "_pos0": "disabled notification escalation"}); err != nil {
		t.Fatal(err)
	}
	assertEqual(t, 1, len(notifications), "disabled notification count")

	writeEscalationNotificationsConfig(t, vault, true)
	if err := escalationV7CreateCmd(Args{"vault": vault, "quiet": "true", "local": "true", "severity": "P0", "task": "APP-T-0001", "reason": "security_concern", "_pos0": "persistent banner escalation"}); err != nil {
		t.Fatal(err)
	}
	if !hasOpenP0Escalation(vault) {
		t.Fatal("expected open P0 escalation to set serve banner flag")
	}
	assertEqual(t, 2, len(notifications), "P0 notification count")

	project := RegisteredProject{ProjectID: v7ProjectID(vault), VaultRoot: vault}
	recordDaemonEscalationForRun(project, RunStatus{ProjectID: project.ProjectID, RecordID: "APP-T-0001", ItemID: "APP-T-0001"}, "park", "daemon parked the run")
	idx, err := loadV7Index(vault)
	if err != nil {
		t.Fatal(err)
	}
	foundDaemon := false
	for _, note := range idx.Escalations {
		if stringField(note.Data, "source") == "daemon" && stringField(note.Data, "reason") == "park" {
			foundDaemon = true
		}
	}
	if !foundDaemon {
		t.Fatal("expected daemon-created escalation")
	}

	if err := escalationV7CreateCmd(Args{"vault": vault, "quiet": "true", "local": "true", "severity": "P2", "task": "APP-T-0001", "reason": "confused", "_pos0": "bad reason"}); err == nil {
		t.Fatal("expected invalid runner escalation reason to fail")
	}
	if !strings.Contains(defaultWorkflowMarkdown(), "system_error|security_concern|unresolvable_conflict|stuck_loop") {
		t.Fatal("runner prompt must state eligible escalation reasons")
	}
}

func TestEscalationStaleBumpAck(t *testing.T) {
	vault := pickupV7TestVault(t)
	writeDigestTask(t, vault, "APP-T-0001", "Stale task", "ready", "medium", "pending", "")
	writeEscalationNotificationsConfig(t, vault, true)
	old := time.Date(2026, 7, 7, 0, 0, 0, 0, time.UTC)

	var notifications int
	oldNotify := notifyEscalationUser
	notifyEscalationUser = func(_, _ string) error {
		notifications++
		return nil
	}
	t.Cleanup(func() { notifyEscalationUser = oldNotify })

	first, _, err := createV7Escalation(vault, escalationCreateRequest{
		Severity:     "P2",
		TaskID:       "APP-T-0001",
		Description:  "old unacked escalation",
		Source:       "runner",
		Reason:       "system_error",
		Actor:        "agent:test",
		ValidateTask: true,
		Now:          old,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := applyStaleEscalationBumps(vault, old.Add(5*time.Hour)); err != nil {
		t.Fatal(err)
	}
	bumped, err := resolveV7Note(vault, stringField(first.Data, "id"), "escalation")
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "P1", stringField(bumped.Data, "severity"), "stale bump severity")
	assertEqual(t, "P2", stringField(bumped.Data, "stale_bumped_from"), "stale bump from")
	firstBumpedAt := stringField(bumped.Data, "stale_bumped_at")
	assertEqual(t, 1, notifications, "stale bump notification count")

	if _, err := applyStaleEscalationBumps(vault, old.Add(10*time.Hour)); err != nil {
		t.Fatal(err)
	}
	again, err := resolveV7Note(vault, stringField(first.Data, "id"), "escalation")
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "P1", stringField(again.Data, "severity"), "second stale bump severity")
	assertEqual(t, firstBumpedAt, stringField(again.Data, "stale_bumped_at"), "second stale bump timestamp")
	assertEqual(t, 1, notifications, "second stale bump notification count")

	acked, _, err := createV7Escalation(vault, escalationCreateRequest{
		Severity:     "P2",
		TaskID:       "APP-T-0001",
		Description:  "acked old escalation",
		Source:       "runner",
		Reason:       "system_error",
		Actor:        "agent:test",
		ValidateTask: true,
		Now:          old,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := escalationV7AckCmd(Args{"vault": vault, "quiet": "true", "local": "true", "id": stringField(acked.Data, "id"), "by": "human:sarav"}); err != nil {
		t.Fatal(err)
	}
	if _, err := applyStaleEscalationBumps(vault, old.Add(10*time.Hour)); err != nil {
		t.Fatal(err)
	}
	afterAck, err := resolveV7Note(vault, stringField(acked.Data, "id"), "escalation")
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "P2", stringField(afterAck.Data, "severity"), "acked stale severity")
	assertEqual(t, escalationStatusAcknowledged, stringField(afterAck.Data, "status"), "acked status")
	assertEqual(t, "human:sarav", stringField(afterAck.Data, "acknowledged_by"), "ack actor")
}

func TestEscalationNotifierGated(t *testing.T) {
	vault := pickupV7TestVault(t)
	writeDigestTask(t, vault, "APP-T-0001", "Notifier gate", "ready", "medium", "pending", "")

	var notifications []string
	oldNotify := notifyEscalationUser
	notifyEscalationUser = func(title, message string) error {
		notifications = append(notifications, title+"|"+message)
		return nil
	}
	t.Cleanup(func() { notifyEscalationUser = oldNotify })

	cfg := readEscalationRuntimeConfig(vault)
	assertEqual(t, false, cfg.NotificationsEnabled, "default notification config")
	if _, _, err := createV7Escalation(vault, escalationCreateRequest{Severity: "P1", TaskID: "APP-T-0001", Description: "default-off notification", Source: "runner", Reason: "system_error", Actor: "agent:test", ValidateTask: true}); err != nil {
		t.Fatal(err)
	}
	assertEqual(t, 0, len(notifications), "default-off notification count")

	writeEscalationNotificationsConfig(t, vault, false)
	if _, _, err := createV7Escalation(vault, escalationCreateRequest{Severity: "P0", TaskID: "APP-T-0001", Description: "configured-off notification", Source: "runner", Reason: "system_error", Actor: "agent:test", ValidateTask: true}); err != nil {
		t.Fatal(err)
	}
	assertEqual(t, 0, len(notifications), "configured-off notification count")

	writeEscalationNotificationsConfig(t, vault, true)
	if _, _, err := createV7Escalation(vault, escalationCreateRequest{Severity: "P1", TaskID: "APP-T-0001", Description: "configured-on notification", Source: "runner", Reason: "system_error", Actor: "agent:test", ValidateTask: true}); err != nil {
		t.Fatal(err)
	}
	assertEqual(t, 1, len(notifications), "configured-on notification count")

	recordPath := filepath.Join(t.TempDir(), "notifications.tsv")
	cmd := exec.Command("go", "run", "./cmd/tusker", "escalate", "-s", "P1", "--vault", vault, "--local", "--quiet", "--task", "APP-T-0001", "e2e recording notification")
	cmd.Dir = repoRootForFreshCloneTest(t)
	cmd.Env = append(os.Environ(),
		escalationNotifierModeEnv+"=record",
		escalationNotifierRecordEnv+"="+recordPath,
	)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("e2e escalation notification command failed: %v\n%s", err, output)
	}
	recorded, err := os.ReadFile(recordPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(recorded), "e2e recording notification") {
		t.Fatalf("e2e notifier did not record notification:\n%s", recorded)
	}
}

func TestDigestRender(t *testing.T) {
	vault := pickupV7TestVault(t)
	stateRoot := filepath.Join(t.TempDir(), "state")
	t.Setenv("TUSKER_STATE_ROOT", stateRoot)
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	projectID := v7ProjectID(vault)
	if err := store.UpsertProject(RegisteredProject{ProjectID: projectID, ProjectKey: "app", Name: "app", RepoRoot: filepath.Dir(vault), VaultRoot: vault, WorkflowPath: workflowPath(vault), Enabled: true, Health: projectHealthHealthy}); err != nil {
		t.Fatal(err)
	}

	writeDigestTask(t, vault, "APP-T-0001", "Closed task", "done", "medium", "satisfied", "2026-07-06T07:00:00Z")
	writeDigestTask(t, vault, "APP-T-0002", "Human acceptance", "review", "high", "satisfied", "")
	writeDigestTask(t, vault, "APP-T-0003", "Parked task", "ready", "medium", "pending", "")
	if err := newV7Gate(Args{"vault": vault, "quiet": "true", "blocks": "APP-T-0002", "kind": "release", "owner": "human:release", "action": "Authorize the production release.", "verification": "Release authority records approval.", "why-agent-cannot": "Only the production release authority can deploy.", "covers": "A1"}); err != nil {
		t.Fatal(err)
	}
	writeDigestWave(t, vault, "W-0001", "Morning wave", []string{"APP-T-0001"}, "2026-07-06T08:00:00Z")
	if _, _, err := createV7Escalation(vault, escalationCreateRequest{Severity: "P2", TaskID: "APP-T-0003", Description: "digest escalation", Source: "runner", Reason: "system_error", Actor: "agent:test", ValidateTask: true}); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertRun(RunStatus{ProjectID: projectID, RecordID: "APP-T-0003", ItemID: "APP-T-0003", Runner: string(RunnerCodexAppServer), Lane: runLaneExecute, LeaseState: string(LeaseStateParkedNoProgress), AttemptOutcome: string(AttemptOutcomeBlocked), AttemptCount: 3, LastError: "continuation retry cap reached", UpdatedAt: "2026-07-06T09:00:00Z", Terminal: true}); err != nil {
		t.Fatal(err)
	}
	if err := store.SetSetting(digestWatermarkKey(projectID), "2026-07-06T00:00:00Z"); err != nil {
		t.Fatal(err)
	}

	var cmdErr error
	output := captureStdout(t, func() {
		cmdErr = digestCmd(Args{"vault": vault})
	})
	if cmdErr != nil {
		t.Fatal(cmdErr)
	}
	assertDigestSectionOrder(t, output)
	for _, expected := range []string{"digest escalation", "W-0001", "Closed task", "APP-T-0003", "continuation retry cap reached", "APP-T-0002"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("digest missing %q:\n%s", expected, output)
		}
	}
	if strings.Contains(output, "Budget / Circuit Status") {
		t.Fatalf("digest must not render an authoritative budget/circuit section:\n%s", output)
	}
	watermark, err := store.GetSetting(digestWatermarkKey(projectID))
	if err != nil {
		t.Fatal(err)
	}
	if watermark == "" || watermark == "2026-07-06T00:00:00Z" {
		t.Fatalf("expected watermark to advance, got %q", watermark)
	}

	digest, err := buildTuskerDigest(vault, store, digestBuildOptions{
		Since:         time.Date(2026, 7, 6, 7, 30, 0, 0, time.UTC),
		SinceOverride: "2026-07-06T07:30:00Z",
		Now:           time.Date(2026, 7, 7, 8, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(digest.Landed) != 1 || digest.Landed[0].ID != "W-0001" {
		t.Fatalf("--since should include only the wave landed after override, got %#v", digest.Landed)
	}
}

func TestServeDigest(t *testing.T) {
	server := newServeFixture(t)
	if _, _, err := createV7Escalation(server.vaultPath, escalationCreateRequest{Severity: "P0", TaskID: "APP-T-0009", Description: "serve digest p0", Source: "runner", Reason: "security_concern", Actor: "agent:test", ValidateTask: true}); err != nil {
		t.Fatal(err)
	}
	if err := server.store.SetSetting(digestWatermarkKey("app"), "2026-07-06T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	expected, err := buildTuskerDigest(server.vaultPath, server.store, digestBuildOptions{Now: server.now()})
	if err != nil {
		t.Fatal(err)
	}
	var got tuskerDigest
	serveDecode(t, server, "/api/digest", &got)
	if !reflect.DeepEqual(expected, got) {
		t.Fatalf("/api/digest mismatch\nexpected: %#v\ngot: %#v", expected, got)
	}
	watermark, err := server.store.GetSetting(digestWatermarkKey("app"))
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "2026-07-06T00:00:00Z", watermark, "serve digest watermark")
	if !got.PersistentEscalationBanner {
		t.Fatal("expected P0 digest banner flag")
	}
	var daemon map[string]any
	serveDecode(t, server, "/api/daemon", &daemon)
	assertEqual(t, true, daemon["persistentEscalationBanner"], "daemon P0 banner flag")

	empty := newServeEmptyNeedsFixture(t)
	req := httptest.NewRequest(http.MethodGet, "/api/digest", nil)
	rec := httptest.NewRecorder()
	empty.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("/api/digest returned %d: %s", rec.Code, rec.Body.String())
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"openEscalations", "landed", "redParked", "pendingHardGates"} {
		if strings.TrimSpace(string(raw[key])) != "[]" {
			t.Fatalf("%s should marshal as [], got %s", key, raw[key])
		}
	}
}

func writeDigestTask(t *testing.T, vault, id, title, status, risk, proofStatus, closedAt string) {
	t.Helper()
	now := "2026-07-06T06:00:00Z"
	data := map[string]any{
		"schema":         "tusker.task/v7",
		"kind":           "task",
		"id":             id,
		"project":        v7ProjectID(vault),
		"title":          title,
		"epic":           "APP",
		"status":         status,
		"readiness":      "ready",
		"priority":       "p1",
		"risk":           risk,
		"size":           "s",
		"proof_mode":     "inline",
		"proof_status":   proofStatus,
		"proof_required": []string{"focused_test"},
		"next_owner":     "agent:codex",
		"next_action":    "Continue task.",
		"created_at":     now,
		"created_by":     "agent:test",
		"updated_at":     now,
		"updated_by":     "agent:test",
	}
	if status == "done" {
		data["readiness"] = "done"
		data["next_owner"] = "none"
		data["next_action"] = ""
		data["accepted_by"] = "reviewer:agent"
		data["accepted_at"] = closedAt
		data["closed_at"] = closedAt
	}
	body := "# " + id + " · " + title + "\n\n## Intent\n\nTest fixture.\n\n## Acceptance\n\n| ID | Outcome | Proof |\n|---|---|---|\n| A1 | Fixture is valid. | Inline verification |\n\n## Verification\n\n| Covers | Check | Result | Notes |\n|---|---|---|---|\n| A1 | fixture setup | pass | Test fixture. |\n"
	data["state_rev"] = v7StateRev(data, body)
	content, err := serializeDocument(data, body, v7FrontmatterOrder["task"])
	if err != nil {
		t.Fatal(err)
	}
	if err := writeText(filepath.Join(vault, "work", "tasks", id+".md"), content); err != nil {
		t.Fatal(err)
	}
}

func writeDigestWave(t *testing.T, vault, id, title string, members []string, landedAt string) {
	t.Helper()
	landings := make([]map[string]any, 0, len(members)+1)
	for _, member := range members {
		landings = append(landings, map[string]any{
			"task": member, "gate_result": "pass", "timestamp": landedAt,
		})
	}
	landings = append(landings, map[string]any{
		"task": "wave", "gate_result": "pass", "timestamp": landedAt,
	})
	data := map[string]any{
		"schema":     "tusker.wave/v7",
		"kind":       "wave",
		"id":         id,
		"project":    v7ProjectID(vault),
		"title":      title,
		"status":     "landed",
		"members":    members,
		"landings":   landings,
		"landed_at":  landedAt,
		"created_at": "2026-07-06T06:00:00Z",
		"created_by": "agent:test",
		"updated_at": landedAt,
		"updated_by": "agent:test",
	}
	body := "# " + id + " · " + title + "\n"
	data["state_rev"] = v7StateRev(data, body)
	content, err := serializeDocument(data, body, v7FrontmatterOrder["wave"])
	if err != nil {
		t.Fatal(err)
	}
	if err := writeText(filepath.Join(vault, "work", "waves", id+".md"), content); err != nil {
		t.Fatal(err)
	}
}

func writeEscalationNotificationsConfig(t *testing.T, vault string, enabled bool) {
	t.Helper()
	value := "false"
	if enabled {
		value = "true"
	}
	if err := writeText(filepath.Join(filepath.Dir(vault), "tusker.yaml"), strings.Join([]string{
		"schema: tusker.config/v1",
		"project_id: app",
		"storage:",
		"  root: " + filepath.Base(vault),
		"escalation:",
		"  notifications_enabled: " + value,
	}, "\n")+"\n"); err != nil {
		t.Fatal(err)
	}
}

func assertDigestSectionOrder(t *testing.T, output string) {
	t.Helper()
	sections := []string{"## Open Escalations", "## Landed", "## Red / Parked", "## Pending Hard Gates"}
	last := -1
	for _, section := range sections {
		idx := strings.Index(output, section)
		if idx < 0 {
			t.Fatalf("missing digest section %s:\n%s", section, output)
		}
		if idx <= last {
			t.Fatalf("section %s out of order:\n%s", section, output)
		}
		last = idx
	}
}
