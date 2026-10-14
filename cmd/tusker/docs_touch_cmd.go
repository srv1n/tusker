package main

import (
	"fmt"
	"os"
	"strings"

	"tusker/internal/docgraph"
)

// docsTouchCmd runs the close-time doc-touch check without a task: every path
// changed in <ref>...HEAD plus uncommitted work (staged, unstaged, untracked)
// is intersected with each document's describes globs.
func docsTouchCmd(args Args) error {
	ref := strings.TrimSpace(args.String("since"))
	if ref == "" || ref == "true" {
		return tuskerError(errorMissingArg, "docs touch needs --since <ref>, for example: tusker check --only docs --since origin/main")
	}
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	rule, err := v7DocsTouchRule(vaultPath)
	if err != nil {
		return err
	}
	repoRoot := v7RepoRoot(vaultPath)
	if !v7GitRepo(repoRoot) {
		return tuskerError(errorInvalidArg, "docs touch needs a git repository: "+repoRoot)
	}
	if _, ok := gitRevParse(repoRoot, ref+"^{commit}"); !ok {
		return tuskerError(errorInvalidArg, fmt.Sprintf("docs touch --since %q does not resolve to a commit", ref))
	}
	waivers := map[string]bool{}
	for _, subject := range strings.Split(args.String("waive"), "\n") {
		if subject = strings.TrimSpace(subject); subject != "" && subject != "true" {
			waivers[subject] = true
		}
	}
	result := map[string]any{"schema": "tusker.docs-touch/v1", "since": ref, "rule": rule, "documents": []map[string]string{}}
	if rule == "off" {
		if args.Bool("json") {
			emitJSON(result)
		}
		return nil
	}
	corpus, _, err := docgraph.LoadRepository(repoRoot)
	if err != nil {
		return err
	}
	changed, err := docsTouchChangedPaths(repoRoot, ref)
	if err != nil {
		return err
	}
	// No describes anywhere yields an empty report, matching v7DocTouchCheck.
	report := docgraph.CheckDocTouch(corpus, changed, waivers)
	missing := make([]string, 0, len(report.Missing))
	rows := make([]map[string]string, 0, len(report.Missing))
	for _, doc := range report.Missing {
		missing = append(missing, doc.Subject)
		rows = append(rows, map[string]string{"subject": doc.Subject, "path": doc.Path})
		if !args.Bool("json") {
			fmt.Fprintf(os.Stderr, "warning: doc-touch drift: %s (%s) describes changed code; edit the doc or pass --waive %s\n", doc.Subject, doc.Path, doc.Subject)
		}
	}
	result["documents"], result["changed_paths"] = rows, changed
	if args.Bool("json") {
		emitJSON(result)
	}
	if rule == "block" && len(missing) > 0 {
		err := tuskerError("DOC_TOUCH_DRIFT", "documentation drift since "+ref+": "+strings.Join(missing, ", "), withContext(map[string]any{"documents": missing, "changed_paths": changed}))
		if args.Bool("json") {
			return afterResultEmitted(err)
		}
		return err
	}
	return nil
}

// docsTouchChangedPaths unions the committed range, the index (vs HEAD), the
// worktree (vs index) and untracked files; `git diff HEAD` alone would hide a
// staged change whose working copy was restored. --no-renames makes a rename
// show as delete + add, so both paths count.
func docsTouchChangedPaths(repoRoot, ref string) ([]string, error) {
	var all strings.Builder
	for _, gitArgs := range [][]string{
		{"diff", "--no-renames", "--name-only", "-z", ref + "...HEAD"},
		{"diff", "--no-renames", "--name-only", "-z", "--cached"},
		{"diff", "--no-renames", "--name-only", "-z"},
		{"ls-files", "--others", "--exclude-standard", "-z"},
	} {
		output, err := gitCombined(repoRoot, gitArgs...)
		if err != nil {
			return nil, err
		}
		all.WriteString(output)
		all.WriteString("\x00")
	}
	return splitGitPaths(all.String()), nil
}
