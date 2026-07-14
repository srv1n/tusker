package main

import (
	"context"
	"strings"
	"time"
)

const daemonWriteNotifyBuffer = 128

var (
	daemonWriteNotifyDebounce = 350 * time.Millisecond
	daemonWriteNotifyTimeout  = 25 * time.Millisecond
)

func notifyDaemonAfterV7Write(projectID string) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return
	}
	_ = sendDaemonControlOneWay(DefaultStateRoot(), daemonControlRequest{
		Command:   "notify",
		ProjectID: projectID,
	}, daemonWriteNotifyTimeout)
}

func (d *Daemon) queueWriteNotify(projectID string) bool {
	if d == nil || d.writeNotify == nil {
		return false
	}
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return false
	}
	select {
	case d.writeNotify <- projectID:
	default:
		// The periodic sweep remains the fallback if an extreme burst fills the
		// queue. Control requests stay non-blocking either way.
	}
	return true
}

func (d *Daemon) runWriteNotifyLoop(ctx context.Context) {
	if d == nil || d.writeNotify == nil {
		return
	}
	pending := map[string]time.Time{}
	var timer *time.Timer
	var timerC <-chan time.Time
	resetTimer := func() {
		if timer != nil {
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
		}
		if len(pending) == 0 {
			timerC = nil
			return
		}
		var earliest time.Time
		for _, deadline := range pending {
			if earliest.IsZero() || deadline.Before(earliest) {
				earliest = deadline
			}
		}
		delay := time.Until(earliest)
		if delay < 0 {
			delay = 0
		}
		if timer == nil {
			timer = time.NewTimer(delay)
		} else {
			timer.Reset(delay)
		}
		timerC = timer.C
	}
	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case projectID := <-d.writeNotify:
			pending[projectID] = time.Now().Add(daemonWriteNotifyDebounce)
			resetTimer()
		case now := <-timerC:
			for projectID, deadline := range pending {
				if deadline.After(now) {
					continue
				}
				delete(pending, projectID)
				if err := d.reconcileProjectOnce(ctx, projectID); err != nil && d.store != nil {
					_ = d.store.SetSetting("daemon_last_notify_error", projectID+": "+err.Error())
				}
			}
			resetTimer()
		}
	}
}
