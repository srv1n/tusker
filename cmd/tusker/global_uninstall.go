package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/charmbracelet/x/term"
)

const (
	globalUninstallDaemonService = "daemon_service_uninstall"
	globalUninstallEmptyDir      = "remove_empty_dir"
	globalUninstallStateRoot     = "remove_state_root"
)

type globalUninstallOutcome struct {
	Kind   string `json:"kind"`
	Path   string `json:"path"`
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

// planGlobalUninstall returns the non-destructive machine-level cleanup plan.
func planGlobalUninstall() []tuskerPurgeAction {
	return planGlobalUninstallWithState(false)
}

func planGlobalUninstallWithState(includeStateRoot bool) []tuskerPurgeAction {
	home := userHomeDir()
	stateRoot := filepath.Clean(DefaultStateRoot())
	var actions []tuskerPurgeAction
	add := func(kind, path, reason string) {
		if strings.TrimSpace(path) == "" || !globalExpectedPathSafe(path) {
			return
		}
		actions = append(actions, tuskerPurgeAction{Kind: kind, Path: path, Reason: reason})
	}

	for _, binDir := range globalUninstallBinDirs(home) {
		binPath := filepath.Join(binDir, "tusker")
		if target, ok := globalTuskerBinTarget(binPath); ok {
			action := tuskerPurgeAction{Kind: "remove_path", Path: binPath, Reason: "installed Tusker binary", Target: target}
			actions = append(actions, action)
		}
		backup := filepath.Join(binDir, "tusker.previous")
		if globalTuskerBackup(backup) {
			add("remove_path", backup, "previous Tusker release backup")
		}
	}

	for _, skillDir := range []string{currentSkillInstallDir, "obsidian-vault-tracker"} {
		for _, agentDir := range []string{".agents", ".codex", ".claude"} {
			add("remove_path", filepath.Join(home, agentDir, "skills", skillDir), "user Tusker skill install")
		}
	}

	configPath := userGlobalTuskerConfigPath()
	add("remove_path", configPath, "user Tusker configuration")
	add(globalUninstallEmptyDir, filepath.Dir(configPath), "user Tusker configuration directory if empty")

	serviceConfig := globalUninstallDaemonServiceConfig(stateRoot)
	if daemonServiceGOOS == "darwin" {
		add(globalUninstallDaemonService, serviceConfig.plistPath(), "launchd Tusker daemon service")
	}
	add("remove_path", filepath.Join(stateRoot, "bin", "tusker-daemon"), "daemon service executable left by launchd uninstall")
	add("remove_path", filepath.Join(stateRoot, "acp-adapters"), "installed ACP adapters")
	addGlobalDaemonLogs(&actions, filepath.Join(stateRoot, "logs"))
	if includeStateRoot {
		add(globalUninstallStateRoot, stateRoot, "destructive Tusker runtime history (explicit --state)")
	}

	return sortGlobalUninstallActions(dedupePurgeActions(actions))
}

func globalUninstallBinDirs(home string) []string {
	return []string{
		filepath.Join(home, ".local", "bin"),
		"/opt/homebrew/bin",
		"/usr/local/bin",
	}
}

func globalTuskerBinTarget(path string) (string, bool) {
	if !globalExpectedPathSafe(path) {
		return "", false
	}
	info, err := os.Lstat(path)
	if err != nil {
		return "", false
	}
	if info.Mode()&os.ModeSymlink != 0 {
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil {
			return "", false
		}
		targetInfo, err := os.Stat(resolved)
		if err != nil || !targetInfo.Mode().IsRegular() || targetInfo.Mode()&0o111 == 0 || !globalExecutableHeader(resolved) {
			return "", false
		}
		if filepath.Base(resolved) != "tusker" && !globalSameAsCurrentExecutable(resolved) {
			return "", false
		}
		return resolved, true
	}
	if info.Mode().IsRegular() && info.Mode()&0o111 != 0 && globalExecutableHeader(path) && globalSameAsCurrentExecutable(path) {
		resolved, err := filepath.EvalSymlinks(path)
		if err == nil {
			path = resolved
		}
		return path, true
	}
	return "", false
}

func globalExecutableHeader(path string) bool {
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer file.Close()
	var header [4]byte
	if _, err := io.ReadFull(file, header[:]); err != nil {
		return false
	}
	switch string(header[:]) {
	case "\x7fELF", "MZ\x90\x00", "\xfe\xed\xfa\xce", "\xce\xfa\xed\xfe", "\xfe\xed\xfa\xcf", "\xcf\xfa\xed\xfe", "\xca\xfe\xba\xbe", "\xbe\xba\xfe\xca":
		return true
	default:
		return false
	}
}

func globalSameAsCurrentExecutable(path string) bool {
	executable, err := os.Executable()
	if err != nil {
		return false
	}
	current, err := filepath.EvalSymlinks(executable)
	if err != nil {
		return false
	}
	target, err := filepath.EvalSymlinks(path)
	if err != nil {
		return false
	}
	currentInfo, err := os.Stat(current)
	if err != nil {
		return false
	}
	targetInfo, err := os.Stat(target)
	if err != nil {
		return false
	}
	return os.SameFile(currentInfo, targetInfo)
}

func globalTuskerBackup(path string) bool {
	if !globalExpectedPathSafe(path) {
		return false
	}
	info, err := os.Lstat(path)
	return err == nil && info.Mode()&os.ModeSymlink == 0 && info.Mode().IsRegular()
}

func addGlobalDaemonLogs(actions *[]tuskerPurgeAction, logDir string) {
	if !globalExpectedPathSafe(logDir) {
		return
	}
	entries, err := os.ReadDir(logDir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !(name == "daemon.log" || (strings.HasPrefix(name, "daemon-") && strings.HasSuffix(name, ".log"))) {
			continue
		}
		*actions = append(*actions, tuskerPurgeAction{Kind: "remove_path", Path: filepath.Join(logDir, name), Reason: "daemon log left by launchd uninstall"})
	}
}

func sortGlobalUninstallActions(actions []tuskerPurgeAction) []tuskerPurgeAction {
	priority := map[string]int{
		globalUninstallDaemonService: 0,
		"remove_path":                1,
		globalUninstallEmptyDir:      2,
		globalUninstallStateRoot:     3,
	}
	sort.SliceStable(actions, func(i, j int) bool {
		left, right := priority[actions[i].Kind], priority[actions[j].Kind]
		if left != right {
			return left < right
		}
		return actions[i].Path < actions[j].Path
	})
	return actions
}

func globalUninstallDaemonServiceConfig(stateRoot string) daemonServiceConfig {
	config, err := currentDaemonServiceConfig()
	if err != nil {
		home := userHomeDir()
		return daemonServiceConfig{
			Label:          daemonServiceLabel,
			StateRoot:      stateRoot,
			Home:           home,
			LaunchAgentDir: filepath.Join(home, "Library", "LaunchAgents"),
			Executable:     filepath.Join(stateRoot, "bin", "tusker-daemon"),
		}
	}
	config.StateRoot = stateRoot
	config.Executable = filepath.Join(stateRoot, "bin", "tusker-daemon")
	config.Home = userHomeDir()
	config.LaunchAgentDir = filepath.Join(config.Home, "Library", "LaunchAgents")
	return config
}

func tuskerGlobalUninstallCmd(args Args) error {
	if args.Bool("help") {
		printGlobalUninstallHelp()
		return nil
	}
	if args.Bool("force-state") && !args.Bool("state") {
		return tuskerError(errorInvalidArg, "--force-state requires --state")
	}
	stateRoot := filepath.Clean(DefaultStateRoot())
	if info, err := os.Lstat(stateRoot); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return tuskerError(errorInvalidTransition, "refusing to uninstall through a symlinked Tusker state root", withHint("point TUSKER_STATE_ROOT at the real state directory"))
	}
	if args.Bool("state") {
		if err := validateGlobalUninstallStateRoot(stateRoot); err != nil {
			return err
		}
	}
	var stateLock *os.File
	if args.Bool("state") {
		var busy bool
		var err error
		stateLock, busy, err = globalUninstallStateRootBusy(stateRoot)
		if err != nil {
			return tuskerError(errorInvalidTransition, "cannot inspect Tusker daemon liveness", withHint(err.Error()))
		}
		if busy {
			return tuskerError(errorInvalidTransition, "refusing --state while the Tusker daemon is live", withHint("stop the daemon and retry the state-root removal"))
		}
		defer func() {
			if stateLock != nil {
				_ = syscall.Flock(int(stateLock.Fd()), syscall.LOCK_UN)
				_ = stateLock.Close()
			}
		}()
	}
	actions := planGlobalUninstallWithState(args.Bool("state"))
	projects := globalRegisteredProjects(stateRoot)
	apply := args.Bool("yes") || args.Bool("write") || args.Bool("apply")
	if apply && args.Bool("state") {
		if err := confirmGlobalStateRemoval(args, stateRoot); err != nil {
			return err
		}
	}

	if !apply {
		if args.Bool("json") {
			emitJSON(map[string]any{"ok": true, "dry_run": true, "count": len(actions), "actions": globalUninstallJSONActions(actions), "registered_projects": projects})
			return nil
		}
		if !args.Bool("quiet") {
			fmt.Printf("Tusker global uninstall dry-run (%d actions). Re-run with --yes to apply.\n", len(actions))
			printGlobalUninstallActions(actions)
			printGlobalProjectReminder(projects)
		}
		return nil
	}

	outcomes, err := applyGlobalUninstall(actions, stateRoot)
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": err == nil, "dry_run": false, "count": len(actions), "actions": globalUninstallJSONActions(actions), "outcomes": outcomes, "registered_projects": projects})
	} else if !args.Bool("quiet") {
		fmt.Printf("Tusker global uninstall applied (%d actions).\n", len(actions))
		printGlobalUninstallOutcomes(outcomes)
		printGlobalProjectReminder(projects)
	}
	return err
}

func validateGlobalUninstallStateRoot(stateRoot string) error {
	stateRoot = filepath.Clean(stateRoot)
	home := filepath.Clean(userHomeDir())
	canonicalRoot := canonicalPath(stateRoot)
	canonicalHome := canonicalPath(home)
	if filepath.Dir(stateRoot) == stateRoot || stateRoot == home || isWithinPath(home, stateRoot) || canonicalRoot == canonicalHome || isWithinPath(canonicalHome, canonicalRoot) {
		return tuskerError(errorInvalidTransition, "refusing to remove an unsafe Tusker state root: "+stateRoot, withHint("choose a dedicated state directory below the user home directory"))
	}
	info, err := os.Lstat(runtimeStoreDBPath(stateRoot))
	if errors.Is(err, os.ErrNotExist) {
		return tuskerError(errorInvalidTransition, "refusing to remove a path that is not a Tusker state root: "+stateRoot, withHint("daemon.db is missing from the requested state root"))
	}
	if err != nil {
		return tuskerError(errorInvalidTransition, "cannot inspect Tusker state root marker: "+stateRoot, withHint(err.Error()))
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return tuskerError(errorInvalidTransition, "refusing an invalid Tusker state root marker: "+runtimeStoreDBPath(stateRoot), withHint("daemon.db must be a regular file"))
	}
	return nil
}

func globalUninstallStateRootBusy(stateRoot string) (*os.File, bool, error) {
	for _, path := range []string{filepath.Join(stateRoot, daemonPIDFileName), filepath.Join(stateRoot, daemonLockFileName)} {
		if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
			return nil, false, fmt.Errorf("daemon liveness marker is a symlink: %s", path)
		}
	}
	lockPath := filepath.Join(stateRoot, daemonLockFileName)
	lock, err := os.OpenFile(lockPath, os.O_RDWR|os.O_CREATE|syscall.O_NOFOLLOW, 0o600)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			_ = lock.Close()
			return nil, true, nil
		}
		_ = lock.Close()
		return nil, false, err
	}
	if readDaemonLiveness(stateRoot, time.Now().UTC()).Alive {
		_ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
		_ = lock.Close()
		return nil, true, nil
	}
	return lock, false, nil
}

func confirmGlobalStateRemoval(args Args, stateRoot string) error {
	if args.Bool("force-state") {
		if globalUninstallIsTTY(os.Stdin) {
			return tuskerError(errorInvalidArg, "--force-state is only allowed for non-TTY state removal")
		}
		return nil
	}
	if !globalUninstallIsTTY(os.Stdin) {
		return tuskerError(errorInvalidArg, "--state --yes requires interactive confirmation", withHint("type the confirmation in a TTY or pass --force-state from a non-TTY"))
	}
	token := "DELETE " + stateRoot
	fmt.Printf("This permanently deletes Tusker runtime history. Type %q to continue: ", token)
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	if strings.TrimSpace(line) != token {
		return tuskerError(errorInvalidArg, "state-root deletion was not confirmed")
	}
	return nil
}

func globalUninstallIsTTY(file *os.File) bool {
	if file == nil {
		return false
	}
	return term.IsTerminal(file.Fd())
}

func globalRegisteredProjects(stateRoot string) []RegisteredProject {
	store, err := OpenRuntimeStoreReadOnly(stateRoot)
	if err != nil {
		return nil
	}
	defer store.Close()
	loaded, err := loadRegisteredProjects(store, registeredProjectLoadOptions{MetadataOnly: true, LoadDisabled: true})
	if err != nil {
		return nil
	}
	projects := make([]RegisteredProject, 0, len(loaded))
	for _, entry := range loaded {
		projects = append(projects, entry.Project)
	}
	return projects
}

func applyGlobalUninstall(actions []tuskerPurgeAction, stateRoot string) ([]globalUninstallOutcome, error) {
	serviceConfig := globalUninstallDaemonServiceConfig(stateRoot)
	var outcomes []globalUninstallOutcome
	var errs []error
	for _, action := range actions {
		outcome := globalUninstallOutcome{Kind: action.Kind, Path: action.Path}
		if _, err := os.Lstat(action.Path); errors.Is(err, os.ErrNotExist) && action.Kind != globalUninstallDaemonService {
			outcome.Status = "absent"
			outcomes = append(outcomes, outcome)
			continue
		}
		var err error
		switch action.Kind {
		case globalUninstallDaemonService:
			info, statErr := os.Lstat(action.Path)
			if errors.Is(statErr, os.ErrNotExist) {
				outcome.Status = "absent"
				outcomes = append(outcomes, outcome)
				continue
			}
			if statErr != nil {
				err = statErr
			} else if info.Mode()&os.ModeSymlink != 0 {
				err = fmt.Errorf("refusing symlink launchd plist: %s", action.Path)
			} else {
				err = daemonServiceUninstall(Args{"quiet": "true"}, serviceConfig)
			}
		case globalUninstallEmptyDir:
			info, statErr := os.Lstat(action.Path)
			if errors.Is(statErr, os.ErrNotExist) {
				outcome.Status = "absent"
				outcomes = append(outcomes, outcome)
				continue
			}
			if statErr != nil {
				err = statErr
			} else if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
				err = fmt.Errorf("refusing non-directory config parent: %s", action.Path)
			} else {
				entries, readErr := os.ReadDir(action.Path)
				if readErr != nil {
					err = readErr
				} else if len(entries) == 0 {
					err = os.Remove(action.Path)
				} else {
					outcome.Status = "skipped (not empty)"
					outcomes = append(outcomes, outcome)
					continue
				}
			}
		default:
			purgeAction := action
			if action.Kind == globalUninstallStateRoot {
				purgeAction.Kind = "remove_path"
			}
			err = applyTuskerPurge("", []tuskerPurgeAction{purgeAction})
		}
		if err != nil {
			outcome.Status = "error"
			outcome.Error = err.Error()
			errs = append(errs, fmt.Errorf("%s: %w", action.Path, err))
		} else {
			outcome.Status = "removed"
		}
		outcomes = append(outcomes, outcome)
	}
	return outcomes, errors.Join(errs...)
}

func globalUninstallJSONActions(actions []tuskerPurgeAction) []map[string]any {
	out := make([]map[string]any, 0, len(actions))
	for _, action := range actions {
		item := map[string]any{"kind": action.Kind, "path": action.Path, "reason": action.Reason, "exists": globalUninstallPathExists(action.Path)}
		if action.Target != "" {
			item["target"] = action.Target
		}
		out = append(out, item)
	}
	return out
}

func globalUninstallPathExists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func globalExpectedPathSafe(path string) bool {
	path = filepath.Clean(path)
	if path == "." || path == string(filepath.Separator) {
		return true
	}
	for current := filepath.Dir(path); ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err == nil && info.Mode()&os.ModeSymlink != 0 && !globalAllowedSystemAlias(current) {
			return false
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return false
		}
		parent := filepath.Dir(current)
		if parent == current {
			return true
		}
	}
}

func globalAllowedSystemAlias(path string) bool {
	switch filepath.Clean(path) {
	case "/private", "/tmp", "/var":
		return true
	default:
		return false
	}
}

func printGlobalUninstallActions(actions []tuskerPurgeAction) {
	for _, action := range actions {
		target := ""
		if action.Target != "" {
			target = " -> " + action.Target
		}
		exists := "missing"
		if globalUninstallPathExists(action.Path) {
			exists = "exists"
		}
		fmt.Printf("- [%s] %s: %s%s (%s)\n", exists, action.Kind, action.Path, target, action.Reason)
	}
}

func printGlobalUninstallOutcomes(outcomes []globalUninstallOutcome) {
	for _, outcome := range outcomes {
		if outcome.Error != "" {
			fmt.Printf("- %s: %s (%s: %s)\n", outcome.Kind, outcome.Path, outcome.Status, outcome.Error)
			continue
		}
		fmt.Printf("- %s: %s (%s)\n", outcome.Kind, outcome.Path, outcome.Status)
	}
}

func printGlobalProjectReminder(projects []RegisteredProject) {
	fmt.Println("Registered projects (read-only); repo-level cleanup remains `tusker purge --repo <r>`:")
	if len(projects) == 0 {
		fmt.Println("- none")
		return
	}
	for _, project := range projects {
		name := strings.TrimSpace(project.Name)
		if name == "" {
			name = project.ProjectID
		}
		fmt.Printf("- %s: %s\n", name, project.RepoRoot)
	}
}

func printGlobalUninstallHelp() {
	fmt.Print(`Usage:
  tusker uninstall [--yes] [--state] [--force-state] [--json]

Purpose:
  Show or apply machine-level Tusker cleanup. Repo source and repo-scoped
  state are not removed; use ` + "`tusker purge --repo <r>`" + ` ` + `for that cleanup.

Behavior:
  - default is a dry-run
  - --yes applies machine-level cleanup
  - --state includes destructive runtime history only after explicit confirmation
  - --force-state is the non-TTY confirmation path for --state
`)
}
