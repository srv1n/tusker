package main

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

type Event struct {
	Seq       int            `json:"seq"`
	At        string         `json:"at"`
	AttemptID string         `json:"attempt_id"`
	Runner    RunnerName     `json:"runner"`
	Kind      string         `json:"kind"`
	Payload   map[string]any `json:"payload,omitempty"`
}

type EventLog struct {
	path string

	flockFn        func(int, int) error
	writeFn        func(*os.File, []byte) (int, error)
	syncFn         func(*os.File) error
	fullValidateFn func(*os.File, string) (eventLogValidation, error)
}

const (
	eventLogSequenceMetadataVersion = 1
	eventLogSequenceMetadataMaxSize = 64 << 10
)

type eventLogValidation struct {
	Found        bool
	LastSequence int
}

type eventLogFileSnapshot struct {
	Device          uint64
	Inode           uint64
	Size            int64
	ModTimeUnixNano int64
}

type eventLogLockedFiles struct {
	eventPath string
	lockPath  string
	event     eventLogFileSnapshot
	lock      eventLogFileSnapshot
}

type eventLogSequenceMetadata struct {
	Version         int    `json:"version"`
	Device          uint64 `json:"device"`
	Inode           uint64 `json:"inode"`
	LockDevice      uint64 `json:"lock_device,omitempty"`
	LockInode       uint64 `json:"lock_inode,omitempty"`
	Size            int64  `json:"size"`
	ModTimeUnixNano int64  `json:"mod_time_unix_nano"`
	LastSequence    int    `json:"last_sequence"`
	Checksum        string `json:"checksum"`
}

func NewEventLog(path string) *EventLog {
	return &EventLog{path: path}
}

func (l *EventLog) Append(kind string, attemptID string, runner RunnerName, payload map[string]any) error {
	return l.withLockedFile(func(eventFile *os.File, locked eventLogLockedFiles) error {
		lastSequence, before, err := l.validatedSequenceState(eventFile)
		if err != nil {
			return err
		}
		if err := locked.verifyPathIdentities(); err != nil {
			return err
		}
		if lastSequence == int(^uint(0)>>1) {
			return fmt.Errorf("event log %q sequence is exhausted at %d", l.path, lastSequence)
		}
		nextSeq := lastSequence + 1
		event := Event{
			Seq:       nextSeq,
			At:        time.Now().UTC().Format(time.RFC3339),
			AttemptID: attemptID,
			Runner:    runner,
			Kind:      kind,
			Payload:   payload,
		}
		raw, err := json.Marshal(event)
		if err != nil {
			return fmt.Errorf("marshal event log record: %w", err)
		}
		raw = append(raw, '\n')
		n, err := l.write(eventFile, raw)
		if err != nil {
			return fmt.Errorf("append event log record: %w", err)
		}
		if n != len(raw) {
			return fmt.Errorf("append event log record: wrote %d of %d bytes: %w", n, len(raw), io.ErrShortWrite)
		}
		if err := l.sync(eventFile); err != nil {
			return fmt.Errorf("sync event log: %w", err)
		}
		after, err := snapshotEventLogFile(eventFile, l.path)
		if err != nil {
			return err
		}
		expectedSize := before.Size + int64(len(raw))
		if after.Device != before.Device || after.Inode != before.Inode || after.Size != expectedSize {
			return fmt.Errorf("event log %q changed outside the locked append: expected identity %d:%d and size %d, got %d:%d and size %d", l.path, before.Device, before.Inode, expectedSize, after.Device, after.Inode, after.Size)
		}
		if err := locked.verifyPathIdentities(); err != nil {
			return err
		}
		return writeEventLogSequenceMetadata(l.path, eventLogSequenceMetadata{
			Version:         eventLogSequenceMetadataVersion,
			Device:          after.Device,
			Inode:           after.Inode,
			LockDevice:      locked.lock.Device,
			LockInode:       locked.lock.Inode,
			Size:            after.Size,
			ModTimeUnixNano: after.ModTimeUnixNano,
			LastSequence:    nextSeq,
		}, locked.verifyPathIdentities)
	})
}

func (l *EventLog) Validate() error {
	_, err := l.Contains("", "")
	return err
}

func (l *EventLog) Contains(attemptID, kind string) (bool, error) {
	found := false
	err := l.withLockedFile(func(eventFile *os.File, _ eventLogLockedFiles) error {
		var err error
		found, err = validatedEventLogContains(eventFile, l.path, attemptID, kind)
		return err
	})
	return found, err
}

func (l *EventLog) withLockedFile(action func(*os.File, eventLogLockedFiles) error) (returnErr error) {
	if l == nil {
		return errors.New("event log is nil")
	}
	if strings.TrimSpace(l.path) == "" {
		return errors.New("event log path is empty")
	}
	if err := ensureDir(filepath.Dir(l.path)); err != nil {
		return fmt.Errorf("create event log directory: %w", err)
	}
	metadata, established, err := readEventLogSequenceMetadata(l.path)
	if err != nil {
		return err
	}

	lockPath := l.path + ".lock"
	lockFlags := os.O_RDWR
	if !established {
		lockFlags |= os.O_CREATE
	}
	lockFile, err := os.OpenFile(lockPath, lockFlags, 0o600)
	if err != nil {
		return fmt.Errorf("open event log lock %q: %w", lockPath, err)
	}
	locked := false
	defer func() {
		if locked {
			joinEventLogError(&returnErr, l.flock(int(lockFile.Fd()), syscall.LOCK_UN), "unlock event log")
		}
		joinEventLogError(&returnErr, lockFile.Close(), "close event log lock")
	}()
	if err := requireRegularEventLogFile(lockFile, lockPath); err != nil {
		return err
	}
	if err := lockFile.Chmod(0o600); err != nil {
		return fmt.Errorf("set owner-only event log lock permissions %q: %w", lockPath, err)
	}
	if err := l.flock(int(lockFile.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("lock event log %q: %w", lockPath, err)
	}
	locked = true
	lockSnapshot, err := snapshotEventLogFile(lockFile, lockPath)
	if err != nil {
		return err
	}
	if established && !metadata.matchesLockIdentity(lockSnapshot) {
		return fmt.Errorf("event log lock %q identity changed: sequence metadata does not match current lock file", lockPath)
	}

	eventFlags := os.O_APPEND | os.O_RDWR
	if !established {
		eventFlags |= os.O_CREATE
	}
	eventFile, err := os.OpenFile(l.path, eventFlags, 0o600)
	if err != nil {
		return fmt.Errorf("open event log %q: %w", l.path, err)
	}
	defer func() {
		joinEventLogError(&returnErr, eventFile.Close(), "close event log")
	}()
	if err := requireRegularEventLogFile(eventFile, l.path); err != nil {
		return err
	}
	if err := eventFile.Chmod(0o600); err != nil {
		return fmt.Errorf("set owner-only event log permissions %q: %w", l.path, err)
	}
	eventSnapshot, err := snapshotEventLogFile(eventFile, l.path)
	if err != nil {
		return err
	}
	lockedFiles := eventLogLockedFiles{
		eventPath: l.path,
		lockPath:  lockPath,
		event:     eventSnapshot,
		lock:      lockSnapshot,
	}
	if err := lockedFiles.verifyPathIdentities(); err != nil {
		return err
	}
	return action(eventFile, lockedFiles)
}

func validatedEventLogContains(file *os.File, path, attemptID, kind string) (bool, error) {
	validation, err := fullyValidateEventLog(file, path, attemptID, kind)
	return validation.Found, err
}

func fullyValidateEventLog(file *os.File, path, attemptID, kind string) (eventLogValidation, error) {
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return eventLogValidation{}, fmt.Errorf("seek event log %q: %w", path, err)
	}
	reader := bufio.NewReader(file)
	validation := eventLogValidation{}
	lineNumber := 0
	for {
		line, err := reader.ReadBytes('\n')
		if errors.Is(err, io.EOF) {
			if len(line) != 0 {
				return eventLogValidation{}, fmt.Errorf("event log %q has a partial trailing record: missing newline", path)
			}
			break
		}
		if err != nil {
			return eventLogValidation{}, fmt.Errorf("read event log %q: %w", path, err)
		}
		lineNumber++
		line = bytes.TrimSuffix(line, []byte{'\n'})
		if len(bytes.TrimSpace(line)) == 0 {
			return eventLogValidation{}, fmt.Errorf("event log %q has a malformed record at line %d: empty JSON line", path, lineNumber)
		}
		var event struct {
			Seq       *int   `json:"seq"`
			AttemptID string `json:"attempt_id"`
			Kind      string `json:"kind"`
		}
		if err := json.Unmarshal(line, &event); err != nil {
			return eventLogValidation{}, fmt.Errorf("event log %q has a malformed record at line %d: %w", path, lineNumber, err)
		}
		if event.Seq != nil {
			if *event.Seq <= 0 {
				return eventLogValidation{}, fmt.Errorf("event log %q has a malformed record at line %d: sequence must be positive", path, lineNumber)
			}
			if validation.LastSequence > 0 && *event.Seq <= validation.LastSequence {
				return eventLogValidation{}, fmt.Errorf("event log %q has a non-monotone sequence at line %d: %d after %d", path, lineNumber, *event.Seq, validation.LastSequence)
			}
			validation.LastSequence = *event.Seq
		}
		if attemptID != "" && kind != "" && event.AttemptID == attemptID && event.Kind == kind {
			validation.Found = true
		}
	}
	return validation, nil
}

func (l *EventLog) validatedSequenceState(file *os.File) (int, eventLogFileSnapshot, error) {
	before, err := snapshotEventLogFile(file, l.path)
	if err != nil {
		return 0, eventLogFileSnapshot{}, err
	}
	metadata, present, err := readEventLogSequenceMetadata(l.path)
	if err != nil {
		return 0, eventLogFileSnapshot{}, err
	}
	if present {
		if !metadata.matchesIdentity(before) {
			return 0, eventLogFileSnapshot{}, fmt.Errorf("event log %q identity changed: sequence metadata refers to %d:%d, current file is %d:%d", l.path, metadata.Device, metadata.Inode, before.Device, before.Inode)
		}
		if metadata.matches(before) {
			return metadata.LastSequence, before, nil
		}
	}
	validation, err := l.fullValidate(file, l.path)
	if err != nil {
		return 0, eventLogFileSnapshot{}, err
	}
	after, err := snapshotEventLogFile(file, l.path)
	if err != nil {
		return 0, eventLogFileSnapshot{}, err
	}
	if after != before {
		return 0, eventLogFileSnapshot{}, fmt.Errorf("event log %q changed while validating history", l.path)
	}
	return validation.LastSequence, after, nil
}

func (l *EventLog) fullValidate(file *os.File, path string) (eventLogValidation, error) {
	if l.fullValidateFn != nil {
		return l.fullValidateFn(file, path)
	}
	return fullyValidateEventLog(file, path, "", "")
}

func (m eventLogSequenceMetadata) matches(snapshot eventLogFileSnapshot) bool {
	return m.hasValidChecksum() &&
		m.Version == eventLogSequenceMetadataVersion &&
		m.LastSequence >= 0 &&
		m.matchesIdentity(snapshot) &&
		m.Size == snapshot.Size &&
		m.ModTimeUnixNano == snapshot.ModTimeUnixNano
}

func (m eventLogSequenceMetadata) matchesIdentity(snapshot eventLogFileSnapshot) bool {
	return m.Device == snapshot.Device && m.Inode == snapshot.Inode
}

func (m eventLogSequenceMetadata) matchesLockIdentity(snapshot eventLogFileSnapshot) bool {
	return (m.LockDevice == 0 && m.LockInode == 0) || (m.LockDevice == snapshot.Device && m.LockInode == snapshot.Inode)
}

func (m eventLogSequenceMetadata) hasValidChecksum() bool {
	checksum := strings.TrimSpace(m.Checksum)
	if checksum == "" {
		return false
	}
	m.Checksum = ""
	raw, err := json.Marshal(m)
	if err != nil {
		return false
	}
	expected := fmt.Sprintf("%x", sha256.Sum256(raw))
	return checksum == expected
}

func eventLogSequenceMetadataPath(path string) string {
	return path + ".seq"
}

func readEventLogSequenceMetadata(eventPath string) (eventLogSequenceMetadata, bool, error) {
	metadataPath := eventLogSequenceMetadataPath(eventPath)
	info, err := os.Lstat(metadataPath)
	if errors.Is(err, os.ErrNotExist) {
		return eventLogSequenceMetadata{}, false, nil
	}
	if err != nil {
		return eventLogSequenceMetadata{}, false, fmt.Errorf("stat event log sequence metadata %q: %w", metadataPath, err)
	}
	if !info.Mode().IsRegular() {
		return eventLogSequenceMetadata{}, false, fmt.Errorf("event log sequence metadata %q is not a regular file", metadataPath)
	}
	file, err := os.Open(metadataPath)
	if err != nil {
		return eventLogSequenceMetadata{}, false, fmt.Errorf("open event log sequence metadata %q: %w", metadataPath, err)
	}
	defer file.Close()
	if err := file.Chmod(0o600); err != nil {
		return eventLogSequenceMetadata{}, false, fmt.Errorf("set owner-only event log sequence metadata permissions %q: %w", metadataPath, err)
	}
	raw, err := io.ReadAll(io.LimitReader(file, eventLogSequenceMetadataMaxSize+1))
	if err != nil {
		return eventLogSequenceMetadata{}, false, fmt.Errorf("read event log sequence metadata %q: %w", metadataPath, err)
	}
	if len(raw) == 0 || len(raw) > eventLogSequenceMetadataMaxSize || raw[len(raw)-1] != '\n' || bytes.Count(raw, []byte{'\n'}) != 1 {
		return eventLogSequenceMetadata{}, false, nil
	}
	var metadata eventLogSequenceMetadata
	if err := json.Unmarshal(bytes.TrimSuffix(raw, []byte{'\n'}), &metadata); err != nil {
		return eventLogSequenceMetadata{}, false, nil
	}
	if metadata.Version != eventLogSequenceMetadataVersion || metadata.Size < 0 || metadata.LastSequence < 0 || (metadata.LockDevice == 0) != (metadata.LockInode == 0) || !metadata.hasValidChecksum() {
		return eventLogSequenceMetadata{}, false, nil
	}
	return metadata, true, nil
}

func writeEventLogSequenceMetadata(eventPath string, metadata eventLogSequenceMetadata, verifyPaths func() error) (returnErr error) {
	metadataPath := eventLogSequenceMetadataPath(eventPath)
	metadata.Checksum = ""
	unsigned, err := json.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("marshal event log sequence metadata checksum: %w", err)
	}
	metadata.Checksum = fmt.Sprintf("%x", sha256.Sum256(unsigned))
	raw, err := json.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("marshal event log sequence metadata: %w", err)
	}
	raw = append(raw, '\n')
	temp, err := os.CreateTemp(filepath.Dir(metadataPath), "."+filepath.Base(metadataPath)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create event log sequence metadata temp file: %w", err)
	}
	tempPath := temp.Name()
	tempClosed := false
	defer func() {
		if !tempClosed {
			joinEventLogError(&returnErr, temp.Close(), "close event log sequence metadata temp file")
		}
		if err := os.Remove(tempPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			joinEventLogError(&returnErr, err, "remove event log sequence metadata temp file")
		}
	}()
	if err := temp.Chmod(0o600); err != nil {
		return fmt.Errorf("set owner-only event log sequence metadata temp permissions: %w", err)
	}
	if n, err := temp.Write(raw); err != nil {
		return fmt.Errorf("write event log sequence metadata: %w", err)
	} else if n != len(raw) {
		return fmt.Errorf("write event log sequence metadata: wrote %d of %d bytes: %w", n, len(raw), io.ErrShortWrite)
	}
	if err := temp.Sync(); err != nil {
		return fmt.Errorf("sync event log sequence metadata: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close event log sequence metadata before rename: %w", err)
	}
	tempClosed = true
	if verifyPaths != nil {
		if err := verifyPaths(); err != nil {
			return err
		}
	}
	if err := os.Rename(tempPath, metadataPath); err != nil {
		return fmt.Errorf("replace event log sequence metadata %q: %w", metadataPath, err)
	}
	if verifyPaths != nil {
		if err := verifyPaths(); err != nil {
			return err
		}
	}
	directory, err := os.Open(filepath.Dir(metadataPath))
	if err != nil {
		return fmt.Errorf("open event log sequence metadata directory: %w", err)
	}
	defer func() {
		joinEventLogError(&returnErr, directory.Close(), "close event log sequence metadata directory")
	}()
	if err := directory.Sync(); err != nil {
		return fmt.Errorf("sync event log sequence metadata directory: %w", err)
	}
	return nil
}

func snapshotEventLogFile(file *os.File, path string) (eventLogFileSnapshot, error) {
	info, err := file.Stat()
	if err != nil {
		return eventLogFileSnapshot{}, fmt.Errorf("stat event log %q: %w", path, err)
	}
	return snapshotEventLogFileInfo(info, path)
}

func snapshotEventLogPath(path string) (eventLogFileSnapshot, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return eventLogFileSnapshot{}, fmt.Errorf("stat event log path %q: %w", path, err)
	}
	return snapshotEventLogFileInfo(info, path)
}

func snapshotEventLogFileInfo(info os.FileInfo, path string) (eventLogFileSnapshot, error) {
	if !info.Mode().IsRegular() {
		return eventLogFileSnapshot{}, fmt.Errorf("event log file %q is not a regular file", path)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return eventLogFileSnapshot{}, fmt.Errorf("stat event log %q: filesystem identity is unavailable", path)
	}
	return eventLogFileSnapshot{
		Device:          uint64(stat.Dev),
		Inode:           uint64(stat.Ino),
		Size:            info.Size(),
		ModTimeUnixNano: info.ModTime().UnixNano(),
	}, nil
}

func (s eventLogFileSnapshot) matchesIdentity(other eventLogFileSnapshot) bool {
	return s.Device == other.Device && s.Inode == other.Inode
}

func (f eventLogLockedFiles) verifyPathIdentities() error {
	if err := verifyEventLogPathIdentity(f.lockPath, f.lock, "event log lock"); err != nil {
		return err
	}
	return verifyEventLogPathIdentity(f.eventPath, f.event, "event log")
}

func verifyEventLogPathIdentity(path string, expected eventLogFileSnapshot, label string) error {
	actual, err := snapshotEventLogPath(path)
	if err != nil {
		return fmt.Errorf("%s %q path identity changed: %w", label, path, err)
	}
	if !expected.matchesIdentity(actual) {
		return fmt.Errorf("%s %q path identity changed: expected %d:%d, got %d:%d", label, path, expected.Device, expected.Inode, actual.Device, actual.Inode)
	}
	return nil
}

func requireRegularEventLogFile(file *os.File, path string) error {
	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("stat event log file %q: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("event log file %q is not a regular file", path)
	}
	return nil
}

func joinEventLogError(target *error, err error, action string) {
	if err == nil {
		return
	}
	wrapped := fmt.Errorf("%s: %w", action, err)
	if *target == nil {
		*target = wrapped
		return
	}
	*target = errors.Join(*target, wrapped)
}

func (l *EventLog) flock(fd int, operation int) error {
	if l.flockFn != nil {
		return l.flockFn(fd, operation)
	}
	return syscall.Flock(fd, operation)
}

func (l *EventLog) write(file *os.File, raw []byte) (int, error) {
	if l.writeFn != nil {
		return l.writeFn(file, raw)
	}
	return file.Write(raw)
}

func (l *EventLog) sync(file *os.File) error {
	if l.syncFn != nil {
		return l.syncFn(file)
	}
	return file.Sync()
}
