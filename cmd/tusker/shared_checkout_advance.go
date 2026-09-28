package main

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
)

type sharedCheckoutEntry struct{ Mode, OID string }

func (entry sharedCheckoutEntry) absent() bool {
	return entry.Mode == "" || strings.Trim(entry.Mode, "0") == ""
}

// same compares entries, treating every spelling of "absent" as equal.
func (entry sharedCheckoutEntry) same(other sharedCheckoutEntry) bool {
	return entry == other || (entry.absent() && other.absent())
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
// oldRev to newRev without overwriting a file someone changed. For every path
// the landing changes, the index entry must be oldRev's or newRev's, and the
// file on disk must already hold newRev's content (the submitted work left it
// there) or still hold oldRev's content exactly with oldRev staged, in which
// case Tusker writes newRev's version just before the ref moves. Tusker's own
// records (.tusker/ bookkeeping) skip the disk comparison and are never
// written: the live copy runs ahead of the landed commit. Unrelated staged,
// unstaged and untracked work is left exactly as it was. Refusals leave the
// ref, index and files untouched.
func advanceSharedCheckoutInPlace(workDir, defaultBranch, newRev, oldRev string) (err error) {
	ref := "refs/heads/" + defaultBranch
	refuse := func(message string) error {
		return tuskerError(errorInvalidTransition, "cannot advance shared checkout of "+defaultBranch+" at "+workDir+": "+message, withPath(workDir))
	}
	indexPath, err := sharedCheckoutIndexPath(workDir)
	if err != nil {
		return err
	}
	// Claim git's own index lock so no git command rewrites the index under
	// us. The prepared index is built inside the lock file and renamed over
	// the index, exactly as git itself commits an index update.
	lockPath := indexPath + ".lock"
	lock, err := os.OpenFile(lockPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return refuse("git index is locked: " + err.Error())
	}
	refMoved := false
	defer func() {
		// Once the ref has moved, the prepared index is the only correct
		// index; never delete it.
		if !refMoved {
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
	changes, paths, err := sharedCheckoutChanges(workDir, oldRev, newRev)
	if err != nil {
		return refuse(err.Error())
	}
	index, err := sharedCheckoutIndexEntries(workDir, changes)
	if err != nil {
		return refuse(err.Error())
	}

	var thirdStaged, divergent, writes []string
	for _, path := range paths {
		c := changes[path]
		entry := index[path]
		stagedOld := entry.same(c.Old)
		if !stagedOld && !entry.same(c.New) {
			thirdStaged = append(thirdStaged, path)
		}
		// Tusker's own records on disk are the live copy and run ahead of
		// the commit being landed: no disk comparison and no disk write.
		if workspacePathIsTuskerBookkeeping(path) {
			continue
		}
		matches, err := sharedCheckoutWorktreeMatches(workDir, path, c.New)
		if err != nil {
			return err
		}
		if matches {
			continue
		}
		// Nobody touched the file since oldRev: Tusker writes newRev's copy.
		if unchanged, err := sharedCheckoutWorktreeMatches(workDir, path, c.Old); err != nil {
			return err
		} else if unchanged && stagedOld {
			writes = append(writes, path)
			continue
		}
		divergent = append(divergent, path)
	}
	if len(divergent) > 0 {
		return refuse("working-tree files match neither the old nor the landed commit (edited after submit?): " + strings.Join(limitStrings(divergent, 12), ", "))
	}
	if len(thirdStaged) > 0 {
		return refuse("index stages a version that is neither the old nor the landed commit: " + strings.Join(limitStrings(thirdStaged, 12), ", "))
	}
	if len(paths) > 0 {
		cmd := exec.Command("git", "-C", workDir, "update-index", "-z", "--index-info")
		cmd.Env = append(os.Environ(), "GIT_INDEX_FILE="+lockPath)
		cmd.Stdin = strings.NewReader(sharedCheckoutIndexInfo(changes, paths))
		if output, err := cmd.CombinedOutput(); err != nil {
			return refuse("could not prepare index: " + firstActionableLine(string(output), err.Error()))
		}
	}
	// ponytail: a failed write leaves earlier writes on disk with the ref
	// unmoved. Those paths now hold newRev's content, which a retry accepts.
	if err := sharedCheckoutWriteLanded(workDir, changes, writes); err != nil {
		return refuse("could not write landed files: " + err.Error())
	}
	if err := updateGitRef(workDir, ref, newRev, oldRev); err != nil {
		return refuse(defaultBranch + " moved during landing (compare-and-swap failed): " + err.Error())
	}
	refMoved = true
	// A crash from here until the rename leaves HEAD at newRev with the old
	// index and this lock file; promotion recovery runs
	// repairSharedCheckoutIndexAfterAdvance once the lock is gone.
	if err := sharedCheckoutRename(lockPath, indexPath); err != nil {
		if err := sharedCheckoutRename(lockPath, indexPath); err != nil {
			return tuskerError(errorInvalidTransition, defaultBranch+" advanced to "+shortCommit(newRev)+" but installing the prepared index failed; it is left at "+lockPath+": "+err.Error(), withPath(lockPath))
		}
	}
	return nil
}

// sharedCheckoutRename is swapped by tests to simulate a failed index install.
var sharedCheckoutRename = os.Rename

// repairSharedCheckoutIndexAfterAdvance finishes an in-place advance whose
// ref moved but whose prepared index was never installed: every path in
// oldRev..newRev whose index entry is still oldRev's gets newRev's entry.
// Nothing else is touched. A leftover index.lock is reported, never removed.
func repairSharedCheckoutIndexAfterAdvance(workDir, oldRev, newRev string) error {
	changes, paths, err := sharedCheckoutChanges(workDir, oldRev, newRev)
	if err != nil {
		return err
	}
	index, err := sharedCheckoutIndexEntries(workDir, changes)
	if err != nil {
		return err
	}
	var stale []string
	for _, path := range paths {
		if index[path].same(changes[path].Old) {
			stale = append(stale, path)
		}
	}
	if len(stale) == 0 {
		return nil
	}
	output, err := gitCombinedInput(workDir, sharedCheckoutIndexInfo(changes, stale), "update-index", "-z", "--index-info")
	if err != nil {
		message := "could not repair shared-checkout index after advancing to " + shortCommit(newRev) + ": " + firstActionableLine(output, err.Error())
		if indexPath, pathErr := sharedCheckoutIndexPath(workDir); pathErr == nil && fileExists(indexPath+".lock") {
			message += "; " + indexPath + ".lock exists (a crashed landing may have left it; if no git process is running, inspect and remove it, then retry)"
		}
		return tuskerError(errorInvalidTransition, message, withPath(workDir))
	}
	return nil
}

func gitCombinedInput(workDir, input string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", workDir}, args...)...)
	cmd.Stdin = strings.NewReader(input)
	output, err := cmd.CombinedOutput()
	return string(output), err
}

func sharedCheckoutIndexPath(workDir string) (string, error) {
	indexPath, err := gitOutputTrim(workDir, "rev-parse", "--git-path", "index")
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(indexPath) {
		indexPath = filepath.Join(workDir, indexPath)
	}
	return indexPath, nil
}

type sharedCheckoutChange struct{ Old, New sharedCheckoutEntry }

// sharedCheckoutChanges lists every path oldRev..newRev changes with its old
// and new tree entries. --no-renames makes a rename a delete plus an add.
func sharedCheckoutChanges(workDir, oldRev, newRev string) (map[string]sharedCheckoutChange, []string, error) {
	raw, err := gitCommandInput(workDir, "", "diff-tree", "-r", "-z", "--no-renames", oldRev, newRev)
	if err != nil {
		return nil, nil, err
	}
	fields := strings.Split(strings.TrimSuffix(raw, "\x00"), "\x00")
	changes := map[string]sharedCheckoutChange{}
	for i := 0; i+1 < len(fields); i += 2 {
		header := strings.Fields(strings.TrimPrefix(fields[i], ":"))
		if len(header) < 4 {
			return nil, nil, errors.New("unexpected diff-tree output: " + fields[i])
		}
		changes[fields[i+1]] = sharedCheckoutChange{Old: sharedCheckoutEntry{header[0], header[2]}, New: sharedCheckoutEntry{header[1], header[3]}}
	}
	paths := make([]string, 0, len(changes))
	for path := range changes {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return changes, paths, nil
}

// sharedCheckoutIndexEntries reads the stage-0 index entries of the changed
// paths; a missing path reads as the absent entry.
func sharedCheckoutIndexEntries(workDir string, changes map[string]sharedCheckoutChange) (map[string]sharedCheckoutEntry, error) {
	staged, err := gitCommandInput(workDir, "", "ls-files", "-s", "-z")
	if err != nil {
		return nil, err
	}
	index := map[string]sharedCheckoutEntry{}
	for _, record := range strings.Split(staged, "\x00") {
		meta, path, ok := strings.Cut(record, "\t")
		if !ok {
			continue
		}
		parts := strings.Fields(meta)
		if len(parts) != 3 {
			return nil, errors.New("unexpected ls-files output: " + record)
		}
		if parts[2] != "0" {
			return nil, errors.New("index has unmerged path " + path)
		}
		if _, ok := changes[path]; ok {
			index[path] = sharedCheckoutEntry{parts[0], parts[1]}
		}
	}
	return index, nil
}

func sharedCheckoutIndexInfo(changes map[string]sharedCheckoutChange, paths []string) string {
	var update strings.Builder
	for _, path := range paths {
		c := changes[path]
		if c.New.absent() {
			update.WriteString("0 " + strings.Repeat("0", len(c.Old.OID)) + "\t" + path + "\x00")
		} else {
			update.WriteString(c.New.Mode + " " + c.New.OID + "\t" + path + "\x00")
		}
	}
	return update.String()
}

// sharedCheckoutWriteLanded makes each path hold newRev's entry on disk:
// deletions first (so a file can replace a directory), then creations and
// updates through a temp file renamed into place.
func sharedCheckoutWriteLanded(workDir string, changes map[string]sharedCheckoutChange, paths []string) error {
	for _, path := range paths {
		if !changes[path].New.absent() {
			continue
		}
		absolute := filepath.Join(workDir, filepath.FromSlash(path))
		if err := os.Remove(absolute); err != nil && !sharedCheckoutMissing(err) {
			return err
		}
		// Drop directories the deletion emptied, as git checkout does.
		for dir := filepath.Dir(absolute); dir != workDir && strings.HasPrefix(dir, workDir) && os.Remove(dir) == nil; dir = filepath.Dir(dir) {
		}
	}
	for _, path := range paths {
		want := changes[path].New
		if want.absent() {
			continue
		}
		absolute := filepath.Join(workDir, filepath.FromSlash(path))
		args := []string{"cat-file", "--filters", "--path=" + path, want.OID}
		if want.Mode == "120000" {
			args = []string{"cat-file", "blob", want.OID}
		}
		body, err := gitCommandInput(workDir, "", args...)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(absolute), 0o755); err != nil {
			return err
		}
		temp := filepath.Join(filepath.Dir(absolute), ".tusker-land-"+filepath.Base(absolute))
		_ = os.Remove(temp)
		if want.Mode == "120000" {
			err = os.Symlink(body, temp)
		} else {
			perm := os.FileMode(0o644)
			if want.Mode == "100755" {
				perm = 0o755
			}
			if err = os.WriteFile(temp, []byte(body), perm); err == nil {
				err = os.Chmod(temp, perm)
			}
		}
		if err == nil {
			err = os.Rename(temp, absolute)
		}
		if err != nil {
			_ = os.Remove(temp)
			return err
		}
	}
	return nil
}

func sharedCheckoutMissing(err error) bool {
	return errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ENOTDIR)
}

// sharedCheckoutWorktreeMatches reports whether the file at path already is
// the given entry: same blob through git's filters, same exec bit or symlink
// kind, and absent when the entry is absent. A directory standing where an
// absent entry sits (a file replaced by a directory) counts as absent.
func sharedCheckoutWorktreeMatches(workDir, path string, want sharedCheckoutEntry) (bool, error) {
	absolute := filepath.Join(workDir, filepath.FromSlash(path))
	info, err := os.Lstat(absolute)
	if sharedCheckoutMissing(err) {
		return want.absent(), nil
	}
	if err != nil {
		return false, err
	}
	if info.IsDir() {
		return want.absent(), nil
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
