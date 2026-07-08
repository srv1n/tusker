package main

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	defaultFeedbackMaxLines   = 10
	defaultFeedbackMaxChars   = 1600
	defaultFeedbackDedupeDays = 14
)

var (
	feedbackRequiredFields = []string{"context", "friction", "product-idea", "impact", "related"}
	feedbackCommandPattern = regexp.MustCompile(`(?i)\btusker\s+[a-z][a-z0-9-]*(?:\s+[a-z][a-z0-9-]*)?`)
	feedbackPriorityRegex  = regexp.MustCompile(`(?i)\bP[0-3]\b`)
)

type feedbackBudget struct {
	MaxLines int
	MaxChars int
}

type feedbackRecord struct {
	Path            string
	RelativePath    string
	VaultPath       string
	RepoRoot        string
	SourceRepo      string
	Date            string
	Actor           string
	Slug            string
	Fields          map[string]string
	Theme           string
	PriorityHint    string
	AffectedCommand string
	ContentLines    int
	ContentChars    int
	Issues          []Issue
}

type feedbackDigest struct {
	Date       string
	Since      string
	Repos      []string
	Records    []feedbackRecord
	Actionable []feedbackRecord
	Flagged    []feedbackRecord
	OutputPath string
}

func feedbackV7Cmd(args Args) error {
	switch strings.ToLower(strings.TrimSpace(firstNonEmpty(args.String("_pos0"), args.String("command")))) {
	case "add":
		return feedbackAddCmd(args)
	case "digest":
		return feedbackDigestCmd(args)
	case "signals":
		return feedbackSignalsCmd(args)
	case "review":
		return feedbackReviewCmd(args)
	case "promote":
		return feedbackPromoteCmd(args)
	case "", "help":
		printFeedbackHelp()
		return nil
	default:
		return tuskerError(errorInvalidArg, "unknown feedback command: "+args.String("_pos0"))
	}
}

func feedbackAddCmd(args Args) error {
	vaultPath, repoRoot, err := feedbackSingleTarget(args)
	if err != nil {
		return err
	}
	fields, related := feedbackFieldsFromArgs(args)
	var missing []string
	for _, field := range feedbackRequiredFields {
		if strings.TrimSpace(fields[field]) == "" {
			missing = append(missing, "--"+field)
		}
	}
	if len(missing) > 0 {
		return tuskerError(errorMissingArg, "feedback note missing required fields: "+strings.Join(missing, ", "), withHint("provide context, friction, product-idea, impact, and related"))
	}

	budget := feedbackBudgetFor(vaultPath, args)
	lines, chars := feedbackContentStats(fields)
	if !args.Bool("allow-long") && feedbackOverBudget(lines, chars, budget) {
		return tuskerError(errorInvalidArg, fmt.Sprintf("feedback note is too long: %d lines, %d chars; limit is %d lines, %d chars", lines, chars, budget.MaxLines, budget.MaxChars), withHint("keep product feedback concise or pass --allow-long for an intentional exception"), withContext(map[string]any{"lines": lines, "chars": chars, "max_lines": budget.MaxLines, "max_chars": budget.MaxChars}))
	}
	if !args.Bool("allow-progress-report") && feedbackLooksLikeProgressReport(feedbackRecord{Fields: fields}) {
		return tuskerError(errorInvalidArg, "feedback note looks like a progress report instead of product feedback", withHint("keep implementation status in attempts/proof, or pass --allow-progress-report for an intentional exception"))
	}

	date := strings.TrimSpace(args.String("date"))
	if date == "" {
		date = todayISO()
	}
	noteDate, err := time.Parse("2006-01-02", date)
	if err != nil {
		return tuskerError(errorInvalidArg, "feedback date must be YYYY-MM-DD: "+date, withContext(map[string]any{"arg": "--date"}))
	}
	if dedupeKey := normalizedFeedbackDedupeKey(fields["dedupe-key"]); dedupeKey != "" && !args.Bool("allow-duplicate") {
		match, err := findRecentFeedbackDedupeMatch(vaultPath, repoRoot, dedupeKey, noteDate)
		if err != nil {
			return err
		}
		if match.Path != "" {
			return tuskerError(errorInvalidArg, "feedback note duplicates recent dedupe-key "+fields["dedupe-key"]+" in "+match.RelativePath, withHint("use a different --dedupe-key or pass --allow-duplicate for an intentional repeat"), withPath(match.Path), withContext(map[string]any{"dedupe_key": fields["dedupe-key"], "existing_path": match.RelativePath}))
		}
	}
	actor := feedbackSlug(firstNonEmpty(args.String("actor"), defaultActorName()), "agent")
	slug := feedbackSlug(firstNonEmpty(args.String("slug"), args.String("topic"), fields["product-idea"], fields["friction"]), "feedback")
	filePath := uniqueFeedbackNotePath(vaultPath, date, actor, slug)
	content := renderFeedbackNoteMarkdown(fields, related)
	if err := writeText(filePath, content); err != nil {
		return err
	}

	if args.Bool("json") {
		emitJSON(map[string]any{"ok": true, "path": filePath, "vault": vaultPath, "repo": repoRoot, "lines": lines, "chars": chars})
		return nil
	}
	if !args.Bool("quiet") {
		fmt.Printf("Wrote feedback note %s\n", filePath)
	}
	return nil
}

func feedbackDigestCmd(args Args) error {
	digest, err := buildFeedbackDigest(args)
	if err != nil {
		return err
	}
	markdown := renderFeedbackDigestMarkdown(digest)
	if args.Bool("write") {
		outputPath, err := feedbackDigestOutputPath(args, digest)
		if err != nil {
			return err
		}
		if err := writeText(outputPath, markdown); err != nil {
			return err
		}
		digest.OutputPath = outputPath
	}
	if args.Bool("json") {
		emitJSON(map[string]any{
			"ok":          true,
			"date":        digest.Date,
			"since":       digest.Since,
			"repos":       digest.Repos,
			"counts":      map[string]any{"notes": len(digest.Records), "actionable": len(digest.Actionable), "flagged": len(digest.Flagged)},
			"output_path": nullIfEmptyString(digest.OutputPath),
		})
		return nil
	}
	fmt.Print(markdown)
	if digest.OutputPath != "" && !args.Bool("quiet") {
		fmt.Printf("\nWrote feedback digest %s\n", digest.OutputPath)
	}
	return nil
}

func buildFeedbackDigest(args Args) (feedbackDigest, error) {
	since := strings.TrimSpace(args.String("since"))
	if since == "" {
		return feedbackDigest{}, tuskerError(errorMissingArg, "feedback digest requires --since <YYYY-MM-DD>", withContext(map[string]any{"arg": "--since"}))
	}
	sinceDate, err := time.Parse("2006-01-02", since)
	if err != nil {
		return feedbackDigest{}, tuskerError(errorInvalidArg, "feedback digest --since must be YYYY-MM-DD: "+since)
	}
	date := strings.TrimSpace(args.String("date"))
	if date == "" {
		date = todayISO()
	}
	if _, err := time.Parse("2006-01-02", date); err != nil {
		return feedbackDigest{}, tuskerError(errorInvalidArg, "feedback digest --date must be YYYY-MM-DD: "+date)
	}

	targets, err := feedbackDigestTargets(args)
	if err != nil {
		return feedbackDigest{}, err
	}
	digest := feedbackDigest{Date: date, Since: since}
	for _, target := range targets {
		digest.Repos = append(digest.Repos, target.repoRoot)
		records, err := feedbackRecordsForVault(target.vaultPath, target.repoRoot, sinceDate)
		if err != nil {
			return feedbackDigest{}, err
		}
		digest.Records = append(digest.Records, records...)
	}
	sort.SliceStable(digest.Records, func(i, j int) bool {
		if digest.Records[i].Date != digest.Records[j].Date {
			return digest.Records[i].Date < digest.Records[j].Date
		}
		return digest.Records[i].RelativePath < digest.Records[j].RelativePath
	})
	for _, record := range digest.Records {
		if len(record.Issues) > 0 {
			digest.Flagged = append(digest.Flagged, record)
		} else {
			digest.Actionable = append(digest.Actionable, record)
		}
	}
	return digest, nil
}

type feedbackTarget struct {
	vaultPath string
	repoRoot  string
}

func feedbackDigestTargets(args Args) ([]feedbackTarget, error) {
	repos := splitFeedbackList(args.String("repo"))
	if len(repos) == 0 {
		if vaultArg := strings.TrimSpace(args.String("vault")); vaultArg != "" {
			vaultPath, err := filepath.Abs(vaultArg)
			if err != nil {
				return nil, err
			}
			return []feedbackTarget{{vaultPath: vaultPath, repoRoot: filepath.Dir(vaultPath)}}, nil
		}
		vaultPath, err := resolveVaultPath(args, false)
		if err != nil {
			return nil, err
		}
		return []feedbackTarget{{vaultPath: vaultPath, repoRoot: filepath.Dir(vaultPath)}}, nil
	}
	var targets []feedbackTarget
	for _, repo := range repos {
		vaultPath, repoRoot, err := feedbackVaultForRepoPath(repo)
		if err != nil {
			return nil, err
		}
		targets = append(targets, feedbackTarget{vaultPath: vaultPath, repoRoot: repoRoot})
	}
	return targets, nil
}

func feedbackSingleTarget(args Args) (string, string, error) {
	if vaultArg := strings.TrimSpace(args.String("vault")); vaultArg != "" {
		vaultPath, err := filepath.Abs(vaultArg)
		if err != nil {
			return "", "", err
		}
		return vaultPath, filepath.Dir(vaultPath), nil
	}
	repos := splitFeedbackList(args.String("repo"))
	if len(repos) > 1 {
		return "", "", tuskerError(errorInvalidArg, "feedback add accepts one --repo path; use feedback digest for multiple repos")
	}
	if len(repos) == 1 {
		return feedbackVaultForRepoPath(repos[0])
	}
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return "", "", err
	}
	return vaultPath, filepath.Dir(vaultPath), nil
}

func feedbackVaultForRepoPath(path string) (string, string, error) {
	abs, err := filepath.Abs(strings.TrimSpace(path))
	if err != nil {
		return "", "", err
	}
	base := filepath.Base(abs)
	if isVaultDir(abs) || (base == defaultRepoVaultDir || base == legacyRepoVaultDir) && (dirExists(abs) || dirExists(filepath.Join(abs, "feedback"))) {
		return abs, filepath.Dir(abs), nil
	}
	if discovered, _ := discoverVault(abs); discovered != "" {
		return discovered, filepath.Dir(discovered), nil
	}
	for _, name := range []string{defaultRepoVaultDir, legacyRepoVaultDir} {
		candidate := filepath.Join(abs, name)
		if dirExists(filepath.Join(candidate, "feedback")) {
			return candidate, abs, nil
		}
	}
	return filepath.Join(abs, defaultRepoVaultDir), abs, nil
}

func feedbackFieldsFromArgs(args Args) (map[string]string, []string) {
	fields := map[string]string{}
	for _, field := range feedbackRequiredFields {
		fields[field] = feedbackCleanValue(firstNonEmpty(args.String(field), args.String(strings.ReplaceAll(field, "-", "_"))))
	}
	related := splitFeedbackList(fields["related"])
	fields["related"] = strings.Join(related, ", ")
	for _, optional := range []string{"theme", "priority-hint", "affected-command", "dedupe-key"} {
		value := feedbackCleanValue(firstNonEmpty(args.String(optional), args.String(strings.ReplaceAll(optional, "-", "_"))))
		if value != "" {
			fields[optional] = value
		}
	}
	return fields, related
}

func renderFeedbackNoteMarkdown(fields map[string]string, related []string) string {
	lines := []string{
		"# Agent Feedback",
		"",
		"- context: " + fields["context"],
		"- friction: " + fields["friction"],
		"- product-idea: " + fields["product-idea"],
		"- impact: " + fields["impact"],
		"- related: " + strings.Join(related, ", "),
	}
	for _, optional := range []string{"theme", "priority-hint", "affected-command", "dedupe-key"} {
		if strings.TrimSpace(fields[optional]) != "" {
			lines = append(lines, "- "+optional+": "+fields[optional])
		}
	}
	return strings.Join(lines, "\n") + "\n"
}

func findRecentFeedbackDedupeMatch(vaultPath, repoRoot, dedupeKey string, date time.Time) (feedbackRecord, error) {
	since := date.AddDate(0, 0, -defaultFeedbackDedupeDays)
	records, err := feedbackRecordsForVault(vaultPath, repoRoot, since)
	if err != nil {
		return feedbackRecord{}, err
	}
	for _, record := range records {
		recordDate, err := time.Parse("2006-01-02", record.Date)
		if err != nil || recordDate.After(date) {
			continue
		}
		if normalizedFeedbackDedupeKey(record.Fields["dedupe-key"]) == dedupeKey {
			return record, nil
		}
	}
	return feedbackRecord{}, nil
}

func uniqueFeedbackNotePath(vaultPath, date, actor, slug string) string {
	dir := filepath.Join(vaultPath, "feedback", "agents")
	base := date + "-" + actor + "-" + slug
	candidate := filepath.Join(dir, base+".md")
	for i := 2; fileExists(candidate); i++ {
		candidate = filepath.Join(dir, fmt.Sprintf("%s-%d.md", base, i))
	}
	return candidate
}

func feedbackRecordsForVault(vaultPath, repoRoot string, sinceDate time.Time) ([]feedbackRecord, error) {
	agentsDir := filepath.Join(vaultPath, "feedback", "agents")
	if !dirExists(agentsDir) {
		return nil, nil
	}
	var records []feedbackRecord
	if err := walkDirUnsorted(agentsDir, func(current string, entry fs.DirEntry) error {
		rel, err := filepath.Rel(agentsDir, current)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if entry.IsDir() {
			if feedbackExcludedPath(rel) {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(entry.Name()), ".md") || feedbackExcludedPath(rel) {
			return nil
		}
		if feedbackMigrationDraftPath(rel) {
			return nil
		}
		record := parseFeedbackRecord(current, vaultPath, repoRoot)
		if record.Date != "" {
			parsed, err := time.Parse("2006-01-02", record.Date)
			if err == nil && parsed.Before(sinceDate) {
				return nil
			}
		}
		records = append(records, record)
		return nil
	}); err != nil {
		return nil, err
	}
	return records, nil
}

func parseFeedbackRecord(filePath, vaultPath, repoRoot string) feedbackRecord {
	rel, _ := filepath.Rel(vaultPath, filePath)
	rel = filepath.ToSlash(rel)
	record := feedbackRecord{
		Path:         filePath,
		RelativePath: filepath.ToSlash(rel),
		VaultPath:    vaultPath,
		RepoRoot:     repoRoot,
		SourceRepo:   filepath.Base(repoRoot),
		Fields:       map[string]string{},
	}
	record.Date, record.Actor, record.Slug = feedbackFilenameParts(filepath.Base(filePath))
	if record.Date == "" {
		record.Issues = append(record.Issues, issue("FEEDBACK_DATE_MISSING", "feedback note filename must start with YYYY-MM-DD", record.RelativePath, "use YYYY-MM-DD-<actor>-<slug>.md", nil))
	}
	raw, err := readText(filePath)
	if err != nil {
		record.Issues = append(record.Issues, issue("FEEDBACK_READ_FAILED", err.Error(), record.RelativePath, "", nil))
		return record
	}
	data, body, err := parseFrontmatter(raw)
	if err != nil {
		record.Issues = append(record.Issues, issue("FEEDBACK_FRONTMATTER_INVALID", err.Error(), record.RelativePath, "", nil))
		body = raw
	}
	record.Fields = extractFeedbackFields(data, body)
	if date := feedbackCleanValue(firstNonEmpty(record.Fields["date"], stringField(data, "date"))); date != "" {
		record.Date = date
	}
	if actor := feedbackCleanValue(firstNonEmpty(record.Fields["actor"], stringField(data, "actor"))); actor != "" {
		record.Actor = actor
	}
	record.Theme = feedbackTheme(record)
	record.PriorityHint = feedbackPriorityHint(record)
	record.AffectedCommand = feedbackAffectedCommand(record)
	record.ContentLines, record.ContentChars = feedbackContentStats(record.Fields)
	record.Issues = append(record.Issues, validateFeedbackRecord(record, feedbackBudgetFor(vaultPath, nil))...)
	return record
}

func validateFeedbackNotes(vaultPath string) []Issue {
	records, err := feedbackRecordsForVault(vaultPath, filepath.Dir(vaultPath), time.Time{})
	if err != nil {
		return []Issue{issue("FEEDBACK_VALIDATE_FAILED", err.Error(), "feedback/agents", "", nil)}
	}
	var warnings []Issue
	for _, record := range records {
		warnings = append(warnings, record.Issues...)
	}
	return warnings
}

func validateV7FeedbackNotes(vaultPath string) []Issue {
	return validateFeedbackNotes(vaultPath)
}

func validateFeedbackRecord(record feedbackRecord, budget feedbackBudget) []Issue {
	var warnings []Issue
	for _, field := range feedbackRequiredFields {
		if strings.TrimSpace(record.Fields[field]) == "" {
			warnings = append(warnings, issue("FEEDBACK_MISSING_FIELD", "feedback note missing "+field, record.RelativePath, "use context, friction, product-idea, impact, and related fields", map[string]any{"field": field}))
		}
	}
	if feedbackOverBudget(record.ContentLines, record.ContentChars, budget) {
		warnings = append(warnings, issue("FEEDBACK_NOTE_LONG", fmt.Sprintf("feedback note has %d content lines and %d chars; limit is %d lines and %d chars", record.ContentLines, record.ContentChars, budget.MaxLines, budget.MaxChars), record.RelativePath, "summarize product friction instead of writing a status report", map[string]any{"lines": record.ContentLines, "chars": record.ContentChars, "max_lines": budget.MaxLines, "max_chars": budget.MaxChars}))
	}
	if feedbackLooksLikeProgressReport(record) {
		warnings = append(warnings, issue("FEEDBACK_PROGRESS_REPORT", "feedback note looks like a progress report instead of product feedback", record.RelativePath, "keep implementation status in attempts/proof; feedback should name friction and a product idea", nil))
	}
	if record.Date != "" {
		if _, err := time.Parse("2006-01-02", record.Date); err != nil {
			warnings = append(warnings, issue("FEEDBACK_DATE_INVALID", "feedback note date must be YYYY-MM-DD: "+record.Date, record.RelativePath, "", nil))
		}
	}
	return warnings
}

func extractFeedbackFields(data map[string]any, body string) map[string]string {
	fields := map[string]string{}
	for key, value := range data {
		normalized := normalizeFeedbackFieldName(key)
		if normalized == "" {
			continue
		}
		if normalized == "related" {
			fields[normalized] = strings.Join(normalizeList(value), ", ")
			continue
		}
		fields[normalized] = feedbackCleanValue(toString(value))
	}
	for key, value := range feedbackLabeledLines(body) {
		if fields[key] == "" {
			fields[key] = value
		}
	}
	for _, field := range feedbackRequiredFields {
		if _, ok := fields[field]; !ok {
			fields[field] = ""
		}
	}
	return fields
}

func feedbackLabeledLines(body string) map[string]string {
	fields := map[string]string{}
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		trimmed = strings.TrimPrefix(trimmed, "- ")
		trimmed = strings.TrimPrefix(trimmed, "* ")
		if !strings.Contains(trimmed, ":") {
			continue
		}
		label, value, ok := strings.Cut(trimmed, ":")
		if !ok {
			continue
		}
		key := normalizeFeedbackFieldName(label)
		if key == "" {
			continue
		}
		fields[key] = feedbackCleanValue(value)
	}
	return fields
}

func normalizeFeedbackFieldName(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	normalized = strings.Trim(normalized, "`* ")
	normalized = strings.ReplaceAll(normalized, "_", "-")
	normalized = strings.ReplaceAll(normalized, " ", "-")
	switch normalized {
	case "context", "friction", "impact", "theme", "date", "actor", "dedupe-key":
		return normalized
	case "dedupe", "dedupekey", "duplicate-key":
		return "dedupe-key"
	case "product-idea", "product":
		return "product-idea"
	case "related", "related-task", "related-command", "related-task-or-command", "related-tasks-or-commands":
		return "related"
	case "priority", "priority-hint", "priority-hints":
		return "priority-hint"
	case "affected-command", "affected-commands", "command":
		return "affected-command"
	case "source-repo", "source":
		return "source-repo"
	default:
		return ""
	}
}

func feedbackContentStats(fields map[string]string) (int, int) {
	var lines int
	var parts []string
	for _, field := range append(append([]string{}, feedbackRequiredFields...), "theme", "priority-hint", "affected-command", "dedupe-key") {
		value := strings.TrimSpace(fields[field])
		if value == "" {
			continue
		}
		for _, line := range strings.Split(value, "\n") {
			if strings.TrimSpace(line) != "" {
				lines++
			}
		}
		parts = append(parts, value)
	}
	return lines, len(strings.Join(parts, "\n"))
}

func feedbackOverBudget(lines, chars int, budget feedbackBudget) bool {
	return budget.MaxLines > 0 && lines > budget.MaxLines || budget.MaxChars > 0 && chars > budget.MaxChars
}

func feedbackBudgetFor(vaultPath string, args Args) feedbackBudget {
	budget := feedbackBudget{MaxLines: defaultFeedbackMaxLines, MaxChars: defaultFeedbackMaxChars}
	if strings.TrimSpace(vaultPath) != "" {
		configPath := filepath.Join(filepath.Dir(vaultPath), "tusker.yaml")
		if raw, err := readText(configPath); err == nil {
			var cfg struct {
				Feedback struct {
					NoteMaxLines int `yaml:"note_max_lines"`
					NoteMaxChars int `yaml:"note_max_chars"`
					MaxLines     int `yaml:"max_lines"`
					MaxChars     int `yaml:"max_chars"`
				} `yaml:"feedback"`
				Validation struct {
					FeedbackNoteMaxLines int `yaml:"feedback_note_max_lines"`
					FeedbackNoteMaxChars int `yaml:"feedback_note_max_chars"`
				} `yaml:"validation"`
			}
			if yaml.Unmarshal([]byte(raw), &cfg) == nil {
				if cfg.Feedback.NoteMaxLines > 0 {
					budget.MaxLines = cfg.Feedback.NoteMaxLines
				} else if cfg.Feedback.MaxLines > 0 {
					budget.MaxLines = cfg.Feedback.MaxLines
				} else if cfg.Validation.FeedbackNoteMaxLines > 0 {
					budget.MaxLines = cfg.Validation.FeedbackNoteMaxLines
				}
				if cfg.Feedback.NoteMaxChars > 0 {
					budget.MaxChars = cfg.Feedback.NoteMaxChars
				} else if cfg.Feedback.MaxChars > 0 {
					budget.MaxChars = cfg.Feedback.MaxChars
				} else if cfg.Validation.FeedbackNoteMaxChars > 0 {
					budget.MaxChars = cfg.Validation.FeedbackNoteMaxChars
				}
			}
		}
	}
	if args != nil {
		if value := atoiSafe(args.String("max-lines")); value > 0 {
			budget.MaxLines = value
		}
		if value := atoiSafe(args.String("max-chars")); value > 0 {
			budget.MaxChars = value
		}
	}
	return budget
}

func feedbackLooksLikeProgressReport(record feedbackRecord) bool {
	text := strings.ToLower(strings.Join([]string{
		record.Fields["context"],
		record.Fields["friction"],
		record.Fields["product-idea"],
		record.Fields["impact"],
		record.Fields["related"],
	}, "\n"))
	hits := 0
	for _, marker := range []string{"changed files", "tests run", "validation", "implemented", "completed", "work log", "summary:", "next steps", "diff", "commit"} {
		if strings.Contains(text, marker) {
			hits++
		}
	}
	return hits >= 2
}

func normalizedFeedbackDedupeKey(value string) string {
	return strings.ToLower(feedbackCleanValue(value))
}

func feedbackTheme(record feedbackRecord) string {
	if value := strings.TrimSpace(record.Fields["theme"]); value != "" {
		return value
	}
	text := strings.ToLower(record.Fields["context"] + "\n" + record.Fields["friction"] + "\n" + record.Fields["product-idea"] + "\n" + record.Fields["related"])
	switch {
	case strings.Contains(text, "cas") || strings.Contains(text, "conflict") || strings.Contains(text, "retry"):
		return "concurrency and retries"
	case strings.Contains(text, "validate") || strings.Contains(text, "proof") || strings.Contains(text, "verification"):
		return "validation and proof"
	case strings.Contains(text, "install") || strings.Contains(text, "update") || strings.Contains(text, "bootstrap") || strings.Contains(text, "agents.md") || strings.Contains(text, "claude.md"):
		return "bootstrap and repo contract"
	case strings.Contains(text, "status") || strings.Contains(text, "handoff") || strings.Contains(text, "protected"):
		return "workflow control"
	case strings.Contains(text, "feedback"):
		return "feedback loop"
	default:
		return "general workflow"
	}
}

func feedbackPriorityHint(record feedbackRecord) string {
	if value := strings.TrimSpace(record.Fields["priority-hint"]); value != "" {
		return strings.ToUpper(value)
	}
	text := record.Fields["context"] + "\n" + record.Fields["friction"] + "\n" + record.Fields["impact"] + "\n" + record.Fields["product-idea"]
	if match := feedbackPriorityRegex.FindString(text); match != "" {
		return strings.ToUpper(match)
	}
	lower := strings.ToLower(text)
	switch {
	case strings.Contains(lower, "data loss") || strings.Contains(lower, "corrupt") || strings.Contains(lower, "security") || strings.Contains(lower, "blocked all"):
		return "P0"
	case strings.Contains(lower, "blocked") || strings.Contains(lower, "wasted turn") || strings.Contains(lower, "manual cleanup") || strings.Contains(lower, "can't continue"):
		return "P1"
	case strings.Contains(lower, "confusing") || strings.Contains(lower, "friction") || strings.Contains(lower, "slow"):
		return "P2"
	default:
		return "P3"
	}
}

func feedbackAffectedCommand(record feedbackRecord) string {
	if value := strings.TrimSpace(record.Fields["affected-command"]); value != "" {
		return value
	}
	text := record.Fields["related"] + "\n" + record.Fields["context"] + "\n" + record.Fields["friction"] + "\n" + record.Fields["product-idea"]
	if match := feedbackCommandPattern.FindString(text); match != "" {
		return strings.Join(strings.Fields(strings.Trim(match, "`")), " ")
	}
	return "n/a"
}

func renderFeedbackDigestMarkdown(digest feedbackDigest) string {
	var b strings.Builder
	b.WriteString("# Agent Feedback Digest - " + digest.Date + "\n\n")
	b.WriteString("- Since: " + digest.Since + "\n")
	b.WriteString(fmt.Sprintf("- Repos: %d\n", len(digest.Repos)))
	b.WriteString(fmt.Sprintf("- Actionable notes: %d\n", len(digest.Actionable)))
	b.WriteString(fmt.Sprintf("- Flagged notes: %d\n\n", len(digest.Flagged)))
	renderFeedbackGroupedSection(&b, "By Theme", digest.Actionable, func(record feedbackRecord) string { return record.Theme })
	renderFeedbackGroupedSection(&b, "By Source Repo", digest.Actionable, func(record feedbackRecord) string { return record.SourceRepo })
	renderFeedbackGroupedSection(&b, "By Priority Hint", digest.Actionable, func(record feedbackRecord) string { return record.PriorityHint })
	renderFeedbackGroupedSection(&b, "By Affected Command", digest.Actionable, func(record feedbackRecord) string { return record.AffectedCommand })
	if len(digest.Flagged) > 0 {
		b.WriteString("## Malformed Or Over Budget\n\n")
		for _, record := range digest.Flagged {
			b.WriteString("- " + feedbackRecordLabel(record) + "\n")
			for _, current := range record.Issues {
				b.WriteString("  - " + current.Code + ": " + current.Message + "\n")
			}
		}
		b.WriteString("\n")
	}
	return b.String()
}

func renderFeedbackGroupedSection(b *strings.Builder, title string, records []feedbackRecord, keyFn func(feedbackRecord) string) {
	b.WriteString("## " + title + "\n\n")
	groups := map[string][]feedbackRecord{}
	for _, record := range records {
		key := firstNonEmpty(strings.TrimSpace(keyFn(record)), "n/a")
		groups[key] = append(groups[key], record)
	}
	if len(groups) == 0 {
		b.WriteString("_No actionable feedback._\n\n")
		return
	}
	for _, key := range sortedFeedbackGroupKeys(groups) {
		b.WriteString("### " + key + "\n\n")
		for _, record := range groups[key] {
			b.WriteString("- " + feedbackRecordSummary(record) + "\n")
		}
		b.WriteString("\n")
	}
}

func sortedFeedbackGroupKeys(groups map[string][]feedbackRecord) []string {
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func feedbackRecordSummary(record feedbackRecord) string {
	idea := feedbackShort(record.Fields["product-idea"], 140)
	friction := feedbackShort(record.Fields["friction"], 120)
	impact := feedbackShort(record.Fields["impact"], 100)
	return fmt.Sprintf("[%s] %s `%s`: %s; friction: %s; impact: %s (%s)", record.PriorityHint, record.SourceRepo, record.AffectedCommand, idea, friction, impact, record.RelativePath)
}

func feedbackRecordLabel(record feedbackRecord) string {
	return fmt.Sprintf("%s %s (%s)", record.SourceRepo, record.RelativePath, firstNonEmpty(record.Date, "undated"))
}

func feedbackDigestOutputPath(args Args, digest feedbackDigest) (string, error) {
	if outputVault := strings.TrimSpace(args.String("output-vault")); outputVault != "" {
		abs, err := filepath.Abs(outputVault)
		if err != nil {
			return "", err
		}
		return filepath.Join(abs, "feedback", "digests", digest.Date+".md"), nil
	}
	if vaultArg := strings.TrimSpace(args.String("vault")); vaultArg != "" {
		abs, err := filepath.Abs(vaultArg)
		if err != nil {
			return "", err
		}
		return filepath.Join(abs, "feedback", "digests", digest.Date+".md"), nil
	}
	targets, err := feedbackDigestTargets(args)
	if err != nil {
		return "", err
	}
	if len(targets) == 0 {
		return "", tuskerError(errorMissingArg, "feedback digest --write needs --repo, --vault, or --output-vault")
	}
	return filepath.Join(targets[0].vaultPath, "feedback", "digests", digest.Date+".md"), nil
}

func ensureFeedbackReadmeForRepo(repoPath string) (string, bool, error) {
	return ensureFeedbackReadmeForRepoVault(repoPath, "")
}

func ensureFeedbackReadmeForRepoVault(repoPath, vaultArg string) (string, bool, error) {
	if strings.TrimSpace(vaultArg) != "" {
		vaultPath, err := filepath.Abs(vaultArg)
		if err != nil {
			return "", false, err
		}
		return ensureFeedbackReadmeForVault(vaultPath)
	}
	vaultPath, _, err := feedbackVaultForRepoPath(repoPath)
	if err != nil {
		return "", false, err
	}
	return ensureFeedbackReadmeForVault(vaultPath)
}

func ensureFeedbackReadmeForVault(vaultPath string) (string, bool, error) {
	readmePath := filepath.Join(vaultPath, "feedback", "agents", "README.md")
	if fileExists(readmePath) {
		return readmePath, false, nil
	}
	if err := writeText(readmePath, feedbackAgentsReadmeTemplate()); err != nil {
		return "", false, err
	}
	return readmePath, true, nil
}

func feedbackAgentsReadmeTemplate() string {
	return strings.Join([]string{
		"# Agent Feedback",
		"",
		"Use this folder for concise, actionable feedback about Tusker or repo workflow friction.",
		"",
		"Create one Markdown file per meaningful work turn:",
		"",
		"`YYYY-MM-DD-<agent-or-tool>-<short-topic>.md`",
		"",
		"Keep each note under 10 content lines:",
		"",
		"- context:",
		"- friction:",
		"- product-idea:",
		"- impact:",
		"- related:",
		"",
		"Do not paste transcripts, logs, diffs, secrets, raw tool output, or routine progress summaries. Skip the note when there is no useful product signal.",
		"",
	}, "\n")
}

func feedbackPointerInstruction() string {
	return "- Record concise Tusker/product friction with `tusker feedback add`; skip routine progress reports."
}

func feedbackExcludedPath(rel string) bool {
	lower := strings.ToLower(filepath.ToSlash(rel))
	base := filepath.Base(lower)
	return base == "readme.md" ||
		strings.Contains(lower, "/template") ||
		strings.Contains(lower, "/templates/") ||
		strings.Contains(lower, "/example") ||
		strings.Contains(lower, "/examples/") ||
		strings.HasPrefix(base, "template") ||
		strings.HasPrefix(base, "example")
}

func feedbackMigrationDraftPath(rel string) bool {
	base := strings.ToLower(filepath.Base(filepath.ToSlash(rel)))
	return strings.HasSuffix(base, "-agent-guidance-migration-draft.md")
}

func feedbackFilenameParts(base string) (string, string, string) {
	base = strings.TrimSuffix(base, filepath.Ext(base))
	if len(base) < len("2006-01-02") {
		return "", "", base
	}
	date := base[:10]
	if _, err := time.Parse("2006-01-02", date); err != nil {
		return "", "", base
	}
	rest := strings.TrimPrefix(base[10:], "-")
	parts := strings.SplitN(rest, "-", 2)
	actor := ""
	slug := ""
	if len(parts) > 0 {
		actor = parts[0]
	}
	if len(parts) > 1 {
		slug = parts[1]
	}
	return date, actor, slug
}

func splitFeedbackList(value string) []string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, ",", "\n")
	var out []string
	for _, part := range strings.Split(value, "\n") {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func feedbackCleanValue(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}

func feedbackShort(value string, limit int) string {
	value = feedbackCleanValue(value)
	if len(value) <= limit {
		return value
	}
	if limit <= 3 {
		return value[:limit]
	}
	return strings.TrimRight(value[:limit-3], " .,;:") + "..."
}

func feedbackSlug(value, fallbackValue string) string {
	value = strings.ToLower(feedbackCleanValue(value))
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	slug := strings.Trim(b.String(), "-")
	if len(slug) > 48 {
		slug = strings.Trim(slug[:48], "-")
	}
	if slug == "" {
		return fallbackValue
	}
	return slug
}

func printFeedbackHelp() {
	fmt.Println(`Tusker feedback

Commands:
  tusker feedback add --context <text> --friction <text> --product-idea <text> --impact <text> --related <text>
  tusker feedback digest --since <YYYY-MM-DD> --repo <path[,path...]>
  tusker feedback signals --since <YYYY-MM-DD> [--repo <path[,path...]>] [--write]
  tusker feedback review --since <YYYY-MM-DD> [--repo <path[,path...]>] [--write]
  tusker feedback promote <signal-id> [--write]

Concepts:
  Events are history: timestamped facts about what happened.
  Feedback notes are subjective input: concise agent or human observations.
  Signals are derived product facts stored under .tusker/feedback/signals/YYYY-MM-DD/*.json.

Options:
  --repo <path>          Repo root. For digest/signals/review, comma or newline separated paths are accepted.
  --vault <path>         Tusker vault path.
  --actor <name>         Feedback actor for generated filenames.
  --slug <slug>          Feedback filename slug.
  --allow-long           Allow a note over the configured feedback budget.
  --allow-progress-report
                         Allow a note that looks like an implementation progress report.
  --dedupe-key <key>     Reject duplicate recent feedback with the same key.
  --allow-duplicate      Allow a duplicate --dedupe-key.
  --write                Write digest to .tusker/feedback/digests/<date>.md.
  --output-vault <path>  Vault where --write should place generated feedback output.
  --review <path>        Review packet path for feedback promote.
  --action <kind>        Promotion action, e.g. create-task, decision, skip.`)
}
