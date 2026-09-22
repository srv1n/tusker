package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func doctorJSONReportBytes(t *testing.T, vault, target string) ([]byte, int) {
	t.Helper()
	var code int
	captured := captureStdout(t, func() {
		var err error
		code, err = executionDoctorCmd(Args{"vault": vault, "_pos0": target, "json": "true"})
		if err != nil {
			t.Fatalf("doctor %s json: %v", target, err)
		}
	})
	return []byte(strings.TrimSpace(captured)), code
}

func doctorFindingByCode(report doctorReport, code string) *DiagnosticFinding {
	for index := range report.Diagnosis.Findings {
		if report.Diagnosis.Findings[index].Code == code {
			return &report.Diagnosis.Findings[index]
		}
	}
	return nil
}

func TestSelfServiceDoctor(t *testing.T) {
	t.Run("A1 paused reservation names its owner without blaming capacity", func(t *testing.T) {
		vault, store, project, _, _ := selfServiceArmedABFixture(t)
		if _, err := directWavePause(vault, store, "W-0001", "human:sarav"); err != nil {
			t.Fatalf("pause: %v", err)
		}

		raw, code := doctorJSONReportBytes(t, vault, "APP-T-0001")
		if code != 1 {
			t.Fatalf("doctor exit = %d, want 1: %s", code, raw)
		}
		var report doctorReport
		if err := json.Unmarshal(raw, &report); err != nil {
			t.Fatalf("unmarshal doctor report: %v", err)
		}
		if report.SubjectType != "task" || report.SubjectID != "APP-T-0001" {
			t.Fatalf("report subject = %s/%s", report.SubjectType, report.SubjectID)
		}
		reservation := doctorFindingByCode(report, "doctor-queued-reservation")
		if reservation == nil {
			t.Fatalf("doctor did not identify the owning queued reservation: %v", report.Diagnosis.FindingCodes())
		}
		if !strings.Contains(reservation.Evidence.Detail, "W-0001") {
			t.Fatalf("reservation finding does not name its wave: %q", reservation.Evidence.Detail)
		}
		if reservation.NextActor != DiagnosticAuthorityDaemon {
			t.Fatalf("reservation next actor = %s, want daemon", reservation.NextActor)
		}
		paused := doctorFindingByCode(report, "doctor-wave-paused")
		if paused == nil {
			t.Fatalf("doctor did not report the paused wave: %v", report.Diagnosis.FindingCodes())
		}
		if paused.Action == nil || len(paused.Action.Argv) == 0 || paused.Action.Argv[0] != "tusker" {
			t.Fatalf("paused wave has no executable repair argv: %#v", paused.Action)
		}
		if !strings.Contains(strings.Join(paused.Action.Argv, " "), "wave resume W-0001") {
			t.Fatalf("paused repair is not wave resume: %q", paused.Action.Argv)
		}
		for _, finding := range report.Diagnosis.Findings {
			if strings.Contains(finding.Code, "capacity") {
				t.Fatalf("doctor falsely blamed capacity with zero active workers: %#v", finding)
			}
		}
		if report.Diagnosis.PrimaryCode != "doctor-wave-paused" {
			t.Fatalf("primary = %s, want doctor-wave-paused", report.Diagnosis.PrimaryCode)
		}

		human := captureStdout(t, func() {
			humanCode, err := executionDoctorCmd(Args{"vault": vault, "_pos0": "APP-T-0001"})
			if err != nil {
				t.Fatalf("doctor human: %v", err)
			}
			if humanCode != 1 {
				t.Fatalf("doctor human exit = %d, want 1", humanCode)
			}
		})
		if !strings.Contains(human, "doctor-wave-paused") || !strings.Contains(human, "tusker wave resume W-0001") {
			t.Fatalf("human output misses cause and repair: %q", human)
		}
		if strings.Contains(strings.ToLower(human), "capacity") {
			t.Fatalf("human output blames capacity: %q", human)
		}
		_ = project
	})

	t.Run("A2 every blocker carries scope evidence actor and repair or explicit none", func(t *testing.T) {
		vault, store, _, _, _ := selfServiceArmedABFixture(t)

		raw, _ := doctorJSONReportBytes(t, vault, "APP-T-0002")
		var dependent doctorReport
		if err := json.Unmarshal(raw, &dependent); err != nil {
			t.Fatal(err)
		}
		waiting := doctorFindingByCode(dependent, "doctor-dependency-waiting")
		if waiting == nil {
			t.Fatalf("dependent has no dependency finding: %v", dependent.Diagnosis.FindingCodes())
		}

		writeHumanGate(t, vault, "G-DOCTOR-1", "APP-T-0002")
		raw, _ = doctorJSONReportBytes(t, vault, "APP-T-0002")
		var gated doctorReport
		if err := json.Unmarshal(raw, &gated); err != nil {
			t.Fatal(err)
		}
		gate := doctorFindingByCode(gated, "doctor-human-gate-open")
		if gate == nil {
			t.Fatalf("gated dependent has no human-gate finding: %v", gated.Diagnosis.FindingCodes())
		}
		if gate.Classification != DiagnosticHumanDecision || gate.NextActor != DiagnosticAuthorityHuman {
			t.Fatalf("gate finding misclassified: %#v", gate)
		}

		writeDirectTask(t, vault, "APP-T-0009", "", map[string]any{"wave": "W-9999"})
		raw, code := doctorJSONReportBytes(t, vault, "APP-T-0009")
		if code != 1 {
			t.Fatalf("dangling wave exit = %d, want 1: %s", code, raw)
		}
		var dangling doctorReport
		if err := json.Unmarshal(raw, &dangling); err != nil {
			t.Fatal(err)
		}
		missing := doctorFindingByCode(dangling, "doctor-wave-missing")
		if missing == nil || missing.Classification != DiagnosticProductDefect {
			t.Fatalf("dangling wave reference misdiagnosed: %v", dangling.Diagnosis.FindingCodes())
		}
		if missing.Action == nil || missing.Action.Type != RecoveryActionNoSupportedFix {
			t.Fatalf("dangling wave lacks explicit no-supported-repair: %#v", missing.Action)
		}

		for label, report := range map[string]doctorReport{"dependent": dependent, "gated": gated, "dangling": dangling} {
			for _, finding := range report.Diagnosis.Findings {
				if strings.TrimSpace(finding.Code) == "" || !validDiagnosticClassification(finding.Classification) {
					t.Fatalf("%s finding lacks stable code/classification: %#v", label, finding)
				}
				if strings.TrimSpace(finding.Scope.Project) == "" && strings.TrimSpace(finding.Scope.Record) == "" && strings.TrimSpace(finding.Scope.Wave) == "" {
					t.Fatalf("%s finding lacks scope: %#v", label, finding)
				}
				if strings.TrimSpace(finding.Evidence.Source) == "" || strings.TrimSpace(finding.Evidence.Revision) == "" || strings.TrimSpace(finding.Evidence.ObservedAt) == "" {
					t.Fatalf("%s finding lacks evidence: %#v", label, finding)
				}
				if !validDiagnosticAuthority(finding.NextActor) {
					t.Fatalf("%s finding lacks next actor: %#v", label, finding)
				}
				if action := finding.Action; action != nil {
					hasArgv := len(action.Argv) > 0
					isNone := action.Type == RecoveryActionNoSupportedFix
					if hasArgv == isNone {
						t.Fatalf("%s finding must carry argv or explicit no-supported-repair: %#v", label, finding)
					}
					if !validDiagnosticAuthority(action.RequiredAuthority) || action.RequiredAuthority == DiagnosticAuthorityNone {
						t.Fatalf("%s action lacks authority: %#v", label, finding)
					}
					if strings.TrimSpace(action.Postcondition) == "" {
						t.Fatalf("%s action lacks postcondition: %#v", label, finding)
					}
				}
			}
		}
		_ = store
	})

	t.Run("A3 waits name owner and next check while unavailable stays visible", func(t *testing.T) {
		vault, _, _, _, _ := selfServiceArmedABFixture(t)
		raw, code := doctorJSONReportBytes(t, vault, "APP-T-0002")
		if code != 0 {
			t.Fatalf("dependency wait exit = %d, want 0: %s", code, raw)
		}
		var report doctorReport
		if err := json.Unmarshal(raw, &report); err != nil {
			t.Fatal(err)
		}
		if report.Diagnosis.PrimaryClassification != DiagnosticNormalWait {
			t.Fatalf("wait primary = %s", report.Diagnosis.PrimaryClassification)
		}
		waiting := doctorFindingByCode(report, "doctor-dependency-waiting")
		if waiting == nil {
			t.Fatalf("no dependency wait finding: %v", report.Diagnosis.FindingCodes())
		}
		if waiting.Action != nil {
			t.Fatalf("normal wait suggests repair: %#v", waiting.Action)
		}
		if strings.TrimSpace(waiting.RetryAfter) == "" {
			t.Fatal("normal wait names no next check")
		}

		unavailable, err := diagnoseTaskForDoctorWithRuntime(vault, nil, tuskerError(errorReadinessContractInvalid, "runtime store is gone"), "test-project", "APP-T-0001", time.Now().UTC())
		if err != nil {
			t.Fatalf("unavailable diagnosis errored: %v", err)
		}
		if unavailable.PrimaryClassification != DiagnosticUnavailable {
			t.Fatalf("unavailable primary = %s", unavailable.PrimaryClassification)
		}
		if got := unavailable.SafeActions(); len(got) != 0 {
			t.Fatalf("unavailable diagnosis offers safe actions: %#v", got)
		}
		if report.BuildComparison != "unknown" {
			t.Fatalf("build comparison = %q, want explicit unknown", report.BuildComparison)
		}
	})

	t.Run("A4 help capabilities export and repeat reads leave state alone", func(t *testing.T) {
		vault, store, project, _, _ := selfServiceArmedABFixture(t)

		help := captureStdout(t, func() {
			if code, err := run("doctor", Args{"help": "true"}); err != nil || code != 0 {
				t.Fatalf("doctor help: code=%d err=%v", code, err)
			}
		})
		if !strings.Contains(help, "tusker doctor <TASK-ID|WAVE-ID>") || !strings.Contains(help, "--output") {
			t.Fatalf("doctor help is missing usage: %q", help)
		}
		foundDoctor := false
		for _, entry := range installedCapabilityCommands() {
			if entry.Command != "doctor" {
				continue
			}
			foundDoctor = true
			hasJSON, hasOutput := false, false
			for _, flag := range entry.Flags {
				hasJSON = hasJSON || flag == "--json"
				hasOutput = hasOutput || flag == "--output"
			}
			if !hasJSON || !hasOutput {
				t.Fatalf("capabilities doctor entry lacks flags: %#v", entry)
			}
		}
		if !foundDoctor {
			t.Fatal("capabilities manifest has no doctor entry")
		}

		beforeTask, err := os.ReadFile(filepath.Join(vault, "work", "tasks", "APP-T-0001.md"))
		if err != nil {
			t.Fatal(err)
		}
		beforeDirectives := len(queuedDirectives(t, store, project.ProjectID))
		first, firstCode := doctorJSONReportBytes(t, vault, "W-0001")
		second, secondCode := doctorJSONReportBytes(t, vault, "W-0001")
		if firstCode != secondCode || string(first) != string(second) {
			t.Fatal("repeated diagnosis is not stable")
		}
		afterTask, err := os.ReadFile(filepath.Join(vault, "work", "tasks", "APP-T-0001.md"))
		if err != nil {
			t.Fatal(err)
		}
		if string(beforeTask) != string(afterTask) {
			t.Fatal("doctor mutated vault state")
		}
		if got := len(queuedDirectives(t, store, project.ProjectID)); got != beforeDirectives {
			t.Fatalf("doctor changed reservations: %d -> %d", beforeDirectives, got)
		}

		exportPath := filepath.Join(t.TempDir(), "doctor.json")
		captureStdout(t, func() {
			if code, err := executionDoctorCmd(Args{"vault": vault, "_pos0": "W-0001", "json": "true", "output": exportPath}); err != nil {
				t.Fatalf("doctor export: %v", err)
			} else if code != firstCode {
				t.Fatalf("export run exit = %d, want %d", code, firstCode)
			}
		})
		exported, err := os.ReadFile(exportPath)
		if err != nil {
			t.Fatal(err)
		}
		var exportedReport doctorReport
		if err := json.Unmarshal(exported, &exportedReport); err != nil {
			t.Fatalf("exported JSON is not a doctor report: %v", err)
		}
		if exportedReport.Diagnosis.PrimaryCode == "" || exportedReport.Schema != "tusker.doctor/v1" {
			t.Fatalf("exported report is incomplete: %s", exported)
		}
		if code, err := executionDoctorCmd(Args{"vault": vault, "_pos0": "W-0001", "json": "true", "output": exportPath}); err == nil || code != 1 {
			t.Fatalf("export overwrote an existing file: code=%d err=%v", code, err)
		}
		if code, err := executionDoctorCmd(Args{"vault": vault, "_pos0": "W-9999", "json": "true"}); err == nil || code != 2 {
			t.Fatalf("unknown target was not refused with exit 2: code=%d err=%v", code, err)
		}
	})
}
