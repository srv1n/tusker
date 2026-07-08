package main

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	daemonLaunchdLabel             = "com.tusker.daemon"
	daemonLaunchdEnvKey            = "TUSKER_LAUNCHD"
	daemonLaunchdThrottleSeconds   = 10
	daemonCrashLoopReason          = "crash_loop"
	daemonCrashLoopSettingKey      = "daemon_crash_loop_status"
	daemonRestartTimestampsKey     = "daemon_abnormal_restart_timestamps"
	daemonLastRestartCauseKey      = "daemon_last_restart_cause"
	daemonCrashLoopBurst           = 5
	daemonCrashLoopWindowSeconds   = 10 * 60
	daemonRestartCauseStalePID     = "stale_pid"
	daemonRestartCauseRunError     = "run_error"
	daemonRestartCauseWatchdog     = "watchdog_stale"
	daemonRestartCauseCircuitOpen  = "crash_loop_open"
	daemonRestartCauseCleanStartup = "clean_start"
)

var launchctlRun = func(args ...string) error {
	cmd := exec.Command("launchctl", args...)
	output, err := cmd.CombinedOutput()
	if err == nil {
		return nil
	}
	detail := strings.TrimSpace(string(output))
	if detail != "" {
		return fmt.Errorf("launchctl %s: %w: %s", strings.Join(args, " "), err, detail)
	}
	return fmt.Errorf("launchctl %s: %w", strings.Join(args, " "), err)
}

type daemonCrashLoopStatus struct {
	Open             bool     `json:"open"`
	Reason           string   `json:"reason,omitempty"`
	OpenedAt         string   `json:"opened_at,omitempty"`
	LastCheckedAt    string   `json:"last_checked_at,omitempty"`
	LastRestartCause string   `json:"last_restart_cause,omitempty"`
	RestartCount     int      `json:"restart_count"`
	RestartedAt      []string `json:"restarted_at,omitempty"`
	WindowSeconds    int      `json:"window_seconds"`
	Burst            int      `json:"burst"`
	Summary          string   `json:"summary,omitempty"`
}

func daemonLogPath(stateRoot string) string {
	return filepath.Join(stateRoot, "daemon.log")
}

func daemonLaunchdAgentDir() (string, error) {
	home := userHomeDir()
	if strings.TrimSpace(home) == "" {
		return "", tuskerError(errorConfigInvalid, "cannot resolve home directory for launchd agent install")
	}
	return filepath.Join(home, "Library", "LaunchAgents"), nil
}

func daemonLaunchdPlistPath() (string, error) {
	dir, err := daemonLaunchdAgentDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, daemonLaunchdLabel+".plist"), nil
}

func daemonLaunchdDomainTarget() string {
	return fmt.Sprintf("gui/%d", os.Getuid())
}

func daemonLaunchdServiceTarget() string {
	return daemonLaunchdDomainTarget() + "/" + daemonLaunchdLabel
}

func daemonLaunchdInstalled() (bool, string, error) {
	path, err := daemonLaunchdPlistPath()
	if err != nil {
		return false, "", err
	}
	if _, err := os.Stat(path); err == nil {
		return true, path, nil
	} else if os.IsNotExist(err) {
		return false, path, nil
	} else {
		return false, path, err
	}
}

func daemonLaunchdPlist(stateRoot, executable string) string {
	args := []string{executable, "daemon", "run"}
	env := map[string]string{
		daemonLaunchdEnvKey: "1",
		"TUSKER_STATE_ROOT": stateRoot,
	}
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	b.WriteString(`<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">` + "\n")
	b.WriteString(`<plist version="1.0">` + "\n")
	b.WriteString("<dict>\n")
	plistKeyString(&b, "Label", daemonLaunchdLabel)
	plistKeyArray(&b, "ProgramArguments", args)
	b.WriteString("\t<key>RunAtLoad</key>\n\t<true/>\n")
	b.WriteString("\t<key>KeepAlive</key>\n\t<dict>\n\t\t<key>SuccessfulExit</key>\n\t\t<false/>\n\t</dict>\n")
	b.WriteString(fmt.Sprintf("\t<key>ThrottleInterval</key>\n\t<integer>%d</integer>\n", daemonLaunchdThrottleSeconds))
	plistKeyString(&b, "StandardOutPath", daemonLogPath(stateRoot))
	plistKeyString(&b, "StandardErrorPath", daemonLogPath(stateRoot))
	plistKeyDict(&b, "EnvironmentVariables", env)
	b.WriteString("</dict>\n</plist>\n")
	return b.String()
}

func plistKeyString(b *strings.Builder, key, value string) {
	b.WriteString("\t<key>" + plistEscape(key) + "</key>\n")
	b.WriteString("\t<string>" + plistEscape(value) + "</string>\n")
}

func plistKeyArray(b *strings.Builder, key string, values []string) {
	b.WriteString("\t<key>" + plistEscape(key) + "</key>\n")
	b.WriteString("\t<array>\n")
	for _, value := range values {
		b.WriteString("\t\t<string>" + plistEscape(value) + "</string>\n")
	}
	b.WriteString("\t</array>\n")
}

func plistKeyDict(b *strings.Builder, key string, values map[string]string) {
	b.WriteString("\t<key>" + plistEscape(key) + "</key>\n")
	b.WriteString("\t<dict>\n")
	for _, dictKey := range sortedStringMapKeys(values) {
		b.WriteString("\t\t<key>" + plistEscape(dictKey) + "</key>\n")
		b.WriteString("\t\t<string>" + plistEscape(values[dictKey]) + "</string>\n")
	}
	b.WriteString("\t</dict>\n")
}

func sortedStringMapKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func plistEscape(value string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(value))
	return b.String()
}

func daemonRunningUnderLaunchd() bool {
	return strings.TrimSpace(os.Getenv(daemonLaunchdEnvKey)) != ""
}

func daemonStalePIDFileExists(stateRoot string) bool {
	pidFile, ok, err := readDaemonPIDFile(filepath.Join(stateRoot, daemonPIDFileName))
	if err != nil || !ok || pidFile.PID <= 0 {
		return false
	}
	return !processAlive(pidFile.PID)
}

func daemonInstallCmd(args Args) error {
	stateRoot := DefaultStateRoot()
	if err := ensureDir(stateRoot); err != nil {
		return err
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	if resolved, err := filepath.EvalSymlinks(executable); err == nil && strings.TrimSpace(resolved) != "" {
		executable = resolved
	}
	plistPath, err := daemonLaunchdPlistPath()
	if err != nil {
		return err
	}
	if err := ensureDir(filepath.Dir(plistPath)); err != nil {
		return err
	}
	if err := writeText(plistPath, daemonLaunchdPlist(stateRoot, executable)); err != nil {
		return err
	}
	_ = launchctlRun("bootout", daemonLaunchdDomainTarget(), plistPath)
	if err := launchctlRun("bootstrap", daemonLaunchdDomainTarget(), plistPath); err != nil {
		return err
	}
	if err := launchctlRun("kickstart", "-k", daemonLaunchdServiceTarget()); err != nil {
		return err
	}
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": true, "installed": true, "label": daemonLaunchdLabel, "plist": plistPath, "state_root": stateRoot, "log_path": daemonLogPath(stateRoot)})
		return nil
	}
	fmt.Printf("Installed launchd agent %s\n", daemonLaunchdLabel)
	fmt.Printf("Plist: %s\n", plistPath)
	fmt.Printf("Log: %s\n", daemonLogPath(stateRoot))
	return nil
}

func daemonUninstallCmd(args Args) error {
	plistPath, err := daemonLaunchdPlistPath()
	if err != nil {
		return err
	}
	_ = launchctlRun("bootout", daemonLaunchdDomainTarget(), plistPath)
	if err := os.Remove(plistPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": true, "installed": false, "label": daemonLaunchdLabel, "plist": plistPath})
		return nil
	}
	fmt.Printf("Uninstalled launchd agent %s\n", daemonLaunchdLabel)
	return nil
}

func (s *RuntimeStore) ReadCrashLoopStatus() (daemonCrashLoopStatus, error) {
	raw, err := s.GetSetting(daemonCrashLoopSettingKey)
	if err != nil {
		return daemonCrashLoopStatus{}, err
	}
	status := defaultCrashLoopStatus()
	if strings.TrimSpace(raw) == "" {
		return status, nil
	}
	if err := json.Unmarshal([]byte(raw), &status); err != nil {
		return daemonCrashLoopStatus{}, err
	}
	status.WindowSeconds = daemonCrashLoopWindowSeconds
	status.Burst = daemonCrashLoopBurst
	if status.Open && strings.TrimSpace(status.Reason) == "" {
		status.Reason = daemonCrashLoopReason
	}
	return status, nil
}

func (s *RuntimeStore) SetCrashLoopStatus(status daemonCrashLoopStatus) error {
	status.WindowSeconds = daemonCrashLoopWindowSeconds
	status.Burst = daemonCrashLoopBurst
	if status.Open && strings.TrimSpace(status.Reason) == "" {
		status.Reason = daemonCrashLoopReason
	}
	raw, err := json.Marshal(status)
	if err != nil {
		return err
	}
	return s.SetSetting(daemonCrashLoopSettingKey, string(raw))
}

func (s *RuntimeStore) ClearCrashLoopStatus(checkedAt string) error {
	if err := s.SetSetting(daemonRestartTimestampsKey, "[]"); err != nil {
		return err
	}
	return s.SetCrashLoopStatus(daemonCrashLoopStatus{
		Open:          false,
		Reason:        daemonCrashLoopReason,
		LastCheckedAt: checkedAt,
		WindowSeconds: daemonCrashLoopWindowSeconds,
		Burst:         daemonCrashLoopBurst,
		Summary:       "crash loop circuit closed by operator",
	})
}

func defaultCrashLoopStatus() daemonCrashLoopStatus {
	return daemonCrashLoopStatus{
		Open:          false,
		Reason:        daemonCrashLoopReason,
		WindowSeconds: daemonCrashLoopWindowSeconds,
		Burst:         daemonCrashLoopBurst,
	}
}

func (s *RuntimeStore) recordDaemonAbnormalStart(cause string, now time.Time) (daemonCrashLoopStatus, error) {
	cause = firstNonEmpty(strings.TrimSpace(cause), "abnormal_exit")
	restarts, err := s.readDaemonRestartTimestamps(now)
	if err != nil {
		return daemonCrashLoopStatus{}, err
	}
	restarts = append(restarts, now.UTC().Format(time.RFC3339Nano))
	if err := s.writeDaemonRestartTimestamps(restarts); err != nil {
		return daemonCrashLoopStatus{}, err
	}
	if err := s.SetSetting(daemonLastRestartCauseKey, cause); err != nil {
		return daemonCrashLoopStatus{}, err
	}
	status, err := s.ReadCrashLoopStatus()
	if err != nil {
		return daemonCrashLoopStatus{}, err
	}
	status.LastCheckedAt = now.UTC().Format(time.RFC3339Nano)
	status.LastRestartCause = cause
	status.RestartCount = len(restarts)
	status.RestartedAt = append([]string{}, restarts...)
	status.WindowSeconds = daemonCrashLoopWindowSeconds
	status.Burst = daemonCrashLoopBurst
	if len(restarts) > daemonCrashLoopBurst {
		status.Open = true
		status.Reason = daemonCrashLoopReason
		if strings.TrimSpace(status.OpenedAt) == "" {
			status.OpenedAt = now.UTC().Format(time.RFC3339Nano)
		}
		status.Summary = fmt.Sprintf("%s: %d abnormal daemon starts within %s; run `tusker daemon resume` after repair", daemonCrashLoopReason, len(restarts), (time.Duration(daemonCrashLoopWindowSeconds) * time.Second).String())
	}
	if err := s.SetCrashLoopStatus(status); err != nil {
		return daemonCrashLoopStatus{}, err
	}
	return status, nil
}

func (s *RuntimeStore) readDaemonRestartTimestamps(now time.Time) ([]string, error) {
	raw, err := s.GetSetting(daemonRestartTimestampsKey)
	if err != nil {
		return nil, err
	}
	var stored []string
	if strings.TrimSpace(raw) != "" {
		if err := json.Unmarshal([]byte(raw), &stored); err != nil {
			return nil, err
		}
	}
	cutoff := now.UTC().Add(-time.Duration(daemonCrashLoopWindowSeconds) * time.Second)
	var filtered []string
	for _, stamp := range stored {
		parsed, err := time.Parse(time.RFC3339Nano, stamp)
		if err != nil {
			continue
		}
		if !parsed.Before(cutoff) {
			filtered = append(filtered, parsed.UTC().Format(time.RFC3339Nano))
		}
	}
	return filtered, nil
}

func (s *RuntimeStore) writeDaemonRestartTimestamps(stamps []string) error {
	raw, err := json.Marshal(stamps)
	if err != nil {
		return err
	}
	return s.SetSetting(daemonRestartTimestampsKey, string(raw))
}

func (d *Daemon) crashLoopDispatchBlocker() (string, error) {
	if d == nil || d.store == nil {
		return "", nil
	}
	status, err := d.store.ReadCrashLoopStatus()
	if err != nil {
		return "", err
	}
	if !status.Open {
		return "", nil
	}
	return "daemon circuit open: " + firstNonEmpty(status.Summary, daemonCrashLoopReason), nil
}

func (d *Daemon) ResumeCrashLoopCircuit() (daemonCrashLoopStatus, bool, error) {
	if d == nil || d.store == nil {
		return daemonCrashLoopStatus{}, false, nil
	}
	status, err := d.store.ReadCrashLoopStatus()
	if err != nil {
		return daemonCrashLoopStatus{}, false, err
	}
	if !status.Open {
		return status, false, nil
	}
	checkedAt := time.Now().UTC().Format(time.RFC3339Nano)
	if err := d.store.ClearCrashLoopStatus(checkedAt); err != nil {
		return daemonCrashLoopStatus{}, false, err
	}
	closed, err := d.store.ReadCrashLoopStatus()
	return closed, true, err
}
