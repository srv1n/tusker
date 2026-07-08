package main

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type feedbackSignalDerivation struct {
	Signals              []feedbackSignal
	SourceCounts         map[string]int
	RawEmitted           int
	Skipped              int
	NoExplicitNoteVaults []string
}

func feedbackSignalsCmd(args Args) error {
	targets, err := feedbackDigestTargets(args)
	if err != nil {
		return err
	}
	date := feedbackCommandDate(args, "feedback signals")
	if date == "" {
		return tuskerError(errorInvalidArg, "feedback signals --date must be YYYY-MM-DD: "+args.String("date"))
	}
	sinceDate, since, err := feedbackCommandSince(args, "feedback signals")
	if err != nil {
		return err
	}
	derivation, err := deriveFeedbackSignalsForTargets(targets, date, sinceDate)
	if err != nil {
		return err
	}
	outputVault, err := feedbackGeneratedOutputVault(args, targets)
	if err != nil {
		return err
	}
	var written []string
	if args.Bool("write") {
		for _, signal := range derivation.Signals {
			path, err := writeFeedbackSignal(outputVault, signal)
			if err != nil {
				return err
			}
			written = append(written, path)
		}
	}
	if args.Bool("json") {
		emitJSON(map[string]any{
			"ok":            true,
			"date":          date,
			"since":         since,
			"counts":        feedbackSignalCountPayload(derivation, len(written)),
			"written_paths": written,
			"signals":       derivation.Signals,
		})
		return nil
	}
	fmt.Print(renderFeedbackSignalsCommandMarkdown(date, since, derivation, written))
	return nil
}

func feedbackReviewCmd(args Args) error {
	targets, err := feedbackDigestTargets(args)
	if err != nil {
		return err
	}
	date := feedbackCommandDate(args, "feedback review")
	if date == "" {
		return tuskerError(errorInvalidArg, "feedback review --date must be YYYY-MM-DD: "+args.String("date"))
	}
	sinceDate, since, err := feedbackCommandSince(args, "feedback review")
	if err != nil {
		return err
	}
	persisted, err := feedbackReviewSignalsForTargets(targets, sinceDate, date)
	if err != nil {
		return err
	}
	derivation, err := deriveFeedbackSignalsForTargets(targets, date, sinceDate)
	if err != nil {
		return err
	}
	signals := append(persisted, feedbackReviewSignalsFromFeedbackSignals(derivation.Signals)...)
	packet := buildFeedbackReviewPacketWithDiagnostics(date, since, signals, feedbackReviewDiagnostics{
		RawSignals:           derivation.RawEmitted + len(persisted),
		SkippedSignals:       derivation.Skipped,
		NoExplicitNoteVaults: derivation.NoExplicitNoteVaults,
	})
	markdown := renderFeedbackReviewPacketMarkdown(packet)
	outputPath := ""
	if args.Bool("write") {
		outputVault, err := feedbackGeneratedOutputVault(args, targets)
		if err != nil {
			return err
		}
		outputPath = feedbackReviewOutputPath(outputVault, date)
		if err := writeText(outputPath, markdown); err != nil {
			return err
		}
	}
	if args.Bool("json") {
		emitJSON(map[string]any{
			"ok":          true,
			"date":        date,
			"since":       since,
			"counts":      feedbackReviewCountPayload(packet),
			"output_path": nullIfEmptyString(outputPath),
		})
		return nil
	}
	fmt.Print(markdown)
	if outputPath != "" && !args.Bool("quiet") {
		fmt.Printf("\nWrote feedback review %s\n", outputPath)
	}
	return nil
}

func feedbackPromoteCmd(args Args) error {
	if feedbackPromoteCanonRequested(args) {
		return feedbackPromoteCanonCmd(args)
	}
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	source, err := feedbackPromoteSourceFromArgs(vaultPath, args)
	if err != nil {
		return err
	}
	plan, err := planFeedbackPromotion(source, feedbackPromoteOptions{
		VaultPath:    vaultPath,
		RepoRoot:     filepath.Dir(vaultPath),
		Apply:        args.Bool("write") || args.Bool("apply"),
		SummaryLimit: atoiDefault(args.String("limit"), feedbackPromoteDefaultSummaryLimit),
		NowDate:      firstNonEmpty(args.String("date"), todayISO()),
		DefaultEpic:  strings.ToUpper(firstNonEmpty(args.String("epic"), v7EpicFromTaskID(source.RelatedTask), "VSD")),
		DefaultActor: "agent:" + defaultActorName(),
	})
	if args.Bool("write") || args.Bool("apply") {
		if err := applyFeedbackPromotePlan(vaultPath, &plan, args); err != nil {
			return err
		}
	}
	if args.Bool("json") {
		emitJSON(map[string]any{
			"ok":       true,
			"mode":     plan.Mode,
			"summary":  plan.Summary,
			"outcomes": plan.Outcomes,
		})
		return nil
	}
	fmt.Print(renderFeedbackPromotePlanMarkdown(plan, atoiDefault(args.String("limit"), feedbackPromoteDefaultSummaryLimit)))
	return nil
}

func feedbackCommandDate(args Args, command string) string {
	date := strings.TrimSpace(args.String("date"))
	if date == "" {
		return todayISO()
	}
	if _, err := time.Parse("2006-01-02", date); err != nil {
		return ""
	}
	return date
}

func feedbackCommandSince(args Args, command string) (time.Time, string, error) {
	since := strings.TrimSpace(args.String("since"))
	if since == "" {
		return time.Time{}, "", tuskerError(errorMissingArg, command+" requires --since <YYYY-MM-DD>", withContext(map[string]any{"arg": "--since"}))
	}
	parsed, err := time.Parse("2006-01-02", since)
	if err != nil {
		return time.Time{}, "", tuskerError(errorInvalidArg, command+" --since must be YYYY-MM-DD: "+since)
	}
	return parsed, since, nil
}

func feedbackReviewOutputPath(vaultPath, date string) string {
	return filepath.Join(vaultPath, "feedback", "reviews", firstNonEmpty(feedbackReviewDateOnly(date), todayISO())+".md")
}

func deriveFeedbackSignalsForVault(vaultPath, date string, sinceDate time.Time) ([]feedbackSignal, map[string]int, error) {
	derivation, err := deriveFeedbackSignalsForTargets([]feedbackTarget{{vaultPath: vaultPath, repoRoot: filepath.Dir(vaultPath)}}, date, sinceDate)
	if err != nil {
		return nil, nil, err
	}
	return derivation.Signals, derivation.SourceCounts, nil
}

func deriveFeedbackSignalsForTargets(targets []feedbackTarget, date string, sinceDate time.Time) (feedbackSignalDerivation, error) {
	derivation := feedbackSignalDerivation{SourceCounts: map[string]int{}}
	var emissions []feedbackSignal
	for _, target := range targets {
		targetEmissions, sourceCounts, err := deriveFeedbackSignalEmissionsForVault(target.vaultPath, date, sinceDate)
		if err != nil {
			return feedbackSignalDerivation{}, err
		}
		for key, count := range sourceCounts {
			derivation.SourceCounts[key] += count
		}
		records, err := feedbackRecordsForVault(target.vaultPath, target.repoRoot, sinceDate)
		if err != nil {
			return feedbackSignalDerivation{}, err
		}
		derivation.SourceCounts["feedback_notes"] += len(records)
		if len(records) == 0 {
			derivation.NoExplicitNoteVaults = append(derivation.NoExplicitNoteVaults, feedbackTargetLabel(target))
		}
		for _, signal := range targetEmissions {
			emissions = append(emissions, withFeedbackSignalOccurrence(signal, target.vaultPath))
		}
	}
	collapsed := collapseFeedbackSignals(emissions)
	derivation.Signals = collapsed.Signals
	derivation.RawEmitted = collapsed.RawEmitted
	derivation.Skipped = collapsed.Skipped
	derivation.NoExplicitNoteVaults = uniqueStrings(feedbackReviewCleanList(derivation.NoExplicitNoteVaults))
	sortFeedbackSignalsForReview(derivation.Signals)
	return derivation, nil
}

func deriveFeedbackSignalEmissionsForVault(vaultPath, date string, sinceDate time.Time) ([]feedbackSignal, map[string]int, error) {
	events, err := feedbackSignalEventsForVault(vaultPath, sinceDate)
	if err != nil {
		return nil, nil, err
	}
	tasks, err := feedbackSignalTasksForVault(vaultPath, sinceDate, events)
	if err != nil {
		return nil, nil, err
	}
	input := feedbackSignalReducerInput{
		Date:    date,
		Project: v7ProjectID(vaultPath),
		Source:  "event_reducer",
		Tasks:   tasks,
		Events:  events,
	}
	signals := deriveFeedbackSignalEmissions(input)
	counts := map[string]int{"events": len(events), "tasks": len(tasks)}
	return signals, counts, nil
}

func sortFeedbackSignalsForReview(signals []feedbackSignal) {
	sort.SliceStable(signals, func(i, j int) bool {
		if feedbackReviewSeverityRank(signals[i].Severity) != feedbackReviewSeverityRank(signals[j].Severity) {
			return feedbackReviewSeverityRank(signals[i].Severity) > feedbackReviewSeverityRank(signals[j].Severity)
		}
		if feedbackReviewConfidenceRank(signals[i].Confidence) != feedbackReviewConfidenceRank(signals[j].Confidence) {
			return feedbackReviewConfidenceRank(signals[i].Confidence) > feedbackReviewConfidenceRank(signals[j].Confidence)
		}
		return signals[i].ID < signals[j].ID
	})
}

func feedbackReviewSignalsForTargets(targets []feedbackTarget, sinceDate time.Time, reviewDate string) ([]feedbackReviewSignal, error) {
	var out []feedbackReviewSignal
	for _, target := range targets {
		signals, err := feedbackReviewSignalsForVault(target.vaultPath, sinceDate, reviewDate)
		if err != nil {
			return nil, err
		}
		out = append(out, signals...)
	}
	return out, nil
}

func feedbackGeneratedOutputVault(args Args, targets []feedbackTarget) (string, error) {
	if outputVault := strings.TrimSpace(args.String("output-vault")); outputVault != "" {
		return filepath.Abs(outputVault)
	}
	if vaultArg := strings.TrimSpace(args.String("vault")); vaultArg != "" {
		return filepath.Abs(vaultArg)
	}
	if len(targets) == 0 {
		return "", tuskerError(errorMissingArg, "feedback output needs --repo, --vault, or --output-vault")
	}
	return targets[0].vaultPath, nil
}

func feedbackTargetLabel(target feedbackTarget) string {
	if target.repoRoot != "" {
		return filepath.Base(target.repoRoot) + ":" + filepath.ToSlash(target.vaultPath)
	}
	return filepath.ToSlash(target.vaultPath)
}

func feedbackSignalCountPayload(derivation feedbackSignalDerivation, written int) map[string]any {
	return map[string]any{
		"signals":          len(derivation.Signals),
		"raw_emitted":      derivation.RawEmitted,
		"collapsed":        len(derivation.Signals),
		"skipped":          derivation.Skipped,
		"written":          written,
		"sources":          derivation.SourceCounts,
		"no_explicit_note": derivation.NoExplicitNoteVaults,
	}
}

func feedbackReviewCountPayload(packet feedbackReviewPacket) map[string]any {
	return map[string]any{
		"signals":          len(packet.Signals),
		"raw_emitted":      packet.RawSignalCount,
		"collapsed":        packet.CollapsedSignalCount,
		"skipped":          packet.SkippedSignalCount,
		"actionable":       len(packet.Actionable),
		"ignored":          len(packet.Ignored),
		"no_explicit_note": packet.NoExplicitNoteVaults,
	}
}

func feedbackSignalEventsForVault(vaultPath string, sinceDate time.Time) ([]feedbackSignalEventInput, error) {
	eventsDir := filepath.Join(vaultPath, "events")
	if !dirExists(eventsDir) {
		return nil, nil
	}
	var events []feedbackSignalEventInput
	if err := walkDirUnsorted(eventsDir, func(current string, entry fs.DirEntry) error {
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".json") {
			return nil
		}
		raw, err := readText(current)
		if err != nil {
			return nil
		}
		var event struct {
			At         string         `json:"at"`
			Project    string         `json:"project"`
			Object     string         `json:"object"`
			ObjectKind string         `json:"object_kind"`
			EventKind  string         `json:"event_kind"`
			Payload    map[string]any `json:"payload"`
		}
		if err := json.Unmarshal([]byte(raw), &event); err != nil {
			return nil
		}
		if !feedbackSignalDateInWindow(event.At, sinceDate) {
			return nil
		}
		taskID := ""
		if event.ObjectKind == "task" {
			taskID = event.Object
		}
		if taskID == "" && event.Payload != nil {
			taskID = firstNonEmpty(toString(event.Payload["task"]), toString(event.Payload["task_id"]), toString(event.Payload["record_id"]))
		}
		if taskID == "" && strings.Contains(event.Object, "-A-") {
			taskID = strings.Split(event.Object, "-A-")[0]
		}
		attemptID := ""
		if event.ObjectKind == "attempt" {
			attemptID = event.Object
		}
		if event.Payload != nil {
			attemptID = firstNonEmpty(attemptID, toString(event.Payload["attempt"]), toString(event.Payload["attempt_id"]))
		}
		events = append(events, feedbackSignalEventInput{
			At:        event.At,
			Kind:      event.EventKind,
			Project:   firstNonEmpty(event.Project, v7ProjectID(vaultPath)),
			TaskID:    strings.ToUpper(strings.TrimSpace(taskID)),
			AttemptID: attemptID,
			Source:    "events",
			Payload:   event.Payload,
		})
		return nil
	}); err != nil {
		return nil, err
	}
	sort.SliceStable(events, func(i, j int) bool {
		return events[i].At < events[j].At
	})
	return events, nil
}

func feedbackSignalTasksForVault(vaultPath string, sinceDate time.Time, events []feedbackSignalEventInput) ([]feedbackSignalTaskInput, error) {
	eventTasks := map[string]bool{}
	eventRollups := map[string]*feedbackSignalTaskInput{}
	for _, event := range events {
		taskID := strings.ToUpper(strings.TrimSpace(event.TaskID))
		if taskID == "" {
			continue
		}
		eventTasks[taskID] = true
		rollup := eventRollups[taskID]
		if rollup == nil {
			rollup = &feedbackSignalTaskInput{ID: taskID, Project: feedbackSignalProjectFromTask(taskID), Source: "events"}
			eventRollups[taskID] = rollup
		}
		switch feedbackSignalReviewEventClass(event) {
		case "review":
			rollup.ReviewTransitions++
		case "rework":
			rollup.ReworkTransitions++
		}
		if total := feedbackSignalTokenTotal(event.Payload); total > 0 {
			rollup.TokenTotal += total
			rollup.CompletionIterations++
			rollup.AttemptID = firstNonEmpty(rollup.AttemptID, event.AttemptID)
		}
	}
	notes, err := listAllNotes(vaultPath)
	if err != nil {
		return nil, err
	}
	var tasks []feedbackSignalTaskInput
	seen := map[string]bool{}
	for _, note := range notes {
		if effectiveV7Kind(note.Data) != "task" {
			continue
		}
		taskID := strings.ToUpper(stringField(note.Data, "id"))
		date := firstNonEmpty(frontmatterDateOnly(note.Data, "updated_at"), frontmatterDateOnly(note.Data, "created_at"))
		if !eventTasks[taskID] && !includeImproveDate(date, sinceDate) {
			continue
		}
		task := feedbackSignalTaskInputFromNote(note)
		task.ID = taskID
		task.Project = firstNonEmpty(task.Project, feedbackSignalProjectFromTask(taskID), v7ProjectID(vaultPath))
		if rollup := eventRollups[taskID]; rollup != nil {
			task.ReviewTransitions = rollup.ReviewTransitions
			task.ReworkTransitions = rollup.ReworkTransitions
			task.TokenTotal = rollup.TokenTotal
			task.CompletionIterations = rollup.CompletionIterations
			task.AttemptID = firstNonEmpty(task.AttemptID, rollup.AttemptID)
		}
		task.Source = firstNonEmpty(task.Source, note.RelativePath)
		tasks = append(tasks, task)
		seen[taskID] = true
	}
	for taskID, rollup := range eventRollups {
		if !seen[taskID] {
			tasks = append(tasks, *rollup)
		}
	}
	return tasks, nil
}

func feedbackContractSignalsFromTasks(date string, tasks []feedbackSignalTaskInput) []feedbackSignal {
	var signals []feedbackSignal
	seen := map[string]bool{}
	for _, task := range tasks {
		labels := feedbackContractLabels(task)
		if len(labels) == 0 {
			continue
		}
		severity := "P2"
		if containsString(labels, "thin") || containsString(labels, "unverifiable") || containsString(labels, "missing-proof-map") || containsString(labels, "contradictory") {
			severity = "P1"
		}
		signal := completeFeedbackSignal(feedbackSignal{
			Date:       date,
			Project:    firstNonEmpty(task.Project, feedbackSignalProjectFromTask(task.ID)),
			TaskID:     task.ID,
			AttemptID:  task.AttemptID,
			Source:     firstNonEmpty(task.Source, "task_contract_reducer"),
			Category:   "acceptance_quality",
			Severity:   severity,
			Confidence: "high",
			DedupeKey:  feedbackSignalDedupeKey(task.Project, "acceptance_quality", task.ID, strings.Join(labels, "-")),
			Summary:    task.ID + " acceptance criteria are " + strings.Join(labels, ", ") + ".",
			ObservedFacts: map[string]any{
				"task":               task.ID,
				"labels":             labels,
				"acceptance_total":   task.AcceptanceTotal,
				"missing_acceptance": feedbackSignalShortList(task.MissingAcceptance),
				"missing_proof":      feedbackSignalShortList(task.MissingProof),
			},
			Recommendation: "Repair acceptance criteria and proof mapping before dispatch.",
		})
		signals = appendFeedbackSignal(signals, seen, signal)
	}
	return signals
}

func feedbackContractLabels(task feedbackSignalTaskInput) []string {
	var labels []string
	if task.AcceptanceTotal == 0 {
		labels = append(labels, "thin", "unverifiable")
	}
	if len(task.MissingAcceptance) > 0 {
		labels = append(labels, "missing-acceptance")
	}
	if len(task.ProofMapGaps) > 0 || containsString(task.MissingAcceptance, "proof-map") {
		labels = append(labels, "missing-proof-map")
	}
	if len(task.ProofEvidenceGaps) > 0 || len(task.MissingProof) > 0 {
		labels = append(labels, "missing-proof-evidence")
	}
	if strings.EqualFold(task.Status, "done") && (len(task.MissingAcceptance) > 0 || len(task.ProofMapGaps) > 0 || len(task.ProofEvidenceGaps) > 0 || len(task.MissingProof) > 0) {
		labels = append(labels, "done-with-gaps")
	}
	if strings.EqualFold(task.Title, "TBD") {
		labels = append(labels, "thin")
	}
	return uniqueStringsPreserveOrder(labels)
}

func feedbackSignalDateInWindow(value string, sinceDate time.Time) bool {
	if sinceDate.IsZero() || strings.TrimSpace(value) == "" {
		return true
	}
	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		return !parsed.Before(sinceDate)
	}
	if len(value) >= 10 {
		if parsed, err := time.Parse("2006-01-02", value[:10]); err == nil {
			return !parsed.Before(sinceDate)
		}
	}
	return true
}

func feedbackSignalProjectFromTask(taskID string) string {
	if idx := strings.Index(taskID, "-T-"); idx > 0 {
		return taskID[:idx]
	}
	return ""
}

func dedupeFeedbackCommandSignals(signals []feedbackSignal) []feedbackSignal {
	seen := map[string]feedbackSignal{}
	for _, signal := range signals {
		signal = completeFeedbackSignal(signal)
		if len(validateFeedbackSignal(signal)) > 0 {
			continue
		}
		current, ok := seen[signal.DedupeKey]
		if !ok || feedbackReviewSeverityRank(signal.Severity) > feedbackReviewSeverityRank(current.Severity) {
			seen[signal.DedupeKey] = signal
		}
	}
	var out []feedbackSignal
	for _, signal := range seen {
		out = append(out, signal)
	}
	return out
}

func feedbackReviewSignalsFromFeedbackSignals(signals []feedbackSignal) []feedbackReviewSignal {
	var out []feedbackReviewSignal
	for _, signal := range signals {
		occurrenceVaults, occurrenceProjects, occurrenceSources := feedbackSignalOccurrenceLists(signal.Occurrences)
		out = append(out, feedbackReviewSignal{
			Schema:             signal.Schema,
			ID:                 signal.ID,
			Date:               signal.Date,
			Project:            signal.Project,
			Task:               signal.TaskID,
			Attempt:            signal.AttemptID,
			Source:             signal.Source,
			Category:           signal.Category,
			Severity:           signal.Severity,
			Confidence:         signal.Confidence,
			DedupeKey:          signal.DedupeKey,
			Summary:            signal.Summary,
			ObservedFacts:      feedbackSignalFactsList(signal.ObservedFacts),
			Recommendation:     signal.Recommendation,
			Frequency:          feedbackSignalFrequency(signal),
			Occurrences:        len(signal.Occurrences),
			OccurrenceVaults:   occurrenceVaults,
			OccurrenceProjects: occurrenceProjects,
			OccurrenceSources:  occurrenceSources,
			SourcePath:         feedbackSignalRelativePath(signal),
		})
	}
	return out
}

func feedbackSignalOccurrenceLists(occurrences []feedbackSignalOccurrence) ([]string, []string, []string) {
	var vaults []string
	var projects []string
	var sources []string
	for _, occurrence := range occurrences {
		vaults = append(vaults, occurrence.Vault)
		projects = append(projects, occurrence.Project)
		sources = append(sources, occurrence.Source)
	}
	return uniqueStrings(feedbackReviewCleanList(vaults)), uniqueStrings(feedbackReviewCleanList(projects)), uniqueStrings(feedbackReviewCleanList(sources))
}

func feedbackSignalFactsList(facts map[string]any) []string {
	var out []string
	for _, key := range sortedAnyMapKeys(facts) {
		value := facts[key]
		switch typed := value.(type) {
		case []string:
			for _, item := range typed {
				out = append(out, key+": "+item)
			}
		case []any:
			for _, item := range typed {
				out = append(out, key+": "+toString(item))
			}
		default:
			if text := toString(value); strings.TrimSpace(text) != "" {
				out = append(out, key+": "+text)
			}
		}
	}
	return feedbackReviewCleanList(out)
}

func feedbackSignalFrequency(signal feedbackSignal) int {
	if len(signal.Occurrences) > 1 {
		return len(signal.Occurrences)
	}
	for _, key := range []string{"occurrence_count", "review_transitions", "rework_transitions", "review_requests", "handoff_count", "turns", "total_tokens"} {
		if value := intValue(signal.ObservedFacts[key]); value > 1 {
			if key == "total_tokens" {
				return 1
			}
			return value
		}
	}
	return 1
}

func renderFeedbackSignalsCommandMarkdown(date, since string, derivation feedbackSignalDerivation, written []string) string {
	var b strings.Builder
	b.WriteString("# Tusker Feedback Signals - " + date + "\n\n")
	b.WriteString("- Since: " + since + "\n")
	b.WriteString(fmt.Sprintf("- Sources: events=%d tasks=%d feedback_notes=%d\n", derivation.SourceCounts["events"], derivation.SourceCounts["tasks"], derivation.SourceCounts["feedback_notes"]))
	b.WriteString(fmt.Sprintf("- Raw signals emitted: %d\n", derivation.RawEmitted))
	b.WriteString(fmt.Sprintf("- Collapsed signals: %d\n", len(derivation.Signals)))
	b.WriteString(fmt.Sprintf("- Signals skipped: %d\n", derivation.Skipped))
	b.WriteString("- No explicit feedback notes: " + feedbackReviewList(derivation.NoExplicitNoteVaults) + "\n")
	b.WriteString(fmt.Sprintf("- Signals: %d\n\n", len(derivation.Signals)))
	b.WriteString("| Severity | Confidence | Category | Task | Signal | Summary |\n")
	b.WriteString("|---|---|---|---|---|---|\n")
	if len(derivation.Signals) == 0 {
		b.WriteString("| - | - | - | - | - | No feedback signals found for this window. |\n")
	} else {
		for _, signal := range derivation.Signals {
			b.WriteString("| " + signal.Severity + " | " + signal.Confidence + " | " + signal.Category + " | " + firstNonEmpty(signal.TaskID, "-") + " | `" + signal.ID + "` | " + markdownCell(signal.Summary) + " |\n")
		}
	}
	if len(written) > 0 {
		b.WriteString("\n## Written\n\n")
		for _, path := range written {
			b.WriteString("- " + path + "\n")
		}
	}
	return b.String()
}

func feedbackPromoteSourceFromArgs(vaultPath string, args Args) (feedbackPromoteSource, error) {
	if reviewPath := strings.TrimSpace(args.String("review")); reviewPath != "" {
		abs, err := filepath.Abs(reviewPath)
		if err != nil {
			return feedbackPromoteSource{}, err
		}
		title := firstNonEmpty(args.String("title"), "Promote findings from "+filepath.Base(reviewPath))
		return normalizeFeedbackPromoteSource(feedbackPromoteSource{
			Kind:         "daily_review_action",
			ID:           "review:" + filepath.ToSlash(reviewPath),
			Path:         filepath.ToSlash(reviewPath),
			Title:        title,
			Summary:      "Promote one bounded action from " + abs,
			ProductIdea:  title,
			Severity:     firstNonEmpty(args.String("severity"), "P2"),
			DedupeKey:    "review:" + filepath.Base(reviewPath),
			SourceSignal: filepath.ToSlash(reviewPath),
			Prevention:   firstNonEmpty(args.String("prevention"), "Prevent recurrence by turning the reviewed finding into one explicit Tusker work item."),
			RepeatCount:  2,
		}), nil
	}
	ref := strings.TrimSpace(firstNonEmpty(args.String("signal"), args.String("id"), args.String("_pos1")))
	if ref == "" {
		return feedbackPromoteSource{}, tuskerError(errorMissingArg, "feedback promote requires <signal-id>, --signal <id>, or --review <path>")
	}
	signal, err := feedbackSignalByRef(vaultPath, ref)
	if err != nil {
		return feedbackPromoteSource{}, err
	}
	return feedbackPromoteSourceFromSignal(signal), nil
}

func feedbackPromoteSourceFromSignal(signal feedbackSignal) feedbackPromoteSource {
	return normalizeFeedbackPromoteSource(feedbackPromoteSource{
		Kind:         "feedback_signal",
		ID:           signal.ID,
		Path:         feedbackSignalRelativePath(signal),
		Title:        firstNonEmpty(signal.Recommendation, signal.Summary),
		Summary:      signal.Summary,
		Friction:     signal.Summary,
		ProductIdea:  signal.Recommendation,
		Severity:     signal.Severity,
		DedupeKey:    signal.DedupeKey,
		SourceSignal: signal.ID,
		RelatedTask:  signal.TaskID,
		Prevention:   signal.Recommendation,
		RepeatCount:  feedbackSignalFrequency(signal),
		Evidence: []feedbackPromoteEvidence{{
			Source: "feedback_signal",
			Ref:    signal.ID,
			Title:  signal.Summary,
			Date:   signal.Date,
		}},
	})
}

func feedbackSignalByRef(vaultPath, ref string) (feedbackSignal, error) {
	if fileExists(ref) {
		return feedbackSignalFromFile(ref)
	}
	var found feedbackSignal
	signalsDir := filepath.Join(vaultPath, "feedback", "signals")
	if !dirExists(signalsDir) {
		return feedbackSignal{}, tuskerError(errorNotFound, "feedback signal not found: "+ref)
	}
	if err := walkDirUnsorted(signalsDir, func(current string, entry fs.DirEntry) error {
		if entry.IsDir() || found.ID != "" || !strings.HasSuffix(strings.ToLower(entry.Name()), ".json") {
			return nil
		}
		signal, err := feedbackSignalFromFile(current)
		if err != nil {
			return nil
		}
		if signal.ID == ref || signal.DedupeKey == ref {
			found = signal
		}
		return nil
	}); err != nil {
		return feedbackSignal{}, err
	}
	if found.ID == "" {
		return feedbackSignal{}, tuskerError(errorNotFound, "feedback signal not found: "+ref)
	}
	return found, nil
}

func feedbackSignalFromFile(path string) (feedbackSignal, error) {
	raw, err := readText(path)
	if err != nil {
		return feedbackSignal{}, err
	}
	var signal feedbackSignal
	if err := json.Unmarshal([]byte(raw), &signal); err != nil {
		return feedbackSignal{}, err
	}
	return completeFeedbackSignal(signal), nil
}

func validateV7FeedbackSignals(vaultPath string) []Issue {
	signalsDir := filepath.Join(vaultPath, "feedback", "signals")
	if !dirExists(signalsDir) {
		return nil
	}
	var issues []Issue
	if err := walkDirUnsorted(signalsDir, func(current string, entry fs.DirEntry) error {
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".json") {
			return nil
		}
		signal, err := feedbackSignalFromFile(current)
		rel, _ := filepath.Rel(vaultPath, current)
		rel = filepath.ToSlash(rel)
		if err != nil {
			issues = append(issues, issue("FEEDBACK_SIGNAL_READ_FAILED", err.Error(), rel, "", nil))
			return nil
		}
		for _, currentIssue := range validateFeedbackSignal(signal) {
			currentIssue.Path = rel
			issues = append(issues, currentIssue)
		}
		return nil
	}); err != nil {
		return []Issue{issue("FEEDBACK_SIGNAL_VALIDATE_FAILED", err.Error(), "feedback/signals", "", nil)}
	}
	return issues
}

func applyFeedbackPromotePlan(vaultPath string, plan *feedbackPromotePlan, args Args) error {
	if len(plan.Outcomes) != 1 {
		return tuskerError(errorInvalidArg, "feedback promote applies exactly one outcome at a time")
	}
	outcome := &plan.Outcomes[0]
	switch outcome.Operation {
	case "skip", "link", "update":
		return writeFeedbackPromotionRecord(vaultPath, plan, outcome, args)
	case "create":
		switch outcome.Kind {
		case "task", "cli_proposal":
			return applyFeedbackPromoteTask(vaultPath, plan, outcome, args)
		case "decision":
			return applyFeedbackPromoteDecision(vaultPath, plan, outcome, args)
		case "runbook", "skill", "gate":
			return writeFeedbackPromotionRecord(vaultPath, plan, outcome, args)
		default:
			return writeFeedbackPromotionRecord(vaultPath, plan, outcome, args)
		}
	default:
		return tuskerError(errorInvalidArg, "unknown feedback promote outcome: "+outcome.Operation)
	}
}

func applyFeedbackPromoteTask(vaultPath string, plan *feedbackPromotePlan, outcome *feedbackPromoteOutcome, args Args) error {
	epic := strings.ToUpper(firstNonEmpty(args.String("epic"), v7EpicFromTaskID(firstNonEmpty(outcome.RelatedTasks...)), "VSD"))
	id := nextSafeV7TaskID(vaultPath, epic)
	if err := newV7Task(Args{
		"vault":      vaultPath,
		"quiet":      "true",
		"epic":       epic,
		"id":         id,
		"title":      outcome.Title,
		"status":     "backlog",
		"priority":   strings.ToLower(firstNonEmpty(outcome.Severity, "P2")),
		"risk":       feedbackPromoteRiskFromSeverity(outcome.Severity),
		"size":       "m",
		"proof-mode": "inline",
	}); err != nil {
		return err
	}
	path := filepath.Join(vaultPath, "work", "tasks", id+".md")
	data, body, err := parseFrontmatterMustRead(path)
	if err != nil {
		return err
	}
	body = replaceSection(body, "## Intent", strings.Join([]string{
		outcome.Title,
		"",
		"Source feedback: " + strings.Join(outcome.SourceRefs, ", "),
		"Prevention statement: " + outcome.Prevention,
	}, "\n"))
	body = replaceSection(body, "## Acceptance", "| ID | Outcome | Proof |\n|---|---|---|\n| A1 | The feedback recurrence path is removed or explicitly guarded. | Focused regression or documented behavior proof |\n| A2 | The task links the source feedback signal/review and states how recurrence is prevented. | Task/evidence review |")
	body = replaceSection(body, "## Verification", "| Covers | Check | Result | Notes |\n|---|---|---|---|\n| A1-A2 | go test ./cmd/tusker -run '<focused-regression>' -count=1 | pending | Replace with the exact regression when implementing. |")
	body = replaceSection(body, "## Knowledge delta", "Promoted from feedback: "+strings.Join(outcome.SourceRefs, ", ")+". Prevention: "+outcome.Prevention)
	data["state_rev"] = v7StateRev(data, body)
	content, err := serializeDocument(data, body, v7FrontmatterOrder["task"])
	if err != nil {
		return err
	}
	if err := writeText(path, content); err != nil {
		return err
	}
	rel := filepath.ToSlash(filepath.Join("work", "tasks", id+".md"))
	outcome.TargetID = id
	outcome.TargetPath = rel
	plan.Summary = summarizeFeedbackPromoteOutcomes(plan.Outcomes, atoiDefault(args.String("limit"), feedbackPromoteDefaultSummaryLimit))
	return emitV7Event(vaultPath, id, "task", "updated", "agent:"+defaultActorName(), map[string]any{"source": "feedback_promote", "source_refs": outcome.SourceRefs, "dedupe_key": outcome.DedupeKey})
}

func applyFeedbackPromoteDecision(vaultPath string, plan *feedbackPromotePlan, outcome *feedbackPromoteOutcome, args Args) error {
	epic := strings.ToUpper(firstNonEmpty(args.String("epic"), v7EpicFromTaskID(firstNonEmpty(outcome.RelatedTasks...)), "VSD"))
	id := fmt.Sprintf("%s-D-%s", epic, padNumber(nextV7Sequence(vaultPath, epic, "decision")))
	if err := newV7Decision(Args{
		"vault":    vaultPath,
		"quiet":    "true",
		"epic":     epic,
		"id":       id,
		"title":    outcome.Title,
		"decision": firstNonEmpty(toString(outcome.ProposedFields["suggested_resolution"]), outcome.Prevention),
	}); err != nil {
		return err
	}
	outcome.TargetID = id
	outcome.TargetPath = filepath.ToSlash(filepath.Join("work", "decisions", id+".md"))
	plan.Summary = summarizeFeedbackPromoteOutcomes(plan.Outcomes, atoiDefault(args.String("limit"), feedbackPromoteDefaultSummaryLimit))
	return nil
}

func writeFeedbackPromotionRecord(vaultPath string, plan *feedbackPromotePlan, outcome *feedbackPromoteOutcome, args Args) error {
	date := firstNonEmpty(args.String("date"), todayISO())
	path := filepath.Join(vaultPath, "feedback", "promotions", date, feedbackSlug(outcome.Kind+"-"+outcome.Title, "promotion")+".md")
	content := strings.Join([]string{
		"# Feedback Promotion",
		"",
		"- operation: " + outcome.Operation,
		"- kind: " + outcome.Kind,
		"- title: " + outcome.Title,
		"- source-refs: " + strings.Join(outcome.SourceRefs, ", "),
		"- related-tasks: " + strings.Join(outcome.RelatedTasks, ", "),
		"- dedupe-key: " + outcome.DedupeKey,
		"- prevention: " + outcome.Prevention,
		"- reasons: " + strings.Join(outcome.Reasons, "; "),
		"",
	}, "\n")
	if err := writeText(path, content); err != nil {
		return err
	}
	rel, _ := filepath.Rel(vaultPath, path)
	outcome.TargetPath = filepath.ToSlash(rel)
	plan.Summary = summarizeFeedbackPromoteOutcomes(plan.Outcomes, atoiDefault(args.String("limit"), feedbackPromoteDefaultSummaryLimit))
	return nil
}

func feedbackPromoteRiskFromSeverity(severity string) string {
	switch strings.ToUpper(severity) {
	case "P0", "P1":
		return "high"
	default:
		return "medium"
	}
}
