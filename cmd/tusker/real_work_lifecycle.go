package main

// Real-work lifecycle contract.
//
// Supported path (one contract, shared by CLI and UI):
//
//	prepare -> ready -> claim/start -> progress -> submit ->
//	independent review -> accepted completion (close)
//
// Canonical commands:
//   tusker work start <id> --by <agent>      claim ownership (runtime lease)
//   tusker work readiness <id>                same admission validators, read-only
//   tusker work progress <id>                 same run/task projection as status
//   tusker work wait <id> [--timeout 60]      bounded wait on the same run row
//   tusker work reconcile <id> --by <agent>   verify shared-checkout binding, read-only
//   tusker work submit <id> --by <agent> ...  bind material + move task to review
//   tusker work review <id> --by reviewer:... claim the independent review session
//   tusker review submit <id> --attempt ...   reviewer verdict (shared validators)
//   tusker close <id> --by reviewer|human ... accepted completion (requires review)
//   tusker work cancel <id> --by <owner>      release ownership (CAS, prevents late success)
//   tusker work retry <id> --by <agent>       truthful new attempt after terminal state
//
// Legacy aliases `attempt start`, `attempt handoff`, and `finish` participate
// in the same contract: `attempt start` on a workflow vault claims a runtime
// work session AND links a file attempt note bound to that session, so a
// following `finish`/`handoff` recognizes it. When a live runtime session
// exists but no file attempt note does, `handoff`/`finish` materialize the
// bound file attempt honestly (marked reconciled_from_runtime) instead of
// returning "No attempt exists".
//
// Shared-checkout route: run `work start` from the checkout that holds the
// implementation, then `work reconcile` to verify the binding, then
// `work submit`. An empty worktree claim is never treated as an import of
// unrelated implementation: submit fingerprints the bound workspace material
// and review refuses changed/stale material. No execution events or durations
// are invented; adoption is recorded only as linkage metadata.

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const realWorkLifecycleSchema = "tusker.work-lifecycle/v1"

// v7MissingAttemptError is the single "No attempt exists" contract shared by
// the file-attempt lookup and the runtime bridge, so every path teaches the
// same recognized replacement.
func v7MissingAttemptError(taskID string) error {
	return tuskerError(errorNotFound, "No attempt exists for "+taskID, withHint("attempts are runtime/session state; run `tusker work start "+taskID+" --by <agent>` (or `tusker attempt start "+taskID+"`, which claims the same session), then retry `tusker finish "+taskID+" --request-review`"))
}

// realWorkStage derives the operator-visible stage from the run row and task
// status. It is a projection, never a second state machine.
func realWorkStage(run *RunStatus, taskStatus string) string {
	taskStatus = strings.ToLower(strings.TrimSpace(taskStatus))
	if run == nil {
		return "unclaimed"
	}
	switch LeaseState(run.LeaseState) {
	case LeaseStateClaimed, LeaseStateRunning:
		if strings.ToLower(run.Lane) == runLaneReview {
			return "in_review"
		}
		return "in_progress"
	case LeaseStateReleased:
		switch projectedAttemptOutcome(run.AttemptOutcome, run.LastError) {
		case AttemptOutcomeSucceeded:
			if taskStatus == "done" {
				return "completed"
			}
			if taskStatus == "review" {
				return "submitted"
			}
			return "submitted"
		case AttemptOutcomeUnknown:
			return "outcome_unknown"
		case AttemptOutcomeFailed:
			return "failed"
		case AttemptOutcomeInterrupted:
			return "cancelled"
		case AttemptOutcomeAbandoned:
			return "abandoned"
		default:
			return "released"
		}
	default:
		return "unclaimed"
	}
}

type realWorkProgress struct {
	Schema     string             `json:"schema"`
	TaskID     string             `json:"task_id"`
	Stage      string             `json:"stage"`
	TaskStatus string             `json:"task_status,omitempty"`
	Run        *RunStatus         `json:"run,omitempty"`
	Blockers   []ReadinessBlocker `json:"blockers,omitempty"`
	Next       string             `json:"next"`
	Workspace  string             `json:"workspace,omitempty"`
	Profile    string             `json:"effective_profile,omitempty"`
	Head       string             `json:"head,omitempty"`
	Freshness  string             `json:"freshness,omitempty"`
}

// workRealLifecycleCmd dispatches the parity subcommands owned by this packet.
func workRealLifecycleCmd(args Args, action string) error {
	args["id"] = firstNonEmpty(args.String("id"), args.String("_pos0"))
	if err := scopeWorkSessionProject(args); err != nil {
		return err
	}
	switch action {
	case "readiness":
		return workReadinessCmd(args)
	case "progress":
		return workProgressCmd(args)
	case "wait":
		return workWaitCmd(args)
	case "cancel":
		return workCancelCmd(args)
	case "retry":
		return workRetryCmd(args)
	case "reconcile":
		return workReconcileCmd(args)
	case "profile":
		return workProfileCmd(args)
	default:
		return tuskerError(errorInvalidArg, "unknown work action: "+action)
	}
}

// workReadinessCmd runs the exact admission validators used by `work start`
// without claiming anything.
func workReadinessCmd(args Args) error {
	vault, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	id, err := requireArg(args, "id")
	if err != nil {
		return err
	}
	lane := firstNonEmpty(strings.TrimSpace(args.String("lane")), runLaneExecute)
	note, err := resolveV7Note(vault, id, "task")
	if err != nil {
		return err
	}
	idx, err := loadV7Index(vault)
	if err != nil {
		return err
	}
	notesByID, notesByRecordID := v7NoteMaps(idx)
	blockers := workSessionAdmissionBlockersForLane(note, idx, notesByID, notesByRecordID, lane)
	out := map[string]any{
		"ok": len(blockers) == 0, "schema": realWorkLifecycleSchema,
		"task_id": id, "lane": lane, "blockers": blockers,
		"next": firstNonEmpty(workSessionNextHintForReadiness(blockers, id), "tusker work start "+id+" --by <agent>"),
	}
	if args.Bool("json") {
		emitJSON(out)
	} else {
		emitJSON(out)
	}
	if len(blockers) > 0 {
		return workSessionStartBlocker(blockers[0])
	}
	return nil
}

func workSessionNextHintForReadiness(blockers []ReadinessBlocker, id string) string {
	if len(blockers) == 0 {
		return "tusker work start " + id + " --by <agent>"
	}
	return ""
}

func v7NoteMaps(idx v7Index) (map[string]Note, map[string]Note) {
	byID := map[string]Note{}
	byRecord := map[string]Note{}
	for _, task := range idx.Tasks {
		byID[stringField(task.Data, "id")] = task
		byRecord[trackerRecordID(task)] = task
	}
	return byID, byRecord
}

// workProgressCmd reports current progress from the same run row and task
// state the UI Serve actions project.
func workProgressCmd(args Args) error {
	vault, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	id, err := requireArg(args, "id")
	if err != nil {
		return err
	}
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		return err
	}
	defer store.Close()
	run, err := findRunScopedOrAmbiguous(store, args.String("project"), id)
	if err != nil {
		return err
	}
	taskStatus := ""
	if note, noteErr := resolveV7Note(vault, id, "task"); noteErr == nil {
		taskStatus = stringField(note.Data, "status")
	}
	stage := realWorkStage(run, taskStatus)
	next := ""
	var blockers []ReadinessBlocker
	if run == nil {
		if note, noteErr := resolveV7Note(vault, id, "task"); noteErr == nil {
			idx, idxErr := loadV7Index(vault)
			if idxErr == nil {
				byID, byRecord := v7NoteMaps(idx)
				blockers = workSessionAdmissionBlockersForLane(note, idx, byID, byRecord, runLaneExecute)
			}
		}
		next = "tusker work start " + id + " --by <agent>"
	} else {
		next = workSessionNext(*run)
	}
	profile, head, freshness := "", "", ""
	if run != nil {
		profile = firstNonEmpty(run.RunnerProfile, run.Runner)
		freshness = string(runFreshness(run, time.Now().UTC()))
		if identity, identityErr := store.RunIdentity(run.ProjectID, run.RecordID); identityErr == nil && identity != nil {
			head = identity.Head
		}
	}
	emitJSON(realWorkProgress{
		Schema: realWorkLifecycleSchema, TaskID: id, Stage: stage,
		TaskStatus: taskStatus, Run: run, Blockers: blockers, Next: next,
		Workspace: firstNonEmpty(runWorkspace(run)), Profile: profile, Head: head, Freshness: freshness,
	})
	return nil
}

func runWorkspace(run *RunStatus) string {
	if run == nil {
		return ""
	}
	return run.WorkspacePath
}

// workWaitCmd bounds waiting on the same run row. It never invents terminal
// state: a timeout is a timeout, reported nonzero.
func workWaitCmd(args Args) error {
	id, err := requireArg(args, "id")
	if err != nil {
		return err
	}
	timeoutSecs := intArg(args, "timeout")
	if timeoutSecs <= 0 {
		timeoutSecs = 60
	}
	if timeoutSecs > 600 {
		timeoutSecs = 600
	}
	interval := time.Second
	deadline := time.Now().Add(time.Duration(timeoutSecs) * time.Second)
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		return err
	}
	defer store.Close()
	for {
		run, findErr := findRunScopedOrAmbiguous(store, args.String("project"), id)
		if findErr != nil {
			return findErr
		}
		if run != nil && LeaseState(run.LeaseState) == LeaseStateReleased {
			emitJSON(map[string]any{
				"ok": true, "schema": realWorkLifecycleSchema, "task_id": id,
				"stage": realWorkStage(run, ""), "run": run,
				"next": "tusker work progress " + id,
			})
			return nil
		}
		if !time.Now().Before(deadline) {
			emitJSON(map[string]any{
				"ok": false, "schema": realWorkLifecycleSchema, "task_id": id,
				"error": "wait timeout: run did not release within bound",
				"next":  "tusker work progress " + id,
			})
			return tuskerError("WAIT_TIMEOUT", "work wait timed out for "+id+" after "+fmt.Sprintf("%d", timeoutSecs)+"s", withHint("inspect with `tusker work progress "+id+"`; rerun wait with a larger --timeout"))
		}
		time.Sleep(interval)
	}
}

// workCancelCmd releases execution ownership through the same CAS path as
// `work release`. A released run can never record late success: finish paths
// require a live owned lease at the same generation.
func workCancelCmd(args Args) error {
	if strings.TrimSpace(firstNonEmpty(args.String("reason"), "")) == "" {
		return tuskerError(errorMissingArg, "work cancel requires --reason")
	}
	if args.String("owner") == "" {
		args["owner"] = firstNonEmpty(args.String("by"), args.String("actor"))
	}
	if strings.TrimSpace(args.String("owner")) == "" {
		return tuskerError(errorMissingArg, "work cancel requires --by")
	}
	if err := requireWorkSessionRevision(args); err != nil {
		return err
	}
	if err := runsLifecycleCmd(args, "release"); err != nil {
		return err
	}
	workSessionNotifyRun(args.String("project"), args.String("id"))
	return nil
}

// workRetryCmd starts a truthful new attempt after terminal state. Prior
// attempts stay in history; nothing is rewritten.
func workRetryCmd(args Args) error {
	id, err := requireArg(args, "id")
	if err != nil {
		return err
	}
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		return err
	}
	run, err := findRunScopedOrAmbiguous(store, args.String("project"), id)
	_ = store.Close()
	if err != nil {
		return err
	}
	if run != nil && (LeaseState(run.LeaseState) == LeaseStateClaimed || LeaseState(run.LeaseState) == LeaseStateRunning) {
		return tuskerError(errorInvalidTransition, "work retry refused: session still live for "+id, withHint("cancel first with `tusker work cancel "+id+" --by "+run.LeaseOwner+" --reason <text>`"))
	}
	if args.String("owner") == "" {
		args["owner"] = firstNonEmpty(args.String("by"), args.String("actor"))
	}
	if args.String("source") == "" {
		args["source"] = "tusker_cli"
	}
	return workSessionStartCmd(args)
}

// workReconcileCmd verifies the shared-checkout binding before review. It is
// read-only: it refuses wrong-workspace and stale bindings instead of moving
// material.
func workReconcileCmd(args Args) error {
	vault, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	id, err := requireArg(args, "id")
	if err != nil {
		return err
	}
	owner := firstNonEmpty(args.String("by"), args.String("actor"), args.String("owner"))
	note, err := resolveV7Note(vault, id, "task")
	if err != nil {
		return err
	}
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		return err
	}
	defer store.Close()
	run, err := findRunScopedOrAmbiguous(store, args.String("project"), id)
	if err != nil {
		return err
	}
	if run == nil {
		emitJSON(reconcilePayload(id, "", "", "", nil, "", "run `tusker work start "+id+" --by <agent>` from the checkout holding the implementation"))
		return tuskerError(errorNotFound, "work reconcile found no live work session for "+id, withHint("run `tusker work start "+id+" --by <agent>` from the checkout holding the implementation"))
	}
	if owner != "" && run.LeaseOwner != "" && owner != run.LeaseOwner &&
		LeaseState(run.LeaseState) != LeaseStateReleased {
		emitJSON(reconcilePayload(id, run.WorkspacePath, "", "", nil, "", ""))
		return tuskerError("WORKSPACE_OWNER_MISMATCH", "work reconcile refused: session owned by "+run.LeaseOwner, withHint("reconcile as --by "+run.LeaseOwner+" or wait for release"))
	}
	workRevision := intField(note.Data, "work_revision")
	if workRevision != 0 && run.WorkRevision != workRevision {
		emitJSON(reconcilePayload(id, run.WorkspacePath, "", "", nil, "", "reload with `tusker work progress "+id+"`"))
		return tuskerError("WORK_SESSION_STALE", "work reconcile refused: session revision is stale (task revision "+fmt.Sprintf("%d", workRevision)+", run revision "+fmt.Sprintf("%d", run.WorkRevision)+")", withHint("reload with `tusker work progress "+id+"`; submit from the bound workspace only"))
	}
	checkout := v7RepoRoot(vault)
	scope, scopeErr := canonicalTaskMaterialScope(vault, note)
	if scopeErr != nil {
		return scopeErr
	}
	if len(scope) == 0 {
		return tuskerError(errorInvalidTransition, "work reconcile requires declared owned paths, generated outputs, or spec references before submission")
	}
	generatedOutputScope, generatedErr := taskGeneratedOutputScope(note)
	if generatedErr != nil {
		return generatedErr
	}
	boundMaterial, err := workspaceTreeStateHashForPaths(run.WorkspacePath, scope, generatedOutputScope)
	if err != nil {
		emitJSON(reconcilePayload(id, run.WorkspacePath, checkout, "", scope, "", "submit from the bound workspace or release and restart where the implementation lives"))
		return tuskerError("WORKSPACE_MISMATCH", "work reconcile cannot read bound workspace material: "+err.Error(), withHint("submit from the bound workspace or release and restart where the implementation lives"))
	}
	if !workspacePathsCompatible(checkout, run.WorkspacePath) {
		currentMaterial, hashErr := workspaceTreeStateHashForPaths(checkout, scope, generatedOutputScope)
		if hashErr != nil || currentMaterial != boundMaterial {
			emitJSON(reconcilePayload(id, run.WorkspacePath, checkout, boundMaterial, scope, "", "release the stray claim and start where the implementation lives"))
			return tuskerError("WORKSPACE_MISMATCH", "work reconcile refused: current checkout does not match the bound implementation workspace", withHint("run `tusker work submit "+id+"` from the bound workspace, or `tusker work cancel "+id+" --by "+run.LeaseOwner+" --reason <text>` then `tusker work start "+id+"` where the implementation lives"))
		}
	}
	head := ""
	if identity, identityErr := store.RunIdentity(run.ProjectID, run.RecordID); identityErr == nil && identity != nil {
		head = identity.Head
	}
	emitJSON(reconcilePayload(id, run.WorkspacePath, checkout, boundMaterial, scope, head, "tusker work submit "+id+" --by "+run.LeaseOwner+" --deliverable <summary> --verification <summary> --gate-verdicts <A1=pass>"))
	return nil
}

func reconcilePayload(taskID, workspace, checkout, material string, scope []string, head, next string) map[string]any {
	return map[string]any{
		"ok": workspace != "" && material != "", "schema": realWorkLifecycleSchema,
		"task_id": taskID, "workspace": workspace, "current_checkout": checkout,
		"material_fingerprint": material, "material_scope": scope, "head": head, "next": next,
	}
}

// workProfileCmd reports the effective lane profile from the same workflow
// source the UI reads, plus whatever the run actually recorded. It never
// invents a fallback: unavailable profiles are reported as precondition gaps.
func workProfileCmd(args Args) error {
	vault, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	id, err := requireArg(args, "id")
	if err != nil {
		return err
	}
	lane := firstNonEmpty(strings.TrimSpace(args.String("lane")), runLaneExecute)
	if lane != runLaneExecute && lane != runLaneReview {
		return tuskerError(errorInvalidArg, "work profile lane must be execute or review")
	}
	wf, err := loadWorkflow(vault)
	if err != nil {
		return err
	}
	profileName := strings.TrimSpace(wf.Data.RunnerLaneProfiles[lane])
	definition, defined := wf.Data.RunnerProfiles[profileName]
	source := strings.TrimSpace(wf.Data.RunnerProfileSources[profileName])
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		return err
	}
	defer store.Close()
	run, _ := findRunScopedOrAmbiguous(store, args.String("project"), id)
	recorded := map[string]any{}
	if run != nil {
		recorded = map[string]any{
			"runner": run.Runner, "runner_profile": run.RunnerProfile,
			"runner_harness": run.RunnerHarness, "runner_model": run.RunnerModel,
			"runner_effort": run.RunnerEffort, "fallback_reason": run.RunnerFallbackReason,
		}
	}
	out := map[string]any{
		"ok": true, "schema": realWorkLifecycleSchema, "task_id": id, "lane": lane,
		"configured_profile": profileName, "profile_source": source,
		"profile_defined": defined,
		"recorded":        recorded,
		"next":            "tusker work start " + id + " --by <agent>",
	}
	if defined {
		out["harness"] = string(RunnerName(definition.Harness))
		out["model"] = definition.Model
		out["effort"] = definition.Effort
	} else {
		out["ok"] = false
		out["error"] = "no explicit lane profile configured for " + lane
		out["next"] = "configure an explicit project or machine-local profile for lane " + lane
	}
	emitJSON(out)
	if !defined {
		return tuskerError(errorConfigInvalid, "work profile: no explicit lane profile configured for "+lane, withHint("configure an explicit project or machine-local profile for lane "+lane))
	}
	return nil
}

// ensureFileAttemptForWorkSession links the runtime work session to a file
// attempt note so legacy `finish`/`handoff` commands recognize the session.
// It is idempotent: an existing note bound to the active runtime attempt is
// reused.
func ensureFileAttemptForWorkSession(vault, taskID string) (string, error) {
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		return "", err
	}
	defer store.Close()
	note, err := resolveV7Note(vault, taskID, "task")
	if err != nil {
		return "", err
	}
	run, err := findRunForVault(store, vault, trackerRecordID(note))
	if err != nil {
		return "", err
	}
	if run == nil || strings.TrimSpace(run.ActiveAttemptID) == "" {
		return "", tuskerError(errorNotFound, "no live work session for "+taskID)
	}
	idx, err := loadV7Index(vault)
	if err != nil {
		return "", err
	}
	for _, attempt := range idx.Attempts[taskID] {
		if strings.EqualFold(strings.TrimSpace(stringField(attempt.Data, "runtime_attempt_id")), run.ActiveAttemptID) {
			return stringField(attempt.Data, "id"), nil
		}
	}
	attemptID := fmt.Sprintf("%s-A-%s", taskID, padNumber(nextV7AttemptSequence(vault, taskID)))
	branch := currentGitBranch()
	now := time.Now().UTC().Format(time.RFC3339)
	data := map[string]any{
		"schema": "tusker.attempt/v1", "kind": "attempt", "id": attemptID,
		"runtime_attempt_id": run.ActiveAttemptID, "lane": run.Lane,
		"project": v7ProjectID(vault), "task": taskID,
		"task_state_rev": stringField(note.Data, "state_rev"),
		"runner":         firstNonEmpty(run.Runner, "codex"),
		"agent_model":    run.RunnerModel,
		"workspace_kind": "work_session",
		"workspace_path": run.WorkspacePath,
		"branch":         firstNonEmpty(run.WorkspacePath, branch),
		"status":         "started", "started_at": now,
		"reconciled_from_runtime": true,
		"evidence":                []string{},
	}
	body := fmt.Sprintf("# %s · Agent attempt summary\n\n## Outcome\n\nStarted via work session claim %s.\n\n## Changed areas\n\nPending.\n\n## Verification\n\nPending.\n\n## Handoff\n\nPending.\n\n## Follow-ups proposed\n\nNone.\n", attemptID, run.ActiveAttemptID)
	data["state_rev"] = v7StateRev(data, body)
	path := filepath.Join(vault, "attempts", taskID, attemptID+".md")
	content, err := serializeDocument(data, body, v7FrontmatterOrder["attempt"])
	if err != nil {
		return "", err
	}
	if err := writeText(path, content); err != nil {
		return "", err
	}
	_ = emitV7Event(vault, taskID, "task", "attempt_started", firstNonEmpty(run.LeaseOwner, "agent:"+defaultActorName()), map[string]any{"attempt": attemptID, "runtime_attempt_id": run.ActiveAttemptID})
	return attemptID, nil
}

// ensureFileAttemptForHandoff materializes the bound file attempt when a live
// runtime session exists but no file attempt note does. When file attempts
// exist, it enforces the workspace binding: an unrelated worktree note is
// refused instead of being treated as an import.
func ensureFileAttemptForHandoff(vault, taskID, actor string) (string, error) {
	idx, err := loadV7Index(vault)
	if err != nil {
		return "", err
	}
	if len(idx.Attempts[taskID]) > 0 {
		return refuseUnrelatedFileAttempt(vault, taskID, idx)
	}
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		return "", err
	}
	defer store.Close()
	note, err := resolveV7Note(vault, taskID, "task")
	if err != nil {
		return "", err
	}
	run, err := findRunForVault(store, vault, trackerRecordID(note))
	if err != nil {
		return "", err
	}
	if run == nil || strings.TrimSpace(run.ActiveAttemptID) == "" {
		return "", v7MissingAttemptError(taskID)
	}
	if actor != "" && strings.HasPrefix(actor, "agent:") && run.LeaseOwner != "" && run.LeaseOwner != actor &&
		LeaseState(run.LeaseState) != LeaseStateReleased {
		return "", tuskerError("WORK_SESSION_REQUIRED", "agent mutation requires a live work session owned by "+actor, withHint("run `tusker work start "+taskID+" --by "+actor+"` or use explicit human --break-glass"))
	}
	attemptID, err := ensureFileAttemptForWorkSession(vault, taskID)
	if err != nil {
		return "", err
	}
	fmt.Fprintf(os.Stderr, "linked live work session to file attempt %s\n", attemptID)
	return attemptID, nil
}

// refuseUnrelatedFileAttempt keeps a new empty worktree note from silently
// importing old implementation: when a live runtime session exists, the
// handoff note must link to it (runtime_attempt_id) or share its workspace.
func refuseUnrelatedFileAttempt(vault, taskID string, idx v7Index) (string, error) {
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		return "", err
	}
	defer store.Close()
	note, err := resolveV7Note(vault, taskID, "task")
	if err != nil {
		return "", err
	}
	run, err := findRunForVault(store, vault, trackerRecordID(note))
	if err != nil {
		return "", err
	}
	attempts := append([]Note(nil), idx.Attempts[taskID]...)
	sort.Slice(attempts, func(i, j int) bool { return stringField(attempts[i].Data, "id") < stringField(attempts[j].Data, "id") })
	latest := stringField(attempts[len(attempts)-1].Data, "id")
	if run == nil || strings.TrimSpace(run.ActiveAttemptID) == "" {
		return latest, nil
	}
	latestNote := attempts[len(attempts)-1]
	if strings.EqualFold(strings.TrimSpace(stringField(latestNote.Data, "runtime_attempt_id")), run.ActiveAttemptID) {
		return latest, nil
	}
	if workspacePathsCompatible(strings.TrimSpace(stringField(latestNote.Data, "workspace_path")), run.WorkspacePath) {
		return latest, nil
	}
	if strings.TrimSpace(stringField(latestNote.Data, "workspace_path")) == "" && strings.TrimSpace(stringField(latestNote.Data, "runtime_attempt_id")) == "" {
		// Legacy pre-runtime notes carry neither binding. They are usable only
		// when the operator explicitly reconciles them through the supported
		// path; refusing here would strand old history, so allow with the
		// workspace check deferred to submit/review material verification.
		return latest, nil
	}
	return "", tuskerError("WORKSPACE_MISMATCH", "handoff refused: file attempt "+latest+" is not bound to the live work session workspace", withHint("submit from the bound workspace with `tusker work submit "+taskID+"`, or cancel the stray session and reconcile with `tusker work reconcile "+taskID+" --by "+run.LeaseOwner+"`"))
}
