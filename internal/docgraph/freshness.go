package docgraph

import (
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

var ErrDocumentNotFound = errors.New("document subject not found")

// FreshnessRecord is the compact status view used by `tusker docs status`.
type FreshnessRecord struct {
	Document        Document
	TouchingCommits int
	NeverVerified   bool
}

// CurrentCommit returns the repository commit used as the immutable anchor
// for a freshness stamp. A date-only stamp cannot answer which commits should
// count after a clock change or a rebased branch.
func CurrentCommit(repoRoot string) (string, error) {
	if !isGitRepo(repoRoot) {
		return "", fmt.Errorf("repository is not a git worktree: %s", repoRoot)
	}
	out, err := exec.Command("git", "-C", repoRoot, "rev-parse", "HEAD").Output()
	if err != nil {
		return "", fmt.Errorf("resolve repository HEAD: %w", err)
	}
	commit := strings.TrimSpace(string(out))
	if commit == "" {
		return "", errors.New("repository has no commit to stamp")
	}
	return commit, nil
}

// DocsFreshness counts distinct commits touching each document's describes
// paths after its last_verified commit. Unstamped (or legacy date-only) docs
// sort ahead because their freshness is unknown.
func DocsFreshness(repoRoot string) ([]FreshnessRecord, error) {
	corpus, _, err := LoadRepository(repoRoot)
	if err != nil {
		return nil, err
	}
	result := make([]FreshnessRecord, 0, len(corpus.Documents))
	for _, doc := range corpus.Documents {
		stamp, ok := verifiedStamp(doc.LastVerified)
		if ok && !verifiedCommitExists(repoRoot, stamp.Commit) {
			ok = false
		}
		record := FreshnessRecord{Document: doc, NeverVerified: !ok}
		if ok {
			record.TouchingCommits = countTouchingCommits(repoRoot, doc.Describes, stamp.Commit)
		}
		result = append(result, record)
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].NeverVerified != result[j].NeverVerified {
			return result[i].NeverVerified
		}
		if result[i].TouchingCommits != result[j].TouchingCommits {
			return result[i].TouchingCommits > result[j].TouchingCommits
		}
		return result[i].Document.Path < result[j].Document.Path
	})
	return result, nil
}

// DocsCoverageGaps lists top-level repository code areas that no canonical
// document claims. The inventory is intentionally coarse: docs declare
// coarse paths, so the useful question is whether a top-level subsystem has a
// route at all, not whether every file has prose.
func DocsCoverageGaps(repoRoot string) ([]string, error) {
	corpus, _, err := LoadRepository(repoRoot)
	if err != nil {
		return nil, err
	}
	claimed := make(map[string]bool)
	for _, doc := range corpus.Documents {
		if doc.Kind != KindCanonical {
			continue
		}
		for _, path := range doc.Describes {
			path = strings.Trim(filepath.ToSlash(filepath.Clean(strings.TrimSpace(path))), "/")
			if path == "" || path == "." {
				continue
			}
			claimed[strings.Split(path, "/")[0]] = true
		}
	}
	tracked, err := trackedGoTopLevels(repoRoot)
	if err != nil {
		return nil, err
	}
	var gaps []string
	for _, name := range tracked {
		if !claimed[name] {
			gaps = append(gaps, name)
		}
	}
	sort.Strings(gaps)
	return gaps, nil
}

func trackedGoTopLevels(repoRoot string) ([]string, error) {
	if !isGitRepo(repoRoot) {
		return nil, nil
	}
	out, err := exec.Command("git", "-C", repoRoot, "ls-files", "-z", "--", "*.go").Output()
	if err != nil {
		return nil, fmt.Errorf("list tracked Go files: %w", err)
	}
	seen := map[string]bool{}
	for _, path := range strings.Split(string(out), "\x00") {
		path = filepath.ToSlash(filepath.Clean(path))
		if path == "" || path == "." || strings.HasPrefix(path, "../") {
			continue
		}
		seen[strings.Split(path, "/")[0]] = true
	}
	areas := make([]string, 0, len(seen))
	for area := range seen {
		areas = append(areas, area)
	}
	sort.Strings(areas)
	return areas, nil
}

// StampDocument writes today's verification date and current commit into the
// matching document. It refuses an unknown subject before touching the
// repository.
func StampDocument(repoRoot, subject, date string) (string, error) {
	corpus, _, err := LoadRepository(repoRoot)
	if err != nil {
		return "", err
	}
	found := false
	for _, doc := range corpus.Documents {
		if strings.EqualFold(strings.TrimSpace(doc.Subject), strings.TrimSpace(subject)) {
			found = true
			break
		}
	}
	if !found {
		return "", fmt.Errorf("%w: %s", ErrDocumentNotFound, subject)
	}
	commit, err := CurrentCommit(repoRoot)
	if err != nil {
		return "", err
	}
	return StampDocumentAt(repoRoot, subject, date, commit)
}

// StampDocumentAt is the deterministic form used when the caller already
// resolved the commit. Keeping the commit explicit avoids a second HEAD lookup
// between the command's output and the bytes written to disk.
func StampDocumentAt(repoRoot, subject, date, commit string) (string, error) {
	if strings.TrimSpace(commit) == "" {
		return "", errors.New("freshness stamp requires a repository commit")
	}
	if !verifiedCommitExists(repoRoot, commit) {
		return "", fmt.Errorf("freshness stamp commit is not a known repository commit: %s", commit)
	}
	root, err := openDocsMapRoot(repoRoot)
	if err != nil {
		return "", err
	}
	defer root.Close()
	corpus, _, err := LoadRepository(repoRoot)
	if err != nil {
		return "", err
	}
	for _, doc := range corpus.Documents {
		if strings.EqualFold(strings.TrimSpace(doc.Subject), strings.TrimSpace(subject)) {
			clean := filepath.Clean(filepath.FromSlash(doc.Path))
			if err := rejectDocsMapSymlinkPath(root, clean); err != nil {
				return "", fmt.Errorf("documentation file path is symlinked: %s: %w", doc.Path, err)
			}
			content, err := root.ReadFile(clean)
			if err != nil {
				return "", err
			}
			stamp := strings.TrimSpace(date)
			if strings.TrimSpace(commit) != "" {
				stamp += " @ " + strings.TrimSpace(commit)
			}
			updated, err := stampFrontmatter(string(content), stamp)
			if err != nil {
				return "", err
			}
			if err := writeDocsMapArtifact(root, doc.Path, []byte(updated)); err != nil {
				return "", err
			}
			return doc.Path, nil
		}
	}
	return "", fmt.Errorf("%w: %s", ErrDocumentNotFound, subject)
}

type verifiedStampValue struct {
	Date   time.Time
	Commit string
}

func verifiedStamp(value string) (verifiedStampValue, bool) {
	value = strings.TrimSpace(value)
	parts := strings.SplitN(value, " @ ", 2)
	if len(parts) != 2 || strings.TrimSpace(parts[1]) == "" {
		return verifiedStampValue{}, false
	}
	parsed, err := time.Parse("2006-01-02", strings.TrimSpace(parts[0]))
	if err != nil {
		return verifiedStampValue{}, false
	}
	return verifiedStampValue{Date: parsed, Commit: strings.TrimSpace(parts[1])}, true
}

func verifiedCommitExists(repoRoot, commit string) bool {
	commit = strings.TrimSpace(commit)
	if commit == "" || !isGitRepo(repoRoot) {
		return false
	}
	out, err := exec.Command("git", "-C", repoRoot, "rev-parse", "--verify", commit+"^{commit}").Output()
	return err == nil && strings.TrimSpace(string(out)) != ""
}

func countTouchingCommits(repoRoot string, describes []string, commit string) int {
	if len(describes) == 0 || !isGitRepo(repoRoot) {
		return 0
	}
	if strings.TrimSpace(commit) == "" {
		return 0
	}
	args := []string{"-C", repoRoot, "log", "--format=%H", commit + "..HEAD", "--"}
	for _, path := range describes {
		path = filepath.ToSlash(filepath.Clean(strings.TrimSpace(path)))
		path = strings.TrimSuffix(path, "/**")
		if path != "" {
			args = append(args, path)
		}
	}
	if len(args) == 6 { // no usable pathspecs
		return 0
	}
	out, err := exec.Command("git", args...).Output()
	if err != nil {
		return 0
	}
	seen := map[string]struct{}{}
	for _, hash := range strings.Fields(string(out)) {
		seen[hash] = struct{}{}
	}
	return len(seen)
}

func isGitRepo(repoRoot string) bool {
	return exec.Command("git", "-C", repoRoot, "rev-parse", "--git-dir").Run() == nil
}

func stampFrontmatter(content, date string) (string, error) {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	if !strings.HasPrefix(content, "---\n") {
		return "", &ParseError{Code: "DOC_HEADER_MISSING", Message: "missing YAML front matter (expected an opening --- line)"}
	}
	lines := strings.SplitAfter(content, "\n")
	for i := 1; i < len(lines); i++ {
		line := strings.TrimSuffix(lines[i], "\n")
		if strings.TrimSpace(line) == "---" {
			for j := 1; j < i; j++ {
				key := strings.TrimSpace(strings.TrimSuffix(lines[j], "\n"))
				if strings.HasPrefix(key, "last_verified:") {
					lines[j] = "last_verified: " + date + "\n"
					return strings.Join(lines, ""), nil
				}
			}
			lines = append(lines[:i], append([]string{"last_verified: " + date + "\n"}, lines[i:]...)...)
			return strings.Join(lines, ""), nil
		}
	}
	return "", &ParseError{Code: "DOC_HEADER_PARSE_ERROR", Message: "front matter has no closing --- line"}
}
