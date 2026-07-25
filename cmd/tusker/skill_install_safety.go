package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const skillBundleMarker = ".tusker-skill-bundle"

func replaceSkillInstallDestination(destination string, populate func(string) error) error {
	abs, err := filepath.Abs(destination)
	if err != nil {
		return err
	}
	boundary, err := skillInstallBoundary(abs)
	if err != nil {
		return err
	}
	return replaceOwnedFilesystemEntry(abs, boundary, true, populate)
}

func skillInstallBoundary(destination string) (string, error) {
	for _, rootName := range []string{".agents", ".claude", ".codex"} {
		suffix := filepath.Join(rootName, "skills", currentSkillInstallDir)
		if destination == suffix || !strings.HasSuffix(destination, string(filepath.Separator)+suffix) {
			continue
		}
		boundary := strings.TrimSuffix(destination, string(filepath.Separator)+suffix)
		if boundary == "" {
			boundary = string(filepath.Separator)
		}
		return boundary, nil
	}
	return nearestExistingSkillInstallBoundary(filepath.Dir(destination))
}

func nearestExistingSkillInstallBoundary(path string) (string, error) {
	for {
		info, err := os.Lstat(path)
		switch {
		case err == nil:
			if path == string(filepath.Separator) {
				return "", fmt.Errorf("refusing filesystem mutation with root as the managed boundary")
			}
			if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
				return "", fmt.Errorf("managed boundary must be a real directory: %s", path)
			}
			return path, nil
		case !os.IsNotExist(err):
			return "", fmt.Errorf("inspect managed boundary %s: %w", path, err)
		}
		parent := filepath.Dir(path)
		if parent == path {
			return "", fmt.Errorf("refusing filesystem mutation without a non-root managed boundary")
		}
		path = parent
	}
}

func replaceOwnedFilesystemEntry(destination, boundary string, allowDestinationSymlink bool, populate func(string) error) error {
	parent := filepath.Dir(destination)
	if err := ensureSafeDirectoryChain(boundary, parent); err != nil {
		return err
	}
	lock := filepath.Join(parent, ".tusker-"+filepath.Base(destination)+"-replace.lock")
	if err := os.Mkdir(lock, 0o700); err != nil {
		return fmt.Errorf("lock managed destination %s: %w", destination, err)
	}
	defer os.Remove(lock)

	if err := validateReplaceableDestination(destination, allowDestinationSymlink); err != nil {
		return err
	}
	stageRoot, err := os.MkdirTemp(parent, ".tusker-stage-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stageRoot)
	stage := filepath.Join(stageRoot, "next")
	if err := populate(stage); err != nil {
		return err
	}

	backup := ""
	if _, err := os.Lstat(destination); err == nil {
		backup, err = unusedSiblingPath(parent, ".tusker-backup-")
		if err != nil {
			return err
		}
		if err := os.Rename(destination, backup); err != nil {
			return fmt.Errorf("move managed destination to local backup: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.Rename(stage, destination); err != nil {
		if backup != "" {
			_ = os.Rename(backup, destination)
		}
		return fmt.Errorf("activate managed destination: %w", err)
	}
	if backup != "" {
		if err := removeOwnedBackup(backup); err != nil {
			return fmt.Errorf("remove replaced managed backup %s: %w", backup, err)
		}
	}
	return nil
}

func removeOwnedBackup(path string) error {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return os.Remove(path)
	}
	return os.RemoveAll(path)
}

func unusedSiblingPath(parent, pattern string) (string, error) {
	path, err := os.MkdirTemp(parent, pattern)
	if err != nil {
		return "", err
	}
	if err := os.Remove(path); err != nil {
		return "", err
	}
	return path, nil
}

func validateReplaceableDestination(destination string, allowSymlink bool) error {
	info, err := os.Lstat(destination)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		if allowSymlink {
			return nil
		}
		return fmt.Errorf("managed destination must not be a symlink: %s", destination)
	}
	if !info.IsDir() {
		return fmt.Errorf("managed destination must be a directory or owned skill symlink: %s", destination)
	}
	return nil
}

func ensureSafeDirectoryChain(boundary, target string) error {
	boundary, err := filepath.Abs(boundary)
	if err != nil {
		return err
	}
	target, err = filepath.Abs(target)
	if err != nil {
		return err
	}
	if boundary == string(filepath.Separator) {
		return fmt.Errorf("refusing filesystem mutation with root as the managed boundary")
	}
	rel, err := filepath.Rel(boundary, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("managed destination %s escapes boundary %s", target, boundary)
	}
	info, err := os.Lstat(boundary)
	if err != nil {
		return fmt.Errorf("inspect managed boundary %s: %w", boundary, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("managed boundary must be a real directory: %s", boundary)
	}
	current := boundary
	if rel == "." {
		return nil
	}
	for _, component := range strings.Split(rel, string(filepath.Separator)) {
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			if err := os.Mkdir(current, 0o755); err != nil {
				return err
			}
			continue
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("managed destination ancestor must be a real directory: %s", current)
		}
	}
	return nil
}

func bundleOutputBoundary(repoRoot, out string) (string, error) {
	repoRoot, err := filepath.Abs(repoRoot)
	if err != nil {
		return "", err
	}
	out, err = filepath.Abs(out)
	if err != nil {
		return "", err
	}
	if out == string(filepath.Separator) || sameCleanPath(out, repoRoot) {
		return "", fmt.Errorf("refusing bundle output at filesystem or repository root: %s", out)
	}
	if rel, relErr := filepath.Rel(out, repoRoot); relErr == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("refusing bundle output that is an ancestor of the repository: %s", out)
	}
	if rel, relErr := filepath.Rel(repoRoot, out); relErr == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return repoRoot, nil
	}
	return filepath.Dir(out), nil
}
