package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestDeliveryPlanSchemaRoundTrip(t *testing.T) {
	vault := deliveryTestVault(t)
	out := filepath.Join(v7RepoRoot(vault), ".tusker", "scratch", "plan.yaml")
	if err := deliveryPlanCmd(Args{"vault": vault, "spec": "docs/specs/delivery.md", "out": out, "epic": "APP", "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	plan, raw, err := readDeliveryPlan(out)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, deliveryPlanSchema, plan.Schema, "plan schema")
	assertEqual(t, []string{"docs/specs/delivery.md"}, plan.SpecRefs, "plan spec refs")
	if len(plan.Tasks) != 1 || plan.Tasks[0].Artifact.Path == "" || len(plan.Tasks[0].Verification) != 1 {
		t.Fatalf("template omitted required plan fields: %#v\n%s", plan, raw)
	}
	encoded, err := yaml.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	var roundTrip deliveryPlan
	if err := yaml.Unmarshal(encoded, &roundTrip); err != nil {
		t.Fatal(err)
	}
	assertEqual(t, plan, roundTrip, "delivery plan round trip")
}

func TestDeliveryImportAtomicDedupeAndRollback(t *testing.T) {
	vault := deliveryTestVault(t)
	planPath := writeDeliveryTestPlan(t, vault, validDeliveryPlan())

	dryOutput := captureStdout(t, func() {
		if err := deliveryImportCmd(Args{"vault": vault, "plan": planPath, "wave": "Morning", "dry-run": "true", "json": "true"}); err != nil {
			t.Fatal(err)
		}
	})
	for _, want := range []string{"APP-T-0001", "APP-T-0002", "frontiers", "expectedConcurrency", `"inert":true`} {
		assertContainsIndexTest(t, dryOutput, want)
	}
	if fileExists(filepath.Join(vault, "work", "waves", "W-0001.md")) {
		t.Fatal("dry-run wrote a wave")
	}

	if err := deliveryImportCmd(Args{"vault": vault, "plan": planPath, "wave": "Morning", "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	firstPath := filepath.Join(vault, "work", "tasks", "APP-T-0001.md")
	first, _, err := parseFrontmatterMustRead(firstPath)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "schema", stringField(first, "delivery_source_key"), "source key")
	assertEqual(t, "backlog", stringField(first, "status"), "held task status")
	assertEqual(t, "held", stringField(first, "readiness"), "held task readiness")
	assertEqual(t, "W-0001", stringField(first, "wave"), "wave back pointer")
	assertEqual(t, []string{"A1"}, normalizeList(mapField(first, "artifact_contract")["acceptance_ids"]), "artifact acceptance round trip")
	specBody := mustReadIndexTest(t, filepath.Join(v7RepoRoot(vault), "docs", "specs", "delivery.md"))
	for _, want := range []string{"tusker:delivery-import:", "[[APP-T-0001]]", "[[APP-T-0002]]", "[[W-0001]]"} {
		assertContainsIndexTest(t, specBody, want)
	}

	if err := deliveryImportCmd(Args{"vault": vault, "plan": planPath, "wave": "Morning", "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	tasks, err := filepath.Glob(filepath.Join(vault, "work", "tasks", "APP-T-*.md"))
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, 2, len(tasks), "idempotent task count")

	changed := validDeliveryPlan()
	changed.Tasks[0].Outcome = "A changed but still concrete schema outcome."
	planPath = writeDeliveryTestPlan(t, vault, changed)
	if err := deliveryImportCmd(Args{"vault": vault, "plan": planPath, "wave": "Morning", "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	changedBody := mustReadIndexTest(t, firstPath)
	assertContainsIndexTest(t, changedBody, "A changed but still concrete schema outcome.")

	before := snapshotDeliveryRecords(t, vault)
	changed.Tasks[0].Outcome = "Another concrete change that must roll back."
	planPath = writeDeliveryTestPlan(t, vault, changed)
	err = deliveryImportCmd(Args{"vault": vault, "plan": planPath, "wave": "Morning", "quiet": "true", "fail-after-first-write": "true"})
	if err == nil || !strings.Contains(err.Error(), "forced") {
		t.Fatalf("expected forced failure, got %v", err)
	}
	after := snapshotDeliveryRecords(t, vault)
	assertEqual(t, before, after, "atomic rollback snapshot")

	removed := validDeliveryPlan()
	removed.Tasks = removed.Tasks[:1]
	planPath = writeDeliveryTestPlan(t, vault, removed)
	if err := deliveryImportCmd(Args{"vault": vault, "plan": planPath, "wave": "Morning", "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	second, _, err := parseFrontmatterMustRead(filepath.Join(vault, "work", "tasks", "APP-T-0002.md"))
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "", stringField(second, "wave"), "removed task wave back pointer")
	wave, _, err := parseFrontmatterMustRead(filepath.Join(vault, "work", "waves", "W-0001.md"))
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, []string{"APP-T-0001"}, normalizeList(wave["members"]), "changed plan wave members")
	specBody = mustReadIndexTest(t, filepath.Join(v7RepoRoot(vault), "docs", "specs", "delivery.md"))
	if strings.Contains(specBody, "[[APP-T-0002]]") {
		t.Fatal("changed plan leaked removed source mapping into spec back-links")
	}

	secondary := filepath.Join(v7RepoRoot(vault), "docs", "specs", "secondary.md")
	if err := writeText(secondary, "# Secondary\n\n## Work streams\n"); err != nil {
		t.Fatal(err)
	}
	refsChanged := removed
	refsChanged.SpecRefs = []string{"docs/specs/delivery.md", "docs/specs/secondary.md"}
	planPath = writeDeliveryTestPlan(t, vault, refsChanged)
	if err := deliveryImportCmd(Args{"vault": vault, "plan": planPath, "wave": "Morning", "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	assertContainsIndexTest(t, mustReadIndexTest(t, secondary), "[[W-0001]]")
	refsChanged.SpecRefs = []string{"docs/specs/delivery.md"}
	planPath = writeDeliveryTestPlan(t, vault, refsChanged)
	if err := deliveryImportCmd(Args{"vault": vault, "plan": planPath, "wave": "Morning", "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(mustReadIndexTest(t, secondary), "[[W-0001]]") {
		t.Fatal("removed governing ref retained stale delivery backlinks")
	}
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Added governing decision", "decision": "Keep the imported wave identity."}, newV7Decision)
	refsChanged.SpecRefs = []string{"docs/specs/delivery.md", "APP-D-0001"}
	planPath = writeDeliveryTestPlan(t, vault, refsChanged)
	if err := deliveryImportCmd(Args{"vault": vault, "plan": planPath, "wave": "Morning", "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	waves, err := filepath.Glob(filepath.Join(vault, "work", "waves", "W-*.md"))
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, 1, len(waves), "governing-ref change wave count")
	decisionPath := filepath.Join(vault, "work", "decisions", "APP-D-0001.md")
	assertContainsIndexTest(t, mustReadIndexTest(t, decisionPath), "[[W-0001]]")
	refsChanged.SpecRefs = []string{"docs/specs/delivery.md"}
	planPath = writeDeliveryTestPlan(t, vault, refsChanged)
	if err := deliveryImportCmd(Args{"vault": vault, "plan": planPath, "wave": "Morning", "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(mustReadIndexTest(t, decisionPath), "[[W-0001]]") {
		t.Fatal("removed governing decision retained stale delivery backlinks")
	}

	replaced := validDeliveryPlan()
	replaced.Tasks[0].SourceKey = "schema-v2"
	replaced.Tasks[1].SourceKey = "cli-v2"
	replaced.Tasks[1].Dependencies[0].Task = "schema-v2"
	planPath = writeDeliveryTestPlan(t, vault, replaced)
	if err := deliveryImportCmd(Args{"vault": vault, "plan": planPath, "wave": "Morning", "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	waves, err = filepath.Glob(filepath.Join(vault, "work", "waves", "W-*.md"))
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, 1, len(waves), "full source-key replacement wave count")
	wave, _, err = parseFrontmatterMustRead(waves[0])
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "W-0001", stringField(wave, "id"), "stable replacement wave id")
	assertEqual(t, []string{"APP-T-0003", "APP-T-0004"}, normalizeList(wave["members"]), "replacement members")
	for _, oldID := range []string{"APP-T-0001", "APP-T-0002"} {
		old, _, err := parseFrontmatterMustRead(filepath.Join(vault, "work", "tasks", oldID+".md"))
		if err != nil {
			t.Fatal(err)
		}
		assertEqual(t, "", stringField(old, "wave"), oldID+" replacement back pointer")
	}
}

func TestDeliveryManifestRejectsInvalidMatrixWithoutWrites(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*deliveryPlan)
		want   string
	}{
		{"duplicate", func(p *deliveryPlan) { p.Tasks[1].SourceKey = p.Tasks[0].SourceKey }, "duplicate source_key"},
		{"dangling", func(p *deliveryPlan) { p.Tasks[1].Dependencies[0].Task = "missing" }, "dangling dependency"},
		{"cycle", func(p *deliveryPlan) { p.Tasks[0].Dependencies = []deliveryDependency{{Task: "cli", Kind: "hard"}} }, "cycle"},
		{"placeholder acceptance", func(p *deliveryPlan) { p.Tasks[0].Acceptance[0].Outcome = "TBD" }, "acceptance"},
		{"placeholder proof", func(p *deliveryPlan) { p.Tasks[0].Verification[0].Check = "command: <...>" }, "verification"},
		{"artifact", func(p *deliveryPlan) { p.Tasks[0].Artifact.Path = ".tusker/scratch/fake.png" }, "artifact"},
		{"artifact acceptance missing", func(p *deliveryPlan) { p.Tasks[0].Artifact.AcceptanceIDs = nil }, "acceptance_ids"},
		{"artifact acceptance unknown", func(p *deliveryPlan) { p.Tasks[0].Artifact.AcceptanceIDs = []string{"A9"} }, "unknown acceptance"},
		{"outside spec", func(p *deliveryPlan) { p.SpecRefs = []string{"../outside.md"} }, "spec_ref"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			vault := deliveryTestVault(t)
			plan := validDeliveryPlan()
			tc.mutate(&plan)
			path := writeDeliveryTestPlan(t, vault, plan)
			err := deliveryImportCmd(Args{"vault": vault, "plan": path, "quiet": "true"})
			if err == nil || !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(tc.want)) {
				t.Fatalf("expected %q rejection, got %v", tc.want, err)
			}
			if fileExists(filepath.Join(vault, "work", "waves", "W-0001.md")) {
				t.Fatal("invalid import wrote a wave")
			}
		})
	}
}

func TestDeliveryDAGFrontiersAndMapping(t *testing.T) {
	plan := validDeliveryPlan()
	plan.Tasks = append(plan.Tasks, deliveryPlanTask{
		SourceKey: "docs", Title: "Document delivery", Outcome: "Durable truth is routed.",
		Acceptance:   []deliveryAcceptance{{ID: "A1", Outcome: "Canon names the delivery behavior."}},
		Verification: []deliveryVerification{{Covers: "A1", Check: "command: go test ./cmd/tusker -run TestDeliverySkillContract -count=1"}},
		Dependencies: []deliveryDependency{{Task: "schema", Kind: "soft"}},
		Artifact:     deliveryArtifactContract{Kind: "document", Path: "skill/SKILL.md", Summary: "Rendered operator contract.", AcceptanceIDs: []string{"A1"}},
	})
	frontiers, cycle := deliveryFrontiers(plan)
	if cycle {
		t.Fatal("valid graph reported a cycle")
	}
	assertEqual(t, [][]string{{"schema"}, {"cli", "docs"}}, frontiers, "topological frontiers")
	assertEqual(t, 2, deliveryExpectedConcurrency(plan, frontiers), "expected concurrency")
}

func TestDeliveryImportJSONReportsAllPreflightIssues(t *testing.T) {
	vault := deliveryTestVault(t)
	plan := validDeliveryPlan()
	plan.Tasks[0].Acceptance[0].Outcome = "TBD"
	plan.Tasks[0].Artifact.Path = "../escape"
	path := writeDeliveryTestPlan(t, vault, plan)
	err := deliveryImportCmd(Args{"vault": vault, "plan": path, "json": "true"})
	if err == nil {
		t.Fatal("expected invalid delivery plan")
	}
	issue := errorToIssue(err)
	context, ok := issue.Context.(map[string]any)
	if !ok {
		t.Fatalf("missing structured error context: %#v", issue.Context)
	}
	delivery, ok := context["delivery"].(deliveryImportReport)
	if !ok {
		t.Fatalf("missing structured delivery report: %#v", issue.Context)
	}
	if len(delivery.Issues) < 2 || len(delivery.Frontiers) == 0 || len(delivery.TaskMapping) != 2 {
		t.Fatalf("incomplete preflight issue report: %#v", delivery)
	}
}

func TestDeliveryHelpSpecRefsAndSkillContract(t *testing.T) {
	help := captureStdout(t, printV7Help)
	for _, want := range []string{"delivery plan", "delivery import", "inert", "Tusker", "final"} {
		assertContainsIndexTest(t, help, want)
	}
	skill := mustReadIndexTest(t, filepath.Join("..", "..", "skill", "SKILL.md"))
	for _, want := range []string{"explicit stable scope", "source-keyed tasks", "Tusker owns the final records", "never dispatch"} {
		assertContainsIndexTest(t, skill, want)
	}
}

func TestDeliverySpecRefsDecisionBacklinksAndRollback(t *testing.T) {
	vault := deliveryTestVault(t)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Delivery authority", "decision": "Use imported delivery plans."}, newV7Decision)
	plan := validDeliveryPlan()
	plan.SpecRefs = []string{"APP-D-0001"}
	path := writeDeliveryTestPlan(t, vault, plan)
	decisionPath := filepath.Join(vault, "work", "decisions", "APP-D-0001.md")
	before := mustReadIndexTest(t, decisionPath)
	err := deliveryImportCmd(Args{"vault": vault, "plan": path, "quiet": "true", "fail-after-first-write": "true"})
	if err == nil {
		t.Fatal("expected forced decision import failure")
	}
	assertEqual(t, before, mustReadIndexTest(t, decisionPath), "decision rollback")
	if err := deliveryImportCmd(Args{"vault": vault, "plan": path, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	decision := mustReadIndexTest(t, decisionPath)
	for _, want := range []string{"tusker:delivery-import:", "[[APP-T-0001]]", "[[APP-T-0002]]", "[[W-0001]]"} {
		assertContainsIndexTest(t, decision, want)
	}
	data, body, err := parseFrontmatter(decision)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, v7StateRev(data, body), stringField(data, "state_rev"), "decision state revision")
}

func deliveryTestVault(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	vault := filepath.Join(repo, ".tusker")
	mustWave(t, Args{"vault": vault, "quiet": "true"}, bootstrap)
	mustWave(t, Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "Delivery", "summary": "Delivery tests."}, newV7Epic)
	if err := writeText(filepath.Join(repo, "docs", "specs", "delivery.md"), "# Delivery\n\n## Work streams\n\n- Existing context.\n"); err != nil {
		t.Fatal(err)
	}
	return vault
}

func validDeliveryPlan() deliveryPlan {
	return deliveryPlan{
		Schema: deliveryPlanSchema, Scope: "app-delivery", Title: "Delivery", Epic: "APP", SpecRefs: []string{"docs/specs/delivery.md"}, Concurrency: 3, RunnerProfile: "focused",
		Tasks: []deliveryPlanTask{
			{
				SourceKey: "schema", Title: "Define schema", Outcome: "A versioned delivery schema round-trips.",
				Acceptance:   []deliveryAcceptance{{ID: "A1", Outcome: "All required delivery fields survive round-trip."}},
				Verification: []deliveryVerification{{Covers: "A1", Check: "command: go test ./cmd/tusker -run TestDeliveryPlanSchemaRoundTrip -count=1"}},
				Artifact:     deliveryArtifactContract{Kind: "diff_summary", Path: "cmd/tusker/delivery_cmd.go", Summary: "Schema and validation implementation.", AcceptanceIDs: []string{"A1"}},
				OwnedPaths:   []string{"cmd/tusker/delivery_cmd.go"}, KnowledgeNodes: []string{"project/CANON.md"}, Risk: "medium", Priority: "p1", Domains: []string{"project"},
			},
			{
				SourceKey: "cli", Title: "Import graph", Outcome: "The CLI atomically imports the graph.",
				Acceptance:   []deliveryAcceptance{{ID: "A1", Outcome: "Repeated imports preserve stable task IDs."}},
				Verification: []deliveryVerification{{Covers: "A1", Check: "command: go test ./cmd/tusker -run TestDeliveryImportAtomicDedupeAndRollback -count=1"}},
				Dependencies: []deliveryDependency{{Task: "schema", Kind: "hard"}},
				Artifact:     deliveryArtifactContract{Kind: "behavior_matrix", Path: "cmd/tusker/delivery_cmd_test.go", Summary: "Import behavior matrix.", AcceptanceIDs: []string{"A1"}},
				OwnedPaths:   []string{"cmd/tusker/delivery_cmd_test.go"}, Risk: "medium", Priority: "p1", Domains: []string{"project"},
			},
		},
	}
}

func writeDeliveryTestPlan(t *testing.T, vault string, plan deliveryPlan) string {
	t.Helper()
	raw, err := yaml.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(v7RepoRoot(vault), ".tusker", "scratch", "delivery-plan.yaml")
	if err := writeText(path, string(raw)); err != nil {
		t.Fatal(err)
	}
	return path
}

func snapshotDeliveryRecords(t *testing.T, vault string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, pattern := range []string{filepath.Join(vault, "work", "tasks", "*.md"), filepath.Join(vault, "work", "waves", "*.md"), filepath.Join(v7RepoRoot(vault), "docs", "specs", "*.md")} {
		paths, err := filepath.Glob(pattern)
		if err != nil {
			t.Fatal(err)
		}
		for _, path := range paths {
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			out[path] = string(raw)
		}
	}
	return out
}
