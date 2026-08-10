package main

import (
	"context"
	"fmt"
	"strings"
	"time"
)

func canonicalStatusRetiresRuntimeRows(wf Workflow, status string) bool {
	status = strings.TrimSpace(status)
	if status == "backlog" {
		return true
	}
	return trackerStateTerminal(wf, status)
}

func runtimeRunNeedsTerminalRetirement(run RunStatus) bool {
	if !run.Terminal {
		return true
	}
	return LeaseState(strings.TrimSpace(run.LeaseState)) != LeaseStateReleased
}

func canonicalRuntimeRetirementReason(source, status string) string {
	status = firstNonEmpty(strings.TrimSpace(status), "unknown")
	source = strings.TrimSpace(source)
	if source == "" {
		return "canonical status " + status
	}
	return source + ": canonical status " + status
}

func retireCanonicalRuntimeRowsForTask(vaultPath, taskID, status, actor, source string) (int, error) {
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		return 0, err
	}
	defer store.Close()
	projectID, ok, err := registeredProjectIDForVault(store, vaultPath)
	if err != nil || !ok {
		return 0, err
	}
	return retireCanonicalRuntimeRows(store, DefaultStateRoot(), projectID, taskID, status, actor, source, time.Now().UTC())
}

// preflightCanonicalRuntimeRetirement refuses a terminal task mutation while
// its canonical runner is still alive. This runs before task/gate CAS writes;
// retirement rechecks again immediately before clearing process identity.
func preflightCanonicalRuntimeRetirement(vaultPath, taskID string) error {
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		return err
	}
	defer store.Close()
	projectID, ok, err := registeredProjectIDForVault(store, vaultPath)
	if err != nil || !ok {
		return err
	}
	run, err := store.FindRunScoped(projectID, taskID)
	if err != nil || run == nil {
		return err
	}
	if runtimeRunNeedsTerminalRetirement(*run) && runProcessGroupAlive(*run) {
		return tuskerError("LIVE_RUNNER_RETIREMENT_REFUSED", "runner is still live; stop it before changing terminal task state", withHint(fmt.Sprintf("tusker runs interrupt --project %s --id %s", projectID, run.RecordID)), withContext(map[string]any{"project_id": projectID, "record_id": run.RecordID, "pid": run.ProcessPID, "pgid": run.ProcessPGID}))
	}
	return nil
}

func registeredProjectIDForVault(store *RuntimeStore, vaultPath string) (string, bool, error) {
	// Canonical terminal state must retire runtime rows even when automation is
	// disabled for the project. Disabled controls polling, not lifecycle truth.
	projects, err := loadRegisteredProjects(store, registeredProjectLoadOptions{MetadataOnly: true, LoadDisabled: true})
	if err != nil {
		return "", false, err
	}
	for _, loaded := range projects {
		project := loaded.Project
		if sameCanonicalProjectPath(project.VaultRoot, vaultPath) {
			return project.ProjectID, true, nil
		}
	}
	return "", false, nil
}

func retireCanonicalRuntimeRows(store *RuntimeStore, stateRoot, projectID, taskID, status, actor, source string, now time.Time) (int, error) {
	if store == nil {
		return 0, tuskerError(errorConfigInvalid, "runtime store is required")
	}
	runs, err := store.ListRuns()
	if err != nil {
		return 0, err
	}
	reason := canonicalRuntimeRetirementReason(source, status)
	changed := 0
	for _, run := range runs {
		if run.ProjectID != projectID {
			continue
		}
		if run.RecordID != taskID && run.ItemID != taskID {
			continue
		}
		if !runtimeRunNeedsTerminalRetirement(run) {
			continue
		}
		if runProcessGroupAlive(run) {
			return changed, tuskerError("LIVE_RUNNER_RETIREMENT_REFUSED", "runner is still live; stop it before terminal retirement", withHint(fmt.Sprintf("tusker runs interrupt --project %s --id %s", projectID, run.RecordID)))
		}
		if _, err := retireRuntimeRun(store, stateRoot, run, actor, reason, now, true); err != nil {
			return changed, err
		}
		changed++
	}
	return changed, nil
}

func (d *Daemon) retireCanonicalRuntimeRun(ctx context.Context, project RegisteredProject, run RunStatus, status, actor, source string) (RunStatus, bool, error) {
	if d == nil || d.store == nil {
		return run, false, tuskerError(errorConfigInvalid, "daemon runtime store is required")
	}
	if !runtimeRunNeedsTerminalRetirement(run) {
		return run, false, nil
	}
	reason := canonicalRuntimeRetirementReason(source, status)
	if strings.Contains(run.LastError, "automation plan do_not_dispatch") {
		reason += "; " + run.LastError
	}
	if isDispatchingLeaseState(run.LeaseState) {
		if interrupted, err := d.stopRunExecution(ctx, run); err != nil {
			return run, false, fmt.Errorf("refusing runtime retirement after stop failure: %w", err)
		} else if interrupted {
			reason += "; daemon stopped active execution"
		}
		if runProcessGroupAlive(run) {
			return run, false, tuskerError("LIVE_RUNNER_RETIREMENT_REFUSED", "runner remained live after stop; runtime identity was preserved", withHint(fmt.Sprintf("tusker runs interrupt --project %s --id %s", project.ProjectID, run.RecordID)))
		}
	}
	if runProcessGroupAlive(run) {
		return run, false, tuskerError("LIVE_RUNNER_RETIREMENT_REFUSED", "runner is live; runtime identity was preserved", withHint(fmt.Sprintf("tusker runs interrupt --project %s --id %s", project.ProjectID, run.RecordID)))
	}
	retired, err := retireRuntimeRun(d.store, d.stateRoot, run, actor, reason, time.Now().UTC(), true)
	if err != nil {
		return run, false, err
	}
	return retired, true, nil
}
