package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

var departureControlNotify = func(stateRoot, projectID, cause string) {
	request := daemonControlRequest{Command: "reconcile_project", ProjectID: projectID, Cause: cause}
	if strings.TrimSpace(projectID) == "" {
		request.Command = "reconcile_registry"
	}
	_ = sendDaemonControlOneWay(stateRoot, request, 250*time.Millisecond)
}

func departureCheckCmd(args Args) error {
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	projectID := strings.TrimSpace(args.String("project"))
	if projectID == "" {
		return tuskerError(errorMissingArg, "--project is required")
	}
	wf, err := loadWorkflow(vaultPath)
	if err != nil {
		return err
	}
	decision, err := defaultDeparturePlanner().PlanDeparture(vaultPath, projectID, wf)
	if err != nil {
		return err
	}
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": true, "decision": decision})
		return nil
	}
	fmt.Printf("Departure check: %s\n", decision.Disposition)
	for _, reason := range decision.Reasons {
		fmt.Printf("%s\n", reason.Message)
	}
	return nil
}

func departureStatusCmd(args Args) error {
	projectID := strings.TrimSpace(args.String("project"))
	if projectID == "" {
		return tuskerError(errorMissingArg, "--project is required")
	}
	store, err := OpenRuntimeStore(firstNonEmpty(strings.TrimSpace(args.String("state-root")), DefaultStateRoot()))
	if err != nil {
		return err
	}
	defer store.Close()
	runs, err := store.ListDepartureRuns(projectID)
	if err != nil {
		return err
	}
	var current *DepartureRun
	if len(runs) > 0 {
		current = &runs[0]
	}
	payload := map[string]any{"project_id": projectID, "current": current, "count": len(runs)}
	hold, err := store.departureHold(projectID, false)
	if err != nil {
		return err
	}
	releaseHold, err := store.departureHold(projectID, true)
	if err != nil {
		return err
	}
	payload["hold"], payload["release_hold"] = hold, releaseHold
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": true, "status": payload})
		return nil
	}
	if hold != nil {
		fmt.Printf("Departure hold: %s; resume: %s\n", hold.Reason, hold.ResumeAction)
	}
	if releaseHold != nil {
		fmt.Printf("Release hold: %s; resume: %s\n", releaseHold.Reason, releaseHold.ResumeAction)
	}
	if current == nil {
		fmt.Println("No scheduled departures have run.")
		return nil
	}
	fmt.Printf("Latest departure: %s (%s)\n", current.ID, current.State)
	return nil
}

func departureHistoryCmd(args Args) error {
	projectID := strings.TrimSpace(args.String("project"))
	if projectID == "" {
		return tuskerError(errorMissingArg, "--project is required")
	}
	limit := 20
	if raw := strings.TrimSpace(args.String("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 100 {
			return tuskerError(errorInvalidArg, "--limit must be between 1 and 100")
		}
		limit = parsed
	}
	store, err := OpenRuntimeStore(firstNonEmpty(strings.TrimSpace(args.String("state-root")), DefaultStateRoot()))
	if err != nil {
		return err
	}
	defer store.Close()
	runs, err := store.ListDepartureRuns(projectID)
	if err != nil {
		return err
	}
	if len(runs) > limit {
		runs = runs[:limit]
	}
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": true, "history": runs, "limit": limit})
		return nil
	}
	if len(runs) == 0 {
		fmt.Println("No scheduled departure history.")
		return nil
	}
	for _, run := range runs {
		fmt.Printf("%s %s\n", run.ScheduledWindow, run.State)
	}
	return nil
}

func departureHoldCmd(args Args) error {
	reason, err := requireArg(args, "reason")
	if err != nil {
		return err
	}
	by, err := v7HumanActor(args, "departure hold")
	if err != nil {
		return err
	}
	stateRoot := firstNonEmpty(strings.TrimSpace(args.String("state-root")), DefaultStateRoot())
	projectID := strings.TrimSpace(args.String("project"))
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		return err
	}
	defer store.Close()
	hold, err := store.SetDepartureHold(projectID, args.Bool("release-only"), reason, by, time.Now().UTC())
	if err != nil {
		return err
	}
	departureControlNotify(stateRoot, projectID, "departure_hold")
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": true, "hold": hold})
		return nil
	}
	fmt.Printf("Departure hold active: %s\nResume: %s\n", hold.Reason, hold.ResumeAction)
	return nil
}

func departureResumeCmd(args Args) error {
	by, err := v7HumanActor(args, "departure resume")
	if err != nil {
		return err
	}
	stateRoot := firstNonEmpty(strings.TrimSpace(args.String("state-root")), DefaultStateRoot())
	projectID := strings.TrimSpace(args.String("project"))
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		return err
	}
	defer store.Close()
	hold, err := store.ResumeDepartureHold(projectID, args.Bool("release-only"), by, time.Now().UTC())
	if err != nil {
		return err
	}
	departureControlNotify(stateRoot, projectID, "departure_resume")
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": true, "resumed": hold})
		return nil
	}
	fmt.Printf("Departure hold resumed by %s.\n", hold.ClearedBy)
	return nil
}

func printDepartureHelp() {
	fmt.Println(`Usage:
  tusker departure check|status|history --project <id> [--json]
  tusker departure hold [--project <id>] [--release-only] --reason <why> --by <actor>
  tusker departure resume [--project <id>] [--release-only] --by <actor>`)
}
