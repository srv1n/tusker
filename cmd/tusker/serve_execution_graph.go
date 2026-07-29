package main

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
)

type serveExecutionRenameRequest struct {
	Name string `json:"name"`
}
type serveExecutionBindRequest struct {
	TaskID string `json:"task_id"`
}

func serveExecutionActionID(path, action string) (string, bool) {
	prefix, suffix := "/api/executions/", "/"+action
	if !strings.HasPrefix(path, prefix) || !strings.HasSuffix(path, suffix) {
		return "", false
	}
	id := strings.TrimSuffix(strings.TrimPrefix(path, prefix), suffix)
	return id, id != "" && !strings.Contains(id, "/")
}

// handleExecutionRename is deliberately narrow: it appends a name event and
// returns canonical readback, never mutating identity or provider evidence.
func (s *serveServer) handleExecutionRename(w http.ResponseWriter, r *http.Request, executionID string) {
	project, err := s.projectForSnapshot(r.URL.Query().Get("project"))
	if err != nil {
		serveJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
		return
	}
	var body serveExecutionRenameRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&body); err != nil {
		serveJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid rename request"})
		return
	}
	view, err := s.store.RenameExecution(project.ProjectID, executionID, body.Name, "serve:operator")
	if err != nil {
		serveJSON(w, http.StatusConflict, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	serveJSON(w, http.StatusOK, map[string]any{"ok": true, "execution": view})
}

// handleExecutionBindPreview exposes the exact authority boundary before a
// write. It does not make past provider observations proof eligible.
func (s *serveServer) handleExecutionBindPreview(w http.ResponseWriter, r *http.Request, executionID string) {
	project, err := s.projectForSnapshot(r.URL.Query().Get("project"))
	if err != nil {
		serveJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
		return
	}
	taskID := strings.TrimSpace(r.URL.Query().Get("task_id"))
	waveID, err := executionCanonicalWave(firstNonEmpty(project.VaultRoot, s.vaultPath), taskID)
	if err != nil {
		serveJSON(w, http.StatusConflict, map[string]any{"ok": false, "error": err.Error(), "proof_boundary": "Earlier unbound history remains ineligible."})
		return
	}
	view, err := s.store.ExecutionView(executionID)
	if err != nil || view == nil || view.ProjectID != project.ProjectID {
		serveJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": "execution not found"})
		return
	}
	var conflicts int
	if err := s.store.queryRowScan(`SELECT COUNT(*) FROM runs WHERE project_id=? AND (item_id=? OR record_id=?) AND terminal=0 AND lease_state IN ('claimed','running')`, []any{project.ProjectID, taskID, taskID}, &conflicts); err != nil {
		serveJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	serveJSON(w, http.StatusOK, map[string]any{"ok": conflicts == 0, "task_id": taskID, "wave_id": waveID, "binding_generation": view.BindingGeneration + 1, "conflicts": conflicts, "proof_boundary": "Only observations recorded after this binding generation may be considered for delivery authority; earlier history remains observable but proof ineligible."})
}

func (s *serveServer) handleExecutionBind(w http.ResponseWriter, r *http.Request, executionID string) {
	project, err := s.projectForSnapshot(r.URL.Query().Get("project"))
	if err != nil {
		serveJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
		return
	}
	var body serveExecutionBindRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&body); err != nil {
		serveJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid binding request"})
		return
	}
	waveID, err := executionCanonicalWave(firstNonEmpty(project.VaultRoot, s.vaultPath), body.TaskID)
	if err != nil {
		serveJSON(w, http.StatusConflict, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	view, err := s.store.ExecutionView(executionID)
	if err != nil || view == nil || view.ProjectID != project.ProjectID {
		serveJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": "execution not found"})
		return
	}
	action := "bind"
	if view.BindingGeneration > 0 && view.BoundTaskID != "" {
		action = "rebind"
	}
	view, err = s.store.BindExecution(ExecutionBindingInput{ProjectID: project.ProjectID, ExecutionID: executionID, TaskID: body.TaskID, WaveID: waveID, Actor: "serve:operator"}, action)
	if err != nil {
		serveJSON(w, http.StatusConflict, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	serveJSON(w, http.StatusOK, map[string]any{"ok": true, "execution": view, "proof_boundary": "Pre-binding observations remain proof ineligible."})
}

// handleExecutionGraph uses the same read-only RuntimeStore query as `tusker
// execution list`; it never enters a lifecycle or provider-adapter path.
func (s *serveServer) handleExecutionGraph(w http.ResponseWriter, r *http.Request) {
	project, err := s.projectForSnapshot(r.URL.Query().Get("project"))
	if err != nil {
		serveJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
		return
	}
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	page, err := s.store.ExecutionGraph(project.ProjectID, ExecutionGraphFilter{ExecutionID: q.Get("execution"), RootID: q.Get("root"), ParentID: q.Get("parent"), TaskID: q.Get("task"), WaveID: q.Get("wave"), Source: q.Get("source"), Provider: q.Get("provider"), ProviderID: q.Get("provider_id"), AgentType: q.Get("agent_type"), Binding: q.Get("binding"), Lifecycle: q.Get("lifecycle"), Name: firstNonEmpty(q.Get("name"), q.Get("search")), Attention: q.Get("attention"), Cursor: q.Get("cursor"), Limit: limit})
	if err != nil {
		serveJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	serveJSON(w, http.StatusOK, page)
}

func serveExecutionCancelID(path string) (string, bool) {
	const prefix, suffix = "/api/executions/", "/cancel"
	if !strings.HasPrefix(path, prefix) || !strings.HasSuffix(path, suffix) {
		return "", false
	}
	id := strings.TrimSuffix(strings.TrimPrefix(path, prefix), suffix)
	return id, id != "" && !strings.Contains(id, "/")
}

// handleExecutionCancel has a deliberately narrower contract than run
// interrupt: provider acknowledgement is never claimed until a provider
// adapter has a target-specific control implementation.
func (s *serveServer) handleExecutionCancel(w http.ResponseWriter, r *http.Request, executionID string) {
	view, err := s.store.ExecutionView(executionID)
	if err != nil || view == nil {
		serveJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": "execution not found"})
		return
	}
	if project := strings.TrimSpace(r.URL.Query().Get("project")); project != "" && project != view.ProjectID {
		serveJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": "execution not found"})
		return
	}
	control, err := s.store.RequestExecutionCancellation(executionID, firstNonEmpty(r.Header.Get("Idempotency-Key"), "serve"))
	if err != nil {
		serveJSON(w, http.StatusOK, map[string]any{"ok": false, "control": control, "error": err.Error()})
		return
	}
	serveJSON(w, http.StatusOK, map[string]any{"ok": control.Available, "control": control, "execution_id": executionID})
}

func (s *serveServer) handleExecutionInbox(w http.ResponseWriter, r *http.Request) {
	project, err := s.projectForSnapshot(r.URL.Query().Get("project"))
	if err != nil {
		serveJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
		return
	}
	views, err := s.store.ListUnboundDirectExecutions(project.ProjectID)
	if err != nil {
		serveJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	serveJSON(w, http.StatusOK, map[string]any{"schema": executionGraphSchema, "executions": views, "read_only": true})
}

func (s *serveServer) handleExecutionTimeline(w http.ResponseWriter, r *http.Request) {
	project, err := s.projectForSnapshot(r.URL.Query().Get("project"))
	if err != nil {
		serveJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	page, err := s.store.ExecutionTimeline(project.ProjectID, r.URL.Query().Get("execution"), r.URL.Query().Get("wave"), r.URL.Query().Get("direction"), r.URL.Query().Get("cursor"), limit)
	if err != nil {
		serveJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	serveJSON(w, http.StatusOK, page)
}
