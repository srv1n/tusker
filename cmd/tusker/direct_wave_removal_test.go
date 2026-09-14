package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

var directWaveRemovalScanRoots = []string{
	"cmd",
	"internal",
	"skills",
	"docs/system",
	"docs/templates",
	"e2e",
	"scripts",
}

var directWaveRemovalAllowlist = map[string]bool{
	"cmd/tusker/direct_wave_removal_test.go":               true,
	"cmd/tusker/direct_authoring_test.go":                  true,
	"cmd/tusker/direct_wave_authority_test.go":             true,
	// Packet guidance tests assert the removed vocabulary is absent, so they
	// necessarily name the forbidden tokens.
	"cmd/tusker/direct_wave_packet_test.go":                 true,
	"internal/serve/ui/test/direct-wave-authority.test.ts": true,
	"internal/serve/ui/test/pilot-start-readiness.test.ts": true,
	"internal/serve/ui/test/wux-integration.test.ts":       true,
	"internal/serve/ui/test/task-authoring.browser.mjs":    true,
}

var directWaveRemovalForbidden = []*regexp.Regexp{
	regexp.MustCompile(`tusker\.delivery-plan`),
	regexp.MustCompile(`\bdeliveryPlan(V2)?\b|\boperationalDeliveryPlan|\bvalidDeliveryPlan|\bwriteDelivery(V2)?TestPlan`),
	regexp.MustCompile(`\bdeliveryImport\b|\bdeliveryStart\b|\bdeliveryReview\b|\bdeliveryDoctor\b|\bdeliveryContext\b|\bdeliveryBind\b|\bdeliveryAmendment\b|\bdeliveryRollout\b|\bdeliveryPhaseReadiness\b|\bdeliveryCapabilities\b`),
	regexp.MustCompile(`factory_intake|factoryIntake|FactoryIntake`),
	regexp.MustCompile(`\buseDelivery[A-Z]|\buseRunTask\b|\buseWaveExecute\b|api\.runTask|api\.waveExecute|\bdeliveryRequest\b`),
	regexp.MustCompile(`/api/delivery/|/delivery/plans|/delivery/review|/delivery/start`),
	regexp.MustCompile(`"wave",\s*"(arm|disarm|preflight|refingerprint|execute)"|"delivery",\s*"(import|plan|start|review|context|bind|doctor|rollout)"`),
	regexp.MustCompile(`delivery (plan|import|start|review|context|bind|doctor|rollout) --`),
	regexp.MustCompile(`mark[-_]?ready`),
	regexp.MustCompile(`_BKP|backupParser|BackupParser`),
	regexp.MustCompile(`\bDeliveryPlan(List|Summary)?\b|\bDeliveryReview(Link|State)?\b|\bDeliveryStartResult\b|\bDeliveryError(Payload)?\b|\bDeliveryCrossScopeDependency\b|\bWaveExecute(Result)?\b|\bWaveExecutionReceipt\b`),
	regexp.MustCompile(`migrate direct-waves`),
	regexp.MustCompile(`delivery_cross_scope_dependencies|delivery_source_key|delivery_plan_scope|delivery_contract_fingerprint|delivery_plan_fingerprint|context_fingerprint|delivery_plan_schema`),
	// Prose-level patterns: canonical docs and operator-facing text must not
	// keep teaching the removed commands or plan authority, even when they use
	// no identifier-shaped token.
	regexp.MustCompile(`(?i)\bdelivery[ _-]?plans?\b`),
	regexp.MustCompile(`(?i)\bdelivery[ _-]?(import|start|review|context|bind|doctor|rollout|amendment|capabilities)\b`),
	regexp.MustCompile(`\bwave[ _](arm|disarm|preflight|refingerprint|execute)\b`),
}

func TestDirectWaveRemovalScanFindsNoLegacySurfaces(t *testing.T) {
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	scanned := 0
	var violations []string
	for _, root := range directWaveRemovalScanRoots {
		base := filepath.Join(repoRoot, root)
		err := filepath.WalkDir(base, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				switch entry.Name() {
				case "node_modules", "dist", ".git", "__pycache__":
					return filepath.SkipDir
				}
				return nil
			}
			ext := filepath.Ext(path)
			if ext != ".go" && ext != ".ts" && ext != ".tsx" && ext != ".md" && ext != ".py" && ext != ".mjs" && ext != ".sh" && ext != ".json" && ext != ".yaml" && ext != ".yml" {
				return nil
			}
			rel, err := filepath.Rel(repoRoot, path)
			if err != nil {
				return err
			}
			rel = filepath.ToSlash(rel)
			if directWaveRemovalAllowlist[rel] {
				return nil
			}
			if strings.HasPrefix(rel, "docs/reports/") || strings.HasPrefix(rel, "docs/specs/") {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			scanned++
			for i, line := range strings.Split(string(data), "\n") {
				for _, pattern := range directWaveRemovalForbidden {
					if pattern.MatchString(line) {
						violations = append(violations, rel+":"+strconv.Itoa(i+1)+" matches "+pattern.String()+": "+strings.TrimSpace(line))
					}
				}
			}
			return nil
		})
		if err != nil && !os.IsNotExist(err) {
			t.Fatalf("scan %s: %v", root, err)
		}
	}
	if scanned == 0 {
		t.Fatal("removal scan matched zero files; scan roots are wrong")
	}
	if len(violations) > 0 {
		t.Fatalf("delivery-plan/factory-intake surfaces survive in %d places:\n%s", len(violations), strings.Join(violations, "\n"))
	}
}

func TestDirectWaveRemovalFixtureJourneyUsesNoPlanFile(t *testing.T) {
	vault := v7DirectTestVault(t)
	request := writeDirectIntakeRequest(t, vault, map[string]any{
		"schema":      "tusker.wave-authoring/v1",
		"request_key": "removal-journey",
		"title":       "Removal journey",
		"outcome":     "Direct create/read/update/review/packet works with no plan file.",
		"tasks": []map[string]any{
			{"key": "root", "title": "Removal root", "work_level": "light", "body": "# Removal root\n\n## Intent\n\nProve direct authoring.\n\n## Acceptance\n\n| ID | Outcome |\n| --- | --- |\n| A1 | Works. |\n"},
			{"key": "leaf", "title": "Removal leaf", "work_level": "light", "dependencies": []map[string]any{{"task": "root", "kind": "hard"}}, "body": "# Removal leaf\n\n## Intent\n\nFollow the root.\n\n## Acceptance\n\n| ID | Outcome |\n| --- | --- |\n| A1 | Works. |\n"},
		},
	})
	if err := waveV7CreateCmd(Args{"vault": vault, "file": request, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	idx := mustIndex(t, vault)
	wave, ok := idx.Waves["W-0001"]
	if !ok {
		t.Fatalf("direct authoring created no wave: %#v", idx.Waves)
	}
	if stringField(wave.Data, "authorization") == "armed" {
		t.Fatal("creation armed the wave; creation must be inert")
	}
	if len(idx.Tasks) != 2 {
		t.Fatalf("direct authoring created %d tasks, want 2", len(idx.Tasks))
	}
	for id, task := range idx.Tasks {
		packet := v7Packet(vault, task, idx, "agent")
		if !strings.Contains(packet, "## Intent") {
			t.Fatalf("packet for %s lost the authored body", id)
		}
	}
	if err := waveReviewCmd(Args{"vault": vault, "_pos0": "W-0001", "quiet": "true"}); err != nil {
		t.Fatalf("direct wave review failed with no plan file: %v", err)
	}
}

func TestDirectWaveRemovalRollbackAndAutonomousCoverageStillRun(t *testing.T) {
	t.Run("rollback-idempotence", TestDirectWaveAuthoringRollbackIdempotencyAndConflict)
	t.Run("receipt-replay", TestDirectWaveAuthoringReceiptReplayIgnoresMemberOrder)
	t.Run("autonomous-frontier", TestDirectWaveAutonomousFrontierAdvancesAfterCompletion)
	t.Run("pause-resume", TestDirectWaveAutonomousPausePreservesAuthorityAndContinuity)
}
