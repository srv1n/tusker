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

const (
	feedbackReviewDefaultMaxFacts    = 2
	feedbackReviewDefaultMaxFindings = 8
)

type feedbackReviewSignal struct {
	Schema             string
	ID                 string
	Date               string
	Project            string
	Task               string
	TaskIDs            []string
	Attempt            string
	AttemptIDs         []string
	Source             string
	Category           string
	Severity           string
	Confidence         string
	DedupeKey          string
	Summary            string
	ObservedFacts      []string
	LikelyCause        string
	Recommendation     string
	ActionType         string
	Prevention         string
	Frequency          int
	Occurrences        int
	OccurrenceVaults   []string
	OccurrenceProjects []string
	OccurrenceSources  []string
	LastSeen           string
	SourcePath         string
	SourceRefs         []string
}

func feedbackReviewSignalsJSON(signals []feedbackReviewSignal) []map[string]any {
	out := make([]map[string]any, 0, len(signals))
	for _, signal := range signals {
		out = append(out, map[string]any{
			"id": feedbackReviewSignalID(signal), "date": signal.Date, "project": signal.Project,
			"task": signal.Task, "task_ids": feedbackReviewTaskIDs(signal), "source": signal.Source,
			"source_path": signal.SourcePath, "source_refs": signal.SourceRefs, "category": signal.Category,
			"severity": signal.Severity, "confidence": signal.Confidence, "dedupe_key": signal.DedupeKey,
			"summary": signal.Summary, "observed_facts": signal.ObservedFacts,
			"recommendation": signal.Recommendation, "frequency": feedbackReviewSignalFrequency(signal),
		})
	}
	return out
}

func feedbackReviewFindingsJSON(findings []feedbackReviewFinding) []map[string]any {
	out := make([]map[string]any, 0, len(findings))
	for _, finding := range findings {
		out = append(out, map[string]any{
			"id": finding.ID, "key": finding.Key, "category": finding.Category,
			"severity": finding.Severity, "confidence": finding.Confidence, "summary": finding.Summary,
			"likely_cause": finding.LikelyCause, "recommendation": finding.Recommendation,
			"action_type": finding.ActionType, "prevention": finding.Prevention,
			"signal_ids": finding.SignalIDs, "task_ids": finding.TaskIDs, "source_refs": finding.SourceRefs,
			"dates": finding.Dates, "frequency": finding.Frequency, "latest_date": finding.LatestDate,
		})
	}
	return out
}

type feedbackReviewDiagnostics struct {
	RawSignals           int
	SkippedSignals       int
	NoExplicitNoteVaults []string
}

type feedbackReviewPacket struct {
	Date                 string
	Since                string
	Signals              []feedbackReviewSignal
	Findings             []feedbackReviewFinding
	Actionable           []feedbackReviewFinding
	Ignored              []feedbackReviewFinding
	RawSignalCount       int
	CollapsedSignalCount int
	SkippedSignalCount   int
	NoExplicitNoteVaults []string
}

type feedbackReviewFinding struct {
	ID             string
	Key            string
	Category       string
	Severity       string
	Confidence     string
	Summary        string
	LikelyCause    string
	Recommendation string
	ActionType     string
	Prevention     string
	Signals        []feedbackReviewSignal
	SignalIDs      []string
	TaskIDs        []string
	SourceRefs     []string
	Dates          []string
	Frequency      int
	LatestDate     string
	severityRank   int
	confidenceRank int
}

func feedbackReviewSignalsForVault(vaultPath string, sinceDate time.Time, reviewDate string) ([]feedbackReviewSignal, error) {
	signalsDir := filepath.Join(vaultPath, "feedback", "signals")
	if !dirExists(signalsDir) {
		return nil, nil
	}
	var paths []string
	if err := walkDirUnsorted(signalsDir, func(current string, entry fs.DirEntry) error {
		if entry.IsDir() {
			return nil
		}
		if strings.HasSuffix(strings.ToLower(entry.Name()), ".json") {
			paths = append(paths, current)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	sort.Strings(paths)
	signals, err := feedbackReviewSignalsFromFiles(paths)
	if err != nil {
		return nil, err
	}
	var out []feedbackReviewSignal
	for _, signal := range signals {
		if (sinceDate.IsZero() && strings.TrimSpace(reviewDate) == "") || feedbackReviewSignalInWindow(signal, sinceDate, reviewDate) {
			out = append(out, signal)
		}
	}
	return out, nil
}

func feedbackReviewSignalsFromFiles(paths []string) ([]feedbackReviewSignal, error) {
	var files []string
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		info, err := filepath.Abs(path)
		if err == nil {
			path = info
		}
		if dirExists(path) {
			if err := walkDirUnsorted(path, func(current string, entry fs.DirEntry) error {
				if entry.IsDir() {
					return nil
				}
				if strings.HasSuffix(strings.ToLower(entry.Name()), ".json") {
					files = append(files, current)
				}
				return nil
			}); err != nil {
				return nil, err
			}
			continue
		}
		files = append(files, path)
	}
	sort.Strings(files)
	var signals []feedbackReviewSignal
	for _, filePath := range files {
		raw, err := readText(filePath)
		if err != nil {
			return nil, err
		}
		parsed, err := feedbackReviewSignalsFromJSON([]byte(raw), filePath)
		if err != nil {
			return nil, err
		}
		signals = append(signals, parsed...)
	}
	return signals, nil
}

func feedbackReviewSignalsFromJSON(raw []byte, sourcePath string) ([]feedbackReviewSignal, error) {
	var payload any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("parse feedback review signal %s: %w", sourcePath, err)
	}
	switch value := payload.(type) {
	case []any:
		out := make([]feedbackReviewSignal, 0, len(value))
		for _, item := range value {
			mapped, ok := item.(map[string]any)
			if !ok {
				continue
			}
			out = append(out, feedbackReviewSignalFromMap(mapped, sourcePath))
		}
		return out, nil
	case map[string]any:
		for _, key := range []string{"signals", "items", "records"} {
			if nested := feedbackReviewMapValue(value, key); nested != nil {
				var out []feedbackReviewSignal
				for _, item := range feedbackReviewListOfMaps(nested) {
					out = append(out, feedbackReviewSignalFromMap(item, sourcePath))
				}
				return out, nil
			}
		}
		return []feedbackReviewSignal{feedbackReviewSignalFromMap(value, sourcePath)}, nil
	default:
		return nil, fmt.Errorf("parse feedback review signal %s: expected object, array, or object with signals", sourcePath)
	}
}

func feedbackReviewSignalFromMap(raw map[string]any, sourcePath string) feedbackReviewSignal {
	occurrenceCount, occurrenceVaults, occurrenceProjects, occurrenceSources := feedbackReviewOccurrenceFacts(raw)
	signal := feedbackReviewSignal{
		Schema:             feedbackReviewMapString(raw, "schema"),
		ID:                 feedbackReviewMapString(raw, "id", "signal_id", "signal"),
		Date:               feedbackReviewDateOnly(feedbackReviewMapString(raw, "date", "created_at", "updated_at")),
		Project:            feedbackReviewMapString(raw, "project"),
		Task:               feedbackReviewMapString(raw, "task", "task_id"),
		TaskIDs:            feedbackReviewMapList(raw, "task_ids", "tasks", "related_tasks"),
		Attempt:            feedbackReviewMapString(raw, "attempt", "attempt_id"),
		AttemptIDs:         feedbackReviewMapList(raw, "attempt_ids", "attempts"),
		Source:             feedbackReviewMapString(raw, "source", "source_kind"),
		Category:           feedbackReviewMapString(raw, "category", "kind"),
		Severity:           feedbackReviewMapString(raw, "severity", "priority"),
		Confidence:         feedbackReviewMapString(raw, "confidence"),
		DedupeKey:          feedbackReviewMapString(raw, "dedupe_key", "dedupe-key", "key"),
		Summary:            feedbackReviewMapString(raw, "summary", "title"),
		ObservedFacts:      feedbackReviewMapList(raw, "observed_facts", "observed-facts", "facts", "observed", "evidence"),
		LikelyCause:        feedbackReviewMapString(raw, "likely_cause", "likely-cause", "cause"),
		Recommendation:     feedbackReviewMapString(raw, "recommendation", "recommended_action", "recommended-action", "action", "proposed_action"),
		ActionType:         feedbackReviewMapString(raw, "action_type", "action-type", "classification"),
		Prevention:         feedbackReviewMapString(raw, "prevention", "prevents", "prevent_recurrence"),
		Frequency:          feedbackReviewMapInt(raw, "frequency", "count"),
		Occurrences:        maxInt(feedbackReviewMapInt(raw, "occurrences", "occurrence_count"), occurrenceCount),
		OccurrenceVaults:   occurrenceVaults,
		OccurrenceProjects: occurrenceProjects,
		OccurrenceSources:  occurrenceSources,
		LastSeen:           feedbackReviewDateOnly(feedbackReviewMapString(raw, "last_seen", "last-seen", "seen_at")),
		SourcePath:         sourcePath,
	}
	if signal.Date == "" {
		signal.Date = signal.LastSeen
	}
	signal.TaskIDs = uniqueStrings(feedbackReviewTaskIDs(signal))
	signal.AttemptIDs = uniqueStrings(feedbackReviewCleanList(append(signal.AttemptIDs, signal.Attempt)))
	signal.ObservedFacts = feedbackReviewCleanList(signal.ObservedFacts)
	signal.SourceRefs = uniqueStrings(feedbackReviewCleanList(append(signal.SourceRefs, signal.SourcePath)))
	return signal
}

func buildFeedbackReviewPacket(date, since string, signals []feedbackReviewSignal) feedbackReviewPacket {
	return buildFeedbackReviewPacketWithDiagnostics(date, since, signals, feedbackReviewDiagnostics{})
}

func buildFeedbackReviewPacketWithDiagnostics(date, since string, signals []feedbackReviewSignal, diagnostics feedbackReviewDiagnostics) feedbackReviewPacket {
	date = feedbackReviewDateOnly(firstNonEmpty(date, todayISO()))
	since = feedbackReviewDateOnly(since)
	packet := feedbackReviewPacket{
		Date:                 date,
		Since:                since,
		SkippedSignalCount:   diagnostics.SkippedSignals,
		NoExplicitNoteVaults: uniqueStrings(feedbackReviewCleanList(diagnostics.NoExplicitNoteVaults)),
	}
	for _, signal := range signals {
		if !feedbackReviewSignalInWindow(signal, feedbackReviewParseDate(since), date) {
			continue
		}
		packet.Signals = append(packet.Signals, signal)
	}
	packet.RawSignalCount = diagnostics.RawSignals
	if packet.RawSignalCount == 0 {
		packet.RawSignalCount = len(packet.Signals)
	}
	packet.Findings = feedbackReviewFindings(packet.Signals)
	packet.CollapsedSignalCount = len(packet.Findings)
	for _, finding := range packet.Findings {
		if feedbackReviewFindingIsNoise(finding) {
			packet.Ignored = append(packet.Ignored, finding)
		} else {
			packet.Actionable = append(packet.Actionable, finding)
		}
	}
	return packet
}

func renderFeedbackReviewPacketMarkdown(packet feedbackReviewPacket) string {
	var b strings.Builder
	b.WriteString("# Daily Product Review - " + packet.Date + "\n\n")
	if packet.Since != "" {
		b.WriteString("- Since: " + packet.Since + "\n")
	}
	b.WriteString(fmt.Sprintf("- Signals reviewed: %d\n", len(packet.Signals)))
	b.WriteString(fmt.Sprintf("- Raw signals emitted: %d\n", packet.RawSignalCount))
	b.WriteString(fmt.Sprintf("- Collapsed findings: %d\n", packet.CollapsedSignalCount))
	b.WriteString(fmt.Sprintf("- Signals skipped: %d\n", packet.SkippedSignalCount))
	b.WriteString("- No explicit feedback notes: " + feedbackReviewList(packet.NoExplicitNoteVaults) + "\n")
	b.WriteString(fmt.Sprintf("- Actionable friction groups: %d\n", len(packet.Actionable)))
	if len(packet.Actionable) == 0 {
		b.WriteString("- Recommendation: no product action recommended\n")
	}
	b.WriteString("\n")

	renderFeedbackReviewFacts(&b, packet)
	renderFeedbackReviewLikelyCauses(&b, packet)
	renderFeedbackReviewProposedActions(&b, packet)
	renderFeedbackReviewHumanDecisions(&b, packet)
	return b.String()
}

func feedbackReviewPacketOutputPath(vaultPath, date string) string {
	return filepath.Join(vaultPath, "feedback", "reviews", feedbackReviewDateOnly(firstNonEmpty(date, todayISO()))+".md")
}

func feedbackReviewFindings(signals []feedbackReviewSignal) []feedbackReviewFinding {
	groups := map[string][]feedbackReviewSignal{}
	for _, signal := range signals {
		if strings.TrimSpace(signal.ID+signal.Summary+signal.DedupeKey+signal.Category) == "" {
			continue
		}
		key := feedbackReviewSignalGroupKey(signal)
		groups[key] = append(groups[key], signal)
	}
	findings := make([]feedbackReviewFinding, 0, len(groups))
	for key, group := range groups {
		sort.SliceStable(group, func(i, j int) bool {
			return feedbackReviewSignalLess(group[i], group[j])
		})
		primary := group[0]
		finding := feedbackReviewFinding{
			Key:            key,
			Category:       firstNonEmpty(primary.Category, "general"),
			Severity:       firstNonEmpty(primary.Severity, "low"),
			Confidence:     firstNonEmpty(primary.Confidence, "low"),
			Summary:        feedbackReviewCleanText(firstNonEmpty(primary.Summary, primary.Recommendation, "Untitled feedback signal"), 180),
			LikelyCause:    feedbackReviewCleanText(primary.LikelyCause, 220),
			Recommendation: feedbackReviewCleanText(primary.Recommendation, 220),
			Signals:        group,
		}
		for _, signal := range group {
			finding.SignalIDs = append(finding.SignalIDs, feedbackReviewSignalID(signal))
			finding.TaskIDs = append(finding.TaskIDs, feedbackReviewTaskIDs(signal)...)
			finding.SourceRefs = append(finding.SourceRefs, signal.SourceRefs...)
			if date := feedbackReviewSignalDate(signal); date != "" {
				finding.Dates = append(finding.Dates, date)
				if finding.LatestDate == "" || date > finding.LatestDate {
					finding.LatestDate = date
				}
			}
			finding.Frequency += feedbackReviewSignalFrequency(signal)
			if feedbackReviewSeverityRank(signal.Severity) > feedbackReviewSeverityRank(finding.Severity) {
				finding.Severity = signal.Severity
			}
			if feedbackReviewConfidenceRank(signal.Confidence) > feedbackReviewConfidenceRank(finding.Confidence) {
				finding.Confidence = signal.Confidence
			}
			if finding.Recommendation == "" {
				finding.Recommendation = feedbackReviewCleanText(signal.Recommendation, 220)
			}
			if finding.LikelyCause == "" {
				finding.LikelyCause = feedbackReviewCleanText(signal.LikelyCause, 220)
			}
			if finding.Prevention == "" {
				finding.Prevention = feedbackReviewCleanText(signal.Prevention, 220)
			}
		}
		finding.SignalIDs = uniqueStrings(feedbackReviewCleanList(finding.SignalIDs))
		finding.TaskIDs = uniqueStrings(feedbackReviewCleanList(finding.TaskIDs))
		finding.SourceRefs = uniqueStrings(feedbackReviewCleanList(finding.SourceRefs))
		finding.Dates = uniqueStrings(feedbackReviewCleanList(finding.Dates))
		finding.severityRank = feedbackReviewSeverityRank(finding.Severity)
		finding.confidenceRank = feedbackReviewConfidenceRank(finding.Confidence)
		finding.ActionType = feedbackReviewActionType(finding)
		if finding.Prevention == "" {
			finding.Prevention = feedbackReviewPrevention(finding)
		}
		finding.ID = feedbackReviewFindingID(finding)
		findings = append(findings, finding)
	}
	sort.SliceStable(findings, func(i, j int) bool {
		if findings[i].severityRank != findings[j].severityRank {
			return findings[i].severityRank > findings[j].severityRank
		}
		if findings[i].confidenceRank != findings[j].confidenceRank {
			return findings[i].confidenceRank > findings[j].confidenceRank
		}
		if findings[i].Frequency != findings[j].Frequency {
			return findings[i].Frequency > findings[j].Frequency
		}
		if findings[i].LatestDate != findings[j].LatestDate {
			return findings[i].LatestDate > findings[j].LatestDate
		}
		return findings[i].Key < findings[j].Key
	})
	if len(findings) > feedbackReviewDefaultMaxFindings {
		findings = findings[:feedbackReviewDefaultMaxFindings]
	}
	return findings
}

func renderFeedbackReviewFacts(b *strings.Builder, packet feedbackReviewPacket) {
	b.WriteString("## Facts\n\n")
	if len(packet.Actionable) == 0 {
		if len(packet.Ignored) == 0 {
			b.WriteString("- No actionable feedback signals were present; no product action recommended.\n\n")
			return
		}
		b.WriteString("- Only low-confidence or one-off noise was present; no product action recommended.\n")
		for _, finding := range packet.Ignored {
			b.WriteString("- Ignored " + feedbackReviewFindingTitle(finding) + ". " + feedbackReviewCitation(finding) + "\n")
		}
		b.WriteString("\n")
		return
	}
	for i, finding := range packet.Actionable {
		b.WriteString(fmt.Sprintf("%d. **%s** - %s. Severity %s, confidence %s, frequency %d, latest %s. %s\n",
			i+1,
			feedbackReviewFindingTitle(finding),
			finding.Summary,
			firstNonEmpty(finding.Severity, "unknown"),
			firstNonEmpty(finding.Confidence, "unknown"),
			finding.Frequency,
			firstNonEmpty(finding.LatestDate, "undated"),
			feedbackReviewCitation(finding),
		))
		if facts := feedbackReviewFindingFacts(finding); len(facts) > 0 {
			b.WriteString("   Facts: " + strings.Join(facts, "; ") + ".\n")
		}
	}
	b.WriteString("\n")
}

func renderFeedbackReviewLikelyCauses(b *strings.Builder, packet feedbackReviewPacket) {
	b.WriteString("## Likely Causes\n\n")
	if len(packet.Actionable) == 0 {
		b.WriteString("- None inferred.\n\n")
		return
	}
	for i, finding := range packet.Actionable {
		b.WriteString(fmt.Sprintf("%d. %s %s\n", i+1, feedbackReviewLikelyCause(finding), feedbackReviewCitation(finding)))
	}
	b.WriteString("\n")
}

func renderFeedbackReviewProposedActions(b *strings.Builder, packet feedbackReviewPacket) {
	b.WriteString("## Proposed Actions\n\n")
	if len(packet.Actionable) == 0 {
		if len(packet.Ignored) == 0 {
			b.WriteString("- None. No product action recommended.\n\n")
			return
		}
		for _, finding := range packet.Ignored {
			b.WriteString("- **ignore-as-noise** - No product work recommended; wait for repetition before creating backlog. Prevents recurrence by avoiding task churn from a low-confidence one-off. " + feedbackReviewCitation(finding) + "\n")
		}
		b.WriteString("\n")
		return
	}
	for i, finding := range packet.Actionable {
		action := firstNonEmpty(finding.Recommendation, feedbackReviewDefaultAction(finding))
		b.WriteString(fmt.Sprintf("%d. **%s** - %s. Prevents recurrence by %s. %s\n",
			i+1,
			finding.ActionType,
			action,
			finding.Prevention,
			feedbackReviewCitation(finding),
		))
	}
	b.WriteString("\n")
}

func renderFeedbackReviewHumanDecisions(b *strings.Builder, packet feedbackReviewPacket) {
	b.WriteString("## Needs Human Decision\n\n")
	var decisions []feedbackReviewFinding
	for _, finding := range packet.Actionable {
		if finding.ActionType == "decision" {
			decisions = append(decisions, finding)
		}
	}
	if len(decisions) == 0 {
		b.WriteString("- None.\n")
		return
	}
	for _, finding := range decisions {
		b.WriteString("- Decide how to handle " + feedbackReviewFindingTitle(finding) + ". " + feedbackReviewCitation(finding) + "\n")
	}
}

func feedbackReviewSignalGroupKey(signal feedbackReviewSignal) string {
	category := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(signal.Category), "-", "_"))
	taskIDs := feedbackReviewTaskIDs(signal)
	if len(taskIDs) > 0 && (category == "acceptance_quality" || category == "review_loop") {
		return "task:" + category + ":" + strings.Join(taskIDs, ",")
	}
	if category == "token_burn" && (len(taskIDs) > 0 || strings.TrimSpace(signal.Attempt) != "") {
		return "task:" + category + ":" + strings.Join(taskIDs, ",") + ":" + strings.TrimSpace(signal.Attempt)
	}
	if key := strings.ToLower(feedbackReviewCleanText(signal.DedupeKey, 120)); key != "" {
		return "dedupe:" + key
	}
	taskPart := strings.Join(taskIDs, ",")
	summary := strings.Join(improveSignificantTokens(signal.Summary+" "+signal.Recommendation), " ")
	if summary == "" {
		summary = feedbackReviewCleanText(signal.Summary, 80)
	}
	return strings.ToLower(strings.Join([]string{signal.Category, taskPart, summary}, "|"))
}

func feedbackReviewSignalLess(left, right feedbackReviewSignal) bool {
	leftSeverity := feedbackReviewSeverityRank(left.Severity)
	rightSeverity := feedbackReviewSeverityRank(right.Severity)
	if leftSeverity != rightSeverity {
		return leftSeverity > rightSeverity
	}
	leftConfidence := feedbackReviewConfidenceRank(left.Confidence)
	rightConfidence := feedbackReviewConfidenceRank(right.Confidence)
	if leftConfidence != rightConfidence {
		return leftConfidence > rightConfidence
	}
	leftFrequency := feedbackReviewSignalFrequency(left)
	rightFrequency := feedbackReviewSignalFrequency(right)
	if leftFrequency != rightFrequency {
		return leftFrequency > rightFrequency
	}
	if feedbackReviewSignalDate(left) != feedbackReviewSignalDate(right) {
		return feedbackReviewSignalDate(left) > feedbackReviewSignalDate(right)
	}
	return feedbackReviewSignalID(left) < feedbackReviewSignalID(right)
}

func feedbackReviewFindingIsNoise(finding feedbackReviewFinding) bool {
	if strings.EqualFold(finding.ActionType, "ignore-as-noise") {
		return true
	}
	category := strings.ToLower(finding.Category)
	return category == "noise" || category == "low_signal" || finding.severityRank <= 2 && finding.confidenceRank <= 1 && finding.Frequency <= 1
}

func feedbackReviewActionType(finding feedbackReviewFinding) string {
	for _, signal := range finding.Signals {
		if action := feedbackReviewNormalizeActionType(signal.ActionType); action != "" {
			return action
		}
	}
	text := strings.ToLower(strings.Join([]string{finding.Category, finding.Summary, finding.LikelyCause, finding.Recommendation}, " "))
	if strings.Contains(text, "decision") || strings.Contains(text, "choose") || strings.Contains(text, "human") || strings.Contains(text, "policy call") {
		return "decision"
	}
	if finding.severityRank <= 2 && finding.confidenceRank <= 1 && finding.Frequency <= 1 || strings.Contains(text, "noise") {
		return "ignore-as-noise"
	}
	switch strings.ToLower(finding.Category) {
	case "acceptance_quality", "acceptance-quality":
		return "acceptance-criteria fix"
	case "cli_friction", "cli-friction":
		return "CLI hint"
	case "workflow_repeat", "workflow-repeat", "environment_setup", "environment-setup":
		return "skill/runbook update"
	case "closeout_churn", "closeout-churn":
		return "guardrail/policy update"
	case "review_loop", "review-loop", "token_burn", "token-burn":
		return "product ticket"
	}
	switch {
	case strings.Contains(text, "guardrail") || strings.Contains(text, "policy"):
		return "guardrail/policy update"
	case strings.Contains(text, "skill") || strings.Contains(text, "runbook"):
		return "skill/runbook update"
	case strings.Contains(text, "cli") || strings.Contains(text, "command") || strings.Contains(text, "flag"):
		return "CLI hint"
	default:
		return "product ticket"
	}
}

func feedbackReviewNormalizeActionType(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	normalized = strings.ReplaceAll(normalized, "_", "-")
	switch normalized {
	case "product-ticket", "ticket":
		return "product ticket"
	case "cli-hint", "hint":
		return "CLI hint"
	case "acceptance-criteria-fix", "acceptance-fix":
		return "acceptance-criteria fix"
	case "skill-runbook-update", "skill/runbook-update", "runbook", "skill":
		return "skill/runbook update"
	case "guardrail-policy-update", "guardrail/policy-update", "policy", "guardrail":
		return "guardrail/policy update"
	case "decision":
		return "decision"
	case "ignore-as-noise", "ignore", "noise":
		return "ignore-as-noise"
	default:
		return ""
	}
}

func feedbackReviewLikelyCause(finding feedbackReviewFinding) string {
	if strings.EqualFold(finding.Category, "acceptance_quality") || strings.EqualFold(finding.Category, "acceptance-quality") {
		return "Likely cause: bad task contract, not implementation alone: acceptance criteria or proof mapping caused the burn."
	}
	if finding.LikelyCause != "" {
		return "Likely cause: " + finding.LikelyCause + "."
	}
	switch strings.ToLower(finding.Category) {
	case "review_loop", "review-loop":
		return "Likely cause: review expectations were not visible early enough, so work looped after handoff."
	case "token_burn", "token-burn":
		return "Likely cause: agents spent context on proof or investigation without a state transition."
	case "cli_friction", "cli-friction":
		return "Likely cause: the CLI made the correct next action harder to discover than the wrong one."
	case "closeout_churn", "closeout-churn":
		return "Likely cause: closeout ownership or proof policy was ambiguous."
	case "workflow_repeat", "workflow-repeat":
		return "Likely cause: repeated workflow knowledge has not been promoted into a runbook or skill."
	case "environment_setup", "environment-setup":
		return "Likely cause: setup requirements were implicit or too late in the workflow."
	default:
		return "Likely cause: repeated product friction, inferred from the cited signal facts."
	}
}

func feedbackReviewDefaultAction(finding feedbackReviewFinding) string {
	switch finding.ActionType {
	case "acceptance-criteria fix":
		return "Tighten the task acceptance criteria and proof mapping before dispatch."
	case "CLI hint":
		return "Add a CLI hint or affordance at the point where agents hit the friction."
	case "skill/runbook update":
		return "Promote the repeated workflow into a narrow skill or runbook."
	case "guardrail/policy update":
		return "Adjust the guardrail or closeout policy so ownership is explicit."
	case "decision":
		return "Make the product call before creating implementation work."
	case "ignore-as-noise":
		return "Ignore this signal unless it repeats with stronger evidence."
	default:
		return "Create a product ticket for the recurring friction."
	}
}

func feedbackReviewPrevention(finding feedbackReviewFinding) string {
	switch finding.ActionType {
	case "acceptance-criteria fix":
		return "requiring observable acceptance criteria and a proof map before the task is runnable"
	case "CLI hint":
		return "surfacing the right command, flag, or next step at the point of confusion"
	case "skill/runbook update":
		return "making the repeated workflow reusable instead of rediscovered in future turns"
	case "guardrail/policy update":
		return "making ownership and stop conditions explicit before closeout"
	case "decision":
		return "recording the product decision before agents implement around an unresolved choice"
	case "ignore-as-noise":
		return "waiting for repeated, cited evidence before creating product work"
	default:
		return "fixing the workflow that repeatedly created the cited friction"
	}
}

func feedbackReviewFindingTitle(finding feedbackReviewFinding) string {
	category := strings.ReplaceAll(firstNonEmpty(finding.Category, "general"), "_", " ")
	if finding.Summary == "" {
		return category
	}
	return category + ": " + finding.Summary
}

func feedbackReviewFindingFacts(finding feedbackReviewFinding) []string {
	var facts []string
	for _, signal := range finding.Signals {
		for _, fact := range signal.ObservedFacts {
			cleaned := feedbackReviewCleanText(fact, 180)
			if cleaned == "" || cleaned == "raw log-like detail omitted" {
				continue
			}
			facts = append(facts, cleaned)
			if len(facts) >= feedbackReviewDefaultMaxFacts {
				return uniqueStrings(facts)
			}
		}
	}
	return uniqueStrings(facts)
}

func feedbackReviewCitation(finding feedbackReviewFinding) string {
	return "Citation: findings " + firstNonEmpty(finding.ID, "n/a") + "; signals " + feedbackReviewList(finding.SignalIDs) + "; sources " + feedbackReviewList(finding.SourceRefs) + "; tasks " + feedbackReviewList(finding.TaskIDs) + "; dates " + feedbackReviewList(finding.Dates) + "."
}

func feedbackReviewFindingID(finding feedbackReviewFinding) string {
	base := strings.Join([]string{
		finding.Key,
		finding.Category,
		strings.Join(finding.SignalIDs, ","),
		strings.Join(finding.SourceRefs, ","),
	}, "|")
	return "FR-" + feedbackSignalHash(base)[:12]
}

func feedbackReviewList(values []string) string {
	values = uniqueStrings(feedbackReviewCleanList(values))
	sort.Strings(values)
	if len(values) == 0 {
		return "n/a"
	}
	if len(values) > 4 {
		return strings.Join(values[:4], ", ") + fmt.Sprintf(", +%d more", len(values)-4)
	}
	return strings.Join(values, ", ")
}

func feedbackReviewSignalID(signal feedbackReviewSignal) string {
	return firstNonEmpty(signal.ID, signal.DedupeKey, filepath.Base(signal.SourcePath))
}

func feedbackReviewSignalDate(signal feedbackReviewSignal) string {
	return feedbackReviewDateOnly(firstNonEmpty(signal.LastSeen, signal.Date))
}

func feedbackReviewSignalFrequency(signal feedbackReviewSignal) int {
	if signal.Frequency > 0 {
		return signal.Frequency
	}
	if signal.Occurrences > 0 {
		return signal.Occurrences
	}
	return 1
}

func feedbackReviewTaskIDs(signal feedbackReviewSignal) []string {
	var values []string
	values = append(values, signal.TaskIDs...)
	values = append(values, signal.Task)
	values = append(values, v7TaskIDPattern.FindAllString(strings.ToUpper(strings.Join([]string{
		signal.Summary,
		signal.Recommendation,
		signal.LikelyCause,
		signal.DedupeKey,
		strings.Join(signal.ObservedFacts, " "),
	}, " ")), -1)...)
	return feedbackReviewCleanList(values)
}

func feedbackReviewSignalInWindow(signal feedbackReviewSignal, sinceDate time.Time, reviewDate string) bool {
	date := feedbackReviewParseDate(feedbackReviewSignalDate(signal))
	if date.IsZero() {
		return true
	}
	if !sinceDate.IsZero() && date.Before(sinceDate) {
		return false
	}
	until := feedbackReviewParseDate(reviewDate)
	if !until.IsZero() && date.After(until) {
		return false
	}
	return true
}

func feedbackReviewSeverityRank(value string) int {
	normalized := strings.ToLower(strings.TrimSpace(value))
	switch normalized {
	case "p0", "critical", "blocker":
		return 5
	case "p1", "high", "major":
		return 4
	case "p2", "medium", "moderate":
		return 3
	case "p3", "low", "minor":
		return 2
	case "info", "informational", "noise", "none":
		return 1
	default:
		return 2
	}
}

func feedbackReviewConfidenceRank(value string) int {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "high", "certain":
		return 3
	case "medium", "med", "probable":
		return 2
	case "low", "weak":
		return 1
	default:
		return 1
	}
}

func feedbackReviewCleanList(values []string) []string {
	var out []string
	for _, value := range values {
		cleaned := feedbackReviewCleanText(value, 240)
		if cleaned != "" {
			out = append(out, cleaned)
		}
	}
	return out
}

func feedbackReviewCleanText(value string, limit int) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")
	var kept []string
	rawLike := false
	for _, line := range strings.Split(value, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "```") || v7RawLogLinePattern.MatchString(line) {
			rawLike = true
			continue
		}
		kept = append(kept, line)
		if len(kept) >= 2 {
			break
		}
	}
	if len(kept) == 0 {
		if rawLike {
			return "raw log-like detail omitted"
		}
		return ""
	}
	cleaned := feedbackCleanValue(strings.Join(kept, "; "))
	if strings.Contains(cleaned, "```") {
		return "raw log-like detail omitted"
	}
	return feedbackShort(cleaned, limit)
}

func feedbackReviewDateOnly(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 10 {
		return value[:10]
	}
	return value
}

func feedbackReviewParseDate(value string) time.Time {
	value = feedbackReviewDateOnly(value)
	if value == "" {
		return time.Time{}
	}
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil {
		return time.Time{}
	}
	return parsed
}

func feedbackReviewMapString(raw map[string]any, keys ...string) string {
	value := feedbackReviewMapValue(raw, keys...)
	return feedbackReviewCleanText(toString(value), 500)
}

func feedbackReviewMapList(raw map[string]any, keys ...string) []string {
	value := feedbackReviewMapValue(raw, keys...)
	return feedbackReviewCleanList(normalizeList(value))
}

func feedbackReviewMapInt(raw map[string]any, keys ...string) int {
	value := feedbackReviewMapValue(raw, keys...)
	switch v := value.(type) {
	case float64:
		return int(v)
	case int:
		return v
	case json.Number:
		n, _ := v.Int64()
		return int(n)
	case string:
		return atoiSafe(v)
	default:
		return atoiSafe(toString(v))
	}
}

func feedbackReviewMapValue(raw map[string]any, keys ...string) any {
	wanted := map[string]bool{}
	for _, key := range keys {
		wanted[feedbackReviewFieldKey(key)] = true
	}
	for key, value := range raw {
		if wanted[feedbackReviewFieldKey(key)] {
			return value
		}
	}
	return nil
}

func feedbackReviewFieldKey(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, "_", "-")
	return value
}

func feedbackReviewListOfMaps(value any) []map[string]any {
	switch v := value.(type) {
	case []any:
		out := make([]map[string]any, 0, len(v))
		for _, item := range v {
			if mapped, ok := item.(map[string]any); ok {
				out = append(out, mapped)
			}
		}
		return out
	case []map[string]any:
		return v
	case map[string]any:
		return []map[string]any{v}
	default:
		return nil
	}
}

func feedbackReviewOccurrenceFacts(raw map[string]any) (int, []string, []string, []string) {
	value := feedbackReviewMapValue(raw, "occurrences")
	var maps []map[string]any
	switch typed := value.(type) {
	case []any, []map[string]any, map[string]any:
		maps = feedbackReviewListOfMaps(typed)
	default:
		return 0, nil, nil, nil
	}
	var vaults []string
	var projects []string
	var sources []string
	for _, occurrence := range maps {
		vaults = append(vaults, feedbackReviewMapString(occurrence, "vault"))
		projects = append(projects, strings.ToUpper(feedbackReviewMapString(occurrence, "project")))
		sources = append(sources, feedbackReviewMapString(occurrence, "source"))
	}
	return len(maps), uniqueStrings(feedbackReviewCleanList(vaults)), uniqueStrings(feedbackReviewCleanList(projects)), uniqueStrings(feedbackReviewCleanList(sources))
}
