package main

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	defaultDiskPressureMinFreeBytes   uint64  = 2 << 30
	defaultDiskPressureMinFreePercent float64 = 1

	diskPressureConfigSettingKey = "disk_pressure_config"
	diskPressureStatusSettingKey = "disk_pressure_status"
	diskPressureErrorPrefix      = "dispatch blocked: disk_pressure:"
	diskPressureObservationTTL   = 5 * time.Minute
)

type DiskPressureConfig struct {
	Enabled        bool    `json:"enabled"`
	MinFreeBytes   uint64  `json:"min_free_bytes"`
	MinFreePercent float64 `json:"min_free_percent"`
	Source         string  `json:"source"`
}

type DiskPressureFilesystem struct {
	Kind                    string  `json:"kind"`
	Path                    string  `json:"path"`
	FilesystemPath          string  `json:"filesystem_path"`
	FilesystemID            string  `json:"filesystem_id,omitempty"`
	AvailableBytes          uint64  `json:"available_bytes"`
	AvailablePercent        float64 `json:"available_percent"`
	TotalBytes              uint64  `json:"total_bytes"`
	EffectiveThresholdBytes uint64  `json:"effective_threshold_bytes"`
	WarningThresholdBytes   uint64  `json:"warning_threshold_bytes"`
	State                   string  `json:"state"`
	CheckedAt               string  `json:"checked_at,omitempty"`
	Error                   string  `json:"error,omitempty"`
}

type DiskPressureStatus struct {
	State                   string                   `json:"state"`
	Enabled                 bool                     `json:"enabled"`
	DispatchPaused          bool                     `json:"dispatch_paused"`
	Warning                 bool                     `json:"warning"`
	Recovered               bool                     `json:"recovered"`
	CheckedAt               string                   `json:"checked_at,omitempty"`
	Reason                  string                   `json:"reason,omitempty"`
	MinFreeBytes            uint64                   `json:"min_free_bytes"`
	MinFreePercent          float64                  `json:"min_free_percent"`
	EffectiveThresholdBytes uint64                   `json:"effective_threshold_bytes"`
	WarningThresholdBytes   uint64                   `json:"warning_threshold_bytes"`
	Filesystems             []DiskPressureFilesystem `json:"filesystems"`
	Config                  DiskPressureConfig       `json:"config"`
}

type diskPressurePath struct {
	Kind string
	Path string
}

type diskFilesystemStat struct {
	Blocks          uint64
	AvailableBlocks uint64
	BlockSize       uint64
	FilesystemID    string
}

type diskStatFunc func(path string) (diskFilesystemStat, error)

func defaultDiskPressureConfig() DiskPressureConfig {
	return DiskPressureConfig{
		Enabled:        true,
		MinFreeBytes:   defaultDiskPressureMinFreeBytes,
		MinFreePercent: defaultDiskPressureMinFreePercent,
		Source:         "default",
	}
}

func diskPressureStatusFromAny(value any) DiskPressureStatus {
	if status, ok := value.(DiskPressureStatus); ok {
		return status
	}
	config := defaultDiskPressureConfig()
	status := DiskPressureStatus{
		State:          "unknown",
		Enabled:        config.Enabled,
		MinFreeBytes:   config.MinFreeBytes,
		MinFreePercent: config.MinFreePercent,
		Filesystems:    []DiskPressureFilesystem{},
		Config:         config,
	}
	if value == nil {
		return status
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return status
	}
	var decoded DiskPressureStatus
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return status
	}
	if decoded.Filesystems == nil {
		decoded.Filesystems = []DiskPressureFilesystem{}
	}
	return decoded
}

func validateDiskPressureConfig(config DiskPressureConfig) error {
	if math.IsNaN(config.MinFreePercent) || math.IsInf(config.MinFreePercent, 0) || config.MinFreePercent < 0 || config.MinFreePercent > 100 {
		return tuskerError(errorConfigInvalid, "disk pressure min_free_percent must be between 0 and 100")
	}
	return nil
}

func (s *RuntimeStore) DiskPressureConfig() (DiskPressureConfig, error) {
	config := defaultDiskPressureConfig()
	if s == nil {
		return config, tuskerError(errorConfigInvalid, "runtime store is required")
	}
	raw, err := s.GetSetting(diskPressureConfigSettingKey)
	if err != nil {
		return config, err
	}
	if strings.TrimSpace(raw) == "" {
		return config, nil
	}
	if err := json.Unmarshal([]byte(raw), &config); err != nil {
		return config, tuskerError(errorConfigInvalid, "daemon disk pressure setting is invalid: "+err.Error())
	}
	config.Source = "runtime"
	if err := validateDiskPressureConfig(config); err != nil {
		return config, err
	}
	return config, nil
}

func (s *RuntimeStore) SetDiskPressureConfig(config DiskPressureConfig) error {
	if s == nil {
		return tuskerError(errorConfigInvalid, "runtime store is required")
	}
	if err := validateDiskPressureConfig(config); err != nil {
		return err
	}
	config.Source = "runtime"
	raw, err := json.Marshal(config)
	if err != nil {
		return err
	}
	if err := s.SetSetting(diskPressureConfigSettingKey, string(raw)); err != nil {
		return err
	}
	state := "unknown"
	if !config.Enabled {
		state = "disabled"
	}
	return s.writeDiskPressureStatus(DiskPressureStatus{
		State:          state,
		Enabled:        config.Enabled,
		MinFreeBytes:   config.MinFreeBytes,
		MinFreePercent: config.MinFreePercent,
		Filesystems:    []DiskPressureFilesystem{},
		Config:         config,
	})
}

func (s *RuntimeStore) DiskPressureStatus() (DiskPressureStatus, error) {
	config, err := s.DiskPressureConfig()
	if err != nil {
		return DiskPressureStatus{}, err
	}
	status := DiskPressureStatus{
		State:          "unknown",
		Enabled:        config.Enabled,
		MinFreeBytes:   config.MinFreeBytes,
		MinFreePercent: config.MinFreePercent,
		Filesystems:    []DiskPressureFilesystem{},
		Config:         config,
	}
	if !config.Enabled {
		status.State = "disabled"
	}
	raw, err := s.GetSetting(diskPressureStatusSettingKey)
	if err != nil || strings.TrimSpace(raw) == "" {
		return status, err
	}
	var persisted DiskPressureStatus
	if err := json.Unmarshal([]byte(raw), &persisted); err != nil {
		return DiskPressureStatus{}, tuskerError(errorConfigInvalid, "daemon disk pressure status is invalid: "+err.Error())
	}
	if !sameDiskPressureConfig(persisted.Config, config) {
		return status, nil
	}
	status = persisted
	status.Enabled = config.Enabled
	status.MinFreeBytes = config.MinFreeBytes
	status.MinFreePercent = config.MinFreePercent
	status.Config = config
	if status.Filesystems == nil {
		status.Filesystems = []DiskPressureFilesystem{}
	}
	if !config.Enabled {
		status.State = "disabled"
		status.DispatchPaused = false
		status.Warning = false
		status.Recovered = false
		status.Reason = ""
	}
	fresh := make([]DiskPressureFilesystem, 0, len(status.Filesystems))
	for _, observation := range status.Filesystems {
		if diskPressureObservationFresh(observation, time.Now().UTC()) {
			fresh = append(fresh, observation)
		}
	}
	if len(fresh) != len(status.Filesystems) {
		status.Filesystems = fresh
		if len(fresh) == 0 {
			status.State = "unknown"
			status.DispatchPaused = false
			status.Warning = false
			status.Recovered = false
			status.Reason = "disk pressure measurements are stale"
		} else {
			status = summarizeDiskPressureStatus(status)
		}
	}
	return status, nil
}

func sameDiskPressureConfig(left, right DiskPressureConfig) bool {
	return left.Enabled == right.Enabled && left.MinFreeBytes == right.MinFreeBytes && left.MinFreePercent == right.MinFreePercent
}

func (s *RuntimeStore) writeDiskPressureStatus(status DiskPressureStatus) error {
	raw, err := json.Marshal(status)
	if err != nil {
		return err
	}
	return s.SetSetting(diskPressureStatusSettingKey, string(raw))
}

func defaultDiskStat(path string) (diskFilesystemStat, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return diskFilesystemStat{}, err
	}
	if stat.Bsize <= 0 {
		return diskFilesystemStat{}, fmt.Errorf("filesystem reported invalid block size %d", stat.Bsize)
	}
	return diskFilesystemStat{
		Blocks:          uint64(stat.Blocks),
		AvailableBlocks: uint64(stat.Bavail),
		BlockSize:       uint64(stat.Bsize),
		FilesystemID:    fmt.Sprintf("fsid:%v", stat.Fsid),
	}, nil
}

func nearestExistingDiskPath(path string) (string, error) {
	abs, err := filepath.Abs(strings.TrimSpace(path))
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(abs); err == nil {
			if resolved, resolveErr := filepath.EvalSymlinks(abs); resolveErr == nil {
				return filepath.Clean(resolved), nil
			}
			return filepath.Clean(abs), nil
		} else if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(abs)
		if parent == abs {
			return "", fmt.Errorf("no existing ancestor for %s", path)
		}
		abs = parent
	}
}

func saturatingMultiply(left, right uint64) uint64 {
	if left == 0 || right == 0 {
		return 0
	}
	maxUint64 := ^uint64(0)
	if left > maxUint64/right {
		return maxUint64
	}
	return left * right
}

func saturatingDouble(value uint64) uint64 {
	maxUint64 := ^uint64(0)
	if value > maxUint64/2 {
		return maxUint64
	}
	return value * 2
}

func diskPressurePercentBytes(total uint64, percent float64) uint64 {
	if total == 0 || percent <= 0 {
		return 0
	}
	value := math.Ceil(float64(total) * percent / 100)
	if value >= float64(^uint64(0)) {
		return ^uint64(0)
	}
	return uint64(value)
}

func evaluateDiskPressure(config DiskPressureConfig, paths []diskPressurePath, statFn diskStatFunc, now time.Time) DiskPressureStatus {
	status := DiskPressureStatus{
		State:          "ok",
		Enabled:        config.Enabled,
		CheckedAt:      now.UTC().Format(time.RFC3339Nano),
		MinFreeBytes:   config.MinFreeBytes,
		MinFreePercent: config.MinFreePercent,
		Filesystems:    []DiskPressureFilesystem{},
		Config:         config,
	}
	if !config.Enabled {
		status.State = "disabled"
		return status
	}
	if statFn == nil {
		statFn = defaultDiskStat
	}
	for _, target := range paths {
		observation := DiskPressureFilesystem{Kind: target.Kind, Path: filepath.Clean(target.Path), State: "ok", CheckedAt: now.UTC().Format(time.RFC3339Nano)}
		filesystemPath, err := nearestExistingDiskPath(target.Path)
		if err == nil {
			observation.FilesystemPath = filesystemPath
			var stat diskFilesystemStat
			stat, err = statFn(filesystemPath)
			if err == nil {
				observation.FilesystemID = strings.TrimSpace(stat.FilesystemID)
				observation.TotalBytes = saturatingMultiply(stat.Blocks, stat.BlockSize)
				observation.AvailableBytes = saturatingMultiply(stat.AvailableBlocks, stat.BlockSize)
				if stat.Blocks > 0 {
					observation.AvailablePercent = float64(stat.AvailableBlocks) * 100 / float64(stat.Blocks)
				}
				percentFloor := diskPressurePercentBytes(observation.TotalBytes, config.MinFreePercent)
				observation.EffectiveThresholdBytes = config.MinFreeBytes
				if percentFloor > observation.EffectiveThresholdBytes {
					observation.EffectiveThresholdBytes = percentFloor
				}
				observation.WarningThresholdBytes = saturatingDouble(observation.EffectiveThresholdBytes)
				switch {
				case observation.AvailableBytes < observation.EffectiveThresholdBytes:
					observation.State = "paused"
				case observation.AvailableBytes < observation.WarningThresholdBytes:
					observation.State = "warning"
				}
			}
		}
		if err != nil {
			observation.State = "error"
			observation.Error = err.Error()
		}
		status.Filesystems = append(status.Filesystems, observation)
	}
	return summarizeDiskPressureStatus(status)
}

func summarizeDiskPressureStatus(status DiskPressureStatus) DiskPressureStatus {
	if !status.Enabled {
		status.State = "disabled"
		status.DispatchPaused = false
		status.Warning = false
		status.Recovered = false
		status.Reason = ""
		return status
	}
	status.State = "ok"
	status.DispatchPaused = false
	status.Warning = false
	status.Recovered = false
	status.Reason = ""
	status.EffectiveThresholdBytes = 0
	status.WarningThresholdBytes = 0
	for _, observation := range status.Filesystems {
		if observation.EffectiveThresholdBytes > status.EffectiveThresholdBytes {
			status.EffectiveThresholdBytes = observation.EffectiveThresholdBytes
		}
		if observation.WarningThresholdBytes > status.WarningThresholdBytes {
			status.WarningThresholdBytes = observation.WarningThresholdBytes
		}
		if observation.State == "warning" || observation.State == "paused" {
			status.Warning = true
		}
		if observation.State == "paused" || observation.State == "error" {
			status.DispatchPaused = true
		}
	}
	for _, observation := range status.Filesystems {
		if observation.State == "error" {
			status.State = "error"
			status.Reason = fmt.Sprintf("cannot measure %s filesystem %s: %s", observation.Kind, firstNonEmpty(observation.FilesystemPath, observation.Path), observation.Error)
			return status
		}
	}
	for _, observation := range status.Filesystems {
		if observation.State == "paused" {
			status.State = "paused"
			status.Reason = fmt.Sprintf("%s filesystem %s has %d bytes (%.2f%%) available, below effective threshold %d bytes", observation.Kind, observation.FilesystemPath, observation.AvailableBytes, observation.AvailablePercent, observation.EffectiveThresholdBytes)
			return status
		}
	}
	for _, observation := range status.Filesystems {
		if observation.State == "warning" {
			status.State = "warning"
			status.Reason = fmt.Sprintf("%s filesystem %s has %d bytes (%.2f%%) available, below warning threshold %d bytes", observation.Kind, observation.FilesystemPath, observation.AvailableBytes, observation.AvailablePercent, observation.WarningThresholdBytes)
			return status
		}
	}
	return status
}

func diskPressureObservationFresh(observation DiskPressureFilesystem, now time.Time) bool {
	checkedAt, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(observation.CheckedAt))
	if err != nil {
		return false
	}
	age := now.Sub(checkedAt)
	return age >= -time.Minute && age <= diskPressureObservationTTL
}

func mergeDiskPressureFilesystems(previous, current []DiskPressureFilesystem, now time.Time) []DiskPressureFilesystem {
	merged := make([]DiskPressureFilesystem, 0, len(previous)+len(current))
	for _, observation := range previous {
		if !diskPressureObservationFresh(observation, now) {
			continue
		}
		merged = append(merged, observation)
	}
	indexes := make(map[string]int, len(merged))
	for index, observation := range merged {
		indexes[diskPressureFilesystemKey(observation)] = index
	}
	for _, observation := range current {
		key := diskPressureFilesystemKey(observation)
		if index, ok := indexes[key]; ok {
			merged[index] = observation
			continue
		}
		indexes[key] = len(merged)
		merged = append(merged, observation)
	}
	return merged
}

func diskPressureFilesystemKey(observation DiskPressureFilesystem) string {
	if identity := strings.TrimSpace(observation.FilesystemID); identity != "" {
		return "filesystem\x00" + identity
	}
	return observation.Kind + "\x00" + filepath.Clean(observation.Path)
}

func diskPressureBlockingObservationsFreshlyRemeasured(previous, current []DiskPressureFilesystem) bool {
	currentByFilesystem := make(map[string]DiskPressureFilesystem, len(current))
	for _, observation := range current {
		currentByFilesystem[diskPressureFilesystemKey(observation)] = observation
	}
	foundBlocking := false
	for _, observation := range previous {
		if observation.State != "paused" && observation.State != "error" {
			continue
		}
		foundBlocking = true
		fresh, ok := currentByFilesystem[diskPressureFilesystemKey(observation)]
		if !ok || fresh.State == "paused" || fresh.State == "error" {
			return false
		}
	}
	return foundBlocking
}

func (s *RuntimeStore) mergeAndWriteDiskPressureStatus(decision DiskPressureStatus, now time.Time) (DiskPressureStatus, error) {
	const maxCASAttempts = 64
	for attempt := 0; attempt < maxCASAttempts; attempt++ {
		raw, err := s.GetSetting(diskPressureStatusSettingKey)
		if err != nil {
			return decision, err
		}
		var previous DiskPressureStatus
		if strings.TrimSpace(raw) != "" {
			if err := json.Unmarshal([]byte(raw), &previous); err != nil {
				return decision, tuskerError(errorConfigInvalid, "daemon disk pressure status is invalid: "+err.Error())
			}
		}

		status := decision
		if sameDiskPressureConfig(previous.Config, decision.Config) {
			status.Filesystems = mergeDiskPressureFilesystems(previous.Filesystems, decision.Filesystems, now)
			status = summarizeDiskPressureStatus(status)
			if previous.DispatchPaused && status.State == "ok" && diskPressureBlockingObservationsFreshlyRemeasured(previous.Filesystems, decision.Filesystems) {
				status.State = "recovered"
				status.Recovered = true
			}
		}
		encoded, err := json.Marshal(status)
		if err != nil {
			return status, err
		}

		var result interface{ RowsAffected() (int64, error) }
		if strings.TrimSpace(raw) == "" {
			result, err = s.exec(`INSERT INTO daemon_settings (key, value) VALUES (?, ?) ON CONFLICT(key) DO NOTHING`, diskPressureStatusSettingKey, string(encoded))
		} else {
			result, err = s.exec(`UPDATE daemon_settings SET value = ? WHERE key = ? AND value = ?`, string(encoded), diskPressureStatusSettingKey, raw)
		}
		if err != nil {
			return status, err
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return status, err
		}
		if affected == 1 {
			return status, nil
		}
	}
	return decision, tuskerError("CAS_CONFLICT", "disk pressure status changed too frequently; retry dispatch")
}

func (d *Daemon) checkDiskPressureForDispatch(workspacePath string) (DiskPressureStatus, error) {
	if d == nil || d.store == nil {
		return DiskPressureStatus{}, tuskerError(errorConfigInvalid, "runtime store is required for disk pressure check")
	}
	config, err := d.store.DiskPressureConfig()
	if err != nil {
		return DiskPressureStatus{}, err
	}
	statFn := d.diskStat
	if statFn == nil {
		statFn = defaultDiskStat
	}
	now := time.Now().UTC()
	decision := evaluateDiskPressure(config, []diskPressurePath{
		{Kind: "state_root", Path: d.stateRoot},
		{Kind: "workspace", Path: workspacePath},
	}, statFn, now)
	status, err := d.store.mergeAndWriteDiskPressureStatus(decision, now)
	if err != nil {
		return status, err
	}
	if status.Recovered && !status.DispatchPaused {
		decision.State = "recovered"
		decision.Recovered = true
	}
	return decision, nil
}

func diskPressureDispatchReason(status DiskPressureStatus) string {
	reason := strings.TrimSpace(status.Reason)
	if reason == "" {
		reason = "configured free-space floor is not satisfied"
	}
	return diskPressureErrorPrefix + " " + reason
}

func isDiskPressureDispatchReason(reason string) bool {
	return strings.HasPrefix(strings.TrimSpace(reason), diskPressureErrorPrefix)
}

func parseDiskPressureEnabled(raw string) (bool, error) {
	enabled, err := strconv.ParseBool(strings.TrimSpace(raw))
	if err != nil {
		return false, tuskerError(errorInvalidArg, "--disk-pressure-enabled must be true or false")
	}
	return enabled, nil
}
