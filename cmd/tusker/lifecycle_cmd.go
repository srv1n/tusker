package main

import (
	"fmt"
	"os"
	"path"
	"sort"
	"strings"
	"time"
)

// The task lifecycle is four verbs: start, done, review and stop. Each one
// routes on the ID and on the caller's state to the existing transition, so
// an agent needs one command per intent rather than one per state machine.

// doneCmd is the only way to say work is finished. A caller holding the
// task's live work session submits it. Anything else is recorded through
// adopt_completed: Tusker reruns the task's command proof in the checkout and
// moves it to review (an agent) or done (a person or an authorized agent). A
// person running done on a task already in review closes it.
func doneCmd(args Args) error {
	// --action selects a typed recovery (rerun_checks, adopt_completed) for
	// one task, as documented in the work-session help.
	if strings.TrimSpace(args.String("action")) != "" {
		args["id"] = firstNonEmpty(args.String("id"), args.String("_pos0"))
		return workRecoverCmd(args)
	}
	vault, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	actor, err := requireArg(args, "by")
	if err != nil {
		return err
	}
	ids, err := doneCandidates(vault, args)
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		return tuskerError(errorMissingArg, "tusker done needs task IDs, --wave <W-ID>, or --since <git-ref>")
	}
	if len(ids) == 1 && args.String("wave") == "" && args.String("since") == "" {
		if handled, err := doneSingle(vault, ids[0], actor, args); handled {
			return err
		}
	}
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		return err
	}
	defer store.Close()
	projectID, registered, err := registeredProjectIDForVault(store, vault)
	if err != nil {
		return err
	}
	if !registered {
		return tuskerError(errorNotFound, "tusker done requires a registered project; run `tusker projects add --repo .`")
	}
	wf, err := loadWorkflow(vault)
	if err != nil {
		return err
	}
	// Adopt in rounds so a dependency lands before its dependents, whatever
	// order the IDs arrive in. A round that adopts nothing ends the loop.
	results := map[string]serveRecoveryResult{}
	pending := ids
	for len(pending) > 0 {
		var retry []string
		for _, id := range pending {
			result, err := adoptCompletedWork(vault, store, projectID, id, actor, wf.Data.Retry.MaxAttempts, time.Now().UTC())
			if err != nil {
				result = serveRecoveryResult{Action: "adopt_completed", TaskID: id, Refused: true, Reason: err.Error()}
			}
			if result.Refused {
				result.Reason = doneRefusalRemedy(vault, id, actor, result.Reason)
			}
			results[id] = result
			if !result.OK && strings.Contains(result.Reason, "unfinished dependency") {
				retry = append(retry, id)
			}
		}
		if len(retry) == len(pending) {
			break
		}
		pending = retry
	}
	ok := true
	out := make([]serveRecoveryResult, 0, len(ids))
	for _, id := range ids {
		out = append(out, results[id])
		ok = ok && results[id].OK
	}
	_ = sendDaemonControlOneWay(DefaultStateRoot(), daemonControlRequest{Command: "reconcile_project", ProjectID: projectID, Cause: "done"}, 250*time.Millisecond)
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": ok, "results": out})
	} else {
		for _, r := range out {
			outcome := "refused"
			if r.OK && r.Lane == runLaneReview {
				outcome = "review"
			} else if r.OK {
				outcome = "done"
			}
			fmt.Printf("%-12s %-8s %s\n", r.TaskID, outcome, r.Reason)
		}
	}
	if !ok {
		return afterResultEmitted(tuskerError(errorEvidenceGate, "some tasks were not recorded; fix the named reason and rerun tusker done"))
	}
	return nil
}

// doneCandidates unions explicit IDs, wave members, and open tasks whose
// owned paths changed since a git ref. The ref form needs nothing from the
// agent that did the work: no trailer, claim or harness integration.
func doneCandidates(vault string, args Args) ([]string, error) {
	var ids []string
	for _, id := range strings.Fields(args.String("_pos")) {
		ids = append(ids, strings.TrimSpace(id))
	}
	idx, err := loadV7Index(vault)
	if err != nil {
		return nil, err
	}
	if waveID := strings.TrimSpace(args.String("wave")); waveID != "" {
		wave, ok := idx.Waves[waveID]
		if !ok {
			return nil, tuskerError(errorNotFound, "wave not found: "+waveID)
		}
		ids = append(ids, normalizeList(wave.Data["members"])...)
	}
	if since := strings.TrimSpace(args.String("since")); since != "" {
		changed, err := gitOutputTrim(v7RepoRoot(vault), "diff", "--name-only", since+"..HEAD")
		if err != nil {
			return nil, tuskerError(errorInvalidArg, "cannot list files changed since "+since+": "+err.Error())
		}
		files := strings.Fields(changed)
		var touched []string
		for id, task := range idx.Tasks {
			switch strings.ToLower(stringField(task.Data, "status")) {
			case "done", "cancelled", "superseded":
				continue
			}
			if adoptOwnedPathsTouched(normalizeList(task.Data["owned_paths"]), files) {
				touched = append(touched, id)
			}
		}
		sort.Strings(touched)
		ids = append(ids, touched...)
	}
	return uniqueStringsPreserveOrder(ids), nil
}

func adoptOwnedPathsTouched(owned, files []string) bool {
	for _, pattern := range owned {
		pattern = strings.TrimSuffix(strings.TrimPrefix(pattern, "./"), "/")
		for _, file := range files {
			if matched, _ := path.Match(pattern, file); matched || file == pattern || strings.HasPrefix(file, pattern+"/") {
				return true
			}
		}
	}
	return false
}

// doneSingle handles the two single-task cases that are not adoption: the
// caller's own live session is submitted, and a person closes a task that is
// already in review. It reports handled=false to fall through to adoption.
func doneSingle(vault, id, actor string, args Args) (bool, error) {
	args["id"] = id
	note, err := resolveV7Note(vault, id, "task")
	if err != nil {
		return true, err
	}
	inReview := strings.EqualFold(stringField(note.Data, "status"), "review") && !taskAdoptedExternalImplementation(note)
	// A non-agent closing a task in review closes it, even when that actor
	// still holds the review session it just used to record its verdict.
	if inReview && !strings.HasPrefix(actor, "agent:") {
		return true, closeV7Cmd(args)
	}
	if held, err := heldWorkSession(args, id, actor); err != nil {
		return true, err
	} else if held {
		if args.String("deliverable") == "" {
			args["deliverable"] = args.String("summary")
		}
		return true, workSessionCmd(args, "submit")
	}
	if !inReview {
		return false, nil
	}
	return true, tuskerError(errorInvalidTransition, id+" is in review and waits for acceptance", withHint("a reviewer runs `tusker review "+id+"`; a person runs `tusker done "+id+" --by human:<name>`"))
}

// heldWorkSession reports whether actor holds the task's live work session.
// A dispatched worker always holds its own.
func heldWorkSession(args Args, id, actor string) (bool, error) {
	if strings.TrimSpace(os.Getenv("TUSKER_ATTEMPT_ID")) != "" {
		return true, nil
	}
	if err := scopeWorkSessionProject(args); err != nil {
		return false, err
	}
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		return false, err
	}
	defer store.Close()
	run, err := findRunForCLI(store, args, id)
	if err != nil || run == nil {
		return false, nil
	}
	_, live := holderLiveness(*run, time.Now().UTC())
	return live && run.LeaseOwner == actor, nil
}

// startCmd begins work on a task, wave or batch. A task starts in your own
// session here (renewing it when you already hold it); --background queues it
// for the daemon. A wave is authorized and queued, or resumed when paused. A
// batch runs in the foreground.
func startCmd(args Args) error {
	id := strings.TrimSpace(firstNonEmpty(args.String("id"), args.String("_pos0")))
	if v7TaskIDPattern.MatchString(strings.ToUpper(id)) || strings.HasPrefix(strings.ToUpper(id), "W-") {
		id = strings.ToUpper(id)
	}
	if id == "" {
		return tuskerError(errorMissingArg, "Usage: tusker start <TASK-ID|W-ID|B-ID> --by <actor> [--background]")
	}
	args["id"] = id
	switch {
	case strings.HasPrefix(id, "B-"):
		return coordinatorBatchCmd(args, "run")
	case strings.HasPrefix(id, "W-"):
		vault, err := resolveVaultPath(args, false)
		if err != nil {
			return err
		}
		note, err := resolveV7Note(vault, id, "wave")
		if err != nil {
			return err
		}
		if stringField(note.Data, "authorization") == "paused" {
			return waveV7ResumeCmd(args)
		}
		return waveStartCmd(args)
	case !isTaskID(args, id):
		return coordinatorBatchCmd(args, "run")
	case args.Bool("background"):
		return directRunCmd(args)
	}
	actor := firstNonEmpty(args.String("by"), args.String("actor"))
	if held, err := heldWorkSession(args, id, actor); err != nil {
		return err
	} else if held {
		return workSessionCmd(args, "heartbeat")
	}
	return workRetryCmd(args)
}

// reviewCmd is the reviewer's verb. With no verdict it claims the review
// session. A run verdict (pass, changes_requested, blocked) is recorded
// against the exact revision; a batch verdict (accept, accept-with-followups,
// reject) closes the batch-bound task.
func reviewCmd(args Args) error {
	args["id"] = firstNonEmpty(args.String("id"), args.String("_pos0"))
	switch args.String("verdict") {
	case "":
		return workSessionCmd(args, "review")
	case "accept", "accept-with-followups", "reject":
		return coordinatorCloseCmd(args)
	default:
		return reviewSubmitCmd(args)
	}
}

// stopCmd ends work without finishing it. On a task it releases your session,
// or records it as failed (--failed), or cancels the task (--cancel), or stops
// a daemon run you do not own. On a wave it pauses admission; on a batch it
// stops the workers (--drain lets them finish).
func stopCmd(args Args) error {
	if args.Bool("all") {
		return stopAllCmd(args)
	}
	id := strings.TrimSpace(firstNonEmpty(args.String("id"), args.String("_pos0")))
	if v7TaskIDPattern.MatchString(strings.ToUpper(id)) || strings.HasPrefix(strings.ToUpper(id), "W-") {
		id = strings.ToUpper(id)
	}
	if id == "" {
		return tuskerError(errorMissingArg, "Usage: tusker stop <TASK-ID|W-ID|B-ID> --by <actor> --reason <text> [--failed|--cancel]")
	}
	args["id"] = id
	switch {
	case strings.HasPrefix(id, "B-"):
		return coordinatorBatchCmd(args, "stop")
	case strings.HasPrefix(id, "W-"):
		return waveV7PauseCmd(args)
	case !isTaskID(args, id):
		return coordinatorBatchCmd(args, "stop")
	case args.Bool("cancel"):
		return discardV7Cmd(args)
	}
	actor := firstNonEmpty(args.String("by"), args.String("actor"))
	held, err := heldWorkSession(args, id, actor)
	if err != nil {
		return err
	}
	if !held {
		return runsStopCmd(args)
	}
	if args.Bool("failed") {
		return workSessionCmd(args, "fail")
	}
	return workSessionCmd(args, "release")
}

// isTaskID reports whether id names a task in the vault. Anything else that
// is not a wave is treated as a coordinator batch, whose names are free-form.
func isTaskID(args Args, id string) bool {
	if v7TaskIDPattern.MatchString(id) {
		return true
	}
	vault, err := resolveVaultPath(args, false)
	if err != nil {
		return false
	}
	_, err = resolveV7Note(vault, id, "task")
	return err == nil
}
