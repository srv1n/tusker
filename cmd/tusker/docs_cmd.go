package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"tusker/internal/docgraph"
)

// docsCmd dispatches the docs subcommands through a table so new subcommands
// (map is wired at integration) slot in without touching the caller, and an
// unknown subcommand fails cleanly rather than falling through.
func docsCmd(subcommand string, args Args) error {
	handlers := map[string]func(Args) error{
		"find":      docsFindCmd,
		"new":       docsNewCmd,
		"map":       docsMapCmd,
		"status":    docsStatusCmd,
		"verify":    docsVerifyCmd,
		"adopt":     docsAdoptCmd,
		"browse":    docsBrowseCmd,
		"read":      docsReadCmd,
		"backlinks": docsBacklinksCmd,
		"check":     docsCheckCmd,
		"touch":     docsTouchCmd,
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
	staleOnly := args.Bool("stale")
	staleCount := 0
	filtered := result[:0]
	for _, item := range result {
		if item.Stale {
			staleCount++
		}
		if !staleOnly || item.Stale {
			filtered = append(filtered, item)
		}
	}
	result = filtered
	if args.Bool("json") {
		rows := make([]map[string]any, 0, len(result))
		for _, item := range result {
			rows = append(rows, map[string]any{
				"subject": item.Document.Subject, "path": item.Document.Path,
				"last_verified": item.Document.LastVerified, "touching_commits": item.TouchingCommits,
				"never_verified": item.NeverVerified, "stale": item.Stale,
			})
		}
		emitJSON(map[string]any{"documents": rows, "coverage_gaps": gaps})
	} else {
		for _, item := range result {
			lastVerified := strings.TrimSpace(item.Document.LastVerified)
			freshness := fmt.Sprintf("%d commits since %s", item.TouchingCommits, lastVerified)
			switch {
			case len(item.Document.Describes) == 0:
				freshness = "no describes"
			case item.NeverVerified && lastVerified != "" && !strings.Contains(lastVerified, " @ "):
				freshness = "date-only stamp, run tusker docs verify " + item.Document.Subject
			case item.NeverVerified:
				freshness = "never verified"
			case item.Stale:
				freshness = "STALE — " + freshness
			}
			fmt.Printf("%s — %s — %s\n", item.Document.Path, item.Document.Subject, freshness)
		}
		if len(gaps) > 0 && !staleOnly {
			fmt.Println("\nCoverage gaps:")
			for _, gap := range gaps {
				fmt.Printf("  %s\n", gap)
			}
		}
	}
	if staleOnly && args.Bool("strict") && staleCount > 0 {
		err := tuskerError("DOCS_STALE", fmt.Sprintf("%d stale document(s): described code changed since last_verified", staleCount))
		if args.Bool("json") {
			return afterResultEmitted(err)
		}
		return err
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
		return tuskerError(errorMissingArg, "docs find needs a query, for example: tusker list docs --grep worktree")
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

var docsDomainPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

// docsPortableKind normalizes the --kind flag to its portable document kind.
// "spec" stays accepted as the compatibility spelling for proposal.
func docsPortableKind(raw string) (docgraph.Kind, bool) {
	switch strings.TrimSpace(strings.ToLower(raw)) {
	case "", "doc":
		return docgraph.KindDoc, true
	case "proposal", "spec":
		return docgraph.KindProposal, true
	case "decision":
		return docgraph.KindDecision, true
	default:
		return "", false
	}
}

func docsNewCmd(args Args) error {
	subject := positionalPhrase(args)
	if subject == "" {
		return tuskerError(errorMissingArg, "docs new needs a subject, for example: tusker new doc worktree-lifecycle")
	}
	kind, ok := docsPortableKind(args.String("kind"))
	if !ok {
		return tuskerError(errorInvalidArg, `docs new --kind must be "doc", "proposal", "decision" or "spec", got: `+strings.TrimSpace(args.String("kind")))
	}
	domain := strings.TrimSpace(args.String("domain"))
	if domain != "" && !docsDomainPattern.MatchString(domain) {
		return tuskerError(errorInvalidArg, `docs new --domain must be a lowercase slug like "billing", got: `+domain)
	}
	// Every managed document carries one-line triage that docs find and
	// browse show agents before they open it, so creation requires both.
	readWhen := strings.Join(strings.Fields(args.String("read-when")), " ")
	skipWhen := strings.Join(strings.Fields(args.String("skip-when")), " ")
	if readWhen == "" || skipWhen == "" {
		return tuskerError(errorMissingArg, `docs new needs --read-when and --skip-when, one line each, for example: --read-when "Changing worktree creation or cleanup." --skip-when "You need task lifecycle rather than worktree mechanics."`)
	}
	wantIndex := args.Bool("index")
	if wantIndex && (kind != docgraph.KindDoc || domain == "") {
		return tuskerError(errorInvalidArg, "docs new --index scaffolds a domain reading index: use it with --kind doc (or no --kind) and --domain <name>")
	}
	if domain != "" && kind != docgraph.KindDoc {
		return tuskerError(errorInvalidArg, "docs new --domain applies to docs only; proposals live in docs/system/proposals and decisions in docs/system/decisions")
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

	var relative, parent, decidesFor string
	switch {
	case kind == docgraph.KindDoc && wantIndex:
		relative = filepath.ToSlash(filepath.Join("docs/system/domains", domain, "00-index.md"))
		parent, err = docsScaffoldParent(corpus)
		if err != nil {
			return err
		}
	case kind == docgraph.KindDoc && domain != "":
		indexSubject, indexErr := docsDomainIndexSubject(corpus, domain)
		if indexErr != nil {
			return indexErr
		}
		relative = filepath.ToSlash(filepath.Join("docs/system/domains", domain, docsSubjectSlug(subject)+".md"))
		parent = indexSubject
	case kind == docgraph.KindDoc:
		relative = filepath.ToSlash(filepath.Join("docs/system", docsSubjectSlug(subject)+".md"))
		parent, err = docsScaffoldParent(corpus)
		if err != nil {
			return err
		}
	case kind == docgraph.KindProposal:
		relative = filepath.ToSlash(filepath.Join("docs/system/proposals", docsSubjectSlug(subject)+".md"))
		parent, err = docsScaffoldParent(corpus)
		if err != nil {
			return err
		}
	default: // docgraph.KindDecision
		relative = filepath.ToSlash(filepath.Join("docs/system/decisions", docsSubjectSlug(subject)+".md"))
		parent, err = docsScaffoldParent(corpus)
		if err != nil {
			return err
		}
		decidesFor = strings.TrimSpace(args.String("decides-for"))
		if decidesFor == "" {
			decidesFor = parent
		}
	}
	absolute := filepath.Join(repoRoot, filepath.FromSlash(relative))
	if fileExists(absolute) {
		if wantIndex {
			return tuskerError(errorAlreadyExists, "domain index already exists at "+relative+"; update it in place instead of recreating it")
		}
		return tuskerError(errorAlreadyExists, "a file already exists at "+relative+"; pick a different subject or update that file")
	}
	scaffold := docsScaffoldPortable(subject, kind, parent, decidesFor, readWhen, skipWhen)
	var predecessor *docsSupersession
	if old := strings.TrimSpace(args.String("supersedes")); old != "" {
		if predecessor, err = docsPrepareSupersession(repoRoot, corpus, old, kind, subject, relative); err != nil {
			return err
		}
		if scaffold, err = docsSetFrontmatterLine(scaffold, "supersedes", old); err != nil {
			return err
		}
		scaffold += "\n" + predecessor.link + "\n"
		// Validate against the corpus as it will be after both writes.
		for i := range corpus.Documents {
			if corpus.Documents[i].Path == predecessor.doc.Path {
				corpus.Documents[i] = predecessor.updated
			}
		}
	}
	candidate, parseErr := docgraph.ParseDocHeaders(relative, []byte(scaffold))
	if parseErr != nil {
		return tuskerError(errorInvalidField, "generated document scaffold is invalid: "+parseErr.Error(), withPath(relative))
	}
	// Validate the candidate inside the real corpus so parent, decides_for
	// and index chains resolve, but only fail on issues attached to the new
	// file: unrelated corpus debt must not block creation.
	var candidateIssues []docgraph.Issue
	for _, issue := range docgraph.ValidateCorpus(docgraph.Corpus{Documents: append(append([]docgraph.Document{}, corpus.Documents...), candidate)}) {
		if issue.Path == relative {
			candidateIssues = append(candidateIssues, issue)
		}
	}
	if len(candidateIssues) > 0 {
		return tuskerError(errorInvalidField, "generated document scaffold failed documentation validation", withPath(relative), withContext(candidateIssues))
	}
	if args.Bool("print") {
		result := map[string]any{"ok": true, "path": relative, "subject": subject, "kind": string(kind), "content": scaffold, "written": false}
		if predecessor != nil {
			result["superseded"] = map[string]any{"path": predecessor.doc.Path, "subject": predecessor.doc.Subject, "content": predecessor.content}
		}
		if args.Bool("json") {
			emitJSON(result)
		} else if predecessor != nil {
			fmt.Printf("# %s (new)\n%s\n# %s (superseded)\n%s", relative, scaffold, predecessor.doc.Path, predecessor.content)
		} else {
			fmt.Print(scaffold)
		}
		return nil
	}
	if predecessor == nil {
		if err := docsAdoptWriteText(repoRoot, relative, scaffold); err != nil {
			return err
		}
	} else {
		// Both documents land together or neither does. Apply the ordinary
		// writer's no-symlinked-ancestor rule to both paths, and commit only if
		// the predecessor still holds the bytes the update was prepared from.
		for _, rel := range []string{relative, predecessor.doc.Path} {
			if _, err := docgraph.ReadDocumentFile(repoRoot, rel); err != nil && !errors.Is(err, fs.ErrNotExist) {
				return tuskerError(errorInvalidTransition, "docs new --supersedes refuses an unsafe document path: "+err.Error(), withPath(rel))
			}
		}
		if err := os.MkdirAll(filepath.Dir(absolute), 0o755); err != nil {
			return err
		}
		oldAbsolute := filepath.Join(repoRoot, filepath.FromSlash(predecessor.doc.Path))
		writes := map[string]string{absolute: scaffold, oldAbsolute: predecessor.content}
		if docsSupersedeBeforeCommit != nil {
			docsSupersedeBeforeCommit()
		}
		if err := commitV7DocumentWritesExpecting(writes, map[string][]byte{oldAbsolute: predecessor.raw}); err != nil {
			return err
		}
	}
	if args.Bool("json") {
		result := map[string]any{"ok": true, "path": relative, "subject": subject, "kind": string(kind)}
		if predecessor != nil {
			result["superseded"] = predecessor.doc.Path
		}
		emitJSON(result)
		return nil
	}
	fmt.Printf("Created %s\n", relative)
	if predecessor != nil {
		fmt.Printf("Superseded %s\n", predecessor.doc.Path)
	}
	return nil
}

// docsSupersession is the prepared predecessor side of docs new --supersedes:
// the old document, its rewritten bytes, and the body link for the new doc.
type docsSupersession struct {
	doc     docgraph.Document
	raw     []byte
	updated docgraph.Document
	content string
	link    string
}

// docsPrepareSupersession checks every refusal case before anything is
// written: missing subject, already superseded, kind family mismatch, and
// uncommitted changes to the old document.
func docsPrepareSupersession(repoRoot string, corpus docgraph.Corpus, oldSubject string, kind docgraph.Kind, newSubject, newRelative string) (*docsSupersession, error) {
	var old *docgraph.Document
	for i := range corpus.Documents {
		if strings.EqualFold(strings.TrimSpace(corpus.Documents[i].Subject), oldSubject) {
			old = &corpus.Documents[i]
			break
		}
	}
	if old == nil {
		return nil, tuskerError(errorNotFound, fmt.Sprintf("docs new --supersedes: no document has subject %q", oldSubject))
	}
	if strings.EqualFold(strings.TrimSpace(old.Status), "superseded") {
		return nil, tuskerError(errorInvalidTransition, fmt.Sprintf("docs new --supersedes: %q is already superseded by %q; supersede that document instead", old.Subject, old.SupersededBy), withPath(old.Path))
	}
	if docsKindFamily(old.Kind) != docsKindFamily(kind) {
		return nil, tuskerError(errorInvalidArg, fmt.Sprintf("docs new --supersedes: %q is a %s, which a %s cannot supersede; use a matching --kind", old.Subject, docsKindFamily(old.Kind), docsKindFamily(kind)), withPath(old.Path))
	}
	if docsPathDirty(repoRoot, old.Path) {
		return nil, tuskerError(errorInvalidTransition, fmt.Sprintf("docs new --supersedes: %s has uncommitted changes; commit, stash, or restore it first", old.Path), withPath(old.Path))
	}
	raw, err := docgraph.ReadDocumentFile(repoRoot, old.Path)
	if err != nil {
		return nil, err
	}
	content, err := docsSetFrontmatterLine(string(raw), "status", "superseded")
	if err == nil {
		content, err = docsSetFrontmatterLine(content, "superseded_by", newSubject)
	}
	if err != nil {
		return nil, tuskerError(errorInvalidField, "docs new --supersedes cannot rewrite "+old.Path+": "+err.Error(), withPath(old.Path))
	}
	updated, err := docgraph.ParseDocHeaders(old.Path, []byte(content))
	if err != nil {
		return nil, tuskerError(errorInvalidField, "docs new --supersedes produced an invalid header for "+old.Path+": "+err.Error(), withPath(old.Path))
	}
	target, err := filepath.Rel(filepath.Dir(filepath.FromSlash(newRelative)), filepath.FromSlash(old.Path))
	if err != nil {
		return nil, err
	}
	return &docsSupersession{
		doc:     *old,
		raw:     raw,
		updated: updated,
		content: content,
		link:    fmt.Sprintf("Supersedes [%s](%s).", old.Subject, filepath.ToSlash(target)),
	}, nil
}

// docsSupersedeBeforeCommit is a test hook run after the predecessor update
// is prepared and before the guarded commit.
var docsSupersedeBeforeCommit func()

// docsPathDirty reports whether git shows any change (including untracked)
// for relative. Outside a git work tree (linked worktrees included) it
// reports clean; a git failure inside a work tree counts as dirty.
func docsPathDirty(repoRoot, relative string) bool {
	inside, err := exec.Command("git", "-C", repoRoot, "rev-parse", "--is-inside-work-tree").Output()
	if err != nil || strings.TrimSpace(string(inside)) != "true" {
		return false
	}
	out, err := exec.Command("git", "-C", repoRoot, "status", "--porcelain", "--", relative).Output()
	return err != nil || strings.TrimSpace(string(out)) != ""
}

// docsKindFamily groups kinds that may supersede each other: docs (including
// legacy canonical), proposals (spec is a proposal), and decisions.
func docsKindFamily(kind docgraph.Kind) docgraph.Kind {
	if kind.IsCanonicalFamily() {
		return docgraph.KindDoc
	}
	return kind
}

// docsSetFrontmatterLine replaces the top-level `key:` line in the front
// matter, or appends one before the closing fence. The body is untouched.
func docsSetFrontmatterLine(content, key, value string) (string, error) {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	if !strings.HasPrefix(content, "---\n") {
		return "", fmt.Errorf("missing YAML front matter")
	}
	lines := strings.SplitAfter(content, "\n")
	entry := key + ": " + value + "\n"
	for i := 1; i < len(lines); i++ {
		line := strings.TrimSuffix(lines[i], "\n")
		if strings.TrimSpace(line) == "---" {
			lines = append(lines[:i], append([]string{entry}, lines[i:]...)...)
			return strings.Join(lines, ""), nil
		}
		if strings.HasPrefix(line, key+":") {
			lines[i] = entry
			return strings.Join(lines, ""), nil
		}
	}
	return "", fmt.Errorf("front matter has no closing --- line")
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

// docsScaffoldPortable renders a creation scaffold with an explicit portable
// kind and kind-specific lifecycle. Creation never claims code conformance:
// new documents start unverified and new proposals start proposed.
func docsScaffoldPortable(subject string, kind docgraph.Kind, parent, decidesFor, readWhen, skipWhen string) string {
	created := time.Now().Local().Format("2006-01-02")
	var builder strings.Builder
	builder.WriteString("---\n")
	fmt.Fprintf(&builder, "kind: %s                # doc, proposal, or decision\n", string(kind))
	fmt.Fprintf(&builder, "subject: %s            # unique key; the one right name for this document\n", subject)
	builder.WriteString("keywords: []            # search aliases a reader might type instead of the subject\n")
	fmt.Fprintf(&builder, "part_of: %s       # subject of the parent document this sits under\n", parent)
	builder.WriteString("describes: []           # coarse repository paths this document explains\n")
	switch kind {
	case docgraph.KindProposal:
		builder.WriteString("status: proposed        # proposed, accepted, implemented, or superseded (acceptance records intent, not proof)\n")
	case docgraph.KindDecision:
		builder.WriteString("status: proposed        # proposed, accepted, or superseded\n")
		fmt.Fprintf(&builder, "decides_for: %s  # proposal or spec subject this decision settles\n", decidesFor)
	default:
		builder.WriteString("status: current         # current, or superseded (then set superseded_by)\n")
	}
	if kind == docgraph.KindDoc {
		// A fresh chapter governs no code until its author lists describes;
		// claiming unverified with empty describes would fail DOC_DESCRIBES_REQUIRED.
		builder.WriteString("code_conformance: not_applicable # once describes lists code, set unverified (or matches with a last_verified stamp)\n")
	} else {
		builder.WriteString("code_conformance: unverified # unverified, matches, drift, or not_applicable; matches needs last_verified scope and stamp\n")
	}
	fmt.Fprintf(&builder, "created: %s      # date this document was first written\n", created)
	builder.WriteString("last_verified:          # date @ commit this document was last checked against code\n")
	fmt.Fprintf(&builder, "read_when: %s # one line: when a reader should open this\n", strconv.Quote(readWhen))
	fmt.Fprintf(&builder, "skip_when: %s # one line: when a reader should look elsewhere\n", strconv.Quote(skipWhen))
	if kind == docgraph.KindProposal {
		builder.WriteString("updates: []             # current documents this change will update when implemented\n")
		builder.WriteString("sources: []             # links or records supporting this contract\n")
		builder.WriteString("decisions_locked: false # set true only when its updates are ready to land\n")
	}
	builder.WriteString("---\n\n")
	fmt.Fprintf(&builder, "# %s\n", subject)
	return builder.String()
}

// docsDomainIndexSubject returns the reading parent for a domain chapter: the
// subject of docs/system/domains/<domain>/00-index.md. A missing index names
// the exact scaffold command instead of silently parenting under overview.
func docsDomainIndexSubject(corpus docgraph.Corpus, domain string) (string, error) {
	want := filepath.ToSlash(filepath.Join("docs/system/domains", domain, "00-index.md"))
	for _, doc := range corpus.Documents {
		if filepath.ToSlash(filepath.Clean(doc.Path)) != want {
			continue
		}
		if subject := strings.TrimSpace(doc.Subject); subject != "" {
			return subject, nil
		}
		break
	}
	return "", tuskerError(errorInvalidField, fmt.Sprintf("docs new cannot select a parent: %s is missing or declares no subject; create it with: tusker new doc <subject> --domain %s --index", want, domain))
}

func docsScaffoldParent(corpus docgraph.Corpus) (string, error) {
	for _, doc := range corpus.Documents {
		if filepath.ToSlash(filepath.Clean(doc.Path)) != "docs/system/00-overview.md" {
			continue
		}
		if subject := strings.TrimSpace(doc.Subject); subject != "" {
			return subject, nil
		}
		break
	}
	return "", tuskerError(errorInvalidField, "docs new cannot select a parent: docs/system/00-overview.md must declare a subject; run tusker init or repair that managed root, then retry")
}
