package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
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

func runnerBaseEnv() []string {
	return setEnvValue(os.Environ(), "PATH", runnerCommandSearchPath())
}

func runnerCommandSearchPath() string {
	parts := []string{}
	parts = appendPathList(parts, os.Getenv(runnerPathPrefixEnv))
	parts = appendPathList(parts, os.Getenv("PATH"))
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
	probe, err := runnerCommandPreflightProbe(command, runnerCommandSearchPath())
	if err != nil {
		return "runner preflight blocked: " + err.Error()
	}
	if strings.TrimSpace(probe.Executable) == "" {
		return "runner preflight blocked: runner command is empty"
	}
	resolved, err := lookPathInSearchPath(probe.Executable, probe.SearchPath)
	if err != nil {
		return fmt.Sprintf("runner preflight blocked: executable %q not found: %s", probe.Executable, err.Error())
	}
	if runnerExecutableNeedsHealthCheck(runner, probe.Executable) {
		if err := runnerExecutableHealthCheck(resolved, probe.SearchPath); err != nil {
			return fmt.Sprintf("runner preflight blocked: executable %q resolved to %s but failed health check: %s", probe.Executable, resolved, err.Error())
		}
	}
	return ""
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
	executable = strings.TrimSpace(executable)
	if executable == "" {
		return "", exec.ErrNotFound
	}
	if strings.ContainsRune(executable, os.PathSeparator) {
		if isExecutableFile(executable) {
			return executable, nil
		}
		return "", fmt.Errorf("%s is not executable or does not exist", executable)
	}
	for _, dir := range filepath.SplitList(searchPath) {
		if dir == "" {
			dir = "."
		}
		candidate := filepath.Join(dir, executable)
		if isExecutableFile(candidate) {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("%s was not found in PATH", executable)
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
