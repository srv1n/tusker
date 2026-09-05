package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDeliveryStart(t *testing.T) {
	newPlan := func(t *testing.T, vault string) (deliveryPlanV2, string, string) {
		t.Helper()
		plan := validDeliveryPlanV2()
		plan.HumanGates = nil
		path := writeDeliveryV2TestPlan(t, vault, plan)
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return plan, path, deliveryFingerprint(raw)
	}
	start := func(vault, path, confirm string, env wavePreflightEnvironment) (deliveryStartResult, error) {
		return deliveryStart(Args{"vault": vault, "plan": path, "by": "human:test", "confirm": confirm, "quiet": "true"}, fixedWaveEnvironmentInspector(env))
	}

	t.Run("arms exact held V2 wave and reports delivery boundary", func(t *testing.T) {
		vault := deliveryTestVault(t)
		_, path, confirm := newPlan(t, vault)
		result, err := start(vault, path, confirm, greenWaveEnvironment())
		if err != nil {
			t.Fatal(err)
		}
		if result.WaveID != "W-0001" || result.AuthorizationFingerprint == "" || len(result.FirstFrontier) != 1 || result.ExpectedConcurrency != 1 || result.IntegrationLane != "integration/W-0001" || !strings.HasSuffix(result.StatusLink, "/ops#wave-W-0001") {
			t.Fatalf("start result omitted exact delivery boundary: %#v", result)
		}
		idx, err := loadV7Index(vault)
		if err != nil {
			t.Fatal(err)
		}
		wave := idx.Waves[result.WaveID]
		if stringField(wave.Data, "authorization") != "armed" || stringField(wave.Data, "authorization_fingerprint") != result.AuthorizationFingerprint || stringField(wave.Data, "authorized_by") != "human:test" {
			t.Fatalf("wave was not exact-human armed: %#v", wave.Data)
		}
		for _, id := range normalizeList(wave.Data["members"]) {
			if got := stringField(idx.Tasks[id].Data, "status"); got != "ready" {
				t.Fatalf("%s was not promoted only with the armed wave: %s", id, got)
			}
		}
	})

	t.Run("starts immediately after committing the tracked plan", func(t *testing.T) {
		vault := deliveryTestVault(t)
		repo := v7RepoRoot(vault)
		runGitDir(t, repo, "init", "-b", "main")
		runGitDir(t, repo, "config", "user.email", "test@example.com")
		runGitDir(t, repo, "config", "user.name", "Test User")
		runGitDir(t, repo, "add", ".")
		runGitDir(t, repo, "commit", "-m", "seed")

		plan := validDeliveryPlanV2()
		plan.HumanGates = nil
		scratchPath := writeDeliveryV2TestPlan(t, vault, plan)
		raw, err := os.ReadFile(scratchPath)
		if err != nil {
			t.Fatal(err)
		}
		trackedPath := filepath.Join(repo, "docs", "plans", "delivery.yaml")
		if err := writeText(trackedPath, string(raw)); err != nil {
			t.Fatal(err)
		}
		runGitDir(t, repo, "add", "docs/plans/delivery.yaml")
		runGitDir(t, repo, "commit", "-m", "track delivery plan")

		review, err := buildDeliveryReviewWithInspector(vault, trackedPath, fixedWaveEnvironmentInspector(greenWaveEnvironment()))
		if err != nil || !review.Ready {
			t.Fatalf("committed plan was not reviewable: %#v %v", review.Start, err)
		}
		result, err := start(vault, trackedPath, deliveryFingerprint(raw), greenWaveEnvironment())
		if err != nil {
			t.Fatal(err)
		}
		idx, err := loadV7Index(vault)
		if err != nil {
			t.Fatal(err)
		}
		if got, want := stringField(idx.Waves[result.WaveID].Data, "integration_base_sha"), strings.TrimSpace(gitDirOutput(t, repo, "rev-parse", "HEAD")); got != want {
			t.Fatalf("Start froze %q instead of committed plan base %q", got, want)
		}
	})

	t.Run("refuses missing actor or stale confirmation before mutation", func(t *testing.T) {
		vault := deliveryTestVault(t)
		_, path, confirm := newPlan(t, vault)
		before := snapshotDeliveryRecords(t, vault)
		_, err := deliveryStart(Args{"vault": vault, "plan": path, "confirm": confirm}, nil)
		if err == nil || errorToIssue(err).Code != errorInvalidArg {
			t.Fatalf("missing human actor was not refused as invalid actor input: %v", err)
		}
		_, err = deliveryStart(Args{"vault": vault, "plan": path, "by": "human:test", "confirm": "sha256:stale"}, nil)
		if err == nil || !strings.Contains(err.Error(), "confirmed plan fingerprint differs") {
			t.Fatalf("stale confirmation was accepted: %v", err)
		}
		assertEqual(t, before, snapshotDeliveryRecords(t, vault), "invalid Start must be read-only")
	})

	t.Run("refuses stale bounded context", func(t *testing.T) {
		vault := deliveryTestVault(t)
		_, path, confirm := newPlan(t, vault)
		if err := newV7Task(Args{"vault": vault, "quiet": "true", "epic": "APP", "id": "APP-T-0001", "title": "Context drift", "risk": "medium", "priority": "p1", "domains": "project", "spec-refs": "docs/specs/delivery.md", "v7": "true"}); err != nil {
			t.Fatal(err)
		}
		before := snapshotDeliveryRecords(t, vault)
		_, err := start(vault, path, confirm, greenWaveEnvironment())
		if err == nil || !strings.Contains(err.Error(), "planning context fingerprint differs") {
			t.Fatalf("stale context was accepted: %v", err)
		}
		assertEqual(t, before, snapshotDeliveryRecords(t, vault), "stale context must refuse before import")
	})

	t.Run("reviewed default SHA refuses advance before any mutation", func(t *testing.T) {
		repo, vault := newLandTestRepo(t, 1, "true")
		if err := writeText(repo+"/docs/specs/delivery.md", "# Delivery\n\n## Work streams\n"); err != nil {
			t.Fatal(err)
		}
		_, path, confirm := newPlan(t, vault)
		review, err := buildDeliveryReviewWithInspector(vault, path, fixedWaveEnvironmentInspector(greenWaveEnvironment()))
		if err != nil || !review.Ready {
			t.Fatalf("plan was not reviewable at base A: %#v %v", review, err)
		}
		if err := writeText(repo+"/advance.txt", "base B\n"); err != nil {
			t.Fatal(err)
		}
		runGitDir(t, repo, "add", "advance.txt")
		runGitDir(t, repo, "commit", "-m", "advance reviewed default")
		currentContext, contextErr := buildDeliveryPlanningContextForScope(vault, "docs/specs/delivery.md", "v2-delivery")
		if contextErr != nil {
			t.Fatal(contextErr)
		}
		if currentContext.ContextFingerprint == review.Start.ContextFingerprint {
			t.Fatalf("default advance did not change planning context: reviewed=%s current=%s default=%#v", review.Start.ContextFingerprint, currentContext.ContextFingerprint, currentContext.Policy.Branches)
		}
		before := snapshotDeliveryRecords(t, vault)
		refs := gitDirOutput(t, repo, "show-ref")
		_, err = start(vault, path, confirm, greenWaveEnvironment())
		if err == nil || !strings.Contains(err.Error(), "planning context fingerprint differs") {
			t.Fatalf("Start accepted consent reviewed at a different default SHA: %v", err)
		}
		assertEqual(t, before, snapshotDeliveryRecords(t, vault), "default-SHA drift must refuse before import")
		assertEqual(t, refs, gitDirOutput(t, repo, "show-ref"), "default-SHA refusal must not move refs")
		if fileExists(vault+"/work/waves/W-0002.md") || fileExists(vault+"/work/epics/VTP.md") {
			t.Fatal("default-SHA drift leaked imported records")
		}
	})

	t.Run("changed reviewed plan cannot reuse an older frozen scope", func(t *testing.T) {
		repo, vault := newLandTestRepo(t, 1, "true")
		if err := writeText(repo+"/docs/specs/delivery.md", "# Delivery\n\n## Work streams\n"); err != nil {
			t.Fatal(err)
		}
		planA, path, confirmA := newPlan(t, vault)
		env := greenWaveEnvironment()
		env.DaemonAlive = false
		if _, err := start(vault, path, confirmA, env); err == nil {
			t.Fatal("expected held import at reviewed base A")
		}
		idx, err := loadV7Index(vault)
		if err != nil {
			t.Fatal(err)
		}
		waveA := idx.Waves["W-0002"]
		frozenA := stringField(waveA.Data, "integration_base_sha")
		if frozenA == "" || stringField(waveA.Data, "authorization") != "disarmed" {
			t.Fatalf("base A did not remain held/frozen: %#v", waveA.Data)
		}

		if err := writeText(repo+"/advance-again.txt", "base B\n"); err != nil {
			t.Fatal(err)
		}
		runGitDir(t, repo, "add", "advance-again.txt")
		runGitDir(t, repo, "commit", "-m", "advance frozen delivery base")
		planB := planA
		planB.ContextFingerprint = ""
		planB.Summary = "A newly reviewed plan at base B must not inherit base A."
		path = writeDeliveryV2TestPlan(t, vault, planB)
		rawB, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatal(readErr)
		}
		confirmB := deliveryFingerprint(rawB)
		before := snapshotDeliveryRecords(t, vault)
		refs := gitDirOutput(t, repo, "show-ref")
		_, err = start(vault, path, confirmB, greenWaveEnvironment())
		if err == nil || !strings.Contains(err.Error(), "new plan scope/wave or perform an explicit controlled rebase") {
			t.Fatalf("changed B consent reused frozen A: %v", err)
		}
		assertEqual(t, before, snapshotDeliveryRecords(t, vault), "changed plan/frozen-scope refusal must precede mutation")
		assertEqual(t, refs, gitDirOutput(t, repo, "show-ref"), "frozen-scope refusal must not move refs")
		idx, _ = loadV7Index(vault)
		waveAfter := idx.Waves["W-0002"]
		if stringField(waveAfter.Data, "integration_base_sha") != frozenA || stringField(waveAfter.Data, "authorization") != "disarmed" {
			t.Fatalf("changed consent rewrote or armed frozen scope: %#v", waveAfter.Data)
		}
	})

	t.Run("preflight refusal leaves every imported member held and disarmed", func(t *testing.T) {
		vault := deliveryTestVault(t)
		_, path, confirm := newPlan(t, vault)
		env := greenWaveEnvironment()
		env.DaemonAlive = false
		_, err := start(vault, path, confirm, env)
		if err == nil || !strings.Contains(err.Error(), "managed daemon") {
			t.Fatalf("environmental preflight did not refuse with its first remedy: %v", err)
		}
		idx, err := loadV7Index(vault)
		if err != nil {
			t.Fatal(err)
		}
		wave := idx.Waves["W-0001"]
		if stringField(wave.Data, "authorization") != "disarmed" {
			t.Fatalf("failed Start armed a wave: %#v", wave.Data)
		}
		for _, id := range normalizeList(wave.Data["members"]) {
			if task := idx.Tasks[id]; stringField(task.Data, "status") != "backlog" || stringField(task.Data, "readiness") != "held" {
				t.Fatalf("preflight failure leaked runnable member %s: %#v", id, task.Data)
			}
		}
	})

	t.Run("import crash rolls back before authorization", func(t *testing.T) {
		vault := deliveryTestVault(t)
		_, path, confirm := newPlan(t, vault)
		before := snapshotDeliveryRecords(t, vault)
		_, err := deliveryStart(Args{
			"vault": vault, "plan": path, "by": "human:test", "confirm": confirm,
			"quiet": "true", "fail-after-first-write": "true",
		}, fixedWaveEnvironmentInspector(greenWaveEnvironment()))
		if err == nil || !strings.Contains(err.Error(), "forced delivery import write failure") {
			t.Fatalf("expected injected import crash, got %v", err)
		}
		assertEqual(t, before, snapshotDeliveryRecords(t, vault), "crashed Start import must roll back every record")
	})

	t.Run("exact replay converges", func(t *testing.T) {
		vault := deliveryTestVault(t)
		_, path, confirm := newPlan(t, vault)
		first, err := start(vault, path, confirm, greenWaveEnvironment())
		if err != nil {
			t.Fatal(err)
		}
		before := snapshotDeliveryRecords(t, vault)
		second, err := start(vault, path, confirm, greenWaveEnvironment())
		if err != nil {
			t.Fatal(err)
		}
		if !second.Replayed || first.WaveID != second.WaveID || first.AuthorizationFingerprint != second.AuthorizationFingerprint {
			t.Fatalf("replay did not converge: first=%#v second=%#v", first, second)
		}
		assertEqual(t, before, snapshotDeliveryRecords(t, vault), "exact Start replay must not duplicate or rewrite records")
	})

	t.Run("does not create or move integration refs", func(t *testing.T) {
		repo, vault := newLandTestRepo(t, 1, "true")
		if err := writeText(repo+"/docs/specs/delivery.md", "# Delivery\n\n## Work streams\n"); err != nil {
			t.Fatal(err)
		}
		// W-0001 belongs to the fixture's ordinary wave, so Start imports its
		// own W-0002. Its integration ref must remain absent after Start.
		_, path, confirm := newPlan(t, vault)
		before := gitDirOutput(t, repo, "show-ref")
		first, err := start(vault, path, confirm, greenWaveEnvironment())
		if err != nil {
			t.Fatal(err)
		}
		after := gitDirOutput(t, repo, "show-ref")
		assertEqual(t, before, after, "Start must not create or move Git refs")
		idx, err := loadV7Index(vault)
		if err != nil {
			t.Fatal(err)
		}
		wave := idx.Waves["W-0002"]
		if stringField(wave.Data, "integration_base_sha") == "" || gitRefExists(repo, "refs/heads/integration/W-0002") || !waveIntegrationBaseClean(vault, wave) {
			t.Fatalf("fresh absent integration lane was not authorized against its frozen base: %#v", wave.Data)
		}
		frozen := stringField(wave.Data, "integration_base_sha")
		replaySnapshot := snapshotDeliveryRecords(t, vault)
		second, err := start(vault, path, confirm, greenWaveEnvironment())
		if err != nil || !second.Replayed || second.AuthorizationFingerprint != first.AuthorizationFingerprint {
			t.Fatalf("unchanged reviewed base did not replay exactly: first=%#v second=%#v err=%v", first, second, err)
		}
		assertEqual(t, replaySnapshot, snapshotDeliveryRecords(t, vault), "Git-backed Start replay must converge")
		idx, _ = loadV7Index(vault)
		wave = idx.Waves["W-0002"]
		if stringField(wave.Data, "integration_base_sha") != frozen {
			t.Fatal("Start replay replaced the already imported frozen base")
		}
		branchName, branchBase, err := v7WorkspaceBranchForTask(vault, idx.Tasks[normalizeList(wave.Data["members"])[0]])
		if err != nil || branchName == "" || branchBase != stringField(wave.Data, "integration_base_sha") {
			t.Fatalf("fresh task workspace was not pinned to the absent lane's frozen base: %q %q %v", branchName, branchBase, err)
		}
		if err := ensureV7WaveIntegrationBranch(vault, wave); err != nil {
			t.Fatal(err)
		}
		if got, err := gitOutputTrim(repo, "rev-parse", "integration/W-0002"); err != nil || got != stringField(wave.Data, "integration_base_sha") {
			t.Fatalf("first serialized completion did not CAS-create the frozen integration base: %q %v", got, err)
		}
	})

	t.Run("material race refuses without losing gate or task update", func(t *testing.T) {
		vault := deliveryTestVault(t)
		_, path, confirm := newPlan(t, vault)
		originalHook := deliveryStartBeforeArm
		deliveryStartBeforeArm = func() {
			if err := newV7Gate(Args{
				"vault": vault, "quiet": "true", "blocks": "VTP-T-0001", "kind": "decision", "owner": "human:product",
				"action": "Resolve the conflicting product requirements.", "verification": "The governing specification records the selected requirement.",
				"why-agent-cannot": "The governing specification contains incompatible product requirements that only the product owner can resolve.",
				"suggestion":       "Choose the requirement that preserves the existing user workflow.", "covers": "A1",
			}); err != nil {
				t.Fatalf("race injection failed: %v", err)
			}
		}
		t.Cleanup(func() { deliveryStartBeforeArm = originalHook })
		_, err := start(vault, path, confirm, greenWaveEnvironment())
		if err == nil || !strings.Contains(err.Error(), "wave material changed after delivery preflight") {
			t.Fatalf("Start armed material that changed after preflight: %v", err)
		}
		idx, loadErr := loadV7Index(vault)
		if loadErr != nil {
			t.Fatal(loadErr)
		}
		wave := idx.Waves["W-0001"]
		task := idx.Tasks["VTP-T-0001"]
		gate := idx.Gates["VTP-G-0001"]
		if stringField(wave.Data, "authorization") != "disarmed" || stringField(task.Data, "status") == "ready" || stringField(task.Data, "readiness") == "ready" {
			t.Fatalf("material race leaked armed/ready state: wave=%#v task=%#v", wave.Data, task.Data)
		}
		if stringField(gate.Data, "status") != "open" || stringField(task.Data, "next_ref") != "VTP-G-0001" {
			t.Fatalf("material race lost gate/task update: gate=%#v task=%#v", gate.Data, task.Data)
		}
	})

	t.Run("reviewed import cannot adopt a later cooperative wave add", func(t *testing.T) {
		vault := deliveryTestVault(t)
		if err := newV7Task(Args{
			"vault": vault, "quiet": "true", "epic": "APP", "id": "APP-T-0001",
			"title": "Unrelated queued work", "risk": "low", "priority": "p2", "v7": "true",
		}); err != nil {
			t.Fatal(err)
		}
		_, path, confirm := newPlan(t, vault)
		originalHook := deliveryStartBeforeArm
		var originalMembers []string
		var addedTaskAfterRace string
		var waveAfterRace string
		deliveryStartBeforeArm = func() {
			idx, err := loadV7Index(vault)
			if err != nil {
				t.Fatalf("load imported wave before race: %v", err)
			}
			originalMembers = append([]string{}, normalizeList(idx.Waves["W-0001"].Data["members"])...)
			if err := waveV7AddCmd(Args{
				"vault": vault, "quiet": "true", "_pos0": "W-0001", "_pos1": "APP-T-0001", "by": "human:other",
			}); err != nil {
				t.Fatalf("cooperative wave-add race failed: %v", err)
			}
			addedTaskAfterRace = mustReadIndexTest(t, vault+"/work/tasks/APP-T-0001.md")
			waveAfterRace = mustReadIndexTest(t, vault+"/work/waves/W-0001.md")
		}
		t.Cleanup(func() { deliveryStartBeforeArm = originalHook })

		_, err := start(vault, path, confirm, greenWaveEnvironment())
		if err == nil || !strings.Contains(err.Error(), "wave material changed after delivery preflight") {
			t.Fatalf("Start adopted post-consent wave membership: %v", err)
		}
		idx, loadErr := loadV7Index(vault)
		if loadErr != nil {
			t.Fatal(loadErr)
		}
		wave := idx.Waves["W-0001"]
		if stringField(wave.Data, "authorization") != "disarmed" || !containsString(normalizeList(wave.Data["members"]), "APP-T-0001") {
			t.Fatalf("wave-add race was lost or armed: %#v", wave.Data)
		}
		if len(originalMembers) == 0 {
			t.Fatal("race hook did not capture the exact imported members")
		}
		for _, id := range originalMembers {
			task := idx.Tasks[id]
			if stringField(task.Data, "status") != "backlog" || stringField(task.Data, "readiness") != "held" {
				t.Fatalf("post-consent refusal did not restore imported member %s to held: %#v", id, task.Data)
			}
		}
		assertEqual(t, addedTaskAfterRace, mustReadIndexTest(t, vault+"/work/tasks/APP-T-0001.md"), "refusal must preserve the concurrently added task update")
		assertEqual(t, waveAfterRace, mustReadIndexTest(t, vault+"/work/waves/W-0001.md"), "refusal must preserve concurrent wave membership and metadata")
	})

	t.Run("early post-import refusal also restores readiness-only drift", func(t *testing.T) {
		vault := deliveryTestVault(t)
		if err := newV7Task(Args{
			"vault": vault, "quiet": "true", "epic": "APP", "id": "APP-T-0001",
			"title": "Unrelated early queued work", "risk": "low", "priority": "p2", "v7": "true",
		}); err != nil {
			t.Fatal(err)
		}
		_, path, confirm := newPlan(t, vault)
		originalHook := deliveryStartAfterImportUnlock
		var originalMembers []string
		var addedTaskAfterRace string
		var waveAfterRace string
		deliveryStartAfterImportUnlock = func() {
			idx, err := loadV7Index(vault)
			if err != nil {
				t.Fatalf("load imported wave before early race: %v", err)
			}
			originalMembers = append([]string{}, normalizeList(idx.Waves["W-0001"].Data["members"])...)
			if err := waveV7AddCmd(Args{
				"vault": vault, "quiet": "true", "_pos0": "W-0001", "_pos1": "APP-T-0001", "by": "human:other",
			}); err != nil {
				t.Fatalf("early cooperative wave-add race failed: %v", err)
			}
			addedTaskAfterRace = mustReadIndexTest(t, vault+"/work/tasks/APP-T-0001.md")
			waveAfterRace = mustReadIndexTest(t, vault+"/work/waves/W-0001.md")
		}
		t.Cleanup(func() { deliveryStartAfterImportUnlock = originalHook })

		_, err := start(vault, path, confirm, greenWaveEnvironment())
		if err == nil {
			t.Fatal("Start accepted an early post-import membership mutation")
		}
		idx, loadErr := loadV7Index(vault)
		if loadErr != nil {
			t.Fatal(loadErr)
		}
		wave := idx.Waves["W-0001"]
		if stringField(wave.Data, "authorization") != "disarmed" || !containsString(normalizeList(wave.Data["members"]), "APP-T-0001") {
			t.Fatalf("early refusal lost the wave-add race or armed: %#v", wave.Data)
		}
		if len(originalMembers) == 0 {
			t.Fatal("early race hook did not capture imported members")
		}
		for _, id := range originalMembers {
			task := idx.Tasks[id]
			if stringField(task.Data, "status") != "backlog" || stringField(task.Data, "readiness") != "held" {
				t.Fatalf("early refusal did not restore imported member %s to held: %#v", id, task.Data)
			}
		}
		assertEqual(t, addedTaskAfterRace, mustReadIndexTest(t, vault+"/work/tasks/APP-T-0001.md"), "early refusal must preserve the concurrently added task update")
		assertEqual(t, waveAfterRace, mustReadIndexTest(t, vault+"/work/waves/W-0001.md"), "early refusal must preserve concurrent wave membership and metadata")
	})

	t.Run("missing imported member does not bypass surviving readiness cleanup", func(t *testing.T) {
		vault := deliveryTestVault(t)
		if err := newV7Task(Args{
			"vault": vault, "quiet": "true", "epic": "APP", "id": "APP-T-0001",
			"title": "Unrelated queued work", "risk": "low", "priority": "p2", "v7": "true",
		}); err != nil {
			t.Fatal(err)
		}
		plan := validDeliveryPlanV2()
		plan.HumanGates = nil
		second := plan.Tasks[0]
		second.SourceKey = "verify"
		second.Title = "Verify V2"
		second.Outcome = "The V2 import is independently verified."
		second.Acceptance = []deliveryAcceptance{{ID: "A2", Outcome: "Verification covers the imported records."}}
		second.Verification = []deliveryVerification{{Covers: "A2", Check: "command: go test ./cmd/tusker -run '^TestDeliveryStart$' -count=1"}}
		second.Artifact = deliveryArtifactContract{
			Kind: "diff_summary", Path: "cmd/tusker/delivery_start_cmd_test.go",
			Summary: "Delivery Start refusal regression.", AcceptanceIDs: []string{"A2"},
		}
		plan.Tasks = append(plan.Tasks, second)
		path := writeDeliveryV2TestPlan(t, vault, plan)
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		confirm := deliveryFingerprint(raw)
		originalHook := deliveryStartAfterImportUnlock
		var originalMembers []string
		var missingMember string
		var addedTaskAfterRace string
		var waveAfterRace string
		deliveryStartAfterImportUnlock = func() {
			idx, err := loadV7Index(vault)
			if err != nil {
				t.Fatalf("load imported wave before missing-member race: %v", err)
			}
			originalMembers = sortedStrings(normalizeList(idx.Waves["W-0001"].Data["members"]))
			if len(originalMembers) < 2 {
				t.Fatalf("missing-member race requires two imported members: %v", originalMembers)
			}
			if err := waveV7AddCmd(Args{
				"vault": vault, "quiet": "true", "_pos0": "W-0001", "_pos1": "APP-T-0001", "by": "human:other",
			}); err != nil {
				t.Fatalf("cooperative wave-add before member removal failed: %v", err)
			}
			missingMember = originalMembers[len(originalMembers)-1]
			if err := os.Remove(idx.Tasks[missingMember].AbsolutePath); err != nil {
				t.Fatalf("remove imported member during Start: %v", err)
			}
			addedTaskAfterRace = mustReadIndexTest(t, vault+"/work/tasks/APP-T-0001.md")
			waveAfterRace = mustReadIndexTest(t, vault+"/work/waves/W-0001.md")
		}
		t.Cleanup(func() { deliveryStartAfterImportUnlock = originalHook })

		_, err = start(vault, path, confirm, greenWaveEnvironment())
		if err == nil || !strings.Contains(err.Error(), "explicit review or repair") || !strings.Contains(err.Error(), missingMember+" (missing)") {
			t.Fatalf("missing imported member did not produce a repair refusal: %v", err)
		}
		idx, loadErr := loadV7Index(vault)
		if loadErr != nil {
			t.Fatal(loadErr)
		}
		for _, id := range originalMembers {
			if id == missingMember {
				if _, exists := idx.Tasks[id]; exists {
					t.Fatalf("refusal recreated concurrently removed member %s", id)
				}
				continue
			}
			task := idx.Tasks[id]
			if stringField(task.Data, "status") != "backlog" || stringField(task.Data, "readiness") != "held" {
				t.Fatalf("missing member bypassed safe cleanup for survivor %s: %#v", id, task.Data)
			}
		}
		assertEqual(t, addedTaskAfterRace, mustReadIndexTest(t, vault+"/work/tasks/APP-T-0001.md"), "missing-member refusal must preserve the concurrently added task")
		assertEqual(t, waveAfterRace, mustReadIndexTest(t, vault+"/work/waves/W-0001.md"), "missing-member refusal must preserve concurrent wave membership")
	})

	t.Run("refusal preserves a concurrently progressed imported task", func(t *testing.T) {
		vault := deliveryTestVault(t)
		_, path, confirm := newPlan(t, vault)
		originalHook := deliveryStartBeforeArm
		var progressedTask string
		var waveAfterProgress string
		deliveryStartBeforeArm = func() {
			taskPath := vault + "/work/tasks/VTP-T-0001.md"
			_, _, err := mutateV7DocumentLocked(taskPath, v7FrontmatterOrder["task"], func(data map[string]any, body string) (map[string]any, string, bool, error) {
				data["status"] = "review"
				data["readiness"] = "waiting_on_review"
				data["next_owner"] = "reviewer:agent"
				data["next_source"] = "review_policy"
				data["next_ref"] = ""
				data["next_action"] = "Review the concurrent work."
				data["work_revision"] = 7
				data["updated_at"] = "2026-07-25T15:00:00Z"
				data["updated_by"] = "human:other"
				return data, body, true, nil
			})
			if err != nil {
				t.Fatalf("progress task during Start: %v", err)
			}
			progressedTask = mustReadIndexTest(t, taskPath)
			waveAfterProgress = mustReadIndexTest(t, vault+"/work/waves/W-0001.md")
		}
		t.Cleanup(func() { deliveryStartBeforeArm = originalHook })

		_, err := start(vault, path, confirm, greenWaveEnvironment())
		if err == nil || !strings.Contains(err.Error(), "task state/work/source/owner changed") || !strings.Contains(err.Error(), "explicit review or repair") {
			t.Fatalf("Start did not preserve/refuse a progressed imported task: %v", err)
		}
		assertEqual(t, progressedTask, mustReadIndexTest(t, vault+"/work/tasks/VTP-T-0001.md"), "refusal must not rewind a progressed imported task")
		assertEqual(t, waveAfterProgress, mustReadIndexTest(t, vault+"/work/waves/W-0001.md"), "task-progress refusal must not rewrite the wave")
		idx, loadErr := loadV7Index(vault)
		if loadErr != nil {
			t.Fatal(loadErr)
		}
		if task := idx.Tasks["VTP-T-0001"]; stringField(task.Data, "status") != "review" || stringField(task.Data, "next_owner") != "reviewer:agent" || intField(task.Data, "work_revision") != 7 {
			t.Fatalf("concurrent task progress was lost: %#v", task.Data)
		}
		if stringField(idx.Waves["W-0001"].Data, "authorization") != "disarmed" {
			t.Fatalf("task-progress refusal armed the wave: %#v", idx.Waves["W-0001"].Data)
		}
	})

	t.Run("refusal preserves another human's exact arm", func(t *testing.T) {
		vault := deliveryTestVault(t)
		_, path, confirm := newPlan(t, vault)
		originalHook := deliveryStartBeforeArm
		var otherArmWave string
		var otherArmTask string
		deliveryStartBeforeArm = func() {
			env := greenWaveEnvironment()
			if err := mutateWaveAuthorization(Args{
				"vault": vault, "_pos0": "W-0001", "by": "human:other", "quiet": "true",
			}, "armed", &env); err != nil {
				t.Fatalf("other-human arm during Start: %v", err)
			}
			otherArmWave = mustReadIndexTest(t, vault+"/work/waves/W-0001.md")
			otherArmTask = mustReadIndexTest(t, vault+"/work/tasks/VTP-T-0001.md")
		}
		t.Cleanup(func() { deliveryStartBeforeArm = originalHook })

		_, err := start(vault, path, confirm, greenWaveEnvironment())
		if err == nil || !strings.Contains(err.Error(), "authorization changed after reviewed import") || !strings.Contains(err.Error(), "explicit review or repair") {
			t.Fatalf("Start adopted or rewound another human's authorization: %v", err)
		}
		assertEqual(t, otherArmWave, mustReadIndexTest(t, vault+"/work/waves/W-0001.md"), "refusal must preserve another human's wave authorization")
		assertEqual(t, otherArmTask, mustReadIndexTest(t, vault+"/work/tasks/VTP-T-0001.md"), "refusal must preserve another human's promoted task")
		idx, loadErr := loadV7Index(vault)
		if loadErr != nil {
			t.Fatal(loadErr)
		}
		wave := idx.Waves["W-0001"]
		if stringField(wave.Data, "authorization") != "armed" || stringField(wave.Data, "authorized_by") != "human:other" {
			t.Fatalf("another human's arm was not preserved: %#v", wave.Data)
		}
	})

	t.Run("final lock rereads the reviewed plan bytes", func(t *testing.T) {
		vault := deliveryTestVault(t)
		_, path, confirm := newPlan(t, vault)
		originalHook := deliveryStartBeforeArm
		deliveryStartBeforeArm = func() {
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read plan for race injection: %v", err)
			}
			if err := writeText(path, string(raw)+"\n# changed after preflight\n"); err != nil {
				t.Fatalf("mutate plan for race injection: %v", err)
			}
		}
		t.Cleanup(func() { deliveryStartBeforeArm = originalHook })

		_, err := start(vault, path, confirm, greenWaveEnvironment())
		if err == nil || !strings.Contains(err.Error(), "confirmed plan fingerprint differs") {
			t.Fatalf("Start authorized plan bytes changed after preflight: %v", err)
		}
		idx, loadErr := loadV7Index(vault)
		if loadErr != nil {
			t.Fatal(loadErr)
		}
		wave := idx.Waves["W-0001"]
		task := idx.Tasks["VTP-T-0001"]
		if stringField(wave.Data, "authorization") != "disarmed" || stringField(task.Data, "status") == "ready" || stringField(task.Data, "readiness") == "ready" {
			t.Fatalf("plan-byte race leaked authorization: wave=%#v task=%#v", wave.Data, task.Data)
		}
	})

	t.Run("final lock reinspects integration state after default advances", func(t *testing.T) {
		repo, vault := newLandTestRepo(t, 1, "true")
		if err := writeText(repo+"/docs/specs/delivery.md", "# Delivery\n\n## Work streams\n"); err != nil {
			t.Fatal(err)
		}
		_, path, confirm := newPlan(t, vault)
		var integrationChecks []bool
		inspector := func(vaultPath string, wave Note) wavePreflightEnvironment {
			env := greenWaveEnvironment()
			env.IntegrationClean = waveIntegrationBaseClean(vaultPath, wave)
			integrationChecks = append(integrationChecks, env.IntegrationClean)
			return env
		}
		originalHook := deliveryStartBeforeArm
		deliveryStartBeforeArm = func() {
			if err := writeText(repo+"/advance-during-start.txt", "base B\n"); err != nil {
				t.Fatalf("write default advance: %v", err)
			}
			runGitDir(t, repo, "add", "advance-during-start.txt")
			runGitDir(t, repo, "commit", "-m", "advance default during Start")
		}
		t.Cleanup(func() { deliveryStartBeforeArm = originalHook })

		_, err := deliveryStart(Args{
			"vault": vault, "plan": path, "by": "human:test", "confirm": confirm, "quiet": "true",
		}, inspector)
		if err == nil {
			t.Fatal("Start armed frozen base A after the default advanced to B")
		}
		if len(integrationChecks) < 2 || !integrationChecks[0] || integrationChecks[len(integrationChecks)-1] {
			t.Fatalf("environment was not freshly inspected under the final lock: %#v err=%v", integrationChecks, err)
		}
		idx, loadErr := loadV7Index(vault)
		if loadErr != nil {
			t.Fatal(loadErr)
		}
		wave := idx.Waves["W-0002"]
		task := idx.Tasks["VTP-T-0001"]
		if stringField(wave.Data, "authorization") != "disarmed" || stringField(task.Data, "status") == "ready" || stringField(task.Data, "readiness") == "ready" || gitRefExists(repo, "refs/heads/integration/W-0002") {
			t.Fatalf("late default advance leaked authorization or integration ref: wave=%#v task=%#v", wave.Data, task.Data)
		}
	})

	t.Run("frozen integration base refuses default drift and divergent refs", func(t *testing.T) {
		prepareHeld := func(t *testing.T) (string, string, Note) {
			t.Helper()
			repo, vault := newLandTestRepo(t, 1, "true")
			if err := writeText(repo+"/docs/specs/delivery.md", "# Delivery\n\n## Work streams\n"); err != nil {
				t.Fatal(err)
			}
			_, path, confirm := newPlan(t, vault)
			env := greenWaveEnvironment()
			env.DaemonAlive = false
			if _, err := start(vault, path, confirm, env); err == nil {
				t.Fatal("expected held environmental preflight refusal")
			}
			idx, err := loadV7Index(vault)
			if err != nil {
				t.Fatal(err)
			}
			return repo, vault, idx.Waves["W-0002"]
		}

		t.Run("default drift", func(t *testing.T) {
			repo, vault, wave := prepareHeld(t)
			if err := writeText(repo+"/default-drift.txt", "drift\n"); err != nil {
				t.Fatal(err)
			}
			runGitDir(t, repo, "add", "default-drift.txt")
			runGitDir(t, repo, "commit", "-m", "advance default")
			if waveIntegrationBaseClean(vault, wave) {
				t.Fatal("absent integration ref accepted after configured default drifted")
			}
		})

		t.Run("existing divergent ref", func(t *testing.T) {
			repo, vault, wave := prepareHeld(t)
			if err := writeText(repo+"/divergent.txt", "divergent\n"); err != nil {
				t.Fatal(err)
			}
			runGitDir(t, repo, "add", "divergent.txt")
			runGitDir(t, repo, "commit", "-m", "divergent integration")
			runGitDir(t, repo, "branch", "integration/W-0002", "main")
			if waveIntegrationBaseClean(vault, wave) {
				t.Fatal("existing integration ref diverging from frozen base was accepted")
			}
		})
	})

	t.Run("authorization boundary leaves unrelated authority untouched", func(t *testing.T) {
		repo, vault := newLandTestRepo(t, 1, "true")
		if err := writeText(repo+"/docs/specs/delivery.md", "# Delivery\n\n## Work streams\n"); err != nil {
			t.Fatal(err)
		}
		if err := newV7Gate(Args{"vault": vault, "quiet": "true", "blocks": "APP-T-0001", "kind": "release", "owner": "human:release", "action": "Authorize the production release.", "verification": "Release authority records approval.", "why-agent-cannot": "Only the production release authority can deploy."}); err != nil {
			t.Fatal(err)
		}
		unrelatedTask := mustReadIndexTest(t, vault+"/work/tasks/APP-T-0001.md")
		gate := mustReadIndexTest(t, vault+"/work/gates/APP-G-0001.md")
		config := mustReadIndexTest(t, managedTuskerConfigPath(filepath.Join(repo, defaultRepoVaultDir)))
		refs := gitDirOutput(t, repo, "show-ref")
		_, path, confirm := newPlan(t, vault)
		if _, err := start(vault, path, confirm, greenWaveEnvironment()); err != nil {
			t.Fatal(err)
		}
		assertEqual(t, unrelatedTask, mustReadIndexTest(t, vault+"/work/tasks/APP-T-0001.md"), "Start must not include unrelated work")
		assertEqual(t, gate, mustReadIndexTest(t, vault+"/work/gates/APP-G-0001.md"), "Start must not satisfy or mutate gates")
		assertEqual(t, config, mustReadIndexTest(t, managedTuskerConfigPath(filepath.Join(repo, defaultRepoVaultDir))), "Start must not mutate runner or automation configuration")
		assertEqual(t, refs, gitDirOutput(t, repo, "show-ref"), "Start must not move any Git ref")
		idx, err := loadV7Index(vault)
		if err != nil {
			t.Fatal(err)
		}
		wave := idx.Waves["W-0002"]
		for _, forbidden := range []string{"release", "spend", "daemon", "runner_permissions"} {
			if _, found := wave.Data[forbidden]; found {
				t.Fatalf("Start minted forbidden authority %q: %#v", forbidden, wave.Data)
			}
		}
	})
}

func fixedWaveEnvironmentInspector(value wavePreflightEnvironment) wavePreflightEnvironmentInspector {
	return func(string, Note) wavePreflightEnvironment { return value }
}

func TestDeliveryStartCLIContract(t *testing.T) {
	command, args := parseCLI([]string{"tusker", "delivery", "start", "--plan", "plan.yaml", "--confirm", "sha256:exact", "--by", "human:test"})
	if command != "delivery start" || args.String("plan") != "plan.yaml" || args.String("confirm") != "sha256:exact" || args.String("by") != "human:test" {
		t.Fatalf("delivery start parsing drifted: %q %#v", command, args)
	}
	if !cliCommandMutatesVault("delivery start") {
		t.Fatal("delivery start must notify targeted reconciliation after its atomic mutation")
	}
	help := captureStdout(t, printV7Help)
	for _, want := range []string{"delivery review", "delivery start", "delivery doctor"} {
		if !strings.Contains(help, want) {
			t.Fatalf("V7 help omitted %q:\n%s", want, help)
		}
	}
}
