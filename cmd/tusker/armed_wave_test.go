package main

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestArmedWaveFrontier(t *testing.T) {
	vault, idx, wave := armedWaveTestFixture(t)
	snapshot := buildArmedWaveSnapshot(vault, idx, wave, nil, time.Unix(0, 0).UTC())
	assertEqual(t, []string{"APP-T-0001", "APP-T-0006"}, snapshot.Frontier, "complete initial frontier at cap")
}

func TestArmedWaveDrain(t *testing.T) {
	vault, idx, wave := armedWaveTestFixture(t)
	root := idx.Tasks["APP-T-0001"]
	root.Data["status"], root.Data["readiness"], root.Data["proof_status"] = "done", "done", "satisfied"
	idx.Tasks["APP-T-0001"] = root
	for _, id := range []string{"APP-T-0002", "APP-T-0003"} {
		task := idx.Tasks[id]
		task.Data["status"], task.Data["readiness"], task.Data["next_owner"] = "ready", "ready", "agent"
		idx.Tasks[id] = task
	}
	snapshot := buildArmedWaveSnapshot(vault, idx, wave, nil, time.Unix(0, 0).UTC())
	assertEqual(t, []string{"APP-T-0002", "APP-T-0003"}, snapshot.Frontier, "later parallel frontier")

	soft := idx.Tasks["APP-T-0002"]
	soft.Data["status"], soft.Data["proof_status"] = "review", "satisfied"
	idx.Tasks["APP-T-0002"] = soft
	next := idx.Tasks["APP-T-0004"]
	next.Data["status"], next.Data["readiness"], next.Data["next_owner"] = "ready", "ready", "agent"
	idx.Tasks["APP-T-0004"] = next
	snapshot = buildArmedWaveSnapshot(vault, idx, wave, nil, time.Unix(0, 0).UTC())
	if !containsString(snapshot.Frontier, "APP-T-0004") {
		t.Fatalf("proof-satisfied soft edge did not flow during review: %#v", snapshot)
	}
}

func TestArmedWaveFailureContainment(t *testing.T) {
	vault, idx, wave := armedWaveTestFixture(t)
	for _, id := range []string{"APP-T-0001", "APP-T-0002", "APP-T-0003", "APP-T-0004", "APP-T-0005"} {
		task := idx.Tasks[id]
		task.Data["status"], task.Data["readiness"], task.Data["next_owner"] = "ready", "ready", "agent"
		idx.Tasks[id] = task
	}
	runs := map[string]RunStatus{
		"APP-T-0002": {ItemID: "APP-T-0002", LeaseState: string(LeaseStateParkedNoProgress), LastError: "attempt cap reached"},
	}
	machineClosure := idx.Tasks["APP-T-0004"]
	machineClosure.Data["dependencies"] = []string{"APP-T-0002:hard"}
	idx.Tasks["APP-T-0004"] = machineClosure
	fingerprint, issues := waveMaterialFingerprint(vault, idx, wave)
	if len(issues) > 0 {
		t.Fatal(issues)
	}
	wave.Data["authorization_fingerprint"] = fingerprint
	human := idx.Tasks["APP-T-0003"]
	human.Data["readiness"], human.Data["next_owner"] = "waiting_on_human", "human"
	idx.Tasks["APP-T-0003"] = human
	snapshot := buildArmedWaveSnapshot(vault, idx, wave, runs, time.Unix(0, 0).UTC())
	states := armedWaveStateMap(snapshot)
	assertEqual(t, armedWaveMachineParked, states["APP-T-0002"], "exhausted root")
	assertEqual(t, armedWaveMachineParked, states["APP-T-0004"], "hard machine closure")
	assertEqual(t, armedWaveHumanBlocked, states["APP-T-0003"], "human root")
	assertEqual(t, armedWaveHumanBlocked, states["APP-T-0005"], "hard human closure")
	assertEqual(t, armedWaveRunnable, states["APP-T-0006"], "independent branch continues")
	wf := defaultWorkflow()
	wf.Retry.MaxAttempts = 3
	retry := (&Daemon{}).scheduleRetry(RunStatus{ItemID: "APP-T-0006", AttemptCount: 1}, wf, "transient transport failure")
	assertEqual(t, string(LeaseStateRetryQueued), retry.LeaseState, "retryable failure stays within policy")
}

func TestArmedWaveRestart(t *testing.T) {
	vault, idx, wave := armedWaveTestFixture(t)
	runs := map[string]RunStatus{"APP-T-0001": {ItemID: "APP-T-0001", LeaseState: string(LeaseStateRunning)}}
	before := buildArmedWaveSnapshot(vault, idx, wave, runs, time.Unix(0, 0).UTC())
	after := buildArmedWaveSnapshot(vault, idx, wave, runs, time.Unix(3600, 0).UTC())
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("restart changed reconstructed desired state:\nbefore=%#v\nafter=%#v", before, after)
	}
	if containsString(after.Frontier, "APP-T-0001") {
		t.Fatal("live claimed member was duplicated after restart")
	}
}

func TestArmedWaveProjection(t *testing.T) {
	vault, idx, wave := armedWaveTestFixture(t)
	runs := map[string]RunStatus{
		"APP-T-0001": {ItemID: "APP-T-0001", LeaseState: string(LeaseStateRunning)},
		"APP-T-0002": {ItemID: "APP-T-0002", LeaseState: string(LeaseStateParkedNoProgress)},
	}
	for id, status := range map[string]string{"APP-T-0003": "review", "APP-T-0004": "done"} {
		task := idx.Tasks[id]
		task.Data["status"], task.Data["readiness"] = status, status
		idx.Tasks[id] = task
	}
	human := idx.Tasks["APP-T-0005"]
	human.Data["readiness"] = "waiting_on_human"
	idx.Tasks["APP-T-0005"] = human
	wave.Data["landings"] = []map[string]any{{"task": "APP-T-0004", "gate_result": "pass"}}
	snapshot := buildArmedWaveSnapshot(vault, idx, wave, runs, time.Unix(0, 0).UTC())
	states := armedWaveStateMap(snapshot)
	for id, want := range map[string]string{
		"APP-T-0001": armedWaveRunning, "APP-T-0002": armedWaveMachineParked,
		"APP-T-0003": armedWaveReview, "APP-T-0004": armedWaveLanded,
		"APP-T-0005": armedWaveHumanBlocked, "APP-T-0006": armedWaveRunnable,
	} {
		assertEqual(t, want, states[id], "projection "+id)
	}
	wave.Data["authorization_fingerprint"] = "stale"
	stale := buildArmedWaveSnapshot(vault, idx, wave, nil, time.Unix(0, 0).UTC())
	assertEqual(t, armedWaveStaleAuthorization, armedWaveStateMap(stale)["APP-T-0006"], "stale authorization")
}

func TestArmedWaveProjectionSurfaces(t *testing.T) {
	vault, idx, wave := armedWaveTestFixture(t)
	if err := writeText(filepath.Join(vault, "WORKFLOW.md"), defaultWorkflowMarkdown()); err != nil {
		t.Fatal(err)
	}
	project := registerAutomationTestProject(t, vault)
	setAutomationV7TaskFields(t, vault, "APP-T-0003", map[string]any{"status": "review", "readiness": "waiting_on_review", "proof_status": "satisfied", "next_owner": "reviewer"})
	setAutomationV7TaskFields(t, vault, "APP-T-0004", map[string]any{"status": "done", "readiness": "done", "proof_status": "satisfied", "closed_at": "2026-07-14T10:00:00Z"})
	setAutomationV7TaskFields(t, vault, "APP-T-0005", map[string]any{"readiness": "waiting_on_human", "next_action": "human approval required"})
	writeArmedWaveTestFields(t, vault, map[string]any{"landings": []map[string]any{{"task": "APP-T-0004", "gate_result": "pass"}}})
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for _, run := range []RunStatus{
		{ProjectID: project.ProjectID, RecordID: "APP-T-0001", ItemID: "APP-T-0001", LeaseState: string(LeaseStateRunning), AttemptCount: 1},
		{ProjectID: project.ProjectID, RecordID: "APP-T-0002", ItemID: "APP-T-0002", LeaseState: string(LeaseStateParkedNoProgress), AttemptCount: 3, LastError: "attempt cap reached", Terminal: true},
	} {
		if err := store.UpsertRun(run); err != nil {
			t.Fatal(err)
		}
	}

	golden := map[string]armedWaveMember{
		"APP-T-0001": {ID: "APP-T-0001", State: armedWaveRunning, Reason: "live implementation or review lease"},
		"APP-T-0002": {ID: "APP-T-0002", State: armedWaveMachineParked, Reason: "attempt cap reached"},
		"APP-T-0003": {ID: "APP-T-0003", State: armedWaveReview, Reason: "awaiting objective review and landing"},
		"APP-T-0004": {ID: "APP-T-0004", State: armedWaveLanded, Reason: "landed on wave integration branch"},
		"APP-T-0005": {ID: "APP-T-0005", State: armedWaveHumanBlocked, Reason: "human approval required"},
		"APP-T-0006": {ID: "APP-T-0006", State: armedWaveRunnable, Reason: "eligible in current armed-wave frontier"},
	}
	assertArmedWavePublicSurfaceGolden(t, vault, store, golden)

	writeArmedWaveTestFields(t, vault, map[string]any{"authorization_fingerprint": "sha256:stale"})
	staleGolden := map[string]armedWaveMember{}
	for id := range golden {
		state, reason := armedWaveStaleAuthorization, "wave authorization is stale"
		if id == "APP-T-0004" {
			state, reason = armedWaveLanded, "landed on wave integration branch"
		}
		staleGolden[id] = armedWaveMember{ID: id, State: state, Reason: reason}
	}
	assertArmedWavePublicSurfaceGolden(t, vault, store, staleGolden)

	idx, _ = loadV7Index(vault)
	wave = idx.Waves["W-0001"]
	text := renderV7WaveShow(vault, idx, wave)
	for _, want := range []string{"## timeline", "APP-T-0001", armedWaveStaleAuthorization, "wave authorization is stale", armedWaveLanded} {
		if !strings.Contains(text, want) {
			t.Fatalf("wave timeline missing %q:\n%s", want, text)
		}
	}
}

func assertArmedWavePublicSurfaceGolden(t *testing.T, vault string, store *RuntimeStore, want map[string]armedWaveMember) {
	t.Helper()
	queueOutput := captureStdout(t, func() {
		if err := automationQueueCmd(Args{"vault": vault, "json": "true"}); err != nil {
			t.Fatal(err)
		}
	})
	var queuePayload struct {
		Queue automationQueueReport `json:"queue"`
	}
	if err := json.Unmarshal([]byte(queueOutput), &queuePayload); err != nil {
		t.Fatal(err)
	}
	queue := map[string]armedWaveMember{}
	for _, item := range append(queuePayload.Queue.Eligible, queuePayload.Queue.Blocked...) {
		if item.WaveID == "W-0001" {
			queue[item.ID] = armedWaveMember{ID: item.ID, State: item.ArmedWaveState, Reason: item.ArmedWaveReason}
		}
	}
	assertEqual(t, want, queue, "automation queue wave state/reason golden")

	statusOutput := captureStdout(t, func() {
		if err := automationStatusCmd(Args{"json": "true"}); err != nil {
			t.Fatal(err)
		}
	})
	var statusPayload struct {
		Status automationStatusReport `json:"status"`
	}
	if err := json.Unmarshal([]byte(statusOutput), &statusPayload); err != nil {
		t.Fatal(err)
	}
	assertEqual(t, want, armedWaveMemberMap(findArmedWaveSnapshot(t, statusPayload.Status.ArmedWaves, "W-0001")), "automation status wave state/reason golden")

	digest, err := buildTuskerDigest(vault, store, digestBuildOptions{Now: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, want, armedWaveMemberMap(findArmedWaveSnapshot(t, digest.ArmedWaves, "W-0001")), "digest wave state/reason golden")

	idx, err := loadV7Index(vault)
	if err != nil {
		t.Fatal(err)
	}
	timeline := map[string]armedWaveMember{}
	for _, row := range v7WavePayload(vault, idx, idx.Waves["W-0001"])["timeline"].([]map[string]any) {
		id := stringField(row, "task")
		timeline[id] = armedWaveMember{ID: id, State: stringField(row, "state"), Reason: stringField(row, "reason")}
	}
	assertEqual(t, want, timeline, "wave timeline state/reason golden")
}

func findArmedWaveSnapshot(t *testing.T, waves []armedWaveSnapshot, id string) armedWaveSnapshot {
	t.Helper()
	for _, wave := range waves {
		if wave.WaveID == id {
			return wave
		}
	}
	t.Fatalf("wave %s missing from projection: %#v", id, waves)
	return armedWaveSnapshot{}
}

func armedWaveMemberMap(snapshot armedWaveSnapshot) map[string]armedWaveMember {
	out := map[string]armedWaveMember{}
	for _, member := range snapshot.Members {
		out[member.ID] = member
	}
	return out
}

func writeArmedWaveTestFields(t *testing.T, vault string, fields map[string]any) {
	t.Helper()
	path := filepath.Join(vault, "work", "waves", "W-0001.md")
	data, body, err := parseFrontmatterMustRead(path)
	if err != nil {
		t.Fatal(err)
	}
	for key, value := range fields {
		data[key] = value
	}
	data["state_rev"] = v7StateRev(data, body)
	content, err := serializeDocument(data, body, v7FrontmatterOrder["wave"])
	if err != nil {
		t.Fatal(err)
	}
	if err := writeText(path, content); err != nil {
		t.Fatal(err)
	}
}

func TestArmedWaveWorkspaceIsolation(t *testing.T) {
	vault, idx, _ := armedWaveTestFixture(t)
	task := idx.Tasks["APP-T-0001"]
	wf := defaultWorkflow()
	wf.Workspace.Strategy = string(WorkspaceStrategyShared)
	if got := armedWaveDispatchBlocker(vault, task, wf, nil); !strings.Contains(got, "isolated") {
		t.Fatalf("shared workspace was not rejected: %q", got)
	}
}

func TestArmedWaveDelivery(t *testing.T) {
	existing := WorkspaceMetadata{ProjectID: "app", RecordID: "APP-T-0001", ItemID: "APP-T-0001", RepoRoot: "/repo", Strategy: string(WorkspaceStrategyWorktree)}
	err := validateWorkspaceMetadata(existing, WorkspacePrepareRequest{ProjectID: "app", RecordID: "APP-T-0002", ItemID: "APP-T-0002", RepoRoot: "/repo", Strategy: WorkspaceStrategyWorktree})
	if err == nil {
		t.Fatal("another task was allowed to absorb an attributable workspace")
	}
	vault := automationTestVault(t)
	mustRunPickupTest(t, Args{
		"vault": vault, "quiet": "true", "epic": "APP", "title": "Typed armed-wave review",
		"risk": "medium", "priority": "p0", "v7": "true",
	}, newV7Task)
	setAutomationV7TaskFields(t, vault, "APP-T-0001", map[string]any{
		"status":        "review",
		"wave":          "W-0001",
		"source_sha":    "abc123",
		"work_revision": 2,
	})
	note, err := resolveV7Note(vault, "APP-T-0001", "task")
	if err != nil {
		t.Fatal(err)
	}
	wfFile, err := loadWorkflow(vault)
	if err != nil {
		t.Fatal(err)
	}
	// Custom legacy reviewer choreography must be ignored even when it asks
	// the reviewer to close. The fixed typed-result contract is authoritative.
	wfFile.Data.Reviewer.Prompt = "Legacy reviewer closes with {{ reviewer.close_command }}"
	prompt, err := renderAttemptPrompt(newRegisteredProject(filepath.Dir(vault), vault), wfFile, note, t.TempDir(), 1, "attempt-review", runLaneReview, RunStatus{}, RunStatus{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"tusker land", "tusker close", "tusker status", "rework"} {
		if strings.Contains(prompt, forbidden) {
			t.Fatalf("reviewer prompt retained authority %q: %q", forbidden, prompt)
		}
	}
	if !strings.Contains(prompt, "tusker review submit") {
		t.Fatalf("reviewer prompt lacks typed submit contract: %q", prompt)
	}
	for _, flag := range []string{"--task-rev", "--source-sha", "--work-rev", "--proof-fingerprint", "--gate-fingerprint"} {
		if !strings.Contains(prompt, flag) {
			t.Fatalf("reviewer prompt lacks %s: %q", flag, prompt)
		}
	}
}

func TestArmedWaveLandingCache(t *testing.T) {
	repo := t.TempDir()
	gitDirOutput(t, repo, "init")
	gitDirOutput(t, repo, "config", "user.email", "test@example.com")
	gitDirOutput(t, repo, "config", "user.name", "Test")
	if err := writeText(filepath.Join(repo, "a.txt"), "a\n"); err != nil {
		t.Fatal(err)
	}
	gitDirOutput(t, repo, "add", "a.txt")
	gitDirOutput(t, repo, "commit", "-m", "base")
	fp := v7LandingGateFingerprint(repo, "task-batch:APP-T-0001@task/APP-T-0001", []string{"go test ./..."})
	if fp == "" {
		t.Fatal("landing fingerprint is empty")
	}
	vault := filepath.Join(repo, ".tusker")
	if err := writeV7LandingGateCache(vault, fp, []string{"go test ./..."}); err != nil {
		t.Fatal(err)
	}
	if !v7LandingGateCacheHit(vault, fp) {
		t.Fatal("durable merged-state validation cache missed")
	}
	originalProbe := landingToolchainProbe
	t.Cleanup(func() { landingToolchainProbe = originalProbe })
	landingToolchainProbe = func(string, []string) map[string]string { return map[string]string{"go": "go1"} }
	taskOne := v7LandingGateFingerprint(repo, "task-batch:APP-T-0001@task/APP-T-0001", []string{"go test ./..."})
	taskTwo := v7LandingGateFingerprint(repo, "task-batch:APP-T-0002@task/APP-T-0002", []string{"go test ./..."})
	if taskOne == taskTwo {
		t.Fatal("task/lane identity did not invalidate landing validation cache")
	}
	landingToolchainProbe = func(string, []string) map[string]string { return map[string]string{"go": "go2"} }
	toolchainTwo := v7LandingGateFingerprint(repo, "task-batch:APP-T-0001@task/APP-T-0001", []string{"go test ./..."})
	if taskOne == toolchainTwo {
		t.Fatal("toolchain change did not invalidate landing validation cache")
	}
	if err := writeText(filepath.Join(repo, "Makefile"), "build-go:\n\tgo build ./...\nvet:\n\tgo vet ./...\ntest:\n\tgo test ./...\n"); err != nil {
		t.Fatal(err)
	}
	toolchains := v7LandingToolchainFingerprints(repo, []string{"make build-go", "make vet", "make test"})
	if !strings.Contains(toolchains["go"], runtime.Version()) {
		t.Fatalf("make-backed Go validation did not bind the actual Go binary/version: %#v", toolchains)
	}
	if toolchains["make"] == "" {
		t.Fatalf("make-backed validation did not bind the make executable: %#v", toolchains)
	}
}

func armedWaveStateMap(snapshot armedWaveSnapshot) map[string]string {
	out := map[string]string{}
	for _, member := range snapshot.Members {
		out[member.ID] = member.State
	}
	return out
}

func armedWaveTestFixture(t *testing.T) (string, v7Index, Note) {
	t.Helper()
	vault := deliveryTestVault(t)
	plan := validDeliveryPlan()
	plan.Concurrency = 2
	base := plan.Tasks[0]
	plan.Tasks = []deliveryPlanTask{
		base,
		armedWavePlanTask(base, "parallel-a", []deliveryDependency{{Task: "schema", Kind: "hard"}}),
		armedWavePlanTask(base, "parallel-b", []deliveryDependency{{Task: "schema", Kind: "hard"}}),
		armedWavePlanTask(base, "soft-child", []deliveryDependency{{Task: "parallel-a", Kind: "soft"}}),
		armedWavePlanTask(base, "hard-child", []deliveryDependency{{Task: "parallel-b", Kind: "hard"}}),
		armedWavePlanTask(base, "independent", nil),
	}
	path := writeDeliveryTestPlan(t, vault, plan)
	if err := deliveryImportCmd(Args{"vault": vault, "plan": path, "wave": "Drain", "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	armWaveForTest(t, vault)
	idx, err := loadV7Index(vault)
	if err != nil {
		t.Fatal(err)
	}
	wave := idx.Waves["W-0001"]
	return vault, idx, wave
}

func armedWavePlanTask(base deliveryPlanTask, key string, deps []deliveryDependency) deliveryPlanTask {
	task := base
	task.SourceKey = key
	task.Title = key
	task.Dependencies = deps
	task.OwnedPaths = []string{"cmd/tusker/" + key + ".go"}
	task.Artifact.Path = task.OwnedPaths[0]
	return task
}
