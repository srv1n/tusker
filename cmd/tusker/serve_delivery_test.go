package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newServeDeliveryFixture(t *testing.T) (*serveServer, string) {
	t.Helper()
	stateRoot := filepath.Join(t.TempDir(), "state")
	t.Setenv("TUSKER_STATE_ROOT", stateRoot)
	vault := deliveryTestVault(t)
	repo := v7RepoRoot(vault)
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	project := RegisteredProject{ProjectID: "delivery", ProjectKey: "delivery", Name: "delivery", RepoRoot: repo, VaultRoot: vault, WorkflowPath: workflowPath(vault), Enabled: false, Health: projectHealthDisabled}
	if err := store.UpsertProject(project); err != nil {
		t.Fatal(err)
	}
	return newServeServer(vault, repo, defaultServeAddr, store, nil), repo
}

func TestServeDeliveryReviewUsesCanonicalProjectionWithoutMutation(t *testing.T) {
	server, repo := newServeDeliveryFixture(t)
	vault := server.vaultPath
	plan := validDeliveryPlanV2()
	plan.HumanGates = nil
	path := writeDeliveryV2TestPlan(t, vault, plan)
	if err := deliveryV2ImportCmd(vault, path, Args{"quiet": "true", "skip-integration-branch": "true"}); err != nil {
		t.Fatal(err)
	}
	before := snapshotDeliveryRecords(t, vault)
	stateRoot := DefaultStateRoot()
	beforeState := snapshotTree(t, stateRoot)
	beforeDB, err := os.ReadFile(runtimeStoreDBPath(stateRoot))
	if err != nil {
		t.Fatal(err)
	}
	beforeProjects, err := server.store.ListProjects()
	if err != nil {
		t.Fatal(err)
	}
	beforeRuns, err := server.store.ListRuns()
	if err != nil {
		t.Fatal(err)
	}

	var review deliveryReview
	serveDecode(t, server, "/api/delivery/review?project=delivery&plan="+filepath.ToSlash(filepath.Join(".tusker", "scratch", filepath.Base(path))), &review)
	if review.Schema != deliveryReviewSchema || !review.ReadOnly || len(review.What) == 0 || len(review.Proof) == 0 || review.Start.PlanFingerprint == "" {
		t.Fatalf("Serve did not return the canonical five-section review: %#v", review)
	}
	if review.Start.State != "disabled" {
		t.Fatalf("read-only canonical environment projection state=%q review=%#v", review.Start.State, review.Start)
	}
	assertEqual(t, before, snapshotDeliveryRecords(t, vault), "review must not import or arm delivery")
	assertSnapshotEqual(t, beforeState, snapshotTree(t, stateRoot), "delivery review runtime state")
	afterDB, err := os.ReadFile(runtimeStoreDBPath(stateRoot))
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, beforeDB, afterDB, "delivery review runtime database bytes")
	afterProjects, err := server.store.ListProjects()
	if err != nil {
		t.Fatal(err)
	}
	afterRuns, err := server.store.ListRuns()
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, beforeProjects, afterProjects, "delivery review registration rows and revisions")
	assertEqual(t, beforeRuns, afterRuns, "delivery review runtime rows and revisions")
	if filepath.Dir(path) != filepath.Join(repo, ".tusker", "scratch") {
		t.Fatal("fixture plan unexpectedly escaped repository")
	}
}

func TestServeDeliveryReviewPreservesRelationshipsAndResolvableDeepLinks(t *testing.T) {
	server, _ := newServeDeliveryFixture(t)
	vault := server.vaultPath
	plan := operationalDeliveryPlanV2()
	path := writeDeliveryV2TestPlan(t, vault, plan)
	if err := deliveryV2ImportCmd(vault, path, Args{"quiet": "true", "skip-integration-branch": "true"}); err != nil {
		t.Fatal(err)
	}

	var review deliveryReview
	serveDecode(t, server, "/api/delivery/review?project=delivery&plan=.tusker/scratch/delivery-plan-v2.yaml", &review)
	if len(review.What) == 0 || len(review.What[0].Links) == 0 {
		t.Fatalf("requirement lost governing-spec links: %#v", review.What)
	}
	if len(review.Proof) == 0 || review.Proof[0].TaskID == "" || review.Proof[0].TaskHref == "" || len(review.Proof[0].Checks) == 0 || review.Proof[0].Checks[0].Href == "" || len(review.Proof[0].ArtifactRefs) == 0 || review.Proof[0].ArtifactRefs[0].Href == "" {
		t.Fatalf("proof lost task/check/artifact relationships: %#v", review.Proof)
	}
	if len(review.Flow.SharedResources) == 0 || len(review.Flow.SharedResources[0].ReferencedBy) == 0 || len(review.Flow.SharedResources[0].TaskLinks) == 0 || review.Flow.WaveID == "" || review.Flow.WaveHref == "" {
		t.Fatalf("flow lost shared-resource or wave relationships: %#v", review.Flow)
	}
	if len(review.Decisions) == 0 || review.Decisions[0].GateID == "" || review.Decisions[0].GateHref == "" || review.Decisions[0].TaskID == "" || len(review.Decisions[0].AcceptanceIDs) == 0 {
		t.Fatalf("human decision lost gate/task/acceptance relationships: %#v", review.Decisions)
	}
}

func TestServeDeliveryReviewProjectsV2PreparationErrorsAsInvalid(t *testing.T) {
	server, _ := newServeDeliveryFixture(t)
	plan := validDeliveryPlanV2()
	plan.HumanGates[0].TaskSourceKey = "missing-task"
	path := writeDeliveryV2TestPlan(t, server.vaultPath, plan)

	var review deliveryReview
	serveDecode(t, server, "/api/delivery/review?project=delivery&plan="+filepath.ToSlash(filepath.Join(".tusker", "scratch", filepath.Base(path))), &review)
	if review.Ready || review.Start.State != "invalid" || !strings.Contains(review.Start.NextAction, "human gate") {
		t.Fatalf("V2 preparation defect did not project canonical invalid state: %#v", review.Start)
	}
}

func TestServeDeliveryRejectsUnsafePathsAndStaleConfirmation(t *testing.T) {
	server, repo := newServeDeliveryFixture(t)
	vault := server.vaultPath
	plan := validDeliveryPlanV2()
	plan.HumanGates = nil
	path := writeDeliveryV2TestPlan(t, vault, plan)
	rel := filepath.ToSlash(filepath.Join(".tusker", "scratch", filepath.Base(path)))

	for _, unsafe := range []string{"/tmp/plan.yaml", "../plan.yaml"} {
		req := httptest.NewRequest(http.MethodGet, "/api/delivery/review?project=delivery&plan="+unsafe, nil)
		rec := httptest.NewRecorder()
		server.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("unsafe path %q status=%d body=%s", unsafe, rec.Code, rec.Body.String())
		}
	}
	outside := filepath.Join(t.TempDir(), "outside.yaml")
	if err := writeText(outside, "schema: tusker.delivery-plan/v2\n"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(repo, "escaped.yaml")); err != nil {
		t.Fatal(err)
	}
	if _, err := serveDeliveryPlanPath(RegisteredProject{RepoRoot: repo}, "escaped.yaml"); err == nil {
		t.Fatal("symlink escape was accepted")
	}

	before := snapshotDeliveryRecords(t, vault)
	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:7420/api/delivery/start?project=delivery", bytes.NewBufferString(`{"plan":"`+rel+`","confirm":"sha256:stale"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("stale confirmation status=%d body=%s", rec.Code, rec.Body.String())
	}
	assertEqual(t, before, snapshotDeliveryRecords(t, vault), "stale Start must not mutate")
}

func TestServeDeliveryRejectsPlanOwnedByNestedRegisteredProject(t *testing.T) {
	server, repo := newServeDeliveryFixture(t)
	repoAlias := filepath.Join(t.TempDir(), "repo-alias")
	if err := os.Symlink(repo, repoAlias); err != nil {
		t.Fatal(err)
	}
	if !serveSameRegisteredProject(
		RegisteredProject{ProjectID: "delivery", RepoRoot: repo, VaultRoot: server.vaultPath},
		RegisteredProject{ProjectID: "delivery-alias", RepoRoot: repoAlias, VaultRoot: filepath.Join(repoAlias, ".tusker")},
	) {
		t.Fatal("canonical symlink alias was treated as a different project")
	}
	childRepo := filepath.Join(repo, "nested-project")
	childVault := filepath.Join(childRepo, ".tusker")
	if err := writeText(workflowPath(childVault), defaultWorkflowMarkdown()); err != nil {
		t.Fatal(err)
	}
	parentPlan := writeDeliveryV2TestPlan(t, server.vaultPath, validDeliveryPlanV2())
	raw, err := os.ReadFile(parentPlan)
	if err != nil {
		t.Fatal(err)
	}
	childPlan := filepath.Join(childVault, "scratch", "delivery-plan-v2.yaml")
	if err := writeText(childPlan, string(raw)); err != nil {
		t.Fatal(err)
	}
	child := RegisteredProject{ProjectID: "nested", ProjectKey: "nested", Name: "nested", RepoRoot: childRepo, VaultRoot: childVault, WorkflowPath: workflowPath(childVault), Enabled: false, Health: projectHealthDisabled}
	if err := server.store.UpsertProject(child); err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodGet, "/api/delivery/review?project=delivery&plan=nested-project/.tusker/scratch/delivery-plan-v2.yaml", nil)
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("nested project plan status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var problem serveDeliveryError
	if err := json.Unmarshal(recorder.Body.Bytes(), &problem); err != nil {
		t.Fatal(err)
	}
	if problem.Schema != serveDeliveryErrorSchema || problem.Error.Code != errorInvalidArg || !strings.Contains(problem.Error.Message, "different registered nested project") {
		t.Fatalf("nested project refusal lost typed identity: %#v", problem)
	}
}

func TestServeDeliveryStartReplaysCanonicalAuthorization(t *testing.T) {
	server, _ := newServeDeliveryFixture(t)
	vault := server.vaultPath
	plan := validDeliveryPlanV2()
	plan.HumanGates = nil
	path := writeDeliveryV2TestPlan(t, vault, plan)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	rel := filepath.ToSlash(filepath.Join(".tusker", "scratch", filepath.Base(path)))
	confirm := deliveryFingerprint(raw)
	original := serveDeliveryStartFn
	serveDeliveryStartFn = func(args Args) (deliveryStartResult, error) {
		return deliveryStart(args, fixedWaveEnvironmentInspector(greenWaveEnvironment()))
	}
	t.Cleanup(func() { serveDeliveryStartFn = original })

	post := func() deliveryStartResult {
		request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:7420/api/delivery/start?project=delivery", bytes.NewBufferString(`{"plan":"`+rel+`","confirm":"`+confirm+`"}`))
		request.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()
		server.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusOK {
			t.Fatalf("Start status=%d body=%s", recorder.Code, recorder.Body.String())
		}
		var result deliveryStartResult
		if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	first := post()
	beforeReplay := snapshotDeliveryRecords(t, vault)
	second := post()
	if !second.Replayed || second.WaveID != first.WaveID || second.AuthorizationFingerprint != first.AuthorizationFingerprint {
		t.Fatalf("Serve replay did not preserve canonical authorization: first=%#v second=%#v", first, second)
	}
	assertEqual(t, beforeReplay, snapshotDeliveryRecords(t, vault), "Serve replay must not rewrite delivery records")
}

func TestServeDeliveryStartPreservesTypedPreflightRefusal(t *testing.T) {
	server, _ := newServeDeliveryFixture(t)
	vault := server.vaultPath
	plan := validDeliveryPlanV2()
	plan.HumanGates = nil
	path := writeDeliveryV2TestPlan(t, vault, plan)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	rel := filepath.ToSlash(filepath.Join(".tusker", "scratch", filepath.Base(path)))
	env := greenWaveEnvironment()
	env.ProjectEnabled = false
	original := serveDeliveryStartFn
	serveDeliveryStartFn = func(args Args) (deliveryStartResult, error) {
		return deliveryStart(args, fixedWaveEnvironmentInspector(env))
	}
	t.Cleanup(func() { serveDeliveryStartFn = original })

	request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:7420/api/delivery/start?project=delivery", bytes.NewBufferString(`{"plan":"`+rel+`","confirm":"`+deliveryFingerprint(raw)+`"}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusConflict {
		t.Fatalf("blocked Start status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var problem serveDeliveryError
	if err := json.Unmarshal(recorder.Body.Bytes(), &problem); err != nil {
		t.Fatal(err)
	}
	context, ok := problem.Error.Context.(map[string]any)
	if problem.Schema != serveDeliveryErrorSchema || problem.Error.Code != errorInvalidTransition || !ok || context["delivery_start"] == nil {
		t.Fatalf("blocked Start lost typed canonical refusal: %#v", problem)
	}

	var review deliveryReview
	serveDecode(t, server, "/api/delivery/review?project=delivery&plan="+rel, &review)
	if review.Start.State != "disabled" || review.Start.NextAction == "" {
		t.Fatalf("blocked Start did not project one canonical disabled remedy: %#v", review.Start)
	}
}

func TestDeliveryReviewEnvironmentStatesHaveOneTruthfulAction(t *testing.T) {
	t.Setenv("TUSKER_STATE_ROOT", t.TempDir())
	base := greenWaveEnvironment()
	wave := Note{Data: map[string]any{"id": "APP-W-0001", "project": "delivery"}}
	tests := []struct {
		name       string
		mutate     func(*wavePreflightEnvironment, *wavePreflightReport)
		state      string
		actionPart string
	}{
		{name: "changed", mutate: func(_ *wavePreflightEnvironment, report *wavePreflightReport) { report.AuthorizationStale = true }, state: "changed", actionPart: "Regenerate"},
		{name: "unregistered", mutate: func(env *wavePreflightEnvironment, _ *wavePreflightReport) { env.ProjectRegistered = false }, state: "disabled", actionPart: "Register"},
		{name: "workflow", mutate: func(env *wavePreflightEnvironment, _ *wavePreflightReport) { env.WorkflowCompatible = false }, state: "invalid", actionPart: "workflow"},
		{name: "skill", mutate: func(env *wavePreflightEnvironment, _ *wavePreflightReport) { env.SkillCompatible = false }, state: "invalid", actionPart: "project skill"},
		{name: "disabled", mutate: func(env *wavePreflightEnvironment, _ *wavePreflightReport) { env.ProjectEnabled = false }, state: "disabled", actionPart: "Enable"},
		{name: "unhealthy", mutate: func(env *wavePreflightEnvironment, _ *wavePreflightReport) { env.ProjectHealthy = false }, state: "disabled", actionPart: "health"},
		{name: "daemon off", mutate: func(env *wavePreflightEnvironment, _ *wavePreflightReport) { env.DaemonAlive = false }, state: "daemon-off", actionPart: "Start"},
		{name: "daemon stalled", mutate: func(env *wavePreflightEnvironment, _ *wavePreflightReport) { env.DaemonReconciling = false }, state: "daemon-off", actionPart: "polling"},
		{name: "runner", mutate: func(env *wavePreflightEnvironment, _ *wavePreflightReport) { env.RunnerCompatible = false }, state: "runner-blocked", actionPart: "supported"},
		{name: "approval", mutate: func(env *wavePreflightEnvironment, _ *wavePreflightReport) { env.ApprovalFree = false }, state: "runner-blocked", actionPart: "approval-free"},
		{name: "shared", mutate: func(env *wavePreflightEnvironment, _ *wavePreflightReport) { env.IsolatedWorkspace = false }, state: "shared-workspace", actionPart: "isolated"},
		{name: "integration", mutate: func(env *wavePreflightEnvironment, _ *wavePreflightReport) { env.IntegrationClean = false }, state: "shared-workspace", actionPart: "integration"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			env := base
			report := wavePreflightReport{}
			test.mutate(&env, &report)
			review := deliveryReview{}
			deliveryReviewProjectState("", v7Index{}, wave, env, report, &review)
			if review.Start.State != test.state || review.Start.NextAction == "" || !strings.Contains(review.Start.NextAction, test.actionPart) {
				t.Fatalf("state=%q action=%q; want state=%q action containing %q", review.Start.State, review.Start.NextAction, test.state, test.actionPart)
			}
		})
	}

	for _, test := range []struct {
		authorization string
		state         string
	}{
		{authorization: "disarmed", state: "held"},
		{authorization: "armed", state: "gated"},
	} {
		report := wavePreflightReport{Authorization: test.authorization, HumanGates: []map[string]any{{"id": "APP-G-0001"}}}
		review := deliveryReview{}
		deliveryReviewProjectState("", v7Index{}, wave, base, report, &review)
		if review.Start.State != test.state {
			t.Fatalf("%s wave with an open gate state=%q; want %q", test.authorization, review.Start.State, test.state)
		}
	}
}
