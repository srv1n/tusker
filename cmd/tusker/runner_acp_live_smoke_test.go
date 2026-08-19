package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// This environment-gated test exercises the actual sealed adapter, authenticated Codex
// session, detached wrapper, config negotiation, prompt, and terminal status.
// Ordinary unit runs never contact a provider.
func TestLiveCodexACPPrimarySmoke(t *testing.T) {
	if os.Getenv("TUSKER_LIVE_CODEX_ACP") != "1" {
		t.Skip("set TUSKER_LIVE_CODEX_ACP=1 for the authenticated local smoke")
	}
	liveVault := os.Getenv("TUSKER_LIVE_CODEX_ACP_VAULT")
	wrapperExecutable := os.Getenv("TUSKER_LIVE_CODEX_ACP_WRAPPER")
	if liveVault == "" || wrapperExecutable == "" {
		t.Fatal("live ACP smoke requires TUSKER_LIVE_CODEX_ACP_VAULT and TUSKER_LIVE_CODEX_ACP_WRAPPER")
	}
	wrapperFingerprint, err := liveACPWrapperFingerprint(wrapperExecutable)
	if err != nil {
		t.Fatal(err)
	}
	wfFile, err := loadWorkflow(liveVault)
	if err != nil {
		t.Fatal(err)
	}
	definition, ok := wfFile.Data.Runners[string(RunnerCodexACP)]
	if !ok {
		t.Fatal("live project has no codex_acp definition")
	}
	profileName := "review-independent"
	writeSmoke := os.Getenv("TUSKER_LIVE_CODEX_ACP_WRITE") == "1"
	credentialIsolationSmoke := os.Getenv("TUSKER_LIVE_CODEX_ACP_CREDENTIAL_ISOLATION") == "1"
	if credentialIsolationSmoke {
		writeSmoke = true
	}
	if writeSmoke {
		profileName = "execute-standard"
	}
	profileDefinition, ok := wfFile.Data.RunnerProfiles[profileName]
	if !ok || RunnerName(profileDefinition.Harness) != RunnerCodexACP {
		t.Fatalf("live project has no codex_acp %s profile", profileName)
	}
	profile := ResolvedRunnerProfile{Name: profileName, Source: configSourceLocal, Reason: "authenticated smoke", Definition: profileDefinition}

	store, wrapper := setupRunnerWrapperRuntime(t)
	prompt := "Reply with a brief confirmation that the ACP smoke turn completed. Do not use tools or modify files.\n"
	const writeSmokeContent = "tusker codex acp workspace write smoke\n"
	const credentialIsolationContent = "OPENAI_API_KEY=absent\nCODEX_API_KEY=absent\nCODEX_HOME=absent\n"
	if credentialIsolationSmoke {
		helper := `#!/bin/sh
set -eu
output=auth-env-result.txt
: > "$output"
if [ "${OPENAI_API_KEY+x}" = x ]; then echo OPENAI_API_KEY=present >> "$output"; else echo OPENAI_API_KEY=absent >> "$output"; fi
if [ "${CODEX_API_KEY+x}" = x ]; then echo CODEX_API_KEY=present >> "$output"; else echo CODEX_API_KEY=absent >> "$output"; fi
if [ "${CODEX_HOME+x}" = x ]; then echo CODEX_HOME=present >> "$output"; else echo CODEX_HOME=absent >> "$output"; fi
`
		if err := os.WriteFile(filepath.Join(wrapper.Start.WorkspacePath, "check-auth-env.sh"), []byte(helper), 0o500); err != nil {
			t.Fatal(err)
		}
		prompt = "Run ./check-auth-env.sh exactly once. It must create auth-env-result.txt. Do not print environment values and do not modify any other file.\n"
	} else if writeSmoke {
		prompt = "Create a file named acp-write-smoke.txt in the current workspace with exactly this single line and no other changes: tusker codex acp workspace write smoke\n"
	}
	if err := writeText(wrapper.Start.PromptPath, prompt); err != nil {
		t.Fatal(err)
	}
	registerCodexACPWrapperAdmission(t, store, wrapper, definition, profile)
	runner, _, err := runnerForName(string(RunnerCodexACP), Workflow{Runners: map[string]RunnerDefinition{string(RunnerCodexACP): definition}})
	if err != nil {
		t.Fatal(err)
	}
	codexRunner := runner.(*CodexACPRunner)
	plan, err := codexRunner.admission.withProfile(profile)
	if err != nil {
		t.Fatal(err)
	}
	descriptor, argv, err := plan.descriptorAndArgv()
	if err != nil {
		t.Fatal(err)
	}
	wrapper.Runner = string(RunnerCodexACP)
	wrapper.Start.Command = argv[0]
	wrapper.Start.CommandArgv = argv
	wrapper.Start.CommandExecutableFP = descriptor.Adapter.Fingerprint
	wrapper.Start.RawLogMaxBytes = 2 << 20
	wrapper.Start.RunnerProfile = profile.Name
	wrapper.Start.RunnerHarness = string(RunnerCodexACP)
	wrapper.Start.RunnerModel = descriptor.Model
	wrapper.Start.RunnerEffort = descriptor.Effort
	wrapper.Start.CodexACP = &plan
	wrapper.Start.CodexPolicy = codexPolicyForResolvedProfile(codexPolicyFromWorkflow(wfFile.Data), wrapper.Start.Lane, profile)
	wrapper.Start.CodexPolicy.TurnTimeoutMS = 180_000
	wrapper.Start.CodexPolicy.StallTimeoutMS = 90_000
	wrapper.Start.CodexPolicy.ReadTimeoutMS = 30_000
	wrapper.Start.CodexPolicy.MaxTurns = 1
	run, err := store.FindRun(wrapper.Start.RecordID)
	if err != nil || run == nil {
		t.Fatalf("find smoke run: run=%#v err=%v", run, err)
	}
	run.Runner = string(RunnerCodexACP)
	run.RunnerProfile = profile.Name
	run.RunnerHarness = string(RunnerCodexACP)
	run.RunnerModel = descriptor.Model
	run.RunnerEffort = descriptor.Effort
	if err := store.UpsertRun(*run); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveAttempt(RunAttempt{
		AttemptID: wrapper.Start.AttemptID, ProjectID: wrapper.Start.ProjectID, RecordID: wrapper.Start.RecordID,
		ItemID: wrapper.Start.ItemID, Runner: string(RunnerCodexACP), Lane: wrapper.Start.Lane,
		WorkRevision: wrapper.Start.WorkRevision, WorkspacePath: wrapper.Start.WorkspacePath,
		Outcome: string(AttemptOutcomeNone), StartedAt: time.Now().UTC().Format(time.RFC3339),
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveRunAuthorization(RunAuthorization{
		ProjectID: wrapper.Start.ProjectID, RecordID: wrapper.Start.RecordID, LeaseGeneration: wrapper.Start.LeaseGeneration,
		Source: "live_smoke", Actor: "human:local-acp-smoke", Trigger: "explicit", CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TUSKER_WRAPPER_EXE", wrapperExecutable)
	smokeStartedAt := time.Now().UTC()
	result, err := runner.Start(context.Background(), wrapper.Start)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if result.PGID > 0 && processExists(result.PID) {
			if result.PGID == syscall.Getpgrp() {
				t.Errorf("refusing live-smoke cleanup of the test process group %d", result.PGID)
				return
			}
			_ = syscall.Kill(-result.PGID, syscall.SIGKILL)
		}
	}()
	deadline := time.Now().Add(4 * time.Minute)
	for time.Now().Before(deadline) && !fileExists(wrapper.Start.StatusPath) {
		time.Sleep(100 * time.Millisecond)
	}
	if !fileExists(wrapper.Start.StatusPath) {
		t.Fatal("timed out waiting for live ACP terminal status")
	}
	status, err := readRunnerProcessStatus(wrapper.Start.StatusPath)
	if err != nil {
		t.Fatal(err)
	}
	// Successful runner status deliberately omits the sentinel "none" outcome.
	if status.ExitCode != 0 || strings.TrimSpace(status.Outcome) != "" {
		t.Fatalf("live ACP smoke failed: exit_code=%d outcome=%q raw_log=%s event_log=%s wrapper_log=%s", status.ExitCode, status.Outcome,
			redactedLiveACPPath(liveVault, wrapper.Start.RawLogPath),
			redactedLiveACPPath(liveVault, wrapper.Start.EventSinkPath),
			redactedLiveACPPath(liveVault, runnerWrapperLogPath(wrapper.Start)))
	}
	events, err := readText(wrapper.Start.EventSinkPath)
	if err != nil {
		t.Fatal(err)
	}
	expectedConfig := CodexACPConfigPlan{Steps: []CodexACPConfigStep{{Semantic: "model", OptionID: "model", Value: descriptor.Model}}}
	if descriptor.Effort != "" {
		expectedConfig.Steps = append(expectedConfig.Steps, CodexACPConfigStep{Semantic: "reasoning_effort", OptionID: "reasoning_effort", Value: descriptor.Effort})
	}
	mode, modeErr := descriptor.Mode.initialAgentMode()
	if modeErr != nil {
		t.Fatal(modeErr)
	}
	expectedConfig.Steps = append(expectedConfig.Steps, CodexACPConfigStep{Semantic: "mode", OptionID: "mode", Value: mode})
	negotiation, err := liveACPNegotiationReceipt(events, wrapper.Start.AttemptID, RunnerCodexACP, descriptor.AdapterVersion, codexACPConfigReceipt(expectedConfig))
	if err != nil {
		t.Fatal(err)
	}
	smokeFinishedAt := time.Now().UTC()
	if credentialIsolationSmoke {
		written, readErr := os.ReadFile(filepath.Join(wrapper.Start.WorkspacePath, "auth-env-result.txt"))
		if readErr != nil || string(written) != credentialIsolationContent {
			t.Fatalf("live ACP child credential-isolation proof mismatch: bytes=%d err=%v path=%s", len(written), readErr,
				redactedLiveACPPath(liveVault, filepath.Join(wrapper.Start.WorkspacePath, "auth-env-result.txt")))
		}
	} else if writeSmoke {
		written, readErr := os.ReadFile(filepath.Join(wrapper.Start.WorkspacePath, "acp-write-smoke.txt"))
		if readErr != nil || string(written) != writeSmokeContent {
			t.Fatalf("live ACP workspace-write proof mismatch: bytes=%d err=%v path=%s", len(written), readErr,
				redactedLiveACPPath(liveVault, filepath.Join(wrapper.Start.WorkspacePath, "acp-write-smoke.txt")))
		}
	}
	if receiptPath := os.Getenv("TUSKER_LIVE_CODEX_ACP_RECEIPT"); receiptPath != "" {
		if current, fingerprintErr := liveACPWrapperFingerprint(wrapperExecutable); fingerprintErr != nil || current != wrapperFingerprint {
			t.Fatal("live ACP wrapper executable changed during smoke")
		}
		sourceRevision, revisionErr := canonicalLiveACPSourceRevision(liveVault)
		if revisionErr != nil {
			t.Fatal(revisionErr)
		}
		receipt, marshalErr := json.Marshal(map[string]any{
			"schema": "tusker.codex-acp-live-smoke/v1", "ok": true,
			"source_revision": sourceRevision, "canonical_revision": sourceRevision,
			"runner": string(RunnerCodexACP), "adapter_version": definition.AdapterVersion,
			"adapter_fingerprint":            descriptor.ManifestFingerprint,
			"adapter_executable_fingerprint": descriptor.Adapter.Fingerprint,
			"wrapper_executable_fingerprint": wrapperFingerprint,
			"bundle_receipt_digest":          plan.BundleReceipt.VerifiedContentDigest,
			"protocol":                       negotiation["protocol"], "agent_name": negotiation["agent_name"],
			"agent_version": negotiation["agent_version"], "capabilities": map[string]any{
				"load_session": negotiation["load_session"], "resume_session": negotiation["resume_session"],
			},
			"profile": profile.Name, "model": descriptor.Model, "effort": descriptor.Effort,
			"exit_code": status.ExitCode, "outcome": status.Outcome, "workspace_write": writeSmoke,
			"credential_isolation": credentialIsolationSmoke,
			"started_at":           smokeStartedAt.Format(time.RFC3339Nano), "finished_at": smokeFinishedAt.Format(time.RFC3339Nano),
			"duration_ms":    smokeFinishedAt.Sub(smokeStartedAt).Milliseconds(),
			"log_path":       redactedLiveACPPath(liveVault, wrapper.Start.RawLogPath),
			"raw_log_path":   redactedLiveACPPath(liveVault, wrapper.Start.RawLogPath),
			"event_log_path": redactedLiveACPPath(liveVault, wrapper.Start.EventSinkPath),
			"status_path":    redactedLiveACPPath(liveVault, wrapper.Start.StatusPath),
		})
		if marshalErr != nil || writeLiveACPReceipt(receiptPath, append(receipt, '\n')) != nil {
			t.Fatal("write live ACP smoke receipt")
		}
	}
}

// liveACPNegotiationReceipt extracts only the bounded, non-secret handshake
// facts already written to the event log. It deliberately does not infer
// capabilities from the configured profile or a static adapter name.
func liveACPNegotiationReceipt(raw, attemptID string, runner RunnerName, expectedVersion, expectedConfigReceipt string) (map[string]any, error) {
	result := map[string]any{"protocol": "acp/v1"}
	found := map[string]bool{}
	scanner := bufio.NewScanner(strings.NewReader(raw))
	for scanner.Scan() {
		var event Event
		if err := json.Unmarshal([]byte(scanner.Text()), &event); err != nil {
			return nil, fmt.Errorf("live ACP event log contains malformed JSON: %w", err)
		}
		if event.AttemptID != attemptID || event.Runner != runner {
			continue
		}
		switch event.Kind {
		case "acp_protocol_negotiated", "acp_codex_config_applied", "acp_turn_terminal":
			if found[event.Kind] {
				return nil, fmt.Errorf("live ACP event log contains duplicate %s", event.Kind)
			}
			found[event.Kind] = true
			if event.Kind != "acp_protocol_negotiated" {
				if event.Kind == "acp_codex_config_applied" {
					steps, stepsOK := event.Payload["steps"].(float64)
					receipt, receiptOK := event.Payload["config_receipt"].(string)
					if !stepsOK || steps < 0 || !receiptOK || receipt != expectedConfigReceipt {
						return nil, errors.New("live ACP config event does not match the admitted configuration receipt")
					}
				} else {
					outcome, outcomeOK := event.Payload["transport_outcome"].(string)
					delivery, deliveryOK := event.Payload["delivery_phase"].(string)
					if !outcomeOK || outcome != "completed" || !deliveryOK || delivery != "terminal_received" {
						return nil, errors.New("live ACP terminal event is not a completed terminal receipt")
					}
				}
				continue
			}
			name, nameOK := event.Payload["agent_name"].(string)
			version, versionOK := event.Payload["agent_version"].(string)
			load, loadOK := event.Payload["load_session"].(bool)
			resume, resumeOK := event.Payload["resume_session"].(bool)
			if !nameOK || name != codexACPAgentName || strings.TrimSpace(name) == "" || len(name) > 256 || !versionOK || version != expectedVersion || strings.TrimSpace(version) == "" || len(version) > 256 || !loadOK || !resumeOK {
				return nil, errors.New("live ACP negotiation event has incomplete typed identity/capability facts")
			}
			result["agent_name"], result["agent_version"] = name, version
			result["load_session"], result["resume_session"] = load, resume
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan live ACP event log: %w", err)
	}
	for _, kind := range []string{"acp_protocol_negotiated", "acp_codex_config_applied", "acp_turn_terminal"} {
		if !found[kind] {
			return nil, fmt.Errorf("live ACP event log omitted %s for attempt %s", kind, attemptID)
		}
	}
	return result, nil
}

func canonicalLiveACPSourceRevision(vault string) (string, error) {
	vault, err := filepath.EvalSymlinks(filepath.Clean(vault))
	if err != nil {
		return "", fmt.Errorf("resolve live ACP vault: %w", err)
	}
	root, err := liveACPGitOutput(filepath.Dir(vault), "rev-parse", "--show-toplevel")
	if err != nil {
		return "", fmt.Errorf("resolve live ACP source repository: %w", err)
	}
	workingRoot, err := liveACPGitOutput(".", "rev-parse", "--show-toplevel")
	if err != nil || filepath.Clean(root) != filepath.Clean(workingRoot) {
		return "", errors.New("live ACP vault is not bound to the current source repository")
	}
	if dirty, _ := liveACPGitOutput(root, "status", "--porcelain"); strings.TrimSpace(dirty) != "" {
		return "", errors.New("live ACP source repository is dirty; receipt revision is not authoritative")
	}
	revision, err := liveACPGitOutput(root, "rev-parse", "--verify", "HEAD^{commit}")
	if err != nil || strings.TrimSpace(revision) == "" {
		return "", errors.New("live ACP source repository has no canonical HEAD")
	}
	return strings.TrimSpace(revision), nil
}

func liveACPGitOutput(dir string, args ...string) (string, error) {
	command := exec.Command("git", append([]string{"-C", dir}, args...)...)
	raw, err := command.Output()
	return strings.TrimSpace(string(raw)), err
}

func liveACPWrapperFingerprint(path string) (string, error) {
	clean := filepath.Clean(path)
	if !filepath.IsAbs(path) || clean != path {
		return "", errors.New("live ACP wrapper must be an absolute canonical path")
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil || resolved != clean {
		return "", errors.New("live ACP wrapper must not be a symlink")
	}
	info, err := os.Stat(clean)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&0o111 == 0 {
		return "", errors.New("live ACP wrapper must be a regular executable")
	}
	return acpExecutableFingerprint(clean)
}

func writeLiveACPReceipt(path string, data []byte) error {
	clean := filepath.Clean(path)
	if !filepath.IsAbs(path) || clean != path {
		return errors.New("live ACP receipt must be an absolute canonical path")
	}
	parent := filepath.Dir(clean)
	parentInfo, err := os.Lstat(parent)
	if err != nil || !parentInfo.IsDir() || parentInfo.Mode()&os.ModeSymlink != 0 {
		return errors.New("live ACP receipt parent must be a real directory")
	}
	temp, err := os.CreateTemp(parent, ".tusker-live-acp-receipt-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		return err
	}
	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Link(tempPath, clean); err != nil {
		return err
	}
	dir, err := os.Open(parent)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func redactedLiveACPPath(root, path string) string {
	root = filepath.Clean(root)
	path = filepath.Clean(path)
	if rel, err := filepath.Rel(root, path); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return rel
	}
	return "<redacted>/" + filepath.Base(path)
}
