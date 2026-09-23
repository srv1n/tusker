package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

type runnerExitClassification struct {
	outcome      AttemptOutcome
	exitCode     int
	reason       string
	reasonCode   string
	trackerState string
}

func classifyRunnerProcessExit(run RunStatus, status runnerProcessStatus, note Note, vaultPath string, activeStates []string) runnerExitClassification {
	trackerState := strings.TrimSpace(stringField(note.Data, "status"))
	if status.ReasonCode != "" {
		if spec, ok := runFailureReason(RunFailureReasonCode(status.ReasonCode)); ok {
			return runnerExitClassification{outcome: spec.Outcome, exitCode: status.ExitCode, reason: firstNonEmpty(strings.TrimSpace(status.Reason), spec.Guidance), reasonCode: status.ReasonCode, trackerState: trackerState}
		}
		return runnerExitClassification{outcome: AttemptOutcomeUnknown, exitCode: status.ExitCode, reason: "invalid runner reason code: " + status.ReasonCode, trackerState: trackerState}
	}
	if AttemptOutcome(strings.TrimSpace(status.Outcome)) == AttemptOutcomeUnknown {
		return runnerExitClassification{
			outcome:      AttemptOutcomeUnknown,
			exitCode:     status.ExitCode,
			reason:       firstNonEmpty(strings.TrimSpace(status.Reason), "runner outcome is unknown"),
			trackerState: trackerState,
		}
	}
	if status.ExitCode != 0 {
		return runnerExitClassification{
			outcome:      AttemptOutcomeFailed,
			exitCode:     status.ExitCode,
			reason:       firstNonEmpty(strings.TrimSpace(status.Reason), fmt.Sprintf("runner exited with code %d", status.ExitCode)),
			trackerState: trackerState,
		}
	}

	if AttemptOutcome(strings.TrimSpace(status.Outcome)) == AttemptOutcomeTurnCapExhausted && runnerTrackerStateActive(trackerState, activeStates) && run.Lane != runLaneReview {
		reason := firstNonEmpty(strings.TrimSpace(status.Reason), fmt.Sprintf("turn cap exhausted for attempt %s", run.ActiveAttemptID))
		return runnerExitClassification{outcome: AttemptOutcomeTurnCapExhausted, exitCode: 0, reason: reason, trackerState: trackerState}
	}
	if run.Lane != runLaneReview {
		if wait, reason := v7MachineCompleteWaitingForHuman(vaultPath, note); wait {
			return runnerExitClassification{outcome: AttemptOutcomeWaitingForHuman, exitCode: 0, reason: reason, trackerState: trackerState}
		}
	}
	if runnerTrackerStateActive(trackerState, activeStates) && run.Lane != runLaneReview {
		return runnerExitClassification{outcome: AttemptOutcomeEarlyExit, exitCode: 0, reason: runnerEarlyExitActiveTrackerReason, trackerState: trackerState}
	}
	return runnerExitClassification{outcome: AttemptOutcomeSucceeded, exitCode: 0, trackerState: trackerState}
}

func runnerTrackerStateActive(status string, activeStates []string) bool {
	if len(activeStates) == 0 {
		activeStates = []string{"ready", "rework"}
	}
	return containsString(activeStates, strings.TrimSpace(status))
}

func runnerProcessFinishedAt(status runnerProcessStatus) string {
	return firstNonEmpty(status.CompletedAt, time.Now().UTC().Format(time.RFC3339))
}

func resolveRunnerExitNote(req StartRequest) (Note, error) {
	notePath := strings.TrimSpace(req.NotePath)
	if notePath != "" {
		raw, err := readText(notePath)
		if err != nil {
			return Note{}, err
		}
		data, body, err := parseFrontmatter(raw)
		if err != nil {
			return Note{}, err
		}
		note := Note{AbsolutePath: notePath, Data: data, Body: body}
		if vaultPath := strings.TrimSpace(req.VaultPath); vaultPath != "" {
			if rel, err := filepath.Rel(vaultPath, notePath); err == nil {
				note.RelativePath = filepath.ToSlash(rel)
			}
		}
		return note, nil
	}
	if vaultPath := strings.TrimSpace(req.VaultPath); vaultPath != "" {
		return resolveNote(vaultPath, req.RecordID)
	}
	return Note{}, tuskerError(errorInvalidArg, "runner exit classification requires a note path or vault path")
}
