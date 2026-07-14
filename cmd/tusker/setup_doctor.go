package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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
	RepoRoot        string
	Store           *RuntimeStore
	Source          string
	ExecutablePath  string
	InstalledPath   string
	WorkflowInspect func(system, workflow string) ([]byte, error)
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
	stateRoot := firstNonEmpty(strings.TrimSpace(args.String("state-root")), DefaultStateRoot())
	var store *RuntimeStore
	if fileExists(runtimeStoreDBPath(stateRoot)) {
		if apply && !args.Bool("dry-run") {
			store, err = OpenRuntimeStore(stateRoot)
		} else {
			store, err = OpenRuntimeStoreReadOnly(stateRoot)
		}
		if err != nil {
			return err
		}
		defer store.Close()
	}
	report, err := runSetupDoctor(setupDoctorInput{RepoRoot: absRepo, Store: store, Source: args.String("source"), WorkflowInspect: inspectRZNWorkflow}, apply && !args.Bool("dry-run"))
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
			legacyVault := filepath.Join(project.RepoRoot, "tusker")
			canonicalExists := pathExistsIncludingSymlink(expectedVault)
			legacyRegistration := sameCleanPath(project.VaultRoot, legacyVault)
			missingRegistration := !pathExistsIncludingSymlink(project.VaultRoot)
			if canonicalExists && !sameCleanPath(project.VaultRoot, expectedVault) && (legacyRegistration || missingRegistration) {
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
		resolved, resolveErr := filepath.EvalSymlinks(localVault)
		if resolveErr != nil {
			add(setupFinding{Code: "broken_vault_symlink", Status: "error", Path: localVault, Message: "repo-local .tusker symlink is broken", Action: "recreate the vault link with tusker vault mount", Repairable: false})
		} else if sameResolvedFile(resolved, filepath.Join(repo, "tusker")) {
			add(setupFinding{Code: "stale_vault_symlink", Status: "warning", Path: localVault, Message: "repo-local .tusker symlink still targets the legacy repo/tusker vault", Action: "run tusker migrate vault-root --to .tusker and replace the legacy link after verification", Repairable: false})
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
	config, configExists, err := readHandoffConfig(configPath)
	if err != nil {
		add(setupFinding{Code: "handoff_config_invalid", Status: "error", Path: configPath, Message: err.Error(), Action: "repair the JSON without removing project-specific routing", Repairable: false})
	} else {
		if !configExists {
			add(setupFinding{Code: "handoff_config_missing", Status: "warning", Path: configPath, Message: "ChatGPT handoff config is missing", Action: "run chatgpt-handoff init --project <url-or-id>", Repairable: false})
		} else if schema := mapString(config, "schema"); schema != "rzn.chatgpt_handoff.config/v1" {
			add(setupFinding{Code: "handoff_config_schema", Status: "error", Path: configPath, Message: fmt.Sprintf("unsupported ChatGPT handoff schema %q", schema), Action: "run chatgpt-handoff init and preserve project-specific values", Repairable: false})
		} else {
			if !handoffProfilePresent(repo, config) {
				add(setupFinding{Code: "handoff_profile_missing", Status: "warning", Path: filepath.Join(repo, ".chatgpt-handoff", "profile.md"), Message: "ChatGPT handoff profile is not configured", Action: "run chatgpt-handoff init or add .chatgpt-handoff/profile.md", Repairable: false})
			}
			if !handoffRoutingPresent(config) {
				add(setupFinding{Code: "handoff_routing_missing", Status: "warning", Path: configPath, Message: "ChatGPT Project routing is not configured", Action: "set project_id or project_url in .chatgpt-handoff.json", Repairable: false})
			}
			zipFinding, nextConfig := diagnoseHandoffZip(repo, config, configPath)
			if zipFinding != nil {
				if apply && zipFinding.Repairable {
					if err := writeHandoffConfig(configPath, nextConfig); err != nil {
						return report, err
					}
					zipFinding.Changed = true
					config = nextConfig
				}
				add(*zipFinding)
			}
			for _, finding := range diagnoseHandoffWorkflows(config, configPath, input.WorkflowInspect) {
				add(finding)
			}
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
	if !isCanonicalTuskerSkillPackage(path) {
		return skillSourceReport{Kind: "invalid", Path: path}
	}
	return skillSourceReport{Kind: "canonical", Path: path}
}

func isCanonicalTuskerSkillPackage(path string) bool {
	raw, err := os.ReadFile(filepath.Join(path, "SKILL.md"))
	if err != nil {
		return false
	}
	frontmatter, body, err := parseFrontmatter(string(raw))
	if err != nil || stringField(frontmatter, "name") != "tusker" {
		return false
	}
	metadata, ok := frontmatter["metadata"].(map[string]any)
	if !ok || stringField(metadata, "wave_authorization_schema") != "tusker.wave-authorization/v1" || intField(metadata, "workflow_version") != 1 || intField(metadata, "tracker_schema_version") != 7 {
		return false
	}
	if !strings.Contains(body, "# Tusker Operator Skill") {
		return false
	}
	for _, rel := range []string{filepath.Join("references", "COMMANDS.md"), filepath.Join("references", "REPO_CONTRACT.md"), filepath.Join("references", "WORKFLOW.md")} {
		if !fileExists(filepath.Join(path, rel)) {
			return false
		}
	}
	return true
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

func handoffProfilePresent(repo string, _ map[string]any) bool {
	return fileExists(filepath.Join(repo, ".chatgpt-handoff", "profile.md"))
}

func handoffRoutingPresent(config map[string]any) bool {
	return mapString(config, "project_id") != "" || mapString(config, "project_url") != ""
}

func diagnoseHandoffZip(repo string, config map[string]any, configPath string) (*setupFinding, map[string]any) {
	zipConfig, _ := config["zip"].(map[string]any)
	artifactsDir := firstNonEmpty(mapString(zipConfig, "artifacts_dir"), "artifacts")
	pattern := firstNonEmpty(mapString(zipConfig, "pattern"), "-codebase-")
	configuredRoot := filepath.Join(repo, filepath.FromSlash(artifactsDir))
	if handoffZipExists(configuredRoot, pattern) {
		return nil, config
	}

	if artifact, ok := newestZip(filepath.Join(repo, "artifacts")); ok {
		relDir, err := filepath.Rel(repo, filepath.Dir(artifact))
		if err == nil && relDir != "." && !strings.HasPrefix(relDir, "..") {
			next := cloneStringMap(config)
			nextZip, _ := next["zip"].(map[string]any)
			if nextZip == nil {
				nextZip = map[string]any{}
				next["zip"] = nextZip
			}
			changed := false
			if detectedDir := filepath.ToSlash(relDir); mapString(nextZip, "artifacts_dir") != detectedDir {
				nextZip["artifacts_dir"] = detectedDir
				changed = true
			}
			if detectedPattern := inferHandoffZipPattern(filepath.Base(artifact)); detectedPattern != "" && mapString(nextZip, "pattern") != detectedPattern {
				nextZip["pattern"] = detectedPattern
				changed = true
			}
			if changed {
				return &setupFinding{Code: "handoff_zip_config_stale", Status: "error", Path: configPath, Message: "configured zip artifact selector does not match the available handoff artifact", Action: "update zip.artifacts_dir and zip.pattern to the detected artifact", Repairable: true}, next
			}
		}
	}

	makeTarget := firstNonEmpty(mapString(zipConfig, "make_target"), "codebasezip")
	if makefileHasTarget(filepath.Join(repo, "Makefile"), makeTarget) {
		return nil, config
	}
	return &setupFinding{Code: "handoff_zip_unavailable", Status: "error", Path: configuredRoot, Message: "no matching handoff zip or configured build target is available", Action: "add the configured Make target or pass an explicit --zip artifact", Repairable: false}, config
}

func handoffZipExists(root, pattern string) bool {
	_, ok := newestZipMatching(root, pattern)
	return ok
}

func newestZip(root string) (string, bool) { return newestZipMatching(root, "") }

func newestZipMatching(root, pattern string) (string, bool) {
	newest := ""
	var newestMod int64
	_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".zip") || !strings.Contains(entry.Name(), pattern) {
			return nil
		}
		info, statErr := entry.Info()
		if statErr == nil && (newest == "" || info.ModTime().UnixNano() > newestMod || (info.ModTime().UnixNano() == newestMod && path > newest)) {
			newest, newestMod = path, info.ModTime().UnixNano()
		}
		return nil
	})
	return newest, newest != ""
}

var (
	handoffTimestampPattern  = regexp.MustCompile(`^(.+?)(\d{8}[-_]\d{6})(.*)\.zip$`)
	handoffRepoPrefixPattern = regexp.MustCompile(`^[A-Za-z0-9_.]+-`)
)

func inferHandoffZipPattern(name string) string {
	for _, marker := range []string{"-codebase-", "-source-review-"} {
		if strings.Contains(name, marker) {
			return marker
		}
	}
	if match := handoffTimestampPattern.FindStringSubmatch(name); len(match) == 4 {
		if generic := handoffRepoPrefixPattern.ReplaceAllString(match[1], "-"); generic != "" {
			return generic
		}
		return match[3]
	}
	return ""
}

func makefileHasTarget(path, target string) bool {
	raw, err := os.ReadFile(path)
	if err != nil || target == "" {
		return false
	}
	needle := []byte("\n" + target + ":")
	return bytes.HasPrefix(raw, []byte(target+":")) || bytes.Contains(raw, needle)
}

func cloneStringMap(values map[string]any) map[string]any {
	raw, _ := json.Marshal(values)
	var clone map[string]any
	_ = json.Unmarshal(raw, &clone)
	return clone
}

func diagnoseHandoffWorkflows(config map[string]any, configPath string, inspect func(system, workflow string) ([]byte, error)) []setupFinding {
	rzn, _ := config["rzn"].(map[string]any)
	system := firstNonEmpty(mapString(rzn, "system"), "chatgpt")
	roles := []struct {
		name     string
		workflow string
		required []string
	}{
		{name: "send", workflow: firstNonEmpty(mapString(rzn, "send_workflow"), "send"), required: []string{"project_id", "message_text", "model_slug", "model_effort"}},
		{name: "read", workflow: firstNonEmpty(mapString(rzn, "read_workflow"), "read"), required: []string{"chat_id", "download_attachments", "attachments_scroll"}},
		{name: "projects", workflow: firstNonEmpty(mapString(rzn, "projects_workflow"), "projects")},
	}
	if boolWithDefault(rzn, "include_model_version_param", true) {
		roles[0].required = append(roles[0].required, "model_version")
	}
	if boolWithDefault(rzn, "include_require_exact_model_param", true) {
		roles[0].required = append(roles[0].required, "require_exact_model")
	}

	findings := []setupFinding{}
	for _, role := range roles {
		raw, err := loadHandoffWorkflow(rzn, filepath.Dir(configPath), system, role.workflow, inspect)
		if err != nil {
			findings = append(findings, setupFinding{Code: "handoff_workflow_" + role.name + "_missing", Status: "error", Path: configPath, Message: fmt.Sprintf("cannot inspect configured %s workflow %s/%s: %v", role.name, system, role.workflow, err), Action: "refresh the installed rzn-browser workflow catalog", Repairable: false})
			continue
		}
		contract, err := parseHandoffWorkflowContract(raw)
		expectedID := system + "/" + role.workflow
		expectedCapability := system + "." + role.name
		missing := []string{}
		for _, required := range role.required {
			if !contract.Inputs[required] {
				missing = append(missing, required)
			}
		}
		identityOK := handoffWorkflowIdentityMatches(contract, expectedID, system, expectedCapability)
		if err != nil || !identityOK || len(missing) > 0 {
			detail := firstNonEmpty(errorString(err), fmt.Sprintf("reference=%q id=%q system=%q capability=%q missing_inputs=%s", contract.Reference, contract.ID, contract.System, contract.Capability, strings.Join(missing, ",")))
			findings = append(findings, setupFinding{Code: "handoff_workflow_" + role.name + "_stale", Status: "error", Path: configPath, Message: fmt.Sprintf("configured %s workflow contract is stale: %s", role.name, detail), Action: "refresh the installed rzn-browser workflow catalog", Repairable: false})
		}
	}
	return findings
}

func loadHandoffWorkflow(rzn map[string]any, configDir, system, workflow string, inspect func(system, workflow string) ([]byte, error)) ([]byte, error) {
	if dir := mapString(rzn, "workflows_dir"); dir != "" {
		if !filepath.IsAbs(dir) {
			dir = filepath.Join(configDir, dir)
		}
		for _, candidate := range []string{
			filepath.Join(dir, system, system+"_"+workflow+".json"),
			filepath.Join(dir, system, workflow+".json"),
			filepath.Join(dir, system+"_"+workflow+".json"),
		} {
			if raw, err := os.ReadFile(candidate); err == nil {
				return raw, nil
			}
		}
		return nil, fmt.Errorf("workflow manifest not found under %s", dir)
	}
	if inspect == nil {
		return nil, fmt.Errorf("rzn-browser workflow inspector unavailable")
	}
	return inspect(system, workflow)
}

type handoffWorkflowContract struct {
	Reference  string
	ID         string
	System     string
	Capability string
	Inputs     map[string]bool
}

func parseHandoffWorkflowContract(raw []byte) (handoffWorkflowContract, error) {
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return handoffWorkflowContract{}, err
	}
	contract := handoffWorkflowContract{
		Reference:  mapString(payload, "reference"),
		ID:         mapString(payload, "id"),
		System:     mapString(payload, "system"),
		Capability: mapString(payload, "capability"),
		Inputs:     map[string]bool{},
	}
	if list, ok := payload["inputs"].([]any); ok {
		for _, item := range list {
			if value, ok := item.(map[string]any); ok {
				contract.Inputs[mapString(value, "name")] = true
			}
		}
	}
	if params, ok := payload["params"].(map[string]any); ok {
		if properties, ok := params["properties"].(map[string]any); ok {
			for name := range properties {
				contract.Inputs[name] = true
			}
		}
	}
	if contract.ID == "" {
		return contract, fmt.Errorf("workflow contract has no id")
	}
	return contract, nil
}

func handoffWorkflowIdentityMatches(contract handoffWorkflowContract, expectedReference, expectedSystem, expectedCapability string) bool {
	if contract.System != expectedSystem || contract.Capability != expectedCapability {
		return false
	}
	if contract.Reference == "" {
		return contract.ID == expectedReference
	}
	if contract.Reference != expectedReference {
		return false
	}
	return contract.ID == expectedReference || strings.HasPrefix(contract.ID, expectedReference+"_")
}

func boolWithDefault(values map[string]any, key string, fallback bool) bool {
	value, ok := values[key].(bool)
	if !ok {
		return fallback
	}
	return value
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func inspectRZNWorkflow(system, workflow string) ([]byte, error) {
	return exec.Command("rzn-browser", "workflow", "inspect", system, workflow, "--json").CombinedOutput()
}

func pathExistsIncludingSymlink(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func mapString(values map[string]any, key string) string {
	value, _ := values[key].(string)
	return strings.TrimSpace(value)
}

func printSetupHelp() {
	fmt.Println(`Usage:
  tusker setup doctor [--repo <path>] [--state-root <path>] [--source <canonical-tusker-checkout>] [--json]
  tusker setup repair [--repo <path>] [--state-root <path>] [--source <canonical-tusker-checkout>] [--dry-run] [--json]

Purpose:
  Diagnose onboarding drift across registered vault roots, WORKFLOW.md paths,
  Tusker binaries and generated skills, plus offline ChatGPT handoff routing,
  zip artifact matching, and installed rzn-browser workflow contracts. Doctor never writes.
  Repair applies only deterministic local fixes and is idempotent; credentials,
  project routing, and browser workflow refreshes remain explicit actions.`)
}
