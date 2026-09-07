package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	runnercore "tusker/internal/runner"
)

func runnerConformanceCmd(args Args) (int, error) {
	code, report, err := runRunnerConformance(args)
	if err != nil {
		return code, err
	}
	if args.Bool("quiet") {
		return code, nil
	}
	if args.Bool("json") {
		emitJSON(report)
	} else {
		printConformanceReport(report)
	}
	return code, nil
}

func runRunnerConformance(args Args) (int, runnercore.ConformanceReport, error) {
	harness := strings.TrimSpace(firstNonEmpty(args.String("harness"), args.String("_pos0")))
	if harness == "" {
		return 2, runnercore.ConformanceReport{}, tuskerError(errorMissingArg, "runner conformance requires --harness")
	}
	preset := runnercore.PermissionPreset(strings.TrimSpace(args.String("preset")))
	if preset == "" {
		preset = runnercore.PresetReadOnly
	}
	vault, err := resolveVaultPath(args, false)
	if err != nil {
		return 2, runnercore.ConformanceReport{}, err
	}
	definition, model, effort, err := conformanceHarnessDefinition(vault, harness)
	if err != nil {
		return 2, runnercore.ConformanceReport{}, err
	}
	live := args.Bool("live")
	exercise := strings.TrimSpace(args.String("exercise"))
	script := strings.TrimSpace(args.String("script"))
	if script != "" {
		if exercise != "" && exercise != "script" {
			return 2, runnercore.ConformanceReport{}, tuskerError(errorInvalidArg, "--script conflicts with --exercise "+exercise)
		}
		exercise = "script"
		info, statErr := os.Stat(script)
		if statErr != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
			return 2, runnercore.ConformanceReport{}, tuskerError(errorInvalidArg, "--script must name an existing executable file")
		}
		script, err = filepath.Abs(script)
		if err != nil {
			return 2, runnercore.ConformanceReport{}, err
		}
	}
	if exercise != "" && exercise != "print" && exercise != "timer" && exercise != "script" {
		return 2, runnercore.ConformanceReport{}, tuskerError(errorInvalidArg, "--exercise must be print, timer, or script")
	}
	if exercise != "" && !live {
		return 2, runnercore.ConformanceReport{}, tuskerError(errorInvalidArg, "--exercise requires --live")
	}
	if live && preset == runnercore.PresetDangerFullAccess && !args.Bool("external-containment") {
		return 2, runnercore.ConformanceReport{}, tuskerError(errorInvalidArg, "danger-full-access live conformance requires --external-containment")
	}
	workspace := v7RepoRoot(vault)
	if override := strings.TrimSpace(args.String("workspace")); override != "" {
		workspace = override
	}
	if live {
		workspace, err = os.MkdirTemp("", "tusker-runner-canary-")
		if err != nil {
			return 2, runnercore.ConformanceReport{}, err
		}
		defer os.RemoveAll(workspace)
	}
	input := runnercore.RunInput{Workspace: workspace, Preset: preset, Model: model, Effort: effort, SearchPath: runnerCommandSearchPath(), Deadline: 30 * time.Minute, PolicyCanary: live, ProtectedPath: filepath.Join(vault, ".runner-conformance-sentinel"), Exercise: exercise, ExerciseScript: script}
	report, runErr := runnercore.Conformance(context.Background(), definition, input, live)
	if cacheErr := saveConformanceReport(DefaultStateRoot(), report); cacheErr != nil {
		return 2, report, cacheErr
	}
	if runErr == nil {
		return 0, report, nil
	}
	var admissionErr *runnercore.AdmissionError
	if errors.As(runErr, &admissionErr) && (admissionErr.Code == "runtime_missing" || admissionErr.Code == "auth_missing" || admissionErr.Code == "invalid_configuration") {
		return 2, report, nil
	}
	return 1, report, nil
}

func conformanceHarnessDefinition(vaultPath, name string) (runnercore.HarnessDefinition, string, string, error) {
	definition := runnercore.HarnessDefinition{ID: name, Provider: runnerVendor(name), Transport: runnercore.TransportCLI, SchemaVersion: 1}
	profileName := name
	resolved, err := resolveTuskerConfig(vaultPath)
	if err != nil {
		return definition, "", "", err
	}
	profiles := runnerProfilesFromSchema(resolved.Config.Automation.Profiles)
	profile, found := profiles[profileName]
	if found {
		name = strings.TrimSpace(profile.Harness)
		definition.ID = profileName
		definition.Provider = runnerVendor(name)
	}
	command := ""
	switch RunnerName(name) {
	case RunnerCodexExec:
		definition.Dialect, definition.Executable = "codex", "codex"
		command = defaultCodexExecCommand()
	case RunnerClaude:
		definition.Dialect, definition.Executable = "claude", "claude"
		command = "claude"
	case RunnerACP:
		definition.Provider, definition.Transport, definition.Dialect = "acp", runnercore.TransportACP, ""
		definition.NativeContainment = argsNativeContainment(profile)
		command = profile.Command
	default:
		if strings.EqualFold(name, "muse") {
			definition.Provider, definition.Dialect, definition.Executable = "muse", "codex", "codex"
			command = "codex --profile muse exec --json --skip-git-repo-check -"
		} else {
			return definition, "", "", tuskerError(errorConfigInvalid, "unknown harness or profile "+profileName)
		}
	}
	if found && strings.TrimSpace(profile.Command) != "" {
		command = profile.Command
	}
	fields, parseErr := shellLikeFields(command)
	if parseErr != nil || len(fields) == 0 {
		return definition, "", "", tuskerError(errorConfigInvalid, "harness command must be structured and parseable")
	}
	definition.Executable, definition.Args = fields[0], append([]string(nil), fields[1:]...)
	definition.Args = removeGeneratedProfileArgs(definition.Dialect, definition.Args, profile.Model, profile.Effort)
	if definition.Dialect == "codex" && strings.Contains(" "+strings.Join(definition.Args, " ")+" ", " --profile muse ") {
		definition.Provider = "muse"
	}
	return definition, strings.TrimSpace(profile.Model), strings.TrimSpace(profile.Effort), nil
}

func argsNativeContainment(profile RunnerProfileDefinition) bool {
	return strings.TrimSpace(profile.Harness) == string(RunnerACP) && profile.NativeContainment
}

func removeGeneratedProfileArgs(dialect string, args []string, model, effort string) []string {
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if (arg == "--model" || arg == "-m") && i+1 < len(args) && args[i+1] == model {
			i++
			continue
		}
		if arg == "--effort" && i+1 < len(args) && args[i+1] == effort {
			i++
			continue
		}
		if dialect == "codex" && (arg == "-c" || arg == "--config") && i+1 < len(args) && strings.HasPrefix(strings.Trim(args[i+1], "'\""), "model_reasoning_effort=") {
			i++
			continue
		}
		out = append(out, arg)
	}
	return out
}

func printConformanceReport(report runnercore.ConformanceReport) {
	fmt.Printf("%s %s %s ready=%t live=%t\n", report.HarnessID, report.Transport, report.Preset, report.Ready, report.Live)
	for _, result := range report.Cases {
		fmt.Printf("  %-20s %-11s %s\n", result.ID, result.Result, result.Evidence)
	}
}

func preparedRunnerForDispatch(vaultPath string, runner RunnerName, command string, selected ResolvedRunnerProfile, policy CodexPolicy, workspace, searchPath string) (runnercore.PreparedLaunch, error) {
	definition, model, effort, err := conformanceHarnessDefinition(vaultPath, firstNonEmpty(selected.Name, string(runner)))
	if err != nil {
		return runnercore.PreparedLaunch{}, err
	}
	if strings.TrimSpace(command) != "" {
		fields, parseErr := shellLikeFields(command)
		if parseErr != nil || len(fields) == 0 {
			return runnercore.PreparedLaunch{}, tuskerError(errorConfigInvalid, "runner command is not structured")
		}
		definition.Executable = fields[0]
		definition.Args = removeGeneratedProfileArgs(definition.Dialect, fields[1:], model, effort)
	}
	preset := runnercore.PermissionPreset(strings.TrimSpace(selected.Definition.PermissionPreset))
	if preset == "" {
		preset = permissionPresetForPolicy(policy)
	}
	input := runnercore.RunInput{Workspace: workspace, Preset: preset, Model: model, Effort: effort, SearchPath: searchPath}
	var cached runnercore.ConformanceReport
	if definition.Provider == "muse" {
		raw, readErr := os.ReadFile(conformanceReportCachePath(DefaultStateRoot(), runnercore.ConformanceReport{HarnessID: definition.ID, Preset: preset}))
		if readErr == nil && json.Unmarshal(raw, &cached) == nil && cached.Ready && cached.ValidUntil != nil && cached.ValidUntil.After(time.Now().UTC()) {
			input.VerifiedAuth = true
		}
	}
	prepared, err := runnercore.Prepare(context.Background(), definition, input)
	if err == nil && definition.Provider == "muse" && cached.ExecutableIdentity != prepared.ExecutableIdentity {
		return runnercore.PreparedLaunch{}, &runnercore.AdmissionError{Code: "launch_changed", HarnessID: definition.ID, Check: "executable_identity", Reason: "Muse executable changed since live conformance", Remedy: "Rerun live conformance."}
	}
	return prepared, err
}

func permissionPresetForPolicy(policy CodexPolicy) runnercore.PermissionPreset {
	mode := strings.TrimSpace(firstNonEmpty(policy.TurnSandboxPolicy, policy.ThreadSandbox))
	if mode == "danger-full-access" {
		return runnercore.PresetDangerFullAccess
	}
	if mode == "read-only" {
		return runnercore.PresetReadOnly
	}
	if policy.TurnSandboxNetwork != nil && *policy.TurnSandboxNetwork {
		return runnercore.PresetWorkspaceNetwork
	}
	return runnercore.PresetWorkspaceOffline
}

func conformanceReportCachePath(stateRoot string, report runnercore.ConformanceReport) string {
	name := strings.NewReplacer("/", "_", "\\", "_", ":", "_").Replace(report.HarnessID + "-" + string(report.Preset))
	return filepath.Join(stateRoot, "runner-conformance", name+".json")
}

func saveConformanceReport(stateRoot string, report runnercore.ConformanceReport) error {
	raw, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	path := conformanceReportCachePath(stateRoot, report)
	if err := ensureDir(filepath.Dir(path)); err != nil {
		return err
	}
	return os.WriteFile(path, append(raw, '\n'), 0o600)
}
