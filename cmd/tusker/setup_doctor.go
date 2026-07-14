package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

const setupDoctorSchema = "tusker.setup-doctor/v1"

type setupFinding struct {
	Code       string `json:"code"`
	Status     string `json:"status"`
	Path       string `json:"path,omitempty"`
	Message    string `json:"message"`
	Action     string `json:"action,omitempty"`
	Repairable bool   `json:"repairable"`
	Changed    bool   `json:"changed,omitempty"`
}

type setupDoctorReport struct {
	Schema   string         `json:"schema"`
	RepoRoot string         `json:"repo_root"`
	DryRun   bool           `json:"dry_run"`
	OK       bool           `json:"ok"`
	Findings []setupFinding `json:"findings"`
}

type setupDoctorInput struct {
	RepoRoot       string
	Store          *RuntimeStore
	Source         string
	ExecutablePath string
	InstalledPath  string
}

func setupDoctorCmd(args Args, apply bool) error {
	if args.Bool("help") {
		printSetupHelp()
		return nil
	}
	repo := firstNonEmpty(strings.TrimSpace(args.String("repo")), ".")
	absRepo, err := filepath.Abs(repo)
	if err != nil {
		return err
	}
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		return err
	}
	defer store.Close()
	report, err := runSetupDoctor(setupDoctorInput{RepoRoot: absRepo, Store: store, Source: args.String("source")}, apply && !args.Bool("dry-run"))
	if err != nil {
		return err
	}
	if args.Bool("json") {
		emitJSON(report)
		return nil
	}
	verb := "Setup doctor"
	if !report.DryRun {
		verb = "Setup repair"
	}
	fmt.Printf("%s: %d finding(s)\n", verb, len(report.Findings))
	for _, finding := range report.Findings {
		suffix := ""
		if finding.Changed {
			suffix = " [repaired]"
		}
		fmt.Printf("  %s %-30s %s%s\n", strings.ToUpper(finding.Status), finding.Code, finding.Message, suffix)
		if finding.Action != "" {
			fmt.Printf("    action: %s\n", finding.Action)
		}
	}
	return nil
}

func runSetupDoctor(input setupDoctorInput, apply bool) (setupDoctorReport, error) {
	repo, err := filepath.Abs(input.RepoRoot)
	if err != nil {
		return setupDoctorReport{}, err
	}
	report := setupDoctorReport{Schema: setupDoctorSchema, RepoRoot: repo, DryRun: !apply, OK: true, Findings: []setupFinding{}}
	add := func(f setupFinding) { report.Findings = append(report.Findings, f) }

	if input.Store != nil {
		loaded, err := loadRegisteredProjects(input.Store, registeredProjectLoadOptions{MetadataOnly: true, LoadDisabled: true})
		if err != nil {
			return report, err
		}
		projects := loadedRegisteredProjects(loaded)
		for _, project := range projects {
			if !sameCanonicalProjectPath(project.RepoRoot, repo) {
				continue
			}
			expectedVault := filepath.Join(project.RepoRoot, ".tusker")
			currentMissing := !fileExists(project.VaultRoot)
			if currentMissing && fileExists(expectedVault) && !sameCleanPath(project.VaultRoot, expectedVault) {
				finding := setupFinding{Code: "stale_vault_root", Status: "error", Path: project.VaultRoot, Repairable: true,
					Message: fmt.Sprintf("registered project %s points at %s instead of %s", project.ProjectID, project.VaultRoot, expectedVault),
					Action:  "update the registration to the repository's .tusker root"}
				if apply {
					project.VaultRoot = expectedVault
					project.WorkflowPath = workflowPath(expectedVault)
					if err := input.Store.UpsertProject(project); err != nil {
						return report, err
					}
					finding.Changed = true
				}
				add(finding)
				continue
			}
			expectedWorkflow := workflowPath(project.VaultRoot)
			if !sameCleanPath(project.WorkflowPath, expectedWorkflow) || !fileExists(expectedWorkflow) {
				finding := setupFinding{Code: "workflow_path_mismatch", Status: "error", Path: project.WorkflowPath,
					Message: fmt.Sprintf("registered project %s does not resolve to its WORKFLOW.md", project.ProjectID),
					Action:  "restore <vault>/WORKFLOW.md, then rerun setup repair", Repairable: fileExists(expectedWorkflow)}
				if apply && finding.Repairable {
					project.WorkflowPath = expectedWorkflow
					if err := input.Store.UpsertProject(project); err != nil {
						return report, err
					}
					finding.Changed = true
				}
				add(finding)
			}
		}
	}

	localVault := filepath.Join(repo, ".tusker")
	if info, err := os.Lstat(localVault); err == nil && info.Mode()&os.ModeSymlink != 0 {
		if _, err := filepath.EvalSymlinks(localVault); err != nil {
			add(setupFinding{Code: "broken_vault_symlink", Status: "error", Path: localVault, Message: "repo-local .tusker symlink is broken", Action: "recreate the vault link with tusker vault mount", Repairable: false})
		}
	}

	sourceReport := classifySkillSyncSource(input.Source, repo)
	for _, destination := range []string{filepath.Join(repo, ".agents", "skills", currentSkillInstallDir), filepath.Join(repo, ".claude", "skills", currentSkillInstallDir)} {
		status, detail := installedSkillStatus(destination, sourceReport.Path)
		if status == "current" {
			continue
		}
		finding := setupFinding{Code: "skill_install_" + status, Status: "error", Path: destination, Message: detail,
			Action: "run tusker skill sync --repo . --source <canonical-tusker-checkout>", Repairable: sourceReport.Kind == "canonical"}
		if apply && finding.Repairable {
			if err := installSkillPayloadWithModeFrom(destination, skillInstallModeLink, sourceReport.Path); err != nil {
				return report, err
			}
			finding.Changed = true
		}
		add(finding)
	}

	executable := input.ExecutablePath
	if executable == "" {
		executable, _ = os.Executable()
	}
	installed := input.InstalledPath
	if installed == "" {
		installed, _ = exec.LookPath("tusker")
	}
	if executable != "" && installed != "" && !sameResolvedFile(executable, installed) {
		add(setupFinding{Code: "binary_version_mismatch", Status: "warning", Path: installed, Message: "the tusker on PATH is not the running binary", Action: "run tusker update --repo .", Repairable: false})
	}

	configPath := filepath.Join(repo, ".chatgpt-handoff.json")
	config, _, err := readHandoffConfig(configPath)
	if err != nil {
		add(setupFinding{Code: "handoff_config_invalid", Status: "error", Path: configPath, Message: err.Error(), Action: "repair the JSON without removing project-specific routing", Repairable: false})
	} else {
		if !handoffProfilePresent(repo, config) {
			add(setupFinding{Code: "handoff_profile_missing", Status: "warning", Path: configPath, Message: "ChatGPT handoff profile is not configured", Action: "set profile in .chatgpt-handoff.json or add .chatgpt-handoff/profile.md", Repairable: false})
		}
		if !handoffRoutingPresent(config) {
			add(setupFinding{Code: "handoff_routing_missing", Status: "warning", Path: configPath, Message: "ChatGPT Project routing is not configured", Action: "set project_id or routing in .chatgpt-handoff.json", Repairable: false})
		}
		pattern := handoffAttachmentPattern(config)
		patternCode := ""
		patternMessage := ""
		if pattern == "" {
			patternCode = "handoff_zip_pattern_missing"
			patternMessage = "zip attachment matching is unspecified; safe default is *.zip"
		} else if matched, matchErr := filepath.Match(pattern, "handoff-result.zip"); matchErr != nil || !matched {
			patternCode = "handoff_zip_pattern_invalid"
			patternMessage = fmt.Sprintf("zip attachment pattern %q does not match handoff-result.zip", pattern)
		}
		if patternCode != "" {
			finding := setupFinding{Code: patternCode, Status: "warning", Path: configPath, Message: patternMessage, Action: "set attachments.zip_pattern to *.zip", Repairable: true}
			if apply {
				attachments, _ := config["attachments"].(map[string]any)
				if attachments == nil {
					attachments = map[string]any{}
				}
				attachments["zip_pattern"] = "*.zip"
				config["attachments"] = attachments
				if err := writeHandoffConfig(configPath, config); err != nil {
					return report, err
				}
				finding.Changed = true
			}
			add(finding)
		}
		installedVersion := mapString(config, "browser_workflow_version")
		requiredVersion := mapString(config, "required_browser_workflow_version")
		if installedVersion != "" && requiredVersion != "" && installedVersion != requiredVersion {
			add(setupFinding{Code: "handoff_browser_workflow_stale", Status: "error", Path: configPath,
				Message: fmt.Sprintf("browser workflow %s does not match required %s", installedVersion, requiredVersion),
				Action:  "run rzn-browser workflow pull --repo-root <canonical-rzn-browser-checkout>", Repairable: false})
		}
	}

	sort.SliceStable(report.Findings, func(i, j int) bool {
		if report.Findings[i].Code == report.Findings[j].Code {
			return report.Findings[i].Path < report.Findings[j].Path
		}
		return report.Findings[i].Code < report.Findings[j].Code
	})
	for _, finding := range report.Findings {
		if finding.Status == "error" && !finding.Changed {
			report.OK = false
		}
	}
	return report, nil
}

type skillSourceReport struct {
	Kind string `json:"kind"`
	Path string `json:"path,omitempty"`
}

func classifySkillSyncSource(sourceArg, repo string) skillSourceReport {
	sourceArg = strings.TrimSpace(sourceArg)
	if sourceArg == "" {
		if root, err := findRepoRoot(mustGetwd()); err == nil && root != "" {
			sourceArg = root
		} else {
			sourceArg = repo
		}
	}
	abs, err := filepath.Abs(sourceArg)
	if err != nil {
		return skillSourceReport{Kind: "invalid"}
	}
	path := abs
	if !fileExists(filepath.Join(path, "SKILL.md")) && fileExists(filepath.Join(path, "skill", "SKILL.md")) {
		path = filepath.Join(path, "skill")
	}
	if !fileExists(filepath.Join(path, "SKILL.md")) {
		return skillSourceReport{Kind: "invalid", Path: path}
	}
	clean := filepath.ToSlash(path)
	if strings.Contains(clean, "/.agents/skills/") || strings.Contains(clean, "/.claude/skills/") || strings.Contains(clean, "/_generated/") {
		return skillSourceReport{Kind: "generated", Path: path}
	}
	return skillSourceReport{Kind: "canonical", Path: path}
}

func installedSkillStatus(destination, canonicalSource string) (string, string) {
	info, err := os.Lstat(destination)
	if os.IsNotExist(err) {
		return "missing", "generated skill install is missing"
	}
	if err != nil {
		return "invalid", err.Error()
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return "generated_copy", "generated skill install is a materialized copy; local development expects a canonical symlink"
	}
	target, err := filepath.EvalSymlinks(destination)
	if err != nil {
		return "broken", "generated skill symlink is broken"
	}
	if canonicalSource == "" || !sameResolvedFile(target, canonicalSource) {
		return "stale", "generated skill symlink does not target the selected canonical source"
	}
	return "current", ""
}

func sameResolvedFile(a, b string) bool {
	ra, errA := filepath.EvalSymlinks(a)
	rb, errB := filepath.EvalSymlinks(b)
	if errA != nil || errB != nil {
		return sameCleanPath(a, b)
	}
	return sameCleanPath(ra, rb)
}

func readHandoffConfig(path string) (map[string]any, bool, error) {
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return map[string]any{}, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	var config map[string]any
	if err := json.Unmarshal(raw, &config); err != nil {
		return nil, true, fmt.Errorf("invalid ChatGPT handoff config: %w", err)
	}
	return config, true, nil
}

func writeHandoffConfig(path string, config map[string]any) error {
	raw, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	return writeText(path, string(raw)+"\n")
}

func handoffProfilePresent(repo string, config map[string]any) bool {
	if strings.TrimSpace(mapString(config, "profile")) != "" || fileExists(filepath.Join(repo, ".chatgpt-handoff", "profile.md")) {
		return true
	}
	profile, ok := config["profile"].(map[string]any)
	return ok && len(profile) > 0
}

func handoffRoutingPresent(config map[string]any) bool {
	if mapString(config, "project_id") != "" {
		return true
	}
	routing, ok := config["routing"].(map[string]any)
	return ok && (mapString(routing, "project_id") != "" || mapString(routing, "project") != "")
}

func handoffAttachmentPattern(config map[string]any) string {
	for _, key := range []string{"zip_attachment_pattern", "attachment_pattern"} {
		if value := mapString(config, key); value != "" {
			return value
		}
	}
	attachments, _ := config["attachments"].(map[string]any)
	return firstNonEmpty(mapString(attachments, "zip_pattern"), mapString(attachments, "pattern"))
}

func mapString(values map[string]any, key string) string {
	value, _ := values[key].(string)
	return strings.TrimSpace(value)
}

func printSetupHelp() {
	fmt.Println(`Usage:
  tusker setup doctor [--repo <path>] [--source <canonical-tusker-checkout>] [--json]
  tusker setup repair [--repo <path>] [--source <canonical-tusker-checkout>] [--dry-run] [--json]

Purpose:
  Diagnose onboarding drift across registered vault roots, WORKFLOW.md paths,
  Tusker binaries and generated skills, plus offline ChatGPT handoff routing,
  zip attachment matching, and browser workflow versions. Doctor never writes.
  Repair applies only deterministic local fixes and is idempotent; credentials,
  project routing, and browser workflow refreshes remain explicit actions.`)
}
