package docgraph

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	inlineCodeSpan   = regexp.MustCompile("`([^`\n]+)`")
	ignorePathMarker = regexp.MustCompile(`<!--\s*docs-check:\s*ignore-path\s+(\S+)\s*-->`)
	pathLineSuffix   = regexp.MustCompile(`(:\d+(-\d+)?)+$`)
	// schemaID matches versioned schema names such as tusker.task/v7.
	schemaID = regexp.MustCompile(`^[\w-]+\.[\w.-]+/v\d+$`)
)

// deadRefIssues reports describes entries and inline backtick paths in
// current documents that no longer exist under repoRoot. Proposals and
// superseded documents are exempt: they name planned or historical paths.
func deadRefIssues(repoRoot string, corpus Corpus) []Issue {
	var issues []Issue
	for _, doc := range corpus.Documents {
		if !deadRefApplies(doc) {
			continue
		}
		ignored := map[string]bool{}
		for _, m := range ignorePathMarker.FindAllStringSubmatch(doc.Body, -1) {
			ignored[m[1]] = true
		}
		for _, described := range doc.Describes {
			described = strings.TrimSpace(described)
			if described == "" || ignored[described] || pathExists(repoRoot, described) {
				continue
			}
			issues = append(issues, Issue{Code: "DOC_DESCRIBES_DEAD", Path: doc.Path,
				Message: fmt.Sprintf("describes entry %q matches no file in the repository", described)})
		}
		seen := map[string]bool{}
		for _, ref := range codePathRefs(doc.Body) {
			if seen[ref] || ignored[ref] {
				continue
			}
			seen[ref] = true
			if !pathExists(repoRoot, ref) {
				issues = append(issues, Issue{Code: "DOC_CODE_PATH_DEAD", Path: doc.Path,
					Message: fmt.Sprintf("inline code path `%s` does not exist in the repository", ref)})
			}
		}
	}
	return issues
}

func deadRefApplies(doc Document) bool {
	status := strings.ToLower(strings.TrimSpace(doc.Status))
	switch {
	case doc.Kind.IsCanonicalFamily():
		return status == "current" || status == "canonical"
	case doc.Kind == KindDecision:
		return status == "accepted"
	}
	return false
}

// codePathRefs returns inline backtick spans outside fenced blocks that look
// like repository paths, with any trailing :line or #anchor stripped.
func codePathRefs(body string) []string {
	var refs []string
	// CommonMark fences: a fence closes only on a line of the opener's
	// character at least as long as the opener, with nothing after it.
	var fenceChar byte
	fenceLen := 0
	for _, line := range strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n") {
		trimmed := strings.TrimSpace(line)
		indent := len(line) - len(strings.TrimLeft(line, " "))
		run := 0
		if indent <= 3 && trimmed != "" && (trimmed[0] == '`' || trimmed[0] == '~') {
			for run < len(trimmed) && trimmed[run] == trimmed[0] {
				run++
			}
		}
		if fenceLen > 0 {
			if run >= fenceLen && trimmed[0] == fenceChar && strings.TrimSpace(trimmed[run:]) == "" {
				fenceLen = 0
			}
			continue
		}
		if run >= 3 {
			fenceChar, fenceLen = trimmed[0], run
			continue
		}
		for _, m := range inlineCodeSpan.FindAllStringSubmatch(line, -1) {
			if ref, ok := codePathCandidate(m[1]); ok {
				refs = append(refs, ref)
			}
		}
	}
	return refs
}

func codePathCandidate(span string) (string, bool) {
	s := strings.TrimSpace(span)
	if !strings.Contains(s, "/") || strings.ContainsAny(s, " \t") ||
		strings.Contains(s, "://") || strings.HasPrefix(s, "-") || strings.HasPrefix(s, "$") {
		return "", false
	}
	// Not repo paths: absolute paths and API/slash routes (/api/x, /clear),
	// home paths (~/.config), <placeholder> templates, env assignments and
	// query strings (key=value), and versioned schema IDs. A bare ? is a glob.
	if strings.HasPrefix(s, "/") || strings.HasPrefix(s, "~") ||
		strings.ContainsAny(s, "<>=") || schemaID.MatchString(s) {
		return "", false
	}
	if i := strings.Index(s, "#"); i >= 0 {
		s = s[:i]
	}
	s = pathLineSuffix.ReplaceAllString(s, "")
	if s == "" {
		return "", false
	}
	return s, true
}

// pathExists reports whether a repo-relative path exists, or a glob matches
// at least one file. Absolute paths and paths escaping repoRoot never exist.
func pathExists(repoRoot, rel string) bool {
	rel = filepath.ToSlash(strings.TrimSpace(rel))
	if strings.HasPrefix(rel, "/") || filepath.IsAbs(rel) {
		return false
	}
	rel = path.Clean(rel)
	if rel == ".." || strings.HasPrefix(rel, "../") {
		return false
	}
	if !hasGlobMeta(rel) {
		_, err := os.Stat(filepath.Join(repoRoot, filepath.FromSlash(rel)))
		return err == nil
	}
	// Walk only below the literal prefix before the first glob segment.
	segments := strings.Split(rel, "/")
	base := "."
	for i, seg := range segments {
		if hasGlobMeta(seg) {
			base = path.Join(segments[:i]...)
			break
		}
	}
	if base == "" {
		base = "."
	}
	// ponytail: one walk per glob; share a single file list across globs if
	// docs check gets slow on large repos with many glob describes.
	found := false
	_ = filepath.WalkDir(filepath.Join(repoRoot, filepath.FromSlash(base)), func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		r, err := filepath.Rel(repoRoot, p)
		if err == nil && globCovers(rel, filepath.ToSlash(r)) {
			found = true
			return fs.SkipAll
		}
		return nil
	})
	return found
}
