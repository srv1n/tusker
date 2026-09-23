package main

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log"
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
	rows, err := s.query(`SELECT id,project_id,recipient_kind,recipient_id,reason,message_ids_json,state,model_turns,idempotency_key,created_at,claim_id,claimed_at FROM agent_wakeups WHERE (state IN ('queued','held') OR (state='delivering' AND claimed_at<?)) AND (?='' OR project_id=?) ORDER BY created_at,id`, time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano), project, project)
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

func (d *Daemon) openYieldQuestion(run RunStatus) (bool, error) {
	messages, err := d.store.ListAgentMessagesForTask(run.ProjectID, run.ItemID)
	if err != nil {
		return false, err
	}
	for _, m := range messages {
		if m.Kind == "question" && m.Sender == "task:"+run.ItemID && m.YieldSender && m.AnsweredAt == "" && m.WorkRevision == run.WorkRevision && (m.RouteGeneration == 0 || m.RouteGeneration == run.LeaseGeneration) {
			return true, nil
		}
	}
	return false, nil
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
			if recordErr != nil || record == nil || record.ProjectID != w.ProjectID {
				_ = d.store.SetAgentWakeupClaimState(w.ID, claimID, "held")
				continue
			}
			if message.RecipientGeneration > 0 {
				var matching int
				if err := d.store.queryRowScan(`SELECT COUNT(*) FROM agent_contacts WHERE project_id=? AND address_kind='execution' AND address_id=? AND generation=?`, []any{w.ProjectID, record.ExecutionID, message.RecipientGeneration}, &matching); err != nil || matching != 1 {
					_ = d.store.SetAgentWakeupClaimState(w.ID, claimID, "stale")
					_, _ = d.store.exec(`UPDATE agent_messages SET transport_state='stale' WHERE project_id=? AND id=?`, w.ProjectID, message.ID)
					continue
				}
			}
			if record.AttemptID == "" {
				if record.Source == "direct_claude" || record.Source == "direct_codex" {
					_ = d.store.SetAgentWakeupClaimState(w.ID, claimID, "held")
					continue
				}
				_ = d.store.SetAgentWakeupClaimState(w.ID, claimID, "unsupported")
				_, _ = d.store.exec(`UPDATE agent_messages SET transport_state='unsupported' WHERE project_id=? AND id=? AND transport_state NOT IN ('delivered','consumed') AND consumed_at=''`, w.ProjectID, message.ID)
				continue
			}
			provider, providerErr := d.store.ExecutionProvider(record.ExecutionID)
			if providerErr != nil {
				_ = d.store.SetAgentWakeupClaimState(w.ID, claimID, "held")
				continue
			}
			if provider != "" && provider != "codex" {
				_ = d.store.SetAgentWakeupClaimState(w.ID, claimID, "unsupported")
				_, _ = d.store.exec(`UPDATE agent_messages SET transport_state='unsupported' WHERE project_id=? AND id=?`, w.ProjectID, message.ID)
				continue
			}
			handle := liveRegistry.FindAttempt(record.AttemptID)
			codex, ok := handle.(*codexLiveHandle)
			if !ok || handle.ProjectID() != w.ProjectID || handle.AttemptID() != record.AttemptID {
				_ = d.store.SetAgentWakeupClaimState(w.ID, claimID, "held")
				continue
			}
			if _, _, turnID, _ := codex.liveState(); turnID == "" {
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
		if message.Kind == "answer" {
			parent, err := d.store.AgentMessage(w.ProjectID, message.ReplyTo)
			if err != nil || !parent.YieldSender || parent.Sender != "task:"+w.RecipientID {
				_ = d.store.SetAgentWakeupClaimState(w.ID, claimID, "stale")
				continue
			}
			if message.ConsumedAt != "" {
				_ = d.store.SetAgentWakeupClaimState(w.ID, claimID, "delivered")
				continue
			}
			var firstAnswer string
			if err := d.store.queryRowScan(`SELECT id FROM agent_messages WHERE project_id=? AND reply_to=? AND kind='answer' ORDER BY created_at,id LIMIT 1`, []any{w.ProjectID, parent.ID}, &firstAnswer); err != nil {
				return err
			}
			if firstAnswer != message.ID {
				_ = d.store.SetAgentWakeupClaimState(w.ID, claimID, "stale")
				continue
			}
			run, err := d.store.FindRunScoped(w.ProjectID, w.RecipientID)
			if err != nil {
				return err
			}
			if run == nil || run.LeaseState == string(LeaseStateInterrupted) {
				_ = d.store.SetAgentWakeupClaimState(w.ID, claimID, "held")
				continue
			}
			if (parent.WorkRevision > 0 && run.WorkRevision != parent.WorkRevision) || (parent.RouteGeneration > 0 && run.LeaseGeneration != parent.RouteGeneration) {
				_ = d.store.SetAgentWakeupClaimState(w.ID, claimID, "stale")
				continue
			}
			if blocked, _, err := d.automaticRetryBlockedByStopIntent(*run); err != nil {
				return err
			} else if blocked {
				_ = d.store.SetAgentWakeupClaimState(w.ID, claimID, "held")
				continue
			}
			if retryHasLiveAttempt(*run, time.Now().UTC()) == retryLiveAttempt {
				if message.TransportState == "delivered" {
					_ = d.store.SetAgentWakeupClaimState(w.ID, claimID, "delivered")
					continue
				}
				if soft, _ := workerSoftDelivery(run.RunnerHarness); soft {
					created, _ := time.Parse(time.RFC3339Nano, message.CreatedAt)
					if time.Since(created) < 120*time.Second {
						_ = d.store.SetAgentWakeupClaimState(w.ID, claimID, "held")
						continue
					}
				}
				project, task, wave, err := runSayContext(d.store, *run)
				if err != nil {
					_ = d.store.SetAgentWakeupClaimState(w.ID, claimID, "held")
					continue
				}
				result, sayErr := runSayHard(d.store, d.stateRoot, project, task, wave, *run, "agent-message", message.Body, "message:"+message.ID, time.Now().UTC())
				if sayErr != nil {
					state := "held"
					if result.Delivery.State == "uncertain" || result.Delivery.State == "delivering" {
						state = "uncertain"
					}
					_ = d.store.SetAgentWakeupClaimState(w.ID, claimID, state)
					continue
				}
				_ = d.store.SetAgentWakeupClaimState(w.ID, claimID, "delivered")
				_, _ = d.store.exec(`UPDATE agent_messages SET transport_state='delivered' WHERE project_id=? AND id=?`, w.ProjectID, message.ID)
				continue
			}
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
	result, err := s.exec(`UPDATE agent_wakeups SET state='delivering',claim_id=?,claimed_at=? WHERE id=? AND (state IN ('queued','held') OR (state='delivering' AND claimed_at<?))`, claimID, now.Format(time.RFC3339Nano), id, now.Add(-time.Minute).Format(time.RFC3339Nano))
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
			now := time.Now().UTC()
			snapshot := buildArmedWaveSnapshot(project.VaultRoot, idx, wave, runs, now)
			stalled, blockers := architectWaveStalled(snapshot, idx, runs, now)
			outcome := ""
			switch {
			case allDone:
				outcome = "completed"
			case stalled:
				outcome = "stalled"
			}
			if outcome == "" {
				continue
			}
			if outcome == "stalled" {
				sort.Strings(pending)
			}
			architect, ok, conflict, err := d.waveArchitectAddress(project.ProjectID, snapshot.WaveID, idx, members)
			if err != nil {
				return err
			}
			if conflict {
				log.Printf("architect wave report: conflicting architects project=%s wave=%s", project.ProjectID, snapshot.WaveID)
			}
			if !ok {
				continue
			}
			report := ArchitectReport{ObjectiveID: firstNonEmpty(stringField(wave.Data, "epic"), stringField(wave.Data, "id")), Outcome: outcome, PendingQuestions: pending, Blockers: blockers}
			for _, member := range snapshot.Members {
				evidence := member.ID + ":" + member.State
				if outcome == "stalled" {
					evidence += ":" + member.Reason
				}
				report.Evidence = append(report.Evidence, evidence)
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

// ponytail: make this a wave-level workflow setting if operators need to tune
// report latency; codex.stall_timeout_ms is a runner timeout, not wave quiet time.
const architectWaveStallWindow = 10 * time.Minute

func architectWaveStalled(snapshot armedWaveSnapshot, idx v7Index, runs map[string]RunStatus, now time.Time) (bool, []string) {
	if snapshot.Authorization != "armed" {
		return false, nil
	}
	parked := false
	blockers := []string{}
	for _, member := range snapshot.Members {
		run := runs[member.ID]
		if retry, err := time.Parse(time.RFC3339Nano, run.NextRetryAt); err == nil && retry.After(now) {
			return false, nil
		}
		if updated, err := time.Parse(time.RFC3339Nano, run.UpdatedAt); err == nil && updated.After(now.Add(-architectWaveStallWindow)) {
			return false, nil
		}
		switch member.State {
		case armedWaveRunning, armedWaveRunnable, armedWaveReview:
			return false, nil
		case armedWaveMachineParked, armedWaveHumanBlocked:
			parked = true
			blockers = append(blockers, member.ID+":"+member.State+":"+member.Reason)
		case armedWaveDependencyWaiting:
			if state, reason, blocked := architectHardDependencyBlocker(member.ID, idx, runs, now, map[string]bool{}); blocked {
				parked = true
				blockers = append(blockers, member.ID+":"+state+":"+reason)
			} else {
				blockers = append(blockers, member.ID+":"+member.State+":"+member.Reason)
			}
		}
	}
	return parked, blockers
}

func architectHardDependencyBlocker(id string, idx v7Index, runs map[string]RunStatus, now time.Time, seen map[string]bool) (string, string, bool) {
	if seen[id] {
		return "", "", false
	}
	seen[id] = true
	task, ok := idx.Tasks[id]
	if !ok {
		return armedWaveMachineParked, "hard dependency " + id + " is missing", true
	}
	for _, edge := range v7TaskDependencyEdges(task, idx) {
		if edge.Hardness != v7DependencyHardnessHard {
			continue
		}
		dep, exists := idx.Tasks[edge.ID]
		if exists && v7DependencySatisfiedForReadiness(edge, dep, true) {
			continue
		}
		if !exists {
			return armedWaveMachineParked, "hard dependency " + edge.ID + " is missing", true
		}
		run := runs[edge.ID]
		if retry, err := time.Parse(time.RFC3339Nano, run.NextRetryAt); err == nil && retry.After(now) {
			continue
		}
		if armedWaveTaskHumanBlocked(idx, dep) {
			return armedWaveHumanBlocked, "hard dependency closure of " + edge.ID, true
		}
		if armedWaveRunMachineParked(run) {
			return armedWaveMachineParked, "hard dependency closure of " + edge.ID + ": " + firstNonEmpty(run.LastError, "attempt policy exhausted"), true
		}
		if state, reason, blocked := architectHardDependencyBlocker(edge.ID, idx, runs, now, seen); blocked {
			return state, "hard dependency closure of " + edge.ID + ": " + reason, true
		}
	}
	return "", "", false
}

func (d *Daemon) waveArchitectAddress(projectID, waveID string, idx v7Index, members []string) (AgentAddress, bool, bool, error) {
	authored, ok := waveArchitectAddress(idx, members)
	if ok {
		return authored, true, false, nil
	}
	// An authored conflict is terminal. It cannot be hidden by a runtime contact.
	var seen AgentAddress
	for _, id := range members {
		for _, contact := range authoredAgentContacts(idx.Tasks[id]) {
			if contact.Role != "architect" {
				continue
			}
			if seen.ID != "" && seen != contact.Address {
				return AgentAddress{}, false, true, nil
			}
			seen = contact.Address
		}
	}
	contacts, err := d.store.AgentContacts(projectID, waveID)
	if err != nil {
		return AgentAddress{}, false, false, err
	}
	for _, contact := range contacts {
		if contact.Role != "architect" {
			continue
		}
		if seen.ID != "" && seen != contact.Address {
			return AgentAddress{}, false, true, nil
		}
		seen = contact.Address
	}
	return seen, seen.ID != "", false, nil
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
