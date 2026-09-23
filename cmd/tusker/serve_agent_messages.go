package main

import (
	"net/http"
	"strings"
)

func (s *serveServer) handleAgentMessages(w http.ResponseWriter, r *http.Request) {
	project := strings.TrimSpace(r.URL.Query().Get("project"))
	if project == "" {
		serveJSON(w, http.StatusBadRequest, map[string]any{"error": "project is required"})
		return
	}
	messages, err := s.store.ListAgentMessages(project, r.URL.Query().Get("recipientKind"), r.URL.Query().Get("recipient"))
	if err != nil {
		serveJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	serveJSON(w, http.StatusOK, map[string]any{"messages": messages})
}

func (s *serveServer) handleAgentMessage(w http.ResponseWriter, r *http.Request, id string) {
	project := strings.TrimSpace(r.URL.Query().Get("project"))
	message, err := s.store.AgentMessage(project, id)
	if err != nil {
		serveJSON(w, http.StatusNotFound, map[string]any{"error": "message not found"})
		return
	}
	serveJSON(w, http.StatusOK, message)
}

func (s *serveServer) handleAgentMessageSend(w http.ResponseWriter, body serveActionBody) {
	project := body.string("projectId", "project")
	sender, actorErr := s.serveOperatorActor(body, "serve agent message")
	if actorErr != nil {
		status, result := serveOperatorActorResult("tusker message", actorErr)
		serveJSON(w, status, result)
		return
	}
	replyTo := body.string("replyTo", "reply_to")
	kind := firstNonEmpty(body.string("kind"), "notice")
	if replyTo != "" {
		kind = "answer"
		parent, err := s.store.AgentMessage(project, replyTo)
		if err != nil || parent.AnsweredAt != "" {
			serveJSON(w, http.StatusOK, serveActionResult{Refused: true, Reason: "Question is already answered or unavailable; refresh the task."})
			return
		}
	}
	m := AgentMessage{IdempotencyKey: body.string("idempotencyKey", "key"), ProjectID: project, Sender: sender, Recipient: AgentAddress{Kind: firstNonEmpty(body.string("recipientKind"), "task"), ID: body.string("recipientId", "recipient")}, OriginTaskID: body.string("originTaskId", "taskId"), OriginWaveID: body.string("originWaveId", "waveId"), Kind: kind, Body: body.string("body"), ReplyTo: replyTo, ReplyRequired: body.bool("replyRequired"), YieldSender: body.bool("yieldSender")}
	var stored AgentMessage
	var duplicate bool
	var err error
	if replyTo != "" {
		stored, duplicate, err = s.store.PutAgentMessageAsOperator(m)
	} else {
		stored, duplicate, err = s.store.PutAgentMessage(m)
	}
	if err != nil {
		serveJSON(w, http.StatusOK, serveActionResult{Refused: true, Reason: err.Error()})
		return
	}
	s.invalidateProjectSnapshot(project)
	serveJSON(w, http.StatusOK, map[string]any{"ok": true, "duplicate": duplicate, "message": stored})
}

func (s *serveServer) handleAgentMessageState(w http.ResponseWriter, id, action string, body serveActionBody) {
	if _, err := s.serveOperatorActor(body, "serve agent message "+action); err != nil {
		status, result := serveOperatorActorResult("tusker message "+action, err)
		serveJSON(w, status, result)
		return
	}
	project := body.string("projectId", "project")
	state := map[string]string{"consume": "consumed", "apply": "applied"}[action]
	if err := s.store.MarkAgentMessage(project, id, state); err != nil {
		serveJSON(w, http.StatusOK, serveActionResult{Refused: true, Reason: err.Error()})
		return
	}
	serveJSON(w, http.StatusOK, map[string]any{"ok": true, "state": state})
}
