package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const deliveryRolloutSchema = "tusker.delivery-rollout/v1"

type deliveryRolloutInput struct {
	Store           *RuntimeStore
	Source          string
	ExecutablePath  string
	InstalledPath   string
	WorkflowInspect func(system, workflow string) ([]byte, error)
	ServiceCheck    func(apply bool) ([]setupFinding, error)
}

type deliveryRolloutProject struct {
	ProjectID string         `json:"project_id"`
	RepoRoot  string         `json:"repo_root"`
	Status    string         `json:"status"`
	Action    string         `json:"action,omitempty"`
	Findings  []setupFinding `json:"findings"`
}

type deliveryRolloutReport struct {
	Schema   string                   `json:"schema"`
	DryRun   bool                     `json:"dry_run"`
	OK       bool                     `json:"ok"`
	Projects []deliveryRolloutProject `json:"projects"`
	Service  []setupFinding           `json:"service"`
}

func deliveryRolloutCmd(args Args) error {
	action := strings.ToLower(strings.TrimSpace(args.String("_pos0")))
	if action == "" || action == "help" || args.Bool("help") {
		fmt.Println("Usage:\n  tusker delivery rollout doctor [--state-root <path>] [--source <canonical-checkout>] [--json]\n  tusker delivery rollout repair [--state-root <path>] [--source <canonical-checkout>] [--dry-run] [--json]")
		return nil
	}
	if action != "doctor" && action != "repair" {
		return tuskerError(errorInvalidArg, "delivery rollout action must be doctor or repair")
	}
	store, err := OpenRuntimeStore(firstNonEmpty(strings.TrimSpace(args.String("state-root")), DefaultStateRoot()))
	if err != nil {
		return err
	}
	defer store.Close()
	apply := action == "repair" && !args.Bool("dry-run")
	if apply {
		if err := rejectAgentSpawn("tusker delivery rollout repair"); err != nil {
			return err
		}
	}
	report, err := runDeliveryRollout(deliveryRolloutInput{Store: store, Source: args.String("source"), WorkflowInspect: inspectRZNWorkflow}, apply)
	if err != nil {
		return err
	}
	if args.Bool("json") {
		emitJSON(report)
		return nil
	}
	fmt.Printf("Delivery rollout: %d project(s), %d service finding(s), ok=%v\n", len(report.Projects), len(report.Service), report.OK)
	for _, project := range report.Projects {
		fmt.Printf("  %-12s %-11s %d finding(s)\n", project.ProjectID, project.Status, len(project.Findings))
		if project.Action != "" {
			fmt.Printf("    action: %s\n", project.Action)
		}
	}
	return nil
}

func runDeliveryRollout(input deliveryRolloutInput, apply bool) (deliveryRolloutReport, error) {
	report := deliveryRolloutReport{Schema: deliveryRolloutSchema, DryRun: !apply, OK: true, Projects: []deliveryRolloutProject{}, Service: []setupFinding{}}
	if input.Store == nil {
		return report, tuskerError(errorConfigInvalid, "delivery rollout requires a runtime project registry")
	}
	projects, err := input.Store.ListProjects()
	if err != nil {
		return report, err
	}
	sort.Slice(projects, func(i, j int) bool { return projects[i].ProjectID < projects[j].ProjectID })
	for _, project := range projects {
		item := deliveryRolloutProject{ProjectID: project.ProjectID, RepoRoot: project.RepoRoot, Status: "healthy", Findings: []setupFinding{}}
		if reason, action := deliveryRolloutCompatibility(project); reason != "" {
			item.Status, item.Action = "quarantined", action
			item.Findings = append(item.Findings, setupFinding{Code: "project_incompatible", Status: "error", Path: project.RepoRoot, Message: reason, Action: action})
			report.OK = false
			if apply {
				project.Health = projectHealthError
				project.LastError = "delivery rollout quarantine: " + reason + "; action: " + action
				if err := input.Store.UpsertProject(project); err != nil {
					return report, err
				}
			}
			report.Projects = append(report.Projects, item)
			continue
		}

		doctor, err := runSetupDoctor(setupDoctorInput{RepoRoot: project.RepoRoot, Store: input.Store, Source: input.Source, ExecutablePath: input.ExecutablePath, InstalledPath: input.InstalledPath, WorkflowInspect: input.WorkflowInspect, SuppressHandoffRepair: true}, apply)
		if err != nil {
			return report, err
		}
		item.Findings = append(item.Findings, doctor.Findings...)
		workflowFindings, err := deliveryRolloutWorkflowPolicy(project, apply)
		if err != nil {
			item.Status, item.Action = "quarantined", "repair the workflow/config syntax, then rerun delivery rollout repair"
			item.Findings = append(item.Findings, setupFinding{Code: "workflow_policy_invalid", Status: "error", Path: project.WorkflowPath, Message: err.Error(), Action: item.Action})
		} else {
			item.Findings = append(item.Findings, workflowFindings...)
			if apply && deliveryRolloutChanged(item.Findings) {
				item.Status = "repaired"
			}
		}
		for _, finding := range item.Findings {
			if finding.Status == "error" && !finding.Changed {
				if finding.Repairable {
					if item.Status == "healthy" {
						item.Status = "needs_repair"
					}
				} else {
					item.Status = "quarantined"
					item.Action = firstNonEmpty(item.Action, finding.Action)
				}
			}
		}
		if item.Status == "quarantined" {
			report.OK = false
		}
		report.Projects = append(report.Projects, item)
	}
	if input.ServiceCheck != nil {
		report.Service, err = input.ServiceCheck(apply)
		if err != nil {
			return report, err
		}
	} else {
		report.Service, err = deliveryRolloutServiceDoctor(apply)
		if err != nil {
			return report, err
		}
	}
	for _, finding := range report.Service {
		if finding.Status == "error" && !finding.Changed {
			report.OK = false
		}
	}
	return report, nil
}

func deliveryRolloutCompatibility(project RegisteredProject) (string, string) {
	if strings.TrimSpace(project.RepoRoot) == "" || !dirExists(project.RepoRoot) {
		return "registered repository is missing", "restore the checkout or remove the stale registration"
	}
	if strings.TrimSpace(project.VaultRoot) == "" || !dirExists(project.VaultRoot) {
		return "registered Tusker vault is missing", "restore <repo>/.tusker and its WORKFLOW.md"
	}
	rel, err := filepath.Rel(project.RepoRoot, project.VaultRoot)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "registered vault is outside its repository", "register the repository-local .tusker vault"
	}
	raw, err := readText(workflowPath(project.VaultRoot))
	if err != nil {
		return "workflow is incompatible: " + err.Error(), "repair .tusker/WORKFLOW.md to workflow_version 1 and tracker_schema_version 7"
	}
	data, _, err := parseFrontmatter(raw)
	if err != nil {
		return "workflow is incompatible: " + err.Error(), "repair .tusker/WORKFLOW.md to workflow_version 1 and tracker_schema_version 7"
	}
	if intField(data, "workflow_version") != 1 || intField(data, "tracker_schema_version") != 7 {
		return "unsupported workflow or tracker schema", "migrate the project to workflow_version 1 and tracker_schema_version 7"
	}
	return "", ""
}

func deliveryRolloutWorkflowPolicy(project RegisteredProject, apply bool) ([]setupFinding, error) {
	findings := []setupFinding{}
	workflowChanged, workflowText, err := migratedObjectiveWorkflow(project.WorkflowPath)
	if err != nil {
		return findings, err
	}
	workflowChanged2, workflowText, err := migratedUnattendedWorkflow(project.WorkflowPath, workflowText)
	if err != nil {
		return findings, err
	}
	workflowChanged = workflowChanged || workflowChanged2
	if workflowChanged {
		finding := setupFinding{Code: "workflow_unattended_policy", Status: "error", Path: project.WorkflowPath, Message: "workflow uses a legacy runner, routine approval policy, or risk-based human close default", Action: "migrate to codex_exec, approval_policy never, and objective reviewer close", Repairable: true}
		if apply {
			if err := writeText(project.WorkflowPath, workflowText); err != nil {
				return findings, err
			}
			finding.Changed = true
		}
		findings = append(findings, finding)
	}
	configPath := filepath.Join(project.RepoRoot, "tusker.yaml")
	configChanged, configText, err := migratedObjectiveCloseConfig(configPath)
	if err != nil {
		return findings, err
	}
	configChanged2, configText, err := migratedUnattendedConfig(configPath, configText)
	if err != nil {
		return findings, err
	}
	configChanged = configChanged || configChanged2
	if configChanged {
		finding := setupFinding{Code: "config_unattended_policy", Status: "error", Path: configPath, Message: "project config uses a legacy runner, routine approval policy, or invalid close policy", Action: "migrate the known automation and close-policy keys", Repairable: true}
		if apply {
			if err := writeText(configPath, configText); err != nil {
				return findings, err
			}
			finding.Changed = true
		}
		findings = append(findings, finding)
	}
	return findings, nil
}

func migratedUnattendedWorkflow(path, text string) (bool, string, error) {
	data, body, err := parseFrontmatter(text)
	if err != nil {
		return false, "", err
	}
	changed := migrateKnownRunnerPolicy(data)
	if !changed {
		return false, text, nil
	}
	fm, err := stringifyFrontmatter(data, nil)
	if err != nil {
		return false, "", err
	}
	return true, fm + "\n" + strings.TrimLeft(body, "\n"), nil
}

func migratedUnattendedConfig(path, text string) (bool, string, error) {
	if !fileExists(path) {
		return false, text, nil
	}
	var data map[string]any
	if err := yaml.Unmarshal([]byte(text), &data); err != nil {
		return false, "", err
	}
	if !migrateKnownRunnerPolicy(data) {
		return false, text, nil
	}
	raw, err := yaml.Marshal(data)
	return true, string(raw), err
}

func migrateKnownRunnerPolicy(data map[string]any) bool {
	changed := false
	replaceRunner := func(value any) any {
		if strings.EqualFold(strings.TrimSpace(fmt.Sprint(value)), "codex_app_server") {
			changed = true
			return "codex_exec"
		}
		return value
	}
	if agents, ok := data["agents"].(map[string]any); ok {
		agents["default"] = replaceRunner(agents["default"])
		if list, ok := agents["enabled"].([]any); ok {
			for i := range list {
				list[i] = replaceRunner(list[i])
			}
		}
	}
	if reviewer, ok := data["reviewer"].(map[string]any); ok {
		reviewer["runner"] = replaceRunner(reviewer["runner"])
	}
	if codex, ok := data["codex"].(map[string]any); ok && stringField(codex, "approval_policy") != "never" {
		codex["approval_policy"] = "never"
		changed = true
	}
	for _, rootKey := range []string{"runners", "runner_profiles"} {
		if entries, ok := data[rootKey].(map[string]any); ok {
			for _, raw := range entries {
				if item, ok := raw.(map[string]any); ok {
					for _, key := range []string{"kind", "harness"} {
						if _, exists := item[key]; exists {
							item[key] = replaceRunner(item[key])
						}
					}
					if policy, exists := item["approval_policy"]; exists && strings.TrimSpace(fmt.Sprint(policy)) != "never" {
						item["approval_policy"] = "never"
						changed = true
					}
				}
			}
		}
	}
	if automation, ok := data["automation"].(map[string]any); ok {
		for _, key := range []string{"default_runner"} {
			if _, exists := automation[key]; exists {
				automation[key] = replaceRunner(automation[key])
			}
		}
		if profiles, ok := automation["profiles"].(map[string]any); ok {
			for _, raw := range profiles {
				if profile, ok := raw.(map[string]any); ok {
					if _, exists := profile["harness"]; exists {
						profile["harness"] = replaceRunner(profile["harness"])
					}
				}
			}
		}
		if runners, ok := automation["runners"].(map[string]any); ok {
			for _, raw := range runners {
				if runner, ok := raw.(map[string]any); ok {
					if _, exists := runner["kind"]; exists {
						runner["kind"] = replaceRunner(runner["kind"])
					}
					if policy, exists := runner["approval_policy"]; exists && strings.TrimSpace(fmt.Sprint(policy)) != "never" {
						runner["approval_policy"] = "never"
						changed = true
					}
				}
			}
		}
	}
	return changed
}

func deliveryRolloutChanged(findings []setupFinding) bool {
	for _, finding := range findings {
		if finding.Changed {
			return true
		}
	}
	return false
}

func deliveryRolloutServiceDoctor(apply bool) ([]setupFinding, error) {
	if daemonServiceGOOS != "darwin" {
		return []setupFinding{{Code: "daemon_service_unsupported", Status: "warning", Message: "managed launchd repair is available only on macOS", Action: "use the platform service manager for tusker daemon run", Repairable: false}}, nil
	}
	config, err := currentDaemonServiceConfig()
	if err != nil {
		return []setupFinding{{Code: "daemon_service_unavailable", Status: "error", Message: err.Error(), Action: "repair the managed daemon service", Repairable: false}}, nil
	}
	plistCurrent := false
	if raw, readErr := os.ReadFile(config.plistPath()); readErr == nil {
		plistCurrent = string(raw) == renderDaemonServicePlist(config)
	}
	binaryCurrent := false
	if source, sourceErr := os.ReadFile(config.SourceExecutable); sourceErr == nil {
		if installed, installedErr := os.ReadFile(config.Executable); installedErr == nil {
			binaryCurrent = bytes.Equal(source, installed)
		}
	}
	loaded, loadedErr := daemonServiceLoaded(config)
	if loadedErr != nil {
		return nil, loadedErr
	}
	health, healthErr := daemonServiceHealth(config)
	if healthErr != nil && fileExists(config.Executable) {
		return nil, healthErr
	}
	healthy := healthErr == nil && health.healthyAt(time.Now().UTC())
	if plistCurrent && binaryCurrent && loaded && healthy {
		return []setupFinding{}, nil
	}
	finding := setupFinding{Code: "daemon_service_drift", Status: "error", Path: config.plistPath(), Message: fmt.Sprintf("managed daemon drift: plist_current=%t binary_current=%t loaded=%t healthy=%t", plistCurrent, binaryCurrent, loaded, healthy), Action: "refresh the canonical service binary/plist and start a freshly polling managed daemon", Repairable: true}
	if apply {
		var repairErr error
		if !plistCurrent || !binaryCurrent {
			repairErr = daemonServiceInstall(Args{"quiet": "true"}, config)
		} else {
			repairErr = daemonServiceStart(Args{"quiet": "true"}, config)
		}
		if repairErr != nil {
			return nil, repairErr
		}
		finding.Changed = true
	}
	return []setupFinding{finding}, nil
}
