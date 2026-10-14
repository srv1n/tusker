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
