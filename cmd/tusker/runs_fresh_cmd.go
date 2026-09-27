package main

import (
	"fmt"
	"strings"
)

// runsFreshCmd is the CLI entry for the Serve "Start fresh" run control. It
// resolves the run like `runs continue`/`runs interrupt` and then calls the
// same transition the Serve handler uses, so the queued intent, run row,
// session close, and supervisor decision are identical on both surfaces.
func runsFreshCmd(args Args) error {
	id, err := requireArg(args, "id")
	if err != nil {
		return err
	}
	actor := strings.TrimSpace(args.String("by"))
	if actor == "" {
		return tuskerError(errorMissingArg, "runs fresh requires --by <actor>")
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
	prior, err := loadRunSessionControlIntent(store, run.ProjectID, run.RecordID)
	if err != nil {
		return tuskerError(errorInvalidTransition, "run control intent is unreadable: "+err.Error())
	}
	result := runSessionControlResult{
		TaskID: firstNonEmpty(run.ItemID, id), ProjectID: run.ProjectID,
		Action: runSessionControlFresh, State: "refused",
		LeaseState: serveLeaseState(run.LeaseState), LeaseStateRaw: run.LeaseState,
		ProcessRunning: runProcessGroupAlive(*run),
	}
	result, _ = newServeServer("", "", "", store, nil).applyRunFreshControl(*run, actor, strings.TrimSpace(args.String("reason")), prior, result)
	if args.Bool("json") {
		emitJSON(result)
	}
	if !result.OK {
		return tuskerError(errorInvalidTransition, firstNonEmpty(result.Reason, "start_fresh was refused"))
	}
	if !args.Bool("json") {
		fmt.Printf("Fresh %s: %s\n", firstNonEmpty(run.ItemID, id), result.Reason)
	}
	return nil
}
