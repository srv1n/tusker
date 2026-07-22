package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"time"
)

const (
	// Cold executable launches can spend several seconds in platform security
	// scanning (notably XProtect on macOS). Keep the probe bounded without
	// mistaking that one-time startup cost for a missing runner.
	runnerPreflightTimeout       = 30 * time.Second
	runnerPathPrefixEnv          = "TUSKER_RUNNER_PATH_PREFIX"
	runnerPreflightOutputMaxRune = 300
)

var runnerLoginShellPath = readRunnerLoginShellPath

func runnerBaseEnv() []string {
	return setEnvValue(os.Environ(), "PATH", runnerCommandSearchPath())
}

func runnerCommandSearchPath() string {
	parts := []string{}
	parts = appendPathList(parts, os.Getenv(runnerPathPrefixEnv))
	parts = appendPathList(parts, os.Getenv("PATH"))
	parts = appendPathList(parts, runnerLoginShellPath())
	parts = append(parts, runnerPreferredPathDirs()...)
	parts = append(parts,
		"/opt/homebrew/bin",
		"/usr/local/bin",
		"/usr/bin",
		"/bin",
		"/usr/sbin",
		"/sbin",
	)
	return strings.Join(uniquePathStrings(parts), string(os.PathListSeparator))
}

// readRunnerLoginShellPath mirrors the operator's login shell rather than
// assuming the daemon inherited an interactive terminal PATH. It is best-effort:
// a broken shell profile must never prevent normal PATH resolution.
func readRunnerLoginShellPath() string {
	shell := runnerLoginShell()
	if shell == "" {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, shell, "-lc", `printf %s "$PATH"`).Output()
	if err != nil || ctx.Err() != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

func runnerLoginShell() string {
	if shell := strings.TrimSpace(os.Getenv("SHELL")); isExecutableFile(shell) {
		return shell
	}
	// launchd does not promise SHELL. On macOS, ask Directory Services for the
	// account's actual login shell before falling back to common local shells.
	if current, err := user.Current(); err == nil && strings.TrimSpace(current.Username) != "" {
		if dscl, lookErr := exec.LookPath("dscl"); lookErr == nil {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			output, commandErr := exec.CommandContext(ctx, dscl, ".", "-read", "/Users/"+current.Username, "UserShell").Output()
			timedOut := ctx.Err() != nil
			cancel()
			if commandErr == nil && !timedOut {
				fields := strings.Fields(string(output))
				if len(fields) >= 2 {
					if candidate := fields[len(fields)-1]; isExecutableFile(candidate) {
						return candidate
					}
				}
			}
		}
	}
	for _, candidate := range []string{"/bin/zsh", "/bin/bash", "/bin/sh"} {
		if isExecutableFile(candidate) {
			return candidate
		}
	}
	return ""
}

func runnerPreferredPathDirs() []string {
	return runnerPreferredPathDirsFrom([]string{
		"/Applications/Codex.app/Contents/Resources",
		"/Applications/ChatGPT.app/Contents/Resources",
	})
}

func runnerPreferredPathDirsFrom(candidates []string) []string {
	dirs := make([]string, 0, len(candidates))
	for _, dir := range candidates {
		if isExecutableFile(filepath.Join(dir, "codex")) {
			dirs = append(dirs, dir)
		}
	}
	return dirs
}

func appendPathList(parts []string, raw string) []string {
	for _, part := range filepath.SplitList(strings.TrimSpace(raw)) {
		part = strings.TrimSpace(part)
		if part != "" {
			parts = append(parts, part)
		}
	}
	return parts
}

func uniquePathStrings(values []string) []string {
	seen := map[string]struct{}{}
	out := []string{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		key := value
		if abs, err := filepath.Abs(value); err == nil {
			key = abs
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, value)
	}
	return out
}

func setEnvValue(env []string, key, value string) []string {
	prefix := key + "="
	out := make([]string, 0, len(env)+1)
	replaced := false
	for _, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			if !replaced {
				out = append(out, prefix+value)
				replaced = true
			}
			continue
		}
		out = append(out, entry)
	}
	if !replaced {
		out = append(out, prefix+value)
	}
	return out
}

func runnerCommandPreflightBlocker(runner RunnerName, command string) string {
	_, blocker := runnerCommandPreflight(runner, command)
	return blocker
}

type runnerCommandPreflightResult struct {
	ResolvedExecutable string
	RunnerPathPrefix   string
}

// runnerCommandPreflight validates every candidate for a bare runner command.
// The first healthy candidate is also returned as a PATH prefix, ensuring the
// later shell launch executes the same binary that passed preflight.
func runnerCommandPreflight(runner RunnerName, command string) (runnerCommandPreflightResult, string) {
	probe, err := runnerCommandPreflightProbe(command, runnerCommandSearchPath())
	if err != nil {
		return runnerCommandPreflightResult{}, "runner preflight blocked: " + err.Error()
	}
	if strings.TrimSpace(probe.Executable) == "" {
		return runnerCommandPreflightResult{}, "runner preflight blocked: runner command is empty"
	}
	candidates, err := executableCandidatesInSearchPath(probe.Executable, probe.SearchPath)
	if err != nil {
		return runnerCommandPreflightResult{}, fmt.Sprintf("runner preflight blocked: executable %q not found: %s", probe.Executable, err.Error())
	}
	var firstHealthErr error
	for _, resolved := range candidates {
		if runnerExecutableNeedsHealthCheck(runner, probe.Executable) {
			if err := runnerExecutableHealthCheck(resolved, probe.SearchPath); err != nil {
				if firstHealthErr == nil {
					firstHealthErr = err
				}
				continue
			}
		}
		result := runnerCommandPreflightResult{ResolvedExecutable: resolved}
		if !strings.ContainsRune(probe.Executable, os.PathSeparator) {
			result.RunnerPathPrefix = filepath.Dir(resolved)
		}
		return result, ""
	}
	first := candidates[0]
	if !runnerExecutableNeedsHealthCheck(runner, probe.Executable) {
		return runnerCommandPreflightResult{ResolvedExecutable: first}, ""
	}
	// Report the first candidate's diagnostic: it is normally the stale binary
	// the operator needs to repair, while still proving no later candidate works.
	return runnerCommandPreflightResult{}, fmt.Sprintf("runner preflight blocked: executable %q resolved to %s but failed health check (and no later PATH candidate worked): %s", probe.Executable, first, firstHealthErr)
}

// runnerCommandWithPathPrefix reapplies the healthy directory *inside* the
// shell command. A login shell may replace inherited PATH while sourcing its
// profile, so environment-only prefixing can resurrect the broken candidate.
func runnerCommandWithPathPrefix(command, prefix string) string {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return command
	}
	return "PATH=" + shellSingleQuote(prefix) + ":\"$PATH\"; export PATH; " + command
}

type runnerCommandProbe struct {
	Executable string
	SearchPath string
}

func runnerCommandPreflightProbe(command, baseSearchPath string) (runnerCommandProbe, error) {
	fields, err := shellLikeFields(command)
	if err != nil {
		return runnerCommandProbe{}, fmt.Errorf("cannot parse runner command: %w", err)
	}
	searchPath := baseSearchPath
	for i := 0; i < len(fields); i++ {
		field := strings.TrimSpace(fields[i])
		if field == "" {
			continue
		}
		if field == "command" {
			continue
		}
		if field == "env" {
			executable, path := envCommandExecutable(fields[i+1:], searchPath)
			return runnerCommandProbe{Executable: executable, SearchPath: path}, nil
		}
		if key, value, ok := shellAssignment(field); ok {
			if key == "PATH" {
				searchPath = expandPathAssignment(value, searchPath)
			}
			continue
		}
		return runnerCommandProbe{Executable: field, SearchPath: searchPath}, nil
	}
	return runnerCommandProbe{SearchPath: searchPath}, nil
}

func envCommandExecutable(fields []string, searchPath string) (string, string) {
	for i := 0; i < len(fields); i++ {
		field := strings.TrimSpace(fields[i])
		if field == "" {
			continue
		}
		if field == "-u" && i+1 < len(fields) {
			i++
			continue
		}
		if strings.HasPrefix(field, "-") {
			continue
		}
		if key, value, ok := shellAssignment(field); ok {
			if key == "PATH" {
				searchPath = expandPathAssignment(value, searchPath)
			}
			continue
		}
		return field, searchPath
	}
	return "", searchPath
}

func shellLikeFields(input string) ([]string, error) {
	var fields []string
	var b strings.Builder
	var quote rune
	inField := false
	escaped := false
	flush := func() {
		if inField {
			fields = append(fields, b.String())
			b.Reset()
			inField = false
		}
	}
	for _, r := range input {
		if escaped {
			b.WriteRune(r)
			inField = true
			escaped = false
			continue
		}
		if r == '\\' && quote != '\'' {
			escaped = true
			inField = true
			continue
		}
		if quote != 0 {
			if r == quote {
				quote = 0
				continue
			}
			b.WriteRune(r)
			inField = true
			continue
		}
		switch r {
		case '\'', '"':
			quote = r
			inField = true
		case ' ', '\t', '\n', '\r':
			flush()
		default:
			b.WriteRune(r)
			inField = true
		}
	}
	if escaped {
		b.WriteRune('\\')
	}
	if quote != 0 {
		return nil, fmt.Errorf("unterminated quoted string")
	}
	flush()
	return fields, nil
}

func shellAssignment(field string) (string, string, bool) {
	idx := strings.Index(field, "=")
	if idx <= 0 {
		return "", "", false
	}
	key := field[:idx]
	for i, r := range key {
		if i == 0 {
			if (r < 'A' || r > 'Z') && (r < 'a' || r > 'z') && r != '_' {
				return "", "", false
			}
			continue
		}
		if (r < 'A' || r > 'Z') && (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '_' {
			return "", "", false
		}
	}
	return key, field[idx+1:], true
}

func expandPathAssignment(value, baseSearchPath string) string {
	value = strings.ReplaceAll(value, "${PATH}", baseSearchPath)
	value = strings.ReplaceAll(value, "$PATH", baseSearchPath)
	return value
}

func lookPathInSearchPath(executable, searchPath string) (string, error) {
	candidates, err := executableCandidatesInSearchPath(executable, searchPath)
	if err != nil {
		return "", err
	}
	return candidates[0], nil
}

func executableCandidatesInSearchPath(executable, searchPath string) ([]string, error) {
	executable = strings.TrimSpace(executable)
	if executable == "" {
		return nil, exec.ErrNotFound
	}
	if strings.ContainsRune(executable, os.PathSeparator) {
		if isExecutableFile(executable) {
			return []string{executable}, nil
		}
		return nil, fmt.Errorf("%s is not executable or does not exist", executable)
	}
	candidates := []string{}
	seen := map[string]struct{}{}
	for _, dir := range filepath.SplitList(searchPath) {
		if dir == "" {
			dir = "."
		}
		candidate := filepath.Join(dir, executable)
		if !isExecutableFile(candidate) {
			continue
		}
		key, err := filepath.Abs(candidate)
		if err != nil {
			key = candidate
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		candidates = append(candidates, candidate)
	}
	if len(candidates) == 0 {
		return nil, fmt.Errorf("%s was not found in PATH", executable)
	}
	return candidates, nil
}

func isExecutableFile(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	return info.Mode()&0o111 != 0
}

func runnerExecutableNeedsHealthCheck(runner RunnerName, executable string) bool {
	base := strings.ToLower(filepath.Base(strings.TrimSpace(executable)))
	if base == "codex" || base == "claude" {
		return true
	}
	switch runner {
	case RunnerCodex, RunnerCodexAppServer, RunnerCodexExec, RunnerCodexCloud:
		return strings.Contains(base, "codex")
	case RunnerClaude:
		return strings.Contains(base, "claude")
	default:
		return false
	}
}

func runnerExecutableHealthCheck(resolved, searchPath string) error {
	ctx, cancel := context.WithTimeout(context.Background(), runnerPreflightTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, resolved, "--version")
	cmd.Env = setEnvValue(os.Environ(), "PATH", searchPath)
	output, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return fmt.Errorf("--version timed out after %s", runnerPreflightTimeout)
	}
	if err != nil {
		summary := strings.TrimSpace(string(output))
		if summary != "" {
			return fmt.Errorf("--version failed: %v: %s", err, truncateRunes(summary, runnerPreflightOutputMaxRune))
		}
		return fmt.Errorf("--version failed: %v", err)
	}
	return nil
}

func truncateRunes(value string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= maxRunes {
		return value
	}
	return string(runes[:maxRunes]) + "..."
}
