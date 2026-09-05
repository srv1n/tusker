package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func resetCmd(args Args) error {
	if args.Bool("help") {
		printResetHelp()
		return nil
	}
	repo := strings.TrimSpace(args.String("repo"))
	if repo == "" {
		repo = "."
	}
	repoRoot, err := filepath.Abs(repo)
	if err != nil {
		return err
	}
	if !dirExists(repoRoot) {
		return tuskerError(errorNotFound, "project directory does not exist", withPath(repoRoot))
	}
	if !resetLooksLikeProject(repoRoot) {
		return tuskerError(errorInvalidArg, "reset target is not a Tusker project", withPath(repoRoot), withHint("run reset from a project containing .git or .tusker"))
	}

	if args.Bool("dry-run") {
		actions, err := planTuskerPurge(repoRoot)
		if err != nil {
			return err
		}
		if args.Bool("json") {
			emitJSON(map[string]any{
				"ok": true, "repo": repoRoot, "dry_run": true,
				"preserved": []string{filepath.Join(repoRoot, defaultRepoVaultDir, "specs")},
				"actions":   actions,
			})
			return nil
		}
		fmt.Printf("Tusker reset dry-run for %s (%d actions). Re-run with --yes to apply.\n", repoRoot, len(actions))
		fmt.Printf("- preserve: %s/specs\n", filepath.Join(repoRoot, defaultRepoVaultDir))
		for _, action := range actions {
			fmt.Printf("- %s: %s (%s)\n", action.Kind, action.Path, action.Reason)
		}
		return nil
	}
	if !args.Bool("yes") {
		return tuskerError(errorInvalidArg, "reset is destructive; pass --yes to delete Tusker state and relaunch the project")
	}

	previousDir, err := os.Getwd()
	if err != nil {
		return err
	}
	if previousDir != repoRoot {
		if err := os.Chdir(repoRoot); err != nil {
			return err
		}
		defer os.Chdir(previousDir)
	}

	initArgs := copyArgsForInternalMutation(args)
	initArgs["purge-state"] = "true"
	initArgs["preserve-specs"] = "true"
	initArgs["yes"] = "true"
	initArgs["vault-only"] = "true"
	initArgs["no-mount"] = "true"
	initArgs["no-pointers"] = "true"
	initArgs["no-contract"] = "true"
	return initCmd(initArgs)
}

func resetLooksLikeProject(repoRoot string) bool {
	return fileExists(filepath.Join(repoRoot, ".git")) ||
		dirExists(filepath.Join(repoRoot, ".git")) ||
		dirExists(filepath.Join(repoRoot, defaultRepoVaultDir))
}

func printResetHelp() {
	fmt.Println(`Usage:
  tusker reset --yes [--repo <path>]
  tusker reset --dry-run [--repo <path>] [--json]
  tusker relaunch --yes [--repo <path>]

Purpose:
  Delete known repo-local Tusker state and initialize a clean V7 vault.
  Documentation specs in .tusker/specs/** are preserved; source files and
  docs outside Tusker state are untouched.

Behavior:
  - --dry-run previews the deletion plan without changing anything
  - without --yes, the command refuses to delete anything
  - repo pointers, repo-contract files, and Obsidian mounts are not recreated
  - the relaunch alias is kept for the “delete and relaunch” workflow`)
}
