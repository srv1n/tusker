package docgraph

import (
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"tusker/internal/v7schema"
)

// The repository has one managed document corpus. Product behavior lives in
// docs/system; specs and decisions live below .tusker/specs. Keeping these
// roots here gives commands, traceability, and generated maps the same route.
const (
	DocsSystemRoot = "docs/system"
	SpecsRoot      = ".tusker/specs"
	DecisionsRoot  = ".tusker/specs/decisions"

	// DefaultFindLimit keeps the first answer small enough to fit in a worker
	// capsule. A caller can ask for a different bounded limit with
	// FindWithLimit, but an unbounded search is never the default.
	DefaultFindLimit = 8
)

var (
	resolverWikiLink     = regexp.MustCompile(`\[\[([^\]]+)\]\]`)
	resolverMarkdownLink = regexp.MustCompile(`\[[^\]]*\]\(\s*(?:<([^>]+)>|([^\s)]+))`)
)

// Resolution identifies the document reached by a subject or repository
// relative reference. CanonicalRef is the stable managed path callers can
// show or persist. ResolvedFrom is set only when ResolveCurrent follows a
// supersession tombstone.
type Resolution struct {
	Document     Document
	Requested    string
	CanonicalRef string
	ResolvedFrom string
}

// DocumentLink is a resolved semantic relationship between two managed
// documents. Ref retains the authored spelling for diagnostics and readers;
// From and To always use document subjects.
type DocumentLink struct {
	From string `json:"from"`
	To   string `json:"to"`
	Kind string `json:"kind"`
	Ref  string `json:"ref,omitempty"`
}

// BrokenLink records a reference that points at a managed-document route but
// cannot be resolved. Repository paths outside the managed corpus (for
// example a report or a source file) are intentionally not treated as broken
// document links.
type BrokenLink struct {
	From string `json:"from"`
	Path string `json:"path"`
	Kind string `json:"kind"`
	Ref  string `json:"ref"`
}

// Resolver indexes one parsed corpus. It is deliberately in-memory and
// immutable after construction so all consumers observe the same subject and
// path rules during one command.
type Resolver struct {
	bySubject       map[string]Document
	byPath          map[string]Document
	byBase          map[string][]Document
	trackerPrefixes map[string]bool
}

// NewResolver builds a deterministic resolver. Duplicate subjects remain in
// the corpus for validation, while lookups choose canonical documents first
// and then the lexicographically earliest path so a malformed corpus still
// produces stable diagnostics.
func NewResolver(corpus Corpus) *Resolver {
	r := &Resolver{
		bySubject:       make(map[string]Document),
		byPath:          make(map[string]Document),
		byBase:          make(map[string][]Document),
		trackerPrefixes: make(map[string]bool),
	}
	docs := append([]Document(nil), corpus.Documents...)
	sort.SliceStable(docs, func(i, j int) bool {
		if documentKindRank(docs[i].Kind) != documentKindRank(docs[j].Kind) {
			return documentKindRank(docs[i].Kind) < documentKindRank(docs[j].Kind)
		}
		return docs[i].Path < docs[j].Path
	})
	for _, doc := range docs {
		path := normalizeDocumentPath(doc.Path)
		if path == "" {
			continue
		}
		if _, exists := r.byPath[path]; !exists {
			r.byPath[path] = doc
		}
		base := strings.ToLower(filepath.Base(filepath.FromSlash(path)))
		if base != "." && base != "" {
			r.byBase[base] = append(r.byBase[base], doc)
		}
		subject := normalizeSubject(doc.Subject)
		if subject != "" {
			if _, exists := r.bySubject[subject]; !exists {
				r.bySubject[subject] = doc
			}
		}
		for _, ref := range ExtractReferences(doc.Body) {
			if prefix := trackerReferenceProject(ref); prefix != "" {
				r.trackerPrefixes[prefix] = true
			}
		}
	}
	return r
}

func documentKindRank(kind Kind) int {
	switch kind {
	case KindCanonical:
		return 0
	case KindSpec:
		return 1
	case KindDecision:
		return 2
	default:
		return 3
	}
}

// ResolveReference resolves a managed subject or path using the shared
// current-document convention.
func ResolveReference(corpus Corpus, ref string) (Resolution, bool) {
	return NewResolver(corpus).Resolve(ref)
}

// ResolveReferenceFrom resolves a body link relative to the source document's
// path. Relative links are normalized before lookup, while absolute paths and
// URLs are rejected.
func ResolveReferenceFrom(corpus Corpus, sourcePath, ref string) (Resolution, bool) {
	return NewResolver(corpus).ResolveFrom(sourcePath, ref)
}

// ResolveCurrentReference resolves a reference and follows a chain of
// superseded_by subjects to the current document.
func ResolveCurrentReference(corpus Corpus, ref string) (Resolution, bool) {
	return NewResolver(corpus).ResolveCurrent(ref)
}

// Resolve resolves a subject or managed repository path without following a
// tombstone. Use ResolveCurrent when the user asked for the current route.
func (r *Resolver) Resolve(ref string) (Resolution, bool) {
	return r.ResolveFrom("", ref)
}

// ResolveFrom is Resolve with source-relative Markdown path support.
func (r *Resolver) ResolveFrom(sourcePath, ref string) (Resolution, bool) {
	if r == nil {
		return Resolution{}, false
	}
	requested := strings.TrimSpace(ref)
	clean := NormalizeReference(requested)
	if clean == "" {
		return Resolution{}, false
	}
	// A Markdown path names a location from the source document. Try that
	// route before subject lookup, including a same-directory `guide.md` link,
	// so a subject or basename in another directory cannot steal it.
	if sourcePath != "" {
		candidate := filepath.ToSlash(filepath.Clean(filepath.Join(filepath.Dir(filepath.FromSlash(sourcePath)), filepath.FromSlash(clean))))
		if !referenceEscapes(candidate) {
			if doc, ok := r.byPath[candidate]; ok {
				return Resolution{Document: doc, Requested: requested, CanonicalRef: doc.Path}, true
			}
		}
	}
	if doc, ok := r.bySubject[normalizeSubject(clean)]; ok {
		return Resolution{Document: doc, Requested: requested, CanonicalRef: doc.Path}, true
	}
	if doc, ok := r.byPath[normalizeDocumentPath(clean)]; ok {
		return Resolution{Document: doc, Requested: requested, CanonicalRef: doc.Path}, true
	}
	// A bare filename is convenient in small Markdown documents, but a path
	// containing directories must match exactly. Basename fallback for a
	// qualified path could silently turn an obsolete route into a different
	// governing document with the same filename.
	if strings.Contains(clean, "/") {
		return Resolution{}, false
	}
	base := strings.ToLower(filepath.Base(filepath.FromSlash(clean)))
	if candidates := r.byBase[base]; len(candidates) == 1 {
		doc := candidates[0]
		return Resolution{Document: doc, Requested: requested, CanonicalRef: doc.Path}, true
	}
	return Resolution{}, false
}

// ResolveCurrent follows superseded_by references without hiding the
// historical node from graph callers. Cycles stop at the last resolvable node;
// validation remains responsible for reporting malformed tombstones.
func (r *Resolver) ResolveCurrent(ref string) (Resolution, bool) {
	return r.ResolveCurrentFrom("", ref)
}

// ResolveCurrentFrom is ResolveCurrent with source-relative reference support.
func (r *Resolver) ResolveCurrentFrom(sourcePath, ref string) (Resolution, bool) {
	current, ok := r.ResolveFrom(sourcePath, ref)
	if !ok {
		return Resolution{}, false
	}
	initial := current
	seen := map[string]bool{}
	for {
		key := normalizeSubject(current.Document.Subject)
		if key == "" || seen[key] || !strings.EqualFold(strings.TrimSpace(current.Document.Status), "superseded") {
			break
		}
		seen[key] = true
		next, ok := r.ResolveFrom(current.Document.Path, current.Document.SupersededBy)
		if !ok {
			break
		}
		current = next
	}
	if current.Document.Path != initial.Document.Path {
		current.ResolvedFrom = initial.Document.Subject
	}
	return current, true
}

// NormalizeReference strips supported Markdown/Obsidian decoration, anchors,
// and local ./ prefixes. External URLs, absolute paths, and empty anchors
// return an empty string because they are outside this document graph.
func NormalizeReference(ref string) string {
	ref = strings.TrimSpace(strings.Trim(ref, "`"))
	if ref == "" {
		return ""
	}
	if strings.HasPrefix(ref, "[[") && strings.HasSuffix(ref, "]]") {
		ref = strings.TrimSuffix(strings.TrimPrefix(ref, "[["), "]]")
	}
	if pipe := strings.IndexByte(ref, '|'); pipe >= 0 {
		ref = ref[:pipe]
	}
	ref = strings.TrimSpace(strings.Trim(ref, "<>"))
	if ref == "" || strings.HasPrefix(ref, "#") || strings.Contains(ref, "://") || strings.HasPrefix(ref, "mailto:") {
		return ""
	}
	if hash := strings.IndexByte(ref, '#'); hash >= 0 {
		ref = ref[:hash]
	}
	ref = strings.TrimSpace(strings.Trim(ref, "`<>"))
	if ref == "" || filepath.IsAbs(filepath.FromSlash(ref)) {
		return ""
	}
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(ref)))
	if clean == "." || clean == ".." {
		return ""
	}
	return clean
}

func normalizeSubject(subject string) string {
	return strings.ToLower(strings.TrimSpace(subject))
}

func normalizeDocumentPath(path string) string {
	path = strings.TrimSpace(strings.ReplaceAll(path, "\\", "/"))
	if path == "" {
		return ""
	}
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(path)))
	if clean == "." || referenceEscapes(clean) || filepath.IsAbs(filepath.FromSlash(clean)) {
		return ""
	}
	return strings.TrimPrefix(clean, "./")
}

func referenceEscapes(path string) bool {
	return path == ".." || strings.HasPrefix(path, "../")
}

// ExtractReferences returns stable, de-duplicated targets from Markdown and
// Obsidian links. Fenced code is ignored so examples do not become graph
// edges. The authored order is retained for readable detail views.
func ExtractReferences(body string) []string {
	seen := map[string]bool{}
	var refs []string
	add := func(raw string) {
		ref := NormalizeReference(raw)
		if strings.HasPrefix(strings.TrimSpace(raw), "./") && ref != "" && !strings.HasPrefix(ref, "./") {
			ref = "./" + ref
		}
		if ref == "" || seen[ref] {
			return
		}
		seen[ref] = true
		refs = append(refs, ref)
	}
	inFence := false
	for _, line := range strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		for _, match := range resolverWikiLink.FindAllStringSubmatch(line, -1) {
			if len(match) > 1 {
				add(match[1])
			}
		}
		for _, match := range resolverMarkdownLink.FindAllStringSubmatchIndex(line, -1) {
			if len(match) < 6 {
				continue
			}
			start := match[0]
			if start > 0 && line[start-1] == '!' {
				continue
			}
			if match[2] >= 0 {
				add(line[match[2]:match[3]])
			} else if match[4] >= 0 {
				add(line[match[4]:match[5]])
			}
		}
	}
	return refs
}

// SemanticLinks extracts metadata and body relationships through the same
// resolver used by search and traceability. Broken only includes references
// that claim to be managed-document routes; source/report links remain normal
// navigation and are not silently promoted into the governing corpus.
func SemanticLinks(corpus Corpus) ([]DocumentLink, []BrokenLink) {
	return NewResolver(corpus).SemanticLinks(corpus)
}

func (r *Resolver) SemanticLinks(corpus Corpus) ([]DocumentLink, []BrokenLink) {
	if r == nil {
		return nil, nil
	}
	var links []DocumentLink
	var broken []BrokenLink
	seen := map[string]bool{}
	add := func(doc Document, kind, raw string, strict bool) {
		raw = strings.TrimSpace(raw)
		if raw == "" || strings.TrimSpace(doc.Subject) == "" {
			return
		}
		resolved, ok := r.ResolveFrom(doc.Path, raw)
		if !ok {
			if strict {
				broken = append(broken, BrokenLink{From: doc.Subject, Path: doc.Path, Kind: kind, Ref: raw})
			}
			return
		}
		key := strings.ToLower(doc.Subject) + "\x00" + strings.ToLower(resolved.Document.Subject) + "\x00" + kind
		if seen[key] {
			return
		}
		seen[key] = true
		links = append(links, DocumentLink{From: doc.Subject, To: resolved.Document.Subject, Kind: kind, Ref: raw})
	}

	for _, doc := range corpus.Documents {
		add(doc, "part_of", doc.PartOf, true)
		for _, target := range doc.Updates {
			add(doc, "updates", target, documentReferenceLike(target))
		}
		for _, target := range doc.Sources {
			add(doc, "source", target, sourceReferenceLike(doc.Path, target, r))
		}
		add(doc, "decides_for", doc.DecidesFor, true)
		add(doc, "superseded_by", doc.SupersededBy, true)
		for _, target := range ExtractReferences(doc.Body) {
			add(doc, "link", target, bodyReferenceLike(doc.Path, target, r))
		}
	}
	sort.SliceStable(links, func(i, j int) bool {
		a, b := links[i], links[j]
		if a.From != b.From {
			return a.From < b.From
		}
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		if a.To != b.To {
			return a.To < b.To
		}
		return a.Ref < b.Ref
	})
	sort.SliceStable(broken, func(i, j int) bool {
		a, b := broken[i], broken[j]
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		return a.Ref < b.Ref
	})
	return links, broken
}

// Backlinks returns every semantic relationship that points at subject,
// ordered for deterministic API and CLI output.
func Backlinks(corpus Corpus, subject string) []DocumentLink {
	return NewResolver(corpus).Backlinks(subject, corpus)
}

func (r *Resolver) Backlinks(subject string, corpus Corpus) []DocumentLink {
	if r == nil {
		return nil
	}
	want := normalizeSubject(subject)
	links, _ := r.SemanticLinks(corpus)
	result := make([]DocumentLink, 0)
	for _, link := range links {
		if normalizeSubject(link.To) == want {
			result = append(result, link)
		}
	}
	return result
}

func brokenLinkIssues(corpus Corpus) []Issue {
	_, broken := SemanticLinks(corpus)
	issues := make([]Issue, 0, len(broken))
	for _, link := range broken {
		issues = append(issues, Issue{
			Code:    "DOC_LINK_DANGLING",
			Path:    link.Path,
			Message: link.Kind + " reference " + link.Ref + " does not resolve to a managed document",
		})
	}
	return issues
}

func documentReferenceLike(ref string) bool {
	clean := NormalizeReference(ref)
	if clean == "" {
		return false
	}
	return strings.HasPrefix(clean, DocsSystemRoot+"/") ||
		strings.HasPrefix(clean, SpecsRoot+"/") ||
		strings.HasSuffix(strings.ToLower(clean), ".md") ||
		(!strings.Contains(clean, "/") && !strings.Contains(clean, "."))
}

func bodyReferenceLike(sourcePath, ref string, resolver *Resolver) bool {
	clean := NormalizeReference(ref)
	if clean == "" {
		return false
	}
	if strings.HasPrefix(clean, DocsSystemRoot+"/") || strings.HasPrefix(clean, SpecsRoot+"/") {
		return true
	}
	if strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, "./") {
		candidate := filepath.ToSlash(filepath.Clean(filepath.Join(filepath.Dir(filepath.FromSlash(sourcePath)), filepath.FromSlash(clean))))
		return strings.HasPrefix(candidate, DocsSystemRoot+"/") || strings.HasPrefix(candidate, SpecsRoot+"/")
	}
	if _, ok := resolver.bySubject[normalizeSubject(clean)]; ok {
		return true
	}
	if _, ok := resolver.ResolveFrom(sourcePath, ref); ok {
		return true
	}
	if resolver.isExternalTrackerReference(clean) {
		return false
	}
	return !strings.Contains(clean, "/") && !strings.Contains(clean, ".")
}

func (r *Resolver) isExternalTrackerReference(ref string) bool {
	if trackerReferenceProject(ref) != "" || v7schema.WaveIDPattern.MatchString(ref) || v7schema.EscalationIDPattern.MatchString(ref) {
		return true
	}
	return len(ref) == 3 && r.trackerPrefixes[strings.ToUpper(ref)]
}

func trackerReferenceProject(ref string) string {
	for _, pattern := range []*regexp.Regexp{
		v7schema.TaskIDPattern,
		v7schema.GateIDPattern,
		v7schema.DecisionIDPattern,
		v7schema.ProposalIDPattern,
		v7schema.EvidenceIDPattern,
		v7schema.AttemptIDPattern,
	} {
		match := pattern.FindStringSubmatch(ref)
		if len(match) > 1 {
			return match[1]
		}
	}
	return ""
}

func sourceReferenceLike(sourcePath, ref string, resolver *Resolver) bool {
	clean := NormalizeReference(ref)
	if clean == "" {
		return false
	}
	if strings.HasPrefix(clean, DocsSystemRoot+"/") || strings.HasPrefix(clean, SpecsRoot+"/") {
		return true
	}
	if strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, "./") {
		candidate := filepath.ToSlash(filepath.Clean(filepath.Join(filepath.Dir(filepath.FromSlash(sourcePath)), filepath.FromSlash(clean))))
		return strings.HasPrefix(candidate, DocsSystemRoot+"/") || strings.HasPrefix(candidate, SpecsRoot+"/")
	}
	if _, ok := resolver.bySubject[normalizeSubject(clean)]; ok {
		return true
	}
	_, ok := resolver.ResolveFrom(sourcePath, ref)
	return ok
}
