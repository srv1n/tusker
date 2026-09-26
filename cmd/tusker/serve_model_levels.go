package main

import (
	"encoding/json"
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
		report, readErr := modelLevelsReadForScope(project.VaultRoot, firstNonEmpty(r.URL.Query().Get("scope"), "project"))
		if readErr != nil {
			serveJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": readErr.Error()})
			return
		}
		serveJSON(w, http.StatusOK, report)
		return
	}
	args := Args{"vault": project.VaultRoot, "_no-output": "true", "scope": firstNonEmpty(body.string("scope"), "project"), "level": body.string("level"), "lane": body.string("lane"), "profiles": body.csv("profiles"), "if-revision": body.string("revision", "ifRevision"), "name": body.string("name"), "display-name": body.string("displayName", "display_name"), "eligible-tiers": body.csv("eligibleTiers", "eligible_tiers"), "harness": body.string("harness"), "model": body.string("model"), "effort": body.string("effort"), "preset": body.string("preset"), "command": body.string("command"), "private-folders": body.csv("privateFolders", "private_folders")}
	if access, present := body["access"]; present {
		encoded, encodeErr := json.Marshal(access)
		if encodeErr != nil {
			serveJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": "access must be a JSON object"})
			return
		}
		args["access"] = string(encoded)
	}
	if _, ok := body["eligibleTiers"]; !ok {
		if _, ok = body["eligible_tiers"]; !ok {
			delete(args, "eligible-tiers")
		}
	}
	if strings.HasPrefix(body.string("action"), "profile-") {
		// Profile definitions live only in the global config; a project
		// selects them through tier mappings ("set"/"reset").
		args["scope"] = firstNonEmpty(body.string("scope"), "global")
	}
	switch body.string("action") {
	case "set":
		err = modelsSetCmd(args)
	case "reset":
		err = modelsResetCmd(args)
	case "profile-set":
		err = modelsProfileSetCmd(args)
	case "profile-disable", "profile-enable", "profile-remove":
		err = modelsProfileLifecycleCmd(args, body.string("action"))
	case "private-folders":
		err = modelsPrivateFoldersSetCmd(args)
	default:
		err = tuskerError(errorInvalidArg, "model settings action must be set, reset, profile-set, profile-disable, profile-enable, profile-remove, or private-folders")
	}
	if err != nil {
		issue := errorToIssue(err)
		serveJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": issue.Message, "issue": issue})
		return
	}
	if strings.HasPrefix(body.string("action"), "profile-") || args["scope"] == "global" {
		// Global profiles and mappings feed every project's routes.
		s.invalidateProjectSnapshot("")
	} else {
		s.invalidateProjectSnapshot(project.ProjectID)
	}
	report, err := modelLevelsReadForScope(project.VaultRoot, firstNonEmpty(body.string("scope"), "project"))
	if err != nil {
		serveJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": err.Error()})
		return
	}
	serveJSON(w, http.StatusOK, report)
}

func (s *serveServer) handleModelCatalog(w http.ResponseWriter, r *http.Request) {
	serveJSON(w, http.StatusOK, discoverRunnerCatalogWithRefresh(false, r.URL.Query().Get("refresh") == "1"))
}
