package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// TestTaskAuthoringJourney is the deterministic half of FLW-T-0041. It uses
// the public CLI parser/router for direct wave authoring, route preview, and
// standalone task creation, then inspects the same persisted packets a fresh
// worker receives. It deliberately does not launch a model, daemon, or browser.
func TestTaskAuthoringJourney(t *testing.T) {
	t.Setenv("TUSKER_STATE_ROOT", filepath.Join(t.TempDir(), "state"))
	vault := v7DirectTestVault(t)
	repo := v7RepoRoot(vault)
	initializeOrchestrationGitRepo(t, repo)
	configureTaskAuthoringJourneyProfiles(t, vault)
	if err := os.MkdirAll(filepath.Join(repo, "owned"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"bounded.txt", "cross-file.txt", "risk-case.txt"} {
		if err := writeText(filepath.Join(repo, "owned", name), "fixture\n"); err != nil {
			t.Fatal(err)
		}
	}
	if err := newV7Epic(Args{"vault": vault, "quiet": "true", "acronym": "JNY", "title": "Task authoring journey"}); err != nil {
		t.Fatal(err)
	}

	requestPath := taskAuthoringJourneyRequest(t, vault)
	runGitDir(t, repo, "add", ".")
	runGitDir(t, repo, "commit", "-m", "seed task authoring journey")

	createOutput := trustJourneyCLI(t, vault, "wave", "create", "--file", requestPath, "--request-key", "journey", "--by", "agent:architect-origin", "--json")
	var envelope struct {
		OK    bool `json:"ok"`
		Inert bool `json:"inert"`
		Wave  struct {
			WaveID      string            `json:"waveId"`
			TaskMapping map[string]string `json:"taskMapping"`
		} `json:"wave"`
	}
	if err := json.Unmarshal([]byte(createOutput), &envelope); err != nil {
		t.Fatalf("wave create did not return its public JSON contract: %v\n%s", err, createOutput)
	}
	if !envelope.OK || !envelope.Inert || envelope.Wave.WaveID == "" || len(envelope.Wave.TaskMapping) != 3 {
		t.Fatalf("authoring response omitted the inert wave boundary: %#v", envelope)
	}

	idx, err := loadV7Index(vault)
	if err != nil {
		t.Fatal(err)
	}
	wave := idx.Waves[envelope.Wave.WaveID]
	if stringField(wave.Data, "authorization") != "disarmed" || stringField(wave.Data, "status") != "open" {
		t.Fatalf("authoring must leave the wave disarmed: %#v", wave.Data)
	}
	waveBody := mustReadIndexTest(t, wave.AbsolutePath)
	for _, fragment := range []string{"## Intended result", "## Ordered work", "## Blockers and human actions", "## Closure criteria", "human:operator"} {
		assertContainsIndexTest(t, waveBody, fragment)
	}

	levels := map[string]string{"bounded": "light", "cross-file": "standard", "risk-case": "demanding"}
	for key, level := range levels {
		id := envelope.Wave.TaskMapping[key]
		task := idx.Tasks[id]
		if task.Data == nil {
			t.Fatalf("authoring omitted task %s (%s)", key, id)
		}
		if got := stringField(task.Data, "work_level"); got != level {
			t.Fatalf("%s work level=%q want %q", id, got, level)
		}
		body := mustReadIndexTest(t, task.AbsolutePath)
		for _, fragment := range []string{"## Intent", "## Implementation notes", "## Acceptance", "## Verification"} {
			assertContainsIndexTest(t, body, fragment)
		}
	}
	crossID := envelope.Wave.TaskMapping["cross-file"]
	if got := normalizeList(idx.Tasks[crossID].Data["gates"]); len(got) != 1 {
		t.Fatalf("cross-file task lost its named human gate: %#v", idx.Tasks[crossID].Data["gates"])
	}

	// The configured tier mapping owns vendor/model selection. Public previews
	// must agree for both lanes while the task itself remains model-neutral.
	boundedID := envelope.Wave.TaskMapping["bounded"]
	var execute runnerRoutePreview
	if err := json.Unmarshal([]byte(trustJourneyCLI(t, vault, "runner", "route", boundedID, "--lane", runLaneExecute, "--json")), &execute); err != nil {
		t.Fatal(err)
	}
	var review runnerRoutePreview
	if err := json.Unmarshal([]byte(trustJourneyCLI(t, vault, "runner", "route", boundedID, "--lane", runLaneReview, "--json")), &review); err != nil {
		t.Fatal(err)
	}
	if execute.Profile != "journey-light-worker" || execute.Model != "fixture-light-worker" || len(execute.Blockers) != 0 {
		t.Fatalf("execute route did not resolve the configured Tier 1 worker: %#v", execute)
	}
	if review.Profile != "journey-light-reviewer" || review.Model != "fixture-light-reviewer" || len(review.Blockers) != 0 {
		t.Fatalf("review route did not resolve the configured independent reviewer: %#v", review)
	}

	// A standalone task remains legal without a synthetic one-member wave, but
	// receives the same tier and route contract as a batch member.
	standaloneID := "APP-T-0002"
	standaloneBody := filepath.Join(v7RepoRoot(vault), ".tusker", "scratch", "standalone-body.md")
	if err := writeText(standaloneBody, "# Standalone authoring case\n\n## Intent\n\nProve standalone task authoring.\n\n## Acceptance\n\n| ID | Outcome |\n| --- | --- |\n| A1 | Task exists with its contract. |\n"); err != nil {
		t.Fatal(err)
	}
	trustJourneyCLI(t, vault, "new", "task", "--id", standaloneID, "--epic", "APP", "--title", "Standalone authoring case", "--by", "agent:architect", "--work-level", "light", "--body-file", standaloneBody, "--spec-refs", ".tusker/specs/delivery.md", "--owned-paths", "owned/standalone.txt")
	standalone, err := resolveV7Note(vault, standaloneID, "task")
	if err != nil {
		t.Fatal(err)
	}
	if stringField(standalone.Data, "wave") != "" || stringField(standalone.Data, "work_level") != "light" {
		t.Fatalf("standalone task was silently wrapped or lost classification: %#v", standalone.Data)
	}
	standaloneRoute := trustJourneyCLI(t, vault, "runner", "route", standaloneID, "--lane", runLaneExecute, "--json")
	var standalonePreview runnerRoutePreview
	if err := json.Unmarshal([]byte(standaloneRoute), &standalonePreview); err != nil {
		t.Fatal(err)
	}
	if standalonePreview.Profile != "journey-light-worker" || len(standalonePreview.Blockers) != 0 {
		t.Fatalf("standalone task does not use the same tier route: %#v", standalonePreview)
	}

	// An open human gate is a hard execution boundary. The public start path
	// must explain the gate and create no runtime ownership record.
	// Move the authored task to a dispatchable state through the same persisted
	// task fields used by the public status command's ready transition. This
	// keeps the fixture focused on the next public boundary: admission must
	// still refuse the task because its human gate is open.
	setAutomationV7TaskFields(t, vault, crossID, map[string]any{
		"status":        "ready",
		"readiness":     "ready",
		"next_owner":    "agent",
		"work_revision": 1,
	})
	idx, err = loadV7Index(vault)
	if err != nil {
		t.Fatal(err)
	}
	gateIDs := normalizeList(idx.Tasks[crossID].Data["gates"])
	if len(gateIDs) != 1 || !boolField(idx.Gates[gateIDs[0]].Data, "blocking") || stringField(idx.Gates[gateIDs[0]].Data, "status") != "open" {
		t.Fatalf("authored human gate is not open/blocking: task=%#v gate=%#v", idx.Tasks[crossID].Data, idx.Gates)
	}
	if blocker := workSessionOpenHumanGateBlocker(idx.Tasks[crossID], idx); blocker == nil {
		t.Fatalf("authored human gate did not produce the canonical admission blocker: %#v", idx.Gates[gateIDs[0]].Data)
	}
	registerAutomationTestProject(t, vault)
	if err := func() error {
		full := []string{"tusker", "work", "start", crossID, "--by", "agent:worker", "--source", "codex"}
		var runErr error
		captureStdout(t, func() {
			command, args := parseCLI(full)
			args["vault"], args["quiet"] = vault, "true"
			_, runErr = run(command, args)
		})
		return runErr
	}(); err == nil || !strings.Contains(strings.ToLower(err.Error()), "human") {
		t.Fatalf("open human gate was not a public start blocker: %v", err)
	}
}

func taskAuthoringJourneyTaskBody(title, outcome, notes, check string) string {
	return "# " + title + "\n\n## Intent\n\n" + outcome + "\n\n## Implementation notes\n\n" + notes +
		"\n\n## Acceptance\n\n| ID | Outcome |\n| --- | --- |\n| A1 | " + outcome + " |\n\n## Verification\n\n| Covers | Check | Result |\n| --- | --- | --- |\n| A1 | " + check + " | pending |\n"
}

func taskAuthoringJourneyRequest(t *testing.T, vault string) string {
	t.Helper()
	newTask := func(key, level, title, outcome, notes, check, artifact string, deps ...map[string]any) map[string]any {
		entry := map[string]any{
			"key":               key,
			"title":             title,
			"work_level":        level,
			"epic":              "JNY",
			"spec_refs":         []string{".tusker/specs/delivery.md"},
			"body":              taskAuthoringJourneyTaskBody(title, outcome, notes, check),
			"owned_paths":       []string{artifact},
			"generated_outputs": []string{artifact},
		}
		if len(deps) > 0 {
			entry["dependencies"] = deps
		}
		return entry
	}
	request := map[string]any{
		"schema":      "tusker.wave-authoring/v1",
		"request_key": "journey",
		"title":       "Task authoring journey",
		"outcome":     "A fresh authoring contract retains tier choices, human authority, provenance, and a readable wave.",
		"spec_refs":   []string{".tusker/specs/delivery.md"},
		"concurrency": 1,
		"tasks": []map[string]any{
			newTask("bounded", "light", "Implement the bounded result",
				"Write the exact bounded result in its declared path.",
				"Work within the declared owned path and preserve the existing task contract. Escalate any material scope or interface question to codex:architect-origin before proceeding.",
				`command: test "$(cat owned/bounded.txt)" = "bounded result"`, "owned/bounded.txt"),
			newTask("cross-file", "standard", "Coordinate the cross-file change",
				"Apply the settled cross-file change after the named human decision.",
				"Follow the surrounding task dependency and wait for the named product decision before editing. Preserve the existing interfaces and escalate material scope or interface questions to codex:architect-origin.",
				"command: test -f owned/cross-file.txt", "owned/cross-file.txt"),
			newTask("risk-case", "demanding", "Prove the risk case",
				"Exercise the prescribed recovery-sensitive risk case without widening scope.",
				"Use the upstream cross-file result and keep recovery behavior inside the declared owned path. Do not widen the contract; escalate any material scope or interface question to codex:architect-origin.",
				"command: test -f owned/risk-case.txt", "owned/risk-case.txt",
				map[string]any{"task": "cross-file", "kind": "hard"}),
		},
		"human_actions": []map[string]any{{
			"key":              "product-decision",
			"task":             "cross-file",
			"owner":            "human:operator",
			"action":           "Resolve the documented cross-file product conflict.",
			"verification":     "The selected decision is recorded in the governing specification.",
			"why_agent_cannot": "Only the product owner can resolve this product conflict.",
		}},
	}
	raw, err := yaml.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(vault, "scratch", "task-authoring-journey.yaml")
	if err := writeText(path, string(raw)); err != nil {
		t.Fatal(err)
	}
	return path
}

func configureTaskAuthoringJourneyProfiles(t *testing.T, vault string) {
	t.Helper()
	profiles := []struct {
		level   string
		lane    string
		model   string
		harness string
	}{
		{"light", runLaneExecute, "fixture-light-worker", "journey-light-worker"},
		{"light", runLaneReview, "fixture-light-reviewer", "journey-light-reviewer"},
		{"standard", runLaneExecute, "fixture-standard-worker", "journey-standard-worker"},
		{"standard", runLaneReview, "fixture-standard-reviewer", "journey-standard-reviewer"},
		{"demanding", runLaneExecute, "fixture-demanding-worker", "journey-demanding-worker"},
		{"demanding", runLaneReview, "fixture-demanding-reviewer", "journey-demanding-reviewer"},
	}
	levelProfiles := map[string]map[string][]string{}
	for _, item := range profiles {
		profile := directEmergencyRunnerProfileForTest()
		profile["model"] = item.model
		profile["effort"] = "medium"
		profile["eligible_tiers"] = []string{item.level}
		if _, err := setProjectLocalConfigWithReadback(vault, "automation.profiles."+item.harness, profile); err != nil {
			t.Fatal(err)
		}
		if levelProfiles[item.level] == nil {
			levelProfiles[item.level] = map[string][]string{}
		}
		levelProfiles[item.level][item.lane] = []string{item.harness}
	}
	for level, lanes := range levelProfiles {
		for lane, names := range lanes {
			if _, err := setProjectLocalConfigWithReadback(vault, "automation.model_levels."+level+"."+lane, names); err != nil {
				t.Fatal(err)
			}
		}
	}
}
