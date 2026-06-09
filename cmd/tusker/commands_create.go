package main

import (
	"fmt"
	"path/filepath"
	"strings"
)

func bootstrap(args Args) error {
	if args.Bool("legacy") || args.Bool("v5") {
		return tuskerError(errorInvalidArg, "legacy bootstrap has been removed; V7 init is the only supported bootstrap path", withHint("use `tusker init --yes`"))
	}
	return bootstrapV7(args)
}

func bootstrapV7(args Args) error {
	vaultPath, err := resolveVaultPath(args, true)
	if err != nil {
		return err
	}
	if err := bootstrapV7Dirs(vaultPath); err != nil {
		return err
	}
	if err := writeDefaultRootTuskerConfig(vaultPath); err != nil {
		return err
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
		fmt.Printf("Tusker V7 vault initialized at %s\n", vaultPath)
	}
	return nil
}

func bootstrapLegacy(args Args) error {
	return tuskerError(errorInvalidArg, "legacy bootstrap has been removed; V7 init is the only supported bootstrap path", withHint("use `tusker init --yes`"))
}

func writeDefaultRootTuskerConfig(vaultPath string) error {
	configPath := filepath.Join(filepath.Dir(vaultPath), "tusker.yaml")
	if fileExists(configPath) {
		return nil
	}
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

automation:
  enabled: true
  trigger_states: [ready, rework]
  default_runner: codex_app_server
  enabled_runners: [codex_app_server, codex_exec, claude-code]
  workspace:
    strategy: worktree
    root: ../.tusker-worktrees
  concurrency:
    max_active_runs: 2
    max_active_runs_per_project: 1
    max_concurrent_by_state:
      rework: 1
  runners:
    codex_app_server:
      kind: codex_app_server
      command: codex app-server
    codex_exec:
      kind: codex_exec
      command: codex exec --skip-git-repo-check -
    claude-code:
      kind: claude-code
      command: claude -p --output-format stream-json --input-format stream-json --permission-mode bypassPermissions
  fanout:
    enabled: false
    max_children: 0
    allowed_child_types: []
    merge_rule: manual_review
`, projectID, root, root, root, root, root))
}
