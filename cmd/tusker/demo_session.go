package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

var demoSessionScenarios = []string{
	"restart", "kill-worker", "say-hard", "say-soft", "ask-wait",
	"ask-nowait", "stop-continue", "start-fresh", "permission-deny",
}

type demoSessionCheck struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
	Reason string `json:"reason,omitempty"`
}

type demoSessionObservation struct {
	State string `json:"state"`
	At    string `json:"at"`
}

type demoSessionProof struct {
	Scenario     string                   `json:"scenario"`
	Harness      string                   `json:"harness"`
	Label        string                   `json:"proof_label"`
	Status       string                   `json:"status"`
	Reason       string                   `json:"reason,omitempty"`
	NativeBefore string                   `json:"native_session_id_before,omitempty"`
	NativeAfter  string                   `json:"native_session_id_after,omitempty"`
	AttemptIDs   []string                 `json:"attempt_ids"`
	Observations []demoSessionObservation `json:"states_observed"`
	Checks       []demoSessionCheck       `json:"checks"`
	RecordedAt   string                   `json:"recorded_at"`
}

// demoSessionCmd records only observed states and supported actions.
func demoSessionCmd(args Args) (int, error) {
	repo, manifest, err := demoResolveRepo(args, true)
	if err != nil {
		return demoFail(args, demoExitForError(err), err)
	}
	scenario := strings.TrimSpace(args.String("scenario"))
	harness := strings.TrimSpace(args.String("harness"))
	if !demoSessionKnown(scenario) || !demoSessionHarnessKnown(harness) {
		return demoFail(args, demoExitInvalid, tuskerError(errorInvalidArg, "demo session requires a known --scenario and --harness"))
	}
	proof := demoSessionProof{Scenario: scenario, Harness: harness, Label: "live-provider", Status: "unavailable", AttemptIDs: []string{}, Observations: []demoSessionObservation{}, Checks: []demoSessionCheck{}, RecordedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	if fixture := strings.TrimSpace(os.Getenv("TUSKER_DEMO_SESSION_FIXTURE_BIN")); fixture != "" {
		proof, err = demoRunSessionFixture(fixture, harness, scenario)
		if err != nil {
			return demoFail(args, demoExitAssertion, err)
		}
	} else if reason := demoSessionUnsupported(harness, scenario); reason != "" {
		proof.Status, proof.Reason = "unsupported", reason
	} else {
		exec := demoNewExec()
		for _, task := range manifest.Tasks {
			if lease := demoActiveLease(exec, repo, task.TaskID); lease != "none" {
				proof.Status, proof.Reason = "refused", fmt.Sprintf("active run %s: %s", task.TaskID, lease)
				break
			}
		}
		if proof.Status != "refused" {
			if wanted := strings.TrimSpace(args.String("profile")); wanted != "" {
				profile, blockers, routeErr := demoResolveTaskRoute(exec, repo, demoVaultPath(repo), manifest.Tasks["s1"].TaskID)
				if routeErr != nil || len(blockers) > 0 || profile.Profile != wanted || profile.Harness != harness {
					proof.Status, proof.Reason = "refused", fmt.Sprintf("standalone route does not match profile %q and harness %q", wanted, harness)
				}
			}
		}
		if proof.Status != "refused" {
			catalog, loadErr := demoLoadCatalog(exec, repo, demoVaultPath(repo))
			if loadErr != nil {
				proof.Reason = "runner catalog unavailable: " + loadErr.Error()
			} else {
				proof.Reason = "harness not installed or not logged in"
				for _, entry := range catalog {
					if entry.Name == harness {
						proof.Reason = firstNonEmpty(entry.Problem, entry.State, proof.Reason)
						if entry.Available {
							proof.Status = "manual-required"
							proof.Reason = "run the Serve UI checklist; automatic fault injection is not available through the demo command"
						}
						break
					}
				}
			}
			if proof.Status == "manual-required" {
				var capability struct {
					Capability string `json:"capability"`
				}
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				if err := demoHTTPJSON(ctx, "GET", demoServeBaseURL+"/api/capability", "", &capability); err != nil {
					proof.Status, proof.Reason = "refused", "resident daemon unavailable; start `tusker daemon run` in an independent shell"
				} else {
					proof = demoRunSessionLive(repo, manifest, harness, scenario)
				}
			}
		}
	}
	path := filepath.Join(demoDir(repo), "session-"+harness+"-"+scenario+".json")
	if err := demoWriteSessionProof(path, proof); err != nil {
		return demoFail(args, demoExitInternal, err)
	}
	result := map[string]any{"ok": proof.Status == "passed" || proof.Status == "unsupported", "scenario": scenario, "harness": harness, "status": proof.Status, "reason": proof.Reason, "proof": path, "proof_label": proof.Label, "text": fmt.Sprintf("%s/%s: %s (%s); proof %s", harness, scenario, proof.Status, proof.Reason, path)}
	if err := demoEmit(args, result); err != nil {
		return demoExitInternal, err
	}
	if proof.Status == "failed" {
		return demoExitAssertion, nil
	}
	if proof.Status == "refused" || (proof.Status == "unavailable" && args.String("require-harness") == harness) {
		return demoExitPrecondition, nil
	}
	return demoExitOK, nil
}

// The fixture executable emits observed run facts, not a verdict. Assertions
// here enforce the same identity and state rules expected of a live provider.
func demoRunSessionFixture(bin, harness, scenario string) (demoSessionProof, error) {
	cmd := exec.Command(bin, harness, scenario)
	raw, err := cmd.Output()
	if err != nil {
		return demoSessionProof{}, fmt.Errorf("session fixture %s/%s: %w", harness, scenario, err)
	}
	var proof demoSessionProof
	if err := json.Unmarshal(raw, &proof); err != nil {
		return demoSessionProof{}, err
	}
	if proof.Scenario != scenario || proof.Harness != harness {
		return demoSessionProof{}, fmt.Errorf("fixture identity mismatch for %s/%s", harness, scenario)
	}
	proof.Label = "fixture"
	proof.RecordedAt = time.Now().UTC().Format(time.RFC3339Nano)
	if proof.AttemptIDs == nil {
		proof.AttemptIDs = []string{}
	}
	if proof.Observations == nil {
		proof.Observations = []demoSessionObservation{}
	}
	proof.Checks = demoSessionChecks(proof)
	proof.Status = "passed"
	for _, check := range proof.Checks {
		if !check.Passed {
			proof.Status = "failed"
			proof.Reason = check.Reason
		}
	}
	return proof, nil
}

func demoSessionChecks(proof demoSessionProof) []demoSessionCheck {
	states := map[string]bool{}
	for _, observation := range proof.Observations {
		if _, err := time.Parse(time.RFC3339Nano, observation.At); err == nil {
			states[observation.State] = true
		}
	}
	check := func(name string, passed bool) demoSessionCheck {
		return demoSessionCheck{Name: name, Passed: passed, Reason: name + " was not observed"}
	}
	checks := []demoSessionCheck{check("native session before", proof.NativeBefore != ""), check("attempt recorded", len(proof.AttemptIDs) > 0)}
	if proof.Scenario == "start-fresh" {
		checks = append(checks, check("new native session", proof.NativeAfter != "" && proof.NativeAfter != proof.NativeBefore))
	} else {
		checks = append(checks, check("same native session", proof.NativeAfter == proof.NativeBefore))
	}
	required := map[string][]string{
		"restart": {"Working"}, "kill-worker": {"Lost", "Working"},
		"say-hard": {"Working"}, "say-soft": {"Working"},
		"ask-wait": {"Waiting on you", "Working"}, "ask-nowait": {"Stopped", "Working"},
		"stop-continue": {"Stopped", "Working"}, "start-fresh": {"Stopped", "Working"},
		"permission-deny": {"Blocked", "Working"},
	}
	for _, state := range required[proof.Scenario] {
		checks = append(checks, check("state "+state, states[state]))
	}
	if proof.Scenario == "restart" {
		checks = append(checks, check("zero new attempts", len(proof.AttemptIDs) == 1))
	}
	if proof.Scenario == "kill-worker" || proof.Scenario == "say-hard" || proof.Scenario == "stop-continue" || proof.Scenario == "start-fresh" {
		checks = append(checks, check("continuation attempt", len(proof.AttemptIDs) >= 2))
	}
	return checks
}

func demoSessionKnown(name string) bool {
	for _, scenario := range demoSessionScenarios {
		if scenario == name {
			return true
		}
	}
	return false
}

func demoSessionHarnessKnown(name string) bool {
	switch name {
	case "claude-code", "codex_exec", "muse", "devin":
		return true
	}
	return false
}

func demoSessionUnsupported(harness, scenario string) string {
	if scenario == "say-soft" && harness != "claude-code" {
		return harness + " driver does not declare soft Say; use hard Say or deliver on Continue"
	}
	return ""
}

func demoWriteSessionProof(path string, proof demoSessionProof) error {
	raw, err := json.MarshalIndent(proof, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(raw, '\n'), 0o644)
}
