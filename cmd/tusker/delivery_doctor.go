package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// deliveryDoctorFinding is intentionally a small, stable public contract:
// callers can key automation and UI remedies on code/path/source_keys without
// parsing prose.
type deliveryDoctorFinding struct {
	Code       string   `json:"code"`
	Path       string   `json:"path"`
	SourceKeys []string `json:"source_keys,omitempty"`
	Message    string   `json:"message"`
	Remedy     string   `json:"remedy"`
	Provenance string   `json:"provenance,omitempty"`
}

type deliveryDoctorReport struct {
	Schema      string                  `json:"schema"`
	Plan        string                  `json:"plan"`
	DryRun      bool                    `json:"dry_run"`
	OK          bool                    `json:"ok"`
	Findings    []deliveryDoctorFinding `json:"findings"`
	Frontiers   [][]string              `json:"frontiers"`
	Concurrency int                     `json:"concurrency"`
}

func deliveryDoctorCmd(args Args) error {
	vault, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	path := strings.TrimSpace(firstNonEmpty(args.String("plan"), args.String("_pos0")))
	if path == "" {
		return tuskerError(errorMissingArg, "Usage: tusker delivery doctor --plan <plan.yaml> [--json]")
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(v7RepoRoot(vault), path)
	}
	report, err := deliveryPlanDoctor(vault, path)
	if err != nil {
		return err
	}
	if !args.Bool("json") {
		fmt.Printf("Delivery doctor: %d finding(s), safe=%v, expected concurrency=%d. Read-only.\n", len(report.Findings), report.OK, report.Concurrency)
		for _, f := range report.Findings {
			fmt.Printf("  [%s] %s: %s\n    remedy: %s\n", f.Code, f.Path, f.Message, f.Remedy)
		}
	}
	if !report.OK {
		return tuskerError(errorInvalidArg, "delivery plan is operationally unsafe", withContext(map[string]any{"delivery_doctor": report}))
	}
	if args.Bool("json") {
		emitJSON(report)
	}
	return nil
}

func deliveryPlanDoctor(vault, path string) (deliveryDoctorReport, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return deliveryDoctorReport{}, err
	}
	return deliveryPlanDoctorBytes(vault, path, raw)
}

func deliveryPlanDoctorBytes(vault, path string, raw []byte) (deliveryDoctorReport, error) {
	report := deliveryDoctorReport{Schema: "tusker.delivery-doctor/v1", Plan: path, DryRun: true, Findings: []deliveryDoctorFinding{}, Frontiers: [][]string{}}
	var v2 deliveryPlanV2
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	decoder.KnownFields(true)
	if err := decoder.Decode(&v2); err != nil {
		return report, tuskerError(errorInvalidArg, "invalid delivery plan YAML: "+err.Error())
	}
	if v2.Schema != deliveryPlanV2Schema {
		report.add("UNSUPPORTED_PLAN_SCHEMA", "schema", nil, "doctor requires tusker.delivery-plan/v2", "author a V2 delivery plan", "schema")
		report.finish()
		return report, nil
	}
	plan, prep := deliveryV2Prepare(vault, v2)
	planTaskKeys := doctorPlanTaskKeys(plan)
	for _, issue := range prep {
		report.addContractIssue(issue, "v2", planTaskKeys)
	}
	base, frontiers := validateDeliveryPlan(vault, plan)
	report.Frontiers = frontiers
	for _, issue := range base {
		report.addContractIssue(issue, "base validator", planTaskKeys)
	}
	report.Concurrency = deliveryExpectedConcurrency(plan, frontiers)
	doctorOperationalFindings(&report, vault, plan)
	report.finish()
	return report, nil
}

func (r *deliveryDoctorReport) add(code, path string, keys []string, message, remedy, provenance string) {
	keys = uniqueStrings(keys)
	sort.Strings(keys)
	r.Findings = append(r.Findings, deliveryDoctorFinding{Code: code, Path: path, SourceKeys: keys, Message: message, Remedy: remedy, Provenance: provenance})
}
func (r *deliveryDoctorReport) finish() {
	sort.Slice(r.Findings, func(i, j int) bool {
		a, b := r.Findings[i], r.Findings[j]
		if a.Code != b.Code {
			return a.Code < b.Code
		}
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		if joinedA, joinedB := strings.Join(a.SourceKeys, ","), strings.Join(b.SourceKeys, ","); joinedA != joinedB {
			return joinedA < joinedB
		}
		if a.Message != b.Message {
			return a.Message < b.Message
		}
		if a.Remedy != b.Remedy {
			return a.Remedy < b.Remedy
		}
		return a.Provenance < b.Provenance
	})
	compacted := r.Findings[:0]
	for _, finding := range r.Findings {
		if len(compacted) > 0 {
			last := compacted[len(compacted)-1]
			if last.Code == finding.Code && last.Path == finding.Path && strings.Join(last.SourceKeys, "\x00") == strings.Join(finding.SourceKeys, "\x00") && last.Message == finding.Message {
				continue
			}
		}
		compacted = append(compacted, finding)
	}
	r.Findings = compacted
	r.OK = len(r.Findings) == 0
}

func (r *deliveryDoctorReport) addContractIssue(issue deliveryIssue, provenance string, allTaskKeys []string) {
	taskKey := deliveryDoctorTaskKey(issue.Message, allTaskKeys)
	requirementID := deliveryDoctorRequirementID(issue.Message, issue.Code)
	code, path, keys, remedy := deliveryDoctorContractFindingForCode(issue.Code, taskKey, requirementID, allTaskKeys)
	r.add(code, path, keys, issue.Message, remedy, provenance)
}

func deliveryDoctorTaskKey(issue string, taskKeys []string) string {
	for _, key := range taskKeys {
		if strings.HasPrefix(issue, key+":") {
			return key
		}
	}
	return ""
}

func deliveryDoctorRequirementID(issue, code string) string {
	if code != "REQUIREMENT_UNCOVERED" {
		return ""
	}
	const prefix = "requirement "
	if !strings.HasPrefix(issue, prefix) {
		return ""
	}
	value := strings.TrimPrefix(issue, prefix)
	if i := strings.IndexByte(value, ' '); i >= 0 {
		return strings.TrimSpace(value[:i])
	}
	return strings.TrimSpace(value)
}

func deliveryDoctorContractFindingForCode(code, taskKey, requirementID string, allTaskKeys []string) (string, string, []string, string) {
	taskPath := func(field string) string {
		if taskKey == "" {
			return field
		}
		return "tasks." + taskKey + "." + field
	}
	keys := []string{}
	if taskKey != "" {
		keys = []string{taskKey}
	}
	switch code {
	case "REQUIRED_CAPABILITY_UNAVAILABLE":
		return code, "required_capabilities", nil, "install or select a binary that enforces the exact required capability"
	case "REQUIREMENT_UNCOVERED":
		return code, "requirements." + requirementID, []string{requirementID}, "map the requirement to at least one task with observable acceptance"
	case "ACCEPTANCE_UNMAPPED":
		return code, taskPath("acceptance"), keys, "add an exact verification row covering the acceptance ID"
	case "PROOF_ACCEPTANCE_UNKNOWN":
		return code, taskPath("verification"), keys, "map verification only to declared acceptance IDs"
	case "PROOF_UNSUPPORTED":
		return code, taskPath("verification"), keys, "use an exact supported command: or manual proof: check"
	case "PROOF_UNFILLED":
		return code, taskPath("verification"), keys, "add exact verification covering every acceptance ID"
	case "ACCEPTANCE_INVALID":
		return code, taskPath("acceptance"), keys, "give every acceptance row a unique stable ID and concrete outcome"
	case "ARTIFACT_INVALID":
		return code, taskPath("artifact"), keys, "declare a valid operator-facing artifact with covered acceptance IDs"
	case "DEPENDENCY_CYCLE":
		return code, "tasks.*.dependencies", allTaskKeys, "remove or redirect dependency edges until the graph is acyclic"
	case "DEPENDENCY_DANGLING":
		return code, taskPath("dependencies"), keys, "reference an existing task source_key"
	case "REQUIREMENT_REFERENCE_UNKNOWN":
		return code, taskPath("requirement_refs"), keys, "reference a declared requirement ID"
	case "UNSUPPORTED_RUNNER":
		return code, taskPath("runner_profile"), keys, "choose a configured runner profile"
	default:
		return "PLAN_CONTRACT_INVALID", "plan", keys, "repair the reported V2 plan field"
	}
}

func doctorPlanTaskKeys(plan deliveryPlan) []string {
	keys := make([]string, 0, len(plan.Tasks))
	for _, task := range plan.Tasks {
		if key := strings.TrimSpace(task.SourceKey); key != "" {
			keys = append(keys, key)
		}
	}
	keys = uniqueStrings(keys)
	sort.Strings(keys)
	return keys
}

func doctorOperationalFindings(report *deliveryDoctorReport, vault string, plan deliveryPlan) {
	v2 := plan.v2
	if v2 == nil {
		return
	}
	if deliveryPlaceholder(v2.Summary) {
		report.add("OPERATOR_SUMMARY_MISSING", "summary", nil, "V2 plan has no concrete operator-facing summary", "add a concise summary of the delivered outcome and operating constraints", "plan.summary")
	}
	tasks := map[string]deliveryPlanTask{}
	for _, task := range plan.Tasks {
		tasks[task.SourceKey] = task
	}
	resources := map[string]deliverySharedResource{}
	for _, resource := range v2.SharedResources {
		if deliveryPlaceholder(resource.SourceKey) || deliveryPlaceholder(resource.Kind) || resource.Capacity < 0 {
			report.add("RESOURCE_DECLARATION_INVALID", "shared_resources", []string{resource.SourceKey}, "shared resource requires source_key, kind, and non-negative capacity", "declare a stable resource with its capacity", "plan.shared_resources")
			continue
		}
		if _, exists := resources[resource.SourceKey]; exists {
			report.add("RESOURCE_DECLARATION_DUPLICATE", "shared_resources", []string{resource.SourceKey}, "duplicate shared resource source_key", "use one declaration per resource", "plan.shared_resources")
		}
		resources[resource.SourceKey] = resource
	}
	for _, task := range plan.Tasks {
		for _, key := range task.ResourceRefs {
			if _, ok := resources[key]; !ok {
				report.add("RESOURCE_UNDECLARED", "tasks."+task.SourceKey+".resource_refs", []string{task.SourceKey, key}, "task references an undeclared shared resource", "declare the resource in shared_resources", "task resource_refs")
			}
		}
	}
	strategies := map[string]deliveryOverlapStrategy{}
	for _, s := range v2.OwnedPathOverlaps {
		if deliveryPlaceholder(s.SourceKey) || len(s.Tasks) < 2 || (strings.ToLower(s.Strategy) != "integrator" && strings.ToLower(s.Strategy) != "serialize") {
			report.add("OVERLAP_STRATEGY_INVALID", "owned_path_overlaps", append([]string{s.SourceKey}, s.Tasks...), "overlap strategy requires a source key, two tasks, and integrator or serialize", "declare an explicit integrator or serialization strategy", "plan.owned_path_overlaps")
			continue
		}
		if _, exists := strategies[s.SourceKey]; exists {
			report.add("OVERLAP_STRATEGY_DUPLICATE", "owned_path_overlaps."+s.SourceKey, append([]string{s.SourceKey}, s.Tasks...), "duplicate overlap strategy source_key", "use one strategy declaration per source_key", "plan.owned_path_overlaps")
			continue
		}
		unknown := []string{}
		for _, key := range s.Tasks {
			if _, ok := tasks[key]; !ok {
				unknown = append(unknown, key)
			}
		}
		if len(unknown) > 0 {
			report.add("OVERLAP_STRATEGY_TASK_UNKNOWN", "owned_path_overlaps."+s.SourceKey+".tasks", append([]string{s.SourceKey}, unknown...), "overlap strategy references unknown tasks", "name only declared task source_keys", "plan.owned_path_overlaps")
			continue
		}
		if len(s.Paths)+len(s.GeneratedOutputs)+len(s.MigrationKeys)+len(s.Resources) == 0 {
			report.add("OVERLAP_STRATEGY_SCOPE_MISSING", "owned_path_overlaps."+s.SourceKey, append([]string{s.SourceKey}, s.Tasks...), "overlap strategy does not identify the paths or resources it controls", "list the exact paths, generated outputs, migration keys, or resources covered", "plan.owned_path_overlaps")
			continue
		}
		switch strings.ToLower(s.Strategy) {
		case "serialize":
			if !doctorTasksTotallyOrdered(plan, s.Tasks) {
				report.add("OVERLAP_SERIALIZATION_UNPROVEN", "owned_path_overlaps."+s.SourceKey+".tasks", append([]string{s.SourceKey}, s.Tasks...), "serialize strategy is not backed by dependency ordering", "add dependency ordering so every covered task pair cannot share a runnable frontier", "task dependencies")
				continue
			}
		case "integrator":
			integrator, ok := tasks[s.Integrator]
			if strings.TrimSpace(s.Integrator) == "" || !ok {
				report.add("OVERLAP_INTEGRATOR_UNKNOWN", "owned_path_overlaps."+s.SourceKey+".integrator", append([]string{s.SourceKey, s.Integrator}, s.Tasks...), "integrator strategy does not name a real task", "set integrator to a declared task source_key", "plan.owned_path_overlaps")
				continue
			}
			ordered := true
			for _, key := range s.Tasks {
				if key != s.Integrator && !doctorTaskReaches(plan, key, s.Integrator) {
					ordered = false
				}
			}
			if !ordered {
				report.add("OVERLAP_INTEGRATOR_UNORDERED", "owned_path_overlaps."+s.SourceKey+".integrator", append([]string{s.SourceKey, s.Integrator}, s.Tasks...), "integrator does not run after every covered task", "make the integrator depend transitively on every covered task", "task dependencies")
				continue
			}
			if !doctorIntegratorOwns(integrator, s) {
				report.add("OVERLAP_INTEGRATOR_SCOPE_MISMATCH", "owned_path_overlaps."+s.SourceKey+".integrator", append([]string{s.SourceKey, s.Integrator}, s.Tasks...), "integrator does not own every declared collision path or resource", "assign the declared shared paths and resources to the integrator task", "task ownership")
				continue
			}
		}
		strategies[s.SourceKey] = s
	}
	for _, frontier := range report.Frontiers {
		resourceUsers := map[string][]string{}
		for _, taskKey := range frontier {
			for _, resourceKey := range tasks[taskKey].ResourceRefs {
				resourceUsers[resourceKey] = append(resourceUsers[resourceKey], taskKey)
			}
		}
		for resourceKey, users := range resourceUsers {
			resource := resources[resourceKey]
			if resource.Capacity > 0 && len(users) > resource.Capacity && !doctorSafeStrategy(strategies, users, "resource", []string{resourceKey}) {
				report.add("RESOURCE_CAPACITY_EXCEEDED", "shared_resources."+resourceKey, append(users, resourceKey), fmt.Sprintf("frontier needs %d slots but resource capacity is %d", len(users), resource.Capacity), "add an explicit serialization/integrator strategy or raise declared capacity", "resource capacity")
			}
		}
		for i := 0; i < len(frontier); i++ {
			for j := i + 1; j < len(frontier); j++ {
				a, b := tasks[frontier[i]], tasks[frontier[j]]
				keys := []string{a.SourceKey, b.SourceKey}
				pathConflicts := doctorOwnedPathConflicts(a.OwnedPaths, b.OwnedPaths)
				if len(pathConflicts) > 0 && !doctorSafeStrategy(strategies, keys, "path", pathConflicts) {
					report.add("OWNED_PATH_FRONTIER_CONFLICT", "tasks.*.owned_paths", keys, "simultaneously runnable tasks overlap owned paths", "add owned_path_overlaps strategy: serialize or integrator", "frontier")
				}
				generated := doctorIntersection(a.GeneratedOutputs, b.GeneratedOutputs)
				if len(generated) > 0 && !doctorSafeStrategy(strategies, keys, "generated", generated) {
					report.add("GENERATED_OUTPUT_FRONTIER_CONFLICT", "tasks.*.generated_outputs", keys, "simultaneously runnable tasks claim the same generated output", "add an explicit serialization or integrator strategy", "frontier")
				}
				migrations := doctorIntersection(a.MigrationKeys, b.MigrationKeys)
				if len(migrations) > 0 && !doctorSafeStrategy(strategies, keys, "migration", migrations) {
					report.add("MIGRATION_FRONTIER_CONFLICT", "tasks.*.migration_keys", keys, "simultaneously runnable tasks allocate the same migration namespace", "add an explicit serialization or integrator strategy", "frontier")
				}
				for _, resourceKey := range doctorIntersection(a.ResourceRefs, b.ResourceRefs) {
					resource := resources[resourceKey]
					if resource.Capacity <= 1 && !doctorSafeStrategy(strategies, keys, "resource", []string{resourceKey}) {
						report.add("RESOURCE_FRONTIER_CONFLICT", "shared_resources."+resourceKey, append(keys, resourceKey), "simultaneously runnable tasks contend for a scarce resource", "add an explicit serialization or integrator strategy", "resource capacity")
					}
				}
			}
		}
	}
	doctorGates(report, plan, tasks)
	doctorAssumptions(report, plan)
	doctorCapacity(report, vault, plan)
}

func doctorSafeStrategy(strategies map[string]deliveryOverlapStrategy, taskKeys []string, kind string, values []string) bool {
	// A downstream integrator can reconcile files, generated output, or a
	// migration allocation after isolated worktrees finish. It cannot
	// retroactively make two simultaneous users fit through a capacity-one
	// external resource. A valid serialization strategy already removes those
	// tasks from the same frontier, so a live resource conflict is never safe
	// merely because a strategy row names it.
	if kind == "resource" {
		return false
	}
	for _, s := range strategies {
		if !containsAll(s.Tasks, taskKeys) {
			continue
		}
		covered := false
		switch kind {
		case "path":
			covered = doctorPathsCover(s.Paths, values)
		case "generated":
			covered = containsAll(s.GeneratedOutputs, values)
		case "migration":
			covered = containsAll(s.MigrationKeys, values)
		case "resource":
			covered = containsAll(s.Resources, values)
		}
		if covered {
			return true
		}
	}
	return false
}
func containsAll(values, wanted []string) bool {
	set := map[string]bool{}
	for _, v := range values {
		set[v] = true
	}
	for _, v := range wanted {
		if !set[v] {
			return false
		}
	}
	return true
}
func doctorOwnedPathConflicts(a, b []string) []string {
	var conflicts []string
	for _, x := range normalizeOwnedPaths(a) {
		for _, y := range normalizeOwnedPaths(b) {
			if ownedPathsOverlap(x, y) {
				if len(x) >= len(y) {
					conflicts = append(conflicts, x)
				} else {
					conflicts = append(conflicts, y)
				}
			}
		}
	}
	return uniqueStrings(conflicts)
}

func doctorPathsCover(declared, conflicts []string) bool {
	for _, conflict := range conflicts {
		covered := false
		for _, path := range declared {
			if ownedPathsOverlap(strings.Trim(path, "/"), conflict) {
				covered = true
				break
			}
		}
		if !covered {
			return false
		}
	}
	return true
}

func doctorTasksTotallyOrdered(plan deliveryPlan, keys []string) bool {
	for i := 0; i < len(keys); i++ {
		for j := i + 1; j < len(keys); j++ {
			if !doctorTaskReaches(plan, keys[i], keys[j]) && !doctorTaskReaches(plan, keys[j], keys[i]) {
				return false
			}
		}
	}
	return true
}

func doctorTaskReaches(plan deliveryPlan, from, to string) bool {
	next := map[string][]string{}
	for _, task := range plan.Tasks {
		for _, dep := range task.Dependencies {
			next[dep.Task] = append(next[dep.Task], task.SourceKey)
		}
	}
	seen := map[string]bool{from: true}
	queue := []string{from}
	for len(queue) > 0 {
		key := queue[0]
		queue = queue[1:]
		for _, dependent := range next[key] {
			if dependent == to {
				return true
			}
			if !seen[dependent] {
				seen[dependent] = true
				queue = append(queue, dependent)
			}
		}
	}
	return false
}

func doctorIntegratorOwns(task deliveryPlanTask, strategy deliveryOverlapStrategy) bool {
	if !doctorPathsCover(task.OwnedPaths, strategy.Paths) {
		return false
	}
	return containsAll(task.GeneratedOutputs, strategy.GeneratedOutputs) &&
		containsAll(task.MigrationKeys, strategy.MigrationKeys) &&
		containsAll(task.ResourceRefs, strategy.Resources)
}
func doctorIntersection(a, b []string) []string {
	seen := map[string]bool{}
	for _, v := range a {
		seen[v] = true
	}
	var out []string
	for _, v := range b {
		if seen[v] {
			out = append(out, v)
		}
	}
	return uniqueStrings(out)
}

func doctorGates(report *deliveryDoctorReport, plan deliveryPlan, tasks map[string]deliveryPlanTask) {
	manualByTask := map[string][]string{}
	for _, task := range plan.Tasks {
		for _, verification := range task.Verification {
			if strings.HasPrefix(strings.TrimSpace(verification.Check), "manual proof:") {
				manualByTask[task.SourceKey] = append(manualByTask[task.SourceKey], doctorAcceptanceIDs(splitCSV(verification.Covers))...)
			}
		}
	}
	for key, ids := range manualByTask {
		manualByTask[key] = doctorAcceptanceIDs(ids)
	}
	gateByTaskAcceptance := map[string]bool{}
	for _, g := range plan.v2.HumanGates {
		gateAcceptance := doctorAcceptanceIDs(g.AcceptanceIDs)
		if deliveryPlaceholder(g.SourceKey) || strings.TrimSpace(g.Owner) == "" || deliveryPlaceholder(g.Action) || deliveryPlaceholder(g.Verification) || deliveryPlaceholder(g.WhyAgentCannot) {
			report.add("HUMAN_GATE_PROOF_INVALID", "human_gates."+g.SourceKey, []string{g.SourceKey, g.TaskSourceKey}, "human proof requires source_key, owner, action, verification, and why_agent_cannot", "complete the source-keyed human gate contract", "human gate")
		}
		for _, id := range gateAcceptance {
			gateByTaskAcceptance[g.TaskSourceKey+":"+id] = true
		}
		if manual := manualByTask[g.TaskSourceKey]; len(manual) > 0 && (!containsAll(gateAcceptance, manual) || !containsAll(manual, gateAcceptance)) {
			report.add("HUMAN_GATE_ACCEPTANCE_MISMATCH", "human_gates."+g.SourceKey+".acceptance_ids", []string{g.SourceKey, g.TaskSourceKey}, "human gate acceptance IDs do not exactly match the task's human-owned proof", "list every and only acceptance ID covered by manual proof", "acceptance mapping")
		}
		expected := doctorDependencyClosure(plan, g.TaskSourceKey)
		if len(g.DependencyClosure) == 0 && len(expected) > 0 {
			report.add("HUMAN_GATE_CLOSURE_MISSING", "human_gates."+g.SourceKey+".dependency_closure", []string{g.SourceKey, g.TaskSourceKey}, "human gate does not declare its affected dependency closure", "list every downstream source_key affected by this gate", "dependency closure")
		} else if !containsAll(g.DependencyClosure, expected) || !containsAll(expected, g.DependencyClosure) {
			report.add("HUMAN_GATE_CLOSURE_MISMATCH", "human_gates."+g.SourceKey+".dependency_closure", append([]string{g.SourceKey}, g.TaskSourceKey), "human gate dependency closure does not exactly match its downstream task closure", "list every and only downstream source_key affected by this gate", "dependency closure")
		}
	}
	for _, task := range plan.Tasks {
		for _, v := range task.Verification {
			if !strings.HasPrefix(strings.TrimSpace(v.Check), "manual proof:") {
				continue
			}
			for _, id := range doctorAcceptanceIDs(splitCSV(v.Covers)) {
				if !gateByTaskAcceptance[task.SourceKey+":"+id] {
					report.add("HUMAN_PROOF_GATE_MISSING", "tasks."+task.SourceKey+".verification", []string{task.SourceKey}, "manual proof has no exact source-keyed human gate", "add a human gate with the same task and acceptance IDs", "verification")
					break
				}
			}
		}
	}
}

func doctorAcceptanceIDs(ids []string) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if canonical := deliveryAcceptanceID(id); canonical != "" {
			out = append(out, canonical)
		}
	}
	out = uniqueStrings(out)
	sort.Strings(out)
	return out
}

func doctorDependencyClosure(plan deliveryPlan, root string) []string {
	next := map[string][]string{}
	for _, task := range plan.Tasks {
		for _, dep := range task.Dependencies {
			if fallback(strings.ToLower(strings.TrimSpace(dep.Kind)), "hard") != "hard" {
				continue
			}
			next[dep.Task] = append(next[dep.Task], task.SourceKey)
		}
	}
	seen := map[string]bool{}
	var visit func(string)
	visit = func(key string) {
		for _, dependent := range next[key] {
			if !seen[dependent] {
				seen[dependent] = true
				visit(dependent)
			}
		}
	}
	visit(root)
	out := make([]string, 0, len(seen))
	for key := range seen {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

func doctorAssumptions(report *deliveryDoctorReport, plan deliveryPlan) {
	for _, a := range plan.v2.Assumptions {
		needle := strings.ToLower(strings.TrimSpace(a.Statement))
		if needle == "" {
			continue
		}
		for _, requirement := range plan.v2.Requirements {
			if strings.Contains(strings.ToLower(requirement.Outcome), needle) {
				report.add("ASSUMPTION_PRESENTED_AS_FACT", "requirements."+requirement.ID+".outcome", []string{a.SourceKey, requirement.ID}, "a labeled assumption is repeated as a requirement fact", "resolve it as a product decision or retain it only as an assumption", "assumptions")
			}
		}
		for _, t := range plan.Tasks {
			if strings.Contains(strings.ToLower(t.Outcome), needle) {
				report.add("ASSUMPTION_PRESENTED_AS_FACT", "tasks."+t.SourceKey+".outcome", []string{a.SourceKey, t.SourceKey}, "a labeled assumption is repeated as a task outcome", "turn it into an acceptance fact or retain it only as an assumption", "assumptions")
			}
			for _, acceptance := range t.Acceptance {
				if strings.Contains(strings.ToLower(acceptance.Outcome), needle) {
					report.add("ASSUMPTION_PRESENTED_AS_FACT", "tasks."+t.SourceKey+".acceptance."+deliveryAcceptanceID(acceptance.ID), []string{a.SourceKey, t.SourceKey}, "a labeled assumption is repeated as an acceptance fact", "resolve it as a product decision or retain it only as an assumption", "assumptions")
				}
			}
		}
	}
}

func doctorCapacity(report *deliveryDoctorReport, vault string, plan deliveryPlan) {
	wf, err := loadWorkflow(vault)
	if err != nil {
		if plan.Concurrency > 1 || strings.TrimSpace(plan.RunnerProfile) != "" {
			report.add("CAPACITY_PROVENANCE_UNAVAILABLE", "workflow", doctorPlanTaskKeys(plan), "cannot resolve project and runner capacity: "+err.Error(), "repair workflow configuration before delivery", "workflow")
		}
		return
	}
	for _, candidate := range []struct {
		n int
		p string
	}{{wf.Data.Runtime.MaxActiveRunsPerProject, "runtime.max_active_runs_per_project"}, {wf.Data.Agents.MaxConcurrentAgents, "agents.max_concurrent_agents"}, {wf.Data.Workspace.MaxLiveWorktrees, "workspace.max_live_worktrees"}} {
		if candidate.n > 0 && report.Concurrency > candidate.n {
			report.add("CONCURRENCY_CAP_EXCEEDED", "concurrency", doctorPlanTaskKeys(plan), fmt.Sprintf("requested frontier concurrency %d exceeds capacity %d", report.Concurrency, candidate.n), fmt.Sprintf("set concurrency to %d or lower", candidate.n), candidate.p)
		}
	}
	profileNames := map[string]bool{}
	for n := range wf.Data.RunnerProfiles {
		profileNames[n] = true
	}
	for _, t := range plan.Tasks {
		name := firstNonEmpty(t.RunnerProfile, plan.RunnerProfile)
		if name != "" && !profileNames[name] {
			report.add("UNSUPPORTED_RUNNER", "tasks."+t.SourceKey+".runner_profile", []string{t.SourceKey}, "runner profile is not supported by this project", "choose a configured runner profile", "workflow.runner_profiles")
		}
	}
}
