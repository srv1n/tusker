package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
)

type runnerAccessPaths struct {
	protected []string
	state     []string
}

var accessNonDarwinLog sync.Once
var accessNativeSandboxLog sync.Once

func effectiveRunnerDenyPaths(worktree string) (runnerAccessPaths, error) {
	resolved, err := resolveTuskerConfigForPaths("", "", false)
	if err != nil {
		return runnerAccessPaths{}, err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return runnerAccessPaths{}, err
	}
	paths := runnerAccessPaths{
		// Keychain files expose item labels, accounts and services in clear
		// text, so agents may not read them. Harness logins must not live there.
		protected: []string{filepath.Join(home, ".ssh"), filepath.Join(home, ".aws"), filepath.Join(home, ".gnupg"), filepath.Join(home, "Library", "Keychains"), "/Library/Keychains"},
		state:     []string{filepath.Join(home, ".config", "tusker"), filepath.Dir(userGlobalTuskerConfigPath())},
	}
	for _, path := range resolved.Config.Access.ProtectedPaths {
		if path == "~" {
			path = home
		} else if strings.HasPrefix(path, "~/") {
			path = filepath.Join(home, path[2:])
		}
		if !filepath.IsAbs(path) {
			return runnerAccessPaths{}, tuskerError(errorConfigInvalid, "access.protected_paths entries must be absolute or start with ~/ ")
		}
		paths.protected = append(paths.protected, filepath.Clean(path))
	}
	paths.protected = uniqueAccessPaths(paths.protected)
	paths.state = uniqueAccessPaths(paths.state)
	for _, path := range append(append([]string{}, paths.protected...), paths.state...) {
		if pathWithinResolved(path, worktree) || pathWithinResolved(worktree, path) {
			return runnerAccessPaths{}, tuskerError(errorConfigInvalid, "runner worktree overlaps protected path: "+path)
		}
	}
	return paths, nil
}

func uniqueAccessPaths(paths []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		path = filepath.Clean(path)
		if !seen[path] {
			seen[path] = true
			out = append(out, path)
		}
	}
	sort.Strings(out)
	return out
}

func sandboxExecProfile(paths runnerAccessPaths) string {
	var b strings.Builder
	b.WriteString("(version 1)\n(allow default)\n")
	// Seatbelt matches the physical path: /var and /tmp resolve through /private.
	for _, path := range paths.protected {
		fmt.Fprintf(&b, "(deny file-read* file-write* (subpath %s))\n", strconv.Quote(canonicalPath(path)))
	}
	for _, path := range paths.state {
		fmt.Fprintf(&b, "(deny file-write* (subpath %s))\n", strconv.Quote(canonicalPath(path)))
	}
	return b.String()
}

func wrapRunnerAccessArgv(argv []string, paths runnerAccessPaths, policy CodexPolicy) []string {
	if strings.TrimSpace(firstNonEmpty(policy.TurnSandboxPolicy, policy.ThreadSandbox)) != "danger-full-access" {
		accessNativeSandboxLog.Do(func() { log.Print("runner access: harness sandbox active; skipping sandbox-exec wrapper") })
		return argv
	}
	if runtime.GOOS != "darwin" {
		accessNonDarwinLog.Do(func() { log.Print("runner access sandbox-exec is unavailable outside darwin") })
		return argv
	}
	return append([]string{"/usr/bin/sandbox-exec", "-p", sandboxExecProfile(paths)}, argv...)
}

func claudeAccessDenyRules(paths runnerAccessPaths) []string {
	var rules []string
	for _, path := range paths.protected {
		for _, tool := range []string{"Read", "Edit"} {
			rules = append(rules, tool+"(/"+path+"/**)")
		}
	}
	for _, path := range paths.state {
		for _, tool := range []string{"Edit"} {
			rules = append(rules, tool+"(/"+path+"/**)")
		}
	}
	return append(rules, "Bash(git push --force*)", "Bash(git push -f*)", "Bash(git reset --hard*)", "Bash(git push * --delete*)", "Bash(rm -rf /*)")
}

func addClaudeAccessSettings(path string, paths runnerAccessPaths) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var settings map[string]any
	if err := json.Unmarshal(data, &settings); err != nil {
		return err
	}
	permissions, _ := settings["permissions"].(map[string]any)
	if permissions == nil {
		permissions = map[string]any{}
		settings["permissions"] = permissions
	}
	permissions["deny"] = claudeAccessDenyRules(paths)
	data, err = json.Marshal(settings)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

// claudeReviewAllowRules lets a read-only (plan mode) Claude reviewer run the
// commands a review needs: plan mode denies every unlisted Bash call, and
// `tusker review` is the only way to submit a verdict.
func claudeReviewAllowRules(notePath string) []string {
	rules := []string{
		"Bash(tusker review *)", "Bash(tusker show *)",
		"Bash(git diff*)", "Bash(git log*)", "Bash(git show*)", "Bash(git status*)",
		"Bash(grep *)", "Bash(rg *)", "Bash(head *)", "Bash(tail *)", "Bash(wc *)", "Bash(ls*)",
	}
	_, body, err := parseFrontmatterMustRead(notePath)
	if err != nil {
		return rules
	}
	for _, row := range parseV7VerificationRows(body) {
		command, ok := strings.CutPrefix(strings.TrimSpace(row.Check), "command:")
		if command = strings.TrimSpace(command); ok && command != "" {
			rules = append(rules, "Bash("+command+")")
		}
	}
	// A PreToolUse hook such as `rtk hook claude` rewrites `git status` to
	// `rtk git status` before the permission check, so mirror each rule.
	for _, rule := range slices.Clone(rules) {
		rules = append(rules, "Bash(rtk "+strings.TrimPrefix(rule, "Bash("))
	}
	return rules
}

func addClaudeAllowRules(path string, rules []string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var settings map[string]any
	if err := json.Unmarshal(data, &settings); err != nil {
		return err
	}
	permissions, _ := settings["permissions"].(map[string]any)
	if permissions == nil {
		permissions = map[string]any{}
		settings["permissions"] = permissions
	}
	existing, _ := permissions["allow"].([]any)
	for _, rule := range rules {
		existing = append(existing, rule)
	}
	permissions["allow"] = existing
	data, err = json.Marshal(settings)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}
