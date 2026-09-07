package main

import (
	"net/http"
	"sort"
	"strings"
	"time"
)

const waveExecuteDirectiveTTL = 7 * 24 * time.Hour

type serveWaveExecuteReceipt struct {
	WaveID                   string   `json:"waveId"`
	AuthorizationFingerprint string   `json:"authorizationFingerprint"`
	QueuedTaskIDs            []string `json:"queuedTaskIds"`
	AlreadyQueuedTaskIDs     []string `json:"alreadyQueuedTaskIds"`
	StatusLink               string   `json:"statusLink"`
	ProjectPollingEnabled    bool     `json:"projectPollingEnabled"`
}

type serveWaveExecuteResult struct {
	serveActionResult
	Execution *serveWaveExecuteReceipt `json:"execution,omitempty"`
}

func (s *serveServer) handleWaveExecute(w http.ResponseWriter, waveID string, body serveActionBody) {
	_, project, err := serveBaseArgsForBody(s, body)
	if err != nil {
		s.serveWaveExecuteFailure(w, err)
		return
	}
	actor, err := s.serveOperatorActor(body, "serve wave execute")
	if err != nil {
		s.serveWaveExecuteFailure(w, err)
		return
	}
	pollingWasEnabled := project.Enabled
	snapshot, err := s.loadSnapshotForProject(project.ProjectID)
	if err != nil {
		s.serveWaveExecuteFailure(w, err)
		return
	}
	if snapshot.workflow.AutomationEnabled {
		s.serveWaveExecuteRefusal(w, "project automation is already enabled; use the autonomous armed-wave scheduler instead of a manual wave execution")
		return
	}
	if !snapshot.workflow.DispatchScope.isArmedWaves() {
		s.serveWaveExecuteRefusal(w, "manual wave execution requires automation.dispatch_scope: armed_waves")
		return
	}
	status, err := s.store.DaemonStatus()
	if err != nil {
		s.serveWaveExecuteFailure(w, err)
		return
	}
	if !boolFromAny(status["daemon_alive"]) {
		s.serveWaveExecuteRefusal(w, "resident daemon is not running")
		return
	}

	idx, err := loadV7Index(project.VaultRoot)
	if err != nil {
		s.serveWaveExecuteFailure(w, err)
		return
	}
	waveID = strings.ToUpper(strings.TrimSpace(waveID))
	wave, ok := idx.Waves[waveID]
	if !ok {
		s.serveWaveExecuteRefusal(w, "wave not found")
		return
	}
	authorization := waveAuthorizationProjection(project.VaultRoot, idx, wave)
	fingerprint := stringField(authorization, "fingerprint")
	state := stringField(authorization, "state")
	if state != "armed" && state != "disarmed" && state != "stale" {
		s.serveWaveExecuteRefusal(w, "wave is not ready to execute")
		return
	}
	if state != "armed" || boolFromAny(authorization["stale"]) {
		manualInspector := func(vaultPath string, candidate Note) wavePreflightEnvironment {
			env := inspectWavePreflightEnvironmentReadOnly(vaultPath, candidate)
			// handleWaveExecute already proved the resident daemon is alive. A manual
			// Execute should not be blocked by the autonomous scheduler's project-health checks.
			env.ProjectEnabled = true
			env.ProjectHealthy = true
			env.DaemonReconciling = true
			return env
		}
		if _, err := mutateWaveAuthorizationWithInspector(Args{"vault": project.VaultRoot, "_pos0": waveID, "by": actor, "quiet": "true"}, "armed", manualInspector, nil); err != nil {
			s.serveWaveExecuteFailure(w, err)
			return
		}
		idx, err = loadV7Index(project.VaultRoot)
		if err != nil {
			s.serveWaveExecuteFailure(w, err)
			return
		}
		wave = idx.Waves[waveID]
		authorization = waveAuthorizationProjection(project.VaultRoot, idx, wave)
		fingerprint = stringField(authorization, "fingerprint")
		if stringField(authorization, "state") != "armed" || fingerprint == "" {
			s.serveWaveExecuteRefusal(w, "wave changed while it was being queued")
			return
		}
	}

	runs := map[string]RunStatus{}
	for _, run := range snapshot.runs {
		runs[run.RecordID] = run
	}
	frontier := buildArmedWaveSnapshot(project.VaultRoot, idx, wave, runs, s.now().UTC())
	eligible := []string{}
	for _, member := range frontier.Members {
		switch member.State {
		case armedWaveRunnable, armedWaveDependencyWaiting:
			eligible = append(eligible, member.ID)
		case armedWaveRunning, armedWaveReview, armedWaveLanded:
			// Already progressing or complete; replay stays idempotent.
		default:
			s.serveWaveExecuteRefusal(w, member.ID+" is not executable: "+member.Reason)
			return
		}
	}
	sort.Strings(eligible)
	now := s.now().UTC()
	queued, already := []string{}, []string{}
	if len(eligible) > 0 {
		queued, already, err = s.store.QueueWaveRunDirectives(project.ProjectID, waveID, fingerprint, stringField(wave.Data, "authorized_at"), actor, eligible, now, waveExecuteDirectiveTTL, !pollingWasEnabled)
		if err != nil {
			s.serveWaveExecuteFailure(w, err)
			return
		}
	}

	s.invalidateProjectSnapshot(project.ProjectID)
	changes := make([]daemonControlChange, 0, len(eligible))
	for _, taskID := range eligible {
		changes = append(changes, daemonControlChange{ID: taskID, Kind: "run", Eligibility: []string{"runtime", "authorization"}})
	}
	_ = sendDaemonControlOneWay(DefaultStateRoot(), daemonControlRequest{Command: "reconcile_project", ProjectID: project.ProjectID, Cause: "wave_execute", Changes: changes}, 250*time.Millisecond)
	receipt := &serveWaveExecuteReceipt{WaveID: waveID, AuthorizationFingerprint: fingerprint, QueuedTaskIDs: queued, AlreadyQueuedTaskIDs: already, StatusLink: waveDeepLink(stringField(wave.Data, "project"), waveID), ProjectPollingEnabled: true}
	serveJSON(w, http.StatusOK, serveWaveExecuteResult{serveActionResult: serveActionResult{OK: true, Reason: "wave queued for exact-scope daemon execution", Command: "tusker wave execute", ProjectID: project.ProjectID}, Execution: receipt})
}

func (s *serveServer) serveWaveExecuteRefusal(w http.ResponseWriter, reason string) {
	serveJSON(w, http.StatusOK, serveWaveExecuteResult{serveActionResult: serveActionResult{Refused: true, Reason: reason, Command: "tusker wave execute"}})
}

func (s *serveServer) serveWaveExecuteFailure(w http.ResponseWriter, err error) {
	issue := errorToIssue(err)
	serveJSON(w, http.StatusOK, serveWaveExecuteResult{serveActionResult: serveActionResult{Refused: true, Reason: issue.Message, Command: "tusker wave execute", Issue: &issue}})
}

func runDirectiveMatchesTaskAuthority(vaultPath string, task Note, directive *RunDirective, now time.Time) bool {
	if !runDirectiveActive(directive, now) {
		return false
	}
	if directive.WaveID == "" && directive.AuthorizationFingerprint == "" {
		return true
	}
	if directive.WaveID == "" || directive.AuthorizationFingerprint == "" || directive.WaveAuthorizedAt == "" || stringField(task.Data, "wave") != directive.WaveID {
		return false
	}
	wave, _, armed := armedWaveForTask(vaultPath, task)
	return armed && stringField(wave.Data, "authorization_fingerprint") == directive.AuthorizationFingerprint && stringField(wave.Data, "authorized_at") == directive.WaveAuthorizedAt
}
