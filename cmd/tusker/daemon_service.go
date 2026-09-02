package main

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"syscall"
	"time"
)

const (
	daemonServiceLabel            = "com.tusker.daemon"
	daemonServiceThrottleInterval = 10
	daemonLaunchctlPath           = "/bin/launchctl"
	daemonServiceStartupTimeout   = 5 * time.Second
	daemonServiceBootstrapRetries = 60
	daemonLogRetentionCount       = 5
	daemonLogRetentionAge         = 7 * 24 * time.Hour
	daemonLogMaxBytes             = 10 << 20
)

var (
	daemonServiceGOOS          = runtime.GOOS
	daemonServiceConfigCurrent = currentDaemonServiceConfig
	daemonServiceCommandRun    = runDaemonServiceCommandReal
	daemonServiceWaitReady     = waitForDaemonServiceReady
)

type daemonServiceConfig struct {
	Label            string
	SourceExecutable string
	Executable       string
	StateRoot        string
	Home             string
	Path             string
	LaunchAgentDir   string
}

type daemonServiceCommand struct {
	Name string
	Args []string
}

type daemonServiceRuntimeHealth struct {
	Alive      bool
	StartedAt  time.Time
	LastPollAt time.Time
}

func (h daemonServiceRuntimeHealth) readySince(startedAt time.Time) bool {
	return h.hasCurrentProcessPoll() && !h.LastPollAt.Before(startedAt)
}

func (h daemonServiceRuntimeHealth) healthyAt(now time.Time) bool {
	return h.hasCurrentProcessPoll() && !h.LastPollAt.Before(now.Add(-daemonHeartbeatDeadThreshold))
}

func (h daemonServiceRuntimeHealth) hasCurrentProcessPoll() bool {
	return h.Alive && !h.StartedAt.IsZero() && !h.LastPollAt.IsZero() && !h.LastPollAt.Before(h.StartedAt)
}

func (c daemonServiceConfig) plistPath() string {
	return filepath.Join(c.LaunchAgentDir, c.Label+".plist")
}

func (c daemonServiceConfig) logDir() string {
	return filepath.Join(c.StateRoot, "logs")
}

func (c daemonServiceConfig) binaryDir() string {
	return filepath.Join(c.StateRoot, "bin")
}

func (c daemonServiceConfig) stdoutPath() string {
	return filepath.Join(c.logDir(), "daemon.log")
}

func (c daemonServiceConfig) stderrPath() string {
	return c.stdoutPath()
}

// rotateDaemonServiceLogs bounds launchd's append-only log files at service
// lifecycle boundaries. launchd has no portable native rotation policy; the
// daemon itself therefore keeps a small, owner-only history whenever it is
// installed or restarted, retaining crash evidence instead of deleting it.
func rotateDaemonServiceLogs(c daemonServiceConfig, now time.Time) error {
	if err := ensureDir(c.logDir()); err != nil {
		return err
	}
	if err := os.Chmod(c.logDir(), 0o700); err != nil {
		return err
	}
	unlock, err := acquireDaemonLogRotationLock(c.logDir())
	if err != nil {
		return err
	}
	defer unlock()
	current := c.stdoutPath()
	if info, err := inspectOwnedDaemonLog(current); err == nil {
		if err := os.Chmod(current, 0o600); err != nil {
			return err
		}
		if info.Size() >= daemonLogMaxBytes {
			archive := filepath.Join(c.logDir(), fmt.Sprintf("daemon-%s.log", now.UTC().Format("20060102T150405.000000000Z")))
			if _, err := os.Lstat(archive); !os.IsNotExist(err) {
				return fmt.Errorf("daemon log archive already exists: %s", archive)
			}
			if err := os.Rename(current, archive); err != nil {
				return err
			}
			if err := os.Chmod(archive, 0o600); err != nil {
				return err
			}
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	entries, err := os.ReadDir(c.logDir())
	if err != nil {
		return err
	}
	type logEntry struct {
		path string
		mod  time.Time
	}
	var archives []logEntry
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), "daemon-") || !strings.HasSuffix(entry.Name(), ".log") {
			continue
		}
		path := filepath.Join(c.logDir(), entry.Name())
		info, err := inspectOwnedDaemonLog(path)
		if err != nil {
			return err
		}
		if now.Sub(info.ModTime()) > daemonLogRetentionAge {
			if err := os.Remove(path); err != nil {
				return err
			}
			continue
		}
		if err := os.Chmod(path, 0o600); err != nil {
			return err
		}
		archives = append(archives, logEntry{path: path, mod: info.ModTime()})
	}
	sort.Slice(archives, func(i, j int) bool { return archives[i].mod.After(archives[j].mod) })
	if len(archives) > daemonLogRetentionCount {
		for _, entry := range archives[daemonLogRetentionCount:] {
			if err := os.Remove(entry.path); err != nil {
				return err
			}
		}
	}
	return nil
}

// enforceDaemonServiceLogBound trims the launchd-owned inode in place. A
// rename-based rotation is not sufficient for a long-running launchd job:
// launchd keeps the old descriptor open and would continue writing to an
// unbounded archived inode. Trimming in place keeps the live descriptor and
// bounds disk usage even when the daemon never restarts.
func enforceDaemonServiceLogBound(c daemonServiceConfig) error {
	file, err := os.OpenFile(c.stdoutPath(), os.O_RDWR|syscall.O_NOFOLLOW, 0)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer file.Close()
	if err := requirePrivateRunnerFile(file, c.stdoutPath()); err != nil {
		return err
	}
	info, err := file.Stat()
	if err != nil || info.Size() <= daemonLogMaxBytes {
		return err
	}
	keep := int64(daemonLogMaxBytes / 2)
	if _, err := file.Seek(-keep, io.SeekEnd); err != nil {
		return err
	}
	tail := make([]byte, keep)
	if _, err := io.ReadFull(file, tail); err != nil {
		return err
	}
	if err := file.Truncate(0); err != nil {
		return err
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if _, err := file.Write(tail); err != nil {
		return err
	}
	return file.Sync()
}

func startDaemonServiceLogBoundGuard(c daemonServiceConfig) func() {
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				_ = enforceDaemonServiceLogBound(c)
			case <-stop:
				return
			}
		}
	}()
	return func() {
		close(stop)
		<-done
		_ = enforceDaemonServiceLogBound(c)
	}
}

func inspectOwnedDaemonLog(path string) (os.FileInfo, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("daemon log is not a regular non-symlink file: %s", path)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Getuid()) {
		return nil, fmt.Errorf("daemon log is not owned by the current user: %s", path)
	}
	if stat.Nlink != 1 {
		return nil, fmt.Errorf("daemon log has unexpected hard links: %s", path)
	}
	return info, nil
}

func acquireDaemonLogRotationLock(logDir string) (func(), error) {
	path := filepath.Join(logDir, ".rotation.lock")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, err
	}
	if err := requirePrivateRunnerFile(file, path); err != nil {
		_ = file.Close()
		return nil, err
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX); err != nil {
		_ = file.Close()
		return nil, err
	}
	return func() {
		_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
		_ = file.Close()
	}, nil
}

func (c daemonServiceConfig) domainTarget() string {
	return fmt.Sprintf("gui/%d", os.Getuid())
}

func (c daemonServiceConfig) serviceTarget() string {
	return c.domainTarget() + "/" + c.Label
}

func currentDaemonServiceConfig() (daemonServiceConfig, error) {
	executable, err := os.Executable()
	if err != nil {
		return daemonServiceConfig{}, fmt.Errorf("resolve current tusker executable: %w", err)
	}
	home := userHomeDir()
	if home == "" {
		return daemonServiceConfig{}, fmt.Errorf("resolve home directory for launchd user agent")
	}
	stateRoot, err := daemonServiceStateRoot(home, os.Getenv("TUSKER_STATE_ROOT"))
	if err != nil {
		return daemonServiceConfig{}, err
	}
	path := strings.TrimSpace(os.Getenv("PATH"))
	if path == "" {
		path = "/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin"
	}
	return daemonServiceConfig{
		Label:            daemonServiceLabel,
		SourceExecutable: executable,
		Executable:       filepath.Join(stateRoot, "bin", "tusker-daemon"),
		StateRoot:        stateRoot,
		Home:             home,
		Path:             path,
		LaunchAgentDir:   filepath.Join(home, "Library", "LaunchAgents"),
	}, nil
}

func daemonServiceStateRoot(home, explicit string) (string, error) {
	stateRoot := filepath.Join(home, "Library", "Application Support", "tusker")
	explicit = strings.TrimSpace(explicit)
	if explicit == "" {
		return stateRoot, nil
	}
	configured, err := filepath.Abs(explicit)
	if err != nil {
		return "", err
	}
	configured = canonicalPath(configured)
	stateRoot = canonicalPath(stateRoot)
	if configured != stateRoot {
		return "", tuskerError(errorConfigInvalid,
			"daemon service requires the canonical macOS Application Support state root",
			withHint("unset TUSKER_STATE_ROOT for daemon service commands; the override remains available for foreground daemon runs and tests"),
			withContext(map[string]any{"configured_state_root": configured, "required_state_root": stateRoot}))
	}
	return stateRoot, nil
}

func (c daemonServiceConfig) validate() error {
	for field, value := range map[string]string{
		"label":              c.Label,
		"source executable":  c.SourceExecutable,
		"service executable": c.Executable,
		"state root":         c.StateRoot,
		"home":               c.Home,
		"PATH":               c.Path,
		"LaunchAgents path":  c.LaunchAgentDir,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("daemon service %s is empty", field)
		}
	}
	return nil
}

// renderDaemonServicePlist is intentionally pure so plist contents can be tested without launchd.
func renderDaemonServicePlist(c daemonServiceConfig) string {
	var b strings.Builder
	b.WriteString("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n")
	b.WriteString("<!DOCTYPE plist PUBLIC \"-//Apple//DTD PLIST 1.0//EN\" \"http://www.apple.com/DTDs/PropertyList-1.0.dtd\">\n")
	b.WriteString("<plist version=\"1.0\">\n<dict>\n")
	writeDaemonServicePlistString(&b, "Label", c.Label)
	b.WriteString("<key>ProgramArguments</key>\n<array>\n")
	writeDaemonServicePlistValue(&b, c.Executable)
	writeDaemonServicePlistValue(&b, "daemon")
	writeDaemonServicePlistValue(&b, "run")
	b.WriteString("</array>\n")
	b.WriteString("<key>RunAtLoad</key>\n<true/>\n")
	b.WriteString("<key>KeepAlive</key>\n<dict>\n<key>SuccessfulExit</key>\n<false/>\n</dict>\n")
	b.WriteString("<key>ThrottleInterval</key>\n<integer>10</integer>\n")
	b.WriteString("<key>EnvironmentVariables</key>\n<dict>\n")
	writeDaemonServicePlistString(&b, "PATH", c.Path)
	writeDaemonServicePlistString(&b, "TUSKER_STATE_ROOT", c.StateRoot)
	writeDaemonServicePlistString(&b, daemonLaunchdEnvKey, "1")
	b.WriteString("</dict>\n")
	writeDaemonServicePlistString(&b, "StandardOutPath", c.stdoutPath())
	writeDaemonServicePlistString(&b, "StandardErrorPath", c.stderrPath())
	b.WriteString("</dict>\n</plist>\n")
	return b.String()
}

func writeDaemonServicePlistString(b *strings.Builder, key, value string) {
	b.WriteString("<key>")
	writeDaemonServicePlistEscaped(b, key)
	b.WriteString("</key>\n")
	writeDaemonServicePlistValue(b, value)
}

func writeDaemonServicePlistValue(b *strings.Builder, value string) {
	b.WriteString("<string>")
	writeDaemonServicePlistEscaped(b, value)
	b.WriteString("</string>\n")
}

func writeDaemonServicePlistEscaped(b *strings.Builder, value string) {
	_ = xml.EscapeText(b, []byte(value))
}

// planDaemonService is intentionally pure so lifecycle command construction can be tested without launchd.
func planDaemonService(action string, c daemonServiceConfig) ([]daemonServiceCommand, error) {
	action = strings.TrimSpace(action)
	switch action {
	case "install":
		return []daemonServiceCommand{
			{Name: daemonLaunchctlPath, Args: []string{"bootout", c.serviceTarget()}},
			{Name: daemonLaunchctlPath, Args: []string{"bootstrap", c.domainTarget(), c.plistPath()}},
		}, nil
	case "start":
		return []daemonServiceCommand{{Name: daemonLaunchctlPath, Args: []string{"bootstrap", c.domainTarget(), c.plistPath()}}}, nil
	case "stop":
		return []daemonServiceCommand{
			{Name: c.Executable, Args: []string{"daemon", "stop"}},
			{Name: daemonLaunchctlPath, Args: []string{"bootout", c.serviceTarget()}},
		}, nil
	case "status":
		return []daemonServiceCommand{{Name: daemonLaunchctlPath, Args: []string{"print", c.serviceTarget()}}}, nil
	case "uninstall":
		return []daemonServiceCommand{{Name: daemonLaunchctlPath, Args: []string{"bootout", c.serviceTarget()}}}, nil
	default:
		return nil, tuskerError(errorInvalidArg, "daemon service action must be install, start, stop, refresh, status, or uninstall")
	}
}

func daemonServiceCmd(args Args) error {
	if daemonServiceGOOS != "darwin" {
		return fmt.Errorf("tusker daemon service is supported only on macOS; use your platform service manager to run `tusker daemon run` on %s", daemonServiceGOOS)
	}
	config, err := daemonServiceConfigCurrent()
	if err != nil {
		return err
	}
	if err := config.validate(); err != nil {
		return err
	}
	action := strings.TrimSpace(args.String("_pos0"))
	switch action {
	case "install":
		if err := rejectAgentSpawn("tusker daemon service install"); err != nil {
			return err
		}
		return daemonServiceInstall(args, config)
	case "start":
		if err := rejectAgentSpawn("tusker daemon service start"); err != nil {
			return err
		}
		return daemonServiceStart(args, config)
	case "stop":
		return daemonServiceStop(args, config)
	case "refresh":
		return daemonServiceRefresh(args, config)
	case "status":
		return daemonServiceStatus(args, config)
	case "uninstall":
		return daemonServiceUninstall(args, config)
	default:
		_, err := planDaemonService(action, config)
		return err
	}
}

// daemonServiceRefresh updates the dormant launchd executable without starting
// a daemon. The macOS app installer uses it after unloading an older service so
// the app bundle, foreground runtime, and any later launchd start stay aligned.
func daemonServiceRefresh(args Args, config daemonServiceConfig) error {
	if err := installDaemonServiceExecutable(config); err != nil {
		return err
	}
	displayDaemonServiceResult(args, map[string]any{
		"ok": true, "action": "refresh", "executable": config.Executable,
	}, "Refreshed daemon service executable: "+config.Executable)
	return nil
}

func daemonServiceInstall(args Args, config daemonServiceConfig) error {
	if err := requireDaemonServiceProjectAccess(args, config); err != nil {
		return err
	}
	if err := ensureDir(config.LaunchAgentDir); err != nil {
		return fmt.Errorf("create LaunchAgents directory: %w", err)
	}
	if err := ensureDir(config.logDir()); err != nil {
		return fmt.Errorf("create daemon service log directory: %w", err)
	}
	if err := rotateDaemonServiceLogs(config, time.Now().UTC()); err != nil {
		return fmt.Errorf("rotate daemon service logs: %w", err)
	}
	if err := installDaemonServiceExecutable(config); err != nil {
		return err
	}
	if err := writeDaemonServicePlist(config.plistPath(), renderDaemonServicePlist(config)); err != nil {
		return err
	}
	commands, _ := planDaemonService("install", config)
	if _, err := runDaemonServiceCommand(commands[0], config); err != nil && !daemonServiceNotLoaded(err) {
		return fmt.Errorf("unload existing daemon service before install: %w", err)
	}
	startedAt := time.Now().UTC()
	if err := bootstrapDaemonService(commands[1], config); err != nil {
		return fmt.Errorf("install daemon service: %w", err)
	}
	if err := daemonServiceWaitReady(config, startedAt, daemonServiceStartupTimeout); err != nil {
		return err
	}
	displayDaemonServiceResult(args, map[string]any{
		"ok": true, "action": "install", "label": config.Label, "plist": config.plistPath(), "state_root": config.StateRoot,
		"executable": config.Executable,
	}, "Installed and started daemon service: "+config.Label)
	return nil
}

func daemonServiceStart(args Args, config daemonServiceConfig) error {
	if err := rotateDaemonServiceLogs(config, time.Now().UTC()); err != nil {
		return fmt.Errorf("rotate daemon service logs: %w", err)
	}
	if _, err := os.Stat(config.plistPath()); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return tuskerError(errorNotFound, "daemon service is not installed", withHint("run `tusker daemon service install` first"))
		}
		return fmt.Errorf("check daemon service plist: %w", err)
	}
	if err := requireDaemonServiceProjectAccess(args, config); err != nil {
		return err
	}
	loaded, err := daemonServiceLoaded(config)
	if err != nil {
		return err
	}
	commands, _ := planDaemonService("start", config)
	command := commands[0]
	if loaded {
		command = daemonServiceCommand{Name: daemonLaunchctlPath, Args: []string{"kickstart", config.serviceTarget()}}
	}
	startedAt := time.Now().UTC()
	if err := bootstrapDaemonService(command, config); err != nil {
		return fmt.Errorf("start daemon service: %w", err)
	}
	if err := daemonServiceWaitReady(config, startedAt, daemonServiceStartupTimeout); err != nil {
		return err
	}
	displayDaemonServiceResult(args, map[string]any{"ok": true, "action": "start", "label": config.Label}, "Started daemon service: "+config.Label)
	return nil
}

func daemonServiceStop(args Args, config daemonServiceConfig) error {
	loaded, err := daemonServiceLoaded(config)
	if err != nil {
		return err
	}
	if !loaded {
		displayDaemonServiceResult(args, map[string]any{"ok": true, "action": "stop", "stopped": false, "label": config.Label}, "Daemon service is not running")
		return nil
	}
	commands, _ := planDaemonService("stop", config)
	if _, err := runDaemonServiceCommand(commands[0], config); err != nil {
		return fmt.Errorf("stop daemon service cleanly: %w", err)
	}
	if _, err := runDaemonServiceCommand(commands[1], config); err != nil && !daemonServiceNotLoaded(err) {
		return fmt.Errorf("unload daemon service after clean stop: %w", err)
	}
	displayDaemonServiceResult(args, map[string]any{"ok": true, "action": "stop", "stopped": true, "loaded": false, "label": config.Label}, "Stopped and unloaded daemon service: "+config.Label)
	return nil
}

func daemonServiceStatus(args Args, config daemonServiceConfig) error {
	installed := true
	if _, err := os.Stat(config.plistPath()); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			installed = false
		} else {
			return fmt.Errorf("check daemon service plist: %w", err)
		}
	}
	loaded, err := daemonServiceLoaded(config)
	if err != nil {
		return err
	}
	runtimeHealth, err := daemonServiceHealth(config)
	if err != nil {
		return err
	}
	protectedProjects, err := daemonServiceProtectedProjects(config)
	if err != nil {
		return err
	}
	authorization := "not_required"
	if len(protectedProjects) > 0 {
		authorization = "unknown"
	}
	payload := map[string]any{
		"ok": true, "action": "status", "label": config.Label, "installed": installed, "loaded": loaded,
		"plist": config.plistPath(), "stdout_log": config.stdoutPath(), "stderr_log": config.stderrPath(), "state_root": config.StateRoot,
		"executable":   config.Executable,
		"daemon_alive": runtimeHealth.Alive,
		"last_poll_at": nullIfBlank(runtimeHealth.LastPollAt.Format(time.RFC3339Nano)),
		"healthy":      runtimeHealth.healthyAt(time.Now().UTC()),
		"project_storage": map[string]any{
			"shared_state_root":           config.StateRoot,
			"protected_projects":          protectedProjects,
			"protected_override_required": len(protectedProjects) > 0,
			"authorization":               authorization,
		},
	}
	if args.Bool("json") {
		emitJSON(payload)
		return nil
	}
	fmt.Printf("Daemon service: %s\n", config.Label)
	fmt.Printf("Installed: %t\n", installed)
	fmt.Printf("Loaded: %t\n", loaded)
	fmt.Printf("Healthy: %t\n", runtimeHealth.healthyAt(time.Now().UTC()))
	fmt.Printf("Protected-project override required: %t\n", len(protectedProjects) > 0)
	for _, issue := range protectedProjects {
		fmt.Printf("Protected project: %s (%s)\n", firstNonEmpty(issue.Name, issue.ProjectID), issue.MatchedPath)
	}
	fmt.Printf("Plist: %s\n", config.plistPath())
	fmt.Printf("Logs: %s, %s\n", config.stdoutPath(), config.stderrPath())
	return nil
}

func daemonServiceUninstall(args Args, config daemonServiceConfig) error {
	commands, _ := planDaemonService("uninstall", config)
	if _, err := runDaemonServiceCommand(commands[0], config); err != nil && !daemonServiceNotLoaded(err) {
		return fmt.Errorf("unload daemon service: %w", err)
	}
	removed := true
	if err := os.Remove(config.plistPath()); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			removed = false
		} else {
			return fmt.Errorf("remove daemon service plist: %w", err)
		}
	}
	displayDaemonServiceResult(args, map[string]any{"ok": true, "action": "uninstall", "uninstalled": removed, "label": config.Label}, "Uninstalled daemon service: "+config.Label)
	return nil
}

func daemonServiceLoaded(config daemonServiceConfig) (bool, error) {
	commands, _ := planDaemonService("status", config)
	if _, err := runDaemonServiceCommand(commands[0], config); err != nil {
		if daemonServiceNotLoaded(err) {
			return false, nil
		}
		return false, fmt.Errorf("inspect daemon service: %w", err)
	}
	return true, nil
}

func runDaemonServiceCommand(command daemonServiceCommand, config daemonServiceConfig) ([]byte, error) {
	return daemonServiceCommandRun(command, config)
}

func runDaemonServiceCommandReal(command daemonServiceCommand, config daemonServiceConfig) ([]byte, error) {
	cmd := exec.Command(command.Name, command.Args...)
	cmd.Env = environmentWith(os.Environ(), "TUSKER_STATE_ROOT", config.StateRoot)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return output, fmt.Errorf("%s: %w: %s", command.String(), err, strings.TrimSpace(string(output)))
	}
	return output, nil
}

func bootstrapDaemonService(command daemonServiceCommand, config daemonServiceConfig) error {
	var lastErr error
	for attempt := 0; attempt < daemonServiceBootstrapRetries; attempt++ {
		if _, err := runDaemonServiceCommand(command, config); err == nil {
			return nil
		} else if !daemonServiceBootstrapRetryable(err) {
			return err
		} else {
			lastErr = err
		}
		time.Sleep(100 * time.Millisecond)
	}
	return lastErr
}

func daemonServiceBootstrapRetryable(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "bootstrap failed: 5") || strings.Contains(message, "input/output error")
}

func (c daemonServiceCommand) String() string {
	return strings.TrimSpace(c.Name + " " + strings.Join(c.Args, " "))
}

func environmentWith(environment []string, key, value string) []string {
	prefix := key + "="
	result := make([]string, 0, len(environment)+1)
	for _, item := range environment {
		if !strings.HasPrefix(item, prefix) {
			result = append(result, item)
		}
	}
	return append(result, prefix+value)
}

func daemonServiceNotLoaded(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "could not find service") || strings.Contains(message, "no such process") || strings.Contains(message, "no such service")
}

func writeDaemonServicePlist(path, content string) error {
	temp, err := os.CreateTemp(filepath.Dir(path), ".tusker-daemon-service-*.plist")
	if err != nil {
		return fmt.Errorf("create daemon service plist: %w", err)
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(0o644); err != nil {
		_ = temp.Close()
		return fmt.Errorf("set daemon service plist permissions: %w", err)
	}
	if _, err := temp.WriteString(content); err != nil {
		_ = temp.Close()
		return fmt.Errorf("write daemon service plist: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close daemon service plist: %w", err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("replace daemon service plist: %w", err)
	}
	return nil
}

// installDaemonServiceExecutable makes launchd independent of a checkout path.
// The service copy is refreshed atomically on every install, so a repo move or
// replacement cannot leave launchd pointing at a disappearing build artifact.
func installDaemonServiceExecutable(config daemonServiceConfig) error {
	info, err := os.Stat(config.SourceExecutable)
	if err != nil {
		return fmt.Errorf("inspect daemon service source executable: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode()&0o111 == 0 {
		return fmt.Errorf("daemon service source is not an executable regular file: %s", config.SourceExecutable)
	}
	if err := ensureDir(config.binaryDir()); err != nil {
		return fmt.Errorf("create daemon service binary directory: %w", err)
	}
	source, err := os.Open(config.SourceExecutable)
	if err != nil {
		return fmt.Errorf("open daemon service source executable: %w", err)
	}
	defer source.Close()
	temp, err := os.CreateTemp(config.binaryDir(), ".tusker-daemon-*")
	if err != nil {
		return fmt.Errorf("create daemon service executable: %w", err)
	}
	tempPath := temp.Name()
	renamed := false
	defer func() {
		_ = temp.Close()
		if !renamed {
			_ = os.Remove(tempPath)
		}
	}()
	if err := temp.Chmod(0o755); err != nil {
		return fmt.Errorf("set daemon service executable permissions: %w", err)
	}
	if _, err := io.Copy(temp, source); err != nil {
		return fmt.Errorf("copy daemon service executable: %w", err)
	}
	if err := temp.Sync(); err != nil {
		return fmt.Errorf("sync daemon service executable: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close daemon service executable: %w", err)
	}
	if err := os.Rename(tempPath, config.Executable); err != nil {
		return fmt.Errorf("replace daemon service executable: %w", err)
	}
	renamed = true
	return nil
}

func daemonServiceHealth(config daemonServiceConfig) (daemonServiceRuntimeHealth, error) {
	store, err := OpenRuntimeStore(config.StateRoot)
	if err != nil {
		return daemonServiceRuntimeHealth{}, fmt.Errorf("open daemon service runtime store: %w", err)
	}
	defer store.Close()
	status, err := store.DaemonStatus()
	if err != nil {
		return daemonServiceRuntimeHealth{}, fmt.Errorf("read daemon service status: %w", err)
	}
	liveness := readDaemonLiveness(config.StateRoot, time.Now().UTC())
	health := daemonServiceRuntimeHealth{Alive: liveness.Alive}
	if raw := strings.TrimSpace(liveness.StartedAt); raw != "" {
		if parsed, err := time.Parse(time.RFC3339Nano, raw); err == nil {
			health.StartedAt = parsed
		}
	}
	if raw := strings.TrimSpace(stringValue(status["daemon_last_poll_at"])); raw != "" {
		if parsed, err := time.Parse(time.RFC3339Nano, raw); err == nil {
			health.LastPollAt = parsed
		}
	}
	return health, nil
}

func daemonServiceProtectedProjects(config daemonServiceConfig) ([]macOSProtectedProject, error) {
	projects, err := daemonServiceRegisteredProjects(config)
	if err != nil {
		return nil, err
	}
	return macOSProtectedProjects(projects, config.Home), nil
}

func daemonServiceRegisteredProjects(config daemonServiceConfig) ([]RegisteredProject, error) {
	store, err := OpenRuntimeStore(config.StateRoot)
	if err != nil {
		return nil, fmt.Errorf("open daemon service runtime store: %w", err)
	}
	defer store.Close()
	loaded, err := loadRegisteredProjects(store, registeredProjectLoadOptions{MetadataOnly: true})
	if err != nil {
		return nil, fmt.Errorf("read registered projects for daemon service: %w", err)
	}
	return loadedRegisteredProjects(loaded), nil
}

func requireDaemonServiceProjectAccess(args Args, config daemonServiceConfig) error {
	projects, err := daemonServiceRegisteredProjects(config)
	if err != nil {
		return err
	}
	protectedProjects := macOSProtectedProjects(projects, config.Home)
	if len(protectedProjects) == 0 || args.Bool("allow-protected-projects") {
		store, err := OpenRuntimeStore(config.StateRoot)
		if err != nil {
			return fmt.Errorf("open daemon service runtime store: %w", err)
		}
		defer store.Close()
		loaded, err := loadRegisteredProjects(store, registeredProjectLoadOptions{})
		if err != nil {
			return fmt.Errorf("load registered projects for daemon service: %w", err)
		}
		for _, item := range loaded {
			project := item.Project
			if !project.Enabled {
				continue
			}
			if err := validateProjectStorageBoundary(project.RepoRoot, project.VaultRoot); err != nil {
				return err
			}
			if item.LoadError != nil {
				return tuskerError(errorConfigInvalid,
					"daemon service cannot start because project "+firstNonEmpty(project.Name, project.ProjectID)+" has an invalid workflow contract: "+item.LoadError.Error(),
					withPath(project.WorkflowPath),
					withHint("repair the repo-local WORKFLOW.md, then retry daemon service install/start"),
					withContext(map[string]any{"project_id": project.ProjectID, "repo_root": project.RepoRoot, "vault_root": project.VaultRoot}))
			}
		}
		return nil
	}
	issue := protectedProjects[0]
	message := fmt.Sprintf("daemon service cannot start because enabled project %s is under macOS-protected %s", firstNonEmpty(issue.Name, issue.ProjectID), issue.Location)
	if len(protectedProjects) > 1 {
		message = fmt.Sprintf("daemon service cannot start because %d enabled projects are under macOS-protected locations", len(protectedProjects))
	}
	return tuskerError(errorInvalidTransition, message,
		withHint("move the repository to ~/Developer or ~/Projects (recommended), or grant Full Disk Access to "+config.Executable+" and retry with --allow-protected-projects"),
		withContext(map[string]any{
			"protected_projects": protectedProjects,
			"service_executable": config.Executable,
			"shared_state_root":  config.StateRoot,
		}))
}

func waitForDaemonServiceReady(config daemonServiceConfig, startedAt time.Time, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		health, err := daemonServiceHealth(config)
		if err == nil && health.readySince(startedAt) {
			return nil
		}
		if time.Now().After(deadline) {
			if err != nil {
				return fmt.Errorf("daemon service failed its startup health check: %w", err)
			}
			return tuskerError(errorInvalidTransition,
				"daemon service started but did not complete a fresh poll within 5s",
				withHint("check `tusker daemon service status`; if protected projects are listed, move them outside the protected folder or grant Full Disk Access and retry with --allow-protected-projects"),
				withContext(map[string]any{"state_root": config.StateRoot, "service_executable": config.Executable}))
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func displayDaemonServiceResult(args Args, payload map[string]any, message string) {
	if args.Bool("json") {
		emitJSON(payload)
		return
	}
	fmt.Println(message)
}
