package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// v7FullGateStateRoot is the authority for provider-owned state. Absolute
// pathnames are retained only for durable references and diagnostics; every
// state mutation and read is resolved through the opened root descriptor.
// os.Root prevents ".." and symlink traversal from escaping the opened tree,
// and the descriptor continues to name the original tree if its pathname is
// replaced while the daemon is running.
type v7FullGateStateRoot struct {
	path string
	root *os.Root
}

func openV7FullGateStateRoot(path string) (*v7FullGateStateRoot, error) {
	clean, err := filepath.Abs(filepath.Clean(strings.TrimSpace(path)))
	if err != nil || strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("%w: daemon state root is unavailable", errV7FullGateProvider)
	}
	before, err := os.Lstat(clean)
	if err != nil || !before.IsDir() || before.Mode()&os.ModeSymlink != 0 || before.Mode()&0o022 != 0 {
		return nil, fmt.Errorf("%w: daemon state root must be a non-group/world-writable directory", errV7FullGateProvider)
	}
	root, err := os.OpenRoot(clean)
	if err != nil {
		return nil, fmt.Errorf("%w: open daemon state root: %v", errV7FullGateProvider, err)
	}
	opened, openErr := root.Stat(".")
	after, pathErr := os.Lstat(clean)
	if openErr != nil || pathErr != nil || !os.SameFile(before, opened) || !os.SameFile(opened, after) {
		_ = root.Close()
		return nil, fmt.Errorf("%w: daemon state root identity changed while opening", errV7FullGateProvider)
	}
	return &v7FullGateStateRoot{path: clean, root: root}, nil
}

func (state *v7FullGateStateRoot) Close() error {
	if state == nil || state.root == nil {
		return nil
	}
	err := state.root.Close()
	state.root = nil
	return err
}

func (state *v7FullGateStateRoot) sameIdentity(other *v7FullGateStateRoot) bool {
	if state == nil || state.root == nil || other == nil || other.root == nil {
		return false
	}
	left, leftErr := state.root.Stat(".")
	right, rightErr := other.root.Stat(".")
	return leftErr == nil && rightErr == nil && os.SameFile(left, right)
}

func (state *v7FullGateStateRoot) relative(path string) (string, error) {
	if state == nil || state.root == nil {
		return "", fmt.Errorf("%w: daemon state root handle is unavailable", errV7FullGateProvider)
	}
	if !filepath.IsAbs(path) {
		clean := filepath.Clean(path)
		if clean == "." || clean == "" || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			return "", fmt.Errorf("%w: invalid state-root-relative path %q", errV7FullGateProvider, path)
		}
		return clean, nil
	}
	clean, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(state.path, clean)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%w: path escapes daemon state root", errV7FullGateProvider)
	}
	return rel, nil
}

func (state *v7FullGateStateRoot) absolute(rel string) (string, error) {
	rel, err := state.relative(rel)
	if err != nil {
		return "", err
	}
	return filepath.Join(state.path, rel), nil
}

func (state *v7FullGateStateRoot) lstat(rel string) (os.FileInfo, error) {
	rel, err := state.relative(rel)
	if err != nil {
		return nil, err
	}
	return state.root.Lstat(rel)
}

func (state *v7FullGateStateRoot) open(rel string) (*os.File, error) {
	rel, err := state.relative(rel)
	if err != nil {
		return nil, err
	}
	return state.root.Open(rel)
}

func (state *v7FullGateStateRoot) openRegular(rel string, max int64, executable bool) (*os.File, os.FileInfo, error) {
	rel, err := state.relative(rel)
	if err != nil {
		return nil, nil, err
	}
	before, err := state.root.Lstat(rel)
	if err != nil {
		return nil, nil, err
	}
	if !before.Mode().IsRegular() || before.Mode()&os.ModeSymlink != 0 || before.Mode()&0o022 != 0 || executable && before.Mode()&0o111 == 0 {
		return nil, nil, fmt.Errorf("%w: state file must be a non-group/world-writable regular file", errV7FullGateProvider)
	}
	if before.Size() < 0 || before.Size() > max {
		return nil, nil, fmt.Errorf("%w: state file exceeds %d-byte bound", errV7FullGateProvider, max)
	}
	file, err := state.root.Open(rel)
	if err != nil {
		return nil, nil, err
	}
	opened, err := file.Stat()
	if err != nil || !os.SameFile(before, opened) || opened.Size() != before.Size() {
		_ = file.Close()
		return nil, nil, fmt.Errorf("%w: state file identity changed while opening", errV7FullGateProvider)
	}
	return file, opened, nil
}

func (state *v7FullGateStateRoot) readRegular(rel string, max int64, executable bool) ([]byte, error) {
	file, opened, err := state.openRegular(rel, max, executable)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, max+1))
	if err != nil || int64(len(raw)) != opened.Size() || int64(len(raw)) > max {
		return nil, fmt.Errorf("%w: state file changed or exceeded its bound while reading", errV7FullGateProvider)
	}
	after, err := file.Stat()
	if err != nil || !os.SameFile(opened, after) || after.Size() != opened.Size() {
		return nil, fmt.Errorf("%w: state file identity changed while reading", errV7FullGateProvider)
	}
	return raw, nil
}

// overwriteRegular updates the exact regular-file inode already rooted in
// state. It opens without O_TRUNC, checks both the rooted pathname and any
// caller-pinned identity, and only then truncates the verified descriptor.
func (state *v7FullGateStateRoot) overwriteRegular(rel string, raw []byte, max int64, expected os.FileInfo) error {
	if int64(len(raw)) > max {
		return fmt.Errorf("%w: state file exceeds %d-byte bound", errV7FullGateProvider, max)
	}
	rel, err := state.relative(rel)
	if err != nil {
		return err
	}
	pinned, opened, err := state.openRegular(rel, max, false)
	if err != nil {
		return err
	}
	defer pinned.Close()
	if expected != nil && !os.SameFile(expected, opened) {
		return fmt.Errorf("%w: state file no longer matches its pinned identity", errV7FullGateProvider)
	}
	writer, err := state.root.OpenFile(rel, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	writerInfo, err := writer.Stat()
	if err != nil || !os.SameFile(opened, writerInfo) || expected != nil && !os.SameFile(expected, writerInfo) {
		_ = writer.Close()
		return fmt.Errorf("%w: state file identity changed while opening for update", errV7FullGateProvider)
	}
	if err := writer.Truncate(0); err == nil {
		var written int
		written, err = writer.Write(raw)
		if err == nil && written != len(raw) {
			err = io.ErrShortWrite
		}
	}
	if err == nil {
		err = writer.Sync()
	}
	after, statErr := writer.Stat()
	closeErr := writer.Close()
	pathAfter, pathErr := state.root.Lstat(rel)
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if statErr != nil || pathErr != nil || !os.SameFile(writerInfo, after) || !os.SameFile(after, pathAfter) || after.Size() != int64(len(raw)) {
		return fmt.Errorf("%w: state file identity changed while updating", errV7FullGateProvider)
	}
	return state.syncDir(filepath.Dir(rel))
}

func (state *v7FullGateStateRoot) readDir(rel string) ([]os.DirEntry, error) {
	rel, err := state.relative(rel)
	if err != nil {
		return nil, err
	}
	before, err := state.root.Lstat(rel)
	if err != nil {
		return nil, err
	}
	if !before.IsDir() || before.Mode()&os.ModeSymlink != 0 || before.Mode()&0o022 != 0 {
		return nil, fmt.Errorf("%w: state directory is invalid", errV7FullGateProvider)
	}
	dir, err := state.root.Open(rel)
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	opened, err := dir.Stat()
	if err != nil || !os.SameFile(before, opened) {
		return nil, fmt.Errorf("%w: state directory identity changed while opening", errV7FullGateProvider)
	}
	entries, err := dir.ReadDir(-1)
	if err != nil {
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	return entries, nil
}

func (state *v7FullGateStateRoot) syncDir(rel string) error {
	dir, err := state.open(rel)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func (state *v7FullGateStateRoot) syncRoot() error {
	dir, err := state.root.Open(".")
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func (state *v7FullGateStateRoot) ensureDir(rel string, mode os.FileMode) (bool, error) {
	rel, err := state.relative(rel)
	if err != nil {
		return false, err
	}
	parts := strings.Split(rel, string(filepath.Separator))
	current := ""
	created := false
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return false, fmt.Errorf("%w: invalid durable directory path", errV7FullGateProvider)
		}
		parent := current
		if current == "" {
			current = part
		} else {
			current = filepath.Join(current, part)
		}
		info, statErr := state.root.Lstat(current)
		if statErr == nil {
			if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode()&0o022 != 0 {
				return false, fmt.Errorf("%w: durable directory path is not a directory", errV7FullGateProvider)
			}
			continue
		}
		if !errors.Is(statErr, os.ErrNotExist) {
			return false, statErr
		}
		if err := state.root.Mkdir(current, mode); err != nil {
			return false, err
		}
		if err := state.syncDir(current); err != nil {
			return false, err
		}
		if parent == "" {
			if err := state.syncRoot(); err != nil {
				return false, err
			}
		} else if err := state.syncDir(parent); err != nil {
			return false, err
		}
		created = true
	}
	return created, nil
}

func (state *v7FullGateStateRoot) writeDurable(rel string, raw []byte, mode os.FileMode, replace bool) error {
	rel, err := state.relative(rel)
	if err != nil {
		return err
	}
	parent := filepath.Dir(rel)
	if parent == "." {
		return fmt.Errorf("%w: state file requires a rooted parent directory", errV7FullGateProvider)
	}
	tmp := rel + ".tmp-" + strings.ToLower(newRecordID())
	file, err := state.root.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	if _, err = file.Write(raw); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		_ = state.root.Remove(tmp)
		return err
	}
	if closeErr != nil {
		_ = state.root.Remove(tmp)
		return closeErr
	}
	if replace {
		err = state.root.Rename(tmp, rel)
	} else {
		err = state.root.Link(tmp, rel)
		if err == nil {
			err = state.root.Remove(tmp)
		}
	}
	if err != nil {
		_ = state.root.Remove(tmp)
		return err
	}
	return state.syncDir(parent)
}

func (state *v7FullGateStateRoot) writeJSON(rel string, raw []byte, replace bool) error {
	payload := append(append([]byte(nil), raw...), '\n')
	return state.writeDurable(rel, payload, 0o600, replace)
}

func (state *v7FullGateStateRoot) remove(rel string) error {
	rel, err := state.relative(rel)
	if err != nil {
		return err
	}
	return state.root.Remove(rel)
}

func (state *v7FullGateStateRoot) removeAll(rel string) error {
	rel, err := state.relative(rel)
	if err != nil {
		return err
	}
	return state.root.RemoveAll(rel)
}

func (state *v7FullGateStateRoot) rename(oldRel, newRel string) error {
	oldRel, err := state.relative(oldRel)
	if err != nil {
		return err
	}
	newRel, err = state.relative(newRel)
	if err != nil {
		return err
	}
	return state.root.Rename(oldRel, newRel)
}
