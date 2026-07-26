package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
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

func beginServeDeliveryMutationTracking(t *testing.T) func() []string {
	t.Helper()
	finished := false
	beginCLIVaultMutationTracking()
	t.Cleanup(func() {
		if !finished {
			_ = finishCLIVaultMutationTracking()
		}
	})
	return func() []string {
		t.Helper()
		if finished {
			t.Fatal("Serve delivery mutation tracking finished twice")
		}
		finished = true
		return finishCLIVaultMutationTracking()
	}
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
	if review.Schema != deliveryReviewSchema || !review.ReadOnly || len(review.What) == 0 || len(review.Proof) == 0 || review.Start.PlanFingerprint == "" || review.Start.PlanIdentity == "" {
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

func TestDeliveryReviewFreshPlanProjectsProspectiveEnvironmentWithoutMutation(t *testing.T) {
	server, repo := newServeDeliveryFixture(t)
	vault := server.vaultPath
	runGitDir(t, repo, "init", "-b", "main")
	runGitDir(t, repo, "config", "user.email", "test@example.com")
	runGitDir(t, repo, "config", "user.name", "Test User")
	runGitDir(t, repo, "add", ".")
	runGitDir(t, repo, "commit", "-m", "seed prospective delivery review")
	plan := validDeliveryPlanV2()
	plan.HumanGates = nil
	path := writeDeliveryV2TestPlan(t, vault, plan)
	stateRoot := DefaultStateRoot()
	beforeRecords := snapshotDeliveryRecords(t, vault)
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

	tests := []struct {
		name   string
		mutate func(*wavePreflightEnvironment)
		state  string
		label  string
		action string
		ready  bool
	}{
		{
			name: "unregistered",
			mutate: func(env *wavePreflightEnvironment) {
				env.ProjectRegistered = false
			},
			state: "disabled", label: "Project is not registered",
			action: "Register this project in Project Settings, then review the delivery again.",
		},
		{
			name: "automation off",
			mutate: func(env *wavePreflightEnvironment) {
				env.ProjectEnabled = false
			},
			state: "disabled", label: "Project automation is off",
			action: "Enable this project's automation in Project Settings, then review the delivery again.",
		},
		{
			name: "daemon off",
			mutate: func(env *wavePreflightEnvironment) {
				env.DaemonAlive = false
			},
			state: "daemon-off", label: "Resident daemon is off",
			action: "Start the resident daemon, then review the delivery again.",
		},
		{
			name: "runner block",
			mutate: func(env *wavePreflightEnvironment) {
				env.RunnerCompatible = false
			},
			state: "runner-blocked", label: "Runner is incompatible",
			action: "Configure a supported unattended runner for this wave, then review again.",
		},
		{
			name: "shared workspace",
			mutate: func(env *wavePreflightEnvironment) {
				env.IsolatedWorkspace = false
			},
			state: "shared-workspace", label: "Workspace is shared",
			action: "Select an isolated workspace strategy in Project Settings, then review again.",
		},
		{name: "healthy", mutate: func(*wavePreflightEnvironment) {}, state: "held", label: "Ready to start", ready: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			env := greenWaveEnvironment()
			test.mutate(&env)
			var prospective Note
			review, reviewErr := buildDeliveryReviewWithInspector(vault, path, func(_ string, wave Note) wavePreflightEnvironment {
				prospective = wave
				return env
			})
			if reviewErr != nil {
				t.Fatal(reviewErr)
			}
			action := test.action
			if test.ready {
				action = deliveryReviewStartCommand(vault, path, review.Start.PlanFingerprint)
			}
			if review.Ready != test.ready || review.Start.State != test.state || review.Start.StateLabel != test.label || review.Start.NextAction != action {
				t.Fatalf("fresh state=%#v ready=%t; want state=%q label=%q action=%q ready=%t", review.Start, review.Ready, test.state, test.label, action, test.ready)
			}
			wantBlockers := 1
			if test.ready {
				wantBlockers = 0
			}
			if len(review.Start.Blockers) != wantBlockers {
				t.Fatalf("fresh blockers=%#v; want exactly %d", review.Start.Blockers, wantBlockers)
			}
			if review.Start.Authorization != "not imported" || review.Flow.WaveID != "" || review.Flow.WaveHref != "" {
				t.Fatalf("prospective review invented canonical identity or authorization: start=%#v flow=%#v", review.Start, review.Flow)
			}
			if _, claimed := prospective.Data["authorization"]; claimed {
				t.Fatalf("prospective wave invented authorization: %#v", prospective.Data)
			}
			if !strings.HasPrefix(stringField(prospective.Data, "id"), "PROSPECTIVE-") ||
				stringField(prospective.Data, "project") != v7ProjectID(vault) ||
				stringField(prospective.Data, "delivery_plan_scope") != plan.Scope ||
				stringField(prospective.Data, "delivery_plan_fingerprint") != review.Start.PlanFingerprint ||
				stringField(prospective.Data, "runner_profile") != plan.RunnerProfile ||
				stringField(prospective.Data, "integration_base_sha") == "" ||
				!strings.HasPrefix(stringField(prospective.Data, "integration_branch"), "integration/PROSPECTIVE-") {
				t.Fatalf("prospective wave omitted required read-only environment material: %#v", prospective.Data)
			}
		})
	}

	assertEqual(t, beforeRecords, snapshotDeliveryRecords(t, vault), "fresh review must not import delivery")
	assertSnapshotEqual(t, beforeState, snapshotTree(t, stateRoot), "fresh review runtime state")
	afterDB, err := os.ReadFile(runtimeStoreDBPath(stateRoot))
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, beforeDB, afterDB, "fresh review runtime database bytes")
	afterProjects, err := server.store.ListProjects()
	if err != nil {
		t.Fatal(err)
	}
	afterRuns, err := server.store.ListRuns()
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, beforeProjects, afterProjects, "fresh review registration rows and revisions")
	assertEqual(t, beforeRuns, afterRuns, "fresh review runtime rows and revisions")
}

func TestDeliveryReviewCompletedOutranksDisabledEnvironmentButNotStaleIdentity(t *testing.T) {
	server, _ := newServeDeliveryFixture(t)
	vault := server.vaultPath
	plan := validDeliveryPlanV2()
	plan.HumanGates = nil
	path := writeDeliveryV2TestPlan(t, vault, plan)
	if err := deliveryV2ImportCmd(vault, path, Args{"quiet": "true", "skip-integration-branch": "true"}); err != nil {
		t.Fatal(err)
	}
	writeArmedWaveTestFields(t, vault, map[string]any{
		"landings": []map[string]any{{"task": "wave", "gate_result": "pass"}},
	})

	var completed deliveryReview
	serveDecode(t, server, "/api/delivery/review?project=delivery&plan=.tusker/scratch/delivery-plan-v2.yaml", &completed)
	if !completed.Ready || completed.Start.State != "completed" || completed.Start.StateLabel != "Delivery completed" ||
		completed.Start.NextAction != "Review the delivered artifacts and integration outcome." || len(completed.Start.Blockers) != 0 ||
		completed.Start.ActionHref == "" {
		t.Fatalf("disabled infrastructure rewrote completed delivery truth: %#v", completed.Start)
	}

	writeArmedWaveTestFields(t, vault, map[string]any{
		"authorization":             "armed",
		"authorization_fingerprint": "sha256:stale",
	})
	var stale deliveryReview
	serveDecode(t, server, "/api/delivery/review?project=delivery&plan=.tusker/scratch/delivery-plan-v2.yaml", &stale)
	if stale.Ready || stale.Start.State != "changed" || stale.Start.StateLabel != "Delivery changed" ||
		!strings.Contains(stale.Start.NextAction, "Regenerate delivery review") ||
		!strings.Contains(strings.Join(stale.Start.Blockers, "\n"), "authorization fingerprint is stale") {
		t.Fatalf("stale delivery identity did not outrank terminal completion: %#v", stale.Start)
	}
}

func TestDeliveryReviewProjectsBudgetParkButNotRetryOrHold(t *testing.T) {
	server, _ := newServeDeliveryFixture(t)
	vault := server.vaultPath
	plan := validDeliveryPlanV2()
	plan.HumanGates = nil
	path := writeDeliveryV2TestPlan(t, vault, plan)
	if err := deliveryV2ImportCmd(vault, path, Args{"quiet": "true", "skip-integration-branch": "true"}); err != nil {
		t.Fatal(err)
	}
	idx, err := loadV7Index(vault)
	if err != nil {
		t.Fatal(err)
	}
	wave := idx.Waves["W-0001"]
	members := normalizeList(wave.Data["members"])
	if len(members) != 1 {
		t.Fatalf("budget fixture members=%#v", members)
	}
	taskID := members[0]
	setAutomationV7TaskFields(t, vault, taskID, map[string]any{"status": "ready", "readiness": "ready", "next_owner": "agent"})
	idx, err = loadV7Index(vault)
	if err != nil {
		t.Fatal(err)
	}
	wave = idx.Waves["W-0001"]
	fingerprint, issues := waveMaterialFingerprint(vault, idx, wave)
	if len(issues) > 0 {
		t.Fatal(issues)
	}
	writeArmedWaveTestFields(t, vault, map[string]any{
		"authorization":             "armed",
		"authorization_fingerprint": fingerprint,
	})
	run := RunStatus{
		ProjectID: "delivery", RecordID: taskID, ItemID: taskID,
		LeaseState: string(LeaseStateParkedBudget), LastError: "project budget is exhausted", Terminal: true,
	}
	if err := server.store.UpsertRun(run); err != nil {
		t.Fatal(err)
	}

	review, err := buildDeliveryReviewWithInspector(vault, path, fixedWaveEnvironmentInspector(greenWaveEnvironment()))
	if err != nil {
		t.Fatal(err)
	}
	if review.Start.State != "parked" || !strings.Contains(review.Start.NextAction, "project budget is exhausted") {
		t.Fatalf("budget park was not projected as parked: %#v", review.Start)
	}

	run.LeaseState = string(LeaseStateRetryQueued)
	run.LastError = "retry is queued"
	run.Terminal = false
	if err := server.store.UpsertRun(run); err != nil {
		t.Fatal(err)
	}
	retrying, err := buildDeliveryReviewWithInspector(vault, path, fixedWaveEnvironmentInspector(greenWaveEnvironment()))
	if err != nil {
		t.Fatal(err)
	}
	if retrying.Start.State != "armed" {
		t.Fatalf("retry queue was conflated with a parked delivery: %#v", retrying.Start)
	}

	writeArmedWaveTestFields(t, vault, map[string]any{
		"authorization":             "disarmed",
		"authorization_fingerprint": "",
	})
	held, err := buildDeliveryReviewWithInspector(vault, path, fixedWaveEnvironmentInspector(greenWaveEnvironment()))
	if err != nil {
		t.Fatal(err)
	}
	if held.Start.State != "held" {
		t.Fatalf("held review was conflated with a parked delivery: %#v", held.Start)
	}
}

func TestDeliveryReviewPageUsesStandardScrollableLayout(t *testing.T) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("could not locate delivery review test source")
	}
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(currentFile), "..", ".."))
	pageSource, err := os.ReadFile(filepath.Join(repoRoot, "internal", "serve", "ui", "src", "features", "delivery", "DeliveryReview.tsx"))
	if err != nil {
		t.Fatal(err)
	}
	scrollSource, err := os.ReadFile(filepath.Join(repoRoot, "internal", "serve", "ui", "src", "components", "ui", "page.tsx"))
	if err != nil {
		t.Fatal(err)
	}
	page := string(pageSource)
	if !strings.Contains(page, `import { PageScroll } from "@/components/ui/page";`) ||
		!strings.Contains(page, "<PageScroll>") ||
		!strings.Contains(page, "</PageScroll>") ||
		!strings.Contains(page, "data-delivery-review-page") {
		t.Fatalf("delivery review page is not wrapped in PageScroll")
	}
	if !strings.Contains(string(scrollSource), `"tk-scroll h-full overflow-y-auto"`) {
		t.Fatalf("PageScroll lost its full-height vertical overflow contract")
	}
}

func TestServeDeliveryReviewPreservesRelationshipsAndResolvableDeepLinks(t *testing.T) {
	server, _ := newServeDeliveryFixture(t)
	vault := server.vaultPath
	plan := operationalDeliveryPlanV2()
	for _, task := range plan.Tasks {
		artifactPath, ok := safeRepoPath(v7RepoRoot(vault), filepath.ToSlash(task.Artifact.Path))
		if !ok {
			t.Fatalf("fixture artifact path escapes repository: %q", task.Artifact.Path)
		}
		if err := writeText(artifactPath, "fixture artifact for "+task.SourceKey+"\n"); err != nil {
			t.Fatal(err)
		}
	}
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
	var reviewed deliveryReview
	serveDecode(t, server, "/api/delivery/review?project=delivery&plan="+rel, &reviewed)
	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:7420/api/delivery/start?project=delivery", bytes.NewBufferString(`{"plan":"`+rel+`","confirm":"sha256:stale","planIdentity":"`+reviewed.Start.PlanIdentity+`"}`))
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

func TestServeDeliveryPlanSnapshotRejectsNoFollowComponentSwaps(t *testing.T) {
	server, repo := newServeDeliveryFixture(t)
	vault := server.vaultPath
	plan := validDeliveryPlanV2()
	plan.HumanGates = nil
	canonicalPath := writeDeliveryV2TestPlan(t, vault, plan)
	raw, err := os.ReadFile(canonicalPath)
	if err != nil {
		t.Fatal(err)
	}
	planDir := filepath.Join(repo, "plans")
	planPath := filepath.Join(planDir, "review.yaml")
	if err := writeText(planPath, string(raw)); err != nil {
		t.Fatal(err)
	}
	externalPath := filepath.Join(t.TempDir(), "external.yaml")
	if err := writeText(externalPath, strings.Replace(string(raw), plan.Summary, "External plan bytes must never be read.", 1)); err != nil {
		t.Fatal(err)
	}
	nestedRepo := filepath.Join(repo, "nested-project")
	nestedVault := filepath.Join(nestedRepo, ".tusker")
	nestedPlanDir := filepath.Join(nestedRepo, "plans")
	nestedPlan := filepath.Join(nestedPlanDir, "review.yaml")
	if err := writeText(workflowPath(nestedVault), defaultWorkflowMarkdown()); err != nil {
		t.Fatal(err)
	}
	if err := writeText(nestedPlan, strings.Replace(string(raw), plan.Summary, "Nested-project bytes must never be read.", 1)); err != nil {
		t.Fatal(err)
	}
	if err := server.store.UpsertProject(RegisteredProject{ProjectID: "nested-race", ProjectKey: "nested-race", RepoRoot: nestedRepo, VaultRoot: nestedVault, WorkflowPath: workflowPath(nestedVault)}); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name      string
		component int
		swap      func() func()
	}{
		{
			name:      "final file to external symlink",
			component: 1,
			swap: func() func() {
				return swapServeDeliveryPlanWithSymlink(t, planPath, externalPath)
			},
		},
		{
			name:      "intermediate directory to nested-project symlink",
			component: 0,
			swap: func() func() {
				return swapServeDeliveryPlanWithSymlink(t, planDir, nestedPlanDir)
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			beforeRecords := snapshotDeliveryRecords(t, vault)
			beforeState := snapshotTree(t, DefaultStateRoot())
			originalHook := serveDeliveryPlanOpenComponentHook
			var restore func()
			swapped := false
			serveDeliveryPlanOpenComponentHook = func(relative string, component int) {
				if !swapped && relative == filepath.Join("plans", "review.yaml") && component == test.component {
					swapped = true
					restore = test.swap()
				}
			}
			defer func() {
				serveDeliveryPlanOpenComponentHook = originalHook
				if restore != nil {
					restore()
				}
			}()

			request := httptest.NewRequest(http.MethodGet, "/api/delivery/review?project=delivery&plan=plans/review.yaml", nil)
			recorder := httptest.NewRecorder()
			server.ServeHTTP(recorder, request)
			if !swapped || recorder.Code != http.StatusUnprocessableEntity {
				t.Fatalf("component swap accepted: swapped=%t status=%d body=%s", swapped, recorder.Code, recorder.Body.String())
			}
			assertEqual(t, beforeRecords, snapshotDeliveryRecords(t, vault), "component swap must not import records")
			assertSnapshotEqual(t, beforeState, snapshotTree(t, DefaultStateRoot()), "component swap runtime state")
		})
	}
}

func TestServeDeliveryBoundSnapshotSurvivesFormerReopenSwaps(t *testing.T) {
	for _, phase := range []string{"schema", "review"} {
		t.Run(phase, func(t *testing.T) {
			server, _ := newServeDeliveryFixture(t)
			vault := server.vaultPath
			plan := validDeliveryPlanV2()
			plan.HumanGates = nil
			path := writeDeliveryV2TestPlan(t, vault, plan)
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			external := filepath.Join(t.TempDir(), "external.yaml")
			if err := writeText(external, strings.Replace(string(raw), plan.Summary, "Untrusted replacement bytes.", 1)); err != nil {
				t.Fatal(err)
			}
			before := snapshotDeliveryRecords(t, vault)
			originalHook := serveDeliveryPlanPhaseHook
			var restore func()
			swapped := false
			serveDeliveryPlanPhaseHook = func(current string) {
				if !swapped && current == phase {
					swapped = true
					restore = swapServeDeliveryPlanWithSymlink(t, path, external)
				}
			}
			defer func() {
				serveDeliveryPlanPhaseHook = originalHook
				if restore != nil {
					restore()
				}
			}()

			var review deliveryReview
			serveDecode(t, server, "/api/delivery/review?project=delivery&plan=.tusker/scratch/delivery-plan-v2.yaml", &review)
			if !swapped || review.Start.PlanFingerprint != deliveryFingerprint(raw) || strings.Contains(strings.Join(review.Start.Blockers, "\n"), "Untrusted replacement bytes") {
				t.Fatalf("%s phase consumed replacement bytes: swapped=%t review=%#v", phase, swapped, review.Start)
			}
			assertEqual(t, before, snapshotDeliveryRecords(t, vault), phase+" snapshot must remain read-only")
		})
	}

	for _, phase := range []string{"start", "import"} {
		t.Run(phase, func(t *testing.T) {
			server, _ := newServeDeliveryFixture(t)
			vault := server.vaultPath
			plan := validDeliveryPlanV2()
			plan.HumanGates = nil
			path := writeDeliveryV2TestPlan(t, vault, plan)
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			external := filepath.Join(t.TempDir(), "external.yaml")
			if err := writeText(external, string(raw)); err != nil {
				t.Fatal(err)
			}
			var review deliveryReview
			serveDecode(t, server, "/api/delivery/review?project=delivery&plan=.tusker/scratch/delivery-plan-v2.yaml", &review)
			beforeRecords := snapshotDeliveryRecords(t, vault)
			beforeState := snapshotTree(t, DefaultStateRoot())
			originalHook := serveDeliveryPlanPhaseHook
			var restore func()
			swapped := false
			serveDeliveryPlanPhaseHook = func(current string) {
				if !swapped && current == phase {
					swapped = true
					restore = swapServeDeliveryPlanWithSymlink(t, path, external)
				}
			}
			defer func() {
				serveDeliveryPlanPhaseHook = originalHook
				if restore != nil {
					restore()
				}
			}()

			body := `{"plan":".tusker/scratch/delivery-plan-v2.yaml","confirm":"` + review.Start.PlanFingerprint + `","planIdentity":"` + review.Start.PlanIdentity + `"}`
			request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:7420/api/delivery/start?project=delivery", bytes.NewBufferString(body))
			request.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()
			server.ServeHTTP(recorder, request)
			if !swapped || recorder.Code != http.StatusConflict {
				t.Fatalf("%s path swap accepted: swapped=%t status=%d body=%s", phase, swapped, recorder.Code, recorder.Body.String())
			}
			assertEqual(t, beforeRecords, snapshotDeliveryRecords(t, vault), phase+" path swap must refuse before import")
			assertSnapshotEqual(t, beforeState, snapshotTree(t, DefaultStateRoot()), phase+" path swap runtime state")
		})
	}
}

func TestServeDeliveryImportCommitSwapRestoresEveryPreimage(t *testing.T) {
	server, repo := newServeDeliveryFixture(t)
	vault := server.vaultPath
	specPath := filepath.Join(repo, "docs", "specs", "delivery.md")
	if err := os.Chmod(specPath, 0o600); err != nil {
		t.Fatal(err)
	}
	plan := validDeliveryPlanV2()
	path := writeDeliveryV2TestPlan(t, vault, plan)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	external := filepath.Join(t.TempDir(), "external.yaml")
	if err := writeText(external, string(raw)); err != nil {
		t.Fatal(err)
	}
	var review deliveryReview
	serveDecode(t, server, "/api/delivery/review?project=delivery&plan=.tusker/scratch/delivery-plan-v2.yaml", &review)
	beforeRepo := snapshotTree(t, repo)
	beforeState := snapshotTree(t, DefaultStateRoot())
	originalHook := serveDeliveryPlanPhaseHook
	var restore func()
	swapped := false
	serveDeliveryPlanPhaseHook = func(phase string) {
		if !swapped && phase == "commit" {
			swapped = true
			restore = swapServeDeliveryPlanWithSymlink(t, path, external)
		}
	}
	defer func() {
		serveDeliveryPlanPhaseHook = originalHook
		if restore != nil {
			restore()
		}
	}()

	finishMutations := beginServeDeliveryMutationTracking(t)
	body := `{"plan":".tusker/scratch/delivery-plan-v2.yaml","confirm":"` + review.Start.PlanFingerprint + `","planIdentity":"` + review.Start.PlanIdentity + `"}`
	request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:7420/api/delivery/start?project=delivery", bytes.NewBufferString(body))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	if restore != nil {
		restore()
	}
	mutatedVaults := finishMutations()
	if !swapped || recorder.Code != http.StatusConflict || !strings.Contains(recorder.Body.String(), "exact import preimages were restored") {
		t.Fatalf("commit-boundary swap was not fully rolled back: swapped=%t status=%d body=%s", swapped, recorder.Code, recorder.Body.String())
	}
	if len(mutatedVaults) != 0 {
		t.Fatalf("rejected commit-boundary Start published mutation callbacks: %v", mutatedVaults)
	}
	assertSnapshotEqual(t, beforeRepo, snapshotTree(t, repo), "commit-boundary repository preimages")
	assertSnapshotEqual(t, beforeState, snapshotTree(t, DefaultStateRoot()), "commit-boundary runtime state")
	if info, err := os.Stat(specPath); err != nil {
		t.Fatal(err)
	} else if info.Mode().Perm() != 0o600 {
		t.Fatalf("commit-boundary rollback changed spec mode: got %o want 600", info.Mode().Perm())
	}
}

func TestServeDeliveryPostCommitSwapRestoresEveryPreimage(t *testing.T) {
	server, repo := newServeDeliveryFixture(t)
	vault := server.vaultPath
	plan := validDeliveryPlanV2()
	path := writeDeliveryV2TestPlan(t, vault, plan)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	external := filepath.Join(t.TempDir(), "external.yaml")
	if err := writeText(external, string(raw)); err != nil {
		t.Fatal(err)
	}
	var review deliveryReview
	serveDecode(t, server, "/api/delivery/review?project=delivery&plan=.tusker/scratch/delivery-plan-v2.yaml", &review)
	beforeRepo := snapshotTree(t, repo)
	beforeState := snapshotTree(t, DefaultStateRoot())
	originalHook := deliveryStartAfterImportUnlock
	var restore func()
	swapped := false
	deliveryStartAfterImportUnlock = func() {
		if !swapped {
			swapped = true
			restore = swapServeDeliveryPlanWithSymlink(t, path, external)
		}
	}
	defer func() {
		deliveryStartAfterImportUnlock = originalHook
		if restore != nil {
			restore()
		}
	}()

	finishMutations := beginServeDeliveryMutationTracking(t)
	body := `{"plan":".tusker/scratch/delivery-plan-v2.yaml","confirm":"` + review.Start.PlanFingerprint + `","planIdentity":"` + review.Start.PlanIdentity + `"}`
	request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:7420/api/delivery/start?project=delivery", bytes.NewBufferString(body))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	if restore != nil {
		restore()
	}
	mutatedVaults := finishMutations()
	if !swapped || recorder.Code != http.StatusConflict || !strings.Contains(recorder.Body.String(), "exact import preimages were restored") {
		t.Fatalf("post-commit swap was not fully rolled back: swapped=%t status=%d body=%s", swapped, recorder.Code, recorder.Body.String())
	}
	if len(mutatedVaults) != 0 {
		t.Fatalf("rejected post-commit Start published mutation callbacks: %v", mutatedVaults)
	}
	assertSnapshotEqual(t, beforeRepo, snapshotTree(t, repo), "post-commit repository preimages")
	assertSnapshotEqual(t, beforeState, snapshotTree(t, DefaultStateRoot()), "post-commit runtime state")
}

func TestServeDeliveryImportRollbackFailureIsActionableAndLeavesNoPartialDocument(t *testing.T) {
	server, repo := newServeDeliveryFixture(t)
	vault := server.vaultPath
	plan := validDeliveryPlanV2()
	path := writeDeliveryV2TestPlan(t, vault, plan)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	external := filepath.Join(t.TempDir(), "external.yaml")
	if err := writeText(external, string(raw)); err != nil {
		t.Fatal(err)
	}
	var review deliveryReview
	serveDecode(t, server, "/api/delivery/review?project=delivery&plan=.tusker/scratch/delivery-plan-v2.yaml", &review)
	beforeState := snapshotTree(t, DefaultStateRoot())
	originalPhaseHook := serveDeliveryPlanPhaseHook
	originalRollbackHook := deliveryImportRollbackWriteHook
	var restore func()
	swapped := false
	rollbackFailed := false
	serveDeliveryPlanPhaseHook = func(phase string) {
		if !swapped && phase == "commit" {
			swapped = true
			restore = swapServeDeliveryPlanWithSymlink(t, path, external)
		}
	}
	deliveryImportRollbackWriteHook = func(path string) error {
		if !rollbackFailed && strings.Contains(path, filepath.Join("work", "epics")) {
			rollbackFailed = true
			return errors.New("forced exact restoration failure")
		}
		return nil
	}
	defer func() {
		serveDeliveryPlanPhaseHook = originalPhaseHook
		deliveryImportRollbackWriteHook = originalRollbackHook
		if restore != nil {
			restore()
		}
	}()

	finishMutations := beginServeDeliveryMutationTracking(t)
	body := `{"plan":".tusker/scratch/delivery-plan-v2.yaml","confirm":"` + review.Start.PlanFingerprint + `","planIdentity":"` + review.Start.PlanIdentity + `"}`
	request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:7420/api/delivery/start?project=delivery", bytes.NewBufferString(body))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	if restore != nil {
		restore()
	}
	mutatedVaults := finishMutations()
	if !swapped || !rollbackFailed || recorder.Code != http.StatusConflict ||
		!strings.Contains(recorder.Body.String(), "exact rollback could not be proven") ||
		!strings.Contains(recorder.Body.String(), "restore every reported path") {
		t.Fatalf("rollback failure lost its fail-closed repair contract: swapped=%t rollbackFailed=%t status=%d body=%s", swapped, rollbackFailed, recorder.Code, recorder.Body.String())
	}
	if len(mutatedVaults) != 0 {
		t.Fatalf("unproven rollback published mutation callbacks: %v", mutatedVaults)
	}
	epicPath := filepath.Join(vault, "work", "epics", "VTP.md")
	epicRaw, err := os.ReadFile(epicPath)
	if err != nil {
		t.Fatalf("irrecoverable document was not retained as one complete file: %v", err)
	}
	if _, _, err := parseFrontmatter(string(epicRaw)); err != nil {
		t.Fatalf("irrecoverable document is partial or malformed: %v\n%s", err, epicRaw)
	}
	for _, path := range []string{
		filepath.Join(vault, "work", "tasks", "VTP-T-0001.md"),
		filepath.Join(vault, "work", "gates", "VTP-G-0001.md"),
		filepath.Join(vault, "work", "waves", "W-0001.md"),
	} {
		if fileExists(path) {
			t.Fatalf("rollback failure retained unrelated partial import document %s", path)
		}
	}
	assertSnapshotEqual(t, beforeState, snapshotTree(t, DefaultStateRoot()), "failed rollback runtime state")
	for rel := range snapshotTree(t, repo) {
		if strings.Contains(rel, ".delivery-") {
			t.Fatalf("failed rollback retained transaction temporary file %s", rel)
		}
	}
}

func TestServeDeliveryAtomicEditorReplacementIsChanged(t *testing.T) {
	server, _ := newServeDeliveryFixture(t)
	vault := server.vaultPath
	plan := validDeliveryPlanV2()
	plan.HumanGates = nil
	path := writeDeliveryV2TestPlan(t, vault, plan)
	var reviewed deliveryReview
	serveDecode(t, server, "/api/delivery/review?project=delivery&plan=.tusker/scratch/delivery-plan-v2.yaml", &reviewed)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	replacement := strings.Replace(string(raw), plan.Summary, "Atomic editor replacement.", 1)
	if replacement == string(raw) {
		t.Fatal("atomic editor fixture did not change plan bytes")
	}
	replacementPath := filepath.Join(filepath.Dir(path), ".delivery-plan-v2.yaml.replacement")
	if err := writeText(replacementPath, replacement); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(replacementPath, path); err != nil {
		t.Fatal(err)
	}
	before := snapshotDeliveryRecords(t, vault)
	body := `{"plan":".tusker/scratch/delivery-plan-v2.yaml","confirm":"` + reviewed.Start.PlanFingerprint + `","planIdentity":"` + reviewed.Start.PlanIdentity + `"}`
	request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:7420/api/delivery/start?project=delivery", bytes.NewBufferString(body))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusConflict || !strings.Contains(strings.ToLower(recorder.Body.String()), "identity changed") {
		t.Fatalf("atomic replacement was not reported as changed: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	assertEqual(t, before, snapshotDeliveryRecords(t, vault), "atomic replacement refusal must not import")

	var current deliveryReview
	serveDecode(t, server, "/api/delivery/review?project=delivery&plan=.tusker/scratch/delivery-plan-v2.yaml", &current)
	if current.Start.PlanIdentity == reviewed.Start.PlanIdentity || current.Start.PlanFingerprint == reviewed.Start.PlanFingerprint {
		t.Fatalf("atomic replacement did not produce a fresh identity and fingerprint: before=%#v after=%#v", reviewed.Start, current.Start)
	}
}

func swapServeDeliveryPlanWithSymlink(t *testing.T, path, target string) func() {
	t.Helper()
	backup := path + ".bound-original"
	if err := os.Rename(path, backup); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		_ = os.Rename(backup, path)
		t.Fatal(err)
	}
	restored := false
	restore := func() {
		if restored {
			return
		}
		restored = true
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		if err := os.Rename(backup, path); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(restore)
	return restore
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
	var reviewed deliveryReview
	serveDecode(t, server, "/api/delivery/review?project=delivery&plan="+rel, &reviewed)
	original := serveDeliveryStartFn
	serveDeliveryStartFn = func(args Args, source *deliveryPlanSource) (deliveryStartResult, error) {
		return deliveryStartWithPlanSource(args, fixedWaveEnvironmentInspector(greenWaveEnvironment()), source)
	}
	t.Cleanup(func() { serveDeliveryStartFn = original })

	post := func() deliveryStartResult {
		request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:7420/api/delivery/start?project=delivery", bytes.NewBufferString(`{"plan":"`+rel+`","confirm":"`+confirm+`","planIdentity":"`+reviewed.Start.PlanIdentity+`"}`))
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
	finishMutations := beginServeDeliveryMutationTracking(t)
	first := post()
	if mutatedVaults := finishMutations(); len(mutatedVaults) != 1 || mutatedVaults[0] != vault {
		t.Fatalf("successful Serve Start did not publish one delayed vault mutation: %v", mutatedVaults)
	}
	beforeReplay := snapshotDeliveryRecords(t, vault)
	second := post()
	if !second.Replayed || second.WaveID != first.WaveID || second.AuthorizationFingerprint != first.AuthorizationFingerprint {
		t.Fatalf("Serve replay did not preserve canonical authorization: first=%#v second=%#v", first, second)
	}
	assertEqual(t, beforeReplay, snapshotDeliveryRecords(t, vault), "Serve replay must not rewrite delivery records")
}

func TestServeDeliveryStartPreservesTypedPreflightRefusal(t *testing.T) {
	server, repo := newServeDeliveryFixture(t)
	vault := server.vaultPath
	plan := validDeliveryPlanV2()
	plan.HumanGates = nil
	path := writeDeliveryV2TestPlan(t, vault, plan)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	rel := filepath.ToSlash(filepath.Join(".tusker", "scratch", filepath.Base(path)))
	var reviewed deliveryReview
	serveDecode(t, server, "/api/delivery/review?project=delivery&plan="+rel, &reviewed)
	env := greenWaveEnvironment()
	env.ProjectEnabled = false
	original := serveDeliveryStartFn
	serveDeliveryStartFn = func(args Args, source *deliveryPlanSource) (deliveryStartResult, error) {
		return deliveryStartWithPlanSource(args, fixedWaveEnvironmentInspector(env), source)
	}
	t.Cleanup(func() { serveDeliveryStartFn = original })

	beforeRepo := snapshotTree(t, repo)
	beforeState := snapshotTree(t, DefaultStateRoot())
	finishMutations := beginServeDeliveryMutationTracking(t)
	request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:7420/api/delivery/start?project=delivery", bytes.NewBufferString(`{"plan":"`+rel+`","confirm":"`+deliveryFingerprint(raw)+`","planIdentity":"`+reviewed.Start.PlanIdentity+`"}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	mutatedVaults := finishMutations()
	if recorder.Code != http.StatusConflict {
		t.Fatalf("blocked Start status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if len(mutatedVaults) != 0 {
		t.Fatalf("preflight-refused Start published mutation callbacks: %v", mutatedVaults)
	}
	assertSnapshotEqual(t, beforeRepo, snapshotTree(t, repo), "preflight-refused repository preimages")
	assertSnapshotEqual(t, beforeState, snapshotTree(t, DefaultStateRoot()), "preflight-refused runtime state")
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

func TestServeDeliveryPostArmFailureRestoresFullTransaction(t *testing.T) {
	server, repo := newServeDeliveryFixture(t)
	vault := server.vaultPath
	specPath := filepath.Join(repo, "docs", "specs", "delivery.md")
	if err := os.Chmod(specPath, 0o640); err != nil {
		t.Fatal(err)
	}
	plan := validDeliveryPlanV2()
	plan.HumanGates = nil
	path := writeDeliveryV2TestPlan(t, vault, plan)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	rel := filepath.ToSlash(filepath.Join(".tusker", "scratch", filepath.Base(path)))
	var reviewed deliveryReview
	serveDecode(t, server, "/api/delivery/review?project=delivery&plan="+rel, &reviewed)
	originalStart := serveDeliveryStartFn
	serveDeliveryStartFn = func(args Args, source *deliveryPlanSource) (deliveryStartResult, error) {
		return deliveryStartWithPlanSource(args, fixedWaveEnvironmentInspector(greenWaveEnvironment()), source)
	}
	t.Cleanup(func() { serveDeliveryStartFn = originalStart })
	originalAfterArm := deliveryStartAfterArmCommit
	deliveryStartAfterArmCommit = func() error {
		return tuskerError(errorInvalidTransition, "forced post-arm delivery failure")
	}
	t.Cleanup(func() { deliveryStartAfterArmCommit = originalAfterArm })

	beforeRepo := snapshotTree(t, repo)
	beforeState := snapshotTree(t, DefaultStateRoot())
	finishMutations := beginServeDeliveryMutationTracking(t)
	request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:7420/api/delivery/start?project=delivery", bytes.NewBufferString(`{"plan":"`+rel+`","confirm":"`+deliveryFingerprint(raw)+`","planIdentity":"`+reviewed.Start.PlanIdentity+`"}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	mutatedVaults := finishMutations()

	if recorder.Code != http.StatusConflict ||
		!strings.Contains(recorder.Body.String(), "forced post-arm delivery failure") ||
		!strings.Contains(recorder.Body.String(), "exact authorization and import preimages were restored") {
		t.Fatalf("post-arm failure lost its full rollback contract: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if len(mutatedVaults) != 0 {
		t.Fatalf("failed arm published mutation callbacks: %v", mutatedVaults)
	}
	assertSnapshotEqual(t, beforeRepo, snapshotTree(t, repo), "failed-arm repository preimages")
	assertSnapshotEqual(t, beforeState, snapshotTree(t, DefaultStateRoot()), "failed-arm runtime state")
	if info, err := os.Stat(specPath); err != nil {
		t.Fatal(err)
	} else if info.Mode().Perm() != 0o640 {
		t.Fatalf("failed-arm rollback changed spec mode: got %o want 640", info.Mode().Perm())
	}
}

func TestServeDeliveryAuthorizationLockCloseFailureRestoresFullTransaction(t *testing.T) {
	for _, tc := range []struct {
		name  string
		match func(vault string, lock *v7DocumentLock) bool
	}{
		{
			name: "material",
			match: func(vault string, lock *v7DocumentLock) bool {
				return filepath.Clean(lock.path) == filepath.Join(vault, "SKILL.md")
			},
		},
		{
			name: "wave",
			match: func(vault string, lock *v7DocumentLock) bool {
				return filepath.Clean(lock.path) == filepath.Join(vault, "work", "waves", "W-0001.md")
			},
		},
		{
			name: "member",
			match: func(vault string, lock *v7DocumentLock) bool {
				return strings.HasPrefix(filepath.Clean(lock.path), filepath.Join(vault, "work", "tasks")+string(os.PathSeparator))
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server, repo := newServeDeliveryFixture(t)
			vault := server.vaultPath
			plan := validDeliveryPlanV2()
			plan.HumanGates = nil
			path := writeDeliveryV2TestPlan(t, vault, plan)
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			rel := filepath.ToSlash(filepath.Join(".tusker", "scratch", filepath.Base(path)))
			var reviewed deliveryReview
			serveDecode(t, server, "/api/delivery/review?project=delivery&plan="+rel, &reviewed)

			originalStart := serveDeliveryStartFn
			serveDeliveryStartFn = func(args Args, source *deliveryPlanSource) (deliveryStartResult, error) {
				return deliveryStartWithPlanSource(args, fixedWaveEnvironmentInspector(greenWaveEnvironment()), source)
			}
			t.Cleanup(func() { serveDeliveryStartFn = originalStart })
			originalClose := closeV7AuthorizationLock
			injected := false
			closeV7AuthorizationLock = func(lock *v7DocumentLock) error {
				err := originalClose(lock)
				if err != nil || injected || !tc.match(vault, lock) {
					return err
				}
				injected = true
				return errors.New("injected " + tc.name + " authorization lock close failure")
			}
			t.Cleanup(func() { closeV7AuthorizationLock = originalClose })

			beforeRepo := snapshotTree(t, repo)
			beforeState := snapshotTree(t, DefaultStateRoot())
			finishMutations := beginServeDeliveryMutationTracking(t)
			request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:7420/api/delivery/start?project=delivery", bytes.NewBufferString(`{"plan":"`+rel+`","confirm":"`+deliveryFingerprint(raw)+`","planIdentity":"`+reviewed.Start.PlanIdentity+`"}`))
			request.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()
			server.ServeHTTP(recorder, request)
			mutatedVaults := finishMutations()

			if !injected || recorder.Code != http.StatusConflict || !strings.Contains(recorder.Body.String(), "authorization locks did not close cleanly") || !strings.Contains(recorder.Body.String(), "exact authorization and import preimages were restored") {
				t.Fatalf("close failure lost fail-closed rollback contract: injected=%t status=%d body=%s", injected, recorder.Code, recorder.Body.String())
			}
			if len(mutatedVaults) != 0 {
				t.Fatalf("close-failed Start published mutation callbacks: %v", mutatedVaults)
			}
			assertSnapshotEqual(t, beforeRepo, snapshotTree(t, repo), "close-failed repository preimages")
			assertSnapshotEqual(t, beforeState, snapshotTree(t, DefaultStateRoot()), "close-failed runtime state")
		})
	}
}

func TestServeDeliveryDoubleFaultsPreserveEverySafeCause(t *testing.T) {
	errorChain := func(t *testing.T, issue *Issue) []string {
		t.Helper()
		if issue == nil {
			t.Fatal("missing typed issue")
		}
		context, ok := issue.Context.(map[string]any)
		if !ok {
			t.Fatalf("missing structured error context: %#v", issue.Context)
		}
		chain, ok := context["error_chain"].([]string)
		if !ok {
			t.Fatalf("missing safe error chain: %#v", context)
		}
		return chain
	}
	assertContainsAll := func(t *testing.T, value string, wants ...string) {
		t.Helper()
		for _, want := range wants {
			if !strings.Contains(value, want) {
				t.Fatalf("%q does not contain %q", value, want)
			}
		}
	}

	t.Run("typed acquisition and material close failures survive successful rollback", func(t *testing.T) {
		finishMutations := beginServeDeliveryMutationTracking(t)
		cause := errors.Join(
			tuskerError("CAS_BUSY", "typed authorization lock acquisition failure"),
			errors.New("injected material authorization lock close failure"),
		)
		authority := &deliveryStartAuthority{ImportCommit: &deliveryImportCommit{}}
		rolledBack := rollbackDeliveryStartTransaction(authority, cause)
		result := serveCommandResult("tusker delivery start", "", rolledBack)
		if result.OK || !result.Refused || result.Issue == nil || result.Issue.Code != "CAS_BUSY" {
			t.Fatalf("double fault lost primary typed refusal: %#v", result)
		}
		assertContainsAll(t, result.Reason, "typed authorization lock acquisition failure", "injected material authorization lock close failure", "exact import preimages were restored")
		assertContainsAll(t, strings.Join(errorChain(t, result.Issue), " "), "typed authorization lock acquisition failure", "injected material authorization lock close failure")

		recorder := httptest.NewRecorder()
		serveDeliveryFailure(recorder, rolledBack)
		var response serveDeliveryError
		if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		if response.Error.Code != "CAS_BUSY" {
			t.Fatalf("delivery failure lost primary typed code: %#v", response)
		}
		assertContainsAll(t, response.Error.Message, "typed authorization lock acquisition failure", "injected material authorization lock close failure")
		if mutated := finishMutations(); len(mutated) != 0 {
			t.Fatalf("refusal reporting published mutation callbacks: %v", mutated)
		}
	})

	t.Run("rollback CAS and subsequent close failures both reach action reason and context", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "member.md")
		const concurrent = "concurrent bytes\n"
		if err := os.WriteFile(path, []byte(concurrent), 0o640); err != nil {
			t.Fatal(err)
		}
		commit := &deliveryImportCommit{
			Paths: []string{path},
			Preimages: map[string]deliveryWritePreimage{
				path: {Content: []byte("preimage\n"), Mode: 0o640, Existed: true},
			},
			Written: map[string][]byte{path: []byte("transaction bytes\n")},
		}
		rolledBack := rollbackDeliveryStartTransaction(
			&deliveryStartAuthority{ArmCommit: commit},
			tuskerError(
				"CAS_BUSY",
				`typed delivery refusal token=rollback-secret-value {"token":"json secret value"}`,
				withContext(map[string]any{
					path: "file:" + path,
					filepath.Join(filepath.Dir(path), "other"): `\\private-server\share\context.txt`,
					"token=first-key-secret":                   "first collision value",
					"token=second-key-secret":                  "second collision value",
					"error_chain":                              "primary context chain",
					"detail":                                   `path=` + path + ` {"authorization":"Basic context secret value"}`,
				}),
			),
		)
		combined := errors.Join(
			rolledBack,
			errors.New(`injected subsequent material close failure path=`+path+` {"password":"close secret value"}`),
		)
		result := serveCommandResult("tusker delivery start", "", combined)
		if result.OK || !result.Refused || result.Issue == nil || result.Issue.Code != "CAS_BUSY" {
			t.Fatalf("rollback double fault lost primary typed refusal: %#v", result)
		}
		assertContainsAll(t, result.Reason, "typed delivery refusal", "[redacted]", "exact transaction rollback could not be proven", "committed import bytes changed before restoration", "injected subsequent material close failure")
		chain := strings.Join(errorChain(t, result.Issue), " ")
		assertContainsAll(t, chain, "typed delivery refusal", "committed import bytes changed before restoration", "injected subsequent material close failure")
		context, ok := result.Issue.Context.(map[string]any)
		if !ok {
			t.Fatalf("safe operator context has unexpected shape: %#v", result.Issue.Context)
		}
		for _, key := range []string{"[path]", "[path]#2", "token=[redacted]", "token=[redacted]#2", "primary_error_chain", "error_chain"} {
			if _, exists := context[key]; !exists {
				t.Fatalf("safe operator context lost colliding or reserved key %q: %#v", key, context)
			}
		}
		if strings.Contains(result.Reason, filepath.Dir(path)) {
			t.Fatalf("safe operator reason leaked an absolute transaction path: %s", result.Reason)
		}
		encodedIssue, err := json.Marshal(result.Issue)
		if err != nil {
			t.Fatal(err)
		}
		for _, forbidden := range []string{
			filepath.Dir(path),
			"rollback-secret-value",
			"json secret value",
			"context secret value",
			"close secret value",
			`\\private-server\share`,
			"first-key-secret",
			"second-key-secret",
		} {
			if !strings.Contains(string(encodedIssue), forbidden) {
				continue
			}
			t.Fatalf("safe operator issue leaked an absolute path or secret: %s", encodedIssue)
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(raw) != concurrent {
			t.Fatalf("failed CAS rollback changed concurrent bytes: %q", raw)
		}
	})
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
