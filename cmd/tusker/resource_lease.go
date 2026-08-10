package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// ResourceLease is a daemon-wide, named scarce-resource reservation. Its
// generation is a fencing token: a holder that loses a takeover must not renew
// or release the new holder's lease.
type ResourceLease struct {
	Name          string `json:"name"`
	Owner         string `json:"owner"`
	Purpose       string `json:"purpose"`
	ProjectID     string `json:"project_id"`
	DepartureID   string `json:"departure_id"`
	HeartbeatAt   string `json:"heartbeat_at"`
	ExpiresAt     string `json:"expires_at"`
	Generation    int    `json:"generation"`
	State         string `json:"state"`
	ReleasedAt    string `json:"released_at,omitempty"`
	ReleaseReason string `json:"release_reason,omitempty"`
	UpdatedAt     string `json:"updated_at"`
}

type ResourceLeaseEvent struct {
	EventID            string `json:"event_id"`
	ResourceName       string `json:"resource_name"`
	EventType          string `json:"event_type"`
	Owner              string `json:"owner"`
	Generation         int    `json:"generation"`
	PreviousOwner      string `json:"previous_owner,omitempty"`
	PreviousGeneration int    `json:"previous_generation,omitempty"`
	Reason             string `json:"reason,omitempty"`
	CreatedAt          string `json:"created_at"`
}

type ResourceLeaseAcquireInput struct {
	Name        string
	Owner       string
	Purpose     string
	ProjectID   string
	DepartureID string
	TTL         time.Duration
	Now         time.Time
	// HolderAlive is optional but, when supplied, is authoritative: an expired
	// lease with a verified-live holder remains held. Without a probe, expiry is
	// the configured recovery policy and the next holder receives a new fence.
	HolderAlive func(ResourceLease) bool
}

type ResourceLeaseRenewal struct {
	Name       string
	Owner      string
	Generation int
	TTL        time.Duration
	Now        time.Time
}

type ResourceLeaseRecovery struct {
	Lease     ResourceLease `json:"lease"`
	Recovered bool          `json:"recovered"`
	Reason    string        `json:"reason,omitempty"`
}

const (
	resourceLeaseHeld                  = "held"
	resourceLeaseReleased              = "released"
	resourceLeaseRefusal               = "RESOURCE_LEASE_HELD"
	defaultResourceLeaseTTL            = 2 * time.Minute
	scheduledPromotionResourcePurpose  = "scheduled full promotion gate"
	resourceLeaseDurableHolderUnknown  = 0
	resourceLeaseDurableHolderAlive    = 1
	resourceLeaseDurableHolderNotAlive = 2
)

var resourceLeaseWaiterState = struct {
	sync.Mutex
	notify func(stateRoot, resourceName, projectID string)
}{
	notify: func(stateRoot, resourceName, projectID string) {
		_ = sendDaemonControlOneWay(stateRoot, daemonControlRequest{
			Command: "reconcile_project", ProjectID: projectID, Cause: "resource_release:" + resourceName,
		}, 250*time.Millisecond)
	},
}

func resourceLeaseWaiterSetting(name string) string {
	return "resource_lease_waiters_v1:" + strings.TrimSpace(name)
}

// RegisterResourceLeaseWaiter records only the project identity. Task-level
// eligibility remains rebuildable from canonical notes; the durable waiter set
// exists solely so a global resource release can target the affected projects.
func (s *RuntimeStore) RegisterResourceLeaseWaiter(name, projectID string) error {
	name, projectID = strings.TrimSpace(name), strings.TrimSpace(projectID)
	if name == "" || projectID == "" {
		return tuskerError(errorInvalidArg, "resource waiter requires resource name and project_id")
	}
	resourceLeaseWaiterState.Lock()
	defer resourceLeaseWaiterState.Unlock()
	waiters, err := s.resourceLeaseWaitersLocked(name)
	if err != nil {
		return err
	}
	if containsString(waiters, projectID) {
		return nil
	}
	waiters = sortedStrings(append(waiters, projectID))
	raw, err := json.Marshal(waiters)
	if err != nil {
		return err
	}
	return s.SetSetting(resourceLeaseWaiterSetting(name), string(raw))
}

func (s *RuntimeStore) ClearResourceLeaseWaiter(name, projectID string) error {
	name, projectID = strings.TrimSpace(name), strings.TrimSpace(projectID)
	if name == "" || projectID == "" {
		return nil
	}
	resourceLeaseWaiterState.Lock()
	defer resourceLeaseWaiterState.Unlock()
	waiters, err := s.resourceLeaseWaitersLocked(name)
	if err != nil || !containsString(waiters, projectID) {
		return err
	}
	filtered := make([]string, 0, len(waiters)-1)
	for _, waiter := range waiters {
		if waiter != projectID {
			filtered = append(filtered, waiter)
		}
	}
	raw, err := json.Marshal(filtered)
	if err != nil {
		return err
	}
	return s.SetSetting(resourceLeaseWaiterSetting(name), string(raw))
}

func (s *RuntimeStore) resourceLeaseWaitersLocked(name string) ([]string, error) {
	raw, err := s.GetSetting(resourceLeaseWaiterSetting(name))
	if err != nil || strings.TrimSpace(raw) == "" {
		return nil, err
	}
	var waiters []string
	if err := json.Unmarshal([]byte(raw), &waiters); err != nil {
		return nil, nil
	}
	return sortedStrings(uniqueStrings(waiters)), nil
}

func (s *RuntimeStore) takeResourceLeaseWaiters(name string) ([]string, error) {
	resourceLeaseWaiterState.Lock()
	defer resourceLeaseWaiterState.Unlock()
	waiters, err := s.resourceLeaseWaitersLocked(name)
	if err != nil || len(waiters) == 0 {
		return waiters, err
	}
	if err := s.SetSetting(resourceLeaseWaiterSetting(name), "[]"); err != nil {
		return nil, err
	}
	return waiters, nil
}

func (s *RuntimeStore) notifyResourceLeaseWaiters(name string) {
	waiters, err := s.takeResourceLeaseWaiters(name)
	if err != nil {
		return
	}
	resourceLeaseWaiterState.Lock()
	notify := resourceLeaseWaiterState.notify
	resourceLeaseWaiterState.Unlock()
	for _, projectID := range waiters {
		notify(s.stateRoot, strings.TrimSpace(name), projectID)
	}
}

func normalizeResourceLeaseNow(now time.Time) time.Time {
	if now.IsZero() {
		now = time.Now()
	}
	return now.UTC()
}

func normalizeResourceLeaseTTL(ttl time.Duration) time.Duration {
	if ttl <= 0 {
		return defaultResourceLeaseTTL
	}
	return ttl
}

func resourceLeaseContention(lease ResourceLease, liveness string) error {
	return tuskerError(resourceLeaseRefusal,
		fmt.Sprintf("resource lease %q is held by %s for %s", lease.Name, lease.Owner, lease.Purpose),
		withHint("wait for the holder to release it, or let daemon reconciliation reclaim it after expiry"),
		withContext(map[string]any{
			"resource": lease.Name, "owner": lease.Owner, "project_id": lease.ProjectID,
			"departure_id": lease.DepartureID, "generation": lease.Generation,
			"expires_at": lease.ExpiresAt, "liveness": liveness,
		}))
}

type resourceLeaseRowQuerier interface {
	QueryRow(query string, args ...any) *sql.Row
}

func scheduledPromotionResourceDepartureID(lease ResourceLease) (string, bool) {
	departureID := strings.TrimSpace(lease.DepartureID)
	if strings.TrimSpace(lease.Purpose) != scheduledPromotionResourcePurpose ||
		departureID == "" || strings.TrimSpace(lease.Owner) != "departure:"+departureID {
		return "", false
	}
	return departureID, true
}

func resourceLeaseDurableHolderState(query resourceLeaseRowQuerier, lease ResourceLease) (int, string, error) {
	recordID, taskDispatch := fairDispatchResourceRecordID(lease)
	if taskDispatch {
		var count int
		err := query.QueryRow(`
			SELECT COUNT(1)
			FROM runs
			WHERE project_id=? AND record_id=? AND lease_owner=?
				AND terminal=0 AND lease_state IN ('claimed', 'running')`,
			lease.ProjectID, recordID, lease.Owner,
		).Scan(&count)
		if err != nil {
			return resourceLeaseDurableHolderUnknown, "", err
		}
		if count > 0 {
			return resourceLeaseDurableHolderAlive, "run", nil
		}
		return resourceLeaseDurableHolderNotAlive, "run", nil
	}
	departureID, scheduledPromotion := scheduledPromotionResourceDepartureID(lease)
	if !scheduledPromotion {
		return resourceLeaseDurableHolderUnknown, "", nil
	}
	var state string
	err := query.QueryRow(`SELECT state FROM departure_runs WHERE id=? AND project_id=?`, departureID, lease.ProjectID).Scan(&state)
	if errors.Is(err, sql.ErrNoRows) {
		return resourceLeaseDurableHolderNotAlive, "scheduled promotion", nil
	}
	if err != nil {
		return resourceLeaseDurableHolderUnknown, "", err
	}
	// Only the gating state owns gate:full. Once the departure advances,
	// terminates, or disappears, its old gate reservation is stale.
	if DepartureState(strings.TrimSpace(state)) == DepartureStateGating {
		return resourceLeaseDurableHolderAlive, "scheduled promotion", nil
	}
	return resourceLeaseDurableHolderNotAlive, "scheduled promotion", nil
}

// AcquireResourceLease atomically acquires one globally named resource. It is
// safe for independent daemon processes because the name is a SQLite primary
// key and the selection, liveness decision, write, and takeover event happen
// in one write transaction.
func (s *RuntimeStore) AcquireResourceLease(input ResourceLeaseAcquireInput) (ResourceLease, bool, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.Owner = strings.TrimSpace(input.Owner)
	input.Purpose = strings.TrimSpace(input.Purpose)
	input.ProjectID = strings.TrimSpace(input.ProjectID)
	input.DepartureID = strings.TrimSpace(input.DepartureID)
	if input.Name == "" || input.Owner == "" || input.Purpose == "" || input.ProjectID == "" {
		return ResourceLease{}, false, tuskerError(errorInvalidArg, "resource lease requires name, owner, purpose, and project_id")
	}
	now := normalizeResourceLeaseNow(input.Now)
	ttl := normalizeResourceLeaseTTL(input.TTL)
	var lease ResourceLease
	acquired := false
	err := s.withBusyRetry(func() error {
		tx, err := s.db.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback()
		current, found, err := findResourceLeaseTx(tx, input.Name)
		if err != nil {
			return err
		}
		sameHolder := found &&
			current.Owner == input.Owner &&
			current.ProjectID == input.ProjectID &&
			current.DepartureID == input.DepartureID
		freshHolder := false
		if found && current.State == resourceLeaseHeld {
			expires, parseErr := time.Parse(time.RFC3339Nano, current.ExpiresAt)
			if parseErr != nil {
				expires, parseErr = time.Parse(time.RFC3339, current.ExpiresAt)
			}
			if parseErr == nil && expires.After(now) {
				freshHolder = true
				if !sameHolder {
					return resourceLeaseContention(current, "fresh")
				}
			}
			if !freshHolder {
				durableState, _, err := resourceLeaseDurableHolderState(tx, current)
				if err != nil {
					return err
				}
				holderAlive := durableState == resourceLeaseDurableHolderAlive
				if durableState == resourceLeaseDurableHolderUnknown && input.HolderAlive != nil {
					holderAlive = input.HolderAlive(current)
				}
				if holderAlive {
					if !sameHolder {
						return resourceLeaseContention(current, "lease_expired_holder_alive")
					}
					// The exact task-run owner is still authoritative. Treat its
					// expired row as an idempotent heartbeat; no fence changed.
					freshHolder = true
				}
			}
		}
		generation := 1
		eventType := "acquired"
		previous := ResourceLease{}
		if found {
			generation = current.Generation
			if current.State == resourceLeaseHeld && sameHolder && freshHolder {
				// Reacquisition by the current holder is an idempotent heartbeat,
				// not a fresh claim and never changes its fencing token.
				lease = current
				lease.Purpose, lease.ProjectID, lease.DepartureID = input.Purpose, input.ProjectID, input.DepartureID
				lease.HeartbeatAt, lease.ExpiresAt, lease.UpdatedAt = now.Format(time.RFC3339Nano), now.Add(ttl).Format(time.RFC3339Nano), now.Format(time.RFC3339Nano)
				_, err = tx.Exec(`UPDATE resource_leases SET purpose=?, project_id=?, departure_id=?, heartbeat_at=?, expires_at=?, updated_at=? WHERE resource_name=? AND owner=? AND generation=? AND state='held'`, lease.Purpose, lease.ProjectID, lease.DepartureID, lease.HeartbeatAt, lease.ExpiresAt, lease.UpdatedAt, lease.Name, lease.Owner, lease.Generation)
				if err != nil {
					return err
				}
				acquired = true
				return tx.Commit()
			}
			generation++
			previous = current
			if current.State == resourceLeaseHeld {
				eventType = "taken_over"
			}
		}
		lease = ResourceLease{Name: input.Name, Owner: input.Owner, Purpose: input.Purpose, ProjectID: input.ProjectID, DepartureID: input.DepartureID, HeartbeatAt: now.Format(time.RFC3339Nano), ExpiresAt: now.Add(ttl).Format(time.RFC3339Nano), Generation: generation, State: resourceLeaseHeld, UpdatedAt: now.Format(time.RFC3339Nano)}
		if found {
			_, err = tx.Exec(`UPDATE resource_leases SET owner=?, purpose=?, project_id=?, departure_id=?, heartbeat_at=?, expires_at=?, generation=?, state='held', released_at='', release_reason='', updated_at=? WHERE resource_name=? AND generation=?`, lease.Owner, lease.Purpose, lease.ProjectID, lease.DepartureID, lease.HeartbeatAt, lease.ExpiresAt, lease.Generation, lease.UpdatedAt, lease.Name, previous.Generation)
		} else {
			_, err = tx.Exec(`INSERT INTO resource_leases (resource_name, owner, purpose, project_id, departure_id, heartbeat_at, expires_at, generation, state, released_at, release_reason, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'held', '', '', ?)`, lease.Name, lease.Owner, lease.Purpose, lease.ProjectID, lease.DepartureID, lease.HeartbeatAt, lease.ExpiresAt, lease.Generation, lease.UpdatedAt)
		}
		if err != nil {
			return err
		}
		eventReason := "resource lease acquired"
		if eventType == "taken_over" {
			eventReason = "expired lease recovered"
		} else if found {
			eventReason = "released lease acquired"
		}
		if _, err = tx.Exec(`INSERT INTO resource_lease_events (event_id, resource_name, event_type, owner, generation, previous_owner, previous_generation, reason, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, newRecordID(), lease.Name, eventType, lease.Owner, lease.Generation, previous.Owner, previous.Generation, eventReason, lease.UpdatedAt); err != nil {
			return err
		}
		acquired = true
		return tx.Commit()
	})
	return lease, acquired, err
}

func (s *RuntimeStore) RenewResourceLease(input ResourceLeaseRenewal) (bool, error) {
	if strings.TrimSpace(input.Name) == "" || strings.TrimSpace(input.Owner) == "" || input.Generation <= 0 {
		return false, tuskerError(errorInvalidArg, "resource lease renewal requires name, owner, and generation")
	}
	now, ttl := normalizeResourceLeaseNow(input.Now), normalizeResourceLeaseTTL(input.TTL)
	current, err := s.FindResourceLease(input.Name)
	if err != nil || current == nil {
		return false, err
	}
	expires, err := time.Parse(time.RFC3339Nano, current.ExpiresAt)
	if err != nil {
		expires, err = time.Parse(time.RFC3339, current.ExpiresAt)
	}
	if err != nil || !expires.After(now) {
		return false, nil
	}
	result, err := s.exec(`UPDATE resource_leases SET heartbeat_at=?, expires_at=?, updated_at=? WHERE resource_name=? AND owner=? AND generation=? AND state='held' AND expires_at=?`, now.Format(time.RFC3339Nano), now.Add(ttl).Format(time.RFC3339Nano), now.Format(time.RFC3339Nano), strings.TrimSpace(input.Name), strings.TrimSpace(input.Owner), input.Generation, current.ExpiresAt)
	if err != nil {
		return false, err
	}
	changed, err := result.RowsAffected()
	return changed == 1, err
}

// ResourceLeaseMatches is the fence check runners perform immediately before
// an irreversible gate or release action. A generation that was taken over is
// deliberately indistinguishable from a missing lease to the old holder.
func (s *RuntimeStore) ResourceLeaseMatches(name, owner string, generation int) (bool, error) {
	return s.ResourceLeaseMatchesAt(name, owner, generation, time.Now().UTC())
}

func (s *RuntimeStore) ResourceLeaseMatchesAt(name, owner string, generation int, now time.Time) (bool, error) {
	if strings.TrimSpace(name) == "" || strings.TrimSpace(owner) == "" || generation <= 0 {
		return false, tuskerError(errorInvalidArg, "resource lease match requires name, owner, and generation")
	}
	lease, err := s.FindResourceLease(name)
	if err != nil || lease == nil || lease.Owner != strings.TrimSpace(owner) || lease.Generation != generation || lease.State != resourceLeaseHeld {
		return false, err
	}
	expires, err := time.Parse(time.RFC3339Nano, lease.ExpiresAt)
	if err != nil {
		expires, err = time.Parse(time.RFC3339, lease.ExpiresAt)
	}
	return err == nil && expires.After(normalizeResourceLeaseNow(now)), err
}

// ReleaseResourceLease is terminal-outcome safe: only the exact owner and
// generation can release, so an old runner cannot clear a takeover.
func (s *RuntimeStore) ReleaseResourceLease(name, owner string, generation int, reason string, now time.Time) (bool, error) {
	if strings.TrimSpace(name) == "" || strings.TrimSpace(owner) == "" || generation <= 0 {
		return false, tuskerError(errorInvalidArg, "resource lease release requires name, owner, and generation")
	}
	now = normalizeResourceLeaseNow(now)
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "terminal runner outcome"
	}
	var changed int64
	err := s.withBusyRetry(func() error {
		tx, err := s.db.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback()
		result, err := tx.Exec(`UPDATE resource_leases SET state='released', released_at=?, release_reason=?, updated_at=? WHERE resource_name=? AND owner=? AND generation=? AND state='held'`, now.Format(time.RFC3339Nano), reason, now.Format(time.RFC3339Nano), strings.TrimSpace(name), strings.TrimSpace(owner), generation)
		if err != nil {
			return err
		}
		changed, err = result.RowsAffected()
		if err != nil || changed != 1 {
			return err
		}
		_, err = tx.Exec(`INSERT INTO resource_lease_events (event_id, resource_name, event_type, owner, generation, previous_owner, previous_generation, reason, created_at) VALUES (?, ?, 'released', ?, ?, ?, ?, ?, ?)`, newRecordID(), strings.TrimSpace(name), strings.TrimSpace(owner), generation, strings.TrimSpace(owner), generation, reason, now.Format(time.RFC3339Nano))
		if err != nil {
			return err
		}
		return tx.Commit()
	})
	if err != nil {
		return false, err
	}
	if changed == 1 {
		s.notifyResourceLeaseWaiters(name)
	}
	return changed == 1, nil
}

func (s *RuntimeStore) releaseInactiveTaskResourceLeases(projectID, recordID, reason string, now time.Time) error {
	projectID, recordID = strings.TrimSpace(projectID), strings.TrimSpace(recordID)
	if projectID == "" || recordID == "" {
		return nil
	}
	leases, err := s.ListResourceLeases()
	if err != nil {
		return err
	}
	var releaseErr error
	for _, lease := range leases {
		leaseRecordID, taskDispatch := fairDispatchResourceRecordID(lease)
		if !taskDispatch || lease.State != resourceLeaseHeld ||
			lease.ProjectID != projectID || leaseRecordID != recordID {
			continue
		}
		durableState, _, err := resourceLeaseDurableHolderState(s.db, lease)
		if err != nil {
			releaseErr = errors.Join(releaseErr, err)
			continue
		}
		if durableState == resourceLeaseDurableHolderAlive {
			continue
		}
		if _, err := s.ReleaseResourceLease(lease.Name, lease.Owner, lease.Generation, reason, now); err != nil {
			releaseErr = errors.Join(releaseErr, err)
		}
	}
	return releaseErr
}

// ReconcileExpiredResourceLeases is startup-safe. A verified-live holder is
// left alone; otherwise the durable expiry policy releases it so the next
// acquire creates an attributable, generation-fenced takeover.
func (s *RuntimeStore) ReconcileExpiredResourceLeases(now time.Time, holderAlive func(ResourceLease) bool) ([]ResourceLeaseRecovery, error) {
	now = normalizeResourceLeaseNow(now)
	leases, err := s.ListResourceLeases()
	if err != nil {
		return nil, err
	}
	result := make([]ResourceLeaseRecovery, 0)
	for _, lease := range leases {
		if lease.State != resourceLeaseHeld {
			continue
		}
		expires, err := time.Parse(time.RFC3339Nano, lease.ExpiresAt)
		if err != nil {
			expires, err = time.Parse(time.RFC3339, lease.ExpiresAt)
		}
		if err != nil || expires.After(now) {
			continue
		}
		durableState, durableKind, durableErr := resourceLeaseDurableHolderState(s.db, lease)
		if durableErr != nil {
			return nil, durableErr
		}
		if durableState == resourceLeaseDurableHolderUnknown && holderAlive != nil && holderAlive(lease) {
			result = append(result, ResourceLeaseRecovery{Lease: lease, Reason: "lease expired but holder is verified alive"})
			continue
		}
		if durableState == resourceLeaseDurableHolderAlive {
			reacquired, acquired, acquireErr := s.AcquireResourceLease(ResourceLeaseAcquireInput{
				Name: lease.Name, Owner: lease.Owner, Purpose: lease.Purpose,
				ProjectID: lease.ProjectID, DepartureID: lease.DepartureID,
				TTL: defaultResourceLeaseTTL, Now: now,
			})
			if acquireErr != nil {
				return nil, acquireErr
			}
			result = append(result, ResourceLeaseRecovery{
				Lease: reacquired, Recovered: acquired,
				Reason: "expired resource reacquired for its live " + durableKind + " owner",
			})
			continue
		}
		changed, releaseErr := s.ReleaseResourceLease(lease.Name, lease.Owner, lease.Generation, "daemon restart reconciliation: lease expired", now)
		if releaseErr != nil {
			return nil, releaseErr
		}
		if changed {
			lease.State, lease.ReleasedAt, lease.ReleaseReason, lease.UpdatedAt = resourceLeaseReleased, now.Format(time.RFC3339Nano), "daemon restart reconciliation: lease expired", now.Format(time.RFC3339Nano)
		}
		result = append(result, ResourceLeaseRecovery{Lease: lease, Recovered: changed, Reason: "lease expired"})
	}
	return result, nil
}

func (s *RuntimeStore) FindResourceLease(name string) (*ResourceLease, error) {
	lease, found, err := findResourceLease(s, strings.TrimSpace(name))
	if err != nil || !found {
		return nil, err
	}
	return &lease, nil
}

func (s *RuntimeStore) ListResourceLeases() ([]ResourceLease, error) {
	rows, err := s.query(`SELECT resource_name, owner, purpose, project_id, departure_id, heartbeat_at, expires_at, generation, state, released_at, release_reason, updated_at FROM resource_leases ORDER BY resource_name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var leases []ResourceLease
	for rows.Next() {
		var lease ResourceLease
		if err := scanResourceLease(rows, &lease); err != nil {
			return nil, err
		}
		leases = append(leases, lease)
	}
	return leases, rows.Err()
}

func (s *RuntimeStore) ListResourceLeaseEvents(name string) ([]ResourceLeaseEvent, error) {
	rows, err := s.query(`SELECT event_id, resource_name, event_type, owner, generation, previous_owner, previous_generation, reason, created_at FROM resource_lease_events WHERE resource_name=? ORDER BY created_at, event_id`, strings.TrimSpace(name))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var events []ResourceLeaseEvent
	for rows.Next() {
		var event ResourceLeaseEvent
		if err := rows.Scan(&event.EventID, &event.ResourceName, &event.EventType, &event.Owner, &event.Generation, &event.PreviousOwner, &event.PreviousGeneration, &event.Reason, &event.CreatedAt); err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

type resourceLeaseScanner interface{ Scan(...any) error }

func scanResourceLease(scanner resourceLeaseScanner, lease *ResourceLease) error {
	return scanner.Scan(&lease.Name, &lease.Owner, &lease.Purpose, &lease.ProjectID, &lease.DepartureID, &lease.HeartbeatAt, &lease.ExpiresAt, &lease.Generation, &lease.State, &lease.ReleasedAt, &lease.ReleaseReason, &lease.UpdatedAt)
}
func findResourceLease(s *RuntimeStore, name string) (ResourceLease, bool, error) {
	var lease ResourceLease
	err := s.queryRowScan(`SELECT resource_name, owner, purpose, project_id, departure_id, heartbeat_at, expires_at, generation, state, released_at, release_reason, updated_at FROM resource_leases WHERE resource_name=?`, []any{name}, &lease.Name, &lease.Owner, &lease.Purpose, &lease.ProjectID, &lease.DepartureID, &lease.HeartbeatAt, &lease.ExpiresAt, &lease.Generation, &lease.State, &lease.ReleasedAt, &lease.ReleaseReason, &lease.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return ResourceLease{}, false, nil
	}
	return lease, err == nil, err
}
func findResourceLeaseTx(tx *sql.Tx, name string) (ResourceLease, bool, error) {
	var lease ResourceLease
	err := tx.QueryRow(`SELECT resource_name, owner, purpose, project_id, departure_id, heartbeat_at, expires_at, generation, state, released_at, release_reason, updated_at FROM resource_leases WHERE resource_name=?`, name).Scan(&lease.Name, &lease.Owner, &lease.Purpose, &lease.ProjectID, &lease.DepartureID, &lease.HeartbeatAt, &lease.ExpiresAt, &lease.Generation, &lease.State, &lease.ReleasedAt, &lease.ReleaseReason, &lease.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return ResourceLease{}, false, nil
	}
	return lease, err == nil, err
}
