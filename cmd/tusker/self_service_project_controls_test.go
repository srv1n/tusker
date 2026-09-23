package main

import (
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestSelfServiceProjectControls proves TSK-T-0044: Background-work toggles
// persist exact actor/source/before/after/time evidence, enable preview and
// readback enumerate the exact resume scope without arming anything, disable
// blocks new claims while admitted work finishes, and concurrent or failing
// writes stay truthful.
func TestSelfServiceProjectControls(t *testing.T) {
	t.Run("A1/toggles_persist_exact_evidence", func(t *testing.T) {
		store := fairDispatchTestStore(t)
		project := RegisteredProject{
			ProjectID: "project-audit", ProjectKey: "project-audit", Name: "audit",
			RepoRoot: t.TempDir(), VaultRoot: t.TempDir(), Enabled: false, Health: projectHealthDisabled,
		}
		if err := store.UpsertProject(project); err != nil {
			t.Fatal(err)
		}
		before, err := store.SetProjectAutomationAudited("project-audit", true, "op-tests", "cli")
		if err != nil {
			t.Fatal(err)
		}
		if before {
			t.Fatal("before state must report the previously disabled project")
		}
		before, err = store.SetProjectAutomationAudited("project-audit", false, "op-ui", "api")
		if err != nil {
			t.Fatal(err)
		}
		if !before {
			t.Fatal("before state must report the previously enabled project")
		}
		audit, err := store.ListProjectAutomationAudit("project-audit")
		if err != nil {
			t.Fatal(err)
		}
		if len(audit) != 2 {
			t.Fatalf("expected two toggle events, got %#v", audit)
		}
		first, second := audit[0], audit[1]
		if first.Actor != "op-tests" || first.Source != "cli" || first.BeforeEnabled || !first.AfterEnabled {
			t.Fatalf("first toggle lost evidence: %#v", first)
		}
		if second.Actor != "op-ui" || second.Source != "api" || !second.BeforeEnabled || second.AfterEnabled {
			t.Fatalf("second toggle lost evidence: %#v", second)
		}
		for _, event := range audit {
			if strings.TrimSpace(event.EventID) == "" || strings.TrimSpace(event.CreatedAt) == "" {
				t.Fatalf("toggle event lost identity/time: %#v", event)
			}
		}
		// The before/after chain must be contiguous: no toggle is lost.
		if second.BeforeEnabled != first.AfterEnabled {
			t.Fatalf("audit chain broke: %#v -> %#v", first, second)
		}
	})

	t.Run("A1/failed_writes_never_report_success", func(t *testing.T) {
		store := fairDispatchTestStore(t)
		project := RegisteredProject{
			ProjectID: "project-fail", ProjectKey: "project-fail", Name: "fail",
			RepoRoot: t.TempDir(), VaultRoot: t.TempDir(), Enabled: false, Health: projectHealthDisabled,
		}
		if err := store.UpsertProject(project); err != nil {
			t.Fatal(err)
		}
		store.projectAutomationAfterUpdate = func(*sql.Tx) error { return errors.New("injected persistence failure") }
		if _, err := store.SetProjectAutomationAudited("project-fail", true, "op-tests", "cli"); err == nil {
			t.Fatal("failed toggle must return an error, not success")
		}
		store.projectAutomationAfterUpdate = nil
		projects, err := store.ListProjects()
		if err != nil {
			t.Fatal(err)
		}
		for _, candidate := range projects {
			if candidate.ProjectID == "project-fail" && candidate.Enabled {
				t.Fatal("failed toggle left the project enabled")
			}
		}
		audit, err := store.ListProjectAutomationAudit("project-fail")
		if err != nil {
			t.Fatal(err)
		}
		if len(audit) != 0 {
			t.Fatalf("failed toggle left audit evidence: %#v", audit)
		}
	})

	t.Run("A1/legacy_changes_stay_unattributed", func(t *testing.T) {
		store := fairDispatchTestStore(t)
		project := RegisteredProject{
			ProjectID: "project-legacy", ProjectKey: "project-legacy", Name: "legacy",
			RepoRoot: t.TempDir(), VaultRoot: t.TempDir(), Enabled: false, Health: projectHealthDisabled,
		}
		if err := store.UpsertProject(project); err != nil {
			t.Fatal(err)
		}
		if err := store.SetProjectEnabled("project-legacy", true); err != nil {
			t.Fatal(err)
		}
		audit, err := store.ListProjectAutomationAudit("project-legacy")
		if err != nil {
			t.Fatal(err)
		}
		if len(audit) != 0 {
			t.Fatalf("unaudited historical change gained attribution: %#v", audit)
		}
	})

	t.Run("A2/preview_and_readback_enumerate_resume_scope", func(t *testing.T) {
		vault, store, project := authorityFixture(t)
		now := time.Now().UTC()
		writeDirectTask(t, vault, "ARM-T-0001", "W-ARMED", nil)
		writeDirectWave(t, vault, "W-ARMED", []string{"ARM-T-0001"}, nil)
		if _, err := directWaveStart(vault, store, "W-ARMED", "human:test"); err != nil {
			t.Fatal(err)
		}
		writeDirectTask(t, vault, "PAU-T-0001", "W-PAUSED", nil)
		writeDirectWave(t, vault, "W-PAUSED", []string{"PAU-T-0001"}, nil)
		if _, err := directWaveStart(vault, store, "W-PAUSED", "human:test"); err != nil {
			t.Fatal(err)
		}
		if _, err := directWavePause(vault, store, "W-PAUSED", "human:test"); err != nil {
			t.Fatal(err)
		}
		writeDirectTask(t, vault, "OFF-T-0001", "W-OFF", nil)
		writeDirectWave(t, vault, "W-OFF", []string{"OFF-T-0001"}, nil)
		writeDirectTask(t, vault, "STL-T-0001", "W-STALE", nil)
		writeDirectWave(t, vault, "W-STALE", []string{"STL-T-0001"}, nil)
		if _, err := directWaveStart(vault, store, "W-STALE", "human:test"); err != nil {
			t.Fatal(err)
		}
		writeDirectTask(t, vault, "STL-T-0001", "W-STALE", map[string]any{"title": "Changed after arming"})
		before := snapshotWaveAuthorizations(t, vault, []string{"W-ARMED", "W-PAUSED", "W-OFF", "W-STALE"})
		beforeTasks := snapshotTaskStatuses(t, vault, []string{"ARM-T-0001", "PAU-T-0001", "OFF-T-0001", "STL-T-0001"})
		// Preview: exact scope, no mutation.
		output := captureStdout(t, func() {
			if err := emitProjectAutomationPreview(store, Args{"json": "true"}, project); err != nil {
				t.Fatal(err)
			}
		})
		if !strings.Contains(output, "W-ARMED") {
			t.Fatalf("preview hid the armed wave: %s", output)
		}
		scope, err := projectAutomationScope(store, project, now)
		if err != nil {
			t.Fatal(err)
		}
		if len(scope.Waves) != 1 || scope.Waves[0].WaveID != "W-ARMED" {
			t.Fatalf("scope must hold exactly the armed wave, got %#v", scope.Waves)
		}
		if scope.Waves[0].ProjectID != project.ProjectID {
			t.Fatalf("scope lost project identity: %#v", scope.Waves[0])
		}
		excluded := map[string]string{}
		for _, entry := range scope.Excluded {
			excluded[entry.WaveID] = entry.Reason
		}
		for waveID, want := range map[string]string{
			"W-PAUSED": "paused", "W-OFF": "disarmed", "W-STALE": "stale",
		} {
			reason, ok := excluded[waveID]
			if !ok || !strings.Contains(reason, want) {
				t.Fatalf("scope must exclude %s as %s, got %#v", waveID, want, excluded)
			}
		}
		// Readback via the CLI command: same scope, same exclusions.
		readback := captureStdout(t, func() {
			if err := projectsAutomationScopeCmd(Args{"id": project.ProjectID, "json": "true"}); err != nil {
				t.Fatal(err)
			}
		})
		for _, want := range []string{"W-ARMED", "W-PAUSED", "W-OFF", "W-STALE"} {
			if !strings.Contains(readback, want) {
				t.Fatalf("readback lost wave %s: %s", want, readback)
			}
		}
		// Neither preview nor readback arms, enables, or edits anything.
		if after := snapshotWaveAuthorizations(t, vault, []string{"W-ARMED", "W-PAUSED", "W-OFF", "W-STALE"}); after != before {
			t.Fatalf("scope read changed wave authorization:\nbefore %s\nafter  %s", before, after)
		}
		if after := snapshotTaskStatuses(t, vault, []string{"ARM-T-0001", "PAU-T-0001", "OFF-T-0001", "STL-T-0001"}); after != beforeTasks {
			t.Fatalf("scope read changed task status:\nbefore %s\nafter  %s", beforeTasks, after)
		}
	})

	t.Run("A3/disable_blocks_claims_while_admitted_work_finishes", func(t *testing.T) {
		vault, store, project := authorityFixture(t)
		now := time.Now().UTC()
		writeDirectTask(t, vault, "CLM-T-0001", "W-CLM", nil)
		writeDirectWave(t, vault, "W-CLM", []string{"CLM-T-0001"}, nil)
		if _, err := directWaveStart(vault, store, "W-CLM", "human:test"); err != nil {
			t.Fatal(err)
		}
		admitted := RunStatus{
			ProjectID: project.ProjectID, RecordID: "CLM-T-0001", ItemID: "CLM-T-0001",
			Runner: string(RunnerCodexExec), Lane: runLaneExecute,
			LeaseState: string(LeaseStateRunning), LeaseOwner: "attempt-live",
			LeaseGeneration: 1, LeaseExpiresAt: now.Add(time.Hour).Format(time.RFC3339),
			ActiveAttemptID: "attempt-live", WorkRevision: 1,
		}
		if err := store.UpsertRun(admitted); err != nil {
			t.Fatal(err)
		}
		directivesBefore, err := store.ListActiveRunDirectives(project.ProjectID, now)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.SetProjectAutomationAudited(project.ProjectID, false, "op-tests", "cli"); err != nil {
			t.Fatal(err)
		}
		// New claims are refused with the project-off owner and repair.
		verdict := EvaluateAdmissionForStage(AdmissionFacts{
			TaskID: "CLM-T-0002", Status: "ready", Lane: runLaneExecute,
			ContractValid: true, RouteOK: true, ProofMapped: true,
			OwnerFree: true, DependenciesSatisfied: true,
			ProjectRegistered: true, ProjectEnabled: false,
			Authority: AdmissionAuthorityWaveArmed, AuthorityMatches: true,
		}, AdmissionStageDaemonDispatch)
		if verdict.Admit || !hasAdmissionBlocker(verdict, AdmissionBlockerProjectDisabled) {
			t.Fatalf("disabled project admitted a new claim: %#v", verdict)
		}
		// Admitted work is untouched: the same run and directives survive.
		current, err := store.FindRunScoped(project.ProjectID, "CLM-T-0001")
		if err != nil {
			t.Fatal(err)
		}
		if current == nil || current.LeaseState != string(LeaseStateRunning) || current.LeaseOwner != "attempt-live" {
			t.Fatalf("disable disturbed admitted work: %#v", current)
		}
		directivesAfter, err := store.ListActiveRunDirectives(project.ProjectID, now)
		if err != nil {
			t.Fatal(err)
		}
		if len(directivesAfter) != len(directivesBefore) {
			t.Fatalf("disable lost directives: before=%d after=%d", len(directivesBefore), len(directivesAfter))
		}
	})

	t.Run("A3/reenable_wakes_intent_without_losing_or_duplicating", func(t *testing.T) {
		vault, store, project := authorityFixture(t)
		now := time.Now().UTC()
		writeDirectTask(t, vault, "RE-T-0001", "W-RE", nil)
		writeDirectWave(t, vault, "W-RE", []string{"RE-T-0001"}, nil)
		if _, err := directWaveStart(vault, store, "W-RE", "human:test"); err != nil {
			t.Fatal(err)
		}
		if _, err := store.SetProjectAutomationAudited(project.ProjectID, false, "op-tests", "cli"); err != nil {
			t.Fatal(err)
		}
		directivesBefore, err := store.ListActiveRunDirectives(project.ProjectID, now)
		if err != nil {
			t.Fatal(err)
		}
		attemptsBefore, err := store.ListAttemptsForRun(project.ProjectID, "RE-T-0001")
		if err != nil {
			t.Fatal(err)
		}
		previous := daemonControlOneWaySender
		defer func() { daemonControlOneWaySender = previous }()
		var wakes []daemonControlRequest
		daemonControlOneWaySender = func(_ string, req daemonControlRequest, _ time.Duration) error {
			wakes = append(wakes, req)
			return nil
		}
		output := captureStdout(t, func() {
			if err := projectsEnableCmd(Args{"id": project.ProjectID, "json": "true"}); err != nil {
				t.Fatal(err)
			}
		})
		if len(wakes) != 1 || wakes[0].Cause != "project_enable" || wakes[0].ProjectID != project.ProjectID {
			t.Fatalf("re-enable did not wake existing intent: %#v", wakes)
		}
		if !strings.Contains(output, "W-RE") {
			t.Fatalf("re-enable hid the resume scope: %s", output)
		}
		directivesAfter, err := store.ListActiveRunDirectives(project.ProjectID, now)
		if err != nil {
			t.Fatal(err)
		}
		if len(directivesAfter) != len(directivesBefore) {
			t.Fatalf("re-enable lost directives: before=%d after=%d", len(directivesBefore), len(directivesAfter))
		}
		attemptsAfter, err := store.ListAttemptsForRun(project.ProjectID, "RE-T-0001")
		if err != nil {
			t.Fatal(err)
		}
		if len(attemptsAfter) != len(attemptsBefore) {
			t.Fatalf("re-enable duplicated attempts: before=%d after=%d", len(attemptsBefore), len(attemptsAfter))
		}
	})

	t.Run("A4/interleaved_toggles_stay_truthful", func(t *testing.T) {
		store := fairDispatchTestStore(t)
		project := RegisteredProject{
			ProjectID: "project-race", ProjectKey: "project-race", Name: "race",
			RepoRoot: t.TempDir(), VaultRoot: t.TempDir(), Enabled: false, Health: projectHealthDisabled,
		}
		if err := store.UpsertProject(project); err != nil {
			t.Fatal(err)
		}
		for index := 0; index < 6; index++ {
			enabled := index%2 == 0
			before, err := store.SetProjectAutomationAudited("project-race", enabled, "op-concurrent", "cli")
			if err != nil {
				t.Fatal(err)
			}
			if before == enabled && index > 0 {
				t.Fatalf("toggle %d lost its transition: before=%v after=%v", index, before, enabled)
			}
		}
		audit, err := store.ListProjectAutomationAudit("project-race")
		if err != nil {
			t.Fatal(err)
		}
		if len(audit) != 6 {
			t.Fatalf("interleaved toggles lost history: %#v", audit)
		}
		for index := 1; index < len(audit); index++ {
			if audit[index].BeforeEnabled != audit[index-1].AfterEnabled {
				t.Fatalf("audit chain broke at %d: %#v", index, audit)
			}
		}
		projects, err := store.ListProjects()
		if err != nil {
			t.Fatal(err)
		}
		for _, candidate := range projects {
			if candidate.ProjectID == "project-race" && candidate.Enabled != audit[len(audit)-1].AfterEnabled {
				t.Fatalf("final state %v disagrees with audit %v", candidate.Enabled, audit[len(audit)-1].AfterEnabled)
			}
		}
	})

	t.Run("A4/failed_validation_persists_and_audits_nothing", func(t *testing.T) {
		vault, store, _ := authorityFixture(t)
		_ = vault
		broken := RegisteredProject{
			ProjectID: "project-broken", ProjectKey: "project-broken", Name: "broken",
			RepoRoot: filepath.Join(t.TempDir(), "repo"), VaultRoot: filepath.Join(t.TempDir(), "elsewhere", "vault"),
			Enabled: false, Health: projectHealthDisabled,
		}
		if err := store.UpsertProject(broken); err != nil {
			t.Fatal(err)
		}
		if err := projectsEnableCmd(Args{"id": "project-broken", "json": "true"}); err == nil {
			t.Fatal("enable past failed validation must not succeed")
		}
		projects, err := store.ListProjects()
		if err != nil {
			t.Fatal(err)
		}
		for _, candidate := range projects {
			if candidate.ProjectID == "project-broken" && candidate.Enabled {
				t.Fatal("failed validation left the project enabled")
			}
		}
		audit, err := store.ListProjectAutomationAudit("project-broken")
		if err != nil {
			t.Fatal(err)
		}
		if len(audit) != 0 {
			t.Fatalf("failed validation left audit evidence: %#v", audit)
		}
	})
}

func snapshotWaveAuthorizations(t *testing.T, vault string, waveIDs []string) string {
	t.Helper()
	var parts []string
	for _, id := range waveIDs {
		raw, err := readText(filepath.Join(vault, "work", "waves", id+".md"))
		if err != nil {
			t.Fatal(err)
		}
		data, _, err := parseFrontmatter(raw)
		if err != nil {
			t.Fatal(err)
		}
		parts = append(parts, id+"="+stringField(data, "authorization")+"/"+stringField(data, "authorization_fingerprint"))
	}
	return strings.Join(parts, ";")
}

func snapshotTaskStatuses(t *testing.T, vault string, taskIDs []string) string {
	t.Helper()
	var parts []string
	for _, id := range taskIDs {
		raw, err := readText(filepath.Join(vault, "work", "tasks", id+".md"))
		if err != nil {
			t.Fatal(err)
		}
		data, _, err := parseFrontmatter(raw)
		if err != nil {
			t.Fatal(err)
		}
		parts = append(parts, id+"="+stringField(data, "status")+"/"+stringField(data, "readiness"))
	}
	return strings.Join(parts, ";")
}
