package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func trustJourneyCLIRefusal(t *testing.T, vault string, argv ...string) error {
	t.Helper()
	full := append([]string{"tusker"}, argv...)
	var runErr error
	captureStdout(t, func() {
		command, args := parseCLI(full)
		args["vault"] = vault
		args["quiet"] = "true"
		_, runErr = run(command, args)
	})
	if runErr == nil {
		t.Fatalf("%s unexpectedly succeeded", strings.Join(full, " "))
	}
	return runErr
}

func TestTrustFreshAgent(t *testing.T) {
	t.Setenv("TUSKER_STATE_ROOT", filepath.Join(t.TempDir(), "state"))
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Clean(filepath.Join(wd, "..", "..", "e2e", "agent_journey"))
	planPath := filepath.Join(fixture, "fixture", "delivery.yaml")
	planBytes, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatal(err)
	}
	var fixturePlan deliveryPlanV2
	if err := yaml.Unmarshal(planBytes, &fixturePlan); err != nil {
		t.Fatal(err)
	}
	if fixturePlan.Schema != deliveryPlanV2Schema || len(fixturePlan.Tasks) != 2 || strings.Join(fixturePlan.SpecRefs, ",") != ".tusker/specs/fresh-agent.md" || strings.Join(fixturePlan.Tasks[0].OwnedPaths, ",") != "owned/greeting.txt" || strings.Join(fixturePlan.Tasks[1].OwnedPaths, ",") != "owned/sibling.txt" {
		t.Fatalf("fresh fixture does not declare two isolated V2 task contracts: %#v", fixturePlan)
	}
	for _, promptName := range []string{"IMPLEMENTER_PROMPT.md", "REVIEWER_PROMPT.md"} {
		prompt, readErr := os.ReadFile(filepath.Join(fixture, promptName))
		if readErr != nil {
			t.Fatal(readErr)
		}
		text := string(prompt)
		if !strings.Contains(text, "tusker work") || !strings.Contains(text, "daemon") || !strings.Contains(text, "dispatch") {
			t.Fatalf("%s does not state the shipped-command and resident-process boundary", promptName)
		}
	}

	repo := t.TempDir()
	initializeOrchestrationGitRepo(t, repo)
	if err := os.MkdirAll(filepath.Join(repo, "docs", "specs"), 0o755); err != nil {
		t.Fatal(err)
	}
	specBytes, err := os.ReadFile(filepath.Join(fixture, "fixture", "docs", "specs", "fresh-agent.md"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "docs", "specs", "fresh-agent.md"), specBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	previousWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previousWD) })
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}
	vault := filepath.Join(repo, ".tusker")
	trustJourneyCLI(t, vault, "init", "--vault", vault, "--yes", "--vault-only", "--no-mount")
	plan := validDeliveryPlanV2()
	plan.HumanGates = nil
	canonicalSpec := filepath.Join(vault, "specs", "fresh-agent.md")
	if err := os.MkdirAll(filepath.Dir(canonicalSpec), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(canonicalSpec, specBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	plan.Scope, plan.Title, plan.SpecRefs = "fresh-agent-test", "Fresh agent test", []string{".tusker/specs/fresh-agent.md"}
	plan.Concurrency = 1
	plan.Epic, plan.EpicContract = "", &deliveryEpicContract{SourceKey: "fresh-agent-test", AcronymHint: "FSH", Title: "Fresh agent test"}
	plan.Requirements = []deliveryRequirement{{ID: "R1", Outcome: "Greeting material stays isolated."}, {ID: "R2", Outcome: "Sibling material stays isolated."}}
	plan.Tasks = []deliveryPlanTask{
		{SourceKey: "greeting", RequirementRefs: []string{"R1"}, Title: "Implement greeting", Outcome: "Write only the greeting material.", Acceptance: []deliveryAcceptance{{ID: "A1", Outcome: "Greeting is exact."}}, Verification: []deliveryVerification{{Covers: "A1", Check: "command: true"}}, Artifact: deliveryArtifactContract{Kind: "diff_summary", Path: "owned/greeting.txt", Summary: "Greeting artifact.", AcceptanceIDs: []string{"A1"}}, OwnedPaths: []string{"owned/greeting.txt"}, Priority: "p1", Risk: "low"},
		{SourceKey: "sibling", RequirementRefs: []string{"R2"}, Title: "Preserve sibling", Outcome: "Keep sibling material independent.", Acceptance: []deliveryAcceptance{{ID: "A1", Outcome: "Sibling remains independent."}}, Verification: []deliveryVerification{{Covers: "A1", Check: "command: true"}}, Artifact: deliveryArtifactContract{Kind: "diff_summary", Path: "owned/sibling.txt", Summary: "Sibling artifact.", AcceptanceIDs: []string{"A1"}}, OwnedPaths: []string{"owned/sibling.txt"}, Priority: "p1", Risk: "low"},
	}
	plan.ContextFingerprint = deliveryPlanV2ContextFingerprint(t, vault, plan)
	rawPlan, err := yaml.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "delivery.yaml"), rawPlan, 0o644); err != nil {
		t.Fatal(err)
	}
	trustJourneyCLI(t, vault, "delivery", "import", "--plan", "delivery.yaml", "--by", "agent:fixture")
	trustJourneyCLI(t, vault, "projects", "add", "--repo", repo, "--vault", vault)

	const taskID = "FSH-T-0001"
	refusal := trustJourneyCLIRefusal(t, vault, "work", "start", taskID, "--by", "agent:fresh-muse", "--source", "codex")
	if workSessionErrorCode(refusal) != "WORK_SESSION_NOT_READY" {
		t.Fatalf("held imported fixture returned the wrong start refusal: %v", refusal)
	}
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if run, findErr := store.FindRun(taskID); findErr != nil || run != nil {
		t.Fatalf("held fresh fixture created an unauthorized run: run=%#v err=%v", run, findErr)
	}
}
