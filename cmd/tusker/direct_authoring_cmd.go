package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const directWaveAuthoringSchema = "tusker.wave-authoring/v1"

const directWaveAuthoringReceiptSchema = "tusker.wave-authoring-receipt/v1"

const directStandaloneTaskEpic = "TSK"

type directWaveAuthoringRequest struct {
	Schema        string                    `yaml:"schema" json:"schema"`
	RequestKey    string                    `yaml:"request_key,omitempty" json:"request_key,omitempty"`
	Title         string                    `yaml:"title" json:"title"`
	Outcome       string                    `yaml:"outcome" json:"outcome"`
	Concurrency   int                       `yaml:"concurrency,omitempty" json:"concurrency,omitempty"`
	SpecRefs      []string                  `yaml:"spec_refs,omitempty" json:"spec_refs,omitempty"`
	SharedContext string                    `yaml:"shared_context,omitempty" json:"shared_context,omitempty"`
	Tasks         []directWaveAuthoringTask `yaml:"tasks" json:"tasks"`
	HumanActions  []directWaveHumanAction   `yaml:"human_actions,omitempty" json:"human_actions,omitempty"`
}

type directWaveAuthoringDependency struct {
	Task string `yaml:"task" json:"task"`
	Kind string `yaml:"kind,omitempty" json:"kind,omitempty"`
}

type directWaveAuthoringTask struct {
	Key                 string                          `yaml:"key" json:"key"`
	Title               string                          `yaml:"title" json:"title"`
	WorkLevel           string                          `yaml:"work_level" json:"work_level"`
	ReviewLevel         string                          `yaml:"review_level,omitempty" json:"review_level,omitempty"`
	ReviewReason        string                          `yaml:"review_reason,omitempty" json:"review_reason,omitempty"`
	Body                string                          `yaml:"body" json:"body"`
	ImplementationNotes string                          `yaml:"implementation_notes,omitempty" json:"implementation_notes,omitempty"`
	Epic                string                          `yaml:"epic,omitempty" json:"epic,omitempty"`
	SpecRefs            []string                        `yaml:"spec_refs,omitempty" json:"spec_refs,omitempty"`
	Dependencies        []directWaveAuthoringDependency `yaml:"dependencies,omitempty" json:"dependencies,omitempty"`
	OwnedPaths          []string                        `yaml:"owned_paths,omitempty" json:"owned_paths,omitempty"`
	GeneratedOutputs    []string                        `yaml:"generated_outputs,omitempty" json:"generated_outputs,omitempty"`
}

type directAuthoringIssue struct {
	Code    string
	Message string
}

func uniqueDirectAuthoringIssues(issues []directAuthoringIssue) []directAuthoringIssue {
	seen := map[string]bool{}
	out := issues[:0]
	for _, issue := range issues {
		key := issue.Code + "\x00" + issue.Message
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, issue)
	}
	return out
}

func directWaveTaskContractFingerprint(data map[string]any, body string) string {
	canon := map[string]any{
		"title":                stringField(data, "title"),
		"body":                 directWaveCanonicalContractBody(body),
		"artifact_contract":    data["artifact_contract"],
		"proof_mode":           stringField(data, "proof_mode"),
		"proof_required":       sortedStrings(normalizeList(data["proof_required"])),
		"proof_required_owner": data["proof_required_owner"],
		"evidence_budget":      intField(data, "evidence_budget"),
		"evidence_required":    sortedStrings(normalizeList(data["evidence_required"])),
		"dependencies":         sortedStrings(normalizeList(data["dependencies"])),
		"gates":                sortedStrings(normalizeList(data["gates"])),
		"work_level":           stringField(data, "work_level"),
		"review_level":         stringField(data, "review_level"),
		"review_reason":        stringField(data, "review_reason"),
		"owned_paths":          sortedStrings(normalizeList(data["owned_paths"])),
		"generated_outputs":    sortedStrings(normalizeList(data["generated_outputs"])),
	}
	raw, _ := yaml.Marshal(canon)
	return v7Fingerprint(raw)
}

// directWaveContractLedgerSections are task-body sections the lifecycle ledger
// appends during authorized work (proof links, activity notes). They are
// durable history, not authored contract material, so the fingerprinted
// contract canon excludes them.
var directWaveContractLedgerSections = map[string]bool{
	"evidence": true,
	"work log": true,
}

// directWaveContractLedgerColumns are table columns whose values the lifecycle
// fills in after authoring (verification outcomes, execution notes). The
// column's presence is authored schema; its per-row values are not contract
// identity, so the fingerprint keeps the header position but drops the cells.
var directWaveContractLedgerColumns = map[string]bool{
	"result": true,
	"notes":  true,
}

func directWaveContractCells(cells []string, ledgerCols map[int]bool) []string {
	out := make([]string, 0, len(cells))
	for i, cell := range cells {
		if ledgerCols[i] {
			continue
		}
		out = append(out, cell)
	}
	return out
}

func directWaveCanonicalContractBody(body string) string {
	lines := strings.Split(v7CanonicalBody(body), "\n")
	// Lifecycle writes extend the proof ledger without re-authoring the
	// contract: evidence and work-log sections, the generated reviewer-findings
	// section, and the typed-review receipt row. Treating those appends as
	// contract edits would stale every wave after its first landing.
	filtered := make([]string, 0, len(lines))
	dropping := false
	inFence := false
	for _, line := range lines {
		delimiter := isFenceDelimiter(line)
		if delimiter {
			inFence = !inFence
		}
		if !delimiter && !inFence && strings.HasPrefix(strings.TrimSpace(line), "## ") {
			dropping = directWaveContractLedgerSections[strings.ToLower(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "## ")))]
			if dropping {
				continue
			}
		} else if dropping {
			continue
		}
		filtered = append(filtered, line)
	}
	if start, end := generatedReviewerFindingBounds(filtered); start != -1 {
		filtered = append(filtered[:start], filtered[end:]...)
	}
	var out []string
	section := ""
	inFence = false
	var tableLedgerCols map[int]bool
	for _, line := range filtered {
		trimmed := strings.TrimSpace(line)
		if isFenceDelimiter(line) {
			inFence = !inFence
			out = append(out, line)
			continue
		}
		if inFence {
			out = append(out, line)
			continue
		}
		if strings.HasPrefix(trimmed, "## ") {
			section = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(trimmed, "## ")))
			tableLedgerCols = nil
			out = append(out, line)
			continue
		}
		if !strings.HasPrefix(trimmed, "|") || (section != "acceptance" && section != "verification") {
			tableLedgerCols = nil
			out = append(out, line)
			continue
		}
		cells := v7MarkdownTableCells(trimmed)
		if waveMaterialLedgerRow(cells) {
			continue
		}
		if tableLedgerCols == nil {
			// First row of a table is its authored header; columns named for
			// lifecycle results are ledger slots, not contract identity.
			tableLedgerCols = map[int]bool{}
			for i, h := range cells {
				if directWaveContractLedgerColumns[strings.ToLower(strings.TrimSpace(h))] {
					tableLedgerCols[i] = true
				}
			}
			out = append(out, "| "+strings.Join(directWaveContractCells(cells, tableLedgerCols), " | ")+" |")
			continue
		}
		out = append(out, "| "+strings.Join(directWaveContractCells(cells, tableLedgerCols), " | ")+" |")
	}
	// Dropped ledger sections leave the separator blank that preceded them;
	// when a dropped section is at an edge of the body that blank would join as
	// stray edge whitespace and drift the canon.
	return strings.Trim(strings.Join(out, "\n"), "\n")
}

type directWaveHumanAction struct {
	Key            string   `yaml:"key" json:"key"`
	Task           string   `yaml:"task" json:"task"`
	Owner          string   `yaml:"owner" json:"owner"`
	Action         string   `yaml:"action" json:"action"`
	Verification   string   `yaml:"verification" json:"verification"`
	WhyAgentCannot string   `yaml:"why_agent_cannot" json:"why_agent_cannot"`
	Covers         []string `yaml:"covers,omitempty" json:"covers,omitempty"`
}

func v7AuthoringInputBytes(vaultPath, value string) ([]byte, error) {
	if value == "-" {
		return io.ReadAll(os.Stdin)
	}
	path := value
	if !filepath.IsAbs(path) {
		path = filepath.Join(v7RepoRoot(vaultPath), path)
	}
	return os.ReadFile(path)
}

func v7AuthoringBodyFile(vaultPath, value string) (string, error) {
	raw, err := v7AuthoringInputBytes(vaultPath, value)
	if err != nil {
		return "", err
	}
	body := normalizeV7AuthoringBody(string(raw))
	if strings.TrimSpace(body) == "" {
		return "", tuskerError(errorMissingField, "authored body is required and must not be whitespace-only", withContext(map[string]any{"field": "body"}))
	}
	return body, nil
}

func normalizeV7AuthoringBody(body string) string {
	return strings.TrimRight(body, "\n") + "\n"
}

func directWaveAuthoringTaskBody(task directWaveAuthoringTask) string {
	body := normalizeV7AuthoringBody(task.Body)
	notes := strings.TrimSpace(task.ImplementationNotes)
	if notes == "" {
		return body
	}
	if strings.Contains(body, "\n## Implementation notes") || strings.HasPrefix(body, "## Implementation notes") {
		return normalizeV7AuthoringBody(replaceSection(body, "## Implementation notes", notes))
	}
	return strings.TrimRight(body, "\n") + "\n\n## Implementation notes\n\n" + notes + "\n"
}

func rebindV7DependencyContracts(taskID string, task Note, idx v7Index) ([]any, error) {
	raw, present := task.Data["dependencies"]
	if !present || raw == nil {
		return nil, nil
	}
	var raws []any
	switch value := raw.(type) {
	case []any:
		raws = value
	case []string:
		for _, item := range value {
			raws = append(raws, item)
		}
	case string:
		raws = []any{value}
	default:
		return nil, tuskerError(errorInvalidArg, "dependencies for "+taskID+" must be a list of durable task IDs")
	}

	consumerWave := strings.TrimSpace(stringField(task.Data, "wave"))
	seen := map[string]bool{}
	contracts := make([]any, 0, len(raws))
	for _, rawEdge := range raws {
		value, ok := rawEdge.(string)
		if !ok || strings.TrimSpace(value) == "" {
			return nil, tuskerError(errorInvalidArg, "dependency target is malformed for "+taskID)
		}
		edge := parseV7DependencyEdge(strings.TrimSpace(value))
		target := strings.TrimSpace(edge.ID)
		if !v7TaskIDPattern.MatchString(target) {
			return nil, tuskerError(errorInvalidArg, "dependency target is malformed: "+strings.TrimSpace(value))
		}
		if target == taskID {
			return nil, tuskerError(errorInvalidArg, "task cannot depend on itself: "+target)
		}
		if seen[target] {
			return nil, tuskerError(errorInvalidArg, "duplicate dependency target: "+target)
		}
		seen[target] = true
		targetTask, ok := idx.Tasks[target]
		if !ok {
			return nil, tuskerError(errorNotFound, "dependency target task not found: "+target)
		}
		kind := edge.Hardness
		if kind == "" {
			kind = v7DefaultDependencyHardness(targetTask)
		}
		if kind != v7DependencyHardnessHard && kind != v7DependencyHardnessSoft {
			return nil, tuskerError(errorInvalidArg, "dependency kind must be hard or soft: "+strings.TrimSpace(value))
		}
		if strings.TrimSpace(stringField(targetTask.Data, "wave")) == consumerWave {
			continue
		}
		contracts = append(contracts, map[string]any{
			"task_id":                     target,
			"kind":                        kind,
			"target_contract_fingerprint": directWaveTaskContract(targetTask),
		})
	}
	sort.Slice(contracts, func(i, j int) bool {
		left, _ := contracts[i].(map[string]any)
		right, _ := contracts[j].(map[string]any)
		return stringField(left, "task_id") < stringField(right, "task_id")
	})
	return contracts, nil
}

var directWaveTaskUpdateInjectCommitFailAfter = 0

func updateV7TaskCmd(args Args) error {
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	if err := ensureV7ControlMutation(vaultPath, args); err != nil {
		return err
	}
	id := strings.ToUpper(strings.TrimSpace(firstNonEmpty(args.String("id"), args.String("_pos0"))))
	if id == "" {
		return tuskerError(errorMissingArg, "Usage: tusker task update <TASK-ID> --if-revision <state_rev> [--body-file <path|->] [--title <title>] [--work-level light|standard|demanding] [--review-level <level> --review-reason <reason>] [--spec-refs <csv>] [--dependencies <csv>] [--rebind-contract] [--rebind-dependency-contracts] [--owned-paths <csv>] [--generated-outputs <csv>] [--execute-profile <name>|--clear-execute-profile] [--review-profile <name>|--clear-review-profile] --by <actor> [--json]")
	}
	if !v7TaskIDPattern.MatchString(id) {
		return tuskerError(errorInvalidArg, "invalid task id: "+id)
	}
	baseRev := strings.TrimSpace(args.String("if-revision"))
	if baseRev == "" {
		return tuskerError(errorMissingArg, "Missing required --if-revision <state_rev>")
	}
	if strings.TrimSpace(firstNonEmpty(args.String("by"), args.String("actor"))) == "" {
		return tuskerError(errorMissingArg, "Missing required --by <actor>")
	}
	actor, err := v7AgentDefaultActor(args, "task update")
	if err != nil {
		return err
	}
	materialLock, err := acquireV7MaterialEpochLock(vaultPath)
	if err != nil {
		return err
	}
	defer materialLock.Close()
	note, err := resolveV7Note(vaultPath, id, "task")
	if err != nil {
		return err
	}
	taskLock, err := acquireV7DocumentLock(note.AbsolutePath, v7DocumentLockTimeout)
	if err != nil {
		return err
	}
	defer func() { _ = taskLock.Close() }()
	data, body, err := parseFrontmatterMustRead(note.AbsolutePath)
	if err != nil {
		return err
	}
	currentRev := stringField(data, "state_rev")
	if strings.TrimSpace(currentRev) != "" && !v7StateRevMatches(data, body, currentRev) {
		return tuskerError("CAS_CONFLICT", "V7 object content changed without a refreshed state_rev: "+filepath.Base(note.AbsolutePath), withPath(note.AbsolutePath), withHint("run `tusker reconcile` to repair the object metadata before retrying the control operation"), withContext(map[string]any{"current_rev": currentRev, "actual_rev": v7StateRev(data, body)}))
	}
	if strings.TrimSpace(currentRev) != baseRev {
		return tuskerError("CAS_CONFLICT", "V7 object changed since it was loaded: "+filepath.Base(note.AbsolutePath), withPath(note.AbsolutePath), withHint("reload the object and retry the Tusker control operation"), withContext(map[string]any{"base_rev": baseRev, "current_rev": currentRev}))
	}
	priorFingerprint := directWaveTaskContractFingerprint(data, body)
	priorStoredFingerprint := strings.TrimSpace(stringField(data, "contract_fingerprint"))
	priorStatus := strings.ToLower(strings.TrimSpace(stringField(data, "status")))
	var idx v7Index
	idxLoaded := false
	loadIdx := func() (v7Index, error) {
		if !idxLoaded {
			idxLoaded = true
			idx, err = loadV7Index(vaultPath)
		}
		return idx, err
	}
	changes := map[string]any{}
	mutate := func(field string, next any) {
		if toString(data[field]) == toString(next) {
			return
		}
		changes[field] = map[string]any{"from": data[field], "to": next}
		data[field] = next
	}
	clear := func(field string) {
		if _, ok := data[field]; !ok {
			return
		}
		changes[field] = map[string]any{"from": data[field], "to": nil}
		delete(data, field)
	}
	if _, ok := args["body-file"]; ok {
		next, err := v7AuthoringBodyFile(vaultPath, strings.TrimSpace(args.String("body-file")))
		if err != nil {
			return err
		}
		if next != body {
			changes["body"] = map[string]any{"from": "<body>", "to": "<body>"}
			body = next
		}
	}
	if value, ok := args["title"]; ok {
		title := strings.TrimSpace(value)
		if title == "" {
			return tuskerError(errorMissingField, "--title must not be empty", withContext(map[string]any{"field": "title"}))
		}
		mutate("title", title)
	}
	workLevel := strings.ToLower(strings.TrimSpace(stringField(data, "work_level")))
	if value, ok := args["work-level"]; ok {
		level := strings.ToLower(strings.TrimSpace(value))
		if !validModelLevel(level) {
			return tuskerError(errorInvalidArg, "--work-level must be light, standard, or demanding")
		}
		mutate("work_level", level)
		workLevel = level
	}
	_, reviewReasonGiven := args["review-reason"]
	if value, ok := args["review-level"]; ok {
		level := strings.ToLower(strings.TrimSpace(value))
		if !validModelLevel(level) {
			return tuskerError(errorInvalidArg, "--review-level must be light, standard, or demanding")
		}
		reason := strings.TrimSpace(args.String("review-reason"))
		existingOverride := strings.TrimSpace(stringField(data, "review_reason")) != "" || (strings.TrimSpace(stringField(data, "review_level")) != "" && strings.TrimSpace(stringField(data, "review_level")) != workLevel)
		if reason == "" && (level != workLevel || existingOverride) {
			return tuskerError(errorInvalidArg, "--review-level override requires --review-reason")
		}
		mutate("review_level", level)
		if reviewReasonGiven {
			mutate("review_reason", reason)
		} else if level == workLevel {
			delete(data, "review_reason")
		}
	} else if reviewReasonGiven {
		return tuskerError(errorInvalidArg, "--review-reason requires --review-level")
	}
	if value, ok := args["spec-refs"]; ok {
		refs := splitCSV(value)
		index, err := loadIdx()
		if err != nil {
			return err
		}
		for _, ref := range refs {
			if msg, hint := v7SpecRefError(vaultPath, ref, index.Decisions); msg != "" {
				return tuskerError(errorInvalidArg, msg, withHint(hint))
			}
		}
		mutate("spec_refs", refs)
	}
	if value, ok := args["dependencies"]; ok {
		index, err := loadIdx()
		if err != nil {
			return err
		}
		deps, err := validateV7TaskUpdateDependencies(index, id, splitCSV(value))
		if err != nil {
			return err
		}
		mutate("dependencies", deps)
	}
	if args.Bool("rebind-dependency-contracts") {
		index, err := loadIdx()
		if err != nil {
			return err
		}
		next, err := rebindV7DependencyContracts(id, Note{Data: data, Body: body}, index)
		if err != nil {
			return err
		}
		if len(next) == 0 {
			clear("dependency_contracts")
		} else {
			previousYAML, _ := yaml.Marshal(data["dependency_contracts"])
			nextYAML, _ := yaml.Marshal(next)
			if !bytes.Equal(previousYAML, nextYAML) {
				changes["dependency_contracts"] = map[string]any{"from": data["dependency_contracts"], "to": next}
				data["dependency_contracts"] = next
			}
		}
	}
	if value, ok := args["owned-paths"]; ok {
		mutate("owned_paths", normalizeOwnedPaths(splitCSV(value)))
	}
	// Per-task profile pins reference global profiles by name; they are
	// routing, not contract, so they never change the fingerprint.
	var profileWorkflow *WorkflowFile
	for _, item := range []struct{ flag, field, lane string }{{"execute-profile", "execute_profile", runLaneExecute}, {"review-profile", "review_profile", runLaneReview}} {
		value, set := args[item.flag]
		if args.Bool("clear-" + item.flag) {
			if set {
				return tuskerError(errorInvalidArg, "--"+item.flag+" and --clear-"+item.flag+" are mutually exclusive")
			}
			clear(item.field)
			continue
		}
		if !set {
			continue
		}
		profile := strings.TrimSpace(value)
		if profile == "" || profile == "true" {
			return tuskerError(errorMissingArg, "--"+item.flag+" requires a global profile name; use --clear-"+item.flag+" to remove the pin")
		}
		if profileWorkflow == nil {
			wf, err := loadWorkflow(vaultPath)
			if err != nil {
				return err
			}
			profileWorkflow = &wf
		}
		if _, err := resolveRunnerProfileForNote(Note{Data: map[string]any{item.field: profile}}, profileWorkflow.Data, item.lane); err != nil {
			return err
		}
		mutate(item.field, profile)
	}
	if value, ok := args["generated-outputs"]; ok {
		mutate("generated_outputs", normalizeOwnedPaths(splitCSV(value)))
	}
	contractRebound := false
	if args.Bool("rebind-contract") {
		nextFingerprint := directWaveTaskContractFingerprint(data, body)
		if priorStoredFingerprint == "" {
			return tuskerError(errorInvalidTransition, "task has no stored contract_fingerprint to rebind")
		}
		if priorStoredFingerprint == nextFingerprint {
			return tuskerError(errorInvalidArg, "task contract_fingerprint is already current")
		}
		changes["contract_fingerprint"] = map[string]any{"from": priorStoredFingerprint, "to": nextFingerprint}
		data["contract_fingerprint"] = nextFingerprint
		contractRebound = true
	}
	if len(changes) == 0 {
		return tuskerError(errorMissingArg, "task update requires at least one mutable field: --body-file, --title, --work-level, --review-level, --spec-refs, --dependencies, --rebind-contract, --rebind-dependency-contracts, --owned-paths, --generated-outputs, --execute-profile, --review-profile, --clear-execute-profile, or --clear-review-profile")
	}
	materialChanged := contractRebound || directWaveTaskContractFingerprint(data, body) != priorFingerprint
	if materialChanged {
		var invalidated int
		body, invalidated = invalidateV7PassedVerificationRows(body)
		if invalidated > 0 {
			changes["verification_results"] = map[string]any{"invalidated": invalidated}
		}
		mutate("proof_status", "pending")
	}
	reworked := false
	if materialChanged && (priorStatus == "done" || priorStatus == "review") {
		reworked = true
		mutate("status", "rework")
		mutate("readiness", "ready")
		mutate("proof_status", "pending")
		mutate("next_owner", "agent")
		mutate("next_source", "task")
		mutate("next_ref", id)
		mutate("next_action", "Execute the task contract and satisfy proof mode.")
		for _, field := range []string{"accepted_by", "accepted_at", "closed_at", "close_authority", "closeout_status", "machine_status", "human_status", "agent_action"} {
			clear(field)
		}
	}
	data["updated_at"] = time.Now().UTC().Format(time.RFC3339)
	data["updated_by"] = actor
	if materialChanged {
		data["contract_fingerprint"] = directWaveTaskContractFingerprint(data, body)
	}
	nextRev := v7StateRev(data, body)
	data["state_rev"] = nextRev
	taskContent, err := serializeDocument(data, body, v7FrontmatterOrder["task"])
	if err != nil {
		return err
	}
	payload := map[string]any{"changes": changes, "state_rev": nextRev, "source": "task update"}
	if reworked {
		payload["lifecycle_transition"] = "rework"
	}
	eventPath, eventContent, err := prepareV7Event(vaultPath, id, "task", "updated", actor, payload, time.Now().UTC())
	if err != nil {
		return err
	}
	if err := ensureDir(filepath.Dir(eventPath)); err != nil {
		return err
	}
	if err := commitV7DocumentWritesWithLocks(map[string]string{note.AbsolutePath: taskContent, eventPath: eventContent}, directWaveTaskUpdateInjectCommitFailAfter, []*v7DocumentLock{taskLock}); err != nil {
		return err
	}
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": true, "task": id, "state_rev": nextRev, "changes": changes})
	} else if !args.Bool("quiet") {
		fmt.Printf("Updated task %s (state_rev %s)\n", id, nextRev)
	}
	return nil
}

func validateV7TaskUpdateDependencies(idx v7Index, taskID string, raws []string) ([]string, error) {
	var deps []string
	var targets []string
	for _, raw := range raws {
		entry := strings.TrimSpace(raw)
		if entry == "" {
			continue
		}
		target, kind, hasKind := strings.Cut(entry, ":")
		target = strings.ToUpper(strings.TrimSpace(wikiTarget(target)))
		if target == "" {
			return nil, tuskerError(errorInvalidArg, "dependency target must be a durable task ID: "+entry)
		}
		kind = strings.ToLower(strings.TrimSpace(kind))
		if hasKind && kind != v7DependencyHardnessHard && kind != v7DependencyHardnessSoft {
			return nil, tuskerError(errorInvalidArg, "dependency kind must be hard or soft: "+entry)
		}
		if target == taskID {
			return nil, tuskerError(errorInvalidArg, "task cannot depend on itself: "+entry)
		}
		dep, ok := idx.Tasks[target]
		if !ok {
			return nil, tuskerError(errorNotFound, "dependency target task not found: "+target)
		}
		if kind == "" {
			kind = v7DefaultDependencyHardness(dep)
		}
		deps = append(deps, target+":"+kind)
		targets = append(targets, target)
	}
	seen := map[string]bool{}
	stack := append([]string{}, targets...)
	for len(stack) > 0 {
		current := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if current == taskID {
			return nil, tuskerError(errorInvalidArg, "dependency update creates a cycle through "+taskID)
		}
		if seen[current] {
			continue
		}
		seen[current] = true
		dep, ok := idx.Tasks[current]
		if !ok {
			continue
		}
		for _, edge := range v7TaskDependencyEdges(dep, idx) {
			stack = append(stack, edge.ID)
		}
	}
	return deps, nil
}

func normalizeDirectWaveAuthoringRequest(req *directWaveAuthoringRequest) {
	req.RequestKey = strings.TrimSpace(req.RequestKey)
	for i := range req.Tasks {
		req.Tasks[i].Key = strings.TrimSpace(req.Tasks[i].Key)
		for j := range req.Tasks[i].Dependencies {
			req.Tasks[i].Dependencies[j].Task = strings.TrimSpace(req.Tasks[i].Dependencies[j].Task)
		}
	}
	for i := range req.HumanActions {
		req.HumanActions[i].Key = strings.TrimSpace(req.HumanActions[i].Key)
		req.HumanActions[i].Task = strings.TrimSpace(req.HumanActions[i].Task)
	}
}

func directWaveAuthoringFingerprint(req directWaveAuthoringRequest) string {
	raw, err := yaml.Marshal(req)
	if err != nil {
		return ""
	}
	return v7Fingerprint(raw)
}

// directWaveFrontiers topologically orders task keys by their in-request
// dependency edges. It reports the frontier layers and whether every task
// resolved (false means the graph contains a cycle or an unresolved member).
func directWaveRequestFrontiers(tasks []directWaveAuthoringTask) ([][]string, bool) {
	remaining := map[string]int{}
	next := map[string][]string{}
	keys := map[string]bool{}
	for _, task := range tasks {
		keys[task.Key] = true
	}
	for _, task := range tasks {
		deps := map[string]bool{}
		for _, dep := range task.Dependencies {
			if keys[dep.Task] && !deps[dep.Task] {
				deps[dep.Task] = true
				next[dep.Task] = append(next[dep.Task], task.Key)
			}
		}
		remaining[task.Key] = len(deps)
	}
	var out [][]string
	seen := 0
	for len(remaining) > 0 {
		var frontier []string
		for id, n := range remaining {
			if n == 0 {
				frontier = append(frontier, id)
			}
		}
		sort.Strings(frontier)
		if len(frontier) == 0 {
			return nil, false
		}
		out = append(out, frontier)
		for _, id := range frontier {
			delete(remaining, id)
			seen++
			for _, child := range next[id] {
				remaining[child]--
			}
		}
	}
	return out, seen == len(tasks)
}

func directWaveExpectedConcurrency(concurrency int, frontiers [][]string) int {
	maxFrontier := 0
	for _, frontier := range frontiers {
		maxFrontier = maxInt(maxFrontier, len(frontier))
	}
	if concurrency <= 0 {
		return maxInt(1, maxFrontier)
	}
	return minInt(concurrency, maxInt(1, maxFrontier))
}

func validateDirectWaveAuthoring(vaultPath string, req directWaveAuthoringRequest) ([]directAuthoringIssue, [][]string) {
	var issues []directAuthoringIssue
	add := func(code, message string) {
		issues = append(issues, directAuthoringIssue{Code: code, Message: message})
	}
	if req.Schema != directWaveAuthoringSchema {
		add("AUTHORING_REQUEST_INVALID", "schema must be "+directWaveAuthoringSchema)
	}
	if strings.TrimSpace(req.RequestKey) == "" {
		add("AUTHORING_REQUEST_INVALID", "request_key is required; pass --request-key or set request_key in the request")
	}
	if strings.TrimSpace(req.Title) == "" {
		add("AUTHORING_REQUEST_INVALID", "title is required")
	}
	if strings.TrimSpace(req.Outcome) == "" {
		add("AUTHORING_REQUEST_INVALID", "outcome is required")
	}
	if len(req.Tasks) == 0 {
		add("AUTHORING_REQUEST_INVALID", "at least one task is required")
	}
	var idx v7Index
	idxLoaded := false
	idxFailed := false
	loadIdx := func() (v7Index, bool) {
		if !idxLoaded {
			idxLoaded = true
			var err error
			idx, err = loadV7Index(vaultPath)
			if err != nil {
				idxFailed = true
				add("AUTHORING_REQUEST_INVALID", "V7 index load failed: "+err.Error())
			}
		}
		return idx, !idxFailed
	}
	resolveSpecRefs := func(refs []string, where string) {
		if len(refs) == 0 {
			return
		}
		index, ok := loadIdx()
		if !ok {
			return
		}
		for _, ref := range refs {
			if msg, hint := v7SpecRefError(vaultPath, ref, index.Decisions); msg != "" {
				add("AUTHORING_REQUEST_INVALID", where+": "+msg+"; "+hint)
			}
		}
	}
	resolveSpecRefs(req.SpecRefs, "wave")
	keys := map[string]bool{}
	taskAcceptanceIDs := map[string]map[string]bool{}
	for _, task := range req.Tasks {
		key := strings.TrimSpace(task.Key)
		if key == "" {
			add("AUTHORING_REQUEST_INVALID", "every task requires a nonempty temporary key")
		} else if keys[key] {
			add("AUTHORING_REQUEST_INVALID", "duplicate task key: "+key)
		}
		keys[key] = true
		if strings.TrimSpace(task.Title) == "" {
			add("AUTHORING_REQUEST_INVALID", key+": title is required")
		}
		workLevel := strings.ToLower(strings.TrimSpace(task.WorkLevel))
		if workLevel == "" {
			add("AUTHORING_REQUEST_INVALID", key+": work_level is required for agent work; use light, standard, or demanding")
		} else if !validModelLevel(workLevel) {
			add("AUTHORING_REQUEST_INVALID", key+": invalid work_level "+task.WorkLevel)
		}
		reviewLevel := strings.ToLower(strings.TrimSpace(task.ReviewLevel))
		reviewReason := strings.TrimSpace(task.ReviewReason)
		if reviewLevel != "" && !validModelLevel(reviewLevel) {
			add("AUTHORING_REQUEST_INVALID", key+": invalid review_level "+task.ReviewLevel)
		} else if reviewLevel != "" && reviewLevel != workLevel && reviewReason == "" {
			add("AUTHORING_REQUEST_INVALID", key+": review_level override requires review_reason")
		} else if reviewLevel == "" && reviewReason != "" {
			add("AUTHORING_REQUEST_INVALID", key+": review_reason requires an explicit review_level override")
		}
		if strings.TrimSpace(task.Body) == "" {
			add("AUTHORING_REQUEST_INVALID", key+": body is required and must not be whitespace-only")
		}
		acceptance := map[string]bool{}
		for _, id := range v7AcceptanceIDs(task.Body) {
			acceptance[id] = true
		}
		taskAcceptanceIDs[key] = acceptance
		if epic := strings.ToUpper(strings.TrimSpace(task.Epic)); epic != "" {
			if !epicAcronymPattern.MatchString(epic) {
				add("AUTHORING_REQUEST_INVALID", key+": epic must be a three-letter acronym")
			} else if index, ok := loadIdx(); ok {
				if _, exists := index.Epics[epic]; !exists {
					add("AUTHORING_REQUEST_INVALID", key+": epic does not exist: "+epic)
				}
			}
		}
		resolveSpecRefs(task.SpecRefs, key)
		for _, dep := range task.Dependencies {
			kind := fallback(strings.ToLower(strings.TrimSpace(dep.Kind)), "hard")
			if kind != "hard" && kind != "soft" {
				add("AUTHORING_REQUEST_INVALID", key+": dependency kind must be hard or soft")
			}
		}
	}
	for _, task := range req.Tasks {
		for _, dep := range task.Dependencies {
			if !keys[dep.Task] {
				add("DEPENDENCY_DANGLING", task.Key+": dangling dependency "+dep.Task)
			}
		}
	}
	frontiers, resolved := directWaveRequestFrontiers(req.Tasks)
	if !resolved {
		add("DEPENDENCY_CYCLE", "task dependency graph contains a cycle")
	}
	if resolved {
		byKey := map[string]directWaveAuthoringTask{}
		for _, task := range req.Tasks {
			byKey[task.Key] = task
		}
		for _, frontier := range frontiers {
			for i := 0; i < len(frontier); i++ {
				for j := i + 1; j < len(frontier); j++ {
					left, right := byKey[frontier[i]], byKey[frontier[j]]
					leftPaths := normalizeOwnedPaths(append(append([]string{}, left.OwnedPaths...), left.GeneratedOutputs...))
					rightPaths := normalizeOwnedPaths(append(append([]string{}, right.OwnedPaths...), right.GeneratedOutputs...))
					if directAuthoringPathsOverlap(leftPaths, rightPaths) {
						add("OWNED_PATH_FRONTIER_CONFLICT", frontier[i]+" and "+frontier[j]+" own colliding paths on the same frontier; serialize them with a dependency or split ownership")
					}
				}
			}
		}
	}
	actionKeys := map[string]bool{}
	for _, action := range req.HumanActions {
		key := strings.TrimSpace(action.Key)
		if key == "" {
			add("AUTHORING_REQUEST_INVALID", "every human action requires a nonempty key")
		} else if keys[key] || actionKeys[key] {
			add("AUTHORING_REQUEST_INVALID", "duplicate human action key: "+key)
		}
		actionKeys[key] = true
		if !keys[strings.TrimSpace(action.Task)] {
			add("AUTHORING_REQUEST_INVALID", "human action "+key+": unknown task "+action.Task)
		}
		for field, value := range map[string]string{"owner": action.Owner, "action": action.Action, "verification": action.Verification, "why_agent_cannot": action.WhyAgentCannot} {
			if strings.TrimSpace(value) == "" {
				add("AUTHORING_REQUEST_INVALID", "human action "+key+": "+field+" is required")
			}
		}
		if strings.TrimSpace(action.Owner) != "" && v7ProofOwnerClass(action.Owner) != "human" {
			add("AUTHORING_REQUEST_INVALID", "human action "+key+": owner must be human:<name>")
		}
		for _, cover := range action.Covers {
			id := normalizeV7AcceptanceID(cover)
			if id == "" || !taskAcceptanceIDs[strings.TrimSpace(action.Task)][id] {
				add("AUTHORING_REQUEST_INVALID", "human action "+key+": covers unknown acceptance id "+strings.TrimSpace(cover)+" for task "+strings.TrimSpace(action.Task))
			}
		}
	}
	return uniqueDirectAuthoringIssues(issues), frontiers
}

func directAuthoringPathsOverlap(left, right []string) bool {
	set := map[string]bool{}
	for _, path := range left {
		set[path] = true
	}
	for _, path := range right {
		if set[path] {
			return true
		}
	}
	return false
}

func directAuthoringTaskIDs(vaultPath string, idx v7Index, tasks []directWaveAuthoringTask) map[string]string {
	used := map[string]bool{}
	maxSeq := map[string]int{}
	for id := range idx.Tasks {
		used[id] = true
		if match := v7TaskIDPattern.FindStringSubmatch(id); match != nil {
			maxSeq[match[1]] = maxInt(maxSeq[match[1]], atoiSafe(match[2]))
		}
	}
	mapping := map[string]string{}
	for _, task := range tasks {
		epic := strings.ToUpper(strings.TrimSpace(task.Epic))
		if epic == "" {
			epic = directStandaloneTaskEpic
		}
		for {
			maxSeq[epic]++
			id := fmt.Sprintf("%s-T-%s", epic, padNumber(maxSeq[epic]))
			if used[id] || len(v7TaskIDCollisionPaths(vaultPath, id)) > 0 {
				continue
			}
			used[id] = true
			mapping[task.Key] = id
			break
		}
	}
	return mapping
}

func directAuthoringGateIDs(idx v7Index, actions []directWaveHumanAction, taskMapping map[string]string) map[string]string {
	used := map[string]bool{}
	maxSeq := map[string]int{}
	for id := range idx.Gates {
		used[id] = true
		if match := v7GateIDPattern.FindStringSubmatch(id); match != nil {
			maxSeq[match[1]] = maxInt(maxSeq[match[1]], atoiSafe(match[2]))
		}
	}
	mapping := map[string]string{}
	for _, action := range actions {
		epic := v7EpicFromTaskID(taskMapping[action.Task])
		if epic == "" {
			epic = directStandaloneTaskEpic
		}
		for {
			maxSeq[epic]++
			id := fmt.Sprintf("%s-G-%s", epic, padNumber(maxSeq[epic]))
			if used[id] {
				continue
			}
			used[id] = true
			mapping[action.Key] = id
			break
		}
	}
	return mapping
}

func directAuthoringTaskGateIDs(actions []directWaveHumanAction, gateMapping map[string]string, taskKey string) []string {
	var ids []string
	for _, action := range actions {
		if action.Task == taskKey {
			ids = append(ids, gateMapping[action.Key])
		}
	}
	return ids
}

func renderDirectAuthoringWaveBody(req directWaveAuthoringRequest, waveID string, taskMapping map[string]string, frontiers [][]string) string {
	tasks := map[string]directWaveAuthoringTask{}
	for _, task := range req.Tasks {
		tasks[task.Key] = task
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# %s · %s\n\n## Intended result\n\n%s\n", waveID, req.Title, strings.TrimSpace(req.Outcome))
	if shared := strings.TrimSpace(req.SharedContext); shared != "" {
		fmt.Fprintf(&b, "\n## Shared context\n\n%s\n", shared)
	}
	b.WriteString("\n## Members\n\n")
	for _, task := range req.Tasks {
		fmt.Fprintf(&b, "- [%s](../tasks/%s.md) — %s\n", taskMapping[task.Key], taskMapping[task.Key], task.Title)
	}
	b.WriteString("\n## Ordered work\n\n")
	for i, frontier := range frontiers {
		fmt.Fprintf(&b, "### Stage %d\n\n", i+1)
		for _, key := range frontier {
			task, ok := tasks[key]
			if !ok {
				continue
			}
			id := taskMapping[key]
			line := fmt.Sprintf("- [%s](../tasks/%s.md) — %s", id, id, task.Title)
			var deps []string
			for _, dep := range task.Dependencies {
				if depID := taskMapping[dep.Task]; depID != "" {
					deps = append(deps, depID)
				}
			}
			if len(deps) > 0 {
				line += " (after " + strings.Join(deps, ", ") + ")"
			}
			b.WriteString(line + "\n")
		}
	}
	b.WriteString("\n## Blockers and human actions\n\n")
	if len(req.HumanActions) == 0 {
		b.WriteString("- None.\n")
	}
	for _, action := range req.HumanActions {
		fmt.Fprintf(&b, "- Human action: %s must %s for [%s](../tasks/%s.md).\n", action.Owner, action.Action, taskMapping[action.Task], taskMapping[action.Task])
	}
	b.WriteString("\n## Closure criteria\n\n")
	b.WriteString("- Every member task reaches its reviewed terminal state and the wave outcome holds.\n")
	b.WriteString("\nMembers remain backlog/held and the wave stays inert until `tusker wave start` authorizes it.\n")
	return b.String()
}

type directWaveAuthoringReport struct {
	WaveID              string            `json:"waveId"`
	WavePath            string            `json:"wavePath"`
	TaskMapping         map[string]string `json:"taskMapping"`
	TaskPaths           map[string]string `json:"taskPaths"`
	GateMapping         map[string]string `json:"gateMapping,omitempty"`
	Frontiers           [][]string        `json:"frontiers"`
	Readiness           map[string]string `json:"readiness"`
	ExpectedConcurrency int               `json:"expectedConcurrency"`
	RequestFingerprint  string            `json:"requestFingerprint"`
	Warnings            []string          `json:"warnings,omitempty"`
}

func waveV7DirectAuthoringCmd(vaultPath string, args Args) error {
	file := strings.TrimSpace(args.String("file"))
	raw, err := v7AuthoringInputBytes(vaultPath, file)
	if err != nil {
		return err
	}
	var req directWaveAuthoringRequest
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	decoder.KnownFields(true)
	if err := decoder.Decode(&req); err != nil {
		return tuskerError(errorInvalidArg, "invalid wave authoring request: "+err.Error())
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return tuskerError(errorInvalidArg, "invalid wave authoring request: expected exactly one document")
	}
	if key := strings.TrimSpace(firstNonEmpty(args.String("request-key"), args.String("request_key"))); key != "" {
		req.RequestKey = key
	}
	normalizeDirectWaveAuthoringRequest(&req)
	validateRequest := func() ([]directAuthoringIssue, [][]string, error) {
		issues, frontiers := validateDirectWaveAuthoring(vaultPath, req)
		if len(issues) > 0 {
			parts := make([]string, len(issues))
			for i, issue := range issues {
				parts[i] = issue.Code + " " + issue.Message
			}
			return nil, nil, tuskerError(errorInvalidArg, "wave authoring request is invalid: "+strings.Join(parts, "; "))
		}
		return issues, frontiers, nil
	}
	if _, _, err := validateRequest(); err != nil {
		return err
	}
	actor, err := v7AgentDefaultActor(args, "wave create")
	if err != nil {
		return err
	}
	fingerprint := directWaveAuthoringFingerprint(req)
	materialLock, err := acquireV7MaterialEpochLock(vaultPath)
	if err != nil {
		return err
	}
	defer materialLock.Close()
	if err := ensureV7WorkNamespaces(vaultPath); err != nil {
		return err
	}
	idx, err := loadV7Index(vaultPath)
	if err != nil {
		return err
	}
	_, frontiers, err := validateRequest()
	if err != nil {
		return err
	}
	for id, wave := range idx.Waves {
		if stringField(wave.Data, "authoring_request_key") != req.RequestKey {
			continue
		}
		if stringField(wave.Data, "authoring_request_fingerprint") == fingerprint {
			taskMapping, gateMapping, err := directAuthoringReceiptReplay(idx, wave, req, fingerprint)
			if err != nil {
				return err
			}
			report := buildDirectAuthoringReport(vaultPath, id, req, taskMapping, gateMapping, frontiers, fingerprint)
			emitDirectAuthoringReport(report, args)
			return nil
		}
		return tuskerError(errorAlreadyExists, "wave authoring request_key "+req.RequestKey+" was already used with different content", withHint("choose a new request key or resend the identical request"))
	}
	taskMapping := directAuthoringTaskIDs(vaultPath, idx, req.Tasks)
	gateMapping := directAuthoringGateIDs(idx, req.HumanActions, taskMapping)
	waveID := nextV7WaveIDFromIndex(idx)
	now := time.Now().UTC().Format(time.RFC3339)
	writes := map[string]string{}
	for _, task := range req.Tasks {
		id := taskMapping[task.Key]
		path := filepath.Join(vaultPath, "work", "tasks", id+".md")
		deps := make([]string, 0, len(task.Dependencies))
		for _, dep := range task.Dependencies {
			deps = append(deps, taskMapping[dep.Task]+":"+fallback(strings.ToLower(strings.TrimSpace(dep.Kind)), "hard"))
		}
		data := map[string]any{
			"schema": "tusker.task/v7", "kind": "task", "id": id, "project": v7ProjectID(vaultPath),
			"title": strings.TrimSpace(task.Title), "status": "backlog", "readiness": "held",
			"proof_mode": "inline", "proof_status": "pending", "proof_required": []string{"focused_test"}, "evidence_budget": 0,
			"raw_artifacts_allowed": false, "next_owner": "agent", "next_source": "task", "next_ref": id,
			"next_action": "Execute the task contract and satisfy proof mode.",
			"work_level":  strings.ToLower(strings.TrimSpace(task.WorkLevel)),
			"wave":        waveID,
			"created_at":  now, "created_by": actor, "updated_at": now, "updated_by": actor,
		}
		if epic := strings.ToUpper(strings.TrimSpace(task.Epic)); epic != "" {
			data["epic"] = epic
		}
		if level := strings.ToLower(strings.TrimSpace(task.ReviewLevel)); level != "" {
			data["review_level"] = level
		}
		if reason := strings.TrimSpace(task.ReviewReason); reason != "" {
			data["review_reason"] = reason
		}
		if len(task.SpecRefs) > 0 {
			data["spec_refs"] = task.SpecRefs
		}
		if len(deps) > 0 {
			data["dependencies"] = deps
		}
		if owned := normalizeOwnedPaths(task.OwnedPaths); len(owned) > 0 {
			data["owned_paths"] = owned
		}
		if generated := normalizeOwnedPaths(task.GeneratedOutputs); len(generated) > 0 {
			data["generated_outputs"] = generated
		}
		if gates := directAuthoringTaskGateIDs(req.HumanActions, gateMapping, task.Key); len(gates) > 0 {
			data["gates"] = gates
		}
		if err := CaptureTaskAuthoringProvenanceFromEnvironment(data); err != nil {
			return tuskerError(errorInvalidField, id+": "+err.Error())
		}
		body := directWaveAuthoringTaskBody(task)
		data["contract_fingerprint"] = directWaveTaskContractFingerprint(data, body)
		data["state_rev"] = v7StateRev(data, body)
		content, err := serializeDocument(data, body, v7FrontmatterOrder["task"])
		if err != nil {
			return err
		}
		writes[path] = content
	}
	for _, action := range req.HumanActions {
		id := gateMapping[action.Key]
		taskID := taskMapping[action.Task]
		path := filepath.Join(vaultPath, "work", "gates", id+".md")
		title := v7GateDefaultTitle(action.Action, []string{taskID})
		data := map[string]any{
			"schema": "tusker.gate/v1", "kind": "gate", "id": id, "project": v7ProjectID(vaultPath),
			"title": title, "gate_kind": "manual_hold", "status": "open", "owner": strings.TrimSpace(action.Owner),
			"priority": "p2", "blocking": true, "blocks": []string{taskID},
			"why_agent_cannot": strings.TrimSpace(action.WhyAgentCannot),
			"action":           strings.TrimSpace(action.Action),
			"verification":     strings.TrimSpace(action.Verification),
			"created_at":       now, "created_by": actor, "updated_at": now, "updated_by": actor,
		}
		if len(action.Covers) > 0 {
			data["covers"] = action.Covers
		}
		body := v7GateBody(id, title, action.Action, action.Verification, []string{taskID}, "manual_hold", action.WhyAgentCannot, "")
		data["state_rev"] = v7StateRev(data, body)
		content, err := serializeDocument(data, body, v7FrontmatterOrder["gate"])
		if err != nil {
			return err
		}
		writes[path] = content
	}
	members := make([]string, 0, len(req.Tasks))
	for _, task := range req.Tasks {
		members = append(members, taskMapping[task.Key])
	}
	waveData := map[string]any{
		"schema": "tusker.wave/v7", "kind": "wave", "id": waveID, "project": v7ProjectID(vaultPath),
		"title": strings.TrimSpace(req.Title), "summary": strings.TrimSpace(req.Outcome),
		"status": "open", "authorization": "disarmed", "members": members,
		"integration_branch":            v7IntegrationBranchName(waveID),
		"authoring_request_key":         req.RequestKey,
		"authoring_request_fingerprint": fingerprint,
		"authoring_receipt": map[string]any{
			"schema":              directWaveAuthoringReceiptSchema,
			"request_key":         req.RequestKey,
			"request_fingerprint": fingerprint,
			"task_id_by_key":      copyStringStringMap(taskMapping),
			"gate_id_by_key":      copyStringStringMap(gateMapping),
		},
		"created_at": now, "created_by": actor, "updated_at": now, "updated_by": actor,
	}
	if base, err := waveAuthoringIntegrationBaseSHA(vaultPath); err != nil {
		return err
	} else if base != "" {
		// This is a snapshot of the configured default ref. Wave Start defers
		// creating the integration ref to the serialized landing path; until
		// then task worktrees and authorization material branch from the exact
		// frozen commit, not whatever the default happens to be later.
		waveData["integration_base_sha"] = base
	}
	if len(req.SpecRefs) > 0 {
		waveData["spec_refs"] = req.SpecRefs
	}
	if req.Concurrency > 0 {
		waveData["concurrency"] = req.Concurrency
	}
	waveBody := renderDirectAuthoringWaveBody(req, waveID, taskMapping, frontiers)
	waveData["state_rev"] = v7StateRev(waveData, waveBody)
	waveContent, err := serializeDocument(waveData, waveBody, v7FrontmatterOrder["wave"])
	if err != nil {
		return err
	}
	writes[filepath.Join(vaultPath, "work", "waves", waveID+".md")] = waveContent
	eventNow := time.Now().UTC()
	eventWrites := func(objectID, objectKind string, payload map[string]any) error {
		path, content, err := prepareV7Event(vaultPath, objectID, objectKind, "created", actor, payload, eventNow)
		if err != nil {
			return err
		}
		if err := ensureDir(filepath.Dir(path)); err != nil {
			return err
		}
		writes[path] = content
		return nil
	}
	for _, task := range req.Tasks {
		id := taskMapping[task.Key]
		if err := eventWrites(id, "task", map[string]any{"path": filepath.ToSlash(filepath.Join("work", "tasks", id+".md")), "wave": waveID}); err != nil {
			return err
		}
	}
	for _, action := range req.HumanActions {
		if err := eventWrites(gateMapping[action.Key], "gate", map[string]any{"blocks": []string{taskMapping[action.Task]}}); err != nil {
			return err
		}
	}
	if err := eventWrites(waveID, "wave", map[string]any{"members": members}); err != nil {
		return err
	}
	failAfter := 0
	if raw := strings.TrimSpace(args.String("fail-after-write-count")); raw != "" {
		failAfter = atoiSafe(raw)
		if failAfter < 1 {
			return tuskerError(errorInvalidArg, "fail-after-write-count must be a positive integer")
		}
	} else if args.Bool("fail-after-first-write") {
		failAfter = 1
	}
	if err := commitV7DocumentWrites(writes, failAfter); err != nil {
		return err
	}
	report := buildDirectAuthoringReport(vaultPath, waveID, req, taskMapping, gateMapping, frontiers, fingerprint)
	// Warn (never refuse) about contracts that `wave review --check` will
	// refuse at Start, so the author repairs them now instead of discovering
	// them at arming.
	if idx, err := loadV7Index(vaultPath); err == nil {
		for _, blocker := range directWaveArmContractBlockers(vaultPath, idx, idx.Waves[waveID]) {
			report.Warnings = append(report.Warnings, blocker.TaskID+" "+blocker.Code+" "+blocker.Reason)
		}
	}
	emitDirectAuthoringReport(report, args)
	return nil
}

// waveAuthoringIntegrationBaseSHA freezes the configured default-branch tip at
// authoring time so task worktrees and wave authorization material bind an
// exact commit while the integration ref stays deliberately uncreated. A vault
// that is not inside a Git repository has no base to freeze.
func waveAuthoringIntegrationBaseSHA(vaultPath string) (string, error) {
	repoRoot := v7RepoRoot(vaultPath)
	if !v7GitRepo(repoRoot) {
		return "", nil
	}
	base := v7DefaultBranch(vaultPath)
	sha, err := gitOutputTrim(repoRoot, "rev-parse", "refs/heads/"+base)
	if err != nil {
		return "", tuskerError(errorInvalidTransition, "wave create could not read configured default integration base "+base+"; commit the default branch before authoring")
	}
	return sha, nil
}

func copyStringStringMap(src map[string]string) map[string]string {
	out := make(map[string]string, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}

func directAuthoringReceiptDrift(waveID, detail string) error {
	return tuskerError(errorInvalidTransition, "AUTHORING_RECEIPT_DRIFT "+waveID+": "+detail, withHint("the immutable authoring receipt no longer matches the authored records; do not retry with the same request key"))
}

func directAuthoringReceiptReplay(idx v7Index, wave Note, req directWaveAuthoringRequest, fingerprint string) (map[string]string, map[string]string, error) {
	waveID := stringField(wave.Data, "id")
	raw, ok := wave.Data["authoring_receipt"]
	if !ok {
		return nil, nil, directAuthoringReceiptDrift(waveID, "authoring_receipt is missing")
	}
	encoded, err := yaml.Marshal(raw)
	if err != nil {
		return nil, nil, directAuthoringReceiptDrift(waveID, "authoring_receipt is corrupt")
	}
	var receipt map[string]any
	if err := yaml.Unmarshal(encoded, &receipt); err != nil || receipt == nil {
		return nil, nil, directAuthoringReceiptDrift(waveID, "authoring_receipt is corrupt")
	}
	if receipt["schema"] != directWaveAuthoringReceiptSchema || stringField(receipt, "request_key") != req.RequestKey || stringField(receipt, "request_fingerprint") != fingerprint {
		return nil, nil, directAuthoringReceiptDrift(waveID, "authoring_receipt identity does not match the request")
	}
	decodeMap := func(field string) (map[string]string, error) {
		out := map[string]string{}
		raw, present := receipt[field]
		if !present {
			return out, nil
		}
		encoded, err := yaml.Marshal(raw)
		if err != nil {
			return nil, directAuthoringReceiptDrift(waveID, "authoring_receipt "+field+" is corrupt")
		}
		if err := yaml.Unmarshal(encoded, &out); err != nil || out == nil {
			return nil, directAuthoringReceiptDrift(waveID, "authoring_receipt "+field+" is corrupt")
		}
		return out, nil
	}
	taskIDs, err := decodeMap("task_id_by_key")
	if err != nil {
		return nil, nil, err
	}
	gateIDs, err := decodeMap("gate_id_by_key")
	if err != nil {
		return nil, nil, err
	}
	if len(taskIDs) != len(req.Tasks) || len(gateIDs) != len(req.HumanActions) {
		return nil, nil, directAuthoringReceiptDrift(waveID, "authoring_receipt mapping cardinality does not match the request")
	}
	memberSet := map[string]bool{}
	for _, member := range normalizeList(wave.Data["members"]) {
		memberSet[member] = true
	}
	taskMapping := map[string]string{}
	for _, task := range req.Tasks {
		id, ok := taskIDs[task.Key]
		if !ok || id == "" {
			return nil, nil, directAuthoringReceiptDrift(waveID, "task key "+task.Key+" has no receipt target")
		}
		record, exists := idx.Tasks[id]
		if !exists {
			return nil, nil, directAuthoringReceiptDrift(waveID, "task key "+task.Key+" receipt target "+id+" is missing")
		}
		if stringField(record.Data, "wave") != waveID || !memberSet[id] {
			return nil, nil, directAuthoringReceiptDrift(waveID, "task key "+task.Key+" receipt target "+id+" is no longer a member of "+waveID)
		}
		taskMapping[task.Key] = id
	}
	gateMapping := map[string]string{}
	for _, action := range req.HumanActions {
		id, ok := gateIDs[action.Key]
		if !ok || id == "" {
			return nil, nil, directAuthoringReceiptDrift(waveID, "gate key "+action.Key+" has no receipt target")
		}
		record, exists := idx.Gates[id]
		if !exists {
			return nil, nil, directAuthoringReceiptDrift(waveID, "gate key "+action.Key+" receipt target "+id+" is missing")
		}
		if taskID := taskMapping[action.Task]; !containsString(normalizeList(record.Data["blocks"]), taskID) {
			return nil, nil, directAuthoringReceiptDrift(waveID, "gate key "+action.Key+" receipt target "+id+" no longer blocks "+taskID)
		}
		gateMapping[action.Key] = id
	}
	return taskMapping, gateMapping, nil
}

func buildDirectAuthoringReport(vaultPath, waveID string, req directWaveAuthoringRequest, taskMapping, gateMapping map[string]string, frontiers [][]string, fingerprint string) directWaveAuthoringReport {
	repoRoot := v7RepoRoot(vaultPath)
	relative := func(path string) string {
		rel, err := filepath.Rel(repoRoot, path)
		if err != nil {
			return filepath.ToSlash(path)
		}
		return filepath.ToSlash(rel)
	}
	taskPaths := map[string]string{}
	for key, id := range taskMapping {
		taskPaths[key] = relative(filepath.Join(vaultPath, "work", "tasks", id+".md"))
	}
	idFrontiers := make([][]string, 0, len(frontiers))
	readiness := map[string]string{}
	for i, frontier := range frontiers {
		var ids []string
		for _, key := range frontier {
			id := taskMapping[key]
			ids = append(ids, id)
			if i == 0 {
				readiness[key] = "frontier_ready"
			} else {
				readiness[key] = "dependency_waiting"
			}
		}
		sort.Strings(ids)
		idFrontiers = append(idFrontiers, ids)
	}
	return directWaveAuthoringReport{
		WaveID:              waveID,
		WavePath:            relative(filepath.Join(vaultPath, "work", "waves", waveID+".md")),
		TaskMapping:         taskMapping,
		TaskPaths:           taskPaths,
		GateMapping:         gateMapping,
		Frontiers:           idFrontiers,
		Readiness:           readiness,
		ExpectedConcurrency: directWaveExpectedConcurrency(req.Concurrency, frontiers),
		RequestFingerprint:  fingerprint,
	}
}

func emitDirectAuthoringReport(report directWaveAuthoringReport, args Args) {
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": true, "wave": report, "inert": true})
		return
	}
	if args.Bool("quiet") {
		return
	}
	frontiers := make([]string, 0, len(report.Frontiers))
	for _, frontier := range report.Frontiers {
		frontiers = append(frontiers, "{"+strings.Join(frontier, ", ")+"}")
	}
	fmt.Printf("Created %s with %d tasks; frontiers: %s; expected concurrency: %d. No work was dispatched.\n", report.WaveID, len(report.TaskMapping), strings.Join(frontiers, " -> "), report.ExpectedConcurrency)
	fmt.Printf("  Wave brief: %s\n", report.WavePath)
	keys := make([]string, 0, len(report.TaskPaths))
	for key := range report.TaskPaths {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		fmt.Printf("  Task %s: %s\n", key, report.TaskPaths[key])
	}
	for _, warning := range report.Warnings {
		fmt.Printf("warning: %s\n", warning)
	}
	if len(report.Warnings) > 0 {
		fmt.Printf("  `tusker wave review %s --check` will refuse Start until these contracts are fixed.\n", report.WaveID)
	}
}
