package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type crossScopeReviewFixture struct {
	Vault        string
	ConsumerPlan string
	Index        v7Index
	Producer     Note
	Consumer     Note
}

func TestDeliveryCrossScopeReview(t *testing.T) {
	t.Setenv("TUSKER_STATE_ROOT", filepath.Join(t.TempDir(), "state"))
	fixture := newCrossScopeReviewFixture(t, deliveryTestVault(t))
	before := snapshotDeliveryRecords(t, fixture.Vault)

	review, err := buildDeliveryReviewWithInspector(
		fixture.Vault,
		fixture.ConsumerPlan,
		fixedWaveEnvironmentInspector(greenWaveEnvironment()),
	)
	if err != nil {
		t.Fatal(err)
	}
	row := oneCrossScopeReviewDependency(t, review.Flow.CrossScopeDependencies)
	assertCrossScopeReviewIdentity(t, row, fixture)
	if row.TargetIntegrity != "resolved" || row.ProducerLifecycle != "incomplete" ||
		row.BlockerClass != "lifecycle" || row.Satisfied {
		t.Fatalf("incomplete producer classification=%#v", row)
	}
	if row.Repair == "" || !strings.Contains(row.Repair, "Complete producer producer/v1/provider") {
		t.Fatalf("incomplete producer lost actionable repair: %#v", row)
	}

	rendered := renderDeliveryReview(review)
	for _, expected := range []string{
		"Cross-scope hard dependencies",
		"producer/v1/provider",
		"PRD-T-0001",
		row.PersistedContractFingerprint,
		"Producer before consumer",
	} {
		if !strings.Contains(rendered, expected) {
			t.Fatalf("delivery review omitted %q:\n%s", expected, rendered)
		}
	}
	assertCrossScopeReviewHidesStorageKeys(t, rendered)

	var consumerWave Note
	for _, wave := range fixture.Index.Waves {
		if stringField(wave.Data, "delivery_plan_scope") == "consumer/v1" {
			consumerWave = wave
			break
		}
	}
	if consumerWave.Data == nil {
		t.Fatal("consumer wave was not imported")
	}
	preflight := buildWavePreflight(fixture.Vault, fixture.Index, consumerWave, greenWaveEnvironment())
	if !preflight.ReadOnly || preflight.CrossScopeReview.Schema != deliveryCrossScopeReviewSchema ||
		!preflight.CrossScopeReview.ReadOnly {
		t.Fatalf("wave preflight lost the read-only shared projection: %#v", preflight.CrossScopeReview)
	}
	preflightRow := oneCrossScopeReviewDependency(t, preflight.CrossScopeReview.Dependencies)
	assertEqual(t, row, preflightRow, "delivery review and wave preflight must share one projection")
	preflightJSON, err := json.Marshal(map[string]any{"ok": preflight.OK, "preflight": preflight})
	if err != nil {
		t.Fatal(err)
	}
	preflightText := renderWavePreflight(preflight)
	for name, output := range map[string]string{"json": string(preflightJSON), "text": preflightText} {
		for _, expected := range []string{
			"producer/v1",
			"provider",
			"PRD-T-0001",
			"hard",
			row.PersistedContractFingerprint,
			row.ProducerState,
			row.ProducerLifecycle,
			row.Implication,
			row.Repair,
		} {
			if !strings.Contains(output, expected) {
				t.Fatalf("wave preflight %s omitted %q:\n%s", name, expected, output)
			}
		}
		assertCrossScopeReviewHidesStorageKeys(t, output)
	}
	assertEqual(t, before, snapshotDeliveryRecords(t, fixture.Vault), "delivery review projection must be read only")

	t.Run("missing target is structural", func(t *testing.T) {
		idx := cloneCrossScopeReviewIndex(fixture.Index)
		delete(idx.Tasks, row.TaskID)
		missing := oneCrossScopeReviewDependency(t, deliveryCrossScopeReviewForTask(idx, fixture.Consumer).Dependencies)
		if missing.TargetIntegrity != "missing" || missing.BlockerClass != "structural" ||
			missing.ProducerLifecycle != "unknown" || !strings.Contains(missing.Repair, "Import or restore exactly one producer") {
			t.Fatalf("missing target classification=%#v", missing)
		}
	})

	t.Run("contract drift is structural", func(t *testing.T) {
		idx := mutateCrossScopeReviewProducer(fixture, func(producer *Note) {
			producer.Data["delivery_contract_fingerprint"] = "sha256:changed"
		})
		corrupt := oneCrossScopeReviewDependency(t, deliveryCrossScopeReviewForTask(idx, fixture.Consumer).Dependencies)
		if corrupt.TargetIntegrity != "corrupt" || corrupt.BlockerClass != "structural" ||
			!strings.Contains(corrupt.Repair, "Restore the exact durable hard edge and contract fingerprint") ||
			!strings.Contains(corrupt.Repair, "re-import") {
			t.Fatalf("corrupt target classification=%#v", corrupt)
		}
	})

	t.Run("missing provenance does not invent a contract", func(t *testing.T) {
		consumer := fixture.Consumer
		consumer.Data = cloneMap(consumer.Data)
		delete(consumer.Data, "delivery_cross_scope_dependencies")
		missingProvenance := oneCrossScopeReviewDependency(t, deliveryCrossScopeReviewForTask(fixture.Index, consumer).Dependencies)
		if missingProvenance.TargetIntegrity != "corrupt" || missingProvenance.BlockerClass != "structural" ||
			missingProvenance.ContractProvenance != "missing" || missingProvenance.TaskID != "PRD-T-0001" ||
			missingProvenance.PersistedContractFingerprint != "" ||
			!strings.Contains(missingProvenance.Repair, "Restore the exact durable hard edge and contract fingerprint") {
			t.Fatalf("missing provenance classification=%#v", missingProvenance)
		}
	})

	t.Run("failed target is lifecycle", func(t *testing.T) {
		idx := mutateCrossScopeReviewProducer(fixture, func(producer *Note) {
			producer.Data["status"] = "cancelled"
		})
		failed := oneCrossScopeReviewDependency(t, deliveryCrossScopeReviewForTask(idx, fixture.Consumer).Dependencies)
		if failed.TargetIntegrity != "resolved" || failed.ProducerState != "cancelled" ||
			failed.ProducerLifecycle != "failed" || failed.BlockerClass != "lifecycle" ||
			!strings.Contains(failed.Repair, "Repair or reopen producer producer/v1/provider") ||
			strings.Contains(failed.Repair, "re-import") {
			t.Fatalf("failed producer classification=%#v", failed)
		}
	})

	t.Run("done target satisfies the edge", func(t *testing.T) {
		idx := mutateCrossScopeReviewProducer(fixture, func(producer *Note) {
			producer.Data["status"] = "done"
		})
		complete := oneCrossScopeReviewDependency(t, deliveryCrossScopeReviewForTask(idx, fixture.Consumer).Dependencies)
		if complete.TargetIntegrity != "resolved" || complete.ProducerLifecycle != "complete" ||
			complete.BlockerClass != "none" || !complete.Satisfied || complete.Repair != "" {
			t.Fatalf("complete producer classification=%#v", complete)
		}
	})
}

func TestCrossScopeStatusProjection(t *testing.T) {
	stateRoot := filepath.Join(t.TempDir(), "state")
	t.Setenv("TUSKER_STATE_ROOT", stateRoot)
	fixture := newCrossScopeReviewFixture(t, deliveryTestVault(t))
	beforeRepo := snapshotTree(t, v7RepoRoot(fixture.Vault))

	showJSON := captureStdout(t, func() {
		if err := showCmd(Args{"vault": fixture.Vault, "id": "CON-T-0001", "json": "true"}); err != nil {
			t.Fatal(err)
		}
	})
	showJSONAgain := captureStdout(t, func() {
		if err := showCmd(Args{"vault": fixture.Vault, "id": "CON-T-0001", "json": "true"}); err != nil {
			t.Fatal(err)
		}
	})
	assertEqual(t, showJSON, showJSONAgain, "status JSON must be deterministic")
	var status showTaskStatusProjection
	if err := json.Unmarshal([]byte(showJSON), &status); err != nil {
		t.Fatal(err)
	}
	if status.Schema != "tusker.task-status/v1" || !status.ReadOnly || status.ID != "CON-T-0001" {
		t.Fatalf("unexpected task status projection: %#v", status)
	}
	statusRow := oneCrossScopeReviewDependency(t, status.CrossScopeDependencies.Dependencies)
	assertCrossScopeReviewIdentity(t, statusRow, fixture)

	packetJSON := captureStdout(t, func() {
		if err := packetV7Cmd(Args{
			"vault": fixture.Vault, "id": "CON-T-0001", "for": "agent", "force": "true", "json": "true",
		}); err != nil {
			t.Fatal(err)
		}
	})
	var packet v7PacketStatusProjection
	if err := json.Unmarshal([]byte(packetJSON), &packet); err != nil {
		t.Fatal(err)
	}
	if packet.Schema != "tusker.task-packet/v1" || !packet.ReadOnly ||
		packet.TaskID != "CON-T-0001" || packet.Audience != "agent" || packet.Path != "" {
		t.Fatalf("unexpected task packet projection: %#v", packet)
	}
	packetRow := oneCrossScopeReviewDependency(t, packet.CrossScopeDependencies.Dependencies)
	assertEqual(t, statusRow, packetRow, "show and packet must share one cross-scope projection")

	showText := captureStdout(t, func() {
		if err := showCmd(Args{"vault": fixture.Vault, "id": "CON-T-0001", "capsule": "true"}); err != nil {
			t.Fatal(err)
		}
	})
	packetText := captureStdout(t, func() {
		if err := packetV7Cmd(Args{"vault": fixture.Vault, "id": "CON-T-0001", "for": "agent", "force": "true"}); err != nil {
			t.Fatal(err)
		}
	})
	for name, output := range map[string]string{"show": showText, "packet": packetText} {
		for _, expected := range []string{"Cross-scope hard dependencies", "producer/v1/provider", "PRD-T-0001", "Producer before consumer"} {
			if !strings.Contains(output, expected) {
				t.Fatalf("%s omitted %q:\n%s", name, expected, output)
			}
		}
		assertCrossScopeReviewHidesStorageKeys(t, output)
	}
	assertSnapshotEqual(t, beforeRepo, snapshotTree(t, v7RepoRoot(fixture.Vault)), "show and packet read-only projection")
}

func TestServeCrossScopeDependencies(t *testing.T) {
	server, repo := newServeDeliveryFixture(t)
	fixture := newCrossScopeReviewFixture(t, server.vaultPath)
	beforeRepo := snapshotTree(t, repo)
	stateRoot := DefaultStateRoot()
	beforeState := snapshotTree(t, stateRoot)
	beforeDB, err := os.ReadFile(runtimeStoreDBPath(stateRoot))
	if err != nil {
		t.Fatal(err)
	}

	var review deliveryReview
	serveDecode(t, server, "/api/delivery/review?project=delivery&plan=.tusker/scratch/delivery-plan-v2.yaml", &review)
	row := oneCrossScopeReviewDependency(t, review.Flow.CrossScopeDependencies)
	assertCrossScopeReviewIdentity(t, row, fixture)
	if row.TargetIntegrity != "resolved" || row.ProducerLifecycle != "incomplete" ||
		row.BlockerClass != "lifecycle" || row.Repair == "" {
		t.Fatalf("Serve lost current producer lifecycle: %#v", row)
	}
	raw, err := json.Marshal(review)
	if err != nil {
		t.Fatal(err)
	}
	assertCrossScopeReviewHidesStorageKeys(t, string(raw))

	assertSnapshotEqual(t, beforeRepo, snapshotTree(t, repo), "Serve cross-scope review repository")
	assertSnapshotEqual(t, beforeState, snapshotTree(t, stateRoot), "Serve cross-scope review runtime state")
	afterDB, err := os.ReadFile(runtimeStoreDBPath(stateRoot))
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, beforeDB, afterDB, "Serve cross-scope review runtime database bytes")
}

func newCrossScopeReviewFixture(t *testing.T, vault string) crossScopeReviewFixture {
	t.Helper()
	producerPlan := crossScopePlan("producer/v1", "PRD", "provider")
	producerPath := writeDeliveryV2TestPlan(t, vault, producerPlan)
	if err := deliveryV2ImportCmd(vault, producerPath, Args{"vault": vault, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}

	consumerPlan := crossScopePlan("consumer/v1", "CON", "consumer")
	consumerPlan.Tasks[0].Dependencies = []deliveryDependency{{Task: "provider", Kind: "hard"}}
	deliveryV2DependencyScope(&consumerPlan.Tasks[0].Dependencies[0], "producer/v1")
	consumerPath := writeDeliveryV2TestPlan(t, vault, consumerPlan)
	if err := deliveryV2ImportCmd(vault, consumerPath, Args{"vault": vault, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}

	idx, err := loadV7Index(vault)
	if err != nil {
		t.Fatal(err)
	}
	fixture := crossScopeReviewFixture{Vault: vault, ConsumerPlan: consumerPath, Index: idx}
	for _, task := range idx.Tasks {
		switch stringField(task.Data, "delivery_plan_scope") {
		case "producer/v1":
			fixture.Producer = task
		case "consumer/v1":
			fixture.Consumer = task
		}
	}
	if fixture.Producer.Data == nil || fixture.Consumer.Data == nil {
		t.Fatalf("cross-scope fixture import incomplete: producer=%#v consumer=%#v", fixture.Producer.Data, fixture.Consumer.Data)
	}
	return fixture
}

func oneCrossScopeReviewDependency(t *testing.T, rows []deliveryCrossScopeReviewDependency) deliveryCrossScopeReviewDependency {
	t.Helper()
	if len(rows) != 1 {
		t.Fatalf("want one cross-scope dependency, got %#v", rows)
	}
	return rows[0]
}

func assertCrossScopeReviewIdentity(t *testing.T, row deliveryCrossScopeReviewDependency, fixture crossScopeReviewFixture) {
	t.Helper()
	if row.Scope != "producer/v1" || row.SourceKey != "provider" ||
		row.TaskID != "PRD-T-0001" || row.Kind != v7DependencyHardnessHard ||
		row.ContractProvenance != "persisted" || row.PersistedContractFingerprint == "" ||
		row.PersistedContractFingerprint != stringField(fixture.Producer.Data, "delivery_contract_fingerprint") ||
		row.ConsumerTaskID != "CON-T-0001" || row.ConsumerSourceKey != "consumer" ||
		row.ProducerState == "" || row.Implication == "" || row.TaskHref == "" {
		t.Fatalf("cross-scope identity/provenance projection=%#v", row)
	}
	if !strings.Contains(row.Implication, "producer/v1/provider") ||
		!strings.Contains(row.Implication, "PRD-T-0001") ||
		!strings.Contains(row.Implication, "CON-T-0001") {
		t.Fatalf("ordering implication lost qualified identities: %#v", row)
	}
}

func assertCrossScopeReviewHidesStorageKeys(t *testing.T, output string) {
	t.Helper()
	for _, forbidden := range []string{
		"delivery_cross_scope_dependencies",
		"target_contract_fingerprint",
		"delivery_plan_scope",
		"delivery_source_key",
	} {
		if strings.Contains(output, forbidden) {
			t.Fatalf("operator surface leaked storage key %q:\n%s", forbidden, output)
		}
	}
}

func cloneCrossScopeReviewIndex(idx v7Index) v7Index {
	cloned := idx
	cloned.Tasks = make(map[string]Note, len(idx.Tasks))
	for id, task := range idx.Tasks {
		cloned.Tasks[id] = task
	}
	return cloned
}

func mutateCrossScopeReviewProducer(fixture crossScopeReviewFixture, mutate func(*Note)) v7Index {
	idx := cloneCrossScopeReviewIndex(fixture.Index)
	id := stringField(fixture.Producer.Data, "id")
	producer := idx.Tasks[id]
	producer.Data = cloneMap(producer.Data)
	mutate(&producer)
	idx.Tasks[id] = producer
	return idx
}
