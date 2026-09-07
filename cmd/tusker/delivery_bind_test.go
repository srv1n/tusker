package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func deliveryBindFixture(t *testing.T) (string, deliveryPlanV2, string, map[string]string) {
	t.Helper()
	vault := deliveryTestVault(t)
	plan := validDeliveryPlanV2()
	plan.HumanGates = nil
	second := plan.Tasks[0]
	second.SourceKey, second.Title = "second", "Second held task"
	plan.Tasks = append(plan.Tasks, second)
	path := writeDeliveryV2TestPlan(t, vault, plan)
	if err := deliveryV2ImportCmd(vault, path, Args{"vault": vault, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	idx, err := loadV7Index(vault)
	if err != nil {
		t.Fatal(err)
	}
	mapping := map[string]string{}
	for id, task := range idx.Tasks {
		mapping[stringField(task.Data, "delivery_source_key")] = id
		data, body, err := parseFrontmatterMustRead(task.AbsolutePath)
		if err != nil {
			t.Fatal(err)
		}
		for _, key := range []string{"delivery_source_key", "delivery_plan_scope", "delivery_contract_fingerprint", "wave"} {
			delete(data, key)
		}
		data["state_rev"] = v7StateRev(data, body)
		content, err := serializeDocument(data, body, v7FrontmatterOrder["task"])
		if err != nil {
			t.Fatal(err)
		}
		if err := writeText(task.AbsolutePath, content); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Remove(filepath.Join(vault, "work", "waves", "W-0001.md")); err != nil {
		t.Fatal(err)
	}
	return vault, plan, path, mapping
}

func TestDeliveryBindImportsExactHeldTasksAtomically(t *testing.T) {
	vault, plan, path, mapping := deliveryBindFixture(t)
	args := Args{"vault": vault, "plan": path, "task": deliveryEncodeBindMappings(mapping), "quiet": "true"}
	before := snapshotDeliveryRecords(t, vault)
	args["dry-run"] = "true"
	if err := deliveryBindCmd(args); err != nil {
		t.Fatalf("dry-run bind: %v", err)
	}
	assertEqual(t, before, snapshotDeliveryRecords(t, vault), "dry-run bind remains read-only")
	delete(args, "dry-run")
	if err := deliveryBindCmd(args); err != nil {
		t.Fatalf("bind: %v", err)
	}
	idx, err := loadV7Index(vault)
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range plan.Tasks {
		id := mapping[task.SourceKey]
		got := idx.Tasks[id]
		assertEqual(t, task.SourceKey, stringField(got.Data, "delivery_source_key"), "bound source key")
		assertEqual(t, plan.Scope, stringField(got.Data, "delivery_plan_scope"), "bound scope")
		if stringField(got.Data, "delivery_contract_fingerprint") == "" {
			t.Fatalf("%s omitted contract fingerprint", id)
		}
	}
	if len(idx.Tasks) != len(mapping) {
		t.Fatalf("bind allocated duplicate tasks: %#v", idx.Tasks)
	}
}

func TestDeliveryBindRefusesBadTargetsAndRollsBack(t *testing.T) {
	vault, _, path, mapping := deliveryBindFixture(t)
	before := snapshotDeliveryRecords(t, vault)
	bad := deliveryEncodeBindMappings(map[string]string{"import": mapping["import"], "second": mapping["import"]})
	err := deliveryBindCmd(Args{"vault": vault, "plan": path, "task": bad, "quiet": "true"})
	if err == nil || !strings.Contains(err.Error(), "multiple source keys") {
		t.Fatalf("duplicate target refusal = %v", err)
	}
	assertEqual(t, before, snapshotDeliveryRecords(t, vault), "duplicate target leaves records unchanged")

	data, body, err := parseFrontmatterMustRead(filepath.Join(vault, "work", "tasks", mapping["second"]+".md"))
	if err != nil {
		t.Fatal(err)
	}
	data["readiness"] = "ready"
	data["state_rev"] = v7StateRev(data, body)
	content, err := serializeDocument(data, body, v7FrontmatterOrder["task"])
	if err != nil {
		t.Fatal(err)
	}
	if err := writeText(filepath.Join(vault, "work", "tasks", mapping["second"]+".md"), content); err != nil {
		t.Fatal(err)
	}
	err = deliveryBindCmd(Args{"vault": vault, "plan": path, "task": deliveryEncodeBindMappings(mapping), "quiet": "true"})
	if err == nil || !strings.Contains(err.Error(), "backlog/held") {
		t.Fatalf("non-held target refusal = %v", err)
	}
}

func TestDeliveryBindIgnoresUnrelatedStaleStateRevision(t *testing.T) {
	vault, _, path, mapping := deliveryBindFixture(t)
	foreignPath := filepath.Join(vault, "work", "tasks", mapping["second"]+".md")
	if err := writeText(foreignPath, mustReadIndexTest(t, foreignPath)+"\n<!-- unrelated stale edit -->\n"); err != nil {
		t.Fatal(err)
	}
	// A target is still strict: make the other target valid and use only a
	// single-task plan whose mapped target is current.
	plan := validDeliveryPlanV2()
	plan.HumanGates = nil
	path = writeDeliveryV2TestPlan(t, vault, plan)
	if err := deliveryBindCmd(Args{"vault": vault, "plan": path, "task": "import=" + mapping["import"], "quiet": "true"}); err != nil {
		t.Fatalf("unrelated stale revision blocked bind: %v", err)
	}
}
