package main

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

type runsListItem struct {
	RunID   string `json:"run_id"`
	TaskID  string `json:"task_id"`
	Profile string `json:"profile"`
	State   string `json:"state"`
	Started string `json:"started"`
	Attempt int    `json:"attempt"`
}

func runsListCmd(args Args) error {
	limit := 100
	if raw := strings.TrimSpace(args.String("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			return tuskerError(errorInvalidArg, "--limit requires a positive integer")
		}
		limit = parsed
	}
	store, err := OpenRuntimeStoreReadOnly(DefaultStateRoot())
	if err != nil {
		return err
	}
	defer store.Close()
	runs, err := store.ListRuns()
	if err != nil {
		return err
	}
	sort.SliceStable(runs, func(i, j int) bool { return runs[i].StartedAt > runs[j].StartedAt })
	out := make([]runsListItem, 0, minInt(limit, len(runs)))
	for _, run := range runs {
		if args.String("project") != "" && run.ProjectID != args.String("project") {
			continue
		}
		if args.Bool("active") && run.LeaseState != string(LeaseStateClaimed) && run.LeaseState != string(LeaseStateRunning) {
			continue
		}
		out = append(out, runsListItem{firstNonEmpty(run.ActiveAttemptID, run.RecordID), firstNonEmpty(run.RecordID, run.ItemID), run.RunnerProfile, run.LeaseState, run.StartedAt, run.AttemptCount})
		if len(out) == limit {
			break
		}
	}
	if args.Bool("json") {
		emitJSON(out)
		return nil
	}
	for _, run := range out {
		fmt.Printf("%s  %s  profile=%s  state=%s  started=%s  attempt=%d\n", run.RunID, run.TaskID, run.Profile, run.State, run.Started, run.Attempt)
	}
	return nil
}
