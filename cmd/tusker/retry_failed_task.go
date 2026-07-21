package main

import (
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// retryFailedTaskResult reports what a single retry request did to one run.
//
// The no-op cases are reported honestly and distinctly: AlreadyLive means a
// real attempt is in flight (verified process or unexpired lease) and AttemptID
// names it; AlreadyQueued means the run is merely parked for retry with no live
// attempt, so AttemptID is empty. Expedited means a queued run whose retry was
// scheduled for the future was pulled forward to now on operator demand.
type retryFailedTaskResult struct {
	Run           RunStatus
	AttemptID     string
	Requeued      bool
	AlreadyLive   bool
	AlreadyQueued bool
	Expedited     bool
	Reason        string
	Reset         BudgetRedriveRecord
}

// retryLiveness classifies whether a run already carries an attempt that a
// repeat retry must not duplicate.
type retryLiveness int

const (
	// retryNotLive: nothing live or queued — the run is safe to requeue.
	retryNotLive retryLiveness = iota
	// retryLiveAttempt: a real attempt is in flight (verified live process, or
	// an unexpired Claimed/Running lease).
	retryLiveAttempt
	// retryQueued: the run is parked for retry (RetryQueued, non-terminal) but
	// has no live attempt behind it.
	retryQueued
)

// retryProcessVerifiablyAlive reports whether the run's recorded process is
// provably still ours and running. Unlike the daemon's broader liveness probe,
// it refuses the bare PGID fallback whenever a PID is recorded but its identity
// does not match: a reused or other-user PGID that answers kill(-pgid,0) with
// EPERM must not be mistaken for a live attempt of ours. Dead is dead.
func retryProcessVerifiablyAlive(run RunStatus) bool {
	if processIdentityMatches(run) {
		return true
	}
	if run.ProcessPID > 0 {
		// A PID is recorded but its identity explicitly mismatches: the process
		// is gone or belongs to someone else. Do not fall back to a bare PGID
		// probe (EPERM/reuse would read as alive).
		return false
	}
	// No PID was ever recorded, so there is no identity to verify against; fall
	// back to a best-effort process-group existence check.
	return run.ProcessPGID > 0 && processGroupExists(run.ProcessPGID)
}

// retryLeaseUnexpired reports whether a Claimed/Running lease is still within
// its TTL. A stale (expired) lease is a crashed worker's leftover, not a live
// attempt, and must not block a retry — nothing else will reap it in the
// interactive / daemon-off case.
func retryLeaseUnexpired(run RunStatus, now time.Time) bool {
	expires, err := time.Parse(time.RFC3339, strings.TrimSpace(run.LeaseExpiresAt))
	if err != nil {
		return false
	}
	return expires.After(now)
}

// retryHasLiveAttempt classifies whether a run already has an attempt in flight
// or queued. Claimed/Running counts as live only when the lease is unexpired or
// the process is verifiably alive; a stale lease left by a crashed worker is
// reapable, so a retry sees through it.
func retryHasLiveAttempt(run RunStatus, now time.Time) retryLiveness {
	if retryProcessVerifiablyAlive(run) {
		return retryLiveAttempt
	}
	switch LeaseState(strings.TrimSpace(run.LeaseState)) {
	case LeaseStateClaimed, LeaseStateRunning:
		if retryLeaseUnexpired(run, now) {
			return retryLiveAttempt
		}
		// Stale lease with no live process: reap it via retry.
		return retryNotLive
	case LeaseStateRetryQueued:
		// A run already queued for retry (and not terminal) is the attempt a
		// repeat retry would produce.
		if !run.Terminal {
			return retryQueued
		}
		return retryNotLive
	default:
		return retryNotLive
	}
}

// retryQueuedInFuture reports the run's pending NextRetryAt when it is still in
// the future relative to now, so an operator retry can expedite it.
func retryQueuedInFuture(run RunStatus, now time.Time) (string, bool) {
	raw := strings.TrimSpace(run.NextRetryAt)
	if raw == "" {
		return "", false
	}
	due, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return "", false
	}
	if due.After(now) {
		return raw, true
	}
	return "", false
}

// isCASConflict reports whether err is a lost optimistic-concurrency race.
func isCASConflict(err error) bool {
	var typed *TuskerError
	return errors.As(err, &typed) && typed.Code == "CAS_CONFLICT"
}

// retryFailedRun retries one failed piece of work without disturbing its
// neighbours. It is idempotent: a repeat retry while an attempt is already live
// or queued returns a friendly no-op instead of creating a duplicate, and two
// concurrent retries converge on the winner's outcome rather than surfacing a
// raw CAS conflict. It resets only the target run, so sibling runs are left
// exactly as they were.
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
	actor = firstNonEmpty(strings.TrimSpace(actor), defaultActorName())
	reason = firstNonEmpty(strings.TrimSpace(reason), "operator retry")

	run, err := store.FindRun(identity)
	if err != nil {
		return retryFailedTaskResult{}, err
	}
	if run == nil {
		return retryFailedTaskResult{}, tuskerError(errorNotFound, "run not found: "+identity)
	}

	switch retryHasLiveAttempt(*run, now) {
	case retryLiveAttempt:
		// A genuine attempt is live: report it and name it.
		return retryFailedTaskResult{
			Run:         *run,
			AttemptID:   strings.TrimSpace(run.ActiveAttemptID),
			AlreadyLive: true,
			Reason:      "attempt already live for " + firstNonEmpty(run.ItemID, run.RecordID),
		}, nil
	case retryQueued:
		// Already parked for retry. If the retry is scheduled for the future, an
		// explicit operator retry expedites it (advance to now, keep counters);
		// otherwise it is a genuine no-op with no live attempt to name.
		if due, ok := retryQueuedInFuture(*run, now); ok {
			res, err := expediteQueuedRetry(store, run, actor, reason, now, due)
			if err != nil {
				if isCASConflict(err) {
					return retryConcurrentReadback(store, identity, now)
				}
				return retryFailedTaskResult{}, err
			}
			return res, nil
		}
		return retryFailedTaskResult{
			Run:           *run,
			AlreadyQueued: true,
			Reason:        "retry already queued for " + firstNonEmpty(run.ItemID, run.RecordID),
		}, nil
	}

	// Reset only this run. RedriveRunIfSnapshot is scoped to the single record,
	// so neighbouring runs are never read or written by this transition.
	prev := *run
	reset, err := redriveRuntimeRun(store, run, actor, reason, now)
	if err != nil {
		// Concurrent idempotency: a lost CAS race means another retry won. Re-read
		// and report the winner's outcome as a clean no-op rather than an error.
		if isCASConflict(err) {
			return retryConcurrentReadback(store, identity, now)
		}
		return retryFailedTaskResult{}, err
	}
	if auditErr := recordRetryAudit(store, prev, *run, actor, reason, "operator_redrive", now); auditErr != nil {
		return retryFailedTaskResult{}, auditErr
	}
	return retryFailedTaskResult{
		Run:       *run,
		AttemptID: strings.TrimSpace(run.ActiveAttemptID),
		Requeued:  true,
		Reason:    "requeued " + firstNonEmpty(run.ItemID, run.RecordID) + " for retry",
		Reset:     reset,
	}, nil
}

// expediteQueuedRetry pulls a future-scheduled retry forward to now without
// touching the attempt counters, so an operator can force an immediate retry of
// a run parked in backoff.
func expediteQueuedRetry(store *RuntimeStore, run *RunStatus, actor, reason string, now time.Time, previousDue string) (retryFailedTaskResult, error) {
	prev := *run
	expedited := prev
	expedited.NextRetryAt = now.Format(time.RFC3339)
	expedited.LastError = "retry expedited by " + actor + ": " + reason
	expedited.LastEventAt = now.Format(time.RFC3339)
	expedited.UpdatedAt = now.Format(time.RFC3339)
	updated, err := store.UpsertRunIfSnapshot(prev, expedited)
	if err != nil {
		return retryFailedTaskResult{}, err
	}
	if !updated {
		return retryFailedTaskResult{}, tuskerError("CAS_CONFLICT", "run changed while retry was being expedited: "+firstNonEmpty(prev.ItemID, prev.RecordID), withHint("reload the run and retry"))
	}
	*run = expedited
	if auditErr := recordRetryAudit(store, prev, expedited, actor, reason, "operator_redrive_expedite", now); auditErr != nil {
		return retryFailedTaskResult{}, auditErr
	}
	return retryFailedTaskResult{
		Run:       expedited,
		Expedited: true,
		Reason:    "expedited " + firstNonEmpty(expedited.ItemID, expedited.RecordID) + " (was queued until " + previousDue + ")",
	}, nil
}

// retryConcurrentReadback re-reads a run after a lost CAS race and reports the
// winning retry's outcome as a friendly no-op, so the losing caller of a
// concurrent pair gets a clean result rather than a raw CAS_CONFLICT.
func retryConcurrentReadback(store *RuntimeStore, identity string, now time.Time) (retryFailedTaskResult, error) {
	run, err := store.FindRun(identity)
	if err != nil {
		return retryFailedTaskResult{}, err
	}
	if run == nil {
		return retryFailedTaskResult{}, tuskerError(errorNotFound, "run not found: "+identity)
	}
	if retryHasLiveAttempt(*run, now) == retryLiveAttempt {
		return retryFailedTaskResult{
			Run:         *run,
			AttemptID:   strings.TrimSpace(run.ActiveAttemptID),
			AlreadyLive: true,
			Reason:      "attempt already live for " + firstNonEmpty(run.ItemID, run.RecordID),
		}, nil
	}
	return retryFailedTaskResult{
		Run:           *run,
		AlreadyQueued: true,
		Reason:        "retry already applied concurrently for " + firstNonEmpty(run.ItemID, run.RecordID),
	}, nil
}

// recordRetryAudit writes the same supervisor-decision audit trail the redrive
// path has always emitted, so an operator retry (from any entry point) is
// visible as a run transition.
func recordRetryAudit(store *RuntimeStore, prev, next RunStatus, actor, reason, contextSignal string, now time.Time) error {
	if store == nil {
		return nil
	}
	auditPayload, err := json.Marshal(map[string]any{
		"actor":                    actor,
		"reason":                   reason,
		"reset_at":                 now.Format(time.RFC3339),
		"previous_attempt_count":   prev.AttemptCount,
		"previous_lease_state":     prev.LeaseState,
		"previous_attempt_outcome": prev.AttemptOutcome,
	})
	if err != nil {
		return err
	}
	_, err = store.SaveSupervisorDecision(SupervisorDecision{
		ProjectID:        next.ProjectID,
		RecordID:         next.RecordID,
		ItemID:           next.ItemID,
		Runner:           next.Runner,
		WorkRevision:     next.WorkRevision,
		AttemptID:        prev.ActiveAttemptID,
		ParentAttemptID:  prev.ActiveAttemptID,
		SessionRef:       prev.SessionRef,
		ParentSessionRef: prev.SessionRef,
		Kind:             string(SupervisorDecisionRedrive),
		Reason:           reason,
		WorkspacePath:    prev.WorkspacePath,
		ValidationDelta:  string(auditPayload),
		LeaseState:       next.LeaseState,
		ContextSignal:    contextSignal,
		CreatedAt:        now.Format(time.RFC3339),
	})
	return err
}
