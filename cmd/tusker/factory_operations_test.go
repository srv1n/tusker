package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestFactoryOperationsProjection(t *testing.T) {
	t.Run("state matrix and precedence", func(t *testing.T) {
		tests := []struct {
			name       string
			mutate     func(*factoryOperationsFacts)
			section    string
			wantState  string
			wantAction string
		}{
			{
				name: "disabled idle",
				mutate: func(f *factoryOperationsFacts) {
					f.Workflow.AutomationEnabled = false
					f.Workflow.DispatchScope = automationDispatchScopeProjection{Configured: "all_eligible", Effective: "all_eligible", Provenance: configSourceProject}
				},
				section: "next", wantState: "idle", wantAction: "tusker config resolve automation.enabled --json",
			},
			{
				name: "disarmed",
				mutate: func(f *factoryOperationsFacts) {
					f.WaveFacts["W-0001"] = factoryOperationsWaveFact{State: "disarmed", IntegrationRef: "integration/W-0001"}
				},
				section: "blocked", wantState: "disarmed", wantAction: "tusker wave preflight W-0001 --json",
			},
			{
				name: "stale authorization",
				mutate: func(f *factoryOperationsFacts) {
					f.WaveFacts["W-0001"] = factoryOperationsWaveFact{State: "stale", Stale: true, CurrentFingerprint: "sha256:new", AuthorizedFingerprint: "sha256:old", IntegrationRef: "integration/W-0001"}
				},
				section: "blocked", wantState: "stale_authorization", wantAction: "tusker wave preflight W-0001 --json",
			},
			{
				name: "legacy broad scope preserves named wave authorization",
				mutate: func(f *factoryOperationsFacts) {
					f.Workflow.DispatchScope = automationDispatchScopeProjection{
						Effective: "all_eligible", Provenance: "legacy enabled config without dispatch_scope",
						Warning: legacyDispatchScopeWarning, Repair: legacyDispatchScopeRepair,
					}
					f.WaveFacts["W-0001"] = factoryOperationsWaveFact{State: "stale", Stale: true, IntegrationRef: "integration/W-0001"}
				},
				section: "blocked", wantState: "stale_authorization", wantAction: "tusker wave preflight W-0001 --json",
			},
			{
				name: "live run wins over stale authorization",
				mutate: func(f *factoryOperationsFacts) {
					run := RunStatus{
						ProjectID: "app", RecordID: "APP-T-0001", ItemID: "APP-T-0001", Lane: runLaneExecute,
						LeaseState: string(LeaseStateRunning), LeaseExpiresAt: f.Now.Add(time.Minute).Format(time.RFC3339), WorkRevision: 3,
					}
					f.Runs["APP-T-0001"], f.AllRuns = run, []RunStatus{run}
					f.WaveFacts["W-0001"] = factoryOperationsWaveFact{State: "stale", Stale: true, IntegrationRef: "integration/W-0001"}
				},
				section: "working", wantState: "running", wantAction: "tusker runs inspect APP-T-0001 --json",
			},
			{
				name: "expired run is stale not live",
				mutate: func(f *factoryOperationsFacts) {
					run := RunStatus{
						ProjectID: "app", RecordID: "APP-T-0001", ItemID: "APP-T-0001", Lane: runLaneExecute,
						LeaseState: string(LeaseStateRunning), LeaseExpiresAt: f.Now.Add(-time.Minute).Format(time.RFC3339), WorkRevision: 3,
					}
					f.Runs["APP-T-0001"], f.AllRuns = run, []RunStatus{run}
				},
				section: "blocked", wantState: "stale_run", wantAction: "tusker runs inspect APP-T-0001 --json",
			},
			{
				name: "durable retry continuation wins over later authorization drift",
				mutate: func(f *factoryOperationsFacts) {
					run := RunStatus{ProjectID: "app", RecordID: "APP-T-0001", ItemID: "APP-T-0001", Lane: runLaneExecute, LeaseState: string(LeaseStateRetryQueued), WorkRevision: 3, LastError: "transient provider failure"}
					f.Runs["APP-T-0001"], f.AllRuns = run, []RunStatus{run}
					f.WaveFacts["W-0001"] = factoryOperationsWaveFact{State: "stale", Stale: true, IntegrationRef: "integration/W-0001"}
				},
				section: "next", wantState: "retry_queued", wantAction: "tusker runs inspect APP-T-0001 --json",
			},
			{
				name: "objective review",
				mutate: func(f *factoryOperationsFacts) {
					f.Index.Tasks["APP-T-0001"] = factoryOperationsTestTask("APP-T-0001", "review", "ready", "W-0001")
				},
				section: "review", wantState: "in_review", wantAction: "tusker show APP-T-0001 --capsule",
			},
			{
				name: "machine rework",
				mutate: func(f *factoryOperationsFacts) {
					task := factoryOperationsTestTask("APP-T-0001", "rework", "ready", "W-0001")
					task.Data["next_action"] = "Repair the failing acceptance check."
					f.Index.Tasks["APP-T-0001"] = task
				},
				section: "review", wantState: "rework", wantAction: "tusker show APP-T-0001 --capsule",
			},
			{
				name: "completion repair",
				mutate: func(f *factoryOperationsFacts) {
					f.Index.Tasks["APP-T-0001"] = factoryOperationsTestTask("APP-T-0001", "review", "ready", "W-0001")
					f.Completions["APP-T-0001"] = factoryOperationsCompletionFact{
						Result: ReviewResult{TaskID: "APP-T-0001", WorkRevision: 2},
						Repair: "completion transaction identity needs machine repair",
					}
				},
				section: "blocked", wantState: "completion_repair", wantAction: "tusker show APP-T-0001 --capsule",
			},
			{
				name: "parked",
				mutate: func(f *factoryOperationsFacts) {
					run := RunStatus{ProjectID: "app", RecordID: "APP-T-0001", ItemID: "APP-T-0001", LeaseState: string(LeaseStateParkedNoProgress), LastError: "attempt policy exhausted"}
					f.Runs["APP-T-0001"], f.AllRuns = run, []RunStatus{run}
				},
				section: "blocked", wantState: "parked", wantAction: "tusker runs inspect APP-T-0001 --json",
			},
			{
				name: "integrated",
				mutate: func(f *factoryOperationsFacts) {
					f.Index.Tasks["APP-T-0001"] = factoryOperationsTestTask("APP-T-0001", "done", "done", "W-0001")
					f.Completions["APP-T-0001"] = factoryOperationsCompletionFact{
						Result:      ReviewResult{TaskID: "APP-T-0001", WorkRevision: 2, ImplementationSHA: "impl123", ResultRevision: "result123", Verdict: "pass"},
						Transaction: &completionTransaction{TaskID: "APP-T-0001", Phase: completionPhaseTerminal, IntegrationRef: "integration/W-0001", StagedSHA: "integrated123"},
					}
				},
				section: "delivered", wantState: "integrated", wantAction: "tusker show APP-T-0001 --capsule",
			},
			{
				name: "promoted",
				mutate: func(f *factoryOperationsFacts) {
					f.Index.Tasks["APP-T-0001"] = factoryOperationsTestTask("APP-T-0001", "done", "done", "W-0001")
					f.Departures = []DepartureRun{{
						ID: "departure-1", ProjectID: "app", State: DepartureStatePassed,
						Candidate: DepartureCandidate{CargoTaskIDs: []string{"APP-T-0001"}, CandidateSHA: "integrated123"},
						Promotion: DeparturePromotion{CommittedRef: "main", CommittedSHA: "promoted123"},
					}}
				},
				section: "delivered", wantState: "promoted", wantAction: "tusker show APP-T-0001 --capsule",
			},
			{
				name: "capacity wait",
				mutate: func(f *factoryOperationsFacts) {
					f.Workflow.DispatchScope = automationDispatchScopeProjection{Configured: "all_eligible", Effective: "all_eligible", Provenance: configSourceProject}
					f.GlobalCapacityLimit = 1
					f.AllRuns = []RunStatus{{
						ProjectID: "other", RecordID: "OTHER-T-1", LeaseState: string(LeaseStateRunning),
						LeaseExpiresAt: f.Now.Add(time.Minute).Format(time.RFC3339),
					}}
				},
				section: "next", wantState: "waiting_capacity", wantAction: "tusker runs inspect APP-T-0001 --json",
			},
			{
				name: "resource wait",
				mutate: func(f *factoryOperationsFacts) {
					f.Workflow.DispatchScope = automationDispatchScopeProjection{Configured: "all_eligible", Effective: "all_eligible", Provenance: configSourceProject}
					task := f.Index.Tasks["APP-T-0001"]
					task.Data["resource_refs"] = []string{"gpu-a"}
					f.Index.Tasks["APP-T-0001"] = task
					f.ResourceLeases = []ResourceLease{{
						Name: "gpu-a", Purpose: "scheduled full promotion gate", ProjectID: "other", State: resourceLeaseHeld,
						ExpiresAt: f.Now.Add(time.Minute).Format(time.RFC3339),
					}}
				},
				section: "blocked", wantState: "waiting_resource", wantAction: "tusker runs inspect APP-T-0001 --json",
			},
			{
				name: "ready frontier",
				mutate: func(f *factoryOperationsFacts) {
					f.Workflow.DispatchScope = automationDispatchScopeProjection{Configured: "all_eligible", Effective: "all_eligible", Provenance: configSourceProject}
				},
				section: "next", wantState: "ready", wantAction: "tusker automation explain APP-T-0001 --json",
			},
		}
		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				facts := factoryOperationsTestFacts()
				tc.mutate(&facts)
				projection := composeFactoryOperations(facts)
				item := factoryOperationsFindTask(t, projection, tc.section, "APP-T-0001")
				if item.State != tc.wantState {
					t.Fatalf("state = %q, want %q; projection=%#v", item.State, tc.wantState, projection)
				}
				if item.SafeAction != tc.wantAction {
					t.Fatalf("safe action = %q, want exact %q", item.SafeAction, tc.wantAction)
				}
			})
		}
	})

	t.Run("freshness controls capacity and resource blockers", func(t *testing.T) {
		facts := factoryOperationsTestFacts()
		facts.Workflow.DispatchScope = automationDispatchScopeProjection{Configured: "all_eligible", Effective: "all_eligible", Provenance: configSourceProject}
		facts.GlobalCapacityLimit = 1
		facts.AllRuns = []RunStatus{{
			ProjectID: "other", RecordID: "OTHER-T-1", LeaseState: string(LeaseStateClaimed),
			LeaseExpiresAt: facts.Now.Add(-time.Second).Format(time.RFC3339),
		}}
		task := facts.Index.Tasks["APP-T-0001"]
		task.Data["resource_refs"] = []string{"gpu-a"}
		facts.Index.Tasks["APP-T-0001"] = task
		facts.ResourceLeases = []ResourceLease{{
			Name: "gpu-a", Purpose: "old holder", ProjectID: "other", State: resourceLeaseHeld,
			ExpiresAt: facts.Now.Add(-time.Second).Format(time.RFC3339),
		}}
		projection := composeFactoryOperations(facts)
		if projection.Capacity.Global.Active != 0 || projection.Capacity.Global.Available != 1 {
			t.Fatalf("expired run consumed capacity: %#v", projection.Capacity.Global)
		}
		if len(projection.Capacity.ResourceHolds) != 0 {
			t.Fatalf("expired resource projected held: %#v", projection.Capacity.ResourceHolds)
		}
		if item := factoryOperationsFindTask(t, projection, "next", "APP-T-0001"); item.State != "ready" {
			t.Fatalf("expired capacity/resource blocked frontier: %#v", item)
		}
	})

	t.Run("departure outcomes never infer promotion", func(t *testing.T) {
		tests := []struct {
			name       string
			mode       string
			promotion  DeparturePromotion
			wantState  string
			wantPhrase string
			wantRef    string
			wantSHA    string
		}{
			{name: "shadow validation", mode: scheduledPromotionShadow, wantState: "shadow_validated", wantPhrase: "no integration or default ref was changed"},
			{name: "staged only", mode: scheduledPromotionStage, wantState: "staged_only", wantPhrase: "default ref was not promoted"},
			{
				name: "actual promotion", mode: scheduledPromotionPromote,
				promotion: DeparturePromotion{CommittedRef: "refs/heads/main", CommittedSHA: "promoted123"},
				wantState: "promotion_committed", wantPhrase: "were promoted to refs/heads/main at promoted123",
				wantRef: "refs/heads/main", wantSHA: "promoted123",
			},
		}
		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				facts := factoryOperationsTestFacts()
				departure := DepartureRun{
					ID: "departure-1", ProjectID: "app", State: DepartureStatePassed,
					PolicyID:  departurePolicyID(ScheduledPromotionProjection{Mode: tc.mode}),
					Candidate: DepartureCandidate{CargoTaskIDs: []string{"APP-T-0001"}, CandidateSHA: "candidate123"},
					Promotion: tc.promotion,
				}
				item, section := factoryOperationsDepartureItem(facts, departure)
				if section != "delivered" || item.State != tc.wantState || !strings.Contains(item.ProductOutcome, tc.wantPhrase) {
					t.Fatalf("departure projection = section %q item %#v", section, item)
				}
				if item.Revisions.DefaultRef != tc.wantRef || item.Revisions.DefaultSHA != tc.wantSHA {
					t.Fatalf("departure invented default revision: %#v", item.Revisions)
				}
				if tc.promotion.CommittedSHA == "" && strings.Contains(item.State, "promoted") {
					t.Fatalf("non-promotion was labeled promoted: %#v", item)
				}
			})
		}
	})

	t.Run("bounded outcome artifacts revisions and forbidden exhaust", func(t *testing.T) {
		facts := factoryOperationsTestFacts()
		facts.Workflow.DispatchScope = automationDispatchScopeProjection{Configured: "all_eligible", Effective: "all_eligible", Provenance: configSourceProject}
		task := facts.Index.Tasks["APP-T-0001"]
		task.Data["state_rev"] = "sha256:state"
		task.Data["work_revision"] = 4
		facts.Index.Tasks["APP-T-0001"] = task
		facts.Index.Evidence["APP-T-0001"] = []Note{{
			Data: map[string]any{
				"id": "APP-T-0001-E-0001", "kind": "evidence", "task": "APP-T-0001",
				"status": "accepted", "evidence_kind": "verification_summary", "covers": []string{"A1"},
			},
			Body: "## Summary\n\ntoken=supersecret The stable JSON response matches the UI fixture.",
		}}
		facts.Completions["APP-T-0001"] = factoryOperationsCompletionFact{
			Result:      ReviewResult{TaskID: "APP-T-0001", WorkRevision: 4, ImplementationSHA: "impl456", ResultRevision: "review456"},
			Transaction: &completionTransaction{TaskID: "APP-T-0001", Phase: completionPhaseRefCommitted, IntegrationRef: "integration/W-0001", StagedSHA: "land456"},
		}
		waveFact := facts.WaveFacts["W-0001"]
		waveFact.IntegrationSHA = ""
		facts.WaveFacts["W-0001"] = waveFact
		projection := composeFactoryOperations(facts)
		item := factoryOperationsFindTask(t, projection, "next", "APP-T-0001")
		if !strings.Contains(item.ProductOutcome, "operator sees the shipped result") {
			t.Fatalf("product outcome did not come from acceptance: %q", item.ProductOutcome)
		}
		if len(item.AcceptedArtifacts) != 1 || item.AcceptedArtifacts[0].EvidenceRef != "APP-T-0001-E-0001" {
			t.Fatalf("accepted artifact projection = %#v", item.AcceptedArtifacts)
		}
		if strings.Contains(item.AcceptedArtifacts[0].Summary, "supersecret") || !strings.Contains(item.AcceptedArtifacts[0].Summary, "[redacted]") {
			t.Fatalf("accepted artifact summary was not bounded/redacted: %q", item.AcceptedArtifacts[0].Summary)
		}
		if item.Revisions.StateRevision != "sha256:state" || item.Revisions.WorkRevision != 4 ||
			item.Revisions.ImplementationSHA != "impl456" || item.Revisions.ResultRevision != "review456" ||
			item.Revisions.IntegrationSHA != "land456" || item.Revisions.DefaultRef != "" || item.Revisions.DefaultSHA != "" {
			t.Fatalf("revision projection = %#v", item.Revisions)
		}
		raw, err := json.Marshal(projection)
		if err != nil {
			t.Fatal(err)
		}
		for _, forbidden := range []string{
			"rawLogPath", "promptPath", "eventSinkPath", "sessionRef", "lastHeartbeatAt",
			"logsSummary", "finalSummary", "tokenTotal", "transcript", "frontmatter",
		} {
			if strings.Contains(string(raw), forbidden) {
				t.Fatalf("projection leaked forbidden exhaust field %q: %s", forbidden, raw)
			}
		}
		if projection.Project.DispatchScope.Configured != "all_eligible" ||
			projection.Project.DispatchScope.Effective != "all_eligible" ||
			projection.Project.DispatchScope.Provenance != configSourceProject {
			t.Fatalf("dispatch provenance = %#v", projection.Project.DispatchScope)
		}
		if projection.Project.PromotionMode.Mode != scheduledPromotionShadow ||
			!projection.Project.PromotionMode.Observe || projection.Project.PromotionMode.Promote {
			t.Fatalf("promotion mode = %#v", projection.Project.PromotionMode)
		}
		rendered := renderFactoryOperations(projection)
		for _, expected := range []string{
			"Registry: registered=true enabled=true health=healthy",
			"Capacity: project",
			"Fingerprints: current=sha256:current authorized=sha256:current",
			"Artifact: diff_summary · APP-T-0001-E-0001",
			"Revisions: state=sha256:state",
			"Safe action: tusker automation explain APP-T-0001 --json",
		} {
			if !strings.Contains(rendered, expected) {
				t.Fatalf("plain CLI projection missing %q:\n%s", expected, rendered)
			}
		}
	})

	t.Run("plain output renders compatibility warnings and repairs", func(t *testing.T) {
		facts := factoryOperationsTestFacts()
		facts.Workflow.DispatchScope = automationDispatchScopeProjection{
			Effective: "all_eligible", Provenance: "legacy enabled config without dispatch_scope",
			Warning: legacyDispatchScopeWarning, Repair: legacyDispatchScopeRepair,
		}
		facts.Workflow.CompletionReactor = completionReactorModeProjection{
			Effective: "legacy", Provenance: "legacy enabled config without completion_reactor.mode",
			Warning: legacyCompletionReactorModeWarning, Repair: legacyCompletionReactorModeRepair,
		}
		rendered := renderFactoryOperations(composeFactoryOperations(facts))
		for _, expected := range []string{
			"Dispatch scope warning: " + legacyDispatchScopeWarning,
			"Dispatch scope repair: " + legacyDispatchScopeRepair,
			"Completion reactor warning: " + legacyCompletionReactorModeWarning,
			"Completion reactor repair: " + legacyCompletionReactorModeRepair,
		} {
			if !strings.Contains(rendered, expected) {
				t.Fatalf("plain output omitted %q:\n%s", expected, rendered)
			}
		}
	})

	t.Run("registry enablement stays separate from workflow automation", func(t *testing.T) {
		facts := factoryOperationsTestFacts()
		facts.ProjectRegistered = false
		facts.Project.Enabled = true
		facts.Project.Health = projectHealthHealthy
		facts.Workflow.AutomationEnabled = true
		projection := composeFactoryOperations(facts)
		if projection.Project.Registered || projection.Project.Enabled || projection.Project.Health != string(projectHealthDisabled) {
			t.Fatalf("unregistered project projected false registry authority: %#v", projection.Project)
		}
		if !projection.Project.AutomationEnabled {
			t.Fatal("workflow automation fact was incorrectly collapsed into registry enablement")
		}
	})

	t.Run("only genuine human gates become decisions", func(t *testing.T) {
		facts := factoryOperationsTestFacts()
		facts.Workflow.DispatchScope = automationDispatchScopeProjection{Configured: "all_eligible", Effective: "all_eligible", Provenance: configSourceProject}
		facts.Index.Tasks = map[string]Note{
			"APP-T-0001": factoryOperationsTestTask("APP-T-0001", "ready", "ready", ""),
			"APP-T-0002": factoryOperationsTestTask("APP-T-0002", "ready", "ready", ""),
			"APP-T-0003": factoryOperationsTestTask("APP-T-0003", "ready", "ready", ""),
			"APP-T-0004": factoryOperationsTestTask("APP-T-0004", "ready", "blocked_by_dependency", ""),
		}
		dependent := facts.Index.Tasks["APP-T-0004"]
		dependent.Data["dependencies"] = []string{"APP-T-0001"}
		facts.Index.Tasks["APP-T-0004"] = dependent
		facts.Index.Gates = map[string]Note{
			"APP-G-0001": factoryOperationsTestGate("APP-G-0001", "APP-T-0001", "human:sarav", "decision",
				"Choose the customer-visible retention period.", "Decision record names the selected period.", "The requirements are ambiguous; only the accountable product owner can resolve the conflict."),
			"APP-G-0002": factoryOperationsTestGate("APP-G-0002", "APP-T-0002", "human:sarav", "signoff",
				"Review the implementation diff.", "Confirm the implementation is correct.", "A human was requested."),
			"APP-G-0003": factoryOperationsTestGate("APP-G-0003", "APP-T-0003", "agent:reviewer", "verification",
				"Run the deterministic contract check.", "go test ./cmd/tusker -run TestContract", ""),
		}
		projection := composeFactoryOperations(facts)
		if len(projection.NeedsYourDecision) != 1 || projection.NeedsYourDecision[0].GateID != "APP-G-0001" {
			t.Fatalf("human decisions = %#v", projection.NeedsYourDecision)
		}
		if !reflect.DeepEqual(projection.NeedsYourDecision[0].AffectedTaskIDs, []string{"APP-T-0001", "APP-T-0004"}) {
			t.Fatalf("decision closure = %#v", projection.NeedsYourDecision[0].AffectedTaskIDs)
		}
		if factoryOperationsFindTask(t, projection, "blocked", "APP-T-0002").State != "blocked" {
			t.Fatal("invalid human gate did not return to machine-blocked work")
		}
		if factoryOperationsFindTask(t, projection, "blocked", "APP-T-0003").State != "blocked" {
			t.Fatal("machine gate entered the decision queue")
		}
	})

	t.Run("CLI and Serve share one read-only wire projection", func(t *testing.T) {
		server := newServeFixture(t)
		beforeTree := factoryOperationsTreeDigest(t, server.vaultPath)
		beforeRuns, err := server.store.ListRuns()
		if err != nil {
			t.Fatal(err)
		}
		oldClock := factoryOperationsNow
		factoryOperationsNow = server.now
		t.Cleanup(func() { factoryOperationsNow = oldClock })

		var apiProjection factoryOperationsProjection
		serveDecode(t, server, "/api/factory-operations?project=app", &apiProjection)
		if apiProjection.Schema != factoryOperationsSchema || !apiProjection.ReadOnly {
			t.Fatalf("Serve projection = %#v", apiProjection)
		}
		if !reflect.DeepEqual(apiProjection.SectionOrder, factoryOperationsSectionOrder) {
			t.Fatalf("section order = %#v", apiProjection.SectionOrder)
		}
		previousWD, err := os.Getwd()
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Chdir(filepath.Dir(server.vaultPath)); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chdir(previousWD) })
		command, parsed := parseCLI([]string{"tusker", "factory", "operations", "--json"})
		if command != "factory operations" || !parsed.Bool("json") {
			t.Fatalf("parseCLI = %q %#v", command, parsed)
		}
		var commandErr error
		output := captureStdout(t, func() {
			code, err := run(command, parsed)
			commandErr = err
			if code != 0 {
				t.Fatalf("factory operations exit code = %d", code)
			}
		})
		if commandErr != nil {
			t.Fatal(commandErr)
		}
		var cliProjection factoryOperationsProjection
		if err := json.Unmarshal([]byte(output), &cliProjection); err != nil {
			t.Fatalf("decode CLI projection: %v\n%s", err, output)
		}
		if !reflect.DeepEqual(cliProjection, apiProjection) {
			t.Fatalf("CLI and Serve projections diverged\nCLI: %#v\nAPI: %#v", cliProjection, apiProjection)
		}
		if cliCommandMutatesVault("factory operations") {
			t.Fatal("factory operations was accidentally classified as a mutating CLI command")
		}
		helpCommand, helpArgs := parseCLI([]string{"tusker", "help", "factory", "operations"})
		helpOutput := captureStdout(t, func() {
			code, helpErr := run(helpCommand, helpArgs)
			if code != 0 || helpErr != nil {
				t.Fatalf("factory help failed: code=%d err=%v", code, helpErr)
			}
		})
		if !strings.Contains(helpOutput, "read-only") || !strings.Contains(helpOutput, "factory operations") {
			t.Fatalf("factory help missing public contract:\n%s", helpOutput)
		}
		bareCommand, bareArgs := parseCLI([]string{"tusker", "factory"})
		bareOutput := captureStdout(t, func() {
			code, bareErr := run(bareCommand, bareArgs)
			if code != 0 || bareErr != nil {
				t.Fatalf("bare factory command failed: code=%d err=%v", code, bareErr)
			}
		})
		if bareCommand != "factory" || !strings.Contains(bareOutput, "tusker factory operations") {
			t.Fatalf("bare factory command did not route to help: command=%q\n%s", bareCommand, bareOutput)
		}
		for _, flag := range []string{"write", "refresh", "dispatch"} {
			code, mutationErr := run("factory operations", Args{flag: "true"})
			if code != 0 || mutationErr == nil || !strings.Contains(mutationErr.Error(), "read-only") {
				t.Fatalf("--%s refusal: code=%d err=%v", flag, code, mutationErr)
			}
		}
		for _, argv := range [][]string{
			{"tusker", "factory", "operations", "--wat"},
			{"tusker", "factory", "operations", "extra"},
			{"tusker", "factory", "operations", "--json", "false"},
			{"tusker", "factory", "operations", "--vault", server.vaultPath},
		} {
			invalidCommand, invalidArgs := parseCLI(argv)
			code, invalidErr := run(invalidCommand, invalidArgs)
			if code != 0 || invalidErr == nil || errorToIssue(invalidErr).Code != errorInvalidArg {
				t.Fatalf("%v was not refused exactly: code=%d args=%#v err=%v", argv, code, invalidArgs, invalidErr)
			}
		}
		post := httptest.NewRequest(http.MethodPost, "/api/factory-operations?project=app", nil)
		post.Host = "127.0.0.1:7420"
		rec := httptest.NewRecorder()
		server.ServeHTTP(rec, post)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("POST factory operations = %d, want 405: %s", rec.Code, rec.Body.String())
		}
		afterRuns, err := server.store.ListRuns()
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(beforeRuns, afterRuns) {
			t.Fatalf("read projection mutated runtime rows\nbefore=%#v\nafter=%#v", beforeRuns, afterRuns)
		}
		if afterTree := factoryOperationsTreeDigest(t, server.vaultPath); afterTree != beforeTree {
			t.Fatalf("read projection mutated canonical vault: before=%s after=%s", beforeTree, afterTree)
		}
	})
}

func factoryOperationsTestFacts() factoryOperationsFacts {
	task := factoryOperationsTestTask("APP-T-0001", "ready", "ready", "W-0001")
	wave := Note{Data: map[string]any{
		"kind": "wave", "id": "W-0001", "project": "app", "title": "Factory wave",
		"authorization": "armed", "members": []string{"APP-T-0001"}, "integration_branch": "integration/W-0001",
	}}
	var workflow Workflow
	workflow.AutomationEnabled = true
	workflow.DispatchScope = automationDispatchScopeProjection{Configured: "armed_waves", Effective: "armed_waves", Provenance: configSourceProject}
	workflow.CompletionReactor = completionReactorModeProjection{Configured: "authoritative", Effective: "authoritative", Provenance: configSourceProject}
	workflow.Runtime.MaxActiveRunsPerProject = 2
	workflow.ScheduledPromotion.Effective = ScheduledPromotionProjection{
		Configured: true, Mode: scheduledPromotionShadow, Provenance: configSourceProject, Observe: true,
	}
	return factoryOperationsFacts{
		VaultPath: "/repo/.tusker", RepoRoot: "/repo",
		Project:           RegisteredProject{ProjectID: "app", Name: "App", Enabled: true, Health: projectHealthHealthy},
		ProjectRegistered: true, Workflow: workflow, AutomationSource: configSourceProject,
		Index: v7Index{
			Tasks: map[string]Note{"APP-T-0001": task}, Gates: map[string]Note{}, Waves: map[string]Note{"W-0001": wave},
			Evidence: map[string][]Note{}, Attempts: map[string][]Note{},
		},
		Runs: map[string]RunStatus{}, AllRuns: []RunStatus{}, Completions: map[string]factoryOperationsCompletionFact{},
		Departures: []DepartureRun{}, ResourceLeases: []ResourceLease{}, GlobalCapacityLimit: 2,
		DefaultRef: "main", DefaultSHA: "default123",
		WaveFacts: map[string]factoryOperationsWaveFact{
			"W-0001": {State: "armed", CurrentFingerprint: "sha256:current", AuthorizedFingerprint: "sha256:current", IntegrationRef: "integration/W-0001", IntegrationSHA: "integration123"},
		},
		Now: time.Date(2026, 7, 26, 8, 0, 0, 0, time.UTC),
	}
}

func factoryOperationsTestTask(id, status, readiness, wave string) Note {
	return Note{
		Data: map[string]any{
			"kind": "task", "id": id, "project": "app", "title": "Visible factory outcome",
			"status": status, "readiness": readiness, "next_owner": "agent", "wave": wave,
			"state_rev": "sha256:" + strings.ToLower(id), "work_revision": 1,
		},
		Body: "## Acceptance\n\n| ID | Outcome | Proof |\n|---|---|---|\n| A1 | The operator sees the shipped result without orchestration exhaust. | focused test |\n",
	}
}

func factoryOperationsTestGate(id, taskID, owner, kind, action, verification, why string) Note {
	data := map[string]any{
		"kind": "gate", "id": id, "project": "app", "status": "open", "blocking": true,
		"blocks": []string{taskID}, "owner": owner, "gate_kind": kind, "action": action,
		"verification": verification, "why_agent_cannot": why,
	}
	if kind == "decision" {
		data["suggestion"] = "Choose the shortest retention period compatible with the product requirement."
	}
	return Note{Data: data}
}

func factoryOperationsFindTask(t *testing.T, projection factoryOperationsProjection, section, taskID string) factoryOperationsItem {
	t.Helper()
	var items []factoryOperationsItem
	switch section {
	case "delivered":
		items = projection.Delivered
	case "working":
		items = projection.WorkingNow
	case "review":
		items = projection.ReviewOrRework
	case "blocked":
		items = projection.Blocked
	case "next":
		items = projection.NextFrontier
	default:
		t.Fatalf("unknown test section %q", section)
	}
	for _, item := range items {
		if item.TaskID == taskID {
			return item
		}
	}
	t.Fatalf("task %s missing from %s: %#v", taskID, section, items)
	return factoryOperationsItem{}
}

func factoryOperationsTreeDigest(t *testing.T, root string) string {
	t.Helper()
	paths := []string{}
	if err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			paths = append(paths, path)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	sort.Strings(paths)
	hash := sha256.New()
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = hash.Write([]byte(relative))
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write(raw)
		_, _ = hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil))
}
