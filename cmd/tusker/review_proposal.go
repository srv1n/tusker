package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"
)

// harvestReviewProposal is the capability boundary between a dispatched
// reviewer and the daemon. The raw-log location comes only from the persisted
// active run, never from a CLI argument or worker environment. A proposal is
// consumed at most once by SaveReviewResult's attempt key; bad transport is
// contained by the caller to this run rather than returned as a poll-wide error.
func (d *Daemon) harvestReviewProposal(project RegisteredProject, note Note, run RunStatus) (bool, string) {
	if d == nil || d.store == nil || run.Lane != runLaneReview || strings.TrimSpace(run.ActiveAttemptID) == "" {
		return false, ""
	}
	path := strings.TrimSpace(run.RawLogPath)
	if path == "" || !pathWithin(d.stateRoot, path) {
		return true, "review proposal raw-log path escapes daemon-owned attempt output"
	}
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return false, ""
	}
	if err != nil {
		return true, "review proposal raw log cannot be inspected: " + err.Error()
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Size() < 1 {
		return true, "review proposal raw log is not a regular attempt output"
	}
	raw, err := readReviewProposalLog(path, info.Size())
	if err != nil {
		return true, "review proposal cannot be read: " + err.Error()
	}
	proposal, found, err := reviewProposalFromRawLog(raw)
	if err != nil {
		return true, "review proposal is malformed or conflicting"
	}
	if !found {
		return false, ""
	}
	if err := d.validateReviewProposal(project, note, run, proposal); err != nil {
		return true, "review proposal rejected: " + firstActionableLine("", err.Error())
	}
	result := proposal.Result
	result.Runner, result.RunnerProfile = run.Runner, run.RunnerProfile
	result.ResultRevision = reviewResultFingerprint(result)
	if _, err := d.store.SaveReviewResult(result); err != nil {
		return true, "review proposal could not be recorded: " + firstActionableLine("", err.Error())
	}
	return true, ""
}

func reviewProposalFromRawLog(raw []byte) (reviewProposal, bool, error) {
	// A final newline is required.  The wrapper may reconcile while its child is
	// still writing, and accepting a partial marker would make transport timing
	// authority. Existing identical markers are idempotent; any distinct marker
	// is an attacker-controlled conflict and must park this one review task.
	lines := strings.Split(string(raw), "\n")
	if len(lines) == 0 || lines[len(lines)-1] != "" {
		return reviewProposal{}, false, nil
	}
	var proposal reviewProposal
	found := false
	for _, line := range lines[:len(lines)-1] {
		if !strings.HasPrefix(line, reviewProposalMarker) {
			continue
		}
		payload := strings.TrimPrefix(line, reviewProposalMarker)
		if len(payload) == 0 || len(payload) > reviewProposalMax {
			return reviewProposal{}, false, fmt.Errorf("proposal marker is oversized")
		}
		var candidate reviewProposal
		if err := json.Unmarshal([]byte(payload), &candidate); err != nil {
			return reviewProposal{}, false, err
		}
		canonical, err := json.Marshal(candidate)
		if err != nil || string(canonical) != payload {
			return reviewProposal{}, false, fmt.Errorf("proposal marker is not canonical")
		}
		if found && !reflect.DeepEqual(candidate, proposal) {
			return reviewProposal{}, false, fmt.Errorf("conflicting proposal markers")
		}
		proposal, found = candidate, true
	}
	return proposal, found, nil
}

func readReviewProposalLog(path string, size int64) ([]byte, error) {
	const tailLimit = 2 * reviewProposalMax
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	if size > tailLimit {
		if _, err := file.Seek(size-tailLimit, 0); err != nil {
			return nil, err
		}
	}
	raw, err := io.ReadAll(io.LimitReader(file, tailLimit+1))
	if err != nil || len(raw) > tailLimit {
		return nil, fmt.Errorf("review proposal log tail is oversized")
	}
	if size > tailLimit {
		if cut := bytes.IndexByte(raw, '\n'); cut >= 0 {
			raw = raw[cut+1:]
		} else {
			return nil, fmt.Errorf("review proposal log tail has no complete record")
		}
	}
	return raw, nil
}

func (d *Daemon) validateReviewProposal(project RegisteredProject, note Note, run RunStatus, proposal reviewProposal) error {
	if strings.TrimSpace(proposal.Schema) != reviewProposalSchema || strings.TrimSpace(proposal.AttemptID) != run.ActiveAttemptID {
		return fmt.Errorf("proposal attempt identity is not the active review attempt")
	}
	attempt, err := d.store.ReviewAttempt(run.ActiveAttemptID)
	if err != nil {
		return err
	}
	if attempt.ProjectID != project.ProjectID || attempt.RecordID != run.RecordID || attempt.Lane != runLaneReview || attempt.WorkRevision != run.WorkRevision || attempt.Runner != run.Runner {
		return fmt.Errorf("proposal attempt does not match the daemon-owned review run")
	}
	if run.ProjectID != project.ProjectID || run.RecordID != stringField(note.Data, "id") || run.Lane != runLaneReview || !isDispatchingLeaseState(run.LeaseState) || firstNonEmpty(run.LeaseOwner, run.ActiveAttemptID) != run.ActiveAttemptID {
		return fmt.Errorf("proposal run lease is no longer authoritative")
	}
	result := proposal.Result
	if result.Runner != "" || result.RunnerProfile != "" || result.ResultRevision != "" {
		return fmt.Errorf("worker proposal attempted to choose runner authority")
	}
	result.Runner, result.RunnerProfile = run.Runner, run.RunnerProfile
	if err := normalizeReviewResult(&result); err != nil {
		return err
	}
	if result.Schema != reviewResultSchema || result.ProjectID != project.ProjectID || result.TaskID != run.RecordID || result.AttemptID != run.ActiveAttemptID || result.WorkRevision != run.WorkRevision ||
		result.TaskStateRev != stringField(note.Data, "state_rev") || result.ImplementationSHA != firstNonEmpty(stringField(note.Data, "source_sha"), stringField(note.Data, "source_commit")) {
		return fmt.Errorf("proposal task/work/source snapshot drifted")
	}
	wf, err := loadWorkflow(project.VaultRoot)
	if err != nil {
		return err
	}
	if result.Actor != reviewerActorForNote(wf.Data.Reviewer.Actor, note) {
		return fmt.Errorf("proposal reviewer actor is not authorized")
	}
	proof, gates, err := reviewObjectiveSnapshots(project.VaultRoot, note)
	if err != nil || result.ProofFingerprint != proof || result.GateFingerprint != gates {
		return fmt.Errorf("proposal proof or gate snapshot drifted")
	}
	switch result.Verdict {
	case "pass":
		want, got := sortedUniqueStrings(v7AcceptanceIDs(note.Body)), sortedUniqueStrings(result.Covers)
		if strings.Join(want, ",") != strings.Join(got, ",") {
			return fmt.Errorf("pass proposal does not cover the exact acceptance set")
		}
		report, reportErr := loadV7ProofReport(project.VaultRoot, run.RecordID)
		if reportErr != nil || report.Status != "satisfied" || len(report.OpenGates) != 0 {
			return fmt.Errorf("pass proposal requires currently satisfied proof and gates")
		}
	case "blocked":
		if result.Blocker == "human" {
			open, openErr := reviewHasOpenHumanBlocker(project.VaultRoot, run.RecordID)
			if openErr != nil || !open {
				return fmt.Errorf("human blocker proposal has no open human gate")
			}
		}
	}
	return nil
}
