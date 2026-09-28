package main

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

type sharedCheckoutEntry struct{ Mode, OID string }

func (entry sharedCheckoutEntry) absent() bool {
	return entry.Mode == "" || strings.Trim(entry.Mode, "0") == ""
}

// sharedCheckoutStrategy reports whether landing must advance the default
// branch in place because concurrent agents share its checked-out folder.
func sharedCheckoutStrategy(vaultPath string) (bool, error) {
	if !fileExists(workflowPath(vaultPath)) {
		return false, nil
	}
	wf, err := loadWorkflow(vaultPath)
	if err != nil {
		return false, err
	}
	return workspaceStrategyFromWorkflow(wf.Data.Workspace.Strategy) == WorkspaceStrategyShared, nil
}

// advanceV7DefaultBranchRefShared is the shared-checkout counterpart of
// advanceV7DefaultBranchRef. It never runs the destructive preparation
// helpers (checkout of control docs, generated-projection reset, untracked
// Tusker file removal): the shared working tree belongs to live agents.
func advanceV7DefaultBranchRefShared(repoRoot, defaultBranch, newRev, oldRev string) ([]v7Worktree, error) {
	checkouts := v7DefaultBranchCheckouts(repoRoot, defaultBranch)
	switch len(checkouts) {
	case 0:
		return nil, updateGitRef(repoRoot, "refs/heads/"+defaultBranch, newRev, oldRev)
	case 1:
		return checkouts, advanceSharedCheckoutInPlace(checkouts[0].Path, defaultBranch, newRev, oldRev)
	default:
		return nil, tuskerError(errorInvalidTransition, defaultBranch+" is checked out in more than one worktree; shared-checkout landing needs exactly one")
	}
}

// advanceSharedCheckoutInPlace moves the checked-out default branch from
// oldRev to newRev without writing a single working-tree file. Every path the
// landing changes must already hold newRev's content on disk (the submitted
// work left it there); only those index entries move. Unrelated staged,
// unstaged and untracked work is left exactly as it was. Refusals leave the
// ref, index and files untouched.
func advanceSharedCheckoutInPlace(workDir, defaultBranch, newRev, oldRev string) (err error) {
	ref := "refs/heads/" + defaultBranch
	refuse := func(message string) error {
		return tuskerError(errorInvalidTransition, "cannot advance shared checkout of "+defaultBranch+" at "+workDir+": "+message, withPath(workDir))
	}
	indexPath, err := gitOutputTrim(workDir, "rev-parse", "--git-path", "index")
	if err != nil {
		return err
	}
	if !filepath.IsAbs(indexPath) {
		indexPath = filepath.Join(workDir, indexPath)
	}
	// Claim git's own index lock so no git command rewrites the index under
	// us. The prepared index is built inside the lock file and renamed over
	// the index, exactly as git itself commits an index update.
	lockPath := indexPath + ".lock"
	lock, err := os.OpenFile(lockPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return refuse("git index is locked: " + err.Error())
	}
	landed := false
	defer func() {
		if !landed {
			err = errors.Join(err, os.Remove(lockPath))
		}
	}()
	src, err := os.Open(indexPath)
	if err != nil {
		lock.Close()
		return err
	}
	_, copyErr := io.Copy(lock, src)
	src.Close()
	if err := errors.Join(copyErr, lock.Close()); err != nil {
		return err
	}

	if head, _ := gitOutputTrim(workDir, "symbolic-ref", "-q", "HEAD"); head != ref {
		return refuse("HEAD is not " + ref)
	}
	if current, _ := gitOutputTrim(workDir, "rev-parse", "--verify", ref+"^{commit}"); !strings.EqualFold(current, strings.TrimSpace(oldRev)) {
		return refuse(defaultBranch + " moved to " + shortCommit(current) + " (expected " + shortCommit(oldRev) + "); retry the landing")
	}
	if !gitMergeBaseAncestor(workDir, oldRev, newRev) {
		return refuse(shortCommit(oldRev) + " is not an ancestor of " + shortCommit(newRev))
	}

	// D: every path the landing changes, with its old and new tree entries.
	// --no-renames makes a rename a delete plus an add.
	raw, err := gitCommandInput(workDir, "", "diff-tree", "-r", "-z", "--no-renames", oldRev, newRev)
	if err != nil {
		return err
	}
	fields := strings.Split(strings.TrimSuffix(raw, "\x00"), "\x00")
	type change struct{ Old, New sharedCheckoutEntry }
	changes := map[string]change{}
	for i := 0; i+1 < len(fields); i += 2 {
		header := strings.Fields(strings.TrimPrefix(fields[i], ":"))
		if len(header) < 4 {
			return refuse("unexpected diff-tree output: " + fields[i])
		}
		changes[fields[i+1]] = change{Old: sharedCheckoutEntry{header[0], header[2]}, New: sharedCheckoutEntry{header[1], header[3]}}
	}
	paths := make([]string, 0, len(changes))
	for path := range changes {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	staged, err := gitCommandInput(workDir, "", "ls-files", "-s", "-z")
	if err != nil {
		return err
	}
	index := map[string]sharedCheckoutEntry{}
	for _, record := range strings.Split(staged, "\x00") {
		meta, path, ok := strings.Cut(record, "\t")
		if !ok {
			continue
		}
		parts := strings.Fields(meta)
		if len(parts) != 3 {
			return refuse("unexpected ls-files output: " + record)
		}
		if parts[2] != "0" {
			return refuse("index has unmerged path " + path)
		}
		if _, ok := changes[path]; ok {
			index[path] = sharedCheckoutEntry{parts[0], parts[1]}
		}
	}

	var thirdStaged, divergent []string
	var update strings.Builder
	for _, path := range paths {
		c := changes[path]
		entry := index[path]
		// Tusker's own records on disk are the live copy and run ahead of
		// the commit being landed. Land their entries without the byte
		// checks; the working files are left as they are, like every file.
		if !workspacePathIsTuskerBookkeeping(path) {
			if entry != c.Old && entry != c.New && !(entry.absent() && (c.Old.absent() || c.New.absent())) {
				thirdStaged = append(thirdStaged, path)
			}
			matches, err := sharedCheckoutWorktreeMatches(workDir, path, c.New)
			if err != nil {
				return err
			}
			if !matches {
				divergent = append(divergent, path)
			}
		}
		if c.New.absent() {
			update.WriteString("0 " + strings.Repeat("0", len(c.Old.OID)) + "\t" + path + "\x00")
		} else {
			update.WriteString(c.New.Mode + " " + c.New.OID + "\t" + path + "\x00")
		}
	}
	if len(divergent) > 0 {
		return refuse("working-tree files do not match the landed commit (edited after submit?): " + strings.Join(limitStrings(divergent, 12), ", "))
	}
	if len(thirdStaged) > 0 {
		return refuse("index stages a version that is neither the old nor the landed commit: " + strings.Join(limitStrings(thirdStaged, 12), ", "))
	}
	if len(paths) > 0 {
		cmd := exec.Command("git", "-C", workDir, "update-index", "-z", "--index-info")
		cmd.Env = append(os.Environ(), "GIT_INDEX_FILE="+lockPath)
		cmd.Stdin = strings.NewReader(update.String())
		if output, err := cmd.CombinedOutput(); err != nil {
			return refuse("could not prepare index: " + firstActionableLine(string(output), err.Error()))
		}
	}
	if err := updateGitRef(workDir, ref, newRev, oldRev); err != nil {
		return refuse(defaultBranch + " moved during landing (compare-and-swap failed): " + err.Error())
	}
	// ponytail: no crash journal. If the process dies between the ref CAS
	// above and this rename, git status shows the landed paths as staged
	// reversions (index still at the old commit) until `git reset -q -- <D>`
	// runs; the working tree is already correct. Journal D before the CAS if
	// that window matters.
	if err := os.Rename(lockPath, indexPath); err != nil {
		return err
	}
	landed = true
	return nil
}

// sharedCheckoutWorktreeMatches reports whether the file at path already is
// the landed entry: same blob through git's filters, same exec bit or symlink
// kind, and absent when the landing deletes it.
func sharedCheckoutWorktreeMatches(workDir, path string, want sharedCheckoutEntry) (bool, error) {
	absolute := filepath.Join(workDir, filepath.FromSlash(path))
	info, err := os.Lstat(absolute)
	if errors.Is(err, os.ErrNotExist) {
		return want.absent(), nil
	}
	if err != nil {
		return false, err
	}
	var oid string
	switch want.Mode {
	case "100644", "100755":
		if !info.Mode().IsRegular() || (info.Mode()&0o111 != 0) != (want.Mode == "100755") {
			return false, nil
		}
		oid, err = gitCommandInput(workDir, "", "hash-object", "--path", path, "--", absolute)
	case "120000":
		if info.Mode()&os.ModeSymlink == 0 {
			return false, nil
		}
		target, readErr := os.Readlink(absolute)
		if readErr != nil {
			return false, readErr
		}
		oid, err = gitCommandInput(workDir, target, "hash-object", "--no-filters", "--stdin")
	default:
		// Absent-but-present, gitlinks and anything else Git cannot hash here.
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(oid) == want.OID, nil
}
