package main

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// v7ImplicitSingletonDeliveryUnit is deliberately a normal, disarmed wave
// record.  It reuses the landing lane but is never a dispatch authorization.
// Keeping that distinction here prevents scheduled landing from widening into
// implementation or release authority.
const v7ImplicitSingletonDeliveryUnit = "implicit_singleton"

func implicitSingletonDeliveryEnabled(vaultPath string) (ScheduledPromotionProjection, bool, error) {
	wf, err := loadWorkflow(vaultPath)
	if err != nil {
		var refusal *TuskerError
		if errors.As(err, &refusal) && refusal.Code == errorNotFound {
			return scheduledPromotionProjection(defaultScheduledPromotionPolicy(), false, "migration default (WORKFLOW.md absent)"), false, nil
		}
		return ScheduledPromotionProjection{}, false, err
	}
	policy := wf.Data.ScheduledPromotion.Effective
	return policy, policy.Stage && (policy.Mode == scheduledPromotionStage || policy.Mode == scheduledPromotionPromote), nil
}

// ensureV7ImplicitSingletonDeliveryUnit gives a reviewed standalone task the
// same staging boundary as a wave. Disabled and shadow policy are hard no-ops.
// The task's source state revision is retained as a binding record, while the
// existing landing source checks remain responsible for commit provenance.
func ensureV7ImplicitSingletonDeliveryUnit(vaultPath, taskID string, args Args) (string, bool, error) {
	policy, enabled, err := implicitSingletonDeliveryEnabled(vaultPath)
	if err != nil || !enabled {
		return "", false, err
	}
	idx, err := loadV7Index(vaultPath)
	if err != nil {
		return "", false, err
	}
	taskID = strings.ToUpper(strings.TrimSpace(taskID))
	task, ok := idx.Tasks[taskID]
	if !ok {
		return "", false, tuskerError(errorNotFound, "V7 task not found: "+taskID)
	}
	if waveID := stringField(task.Data, "wave"); waveID != "" {
		return waveID, false, nil // explicit waves (including multi-member) stay untouched.
	}
	status := strings.ToLower(stringField(task.Data, "status"))
	if status != "review" && status != "done" {
		return "", false, tuskerError(errorInvalidTransition, taskID+" must complete implementation and enter review before scheduled staging")
	}
	for id, wave := range idx.Waves {
		if stringField(wave.Data, "delivery_unit") != v7ImplicitSingletonDeliveryUnit || stringField(wave.Data, "delivery_task") != taskID {
			continue
		}
		if members := normalizeList(wave.Data["members"]); len(members) != 1 || members[0] != taskID {
			return "", false, tuskerError(errorInvalidTransition, "implicit delivery unit "+id+" no longer binds exactly to "+taskID)
		}
		if err := bindV7TaskToDeliveryUnit(vaultPath, task, id, args); err != nil {
			return "", false, err
		}
		return id, false, nil
	}

	// Creation spans the wave and its task back-pointer. Keep the material
	// epoch held while reloading both records and commit them through the same
	// guarded transaction used by delivery import.
	materialLock, err := acquireV7MaterialEpochLock(vaultPath)
	if err != nil {
		return "", false, err
	}
	defer func() { _ = materialLock.Close() }()
	idx, err = loadV7Index(vaultPath)
	if err != nil {
		return "", false, err
	}
	task, ok = idx.Tasks[taskID]
	if !ok {
		return "", false, tuskerError(errorNotFound, "V7 task not found: "+taskID)
	}
	if waveID := stringField(task.Data, "wave"); waveID != "" {
		return waveID, false, nil
	}
	if err := ensureDeliveryWorkNamespaces(vaultPath); err != nil {
		return "", false, err
	}
	id := nextV7WaveID(vaultPath)
	branch := v7IntegrationBranchName(id)
	if err := ensureV7IntegrationBranch(vaultPath, branch); err != nil {
		return "", false, err
	}
	taskLock, err := acquireV7DocumentLock(task.AbsolutePath, v7DocumentLockTimeout)
	if err != nil {
		return "", false, err
	}
	defer func() { _ = taskLock.Close() }()
	currentTaskData, currentTaskBody, err := parseFrontmatterMustRead(task.AbsolutePath)
	if err != nil {
		return "", false, err
	}
	if waveID := stringField(currentTaskData, "wave"); waveID != "" {
		return waveID, false, nil
	}
	status = strings.ToLower(stringField(currentTaskData, "status"))
	if status != "review" && status != "done" {
		return "", false, tuskerError(errorInvalidTransition, taskID+" must complete implementation and enter review before scheduled staging")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	actor := landV7Actor(args)
	body := fmt.Sprintf("# Scheduled staging · %s\n\nThis internal delivery unit stages one reviewed task through the configured landing policy.\n", taskID)
	data := map[string]any{
		"schema": "tusker.wave/v7", "kind": "wave", "id": id, "project": v7ProjectID(vaultPath),
		"title": "Scheduled staging", "status": "open", "authorization": "disarmed", "members": []string{taskID},
		"integration_branch": branch, "delivery_unit": v7ImplicitSingletonDeliveryUnit, "delivery_task": taskID,
		"delivery_source_state_rev": stringField(currentTaskData, "state_rev"), "delivery_policy_mode": policy.Mode,
		"execution_provenance": "inherited", "release_authorized": false,
		"created_at": now, "created_by": actor, "updated_at": now, "updated_by": actor,
	}
	data["state_rev"] = v7StateRev(data, body)
	wavePath := filepath.Join(vaultPath, "work", "waves", id+".md")
	waveContent, err := serializeDocument(data, body, v7FrontmatterOrder["wave"])
	if err != nil {
		return "", false, err
	}
	currentTaskData["wave"] = id
	currentTaskData["updated_at"] = now
	currentTaskData["updated_by"] = actor
	currentTaskData["state_rev"] = v7StateRev(currentTaskData, currentTaskBody)
	taskContent, err := serializeDocument(currentTaskData, currentTaskBody, v7FrontmatterOrder["task"])
	if err != nil {
		return "", false, err
	}
	if err := commitDeliveryWritesGuardedWithLocks(map[string]string{wavePath: waveContent, task.AbsolutePath: taskContent}, 0, nil, []*v7DocumentLock{materialLock, taskLock}); err != nil {
		return "", false, err
	}
	if err := emitV7Event(vaultPath, id, "wave", "implicit_delivery_unit_created", actor, map[string]any{"task": taskID, "policy_mode": policy.Mode}); err != nil {
		return "", false, err
	}
	return id, true, nil
}

func bindV7TaskToDeliveryUnit(vaultPath string, task Note, waveID string, args Args) error {
	if current := stringField(task.Data, "wave"); current == waveID {
		return nil
	} else if current != "" {
		return tuskerError(errorInvalidTransition, "task "+trackerRecordID(task)+" already belongs to explicit wave "+current)
	}
	baseRev := stringField(task.Data, "state_rev")
	data, body, err := parseFrontmatterMustRead(task.AbsolutePath)
	if err != nil {
		return err
	}
	data["wave"] = waveID
	data["updated_at"] = time.Now().UTC().Format(time.RFC3339)
	data["updated_by"] = landV7Actor(args)
	_, err = saveV7DocumentCAS(task.AbsolutePath, data, body, v7FrontmatterOrder["task"], baseRev)
	return err
}

func v7ImplicitDeliveryUnit(wave Note) bool {
	return stringField(wave.Data, "delivery_unit") == v7ImplicitSingletonDeliveryUnit
}
