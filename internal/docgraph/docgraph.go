// Package docgraph parses and validates the repository's canonical document
// headers. It intentionally knows nothing about Tusker task records: docs,
// specs, and decision logs are one small, shared corpus with different edge
// fields.
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

	"gopkg.in/yaml.v3"
)

type Kind string

const (
	KindCanonical Kind = "canonical"
	KindSpec      Kind = "spec"
	KindDecision  Kind = "decision"
)

// Document is the normalized in-memory representation shared by all managed
// documentation kinds. Raw is retained so later graph/map work can consume
// fields without having to re-parse the file.
type Document struct {
	Path         string
	Kind         Kind
	Subject      string
	Keywords     []string
	PartOf       string
	Updates      []string
	Sources      []string
	DecidesFor   string
	Status       string
	SupersededBy string
	Raw          map[string]any
	Body         string
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
	kind, ok := kindForPath(rel)
	if !ok {
		return Document{}, &ParseError{Code: "DOC_PATH_UNMANAGED", Message: "document is outside the managed docs, specs, and decision-log roots"}
	}

	frontmatter, body, err := parseFrontmatter(string(content))
	if err != nil {
		return Document{}, err
	}
	doc := Document{
		Path:         rel,
		Kind:         kind,
		Subject:      scalar(frontmatter["subject"]),
		Keywords:     list(frontmatter["keywords"]),
		PartOf:       scalar(frontmatter["part_of"]),
		Updates:      list(frontmatter["updates"]),
		Sources:      list(frontmatter["sources"]),
		DecidesFor:   scalar(frontmatter["decides_for"]),
		Status:       scalar(frontmatter["status"]),
		SupersededBy: scalar(frontmatter["superseded_by"]),
		Raw:          frontmatter,
		Body:         body,
	}
	return doc, nil
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
			if entry.Name() == "INDEX.md" {
				return nil
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
	if strings.TrimSpace(doc.Subject) == "" {
		issues = append(issues, Issue{Code: "DOC_REQUIRED_FIELD_MISSING", Path: doc.Path, Message: `missing required header field "subject"`})
	}
	if !isRoot(doc) && strings.TrimSpace(doc.PartOf) == "" {
		issues = append(issues, Issue{Code: "DOC_REQUIRED_FIELD_MISSING", Path: doc.Path, Message: `missing required header field "part_of"`})
	}
	if doc.Kind == KindDecision && strings.TrimSpace(doc.DecidesFor) == "" {
		issues = append(issues, Issue{Code: "DOC_REQUIRED_FIELD_MISSING", Path: doc.Path, Message: `missing required header field "decides_for"`})
	}
	return issues
}

func isRoot(doc Document) bool {
	return doc.Kind == KindCanonical && (doc.Subject == "overview" || doc.Path == "docs/system/00-overview.md")
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
