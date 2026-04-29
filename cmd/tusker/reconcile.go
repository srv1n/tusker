package main

import "context"

type ReconcileDecision struct {
	LeaseState LeaseState
	Outcome    AttemptOutcome
	Resume     bool
}

type Reconciler struct{}

func (r *Reconciler) ReconcileAttempt(ctx context.Context, runner Runner, record AttemptRecord) (ReconcileDecision, error) {
	result, err := runner.Reconcile(ctx, ReconcileRequest{AttemptID: record.AttemptID, SessionRef: record.SessionRef})
	if err != nil {
		return ReconcileDecision{}, err
	}
	return ReconcileDecision{LeaseState: result.LeaseState, Outcome: result.Outcome}, nil
}
