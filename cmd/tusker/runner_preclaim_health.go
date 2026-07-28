package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const runnerInfrastructureBlockedState = "infrastructure_blocked"

// RunnerInfrastructureBlock is the durable, operator-facing reason a daemon
// refused a runner before it created a lease or attempt. It intentionally
// contains one remedy: this is an infrastructure repair, not a menu of ways
// to bypass dispatch safety.
type RunnerInfrastructureBlock struct {
	State          string `json:"state"`
	Runner         string `json:"runner"`
	Command        string `json:"command"`
	Executable     string `json:"executable"`
	PathProvenance string `json:"pathProvenance"`
	FailedCheck    string `json:"failedCheck"`
	Reason         string `json:"reason"`
	Remedy         string `json:"remedy"`
}

type runnerPreclaimHealthResult struct {
	Preflight runnerCommandPreflightResult
	Block     *RunnerInfrastructureBlock
}

func runnerPreclaimHealth(runner RunnerName, command string) runnerPreclaimHealthResult {
	return runnerPreclaimHealthWithSearchPath(runner, command, runnerCommandSearchPath())
}

func runnerPreclaimHealthWithSearchPath(runner RunnerName, command, searchPath string) runnerPreclaimHealthResult {
	block := func(check, executable, reason string) runnerPreclaimHealthResult {
		return runnerPreclaimHealthResult{Block: &RunnerInfrastructureBlock{
			State:          runnerInfrastructureBlockedState,
			Runner:         string(runner),
			Command:        strings.TrimSpace(command),
			Executable:     executable,
			PathProvenance: searchPath,
			FailedCheck:    check,
			Reason:         reason,
			Remedy:         runnerInfrastructureRemedy(runner),
		}}
	}

	if err := validateRunnerCommandShape(command, nil); err != nil {
		return block("command_shape", "", err.Error())
	}
	probe, err := runnerCommandPreflightProbe(command, searchPath)
	if err != nil {
		return block("command_shape", "", err.Error())
	}
	candidates, err := authorizedExecutableCandidates(probe.Executable, probe.SearchPath)
	if err != nil {
		return block("executable", probe.Executable, err.Error())
	}

	// The first candidate is the authorized executable: an explicit command,
	// command-local PATH assignment, or the daemon's effective PATH selected it.
	// Reporting a later candidate is useful diagnosis, but silently replacing the
	// authorized binary would make the inspected command differ from dispatch.
	resolved := candidates[0]
	if !isExecutableFile(resolved) {
		reason := fmt.Sprintf("authorized executable %q resolved to %s but is not executable", probe.Executable, resolved)
		if len(candidates) > 1 {
			reason += fmt.Sprintf("; discovered alternate %s was not substituted", candidates[1])
		}
		return block("permission", resolved, reason)
	}
	version := ""
	if runnerExecutableNeedsHealthCheck(runner, probe.Executable) {
		version, err = runnerExecutableHealthCheck(resolved, probe.SearchPath)
		if err != nil {
			reason := fmt.Sprintf("executable %q resolved to %s but failed health check: %s", probe.Executable, resolved, err)
			if len(candidates) > 1 {
				reason += fmt.Sprintf("; discovered alternate %s was not substituted", candidates[1])
			}
			return block("version", resolved, reason)
		}
	}
	result := runnerCommandPreflightResult{ResolvedExecutable: resolved, ExecutableVersion: version, SearchPath: probe.SearchPath}
	if !strings.ContainsRune(probe.Executable, os.PathSeparator) {
		result.RunnerPathPrefix = filepath.Dir(resolved)
	}
	return runnerPreclaimHealthResult{Preflight: result}
}

// authorizedExecutableCandidates deliberately retains non-executable files.
// exec.LookPath-style helpers discard them, which would make a later PATH hit
// silently replace the configured first candidate before we can report a
// permission failure.
func authorizedExecutableCandidates(executable, searchPath string) ([]string, error) {
	executable = strings.TrimSpace(executable)
	if executable == "" {
		return nil, fmt.Errorf("runner executable is empty")
	}
	if strings.ContainsRune(executable, os.PathSeparator) {
		info, err := os.Stat(executable)
		if err != nil || info.IsDir() {
			return nil, fmt.Errorf("%s does not exist or is not a file", executable)
		}
		return []string{executable}, nil
	}
	seen := map[string]struct{}{}
	candidates := []string{}
	for _, dir := range filepath.SplitList(searchPath) {
		if dir == "" {
			dir = "."
		}
		candidate := filepath.Join(dir, executable)
		info, err := os.Stat(candidate)
		if err != nil || info.IsDir() {
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

func runnerInfrastructureRemedy(runner RunnerName) string {
	return fmt.Sprintf("Repair the authorized %s runner command or its daemon PATH, then redrive the task.", runner)
}

func validateRunnerCommandShape(command string, argv []string) error {
	command = strings.TrimSpace(command)
	if command == "" && len(argv) == 0 {
		return fmt.Errorf("runner command is empty")
	}
	if len(argv) > 0 && strings.TrimSpace(argv[0]) == "" {
		return fmt.Errorf("runner command argv executable is empty")
	}
	if containsString(argv, "app-server") {
		return fmt.Errorf("runner command is app-server mode; the daemon needs a detached CLI command")
	}
	if command != "" {
		if strings.ContainsAny(command, ";|&`") {
			return fmt.Errorf("runner command contains shell control syntax")
		}
		fields, err := shellLikeFields(command)
		if err != nil {
			return fmt.Errorf("cannot parse runner command: %w", err)
		}
		if len(fields) == 0 {
			return fmt.Errorf("runner command is empty")
		}
		if containsString(fields, "app-server") {
			return fmt.Errorf("runner command is app-server mode; the daemon needs a detached CLI command")
		}
	}
	return nil
}

func runnerInfrastructureBlockReason(block *RunnerInfrastructureBlock) string {
	if block == nil {
		return ""
	}
	return "infrastructure_blocked: " + block.Reason + "; remedy: " + block.Remedy
}
