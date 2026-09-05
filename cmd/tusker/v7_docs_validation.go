package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"tusker/internal/docgraph"
)

// validateCanonicalDocOpenings applies the existing readable-shape heuristic
// to managed docs. Header completeness remains owned by docgraph; this check
// only protects the first reader-facing paragraph.
func validateCanonicalDocOpenings(repoRoot string) ([]Issue, error) {
	corpus, _, err := docgraph.LoadRepository(repoRoot)
	if err != nil {
		return nil, err
	}
	var issues []Issue
	for _, doc := range corpus.Documents {
		offenders := v7DocOpeningCodeWords(doc.Body)
		if len(offenders) == 0 {
			continue
		}
		shown := offenders
		if len(shown) > 5 {
			shown = shown[:5]
		}
		issues = append(issues, issue(
			"DOC_OPENING_CODE_WORDS",
			fmt.Sprintf("managed document opening slips into code words: %s", strings.Join(shown, ", ")),
			doc.Path,
			"rewrite the opening paragraph in plain language; keep paths, symbols, and commands in implementation details",
			map[string]any{"words": offenders},
		))
	}
	return issues, nil
}

func v7DocOpeningCodeWords(body string) []string {
	opening := v7DocOpeningParagraph(body)
	if opening == "" {
		return nil
	}
	appendixSymbols := map[string]bool{}
	seen := map[string]bool{}
	seenBase := map[string]bool{}
	var offenders []string
	add := func(word string) {
		word = strings.TrimSpace(word)
		if word == "" || seen[word] {
			return
		}
		base := word
		if idx := strings.LastIndex(word, "/"); idx >= 0 {
			base = word[idx+1:]
		}
		if seenBase[base] {
			return
		}
		seen[word], seenBase[base] = true, true
		offenders = append(offenders, word)
	}
	for _, line := range strings.Split(opening, "\n") {
		v7CollectLineCodeWords(line, appendixSymbols, add)
	}
	return offenders
}

func v7DocOpeningParagraph(body string) string {
	lines := strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n")
	seenTitle := false
	started := false
	var opening []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			if started {
				break
			}
			continue
		}
		if strings.HasPrefix(trimmed, "#") {
			if !seenTitle {
				seenTitle = true
				continue
			}
			if started {
				break
			}
			continue
		}
		if !seenTitle || strings.HasPrefix(trimmed, "|") || strings.HasPrefix(trimmed, "-") {
			continue
		}
		if trimmed == "" {
			if started {
				break
			}
			continue
		}
		started = true
		opening = append(opening, line)
	}
	return strings.TrimSpace(strings.Join(opening, "\n"))
}

// validateLockedSpecUpdates enforces the locked-spec landing rule. A target
// must exist and either have landed after the spec or have a clearly related
// doc-update task in the owning epic. A pending task is surfaced as a warning;
// an unplanned update is a validation error.
func validateLockedSpecUpdates(repoRoot, vaultPath string, notes []Note) ([]Issue, error) {
	corpus, _, err := docgraph.LoadRepository(repoRoot)
	if err != nil {
		return nil, err
	}
	byPath := map[string]docgraph.Document{}
	bySubject := map[string]docgraph.Document{}
	for _, doc := range corpus.Documents {
		byPath[doc.Path] = doc
		if strings.TrimSpace(doc.Subject) != "" {
			bySubject[strings.ToLower(strings.TrimSpace(doc.Subject))] = doc
		}
	}
	var issues []Issue
	for _, spec := range corpus.Documents {
		if spec.Kind != docgraph.KindSpec || !rawBool(spec.Raw["decisions_locked"]) {
			continue
		}
		specPath := filepath.ToSlash(spec.Path)
		for _, rawTarget := range spec.Updates {
			target := filepath.ToSlash(strings.TrimSpace(rawTarget))
			if target == "" {
				continue
			}
			targetDoc, targetOK := byPath[target]
			if !targetOK {
				targetDoc, targetOK = bySubject[strings.ToLower(target)]
			}
			if !targetOK && !repoFileExistsForSpec(repoRoot, target) {
				issues = append(issues, issue("SPEC_UPDATE_TARGET_MISSING", fmt.Sprintf("locked spec updates target %q, but it does not exist", target), spec.Path, "create the target document or remove it from updates", map[string]any{"target": target}))
				continue
			}
			relatedTask := docUpdateTaskExists(notes, specPath, target)
			landed := targetOK && docUpdatedAfterSpecForTarget(repoRoot, specPath, targetDoc.Path, target)
			// A follow-up documentation task is the explicit authority for a
			// multi-commit update. Without that link, a later edit to the target
			// is not evidence that this spec's update landed: it may be an
			// unrelated typo fix.
			if !landed && relatedTask {
				landed = targetOK && docTargetChangedAfterSpec(repoRoot, specPath, targetDoc.Path, target)
			}
			if !targetOK {
				landed = docUpdatedAfterSpecForTarget(repoRoot, specPath, target, target)
			}
			if landed {
				continue
			}
			if relatedTask {
				issues = append(issues, issue("SPEC_UPDATES_PENDING", fmt.Sprintf("locked spec update %q has a related doc-update task but has not landed", target), spec.Path, "land the target edit before closing the task", map[string]any{"target": target}))
				continue
			}
			issues = append(issues, issue("SPEC_UPDATES_UNLANDED", fmt.Sprintf("locked spec update %q was not edited and has no doc-update task", target), spec.Path, "edit the target in the same change or create a doc-update task in the owning epic", map[string]any{"target": target}))
		}
	}
	return issues, nil
}

func rawBool(value any) bool {
	switch current := value.(type) {
	case bool:
		return current
	case string:
		parsed, err := strconv.ParseBool(strings.TrimSpace(current))
		return err == nil && parsed
	default:
		return false
	}
}

func repoFileExistsForSpec(repoRoot, relative string) bool {
	if filepath.IsAbs(relative) || relative == "." || strings.HasPrefix(relative, "../") {
		return false
	}
	info, err := os.Stat(filepath.Join(repoRoot, filepath.FromSlash(relative)))
	return err == nil && info.Mode().IsRegular()
}

func docUpdatedAfterSpec(repoRoot, specPath, targetPath string) bool {
	return docUpdatedAfterSpecForTarget(repoRoot, specPath, targetPath, targetPath)
}

func docUpdatedAfterSpecForTarget(repoRoot, specPath, targetPath, targetRef string) bool {
	if gitRepoAvailable(repoRoot) && gitPathDirty(repoRoot, specPath) {
		return false
	}
	// The only objective evidence that an update landed without a linked
	// follow-up task is a single change that touched both the locked spec and
	// its target. Comparing commit timestamps lets an unrelated later typo
	// masquerade as the required update.
	if changed := gitChangedPathsForSpecUpdate(repoRoot, specPath, targetPath, targetRef); changed {
		return true
	}
	if gitRepoAvailable(repoRoot) {
		return false
	}
	return docFileUpdatedAfterSpec(repoRoot, specPath, targetPath)
}

func docFileUpdatedAfterSpec(repoRoot, specPath, targetPath string) bool {
	specTime, specOK := docRevisionTime(repoRoot, specPath)
	targetTime, targetOK := docRevisionTime(repoRoot, targetPath)
	return specOK && targetOK && targetTime >= specTime
}

// docTargetChangedAfterSpec reports whether the target has a committed change
// after the spec was introduced. It is only used when docUpdateTaskExists has
// already supplied the intent link for a multi-commit update.
func docTargetChangedAfterSpec(repoRoot, specPath, targetPath, targetRef string) bool {
	if gitRepoAvailable(repoRoot) && gitPathDirty(repoRoot, specPath) {
		return false
	}
	specCommit, specOK := docAuthorityCommit(repoRoot, specPath, targetRef)
	targetCommit, targetOK := docRevisionCommit(repoRoot, targetPath)
	if !specOK || !targetOK || specCommit == targetCommit {
		return false
	}
	return gitMergeBaseAncestor(repoRoot, specCommit, targetCommit)
}

// gitChangedPathsForSpecUpdate is deliberately path-based. A commit that
// introduced or materially updated the locked spec must include the target
// path in its own diff; a later unrelated edit is not sufficient evidence.
func gitChangedPathsForSpecUpdate(repoRoot, specPath, targetPath, targetRef string) bool {
	specCommit, ok := docAuthorityCommit(repoRoot, specPath, targetRef)
	if !ok {
		return false
	}
	output, err := gitOutputTrim(repoRoot, "diff-tree", "--root", "--no-commit-id", "--name-only", "-r", "-m", specCommit)
	if err != nil {
		return false
	}
	changed := map[string]bool{}
	for _, path := range strings.Split(output, "\n") {
		path = filepath.ToSlash(strings.TrimSpace(path))
		if path != "" {
			changed[path] = true
		}
	}
	return changed[filepath.ToSlash(filepath.Clean(specPath))] && changed[filepath.ToSlash(filepath.Clean(targetPath))]
}

func docAuthorityCommit(repoRoot, relative, targetRef string) (string, bool) {
	// Anchor the obligation to the commit that introduced or changed the
	// authority fields, rather than a later metadata-only edit to the spec.
	// The target-specific arm also catches changes to a multiline updates list.
	targetPattern := regexp.QuoteMeta(filepath.ToSlash(strings.TrimSpace(targetRef)))
	pattern := "(^[[:space:]]*decisions_locked:[[:space:]]*true[[:space:]]*$)|(^[[:space:]]*updates:[[:space:]]*.*$)|(^[[:space:]]*-[[:space:]]*" + targetPattern + "[[:space:]]*$)"
	output, err := gitOutputTrim(repoRoot, "log", "--follow", "--format=%H", "-1", "-G", pattern, "--", filepath.ToSlash(filepath.Clean(relative)))
	return strings.TrimSpace(output), err == nil && strings.TrimSpace(output) != ""
}

func docRevisionCommit(repoRoot, relative string) (string, bool) {
	output, err := gitOutputTrim(repoRoot, "log", "--follow", "--format=%H", "-1", "--", filepath.ToSlash(filepath.Clean(relative)))
	return strings.TrimSpace(output), err == nil && strings.TrimSpace(output) != ""
}

func gitPathDirty(repoRoot, relative string) bool {
	path := filepath.ToSlash(filepath.Clean(relative))
	for _, args := range [][]string{
		{"diff", "--name-only", "HEAD", "--", path},
		{"ls-files", "--others", "--exclude-standard", "--", path},
	} {
		output, err := gitOutputTrim(repoRoot, args...)
		if err == nil && strings.TrimSpace(output) != "" {
			return true
		}
	}
	return false
}

func gitRepoAvailable(repoRoot string) bool {
	_, err := gitOutputTrim(repoRoot, "rev-parse", "--git-dir")
	return err == nil
}

func docRevisionTime(repoRoot, relative string) (int64, bool) {
	if output, err := gitOutputTrim(repoRoot, "log", "-1", "--format=%ct", "--", relative); err == nil {
		if parsed, err := strconv.ParseInt(strings.TrimSpace(output), 10, 64); err == nil {
			return parsed, true
		}
	}
	info, err := os.Stat(filepath.Join(repoRoot, filepath.FromSlash(relative)))
	if err != nil {
		return 0, false
	}
	return info.ModTime().Unix(), true
}

// docUpdateTaskExists accepts only an exact task-level governing spec
// reference. Epic metadata is optional because imported epics predate the
// spec_refs field, but when an owning epic does declare the spec we still keep
// the task scoped to it.
func docUpdateTaskExists(notes []Note, specPath, target string) bool {
	owningEpics := map[string]bool{}
	for _, note := range notes {
		if effectiveV7Kind(note.Data) != "epic" || !specRefListContains(note.Data["spec_refs"], specPath) {
			continue
		}
		if id := strings.TrimSpace(stringField(note.Data, "id")); id != "" {
			owningEpics[id] = true
		}
	}
	hasOwningEpic := len(owningEpics) > 0
	for _, note := range notes {
		if effectiveV7Kind(note.Data) != "task" || !specRefListContains(note.Data["spec_refs"], specPath) {
			continue
		}
		if hasOwningEpic && !owningEpics[strings.TrimSpace(stringField(note.Data, "epic"))] {
			continue
		}
		if containsDocUpdateMarker(note, target) {
			return true
		}
	}
	return false
}

func specRefListContains(value any, want string) bool {
	want = filepath.ToSlash(strings.TrimSpace(want))
	for _, ref := range normalizeList(value) {
		if filepath.ToSlash(strings.TrimSpace(v7CleanSpecRef(ref))) == want {
			return true
		}
	}
	return false
}

func containsDocUpdateMarker(note Note, target string) bool {
	for _, node := range normalizeList(note.Data["doc_nodes"]) {
		if strings.EqualFold(filepath.ToSlash(strings.TrimSpace(node)), target) || strings.EqualFold(strings.TrimSpace(node), strings.TrimSuffix(filepath.Base(target), filepath.Ext(target))) {
			return true
		}
	}
	text := strings.ToLower(stringField(note.Data, "title") + "\n" + note.Body)
	return strings.Contains(text, "documentation") || strings.Contains(text, "doc update") || strings.Contains(text, "canonical doc") || strings.Contains(text, "readable-shape")
}
