package main

import (
	"reflect"
	"testing"
	"time"
)

func TestReworkRedispatchAfterReviewChanges(t *testing.T) {
	env := newReviewPassEnv(t, map[string]string{"reviewed.txt": "reviewed\n"}, []string{"reviewed.txt"})
	env.submitProposal(t, "changes_requested")
	env.passHandler(t)
	note := env.task(t)
	assertEqual(t, "rework", stringField(note.Data, "status"), "review handback")
	ctx := &automationCommandContext{Store: env.daemon.store, Project: env.project}
	for _, lane := range []string{runLaneReview, runLaneExecute} {
		t.Run(lane, func(t *testing.T) {
			run := env.run
			run.Lane = lane
			ctx.ProjectRuns = map[string]RunStatus{run.RecordID: run}
			effective := ctx.effectiveRunForTask(note, run.Runner)
			assertEqual(t, runLaneExecute, effective.Lane, "next lane")
			assertEqual(t, "", automationRunBlocker(effective, time.Now().UTC()), "dispatch blocker")
			if !shouldDispatchRun(effective, time.Now().UTC()) {
				t.Fatalf("review changes did not release the next implementation: %#v", effective)
			}
			assertEqual(t, run.AttemptCount, effective.AttemptCount, "attempt cap retained")
			if !reflect.DeepEqual(run, ctx.ProjectRuns[run.RecordID]) {
				t.Fatal("planning mutated the stored run")
			}
		})
	}
	for _, state := range []LeaseState{LeaseStateClaimed, LeaseStateRunning} {
		t.Run(string(state), func(t *testing.T) {
			run := env.run
			run.LeaseState = string(state)
			run.ActiveAttemptID = "next-implementation"
			ctx.ProjectRuns = map[string]RunStatus{run.RecordID: run}
			effective := ctx.effectiveRunForTask(note, run.Runner)
			assertEqual(t, run.ActiveAttemptID, effective.ActiveAttemptID, "active attempt preserved")
			assertEqual(t, "existing run is "+string(state), automationRunBlocker(effective, time.Now().UTC()), "duplicate attempt blocked")
		})
	}
}

func TestReworkRedispatchKeepsOperatorRelease(t *testing.T) {
	env := newReviewPassEnv(t, map[string]string{"reviewed.txt": "reviewed\n"}, []string{"reviewed.txt"})
	env.submitProposal(t, "changes_requested")
	env.passHandler(t)
	note := env.task(t)
	ctx := &automationCommandContext{Store: env.daemon.store, Project: env.project}
	for _, lane := range []string{runLaneReview, runLaneExecute} {
		for _, reason := range []string{"operator stop", "retired by human:owner: stop this task", "budget exhausted"} {
			t.Run(lane+"/"+reason, func(t *testing.T) {
				run := env.run
				run.Lane, run.LastError = lane, reason
				run.Terminal = reason == "retired by human:owner: stop this task"
				ctx.ProjectRuns = map[string]RunStatus{run.RecordID: run}
				effective := ctx.effectiveRunForTask(note, run.Runner)
				if !reflect.DeepEqual(run, effective) {
					t.Fatalf("operator release changed: before=%#v after=%#v", run, effective)
				}
				assertEqual(t, automationRunBlocker(run, time.Now().UTC()), automationRunBlocker(effective, time.Now().UTC()), "release refusal preserved")
				if shouldDispatchRun(effective, time.Now().UTC()) {
					t.Fatal("operator release became dispatchable")
				}
			})
		}
	}
}
