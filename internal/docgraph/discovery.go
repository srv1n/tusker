package docgraph

import (
	"errors"
	"fmt"
	"os"
	pathpkg "path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const (
	DefaultBrowseLimit = 40
	MaxBrowseLimit     = 200
)

// PathError is a stable, machine-readable failure for a managed-document
// route. Commands add the same code to their typed CLI errors.
type PathError struct {
	Code    string
	Path    string
	Message string
}

func (e *PathError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

type BrowseEntry struct {
	Path          string `json:"path"`
	Name          string `json:"name"`
	Kind          string `json:"kind"`
	ChildrenKnown bool   `json:"children_known"`
	Summary       string `json:"summary"`
	Subject       string `json:"subject,omitempty"`
	Title         string `json:"title,omitempty"`
	DocumentKind  Kind   `json:"document_kind,omitempty"`
	Status        string `json:"status,omitempty"`
	ReadWhen      string `json:"read_when,omitempty"`
	SkipWhen      string `json:"skip_when,omitempty"`
	PartOf        string `json:"part_of,omitempty"`
}

type BrowseResult struct {
	Path      string        `json:"path"`
	Entries   []BrowseEntry `json:"entries"`
	Truncated bool          `json:"truncated"`
	Limit     int           `json:"limit"`
	Omitted   int           `json:"omitted"`
}

// NormalizeManagedPath accepts only repository-relative paths in the shared
// documentation roots. It uses slash semantics on every platform so CLI
// output and references remain portable.
func NormalizeManagedPath(raw string) (string, error) {
	original := strings.TrimSpace(raw)
	normalized := strings.TrimSpace(strings.ReplaceAll(original, "\\", "/"))
	if normalized == "" {
		return "", &PathError{Code: "DOC_PATH_EMPTY", Path: original, Message: "document path is required"}
	}
	if strings.HasPrefix(normalized, "/") {
		return "", &PathError{Code: "DOC_PATH_ESCAPE", Path: original, Message: "document path must be repository-relative"}
	}
	clean := pathpkg.Clean(normalized)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", &PathError{Code: "DOC_PATH_ESCAPE", Path: original, Message: "document path escapes the repository"}
	}
	if !isManagedPath(clean) {
		return "", &PathError{Code: "DOC_PATH_UNMANAGED", Path: original, Message: "path is outside the managed docs and specs roots"}
	}
	return clean, nil
}

func isManagedPath(path string) bool {
	return path == DocsSystemRoot || strings.HasPrefix(path, DocsSystemRoot+"/") ||
		path == SpecsRoot || strings.HasPrefix(path, SpecsRoot+"/")
}

// Browse reads one directory level. It deliberately opens the repository via
// os.Root so a symlink cannot redirect a managed path outside the worktree.
func Browse(repoRoot, relative string, limit int) (BrowseResult, error) {
	clean, err := NormalizeManagedPath(relative)
	if err != nil {
		return BrowseResult{}, err
	}
	if limit <= 0 {
		limit = DefaultBrowseLimit
	}
	if limit > MaxBrowseLimit {
		limit = MaxBrowseLimit
	}

	root, err := openDocsMapRoot(repoRoot)
	if err != nil {
		return BrowseResult{}, err
	}
	defer root.Close()
	if err := rejectDocsMapSymlinkPath(root, filepath.FromSlash(clean)); err != nil {
		return BrowseResult{}, &PathError{Code: "DOC_PATH_SYMLINK", Path: clean, Message: "managed document path is symlinked: " + clean}
	}

	info, err := root.Lstat(filepath.FromSlash(clean))
	if errors.Is(err, os.ErrNotExist) {
		return BrowseResult{}, &PathError{Code: "DOC_PATH_NOT_FOUND", Path: clean, Message: "managed document directory does not exist: " + clean}
	}
	if err != nil {
		return BrowseResult{}, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return BrowseResult{}, &PathError{Code: "DOC_PATH_SYMLINK", Path: clean, Message: "managed document path is symlinked: " + clean}
	}
	if !info.IsDir() {
		return BrowseResult{}, &PathError{Code: "DOC_PATH_NOT_DIRECTORY", Path: clean, Message: "managed document path is not a directory: " + clean}
	}

	directory, err := root.Open(filepath.FromSlash(clean))
	if err != nil {
		return BrowseResult{}, err
	}
	entries, err := directory.ReadDir(-1)
	closeErr := directory.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return BrowseResult{}, err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	result := BrowseResult{Path: clean + "/", Limit: limit, Entries: []BrowseEntry{}}
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		child := pathpkg.Join(clean, entry.Name())
		if entry.IsDir() {
			summary, summaryErr := browseFolderSummary(root, child)
			if summaryErr != nil {
				var pathErr *PathError
				if errors.As(summaryErr, &pathErr) {
					return BrowseResult{}, pathErr
				}
				return BrowseResult{}, browseParseError(child+"/00-index.md", summaryErr)
			}
			result.Entries = append(result.Entries, BrowseEntry{
				Path:          child + "/",
				Name:          entry.Name(),
				Kind:          "folder",
				ChildrenKnown: true,
				Summary:       summary,
			})
			continue
		}
		if entry.Name() == "INDEX.md" || !strings.HasSuffix(strings.ToLower(entry.Name()), ".md") {
			continue
		}
		content, err := root.ReadFile(filepath.FromSlash(child))
		if err != nil {
			return BrowseResult{}, err
		}
		doc, err := ParseDocHeaders(child, content)
		if err != nil {
			return BrowseResult{}, browseParseError(child, err)
		}
		result.Entries = append(result.Entries, BrowseEntry{
			Path:         child,
			Name:         entry.Name(),
			Kind:         "file",
			Summary:      "",
			Subject:      doc.Subject,
			Title:        DocumentTitle(doc),
			DocumentKind: doc.Kind,
			Status:       doc.Status,
			ReadWhen:     compactDiscoveryValue(doc.Raw["read_when"]),
			SkipWhen:     compactDiscoveryValue(doc.Raw["skip_when"]),
			PartOf:       doc.PartOf,
		})
	}
	sort.SliceStable(result.Entries, func(i, j int) bool {
		if result.Entries[i].Kind != result.Entries[j].Kind {
			return result.Entries[i].Kind == "folder"
		}
		return result.Entries[i].Name < result.Entries[j].Name
	})
	if len(result.Entries) > limit {
		result.Omitted = len(result.Entries) - limit
		result.Truncated = true
		result.Entries = result.Entries[:limit]
	}
	return result, nil
}

func browseFolderSummary(root *os.Root, path string) (string, error) {
	summaryPath := pathpkg.Join(path, "00-index.md")
	if err := rejectDocsMapSymlinkPath(root, filepath.FromSlash(summaryPath)); err != nil {
		return "", &PathError{Code: "DOC_PATH_SYMLINK", Path: summaryPath, Message: "managed document path is symlinked: " + summaryPath}
	}
	content, err := root.ReadFile(filepath.FromSlash(summaryPath))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", err
	}
	doc, err := ParseDocHeaders(summaryPath, content)
	if err != nil {
		return "", err
	}
	return DocumentTitle(doc), nil
}

func browseParseError(path string, err error) error {
	code := "DOC_HEADER_PARSE_ERROR"
	if parsed, ok := err.(*ParseError); ok {
		code = parsed.Code
	}
	return &PathError{Code: code, Path: path, Message: path + ": " + err.Error()}
}

// DocumentTitle returns the user-facing title without requiring every caller
// to know that older documents may have only a first Markdown heading.
func DocumentTitle(doc Document) string {
	if title := compactDiscoveryValue(doc.Raw["title"]); title != "" {
		return title
	}
	for _, line := range strings.Split(doc.Body, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "# ") {
			return strings.TrimSpace(strings.TrimPrefix(trimmed, "# "))
		}
	}
	return doc.Subject
}

func compactDiscoveryValue(value any) string {
	if value == nil {
		return ""
	}
	return strings.Join(strings.Fields(fmt.Sprint(value)), " ")
}

type Section struct {
	Heading string `json:"heading"`
	Body    string `json:"body"`
}

type SectionNotFoundError struct {
	Heading   string
	Available []string
}

func (e *SectionNotFoundError) Error() string {
	if e == nil {
		return ""
	}
	if len(e.Available) == 0 {
		return fmt.Sprintf("document section %q was not found; the document has no headings", e.Heading)
	}
	return fmt.Sprintf("document section %q was not found; available headings: %s", e.Heading, strings.Join(e.Available, ", "))
}

var markdownHeading = regexp.MustCompile(`^(#{1,6})[ \t]+(.+?)[ \t]*$`)

// ReadSection returns one exact Markdown heading section. Nested headings
// remain in the section; the next heading at the same or shallower level ends
// it. The input may be either "## Heading" or the heading text alone.
func ReadSection(body, heading string) (Section, error) {
	target := strings.TrimSpace(heading)
	if target == "" {
		return Section{}, &SectionNotFoundError{Heading: target}
	}
	wantLine := strings.HasPrefix(target, "#")
	wantText := strings.TrimSpace(strings.TrimLeft(target, "#"))
	lines := strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n")
	headings := markdownHeadingLines(lines)
	available := make([]string, 0, len(headings))
	start, level := -1, 0
	for _, headingLine := range headings {
		available = append(available, headingLine.Full)
		if (wantLine && headingLine.Full == target) || (!wantLine && headingLine.Text == wantText) {
			start = headingLine.Index
			level = headingLine.Level
			break
		}
	}
	if start < 0 {
		return Section{}, &SectionNotFoundError{Heading: target, Available: available}
	}
	end := len(lines)
	for _, headingLine := range headings {
		if headingLine.Index > start && headingLine.Level <= level {
			end = headingLine.Index
			break
		}
	}
	return Section{Heading: strings.TrimSpace(lines[start]), Body: strings.TrimSpace(strings.Join(lines[start+1:end], "\n"))}, nil
}

type markdownHeadingLine struct {
	Index int
	Level int
	Text  string
	Full  string
}

func markdownHeadingLines(lines []string) []markdownHeadingLine {
	result := make([]markdownHeadingLine, 0)
	inFence := false
	for index, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		match := markdownHeading.FindStringSubmatch(trimmed)
		if len(match) == 0 {
			continue
		}
		result = append(result, markdownHeadingLine{Index: index, Level: len(match[1]), Text: strings.TrimSpace(match[2]), Full: trimmed})
	}
	return result
}
