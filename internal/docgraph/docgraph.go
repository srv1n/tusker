// Package docgraph parses and validates the repository's canonical document
// headers. It keeps recognized tracker IDs external to the document corpus
// while treating docs, specs, and decision logs as one shared corpus.
package docgraph

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Kind string

const (
	// KindDoc, KindProposal and KindDecision are the portable S46 document
	// kinds. Authors declare them with explicit `kind` front matter; the
	// kind-specific lifecycle and conformance rules in this package apply to
	// these values.
	KindDoc      Kind = "doc"
	KindProposal Kind = "proposal"
	KindDecision Kind = "decision"

	// KindCanonical and KindSpec are legacy compatibility spellings retained
	// for documents that predate explicit kinds. New documents must use the
	// portable kinds above; legacy values keep their historical path-inferred
	// meaning and surface a migration diagnostic via MigrationDiagnostics.
	KindCanonical Kind = "canonical"
	KindSpec      Kind = "spec"
)

// KindSource describes how a Document's Kind was determined. Explicit kinds
// win over the file path; inferred kinds are the legacy compatibility
// fallback and must surface a migration diagnostic.
const (
	KindSourceExplicit = "explicit"
	KindSourceLegacy   = "legacy"
)

// Document is the normalized in-memory representation shared by all managed
// documentation kinds. Raw is retained so later graph/map work can consume
// fields without having to re-parse the file.
//
// Kind is the normalized kind: an explicit `kind` front-matter value wins
// over the file path (KindSourceExplicit). Without one, the historical
// path-inferred kind applies (KindSourceLegacy) and MigrationDiagnostics
// reports the document for an explicit-kind disposition.
//
// Status keeps the authored lifecycle value, which is kind-specific: docs
// use current|superseded, proposals use proposed|accepted|implemented|
// superseded, and decisions use proposed|accepted|superseded. CodeConformance
// is independent of lifecycle: unverified|matches|drift|not_applicable, with
// an empty value meaning unknown legacy conformance (treated as unverified).
type Document struct {
	Path            string
	Kind            Kind
	KindSource      string
	Subject         string
	Keywords        []string
	PartOf          string
	Describes       []string
	Updates         []string
	Sources         []string
	DecidesFor      string
	Status          string
	SupersededBy    string
	CodeConformance string
	LastVerified    string
	Raw             map[string]any
	Body            string
}

// IsCanonicalFamily reports whether the kind belongs to the current-knowledge
// family (legacy canonical or explicit doc), as opposed to proposal/decision
// records.
func (k Kind) IsCanonicalFamily() bool {
	return k == KindCanonical || k == KindDoc
}

// IsForwardingStub reports whether the document is a legacy forwarding stub:
// a subject-less placeholder left at a moved document's old path that carries
// only a successor link (superseded_by) and minimum identity metadata. Stubs
// never own a subject, so they cannot create duplicate-subject ownership;
// resolution follows them to the current document.
func IsForwardingStub(doc Document) bool {
	if strings.TrimSpace(doc.Subject) != "" {
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(doc.Status), "superseded") {
		return false
	}
	return strings.TrimSpace(doc.SupersededBy) != ""
}

// Corpus is the shared parsed documentation model used by validation and the
// later find/map commands. Header issues are returned separately by
// LoadRepository so callers can still inspect every successfully parsed node.
type Corpus struct {
	Documents []Document
}

type Issue struct {
	Code    string
	Path    string
	Message string
}

// ParseError is returned when a document cannot be turned into a shared
// document model. Validation adds the file path and keeps checking the rest
// of the corpus so one bad header does not hide the next defect.
type ParseError struct {
	Code    string
	Message string
}

func (e *ParseError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

var (
	errMissingFrontmatter = &ParseError{Code: "DOC_HEADER_MISSING", Message: "missing YAML front matter (expected an opening --- line)"}
	versionedFilename     = regexp.MustCompile(`(?i)(?:[-_](?:v\d+|new|final)|\s*\(\d+\))$`)
)

// ParseDocHeaders parses one Markdown file. path is repo-relative when the
// caller is scanning a repository; it is used only to infer the document kind
// and is preserved on the returned model for diagnostics.
func ParseDocHeaders(path string, content []byte) (Document, error) {
	rel := filepath.ToSlash(filepath.Clean(path))
	inferred, ok := kindForPath(rel)
	if !ok {
		return Document{}, &ParseError{Code: "DOC_PATH_UNMANAGED", Message: "document is outside the managed docs, specs, and decision-log roots"}
	}

	frontmatter, body, err := parseFrontmatter(string(content))
	if err != nil {
		return Document{}, err
	}
	if err := validateFrontmatterTypes(frontmatter); err != nil {
		return Document{}, err
	}
	// An explicit kind always beats the file path. The legacy `spec`
	// spelling normalizes to proposal; unknown values keep the inferred
	// kind on the model so the corpus still loads, and validation reports
	// DOC_KIND_INVALID.
	kind, source := inferred, KindSourceLegacy
	if raw, declared := frontmatter["kind"]; declared && strings.TrimSpace(scalar(raw)) != "" {
		if explicit, ok := normalizeKind(scalar(raw)); ok {
			kind, source = explicit, KindSourceExplicit
		} else {
			source = KindSourceExplicit
		}
	}
	doc := Document{
		Path:            rel,
		Kind:            kind,
		KindSource:      source,
		Subject:         scalar(frontmatter["subject"]),
		Keywords:        list(frontmatter["keywords"]),
		PartOf:          scalar(frontmatter["part_of"]),
		Describes:       list(frontmatter["describes"]),
		Updates:         list(frontmatter["updates"]),
		Sources:         list(frontmatter["sources"]),
		DecidesFor:      scalar(frontmatter["decides_for"]),
		Status:          scalar(frontmatter["status"]),
		SupersededBy:    scalar(frontmatter["superseded_by"]),
		CodeConformance: strings.ToLower(strings.TrimSpace(scalar(frontmatter["code_conformance"]))),
		LastVerified:    dateScalar(frontmatter["last_verified"]),
		Raw:             frontmatter,
		Body:            body,
	}
	return doc, nil
}

// normalizeKind maps an explicit `kind` front-matter value to its normalized
// kind. `spec` stays accepted as the compatibility spelling for proposal and
// `canonical` keeps its historical meaning; anything else is rejected.
func normalizeKind(value string) (Kind, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "doc":
		return KindDoc, true
	case "proposal":
		return KindProposal, true
	case "decision":
		return KindDecision, true
	case "spec":
		return KindProposal, true
	case "canonical":
		return KindCanonical, true
	default:
		return "", false
	}
}

func validateFrontmatterTypes(frontmatter map[string]any) error {
	for _, key := range []string{"title", "subject", "part_of", "decides_for", "status", "superseded_by", "read_when", "skip_when", "kind", "code_conformance"} {
		value, ok := frontmatter[key]
		if !ok || value == nil {
			continue
		}
		if _, ok := value.(string); !ok {
			return &ParseError{Code: "DOC_HEADER_TYPE_INVALID", Message: fmt.Sprintf("front matter field %q must be a string", key)}
		}
	}
	if value, ok := frontmatter["last_verified"]; ok && value != nil {
		switch value.(type) {
		case string, time.Time:
		default:
			return &ParseError{Code: "DOC_HEADER_TYPE_INVALID", Message: `front matter field "last_verified" must be a date or string`}
		}
	}
	for _, key := range []string{"keywords", "describes", "updates", "sources", "aliases"} {
		value, ok := frontmatter[key]
		if !ok || value == nil {
			continue
		}
		if !validStringListValue(value) {
			return &ParseError{Code: "DOC_HEADER_TYPE_INVALID", Message: fmt.Sprintf("front matter field %q must be a string or list of strings", key)}
		}
	}
	return nil
}

func validStringListValue(value any) bool {
	if _, ok := value.(string); ok {
		return true
	}
	switch current := value.(type) {
	case []string:
		return true
	case []any:
		for _, item := range current {
			if _, ok := item.(string); !ok {
				return false
			}
		}
		return true
	default:
		return false
	}
}

// DocTouchReport identifies managed documents whose declared code paths
// intersect a change set, and the implicated documents still needing either a
// doc edit or a close-time waiver.
type DocTouchReport struct {
	Implicated []Document
	Missing    []Document
}

// CheckDocTouch applies the decision-4 path intersection. Describes entries
// are coarse repository paths: a directory entry covers every descendant.
func CheckDocTouch(corpus Corpus, changedPaths []string, waivers map[string]bool) DocTouchReport {
	changed := make([]string, 0, len(changedPaths))
	for _, path := range changedPaths {
		path = filepath.ToSlash(filepath.Clean(path))
		if path != "" && path != "." {
			changed = append(changed, path)
		}
	}
	var report DocTouchReport
	for _, doc := range corpus.Documents {
		if len(doc.Describes) == 0 || !describesChanged(doc.Describes, changed) {
			continue
		}
		report.Implicated = append(report.Implicated, doc)
		if pathChanged(doc.Path, changed) || waivers[strings.TrimSpace(doc.Subject)] || waivers[doc.Path] {
			continue
		}
		report.Missing = append(report.Missing, doc)
	}
	sort.Slice(report.Implicated, func(i, j int) bool { return report.Implicated[i].Path < report.Implicated[j].Path })
	sort.Slice(report.Missing, func(i, j int) bool { return report.Missing[i].Path < report.Missing[j].Path })
	return report
}

func describesChanged(describes, changed []string) bool {
	for _, described := range describes {
		described = filepath.ToSlash(filepath.Clean(strings.TrimSpace(described)))
		described = strings.TrimSuffix(described, "/**")
		described = strings.TrimSuffix(described, "/")
		if described == "" {
			continue
		}
		if described == "." && len(changed) > 0 {
			return true
		}
		for _, path := range changed {
			if path == described || strings.HasPrefix(path, described+"/") {
				return true
			}
		}
	}
	return false
}

func pathChanged(path string, changed []string) bool {
	path = filepath.ToSlash(filepath.Clean(path))
	for _, current := range changed {
		if current == path {
			return true
		}
	}
	return false
}

// LoadRepository scans only the canonical documentation roots:
// docs/system, .tusker/specs, and .tusker/specs/decisions. It returns named,
// header issues and reserves the error return for repository I/O.
func LoadRepository(repoRoot string) (Corpus, []Issue, error) {
	documents, issues, err := scanRepository(repoRoot)
	if err != nil {
		return Corpus{}, nil, err
	}
	return Corpus{Documents: documents}, issues, nil
}

// ValidateRepository validates the shared repository corpus, including
// cross-document subject uniqueness and tombstone successor references.
func ValidateRepository(repoRoot string) ([]Issue, error) {
	corpus, issues, err := LoadRepository(repoRoot)
	if err != nil {
		return nil, err
	}

	bySubject := make(map[string][]Document)
	for _, doc := range corpus.Documents {
		if strings.TrimSpace(doc.Subject) != "" {
			bySubject[doc.Subject] = append(bySubject[doc.Subject], doc)
		}
	}
	for subject, matches := range bySubject {
		if len(matches) < 2 {
			continue
		}
		paths := make([]string, 0, len(matches))
		for _, doc := range matches {
			paths = append(paths, doc.Path)
		}
		sort.Strings(paths)
		for _, doc := range matches {
			issues = append(issues, Issue{
				Code:    "DOC_DUPLICATE_SUBJECT",
				Path:    doc.Path,
				Message: fmt.Sprintf("duplicate subject %q; also declared in %s", subject, strings.Join(pathsWithout(paths, doc.Path), ", ")),
			})
		}
	}

	knownSubjects := make(map[string]struct{}, len(bySubject))
	for subject := range bySubject {
		knownSubjects[subject] = struct{}{}
	}
	for _, doc := range corpus.Documents {
		if strings.EqualFold(strings.TrimSpace(doc.Status), "superseded") {
			if strings.TrimSpace(doc.SupersededBy) == "" {
				issues = append(issues, Issue{
					Code:    "DOC_TOMBSTONE_SUCCESSOR_MISSING",
					Path:    doc.Path,
					Message: "superseded document must name its successor with superseded_by",
				})
				continue
			}
			if _, ok := knownSubjects[doc.SupersededBy]; !ok {
				issues = append(issues, Issue{
					Code:    "DOC_TOMBSTONE_SUCCESSOR_NOT_FOUND",
					Path:    doc.Path,
					Message: fmt.Sprintf("superseded document names successor %q, but that subject does not exist", doc.SupersededBy),
				})
			}
		}
	}
	issues = append(issues, brokenLinkIssues(corpus)...)

	sortIssues(issues)
	return issues, nil
}

// ValidateCorpus runs the header-field and cross-document checks over an
// already-parsed corpus without touching disk. It exists so an editor save
// path can validate a speculative edit — a document substituted in memory —
// before writing anything. It does not cover parse errors or versioned
// filenames, which are disk-scan concerns handled by LoadRepository.
func ValidateCorpus(corpus Corpus) []Issue {
	var issues []Issue
	for _, doc := range corpus.Documents {
		issues = append(issues, validateHeader(doc)...)
	}

	bySubject := make(map[string][]Document)
	for _, doc := range corpus.Documents {
		if strings.TrimSpace(doc.Subject) != "" {
			bySubject[doc.Subject] = append(bySubject[doc.Subject], doc)
		}
	}
	for subject, matches := range bySubject {
		if len(matches) < 2 {
			continue
		}
		paths := make([]string, 0, len(matches))
		for _, doc := range matches {
			paths = append(paths, doc.Path)
		}
		sort.Strings(paths)
		for _, doc := range matches {
			issues = append(issues, Issue{
				Code:    "DOC_DUPLICATE_SUBJECT",
				Path:    doc.Path,
				Message: fmt.Sprintf("duplicate subject %q; also declared in %s", subject, strings.Join(pathsWithout(paths, doc.Path), ", ")),
			})
		}
	}

	knownSubjects := make(map[string]struct{}, len(bySubject))
	for subject := range bySubject {
		knownSubjects[subject] = struct{}{}
	}
	for _, doc := range corpus.Documents {
		if strings.EqualFold(strings.TrimSpace(doc.Status), "superseded") {
			if strings.TrimSpace(doc.SupersededBy) == "" {
				issues = append(issues, Issue{
					Code:    "DOC_TOMBSTONE_SUCCESSOR_MISSING",
					Path:    doc.Path,
					Message: "superseded document must name its successor with superseded_by",
				})
				continue
			}
			if _, ok := knownSubjects[doc.SupersededBy]; !ok {
				issues = append(issues, Issue{
					Code:    "DOC_TOMBSTONE_SUCCESSOR_NOT_FOUND",
					Path:    doc.Path,
					Message: fmt.Sprintf("superseded document names successor %q, but that subject does not exist", doc.SupersededBy),
				})
			}
		}
	}
	issues = append(issues, brokenLinkIssues(corpus)...)

	sortIssues(issues)
	return issues
}

func scanRepository(repoRoot string) ([]Document, []Issue, error) {
	root, err := filepath.Abs(repoRoot)
	if err != nil {
		return nil, nil, err
	}
	var paths []string
	for _, relativeRoot := range []string{"docs/system", ".tusker/specs"} {
		scanRoot := filepath.Join(root, filepath.FromSlash(relativeRoot))
		if _, err := os.Stat(scanRoot); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return nil, nil, err
		}
		err := filepath.WalkDir(scanRoot, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				return nil
			}
			// Symlinked entries stay out of the corpus: Browse and the map
			// writers already refuse symlinked managed paths, and the scan
			// must not follow a link out of the worktree to read bytes.
			if entry.Type()&fs.ModeSymlink != 0 {
				return nil
			}
			// Only the known generated corpus index is excluded. Authored
			// indexes (00-index.md files and any authored nested INDEX.md)
			// remain visible corpus members.
			if strings.EqualFold(entry.Name(), "INDEX.md") {
				if rel, relErr := filepath.Rel(root, path); relErr == nil && filepath.ToSlash(rel) == legacyIndexRelPath {
					return nil
				}
			}
			if strings.HasSuffix(strings.ToLower(entry.Name()), ".md") {
				paths = append(paths, path)
			}
			return nil
		})
		if err != nil {
			return nil, nil, err
		}
	}
	sort.Strings(paths)

	var documents []Document
	var issues []Issue
	for _, path := range paths {
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return nil, nil, err
		}
		relative = filepath.ToSlash(relative)
		if versionedFilename.MatchString(strings.TrimSuffix(filepath.Base(relative), filepath.Ext(relative))) {
			issues = append(issues, Issue{
				Code:    "DOC_VERSIONED_FILENAME",
				Path:    relative,
				Message: "version-suffixed document filename is forbidden; update the existing subject in place or create an explicit tombstone",
			})
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return nil, nil, err
		}
		doc, err := ParseDocHeaders(relative, content)
		if err != nil {
			parseErr, ok := err.(*ParseError)
			if !ok {
				parseErr = &ParseError{Code: "DOC_HEADER_PARSE_ERROR", Message: err.Error()}
			}
			issues = append(issues, Issue{Code: parseErr.Code, Path: relative, Message: parseErr.Message})
			continue
		}
		documents = append(documents, doc)
		issues = append(issues, validateHeader(doc)...)
	}
	return documents, issues, nil
}

func validateHeader(doc Document) []Issue {
	var issues []Issue
	stub := IsForwardingStub(doc)
	if !stub && strings.TrimSpace(doc.Subject) == "" {
		issues = append(issues, Issue{Code: "DOC_REQUIRED_FIELD_MISSING", Path: doc.Path, Message: `missing required header field "subject"`})
	}
	if !stub && !isRoot(doc) && strings.TrimSpace(doc.PartOf) == "" {
		issues = append(issues, Issue{Code: "DOC_REQUIRED_FIELD_MISSING", Path: doc.Path, Message: `missing required header field "part_of"`})
	}
	if doc.Kind == KindDecision && strings.TrimSpace(doc.DecidesFor) == "" {
		issues = append(issues, Issue{Code: "DOC_REQUIRED_FIELD_MISSING", Path: doc.Path, Message: `missing required header field "decides_for"`})
	}
	if _, declared := doc.Raw["kind"]; declared && strings.TrimSpace(scalar(doc.Raw["kind"])) != "" {
		if _, ok := normalizeKind(scalar(doc.Raw["kind"])); !ok {
			issues = append(issues, Issue{Code: "DOC_KIND_INVALID", Path: doc.Path, Message: fmt.Sprintf("unknown document kind %q; declare one of doc, proposal, or decision", strings.TrimSpace(scalar(doc.Raw["kind"])))})
		}
	}
	if !ValidLifecycle(doc.Kind, doc.KindSource, doc.Status) {
		issues = append(issues, Issue{Code: "DOC_LIFECYCLE_INVALID", Path: doc.Path, Message: fmt.Sprintf("lifecycle %q is not valid for %s document kind %q; use %s", doc.Status, doc.KindSource, doc.Kind, strings.Join(LifecycleValues(doc.Kind), "|"))})
	}
	if code, message := checkConformance(doc); code != "" {
		issues = append(issues, Issue{Code: code, Path: doc.Path, Message: message})
	}
	return issues
}

// LifecycleValues returns the valid lifecycle values for a document kind.
// Docs use current|superseded, proposals use proposed|accepted|implemented|
// superseded, and decisions use proposed|accepted|superseded. Proposal
// acceptance records intent, not implementation proof: implemented requires
// recorded completion evidence and updated current documentation.
func LifecycleValues(kind Kind) []string {
	switch kind {
	case KindProposal, KindSpec:
		return []string{"proposed", "accepted", "implemented", "superseded"}
	case KindDecision:
		return []string{"proposed", "accepted", "superseded"}
	default:
		return []string{"current", "superseded"}
	}
}

// ValidLifecycle reports whether a lifecycle value is acceptable for a kind.
// Explicit portable kinds (doc, proposal, decision) enforce their exact
// value set, so a legacy `canonical` status is never silently translated
// into an approved or implemented state. Legacy compatibility spellings and
// path-inferred kinds additionally accept the historical `canonical` value;
// an empty value stays valid here and is dispositioned by
// MigrationDiagnostics instead.
func ValidLifecycle(kind Kind, source, status string) bool {
	value := strings.ToLower(strings.TrimSpace(status))
	if value == "" {
		return true
	}
	for _, allowed := range LifecycleValues(kind) {
		if value == allowed {
			return true
		}
	}
	if source == KindSourceLegacy || kind == KindCanonical || kind == KindSpec {
		return value == "canonical"
	}
	return false
}

// ValidCodeConformance reports whether a code_conformance value names a known
// state. Conformance is independent of lifecycle: unknown legacy conformance
// is simply unset and treated as unverified.
func ValidCodeConformance(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "unverified", "matches", "drift", "not_applicable":
		return true
	default:
		return false
	}
}

// checkConformance validates the independent code-conformance claim. matches
// requires both the last_verified date/commit stamp and a stated verification
// scope (describes); anything less cannot claim the code matches.
func checkConformance(doc Document) (string, string) {
	if !ValidCodeConformance(doc.CodeConformance) {
		return "DOC_CONFORMANCE_INVALID", fmt.Sprintf("unknown code_conformance %q; declare one of unverified, matches, drift, or not_applicable", doc.CodeConformance)
	}
	if doc.CodeConformance != "matches" {
		return "", ""
	}
	if stamp, ok := verifiedStamp(doc.LastVerified); !ok || strings.TrimSpace(stamp.Commit) == "" {
		return "DOC_CONFORMANCE_INVALID", `code_conformance "matches" requires a last_verified "YYYY-MM-DD @ <commit>" stamp`
	}
	if len(doc.Describes) == 0 {
		return "DOC_CONFORMANCE_INVALID", `code_conformance "matches" requires a stated verification scope in describes`
	}
	return "", ""
}

func isRoot(doc Document) bool {
	return doc.Kind.IsCanonicalFamily() && (doc.Subject == "overview" || doc.Path == "docs/system/00-overview.md")
}

func kindForPath(path string) (Kind, bool) {
	switch {
	case path == "docs/system" || strings.HasPrefix(path, "docs/system/"):
		return KindCanonical, true
	case path == ".tusker/specs/decisions" || strings.HasPrefix(path, ".tusker/specs/decisions/"):
		return KindDecision, true
	case path == ".tusker/specs" || strings.HasPrefix(path, ".tusker/specs/"):
		return KindSpec, true
	default:
		return "", false
	}
}

func parseFrontmatter(text string) (map[string]any, string, error) {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	if !strings.HasPrefix(text, "---\n") {
		return nil, "", errMissingFrontmatter
	}
	lines := strings.SplitAfter(text, "\n")
	lineStart := len(lines[0])
	closingStart := -1
	bodyStart := -1
	for i := 1; i < len(lines); i++ {
		line := strings.TrimSuffix(lines[i], "\n")
		if strings.TrimSpace(line) == "---" {
			closingStart = lineStart
			bodyStart = lineStart + len(lines[i])
			break
		}
		lineStart += len(lines[i])
	}
	if closingStart < 0 {
		return nil, "", &ParseError{Code: "DOC_HEADER_PARSE_ERROR", Message: "front matter has no closing --- line"}
	}
	openingLength := len(lines[0])
	raw := text[openingLength:closingStart]
	body := text[bodyStart:]
	var data map[string]any
	if err := yaml.Unmarshal([]byte(raw), &data); err != nil {
		return nil, "", &ParseError{Code: "DOC_HEADER_PARSE_ERROR", Message: "could not parse YAML front matter: " + err.Error()}
	}
	if data == nil {
		data = map[string]any{}
	}
	return data, body, nil
}
func scalar(value any) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func dateScalar(value any) string {
	if parsed, ok := value.(time.Time); ok {
		return parsed.Format("2006-01-02")
	}
	return scalar(value)
}

func list(value any) []string {
	if value == nil {
		return nil
	}
	if current, ok := value.(string); ok {
		if strings.TrimSpace(current) == "" {
			return nil
		}
		return []string{strings.TrimSpace(current)}
	}
	var result []string
	switch current := value.(type) {
	case []any:
		for _, item := range current {
			if value := scalar(item); value != "" {
				result = append(result, value)
			}
		}
	case []string:
		for _, item := range current {
			if value := strings.TrimSpace(item); value != "" {
				result = append(result, value)
			}
		}
	}
	return result
}

func pathsWithout(paths []string, excluded string) []string {
	result := make([]string, 0, len(paths)-1)
	removed := false
	for _, path := range paths {
		if path == excluded && !removed {
			removed = true
			continue
		}
		result = append(result, path)
	}
	return result
}

func sortIssues(issues []Issue) {
	sort.SliceStable(issues, func(i, j int) bool {
		if issues[i].Path != issues[j].Path {
			return issues[i].Path < issues[j].Path
		}
		if issues[i].Code != issues[j].Code {
			return issues[i].Code < issues[j].Code
		}
		return issues[i].Message < issues[j].Message
	})
}
