package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestDemoCrossWaveAuthorization(t *testing.T) {
	bin := demoTestBinary(t)
	demoTestEnv(t, bin)
	repo := t.TempDir()

	if code := demoRunInner(t, "demo seed", Args{"repo": repo, "scenario": demoScenario}); code != demoExitOK {
		t.Fatalf("boundary 1 seed exit %d", code)
	}
	vault := filepath.Join(repo, ".tusker")
	manifest := demoManifestForTest(t, repo)
	if len(manifest.Waves) != 4 || len(manifest.Tasks) != 13 {
		t.Fatalf("boundary 1 seed mapping: %d waves %d tasks", len(manifest.Waves), len(manifest.Tasks))
	}
	waveID := manifest.Waves["follow-up"].WaveID
	if waveID == "" {
		t.Fatal("boundary 1: seed did not map the follow-up wave")
	}
	taskIDs := map[string]string{}
	for _, key := range []string{"c1", "c2", "c3", "c4", "a4", "b4"} {
		id := manifest.Tasks[key].TaskID
		if id == "" {
			t.Fatalf("boundary 1: seed did not map %s", key)
		}
		taskIDs[key] = id
	}
	assertCrossWaveContracts(t, vault, taskIDs["c1"], taskIDs["a4"], taskIDs["b4"], "boundary 1")

	store, err := OpenRuntimeStore(os.Getenv("TUSKER_STATE_ROOT"))
	if err != nil {
		t.Fatalf("boundary 1: open runtime store: %s", err.Error())
	}
	defer store.Close()
	projectID := firstNonEmpty(manifest.RuntimeProjectID, manifest.LocalProjectID)
	if projectID == "" {
		t.Fatal("boundary 1: seed did not register a runtime project")
	}
	server := newServeServer(vault, repo, defaultServeAddr, store, nil)

	directReview := func(boundary string) directWaveReview {
		t.Helper()
		review, err := buildDirectWaveReview(vault, store, projectID, waveID, nil)
		if err != nil {
			t.Fatalf("%s: wave review: %s", boundary, err.Error())
		}
		for _, blocker := range review.Blockers {
			if blocker.Code == "DEPENDENCY_CONTRACT_INVALID" || blocker.Code == "CONTRACT_FINGERPRINT_STALE" {
				t.Fatalf("%s: wave review reports %s: %s", boundary, blocker.Code, blocker.Reason)
			}
		}
		return review
	}
	serveReview := func(boundary string) directWaveReview {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/api/projects/"+projectID+"/waves/"+waveID+"/review", nil)
		rec := httptest.NewRecorder()
		server.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: serve review returned %d: %s", boundary, rec.Code, rec.Body.String())
		}
		var review directWaveReview
		if err := json.Unmarshal(rec.Body.Bytes(), &review); err != nil {
			t.Fatalf("%s: decode serve review: %s", boundary, err.Error())
		}
		return review
	}
	externalFact := func(review directWaveReview, taskID string) (directWaveExternalDependency, bool) {
		for _, fact := range review.ExternalDependencies {
			if fact.TaskID == taskID {
				return fact, true
			}
		}
		return directWaveExternalDependency{}, false
	}
	member := func(review directWaveReview, taskID string) (directWaveReviewMember, bool) {
		for _, m := range review.Members {
			if m.TaskID == taskID {
				return m, true
			}
		}
		return directWaveReviewMember{}, false
	}
	waveStartEnabled := func(review directWaveReview) bool {
		for _, control := range review.Controls {
			if control.Action == "wave start" && control.Scope == waveID && control.Enabled {
				return true
			}
		}
		return false
	}
	taskStatus := func(boundary, taskID string) string {
		t.Helper()
		note, err := resolveV7Note(vault, taskID, "task")
		if err != nil {
			t.Fatalf("%s: resolve %s: %s", boundary, taskID, err.Error())
		}
		return strings.ToLower(strings.TrimSpace(stringField(note.Data, "status")))
	}

	review := directReview("boundary 1")
	if review.Authorization != "inert" {
		t.Fatalf("boundary 1: fresh follow-up authorization %q, want inert", review.Authorization)
	}

	if code := demoRunInner(t, "demo run", Args{"repo": repo, "waves": "alpha", "fast": "true"}); code != demoExitOK {
		t.Fatalf("boundary 2 alpha run exit %d", code)
	}
	if got := taskStatus("boundary 2", taskIDs["a4"]); got != "done" {
		t.Fatalf("boundary 2: a4 status %q, want done", got)
	}
	if got := taskStatus("boundary 2", taskIDs["b4"]); got == "done" {
		t.Fatal("boundary 2: b4 finished before its wave ran")
	}
	review = directReview("boundary 2")
	served := serveReview("boundary 2")
	for _, rv := range []struct {
		name   string
		review directWaveReview
	}{{"direct", review}, {"serve", served}} {
		a4, ok := externalFact(rv.review, taskIDs["a4"])
		if !ok || a4.Classification != "external" || a4.Status != "done" {
			t.Fatalf("boundary 2 %s: a4 fact %#v, want external/done", rv.name, a4)
		}
		if a4.Title == "" || a4.Title != manifest.Tasks["a4"].Title {
			t.Fatalf("boundary 2 %s: a4 title %q, want %q", rv.name, a4.Title, manifest.Tasks["a4"].Title)
		}
		if a4.WaveID != manifest.Waves["alpha"].WaveID || a4.WaveTitle == "" {
			t.Fatalf("boundary 2 %s: a4 wave facts %#v, want wave %s with a title", rv.name, a4, manifest.Waves["alpha"].WaveID)
		}
		b4, ok := externalFact(rv.review, taskIDs["b4"])
		if !ok || b4.Classification != "external" || b4.Status == "done" || b4.Status == "" {
			t.Fatalf("boundary 2 %s: b4 fact %#v, want external/unfinished", rv.name, b4)
		}
		if b4.Title != manifest.Tasks["b4"].Title || b4.WaveID != manifest.Waves["beta"].WaveID || b4.WaveTitle == "" {
			t.Fatalf("boundary 2 %s: b4 wave facts %#v, want beta title/wave", rv.name, b4)
		}
		c1, ok := member(rv.review, taskIDs["c1"])
		if !ok || c1.WaitingReason != "waiting for dependency "+taskIDs["b4"] {
			t.Fatalf("boundary 2 %s: c1 wait %q, want waiting for dependency %s", rv.name, c1.WaitingReason, taskIDs["b4"])
		}
		if !waveStartEnabled(rv.review) {
			t.Fatalf("boundary 2 %s: wave start control is not enabled before authorization", rv.name)
		}
	}
	if review.Authorization != "inert" || served.Authorization != "inert" {
		t.Fatalf("boundary 2: authorization direct=%q serve=%q, want inert", review.Authorization, served.Authorization)
	}

	started, err := directWaveStart(vault, store, waveID, "human:test-operator")
	if err != nil {
		t.Fatalf("boundary 3: wave start refused: %s", err.Error())
	}
	if started.Authorization != "authorized" {
		t.Fatalf("boundary 3: authorization %q, want authorized", started.Authorization)
	}
	if len(started.QueuedTaskIDs) != 0 {
		t.Fatalf("boundary 3: queued %v while Beta unfinished, want none", started.QueuedTaskIDs)
	}
	review = directReview("boundary 3")
	served = serveReview("boundary 3")
	for _, rv := range []struct {
		name   string
		review directWaveReview
	}{{"direct", review}, {"serve", served}} {
		if rv.review.Authorization != "authorized" {
			t.Fatalf("boundary 3 %s: authorization %q, want authorized", rv.name, rv.review.Authorization)
		}
		c1, ok := member(rv.review, taskIDs["c1"])
		if !ok || c1.WaitingReason != "waiting for dependency "+taskIDs["b4"] {
			t.Fatalf("boundary 3 %s: c1 wait %q, want waiting for dependency %s", rv.name, c1.WaitingReason, taskIDs["b4"])
		}
		if waveStartEnabled(rv.review) {
			t.Fatalf("boundary 3 %s: an enabled wave start control remains after authorization", rv.name)
		}
	}

	if code := demoRunInner(t, "demo run", Args{"repo": repo, "waves": "beta", "fast": "true"}); code != demoExitOK {
		t.Fatalf("boundary 4 beta run exit %d", code)
	}
	for _, key := range []string{"a4", "b4"} {
		if got := taskStatus("boundary 4", taskIDs[key]); got != "done" {
			t.Fatalf("boundary 4: %s status %q, want done", key, got)
		}
	}
	wave, err := resolveV7Note(vault, waveID, "wave")
	if err != nil {
		t.Fatalf("boundary 4: resolve wave: %s", err.Error())
	}
	armedFingerprint := stringField(wave.Data, "authorization_fingerprint")
	armedAt := stringField(wave.Data, "authorized_at")
	if armedFingerprint == "" || armedAt == "" || stringField(wave.Data, "authorization") != "armed" {
		t.Fatalf("boundary 4: wave lost its armed authorization: %#v", wave.Data)
	}
	c1Note, err := resolveV7Note(vault, taskIDs["c1"], "task")
	if err != nil {
		t.Fatalf("boundary 4: resolve c1: %s", err.Error())
	}
	c1Record := trackerRecordID(c1Note)
	queued, err := queueAuthorizedWaveFrontier(vault, store, projectID, waveID, time.Now().UTC())
	if err != nil {
		t.Fatalf("boundary 4: continuation refused: %s", err.Error())
	}
	if len(queued) != 1 || queued[0] != taskIDs["c1"] {
		t.Fatalf("boundary 4: continuation queued %v, want only %s", queued, taskIDs["c1"])
	}
	countActiveC1Directives := func() int {
		directives, err := store.ListActiveRunDirectives(projectID, time.Now().UTC())
		if err != nil {
			t.Fatalf("boundary 4: list directives: %s", err.Error())
		}
		count := 0
		for _, directive := range directives {
			if directive.RecordID != c1Record || directive.State != "queued" {
				continue
			}
			if directive.WaveID != waveID || directive.AuthorizationFingerprint != armedFingerprint || directive.WaveAuthorizedAt != armedAt {
				t.Fatalf("boundary 4: c1 directive is not bound to the stored authorization: %#v", directive)
			}
			count++
		}
		return count
	}
	if got := countActiveC1Directives(); got != 1 {
		t.Fatalf("boundary 4: %d active c1 directives, want exactly one", got)
	}
	again, err := queueAuthorizedWaveFrontier(vault, store, projectID, waveID, time.Now().UTC())
	if err != nil {
		t.Fatalf("boundary 4: second continuation errored: %s", err.Error())
	}
	if len(again) != 0 {
		t.Fatalf("boundary 4: second continuation queued %v, want nothing", again)
	}
	if got := countActiveC1Directives(); got != 1 {
		t.Fatalf("boundary 4: idempotent continuation left %d active c1 admissions, want one", got)
	}

	if code := demoRunInner(t, "demo run", Args{"repo": repo, "waves": "follow-up", "fast": "true"}); code != demoExitOK {
		t.Fatalf("boundary 5: follow-up run exit %d", code)
	}
	manifest = demoManifestForTest(t, repo)
	for _, key := range []string{"c1", "c2", "c3", "c4"} {
		if got := taskStatus("boundary 5", taskIDs[key]); got != "done" {
			t.Fatalf("boundary 5: %s status %q, want done", key, got)
		}
	}
	finished := map[string]time.Time{}
	doneCount := map[string]int{}
	for _, run := range manifest.Runs {
		for _, interval := range run.Intervals {
			if interval.Outcome != "done" {
				continue
			}
			for key, id := range taskIDs {
				if interval.TaskID != id {
					continue
				}
				doneCount[key]++
				end, err := time.Parse(time.RFC3339Nano, interval.Finished)
				if err != nil {
					t.Fatalf("boundary 5: interval for %s has unparsable finish %q", key, interval.Finished)
				}
				if end.After(finished[key]) {
					finished[key] = end
				}
			}
		}
	}
	startedAt := map[string]time.Time{}
	for _, run := range manifest.Runs {
		for _, interval := range run.Intervals {
			for key, id := range taskIDs {
				if interval.TaskID != id {
					continue
				}
				start, err := time.Parse(time.RFC3339Nano, interval.Started)
				if err != nil {
					t.Fatalf("boundary 5: interval for %s has unparsable start %q", key, interval.Started)
				}
				if startedAt[key].IsZero() || start.Before(startedAt[key]) {
					startedAt[key] = start
				}
			}
		}
	}
	for _, key := range []string{"c1", "c2", "c3", "c4"} {
		if doneCount[key] != 1 {
			t.Fatalf("boundary 5: %s has %d accepted completions, want exactly one", key, doneCount[key])
		}
	}
	for _, key := range []string{"c2", "c3"} {
		if startedAt[key].Before(finished["c1"]) {
			t.Fatalf("boundary 5: %s started %s before c1 finished %s", key, startedAt[key], finished["c1"])
		}
	}
	for _, dep := range []string{"c2", "c3"} {
		if startedAt["c4"].Before(finished[dep]) {
			t.Fatalf("boundary 5: c4 started %s before %s finished %s", startedAt["c4"], dep, finished[dep])
		}
	}
	if directive, err := store.RunDirective(projectID, c1Record); err != nil || directive == nil {
		t.Fatalf("boundary 5: c1 directive history missing: %#v err=%v", directive, err)
	} else if directive.WaveID != waveID {
		t.Fatalf("boundary 5: c1 directive bound to wave %q, want %s", directive.WaveID, waveID)
	}
	if code := demoRunInner(t, "demo check", Args{"repo": repo}); code != demoExitOK {
		t.Fatalf("boundary 5: demo check exit %d, want PASS", code)
	}
}

func assertCrossWaveContracts(t *testing.T, vault, c1, a4, b4, boundary string) {
	t.Helper()
	task, err := resolveV7Note(vault, c1, "task")
	if err != nil {
		t.Fatalf("%s: resolve c1: %s", boundary, err.Error())
	}
	targets := []string{a4, b4}
	sort.Strings(targets)
	var edges []string
	for _, raw := range normalizeList(task.Data["dependencies"]) {
		edge := parseV7DependencyEdge(raw)
		edges = append(edges, edge.ID+":"+edge.Hardness)
	}
	sort.Strings(edges)
	if len(edges) != 2 || edges[0] != targets[0]+":hard" || edges[1] != targets[1]+":hard" {
		t.Fatalf("%s: c1 dependency edges %v, want %v:hard", boundary, edges, targets)
	}
	contracts, err := dependencyContractEntries(task)
	if err != nil {
		t.Fatalf("%s: c1 dependency_contracts malformed: %s", boundary, err.Error())
	}
	if len(contracts) != 2 {
		t.Fatalf("%s: c1 dependency_contracts %#v, want two rows", boundary, contracts)
	}
	pinned := map[string]string{}
	for _, entry := range contracts {
		if entry.Kind != v7DependencyHardnessHard {
			t.Fatalf("%s: contract kind for %s: %q, want hard", boundary, entry.TaskID, entry.Kind)
		}
		pinned[strings.ToUpper(strings.TrimSpace(entry.TaskID))] = entry.TargetContractFingerprint
	}
	for _, target := range targets {
		producer, err := resolveV7Note(vault, target, "task")
		if err != nil {
			t.Fatalf("%s: resolve %s: %s", boundary, target, err.Error())
		}
		if pinned[target] == "" || pinned[target] != directWaveTaskContract(producer) {
			t.Fatalf("%s: contract fingerprint for %s: %q, want current %q", boundary, target, pinned[target], directWaveTaskContract(producer))
		}
	}
}
