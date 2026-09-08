package main

import (
	"encoding/json"
	"net/http"
)

func (s *serveServer) handleWorkerLifecycle(w http.ResponseWriter, r *http.Request) {
	var req daemonControlRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10)).Decode(&req); err != nil {
		serveJSON(w, http.StatusBadRequest, serveActionResult{OK: false, Refused: true, Reason: "invalid worker lifecycle request"})
		return
	}
	var err error
	if req.Worker != nil && req.Worker.Action == "submit" {
		err = queueWorkerLifecycle(s.store, req)
	} else {
		err = applyWorkerLifecycle(s.store, req)
	}
	if err != nil {
		serveJSON(w, http.StatusConflict, serveActionResult{OK: false, Refused: true, Reason: err.Error()})
		return
	}
	serveJSON(w, http.StatusOK, serveActionResult{OK: true})
}
