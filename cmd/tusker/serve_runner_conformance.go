package main

import (
	"encoding/json"
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
	if body.bool("draft") {
		args["draft"] = "true"
		args["draft-id"] = body.string("draftId", "draft_id")
		args["model"] = body.string("model")
		args["effort"] = body.string("effort")
		if access, present := body["access"]; present {
			encoded, encodeErr := json.Marshal(access)
			if encodeErr != nil {
				serveJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": "draft access must be a JSON object"})
				return
			}
			args["access"] = string(encoded)
		}
	}
	if body.bool("setup") {
		args["setup"] = "true"
	}
	// Setup is an explicit local discovery/resolve check. It uses POST so the
	// unsaved draft can travel as a typed body, but never starts a model turn.
	if r.Method == http.MethodPost && !body.bool("setup") {
		args["live"] = "true"
		if args["preset"] == "danger-full-access" {
			args["external-containment"] = "true"
		}
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
	if code != 0 && !body.bool("setup") {
		status = http.StatusUnprocessableEntity
	}
	serveJSON(w, status, report)
}
