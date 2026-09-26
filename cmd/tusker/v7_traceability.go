package main

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"tusker/internal/docgraph"
)

var (
	v7WorkStreamTaskIDPattern  = regexp.MustCompile(`\b[A-Z]{3}-T-\d{4}\b`)
	v7WorkStreamEpicPathRegexp = regexp.MustCompile(`(?:^|[\s(])(?:\.tusker/)?work/epics/([A-Z]{3})\.md\b`)
	v7WikiLinkPattern          = regexp.MustCompile(`\[\[([^\]]+)\]\]`)
	v7MarkdownLinkPattern      = regexp.MustCompile(`\[([^\]]+)\]\(([^)]+)\)`)
)

type v7TraceabilityDoc struct {
	Path string
	Body string
}

func v7SpecRefsCapsuleLine(vaultPath string, note Note) string {
	kind := noteDisplayKind(note.Data)
	if kind != "task" && kind != "epic" {
		return ""
	}
	refs := normalizeList(note.Data["spec_refs"])
	if len(refs) == 0 {
		return ""
	}
	return "- Read next spec refs: " + strings.Join(v7SpecRefDisplayTargets(vaultPath, refs), ", ")
}

func v7SpecRefsPacketSection(vaultPath string, note Note) string {
	refs := normalizeList(note.Data["spec_refs"])
	if len(refs) == 0 {
		return "- None declared."
	}
	lines := []string{"Read these governing specs/decisions before implementation:"}
	for _, target := range v7SpecRefDisplayTargets(vaultPath, refs) {
		lines = append(lines, "- "+target)
	}
	return strings.Join(lines, "\n")
}

func v7SpecRefDisplayTargets(vaultPath string, refs []string) []string {
	var out []string
	for _, ref := range refs {
		normalized := v7NormalizeSpecRef(ref)
		if normalized == "" {
			continue
		}
		readPath := v7SpecRefReadPath(vaultPath, normalized)
		if readPath != "" && readPath != normalized {
			out = append(out, "`"+normalized+"` -> `"+readPath+"`")
			continue
		}
		out = append(out, "`"+normalized+"`")
	}
	return out
}

func v7SpecRefReadPath(vaultPath, ref string) string {
	normalized := v7NormalizeSpecRef(ref)
	ref, anchor := v7CleanSpecRef(normalized), v7SpecRefAnchor(normalized)
	if ref == "" {
		return ""
	}
	withAnchor := func(value string) string {
		if anchor == "" || value == "" {
			return value
		}
		return value + "#" + anchor
	}
	if id := v7SpecRefDecisionID(ref); id != "" {
		return withAnchor(vaultDisplayPath(vaultPath, filepath.ToSlash(filepath.Join("work", "decisions", id+".md"))))
	}
	if strings.HasPrefix(ref, "work/") {
		return withAnchor(vaultDisplayPath(vaultPath, ref))
	}
	if canonical := v7CanonicalSpecRef(vaultPath, ref); canonical != "" {
		return withAnchor(canonical)
	}
	return withAnchor(ref)
}

func v7CanonicalSpecRef(vaultPath, ref string) string {
	repoRoot := v7RepoRoot(vaultPath)
	corpus, _, err := docgraph.LoadRepository(repoRoot)
	if err != nil {
		return ""
	}
	// Follow supersession so a migrated (legacy) path reports the actual
	// portable document instead of a stale forwarding stub. "spec" stays
	// accepted as the compatibility spelling for proposal, so portable
	// proposals resolve as governing references alongside legacy specs and
	// decisions.
	resolved, ok := docgraph.ResolveCurrentReference(corpus, ref)
	if !ok || (resolved.Document.Kind != docgraph.KindSpec && resolved.Document.Kind != docgraph.KindProposal && resolved.Document.Kind != docgraph.KindDecision) {
		return ""
	}
	return resolved.CanonicalRef
}

// v7GoverningSpecHint is the shared repair hint for unresolvable governing
// references: portable proposals/decisions/domains, managed legacy specs,
// and tracker lifecycle decisions.
func v7GoverningSpecHint() string {
	return "use a subject or path under docs/system (proposals, decisions, domains) or .tusker/specs (decisions included), or a V7 decision id"
}

// v7SpecRefFailureReason explains why a governing reference does not resolve.
// It returns "" when the ref names a governing spec/proposal/decision (or a
// tracker lifecycle decision) with its section present. Legacy managed paths
// resolve through model compatibility; superseded stubs resolve to their
// current document. Ambiguous subjects fail instead of silently picking one
// route, and tracker decision IDs stay distinct from product decision files.
func v7SpecRefFailureReason(vaultPath, ref string, decisionIDs map[string]Note) string {
	normalized := v7NormalizeSpecRef(ref)
	if normalized == "" {
		return "reference is empty"
	}
	clean, anchor := v7CleanSpecRef(normalized), v7SpecRefAnchor(normalized)
	if id := v7SpecRefDecisionID(clean); id != "" {
		note, ok := decisionIDs[id]
		if !ok {
			return "unknown task/decision id " + id
		}
		if !v7SpecRefSectionExists(note.Body, anchor) {
			return "missing section #" + anchor + " in " + id
		}
		return ""
	}
	if clean == "" || v7SpecRefPathEscapes(clean) {
		return "path escapes the repository: " + normalized
	}
	corpus, _, err := docgraph.LoadRepository(v7RepoRoot(vaultPath))
	if err != nil {
		return "could not load the documentation corpus: " + err.Error()
	}
	if _, strictOK := docgraph.ResolveStrictReference(corpus, clean); strictOK {
		current, ok := docgraph.ResolveCurrentReference(corpus, clean)
		if !ok {
			return "reference does not resolve: " + normalized
		}
		switch current.Document.Kind {
		case docgraph.KindSpec, docgraph.KindProposal, docgraph.KindDecision:
		default:
			return fmt.Sprintf("wrong kind: %s resolves to %s document %s, not a governing spec, proposal, or decision", normalized, current.Document.Kind, current.CanonicalRef)
		}
		if anchor != "" && !v7SpecRefSectionExists(current.Document.Body, anchor) {
			return "missing section #" + anchor + " in " + current.CanonicalRef
		}
		return ""
	}
	if _, lenientOK := docgraph.ResolveReference(corpus, clean); lenientOK {
		return "ambiguous subject " + normalized + v7SpecRefAmbiguitySuffix(corpus, clean)
	}
	return "missing target: no managed document matches " + normalized
}

// v7SpecRefAmbiguitySuffix names the competing routes for an ambiguous
// subject so the caller can disambiguate with an exact path. Output stays
// bounded no matter how many documents claim the subject.
func v7SpecRefAmbiguitySuffix(corpus docgraph.Corpus, clean string) string {
	key := strings.ToLower(strings.TrimSpace(clean))
	var paths []string
	for _, doc := range corpus.Documents {
		if strings.ToLower(strings.TrimSpace(doc.Subject)) == key {
			paths = append(paths, doc.Path)
		}
	}
	if len(paths) == 0 {
		base := strings.ToLower(filepath.Base(filepath.FromSlash(clean)))
		for _, doc := range corpus.Documents {
			if strings.ToLower(filepath.Base(filepath.FromSlash(doc.Path))) == base {
				paths = append(paths, doc.Path)
			}
		}
	}
	sort.Strings(paths)
	const maxAmbiguousPaths = 5
	if len(paths) > maxAmbiguousPaths {
		return fmt.Sprintf(" (also declared in %s, and %d more)", strings.Join(paths[:maxAmbiguousPaths], ", "), len(paths)-maxAmbiguousPaths)
	}
	if len(paths) == 0 {
		return ""
	}
	return " (also declared in " + strings.Join(paths, ", ") + ")"
}

func v7SpecRefRequiredReads(vaultPath string, note Note) []string {
	var reads []string
	for _, ref := range normalizeList(note.Data["spec_refs"]) {
		if target := v7SpecRefReadPath(vaultPath, ref); target != "" {
			reads = append(reads, target)
		}
	}
	return uniqueStrings(reads)
}

func validateV7SpecTraceability(vaultPath string, notes []Note) []Issue {
	var warnings []Issue
	taskIDs := map[string]bool{}
	epicIDs := map[string]bool{}
	decisionIDs := map[string]Note{}
	var workNotes []Note
	var decisionDocs []v7TraceabilityDoc

	for _, note := range notes {
		kind := effectiveV7Kind(note.Data)
		switch kind {
		case "task":
			if strings.HasSuffix(stringField(note.Data, "schema"), "/v7") {
				taskIDs[stringField(note.Data, "id")] = true
				workNotes = append(workNotes, note)
			}
		case "epic":
			if strings.HasSuffix(stringField(note.Data, "schema"), "/v7") {
				epicIDs[stringField(note.Data, "id")] = true
				workNotes = append(workNotes, note)
			}
		case "decision":
			id := stringField(note.Data, "id")
			if id != "" {
				decisionIDs[id] = note
			}
			decisionDocs = append(decisionDocs, v7TraceabilityDoc{Path: vaultDisplayPath(vaultPath, note.RelativePath), Body: note.Body})
		}
	}

	for _, note := range workNotes {
		if finding, ok := v7DemandingTaskSpecRefIssue(vaultPath, note, note.RelativePath); ok && tuskerTier(vaultPath) == 1 {
			warnings = append(warnings, finding)
		}
		warnings = append(warnings, validateV7SpecRefs(vaultPath, note, decisionIDs)...)
	}

	docs, docWarnings := v7TraceabilityDocs(vaultPath, decisionDocs)
	warnings = append(warnings, docWarnings...)
	for _, doc := range docs {
		content := v7WorkStreamsSection(doc.Body)
		if content == "" {
			continue
		}
		for _, ref := range v7WorkStreamRefs(content) {
			switch {
			case v7TaskIDPattern.MatchString(ref):
				if !taskIDs[ref] {
					warnings = append(warnings, issue("WORK_STREAM_REF_DANGLING", "Work streams references unknown task "+ref, doc.Path, "link to an existing task or remove the stale work-stream link", map[string]any{"ref": ref}))
				}
			case epicAcronymPattern.MatchString(ref):
				if !epicIDs[ref] {
					warnings = append(warnings, issue("WORK_STREAM_REF_DANGLING", "Work streams references unknown epic "+ref, doc.Path, "link to an existing epic or remove the stale work-stream link", map[string]any{"ref": ref}))
				}
			}
		}
	}
	return warnings
}

// v7DemandingTaskSpecRefIssue is shared by whole-vault traceability, task
// readiness validation, and every ready-transition seam. Tier 1 keeps the
// probationary warning-only behavior; the strict tiers require every declared
// governing spec or decision, including an exact section anchor, to resolve.
func v7DemandingTaskSpecRefIssue(vaultPath string, note Note, where string) (Issue, bool) {
	if effectiveV7Kind(note.Data) != "task" || !v7TaskIsDemanding(note.Data) || strings.TrimSpace(stringField(note.Data, "readiness")) != "ready" {
		return Issue{}, false
	}
	refs := normalizeList(note.Data["spec_refs"])
	if tuskerTier(vaultPath) == 1 && len(refs) > 0 {
		return Issue{}, false
	}
	if tuskerTier(vaultPath) >= 2 && v7TaskHasResolvableSpecRef(vaultPath, refs) {
		return Issue{}, false
	}
	if tuskerTier(vaultPath) == 1 {
		return issue(
			"TASK_SPEC_REF_REQUIRED",
			"demanding ready task should declare at least one spec_refs link",
			where,
			"add a repo-relative spec/decision path when available; tier 1 permits ready work while the link is being recovered",
			map[string]any{"id": stringField(note.Data, "id")},
		), true
	}
	return issue(
		"TASK_SPEC_REF_REQUIRED",
		"demanding ready task must declare resolvable spec_refs links",
		where,
		"make every mandatory repo-relative spec/decision path and section resolve, or keep the task out of ready",
		map[string]any{"id": stringField(note.Data, "id")},
	), true
}

func v7TaskHasResolvableSpecRef(vaultPath string, refs []string) bool {
	if len(refs) == 0 || strings.TrimSpace(vaultPath) == "" {
		return false
	}
	idx, err := loadV7Index(vaultPath)
	if err != nil {
		return false
	}
	for _, ref := range refs {
		if v7NormalizeSpecRef(ref) == "" || !v7SpecRefExists(vaultPath, ref, idx.Decisions) {
			return false
		}
	}
	return true
}

func validateV7SpecRefs(vaultPath string, note Note, decisionIDs map[string]Note) []Issue {
	var warnings []Issue
	for _, ref := range normalizeList(note.Data["spec_refs"]) {
		clean := v7NormalizeSpecRef(ref)
		if clean == "" {
			continue
		}
		if reason := v7SpecRefFailureReason(vaultPath, clean, decisionIDs); reason != "" {
			warnings = append(warnings, issue("SPEC_REF_DANGLING", fmt.Sprintf("%s spec_refs reference does not resolve: %s (%s)", effectiveV7Kind(note.Data), clean, reason), note.RelativePath, v7GoverningSpecHint(), map[string]any{"ref": clean}))
		}
	}
	return warnings
}

// v7SpecRefError is the one authoring-time spec_ref check: it returns empty
// strings when ref resolves, else a message carrying the real failure reason
// and a repair hint. Unmanaged docs (no front matter, or no governing kind)
// surface as missing-target or wrong-kind, so the hint names the fix.
func v7SpecRefError(vaultPath, ref string, decisionIDs map[string]Note) (message, hint string) {
	reason := v7SpecRefFailureReason(vaultPath, ref, decisionIDs)
	if reason == "" {
		return "", ""
	}
	hint = v7GoverningSpecHint()
	if strings.HasPrefix(reason, "wrong kind:") || strings.HasPrefix(reason, "missing target:") {
		hint += "; if the document exists, give it YAML front matter with `kind: spec|proposal|decision`"
	}
	return "spec_ref does not resolve: " + strings.TrimSpace(ref) + " (" + reason + ")", hint
}

func v7SpecRefExists(vaultPath, ref string, decisionIDs map[string]Note) bool {
	return v7SpecRefFailureReason(vaultPath, ref, decisionIDs) == ""
}

func v7SpecRefSectionExists(body, anchor string) bool {
	if anchor == "" {
		return true
	}
	_, err := docgraph.ReadSection(body, anchor)
	return err == nil
}

func v7SpecRefDecisionID(ref string) string {
	target := v7CleanSpecRef(ref)
	if v7DecisionIDPattern.MatchString(target) {
		return target
	}
	return ""
}

func v7SpecRefPathEscapes(ref string) bool {
	if filepath.IsAbs(ref) {
		return true
	}
	clean := filepath.Clean(filepath.FromSlash(ref))
	return clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator))
}

func v7CleanSpecRef(ref string) string {
	ref = strings.TrimSpace(strings.Trim(ref, "`"))
	if ref == "" {
		return ""
	}
	ref = wikiTarget(ref)
	ref = strings.Split(ref, "#")[0]
	ref = strings.TrimSpace(strings.Trim(ref, "`"))
	if ref == "" || strings.Contains(ref, "://") {
		return ref
	}
	ref = filepath.ToSlash(filepath.Clean(filepath.FromSlash(ref)))
	if ref == "." {
		return ""
	}
	return strings.TrimPrefix(ref, "./")
}

func v7NormalizeSpecRef(ref string) string {
	ref = strings.TrimSpace(strings.Trim(ref, "`"))
	if ref == "" {
		return ""
	}
	if strings.HasPrefix(ref, "[[") && strings.HasSuffix(ref, "]]") {
		ref = strings.TrimSuffix(strings.TrimPrefix(ref, "[["), "]]")
		if pipe := strings.IndexByte(ref, '|'); pipe >= 0 {
			ref = ref[:pipe]
		}
	}
	ref = strings.TrimSpace(ref)
	base, anchor := ref, ""
	if hash := strings.IndexByte(ref, '#'); hash >= 0 {
		base, anchor = ref[:hash], strings.TrimSpace(ref[hash+1:])
		if anchor == "" {
			return ""
		}
	}
	base = v7CleanSpecRef(base)
	if base == "" {
		return ""
	}
	if anchor != "" {
		return base + "#" + anchor
	}
	return base
}

func v7SpecRefAnchor(ref string) string {
	normalized := v7NormalizeSpecRef(ref)
	if hash := strings.IndexByte(normalized, '#'); hash >= 0 {
		return strings.TrimSpace(normalized[hash+1:])
	}
	return ""
}

func v7TraceabilityDocs(vaultPath string, decisionDocs []v7TraceabilityDoc) ([]v7TraceabilityDoc, []Issue) {
	docs := append([]v7TraceabilityDoc{}, decisionDocs...)
	var warnings []Issue
	repoRoot := v7RepoRoot(vaultPath)
	for _, relRoot := range []string{docgraph.DocsSystemRoot, docgraph.SpecsRoot} {
		root := filepath.Join(repoRoot, filepath.FromSlash(relRoot))
		if !dirExists(root) {
			continue
		}
		err := walkDirUnsorted(root, func(current string, entry fs.DirEntry) error {
			if entry.IsDir() {
				return nil
			}
			if !strings.HasSuffix(entry.Name(), ".md") {
				return nil
			}
			body, err := readText(current)
			if err != nil {
				rel := v7PathForMessage(vaultPath, current)
				warnings = append(warnings, issue("WORK_STREAM_DOC_READ_FAILED", "could not read traceability doc: "+err.Error(), rel, "", nil))
				return nil
			}
			rel, err := filepath.Rel(repoRoot, current)
			if err != nil {
				rel = current
			}
			docs = append(docs, v7TraceabilityDoc{Path: filepath.ToSlash(rel), Body: body})
			return nil
		})
		if err != nil {
			warnings = append(warnings, issue("WORK_STREAM_DOC_SCAN_FAILED", "could not scan traceability docs: "+err.Error(), relRoot, "", nil))
		}
	}
	return docs, warnings
}

func v7WorkStreamsSection(body string) string {
	for _, heading := range []string{"## Work streams", "## Work Streams"} {
		if content := strings.TrimSpace(sectionContent(body, heading)); content != "" {
			return content
		}
	}
	return ""
}

func v7WorkStreamRefs(content string) []string {
	seen := map[string]bool{}
	add := func(raw string) {
		if ref := v7WorkStreamRefID(raw); ref != "" {
			seen[ref] = true
		}
	}
	for _, match := range v7WorkStreamTaskIDPattern.FindAllString(content, -1) {
		add(match)
	}
	for _, match := range v7WorkStreamEpicPathRegexp.FindAllStringSubmatch(content, -1) {
		if len(match) > 1 {
			add(match[1])
		}
	}
	for _, match := range v7WikiLinkPattern.FindAllStringSubmatch(content, -1) {
		if len(match) > 1 {
			add(match[1])
		}
	}
	for _, match := range v7MarkdownLinkPattern.FindAllStringSubmatch(content, -1) {
		if len(match) > 1 {
			add(match[1])
		}
		if len(match) > 2 {
			add(match[2])
		}
	}
	refs := make([]string, 0, len(seen))
	for ref := range seen {
		refs = append(refs, ref)
	}
	sort.Strings(refs)
	return refs
}

func v7WorkStreamRefID(raw string) string {
	target := strings.TrimSpace(strings.Trim(wikiTarget(raw), "`"))
	target = strings.Split(target, "#")[0]
	target = strings.TrimPrefix(target, "./")
	target = filepath.ToSlash(target)
	base := strings.TrimSuffix(filepath.Base(filepath.FromSlash(target)), ".md")
	if v7TaskIDPattern.MatchString(base) {
		return base
	}
	if epicAcronymPattern.MatchString(base) {
		if target == base || strings.Contains(target, "work/epics/") {
			return base
		}
	}
	return ""
}
