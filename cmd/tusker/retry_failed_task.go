package main

import (
	"strings"
	"time"
)

// retryFailedTaskResult reports what a single retry request did to one run.
type retryFailedTaskResult struct {
	Run         RunStatus
	AttemptID   string
	Requeued    bool
	AlreadyLive bool
	Reason      string
	Reset       BudgetRedriveRecord
}

// retryHasLiveAttempt reports whether a run already has an attempt in flight or
// queued. When it does, retrying is a no-op: opening a second attempt would put
// two competing attempts on the same piece.
func retryHasLiveAttempt(run RunStatus) bool {
	if runProcessGroupAlive(run) {
		return true
	}
	switch LeaseState(strings.TrimSpace(run.LeaseState)) {
	case LeaseStateClaimed, LeaseStateRunning:
		return true
	case LeaseStateRetryQueued:
		// A run already queued for retry (and not terminal) is the attempt a
		// repeat retry would produce, so the repeat is a no-op.
		return !run.Terminal
	default:
		return false
	}
}

// retryFailedRun retries one failed piece of work without disturbing its
// neighbours. It is idempotent: a repeat retry while an attempt is already live
// returns that attempt instead of creating a duplicate. It resets only the
// target run, so sibling runs are left exactly as they were.
func retryFailedRun(store *RuntimeStore, identity, actor, reason string, now time.Time) (retryFailedTaskResult, error) {
	if store == nil {
		return retryFailedTaskResult{}, tuskerError(errorNotFound, "run not found")
	}
	identity = strings.TrimSpace(identity)
	if identity == "" {
		return retryFailedTaskResult{}, tuskerError(errorInvalidArg, "retry requires a run id")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	now = now.UTC()

	run, err := store.FindRun(identity)
	if err != nil {
		return retryFailedTaskResult{}, err
	}
	if run == nil {
		return retryFailedTaskResult{}, tuskerError(errorNotFound, "run not found: "+identity)
	}

	// Idempotent guard: if an attempt is already live or queued, this repeat
	// retry returns the same attempt rather than opening a competing one.
	if retryHasLiveAttempt(*run) {
		return retryFailedTaskResult{
			Run:         *run,
			AttemptID:   strings.TrimSpace(run.ActiveAttemptID),
			AlreadyLive: true,
			Reason:      "attempt already live for " + firstNonEmpty(run.ItemID, run.RecordID),
		}, nil
	}

	// Reset only this run. RedriveRunIfSnapshot is scoped to the single record,
	// so neighbouring runs are never read or written by this transition.
	reset, err := redriveRuntimeRun(store, run, actor, reason, now)
	if err != nil {
		return retryFailedTaskResult{}, err
	}
	return retryFailedTaskResult{
		Run:       *run,
		AttemptID: strings.TrimSpace(run.ActiveAttemptID),
		Requeued:  true,
		Reason:    "requeued " + firstNonEmpty(run.ItemID, run.RecordID) + " for retry",
		Reset:     reset,
	}, nil
}
