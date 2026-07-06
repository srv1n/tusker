package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	daemonLockFileName = "daemon.lock"
	daemonPIDFileName  = "daemon.pid"
)

type daemonGuard struct {
	stateRoot string
	lockPath  string
	pidPath   string
	lockFile  *os.File
	released  bool
}

type daemonPIDFile struct {
	PID       int    `json:"pid"`
	StartedAt string `json:"started_at"`
	StateRoot string `json:"state_root,omitempty"`
}

type daemonLiveness struct {
	PID              int
	StartedAt        string
	UptimeSeconds    int64
	Alive            bool
	RuntimeStorePath string
}

type daemonAlreadyRunningError struct {
	PID       int
	StateRoot string
}

func (e *daemonAlreadyRunningError) Error() string {
	if e.PID > 0 {
		return fmt.Sprintf("daemon already running with pid %d", e.PID)
	}
	return "daemon already running"
}

func acquireDaemonGuard(stateRoot string) (*daemonGuard, error) {
	if err := ensureDir(stateRoot); err != nil {
		return nil, err
	}
	lockPath := filepath.Join(stateRoot, daemonLockFileName)
	pidPath := filepath.Join(stateRoot, daemonPIDFileName)
	lockFile, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(lockFile.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = lockFile.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			pidFile, _, _ := readDaemonPIDFile(pidPath)
			return nil, &daemonAlreadyRunningError{PID: pidFile.PID, StateRoot: stateRoot}
		}
		return nil, err
	}
	guard := &daemonGuard{stateRoot: stateRoot, lockPath: lockPath, pidPath: pidPath, lockFile: lockFile}
	if err := guard.writePIDFile(time.Now().UTC()); err != nil {
		_ = guard.Close()
		return nil, err
	}
	return guard, nil
}

func (g *daemonGuard) writePIDFile(startedAt time.Time) error {
	pidFile := daemonPIDFile{
		PID:       os.Getpid(),
		StartedAt: startedAt.Format(time.RFC3339Nano),
		StateRoot: g.stateRoot,
	}
	data, err := json.MarshalIndent(pidFile, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmpPath := g.pidPath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, g.pidPath); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return nil
}

func (g *daemonGuard) Close() error {
	if g == nil || g.released {
		return nil
	}
	g.released = true
	var firstErr error
	if err := os.Remove(g.pidPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		firstErr = err
	}
	if g.lockFile != nil {
		if err := syscall.Flock(int(g.lockFile.Fd()), syscall.LOCK_UN); err != nil && firstErr == nil {
			firstErr = err
		}
		if err := g.lockFile.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func readDaemonLiveness(stateRoot string, now time.Time) daemonLiveness {
	status := daemonLiveness{RuntimeStorePath: runtimeStoreDBPath(stateRoot)}
	pidFile, ok, err := readDaemonPIDFile(filepath.Join(stateRoot, daemonPIDFileName))
	if err != nil || !ok || pidFile.PID <= 0 || !processAlive(pidFile.PID) {
		return status
	}
	status.PID = pidFile.PID
	status.StartedAt = pidFile.StartedAt
	status.Alive = true
	if startedAt, err := time.Parse(time.RFC3339Nano, pidFile.StartedAt); err == nil {
		uptime := now.Sub(startedAt)
		if uptime < 0 {
			uptime = 0
		}
		status.UptimeSeconds = int64(uptime / time.Second)
	}
	return status
}

func readDaemonPIDFile(path string) (daemonPIDFile, bool, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return daemonPIDFile{}, false, nil
	}
	if err != nil {
		return daemonPIDFile{}, false, err
	}
	var pidFile daemonPIDFile
	if err := json.Unmarshal(raw, &pidFile); err == nil {
		return pidFile, true, nil
	}
	if pid, err := strconv.Atoi(strings.TrimSpace(string(raw))); err == nil {
		return daemonPIDFile{PID: pid}, true, nil
	}
	return daemonPIDFile{}, true, fmt.Errorf("invalid daemon pid file: %s", path)
}

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
