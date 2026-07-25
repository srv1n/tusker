package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

var serveCommandStdoutMu sync.Mutex

type serveActionBody map[string]any

func (b serveActionBody) string(keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(toString(b[key])); value != "" {
			return value
		}
	}
	return ""
}

func (b serveActionBody) bool(key string) bool {
	switch strings.ToLower(strings.TrimSpace(toString(b[key]))) {
	case "1", "true", "yes", "y", "on":
		return true
	default:
		value, _ := b[key].(bool)
		return value
	}
}

func (b serveActionBody) csv(keys ...string) string {
	for _, key := range keys {
		if raw, ok := b[key]; ok {
			switch value := raw.(type) {
			case []any:
				var out []string
				for _, item := range value {
					out = append(out, toString(item))
				}
				return strings.Join(filterStrings(out), ",")
			case []string:
				return strings.Join(filterStrings(value), ",")
			default:
				if text := strings.TrimSpace(toString(raw)); text != "" {
					return text
				}
			}
		}
	}
	return ""
}

func serveReadActionBody(r *http.Request) (serveActionBody, error) {
	defer r.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(r.Body, 128*1024))
	if err != nil {
		return nil, err
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		return serveActionBody{}, nil
	}
	var body serveActionBody
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, tuskerError(errorInvalidArg, "invalid JSON body: "+err.Error())
	}
	return body, nil
}

func serveBaseArgs(s *serveServer) Args {
	return Args{"vault": s.vaultPath, "repo": s.repoRoot}
}

func serveBaseArgsForBody(s *serveServer, body serveActionBody) (Args, RegisteredProject, error) {
	project, err := s.projectForSnapshot(body.string("projectId", "project_id", "project"))
	if err != nil {
		return nil, RegisteredProject{}, err
	}
	return Args{"vault": project.VaultRoot, "repo": project.RepoRoot}, project, nil
}

func serveInvokeCommand(args Args, fn func(Args) error) (output string, runErr error) {
	serveCommandStdoutMu.Lock()
	defer serveCommandStdoutMu.Unlock()

	previous := os.Stdout
	tmp, err := os.CreateTemp("", "tusker-serve-stdout-*")
	if err != nil {
		return "", err
	}
	tmpPath := tmp.Name()
	defer func() {
		os.Stdout = previous
		_, _ = tmp.Seek(0, 0)
		out, readErr := io.ReadAll(tmp)
		output = strings.TrimSpace(string(out))
		if readErr != nil && runErr == nil {
			runErr = readErr
		}
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		if recovered := recover(); recovered != nil {
			runErr = tuskerError(errorHookFailed, fmt.Sprintf("serve command panic: %v", recovered))
		}
	}()

	os.Stdout = tmp
	runErr = fn(args)
	return output, runErr
}

func serveCommandResult(command, output string, err error) serveActionResult {
	if err != nil {
		issue := errorToIssue(err)
		reason := issue.Message
		if issue.Hint != "" {
			reason += " Hint: " + issue.Hint
		}
		return serveActionResult{OK: false, Refused: true, Reason: reason, Command: command, Output: output, Issue: &issue}
	}
	reason := firstNonEmpty(firstActionableLine(output, ""), "action completed")
	return serveActionResult{OK: true, Reason: reason, Command: command, Output: output}
}

func (s *serveServer) handleAPIMutation(w http.ResponseWriter, r *http.Request, path string) bool {
	if r.Method != http.MethodPost {
		return false
	}
	trimmed := strings.Trim(path, "/")
	parts := strings.Split(trimmed, "/")
	if len(parts) == 0 || parts[0] != "api" {
		return false
	}
	body, err := serveReadActionBody(r)
	if err != nil {
		issue := errorToIssue(err)
		serveJSON(w, http.StatusOK, serveActionResult{Refused: true, Reason: issue.Message, Issue: &issue})
		return true
	}
	if _, exists := body["projectId"]; !exists {
		if projectID := strings.TrimSpace(r.URL.Query().Get("project")); projectID != "" {
			body["projectId"] = projectID
		}
	}
	switch {
	case len(parts) == 2 && parts[1] == "projects":
		s.handleProjectRegisterAction(w, body)
	case len(parts) == 4 && parts[1] == "projects" && parts[3] == "automation":
		s.handleProjectAutomationAction(w, parts[2], body)
	case len(parts) == 4 && parts[1] == "projects" && parts[3] == "settings":
		s.handleProjectSettingsAction(w, parts[2], body)
	case len(parts) == 4 && parts[1] == "runs" && parts[3] == "redrive":
		s.handleRunRedrive(w, r, parts[2])
	case len(parts) == 4 && parts[1] == "runs" && parts[3] == "acknowledge":
		s.handleRunAcknowledge(w, r, parts[2])
	case len(parts) == 4 && parts[1] == "tasks" && parts[3] == "status":
		s.handleTaskStatusAction(w, parts[2], body)
	case len(parts) == 4 && parts[1] == "tasks" && parts[3] == "run":
		s.handleTaskRunDirective(w, parts[2], body)
	case len(parts) == 4 && parts[1] == "tasks" && parts[3] == "discard":
		s.handleTaskDiscardAction(w, parts[2], body)
	case len(parts) == 4 && parts[1] == "tasks" && parts[3] == "close":
		s.handleTaskCloseAction(w, parts[2], body)
	case len(parts) == 4 && parts[1] == "tasks" && parts[3] == "land":
		s.handleLandAction(w, parts[2], body)
	case len(parts) == 4 && parts[1] == "waves" && parts[3] == "land":
		s.handleLandAction(w, parts[2], body)
	case len(parts) == 4 && parts[1] == "gates" && (parts[3] == "satisfy" || parts[3] == "waive" || parts[3] == "obsolete"):
		s.handleGateAction(w, parts[2], parts[3], body)
	case len(parts) == 2 && parts[1] == "evidence":
		s.handleEvidenceAddAction(w, body)
	case len(parts) == 2 && parts[1] == "feedback":
		s.handleFeedbackAddAction(w, body)
	case len(parts) == 3 && parts[1] == "daemon":
		s.handleDaemonAction(w, parts[2], body)
	default:
		return false
	}
	return true
}

// handleTaskRunDirective records an operator's one-shot request. It never
// launches a runner: only the resident daemon can consume the directive.
func (s *serveServer) handleTaskRunDirective(w http.ResponseWriter, taskID string, body serveActionBody) {
	_, project, err := serveBaseArgsForBody(s, body)
	if err != nil {
		serveJSON(w, http.StatusOK, serveCommandResult("tusker task run", "", err))
		return
	}
	snap, err := s.loadSnapshotForProject(project.ProjectID)
	if err != nil {
		serveJSON(w, http.StatusOK, serveCommandResult("tusker task run", "", err))
		return
	}
	note, ok := snap.notesByID[strings.TrimSpace(taskID)]
	if !ok || serveNoteKind(note) != "task" {
		serveJSON(w, http.StatusOK, serveActionResult{Refused: true, Reason: "task not found"})
		return
	}
	status := stringField(note.Data, "status")
	if !containsString(snap.workflow.Tracker.ActiveStates, status) {
		serveJSON(w, http.StatusOK, serveActionResult{Refused: true, Reason: "task is not runnable; it must be ready or rework"})
		return
	}
	if run, ok := serveFindRun(snap.runs, trackerRecordID(note)); ok && isDispatchingLeaseState(run.LeaseState) {
		serveJSON(w, http.StatusOK, serveActionResult{Refused: true, Reason: "task already has a live run"})
		return
	}
	daemon, _ := s.store.DaemonStatus()
	if !boolFromAny(daemon["daemon_alive"]) {
		serveJSON(w, http.StatusOK, serveActionResult{Refused: true, Reason: "daemon is not running; start it before queuing a one-shot run"})
		return
	}
	if explanation, ok := snap.queue[stringField(note.Data, "id")]; ok {
		blockers := make([]string, 0, len(explanation.Blockers))
		for _, blocker := range explanation.Blockers {
			if !runDirectiveBypassableBlocker(blocker) {
				blockers = append(blockers, blocker)
			}
		}
		if len(blockers) > 0 {
			serveJSON(w, http.StatusOK, serveActionResult{Refused: true, Reason: "task cannot be dispatched: " + strings.Join(blockers, "; ")})
			return
		}
	} else {
		serveJSON(w, http.StatusOK, serveActionResult{Refused: true, Reason: "task dispatchability could not be verified; refresh the project and try again"})
		return
	}
	now := time.Now().UTC()
	directive := RunDirective{ProjectID: project.ProjectID, RecordID: trackerRecordID(note), Actor: serveOperatorActor(), CreatedAt: now.Format(time.RFC3339), ExpiresAt: now.Add(10 * time.Minute).Format(time.RFC3339), State: "queued"}
	queued, err := s.store.QueueRunDirective(directive)
	if err != nil {
		serveJSON(w, http.StatusOK, serveCommandResult("tusker task run", "", err))
		return
	}
	if !queued {
		reason := "task is already queued for dispatch"
		if runs, listErr := s.store.ListRuns(); listErr == nil {
			for _, run := range runs {
				if run.ProjectID == project.ProjectID && run.RecordID == directive.RecordID && isDispatchingLeaseState(run.LeaseState) {
					reason = "task already has a live run"
					break
				}
			}
		}
		serveJSON(w, http.StatusOK, serveActionResult{Refused: true, Reason: reason})
		return
	}
	s.refreshProjectSnapshot(project.ProjectID)
	serveJSON(w, http.StatusOK, serveActionResult{OK: true, Reason: "queued for daemon dispatch", Command: "tusker task run"})
}

func serveOperatorActor() string {
	name := strings.TrimSpace(firstNonEmpty(os.Getenv("USER"), os.Getenv("LOGNAME"), defaultActorName()))
	if strings.HasPrefix(name, "human:") {
		return name
	}
	return "human:" + name
}

func (s *serveServer) handleProjectRegisterAction(w http.ResponseWriter, body serveActionBody) {
	repoRoot := body.string("repoRoot", "repo_root", "repo", "path")
	if repoRoot == "" {
		serveJSON(w, http.StatusOK, serveActionResult{Refused: true, Reason: "project registration requires a repository path"})
		return
	}
	absRepo, err := filepath.Abs(repoRoot)
	if err != nil {
		serveJSON(w, http.StatusOK, serveCommandResult("tusker projects add", "", err))
		return
	}
	vaultRoot := body.string("vaultRoot", "vault_root", "vault")
	if vaultRoot == "" {
		vaultRoot = filepath.Join(absRepo, ".tusker")
	}
	absVault, err := filepath.Abs(vaultRoot)
	if err == nil {
		err = validateProjectStorageBoundary(absRepo, absVault)
	}
	if err == nil {
		_, err = loadWorkflow(absVault)
	}
	if err != nil {
		result := serveCommandResult("tusker projects add", "", err)
		serveJSON(w, http.StatusOK, result)
		return
	}
	project := newRegisteredProject(absRepo, absVault)
	project.Enabled = false
	project.Health = projectHealthDisabled
	project, created, err := s.store.RegisterProject(project)
	if err != nil {
		serveJSON(w, http.StatusOK, serveCommandResult("tusker projects add", "", err))
		return
	}
	if !created {
		serveJSON(w, http.StatusOK, serveActionResult{
			OK: true, ProjectID: project.ProjectID,
			Reason:  "Project already registered; using the existing registration.",
			Command: "tusker projects add",
		})
		return
	}
	go s.warmSnapshot(project.ProjectID)
	serveJSON(w, http.StatusOK, serveActionResult{
		OK: true, ProjectID: project.ProjectID,
		Reason:  "Project registered. Daemon automation is off until enabled in project settings.",
		Command: "tusker projects add",
	})
}

func (s *serveServer) handleProjectAutomationAction(w http.ResponseWriter, projectID string, body serveActionBody) {
	raw, present := body["enabled"]
	if !present {
		serveJSON(w, http.StatusOK, serveActionResult{Refused: true, ProjectID: projectID, Reason: "project automation action requires enabled"})
		return
	}
	enabled := serveActionBody{"enabled": raw}.bool("enabled")
	loaded, err := loadRegisteredProjects(s.store, registeredProjectLoadOptions{
		MetadataOnly: !enabled,
		LoadDisabled: enabled,
		ProjectID:    projectID,
	})
	var project *RegisteredProject
	if err == nil {
		if len(loaded) == 1 {
			project = &loaded[0].Project
		}
		if project == nil {
			err = tuskerError(errorNotFound, "project not found: "+projectID)
		} else if enabled && loaded[0].LoadError != nil {
			err = loaded[0].LoadError
		}
	}
	if err == nil && enabled {
		err = validateProjectStorageBoundary(project.RepoRoot, project.VaultRoot)
	}
	if err == nil {
		_, err = setProjectLocalConfigWithReadback(project.VaultRoot, "automation.enabled", enabled)
	}
	if err == nil {
		err = s.store.SetProjectEnabled(projectID, enabled)
	}
	if err != nil {
		result := serveCommandResult("tusker projects automation", "", err)
		result.ProjectID = projectID
		serveJSON(w, http.StatusOK, result)
		return
	}
	state := "disabled"
	if enabled {
		state = "enabled"
		go s.warmSnapshot(projectID)
	}
	serveJSON(w, http.StatusOK, serveActionResult{
		OK: true, ProjectID: projectID, Reason: "Daemon automation " + state + " for " + project.Name,
		Command: "tusker projects " + state,
	})
}

func (s *serveServer) handleProjectSettingsAction(w http.ResponseWriter, projectID string, body serveActionBody) {
	loaded, err := loadRegisteredProjects(s.store, registeredProjectLoadOptions{MetadataOnly: true, LoadDisabled: true, ProjectID: projectID})
	if err != nil || len(loaded) != 1 {
		if err == nil {
			err = tuskerError(errorNotFound, "project not found: "+projectID)
		}
		serveJSON(w, http.StatusOK, serveCommandResult("tusker projects settings", "", err))
		return
	}
	project := loaded[0].Project
	var key string
	var value any
	if mode := body.string("workspaceMode"); mode != "" {
		if !validWorkspaceStrategy(mode) {
			serveJSON(w, http.StatusOK, serveActionResult{Refused: true, Reason: "invalid workspace mode"})
			return
		}
		key, value = "workspace.strategy", mode
	} else if limit := body.string("maxActiveRunsPerProject"); limit != "" {
		n, parseErr := strconv.Atoi(limit)
		if parseErr != nil || n < 1 {
			serveJSON(w, http.StatusOK, serveActionResult{Refused: true, Reason: "concurrency must be positive"})
			return
		}
		key, value = "runtime.max_active_runs_per_project", n
	} else {
		serveJSON(w, http.StatusOK, serveActionResult{Refused: true, Reason: "no supported setting supplied"})
		return
	}
	_, err = setProjectLocalConfigWithReadback(project.VaultRoot, key, value)
	serveJSON(w, http.StatusOK, serveCommandResult("tusker projects settings", "", err))
}

func (s *serveServer) handleTaskStatusAction(w http.ResponseWriter, taskID string, body serveActionBody) {
	status := strings.ToLower(firstNonEmpty(body.string("status"), body.string("to")))
	if status == "" {
		result := serveActionResult{Refused: true, Reason: "task status action requires status"}
		serveJSON(w, http.StatusOK, result)
		return
	}
	args, project, projectErr := serveBaseArgsForBody(s, body)
	if projectErr != nil {
		serveJSON(w, http.StatusOK, serveCommandResult("tusker status", "", projectErr))
		return
	}
	args["id"] = strings.ToUpper(strings.TrimSpace(taskID))
	args["status"] = status
	args["reason"] = firstNonEmpty(body.string("reason"), "operator action from serve")
	if actor := body.string("actor", "by"); actor != "" {
		args["by"] = actor
	} else if status == "rework" {
		if action := s.humanActionForTask(project.ProjectID, args["id"]); action != nil {
			args["by"] = s.humanActionOwner(project.ProjectID, action.GateID)
		}
	}
	if body.bool("force") {
		args["force"] = "true"
	}
	output, err := serveInvokeCommand(args, statusV7Cmd)
	result := serveCommandResult("tusker status "+args["id"]+" "+status, output, err)
	result.TaskID = args["id"]
	s.invalidateProjectSnapshot(project.ProjectID)
	s.decorateTaskActionResultForProject(&result, args["id"], project.ProjectID)
	serveJSON(w, http.StatusOK, result)
}

func (s *serveServer) handleTaskDiscardAction(w http.ResponseWriter, taskID string, body serveActionBody) {
	args, project, projectErr := serveBaseArgsForBody(s, body)
	if projectErr != nil {
		serveJSON(w, http.StatusOK, serveCommandResult("tusker discard", "", projectErr))
		return
	}
	args["id"] = strings.ToUpper(strings.TrimSpace(taskID))
	impact, err := v7DiscardImpactForTask(project.VaultRoot, args["id"])
	if err != nil {
		serveJSON(w, http.StatusOK, serveCommandResult("tusker discard "+args["id"]+" --dry-run", "", err))
		return
	}
	servedImpact := serveDiscardImpactFromV7(impact)
	if body.bool("dryRun") || body.bool("dry_run") {
		serveJSON(w, http.StatusOK, serveActionResult{
			OK: true, Reason: "discard impact calculated", TaskID: args["id"], Discard: &servedImpact,
		})
		return
	}
	args["reason"] = body.string("reason")
	args["dependents"] = body.string("dependents")
	if actor := body.string("actor", "by"); actor != "" {
		args["by"] = actor
	}
	output, err := serveInvokeCommand(args, discardV7Cmd)
	result := serveCommandResult("tusker discard "+args["id"], output, err)
	result.TaskID = args["id"]
	result.Discard = &servedImpact
	s.invalidateProjectSnapshot(project.ProjectID)
	s.decorateTaskActionResultForProject(&result, args["id"], project.ProjectID)
	serveJSON(w, http.StatusOK, result)
}

func serveDiscardImpactFromV7(impact v7DiscardImpact) serveDiscardImpact {
	convert := func(rows []v7DiscardDependent) []serveDiscardDependent {
		out := make([]serveDiscardDependent, 0, len(rows))
		for _, row := range rows {
			out = append(out, serveDiscardDependent{ID: row.ID, Title: row.Title, Status: row.Status})
		}
		return out
	}
	return serveDiscardImpact{
		TaskID: impact.TaskID, Title: impact.Title, Status: impact.Status,
		DirectDependents: convert(impact.DirectDependents), CascadeDependents: convert(impact.CascadeDependents),
		OpenGates: append([]string{}, impact.OpenGates...), RequiresResolution: impact.RequiresResolution,
		PreservesHistory: impact.PreservesHistory,
	}
}

func (s *serveServer) handleTaskCloseAction(w http.ResponseWriter, taskID string, body serveActionBody) {
	args, project, projectErr := serveBaseArgsForBody(s, body)
	if projectErr != nil {
		serveJSON(w, http.StatusOK, serveCommandResult("tusker close", "", projectErr))
		return
	}
	args["id"] = strings.ToUpper(strings.TrimSpace(taskID))
	args["reason"] = firstNonEmpty(body.string("reason"), "accepted from serve")
	actor := body.string("actor", "by", "acceptedBy", "accepted_by")
	if actor != "" {
		args["by"] = actor
	}
	if body.bool("force") {
		args["force"] = "true"
	}
	output, err := serveInvokeCommand(args, closeV7Cmd)
	result := serveCommandResult("tusker close "+args["id"], output, err)
	result.TaskID = args["id"]
	s.invalidateProjectSnapshot(project.ProjectID)
	s.decorateTaskActionResultForProject(&result, args["id"], project.ProjectID)
	serveJSON(w, http.StatusOK, result)
}

func (s *serveServer) handleLandAction(w http.ResponseWriter, target string, body serveActionBody) {
	target = strings.ToUpper(strings.TrimSpace(target))
	args, project, projectErr := serveBaseArgsForBody(s, body)
	if projectErr != nil {
		serveJSON(w, http.StatusOK, serveCommandResult("tusker land", "", projectErr))
		return
	}
	args["_pos0"] = target
	if from := body.string("from", "branch", "source"); from != "" {
		args["from"] = from
	}
	if branch := body.string("branch"); branch != "" && !v7WaveIDPattern.MatchString(target) {
		args["branch"] = branch
	}
	output, err := serveInvokeCommand(args, landV7Cmd)
	result := serveCommandResult("tusker land "+target, output, err)
	result.TaskID = target
	s.invalidateProjectSnapshot(project.ProjectID)
	if v7TaskIDPattern.MatchString(target) {
		s.decorateTaskActionResultForProject(&result, target, project.ProjectID)
	}
	serveJSON(w, http.StatusOK, result)
}

func (s *serveServer) handleGateAction(w http.ResponseWriter, gateID, action string, body serveActionBody) {
	args, project, projectErr := serveBaseArgsForBody(s, body)
	if projectErr != nil {
		serveJSON(w, http.StatusOK, serveCommandResult("tusker gate", "", projectErr))
		return
	}
	args["_pos0"] = action
	args["id"] = strings.ToUpper(strings.TrimSpace(gateID))
	if reason := body.string("reason"); reason != "" {
		args["reason"] = reason
	}
	if evidence := body.string("evidence"); evidence != "" {
		args["evidence"] = evidence
	}
	if refs := body.csv("evidenceRefs", "evidence_refs", "evidence-refs", "evidenceRef", "evidence_ref"); refs != "" {
		args["evidence-refs"] = refs
	}
	if actor := body.string("actor", "by"); actor != "" {
		args["by"] = actor
	} else if owner := s.humanActionOwner(project.ProjectID, args["id"]); owner != "" {
		// Completing an owner action must be recorded as that human owner, not
		// silently attributed to the default agent actor.
		args["by"] = owner
	}
	if body.bool("force") {
		args["force"] = "true"
	}
	output, err := serveInvokeCommand(args, gateV7Cmd)
	result := serveCommandResult("tusker gate "+action+" "+args["id"], output, err)
	result.GateID = args["id"]
	s.invalidateProjectSnapshot(project.ProjectID)
	if gate := s.findGateDetailForProject(args["id"], project.ProjectID); gate != nil {
		result.Gate = gate
		taskIDParts := append([]string{body.string("taskId", "task_id", "task")}, gate.Blocks...)
		taskID := firstNonEmpty(taskIDParts...)
		s.decorateTaskActionResultForProject(&result, taskID, project.ProjectID)
	}
	serveJSON(w, http.StatusOK, result)
}

func (s *serveServer) humanActionForTask(projectID, taskID string) *serveHumanAction {
	snap, err := s.loadFreshSnapshotForProject(projectID)
	if err != nil {
		return nil
	}
	task, ok := snap.notesByID[taskID]
	if !ok || serveNoteKind(task) != "task" {
		return nil
	}
	return serveHumanActionForTask(snap, task)
}

func (s *serveServer) humanActionOwner(projectID, gateID string) string {
	detail := s.findGateDetailForProject(gateID, projectID)
	if detail == nil || !serveHumanOwner(detail.Owner) {
		return ""
	}
	return detail.Owner
}

func (s *serveServer) handleEvidenceAddAction(w http.ResponseWriter, body serveActionBody) {
	taskID := strings.ToUpper(firstNonEmpty(body.string("taskId", "task_id", "id"), body.string("task")))
	args, project, projectErr := serveBaseArgsForBody(s, body)
	if projectErr != nil {
		serveJSON(w, http.StatusOK, serveCommandResult("tusker evidence add", "", projectErr))
		return
	}
	args["id"] = taskID
	for _, pair := range []struct{ arg, key string }{
		{"kind", "kind"},
		{"summary", "summary"},
		{"command", "command"},
		{"result", "result"},
		{"path", "path"},
		{"external-url", "externalUrl"},
		{"external-url", "external_url"},
		{"status", "status"},
		{"accepted-by", "acceptedBy"},
		{"accepted-by", "accepted_by"},
		{"checked-by", "checkedBy"},
		{"checked-by", "checked_by"},
		{"redaction-note", "redactionNote"},
		{"redaction-note", "redaction_note"},
		{"evidence-id", "evidenceId"},
		{"evidence-id", "evidence_id"},
		{"by", "actor"},
	} {
		if value := body.string(pair.key); value != "" {
			args[pair.arg] = value
		}
	}
	if covers := body.csv("covers"); covers != "" {
		args["covers"] = covers
	}
	for _, key := range []string{"link-only", "allow-self-check", "redacted"} {
		bodyKey := strings.ReplaceAll(key, "-", "")
		if body.bool(key) || body.bool(bodyKey) || body.bool(strings.ReplaceAll(key, "-", "_")) {
			args[key] = "true"
		}
	}
	output, err := serveInvokeCommand(args, evidenceV7AddCmd)
	result := serveCommandResult("tusker evidence add "+taskID, output, err)
	result.TaskID = taskID
	result.EvidenceID = firstNonEmpty(args["evidence-id"], serveEvidenceIDFromOutput(output))
	s.invalidateProjectSnapshot(project.ProjectID)
	if result.EvidenceID != "" {
		if evidence := s.findEvidenceDocForProject(result.EvidenceID, project.ProjectID); evidence != nil {
			result.Evidence = evidence
			result.EvidenceID = evidence.ID
		}
	}
	s.decorateTaskActionResultForProject(&result, taskID, project.ProjectID)
	serveJSON(w, http.StatusOK, result)
}

func (s *serveServer) handleFeedbackAddAction(w http.ResponseWriter, body serveActionBody) {
	args, project, projectErr := serveBaseArgsForBody(s, body)
	if projectErr != nil {
		serveJSON(w, http.StatusOK, serveCommandResult("tusker feedback add", "", projectErr))
		return
	}
	args["_pos0"] = "add"
	for _, key := range []string{"context", "friction", "product-idea", "productIdea", "impact", "related", "theme", "priority-hint", "priorityHint", "affected-command", "affectedCommand", "dedupe-key", "dedupeKey", "actor", "slug", "date"} {
		if value := body.string(key); value != "" {
			args[serveFeedbackArgName(key)] = value
		}
	}
	for _, key := range []string{"allow-long", "allowLong", "allow-progress-report", "allowProgressReport", "allow-duplicate", "allowDuplicate"} {
		if body.bool(key) {
			args[serveFeedbackArgName(key)] = "true"
		}
	}
	output, err := serveInvokeCommand(args, feedbackV7Cmd)
	result := serveCommandResult("tusker feedback add", output, err)
	result.FeedbackPath = serveFeedbackPathFromOutput(output)
	s.invalidateProjectSnapshot(project.ProjectID)
	serveJSON(w, http.StatusOK, result)
}

func (s *serveServer) handleDaemonAction(w http.ResponseWriter, action string, body serveActionBody) {
	var (
		command string
		fn      func(Args) error
		args    = serveBaseArgs(s)
	)
	args["json"] = "true"
	switch action {
	case "start":
		s.handleDaemonStartAction(w, body)
		return
	case "stop":
		command, fn = "tusker daemon stop", daemonStopCmd
		if body.bool("drain") {
			args["drain"] = "true"
		}
	case "resume":
		command, fn = "tusker daemon resume", daemonResumeCmd
	case "limits":
		command, fn = "tusker daemon limits", daemonLimitsCmd
		if limit := firstNonEmpty(body.string("maxActiveRuns", "max_active_runs"), body.string("limit")); limit != "" {
			args["max-active-runs"] = limit
		}
		for _, mapping := range []struct {
			arg  string
			keys []string
		}{
			{arg: "disk-pressure-enabled", keys: []string{"diskPressureEnabled", "disk_pressure_enabled"}},
			{arg: "disk-pressure-min-free-bytes", keys: []string{"diskPressureMinFreeBytes", "disk_pressure_min_free_bytes"}},
			{arg: "disk-pressure-min-free-percent", keys: []string{"diskPressureMinFreePercent", "disk_pressure_min_free_percent"}},
		} {
			if value := body.string(mapping.keys...); value != "" {
				args[mapping.arg] = value
			}
		}
	default:
		result := serveActionResult{Refused: true, Reason: "unknown daemon action: " + action}
		serveJSON(w, http.StatusOK, result)
		return
	}
	output, err := serveInvokeCommand(args, fn)
	result := serveCommandResult(command, output, err)
	if status := s.currentDaemonStatus(); status != nil {
		result.Daemon = status
	}
	serveJSON(w, http.StatusOK, result)
}

func (s *serveServer) handleDaemonStartAction(w http.ResponseWriter, body serveActionBody) {
	if readDaemonLiveness(DefaultStateRoot(), time.Now().UTC()).Alive {
		result := serveActionResult{OK: true, Reason: "daemon is already running", Command: "tusker daemon run"}
		if status := s.currentDaemonStatus(); status != nil {
			result.Daemon = status
		}
		serveJSON(w, http.StatusOK, result)
		return
	}
	args := serveBaseArgs(s)
	if body.bool("once") {
		args["once"] = "true"
	}
	done := make(chan error, 1)
	go func() {
		done <- daemonRunCmd(args)
	}()
	select {
	case err := <-done:
		result := serveCommandResult("tusker daemon run", "", err)
		serveJSON(w, http.StatusOK, result)
	case <-time.After(250 * time.Millisecond):
		result := serveActionResult{OK: true, Reason: "daemon start requested", Command: "tusker daemon run"}
		serveJSON(w, http.StatusOK, result)
	}
}

func (s *serveServer) decorateTaskActionResult(result *serveActionResult, taskID string) {
	s.decorateTaskActionResultForProject(result, taskID, "")
}

func (s *serveServer) decorateTaskActionResultForProject(result *serveActionResult, taskID, projectID string) {
	if taskID == "" {
		return
	}
	snap, err := s.loadFreshSnapshotForProject(projectID)
	if err != nil {
		return
	}
	task, ok := snap.notesByID[taskID]
	if !ok || serveNoteKind(task) != "task" {
		return
	}
	detail := serveTaskDetail{
		serveTaskCapsule: serveTaskCapsuleFor(snap, task),
		Intent:           sectionContent(task.Body, "## Intent"),
		Acceptance:       serveAcceptanceRows(task),
		NonGoals:         serveBullets(sectionContent(task.Body, "## Non-goals")),
		Verification:     serveVerificationRows(task),
		Evidence:         serveEvidenceCards(snap, task),
		KnowledgeDelta:   sectionContent(task.Body, "## Knowledge delta"),
		Deps:             serveTaskDeps(snap, task),
		Gates:            serveGatesForTask(snap, taskID),
		HumanAction:      serveHumanActionForTask(snap, task),
		RunHistory:       serveRunHistory(s, snap, taskID),
	}
	result.Task = &detail
	result.CanonicalStatus = detail.RawStatus
}

func (s *serveServer) currentDaemonStatus() *serveDaemonStatus {
	snap, err := s.loadSnapshot()
	if err != nil {
		return nil
	}
	return s.daemonStatusFromSnapshot(snap)
}

func (s *serveServer) findGateDetail(id string) *serveGateDetail {
	return s.findGateDetailForProject(id, "")
}

func (s *serveServer) findGateDetailForProject(id, projectID string) *serveGateDetail {
	snap, err := s.loadFreshSnapshotForProject(projectID)
	if err != nil {
		return nil
	}
	for _, gate := range snap.gates {
		if stringField(gate.Data, "id") == id {
			detail := serveGateDetailFromNote(gate)
			return &detail
		}
	}
	return nil
}

func (s *serveServer) findEvidenceDoc(id string) *serveEvidenceDoc {
	return s.findEvidenceDocForProject(id, "")
}

func (s *serveServer) findEvidenceDocForProject(id, projectID string) *serveEvidenceDoc {
	snap, err := s.loadFreshSnapshotForProject(projectID)
	if err != nil {
		return nil
	}
	for _, evidence := range snap.evidence {
		if stringField(evidence.Data, "id") == id {
			doc := serveEvidenceDocFromNote(evidence)
			return &doc
		}
	}
	return nil
}

func serveEvidenceIDFromOutput(output string) string {
	fields := strings.Fields(output)
	for i, field := range fields {
		if strings.EqualFold(field, "evidence") && i+1 < len(fields) {
			return strings.Trim(fields[i+1], ":,")
		}
	}
	return ""
}

func serveFeedbackPathFromOutput(output string) string {
	output = strings.TrimSpace(output)
	if strings.HasPrefix(output, "Wrote feedback note ") {
		return strings.TrimSpace(strings.TrimPrefix(output, "Wrote feedback note "))
	}
	return ""
}

func serveFeedbackArgName(key string) string {
	key = strings.ReplaceAll(key, "_", "-")
	switch key {
	case "productIdea":
		return "product-idea"
	case "priorityHint":
		return "priority-hint"
	case "affectedCommand":
		return "affected-command"
	case "dedupeKey":
		return "dedupe-key"
	case "allowLong":
		return "allow-long"
	case "allowProgressReport":
		return "allow-progress-report"
	case "allowDuplicate":
		return "allow-duplicate"
	default:
		return key
	}
}

func (s *serveServer) handleGates(w http.ResponseWriter, r *http.Request) {
	snap, err := s.loadSnapshotForRequest(r)
	if err != nil {
		serveJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	taskFilter := strings.TrimSpace(r.URL.Query().Get("task"))
	out := []serveGateDetail{}
	for _, gate := range snap.gates {
		if taskFilter != "" && !serveGateBlocksTask(gate, taskFilter) {
			continue
		}
		out = append(out, serveGateDetailFromNote(gate))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	serveJSON(w, http.StatusOK, out)
}

func (s *serveServer) handleGate(w http.ResponseWriter, r *http.Request, id string) {
	snap, err := s.loadSnapshotForRequest(r)
	if err == nil {
		for _, gate := range snap.gates {
			if stringField(gate.Data, "id") == strings.TrimSpace(id) {
				detail := serveGateDetailFromNote(gate)
				serveJSON(w, http.StatusOK, &detail)
				return
			}
		}
	}
	serveJSON(w, http.StatusNotFound, map[string]any{"error": "gate not found"})
}

func (s *serveServer) handleEvidence(w http.ResponseWriter, r *http.Request) {
	snap, err := s.loadSnapshotForRequest(r)
	if err != nil {
		serveJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	taskFilter := strings.TrimSpace(r.URL.Query().Get("task"))
	out := []serveEvidenceDoc{}
	for _, evidence := range snap.evidence {
		if taskFilter != "" && stringField(evidence.Data, "task") != taskFilter && !serveEvidenceCoversTask(evidence, taskFilter) {
			continue
		}
		out = append(out, serveEvidenceDocFromNote(evidence))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	serveJSON(w, http.StatusOK, out)
}

func (s *serveServer) handleEvidenceDoc(w http.ResponseWriter, r *http.Request, id string) {
	snap, err := s.loadSnapshotForRequest(r)
	if err == nil {
		for _, evidence := range snap.evidence {
			if stringField(evidence.Data, "id") == strings.TrimSpace(id) {
				doc := serveEvidenceDocFromNote(evidence)
				serveJSON(w, http.StatusOK, &doc)
				return
			}
		}
	}
	serveJSON(w, http.StatusNotFound, map[string]any{"error": "evidence not found"})
}

func (s *serveServer) handleDecisions(w http.ResponseWriter, r *http.Request) {
	snap, err := s.loadSnapshotForRequest(r)
	if err != nil {
		serveJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	epicFilter := strings.TrimSpace(r.URL.Query().Get("epic"))
	out := []serveDecisionDoc{}
	for _, decision := range snap.decisions {
		if epicFilter != "" && stringField(decision.Data, "epic") != epicFilter {
			continue
		}
		out = append(out, serveDecisionDocFromNote(decision))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	serveJSON(w, http.StatusOK, out)
}

func (s *serveServer) handleDecision(w http.ResponseWriter, r *http.Request, id string) {
	snap, err := s.loadSnapshotForRequest(r)
	if err != nil {
		serveJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	for _, decision := range snap.decisions {
		if stringField(decision.Data, "id") == strings.TrimSpace(id) {
			serveJSON(w, http.StatusOK, serveDecisionDocFromNote(decision))
			return
		}
	}
	serveJSON(w, http.StatusNotFound, map[string]any{"error": "decision not found"})
}

func (s *serveServer) handleFeedback(w http.ResponseWriter, r *http.Request) {
	project, err := s.projectForSnapshot(strings.TrimSpace(r.URL.Query().Get("project")))
	if err != nil {
		serveJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
		return
	}
	records, err := feedbackRecordsForVault(project.VaultRoot, project.RepoRoot, time.Time{})
	if err != nil {
		serveJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	out := []serveFeedbackDoc{}
	for _, record := range records {
		out = append(out, serveFeedbackDocFromRecord(record))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].RelativePath > out[j].RelativePath })
	serveJSON(w, http.StatusOK, out)
}

func (s *serveServer) handleFeedbackDoc(w http.ResponseWriter, r *http.Request, ref string) {
	ref = strings.TrimPrefix(strings.TrimSpace(ref), "/")
	project, err := s.projectForSnapshot(strings.TrimSpace(r.URL.Query().Get("project")))
	if err != nil {
		serveJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
		return
	}
	records, err := feedbackRecordsForVault(project.VaultRoot, project.RepoRoot, time.Time{})
	if err != nil {
		serveJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	for _, record := range records {
		if record.RelativePath == ref || filepath.Base(record.RelativePath) == ref {
			serveJSON(w, http.StatusOK, serveFeedbackDocFromRecord(record))
			return
		}
	}
	serveJSON(w, http.StatusNotFound, map[string]any{"error": "feedback not found"})
}

func (s *serveServer) handleAttempts(w http.ResponseWriter, r *http.Request) {
	taskID := strings.TrimSpace(r.URL.Query().Get("task"))
	snap, err := s.loadSnapshotForRequest(r)
	if err != nil {
		serveJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	out := []serveAttemptDetail{}
	for _, run := range snap.runs {
		if taskID != "" && run.ItemID != taskID && run.RecordID != taskID {
			continue
		}
		attempts, _ := s.store.ListAttemptsForRun(run.ProjectID, run.RecordID)
		for _, attempt := range attempts {
			out = append(out, s.serveAttemptDetail(run, attempt))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt > out[j].StartedAt })
	serveJSON(w, http.StatusOK, out)
}

func (s *serveServer) handleAttempt(w http.ResponseWriter, r *http.Request, id string) {
	id = strings.TrimSpace(id)
	snap, err := s.loadSnapshotForRequest(r)
	if err != nil {
		serveJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	for _, run := range snap.runs {
		attempts, _ := s.store.ListAttemptsForRun(run.ProjectID, run.RecordID)
		for _, attempt := range attempts {
			if attempt.AttemptID == id {
				serveJSON(w, http.StatusOK, s.serveAttemptDetail(run, attempt))
				return
			}
		}
	}
	serveJSON(w, http.StatusNotFound, map[string]any{"error": "attempt not found"})
}

func (s *serveServer) serveAttemptDetail(run RunStatus, attempt RunAttempt) serveAttemptDetail {
	turns, _ := s.store.ListTurnsForAttempt(attempt.AttemptID)
	return serveAttemptDetail{
		ID:             attempt.AttemptID,
		TaskID:         firstNonEmpty(attempt.ItemID, run.ItemID, attempt.RecordID),
		ProjectID:      attempt.ProjectID,
		Runner:         attempt.Runner,
		Lane:           serveLane(firstNonEmpty(attempt.Lane, run.Lane)),
		Outcome:        serveRunOutcomeFromAttempt(attempt.Outcome, run.LeaseState),
		StartedAt:      attempt.StartedAt,
		FinishedAt:     attempt.FinishedAt,
		DurationSec:    serveDurationSec(attempt.StartedAt, firstNonEmpty(attempt.FinishedAt, run.UpdatedAt), s.now()),
		WorkspacePath:  attempt.WorkspacePath,
		BranchName:     attempt.BranchName,
		PullRequestURL: attempt.PullRequestURL,
		PromptPath:     attempt.PromptPath,
		EventSinkPath:  attempt.EventSinkPath,
		RawLogPath:     attempt.RawLogPath,
		StatusPath:     attempt.StatusPath,
		LastError:      attempt.LastError,
		LogsSummary:    attempt.LogsSummary,
		FinalSummary:   attempt.FinalSummary,
		Turns:          turns,
		Events:         serveRunEvents(run, []RunAttempt{attempt}),
	}
}

func serveGateDetailFromNote(gate Note) serveGateDetail {
	base := serveGateFromNote(gate)
	status := stringField(gate.Data, "status")
	reason := firstNonEmpty(stringField(gate.Data, "waive_reason"), stringField(gate.Data, "obsolete_reason"))
	title := stringField(gate.Data, "title")
	return serveGateDetail{
		serveGate:           base,
		Title:               title,
		Status:              status,
		Blocking:            boolField(gate.Data, "blocking"),
		Blocks:              serveGateBlockIDs(gate),
		Reason:              reason,
		UpdatedAt:           stringField(gate.Data, "updated_at"),
		Body:                gate.Body,
		Action:              firstNonEmpty(stringField(gate.Data, "action"), sectionContent(gate.Body, "## Action"), title),
		WhyAgentCannot:      firstNonEmpty(stringField(gate.Data, "why_agent_cannot"), sectionContent(gate.Body, "## Why agent cannot do this")),
		CompletionCondition: firstNonEmpty(stringField(gate.Data, "verification"), sectionContent(gate.Body, "## Verification")),
		HumanOwned:          serveHumanOwner(stringField(gate.Data, "owner")),
	}
}

func serveEvidenceDocFromNote(evidence Note) serveEvidenceDoc {
	id := stringField(evidence.Data, "id")
	return serveEvidenceDoc{
		ID:            id,
		TaskID:        stringField(evidence.Data, "task"),
		Title:         firstNonEmpty(stringField(evidence.Data, "title"), id),
		Kind:          firstNonEmpty(stringField(evidence.Data, "evidence_kind"), stringField(evidence.Data, "kind")),
		Status:        stringField(evidence.Data, "status"),
		Covers:        normalizeList(evidence.Data["covers"]),
		ArtifactPaths: normalizeList(evidence.Data["artifact_paths"]),
		CreatedBy:     stringField(evidence.Data, "created_by"),
		CreatedAt:     stringField(evidence.Data, "created_at"),
		AcceptedBy:    stringField(evidence.Data, "accepted_by"),
		AcceptedAt:    stringField(evidence.Data, "accepted_at"),
		Summary:       strings.TrimSpace(sectionContent(evidence.Body, "## Summary")),
		RelativePath:  evidence.RelativePath,
	}
}

func serveDecisionDocFromNote(decision Note) serveDecisionDoc {
	return serveDecisionDoc{
		ID:           stringField(decision.Data, "id"),
		Title:        stringField(decision.Data, "title"),
		EpicID:       stringField(decision.Data, "epic"),
		Status:       stringField(decision.Data, "status"),
		Decision:     firstNonEmpty(stringField(decision.Data, "decision"), sectionContent(decision.Body, "## Decision")),
		DecidedBy:    stringField(decision.Data, "decided_by"),
		DecidedAt:    stringField(decision.Data, "decided_at"),
		WorkStreams:  serveBullets(sectionContent(decision.Body, "## Work streams")),
		RelativePath: decision.RelativePath,
	}
}

func serveFeedbackDocFromRecord(record feedbackRecord) serveFeedbackDoc {
	return serveFeedbackDoc{
		ID:              filepath.Base(record.RelativePath),
		Date:            record.Date,
		Actor:           record.Actor,
		Slug:            record.Slug,
		RelativePath:    record.RelativePath,
		Context:         record.Fields["context"],
		Friction:        record.Fields["friction"],
		ProductIdea:     record.Fields["product-idea"],
		Impact:          record.Fields["impact"],
		Related:         splitFeedbackList(record.Fields["related"]),
		Theme:           record.Theme,
		PriorityHint:    record.PriorityHint,
		AffectedCommand: record.AffectedCommand,
		Fields:          record.Fields,
		Issues:          record.Issues,
	}
}

// serveAcknowledgeReason is the retirement reason recorded when an operator
// clears a settled failed run from the serve attention surface. It mirrors
// `tusker runs retire --reason <text>` so the CLI and UI leave the same trail.
const serveAcknowledgeReason = "acknowledged from serve UI"

// handleRunAcknowledge clears a settled failed run from the attention surface by
// retiring the runtime row through retireRuntimeRun — the same transition as
// `tusker runs retire`. It refuses (409) while the run is still executing or
// holding a live lease, so an operator can never acknowledge away a run that is
// still doing work; the caller must interrupt it first.
func (s *serveServer) handleRunAcknowledge(w http.ResponseWriter, r *http.Request, taskID string) {
	snap, err := s.loadSnapshotForRequest(r)
	if err != nil {
		serveJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	taskID = strings.TrimSpace(taskID)
	run, ok := serveFindRun(snap.runs, taskID)
	if !ok {
		serveJSON(w, http.StatusNotFound, serveActionResult{Refused: true, TaskID: taskID, Reason: "no run found for this task"})
		return
	}
	if refused, reason := serveAcknowledgeRefusal(run, s.now()); refused {
		serveJSON(w, http.StatusConflict, serveActionResult{Refused: true, TaskID: firstNonEmpty(run.ItemID, taskID), ProjectID: snap.projectID, Reason: reason})
		return
	}
	actor := defaultActorName()
	if _, err := retireRuntimeRun(s.store, DefaultStateRoot(), run, actor, serveAcknowledgeReason, s.now(), false); err != nil {
		// retireRuntimeRun re-checks the live-heartbeat guard; surface a late
		// refusal as a conflict rather than a 500 so the card can be restored.
		issue := errorToIssue(err)
		reason := issue.Message
		if issue.Hint != "" {
			reason += " Hint: " + issue.Hint
		}
		serveJSON(w, http.StatusConflict, serveActionResult{Refused: true, TaskID: firstNonEmpty(run.ItemID, taskID), ProjectID: snap.projectID, Reason: reason})
		return
	}
	s.refreshProjectSnapshot(snap.projectID)
	serveJSON(w, http.StatusOK, serveActionResult{
		OK:        true,
		TaskID:    firstNonEmpty(run.ItemID, taskID),
		ProjectID: snap.projectID,
		Reason:    "run acknowledged and retired",
	})
}

// serveAcknowledgeRefusal reports whether a run is too live to acknowledge. A
// running/claimed lease or a verified-live process must be interrupted first;
// only a settled row can be retired from under the operator.
func serveAcknowledgeRefusal(run RunStatus, now time.Time) (bool, string) {
	if runProcessGroupAlive(run) {
		return true, "run is still executing; interrupt it before acknowledging"
	}
	if !run.Terminal {
		switch LeaseState(strings.TrimSpace(run.LeaseState)) {
		case LeaseStateClaimed, LeaseStateRunning:
			return true, "run is still leased and active; interrupt it before acknowledging"
		}
	}
	if runHasFreshLiveHeartbeat(run, now) {
		return true, "run has a fresh heartbeat and a verified-live process; interrupt it before acknowledging"
	}
	return false, ""
}

// serveRunRetired reports whether a run has already been retired — via the CLI
// `tusker runs retire`, the serve acknowledge action, or the daemon's canonical
// terminal retirement, all of which route through retireRuntimeRun. That
// function is the single writer of the "retired by <actor>: <reason>" LastError
// on a terminal row, so the prefix is the reliable retirement marker. A retired
// run is cleared from the attention (needs) surface and the runs list.
func serveRunRetired(run RunStatus) bool {
	return run.Terminal && strings.HasPrefix(strings.TrimSpace(run.LastError), "retired by ")
}
