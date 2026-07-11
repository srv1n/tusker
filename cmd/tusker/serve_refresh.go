package main

import (
	"net/http"
	"strings"
	"time"
)

const serveManualRefreshMinInterval = 2 * time.Second

func serveProjectRefreshID(path string) (string, bool) {
	const prefix = "/api/projects/"
	const suffix = "/refresh"
	if !strings.HasPrefix(path, prefix) || !strings.HasSuffix(path, suffix) {
		return "", false
	}
	projectID := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(path, prefix), suffix))
	if projectID == "" || strings.Contains(projectID, "/") {
		return "", false
	}
	return projectID, true
}

func (s *serveServer) handleProjectRefresh(w http.ResponseWriter, projectID string) {
	project, err := s.projectForSnapshot(projectID)
	if err != nil {
		serveJSON(w, http.StatusNotFound, serveActionResult{OK: false, Refused: true, Reason: err.Error(), ProjectID: projectID})
		return
	}
	projectID = project.ProjectID
	now := s.now()
	s.refreshMu.Lock()
	if s.refreshedAt == nil {
		s.refreshedAt = map[string]time.Time{}
	}
	last := s.refreshedAt[projectID]
	if !last.IsZero() && now.Sub(last) < serveManualRefreshMinInterval {
		s.refreshMu.Unlock()
		serveJSON(w, http.StatusAccepted, serveActionResult{OK: true, Reason: "Refresh already queued; rapid request coalesced.", ProjectID: projectID})
		return
	}
	s.refreshedAt[projectID] = now
	s.refreshMu.Unlock()

	err = sendDaemonControlOneWay(DefaultStateRoot(), daemonControlRequest{Command: "reconcile_project", ProjectID: projectID}, 250*time.Millisecond)
	if err != nil {
		s.refreshMu.Lock()
		if s.refreshedAt[projectID] == now {
			delete(s.refreshedAt, projectID)
		}
		s.refreshMu.Unlock()
		serveJSON(w, http.StatusServiceUnavailable, serveActionResult{OK: false, Refused: true, Reason: "Daemon refresh channel is unavailable.", ProjectID: projectID})
		return
	}
	serveJSON(w, http.StatusAccepted, serveActionResult{OK: true, Reason: "Targeted project refresh queued.", ProjectID: projectID})
}
