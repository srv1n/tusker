package main

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

type AgentWakeup struct {
	ID, ProjectID, RecipientKind, RecipientID, Reason, State, IdempotencyKey, CreatedAt string
	ClaimID, ClaimedAt                                                                  string
	MessageIDs                                                                          []string
	ModelTurns                                                                          int
}

func (s *RuntimeStore) ListQueuedAgentWakeups(project string) ([]AgentWakeup, error) {
	rows, err := s.query(`SELECT id,project_id,recipient_kind,recipient_id,reason,message_ids_json,state,model_turns,idempotency_key,created_at,claim_id,claimed_at FROM agent_wakeups WHERE (state IN ('queued','held','unsupported') OR (state='delivering' AND claimed_at<?)) AND (?='' OR project_id=?) ORDER BY created_at,id`, time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano), project, project)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AgentWakeup{}
	for rows.Next() {
		var w AgentWakeup
		var raw string
		if err := rows.Scan(&w.ID, &w.ProjectID, &w.RecipientKind, &w.RecipientID, &w.Reason, &raw, &w.State, &w.ModelTurns, &w.IdempotencyKey, &w.CreatedAt, &w.ClaimID, &w.ClaimedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(raw), &w.MessageIDs)
		out = append(out, w)
	}
	return out, rows.Err()
}

func (s *RuntimeStore) SetAgentWakeupState(id, state string) error {
	if state != "delivered" && state != "scheduled" && state != "held" && state != "unsupported" && state != "stale" && state != "uncertain" {
		return errors.New("invalid wakeup state")
	}
	_, err := s.exec(`UPDATE agent_wakeups SET state=? WHERE id=?`, state, id)
	return err
}

func (d *Daemon) processAgentWakeups(project string) error {
	wakeups, err := d.store.ListQueuedAgentWakeups(project)
	if err != nil {
		return err
	}
	for _, w := range wakeups {
		if len(w.MessageIDs) != 1 {
			_ = d.store.SetAgentWakeupState(w.ID, "stale")
			continue
		}
		claimID := d.store.ClaimAgentWakeup(w.ID)
		if claimID == "" {
			continue
		}
		message, err := d.store.AgentMessage(w.ProjectID, w.MessageIDs[0])
		if err != nil {
			_ = d.store.SetAgentWakeupClaimState(w.ID, claimID, "stale")
			continue
		}
		registered, projectErr := projectByID(d.store, w.ProjectID)
		if projectErr != nil || !registered.Enabled {
			_ = d.store.SetAgentWakeupClaimState(w.ID, claimID, "held")
			continue
		}
		if message.ExpiresAt != "" {
			expires, parseErr := time.Parse(time.RFC3339, message.ExpiresAt)
			if parseErr != nil || !expires.After(time.Now().UTC()) {
				_ = d.store.SetAgentWakeupClaimState(w.ID, claimID, "stale")
				continue
			}
		}
		if message.WorkRevision > 0 && message.OriginTaskID != "" {
			origin, originErr := d.store.FindRunScoped(w.ProjectID, message.OriginTaskID)
			if originErr != nil || origin == nil || origin.WorkRevision != message.WorkRevision || (message.RouteGeneration > 0 && origin.LeaseGeneration != message.RouteGeneration) {
				_ = d.store.SetAgentWakeupClaimState(w.ID, claimID, "stale")
				continue
			}
		}
		if w.RecipientKind == "execution" {
			record, recordErr := d.store.Execution(w.RecipientID)
			if recordErr != nil || record == nil || record.ProjectID != w.ProjectID || record.AttemptID == "" {
				_ = d.store.SetAgentWakeupClaimState(w.ID, claimID, "held")
				continue
			}
			handle := liveRegistry.FindAttempt(record.AttemptID)
			codex, ok := handle.(*codexLiveHandle)
			if !ok || handle.ProjectID() != w.ProjectID || handle.AttemptID() != record.AttemptID {
				_ = d.store.SetAgentWakeupClaimState(w.ID, claimID, "held")
				continue
			}
			if err := codex.deliverAgentMessage(message); err != nil {
				_ = d.store.SetAgentWakeupClaimState(w.ID, claimID, "uncertain")
				continue
			}
			_ = d.store.SetAgentWakeupClaimState(w.ID, claimID, "delivered")
			_, _ = d.store.exec(`UPDATE agent_messages SET transport_state='delivered' WHERE project_id=? AND id=?`, w.ProjectID, message.ID)
			continue
		}
		if w.RecipientKind != "task" {
			_ = d.store.SetAgentWakeupClaimState(w.ID, claimID, "unsupported")
			continue
		}
		// Task addresses deliberately survive owner turnover. The normal retry
		// scheduler resolves the current session/profile (including Muse) later.
		if changed, err := d.store.QueueAgentContinuation(w.ProjectID, w.RecipientID); err != nil {
			return err
		} else if !changed {
			_ = d.store.SetAgentWakeupClaimState(w.ID, claimID, "held")
			continue
		}
		_ = d.store.SetAgentWakeupClaimState(w.ID, claimID, "scheduled")
	}
	return nil
}

func (s *RuntimeStore) ClaimAgentWakeup(id string) string {
	claimID := "wake-claim-" + strings.ToLower(newRecordID())
	now := time.Now().UTC()
	result, err := s.exec(`UPDATE agent_wakeups SET state='delivering',claim_id=?,claimed_at=? WHERE id=? AND (state IN ('queued','held','unsupported') OR (state='delivering' AND claimed_at<?))`, claimID, now.Format(time.RFC3339Nano), id, now.Add(-time.Minute).Format(time.RFC3339Nano))
	if err != nil {
		return ""
	}
	n, _ := result.RowsAffected()
	if n != 1 {
		return ""
	}
	return claimID
}

func (s *RuntimeStore) SetAgentWakeupClaimState(id, claimID, state string) error {
	if state != "delivered" && state != "scheduled" && state != "held" && state != "unsupported" && state != "stale" && state != "uncertain" {
		return errors.New("invalid wakeup state")
	}
	result, err := s.exec(`UPDATE agent_wakeups SET state=?,claim_id='',claimed_at='' WHERE id=? AND claim_id=?`, state, id, claimID)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n != 1 {
		return errors.New("agent wakeup claim lost")
	}
	return nil
}

// processArchitectWaveReports converts material wave state changes into one
// durable architect message. The architect decides; this code only reports.
func (d *Daemon) processArchitectWaveReports(projectFilter string) error {
	projects, err := d.store.ListProjects()
	if err != nil {
		return err
	}
	allRuns, err := d.store.ListRuns()
	if err != nil {
		return err
	}
	for _, project := range projects {
		if !project.Enabled || (projectFilter != "" && project.ProjectID != projectFilter) {
			continue
		}
		idx, err := loadV7Index(project.VaultRoot)
		if err != nil {
			continue
		}
		runs := map[string]RunStatus{}
		for _, run := range allRuns {
			if run.ProjectID == project.ProjectID {
				runs[firstNonEmpty(run.ItemID, run.RecordID)] = run
			}
		}
		messages, err := d.store.ListAgentMessages(project.ProjectID, "", "")
		if err != nil {
			return err
		}
		for _, wave := range sortedV7Waves(idx) {
			members := normalizeList(wave.Data["members"])
			if len(members) == 0 {
				continue
			}
			memberSet, allDone, pending := map[string]bool{}, true, []string{}
			for _, id := range members {
				memberSet[id] = true
				if task, ok := idx.Tasks[id]; !ok || stringField(task.Data, "status") != "done" {
					allDone = false
				}
			}
			for _, message := range messages {
				if memberSet[message.OriginTaskID] && message.Kind == "question" && message.AnsweredAt == "" {
					pending = append(pending, message.ID)
				}
			}
			snapshot := buildArmedWaveSnapshot(project.VaultRoot, idx, wave, runs, time.Now().UTC())
			outcome := ""
			switch {
			case allDone:
				outcome = "completed"
			case snapshot.Authorization == "armed" && len(snapshot.Frontier) == 0 && len(pending) > 0:
				outcome = "stalled"
			}
			if outcome == "" {
				continue
			}
			architect, ok := waveArchitectAddress(idx, members)
			if !ok {
				continue
			}
			report := ArchitectReport{ObjectiveID: firstNonEmpty(stringField(wave.Data, "epic"), stringField(wave.Data, "id")), Outcome: outcome, PendingQuestions: pending}
			for _, member := range snapshot.Members {
				report.Evidence = append(report.Evidence, member.ID+":"+member.State)
			}
			body, _ := json.Marshal(report)
			digest := sha256.Sum256(body)
			trigger := "wave:" + snapshot.WaveID + ":" + outcome + ":" + fmt.Sprintf("%x", digest[:8])
			_, _, err = d.store.RecordArchitectContinuation(project.ProjectID, trigger, report)
			if err != nil {
				return err
			}
			_, _, err = d.store.PutAgentMessage(AgentMessage{IdempotencyKey: trigger, ProjectID: project.ProjectID, Sender: "execution:tusker-wave-" + snapshot.WaveID, Recipient: architect, OriginWaveID: snapshot.WaveID, Kind: "wave_result", Body: string(body), ReplyRequired: true})
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func waveArchitectAddress(idx v7Index, members []string) (AgentAddress, bool) {
	var architect AgentAddress
	for _, id := range members {
		for _, contact := range authoredAgentContacts(idx.Tasks[id]) {
			if contact.Role != "architect" {
				continue
			}
			if architect.ID != "" && architect != contact.Address {
				return AgentAddress{}, false
			}
			architect = contact.Address
		}
	}
	return architect, architect.ID != ""
}

// QueueAgentContinuation preserves a live owner and uses the normal scheduler
// only after that owner has settled. It does not reset budgets or attempt
// counters like an operator redrive.
func (s *RuntimeStore) QueueAgentContinuation(projectID, identity string) (bool, error) {
	run, err := findRunScopedOrAmbiguous(s, projectID, identity)
	if err != nil || run == nil {
		return false, err
	}
	if retryHasLiveAttempt(*run, time.Now().UTC()) == retryLiveAttempt {
		return false, nil
	}
	next := *run
	next.LeaseState = string(LeaseStateRetryQueued)
	next.NextRetryAt = time.Now().UTC().Format(time.RFC3339)
	next.Terminal = false
	next.LastError = "agent message ready for continuation"
	next.UpdatedAt = next.NextRetryAt
	return s.UpsertRunIfSnapshot(*run, next)
}

// QueueAgentWakeup is bookkeeping only. One message per wakeup keeps delivery
// receipts unambiguous; the recipient packet coalesces outstanding messages.
func (s *RuntimeStore) QueueAgentWakeup(project string, recipient AgentAddress, reason, key string, messageIDs []string) (AgentWakeup, bool, error) {
	if project == "" || recipient.ID == "" || key == "" {
		return AgentWakeup{}, false, errors.New("wakeup project, recipient and key are required")
	}
	if len(messageIDs) != 1 {
		return AgentWakeup{}, false, errors.New("wakeup requires exactly one message")
	}
	sort.Strings(messageIDs)
	raw, _ := json.Marshal(messageIDs)
	w := AgentWakeup{ID: "wake-" + strings.ToLower(newRecordID()), ProjectID: project, RecipientKind: recipient.Kind, RecipientID: recipient.ID, Reason: reason, State: "queued", IdempotencyKey: key, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano), MessageIDs: messageIDs}
	result, err := s.exec(`INSERT OR IGNORE INTO agent_wakeups(id,project_id,recipient_kind,recipient_id,reason,message_ids_json,state,model_turns,idempotency_key,created_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, w.ID, w.ProjectID, w.RecipientKind, w.RecipientID, w.Reason, string(raw), w.State, 0, w.IdempotencyKey, w.CreatedAt)
	if err != nil {
		return AgentWakeup{}, false, err
	}
	n, _ := result.RowsAffected()
	if n == 1 {
		return w, false, nil
	}
	var existing AgentWakeup
	var existingRaw string
	if err := s.queryRowScan(`SELECT id,project_id,recipient_kind,recipient_id,reason,message_ids_json,state,model_turns,idempotency_key,created_at,claim_id,claimed_at FROM agent_wakeups WHERE project_id=? AND idempotency_key=?`, []any{project, key}, &existing.ID, &existing.ProjectID, &existing.RecipientKind, &existing.RecipientID, &existing.Reason, &existingRaw, &existing.State, &existing.ModelTurns, &existing.IdempotencyKey, &existing.CreatedAt, &existing.ClaimID, &existing.ClaimedAt); err != nil {
		return AgentWakeup{}, false, err
	}
	_ = json.Unmarshal([]byte(existingRaw), &existing.MessageIDs)
	if existing.RecipientKind != recipient.Kind || existing.RecipientID != recipient.ID || existing.Reason != reason || strings.Join(existing.MessageIDs, "\x00") != strings.Join(messageIDs, "\x00") {
		return AgentWakeup{}, false, errors.New("wakeup idempotency key was reused with different content")
	}
	return existing, true, nil
}

func coordinationWaitCycle(edges map[string]string) []string {
	for start := range edges {
		seen := map[string]int{}
		path := []string{}
		for at := start; at != ""; at = edges[at] {
			if i, ok := seen[at]; ok {
				return append(path[i:], at)
			}
			seen[at] = len(path)
			path = append(path, at)
		}
	}
	return nil
}

type ArchitectReport struct {
	ObjectiveID      string   `json:"objectiveId"`
	Outcome          string   `json:"outcome"`
	Accepted         []string `json:"accepted"`
	Blockers         []string `json:"blockers"`
	PendingQuestions []string `json:"pendingQuestions"`
	Evidence         []string `json:"evidence"`
}

type ArchitectProposal struct {
	Kind            string `json:"kind"`
	ContextRevision string `json:"contextRevision"`
	PlanPath        string `json:"planPath,omitempty"`
	AutoStart       bool   `json:"autoStart"`
}
type ContinuationPolicy struct {
	Enabled  bool `json:"enabled"`
	Stopped  bool `json:"stopped"`
	MaxWaves int  `json:"maxWaves"`
}

func (s *RuntimeStore) SetArchitectProposal(id string, proposal ArchitectProposal) error {
	if proposal.Kind != "next_wave" && proposal.Kind != "repair" && proposal.Kind != "needs_user" && proposal.Kind != "objective_complete" {
		return errors.New("unsupported architect proposal kind")
	}
	raw, _ := json.Marshal(proposal)
	result, err := s.exec(`UPDATE architect_continuations SET proposal_json=?,state='proposed',last_error='' WHERE id=? AND state='pending'`, string(raw), id)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n != 1 {
		return errors.New("continuation is not pending")
	}
	return nil
}

// ApplyArchitectContinuation claims one proposal and delegates its validated
// mutation to existing delivery machinery. Paused/stopped policies retain it.
func (s *RuntimeStore) ApplyArchitectContinuation(id, currentRevision string, policy ContinuationPolicy, apply func(ArchitectProposal) (string, error)) (bool, error) {
	if !policy.Enabled || policy.Stopped {
		return false, nil
	}
	var raw, state, projectID, objectiveID string
	if err := s.queryRowScan(`SELECT proposal_json,state,project_id,objective_id FROM architect_continuations WHERE id=?`, []any{id}, &raw, &state, &projectID, &objectiveID); err != nil {
		return false, err
	}
	if state == "applied" {
		return false, nil
	}
	if state != "proposed" {
		return false, errors.New("continuation has no applicable proposal")
	}
	var proposal ArchitectProposal
	if err := json.Unmarshal([]byte(raw), &proposal); err != nil {
		return false, err
	}
	if proposal.ContextRevision != "" && proposal.ContextRevision != currentRevision {
		_, _ = s.exec(`UPDATE architect_continuations SET state='conflict',last_error='context revision changed' WHERE id=? AND state='proposed'`, id)
		return false, errors.New("architect proposal context revision changed")
	}
	if (proposal.Kind == "next_wave" || proposal.Kind == "repair") && proposal.ContextRevision == "" {
		return false, errors.New("mutating architect proposal requires a context revision")
	}
	if policy.MaxWaves > 0 {
		var applied int
		if err := s.queryRowScan(`SELECT COUNT(*) FROM architect_continuations WHERE project_id=? AND objective_id=? AND state='applied' AND applied_wave_id<>''`, []any{projectID, objectiveID}, &applied); err != nil {
			return false, err
		}
		if applied >= policy.MaxWaves {
			return false, errors.New("architect continuation wave limit reached")
		}
	}
	claim, err := s.exec(`UPDATE architect_continuations SET state='applying' WHERE id=? AND state='proposed'`, id)
	if err != nil {
		return false, err
	}
	claimed, _ := claim.RowsAffected()
	if claimed != 1 {
		return false, nil
	}
	if proposal.Kind != "next_wave" && proposal.Kind != "repair" {
		_, err := s.exec(`UPDATE architect_continuations SET state='applied' WHERE id=? AND state='applying'`, id)
		return true, err
	}
	waveID, err := apply(proposal)
	if err != nil {
		_, _ = s.exec(`UPDATE architect_continuations SET state='proposed',last_error=? WHERE id=? AND state='applying'`, err.Error(), id)
		return false, err
	}
	result, err := s.exec(`UPDATE architect_continuations SET state='applied',applied_wave_id=?,last_error='' WHERE id=? AND state='applying'`, waveID, id)
	if err != nil {
		return false, err
	}
	n, _ := result.RowsAffected()
	return n == 1, nil
}

func (s *RuntimeStore) RecordArchitectContinuation(project, trigger string, report ArchitectReport) (string, bool, error) {
	if project == "" || report.ObjectiveID == "" || trigger == "" {
		return "", false, errors.New("continuation project, objective and trigger are required")
	}
	raw, _ := json.Marshal(report)
	id := "continuation-" + strings.ToLower(newRecordID())
	result, err := s.exec(`INSERT OR IGNORE INTO architect_continuations(id,project_id,objective_id,trigger_key,report_json,created_at) VALUES(?,?,?,?,?,?)`, id, project, report.ObjectiveID, trigger, string(raw), time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return "", false, err
	}
	n, _ := result.RowsAffected()
	if n == 1 {
		return id, false, nil
	}
	var existingID, existingRaw string
	if err := s.queryRowScan(`SELECT id,report_json FROM architect_continuations WHERE project_id=? AND objective_id=? AND trigger_key=?`, []any{project, report.ObjectiveID, trigger}, &existingID, &existingRaw); err != nil {
		return "", false, err
	}
	if existingRaw != string(raw) {
		return "", false, errors.New("continuation trigger was reused with different report content")
	}
	return existingID, true, nil
}
