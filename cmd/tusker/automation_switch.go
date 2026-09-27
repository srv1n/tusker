package main

import (
	"fmt"
	"strings"
	"time"
)

// The global automation switch (F11). Off means nothing dispatches in any
// project and every running worker is interrupted; the daemon and Serve stay
// up. It is stored in daemon_settings and every flip writes an audit row to
// project_automation_audit under the reserved project id "*".
const (
	globalAutomationOffSettingKey = "automation_global_off"
	globalAutomationAuditScope    = "*"
	globalAutomationOffReason     = "automation is off globally; run `tusker automation on` to resume dispatch"
)

func (s *RuntimeStore) GlobalAutomationEnabled() (bool, error) {
	raw, err := s.GetSetting(globalAutomationOffSettingKey)
	return strings.TrimSpace(raw) != "1", err
}

// SetGlobalAutomationAudited flips the switch and records actor, source,
// before, after, and time in one transaction.
func (s *RuntimeStore) SetGlobalAutomationAudited(enabled bool, actor, source string) (beforeEnabled bool, err error) {
	err = s.withBusyRetry(func() error {
		tx, txErr := s.db.Begin()
		if txErr != nil {
			return txErr
		}
		defer tx.Rollback()
		var raw string
		_ = tx.QueryRow(`SELECT value FROM daemon_settings WHERE key = ?`, globalAutomationOffSettingKey).Scan(&raw)
		beforeEnabled = strings.TrimSpace(raw) != "1"
		value := ""
		if !enabled {
			value = "1"
		}
		if _, txErr = tx.Exec(`INSERT INTO daemon_settings (key, value) VALUES (?, ?)
			ON CONFLICT(key) DO UPDATE SET value = excluded.value`, globalAutomationOffSettingKey, value); txErr != nil {
			return txErr
		}
		if _, txErr = tx.Exec(`INSERT INTO project_automation_audit(event_id, project_id, actor, source, before_enabled, after_enabled, created_at) VALUES(?,?,?,?,?,?,?)`,
			"automation-"+strings.ToLower(newRecordID()), globalAutomationAuditScope,
			strings.TrimSpace(actor), strings.TrimSpace(source),
			boolToInt(beforeEnabled), boolToInt(enabled), time.Now().UTC().Format(time.RFC3339Nano)); txErr != nil {
			return txErr
		}
		return tx.Commit()
	})
	return beforeEnabled, err
}

func (d *Daemon) globalAutomationBlocker() (string, error) {
	if d == nil || d.store == nil {
		return "", nil
	}
	enabled, err := d.store.GlobalAutomationEnabled()
	if err != nil || enabled {
		return "", err
	}
	return globalAutomationOffReason, nil
}

func automationOnCmd(args Args) error  { return setGlobalAutomationCmd(args, true) }
func automationOffCmd(args Args) error { return setGlobalAutomationCmd(args, false) }

func setGlobalAutomationCmd(args Args, enabled bool) error {
	operation := "automation off"
	if enabled {
		operation = "automation on"
	}
	if err := requireOwnerSession(operation); err != nil {
		return err
	}
	stateRoot := DefaultStateRoot()
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		return err
	}
	defer store.Close()
	before, err := store.SetGlobalAutomationAudited(enabled, defaultActorName(), "cli")
	if err != nil {
		return err
	}
	interrupted := []string{}
	failures := map[string]string{}
	if !enabled {
		interrupted, failures, err = interruptAllRunningWorkers(stateRoot, store)
		if err != nil {
			return err
		}
	}
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": len(failures) == 0, "automation_enabled": enabled, "before_enabled": before, "interrupted": interrupted, "interrupt_failures": failures})
		return nil
	}
	if enabled {
		fmt.Println("Automation is on. Dispatch resumes; interrupted runs stay stopped until you Continue them.")
		return nil
	}
	fmt.Printf("Automation is off. Nothing dispatches in any project. Interrupted %d running worker(s).\n", len(interrupted))
	for id, reason := range failures {
		fmt.Printf("  could not interrupt %s: %s\n", id, reason)
	}
	return nil
}

// interruptAllRunningWorkers sends every daemon-owned claimed/running run
// through the same interrupt path as `runs interrupt`, which keeps the
// native session ID so Continue can resume it. Hand runs are the owner's own
// sessions, not workers, and are left alone.
// ponytail: one sweep at flip time; a candidate dispatched in the same instant
// the switch flips can slip past it. Add a poll-time sweep if that shows up.
func interruptAllRunningWorkers(stateRoot string, store *RuntimeStore) ([]string, map[string]string, error) {
	runs, err := store.ListRuns()
	if err != nil {
		return nil, nil, err
	}
	interrupted := []string{}
	failures := map[string]string{}
	for _, run := range runs {
		if run.Terminal || run.HandRun || !isDispatchingLeaseState(run.LeaseState) {
			continue
		}
		id := firstNonEmpty(run.ItemID, run.RecordID)
		if _, _, err := interruptRuntimeRunScoped(stateRoot, store, run.ProjectID, run.RecordID); err != nil {
			failures[id] = err.Error()
			continue
		}
		interrupted = append(interrupted, id)
	}
	return interrupted, failures, nil
}
