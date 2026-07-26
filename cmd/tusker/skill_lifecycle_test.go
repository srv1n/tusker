package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSkillSymlinkTargetIsRelativeInsideTheRepo(t *testing.T) {
	repo := t.TempDir()
	source := filepath.Join(repo, "skills", "tusker")
	writeCanonicalTuskerSkillFixture(t, repo)
	if err := writeText(filepath.Join(repo, "go.mod"), "module sample\n"); err != nil {
		t.Fatal(err)
	}
	for _, install := range []string{".agents", ".claude"} {
		destination := filepath.Join(repo, install, "skills", "tusker")
		target := skillSymlinkTarget(source, destination)
		if filepath.IsAbs(target) {
			t.Fatalf("%s install got an absolute target %q: it would dangle in every worktree and fresh clone", install, target)
		}
		if want := filepath.Join("..", "..", "skills", "tusker"); target != want {
			t.Fatalf("%s install target %q, want %q", install, target, want)
		}
		if err := installSkillPayloadSymlink(destination, source); err != nil {
			t.Fatal(err)
		}
		resolved, err := filepath.EvalSymlinks(filepath.Join(destination, "SKILL.md"))
		if err != nil {
			t.Fatal(err)
		}
		canonical, err := filepath.EvalSymlinks(filepath.Join(source, "SKILL.md"))
		if err != nil {
			t.Fatal(err)
		}
		if resolved != canonical {
			t.Fatalf("%s install does not resolve to the canonical skill: %s != %s", install, resolved, canonical)
		}
	}
}

func TestSkillSymlinkTargetStaysAbsoluteOutsideTheRepo(t *testing.T) {
	repo := t.TempDir()
	source := filepath.Join(repo, "skills", "tusker")
	if err := writeText(filepath.Join(source, "SKILL.md"), "# skill\n"); err != nil {
		t.Fatal(err)
	}
	if err := writeText(filepath.Join(repo, "go.mod"), "module sample\n"); err != nil {
		t.Fatal(err)
	}
	target := skillSymlinkTarget(source, filepath.Join(t.TempDir(), ".claude", "skills", "tusker"))
	if target != source {
		t.Fatalf("user-home install lost its absolute target: %q", target)
	}
}

func TestSkillExecutionOwnershipProtocol(t *testing.T) {
	source := filepath.Join("..", "..", "skills", "tusker", "SKILL.md")
	raw, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, required := range []string{
		"Never start `tusker daemon run`", "independently running resident daemon",
		"The daemon atomically claims the task before it creates the worker process",
		"it must not claim again", "tusker runs inspect <TASK-ID>",
		"runner harness owns session attachment, heartbeats, process monitoring",
		"heartbeat expiry and safe reclaim", "Never write `active` or", "`in_progress` into task frontmatter",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("canonical skill missing %q", required)
		}
	}
	if strings.Contains(text, "tusker runs claim <TASK-ID>") {
		t.Fatal("dispatched worker instructions must not repeat the daemon's pre-spawn claim")
	}
	for _, installed := range []string{filepath.Join("..", "..", ".agents", "skills", "tusker", "SKILL.md"), filepath.Join("..", "..", ".claude", "skills", "tusker", "SKILL.md")} {
		resolved, err := filepath.EvalSymlinks(installed)
		if err != nil {
			t.Fatal(err)
		}
		if resolved != filepath.Clean(source) {
			absResolved, _ := filepath.Abs(resolved)
			absSource, _ := filepath.Abs(source)
			if absResolved != absSource {
				t.Fatalf("generated install %s does not resolve to canonical skill", installed)
			}
		}
	}
}

func TestSkillReservesHumanApprovalForHumanOnlyBoundaries(t *testing.T) {
	source := filepath.Join("..", "..", "skills", "tusker", "SKILL.md")
	raw, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, required := range []string{
		"## Human Approval Boundary",
		"Everything already decided by the task, acceptance criteria, governing spec, or",
		"final human acceptance of screenshots, recordings, UX feel, brand quality",
		"Risk changes proof depth, reviewer strength, and landing safeguards; risk alone",
		"Independent reviewers may",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("canonical skill missing human-approval rule %q", required)
		}
	}
}

func TestFactoryExecutionSkillContract(t *testing.T) {
	root := filepath.Join("..", "..", "skills", "tusker")
	raw, err := os.ReadFile(filepath.Join(root, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	normalizedText := strings.Join(strings.Fields(text), " ")
	for _, required := range []string{
		"`tusker.delivery-plan/v2` DAG",
		"Tusker and the agent own",
		"A delivery epic groups the product outcome",
		"an epic is never executable authority",
		"`automation.dispatch_scope: armed_waves`",
		"tusker delivery start --plan <PLAN.yaml>",
		"--confirm <PLAN-FINGERPRINT>",
		"tusker work start <TASK-ID>",
		"works while project automation is disabled",
		"it must not claim again",
		"tusker review submit <TASK-ID>",
		"The deterministic control plane",
		"never merges, lands, closes, moves refs, or schedules successors",
		"desired outcomes, observable acceptance",
		"important tests and failure cases",
		"constraints, priorities, non-goals",
	} {
		if !strings.Contains(normalizedText, required) {
			t.Fatalf("canonical factory execution skill missing %q", required)
		}
	}

	allGuidance := strings.Builder{}
	if err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(path), ".md") {
			return nil
		}
		raw, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		allGuidance.Write(raw)
		allGuidance.WriteByte('\n')
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	guidance := allGuidance.String()
	for _, forbidden := range []string{
		"Before unattended delivery, run `tusker wave preflight",
		"Independent reviewer agents may close",
		"Independent reviewers may close",
		"reviewer may close every risk tier",
		"tusker close <TASK-ID> --by reviewer:",
		"reviewer.land_command",
		"reviewer.close_command",
		"reviewer.finalize_command",
	} {
		if strings.Contains(guidance, forbidden) {
			t.Fatalf("factory guidance retained legacy authority phrase %q", forbidden)
		}
	}

	contract, err := loadFactoryIntakeContract(filepath.Join(root, "assets", "factory-intake-contract.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, guardrail := range []string{
		"tracked_modifying_work_requires_work_start",
		"dispatched_worker_verifies_existing_claim",
		"reviewer_submits_typed_result_only",
		"deterministic_handlers_own_merge_close_and_successor_wake",
		"epic_is_never_execution_authority",
		"project_automation_is_separate_explicit_opt_in",
		"fresh_dispatch_scope_is_armed_waves",
	} {
		if !containsString(contract.Guardrails, guardrail) {
			t.Fatalf("factory intake contract missing execution guardrail %q", guardrail)
		}
	}

	bootstrapRaw, err := os.ReadFile(filepath.Join(root, "assets", "snippets", "AGENTS.md.snippet"))
	if err != nil {
		t.Fatal(err)
	}
	bootstrap := strings.Join(strings.Fields(string(bootstrapRaw)), " ")
	for _, required := range []string{
		"`tusker.delivery-plan/v2` DAG",
		"`tusker work start`",
		"`tusker review submit`",
		"Deterministic Tusker handlers own",
		"fingerprint-bound `tusker delivery start`",
		"`armed_waves`",
		"whole-epic authority",
	} {
		if !strings.Contains(bootstrap, required) {
			t.Fatalf("repo bootstrap guidance missing %q", required)
		}
	}
}
