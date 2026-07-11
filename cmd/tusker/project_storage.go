package main

import (
	"path/filepath"
	"strings"
)

func validateProjectStorageBoundary(repoRoot, vaultRoot string) error {
	repoRoot = strings.TrimSpace(repoRoot)
	vaultRoot = strings.TrimSpace(vaultRoot)
	if repoRoot == "" || vaultRoot == "" {
		return tuskerError(errorConfigInvalid, "project registration requires repo_root and vault_root")
	}
	repoAbs, err := filepath.Abs(repoRoot)
	if err != nil {
		return err
	}
	vaultAbs, err := filepath.Abs(vaultRoot)
	if err != nil {
		return err
	}
	if !pathWithinLexical(repoAbs, vaultAbs) || !pathWithinResolved(repoAbs, vaultAbs) {
		return tuskerError(errorConfigInvalid,
			"project vault must live inside the registered repository",
			withPath(vaultRoot),
			withHint("move the Tusker vault to <repo>/.tusker (recommended) or another directory inside the repository, then register the project again"),
			withContext(map[string]any{"repo_root": repoAbs, "vault_root": vaultAbs}))
	}
	return nil
}

func pathWithinResolved(root, target string) bool {
	resolvedRoot := resolvePathWithMissingTail(root)
	resolvedTarget := resolvePathWithMissingTail(target)
	if resolvedRoot == "" || resolvedTarget == "" {
		return false
	}
	return pathWithinLexical(resolvedRoot, resolvedTarget)
}

func pathWithinLexical(root, target string) bool {
	root = filepath.Clean(root)
	target = filepath.Clean(target)
	relative, err := filepath.Rel(root, target)
	if err != nil || filepath.IsAbs(relative) {
		return false
	}
	return relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)))
}
