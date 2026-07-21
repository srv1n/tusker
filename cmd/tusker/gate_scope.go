package main

import (
	"path/filepath"
	"sort"
	"strings"
)

// Per-change gate scoping, mechanized from
// .tusker/specs/build-and-test-economics.md (Stage 1 — per-change gate). The
// per-change stage must build, style, and test only the areas a change actually
// touched; the wave-end collective gate (runGateTier over the full harvest set)
// stays whole. The mapping from a touched path to the commands that cover it is
// project configuration — a GateScope names a set of path globs and the harvest
// commands for that area — so no per-language runner knowledge lives in the
// binary. A change that touches an area runs that area's scope; areas the change
// did not touch are skipped.

// GateScope maps an area of the project to the harvest commands that cover it.
// Paths are globs (filepath.Match) or directory prefixes; a scope is selected
// when the change touched any path it owns.
type GateScope struct {
	Name     string   `yaml:"name,omitempty" json:"name,omitempty"`
	Paths    []string `yaml:"paths,omitempty" json:"paths,omitempty"`
	Commands []string `yaml:"commands,omitempty" json:"commands,omitempty"`
}

// scopeOwnsPath reports whether one of the scope's path patterns covers the
// touched path. A pattern matches when it equals the path, is a directory
// prefix of it, or matches it as a filepath glob (tried against the whole path
// and against each trailing segment so "*.go" covers "pkg/foo/bar.go").
func scopeOwnsPath(scope GateScope, touched string) bool {
	touched = strings.TrimSpace(filepath.ToSlash(touched))
	if touched == "" {
		return false
	}
	for _, raw := range scope.Paths {
		pattern := strings.TrimSpace(filepath.ToSlash(raw))
		if pattern == "" {
			continue
		}
		trimmed := strings.TrimSuffix(pattern, "/")
		if touched == trimmed || strings.HasPrefix(touched, trimmed+"/") {
			return true
		}
		if ok, _ := filepath.Match(pattern, touched); ok {
			return true
		}
		segments := strings.Split(touched, "/")
		for i := range segments {
			if ok, _ := filepath.Match(pattern, strings.Join(segments[i:], "/")); ok {
				return true
			}
		}
	}
	return false
}

// selectScopedCommands returns, for the areas the change touched, the union of
// their harvest commands (order-preserving, de-duplicated) and the names of the
// scopes that were selected. Scopes whose paths the change did not touch are
// left out entirely — that is how the per-change gate skips untouched areas.
func selectScopedCommands(scopes []GateScope, touched []string) (commands []string, selected []string) {
	seenCommand := map[string]bool{}
	for _, scope := range scopes {
		matched := false
		for _, path := range touched {
			if scopeOwnsPath(scope, path) {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}
		selected = append(selected, scope.Name)
		for _, command := range scope.Commands {
			command = strings.TrimSpace(command)
			if command == "" || seenCommand[command] {
				continue
			}
			seenCommand[command] = true
			commands = append(commands, command)
		}
	}
	return commands, selected
}

// runSelectiveGateTier is the Stage 1 per-change gate. It asks the runtime which
// paths the change touched (diff against the base branch), selects the scopes
// that own those paths, and harvests only their commands behind the same
// preflight and one-pass defect harvest as the full gate. Areas the change did
// not touch never run. When the change touched nothing a scope owns, the gate
// passes without running any command — there is nothing in scope to check. The
// wave-end collective gate stays whole: callers keep using runGateTier over the
// project's full harvest set for that stage.
func runSelectiveGateTier(policy GateTierPolicy, scopes []GateScope, base, requestedProfile string, rt gateTierRuntime) (GateTierResult, error) {
	started := rt.now()
	if rt.DiffPaths == nil {
		return GateTierResult{Schema: gateTierSchema, Mode: gateTierModeSelective},
			tuskerError(errorInvalidArg, "selective gate runtime is missing a diff-paths boundary")
	}
	touched, err := rt.DiffPaths(rt.Workspace, base)
	if err != nil {
		return GateTierResult{Schema: gateTierSchema, Mode: gateTierModeSelective},
			tuskerError("DIFF_PATHS_FAILED", "cannot determine which paths the change touched: "+err.Error())
	}

	commands, selected := selectScopedCommands(scopes, touched)
	if len(commands) == 0 {
		// The change touched no area any scope owns: nothing to build, style, or
		// test at the per-change stage. That is a pass, not a refusal.
		return GateTierResult{
			Schema:     gateTierSchema,
			Mode:       gateTierModeSelective,
			Outcome:    gateOutcomePassed,
			Profile:    strings.TrimSpace(firstNonEmpty(requestedProfile, policy.Profile)),
			Touched:    touched,
			Commands:   nil,
			DurationMS: rt.now().Sub(started).Milliseconds(),
		}, nil
	}

	scoped := policy
	scoped.HarvestCommands = commands
	result, runErr := runGateTier(scoped, requestedProfile, rt)
	result.Mode = gateTierModeSelective
	result.Touched = touched
	result.Scopes = selected
	// Preserve the selected scope commands as the run's declared command set even
	// when a ledger short-circuit or refusal replaced Ran.
	result.Commands = commands
	if result.DurationMS == 0 {
		result.DurationMS = rt.now().Sub(started).Milliseconds()
	}
	return result, runErr
}

// changedGatePaths is the default DiffPaths boundary: the set of files a change
// touched relative to the base branch — committed changes since the merge-base
// plus anything still uncommitted (staged, unstaged, or untracked) in the tree.
func changedGatePaths(workspace, base string) ([]string, error) {
	base = strings.TrimSpace(base)
	if base == "" {
		base = resolveDefaultBranch(workspace, "")
	}
	paths := map[string]bool{}
	collect := func(out string) {
		for _, line := range strings.Split(out, "\n") {
			line = strings.TrimSpace(line)
			if line != "" {
				paths[filepath.ToSlash(line)] = true
			}
		}
	}
	if base != "" {
		if mergeBase, err := gitFactOutput(workspace, "merge-base", base, "HEAD"); err == nil && mergeBase != "" {
			if out, err := gitFactOutput(workspace, "diff", "--name-only", mergeBase, "--"); err == nil {
				collect(out)
			}
		}
	}
	// Uncommitted work the change carries but has not committed yet.
	if out, err := gitFactOutput(workspace, "diff", "--name-only", "HEAD", "--"); err == nil {
		collect(out)
	}
	if out, err := gitFactOutput(workspace, "ls-files", "--others", "--exclude-standard"); err == nil {
		collect(out)
	}
	ordered := make([]string, 0, len(paths))
	for path := range paths {
		ordered = append(ordered, path)
	}
	sort.Strings(ordered)
	return ordered, nil
}
