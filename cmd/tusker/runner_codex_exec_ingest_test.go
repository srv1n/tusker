package main

import (
	"bufio"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func TestCodexExecIncrementalIngestReadsOnlyAppend(t *testing.T) {
	store, daemon, run, _ := codexExecGovernorFixture(t)
	defer store.Close()
	assertCodexExecIngest(t, daemon, run, true)
	state := daemon.codexExecIngest[run.ActiveAttemptID]
	before := state.stats
	turnsBefore, err := store.ListTurnsForAttempt(run.ActiveAttemptID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`CREATE TRIGGER refuse_replayed_turn BEFORE UPDATE ON turns BEGIN SELECT RAISE(ABORT, 'historical turn rewrite'); END`); err != nil {
		t.Fatal(err)
	}
	assertCodexExecIngest(t, daemon, run, false)
	assertEqual(t, before, state.stats, "unchanged poll reads and writes")
	appended := codexExecCompletedTurnJSONL("turn-3")
	appendCodexExecLogForTest(t, run.RawLogPath, appended)
	assertCodexExecIngest(t, daemon, run, true)
	assertEqual(t, int64(len(appended)), state.stats.rawBytes-before.rawBytes, "appended raw bytes read")
	assertEqual(t, int64(1), state.stats.rawRecords-before.rawRecords, "appended raw records")
	assertEqual(t, before.turnReads, state.stats.turnReads, "cached turn reads")
	assertEqual(t, int64(1), state.stats.turnWrites-before.turnWrites, "new turn writes")
	before = state.stats
	assertCodexExecIngest(t, daemon, run, false)
	assertEqual(t, before, state.stats, "second unchanged poll reads and writes")
	turnsAfter, err := store.ListTurnsForAttempt(run.ActiveAttemptID)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, turnsBefore[0].LastEventAt, turnsAfter[0].LastEventAt, "historical turn timestamp")
	assertCodexExecEventCount(t, run.EventSinkPath, "turn_completed", 3)
}

func TestCodexExecIncrementalIngestWaitsForCompleteRecord(t *testing.T) {
	store, daemon, run, _ := codexExecGovernorFixture(t)
	defer store.Close()
	thread := `{"type":"thread.started","thread_id":"session-governor"}` + "\n"
	complete := codexExecCompletedTurnJSONL("partial-turn")
	if err := writeText(run.RawLogPath, thread+strings.TrimSuffix(complete, "\n")); err != nil {
		t.Fatal(err)
	}
	assertCodexExecIngest(t, daemon, run, true)
	state := daemon.codexExecIngest[run.ActiveAttemptID]
	assertEqual(t, int64(len(thread)), state.rawOffset, "cursor before trailing record")
	assertEqual(t, 0, len(state.known), "complete JSON without newline is pending")
	before := state.stats
	assertCodexExecIngest(t, daemon, run, false)
	assertEqual(t, before, state.stats, "unchanged partial tail is not reread")
	appendCodexExecLogForTest(t, run.RawLogPath, "\n")
	assertCodexExecIngest(t, daemon, run, true)
	assertEqual(t, int64(len(thread+complete)), state.rawOffset, "cursor after complete trailing record")
	assertCodexExecEventCount(t, run.EventSinkPath, "turn_completed", 1)

	// Restart also waits for a split JSON record and then emits it once.
	daemon = &Daemon{stateRoot: daemon.stateRoot, store: store}
	assertCodexExecIngest(t, daemon, run, false)
	assertEqual(t, int64(0), daemon.codexExecIngest[run.ActiveAttemptID].stats.turnWrites, "restart does not rewrite completed turns")
	next := codexExecCompletedTurnJSONL("split-turn")
	appendCodexExecLogForTest(t, run.RawLogPath, next[:len(next)/2])
	assertCodexExecIngest(t, daemon, run, false)
	appendCodexExecLogForTest(t, run.RawLogPath, next[len(next)/2:])
	assertCodexExecIngest(t, daemon, run, true)
	assertCodexExecIngest(t, daemon, run, false)
	assertCodexExecEventCount(t, run.EventSinkPath, "turn_completed", 2)
}

func TestCodexExecIncrementalIngestReplaysTruncateAndReplace(t *testing.T) {
	for _, mode := range []string{"truncate", "replace", "same-size", "truncate-regrow"} {
		t.Run(mode, func(t *testing.T) {
			store, daemon, run, _ := codexExecGovernorFixture(t)
			defer store.Close()
			assertCodexExecIngest(t, daemon, run, true)
			original, err := readText(run.RawLogPath)
			if err != nil {
				t.Fatal(err)
			}
			var replacement string
			switch mode {
			case "same-size":
				replacement = strings.ReplaceAll(original, "turn-2", "turn-3")
			case "truncate-regrow":
				replacement = codexExecCompletedTurnJSONL("turn-3") + `{"type":"diagnostic","message":"` + strings.Repeat("padding", len(original)) + `"}` + "\n"
			default:
				replacement = codexExecCompletedTurnJSONL("turn-3")
			}
			if mode == "replace" {
				if err := writeText(run.RawLogPath+".replacement", replacement); err != nil {
					t.Fatal(err)
				}
				if err := os.Rename(run.RawLogPath+".replacement", run.RawLogPath); err != nil {
					t.Fatal(err)
				}
			} else if err := writeText(run.RawLogPath, replacement); err != nil {
				t.Fatal(err)
			}
			// Ensure a same-size rewrite has a distinct filesystem timestamp.
			when := time.Now().Add(time.Second)
			if err := os.Chtimes(run.RawLogPath, when, when); err != nil {
				t.Fatal(err)
			}
			assertCodexExecIngest(t, daemon, run, true)
			assertCodexExecIngest(t, daemon, run, false)
			assertCodexExecEventCount(t, run.EventSinkPath, "turn_completed", 3)
			daemon = &Daemon{stateRoot: daemon.stateRoot, store: store}
			assertCodexExecIngest(t, daemon, run, false)
			assertEqual(t, int64(0), daemon.codexExecIngest[run.ActiveAttemptID].stats.turnWrites, "replacement restart does not rewrite turns")
			assertCodexExecEventCount(t, run.EventSinkPath, "turn_completed", 3)
		})
	}
}

func TestCodexExecIncrementalIngestRetriesFailedTurnSave(t *testing.T) {
	store, daemon, run, _ := codexExecGovernorFixture(t)
	defer store.Close()
	if _, err := store.db.Exec(`CREATE TRIGGER refuse_turn_save BEFORE INSERT ON turns BEGIN SELECT RAISE(ABORT, 'forced turn failure'); END`); err != nil {
		t.Fatal(err)
	}
	changed, err := daemon.ingestCodexExecRawLog(run)
	if !changed || err == nil || !strings.Contains(err.Error(), "save codex-exec turn") {
		t.Fatalf("turn persistence failure was hidden: changed=%t err=%v", changed, err)
	}
	state := daemon.codexExecIngest[run.ActiveAttemptID]
	assertEqual(t, int64(1), state.rawLine, "failed turn record remains pending")
	assertCodexExecEventCount(t, run.EventSinkPath, "turn_started", 1)
	if _, err := store.db.Exec(`DROP TRIGGER refuse_turn_save`); err != nil {
		t.Fatal(err)
	}
	// Throw away the in-memory cache to prove the durable event supplies dedup.
	daemon = &Daemon{stateRoot: daemon.stateRoot, store: store}
	assertCodexExecIngest(t, daemon, run, true)
	assertCodexExecIngest(t, daemon, run, false)
	assertCodexExecEventCount(t, run.EventSinkPath, "thread_started", 1)
	assertCodexExecEventCount(t, run.EventSinkPath, "turn_started", 2)
	assertCodexExecEventCount(t, run.EventSinkPath, "turn_completed", 2)
	turns, err := store.ListTurnsForAttempt(run.ActiveAttemptID)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, 0, turns[0].TurnIndex, "failed save keeps first turn index")
}

func TestCodexExecIncrementalIngestRetriesDurableAppendError(t *testing.T) {
	store, daemon, run, _ := codexExecGovernorFixture(t)
	defer store.Close()
	if err := writeText(run.RawLogPath, ""); err != nil {
		t.Fatal(err)
	}
	assertCodexExecIngest(t, daemon, run, false)
	state := daemon.codexExecIngest[run.ActiveAttemptID]
	state.eventLog.syncFn = func(*os.File) error { return errors.New("forced sync failure") }
	record := `{"type":"turn.completed","turn_id":"command-turn","item":{"id":"command-1","type":"commandExecution","command":"go test ./...","status":"completed","exit_code":0,"aggregated_output":"PASS"},"token_usage":{"input_tokens":2,"output_tokens":1,"total_tokens":3}}` + "\n"
	appendCodexExecLogForTest(t, run.RawLogPath, record)
	if _, err := daemon.ingestCodexExecRawLog(run); err == nil || !strings.Contains(err.Error(), "forced sync failure") {
		t.Fatalf("expected durable append error, got %v", err)
	}
	assertEqual(t, int64(0), state.rawOffset, "failed command record remains pending")
	assertCodexExecEventCount(t, run.EventSinkPath, "command_completed", 1)
	state.eventLog.syncFn = nil
	assertCodexExecIngest(t, daemon, run, true)
	assertCodexExecIngest(t, daemon, run, false)
	assertCodexExecEventCount(t, run.EventSinkPath, "command_completed", 1)
	assertCodexExecEventCount(t, run.EventSinkPath, "turn_completed", 1)
	events := readReviewPacketEvents(run.EventSinkPath)
	command := reviewPacketEventPayload(events[0])
	assertEqual(t, fmt.Sprintf("sha256:%x", sha256.Sum256([]byte("PASS"))), stringValue(command["output_sha256"]), "command proof preserved")
	daemon = &Daemon{stateRoot: daemon.stateRoot, store: store}
	assertCodexExecIngest(t, daemon, run, false)
	assertEqual(t, int64(0), daemon.codexExecIngest[run.ActiveAttemptID].stats.turnWrites, "durable error restart does not rewrite turns")
	assertCodexExecEventCount(t, run.EventSinkPath, "command_completed", 1)
}

func TestCodexExecIncrementalIngestKeepsCursorOnPartialAppendError(t *testing.T) {
	store, daemon, run, _ := codexExecGovernorFixture(t)
	defer store.Close()
	if err := writeText(run.RawLogPath, ""); err != nil {
		t.Fatal(err)
	}
	assertCodexExecIngest(t, daemon, run, false)
	state := daemon.codexExecIngest[run.ActiveAttemptID]
	state.eventLog.writeFn = func(file *os.File, record []byte) (int, error) {
		n, _ := file.Write(record[:len(record)/2])
		return n, errors.New("forced partial append")
	}
	appendCodexExecLogForTest(t, run.RawLogPath, codexExecCompletedTurnJSONL("failed-append-turn"))
	if _, err := daemon.ingestCodexExecRawLog(run); err == nil || !strings.Contains(err.Error(), "forced partial append") {
		t.Fatalf("expected partial append error, got %v", err)
	}
	assertEqual(t, int64(0), state.rawOffset, "partial append cursor")
	state.eventLog.writeFn = nil
	if _, err := daemon.ingestCodexExecRawLog(run); err == nil || !strings.Contains(err.Error(), "partial trailing record") {
		t.Fatalf("partial proof must fail closed, got %v", err)
	}
	assertEqual(t, int64(0), state.rawOffset, "partial proof cursor remains pending")
	// Repair only the failed tail; replay can now recover its pending raw event.
	if err := os.Truncate(run.EventSinkPath, 0); err != nil {
		t.Fatal(err)
	}
	assertCodexExecIngest(t, daemon, run, true)
	assertCodexExecIngest(t, daemon, run, false)
	assertCodexExecEventCount(t, run.EventSinkPath, "turn_completed", 1)
}

func TestCodexExecIncrementalIngestValidatesChangedSinkOnIdlePoll(t *testing.T) {
	store, daemon, run, _ := codexExecGovernorFixture(t)
	defer store.Close()
	assertCodexExecIngest(t, daemon, run, true)
	state := daemon.codexExecIngest[run.ActiveAttemptID]
	before := state.stats
	appendCodexExecLogForTest(t, run.EventSinkPath, `{"seq":42`)
	if _, err := daemon.ingestCodexExecRawLog(run); err == nil || !strings.Contains(err.Error(), "partial trailing record") {
		t.Fatalf("idle poll missed corrupt appended sink, got %v", err)
	}
	assertEqual(t, before.rawBytes, state.stats.rawBytes, "idle sink validation does not read raw log")
	assertEqual(t, before.turnWrites, state.stats.turnWrites, "idle sink validation does not write turns")
}

func TestCodexExecIncrementalIngestRecordsStartAfterEarlyUsage(t *testing.T) {
	store, daemon, run, _ := codexExecGovernorFixture(t)
	defer store.Close()
	started := "2026-10-15T00:00:01Z"
	completed := "2026-10-15T00:00:02Z"
	raw := `{"type":"turn.usage","turn_id":"early-usage","token_usage":{"input_tokens":5,"output_tokens":1,"total_tokens":6}}` + "\n" +
		`{"type":"turn.started","turn_id":"early-usage","timestamp":"` + started + `"}` + "\n" +
		`{"type":"turn.completed","turn_id":"early-usage","timestamp":"` + completed + `"}` + "\n"
	if err := writeText(run.RawLogPath, raw); err != nil {
		t.Fatal(err)
	}
	assertCodexExecIngest(t, daemon, run, true)
	turns, err := store.ListTurnsForAttempt(run.ActiveAttemptID)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, started, turns[0].StartedAt, "early usage retains subsequent start timestamp")
	assertEqual(t, completed, turns[0].LastEventAt, "terminal event timestamp")
	daemon = &Daemon{stateRoot: daemon.stateRoot, store: store}
	assertCodexExecIngest(t, daemon, run, false)
	assertEqual(t, int64(0), daemon.codexExecIngest[run.ActiveAttemptID].stats.turnWrites, "timestamp restart does not rewrite turns")
}

func TestCodexExecIncrementalIngestBoundsCachesWithoutLosingDedup(t *testing.T) {
	store, daemon, run, _ := codexExecGovernorFixture(t)
	defer store.Close()
	var raw strings.Builder
	for i := 0; i <= codexExecIngestTurnLimit; i++ {
		raw.WriteString(codexExecCompletedTurnJSONL(fmt.Sprintf("turn-%03d", i)))
	}
	command := `{"type":"item.completed","item":{"id":"old-command","type":"commandExecution","command":"true","status":"completed","exit_code":0}}` + "\n"
	raw.WriteString(command)
	if err := writeText(run.RawLogPath, raw.String()); err != nil {
		t.Fatal(err)
	}
	assertCodexExecIngest(t, daemon, run, true)
	state := daemon.codexExecIngest[run.ActiveAttemptID]
	assertEqual(t, codexExecIngestTurnLimit, len(state.known), "bounded turn cache")
	for i := 0; i <= codexExecIngestEventLimit; i++ {
		state.rememberEvent(fmt.Sprintf("command_completed:synthetic-%d", i))
	}
	assertEqual(t, codexExecIngestEventLimit, len(state.events), "bounded event cache")
	delete(state.events, "command_completed:old-command")
	delete(state.known, "turn-000")
	before := state.stats
	appendCodexExecLogForTest(t, run.RawLogPath, command+codexExecCompletedTurnJSONL("turn-000"))
	assertCodexExecIngest(t, daemon, run, false)
	assertEqual(t, before.turnWrites, state.stats.turnWrites, "evicted historical turn is not rewritten")
	if state.stats.eventReads <= before.eventReads {
		t.Fatal("expected exact durable lookup for evicted event")
	}
	assertCodexExecEventCount(t, run.EventSinkPath, "command_completed", 1)
	assertCodexExecEventCount(t, run.EventSinkPath, "turn_completed", codexExecIngestTurnLimit+1)
	before = state.stats
	assertCodexExecIngest(t, daemon, run, false)
	assertEqual(t, before, state.stats, "eviction does not add work to unchanged polls")
	daemon = &Daemon{stateRoot: daemon.stateRoot, store: store}
	assertCodexExecIngest(t, daemon, run, false)
	assertEqual(t, codexExecIngestTurnLimit, len(daemon.codexExecIngest[run.ActiveAttemptID].known), "bounded restart turn cache")
}

func TestCodexExecIncrementalIngestRejectsOversizedRecord(t *testing.T) {
	reader := bufio.NewReader(strings.NewReader(strings.Repeat("x", int(completionAuthoritativeRawLogMaxBytes)+1)))
	if _, err := readCodexExecIngestLine(reader); err == nil || !strings.Contains(err.Error(), "JSONL record exceeds") {
		t.Fatalf("expected surfaced record limit, got %v", err)
	}
}

func assertCodexExecIngest(t *testing.T, daemon *Daemon, run RunStatus, expected bool) {
	t.Helper()
	changed, err := daemon.ingestCodexExecRawLog(run)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, expected, changed, "codex exec ingestion changed")
}

func appendCodexExecLogForTest(t *testing.T, path, text string) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(text); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func assertCodexExecEventCount(t *testing.T, path, kind string, expected int) {
	t.Helper()
	text, err := readText(path)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, expected, strings.Count(text, `"kind":"`+kind+`"`), kind+" event count")
}

func codexExecCompletedTurnJSONL(turnID string) string {
	return `{"type":"turn.completed","turn_id":"` + turnID + `","token_usage":{"input_tokens":5,"output_tokens":1,"total_tokens":6}}` + "\n"
}
