package main

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

const directRunDirectiveTTL = 7 * 24 * time.Hour

type directTaskRouteInspector func(task Note, lane string) error

func directTaskRouteInspectorForWorkflow(wf Workflow) directTaskRouteInspector {
	return func(task Note, lane string) error {
		if blockers := routePreviewForNote(task, wf, lane).Blockers; len(blockers) > 0 {
			return errors.New(strings.Join(blockers, "; "))
		}
		return nil
	}
}

func directTaskRouteInspectorForVault(vaultPath string) directTaskRouteInspector {
	wf, err := loadWorkflow(vaultPath)
	if err != nil {
		return unavailableDirectTaskRouteInspector(err)
	}
	return directTaskRouteInspectorForWorkflow(wf.Data)
}

func unavailableDirectTaskRouteInspector(workflowErr error) directTaskRouteInspector {
	message := "workflow route resolution unavailable"
	if workflowErr != nil {
		message += ": " + runnerRouteBlocker(workflowErr)
	}
	message += "; repair WORKFLOW.md and retry"
	return func(Note, string) error { return errors.New(message) }
}

// directRouteBlockers is the last read-only route check before durable
// directives are queued. It deliberately reloads workflow/configuration after
// the wave authorization decision, so a route removed after authorization
// cannot reach the daemon through an already authorized wave.
func directRouteBlockers(vaultPath string, idx v7Index, taskIDs []string) []string {
	if len(taskIDs) == 0 {
		return nil
	}
	return directRouteBlockersWithInspector(idx, taskIDs, directTaskRouteInspectorForVault(vaultPath))
}

func directRouteBlockersWithInspector(idx v7Index, taskIDs []string, inspector directTaskRouteInspector) []string {
	if len(taskIDs) == 0 {
		return nil
	}
	if inspector == nil {
		return []string{"route resolution is unavailable; repair WORKFLOW.md and retry"}
	}
	var blockers []string
	for _, taskID := range taskIDs {
		task, ok := idx.Tasks[taskID]
		if !ok {
			blockers = append(blockers, taskID+": task no longer resolves; refresh the wave and review its membership")
			continue
		}
		for _, lane := range []string{runLaneExecute, runLaneReview} {
			if err := inspector(task, lane); err != nil {
				blockers = append(blockers, fmt.Sprintf("%s: %s route blocked: %s", taskID, lane, runnerRouteBlocker(err)))
			}
		}
	}
	return blockers
}

// A task whose stored contract pin no longer matches its current bytes (or
// whose record was rewritten outside the CAS ledger) is never treated as
// authorized, regardless of what a queued directive or stored run
// authorization claims. Rebinding the pin with `tusker task update` restores
// admission under the new contract.
func runDirectiveMatchesTaskAuthority(vaultPath string, task Note, directive *RunDirective, now time.Time) bool {
	if !runDirectiveActive(directive, now) || directWaveTaskContractStaleReason(task) != "" {
		return false
	}
	if directive.WaveID == "" {
		if directive.AuthorizationFingerprint == "" {
			return true
		}
		return directWaveTaskContract(task) == directive.AuthorizationFingerprint
	}
	if directive.WaveID == "" || directive.AuthorizationFingerprint == "" || directive.WaveAuthorizedAt == "" || stringField(task.Data, "wave") != directive.WaveID {
		return false
	}
	wave, _, armed := armedWaveForTask(vaultPath, task)
	return armed && stringField(wave.Data, "authorization_fingerprint") == directive.AuthorizationFingerprint && stringField(wave.Data, "authorized_at") == directive.WaveAuthorizedAt
}

func runDirectiveAuthorizationMatchesTaskAuthority(vaultPath string, task Note, run RunStatus, auth *RunAuthorization) bool {
	if auth == nil || auth.Source != "human_run_directive" || auth.LeaseGeneration != run.LeaseGeneration || directWaveTaskContractStaleReason(task) != "" {
		return false
	}
	if auth.DirectiveWaveID == "" {
		if auth.DirectiveAuthorizationFingerprint == "" {
			return true
		}
		return auth.DirectiveAuthorizationFingerprint == directWaveTaskContract(task)
	}
	if auth.DirectiveAuthorizationFingerprint == "" || auth.DirectiveWaveAuthorizedAt == "" || stringField(task.Data, "wave") != auth.DirectiveWaveID {
		return false
	}
	wave, _, armed := armedWaveForTask(vaultPath, task)
	return armed && stringField(wave.Data, "authorization_fingerprint") == auth.DirectiveAuthorizationFingerprint && stringField(wave.Data, "authorized_at") == auth.DirectiveWaveAuthorizedAt
}

func consumedRunDirectiveMatchesTaskAuthority(vaultPath string, task Note, run RunStatus, directive *RunDirective, auth *RunAuthorization, now time.Time) bool {
	if directive == nil || directive.State != "consumed" || auth == nil || auth.Source != "human_run_directive" ||
		auth.LeaseGeneration != run.LeaseGeneration || auth.Actor != directive.Actor || directWaveTaskContractStaleReason(task) != "" {
		return false
	}
	expiresAt, err := time.Parse(time.RFC3339Nano, directive.ExpiresAt)
	if err != nil || !expiresAt.After(now) {
		return false
	}
	if directive.WaveID == "" {
		if directive.AuthorizationFingerprint == "" {
			return true
		}
		return directWaveTaskContract(task) == directive.AuthorizationFingerprint
	}
	if directive.WaveID == "" || directive.AuthorizationFingerprint == "" || directive.WaveAuthorizedAt == "" || stringField(task.Data, "wave") != directive.WaveID {
		return false
	}
	wave, _, armed := armedWaveForTask(vaultPath, task)
	return armed && stringField(wave.Data, "authorization_fingerprint") == directive.AuthorizationFingerprint && stringField(wave.Data, "authorized_at") == directive.WaveAuthorizedAt
}

func admittedWaveAuthorizationForTask(vaultPath string, task Note) (Note, bool) {
	waveID := stringField(task.Data, "wave")
	if waveID == "" {
		return Note{}, false
	}
	idx, err := loadV7Index(vaultPath)
	if err != nil {
		return Note{}, false
	}
	wave, ok := idx.Waves[waveID]
	if !ok {
		return Note{}, false
	}
	switch strings.ToLower(strings.TrimSpace(stringField(wave.Data, "authorization"))) {
	case "armed", "paused":
		return wave, true
	default:
		return Note{}, false
	}
}

func runDirectiveAdmittedContinuityMatchesTaskAuthority(vaultPath string, task Note, run RunStatus, auth *RunAuthorization) bool {
	if auth == nil || auth.Source != "human_run_directive" || auth.LeaseGeneration != run.LeaseGeneration || directWaveTaskContractStaleReason(task) != "" {
		return false
	}
	if auth.DirectiveWaveID == "" {
		if auth.DirectiveAuthorizationFingerprint == "" {
			return true
		}
		return auth.DirectiveAuthorizationFingerprint == directWaveTaskContract(task)
	}
	if auth.DirectiveAuthorizationFingerprint == "" || auth.DirectiveWaveAuthorizedAt == "" || stringField(task.Data, "wave") != auth.DirectiveWaveID {
		return false
	}
	wave, ok := admittedWaveAuthorizationForTask(vaultPath, task)
	return ok && stringField(wave.Data, "authorization_fingerprint") == auth.DirectiveAuthorizationFingerprint && stringField(wave.Data, "authorized_at") == auth.DirectiveWaveAuthorizedAt
}

func consumedRunDirectiveContinuityMatchesTaskAuthority(vaultPath string, task Note, run RunStatus, directive *RunDirective, auth *RunAuthorization, now time.Time) bool {
	if directive == nil || directive.State != "consumed" || auth == nil || auth.Source != "human_run_directive" ||
		auth.LeaseGeneration != run.LeaseGeneration || auth.Actor != directive.Actor || directWaveTaskContractStaleReason(task) != "" {
		return false
	}
	expiresAt, err := time.Parse(time.RFC3339Nano, directive.ExpiresAt)
	if err != nil || !expiresAt.After(now) {
		return false
	}
	if directive.WaveID == "" {
		if directive.AuthorizationFingerprint == "" {
			return true
		}
		return directWaveTaskContract(task) == directive.AuthorizationFingerprint
	}
	if directive.WaveID == "" || directive.AuthorizationFingerprint == "" || directive.WaveAuthorizedAt == "" || stringField(task.Data, "wave") != directive.WaveID {
		return false
	}
	wave, ok := admittedWaveAuthorizationForTask(vaultPath, task)
	return ok && stringField(wave.Data, "authorization_fingerprint") == auth.DirectiveAuthorizationFingerprint && stringField(wave.Data, "authorized_at") == auth.DirectiveWaveAuthorizedAt
}
