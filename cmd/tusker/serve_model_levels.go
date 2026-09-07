package main

import (
	"net/http"
	"strings"
)

func (s *serveServer) handleModelLevels(w http.ResponseWriter, r *http.Request, body serveActionBody) {
	projectID := strings.TrimSpace(r.URL.Query().Get("project"))
	if projectID == "" {
		projectID = body.string("projectId", "project_id", "project")
	}
	project, err := s.projectForSnapshot(projectID)
	if err != nil {
		serveJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
		return
	}
	if r.Method == http.MethodGet {
		report, readErr := modelLevelsRead(project.VaultRoot)
		if readErr != nil {
			serveJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": readErr.Error()})
			return
		}
		serveJSON(w, http.StatusOK, report)
		return
	}
	args := Args{"vault": project.VaultRoot, "_no-output": "true", "scope": firstNonEmpty(body.string("scope"), "project"), "level": body.string("level"), "lane": body.string("lane"), "profiles": body.csv("profiles"), "if-revision": body.string("revision", "ifRevision"), "name": body.string("name"), "harness": body.string("harness"), "model": body.string("model"), "effort": body.string("effort"), "preset": body.string("preset"), "command": body.string("command")}
	switch body.string("action") {
	case "set":
		err = modelsSetCmd(args)
	case "reset":
		err = modelsResetCmd(args)
	case "profile-set":
		err = modelsProfileSetCmd(args)
	default:
		err = tuskerError(errorInvalidArg, "model settings action must be set, reset, or profile-set")
	}
	if err != nil {
		issue := errorToIssue(err)
		serveJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": issue.Message, "issue": issue})
		return
	}
	s.invalidateProjectSnapshot(project.ProjectID)
	report, err := modelLevelsRead(project.VaultRoot)
	if err != nil {
		serveJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": err.Error()})
		return
	}
	serveJSON(w, http.StatusOK, report)
}

func (s *serveServer) handleModelCatalog(w http.ResponseWriter, _ *http.Request) {
	serveJSON(w, http.StatusOK, discoverRunnerCatalog(false))
}
