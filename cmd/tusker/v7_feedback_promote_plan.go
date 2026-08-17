package main

import (
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const (
	feedbackPromoteDefaultSummaryLimit = 20
	feedbackPromoteModeDryRun          = "dry-run"
	feedbackPromoteModeApply           = "apply"
)

var feedbackPromoteTaskIDPattern = regexp.MustCompile(`\b[A-Z][A-Z0-9]*-T-[0-9]+\b`)

type feedbackPromoteOptions struct {
	VaultPath    string
	RepoRoot     string
	Apply        bool
	SummaryLimit int
	ExistingWork []feedbackPromoteExistingWork
	NowDate      string
	DefaultEpic  string
	DefaultOwner string
	DefaultActor string
}

type feedbackPromoteSource struct {
	Kind            string
	ID              string
	Path            string
	Title           string
	Summary         string
	Friction        string
	ProductIdea     string
	Impact          string
	Severity        string
	DedupeKey       string
	SourceSignal    string
	RelatedTask     string
	AffectedCommand string
	Prevention      string
	OutcomeHint     string
	RepeatCount     int
	Evidence        []feedbackPromoteEvidence
	Tags            []string
}

type feedbackPromoteReviewAction struct {
	ID              string
	Path            string
	Title           string
	Summary         string
	Friction        string
	ProductIdea     string
	Impact          string
	Severity        string
	DedupeKey       string
	SourceSignal    string
	RelatedTask     string
	AffectedCommand string
	Prevention      string
	OutcomeHint     string
	RepeatCount     int
	Evidence        []feedbackPromoteEvidence
	Tags            []string
}

type feedbackPromoteEvidence struct {
	Source string
	Ref    string
	Title  string
	Date   string
}

type feedbackPromoteExistingWork struct {
	ID           string
	Kind         string
	Title        string
	Path         string
	DedupeKeys   []string
	SourceRefs   []string
	RelatedTasks []string
}

type feedbackPromoteDuplicateMatch struct {
	ID     string `json:"id"`
	Kind   string `json:"kind"`
	Path   string `json:"path"`
	Field  string `json:"field"`
	Value  string `json:"value"`
	Reason string `json:"reason"`
}

type feedbackPromotePlan struct {
	Mode     string
	DryRun   bool
	Source   feedbackPromoteSource
	Outcomes []feedbackPromoteOutcome
	Summary  feedbackPromoteSummary
}

type feedbackPromoteOutcome struct {
	Operation          string
	Kind               string
	Title              string
	TargetID           string
	TargetPath         string
	Severity           string
	DedupeKey          string
	SourceRefs         []string
	RelatedTasks       []string
	Prevention         string
	Reasons            []string
	Duplicate          *feedbackPromoteDuplicateMatch
	NeedsHumanDecision bool
	ProposedFields     map[string]any
}

type feedbackPromoteSummary struct {
	Created            int
	Updated            int
	Linked             int
	Skipped            int
	NeedsHumanDecision int
	Total              int
	Shown              int
	Bounded            bool
}

func planFeedbackSignalPromotion(record feedbackRecord, options feedbackPromoteOptions) (feedbackPromotePlan, error) {
	return planFeedbackPromotion(feedbackPromoteSourceFromFeedbackRecord(record), options)
}

func planFeedbackReviewActionPromotion(action feedbackPromoteReviewAction, options feedbackPromoteOptions) (feedbackPromotePlan, error) {
	return planFeedbackPromotion(feedbackPromoteSourceFromReviewAction(action), options)
}

func planFeedbackPromotion(source feedbackPromoteSource, options feedbackPromoteOptions) (feedbackPromotePlan, error) {
	source = normalizeFeedbackPromoteSource(source)
	if source.Title == "" {
		return feedbackPromotePlan{}, tuskerError(errorMissingArg, "feedback promote source requires a title or product idea")
	}
	if len(options.ExistingWork) == 0 && strings.TrimSpace(options.VaultPath) != "" {
		existing, err := collectFeedbackPromoteExistingWork(options.VaultPath)
		if err != nil {
			return feedbackPromotePlan{}, err
		}
		options.ExistingWork = existing
	}

	mode := feedbackPromoteModeDryRun
	if options.Apply {
		mode = feedbackPromoteModeApply
	}
	plan := feedbackPromotePlan{
		Mode:   mode,
		DryRun: mode == feedbackPromoteModeDryRun,
		Source: source,
	}
	outcome := feedbackPromoteOutcomeForSource(source, options)
	plan.Outcomes = []feedbackPromoteOutcome{outcome}
	plan.Summary = summarizeFeedbackPromoteOutcomes(plan.Outcomes, options.SummaryLimit)
	return plan, nil
}

func feedbackPromoteSourceFromFeedbackRecord(record feedbackRecord) feedbackPromoteSource {
	fields := record.Fields
	if fields == nil {
		fields = map[string]string{}
	}
	path := firstNonEmpty(record.RelativePath, record.Path)
	sourceRef := firstNonEmpty(fields["source-signal"], path)
	title := firstNonEmpty(fields["product-idea"], record.Theme, fields["friction"])
	return normalizeFeedbackPromoteSource(feedbackPromoteSource{
		Kind:            "feedback_signal",
		ID:              firstNonEmpty(sourceRef, path),
		Path:            path,
		Title:           title,
		Summary:         strings.Join(feedbackPromoteNonEmptyStrings([]string{fields["context"], fields["friction"], fields["product-idea"], fields["impact"]}), "\n"),
		Friction:        fields["friction"],
		ProductIdea:     fields["product-idea"],
		Impact:          fields["impact"],
		Severity:        firstNonEmpty(record.PriorityHint, fields["priority-hint"]),
		DedupeKey:       fields["dedupe-key"],
		SourceSignal:    sourceRef,
		RelatedTask:     firstTaskID(fields["related"]),
		AffectedCommand: record.AffectedCommand,
		Prevention:      fields["prevention"],
		OutcomeHint:     fields["promote-as"],
		RepeatCount:     feedbackPromoteRepeatCount(fields["repeat-count"], 1),
		Evidence: []feedbackPromoteEvidence{{
			Source: "feedback",
			Ref:    path,
			Title:  title,
			Date:   record.Date,
		}},
	})
}

func feedbackPromoteSourceFromReviewAction(action feedbackPromoteReviewAction) feedbackPromoteSource {
	sourceRef := firstNonEmpty(action.SourceSignal, action.Path, action.ID)
	return normalizeFeedbackPromoteSource(feedbackPromoteSource{
		Kind:            "daily_review_action",
		ID:              firstNonEmpty(action.ID, sourceRef),
		Path:            action.Path,
		Title:           firstNonEmpty(action.Title, action.ProductIdea, action.Friction),
		Summary:         action.Summary,
		Friction:        action.Friction,
		ProductIdea:     action.ProductIdea,
		Impact:          action.Impact,
		Severity:        action.Severity,
		DedupeKey:       action.DedupeKey,
		SourceSignal:    sourceRef,
		RelatedTask:     action.RelatedTask,
		AffectedCommand: action.AffectedCommand,
		Prevention:      action.Prevention,
		OutcomeHint:     action.OutcomeHint,
		RepeatCount:     action.RepeatCount,
		Evidence:        action.Evidence,
		Tags:            action.Tags,
	})
}

func normalizeFeedbackPromoteSource(source feedbackPromoteSource) feedbackPromoteSource {
	source.Kind = firstNonEmpty(feedbackCleanValue(source.Kind), "feedback_signal")
	source.ID = feedbackCleanValue(source.ID)
	source.Path = filepath.ToSlash(strings.TrimSpace(source.Path))
	source.Title = feedbackCleanValue(firstNonEmpty(source.Title, source.ProductIdea, source.Friction, source.Summary))
	source.Summary = feedbackCleanValue(source.Summary)
	source.Friction = feedbackCleanValue(source.Friction)
	source.ProductIdea = feedbackCleanValue(source.ProductIdea)
	source.Impact = feedbackCleanValue(source.Impact)
	source.Severity = normalizeFeedbackPromoteSeverity(source.Severity)
	source.DedupeKey = normalizedFeedbackDedupeKey(source.DedupeKey)
	source.SourceSignal = feedbackCleanValue(firstNonEmpty(source.SourceSignal, source.Path, source.ID))
	source.RelatedTask = firstTaskID(strings.Join([]string{source.RelatedTask, source.Title, source.Summary, source.Friction, source.ProductIdea, source.Impact}, "\n"))
	source.AffectedCommand = feedbackCleanValue(source.AffectedCommand)
	source.Prevention = feedbackCleanValue(source.Prevention)
	source.OutcomeHint = normalizeFeedbackPromoteOutcomeHint(source.OutcomeHint)
	source.Tags = feedbackPromoteUniqueStrings(normalizePromoteTokens(source.Tags))
	if source.RepeatCount < 0 {
		source.RepeatCount = 0
	}
	if len(source.Evidence) > 0 && source.RepeatCount < len(uniqueFeedbackPromoteEvidenceRefs(source.Evidence)) {
		source.RepeatCount = len(uniqueFeedbackPromoteEvidenceRefs(source.Evidence))
	}
	for i := range source.Evidence {
		source.Evidence[i].Source = feedbackCleanValue(source.Evidence[i].Source)
		source.Evidence[i].Ref = filepath.ToSlash(feedbackCleanValue(source.Evidence[i].Ref))
		source.Evidence[i].Title = feedbackCleanValue(source.Evidence[i].Title)
		source.Evidence[i].Date = feedbackCleanValue(source.Evidence[i].Date)
	}
	return source
}

func feedbackPromoteOutcomeForSource(source feedbackPromoteSource, options feedbackPromoteOptions) feedbackPromoteOutcome {
	if match, ok := matchFeedbackPromoteDuplicate(source, options.ExistingWork); ok {
		return feedbackPromoteDuplicateOutcome(source, match)
	}
	if feedbackPromoteNeedsDecision(source) {
		return feedbackPromoteCreateOutcome(source, "decision", []string{"ambiguous, sensitive, legal, security, or product-policy evidence needs an explicit decision"})
	}
	if !feedbackPromoteEligible(source) {
		return feedbackPromoteSkipOutcome(source, "needs repeated evidence or P0/P1 severity before product work is promoted")
	}
	kind := feedbackPromoteTargetKind(source)
	if kind == "skip" {
		return feedbackPromoteSkipOutcome(source, "source requested skip")
	}
	return feedbackPromoteCreateOutcome(source, kind, feedbackPromoteKindReasons(source, kind))
}

func feedbackPromoteDuplicateOutcome(source feedbackPromoteSource, match feedbackPromoteDuplicateMatch) feedbackPromoteOutcome {
	operation := "link"
	if match.Kind == "task" {
		operation = "update"
	}
	reason := "duplicate " + match.Field + " matched existing " + match.Kind
	return feedbackPromoteOutcome{
		Operation:    operation,
		Kind:         match.Kind,
		Title:        firstNonEmpty(matchTitle(match, source), source.Title),
		TargetID:     match.ID,
		TargetPath:   match.Path,
		Severity:     source.Severity,
		DedupeKey:    source.DedupeKey,
		SourceRefs:   feedbackPromoteSourceRefs(source),
		RelatedTasks: feedbackPromoteRelatedTasks(source),
		Prevention:   feedbackPromotePrevention(source),
		Reasons:      []string{reason},
		Duplicate:    &match,
		ProposedFields: map[string]any{
			"source_refs":          feedbackPromoteSourceRefs(source),
			"related_tasks":        feedbackPromoteRelatedTasks(source),
			"prevention_statement": feedbackPromotePrevention(source),
			"promotion_reason":     reason,
		},
	}
}

func feedbackPromoteCreateOutcome(source feedbackPromoteSource, kind string, reasons []string) feedbackPromoteOutcome {
	fields := feedbackPromoteProposedFields(source, kind)
	return feedbackPromoteOutcome{
		Operation:          "create",
		Kind:               kind,
		Title:              feedbackPromoteTitleForKind(source, kind),
		Severity:           source.Severity,
		DedupeKey:          source.DedupeKey,
		SourceRefs:         feedbackPromoteSourceRefs(source),
		RelatedTasks:       feedbackPromoteRelatedTasks(source),
		Prevention:         feedbackPromotePrevention(source),
		Reasons:            reasons,
		NeedsHumanDecision: kind == "decision" || kind == "gate",
		ProposedFields:     fields,
	}
}

func feedbackPromoteSkipOutcome(source feedbackPromoteSource, reason string) feedbackPromoteOutcome {
	return feedbackPromoteOutcome{
		Operation:    "skip",
		Kind:         "skip",
		Title:        source.Title,
		Severity:     source.Severity,
		DedupeKey:    source.DedupeKey,
		SourceRefs:   feedbackPromoteSourceRefs(source),
		RelatedTasks: feedbackPromoteRelatedTasks(source),
		Prevention:   feedbackPromotePrevention(source),
		Reasons:      []string{reason},
		ProposedFields: map[string]any{
			"source_refs":          feedbackPromoteSourceRefs(source),
			"prevention_statement": feedbackPromotePrevention(source),
			"skip_reason":          reason,
		},
	}
}

func feedbackPromoteProposedFields(source feedbackPromoteSource, kind string) map[string]any {
	fields := map[string]any{
		"title":                feedbackPromoteTitleForKind(source, kind),
		"source_refs":          feedbackPromoteSourceRefs(source),
		"related_tasks":        feedbackPromoteRelatedTasks(source),
		"dedupe_key":           feedbackPromoteNullIfEmptyString(source.DedupeKey),
		"prevention_statement": feedbackPromotePrevention(source),
		"severity":             source.Severity,
	}
	switch kind {
	case "task":
		fields["intent"] = firstNonEmpty(source.ProductIdea, source.Title)
		fields["acceptance"] = feedbackPromoteAcceptance(source)
		fields["priority"] = feedbackPromotePriority(source)
		fields["risk"] = feedbackPromoteRisk(source)
	case "decision":
		fields["question"] = feedbackPromoteDecisionQuestion(source)
		fields["suggested_resolution"] = feedbackPromoteSuggestedResolution(source)
		fields["decision_owner"] = "human:product"
	case "gate":
		fields["gate_kind"] = feedbackPromoteGateKind(source)
		fields["owner"] = feedbackPromoteGateOwner(source)
		fields["action"] = feedbackPromoteGateAction(source)
		fields["verification"] = feedbackPromoteGateVerification(source)
		fields["why_agent_cannot"] = "The source evidence names a human, external, environment, credential, or policy dependency."
	case "runbook", "skill":
		fields["draft_kind"] = kind
		fields["goal"] = firstNonEmpty(source.ProductIdea, source.Title)
		fields["supporting_evidence"] = feedbackPromoteEvidenceRefs(source)
	case "cli_proposal":
		fields["affected_command"] = feedbackPromoteNullIfEmptyString(source.AffectedCommand)
		fields["proposal"] = firstNonEmpty(source.ProductIdea, source.Title)
	}
	return fields
}

func collectFeedbackPromoteExistingWork(vaultPath string) ([]feedbackPromoteExistingWork, error) {
	notes, err := listAllNotes(vaultPath)
	if err != nil {
		return nil, err
	}
	var existing []feedbackPromoteExistingWork
	for _, note := range notes {
		kind := normalizeFeedbackPromoteKind(firstNonEmpty(stringField(note.Data, "kind"), stringField(note.Data, "type")))
		if kind == "" || kind == "feedback_signal" {
			continue
		}
		record := feedbackPromoteExistingWork{
			ID:           stringField(note.Data, "id"),
			Kind:         kind,
			Title:        firstNonEmpty(stringField(note.Data, "title"), feedbackPromoteFirstMarkdownHeading(note.Body)),
			Path:         note.RelativePath,
			DedupeKeys:   feedbackPromoteDedupeKeys(note.Data, note.Body),
			SourceRefs:   feedbackPromoteExistingSourceRefs(note.Data, note.Body, note.RelativePath),
			RelatedTasks: feedbackPromoteExistingTaskRefs(note.Data, note.Body),
		}
		if record.ID == "" && record.Title == "" {
			continue
		}
		existing = append(existing, record)
	}
	sort.SliceStable(existing, func(i, j int) bool {
		if existing[i].Kind != existing[j].Kind {
			return existing[i].Kind < existing[j].Kind
		}
		return firstNonEmpty(existing[i].ID, existing[i].Path) < firstNonEmpty(existing[j].ID, existing[j].Path)
	})
	return existing, nil
}

func matchFeedbackPromoteDuplicate(source feedbackPromoteSource, existing []feedbackPromoteExistingWork) (feedbackPromoteDuplicateMatch, bool) {
	source = normalizeFeedbackPromoteSource(source)
	normalizedExisting := append([]feedbackPromoteExistingWork{}, existing...)
	sort.SliceStable(normalizedExisting, func(i, j int) bool {
		return firstNonEmpty(normalizedExisting[i].ID, normalizedExisting[i].Path) < firstNonEmpty(normalizedExisting[j].ID, normalizedExisting[j].Path)
	})
	for _, current := range normalizedExisting {
		current = normalizeFeedbackPromoteExistingWork(current)
		if source.DedupeKey != "" && containsString(current.DedupeKeys, source.DedupeKey) {
			return feedbackPromoteDuplicateMatch{ID: current.ID, Kind: current.Kind, Path: current.Path, Field: "dedupe_key", Value: source.DedupeKey, Reason: "same dedupe key"}, true
		}
	}
	sourceRefs := feedbackPromoteSourceRefs(source)
	for _, current := range normalizedExisting {
		current = normalizeFeedbackPromoteExistingWork(current)
		if value, ok := firstOverlap(sourceRefs, current.SourceRefs); ok {
			return feedbackPromoteDuplicateMatch{ID: current.ID, Kind: current.Kind, Path: current.Path, Field: "source", Value: value, Reason: "same source reference"}, true
		}
	}
	sourceTitle := feedbackPromoteTitleKey(source.Title)
	if sourceTitle != "" {
		// A bare normalized-title match is far too weak on its own: any note in the
		// vault that happens to share a title would silently mark real feedback as
		// already-linked. Only accept it when the existing note is of a kind this
		// promotion could actually target.
		targetKinds := feedbackPromoteCandidateKinds(source)
		for _, current := range normalizedExisting {
			current = normalizeFeedbackPromoteExistingWork(current)
			if !targetKinds[current.Kind] {
				continue
			}
			if feedbackPromoteTitleKey(current.Title) == sourceTitle {
				return feedbackPromoteDuplicateMatch{ID: current.ID, Kind: current.Kind, Path: current.Path, Field: "title", Value: source.Title, Reason: "same normalized title"}, true
			}
		}
	}
	sourceTasks := feedbackPromoteRelatedTasks(source)
	for _, current := range normalizedExisting {
		current = normalizeFeedbackPromoteExistingWork(current)
		if value, ok := firstOverlap(sourceTasks, append(current.RelatedTasks, current.ID)); ok {
			return feedbackPromoteDuplicateMatch{ID: current.ID, Kind: current.Kind, Path: current.Path, Field: "task", Value: value, Reason: "same related task"}, true
		}
	}
	return feedbackPromoteDuplicateMatch{}, false
}

// feedbackPromoteCandidateKinds is the set of note kinds a promotion of this
// source could actually produce, so title-only duplicate matching stays scoped.
func feedbackPromoteCandidateKinds(source feedbackPromoteSource) map[string]bool {
	kinds := map[string]bool{normalizeFeedbackPromoteKind(feedbackPromoteTargetKind(source)): true}
	if feedbackPromoteNeedsDecision(source) {
		kinds["decision"] = true
	}
	delete(kinds, "")
	delete(kinds, "skip")
	return kinds
}

func normalizeFeedbackPromoteExistingWork(work feedbackPromoteExistingWork) feedbackPromoteExistingWork {
	work.ID = feedbackCleanValue(work.ID)
	work.Kind = normalizeFeedbackPromoteKind(work.Kind)
	work.Title = feedbackCleanValue(work.Title)
	work.Path = filepath.ToSlash(strings.TrimSpace(work.Path))
	work.DedupeKeys = feedbackPromoteUniqueStrings(normalizePromoteDedupeKeys(work.DedupeKeys))
	work.SourceRefs = feedbackPromoteUniqueStrings(normalizePromoteRefs(work.SourceRefs))
	work.RelatedTasks = feedbackPromoteUniqueStrings(feedbackPromoteTaskIDs(strings.Join(work.RelatedTasks, "\n")))
	return work
}

func feedbackPromoteDedupeKeys(data map[string]any, body string) []string {
	var values []string
	for _, key := range []string{"dedupe_key", "dedupe-key", "dedupe", "feedback_dedupe_key", "feedback-dedupe-key"} {
		values = append(values, normalizeList(data[key])...)
	}
	values = append(values, feedbackPromoteBodyValues(body, "dedupe-key")...)
	values = append(values, feedbackPromoteBodyValues(body, "dedupe_key")...)
	return normalizePromoteDedupeKeys(values)
}

func feedbackPromoteExistingSourceRefs(data map[string]any, body, relativePath string) []string {
	var values []string
	for _, key := range []string{"source", "source_ref", "source_refs", "source_signal", "source_feedback", "source_review", "feedback_source", "feedback_sources"} {
		values = append(values, normalizeList(data[key])...)
	}
	for _, label := range []string{"source", "source-ref", "source-signal", "source-feedback", "source-review"} {
		values = append(values, feedbackPromoteBodyValues(body, label)...)
	}
	values = append(values, relativePath)
	return normalizePromoteRefs(values)
}

func feedbackPromoteExistingTaskRefs(data map[string]any, body string) []string {
	var values []string
	for _, key := range []string{"task", "target", "blocks", "blocked_by", "related", "related_tasks", "related_task", "tasks"} {
		values = append(values, normalizeList(data[key])...)
	}
	values = append(values, feedbackPromoteTaskIDs(body)...)
	return feedbackPromoteTaskIDs(strings.Join(values, "\n"))
}

func feedbackPromoteBodyValues(body, label string) []string {
	normalizedLabel := strings.ToLower(strings.ReplaceAll(label, "_", "-"))
	var values []string
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		trimmed = strings.TrimPrefix(trimmed, "- ")
		trimmed = strings.TrimPrefix(trimmed, "* ")
		key, value, ok := strings.Cut(trimmed, ":")
		if !ok {
			continue
		}
		key = strings.ToLower(strings.Trim(strings.ReplaceAll(key, "_", "-"), "`* "))
		if key == normalizedLabel {
			values = append(values, feedbackCleanValue(value))
		}
	}
	return values
}

func feedbackPromoteTargetKind(source feedbackPromoteSource) string {
	if source.OutcomeHint != "" {
		return source.OutcomeHint
	}
	text := feedbackPromoteDecisionText(source)
	switch {
	case feedbackPromoteGateLike(text):
		return "gate"
	case strings.Contains(text, "runbook"):
		return "runbook"
	case strings.Contains(text, "skill") || strings.Contains(text, "workflow") || strings.Contains(text, "playbook"):
		return "skill"
	case source.AffectedCommand != "" && source.AffectedCommand != "n/a" || strings.Contains(text, " cli ") || strings.Contains(text, " command ") || strings.Contains(text, " flag "):
		return "cli_proposal"
	default:
		return "task"
	}
}

func feedbackPromoteNeedsDecision(source feedbackPromoteSource) bool {
	text := feedbackPromoteDecisionText(source)
	for _, tag := range source.Tags {
		text += " " + strings.ToLower(tag)
	}
	for _, marker := range []string{
		"ambiguous", "unclear", "conflicting", "conflict", "choose", "decide", "decision",
		"legal", "license", "compliance", "privacy", "policy", "product policy", "security",
		"sensitive", "customer data", "terms",
	} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func feedbackPromoteEligible(source feedbackPromoteSource) bool {
	if source.RepeatCount >= 2 {
		return true
	}
	switch source.Severity {
	case "P0", "P1":
		return true
	default:
		return false
	}
}

func feedbackPromoteKindReasons(source feedbackPromoteSource, kind string) []string {
	var reasons []string
	if source.RepeatCount >= 2 {
		reasons = append(reasons, fmt.Sprintf("repeated evidence count is %d", source.RepeatCount))
	}
	if source.Severity == "P0" || source.Severity == "P1" {
		reasons = append(reasons, source.Severity+" severity may promote without repeated evidence")
	}
	switch kind {
	case "gate":
		reasons = append(reasons, "evidence names a human, external, environment, credential, or access dependency")
	case "runbook", "skill":
		reasons = append(reasons, "evidence describes repeatable workflow packaging")
	case "cli_proposal":
		reasons = append(reasons, "evidence names a CLI command or flag improvement")
	default:
		reasons = append(reasons, "eligible product work")
	}
	return feedbackPromoteUniqueStrings(reasons)
}

func feedbackPromotePrevention(source feedbackPromoteSource) string {
	if strings.TrimSpace(source.Prevention) != "" {
		return source.Prevention
	}
	change := firstNonEmpty(source.ProductIdea, source.Title)
	friction := firstNonEmpty(source.Friction, source.Summary)
	switch {
	case change != "" && friction != "":
		return "Prevent recurrence by " + change + " so " + strings.ToLower(strings.TrimSuffix(friction, ".")) + " does not recur."
	case change != "":
		return "Prevent recurrence by " + change + "."
	default:
		return "Prevent recurrence by turning this feedback into an explicit owner, outcome, and verification path."
	}
}

func feedbackPromoteSourceRefs(source feedbackPromoteSource) []string {
	var refs []string
	for _, value := range []string{source.SourceSignal, source.Path, source.ID} {
		if strings.TrimSpace(value) != "" {
			refs = append(refs, value)
		}
	}
	refs = append(refs, feedbackPromoteEvidenceRefs(source)...)
	return feedbackPromoteUniqueStrings(normalizePromoteRefs(refs))
}

func feedbackPromoteEvidenceRefs(source feedbackPromoteSource) []string {
	var refs []string
	for _, item := range source.Evidence {
		if item.Ref != "" {
			refs = append(refs, item.Ref)
		}
	}
	return feedbackPromoteUniqueStrings(normalizePromoteRefs(refs))
}

func feedbackPromoteRelatedTasks(source feedbackPromoteSource) []string {
	text := strings.Join([]string{source.RelatedTask, source.Title, source.Summary, source.Friction, source.ProductIdea, source.Impact}, "\n")
	return feedbackPromoteTaskIDs(text)
}

func feedbackPromoteTaskIDs(text string) []string {
	return feedbackPromoteUniqueStrings(feedbackPromoteTaskIDPattern.FindAllString(strings.ToUpper(text), -1))
}

func firstTaskID(text string) string {
	ids := feedbackPromoteTaskIDs(text)
	if len(ids) == 0 {
		return ""
	}
	return ids[0]
}

func normalizeFeedbackPromoteSeverity(value string) string {
	value = strings.ToUpper(strings.TrimSpace(value))
	if value == "" {
		return "P3"
	}
	if strings.HasPrefix(value, "P") && len(value) >= 2 {
		switch value[:2] {
		case "P0", "P1", "P2", "P3":
			return value[:2]
		}
	}
	switch strings.ToLower(value) {
	case "critical":
		return "P0"
	case "high":
		return "P1"
	case "medium":
		return "P2"
	case "low":
		return "P3"
	default:
		return "P3"
	}
}

func normalizeFeedbackPromoteOutcomeHint(value string) string {
	value = strings.ToLower(feedbackCleanValue(value))
	value = strings.ReplaceAll(value, "-", "_")
	value = strings.ReplaceAll(value, " ", "_")
	switch value {
	case "", "auto":
		return ""
	case "task", "create_task":
		return "task"
	case "decision", "create_decision":
		return "decision"
	case "gate", "create_gate", "human_gate", "external_gate":
		return "gate"
	case "runbook", "runbook_draft":
		return "runbook"
	case "skill", "skill_draft":
		return "skill"
	case "cli", "cli_proposal", "proposal", "command_proposal":
		return "cli_proposal"
	case "skip":
		return "skip"
	default:
		return ""
	}
}

func normalizeFeedbackPromoteKind(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, "-", "_")
	switch value {
	case "task", "story":
		return "task"
	case "decision":
		return "decision"
	case "gate":
		return "gate"
	case "proposal", "inbox":
		return "proposal"
	case "doc", "runbook":
		return "runbook"
	case "skill":
		return "skill"
	default:
		return value
	}
}

func normalizePromoteRefs(values []string) []string {
	var out []string
	for _, value := range values {
		value = filepath.ToSlash(feedbackCleanValue(value))
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}

func normalizePromoteDedupeKeys(values []string) []string {
	var out []string
	for _, value := range values {
		if key := normalizedFeedbackDedupeKey(value); key != "" {
			out = append(out, key)
		}
	}
	return out
}

func normalizePromoteTokens(values []string) []string {
	var out []string
	for _, value := range values {
		value = strings.ToLower(feedbackCleanValue(value))
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}

func uniqueFeedbackPromoteEvidenceRefs(items []feedbackPromoteEvidence) []string {
	var refs []string
	for _, item := range items {
		ref := firstNonEmpty(item.Ref, item.Title, item.Source)
		if strings.TrimSpace(ref) != "" {
			refs = append(refs, ref)
		}
	}
	return feedbackPromoteUniqueStrings(normalizePromoteRefs(refs))
}

func feedbackPromoteRepeatCount(value string, fallbackValue int) int {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallbackValue
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 0 {
		return fallbackValue
	}
	return parsed
}

func feedbackPromoteTitleKey(value string) string {
	tokens := feedbackPromoteSignificantTokens(value)
	if len(tokens) == 0 {
		return ""
	}
	return strings.Join(tokens, " ")
}

func firstOverlap(left, right []string) (string, bool) {
	seen := map[string]string{}
	for _, value := range right {
		key := strings.ToLower(feedbackCleanValue(value))
		if key != "" {
			seen[key] = value
		}
	}
	for _, value := range left {
		key := strings.ToLower(feedbackCleanValue(value))
		if original, ok := seen[key]; ok {
			return original, true
		}
	}
	return "", false
}

func feedbackPromoteDecisionText(source feedbackPromoteSource) string {
	return strings.ToLower(strings.Join([]string{
		source.Title,
		source.Summary,
		source.Friction,
		source.ProductIdea,
		source.Impact,
		source.AffectedCommand,
		source.OutcomeHint,
	}, "\n"))
}

func feedbackPromoteGateLike(text string) bool {
	for _, marker := range []string{"human", "external", "manual", "credential", "access", "approval", "account", "environment", "device", "release"} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func feedbackPromoteTitleForKind(source feedbackPromoteSource, kind string) string {
	title := firstNonEmpty(source.ProductIdea, source.Title)
	switch kind {
	case "decision":
		return "Decide: " + strings.TrimPrefix(title, "Decide: ")
	case "gate":
		return "Resolve: " + strings.TrimPrefix(title, "Resolve: ")
	case "runbook":
		return "Draft runbook: " + strings.TrimPrefix(title, "Draft runbook: ")
	case "skill":
		return "Draft skill: " + strings.TrimPrefix(title, "Draft skill: ")
	case "cli_proposal":
		return "Propose CLI change: " + strings.TrimPrefix(title, "Propose CLI change: ")
	default:
		return title
	}
}

func feedbackPromoteAcceptance(source feedbackPromoteSource) string {
	return "The promoted work prevents recurrence: " + feedbackPromotePrevention(source)
}

func feedbackPromotePriority(source feedbackPromoteSource) string {
	switch source.Severity {
	case "P0":
		return "p0"
	case "P1":
		return "p1"
	case "P2":
		return "p2"
	default:
		return "p3"
	}
}

func feedbackPromoteRisk(source feedbackPromoteSource) string {
	switch source.Severity {
	case "P0":
		return "critical"
	case "P1":
		return "high"
	case "P2":
		return "medium"
	default:
		return "low"
	}
}

func feedbackPromoteDecisionQuestion(source feedbackPromoteSource) string {
	return "How should Tusker handle this feedback safely: " + firstNonEmpty(source.ProductIdea, source.Title) + "?"
}

func feedbackPromoteSuggestedResolution(source feedbackPromoteSource) string {
	return firstNonEmpty(source.ProductIdea, "Record a narrow product decision before creating implementation work.")
}

func feedbackPromoteGateKind(source feedbackPromoteSource) string {
	text := feedbackPromoteDecisionText(source)
	switch {
	case strings.Contains(text, "credential") || strings.Contains(text, "token") || strings.Contains(text, "account") || strings.Contains(text, "access"):
		return "auth"
	case strings.Contains(text, "external") || strings.Contains(text, "provider"):
		return "external_service"
	case strings.Contains(text, "release"):
		return "release"
	default:
		return "decision"
	}
}

func feedbackPromoteGateOwner(source feedbackPromoteSource) string {
	text := feedbackPromoteDecisionText(source)
	if strings.Contains(text, "external") || strings.Contains(text, "provider") {
		return "external:provider"
	}
	return "human:product"
}

func feedbackPromoteGateAction(source feedbackPromoteSource) string {
	return firstNonEmpty(source.ProductIdea, source.Title, "Resolve the feedback promotion gate.")
}

func feedbackPromoteGateVerification(source feedbackPromoteSource) string {
	return "Owner confirms the prevention path is valid: " + feedbackPromotePrevention(source)
}

func summarizeFeedbackPromoteOutcomes(outcomes []feedbackPromoteOutcome, limit int) feedbackPromoteSummary {
	if limit <= 0 {
		limit = feedbackPromoteDefaultSummaryLimit
	}
	summary := feedbackPromoteSummary{Total: len(outcomes)}
	for _, outcome := range outcomes {
		switch outcome.Operation {
		case "create":
			summary.Created++
		case "update":
			summary.Updated++
		case "link":
			summary.Linked++
		case "skip":
			summary.Skipped++
		}
		if outcome.NeedsHumanDecision {
			summary.NeedsHumanDecision++
		}
	}
	if len(outcomes) > limit {
		summary.Shown = limit
		summary.Bounded = true
	} else {
		summary.Shown = len(outcomes)
	}
	return summary
}

func renderFeedbackPromotePlanMarkdown(plan feedbackPromotePlan, limit int) string {
	if limit <= 0 {
		limit = feedbackPromoteDefaultSummaryLimit
	}
	summary := summarizeFeedbackPromoteOutcomes(plan.Outcomes, limit)
	var b strings.Builder
	b.WriteString("# Feedback Promotion Plan\n\n")
	b.WriteString("- Mode: " + plan.Mode + "\n")
	b.WriteString(fmt.Sprintf("- Summary: created=%d updated=%d linked=%d skipped=%d needs-human-decision=%d\n\n", summary.Created, summary.Updated, summary.Linked, summary.Skipped, summary.NeedsHumanDecision))
	b.WriteString("| Outcome | Kind | Target | Reason | Prevention |\n")
	b.WriteString("|---|---|---|---|---|\n")
	for i, outcome := range plan.Outcomes {
		if i >= limit {
			break
		}
		target := firstNonEmpty(outcome.TargetID, outcome.TargetPath, outcome.Title)
		b.WriteString("| " + feedbackPromoteMarkdownCell(outcome.Operation) + " | " + feedbackPromoteMarkdownCell(outcome.Kind) + " | " + feedbackPromoteMarkdownCell(target) + " | " + feedbackPromoteMarkdownCell(strings.Join(outcome.Reasons, "; ")) + " | " + feedbackPromoteMarkdownCell(outcome.Prevention) + " |\n")
	}
	if summary.Bounded {
		b.WriteString(fmt.Sprintf("\n_%d more outcomes omitted from summary._\n", summary.Total-summary.Shown))
	}
	return b.String()
}

func matchTitle(match feedbackPromoteDuplicateMatch, source feedbackPromoteSource) string {
	if match.ID != "" {
		return match.ID
	}
	return source.Title
}

func feedbackPromoteUniqueStrings(values []string) []string {
	seen := map[string]bool{}
	var out []string
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

func feedbackPromoteNonEmptyStrings(values []string) []string {
	var out []string
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			out = append(out, value)
		}
	}
	return out
}

func feedbackPromoteSignificantTokens(text string) []string {
	stopwords := map[string]bool{
		"a": true, "add": true, "after": true, "and": true, "as": true, "by": true, "for": true, "from": true,
		"in": true, "into": true, "of": true, "on": true, "or": true, "the": true, "to": true, "with": true,
	}
	words := regexp.MustCompile(`[a-z0-9][a-z0-9-]*`).FindAllString(strings.ToLower(strings.ReplaceAll(text, "-", " ")), -1)
	var out []string
	for _, word := range words {
		word = strings.Trim(word, "-")
		if len(word) < 3 || stopwords[word] || strings.HasPrefix(word, "2026") {
			continue
		}
		if matched := feedbackPromoteTaskIDPattern.MatchString(strings.ToUpper(word)); matched {
			continue
		}
		out = append(out, word)
	}
	return feedbackPromoteUniqueStrings(out)
}

func feedbackPromoteFirstMarkdownHeading(body string) string {
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "# ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "# "))
		}
	}
	return ""
}

func feedbackPromoteMarkdownCell(value string) string {
	value = strings.ReplaceAll(value, "\n", " ")
	value = strings.ReplaceAll(value, "|", "\\|")
	return strings.TrimSpace(value)
}

func feedbackPromoteNullIfEmptyString(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}
