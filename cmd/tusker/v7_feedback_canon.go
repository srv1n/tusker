package main

import (
	"fmt"
	"hash/fnv"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	feedbackCandidateDefaultThreshold = 3
	feedbackCanonEntriesHeading       = "## Canon Entries"
)

var feedbackLessonWordPattern = regexp.MustCompile(`[a-z0-9][a-z0-9-]*`)

type feedbackLessonCandidate struct {
	ID              string
	Key             string
	Topic           string
	Lesson          string
	SourceNotes     []string
	SourceRepos     []string
	RecurrenceCount int
	DateSpan        string
	Records         []feedbackRecord
}

type feedbackCanonEntry struct {
	ID              string
	Class           string
	Status          string
	Topic           string
	Lesson          string
	SourceNotes     []string
	SourceRepos     []string
	RecurrenceCount int
	DateSpan        string
	PromotedAt      string
	Supersedes      []string
	SupersededBy    string
}

func feedbackCandidatesCmd(args Args) error {
	threshold := feedbackCandidateThreshold(args)
	candidates, err := buildFeedbackCandidates(args)
	if err != nil {
		return err
	}
	if args.Bool("json") {
		items := make([]map[string]any, 0, len(candidates))
		for _, candidate := range candidates {
			items = append(items, feedbackCandidateJSON(candidate))
		}
		emitJSON(map[string]any{"ok": true, "threshold": threshold, "count": len(candidates), "candidates": items})
		return nil
	}
	fmt.Print(renderFeedbackCandidatesMarkdown(candidates, threshold))
	return nil
}

func buildFeedbackCandidates(args Args) ([]feedbackLessonCandidate, error) {
	threshold := feedbackCandidateThreshold(args)
	targets, err := feedbackDigestTargets(args)
	if err != nil {
		return nil, err
	}
	sinceDate := time.Time{}
	if since := strings.TrimSpace(args.String("since")); since != "" {
		parsed, parseErr := time.Parse("2006-01-02", since)
		if parseErr != nil {
			return nil, tuskerError(errorInvalidArg, "feedback candidates --since must be YYYY-MM-DD: "+since)
		}
		sinceDate = parsed
	}
	var records []feedbackRecord
	for _, target := range targets {
		current, err := feedbackRecordsForVault(target.vaultPath, target.repoRoot, sinceDate)
		if err != nil {
			return nil, err
		}
		records = append(records, current...)
	}
	return feedbackCandidatesFromRecords(records, threshold), nil
}

func feedbackCandidateThreshold(args Args) int {
	if args != nil {
		if threshold := atoiSafe(args.String("threshold")); threshold > 0 {
			return threshold
		}
	}
	return feedbackCandidateDefaultThreshold
}

func feedbackCandidatesFromRecords(records []feedbackRecord, threshold int) []feedbackLessonCandidate {
	if threshold <= 0 {
		threshold = feedbackCandidateDefaultThreshold
	}
	groups := map[string][]feedbackRecord{}
	for _, record := range records {
		if len(record.Issues) > 0 {
			continue
		}
		key := feedbackCandidateGroupKey(record)
		if key == "" {
			continue
		}
		groups[key] = append(groups[key], record)
	}
	var candidates []feedbackLessonCandidate
	for key, group := range groups {
		group = distinctFeedbackCandidateRecords(group)
		if len(group) < threshold {
			continue
		}
		sort.SliceStable(group, func(i, j int) bool {
			if group[i].Date != group[j].Date {
				return group[i].Date < group[j].Date
			}
			return feedbackCandidateSourceLabel(group[i]) < feedbackCandidateSourceLabel(group[j])
		})
		candidate := feedbackLessonCandidate{
			Key:             key,
			Topic:           feedbackCandidateTopic(group),
			Lesson:          feedbackCandidateLesson(group),
			Records:         group,
			RecurrenceCount: len(group),
		}
		candidate.ID = feedbackLessonCandidateID(key, candidate.Topic)
		candidate.SourceNotes = feedbackCandidateSourceNotes(group)
		candidate.SourceRepos = feedbackCandidateSourceRepos(group)
		candidate.DateSpan = feedbackCandidateDateSpan(group)
		candidates = append(candidates, candidate)
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].RecurrenceCount != candidates[j].RecurrenceCount {
			return candidates[i].RecurrenceCount > candidates[j].RecurrenceCount
		}
		if candidates[i].DateSpan != candidates[j].DateSpan {
			return candidates[i].DateSpan < candidates[j].DateSpan
		}
		return candidates[i].ID < candidates[j].ID
	})
	return candidates
}

func feedbackCandidateGroupKey(record feedbackRecord) string {
	if key := normalizedFeedbackDedupeKey(record.Fields["dedupe-key"]); key != "" {
		return "dedupe:" + key
	}
	if fingerprint := feedbackLessonFingerprint(record.Fields["friction"]); fingerprint != "" {
		return "friction:" + fingerprint
	}
	if fingerprint := feedbackLessonFingerprint(record.Theme); fingerprint != "" {
		return "theme:" + fingerprint
	}
	return ""
}

func feedbackLessonFingerprint(value string) string {
	words := feedbackLessonWordPattern.FindAllString(strings.ToLower(value), -1)
	stop := map[string]bool{
		"a": true, "an": true, "and": true, "are": true, "as": true, "at": true, "be": true,
		"by": true, "for": true, "from": true, "in": true, "into": true, "is": true,
		"it": true, "of": true, "on": true, "or": true, "that": true, "the": true,
		"this": true, "to": true, "with": true,
	}
	seen := map[string]bool{}
	var kept []string
	for _, word := range words {
		word = strings.Trim(word, "-")
		if len(word) < 3 || stop[word] || seen[word] {
			continue
		}
		seen[word] = true
		kept = append(kept, word)
	}
	sort.Strings(kept)
	if len(kept) > 12 {
		kept = kept[:12]
	}
	return strings.Join(kept, "-")
}

func distinctFeedbackCandidateRecords(records []feedbackRecord) []feedbackRecord {
	seen := map[string]bool{}
	var out []feedbackRecord
	for _, record := range records {
		key := feedbackCandidateSourceLabel(record)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, record)
	}
	return out
}

func feedbackCandidateTopic(records []feedbackRecord) string {
	for _, record := range records {
		if topic := firstNonEmpty(record.Fields["theme"], record.Theme); topic != "" {
			return topic
		}
	}
	for _, record := range records {
		if topic := firstNonEmpty(record.Fields["product-idea"], record.Fields["friction"]); topic != "" {
			return feedbackShort(topic, 120)
		}
	}
	return "recurring feedback"
}

func feedbackCandidateLesson(records []feedbackRecord) string {
	for _, record := range records {
		if lesson := firstNonEmpty(record.Fields["product-idea"], record.Fields["friction"]); lesson != "" {
			return lesson
		}
	}
	return "Promote recurring feedback into domain canon."
}

func feedbackLessonCandidateID(key, topic string) string {
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(key))
	return feedbackSlug(topic, "lesson") + "-" + fmt.Sprintf("%08x", hash.Sum32())
}

func feedbackCandidateSourceNotes(records []feedbackRecord) []string {
	var notes []string
	for _, record := range records {
		notes = append(notes, feedbackCandidateSourceLabel(record))
	}
	return feedbackPromoteUniqueStrings(notes)
}

func feedbackCandidateSourceRepos(records []feedbackRecord) []string {
	var repos []string
	for _, record := range records {
		repos = append(repos, firstNonEmpty(record.SourceRepo, filepath.Base(record.RepoRoot)))
	}
	sort.Strings(repos)
	return feedbackPromoteUniqueStrings(repos)
}

func feedbackCandidateDateSpan(records []feedbackRecord) string {
	var dates []string
	for _, record := range records {
		if record.Date != "" {
			dates = append(dates, record.Date)
		}
	}
	sort.Strings(dates)
	dates = feedbackPromoteUniqueStrings(dates)
	if len(dates) == 0 {
		return ""
	}
	if len(dates) == 1 {
		return dates[0]
	}
	return dates[0] + ".." + dates[len(dates)-1]
}

func feedbackCandidateSourceLabel(record feedbackRecord) string {
	repo := firstNonEmpty(record.SourceRepo, filepath.Base(record.RepoRoot))
	if repo == "" {
		return record.RelativePath
	}
	return repo + ":" + record.RelativePath
}

func renderFeedbackCandidatesMarkdown(candidates []feedbackLessonCandidate, threshold int) string {
	var b strings.Builder
	b.WriteString("# Feedback Promotion Candidates\n\n")
	b.WriteString(fmt.Sprintf("- Threshold: %d distinct notes\n", threshold))
	b.WriteString(fmt.Sprintf("- Candidates: %d\n\n", len(candidates)))
	if len(candidates) == 0 {
		b.WriteString("_No promotion candidates met the recurrence threshold._\n")
		return b.String()
	}
	b.WriteString("| Candidate | Recurrence | Date span | Repos | Topic |\n")
	b.WriteString("|---|---:|---|---|---|\n")
	for _, candidate := range candidates {
		b.WriteString("| `" + candidate.ID + "` | " + strconv.Itoa(candidate.RecurrenceCount) + " | " + feedbackPromoteMarkdownCell(candidate.DateSpan) + " | " + feedbackPromoteMarkdownCell(strings.Join(candidate.SourceRepos, ", ")) + " | " + feedbackPromoteMarkdownCell(candidate.Topic) + " |\n")
	}
	for _, candidate := range candidates {
		b.WriteString("\n## " + candidate.ID + "\n\n")
		b.WriteString("- key: " + candidate.Key + "\n")
		b.WriteString("- lesson: " + candidate.Lesson + "\n")
		b.WriteString("- source notes:\n")
		for _, note := range candidate.SourceNotes {
			b.WriteString("  - " + note + "\n")
		}
	}
	return b.String()
}

func feedbackCandidateJSON(candidate feedbackLessonCandidate) map[string]any {
	return map[string]any{
		"id":               candidate.ID,
		"key":              candidate.Key,
		"topic":            candidate.Topic,
		"lesson":           candidate.Lesson,
		"source_notes":     candidate.SourceNotes,
		"source_repos":     candidate.SourceRepos,
		"recurrence_count": candidate.RecurrenceCount,
		"date_span":        candidate.DateSpan,
	}
}

func feedbackPromoteCanonRequested(args Args) bool {
	for _, key := range []string{"candidate", "domain", "class", "sources", "source-notes", "lesson", "topic", "supersedes"} {
		if strings.TrimSpace(args.String(key)) != "" {
			return true
		}
	}
	return false
}

func feedbackPromoteCanonCmd(args Args) error {
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	domain := strings.TrimSpace(args.String("domain"))
	if domain == "" {
		return tuskerError(errorMissingArg, "feedback promote canon requires --domain <domain>", withHint("choose the target domain canon explicitly; candidates never auto-promote"))
	}
	class := normalizeFeedbackCanonClass(args.String("class"))
	if class == "" {
		return tuskerError(errorMissingArg, "feedback promote canon requires --class <prohibition|pattern|preference>", withHint("negative lessons should usually use --class prohibition"))
	}
	candidate, err := feedbackPromoteCanonCandidate(vaultPath, args)
	if err != nil {
		return err
	}
	entry, err := applyFeedbackCanonPromotion(vaultPath, domain, class, candidate, args)
	if err != nil {
		return err
	}
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": true, "entry": feedbackCanonEntryJSON(entry), "domain": domain})
		return nil
	}
	if !args.Bool("quiet") {
		fmt.Printf("Promoted feedback candidate %s to %s canon entry %s\n", candidate.ID, domain, entry.ID)
	}
	return nil
}

func normalizeFeedbackCanonClass(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, "-", "_")
	switch value {
	case "prohibition", "pattern", "preference":
		return value
	default:
		return ""
	}
}

func feedbackPromoteCanonCandidate(vaultPath string, args Args) (feedbackLessonCandidate, error) {
	if sources := splitFeedbackList(firstNonEmpty(args.String("sources"), args.String("source-notes"))); len(sources) > 0 {
		return feedbackCandidateFromSourcePaths(vaultPath, sources)
	}
	ref := strings.TrimSpace(firstNonEmpty(args.String("candidate"), args.String("_pos1")))
	if ref == "" {
		return feedbackLessonCandidate{}, tuskerError(errorMissingArg, "feedback promote canon requires --candidate <id> or --sources <path[,path...]>", withHint("run `tusker feedback candidates` to list promotable lessons"))
	}
	candidates, err := buildFeedbackCandidates(args)
	if err != nil {
		return feedbackLessonCandidate{}, err
	}
	for _, candidate := range candidates {
		if candidate.ID == ref || candidate.Key == ref {
			return candidate, nil
		}
	}
	return feedbackLessonCandidate{}, tuskerError(errorNotFound, "feedback promotion candidate not found: "+ref, withHint("run `tusker feedback candidates --threshold "+strconv.Itoa(feedbackCandidateThreshold(args))+"` and pass the shown candidate id"))
}

func feedbackCandidateFromSourcePaths(vaultPath string, sources []string) (feedbackLessonCandidate, error) {
	repoRoot := filepath.Dir(vaultPath)
	var records []feedbackRecord
	for _, source := range sources {
		source = strings.TrimSpace(source)
		if source == "" {
			continue
		}
		path := source
		if !filepath.IsAbs(path) {
			path = filepath.Join(vaultPath, filepath.FromSlash(strings.TrimPrefix(source, filepath.Base(vaultPath)+"/")))
		}
		if !fileExists(path) {
			return feedbackLessonCandidate{}, tuskerError(errorNotFound, "feedback source note not found: "+source, withPath(path))
		}
		records = append(records, parseFeedbackRecord(path, vaultPath, repoRoot))
	}
	if len(records) == 0 {
		return feedbackLessonCandidate{}, tuskerError(errorMissingArg, "feedback promote canon requires at least one source note")
	}
	key := feedbackCandidateGroupKey(records[0])
	if key == "" {
		key = "sources:" + feedbackLessonFingerprint(strings.Join(sources, " "))
	}
	candidate := feedbackLessonCandidate{
		Key:             key,
		Topic:           feedbackCandidateTopic(records),
		Lesson:          feedbackCandidateLesson(records),
		SourceNotes:     feedbackCandidateSourceNotes(records),
		SourceRepos:     feedbackCandidateSourceRepos(records),
		RecurrenceCount: len(distinctFeedbackCandidateRecords(records)),
		DateSpan:        feedbackCandidateDateSpan(records),
		Records:         records,
	}
	candidate.ID = feedbackLessonCandidateID(candidate.Key, candidate.Topic)
	return candidate, nil
}

func applyFeedbackCanonPromotion(vaultPath, domain, class string, candidate feedbackLessonCandidate, args Args) (feedbackCanonEntry, error) {
	canonPath := filepath.Join(vaultPath, "knowledge", "domains", domain, "CANON.md")
	if !fileExists(canonPath) {
		return feedbackCanonEntry{}, tuskerError(errorNotFound, "domain canon not found: "+domain, withPath(canonPath))
	}
	data, body, err := parseFrontmatterMustRead(canonPath)
	if err != nil {
		return feedbackCanonEntry{}, err
	}
	entries := feedbackCanonEntriesFromBody(body)
	date := firstNonEmpty(args.String("date"), todayISO())
	topic := feedbackCleanValue(firstNonEmpty(args.String("topic"), candidate.Topic))
	lesson := feedbackCleanValue(firstNonEmpty(args.String("lesson"), candidate.Lesson))
	entry := feedbackCanonEntry{
		ID:              uniqueFeedbackCanonEntryID(entries, date, topic, lesson),
		Class:           class,
		Status:          "current",
		Topic:           topic,
		Lesson:          lesson,
		SourceNotes:     candidate.SourceNotes,
		SourceRepos:     candidate.SourceRepos,
		RecurrenceCount: candidate.RecurrenceCount,
		DateSpan:        candidate.DateSpan,
		PromotedAt:      date,
		Supersedes:      splitFeedbackList(args.String("supersedes")),
	}
	if entry.Lesson == "" {
		return feedbackCanonEntry{}, tuskerError(errorMissingArg, "feedback promote canon requires a lesson text", withHint("pass --lesson or promote a candidate with a product-idea/friction field"))
	}
	if len(entry.Supersedes) > 0 {
		var superseded []string
		entries, superseded = markFeedbackCanonSuperseded(entries, entry.Supersedes, entry.ID)
		if len(superseded) != len(entry.Supersedes) {
			return feedbackCanonEntry{}, tuskerError(errorNotFound, "cannot supersede unknown canon entry: "+missingFeedbackCanonSupersedes(entry.Supersedes, superseded), withPath(canonPath))
		}
	}
	entries = append(entries, entry)
	body = replaceOrAppendFeedbackCanonEntries(body, entries)
	data["updated_at"] = time.Now().UTC().Format(time.RFC3339)
	data["state_rev"] = v7StateRev(data, body)
	content, err := serializeDocument(data, body, v7FrontmatterOrder["domain_canon"])
	if err != nil {
		return feedbackCanonEntry{}, err
	}
	if err := writeText(canonPath, content); err != nil {
		return feedbackCanonEntry{}, err
	}
	_ = emitV7Event(vaultPath, entry.ID, "canon", "updated", "agent:"+defaultActorName(), map[string]any{"source": "feedback_promote", "domain": domain, "class": class, "source_notes": entry.SourceNotes, "supersedes": entry.Supersedes})
	return entry, nil
}

func uniqueFeedbackCanonEntryID(entries []feedbackCanonEntry, date, topic, lesson string) string {
	base := "lesson-" + strings.ReplaceAll(date, "-", "") + "-" + feedbackSlug(firstNonEmpty(topic, lesson), "canon")
	seen := map[string]bool{}
	for _, entry := range entries {
		seen[entry.ID] = true
	}
	id := base
	for i := 2; seen[id]; i++ {
		id = fmt.Sprintf("%s-%d", base, i)
	}
	return id
}

func feedbackCanonEntriesFromBody(body string) []feedbackCanonEntry {
	content := feedbackCanonEntriesSectionContent(body)
	if strings.TrimSpace(content) == "" {
		return nil
	}
	var entries []feedbackCanonEntry
	var current *feedbackCanonEntry
	flush := func() {
		if current != nil && current.ID != "" {
			entries = append(entries, *current)
		}
	}
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "### ") {
			flush()
			current = &feedbackCanonEntry{ID: strings.TrimSpace(strings.TrimPrefix(trimmed, "### "))}
			continue
		}
		if current == nil || !strings.HasPrefix(trimmed, "- ") || !strings.Contains(trimmed, ":") {
			continue
		}
		key, value, _ := strings.Cut(strings.TrimPrefix(trimmed, "- "), ":")
		key = strings.ReplaceAll(strings.ToLower(strings.TrimSpace(key)), "-", "_")
		value = strings.TrimSpace(value)
		switch key {
		case "class":
			current.Class = value
		case "status":
			current.Status = value
		case "topic":
			current.Topic = value
		case "lesson":
			current.Lesson = value
		case "source_notes":
			current.SourceNotes = splitFeedbackList(value)
		case "source_repos":
			current.SourceRepos = splitFeedbackList(value)
		case "recurrence_count":
			current.RecurrenceCount = atoiSafe(value)
		case "date_span":
			current.DateSpan = value
		case "promoted_at":
			current.PromotedAt = value
		case "supersedes":
			current.Supersedes = splitFeedbackList(value)
		case "superseded_by":
			current.SupersededBy = value
		}
	}
	flush()
	return entries
}

func markFeedbackCanonSuperseded(entries []feedbackCanonEntry, supersedes []string, replacement string) ([]feedbackCanonEntry, []string) {
	wanted := map[string]bool{}
	for _, id := range supersedes {
		wanted[strings.TrimSpace(id)] = true
	}
	var found []string
	for i := range entries {
		if wanted[entries[i].ID] {
			entries[i].Status = "superseded"
			entries[i].SupersededBy = replacement
			found = append(found, entries[i].ID)
		}
	}
	return entries, found
}

func missingFeedbackCanonSupersedes(wanted, found []string) string {
	seen := map[string]bool{}
	for _, id := range found {
		seen[id] = true
	}
	for _, id := range wanted {
		if !seen[id] {
			return id
		}
	}
	return strings.Join(wanted, ", ")
}

func replaceOrAppendFeedbackCanonEntries(body string, entries []feedbackCanonEntry) string {
	content := renderFeedbackCanonEntries(entries)
	lines := strings.Split(body, "\n")
	start := feedbackCanonEntriesHeadingIndex(lines)
	if start >= 0 {
		end := len(lines)
		for i := start + 1; i < len(lines); i++ {
			if strings.HasPrefix(strings.TrimSpace(lines[i]), "## ") {
				end = i
				break
			}
		}
		prefix := strings.Join(lines[:start+1], "\n")
		suffix := strings.Join(lines[end:], "\n")
		return prefix + "\n\n" + content + "\n\n" + suffix
	}
	return strings.TrimRight(body, "\n") + "\n\n" + feedbackCanonEntriesHeading + "\n\n" + content + "\n"
}

func feedbackCanonEntriesSectionContent(body string) string {
	lines := strings.Split(body, "\n")
	start := feedbackCanonEntriesHeadingIndex(lines)
	if start < 0 {
		return ""
	}
	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		if strings.HasPrefix(strings.TrimSpace(lines[i]), "## ") {
			end = i
			break
		}
	}
	return strings.TrimSpace(strings.Join(lines[start+1:end], "\n"))
}

func feedbackCanonEntriesHeadingIndex(lines []string) int {
	for i, line := range lines {
		if strings.TrimSpace(line) == feedbackCanonEntriesHeading {
			return i
		}
	}
	return -1
}

func renderFeedbackCanonEntries(entries []feedbackCanonEntry) string {
	var b strings.Builder
	for i, entry := range entries {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString("### " + entry.ID + "\n\n")
		b.WriteString("- class: " + entry.Class + "\n")
		b.WriteString("- status: " + firstNonEmpty(entry.Status, "current") + "\n")
		b.WriteString("- topic: " + entry.Topic + "\n")
		b.WriteString("- lesson: " + entry.Lesson + "\n")
		b.WriteString("- source_notes: " + strings.Join(entry.SourceNotes, ", ") + "\n")
		b.WriteString("- source_repos: " + strings.Join(entry.SourceRepos, ", ") + "\n")
		b.WriteString("- recurrence_count: " + strconv.Itoa(entry.RecurrenceCount) + "\n")
		b.WriteString("- date_span: " + entry.DateSpan + "\n")
		b.WriteString("- promoted_at: " + entry.PromotedAt + "\n")
		if len(entry.Supersedes) > 0 {
			b.WriteString("- supersedes: " + strings.Join(entry.Supersedes, ", ") + "\n")
		}
		if entry.SupersededBy != "" {
			b.WriteString("- superseded_by: " + entry.SupersededBy + "\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func feedbackCanonEntryJSON(entry feedbackCanonEntry) map[string]any {
	return map[string]any{
		"id":               entry.ID,
		"class":            entry.Class,
		"status":           entry.Status,
		"topic":            entry.Topic,
		"lesson":           entry.Lesson,
		"source_notes":     entry.SourceNotes,
		"source_repos":     entry.SourceRepos,
		"recurrence_count": entry.RecurrenceCount,
		"date_span":        entry.DateSpan,
		"promoted_at":      entry.PromotedAt,
		"supersedes":       entry.Supersedes,
		"superseded_by":    entry.SupersededBy,
	}
}

func v7DomainCanonProhibitions(body string) []feedbackCanonEntry {
	var prohibitions []feedbackCanonEntry
	for _, entry := range feedbackCanonEntriesFromBody(body) {
		if entry.Class == "prohibition" && firstNonEmpty(entry.Status, "current") != "superseded" {
			prohibitions = append(prohibitions, entry)
		}
	}
	sort.SliceStable(prohibitions, func(i, j int) bool {
		return prohibitions[i].ID < prohibitions[j].ID
	})
	return prohibitions
}

func renderV7DomainCanonProhibitions(body string) string {
	prohibitions := v7DomainCanonProhibitions(body)
	if len(prohibitions) == 0 {
		return ""
	}
	var lines []string
	for _, entry := range prohibitions {
		label := entry.Lesson
		if entry.Topic != "" {
			label = entry.Topic + ": " + entry.Lesson
		}
		lines = append(lines, "- `"+entry.ID+"` "+label)
	}
	return strings.Join(lines, "\n")
}
