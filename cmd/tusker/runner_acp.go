package main

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"tusker/internal/acp"
)

const codexACPAgentName = "@agentclientprotocol/codex-acp"

// ACPRunner is the provider-neutral, local ACP v1 transport boundary. It is
// intentionally not a Codex or Claude adapter: provider descriptors, command
// construction, and normalized tool decoders arrive only after their separate
// parity gates. Until then all unrecognized permission requests are rejected.
type ACPRunner struct{ runner RunnerName }

func (r *ACPRunner) Name() RunnerName {
	if r == nil || r.runner == "" {
		return RunnerACP
	}
	return r.runner
}

func (r *ACPRunner) Capabilities() RunnerCapabilities {
	// Only Devin currently supports negotiated session/load; other ACP adapters
	// cannot safely treat a persisted session as resumable.
	return RunnerCapabilities{
		StructuredEvents:   true,
		ResumeSession:      r.Name() == RunnerDevin,
		ExplicitApprovals:  true,
		Heartbeats:         true,
		MachineFinalStatus: true,
		UsageMetrics:       false,
	}
}

// Start always goes through the detached wrapper. The wrapper owns the
// attempt heartbeat, durable PID/PGID receipt, lease fence, and final cleanup;
// an in-process ACP launch would bypass each of those controls.
func (r *ACPRunner) Start(ctx context.Context, req StartRequest) (*StartResult, error) {
	if err := validateACPLaunchRequestForRunner(r.Name(), req); err != nil {
		return nil, err
	}
	return startDetachedRunnerWrapper(ctx, r.Name(), req, nil, r.Capabilities())
}

// Resume uses Devin's negotiated session/load after the daemon has matched the
// saved session to this task, workspace, and revision. Generic ACP stays off.
func (r *ACPRunner) Resume(ctx context.Context, req ResumeRequest) (*ResumeResult, error) {
	if strings.TrimSpace(req.SessionRef) == "" {
		return nil, tuskerError(errorMissingArg, "acp_v1 resume requires session_ref")
	}
	if r.Name() != RunnerDevin {
		return nil, tuskerError(errorInvalidTransition, "acp_v1 session resume requires a provider adapter")
	}
	if _, err := acpRawSessionRef("devin", req.SessionRef); err != nil {
		return nil, err
	}
	start := StartRequest{
		ProjectID: req.ProjectID, RecordID: req.RecordID, ItemID: req.ItemID, AttemptID: req.AttemptID,
		Lane: req.Lane, WorkRevision: req.WorkRevision, LeaseGeneration: req.LeaseGeneration, ActiveStates: req.ActiveStates,
		WorkingDir: req.WorkingDir, WorkspacePath: req.WorkspacePath, RepoRoot: req.RepoRoot,
		PromptPath: req.PromptPath, EventSinkPath: req.EventSinkPath, RawLogPath: req.RawLogPath,
		RawLogMaxBytes: req.RawLogMaxBytes, StatusPath: req.StatusPath, Command: req.Command,
		CommandArgv: req.CommandArgv, CommandExecutableFP: req.CommandExecutableFP, CommandSearchPath: req.CommandSearchPath,
		RunnerPathPrefix: req.RunnerPathPrefix, RunnerProfile: req.RunnerProfile, RunnerHarness: req.RunnerHarness,
		RunnerModel: req.RunnerModel, RunnerEffort: req.RunnerEffort, PrivateFolders: req.PrivateFolders,
		NotePath: req.NotePath, VaultPath: req.VaultPath, CodexPolicy: req.CodexPolicy,
		ExternalLoop: req.ExternalLoop, Principal: req.Principal, Actor: req.Actor,
	}
	if err := validateACPLaunchRequestForRunner(RunnerDevin, start); err != nil {
		return nil, err
	}
	return startDetachedRunnerWrapper(ctx, RunnerDevin, start, &req, r.Capabilities())
}

// Reconcile never infers local process state from an ACP session reference.
// Once the fenced wrapper process is gone, the shared transport cannot prove
// whether a provider turn ran, so it returns a non-resumable abandonment for
// the existing supervisor policy to handle.
func (r *ACPRunner) Reconcile(ctx context.Context, req ReconcileRequest) (*ReconcileResult, error) {
	_ = ctx
	if strings.TrimSpace(req.SessionRef) == "" {
		return &ReconcileResult{LeaseState: LeaseStateReleased, Outcome: AttemptOutcomeAbandoned, Reason: "ACP local attempt has no bound session reference"}, nil
	}
	return &ReconcileResult{LeaseState: LeaseStateReleased, Outcome: AttemptOutcomeAbandoned, Reason: "ACP local transport cannot reconcile a lost fenced process; no automatic resume"}, nil
}

func (r *ACPRunner) Interrupt(ctx context.Context, req InterruptRequest) error {
	if strings.TrimSpace(req.AttemptID) == "" {
		return tuskerError(errorMissingArg, "acp_v1 interrupt requires attempt_id")
	}
	handle := liveRegistry.Find(req.AttemptID)
	if handle == nil {
		return errLiveHandleNotFound
	}
	if handle.Runner() != r.Name() || handle.AttemptID() != req.AttemptID {
		return tuskerError(errorInvalidTransition, "attempt is not owned by a live ACP runner")
	}
	return handle.Interrupt(ctx)
}

func (r *ACPRunner) Collect(ctx context.Context, req CollectRequest) (*CollectResult, error) {
	_ = ctx
	_ = req
	// ACP transport reports only its bounded turn result. Artifact discovery,
	// evidence acceptance, and task completion stay in Tusker's existing paths.
	return &CollectResult{Artifacts: map[string]string{}}, nil
}

type acpAttemptProvenance struct {
	ProjectID      string
	TaskID         string
	RuntimeStore   *RuntimeStore
	PrivateFolders []string
	Runner         RunnerName
	Principal      string
	Actor          string
	AttemptID      string
	Adapter        string
	ProcessID      int
	SessionID      string
	TurnID         string
	ToolCall       string
}

func (p acpAttemptProvenance) payload() map[string]any {
	return map[string]any{
		"principal":    p.Principal,
		"actor":        p.Actor,
		"attempt_id":   p.AttemptID,
		"adapter":      p.Adapter,
		"protocol":     "acp/v1",
		"process_id":   p.ProcessID,
		"session_id":   p.SessionID,
		"turn_id":      p.TurnID,
		"tool_call_id": p.ToolCall,
		"authority":    "observation_only",
	}
}

// acpLiveHandle is one process/session/turn beneath exactly one Tusker
// attempt. It never owns a RunStatus or a task transition: publishing the
// process status gives the pre-existing supervisor an observation to classify.
type acpLiveHandle struct {
	projectID string
	recordID  string
	itemID    string
	attemptID string
	runner    RunnerName

	client       *acp.Client
	log          *acpLogSink
	eventLog     *EventLog
	req          StartRequest
	runtimeStore *RuntimeStore

	mu          sync.RWMutex
	provenance  acpAttemptProvenance
	closeOnce   sync.Once
	stopOnce    sync.Once
	updatesDone chan struct{}
}

func (h *acpLiveHandle) AttemptID() string  { return h.attemptID }
func (h *acpLiveHandle) ProjectID() string  { return h.projectID }
func (h *acpLiveHandle) RecordID() string   { return h.recordID }
func (h *acpLiveHandle) ItemID() string     { return h.itemID }
func (h *acpLiveHandle) Runner() RunnerName { return h.runner }

func (h *acpLiveHandle) currentProvenance() acpAttemptProvenance {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.provenance
}

func (h *acpLiveHandle) updateProvenance(update func(*acpAttemptProvenance)) {
	h.mu.Lock()
	update(&h.provenance)
	h.mu.Unlock()
}

func (h *acpLiveHandle) close() {
	if h == nil {
		return
	}
	h.closeOnce.Do(func() {
		if h.client != nil {
			_ = h.client.Close()
		}
		if h.log != nil {
			_ = h.log.Close()
		}
		if h.runtimeStore != nil {
			_ = h.runtimeStore.Close()
		}
	})
}

func (h *acpLiveHandle) Interrupt(ctx context.Context) error {
	_ = ctx // Interrupt must still clean up when its caller's context is cancelled.
	if h == nil || h.client == nil {
		return errLiveHandleNotFound
	}
	var interruptErr error
	h.stopOnce.Do(func() {
		cancelCtx, cancel := context.WithTimeout(context.Background(), acpCancelDrain(h.req.CodexPolicy))
		defer cancel()
		interruptErr = h.client.Cancel(cancelCtx)
		if errors.Is(interruptErr, acp.ErrNoPrompt) || errors.Is(interruptErr, acp.ErrClosed) {
			interruptErr = nil
		}
		h.close()
	})
	return interruptErr
}

func startLiveACP(ctx context.Context, req StartRequest) (*StartResult, error) {
	return startLiveACPForRunner(ctx, req, RunnerACP)
}

func validateCodexACPAgentIdentity(info acp.AgentInfo, expectedVersion string) error {
	expectedVersion = strings.TrimSpace(expectedVersion)
	if info.Name != codexACPAgentName || info.Version != expectedVersion {
		return tuskerError(errorConfigInvalid, fmt.Sprintf(
			"codex_acp initialize identity mismatch: expected name=%q version=%q, got name=%q version=%q",
			codexACPAgentName, boundedACPObservation(expectedVersion), boundedACPObservation(info.Name), boundedACPObservation(info.Version),
		))
	}
	return nil
}

func startLiveACPForRunner(ctx context.Context, req StartRequest, runner RunnerName) (*StartResult, error) {
	return startLiveACPForRunnerWithSession(ctx, req, runner, "")
}

func startLiveACPForRunnerWithSession(ctx context.Context, req StartRequest, runner RunnerName, sessionRef string) (*StartResult, error) {
	if err := validateACPLaunchRequestForRunner(runner, req); err != nil {
		return nil, err
	}
	if req.ContainmentPGID <= 0 {
		return nil, tuskerError(errorInvalidTransition, "acp_v1 must launch inside a detached runner wrapper containment group")
	}
	if actual := processGroupID(os.Getpid()); actual != req.ContainmentPGID {
		return nil, tuskerError(errorInvalidTransition, fmt.Sprintf("acp_v1 wrapper containment mismatch: wrapper pgid=%d expected=%d", actual, req.ContainmentPGID))
	}
	if fileExists(req.StatusPath) {
		return nil, tuskerError(errorInvalidTransition, "acp_v1 refuses a pre-existing terminal status path")
	}

	workspace, argv, adapter, err := resolveACPRunnerLaunchForRunner(runner, req)
	if err != nil {
		return nil, err
	}
	var codexPlan *CodexACPProviderPlan
	var codexDescriptor CodexACPDescriptor
	var environment []string
	if runner == RunnerCodexACP {
		if req.CodexACP == nil {
			return nil, tuskerError(errorConfigInvalid, "codex_acp launch is missing its provider plan")
		}
		codexPlan = req.CodexACP
		codexDescriptor, environment, err = codexPlan.wrapperEnvironment()
		if err != nil {
			return nil, tuskerError(errorConfigInvalid, "codex_acp launch environment/auth validation failed: "+err.Error())
		}
		// descriptorAndArgv revalidates the same receipt immediately before
		// launch.  Reject any serialized argv drift rather than using a fresh
		// value hidden from the wrapper request.
		_, expectedArgv, verifyErr := codexPlan.descriptorAndArgv()
		if verifyErr != nil || !equalStringSlices(argv, expectedArgv) {
			if verifyErr == nil {
				verifyErr = errors.New("serialized Codex ACP argv drift")
			}
			return nil, tuskerError(errorConfigInvalid, "codex_acp pre-spawn receipt validation failed: "+verifyErr.Error())
		}
	}
	physical := argv[0]
	provenance, err := resolveACPAttemptProvenance(req, adapter)
	if err != nil {
		return nil, err
	}
	provenance.Runner = runner
	provenance.PrivateFolders = append([]string(nil), req.PrivateFolders...)
	prompt, err := readText(req.PromptPath)
	if err != nil {
		return nil, err
	}
	log, err := openACPLogSink(req)
	if err != nil {
		return nil, err
	}
	closeLog := true
	defer func() {
		if closeLog {
			_ = log.Close()
		}
	}()

	eventLog := NewEventLog(req.EventSinkPath)
	policy := codexPolicyForLane(req.CodexPolicy, req.Lane)
	if codexPlan != nil {
		if err := appendACPEvent(eventLog, "acp_codex_bundle_verified", acpAttemptProvenance{Runner: runner, AttemptID: req.AttemptID}, map[string]any{
			"provider": codexACPProvider, "adapter_version": boundedACPObservation(codexPlan.AdapterVersion),
			"receipt": boundedACPObservation(codexPlan.BundleReceipt.VerifiedContentDigest),
		}); err != nil {
			return nil, err
		}
	}
	// Permission callbacks cannot arrive until a prompt is sent below, after
	// handle receives the exact process and namespaced session provenance.
	// Keeping this as a handle lookup avoids closing over a stale pre-session
	// value and makes every tool observation carry the same attempt binding.
	var handle *acpLiveHandle
	if codexPlan != nil {
		// This is deliberately the last filesystem-dependent operation before
		// acp.Start.  The lower-level client receives only the just-revalidated
		// direct native argv, never a mutable bundle path it resolves itself.
		_, launchArgv, launchErr := codexPlan.descriptorAndArgv()
		if launchErr != nil || !equalStringSlices(argv, launchArgv) {
			if launchErr == nil {
				launchErr = errors.New("Codex ACP argv changed before process start")
			}
			return nil, tuskerError(errorConfigInvalid, "codex_acp final pre-spawn bundle validation failed: "+launchErr.Error())
		}
		argv = launchArgv
		physical = argv[0]
	}
	client, err := acp.Start(ctx, acp.Config{
		Argv: argv,
		CWD:  workspace,
		Env: func() []string {
			if codexPlan != nil {
				return environment
			}
			return acpRunnerEnvironment(req, workspace, policy)
		}(),
		Stderr: acpDiagnosticSink{log: log},
		Timeouts: acp.Timeouts{
			// Agent turns may legitimately run for hours. The wrapper remains
			// explicitly cancellable, but elapsed wall time or quiet reasoning
			// must not kill otherwise live work.
			Prompt: -1,
			Stall:  -1,
		},
		PermissionHandler: func(permissionCtx context.Context, request acp.PermissionRequest) (acp.PermissionDecision, error) {
			if handle == nil {
				return acp.Reject, nil
			}
			if codexPlan != nil {
				return evaluateCodexACPTransportPermission(permissionCtx, eventLog, handle.currentProvenance(), request, workspace, codexPlan.Mode, policy)
			}
			return evaluateACPTransportPermission(permissionCtx, eventLog, handle.currentProvenance(), request)
		},
		ValidateProcess: func(pid int) error {
			if pid <= 0 || processGroupID(pid) != req.ContainmentPGID {
				return fmt.Errorf("ACP child escaped wrapper containment: pid=%d pgid=%d expected=%d", pid, processGroupID(pid), req.ContainmentPGID)
			}
			actual, fingerprintErr := acpExecutableFingerprint(physical)
			if fingerprintErr != nil || actual != strings.TrimSpace(req.CommandExecutableFP) {
				return fmt.Errorf("ACP adapter executable fingerprint drift after start")
			}
			return nil
		},
	})
	if err != nil {
		return nil, err
	}
	provenance.ProcessID = client.ProcessID()
	runtimeStore, storeErr := OpenRuntimeStore(DefaultStateRoot())
	if storeErr != nil {
		_ = client.Close()
		_ = log.Close()
		return nil, fmt.Errorf("open runtime store for ACP permissions: %w", storeErr)
	}
	provenance.RuntimeStore = runtimeStore
	handle = &acpLiveHandle{
		projectID:    req.ProjectID,
		recordID:     req.RecordID,
		itemID:       req.ItemID,
		attemptID:    req.AttemptID,
		runner:       runner,
		client:       client,
		log:          log,
		eventLog:     eventLog,
		req:          req,
		provenance:   provenance,
		runtimeStore: runtimeStore,
	}
	log.bindTerminator(handle.close)
	if log.overflowed() {
		handle.close()
		return nil, tuskerError(errorInvalidTransition, "acp_v1 diagnostic log exceeded its configured byte limit during launch")
	}

	init, err := client.Initialize(ctx)
	if err != nil {
		handle.close()
		return nil, err
	}
	if err := appendACPEvent(eventLog, "acp_protocol_negotiated", handle.currentProvenance(), map[string]any{
		"agent_name":     boundedACPObservation(init.AgentInfo.Name),
		"agent_version":  boundedACPObservation(init.AgentInfo.Version),
		"load_session":   init.AgentCapabilities.LoadSession,
		"resume_session": init.AgentCapabilities.ResumeSession,
	}); err != nil {
		handle.close()
		return nil, err
	}
	if codexPlan != nil {
		if err := validateCodexACPAgentIdentity(init.AgentInfo, codexPlan.AdapterVersion); err != nil {
			handle.close()
			return nil, err
		}
	}
	if codexPlan != nil {
		if err := appendACPEvent(eventLog, "acp_codex_auth_selected", handle.currentProvenance(), map[string]any{
			"auth_source": boundedACPObservation(codexPlan.AuthSource), "principal_sha256": boundedACPObservation(codexPlan.AuthPrincipalSHA256),
			"authenticate_called": false,
		}); err != nil {
			handle.close()
			return nil, err
		}
	}
	var session acp.Session
	if sessionRef != "" {
		if runner != RunnerDevin || !init.AgentCapabilities.LoadSession {
			handle.close()
			return nil, tuskerError(errorInvalidTransition, "ACP adapter did not negotiate Devin session/load")
		}
		rawSession, decodeErr := acpRawSessionRef("devin", sessionRef)
		if decodeErr != nil {
			handle.close()
			return nil, decodeErr
		}
		session, err = client.LoadSession(ctx, rawSession)
	} else {
		session, err = client.NewSession(ctx)
	}
	if err != nil {
		handle.close()
		return nil, err
	}
	if err := validateACPObservationID(session.ID, "session"); err != nil {
		handle.close()
		return nil, err
	}
	if runner == RunnerDevin {
		if err := configureDevinSession(ctx, client, session, policy, req.RunnerModel); err != nil {
			handle.close()
			return nil, err
		}
	}
	if codexPlan != nil {
		plan, configErr := applyCodexACPConfig(ctx, client, codexDescriptor, session)
		if configErr != nil {
			handle.close()
			return nil, tuskerError(errorConfigInvalid, "codex_acp configuration was not applied exactly: "+configErr.Error())
		}
		if err := appendACPEvent(eventLog, "acp_codex_config_applied", handle.currentProvenance(), map[string]any{
			"steps": len(plan.Steps), "config_receipt": boundedACPObservation(codexACPConfigReceipt(plan)),
		}); err != nil {
			handle.close()
			return nil, err
		}
		binding := CodexACPAuthorityBinding{
			ProjectID: req.ProjectID, WorkspacePath: workspace, RunnerProfile: req.RunnerProfile,
			AuthPrincipalDigest: codexPlan.AuthPrincipalSHA256, OriginAttemptID: req.AttemptID, WorkRevision: req.WorkRevision,
		}
		stored, sessionErr := codexDescriptor.EncodeSessionRef(session.ID, binding)
		if sessionErr != nil {
			handle.close()
			return nil, tuskerError(errorInvalidTransition, "codex_acp session binding failed: "+sessionErr.Error())
		}
		handle.updateProvenance(func(p *acpAttemptProvenance) { p.SessionID = stored })
	} else {
		handle.updateProvenance(func(p *acpAttemptProvenance) { p.SessionID = acpStoredSessionRef(adapter, session.ID) })
	}
	if err := appendACPEvent(eventLog, "acp_session_bound", handle.currentProvenance(), map[string]any{
		"session_observation": "bound_to_current_attempt",
	}); err != nil {
		handle.close()
		return nil, err
	}
	if _, err := fmt.Fprintf(log, "acp/v1 transport started adapter=%s attempt=%s process=%d\n", adapter, req.AttemptID, provenance.ProcessID); err != nil {
		handle.close()
		return nil, err
	}

	liveRegistry.Register(handle)
	handle.updatesDone = make(chan struct{})
	go handle.observeUpdates()
	go handle.runPrompt(prompt)
	closeLog = false
	processStartedAt := recordedProcessStartTime(provenance.ProcessID, time.Now().UTC().Format(time.RFC3339))
	return &StartResult{
		SessionRef:   handle.currentProvenance().SessionID,
		StartedAt:    processStartedAt,
		PID:          provenance.ProcessID,
		PGID:         req.ContainmentPGID,
		ProcessStart: processStartedAt,
		StatusPath:   req.StatusPath,
		Capabilities: (&ACPRunner{runner: runner}).Capabilities(),
		Outcome:      AttemptOutcomeNone,
	}, nil
}

func (h *acpLiveHandle) observeUpdates() {
	defer close(h.updatesDone)
	// Save bounded message snapshots, so streamed fragments become readable
	// messages and credentials split over chunks are redacted together.
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	var message string
	var messageID uint64
	dirty := false
	type pendingEvent struct {
		kind                           string
		fields                         map[string]any
		title, command, status, output string
	}
	pending := map[string]pendingEvent{}
	var pendingOrder []string
	flush := func() {
		if dirty {
			_ = appendACPEvent(h.eventLog, "agent_message", h.currentProvenance(), map[string]any{
				"text": runActivityText(message), "activity": true,
				"message_id": fmt.Sprintf("acp:%d", messageID),
			})
			dirty = false
		}
		for _, id := range pendingOrder {
			event := pending[id]
			_ = appendACPEvent(h.eventLog, event.kind, h.currentProvenance(), event.fields)
			delete(pending, id)
		}
		pendingOrder = pendingOrder[:0]
	}
	defer flush()
	for {
		select {
		case <-ticker.C:
			flush()
		case update, ok := <-h.client.Updates():
			if !ok {
				return
			}
			activity := acpActivity(update.Params)
			if activity.SessionUpdate == "agent_message_chunk" {
				if len(pending) > 0 {
					flush()
				}
				if message == "" {
					messageID = update.Sequence
				}
				// ponytail: retain the first 16K runes of each message; full
				// transcript paging belongs in a future transcript viewer.
				message = truncateRunes(message+activityContentText(activity.Content), runActivityTextLimit+1)
				dirty = true
				continue
			}
			if activity.SessionUpdate == "agent_thought_chunk" {
				continue
			}
			if dirty {
				flush()
				message = ""
			}
			fields := map[string]any{"method": update.Method}
			kind := "acp_session_update_observed"
			key := activity.SessionUpdate
			var toolTitle, toolCommand, toolStatus, toolOutput string
			if activity.SessionUpdate == "tool_call" || activity.SessionUpdate == "tool_call_update" {
				kind = "tool_call"
				fields["activity"] = true
				var input struct {
					Command string `json:"command"`
				}
				_ = json.Unmarshal(activity.RawInput, &input)
				toolTitle, toolCommand, toolStatus, toolOutput = activity.Title, input.Command, activity.Status, activityContentText(activity.Content)
				toolID := boundedACPObservation(activity.ToolCallID)
				fields["tool_call_id"] = toolID
				// The event reader uses message_id as the stable display identity.
				// Keep start and update records together even when the provider only
				// sends the ID on the update; a sequence fallback still avoids
				// collapsing unrelated malformed observations.
				if toolID == "" {
					toolID = fmt.Sprintf("sequence-%d", update.Sequence)
				}
				fields["message_id"] = "acp:tool:" + toolID
				key = "tool:" + toolID
				if activity.Status == "failed" {
					fields["level"] = "error"
				}
			} else if activity.SessionUpdate == "plan" {
				kind = "plan"
				fields["activity"] = true
				fields["message_id"] = "acp:plan"
				var entries []struct {
					Content string `json:"content"`
					Status  string `json:"status"`
				}
				_ = json.Unmarshal(activity.Plan, &entries)
				var checklist []string
				for _, entry := range entries {
					mark := "[ ]"
					if entry.Status == "completed" {
						mark = "[x]"
					}
					checklist = append(checklist, mark+" "+entry.Content)
				}
				fields["text"] = runActivityText(strings.Join(checklist, "\n"))
			} else if activity.SessionUpdate == "usage_update" {
				kind = "usage"
			}
			if kind == "tool_call" {
				if prior, ok := pending[key]; ok {
					toolTitle = firstNonEmpty(prior.title, toolTitle)
					toolCommand = firstNonEmpty(prior.command, toolCommand)
					toolOutput = strings.TrimSpace(prior.output + "\n" + toolOutput)
				}
				parts := []string{firstNonEmpty(toolTitle, activity.ToolCallID, "Tool")}
				for _, part := range []string{toolCommand, toolStatus, toolOutput} {
					if part != "" {
						parts = append(parts, part)
					}
				}
				fields["text"] = runActivityText(strings.Join(parts, "\n"))
			}
			if _, exists := pending[key]; !exists {
				pendingOrder = append(pendingOrder, key)
			}
			pending[key] = pendingEvent{kind: kind, fields: fields, title: toolTitle, command: toolCommand, status: toolStatus, output: truncateRunes(toolOutput, runActivityTextLimit)}
		}
	}
}

func (h *acpLiveHandle) runPrompt(prompt string) {
	result, err := h.client.Prompt(context.Background(), prompt)
	outcome, exitCode, reason := acpTerminalStatus(result, err)
	// Drain the final message before publishing terminal status. The detached
	// wrapper can exit as soon as that status appears.
	_ = h.client.Close()
	<-h.updatesDone
	h.updateProvenance(func(p *acpAttemptProvenance) {
		if strings.TrimSpace(result.TurnID) != "" {
			p.TurnID = boundedACPObservation(result.TurnID)
		}
	})
	provenance := h.currentProvenance()
	_ = appendACPEvent(h.eventLog, "acp_turn_terminal", provenance, map[string]any{
		"transport_outcome": string(result.Outcome),
		"delivery_phase":    string(result.Delivery),
		"stop_reason":       boundedACPObservation(result.StopReason),
	})
	if _, logErr := fmt.Fprintf(h.log, "acp/v1 turn terminal outcome=%s delivery=%s\n", result.Outcome, result.Delivery); logErr != nil && exitCode == 0 {
		outcome, exitCode, reason = AttemptOutcomeFailed, 1, "acp_v1 diagnostic log failed: "+logErr.Error()
	}
	// This writes an attempt-local process observation only. The wrapper and
	// daemon retain ownership of task, evidence, review, gate, and wave state.
	_, _ = writeRunnerStatusFileIfAbsentWithOutcome(h.req.StatusPath, exitCode, outcome, reason, 0)
	h.close()
	liveRegistry.Unregister(h.attemptID)
}

func acpTerminalStatus(result acp.PromptResult, err error) (AttemptOutcome, int, string) {
	reason := ""
	if err != nil {
		reason = "acp_v1 transport error: " + boundedACPObservation(err.Error())
	}
	switch result.Outcome {
	case acp.OutcomeCompleted:
		return AttemptOutcomeNone, 0, ""
	case acp.OutcomeBudgetExceeded:
		return AttemptOutcomeBudgetExceeded, exitCodeForOutcome(AttemptOutcomeBudgetExceeded), firstNonEmpty(reason, "acp_v1 reported max_tokens")
	case acp.OutcomeTurnCapExhausted:
		return AttemptOutcomeTurnCapExhausted, 0, firstNonEmpty(reason, "acp_v1 reported max_turn_requests")
	case acp.OutcomeCancelled:
		return AttemptOutcomeCancelled, exitCodeForOutcome(AttemptOutcomeCancelled), firstNonEmpty(reason, "acp_v1 prompt cancelled")
	case acp.OutcomeRefused:
		return AttemptOutcomeBlocked, 1, firstNonEmpty(reason, "acp_v1 refused the prompt or a required permission")
	case acp.OutcomeDeliveryUnknown:
		return AttemptOutcomeUnknown, 1, firstNonEmpty(reason, "acp_v1 delivery_unknown; inspect retained work before recovery")
	case acp.OutcomeTimedOut:
		return AttemptOutcomeFailed, 1, firstNonEmpty(reason, "acp_v1 prompt timed out")
	case acp.OutcomePoisoned, acp.OutcomeProtocolFailed:
		return AttemptOutcomeFailed, 1, firstNonEmpty(reason, "acp_v1 transport failed")
	default:
		return AttemptOutcomeFailed, 1, firstNonEmpty(reason, "acp_v1 terminated without a trustworthy result")
	}
}

func validateACPLaunchRequest(req StartRequest) error {
	return validateACPLaunchRequestForRunner(RunnerACP, req)
}

func validateACPLaunchRequestForRunner(runner RunnerName, req StartRequest) error {
	if strings.TrimSpace(req.AttemptID) == "" || strings.TrimSpace(req.ProjectID) == "" || strings.TrimSpace(req.RecordID) == "" {
		return tuskerError(errorInvalidArg, "acp_v1 requires project, record, and attempt identities")
	}
	if len(req.CommandArgv) == 0 || !filepath.IsAbs(strings.TrimSpace(req.CommandArgv[0])) {
		return tuskerError(errorConfigInvalid, "acp_v1 requires a pre-resolved absolute adapter argv executable")
	}
	if strings.TrimSpace(req.CommandExecutableFP) == "" || !v7CloseAuthorityDigest(strings.TrimSpace(req.CommandExecutableFP), "sha256:") {
		return tuskerError(errorConfigInvalid, "acp_v1 requires a valid preinstalled adapter executable fingerprint")
	}
	if req.RawLogMaxBytes <= 0 {
		return tuskerError(errorConfigInvalid, "acp_v1 requires a positive bounded raw-log byte limit")
	}
	for _, arg := range req.CommandArgv {
		if strings.Contains(arg, "{{") || strings.Contains(arg, "}}") {
			return tuskerError(errorConfigInvalid, "acp_v1 argv must be resolved before launch; template expansion is not allowed")
		}
	}
	if _, err := runnerWorkspaceCWD(runner, req.WorkspacePath); err != nil {
		return err
	}
	return nil
}

func resolveACPRunnerLaunch(req StartRequest) (string, []string, string, error) {
	return resolveACPRunnerLaunchForRunner(RunnerACP, req)
}

func resolveACPRunnerLaunchForRunner(runner RunnerName, req StartRequest) (string, []string, string, error) {
	workspace, err := runnerWorkspaceCWD(runner, req.WorkspacePath)
	if err != nil {
		return "", nil, "", err
	}
	physical, err := filepath.EvalSymlinks(req.CommandArgv[0])
	if err != nil || !filepath.IsAbs(physical) {
		return "", nil, "", tuskerError(errorConfigInvalid, "acp_v1 adapter executable could not be resolved")
	}
	info, err := os.Stat(physical)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&0o111 == 0 {
		return "", nil, "", tuskerError(errorConfigInvalid, "acp_v1 adapter executable is not a regular executable file")
	}
	if pathWithin(workspace, physical) || (strings.TrimSpace(req.RepoRoot) != "" && pathWithin(req.RepoRoot, physical)) {
		return "", nil, "", tuskerError(errorConfigInvalid, "acp_v1 refuses an adapter executable inside the workspace or repository")
	}
	if shebang, err := acpExecutableHasShebang(physical); err != nil || shebang {
		return "", nil, "", tuskerError(errorConfigInvalid, "acp_v1 generic runtime refuses a shebang adapter executable")
	}
	actual, err := acpExecutableFingerprint(physical)
	if err != nil || actual != strings.TrimSpace(req.CommandExecutableFP) {
		return "", nil, "", tuskerError(errorConfigInvalid, "acp_v1 adapter executable fingerprint drift")
	}
	argv := append([]string{physical}, req.CommandArgv[1:]...)
	return workspace, argv, acpAdapterID(physical), nil
}

func acpExecutableHasShebang(path string) (bool, error) {
	file, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer file.Close()
	var prefix [2]byte
	n, err := io.ReadFull(file, prefix[:])
	if err != nil && !(errors.Is(err, io.EOF) && n == 0) && !(errors.Is(err, io.ErrUnexpectedEOF) && n < 2) {
		return false, err
	}
	return n == len(prefix) && prefix[0] == '#' && prefix[1] == '!', nil
}

func acpExecutableFingerprint(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	before, err := file.Stat()
	if err != nil {
		return "", err
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	after, err := file.Stat()
	if err != nil || !os.SameFile(before, after) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		return "", fmt.Errorf("acp adapter changed while fingerprinting")
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil)), nil
}

func acpAdapterID(executable string) string {
	base := strings.ToLower(filepath.Base(executable))
	base = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			return r
		}
		return '-'
	}, base)
	if base == "" {
		return "adapter"
	}
	return truncateRunes(base, 64)
}

func resolveACPAttemptProvenance(req StartRequest, adapter string) (acpAttemptProvenance, error) {
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		return acpAttemptProvenance{}, fmt.Errorf("open runtime store for ACP attempt binding: %w", err)
	}
	defer store.Close()
	run, err := findRunScopedOrAmbiguous(store, req.ProjectID, req.RecordID)
	if err != nil {
		return acpAttemptProvenance{}, err
	}
	if run == nil || !runnerWrapperOwnsRun(*run, req) {
		return acpAttemptProvenance{}, tuskerError(errorInvalidTransition, "acp_v1 attempt no longer owns its Tusker lease")
	}
	actor := strings.TrimSpace(req.Actor)
	if authorization, authErr := store.LatestRunAuthorization(req.ProjectID, req.RecordID); authErr == nil && authorization != nil && authorization.LeaseGeneration == req.LeaseGeneration && actor == "" {
		actor = strings.TrimSpace(authorization.Actor)
	}
	if actor == "" {
		actor = "unknown"
	}
	principal := strings.TrimSpace(req.Principal)
	if principal == "" {
		// Tusker's current durable authorization model has an actor but no
		// separate principal column. Preserve that fact rather than letting an
		// adapter identity fill the gap.
		principal = "actor-derived:" + actor
	}
	return acpAttemptProvenance{ProjectID: req.ProjectID, TaskID: req.ItemID, Runner: RunnerACP, Principal: principal, Actor: actor, AttemptID: req.AttemptID, Adapter: adapter}, nil
}

func acpRunnerEnvironment(req StartRequest, workspace string, policy CodexPolicy) []string {
	_ = workspace
	_ = policy
	// ACP adapters receive a deliberately positive, tiny environment. The
	// generic transport has no provider-specific control-plane contract, so it
	// must never inherit TUSKER_* values (including test/service overrides),
	// runner profile metadata, or a caller-selected PATH prefix. Credential
	// discovery remains possible through HOME/keychain and the explicitly named
	// provider variables; everything else is absent by construction.
	allowed := map[string]struct{}{
		"HOME": {}, "TMPDIR": {}, "LANG": {}, "TERM": {}, "LC_ALL": {}, "LC_CTYPE": {}, "LC_MESSAGES": {}, "LC_COLLATE": {}, "LC_MONETARY": {}, "LC_NUMERIC": {}, "LC_TIME": {},
		"XDG_CONFIG_HOME": {}, "XDG_CACHE_HOME": {}, "XDG_DATA_HOME": {}, "XDG_RUNTIME_DIR": {},
		"SSL_CERT_FILE": {}, "SSL_CERT_DIR": {},
		"OPENAI_API_KEY": {}, "ANTHROPIC_API_KEY": {}, "GOOGLE_API_KEY": {}, "GEMINI_API_KEY": {},
		"CODEX_HOME": {}, "CLAUDE_CONFIG_DIR": {},
	}
	out := make([]string, 0, len(allowed)+1)
	for _, entry := range os.Environ() {
		key := entry
		if i := strings.IndexByte(entry, '='); i >= 0 {
			key = entry[:i]
		}
		if strings.HasPrefix(key, "TUSKER_") {
			continue
		}
		if _, ok := allowed[key]; ok {
			out = append(out, entry)
			continue
		}
	}
	// The executable and cwd are already absolute. A fixed system PATH is
	// retained only for adapter-spawned tools; it is independent of both
	// RunnerPathPrefix and CommandSearchPath.
	out = append(out, "PATH="+strings.Join([]string{"/usr/local/bin", "/opt/homebrew/bin", "/usr/bin", "/bin", "/usr/sbin", "/sbin"}, string(os.PathListSeparator)))
	return out
}

func acpDurationMS(value int) time.Duration {
	if value <= 0 {
		return 0
	}
	return time.Duration(value) * time.Millisecond
}

func configureDevinSession(ctx context.Context, client *acp.Client, session acp.Session, policy CodexPolicy, model string) error {
	if policy.ThreadSandbox != "workspace-write" || policy.TurnSandboxNetwork == nil || !*policy.TurnSandboxNetwork {
		return tuskerError(errorConfigInvalid, "Devin ACP requires sandboxed workspace-write with network enabled")
	}
	current, err := setDevinConfigOption(ctx, client, session, "mode", "smart")
	if err != nil {
		return tuskerError(errorConfigInvalid, "Devin ACP mode configuration failed: "+err.Error())
	}
	if _, err := setDevinConfigOption(ctx, client, current, "model", strings.TrimSpace(model)); err != nil {
		return tuskerError(errorConfigInvalid, "Devin ACP model configuration failed: "+err.Error())
	}
	return nil
}

func setDevinConfigOption(ctx context.Context, client *acp.Client, session acp.Session, id, value string) (acp.Session, error) {
	if value == "" {
		return session, fmt.Errorf("%s is blank", id)
	}
	for _, option := range session.ConfigOptions {
		if option.ID != id {
			continue
		}
		if option.CurrentValue == value {
			return session, nil
		}
		return client.SetConfigOption(ctx, id, value)
	}
	return session, fmt.Errorf("option %q was not advertised", id)
}

func acpCancelDrain(policy CodexPolicy) time.Duration {
	// The protocol's five-second drain is the hard ceiling. A shorter resolved
	// read timeout may tighten it, never expand it.
	if value := acpDurationMS(policy.ReadTimeoutMS); value > 0 && value < 5*time.Second {
		return value
	}
	return 5 * time.Second
}

func evaluateCodexACPTransportPermission(ctx context.Context, eventLog *EventLog, provenance acpAttemptProvenance, request acp.PermissionRequest, workspace string, mode CodexACPMode, policy CodexPolicy) (acp.PermissionDecision, error) {
	if approval, handled, approvalErr := awaitCodexACPAgentAccessApproval(ctx, provenance, request, workspace, policy); handled {
		p := provenance
		p.ToolCall = boundedACPObservation(request.ToolCallID)
		_ = appendACPEvent(eventLog, "acp_permission_decided", p, map[string]any{
			"operation_class": "execute",
			"policy_rule":     "destructive_approval",
			"outcome":         string(approval),
			"reason_code": firstNonEmpty(func() string {
				if approvalErr != nil {
					return approvalErr.Error()
				}
				return "operator_decision"
			}(), "operator_decision"),
		})
		return approval, approvalErr
	}
	normalized := DecodeCodexACPPermission(request)
	allowed := map[string]bool{"read": true}
	permissionPolicy := ACPPermissionPolicy{AllowedToolKinds: allowed, BudgetAuthorized: true}
	switch mode {
	case CodexACPModeReadOnly:
		permissionPolicy.ReadOnly = true
	case CodexACPModeWorkspaceWrite:
		allowed["write"], allowed["execute"] = true, true
		permissionPolicy.AllowWorkspaceWrite = true
		permissionPolicy.AllowExecute = true
		if policy.TurnSandboxNetwork != nil && *policy.TurnSandboxNetwork {
			allowed["network"] = true
			permissionPolicy.AllowNetwork = true
		}
	default:
		permissionPolicy.BudgetAuthorized = false
	}
	decision := EvaluateACPPermission(normalized.BrokerRequest(provenance.AttemptID, request.SessionID, workspace, ctx.Err() != nil), permissionPolicy)
	p := provenance
	p.ToolCall = boundedACPObservation(request.ToolCallID)
	_ = appendACPEvent(eventLog, "acp_permission_decided", p, map[string]any{
		"operation_class": decision.Audit.OperationClass,
		"policy_rule":     decision.Audit.PolicyRule,
		"outcome":         decision.Audit.Outcome,
		"reason_code":     decision.Audit.ReasonCode,
	})
	if ctx.Err() != nil || decision.Outcome == ACPPermissionCancelled {
		return acp.Cancelled, nil
	}
	if decision.Outcome == ACPPermissionAllowOnce {
		return acp.AllowOnce, nil
	}
	return acp.Reject, nil
}

func evaluateACPTransportPermission(ctx context.Context, eventLog *EventLog, provenance acpAttemptProvenance, request acp.PermissionRequest) (acp.PermissionDecision, error) {
	options := make([]ACPPermissionOption, 0, len(request.Options))
	for _, option := range request.Options {
		options = append(options, ACPPermissionOption{OptionID: option.ID, Kind: option.Kind})
	}
	decision := EvaluateACPPermission(ACPPermissionRequest{
		AttemptID: provenance.AttemptID, BoundAttemptID: provenance.AttemptID,
		// internal/acp has already bound this request to its live session before
		// invoking us. The provider-neutral runtime deliberately receives no
		// adapter-specific session decoder, so it preserves that exact bound
		// observation instead of attempting to reverse a stored session token.
		SessionID: request.SessionID, BoundSessionID: request.SessionID,
		// No provider-neutral decoder is allowed to infer target/tool semantics
		// from arbitrary ACP JSON. Empty fields fail closed in the broker.
		ToolKind: "", Target: "", Workspace: "", Options: options, Cancelled: ctx.Err() != nil,
	}, ACPPermissionPolicy{})
	p := provenance
	p.ToolCall = boundedACPObservation(request.ToolCallID)
	_ = appendACPEvent(eventLog, "acp_permission_decided", p, map[string]any{
		"operation_class": decision.Audit.OperationClass,
		"policy_rule":     decision.Audit.PolicyRule,
		"outcome":         decision.Audit.Outcome,
		"reason_code":     decision.Audit.ReasonCode,
	})
	if ctx.Err() != nil || decision.Outcome == ACPPermissionCancelled {
		return acp.Cancelled, nil
	}
	if decision.Outcome == ACPPermissionAllowOnce {
		return acp.AllowOnce, nil
	}
	return acp.Reject, nil
}

func acpStoredSessionRef(adapter, raw string) string {
	return "acp:v1:" + adapter + ":" + base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func acpRawSessionRef(adapter, stored string) (string, error) {
	prefix := "acp:v1:" + adapter + ":"
	if !strings.HasPrefix(stored, prefix) {
		return "", tuskerError(errorInvalidTransition, "ACP session adapter does not match the selected runner")
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(stored, prefix))
	if err != nil || validateACPObservationID(string(raw), "session") != nil {
		return "", tuskerError(errorInvalidTransition, "ACP stored session reference is invalid")
	}
	return string(raw), nil
}

// Session binding is already a durable attempt-local observation. Recover it
// from that record when the detached wrapper cannot return its child result.
func acpSessionRefFromAttemptEvents(path, attemptID, runner string) string {
	if !isACPRunner(RunnerName(runner)) || strings.TrimSpace(attemptID) == "" {
		return ""
	}
	for _, event := range readReviewPacketEvents(path) {
		if stringField(event, "kind") != "acp_session_bound" || stringField(event, "attempt_id") != attemptID || stringField(event, "runner") != runner {
			continue
		}
		payload, ok := event["payload"].(map[string]any)
		if !ok || stringField(payload, "attempt_id") != attemptID {
			continue
		}
		ref := stringField(payload, "session_id")
		if stringField(payload, "adapter") != runner {
			continue
		}
		if _, err := acpRawSessionRef(runner, ref); err == nil {
			return ref
		}
	}
	return ""
}

func validateACPObservationID(value, kind string) error {
	if value = strings.TrimSpace(value); value == "" || len(value) > 512 || strings.ContainsAny(value, "\r\n\x00") {
		return tuskerError(errorInvalidTransition, "acp_v1 returned an invalid "+kind+" observation identifier")
	}
	return nil
}

func boundedACPObservation(value string) string {
	value = strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(value, "\r", " "), "\n", " "))
	return truncateRunes(value, 256)
}

func appendACPEvent(eventLog *EventLog, kind string, provenance acpAttemptProvenance, fields map[string]any) error {
	payload := provenance.payload()
	for key, value := range fields {
		payload[key] = value
	}
	if eventLog == nil {
		return nil
	}
	runner := provenance.Runner
	if runner == "" {
		runner = RunnerACP
	}
	return eventLog.Append(kind, provenance.AttemptID, runner, payload)
}

type acpLogSink struct{ writer *boundedRawLogWriter }

func openACPLogSink(req StartRequest) (*acpLogSink, error) {
	if err := ensureDir(filepath.Dir(req.RawLogPath)); err != nil {
		return nil, err
	}
	writer, err := openBoundedRawLog(req.RawLogPath, req.RawLogMaxBytes, false)
	if err != nil {
		return nil, fmt.Errorf("open bounded ACP diagnostic log: %w", err)
	}
	return &acpLogSink{writer: writer}, nil
}

func (s *acpLogSink) Write(p []byte) (int, error) {
	if s == nil || s.writer == nil {
		return 0, os.ErrInvalid
	}
	return s.writer.Write(p)
}

func (s *acpLogSink) Close() error {
	if s == nil || s.writer == nil {
		return nil
	}
	return s.writer.close()
}

func (s *acpLogSink) bindTerminator(terminate func()) {
	if s != nil && s.writer != nil {
		s.writer.bindTerminator(terminate)
	}
}

func (s *acpLogSink) overflowed() bool {
	return s != nil && s.writer != nil && s.writer.overflowed()
}

// acpDiagnosticSink records only bounded diagnostic metadata, never raw
// adapter stderr. Provider diagnostics can contain prompts and credentials;
// their bytes are therefore redacted before they reach Tusker's raw-log path.
type acpDiagnosticSink struct{ log *acpLogSink }

func (s acpDiagnosticSink) Write(p []byte) (int, error) {
	if s.log == nil {
		return 0, os.ErrInvalid
	}
	sum := sha256.Sum256(p)
	if _, err := fmt.Fprintf(s.log, "acp adapter stderr bytes=%d sha256=%x\n", len(p), sum[:]); err != nil {
		return 0, err
	}
	return len(p), nil
}
