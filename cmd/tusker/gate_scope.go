package main

import (
	"fmt"
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

// unscopedTouchedPaths returns the touched paths that no configured scope owns.
// These are coverage gaps: at the per-change stage there is no scoped command
// that would verify them, so the selective gate must not silently narrow them
// away — it falls back to the full harvest set instead.
func unscopedTouchedPaths(scopes []GateScope, touched []string) []string {
	var unscoped []string
	for _, path := range touched {
		if strings.TrimSpace(path) == "" {
			continue
		}
		owned := false
		for _, scope := range scopes {
			if scopeOwnsPath(scope, path) {
				owned = true
				break
			}
		}
		if !owned {
			unscoped = append(unscoped, path)
		}
	}
	return unscoped
}

// runSelectiveGateTier is the Stage 1 per-change gate. It asks the runtime which
// paths the change touched (diff against the base branch), selects the scopes
// that own those paths, and harvests only their commands behind the same
// preflight and one-pass defect harvest as the full gate. Areas the change did
// not touch never run. The gate fails closed: if the change set cannot be
// computed it REFUSES rather than passes, and a touched path that no scope owns
// falls back to the project's full harvest set rather than being skipped. Only a
// truly empty diff (nothing touched at all) passes without running a command,
// and even that pass runs preflight and stamps the tree hash. The wave-end
// collective gate stays whole: callers keep using runGateTier over the project's
// full harvest set for that stage.
func runSelectiveGateTier(policy GateTierPolicy, scopes []GateScope, base, requestedProfile string, rt gateTierRuntime) (GateTierResult, error) {
	started := rt.now()
	profile := strings.TrimSpace(firstNonEmpty(requestedProfile, policy.Profile))
	if rt.DiffPaths == nil {
		return GateTierResult{Schema: gateTierSchema, Mode: gateTierModeSelective, Profile: profile},
			tuskerError(errorInvalidArg, "selective gate runtime is missing a diff-paths boundary")
	}
	touched, err := rt.DiffPaths(rt.Workspace, base)
	if err != nil {
		// Fail closed: an unavailable change set is a refusal, never a pass on a
		// narrowed or empty diff.
		return GateTierResult{
			Schema:  gateTierSchema,
			Mode:    gateTierModeSelective,
			Profile: profile,
			Outcome: gateOutcomeRefused,
			Refusal: &GateRefusal{
				Cause:  gateRefusalDiffUnavailable,
				Detail: "cannot determine which paths the change touched: " + err.Error(),
				Remedy: "make the base branch and merge-base resolvable (fetch the base branch or pass --base) so the change set is computable; the gate refuses rather than gating on a narrowed or empty diff",
			},
			DurationMS: rt.now().Sub(started).Milliseconds(),
		}, nil
	}

	commands, selected := selectScopedCommands(scopes, touched)

	// Fail closed on coverage gaps: any touched path that no scope owns is
	// unaccounted for at the per-change stage. Fall back to the full harvest set
	// rather than pass on an unverified area.
	if unscoped := unscopedTouchedPaths(scopes, touched); len(unscoped) > 0 {
		result, runErr := runGateTier(policy, requestedProfile, rt)
		result.Mode = gateTierModeSelective
		result.Touched = touched
		result.Scopes = selected
		result.Fallback = fmt.Sprintf(
			"%d touched path(s) own no configured scope; ran the full harvest set instead of narrowing: %s",
			len(unscoped), strings.Join(unscoped, ", "))
		if result.DurationMS == 0 {
			result.DurationMS = rt.now().Sub(started).Milliseconds()
		}
		return result, runErr
	}

	if len(commands) == 0 {
		// A truly empty diff: nothing was touched (or nothing maps to a command).
		// Still stamp the tree and run preflight so even a pass is attributable to
		// one revision and honors the gate's preconditions.
		if rt.TreeHash == nil {
			return GateTierResult{Schema: gateTierSchema, Mode: gateTierModeSelective, Profile: profile},
				tuskerError(errorInvalidArg, "selective gate runtime is missing a tree hash boundary")
		}
		treeHash, hashErr := rt.TreeHash(rt.Workspace)
		if hashErr != nil {
			return GateTierResult{Schema: gateTierSchema, Mode: gateTierModeSelective, Profile: profile},
				tuskerError("TREE_HASH_FAILED", "cannot hash the gate tree state: "+hashErr.Error())
		}
		result := GateTierResult{
			Schema:   gateTierSchema,
			Mode:     gateTierModeSelective,
			Profile:  profile,
			TreeHash: treeHash,
			Touched:  touched,
		}
		if rt.Toolchain == nil {
			return result, tuskerError(errorInvalidArg, "selective gate runtime is missing a toolchain fingerprint boundary")
		}
		result.Toolchain = strings.TrimSpace(rt.Toolchain(rt.Workspace, policy.HarvestCommands))
		if refusal := gateTierPreflight(policy, requestedProfile, rt); refusal != nil {
			result.Outcome = gateOutcomeRefused
			result.Refusal = refusal
			result.DurationMS = rt.now().Sub(started).Milliseconds()
			return result, nil
		}
		result.Outcome = gateOutcomePassed
		result.DurationMS = rt.now().Sub(started).Milliseconds()
		return result, nil
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
	// The committed delta since the merge-base is load-bearing: without it,
	// committed changes vanish from the diff and the selective gate would pass on
	// nothing. Its unavailability (no resolvable base, merge-base error, or diff
	// error) is a hard refusal, never a silent narrowing.
	if base == "" {
		return nil, tuskerError("GATE_DIFF_UNAVAILABLE",
			"cannot resolve a default branch to diff committed changes against; refusing rather than gating on a narrowed change set")
	}
	mergeBase, err := gitFactOutput(workspace, "merge-base", base, "HEAD")
	if err != nil || mergeBase == "" {
		detail := "merge-base against " + base + " is empty"
		if err != nil {
			detail = err.Error()
		}
		return nil, tuskerError("GATE_DIFF_UNAVAILABLE",
			"cannot resolve the committed-change base for the selective gate: "+detail)
	}
	if out, err := gitFactOutput(workspace, "diff", "--name-only", mergeBase, "--"); err == nil {
		collect(out)
	} else {
		return nil, tuskerError("GATE_DIFF_UNAVAILABLE",
			"committed-delta diff against the merge-base failed: "+err.Error())
	}
	// Uncommitted work the change carries but has not committed yet. These queries
	// are additive; the committed delta above already guarantees at least one
	// successful query, so a failure here does not blind the gate.
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
