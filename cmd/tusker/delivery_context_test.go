package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func TestDeliveryPlanningContext(t *testing.T) {
	t.Run("canonicalizes only exact generated work-stream blocks", func(t *testing.T) {
		const scope = "context-canonical/v1"
		base := "# Governing spec\n\nHuman-owned requirements.\n"
		report := deliveryImportReport{PlanScope: scope, WaveID: "W-0001", TaskMapping: map[string]string{"source": "APP-T-0001"}}
		expected := deliveryContextWorkStreamExpectation{WaveID: "W-0001", TaskSources: map[string]string{"APP-T-0001": "source"}}
		want := strings.TrimRight(base, "\n")
		validNew := renderDeliveryWorkStreams(base, report)
		if got := string(deliveryContextCanonicalDocumentMaterialWithExpectation([]byte(validNew), scope, expected, true)); got != want {
			t.Fatalf("new generated block was not canonicalized:\nwant=%q\n got=%q", want, got)
		}

		begin, end := deliveryScopeMarkers(scope)
		legacyBlock := strings.Join([]string{
			begin, "", "- `[[APP-T-0001]]` implements delivery source `source`.", "", "- `[[W-0001]]` is the imported delivery wave.", "", end,
		}, "\n")
		validLegacy := strings.TrimRight(base, "\n") + "\n\n## Work streams\n\n" + legacyBlock + "\n"
		if got := string(deliveryContextCanonicalDocumentMaterialWithExpectation([]byte(validLegacy), scope, expected, true)); got != want {
			t.Fatalf("legacy generated block was not canonicalized:\nwant=%q\n got=%q", want, got)
		}
		if got := deliveryContextCanonicalDocumentMaterialWithExpectation([]byte(validNew), scope, deliveryContextWorkStreamExpectation{}, false); deliveryFingerprint(got) == deliveryFingerprint([]byte(want)) {
			t.Fatalf("unbound marker was incorrectly treated as generated: %q", string(got))
		}
		emptyScaffold := strings.TrimRight(base, "\n") + "\n\n## Work streams\n"
		if got := string(deliveryContextCanonicalDocumentMaterialWithExpectation([]byte(emptyScaffold), scope, deliveryContextWorkStreamExpectation{}, false)); got != want {
			t.Fatalf("empty pre-import work-stream scaffold was not canonicalized:\nwant=%q\n got=%q", want, got)
		}

		otherScope := "other-context/v1"
		other := renderDeliveryWorkStreams(base, deliveryImportReport{PlanScope: otherScope, WaveID: "W-0002", TaskMapping: map[string]string{"other": "APP-T-0002"}})
		cases := map[string]string{
			"edited payload":               strings.Replace(validNew, "- `[[APP-T-0001]]` implements delivery source `source`.", "- human-edited payload", 1),
			"valid-looking edited source":  strings.Replace(validNew, "source`.", "renamed-source`.", 1),
			"extra payload":                strings.Replace(validNew, "\n\n- `[[W-0001]]`", "\n\nhuman payload\n\n- `[[W-0001]]`", 1),
			"duplicate same-scope block":   validNew + "\n\n" + validNew,
			"misordered markers":           strings.Replace(validNew, begin, end, 1),
			"unterminated marker":          strings.Replace(validNew, end, "", 1),
			"other scope":                  other,
			"later human terminal heading": validNew + "\n\n## Work streams\n",
		}
		for name, raw := range cases {
			t.Run(name, func(t *testing.T) {
				got := deliveryContextCanonicalDocumentMaterialWithExpectation([]byte(raw), scope, expected, true)
				if deliveryFingerprint(got) == deliveryFingerprint([]byte(want)) {
					t.Fatalf("non-generated or changed payload was incorrectly erased: %q", string(got))
				}
			})
		}
	})

	t.Run("generated import keeps review context current but human edits still drift", func(t *testing.T) {
		vault := deliveryContextTestVault(t)
		plan := validDeliveryPlanV2()
		plan.HumanGates = nil
		specPath := filepath.Join(v7RepoRoot(vault), filepath.FromSlash(plan.SpecRefs[0]))
		if err := writeText(specPath, "# Delivery\n\nA spec with no pre-existing work-stream heading.\n"); err != nil {
			t.Fatal(err)
		}
		path := writeDeliveryV2TestPlan(t, vault, plan)
		before, err := buildDeliveryPlanningContextForScope(vault, strings.Join(plan.SpecRefs, ","), plan.Scope)
		if err != nil {
			t.Fatal(err)
		}
		if err := deliveryImportCmd(Args{"vault": vault, "plan": path, "quiet": "true"}); err != nil {
			t.Fatal(err)
		}
		after, err := buildDeliveryPlanningContextForScope(vault, strings.Join(plan.SpecRefs, ","), plan.Scope)
		if err != nil {
			t.Fatal(err)
		}
		if after.ContextFingerprint != before.ContextFingerprint {
			t.Fatalf("generated delivery-import block changed planning context: before=%s after=%s", before.ContextFingerprint, after.ContextFingerprint)
		}
		review, err := buildDeliveryReviewWithInspector(vault, path, fixedWaveEnvironmentInspector(greenWaveEnvironment()))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(strings.Join(review.Start.Blockers, "\n"), "planning context fingerprint differs") {
			t.Fatalf("same plan became stale immediately after import: %#v", review.Start)
		}
		if err := deliveryImportCmd(Args{"vault": vault, "plan": path, "quiet": "true"}); err != nil {
			t.Fatal(err)
		}
		review, err = buildDeliveryReviewWithInspector(vault, path, fixedWaveEnvironmentInspector(greenWaveEnvironment()))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(strings.Join(review.Start.Blockers, "\n"), "planning context fingerprint differs") {
			t.Fatalf("idempotent import made the same plan stale: %#v", review.Start)
		}

		spec, err := readText(specPath)
		if err != nil {
			t.Fatal(err)
		}
		if err := writeText(specPath, spec+"\nHuman-authored acceptance changed.\n"); err != nil {
			t.Fatal(err)
		}
		review, err = buildDeliveryReviewWithInspector(vault, path, fixedWaveEnvironmentInspector(greenWaveEnvironment()))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(strings.Join(review.Start.Blockers, "\n"), "planning context fingerprint differs") {
			t.Fatalf("human spec edit did not invalidate reviewed context: %#v", review.Start)
		}
	})

	t.Run("plan scope ignores its own imported records but not other work", func(t *testing.T) {
		vault := deliveryContextTestVault(t)
		plan := validDeliveryPlanV2()
		plan.HumanGates = nil

		// This is genuinely related work, but it belongs to no delivery scope.
		// The scope filter must not hide it merely because it cites the same spec.
		if err := newV7Task(Args{
			"vault": vault, "quiet": "true", "epic": "APP", "id": "APP-T-0001", "title": "Pre-existing related work",
			"risk": "medium", "priority": "p1", "domains": "project", "spec-refs": plan.SpecRefs[0], "v7": "true",
		}); err != nil {
			t.Fatal(err)
		}

		before, err := buildDeliveryPlanningContextForScope(vault, strings.Join(plan.SpecRefs, ","), plan.Scope)
		if err != nil {
			t.Fatal(err)
		}
		assertDeliveryContextIDs(t, before.DuplicateTaskClues, []string{"APP-T-0001"})
		plan.ContextFingerprint = before.ContextFingerprint
		path := writeDeliveryV2TestPlan(t, vault, plan)
		if err := deliveryImportCmd(Args{"vault": vault, "plan": path, "quiet": "true"}); err != nil {
			t.Fatal(err)
		}
		after, err := buildDeliveryPlanningContextForScope(vault, strings.Join(plan.SpecRefs, ","), plan.Scope)
		if err != nil {
			t.Fatal(err)
		}
		if after.ContextFingerprint != before.ContextFingerprint {
			t.Fatalf("own held import changed scoped planning context: before=%s after=%s", before.ContextFingerprint, after.ContextFingerprint)
		}
		assertDeliveryContextIDs(t, after.DuplicateTaskClues, []string{"APP-T-0001"})

		if err := newV7Task(Args{
			"vault": vault, "quiet": "true", "epic": "APP", "id": "APP-T-0002", "title": "Later related work",
			"risk": "medium", "priority": "p1", "domains": "project", "spec-refs": plan.SpecRefs[0], "v7": "true",
		}); err != nil {
			t.Fatal(err)
		}
		drifted, err := buildDeliveryPlanningContextForScope(vault, strings.Join(plan.SpecRefs, ","), plan.Scope)
		if err != nil {
			t.Fatal(err)
		}
		if drifted.ContextFingerprint == before.ContextFingerprint {
			t.Fatal("unscoped related work did not invalidate scoped planning context")
		}
	})

	t.Run("bounded deterministic and read only", func(t *testing.T) {
		vault := deliveryContextTestVault(t)
		repo := v7RepoRoot(vault)
		specRef := ".tusker/specs/planning-context.md"
		specPath := filepath.Join(repo, filepath.FromSlash(specRef))
		specText := `---
title: Planning context
subject: planning-context
part_of: overview
domains:
  - project
decision_refs:
  - APP-D-0001
---

# Planning context

Deliver a bounded repository-fact packet. [[APP-D-0001]]
`
		if err := writeText(specPath, specText); err != nil {
			t.Fatal(err)
		}
		if err := writeText(filepath.Join(repo, ".tusker", "specs", "unrelated.md"), "# Unrelated\n"); err != nil {
			t.Fatal(err)
		}
		if err := newV7Decision(Args{
			"vault": vault, "quiet": "true", "epic": "APP", "title": "Keep planning context bounded",
			"decision": "Only cited documents and routed metadata may enter the planning packet.",
		}); err != nil {
			t.Fatal(err)
		}
		if err := newV7Task(Args{
			"vault": vault, "quiet": "true", "epic": "APP", "id": "APP-T-0001", "title": "Existing bounded context",
			"risk": "high", "priority": "p0", "domains": "project", "spec-refs": specRef, "v7": "true",
		}); err != nil {
			t.Fatal(err)
		}
		setDeliveryContextTaskFields(t, vault, "APP-T-0001", map[string]any{
			"owned_paths":       []string{"cmd/tusker/delivery_context_cmd.go"},
			"generated_outputs": []string{".tusker/_generated/context.json"},
			"migration_keys":    []string{"delivery-context-v1"},
			"resource_refs":     []string{"go-test-gate"},
			"artifact_contract": map[string]any{"kind": "behavior_matrix", "path": "cmd/tusker/delivery_context_test.go"},
		}, "")
		if err := newV7Task(Args{
			"vault": vault, "quiet": "true", "epic": "APP", "id": "APP-T-0002", "title": "UNRELATED-TITLE-MARKER",
			"risk": "low", "priority": "p2", "domains": "project", "spec-refs": ".tusker/specs/unrelated.md", "v7": "true",
		}); err != nil {
			t.Fatal(err)
		}
		setDeliveryContextTaskFields(t, vault, "APP-T-0002", nil, "\nUNRELATED-BODY-SECRET-MARKER\n")
		if err := newV7Gate(Args{
			"vault": vault, "quiet": "true", "id": "APP-G-0001", "blocks": "APP-T-0001", "kind": "auth",
			"owner": "human:owner", "title": "Authorize external account access", "action": "Authorize access to the owner's external account.",
			"verification": "The account owner confirms authorization is granted.", "why-agent-cannot": "Only the account owner can grant external account authorization.",
		}); err != nil {
			t.Fatal(err)
		}
		if err := writeText(filepath.Join(repo, ".env"), "PLANNING_SECRET_VALUE=never-emit\n"); err != nil {
			t.Fatal(err)
		}
		if err := writeText(filepath.Join(vault, "scratch", "raw-planning.log"), "RAW-LOG-SECRET-MARKER\n"); err != nil {
			t.Fatal(err)
		}

		wfFile, err := loadWorkflow(vault)
		if err != nil {
			t.Fatal(err)
		}
		wf := wfFile.Data
		wf.Workspace.MaxLiveWorktrees = 4
		wf.Orchestration.SharedNamespaces = []string{"go.sum"}
		wf.Orchestration.Gate = GateTierPolicy{
			Profile: "default", HarvestCommands: []string{
				"TOKEN=$TOKEN PASSWORD=VERY-SENSITIVE-PASSWORD-VALUE go test ./...",
				"curl -H 'Authorization: Bearer VERY-SENSITIVE-HEADER-VALUE' https://example.invalid",
				"curl https://user:VERY-SENSITIVE-URL-VALUE@example.invalid/health",
				"go test ./...",
			}, BuildSlotLocks: []string{".tusker/scratch/go-build.lock"},
			MinFreeDiskGB: 2, Scopes: []GateScope{{
				Name: "cli", Paths: []string{"cmd/tusker/"}, Commands: []string{
					"TOKEN=VERY-SENSITIVE-TOKEN-VALUE go test ./cmd/tusker",
					"curl --token VERY-SENSITIVE-FLAG-VALUE https://example.invalid",
					"CGO_ENABLED=0 go test ./cmd/tusker -run '^TestDeliveryPlanningContext' -count=1",
				},
			}},
		}
		writeDeliveryContextWorkflow(t, vault, wf, wfFile.Body)
		if _, err := setProjectLocalConfigWithReadback(vault, "automation.enabled", true); err != nil {
			t.Fatal(err)
		}
		workflowRaw, err := os.ReadFile(workflowPath(vault))
		if err != nil {
			t.Fatal(err)
		}
		localConfigRaw, err := os.ReadFile(managedTuskerLocalConfigPath(vault))
		if err != nil {
			t.Fatal(err)
		}
		forbiddenConfigFingerprints := []string{deliveryFingerprint(workflowRaw), deliveryFingerprint(localConfigRaw)}

		stateRoot := filepath.Join(t.TempDir(), "state")
		store, err := OpenRuntimeStore(stateRoot)
		if err != nil {
			t.Fatal(err)
		}
		project := newRegisteredProject(repo, vault)
		project.Enabled = true
		project.Health = projectHealthHealthy
		if err := store.UpsertProject(project); err != nil {
			_ = store.Close()
			t.Fatal(err)
		}
		if runs, err := store.ListRuns(); err != nil || len(runs) != 0 {
			_ = store.Close()
			t.Fatalf("fixture must begin with zero runtime rows: runs=%d err=%v", len(runs), err)
		}
		if err := store.Close(); err != nil {
			t.Fatal(err)
		}
		restoreStateRoot := deliveryContextStateRoot
		deliveryContextStateRoot = func() string { return stateRoot }
		t.Cleanup(func() { deliveryContextStateRoot = restoreStateRoot })

		repoBefore := deliveryContextTreeFingerprint(t, repo)
		stateBefore := deliveryContextTreeFingerprint(t, stateRoot)
		command, args := parseCLI([]string{"tusker", "delivery", "context", "--vault", vault, "--spec", specRef, "--json"})
		if command != "delivery context" {
			t.Fatalf("CLI did not route delivery context: %q", command)
		}
		first := captureStdout(t, func() {
			if _, err := runInner(command, args); err != nil {
				t.Fatal(err)
			}
		})
		second := captureStdout(t, func() {
			if _, err := runInner(command, args); err != nil {
				t.Fatal(err)
			}
		})
		if first != second {
			t.Fatalf("unchanged planning context is not byte-stable\nfirst=%s\nsecond=%s", first, second)
		}
		if got := deliveryContextTreeFingerprint(t, repo); got != repoBefore {
			t.Fatalf("read-only context changed repository tree: before=%s after=%s", repoBefore, got)
		}
		if got := deliveryContextTreeFingerprint(t, stateRoot); got != stateBefore {
			t.Fatalf("read-only context changed runtime tree: before=%s after=%s", stateBefore, got)
		}

		var report deliveryPlanningContext
		if err := json.Unmarshal([]byte(first), &report); err != nil {
			t.Fatal(err)
		}
		if report.Schema != deliveryPlanningContextSchema || report.ContextFingerprint == "" || !report.ReadOnly {
			t.Fatalf("planning context identity missing: %#v", report)
		}
		factory, err := embeddedFactoryIntakeContractProvenance()
		if err != nil {
			t.Fatal(err)
		}
		if report.PlanContract.FactoryIntakeContract != factory {
			t.Fatalf("planning context omitted current factory contract provenance: %#v", report.PlanContract)
		}
		if !report.Readiness.NoWorkDispatched || report.Readiness.DispatchAuthorized || !report.Readiness.AutomationEnabled {
			t.Fatalf("read-only automation projection is unsafe: %#v", report.Readiness)
		}
		if report.Readiness.RegistrationState != "registered" || report.Readiness.ProjectEnabled == nil || !*report.Readiness.ProjectEnabled {
			t.Fatalf("registration projection missing: %#v", report.Readiness)
		}
		assertDeliveryContextIDs(t, report.DuplicateTaskClues, []string{"APP-T-0001"})
		assertDeliveryContextEpicIDs(t, report.EpicCandidates, []string{"APP"})
		assertDeliveryContextGateIDs(t, report.HumanGates, []string{"APP-G-0001"})
		if len(report.Decisions) != 1 || report.Decisions[0].Ref != "APP-D-0001" {
			t.Fatalf("explicit decision citation missing: %#v", report.Decisions)
		}
		if len(report.TestCommands.Focused) != 1 || len(report.TestCommands.Integration) != 1 {
			t.Fatalf("configured test commands missing: %#v", report.TestCommands)
		}
		if len(report.RunnerProfiles) == 0 || len(report.KnowledgeDomains) != 1 || !report.KnowledgeDomains[0].Complete {
			t.Fatalf("runner or knowledge projection missing: profiles=%#v domains=%#v", report.RunnerProfiles, report.KnowledgeDomains)
		}
		if report.PlanContract.AuthoringSchema != deliveryPlanV2Schema || len(report.PlanContract.ValidationRules) < 10 {
			t.Fatalf("delivery authoring contract incomplete: %#v", report.PlanContract)
		}
		for _, marker := range append([]string{
			repo, vault, stateRoot, "PLANNING_SECRET_VALUE", "RAW-LOG-SECRET-MARKER", "UNRELATED-TITLE-MARKER", "UNRELATED-BODY-SECRET-MARKER",
			"VERY-SENSITIVE-TOKEN-VALUE", "VERY-SENSITIVE-PASSWORD-VALUE", "VERY-SENSITIVE-FLAG-VALUE",
			"VERY-SENSITIVE-HEADER-VALUE", "VERY-SENSITIVE-URL-VALUE",
		}, forbiddenConfigFingerprints...) {
			if strings.Contains(first, marker) {
				t.Fatalf("bounded packet leaked %q:\n%s", marker, first)
			}
		}
		if !strings.Contains(first, "CGO_ENABLED=0") {
			t.Fatalf("harmless exact assignment was redacted:\n%s", first)
		}
		if !deliveryContextHasUnknown(report.Unknowns, "test_command") {
			t.Fatalf("sensitive configured commands were not represented as typed unknowns: %#v", report.Unknowns)
		}
		for _, clue := range report.DuplicateTaskClues {
			if len(clue.Provenance) == 0 {
				t.Fatalf("task clue lacks provenance: %#v", clue)
			}
		}
		for _, domain := range report.KnowledgeDomains {
			if len(domain.Provenance) == 0 {
				t.Fatalf("knowledge domain lacks provenance: %#v", domain)
			}
		}

		readStore, err := OpenRuntimeStoreReadOnly(stateRoot)
		if err != nil {
			t.Fatal(err)
		}
		runs, err := readStore.ListRuns()
		if closeErr := readStore.Close(); err == nil {
			err = closeErr
		}
		if err != nil || len(runs) != 0 {
			t.Fatalf("planning context created runtime rows: runs=%d err=%v", len(runs), err)
		}

		wf.Orchestration.Gate.HarvestCommands[0] = "TOKEN=$TOKEN PASSWORD=ROTATED-SENSITIVE-PASSWORD-VALUE go test ./..."
		wf.Orchestration.Gate.Scopes[0].Commands[0] = "TOKEN=ROTATED-SENSITIVE-TOKEN-VALUE go test ./cmd/tusker"
		writeDeliveryContextWorkflow(t, vault, wf, wfFile.Body)
		afterSecretRotation, err := buildDeliveryPlanningContext(vault, specRef)
		if err != nil {
			t.Fatal(err)
		}
		if afterSecretRotation.ContextFingerprint != report.ContextFingerprint {
			t.Fatalf("secret-only config rotation changed material fingerprint: want=%s got=%s", report.ContextFingerprint, afterSecretRotation.ContextFingerprint)
		}
		rotatedJSON, err := json.Marshal(afterSecretRotation)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(rotatedJSON), "ROTATED-SENSITIVE") {
			t.Fatalf("rotated config secret leaked into packet: %s", rotatedJSON)
		}

		volatile := report
		volatile.Readiness.DaemonAlive = !volatile.Readiness.DaemonAlive
		volatile.Readiness.RegistrationState = "not_registered"
		volatile.Readiness.RuntimeStorePresent = !volatile.Readiness.RuntimeStorePresent
		volatile.Readiness.NoWorkDispatched = !volatile.Readiness.NoWorkDispatched
		if got := deliveryContextMaterialFingerprint(volatile); got != report.ContextFingerprint {
			t.Fatalf("runtime liveness changed material fingerprint: want=%s got=%s", report.ContextFingerprint, got)
		}

		if err := writeText(specPath, specText+"\nMaterial spec change.\n"); err != nil {
			t.Fatal(err)
		}
		afterSpec, err := buildDeliveryPlanningContext(vault, specRef)
		if err != nil {
			t.Fatal(err)
		}
		if afterSpec.ContextFingerprint == report.ContextFingerprint {
			t.Fatal("material spec change did not invalidate context fingerprint")
		}
		canonPath := filepath.Join(vault, "knowledge", "domains", "project", "CANON.md")
		canon, err := readText(canonPath)
		if err != nil {
			t.Fatal(err)
		}
		if err := writeText(canonPath, canon+"\nMaterial knowledge change.\n"); err != nil {
			t.Fatal(err)
		}
		afterKnowledge, err := buildDeliveryPlanningContext(vault, specRef)
		if err != nil {
			t.Fatal(err)
		}
		if afterKnowledge.ContextFingerprint == afterSpec.ContextFingerprint {
			t.Fatal("material knowledge change did not invalidate context fingerprint")
		}
		wfFile, err = loadWorkflow(vault)
		if err != nil {
			t.Fatal(err)
		}
		wf = wfFile.Data
		wf.Orchestration.Gate.HarvestCommands = []string{"go test ./cmd/tusker ./internal/..."}
		writeDeliveryContextWorkflow(t, vault, wf, wfFile.Body)
		afterGate, err := buildDeliveryPlanningContext(vault, specRef)
		if err != nil {
			t.Fatal(err)
		}
		if afterGate.ContextFingerprint == afterKnowledge.ContextFingerprint {
			t.Fatal("material test/gate policy change did not invalidate context fingerprint")
		}
	})

	t.Run("typed unknowns and bounded paths", func(t *testing.T) {
		vault := deliveryTestVault(t)
		repo := v7RepoRoot(vault)
		specRef := ".tusker/specs/missing-context.md"
		if err := writeText(filepath.Join(repo, filepath.FromSlash(specRef)), `---
title: Missing context
subject: missing-context
part_of: overview
domains:
  - missing
---

# Missing context
`); err != nil {
			t.Fatal(err)
		}
		stateRoot := filepath.Join(t.TempDir(), "absent-state")
		restoreStateRoot := deliveryContextStateRoot
		deliveryContextStateRoot = func() string { return stateRoot }
		t.Cleanup(func() { deliveryContextStateRoot = restoreStateRoot })
		report, err := buildDeliveryPlanningContext(vault, specRef)
		if err != nil {
			t.Fatal(err)
		}
		for _, wanted := range []string{"knowledge_domain", "project_registration", "runtime_readiness", "test_command"} {
			found := false
			for _, unknown := range report.Unknowns {
				if unknown.Kind == wanted && unknown.Reason != "" && unknown.Remedy != "" && unknown.Provenance != nil {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("typed unknown %q missing: %#v", wanted, report.Unknowns)
			}
		}
		if _, profileUnknowns := deliveryContextProfiles(Workflow{}, nil, nil); !deliveryContextHasUnknown(profileUnknowns, "runner_profile") {
			t.Fatalf("missing runner facts were not typed: %#v", profileUnknowns)
		}

		outside := filepath.Join(filepath.Dir(repo), "outside-secret.md")
		if err := writeText(outside, "# Outside\n\nOUTSIDE-SECRET-MARKER\n"); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, filepath.Join(repo, ".tusker", "specs", "escape.md")); err != nil {
			t.Fatal(err)
		}
		for _, escaped := range []string{"../outside-secret.md", ".env", ".tusker/specs/escape.md"} {
			if _, err := buildDeliveryPlanningContext(vault, escaped); err == nil {
				t.Fatalf("unsafe spec ref was accepted: %s", escaped)
			}
		}

		outsideKnowledge := filepath.Join(filepath.Dir(repo), "outside-knowledge.md")
		if err := writeText(outsideKnowledge, "# Outside knowledge\n\nOUTSIDE-KNOWLEDGE-SECRET-MARKER\n"); err != nil {
			t.Fatal(err)
		}
		canonPath := filepath.Join(vault, "knowledge", "domains", "project", "CANON.md")
		if err := os.Remove(canonPath); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outsideKnowledge, canonPath); err != nil {
			t.Fatal(err)
		}
		domains, unknowns := deliveryContextKnowledge(vault, []string{"project"}, nil)
		if len(domains) != 1 || domains[0].Complete || domains[0].CanonFingerprint != "" {
			t.Fatalf("escaped knowledge symlink was projected as complete: %#v", domains)
		}
		if !deliveryContextHasUnknown(unknowns, "knowledge_domain") {
			t.Fatalf("escaped knowledge symlink did not produce a typed unknown: %#v", unknowns)
		}
		projected, err := json.Marshal(struct {
			Domains  []deliveryContextKnowledgeDomain `json:"domains"`
			Unknowns []deliveryContextUnknown         `json:"unknowns"`
		}{Domains: domains, Unknowns: unknowns})
		if err != nil {
			t.Fatal(err)
		}
		for _, marker := range []string{outsideKnowledge, "OUTSIDE-KNOWLEDGE-SECRET-MARKER"} {
			if strings.Contains(string(projected), marker) {
				t.Fatalf("escaped knowledge symlink leaked %q: %s", marker, projected)
			}
		}
	})

	for _, tc := range []struct {
		name             string
		run              *RunStatus
		wantNoDispatched bool
	}{
		{name: "no project runtime rows", wantNoDispatched: true},
		{name: "unrelated active claim", run: &RunStatus{
			ProjectID: "unrelated-project", RecordID: "OTHER-T-0001", ItemID: "OTHER-T-0001",
			LeaseState: string(LeaseStateClaimed), LeaseExpiresAt: time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
		}, wantNoDispatched: true},
		{name: "active project claim", run: &RunStatus{
			RecordID: "APP-T-ACTIVE", ItemID: "APP-T-ACTIVE", LeaseState: string(LeaseStateClaimed),
			LeaseExpiresAt: time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
		}},
		{name: "stale project claim", run: &RunStatus{
			RecordID: "APP-T-STALE", ItemID: "APP-T-STALE", LeaseState: string(LeaseStateClaimed),
			LeaseExpiresAt: time.Now().Add(-time.Hour).UTC().Format(time.RFC3339),
		}},
	} {
		tc := tc
		t.Run("runtime readiness "+tc.name, func(t *testing.T) {
			vault := deliveryContextTestVault(t)
			repo := v7RepoRoot(vault)
			stateRoot := filepath.Join(t.TempDir(), "state")
			store, err := OpenRuntimeStore(stateRoot)
			if err != nil {
				t.Fatal(err)
			}
			project := newRegisteredProject(repo, vault)
			project.Enabled = true
			project.Health = projectHealthHealthy
			if err := store.UpsertProject(project); err != nil {
				_ = store.Close()
				t.Fatal(err)
			}
			if tc.run != nil {
				run := *tc.run
				if run.ProjectID == "" {
					run.ProjectID = project.ProjectID
				}
				if err := store.UpsertRun(run); err != nil {
					_ = store.Close()
					t.Fatal(err)
				}
			}
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			restoreStateRoot := deliveryContextStateRoot
			deliveryContextStateRoot = func() string { return stateRoot }
			t.Cleanup(func() { deliveryContextStateRoot = restoreStateRoot })

			readiness, unknowns := deliveryContextReadiness(vault, true, Workflow{}, nil)
			if readiness.NoWorkDispatched != tc.wantNoDispatched {
				t.Fatalf("project-scoped dispatch projection mismatch: want=%t got=%#v", tc.wantNoDispatched, readiness)
			}
			if deliveryContextHasUnknown(unknowns, "runtime_readiness") {
				t.Fatalf("readable project-scoped runtime rows were projected as unknown: %#v", unknowns)
			}
		})
	}
}

func deliveryContextTestVault(t *testing.T) string {
	t.Helper()
	vault := deliveryTestVault(t)
	if err := writeDefaultWorkflow(vault); err != nil {
		t.Fatal(err)
	}
	wfFile, err := loadWorkflow(vault)
	if err != nil {
		t.Fatal(err)
	}
	wfFile.Data.Workspace.MaxLiveWorktrees = 4
	writeDeliveryContextWorkflow(t, vault, wfFile.Data, wfFile.Body)
	return vault
}

func writeDeliveryContextWorkflow(t *testing.T, vault string, wf Workflow, body string) {
	t.Helper()
	raw, err := yaml.Marshal(wf)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeText(workflowPath(vault), "---\n"+strings.TrimSpace(string(raw))+"\n---\n"+body); err != nil {
		t.Fatal(err)
	}
}

func setDeliveryContextTaskFields(t *testing.T, vault, id string, fields map[string]any, bodySuffix string) {
	t.Helper()
	path := filepath.Join(vault, "work", "tasks", id+".md")
	data, body, err := parseFrontmatterMustRead(path)
	if err != nil {
		t.Fatal(err)
	}
	for key, value := range fields {
		data[key] = value
	}
	body += bodySuffix
	data["state_rev"] = v7StateRev(data, body)
	content, err := serializeDocument(data, body, v7FrontmatterOrder["task"])
	if err != nil {
		t.Fatal(err)
	}
	if err := writeText(path, content); err != nil {
		t.Fatal(err)
	}
}

func deliveryContextTreeFingerprint(t *testing.T, root string) string {
	t.Helper()
	snapshot := snapshotTree(t, root)
	keys := make([]string, 0, len(snapshot))
	for key := range snapshot {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	hash := sha256.New()
	for _, key := range keys {
		_, _ = hash.Write([]byte(key))
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write(snapshot[key])
		_, _ = hash.Write([]byte{0})
	}
	return fmt.Sprintf("sha256:%x", hash.Sum(nil))
}

func assertDeliveryContextIDs(t *testing.T, clues []deliveryContextTaskClue, want []string) {
	t.Helper()
	got := make([]string, 0, len(clues))
	for _, clue := range clues {
		got = append(got, clue.ID)
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("task clues mismatch: want=%v got=%v", want, got)
	}
}

func assertDeliveryContextEpicIDs(t *testing.T, clues []deliveryContextEpicClue, want []string) {
	t.Helper()
	got := make([]string, 0, len(clues))
	for _, clue := range clues {
		got = append(got, clue.ID)
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("epic clues mismatch: want=%v got=%v", want, got)
	}
}

func assertDeliveryContextGateIDs(t *testing.T, gates []deliveryContextHumanGate, want []string) {
	t.Helper()
	got := make([]string, 0, len(gates))
	for _, gate := range gates {
		got = append(got, gate.ID)
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("human gates mismatch: want=%v got=%v", want, got)
	}
}

func deliveryContextHasUnknown(unknowns []deliveryContextUnknown, kind string) bool {
	for _, unknown := range unknowns {
		if unknown.Kind == kind {
			return true
		}
	}
	return false
}
