package main

import (
	"bufio"
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
	if path == "" {
		return false, ""
	}
	if !pathWithin(d.stateRoot, path) {
		return true, "review proposal raw-log path escapes daemon-owned attempt output"
	}
	proposal, present, err := readFrozenReviewProposalLog(path)
	if err != nil {
		return true, "review proposal cannot be read: " + err.Error()
	}
	if !present {
		return false, ""
	}
	if err := d.validateReviewProposal(project, note, run, proposal); err != nil {
		return true, "review proposal rejected: " + firstActionableLine("", err.Error())
	}
	result := proposal.Result
	result.Runner, result.RunnerProfile = run.Runner, run.RunnerProfile
	result.WorkerPolicyFP, err = completionCombinedWorkerPolicyFingerprint(run.ExecutePolicyFP, run.WorkerPolicyFP)
	if err != nil {
		return true, "review proposal rejected: terminal review run has no authenticated worker policy"
	}
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

func readFrozenReviewProposalLog(path string) (reviewProposal, bool, error) {
	pathInfo, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return reviewProposal{}, false, nil
	}
	if err != nil {
		return reviewProposal{}, false, err
	}
	if pathInfo.Mode()&os.ModeSymlink != 0 || !pathInfo.Mode().IsRegular() {
		return reviewProposal{}, false, fmt.Errorf("review proposal raw log is not a regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return reviewProposal{}, false, err
	}
	defer file.Close()
	openedInfo, err := file.Stat()
	if err != nil || !openedInfo.Mode().IsRegular() || !os.SameFile(pathInfo, openedInfo) {
		return reviewProposal{}, false, fmt.Errorf("review proposal raw log changed while opening")
	}
	size := openedInfo.Size()
	if size == 0 {
		return reviewProposal{}, false, nil
	}
	proposal, found, err := scanReviewProposalLog(io.LimitReader(file, size))
	if err != nil {
		return reviewProposal{}, false, err
	}
	afterInfo, err := file.Stat()
	currentInfo, currentErr := os.Lstat(path)
	if err != nil || currentErr != nil || afterInfo.Size() != size || !os.SameFile(openedInfo, afterInfo) || !os.SameFile(pathInfo, currentInfo) {
		return reviewProposal{}, false, fmt.Errorf("review proposal raw log changed while reading")
	}
	return proposal, found, nil
}

func scanReviewProposalLog(input io.Reader) (reviewProposal, bool, error) {
	reader := bufio.NewReaderSize(input, reviewProposalMax+len(reviewProposalMarker)+2)
	var proposal reviewProposal
	found := false
	for {
		line, err := reader.ReadSlice('\n')
		if err == bufio.ErrBufferFull {
			if strings.HasPrefix(string(line), reviewProposalMarker) {
				return reviewProposal{}, false, fmt.Errorf("proposal marker is oversized")
			}
			for err == bufio.ErrBufferFull {
				_, err = reader.ReadSlice('\n')
			}
			if err == io.EOF {
				return proposal, found, nil
			}
			if err != nil {
				return reviewProposal{}, false, err
			}
			continue
		}
		if len(line) > 0 && line[len(line)-1] == '\n' {
			line = line[:len(line)-1]
			if len(line) > 0 && line[len(line)-1] == '\r' {
				line = line[:len(line)-1]
			}
			if strings.HasPrefix(string(line), reviewProposalMarker) {
				candidate, ok, parseErr := reviewProposalFromRawLog(append(append([]byte(nil), line...), '\n'))
				if parseErr != nil || !ok {
					return reviewProposal{}, false, firstNonNilError(parseErr, fmt.Errorf("proposal marker is malformed"))
				}
				if found && !reflect.DeepEqual(candidate, proposal) {
					return reviewProposal{}, false, fmt.Errorf("conflicting proposal markers")
				}
				proposal, found = candidate, true
			}
		}
		if err == io.EOF {
			if len(line) > 0 && strings.HasPrefix(string(line), reviewProposalMarker) {
				return reviewProposal{}, false, fmt.Errorf("unterminated proposal marker")
			}
			return proposal, found, nil
		}
		if err != nil {
			return reviewProposal{}, false, err
		}
	}
}

func firstNonNilError(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

func (d *Daemon) validateReviewProposal(project RegisteredProject, note Note, run RunStatus, proposal reviewProposal) error {
	if strings.TrimSpace(proposal.Schema) != reviewProposalSchema || strings.TrimSpace(proposal.AttemptID) != run.ActiveAttemptID {
		return fmt.Errorf("proposal attempt identity is not the active review attempt")
	}
	attempt, err := d.store.ReviewAttempt(run.ActiveAttemptID)
	if err != nil {
		return err
	}
	if attempt.ProjectID != project.ProjectID || attempt.RecordID != run.RecordID || attempt.Lane != runLaneReview || attempt.WorkRevision != run.WorkRevision || attempt.Runner != run.Runner || attempt.WorkerPolicyFP != run.WorkerPolicyFP {
		return fmt.Errorf("proposal attempt does not match the daemon-owned review run")
	}
	if run.ProjectID != project.ProjectID || run.RecordID != stringField(note.Data, "id") || run.Lane != runLaneReview || !isDispatchingLeaseState(run.LeaseState) || firstNonEmpty(run.LeaseOwner, run.ActiveAttemptID) != run.ActiveAttemptID {
		return fmt.Errorf("proposal run lease is no longer authoritative")
	}
	result := proposal.Result
	if result.Runner != "" || result.RunnerProfile != "" || result.WorkerPolicyFP != "" || result.ResultRevision != "" {
		return fmt.Errorf("worker proposal attempted to choose runner authority")
	}
	wf, err := loadWorkflow(project.VaultRoot)
	if err != nil {
		return err
	}
	_, _, executePolicyFP, err := completionLaneWorkerPolicy(wf.Data, note, runLaneExecute)
	if err != nil {
		return err
	}
	reviewProfile, _, reviewPolicyFP, err := completionLaneWorkerPolicy(wf.Data, note, runLaneReview)
	if err != nil {
		return err
	}
	if run.ExecutePolicyFP != executePolicyFP || run.WorkerPolicyFP != reviewPolicyFP || run.RunnerProfile != reviewProfile.Name {
		return fmt.Errorf("proposal worker policy drifted from the current explicit lane profiles")
	}
	result.Runner, result.RunnerProfile = run.Runner, run.RunnerProfile
	result.WorkerPolicyFP, err = completionCombinedWorkerPolicyFingerprint(run.ExecutePolicyFP, run.WorkerPolicyFP)
	if err != nil {
		return err
	}
	if err := normalizeReviewResult(&result); err != nil {
		return err
	}
	if result.Schema != reviewResultSchema || result.ProjectID != project.ProjectID || result.TaskID != run.RecordID || result.AttemptID != run.ActiveAttemptID || result.WorkRevision != run.WorkRevision ||
		result.TaskStateRev != stringField(note.Data, "state_rev") || result.ImplementationSHA != firstNonEmpty(stringField(note.Data, "source_sha"), stringField(note.Data, "source_commit")) {
		return fmt.Errorf("proposal task/work/source snapshot drifted")
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
