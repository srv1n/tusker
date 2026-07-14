package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const deliveryPlanSchema = "tusker.delivery-plan/v1"

type deliveryPlan struct {
	Schema        string             `yaml:"schema" json:"schema"`
	Scope         string             `yaml:"scope,omitempty" json:"scope,omitempty"`
	Title         string             `yaml:"title" json:"title"`
	Epic          string             `yaml:"epic" json:"epic"`
	SpecRefs      []string           `yaml:"spec_refs" json:"spec_refs"`
	Concurrency   int                `yaml:"concurrency,omitempty" json:"concurrency,omitempty"`
	RunnerProfile string             `yaml:"runner_profile,omitempty" json:"runner_profile,omitempty"`
	Tasks         []deliveryPlanTask `yaml:"tasks" json:"tasks"`
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
	RunnerProfile    string                   `yaml:"runner_profile,omitempty" json:"runner_profile,omitempty"`
	ConcurrencyGroup string                   `yaml:"concurrency_group,omitempty" json:"concurrency_group,omitempty"`
	KnowledgeNodes   []string                 `yaml:"knowledge_nodes,omitempty" json:"knowledge_nodes,omitempty"`
	Risk             string                   `yaml:"risk,omitempty" json:"risk,omitempty"`
	Priority         string                   `yaml:"priority,omitempty" json:"priority,omitempty"`
	Size             string                   `yaml:"size,omitempty" json:"size,omitempty"`
	Domains          []string                 `yaml:"domains,omitempty" json:"domains,omitempty"`
}

type deliveryAcceptance struct {
	ID      string `yaml:"id" json:"id"`
	Outcome string `yaml:"outcome" json:"outcome"`
}

type deliveryVerification struct {
	Covers string `yaml:"covers" json:"covers"`
	Check  string `yaml:"check" json:"check"`
	Notes  string `yaml:"notes,omitempty" json:"notes,omitempty"`
}

type deliveryDependency struct {
	Task string `yaml:"task" json:"task"`
	Kind string `yaml:"kind,omitempty" json:"kind,omitempty"`
}

type deliveryArtifactContract struct {
	Kind    string `yaml:"kind" json:"kind"`
	Path    string `yaml:"path" json:"path"`
	Summary string `yaml:"summary" json:"summary"`
}

type deliveryImportReport struct {
	PlanFingerprint     string            `json:"planFingerprint"`
	PlanScope           string            `json:"planScope"`
	WaveID              string            `json:"waveId"`
	WaveTitle           string            `json:"waveTitle"`
	SpecRefs            []string          `json:"specRefs"`
	TaskMapping         map[string]string `json:"taskMapping"`
	Frontiers           [][]string        `json:"frontiers"`
	ExpectedConcurrency int               `json:"expectedConcurrency"`
	Issues              []string          `json:"issues"`
	DryRun              bool              `json:"dryRun"`
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
	plan := deliveryPlan{
		Schema: deliveryPlanSchema, Scope: deliveryGeneratedScope(spec), Title: strings.TrimSuffix(filepath.Base(spec), filepath.Ext(spec)),
		Epic: strings.ToUpper(args.String("epic")), SpecRefs: []string{spec}, Concurrency: 1,
		Tasks: []deliveryPlanTask{{
			SourceKey: "replace-me", Title: "Replace with an observable task", Outcome: "Replace with an observable outcome.",
			Acceptance:   []deliveryAcceptance{{ID: "A1", Outcome: "Replace with a concrete acceptance outcome."}},
			Verification: []deliveryVerification{{Covers: "A1", Check: "command: replace-with-an-exact-command"}},
			Artifact:     deliveryArtifactContract{Kind: "diff_summary", Path: "replace/with/production/path", Summary: "Explain the compact operator artifact."},
		}},
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
	if err := writeText(out, string(raw)); err != nil {
		return err
	}
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": true, "plan": out, "schema": deliveryPlanSchema, "inert": true})
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
	plan, raw, err := readDeliveryPlan(planPath)
	if err != nil {
		return err
	}
	issues, frontiers := validateDeliveryPlan(vaultPath, plan)
	mapping, existingWave, err := deliveryTaskMapping(vaultPath, plan)
	if err != nil {
		return err
	}
	waveID := existingWave
	if waveID == "" {
		waveID = nextV7WaveID(vaultPath)
	}
	report := deliveryImportReport{
		PlanFingerprint: deliveryFingerprint(raw), PlanScope: deliveryPlanScope(plan), WaveID: waveID,
		WaveTitle: fallback(firstNonEmpty(args.String("wave"), plan.Title), "Imported delivery"),
		SpecRefs:  plan.SpecRefs, TaskMapping: mapping, Frontiers: frontiers,
		ExpectedConcurrency: deliveryExpectedConcurrency(plan, frontiers), Issues: issues, DryRun: args.Bool("dry-run"),
	}
	if len(issues) > 0 {
		return tuskerError(
			errorInvalidArg,
			"delivery plan is invalid: "+strings.Join(issues, "; "),
			withContext(map[string]any{"delivery": report}),
		)
	}
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

func readDeliveryPlan(path string) (deliveryPlan, []byte, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return deliveryPlan{}, nil, err
	}
	var plan deliveryPlan
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	decoder.KnownFields(true)
	if err := decoder.Decode(&plan); err != nil {
		return plan, raw, tuskerError(errorInvalidArg, "invalid delivery plan YAML: "+err.Error())
	}
	return plan, raw, nil
}

func validateDeliveryPlan(vaultPath string, plan deliveryPlan) ([]string, [][]string) {
	var issues []string
	if plan.Schema != deliveryPlanSchema {
		issues = append(issues, "schema must be "+deliveryPlanSchema)
	}
	if deliveryPlaceholder(plan.Scope) || !deliveryScopeValid(plan.Scope) {
		issues = append(issues, "scope must be an explicit stable identifier using letters, numbers, dot, underscore, slash, colon, or hyphen")
	}
	if !epicAcronymPattern.MatchString(strings.ToUpper(plan.Epic)) {
		issues = append(issues, "epic must name an existing three-letter V7 epic")
	} else if !fileExists(filepath.Join(vaultPath, "work", "epics", strings.ToUpper(plan.Epic)+".md")) {
		issues = append(issues, "epic does not exist: "+strings.ToUpper(plan.Epic))
	}
	if len(plan.SpecRefs) == 0 {
		issues = append(issues, "at least one governing spec_ref is required")
	}
	for _, ref := range plan.SpecRefs {
		if !deliverySpecRefExists(vaultPath, ref) {
			issues = append(issues, "spec_ref does not resolve inside the repository: "+ref)
		}
	}
	if len(plan.Tasks) == 0 {
		issues = append(issues, "at least one task is required")
	}
	keys := map[string]bool{}
	for _, task := range plan.Tasks {
		key := strings.TrimSpace(task.SourceKey)
		if key == "" || deliveryPlaceholder(key) {
			issues = append(issues, "every task requires a stable non-placeholder source_key")
		} else if keys[key] {
			issues = append(issues, "duplicate source_key: "+key)
		}
		keys[key] = true
		if deliveryPlaceholder(task.Title) || deliveryPlaceholder(task.Outcome) {
			issues = append(issues, key+": title and outcome must be concrete")
		}
		acceptance := map[string]bool{}
		covered := map[string]bool{}
		for _, row := range task.Acceptance {
			if strings.TrimSpace(row.ID) == "" || deliveryPlaceholder(row.Outcome) {
				issues = append(issues, key+": acceptance rows require an id and concrete outcome")
			}
			if acceptance[row.ID] {
				issues = append(issues, key+": duplicate acceptance id "+row.ID)
			}
			acceptance[row.ID] = true
		}
		if len(task.Acceptance) == 0 {
			issues = append(issues, key+": acceptance is required")
		}
		for _, row := range task.Verification {
			check := strings.TrimSpace(row.Check)
			if deliveryPlaceholder(check) || (!strings.HasPrefix(check, "command: ") && !strings.HasPrefix(check, "manual proof: ")) {
				issues = append(issues, key+": verification must use an exact command: or manual proof: check")
			}
			for _, cover := range splitCSV(row.Covers) {
				if !acceptance[cover] {
					issues = append(issues, key+": verification references unknown acceptance "+cover)
				}
				covered[cover] = true
			}
		}
		if len(task.Verification) == 0 {
			issues = append(issues, key+": verification is required")
		}
		for id := range acceptance {
			if !covered[id] {
				issues = append(issues, key+": acceptance "+id+" has no mapped verification")
			}
		}
		if deliveryPlaceholder(task.Artifact.Kind) || deliveryInvalidProductionPath(task.Artifact.Path) || deliveryPlaceholder(task.Artifact.Summary) {
			issues = append(issues, key+": artifact requires kind, summary, and a repo-relative production path")
		}
		if task.Risk != "" {
			if _, ok := risks[strings.ToLower(task.Risk)]; !ok {
				issues = append(issues, key+": invalid risk "+task.Risk)
			}
		}
		if task.Priority != "" {
			if _, ok := priorities[strings.ToLower(task.Priority)]; !ok {
				issues = append(issues, key+": invalid priority "+task.Priority)
			}
		}
		if task.Size != "" {
			if _, ok := sizes[strings.ToLower(task.Size)]; !ok {
				issues = append(issues, key+": invalid size "+task.Size)
			}
		}
		for _, dep := range task.Dependencies {
			kind := fallback(strings.ToLower(strings.TrimSpace(dep.Kind)), "hard")
			if kind != "hard" && kind != "soft" {
				issues = append(issues, key+": dependency kind must be hard or soft")
			}
		}
	}
	for _, task := range plan.Tasks {
		for _, dep := range task.Dependencies {
			if !keys[dep.Task] {
				issues = append(issues, task.SourceKey+": dangling dependency "+dep.Task)
			}
		}
	}
	frontiers, cycle := deliveryFrontiers(plan)
	if cycle {
		issues = append(issues, "task dependency graph contains a cycle")
	}
	return uniqueStrings(issues), frontiers
}

func deliveryTaskMapping(vaultPath string, plan deliveryPlan) (map[string]string, string, error) {
	idx, err := loadV7Index(vaultPath)
	if err != nil {
		return nil, "", err
	}
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

func applyDeliveryImport(vaultPath string, plan deliveryPlan, report deliveryImportReport, args Args) error {
	now := time.Now().UTC().Format(time.RFC3339)
	actor := fallback(firstNonEmpty(args.String("by"), args.String("actor")), "agent:"+defaultActorName())
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
			deps = append(deps, report.TaskMapping[dep.Task]+":"+fallback(strings.ToLower(dep.Kind), "hard"))
		}
		contractFingerprint := deliveryTaskFingerprint(task)
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
			"artifact_contract": map[string]any{"kind": task.Artifact.Kind, "path": task.Artifact.Path, "summary": task.Artifact.Summary},
			"owned_paths":       task.OwnedPaths, "runner_profile": firstNonEmpty(task.RunnerProfile, plan.RunnerProfile),
			"concurrency_group": task.ConcurrencyGroup, "knowledge_nodes": task.KnowledgeNodes, "wave": report.WaveID,
			"created_at": createdAt, "created_by": createdBy, "updated_at": now, "updated_by": actor,
		}
		if existing != nil && (status != "backlog" || readiness != "held") {
			for _, field := range []string{"proof_status", "proof_required", "proof_required_owner", "evidence_budget", "gates", "evidence_required", "machine_status", "human_status", "closeout_status", "agent_action", "next_owner", "next_source", "next_ref", "next_action", "accepted_by", "accepted_at", "closed_at"} {
				if value, ok := existing[field]; ok {
					data[field] = value
				}
			}
		}
		body := renderDeliveryTaskBody(id, task)
		data["state_rev"] = v7StateRev(data, body)
		content, err := serializeDocument(data, body, v7FrontmatterOrder["task"])
		if err != nil {
			return err
		}
		writes[path] = content
	}
	wavePath := filepath.Join(vaultPath, "work", "waves", report.WaveID+".md")
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
		"title": report.WaveTitle, "status": "open", "members": members, "integration_branch": v7IntegrationBranchName(report.WaveID),
		"spec_refs": plan.SpecRefs, "delivery_plan_scope": report.PlanScope, "delivery_plan_fingerprint": report.PlanFingerprint, "concurrency": report.ExpectedConcurrency,
		"runner_profile": plan.RunnerProfile, "created_at": waveCreatedAt, "created_by": waveCreatedBy, "updated_at": now, "updated_by": actor,
	}
	waveBody := fmt.Sprintf("# %s · %s\n\n## Members\n\nImported atomically from delivery plan `%s`. Members remain held until wave preflight and authorization.\n", report.WaveID, report.WaveTitle, report.PlanFingerprint)
	waveData["state_rev"] = v7StateRev(waveData, waveBody)
	waveContent, err := serializeDocument(waveData, waveBody, v7FrontmatterOrder["wave"])
	if err != nil {
		return err
	}
	writes[wavePath] = waveContent
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
	if args.Bool("fail-after-first-write") {
		return commitDeliveryWrites(writes, 1)
	}
	return commitDeliveryWrites(writes, 0)
}

func commitDeliveryWrites(writes map[string]string, failAfter int) error {
	paths := make([]string, 0, len(writes))
	for path := range writes {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	type backup struct {
		content []byte
		existed bool
	}
	backups := map[string]backup{}
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err == nil {
			backups[path] = backup{content: raw, existed: true}
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	rollback := func() {
		for _, path := range paths {
			b := backups[path]
			if b.existed {
				_ = writeText(path, string(b.content))
			} else {
				_ = os.Remove(path)
			}
		}
	}
	for i, path := range paths {
		if err := writeText(path, writes[path]); err != nil {
			rollback()
			return err
		}
		if failAfter > 0 && i+1 >= failAfter {
			rollback()
			return tuskerError(errorInvalidArg, "forced delivery import write failure")
		}
	}
	return nil
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
	if !strings.Contains(body, "## Work streams") && !strings.Contains(body, "## Work Streams") {
		body = strings.TrimRight(body, "\n") + "\n\n## Work streams\n"
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

func deliveryTaskFingerprint(task deliveryPlanTask) string {
	raw, _ := yaml.Marshal(task)
	return deliveryFingerprint(raw)
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
