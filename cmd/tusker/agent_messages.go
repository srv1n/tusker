package main

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

const agentMessageBodyLimit = 32 << 10

type AgentAddress struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

type AgentMessage struct {
	ID                  string       `json:"id"`
	IdempotencyKey      string       `json:"idempotencyKey"`
	ProjectID           string       `json:"projectId"`
	Sender              string       `json:"sender"`
	Recipient           AgentAddress `json:"recipient"`
	OriginTaskID        string       `json:"originTaskId,omitempty"`
	OriginWaveID        string       `json:"originWaveId,omitempty"`
	WorkRevision        int          `json:"workRevision,omitempty"`
	RouteGeneration     int          `json:"routeGeneration,omitempty"`
	RecipientGeneration int          `json:"recipientGeneration,omitempty"`
	Kind                string       `json:"kind"`
	Body                string       `json:"body"`
	ReplyTo             string       `json:"replyTo,omitempty"`
	ReplyRequired       bool         `json:"replyRequired"`
	YieldSender         bool         `json:"yieldSender"`
	State               string       `json:"state"`
	TransportState      string       `json:"transportState"`
	ConsumedAt          string       `json:"consumedAt,omitempty"`
	AnsweredAt          string       `json:"answeredAt,omitempty"`
	AppliedAt           string       `json:"appliedAt,omitempty"`
	ExpiresAt           string       `json:"expiresAt,omitempty"`
	CreatedAt           string       `json:"createdAt"`
}

func (m AgentMessage) validate() error {
	m.ProjectID, m.Sender, m.IdempotencyKey = strings.TrimSpace(m.ProjectID), strings.TrimSpace(m.Sender), strings.TrimSpace(m.IdempotencyKey)
	m.Recipient.Kind, m.Recipient.ID, m.Body = strings.TrimSpace(m.Recipient.Kind), strings.TrimSpace(m.Recipient.ID), strings.TrimSpace(m.Body)
	if m.ProjectID == "" || m.Sender == "" || m.IdempotencyKey == "" || m.Recipient.ID == "" || m.Body == "" {
		return errors.New("project, sender, idempotency key, recipient and body are required")
	}
	if m.Recipient.Kind != "task" && m.Recipient.Kind != "execution" && (m.Recipient.Kind != "operator" || m.Recipient.ID != "operator") {
		return fmt.Errorf("recipient kind %q is unsupported", m.Recipient.Kind)
	}
	if len(m.Body) > agentMessageBodyLimit {
		return fmt.Errorf("message body exceeds %d bytes", agentMessageBodyLimit)
	}
	if m.Kind != "question" && m.Kind != "answer" && m.Kind != "instruction" && m.Kind != "wave_result" && m.Kind != "notice" {
		return fmt.Errorf("message kind %q is unsupported", m.Kind)
	}
	if m.Kind == "answer" && m.ReplyTo == "" {
		return errors.New("answer requires replyTo")
	}
	return nil
}

func (s *RuntimeStore) PutAgentMessage(m AgentMessage) (AgentMessage, bool, error) {
	return s.putAgentMessage(m, false)
}

func (s *RuntimeStore) PutAgentMessageAsOperator(m AgentMessage) (AgentMessage, bool, error) {
	return s.putAgentMessage(m, true)
}

func (s *RuntimeStore) putAgentMessage(m AgentMessage, operatorOverride bool) (AgentMessage, bool, error) {
	recipient, err := normalizeAgentAddress(m.Recipient.ID, m.Recipient.Kind)
	if err != nil {
		return AgentMessage{}, false, err
	}
	m.Recipient = recipient
	if err := m.validate(); err != nil {
		return AgentMessage{}, false, err
	}
	if m.ID == "" {
		m.ID = "msg-" + strings.ToLower(newRecordID())
	}
	if m.State == "" {
		m.State = "queued"
	}
	if m.TransportState == "" {
		m.TransportState = "pending"
	}
	if m.CreatedAt == "" {
		m.CreatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	if m.Kind == "answer" {
		parent, err := s.AgentMessage(m.ProjectID, m.ReplyTo)
		if err != nil {
			return AgentMessage{}, false, fmt.Errorf("reply target: %w", err)
		}
		originalSender, err := normalizeAgentAddress(parent.Sender, m.Recipient.Kind)
		if err != nil || originalSender != m.Recipient {
			return AgentMessage{}, false, errors.New("reply recipient does not match original sender")
		}
		if !operatorOverride {
			answerSender, err := normalizeAgentAddress(m.Sender, parent.Recipient.Kind)
			if err != nil || answerSender != parent.Recipient {
				return AgentMessage{}, false, errors.New("reply sender does not match original recipient")
			}
		}
		return s.putAgentAnswer(m)
	}
	result, err := s.exec(`INSERT OR IGNORE INTO agent_messages(id,idempotency_key,project_id,sender,recipient_kind,recipient_id,origin_task_id,origin_wave_id,work_revision,route_generation,recipient_generation,kind,body,reply_to,reply_required,yield_sender,state,transport_state,consumed_at,answered_at,applied_at,expires_at,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, m.ID, m.IdempotencyKey, m.ProjectID, m.Sender, m.Recipient.Kind, m.Recipient.ID, m.OriginTaskID, m.OriginWaveID, m.WorkRevision, m.RouteGeneration, m.RecipientGeneration, m.Kind, m.Body, m.ReplyTo, m.ReplyRequired, m.YieldSender, m.State, m.TransportState, m.ConsumedAt, m.AnsweredAt, m.AppliedAt, m.ExpiresAt, m.CreatedAt)
	if err != nil {
		return AgentMessage{}, false, err
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		existing, err := s.agentMessageByKey(m.ProjectID, m.Sender, m.IdempotencyKey)
		if err != nil {
			return AgentMessage{}, false, err
		}
		if !sameAgentMessageRequest(existing, m) {
			return AgentMessage{}, false, errors.New("idempotency key was already used for different message content")
		}
		return existing, true, s.queueAgentMessageWakeup(existing)
	}
	return m, false, s.queueAgentMessageWakeup(m)
}

func (s *RuntimeStore) ListAgentMessagesForTask(projectID, taskID string) ([]AgentMessage, error) {
	typed := "task:" + taskID
	rows, err := s.query(`SELECT id,idempotency_key,project_id,sender,recipient_kind,recipient_id,origin_task_id,origin_wave_id,work_revision,route_generation,recipient_generation,kind,body,reply_to,reply_required,yield_sender,state,transport_state,consumed_at,answered_at,applied_at,expires_at,created_at FROM agent_messages m WHERE project_id=? AND (origin_task_id=? OR (recipient_kind='task' AND recipient_id=?) OR sender=? OR EXISTS (SELECT 1 FROM agent_messages p WHERE p.project_id=m.project_id AND p.id=m.reply_to AND p.origin_task_id=?)) ORDER BY created_at,id`, projectID, taskID, taskID, typed, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AgentMessage{}
	for rows.Next() {
		message, err := scanAgentMessageValue(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, message)
	}
	return out, rows.Err()
}

type agentMessageScanner interface{ Scan(...any) error }

func scanAgentMessageValue(row agentMessageScanner) (AgentMessage, error) {
	var m AgentMessage
	var rr, ys int
	err := row.Scan(&m.ID, &m.IdempotencyKey, &m.ProjectID, &m.Sender, &m.Recipient.Kind, &m.Recipient.ID, &m.OriginTaskID, &m.OriginWaveID, &m.WorkRevision, &m.RouteGeneration, &m.RecipientGeneration, &m.Kind, &m.Body, &m.ReplyTo, &rr, &ys, &m.State, &m.TransportState, &m.ConsumedAt, &m.AnsweredAt, &m.AppliedAt, &m.ExpiresAt, &m.CreatedAt)
	m.ReplyRequired, m.YieldSender = rr != 0, ys != 0
	return m, err
}

func (s *RuntimeStore) putAgentAnswer(m AgentMessage) (AgentMessage, bool, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return AgentMessage{}, false, err
	}
	defer tx.Rollback()
	if err := tx.QueryRow(`SELECT origin_task_id,work_revision,route_generation FROM agent_messages WHERE project_id=? AND id=?`, m.ProjectID, m.ReplyTo).Scan(&m.OriginTaskID, &m.WorkRevision, &m.RouteGeneration); err != nil {
		return AgentMessage{}, false, err
	}
	result, err := tx.Exec(`INSERT OR IGNORE INTO agent_messages(id,idempotency_key,project_id,sender,recipient_kind,recipient_id,origin_task_id,origin_wave_id,work_revision,route_generation,recipient_generation,kind,body,reply_to,reply_required,yield_sender,state,transport_state,consumed_at,answered_at,applied_at,expires_at,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, m.ID, m.IdempotencyKey, m.ProjectID, m.Sender, m.Recipient.Kind, m.Recipient.ID, m.OriginTaskID, m.OriginWaveID, m.WorkRevision, m.RouteGeneration, m.RecipientGeneration, m.Kind, m.Body, m.ReplyTo, m.ReplyRequired, m.YieldSender, m.State, m.TransportState, m.ConsumedAt, m.AnsweredAt, m.AppliedAt, m.ExpiresAt, m.CreatedAt)
	if err != nil {
		return AgentMessage{}, false, err
	}
	n, _ := result.RowsAffected()
	duplicate := n == 0
	if duplicate {
		existing, err := scanAgentMessageValue(tx.QueryRow(`SELECT id,idempotency_key,project_id,sender,recipient_kind,recipient_id,origin_task_id,origin_wave_id,work_revision,route_generation,recipient_generation,kind,body,reply_to,reply_required,yield_sender,state,transport_state,consumed_at,answered_at,applied_at,expires_at,created_at FROM agent_messages WHERE project_id=? AND sender=? AND idempotency_key=?`, m.ProjectID, m.Sender, m.IdempotencyKey))
		if err != nil {
			return AgentMessage{}, false, err
		}
		if !sameAgentMessageRequest(existing, m) {
			return AgentMessage{}, false, errors.New("idempotency key was already used for different message content")
		}
		m = existing
	}
	query := `UPDATE agent_messages SET answered_at=? WHERE project_id=? AND id=? AND answered_at=''`
	if duplicate {
		query = `UPDATE agent_messages SET answered_at=CASE WHEN answered_at='' THEN ? ELSE answered_at END WHERE project_id=? AND id=?`
	}
	if result, err = tx.Exec(query, m.CreatedAt, m.ProjectID, m.ReplyTo); err != nil {
		return AgentMessage{}, false, err
	}
	if updated, _ := result.RowsAffected(); updated != 1 {
		return AgentMessage{}, false, errors.New("reply target disappeared before receipt commit")
	}
	if err := tx.Commit(); err != nil {
		return AgentMessage{}, false, err
	}
	return m, duplicate, s.queueAgentMessageWakeup(m)
}

func normalizeAgentAddress(value, fallbackKind string) (AgentAddress, error) {
	value = strings.TrimSpace(value)
	if value == "operator:operator" || (value == "operator" && fallbackKind == "operator") {
		return AgentAddress{Kind: "operator", ID: "operator"}, nil
	}
	if strings.Contains(value, ":") {
		return parseAgentAddress(value)
	}
	if fallbackKind != "task" && fallbackKind != "execution" {
		return AgentAddress{}, errors.New("agent address kind is required")
	}
	return AgentAddress{Kind: fallbackKind, ID: value}, nil
}

func sameAgentMessageRequest(a, b AgentMessage) bool {
	return a.ProjectID == b.ProjectID && a.Sender == b.Sender && a.Recipient == b.Recipient &&
		a.OriginTaskID == b.OriginTaskID && a.OriginWaveID == b.OriginWaveID &&
		a.WorkRevision == b.WorkRevision && a.RouteGeneration == b.RouteGeneration && a.RecipientGeneration == b.RecipientGeneration &&
		a.Kind == b.Kind && a.Body == b.Body && a.ReplyTo == b.ReplyTo &&
		a.ReplyRequired == b.ReplyRequired && a.YieldSender == b.YieldSender && a.ExpiresAt == b.ExpiresAt
}

func (s *RuntimeStore) queueAgentMessageWakeup(m AgentMessage) error {
	if m.Kind == "notice" || m.Recipient.Kind == "operator" {
		return nil
	}
	_, _, err := s.QueueAgentWakeup(m.ProjectID, m.Recipient, m.Kind, "message:"+m.ID, []string{m.ID})
	return err
}

// ConsumeAgentMessage claims an unread message for exactly one caller.
func (s *RuntimeStore) ConsumeAgentMessage(projectID, id string) (bool, error) {
	result, err := s.exec(`UPDATE agent_messages SET state='consumed', consumed_at=? WHERE project_id=? AND id=? AND consumed_at=''`, time.Now().UTC().Format(time.RFC3339Nano), projectID, id)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	return n == 1, err
}

func (s *RuntimeStore) AgentMessage(projectID, id string) (AgentMessage, error) {
	return s.scanAgentMessage(`SELECT id,idempotency_key,project_id,sender,recipient_kind,recipient_id,origin_task_id,origin_wave_id,work_revision,route_generation,recipient_generation,kind,body,reply_to,reply_required,yield_sender,state,transport_state,consumed_at,answered_at,applied_at,expires_at,created_at FROM agent_messages WHERE project_id=? AND id=?`, projectID, id)
}
func (s *RuntimeStore) agentMessageByKey(projectID, sender, key string) (AgentMessage, error) {
	return s.scanAgentMessage(`SELECT id,idempotency_key,project_id,sender,recipient_kind,recipient_id,origin_task_id,origin_wave_id,work_revision,route_generation,recipient_generation,kind,body,reply_to,reply_required,yield_sender,state,transport_state,consumed_at,answered_at,applied_at,expires_at,created_at FROM agent_messages WHERE project_id=? AND sender=? AND idempotency_key=?`, projectID, sender, key)
}
func (s *RuntimeStore) scanAgentMessage(q string, args ...any) (AgentMessage, error) {
	return scanAgentMessageValue(s.db.QueryRow(q, args...))
}

func (s *RuntimeStore) ListAgentMessages(projectID, recipientKind, recipientID string) ([]AgentMessage, error) {
	if recipientID != "" {
		address, err := normalizeAgentAddress(recipientID, recipientKind)
		if err != nil {
			return nil, err
		}
		recipientKind, recipientID = address.Kind, address.ID
	}
	rows, err := s.query(`SELECT id,idempotency_key,project_id,sender,recipient_kind,recipient_id,origin_task_id,origin_wave_id,work_revision,route_generation,recipient_generation,kind,body,reply_to,reply_required,yield_sender,state,transport_state,consumed_at,answered_at,applied_at,expires_at,created_at FROM agent_messages WHERE project_id=? AND (?='' OR recipient_kind=?) AND (?='' OR recipient_id=?) ORDER BY created_at,id`, projectID, recipientKind, recipientKind, recipientID, recipientID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AgentMessage{}
	for rows.Next() {
		var m AgentMessage
		var rr, ys int
		if err := rows.Scan(&m.ID, &m.IdempotencyKey, &m.ProjectID, &m.Sender, &m.Recipient.Kind, &m.Recipient.ID, &m.OriginTaskID, &m.OriginWaveID, &m.WorkRevision, &m.RouteGeneration, &m.RecipientGeneration, &m.Kind, &m.Body, &m.ReplyTo, &rr, &ys, &m.State, &m.TransportState, &m.ConsumedAt, &m.AnsweredAt, &m.AppliedAt, &m.ExpiresAt, &m.CreatedAt); err != nil {
			return nil, err
		}
		m.ReplyRequired, m.YieldSender = rr != 0, ys != 0
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *RuntimeStore) MarkAgentMessage(projectID, id, state string) error {
	column := map[string]string{"consumed": "consumed_at", "applied": "applied_at"}[state]
	if column == "" {
		return errors.New("message state must be consumed or applied")
	}
	result, err := s.exec(`UPDATE agent_messages SET state=?, `+column+`=? WHERE project_id=? AND id=?`, state, time.Now().UTC().Format(time.RFC3339Nano), projectID, id)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n != 1 {
		return sql.ErrNoRows
	}
	return nil
}
