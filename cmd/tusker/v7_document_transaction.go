package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type v7DocumentWritePreimage struct {
	Content []byte
	Mode    os.FileMode
	Existed bool
}

func v7Fingerprint(raw []byte) string {
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func ensureV7WorkNamespaces(vaultPath string) error {
	for _, dir := range []string{"work/tasks", "work/waves", "work/gates", "work/decisions", "work/evidence", "work/events"} {
		if err := ensureDir(filepath.Join(vaultPath, filepath.FromSlash(dir))); err != nil {
			return err
		}
	}
	return nil
}

func convergeUnchangedV7DocumentWrites(writes map[string]string) error {
	for path, next := range writes {
		current, err := readText(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if current == next {
			delete(writes, path)
			continue
		}
		currentData, currentBody, currentErr := parseFrontmatter(current)
		nextData, nextBody, nextErr := parseFrontmatter(next)
		if currentErr != nil || nextErr != nil {
			continue
		}
		for _, field := range []string{"updated_at", "updated_by", "state_rev"} {
			delete(currentData, field)
			delete(nextData, field)
		}
		currentCanonical, err := yaml.Marshal(currentData)
		if err != nil {
			return err
		}
		nextCanonical, err := yaml.Marshal(nextData)
		if err != nil {
			return err
		}
		if bytes.Equal(currentCanonical, nextCanonical) && strings.TrimSpace(currentBody) == strings.TrimSpace(nextBody) {
			delete(writes, path)
		}
	}
	return nil
}

// commitV7DocumentWrites is the document transaction boundary. Callers hold
// the material lock; this function captures every actual write preimage,
// atomically replaces one complete document at a time, and verifies bytes
// after every replacement. A failure restores and byte-verifies the whole set
// before any cache invalidation or mutation notification escapes.
func commitV7DocumentWrites(writes map[string]string, failAfter int) error {
	return commitV7DocumentWritesWithLocks(writes, failAfter, nil)
}

// commitV7DocumentWritesWithLocks is the narrow transaction entry point for
// callers that already hold some document locks under the V7 material epoch.
// Those exact identities are not reacquired: flock is not recursively owned
// across separate file descriptors on every supported platform. Write paths
// not covered by a live caller lock retain the normal sorted acquisition.
func commitV7DocumentWritesWithLocks(writes map[string]string, failAfter int, heldLocks []*v7DocumentLock) error {
	paths := make([]string, 0, len(writes))
	for path := range writes {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	lockPaths := uniqueStrings(paths)
	sort.Strings(lockPaths)
	heldIdentities := make(map[string]struct{}, len(heldLocks))
	for _, lock := range heldLocks {
		if lock == nil || lock.file == nil || strings.TrimSpace(lock.path) == "" {
			return tuskerError(errorInvalidArg, "guarded document write received an invalid held document lock")
		}
		if _, err := lock.file.Stat(); err != nil {
			return tuskerError(errorInvalidTransition, "guarded document write received a closed held document lock", withPath(lock.path))
		}
		identity, err := v7DocumentLockIdentity(lock.path)
		if err != nil {
			return err
		}
		heldIdentities[identity] = struct{}{}
	}
	var documentLocks []*v7DocumentLock
	for _, path := range lockPaths {
		if !fileExists(path) {
			continue
		}
		identity, err := v7DocumentLockIdentity(path)
		if err != nil {
			return err
		}
		if _, held := heldIdentities[identity]; held {
			continue
		}
		lock, err := acquireV7DocumentLock(path, v7DocumentLockTimeout)
		if err != nil {
			for i := len(documentLocks) - 1; i >= 0; i-- {
				_ = documentLocks[i].Close()
			}
			return err
		}
		documentLocks = append(documentLocks, lock)
	}
	defer func() {
		for i := len(documentLocks) - 1; i >= 0; i-- {
			_ = documentLocks[i].Close()
		}
	}()
	backups := map[string]v7DocumentWritePreimage{}
	for _, path := range paths {
		parentInfo, err := os.Lstat(filepath.Dir(path))
		if err != nil {
			return tuskerError(errorInvalidTransition, "document write directory is unavailable", withPath(filepath.Dir(path)), withContext(map[string]any{"cause": err.Error()}))
		}
		if parentInfo.Mode()&os.ModeSymlink != 0 || !parentInfo.IsDir() {
			return tuskerError(errorInvalidTransition, "document write directory is not a real directory", withPath(filepath.Dir(path)))
		}
		info, err := os.Lstat(path)
		if os.IsNotExist(err) {
			backups[path] = v7DocumentWritePreimage{Mode: 0o644}
			continue
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return tuskerError(errorInvalidTransition, "document write target is not a regular file", withPath(path))
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		backups[path] = v7DocumentWritePreimage{Content: raw, Mode: info.Mode().Perm(), Existed: true}
	}

	var attemptedPaths []string
	rollback := func(cause error) error {
		rollbackErr := restoreV7DocumentWritePreimagesOwned(attemptedPaths, backups, writes)
		for _, path := range attemptedPaths {
			invalidateCachedNote(path)
		}
		if rollbackErr != nil {
			return tuskerError(
				errorInvalidTransition,
				"document transaction failed and exact rollback could not be proven; stop and repair the reported paths before retrying",
				withHint("restore every reported path from version control, then rerun the command"),
				withContext(map[string]any{"cause": cause.Error(), "rollback": rollbackErr.Error(), "paths": paths}),
			)
		}
		return cause
	}
	for i, path := range paths {
		attemptedPaths = append(attemptedPaths, path)
		if err := writeV7DocumentTransactionFileCAS(path, []byte(writes[path]), backups[path]); err != nil {
			return rollback(err)
		}
		written, readErr := os.ReadFile(path)
		if readErr != nil || !bytes.Equal(written, []byte(writes[path])) {
			return rollback(tuskerError(errorInvalidTransition, "document transaction post-write CAS mismatch", withPath(path)))
		}
		if failAfter > 0 && i+1 >= failAfter {
			return rollback(tuskerError(errorInvalidArg, "forced document transaction write failure"))
		}
	}
	for _, path := range paths {
		invalidateCachedNote(path)
		recordCLIVaultMutation(path)
	}
	return nil
}

// restoreV7DocumentWritePreimagesOwned restores only paths this transaction
// attempted and only while the current bytes are either its exact intended
// bytes or already the original preimage. Third-party bytes are preserved and
// reported as an unproven rollback.
func restoreV7DocumentWritePreimagesOwned(paths []string, backups map[string]v7DocumentWritePreimage, intended map[string]string) error {
	var failures []string
	var verifyPaths []string
	for index := len(paths) - 1; index >= 0; index-- {
		path := paths[index]
		backup := backups[path]
		want := []byte(intended[path])
		current, err := os.ReadFile(path)
		if backup.Existed {
			if err == nil && bytes.Equal(current, backup.Content) {
				verifyPaths = append(verifyPaths, path)
				continue
			}
			if err != nil || !bytes.Equal(current, want) {
				failures = append(failures, path+": current bytes are not transaction-owned; preserved")
				continue
			}
			if err := writeV7DocumentTransactionFile(path, backup.Content, backup.Mode); err != nil {
				failures = append(failures, path+": "+err.Error())
			} else {
				verifyPaths = append(verifyPaths, path)
			}
			continue
		}
		if os.IsNotExist(err) {
			verifyPaths = append(verifyPaths, path)
			continue
		}
		if err != nil || !bytes.Equal(current, want) {
			failures = append(failures, path+": current bytes are not transaction-owned; preserved")
			continue
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			failures = append(failures, path+": "+err.Error())
			continue
		}
		if err := syncV7DocumentDirectory(filepath.Dir(path)); err != nil {
			failures = append(failures, path+": sync rollback deletion: "+err.Error())
			continue
		}
		verifyPaths = append(verifyPaths, path)
	}
	for _, path := range verifyPaths {
		backup := backups[path]
		info, err := os.Lstat(path)
		if !backup.Existed {
			if err == nil || !os.IsNotExist(err) {
				failures = append(failures, path+": rollback absence could not be proven")
			}
			continue
		}
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != backup.Mode.Perm() {
			failures = append(failures, path+": restored identity or mode differs")
			continue
		}
		raw, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(raw, backup.Content) {
			failures = append(failures, path+": restored bytes differ")
		}
	}
	if len(failures) > 0 {
		sort.Strings(failures)
		return fmt.Errorf("%s", strings.Join(failures, "; "))
	}
	return nil
}

func writeV7DocumentTransactionFile(path string, content []byte, mode os.FileMode) error {
	return writeV7DocumentTransactionFileUnchecked(path, content, mode)
}

func writeV7DocumentTransactionFileCAS(path string, content []byte, expected v7DocumentWritePreimage) error {
	mode := expected.Mode
	return writeV7DocumentTransactionFilePrepared(path, content, mode, func() error {
		info, err := os.Lstat(path)
		if !expected.Existed {
			if os.IsNotExist(err) {
				return nil
			}
			return tuskerError(errorInvalidTransition, "document transaction expected absent target changed before rename", withPath(path))
		}
		if err != nil || !info.Mode().IsRegular() {
			return tuskerError(errorInvalidTransition, "document transaction target changed before rename", withPath(path))
		}
		raw, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(raw, expected.Content) {
			return tuskerError(errorInvalidTransition, "document transaction preimage changed before rename", withPath(path))
		}
		return nil
	})
}

func writeV7DocumentTransactionFileUnchecked(path string, content []byte, mode os.FileMode) error {
	return writeV7DocumentTransactionFilePrepared(path, content, mode, nil)
}

func writeV7DocumentTransactionFilePrepared(path string, content []byte, mode os.FileMode, beforeRename func() error) error {
	if mode.Perm() == 0 {
		mode = 0o644
	}
	temp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".txn-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	renamed := false
	defer func() {
		_ = temp.Close()
		if !renamed {
			_ = os.Remove(tempPath)
		}
	}()
	if err := temp.Chmod(mode.Perm()); err != nil {
		return err
	}
	if written, err := temp.Write(content); err != nil {
		return err
	} else if written != len(content) {
		return fmt.Errorf("write document transaction temporary file: wrote %d of %d bytes", written, len(content))
	}
	if err := temp.Sync(); err != nil {
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if beforeRename != nil {
		if err := beforeRename(); err != nil {
			return err
		}
	}
	if err := os.Rename(tempPath, path); err != nil {
		return err
	}
	renamed = true
	if err := syncV7DocumentDirectory(filepath.Dir(path)); err != nil {
		return fmt.Errorf("sync document transaction parent directory after rename: %w", err)
	}
	return nil
}
