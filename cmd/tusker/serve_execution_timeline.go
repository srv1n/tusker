package main

// Execution timelines are a read model over immutable provider observations.
// Stream events only tell a client to fetch this model; they are never a
// checkpoint or an authority boundary.

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

const executionTimelineSchema = "tusker.execution-timeline/v1"
const executionTimelineDefaultLimit = 100
const executionTimelineMaxLimit = 250

type executionTimelineSourceCursor struct {
	Epoch    string `json:"epoch"`
	Sequence int64  `json:"sequence"`
}

// executionTimelineCursor is deliberately a vector, not a row pointer. A root
// or wave is a fan-in projection, so only a per-source checkpoint can prove a
// client has reached every source's committed tail.
type executionTimelineCursor struct {
	Version              int                                      `json:"v"`
	Sources              map[string]executionTimelineSourceCursor `json:"sources"`
	BeforeOccurredAt     string                                   `json:"before_occurred_at,omitempty"`
	BeforeExecutionID    string                                   `json:"before_execution_id,omitempty"`
	BeforeSourceSequence int64                                    `json:"before_source_sequence,omitempty"`
}
type executionTimelineAppend struct {
	ProjectID, ExecutionID, Provider, ProviderEventID, ObservationID, OccurredAt, ReceivedAt, Status string
	SourceSequence                                                                                   int64
	Authoritative                                                                                    bool
}
type ExecutionTimelineRow struct {
	SourceExecutionID string `json:"source_execution_id"`
	SourceEpoch       string `json:"source_epoch"`
	SourceSequence    int64  `json:"source_sequence"`
	Provider          string `json:"provider"`
	ProviderEventID   string `json:"provider_event_id,omitempty"`
	ObservationID     string `json:"observation_id"`
	OccurredAt        string `json:"occurred_at"`
	Status            string `json:"status"`
	Authoritative     bool   `json:"authoritative"`
	Provenance        string `json:"provenance"`
	Cursor            string `json:"cursor"`
}
type ExecutionTimelinePage struct {
	Schema         string                 `json:"schema"`
	Rows           []ExecutionTimelineRow `json:"rows"`
	CommittedTail  string                 `json:"committed_tail"`
	NextCursor     string                 `json:"next_cursor,omitempty"`
	PreviousCursor string                 `json:"previous_cursor,omitempty"`
	HasOlder       bool                   `json:"has_older"`
	HasNewer       bool                   `json:"has_newer"`
	Reset          bool                   `json:"reset"`
	Gap            bool                   `json:"gap"`
	StaleCursor    bool                   `json:"stale_cursor"`
	Recovery       string                 `json:"recovery,omitempty"`
}

func (s *RuntimeStore) migrateExecutionTimelines() error {
	for _, stmt := range []string{
		`CREATE TABLE IF NOT EXISTS execution_timeline_sources (execution_id TEXT PRIMARY KEY, project_id TEXT NOT NULL, epoch TEXT NOT NULL, next_sequence INTEGER NOT NULL DEFAULT 0, created_at TEXT NOT NULL);`,
		`CREATE TABLE IF NOT EXISTS execution_timeline_events (execution_id TEXT NOT NULL, epoch TEXT NOT NULL, sequence INTEGER NOT NULL, provider TEXT NOT NULL, provider_event_id TEXT NOT NULL DEFAULT '', observation_id TEXT NOT NULL UNIQUE, source_sequence INTEGER NOT NULL DEFAULT 0, occurred_at TEXT NOT NULL, received_at TEXT NOT NULL, status TEXT NOT NULL, authoritative INTEGER NOT NULL, PRIMARY KEY(execution_id, epoch, sequence));`,
		`CREATE INDEX IF NOT EXISTS execution_timeline_events_order ON execution_timeline_events(execution_id, epoch, sequence);`,
	} {
		if _, err := s.exec(stmt); err != nil {
			return err
		}
	}
	return nil
}

func (s *RuntimeStore) appendExecutionTimelineEventTx(tx *sql.Tx, a executionTimelineAppend) error {
	if strings.TrimSpace(a.ExecutionID) == "" {
		return fmt.Errorf("execution timeline requires execution")
	}
	var epoch string
	err := tx.QueryRow(`SELECT epoch FROM execution_timeline_sources WHERE execution_id = ?`, a.ExecutionID).Scan(&epoch)
	if err == sql.ErrNoRows {
		epoch = "epoch-" + strings.ToLower(newRecordID())
		if _, err = tx.Exec(`INSERT INTO execution_timeline_sources(execution_id, project_id, epoch, next_sequence, created_at) VALUES(?,?,?,?,?)`, a.ExecutionID, a.ProjectID, epoch, 0, a.ReceivedAt); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	var existing string
	if err = tx.QueryRow(`SELECT observation_id FROM execution_timeline_events WHERE observation_id = ?`, a.ObservationID).Scan(&existing); err == nil {
		return nil
	} else if err != sql.ErrNoRows {
		return err
	}
	var seq int64
	if err = tx.QueryRow(`UPDATE execution_timeline_sources SET next_sequence = next_sequence + 1 WHERE execution_id = ? RETURNING next_sequence`, a.ExecutionID).Scan(&seq); err != nil {
		return err
	}
	_, err = tx.Exec(`INSERT INTO execution_timeline_events(execution_id, epoch, sequence, provider, provider_event_id, observation_id, source_sequence, occurred_at, received_at, status, authoritative) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, a.ExecutionID, epoch, seq, a.Provider, a.ProviderEventID, a.ObservationID, a.SourceSequence, a.OccurredAt, a.ReceivedAt, a.Status, boolToInt(a.Authoritative))
	return err
}

func encodeExecutionTimelineCursor(c executionTimelineCursor) string {
	c.Version = 2
	if c.Sources == nil {
		c.Sources = map[string]executionTimelineSourceCursor{}
	}
	raw, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(raw)
}
func decodeExecutionTimelineCursor(raw string) (executionTimelineCursor, error) {
	var c executionTimelineCursor
	b, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return c, err
	}
	if err = json.Unmarshal(b, &c); err == nil && c.Version == 2 && len(c.Sources) > 0 {
		return c, nil
	}
	// v1 was a single-source cursor. Accept it as a one-source vector; a
	// multi-source fetch will explicitly reset rather than silently skip peers.
	var legacy struct {
		ExecutionID, Epoch string
		Sequence           int64
	}
	if json.Unmarshal(b, &legacy) == nil && legacy.ExecutionID != "" && legacy.Epoch != "" && legacy.Sequence > 0 {
		return executionTimelineCursor{Version: 1, Sources: map[string]executionTimelineSourceCursor{legacy.ExecutionID: {Epoch: legacy.Epoch, Sequence: legacy.Sequence}}}, nil
	}
	return c, fmt.Errorf("invalid execution timeline cursor")
}

// ExecutionTimeline fetches a root/wave/execution projection. Direction is
// tail, before, or after. A reset/recovery answer makes incomplete history
// visible rather than pretending an empty page was complete.
func (s *RuntimeStore) ExecutionTimeline(projectID, executionID, waveID, direction, rawCursor string, limit int) (ExecutionTimelinePage, error) {
	p := ExecutionTimelinePage{Schema: executionTimelineSchema, Rows: []ExecutionTimelineRow{}}
	if limit <= 0 {
		limit = executionTimelineDefaultLimit
	}
	if limit > executionTimelineMaxLimit {
		limit = executionTimelineMaxLimit
	}
	direction = strings.ToLower(strings.TrimSpace(direction))
	if direction == "" {
		direction = "tail"
	}
	if direction != "tail" && direction != "before" && direction != "after" {
		return p, tuskerError(errorInvalidArg, "timeline direction must be tail, before, or after")
	}
	ids, err := s.executionTimelineSources(projectID, executionID, waveID)
	if err != nil {
		return p, err
	}
	if len(ids) == 0 {
		return p, nil
	}
	cursor := executionTimelineCursor{Version: 2, Sources: map[string]executionTimelineSourceCursor{}}
	if rawCursor != "" {
		cursor, err = decodeExecutionTimelineCursor(rawCursor)
		if err != nil {
			p.StaleCursor, p.Reset, p.Recovery = true, true, "fetch_tail"
			return p, nil
		}
	}
	tails := map[string]executionTimelineSourceCursor{}
	mins := map[string]int64{}
	for _, id := range ids {
		var epoch string
		var max sql.NullInt64
		err := s.queryRowScan(`SELECT t.epoch, MAX(e.sequence) FROM execution_timeline_sources t LEFT JOIN execution_timeline_events e ON e.execution_id=t.execution_id AND e.epoch=t.epoch WHERE t.execution_id=? GROUP BY t.epoch`, []any{id}, &epoch, &max)
		if err == sql.ErrNoRows {
			continue
		}
		if err != nil {
			return p, err
		}
		if max.Valid {
			tails[id] = executionTimelineSourceCursor{Epoch: epoch, Sequence: max.Int64}
		} else {
			tails[id] = executionTimelineSourceCursor{Epoch: epoch}
		}
		var min sql.NullInt64
		if err := s.queryRowScan(`SELECT MIN(sequence) FROM execution_timeline_events WHERE execution_id=? AND epoch=?`, []any{id, epoch}, &min); err != nil {
			return p, err
		}
		if min.Valid {
			mins[id] = min.Int64
		}
	}
	p.CommittedTail = encodeExecutionTimelineCursor(executionTimelineCursor{Sources: tails})
	if rawCursor != "" {
		if len(cursor.Sources) != len(tails) {
			p.StaleCursor, p.Reset, p.Gap, p.Recovery = true, true, true, "fetch_tail"
			return p, nil
		}
		for id, tail := range tails {
			seen, ok := cursor.Sources[id]
			if !ok || seen.Epoch != tail.Epoch || seen.Sequence < 0 || seen.Sequence > tail.Sequence {
				p.StaleCursor, p.Reset, p.Gap, p.Recovery = true, true, true, "fetch_tail"
				return p, nil
			}
			if min, ok := mins[id]; ok && seen.Sequence < min-1 {
				p.Gap, p.Reset, p.Recovery = true, true, "fetch_tail"
				return p, nil
			}
		}
	}
	for _, id := range ids {
		tail := tails[id]
		rows, err := s.query(`SELECT execution_id, epoch, sequence, provider, provider_event_id, observation_id, occurred_at, status, authoritative FROM execution_timeline_events WHERE execution_id=? AND epoch=?`, id, tail.Epoch)
		if err != nil {
			return p, err
		}
		for rows.Next() {
			var r ExecutionTimelineRow
			var auth int
			if err := rows.Scan(&r.SourceExecutionID, &r.SourceEpoch, &r.SourceSequence, &r.Provider, &r.ProviderEventID, &r.ObservationID, &r.OccurredAt, &r.Status, &auth); err != nil {
				rows.Close()
				return p, err
			}
			r.Authoritative = auth != 0
			r.Provenance = "authoritative"
			if !r.Authoritative {
				r.Provenance = "provisional"
			}
			p.Rows = append(p.Rows, r)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return p, err
		}
		rows.Close()
	}
	sort.Slice(p.Rows, func(i, j int) bool {
		if p.Rows[i].OccurredAt != p.Rows[j].OccurredAt {
			return p.Rows[i].OccurredAt < p.Rows[j].OccurredAt
		}
		if p.Rows[i].SourceExecutionID != p.Rows[j].SourceExecutionID {
			return p.Rows[i].SourceExecutionID < p.Rows[j].SourceExecutionID
		}
		return p.Rows[i].SourceSequence < p.Rows[j].SourceSequence
	})
	// Preserve the complete projection prefix for each global row.  We assign
	// those checkpoints only after filtering and bounds selection below; doing
	// it earlier would expose a cursor for a row that is not actually returned.
	rowCheckpoints := map[string]map[string]executionTimelineSourceCursor{}
	rowCheckpoint := map[string]executionTimelineSourceCursor{}
	for id, tail := range tails {
		rowCheckpoint[id] = executionTimelineSourceCursor{Epoch: tail.Epoch}
	}
	for _, r := range p.Rows {
		rowCheckpoint[r.SourceExecutionID] = executionTimelineSourceCursor{Epoch: r.SourceEpoch, Sequence: r.SourceSequence}
		rowCheckpoints[r.ObservationID] = cloneExecutionTimelineCursorSources(rowCheckpoint)
	}
	allRows := len(p.Rows)
	if rawCursor != "" {
		filtered := p.Rows[:0]
		for _, r := range p.Rows {
			cp := cursor.Sources[r.SourceExecutionID]
			if direction == "after" && r.SourceSequence > cp.Sequence {
				filtered = append(filtered, r)
			}
			if direction == "before" && executionTimelineBefore(r, cursor) {
				filtered = append(filtered, r)
			}
		}
		p.Rows = filtered
		if direction == "before" && len(p.Rows) < allRows {
			p.HasNewer = true
		}
		if direction == "after" && len(p.Rows) < allRows {
			p.HasOlder = true
		}
	}
	if direction == "tail" && len(p.Rows) > limit {
		p.Rows = p.Rows[len(p.Rows)-limit:]
		p.HasOlder = true
	}
	if direction == "after" && len(p.Rows) > limit {
		p.Rows = p.Rows[:limit]
		p.HasNewer = true
	}
	if direction == "before" && len(p.Rows) > limit {
		p.Rows = p.Rows[len(p.Rows)-limit:]
		p.HasOlder = true
	}
	if len(p.Rows) > 0 {
		for i := range p.Rows {
			// Indexed assignment is intentional: range values are copies.
			p.Rows[i].Cursor = encodeExecutionTimelineCursor(executionTimelineCursor{Sources: rowCheckpoints[p.Rows[i].ObservationID]})
		}
		next := map[string]executionTimelineSourceCursor{}
		for id, checkpoint := range cursor.Sources {
			next[id] = checkpoint
		}
		if direction == "tail" {
			for id, tail := range tails {
				next[id] = tail
			}
		} else {
			for _, r := range p.Rows {
				next[r.SourceExecutionID] = executionTimelineSourceCursor{Epoch: r.SourceEpoch, Sequence: r.SourceSequence}
			}
		}
		p.NextCursor = encodeExecutionTimelineCursor(executionTimelineCursor{Sources: next})
		first := p.Rows[0]
		p.PreviousCursor = encodeExecutionTimelineCursor(executionTimelineCursor{Sources: tails, BeforeOccurredAt: first.OccurredAt, BeforeExecutionID: first.SourceExecutionID, BeforeSourceSequence: first.SourceSequence})
	}
	return p, nil
}

func executionTimelineBefore(row ExecutionTimelineRow, cursor executionTimelineCursor) bool {
	// Page-boundary cursors carry a global sort anchor. This keeps `before`
	// exclusive across every source in a fan-in projection. Old vector cursors
	// without an anchor retain conservative per-source semantics.
	if cursor.BeforeOccurredAt == "" {
		return row.SourceSequence < cursor.Sources[row.SourceExecutionID].Sequence
	}
	if row.OccurredAt != cursor.BeforeOccurredAt {
		return row.OccurredAt < cursor.BeforeOccurredAt
	}
	if row.SourceExecutionID != cursor.BeforeExecutionID {
		return row.SourceExecutionID < cursor.BeforeExecutionID
	}
	return row.SourceSequence < cursor.BeforeSourceSequence
}

func cloneExecutionTimelineCursorSources(in map[string]executionTimelineSourceCursor) map[string]executionTimelineSourceCursor {
	out := make(map[string]executionTimelineSourceCursor, len(in))
	for id, source := range in {
		out[id] = source
	}
	return out
}

func (s *RuntimeStore) executionTimelineSources(projectID, executionID, waveID string) ([]string, error) {
	q := `SELECT execution_id FROM execution_records WHERE project_id=?`
	args := []any{projectID}
	if executionID != "" {
		q += ` AND (execution_id=? OR root_execution_id=?)`
		args = append(args, executionID, executionID)
	} else if waveID != "" {
		q += ` AND (wave_id=? OR execution_id IN (SELECT execution_id FROM execution_binding_events WHERE wave_id=? AND action != 'detach'))`
		args = append(args, waveID, waveID)
	}
	q += ` ORDER BY execution_id`
	rows, err := s.query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
func projectIDOr(primary, fallback string) string {
	if strings.TrimSpace(primary) != "" {
		return primary
	}
	return fallback
}
