package main

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
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
	// Session/load and session/resume must remain false until a provider adapter
	// both negotiates and implements the exact protocol operation. A generic
	// local process cannot safely treat a persisted ACP session as resumable.
	return RunnerCapabilities{
		StructuredEvents:   true,
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

// Resume is deliberately unavailable in the common transport lane. A later
// provider adapter may enable it only after the negotiated session/load or
// session/resume method is implemented and bound to the current Tusker lease.
func (r *ACPRunner) Resume(ctx context.Context, req ResumeRequest) (*ResumeResult, error) {
	_ = ctx
	if strings.TrimSpace(req.SessionRef) == "" {
		return nil, tuskerError(errorMissingArg, "acp_v1 resume requires session_ref")
	}
	return nil, tuskerError(errorInvalidTransition, "acp_v1 session resume is unavailable until a provider adapter implements a negotiated resume method")
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
	Runner    RunnerName
	Principal string
	Actor     string
	AttemptID string
	Adapter   string
	ProcessID int
	SessionID string
	TurnID    string
	ToolCall  string
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

	client   *acp.Client
	log      *acpLogSink
	eventLog *EventLog
	req      StartRequest

	mu         sync.RWMutex
	provenance acpAttemptProvenance
	closeOnce  sync.Once
	stopOnce   sync.Once
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

func startLiveACPForRunner(ctx context.Context, req StartRequest, runner RunnerName) (*StartResult, error) {
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
			Prompt: acpDurationMS(policy.TurnTimeoutMS),
			Stall:  acpDurationMS(policy.StallTimeoutMS),
		},
		PermissionHandler: func(permissionCtx context.Context, request acp.PermissionRequest) (acp.PermissionDecision, error) {
			if handle == nil {
				return acp.Reject, nil
			}
			// Codex is currently read-only only.  Its public edit callback has no
			// trustworthy target and execute has no granted authority, so retain
			// the generic fail-closed broker until permission parity is reviewed.
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
	handle = &acpLiveHandle{
		projectID:  req.ProjectID,
		recordID:   req.RecordID,
		itemID:     req.ItemID,
		attemptID:  req.AttemptID,
		runner:     runner,
		client:     client,
		log:        log,
		eventLog:   eventLog,
		req:        req,
		provenance: provenance,
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
		if err := appendACPEvent(eventLog, "acp_codex_auth_selected", handle.currentProvenance(), map[string]any{
			"auth_source": boundedACPObservation(codexPlan.AuthSource), "principal_sha256": boundedACPObservation(codexPlan.AuthPrincipalSHA256),
			"authenticate_called": false,
		}); err != nil {
			handle.close()
			return nil, err
		}
	}
	session, err := client.NewSession(ctx)
	if err != nil {
		handle.close()
		return nil, err
	}
	if err := validateACPObservationID(session.ID, "session"); err != nil {
		handle.close()
		return nil, err
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
	for update := range h.client.Updates() {
		// session/update payloads are untrusted, potentially large provider
		// observations. Do not store them or derive authority from them.
		_ = appendACPEvent(h.eventLog, "acp_session_update_observed", h.currentProvenance(), map[string]any{
			"method": update.Method,
		})
	}
}

func (h *acpLiveHandle) runPrompt(prompt string) {
	result, err := h.client.Prompt(context.Background(), prompt)
	outcome, exitCode, reason := acpTerminalStatus(result, err)
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
		return AttemptOutcomeFailed, 1, firstNonEmpty(reason, "acp_v1 delivery_unknown; no automatic retry or resume")
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
	return acpAttemptProvenance{Runner: RunnerACP, Principal: principal, Actor: actor, AttemptID: req.AttemptID, Adapter: adapter}, nil
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

func acpCancelDrain(policy CodexPolicy) time.Duration {
	// The protocol's five-second drain is the hard ceiling. A shorter resolved
	// read timeout may tighten it, never expand it.
	if value := acpDurationMS(policy.ReadTimeoutMS); value > 0 && value < 5*time.Second {
		return value
	}
	return 5 * time.Second
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
