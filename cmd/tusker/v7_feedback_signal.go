package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"
)

const (
	feedbackSignalSchema             = "tusker.feedback_signal/v1"
	feedbackSignalMaxSummaryChars    = 240
	feedbackSignalMaxFactStringChars = 180
	feedbackSignalMaxFactKeys        = 16
	feedbackSignalMaxFactListItems   = 12
	feedbackSignalTokenBurnThreshold = 80000
)

var (
	feedbackSignalCategories = map[string]bool{
		"review_loop":        true,
		"acceptance_quality": true,
		"token_burn":         true,
		"cli_friction":       true,
		"closeout_churn":     true,
		"workflow_repeat":    true,
		"environment_setup":  true,
	}
	feedbackSignalSeverities  = map[string]bool{"p0": true, "p1": true, "p2": true, "p3": true}
	feedbackSignalConfidences = map[string]bool{"low": true, "medium": true, "high": true}
)

type feedbackSignal struct {
	Schema         string                     `json:"schema"`
	ID             string                     `json:"id"`
	Date           string                     `json:"date"`
	Project        string                     `json:"project"`
	TaskID         string                     `json:"task,omitempty"`
	AttemptID      string                     `json:"attempt,omitempty"`
	Source         string                     `json:"source"`
	Category       string                     `json:"category"`
	Severity       string                     `json:"severity"`
	Confidence     string                     `json:"confidence"`
	DedupeKey      string                     `json:"dedupe_key"`
	Summary        string                     `json:"summary"`
	ObservedFacts  map[string]any             `json:"observed_facts"`
	Occurrences    []feedbackSignalOccurrence `json:"occurrences,omitempty"`
	Recommendation string                     `json:"recommendation,omitempty"`
	RawPayload     map[string]any             `json:"raw_payload,omitempty"`
}

type feedbackSignalOccurrence struct {
	Vault      string `json:"vault,omitempty"`
	Project    string `json:"project,omitempty"`
	Source     string `json:"source,omitempty"`
	TaskID     string `json:"task,omitempty"`
	AttemptID  string `json:"attempt,omitempty"`
	SignalID   string `json:"signal,omitempty"`
	SourcePath string `json:"source_path,omitempty"`
}

type feedbackSignalCollapseResult struct {
	Signals    []feedbackSignal
	RawEmitted int
	Skipped    int
}

type feedbackSignalReducerInput struct {
	Date    string
	Project string
	Source  string
	Tasks   []feedbackSignalTaskInput
	Events  []feedbackSignalEventInput
}

type feedbackSignalTaskInput struct {
	Project              string
	ID                   string
	AttemptID            string
	Title                string
	Status               string
	Readiness            string
	NextOwner            string
	Source               string
	AcceptanceIDs        []string
	AcceptanceTotal      int
	AcceptanceSatisfied  int
	MissingAcceptance    []string
	ProofMapGaps         []string
	ProofEvidenceGaps    []string
	MissingProof         []string
	ReviewTransitions    int
	ReworkTransitions    int
	TokenTotal           int
	CompletionIterations int
}

type feedbackSignalEventInput struct {
	At        string
	Kind      string
	Project   string
	TaskID    string
	AttemptID string
	Source    string
	Payload   map[string]any
}

func completeFeedbackSignal(signal feedbackSignal) feedbackSignal {
	signal.Schema = firstNonEmpty(signal.Schema, feedbackSignalSchema)
	signal.Date = firstNonEmpty(strings.TrimSpace(signal.Date), todayISO())
	signal.Project = strings.ToUpper(strings.TrimSpace(signal.Project))
	signal.TaskID = strings.ToUpper(strings.TrimSpace(signal.TaskID))
	signal.AttemptID = strings.TrimSpace(signal.AttemptID)
	signal.Source = feedbackCleanValue(signal.Source)
	signal.Category = strings.ToLower(feedbackCleanValue(signal.Category))
	signal.Severity = normalizeFeedbackSignalSeverity(firstNonEmpty(signal.Severity, "P2"))
	signal.Confidence = strings.ToLower(feedbackCleanValue(firstNonEmpty(signal.Confidence, "medium")))
	signal.DedupeKey = normalizedFeedbackSignalDedupeKey(signal.DedupeKey)
	signal.Summary = feedbackCleanValue(signal.Summary)
	signal.Recommendation = feedbackCleanValue(signal.Recommendation)
	if signal.ObservedFacts == nil {
		signal.ObservedFacts = map[string]any{}
	}
	if signal.ID == "" {
		signal.ID = feedbackSignalID(signal)
	}
	return signal
}

func validateFeedbackSignal(signal feedbackSignal) []Issue {
	signal = completeFeedbackSignal(signal)
	path := feedbackSignalRelativePath(signal)
	var issues []Issue
	if signal.Schema != feedbackSignalSchema {
		issues = append(issues, issue("FEEDBACK_SIGNAL_SCHEMA_INVALID", "feedback signal schema must be "+feedbackSignalSchema, path, "", map[string]any{"schema": signal.Schema}))
	}
	if strings.TrimSpace(signal.ID) == "" {
		issues = append(issues, issue("FEEDBACK_SIGNAL_ID_MISSING", "feedback signal id is required", path, "", nil))
	}
	if _, err := time.Parse("2006-01-02", signal.Date); err != nil {
		issues = append(issues, issue("FEEDBACK_SIGNAL_DATE_INVALID", "feedback signal date must be YYYY-MM-DD: "+signal.Date, path, "", nil))
	}
	if signal.Project == "" {
		issues = append(issues, issue("FEEDBACK_SIGNAL_PROJECT_MISSING", "feedback signal project is required", path, "", nil))
	}
	if signal.Source == "" {
		issues = append(issues, issue("FEEDBACK_SIGNAL_SOURCE_MISSING", "feedback signal source is required", path, "", nil))
	}
	if !feedbackSignalCategories[signal.Category] {
		issues = append(issues, issue("FEEDBACK_SIGNAL_CATEGORY_INVALID", "feedback signal category is invalid: "+signal.Category, path, "use one of: "+strings.Join(feedbackSignalCategoryNames(), ", "), map[string]any{"category": signal.Category}))
	}
	if !feedbackSignalSeverities[strings.ToLower(signal.Severity)] {
		issues = append(issues, issue("FEEDBACK_SIGNAL_SEVERITY_INVALID", "feedback signal severity is invalid: "+signal.Severity, path, "use P0, P1, P2, or P3", map[string]any{"severity": signal.Severity}))
	}
	if !feedbackSignalConfidences[signal.Confidence] {
		issues = append(issues, issue("FEEDBACK_SIGNAL_CONFIDENCE_INVALID", "feedback signal confidence is invalid: "+signal.Confidence, path, "use low, medium, or high", map[string]any{"confidence": signal.Confidence}))
	}
	if signal.DedupeKey == "" {
		issues = append(issues, issue("FEEDBACK_SIGNAL_DEDUPE_KEY_MISSING", "feedback signal dedupe_key is required", path, "", nil))
	}
	if signal.Summary == "" {
		issues = append(issues, issue("FEEDBACK_SIGNAL_SUMMARY_MISSING", "feedback signal summary is required", path, "", nil))
	} else if len(signal.Summary) > feedbackSignalMaxSummaryChars || strings.Contains(signal.Summary, "\n") {
		issues = append(issues, issue("FEEDBACK_SIGNAL_SUMMARY_INVALID", fmt.Sprintf("feedback signal summary must be one short line under %d chars", feedbackSignalMaxSummaryChars), path, "", map[string]any{"chars": len(signal.Summary)}))
	}
	if signal.Recommendation != "" && (len(signal.Recommendation) > feedbackSignalMaxSummaryChars || strings.Contains(signal.Recommendation, "\n")) {
		issues = append(issues, issue("FEEDBACK_SIGNAL_RECOMMENDATION_INVALID", fmt.Sprintf("feedback signal recommendation must be one short line under %d chars", feedbackSignalMaxSummaryChars), path, "", map[string]any{"chars": len(signal.Recommendation)}))
	}
	if len(signal.RawPayload) > 0 {
		issues = append(issues, issue("FEEDBACK_SIGNAL_RAW_PAYLOAD_FORBIDDEN", "feedback signals must not store raw event payloads, transcripts, command logs, attachments, or copied source", path, "summarize raw input into observed_facts", map[string]any{"keys": sortedAnyMapKeys(signal.RawPayload)}))
	}
	issues = append(issues, validateFeedbackSignalFacts(path, signal.ObservedFacts)...)
	return issues
}

func validateFeedbackSignalFacts(path string, facts map[string]any) []Issue {
	if len(facts) == 0 {
		return []Issue{issue("FEEDBACK_SIGNAL_FACTS_MISSING", "feedback signal observed_facts are required", path, "", nil)}
	}
	var issues []Issue
	if len(facts) > feedbackSignalMaxFactKeys {
		issues = append(issues, issue("FEEDBACK_SIGNAL_FACTS_TOO_MANY", fmt.Sprintf("feedback signal observed_facts has %d keys; max is %d", len(facts), feedbackSignalMaxFactKeys), path, "", nil))
	}
	for _, key := range sortedAnyMapKeys(facts) {
		value := facts[key]
		cleanKey := strings.TrimSpace(key)
		if cleanKey == "" || feedbackSignalForbiddenFactKey(cleanKey) {
			issues = append(issues, issue("FEEDBACK_SIGNAL_FACT_KEY_INVALID", "feedback signal observed_facts key is not summary-safe: "+key, path, "use bounded counts, labels, paths, task IDs, and short reason excerpts", map[string]any{"key": key}))
			continue
		}
		if detail := feedbackSignalUnsafeFactValue(value); detail != "" {
			issues = append(issues, issue("FEEDBACK_SIGNAL_FACT_VALUE_INVALID", "feedback signal observed_facts."+key+" is not summary-safe: "+detail, path, "do not store raw transcripts, full logs, attachments, diffs, or copied source", map[string]any{"key": key}))
		}
	}
	return issues
}

func writeFeedbackSignal(vaultPath string, signal feedbackSignal) (string, error) {
	signal = completeFeedbackSignal(signal)
	if issues := validateFeedbackSignal(signal); len(issues) > 0 {
		first := issues[0]
		return "", tuskerError(errorInvalidField, first.Message, withHint(first.Hint), withPath(first.Path), withContext(map[string]any{"issues": issues}))
	}
	path := feedbackSignalPath(vaultPath, signal)
	raw, err := json.MarshalIndent(signal, "", "  ")
	if err != nil {
		return "", err
	}
	if err := writeText(path, string(raw)+"\n"); err != nil {
		return "", err
	}
	return path, nil
}

func feedbackSignalPath(vaultPath string, signal feedbackSignal) string {
	signal = completeFeedbackSignal(signal)
	hash := feedbackSignalHash(signal.DedupeKey)[:10]
	base := feedbackSlug(signal.Category+"-"+signal.DedupeKey, "signal")
	return filepath.Join(vaultPath, "feedback", "signals", signal.Date, base+"-"+hash+".json")
}

func feedbackSignalRelativePath(signal feedbackSignal) string {
	signal = completeFeedbackSignal(signal)
	return filepath.ToSlash(filepath.Join("feedback", "signals", signal.Date, signal.ID+".json"))
}

func deriveFeedbackSignals(input feedbackSignalReducerInput) []feedbackSignal {
	result := collapseFeedbackSignals(deriveFeedbackSignalEmissions(input))
	sortFeedbackSignals(result.Signals)
	return result.Signals
}

func deriveFeedbackSignalEmissions(input feedbackSignalReducerInput) []feedbackSignal {
	date := firstNonEmpty(strings.TrimSpace(input.Date), todayISO())
	project := strings.ToUpper(strings.TrimSpace(input.Project))
	source := feedbackCleanValue(firstNonEmpty(input.Source, "reducer"))
	var signals []feedbackSignal
	taskReviewRollups := map[string]bool{}

	for _, task := range input.Tasks {
		if task.Project == "" {
			task.Project = project
		}
		if task.Source == "" {
			task.Source = source
		}
		if sig, ok := feedbackSignalFromAcceptanceTask(date, task); ok {
			signals = appendFeedbackSignalEmission(signals, sig)
		}
		if sig, ok := feedbackSignalFromReviewTask(date, task); ok {
			signals = appendFeedbackSignalEmission(signals, sig)
			taskReviewRollups[sig.TaskID] = true
		}
		if sig, ok := feedbackSignalFromTaskTokenBurn(date, task); ok {
			signals = appendFeedbackSignalEmission(signals, sig)
		}
	}

	for _, sig := range feedbackSignalsFromReviewEvents(date, project, source, input.Events) {
		if taskReviewRollups[sig.TaskID] {
			continue
		}
		signals = appendFeedbackSignalEmission(signals, sig)
	}
	for _, event := range input.Events {
		if event.Project == "" {
			event.Project = project
		}
		if event.Source == "" {
			event.Source = source
		}
		if sig, ok := feedbackSignalFromTokenEvent(date, event); ok {
			signals = appendFeedbackSignalEmission(signals, sig)
		}
	}

	return signals
}

func sortFeedbackSignals(signals []feedbackSignal) {
	sort.SliceStable(signals, func(i, j int) bool {
		if signals[i].Category != signals[j].Category {
			return signals[i].Category < signals[j].Category
		}
		return signals[i].DedupeKey < signals[j].DedupeKey
	})
}

func feedbackSignalTaskInputFromNote(note Note) feedbackSignalTaskInput {
	taskID := stringField(note.Data, "id")
	project := ""
	if idx := strings.Index(taskID, "-T-"); idx > 0 {
		project = taskID[:idx]
	}
	acceptanceFacts := feedbackSignalAcceptanceFacts(note.Body)
	acceptanceTotal := len(acceptanceFacts.AcceptanceIDs)
	proofEvidenceGaps := normalizeList(note.Data["proof_missing"])
	return feedbackSignalTaskInput{
		Project:             project,
		ID:                  taskID,
		Title:               firstNonEmpty(stringField(note.Data, "title"), firstMarkdownHeading(note.Body)),
		Status:              stringField(note.Data, "status"),
		Readiness:           stringField(note.Data, "readiness"),
		NextOwner:           stringField(note.Data, "next_owner"),
		Source:              firstNonEmpty(note.RelativePath, "task_note"),
		AcceptanceIDs:       acceptanceFacts.AcceptanceIDs,
		AcceptanceTotal:     acceptanceTotal,
		AcceptanceSatisfied: maxInt(0, acceptanceTotal-len(acceptanceFacts.AcceptanceGaps)),
		MissingAcceptance:   acceptanceFacts.AcceptanceGaps,
		ProofMapGaps:        acceptanceFacts.ProofMapGaps,
		ProofEvidenceGaps:   proofEvidenceGaps,
		MissingProof:        proofEvidenceGaps,
	}
}

func feedbackSignalFromAcceptanceTask(date string, task feedbackSignalTaskInput) (feedbackSignal, bool) {
	task.ID = strings.ToUpper(strings.TrimSpace(task.ID))
	if task.ID == "" {
		return feedbackSignal{}, false
	}
	task.AcceptanceIDs = feedbackSignalShortList(task.AcceptanceIDs)
	task.MissingAcceptance = feedbackSignalShortList(task.MissingAcceptance)
	task.ProofMapGaps = feedbackSignalShortList(task.ProofMapGaps)
	task.ProofEvidenceGaps = feedbackSignalShortList(append(task.ProofEvidenceGaps, task.MissingProof...))
	total := maxInt(task.AcceptanceTotal, task.AcceptanceSatisfied+len(task.MissingAcceptance))
	gapCount := len(task.MissingAcceptance) + len(task.ProofMapGaps) + len(task.ProofEvidenceGaps)
	missingCount := maxInt(maxInt(0, total-task.AcceptanceSatisfied), gapCount)
	source := strings.ToLower(strings.TrimSpace(task.Source))
	if source == "events" && total == 0 && gapCount == 0 && len(task.AcceptanceIDs) == 0 {
		return feedbackSignal{}, false
	}
	labels := feedbackContractLabels(task)
	if total == 0 && len(labels) == 0 {
		return feedbackSignal{}, false
	}
	status := strings.ToLower(strings.TrimSpace(task.Status))
	if missingCount == 0 && len(labels) == 0 {
		return feedbackSignal{}, false
	}
	if len(labels) == 0 && !feedbackSignalTaskIsQualityRelevant(status, task.Readiness, task.AttemptID) {
		return feedbackSignal{}, false
	}
	severity := "P2"
	if missingCount >= 4 || status == "done" || containsString(labels, "thin") || containsString(labels, "unverifiable") || containsString(labels, "missing-proof-map") || containsString(labels, "done-with-gaps") {
		severity = "P1"
	}
	confidence := "medium"
	if len(task.MissingAcceptance) > 0 || len(task.ProofMapGaps) > 0 || len(task.ProofEvidenceGaps) > 0 {
		confidence = "high"
	}
	project := strings.ToUpper(strings.TrimSpace(task.Project))
	signal := completeFeedbackSignal(feedbackSignal{
		Date:       date,
		Project:    project,
		TaskID:     task.ID,
		AttemptID:  task.AttemptID,
		Source:     firstNonEmpty(task.Source, "task"),
		Category:   "acceptance_quality",
		Severity:   severity,
		Confidence: confidence,
		DedupeKey:  feedbackSignalDedupeKey(project, "acceptance_quality", task.ID),
		Summary:    fmt.Sprintf("%s has %d unresolved acceptance or proof gap%s while %s.", task.ID, missingCount, plural(missingCount), firstNonEmpty(status, "in progress")),
		ObservedFacts: map[string]any{
			"task":                 task.ID,
			"status":               firstNonEmpty(status, "unknown"),
			"reason_labels":        labels,
			"acceptance_total":     total,
			"acceptance_satisfied": task.AcceptanceSatisfied,
			"acceptance_ids":       task.AcceptanceIDs,
			"acceptance_gaps":      task.MissingAcceptance,
			"proof_map_gaps":       task.ProofMapGaps,
			"proof_evidence_gaps":  task.ProofEvidenceGaps,
		},
		Recommendation: "Tighten acceptance proof before requesting another review.",
	})
	return signal, true
}

func feedbackSignalFromReviewTask(date string, task feedbackSignalTaskInput) (feedbackSignal, bool) {
	task.ID = strings.ToUpper(strings.TrimSpace(task.ID))
	if task.ID == "" {
		return feedbackSignal{}, false
	}
	transitions := task.ReviewTransitions + task.ReworkTransitions
	if transitions < 3 {
		return feedbackSignal{}, false
	}
	severity := "medium"
	if transitions >= 5 {
		severity = "high"
	}
	confidence := "medium"
	if task.ReviewTransitions > 0 && task.ReworkTransitions > 0 {
		confidence = "high"
	}
	project := strings.ToUpper(strings.TrimSpace(task.Project))
	return completeFeedbackSignal(feedbackSignal{
		Date:       date,
		Project:    project,
		TaskID:     task.ID,
		AttemptID:  task.AttemptID,
		Source:     firstNonEmpty(task.Source, "task"),
		Category:   "review_loop",
		Severity:   severity,
		Confidence: confidence,
		DedupeKey:  feedbackSignalDedupeKey(project, "review_loop", task.ID),
		Summary:    fmt.Sprintf("%s bounced through review or rework %d time%s.", task.ID, transitions, plural(transitions)),
		ObservedFacts: map[string]any{
			"task":               task.ID,
			"status":             firstNonEmpty(strings.ToLower(task.Status), "unknown"),
			"review_transitions": task.ReviewTransitions,
			"rework_transitions": task.ReworkTransitions,
		},
		Recommendation: "Review the handoff and rework reasons before another review transition.",
	}), true
}

func feedbackSignalFromTaskTokenBurn(date string, task feedbackSignalTaskInput) (feedbackSignal, bool) {
	if task.TokenTotal < feedbackSignalTokenBurnThreshold {
		return feedbackSignal{}, false
	}
	project := strings.ToUpper(strings.TrimSpace(task.Project))
	taskID := strings.ToUpper(strings.TrimSpace(task.ID))
	source := firstNonEmpty(task.Source, "task")
	return feedbackSignalTokenBurnSignal(date, project, taskID, task.AttemptID, source, task.TokenTotal, "task")
}

func feedbackSignalsFromReviewEvents(date, project, source string, events []feedbackSignalEventInput) []feedbackSignal {
	type rollup struct {
		project string
		taskID  string
		source  string
		review  int
		rework  int
	}
	rollups := map[string]*rollup{}
	for _, event := range events {
		taskID := strings.ToUpper(strings.TrimSpace(event.TaskID))
		if taskID == "" {
			taskID = strings.ToUpper(feedbackCleanValue(firstNonEmpty(toString(event.Payload["task"]), toString(event.Payload["task_id"]))))
		}
		if taskID == "" {
			continue
		}
		eventProject := strings.ToUpper(strings.TrimSpace(firstNonEmpty(event.Project, project)))
		eventSource := firstNonEmpty(event.Source, source, "events")
		key := feedbackSignalDedupeKey(eventProject, taskID, eventSource)
		item := rollups[key]
		if item == nil {
			item = &rollup{project: eventProject, taskID: taskID, source: eventSource}
			rollups[key] = item
		}
		switch feedbackSignalReviewEventClass(event) {
		case "review":
			item.review++
		case "rework":
			item.rework++
		}
	}
	var signals []feedbackSignal
	for _, item := range rollups {
		task := feedbackSignalTaskInput{
			Project:           item.project,
			ID:                item.taskID,
			Source:            item.source,
			ReviewTransitions: item.review,
			ReworkTransitions: item.rework,
		}
		if sig, ok := feedbackSignalFromReviewTask(date, task); ok {
			signals = append(signals, sig)
		}
	}
	return signals
}

func feedbackSignalFromTokenEvent(date string, event feedbackSignalEventInput) (feedbackSignal, bool) {
	total := feedbackSignalTokenTotal(event.Payload)
	if total < feedbackSignalTokenBurnThreshold {
		return feedbackSignal{}, false
	}
	project := strings.ToUpper(strings.TrimSpace(event.Project))
	taskID := strings.ToUpper(strings.TrimSpace(firstNonEmpty(event.TaskID, toString(event.Payload["task"]), toString(event.Payload["task_id"]))))
	attemptID := firstNonEmpty(event.AttemptID, toString(event.Payload["attempt"]), toString(event.Payload["attempt_id"]))
	source := firstNonEmpty(event.Source, "events")
	return feedbackSignalTokenBurnSignal(date, project, taskID, attemptID, source, total, firstNonEmpty(event.Kind, "token_usage"))
}

func feedbackSignalTokenBurnSignal(date, project, taskID, attemptID, source string, total int, sourceKind string) (feedbackSignal, bool) {
	severity := "medium"
	if total >= 200000 {
		severity = "high"
	}
	if total >= 500000 {
		severity = "critical"
	}
	dedupeParts := []string{project, "token_burn", taskID, attemptID, source, sourceKind}
	if taskID == "" && attemptID == "" {
		dedupeParts = append(dedupeParts, fmt.Sprintf("%d", total/10000))
	}
	label := firstNonEmpty(attemptID, taskID, sourceKind)
	if label == "" {
		label = "workflow"
	}
	return completeFeedbackSignal(feedbackSignal{
		Date:       date,
		Project:    project,
		TaskID:     taskID,
		AttemptID:  attemptID,
		Source:     source,
		Category:   "token_burn",
		Severity:   severity,
		Confidence: "high",
		DedupeKey:  feedbackSignalDedupeKey(dedupeParts...),
		Summary:    fmt.Sprintf("%s used %d tokens.", label, total),
		ObservedFacts: map[string]any{
			"task":         taskID,
			"attempt":      attemptID,
			"source_kind":  sourceKind,
			"total_tokens": total,
		},
		Recommendation: "Inspect repeated context reads and large tool outputs before continuing the workflow.",
	}), true
}

func appendFeedbackSignal(signals []feedbackSignal, seen map[string]bool, signal feedbackSignal) []feedbackSignal {
	signal = completeFeedbackSignal(signal)
	if seen[signal.DedupeKey] {
		return signals
	}
	if len(validateFeedbackSignal(signal)) > 0 {
		return signals
	}
	seen[signal.DedupeKey] = true
	return append(signals, signal)
}

func appendFeedbackSignalEmission(signals []feedbackSignal, signal feedbackSignal) []feedbackSignal {
	signal = completeFeedbackSignal(signal)
	if len(validateFeedbackSignal(signal)) > 0 {
		return signals
	}
	return append(signals, signal)
}

func collapseFeedbackSignals(signals []feedbackSignal) feedbackSignalCollapseResult {
	result := feedbackSignalCollapseResult{RawEmitted: len(signals)}
	byKey := map[string]feedbackSignal{}
	for _, signal := range signals {
		signal = completeFeedbackSignal(signal)
		if len(validateFeedbackSignal(signal)) > 0 {
			result.Skipped++
			continue
		}
		key := feedbackSignalCollapseKey(signal)
		if key == "" {
			result.Skipped++
			continue
		}
		signal.DedupeKey = key
		signal.Occurrences = feedbackSignalOccurrences(signal)
		current, ok := byKey[key]
		if !ok {
			byKey[key] = signal
			continue
		}
		byKey[key] = mergeFeedbackSignals(current, signal)
	}
	for _, signal := range byKey {
		signal.ObservedFacts = mergeFeedbackSignalFacts(signal.ObservedFacts, map[string]any{
			"occurrence_count": len(feedbackSignalOccurrences(signal)),
		})
		result.Signals = append(result.Signals, signal)
	}
	sortFeedbackSignals(result.Signals)
	return result
}

func feedbackSignalCollapseKey(signal feedbackSignal) string {
	project := strings.ToUpper(strings.TrimSpace(signal.Project))
	taskID := strings.ToUpper(strings.TrimSpace(signal.TaskID))
	attemptID := strings.TrimSpace(signal.AttemptID)
	category := strings.ToLower(strings.TrimSpace(signal.Category))
	if project != "" && taskID != "" && (category == "acceptance_quality" || category == "review_loop") {
		return feedbackSignalDedupeKey(project, category, taskID)
	}
	if project != "" && category == "token_burn" && (taskID != "" || attemptID != "") {
		return feedbackSignalDedupeKey(project, category, taskID, attemptID)
	}
	return signal.DedupeKey
}

func feedbackSignalTaskKey(project, taskID string) string {
	return feedbackSignalDedupeKey(project, taskID)
}

func mergeFeedbackSignals(left, right feedbackSignal) feedbackSignal {
	primary, secondary := left, right
	if feedbackSignalBetterPrimary(right, left) {
		primary, secondary = right, left
	}
	primary.Occurrences = mergeFeedbackSignalOccurrences(feedbackSignalOccurrences(primary), feedbackSignalOccurrences(secondary))
	primary.ObservedFacts = mergeFeedbackSignalFacts(primary.ObservedFacts, secondary.ObservedFacts)
	primary.Project = firstNonEmpty(primary.Project, secondary.Project)
	primary.TaskID = firstNonEmpty(primary.TaskID, secondary.TaskID)
	primary.AttemptID = firstNonEmpty(primary.AttemptID, secondary.AttemptID)
	primary.Source = firstNonEmpty(primary.Source, secondary.Source)
	primary.Summary = firstNonEmpty(primary.Summary, secondary.Summary)
	primary.Recommendation = firstNonEmpty(primary.Recommendation, secondary.Recommendation)
	return completeFeedbackSignal(primary)
}

func feedbackSignalBetterPrimary(left, right feedbackSignal) bool {
	if feedbackReviewSeverityRank(left.Severity) != feedbackReviewSeverityRank(right.Severity) {
		return feedbackReviewSeverityRank(left.Severity) > feedbackReviewSeverityRank(right.Severity)
	}
	if feedbackReviewConfidenceRank(left.Confidence) != feedbackReviewConfidenceRank(right.Confidence) {
		return feedbackReviewConfidenceRank(left.Confidence) > feedbackReviewConfidenceRank(right.Confidence)
	}
	if left.Date != right.Date {
		return left.Date > right.Date
	}
	return left.ID < right.ID
}

func mergeFeedbackSignalFacts(left, right map[string]any) map[string]any {
	out := map[string]any{}
	for key, value := range left {
		out[key] = value
	}
	for key, value := range right {
		if existing, ok := out[key]; ok {
			out[key] = mergeFeedbackSignalFactValue(existing, value)
			continue
		}
		out[key] = value
	}
	return out
}

func mergeFeedbackSignalFactValue(left, right any) any {
	leftList, leftIsList := feedbackSignalFactList(left)
	rightList, rightIsList := feedbackSignalFactList(right)
	if leftIsList || rightIsList {
		return feedbackSignalShortList(append(leftList, rightList...))
	}
	leftInt := intValue(left)
	rightInt := intValue(right)
	if leftInt > 0 || rightInt > 0 {
		return maxInt(leftInt, rightInt)
	}
	if strings.TrimSpace(toString(left)) == "" {
		return right
	}
	return left
}

func feedbackSignalFactList(value any) ([]string, bool) {
	switch typed := value.(type) {
	case []string:
		return typed, true
	case []any:
		var out []string
		for _, item := range typed {
			if text := feedbackCleanValue(toString(item)); text != "" {
				out = append(out, text)
			}
		}
		return out, true
	default:
		return nil, false
	}
}

func feedbackSignalOccurrences(signal feedbackSignal) []feedbackSignalOccurrence {
	if len(signal.Occurrences) > 0 {
		return mergeFeedbackSignalOccurrences(signal.Occurrences, nil)
	}
	occurrence := feedbackSignalOccurrence{
		Project:   signal.Project,
		Source:    signal.Source,
		TaskID:    signal.TaskID,
		AttemptID: signal.AttemptID,
		SignalID:  signal.ID,
	}
	return mergeFeedbackSignalOccurrences([]feedbackSignalOccurrence{occurrence}, nil)
}

func withFeedbackSignalOccurrence(signal feedbackSignal, vaultPath string) feedbackSignal {
	signal = completeFeedbackSignal(signal)
	occurrence := feedbackSignalOccurrence{
		Vault:     filepath.ToSlash(vaultPath),
		Project:   signal.Project,
		Source:    signal.Source,
		TaskID:    signal.TaskID,
		AttemptID: signal.AttemptID,
		SignalID:  signal.ID,
	}
	signal.Occurrences = mergeFeedbackSignalOccurrences(signal.Occurrences, []feedbackSignalOccurrence{occurrence})
	return signal
}

func mergeFeedbackSignalOccurrences(left, right []feedbackSignalOccurrence) []feedbackSignalOccurrence {
	seen := map[string]bool{}
	var out []feedbackSignalOccurrence
	for _, occurrence := range append(left, right...) {
		occurrence.Vault = filepath.ToSlash(strings.TrimSpace(occurrence.Vault))
		occurrence.Project = strings.ToUpper(strings.TrimSpace(occurrence.Project))
		occurrence.Source = feedbackCleanValue(occurrence.Source)
		occurrence.TaskID = strings.ToUpper(strings.TrimSpace(occurrence.TaskID))
		occurrence.AttemptID = strings.TrimSpace(occurrence.AttemptID)
		occurrence.SignalID = strings.TrimSpace(occurrence.SignalID)
		occurrence.SourcePath = filepath.ToSlash(strings.TrimSpace(occurrence.SourcePath))
		key := strings.Join([]string{occurrence.Vault, occurrence.Project, occurrence.Source, occurrence.TaskID, occurrence.AttemptID, occurrence.SignalID, occurrence.SourcePath}, "|")
		if key == "||||||" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, occurrence)
	}
	sort.SliceStable(out, func(i, j int) bool {
		leftKey := strings.Join([]string{out[i].Vault, out[i].Project, out[i].Source, out[i].TaskID, out[i].AttemptID, out[i].SignalID, out[i].SourcePath}, "|")
		rightKey := strings.Join([]string{out[j].Vault, out[j].Project, out[j].Source, out[j].TaskID, out[j].AttemptID, out[j].SignalID, out[j].SourcePath}, "|")
		return leftKey < rightKey
	})
	return out
}

func feedbackSignalTokenTotal(payload map[string]any) int {
	if len(payload) == 0 {
		return 0
	}
	for _, key := range []string{"total_tokens", "tokens", "total"} {
		if total := intValue(payload[key]); total > 0 {
			return total
		}
	}
	for _, key := range []string{"total_token_usage", "usage", "token_usage"} {
		if nested, ok := payload[key].(map[string]any); ok {
			if total := feedbackSignalTokenTotal(nested); total > 0 {
				return total
			}
		}
	}
	input := intValue(firstExisting(payload, "input_tokens", "prompt_tokens"))
	output := intValue(firstExisting(payload, "output_tokens", "completion_tokens"))
	return input + output
}

func feedbackSignalReviewEventClass(event feedbackSignalEventInput) string {
	text := strings.ToLower(strings.Join([]string{
		event.Kind,
		toString(event.Payload["status"]),
		toString(event.Payload["to_status"]),
		toString(event.Payload["to"]),
		toString(event.Payload["from"]),
		toString(event.Payload["readiness"]),
		toString(event.Payload["action"]),
		toString(event.Payload["reason"]),
	}, " "))
	switch {
	case strings.Contains(text, "rework"):
		return "rework"
	case strings.Contains(text, "review"):
		return "review"
	default:
		return ""
	}
}

func feedbackSignalTaskIsQualityRelevant(status, readiness, attemptID string) bool {
	status = strings.ToLower(strings.TrimSpace(status))
	readiness = strings.ToLower(strings.TrimSpace(readiness))
	return status == "review" ||
		status == "rework" ||
		status == "done" ||
		readiness == "waiting_on_review" ||
		readiness == "done" ||
		strings.TrimSpace(attemptID) != ""
}

type feedbackSignalAcceptanceFactSet struct {
	AcceptanceIDs  []string
	AcceptanceGaps []string
	ProofMapGaps   []string
}

func feedbackSignalAcceptanceGaps(body string) []string {
	facts := feedbackSignalAcceptanceFacts(body)
	return uniqueStringsPreserveOrder(append(facts.AcceptanceGaps, facts.ProofMapGaps...))
}

func feedbackSignalAcceptanceFacts(body string) feedbackSignalAcceptanceFactSet {
	acceptanceIDs := v7AcceptanceIDs(body)
	acceptance := strings.ToLower(sectionPreview(body, "## Acceptance", 2400))
	facts := feedbackSignalAcceptanceFactSet{AcceptanceIDs: feedbackSignalShortList(acceptanceIDs)}
	if strings.TrimSpace(acceptance) == "" {
		facts.AcceptanceGaps = []string{"acceptance-section"}
		return facts
	}
	if strings.Contains(acceptance, "complete the task contract") ||
		strings.Contains(acceptance, "define the accepted outcome") ||
		strings.Contains(acceptance, " tbd") ||
		strings.Contains(acceptance, "| tbd |") {
		facts.AcceptanceGaps = append(facts.AcceptanceGaps, acceptanceIDs...)
		if len(facts.AcceptanceGaps) == 0 {
			facts.AcceptanceGaps = append(facts.AcceptanceGaps, "acceptance-placeholder")
		}
	}
	if strings.Contains(acceptance, "inline verification, evidence, gate, or waiver") ||
		!strings.Contains(acceptance, "proof") {
		facts.ProofMapGaps = append(facts.ProofMapGaps, acceptanceIDs...)
		if len(facts.ProofMapGaps) == 0 {
			facts.ProofMapGaps = append(facts.ProofMapGaps, "proof-map")
		}
	}
	facts.AcceptanceGaps = feedbackSignalShortList(uniqueStringsPreserveOrder(facts.AcceptanceGaps))
	facts.ProofMapGaps = feedbackSignalShortList(uniqueStringsPreserveOrder(facts.ProofMapGaps))
	return facts
}

func feedbackSignalDedupeKey(parts ...string) string {
	var clean []string
	for _, part := range parts {
		normalized := normalizedFeedbackSignalDedupeKey(part)
		if normalized != "" {
			clean = append(clean, normalized)
		}
	}
	return strings.Join(clean, ":")
}

func normalizedFeedbackSignalDedupeKey(value string) string {
	value = strings.ToLower(feedbackCleanValue(value))
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

func normalizeFeedbackSignalSeverity(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "P2"
	}
	upper := strings.ToUpper(value)
	if strings.HasPrefix(upper, "P") && len(upper) >= 2 {
		switch upper[:2] {
		case "P0", "P1", "P2", "P3":
			return upper[:2]
		}
	}
	switch strings.ToLower(value) {
	case "critical", "blocker":
		return "P0"
	case "high", "major":
		return "P1"
	case "medium", "moderate":
		return "P2"
	case "low", "minor", "info", "informational":
		return "P3"
	default:
		return strings.ToLower(feedbackCleanValue(value))
	}
}

func feedbackSignalID(signal feedbackSignal) string {
	signal.Schema = firstNonEmpty(signal.Schema, feedbackSignalSchema)
	base := strings.Join([]string{signal.Schema, signal.Date, signal.Project, signal.Category, signal.DedupeKey}, "|")
	return "FS-" + signal.Date + "-" + feedbackSignalHash(base)[:12]
}

func feedbackSignalHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func feedbackSignalUnsafeFactValue(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return feedbackSignalUnsafeFactString(v)
	case bool, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64, json.Number:
		return ""
	case []string:
		if len(v) > feedbackSignalMaxFactListItems {
			return "list is too long"
		}
		for _, item := range v {
			if detail := feedbackSignalUnsafeFactString(item); detail != "" {
				return detail
			}
		}
		return ""
	case []any:
		if len(v) > feedbackSignalMaxFactListItems {
			return "list is too long"
		}
		for _, item := range v {
			switch item.(type) {
			case string, bool, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64, json.Number:
				if detail := feedbackSignalUnsafeFactValue(item); detail != "" {
					return detail
				}
			default:
				return "list contains nested or raw values"
			}
		}
		return ""
	default:
		return "value must be a bounded primitive or primitive list"
	}
}

func feedbackSignalUnsafeFactString(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > feedbackSignalMaxFactStringChars {
		return fmt.Sprintf("string is %d chars; max is %d", len(value), feedbackSignalMaxFactStringChars)
	}
	if strings.Count(value, "\n") > 1 {
		return "string has multiple lines"
	}
	lower := strings.ToLower(value)
	for _, marker := range []string{"function_call_output", "original token count", "diff --git", "```", "stack trace:", "transcript:", "raw log", "begin patch"} {
		if strings.Contains(lower, marker) {
			return "string looks like raw output"
		}
	}
	return ""
}

func feedbackSignalForbiddenFactKey(key string) bool {
	normalized := normalizedFeedbackSignalDedupeKey(key)
	for _, marker := range []string{"raw", "payload", "transcript", "attachment", "diff", "patch", "full-log", "command-log", "source-code", "file-content"} {
		if normalized == marker || strings.Contains(normalized, marker) {
			return true
		}
	}
	return false
}

func feedbackSignalShortList(values []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, value := range values {
		value = feedbackCleanValue(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, feedbackShort(value, feedbackSignalMaxFactStringChars))
		if len(out) >= feedbackSignalMaxFactListItems {
			break
		}
	}
	return out
}

func feedbackSignalCategoryNames() []string {
	keys := make([]string, 0, len(feedbackSignalCategories))
	for key := range feedbackSignalCategories {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sortedAnyMapKeys(values map[string]any) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func firstExisting(values map[string]any, keys ...string) any {
	for _, key := range keys {
		if value, ok := values[key]; ok {
			return value
		}
	}
	return nil
}

func feedbackSignalHelpText() string {
	return strings.Join([]string{
		"Events are history: timestamped facts about what happened.",
		"Feedback notes are subjective input: concise agent or human observations about product friction.",
		"Signals are derived product facts: reducer-created records stored under .tusker/feedback/signals/YYYY-MM-DD/*.json.",
		"Signals must summarize evidence into bounded counts, labels, paths, task IDs, and short reason excerpts instead of raw transcripts, logs, attachments, diffs, or copied source.",
	}, "\n")
}
