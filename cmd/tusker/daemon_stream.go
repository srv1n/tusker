package main

import "strings"

const (
	serveStreamKindPollTick         = "poll_tick"
	serveStreamKindDispatch         = "dispatch"
	serveStreamKindLeaseTransition  = "lease_transition"
	serveStreamKindTaskStatusChange = "task_status_change"
	serveStreamKindReviewBatch      = "review_batch"
)

func (d *Daemon) emitStreamEvent(kind string, keys ...string) {
	if d == nil || d.stream == nil {
		return
	}
	d.stream.Broadcast(serveStreamEvent{Kind: kind, Keys: keys})
}

func (d *Daemon) emitPollTickStreamEvent() {
	d.emitStreamEvent(serveStreamKindPollTick, "daemon", "projects", "needs", "runs", "tasks", "epics", "waves", "review:batch")
}

func (d *Daemon) emitDispatchStreamEvent(run RunStatus) {
	d.emitStreamEvent(serveStreamKindDispatch, serveStreamRunKeys(firstNonEmpty(run.RecordID, run.ItemID))...)
}

func (d *Daemon) emitLeaseTransitionStreamEvent(before, after RunStatus) {
	if !serveStreamRunChanged(before, after) {
		return
	}
	d.emitStreamEvent(serveStreamKindLeaseTransition, serveStreamRunKeys(firstNonEmpty(after.RecordID, before.RecordID, after.ItemID, before.ItemID))...)
}

func (d *Daemon) upsertRunWithStream(before, after RunStatus) error {
	if d == nil || d.store == nil {
		return nil
	}
	if err := d.store.UpsertRun(after); err != nil {
		return err
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
	key := projectID + "\x00" + recordID
	previous, seen := d.pollTaskStatuses[key]
	d.pollTaskStatuses[key] = status
	if !seen || previous == status {
		return
	}
	d.emitStreamEvent(serveStreamKindTaskStatusChange, serveStreamTaskKeys(recordID)...)
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
