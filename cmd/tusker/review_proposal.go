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
	result, err := d.validateReviewProposal(project, note, run, proposal)
	if err != nil {
		return true, "review proposal rejected: " + firstActionableLine("", err.Error())
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
	return readFrozenReviewProposalLogWithOpen(path, os.Open)
}

func readFrozenReviewProposalLogWithOpen(path string, open func(string) (*os.File, error)) (reviewProposal, bool, error) {
	pathInfo, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return reviewProposal{}, false, nil
	}
	if err != nil {
		return reviewProposal{}, false, err
	}
	if err := validateExclusiveRawLog(pathInfo); err != nil {
		return reviewProposal{}, false, fmt.Errorf("review proposal raw log is not exclusive: %w", err)
	}
	if pathInfo.Size() > completionAuthoritativeRawLogMaxBytes {
		return reviewProposal{}, false, fmt.Errorf(
			"review proposal raw log exceeds %d-byte completion-authority limit",
			completionAuthoritativeRawLogMaxBytes,
		)
	}
	file, err := open(path)
	if err != nil {
		return reviewProposal{}, false, err
	}
	defer file.Close()
	openedInfo, err := file.Stat()
	if err != nil || !os.SameFile(pathInfo, openedInfo) {
		return reviewProposal{}, false, fmt.Errorf("review proposal raw log changed while opening")
	}
	if err := validateExclusiveRawLog(openedInfo); err != nil {
		return reviewProposal{}, false, fmt.Errorf("review proposal raw log is not exclusive: %w", err)
	}
	size := openedInfo.Size()
	if size > completionAuthoritativeRawLogMaxBytes {
		return reviewProposal{}, false, fmt.Errorf(
			"review proposal raw log exceeds %d-byte completion-authority limit",
			completionAuthoritativeRawLogMaxBytes,
		)
	}
	if size == 0 {
		return reviewProposal{}, false, nil
	}
	currentInfo, err := os.Lstat(path)
	if err != nil || !os.SameFile(openedInfo, currentInfo) {
		return reviewProposal{}, false, fmt.Errorf("review proposal raw log changed before reading")
	}
	if err := validateExclusiveRawLog(currentInfo); err != nil {
		return reviewProposal{}, false, fmt.Errorf("review proposal raw log is not exclusive: %w", err)
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
	if err := validateExclusiveRawLog(afterInfo); err != nil {
		return reviewProposal{}, false, fmt.Errorf("review proposal raw log is not exclusive: %w", err)
	}
	if err := validateExclusiveRawLog(currentInfo); err != nil {
		return reviewProposal{}, false, fmt.Errorf("review proposal raw log is not exclusive: %w", err)
	}
	return proposal, found, nil
}

func scanReviewProposalLog(input io.Reader) (reviewProposal, bool, error) {
	return scanReviewProposalLogWithLimit(input, completionAuthoritativeRawLogMaxBytes)
}

func scanReviewProposalLogWithLimit(input io.Reader, maxBytes int64) (reviewProposal, bool, error) {
	if maxBytes <= 0 {
		return reviewProposal{}, false, fmt.Errorf("review proposal raw-log scan requires a positive byte limit")
	}
	limited := &io.LimitedReader{R: input, N: maxBytes + 1}
	reader := bufio.NewReaderSize(limited, reviewProposalMax+len(reviewProposalMarker)+2)
	var proposal reviewProposal
	found := false
	finish := func() (reviewProposal, bool, error) {
		if limited.N == 0 {
			return reviewProposal{}, false, fmt.Errorf("review proposal raw log exceeds %d-byte completion-authority limit", maxBytes)
		}
		return proposal, found, nil
	}
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
				return finish()
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
			return finish()
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

func (d *Daemon) validateReviewProposal(project RegisteredProject, note Note, run RunStatus, proposal reviewProposal) (ReviewResult, error) {
	if strings.TrimSpace(proposal.Schema) != reviewProposalSchema || strings.TrimSpace(proposal.AttemptID) != run.ActiveAttemptID {
		return ReviewResult{}, fmt.Errorf("proposal attempt identity is not the active review attempt")
	}
	attempt, err := d.store.ReviewAttempt(run.ActiveAttemptID)
	if err != nil {
		return ReviewResult{}, err
	}
	if attempt.ProjectID != project.ProjectID || attempt.RecordID != run.RecordID || attempt.Lane != runLaneReview || attempt.WorkRevision != run.WorkRevision || attempt.Runner != run.Runner || attempt.WorkerPolicyFP != run.WorkerPolicyFP {
		return ReviewResult{}, fmt.Errorf("proposal attempt does not match the daemon-owned review run")
	}
	if run.ProjectID != project.ProjectID || run.RecordID != stringField(note.Data, "id") || run.Lane != runLaneReview || !isDispatchingLeaseState(run.LeaseState) || firstNonEmpty(run.LeaseOwner, run.ActiveAttemptID) != run.ActiveAttemptID {
		return ReviewResult{}, fmt.Errorf("proposal run lease is no longer authoritative")
	}
	result := proposal.Result
	if result.Runner != "" || result.RunnerProfile != "" || result.WorkerPolicyFP != "" || result.ResultRevision != "" {
		return ReviewResult{}, fmt.Errorf("worker proposal attempted to choose runner authority")
	}
	if result.Schema != reviewResultSchemaV2 {
		return ReviewResult{}, fmt.Errorf("worker proposal must use the authority-less review result transport schema")
	}
	canonicalProjectID, canonicalProjectErr := resolveV7ProjectID(project.VaultRoot)
	if canonicalProjectErr != nil {
		return ReviewResult{}, canonicalProjectErr
	}
	if result.ProjectID != canonicalProjectID {
		return ReviewResult{}, fmt.Errorf("proposal canonical project identity %q does not match %q", result.ProjectID, canonicalProjectID)
	}
	// The worker signs the durable vault's project identity; the resident
	// daemon stores runtime records under its registry identity. Validate the
	// former at the boundary, then bind the accepted result to the latter so
	// review-result storage, completion transactions, and receipts all use one
	// stable runtime key. The raw proposal remains the canonical audit input.
	result.ProjectID = project.ProjectID
	loaded, err := loadProjectContents(d.store, project, false)
	if err != nil {
		return ReviewResult{}, err
	}
	if !registeredProjectIdentityMatches(project, loaded.Project) {
		return ReviewResult{}, tuskerError(errorConfigInvalid, "registered project identity changed during review authority check")
	}
	wf := loaded.Workflow
	result.Schema, result.WorkerPolicyFP, err = reviewResultPolicyForRun(wf.Data, note, run)
	if err != nil {
		return ReviewResult{}, err
	}
	result.Runner, result.RunnerProfile = run.Runner, run.RunnerProfile
	if err := normalizeReviewResult(&result); err != nil {
		return ReviewResult{}, err
	}
	expectedTaskRevision := stringField(note.Data, "state_rev")
	expectedSourceSHA := firstNonEmpty(stringField(note.Data, "source_sha"), stringField(note.Data, "source_commit"))
	if result.TaskID != run.RecordID || result.AttemptID != run.ActiveAttemptID || result.WorkRevision != run.WorkRevision ||
		result.TaskStateRev != expectedTaskRevision || result.ImplementationSHA != expectedSourceSHA {
		return ReviewResult{}, fmt.Errorf(
			"proposal task/work/source snapshot drifted: project %q/%q task %q/%q attempt %q/%q work_revision %d/%d task_state_rev %q/%q source_sha %q/%q",
			result.ProjectID, project.ProjectID,
			result.TaskID, run.RecordID,
			result.AttemptID, run.ActiveAttemptID,
			result.WorkRevision, run.WorkRevision,
			result.TaskStateRev, expectedTaskRevision,
			result.ImplementationSHA, expectedSourceSHA,
		)
	}
	if result.Actor != reviewerActorForNote(wf.Data.Reviewer.Actor, note) {
		return ReviewResult{}, fmt.Errorf("proposal reviewer actor is not authorized")
	}
	proof, gates, err := reviewObjectiveSnapshots(project.VaultRoot, note)
	if err != nil || result.ProofFingerprint != proof || result.GateFingerprint != gates {
		return ReviewResult{}, fmt.Errorf("proposal proof or gate snapshot drifted")
	}
	switch result.Verdict {
	case "pass":
		want, got := sortedUniqueStrings(v7AcceptanceIDs(note.Body)), sortedUniqueStrings(result.Covers)
		if strings.Join(want, ",") != strings.Join(got, ",") {
			return ReviewResult{}, fmt.Errorf("pass proposal does not cover the exact acceptance set")
		}
		report, reportErr := loadV7ProofReport(project.VaultRoot, run.RecordID)
		if reportErr != nil || report.Status != "satisfied" || len(report.OpenGates) != 0 {
			return ReviewResult{}, fmt.Errorf("pass proposal requires currently satisfied proof and gates")
		}
	case "blocked":
		if result.Blocker == "human" {
			open, openErr := reviewHasOpenHumanBlocker(project.VaultRoot, run.RecordID)
			if openErr != nil || !open {
				return ReviewResult{}, fmt.Errorf("human blocker proposal has no open human gate")
			}
		}
	}
	return result, nil
}
