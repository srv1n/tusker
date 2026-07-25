package main

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const deliveryPlanV2Schema = "tusker.delivery-plan/v2"

// V2 deliberately has no identity or lifecycle fields. Source keys are the
// only caller supplied identity; Tusker allocates every durable record ID.
type deliveryPlanV2 struct {
	Schema       string                `yaml:"schema"`
	Scope        string                `yaml:"scope"`
	Title        string                `yaml:"title"`
	Epic         string                `yaml:"epic,omitempty"`
	EpicContract *deliveryEpicContract `yaml:"epic_contract,omitempty"`
	SpecRefs     []string              `yaml:"spec_refs"`
	// ContextFingerprint binds the proposal to the bounded repository facts
	// the planner actually saw. It is deliberately authored, not inferred on
	// import, so a review can reject a plan composed from stale context.
	ContextFingerprint string                `yaml:"context_fingerprint"`
	NonGoals           []string              `yaml:"non_goals,omitempty"`
	Requirements       []deliveryRequirement `yaml:"requirements"`
	Concurrency        int                   `yaml:"concurrency,omitempty"`
	RunnerProfile      string                `yaml:"runner_profile,omitempty"`
	// SharedResources, overlap strategies, assumptions, unresolved decisions,
	// and Summary are authored facts.  The doctor must never manufacture them
	// from filenames or dependency shape.
	SharedResources     []deliverySharedResource     `yaml:"shared_resources,omitempty"`
	OwnedPathOverlaps   []deliveryOverlapStrategy    `yaml:"owned_path_overlaps,omitempty"`
	Assumptions         []deliveryPlanAssumption     `yaml:"assumptions,omitempty"`
	UnresolvedDecisions []deliveryUnresolvedDecision `yaml:"unresolved_decisions,omitempty"`
	Summary             string                       `yaml:"summary,omitempty"`
	Tasks               []deliveryPlanTask           `yaml:"tasks"`
	HumanGates          []deliveryHumanGate          `yaml:"human_gates,omitempty"`
	gateMapping         map[string]string
}

type deliverySharedResource struct {
	SourceKey string `yaml:"source_key"`
	Kind      string `yaml:"kind"`
	Capacity  int    `yaml:"capacity,omitempty"`
}

type deliveryOverlapStrategy struct {
	SourceKey        string   `yaml:"source_key"`
	Tasks            []string `yaml:"tasks"`
	Paths            []string `yaml:"paths,omitempty"`
	GeneratedOutputs []string `yaml:"generated_outputs,omitempty"`
	MigrationKeys    []string `yaml:"migration_keys,omitempty"`
	Resources        []string `yaml:"resources,omitempty"`
	Strategy         string   `yaml:"strategy"` // integrator or serialize
	Integrator       string   `yaml:"integrator,omitempty"`
}

type deliveryPlanAssumption struct {
	SourceKey string `yaml:"source_key"`
	Statement string `yaml:"statement"`
}

type deliveryUnresolvedDecision struct {
	SourceKey string `yaml:"source_key"`
	Question  string `yaml:"question"`
}

type deliveryRequirement struct {
	ID      string `yaml:"id"`
	Outcome string `yaml:"outcome"`
}
type deliveryEpicContract struct {
	SourceKey   string   `yaml:"source_key"`
	AcronymHint string   `yaml:"acronym_hint"`
	Title       string   `yaml:"title"`
	Domains     []string `yaml:"domains,omitempty"`
}
type deliveryHumanGate struct {
	SourceKey         string   `yaml:"source_key"`
	Title             string   `yaml:"title"`
	Kind              string   `yaml:"kind"`
	Owner             string   `yaml:"owner"`
	TaskSourceKey     string   `yaml:"task_source_key"`
	AcceptanceIDs     []string `yaml:"acceptance_ids"`
	DependencyClosure []string `yaml:"dependency_closure,omitempty"`
	Action            string   `yaml:"action"`
	Verification      string   `yaml:"verification"`
	WhyAgentCannot    string   `yaml:"why_agent_cannot"`
	Suggestion        string   `yaml:"suggestion,omitempty"`
}

func deliveryPlanSchemaAt(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	var root yaml.Node
	if err := yaml.Unmarshal(raw, &root); err != nil {
		return "", tuskerError(errorInvalidArg, "invalid delivery plan YAML: "+err.Error())
	}
	if len(root.Content) == 0 || root.Content[0].Kind != yaml.MappingNode {
		return "", tuskerError(errorInvalidArg, "invalid delivery plan YAML: expected mapping")
	}
	for i := 0; i+1 < len(root.Content[0].Content); i += 2 {
		if root.Content[0].Content[i].Value == "schema" {
			return root.Content[0].Content[i+1].Value, nil
		}
	}
	return "", nil
}

func deliveryV2ImportCmd(vaultPath, path string, args Args) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if expected := strings.TrimSpace(args.String("expected-plan-fingerprint")); expected != "" && deliveryFingerprint(raw) != expected {
		return tuskerError(errorInvalidTransition, "delivery plan changed after confirmation; regenerate delivery review and confirm its exact plan fingerprint")
	}
	var v2 deliveryPlanV2
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	decoder.KnownFields(true)
	if err := decoder.Decode(&v2); err != nil {
		return tuskerError(errorInvalidArg, "invalid V2 delivery plan YAML: "+err.Error())
	}
	plan, issues := deliveryV2Prepare(vaultPath, v2)
	baseIssues, frontiers := validateDeliveryPlan(vaultPath, plan)
	issues = append(issues, baseIssues...)
	// Import is deliberately downstream of the doctor. A command caller may
	// skip its explicit UX, but never the operational safety contract. Keep
	// ordinary schema/traceability errors first so callers retain their precise
	// existing remedies.
	if len(issues) == 0 {
		if doctor, err := deliveryPlanDoctor(vaultPath, path); err != nil {
			return err
		} else if !doctor.OK {
			return tuskerError(errorInvalidArg, "delivery plan is operationally unsafe", withContext(map[string]any{"delivery_doctor": doctor}))
		}
	}
	mapping, existingWave, mapErr := deliveryTaskMapping(vaultPath, plan)
	if mapErr != nil {
		return mapErr
	}
	waveID := existingWave
	if waveID == "" {
		waveID = nextV7WaveID(vaultPath)
	}
	report := deliveryImportReport{PlanFingerprint: deliveryFingerprint(raw), PlanScope: deliveryPlanScope(plan), WaveID: waveID, WaveTitle: fallback(firstNonEmpty(args.String("wave"), plan.Title), "Imported delivery"), SpecRefs: plan.SpecRefs, TaskMapping: mapping, Frontiers: frontiers, ExpectedConcurrency: deliveryExpectedConcurrency(plan, frontiers), Issues: uniqueStrings(issues), DryRun: args.Bool("dry-run")}
	if len(report.Issues) > 0 {
		return tuskerError(errorInvalidArg, "delivery plan is invalid: "+strings.Join(report.Issues, "; "), withContext(map[string]any{"delivery": report}))
	}
	gateMapping, err := deliveryV2GateMapping(vaultPath, plan, mapping)
	if err != nil {
		return err
	}
	plan.v2.gateMapping = gateMapping
	if args.Bool("dry-run") {
		emitDeliveryImportReport(report, args)
		return nil
	}
	if err := applyDeliveryImport(vaultPath, plan, report, args); err != nil {
		return err
	}
	emitDeliveryImportReport(report, args)
	return nil
}

func deliveryV2Prepare(vaultPath string, v2 deliveryPlanV2) (deliveryPlan, []string) {
	plan := deliveryPlan{Schema: deliveryPlanSchema, Scope: v2.Scope, Title: v2.Title, Epic: strings.ToUpper(strings.TrimSpace(v2.Epic)), SpecRefs: v2.SpecRefs, Concurrency: v2.Concurrency, RunnerProfile: v2.RunnerProfile, Tasks: v2.Tasks, v2: &v2}
	var issues []string
	if v2.Schema != deliveryPlanV2Schema {
		issues = append(issues, "schema must be "+deliveryPlanV2Schema)
	}
	if !deliveryContextFingerprintValid(v2.ContextFingerprint) {
		issues = append(issues, "V2 plan requires context_fingerprint in sha256:<64 lowercase hex> form")
	}
	if plan.Epic != "" && v2.EpicContract != nil {
		issues = append(issues, "epic and epic_contract are mutually exclusive")
	}
	if plan.Epic == "" && v2.EpicContract == nil {
		issues = append(issues, "V2 plan requires an existing epic or epic_contract")
	}
	if v2.EpicContract != nil {
		c := v2.EpicContract
		if strings.TrimSpace(c.SourceKey) == "" || deliveryPlaceholder(c.SourceKey) || !epicAcronymPattern.MatchString(strings.ToUpper(c.AcronymHint)) || deliveryPlaceholder(c.Title) {
			issues = append(issues, "epic_contract requires stable source_key, three-letter acronym_hint, and concrete title")
		} else {
			idx, err := loadV7Index(vaultPath)
			if err != nil {
				issues = append(issues, err.Error())
			} else {
				for id, epic := range idx.Epics {
					if stringField(epic.Data, "delivery_plan_scope") == plan.Scope && stringField(epic.Data, "delivery_source_key") == c.SourceKey {
						plan.Epic = id
						break
					}
				}
				if plan.Epic == "" {
					plan.Epic = strings.ToUpper(c.AcronymHint)
					if _, exists := idx.Epics[plan.Epic]; exists {
						issues = append(issues, "epic acronym collision: "+plan.Epic)
					}
				}
			}
		}
	}
	if plan.Epic != "" && v2.EpicContract == nil && !fileExists(filepath.Join(vaultPath, "work", "epics", plan.Epic+".md")) {
		issues = append(issues, "epic does not exist: "+plan.Epic)
	}
	reqs := map[string]bool{}
	for _, r := range v2.Requirements {
		id := strings.TrimSpace(r.ID)
		if id == "" || deliveryPlaceholder(r.Outcome) {
			issues = append(issues, "requirements require stable id and concrete outcome")
		}
		if reqs[id] {
			issues = append(issues, "duplicate requirement id: "+id)
		}
		reqs[id] = true
	}
	if len(v2.Requirements) == 0 {
		issues = append(issues, "V2 plan requires at least one requirement")
	}
	for _, nonGoal := range v2.NonGoals {
		if deliveryPlaceholder(nonGoal) {
			issues = append(issues, "non_goals must contain concrete statements")
		}
	}
	covered := map[string]bool{}
	keys := map[string]deliveryPlanTask{}
	for _, task := range plan.Tasks {
		keys[task.SourceKey] = task
		if len(task.RequirementRefs) == 0 {
			issues = append(issues, task.SourceKey+": task must reference at least one requirement")
		}
		for _, ref := range task.RequirementRefs {
			if !reqs[ref] {
				issues = append(issues, task.SourceKey+": unknown requirement "+ref)
			}
			covered[ref] = true
		}
	}
	for id := range reqs {
		if !covered[id] {
			issues = append(issues, "requirement "+id+" is not covered by any task")
		}
	}
	for _, gate := range v2.HumanGates {
		if _, knownKind := v7GateKinds[strings.ToLower(gate.Kind)]; strings.TrimSpace(gate.SourceKey) == "" || deliveryPlaceholder(gate.Title) || !knownKind || strings.TrimSpace(gate.Owner) == "" || strings.TrimSpace(gate.Action) == "" || strings.TrimSpace(gate.Verification) == "" || strings.TrimSpace(gate.WhyAgentCannot) == "" {
			issues = append(issues, "human gate requires source_key, title, kind, owner, action, verification, and why_agent_cannot")
		}
		task, ok := keys[gate.TaskSourceKey]
		if !ok {
			issues = append(issues, "human gate "+gate.SourceKey+": unknown task_source_key "+gate.TaskSourceKey)
			continue
		}
		acceptance := map[string]bool{}
		for _, a := range task.Acceptance {
			acceptance[deliveryAcceptanceID(a.ID)] = true
		}
		if len(gate.AcceptanceIDs) == 0 {
			issues = append(issues, "human gate "+gate.SourceKey+": acceptance_ids required")
		}
		for _, id := range gate.AcceptanceIDs {
			if !acceptance[deliveryAcceptanceID(id)] {
				issues = append(issues, "human gate "+gate.SourceKey+": unknown acceptance "+id)
			}
		}
	}
	for _, assumption := range v2.Assumptions {
		if deliveryPlaceholder(assumption.SourceKey) || deliveryPlaceholder(assumption.Statement) {
			issues = append(issues, "assumptions require source_key and concrete statement")
		}
	}
	for _, decision := range v2.UnresolvedDecisions {
		if deliveryPlaceholder(decision.SourceKey) || deliveryPlaceholder(decision.Question) {
			issues = append(issues, "unresolved_decisions require source_key and concrete question")
		}
	}
	return plan, uniqueStrings(issues)
}

func deliveryContextFingerprintValid(value string) bool {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "sha256:") || len(value) != len("sha256:")+64 {
		return false
	}
	decoded, err := hex.DecodeString(strings.TrimPrefix(value, "sha256:"))
	return err == nil && len(decoded) == 32 && strings.ToLower(value) == value
}

func deliveryV2GateMapping(vaultPath string, plan deliveryPlan, tasks map[string]string) (map[string]string, error) {
	idx, err := loadV7Index(vaultPath)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	wanted := map[string]bool{}
	max := 0
	for _, g := range plan.v2.HumanGates {
		if wanted[g.SourceKey] {
			return nil, tuskerError(errorInvalidArg, "duplicate human gate source_key: "+g.SourceKey)
		}
		wanted[g.SourceKey] = true
	}
	for id, gate := range idx.Gates {
		if m := v7GateIDPattern.FindStringSubmatch(id); m != nil && m[1] == plan.Epic {
			max = maxInt(max, atoiSafe(m[2]))
		}
		if stringField(gate.Data, "delivery_plan_scope") != plan.Scope {
			continue
		}
		key := stringField(gate.Data, "delivery_source_key")
		if !wanted[key] {
			continue
		}
		if out[key] != "" {
			return nil, tuskerError(errorInvalidArg, "multiple gates share delivery source_key "+key+" in the same plan scope")
		}
		out[key] = id
	}
	for _, g := range plan.v2.HumanGates {
		if out[g.SourceKey] == "" {
			max++
			out[g.SourceKey] = fmt.Sprintf("%s-G-%s", plan.Epic, padNumber(max))
		}
	}
	return out, nil
}

func deliveryV2TaskGateIDs(v2 *deliveryPlanV2, taskKey string) []string {
	var ids []string
	for _, g := range v2.HumanGates {
		if g.TaskSourceKey == taskKey {
			ids = append(ids, v2.gateMapping[g.SourceKey])
		}
	}
	return ids
}

func deliveryV2TaskFingerprint(task deliveryPlanTask, gates []deliveryHumanGate) string {
	bound := make([]deliveryHumanGate, 0)
	for _, gate := range gates {
		if gate.TaskSourceKey == task.SourceKey {
			bound = append(bound, gate)
		}
	}
	sort.Slice(bound, func(i, j int) bool { return bound[i].SourceKey < bound[j].SourceKey })
	raw, _ := yaml.Marshal(struct {
		Task       deliveryPlanTask    `yaml:"task"`
		HumanGates []deliveryHumanGate `yaml:"human_gates,omitempty"`
	}{Task: task, HumanGates: bound})
	return deliveryFingerprint(raw)
}

func deliveryV2WaveContractData(plan *deliveryPlanV2) (map[string]any, error) {
	out := map[string]any{"summary": plan.Summary, "context_fingerprint": plan.ContextFingerprint}
	fields := []struct {
		name    string
		value   any
		present bool
	}{
		{name: "requirements", value: plan.Requirements, present: len(plan.Requirements) > 0},
		{name: "non_goals", value: plan.NonGoals, present: len(plan.NonGoals) > 0},
		{name: "shared_resources", value: plan.SharedResources, present: len(plan.SharedResources) > 0},
		{name: "owned_path_overlaps", value: plan.OwnedPathOverlaps, present: len(plan.OwnedPathOverlaps) > 0},
		{name: "assumptions", value: plan.Assumptions, present: len(plan.Assumptions) > 0},
		{name: "unresolved_decisions", value: plan.UnresolvedDecisions, present: len(plan.UnresolvedDecisions) > 0},
	}
	for _, field := range fields {
		if !field.present {
			continue
		}
		value, err := deliveryV2StructuredFrontmatter(field.value)
		if err != nil {
			return nil, fmt.Errorf("encode V2 delivery %s: %w", field.name, err)
		}
		out[field.name] = value
	}
	return out, nil
}

func deliveryV2StructuredFrontmatter(value any) (any, error) {
	raw, err := yaml.Marshal(value)
	if err != nil {
		return nil, err
	}
	var out any
	if err := yaml.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func deliveryV2WriteExtras(vaultPath string, plan deliveryPlan, report deliveryImportReport, writes map[string]string, now, actor string) error {
	if c := plan.v2.EpicContract; c != nil && !fileExists(filepath.Join(vaultPath, "work", "epics", plan.Epic+".md")) {
		body := fmt.Sprintf("# %s · %s\n\n## Thesis\n\nImported atomically from delivery plan `%s`.\n", plan.Epic, c.Title, report.PlanFingerprint)
		data := map[string]any{"schema": "tusker.epic/v7", "kind": "epic", "id": plan.Epic, "project": v7ProjectID(vaultPath), "title": c.Title, "status": "ready", "owner": "human:" + defaultActorName(), "priority": "p2", "domains": c.Domains, "spec_refs": plan.SpecRefs, "delivery_source_key": c.SourceKey, "delivery_plan_scope": report.PlanScope, "next_task_number": 1, "next_gate_number": 1, "next_decision_number": 1, "created_at": now, "updated_at": now}
		data["state_rev"] = v7StateRev(data, body)
		content, err := serializeDocument(data, body, v7FrontmatterOrder["epic"])
		if err != nil {
			return err
		}
		writes[filepath.Join(vaultPath, "work", "epics", plan.Epic+".md")] = content
	}
	for _, g := range plan.v2.HumanGates {
		id := plan.v2.gateMapping[g.SourceKey]
		taskID := report.TaskMapping[g.TaskSourceKey]
		path := filepath.Join(vaultPath, "work", "gates", id+".md")
		createdAt, createdBy := now, actor
		var old map[string]any
		if fileExists(path) {
			parsed, _, parseErr := parseFrontmatterMustRead(path)
			if parseErr != nil {
				return parseErr
			}
			old = parsed
			createdAt = fallback(stringField(old, "created_at"), now)
			createdBy = fallback(stringField(old, "created_by"), actor)
		}
		body := fmt.Sprintf("# %s · %s\n\n## Why agent cannot do this\n\n%s\n\n## Action\n\n%s\n\n## Verification\n\n%s\n", id, g.Title, g.WhyAgentCannot, g.Action, g.Verification)
		data := map[string]any{"schema": "tusker.gate/v1", "kind": "gate", "id": id, "project": v7ProjectID(vaultPath), "title": g.Title, "gate_kind": strings.ToLower(g.Kind), "status": "open", "owner": g.Owner, "priority": "p2", "blocking": true, "blocks": []string{taskID}, "covers": g.AcceptanceIDs, "delivery_source_key": g.SourceKey, "delivery_plan_scope": report.PlanScope, "why_agent_cannot": g.WhyAgentCannot, "action": g.Action, "verification": g.Verification, "created_at": createdAt, "created_by": createdBy, "updated_at": now, "updated_by": actor}
		contractRaw, _ := yaml.Marshal(g)
		contractFingerprint := deliveryFingerprint(contractRaw)
		data["delivery_contract_fingerprint"] = contractFingerprint
		if old != nil && stringField(old, "delivery_contract_fingerprint") == contractFingerprint {
			for _, field := range []string{"status", "satisfaction_evidence", "satisfaction_evidence_refs", "satisfied_by", "satisfied_at", "waived_by", "waived_at", "waive_reason", "obsolete_reason"} {
				if value, ok := old[field]; ok {
					data[field] = value
				}
			}
		}
		if g.Suggestion != "" {
			data["suggestion"] = g.Suggestion
		}
		if len(g.DependencyClosure) > 0 {
			data["dependency_closure"] = g.DependencyClosure
		}
		data["state_rev"] = v7StateRev(data, body)
		content, err := serializeDocument(data, body, v7FrontmatterOrder["gate"])
		if err != nil {
			return err
		}
		writes[path] = content
	}
	// Removed source keyed gates are retained as obsolete history rather than
	// silently deleting a human decision.
	idx, err := loadV7Index(vaultPath)
	if err != nil {
		return err
	}
	wanted := map[string]bool{}
	for _, g := range plan.v2.HumanGates {
		wanted[g.SourceKey] = true
	}
	for id, gate := range idx.Gates {
		if stringField(gate.Data, "delivery_plan_scope") != report.PlanScope || wanted[stringField(gate.Data, "delivery_source_key")] {
			continue
		}
		data, body, err := parseFrontmatterMustRead(gate.AbsolutePath)
		if err != nil {
			return err
		}
		data["status"] = "obsolete"
		data["obsolete_reason"] = "removed from delivery plan source keys"
		data["updated_at"] = now
		data["updated_by"] = actor
		data["state_rev"] = v7StateRev(data, body)
		content, err := serializeDocument(data, body, v7FrontmatterOrder["gate"])
		if err != nil {
			return err
		}
		writes[filepath.Join(vaultPath, "work", "gates", id+".md")] = content
	}
	return nil
}
