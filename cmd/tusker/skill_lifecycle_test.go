package main

import (
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

func TestSkillTaskManagementProtocol(t *testing.T) {
	root := filepath.Join("..", "..", "skills", "tusker")
	source := filepath.Join(root, "SKILL.md")
	text := normalizedSkillGuidance(t, root, "SKILL.md", filepath.Join("references", "PLAN.md"), filepath.Join("references", "WORK.md"), filepath.Join("references", "OPERATE.md"))
	for _, required := range []string{
		"task records only", "mutate its records through the CLI",
		"tusker new task", "tusker status <TASK-ID>", "tusker verify add",
		"Never hand-edit", "tracker failure is not a source-code failure",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("canonical skill missing %q", required)
		}
	}
	for _, forbidden := range []string{"git ", "worktree", "merge", "landing", "move refs", "source-sha"} {
		if strings.Contains(strings.ToLower(text), forbidden) {
			t.Fatalf("task-only skill retained repository authority %q", forbidden)
		}
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
	root := filepath.Join("..", "..", "skills", "tusker")
	text := normalizedSkillGuidance(t, root, "SKILL.md", filepath.Join("references", "PLAN.md"), filepath.Join("references", "WORK.md"))
	for _, required := range []string{
		"Everything already decided by the task, acceptance criteria, governing spec",
		"final human acceptance remains appropriate for UX feel, brand quality",
		"Risk alone is not a gate",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("canonical skill missing human-approval rule %q", required)
		}
	}
}

func TestFactorySkillContractIsTaskScoped(t *testing.T) {
	root := filepath.Join("..", "..", "skills", "tusker")
	normalizedText := normalizedSkillGuidance(t, root, "SKILL.md", filepath.Join("references", "PLAN.md"), filepath.Join("references", "WORK.md"), filepath.Join("references", "OPERATE.md"))
	for _, required := range []string{
		"desired outcome, observable acceptance", "One bounded outcome is one task",
		"Use dependencies only when", "Use gates only for missing human authority",
		"tusker show <TASK-ID> --capsule", "tusker proof status <TASK-ID>",
	} {
		if !strings.Contains(normalizedText, required) {
			t.Fatalf("canonical task-management skill missing %q", required)
		}
	}

	contract := canonicalFactoryIntakeContractForTest(t)
	for _, guardrail := range []string{
		"tracked_modifying_work_requires_work_start",
		"dispatched_worker_verifies_existing_claim",
		"reviewer_submits_typed_result_only",
		"deterministic_handlers_own_close_and_successor_wake",
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
		"Tusker task tracking", "`tusker new epic|task|gate|decision`",
		"Use the CLI before direct markdown edits", "task records do not grant authority",
	} {
		if !strings.Contains(bootstrap, required) {
			t.Fatalf("repo bootstrap guidance missing %q", required)
		}
	}
}

func normalizedSkillGuidance(t *testing.T, root string, files ...string) string {
	t.Helper()
	var guidance strings.Builder
	for _, rel := range files {
		raw, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatal(err)
		}
		guidance.Write(raw)
		guidance.WriteByte('\n')
	}
	return strings.Join(strings.Fields(guidance.String()), " ")
}
