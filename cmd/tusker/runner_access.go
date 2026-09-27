package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
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
		protected: []string{filepath.Join(home, ".ssh"), filepath.Join(home, ".aws"), filepath.Join(home, ".gnupg"), filepath.Join(home, "Library", "Keychains")},
		state:     []string{filepath.Join(home, ".config", "tusker"), filepath.Dir(userGlobalTuskerConfigPath()), filepath.Join(home, "Library", "Application Support", "tusker")},
	}
	stateRoot := DefaultStateRoot()
	if !pathWithinResolved(stateRoot, worktree) {
		paths.state = append(paths.state, stateRoot)
	}
	// ponytail: an overlapping custom state root cannot be denied wholesale;
	// split its metadata from workspaces if this layout becomes supported.
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

func wrapRunnerAccessArgv(argv []string, paths runnerAccessPaths) []string {
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
