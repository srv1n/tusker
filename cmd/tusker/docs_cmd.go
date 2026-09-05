package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"tusker/internal/docgraph"
)

// docsCmd dispatches the docs subcommands through a table so new subcommands
// (map is wired at integration) slot in without touching the caller, and an
// unknown subcommand fails cleanly rather than falling through.
func docsCmd(subcommand string, args Args) error {
	handlers := map[string]func(Args) error{
		"find":   docsFindCmd,
		"new":    docsNewCmd,
		"map":    docsMapCmd,
		"status": docsStatusCmd,
		"verify": docsVerifyCmd,
		"adopt":  docsAdoptCmd,
	}
	handler, ok := handlers[subcommand]
	if !ok {
		return tuskerError(errorInvalidArg, "unknown docs subcommand: "+subcommand)
	}
	return handler(args)
}

func docsStatusCmd(args Args) error {
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	result, err := docgraph.DocsFreshness(v7RepoRoot(vaultPath))
	if err != nil {
		return err
	}
	gaps, err := docgraph.DocsCoverageGaps(v7RepoRoot(vaultPath))
	if err != nil {
		return err
	}
	if args.Bool("json") {
		rows := make([]map[string]any, 0, len(result))
		for _, item := range result {
			rows = append(rows, map[string]any{
				"subject": item.Document.Subject, "path": item.Document.Path,
				"last_verified": item.Document.LastVerified, "touching_commits": item.TouchingCommits,
				"never_verified": item.NeverVerified,
			})
		}
		emitJSON(map[string]any{"documents": rows, "coverage_gaps": gaps})
		return nil
	}
	for _, item := range result {
		freshness := fmt.Sprintf("%d commits since %s", item.TouchingCommits, item.Document.LastVerified)
		if item.NeverVerified {
			freshness = "never verified"
		}
		fmt.Printf("%s — %s — %s\n", item.Document.Path, item.Document.Subject, freshness)
	}
	if len(gaps) > 0 {
		fmt.Println("\nCoverage gaps:")
		for _, gap := range gaps {
			fmt.Printf("  %s\n", gap)
		}
	}
	return nil
}

func docsVerifyCmd(args Args) error {
	subject := positionalPhrase(args)
	if subject == "" {
		return tuskerError(errorMissingArg, "docs verify needs a subject, for example: tusker docs verify worktree-lifecycle")
	}
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	date := time.Now().Local().Format("2006-01-02")
	repoRoot := v7RepoRoot(vaultPath)
	path, err := docgraph.StampDocument(repoRoot, subject, date)
	if err != nil {
		if errors.Is(err, docgraph.ErrDocumentNotFound) {
			return tuskerError(errorNotFound, fmt.Sprintf("document subject %q does not exist", subject))
		}
		return err
	}
	if args.Bool("json") {
		commit, commitErr := docgraph.CurrentCommit(repoRoot)
		if commitErr != nil {
			return commitErr
		}
		emitJSON(map[string]any{"ok": true, "path": path, "subject": subject, "last_verified": date + " @ " + commit})
		return nil
	}
	fmt.Printf("Verified %s (%s)\n", subject, path)
	return nil
}

const docsTouchRuleDefault = "warn"

func v7DocsTouchRule(vaultPath string) (string, error) {
	resolved, err := resolveTuskerConfig(vaultPath)
	if err != nil {
		return "", err
	}
	rule := docsTouchRuleDefault
	if value, ok := lookupConfigValue(resolved.Raw, "docs.touch_rule"); ok {
		rule = strings.ToLower(strings.TrimSpace(fmt.Sprint(value)))
	}
	switch rule {
	case "off", "warn", "block":
		return rule, nil
	default:
		return "", tuskerError(errorConfigInvalid, `docs.touch_rule must be one of "warn", "block", or "off"`)
	}
}

func v7DocTouchCheck(vaultPath string, task Note) error {
	rule, err := v7DocsTouchRule(vaultPath)
	if err != nil {
		return err
	}
	if rule == "off" {
		return nil
	}
	corpus, _, err := docgraph.LoadRepository(v7RepoRoot(vaultPath))
	if err != nil {
		return err
	}
	hasDescribes := false
	for _, doc := range corpus.Documents {
		if len(doc.Describes) > 0 {
			hasDescribes = true
			break
		}
	}
	if !hasDescribes {
		return nil
	}
	changed, available := v7DocTouchChangedPaths(vaultPath, task)
	if !available {
		message := fmt.Sprintf("doc-touch check unavailable for %s; run tusker gate-run/land to check documentation drift", stringField(task.Data, "id"))
		if rule == "block" {
			return tuskerError("DOC_TOUCH_AUTHORITY_UNAVAILABLE", "close blocked: "+message, withHint("record a resolvable task source/base commit or use docs.touch_rule: warn during probation"))
		}
		fmt.Fprintln(os.Stderr, "warning: "+message)
		return nil
	}
	report := docgraph.CheckDocTouch(corpus, changed, parseV7DocTouchWaivers(task.Body))
	if len(report.Missing) == 0 {
		return nil
	}
	for _, doc := range report.Missing {
		fmt.Fprintf(os.Stderr, "warning: doc-touch drift: %s (%s) describes changed code; edit the doc in this diff or add `doc_unchanged | %s | waived | <reason>` to Verification\n", doc.Subject, doc.Path, doc.Subject)
	}
	if rule == "block" {
		missing := make([]string, 0, len(report.Missing))
		for _, doc := range report.Missing {
			missing = append(missing, doc.Subject)
		}
		return tuskerError("DOC_TOUCH_DRIFT", "close blocked by documentation drift: "+strings.Join(missing, ", "), withContext(map[string]any{"documents": missing, "changed_paths": changed}))
	}
	return nil
}

func parseV7DocTouchWaivers(body string) map[string]bool {
	waivers := map[string]bool{}
	for _, row := range parseV7VerificationRows(body) {
		if !strings.EqualFold(strings.TrimSpace(row.CoverText), "doc_unchanged") || !strings.EqualFold(strings.TrimSpace(row.Result), "waived") {
			continue
		}
		subject := strings.TrimSpace(row.Check)
		if subject != "" && strings.TrimSpace(row.Notes) != "" {
			waivers[subject] = true
		}
	}
	return waivers
}

func v7DocTouchChangedPaths(vaultPath string, task Note) ([]string, bool) {
	repoRoot := v7RepoRoot(vaultPath)
	if !v7GitRepo(repoRoot) {
		return nil, false
	}
	source := firstNonEmpty(
		stringField(task.Data, "source_sha"),
		stringField(task.Data, "source_commit"),
		stringField(task.Data, "source_branch_sha"),
	)
	if source != "" {
		if base := v7DocTouchBaseRevision(vaultPath, task); base != "" {
			if output, err := gitCombined(repoRoot, "diff", "--name-only", "-z", base+".."+source); err == nil {
				return splitGitPaths(output), true
			}
		}
		if output, err := gitCombined(repoRoot, "diff", "--name-only", "-z", source+"^", source); err == nil {
			return splitGitPaths(output), true
		}
		if output, err := gitCombined(repoRoot, "diff-tree", "--root", "--no-commit-id", "--name-only", "-r", "-z", source); err == nil {
			return splitGitPaths(output), true
		}
	}
	branch := strings.TrimSpace(stringField(task.Data, "branch"))
	if branch == "" {
		branch = v7TaskBranchName(stringField(task.Data, "id"))
	}
	if head, ok := gitRevParse(repoRoot, branch+"^{commit}"); ok {
		if output, err := gitCombined(repoRoot, "diff", "--name-only", "-z", head+"^", head); err == nil {
			return splitGitPaths(output), true
		}
	}
	return nil, false
}

// v7DocTouchBaseRevision resolves the recorded task/work range when one is
// available. A single source commit is only the fallback: close-time drift
// needs every path changed since the implementation branch's reviewed base.
func v7DocTouchBaseRevision(vaultPath string, task Note) string {
	for _, key := range []string{"base_sha", "source_base_sha", "branch_base_sha", "review_base_sha", "integration_base_sha"} {
		if value := strings.TrimSpace(stringField(task.Data, key)); value != "" {
			return value
		}
	}
	waveID := strings.TrimSpace(stringField(task.Data, "wave"))
	if waveID == "" {
		return ""
	}
	idx, err := loadV7Index(vaultPath)
	if err != nil {
		return ""
	}
	if wave, ok := idx.Waves[waveID]; ok {
		return strings.TrimSpace(stringField(wave.Data, "integration_base_sha"))
	}
	return ""
}

func splitGitPaths(output string) []string {
	seen := map[string]bool{}
	var paths []string
	for _, path := range strings.Split(output, "\x00") {
		path = filepath.ToSlash(filepath.Clean(path))
		if path != "" && path != "." && !seen[path] {
			seen[path] = true
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)
	return paths
}

func docsFindCmd(args Args) error {
	query := positionalPhrase(args)
	if query == "" {
		return tuskerError(errorMissingArg, "docs find needs a query, for example: tusker docs find worktree")
	}
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	corpus, _, err := docgraph.LoadRepository(v7RepoRoot(vaultPath))
	if err != nil {
		return err
	}
	result := docgraph.Find(corpus, query)
	if args.Bool("json") {
		emitJSON(result)
		return nil
	}
	fmt.Print(renderDocsFind(result))
	return nil
}

func renderDocsFind(result docgraph.FindResult) string {
	var builder strings.Builder
	if len(result.Matches) == 0 {
		fmt.Fprintf(&builder, "No document matches %q.\n", result.Query)
		if len(result.Suggestions) > 0 {
			builder.WriteString("Closest subjects:\n")
			for _, suggestion := range result.Suggestions {
				fmt.Fprintf(&builder, "  %s\n", suggestion)
			}
		}
		return builder.String()
	}
	for _, match := range result.Matches {
		line := match.Path
		if match.Description != "" {
			line += " — " + match.Description
		}
		if match.ReadWhen != "" {
			line += " — read when: " + match.ReadWhen
		}
		if match.SkipWhen != "" {
			line += " — skip when: " + match.SkipWhen
		}
		if match.ResolvedFrom != "" {
			line += fmt.Sprintf(" (resolved forward from superseded %q)", match.ResolvedFrom)
		}
		builder.WriteString(line + "\n")
	}
	if result.Truncated {
		fmt.Fprintf(&builder, "Showing %d of %d matches; use the subject or path above to open the full document.\n", len(result.Matches), result.TotalMatches)
	}
	return builder.String()
}

func docsMapCmd(args Args) error {
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	repoRoot := v7RepoRoot(vaultPath)
	if err := docgraph.WriteDocsMap(repoRoot); err != nil {
		return err
	}
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": true})
		return nil
	}
	fmt.Println("Docs map, index, and graph regenerated.")
	return nil
}

func docsNewCmd(args Args) error {
	subject := positionalPhrase(args)
	if subject == "" {
		return tuskerError(errorMissingArg, "docs new needs a subject, for example: tusker docs new worktree-lifecycle")
	}
	kind := strings.TrimSpace(strings.ToLower(args.String("kind")))
	if kind == "" {
		kind = "doc"
	}
	var targetDir string
	switch kind {
	case "doc":
		targetDir = "docs/system"
	case "spec":
		targetDir = ".tusker/specs"
	default:
		return tuskerError(errorInvalidArg, `docs new --kind must be "doc" or "spec", got: `+kind)
	}

	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	repoRoot := v7RepoRoot(vaultPath)
	corpus, _, err := docgraph.LoadRepository(repoRoot)
	if err != nil {
		return err
	}
	for _, doc := range corpus.Documents {
		if strings.EqualFold(strings.TrimSpace(doc.Subject), subject) {
			return tuskerError(errorAlreadyExists, fmt.Sprintf("a document about %q already exists at %s; update it in place instead of starting a second copy", subject, doc.Path))
		}
	}

	relative := filepath.ToSlash(filepath.Join(targetDir, docsSubjectSlug(subject)+".md"))
	absolute := filepath.Join(repoRoot, filepath.FromSlash(relative))
	if fileExists(absolute) {
		return tuskerError(errorAlreadyExists, "a file already exists at "+relative+"; pick a different subject or update that file")
	}
	if err := docsAdoptWriteText(repoRoot, relative, docsScaffold(subject, kind)); err != nil {
		return err
	}
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": true, "path": relative, "subject": subject, "kind": kind})
		return nil
	}
	fmt.Printf("Created %s\n", relative)
	return nil
}

func positionalPhrase(args Args) string {
	return strings.TrimSpace(strings.ReplaceAll(args.String("_pos"), "\n", " "))
}

var docsSlugPattern = regexp.MustCompile(`[^a-z0-9]+`)

func docsSubjectSlug(subject string) string {
	slug := docsSlugPattern.ReplaceAllString(strings.ToLower(strings.TrimSpace(subject)), "-")
	slug = strings.Trim(slug, "-")
	if slug == "" {
		return "untitled"
	}
	return slug
}

func docsScaffold(subject, kind string) string {
	created := time.Now().Local().Format("2006-01-02")
	var builder strings.Builder
	builder.WriteString("---\n")
	fmt.Fprintf(&builder, "subject: %s            # unique key; the one right name for this document\n", subject)
	builder.WriteString("keywords: []            # search aliases a reader might type instead of the subject\n")
	builder.WriteString("part_of:                # subject of the parent document this sits under\n")
	builder.WriteString("describes: []           # coarse repository paths this document explains\n")
	builder.WriteString("status: canonical       # canonical, or superseded (then set superseded_by)\n")
	fmt.Fprintf(&builder, "created: %s      # date this document was first written\n", created)
	builder.WriteString("last_verified:          # date @ commit this document was last checked against code\n")
	builder.WriteString("read_when: \"\"           # one line: when a reader should open this\n")
	builder.WriteString("skip_when: \"\"           # one line: when a reader should look elsewhere\n")
	if kind == "spec" {
		builder.WriteString("sources: []             # links or records supporting this contract\n")
		builder.WriteString("decisions_locked: false # set true only when its updates are ready to land\n")
	}
	builder.WriteString("---\n\n")
	fmt.Fprintf(&builder, "# %s\n", subject)
	return builder.String()
}
