package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

type gitBranchFacts struct {
	Branch         string  `json:"branch"`
	Head           string  `json:"head"`
	Dirty          bool    `json:"dirty"`
	DefaultBranch  string  `json:"default_branch,omitempty"`
	BranchAgeHours float64 `json:"branch_age_hours"`
	Ahead          int     `json:"ahead"`
	Behind         int     `json:"behind"`
}

func captureGitBranchFacts(workspace, configuredDefault string, now time.Time) (gitBranchFacts, error) {
	workspace = strings.TrimSpace(workspace)
	if workspace == "" {
		return gitBranchFacts{}, fmt.Errorf("workspace path is required")
	}
	if info, err := os.Stat(workspace); err != nil || !info.IsDir() {
		return gitBranchFacts{}, fmt.Errorf("workspace is unavailable: %s", workspace)
	}
	inside, err := gitFactOutput(workspace, "rev-parse", "--is-inside-work-tree")
	if err != nil || inside != "true" {
		return gitBranchFacts{}, fmt.Errorf("workspace is not a git worktree: %s", workspace)
	}
	facts := gitBranchFacts{}
	facts.Branch, _ = gitFactOutput(workspace, "branch", "--show-current")
	facts.Head, err = gitFactOutput(workspace, "rev-parse", "HEAD")
	if err != nil || facts.Head == "" {
		return gitBranchFacts{}, fmt.Errorf("capture HEAD: %w", err)
	}
	status, err := gitFactOutput(workspace, "status", "--porcelain", "--untracked-files=normal")
	if err != nil {
		return gitBranchFacts{}, fmt.Errorf("capture dirty state: %w", err)
	}
	facts.Dirty = status != ""
	facts.DefaultBranch = resolveDefaultBranch(workspace, configuredDefault)
	if facts.DefaultBranch == "" {
		return facts, nil
	}
	counts, err := gitFactOutput(workspace, "rev-list", "--left-right", "--count", facts.DefaultBranch+"...HEAD")
	if err == nil {
		fields := strings.Fields(counts)
		if len(fields) == 2 {
			facts.Behind, _ = strconv.Atoi(fields[0])
			facts.Ahead, _ = strconv.Atoi(fields[1])
		}
	}
	base, err := gitFactOutput(workspace, "merge-base", facts.DefaultBranch, "HEAD")
	if err == nil && base != "" {
		stamp, stampErr := gitFactOutput(workspace, "show", "-s", "--format=%ct", base)
		if stampErr == nil {
			seconds, parseErr := strconv.ParseInt(stamp, 10, 64)
			if parseErr == nil {
				age := now.UTC().Sub(time.Unix(seconds, 0).UTC()).Hours()
				if age > 0 {
					facts.BranchAgeHours = age
				}
			}
		}
	}
	return facts, nil
}

func resolveDefaultBranch(workspace, configured string) string {
	candidates := []string{strings.TrimSpace(configured)}
	if symbolic, err := gitFactOutput(workspace, "symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD"); err == nil {
		candidates = append(candidates, symbolic)
	}
	candidates = append(candidates, "main", "master")
	for _, candidate := range uniqueStrings(candidates) {
		if candidate == "" {
			continue
		}
		if _, err := gitFactOutput(workspace, "rev-parse", "--verify", candidate+"^{commit}"); err == nil {
			return candidate
		}
	}
	return ""
}

func gitFactOutput(workspace string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", workspace}, args...)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(string(out)), nil
}
