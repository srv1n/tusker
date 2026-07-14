package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	daemonLaunchdLabel             = "com.tusker.daemon"
	daemonLaunchdEnvKey            = "TUSKER_LAUNCHD"
	daemonCrashLoopReason          = "crash_loop"
	daemonCrashLoopSettingKey      = "daemon_crash_loop_status"
	daemonRestartTimestampsKey     = "daemon_abnormal_restart_timestamps"
	daemonLastRestartCauseKey      = "daemon_last_restart_cause"
	daemonPendingRestartCauseKey   = "daemon_pending_abnormal_restart_cause"
	daemonCorruptRestartHistoryKey = "daemon_corrupt_abnormal_restart_timestamps"
	daemonCorruptCrashStatusKey    = "daemon_corrupt_crash_loop_status"
	daemonCrashLoopBurst           = 5
	daemonCrashLoopWindowSeconds   = 10 * 60
	daemonRestartCauseStalePID     = "stale_pid"
	daemonRestartCauseRunError     = "run_error"
	daemonRestartCauseWatchdog     = "watchdog_stale"
	daemonRestartCauseCircuitOpen  = "crash_loop_open"
	daemonRestartCauseCleanStartup = "clean_start"
)

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

// beginManagedDaemonStart consumes exactly one abnormal predecessor marker.
// A graceful run error can persist its cause before guard cleanup removes the
// pid file; SIGKILL is inferred from the stale pid file. Manual runs do not
// participate in the launchd restart circuit.
func (s *RuntimeStore) beginManagedDaemonStart(stalePID bool, now time.Time) (daemonCrashLoopStatus, error) {
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return daemonCrashLoopStatus{}, err
	}
	defer tx.Rollback()
	setting := func(key string) (string, error) {
		var value string
		err := tx.QueryRow(`SELECT value FROM daemon_settings WHERE key = ?`, key).Scan(&value)
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return value, err
	}
	setSetting := func(key, value string) error {
		_, err := tx.Exec(`INSERT INTO daemon_settings (key, value) VALUES (?, ?)
			ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
		return err
	}

	status := defaultCrashLoopStatus()
	quarantined := map[string]string{}
	rawStatus, err := setting(daemonCrashLoopSettingKey)
	if err != nil {
		return daemonCrashLoopStatus{}, err
	}
	if strings.TrimSpace(rawStatus) != "" {
		if err := json.Unmarshal([]byte(rawStatus), &status); err != nil {
			quarantined[daemonCorruptCrashStatusKey] = rawStatus
			status = defaultCrashLoopStatus()
		}
	}
	status.WindowSeconds = daemonCrashLoopWindowSeconds
	status.Burst = daemonCrashLoopBurst
	pending, err := setting(daemonPendingRestartCauseKey)
	if err != nil {
		return daemonCrashLoopStatus{}, err
	}
	causes := decodePendingRestartCauses(pending)
	if len(causes) == 0 && stalePID {
		causes = append(causes, daemonRestartCauseStalePID)
	}
	if len(causes) == 0 {
		if err := setSetting(daemonLastRestartCauseKey, daemonRestartCauseCleanStartup); err != nil {
			return daemonCrashLoopStatus{}, err
		}
		for key, value := range quarantined {
			if err := setSetting(key, value); err != nil {
				return daemonCrashLoopStatus{}, err
			}
		}
		if err := tx.Commit(); err != nil {
			return daemonCrashLoopStatus{}, err
		}
		return status, nil
	}

	rawRestarts, err := setting(daemonRestartTimestampsKey)
	if err != nil {
		return daemonCrashLoopStatus{}, err
	}
	var stored []string
	if strings.TrimSpace(rawRestarts) != "" {
		if err := json.Unmarshal([]byte(rawRestarts), &stored); err != nil {
			quarantined[daemonCorruptRestartHistoryKey] = rawRestarts
			stored = nil
		}
	}
	cutoff := now.UTC().Add(-time.Duration(daemonCrashLoopWindowSeconds) * time.Second)
	restarts := make([]string, 0, len(stored)+1)
	for _, stamp := range stored {
		parsed, err := time.Parse(time.RFC3339Nano, stamp)
		if err == nil && !parsed.Before(cutoff) {
			restarts = append(restarts, parsed.UTC().Format(time.RFC3339Nano))
		}
	}
	for range causes {
		restarts = append(restarts, now.UTC().Format(time.RFC3339Nano))
	}
	cause := causes[len(causes)-1]
	status.LastCheckedAt = now.UTC().Format(time.RFC3339Nano)
	status.LastRestartCause = cause
	status.RestartCount = len(restarts)
	status.RestartedAt = append([]string{}, restarts...)
	if len(restarts) > daemonCrashLoopBurst {
		status.Open = true
		status.Reason = daemonCrashLoopReason
		if strings.TrimSpace(status.OpenedAt) == "" {
			status.OpenedAt = now.UTC().Format(time.RFC3339Nano)
		}
		status.Summary = fmt.Sprintf("%s: %d abnormal daemon starts within %s; run `tusker daemon resume` after repair", daemonCrashLoopReason, len(restarts), (time.Duration(daemonCrashLoopWindowSeconds) * time.Second).String())
	}
	restartsJSON, err := json.Marshal(restarts)
	if err != nil {
		return daemonCrashLoopStatus{}, err
	}
	statusJSON, err := json.Marshal(status)
	if err != nil {
		return daemonCrashLoopStatus{}, err
	}
	updates := map[string]string{
		daemonRestartTimestampsKey:   string(restartsJSON),
		daemonLastRestartCauseKey:    cause,
		daemonCrashLoopSettingKey:    string(statusJSON),
		daemonPendingRestartCauseKey: "",
	}
	for key, value := range quarantined {
		updates[key] = value
	}
	for key, value := range updates {
		if err := setSetting(key, value); err != nil {
			return daemonCrashLoopStatus{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return daemonCrashLoopStatus{}, err
	}
	return status, nil
}

func (s *RuntimeStore) markManagedDaemonAbnormalExit(cause string) error {
	cause = firstNonEmpty(strings.TrimSpace(cause), daemonRestartCauseRunError)
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var pending string
	if err := tx.QueryRow(`SELECT value FROM daemon_settings WHERE key = ?`, daemonPendingRestartCauseKey).Scan(&pending); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	causes := append(decodePendingRestartCauses(pending), cause)
	pendingJSON, err := json.Marshal(causes)
	if err != nil {
		return err
	}
	for key, value := range map[string]string{
		daemonLastRestartCauseKey:    cause,
		daemonPendingRestartCauseKey: string(pendingJSON),
	} {
		if _, err := tx.Exec(`INSERT INTO daemon_settings (key, value) VALUES (?, ?)
			ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func decodePendingRestartCauses(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var causes []string
	if json.Unmarshal([]byte(raw), &causes) != nil {
		causes = []string{raw}
	}
	filtered := causes[:0]
	for _, cause := range causes {
		if cause = strings.TrimSpace(cause); cause != "" {
			filtered = append(filtered, cause)
		}
	}
	return filtered
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
