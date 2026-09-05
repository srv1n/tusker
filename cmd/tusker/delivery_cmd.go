package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

var deliveryImportNow = time.Now
var deliveryImportRollbackWriteHook func(path string) error
var deliveryImportRollbackAfterRestoreHook func(path string)
var deliveryImportAfterPrecheckHook func()
var deliveryImportAfterWriteHook func(index int, path string)
var deliveryImportBeforeRenameHook func(path string)

type deliveryImportWriteGuard struct {
	Verify                  func() error
	SnapshotVerify          func() error
	SnapshotAdvance         func(string, []byte)
	SnapshotPaths           []string
	AfterPrecheck           func()
	DelayMutationVisibility bool
	Commit                  *deliveryImportCommit
}

type deliveryWritePreimage struct {
	Content []byte
	Mode    os.FileMode
	Existed bool
}

type deliveryImportCommit struct {
	Paths     []string
	Preimages map[string]deliveryWritePreimage
	Written   map[string][]byte
	restored  bool
}

type deliveryImportIdentityChangedError struct {
	cause error
}

func (err *deliveryImportIdentityChangedError) Error() string {
	return err.cause.Error()
}

func (err *deliveryImportIdentityChangedError) Unwrap() error {
	return err.cause
}

func markDeliveryImportIdentityChanged(cause error) error {
	if cause == nil {
		return nil
	}
	return &deliveryImportIdentityChangedError{cause: cause}
}

func isDeliveryImportIdentityChanged(err error) bool {
	var changed *deliveryImportIdentityChangedError
	return errors.As(err, &changed)
}

type deliveryPlan struct {
	Schema        string             `yaml:"schema" json:"schema"`
	Scope         string             `yaml:"scope,omitempty" json:"scope,omitempty"`
	Title         string             `yaml:"title" json:"title"`
	Epic          string             `yaml:"epic" json:"epic"`
	SpecRefs      []string           `yaml:"spec_refs" json:"spec_refs"`
	Concurrency   int                `yaml:"concurrency,omitempty" json:"concurrency,omitempty"`
	RunnerProfile string             `yaml:"runner_profile,omitempty" json:"runner_profile,omitempty"`
	Tasks         []deliveryPlanTask `yaml:"tasks" json:"tasks"`
	v2            *deliveryPlanV2
}

type deliveryPlanTask struct {
	SourceKey        string                   `yaml:"source_key" json:"source_key"`
	Title            string                   `yaml:"title" json:"title"`
	Outcome          string                   `yaml:"outcome" json:"outcome"`
	Acceptance       []deliveryAcceptance     `yaml:"acceptance" json:"acceptance"`
	Verification     []deliveryVerification   `yaml:"verification" json:"verification"`
	Dependencies     []deliveryDependency     `yaml:"dependencies,omitempty" json:"dependencies,omitempty"`
	Artifact         deliveryArtifactContract `yaml:"artifact" json:"artifact"`
	OwnedPaths       []string                 `yaml:"owned_paths,omitempty" json:"owned_paths,omitempty"`
	GeneratedOutputs []string                 `yaml:"generated_outputs,omitempty" json:"generated_outputs,omitempty"`
	MigrationKeys    []string                 `yaml:"migration_keys,omitempty" json:"migration_keys,omitempty"`
	ResourceRefs     []string                 `yaml:"resource_refs,omitempty" json:"resource_refs,omitempty"`
	RunnerProfile    string                   `yaml:"runner_profile,omitempty" json:"runner_profile,omitempty"`
	Complexity       string                   `yaml:"complexity,omitempty" json:"complexity,omitempty"`
	ConcurrencyGroup string                   `yaml:"concurrency_group,omitempty" json:"concurrency_group,omitempty"`
	KnowledgeNodes   []string                 `yaml:"knowledge_nodes,omitempty" json:"knowledge_nodes,omitempty"`
	Risk             string                   `yaml:"risk,omitempty" json:"risk,omitempty"`
	Priority         string                   `yaml:"priority,omitempty" json:"priority,omitempty"`
	Size             string                   `yaml:"size,omitempty" json:"size,omitempty"`
	Domains          []string                 `yaml:"domains,omitempty" json:"domains,omitempty"`
	RequirementRefs  []string                 `yaml:"requirement_refs,omitempty" json:"requirement_refs,omitempty"`
}

type deliveryAcceptance struct {
	ID      string `yaml:"id" json:"id"`
	Outcome string `yaml:"outcome" json:"outcome"`
}

func deliveryAcceptanceID(id string) string {
	return strings.ToUpper(strings.TrimSpace(id))
}

type deliveryVerification struct {
	Covers string `yaml:"covers" json:"covers"`
	Check  string `yaml:"check" json:"check"`
	Notes  string `yaml:"notes,omitempty" json:"notes,omitempty"`
}

type deliveryDependency struct {
	Task string `yaml:"task" json:"task"`
	Kind string `yaml:"kind,omitempty" json:"kind,omitempty"`
	// scope is populated only by the V2 decoder. Keeping it out of the shared
	// V1 wire model means V1 strict decoding still rejects scope outright.
	scope        string
	scopePresent bool
}

// deliveryCrossScopeDependency is deliberately a projection, not an alternate
// scheduler edge.  The normal dependencies field remains authoritative.
type deliveryCrossScopeDependency struct {
	Scope                     string `yaml:"scope" json:"scope"`
	Task                      string `yaml:"task" json:"task"`
	TaskID                    string `yaml:"task_id" json:"task_id"`
	Kind                      string `yaml:"kind" json:"kind"`
	TargetContractFingerprint string `yaml:"target_contract_fingerprint" json:"target_contract_fingerprint"`
}

type deliveryArtifactContract struct {
	Kind          string   `yaml:"kind" json:"kind"`
	Path          string   `yaml:"path" json:"path"`
	Summary       string   `yaml:"summary" json:"summary"`
	AcceptanceIDs []string `yaml:"acceptance_ids" json:"acceptance_ids"`
}

// deliveryPlanValidationRule is the authoring contract projected by
// `delivery context`. Keep these rules aligned with the base validator and the
// V2 doctor: a planner should not need to reverse-engineer either implementation
// before it can author a safe proposal.
type deliveryPlanValidationRule struct {
	ID          string   `json:"id"`
	Fields      []string `json:"fields"`
	Requirement string   `json:"requirement"`
	FailureCode string   `json:"failure_code"`
	Remedy      string   `json:"remedy"`
}

func deliveryPlanValidationRules() []deliveryPlanValidationRule {
	return []deliveryPlanValidationRule{
		{ID: "bounded_scope", Fields: []string{"scope", "spec_refs", "tasks"}, Requirement: "scope is stable, governing spec_refs resolve inside the repository, and tasks cover only cited requirements", FailureCode: "PLAN_CONTRACT_INVALID", Remedy: "remove unrelated runnable work and bind every task to the governing scope"},
		{ID: "planning_context", Fields: []string{"context_fingerprint"}, Requirement: "a V2 proposal records the exact bounded planning-context fingerprint used to author it", FailureCode: "PLAN_CONTRACT_INVALID", Remedy: "regenerate the delivery context for the plan scope and author its fingerprint into the plan"},
		{ID: "factory_intake_provenance", Fields: []string{"factory_intake_contract_schema", "factory_intake_contract_version", "factory_intake_contract_fingerprint"}, Requirement: "every newly imported V2 proposal records the canonical factory-intake contract schema, version, and exact content fingerprint", FailureCode: "PLAN_CONTRACT_INVALID", Remedy: "copy the factory-intake provenance emitted by the planning context; historical unclaimed waves remain readable but are not executable"},
		{ID: "source_keys", Fields: []string{"requirements[].id", "tasks[].source_key"}, Requirement: "every requirement and task has a unique stable source key", FailureCode: "PLAN_CONTRACT_INVALID", Remedy: "replace missing, duplicate, or placeholder keys with stable identifiers"},
		{ID: "requirement_coverage", Fields: []string{"requirements", "tasks[].requirement_refs"}, Requirement: "every requirement is covered by at least one task", FailureCode: "REQUIREMENT_UNCOVERED", Remedy: "map each requirement to a task with observable acceptance"},
		{ID: "acceptance_proof", Fields: []string{"tasks[].acceptance", "tasks[].verification"}, Requirement: "acceptance is observable and every acceptance ID has an exact command or manual-proof mapping", FailureCode: "ACCEPTANCE_UNMAPPED", Remedy: "add exact verification covering every acceptance ID"},
		{ID: "operator_artifact", Fields: []string{"tasks[].artifact"}, Requirement: "every task declares an operator-facing artifact at a repo-relative production path and maps it to acceptance IDs", FailureCode: "ARTIFACT_INVALID", Remedy: "declare a valid visual, behavior, reliability, security, diff, performance, or knowledge artifact"},
		{ID: "acyclic_dag", Fields: []string{"tasks[].dependencies"}, Requirement: "dependencies reference declared task keys and form an acyclic graph", FailureCode: "DEPENDENCY_CYCLE", Remedy: "remove dangling edges and redirect dependencies until the graph is acyclic"},
		{ID: "human_gate_authority", Fields: []string{"human_gates"}, Requirement: "human gates are source-keyed and name an owner, action, verification, why the agent cannot act, acceptance IDs, and affected dependency closure", FailureCode: "HUMAN_GATE_PROOF_INVALID", Remedy: "complete the human-owned proof contract and exact downstream closure"},
		{ID: "shared_resources", Fields: []string{"shared_resources", "tasks[].resource_refs"}, Requirement: "scarce shared resources are declared once with stable keys and capacity", FailureCode: "RESOURCE_UNDECLARED", Remedy: "declare every referenced resource and its measured capacity"},
		{ID: "overlap_strategy", Fields: []string{"tasks[].owned_paths", "tasks[].generated_outputs", "tasks[].migration_keys", "owned_path_overlaps"}, Requirement: "simultaneously runnable ownership collisions have an explicit serialization or downstream-integrator strategy", FailureCode: "OWNED_PATH_FRONTIER_CONFLICT", Remedy: "serialize colliding tasks or add an integrator that owns the complete collision surface"},
		{ID: "capacity", Fields: []string{"concurrency", "runner_profile"}, Requirement: "requested frontier concurrency fits configured runner, workspace, and project capacity", FailureCode: "CONCURRENCY_CAP_EXCEEDED", Remedy: "lower concurrency or configure measured capacity before import"},
		{ID: "runner_profile", Fields: []string{"runner_profile", "tasks[].runner_profile"}, Requirement: "every selected runner profile exists in the resolved project configuration", FailureCode: "UNSUPPORTED_RUNNER", Remedy: "choose a configured profile; never silently substitute an unsupported runner"},
		{ID: "assumptions", Fields: []string{"assumptions", "unresolved_decisions", "non_goals"}, Requirement: "assumptions, unresolved decisions, and non-goals remain explicitly labeled and are not presented as accepted facts", FailureCode: "ASSUMPTION_PRESENTED_AS_FACT", Remedy: "resolve the fact through a decision or keep it only in the labeled assumption/non-goal set"},
	}
}

type deliveryImportReport struct {
	PlanFingerprint           string                                    `json:"planFingerprint"`
	PlanScope                 string                                    `json:"planScope"`
	WaveID                    string                                    `json:"waveId"`
	WaveTitle                 string                                    `json:"waveTitle"`
	SpecRefs                  []string                                  `json:"specRefs"`
	TaskMapping               map[string]string                         `json:"taskMapping"`
	Frontiers                 [][]string                                `json:"frontiers"`
	ExpectedConcurrency       int                                       `json:"expectedConcurrency"`
	Issues                    []string                                  `json:"issues"`
	DryRun                    bool                                      `json:"dryRun"`
	CrossScopeDependencies    map[string][]deliveryCrossScopeDependency `json:"-"`
	CrossScopeSnapshot        func() error                              `json:"-"`
	CrossScopeSnapshotAdvance func(string, []byte)                      `json:"-"`
	CrossScopeSnapshotPaths   []string                                  `json:"-"`
	V2Index                   *v7Index                                  `json:"-"`
}

func deliveryPlanCmd(args Args) error {
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	spec := v7CleanSpecRef(firstNonEmpty(args.String("spec"), args.String("_pos0")))
	if spec == "" {
		return tuskerError(errorMissingArg, "Usage: tusker delivery plan --spec <repo-relative-spec> --out <plan.yaml>")
	}
	if !deliveryRepoPathExists(vaultPath, spec) {
		return tuskerError(errorNotFound, "delivery spec does not resolve inside the repository: "+spec)
	}
	scope := deliveryGeneratedScope(spec)
	context, err := buildDeliveryPlanningContextForScope(vaultPath, spec, scope)
	if err != nil {
		return err
	}
	factory := context.PlanContract.FactoryIntakeContract
	plan := deliveryPlanV2{
		Schema: deliveryPlanV2Schema, Scope: scope, Title: strings.TrimSuffix(filepath.Base(spec), filepath.Ext(spec)),
		SpecRefs: []string{spec}, ContextFingerprint: context.ContextFingerprint,
		FactoryIntakeContractSchema: factory.Schema, FactoryIntakeContractVersion: factory.Version, FactoryIntakeContractFingerprint: factory.Fingerprint,
		Summary:      "Replace bounded authoring markers with the reviewed delivery contract before doctor or import.",
		EpicContract: &deliveryEpicContract{SourceKey: "replace-me-epic", AcronymHint: "REP", Title: "Replace with the delivery epic title"},
		Requirements: []deliveryRequirement{
			{ID: "R1", Outcome: "Replace with the first observable requirement."},
			{ID: "R2", Outcome: "Replace with the dependent observable requirement."},
		},
		Concurrency: 1,
		Tasks: []deliveryPlanTask{
			{
				SourceKey: "replace-me-first", RequirementRefs: []string{"R1"}, Title: "Replace with the first observable task", Outcome: "Replace with the first observable outcome.",
				Acceptance:   []deliveryAcceptance{{ID: "A1", Outcome: "Replace with concrete observable acceptance."}},
				Verification: []deliveryVerification{{Covers: "A1", Check: "command: replace-with-an-exact-command"}},
				Artifact:     deliveryArtifactContract{Kind: "diff_summary", Path: "replace/with/owned/path.go", Summary: "Replace with the compact operator artifact.", AcceptanceIDs: []string{"A1"}},
				OwnedPaths:   []string{"replace/with/owned/path.go"},
				Complexity:   "standard",
			},
			{
				SourceKey: "replace-me-dependent", RequirementRefs: []string{"R2"}, Title: "Replace with the dependent observable task", Outcome: "Replace with the dependent observable outcome.",
				Acceptance:   []deliveryAcceptance{{ID: "A1", Outcome: "Replace with concrete dependent acceptance."}},
				Verification: []deliveryVerification{{Covers: "A1", Check: "command: replace-with-an-exact-command"}},
				Artifact:     deliveryArtifactContract{Kind: "diff_summary", Path: "replace/with/dependent-owned-path.go", Summary: "Replace with the dependent operator artifact.", AcceptanceIDs: []string{"A1"}},
				OwnedPaths:   []string{"replace/with/dependent-owned-path.go"},
				Dependencies: []deliveryDependency{{Task: "replace-me-first", Kind: "hard"}},
			},
		},
	}
	if epic := strings.ToUpper(strings.TrimSpace(args.String("epic"))); epic != "" {
		plan.Epic = epic
		plan.EpicContract = nil
	}
	raw, err := yaml.Marshal(plan)
	if err != nil {
		return err
	}
	out := strings.TrimSpace(args.String("out"))
	if out == "" {
		fmt.Print(string(raw))
		return nil
	}
	if !filepath.IsAbs(out) {
		out = filepath.Join(v7RepoRoot(vaultPath), out)
	}
	if err := secureScratchWriteText(vaultPath, out, string(raw)); err != nil {
		return err
	}
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": true, "plan": out, "schema": deliveryPlanV2Schema, "inert": true})
	} else if !args.Bool("quiet") {
		fmt.Printf("Wrote inert delivery-plan template to %s\n", out)
	}
	return nil
}

func deliveryImportCmd(args Args) error {
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	if !args.Bool("dry-run") {
		if err := ensureV7ControlMutation(vaultPath, args); err != nil {
			return err
		}
	}
	planPath := strings.TrimSpace(firstNonEmpty(args.String("plan"), args.String("_pos0")))
	if planPath == "" {
		return tuskerError(errorMissingArg, "Usage: tusker delivery import --plan <plan.yaml> [--wave <title>] [--dry-run]")
	}
	if !filepath.IsAbs(planPath) {
		planPath = filepath.Join(v7RepoRoot(vaultPath), planPath)
	}
	schema, err := deliveryPlanSchemaAt(planPath)
	if err != nil {
		return err
	}
	if schema != deliveryPlanV2Schema {
		return tuskerError(errorInvalidArg, "unsupported delivery plan schema: "+fallback(schema, "<missing>")+"; expected "+deliveryPlanV2Schema)
	}
	return deliveryV2ImportCmd(vaultPath, planPath, args)
}

type deliveryIssue struct {
	Code    string
	Message string
}

func deliveryIssueMessages(issues []deliveryIssue) []string {
	messages := make([]string, 0, len(issues))
	for _, issue := range issues {
		messages = append(messages, issue.Message)
	}
	return uniqueStrings(messages)
}

func uniqueDeliveryIssues(issues []deliveryIssue) []deliveryIssue {
	seen := map[string]struct{}{}
	out := make([]deliveryIssue, 0, len(issues))
	for _, issue := range issues {
		issue.Message = strings.TrimSpace(issue.Message)
		if issue.Message == "" {
			continue
		}
		key := issue.Code + "\x00" + issue.Message
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, issue)
	}
	return out
}

func validateDeliveryPlan(vaultPath string, plan deliveryPlan) ([]deliveryIssue, [][]string) {
	var issues []deliveryIssue
	expectedSchema := deliveryPlanV2Schema
	if plan.Schema != expectedSchema {
		issues = append(issues, deliveryIssue{Code: "PLAN_CONTRACT_INVALID", Message: "schema must be " + expectedSchema})
	}
	if deliveryPlaceholder(plan.Scope) || !deliveryScopeValid(plan.Scope) {
		issues = append(issues, deliveryIssue{Code: "PLAN_CONTRACT_INVALID", Message: "scope must be an explicit stable identifier using letters, numbers, dot, underscore, slash, colon, or hyphen"})
	}
	if !epicAcronymPattern.MatchString(strings.ToUpper(plan.Epic)) {
		issues = append(issues, deliveryIssue{Code: "PLAN_CONTRACT_INVALID", Message: "epic must name an existing three-letter V7 epic"})
	} else if plan.v2 == nil || (plan.v2.EpicContract == nil && !fileExists(filepath.Join(vaultPath, "work", "epics", strings.ToUpper(plan.Epic)+".md"))) {
		issues = append(issues, deliveryIssue{Code: "PLAN_CONTRACT_INVALID", Message: "epic does not exist: " + strings.ToUpper(plan.Epic)})
	}
	if len(plan.SpecRefs) == 0 {
		issues = append(issues, deliveryIssue{Code: "PLAN_CONTRACT_INVALID", Message: "at least one governing spec_ref is required"})
	}
	for _, ref := range plan.SpecRefs {
		if !deliverySpecRefExists(vaultPath, ref) {
			issues = append(issues, deliveryIssue{Code: "PLAN_CONTRACT_INVALID", Message: "spec_ref does not resolve inside the repository: " + ref})
		}
	}
	if len(plan.Tasks) == 0 {
		issues = append(issues, deliveryIssue{Code: "PLAN_CONTRACT_INVALID", Message: "at least one task is required"})
	}
	keys := map[string]bool{}
	for _, task := range plan.Tasks {
		key := strings.TrimSpace(task.SourceKey)
		if key == "" || deliveryPlaceholder(key) {
			issues = append(issues, deliveryIssue{Code: "PLAN_CONTRACT_INVALID", Message: "every task requires a stable non-placeholder source_key"})
		} else if keys[key] {
			issues = append(issues, deliveryIssue{Code: "PLAN_CONTRACT_INVALID", Message: "duplicate source_key: " + key})
		}
		keys[key] = true
		if deliveryPlaceholder(task.Title) || deliveryPlaceholder(task.Outcome) {
			issues = append(issues, deliveryIssue{Code: "PLAN_CONTRACT_INVALID", Message: key + ": title and outcome must be concrete"})
		}
		acceptance := map[string]bool{}
		covered := map[string]bool{}
		for _, row := range task.Acceptance {
			if strings.TrimSpace(row.ID) == "" || deliveryPlaceholder(row.Outcome) {
				issues = append(issues, deliveryIssue{Code: "ACCEPTANCE_INVALID", Message: key + ": acceptance rows require an id and concrete outcome"})
			}
			id := deliveryAcceptanceID(row.ID)
			if acceptance[id] {
				issues = append(issues, deliveryIssue{Code: "ACCEPTANCE_INVALID", Message: key + ": duplicate acceptance id " + row.ID})
			}
			acceptance[id] = true
		}
		if len(task.Acceptance) == 0 {
			issues = append(issues, deliveryIssue{Code: "PLAN_CONTRACT_INVALID", Message: key + ": acceptance is required"})
		}
		for _, row := range task.Verification {
			check := strings.TrimSpace(row.Check)
			if deliveryPlaceholder(check) || (!strings.HasPrefix(check, "command: ") && !strings.HasPrefix(check, "manual proof: ")) {
				issues = append(issues, deliveryIssue{Code: "PROOF_UNSUPPORTED", Message: key + ": verification must use an exact command: or manual proof: check"})
			}
			for _, cover := range splitCSV(row.Covers) {
				cover = deliveryAcceptanceID(cover)
				if !acceptance[cover] {
					issues = append(issues, deliveryIssue{Code: "PROOF_ACCEPTANCE_UNKNOWN", Message: key + ": verification references unknown acceptance " + cover})
				}
				covered[cover] = true
			}
		}
		if len(task.Verification) == 0 {
			issues = append(issues, deliveryIssue{Code: "PROOF_UNFILLED", Message: key + ": verification is required"})
		}
		for id := range acceptance {
			if !covered[id] {
				issues = append(issues, deliveryIssue{Code: "ACCEPTANCE_UNMAPPED", Message: key + ": acceptance " + id + " has no mapped verification"})
			}
		}
		if deliveryPlaceholder(task.Artifact.Kind) || deliveryInvalidProductionPath(task.Artifact.Path) || deliveryPlaceholder(task.Artifact.Summary) {
			issues = append(issues, deliveryIssue{Code: "ARTIFACT_INVALID", Message: key + ": artifact requires kind, summary, and a repo-relative production path"})
		} else if _, ok := v7OperatorArtifactKinds[strings.ToLower(strings.TrimSpace(task.Artifact.Kind))]; !ok {
			issues = append(issues, deliveryIssue{Code: "ARTIFACT_INVALID", Message: key + ": artifact kind is not an operator-facing visual, performance, behavior, reliability, security, diff, or knowledge artifact"})
		}
		if len(task.Artifact.AcceptanceIDs) == 0 {
			issues = append(issues, deliveryIssue{Code: "ARTIFACT_INVALID", Message: key + ": artifact acceptance_ids must name at least one task acceptance outcome"})
		} else {
			for _, id := range task.Artifact.AcceptanceIDs {
				if !acceptance[deliveryAcceptanceID(id)] {
					issues = append(issues, deliveryIssue{Code: "ARTIFACT_INVALID", Message: key + ": artifact acceptance_ids references unknown acceptance " + id})
				}
			}
		}
		if task.Risk != "" {
			if _, ok := risks[strings.ToLower(task.Risk)]; !ok {
				issues = append(issues, deliveryIssue{Code: "PLAN_CONTRACT_INVALID", Message: key + ": invalid risk " + task.Risk})
			}
		}
		if task.Priority != "" {
			if _, ok := priorities[strings.ToLower(task.Priority)]; !ok {
				issues = append(issues, deliveryIssue{Code: "PLAN_CONTRACT_INVALID", Message: key + ": invalid priority " + task.Priority})
			}
		}
		if task.Size != "" {
			if _, ok := sizes[strings.ToLower(task.Size)]; !ok {
				issues = append(issues, deliveryIssue{Code: "PLAN_CONTRACT_INVALID", Message: key + ": invalid size " + task.Size})
			}
		}
		if task.Complexity != "" {
			if _, ok := map[string]bool{"routine": true, "standard": true, "complex": true, "frontier": true}[strings.ToLower(task.Complexity)]; !ok {
				issues = append(issues, deliveryIssue{Code: "PLAN_CONTRACT_INVALID", Message: key + ": invalid complexity " + task.Complexity})
			}
		}
		qualifiedSeen := map[string]bool{}
		for _, dep := range task.Dependencies {
			kind := fallback(strings.ToLower(strings.TrimSpace(dep.Kind)), "hard")
			if kind != "hard" && kind != "soft" {
				issues = append(issues, deliveryIssue{Code: "PLAN_CONTRACT_INVALID", Message: key + ": dependency kind must be hard or soft"})
			}
			if dep.scopePresent {
				if strings.TrimSpace(dep.scope) == "" {
					issues = append(issues, deliveryIssue{Code: "PLAN_CONTRACT_INVALID", Message: key + ": CROSS_SCOPE_INVALID_SCOPE; supplied scope must be non-blank"})
					continue
				}
				if !deliveryScopeValid(dep.scope) {
					issues = append(issues, deliveryIssue{Code: "PLAN_CONTRACT_INVALID", Message: key + ": CROSS_SCOPE_INVALID_SCOPE " + dep.scope + "; use a stable producer scope"})
				}
				if strings.TrimSpace(dep.scope) == strings.TrimSpace(plan.Scope) {
					issues = append(issues, deliveryIssue{Code: "PLAN_CONTRACT_INVALID", Message: key + ": CROSS_SCOPE_SAME_SCOPE " + dep.scope + "; omit scope for local dependencies"})
				}
				semantic := strings.TrimSpace(dep.scope) + "\x00" + strings.TrimSpace(dep.Task)
				if qualifiedSeen[semantic] {
					issues = append(issues, deliveryIssue{Code: "PLAN_CONTRACT_INVALID", Message: key + ": CROSS_SCOPE_DUPLICATE_DEPENDENCY " + dep.scope + "/" + dep.Task})
				}
				qualifiedSeen[semantic] = true
				if kind != "hard" {
					issues = append(issues, deliveryIssue{Code: "PLAN_CONTRACT_INVALID", Message: key + ": CROSS_SCOPE_HARD_ONLY " + dep.scope + "/" + dep.Task + "; use kind: hard"})
				}
			}
		}
	}
	for _, task := range plan.Tasks {
		for _, dep := range task.Dependencies {
			if strings.TrimSpace(dep.scope) != "" {
				continue
			}
			if !keys[dep.Task] {
				issues = append(issues, deliveryIssue{Code: "DEPENDENCY_DANGLING", Message: task.SourceKey + ": dangling dependency " + dep.Task})
			}
		}
	}
	frontiers, cycle := deliveryFrontiers(plan)
	if cycle {
		issues = append(issues, deliveryIssue{Code: "DEPENDENCY_CYCLE", Message: "task dependency graph contains a cycle"})
	}
	return uniqueDeliveryIssues(issues), frontiers
}

func deliveryTaskMappingFromIndex(idx v7Index, plan deliveryPlan) (map[string]string, string, error) {
	mapping := map[string]string{}
	waveID := ""
	scope := deliveryPlanScope(plan)
	for id, wave := range idx.Waves {
		if stringField(wave.Data, "delivery_plan_scope") != scope {
			continue
		}
		if waveID != "" && waveID != id {
			return nil, "", tuskerError(errorInvalidArg, "multiple waves share delivery plan scope "+scope)
		}
		if stringField(wave.Data, "status") != "open" {
			return nil, "", tuskerError(errorInvalidTransition, "delivery plan belongs to terminal wave "+id+"; import cannot reopen it")
		}
		waveID = id
	}
	used := map[string]bool{}
	wanted := map[string]bool{}
	for _, task := range plan.Tasks {
		wanted[task.SourceKey] = true
	}
	maxSeq := 0
	for id, task := range idx.Tasks {
		used[id] = true
		if match := v7TaskIDPattern.FindStringSubmatch(id); match != nil && match[1] == strings.ToUpper(plan.Epic) {
			maxSeq = maxInt(maxSeq, atoiSafe(match[2]))
		}
		key := stringField(task.Data, "delivery_source_key")
		taskScope := stringField(task.Data, "delivery_plan_scope")
		if wanted[key] && taskScope == scope && stringField(task.Data, "epic") != strings.ToUpper(plan.Epic) {
			return nil, "", tuskerError(errorInvalidArg, "delivery source_key collision "+key+" belongs to epic "+stringField(task.Data, "epic")+" in plan scope "+scope)
		}
		sameScope := taskScope == scope || (taskScope == "" && deliveryRefsOverlap(normalizeList(task.Data["spec_refs"]), plan.SpecRefs))
		if wanted[key] && stringField(task.Data, "epic") == strings.ToUpper(plan.Epic) && sameScope {
			if previous := mapping[key]; previous != "" && previous != id {
				return nil, "", tuskerError(errorInvalidArg, "multiple tasks share delivery source_key "+key+" in the same plan scope")
			}
			mapping[key] = id
			if current := stringField(task.Data, "wave"); current != "" {
				if wave, ok := idx.Waves[current]; ok && stringField(wave.Data, "status") != "open" {
					return nil, "", tuskerError(errorInvalidTransition, "delivery plan belongs to terminal wave "+current+"; import cannot reopen it")
				}
				if waveID != "" && waveID != current {
					return nil, "", tuskerError(errorInvalidArg, "delivery source tasks belong to multiple waves")
				}
				waveID = current
			}
		}
	}
	for _, task := range plan.Tasks {
		if mapping[task.SourceKey] != "" {
			continue
		}
		for {
			maxSeq++
			id := fmt.Sprintf("%s-T-%s", strings.ToUpper(plan.Epic), padNumber(maxSeq))
			if !used[id] {
				mapping[task.SourceKey] = id
				used[id] = true
				break
			}
		}
	}
	return mapping, waveID, nil
}

func deliveryFrontiers(plan deliveryPlan) ([][]string, bool) {
	indegree := map[string]int{}
	next := map[string][]string{}
	for _, task := range plan.Tasks {
		indegree[task.SourceKey] = 0
	}
	for _, task := range plan.Tasks {
		for _, dep := range task.Dependencies {
			if _, ok := indegree[dep.Task]; !ok {
				continue
			}
			indegree[task.SourceKey]++
			next[dep.Task] = append(next[dep.Task], task.SourceKey)
		}
	}
	var frontiers [][]string
	seen := 0
	for {
		var frontier []string
		for key, degree := range indegree {
			if degree == 0 {
				frontier = append(frontier, key)
			}
		}
		if len(frontier) == 0 {
			break
		}
		sort.Strings(frontier)
		frontiers = append(frontiers, frontier)
		for _, key := range frontier {
			delete(indegree, key)
			seen++
			for _, dependent := range next[key] {
				indegree[dependent]--
			}
		}
	}
	return frontiers, seen != len(plan.Tasks)
}

// deliveryWaveContractFrozen reports whether a prior import has crossed the
// boundary at which changing its plan would rewrite execution history. The
// integration base is only a snapshot selected at import time; it is not that
// boundary by itself.
func deliveryWaveContractFrozen(wave map[string]any, idx v7Index) bool {
	if fallback(stringField(wave, "status"), "open") != "open" || fallback(stringField(wave, "authorization"), "disarmed") != "disarmed" {
		return true
	}
	for _, field := range []string{"authorization_fingerprint", "authorized_by", "authorized_at", "authorization_reason", "authorization_updated_by", "authorization_updated_at"} {
		if strings.TrimSpace(stringField(wave, field)) != "" {
			return true
		}
	}
	members := normalizeList(wave["members"])
	if len(members) == 0 {
		return true
	}
	for _, taskID := range members {
		task, ok := idx.Tasks[taskID]
		if !ok || fallback(stringField(task.Data, "status"), "backlog") != "backlog" || fallback(stringField(task.Data, "readiness"), "held") != "held" || len(idx.Attempts[taskID]) > 0 {
			return true
		}
	}
	return false
}

func applyDeliveryImportGuarded(vaultPath string, plan deliveryPlan, report deliveryImportReport, args Args, guard *deliveryImportWriteGuard) error {
	var materialLock *v7DocumentLock
	var err error
	if !args.Bool("material-lock-held") {
		materialLock, err = acquireV7MaterialEpochLock(vaultPath)
		if err != nil {
			return err
		}
		defer materialLock.Close()
	}
	if err := ensureDeliveryWorkNamespaces(vaultPath); err != nil {
		return err
	}
	now := deliveryImportNow().UTC().Format(time.RFC3339)
	actor := fallback(firstNonEmpty(args.String("by"), args.String("actor")), "agent:"+defaultActorName())
	integrationBase, err := deliveryIntegrationBaseSHA(vaultPath)
	if err != nil {
		return err
	}
	if expected := strings.TrimSpace(args.String("expected-integration-base-sha")); expected != "" && integrationBase != expected {
		return tuskerError(errorInvalidTransition, "configured default branch changed after review; regenerate delivery context and review before Start")
	}
	wavePath := filepath.Join(vaultPath, "work", "waves", report.WaveID+".md")
	if fileExists(wavePath) {
		existingWave, _, readErr := parseFrontmatterMustRead(wavePath)
		if readErr != nil {
			return readErr
		}
		frozen := stringField(existingWave, "integration_base_sha")
		if stringField(existingWave, "delivery_plan_fingerprint") != report.PlanFingerprint {
			idx := v7Index{}
			if report.V2Index != nil {
				idx = *report.V2Index
			}
			if deliveryWaveContractFrozen(existingWave, idx) {
				return tuskerError(errorInvalidTransition, "existing delivery scope is frozen to a different reviewed plan; use a new plan scope/wave or perform an explicit controlled rebase")
			}
			// A held/disarmed wave has only selected the old base. An ordinary
			// amendment is reviewed against the current base instead.
		} else if frozen != "" {
			integrationBase = frozen
		}
	}
	writes := map[string]string{}
	for _, task := range plan.Tasks {
		id := report.TaskMapping[task.SourceKey]
		path := filepath.Join(vaultPath, "work", "tasks", id+".md")
		data := map[string]any{}
		var existing map[string]any
		createdAt := now
		createdBy := actor
		status, readiness := "backlog", "held"
		if fileExists(path) {
			parsed, _, err := parseFrontmatterMustRead(path)
			if err != nil {
				return err
			}
			existing = parsed
			data = existing
			createdAt = fallback(stringField(existing, "created_at"), now)
			createdBy = fallback(stringField(existing, "created_by"), actor)
			status = fallback(stringField(existing, "status"), status)
			readiness = fallback(stringField(existing, "readiness"), readiness)
		}
		deps := make([]string, 0, len(task.Dependencies))
		for _, dep := range task.Dependencies {
			target := report.TaskMapping[dep.Task]
			if strings.TrimSpace(dep.scope) != "" {
				for _, projection := range report.CrossScopeDependencies[task.SourceKey] {
					if projection.Scope == strings.TrimSpace(dep.scope) && projection.Task == dep.Task {
						target = projection.TaskID
						break
					}
				}
			}
			deps = append(deps, target+":"+fallback(strings.ToLower(dep.Kind), "hard"))
		}
		contractFingerprint := deliveryV2TaskFingerprint(task, plan.v2.HumanGates)
		var strictContract deliveryProofContract
		strictRequested := deliveryPlanRequiresStrictProofAuthority(plan)
		if strictRequested {
			var err error
			strictContract, err = deliveryCanonicalProofContract(*plan.v2, task)
			if err != nil {
				return tuskerError(errorInvalidArg, err.Error())
			}
		}
		if existing != nil && (status != "backlog" || readiness != "held") && stringField(existing, "delivery_contract_fingerprint") != contractFingerprint {
			return tuskerError(errorInvalidTransition, id+" has progressed beyond held state; changed delivery contract requires an explicit rework/control transition")
		}
		data = map[string]any{
			"schema": "tusker.task/v7", "kind": "task", "id": id, "project": v7ProjectID(vaultPath),
			"title": task.Title, "epic": strings.ToUpper(plan.Epic), "status": status, "readiness": readiness,
			"priority": fallback(strings.ToLower(task.Priority), "p2"), "risk": fallback(strings.ToLower(task.Risk), "medium"), "size": fallback(strings.ToLower(task.Size), "m"),
			"proof_mode": "inline", "proof_status": "pending", "proof_required": []string{"focused_test"}, "evidence_budget": 0,
			"raw_artifacts_allowed": false, "next_owner": "agent", "next_source": "task", "next_ref": id,
			"next_action": "Execute the imported delivery contract and satisfy proof mode.", "domains": task.Domains,
			"spec_refs": plan.SpecRefs, "dependencies": deps, "delivery_source_key": task.SourceKey, "delivery_plan_scope": report.PlanScope, "delivery_contract_fingerprint": contractFingerprint,
			"artifact_contract": map[string]any{"kind": task.Artifact.Kind, "path": task.Artifact.Path, "summary": task.Artifact.Summary, "acceptance_ids": task.Artifact.AcceptanceIDs},
			"owned_paths":       task.OwnedPaths, "runner_profile": firstNonEmpty(task.RunnerProfile, plan.RunnerProfile),
			"concurrency_group": task.ConcurrencyGroup, "knowledge_nodes": task.KnowledgeNodes, "wave": report.WaveID,
			"created_at": createdAt, "created_by": createdBy, "updated_at": now, "updated_by": actor,
		}
		if complexity := strings.ToLower(strings.TrimSpace(task.Complexity)); complexity != "" {
			data["complexity"] = complexity
		}
		if len(task.GeneratedOutputs) > 0 {
			data["generated_outputs"] = task.GeneratedOutputs
		}
		if len(task.MigrationKeys) > 0 {
			data["migration_keys"] = task.MigrationKeys
		}
		if len(task.ResourceRefs) > 0 {
			data["resource_refs"] = task.ResourceRefs
		}
		if len(task.RequirementRefs) > 0 {
			data["requirement_refs"] = task.RequirementRefs
		}
		data["gates"] = deliveryV2TaskGateIDs(plan.v2, task.SourceKey)
		if projections := report.CrossScopeDependencies[task.SourceKey]; len(projections) > 0 {
			data["delivery_cross_scope_dependencies"] = deliveryCrossScopeProjectionValue(projections)
		}
		if strictRequested {
			held := status == "backlog" && readiness == "held"
			if err := deliveryInstallStrictProofContract(data, existing, held, strictContract); err != nil {
				return tuskerError(errorInvalidTransition, id+": "+err.Error())
			}
			lineage, err := deliveryStrictLineageFor(*plan.v2, task)
			if err != nil {
				return tuskerError(errorInvalidArg, id+": "+err.Error())
			}
			data["delivery_strict_import_lineage"] = lineage
			data["delivery_strict_import_lineage_fingerprint"] = lineage.Fingerprint
		}
		if existing != nil && (status != "backlog" || readiness != "held") {
			for _, field := range []string{"proof_status", "proof_required", "proof_required_owner", "evidence_budget", "gates", "evidence_required", "machine_status", "human_status", "closeout_status", "agent_action", "next_owner", "next_source", "next_ref", "next_action", "accepted_by", "accepted_at", "closed_at", "close_authority"} {
				if value, ok := existing[field]; ok {
					data[field] = value
				}
			}
		}
		body := renderDeliveryTaskBody(id, task)
		if len(plan.v2.NonGoals) > 0 {
			body += "\n## Non-goals\n\n" + v7BulletList(plan.v2.NonGoals) + "\n"
		}
		data["state_rev"] = v7StateRev(data, body)
		content, err := serializeDocument(data, body, v7FrontmatterOrder["task"])
		if err != nil {
			return err
		}
		writes[path] = content
	}
	// A producer's contract fingerprint is part of each inbound semantic
	// projection. Refresh the complete inbound closure here, before any write,
	// so a failed or corrupt consumer cannot leave a partially rewritten graph.
	if report.V2Index == nil {
		return tuskerError(errorInvalidTransition, "V2 import missing locked index epoch")
	}
	idx := *report.V2Index
	if err := deliveryRefreshInboundProjectionWrites(vaultPath, idx, plan, report, writes, now, actor); err != nil {
		return err
	}
	waveCreatedAt := now
	waveCreatedBy := actor
	var previousMembers []string
	var previousSpecRefs []string
	if fileExists(wavePath) {
		data, _, err := parseFrontmatterMustRead(wavePath)
		if err != nil {
			return err
		}
		waveCreatedAt = fallback(stringField(data, "created_at"), now)
		waveCreatedBy = fallback(stringField(data, "created_by"), actor)
		previousMembers = normalizeList(data["members"])
		previousSpecRefs = normalizeList(data["spec_refs"])
	}
	members := make([]string, 0, len(plan.Tasks))
	for _, task := range plan.Tasks {
		members = append(members, report.TaskMapping[task.SourceKey])
	}
	memberSet := makeSet(members...)
	for _, previous := range previousMembers {
		if _, retained := memberSet[previous]; retained {
			continue
		}
		path := filepath.Join(vaultPath, "work", "tasks", previous+".md")
		data, body, err := parseFrontmatterMustRead(path)
		if err != nil {
			return err
		}
		if stringField(data, "wave") != report.WaveID {
			continue
		}
		delete(data, "wave")
		data["updated_at"] = now
		data["updated_by"] = actor
		data["state_rev"] = v7StateRev(data, body)
		content, err := serializeDocument(data, body, v7FrontmatterOrder["task"])
		if err != nil {
			return err
		}
		writes[path] = content
	}
	waveData := map[string]any{
		"schema": "tusker.wave/v7", "kind": "wave", "id": report.WaveID, "project": v7ProjectID(vaultPath),
		"title": report.WaveTitle, "status": "open", "authorization": "disarmed", "members": members, "integration_branch": v7IntegrationBranchName(report.WaveID),
		"spec_refs": plan.SpecRefs, "delivery_plan_schema": plan.Schema, "delivery_plan_scope": report.PlanScope, "delivery_plan_fingerprint": report.PlanFingerprint, "concurrency": report.ExpectedConcurrency,
		"runner_profile": plan.RunnerProfile, "created_at": waveCreatedAt, "created_by": waveCreatedBy, "updated_at": now, "updated_by": actor,
	}
	if integrationBase != "" {
		// This is a snapshot of the configured default ref. Held waves may
		// refresh it on an ordinary amendment; Start never creates the ref.
		waveData["integration_base_sha"] = integrationBase
	}
	contractData, err := deliveryV2WaveContractData(plan.v2)
	if err != nil {
		return err
	}
	for field, value := range contractData {
		waveData[field] = value
	}
	if deliveryPlanRequiresStrictProofAuthority(plan) {
		lineage, err := deliveryStrictWaveLineageFor(plan, report)
		if err != nil {
			return tuskerError(errorInvalidArg, err.Error())
		}
		waveData["delivery_strict_import_lineage"] = lineage
		waveData["delivery_strict_import_lineage_fingerprint"] = lineage.Fingerprint
	}
	if fileExists(wavePath) {
		previous, _, _ := parseFrontmatterMustRead(wavePath)
		for _, field := range []string{"authorization", "authorization_fingerprint", "authorized_by", "authorized_at", "authorization_reason", "authorization_updated_by", "authorization_updated_at"} {
			if value, ok := previous[field]; ok {
				waveData[field] = value
			}
		}
	}
	waveBody := fmt.Sprintf("# %s · %s\n\n## Members\n\nImported atomically from delivery plan `%s`. Members remain held until wave preflight and authorization.\n", report.WaveID, report.WaveTitle, report.PlanFingerprint)
	waveData["state_rev"] = v7StateRev(waveData, waveBody)
	waveContent, err := serializeDocument(waveData, waveBody, v7FrontmatterOrder["wave"])
	if err != nil {
		return err
	}
	writes[wavePath] = waveContent
	if err := deliveryV2WriteExtras(vaultPath, plan, report, writes, now, actor); err != nil {
		return err
	}
	currentRefs := makeSet(plan.SpecRefs...)
	allRefs := uniqueStrings(append(append([]string{}, previousSpecRefs...), plan.SpecRefs...))
	for _, ref := range allRefs {
		specPath := deliverySpecRefPath(vaultPath, ref)
		if specPath == "" || !fileExists(specPath) {
			continue
		}
		content, err := readText(specPath)
		if err != nil {
			return err
		}
		_, retain := currentRefs[ref]
		if retain {
			content = renderDeliveryWorkStreams(content, report)
		} else {
			content = removeDeliveryWorkStreams(content, report.PlanScope)
		}
		if v7SpecRefDecisionID(ref) != "" || strings.HasPrefix(v7CleanSpecRef(ref), "work/decisions/") {
			data, body, err := parseFrontmatter(content)
			if err != nil {
				return err
			}
			data["updated_at"] = now
			data["updated_by"] = actor
			data["state_rev"] = v7StateRev(data, body)
			content, err = serializeDocument(data, body, v7FrontmatterOrder["decision"])
			if err != nil {
				return err
			}
		}
		writes[specPath] = content
	}
	if err := convergeUnchangedDeliveryWrites(writes); err != nil {
		return err
	}
	// Import records a frozen integration base but is never an integration
	// operation. In particular, it must not create the named integration ref:
	// the serialized completion lane creates that ref later with its create-only
	// CAS, after the wave has actually completed its required work.
	failAfter := 0
	if args.Bool("fail-after-first-write") {
		failAfter = 1
	}
	if report.CrossScopeSnapshot != nil {
		if guard == nil {
			guard = &deliveryImportWriteGuard{}
		}
		previousSnapshot := guard.SnapshotVerify
		guard.SnapshotVerify = func() error {
			if err := report.CrossScopeSnapshot(); err != nil {
				return err
			}
			if previousSnapshot != nil {
				return previousSnapshot()
			}
			return nil
		}
		guard.SnapshotPaths = append(guard.SnapshotPaths, report.CrossScopeSnapshotPaths...)
		guard.SnapshotAdvance = report.CrossScopeSnapshotAdvance
	}
	if err := commitDeliveryWritesGuarded(writes, failAfter, guard); err != nil {
		return err
	}
	return nil
}

func ensureDeliveryWorkNamespaces(vaultPath string) error {
	for _, kind := range []string{"tasks", "waves", "gates", "epics"} {
		path := filepath.Join(vaultPath, "work", kind)
		info, err := os.Lstat(path)
		if os.IsNotExist(err) {
			if err := ensureDir(path); err != nil {
				return tuskerError(errorInvalidTransition, "delivery import work namespace is unavailable", withPath(path), withContext(map[string]any{"cause": err.Error()}))
			}
			continue
		}
		if err != nil {
			return tuskerError(errorInvalidTransition, "delivery import work namespace is unavailable", withPath(path), withContext(map[string]any{"cause": err.Error()}))
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return tuskerError(errorInvalidTransition, "delivery import work namespace is not a real directory", withPath(path))
		}
	}
	return nil
}

func deliveryIntegrationBaseSHA(vaultPath string) (string, error) {
	repoRoot := v7RepoRoot(vaultPath)
	if !v7GitRepo(repoRoot) {
		return "", nil
	}
	base := v7DefaultBranch(vaultPath)
	sha, err := gitOutputTrim(repoRoot, "rev-parse", "refs/heads/"+base)
	if err != nil {
		return "", tuskerError(errorInvalidTransition, "delivery import could not read configured default integration base "+base+"; repair the default branch before Start")
	}
	return sha, nil
}

func convergeUnchangedDeliveryWrites(writes map[string]string) error {
	for path, next := range writes {
		current, err := readText(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if current == next {
			delete(writes, path)
			continue
		}
		currentData, currentBody, currentErr := parseFrontmatter(current)
		nextData, nextBody, nextErr := parseFrontmatter(next)
		if currentErr != nil || nextErr != nil {
			continue
		}
		for _, field := range []string{"updated_at", "updated_by", "state_rev"} {
			delete(currentData, field)
			delete(nextData, field)
		}
		currentCanonical, err := yaml.Marshal(currentData)
		if err != nil {
			return err
		}
		nextCanonical, err := yaml.Marshal(nextData)
		if err != nil {
			return err
		}
		if bytes.Equal(currentCanonical, nextCanonical) && strings.TrimSpace(currentBody) == strings.TrimSpace(nextBody) {
			delete(writes, path)
		}
	}
	return nil
}

func commitDeliveryWrites(writes map[string]string, failAfter int) error {
	return commitDeliveryWritesGuarded(writes, failAfter, nil)
}

// commitDeliveryWritesGuarded is the document transaction boundary. Guarded
// import callers hold the material lock; this function captures every actual
// write preimage, atomically replaces one complete document at a time, and
// verifies a bound plan after every replacement. A failed guard restores and
// byte-verifies the whole set before any cache invalidation or mutation
// notification escapes.
func commitDeliveryWritesGuarded(writes map[string]string, failAfter int, guard *deliveryImportWriteGuard) error {
	return commitDeliveryWritesGuardedWithLocks(writes, failAfter, guard, nil)
}

// commitDeliveryWritesGuardedWithLocks is the narrow authorization-transaction
// entry point for callers that already hold some document locks under the V7
// material epoch. Those exact identities are not reacquired: flock is not
// recursively owned across separate file descriptors on every supported
// platform. Snapshot or write paths not covered by a live caller lock retain
// the normal sorted acquisition below.
func commitDeliveryWritesGuardedWithLocks(writes map[string]string, failAfter int, guard *deliveryImportWriteGuard, heldLocks []*v7DocumentLock) error {
	paths := make([]string, 0, len(writes))
	for path := range writes {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	lockPaths := uniqueStrings(append(append([]string{}, paths...), guardSnapshotPaths(guard)...))
	sort.Strings(lockPaths)
	heldIdentities := make(map[string]struct{}, len(heldLocks))
	for _, lock := range heldLocks {
		if lock == nil || lock.file == nil || strings.TrimSpace(lock.path) == "" {
			return tuskerError(errorInvalidArg, "guarded delivery write received an invalid held document lock")
		}
		if _, err := lock.file.Stat(); err != nil {
			return tuskerError(errorInvalidTransition, "guarded delivery write received a closed held document lock", withPath(lock.path))
		}
		identity, err := v7DocumentLockIdentity(lock.path)
		if err != nil {
			return err
		}
		heldIdentities[identity] = struct{}{}
	}
	var documentLocks []*v7DocumentLock
	for _, path := range lockPaths {
		if !fileExists(path) {
			continue
		}
		identity, err := v7DocumentLockIdentity(path)
		if err != nil {
			return err
		}
		if _, held := heldIdentities[identity]; held {
			continue
		}
		lock, err := acquireV7DocumentLock(path, v7DocumentLockTimeout)
		if err != nil {
			for i := len(documentLocks) - 1; i >= 0; i-- {
				_ = documentLocks[i].Close()
			}
			return err
		}
		documentLocks = append(documentLocks, lock)
	}
	defer func() {
		for i := len(documentLocks) - 1; i >= 0; i-- {
			_ = documentLocks[i].Close()
		}
	}()
	backups := map[string]deliveryWritePreimage{}
	for _, path := range paths {
		parentInfo, err := os.Lstat(filepath.Dir(path))
		if err != nil {
			return tuskerError(errorInvalidTransition, "delivery import write directory is unavailable", withPath(filepath.Dir(path)), withContext(map[string]any{"cause": err.Error()}))
		}
		if parentInfo.Mode()&os.ModeSymlink != 0 || !parentInfo.IsDir() {
			return tuskerError(errorInvalidTransition, "delivery import write directory is not a real directory", withPath(filepath.Dir(path)))
		}
		info, err := os.Lstat(path)
		if os.IsNotExist(err) {
			backups[path] = deliveryWritePreimage{Mode: 0o644}
			continue
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return tuskerError(errorInvalidTransition, "delivery import write target is not a regular file", withPath(path))
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		backups[path] = deliveryWritePreimage{Content: raw, Mode: info.Mode().Perm(), Existed: true}
	}
	if guard != nil {
		if guard.SnapshotVerify != nil {
			if err := guard.SnapshotVerify(); err != nil {
				return err
			}
		}
		if guard.Verify == nil && guard.SnapshotVerify == nil {
			return tuskerError(errorInvalidTransition, "delivery import write guard has no identity verifier")
		}
		if guard.Verify != nil {
			if err := guard.Verify(); err != nil {
				return markDeliveryImportIdentityChanged(tuskerError(
					errorInvalidTransition,
					"delivery plan identity changed before import commit; no documents were written",
					withHint("restore the reviewed plan path, regenerate delivery review, and confirm its current identity"),
					withContext(map[string]any{"cause": err.Error()}),
				))
			}
		}
		if guard.AfterPrecheck != nil {
			guard.AfterPrecheck()
		}
		if deliveryImportAfterPrecheckHook != nil {
			deliveryImportAfterPrecheckHook()
		}
		if guard.SnapshotVerify != nil {
			if err := guard.SnapshotVerify(); err != nil {
				return err
			}
		}
	}

	var attemptedPaths []string
	rollback := func(cause error, identityChanged bool) error {
		rollbackErr := restoreDeliveryWritePreimagesOwned(attemptedPaths, backups, writes)
		for _, path := range attemptedPaths {
			invalidateCachedNote(path)
		}
		if identityChanged {
			return deliveryImportIdentityError(cause, rollbackErr, paths)
		}
		if rollbackErr != nil {
			return tuskerError(
				errorInvalidTransition,
				"delivery import failed and exact rollback could not be proven; stop and repair the reported paths before retrying",
				withHint("restore every reported path from version control or a verified backup, then rerun delivery review"),
				withContext(map[string]any{"cause": cause.Error(), "rollback": rollbackErr.Error(), "paths": paths}),
			)
		}
		return cause
	}
	for i, path := range paths {
		if guard != nil && guard.SnapshotVerify != nil {
			if err := guard.SnapshotVerify(); err != nil {
				return rollback(err, false)
			}
		}
		attemptedPaths = append(attemptedPaths, path)
		if err := writeDeliveryTransactionFileCAS(path, []byte(writes[path]), backups[path]); err != nil {
			return rollback(err, false)
		}
		written, readErr := os.ReadFile(path)
		if readErr != nil || !bytes.Equal(written, []byte(writes[path])) {
			return rollback(tuskerError(errorInvalidTransition, "delivery import post-write CAS mismatch", withPath(path)), false)
		}
		if guard != nil && guard.SnapshotAdvance != nil {
			guard.SnapshotAdvance(path, []byte(writes[path]))
		}
		if deliveryImportAfterWriteHook != nil {
			deliveryImportAfterWriteHook(i, path)
		}
		if guard != nil && guard.SnapshotVerify != nil {
			if err := guard.SnapshotVerify(); err != nil {
				return rollback(err, false)
			}
		}
		if failAfter > 0 && i+1 >= failAfter {
			return rollback(tuskerError(errorInvalidArg, "forced delivery import write failure"), false)
		}
		if guard != nil && guard.Verify != nil {
			if err := guard.Verify(); err != nil {
				return rollback(err, true)
			}
		}
	}
	if guard != nil && guard.SnapshotVerify != nil {
		if err := guard.SnapshotVerify(); err != nil {
			return rollback(err, false)
		}
	}
	if guard != nil && guard.Verify != nil {
		if err := guard.Verify(); err != nil {
			return rollback(err, true)
		}
	}
	if guard != nil {
		guard.Commit = captureDeliveryImportCommit(paths, backups, writes)
	}
	for _, path := range paths {
		invalidateCachedNote(path)
		if guard == nil || !guard.DelayMutationVisibility {
			recordCLIVaultMutation(path)
		}
	}
	return nil
}

// restoreDeliveryWritePreimagesOwned restores only paths this transaction
// attempted and only while the current bytes are either its exact intended
// bytes or already the original preimage. Third-party bytes are preserved and
// reported as an unproven rollback.
func restoreDeliveryWritePreimagesOwned(paths []string, backups map[string]deliveryWritePreimage, intended map[string]string) error {
	var failures []string
	var verifyPaths []string
	for index := len(paths) - 1; index >= 0; index-- {
		path := paths[index]
		backup := backups[path]
		want := []byte(intended[path])
		if deliveryImportRollbackWriteHook != nil {
			if err := deliveryImportRollbackWriteHook(path); err != nil {
				failures = append(failures, path+": "+err.Error())
				continue
			}
		}
		current, err := os.ReadFile(path)
		if backup.Existed {
			if err == nil && bytes.Equal(current, backup.Content) {
				verifyPaths = append(verifyPaths, path)
				continue
			}
			if err != nil || !bytes.Equal(current, want) {
				failures = append(failures, path+": current bytes are not transaction-owned; preserved")
				continue
			}
			if err := writeDeliveryTransactionFile(path, backup.Content, backup.Mode); err != nil {
				failures = append(failures, path+": "+err.Error())
			} else {
				verifyPaths = append(verifyPaths, path)
				if deliveryImportRollbackAfterRestoreHook != nil {
					deliveryImportRollbackAfterRestoreHook(path)
				}
			}
			continue
		}
		if os.IsNotExist(err) {
			verifyPaths = append(verifyPaths, path)
			continue
		}
		if err != nil || !bytes.Equal(current, want) {
			failures = append(failures, path+": current bytes are not transaction-owned; preserved")
			continue
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			failures = append(failures, path+": "+err.Error())
			continue
		}
		if err := syncV7DocumentDirectory(filepath.Dir(path)); err != nil {
			failures = append(failures, path+": sync rollback deletion: "+err.Error())
			continue
		}
		verifyPaths = append(verifyPaths, path)
		if deliveryImportRollbackAfterRestoreHook != nil {
			deliveryImportRollbackAfterRestoreHook(path)
		}
	}
	for _, path := range verifyPaths {
		backup := backups[path]
		info, err := os.Lstat(path)
		if !backup.Existed {
			if err == nil || !os.IsNotExist(err) {
				failures = append(failures, path+": rollback absence could not be proven")
			}
			continue
		}
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != backup.Mode.Perm() {
			failures = append(failures, path+": restored identity or mode differs")
			continue
		}
		raw, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(raw, backup.Content) {
			failures = append(failures, path+": restored bytes differ")
		}
	}
	if len(failures) > 0 {
		sort.Strings(failures)
		return fmt.Errorf("%s", strings.Join(failures, "; "))
	}
	return nil
}

func guardSnapshotPaths(guard *deliveryImportWriteGuard) []string {
	if guard == nil {
		return nil
	}
	return guard.SnapshotPaths
}

func writeDeliveryTransactionFile(path string, content []byte, mode os.FileMode) error {
	return writeDeliveryTransactionFileUnchecked(path, content, mode)
}

func writeDeliveryTransactionFileCAS(path string, content []byte, expected deliveryWritePreimage) error {
	mode := expected.Mode
	return writeDeliveryTransactionFilePrepared(path, content, mode, func() error {
		if deliveryImportBeforeRenameHook != nil {
			deliveryImportBeforeRenameHook(path)
		}
		info, err := os.Lstat(path)
		if !expected.Existed {
			if os.IsNotExist(err) {
				return nil
			}
			return tuskerError(errorInvalidTransition, "delivery import expected absent target changed before rename", withPath(path))
		}
		if err != nil || !info.Mode().IsRegular() {
			return tuskerError(errorInvalidTransition, "delivery import target changed before rename", withPath(path))
		}
		raw, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(raw, expected.Content) {
			return tuskerError(errorInvalidTransition, "delivery import preimage changed before rename", withPath(path))
		}
		return nil
	})
}

func writeDeliveryTransactionFileUnchecked(path string, content []byte, mode os.FileMode) error {
	return writeDeliveryTransactionFilePrepared(path, content, mode, nil)
}

func writeDeliveryTransactionFilePrepared(path string, content []byte, mode os.FileMode, beforeRename func() error) error {
	if mode.Perm() == 0 {
		mode = 0o644
	}
	temp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".delivery-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	renamed := false
	defer func() {
		_ = temp.Close()
		if !renamed {
			_ = os.Remove(tempPath)
		}
	}()
	if err := temp.Chmod(mode.Perm()); err != nil {
		return err
	}
	if written, err := temp.Write(content); err != nil {
		return err
	} else if written != len(content) {
		return fmt.Errorf("write delivery transaction temporary file: wrote %d of %d bytes", written, len(content))
	}
	if err := temp.Sync(); err != nil {
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if beforeRename != nil {
		if err := beforeRename(); err != nil {
			return err
		}
	}
	if err := os.Rename(tempPath, path); err != nil {
		return err
	}
	renamed = true
	if err := syncV7DocumentDirectory(filepath.Dir(path)); err != nil {
		return fmt.Errorf("sync delivery transaction parent directory after rename: %w", err)
	}
	return nil
}

func restoreDeliveryWritePreimages(paths []string, backups map[string]deliveryWritePreimage) error {
	var failures []string
	for index := len(paths) - 1; index >= 0; index-- {
		path := paths[index]
		if deliveryImportRollbackWriteHook != nil {
			if err := deliveryImportRollbackWriteHook(path); err != nil {
				failures = append(failures, path+": "+err.Error())
				continue
			}
		}
		backup := backups[path]
		if backup.Existed {
			if err := writeDeliveryTransactionFile(path, backup.Content, backup.Mode); err != nil {
				failures = append(failures, path+": "+err.Error())
			}
			continue
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			failures = append(failures, path+": "+err.Error())
			continue
		}
		if err := syncV7DocumentDirectory(filepath.Dir(path)); err != nil {
			failures = append(failures, path+": "+err.Error())
		}
	}
	for _, path := range paths {
		backup := backups[path]
		info, err := os.Lstat(path)
		if !backup.Existed {
			if err == nil || !os.IsNotExist(err) {
				failures = append(failures, path+": created file remains after rollback")
			}
			continue
		}
		if err != nil {
			failures = append(failures, path+": restored file is unavailable: "+err.Error())
			continue
		}
		if !info.Mode().IsRegular() || info.Mode().Perm() != backup.Mode.Perm() {
			failures = append(failures, path+": restored file identity or mode differs")
			continue
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			failures = append(failures, path+": restored file cannot be read: "+err.Error())
		} else if !bytes.Equal(raw, backup.Content) {
			failures = append(failures, path+": restored bytes differ")
		}
	}
	if len(failures) > 0 {
		sort.Strings(failures)
		return fmt.Errorf("%s", strings.Join(uniqueStrings(failures), "; "))
	}
	return nil
}

func captureDeliveryImportCommit(paths []string, backups map[string]deliveryWritePreimage, writes map[string]string) *deliveryImportCommit {
	commit := &deliveryImportCommit{
		Paths:     append([]string(nil), paths...),
		Preimages: make(map[string]deliveryWritePreimage, len(paths)),
		Written:   make(map[string][]byte, len(paths)),
	}
	for _, path := range paths {
		preimage := backups[path]
		preimage.Content = append([]byte(nil), preimage.Content...)
		commit.Preimages[path] = preimage
		commit.Written[path] = []byte(writes[path])
	}
	return commit
}

func (commit *deliveryImportCommit) Restore() error {
	if commit == nil || commit.restored {
		return nil
	}
	defer func() {
		for _, path := range commit.Paths {
			invalidateCachedNote(path)
		}
	}()
	for _, path := range commit.Paths {
		info, err := os.Lstat(path)
		if err != nil {
			return fmt.Errorf("%s: committed import document is unavailable: %w", path, err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Mode().Perm() != commit.Preimages[path].Mode.Perm() {
			return fmt.Errorf("%s: committed import document identity or mode changed before restoration", path)
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("%s: committed import document cannot be read: %w", path, err)
		}
		if !bytes.Equal(raw, commit.Written[path]) {
			return fmt.Errorf("%s: committed import bytes changed before restoration", path)
		}
	}
	restoreErr := restoreDeliveryWritePreimages(commit.Paths, commit.Preimages)
	if restoreErr != nil {
		return restoreErr
	}
	commit.restored = true
	return nil
}

func deliveryImportIdentityError(cause, rollbackErr error, paths []string) error {
	if rollbackErr == nil {
		return markDeliveryImportIdentityChanged(tuskerError(
			errorInvalidTransition,
			"delivery plan identity changed during import; exact import preimages were restored",
			withHint("restore the reviewed plan path, regenerate delivery review, and confirm its current identity"),
			withContext(map[string]any{"cause": cause.Error()}),
		))
	}
	return markDeliveryImportIdentityChanged(tuskerError(
		errorInvalidTransition,
		"delivery plan identity changed during import and exact rollback could not be proven; delivery is fail-closed pending repair",
		withHint("stop delivery, restore every reported path from version control or a verified backup, then regenerate delivery review"),
		withContext(map[string]any{"cause": cause.Error(), "rollback": rollbackErr.Error(), "paths": paths}),
	))
}

func renderDeliveryTaskBody(id string, task deliveryPlanTask) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s · %s\n\n## Intent\n\n%s\n\n## Acceptance\n\n| ID | Outcome | Proof |\n|---|---|---|\n", id, task.Title, task.Outcome)
	for _, row := range task.Acceptance {
		fmt.Fprintf(&b, "| %s | %s | See mapped verification. |\n", row.ID, strings.ReplaceAll(row.Outcome, "|", "\\|"))
	}
	b.WriteString("\n## Verification\n\n| Covers | Check | Result | Notes |\n|---|---|---|---|\n")
	for _, row := range task.Verification {
		fmt.Fprintf(&b, "| %s | %s | pending | %s |\n", row.Covers, strings.ReplaceAll(row.Check, "|", "\\|"), strings.ReplaceAll(row.Notes, "|", "\\|"))
	}
	fmt.Fprintf(&b, "\n## Artifact contract\n\n- Kind: `%s`\n- Path: `%s`\n- Summary: %s\n", task.Artifact.Kind, task.Artifact.Path, task.Artifact.Summary)
	return b.String()
}

func renderDeliveryWorkStreams(body string, report deliveryImportReport) string {
	begin, end := deliveryScopeMarkers(report.PlanScope)
	var lines []string
	lines = append(lines, begin, "")
	// Keep a newly-created section heading inside the generated block. That
	// lets planning-context canonicalization remove the entire import scaffold
	// without treating a human-authored document edit as non-material.
	if !strings.Contains(body, "## Work streams") && !strings.Contains(body, "## Work Streams") {
		lines = append(lines, "## Work streams", "")
	}
	keys := make([]string, 0, len(report.TaskMapping))
	for key := range report.TaskMapping {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		lines = append(lines, fmt.Sprintf("- `[[%s]]` implements delivery source `%s`.", report.TaskMapping[key], key))
	}
	lines = append(lines, "", fmt.Sprintf("- `[[%s]]` is the imported delivery wave.", report.WaveID), "", end)
	block := strings.Join(lines, "\n")
	if start := strings.Index(body, begin); start >= 0 {
		if finish := strings.Index(body[start:], end); finish >= 0 {
			return body[:start] + block + body[start+finish+len(end):]
		}
	}
	return strings.TrimRight(body, "\n") + "\n\n" + block + "\n"
}

func removeDeliveryWorkStreams(body, scope string) string {
	begin, end := deliveryScopeMarkers(scope)
	start := strings.Index(body, begin)
	if start < 0 {
		return body
	}
	finish := strings.Index(body[start:], end)
	if finish < 0 {
		return body
	}
	finish = start + finish + len(end)
	left := strings.TrimRight(body[:start], "\n")
	right := strings.TrimLeft(body[finish:], "\n")
	if left == "" {
		if right == "" {
			return ""
		}
		return right
	}
	if right == "" {
		return left + "\n"
	}
	return left + "\n\n" + right
}

func deliveryScopeMarkers(scope string) (string, string) {
	hash := strings.TrimPrefix(deliveryFingerprint([]byte(strings.TrimSpace(scope))), "sha256:")[:16]
	return "<!-- tusker:delivery-import:" + hash + ":begin -->", "<!-- tusker:delivery-import:" + hash + ":end -->"
}

func deliveryRepoPathExists(vaultPath, ref string) bool {
	clean := v7CleanSpecRef(ref)
	return clean != "" && !v7SpecRefPathEscapes(clean) && !filepath.IsAbs(clean) && fileExists(filepath.Join(v7RepoRoot(vaultPath), filepath.FromSlash(clean)))
}

func deliverySpecRefExists(vaultPath, ref string) bool {
	path := deliverySpecRefPath(vaultPath, ref)
	return path != "" && fileExists(path)
}

func deliverySpecRefPath(vaultPath, ref string) string {
	clean := v7CleanSpecRef(ref)
	if clean == "" || v7SpecRefPathEscapes(clean) || filepath.IsAbs(clean) {
		return ""
	}
	if id := v7SpecRefDecisionID(clean); id != "" {
		return filepath.Join(vaultPath, "work", "decisions", id+".md")
	}
	if strings.HasPrefix(clean, "work/") {
		return filepath.Join(vaultPath, filepath.FromSlash(clean))
	}
	return filepath.Join(v7RepoRoot(vaultPath), filepath.FromSlash(clean))
}

func deliveryRefsOverlap(left, right []string) bool {
	set := map[string]bool{}
	for _, ref := range left {
		set[v7CleanSpecRef(ref)] = true
	}
	for _, ref := range right {
		if set[v7CleanSpecRef(ref)] {
			return true
		}
	}
	return false
}

func deliveryPlanScope(plan deliveryPlan) string {
	return strings.TrimSpace(plan.Scope)
}

func deliveryGeneratedScope(spec string) string {
	hash := strings.TrimPrefix(deliveryFingerprint([]byte(v7CleanSpecRef(spec))), "sha256:")[:16]
	return "delivery-" + hash
}

func deliveryScopeValid(scope string) bool {
	for _, char := range strings.TrimSpace(scope) {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || strings.ContainsRune("._/:-", char) {
			continue
		}
		return false
	}
	return strings.TrimSpace(scope) != ""
}

func deliveryInvalidProductionPath(path string) bool {
	clean := filepath.ToSlash(filepath.Clean(strings.TrimSpace(path)))
	return clean == "" || clean == "." || filepath.IsAbs(path) || clean == ".." || strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, ".tusker/scratch/") || deliveryPlaceholder(clean)
}

func deliveryPlaceholder(value string) bool {
	lower := strings.ToLower(strings.TrimSpace(value))
	if lower == "" {
		return true
	}
	for _, marker := range []string{"tbd", "todo", "replace-me", "replace with", "<...>", "placeholder"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func deliveryExpectedConcurrency(plan deliveryPlan, frontiers [][]string) int {
	maxFrontier := 0
	for _, frontier := range frontiers {
		maxFrontier = maxInt(maxFrontier, len(frontier))
	}
	if plan.Concurrency <= 0 {
		return maxInt(1, maxFrontier)
	}
	return minInt(plan.Concurrency, maxInt(1, maxFrontier))
}

func deliveryFingerprint(raw []byte) string {
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func emitDeliveryImportReport(report deliveryImportReport, args Args) {
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": true, "delivery": report, "inert": true})
		return
	}
	if args.Bool("quiet") {
		return
	}
	mode := "Imported"
	if report.DryRun {
		mode = "Dry-run validated"
	}
	frontiers := make([]string, 0, len(report.Frontiers))
	for _, frontier := range report.Frontiers {
		frontiers = append(frontiers, "{"+strings.Join(frontier, ", ")+"}")
	}
	fmt.Printf("%s %s with %d tasks; frontiers: %s; expected concurrency: %d. No work was dispatched.\n", mode, report.WaveID, len(report.TaskMapping), strings.Join(frontiers, " -> "), report.ExpectedConcurrency)
}
