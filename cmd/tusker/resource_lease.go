package main

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
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
	resourceLeaseHeld       = "held"
	resourceLeaseReleased   = "released"
	resourceLeaseRefusal    = "RESOURCE_LEASE_HELD"
	defaultResourceLeaseTTL = 2 * time.Minute
)

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
			if !freshHolder && input.HolderAlive != nil && input.HolderAlive(current) {
				return resourceLeaseContention(current, "lease_expired_holder_alive")
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
	result, err := s.exec(`UPDATE resource_leases SET state='released', released_at=?, release_reason=?, updated_at=? WHERE resource_name=? AND owner=? AND generation=? AND state='held'`, now.Format(time.RFC3339Nano), reason, now.Format(time.RFC3339Nano), strings.TrimSpace(name), strings.TrimSpace(owner), generation)
	if err != nil {
		return false, err
	}
	changed, err := result.RowsAffected()
	return changed == 1, err
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
		if holderAlive != nil && holderAlive(lease) {
			result = append(result, ResourceLeaseRecovery{Lease: lease, Reason: "lease expired but holder is verified alive"})
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
