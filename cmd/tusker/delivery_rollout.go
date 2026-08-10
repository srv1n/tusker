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

type deliveryRolloutRepairScope string

const (
	deliveryRolloutRepairCore         deliveryRolloutRepairScope = "core"
	deliveryRolloutRepairAutomation   deliveryRolloutRepairScope = "automation"
	deliveryRolloutRepairService      deliveryRolloutRepairScope = "service"
	deliveryRolloutRepairIntegrations deliveryRolloutRepairScope = "integrations"
)

type deliveryRolloutInput struct {
	Store           *RuntimeStore
	Source          string
	ExecutablePath  string
	InstalledPath   string
	WorkflowInspect func(system, workflow string) ([]byte, error)
	ServiceCheck    func(apply bool) ([]setupFinding, error)
}

type deliveryRolloutProject struct {
	ProjectID string            `json:"project_id"`
	RepoRoot  string            `json:"repo_root"`
	Status    string            `json:"status"`
	Action    string            `json:"action,omitempty"`
	Findings  []setupFinding    `json:"findings"`
	Readiness ReadinessContract `json:"readiness"`
}

type deliveryRolloutReport struct {
	Schema           string                     `json:"schema"`
	DryRun           bool                       `json:"dry_run"`
	RepairScope      deliveryRolloutRepairScope `json:"repair_scope"`
	OK               bool                       `json:"ok"`
	Projects         []deliveryRolloutProject   `json:"projects"`
	Service          []setupFinding             `json:"service"`
	ServiceReadiness ReadinessContract          `json:"service_readiness"`
}

func deliveryRolloutCmd(args Args) error {
	action := strings.ToLower(strings.TrimSpace(args.String("_pos0")))
	if action == "" || action == "help" || args.Bool("help") {
		fmt.Println("Usage:\n  tusker delivery rollout doctor [--state-root <path>] [--source <canonical-checkout>] [--json]\n  tusker delivery rollout repair [--scope core|automation|service|integrations] [--state-root <path>] [--source <canonical-checkout>] [--dry-run] [--json]")
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
	scope, err := deliveryRolloutScope(args.String("scope"))
	if err != nil {
		return err
	}
	if apply {
		if err := rejectAgentSpawn("tusker delivery rollout repair"); err != nil {
			return err
		}
	}
	report, err := runDeliveryRolloutScoped(deliveryRolloutInput{Store: store, Source: args.String("source"), WorkflowInspect: inspectRZNWorkflow}, scope, apply)
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

func deliveryRolloutScope(value string) (deliveryRolloutRepairScope, error) {
	scope := deliveryRolloutRepairScope(strings.ToLower(strings.TrimSpace(value)))
	if scope == "" {
		return deliveryRolloutRepairCore, nil
	}
	switch scope {
	case deliveryRolloutRepairCore, deliveryRolloutRepairAutomation, deliveryRolloutRepairService, deliveryRolloutRepairIntegrations:
		return scope, nil
	default:
		return "", tuskerError(errorInvalidArg, "delivery rollout repair scope must be core, automation, service, or integrations")
	}
}

func runDeliveryRollout(input deliveryRolloutInput, apply bool) (deliveryRolloutReport, error) {
	return runDeliveryRolloutScoped(input, deliveryRolloutRepairCore, apply)
}

func runDeliveryRolloutScoped(input deliveryRolloutInput, scope deliveryRolloutRepairScope, apply bool) (deliveryRolloutReport, error) {
	report := deliveryRolloutReport{Schema: deliveryRolloutSchema, DryRun: !apply, RepairScope: scope, OK: true, Projects: []deliveryRolloutProject{}, Service: []setupFinding{}}
	if input.Store == nil {
		return report, tuskerError(errorConfigInvalid, "delivery rollout requires a runtime project registry")
	}
	loaded, err := loadRegisteredProjects(input.Store, registeredProjectLoadOptions{MetadataOnly: true, LoadDisabled: true})
	if err != nil {
		return report, err
	}
	projects := make([]RegisteredProject, 0, len(loaded))
	for _, item := range loaded {
		projects = append(projects, item.Project)
	}
	sort.Slice(projects, func(i, j int) bool { return projects[i].ProjectID < projects[j].ProjectID })
	for _, project := range projects {
		item := deliveryRolloutProject{ProjectID: project.ProjectID, RepoRoot: project.RepoRoot, Status: "healthy", Findings: []setupFinding{}}
		if reason, action := deliveryRolloutCompatibility(project); reason != "" {
			item.Status, item.Action = "quarantined", action
			item.Findings = append(item.Findings, setupFinding{Code: "project_incompatible", Status: "error", Path: project.RepoRoot, Message: reason, Action: action})
			item.Readiness = deliveryRolloutReadiness(project, reason, item.Findings)
			report.OK = false
			if apply {
				project.Health = projectHealthError
				if project.LastError == "" || strings.HasPrefix(project.LastError, "delivery rollout quarantine:") {
					project.LastError = "delivery rollout quarantine: " + reason + "; action: " + action
				}
				if err := input.Store.UpsertProject(project); err != nil {
					return report, err
				}
			}
			report.Projects = append(report.Projects, item)
			continue
		}

		doctor, err := runSetupDoctor(setupDoctorInput{RepoRoot: project.RepoRoot, Store: input.Store, Source: input.Source, ExecutablePath: input.ExecutablePath, InstalledPath: input.InstalledPath, WorkflowInspect: input.WorkflowInspect, SuppressHandoffRepair: scope != deliveryRolloutRepairIntegrations, RepairScope: string(scope)}, apply)
		if err != nil {
			return report, err
		}
		item.Findings = append(item.Findings, doctor.Findings...)
		workflowFindings, err := deliveryRolloutWorkflowPolicy(project, apply && scope == deliveryRolloutRepairAutomation)
		if err != nil {
			item.Status, item.Action = "quarantined", "repair the workflow/config syntax, then rerun delivery rollout repair"
			item.Findings = append(item.Findings, setupFinding{Code: "workflow_policy_invalid", Status: "error", Path: project.WorkflowPath, Message: err.Error(), Action: item.Action})
		} else {
			item.Findings = append(item.Findings, workflowFindings...)
			if apply && deliveryRolloutChanged(item.Findings) {
				item.Status = "repaired"
			}
		}
		item.Readiness = deliveryRolloutReadiness(project, "", item.Findings)
		if item.Readiness.Dimensions.Contract.State != ReadinessStateReady {
			item.Status = "quarantined"
			item.Action = firstNonEmpty(item.Action, deliveryRolloutFirstError(item.Findings), "repair core compatibility before retrying rollout")
		} else if !project.Enabled {
			item.Status = "disabled"
		} else if item.Readiness.Dimensions.Automation.State != ReadinessStateReady && item.Readiness.Dimensions.Automation.State != ReadinessStateNotApplicable {
			item.Status = "needs_repair"
		}
		if item.Status == "quarantined" {
			report.OK = false
			if apply && project.LastError == "" {
				project.Health = projectHealthError
				project.LastError = "delivery rollout quarantine: " + firstNonEmpty(deliveryRolloutFirstError(item.Findings), "incompatible unattended delivery configuration") + "; action: " + item.Action
				if err := input.Store.UpsertProject(project); err != nil {
					return report, err
				}
			}
		} else if item.Status == "needs_repair" || deliveryRolloutHasOutstandingFindings(item.Findings) || deliveryRolloutReadinessHasBlocker(item.Readiness) {
			report.OK = false
		}
		if apply && item.Status != "quarantined" && strings.HasPrefix(project.LastError, "delivery rollout quarantine:") {
			project.LastError = ""
			if project.Enabled {
				project.Health = projectHealthHealthy
			} else {
				project.Health = projectHealthDisabled
			}
			if err := input.Store.UpsertProject(project); err != nil {
				return report, err
			}
		}
		report.Projects = append(report.Projects, item)
	}
	serviceApply := apply && scope == deliveryRolloutRepairService
	if input.ServiceCheck != nil {
		report.Service, err = input.ServiceCheck(serviceApply)
		if err != nil {
			return report, err
		}
	} else if serviceApply {
		report.Service, err = deliveryRolloutServiceDefinitionRepair()
		if err != nil {
			return report, err
		}
	} else {
		report.Service, err = deliveryRolloutServiceDoctor()
		if err != nil {
			return report, err
		}
	}
	for _, finding := range report.Service {
		if finding.Status == "error" && !finding.Changed {
			report.OK = false
		}
	}
	report.ServiceReadiness = deliveryRolloutServiceReadiness(report.Service)
	return report, nil
}

func deliveryRolloutFirstError(findings []setupFinding) string {
	for _, finding := range findings {
		if finding.Status == "error" && !finding.Changed {
			return finding.Message
		}
	}
	return ""
}

func deliveryRolloutHasOutstandingFindings(findings []setupFinding) bool {
	for _, finding := range findings {
		if finding.Status == "error" && !finding.Changed {
			return true
		}
	}
	return false
}

func deliveryRolloutReadinessHasBlocker(contract ReadinessContract) bool {
	for _, dimension := range readinessDimensionsByKind(contract.Dimensions) {
		if dimension.State == ReadinessStateBlocked || dimension.State == ReadinessStateWaiting || dimension.State == ReadinessStateUnavailable {
			return true
		}
	}
	return false
}

func deliveryRolloutReadiness(project RegisteredProject, compatibility string, findings []setupFinding) ReadinessContract {
	projectID := deliveryRolloutProjectID(project)
	provenance := ReadinessProvenance{Source: "delivery_rollout", Revision: projectID}
	dimensions := ReadinessDimensions{
		Contract:            ReadinessDimension{State: ReadinessStateReady, Provenance: provenance},
		Import:              ReadinessDimension{State: ReadinessStateNotApplicable, Provenance: provenance},
		Interactive:         ReadinessDimension{State: ReadinessStateReady, Provenance: provenance},
		Automation:          ReadinessDimension{State: ReadinessStateReady, Provenance: provenance},
		Authorization:       ReadinessDimension{State: ReadinessStateNotApplicable, Provenance: provenance},
		Runtime:             ReadinessDimension{State: ReadinessStateNotApplicable, Provenance: provenance},
		OptionalIntegration: ReadinessDimension{State: ReadinessStateReady, Provenance: provenance},
	}
	blockers := []ReadinessBlocker{}
	add := func(kind ReadinessBlockerKind, authority ReadinessAuthorityDomain, affects []ReadinessDimensionKind, id, reason, remedy string) {
		blockers = append(blockers, ReadinessBlocker{ID: id, Kind: kind, Authority: authority, Affects: affects, ProjectID: projectID, Reason: deliveryRolloutBoundedReadinessText(reason), Remedy: deliveryRolloutBoundedReadinessText(remedy)})
		for _, dimension := range affects {
			switch dimension {
			case ReadinessDimensionContract:
				dimensions.Contract.State = ReadinessStateBlocked
			case ReadinessDimensionInteractive:
				dimensions.Interactive.State = ReadinessStateBlocked
			case ReadinessDimensionAutomation:
				dimensions.Automation.State = ReadinessStateBlocked
			case ReadinessDimensionAuthorization:
				dimensions.Authorization.State = ReadinessStateBlocked
			case ReadinessDimensionRuntime:
				dimensions.Runtime.State = ReadinessStateUnavailable
			case ReadinessDimensionOptionalIntegration:
				dimensions.OptionalIntegration.State = ReadinessStateBlocked
			}
		}
	}
	if compatibility != "" {
		add(ReadinessBlockerContractInvalid, ReadinessAuthorityContract, []ReadinessDimensionKind{ReadinessDimensionContract, ReadinessDimensionInteractive}, "core-compatibility", compatibility, "repair the registered vault and supported core schema")
	}
	for index, finding := range findings {
		if finding.Changed || (finding.Status != "error" && !strings.HasPrefix(finding.Code, "handoff_")) {
			continue
		}
		id := fmt.Sprintf("finding-%03d-%s", index, finding.Code)
		switch {
		case deliveryRolloutCoreFinding(finding):
			add(ReadinessBlockerContractInvalid, ReadinessAuthorityContract, []ReadinessDimensionKind{ReadinessDimensionContract, ReadinessDimensionInteractive}, id, finding.Message, firstNonEmpty(finding.Action, "repair core compatibility"))
		case deliveryRolloutIntegrationFinding(finding):
			add(ReadinessBlockerOptionalIntegrationMissing, ReadinessAuthorityIntegration, []ReadinessDimensionKind{ReadinessDimensionOptionalIntegration}, id, finding.Message, firstNonEmpty(finding.Action, "repair the optional integration"))
		default:
			add(ReadinessBlockerAutomationDisabled, ReadinessAuthorityAutomation, []ReadinessDimensionKind{ReadinessDimensionAutomation}, id, finding.Message, firstNonEmpty(finding.Action, "repair automation configuration"))
		}
	}
	if !project.Enabled {
		add(ReadinessBlockerAutomationDisabled, ReadinessAuthorityAutomation, []ReadinessDimensionKind{ReadinessDimensionAutomation}, "automation-disabled", "project registration is disabled for unattended automation", "enable project automation only through its explicit control")
	}
	deliveryRolloutProjectRuntime(&dimensions, project, add)
	if index, err := loadV7Index(project.VaultRoot); err != nil {
		add(ReadinessBlockerAuthorizationMissing, ReadinessAuthorityAuthorization, []ReadinessDimensionKind{ReadinessDimensionAuthorization}, "wave-index", "wave authorization cannot be inspected: "+err.Error(), "repair the local wave records before unattended execution")
	} else {
		waveIDs := make([]string, 0, len(index.Waves))
		for waveID := range index.Waves {
			waveIDs = append(waveIDs, waveID)
		}
		sort.Strings(waveIDs)
		for _, waveID := range waveIDs {
			wave := index.Waves[waveID]
			state := fallback(stringField(wave.Data, "authorization"), "disarmed")
			if state != "armed" {
				blocker := ReadinessBlocker{ID: "wave-" + waveID, Kind: ReadinessBlockerAuthorizationMissing, Authority: ReadinessAuthorityAuthorization, Affects: []ReadinessDimensionKind{ReadinessDimensionAuthorization}, ProjectID: projectID, WaveID: waveID, Reason: deliveryRolloutBoundedReadinessText("wave authorization is " + state), Remedy: "arm the exact reviewed wave through the approved control"}
				blockers = append(blockers, blocker)
				dimensions.Authorization.State = ReadinessStateBlocked
			}
		}
		if len(index.Waves) > 0 && dimensions.Authorization.State == ReadinessStateNotApplicable {
			dimensions.Authorization.State = ReadinessStateReady
		}
	}
	contract, err := NewReadinessContract(ReadinessInput{Dimensions: dimensions, Blockers: blockers})
	if err != nil {
		return deliveryRolloutReadinessFallback(projectID, provenance, err)
	}
	return contract
}

func deliveryRolloutCoreFinding(finding setupFinding) bool {
	return finding.Code == "project_incompatible" || finding.Code == "stale_vault_root" || finding.Code == "workflow_path_mismatch" || finding.Code == "workflow_policy_invalid" || finding.Code == "broken_vault_symlink" || strings.HasPrefix(finding.Code, "skill_install_")
}

func deliveryRolloutIntegrationFinding(finding setupFinding) bool {
	return strings.HasPrefix(finding.Code, "handoff_")
}

func deliveryRolloutProjectRuntime(dimensions *ReadinessDimensions, project RegisteredProject, add func(ReadinessBlockerKind, ReadinessAuthorityDomain, []ReadinessDimensionKind, string, string, string)) {
	if !project.Enabled || project.Health == projectHealthDisabled {
		dimensions.Runtime.State = ReadinessStateNotApplicable
		return
	}
	dimensions.Runtime.State = ReadinessStateReady
	if project.Health != "" && project.Health != projectHealthHealthy {
		add(ReadinessBlockerRuntimeUnavailable, ReadinessAuthorityRuntime, []ReadinessDimensionKind{ReadinessDimensionRuntime}, "project-health", "project runtime health is "+string(project.Health)+": "+firstNonEmpty(project.LastError, "no further detail recorded"), "restore project runtime health before unattended execution")
		return
	}
	if strings.TrimSpace(project.LastPollAt) == "" {
		return
	}
	lastPoll, err := time.Parse(time.RFC3339, project.LastPollAt)
	if err != nil || lastPoll.Before(time.Now().UTC().Add(-daemonHeartbeatDeadThreshold)) {
		reason := "project runtime poll is stale"
		if err != nil {
			reason = "project runtime poll timestamp is invalid"
		}
		add(ReadinessBlockerRuntimeUnavailable, ReadinessAuthorityRuntime, []ReadinessDimensionKind{ReadinessDimensionRuntime}, "project-runtime", reason, "restore managed daemon reconciliation before unattended execution")
	}
}

func deliveryRolloutProjectID(project RegisteredProject) string {
	return firstNonEmpty(strings.TrimSpace(project.ProjectID), strings.TrimSpace(project.ProjectKey), "managed-project")
}

func deliveryRolloutBoundedReadinessText(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "No additional detail was recorded."
	}
	if len(value) <= 320 {
		return value
	}
	return strings.TrimSpace(value[:317]) + "..."
}

func deliveryRolloutReadinessFallback(projectID string, provenance ReadinessProvenance, cause error) ReadinessContract {
	dimensions := ReadinessDimensions{
		Contract:            ReadinessDimension{State: ReadinessStateBlocked, Provenance: provenance},
		Import:              ReadinessDimension{State: ReadinessStateNotApplicable, Provenance: provenance},
		Interactive:         ReadinessDimension{State: ReadinessStateBlocked, Provenance: provenance},
		Automation:          ReadinessDimension{State: ReadinessStateNotApplicable, Provenance: provenance},
		Authorization:       ReadinessDimension{State: ReadinessStateNotApplicable, Provenance: provenance},
		Runtime:             ReadinessDimension{State: ReadinessStateNotApplicable, Provenance: provenance},
		OptionalIntegration: ReadinessDimension{State: ReadinessStateNotApplicable, Provenance: provenance},
	}
	return ReadinessContract{Schema: ReadinessContractSchema, Version: ReadinessContractVersion, Dimensions: dimensions, Blockers: []ReadinessBlocker{{ID: "readiness-projection", Kind: ReadinessBlockerContractInvalid, Authority: ReadinessAuthorityContract, Affects: []ReadinessDimensionKind{ReadinessDimensionContract, ReadinessDimensionInteractive}, ProjectID: projectID, Reason: deliveryRolloutBoundedReadinessText("rollout readiness projection is invalid: " + cause.Error()), Remedy: "repair the reported core compatibility data and rerun rollout doctor"}}}
}

func deliveryRolloutServiceReadiness(findings []setupFinding) ReadinessContract {
	provenance := ReadinessProvenance{Source: "delivery_rollout_service", Revision: "managed-service"}
	dimensions := ReadinessDimensions{
		Contract:            ReadinessDimension{State: ReadinessStateNotApplicable, Provenance: provenance},
		Import:              ReadinessDimension{State: ReadinessStateNotApplicable, Provenance: provenance},
		Interactive:         ReadinessDimension{State: ReadinessStateNotApplicable, Provenance: provenance},
		Automation:          ReadinessDimension{State: ReadinessStateNotApplicable, Provenance: provenance},
		Authorization:       ReadinessDimension{State: ReadinessStateNotApplicable, Provenance: provenance},
		Runtime:             ReadinessDimension{State: ReadinessStateReady, Provenance: provenance},
		OptionalIntegration: ReadinessDimension{State: ReadinessStateNotApplicable, Provenance: provenance},
	}
	blockers := []ReadinessBlocker{}
	for index, finding := range findings {
		if finding.Status != "error" || finding.Changed {
			continue
		}
		dimensions.Runtime.State = ReadinessStateUnavailable
		blockers = append(blockers, ReadinessBlocker{ID: fmt.Sprintf("service-%03d-%s", index, finding.Code), Kind: ReadinessBlockerRuntimeUnavailable, Authority: ReadinessAuthorityRuntime, Affects: []ReadinessDimensionKind{ReadinessDimensionRuntime}, ProjectID: "managed-service", Reason: deliveryRolloutBoundedReadinessText(finding.Message), Remedy: deliveryRolloutBoundedReadinessText(firstNonEmpty(finding.Action, "repair the managed service"))})
	}
	contract, err := NewReadinessContract(ReadinessInput{Dimensions: dimensions, Blockers: blockers})
	if err != nil {
		return deliveryRolloutServiceReadinessFallback(provenance, err)
	}
	return contract
}

func deliveryRolloutServiceReadinessFallback(provenance ReadinessProvenance, cause error) ReadinessContract {
	dimensions := ReadinessDimensions{
		Contract:            ReadinessDimension{State: ReadinessStateNotApplicable, Provenance: provenance},
		Import:              ReadinessDimension{State: ReadinessStateNotApplicable, Provenance: provenance},
		Interactive:         ReadinessDimension{State: ReadinessStateNotApplicable, Provenance: provenance},
		Automation:          ReadinessDimension{State: ReadinessStateNotApplicable, Provenance: provenance},
		Authorization:       ReadinessDimension{State: ReadinessStateNotApplicable, Provenance: provenance},
		Runtime:             ReadinessDimension{State: ReadinessStateUnavailable, Provenance: provenance},
		OptionalIntegration: ReadinessDimension{State: ReadinessStateNotApplicable, Provenance: provenance},
	}
	return ReadinessContract{Schema: ReadinessContractSchema, Version: ReadinessContractVersion, Dimensions: dimensions, Blockers: []ReadinessBlocker{{ID: "service-readiness-projection", Kind: ReadinessBlockerRuntimeUnavailable, Authority: ReadinessAuthorityRuntime, Affects: []ReadinessDimensionKind{ReadinessDimensionRuntime}, ProjectID: "managed-service", Reason: deliveryRolloutBoundedReadinessText("managed service readiness projection is invalid: " + cause.Error()), Remedy: "repair the reported managed service data and rerun rollout doctor"}}}
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
	if opaque := deliveryOpaqueRunnerCommands(project.WorkflowPath); len(opaque) > 0 {
		findings = append(findings, setupFinding{Code: "runner_harness_opaque", Status: "error", Path: project.WorkflowPath, Message: "unattended runner command is not a canonical enforceable harness: " + strings.Join(opaque, ", "), Action: "replace the opaque wrapper with canonical codex exec or Claude bypassPermissions command", Repairable: false})
	}
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
	configPath := preferredTuskerConfigPath(project.VaultRoot)
	if opaque := deliveryOpaqueRunnerCommands(configPath); len(opaque) > 0 {
		findings = append(findings, setupFinding{Code: "runner_harness_opaque", Status: "error", Path: configPath, Message: "unattended runner command is not a canonical enforceable harness: " + strings.Join(opaque, ", "), Action: "replace the opaque wrapper with canonical codex exec or Claude bypassPermissions command", Repairable: false})
	}
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

func deliveryOpaqueRunnerCommands(path string) []string {
	if !fileExists(path) {
		return nil
	}
	text, err := readText(path)
	if err != nil {
		return []string{"unreadable command configuration"}
	}
	var data map[string]any
	if strings.HasSuffix(path, "WORKFLOW.md") {
		data, _, err = parseFrontmatter(text)
	} else {
		err = yaml.Unmarshal([]byte(text), &data)
	}
	if err != nil {
		return nil // syntax compatibility reports the actionable error elsewhere
	}
	var opaque []string
	check := func(label string, entry map[string]any) {
		kind := strings.ToLower(firstNonEmpty(stringField(entry, "kind"), stringField(entry, "harness")))
		command := strings.TrimSpace(stringField(entry, "command"))
		if command == "" {
			return
		}
		valid := true
		switch kind {
		case "codex", "codex_exec", "codex_app_server":
			valid = strings.HasPrefix(command, "codex exec ") || command == "codex exec"
		case "claude", "claude-code":
			valid = strings.HasPrefix(command, "claude -p ") && strings.Contains(command, "bypassPermissions")
		}
		if !valid {
			opaque = append(opaque, label)
		}
	}
	for _, root := range []string{"runners", "runner_profiles"} {
		if entries, ok := data[root].(map[string]any); ok {
			for name, raw := range entries {
				if entry, ok := raw.(map[string]any); ok {
					check(root+"."+name, entry)
				}
			}
		}
	}
	if automation, ok := data["automation"].(map[string]any); ok {
		for _, root := range []string{"runners", "profiles"} {
			if entries, ok := automation[root].(map[string]any); ok {
				for name, raw := range entries {
					if entry, ok := raw.(map[string]any); ok {
						check("automation."+root+"."+name, entry)
					}
				}
			}
		}
	}
	sort.Strings(opaque)
	return opaque
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

func deliveryRolloutServiceDoctor() ([]setupFinding, error) {
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
	return []setupFinding{finding}, nil
}

// deliveryRolloutServiceDefinitionRepair is deliberately file-only. It never
// talks to launchd, loads a service, or starts a daemon; runtime recovery stays
// a separate explicit service operation.
func deliveryRolloutServiceDefinitionRepair() ([]setupFinding, error) {
	if daemonServiceGOOS != "darwin" {
		return []setupFinding{{Code: "daemon_service_unsupported", Status: "warning", Message: "managed launchd definition repair is available only on macOS", Action: "use the platform service manager for tusker daemon run", Repairable: false}}, nil
	}
	config, err := currentDaemonServiceConfig()
	if err != nil {
		return []setupFinding{{Code: "daemon_service_unavailable", Status: "error", Message: err.Error(), Action: "repair the managed daemon service definition", Repairable: false}}, nil
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
	if plistCurrent && binaryCurrent {
		return []setupFinding{{Code: "daemon_service_runtime_unchecked", Status: "warning", Path: config.plistPath(), Message: "managed service definition is current; runtime was not inspected or started by repair", Action: "run tusker daemon service status, then explicitly start the service if needed", Repairable: false}}, nil
	}
	if err := ensureDir(config.LaunchAgentDir); err != nil {
		return nil, fmt.Errorf("create LaunchAgents directory: %w", err)
	}
	if err := ensureDir(config.logDir()); err != nil {
		return nil, fmt.Errorf("create daemon service log directory: %w", err)
	}
	if err := installDaemonServiceExecutable(config); err != nil {
		return nil, err
	}
	if err := writeDaemonServicePlist(config.plistPath(), renderDaemonServicePlist(config)); err != nil {
		return nil, err
	}
	return []setupFinding{{Code: "daemon_service_definition_drift", Status: "error", Path: config.plistPath(), Message: "managed daemon binary/plist definition was refreshed without loading or starting the service", Action: "run tusker daemon service status, then explicitly start the service if needed", Repairable: true, Changed: true}}, nil
}
