package main

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// departureWindowGrace permits the resident timer to arrive a little after a
// wall-clock boundary without treating ordinary scheduler jitter as a restart
// misfire. A genuinely missed staging/shadow window is recorded as skipped;
// promotion coalesces only its newest missed window.
const departureWindowGrace = time.Minute

const (
	departureHoldGlobalKey        = "departure.hold.global"
	departureHoldReleaseGlobalKey = "departure.hold.release.global"
	departureHoldProjectPrefix    = "departure.hold.project."
	departureHoldReleasePrefix    = "departure.hold.release.project."
)

// DepartureHold is deliberately stored in the runtime DB, not task markdown:
// it is an operator control affecting daemon-owned work across restarts.
type DepartureHold struct {
	Scope        string `json:"scope"`
	ProjectID    string `json:"project_id,omitempty"`
	ReleaseOnly  bool   `json:"release_only,omitempty"`
	Reason       string `json:"reason"`
	By           string `json:"by"`
	CreatedAt    string `json:"created_at"`
	ResumeAction string `json:"resume_action"`
	ClearedAt    string `json:"cleared_at,omitempty"`
	ClearedBy    string `json:"cleared_by,omitempty"`
}

type departureSchedule struct {
	ProjectID string
	Next      time.Time
}

func departurePolicyID(policy ScheduledPromotionProjection) string {
	return fmt.Sprintf("scheduled-promotion/v%d/%s", scheduledPromotionPolicyVersion, policy.Mode)
}

func departureHoldSetting(projectID string, releaseOnly bool) string {
	projectID = strings.TrimSpace(projectID)
	if releaseOnly {
		if projectID == "" {
			return departureHoldReleaseGlobalKey
		}
		return departureHoldReleasePrefix + projectID
	}
	if projectID == "" {
		return departureHoldGlobalKey
	}
	return departureHoldProjectPrefix + projectID
}

func departureResumeAction(projectID string, releaseOnly bool) string {
	if releaseOnly {
		if projectID == "" {
			return "tusker departure resume --release-only --by <actor>"
		}
		return "tusker departure resume --project " + projectID + " --release-only --by <actor>"
	}
	if projectID == "" {
		return "tusker departure resume --by <actor>"
	}
	return "tusker departure resume --project " + projectID + " --by <actor>"
}

func (s *RuntimeStore) SetDepartureHold(projectID string, releaseOnly bool, reason string, by string, now time.Time) (DepartureHold, error) {
	reason, by = strings.TrimSpace(reason), strings.TrimSpace(by)
	if reason == "" || by == "" {
		return DepartureHold{}, fmt.Errorf("departure hold requires reason and actor")
	}
	projectID = strings.TrimSpace(projectID)
	scope := "global"
	if projectID != "" {
		scope = "project"
	}
	hold := DepartureHold{Scope: scope, ProjectID: projectID, ReleaseOnly: releaseOnly, Reason: reason, By: by, CreatedAt: now.UTC().Format(time.RFC3339Nano), ResumeAction: departureResumeAction(projectID, releaseOnly)}
	raw, err := json.Marshal(hold)
	if err != nil {
		return DepartureHold{}, err
	}
	return hold, s.SetSetting(departureHoldSetting(projectID, releaseOnly), string(raw))
}

func (s *RuntimeStore) ClearDepartureHold(projectID string, releaseOnly bool) error {
	return s.SetSetting(departureHoldSetting(projectID, releaseOnly), "")
}

func (s *RuntimeStore) ResumeDepartureHold(projectID string, releaseOnly bool, by string, now time.Time) (DepartureHold, error) {
	key := departureHoldSetting(projectID, releaseOnly)
	raw, err := s.GetSetting(key)
	if err != nil {
		return DepartureHold{}, err
	}
	var hold DepartureHold
	if strings.TrimSpace(raw) == "" {
		return DepartureHold{}, tuskerError(errorNotFound, "no matching departure hold is active")
	}
	if err := json.Unmarshal([]byte(raw), &hold); err != nil {
		return DepartureHold{}, fmt.Errorf("decode %s: %w", key, err)
	}
	if hold.ClearedAt != "" {
		return DepartureHold{}, tuskerError(errorNotFound, "no matching departure hold is active")
	}
	by = strings.TrimSpace(by)
	if by == "" {
		return DepartureHold{}, tuskerError(errorMissingArg, "departure resume requires --by")
	}
	hold.ClearedAt, hold.ClearedBy = now.UTC().Format(time.RFC3339Nano), by
	encoded, err := json.Marshal(hold)
	if err != nil {
		return DepartureHold{}, err
	}
	return hold, s.SetSetting(key, string(encoded))
}

func (s *RuntimeStore) departureHold(projectID string, releaseOnly bool) (*DepartureHold, error) {
	keys := []string{departureHoldSetting("", releaseOnly), departureHoldSetting(projectID, releaseOnly)}
	for _, key := range keys {
		raw, err := s.GetSetting(key)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(raw) == "" {
			continue
		}
		var hold DepartureHold
		if err := json.Unmarshal([]byte(raw), &hold); err != nil {
			return nil, fmt.Errorf("decode %s: %w", key, err)
		}
		if hold.ClearedAt != "" {
			continue
		}
		if hold.ResumeAction == "" {
			hold.ResumeAction = departureResumeAction(hold.ProjectID, hold.ReleaseOnly)
		}
		return &hold, nil
	}
	return nil, nil
}

func departureWindows(wf Workflow) ([]mergeWindow, error) {
	if !wf.ScheduledPromotion.Effective.Observe {
		return nil, nil
	}
	// Scheduled promotion deliberately uses the batch-gate clock. There is one
	// daily-window/DST implementation, and a missing timetable remains inert.
	if len(wf.Orchestration.BatchGate.Windows) == 0 {
		return nil, nil
	}
	return parseMergeWindows(wf.Orchestration.BatchGate.Windows)
}

func (d *Daemon) refreshDepartureSchedule(projectID string, wf Workflow, now time.Time) error {
	windows, err := departureWindows(wf)
	if err != nil {
		return err
	}
	d.departureMu.Lock()
	defer d.departureMu.Unlock()
	if d.departureSchedules == nil {
		d.departureSchedules = map[string]departureSchedule{}
	}
	if len(windows) == 0 {
		delete(d.departureSchedules, projectID)
		return nil
	}
	d.departureSchedules[projectID] = departureSchedule{ProjectID: projectID, Next: mergeWindowNext(windows, now)}
	return nil
}

func (d *Daemon) departureProjectsDue(now time.Time) []string {
	if d == nil {
		return nil
	}
	d.departureMu.Lock()
	defer d.departureMu.Unlock()
	var due []string
	for projectID, schedule := range d.departureSchedules {
		if !schedule.Next.After(now) {
			due = append(due, projectID)
		}
	}
	sort.Strings(due)
	return due
}

func (d *Daemon) nextDepartureWait(now time.Time, fallback time.Duration) time.Duration {
	if d == nil {
		return fallback
	}
	d.departureMu.Lock()
	defer d.departureMu.Unlock()
	for _, schedule := range d.departureSchedules {
		wait := schedule.Next.Sub(now)
		if wait < 0 {
			wait = 0
		}
		if fallback <= 0 || wait < fallback {
			fallback = wait
		}
	}
	return fallback
}

func (d *Daemon) planScheduledDeparture(project RegisteredProject, wf Workflow) (DepartureDecision, error) {
	if d != nil && d.departurePlan != nil {
		return d.departurePlan(project, wf)
	}
	return defaultDeparturePlanner().PlanDeparture(project.VaultRoot, project.ProjectID, WorkflowFile{Data: wf})
}

func (d *Daemon) scheduleDepartureIfDue(project RegisteredProject, wf Workflow, now time.Time) error {
	policy := wf.ScheduledPromotion.Effective
	if _, err := d.store.ReconcileDepartureRunsForProject(project, d.activeDepartureExecutionIDs()...); err != nil {
		return err
	}
	if err := d.refreshDepartureSchedule(project.ProjectID, wf, now); err != nil {
		return err
	}
	if !policy.Observe {
		return nil
	}
	// A red promotion records its failure intent before it mutates canonical
	// task state. Resume that idempotent phase before a fresh window: the failed
	// window may be in the past after a daemon restart.
	if err := d.resumeRepairingDepartureRoutes(project); err != nil {
		return err
	}
	windows, err := departureWindows(wf)
	if err != nil {
		return err
	}
	if len(windows) == 0 {
		return nil
	}
	window := mergeWindowMostRecent(windows, now)
	windowKey := window.UTC().Format(time.RFC3339Nano)
	policyID := departurePolicyID(policy)
	if existing, err := d.store.FindDepartureRunByWindow(project.ProjectID, policyID, windowKey); err != nil {
		return err
	} else if existing != nil {
		return nil
	}

	run, created, err := d.store.GetOrCreateDepartureRun(DepartureRun{ProjectID: project.ProjectID, PolicyID: policyID, ScheduledWindow: windowKey})
	if err != nil || !created {
		return err
	}
	return d.evaluateScheduledDepartureRun(project, wf, run, window, now)
}

func (d *Daemon) evaluateScheduledDepartureRun(project RegisteredProject, wf Workflow, run DepartureRun, window, now time.Time) error {
	if run.State != DepartureStateDue {
		return nil
	}
	transition := func(state DepartureState, skip, block string, decision *DepartureDecision) error {
		_, err := d.transitionDepartureRun(run, func(next *DepartureRun) {
			next.State, next.SkipReason, next.BlockReason = state, skip, block
			if decision != nil {
				next.Candidate, next.Gate = decision.Candidate, decision.GateIntent
			}
		})
		return err
	}
	if hold, err := d.store.departureHold(project.ProjectID, false); err != nil {
		return err
	} else if hold != nil {
		return transition(DepartureStateBlocked, "", departureHoldBlockReason(hold), nil)
	}
	late := now.Sub(window) > departureWindowGrace
	policy := wf.ScheduledPromotion.Effective
	if late && policy.Mode != scheduledPromotionPromote {
		return transition(DepartureStateSkipped, "missed "+policy.Mode+" window; policy is skip-on-misfire", "", nil)
	}
	decision, err := d.planScheduledDeparture(project, wf)
	if err != nil {
		return err
	}
	reason := departureDecisionReason(decision)
	switch decision.Disposition {
	case "empty", "disabled":
		return transition(DepartureStateSkipped, reason, "", &decision)
	case "blocked", "indeterminate":
		return transition(DepartureStateBlocked, "", reason, &decision)
	default:
		// A later promotion/staging worker owns the irreversible part. The
		// evaluating row is its idempotent, restart-safe handoff.
		return transition(DepartureStateEvaluating, "", "", &decision)
	}
}

func departureHoldBlockReason(hold *DepartureHold) string {
	if hold == nil {
		return ""
	}
	return "departure held by " + hold.By + ": " + hold.Reason + "; resume: " + hold.ResumeAction
}

func (d *Daemon) resumeRepairingDepartureRoutes(project RegisteredProject) error {
	if d == nil || d.store == nil {
		return nil
	}
	runs, err := d.store.ListDepartureRuns(project.ProjectID)
	if err != nil {
		return err
	}
	for i := range runs {
		if runs[i].State != DepartureStateRepairing || runs[i].Gate.Failure.Identity == "" {
			continue
		}
		if err := resumePromotionFailureRouting(project.VaultRoot, d.store, &runs[i]); err != nil {
			return err
		}
	}
	return nil
}

func departureDecisionReason(decision DepartureDecision) string {
	if len(decision.Reasons) > 0 && strings.TrimSpace(decision.Reasons[0].Message) != "" {
		return decision.Reasons[0].Message
	}
	return "departure decision: " + firstNonEmpty(strings.TrimSpace(decision.Disposition), "indeterminate")
}
