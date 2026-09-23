package main

import (
	"net/http"
	"strings"
)

type serveRunSayRoute struct {
	Mode      string `json:"mode"`
	Available bool   `json:"available"`
	Note      string `json:"note"`
	Reason    string `json:"reason,omitempty"`
}

type serveRunSayDelivery struct {
	ID       string `json:"id"`
	Body     string `json:"body"`
	State    string `json:"state"`
	StoredAt string `json:"storedAt"`
}

func (s *serveServer) lastRunSayDelivery(run RunStatus) (*serveRunSayDelivery, error) {
	rows, err := s.store.query(`SELECT delivery_id, body, state, stored_at FROM worker_deliveries WHERE project_id = ? AND task_id = ? AND work_revision = ? AND kind = 'instruction' AND idempotency_key NOT LIKE 'continue:%' ORDER BY stored_at DESC, delivery_id DESC LIMIT 1`, run.ProjectID, run.ItemID, run.WorkRevision)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, rows.Err()
	}
	var delivery serveRunSayDelivery
	if err := rows.Scan(&delivery.ID, &delivery.Body, &delivery.State, &delivery.StoredAt); err != nil {
		return nil, err
	}
	return &delivery, rows.Err()
}

func serveSayRoute(run RunStatus, state runOperatorState) serveRunSayRoute {
	route := serveRunSayRoute{Mode: "none", Reason: "This runner cannot receive a message on this session"}
	caps := nativeResumeRunnerCapabilities(RunnerName(run.Runner))
	switch {
	case caps.SoftSay:
		route.Mode, route.Note, route.Reason = "soft", "Delivered between tool calls", ""
	case caps.HardSay:
		route.Mode, route.Note, route.Reason = "hard", "Interrupts the agent, then resumes the same conversation with your message", ""
	}
	if state.State != "working" && state.State != "quiet" {
		route.Available = false
		if state.State == "waiting_on_you" {
			route.Reason = "Answer the open question instead"
		} else {
			route.Reason = "Say is available while the run is Working or Quiet"
		}
		return route
	}
	route.Available = route.Mode != "none"
	return route
}

type serveRunSayResponse struct {
	OK            bool              `json:"ok"`
	Refused       bool              `json:"refused,omitempty"`
	Reason        string            `json:"reason,omitempty"`
	Delivery      *WorkerDelivery   `json:"delivery,omitempty"`
	Route         string            `json:"route,omitempty"`
	OperatorState *runOperatorState `json:"operatorState,omitempty"`
	Duplicate     bool              `json:"duplicate,omitempty"`
	Result        *runSayResult     `json:"result,omitempty"`
	Run           *serveRunSummary  `json:"run,omitempty"`
}

func (s *serveServer) handleRunSay(w http.ResponseWriter, r *http.Request, taskID string, body serveActionBody) {
	s.handleRunMessage(w, r, taskID, body, true)
}

func (s *serveServer) handleRunContinue(w http.ResponseWriter, r *http.Request, taskID string, body serveActionBody) {
	s.handleRunMessage(w, r, taskID, body, false)
}

func (s *serveServer) handleRunMessage(w http.ResponseWriter, r *http.Request, taskID string, body serveActionBody, say bool) {
	projectID := strings.TrimSpace(r.URL.Query().Get("project"))
	if projectID == "" {
		serveJSON(w, http.StatusBadRequest, serveRunSayResponse{Refused: true, Reason: "project query parameter is required"})
		return
	}
	actor, err := s.serveOperatorActor(body, "serve run message")
	if err != nil {
		serveJSON(w, http.StatusForbidden, serveRunSayResponse{Refused: true, Reason: err.Error()})
		return
	}
	snap, err := s.loadFreshSnapshotForProject(projectID)
	if err != nil {
		serveJSON(w, http.StatusNotFound, serveRunSayResponse{Refused: true, Reason: "run not found in project"})
		return
	}
	run, ok := serveFindRun(snap.runs, strings.TrimSpace(taskID))
	if !ok {
		serveJSON(w, http.StatusNotFound, serveRunSayResponse{Refused: true, Reason: "run not found in project"})
		return
	}
	latestBefore, err := findRunScopedOrAmbiguous(s.store, run.ProjectID, run.RecordID)
	if err != nil || latestBefore == nil {
		serveJSON(w, http.StatusConflict, serveRunSayResponse{Refused: true, Reason: "canonical run readback is unavailable"})
		return
	}
	if latestBefore.ActiveAttemptID != run.ActiveAttemptID || latestBefore.LeaseGeneration != run.LeaseGeneration || latestBefore.WorkRevision != run.WorkRevision {
		response := serveRunSayResponse{Refused: true, Reason: "run advanced to a different attempt; refresh before sending"}
		if summary, summaryErr := s.runSummaryChecked(snap, *latestBefore); summaryErr == nil {
			response.Run = &summary
		}
		serveJSON(w, http.StatusConflict, response)
		return
	}
	run = *latestBefore
	project, task, wave, err := runSayContext(s.store, run)
	if err != nil {
		serveJSON(w, http.StatusConflict, serveRunSayResponse{Refused: true, Reason: err.Error()})
		return
	}
	action := "continue"
	if say {
		action = "say"
	}
	capability := s.runActionCapability(action, project, wave, run, nil)
	priorDelivery := false
	if say {
		prior, priorErr := runSayPriorDelivery(s.store, run, actor, body.string("message"), body.string("idempotencyKey"))
		if priorErr != nil {
			serveJSON(w, http.StatusConflict, serveRunSayResponse{Refused: true, Reason: priorErr.Error()})
			return
		}
		priorDelivery = prior != nil
	}
	if !capability.Available && !priorDelivery {
		response := serveRunSayResponse{Refused: true, Reason: capability.Reason}
		if summary, summaryErr := s.runSummaryChecked(snap, run); summaryErr == nil {
			response.Run = &summary
		}
		serveJSON(w, http.StatusConflict, response)
		return
	}
	message := body.string("message")
	if say && message == "" {
		serveJSON(w, http.StatusBadRequest, serveRunSayResponse{Refused: true, Reason: "message is required"})
		return
	}
	if len(message) > workerDeliveryBodyLimit {
		serveJSON(w, http.StatusBadRequest, serveRunSayResponse{Refused: true, Reason: "message exceeds 32KiB"})
		return
	}
	var result runSayResult
	if say {
		result, err = sayRuntimeRun(s.store, DefaultStateRoot(), project, task, wave, run, actor, message, body.string("idempotencyKey"), s.now())
	} else {
		result, err = continueRuntimeRun(s.store, project, task, wave, run, actor, message, s.now())
	}
	if err != nil {
		serveJSON(w, http.StatusConflict, serveRunSayResponse{Refused: true, Reason: err.Error()})
		return
	}
	latest, err := findRunScopedOrAmbiguous(s.store, run.ProjectID, run.RecordID)
	response := serveRunSayResponse{OK: true, Delivery: &result.Delivery, Route: result.Route, OperatorState: &result.OperatorState, Duplicate: result.Duplicate, Result: &result}
	if err != nil || latest == nil {
		serveJSON(w, http.StatusOK, response)
		return
	}
	summary, err := s.runSummaryChecked(snap, *latest)
	if err != nil {
		serveJSON(w, http.StatusOK, response)
		return
	}
	response.Run = &summary
	serveJSON(w, http.StatusOK, response)
}
