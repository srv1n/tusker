package main

import (
	"fmt"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
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
	if err := writeDefaultTuskerConfig(vaultPath); err != nil {
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

func writeDefaultTuskerConfig(vaultPath string) error {
	// Keep an existing root-level config readable, but never create one. This
	// prevents init from producing a duplicate competing configuration file.
	if fileExists(managedTuskerConfigPath(vaultPath)) || fileExists(legacyTuskerConfigPath(v7RepoRoot(vaultPath))) {
		return nil
	}
	configPath := managedTuskerConfigPath(vaultPath)
	projectID := sanitizeProjectID(filepath.Base(filepath.Dir(vaultPath)))
	root := filepath.ToSlash(filepath.Base(vaultPath))
	profiles := semanticBootstrapProfiles(discoverRunnerCatalog(false))
	profileYAML := ""
	if len(profiles) > 0 {
		profileRaw, err := yaml.Marshal(profiles)
		if err != nil {
			return err
		}
		profileYAML = "  profiles:\n" + indentBootstrapYAML(string(profileRaw), "    ") + "\n"
	}
	defaultProfileYAML := ""
	if hasBootstrapProfile(profiles, "execute-standard") {
		defaultProfileYAML = "  default_profile: execute-standard\n"
	}
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
  # Editable semantic defaults. They are policy, not a machine-local model catalog.
%s
%s
  dispatch_scope: armed_waves
  # The deterministic review-completion reactor is separately opt-in. Its
  # modes are disabled, shadow (read-only comparison), and authoritative.
  completion_reactor:
    mode: disabled
  trigger_states: [ready, rework]
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
    codex_exec:
      kind: codex_exec
      command: codex exec --json --skip-git-repo-check -
    claude-code:
      kind: claude-code
      command: claude -p --output-format stream-json --input-format stream-json --permission-mode bypassPermissions
  fanout:
    enabled: false
    max_children: 0
    allowed_child_types: []
    merge_rule: manual_review
`, projectID, root, root, root, root, root, defaultProfileYAML, profileYAML))
}

// writeDefaultRootTuskerConfig is retained for in-package compatibility while
// callers migrate. It no longer writes at repository root.
func writeDefaultRootTuskerConfig(vaultPath string) error {
	return writeDefaultTuskerConfig(vaultPath)
}

func indentBootstrapYAML(value, indent string) string {
	return indent + strings.ReplaceAll(strings.TrimSuffix(value, "\n"), "\n", "\n"+indent)
}
