package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// demoRunSessionLive drives only controls the resident Serve API can safely
// trigger. A missing fault-injection primitive is a result, never a pass.
func demoRunSessionLive(repo string, manifest *demoManifest, harness, scenario string) demoSessionProof {
	proof := demoSessionProof{Scenario: scenario, Harness: harness, Label: "live-provider", Status: "unavailable", AttemptIDs: []string{}, Observations: []demoSessionObservation{}, Checks: []demoSessionCheck{}, RecordedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	missing := map[string]string{
		"restart": "no scoped daemon-restart fault injection; stopping the shared daemon would affect other projects",
	}
	if reason := missing[scenario]; reason != "" {
		proof.Status, proof.Reason = "unsupported", reason
		return proof
	}
	if reason := demoSessionUnsupported(harness, scenario); reason != "" {
		proof.Status, proof.Reason = "unsupported", reason
		return proof
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	var capability struct {
		Capability string `json:"capability"`
	}
	if err := demoHTTPJSON(ctx, http.MethodGet, demoServeBaseURL+"/api/capability", "", &capability); err != nil {
		proof.Status, proof.Reason = "refused", "resident daemon unavailable; start `tusker daemon run` in an independent shell"
		return proof
	}
	project := manifest.RuntimeProjectID
	task := manifest.Tasks["s1"].TaskID
	wave := manifest.Waves["standalone"].WaveID
	if project == "" || task == "" || wave == "" {
		proof.Status, proof.Reason = "refused", "demo runtime registration or standalone task is missing; reset and reseed"
		return proof
	}
	if scenario == "permission-deny" {
		return demoRunSessionPermission(ctx, repo, project, task, wave, harness, capability.Capability, proof)
	}
	if scenario == "ask-wait" || scenario == "ask-nowait" {
		if err := demoSessionPrepareAsk(repo, scenario); err != nil {
			proof.Status, proof.Reason = "refused", err.Error()
			return proof
		}
		defer demoSessionRemoveAsk(repo)
	}
	if _, err := demoStartWave(ctx, project, wave); err != nil {
		proof.Status, proof.Reason = "refused", "standalone Wave Start: "+err.Error()
		return proof
	}
	endpoint := demoServeBaseURL + "/api/runs/" + url.PathEscape(task) + "?project=" + url.QueryEscape(project)
	var before serveRunDetail
	firstState := "working"
	if scenario == "ask-wait" {
		firstState = "waiting_on_you"
	}
	if err := demoSessionWait(ctx, endpoint, firstState, &before, &proof); err != nil {
		proof.Status, proof.Reason = "failed", err.Error()
		return proof
	}
	proof.NativeBefore = demoSessionNative(before)
	demoSessionAttempts(&proof, before)
	if proof.NativeBefore == "" {
		proof.Status, proof.Reason = "failed", "Working was observed without a native session id"
		return proof
	}
	var after serveRunDetail
	switch scenario {
	case "ask-wait", "ask-nowait":
		if err := demoSessionAsk(ctx, project, task, endpoint, capability.Capability, scenario, &after, &proof); err != nil {
			proof.Status, proof.Reason = "failed", err.Error()
			return proof
		}
	case "say-hard", "say-soft":
		message := "Session demo instruction: acknowledge this message in the run transcript."
		var result serveRunSayResponse
		if err := demoSessionPost(ctx, endpoint, "say", capability.Capability, map[string]string{"message": message}, &result); err != nil || !result.OK || result.Refused {
			proof.Status, proof.Reason = "failed", firstNonEmpty(result.Reason, demoSessionError(err))
			return proof
		}
		if scenario == "say-hard" && result.Route != "hard" || scenario == "say-soft" && result.Route != "soft" {
			proof.Status, proof.Reason = "unsupported", "runner Say route is "+result.Route
			return proof
		}
		if err := demoSessionWait(ctx, endpoint, "working", &after, &proof); err != nil {
			proof.Status, proof.Reason = "failed", err.Error()
			return proof
		}
		if after.LastSayDelivery == nil || after.LastSayDelivery.Body != message {
			proof.Status, proof.Reason = "failed", "Say delivery was not visible on run readback"
			return proof
		}
	case "stop-continue", "start-fresh":
		var stop runSessionControlResult
		if err := demoSessionPost(ctx, endpoint, "control", capability.Capability, map[string]string{"action": "stop"}, &stop); err != nil || stop.Refused || !stop.Supported {
			proof.Status, proof.Reason = "failed", firstNonEmpty(stop.Reason, demoSessionError(err))
			return proof
		}
		if err := demoSessionWait(ctx, endpoint, "stopped", &after, &proof); err != nil {
			proof.Status, proof.Reason = "failed", err.Error()
			return proof
		}
		if scenario == "stop-continue" {
			var result serveRunSayResponse
			if err := demoSessionPost(ctx, endpoint, "continue", capability.Capability, map[string]string{"message": "Continue the session demo."}, &result); err != nil || !result.OK || result.Refused {
				proof.Status, proof.Reason = "failed", firstNonEmpty(result.Reason, demoSessionError(err))
				return proof
			}
		} else {
			var fresh runSessionControlResult
			if err := demoSessionPost(ctx, endpoint, "control", capability.Capability, map[string]string{"action": "start_fresh"}, &fresh); err != nil || fresh.Refused || !fresh.Supported {
				proof.Status, proof.Reason = "failed", firstNonEmpty(fresh.Reason, demoSessionError(err))
				return proof
			}
		}
		if err := demoSessionWait(ctx, endpoint, "working", &after, &proof); err != nil {
			proof.Status, proof.Reason = "failed", err.Error()
			return proof
		}
	case "kill-worker":
		if before.ActiveAttemptID == "" {
			proof.Status, proof.Reason = "failed", "Working run has no active attempt"
			return proof
		}
		if err := demoKillSessionWorker(repo, project, task, before.ActiveAttemptID); err != nil {
			proof.Status, proof.Reason = "refused", err.Error()
			return proof
		}
		if err := demoSessionWait(ctx, endpoint, "lost", &after, &proof); err != nil {
			proof.Status, proof.Reason = "failed", err.Error()
			return proof
		}
		var result serveRunSayResponse
		if err := demoSessionPost(ctx, endpoint, "continue", capability.Capability, map[string]string{"message": "Continue after the demo worker was killed."}, &result); err != nil || !result.OK || result.Refused {
			proof.Status, proof.Reason = "failed", firstNonEmpty(result.Reason, demoSessionError(err))
			return proof
		}
		if err := demoSessionWait(ctx, endpoint, "working", &after, &proof); err != nil {
			proof.Status, proof.Reason = "failed", err.Error()
			return proof
		}
	default:
		proof.Status, proof.Reason = "unsupported", "no live driver for scenario"
		return proof
	}
	proof.NativeAfter = demoSessionNative(after)
	demoSessionAttempts(&proof, after)
	proof.Checks = demoSessionChecks(proof)
	proof.Status = "passed"
	for _, check := range proof.Checks {
		if !check.Passed {
			proof.Status, proof.Reason = "failed", check.Reason
			break
		}
	}
	return proof
}

func demoSessionNative(detail serveRunDetail) string {
	if detail.Session != nil {
		return detail.Session.SessionRef
	}
	return ""
}

func demoSessionAttempts(proof *demoSessionProof, detail serveRunDetail) {
	seen := map[string]bool{}
	for _, id := range proof.AttemptIDs {
		seen[id] = true
	}
	for _, attempt := range detail.Attempts {
		if attempt.ID != "" && !seen[attempt.ID] {
			proof.AttemptIDs = append(proof.AttemptIDs, attempt.ID)
			seen[attempt.ID] = true
		}
	}
}

func demoSessionWait(ctx context.Context, endpoint, state string, detail *serveRunDetail, proof *demoSessionProof) error {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		if err := demoHTTPJSON(ctx, http.MethodGet, endpoint, "", detail); err == nil {
			observed := strings.ReplaceAll(detail.OperatorState.State, "_", " ")
			if observed != "" {
				observed = strings.ToUpper(observed[:1]) + observed[1:]
			}
			if observed != "" && (len(proof.Observations) == 0 || proof.Observations[len(proof.Observations)-1].State != observed) {
				proof.Observations = append(proof.Observations, demoSessionObservation{State: observed, At: time.Now().UTC().Format(time.RFC3339Nano)})
			}
			if detail.OperatorState.State == state {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("timed out waiting for %s", state)
		case <-ticker.C:
		}
	}
}

func demoSessionPost(ctx context.Context, endpoint, action, capability string, body any, out any) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.Replace(endpoint, "?", "/"+action+"?", 1), bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(serveCapabilityHeader, capability)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s returned %s", action, resp.Status)
	}
	return nil
}

func demoSessionError(err error) string {
	if err != nil {
		return err.Error()
	}
	return "control was not acknowledged"
}
