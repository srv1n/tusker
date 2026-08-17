package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestBugfixArmedWaveHumanGateUsesBlocks(t *testing.T) {
	task := Note{Data: map[string]any{"id": "APP-T-0001", "readiness": "ready"}}
	idx := v7Index{Gates: map[string]Note{
		"APP-G-0001": {Data: map[string]any{"status": "open", "owner": "human:owner", "task": "APP-T-0001"}},
		"APP-G-0002": {Data: map[string]any{"status": "open", "owner": "human:owner", "blocks": []string{"APP-T-0001"}}},
	}}
	if !armedWaveTaskHumanBlocked(idx, task) {
		t.Fatal("open human gate blocking the task did not mark it human-blocked")
	}
	idx.Gates["APP-G-0002"] = Note{Data: map[string]any{"status": "open", "owner": "human:owner", "blocks": []string{"APP-T-0002"}}}
	if armedWaveTaskHumanBlocked(idx, task) {
		t.Fatal("gate for another task marked this task human-blocked")
	}
}

func TestBugfixWaveMaterialReadinessIgnoresBlockerRewording(t *testing.T) {
	readiness := func(message string) deliveryReview {
		report := wavePreflightReport{
			Authorization: "armed",
			Blockers:      []string{message},
			Checks:        map[string]bool{"specDag": false, "taskContracts": true, "artifacts": true},
		}
		env := wavePreflightEnvironment{
			ProjectRegistered: true, ProjectEnabled: true, ProjectHealthy: true,
			DaemonAlive: true, DaemonReconciling: true, RunnerCompatible: true,
			SkillCompatible: true, WorkflowCompatible: true, ApprovalFree: true,
			IsolatedWorkspace: true, IntegrationClean: true,
		}
		r := deliveryReview{Flow: deliveryReviewFlow{WaveID: "W-0001"}, Start: deliveryReviewStart{State: "held", PlanFingerprint: "sha256:test"}}
		deliveryReviewAddPreflightPhaseBlockers(&r, env, report, "APP", "W-0001")
		if err := finalizeDeliveryReviewReadiness(&r, "/vault", deliveryPlan{}, "plan.yaml"); err != nil {
			t.Fatal(err)
		}
		return r
	}
	one := readiness("member dependency graph contains a cycle")
	two := readiness("the wording changed but the spec DAG blocker key stayed the same")
	if one.ImportReady || two.ImportReady || one.PlanValid != two.PlanValid || one.Start.Readiness != two.Start.Readiness || !reflect.DeepEqual(one.Readiness, two.Readiness) {
		t.Fatalf("rewording changed readiness: first=%#v second=%#v", one, two)
	}
}

func TestBugfixDeliveryDoctorUsesTypedCodeAtBoundary(t *testing.T) {
	var first, second deliveryDoctorReport
	first.addContractIssueFromCode("artifact wording one", "validator", []string{"task"}, "ARTIFACT_INVALID")
	second.addContractIssueFromCode("completely reworded validator output", "validator", []string{"task"}, "ARTIFACT_INVALID")
	if len(first.Findings) != 1 || len(second.Findings) != 1 {
		t.Fatalf("unexpected findings: first=%#v second=%#v", first.Findings, second.Findings)
	}
	if first.Findings[0].Code != "ARTIFACT_INVALID" || second.Findings[0].Code != "ARTIFACT_INVALID" || first.Findings[0].Path != second.Findings[0].Path {
		t.Fatalf("typed code depended on message wording: first=%#v second=%#v", first.Findings[0], second.Findings[0])
	}
}

func TestBugfixDeliveryDoctorMapsBrokenPlanIssuesByMessage(t *testing.T) {
	vault := deliveryTestVault(t)
	plan := validDeliveryPlanV2()
	plan.Epic = "APP"
	plan.EpicContract = nil
	plan.ContextFingerprint = "sha256:" + strings.Repeat("0", 64)
	plan.Tasks[0].Verification = []deliveryVerification{{Covers: "A2", Check: "inspect it somehow"}}
	raw, err := yaml.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	report, err := deliveryPlanDoctorBytes(vault, "broken.yaml", raw)
	if err != nil {
		t.Fatal(err)
	}
	prepared, prepIssues := deliveryV2Prepare(vault, plan)
	if len(prepIssues) != 0 {
		t.Fatalf("test plan unexpectedly failed V2 preparation: %#v", prepIssues)
	}
	issues, _ := validateDeliveryPlan(vault, prepared)
	codes := deliveryDoctorValidatorCodes(vault, prepared, "base")
	if len(codes) != len(issues) {
		t.Fatalf("typed code count diverged from validator issues: codes=%#v issues=%#v", codes, issues)
	}
	want := map[string]string{
		"import: verification references unknown acceptance A2":                  "PROOF_ACCEPTANCE_UNKNOWN",
		"import: verification must use an exact command: or manual proof: check": "PROOF_UNSUPPORTED",
		"import: acceptance A1 has no mapped verification":                       "ACCEPTANCE_UNMAPPED",
	}
	for _, issue := range issues {
		code, ok := deliveryDoctorCodeMap(codes)[issue]
		if !ok || code != want[issue] {
			t.Fatalf("issue %q mapped to %q, want %q; codes=%#v", issue, code, want[issue], codes)
		}
	}
	for issue, code := range want {
		found := false
		for _, finding := range report.Findings {
			if finding.Message == issue {
				found = true
				if finding.Code != code {
					t.Fatalf("doctor finding %q has code %q, want %q", issue, finding.Code, code)
				}
			}
		}
		if !found {
			t.Fatalf("doctor omitted issue %q: %#v", issue, report.Findings)
		}
	}
}

func TestBugfixImplicitSingletonUsesGuardedWritePath(t *testing.T) {
	_, vault := newLandTestRepo(t, 1, "true")
	clearWaveBackpointer(t, vault, "APP-T-0001")
	if err := os.RemoveAll(filepath.Join(vault, "work", "waves")); err != nil {
		t.Fatal(err)
	}
	setSingletonPromotionMode(t, vault, scheduledPromotionStage)
	setWaveTaskState(t, vault, "APP-T-0001", "review", "review", "")
	locked := false
	previousObserver := v7MaterialEpochLockObserver
	v7MaterialEpochLockObserver = func() { locked = true }
	t.Cleanup(func() { v7MaterialEpochLockObserver = previousObserver })
	writes := 0
	previousRenameHook := deliveryImportBeforeRenameHook
	deliveryImportBeforeRenameHook = func(string) { writes++ }
	t.Cleanup(func() { deliveryImportBeforeRenameHook = previousRenameHook })
	unit, created, err := ensureV7ImplicitSingletonDeliveryUnit(vault, "APP-T-0001", Args{"vault": vault, "quiet": "true"})
	if err != nil || !created || !locked || writes < 2 {
		t.Fatalf("singleton did not use guarded epoch/CAS writes: unit=%q created=%v locked=%v writes=%d err=%v", unit, created, locked, writes, err)
	}
	idx, err := loadV7Index(vault)
	if err != nil {
		t.Fatal(err)
	}
	if stringField(idx.Tasks["APP-T-0001"].Data, "wave") != unit || !v7ImplicitDeliveryUnit(idx.Waves[unit]) {
		t.Fatalf("guarded creation did not commit wave/task binding: unit=%q task=%#v wave=%#v", unit, idx.Tasks["APP-T-0001"].Data, idx.Waves[unit].Data)
	}
}

func TestBugfixRebindUsesOriginallyLoadedRevision(t *testing.T) {
	_, vault := newLandTestRepo(t, 1, "true")
	clearWaveBackpointer(t, vault, "APP-T-0001")
	idx, err := loadV7Index(vault)
	if err != nil {
		t.Fatal(err)
	}
	task := idx.Tasks["APP-T-0001"]
	data, body, err := parseFrontmatterMustRead(task.AbsolutePath)
	if err != nil {
		t.Fatal(err)
	}
	data["updated_by"] = "concurrent-writer"
	if _, err := saveV7DocumentCAS(task.AbsolutePath, data, body, v7FrontmatterOrder["task"], stringField(task.Data, "state_rev")); err != nil {
		t.Fatal(err)
	}
	if err := bindV7TaskToDeliveryUnit(vault, task, "W-0099", Args{}); err == nil || !strings.Contains(err.Error(), "changed since it was loaded") {
		t.Fatalf("rebind accepted a changed task revision: %v", err)
	}
}

func TestBugfixUnknownWaveEnvironmentCheckIsVisible(t *testing.T) {
	message := waveEnvironmentBlocker("futureEnvironmentCheck")
	if strings.TrimSpace(message) == "" || !strings.Contains(message, "futureEnvironmentCheck") {
		t.Fatalf("unknown environment check disappeared: %q", message)
	}
}

// Contract-quality checks skip wave-scope blockers by type, not by prose: a
// cross-scope member's wave lives in its own vault and can never resolve from
// the local index (see TestCrossScopeDependencyEligibility). Membership
// blockers against locally resolvable waves must still surface.
func TestBugfixWaveTaskContractSkipsWaveScopeButKeepsMembership(t *testing.T) {
	vault := deliveryTestVault(t)
	blockers := waveTaskContractBlockers(vault, Note{Data: map[string]any{"id": "APP-T-0001", "wave": "W-9999"}})
	if hasWaveBlocker(blockers, "wave does not resolve") {
		t.Fatalf("wave-scope blocker leaked into contract check: %#v", blockers)
	}
	idx, err := loadV7Index(vault)
	if err != nil {
		t.Fatal(err)
	}
	var localWave string
	for id := range idx.Waves {
		localWave = id
		break
	}
	if localWave == "" {
		t.Skip("fixture has no local wave to test membership against")
	}
	blockers = waveTaskContractBlockers(vault, Note{Data: map[string]any{"id": "NOT-A-MEMBER", "wave": localWave}})
	if !hasWaveBlocker(blockers, "is not an authorized member of wave") {
		t.Fatalf("membership blocker was swallowed: %#v", blockers)
	}
}

func hasWaveBlocker(blockers []string, needles ...string) bool {
	for _, blocker := range blockers {
		for _, needle := range needles {
			if strings.Contains(strings.ToLower(blocker), strings.ToLower(needle)) {
				return true
			}
		}
	}
	return false
}
