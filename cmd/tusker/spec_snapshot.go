package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

type tuskerSpecSnapshot struct {
	root string
}

func snapshotTuskerSpecs(repoRoot string) (*tuskerSpecSnapshot, error) {
	source := filepath.Join(repoRoot, defaultRepoVaultDir, "specs")
	info, err := os.Lstat(source)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return nil, fmt.Errorf("cannot preserve Tusker specs: %s is not a real directory", source)
	}
	tempRoot, err := os.MkdirTemp("", "tusker-specs-")
	if err != nil {
		return nil, err
	}
	target := filepath.Join(tempRoot, "specs")
	if err := copyTuskerSpecTree(source, target); err != nil {
		_ = os.RemoveAll(tempRoot)
		return nil, err
	}
	return &tuskerSpecSnapshot{root: tempRoot}, nil
}

func (s *tuskerSpecSnapshot) restore(repoRoot string) error {
	if s == nil {
		return nil
	}
	source := filepath.Join(s.root, "specs")
	target := filepath.Join(repoRoot, defaultRepoVaultDir, "specs")
	return copyTuskerSpecTree(source, target)
}

func (s *tuskerSpecSnapshot) cleanup() {
	if s != nil && s.root != "" {
		_ = os.RemoveAll(s.root)
	}
}

func copyTuskerSpecTree(source, target string) error {
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("cannot preserve symlinked Tusker spec path: %s", path)
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		destination := target
		if relative != "." {
			destination = filepath.Join(target, relative)
		}
		if entry.IsDir() {
			return os.MkdirAll(destination, 0o755)
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
			return err
		}
		return os.WriteFile(destination, body, 0o644)
	})
}
