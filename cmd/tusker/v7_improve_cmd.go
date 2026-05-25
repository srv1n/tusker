package main

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	defaultImproveScanDays          = 30
	defaultImproveScanMaxCandidates = 8
)

var improveStopwords = map[string]bool{
	"a": true, "add": true, "after": true, "and": true, "as": true, "by": true, "for": true, "from": true,
	"implement": true, "in": true, "into": true, "make": true, "of": true, "on": true, "or": true, "the": true,
	"to": true, "update": true, "with": true,
}

type improveScan struct {
	Date        string
	Since       string
	Days        int
	VaultPath   string
	RepoRoot    string
	Mode        string
	Runner      string
	Model       string
	Reasoning   string
	Profile     string
	Sources     []improveSourceStatus
	Candidates  []improveCandidate
	Created     []string
	Extended    []string
	Skipped     []string
	NeedMore    []string
	ReportPath  string
	Inventory   improveInventory
	EvidenceLen int
}

type improveSourceStatus struct {
	Name   string
	Status string
	Detail string
}

type improveEvidence struct {
	Date         string
	Source       string
	Title        string
	Path         string
	Text         string
	Affected     string
	ExistingPath string
}

type improveInventory struct {
	Skills      []string
	AgentDocs   []string
	Subagents   []string
	Automations []string
}

type improveCandidate struct {
	Key           string
	Workflow      string
	Evidence      []improveEvidence
	Confidence    string
	Form          string
	Why           string
	WorthCreating bool
	ExistingPath  string
	SkipReason    string
	Slug          string
}

func improveV7Cmd(args Args) error {
	switch strings.ToLower(strings.TrimSpace(firstNonEmpty(args.String("_pos0"), args.String("command")))) {
	case "scan":
		return improveScanCmd(args)
	case "", "help":
		printImproveHelp()
		return nil
	default:
		return tuskerError(errorInvalidArg, "unknown improve command: "+args.String("_pos0"))
	}
}

func improveScanCmd(args Args) error {
	scan, err := buildImproveScan(args)
	if err != nil {
		return err
	}
	if args.Bool("apply") {
		if err := applyImproveScan(&scan, args); err != nil {
			return err
		}
	}
	markdown := renderImproveScanMarkdown(scan)
	if args.Bool("write") || args.Bool("apply") {
		reportPath := improveScanReportPath(scan, args)
		if err := writeText(reportPath, markdown); err != nil {
			return err
		}
		scan.ReportPath = reportPath
		markdown = renderImproveScanMarkdown(scan)
	}
	if args.Bool("json") {
		emitJSON(improveScanJSON(scan))
		return nil
	}
	fmt.Print(markdown)
	return nil
}

func buildImproveScan(args Args) (improveScan, error) {
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return improveScan{}, err
	}
	date := strings.TrimSpace(args.String("date"))
	if date == "" {
		date = todayISO()
	}
	nowDate, err := time.Parse("2006-01-02", date)
	if err != nil {
		return improveScan{}, tuskerError(errorInvalidArg, "improve scan --date must be YYYY-MM-DD: "+date)
	}
	sinceDate, days, err := improveScanSince(args, nowDate)
	if err != nil {
		return improveScan{}, err
	}
	scan := improveScan{
		Date:      date,
		Since:     sinceDate.Format("2006-01-02"),
		Days:      days,
		VaultPath: vaultPath,
		RepoRoot:  filepath.Dir(vaultPath),
		Mode:      "dry-run",
		Runner:    firstNonEmpty(args.String("runner"), "local"),
		Model:     firstNonEmpty(args.String("model"), "not-recorded"),
		Reasoning: firstNonEmpty(args.String("reasoning"), "low"),
		Profile:   firstNonEmpty(args.String("profile"), "cheap-discovery"),
	}
	if args.Bool("apply") {
		scan.Mode = "apply"
	}
	scan.Inventory = collectImproveInventory(scan.RepoRoot, vaultPath)
	evidence, sources, err := collectImproveEvidence(vaultPath, scan.RepoRoot, sinceDate, args)
	if err != nil {
		return improveScan{}, err
	}
	scan.Sources = sources
	scan.EvidenceLen = len(evidence)
	scan.Candidates = improveCandidates(evidence, scan.Inventory, atoiDefault(args.String("max-candidates"), defaultImproveScanMaxCandidates))
	classifyImproveScan(&scan)
	return scan, nil
}

func improveScanSince(args Args, nowDate time.Time) (time.Time, int, error) {
	if args.Bool("all") {
		return time.Time{}, 0, nil
	}
	if since := strings.TrimSpace(args.String("since")); since != "" {
		parsed, err := time.Parse("2006-01-02", since)
		if err != nil {
			return time.Time{}, 0, tuskerError(errorInvalidArg, "improve scan --since must be YYYY-MM-DD: "+since)
		}
		return parsed, int(nowDate.Sub(parsed).Hours() / 24), nil
	}
	days := atoiDefault(args.String("days"), defaultImproveScanDays)
	if days <= 0 {
		return time.Time{}, 0, tuskerError(errorInvalidArg, "improve scan --days must be > 0")
	}
	return nowDate.AddDate(0, 0, -days), days, nil
}

func collectImproveEvidence(vaultPath, repoRoot string, sinceDate time.Time, args Args) ([]improveEvidence, []improveSourceStatus, error) {
	var evidence []improveEvidence
	var sources []improveSourceStatus
	taskEvidence, err := improveTaskEvidence(vaultPath, sinceDate)
	if err != nil {
		return nil, nil, err
	}
	evidence = append(evidence, taskEvidence...)
	sources = append(sources, improveSourceStatus{Name: "Tusker tasks", Status: "enabled", Detail: fmt.Sprintf("%d task summaries", len(taskEvidence))})

	attemptEvidence, err := improveAttemptEvidence(vaultPath, sinceDate)
	if err != nil {
		return nil, nil, err
	}
	evidence = append(evidence, attemptEvidence...)
	sources = append(sources, improveSourceStatus{Name: "Tusker attempts", Status: "enabled", Detail: fmt.Sprintf("%d attempt summaries", len(attemptEvidence))})

	feedbackEvidence, err := improveFeedbackEvidence(vaultPath, repoRoot, sinceDate)
	if err != nil {
		return nil, nil, err
	}
	evidence = append(evidence, feedbackEvidence...)
	sources = append(sources, improveSourceStatus{Name: "Tusker feedback", Status: "enabled", Detail: fmt.Sprintf("%d feedback notes", len(feedbackEvidence))})

	externalEvidence, externalSources := improveExternalEvidence(args)
	evidence = append(evidence, externalEvidence...)
	sources = append(sources, externalSources...)
	return evidence, sources, nil
}

func improveTaskEvidence(vaultPath string, sinceDate time.Time) ([]improveEvidence, error) {
	notes, err := listAllNotes(vaultPath)
	if err != nil {
		return nil, err
	}
	var out []improveEvidence
	for _, note := range notes {
		if stringField(note.Data, "kind") != "task" && !strings.Contains(stringField(note.Data, "schema"), ".task/") {
			continue
		}
		date := firstNonEmpty(frontmatterDateOnly(note.Data, "updated_at"), frontmatterDateOnly(note.Data, "created_at"))
		if !includeImproveDate(date, sinceDate) {
			continue
		}
		title := strings.TrimSpace(stringField(note.Data, "title"))
		if title == "" {
			title = strings.TrimPrefix(firstMarkdownHeading(note.Body), "# ")
		}
		out = append(out, improveEvidence{
			Date:   firstNonEmpty(date, "undated"),
			Source: "task",
			Title:  title,
			Path:   note.RelativePath,
			Text:   title + "\n" + sectionPreview(note.Body, "## Intent", 280),
		})
	}
	return out, nil
}

func improveFeedbackEvidence(vaultPath, repoRoot string, sinceDate time.Time) ([]improveEvidence, error) {
	records, err := feedbackRecordsForVault(vaultPath, repoRoot, sinceDate)
	if err != nil {
		return nil, err
	}
	var out []improveEvidence
	for _, record := range records {
		if len(record.Issues) > 0 {
			continue
		}
		title := firstNonEmpty(record.Theme, record.Fields["product-idea"], record.Fields["friction"])
		out = append(out, improveEvidence{
			Date:     firstNonEmpty(record.Date, "undated"),
			Source:   "feedback",
			Title:    title,
			Path:     record.RelativePath,
			Text:     strings.Join([]string{record.Fields["context"], record.Fields["friction"], record.Fields["product-idea"], record.Fields["impact"], record.Fields["related"]}, "\n"),
			Affected: record.AffectedCommand,
		})
	}
	return out, nil
}

func improveAttemptEvidence(vaultPath string, sinceDate time.Time) ([]improveEvidence, error) {
	attemptsDir := filepath.Join(vaultPath, "attempts")
	if !dirExists(attemptsDir) {
		return nil, nil
	}
	var out []improveEvidence
	if err := walkDirUnsorted(attemptsDir, func(current string, entry fs.DirEntry) error {
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".md") {
			return nil
		}
		data, body, err := parseFrontmatterMustRead(current)
		if err != nil {
			return nil
		}
		date := firstNonEmpty(frontmatterDateOnly(data, "updated_at"), frontmatterDateOnly(data, "created_at"))
		if !includeImproveDate(date, sinceDate) {
			return nil
		}
		rel, _ := filepath.Rel(vaultPath, current)
		title := firstNonEmpty(stringField(data, "title"), stringField(data, "task"), firstMarkdownHeading(body))
		out = append(out, improveEvidence{
			Date:   firstNonEmpty(date, "undated"),
			Source: "attempt",
			Title:  title,
			Path:   filepath.ToSlash(rel),
			Text:   title + "\n" + sectionPreview(body, "## Summary", 300) + "\n" + sectionPreview(body, "## Handoff", 300),
		})
		return nil
	}); err != nil {
		return nil, err
	}
	return out, nil
}

func improveExternalEvidence(args Args) ([]improveEvidence, []improveSourceStatus) {
	var evidence []improveEvidence
	var statuses []improveSourceStatus
	for _, source := range []struct {
		flag string
		name string
		path string
	}{
		{"include-codex", "Codex sessions", "codex-session"},
		{"include-claude", "Claude Code transcripts", "claude-transcript"},
		{"include-memories", "Memories", "memories-path"},
		{"include-chronicle", "Chronicle", "chronicle-path"},
	} {
		if !args.Bool(source.flag) && strings.TrimSpace(args.String(source.path)) == "" {
			statuses = append(statuses, improveSourceStatus{Name: source.name, Status: "disabled", Detail: "explicit opt-in required"})
			continue
		}
		path := strings.TrimSpace(args.String(source.path))
		if path == "" {
			statuses = append(statuses, improveSourceStatus{Name: source.name, Status: "declared", Detail: "enabled without a local summary path; discovery adapter skipped raw private history"})
			continue
		}
		abs, err := filepath.Abs(path)
		if err != nil || !fileExists(abs) {
			statuses = append(statuses, improveSourceStatus{Name: source.name, Status: "unavailable", Detail: "configured path not found"})
			continue
		}
		items := improveSummaryEvidenceFromFile(source.name, abs)
		evidence = append(evidence, items...)
		statuses = append(statuses, improveSourceStatus{Name: source.name, Status: "available", Detail: fmt.Sprintf("%s (%d summary lines)", abs, len(items))})
	}
	return evidence, statuses
}

func improveSummaryEvidenceFromFile(sourceName, path string) []improveEvidence {
	raw, err := readText(path)
	if err != nil {
		return nil
	}
	var out []improveEvidence
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		lower := strings.ToLower(line)
		if !strings.Contains(lower, "workflow") && !strings.Contains(lower, "repeat") && !strings.Contains(lower, "manual") && !strings.Contains(lower, "tusker") {
			continue
		}
		out = append(out, improveEvidence{
			Date:   "undated",
			Source: sourceName,
			Title:  feedbackShort(line, 120),
			Path:   path,
			Text:   line,
		})
		if len(out) >= 10 {
			break
		}
	}
	return out
}

func collectImproveInventory(repoRoot, vaultPath string) improveInventory {
	inventory := improveInventory{}
	for _, rel := range []string{"tusker/SKILL.md", "skill/SKILL.md"} {
		if fileExists(filepath.Join(repoRoot, filepath.FromSlash(rel))) {
			inventory.Skills = append(inventory.Skills, rel)
		}
	}
	inventory.Skills = append(inventory.Skills, improveMarkdownPaths(repoRoot, []string{".agents/skills", ".claude/skills", ".codex/skills"})...)
	inventory.AgentDocs = append(inventory.AgentDocs, improveMarkdownPaths(vaultPath, []string{"docs/agents", "knowledge/domains"})...)
	inventory.Subagents = append(inventory.Subagents, improveMarkdownPaths(repoRoot, []string{".agents/agents", ".claude/agents", ".codex/agents"})...)
	inventory.Automations = append(inventory.Automations, improveMarkdownPaths(repoRoot, []string{"tusker/automations", ".codex/automations", ".github/workflows"})...)
	sort.Strings(inventory.Skills)
	sort.Strings(inventory.AgentDocs)
	sort.Strings(inventory.Subagents)
	sort.Strings(inventory.Automations)
	return inventory
}

func improveMarkdownPaths(root string, relDirs []string) []string {
	var paths []string
	for _, relDir := range relDirs {
		base := filepath.Join(root, filepath.FromSlash(relDir))
		if !dirExists(base) {
			continue
		}
		_ = walkDirUnsorted(base, func(current string, entry fs.DirEntry) error {
			if entry.IsDir() {
				return nil
			}
			name := strings.ToLower(entry.Name())
			if strings.HasSuffix(name, ".md") || strings.HasSuffix(name, ".yaml") || strings.HasSuffix(name, ".yml") {
				rel, err := filepath.Rel(root, current)
				if err == nil {
					paths = append(paths, filepath.ToSlash(rel))
				}
			}
			return nil
		})
	}
	return uniqueStrings(paths)
}

func improveCandidates(evidence []improveEvidence, inventory improveInventory, limit int) []improveCandidate {
	groups := map[string][]improveEvidence{}
	for _, item := range evidence {
		key := improveEvidenceKey(item)
		if key == "" {
			continue
		}
		groups[key] = append(groups[key], item)
	}
	var candidates []improveCandidate
	for key, items := range groups {
		candidate := improveCandidate{
			Key:      key,
			Workflow: improveWorkflowLabel(key, items),
			Evidence: improveSortedEvidence(items),
		}
		candidate.Slug = improveSlug(candidate.Workflow)
		candidate.Confidence = improveConfidence(candidate)
		candidate.ExistingPath = improveExistingCoverage(candidate, inventory)
		candidate.Form = improveRecommendedForm(candidate)
		candidate.WorthCreating, candidate.Why, candidate.SkipReason = improveCreationDecision(candidate)
		candidates = append(candidates, candidate)
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if len(candidates[i].Evidence) != len(candidates[j].Evidence) {
			return len(candidates[i].Evidence) > len(candidates[j].Evidence)
		}
		if improveConfidenceRank(candidates[i].Confidence) != improveConfidenceRank(candidates[j].Confidence) {
			return improveConfidenceRank(candidates[i].Confidence) > improveConfidenceRank(candidates[j].Confidence)
		}
		return candidates[i].Workflow < candidates[j].Workflow
	})
	if limit > 0 && len(candidates) > limit {
		candidates = candidates[:limit]
	}
	return candidates
}

func improveEvidenceKey(item improveEvidence) string {
	text := strings.ToLower(strings.TrimSpace(item.Title + " " + item.Affected + " " + item.Text))
	if item.Source == "feedback" {
		if item.Affected != "" && item.Affected != "n/a" {
			return "command " + strings.Join(improveSignificantTokens(item.Affected), " ")
		}
	}
	tokens := improveSignificantTokens(text)
	if len(tokens) == 0 {
		return ""
	}
	if len(tokens) > 2 {
		tokens = tokens[:2]
	}
	return strings.Join(tokens, " ")
}

func improveSignificantTokens(text string) []string {
	text = strings.ReplaceAll(text, "-", " ")
	words := regexp.MustCompile(`[a-z0-9][a-z0-9-]*`).FindAllString(strings.ToLower(text), -1)
	var out []string
	for _, word := range words {
		word = strings.Trim(word, "-")
		if len(word) < 3 || improveStopwords[word] || strings.HasPrefix(word, "2026") {
			continue
		}
		if matched, _ := regexp.MatchString(`^[a-z]+-[tgd]-[0-9]+$`, word); matched {
			continue
		}
		out = append(out, word)
	}
	return uniqueStringsPreserveOrder(out)
}

func improveConfidence(candidate improveCandidate) string {
	count := len(candidate.Evidence)
	switch {
	case count >= 3:
		return "high"
	case count == 2:
		return "medium"
	default:
		text := strings.ToLower(candidate.Workflow + " " + improveEvidenceText(candidate.Evidence))
		if strings.Contains(text, "repeat") || strings.Contains(text, "recurring") || strings.Contains(text, "again") || strings.Contains(text, "manual") {
			return "low"
		}
		return "low"
	}
}

func improveRecommendedForm(candidate improveCandidate) string {
	if candidate.ExistingPath != "" {
		return "extend existing"
	}
	text := strings.ToLower(candidate.Workflow + " " + improveEvidenceText(candidate.Evidence))
	switch {
	case strings.Contains(text, "daily") || strings.Contains(text, "weekly") || strings.Contains(text, "scheduled") || strings.Contains(text, "monitor") || strings.Contains(text, "reminder") || strings.Contains(text, "cron"):
		return "automation"
	case strings.Contains(text, "review") || strings.Contains(text, "audit") || strings.Contains(text, "investigate") || strings.Contains(text, "triage"):
		return "custom subagent"
	default:
		return "skill"
	}
}

func improveCreationDecision(candidate improveCandidate) (bool, string, string) {
	count := len(candidate.Evidence)
	text := strings.ToLower(candidate.Workflow + " " + improveEvidenceText(candidate.Evidence))
	if strings.Contains(text, "secret") || strings.Contains(text, "token") || strings.Contains(text, "credential") || strings.Contains(text, "production access") {
		return false, "Sensitive or access-heavy workflow; keep as explicit human-owned work.", "sensitive/access-heavy evidence"
	}
	if candidate.ExistingPath != "" {
		return false, "Existing asset already covers this; extend it instead of creating a duplicate.", "covered by " + candidate.ExistingPath
	}
	if count < 2 {
		return false, "Not enough repeat evidence yet.", "needs at least two occurrences or clearer recurrence cost"
	}
	if candidate.Form != "skill" {
		return false, "Recommended form needs a provider-specific apply path; report it for human review first.", "apply currently creates skill/runbook assets only"
	}
	if candidate.Confidence == "high" || candidate.Confidence == "medium" {
		return true, "Repeated, stable-looking workflow with no matching existing asset.", ""
	}
	return false, "Ambiguous recurrence; wait for more evidence.", "low confidence"
}

func applyImproveScan(scan *improveScan, args Args) error {
	for i := range scan.Candidates {
		candidate := &scan.Candidates[i]
		if !candidate.WorthCreating {
			continue
		}
		path := filepath.Join(scan.VaultPath, "docs", "agents", candidate.Slug+".md")
		if fileExists(path) && !args.Bool("force") {
			candidate.ExistingPath = filepath.ToSlash(filepath.Join("docs", "agents", candidate.Slug+".md"))
			candidate.WorthCreating = false
			candidate.SkipReason = "agent doc already exists"
			continue
		}
		if err := writeText(path, renderImproveAgentDoc(*candidate, *scan)); err != nil {
			return err
		}
		rel, _ := filepath.Rel(scan.VaultPath, path)
		scan.Created = append(scan.Created, filepath.ToSlash(rel))
		candidate.ExistingPath = filepath.ToSlash(rel)
		candidate.Form = "skill"
		candidate.WorthCreating = false
		candidate.Why = "Created narrow agent runbook from repeated Tusker evidence."
	}
	classifyImproveScan(scan)
	return nil
}

func classifyImproveScan(scan *improveScan) {
	scan.Skipped = nil
	scan.NeedMore = nil
	for _, candidate := range scan.Candidates {
		if candidate.ExistingPath != "" && strings.HasPrefix(candidate.Why, "Existing asset") {
			scan.Extended = append(scan.Extended, candidate.ExistingPath)
			continue
		}
		if candidate.SkipReason != "" {
			if strings.Contains(candidate.SkipReason, "needs") || strings.Contains(candidate.SkipReason, "low confidence") {
				scan.NeedMore = append(scan.NeedMore, candidate.Workflow+": "+candidate.SkipReason)
			} else {
				scan.Skipped = append(scan.Skipped, candidate.Workflow+": "+candidate.SkipReason)
			}
		}
	}
	scan.Extended = uniqueStrings(scan.Extended)
	scan.Skipped = uniqueStrings(scan.Skipped)
	scan.NeedMore = uniqueStrings(scan.NeedMore)
}

func renderImproveScanMarkdown(scan improveScan) string {
	var b strings.Builder
	b.WriteString("# Tusker Improvement Scan - " + scan.Date + "\n\n")
	b.WriteString("- Window: " + firstNonEmpty(scan.Since, "all available history"))
	if scan.Days > 0 {
		b.WriteString(fmt.Sprintf(" (%d days)", scan.Days))
	}
	b.WriteString("\n")
	b.WriteString("- Mode: " + scan.Mode + "\n")
	b.WriteString("- Execution profile: " + scan.Profile + "; runner=" + scan.Runner + "; model=" + scan.Model + "; reasoning=" + scan.Reasoning + "\n")
	b.WriteString(fmt.Sprintf("- Evidence items: %d\n\n", scan.EvidenceLen))
	b.WriteString("## Shortlist\n\n")
	b.WriteString("| Repeated workflow | Supporting evidence and dates | Frequency/confidence | Recommended form | Why |\n")
	b.WriteString("|---|---|---|---|---|\n")
	if len(scan.Candidates) == 0 {
		b.WriteString("| _None found_ | No repeated Tusker work matched the scan window. | 0/low | skip | Run again after more task or feedback history exists. |\n")
	} else {
		for _, candidate := range scan.Candidates {
			b.WriteString("| " + markdownCell(candidate.Workflow) + " | " + markdownCell(improveEvidenceSummary(candidate.Evidence)) + " | " + fmt.Sprintf("%d/%s", len(candidate.Evidence), candidate.Confidence) + " | " + markdownCell(candidate.Form) + " | " + markdownCell(candidate.Why) + " |\n")
		}
	}
	b.WriteString("\n## Sources\n\n")
	for _, source := range scan.Sources {
		b.WriteString("- " + source.Name + ": " + source.Status + " - " + source.Detail + "\n")
	}
	b.WriteString("\n## Existing Asset Inventory\n\n")
	b.WriteString(fmt.Sprintf("- Skills: %d\n", len(scan.Inventory.Skills)))
	b.WriteString(fmt.Sprintf("- Agent docs: %d\n", len(scan.Inventory.AgentDocs)))
	b.WriteString(fmt.Sprintf("- Custom subagents: %d\n", len(scan.Inventory.Subagents)))
	b.WriteString(fmt.Sprintf("- Automations: %d\n\n", len(scan.Inventory.Automations)))
	b.WriteString("## Created Or Extended\n\n")
	if len(scan.Created) == 0 && len(scan.Extended) == 0 {
		b.WriteString("- None.\n")
	} else {
		for _, path := range scan.Created {
			b.WriteString("- Created " + path + "\n")
		}
		for _, path := range scan.Extended {
			b.WriteString("- Extend existing " + path + "\n")
		}
	}
	b.WriteString("\n## Deliberately Skipped\n\n")
	if len(scan.Skipped) == 0 {
		b.WriteString("- None.\n")
	} else {
		for _, item := range scan.Skipped {
			b.WriteString("- " + item + "\n")
		}
	}
	b.WriteString("\n## Needs More Evidence\n\n")
	if len(scan.NeedMore) == 0 {
		b.WriteString("- None.\n")
	} else {
		for _, item := range scan.NeedMore {
			b.WriteString("- " + item + "\n")
		}
	}
	if scan.ReportPath != "" {
		b.WriteString("\nReport written: " + scan.ReportPath + "\n")
	}
	return b.String()
}

func renderImproveAgentDoc(candidate improveCandidate, scan improveScan) string {
	date := scan.Date
	title := "Agent runbook: " + candidate.Workflow
	lines := []string{
		"---",
		`schema: "tusker.doc/v5"`,
		`id: "agents/` + candidate.Slug + `"`,
		`project: "` + v7ProjectID(scan.VaultPath) + `"`,
		`title: "` + title + `"`,
		`type: "doc"`,
		`node: "agents/` + candidate.Slug + `"`,
		`audience: "agent"`,
		`kind: "runbook"`,
		`status: "draft"`,
		`summary: "Repeatable workflow identified by tusker improve scan."`,
		`canonical_status: "draft"`,
		`publish: false`,
		`created: "` + date + `"`,
		`updated: "` + date + `"`,
		"---",
		"",
		"# " + title,
		"",
		"## Goal",
		"",
		"Make the repeated workflow predictable: " + candidate.Workflow + ".",
		"",
		"## When To Use",
		"",
		"- Use when the task evidence matches the supporting examples below.",
		"- Skip when the work is one-off, sensitive, or lacks a clear stopping condition.",
		"",
		"## Inputs",
		"",
		"- Current Tusker task or user request.",
		"- Existing project skill and agent docs.",
		"- Relevant source files named by the task packet.",
		"",
		"## Procedure",
		"",
		"1. Confirm the task is agent-runnable with `tusker closeout status <TASK-ID> --json`.",
		"2. Search existing tasks and docs before creating anything new.",
		"3. Follow the smallest repeatable workflow that satisfies the current task acceptance.",
		"4. Record proof with the task's configured proof mode.",
		"5. Stop when only human or external gates remain.",
		"",
		"## Output",
		"",
		"- A task update, evidence row/card, or review handoff tied to acceptance IDs.",
		"- No broad prompt baggage or duplicate skill/doc creation.",
		"",
		"## Supporting Evidence",
		"",
	}
	for _, item := range candidate.Evidence {
		lines = append(lines, "- "+item.Date+" "+item.Source+" "+item.Path+": "+item.Title)
	}
	lines = append(lines, "", "## Stale When", "", "- The repeated workflow changes materially.", "- A narrower skill, subagent, automation, or docs page supersedes this runbook.", "")
	return strings.Join(lines, "\n")
}

func improveScanReportPath(scan improveScan, args Args) string {
	if output := strings.TrimSpace(args.String("output")); output != "" {
		abs, err := filepath.Abs(output)
		if err == nil {
			return abs
		}
		return output
	}
	return filepath.Join(scan.VaultPath, "feedback", "improvements", scan.Date+"-improvement-scan.md")
}

func improveScanJSON(scan improveScan) map[string]any {
	var candidates []map[string]any
	for _, candidate := range scan.Candidates {
		candidates = append(candidates, map[string]any{
			"workflow":       candidate.Workflow,
			"frequency":      len(candidate.Evidence),
			"confidence":     candidate.Confidence,
			"form":           candidate.Form,
			"worth_creating": candidate.WorthCreating,
			"existing_path":  nullIfEmptyString(candidate.ExistingPath),
			"why":            candidate.Why,
		})
	}
	return map[string]any{
		"ok":          true,
		"date":        scan.Date,
		"since":       scan.Since,
		"mode":        scan.Mode,
		"profile":     scan.Profile,
		"runner":      scan.Runner,
		"model":       scan.Model,
		"reasoning":   scan.Reasoning,
		"candidates":  candidates,
		"created":     scan.Created,
		"extended":    scan.Extended,
		"skipped":     scan.Skipped,
		"need_more":   scan.NeedMore,
		"report_path": nullIfEmptyString(scan.ReportPath),
	}
}

func frontmatterDateOnly(data map[string]any, key string) string {
	value := strings.TrimSpace(toString(data[key]))
	if value == "" {
		return ""
	}
	if len(value) >= 10 {
		return value[:10]
	}
	return value
}

func includeImproveDate(value string, sinceDate time.Time) bool {
	if sinceDate.IsZero() || value == "" || value == "undated" {
		return true
	}
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil {
		return true
	}
	return !parsed.Before(sinceDate)
}

func firstMarkdownHeading(body string) string {
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "# ") {
			return strings.TrimSpace(line)
		}
	}
	return ""
}

func sectionPreview(body, heading string, limit int) string {
	idx := strings.Index(body, heading)
	if idx < 0 {
		return feedbackShort(body, limit)
	}
	section := body[idx+len(heading):]
	if next := strings.Index(section, "\n## "); next >= 0 {
		section = section[:next]
	}
	return feedbackShort(strings.TrimSpace(section), limit)
}

func improveSortedEvidence(items []improveEvidence) []improveEvidence {
	out := append([]improveEvidence{}, items...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Date != out[j].Date {
			return out[i].Date < out[j].Date
		}
		return out[i].Path < out[j].Path
	})
	return out
}

func improveExistingCoverage(candidate improveCandidate, inventory improveInventory) string {
	needle := strings.Join(improveSignificantTokens(candidate.Workflow), " ")
	if needle == "" {
		return ""
	}
	for _, collection := range [][]string{inventory.AgentDocs, inventory.Skills, inventory.Subagents, inventory.Automations} {
		for _, path := range collection {
			if strings.Contains(strings.Join(improveSignificantTokens(path), " "), needle) {
				return path
			}
		}
	}
	return ""
}

func improveConfidenceRank(value string) int {
	switch value {
	case "high":
		return 3
	case "medium":
		return 2
	case "low":
		return 1
	default:
		return 0
	}
}

func improveEvidenceText(items []improveEvidence) string {
	var parts []string
	for _, item := range items {
		parts = append(parts, item.Title, item.Text)
	}
	return strings.Join(parts, "\n")
}

func improveEvidenceSummary(items []improveEvidence) string {
	var parts []string
	for _, item := range items {
		parts = append(parts, item.Date+" "+item.Source+" "+item.Path)
	}
	if len(parts) > 3 {
		parts = append(parts[:3], fmt.Sprintf("+%d more", len(parts)-3))
	}
	return strings.Join(parts, "; ")
}

func improveTitleFromKey(key string) string {
	key = strings.TrimPrefix(key, "command ")
	words := strings.Fields(key)
	for i, word := range words {
		words[i] = strings.Trim(word, "-")
	}
	return strings.Join(words, " ")
}

func improveWorkflowLabel(key string, items []improveEvidence) string {
	if len(items) == 0 {
		return improveTitleFromKey(key)
	}
	common := improveSignificantTokens(items[0].Title)
	if len(common) > 4 {
		common = common[:4]
	}
	for _, item := range items[1:] {
		tokens := improveSignificantTokens(item.Title)
		limit := 0
		for limit < len(common) && limit < len(tokens) && common[limit] == tokens[limit] {
			limit++
		}
		common = common[:limit]
	}
	if len(common) >= 2 {
		return strings.Join(common, " ")
	}
	return improveTitleFromKey(key)
}

func improveSlug(value string) string {
	return feedbackSlug(strings.TrimPrefix(value, "command "), "improvement")
}

func markdownCell(value string) string {
	value = strings.ReplaceAll(value, "\n", " ")
	value = strings.ReplaceAll(value, "|", "\\|")
	return strings.TrimSpace(value)
}

func uniqueStringsPreserveOrder(values []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, value := range values {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func printImproveHelp() {
	fmt.Println(`Tusker improve

Commands:
  tusker improve scan [--days 30|--since <YYYY-MM-DD>|--all] [--write]
  tusker improve scan --apply [--runner <name>] [--model <name>] [--reasoning low|medium|high]

Purpose:
  Opt-in scan for repeated manual workflows worth packaging as the smallest
  useful reusable asset. The default is a dry-run report. --apply is required
  before Tusker creates narrow agent runbook drafts under tusker/docs/agents/.

Evidence order:
  1. Tusker tasks, attempts, proof summaries, and feedback notes.
  2. Existing project skills, agent docs, custom agents, and automations.
  3. Optional external source summaries only when explicitly enabled.

Options:
  --days <n>             Look back n days. Default: 30.
  --since <YYYY-MM-DD>   Start date. Overrides --days.
  --all                  Scan all available Tusker history.
  --max-candidates <n>   Limit shortlist size. Default: 8.
  --write                Write report to tusker/feedback/improvements/.
  --apply                Create high-confidence missing skill/runbook assets.
  --force                Allow --apply to replace an existing generated runbook.
  --profile <name>       Runtime profile label, e.g. cheap-discovery or review.
  --runner <name>        Preferred runner label. Stored only in the report.
  --model <name>         Preferred model label. Stored only in the report.
  --reasoning <level>    Preferred reasoning label. Stored only in the report.
  --include-codex        Opt into Codex session discovery.
  --codex-session <path> Local Codex summary/session path; raw private history is never read by default.
  --include-claude       Opt into Claude Code transcript discovery.
  --claude-transcript <path>
                         Local Claude summary/transcript path.
  --include-memories     Opt into a configured memory summary path.
  --memories-path <path> Local memory summary path.
  --include-chronicle    Opt into Chronicle discovery.
  --chronicle-path <path>
                         Local Chronicle summary path.

Examples:
  tusker improve scan
  tusker improve scan --since 2026-05-01 --write
  tusker improve scan --apply --profile cheap-discovery --runner codex --model gpt-5.3-codex-spark
  tusker improve scan --include-codex --codex-session ~/.codex/sessions/summary.jsonl`)
}
