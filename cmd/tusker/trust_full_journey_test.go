package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// trustJourneyCLI exercises the production parser and command router. Fixture
// setup is deliberately separate: it creates an isolated Git repository and
// runtime registry, but no daemon, dispatcher, or external provider.
func trustJourneyCLI(t *testing.T, vault string, argv ...string) string {
	t.Helper()
	full := append([]string{"tusker"}, argv...)
	var code int
	var runErr error
	output := captureStdout(t, func() {
		command, args := parseCLI(full)
		args["vault"] = vault
		args["quiet"] = "true"
		code, runErr = run(command, args)
	})
	if code != 0 || runErr != nil {
		t.Fatalf("%s failed: code=%d err=%v output=%s", strings.Join(full, " "), code, runErr, output)
	}
	return output
}

func trustJourneyPacket(t *testing.T, output string) workSessionPacket {
	t.Helper()
	var packet workSessionPacket
	if err := json.Unmarshal([]byte(output), &packet); err != nil {
		t.Fatalf("decode work-session packet: %v\n%s", err, output)
	}
	return packet
}

func trustJourneyReadyTask(t *testing.T, vault, id string) {
	t.Helper()
	setAutomationV7TaskFields(t, vault, id, map[string]any{
		"status": "ready", "readiness": "ready", "next_owner": "agent", "work_revision": 1,
	})
}

func TestTrustFullJourney(t *testing.T) {
	t.Setenv("TUSKER_STATE_ROOT", filepath.Join(t.TempDir(), "state"))
	previousWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previousWD) })

	repo := t.TempDir()
	initializeOrchestrationGitRepo(t, repo)
	if err := os.MkdirAll(filepath.Join(repo, "docs", "specs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "docs", "specs", "journey.md"), []byte("# Locked journey decision\n\nOnly owned/journey.txt is implementation material.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitDir(t, repo, "add", "docs/specs/journey.md")
	runGitDir(t, repo, "commit", "-m", "add locked journey specification")
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}

	vault := filepath.Join(repo, ".tusker")
	trustJourneyCLI(t, vault, "init", "--vault", vault, "--yes", "--vault-only", "--no-mount")
	plan := validDeliveryPlanV2()
	plan.HumanGates = nil
	plan.Scope, plan.Title, plan.SpecRefs = "trust-full-journey", "Trust full journey", []string{"docs/specs/journey.md"}
	plan.Epic, plan.EpicContract = "", &deliveryEpicContract{SourceKey: "trust-full-journey", AcronymHint: "JNY", Title: "Trust full journey"}
	plan.Requirements = []deliveryRequirement{{ID: "R1", Outcome: "The implementation is confined to its declared material."}, {ID: "R2", Outcome: "The dependent task follows the implementation."}}
	plan.Tasks = []deliveryPlanTask{
		{SourceKey: "implement", RequirementRefs: []string{"R1"}, Title: "Implement declared material", Outcome: "Write the locked implementation.", Acceptance: []deliveryAcceptance{{ID: "A1", Outcome: "The declared implementation exists."}}, Verification: []deliveryVerification{{Covers: "A1", Check: "command: true"}}, Artifact: deliveryArtifactContract{Kind: "diff_summary", Path: "owned/journey.txt", Summary: "Journey implementation.", AcceptanceIDs: []string{"A1"}}, OwnedPaths: []string{"owned/journey.txt"}, Priority: "p1", Risk: "low"},
		{SourceKey: "dependent-doc", RequirementRefs: []string{"R2"}, Title: "Follow the implementation", Outcome: "Stay outside the implementation frontier until it succeeds.", Acceptance: []deliveryAcceptance{{ID: "A1", Outcome: "The dependency is durable."}}, Verification: []deliveryVerification{{Covers: "A1", Check: "command: true"}}, Dependencies: []deliveryDependency{{Task: "implement", Kind: "hard"}}, Artifact: deliveryArtifactContract{Kind: "diff_summary", Path: "docs/followup.md", Summary: "Dependent follow-up.", AcceptanceIDs: []string{"A1"}}, OwnedPaths: []string{"docs/followup.md"}, Priority: "p1", Risk: "low"},
	}
	plan.ContextFingerprint = deliveryPlanV2ContextFingerprint(t, vault, plan)
	rawPlan, err := yaml.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	planPath := filepath.Join(repo, "journey.yaml")
	if err := os.WriteFile(planPath, rawPlan, 0o644); err != nil {
		t.Fatal(err)
	}
	trustJourneyCLI(t, vault, "delivery", "import", "--plan", planPath, "--by", "agent:fixture")

	const taskID = "JNY-T-0001"
	task, err := resolveV7Note(vault, taskID, "task")
	if err != nil {
		t.Fatal(err)
	}
	if got := normalizeList(task.Data["owned_paths"]); strings.Join(got, ",") != "owned/journey.txt" || strings.Join(normalizeList(task.Data["spec_refs"]), ",") != "docs/specs/journey.md" {
		t.Fatalf("delivery import lost declared material: %#v", task.Data)
	}
	dependent, err := resolveV7Note(vault, "JNY-T-0002", "task")
	if err != nil {
		t.Fatal(err)
	}
	if got := normalizeList(dependent.Data["dependencies"]); strings.Join(got, ",") != taskID+":hard" {
		t.Fatalf("delivery import lost DAG edge: %#v", dependent.Data)
	}

	trustJourneyReadyTask(t, vault, taskID)
	registerAutomationTestProject(t, vault)
	runGitDir(t, repo, "add", ".")
	runGitDir(t, repo, "commit", "-m", "seed interactive journey contract")

	first := trustJourneyPacket(t, trustJourneyCLI(t, vault, "work", "start", taskID, "--by", "agent:implementer", "--source", "codex"))
	if first.Run == nil || first.Run.ActiveAttemptID == "" || first.Workspace == "" {
		t.Fatalf("first interactive session lacks durable identity: %#v", first)
	}
	trustJourneyCLI(t, vault, "work", "fail", taskID, "--by", "agent:implementer", "--reason", "injected provider interruption")

	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	run, err := store.FindRun(taskID)
	if err != nil || run == nil || run.AttemptOutcome != string(AttemptOutcomeFailed) || run.LeaseState != string(LeaseStateReleased) {
		_ = store.Close()
		t.Fatalf("interruption did not release a recoverable session: run=%#v err=%v", run, err)
	}
	_ = store.Close()

	recovered := trustJourneyPacket(t, trustJourneyCLI(t, vault, "work", "start", taskID, "--by", "agent:implementer", "--source", "codex"))
	if recovered.Run == nil || recovered.Run.ActiveAttemptID == first.Run.ActiveAttemptID || recovered.Workspace != first.Workspace {
		t.Fatalf("recovery did not create a new attempt in the retained workspace: first=%#v recovered=%#v", first, recovered)
	}
	if err := os.MkdirAll(filepath.Join(recovered.Workspace, "owned"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(recovered.Workspace, "owned", "journey.txt"), []byte("recovered implementation\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitDir(t, recovered.Workspace, "add", "owned/journey.txt")
	runGitDir(t, recovered.Workspace, "commit", "-m", "implement recovered journey")
	trustJourneyCLI(t, vault, "work", "submit", taskID, "--by", "agent:implementer", "--deliverable", "recovered implementation", "--verification", "A1 command will run during review", "--gate-verdicts", "A1=pass")

	store, err = OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	run, err = store.FindRun(taskID)
	if err != nil || run == nil {
		_ = store.Close()
		t.Fatalf("submitted run unavailable: %#v err=%v", run, err)
	}
	attempts, err := store.ListAttemptsForRun(run.ProjectID, run.RecordID)
	_ = store.Close()
	var succeeded RunAttempt
	failed := false
	for _, attempt := range attempts {
		switch attempt.Outcome {
		case string(AttemptOutcomeFailed):
			failed = true
		case string(AttemptOutcomeSucceeded):
			succeeded = attempt
		}
	}
	if err != nil || len(attempts) != 2 || !failed || succeeded.AttemptID == "" || succeeded.EndState.HeadSHA == "" {
		t.Fatalf("failure/recovery attempts were not durable: %#v err=%v", attempts, err)
	}
	setAutomationV7TaskFields(t, vault, taskID, map[string]any{"source_sha": succeeded.EndState.HeadSHA})

	review := trustJourneyPacket(t, trustJourneyCLI(t, vault, "work", "review", taskID, "--by", "reviewer:agent", "--source", "codex"))
	if review.Action != "review" || review.Run == nil || review.Run.Lane != runLaneReview || review.ImplementationAttempt != succeeded.AttemptID || review.ImplementationActor != "agent:implementer" || review.MaterialFingerprint == "" || !strings.Contains(review.Next, "--material-fingerprint "+review.MaterialFingerprint) {
		t.Fatalf("review packet omitted independent immutable provenance: %#v", review)
	}
	trustJourneyCLI(t, vault, "review", "submit", taskID, "--attempt", review.Run.ActiveAttemptID, "--by", "reviewer:agent", "--task-rev", stringField(closedTaskData(t, vault, taskID), "state_rev"), "--source-sha", succeeded.EndState.HeadSHA, "--work-rev", strconv.Itoa(review.Revision), "--proof-fingerprint", review.ProofFingerprint, "--gate-fingerprint", review.GateFingerprint, "--material-fingerprint", review.MaterialFingerprint, "--verdict", "pass", "--covers", "A1", "--summary", "independent review passed")
	trustJourneyCLI(t, vault, "close", taskID, "--by", "reviewer:agent", "--reason", "independent review and command proof passed")

	closed, err := resolveV7Note(vault, taskID, "task")
	if err != nil || stringField(closed.Data, "status") != "done" {
		t.Fatalf("reviewed journey did not close: task=%#v err=%v", closed.Data, err)
	}
	if fileExists(filepath.Join(repo, "owned", "journey.txt")) {
		t.Fatal("implementation leaked from its isolated workspace into the base checkout")
	}
}

func closedTaskData(t *testing.T, vault, id string) map[string]any {
	t.Helper()
	note, err := resolveV7Note(vault, id, "task")
	if err != nil {
		t.Fatal(err)
	}
	return note.Data
}
