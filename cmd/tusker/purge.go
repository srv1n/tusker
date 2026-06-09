package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type tuskerPurgeAction struct {
	Kind   string `json:"kind"`
	Path   string `json:"path"`
	Reason string `json:"reason"`
	Target string `json:"target,omitempty"`
}

func tuskerPurgeCmd(args Args) error {
	if args.Bool("help") {
		printPurgeHelp()
		return nil
	}
	if !args.Bool("only-tusker-state") {
		return tuskerError(errorInvalidArg, "purge requires --only-tusker-state", withHint("the purge command intentionally refuses broad deletion; it only removes known Tusker state"))
	}
	repo := strings.TrimSpace(args.String("repo"))
	if repo == "" {
		repo = "."
	}
	repoRoot, err := filepath.Abs(repo)
	if err != nil {
		return err
	}
	actions, err := planTuskerPurge(repoRoot)
	if err != nil {
		return err
	}
	apply := args.Bool("yes") || args.Bool("write") || args.Bool("apply")
	if apply {
		if err := applyTuskerPurge(repoRoot, actions); err != nil {
			return err
		}
	}
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": true, "repo": repoRoot, "dry_run": !apply, "count": len(actions), "actions": actions})
		return nil
	}
	if args.Bool("quiet") {
		return nil
	}
	if !apply {
		fmt.Printf("Tusker purge dry-run for %s (%d actions). Re-run with --yes to apply.\n", repoRoot, len(actions))
	} else {
		fmt.Printf("Purged Tusker state for %s (%d actions).\n", repoRoot, len(actions))
	}
	for _, action := range actions {
		target := ""
		if action.Target != "" {
			target = " -> " + action.Target
		}
		fmt.Printf("- %s: %s%s (%s)\n", action.Kind, action.Path, target, action.Reason)
	}
	return nil
}

func planTuskerPurge(repoRoot string) ([]tuskerPurgeAction, error) {
	var actions []tuskerPurgeAction
	addRemove := func(path, reason string) {
		if path == "" {
			return
		}
		if _, err := os.Lstat(path); err == nil {
			actions = append(actions, tuskerPurgeAction{Kind: "remove_path", Path: path, Reason: reason})
		}
	}
	addRemove(filepath.Join(repoRoot, defaultRepoVaultDir), "repo-local Tusker V7 vault")
	addRemove(filepath.Join(repoRoot, ".tusker-local"), "repo-local Tusker runtime scratch")
	for _, rel := range []string{
		filepath.Join(".agents", "skills", currentSkillInstallDir),
		filepath.Join(".claude", "skills", currentSkillInstallDir),
		filepath.Join(".codex", "skills", currentSkillInstallDir),
	} {
		addRemove(filepath.Join(repoRoot, rel), "repo-local generated Tusker skill install")
	}
	for _, rel := range []string{"AGENTS.md", "CLAUDE.md"} {
		path := filepath.Join(repoRoot, rel)
		if text, err := readText(path); err == nil && strings.Contains(text, tuskerPointerBegin) && strings.Contains(text, tuskerPointerEnd) {
			actions = append(actions, tuskerPurgeAction{Kind: "remove_pointer_block", Path: path, Reason: "managed Tusker bootstrap block"})
		}
	}
	if err := filepath.WalkDir(repoRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == repoRoot {
			return nil
		}
		name := d.Name()
		if d.IsDir() && (name == ".git" || name == "node_modules" || name == "target" || name == "artifacts" || name == ".cache") {
			return filepath.SkipDir
		}
		if d.IsDir() && name == defaultRepoVaultDir && path != filepath.Join(repoRoot, defaultRepoVaultDir) {
			actions = append(actions, tuskerPurgeAction{Kind: "remove_path", Path: path, Reason: "nested/app-local Tusker vault"})
			return filepath.SkipDir
		}
		if d.IsDir() && name == "tusker" && path == filepath.Join(repoRoot, "tusker") && looksLikeTuskerStateDir(path) {
			actions = append(actions, tuskerPurgeAction{Kind: "remove_path", Path: path, Reason: "legacy root Tusker tracker"})
			return filepath.SkipDir
		}
		return nil
	}); err != nil {
		return nil, err
	}
	if cfg, err := loadWorkspaceVaultConfig(); err == nil {
		for _, project := range cfg.Projects {
			if !workspaceProjectMatchesRepo(project, repoRoot) {
				continue
			}
			if info, err := os.Lstat(project.MountPath); err == nil && info.Mode()&os.ModeSymlink != 0 {
				target, _ := os.Readlink(project.MountPath)
				resolved := resolveSymlinkTarget(project.MountPath, target)
				actions = append(actions, tuskerPurgeAction{Kind: "remove_mount_symlink", Path: project.MountPath, Target: resolved, Reason: "workspace vault mount for purged repo"})
			}
			actions = append(actions, tuskerPurgeAction{Kind: "remove_workspace_project", Path: workspaceConfigPath(), Reason: "workspace config entry for purged repo", Target: project.ProjectID})
		}
	}
	return dedupePurgeActions(actions), nil
}

func applyTuskerPurge(repoRoot string, actions []tuskerPurgeAction) error {
	for _, action := range actions {
		switch action.Kind {
		case "remove_path", "remove_mount_symlink":
			if err := os.RemoveAll(action.Path); err != nil {
				return err
			}
		case "remove_pointer_block":
			changed, err := removeTuskerPointer(action.Path)
			if err != nil {
				return err
			}
			if !changed {
				continue
			}
		case "remove_workspace_project":
			if err := removeWorkspaceProjectsForRepo(repoRoot); err != nil {
				return err
			}
		}
	}
	return nil
}

func removeTuskerPointer(filePath string) (bool, error) {
	current, err := readText(filePath)
	if err != nil {
		return false, err
	}
	begin := strings.Index(current, tuskerPointerBegin)
	end := strings.Index(current, tuskerPointerEnd)
	if begin == -1 || end == -1 || end < begin {
		return false, nil
	}
	next := current[:begin] + current[end+len(tuskerPointerEnd):]
	next = strings.TrimRight(next, " \t\r\n") + "\n"
	if strings.TrimSpace(next) == "" {
		return true, os.Remove(filePath)
	}
	if next == current {
		return false, nil
	}
	return true, writeText(filePath, next)
}

func removeWorkspaceProjectsForRepo(repoRoot string) error {
	cfg, err := loadWorkspaceVaultConfig()
	if err != nil {
		return err
	}
	var kept []WorkspaceProject
	for _, project := range cfg.Projects {
		if workspaceProjectMatchesRepo(project, repoRoot) {
			continue
		}
		kept = append(kept, project)
	}
	cfg.Projects = kept
	return saveWorkspaceVaultConfig(cfg)
}

func workspaceProjectMatchesRepo(project WorkspaceProject, repoRoot string) bool {
	if project.RepoRoot != "" && canonicalPath(project.RepoRoot) == canonicalPath(repoRoot) {
		return true
	}
	for _, candidate := range []string{project.TrackerRoot, project.MountPath} {
		if candidate != "" && isWithinPath(candidate, repoRoot) {
			return true
		}
	}
	return false
}

func looksLikeTuskerStateDir(path string) bool {
	for _, rel := range []string{"SKILL.md", "WORKFLOW.md", "work", "knowledge", "events"} {
		if fileExists(filepath.Join(path, rel)) {
			return true
		}
	}
	return false
}

func resolveSymlinkTarget(path, target string) string {
	if target == "" {
		return ""
	}
	if filepath.IsAbs(target) {
		return target
	}
	return filepath.Clean(filepath.Join(filepath.Dir(path), target))
}

func isWithinPath(path, root string) bool {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(absRoot, absPath)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

func dedupePurgeActions(actions []tuskerPurgeAction) []tuskerPurgeAction {
	seen := map[string]bool{}
	var out []tuskerPurgeAction
	for _, action := range actions {
		key := action.Kind + "\x00" + action.Path + "\x00" + action.Target
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, action)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].Path < out[j].Path
	})
	return out
}

func printPurgeHelp() {
	fmt.Println(`Usage:
  tusker purge --repo <path> --only-tusker-state [--yes] [--json]

Purpose:
  Show or apply a safe deletion plan for Tusker-generated repo state. The
  command refuses broad deletion and only touches known Tusker state: .tusker,
  nested app-local .tusker vaults, repo-local Tusker skill installs, managed
  AGENTS.md/CLAUDE.md blocks, and matching workspace vault mounts.

Behavior:
  - default is dry-run
  - pass --yes to apply
  - product source files are never removed

Examples:
  tusker purge --repo . --only-tusker-state
  tusker purge --repo . --only-tusker-state --yes`)
}
