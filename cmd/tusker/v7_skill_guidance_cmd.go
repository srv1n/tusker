package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type v7AgentGuidanceFinding struct {
	Path           string `json:"path"`
	Classification string `json:"classification"`
	Summary        string `json:"summary"`
	Lines          int    `json:"lines"`
	Content        string `json:"content,omitempty"`
}

type v7AgentGuidanceAudit struct {
	RepoRoot       string                   `json:"repo_root"`
	VaultPath      string                   `json:"vault_path"`
	Findings       []v7AgentGuidanceFinding `json:"findings"`
	Warnings       []Issue                  `json:"warnings"`
	DraftPath      string                   `json:"draft_path,omitempty"`
	PointerUpdates []string                 `json:"pointer_updates,omitempty"`
}

func skillV7AuditAgentGuidanceCmd(args Args) (int, error) {
	repoRoot, vaultPath, err := v7AgentGuidanceAuditRoots(args)
	if err != nil {
		return 0, err
	}
	pointerUpdates := []repoPointerUpdate{}
	if args.Bool("write") {
		var err error
		pointerUpdates, err = upsertRepoTuskerPointers(repoRoot, vaultPath)
		if err != nil {
			return 0, err
		}
	}
	audit, err := auditV7AgentGuidance(repoRoot, vaultPath)
	if err != nil {
		return 0, err
	}
	audit.PointerUpdates = repoPointerUpdatePaths(pointerUpdates)
	if args.Bool("draft") || (args.Bool("write") && len(audit.Findings) > 0) {
		draftPath, err := writeV7AgentGuidanceMigrationDraft(audit, args)
		if err != nil {
			return 0, err
		}
		audit.DraftPath = draftPath
	}
	if args.Bool("json") {
		emitJSON(map[string]any{
			"ok":       len(audit.Findings) == 0 && len(audit.Warnings) == 0,
			"repo":     audit.RepoRoot,
			"vault":    audit.VaultPath,
			"findings": audit.Findings,
			"warnings": audit.Warnings,
			"draft":    nullIfEmptyString(audit.DraftPath),
			"updated":  audit.PointerUpdates,
		})
		if len(audit.Findings) > 0 || len(audit.Warnings) > 0 {
			return 1, nil
		}
		return 0, nil
	}
	if len(audit.Warnings) > 0 {
		fmt.Println("Warnings:")
		for _, warning := range audit.Warnings {
			fmt.Printf("- %s\n", formatIssue(warning))
		}
	}
	for _, updated := range audit.PointerUpdates {
		fmt.Println("Updated bootstrap: " + updated)
	}
	if len(audit.Findings) == 0 {
		if !args.Bool("quiet") {
			fmt.Println("No non-managed AGENTS/CLAUDE guidance found.")
		}
		return 0, nil
	}
	fmt.Println("Non-managed agent guidance:")
	for _, finding := range audit.Findings {
		fmt.Printf("- %s [%s] %s\n", finding.Path, finding.Classification, finding.Summary)
	}
	if audit.DraftPath != "" {
		fmt.Println("Migration draft: " + audit.DraftPath)
	}
	return 1, nil
}

func v7AgentGuidanceAuditRoots(args Args) (string, string, error) {
	if repoArg := strings.TrimSpace(args.String("repo")); repoArg != "" {
		repoRoot, err := filepath.Abs(repoArg)
		if err != nil {
			return "", "", err
		}
		vaultArg := strings.TrimSpace(args.String("vault"))
		if vaultArg != "" && !filepath.IsAbs(vaultArg) {
			return repoRoot, filepath.Join(repoRoot, vaultArg), nil
		}
		vaultPath, err := repoTuskerVaultPath(repoRoot, vaultArg)
		if err != nil {
			return "", "", err
		}
		return repoRoot, vaultPath, nil
	}
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return "", "", err
	}
	return v7RepoRoot(vaultPath), vaultPath, nil
}

func auditV7AgentGuidance(repoRoot, vaultPath string) (v7AgentGuidanceAudit, error) {
	files, err := listV7AgentGuidanceFiles(repoRoot)
	if err != nil {
		return v7AgentGuidanceAudit{}, err
	}
	audit := v7AgentGuidanceAudit{RepoRoot: repoRoot, VaultPath: vaultPath}
	for _, path := range files {
		raw, err := readText(path)
		if err != nil {
			return v7AgentGuidanceAudit{}, err
		}
		content := strings.TrimSpace(stripTuskerManagedGuidance(raw))
		if content == "" {
			continue
		}
		rel := v7GuidancePathForMessage(repoRoot, path)
		audit.Findings = append(audit.Findings, v7AgentGuidanceFinding{
			Path:           rel,
			Classification: classifyV7AgentGuidance(content),
			Summary:        summarizeV7AgentGuidance(content),
			Lines:          countNonEmptyLines(content),
			Content:        content,
		})
	}
	sort.Slice(audit.Findings, func(i, j int) bool {
		if audit.Findings[i].Path != audit.Findings[j].Path {
			return audit.Findings[i].Path < audit.Findings[j].Path
		}
		return audit.Findings[i].Classification < audit.Findings[j].Classification
	})
	if len(audit.Findings) > 0 && !fileExists(filepath.Join(vaultPath, "SKILL.md")) {
		audit.Warnings = append(audit.Warnings, issue(
			"PROJECT_SKILL_MISSING_FOR_AGENT_GUIDANCE",
			"repo has non-managed AGENTS/CLAUDE guidance but no tusker/SKILL.md project skill",
			v7PathForMessage(vaultPath, filepath.Join(vaultPath, "SKILL.md")),
			"create tusker/SKILL.md or review a migration draft before flattening root agent files",
			nil,
		))
	}
	audit.Warnings = append(audit.Warnings, auditV7TuskerBootstrapWarnings(repoRoot, vaultPath)...)
	return audit, nil
}

func auditV7TuskerBootstrapWarnings(repoRoot, vaultPath string) []Issue {
	current := renderTuskerPointerBlock(repoTuskerReadmeLink(repoRoot, vaultPath))
	hasCurrent := false
	hasManaged := false
	for _, filename := range []string{"AGENTS.md", "CLAUDE.md"} {
		path := filepath.Join(repoRoot, filename)
		if !fileExists(path) {
			continue
		}
		text, err := readText(path)
		if err != nil {
			continue
		}
		if strings.Contains(text, current) {
			hasCurrent = true
		}
		if strings.Contains(text, tuskerPointerBegin) || strings.Contains(text, tuskerPointerEnd) {
			hasManaged = true
		}
	}
	if hasCurrent {
		return nil
	}
	action := fmt.Sprintf("run `tusker skill audit-agent-guidance --repo %s --write` or `tusker install --repo %s --no-bin`", repoRoot, repoRoot)
	if hasManaged {
		return []Issue{issue(
			"TUSKER_BOOTSTRAP_STALE",
			"managed AGENTS/CLAUDE Tusker bootstrap is stale",
			"AGENTS.md/CLAUDE.md",
			action,
			nil,
		)}
	}
	return []Issue{issue(
		"TUSKER_BOOTSTRAP_MISSING",
		"repo is missing the managed AGENTS/CLAUDE Tusker bootstrap",
		"AGENTS.md/CLAUDE.md",
		action,
		nil,
	)}
}

func listV7AgentGuidanceFiles(repoRoot string) ([]string, error) {
	var files []string
	if err := filepath.WalkDir(repoRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil || entry == nil {
			return nil
		}
		if entry.IsDir() {
			if path == repoRoot {
				return nil
			}
			switch entry.Name() {
			case ".git", ".tusker", ".tusker-local", ".tusker-runtime", "node_modules", "vendor", "dist", "build", "tusker":
				return filepath.SkipDir
			}
			return nil
		}
		name := entry.Name()
		if name == "AGENTS.md" || name == "CLAUDE.md" {
			files = append(files, path)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	sort.Strings(files)
	return files, nil
}

func stripTuskerManagedGuidance(text string) string {
	for {
		begin := strings.Index(text, tuskerPointerBegin)
		end := strings.Index(text, tuskerPointerEnd)
		if begin == -1 || end == -1 || end <= begin {
			break
		}
		text = text[:begin] + text[end+len(tuskerPointerEnd):]
	}
	return strings.TrimSpace(text)
}

func classifyV7AgentGuidance(content string) string {
	lower := strings.ToLower(content)
	scores := map[string]int{
		"project_knowledge":    scoreContains(lower, "architecture", "canon", "domain", "product", "business", "model", "api", "database", "source of truth"),
		"workflow_rule":        scoreContains(lower, "must", "never", "always", "do not", "required", "forbidden", "protected", "branch", "owner"),
		"verification_recipe":  scoreContains(lower, "test", "verify", "validation", "lint", "build", "rtk", "go test", "pytest", "npm test"),
		"stale_prompt_baggage": scoreContains(lower, "you are", "assistant", "persona", "vibe", "humor", "corporate", "token", "prompt"),
	}
	order := []string{"verification_recipe", "workflow_rule", "project_knowledge", "stale_prompt_baggage"}
	best := "workflow_rule"
	bestScore := 0
	for _, key := range order {
		if scores[key] > bestScore {
			best = key
			bestScore = scores[key]
		}
	}
	return best
}

func scoreContains(text string, needles ...string) int {
	score := 0
	for _, needle := range needles {
		if strings.Contains(text, needle) {
			score++
		}
	}
	return score
}

func summarizeV7AgentGuidance(content string) string {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(strings.TrimLeft(line, "#-*0123456789. \t"))
		if line == "" {
			continue
		}
		if len(line) > 120 {
			return strings.TrimSpace(line[:117]) + "..."
		}
		return line
	}
	return "non-managed guidance"
}

func countNonEmptyLines(content string) int {
	count := 0
	for _, line := range strings.Split(content, "\n") {
		if strings.TrimSpace(line) != "" {
			count++
		}
	}
	return count
}

func writeV7AgentGuidanceMigrationDraft(audit v7AgentGuidanceAudit, args Args) (string, error) {
	target := strings.ToLower(firstNonEmpty(args.String("target"), args.String("to"), "feedback"))
	date := time.Now().UTC().Format("2006-01-02")
	var rel string
	switch target {
	case "knowledge":
		rel = filepath.ToSlash(filepath.Join("knowledge", date+"-agent-guidance-migration-draft.md"))
	default:
		rel = filepath.ToSlash(filepath.Join("feedback", "agents", date+"-agent-guidance-migration-draft.md"))
	}
	path := uniqueV7AgentGuidanceDraftPath(filepath.Join(audit.VaultPath, filepath.FromSlash(rel)))
	if err := ensureDir(filepath.Dir(path)); err != nil {
		return "", err
	}
	if err := writeText(path, renderV7AgentGuidanceMigrationDraft(audit)); err != nil {
		return "", err
	}
	return v7PathForMessage(audit.VaultPath, path), nil
}

func uniqueV7AgentGuidanceDraftPath(path string) string {
	if !fileExists(path) {
		return path
	}
	ext := filepath.Ext(path)
	base := strings.TrimSuffix(path, ext)
	for i := 2; ; i++ {
		candidate := fmt.Sprintf("%s-%02d%s", base, i, ext)
		if !fileExists(candidate) {
			return candidate
		}
	}
}

func renderV7AgentGuidanceMigrationDraft(audit v7AgentGuidanceAudit) string {
	var b strings.Builder
	b.WriteString("# Agent Guidance Migration Draft\n\n")
	b.WriteString("Review this draft before flattening root AGENTS.md or CLAUDE.md files.\n\n")
	b.WriteString("| Source | Classification | Lines | Summary |\n")
	b.WriteString("|---|---|---:|---|\n")
	for _, finding := range audit.Findings {
		fmt.Fprintf(&b, "| `%s` | %s | %d | %s |\n", finding.Path, finding.Classification, finding.Lines, markdownTableCell(finding.Summary))
	}
	for _, finding := range audit.Findings {
		fmt.Fprintf(&b, "\n## %s\n\nClassification: `%s`\n\n```markdown\n%s\n```\n", finding.Path, finding.Classification, strings.TrimSpace(finding.Content))
	}
	return b.String()
}

func markdownTableCell(value string) string {
	value = strings.ReplaceAll(value, "|", "\\|")
	value = strings.ReplaceAll(value, "\n", " ")
	return value
}

func v7GuidancePathForMessage(repoRoot, path string) string {
	if rel, err := filepath.Rel(repoRoot, path); err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
		return filepath.ToSlash(rel)
	}
	return filepath.ToSlash(path)
}
