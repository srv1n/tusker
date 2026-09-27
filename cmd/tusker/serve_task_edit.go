package main

import (
	"net/http"
	"os"
	"strings"
)

// handleTaskAuthoringMutation serves task contract authoring from the UI.
// Both routes are thin adapters over the CLI command functions, so the UI and
// `tusker task update` / `tusker new task` share validation, CAS and events:
//
//	POST /api/tasks            -> newAuthoredV7Task (+ waveV7AddCmd when wave is set)
//	POST /api/tasks/<id>/edit  -> updateV7TaskCmd
func (s *serveServer) handleTaskAuthoringMutation(w http.ResponseWriter, r *http.Request, path string) bool {
	if r.Method != http.MethodPost {
		return false
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	create := len(parts) == 2 && parts[1] == "tasks"
	edit := len(parts) == 4 && parts[1] == "tasks" && parts[3] == "edit"
	if !create && !edit {
		return false
	}
	body, err := serveReadActionBody(r)
	if err != nil {
		serveJSON(w, http.StatusOK, serveCommandResult("tusker task", "", err))
		return true
	}
	if _, ok := body["projectId"]; !ok {
		if projectID := strings.TrimSpace(r.URL.Query().Get("project")); projectID != "" {
			body["projectId"] = projectID
		}
	}
	if create {
		s.handleTaskCreateAction(w, body)
	} else {
		s.handleTaskEditAction(w, parts[2], body)
	}
	return true
}

func (s *serveServer) handleTaskEditAction(w http.ResponseWriter, taskID string, body serveActionBody) {
	const command = "tusker task update"
	args, project, err := serveBaseArgsForBody(s, body)
	if err != nil {
		serveJSON(w, http.StatusOK, serveCommandResult(command, "", err))
		return
	}
	actor, err := s.serveOperatorActor(body, "serve task edit")
	if err != nil {
		status, result := serveOperatorActorResult(command, err)
		serveJSON(w, status, result)
		return
	}
	revision := body.string("revision", "stateRevision")
	if revision == "" {
		serveJSON(w, http.StatusUnprocessableEntity, serveActionResult{Refused: true, Reason: "task edit requires the current revision", Command: command})
		return
	}
	taskID = strings.ToUpper(strings.TrimSpace(taskID))
	args["id"], args["if-revision"], args["by"], args["quiet"] = taskID, revision, actor, "true"
	if _, ok := body["title"]; ok {
		args["title"] = toString(body["title"])
	}
	if _, ok := body["workLevel"]; ok {
		args["work-level"] = toString(body["workLevel"])
	}
	for key, flag := range map[string]string{"executeProfile": "execute-profile", "reviewProfile": "review-profile"} {
		raw, ok := body[key]
		if !ok {
			continue
		}
		if value := strings.TrimSpace(toString(raw)); raw != nil && value != "" {
			args[flag] = value
		} else {
			args["clear-"+flag] = "true"
		}
	}
	if raw, ok := body["body"]; ok {
		cleanup, err := serveBodyFileArg(args, toString(raw))
		if err != nil {
			serveJSON(w, http.StatusOK, serveCommandResult(command, "", err))
			return
		}
		defer cleanup()
	}
	output, err := serveInvokeCommand(args, updateV7TaskCmd)
	result := serveCommandResult(command+" "+taskID, output, err)
	result.TaskID = taskID
	status := http.StatusOK
	if err != nil && serveErrorIssue(err).Code == "CAS_CONFLICT" {
		status = http.StatusConflict
	}
	if err == nil {
		result.Reason = "task contract saved"
		s.invalidateProjectSnapshot(project.ProjectID)
		s.decorateTaskActionResultForProject(&result, taskID, project.ProjectID)
	}
	serveJSON(w, status, result)
}

func (s *serveServer) handleTaskCreateAction(w http.ResponseWriter, body serveActionBody) {
	const command = "tusker new task"
	args, project, err := serveBaseArgsForBody(s, body)
	if err != nil {
		serveJSON(w, http.StatusOK, serveCommandResult(command, "", err))
		return
	}
	actor, err := s.serveOperatorActor(body, "serve task create")
	if err != nil {
		status, result := serveOperatorActorResult(command, err)
		serveJSON(w, status, result)
		return
	}
	epic := strings.ToUpper(body.string("epic"))
	// ponytail: the ID is chosen before the create call so the wave add can
	// name it; newV7Task still refuses a collision, so a race only refuses.
	taskID := nextSafeV7TaskID(project.VaultRoot, firstNonEmpty(epic, "TSK"))
	args["id"], args["by"], args["quiet"] = taskID, actor, "true"
	args["title"], args["work-level"], args["epic"] = body.string("title"), body.string("workLevel"), epic
	args["owned-paths"] = body.csv("owned_paths", "ownedPaths")
	cleanup, err := serveBodyFileArg(args, body.string("body"))
	if err != nil {
		serveJSON(w, http.StatusOK, serveCommandResult(command, "", err))
		return
	}
	defer cleanup()
	output, err := serveInvokeCommand(args, newAuthoredV7Task)
	result := serveCommandResult(command, output, err)
	if err == nil {
		result.TaskID = taskID
		result.Reason = "created task " + taskID
		if wave := strings.ToUpper(body.string("wave")); wave != "" {
			waveArgs := Args{"vault": args["vault"], "repo": args["repo"], "id": wave, "tasks": taskID, "by": actor, "quiet": "true"}
			waveOutput, waveErr := serveInvokeCommand(waveArgs, waveV7AddCmd)
			if waveErr != nil {
				result = serveCommandResult("tusker wave add "+wave+" "+taskID, waveOutput, waveErr)
				result.TaskID = taskID
				result.Reason = "created task " + taskID + " but could not add it to " + wave + ": " + result.Reason
			} else {
				result.Reason += " in " + wave
			}
		}
		s.invalidateProjectSnapshot(project.ProjectID)
	}
	serveJSON(w, http.StatusOK, result)
}

// serveBodyFileArg hands an HTTP body to the CLI's --body-file flag through a
// private temp file, so the command reads exactly what the owner typed.
func serveBodyFileArg(args Args, text string) (func(), error) {
	file, err := os.CreateTemp("", "tusker-serve-task-body-*.md")
	if err != nil {
		return func() {}, err
	}
	cleanup := func() { _ = os.Remove(file.Name()) }
	_, err = file.WriteString(text)
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		cleanup()
		return func() {}, err
	}
	args["body-file"] = file.Name()
	return cleanup, nil
}
