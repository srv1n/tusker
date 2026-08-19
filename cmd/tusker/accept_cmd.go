package main

import (
	"fmt"
	"strings"
)

// acceptV7Cmd gives a reviewer one move for finished work whose proof is
// already green: it confirms the proof, records the reviewer as acceptor, and
// closes the task. When any proof row is non-green or missing, it refuses and
// leaves the task open with a named reason — it never rubber-stamps unproven
// work. It composes the existing status -> close steps; it never re-runs or
// re-judges proof.
func acceptV7Cmd(args Args) error {
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	actor, err := v7RequireAcceptor(args)
	if err != nil {
		return err
	}
	if err := ensureV7ControlMutation(vaultPath, args); err != nil {
		return err
	}
	id := firstNonEmpty(args.String("id"), args.String("_pos0"))
	if strings.TrimSpace(id) == "" {
		return tuskerError(errorMissingArg, "Usage: tusker accept <task-id> [--by reviewer:name]")
	}
	args["id"] = id

	idx, err := loadV7Index(vaultPath)
	if err != nil {
		return err
	}
	task, ok := idx.Tasks[id]
	if !ok {
		return tuskerError(errorNotFound, "V7 task not found: "+id)
	}

	report := computeV7ProofReport(vaultPath, task, idx)
	if !v7ProofGreenForAccept(task, report) && len(v7PendingCommandProofGaps(task, report)) > 0 {
		return tuskerError(
			errorEvidenceGate,
			id+": accept refused, proof is not green: "+v7AcceptRefusalReason(report),
			withHint("cover every acceptance and proof requirement, then re-run accept; accept never closes unproven work"),
			withContext(map[string]any{"proof_status": report.Status, "missing": v7AcceptMissing(report)}),
		)
	}

	// Require an explicit reviewer identity: no --by must never inherit a default
	// actor that trivially clears the acceptor policy. The identity must be
	// namespaced so the close policy can tell a reviewer/human from an agent.
	// Pre-flight every close precondition BEFORE any status write, so a refusal
	// leaves the task exactly where it was instead of stranding it in review.
	// These mirror the checks close enforces, reading the same sources.
	preflight, err := v7AcceptPreflight(vaultPath, args, task, idx, actor)
	if err != nil {
		return err
	}
	task, idx = preflight.Task, preflight.Index
	report = computeV7ProofReport(vaultPath, task, idx)
	if !v7ProofGreenForAccept(task, report) {
		return tuskerError(errorEvidenceGate, id+": accept refused, proof is not green: "+v7AcceptRefusalReason(report))
	}

	// Preconditions hold: move to review, then close so the existing close
	// policy, gates, evidence, and acceptor rules are enforced and the reviewer
	// is recorded as acceptor.
	statusArgs := v7AcceptSubArgs(args)
	statusArgs["status"] = "review"
	if err := statusV7Cmd(statusArgs); err != nil {
		return err
	}

	closeArgs := v7AcceptSubArgs(args)
	if err := closeV7Cmd(closeArgs); err != nil {
		// Preconditions were checked, so a failure here is unexpected. The task
		// has already been moved to review by the status step above; surface that
		// stranded state honestly rather than pretending nothing changed.
		return tuskerError(
			errorInvalidTransition,
			id+": accept moved the task to review but close then failed: "+err.Error()+
				" — the task is stranded in review; resolve the reported cause and re-run `tusker close "+id+"`",
		)
	}

	if !args.Bool("quiet") {
		fmt.Printf("%s: accepted and closed by %s\n", id, actor)
	}
	return nil
}

// v7RequireAcceptor returns the explicit reviewer identity, refusing when --by
// (or --actor) is missing or is not namespaced with a reviewer:/human: prefix.
func v7RequireAcceptor(args Args) (string, error) {
	if strings.TrimSpace(fallback(args.String("by"), args.String("actor"))) == "" {
		return "", tuskerError(
			errorMissingArg,
			"accept requires an explicit acceptor: pass --by reviewer:<name> or --by human:<name>",
			withHint("accept records who signed off; it never inherits a default agent identity"),
		)
	}
	return v7ReviewerOrHumanActor(args, "accept")
}

// v7AcceptSourceStatusAllowed reports whether accept may legally run from the
// task's current status. Accept is a one-step review+close, so it accepts from
// any status that can legally reach review and refuses the terminal states that
// close could never legitimately accept from.
func v7AcceptSourceStatusAllowed(status string) bool {
	switch status {
	case "done", "cancelled", "superseded":
		return false
	default:
		return true
	}
}

// v7AcceptPreflight refuses accept before any state is written when the current
// status is illegal or when close would reject the task. It mirrors the
// close-time checks (acceptor policy, open blocking gates, unclosed
// dependencies, required evidence, acceptance/proof) against the same sources.
func v7AcceptPreflight(vaultPath string, args Args, task Note, idx v7Index, actor string) (v7ClosePreflightResult, error) {
	id := stringField(task.Data, "id")

	status := strings.ToLower(strings.TrimSpace(stringField(task.Data, "status")))
	switch status {
	case "done":
		return v7ClosePreflightResult{}, tuskerError(errorInvalidTransition, id+": accept refused, task is already done", withContext(map[string]any{"status": status}))
	case "cancelled":
		return v7ClosePreflightResult{}, tuskerError(errorInvalidTransition, id+": accept refused, task is cancelled", withContext(map[string]any{"status": status}))
	}
	if !v7AcceptSourceStatusAllowed(status) {
		return v7ClosePreflightResult{}, tuskerError(
			errorInvalidTransition,
			id+": accept refused, cannot accept from status "+fallback(status, "(none)")+"; accept only from ready, review, or rework",
			withContext(map[string]any{"status": status}),
		)
	}

	return v7ClosePreflight(vaultPath, task, idx, v7ClosePreflightRequest{
		Args: args, Actor: actor, Action: "accept", ExpectedTaskID: id,
	})
}

// v7ProofGreenForAccept reports whether every proof row is green — the same
// condition the close ceremony enforces before acceptance.
func v7ProofGreenForAccept(task Note, report v7ProofReport) bool {
	if strings.EqualFold(stringField(task.Data, "proof_status"), "waived") {
		return true
	}
	return len(report.Missing) == 0 && len(report.ModeMissing) == 0
}

func v7AcceptMissing(report v7ProofReport) []string {
	missing := append([]string{}, report.Missing...)
	missing = append(missing, report.ModeMissing...)
	return missing
}

func v7AcceptRefusalReason(report v7ProofReport) string {
	missing := v7AcceptMissing(report)
	if len(missing) == 0 {
		return "proof_status=" + fallback(report.Status, "pending")
	}
	return "proof_status=" + fallback(report.Status, "pending") + "; uncovered: " + strings.Join(missing, ", ")
}

// v7AcceptSubArgs copies the flags the composed status/close steps read, so
// each step sees the reviewer and vault without inheriting stray positionals.
func v7AcceptSubArgs(args Args) Args {
	sub := Args{"id": args.String("id"), "quiet": "true"}
	for _, key := range []string{"vault", "by", "actor", "reason", "force", "json"} {
		if value, ok := args[key]; ok {
			sub[key] = value
		}
	}
	return sub
}
