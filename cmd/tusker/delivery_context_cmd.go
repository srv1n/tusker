package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	deliveryPlanningContextSchema = "tusker.delivery-context/v1"
	deliveryContextMaxDocuments   = 8
	deliveryContextMaxDomains     = 8
	deliveryContextMaxEpics       = 8
	deliveryContextMaxTasks       = 16
	deliveryContextMaxGates       = 8
	deliveryContextMaxProfiles    = 16
	deliveryContextMaxPathClues   = 32
	deliveryContextMaxResources   = 32
)

var (
	deliveryContextStateRoot         = DefaultStateRoot
	deliveryDecisionLinkRegex        = regexp.MustCompile(`\[\[([A-Z]{3}-D-[0-9]{4})(?:[|#][^\]]*)?\]\]`)
	deliverySensitiveAssignmentRegex = regexp.MustCompile(`(?i)(?:^|[\s;])(?:export\s+)?(?:--?)?[a-z0-9_-]*(?:token|password|secret|api[_-]?key|access[_-]?key|private[_-]?key|credential)[a-z0-9_-]*\s*=\s*("[^"]*"|'[^']*'|[^\s;]+)`)
	deliverySensitiveFlagRegex       = regexp.MustCompile(`(?i)(?:^|[\s;])--?[a-z0-9_-]*(?:token|password|secret|api[_-]?key|access[_-]?key|private[_-]?key|credential)[a-z0-9_-]*(?:\s*=|\s+|$)`)
	deliverySensitiveHeaderRegex     = regexp.MustCompile(`(?i)(?:authorization|proxy-authorization|x-api-key|api-key|x-auth-token)\s*:`)
	deliveryURLUserinfoRegex         = regexp.MustCompile(`(?i)[a-z][a-z0-9+.-]*://[^\s/?#@]+@`)
	deliveryUserCredentialFlagRegex  = regexp.MustCompile(`(?i)(?:^|[\s;])(?:-u|--user)(?:\s*=|\s+|$)`)
)

type deliveryContextProvenance struct {
	Kind        string `json:"kind"`
	Ref         string `json:"ref"`
	Fingerprint string `json:"fingerprint,omitempty"`
}

type deliveryContextUnknown struct {
	Kind       string                      `json:"kind"`
	Field      string                      `json:"field"`
	Reason     string                      `json:"reason"`
	Remedy     string                      `json:"remedy"`
	Provenance []deliveryContextProvenance `json:"provenance"`
}

type deliveryContextProject struct {
	ID         string                      `json:"id"`
	RepoRef    string                      `json:"repo_ref"`
	VaultRef   string                      `json:"vault_ref"`
	Provenance []deliveryContextProvenance `json:"provenance"`
}

type deliveryContextIntegrationBase struct {
	Ref        string                      `json:"ref"`
	SHA        string                      `json:"sha,omitempty"`
	Provenance []deliveryContextProvenance `json:"provenance"`
}

type deliveryContextDocument struct {
	Kind        string                      `json:"kind"`
	Ref         string                      `json:"ref"`
	Title       string                      `json:"title"`
	Status      string                      `json:"status,omitempty"`
	Fingerprint string                      `json:"fingerprint"`
	Provenance  []deliveryContextProvenance `json:"provenance"`
}

type deliveryContextEpicClue struct {
	ID         string                      `json:"id"`
	Title      string                      `json:"title"`
	Status     string                      `json:"status"`
	Reason     string                      `json:"reason"`
	Provenance []deliveryContextProvenance `json:"provenance"`
}

type deliveryContextTaskClue struct {
	ID         string                      `json:"id"`
	Title      string                      `json:"title"`
	Status     string                      `json:"status"`
	Reason     string                      `json:"reason"`
	Provenance []deliveryContextProvenance `json:"provenance"`
}

type deliveryContextKnowledgeDomain struct {
	ID               string                      `json:"id"`
	Title            string                      `json:"title"`
	Summary          string                      `json:"summary"`
	Capsule          string                      `json:"capsule,omitempty"`
	RouteReason      string                      `json:"route_reason"`
	IndexRef         string                      `json:"index_ref"`
	CanonRef         string                      `json:"canon_ref"`
	IndexFingerprint string                      `json:"index_fingerprint,omitempty"`
	CanonFingerprint string                      `json:"canon_fingerprint,omitempty"`
	Complete         bool                        `json:"complete"`
	Provenance       []deliveryContextProvenance `json:"provenance"`
}

type deliveryContextCommandGroup struct {
	Name       string                      `json:"name"`
	Paths      []string                    `json:"paths"`
	Commands   []string                    `json:"commands"`
	Provenance []deliveryContextProvenance `json:"provenance"`
}

type deliveryContextTestCommands struct {
	Focused     []deliveryContextCommandGroup `json:"focused"`
	Integration []deliveryContextCommandGroup `json:"integration"`
	Provenance  []deliveryContextProvenance   `json:"provenance"`
}

type deliveryContextRunnerCapabilities struct {
	StructuredEvents    bool `json:"structured_events"`
	ResumeSession       bool `json:"resume_session"`
	ExplicitApprovals   bool `json:"explicit_approvals"`
	Heartbeats          bool `json:"heartbeats"`
	MachineFinalStatus  bool `json:"machine_final_status"`
	UsageMetrics        bool `json:"usage_metrics"`
	ArtifactEnumeration bool `json:"artifact_enumeration"`
}

type deliveryContextRunnerProfile struct {
	Name              string                            `json:"name"`
	Default           bool                              `json:"default"`
	Harness           string                            `json:"harness"`
	Model             string                            `json:"model"`
	Effort            string                            `json:"effort"`
	PermissionPreset  string                            `json:"permission_preset"`
	SandboxMode       string                            `json:"sandbox_mode"`
	Network           *bool                             `json:"network"`
	SubagentsAllowed  *bool                             `json:"subagents_allowed"`
	MaxSubagents      int                               `json:"max_subagents"`
	ProofCapabilities deliveryContextRunnerCapabilities `json:"proof_capabilities"`
	Provenance        []deliveryContextProvenance       `json:"provenance"`
}

type deliveryContextWorkspacePolicy struct {
	Root                    string `json:"root"`
	Strategy                string `json:"strategy"`
	MaxLiveWorktrees        int    `json:"max_live_worktrees"`
	MaxActiveRuns           int    `json:"max_active_runs"`
	MaxActiveRunsPerProject int    `json:"max_active_runs_per_project"`
}

type deliveryContextBranchPolicy struct {
	DefaultBranch       string   `json:"default_branch"`
	DefaultSHA          string   `json:"default_sha,omitempty"`
	ProtectedRefs       []string `json:"protected_refs"`
	TaskBranchTemplate  string   `json:"task_branch_template"`
	WaveBranchTemplate  string   `json:"wave_branch_template"`
	LandingCommand      string   `json:"landing_command"`
	LandingMode         string   `json:"landing_mode"`
	ControlBranchWrites bool     `json:"control_branch_writes"`
}

type deliveryContextGatePolicy struct {
	Profile           string   `json:"profile"`
	HarvestCommands   []string `json:"harvest_commands"`
	BuildSlotLocks    []string `json:"build_slot_locks"`
	AllowDirtyTree    bool     `json:"allow_dirty_tree"`
	MinFreeDiskGB     float64  `json:"min_free_disk_gb"`
	FocusedScopeNames []string `json:"focused_scope_names"`
}

type deliveryContextPolicy struct {
	AutomationEnabled  bool                              `json:"automation_enabled"`
	DispatchScope      automationDispatchScopeProjection `json:"dispatch_scope"`
	Workspace          deliveryContextWorkspacePolicy    `json:"workspace"`
	Branches           deliveryContextBranchPolicy       `json:"branches"`
	Gates              deliveryContextGatePolicy         `json:"gates"`
	ScheduledPromotion ScheduledPromotionProjection      `json:"scheduled_promotion"`
	ReviewerEnabled    bool                              `json:"reviewer_enabled"`
	Provenance         []deliveryContextProvenance       `json:"provenance"`
}

type deliveryContextPathClue struct {
	Path       string                      `json:"path"`
	Reason     string                      `json:"reason"`
	SourceID   string                      `json:"source_id,omitempty"`
	Provenance []deliveryContextProvenance `json:"provenance"`
}

type deliveryContextResourceClue struct {
	Kind       string                      `json:"kind"`
	Ref        string                      `json:"ref"`
	Reason     string                      `json:"reason"`
	SourceID   string                      `json:"source_id,omitempty"`
	Provenance []deliveryContextProvenance `json:"provenance"`
}

type deliveryContextHumanGate struct {
	ID           string                      `json:"id"`
	Title        string                      `json:"title"`
	Kind         string                      `json:"kind"`
	Owner        string                      `json:"owner"`
	Blocking     bool                        `json:"blocking"`
	Blocks       []string                    `json:"blocks"`
	Action       string                      `json:"action"`
	Verification string                      `json:"verification"`
	Reason       string                      `json:"reason"`
	Provenance   []deliveryContextProvenance `json:"provenance"`
}

type deliveryContextPlanContract struct {
	AuthoringSchema       string                          `json:"authoring_schema"`
	SupportedSchemas      []string                        `json:"supported_schemas"`
	FactoryIntakeContract factoryIntakeContractProvenance `json:"factory_intake_contract"`
	DoctorSchema          string                          `json:"doctor_schema"`
	ValidationCommand     string                          `json:"validation_command"`
	DryRunImportCommand   string                          `json:"dry_run_import_command"`
	ValidationRules       []deliveryPlanValidationRule    `json:"validation_rules"`
	Provenance            []deliveryContextProvenance     `json:"provenance"`
}

type deliveryContextRuntimeReadiness struct {
	AutomationEnabled   bool                        `json:"automation_enabled"`
	DaemonAlive         bool                        `json:"daemon_alive"`
	RuntimeStorePresent bool                        `json:"runtime_store_present"`
	RegistrationState   string                      `json:"registration_state"`
	ProjectEnabled      *bool                       `json:"project_enabled"`
	ProjectHealth       string                      `json:"project_health,omitempty"`
	DispatchAuthorized  bool                        `json:"dispatch_authorized"`
	NoWorkDispatched    bool                        `json:"no_work_dispatched"`
	Provenance          []deliveryContextProvenance `json:"provenance"`
}

type deliveryPlanningContext struct {
	Schema             string                           `json:"schema"`
	ContextFingerprint string                           `json:"context_fingerprint"`
	ReadOnly           bool                             `json:"read_only"`
	Project            deliveryContextProject           `json:"project"`
	IntegrationBase    deliveryContextIntegrationBase   `json:"integration_base"`
	Specs              []deliveryContextDocument        `json:"specs"`
	Decisions          []deliveryContextDocument        `json:"decisions"`
	EpicCandidates     []deliveryContextEpicClue        `json:"epic_candidates"`
	DuplicateTaskClues []deliveryContextTaskClue        `json:"duplicate_task_clues"`
	KnowledgeDomains   []deliveryContextKnowledgeDomain `json:"knowledge_domains"`
	TestCommands       deliveryContextTestCommands      `json:"test_commands"`
	RunnerProfiles     []deliveryContextRunnerProfile   `json:"runner_profiles"`
	Policy             deliveryContextPolicy            `json:"policy"`
	LikelyOwnedPaths   []deliveryContextPathClue        `json:"likely_owned_paths"`
	SharedResources    []deliveryContextResourceClue    `json:"shared_resources"`
	HumanGates         []deliveryContextHumanGate       `json:"human_gates"`
	PlanContract       deliveryContextPlanContract      `json:"plan_contract"`
	Readiness          deliveryContextRuntimeReadiness  `json:"readiness"`
	Unknowns           []deliveryContextUnknown         `json:"unknowns"`
}

func deliveryPlanningContextCmd(args Args) error {
	vault, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	spec := strings.TrimSpace(firstNonEmpty(args.String("spec"), args.String("_pos0")))
	if spec == "" {
		return tuskerError(errorMissingArg, "Usage: tusker delivery context --spec <repo-relative-spec> [--json]")
	}
	scope := strings.TrimSpace(args.String("scope"))
	if scope != "" && !deliveryScopeValid(scope) {
		return tuskerError(errorInvalidArg, "--scope must be a valid delivery plan scope")
	}
	report, err := buildDeliveryPlanningContextForScope(vault, spec, scope)
	if err != nil {
		return err
	}
	if args.Bool("json") {
		emitJSON(report)
		return nil
	}
	emitDeliveryPlanningContext(report)
	return nil
}

func buildDeliveryPlanningContext(vault, specArg string) (deliveryPlanningContext, error) {
	return buildDeliveryPlanningContextForScope(vault, specArg, "")
}

// buildDeliveryPlanningContextForScope excludes the records and generated
// work-stream projection owned by one delivery scope. This is the context a V2
// plan binds: importing that exact plan must not invalidate its own context.
// Work from every other scope remains material duplicate/conflict evidence.
func buildDeliveryPlanningContextForScope(vault, specArg, excludedPlanScope string) (deliveryPlanningContext, error) {
	report := deliveryPlanningContext{
		Schema:             deliveryPlanningContextSchema,
		ReadOnly:           true,
		Specs:              []deliveryContextDocument{},
		Decisions:          []deliveryContextDocument{},
		EpicCandidates:     []deliveryContextEpicClue{},
		DuplicateTaskClues: []deliveryContextTaskClue{},
		KnowledgeDomains:   []deliveryContextKnowledgeDomain{},
		TestCommands:       deliveryContextTestCommands{Focused: []deliveryContextCommandGroup{}, Integration: []deliveryContextCommandGroup{}},
		RunnerProfiles:     []deliveryContextRunnerProfile{},
		LikelyOwnedPaths:   []deliveryContextPathClue{},
		SharedResources:    []deliveryContextResourceClue{},
		HumanGates:         []deliveryContextHumanGate{},
		Unknowns:           []deliveryContextUnknown{},
	}
	configProvenance := deliveryContextConfigProvenance(vault)
	projectID, projectErr := resolveV7ProjectID(vault)
	if projectErr != nil {
		projectID = "unknown"
		report.Unknowns = append(report.Unknowns, deliveryContextUnknownFact(
			"project", "project.id", "project identity is not configured", "set project_id in tusker.yaml", configProvenance,
		))
	}
	report.Project = deliveryContextProject{ID: projectID, RepoRef: ".", VaultRef: ".tusker", Provenance: configProvenance}
	report.IntegrationBase, report.Unknowns = deliveryContextResolveIntegrationBase(vault, configProvenance, report.Unknowns)

	notes, notesErr := listOperationalNotesFrontmatter(vault)
	if notesErr != nil {
		report.Unknowns = append(report.Unknowns, deliveryContextUnknownFact(
			"tracker", "epic_candidates,duplicate_task_clues,human_gates", "operational frontmatter could not be inspected", "repair invalid .tusker/work frontmatter", []deliveryContextProvenance{{Kind: "frontmatter_scan", Ref: ".tusker/work"}},
		))
		notes = nil
	}
	expectedWorkStream, expectedWorkStreamKnown := deliveryContextExpectedWorkStream(notes, excludedPlanScope)
	specs, decisions, documentDomains, governingRefs, documentUnknowns, err := deliveryContextDocuments(vault, specArg, excludedPlanScope, expectedWorkStream, expectedWorkStreamKnown)
	if err != nil {
		return report, err
	}
	report.Specs, report.Decisions = specs, decisions
	report.Unknowns = append(report.Unknowns, documentUnknowns...)

	wfFile, workflowErr := loadWorkflow(vault)
	workflowKnown := workflowErr == nil
	if !workflowKnown {
		report.Unknowns = append(report.Unknowns, deliveryContextUnknownFact(
			"project", "workflow", "workflow policy could not be resolved", "repair .tusker/WORKFLOW.md and project automation configuration", configProvenance,
		))
	}

	notes = deliveryContextExcludePlanScope(notes, excludedPlanScope)
	relevantTasks := deliveryContextRelevantTasks(notes, governingRefs)
	taskDomains := deliveryContextDomainsFromNotes(relevantTasks)
	domains := uniqueStrings(append(documentDomains, taskDomains...))
	sort.Strings(domains)

	report.DuplicateTaskClues, report.Unknowns = deliveryContextTaskClues(relevantTasks, report.Unknowns)
	report.EpicCandidates, report.Unknowns = deliveryContextEpicClues(notes, relevantTasks, governingRefs, report.Unknowns)
	report.HumanGates, report.Unknowns = deliveryContextHumanGates(notes, relevantTasks, domains, report.Unknowns)
	report.KnowledgeDomains, report.Unknowns = deliveryContextKnowledge(vault, domains, report.Unknowns)

	if workflowKnown {
		report.TestCommands, report.Unknowns = deliveryContextCommands(wfFile, configProvenance, report.Unknowns)
		report.RunnerProfiles, report.Unknowns = deliveryContextProfiles(wfFile.Data, configProvenance, report.Unknowns)
		report.Policy, report.Unknowns = deliveryContextWorkflowPolicy(vault, wfFile.Data, configProvenance, report.Unknowns)
		report.LikelyOwnedPaths, report.Unknowns = deliveryContextOwnedPathClues(relevantTasks, wfFile.Data, report.Unknowns)
		report.SharedResources, report.Unknowns = deliveryContextResourceClues(relevantTasks, wfFile.Data, configProvenance, report.Unknowns)
	} else {
		for _, missing := range []struct {
			kind, field, reason, remedy string
		}{
			{"test_command", "test_commands.focused", "focused test commands are unknown without workflow policy", "configure orchestration.gate.scopes"},
			{"test_command", "test_commands.integration", "integration test commands are unknown without workflow policy", "configure orchestration.gate.harvest_commands"},
			{"runner_profile", "runner_profiles", "runner profiles are unknown without resolved automation configuration", "configure automation.profiles"},
			{"project", "policy", "workspace, branch, gate, and landing policy are unknown", "repair .tusker/WORKFLOW.md"},
		} {
			report.Unknowns = append(report.Unknowns, deliveryContextUnknownFact(missing.kind, missing.field, missing.reason, missing.remedy, configProvenance))
		}
	}

	report.PlanContract = deliveryContextPlanSchemaContract()
	report.Readiness, report.Unknowns = deliveryContextReadiness(vault, workflowKnown, wfFile.Data, report.Unknowns)
	deliveryContextSort(&report)
	report.ContextFingerprint = deliveryContextMaterialFingerprint(report)
	return report, nil
}

func deliveryContextResolveIntegrationBase(vault string, provenance []deliveryContextProvenance, unknowns []deliveryContextUnknown) (deliveryContextIntegrationBase, []deliveryContextUnknown) {
	branch := v7DefaultBranch(vault)
	base := deliveryContextIntegrationBase{Ref: "refs/heads/" + branch, Provenance: provenance}
	if !v7GitRepo(v7RepoRoot(vault)) {
		return base, unknowns
	}
	sha, err := gitOutputTrim(v7RepoRoot(vault), "rev-parse", base.Ref)
	if err != nil {
		unknowns = append(unknowns, deliveryContextUnknownFact(
			"project", "integration_base.sha", "configured default integration base could not be resolved", "repair the configured default branch before review", provenance,
		))
		return base, unknowns
	}
	base.SHA = sha
	return base, unknowns
}

type deliveryContextDocumentRequest struct {
	Ref      string
	Required bool
}

func deliveryContextDocuments(vault, specArg, excludedPlanScope string, expectedWorkStream deliveryContextWorkStreamExpectation, expectedWorkStreamKnown bool) ([]deliveryContextDocument, []deliveryContextDocument, []string, []string, []deliveryContextUnknown, error) {
	refs := splitCSV(specArg)
	if len(refs) == 0 {
		return nil, nil, nil, nil, nil, tuskerError(errorMissingArg, "--spec must name at least one governing specification")
	}
	if len(refs) > deliveryContextMaxDocuments {
		return nil, nil, nil, nil, nil, tuskerError(errorInvalidArg, fmt.Sprintf("--spec accepts at most %d governing specifications", deliveryContextMaxDocuments))
	}
	queue := make([]deliveryContextDocumentRequest, 0, len(refs))
	for _, ref := range refs {
		queue = append(queue, deliveryContextDocumentRequest{Ref: ref, Required: true})
	}
	seen := map[string]bool{}
	var specs, decisions []deliveryContextDocument
	var domains, governingRefs []string
	unknowns := []deliveryContextUnknown{}
	for len(queue) > 0 {
		request := queue[0]
		queue = queue[1:]
		ref := v7CleanSpecRef(request.Ref)
		if ref == "" || seen[ref] {
			continue
		}
		seen[ref] = true
		isDecision := v7SpecRefDecisionID(ref) != ""
		if (!isDecision && len(specs) >= deliveryContextMaxDocuments) || (isDecision && len(decisions) >= deliveryContextMaxDocuments) {
			kind := "specs"
			if isDecision {
				kind = "decisions"
			}
			unknowns = append(unknowns, deliveryContextUnknownFact(
				"bounded_selection", kind, fmt.Sprintf("additional cited %s were omitted at the deterministic bound of %d", kind, deliveryContextMaxDocuments),
				"narrow the governing --spec set", []deliveryContextProvenance{{Kind: "citation", Ref: ref}},
			))
			continue
		}
		raw, displayRef, err := deliveryContextReadDocument(vault, ref)
		if err != nil {
			if request.Required {
				return nil, nil, nil, nil, nil, tuskerError(errorInvalidArg, "delivery context spec is not a readable allowed repository document: "+ref, withHint("use a Markdown file under docs/specs or docs/design"))
			}
			unknowns = append(unknowns, deliveryContextUnknownFact(
				"document", ref, "an explicitly cited document is missing or outside the allowed document roots", "repair or remove the citation", []deliveryContextProvenance{{Kind: "citation", Ref: ref}},
			))
			continue
		}
		materialRaw := []byte(strings.TrimRight(string(raw), "\n"))
		// A scope with no uniquely reconstructable imported wave has no trusted
		// generated payload. Keep any marker content material rather than guessing
		// that it is Tusker-owned.
		materialRaw = deliveryContextCanonicalDocumentMaterialWithExpectation(raw, excludedPlanScope, expectedWorkStream, expectedWorkStreamKnown)
		data, body, parseErr := parseFrontmatter(string(materialRaw))
		if parseErr != nil {
			if request.Required {
				return nil, nil, nil, nil, nil, tuskerError(errorInvalidArg, "delivery context spec has invalid frontmatter: "+ref)
			}
			unknowns = append(unknowns, deliveryContextUnknownFact(
				"document", ref, "an explicitly cited document has invalid frontmatter", "repair the cited document frontmatter", []deliveryContextProvenance{{Kind: "repo_file", Ref: displayRef, Fingerprint: deliveryFingerprint(raw)}},
			))
			continue
		}
		// A generated delivery-import block is bookkeeping, not authored planning
		// input. Its renderer intentionally normalizes the end of a document, so
		// canonicalize only terminal newlines after stripping that block. This
		// makes import idempotent for a spec that originally had no final newline,
		// while retaining every human-authored character (including all nonterminal
		// whitespace) as fingerprint material.
		fingerprintMaterial := materialRaw
		// Importing a scoped work-stream into a decision refreshes only decision
		// bookkeeping (`updated_*` and state revision) in addition to the marker
		// removed above. Those facts are not planning input, so exclude them from
		// a scope-bound context while preserving all authored decision content.
		if isDecision && excludedPlanScope != "" {
			fingerprintMaterial = deliveryContextDecisionMaterial(data, body)
		}
		provenance := []deliveryContextProvenance{{Kind: "repo_file", Ref: displayRef, Fingerprint: deliveryFingerprint(fingerprintMaterial)}}
		doc := deliveryContextDocument{
			Kind: "spec", Ref: ref, Title: deliveryContextDocumentTitle(data, body, ref),
			Fingerprint: deliveryFingerprint(fingerprintMaterial), Provenance: provenance,
		}
		if isDecision {
			doc.Kind = "decision"
			doc.Status = stringField(data, "status")
			decisions = append(decisions, doc)
		} else {
			specs = append(specs, doc)
		}
		governingRefs = append(governingRefs, ref)
		domains = append(domains, deliveryContextDocumentDomains(data)...)

		nextRefs := append([]string{}, normalizeList(data["spec_refs"])...)
		nextRefs = append(nextRefs, normalizeList(data["decision_refs"])...)
		nextRefs = append(nextRefs, normalizeList(data["decisions"])...)
		for _, match := range deliveryDecisionLinkRegex.FindAllStringSubmatch(body, -1) {
			if len(match) > 1 {
				nextRefs = append(nextRefs, match[1])
			}
		}
		nextRefs = uniqueStrings(nextRefs)
		sort.Strings(nextRefs)
		for _, next := range nextRefs {
			queue = append(queue, deliveryContextDocumentRequest{Ref: next})
		}
		if len(queue) > deliveryContextMaxDocuments*4 {
			unknowns = append(unknowns, deliveryContextUnknownFact(
				"bounded_selection", "document_citations", "document citations exceeded the deterministic traversal bound",
				"narrow spec_refs and decision_refs to governing material", provenance,
			))
			queue = queue[:deliveryContextMaxDocuments*4]
		}
	}
	if len(specs) == 0 {
		return nil, nil, nil, nil, nil, tuskerError(errorInvalidArg, "--spec must resolve to at least one governing specification")
	}
	sort.Slice(specs, func(i, j int) bool { return specs[i].Ref < specs[j].Ref })
	sort.Slice(decisions, func(i, j int) bool { return decisions[i].Ref < decisions[j].Ref })
	domains, governingRefs = uniqueStrings(domains), uniqueStrings(governingRefs)
	sort.Strings(domains)
	sort.Strings(governingRefs)
	return specs, decisions, domains, governingRefs, unknowns, nil
}

func deliveryContextDecisionMaterial(data map[string]any, body string) []byte {
	material := make(map[string]any, len(data))
	for key, value := range data {
		material[key] = value
	}
	for _, field := range []string{"updated_at", "updated_by", "state_rev"} {
		delete(material, field)
	}
	raw, _ := json.Marshal(struct {
		Frontmatter map[string]any `json:"frontmatter"`
		Body        string         `json:"body"`
	}{Frontmatter: material, Body: strings.TrimRight(body, "\n")})
	return raw
}

func deliveryContextReadDocument(vault, ref string) ([]byte, string, error) {
	ref = v7CleanSpecRef(ref)
	decisionID := v7SpecRefDecisionID(ref)
	if decisionID == "" {
		if filepath.IsAbs(ref) || v7SpecRefPathEscapes(ref) || !strings.HasSuffix(strings.ToLower(ref), ".md") ||
			(!strings.HasPrefix(ref, "docs/specs/") && !strings.HasPrefix(ref, "docs/design/")) {
			return nil, "", errors.New("document ref outside allowlist")
		}
	}
	path := deliverySpecRefPath(vault, ref)
	if path == "" {
		return nil, "", errors.New("document path unresolved")
	}
	root, err := filepath.EvalSymlinks(v7RepoRoot(vault))
	if err != nil {
		return nil, "", err
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return nil, "", err
	}
	rel, err := filepath.Rel(root, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil, "", errors.New("document symlink escapes repository")
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.Mode().IsRegular() {
		return nil, "", errors.New("document is not a regular file")
	}
	raw, err := os.ReadFile(resolved)
	if err != nil {
		return nil, "", err
	}
	displayRef := ref
	if decisionID != "" {
		displayRef = filepath.ToSlash(filepath.Join(".tusker", "work", "decisions", decisionID+".md"))
	}
	return raw, displayRef, nil
}

func deliveryContextReadContainedRegularFile(repoRoot, path string) ([]byte, error) {
	root, err := filepath.EvalSymlinks(repoRoot)
	if err != nil {
		return nil, err
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return nil, err
	}
	rel, err := filepath.Rel(root, resolved)
	if err != nil || filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil, errors.New("file symlink escapes repository")
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.Mode().IsRegular() {
		return nil, errors.New("file is not a regular file")
	}
	return os.ReadFile(resolved)
}

func deliveryContextDocumentTitle(data map[string]any, body, ref string) string {
	if title := stringField(data, "title"); title != "" {
		return title
	}
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "# ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "# "))
		}
	}
	return strings.TrimSuffix(filepath.Base(ref), filepath.Ext(ref))
}

func deliveryContextDocumentDomains(data map[string]any) []string {
	domains := append([]string{}, normalizeList(data["domains"])...)
	domains = append(domains, normalizeList(data["knowledge_domains"])...)
	if domain := stringField(data, "domain"); domain != "" {
		domains = append(domains, domain)
	}
	domains = uniqueStrings(domains)
	sort.Strings(domains)
	return domains
}

func deliveryContextRelevantTasks(notes []Note, governingRefs []string) []Note {
	var tasks []Note
	for _, note := range notes {
		if effectiveV7Kind(note.Data) != "task" || v7TerminalTaskStatus(stringField(note.Data, "status")) {
			continue
		}
		if deliveryRefsOverlap(normalizeList(note.Data["spec_refs"]), governingRefs) {
			tasks = append(tasks, note)
		}
	}
	sort.Slice(tasks, func(i, j int) bool { return stringField(tasks[i].Data, "id") < stringField(tasks[j].Data, "id") })
	return tasks
}

// deliveryContextExcludePlanScope removes only the canonical records created
// by the plan whose context is being recomputed. Leaving a plan's own epic,
// tasks, and human gates in the bounded packet makes a successful import alter
// its own fingerprint, which turns a valid Start boundary into a paradox.
func deliveryContextExcludePlanScope(notes []Note, scope string) []Note {
	scope = strings.TrimSpace(scope)
	if scope == "" {
		return notes
	}
	out := make([]Note, 0, len(notes))
	for _, note := range notes {
		kind := effectiveV7Kind(note.Data)
		if (kind == "epic" || kind == "task" || kind == "gate") && stringField(note.Data, "delivery_plan_scope") == scope {
			continue
		}
		out = append(out, note)
	}
	return out
}

type deliveryContextWorkStreamExpectation struct {
	WaveID      string
	TaskSources map[string]string
}

func deliveryContextExpectedWorkStream(notes []Note, scope string) (deliveryContextWorkStreamExpectation, bool) {
	scope = strings.TrimSpace(scope)
	if scope == "" {
		return deliveryContextWorkStreamExpectation{}, false
	}
	var wave Note
	found := 0
	for _, note := range notes {
		if effectiveV7Kind(note.Data) == "wave" && stringField(note.Data, "delivery_plan_scope") == scope {
			wave, found = note, found+1
		}
	}
	if found != 1 {
		return deliveryContextWorkStreamExpectation{}, false
	}
	waveID := stringField(wave.Data, "id")
	members := normalizeList(wave.Data["members"])
	if !v7WaveIDPattern.MatchString(waveID) || len(members) == 0 {
		return deliveryContextWorkStreamExpectation{}, false
	}
	tasks := map[string]Note{}
	for _, note := range notes {
		if effectiveV7Kind(note.Data) == "task" {
			tasks[stringField(note.Data, "id")] = note
		}
	}
	expected := deliveryContextWorkStreamExpectation{WaveID: waveID, TaskSources: map[string]string{}}
	for _, member := range members {
		task, ok := tasks[member]
		if !ok || stringField(task.Data, "delivery_plan_scope") != scope || stringField(task.Data, "wave") != waveID {
			return deliveryContextWorkStreamExpectation{}, false
		}
		source := stringField(task.Data, "delivery_source_key")
		if !v7TaskIDPattern.MatchString(member) || source == "" || expected.TaskSources[member] != "" {
			return deliveryContextWorkStreamExpectation{}, false
		}
		expected.TaskSources[member] = source
	}
	return expected, true
}

type deliveryContextWorkStreamBlock struct {
	start                 int
	end                   int
	legacyHeadingStart    int
	legacyHeadingDetected bool
}

// deliveryContextGeneratedWorkStream recognizes exactly one renderer-owned
// block for one scope. Marker text alone is never authority to remove content:
// duplicate, mismatched, or hand-edited payload remains fingerprint material.
func deliveryContextGeneratedWorkStream(text, scope string, expected deliveryContextWorkStreamExpectation, expectedKnown bool) (deliveryContextWorkStreamBlock, bool) {
	begin, end := deliveryScopeMarkers(scope)
	if strings.Count(text, begin) != 1 || strings.Count(text, end) != 1 {
		return deliveryContextWorkStreamBlock{}, false
	}
	start := strings.Index(text, begin)
	endStart := strings.Index(text[start+len(begin):], end)
	if start < 0 || endStart < 0 {
		return deliveryContextWorkStreamBlock{}, false
	}
	endStart += start + len(begin)
	blockEnd := endStart + len(end)
	lines := strings.Split(text[start:blockEnd], "\n")
	if len(lines) < 7 || lines[0] != begin || lines[1] != "" || lines[len(lines)-1] != end {
		return deliveryContextWorkStreamBlock{}, false
	}
	line := 2
	if lines[line] == "## Work streams" {
		line++
		if line >= len(lines)-1 || lines[line] != "" {
			return deliveryContextWorkStreamBlock{}, false
		}
		line++
	}
	seenTasks, seenSources := map[string]bool{}, map[string]bool{}
	taskSources := map[string]string{}
	previousSource := ""
	for line < len(lines)-1 && strings.HasPrefix(lines[line], "- `[[") {
		id, source, ok := deliveryContextGeneratedTaskLine(lines[line])
		if !ok || seenTasks[id] || seenSources[source] || (previousSource != "" && source <= previousSource) {
			return deliveryContextWorkStreamBlock{}, false
		}
		seenTasks[id], seenSources[source], taskSources[id], previousSource = true, true, source, source
		line++
	}
	if len(seenTasks) == 0 || line >= len(lines)-1 || lines[line] != "" {
		return deliveryContextWorkStreamBlock{}, false
	}
	line++
	if line >= len(lines)-1 || !deliveryContextGeneratedWaveLine(lines[line]) {
		return deliveryContextWorkStreamBlock{}, false
	}
	waveID := deliveryContextGeneratedWaveID(lines[line])
	line++
	if line != len(lines)-2 || lines[line] != "" {
		return deliveryContextWorkStreamBlock{}, false
	}
	if expectedKnown && !deliveryContextWorkStreamMatchesExpected(taskSources, waveID, expected) {
		return deliveryContextWorkStreamBlock{}, false
	}

	block := deliveryContextWorkStreamBlock{start: start, end: blockEnd, legacyHeadingStart: -1}
	const legacyHeading = "## Work streams\n\n"
	if before := text[:start]; strings.HasSuffix(before, legacyHeading) {
		block.legacyHeadingDetected = true
		block.legacyHeadingStart = len(before) - len(legacyHeading)
	}
	return block, true
}

func deliveryContextGeneratedTaskLine(line string) (string, string, bool) {
	const prefix = "- `[["
	const separator = "]]` implements delivery source `"
	const suffix = "`."
	if !strings.HasPrefix(line, prefix) || !strings.HasSuffix(line, suffix) {
		return "", "", false
	}
	rest := strings.TrimPrefix(line, prefix)
	cut := strings.Index(rest, separator)
	if cut < 1 {
		return "", "", false
	}
	id, source := rest[:cut], strings.TrimSuffix(rest[cut+len(separator):], suffix)
	if !v7TaskIDPattern.MatchString(id) || source == "" {
		return "", "", false
	}
	return id, source, true
}

func deliveryContextGeneratedWaveLine(line string) bool {
	return deliveryContextGeneratedWaveID(line) != ""
}

func deliveryContextGeneratedWaveID(line string) string {
	const prefix = "- `[["
	const suffix = "]]` is the imported delivery wave."
	if !strings.HasPrefix(line, prefix) || !strings.HasSuffix(line, suffix) {
		return ""
	}
	id := strings.TrimSuffix(strings.TrimPrefix(line, prefix), suffix)
	if !v7WaveIDPattern.MatchString(id) {
		return ""
	}
	return id
}

func deliveryContextWorkStreamMatchesExpected(actual map[string]string, waveID string, expected deliveryContextWorkStreamExpectation) bool {
	if waveID != expected.WaveID || len(actual) != len(expected.TaskSources) {
		return false
	}
	for id, source := range actual {
		if expected.TaskSources[id] != source {
			return false
		}
	}
	return true
}

func deliveryContextRemoveGeneratedWorkStream(text string, block deliveryContextWorkStreamBlock) string {
	left := text[:block.start]
	if block.legacyHeadingDetected {
		left = text[:block.legacyHeadingStart]
	}
	left = strings.TrimRight(left, "\n")
	right := strings.TrimLeft(text[block.end:], "\n")
	if left == "" {
		return right
	}
	if right == "" {
		return left
	}
	return left + "\n\n" + right
}

// deliveryContextCanonicalDocumentMaterial removes only generated import
// bookkeeping from planning fingerprints. New blocks carry their heading inside
// the markers. The terminal-heading clause is narrow compatibility for blocks
// written by the older renderer, which placed an otherwise empty heading just
// before its marker. Any authored text under that heading remains material.
func deliveryContextCanonicalDocumentMaterialWithExpectation(raw []byte, scope string, expected deliveryContextWorkStreamExpectation, expectedKnown bool) []byte {
	scope = strings.TrimSpace(scope)
	if scope == "" {
		return raw
	}
	if !expectedKnown {
		text := strings.TrimRight(string(raw), "\n")
		begin, end := deliveryScopeMarkers(scope)
		if !strings.Contains(text, begin) && !strings.Contains(text, end) {
			for _, heading := range []string{"## Work streams", "## Work Streams"} {
				if strings.HasSuffix(text, "\n\n"+heading) {
					text = strings.TrimSuffix(text, "\n\n"+heading)
					break
				}
			}
		}
		return []byte(text)
	}
	text := string(raw)
	if block, ok := deliveryContextGeneratedWorkStream(text, scope, expected, expectedKnown); ok {
		text = deliveryContextRemoveGeneratedWorkStream(text, block)
	}
	return []byte(strings.TrimRight(text, "\n"))
}

func deliveryContextDomainsFromNotes(notes []Note) []string {
	var domains []string
	for _, note := range notes {
		domains = append(domains, normalizeList(note.Data["domains"])...)
	}
	domains = uniqueStrings(domains)
	sort.Strings(domains)
	return domains
}

func deliveryContextTaskClues(tasks []Note, unknowns []deliveryContextUnknown) ([]deliveryContextTaskClue, []deliveryContextUnknown) {
	all := make([]deliveryContextTaskClue, 0, len(tasks))
	for _, task := range tasks {
		all = append(all, deliveryContextTaskClue{
			ID: stringField(task.Data, "id"), Title: stringField(task.Data, "title"), Status: stringField(task.Data, "status"),
			Reason:     "open task has an exact governing spec_ref overlap",
			Provenance: deliveryContextNoteProvenance(task, "id", "title", "status", "spec_refs"),
		})
	}
	if len(all) > deliveryContextMaxTasks {
		unknowns = append(unknowns, deliveryContextUnknownFact(
			"bounded_selection", "duplicate_task_clues", fmt.Sprintf("%d additional matching tasks were omitted at the deterministic bound", len(all)-deliveryContextMaxTasks),
			"narrow the governing --spec set before planning", []deliveryContextProvenance{{Kind: "frontmatter_scan", Ref: ".tusker/work/tasks"}},
		))
		all = all[:deliveryContextMaxTasks]
	}
	return all, unknowns
}

func deliveryContextEpicClues(notes, relevantTasks []Note, governingRefs []string, unknowns []deliveryContextUnknown) ([]deliveryContextEpicClue, []deliveryContextUnknown) {
	taskEpics := map[string]bool{}
	for _, task := range relevantTasks {
		taskEpics[stringField(task.Data, "epic")] = true
	}
	var clues []deliveryContextEpicClue
	for _, note := range notes {
		if effectiveV7Kind(note.Data) != "epic" {
			continue
		}
		id := stringField(note.Data, "id")
		reason := ""
		if deliveryRefsOverlap(normalizeList(note.Data["spec_refs"]), governingRefs) {
			reason = "epic has an exact governing spec_ref overlap"
		} else if taskEpics[id] {
			reason = "epic contains an open task with an exact governing spec_ref overlap"
		}
		if reason == "" {
			continue
		}
		clues = append(clues, deliveryContextEpicClue{
			ID: id, Title: stringField(note.Data, "title"), Status: stringField(note.Data, "status"), Reason: reason,
			Provenance: deliveryContextNoteProvenance(note, "id", "title", "status", "spec_refs"),
		})
	}
	sort.Slice(clues, func(i, j int) bool { return clues[i].ID < clues[j].ID })
	if len(clues) > deliveryContextMaxEpics {
		unknowns = append(unknowns, deliveryContextUnknownFact(
			"bounded_selection", "epic_candidates", fmt.Sprintf("%d additional epic candidates were omitted at the deterministic bound", len(clues)-deliveryContextMaxEpics),
			"narrow the governing --spec set", []deliveryContextProvenance{{Kind: "frontmatter_scan", Ref: ".tusker/work/epics"}},
		))
		clues = clues[:deliveryContextMaxEpics]
	}
	return clues, unknowns
}

func deliveryContextHumanGates(notes, relevantTasks []Note, domains []string, unknowns []deliveryContextUnknown) ([]deliveryContextHumanGate, []deliveryContextUnknown) {
	taskIDs := map[string]bool{}
	for _, task := range relevantTasks {
		taskIDs[stringField(task.Data, "id")] = true
	}
	domainSet := map[string]bool{}
	for _, domain := range domains {
		domainSet[domain] = true
	}
	var gates []deliveryContextHumanGate
	for _, note := range notes {
		if effectiveV7Kind(note.Data) != "gate" || stringField(note.Data, "status") != "open" || v7ProofOwnerClass(stringField(note.Data, "owner")) != "human" {
			continue
		}
		reason := ""
		for _, id := range normalizeList(note.Data["blocks"]) {
			if taskIDs[id] {
				reason = "open human gate blocks a matching task"
				break
			}
		}
		if reason == "" {
			for _, domain := range normalizeList(note.Data["domains"]) {
				if domainSet[domain] {
					reason = "open human gate cites a routed domain"
					break
				}
			}
		}
		if reason == "" {
			continue
		}
		blocks := uniqueStrings(normalizeList(note.Data["blocks"]))
		sort.Strings(blocks)
		gates = append(gates, deliveryContextHumanGate{
			ID: stringField(note.Data, "id"), Title: stringField(note.Data, "title"), Kind: stringField(note.Data, "gate_kind"),
			Owner: stringField(note.Data, "owner"), Blocking: boolField(note.Data, "blocking"), Blocks: blocks,
			Action: stringField(note.Data, "action"), Verification: stringField(note.Data, "verification"), Reason: reason,
			Provenance: deliveryContextNoteProvenance(note, "id", "title", "gate_kind", "owner", "blocking", "blocks", "action", "verification", "status"),
		})
	}
	sort.Slice(gates, func(i, j int) bool { return gates[i].ID < gates[j].ID })
	if len(gates) > deliveryContextMaxGates {
		unknowns = append(unknowns, deliveryContextUnknownFact(
			"bounded_selection", "human_gates", fmt.Sprintf("%d additional relevant human gates were omitted at the deterministic bound", len(gates)-deliveryContextMaxGates),
			"narrow the governing --spec or domain set", []deliveryContextProvenance{{Kind: "frontmatter_scan", Ref: ".tusker/work/gates"}},
		))
		gates = gates[:deliveryContextMaxGates]
	}
	return gates, unknowns
}

func deliveryContextKnowledge(vault string, domains []string, unknowns []deliveryContextUnknown) ([]deliveryContextKnowledgeDomain, []deliveryContextUnknown) {
	if len(domains) == 0 && fileExists(filepath.Join(vault, "knowledge", "domains", "project", "INDEX.md")) {
		domains = []string{"project"}
	}
	if len(domains) == 0 {
		unknowns = append(unknowns, deliveryContextUnknownFact(
			"knowledge_domain", "knowledge_domains", "no routed project-knowledge domain is declared", "declare domains in the spec or create the project domain route",
			[]deliveryContextProvenance{{Kind: "knowledge_router", Ref: ".tusker/SKILL.md"}},
		))
		return []deliveryContextKnowledgeDomain{}, unknowns
	}
	domains = uniqueStrings(domains)
	sort.Strings(domains)
	if len(domains) > deliveryContextMaxDomains {
		unknowns = append(unknowns, deliveryContextUnknownFact(
			"bounded_selection", "knowledge_domains", fmt.Sprintf("%d additional domain routes were omitted at the deterministic bound", len(domains)-deliveryContextMaxDomains),
			"reduce the spec to the domains that own this delivery", []deliveryContextProvenance{{Kind: "knowledge_router", Ref: ".tusker/SKILL.md"}},
		))
		domains = domains[:deliveryContextMaxDomains]
	}
	out := make([]deliveryContextKnowledgeDomain, 0, len(domains))
	for _, domain := range domains {
		if validateKnowledgeNodePath(domain) != "" || strings.Contains(domain, "/") {
			unknowns = append(unknowns, deliveryContextUnknownFact(
				"knowledge_domain", domain, "domain ID is not one portable path segment", "repair the domain declaration",
				[]deliveryContextProvenance{{Kind: "domain_declaration", Ref: domain}},
			))
			continue
		}
		indexRef := filepath.ToSlash(filepath.Join(".tusker", "knowledge", "domains", domain, "INDEX.md"))
		canonRef := filepath.ToSlash(filepath.Join(".tusker", "knowledge", "domains", domain, "CANON.md"))
		indexRaw, indexErr := deliveryContextReadContainedRegularFile(v7RepoRoot(vault), filepath.Join(vault, "knowledge", "domains", domain, "INDEX.md"))
		canonRaw, canonErr := deliveryContextReadContainedRegularFile(v7RepoRoot(vault), filepath.Join(vault, "knowledge", "domains", domain, "CANON.md"))
		item := deliveryContextKnowledgeDomain{
			ID: domain, RouteReason: "declared by the governing documents or matching task metadata",
			IndexRef: indexRef, CanonRef: canonRef, Complete: indexErr == nil && canonErr == nil,
			Provenance: []deliveryContextProvenance{},
		}
		if domain == "project" {
			item.RouteReason = "project is the bounded fallback when no narrower domain is declared"
		}
		if indexErr == nil {
			item.IndexFingerprint = deliveryFingerprint(indexRaw)
			item.Provenance = append(item.Provenance, deliveryContextProvenance{Kind: "knowledge_index", Ref: indexRef, Fingerprint: item.IndexFingerprint})
			if data, body, err := parseFrontmatter(string(indexRaw)); err == nil {
				item.Title = stringField(data, "title")
				item.Summary = stringField(data, "summary")
				item.Capsule = capsuleOneLine(Note{Data: data, Body: body})
			}
		}
		if canonErr == nil {
			item.CanonFingerprint = deliveryFingerprint(canonRaw)
			item.Provenance = append(item.Provenance, deliveryContextProvenance{Kind: "knowledge_canon", Ref: canonRef, Fingerprint: item.CanonFingerprint})
			if item.Summary == "" {
				if data, _, err := parseFrontmatter(string(canonRaw)); err == nil {
					item.Summary = stringField(data, "summary")
				}
			}
		}
		if item.Title == "" {
			item.Title = domain
		}
		if !item.Complete {
			unknowns = append(unknowns, deliveryContextUnknownFact(
				"knowledge_domain", domain, "routed INDEX.md or CANON.md is missing, unreadable, non-regular, or outside repository containment",
				"repair both canonical domain files as regular files contained within the repository",
				[]deliveryContextProvenance{{Kind: "knowledge_route", Ref: filepath.ToSlash(filepath.Join(".tusker", "knowledge", "domains", domain))}},
			))
		}
		out = append(out, item)
	}
	return out, unknowns
}

func deliveryContextCommands(wf WorkflowFile, provenance []deliveryContextProvenance, unknowns []deliveryContextUnknown) (deliveryContextTestCommands, []deliveryContextUnknown) {
	out := deliveryContextTestCommands{Focused: []deliveryContextCommandGroup{}, Integration: []deliveryContextCommandGroup{}, Provenance: provenance}
	for _, scope := range wf.Data.Orchestration.Gate.Scopes {
		field := "test_commands.focused." + strings.TrimSpace(scope.Name)
		commands, nextUnknowns := deliveryContextSanitizedCommands(scope.Commands, field, provenance, unknowns)
		unknowns = nextUnknowns
		if len(commands) == 0 {
			continue
		}
		paths := deliveryContextCleanStrings(scope.Paths)
		sort.Strings(paths)
		out.Focused = append(out.Focused, deliveryContextCommandGroup{Name: strings.TrimSpace(scope.Name), Paths: paths, Commands: commands, Provenance: provenance})
	}
	sort.Slice(out.Focused, func(i, j int) bool { return out.Focused[i].Name < out.Focused[j].Name })
	policy := resolveGateTierPolicy(wf.Data)
	commands, nextUnknowns := deliveryContextSanitizedCommands(policy.HarvestCommands, "test_commands.integration", provenance, unknowns)
	unknowns = nextUnknowns
	if len(commands) > 0 {
		out.Integration = append(out.Integration, deliveryContextCommandGroup{Name: "harvest", Paths: []string{"."}, Commands: commands, Provenance: provenance})
	}
	if len(out.Focused) == 0 {
		unknowns = append(unknowns, deliveryContextUnknownFact(
			"test_command", "test_commands.focused", "no focused test scopes are configured", "configure orchestration.gate.scopes with exact paths and commands", provenance,
		))
	}
	if len(out.Integration) == 0 {
		unknowns = append(unknowns, deliveryContextUnknownFact(
			"test_command", "test_commands.integration", "no integration harvest command is configured", "configure orchestration.gate.harvest_commands", provenance,
		))
	}
	return out, unknowns
}

func deliveryContextProfiles(wf Workflow, provenance []deliveryContextProvenance, unknowns []deliveryContextUnknown) ([]deliveryContextRunnerProfile, []deliveryContextUnknown) {
	names := make([]string, 0, len(wf.RunnerProfiles))
	for name := range wf.RunnerProfiles {
		names = append(names, name)
	}
	sort.Strings(names)
	if len(names) == 0 {
		unknowns = append(unknowns, deliveryContextUnknownFact(
			"runner_profile", "runner_profiles", "no runner profiles are configured", "configure automation.profiles and a default profile", provenance,
		))
		return []deliveryContextRunnerProfile{}, unknowns
	}
	if len(names) > deliveryContextMaxProfiles {
		unknowns = append(unknowns, deliveryContextUnknownFact(
			"bounded_selection", "runner_profiles", fmt.Sprintf("%d additional profiles were omitted at the deterministic bound", len(names)-deliveryContextMaxProfiles),
			"remove unused runner profiles or route this delivery explicitly", provenance,
		))
		names = names[:deliveryContextMaxProfiles]
	}
	out := make([]deliveryContextRunnerProfile, 0, len(names))
	for _, name := range names {
		definition := wf.RunnerProfiles[name]
		capabilities := RunnerCapabilities{}
		if runner, _, err := runnerForName(definition.Harness, wf); err == nil {
			capabilities = runner.Capabilities()
		} else {
			unknowns = append(unknowns, deliveryContextUnknownFact(
				"runner_profile", "runner_profiles."+name+".proof_capabilities", "runner harness capabilities could not be resolved",
				"choose a supported runner harness", provenance,
			))
		}
		if definition.Sandbox.Network == nil {
			unknowns = append(unknowns, deliveryContextUnknownFact(
				"runner_profile", "runner_profiles."+name+".network", "network policy is not explicit", "set automation.profiles."+name+".sandbox.network", provenance,
			))
		}
		out = append(out, deliveryContextRunnerProfile{
			Name: name, Default: name == wf.RunnerDefaultProfile, Harness: definition.Harness, Model: definition.Model, Effort: definition.Effort,
			PermissionPreset: definition.PermissionPreset, SandboxMode: definition.Sandbox.Mode, Network: definition.Sandbox.Network,
			SubagentsAllowed: definition.Subagents.Allowed, MaxSubagents: definition.Subagents.MaxConcurrent,
			ProofCapabilities: deliveryContextRunnerCapabilities{
				StructuredEvents: capabilities.StructuredEvents, ResumeSession: capabilities.ResumeSession,
				ExplicitApprovals: capabilities.ExplicitApprovals, Heartbeats: capabilities.Heartbeats,
				MachineFinalStatus: capabilities.MachineFinalStatus, UsageMetrics: capabilities.UsageMetrics,
				ArtifactEnumeration: capabilities.ArtifactEnumeration,
			},
			Provenance: provenance,
		})
	}
	return out, unknowns
}

func deliveryContextWorkflowPolicy(vault string, wf Workflow, provenance []deliveryContextProvenance, unknowns []deliveryContextUnknown) (deliveryContextPolicy, []deliveryContextUnknown) {
	root := strings.TrimSpace(wf.Workspace.Root)
	if filepath.IsAbs(root) {
		root = "[redacted absolute workspace root]"
		unknowns = append(unknowns, deliveryContextUnknownFact(
			"project", "policy.workspace.root", "configured workspace root is absolute and omitted from this portable packet",
			"configure a repo-relative workspace root or inspect the workflow locally", provenance,
		))
	}
	defaultBranch := strings.TrimSpace(wf.Orchestration.DefaultBranch)
	if defaultBranch == "" {
		defaultBranch = v7DefaultBranch(vault)
	}
	defaultSHA := ""
	if v7GitRepo(v7RepoRoot(vault)) {
		if resolved, err := gitOutputTrim(v7RepoRoot(vault), "rev-parse", "refs/heads/"+defaultBranch); err == nil {
			defaultSHA = resolved
		} else {
			unknowns = append(unknowns, deliveryContextUnknownFact(
				"project", "policy.branches.default_sha", "configured default branch commit could not be resolved", "repair the configured default branch before review", provenance,
			))
		}
	}
	gate := resolveGateTierPolicy(wf)
	locks := []string{}
	for _, lock := range deliveryContextCleanStrings(gate.BuildSlotLocks) {
		if filepath.IsAbs(lock) {
			unknowns = append(unknowns, deliveryContextUnknownFact(
				"project", "policy.gates.build_slot_locks", "an absolute build-slot path was omitted", "inspect the workflow locally", provenance,
			))
			continue
		}
		locks = append(locks, filepath.ToSlash(lock))
	}
	scopeNames := []string{}
	for _, scope := range gate.Scopes {
		if strings.TrimSpace(scope.Name) != "" {
			scopeNames = append(scopeNames, strings.TrimSpace(scope.Name))
		}
	}
	scopeNames = uniqueStrings(scopeNames)
	sort.Strings(scopeNames)
	harvestCommands, nextUnknowns := deliveryContextSanitizedCommands(gate.HarvestCommands, "policy.gates.harvest_commands", provenance, unknowns)
	unknowns = nextUnknowns
	policy := deliveryContextPolicy{
		AutomationEnabled: wf.AutomationEnabled, DispatchScope: wf.DispatchScope,
		Workspace: deliveryContextWorkspacePolicy{
			Root: root, Strategy: wf.Workspace.Strategy, MaxLiveWorktrees: wf.Workspace.MaxLiveWorktrees,
			MaxActiveRuns: wf.Agents.MaxConcurrentAgents, MaxActiveRunsPerProject: wf.Runtime.MaxActiveRunsPerProject,
		},
		Branches: deliveryContextBranchPolicy{
			DefaultBranch: defaultBranch, DefaultSHA: defaultSHA, ProtectedRefs: []string{"refs/heads/" + defaultBranch},
			TaskBranchTemplate: "task/<TASK-ID>", WaveBranchTemplate: "integration/<WAVE-ID>",
			LandingCommand: "tusker land <TASK-ID>", LandingMode: "serialized integration lane", ControlBranchWrites: true,
		},
		Gates: deliveryContextGatePolicy{
			Profile: gate.Profile, HarvestCommands: harvestCommands, BuildSlotLocks: locks,
			AllowDirtyTree: gate.AllowDirtyTree, MinFreeDiskGB: gate.MinFreeDiskGB, FocusedScopeNames: scopeNames,
		},
		ScheduledPromotion: wf.ScheduledPromotion.Effective, ReviewerEnabled: wf.Reviewer.Enabled, Provenance: provenance,
	}
	if policy.Workspace.MaxLiveWorktrees <= 0 {
		unknowns = append(unknowns, deliveryContextUnknownFact(
			"project", "policy.workspace.max_live_worktrees", "measured live-worktree capacity is not configured",
			"measure the host limit and set workspace.max_live_worktrees", provenance,
		))
	}
	return policy, unknowns
}

func deliveryContextOwnedPathClues(tasks []Note, wf Workflow, unknowns []deliveryContextUnknown) ([]deliveryContextPathClue, []deliveryContextUnknown) {
	var clues []deliveryContextPathClue
	for _, task := range tasks {
		id := stringField(task.Data, "id")
		provenance := deliveryContextNoteProvenance(task, "id", "owned_paths", "artifact_contract")
		for _, path := range normalizeList(task.Data["owned_paths"]) {
			if deliveryContextSafeRepoPath(path) {
				clues = append(clues, deliveryContextPathClue{Path: filepath.ToSlash(filepath.Clean(path)), Reason: "owned by an open matching task; check for duplicate scope", SourceID: id, Provenance: provenance})
			}
		}
		if artifact := mapField(task.Data, "artifact_contract"); artifact != nil {
			if path := stringField(artifact, "path"); deliveryContextSafeRepoPath(path) {
				clues = append(clues, deliveryContextPathClue{Path: filepath.ToSlash(filepath.Clean(path)), Reason: "operator artifact path declared by an open matching task", SourceID: id, Provenance: provenance})
			}
		}
	}
	workflowProvenance := []deliveryContextProvenance{{Kind: "workflow", Ref: ".tusker/WORKFLOW.md"}}
	for _, scope := range wf.Orchestration.Gate.Scopes {
		for _, path := range scope.Paths {
			if deliveryContextSafeRepoPath(path) {
				clues = append(clues, deliveryContextPathClue{
					Path: filepath.ToSlash(filepath.Clean(path)), Reason: "configured gate scope candidate; validate before claiming ownership",
					SourceID: strings.TrimSpace(scope.Name), Provenance: workflowProvenance,
				})
			}
		}
	}
	sort.Slice(clues, func(i, j int) bool {
		if clues[i].Path != clues[j].Path {
			return clues[i].Path < clues[j].Path
		}
		if clues[i].Reason != clues[j].Reason {
			return clues[i].Reason < clues[j].Reason
		}
		return clues[i].SourceID < clues[j].SourceID
	})
	clues = deliveryContextUniquePathClues(clues)
	if len(clues) == 0 {
		unknowns = append(unknowns, deliveryContextUnknownFact(
			"project", "likely_owned_paths", "no bounded ownership or gate-scope clues are available",
			"inspect the cited spec and declare exact task owned_paths", workflowProvenance,
		))
	}
	if len(clues) > deliveryContextMaxPathClues {
		unknowns = append(unknowns, deliveryContextUnknownFact(
			"bounded_selection", "likely_owned_paths", fmt.Sprintf("%d additional path clues were omitted at the deterministic bound", len(clues)-deliveryContextMaxPathClues),
			"narrow task ownership before planning", workflowProvenance,
		))
		clues = clues[:deliveryContextMaxPathClues]
	}
	return clues, unknowns
}

func deliveryContextResourceClues(tasks []Note, wf Workflow, provenance []deliveryContextProvenance, unknowns []deliveryContextUnknown) ([]deliveryContextResourceClue, []deliveryContextUnknown) {
	var clues []deliveryContextResourceClue
	for _, task := range tasks {
		id := stringField(task.Data, "id")
		taskProvenance := deliveryContextNoteProvenance(task, "id", "generated_outputs", "migration_keys", "resource_refs")
		for _, item := range []struct {
			kind, field, reason string
		}{
			{"generated_output", "generated_outputs", "generated output declared by an open matching task"},
			{"migration_key", "migration_keys", "migration namespace declared by an open matching task"},
			{"resource_ref", "resource_refs", "shared resource referenced by an open matching task"},
		} {
			for _, ref := range normalizeList(task.Data[item.field]) {
				if strings.TrimSpace(ref) != "" && !filepath.IsAbs(ref) {
					clues = append(clues, deliveryContextResourceClue{Kind: item.kind, Ref: strings.TrimSpace(ref), Reason: item.reason, SourceID: id, Provenance: taskProvenance})
				}
			}
		}
	}
	for _, ref := range deliveryContextCleanStrings(wf.Orchestration.SharedNamespaces) {
		if !filepath.IsAbs(ref) {
			clues = append(clues, deliveryContextResourceClue{Kind: "shared_namespace", Ref: ref, Reason: "workflow reserves collision-prone namespace ownership", Provenance: provenance})
		}
	}
	for _, ref := range deliveryContextCleanStrings(resolveGateTierPolicy(wf).BuildSlotLocks) {
		if !filepath.IsAbs(ref) {
			clues = append(clues, deliveryContextResourceClue{Kind: "build_slot_lock", Ref: ref, Reason: "workflow declares a serialized build slot", Provenance: provenance})
		}
	}
	for _, lint := range wf.Orchestration.NamespaceLints {
		if strings.TrimSpace(lint.Glob) != "" && !filepath.IsAbs(lint.Glob) {
			clues = append(clues, deliveryContextResourceClue{Kind: "generated_namespace", Ref: lint.Glob, Reason: "workflow namespace lint " + lint.Name, Provenance: provenance})
		}
	}
	sort.Slice(clues, func(i, j int) bool {
		if clues[i].Kind != clues[j].Kind {
			return clues[i].Kind < clues[j].Kind
		}
		if clues[i].Ref != clues[j].Ref {
			return clues[i].Ref < clues[j].Ref
		}
		return clues[i].SourceID < clues[j].SourceID
	})
	clues = deliveryContextUniqueResourceClues(clues)
	if len(clues) == 0 {
		unknowns = append(unknowns, deliveryContextUnknownFact(
			"project", "shared_resources", "no generated, migration, build-slot, or shared-namespace resource clues are declared",
			"declare shared_resources and overlap strategy in the delivery plan when contention exists", provenance,
		))
	}
	if len(clues) > deliveryContextMaxResources {
		unknowns = append(unknowns, deliveryContextUnknownFact(
			"bounded_selection", "shared_resources", fmt.Sprintf("%d additional resource clues were omitted at the deterministic bound", len(clues)-deliveryContextMaxResources),
			"narrow the delivery resource surface", provenance,
		))
		clues = clues[:deliveryContextMaxResources]
	}
	return clues, unknowns
}

func deliveryContextPlanSchemaContract() deliveryContextPlanContract {
	rules := deliveryPlanValidationRules()
	raw, _ := json.Marshal(rules)
	factory, _ := embeddedFactoryIntakeContractProvenance()
	return deliveryContextPlanContract{
		AuthoringSchema: deliveryPlanV2Schema, SupportedSchemas: []string{deliveryPlanV2Schema, deliveryPlanSchema},
		FactoryIntakeContract: factory,
		DoctorSchema:          "tusker.delivery-doctor/v1",
		ValidationCommand:     "tusker delivery doctor --plan <plan.yaml> --json",
		DryRunImportCommand:   "tusker delivery import --plan <plan.yaml> --dry-run --json",
		ValidationRules:       rules,
		Provenance: []deliveryContextProvenance{
			{Kind: "compiled_contract", Ref: "cmd/tusker/delivery_cmd.go", Fingerprint: deliveryFingerprint(raw)},
			{Kind: "compiled_contract", Ref: "cmd/tusker/delivery_doctor.go"},
		},
	}
}

func deliveryContextReadiness(vault string, workflowKnown bool, wf Workflow, unknowns []deliveryContextUnknown) (deliveryContextRuntimeReadiness, []deliveryContextUnknown) {
	readiness := deliveryContextRuntimeReadiness{
		RegistrationState: "unknown", DispatchAuthorized: false, NoWorkDispatched: false,
		Provenance: []deliveryContextProvenance{{Kind: "runtime_probe", Ref: "daemon.db"}},
	}
	if workflowKnown {
		readiness.AutomationEnabled = wf.AutomationEnabled
	}
	stateRoot := deliveryContextStateRoot()
	readiness.DaemonAlive = readDaemonLiveness(stateRoot, time.Now()).Alive
	store, err := OpenRuntimeStoreReadOnly(stateRoot)
	if errors.Is(err, os.ErrNotExist) {
		readiness.RegistrationState = "not_registered"
		unknowns = append(unknowns, deliveryContextUnknownFact(
			"project_registration", "readiness.registration_state", "runtime registry does not exist", "register the project before expecting resident-daemon observation",
			readiness.Provenance,
		))
		unknowns = append(unknowns, deliveryContextUnknownFact(
			"runtime_readiness", "readiness.no_work_dispatched", "project-scoped runtime rows cannot be inspected because the runtime registry does not exist",
			"initialize or inspect the runtime registry before asserting that no work was dispatched", readiness.Provenance,
		))
		return readiness, unknowns
	}
	if err != nil {
		unknowns = append(unknowns, deliveryContextUnknownFact(
			"project_registration", "readiness.registration_state", "runtime registry could not be read without mutation", "repair or inspect the runtime store",
			readiness.Provenance,
		))
		unknowns = append(unknowns, deliveryContextUnknownFact(
			"runtime_readiness", "readiness.no_work_dispatched", "project-scoped runtime rows could not be inspected",
			"repair or inspect the runtime store before asserting that no work was dispatched", readiness.Provenance,
		))
		return readiness, unknowns
	}
	defer store.Close()
	readiness.RuntimeStorePresent = true
	loadedProjects, err := loadRegisteredProjects(store, registeredProjectLoadOptions{
		MetadataOnly: true,
		LoadDisabled: true,
	})
	if err != nil {
		unknowns = append(unknowns, deliveryContextUnknownFact(
			"project_registration", "readiness.registration_state", "registered projects could not be read", "repair or inspect the runtime store",
			readiness.Provenance,
		))
		unknowns = append(unknowns, deliveryContextUnknownFact(
			"runtime_readiness", "readiness.no_work_dispatched", "project identity could not be matched to project-scoped runtime rows",
			"repair or inspect the runtime store before asserting that no work was dispatched", readiness.Provenance,
		))
		return readiness, unknowns
	}
	repo, vaultRoot := canonicalProjectPath(v7RepoRoot(vault)), canonicalProjectPath(vault)
	projectID, _ := resolveV7ProjectID(vault)
	for _, loaded := range loadedProjects {
		project := loaded.Project
		if canonicalProjectPath(project.RepoRoot) != repo && canonicalProjectPath(project.VaultRoot) != vaultRoot {
			continue
		}
		readiness.RegistrationState = "registered"
		enabled := project.Enabled
		readiness.ProjectEnabled = &enabled
		readiness.ProjectHealth = string(project.Health)
		projectID = project.ProjectID
		break
	}
	if readiness.RegistrationState != "registered" {
		readiness.RegistrationState = "not_registered"
		unknowns = append(unknowns, deliveryContextUnknownFact(
			"project_registration", "readiness.registration_state", "project is absent from the runtime registry", "register the project before expecting resident-daemon observation",
			readiness.Provenance,
		))
	}
	if strings.TrimSpace(projectID) == "" {
		unknowns = append(unknowns, deliveryContextUnknownFact(
			"runtime_readiness", "readiness.no_work_dispatched", "project identity could not be resolved for project-scoped runtime inspection",
			"configure project identity before asserting that no work was dispatched", readiness.Provenance,
		))
		return readiness, unknowns
	}
	runs, err := store.ListRuns()
	if err != nil {
		unknowns = append(unknowns, deliveryContextUnknownFact(
			"runtime_readiness", "readiness.no_work_dispatched", "project-scoped runtime rows could not be read",
			"repair or inspect the runtime store before asserting that no work was dispatched", readiness.Provenance,
		))
		return readiness, unknowns
	}
	readiness.NoWorkDispatched = true
	for _, run := range runs {
		if strings.TrimSpace(run.ProjectID) == strings.TrimSpace(projectID) {
			readiness.NoWorkDispatched = false
			break
		}
	}
	return readiness, unknowns
}

func deliveryContextMaterialFingerprint(report deliveryPlanningContext) string {
	material := report
	material.ContextFingerprint = ""
	material.Readiness.DaemonAlive = false
	material.Readiness.RuntimeStorePresent = false
	material.Readiness.RegistrationState = ""
	material.Readiness.ProjectEnabled = nil
	material.Readiness.ProjectHealth = ""
	material.Readiness.NoWorkDispatched = false
	material.Readiness.Provenance = nil
	filteredUnknowns := make([]deliveryContextUnknown, 0, len(material.Unknowns))
	for _, unknown := range material.Unknowns {
		if unknown.Kind == "project_registration" || unknown.Kind == "runtime_readiness" {
			continue
		}
		filteredUnknowns = append(filteredUnknowns, unknown)
	}
	material.Unknowns = filteredUnknowns
	raw, _ := json.Marshal(material)
	return deliveryFingerprint(raw)
}

func deliveryContextSort(report *deliveryPlanningContext) {
	sort.Slice(report.EpicCandidates, func(i, j int) bool { return report.EpicCandidates[i].ID < report.EpicCandidates[j].ID })
	sort.Slice(report.DuplicateTaskClues, func(i, j int) bool { return report.DuplicateTaskClues[i].ID < report.DuplicateTaskClues[j].ID })
	sort.Slice(report.KnowledgeDomains, func(i, j int) bool { return report.KnowledgeDomains[i].ID < report.KnowledgeDomains[j].ID })
	sort.Slice(report.HumanGates, func(i, j int) bool { return report.HumanGates[i].ID < report.HumanGates[j].ID })
	sort.Slice(report.Unknowns, func(i, j int) bool {
		if report.Unknowns[i].Kind != report.Unknowns[j].Kind {
			return report.Unknowns[i].Kind < report.Unknowns[j].Kind
		}
		if report.Unknowns[i].Field != report.Unknowns[j].Field {
			return report.Unknowns[i].Field < report.Unknowns[j].Field
		}
		return report.Unknowns[i].Reason < report.Unknowns[j].Reason
	})
	report.Unknowns = deliveryContextUniqueUnknowns(report.Unknowns)
}

func deliveryContextUnknownFact(kind, field, reason, remedy string, provenance []deliveryContextProvenance) deliveryContextUnknown {
	if provenance == nil {
		provenance = []deliveryContextProvenance{}
	}
	return deliveryContextUnknown{Kind: kind, Field: field, Reason: reason, Remedy: remedy, Provenance: provenance}
}

func deliveryContextConfigProvenance(vault string) []deliveryContextProvenance {
	repo := v7RepoRoot(vault)
	candidates := []struct {
		path, ref, kind string
	}{
		{filepath.Join(vault, "WORKFLOW.md"), ".tusker/WORKFLOW.md", "workflow"},
		{filepath.Join(repo, "tusker.yaml"), "tusker.yaml", "project_config"},
		{filepath.Join(repo, "tusker.local.yaml"), "tusker.local.yaml", "project_local_config"},
	}
	out := []deliveryContextProvenance{}
	for _, candidate := range candidates {
		if fileExists(candidate.path) {
			// Whole configuration digests are intentionally forbidden here:
			// local config can contain credentials or environment-specific
			// values. The material fingerprint is computed from the sanitized
			// projections in the report instead.
			out = append(out, deliveryContextProvenance{Kind: candidate.kind, Ref: candidate.ref})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Ref < out[j].Ref })
	return out
}

func deliveryContextNoteProvenance(note Note, fields ...string) []deliveryContextProvenance {
	selected := map[string]any{}
	for _, field := range fields {
		if value, ok := note.Data[field]; ok {
			selected[field] = value
		}
	}
	raw, _ := json.Marshal(selected)
	ref := filepath.ToSlash(filepath.Join(".tusker", note.RelativePath))
	return []deliveryContextProvenance{{Kind: "operational_frontmatter", Ref: ref, Fingerprint: deliveryFingerprint(raw)}}
}

func deliveryContextCleanStrings(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func deliveryContextSanitizedCommands(values []string, field string, provenance []deliveryContextProvenance, unknowns []deliveryContextUnknown) ([]string, []deliveryContextUnknown) {
	out := make([]string, 0, len(values))
	for _, command := range deliveryContextCleanStrings(values) {
		if deliveryContextCommandContainsSensitiveCredential(command) {
			unknowns = append(unknowns, deliveryContextUnknownFact(
				"test_command", field, "a configured command may contain a credential-bearing assignment, flag, header, or URL and was omitted",
				"replace credentials with a non-command secret injection mechanism or inspect the command locally", provenance,
			))
			continue
		}
		out = append(out, command)
	}
	return out, unknowns
}

func deliveryContextCommandContainsSensitiveCredential(command string) bool {
	for _, match := range deliverySensitiveAssignmentRegex.FindAllStringSubmatch(command, -1) {
		if len(match) > 1 && deliveryContextLiteralAssignment(match[1]) {
			return true
		}
	}
	return deliverySensitiveFlagRegex.MatchString(command) ||
		deliverySensitiveHeaderRegex.MatchString(command) ||
		deliveryURLUserinfoRegex.MatchString(command) ||
		deliveryUserCredentialFlagRegex.MatchString(command)
}

func deliveryContextLiteralAssignment(value string) bool {
	value = strings.TrimSpace(value)
	if len(value) >= 2 && ((value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'')) {
		value = strings.TrimSpace(value[1 : len(value)-1])
	}
	return value != "" && !strings.HasPrefix(value, "$")
}

func deliveryContextSafeRepoPath(path string) bool {
	path = strings.TrimSpace(path)
	if path == "" || filepath.IsAbs(path) || v7SpecRefPathEscapes(path) {
		return false
	}
	clean := filepath.ToSlash(filepath.Clean(path))
	return clean != "." && clean != ".." && !strings.HasPrefix(clean, "../")
}

func deliveryContextUniquePathClues(values []deliveryContextPathClue) []deliveryContextPathClue {
	out := values[:0]
	seen := map[string]bool{}
	for _, value := range values {
		key := value.Path + "\x00" + value.Reason + "\x00" + value.SourceID
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, value)
	}
	return out
}

func deliveryContextUniqueResourceClues(values []deliveryContextResourceClue) []deliveryContextResourceClue {
	out := values[:0]
	seen := map[string]bool{}
	for _, value := range values {
		key := value.Kind + "\x00" + value.Ref + "\x00" + value.SourceID
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, value)
	}
	return out
}

func deliveryContextUniqueUnknowns(values []deliveryContextUnknown) []deliveryContextUnknown {
	out := values[:0]
	seen := map[string]bool{}
	for _, value := range values {
		key := value.Kind + "\x00" + value.Field + "\x00" + value.Reason + "\x00" + value.Remedy
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, value)
	}
	return out
}

func emitDeliveryPlanningContext(report deliveryPlanningContext) {
	fmt.Printf("Delivery planning context %s\n", report.ContextFingerprint)
	fmt.Printf("Project: %s (repo %s, vault %s)\n", report.Project.ID, report.Project.RepoRef, report.Project.VaultRef)
	fmt.Println("Governing documents:")
	for _, doc := range append(append([]deliveryContextDocument{}, report.Specs...), report.Decisions...) {
		fmt.Printf("  - %s %s [%s]\n", doc.Kind, doc.Ref, doc.Fingerprint)
	}
	fmt.Printf("Existing scope: %d epic candidate(s), %d open duplicate task clue(s), %d relevant human gate(s)\n",
		len(report.EpicCandidates), len(report.DuplicateTaskClues), len(report.HumanGates))
	fmt.Printf("Planning inputs: %d knowledge domain(s), %d runner profile(s), %d owned-path clue(s), %d shared-resource clue(s)\n",
		len(report.KnowledgeDomains), len(report.RunnerProfiles), len(report.LikelyOwnedPaths), len(report.SharedResources))
	fmt.Printf("Policy: default=%s@%s workspace=%s/%s automation=%v daemon_alive=%v registration=%s\n",
		report.IntegrationBase.Ref, shortCommit(report.IntegrationBase.SHA), report.Policy.Workspace.Strategy, report.Policy.Workspace.Root,
		report.Readiness.AutomationEnabled, report.Readiness.DaemonAlive, report.Readiness.RegistrationState)
	if len(report.Unknowns) > 0 {
		fmt.Println("Unknowns:")
		for _, unknown := range report.Unknowns {
			fmt.Printf("  - %s: %s; remedy: %s\n", unknown.Field, unknown.Reason, unknown.Remedy)
		}
	}
	fmt.Println("Read-only planning packet. No work was dispatched.")
}
