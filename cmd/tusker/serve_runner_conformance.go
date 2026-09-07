package main

import (
	"net/http"
	"strings"
)

func (s *serveServer) handleRunnerConformance(w http.ResponseWriter, r *http.Request, body serveActionBody) {
	projectID := strings.TrimSpace(r.URL.Query().Get("project"))
	if projectID == "" {
		projectID = body.string("projectId", "project_id", "project")
	}
	project, err := s.projectForSnapshot(projectID)
	if err != nil {
		serveJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
		return
	}
	args := Args{
		"vault":    project.VaultRoot,
		"harness":  firstNonEmpty(body.string("harness"), r.URL.Query().Get("harness")),
		"preset":   firstNonEmpty(body.string("preset"), r.URL.Query().Get("preset")),
		"exercise": firstNonEmpty(body.string("exercise"), r.URL.Query().Get("exercise")),
	}
	if r.Method == http.MethodPost {
		args["live"] = "true"
	}
	if body.bool("externalContainment") || body.bool("external_containment") {
		args["external-containment"] = "true"
	}
	code, report, runErr := runRunnerConformance(args)
	if runErr != nil {
		serveJSON(w, http.StatusBadRequest, map[string]any{"error": runErr.Error()})
		return
	}
	status := http.StatusOK
	if code != 0 {
		status = http.StatusUnprocessableEntity
	}
	serveJSON(w, status, report)
}
