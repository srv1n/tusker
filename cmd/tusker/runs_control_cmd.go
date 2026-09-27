package main

import (
	"fmt"
	"strings"
)

// runsStopCmd, runsPauseCmd, and runsFreshCmd are the CLI entries for the
// Serve run controls. Each resolves the run like `runs continue`/`runs
// interrupt` and then calls the same transition the Serve handler uses, so
// the durable intent, run row, session close, and supervisor decision are
// identical on both surfaces.
func runsStopCmd(args Args) error {
	return runSessionControlCmd(args, runSessionControlStop, "stop")
}

func runsPauseCmd(args Args) error {
	return runSessionControlCmd(args, runSessionControlPause, "pause")
}

func runsFreshCmd(args Args) error {
	return runSessionControlCmd(args, runSessionControlFresh, "fresh")
}

func runSessionControlCmd(args Args, action, verb string) error {
	id, err := requireArg(args, "id")
	if err != nil {
		return err
	}
	actor := strings.TrimSpace(args.String("by"))
	if actor == "" {
		return tuskerError(errorMissingArg, "runs "+verb+" requires --by <actor>")
	}
	projectID := strings.TrimSpace(args.String("project"))
	vault := strings.TrimSpace(args.String("vault"))
	if projectID != "" && vault != "" {
		return tuskerError(errorInvalidArg, "use either --project or --vault")
	}
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		return err
	}
	defer store.Close()
	var run *RunStatus
	if vault != "" {
		run, err = findRunForVault(store, vault, id)
	} else {
		run, err = findRunScopedOrAmbiguous(store, projectID, id)
	}
	if err != nil {
		return err
	}
	if run == nil {
		return tuskerError(errorNotFound, "run not found: "+id)
	}
	server := newServeServer("", "", "", store, nil)
	result := runSessionControlResult{
		TaskID: firstNonEmpty(run.ItemID, id), ProjectID: run.ProjectID,
		Action: action, State: "refused",
		LeaseState: serveLeaseState(run.LeaseState), LeaseStateRaw: run.LeaseState,
		ProcessRunning: runProcessGroupAlive(*run),
	}
	if action == runSessionControlPause {
		result, _ = server.applyRunPauseControl(*run, result)
	} else {
		prior, intentErr := loadRunSessionControlIntent(store, run.ProjectID, run.RecordID)
		if intentErr != nil {
			return tuskerError(errorInvalidTransition, "run control intent is unreadable: "+intentErr.Error())
		}
		if action == runSessionControlStop {
			result, _ = server.applyRunStopControl(*run, actor, strings.TrimSpace(args.String("reason")), prior, result)
		} else {
			result, _ = server.applyRunFreshControl(*run, actor, strings.TrimSpace(args.String("reason")), prior, result)
		}
	}
	if args.Bool("json") {
		emitJSON(result)
	}
	if result.Refused || (!result.OK && !result.Pending) {
		return tuskerError(errorInvalidTransition, firstNonEmpty(result.Reason, action+" was refused"))
	}
	if !args.Bool("json") {
		fmt.Printf("%s %s: %s\n", runSessionControlVerbLabel(action), firstNonEmpty(run.ItemID, id), result.Reason)
	}
	return nil
}

func runSessionControlVerbLabel(action string) string {
	switch action {
	case runSessionControlStop:
		return "Stop"
	case runSessionControlPause:
		return "Pause"
	default:
		return "Fresh"
	}
}
