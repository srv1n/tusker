package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const defaultScratchTTLDays = 14

const defaultScratchBudgetBytes int64 = 200 << 20 // 200 MiB

const scratchRetentionLockTimeout = 10 * time.Second

// maxScratchTTLDays stays below the point where days overflow an int64
// nanosecond time.Duration (~106751 days); past it the cutoff would wrap into
// the future and select every entry for deletion.
const maxScratchTTLDays = 100000

var (
	// errNotTuskerVault means the path is not provably a Tusker vault, so no
	// deletion is authorized there. Callers treat it as "nothing to do".
	errNotTuskerVault = errors.New("not a Tusker vault")
	// errScratchRootUnsafe means scratch exists but is not a real directory
	// (most importantly, it is a symlink that would redirect deletion).
	errScratchRootUnsafe = errors.New("scratch root is not a real directory")
	// errNotScratchChild means a deletion target is not a single direct child
	// of the scratch root.
	errNotScratchChild = errors.New("target is not a direct child of scratch")
)

// scratchEntry is one top-level entry under <vault>/scratch.
type scratchEntry struct {
	Name   string    // entry name, e.g. "SGC-T-0001" or "orig-piano"
	Path   string    // absolute path
	Bytes  int64     // logical size beneath it (or its own size if a file)
	Newest time.Time // newest mtime found beneath it
}

// scratchGCOutcome reports what an apply actually did, including partial
// progress when it stops early. Bytes are logical FileInfo sizes, not measured
// freed blocks: hardlinks, sparse files, and open descriptors all make the two
// differ.
type scratchGCOutcome struct {
	Deleted   []scratchEntry
	Skipped   []scratchEntry // changed after planning; deliberately left alone
	Reclaimed int64
	Failed    string // entry path that errored, if any
}

// scratchRootPath is the unvalidated display path. Never delete based on it;
// use resolveScratchRoot instead.
func scratchRootPath(vaultPath string) string {
	vaultPath = strings.TrimSpace(vaultPath)
	if vaultPath == "" {
		return ""
	}
	return filepath.Join(vaultPath, "scratch")
}

// vaultAuthorizesDeletion is deliberately stricter than isVaultDir: a directory
// that merely contains work/ is not enough to authorize recursive deletion,
// because ordinary repositories have one.
func vaultAuthorizesDeletion(vaultPath string) bool {
	if !isV7VaultLayout(vaultPath) {
		return false
	}
	return fileExists(filepath.Join(vaultPath, "WORKFLOW.md")) ||
		fileExists(filepath.Join(vaultPath, "SKILL.md")) ||
		fileExists(filepath.Join(vaultPath, "config.yaml")) ||
		dirExists(filepath.Join(vaultPath, "_system"))
}

// resolveScratchRoot is the single authorization point for destroying anything
// under scratch. It fails closed: the vault must be provably a Tusker vault,
// and the scratch component itself must be a real directory. A symlinked vault
// root stays supported; only a symlinked scratch is refused, because following
// it would redirect deletion outside the vault.
func resolveScratchRoot(vaultPath string) (string, error) {
	vaultPath = strings.TrimSpace(vaultPath)
	if vaultPath == "" {
		return "", errNotTuskerVault
	}
	abs, err := filepath.Abs(vaultPath)
	if err != nil {
		return "", err
	}
	if !vaultAuthorizesDeletion(abs) {
		return "", errNotTuskerVault
	}
	root := filepath.Join(abs, "scratch")
	info, err := os.Lstat(root)
	if err != nil {
		if os.IsNotExist(err) {
			return root, nil // nothing there yet; callers handle an empty scan
		}
		return "", err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return "", errScratchRootUnsafe
	}
	return root, nil
}

// withScratchRetentionLock serializes Tusker-owned scratch writers with GC.
// The lock is outside scratch, rejects symlink redirects, and has a bounded
// wait so a stuck peer cannot hang an operator command forever.
func withScratchRetentionLock(vaultPath string, fn func() error) error {
	raw := strings.TrimSpace(vaultPath)
	if raw == "" {
		return errNotTuskerVault
	}
	root, err := filepath.Abs(raw)
	if err != nil {
		return errNotTuskerVault
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil || !vaultAuthorizesDeletion(root) {
		return errNotTuskerVault
	}
	lockDir := filepath.Join(root, "_system", "locks")
	for _, dir := range []string{filepath.Join(root, "_system"), lockDir} {
		if info, statErr := os.Lstat(dir); statErr == nil && info.Mode()&os.ModeSymlink != 0 {
			return errScratchRootUnsafe
		}
	}
	if err := os.MkdirAll(lockDir, 0700); err != nil {
		return err
	}
	if info, statErr := os.Stat(lockDir); statErr != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return errScratchRootUnsafe
	}
	lockPath := filepath.Join(lockDir, "scratch-retention.lock")
	if info, statErr := os.Lstat(lockPath); statErr == nil && info.Mode()&os.ModeSymlink != 0 {
		return errScratchRootUnsafe
	}
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	if info, statErr := f.Stat(); statErr != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() < 0 || !ownedSingleLink(info) {
		return errScratchRootUnsafe
	}
	deadline := time.Now().Add(scratchRetentionLockTimeout)
	for {
		err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			break
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) {
			return err
		}
		if time.Now().After(deadline) {
			return tuskerError("SCRATCH_RETENTION_LOCK_TIMEOUT", fmt.Sprintf("timed out waiting for scratch retention lock in %s", root))
		}
		time.Sleep(25 * time.Millisecond)
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	return fn()
}

func ownedSingleLink(info os.FileInfo) bool {
	st, ok := info.Sys().(*syscall.Stat_t)
	return ok && st.Uid == uint32(os.Getuid()) && st.Nlink == 1
}

// withTuskerScratchWrite coordinates a write/move only when its destination
// is inside this vault's canonical scratch tree. Callers outside dispatch use
// this wrapper; dispatch already holds withScratchRetentionLock.
func withTuskerScratchWrite(vaultPath, target string, fn func() error) error {
	root, err := filepath.Abs(strings.TrimSpace(vaultPath))
	if err != nil {
		return err
	}
	targetAbs, err := filepath.Abs(target)
	if err != nil {
		return err
	}
	for _, scratch := range []string{filepath.Join(root, "scratch"), filepath.Join(root, ".tusker", "scratch")} {
		rel, relErr := filepath.Rel(scratch, targetAbs)
		if relErr == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel) {
			return withScratchRetentionLock(vaultPath, fn)
		}
	}
	return fn()
}

func secureScratchWriteText(vaultPath, target, contents string) error {
	return withTuskerScratchWrite(vaultPath, target, func() error {
		return secureScratchWriteTextUnlocked(vaultPath, target, contents)
	})
}

func secureScratchWriteTextUnlocked(vaultPath, target, contents string) error {
	parentFD, base, inside, err := secureScratchParent(vaultPath, target)
	if err != nil {
		return err
	}
	if !inside {
		return writeText(target, contents)
	}
	defer unix.Close(parentFD)
	tmp := fmt.Sprintf(".%s.tmp-%d-%d", base, os.Getpid(), time.Now().UnixNano())
	fd, err := unix.Openat(parentFD, tmp, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(fd), tmp)
	_, writeErr := file.WriteString(contents)
	if writeErr == nil {
		writeErr = file.Sync()
	}
	closeErr := file.Close()
	if writeErr == nil {
		writeErr = closeErr
	}
	if writeErr != nil {
		_ = unix.Unlinkat(parentFD, tmp, 0)
		return writeErr
	}
	if err := unix.Renameat(parentFD, tmp, parentFD, base); err != nil {
		_ = unix.Unlinkat(parentFD, tmp, 0)
		return err
	}
	return unix.Fsync(parentFD)
}

func secureScratchMove(vaultPath, source, target string) error {
	return withTuskerScratchWrite(vaultPath, target, func() error {
		targetFD, base, inside, err := secureScratchParent(vaultPath, target)
		if err != nil {
			return err
		}
		if !inside {
			return os.Rename(source, target)
		}
		defer unix.Close(targetFD)
		sourceParent, err := filepath.EvalSymlinks(filepath.Dir(source))
		if err != nil {
			return err
		}
		sourceFD, err := unix.Open(sourceParent, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if err != nil {
			return err
		}
		defer unix.Close(sourceFD)
		var sourceStat, targetStat unix.Stat_t
		if err := unix.Fstatat(sourceFD, filepath.Base(source), &sourceStat, unix.AT_SYMLINK_NOFOLLOW); err != nil || sourceStat.Mode&unix.S_IFMT != unix.S_IFREG || sourceStat.Uid != uint32(os.Getuid()) || sourceStat.Nlink != 1 || sourceStat.Mode&0077 != 0 {
			return errScratchRootUnsafe
		}
		if err := unix.Fstatat(targetFD, base, &targetStat, unix.AT_SYMLINK_NOFOLLOW); err == nil {
			return os.ErrExist
		} else if !errors.Is(err, unix.ENOENT) {
			return err
		}
		if err := unix.Renameat(sourceFD, filepath.Base(source), targetFD, base); err != nil {
			return err
		}
		if err := unix.Fstatat(targetFD, base, &targetStat, unix.AT_SYMLINK_NOFOLLOW); err != nil || targetStat.Mode&unix.S_IFMT != unix.S_IFREG || targetStat.Uid != uint32(os.Getuid()) || targetStat.Nlink != 1 || targetStat.Mode&0077 != 0 {
			return errScratchRootUnsafe
		}
		if err := unix.Fsync(targetFD); err != nil {
			return err
		}
		return unix.Fsync(sourceFD)
	})
}

func openSecureScratchAppendUnlocked(vaultPath, target string) (*os.File, error) {
	parentFD, base, inside, err := secureScratchParent(vaultPath, target)
	if err != nil {
		return nil, err
	}
	if !inside {
		return os.OpenFile(target, os.O_CREATE|os.O_APPEND|os.O_WRONLY|syscall.O_NOFOLLOW, 0600)
	}
	defer unix.Close(parentFD)
	fd, err := unix.Openat(parentFD, base, unix.O_CREAT|unix.O_APPEND|unix.O_WRONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if err != nil {
		return nil, err
	}
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil || st.Mode&unix.S_IFMT != unix.S_IFREG || st.Uid != uint32(os.Getuid()) || st.Nlink != 1 || st.Mode&0077 != 0 {
		unix.Close(fd)
		return nil, errScratchRootUnsafe
	}
	return os.NewFile(uintptr(fd), target), nil
}

func secureScratchParent(vaultPath, target string) (int, string, bool, error) {
	vaultAbs, err := filepath.Abs(strings.TrimSpace(vaultPath))
	if err != nil {
		return -1, "", false, err
	}
	targetAbs, err := filepath.Abs(target)
	if err != nil {
		return -1, "", false, err
	}
	vaultPhysical, err := filepath.EvalSymlinks(vaultAbs)
	if err != nil {
		return -1, "", false, err
	}
	for _, rootParts := range [][]string{{"scratch"}, {".tusker", "scratch"}} {
		root := filepath.Join(append([]string{vaultAbs}, rootParts...)...)
		rel, relErr := filepath.Rel(root, targetAbs)
		if relErr != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		fd, err := unix.Open(vaultPhysical, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if err != nil {
			return -1, "", true, err
		}
		parts := append(rootParts, strings.Split(rel, string(filepath.Separator))...)
		for _, part := range parts[:len(parts)-1] {
			var st unix.Stat_t
			err = unix.Fstatat(fd, part, &st, unix.AT_SYMLINK_NOFOLLOW)
			if errors.Is(err, unix.ENOENT) {
				err = unix.Mkdirat(fd, part, 0700)
			}
			if err != nil {
				unix.Close(fd)
				return -1, "", true, err
			}
			if err = unix.Fstatat(fd, part, &st, unix.AT_SYMLINK_NOFOLLOW); err != nil || st.Mode&unix.S_IFMT != unix.S_IFDIR || st.Uid != uint32(os.Getuid()) || st.Mode&0022 != 0 {
				unix.Close(fd)
				return -1, "", true, errScratchRootUnsafe
			}
			next, openErr := unix.Openat(fd, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
			unix.Close(fd)
			if openErr != nil {
				return -1, "", true, openErr
			}
			fd = next
		}
		return fd, parts[len(parts)-1], true, nil
	}
	return -1, "", false, nil
}

// removeScratchChild deletes exactly one direct child of a validated scratch
// root. It re-checks the root immediately before removing so a root swapped
// after resolveScratchRoot cannot redirect the delete.
func removeScratchChild(root, name string) error {
	name = strings.TrimSpace(name)
	if root == "" || name == "" || name == "." || name == ".." {
		return errNotScratchChild
	}
	if name != filepath.Base(name) || strings.ContainsRune(name, filepath.Separator) || strings.Contains(name, "/") {
		return errNotScratchChild
	}
	target := filepath.Join(root, name)
	rel, err := filepath.Rel(root, target)
	if err != nil || rel != name {
		return errNotScratchChild
	}
	rootFD, err := unix.Open(root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		if errors.Is(err, unix.ENOENT) {
			return nil
		}
		return err
	}
	defer unix.Close(rootFD)
	return unlinkScratchEntryAt(rootFD, name)
}

func unlinkScratchEntryAt(parentFD int, name string) error {
	var st unix.Stat_t
	if err := unix.Fstatat(parentFD, name, &st, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		if errors.Is(err, unix.ENOENT) {
			return nil
		}
		return err
	}
	if st.Mode&unix.S_IFMT != unix.S_IFDIR {
		return unix.Unlinkat(parentFD, name, 0)
	}
	childFD, err := unix.Openat(parentFD, name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	f := os.NewFile(uintptr(childFD), name)
	entries, readErr := f.ReadDir(-1)
	if readErr != nil {
		_ = f.Close()
		return readErr
	}
	for _, entry := range entries {
		if err := unlinkScratchEntryAt(childFD, entry.Name()); err != nil {
			_ = f.Close()
			return err
		}
	}
	if err := f.Close(); err != nil {
		return err
	}
	return unix.Unlinkat(parentFD, name, unix.AT_REMOVEDIR)
}

// walkScratchEntry measures one top-level entry. Symlinks beneath the entry are
// not followed by WalkDir, so their targets are neither sized nor deleted.
func walkScratchEntry(root, name string) (scratchEntry, error) {
	entry := scratchEntry{Name: name, Path: filepath.Join(root, name)}
	err := filepath.WalkDir(entry.Path, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !d.IsDir() {
			entry.Bytes += info.Size()
		}
		if info.ModTime().After(entry.Newest) {
			entry.Newest = info.ModTime()
		}
		return nil
	})
	return entry, err
}

// scanScratchEntries lists top-level entries under a validated scratch root.
// Returns an empty slice when scratch does not exist. A scan that cannot be
// completed is an error rather than a partial result: deleting from an
// incomplete scan would trade observability for risk.
func scanScratchEntries(vaultPath string) ([]scratchEntry, error) {
	root, err := resolveScratchRoot(vaultPath)
	if err != nil {
		return nil, err
	}
	items, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return []scratchEntry{}, nil
		}
		return nil, err
	}
	entries := make([]scratchEntry, 0, len(items))
	for _, item := range items {
		entry, err := walkScratchEntry(root, item.Name())
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func totalScratchBytes(entries []scratchEntry) int64 {
	var total int64
	for _, entry := range entries {
		total += entry.Bytes
	}
	return total
}

// staleScratchEntries selects entries whose newest content predates the cutoff.
func staleScratchEntries(entries []scratchEntry, cutoff time.Time) []scratchEntry {
	stale := make([]scratchEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.Newest.Before(cutoff) {
			stale = append(stale, entry)
		}
	}
	return stale
}

// planScratchGC returns entries whose newest content is older than ttl.
func planScratchGC(vaultPath string, ttl time.Duration, now time.Time) ([]scratchEntry, error) {
	entries, err := scanScratchEntries(vaultPath)
	if err != nil {
		return nil, err
	}
	return staleScratchEntries(entries, now.Add(-ttl)), nil
}

// applyScratchGC removes planned entries, re-measuring each one immediately
// before deletion and skipping any that became active after planning. It stops
// at the first failure but still reports everything it completed.
func applyScratchGC(vaultPath string, entries []scratchEntry, cutoff time.Time) (scratchGCOutcome, error) {
	var outcome scratchGCOutcome
	err := withScratchRetentionLock(vaultPath, func() error {
		var innerErr error
		outcome, innerErr = applyScratchGCUnlocked(vaultPath, entries, cutoff)
		return innerErr
	})
	return outcome, err
}

func applyScratchGCUnlocked(vaultPath string, entries []scratchEntry, cutoff time.Time) (scratchGCOutcome, error) {
	var outcome scratchGCOutcome
	root, err := resolveScratchRoot(vaultPath)
	if err != nil {
		return outcome, err
	}
	store, storeErr := OpenRuntimeStore(DefaultStateRoot())
	if storeErr != nil {
		return outcome, storeErr
	}
	defer store.Close()
	projectID, projectOK, projectErr := registeredProjectIDForVault(store, vaultPath)
	if projectErr != nil {
		return outcome, projectErr
	}
	for _, entry := range entries {
		current, err := walkScratchEntry(root, entry.Name)
		if err != nil {
			if os.IsNotExist(err) {
				continue // already gone; nothing to report
			}
			outcome.Failed = entry.Path
			return outcome, err
		}
		if !current.Newest.Before(cutoff) {
			outcome.Skipped = append(outcome.Skipped, current)
			continue
		}
		live, checkErr := scratchEntryHasLiveRunStore(store, projectID, projectOK, entry.Name)
		if checkErr != nil {
			outcome.Failed = entry.Path
			return outcome, checkErr
		}
		if live {
			outcome.Skipped = append(outcome.Skipped, current)
			continue
		}
		if err := removeScratchChild(root, entry.Name); err != nil {
			outcome.Failed = entry.Path
			return outcome, err
		}
		outcome.Deleted = append(outcome.Deleted, current)
		outcome.Reclaimed += current.Bytes
	}
	return outcome, nil
}

func scratchEntryHasLiveRunStore(store *RuntimeStore, projectID string, projectOK bool, name string) (bool, error) {
	if !projectOK {
		return false, nil
	}
	if !v7TaskIDPattern.MatchString(strings.TrimSpace(name)) {
		return false, nil
	}
	run, err := store.FindRunScoped(projectID, name)
	if err != nil || run == nil {
		return false, err
	}
	return runProcessGroupAlive(*run), nil
}

func scratchEntryHasLiveRun(vaultPath, name string) (bool, error) {
	if !v7TaskIDPattern.MatchString(strings.TrimSpace(name)) {
		return false, nil
	}
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		return false, err
	}
	defer store.Close()
	projectID, ok, err := registeredProjectIDForVault(store, vaultPath)
	if err != nil || !ok {
		return false, err
	}
	run, err := store.FindRunScoped(projectID, name)
	if err != nil {
		return false, err
	}
	if run == nil {
		return false, nil
	}
	return runProcessGroupAlive(*run), nil
}

// reapTaskScratch removes <vault>/scratch/<taskID>. It is a no-op when the vault
// does not authorize deletion, when the ID is not a canonical task ID, or when
// the task has link-only evidence pointing into that directory.
func reapTaskScratch(vaultPath, taskID string) error {
	taskID = strings.TrimSpace(taskID)
	if !v7TaskIDPattern.MatchString(taskID) {
		// A non-canonical ID must never reach a recursive delete, whatever the
		// caller believed it was.
		return errNotScratchChild
	}
	root, err := resolveScratchRoot(vaultPath)
	if err != nil {
		if errors.Is(err, errNotTuskerVault) {
			return nil
		}
		return err
	}
	return withScratchRetentionLock(vaultPath, func() error {
		if taskHasLinkOnlyScratchEvidence(vaultPath, taskID) {
			return nil
		}
		return removeScratchChild(root, taskID)
	})
}

// taskHasLinkOnlyScratchEvidence reports whether any evidence record for the
// task is link-only with a recorded path inside scratch/<TASK-ID>/. It fails
// open (keeps the scratch) on anything it cannot read or interpret: a false
// keep only retains ephemeral data, while a false delete destroys the only copy
// of referenced evidence.
func taskHasLinkOnlyScratchEvidence(vaultPath, taskID string) bool {
	dir := filepath.Join(vaultPath, "evidence", taskID)
	items, err := os.ReadDir(dir)
	if err != nil {
		return !os.IsNotExist(err)
	}
	for _, item := range items {
		if item.IsDir() || !strings.HasSuffix(item.Name(), ".md") {
			continue
		}
		data, _, err := parseFrontmatterMustRead(filepath.Join(dir, item.Name()))
		if err != nil {
			return true
		}
		if stringField(data, "artifact_durability") != "link_only" {
			continue
		}
		for _, recorded := range normalizeList(data["artifact_paths"]) {
			if scratchPathRefersToTask(recorded, taskID) {
				return true
			}
		}
	}
	return false
}

// scratchPathRefersToTask reports whether a recorded evidence path lands inside
// scratch/<taskID>/. It compares whole path components after normalizing
// separators and cleaning the path, so "scratch/./T/x" and "scratch/a/../T/x"
// match while "notscratch/T/x" does not.
func scratchPathRefersToTask(recorded, taskID string) bool {
	value := strings.TrimSpace(recorded)
	if value == "" {
		return false
	}
	if idx := strings.Index(strings.ToLower(value), "link-only:"); idx == 0 {
		value = value[len("link-only:"):]
	}
	// Normalize both separator forms regardless of host OS: a path recorded on
	// Windows must still be understood here.
	value = strings.ReplaceAll(value, "\\", "/")
	value = path.Clean(value)
	parts := strings.Split(value, "/")
	for i := 0; i+1 < len(parts); i++ {
		if strings.EqualFold(parts[i], "scratch") && strings.EqualFold(parts[i+1], taskID) {
			return true
		}
	}
	return false
}
