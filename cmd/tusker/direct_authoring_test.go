package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

func v7DirectTestVault(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	vault := filepath.Join(repo, ".tusker")
	mustWave(t, Args{"vault": vault, "quiet": "true"}, bootstrap)
	if err := writeDefaultWorkflow(vault); err != nil {
		t.Fatal(err)
	}
	mustWave(t, Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "Delivery", "summary": "Delivery tests."}, newV7Epic)
	if err := writeText(filepath.Join(repo, ".tusker", "specs", "delivery.md"), "---\nsubject: delivery\npart_of: overview\n---\n# Delivery\n\n## Work streams\n\n- Existing context.\n"); err != nil {
		t.Fatal(err)
	}
	return vault
}

func directAuthoringBodyPath(t *testing.T, vault, name, body string) string {
	t.Helper()
	path := filepath.Join(v7RepoRoot(vault), ".tusker", "scratch", name)
	if err := writeText(path, body); err != nil {
		t.Fatal(err)
	}
	return path
}

func directAuthoringStdin(t *testing.T, content string) func() {
	t.Helper()
	pipe, err := os.CreateTemp(t.TempDir(), "stdin-*")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pipe.WriteString(content); err != nil {
		t.Fatal(err)
	}
	if _, err := pipe.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	previous := os.Stdin
	os.Stdin = pipe
	return func() {
		os.Stdin = previous
		_ = pipe.Close()
	}
}

func directAuthoringTaskRecord(t *testing.T, vault, id string) (map[string]any, string) {
	t.Helper()
	data, body, err := parseFrontmatterMustRead(filepath.Join(vault, "work", "tasks", id+".md"))
	if err != nil {
		t.Fatal(err)
	}
	return data, body
}

func assertDirectAuthoringCleanRecord(t *testing.T, data map[string]any) {
	t.Helper()
	for _, field := range []string{"epic", "priority", "size", "risk", "context_fingerprint", "factory_intake_contract_schema", "factory_intake_contract_version", "factory_intake_contract_fingerprint", "delivery_source_key", "delivery_plan_scope", "delivery_plan_fingerprint", "delivery_contract_fingerprint", "authoring_request_key", "authoring_request_fingerprint"} {
		if _, ok := data[field]; ok {
			t.Fatalf("authored record must not contain %s: %#v", field, data)
		}
	}
}

func TestDirectWaveAuthoringNewTaskLevelsAndBodies(t *testing.T) {
	vault := v7DirectTestVault(t)
	levels := []string{"light", "standard", "demanding"}
	bodies := []string{
		"# Light task\n\nSingle sentence body.\n",
		"# Standard task\n\n- item one\n- item two\n\n",
		"# Demanding task\n\nBody with a `literal | pipe` and \\*escaped\\* markdown.\n",
	}
	seen := map[string]bool{}
	for i, level := range levels {
		bodyPath := directAuthoringBodyPath(t, vault, "body-"+level+".md", bodies[i])
		if err := newAuthoredV7Task(Args{"vault": vault, "quiet": "true", "title": "Authored " + level, "work-level": level, "body-file": bodyPath}); err != nil {
			t.Fatalf("%s: %v", level, err)
		}
	}
	restore := directAuthoringStdin(t, "# Stdin task\n\nstdin body.\n")
	defer restore()
	if err := newAuthoredV7Task(Args{"vault": vault, "quiet": "true", "title": "Stdin task", "work-level": "standard", "body-file": "-"}); err != nil {
		t.Fatal(err)
	}
	restore()
	entries, err := os.ReadDir(filepath.Join(vault, "work", "tasks"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 4 {
		t.Fatalf("expected 4 task records, got %d", len(entries))
	}
	idx := mustIndex(t, vault)
	wantBody := map[string]string{
		"TSK-T-0001": strings.TrimRight(bodies[0], "\n"),
		"TSK-T-0002": strings.TrimRight(bodies[1], "\n"),
		"TSK-T-0003": strings.TrimRight(bodies[2], "\n"),
		"TSK-T-0004": "# Stdin task\n\nstdin body.",
	}
	wantLevel := map[string]string{"TSK-T-0001": "light", "TSK-T-0002": "standard", "TSK-T-0003": "demanding", "TSK-T-0004": "standard"}
	for id, expected := range wantBody {
		if seen[id] {
			t.Fatalf("duplicate task id %s", id)
		}
		seen[id] = true
		data, body := directAuthoringTaskRecord(t, vault, id)
		assertDirectAuthoringCleanRecord(t, data)
		if got := stringField(data, "work_level"); got != wantLevel[id] {
			t.Fatalf("%s work_level=%q, want %q", id, got, wantLevel[id])
		}
		if strings.Trim(body, "\n") != expected {
			t.Fatalf("%s body=%q, want %q", id, body, expected)
		}
		if !v7StateRevMatches(data, body, stringField(data, "state_rev")) {
			t.Fatalf("%s state_rev does not match record content", id)
		}
		packet := v7Packet(vault, Note{Data: data, Body: body}, idx, "agent")
		if !strings.Contains(packet, "Work level: `"+wantLevel[id]+"`") {
			t.Fatalf("%s packet lost work level:\n%s", id, packet)
		}
	}
}

func TestDirectWaveAuthoringNewTaskRequiresBodyFile(t *testing.T) {
	vault := v7DirectTestVault(t)
	if err := newAuthoredV7Task(Args{"vault": vault, "quiet": "true", "title": "No body", "work-level": "standard"}); err == nil || !strings.Contains(err.Error(), "--body-file is required") {
		t.Fatalf("absent body-file error=%v, want MISSING_FIELD refusal", err)
	}
	blankPath := directAuthoringBodyPath(t, vault, "blank.md", "   \n\n\t\n")
	if err := newAuthoredV7Task(Args{"vault": vault, "quiet": "true", "title": "Blank body", "work-level": "standard", "body-file": blankPath}); err == nil || !strings.Contains(err.Error(), "must not be whitespace-only") {
		t.Fatalf("whitespace-only body error=%v, want MISSING_FIELD refusal", err)
	}
	entries, err := os.ReadDir(filepath.Join(vault, "work", "tasks"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("refused creates wrote task records: %v", entries)
	}
}

func TestDirectWaveAuthoringNewTaskAllocatesAndPublishesAtomically(t *testing.T) {
	vault := v7DirectTestVault(t)
	baselineEvents := countV7EventFiles(t, vault, "")
	const writers = 4
	start := make(chan struct{})
	errs := make(chan error, writers)
	for _, name := range []string{"a", "b", "c", "d"} {
		bodyPath := directAuthoringBodyPath(t, vault, "body-"+name+".md", "# "+name+"\n\nConcurrent body.\n")
		go func(bodyPath string) {
			<-start
			errs <- newAuthoredV7Task(Args{"vault": vault, "quiet": "true", "title": "Concurrent task", "work-level": "standard", "body-file": bodyPath})
		}(bodyPath)
	}
	close(start)
	for i := 0; i < writers; i++ {
		if err := <-errs; err != nil {
			t.Fatalf("concurrent authored create %d failed: %v", i, err)
		}
	}
	entries, err := os.ReadDir(filepath.Join(vault, "work", "tasks"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != writers {
		t.Fatalf("concurrent authored creates wrote %d task records, want %d: %v", len(entries), writers, entries)
	}
	for i := 1; i <= writers; i++ {
		id := fmt.Sprintf("TSK-T-%04d", i)
		if got := countV7EventFiles(t, vault, id); got != 1 {
			t.Fatalf("%s has %d creation events, want exactly one", id, got)
		}
	}
	if got := countV7EventFiles(t, vault, ""); got != baselineEvents+writers {
		t.Fatalf("concurrent authored creates wrote %d total events, want %d", got, baselineEvents+writers)
	}
}

func TestDirectWaveAuthoringNewTaskRejectsUnresolvableSpecRefs(t *testing.T) {
	for _, ref := range []string{".tusker/specs/missing.md", ".tusker/specs/delivery.md#missing-section"} {
		t.Run(ref, func(t *testing.T) {
			vault := v7DirectTestVault(t)
			bodyPath := directAuthoringBodyPath(t, vault, "body.md", "# Body\n\nConcrete body.\n")
			err := newAuthoredV7Task(Args{"vault": vault, "quiet": "true", "title": "Bad ref", "work-level": "standard", "body-file": bodyPath, "spec-refs": ref})
			if err == nil || !strings.Contains(err.Error(), "does not resolve") {
				t.Fatalf("error=%v, want spec_ref resolution refusal", err)
			}
			entries, readErr := os.ReadDir(filepath.Join(vault, "work", "tasks"))
			if readErr != nil {
				t.Fatal(readErr)
			}
			if len(entries) != 0 {
				t.Fatalf("refused create wrote task records: %v", entries)
			}
		})
	}
}

func TestDirectWaveAuthoringTaskUpdateCAS(t *testing.T) {
	vault := v7DirectTestVault(t)
	bodyPath := directAuthoringBodyPath(t, vault, "body.md", "# Original\n\nOriginal body.\n")
	if err := newAuthoredV7Task(Args{"vault": vault, "quiet": "true", "title": "CAS target", "work-level": "standard", "body-file": bodyPath, "spec-refs": ".tusker/specs/delivery.md"}); err != nil {
		t.Fatal(err)
	}
	taskPath := filepath.Join(vault, "work", "tasks", "TSK-T-0001.md")
	before, beforeBody, err := parseFrontmatterMustRead(taskPath)
	if err != nil {
		t.Fatal(err)
	}
	rev := stringField(before, "state_rev")
	if rev == "" {
		t.Fatal("created task has no state_rev")
	}
	newBodyPath := directAuthoringBodyPath(t, vault, "body-v2.md", "# Updated\n\nReplacement body.\n")
	err = updateV7TaskCmd(Args{"vault": vault, "quiet": "true", "_pos0": "TSK-T-0001", "if-revision": "sha256:stale", "title": "Should not land", "body-file": newBodyPath, "by": "agent:builder"})
	if err == nil || !strings.Contains(err.Error(), "changed since it was loaded") {
		t.Fatalf("stale revision error=%v, want a CAS refusal", err)
	}
	after, afterBody, err := parseFrontmatterMustRead(taskPath)
	if err != nil {
		t.Fatal(err)
	}
	if stringField(after, "title") != "CAS target" || afterBody != beforeBody || stringField(after, "state_rev") != rev {
		t.Fatal("stale update mutated the record")
	}
	if err := updateV7TaskCmd(Args{"vault": vault, "quiet": "true", "_pos0": "TSK-T-0001", "if-revision": rev, "title": "CAS target v2", "body-file": newBodyPath, "by": "agent:builder"}); err != nil {
		t.Fatal(err)
	}
	updated, updatedBody, err := parseFrontmatterMustRead(taskPath)
	if err != nil {
		t.Fatal(err)
	}
	if stringField(updated, "title") != "CAS target v2" {
		t.Fatalf("title=%q", updated["title"])
	}
	if strings.Trim(updatedBody, "\n") != "# Updated\n\nReplacement body." {
		t.Fatalf("body=%q", updatedBody)
	}
	for _, field := range []string{"id", "created_at", "created_by", "proof_mode", "proof_status", "proof_required", "status", "readiness"} {
		if toString(updated[field]) != toString(before[field]) {
			t.Fatalf("update changed immutable field %s: %v -> %v", field, before[field], updated[field])
		}
	}
	if got := normalizeList(updated["spec_refs"]); len(got) != 1 || got[0] != ".tusker/specs/delivery.md" {
		t.Fatalf("spec_refs changed: %v", updated["spec_refs"])
	}
	if stringField(updated, "state_rev") == rev {
		t.Fatal("state_rev was not recomputed")
	}
}

func TestDirectWaveAuthoringRebindsCrossWaveDependencyContract(t *testing.T) {
	vault, store, project := authorityFixture(t)
	writeDirectTask(t, vault, "APP-T-0001", "W-0001", map[string]any{
		"dependencies": []any{"EXT-T-0001:hard"},
		"dependency_contracts": []any{map[string]any{
			"task_id":                     "EXT-T-0001",
			"kind":                        "hard",
			"target_contract_fingerprint": "sha256:stale",
		}},
	})
	writeDirectTask(t, vault, "EXT-T-0001", "", nil)
	writeDirectWave(t, vault, "W-0001", []string{"APP-T-0001"}, nil)

	review, err := buildDirectWaveReview(vault, store, project.ProjectID, "W-0001", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !hasDirectWaveBlocker(review, "DEPENDENCY_CONTRACT_INVALID") {
		t.Fatalf("stale contract pin did not block: %#v", review.Blockers)
	}

	taskPath := filepath.Join(vault, "work", "tasks", "APP-T-0001.md")
	data, _, err := parseFrontmatterMustRead(taskPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := updateV7TaskCmd(Args{
		"vault": vault, "quiet": "true", "_pos0": "APP-T-0001",
		"if-revision":                 stringField(data, "state_rev"),
		"rebind-dependency-contracts": "true", "by": "agent:builder",
	}); err != nil {
		t.Fatal(err)
	}
	updated, _, err := parseFrontmatterMustRead(taskPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := normalizeList(updated["dependencies"]); len(got) != 1 || got[0] != "EXT-T-0001:hard" {
		t.Fatalf("rebind changed dependency edges: %v", got)
	}
	entries, err := dependencyContractEntries(Note{Data: updated})
	if err != nil || len(entries) != 1 {
		t.Fatalf("rebuilt dependency contracts=%v err=%v", entries, err)
	}
	target, err := resolveV7Note(vault, "EXT-T-0001", "task")
	if err != nil {
		t.Fatal(err)
	}
	if entries[0].TaskID != "EXT-T-0001" || entries[0].Kind != "hard" || entries[0].TargetContractFingerprint != directWaveTaskContract(target) {
		t.Fatalf("rebuilt dependency contract=%#v target=%s", entries[0], directWaveTaskContract(target))
	}

	review, err = buildDirectWaveReview(vault, store, project.ProjectID, "W-0001", nil)
	if err != nil {
		t.Fatal(err)
	}
	if hasDirectWaveBlocker(review, "DEPENDENCY_CONTRACT_INVALID") {
		t.Fatalf("fresh contract pin still blocked: %#v", review.Blockers)
	}

	rewriteTaskFile(t, vault, "EXT-T-0001", func(data map[string]any, body string) (map[string]any, string) {
		return data, body + "\nMaterial changed.\n"
	})
	review, err = buildDirectWaveReview(vault, store, project.ProjectID, "W-0001", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !hasDirectWaveBlocker(review, "DEPENDENCY_CONTRACT_INVALID") {
		t.Fatalf("target material change did not stale the dependency contract: %#v", review.Blockers)
	}
}

func TestDirectWaveAuthoringRebindsOwnContractFingerprint(t *testing.T) {
	vault := v7DirectTestVault(t)
	bodyPath := directAuthoringBodyPath(t, vault, "body.md", "# Contract pin\n\nKeep this body byte-for-byte stable.\n")
	if err := newAuthoredV7Task(Args{"vault": vault, "quiet": "true", "title": "Contract pin", "work-level": "standard", "body-file": bodyPath}); err != nil {
		t.Fatal(err)
	}
	taskPath := filepath.Join(vault, "work", "tasks", "TSK-T-0001.md")
	data, body, err := parseFrontmatterMustRead(taskPath)
	if err != nil {
		t.Fatal(err)
	}
	currentFingerprint := directWaveTaskContractFingerprint(data, body)
	data["contract_fingerprint"] = "sha256:stale-contract-pin"
	data["state_rev"] = v7StateRev(data, body)
	staleContent, err := serializeDocument(data, body, v7FrontmatterOrder["task"])
	if err != nil {
		t.Fatal(err)
	}
	if err := writeText(taskPath, staleContent); err != nil {
		t.Fatal(err)
	}
	before, beforeBody, err := parseFrontmatterMustRead(taskPath)
	if err != nil {
		t.Fatal(err)
	}
	semantic := make(map[string]any)
	for _, field := range []string{"schema", "kind", "id", "project", "title", "status", "readiness", "dependencies", "spec_refs", "proof_mode", "proof_status", "proof_required", "work_level", "review_level", "owned_paths", "generated_outputs"} {
		semantic[field] = before[field]
	}
	baseRevision := stringField(before, "state_rev")
	baselineEvents := countV7EventFiles(t, vault, "TSK-T-0001")
	if err := updateV7TaskCmd(Args{
		"vault": vault, "quiet": "true", "_pos0": "TSK-T-0001", "if-revision": baseRevision,
		"rebind-contract": "true", "by": "agent:builder",
	}); err != nil {
		t.Fatal(err)
	}
	after, afterBody, err := parseFrontmatterMustRead(taskPath)
	if err != nil {
		t.Fatal(err)
	}
	if afterBody != beforeBody {
		t.Fatal("contract rebind changed the task body")
	}
	for field, want := range semantic {
		if !reflect.DeepEqual(after[field], want) {
			t.Fatalf("contract rebind changed %s: %#v -> %#v", field, want, after[field])
		}
	}
	if stringField(after, "contract_fingerprint") != currentFingerprint {
		t.Fatalf("contract fingerprint=%q, want %q", stringField(after, "contract_fingerprint"), currentFingerprint)
	}
	if stringField(after, "state_rev") == baseRevision || !v7StateRevMatches(after, afterBody, stringField(after, "state_rev")) {
		t.Fatalf("contract rebind did not advance a valid state_rev: %q", stringField(after, "state_rev"))
	}
	if got := countV7EventFiles(t, vault, "TSK-T-0001"); got != baselineEvents+1 {
		t.Fatalf("contract rebind wrote %d events, want %d", got, baselineEvents+1)
	}
	var foundChanges map[string]any
	if err := filepath.Walk(filepath.Join(vault, "events"), func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil || info.IsDir() || foundChanges != nil {
			return walkErr
		}
		raw, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		var event map[string]any
		if json.Unmarshal(raw, &event) != nil || event["object"] != "TSK-T-0001" || event["event_kind"] != "updated" {
			return nil
		}
		payload, ok := event["payload"].(map[string]any)
		if !ok || payload["source"] != "task update" {
			return nil
		}
		changes, ok := payload["changes"].(map[string]any)
		if ok {
			foundChanges = changes
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	change, ok := foundChanges["contract_fingerprint"].(map[string]any)
	if !ok || change["from"] != "sha256:stale-contract-pin" || change["to"] != currentFingerprint {
		t.Fatalf("audit changes omitted explicit contract rebind: %#v", foundChanges)
	}
}

func hasDirectWaveBlocker(review directWaveReview, code string) bool {
	for _, blocker := range review.Blockers {
		if blocker.Code == code {
			return true
		}
	}
	return false
}

func TestDirectWaveAuthoringBatchCreatesDurableGraph(t *testing.T) {
	vault := v7DirectTestVault(t)
	request := `schema: tusker.wave-authoring/v1
request_key: batch-v1
title: Authored wave
outcome: A durable wave with resolved dependencies.
spec_refs:
  - .tusker/specs/delivery.md
shared_context: Shared context lives once in the wave brief.
tasks:
  - key: root
    title: Root task
    work_level: standard
    body: "# Root\n\nRoot body.\n"
  - key: left
    title: Left task
    work_level: light
    body: "# Left\n\nLeft body.\n"
    dependencies:
      - task: root
        kind: hard
  - key: right
    title: Right task
    work_level: demanding
    body: "# Right\n\nRuns cat spec.md | grep boundary verbatim.\n"
    dependencies:
      - task: root
human_actions:
  - key: creds
    task: right
    owner: human:operator
    action: Provision staging credentials.
    verification: The provider reports ready.
    why_agent_cannot: Human account access is required.
`
	requestPath := directAuthoringBodyPath(t, vault, "wave.yaml", request)
	trailingPath := directAuthoringBodyPath(t, vault, "wave-trailing.yaml", request+"\n---\nsecond: document\n")
	if err := waveV7CreateCmd(Args{"vault": vault, "quiet": "true", "file": trailingPath, "request-key": "trailing-v1"}); err == nil || !strings.Contains(err.Error(), "exactly one document") {
		t.Fatalf("trailing document error=%v, want strict decode refusal", err)
	}
	if _, err := os.Stat(filepath.Join(vault, "work", "waves", "W-0001.md")); !os.IsNotExist(err) {
		t.Fatalf("strict decode refusal wrote a wave: %v", err)
	}
	if err := waveV7CreateCmd(Args{"vault": vault, "quiet": "true", "file": requestPath, "request-key": "batch-v1"}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"TSK-T-0001", "TSK-T-0002", "TSK-T-0003"} {
		data, body := directAuthoringTaskRecord(t, vault, id)
		assertDirectAuthoringCleanRecord(t, data)
		if stringField(data, "status") != "backlog" || stringField(data, "readiness") != "held" {
			t.Fatalf("%s is not inert: status=%v readiness=%v", id, data["status"], data["readiness"])
		}
		if stringField(data, "wave") != "W-0001" {
			t.Fatalf("%s wave=%v", id, data["wave"])
		}
		for _, field := range []string{"key", "source_key", "task_key", "request_key", "authoring_request_key", "authoring_request_fingerprint", "request", "dependencies_label"} {
			if _, ok := data[field]; ok {
				t.Fatalf("%s stored temporary request field %s: %#v", id, field, data)
			}
		}
		for _, field := range []string{"id", "title", "epic", "wave"} {
			if value, ok := data[field].(string); ok {
				for _, label := range []string{"root", "left", "right", "creds"} {
					if value == label {
						t.Fatalf("%s frontmatter %s stores temporary label %q", id, field, label)
					}
				}
			}
		}
		if strings.Contains(body, "batch-v1") || strings.Contains(body, "wave.yaml") {
			t.Fatalf("%s body leaks request key or path:\n%s", id, body)
		}
	}
	left, _ := directAuthoringTaskRecord(t, vault, "TSK-T-0002")
	if got := normalizeList(left["dependencies"]); len(got) != 1 || got[0] != "TSK-T-0001:hard" {
		t.Fatalf("left dependencies=%v", got)
	}
	right, rightBody := directAuthoringTaskRecord(t, vault, "TSK-T-0003")
	if got := normalizeList(right["dependencies"]); len(got) != 1 || got[0] != "TSK-T-0001:hard" {
		t.Fatalf("right dependencies=%v", got)
	}
	for _, dep := range append(normalizeList(left["dependencies"]), normalizeList(right["dependencies"])...) {
		if !regexp.MustCompile(`^[A-Z]+-T-[0-9]+:(hard|soft)$`).MatchString(dep) {
			t.Fatalf("dependency is not a canonical durable edge: %q", dep)
		}
	}
	if !strings.Contains(rightBody, "cat spec.md | grep boundary") {
		t.Fatalf("literal pipe command was rewritten:\n%s", rightBody)
	}
	gates := normalizeList(right["gates"])
	if len(gates) != 1 || gates[0] != "TSK-G-0001" {
		t.Fatalf("right gates=%v", gates)
	}
	gate, gateBody, err := parseFrontmatterMustRead(filepath.Join(vault, "work", "gates", "TSK-G-0001.md"))
	if err != nil {
		t.Fatal(err)
	}
	if stringField(gate, "owner") != "human:operator" || stringField(gate, "why_agent_cannot") == "" {
		t.Fatalf("gate lost human action fields: %#v", gate)
	}
	_ = gateBody
	wave, waveBody, err := parseFrontmatterMustRead(filepath.Join(vault, "work", "waves", "W-0001.md"))
	if err != nil {
		t.Fatal(err)
	}
	if stringField(wave, "status") != "open" || stringField(wave, "authorization") != "disarmed" {
		t.Fatalf("wave is not inert: %#v", wave)
	}
	if stringField(wave, "authoring_request_key") != "batch-v1" || stringField(wave, "authoring_request_fingerprint") == "" {
		t.Fatalf("wave lost idempotency receipt: %#v", wave)
	}
	for _, forbidden := range []string{"delivery_plan_scope", "delivery_plan_fingerprint", "context_fingerprint", "factory_intake_contract_schema", "request", "key", "task_key", "source_key", "root", "left", "right", "creds"} {
		if _, ok := wave[forbidden]; ok {
			t.Fatalf("wave stored request label or plan field %s: %#v", forbidden, wave)
		}
	}
	if strings.Contains(waveBody, "batch-v1") || strings.Contains(waveBody, "wave.yaml") {
		t.Fatalf("wave body stored request bytes or path:\n%s", waveBody)
	}
	if !strings.Contains(waveBody, "Shared context lives once in the wave brief.") {
		t.Fatalf("wave body lost shared context:\n%s", waveBody)
	}
}

func TestDirectWaveAuthoringRollbackIdempotencyAndConflict(t *testing.T) {
	vault := v7DirectTestVault(t)
	request := `schema: tusker.wave-authoring/v1
title: Atomic wave
outcome: A wave that publishes atomically.
tasks:
  - key: only
    title: Only task
    work_level: standard
    body: "# Only\n\nOnly body.\n"
`
	requestPath := directAuthoringBodyPath(t, vault, "wave.yaml", request)
	err := waveV7CreateCmd(Args{"vault": vault, "quiet": "true", "file": requestPath, "request-key": "atomic-v1", "fail-after-first-write": "true"})
	if err == nil {
		t.Fatal("expected injected write failure")
	}
	for _, dir := range []string{"tasks", "waves", "gates"} {
		entries, readErr := os.ReadDir(filepath.Join(vault, "work", dir))
		if readErr != nil {
			t.Fatal(readErr)
		}
		if len(entries) != 0 {
			t.Fatalf("rollback left %s records: %v", dir, entries)
		}
	}
	if err := waveV7CreateCmd(Args{"vault": vault, "quiet": "true", "file": requestPath, "request-key": "atomic-v1"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := parseFrontmatterMustRead(filepath.Join(vault, "work", "tasks", "TSK-T-0001.md")); err != nil {
		t.Fatalf("retry did not allocate the same task id: %v", err)
	}
	if _, _, err := parseFrontmatterMustRead(filepath.Join(vault, "work", "waves", "W-0001.md")); err != nil {
		t.Fatalf("retry did not allocate the same wave id: %v", err)
	}
	if err := waveV7CreateCmd(Args{"vault": vault, "quiet": "true", "file": requestPath, "request-key": "atomic-v1"}); err != nil {
		t.Fatal(err)
	}
	entries, readErr := os.ReadDir(filepath.Join(vault, "work", "tasks"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	if len(entries) != 1 {
		t.Fatalf("identical retry duplicated task records: %v", entries)
	}
	changed := strings.Replace(request, "Only body.", "Changed body.", 1)
	changedPath := directAuthoringBodyPath(t, vault, "wave-changed.yaml", changed)
	err = waveV7CreateCmd(Args{"vault": vault, "quiet": "true", "file": changedPath, "request-key": "atomic-v1"})
	if err == nil || !strings.Contains(err.Error(), "already used") {
		t.Fatalf("changed request under same key error=%v, want conflict", err)
	}
}

func TestDirectWaveAuthoringOwnedPathFrontierConflict(t *testing.T) {
	vault := v7DirectTestVault(t)
	request := func(deps string) string {
		return `schema: tusker.wave-authoring/v1
title: Overlap wave
outcome: Ownership overlap is checked per frontier.
tasks:
  - key: first
    title: First
    work_level: standard
    body: "# First\n\nFirst body.\n"
    owned_paths:
      - cmd/tusker/shared.go
  - key: second
    title: Second
    work_level: standard
    body: "# Second\n\nSecond body.\n"
    owned_paths:
      - cmd/tusker/shared.go
` + deps
	}
	conflictPath := directAuthoringBodyPath(t, vault, "overlap.yaml", request(""))
	err := waveV7CreateCmd(Args{"vault": vault, "quiet": "true", "file": conflictPath, "request-key": "overlap-v1"})
	if err == nil || !strings.Contains(err.Error(), "OWNED_PATH_FRONTIER_CONFLICT") {
		t.Fatalf("same-frontier overlap error=%v, want OWNED_PATH_FRONTIER_CONFLICT", err)
	}
	serializedPath := directAuthoringBodyPath(t, vault, "serialized.yaml", request(`    dependencies:
      - task: first
        kind: hard
`))
	if err := waveV7CreateCmd(Args{"vault": vault, "quiet": "true", "file": serializedPath, "request-key": "serialized-v1"}); err != nil {
		t.Fatalf("serialized overlap must be accepted: %v", err)
	}
	second, _ := directAuthoringTaskRecord(t, vault, "TSK-T-0002")
	if got := normalizeList(second["dependencies"]); len(got) != 1 || got[0] != "TSK-T-0001:hard" {
		t.Fatalf("serialized dependencies=%v", got)
	}
}

func TestDirectWaveAuthoringRejectsMissingEpic(t *testing.T) {
	vault := v7DirectTestVault(t)
	request := `schema: tusker.wave-authoring/v1
title: Epic wave
outcome: Explicit epics must already exist.
tasks:
  - key: only
    title: Only task
    work_level: standard
    epic: NEX
    body: "# Only\n\nOnly body.\n"
`
	requestPath := directAuthoringBodyPath(t, vault, "wave.yaml", request)
	err := waveV7CreateCmd(Args{"vault": vault, "quiet": "true", "file": requestPath, "request-key": "epic-v1"})
	if err == nil || !strings.Contains(err.Error(), "AUTHORING_REQUEST_INVALID") || !strings.Contains(err.Error(), "epic does not exist") {
		t.Fatalf("missing epic error=%v, want AUTHORING_REQUEST_INVALID refusal", err)
	}
	entries, readErr := os.ReadDir(filepath.Join(vault, "work", "tasks"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("missing-epic refusal wrote task records: %v", entries)
	}
}

func countV7EventFiles(t *testing.T, vault, objectID string) int {
	t.Helper()
	count := 0
	root := filepath.Join(vault, "events")
	if _, err := os.Stat(root); os.IsNotExist(err) {
		return 0
	}
	if err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if objectID == "" || strings.HasPrefix(info.Name(), objectID+"--") {
			count++
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return count
}

func rewriteDirectWaveRecord(t *testing.T, vault, id string, mutate func(data map[string]any)) {
	t.Helper()
	path := filepath.Join(vault, "work", "waves", id+".md")
	data, body, err := parseFrontmatterMustRead(path)
	if err != nil {
		t.Fatal(err)
	}
	mutate(data)
	data["state_rev"] = v7StateRev(data, body)
	content, err := serializeDocument(data, body, v7FrontmatterOrder["wave"])
	if err != nil {
		t.Fatal(err)
	}
	if err := writeText(path, content); err != nil {
		t.Fatal(err)
	}
}

func TestDirectWaveAuthoringUpdateReworksCompletedTask(t *testing.T) {
	vault, store, project := authorityFixture(t)
	writeDirectTask(t, vault, "APP-T-0001", "W-0001", nil)
	writeDirectWave(t, vault, "W-0001", []string{"APP-T-0001"}, nil)
	rewriteTaskFile(t, vault, "APP-T-0001", func(data map[string]any, body string) (map[string]any, string) {
		data["status"] = "done"
		data["readiness"] = "done"
		data["proof_status"] = "satisfied"
		data["accepted_by"] = "human:sarav"
		data["accepted_at"] = "2026-02-01T00:00:00Z"
		data["closed_at"] = "2026-02-01T00:00:00Z"
		data["close_authority"] = map[string]any{"kind": "human_acceptor"}
		data["closeout_status"] = "closed"
		data["machine_status"] = "complete"
		data["human_status"] = "done"
		data["agent_action"] = "none"
		data["next_owner"] = "none"
		data["next_source"] = "status"
		data["next_ref"] = ""
		data["next_action"] = ""
		return data, body
	})
	review, err := buildDirectWaveReview(vault, store, project.ProjectID, "W-0001", nil)
	if err != nil {
		t.Fatal(err)
	}
	if review.State != "Completed" || review.Members[0].State != "completed" {
		t.Fatalf("precondition review=%#v", review.State)
	}
	before, _, err := parseFrontmatterMustRead(filepath.Join(vault, "work", "tasks", "APP-T-0001.md"))
	if err != nil {
		t.Fatal(err)
	}
	rev := stringField(before, "state_rev")
	reworkBody := directAuthoringBodyPath(t, vault, "rework-body.md", "# APP-T-0001\n\n## Intent\n\nDo the reworked contract.\n")
	if err := updateV7TaskCmd(Args{"vault": vault, "quiet": "true", "_pos0": "APP-T-0001", "if-revision": rev, "body-file": reworkBody, "by": "agent:builder"}); err != nil {
		t.Fatal(err)
	}
	data, _, err := parseFrontmatterMustRead(filepath.Join(vault, "work", "tasks", "APP-T-0001.md"))
	if err != nil {
		t.Fatal(err)
	}
	if stringField(data, "status") != "rework" || stringField(data, "readiness") != "ready" || stringField(data, "proof_status") != "pending" {
		t.Fatalf("rework transition=%v/%v/%v", data["status"], data["readiness"], data["proof_status"])
	}
	for _, field := range []string{"accepted_by", "accepted_at", "closed_at", "close_authority", "closeout_status", "machine_status", "human_status", "agent_action"} {
		if _, ok := data[field]; ok {
			t.Fatalf("completion field %s survived rework: %#v", field, data[field])
		}
	}
	review, err = buildDirectWaveReview(vault, store, project.ProjectID, "W-0001", nil)
	if err != nil {
		t.Fatal(err)
	}
	if review.State == "Completed" || review.Members[0].State == "completed" {
		t.Fatalf("reworked member still projects completed: %#v", review.Members[0])
	}
	result, err := directTaskBackgroundStart(vault, store, "APP-T-0001", "human:sarav")
	if err != nil {
		t.Fatalf("reworked task still terminal-refused: %v", err)
	}
	if result.Authorization != "authorized" {
		t.Fatalf("reworked task not startable: %#v", result)
	}
	found := false
	root := filepath.Join(vault, "events")
	filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasPrefix(info.Name(), "APP-T-0001--") {
			return nil
		}
		raw, readErr := os.ReadFile(path)
		if readErr == nil && strings.Contains(string(raw), `"lifecycle_transition": "rework"`) {
			found = true
		}
		return nil
	})
	if !found {
		t.Fatal("update event did not record the rework lifecycle transition")
	}
}

func TestDirectWaveAuthoringUpdateEventFailureRollsBack(t *testing.T) {
	vault := v7DirectTestVault(t)
	bodyPath := directAuthoringBodyPath(t, vault, "body.md", "# T\n\nOriginal body.\n")
	if err := newAuthoredV7Task(Args{"vault": vault, "quiet": "true", "title": "Task", "work-level": "standard", "body-file": bodyPath}); err != nil {
		t.Fatal(err)
	}
	taskPath := filepath.Join(vault, "work", "tasks", "TSK-T-0001.md")
	beforeBytes, err := os.ReadFile(taskPath)
	if err != nil {
		t.Fatal(err)
	}
	data, _, err := parseFrontmatterMustRead(taskPath)
	if err != nil {
		t.Fatal(err)
	}
	rev := stringField(data, "state_rev")
	baselineEvents := countV7EventFiles(t, vault, "TSK-T-0001")
	newBody := directAuthoringBodyPath(t, vault, "body2.md", "# T\n\nAmended body.\n")
	directWaveTaskUpdateInjectCommitFailAfter = 1
	err = updateV7TaskCmd(Args{"vault": vault, "quiet": "true", "_pos0": "TSK-T-0001", "if-revision": rev, "body-file": newBody, "by": "agent:builder"})
	directWaveTaskUpdateInjectCommitFailAfter = 0
	if err == nil {
		t.Fatal("expected injected update commit failure")
	}
	afterBytes, err := os.ReadFile(taskPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(beforeBytes) != string(afterBytes) {
		t.Fatal("failed update commit left mutated task record")
	}
	if got := countV7EventFiles(t, vault, "TSK-T-0001"); got != baselineEvents {
		t.Fatalf("failed update commit left %d events, want baseline %d", got, baselineEvents)
	}
	if err := updateV7TaskCmd(Args{"vault": vault, "quiet": "true", "_pos0": "TSK-T-0001", "if-revision": rev, "body-file": newBody, "by": "agent:builder"}); err != nil {
		t.Fatal(err)
	}
	if got := countV7EventFiles(t, vault, "TSK-T-0001"); got != baselineEvents+1 {
		t.Fatalf("retry produced %d task events, want %d", got, baselineEvents+1)
	}
	data, _, err = parseFrontmatterMustRead(taskPath)
	if err != nil {
		t.Fatal(err)
	}
	if stringField(data, "state_rev") == rev {
		t.Fatal("retry did not advance the task state_rev")
	}
}

func TestDirectWaveAuthoringConcurrentDependencyEditsStayAcyclic(t *testing.T) {
	vault := v7DirectTestVault(t)
	for _, name := range []string{"a", "b"} {
		bodyPath := directAuthoringBodyPath(t, vault, "body-"+name+".md", "# "+name+"\n\nBody "+name+".\n")
		if err := newAuthoredV7Task(Args{"vault": vault, "quiet": "true", "title": "Task " + name, "work-level": "standard", "body-file": bodyPath}); err != nil {
			t.Fatal(err)
		}
	}
	revOf := func(id string) string {
		data, _, err := parseFrontmatterMustRead(filepath.Join(vault, "work", "tasks", id+".md"))
		if err != nil {
			t.Fatal(err)
		}
		return stringField(data, "state_rev")
	}
	revA, revB := revOf("TSK-T-0001"), revOf("TSK-T-0002")
	errs := make(chan error, 2)
	go func() {
		errs <- updateV7TaskCmd(Args{"vault": vault, "quiet": "true", "_pos0": "TSK-T-0001", "if-revision": revA, "dependencies": "TSK-T-0002", "by": "agent:builder"})
	}()
	go func() {
		errs <- updateV7TaskCmd(Args{"vault": vault, "quiet": "true", "_pos0": "TSK-T-0002", "if-revision": revB, "dependencies": "TSK-T-0001", "by": "agent:builder"})
	}()
	errA, errB := <-errs, <-errs
	if (errA == nil) == (errB == nil) {
		t.Fatalf("exactly one dependency edit must win: errA=%v errB=%v", errA, errB)
	}
	depsOf := func(id string) []string {
		data, _, err := parseFrontmatterMustRead(filepath.Join(vault, "work", "tasks", id+".md"))
		if err != nil {
			t.Fatal(err)
		}
		return normalizeList(data["dependencies"])
	}
	aDeps, bDeps := depsOf("TSK-T-0001"), depsOf("TSK-T-0002")
	aOnB := len(aDeps) == 1 && aDeps[0] == "TSK-T-0002:hard"
	bOnA := len(bDeps) == 1 && bDeps[0] == "TSK-T-0001:hard"
	if aOnB == bOnA {
		t.Fatalf("final graph is cyclic or lost the winning edge: A=%v B=%v", aDeps, bDeps)
	}
}

func TestDirectWaveAuthoringReceiptReplayIgnoresMemberOrder(t *testing.T) {
	vault := v7DirectTestVault(t)
	request := `schema: tusker.wave-authoring/v1
title: Receipt wave
outcome: Replay must return the original durable mapping.
tasks:
  - key: first
    title: First task
    work_level: standard
    body: "# First\n\nFirst body.\n"
  - key: second
    title: Second task
    work_level: light
    body: "# Second\n\nSecond body.\n"
    dependencies:
      - task: first
human_actions:
  - key: ga
    task: second
    owner: human:operator
    action: Approve the first gate.
    verification: Human verified.
    why_agent_cannot: Human authority.
  - key: gb
    task: second
    owner: human:operator
    action: Approve the second gate.
    verification: Human verified.
    why_agent_cannot: Human authority.
`
	requestPath := directAuthoringBodyPath(t, vault, "wave.yaml", request)
	if err := waveV7CreateCmd(Args{"vault": vault, "quiet": "true", "file": requestPath, "request-key": "receipt-v1"}); err != nil {
		t.Fatal(err)
	}
	rewriteDirectWaveRecord(t, vault, "W-0001", func(data map[string]any) {
		data["members"] = []any{"TSK-T-0002", "TSK-T-0001"}
	})
	rewriteTaskFile(t, vault, "TSK-T-0002", func(data map[string]any, body string) (map[string]any, string) {
		data["gates"] = []any{"TSK-G-0002", "TSK-G-0001"}
		return data, body
	})
	if err := waveV7CreateCmd(Args{"vault": vault, "quiet": "true", "file": requestPath, "request-key": "receipt-v1", "json": "true"}); err != nil {
		t.Fatalf("replay after member/gate reorder failed: %v", err)
	}
	entries, err := os.ReadDir(filepath.Join(vault, "work", "tasks"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("replay duplicated task records: %v", entries)
	}
	rewriteDirectWaveRecord(t, vault, "W-0001", func(data map[string]any) {
		data["members"] = []any{"TSK-T-0001"}
	})
	err = waveV7CreateCmd(Args{"vault": vault, "quiet": "true", "file": requestPath, "request-key": "receipt-v1"})
	if err == nil || !strings.Contains(err.Error(), "AUTHORING_RECEIPT_DRIFT") {
		t.Fatalf("removed member replay error=%v, want AUTHORING_RECEIPT_DRIFT", err)
	}
	rewriteDirectWaveRecord(t, vault, "W-0001", func(data map[string]any) {
		data["members"] = []any{"TSK-T-0002", "TSK-T-0001"}
		delete(data, "authoring_receipt")
	})
	err = waveV7CreateCmd(Args{"vault": vault, "quiet": "true", "file": requestPath, "request-key": "receipt-v1"})
	if err == nil || !strings.Contains(err.Error(), "AUTHORING_RECEIPT_DRIFT") {
		t.Fatalf("missing receipt replay error=%v, want AUTHORING_RECEIPT_DRIFT", err)
	}
}

func TestDirectWaveAuthoringEventsCommitAtomically(t *testing.T) {
	vault := v7DirectTestVault(t)
	request := `schema: tusker.wave-authoring/v1
title: Atomic events
outcome: Events publish with the records they describe.
tasks:
  - key: only
    title: Only task
    work_level: standard
    body: "# Only\n\nOnly body.\n"
`
	requestPath := directAuthoringBodyPath(t, vault, "wave.yaml", request)
	baselineEvents := countV7EventFiles(t, vault, "")
	if err := waveV7CreateCmd(Args{"vault": vault, "quiet": "true", "file": requestPath, "request-key": "atomic-events-v1", "fail-after-first-write": "true"}); err == nil {
		t.Fatal("expected injected write failure")
	}
	for _, dir := range []string{"tasks", "waves", "gates"} {
		entries, readErr := os.ReadDir(filepath.Join(vault, "work", dir))
		if readErr != nil {
			t.Fatal(readErr)
		}
		if len(entries) != 0 {
			t.Fatalf("rollback left %s records: %v", dir, entries)
		}
	}
	if got := countV7EventFiles(t, vault, ""); got != baselineEvents {
		t.Fatalf("rollback left %d event files, want baseline %d", got, baselineEvents)
	}
	if err := waveV7CreateCmd(Args{"vault": vault, "quiet": "true", "file": requestPath, "request-key": "atomic-events-v1"}); err != nil {
		t.Fatal(err)
	}
	if got := countV7EventFiles(t, vault, "TSK-T-0001"); got != 1 {
		t.Fatalf("task created events=%d, want 1", got)
	}
	if got := countV7EventFiles(t, vault, "W-0001"); got != 1 {
		t.Fatalf("wave created events=%d, want 1", got)
	}
	if err := waveV7CreateCmd(Args{"vault": vault, "quiet": "true", "file": requestPath, "request-key": "atomic-events-v1"}); err != nil {
		t.Fatal(err)
	}
	if got := countV7EventFiles(t, vault, ""); got != baselineEvents+2 {
		t.Fatalf("replay duplicated event history: %d", got)
	}
}

func TestDirectWaveAuthoringUpdatePreservesReviewAndCompletionPolicy(t *testing.T) {
	vault := v7DirectTestVault(t)
	bodyPath := directAuthoringBodyPath(t, vault, "reviewed-body.md", "# Reviewed\n\nSubstantive reviewed body.\n")
	if err := newAuthoredV7Task(Args{"vault": vault, "quiet": "true", "title": "Reviewed", "work-level": "light", "review-level": "demanding", "review-reason": "Security-sensitive diff needs the deepest review.", "body-file": bodyPath, "proof-mode": "card", "proof-required": "human_signoff"}); err != nil {
		t.Fatal(err)
	}
	taskPath := filepath.Join(vault, "work", "tasks", "TSK-T-0001.md")
	before, _, err := parseFrontmatterMustRead(taskPath)
	if err != nil {
		t.Fatal(err)
	}
	rev := stringField(before, "state_rev")
	if err := updateV7TaskCmd(Args{"vault": vault, "quiet": "true", "_pos0": "TSK-T-0001", "if-revision": rev, "title": "Reviewed v2", "by": "agent:builder"}); err != nil {
		t.Fatal(err)
	}
	after, _, err := parseFrontmatterMustRead(taskPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"review_level", "review_reason", "proof_mode", "proof_required", "proof_required_owner", "proof_status"} {
		if toString(after[field]) != toString(before[field]) {
			t.Fatalf("update weakened %s: %v -> %v", field, before[field], after[field])
		}
	}
	err = updateV7TaskCmd(Args{"vault": vault, "quiet": "true", "_pos0": "TSK-T-0001", "if-revision": stringField(after, "state_rev"), "review-level": "light", "by": "agent:builder"})
	if err == nil || !strings.Contains(err.Error(), "--review-level override requires --review-reason") {
		t.Fatalf("unreasoned review downgrade error=%v, want the explicit policy refusal", err)
	}
}
