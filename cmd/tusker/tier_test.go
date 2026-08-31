package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestTuskerTierDefaultsAbsentAndInvalidFallbackToFive(t *testing.T) {
	vault := pickupV7TestVault(t)
	if got := tuskerTier(vault); got != 5 {
		t.Fatalf("absent tier = %d, want 5", got)
	}
	absentReport, err := configResolve(vault, "tier")
	if err != nil {
		t.Fatal(err)
	}
	if absentReport.Value != 5 || absentReport.Source != configSourceBuiltIn {
		t.Fatalf("absent tier report = %#v, want built-in 5", absentReport)
	}
	for _, value := range []string{"0", "6", "not-a-tier"} {
		if err := writeText(managedTuskerLocalConfigPath(vault), "tier: "+value+"\n"); err != nil {
			t.Fatal(err)
		}
		if got := tuskerTier(vault); got != 5 {
			t.Fatalf("invalid tier %q defensive fallback = %d, want 5", value, got)
		}
		if _, err := resolveTuskerConfig(vault); err == nil || errorToIssue(err).Code != errorConfigInvalid {
			t.Fatalf("invalid tier %q should be rejected by config validation, got %v", value, err)
		}
	}
	if err := writeText(managedTuskerLocalConfigPath(vault), "tier: 3\n"); err != nil {
		t.Fatal(err)
	}
	if got := tuskerTier(vault); got != 3 {
		t.Fatalf("explicit tier = %d, want 3", got)
	}

	report, err := configResolve(vault, "tier")
	if err != nil {
		t.Fatal(err)
	}
	if report.Value != 3 || report.Source != configSourceLocal || report.Path != managedTuskerLocalConfigPath(vault) {
		t.Fatalf("tier provenance = %#v, want local tier 3", report)
	}
	output := captureStdout(t, func() {
		if err := configResolveCmd(Args{"vault": vault, "key": "tier"}); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(output, "effective: 3") || !strings.Contains(output, "source: "+configSourceLocal) {
		t.Fatalf("config resolve tier output missing value/provenance:\n%s", output)
	}
}

func TestTierOneCreateReadySkipsDispatchabilityRefusal(t *testing.T) {
	for _, tc := range []struct {
		name    string
		tier    int
		wantErr bool
	}{
		{name: "tier one", tier: 1},
		{name: "tier two", tier: 2, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			vault := v7DispatchTestVault(t)
			if _, err := setProjectLocalConfigWithReadback(vault, "tier", tc.tier); err != nil {
				t.Fatal(err)
			}
			err := newV7Task(Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Ready task", "status": "ready"})
			if tc.wantErr {
				if err == nil || !strings.Contains(err.Error(), "not dispatchable") {
					t.Fatalf("expected dispatchability refusal, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestTierOneAllowsUncoveredEvidenceAsWarningOnly(t *testing.T) {
	for _, tc := range []struct {
		name    string
		tier    int
		wantErr bool
	}{
		{name: "tier one", tier: 1},
		{name: "tier two", tier: 2, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			vault := v7DispatchTestVault(t)
			if _, err := setProjectLocalConfigWithReadback(vault, "tier", tc.tier); err != nil {
				t.Fatal(err)
			}
			if err := newV7Task(Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Evidence target"}); err != nil {
				t.Fatal(err)
			}
			err := evidenceV7AddCmd(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "kind": "manual_smoke", "summary": "Recon finding."})
			if tc.wantErr {
				if err == nil || errorToIssue(err).Code != errorMissingArg {
					t.Fatalf("strict tier uncovered evidence = %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			note, err := resolveV7Note(vault, "APP-T-0001-E-0001", "evidence")
			if err != nil {
				t.Fatal(err)
			}
			var errs, warns []Issue
			validateV7Evidence(note, validationContext{VaultPath: vault}, note.RelativePath, &errs, &warns)
			if issuesContainCode(errs, "EVIDENCE_COVERS_MISSING") || !issuesContainCode(warns, "EVIDENCE_COVERS_MISSING") {
				t.Fatalf("tier one uncovered evidence validation errors=%#v warnings=%#v", errs, warns)
			}
		})
	}
}

func TestTierOneNextPicksPlainReadyTask(t *testing.T) {
	vault := v7DispatchTestVault(t)
	if _, err := setProjectLocalConfigWithReadback(vault, "tier", 1); err != nil {
		t.Fatal(err)
	}
	if err := newV7Task(Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Plain ready", "status": "ready"}); err != nil {
		t.Fatal(err)
	}
	selected, ok := pickV7Next(vault, "APP", "")
	if !ok || stringField(selected.Data, "id") != "APP-T-0001" {
		t.Fatalf("tier one next selected %#v, ok=%v", selected.Data, ok)
	}
}

func TestTierTwoNextRestoresFullDispatchBlockers(t *testing.T) {
	vault := v7DispatchTestVault(t)
	if _, err := setProjectLocalConfigWithReadback(vault, "tier", 2); err != nil {
		t.Fatal(err)
	}
	if err := newV7Task(Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Placeholder ready"}); err != nil {
		t.Fatal(err)
	}
	forceV7TaskProjection(t, vault, "APP-T-0001", "ready", "ready", "agent", "Execute.")
	if _, ok := pickV7Next(vault, "APP", ""); ok {
		t.Fatal("tier two next must retain full dispatch blockers")
	}
	idx, err := loadV7Index(vault)
	if err != nil {
		t.Fatal(err)
	}
	reasons := v7NextSkipReasons(vault, idx.Tasks["APP-T-0001"], nextPickFilters{}, resolveV7DispatchContext(vault))
	if !strings.Contains(strings.Join(reasons, "\n"), "verification missing exact command or manual proof") {
		t.Fatalf("tier two blockers missing full verification reason: %v", reasons)
	}
}

func TestTierOneStatusDoneUsesCloseProjection(t *testing.T) {
	vault := v7DispatchTestVault(t)
	if _, err := setProjectLocalConfigWithReadback(vault, "tier", 1); err != nil {
		t.Fatal(err)
	}
	if err := newV7Task(Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Direct close"}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(vault, "work", "tasks", "APP-T-0001.md")
	setAutomationV7TaskFields(t, vault, "APP-T-0001", map[string]any{
		"discarded_by": "agent:old", "discarded_at": "2026-08-17T00:00:00Z", "discard_reason": "old",
	})
	before, _, err := parseFrontmatterMustRead(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := statusV7Cmd(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "status": "done"}); err != nil {
		t.Fatal(err)
	}
	data, _, err := parseFrontmatterMustRead(path)
	if err != nil {
		t.Fatal(err)
	}
	if stringField(data, "status") != "done" || stringField(data, "readiness") != "done" || stringField(data, "next_owner") != "none" || stringField(data, "next_source") != "status" || stringField(data, "next_ref") != "" || stringField(data, "next_action") != "" || stringField(data, "closed_at") == "" {
		t.Fatalf("direct done did not apply close projection: %#v", data)
	}
	if stringField(data, "state_rev") == "" || stringField(data, "state_rev") == stringField(before, "state_rev") {
		t.Fatalf("direct done did not advance state_rev: before=%q after=%q", stringField(before, "state_rev"), stringField(data, "state_rev"))
	}
	for _, key := range []string{"discarded_by", "discarded_at", "discard_reason"} {
		if _, present := data[key]; present {
			t.Fatalf("direct done retained %s: %#v", key, data)
		}
	}
}

func TestTierOneCloseSkipsProofGateButTierTwoKeepsIt(t *testing.T) {
	for _, tc := range []struct {
		name    string
		tier    int
		wantErr bool
	}{
		{name: "tier one", tier: 1},
		{name: "tier two", tier: 2, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			vault := v7DispatchTestVault(t)
			if _, err := setProjectLocalConfigWithReadback(vault, "tier", tc.tier); err != nil {
				t.Fatal(err)
			}
			if err := newV7Task(Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Proofless close", "proof-mode": "inline", "proof-required": "focused_test"}); err != nil {
				t.Fatal(err)
			}
			replaceV7TaskSection(t, vault, "APP-T-0001", "## Acceptance", "| ID | Outcome | Proof |\n|---|---|---|\n| A1 | The task may close after the tier policy is applied. | Focused proof |")
			replaceV7TaskSection(t, vault, "APP-T-0001", "## Verification", "| Covers | Check | Result | Notes |\n|---|---|---|---|")
			setAutomationV7TaskFields(t, vault, "APP-T-0001", map[string]any{
				"status": "review", "readiness": "waiting_on_review", "proof_status": "pending",
			})
			taskPath := filepath.Join(vault, "work", "tasks", "APP-T-0001.md")
			if rows := parseV7VerificationRows(mustBody(t, taskPath)); len(rows) != 0 {
				t.Fatalf("proofless close fixture has %d verification rows", len(rows))
			}

			err := closeV7Cmd(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "by": "reviewer:agent"})
			if tc.wantErr {
				if err == nil || errorToIssue(err).Code != errorEvidenceGate {
					t.Fatalf("expected EVIDENCE_GATE refusal, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("tier one close with no verification rows: %v", err)
			}
			data, _, err := parseFrontmatterMustRead(taskPath)
			if err != nil {
				t.Fatal(err)
			}
			if got := stringField(data, "status"); got != "done" {
				t.Fatalf("tier one close status = %q, want done", got)
			}
		})
	}
}

func TestDefaultTierStatusDoneStillRefuses(t *testing.T) {
	vault := v7DispatchTestVault(t)
	if err := newV7Task(Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Ceremonial close"}); err != nil {
		t.Fatal(err)
	}
	err := statusV7Cmd(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "status": "done"})
	if err == nil || !strings.Contains(err.Error(), "cannot set done directly") {
		t.Fatalf("expected default done refusal, got %v", err)
	}
}

func TestTierDerivesServeAndReviewerDefaultsWhileWorkflowKeysWin(t *testing.T) {
	vault := pickupV7TestVault(t)
	if err := writeText(workflowPath(vault), defaultWorkflowMarkdown()); err != nil {
		t.Fatal(err)
	}
	if _, err := setProjectLocalConfigWithReadback(vault, "tier", 1); err != nil {
		t.Fatal(err)
	}
	wf, err := loadWorkflow(vault)
	if err != nil {
		t.Fatal(err)
	}
	if wf.Data.Runtime.Serve.Enabled || wf.Data.Reviewer.Enabled {
		t.Fatalf("tier one defaults remained enabled: serve=%v reviewer=%v", wf.Data.Runtime.Serve.Enabled, wf.Data.Reviewer.Enabled)
	}

	for _, tier := range []int{3, 4} {
		if _, err := setProjectLocalConfigWithReadback(vault, "tier", tier); err != nil {
			t.Fatal(err)
		}
		wf, err = loadWorkflow(vault)
		if err != nil {
			t.Fatal(err)
		}
		wantEnabled := tier >= 4
		if wf.Data.Runtime.Serve.Enabled != wantEnabled || wf.Data.Reviewer.Enabled != wantEnabled {
			t.Fatalf("tier %d defaults = serve=%v reviewer=%v, want enabled=%v", tier, wf.Data.Runtime.Serve.Enabled, wf.Data.Reviewer.Enabled, wantEnabled)
		}
	}

	data, body, err := parseFrontmatter(defaultWorkflowMarkdown())
	if err != nil {
		t.Fatal(err)
	}
	data["runtime"].(map[string]any)["serve"].(map[string]any)["enabled"] = false
	data["reviewer"].(map[string]any)["enabled"] = false
	content, err := serializeDocument(data, body, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeText(workflowPath(vault), content); err != nil {
		t.Fatal(err)
	}
	if _, err := setProjectLocalConfigWithReadback(vault, "tier", 1); err != nil {
		t.Fatal(err)
	}
	wf, err = loadWorkflow(vault)
	if err != nil {
		t.Fatal(err)
	}
	if wf.Data.Runtime.Serve.Enabled || wf.Data.Reviewer.Enabled {
		t.Fatalf("explicit false workflow keys lost: serve=%v reviewer=%v", wf.Data.Runtime.Serve.Enabled, wf.Data.Reviewer.Enabled)
	}
}

func TestTierOneValidateHasNoDispatchabilityErrorsForPlaceholder(t *testing.T) {
	vault := v7DispatchTestVault(t)
	if _, err := setProjectLocalConfigWithReadback(vault, "tier", 1); err != nil {
		t.Fatal(err)
	}
	if err := newV7Task(Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Tier one placeholder", "status": "ready"}); err != nil {
		t.Fatal(err)
	}
	issues := validateV7DispatchableTasks(vault)
	if issuesContainCode(issues, "TASK_NOT_DISPATCHABLE") {
		t.Fatalf("tier one validation reported dispatchability error: %#v", issues)
	}
}

func TestFactoryOperationsReportsTier(t *testing.T) {
	vault := pickupV7TestVault(t)
	if _, err := setProjectLocalConfigWithReadback(vault, "tier", 1); err != nil {
		t.Fatal(err)
	}
	projection := composeFactoryOperations(factoryOperationsFacts{VaultPath: vault})
	if projection.Project.Tier != 1 {
		t.Fatalf("factory operations tier = %d, want 1", projection.Project.Tier)
	}
}
