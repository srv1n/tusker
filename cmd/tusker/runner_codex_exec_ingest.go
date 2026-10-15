package main

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

const (
	codexExecIngestCacheLimit = 128
	codexExecIngestTurnLimit  = 128
	codexExecIngestEventLimit = 4096
)

// The cursor is deliberately in memory. A restart replays the raw log against
// durable turns and event identities instead of trusting another checkpoint.
type codexExecIngestState struct {
	rawPath, eventPath     string
	rawSnapshot            eventLogFileSnapshot
	rawOffset              int64
	rawLine                int64
	rawBoundary            []byte
	rawInitialized         bool
	usedAt                 time.Time
	known                  map[string]codexExecTurnSnapshot
	turnOverflow           bool
	nextIndex              int
	eventLog               *EventLog
	events                 map[string]bool
	eventBloom             [8192]uint64
	eventSnapshot          eventLogFileSnapshot
	eventOffset            int64
	eventSequence          int
	eventBoundary          []byte
	eventsInitialized      bool
	retry                  bool
	sessionRef, messageRef string
	stats                  codexExecIngestStats
}

// Counters make the no-work and append-only paths measurable in regressions.
type codexExecIngestStats struct {
	rawBytes, rawBoundaryBytes, rawRecords        int64
	eventBytes, eventReads, turnReads, turnWrites int64
}

type codexExecIngestSinkRecord struct {
	Seq       *int           `json:"seq"`
	AttemptID string         `json:"attempt_id"`
	Kind      string         `json:"kind"`
	Payload   map[string]any `json:"payload"`
}

func (d *Daemon) codexExecIngestState(run RunStatus) (*codexExecIngestState, error) {
	if d.codexExecIngest == nil {
		d.codexExecIngest = map[string]*codexExecIngestState{}
	}
	key := run.ActiveAttemptID
	state := d.codexExecIngest[key]
	if state != nil && state.rawPath == run.RawLogPath && state.eventPath == run.EventSinkPath {
		state.usedAt = time.Now()
		return state, nil
	}
	if len(d.codexExecIngest) >= codexExecIngestCacheLimit {
		var oldestKey string
		var oldest time.Time
		for candidate, cached := range d.codexExecIngest {
			if oldest.IsZero() || cached.usedAt.Before(oldest) {
				oldestKey, oldest = candidate, cached.usedAt
			}
		}
		delete(d.codexExecIngest, oldestKey)
	}
	turns, truncated, err := d.store.ListTurnsForAttemptPage(run.ActiveAttemptID, codexExecIngestTurnLimit)
	if err != nil {
		return nil, fmt.Errorf("load codex-exec turns before replay: %w", err)
	}
	state = &codexExecIngestState{
		rawPath: run.RawLogPath, eventPath: run.EventSinkPath, usedAt: time.Now(),
		known: map[string]codexExecTurnSnapshot{}, events: map[string]bool{},
		turnOverflow: truncated, sessionRef: run.SessionRef,
		stats: codexExecIngestStats{turnReads: 1},
	}
	for _, turn := range turns {
		state.known[turn.TurnID] = codexExecTurnSnapshot{
			index: turn.TurnIndex, status: turn.Status, inputTokens: turn.InputTokens,
			outputTokens: turn.OutputTokens, totalTokens: turn.TotalTokens,
			started: turn.StartedAt != "",
		}
		if turn.TurnIndex >= state.nextIndex {
			state.nextIndex = turn.TurnIndex + 1
		}
	}
	if truncated {
		state.nextIndex, err = d.store.NextTurnIndex(run.ProjectID, run.RecordID, run.ActiveAttemptID)
		if err != nil {
			return nil, err
		}
	}
	if strings.TrimSpace(run.EventSinkPath) != "" {
		state.eventLog = NewEventLog(run.EventSinkPath)
	}
	d.codexExecIngest[key] = state
	return state, nil
}

// Refresh only the appended sink suffix, under the existing EventLog lock and
// identity checks. In particular, an append error may mean the complete record
// reached disk before sync/metadata failed; replay must discover it first.
func (s *codexExecIngestState) refreshEvents(attemptID string) error {
	if s.eventLog == nil {
		return nil
	}
	return s.eventLog.withLockedFile(func(file *os.File, locked eventLogLockedFiles) error {
		snapshot, err := snapshotEventLogFile(file, s.eventPath)
		if err != nil {
			return err
		}
		if locked.established && !locked.metadata.matchesIdentity(snapshot) {
			return fmt.Errorf("event log %q identity changed before codex-exec replay", s.eventPath)
		}
		if s.eventsInitialized && snapshot == s.eventSnapshot {
			return nil
		}
		offset, sequence := s.eventOffset, s.eventSequence
		if s.eventsInitialized {
			if !snapshot.matchesIdentity(s.eventSnapshot) || snapshot.Size < offset {
				return fmt.Errorf("event log %q was replaced or truncated during codex-exec replay", s.eventPath)
			}
			if offset > 0 {
				boundary := make([]byte, len(s.eventBoundary))
				n, err := file.ReadAt(boundary, offset-int64(len(boundary)))
				s.stats.eventBytes += int64(n)
				if err != nil || !bytes.Equal(boundary, s.eventBoundary) || snapshot.Size == offset {
					return fmt.Errorf("event log %q changed without a valid append during codex-exec replay", s.eventPath)
				}
			}
		}
		if _, err := file.Seek(offset, io.SeekStart); err != nil {
			return err
		}
		reader := bufio.NewReader(io.LimitReader(file, snapshot.Size-offset))
		pending := &codexExecIngestState{events: map[string]bool{}}
		boundary := append([]byte(nil), s.eventBoundary...)
		s.stats.eventReads++
		for {
			line, err := readCodexExecIngestLine(reader)
			s.stats.eventBytes += int64(len(line))
			if errors.Is(err, io.EOF) {
				if len(line) != 0 {
					return fmt.Errorf("event log %q has a partial trailing record: missing newline", s.eventPath)
				}
				break
			}
			if err != nil {
				return err
			}
			var event codexExecIngestSinkRecord
			if err := json.Unmarshal(line, &event); err != nil {
				return fmt.Errorf("event log %q has a malformed record: %w", s.eventPath, err)
			}
			if event.Seq != nil {
				if *event.Seq <= 0 || *event.Seq <= sequence {
					return fmt.Errorf("event log %q has a non-monotone or invalid sequence %d after %d", s.eventPath, *event.Seq, sequence)
				}
				sequence = *event.Seq
			}
			if event.AttemptID == attemptID && codexExecIngestCachesKind(event.Kind) {
				pending.rememberEvent(codexExecIngestEventKey(event.Kind, event.Payload))
			}
			boundary = codexExecIngestBoundary(boundary, line)
		}
		for key := range pending.events {
			s.rememberEvent(key)
		}
		for i, word := range pending.eventBloom {
			s.eventBloom[i] |= word
		}
		s.eventSnapshot, s.eventOffset, s.eventSequence = snapshot, snapshot.Size, sequence
		s.eventBoundary = boundary
		s.eventsInitialized = true
		return nil
	})
}

func codexExecIngestCachesKind(kind string) bool {
	switch kind {
	case "thread_started", "turn_started", "turn_completed", "turn_usage_updated", "command_completed":
		return true
	}
	return false
}

// A fixed Bloom filter rejects new identities without a historical read.
// Positives evicted from the bounded hot set are checked against durable JSONL;
// false positives can cost a rare scan, but can never discard real evidence.
func (s *codexExecIngestState) eventBits(key string) [4]uint32 {
	digest := sha256.Sum256([]byte(key))
	var bits [4]uint32
	for i := range bits {
		bits[i] = binary.LittleEndian.Uint32(digest[i*4:]) & (uint32(len(s.eventBloom)*64) - 1)
	}
	return bits
}

func (s *codexExecIngestState) rememberEvent(key string) {
	for _, bit := range s.eventBits(key) {
		s.eventBloom[bit/64] |= uint64(1) << (bit % 64)
	}
	if len(s.events) >= codexExecIngestEventLimit && !s.events[key] {
		for old := range s.events {
			delete(s.events, old)
			break
		}
	}
	s.events[key] = true
}

func (s *codexExecIngestState) hasEvent(run RunStatus, key string) (bool, error) {
	if s.events[key] {
		return true, nil
	}
	for _, bit := range s.eventBits(key) {
		if s.eventBloom[bit/64]&(uint64(1)<<(bit%64)) == 0 {
			return false, nil
		}
	}
	if s.eventLog == nil {
		return false, nil
	}
	found := false
	err := s.eventLog.withLockedFile(func(file *os.File, _ eventLogLockedFiles) error {
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			return err
		}
		snapshot, err := snapshotEventLogFile(file, s.eventPath)
		if err != nil {
			return err
		}
		s.stats.eventReads++
		reader := bufio.NewReader(io.LimitReader(file, snapshot.Size))
		sequence := 0
		for {
			line, err := readCodexExecIngestLine(reader)
			s.stats.eventBytes += int64(len(line))
			if errors.Is(err, io.EOF) {
				if len(line) != 0 {
					return fmt.Errorf("event log %q has a partial trailing record: missing newline", s.eventPath)
				}
				return nil
			}
			if err != nil {
				return err
			}
			var event codexExecIngestSinkRecord
			if err := json.Unmarshal(line, &event); err != nil {
				return err
			}
			if event.Seq != nil {
				if *event.Seq <= 0 || *event.Seq <= sequence {
					return fmt.Errorf("event log %q has a non-monotone or invalid sequence %d after %d", s.eventPath, *event.Seq, sequence)
				}
				sequence = *event.Seq
			}
			if event.AttemptID == run.ActiveAttemptID && codexExecIngestEventKey(event.Kind, event.Payload) == key {
				found = true
			}
		}
	})
	if found && err == nil {
		s.rememberEvent(key)
	}
	return found, err
}

func codexExecIngestEventKey(kind string, payload map[string]any) string {
	switch kind {
	case "thread_started":
		return kind
	case "command_completed":
		return kind + ":" + firstNonEmpty(stringValue(payload["command_id"]), stringValue(payload["source_event_id"]))
	case "turn_completed":
		return kind + ":" + stringValue(payload["turn_id"]) + ":" + firstNonEmpty(stringValue(payload["status"]), "completed")
	case "turn_usage_updated":
		return fmt.Sprintf("%s:%s:%s:%s:%s", kind, stringValue(payload["turn_id"]), stringValue(payload["input_tokens"]), stringValue(payload["output_tokens"]), stringValue(payload["total_tokens"]))
	default:
		return kind + ":" + stringValue(payload["turn_id"])
	}
}

func (s *codexExecIngestState) appendEvent(run RunStatus, kind string, payload map[string]any) (bool, error) {
	key := codexExecIngestEventKey(kind, payload)
	found, err := s.hasEvent(run, key)
	if err != nil {
		return false, err
	}
	if found {
		return false, nil
	}
	if s.eventLog != nil {
		if err := s.eventLog.Append(kind, run.ActiveAttemptID, RunnerCodexExec, payload); err != nil {
			return false, err
		}
	}
	s.rememberEvent(key)
	return true, nil
}

func (s *codexExecIngestState) turn(store *RuntimeStore, run RunStatus, turnID string) (codexExecTurnSnapshot, error) {
	snapshot, ok := s.known[turnID]
	if !ok {
		found := false
		if s.turnOverflow {
			var startedAt string
			err := store.queryRowScan(`SELECT turn_index, status, input_tokens, output_tokens, total_tokens, started_at FROM turns WHERE attempt_id = ? AND turn_id = ?`, []any{run.ActiveAttemptID, turnID}, &snapshot.index, &snapshot.status, &snapshot.inputTokens, &snapshot.outputTokens, &snapshot.totalTokens, &startedAt)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return snapshot, err
			}
			found, snapshot.started = err == nil, startedAt != ""
		}
		if !found {
			snapshot.index = s.nextIndex
			s.nextIndex++
		}
		s.rememberTurn(turnID, snapshot)
	}
	return snapshot, nil
}

func (s *codexExecIngestState) rememberTurn(turnID string, snapshot codexExecTurnSnapshot) {
	if _, exists := s.known[turnID]; !exists && len(s.known) >= codexExecIngestTurnLimit {
		for old := range s.known {
			delete(s.known, old)
			break
		}
		s.turnOverflow = true
	}
	s.known[turnID] = snapshot
}

func (s *codexExecIngestState) saveTurn(store *RuntimeStore, turn RunTurn, snapshot codexExecTurnSnapshot) error {
	if err := store.SaveTurn(turn); err != nil {
		return fmt.Errorf("save codex-exec turn: %w", err)
	}
	s.stats.turnWrites++
	s.rememberTurn(turn.TurnID, snapshot)
	return nil
}

// A bounded boundary check catches copy-truncate followed by rapid regrowth
// even when the inode and final size look like an ordinary append.
func (s *codexExecIngestState) rawBoundaryMatches(file *os.File) (bool, error) {
	if len(s.rawBoundary) == 0 {
		return true, nil
	}
	boundary := make([]byte, len(s.rawBoundary))
	n, err := file.ReadAt(boundary, s.rawOffset-int64(len(boundary)))
	s.stats.rawBoundaryBytes += int64(n)
	if errors.Is(err, io.EOF) {
		return false, nil
	}
	return bytes.Equal(boundary, s.rawBoundary), err
}

func (s *codexExecIngestState) advanceRaw(line []byte) {
	s.rawOffset += int64(len(line))
	s.rawLine++
	s.rawBoundary = codexExecIngestBoundary(s.rawBoundary, line)
}

func codexExecIngestBoundary(boundary, line []byte) []byte {
	if len(line) >= 128 {
		return append(boundary[:0], line[len(line)-128:]...)
	} else {
		boundary = append(boundary, line...)
		if len(boundary) > 128 {
			boundary = append([]byte(nil), boundary[len(boundary)-128:]...)
		}
	}
	return boundary
}

func readCodexExecIngestLine(reader *bufio.Reader) ([]byte, error) {
	var line []byte
	for {
		fragment, err := reader.ReadSlice('\n')
		if int64(len(line)+len(fragment)) > completionAuthoritativeRawLogMaxBytes {
			return nil, fmt.Errorf("codex-exec JSONL record exceeds %d bytes", completionAuthoritativeRawLogMaxBytes)
		}
		line = append(line, fragment...)
		if !errors.Is(err, bufio.ErrBufferFull) {
			return line, err
		}
	}
}

func (d *Daemon) codexExecObservedRefs(run RunStatus) (sessionRef, messageRef string) {
	d.codexExecIngestMu.Lock()
	defer d.codexExecIngestMu.Unlock()
	if state := d.codexExecIngest[run.ActiveAttemptID]; state != nil && state.rawPath == run.RawLogPath {
		return firstNonEmpty(run.SessionRef, state.sessionRef), state.messageRef
	}
	return run.SessionRef, ""
}
