package main

import "fmt"

// validateDeliveryPlanAmendment runs for both preview and commit. Resetting a
// task's Markdown to backlog/held cannot erase its independently retained runtime
// history. The caller holds the material epoch lock when committing an import.
func validateDeliveryPlanAmendment(vaultPath string, wave map[string]any, fingerprint string, idx v7Index) error {
	if stringField(wave, "delivery_plan_fingerprint") == fingerprint {
		return nil
	}
	frozen := deliveryWaveContractFrozen(wave, idx)
	if !frozen {
		var err error
		frozen, err = deliveryMembersHaveRuntimeHistory(vaultPath, normalizeList(wave["members"]))
		if err != nil {
			return fmt.Errorf("cannot verify delivery amendment execution history: %w", err)
		}
	}
	if frozen {
		return tuskerError(errorInvalidTransition, "existing delivery scope is frozen to a different reviewed plan; use a new plan scope/wave or perform an explicit controlled rebase")
	}
	return nil
}

func deliveryMembersHaveRuntimeHistory(vaultPath string, members []string) (bool, error) {
	store, missing, err := openRuntimeStoreReadOnly(DefaultStateRoot())
	if err != nil || missing {
		return false, err
	}
	defer store.Close()
	projectID, registered, err := registeredProjectIDForVault(store, vaultPath)
	if err != nil || !registered {
		return false, err
	}
	for _, taskID := range members {
		var history bool
		// Match item_id as well as record_id: review/fanout children retain a
		// separate run identity. Existence, not JSON validity or success, freezes
		// history; corrupt results must never authorize rewriting the contract.
		// The run row also covers a claimed attempt before its first snapshot,
		// and histories whose detailed attempt rows have been retired.
		err := store.queryRowScan(`SELECT
			EXISTS(SELECT 1 FROM attempts WHERE project_id=? AND (record_id=? OR item_id=?)) OR
			EXISTS(SELECT 1 FROM review_results WHERE project_id=? AND task_id=?) OR
			EXISTS(SELECT 1 FROM runs WHERE project_id=? AND (record_id=? OR item_id=?) AND
				(attempt_count>0 OR lease_generation>0 OR active_attempt_id<>''))`,
			[]any{projectID, taskID, taskID, projectID, taskID, projectID, taskID, taskID}, &history)
		if err != nil {
			return false, err
		}
		if history {
			return true, nil
		}
	}
	return false, nil
}
