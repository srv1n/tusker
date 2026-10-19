package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func releaseRunRemedy(taskID string) string {
	return "tusker runs retire " + taskID + " --release --break-glass --by human:<name> --reason 'release abandoned session'"
}

func ownershipConflictReason(conflict map[string]any) string {
	return fmt.Sprintf("an active run holds %s for %s (owner %s; lease age %s; liveness %s); next: `%s`", conflict["holder_path"], conflict["task_id"], conflict["holder"], conflict["lease_age"], conflict["liveness"], releaseRunRemedy(stringValue(conflict["task_id"])))
}

func doneRefusalRemedy(vault, taskID, actor, reason string) string {
	if strings.Contains(reason, "; next: `") {
		return reason
	}
	command := "tusker show " + taskID + " --why"
	switch {
	case strings.Contains(reason, "live run holds"):
		command = releaseRunRemedy(taskID)
	case strings.Contains(reason, "scope path is unreadable or missing"):
		paths := []string{"<current-owned-paths>"}
		if task, err := resolveV7Note(vault, taskID, "task"); err == nil {
			paths = normalizeList(task.Data["owned_paths"])
			for i, path := range paths {
				// Suggest the common Rust file-to-module refactor, without silently
				// widening the authored contract or changing ownership boundaries.
				if strings.HasSuffix(path, ".rs") {
					dir := strings.TrimSuffix(path, ".rs")
					if info, err := os.Stat(filepath.Join(v7RepoRoot(vault), dir)); err == nil && info.IsDir() {
						paths[i] = dir
					}
				}
			}
		}
		command = "tusker edit " + taskID + " --owned-paths " + shellSingleQuote(strings.Join(paths, ",")) + " --by " + shellSingleQuote(actor)
	case strings.Contains(reason, "no valid Result cell"):
		command = "tusker edit " + taskID + " --body-file <corrected-body.md> --by " + shellSingleQuote(actor)
	case strings.Contains(reason, "state_rev"):
		command = "tusker check --fix"
	}
	return reason + "; next: `" + command + "`"
}
