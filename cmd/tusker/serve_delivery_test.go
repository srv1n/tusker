package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func newServeDeliveryFixture(t *testing.T) (*serveServer, string) {
	t.Helper()
	vault := deliveryTestVault(t)
	repo := v7RepoRoot(vault)
	store, err := OpenRuntimeStore(filepath.Join(t.TempDir(), "state"))
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
	before := snapshotDeliveryRecords(t, vault)

	var review deliveryReview
	serveDecode(t, server, "/api/delivery/review?project=delivery&plan="+filepath.ToSlash(filepath.Join(".tusker", "scratch", filepath.Base(path))), &review)
	if review.Schema != deliveryReviewSchema || !review.ReadOnly || len(review.What) == 0 || len(review.Proof) == 0 || review.Start.PlanFingerprint == "" {
		t.Fatalf("Serve did not return the canonical five-section review: %#v", review)
	}
	assertEqual(t, before, snapshotDeliveryRecords(t, vault), "review must not import or arm delivery")
	if filepath.Dir(path) != filepath.Join(repo, ".tusker", "scratch") {
		t.Fatal("fixture plan unexpectedly escaped repository")
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
	req := httptest.NewRequest(http.MethodPost, "/api/delivery/start?project=delivery", bytes.NewBufferString(`{"plan":"`+rel+`","confirm":"sha256:stale"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("stale confirmation status=%d body=%s", rec.Code, rec.Body.String())
	}
	assertEqual(t, before, snapshotDeliveryRecords(t, vault), "stale Start must not mutate")
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
		request := httptest.NewRequest(http.MethodPost, "/api/delivery/start?project=delivery", bytes.NewBufferString(`{"plan":"`+rel+`","confirm":"`+confirm+`"}`))
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
