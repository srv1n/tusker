package main

import (
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func writeDirectTaskBody(t *testing.T, vault, id, waveID string, extra map[string]any, body string) string {
	t.Helper()
	data := map[string]any{
		"schema": "tusker.task/v7", "kind": "task", "id": id, "project": v7ProjectID(vault),
		"title": "Direct " + id, "status": "backlog", "readiness": "held",
		"proof_mode": "inline", "proof_status": "pending", "proof_required": []any{"focused_test"},
		"work_level": "standard", "next_owner": "agent",
		"created_at": "2026-01-01T00:00:00Z", "created_by": "agent:test", "updated_at": "2026-01-01T00:00:00Z", "updated_by": "agent:test",
	}
	for k, v := range extra {
		data[k] = v
	}
	if waveID != "" {
		data["wave"] = waveID
	}
	data["contract_fingerprint"] = directWaveTaskContractFingerprint(data, body)
	data["state_rev"] = v7StateRev(data, body)
	content, err := serializeDocument(data, body, v7FrontmatterOrder["task"])
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(vault, "work", "tasks", id+".md")
	if err := writeText(path, content); err != nil {
		t.Fatal(err)
	}
	return path
}

func rewriteWaveBody(t *testing.T, vault, id string, mutate func(data map[string]any, body string) (map[string]any, string)) {
	t.Helper()
	path := filepath.Join(vault, "work", "waves", id+".md")
	data, body, err := parseFrontmatterMustRead(path)
	if err != nil {
		t.Fatal(err)
	}
	data, body = mutate(data, body)
	data["state_rev"] = v7StateRev(data, body)
	content, err := serializeDocument(data, body, v7FrontmatterOrder["wave"])
	if err != nil {
		t.Fatal(err)
	}
	if err := writeText(path, content); err != nil {
		t.Fatal(err)
	}
}

func packetTaskByID(t *testing.T, idx v7Index, id string) Note {
	t.Helper()
	task, ok := idx.Tasks[id]
	if !ok {
		t.Fatalf("missing task %s", id)
	}
	return task
}

func TestDirectWavePacketStandaloneLevelsRetainContract(t *testing.T) {
	vault, _, _ := authorityFixture(t)
	levels := []struct {
		id       string
		work     string
		review   string
		reason   string
		wantLine string
	}{
		{id: "APP-T-0001", work: "light", wantLine: "- Work level: `light`\n- Review level: `light` (inherited)"},
		{id: "APP-T-0002", work: "standard", wantLine: "- Work level: `standard`\n- Review level: `standard` (inherited)"},
		{id: "APP-T-0003", work: "demanding", wantLine: "- Work level: `demanding`\n- Review level: `demanding` (inherited)"},
		{id: "APP-T-0004", work: "light", review: "demanding", reason: "Touches the billing ledger invariant.", wantLine: "- Work level: `light`\n- Review level: `demanding` (explicit override)"},
	}
	for _, tc := range levels {
		extra := map[string]any{"work_level": tc.work}
		if tc.review != "" {
			extra["review_level"] = tc.review
			extra["review_reason"] = tc.reason
		}
		body := "# " + tc.id + "\n\n## Intent\n\nChange the retry backoff in `runner/backoff.go`.\n\n## Acceptance\n\n| ID | Outcome |\n| --- | --- |\n| A1 | Backoff doubles on retry. |\n\n## Verification\n\n| Covers | Check | Result | Notes |\n| --- | --- | --- | --- |\n| A1 | command: go test ./runner -run Backoff | pending | |\n"
		writeDirectTaskBody(t, vault, tc.id, "", extra, body)
	}
	idx := mustIndex(t, vault)
	for _, tc := range levels {
		task := packetTaskByID(t, idx, tc.id)
		if got := stringField(task.Data, "work_level"); got != tc.work {
			t.Fatalf("%s work_level=%q want %q", tc.id, got, tc.work)
		}
		for _, banned := range []string{"spec_refs", "epic", "priority", "size", "risk"} {
			if _, present := task.Data[banned]; present {
				t.Fatalf("%s carries omitted admin/relationship field %q", tc.id, banned)
			}
		}
		for _, audience := range []string{"agent", "reviewer"} {
			packet := v7Packet(vault, task, idx, audience)
			if !strings.Contains(packet, tc.wantLine) {
				t.Fatalf("%s %s packet lost work/review level:\n%s", tc.id, audience, packet)
			}
			if tc.reason != "" && !strings.Contains(packet, tc.reason) {
				t.Fatalf("%s %s packet lost review reason", tc.id, audience)
			}
			if !strings.Contains(packet, "command: go test ./runner -run Backoff") || !strings.Contains(packet, "Change the retry backoff in `runner/backoff.go`.") {
				t.Fatalf("%s %s packet lost exact body/check strings", tc.id, audience)
			}
			if strings.Contains(packet, "## Wave context") {
				t.Fatalf("%s %s packet gained synthetic wave context", tc.id, audience)
			}
		}
		capsule := renderCapsuleWithVault(task, vault)
		if !strings.Contains(capsule, "Status: backlog") {
			t.Fatalf("%s capsule lost durable status:\n%s", tc.id, capsule)
		}
	}
}

func TestDirectWavePacketWaveContextAndScopedBodies(t *testing.T) {
	vault, _, _ := authorityFixture(t)
	writeDirectWave(t, vault, "W-0001", []string{"APP-T-0001", "APP-T-0002"}, map[string]any{"summary": "Ship the scheduler split."})
	shared := "Shared context: the scheduler interface is `cmd/sched/frontier.go:advanceAuthorizedWaveFrontiers`; keep `run_directives` as the only queue seam."
	rewriteWaveBody(t, vault, "W-0001", func(data map[string]any, body string) (map[string]any, string) {
		data["authoring_receipt"] = map[string]any{"request_key": "wave-req-7", "task_map": map[string]any{"sched-root": "APP-T-0001"}}
		return data, body + "\n## Shared context\n\n" + shared + "\n"
	})
	body1 := "# APP-T-0001\n\n## Intent\n\nSplit the frontier queue into `queueAuthorizedWaveFrontier`.\n\n## Decisions\n\nLocked: directives stay in `run_directives`. Proposed name `scheduleWaveFrontier` is not approved.\n\n## Verification\n\n| Covers | Check | Result | Notes |\n| --- | --- | --- | --- |\n| A1 | command: go test ./x \\| tee proof.log | pending | |\n"
	body2 := "# APP-T-0002\n\n## Intent\n\nWire `advanceAuthorizedWaveFrontiers` into the poll loop.\n\n## Verification\n\n| Covers | Check | Result | Notes |\n| --- | --- | --- | --- |\n| A1 | command: go test ./y | pending | |\n"
	writeDirectTaskBody(t, vault, "APP-T-0001", "W-0001", nil, body1)
	writeDirectTaskBody(t, vault, "APP-T-0002", "W-0001", nil, body2)
	idx := mustIndex(t, vault)
	for _, id := range []string{"APP-T-0001", "APP-T-0002"} {
		task := packetTaskByID(t, idx, id)
		for _, audience := range []string{"agent", "reviewer"} {
			packet := v7Packet(vault, task, idx, audience)
			if !strings.Contains(packet, "## Wave context") || !strings.Contains(packet, "- Wave: W-0001") || !strings.Contains(packet, "- Outcome: Ship the scheduler split.") {
				t.Fatalf("%s %s packet lost wave context:\n%s", id, audience, packet)
			}
			if !strings.Contains(packet, shared) {
				t.Fatalf("%s %s packet lost exact shared context", id, audience)
			}
			for _, leaked := range []string{"authoring_receipt", "wave-req-7", "sched-root"} {
				if strings.Contains(packet, leaked) {
					t.Fatalf("%s %s packet leaked receipt/admin field %q", id, audience, leaked)
				}
			}
		}
	}
	p1 := v7Packet(vault, packetTaskByID(t, idx, "APP-T-0001"), idx, "agent")
	p2 := v7Packet(vault, packetTaskByID(t, idx, "APP-T-0002"), idx, "agent")
	if !strings.Contains(p1, "command: go test ./x \\| tee proof.log") {
		t.Fatalf("literal check string with pipe escape was rewritten:\n%s", p1)
	}
	if !strings.Contains(p1, "Proposed name `scheduleWaveFrontier` is not approved.") {
		t.Fatal("locked-vs-proposed decision clause was lost")
	}
	if strings.Contains(p1, "Wire `advanceAuthorizedWaveFrontiers` into the poll loop.") {
		t.Fatal("sibling task body leaked into the selected task packet")
	}
	if !strings.Contains(p2, "Wire `advanceAuthorizedWaveFrontiers` into the poll loop.") || strings.Contains(p2, "scheduleWaveFrontier") {
		t.Fatal("selected task packet carried the wrong task body")
	}
}

func TestDirectWavePacketExecutionEntryCommands(t *testing.T) {
	vault, _, _ := authorityFixture(t)
	writeDirectWave(t, vault, "W-0001", []string{"APP-T-0001"}, nil)
	writeDirectTaskBody(t, vault, "APP-T-0001", "W-0001", nil, directTaskBody("APP-T-0001", "Do scoped work."))
	writeDirectTaskBody(t, vault, "APP-T-0002", "", nil, directTaskBody("APP-T-0002", "Do standalone work."))
	idx := mustIndex(t, vault)
	waveTask := packetTaskByID(t, idx, "APP-T-0001")
	packet := v7Packet(vault, waveTask, idx, "agent")
	for _, want := range []string{
		"## Execution entry",
		"Task and wave creation are inert",
		"tusker task start APP-T-0001 --mode interactive --by <agent> --current-workspace --json",
		"tusker task start APP-T-0001 --mode background --by <actor> --json",
		"tusker wave start W-0001 --mode background --by human:<name>|operator:<name> --json",
		"advances each dependency frontier automatically",
		"tusker wave pause W-0001 --by human:<name>|operator:<name>",
		"tusker wave resume W-0001 --by human:<name>|operator:<name>",
		"task Start inside a paused wave stays task-scoped and leaves the wave paused",
	} {
		if !strings.Contains(packet, want) {
			t.Fatalf("agent packet missing execution entry content %q:\n%s", want, packet)
		}
	}
	for _, banned := range []*regexp.Regexp{
		regexp.MustCompile(`(?i)mark\s*ready`),
		regexp.MustCompile(`(?i)preflight`),
		regexp.MustCompile(`(?i)wave\s+arm`),
		regexp.MustCompile(`(?i)\bplay\b`),
	} {
		if banned.MatchString(packet) {
			t.Fatalf("agent packet recommends a superseded choreography step: %s\n%s", banned, packet)
		}
	}
	standalone := v7Packet(vault, packetTaskByID(t, idx, "APP-T-0002"), idx, "agent")
	if strings.Contains(standalone, "tusker wave start") || strings.Contains(standalone, "## Wave context") {
		t.Fatal("standalone packet gained wave instructions")
	}
	if !strings.Contains(standalone, "tusker task start APP-T-0002 --mode interactive --by <agent> --current-workspace --json") {
		t.Fatal("standalone packet lost its task start entry")
	}
}

func TestDirectWavePacketRichDagSpecimenAndTerseContrast(t *testing.T) {
	vault, _, _ := authorityFixture(t)
	writeDirectWave(t, vault, "W-0001", []string{"APP-T-0001", "APP-T-0002"}, map[string]any{"summary": "Model the DAG frontier."})
	rich := "# APP-T-0001\n\n" +
		"## Intent\n\n" +
		"Teach `cmd/tusker/frontier.go:dagAdvance` to walk `type FrontierSet map[string][]string` in edge direction dependency -> dependent (an edge `A -> B` means B requires A completed first).\n\n" +
		"## Failure and edge cases\n\n" +
		"- A cycle `A -> B -> A` must be detected at validation time and reported as `GRAPH_CYCLE` naming the cycle members.\n" +
		"- A dependency on a missing task must be reported as `GRAPH_MISSING_DEPENDENCY` naming the absent ID; never silently drop the edge.\n" +
		"- Readiness: a member is ready only while every required dependency is in a terminal done state.\n" +
		"- Failure propagation: a failed member marks dependents `blocked` and leaves the wave Waiting; it never advances past the failure.\n" +
		"- Update semantics: `task update` on a done member reopens it as rework and must not certify stale proof.\n\n" +
		"## Ownership\n\n" +
		"Upstream: `APP-T-0001` owns `frontier.go`. Downstream: `APP-T-0002` consumes `FrontierSet`. Shared: `run_directives` rows are read-only here.\n\n" +
		"## Decisions\n\n" +
		"Locked: edge direction stays dependency -> dependent. Proposed-but-unapproved: renaming `dagAdvance` to `walkFrontier`.\n\n" +
		"## Acceptance\n\n" +
		"| ID | Outcome |\n| --- | --- |\n" +
		"| A1 | `dagAdvance` releases exactly the frontier whose dependencies are done. |\n\n" +
		"## Verification\n\n" +
		"| Covers | Check | Result | Notes |\n| --- | --- | --- | --- |\n" +
		"| A1 | command: go test ./cmd/tusker -run 'TestDagAdvance' -count=1 | pending | |\n"
	writeDirectTaskBody(t, vault, "APP-T-0001", "W-0001", nil, rich)
	writeDirectTaskBody(t, vault, "APP-T-0002", "W-0001", map[string]any{"dependencies": []any{"APP-T-0001:hard"}}, "# APP-T-0002\n\nImplement a DAG\n")
	idx := mustIndex(t, vault)
	richPacket := v7Packet(vault, packetTaskByID(t, idx, "APP-T-0001"), idx, "agent")
	for _, clause := range []string{
		"cmd/tusker/frontier.go:dagAdvance",
		"type FrontierSet map[string][]string",
		"edge direction dependency -> dependent",
		"`A -> B` means B requires A completed first",
		"`GRAPH_CYCLE` naming the cycle members",
		"`GRAPH_MISSING_DEPENDENCY` naming the absent ID",
		"ready only while every required dependency is in a terminal done state",
		"marks dependents `blocked` and leaves the wave Waiting",
		"reopens it as rework and must not certify stale proof",
		"Downstream: `APP-T-0002` consumes `FrontierSet`",
		"Shared: `run_directives` rows are read-only here.",
		"renaming `dagAdvance` to `walkFrontier`",
		"command: go test ./cmd/tusker -run 'TestDagAdvance' -count=1",
	} {
		if !strings.Contains(richPacket, clause) {
			t.Fatalf("rich DAG packet lost specimen clause %q:\n%s", clause, richPacket)
		}
	}
	terse := packetTaskByID(t, idx, "APP-T-0002")
	tersePacket := v7Packet(vault, terse, idx, "agent")
	if !strings.Contains(tersePacket, "Implement a DAG") {
		t.Fatal("terse body was not preserved verbatim")
	}
	if strings.Contains(tersePacket, "GRAPH_CYCLE") {
		t.Fatal("terse packet was silently padded with sibling content")
	}
}

func TestDirectWavePacketShippedDagExampleValid(t *testing.T) {
	vault, _, _ := authorityFixture(t)
	raw, err := readText(filepath.Join("..", "..", "skills", "tusker", "references", "HANDOFF.md"))
	if err != nil {
		t.Fatal(err)
	}
	marker := "## Substantial DAG example"
	pos := strings.Index(raw, marker)
	if pos < 0 {
		t.Fatal("HANDOFF.md lost the substantial DAG example section")
	}
	rest := raw[pos:]
	start := strings.Index(rest, "```yaml")
	if start < 0 {
		t.Fatal("DAG example is missing its yaml fence")
	}
	rest = rest[start+len("```yaml"):]
	end := strings.Index(rest, "```")
	if end < 0 {
		t.Fatal("DAG example yaml fence is unterminated")
	}
	var req directWaveAuthoringRequest
	decoder := yaml.NewDecoder(strings.NewReader(rest[:end]))
	decoder.KnownFields(true)
	if err := decoder.Decode(&req); err != nil {
		t.Fatalf("shipped DAG example does not decode into directWaveAuthoringRequest: %v", err)
	}
	normalizeDirectWaveAuthoringRequest(&req)
	issues, _ := validateDirectWaveAuthoring(vault, req)
	if len(issues) != 0 {
		t.Fatalf("shipped DAG example fails authoring validation: %#v", issues)
	}
	if len(req.Tasks) != 2 || len(req.Tasks[1].Dependencies) != 1 {
		t.Fatalf("shipped DAG example lost its second-task dependency edge: %#v", req.Tasks)
	}
	dep := req.Tasks[1].Dependencies[0]
	if dep.Task != "frontier-helper" || dep.Kind != "hard" {
		t.Fatalf("dependency edge must be {task: frontier-helper, kind: hard}: %#v", dep)
	}
	writeDirectWave(t, vault, "W-0001", []string{"APP-T-0001"}, nil)
	writeDirectTaskBody(t, vault, "APP-T-0001", "W-0001", nil, directTaskBody("APP-T-0001", "Do scoped work."))
	packet := v7Packet(vault, packetTaskByID(t, mustIndex(t, vault), "APP-T-0001"), mustIndex(t, vault), "agent")
	for _, want := range []string{
		"tusker wave pause W-0001 --by human:<name>|operator:<name>",
		"tusker wave resume W-0001 --by human:<name>|operator:<name>",
	} {
		if !strings.Contains(packet, want) {
			t.Fatalf("agent packet wave control lost the human|operator actor contract %q", want)
		}
	}
}

func TestDirectWavePacketHelpAndCapabilitiesMatchDispatch(t *testing.T) {
	commands := installedCapabilityCommands()
	byName := map[string]capabilityCommand{}
	for _, c := range commands {
		byName[c.Command] = c
	}
	for _, name := range []string{"new", "task start", "task update", "wave create", "wave start", "wave pause", "wave resume"} {
		if _, ok := byName[name]; !ok {
			t.Fatalf("capabilities manifest is missing %q", name)
		}
	}
	for _, flag := range []string{"--body-file", "--work-level"} {
		found := false
		for _, f := range byName["new"].Flags {
			if f == flag {
				found = true
			}
		}
		if !found {
			t.Fatalf("new-task capability lost required flag %s", flag)
		}
	}
	if !strings.Contains(byName["wave start"].Purpose, "dependency frontier automatically") {
		t.Fatalf("wave start purpose does not describe automatic frontier advancement: %q", byName["wave start"].Purpose)
	}
	if !strings.Contains(byName["wave pause"].Purpose, "admissions") || !strings.Contains(byName["wave resume"].Purpose, "armed") {
		t.Fatalf("pause/resume purposes do not match actual semantics: %#v %#v", byName["wave pause"].Purpose, byName["wave resume"].Purpose)
	}
	for _, name := range []string{"new", "task start", "wave create", "wave start", "wave pause", "wave resume"} {
		if strings.Contains(strings.ToLower(byName[name].Purpose), "delivery import") || strings.Contains(strings.ToLower(byName[name].Purpose), "delivery plan") {
			t.Fatalf("canonical %q guidance recommends a delivery plan", name)
		}
	}
	for _, sub := range []string{"start", "pause", "resume", "create", "review"} {
		found := false
		for _, s := range byName["wave"].Subcommands {
			if s == sub {
				found = true
			}
		}
		if !found {
			t.Fatalf("wave capability lost dispatched subcommand %q", sub)
		}
	}
}

// TestSoftwareFactoryPacket is the prescribed focused gate. Keep the cases
// here (rather than relying on a broad package run) so an accidental rename or
// deletion cannot make the exact -run gate silently match zero tests.
func TestSoftwareFactoryPacket(t *testing.T) {
	cases := []struct {
		name string
		fn   func(*testing.T)
	}{
		{name: "file-and-stdin-round-trip", fn: testSoftwareFactoryPacketFileAndStdinRoundTrip},
		{name: "rejects-invalid-refs-and-atomic-batches", fn: testSoftwareFactoryPacketRejectsInvalidRefsAndAtomicBatches},
		{name: "cold-reader-semantic-specimen", fn: testSoftwareFactoryPacketColdReaderSemanticSpecimen},
		{name: "cli-help-dispatch-capability-parity", fn: testSoftwareFactoryPacketCLIHelpDispatchCapabilityParity},
	}
	if len(cases) == 0 {
		t.Fatal("software-factory packet gate has no substantive cases")
	}
	for _, tc := range cases {
		t.Run(tc.name, tc.fn)
	}
}

func testSoftwareFactoryPacketFileAndStdinRoundTrip(t *testing.T) {
	vault, _, project, server := directWaveServeFixture(t, nil, nil)

	fileBody := "# File packet\n\n" +
		"## Intent\n\nPreserve `literal | pipe` and \\*escaped\\* markdown.\n\n" +
		"## Acceptance\n\n| ID | Outcome |\n| --- | --- |\n| A1 | The exact body reaches every projection. |\n\n" +
		"## Verification\n\n| Covers | Check | Result | Notes |\n| --- | --- | --- | --- |\n| A1 | command: go test ./cmd/tusker -run 'TestSoftwareFactoryPacket' -count=1 \\| tee packet.log | pending | |\n"
	filePath := directAuthoringBodyPath(t, vault, "software-factory-file.md", fileBody)
	runSoftwareFactoryPacketCLI(t, "new", "task", "--title", "File packet", "--work-level", "standard", "--body-file", filePath, "--vault", vault, "--quiet")

	stdinBody := "# Stdin packet\n\n## Intent\n\nThe stdin body keeps a `consumer -> provider` interface literal.\n\n## Acceptance\n\n| ID | Outcome |\n| --- | --- |\n| A1 | The stdin contract remains readable. |\n\n## Verification\n\n| Covers | Check | Result | Notes |\n| --- | --- | --- | --- |\n| A1 | command: printf 'stdin\\n' | pending | |\n"
	restore := directAuthoringStdin(t, stdinBody)
	runSoftwareFactoryPacketCLIWithStdin(t, stdinBody, "new", "task", "--title", "Stdin packet", "--work-level", "standard", "--body-file", "-", "--vault", vault, "--quiet")
	restore()

	idx := mustIndex(t, vault)
	checks := []struct {
		id          string
		body        string
		intent      string
		packetCheck string
		apiCheck    string
	}{
		{id: "TSK-T-0001", body: fileBody, intent: "Preserve `literal | pipe` and \\*escaped\\* markdown.", packetCheck: "command: go test ./cmd/tusker -run 'TestSoftwareFactoryPacket' -count=1 \\| tee packet.log", apiCheck: "command: go test ./cmd/tusker -run 'TestSoftwareFactoryPacket' -count=1 | tee packet.log"},
		{id: "TSK-T-0002", body: stdinBody, intent: "The stdin body keeps a `consumer -> provider` interface literal.", packetCheck: "command: printf 'stdin\\n'", apiCheck: "printf 'stdin\\n'"},
	}
	for _, tc := range checks {
		task := packetTaskByID(t, idx, tc.id)
		if strings.TrimPrefix(task.Body, "\n") != tc.body {
			t.Fatalf("%s durable body changed during authoring: got %q want %q", tc.id, task.Body, tc.body)
		}
		for _, audience := range []string{"agent", "reviewer"} {
			packet := v7Packet(vault, task, idx, audience)
			for _, want := range []string{strings.TrimSpace(tc.body), tc.intent, "| A1 |", tc.packetCheck} {
				if !strings.Contains(packet, want) {
					t.Fatalf("%s %s packet lost %q:\n%s", tc.id, audience, want, packet)
				}
			}
		}
	}

	var detail serveTaskDetail
	serveDecode(t, server, "/api/tasks/TSK-T-0001?project="+project.ProjectID, &detail)
	if strings.TrimPrefix(detail.Body, "\n") != fileBody || detail.Intent != checks[0].intent {
		t.Fatalf("API projection changed the file body: body=%q intent=%q", detail.Body, detail.Intent)
	}
	if len(detail.Acceptance) != 1 || detail.Acceptance[0].ID != "A1" || detail.Acceptance[0].Text != "The exact body reaches every projection." {
		t.Fatalf("API acceptance projection lost the authored row: %#v", detail.Acceptance)
	}
	if len(detail.Verification) != 1 || detail.Verification[0].Command != checks[0].apiCheck {
		t.Fatalf("API verification projection changed the semantic command: %#v", detail.Verification)
	}
}

func testSoftwareFactoryPacketRejectsInvalidRefsAndAtomicBatches(t *testing.T) {
	invalidVault := v7DirectTestVault(t)
	invalidRequest := "schema: tusker.wave-authoring/v1\nrequest_key: invalid-ref\ntitle: Invalid ref\noutcome: Must not publish.\nspec_refs:\n  - .tusker/specs/missing.md\ntasks:\n  - key: only\n    title: Only task\n    work_level: standard\n    body: |\n      # Only\n\n      This must never be durable.\n"
	invalidPath := directAuthoringBodyPath(t, invalidVault, "invalid-ref.yaml", invalidRequest)
	if err := waveV7CreateCmd(Args{"vault": invalidVault, "quiet": "true", "file": invalidPath, "request-key": "invalid-ref"}); err == nil || !strings.Contains(err.Error(), "does not resolve") {
		t.Fatalf("invalid reference error=%v, want a pre-publish refusal", err)
	}
	for _, dir := range []string{"tasks", "waves", "gates"} {
		entries, err := os.ReadDir(filepath.Join(invalidVault, "work", dir))
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 0 {
			t.Fatalf("invalid reference published %s records: %v", dir, entries)
		}
	}

	vault := v7DirectTestVault(t)
	request := "schema: tusker.wave-authoring/v1\nrequest_key: packet-v1\ntitle: Packet batch\noutcome: Publish one durable packet batch.\nspec_refs:\n  - .tusker/specs/delivery.md\ntasks:\n  - key: only\n    title: Packet task\n    work_level: standard\n    spec_refs:\n      - .tusker/specs/delivery.md\n    body: |\n      # Packet task\n\n      The request-key receipt is part of the durable boundary.\n"
	request = strings.Replace(request, "      The request-key receipt is part of the durable boundary.\n", "      The request-key receipt is part of the durable boundary.\n\n      ## Acceptance\n\n      | ID | Outcome |\n      | --- | --- |\n      | A1 | The request is durable. |\n", 1)
	request += "human_actions:\n  - key: approval\n    task: only\n    owner: human:operator\n    action: Approve the packet.\n    verification: Human confirms the packet.\n    why_agent_cannot: The approval requires human authority.\n    covers:\n      - A1\n"
	requestPath := directAuthoringBodyPath(t, vault, "packet-batch.yaml", request)
	if err := waveV7CreateCmd(Args{"vault": vault, "quiet": "true", "file": requestPath, "request-key": "packet-v1"}); err != nil {
		t.Fatal(err)
	}
	waveData, _, err := parseFrontmatterMustRead(filepath.Join(vault, "work", "waves", "W-0001.md"))
	if err != nil {
		t.Fatal(err)
	}
	if got := normalizeList(waveData["spec_refs"]); len(got) != 1 || got[0] != ".tusker/specs/delivery.md" {
		t.Fatalf("wave spec_refs were not durably persisted: %#v", waveData["spec_refs"])
	}
	createdTaskData, _ := directAuthoringTaskRecord(t, vault, "TSK-T-0001")
	if got := normalizeList(createdTaskData["spec_refs"]); len(got) != 1 || got[0] != ".tusker/specs/delivery.md" {
		t.Fatalf("task spec_refs were not durably persisted: %#v", createdTaskData["spec_refs"])
	}
	createdGateData, _, err := parseFrontmatterMustRead(filepath.Join(vault, "work", "gates", "TSK-G-0001.md"))
	if err != nil {
		t.Fatal(err)
	}
	if got := normalizeList(createdGateData["covers"]); len(got) != 1 || got[0] != "A1" {
		t.Fatalf("human action gate lost its valid acceptance mapping: %#v", createdGateData["covers"])
	}
	before, beforeBody, err := parseFrontmatterMustRead(filepath.Join(vault, "work", "tasks", "TSK-T-0001.md"))
	if err != nil {
		t.Fatal(err)
	}
	beforeWave, beforeWaveBody, err := parseFrontmatterMustRead(filepath.Join(vault, "work", "waves", "W-0001.md"))
	if err != nil {
		t.Fatal(err)
	}
	beforeEvents := countV7EventFiles(t, vault, "")
	changedRequest := strings.Replace(request, "Packet task", "Changed packet task", 1)
	changedPath := directAuthoringBodyPath(t, vault, "packet-batch-changed.yaml", changedRequest)
	if err := waveV7CreateCmd(Args{"vault": vault, "quiet": "true", "file": changedPath, "request-key": "packet-v1"}); err == nil || !strings.Contains(err.Error(), "already used with different content") {
		t.Fatalf("request-key conflict error=%v, want conflict refusal", err)
	}
	after, afterBody, err := parseFrontmatterMustRead(filepath.Join(vault, "work", "tasks", "TSK-T-0001.md"))
	if err != nil {
		t.Fatal(err)
	}
	afterWave, afterWaveBody, err := parseFrontmatterMustRead(filepath.Join(vault, "work", "waves", "W-0001.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(after, before) || afterBody != beforeBody || !reflect.DeepEqual(afterWave, beforeWave) || afterWaveBody != beforeWaveBody || countV7EventFiles(t, vault, "") != beforeEvents {
		t.Fatalf("request-key conflict partially published: before=%#v/%q after=%#v/%q events=%d/%d", before, beforeBody, after, afterBody, countV7EventFiles(t, vault, ""), beforeEvents)
	}
	invalidCoverageVault := v7DirectTestVault(t)
	invalidCoverageRequest := strings.Replace(request, "request_key: packet-v1", "request_key: invalid-cover", 1)
	invalidCoverageRequest = strings.Replace(invalidCoverageRequest, "      - A1", "      - A9", 1)
	invalidCoveragePath := directAuthoringBodyPath(t, invalidCoverageVault, "invalid-cover.yaml", invalidCoverageRequest)
	invalidCoverageEvents := countV7EventFiles(t, invalidCoverageVault, "")
	if err := waveV7CreateCmd(Args{"vault": invalidCoverageVault, "quiet": "true", "file": invalidCoveragePath, "request-key": "invalid-cover"}); err == nil || !strings.Contains(err.Error(), "covers unknown acceptance id A9") {
		t.Fatalf("invalid human-action coverage error=%v, want pre-publish refusal", err)
	}
	for _, dir := range []string{"tasks", "waves", "gates"} {
		entries, readErr := os.ReadDir(filepath.Join(invalidCoverageVault, "work", dir))
		if readErr != nil {
			t.Fatal(readErr)
		}
		if len(entries) != 0 {
			t.Fatalf("invalid human-action coverage published %s records: %v", dir, entries)
		}
	}
	if got := countV7EventFiles(t, invalidCoverageVault, ""); got != invalidCoverageEvents {
		t.Fatalf("invalid human-action coverage emitted events: before=%d after=%d", invalidCoverageEvents, got)
	}
	verifyPath := filepath.Join(vault, "work", "tasks", "TSK-T-0001.md")
	verifyBodyBefore := mustBody(t, verifyPath)
	verifyEventsBefore := countV7EventFiles(t, vault, "TSK-T-0001")
	verifyOutput, verifyErr := runSoftwareFactoryPacketProcessAllowFailure(t, nil, "verify", "add", "TSK-T-0001", "--covers", "A9", "--check", "command: printf invalid", "--result", "pending", "--by", "agent:test", "--vault", vault, "--quiet")
	if verifyErr == nil {
		t.Fatalf("real verify add surface accepted invalid structured acceptance mapping A9: %s", verifyOutput)
	}
	if after := mustBody(t, verifyPath); after != verifyBodyBefore {
		t.Fatalf("rejected invalid structured mapping mutated the task body")
	}
	if got := countV7EventFiles(t, vault, "TSK-T-0001"); got != verifyEventsBefore {
		t.Fatalf("rejected invalid structured mapping emitted events: before=%d after=%d", verifyEventsBefore, got)
	}

	atomicVault := v7DirectTestVault(t)
	atomicPath := directAuthoringBodyPath(t, atomicVault, "packet-atomic.yaml", request)
	atomicEvents := countV7EventFiles(t, atomicVault, "")
	if err := waveV7CreateCmd(Args{"vault": atomicVault, "quiet": "true", "file": atomicPath, "request-key": "packet-atomic", "fail-after-write-count": "4"}); err == nil {
		t.Fatal("injected batch write unexpectedly succeeded")
	}
	for _, dir := range []string{"tasks", "waves", "gates"} {
		entries, err := os.ReadDir(filepath.Join(atomicVault, "work", dir))
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 0 {
			t.Fatalf("injected batch failure left %s records: %v", dir, entries)
		}
	}
	if got := countV7EventFiles(t, atomicVault, ""); got != atomicEvents {
		t.Fatalf("injected batch failure emitted events: before=%d after=%d", atomicEvents, got)
	}
	if _, err := os.Stat(filepath.Join(atomicVault, "work", "waves", "W-0001.md")); !os.IsNotExist(err) {
		t.Fatalf("injected batch failure left a wave receipt/record: err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(atomicVault, "work", "gates", "TSK-G-0001.md")); !os.IsNotExist(err) {
		t.Fatalf("injected batch failure left the staged human-action gate: err=%v", err)
	}
}

func testSoftwareFactoryPacketColdReaderSemanticSpecimen(t *testing.T) {
	vault := v7DirectTestVault(t)
	request := "schema: tusker.wave-authoring/v1\nrequest_key: semantic-v1\ntitle: Semantic packet\noutcome: Preserve a consumer/provider boundary with evidence-aware review.\nshared_context: |\n  The provider owns the interface; the downstream task owns final integration.\ntasks:\n  - key: contract\n    title: Preserve the contract\n    work_level: demanding\n    body: |\n      # Preserve the contract\n\n      ## Intent\n\n      Inspect `cmd/tusker/commands_v7.go: v7Packet` and keep the durable task body authoritative.\n\n      ## Cases\n\n      - C1: A queued job proves admission, not receiver execution.\n      - C2: Unknown evidence remains pending; a self-declared boundary label is not proof.\n\n      ## Decisions\n\n      Locked: the worker supplies the interface and the downstream consumer owns integration.\n      Proposed: add a new registry is not approved.\n\n      ## Acceptance\n\n      | ID | Outcome |\n      | --- | --- |\n      | A1 | The provider contract and reviewer boundary are explicit. |\n\n      ## Verification\n\n      | Covers | Check | Result | Notes |\n      | --- | --- | --- | --- |\n      | A1 | command: go test ./cmd/tusker -run TestSoftwareFactoryPacketColdReaderSemanticSpecimen -count=1 | pending | |\n"
	path := directAuthoringBodyPath(t, vault, "semantic.yaml", request)
	if err := waveV7CreateCmd(Args{"vault": vault, "quiet": "true", "file": path, "request-key": "semantic-v1"}); err != nil {
		t.Fatal(err)
	}
	idx := mustIndex(t, vault)
	task := packetTaskByID(t, idx, "TSK-T-0001")
	for _, audience := range []string{"agent", "reviewer"} {
		packet := v7Packet(vault, task, idx, audience)
		for _, clause := range []string{
			"cmd/tusker/commands_v7.go: v7Packet",
			"C1: A queued job proves admission, not receiver execution.",
			"C2: Unknown evidence remains pending; a self-declared boundary label is not proof.",
			"Locked: the worker supplies the interface and the downstream consumer owns integration.",
			"Proposed: add a new registry is not approved.",
			"command: go test ./cmd/tusker -run TestSoftwareFactoryPacketColdReaderSemanticSpecimen -count=1",
		} {
			if !strings.Contains(packet, clause) {
				t.Fatalf("%s packet lost cold-reader clause %q:\n%s", audience, clause, packet)
			}
		}
		if !strings.Contains(packet, "The provider owns the interface; the downstream task owns final integration.") {
			t.Fatalf("%s packet lost exact shared_context propagation:\n%s", audience, packet)
		}
	}
	if strings.Contains(v7Packet(vault, task, idx, "agent"), "## Packet metadata required") {
		t.Fatal("semantic packet introduced a mandatory metadata section")
	}

	raw, err := readText(filepath.Join("..", "..", "skills", "tusker", "references", "HANDOFF.md"))
	if err != nil {
		t.Fatal(err)
	}
	normalizedGuidance := strings.Join(strings.Fields(raw), " ")
	for _, guidance := range []string{"There is no mandatory heading count, word quota, or keyword rubric", "fresh reader", "packet inspection is never execution authorization"} {
		if !strings.Contains(normalizedGuidance, guidance) {
			t.Fatalf("HANDOFF.md lost evidence-aware guidance %q", guidance)
		}
	}
	run, err := readText(filepath.Join("..", "..", "skills", "tusker", "references", "RUN.md"))
	if err != nil {
		t.Fatal(err)
	}
	normalizedRun := strings.Join(strings.Fields(run), " ")
	for _, guidance := range []string{"Admission, receiver execution, product output, recovery, quality and performance are distinct claims.", "Unknown cost or delivery remains unknown."} {
		if !strings.Contains(normalizedRun, guidance) {
			t.Fatalf("RUN.md lost boundary-aware guidance %q", guidance)
		}
	}
}

func testSoftwareFactoryPacketCLIHelpDispatchCapabilityParity(t *testing.T) {
	help := captureStdout(t, func() {
		if !printCommandHelp("wave create") {
			t.Fatal("wave create help is not registered")
		}
	})
	normalizedHelp := strings.Join(strings.Fields(help), " ")
	for _, want := range []string{
		`tusker wave create "<title>" <TASK-ID>...`,
		`tusker wave create --file <path|-> --request-key <stable-key> [--json]`,
		`atomically authors`,
		`changed request`,
	} {
		if !strings.Contains(normalizedHelp, want) {
			t.Fatalf("wave create help lost exact contract %q:\n%s", want, help)
		}
	}
	processHelp := strings.Join(strings.Fields(runSoftwareFactoryPacketCLI(t, "wave", "create", "--help")), " ")
	if !strings.Contains(processHelp, `tusker wave create --file <path|-> --request-key <stable-key> [--json]`) {
		t.Fatalf("compiled wave create help lost file-input contract: %s", processHelp)
	}
	command, args := parseCLI([]string{"tusker", "wave", "create", "--file", "-", "--request-key", "packet-v1"})
	if command != "wave create" || args.String("file") != "-" || args.String("request-key") != "packet-v1" {
		t.Fatalf("wave create parser/dispatch drifted: command=%q args=%#v", command, args)
	}
	capabilityOutput := runSoftwareFactoryPacketCLI(t, "capabilities", "--json")
	var manifest capabilitiesManifest
	if err := json.Unmarshal([]byte(capabilityOutput), &manifest); err != nil {
		t.Fatalf("capabilities output is not JSON: %v\n%s", err, capabilityOutput)
	}
	var waveCreate capabilityCommand
	for _, candidate := range manifest.Commands {
		if candidate.Command == "wave create" {
			waveCreate = candidate
			break
		}
	}
	if waveCreate.Command != "wave create" || !strings.Contains(waveCreate.Purpose, "atomically") {
		t.Fatalf("capability manifest lost wave-create parity: %#v", waveCreate)
	}
}

func runSoftwareFactoryPacketCLI(t *testing.T, argv ...string) string {
	t.Helper()
	return runSoftwareFactoryPacketProcess(t, nil, argv...)
}

func runSoftwareFactoryPacketCLIWithStdin(t *testing.T, stdin string, argv ...string) string {
	t.Helper()
	return runSoftwareFactoryPacketProcess(t, strings.NewReader(stdin), argv...)
}

func runSoftwareFactoryPacketProcess(t *testing.T, stdin io.Reader, argv ...string) string {
	t.Helper()
	output, err := runSoftwareFactoryPacketProcessAllowFailure(t, stdin, argv...)
	if err != nil {
		t.Fatalf("compiled Tusker CLI %v failed: %v\n%s", argv, err, output)
	}
	return output
}

func runSoftwareFactoryPacketProcessAllowFailure(t *testing.T, stdin io.Reader, argv ...string) (string, error) {
	t.Helper()
	cmd := exec.Command(demoTestBinary(t), argv...)
	cmd.Dir = repoRootForTest()
	cmd.Stdin = stdin
	output, err := cmd.CombinedOutput()
	return string(output), err
}
