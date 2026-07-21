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

	// Read the existing proof result; do not re-run or re-judge it.
	report := computeV7ProofReport(vaultPath, task, idx)
	if !v7ProofGreenForAccept(task, report) {
		return tuskerError(
			errorEvidenceGate,
			id+": accept refused, proof is not green: "+v7AcceptRefusalReason(report),
			withHint("cover every acceptance and proof requirement, then re-run accept; accept never closes unproven work"),
			withContext(map[string]any{"proof_status": report.Status, "missing": v7AcceptMissing(report)}),
		)
	}

	actor := fallback(fallback(args.String("actor"), args.String("by")), "reviewer:agent")

	// Confirm the proof and close in one step: move to review, then close so the
	// existing close policy, gates, evidence, and acceptor rules are enforced and
	// the reviewer is recorded as acceptor.
	statusArgs := v7AcceptSubArgs(args)
	statusArgs["status"] = "review"
	if err := statusV7Cmd(statusArgs); err != nil {
		return err
	}

	closeArgs := v7AcceptSubArgs(args)
	if err := closeV7Cmd(closeArgs); err != nil {
		return err
	}

	if !args.Bool("quiet") {
		fmt.Printf("%s: accepted and closed by %s\n", id, actor)
	}
	return nil
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
