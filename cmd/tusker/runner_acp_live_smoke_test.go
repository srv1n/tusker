package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// This opt-in test exercises the actual sealed adapter, authenticated Codex
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
	if writeSmoke {
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
	result, err := runner.Start(context.Background(), wrapper.Start)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if result.PGID > 0 && processExists(result.PID) {
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
	if status.ExitCode != 0 || (status.Outcome != "" && status.Outcome != string(AttemptOutcomeNone)) {
		raw, _ := readText(wrapper.Start.RawLogPath)
		events, _ := readText(wrapper.Start.EventSinkPath)
		wrapperLog, _ := readText(runnerWrapperLogPath(wrapper.Start))
		t.Fatalf("live ACP smoke failed: status=%#v\nraw=%s\nwrapper=%s\nevents=%s", status, raw, wrapperLog, events)
	}
	events, err := readText(wrapper.Start.EventSinkPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range []string{"acp_protocol_negotiated", "acp_codex_config_applied", "acp_turn_terminal"} {
		if !strings.Contains(events, event) {
			t.Fatalf("live ACP smoke omitted %s: %s", event, events)
		}
	}
	if writeSmoke {
		written, readErr := os.ReadFile(filepath.Join(wrapper.Start.WorkspacePath, "acp-write-smoke.txt"))
		if readErr != nil || string(written) != writeSmokeContent {
			t.Fatalf("live ACP workspace-write proof = %q err=%v", written, readErr)
		}
	}
	if receiptPath := os.Getenv("TUSKER_LIVE_CODEX_ACP_RECEIPT"); receiptPath != "" {
		receipt, marshalErr := json.Marshal(map[string]any{
			"schema": "tusker.codex-acp-live-smoke/v1", "ok": true,
			"runner": string(RunnerCodexACP), "adapter_version": definition.AdapterVersion,
			"profile": profile.Name, "model": profile.Definition.Model, "effort": profile.Definition.Effort,
			"exit_code": status.ExitCode, "outcome": status.Outcome, "workspace_write": writeSmoke,
		})
		if marshalErr != nil || os.WriteFile(receiptPath, append(receipt, '\n'), 0o600) != nil {
			t.Fatal("write live ACP smoke receipt")
		}
	}
}
