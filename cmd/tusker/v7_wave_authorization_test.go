package main

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestWavePreflight(t *testing.T) {
	vault := authorizedWaveTestVault(t)
	before := snapshotDeliveryRecords(t, vault)
	_, wave, idx, err := loadWaveAuthorizationTarget(Args{"vault": vault, "_pos0": "W-0001"})
	if err != nil {
		t.Fatal(err)
	}
	green := wavePreflightEnvironment{true, true, true, true, true, true, true, true, true}
	report := buildWavePreflight(vault, idx, wave, green)
	if !report.OK || !report.ReadOnly || len(report.Frontiers) != 2 || len(report.Artifacts) != 2 {
		t.Fatalf("unexpected green preflight: %#v", report)
	}
	if after := snapshotDeliveryRecords(t, vault); !mapsEqualString(before, after) {
		t.Fatal("preflight mutated records")
	}

	cases := []struct {
		name   string
		mutate func(*wavePreflightEnvironment)
		want   string
	}{
		{"project", func(e *wavePreflightEnvironment) { e.ProjectRegistered = false }, "project"},
		{"daemon", func(e *wavePreflightEnvironment) { e.DaemonAlive = false }, "daemon"},
		{"runner", func(e *wavePreflightEnvironment) { e.RunnerCompatible = false }, "runner"},
		{"approval", func(e *wavePreflightEnvironment) { e.ApprovalFree = false }, "approval"},
		{"workspace", func(e *wavePreflightEnvironment) { e.IsolatedWorkspace = false }, "workspace"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := green
			tc.mutate(&env)
			got := buildWavePreflight(vault, idx, wave, env)
			if got.OK || !strings.Contains(strings.Join(got.Blockers, " "), tc.want) {
				t.Fatalf("missing %s blocker: %#v", tc.want, got.Blockers)
			}
		})
	}

	brokenIdx := idx
	brokenIdx.Tasks = cloneNoteMap(idx.Tasks)
	broken := brokenIdx.Tasks["APP-T-0001"]
	broken.Data = cloneMap(broken.Data)
	delete(broken.Data, "artifact_contract")
	brokenIdx.Tasks["APP-T-0001"] = broken
	if got := buildWavePreflight(vault, brokenIdx, wave, green); got.OK || !hasWaveBlocker(got.Blockers, "artifact contract") {
		t.Fatalf("missing artifact blocker: %#v", got.Blockers)
	}
	broken = brokenIdx.Tasks["APP-T-0001"]
	broken.Data["artifact_contract"] = idx.Tasks["APP-T-0001"].Data["artifact_contract"]
	broken.Data["dependencies"] = []string{"APP-T-0002:hard"}
	brokenIdx.Tasks["APP-T-0001"] = broken
	if got := buildWavePreflight(vault, brokenIdx, wave, green); got.OK || !hasWaveBlocker(got.Blockers, "cycle") {
		t.Fatalf("missing cycle blocker: %#v", got.Blockers)
	}
}

func TestWaveArm(t *testing.T) {
	vault := authorizedWaveTestVault(t)
	unrelatedPath := filepath.Join(vault, "work", "tasks", "APP-T-0003.md")
	unrelatedData, unrelatedBody, err := parseFrontmatterMustRead(filepath.Join(vault, "work", "tasks", "APP-T-0001.md"))
	if err != nil {
		t.Fatal(err)
	}
	unrelatedData = cloneMap(unrelatedData)
	unrelatedData["id"], unrelatedData["title"], unrelatedData["status"], unrelatedData["readiness"] = "APP-T-0003", "Unrelated ready work", "ready", "ready"
	delete(unrelatedData, "wave")
	unrelatedData["state_rev"] = v7StateRev(unrelatedData, unrelatedBody)
	unrelatedContent, err := serializeDocument(unrelatedData, unrelatedBody, v7FrontmatterOrder["task"])
	if err != nil {
		t.Fatal(err)
	}
	if err := writeText(unrelatedPath, unrelatedContent); err != nil {
		t.Fatal(err)
	}
	args := Args{"vault": vault, "_pos0": "W-0001", "by": "human:test", "quiet": "true"}
	green := greenWaveEnvironment()
	before := snapshotDeliveryRecords(t, vault)
	failing := cloneArgs(args)
	failing["fail-after-first-write"] = "true"
	if err := mutateWaveAuthorization(failing, "armed", &green); err == nil {
		t.Fatal("expected forced arm failure")
	}
	if after := snapshotDeliveryRecords(t, vault); !mapsEqualString(before, after) {
		t.Fatal("arm failure did not roll back")
	}

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, actor := range []string{"human:one", "human:two"} {
		wg.Add(1)
		go func(actor string) {
			defer wg.Done()
			call := cloneArgs(args)
			call["by"] = actor
			errs <- mutateWaveAuthorization(call, "armed", &green)
		}(actor)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	waveData, _, err := parseFrontmatterMustRead(filepath.Join(vault, "work", "waves", "W-0001.md"))
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "armed", stringField(waveData, "authorization"), "authorization")
	if stringField(waveData, "authorization_fingerprint") == "" || stringField(waveData, "authorized_by") == "" {
		t.Fatal("arm omitted durable identity")
	}
	for _, id := range []string{"APP-T-0001", "APP-T-0002"} {
		data, _, e := parseFrontmatterMustRead(filepath.Join(vault, "work", "tasks", id+".md"))
		if e != nil {
			t.Fatal(e)
		}
		assertEqual(t, "ready", stringField(data, "status"), id+" promoted")
	}
	if got := mustReadIndexTest(t, unrelatedPath); got != unrelatedContent {
		t.Fatal("arm changed unrelated ready task")
	}
}

func TestWavePause(t *testing.T) {
	vault := authorizedWaveTestVault(t)
	armWaveForTest(t, vault)
	args := Args{"vault": vault, "_pos0": "W-0001", "by": "human:test", "reason": "maintenance", "quiet": "true"}
	if err := waveV7PauseCmd(args); err != nil {
		t.Fatal(err)
	}
	if err := waveV7PauseCmd(args); err != nil {
		t.Fatal("pause not idempotent:", err)
	}
	idx, _ := loadV7Index(vault)
	task := idx.Tasks["APP-T-0001"]
	blockers := v7TaskDispatchBlockers(vault, task)
	if !strings.Contains(strings.Join(blockers, " "), "paused") {
		t.Fatalf("pause did not stop future claims: %#v", blockers)
	}
	green := greenWaveEnvironment()
	if err := mutateWaveAuthorization(args, "armed", &green); err != nil {
		t.Fatal(err)
	}
	idx, _ = loadV7Index(vault)
	if got := stringField(waveAuthorizationProjection(vault, idx, idx.Waves["W-0001"]), "state"); got != "armed" {
		t.Fatalf("resume=%s", got)
	}
}

func TestWaveDisarm(t *testing.T) {
	vault := authorizedWaveTestVault(t)
	armWaveForTest(t, vault)
	args := Args{"vault": vault, "_pos0": "W-0001", "by": "human:test", "reason": "scope withdrawn", "quiet": "true"}
	if err := waveV7DisarmCmd(args); err != nil {
		t.Fatal(err)
	}
	if err := waveV7DisarmCmd(args); err != nil {
		t.Fatal("disarm not idempotent:", err)
	}
	data, _, _ := parseFrontmatterMustRead(filepath.Join(vault, "work", "waves", "W-0001.md"))
	assertEqual(t, "disarmed", stringField(data, "authorization"), "disarmed")
	assertEqual(t, "", stringField(data, "authorization_fingerprint"), "fingerprint cleared")
}

func TestWaveAuthorizationFingerprint(t *testing.T) {
	vault := authorizedWaveTestVault(t)
	armWaveForTest(t, vault)
	idx, _ := loadV7Index(vault)
	wave := idx.Waves["W-0001"]
	original, _ := waveMaterialFingerprint(vault, idx, wave)
	path := filepath.Join(vault, "work", "tasks", "APP-T-0001.md")
	data, body, err := parseFrontmatterMustRead(path)
	if err != nil {
		t.Fatal(err)
	}
	data["proof_status"] = "satisfied"
	body = strings.Replace(body, "| A1 | command: go test ./cmd/tusker -run TestDeliveryPlanSchemaRoundTrip -count=1 | pending |", "| A1 | command: go test ./cmd/tusker -run TestDeliveryPlanSchemaRoundTrip -count=1 | pass |", 1)
	data["state_rev"] = v7StateRev(data, body)
	content, _ := serializeDocument(data, body, v7FrontmatterOrder["task"])
	if err := writeText(path, content); err != nil {
		t.Fatal(err)
	}
	idx, _ = loadV7Index(vault)
	progress, _ := waveMaterialFingerprint(vault, idx, idx.Waves["W-0001"])
	assertEqual(t, original, progress, "proof progress fingerprint")
	taskChanged := idx
	taskChanged.Tasks = cloneNoteMap(idx.Tasks)
	changedTask := taskChanged.Tasks["APP-T-0001"]
	changedTask.Data = cloneMap(changedTask.Data)
	changedTask.Data["title"] = "Materially changed task intent"
	taskChanged.Tasks["APP-T-0001"] = changedTask
	assertWaveFingerprintChanged(t, vault, taskChanged, taskChanged.Waves["W-0001"], original, "task")
	memberWave := idx.Waves["W-0001"]
	memberWave.Data = cloneMap(memberWave.Data)
	memberWave.Data["members"] = []string{"APP-T-0001"}
	assertWaveFingerprintChanged(t, vault, idx, memberWave, original, "member set")
	gateChanged := idx
	gateChanged.Gates = map[string]Note{"APP-G-0001": {Data: map[string]any{"id": "APP-G-0001", "status": "open", "owner": "human:test", "blocking": true, "blocks": []string{"APP-T-0001"}, "action": "Supply a credential.", "verification": "Credential works.", "why_agent_cannot": "The credential is unavailable to agents."}}}
	assertWaveFingerprintChanged(t, vault, gateChanged, gateChanged.Waves["W-0001"], original, "gate")
	specPath := filepath.Join(v7RepoRoot(vault), "docs", "specs", "delivery.md")
	specBefore := mustReadIndexTest(t, specPath)
	if err := writeText(specPath, specBefore+"\nMaterial intent change.\n"); err != nil {
		t.Fatal(err)
	}
	assertWaveFingerprintChanged(t, vault, idx, idx.Waves["W-0001"], original, "spec")
	if err := writeText(specPath, specBefore); err != nil {
		t.Fatal(err)
	}
	data, body, _ = parseFrontmatterMustRead(path)
	data["dependencies"] = []string{"APP-T-0002:hard"}
	data["state_rev"] = v7StateRev(data, body)
	content, _ = serializeDocument(data, body, v7FrontmatterOrder["task"])
	_ = writeText(path, content)
	idx, _ = loadV7Index(vault)
	projection := waveAuthorizationProjection(vault, idx, idx.Waves["W-0001"])
	assertEqual(t, "stale", stringField(projection, "state"), "material change stales auth")
}

func assertWaveFingerprintChanged(t *testing.T, vault string, idx v7Index, wave Note, original, class string) {
	t.Helper()
	changed, _ := waveMaterialFingerprint(vault, idx, wave)
	if changed == original {
		t.Fatalf("%s change preserved authorization fingerprint", class)
	}
}

func TestWaveAuthorizationProjection(t *testing.T) {
	vault := authorizedWaveTestVault(t)
	idx, _ := loadV7Index(vault)
	wave := idx.Waves["W-0001"]
	payload := v7WavePayload(vault, idx, wave)
	auth := payload["authorization"].(map[string]any)
	assertEqual(t, "disarmed", stringField(auth, "state"), "CLI projection")
	if !strings.Contains(stringField(auth, "action"), "preflight") {
		t.Fatal("projection omitted action")
	}
	snap := serveSnapshot{project: RegisteredProject{VaultRoot: vault}, tasks: sortedV7Tasks(idx), waves: sortedV7Waves(idx), notesByID: map[string]Note{}}
	summary := serveWaveSummaryFor(snap, wave)
	assertEqual(t, "disarmed", stringField(summary.Authorization, "state"), "Serve/Mac projection")
}

func TestInteractiveExecutionContract(t *testing.T) {
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root = filepath.Clean(filepath.Join(root, "..", ".."))
	raw, err := os.ReadFile(filepath.Join(root, "skill", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Join(strings.Fields(string(raw)), " ")
	for _, want := range []string{"implements the requested work itself", "does not require daemon enablement or a daemon lifecycle claim", "Never start `tusker daemon run`"} {
		if !strings.Contains(text, want) {
			t.Fatalf("interactive contract missing %q", want)
		}
	}
}

func authorizedWaveTestVault(t *testing.T) string {
	t.Helper()
	vault := deliveryTestVault(t)
	plan := writeDeliveryTestPlan(t, vault, validDeliveryPlan())
	if err := deliveryImportCmd(Args{"vault": vault, "plan": plan, "wave": "Authorized", "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	return vault
}
func armWaveForTest(t *testing.T, vault string) {
	t.Helper()
	green := greenWaveEnvironment()
	if err := mutateWaveAuthorization(Args{"vault": vault, "_pos0": "W-0001", "by": "human:test", "quiet": "true"}, "armed", &green); err != nil {
		t.Fatal(err)
	}
}

func greenWaveEnvironment() wavePreflightEnvironment {
	return wavePreflightEnvironment{true, true, true, true, true, true, true, true, true}
}
func mapsEqualString(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}
func cloneArgs(in Args) Args {
	out := Args{}
	for k, v := range in {
		out[k] = v
	}
	return out
}
