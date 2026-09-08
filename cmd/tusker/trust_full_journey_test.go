package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

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
	for _, scenario := range []string{"source-bound command proof", "verification changes implementation", "stale verification confirmation"} {
		t.Run(scenario, func(t *testing.T) { trustFullJourneyScenario(t, scenario) })
	}
}

func trustFullJourneyScenario(t *testing.T, scenario string) {
	t.Setenv("TUSKER_STATE_ROOT", filepath.Join(t.TempDir(), "state"))
	previousWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previousWD) })

	repo := t.TempDir()
	initializeOrchestrationGitRepo(t, repo)
	if err := os.MkdirAll(filepath.Join(repo, ".tusker", "specs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".tusker", "specs", "journey.md"), []byte("---\nsubject: journey\npart_of: overview\n---\n# Locked journey decision\n\nOnly owned/journey.txt is implementation material.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// The review must execute a real focused assertion, not label `true` as
	// focused-test proof. This test is committed before either work attempt.
	if err := writeText(filepath.Join(repo, "tests", "test_journey.py"), `from pathlib import Path
import unittest

class JourneyTest(unittest.TestCase):
    def test_recovered_implementation(self):
        self.assertEqual(Path("owned/journey.txt").read_text(), "recovered implementation\n")
`); err != nil {
		t.Fatal(err)
	}
	runGitDir(t, repo, "add", ".tusker/specs/journey.md", "tests/test_journey.py")
	runGitDir(t, repo, "commit", "-m", "add locked journey specification")
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}

	vault := filepath.Join(repo, ".tusker")
	trustJourneyCLI(t, vault, "init", "--vault", vault, "--yes", "--vault-only", "--no-mount")
	if _, err := setProjectLocalConfigWithReadback(vault, "automation.validation.commands", []string{"true"}); err != nil {
		t.Fatal(err)
	}
	plan := validDeliveryPlanV2()
	plan.HumanGates = nil
	plan.Scope, plan.Title, plan.SpecRefs = "trust-full-journey", "Trust full journey", []string{".tusker/specs/journey.md"}
	plan.Epic, plan.EpicContract = "", &deliveryEpicContract{SourceKey: "trust-full-journey", AcronymHint: "JNY", Title: "Trust full journey"}
	plan.Requirements = []deliveryRequirement{{ID: "R1", Outcome: "The implementation is confined to its declared material."}, {ID: "R2", Outcome: "The dependent task follows the implementation."}}
	verificationCommand := "python3 -m unittest discover -s tests -p test_journey.py && printf x >> verification-count"
	if scenario == "verification changes implementation" {
		verificationCommand += " && printf 'changed after review' > owned/journey.txt"
	}
	plan.Tasks = []deliveryPlanTask{
		{SourceKey: "implement", RequirementRefs: []string{"R1"}, Title: "Implement declared material", Outcome: "Write the locked implementation.", Acceptance: []deliveryAcceptance{{ID: "A1", Outcome: "The declared implementation exists."}}, Verification: []deliveryVerification{{Covers: "A1", Check: "command: " + verificationCommand}}, Artifact: deliveryArtifactContract{Kind: "diff_summary", Path: "owned/journey.txt", Summary: "Journey implementation.", AcceptanceIDs: []string{"A1"}}, OwnedPaths: []string{"owned/journey.txt"}, Priority: "p1", Risk: "low"},
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
	runGitDir(t, repo, "add", ".")
	runGitDir(t, repo, "commit", "-m", "seed journey contract")
	trustJourneyCLI(t, vault, "delivery", "import", "--plan", planPath, "--by", "agent:fixture")

	const taskID = "JNY-T-0001"
	task, err := resolveV7Note(vault, taskID, "task")
	if err != nil {
		t.Fatal(err)
	}
	if got := normalizeList(task.Data["owned_paths"]); strings.Join(got, ",") != "owned/journey.txt" || strings.Join(normalizeList(task.Data["spec_refs"]), ",") != ".tusker/specs/journey.md" {
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

	first := trustJourneyPacket(t, trustJourneyCLI(t, vault, "work", "start", taskID, "--by", "agent:implementer", "--source", "codex"))
	if first.Run == nil || first.Run.ActiveAttemptID == "" || first.Workspace == "" {
		t.Fatalf("first interactive session lacks durable identity: %#v", first)
	}
	trustJourneyCLI(t, vault, "work", "fail", taskID, "--by", "agent:implementer", "--reason", "injected provider interruption")

	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	runtimeRun, err := store.FindRun(taskID)
	if err != nil || runtimeRun == nil || runtimeRun.AttemptOutcome != string(AttemptOutcomeFailed) || runtimeRun.LeaseState != string(LeaseStateReleased) {
		_ = store.Close()
		t.Fatalf("interruption did not release a recoverable session: run=%#v err=%v", runtimeRun, err)
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
	store, err = OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	runtimeRun, err = store.FindRun(taskID)
	if err != nil || runtimeRun == nil {
		_ = store.Close()
		t.Fatalf("recovered run unavailable: %#v err=%v", runtimeRun, err)
	}
	runtimeRun.StatusPath = filepath.Join(t.TempDir(), "runner.status.json")
	if ok, updateErr := store.UpdateRunIfLease(*runtimeRun, runtimeRun.LeaseOwner, runtimeRun.LeaseGeneration); updateErr != nil || !ok {
		_ = store.Close()
		t.Fatalf("bind daemon status path: ok=%v err=%v", ok, updateErr)
	}
	daemon := &Daemon{stateRoot: DefaultStateRoot(), store: store, notifyWake: make(chan string, 1)}
	t.Cleanup(daemon.stopNotifyTimers)
	request := daemonWorkerLifecycleRequest{Action: "submit", AttemptID: runtimeRun.ActiveAttemptID, RecordID: runtimeRun.RecordID,
		Lane: runtimeRun.Lane, Workspace: runtimeRun.WorkspacePath, StatusPath: runtimeRun.StatusPath,
		LeaseGeneration: runtimeRun.LeaseGeneration, WorkRevision: runtimeRun.WorkRevision,
		Deliverable: "recovered implementation", Verification: "A1 command will run during review", GateVerdicts: "A1=pass"}
	lifecycleRequest := daemonControlRequest{Command: "worker_lifecycle", Identity: runtimeRun.ActiveAttemptID, ProjectID: runtimeRun.ProjectID, Worker: &request}
	rawRequest, _ := json.Marshal(lifecycleRequest)
	if err := os.WriteFile(workerLifecycleRequestPath(runtimeRun.WorkspacePath), rawRequest, 0o600); err != nil {
		t.Fatal(err)
	}
	statusRaw, _ := json.Marshal(runnerProcessStatus{ExitCode: 0, Outcome: string(AttemptOutcomeSucceeded), CompletedAt: time.Now().UTC().Format(time.RFC3339)})
	if !fileExists(workerLifecycleRequestPath(runtimeRun.WorkspacePath)) {
		_ = store.Close()
		t.Fatal("worker lifecycle request was not persisted in the workspace")
	}
	if err := os.WriteFile(runtimeRun.StatusPath, statusRaw, 0o600); err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	projects, err := store.ListProjects()
	if err != nil || len(projects) != 1 {
		_ = store.Close()
		t.Fatalf("registered journey project unavailable: %#v err=%v", projects, err)
	}
	wf, err := loadWorkflow(vault)
	if err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	updated, changed, err := daemon.reconcileRun(context.Background(), projects[0], wf, *runtimeRun)
	if err != nil || !changed {
		_ = store.Close()
		t.Fatalf("daemon terminal reconciliation: changed=%v err=%v", changed, err)
	}
	daemon.notifyMu.Lock()
	_, reviewWakeScheduled := daemon.notifyTimers[runtimeRun.ProjectID]
	daemon.notifyMu.Unlock()
	if !reviewWakeScheduled {
		t.Fatal("execute completion did not schedule review reconciliation")
	}
	if err := store.UpsertRun(updated); err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	proof, err := loadV7ProofReport(vault, taskID)
	if err != nil || containsString(proof.ModeMissing, "artifact_contract:diff_summary evidence missing for A1") {
		_ = store.Close()
		t.Fatalf("daemon did not capture implementation artifact evidence: %#v err=%v", proof, err)
	}
	_ = store.Close()

	store, err = OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	runtimeRun, err = store.FindRun(taskID)
	if err != nil || runtimeRun == nil {
		_ = store.Close()
		t.Fatalf("submitted run unavailable: %#v err=%v", runtimeRun, err)
	}
	attempts, err := store.ListAttemptsForRun(runtimeRun.ProjectID, runtimeRun.RecordID)
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

	// Import the actual source delta as durable, source-bound artifact evidence.
	// This is not a test verdict; the pending command still runs in review.
	diffPath := filepath.Join(".tusker", "scratch", taskID, "journey.diff")
	if err := writeText(diffPath, gitDirOutput(t, recovered.Workspace, "diff", "HEAD^", "HEAD", "--", "owned/journey.txt")); err != nil {
		t.Fatal(err)
	}
	trustJourneyCLI(t, vault, "evidence", "add", taskID, "--by", "agent:implementer", "--kind", "verification_summary", "--path", diffPath, "--covers", "A1", "--summary", "Source delta for the recovered implementation; command verification remains pending.")

	review := trustJourneyPacket(t, trustJourneyCLI(t, vault, "work", "review", taskID, "--by", "reviewer:agent", "--source", "codex"))
	if review.Action != "review" || review.Run == nil || review.Run.Lane != runLaneReview || review.ImplementationAttempt != succeeded.AttemptID || review.ImplementationActor != "agent:implementer" || review.MaterialFingerprint == "" || review.VerificationManifest == "" || !strings.Contains(review.Next, "--confirm-verification "+review.VerificationManifest) || !strings.Contains(review.Next, "--material-fingerprint "+review.MaterialFingerprint) {
		t.Fatalf("review packet omitted independent immutable provenance: %#v", review)
	}
	submit := []string{"review", "submit", taskID, "--attempt", review.Run.ActiveAttemptID, "--by", "reviewer:agent", "--task-rev", stringField(closedTaskData(t, vault, taskID), "state_rev"), "--source-sha", succeeded.EndState.HeadSHA, "--work-rev", strconv.Itoa(review.Revision), "--proof-fingerprint", review.ProofFingerprint, "--gate-fingerprint", review.GateFingerprint, "--material-fingerprint", review.MaterialFingerprint, "--confirm-verification", review.VerificationManifest, "--verdict", "pass", "--covers", "A1", "--summary", "independent review passed"}
	if scenario == "stale verification confirmation" {
		for i, arg := range submit {
			if arg == "--confirm-verification" {
				submit[i+1] = "sha256:stale"
			}
		}
	}
	if scenario != "source-bound command proof" {
		command, args := parseCLI(append([]string{"tusker"}, submit...))
		args["vault"], args["quiet"] = vault, "true"
		_, refusal := run(command, args)
		want := "implementation workspace material changed"
		if scenario == "stale verification confirmation" {
			want = "verification manifest changed"
			if fileExists(filepath.Join(recovered.Workspace, "verification-count")) {
				t.Fatal("stale confirmation executed a verification command")
			}
		}
		if refusal == nil || !strings.Contains(refusal.Error(), want) {
			t.Fatalf("unsafe review was not refused for %q: %v", want, refusal)
		}
		store, err := OpenRuntimeStore(DefaultStateRoot())
		if err != nil {
			t.Fatal(err)
		}
		defer store.Close()
		results, err := store.ListReviewResults(review.Run.ProjectID)
		if err != nil || len(results) != 0 {
			t.Fatalf("unsafe command persisted review authority: %#v err=%v", results, err)
		}
		current, err := resolveV7Note(vault, taskID, "task")
		if err != nil {
			t.Fatal(err)
		}
		rows := parseV7VerificationRows(current.Body)
		if len(rows) != 1 || rows[0].Result != "pending" || stringField(current.Data, "status") == "done" {
			t.Fatalf("unsafe command certified changed material: %#v", rows)
		}
		return
	}
	trustJourneyCLI(t, vault, submit...)
	if count, err := os.ReadFile(filepath.Join(recovered.Workspace, "verification-count")); err != nil || string(count) != "x" {
		t.Fatalf("review did not execute exactly once in the recorded implementation workspace: count=%q err=%v", count, err)
	}
	if fileExists(filepath.Join(repo, "verification-count")) {
		t.Fatal("review ran verification in the base checkout")
	}

	trustJourneyCLI(t, vault, "land", taskID)
	trustJourneyCLI(t, vault, "close", taskID, "--by", "reviewer:agent", "--reason", "independent review and command proof passed")

	closed, err := resolveV7Note(vault, taskID, "task")
	if err != nil || stringField(closed.Data, "status") != "done" {
		t.Fatalf("reviewed journey did not close: task=%#v err=%v", closed.Data, err)
	}
	if got := gitShowFile(t, repo, "integration/W-0001", "owned/journey.txt"); got != "recovered implementation\n" {
		t.Fatalf("reviewed implementation was not landed to integration: contents=%q", got)
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
