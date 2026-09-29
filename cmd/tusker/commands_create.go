package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

func bootstrapV7(args Args) error {
	vaultPath, err := resolveVaultPath(args, true)
	if err != nil {
		return err
	}
	if err := bootstrapV7Dirs(vaultPath); err != nil {
		return err
	}
	configExisted := fileExists(managedTuskerConfigPath(vaultPath))
	if err := writeDefaultTuskerConfig(vaultPath); err != nil {
		return err
	}
	if repo := filepath.Dir(vaultPath); !configExisted && !args.Bool("quiet") && detectValidationCommands(repo) == nil && !fileExists(filepath.Join(repo, "go.mod")) {
		fmt.Fprintln(os.Stderr, "tusker init: no test command detected; set automation.validation.commands in .tusker/config.yaml before landing (the built-in gate runs Go commands).")
	}
	if err := ensureV7Domain(vaultPath, "project", "Project", "Durable project knowledge."); err != nil {
		return err
	}
	if err := writeDefaultV7ProjectSkillIfMissing(vaultPath); err != nil {
		return err
	}
	if err := upsertGitignore(vaultPath); err != nil {
		return err
	}
	if epic := strings.ToUpper(args.String("epic")); epic != "" {
		if !epicAcronymPattern.MatchString(epic) {
			return tuskerError(errorInvalidArg, fmt.Sprintf(`--epic must be 3 uppercase letters, got "%s"`, args.String("epic")), withContext(map[string]any{"arg": "--epic", "value": args.String("epic")}))
		}
		title, err := requireArg(args, "title")
		if err != nil {
			return tuskerError(errorMissingArg, "--title (required with --epic)")
		}
		if err := newV7Epic(Args{
			"vault":   vaultPath,
			"quiet":   "true",
			"acronym": epic,
			"title":   title,
			"owner":   args.String("owner"),
			"summary": args.String("summary"),
			"status":  fallback(args.String("status"), "ready"),
		}); err != nil {
			return err
		}
	}
	if !args.Bool("quiet") {
		fmt.Printf("Tusker vault initialized at %s\n", vaultPath)
	}
	return nil
}

func writeDefaultTuskerConfig(vaultPath string) error {
	if fileExists(managedTuskerConfigPath(vaultPath)) {
		return nil
	}
	configPath := managedTuskerConfigPath(vaultPath)
	projectID := sanitizeProjectID(filepath.Base(filepath.Dir(vaultPath)))
	root := filepath.ToSlash(filepath.Base(vaultPath))
	return writeText(configPath, fmt.Sprintf(`schema: tusker.config/v1
project_id: %s

storage:
  root: %s
  generated_root: %s/_generated
  evidence_root: %s/evidence
  events_root: %s/events
  attempts_root: %s/attempts

runtime:
  lease_backend: local
  lease_ttl_minutes: 120
  mutation_mode: single_user_local

automation:
  # Automation is opt-in. Registration keeps status projections fresh; only
  # an explicit operator change may authorize daemon dispatch.
  enabled: false
  # Runner profiles live only in the global config (tusker runner profiles
  # --write); this project selects them by name via automation.model_levels.
  dispatch_scope: armed_waves
  # After a passing review the daemon lands and closes the task
  # (authoritative). Set disabled to land and close by hand.
  completion_reactor:
    mode: authoritative
%s  trigger_states: [ready, rework]
  # Direct Codex is available after fresh setup. tusker acp setup can add the
  # pinned ACP adapter later when that machine has been configured for it.
  default_runner: codex_exec
  enabled_runners: [codex_exec, claude-code]
  workspace:
    strategy: worktree
    root: workspaces
  concurrency:
    max_active_runs: 2
    max_active_runs_per_project: 1
    max_concurrent_by_state:
      rework: 1
  runners:
    # Direct Codex is the current default runner. ACP is added only by its
    # explicit machine-local setup.
    codex_exec:
      kind: codex_exec
      command: codex exec --json --skip-git-repo-check -
    claude-code:
      kind: claude-code
      command: claude -p
  fanout:
    enabled: false
    max_children: 0
    allowed_child_types: []
    merge_rule: manual_review
`, projectID, root, root, root, root, root, validationConfigBlock(filepath.Dir(vaultPath))))
}

// validationConfigBlock writes the landing gate for the test command init can
// detect. Go repositories and undetected ones keep the built-in Go gate.
func validationConfigBlock(repoRoot string) string {
	commands := detectValidationCommands(repoRoot)
	if len(commands) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("  # Landing gate, detected by tusker init. Edit to match the project.\n  validation:\n    commands:\n")
	for _, command := range commands {
		fmt.Fprintf(&b, "      - %s\n", command)
	}
	return b.String()
}

// detectValidationCommands returns the test command for a non-Go repository
// root, or nil when there is nothing to detect.
func detectValidationCommands(repoRoot string) []string {
	read := func(name string) string {
		raw, err := os.ReadFile(filepath.Join(repoRoot, name))
		if err != nil {
			return ""
		}
		return string(raw)
	}
	if fileExists(filepath.Join(repoRoot, "go.mod")) {
		return nil
	}
	var pkg struct {
		Scripts map[string]string `json:"scripts"`
	}
	if json.Unmarshal([]byte(read("package.json")), &pkg) == nil && pkg.Scripts["test"] != "" && !strings.Contains(pkg.Scripts["test"], "no test specified") {
		return []string{"npm test"}
	}
	if fileExists(filepath.Join(repoRoot, "Cargo.toml")) {
		return []string{"cargo test"}
	}
	if regexp.MustCompile(`(?m)^test:`).MatchString(read("Makefile")) {
		return []string{"make test"}
	}
	if fileExists(filepath.Join(repoRoot, "pytest.ini")) || strings.Contains(read("pyproject.toml"), "[tool.pytest") {
		return []string{"python3 -m pytest"}
	}
	if pyTests, _ := filepath.Glob(filepath.Join(repoRoot, "test_*.py")); len(pyTests) > 0 {
		return []string{"python3 -m unittest"}
	}
	if pyTests, _ := filepath.Glob(filepath.Join(repoRoot, "tests", "test_*.py")); len(pyTests) > 0 {
		return []string{"python3 -m unittest discover -s tests"}
	}
	return nil
}
