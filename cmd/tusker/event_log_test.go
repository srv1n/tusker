package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

const (
	eventLogChildPathEnv  = "TUSKER_TEST_EVENT_LOG_CHILD_PATH"
	eventLogChildCountEnv = "TUSKER_TEST_EVENT_LOG_CHILD_COUNT"
)

func TestEventLogConcurrentProcesses(t *testing.T) {
	if path := os.Getenv(eventLogChildPathEnv); path != "" {
		count, err := strconv.Atoi(os.Getenv(eventLogChildCountEnv))
		if err != nil || count <= 0 {
			t.Fatalf("invalid child append count: %q", os.Getenv(eventLogChildCountEnv))
		}
		log := NewEventLog(path)
		for i := 0; i < count; i++ {
			if err := log.Append("concurrent_append", fmt.Sprintf("%d-%d", os.Getpid(), i), RunnerCodexExec, nil); err != nil {
				t.Fatal(err)
			}
		}
		return
	}

	path := filepath.Join(t.TempDir(), "events.jsonl")
	const processCount = 2
	const appendsPerProcess = 20
	type childProcess struct {
		cmd    *exec.Cmd
		output bytes.Buffer
	}
	children := make([]*childProcess, 0, processCount)
	for i := 0; i < processCount; i++ {
		child := &childProcess{cmd: exec.Command(os.Args[0], "-test.run=^TestEventLogConcurrentProcesses$", "-test.count=1")}
		child.cmd.Env = append(os.Environ(),
			eventLogChildPathEnv+"="+path,
			eventLogChildCountEnv+"="+strconv.Itoa(appendsPerProcess),
		)
		child.cmd.Stdout = &child.output
		child.cmd.Stderr = &child.output
		if err := child.cmd.Start(); err != nil {
			t.Fatal(err)
		}
		children = append(children, child)
	}
	t.Cleanup(func() {
		for _, child := range children {
			if child.cmd.Process != nil {
				_ = child.cmd.Process.Kill()
			}
		}
	})
	var failures []string
	for _, child := range children {
		if err := child.cmd.Wait(); err != nil {
			failures = append(failures, fmt.Sprintf("%v: %s", err, child.output.String()))
		}
	}
	if len(failures) > 0 {
		t.Fatalf("event log append children failed:\n%s", strings.Join(failures, "\n"))
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := bytes.Split(bytes.TrimSuffix(raw, []byte{'\n'}), []byte{'\n'})
	expectedCount := processCount * appendsPerProcess
	if len(lines) != expectedCount {
		t.Fatalf("expected %d records, got %d", expectedCount, len(lines))
	}
	for i, line := range lines {
		var event Event
		if err := json.Unmarshal(line, &event); err != nil {
			t.Fatalf("record %d is invalid JSON: %v", i, err)
		}
		if event.Seq != i+1 {
			t.Fatalf("record %d has sequence %d, want %d", i, event.Seq, i+1)
		}
	}
	assertOwnerOnlyFile(t, path)
	assertOwnerOnlyFile(t, path+".lock")
	assertOwnerOnlyFile(t, eventLogSequenceMetadataPath(path))
}

func TestEventLogContainsValidatesRecordsAfterMatch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	log := NewEventLog(path)
	if err := log.Append("thread_started", "attempt-1", RunnerCodexExec, nil); err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString("not-json\n"); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	found, err := log.Contains("attempt-1", "thread_started")
	if err == nil || !strings.Contains(err.Error(), "malformed record at line 2") {
		t.Fatalf("expected full-log validation after prefix match, got found=%t err=%v", found, err)
	}
}

func TestEventLogAppendPreservesPriorBytesAndUsesOwnerOnlyPermissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.jsonl")
	lockPath := path + ".lock"
	prior := []byte("{\"seq\":7, \"at\":\"legacy\", \"attempt_id\":\"old\", \"runner\":\"codex_exec\", \"kind\":\"existing\", \"extra\":true}\n")
	if err := os.WriteFile(path, prior, 0o666); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o666); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lockPath, nil, 0o666); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(lockPath, 0o666); err != nil {
		t.Fatal(err)
	}

	if err := NewEventLog(path).Append("appended", "attempt-8", RunnerCodexExec, map[string]any{"ok": true}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(raw, prior) {
		t.Fatalf("append changed prior bytes:\nwant prefix: %q\ngot: %q", prior, raw)
	}
	appended := raw[len(prior):]
	if bytes.Count(appended, []byte{'\n'}) != 1 || appended[len(appended)-1] != '\n' {
		t.Fatalf("append must add exactly one complete JSON line, got %q", appended)
	}
	var event Event
	if err := json.Unmarshal(bytes.TrimSuffix(appended, []byte{'\n'}), &event); err != nil {
		t.Fatal(err)
	}
	if event.Seq != 8 || event.Kind != "appended" || event.AttemptID != "attempt-8" {
		t.Fatalf("unexpected appended event: %#v", event)
	}
	assertOwnerOnlyFile(t, path)
	assertOwnerOnlyFile(t, lockPath)
}

func TestEventLogAppendSkipsCompleteExternalRecordsWithoutSequence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	prior := []byte("{\"seq\":1,\"kind\":\"attempt_started\"}\n{\"kind\":\"external_heartbeat\",\"pid\":42}\n")
	if err := os.WriteFile(path, prior, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := NewEventLog(path).Append("attempt_spawned", "attempt-1", RunnerCodexExec, nil); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(raw, prior) {
		t.Fatal("append rewrote complete external records")
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	var appended Event
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &appended); err != nil {
		t.Fatal(err)
	}
	if appended.Seq != 2 {
		t.Fatalf("sequence after external record = %d, want 2", appended.Seq)
	}
	if err := NewEventLog(path).Validate(); err != nil {
		t.Fatalf("complete external record should validate: %v", err)
	}
}

func TestEventLogAppendRefusesResetSequenceHiddenByExternalRecords(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	original := []byte(strings.Join([]string{
		`{"seq":2,"kind":"before_reset"}`,
		`{"kind":"external_heartbeat","pid":41}`,
		`{"seq":1,"kind":"reset"}`,
		`{"kind":"external_heartbeat","pid":42}`,
		"",
	}, "\n"))
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	err := NewEventLog(path).Append("must_not_append", "attempt-3", RunnerCodexExec, nil)
	if err == nil || !strings.Contains(err.Error(), "non-monotone sequence") {
		t.Fatalf("expected old reset history to be refused, got %v", err)
	}
	after, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !bytes.Equal(after, original) {
		t.Fatalf("refused append changed reset history:\nwant: %q\ngot: %q", original, after)
	}
}

func TestEventLogSequenceMetadataFastPathAndExternalAppendInvalidation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	log := NewEventLog(path)
	fullValidations := 0
	log.fullValidateFn = func(file *os.File, path string) (eventLogValidation, error) {
		fullValidations++
		return fullyValidateEventLog(file, path, "", "")
	}
	if err := log.Append("one", "attempt-1", RunnerCodexExec, nil); err != nil {
		t.Fatal(err)
	}
	if err := log.Append("two", "attempt-1", RunnerCodexExec, nil); err != nil {
		t.Fatal(err)
	}
	if fullValidations != 1 {
		t.Fatalf("validated metadata fast path performed %d full scans, want 1", fullValidations)
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString("{\"kind\":\"external_heartbeat\",\"pid\":42}\n"); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := log.Append("three", "attempt-1", RunnerCodexExec, nil); err != nil {
		t.Fatal(err)
	}
	if fullValidations != 2 {
		t.Fatalf("external append triggered %d full scans, want 2 total", fullValidations)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := bytes.Split(bytes.TrimSpace(raw), []byte{'\n'})
	var appended Event
	if err := json.Unmarshal(lines[len(lines)-1], &appended); err != nil {
		t.Fatal(err)
	}
	if appended.Seq != 3 {
		t.Fatalf("sequence after external append = %d, want 3", appended.Seq)
	}
}

func TestEventLogAppendRecoversCorruptMetadataByFullyValidatingHistory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	log := NewEventLog(path)
	if err := log.Append("one", "attempt-1", RunnerCodexExec, nil); err != nil {
		t.Fatal(err)
	}
	metadataPath := eventLogSequenceMetadataPath(path)
	if err := os.WriteFile(metadataPath, []byte("not-json\n"), 0o666); err != nil {
		t.Fatal(err)
	}
	fullValidations := 0
	log.fullValidateFn = func(file *os.File, path string) (eventLogValidation, error) {
		fullValidations++
		return fullyValidateEventLog(file, path, "", "")
	}
	if err := log.Append("two", "attempt-1", RunnerCodexExec, nil); err != nil {
		t.Fatal(err)
	}
	if fullValidations != 1 {
		t.Fatalf("corrupt metadata triggered %d full scans, want 1", fullValidations)
	}
	rawMetadata, err := os.ReadFile(metadataPath)
	if err != nil {
		t.Fatal(err)
	}
	var metadata eventLogSequenceMetadata
	if err := json.Unmarshal(bytes.TrimSpace(rawMetadata), &metadata); err != nil {
		t.Fatal(err)
	}
	metadata.LastSequence = 0
	semanticallyCorrupt, err := json.Marshal(metadata)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(metadataPath, append(semanticallyCorrupt, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := log.Append("three", "attempt-1", RunnerCodexExec, nil); err != nil {
		t.Fatal(err)
	}
	if fullValidations != 2 {
		t.Fatalf("checksum-invalid metadata triggered %d full scans, want 2 total", fullValidations)
	}
	assertOwnerOnlyFile(t, metadataPath)
}

func TestEventLogAppendRefusesHistoryAddedAfterStaleMetadata(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	log := NewEventLog(path)
	if err := log.Append("one", "attempt-1", RunnerCodexExec, nil); err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString("{\"seq\":1,\"kind\":\"external_reset\"}\n"); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	err = log.Append("must_not_append", "attempt-1", RunnerCodexExec, nil)
	if err == nil || !strings.Contains(err.Error(), "non-monotone sequence") {
		t.Fatalf("stale metadata certified reset history: %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, before) {
		t.Fatal("refused append mutated history covered by stale metadata")
	}
}

func TestEventLogAppendRefusesPartialTailAddedAfterMetadataCheckpoint(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	log := NewEventLog(path)
	if err := log.Append("one", "attempt-1", RunnerCodexExec, nil); err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString("{\"kind\":\"partial\""); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	err = log.Append("must_not_append", "attempt-1", RunnerCodexExec, nil)
	if err == nil || !strings.Contains(err.Error(), "partial trailing record") {
		t.Fatalf("stale metadata certified partial tail: %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, before) {
		t.Fatal("refused append mutated partial history")
	}
}

func TestEventLogAppendFailsClosedWhenEstablishedLogIsReplaced(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	log := NewEventLog(path)
	if err := log.Append("one", "attempt-1", RunnerCodexExec, nil); err != nil {
		t.Fatal(err)
	}
	metadataPath := eventLogSequenceMetadataPath(path)
	metadataBefore, err := os.ReadFile(metadataPath)
	if err != nil {
		t.Fatal(err)
	}
	movedPath := path + ".moved"
	if err := os.Rename(path, movedPath); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	err = log.Append("must_not_append", "attempt-2", RunnerCodexExec, nil)
	if err == nil || !strings.Contains(err.Error(), "identity changed") {
		t.Fatalf("replacement was accepted as a new event log: %v", err)
	}
	replacement, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(replacement) != 0 {
		t.Fatalf("replacement received an event instead of failing closed: %q", replacement)
	}
	metadataAfter, err := os.ReadFile(metadataPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(metadataAfter, metadataBefore) {
		t.Fatal("replacement changed sequence metadata")
	}
}

func TestEventLogAppendFailsClosedWhenEstablishedLogIsDeleted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	log := NewEventLog(path)
	if err := log.Append("one", "attempt-1", RunnerCodexExec, nil); err != nil {
		t.Fatal(err)
	}
	metadataPath := eventLogSequenceMetadataPath(path)
	metadataBefore, err := os.ReadFile(metadataPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}

	err = log.Append("must_not_append", "attempt-2", RunnerCodexExec, nil)
	if err == nil || !strings.Contains(err.Error(), "open event log") {
		t.Fatalf("deleted established log was accepted as a new event log: %v", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("deleted established log was recreated: %v", err)
	}
	metadataAfter, err := os.ReadFile(metadataPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(metadataAfter, metadataBefore) {
		t.Fatal("deleted log changed sequence metadata")
	}
}

func TestEventLogAppendFailsClosedWhenEstablishedLockIsDeletedOrReplaced(t *testing.T) {
	tests := map[string]func(t *testing.T, lockPath string){
		"deleted": func(t *testing.T, lockPath string) {
			t.Helper()
			if err := os.Remove(lockPath); err != nil {
				t.Fatal(err)
			}
		},
		"replaced": func(t *testing.T, lockPath string) {
			t.Helper()
			if err := os.Rename(lockPath, lockPath+".moved"); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(lockPath, nil, 0o600); err != nil {
				t.Fatal(err)
			}
		},
	}
	for name, replace := range tests {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "events.jsonl")
			log := NewEventLog(path)
			if err := log.Append("one", "attempt-1", RunnerCodexExec, nil); err != nil {
				t.Fatal(err)
			}
			metadataPath := eventLogSequenceMetadataPath(path)
			metadataBefore, err := os.ReadFile(metadataPath)
			if err != nil {
				t.Fatal(err)
			}
			replace(t, path+".lock")

			err = log.Append("must_not_append", "attempt-2", RunnerCodexExec, nil)
			if err == nil || !strings.Contains(err.Error(), "event log lock") {
				t.Fatalf("changed established lock was accepted: %v", err)
			}
			metadataAfter, err := os.ReadFile(metadataPath)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(metadataAfter, metadataBefore) {
				t.Fatalf("%s lock change published sequence metadata", name)
			}
		})
	}
}

func TestEventLogAppendRefusesPathReplacementBeforeMetadataPublication(t *testing.T) {
	tests := map[string]struct {
		replacePath func(string) string
		deletePath  bool
		wantError   string
	}{
		"event": {
			replacePath: func(path string) string { return path },
			wantError:   "event log ",
		},
		"event_deleted": {
			replacePath: func(path string) string { return path },
			deletePath:  true,
			wantError:   "event log ",
		},
		"lock": {
			replacePath: func(path string) string { return path + ".lock" },
			wantError:   "event log lock",
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "events.jsonl")
			log := NewEventLog(path)
			if err := log.Append("one", "attempt-1", RunnerCodexExec, nil); err != nil {
				t.Fatal(err)
			}
			metadataPath := eventLogSequenceMetadataPath(path)
			metadataBefore, err := os.ReadFile(metadataPath)
			if err != nil {
				t.Fatal(err)
			}
			targetPath := tc.replacePath(path)
			log.syncFn = func(file *os.File) error {
				if err := file.Sync(); err != nil {
					return err
				}
				if err := os.Rename(targetPath, targetPath+".moved"); err != nil {
					return err
				}
				if tc.deletePath {
					return nil
				}
				return os.WriteFile(targetPath, nil, 0o600)
			}

			err = log.Append("must_not_publish_metadata", "attempt-2", RunnerCodexExec, nil)
			if err == nil || !strings.Contains(err.Error(), tc.wantError) || !strings.Contains(err.Error(), "path identity changed") {
				t.Fatalf("path replacement did not fail closed: %v", err)
			}
			metadataAfter, err := os.ReadFile(metadataPath)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(metadataAfter, metadataBefore) {
				t.Fatalf("%s replacement published sequence metadata", name)
			}
		})
	}
}

func TestEventLogPartialAndMalformedTailsAreRefusedWithoutMutation(t *testing.T) {
	valid := []byte("{\"seq\":1,\"kind\":\"valid\"}\n")
	tests := map[string][]byte{
		"partial":   append(append([]byte{}, valid...), []byte("{\"seq\":2}")...),
		"malformed": append(append([]byte{}, valid...), []byte("{\"seq\":2,}\n")...),
		"blank":     append(append([]byte{}, valid...), '\n'),
	}
	for name, original := range tests {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "events.jsonl")
			if err := os.WriteFile(path, original, 0o600); err != nil {
				t.Fatal(err)
			}
			err := NewEventLog(path).Append("must_not_append", "attempt-2", RunnerCodexExec, nil)
			if err == nil || (!strings.Contains(err.Error(), "partial trailing record") && !strings.Contains(err.Error(), "malformed record")) {
				t.Fatalf("expected loud trailing-record error, got %v", err)
			}
			after, readErr := os.ReadFile(path)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if !bytes.Equal(after, original) {
				t.Fatalf("refused append changed history:\nwant: %q\ngot: %q", original, after)
			}
		})
	}
}

func TestEventLogAppendSurfacesLockWriteAndSyncErrors(t *testing.T) {
	sentinel := errors.New("injected event log failure")
	tests := map[string]func(*EventLog){
		"lock": func(log *EventLog) {
			log.flockFn = func(_ int, operation int) error {
				if operation == syscall.LOCK_EX {
					return sentinel
				}
				return nil
			}
		},
		"write": func(log *EventLog) {
			log.writeFn = func(_ *os.File, _ []byte) (int, error) { return 0, sentinel }
		},
		"sync": func(log *EventLog) {
			log.syncFn = func(_ *os.File) error { return sentinel }
		},
		"unlock": func(log *EventLog) {
			log.flockFn = func(fd int, operation int) error {
				if operation == syscall.LOCK_UN {
					return sentinel
				}
				return syscall.Flock(fd, operation)
			}
		},
	}
	for name, inject := range tests {
		t.Run(name, func(t *testing.T) {
			log := NewEventLog(filepath.Join(t.TempDir(), "events.jsonl"))
			inject(log)
			err := log.Append("failure", "attempt-1", RunnerCodexExec, nil)
			if !errors.Is(err, sentinel) {
				t.Fatalf("expected injected %s error, got %v", name, err)
			}
		})
	}
}

func assertOwnerOnlyFile(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if permissions := info.Mode().Perm(); permissions != 0o600 {
		t.Fatalf("%s permissions = %04o, want 0600", path, permissions)
	}
}
