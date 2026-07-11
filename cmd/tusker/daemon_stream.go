package main

import "strings"

const (
	serveStreamKindPollTick         = "poll_tick"
	serveStreamKindDispatch         = "dispatch"
	serveStreamKindLeaseTransition  = "lease_transition"
	serveStreamKindTaskStatusChange = "task_status_change"
	serveStreamKindReviewBatch      = "review_batch"
)

type serveStreamTaskMeta struct {
	Project string
	TaskID  string
	Title   string
	Status  string
}

func (d *Daemon) streamTaskMeta(projectID, recordID string) serveStreamTaskMeta {
	if d != nil && d.pollTaskMeta != nil {
		if meta, ok := d.pollTaskMeta[projectID+"\x00"+recordID]; ok {
			return meta
		}
	}
	return serveStreamTaskMeta{Project: projectID, TaskID: recordID}
}

func serveStreamDeepLink(meta serveStreamTaskMeta) string {
	if meta.Project == "" || meta.TaskID == "" {
		return ""
	}
	return "/p/" + meta.Project + "/work?task=" + meta.TaskID
}

func (d *Daemon) streamEvent(kind string, keys []string, meta serveStreamTaskMeta, urgency string) serveStreamEvent {
	return serveStreamEvent{Kind: kind, Keys: keys, Project: meta.Project, TaskID: meta.TaskID, Title: meta.Title, Status: meta.Status, Urgency: urgency, DeepLinkPath: serveStreamDeepLink(meta)}
}

func (d *Daemon) emitProjectStreamEvent(projectID, kind string, keys ...string) {
	if d == nil || d.stream == nil {
		return
	}
	d.stream.Broadcast(d.streamEvent(kind, keys, serveStreamTaskMeta{Project: projectID}, ""))
}

func (d *Daemon) emitDispatchStreamEvent(run RunStatus) {
	recordID := firstNonEmpty(run.RecordID, run.ItemID)
	if d != nil && d.serve != nil {
		d.serve.refreshProjectSnapshot(run.ProjectID)
	}
	meta := d.streamTaskMeta(run.ProjectID, recordID)
	d.stream.Broadcast(d.streamEvent("run_started", serveStreamRunKeys(recordID), meta, "info"))
}

func (d *Daemon) emitLeaseTransitionStreamEvent(before, after RunStatus) {
	if !serveStreamRunChanged(before, after) {
		return
	}
	recordID := firstNonEmpty(after.RecordID, before.RecordID, after.ItemID, before.ItemID)
	if d != nil && d.serve != nil {
		d.serve.refreshProjectSnapshot(firstNonEmpty(after.ProjectID, before.ProjectID))
	}
	meta := d.streamTaskMeta(firstNonEmpty(after.ProjectID, before.ProjectID), recordID)
	kind, urgency := serveStreamKindLeaseTransition, "info"
	if after.AttemptOutcome == string(AttemptOutcomeFailed) || after.AttemptOutcome == string(AttemptOutcomeBudgetExceeded) {
		kind, urgency = "run_failed", "attention"
	} else if after.AttemptOutcome == string(AttemptOutcomeSucceeded) {
		kind = "run_completed"
	}
	d.stream.Broadcast(d.streamEvent(kind, serveStreamRunKeys(recordID), meta, urgency))
}

func (d *Daemon) upsertRunWithStream(before, after RunStatus) error {
	if d == nil || d.store == nil {
		return nil
	}
	after = normalizeRuntimeRunWrite(after)
	if strings.TrimSpace(before.ProjectID) != "" || strings.TrimSpace(before.RecordID) != "" {
		before = normalizeRuntimeRunWrite(before)
	}
	if d.beforePollRunPersist != nil {
		d.beforePollRunPersist(before, after)
	}
	updated, err := d.store.UpsertRunIfSnapshot(before, after)
	if err != nil {
		return err
	}
	if !updated {
		if strings.TrimSpace(before.ProjectID) == "" && strings.TrimSpace(before.RecordID) == "" {
			context := map[string]any{"insert_conflict": true}
			changedSummary := ""
			if current, loadErr := d.store.FindRun(after.RecordID); loadErr == nil && current != nil {
				changed := runtimeRunChangedColumns(after, *current)
				context["not_applied_columns"] = changed
				if len(changed) > 0 {
					changedSummary = " (not applied: " + strings.Join(changed, ", ") + ")"
				}
			}
			return tuskerError("CAS_CONFLICT", "run changed while daemon poll was applying its snapshot: "+firstNonEmpty(after.ItemID, after.RecordID)+changedSummary, withHint("the daemon left the concurrently created runtime row untouched and will reconcile it on the next poll"), withContext(context))
		}
		alreadyApplied, err := d.store.RunMatchesSnapshot(after)
		if err != nil {
			return err
		}
		if !alreadyApplied {
			context := map[string]any{}
			changedSummary := ""
			if current, loadErr := d.store.FindRun(after.RecordID); loadErr == nil && current != nil {
				if runtimeRunChangesCompatible(before, after, *current) {
					retried, retryErr := d.store.UpsertRunIfSnapshot(*current, after)
					if retryErr != nil {
						return retryErr
					}
					if retried {
						d.emitLeaseTransitionStreamEvent(before, after)
						return nil
					}
				}
				changed := runtimeRunChangedColumns(before, *current)
				context["changed_columns"] = changed
				context["not_applied_columns"] = runtimeRunChangedColumns(after, *current)
				if len(changed) > 0 {
					changedSummary = " (changed: " + strings.Join(changed, ", ") + ")"
				}
				if notApplied := runtimeRunChangedColumns(after, *current); len(notApplied) > 0 {
					changedSummary += " (not applied: " + strings.Join(notApplied, ", ") + ")"
				}
			}
			return tuskerError("CAS_CONFLICT", "run changed while daemon poll was applying its snapshot: "+firstNonEmpty(after.ItemID, after.RecordID)+changedSummary, withHint("the daemon left the newer runtime row untouched and will reconcile it on the next poll"), withContext(context))
		}
	}
	d.emitLeaseTransitionStreamEvent(before, after)
	return nil
}

func (d *Daemon) observeTaskStatusForStream(projectID, recordID, status string) {
	if d == nil {
		return
	}
	projectID = strings.TrimSpace(projectID)
	recordID = strings.TrimSpace(recordID)
	status = strings.TrimSpace(status)
	if projectID == "" || recordID == "" {
		return
	}
	if d.pollTaskStatuses == nil {
		d.pollTaskStatuses = map[string]string{}
	}
	if d.pollTaskMeta == nil {
		d.pollTaskMeta = map[string]serveStreamTaskMeta{}
	}
	key := projectID + "\x00" + recordID
	previous, seen := d.pollTaskStatuses[key]
	d.pollTaskStatuses[key] = status
	if !seen || previous == status {
		return
	}
	if d.serve != nil {
		d.serve.refreshProjectSnapshot(projectID)
	}
	meta := d.streamTaskMeta(projectID, recordID)
	kind, urgency := "task_status_changed", "info"
	switch strings.ToLower(status) {
	case "review":
		kind, urgency = "task_review", "attention"
	case "waiting_on_human", "waiting-on-human", "blocked":
		kind, urgency = "task_waiting_human", "critical"
	}
	meta.Status = status
	d.stream.Broadcast(d.streamEvent(kind, serveStreamTaskKeys(recordID), meta, urgency))
}

func serveStreamRunChanged(before, after RunStatus) bool {
	if strings.TrimSpace(before.ProjectID) == "" && strings.TrimSpace(before.RecordID) == "" && strings.TrimSpace(before.LeaseState) == "" {
		return false
	}
	return before.LeaseState != after.LeaseState ||
		before.AttemptOutcome != after.AttemptOutcome ||
		before.LastEventAt != after.LastEventAt ||
		before.LastHeartbeatAt != after.LastHeartbeatAt ||
		before.UpdatedAt != after.UpdatedAt
}
