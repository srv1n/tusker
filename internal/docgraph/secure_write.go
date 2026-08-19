package docgraph

import (
	"fmt"
	"path/filepath"
)

// WriteDocumentFile updates one repository-relative document without following
// a symlinked root, parent, or leaf. The descriptor is identity-checked before
// truncation and again after writing so a path swap cannot redirect the write.
func WriteDocumentFile(repoRoot, relative string, data []byte) error {
	root, err := openDocsMapRoot(repoRoot)
	if err != nil {
		return err
	}
	defer root.Close()
	if err := validateDocsMapArtifact(root, relative); err != nil {
		return err
	}
	clean := filepath.Clean(filepath.FromSlash(relative))
	if parent := filepath.Dir(clean); parent != "." {
		if err := root.MkdirAll(parent, 0o755); err != nil {
			return err
		}
	}
	return writeDocsMapArtifact(root, relative, data)
}

// ReadDocumentFile reads one repository-relative document without following a
// symlinked root, parent, or leaf.
func ReadDocumentFile(repoRoot, relative string) ([]byte, error) {
	root, err := openDocsMapRoot(repoRoot)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	clean := filepath.Clean(filepath.FromSlash(relative))
	if err := rejectDocsMapSymlinkPath(root, clean); err != nil {
		return nil, fmt.Errorf("documentation file path is symlinked: %s: %w", relative, err)
	}
	return root.ReadFile(clean)
}

// RemoveDocumentFile removes one repository-relative regular document without
// following a symlinked root, parent, or leaf.
func RemoveDocumentFile(repoRoot, relative string) error {
	root, err := openDocsMapRoot(repoRoot)
	if err != nil {
		return err
	}
	defer root.Close()
	clean := filepath.Clean(filepath.FromSlash(relative))
	if err := validateDocsMapArtifact(root, clean); err != nil {
		return err
	}
	return root.Remove(clean)
}
