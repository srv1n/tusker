package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"syscall"
	"time"
)

const (
	v7LandingLockSchema         = "tusker.landing-lock/v1"
	v7LandingLockRecoveryGrace  = 30 * time.Second
	v7LandingAuditProvenance    = "tusker:landing/v2"
	v7LandingReceiptSchema      = "tusker.landing-receipt/v3"
	v7LandingReceiptIndexSchema = "tusker.landing-receipt-index/v2"
	v7LandingGateCacheSchemaV1  = "tusker.landing-gate-cache/v1"
	v7LandingGateCacheSchemaV2  = "tusker.landing-gate-cache/v2"
	v7LandingAuthorityDeparture = "scheduled_departure"
	v7LandingAuthorityWaveDrain = "wave_drain"
)

type v7LandingLockOwner struct {
	Schema               string `json:"schema"`
	Token                string `json:"token"`
	PID                  int    `json:"pid"`
	Host                 string `json:"host"`
	HostVerified         bool   `json:"host_verified"`
	ProcessStartedAt     string `json:"process_started_at"`
	ProcessStartVerified bool   `json:"process_start_verified"`
	AcquiredAt           string `json:"acquired_at"`
}

type v7LandTask struct {
	ID               string
	Branch           string
	SourceSHA        string
	SourceProvenance string
}

type v7LandingAuditEntry struct {
	Task               string
	Branch             string
	SourceSHA          string
	SourceProvenance   string
	Target             string
	BaseSHA            string
	MergeCommit        string
	GateResult         string
	GateSummary        string
	GateFingerprint    string
	ReceiptFingerprint string
	ControlAuthority   string
	Commit             string
	Tree               string
	Actor              string
	Timestamp          string
	// DefectID is retained for completion-worker failure audit compatibility.
	// It is deliberately additive to receipt-based dedupe, never a substitute.
	DefectID string
}

type v7LandingReceiptTask struct {
	Task             string `json:"task"`
	Branch           string `json:"branch"`
	SourceSHA        string `json:"source_sha"`
	SourceProvenance string `json:"source_provenance"`
	BaseSHA          string `json:"base_sha"`
	MergeCommit      string `json:"merge_commit"`
}

type v7LandingReceipt struct {
	Schema             string                    `json:"schema"`
	Fingerprint        string                    `json:"fingerprint"`
	GateFingerprint    string                    `json:"gate_fingerprint"`
	LaneIdentity       string                    `json:"lane_identity"`
	Target             string                    `json:"target"`
	Actor              string                    `json:"actor"`
	ControlAuthority   string                    `json:"control_authority"`
	BatchBaseSHA       string                    `json:"batch_base_sha"`
	BatchHeadSHA       string                    `json:"batch_head_sha"`
	BatchTreeSHA       string                    `json:"batch_tree_sha"`
	BatchSegment       []string                  `json:"batch_segment"`
	Tasks              []v7LandingReceiptTask    `json:"tasks"`
	Commands           []string                  `json:"commands"`
	Toolchains         map[string]string         `json:"toolchains"`
	Outcome            string                    `json:"outcome"`
	GateSummary        string                    `json:"gate_summary"`
	ReceiptIssuedAt    string                    `json:"receipt_issued_at"`
	GateStartedAt      string                    `json:"gate_started_at"`
	GateFinishedAt     string                    `json:"gate_finished_at"`
	CommandOutcomes    []v7LandingCommandOutcome `json:"command_outcomes"`
	ProjectID          string                    `json:"project_id"`
	RepoIdentity       string                    `json:"repo_identity"`
	DepartureID        string                    `json:"departure_id"`
	PolicyID           string                    `json:"policy_id"`
	ScheduledWindow    string                    `json:"scheduled_window"`
	DaemonSessionID    string                    `json:"daemon_session_id"`
	DaemonHost         string                    `json:"daemon_host"`
	DaemonProcess      string                    `json:"daemon_process"`
	AuthorityID        string                    `json:"authority_id"`
	AuthorityGen       int                       `json:"authority_generation"`
	AuthoritySignature []byte                    `json:"authority_signature"`
}

type v7LandingCommandOutcome struct {
	Command    string `json:"command"`
	Result     string `json:"result"`
	StartedAt  string `json:"started_at"`
	FinishedAt string `json:"finished_at"`
}

type v7LandingGateEvidence struct {
	Pass        bool
	Summary     string
	Fingerprint string
	Commands    []string
	Toolchains  map[string]string
	StartedAt   string
	FinishedAt  string
	Outcomes    []v7LandingCommandOutcome
}

type v7LandingBatchResult struct {
	Pass    bool
	Summary string
	Receipt v7LandingReceipt
}

// v7LandedEntry pairs a landing audit row with the wave it belongs to so the
// batch accumulator can defer both audit writes and rework transitions until
// after the whole batch has been evaluated (see A5).
type v7LandedEntry struct {
	WaveID string
	Entry  v7LandingAuditEntry
}

type v7LandFailure struct {
	WaveID  string
	Task    v7LandTask
	Summary string
}

// v7BatchAccumulator collects the outcome of staging tasks into integration
// branches without applying any side effects to the tracker (rework/audit).
type v7BatchAccumulator struct {
	Landed []v7LandedEntry
	Failed []v7LandFailure
}

// v7LandSummary accumulates the human-facing landing summary printed on a
// successful land (A4).
type v7LandSummary struct {
	Landed    []v7LandSummaryRow
	Reworked  []v7LandSummaryRow
	MainNotes []string
}

type v7LandSummaryRow struct {
	Task       string
	Branch     string
	Target     string
	GateResult string
	Commit     string
}

func landV7Cmd(args Args) error {
	return landV7CmdWithAuthority(args, nil, "", nil, nil)
}

func landV7CmdWithFrozenSources(args Args, frozenSources map[string]string) error {
	return tuskerError(errorInvalidTransition, "scheduled landing refusal: frozen sources require an internal daemon authority capability")
}

func landV7CmdWithDepartureAuthority(args Args, frozenSources map[string]string, authority *v7LandingAuthority) error {
	if authority == nil || len(authority.private) != ed25519.PrivateKeySize {
		return tuskerError(errorInvalidTransition, "scheduled landing refusal: daemon authority capability is unavailable")
	}
	internal, err := newV7InternalActor(firstNonEmpty(args.String("actor"), args.String("by")))
	if err != nil {
		return err
	}
	return landV7CmdWithAuthority(args, frozenSources, v7LandingAuthorityDeparture, authority, &internal)
}

func landV7CmdAsWaveDrain(args Args) error {
	// Wave draining is a daemon workflow label, not a signing capability.
	// Its ordinary landing receipt may support idempotent Git staging, but it
	// must never become scheduled-departure source authority.
	internal, err := newV7InternalActor("daemon:wave-drain")
	if err != nil {
		return err
	}
	return landV7CmdWithAuthority(args, nil, "", nil, &internal)
}

func landV7CmdWithAuthority(args Args, frozenSources map[string]string, authority string, capability *v7LandingAuthority, internal *v7InternalActor) error {
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	var actor string
	if internal != nil {
		actor = internal.value
	} else {
		actor, err = v7AgentDefaultActor(args, "land")
		if err != nil {
			return err
		}
	}
	args["by"] = actor
	targets := landTargets(args)
	if len(targets) == 0 {
		return tuskerError(errorMissingArg, "Usage: tusker land <TASK-ID> [TASK-ID...] or tusker land <W-0001>")
	}
	release, err := acquireV7LandingLock(vaultPath)
	if err != nil {
		return err
	}
	defer release()

	summary := &v7LandSummary{}
	if len(targets) == 1 && v7WaveIDPattern.MatchString(targets[0]) {
		if err := landV7WaveToMain(vaultPath, targets[0], args, summary); err != nil {
			return err
		}
	} else if err := landV7TaskTargets(vaultPath, targets, args, summary, frozenSources, authority, capability); err != nil {
		return err
	}
	printV7LandingSummary(summary, args)
	return nil
}

func landTargets(args Args) []string {
	var out []string
	for i := 0; ; i++ {
		value, ok := args[fmt.Sprintf("_pos%d", i)]
		if !ok {
			break
		}
		out = append(out, splitCSV(value)...)
	}
	out = append(out, splitCSV(args.String("task"))...)
	out = append(out, splitCSV(args.String("tasks"))...)
	for i, value := range out {
		out[i] = strings.ToUpper(strings.TrimSpace(wikiTarget(value)))
	}
	return uniqueStrings(filterStrings(out))
}

func acquireV7LandingLock(vaultPath string) (func(), error) {
	lockPath := filepath.Join(vaultPath, "_system", "land.lock")
	if err := ensureDir(filepath.Dir(lockPath)); err != nil {
		return nil, err
	}
	unlock, err := acquireV7LandingLockRecoveryGuard(lockPath)
	if err != nil {
		return nil, err
	}
	defer unlock()

	for {
		file, openErr := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if openErr == nil {
			now := time.Now().UTC()
			processStartedAt, verified := processStartTime(os.Getpid())
			if !verified {
				processStartedAt = now.Format(time.RFC3339Nano)
			}
			host, hostVerified := v7LandingLockHostIdentity()
			owner := v7LandingLockOwner{
				Schema:               v7LandingLockSchema,
				Token:                strings.ToLower(newRecordID()),
				PID:                  os.Getpid(),
				Host:                 host,
				HostVerified:         hostVerified,
				ProcessStartedAt:     processStartedAt,
				ProcessStartVerified: verified,
				AcquiredAt:           now.Format(time.RFC3339Nano),
			}
			encodeErr := json.NewEncoder(file).Encode(owner)
			if encodeErr == nil {
				encodeErr = file.Sync()
			}
			if closeErr := file.Close(); encodeErr == nil {
				encodeErr = closeErr
			}
			if encodeErr != nil {
				_ = os.Remove(lockPath)
				return nil, fmt.Errorf("initialize landing lock: %w", encodeErr)
			}
			return func() { releaseV7LandingLock(lockPath, owner.Token) }, nil
		}
		if !os.IsExist(openErr) {
			return nil, openErr
		}
		recovered, reason, recoverErr := recoverV7LandingLock(lockPath, time.Now().UTC())
		if recoverErr != nil {
			return nil, recoverErr
		}
		if recovered {
			continue
		}
		message := "landing lane is already running"
		if reason != "" {
			message += "; " + reason
		}
		return nil, tuskerError(errorInvalidTransition, message, withPath(lockPath))
	}
}

func acquireV7LandingLockRecoveryGuard(lockPath string) (func(), error) {
	guard, err := os.OpenFile(v7LandingLockRecoveryGuardPath(lockPath), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(guard.Fd()), syscall.LOCK_EX); err != nil {
		_ = guard.Close()
		return nil, err
	}
	return func() {
		_ = syscall.Flock(int(guard.Fd()), syscall.LOCK_UN)
		_ = guard.Close()
	}, nil
}

func v7LandingLockRecoveryGuardPath(lockPath string) string {
	canonical, err := filepath.Abs(lockPath)
	if err != nil {
		canonical = lockPath
	}
	if physicalDir, evalErr := filepath.EvalSymlinks(filepath.Dir(canonical)); evalErr == nil {
		canonical = filepath.Join(physicalDir, filepath.Base(canonical))
	}
	digest := sha256.Sum256([]byte(canonical))
	return filepath.Join(os.TempDir(), fmt.Sprintf("tusker-landing-lock-%x.guard", digest[:16]))
}

func recoverV7LandingLock(lockPath string, now time.Time) (bool, string, error) {
	raw, err := os.ReadFile(lockPath)
	if err != nil {
		return false, "", err
	}
	info, err := os.Stat(lockPath)
	if err != nil {
		return false, "", err
	}
	var owner v7LandingLockOwner
	if err := json.Unmarshal(raw, &owner); err != nil || !validV7LandingLockOwner(owner) {
		return false, "lock owner metadata is malformed and was not stolen", nil
	}
	acquiredAt, _ := time.Parse(time.RFC3339Nano, owner.AcquiredAt)
	recoveryCutoff := now.Add(-v7LandingLockRecoveryGrace)
	if acquiredAt.After(recoveryCutoff) || info.ModTime().After(recoveryCutoff) {
		return false, "lock is too fresh for safe stale recovery", nil
	}
	host, hostVerified := v7LandingLockHostIdentity()
	if !owner.HostVerified || !hostVerified || owner.Host != host {
		return false, "lock owner is on another host and cannot be proven stale", nil
	}
	if processAlive(owner.PID) {
		if !owner.ProcessStartVerified {
			return false, fmt.Sprintf("lock owner pid %d is still alive", owner.PID), nil
		}
		actualStart, ok := processStartTime(owner.PID)
		if !ok || actualStart == owner.ProcessStartedAt {
			return false, fmt.Sprintf("lock owner pid %d is still alive", owner.PID), nil
		}
	}
	if err := os.Remove(lockPath); err != nil {
		return false, "", fmt.Errorf("recover stale landing lock: %w", err)
	}
	return true, "", nil
}

func v7LandingLockHostIdentity() (string, bool) {
	host, err := os.Hostname()
	host = strings.TrimSpace(host)
	if err != nil || host == "" {
		return "unknown", false
	}
	return host, true
}

func validV7LandingLockOwner(owner v7LandingLockOwner) bool {
	if owner.Schema != v7LandingLockSchema ||
		strings.TrimSpace(owner.Token) == "" ||
		owner.PID <= 0 ||
		strings.TrimSpace(owner.Host) == "" ||
		strings.TrimSpace(owner.ProcessStartedAt) == "" {
		return false
	}
	_, err := time.Parse(time.RFC3339Nano, owner.AcquiredAt)
	return err == nil
}

func releaseV7LandingLock(lockPath, token string) {
	unlock, err := acquireV7LandingLockRecoveryGuard(lockPath)
	if err != nil {
		return
	}
	defer unlock()
	raw, err := os.ReadFile(lockPath)
	if err != nil {
		return
	}
	var owner v7LandingLockOwner
	if json.Unmarshal(raw, &owner) != nil || owner.Token != token {
		return
	}
	_ = os.Remove(lockPath)
}

// refuseV7TerminalWaveLanding is the durable landing authority boundary. UI
// projections may lag, so task and wave landing paths both fail closed here.
// A derived status=landed is not itself a receipt: reconcile can observe that
// every member is done before Git has moved. Only landed_at or a durable wave
// landing audit row closes the wave.
func refuseV7TerminalWaveLanding(waveID string, wave Note) error {
	if v7WaveHasDurableTerminal(wave) {
		return tuskerError(errorInvalidTransition, "landing refused: wave "+waveID+" is already landed or closed")
	}
	return nil
}

func v7WaveHasDurableTerminal(wave Note) bool {
	status := strings.ToLower(strings.TrimSpace(stringField(wave.Data, "status")))
	return status == "closed" || status == "cancelled" || status == "superseded" ||
		strings.TrimSpace(stringField(wave.Data, "landed_at")) != "" || v7WaveHasLandingReceipt(wave)
}

func v7WaveHasLandingReceipt(wave Note) bool {
	_, ok := v7WaveLandingReceiptAt(wave.Data["landings"])
	return ok
}

func v7WaveLandingReceiptAt(value any) (string, bool) {
	latest := ""
	found := false
	for _, row := range normalizeLandingAudit(value) {
		if stringField(row, "task") != "wave" || !strings.EqualFold(stringField(row, "gate_result"), "pass") {
			continue
		}
		found = true
		if timestamp := strings.TrimSpace(stringField(row, "timestamp")); timestamp > latest {
			latest = timestamp
		}
	}
	return latest, found
}

func landV7TaskTargets(vaultPath string, targets []string, args Args, summary *v7LandSummary, frozenSources map[string]string, authority string, capability *v7LandingAuthority) error {
	idx, err := loadV7Index(vaultPath)
	if err != nil {
		return err
	}
	repoRoot := v7RepoRoot(vaultPath)
	actor := landV7Actor(args)
	byWave := map[string][]v7LandTask{}
	for _, taskID := range targets {
		task, ok := idx.Tasks[taskID]
		if !ok {
			return tuskerError(errorNotFound, "V7 task not found: "+taskID)
		}
		waveID := stringField(task.Data, "wave")
		if waveID == "" {
			if unitID, created, unitErr := ensureV7ImplicitSingletonDeliveryUnit(vaultPath, taskID, args); unitErr != nil {
				return unitErr
			} else if created {
				// The unit and task back-pointer were written atomically enough for
				// replay: reload so this invocation uses the canonical binding.
				idx, err = loadV7Index(vaultPath)
				if err != nil {
					return err
				}
				task = idx.Tasks[taskID]
				waveID = stringField(task.Data, "wave")
			} else {
				waveID = unitID
			}
			if waveID == "" {
				return tuskerError(errorInvalidTransition, v7NoWaveRefusal(taskID))
			}
		}
		wave, ok := idx.Waves[waveID]
		if !ok {
			return tuskerError(errorNotFound, "V7 wave not found: "+waveID)
		}
		if err := refuseV7TerminalWaveLanding(waveID, wave); err != nil {
			return err
		}
		branch := v7TaskBranchName(taskID)
		if len(targets) == 1 {
			branch = firstNonEmpty(strings.TrimSpace(args.String("branch")), branch)
		}
		sourceSHA := ""
		if frozenSources != nil {
			sourceSHA = strings.TrimSpace(frozenSources[taskID])
			if sourceSHA == "" {
				return tuskerError(errorInvalidTransition, "scheduled staging refusal: source_drift:"+taskID+": frozen source is missing")
			}
			resolved, resolveErr := gitOutputTrim(repoRoot, "rev-parse", sourceSHA+"^{commit}")
			if resolveErr != nil {
				return tuskerError(errorInvalidTransition, "scheduled staging refusal: source_drift:"+taskID+": frozen source is unavailable")
			}
			if !strings.EqualFold(sourceSHA, resolved) {
				return tuskerError(errorInvalidTransition, "scheduled staging refusal: source_drift:"+taskID+": frozen source must be a full immutable commit SHA")
			}
			sourceSHA = resolved
		}
		if sourceSHA == "" && v7GitRepo(repoRoot) && !gitBranchExists(repoRoot, branch) {
			if err := ensureV7TaskLandingBranch(repoRoot, taskID, branch, args); err != nil {
				return err
			}
		}
		if sourceSHA == "" {
			resolved, resolveErr := gitOutputTrim(repoRoot, "rev-parse", branch+"^{commit}")
			if resolveErr != nil {
				return tuskerError(errorInvalidTransition, "landing source is unavailable for "+taskID+": "+firstActionableLine(resolveErr.Error(), resolveErr.Error()))
			}
			sourceSHA = resolved
		}
		landTask := v7LandTask{ID: taskID, Branch: branch, SourceSHA: sourceSHA}
		landTask.SourceProvenance = v7LandingSourceProvenance(vaultPath, repoRoot, landTask, args)
		if landTask.SourceProvenance == "" {
			if args.Bool("trust-from") || args.Bool("trusted-from") {
				landTask.SourceProvenance = "trusted_override"
			} else {
				return tuskerError(errorInvalidTransition, "refused landing source for "+taskID+": exact commit lacks task-owned provenance")
			}
		}
		if trustedV7LandingControlAuthority(authority, actor) && !trustedV7LandingSourceProvenance(landTask.SourceProvenance) {
			integrationBranch := v7WaveIntegrationBranch(idx.Waves[waveID])
			if !gitMergeBaseAncestor(repoRoot, landTask.SourceSHA, integrationBranch) {
				return tuskerError(errorInvalidTransition, "scheduled landing refusal: task_source_provenance_missing:"+taskID)
			}
		}
		byWave[waveID] = append(byWave[waveID], landTask)
	}
	waveIDs := make([]string, 0, len(byWave))
	for waveID := range byWave {
		waveIDs = append(waveIDs, waveID)
	}
	sort.Strings(waveIDs)

	// Phase 1: stage every wave's batch into integration. Passing tasks move
	// the integration ref forward; rework/audit side effects are deferred.
	acc := &v7BatchAccumulator{}
	for _, waveID := range waveIDs {
		if err := stageV7WaveBatch(vaultPath, repoRoot, waveID, byWave[waveID], actor, authority, capability, acc); err != nil {
			return err
		}
	}

	// A5: if nothing landed at all, exit non-zero with an unmistakable
	// failed-batch summary BEFORE touching any task state.
	if len(acc.Landed) == 0 {
		return tuskerError(errorInvalidTransition, v7FailedBatchSummary(acc))
	}

	// Phase 2: apply rework transitions, write landing audit, and build the
	// human-facing summary now that we know the batch produced real landings.
	for _, waveID := range waveIDs {
		var entries []v7LandingAuditEntry
		for _, landed := range acc.Landed {
			if landed.WaveID != waveID {
				continue
			}
			entry := landed.Entry
			if entry.Actor == "" {
				entry.Actor = actor
			}
			entries = append(entries, entry)
			summary.Landed = append(summary.Landed, v7LandSummaryRow{
				Task: entry.Task, Branch: entry.Branch, Target: entry.Target,
				GateResult: entry.GateResult, Commit: entry.Commit,
			})
		}
		for _, failure := range acc.Failed {
			if failure.WaveID != waveID {
				continue
			}
			if err := kickV7LandingTaskToRework(vaultPath, failure.Task.ID, failure.Summary, actor); err != nil {
				return err
			}
			entries = append(entries, v7LandingAuditEntry{
				Task: failure.Task.ID, Branch: failure.Task.Branch,
				SourceSHA: failure.Task.SourceSHA,
				Target:    v7IntegrationBranchName(waveID), GateResult: "fail",
				GateSummary: failure.Summary, Actor: actor,
				Timestamp: time.Now().UTC().Format(time.RFC3339),
			})
			summary.Reworked = append(summary.Reworked, v7LandSummaryRow{
				Task: failure.Task.ID, Branch: failure.Task.Branch,
				Target: v7IntegrationBranchName(waveID), GateResult: "fail",
			})
		}
		if err := appendV7WaveLandingAudit(vaultPath, waveID, entries, actor); err != nil {
			return err
		}
		if err := landV7WaveToMainIfReady(vaultPath, waveID, args, summary); err != nil {
			return err
		}
	}
	return nil
}

// v7NoWaveRefusal renders the actionable A1 refusal for a task with no wave.
func v7NoWaveRefusal(taskID string) string {
	return taskID + " is not in a wave; merge-lane land requires wave membership. " +
		"Create one and retry: tusker wave create \"<title>\" " + taskID +
		"  (then: tusker land " + taskID + ")"
}

// v7FailedBatchSummary renders the A5 failed-batch summary for a batch where
// no requested task landed.
func v7FailedBatchSummary(acc *v7BatchAccumulator) string {
	total := len(acc.Failed)
	target := "integration"
	if total > 0 {
		target = v7IntegrationBranchName(acc.Failed[0].WaveID)
	}
	first := ""
	if total > 0 {
		first = "; first failure: " + acc.Failed[0].Task.ID + ": " + acc.Failed[0].Summary
	}
	return fmt.Sprintf("land batch failed: 0 of %d task%s landed to %s%s", total, plural(total), target, first)
}

func landV7Actor(args Args) string {
	return fallback(fallback(args.String("actor"), args.String("by")), "agent:"+defaultActorName())
}

func v7LandingSourceProvenance(vaultPath, repoRoot string, task v7LandTask, args Args) string {
	idx, err := loadV7Index(vaultPath)
	if err == nil {
		if note, ok := idx.Tasks[task.ID]; ok {
			recorded := firstNonEmpty(
				stringField(note.Data, "source_sha"),
				stringField(note.Data, "source_commit"),
				stringField(note.Data, "source_branch_sha"),
			)
			if strings.EqualFold(strings.TrimSpace(recorded), strings.TrimSpace(task.SourceSHA)) {
				return "durable_task_source"
			}
		}
	}
	if strings.EqualFold(v7CommitWorkspaceRecordID(repoRoot, task.SourceSHA), task.ID) {
		return "workspace_record"
	}
	if v7CommitTouchesTaskTracker(repoRoot, task.SourceSHA, task.ID) {
		return "task_tracker"
	}
	if from := strings.TrimSpace(args.String("from")); from != "" {
		if info, err := os.Stat(from); err == nil && info.IsDir() &&
			strings.EqualFold(v7WorkspaceRecordID(from), task.ID) {
			if head, ok := gitRevParse(from, "HEAD^{commit}"); ok && strings.EqualFold(head, task.SourceSHA) {
				return "workspace_claim"
			}
		}
	}
	if task.Branch == v7TaskBranchName(task.ID) {
		if head, ok := gitRevParse(repoRoot, task.Branch+"^{commit}"); ok && strings.EqualFold(head, task.SourceSHA) {
			return "task_branch_head"
		}
	}
	return ""
}

func trustedV7LandingSourceProvenance(provenance string) bool {
	switch strings.TrimSpace(provenance) {
	case "durable_task_source", "workspace_record", "workspace_claim", "task_tracker":
		return true
	default:
		return false
	}
}

func trustedV7LandingControlAuthority(authority, actor string) bool {
	switch strings.TrimSpace(authority) {
	case v7LandingAuthorityDeparture:
		return strings.HasPrefix(actor, "daemon:departure:") &&
			strings.TrimSpace(strings.TrimPrefix(actor, "daemon:departure:")) != ""
	default:
		return false
	}
}

// ensureV7TaskLandingBranch materializes the task/<ID> branch for a detached
// completed worktree (A2). It prefers an explicit --from ref, otherwise
// auto-discovers a single detached worktree associated with the task. When no
// source can be resolved it returns an actionable refusal naming the exact
// branch command to run before retry.
func ensureV7TaskLandingBranch(repoRoot, taskID, branch string, args Args) error {
	commit, source, err := discoverV7TaskLandingCommit(repoRoot, taskID, branch, args)
	if err != nil {
		return err
	}
	if err := importV7LandingCommit(repoRoot, commit, source); err != nil {
		return err
	}
	if _, err := gitCombined(repoRoot, "branch", branch, commit); err != nil {
		return tuskerError(errorInvalidTransition, "failed to create "+branch+" from "+source+": "+firstActionableLine(err.Error(), err.Error()))
	}
	return nil
}

// importV7LandingCommit makes the exact completed commit available to the
// canonical repository when --from names an isolated copy/clone workspace.
// Worktrees already share the object database and take the no-op path.
func importV7LandingCommit(repoRoot, commit, source string) error {
	if _, err := gitOutputTrim(repoRoot, "rev-parse", commit+"^{commit}"); err == nil {
		return nil
	}
	info, err := os.Stat(source)
	if err != nil || !info.IsDir() {
		return tuskerError(errorInvalidTransition, "landing commit "+commit+" from "+source+" is unavailable in the canonical repository")
	}
	if _, err := gitCombined(repoRoot, "fetch", "--no-tags", source, commit); err != nil {
		return tuskerError(errorInvalidTransition, "failed to import landing commit "+commit+" from "+source+": "+firstActionableLine(err.Error(), err.Error()))
	}
	resolved, err := gitOutputTrim(repoRoot, "rev-parse", commit+"^{commit}")
	if err != nil || !strings.EqualFold(strings.TrimSpace(resolved), strings.TrimSpace(commit)) {
		return tuskerError(errorInvalidTransition, "landing commit "+commit+" from "+source+" was not imported exactly")
	}
	return nil
}

func discoverV7TaskLandingCommit(repoRoot, taskID, branch string, args Args) (string, string, error) {
	if from := strings.TrimSpace(args.String("from")); from != "" {
		commit, source, err := resolveV7LandingSource(repoRoot, taskID, from, args)
		if err != nil {
			return "", "", err
		}
		return commit, source, nil
	}
	matches := v7DetachedWorktreesForTask(repoRoot, taskID)
	if len(matches) == 1 {
		return matches[0].HEAD, matches[0].Path, nil
	}
	if len(matches) > 1 {
		paths := make([]string, 0, len(matches))
		for _, m := range matches {
			paths = append(paths, m.Path)
		}
		return "", "", tuskerError(errorInvalidTransition, taskID+" has no "+branch+" branch and multiple detached worktrees match ("+strings.Join(paths, ", ")+"); pick one with: tusker land "+taskID+" --from <worktree-path-or-commit>")
	}
	return "", "", tuskerError(errorInvalidTransition, taskID+" has no "+branch+" branch and no detached completed worktree to branch from. Create it and retry: git branch "+branch+" <commit>  (or: tusker land "+taskID+" --from <worktree-path-or-commit>)")
}

func resolveV7LandingSource(repoRoot, taskID, ref string, args Args) (string, string, error) {
	if info, err := os.Stat(ref); err == nil && info.IsDir() {
		recordID := v7WorkspaceRecordID(ref)
		if recordID != strings.ToUpper(strings.TrimSpace(taskID)) {
			return "", "", tuskerError(errorInvalidTransition, "refused --from "+ref+" for "+taskID+": worktree metadata record_id "+firstNonEmpty(recordID, "<missing>")+" does not match "+taskID+". Use the task's runner worktree, or rerun with --from pointing at a source whose .tusker/workspace.json record_id is "+taskID)
		}
		commit, err := gitOutputTrim(ref, "rev-parse", "HEAD")
		if err != nil {
			return "", "", tuskerError(errorInvalidArg, "cannot resolve --from "+ref+" for "+taskID+": "+firstActionableLine(err.Error(), err.Error()))
		}
		return commit, ref, nil
	}
	commit, err := gitOutputTrim(repoRoot, "rev-parse", ref+"^{commit}")
	if err != nil {
		return "", "", tuskerError(errorInvalidArg, "cannot resolve --from "+ref+" for "+taskID+": "+firstActionableLine(err.Error(), err.Error()))
	}
	if err := validateV7LandingCommitSource(repoRoot, taskID, ref, commit, args); err != nil {
		return "", "", err
	}
	return commit, ref, nil
}

func validateV7LandingCommitSource(repoRoot, taskID, ref, commit string, args Args) error {
	upperID := strings.ToUpper(strings.TrimSpace(taskID))
	if recordID := v7CommitWorkspaceRecordID(repoRoot, commit); recordID != "" {
		if recordID == upperID {
			return nil
		}
		if args.Bool("trust-from") || args.Bool("trusted-from") {
			_, _ = fmt.Fprintf(os.Stderr, "tusker land: trusted --from override for %s using %s (%s; workspace record_id %s)\n", upperID, ref, shortCommit(commit), recordID)
			return nil
		}
		return tuskerError(errorInvalidTransition, "refused --from "+ref+" for "+upperID+": source commit workspace record_id "+recordID+" does not match "+upperID+". Use the task runner worktree, pass a task-local tracker commit, or rerun with --trust-from to record an explicit trusted override")
	}
	if v7CommitTouchesTaskTracker(repoRoot, commit, upperID) {
		return nil
	}
	if args.Bool("trust-from") || args.Bool("trusted-from") {
		_, _ = fmt.Fprintf(os.Stderr, "tusker land: trusted --from override for %s using %s (%s)\n", upperID, ref, shortCommit(commit))
		return nil
	}
	return tuskerError(errorInvalidTransition, "refused --from "+ref+" for "+upperID+": source commit lacks task-owned provenance. Use the task runner worktree with .tusker/workspace.json record_id "+upperID+", pass a commit that touches .tusker/work/tasks/"+upperID+".md, or rerun with --trust-from to record an explicit trusted override")
}

func v7CommitWorkspaceRecordID(repoRoot, commit string) string {
	raw, err := gitOutputTrim(repoRoot, "show", commit+":.tusker/workspace.json")
	if err != nil {
		return ""
	}
	var meta struct {
		RecordID string `json:"record_id"`
	}
	if json.Unmarshal([]byte(raw), &meta) != nil {
		return ""
	}
	return strings.ToUpper(strings.TrimSpace(meta.RecordID))
}

func v7CommitTouchesTaskTracker(repoRoot, commit, taskID string) bool {
	path := filepath.ToSlash(filepath.Join(".tusker", "work", "tasks", taskID+".md"))
	changed, err := gitOutputTrim(repoRoot, "diff-tree", "--root", "--no-commit-id", "--name-only", "-r", commit)
	if err != nil {
		return false
	}
	found := false
	for _, line := range strings.Split(changed, "\n") {
		if strings.TrimSpace(filepath.ToSlash(line)) == path {
			found = true
			break
		}
	}
	if !found {
		return false
	}
	content, err := gitOutputTrim(repoRoot, "show", commit+":"+path)
	if err != nil {
		return false
	}
	data, _, err := parseFrontmatter(content)
	if err != nil {
		return false
	}
	return strings.EqualFold(stringField(data, "id"), taskID)
}

type v7Worktree struct {
	Path     string
	HEAD     string
	Branch   string
	Detached bool
}

// v7DetachedWorktreesForTask returns detached git worktrees whose workspace
// metadata record id exactly matches taskID.
func v7DetachedWorktreesForTask(repoRoot, taskID string) []v7Worktree {
	var matches []v7Worktree
	root, _ := filepath.Abs(repoRoot)
	upperID := strings.ToUpper(strings.TrimSpace(taskID))
	for _, wt := range v7ListWorktrees(repoRoot) {
		if !wt.Detached || wt.HEAD == "" {
			continue
		}
		abs, _ := filepath.Abs(wt.Path)
		if abs == root {
			continue
		}
		if v7WorkspaceRecordID(wt.Path) == upperID {
			matches = append(matches, wt)
		}
	}
	return matches
}

func v7ListWorktrees(repoRoot string) []v7Worktree {
	output, err := gitCombined(repoRoot, "worktree", "list", "--porcelain")
	if err != nil {
		return nil
	}
	var worktrees []v7Worktree
	var current v7Worktree
	flush := func() {
		if current.Path != "" {
			worktrees = append(worktrees, current)
		}
		current = v7Worktree{}
	}
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimRight(line, "\r")
		switch {
		case strings.TrimSpace(line) == "":
			flush()
		case strings.HasPrefix(line, "worktree "):
			flush()
			current.Path = strings.TrimSpace(strings.TrimPrefix(line, "worktree "))
		case strings.HasPrefix(line, "HEAD "):
			current.HEAD = strings.TrimSpace(strings.TrimPrefix(line, "HEAD "))
		case strings.HasPrefix(line, "branch "):
			current.Branch = strings.TrimSpace(strings.TrimPrefix(line, "branch "))
		case strings.TrimSpace(line) == "detached":
			current.Detached = true
		}
	}
	flush()
	return worktrees
}

func v7WorkspaceRecordID(worktreePath string) string {
	raw, err := os.ReadFile(filepath.Join(worktreePath, ".tusker", "workspace.json"))
	if err != nil {
		return ""
	}
	var meta struct {
		RecordID string `json:"record_id"`
	}
	if json.Unmarshal(raw, &meta) != nil {
		return ""
	}
	return strings.ToUpper(strings.TrimSpace(meta.RecordID))
}

func stageV7WaveBatch(vaultPath, repoRoot, waveID string, tasks []v7LandTask, actor, authority string, capability *v7LandingAuthority, acc *v7BatchAccumulator) error {
	if len(tasks) == 0 {
		return nil
	}
	idx, err := loadV7Index(vaultPath)
	if err != nil {
		return err
	}
	wave, ok := idx.Waves[waveID]
	if !ok {
		return tuskerError(errorNotFound, "V7 wave not found: "+waveID)
	}
	if !v7GitRepo(repoRoot) {
		return tuskerError(errorInvalidTransition, "tusker land requires a Git repository", withPath(repoRoot))
	}
	integrationBranch := v7WaveIntegrationBranch(wave)
	if err := ensureV7WaveIntegrationBranch(vaultPath, wave); err != nil {
		return err
	}
	return landV7BatchRecursive(vaultPath, repoRoot, waveID, integrationBranch, tasks, actor, authority, capability, acc)
}

func landV7BatchRecursive(vaultPath, repoRoot, waveID, integrationBranch string, tasks []v7LandTask, actor, authority string, capability *v7LandingAuthority, acc *v7BatchAccumulator) error {
	if len(tasks) == 0 {
		return nil
	}
	var pending []v7LandTask
	for _, task := range tasks {
		if gitMergeBaseAncestor(repoRoot, task.SourceSHA, integrationBranch) {
			if !trustedV7LandingControlAuthority(authority, actor) {
				head, headOK := gitRevParse(repoRoot, integrationBranch+"^{commit}")
				tree, treeOK := gitRevParse(repoRoot, integrationBranch+"^{tree}")
				if !headOK || !treeOK {
					return tuskerError(errorInvalidTransition, "landing recovery refusal: integration identity unavailable for "+task.ID)
				}
				acc.Landed = append(acc.Landed, v7LandedEntry{WaveID: waveID, Entry: v7LandingAuditEntry{
					Task: task.ID, Branch: task.Branch, SourceSHA: task.SourceSHA,
					SourceProvenance: task.SourceProvenance, Target: integrationBranch,
					GateResult: "pass", GateSummary: "already integrated; no control-plane receipt",
					Commit: head, Tree: tree, Timestamp: time.Now().UTC().Format(time.RFC3339),
				}})
				continue
			}
			var trustedStore *RuntimeStore
			if capability != nil {
				trustedStore = capability.store
			}
			entry, recovered := recoverV7LandingAuditFromReceipt(vaultPath, repoRoot, integrationBranch, task, trustedStore)
			if !recovered {
				return tuskerError(errorInvalidTransition, "landing recovery refusal: verified receipt missing for already-integrated source "+task.ID+"@"+task.SourceSHA)
			}
			acc.Landed = append(acc.Landed, v7LandedEntry{WaveID: waveID, Entry: entry})
			continue
		}
		pending = append(pending, task)
	}
	if len(pending) == 0 {
		return nil
	}
	tasks = pending
	result, err := stageV7LandingBatch(vaultPath, repoRoot, integrationBranch, tasks, actor, authority, capability)
	if err != nil {
		return err
	}
	if result.Pass {
		if err := updateGitRef(repoRoot, "refs/heads/"+integrationBranch, result.Receipt.BatchHeadSHA, result.Receipt.BatchBaseSHA); err != nil {
			return err
		}
		now := time.Now().UTC().Format(time.RFC3339)
		for _, proof := range result.Receipt.Tasks {
			acc.Landed = append(acc.Landed, v7LandedEntry{WaveID: waveID, Entry: v7LandingAuditEntry{
				Task: proof.Task, Branch: proof.Branch, SourceSHA: proof.SourceSHA,
				SourceProvenance: proof.SourceProvenance, Target: integrationBranch,
				BaseSHA: proof.BaseSHA, MergeCommit: proof.MergeCommit,
				GateResult: "pass", GateSummary: result.Receipt.GateSummary,
				GateFingerprint: result.Receipt.GateFingerprint, ReceiptFingerprint: result.Receipt.Fingerprint,
				ControlAuthority: result.Receipt.ControlAuthority,
				Commit:           result.Receipt.BatchHeadSHA, Tree: result.Receipt.BatchTreeSHA,
				Actor: result.Receipt.Actor, Timestamp: now,
			}})
		}
		return nil
	}
	if len(tasks) == 1 {
		acc.Failed = append(acc.Failed, v7LandFailure{WaveID: waveID, Task: tasks[0], Summary: result.Summary})
		return nil
	}
	mid := len(tasks) / 2
	if err := landV7BatchRecursive(vaultPath, repoRoot, waveID, integrationBranch, tasks[:mid], actor, authority, capability, acc); err != nil {
		return err
	}
	return landV7BatchRecursive(vaultPath, repoRoot, waveID, integrationBranch, tasks[mid:], actor, authority, capability, acc)
}

func stageV7LandingBatch(vaultPath, repoRoot, baseBranch string, tasks []v7LandTask, actor, authority string, capability *v7LandingAuthority) (v7LandingBatchResult, error) {
	tmp, err := os.MkdirTemp("", "tusker-land-stage-*")
	if err != nil {
		return v7LandingBatchResult{}, err
	}
	removeWorktree := false
	defer func() {
		if removeWorktree {
			_ = exec.Command("git", "-C", repoRoot, "worktree", "remove", "--force", tmp).Run()
		} else {
			_ = os.RemoveAll(tmp)
		}
	}()
	if output, err := gitCombined(repoRoot, "worktree", "add", "--detach", tmp, baseBranch); err != nil {
		return v7LandingBatchResult{}, tuskerError(errorInvalidTransition, "failed to create landing staging worktree: "+firstActionableLine(output, err.Error()))
	}
	removeWorktree = true
	batchBase, err := gitOutputTrim(tmp, "rev-parse", "HEAD")
	if err != nil {
		return v7LandingBatchResult{}, err
	}
	segment := make([]string, 0, len(tasks)+1)
	proofs := make([]v7LandingReceiptTask, 0, len(tasks))
	for _, task := range tasks {
		base, err := gitOutputTrim(tmp, "rev-parse", "HEAD")
		if err != nil {
			return v7LandingBatchResult{}, err
		}
		source := strings.TrimSpace(task.SourceSHA)
		if output, err := gitCombined(tmp, "merge", "--no-ff", "--no-edit", source); err != nil {
			resolved, unresolved, resolveErr := resolveV7GeneratedProjectionMerge(tmp)
			if resolveErr != nil {
				return v7LandingBatchResult{}, resolveErr
			}
			if !resolved {
				summary := landingFailureSummary("merge "+source, output, err)
				if unresolved != "" {
					summary = limitLandingSummary(summary+"; all unmerged paths: "+unresolved, 500)
				}
				return v7LandingBatchResult{Summary: summary}, nil
			}
		}
		mergeCommit, err := gitOutputTrim(tmp, "rev-parse", "HEAD")
		if err != nil {
			return v7LandingBatchResult{}, err
		}
		if strings.EqualFold(base, mergeCommit) {
			return v7LandingBatchResult{}, tuskerError(errorInvalidTransition, "landing receipt refusal: "+task.ID+" did not produce an exact merge commit")
		}
		segment = append(segment, mergeCommit)
		proofs = append(proofs, v7LandingReceiptTask{
			Task: task.ID, Branch: task.Branch, SourceSHA: source,
			SourceProvenance: task.SourceProvenance, BaseSHA: base, MergeCommit: mergeCommit,
		})
		if err := guardV7LandingTerminalTaskRewinds(tmp, baseBranch); err != nil {
			return v7LandingBatchResult{}, err
		}
	}
	if err := removeV7WorkspaceMetadataFromLanding(tmp); err != nil {
		return v7LandingBatchResult{}, err
	}
	if err := commitV7LandingCleanup(tmp); err != nil {
		return v7LandingBatchResult{}, err
	}
	commit, err := gitOutputTrim(tmp, "rev-parse", "HEAD")
	if err != nil {
		return v7LandingBatchResult{}, err
	}
	if len(segment) == 0 || !strings.EqualFold(segment[len(segment)-1], commit) {
		segment = append(segment, commit)
	}
	tree, err := gitOutputTrim(tmp, "rev-parse", commit+"^{tree}")
	if err != nil {
		return v7LandingBatchResult{}, err
	}
	laneIdentity := v7LandingBatchIdentity(tasks)
	gate := runV7LandingGateEvidenceWithIsolation(vaultPath, tmp, laneIdentity, authority == v7LandingAuthorityDeparture)
	if !gate.Pass {
		return v7LandingBatchResult{Summary: gate.Summary}, nil
	}
	receipt := v7LandingReceipt{
		Schema: v7LandingReceiptSchema, GateFingerprint: gate.Fingerprint,
		LaneIdentity: laneIdentity, Target: baseBranch, Actor: actor, ControlAuthority: authority,
		BatchBaseSHA: batchBase, BatchHeadSHA: commit, BatchTreeSHA: tree,
		BatchSegment: segment, Tasks: proofs, Commands: gate.Commands,
		Toolchains: gate.Toolchains, Outcome: "pass", GateSummary: gate.Summary,
		ReceiptIssuedAt: time.Now().UTC().Format(time.RFC3339Nano),
		GateStartedAt:   gate.StartedAt, GateFinishedAt: gate.FinishedAt,
		CommandOutcomes: append([]v7LandingCommandOutcome(nil), gate.Outcomes...),
	}
	if authority == v7LandingAuthorityDeparture {
		if capability == nil || capability.Issuance.Context.Target != baseBranch ||
			capability.Issuance.Context.Candidate.CandidateSHA == "" ||
			capability.Issuance.Context.Candidate.CandidateTreeHash == "" {
			return v7LandingBatchResult{}, tuskerError(errorInvalidTransition, "scheduled landing refusal: missing or mismatched daemon authority")
		}
		i := capability.Issuance
		receipt.ProjectID, receipt.RepoIdentity, receipt.DepartureID, receipt.PolicyID, receipt.ScheduledWindow = i.ProjectID, i.RepoIdentity, i.DepartureID, i.PolicyID, i.ScheduledWindow
		receipt.DaemonSessionID, receipt.DaemonHost, receipt.DaemonProcess = i.SessionID, i.HostIdentity, i.ProcessIdentity
		receipt.AuthorityID, receipt.AuthorityGen = i.AuthorityID, i.Generation
	}
	receipt.Fingerprint = v7LandingReceiptFingerprint(receipt)
	if capability != nil {
		receipt.AuthoritySignature = ed25519.Sign(capability.private, []byte(receipt.Fingerprint))
	}
	if err := writeV7LandingReceipt(vaultPath, receipt); err != nil {
		return v7LandingBatchResult{}, err
	}
	return v7LandingBatchResult{Pass: true, Summary: gate.Summary, Receipt: receipt}, nil
}

// resolveV7GeneratedProjectionMerge prevents derived Tusker dashboards from
// serializing otherwise-independent task landings. Only an all-generated
// conflict is eligible; any source or task-contract conflict remains a hard
// landing failure.
func resolveV7GeneratedProjectionMerge(workDir string) (bool, string, error) {
	return resolveV7ProjectionMerge(workDir, "")
}

// resolveV7CompletionProjectionMerge additionally retains the integration
// copy of unrelated task records. A worker can carry stale control-plane
// snapshots for other tasks in its worktree; those records are not part of
// the reviewed implementation and cannot veto its completion. The reviewed
// task itself is deliberately excluded from this exception.
func resolveV7CompletionProjectionMerge(workDir, reviewedTaskID string) (bool, string, error) {
	return resolveV7ProjectionMerge(workDir, strings.ToUpper(strings.TrimSpace(reviewedTaskID)))
}

func resolveV7ProjectionMerge(workDir, reviewedTaskID string) (bool, string, error) {
	output, err := gitCombined(workDir, "diff", "--name-only", "--diff-filter=U")
	if err != nil {
		return false, "", err
	}
	paths := strings.Fields(output)
	if len(paths) == 0 {
		return false, "", nil
	}
	retainIntegrationTasks := make([]string, 0)
	for _, path := range paths {
		generated := path == ".tusker/Dashboard.md" || path == ".tusker/workspace.json" || strings.HasPrefix(path, ".tusker/_generated/") || strings.HasPrefix(path, ".tusker/dashboards/")
		if strings.HasPrefix(path, ".tusker/work/epics/") && strings.HasSuffix(path, ".md") {
			generated = v7EpicConflictOnlyTouchesManagedState(workDir, path)
		}
		if strings.HasPrefix(path, ".tusker/work/tasks/") && strings.HasSuffix(path, ".md") && reviewedTaskID != "" {
			taskID := strings.TrimSuffix(filepath.Base(path), ".md")
			if !strings.EqualFold(taskID, reviewedTaskID) {
				retainIntegrationTasks = append(retainIntegrationTasks, path)
				continue
			}
		}
		if !generated {
			return false, strings.Join(paths, ", "), nil
		}
	}
	for _, path := range paths {
		if strings.HasPrefix(path, ".tusker/work/epics/") {
			if output, err := gitCombined(workDir, "checkout", "--ours", "--", path); err != nil {
				return false, "", tuskerError(errorInvalidTransition, "failed to retain epic source while regenerating managed blocks: "+firstActionableLine(output, err.Error()))
			}
		}
		if containsString(retainIntegrationTasks, path) {
			if output, err := gitCombined(workDir, "checkout", "--ours", "--", path); err != nil {
				return false, "", tuskerError(errorInvalidTransition, "failed to retain canonical unrelated task while staging reviewed completion: "+firstActionableLine(output, err.Error()))
			}
		}
	}
	if err := removeV7WorkspaceMetadataFromLanding(workDir); err != nil {
		return false, "", err
	}
	vaultPath := filepath.Join(workDir, ".tusker")
	idx, err := loadV7Index(vaultPath)
	if err != nil {
		return false, "", err
	}
	if err := buildV7Dashboards(vaultPath, idx); err != nil {
		return false, "", err
	}
	if _, err := reconcileV7EpicManagedBlocks(vaultPath, idx); err != nil {
		return false, "", err
	}
	stagePaths := []string{".tusker/Dashboard.md", ".tusker/_generated", ".tusker/dashboards", ".tusker/work/epics"}
	stagePaths = append(stagePaths, retainIntegrationTasks...)
	if output, err := gitCombined(workDir, append([]string{"add", "--"}, stagePaths...)...); err != nil {
		return false, "", tuskerError(errorInvalidTransition, "failed to stage regenerated Tusker projections: "+firstActionableLine(output, err.Error()))
	}
	if output, err := gitCombined(workDir, "commit", "--no-edit"); err != nil {
		return false, "", tuskerError(errorInvalidTransition, "failed to complete generated-projection merge: "+firstActionableLine(output, err.Error()))
	}
	return true, "", nil
}

func v7EpicConflictOnlyTouchesManagedState(workDir, path string) bool {
	var canonical string
	for _, stage := range []string{"1", "2", "3"} {
		output, err := gitCombined(workDir, "show", ":"+stage+":"+path)
		if err != nil {
			return false
		}
		candidate := stripV7EpicManagedState(output)
		if canonical == "" {
			canonical = candidate
			continue
		}
		if candidate != canonical {
			return false
		}
	}
	return true
}

func stripV7EpicManagedState(content string) string {
	for _, heading := range []string{"## Open gates", "## Active work", "## Recently completed"} {
		content = replaceSection(content, heading, "<tusker-managed>")
	}
	lines := strings.Split(content, "\n")
	out := lines[:0]
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "updated_at:") || strings.HasPrefix(trimmed, "state_rev:") {
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

func removeV7WorkspaceMetadataFromLanding(workDir string) error {
	output, err := gitCombined(workDir, "rm", "-f", "--ignore-unmatch", "--", ".tusker/workspace.json")
	if err != nil {
		return tuskerError(errorInvalidTransition, "failed to strip workspace-local metadata from landing: "+firstActionableLine(output, err.Error()))
	}
	return nil
}

func commitV7LandingCleanup(workDir string) error {
	cmd := exec.Command("git", "-C", workDir, "diff", "--cached", "--quiet")
	if err := cmd.Run(); err == nil {
		return nil
	} else if exitErr, ok := err.(*exec.ExitError); !ok || exitErr.ExitCode() != 1 {
		return err
	}
	output, err := gitCombined(workDir, "commit", "-m", "Strip workspace-local landing metadata")
	if err != nil {
		return tuskerError(errorInvalidTransition, "failed to commit landing metadata cleanup: "+firstActionableLine(output, err.Error()))
	}
	return nil
}

func guardV7LandingTerminalTaskRewinds(workDir, baseRef string) error {
	return guardV7LandingTerminalTaskRewindsAt(workDir, baseRef, "HEAD")
}

// guardV7LandingTerminalTaskRewindsAt applies the landing monotonicity guard
// to an arbitrary frozen tree-ish. The completion reactor builds with
// write-tree/commit-tree, so HEAD is intentionally not its candidate.
func guardV7LandingTerminalTaskRewindsAt(workDir, baseRef, candidateRef string) error {
	// --name-status is intentional. A deleted terminal task is just as much a
	// rewind as changing it back to review, and --name-only used to erase that
	// distinction.  Do not return early for a new task: another changed path may
	// still be an attempted terminal rewind.
	output, err := gitCombined(workDir, "diff", "--name-status", baseRef+".."+candidateRef, "--", ".tusker/work/tasks")
	if err != nil {
		return err
	}
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			if strings.TrimSpace(line) != "" {
				return tuskerError(errorInvalidTransition, "landing terminal-task guard encountered malformed Git diff entry")
			}
			continue
		}
		status, rel := fields[0], fields[len(fields)-1]
		paths := []string{rel}
		if strings.HasPrefix(status, "R") || strings.HasPrefix(status, "C") {
			if len(fields) != 3 {
				return tuskerError(errorInvalidTransition, "landing terminal-task guard encountered malformed rename diff entry")
			}
			rel = fields[2]
			// A rename is a delete plus an add for monotonicity purposes. In
			// particular, renaming a terminal task must not smuggle its deletion
			// past a target-only diff walk.
			paths = []string{fields[1], fields[2]}
		}
		for _, rel := range paths {
			if !strings.HasSuffix(rel, ".md") {
				continue
			}
			baseData, baseOK, err := v7GitFrontmatterAtRef(workDir, baseRef, rel)
			if err != nil {
				return err
			}
			headData, headOK, err := v7GitFrontmatterAtRef(workDir, candidateRef, rel)
			if err != nil {
				return err
			}
			if !baseOK {
				// A genuinely new candidate task is harmless, but malformed/missing
				// paired entries are never treated as a reason to skip later paths.
				if headOK {
					continue
				}
				return tuskerError(errorInvalidTransition, "landing terminal-task guard cannot read changed task in either tree: "+rel)
			}
			if !headOK {
				if v7TerminalTaskStatus(stringField(baseData, "status")) {
					return tuskerError(errorInvalidTransition, "landing cannot delete terminal task: "+rel)
				}
				return tuskerError(errorInvalidTransition, "landing terminal-task guard cannot read candidate task: "+rel)
			}
			if err := guardV7TerminalTaskRewind(filepath.Join(workDir, filepath.FromSlash(rel)), "land:"+baseRef, baseData, headData); err != nil {
				return err
			}
		}
	}
	return nil
}

func runV7LandingGate(vaultPath, workDir, laneIdentity string) (bool, string) {
	evidence := runV7LandingGateEvidence(vaultPath, workDir, laneIdentity)
	return evidence.Pass, evidence.Summary
}

func runV7LandingGateEvidence(vaultPath, workDir, laneIdentity string) v7LandingGateEvidence {
	return runV7LandingGateEvidenceWithIsolation(vaultPath, workDir, laneIdentity, false)
}

// Scheduled gates execute repository-controlled commands.  They must not be
// able to read the daemon's runtime state or ask it to mint authority; hosts
// without the narrow sandbox boundary fail closed instead of treating a hash
// and an actor label as authority.
func runV7LandingGateEvidenceWithIsolation(vaultPath, workDir, laneIdentity string, isolated bool) v7LandingGateEvidence {
	started := time.Now().UTC()
	commands := backpressureCommands(vaultPath)
	if len(commands) == 0 {
		commands = []string{"go build ./...", "go vet ./...", "go test ./... -count=1"}
	}
	toolchains := landingToolchainProbe(workDir, commands)
	head, err := gitOutputTrim(workDir, "rev-parse", "HEAD")
	fingerprint := ""
	if err == nil && head != "" {
		fingerprint = v7LandingGateFingerprintFromFacts(head, laneIdentity, commands, toolchains)
	}
	evidence := v7LandingGateEvidence{
		Fingerprint: fingerprint,
		Commands:    append([]string(nil), commands...),
		Toolchains:  cloneStringStringMap(toolchains),
		StartedAt:   started.Format(time.RFC3339Nano),
	}
	// A cache is discovery only for daemon-authorized receipts.  Reusing an
	// ordinary JSON cache here would let a prior same-UID gate mint "green"
	// evidence without crossing the isolated execution boundary.
	if !isolated && fingerprint != "" && v7LandingGateCacheHit(vaultPath, fingerprint) {
		evidence.Pass = true
		evidence.FinishedAt = time.Now().UTC().Format(time.RFC3339Nano)
		for _, command := range commands {
			evidence.Outcomes = append(evidence.Outcomes, v7LandingCommandOutcome{Command: command, Result: "pass", StartedAt: evidence.StartedAt, FinishedAt: evidence.FinishedAt})
		}
		evidence.Summary = "gate cached: " + fingerprint
		return evidence
	}
	for _, command := range commands {
		commandStarted := time.Now().UTC()
		output, err := runV7LandingGateCommand(workDir, command, isolated)
		if err != nil {
			evidence.FinishedAt = time.Now().UTC().Format(time.RFC3339Nano)
			evidence.Outcomes = append(evidence.Outcomes, v7LandingCommandOutcome{Command: command, Result: "fail", StartedAt: commandStarted.Format(time.RFC3339Nano), FinishedAt: evidence.FinishedAt})
			evidence.Summary = landingFailureSummary(command, string(output), err)
			return evidence
		}
		evidence.Outcomes = append(evidence.Outcomes, v7LandingCommandOutcome{Command: command, Result: "pass", StartedAt: commandStarted.Format(time.RFC3339Nano), FinishedAt: time.Now().UTC().Format(time.RFC3339Nano)})
	}
	if fingerprint != "" {
		_ = writeV7LandingGateCache(vaultPath, fingerprint, commands)
	}
	evidence.Pass = true
	evidence.FinishedAt = time.Now().UTC().Format(time.RFC3339Nano)
	evidence.Summary = "gate passed: " + strings.Join(commands, " && ")
	return evidence
}

func runV7LandingGateCommand(workDir, command string, isolated bool) ([]byte, error) {
	if !isolated {
		cmd := exec.Command("sh", "-c", command)
		cmd.Dir = workDir
		return cmd.CombinedOutput()
	}
	sandbox, err := newV7GateSandbox(workDir, true)
	if err != nil {
		return nil, err
	}
	defer sandbox.Close()
	return sandbox.Run(context.Background(), command)
}

type v7GateSandbox struct {
	executable   string
	profile      string
	worktreePath string
	scratchPath  string
	goCachePath  string
	moduleCache  string
}

func newV7GateSandbox(workDir string, writableWorktree bool) (*v7GateSandbox, error) {
	sandboxExec, err := landingGateSandboxPath()
	if err != nil {
		return nil, tuskerError(errorInvalidTransition, "scheduled landing refusal: host cannot isolate gate execution")
	}
	scratch, err := os.MkdirTemp("", "tusker-scheduled-gate-*")
	if err != nil {
		return nil, err
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.RemoveAll(scratch)
		}
	}()
	goCache := filepath.Join(scratch, "go-build")
	if err := os.MkdirAll(goCache, 0o700); err != nil {
		return nil, err
	}
	worktreePath, err := sandboxCanonicalPath(workDir)
	if err != nil {
		return nil, fmt.Errorf("scheduled landing sandbox worktree: %w", err)
	}
	scratchPath, err := sandboxCanonicalPath(scratch)
	if err != nil {
		return nil, fmt.Errorf("scheduled landing sandbox scratch: %w", err)
	}
	goCachePath := filepath.Join(scratchPath, "go-build")
	// Do not inherit the daemon control/state environment. The profile admits
	// only the disposable worktree, a private scratch directory, immutable
	// platform toolchain roots, and read-only module caches. In particular it
	// never grants the user's Library/Application Support runtime state.
	moduleCache := filepath.Join(userHomeDir(), "go", "pkg", "mod")
	moduleCachePath, moduleCacheErr := sandboxCanonicalPath(moduleCache)
	if moduleCacheErr != nil {
		// A missing module cache is not authority to read all of $HOME. Go will
		// fail its command normally if the configured cache is genuinely needed.
		moduleCachePath = filepath.Join(scratchPath, "empty-module-cache")
		if err := os.MkdirAll(moduleCachePath, 0o700); err != nil {
			return nil, err
		}
	}
	gitDir := sandboxGitMetadataPath(workDir)
	runtimePaths := sandboxToolchainReadPaths()
	// Darwin's shell/toolchain startup probes a small read-only sysctl set and
	// opens these two kernel devices even for a no-op command. They are not
	// filesystem authority over user state, but withholding them causes
	// sandbox-exec to abort before the gate command begins.
	writePaths := `(subpath "` + sandboxProfilePath(scratchPath) + `") `
	if writableWorktree {
		writePaths += `(subpath "` + sandboxProfilePath(worktreePath) + `") `
	}
	profile := `(version 1) (deny default) (deny network*) (allow process*) (allow sysctl-read) (allow file-read-data (literal "/")) ` +
		`(allow file-read* (subpath "` + sandboxProfilePath(worktreePath) + `") (subpath "` + sandboxProfilePath(scratchPath) + `") ` + sandboxProfileSubpath(moduleCachePath) + sandboxProfileSubpath(gitDir) +
		runtimePaths + `(literal "/private/var/select/sh") (subpath "/usr") (subpath "/bin") (subpath "/private/etc") (subpath "/dev") (subpath "/System") (subpath "/Library/Developer") (subpath "/Applications/Xcode.app")) ` +
		`(allow file-write* ` + writePaths + `(literal "/dev/null") (literal "/dev/dtracehelper") (literal "/dev/tty"))`
	cleanup = false
	return &v7GateSandbox{
		executable: sandboxExec, profile: profile, worktreePath: worktreePath,
		scratchPath: scratchPath, goCachePath: goCachePath, moduleCache: moduleCachePath,
	}, nil
}

func (s *v7GateSandbox) Close() {
	if s != nil && s.scratchPath != "" {
		_ = os.RemoveAll(s.scratchPath)
	}
}

func (s *v7GateSandbox) Run(ctx context.Context, command string) ([]byte, error) {
	if s == nil || s.executable == "" {
		return nil, tuskerError(errorInvalidTransition, "scheduled landing refusal: gate sandbox is unavailable")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	cmd := exec.Command(s.executable, "-p", s.profile, "/bin/sh", "-c", command)
	cmd.Dir = s.worktreePath
	cmd.Env = []string{
		"PATH=" + sandboxToolchainPATH(),
		"HOME=" + s.scratchPath,
		"TMPDIR=" + s.scratchPath,
		"GOCACHE=" + s.goCachePath,
		"GOMODCACHE=" + s.moduleCache,
		"GOTMPDIR=" + s.scratchPath,
		"CARGO_TARGET_DIR=" + filepath.Join(s.scratchPath, "cargo-target"),
		"npm_config_cache=" + filepath.Join(s.scratchPath, "npm-cache"),
		"YARN_CACHE_FOLDER=" + filepath.Join(s.scratchPath, "yarn-cache"),
		"BUN_INSTALL_CACHE_DIR=" + filepath.Join(s.scratchPath, "bun-cache"),
		"XDG_CACHE_HOME=" + filepath.Join(s.scratchPath, "xdg-cache"),
		"CLANG_MODULE_CACHE_PATH=" + filepath.Join(s.scratchPath, "clang-module-cache"),
		"SWIFTPM_MODULECACHE_OVERRIDE=" + filepath.Join(s.scratchPath, "swift-module-cache"),
		"LANG=C",
		"LC_ALL=C",
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	output, err := runV7SandboxGateCommand(ctx, cmd)
	if ctxErr := ctx.Err(); ctxErr != nil {
		return output, ctxErr
	}
	if err != nil && len(strings.TrimSpace(string(output))) == 0 {
		return []byte("sandbox gate aborted or denied (worktree=" + s.worktreePath + "; scratch=" + s.scratchPath + ")"), err
	}
	return output, err
}

// sandbox-exec constrains file/network authority for the focused landing gate,
// but it is not a lifecycle boundary. This helper deliberately makes only the
// narrow process-group cancellation claim; scheduled full promotion uses the
// configured container/VM provider instead.
func runV7SandboxGateCommand(ctx context.Context, cmd *exec.Cmd) ([]byte, error) {
	if cmd == nil {
		return nil, fmt.Errorf("sandbox gate: nil command")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	if err := cmd.Start(); err != nil {
		return output.Bytes(), err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		return output.Bytes(), err
	case <-ctx.Done():
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		<-done
		return output.Bytes(), ctx.Err()
	}
}

func sandboxCanonicalPath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(abs)
}

func sandboxGitMetadataPath(workDir string) string {
	dir, err := gitOutputTrim(workDir, "rev-parse", "--git-common-dir")
	if err != nil || strings.TrimSpace(dir) == "" {
		return ""
	}
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(workDir, dir)
	}
	canonical, err := sandboxCanonicalPath(dir)
	if err != nil {
		return ""
	}
	return canonical
}

func sandboxToolchainReadPaths() string {
	paths := sandboxToolchainDirs()
	var out strings.Builder
	for _, path := range paths {
		out.WriteString(sandboxProfileSubpath(path))
	}
	return out.String()
}

func sandboxToolchainDirs() []string {
	paths := []string{"/usr/bin", "/bin"}
	for _, name := range []string{"go", "git", "sh"} {
		binary, err := exec.LookPath(name)
		if err != nil {
			continue
		}
		canonical, err := sandboxCanonicalPath(binary)
		if err == nil {
			paths = append(paths, filepath.Dir(canonical))
		}
		if name == "go" {
			if root, rootErr := exec.Command(binary, "env", "GOROOT").Output(); rootErr == nil {
				if canonicalRoot, canonicalErr := sandboxCanonicalPath(strings.TrimSpace(string(root))); canonicalErr == nil {
					paths = append(paths, canonicalRoot)
				}
			}
		}
	}
	paths = uniqueStrings(paths)
	sort.Strings(paths)
	return paths
}

func sandboxToolchainPATH() string {
	return strings.Join(sandboxToolchainDirs(), string(os.PathListSeparator))
}

func sandboxProfileSubpath(path string) string {
	if strings.TrimSpace(path) == "" {
		return ""
	}
	return `(subpath "` + sandboxProfilePath(path) + `") `
}

func sandboxProfilePath(path string) string {
	escaped := strings.ReplaceAll(path, `\`, `\\`)
	return strings.ReplaceAll(escaped, `"`, `\"`)
}

type v7LandingGateCacheRecord struct {
	Schema      string            `json:"schema"`
	Fingerprint string            `json:"fingerprint"`
	Commands    []string          `json:"commands"`
	PassedAt    string            `json:"passed_at"`
	Receipt     *v7LandingReceipt `json:"receipt,omitempty"`
}

type v7LandingReceiptIndexRecord struct {
	Schema              string   `json:"schema"`
	ReceiptFingerprints []string `json:"receipt_fingerprints"`
}

var landingToolchainProbe = v7LandingToolchainFingerprints
var landingGateSandboxPath = func() (string, error) {
	if runtime.GOOS != "darwin" {
		return "", fmt.Errorf("no proven scheduled-gate sandbox for %s", runtime.GOOS)
	}
	return exec.LookPath("sandbox-exec")
}

func v7LandingGateFingerprint(workDir, laneIdentity string, commands []string) string {
	head, err := gitOutputTrim(workDir, "rev-parse", "HEAD")
	if err != nil || head == "" {
		return ""
	}
	return v7LandingGateFingerprintFromFacts(head, laneIdentity, commands, landingToolchainProbe(workDir, commands))
}

func v7LandingGateFingerprintFromFacts(head, laneIdentity string, commands []string, toolchains map[string]string) string {
	parts := []string{"tusker.landing-gate/v2", head, strings.TrimSpace(laneIdentity)}
	keys := make([]string, 0, len(toolchains))
	for key := range toolchains {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		parts = append(parts, key+"="+toolchains[key])
	}
	parts = append(parts, commands...)
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return fmt.Sprintf("%x", sum)
}

func cloneStringStringMap(source map[string]string) map[string]string {
	if source == nil {
		return nil
	}
	cloned := make(map[string]string, len(source))
	for key, value := range source {
		cloned[key] = value
	}
	return cloned
}

func v7LandingBatchIdentity(tasks []v7LandTask) string {
	parts := make([]string, 0, len(tasks))
	for _, task := range tasks {
		parts = append(parts, task.ID+"@"+task.Branch+"@"+task.SourceSHA)
	}
	sort.Strings(parts)
	return "task-batch:" + strings.Join(parts, ",")
}

var landingToolchainTokens = map[string]*regexp.Regexp{
	"go":    regexp.MustCompile(`(^|[^A-Za-z0-9_.-])(go|gofmt)([^A-Za-z0-9_.-]|$)`),
	"node":  regexp.MustCompile(`(^|[^A-Za-z0-9_.-])(node|npm|npx|pnpm|yarn)([^A-Za-z0-9_.-]|$)`),
	"bun":   regexp.MustCompile(`(^|[^A-Za-z0-9_.-])bun([^A-Za-z0-9_.-]|$)`),
	"swift": regexp.MustCompile(`(^|[^A-Za-z0-9_.-])(swift|swiftc|xcodebuild)([^A-Za-z0-9_.-]|$)`),
	"rust":  regexp.MustCompile(`(^|[^A-Za-z0-9_.-])(cargo|rustc|rustup)([^A-Za-z0-9_.-]|$)`),
	"make":  regexp.MustCompile(`(^|[^A-Za-z0-9_.-])(make|gmake)([^A-Za-z0-9_.-]|$)`),
}

func v7LandingToolchainFingerprints(workDir string, commands []string) map[string]string {
	joined := v7LandingToolchainCorpus(workDir, commands)
	probes := map[string][]string{
		"go": {"go", "version"}, "node": {"node", "--version"}, "bun": {"bun", "--version"},
		"swift": {"swift", "--version"}, "rust": {"rustc", "--version", "--verbose"}, "make": {"make", "--version"},
	}
	out := map[string]string{}
	for key, pattern := range landingToolchainTokens {
		if !pattern.MatchString(joined) {
			continue
		}
		probe := probes[key]
		path, err := exec.LookPath(probe[0])
		if err != nil {
			out[key] = "missing"
			continue
		}
		resolved, _ := filepath.EvalSymlinks(path)
		if resolved == "" {
			resolved = path
		}
		version := "version-unavailable"
		if output, err := exec.Command(path, probe[1:]...).CombinedOutput(); err == nil {
			version = strings.TrimSpace(string(output))
		}
		binary := resolved
		if info, err := os.Stat(resolved); err == nil {
			binary = fmt.Sprintf("%s|%d|%d", resolved, info.Size(), info.ModTime().UnixNano())
		}
		out[key] = binary + "|" + version
	}
	// Every other executable still matters: a Python, JVM, .NET, C, or repo-local
	// script gate must never collapse to one reusable empty identity. Resolve
	// command heads without executing them. If shell syntax makes that impossible,
	// return no identity and make the proof non-reusable rather than guessing.
	for i, command := range commands {
		for j, executable := range gateCommandExecutables(workDir, command) {
			if executable == "" {
				return map[string]string{}
			}
			out[fmt.Sprintf("exec:%d:%d", i, j)] = executable
		}
	}
	return out
}

var gateShellBuiltins = map[string]bool{
	".": true, ":": true, "alias": true, "break": true, "cd": true, "command": true,
	"continue": true, "echo": true, "eval": true, "exec": true, "exit": true, "export": true,
	"false": true, "printf": true, "pwd": true, "read": true, "return": true, "set": true,
	"shift": true, "test": true, "times": true, "true": true, "type": true, "ulimit": true,
	"umask": true, "unalias": true, "unset": true, "wait": true, "[": true,
}

func gateCommandExecutables(workDir, command string) []string {
	if strings.ContainsAny(command, "`$|()") {
		return []string{""}
	}
	command = strings.ReplaceAll(strings.ReplaceAll(command, "&&", ";"), "||", ";")
	segments := strings.FieldsFunc(command, func(r rune) bool { return r == ';' })
	if len(segments) == 0 {
		return []string{""}
	}
	var resolved []string
	for _, segment := range segments {
		fields := strings.Fields(segment)
		for len(fields) > 0 && strings.Contains(fields[0], "=") && !strings.HasPrefix(fields[0], "=") {
			fields = fields[1:]
		}
		if len(fields) == 0 {
			return []string{""}
		}
		if fields[0] == "env" {
			fields = fields[1:]
			for len(fields) > 0 && (strings.HasPrefix(fields[0], "-") || strings.Contains(fields[0], "=")) {
				fields = fields[1:]
			}
			if len(fields) == 0 {
				return []string{""}
			}
		}
		name := fields[0]
		if gateShellBuiltins[name] {
			name = "/bin/sh"
		}
		path := name
		if !filepath.IsAbs(path) && !strings.ContainsRune(path, filepath.Separator) {
			var err error
			path, err = exec.LookPath(path)
			if err != nil {
				return []string{""}
			}
		} else if !filepath.IsAbs(path) {
			path = filepath.Join(workDir, path)
		}
		if resolvedPath, err := filepath.EvalSymlinks(path); err == nil && resolvedPath != "" {
			path = resolvedPath
		}
		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			return []string{""}
		}
		resolved = append(resolved, fmt.Sprintf("%s|%d|%d", path, info.Size(), info.ModTime().UnixNano()))
	}
	return resolved
}

func v7LandingToolchainCorpus(workDir string, commands []string) string {
	joined := strings.Join(commands, "\n")
	if !landingToolchainTokens["make"].MatchString(joined) {
		return joined
	}
	candidates := []string{"GNUmakefile", "makefile", "Makefile"}
	for _, fields := range commandFields(commands) {
		for i := 0; i < len(fields)-1; i++ {
			if fields[i] == "-f" || fields[i] == "--file" || fields[i] == "--makefile" {
				candidates = append([]string{fields[i+1]}, candidates...)
			}
		}
	}
	seen := map[string]bool{}
	for _, candidate := range candidates {
		path := candidate
		if !filepath.IsAbs(path) {
			path = filepath.Join(workDir, path)
		}
		path = filepath.Clean(path)
		if seen[path] {
			continue
		}
		seen[path] = true
		raw, err := os.ReadFile(path)
		if err == nil {
			joined += "\n" + string(raw)
			break
		}
	}
	return joined
}

func commandFields(commands []string) [][]string {
	out := make([][]string, 0, len(commands))
	for _, command := range commands {
		out = append(out, strings.Fields(command))
	}
	return out
}

func v7LandingGateCachePath(vaultPath, fingerprint string) string {
	project := fallback(v7ProjectID(vaultPath), "project")
	return filepath.Join(DefaultStateRoot(), "landing-cache", sanitizeWorkspaceKey(project), fingerprint+".json")
}

func v7LandingGateCacheHit(vaultPath, fingerprint string) bool {
	raw, err := os.ReadFile(v7LandingGateCachePath(vaultPath, fingerprint))
	if err != nil {
		return false
	}
	var record v7LandingGateCacheRecord
	if json.Unmarshal(raw, &record) != nil || record.Fingerprint != fingerprint {
		return false
	}
	return record.Schema == v7LandingGateCacheSchemaV1 || record.Schema == v7LandingGateCacheSchemaV2
}

func writeV7LandingGateCache(vaultPath, fingerprint string, commands []string) error {
	path := v7LandingGateCachePath(vaultPath, fingerprint)
	if err := ensureDir(filepath.Dir(path)); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(v7LandingGateCacheRecord{Schema: v7LandingGateCacheSchemaV1, Fingerprint: fingerprint, Commands: commands, PassedAt: time.Now().UTC().Format(time.RFC3339)}, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp-" + newRecordID()
	if err := os.WriteFile(tmp, append(raw, '\n'), 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func writeV7LandingReceipt(vaultPath string, receipt v7LandingReceipt) error {
	if receipt.Schema != v7LandingReceiptSchema ||
		receipt.Fingerprint == "" ||
		receipt.GateFingerprint != v7LandingGateFingerprintFromFacts(receipt.BatchHeadSHA, receipt.LaneIdentity, receipt.Commands, receipt.Toolchains) ||
		receipt.Fingerprint != v7LandingReceiptFingerprint(receipt) {
		return tuskerError(errorInvalidTransition, "landing receipt refusal: fingerprints do not bind the batch facts")
	}
	path := v7LandingGateCachePath(vaultPath, receipt.Fingerprint)
	if err := ensureDir(filepath.Dir(path)); err != nil {
		return err
	}
	record := v7LandingGateCacheRecord{
		Schema: v7LandingGateCacheSchemaV2, Fingerprint: receipt.Fingerprint,
		Commands: append([]string(nil), receipt.Commands...),
		PassedAt: receipt.ReceiptIssuedAt, Receipt: &receipt,
	}
	raw, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp-" + newRecordID()
	if err := os.WriteFile(tmp, append(raw, '\n'), 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	for _, task := range receipt.Tasks {
		if err := writeV7LandingReceiptIndex(vaultPath, receipt, task); err != nil {
			return err
		}
	}
	return nil
}

func writeV7LandingReceiptIndex(vaultPath string, receipt v7LandingReceipt, task v7LandingReceiptTask) error {
	path := v7LandingReceiptIndexPath(vaultPath, receipt.Target, task.Task, task.SourceSHA)
	if err := ensureDir(filepath.Dir(path)); err != nil {
		return err
	}
	fingerprints := []string{}
	if existing, err := os.ReadFile(path); err == nil {
		var record v7LandingReceiptIndexRecord
		if json.Unmarshal(existing, &record) == nil && record.Schema == v7LandingReceiptIndexSchema {
			for _, fingerprint := range record.ReceiptFingerprints {
				if isV7LandingFingerprint(fingerprint) && !containsString(fingerprints, fingerprint) {
					fingerprints = append(fingerprints, fingerprint)
				}
			}
		}
	}
	if !containsString(fingerprints, receipt.Fingerprint) {
		fingerprints = append(fingerprints, receipt.Fingerprint)
	}
	raw, err := json.MarshalIndent(v7LandingReceiptIndexRecord{
		Schema: v7LandingReceiptIndexSchema, ReceiptFingerprints: fingerprints,
	}, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp-" + newRecordID()
	if err := os.WriteFile(tmp, append(raw, '\n'), 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func v7LandingReceiptIndexPath(vaultPath, target, taskID, sourceSHA string) string {
	project := fallback(v7ProjectID(vaultPath), "project")
	sum := sha256.Sum256([]byte(strings.Join([]string{
		"tusker.landing-receipt-index/v2", project, target, taskID, sourceSHA,
	}, "\x00")))
	return filepath.Join(DefaultStateRoot(), "landing-cache", sanitizeWorkspaceKey(project), "by-task", fmt.Sprintf("%x.json", sum))
}

func indexedV7LandingReceipts(vaultPath, target, taskID, sourceSHA string) []v7LandingReceipt {
	path := v7LandingReceiptIndexPath(vaultPath, target, taskID, sourceSHA)
	raw, err := os.ReadFile(path)
	var receipts []v7LandingReceipt
	if err == nil {
		var record v7LandingReceiptIndexRecord
		if json.Unmarshal(raw, &record) == nil && record.Schema == v7LandingReceiptIndexSchema {
			for index := len(record.ReceiptFingerprints) - 1; index >= 0; index-- {
				if receipt, ok := loadV7LandingReceipt(vaultPath, record.ReceiptFingerprints[index]); ok {
					receipts = append(receipts, receipt)
				}
			}
		}
	}
	// V1 indexes were actor-keyed. A fresh daemon cannot know the historical
	// actor/session, so use the receipt cache only as discovery and verify every
	// candidate below. This preserves recoverability without treating cache JSON
	// as authority.
	if len(receipts) == 0 {
		root := filepath.Dir(v7LandingGateCachePath(vaultPath, "placeholder"))
		entries, _ := os.ReadDir(root)
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
				continue
			}
			fingerprint := strings.TrimSuffix(entry.Name(), ".json")
			receipt, ok := loadV7LandingReceipt(vaultPath, fingerprint)
			if !ok || receipt.Target != target {
				continue
			}
			for _, proof := range receipt.Tasks {
				if proof.Task == taskID && proof.SourceSHA == sourceSHA {
					receipts = append(receipts, receipt)
					break
				}
			}
		}
	}
	return receipts
}

func v7LandingReceiptFingerprint(receipt v7LandingReceipt) string {
	parts := []string{
		v7LandingReceiptSchema,
		receipt.GateFingerprint,
		receipt.LaneIdentity,
		receipt.Target,
		receipt.Actor,
		receipt.ControlAuthority,
		receipt.BatchBaseSHA,
		receipt.BatchHeadSHA,
		receipt.BatchTreeSHA,
		receipt.Outcome,
		receipt.GateSummary,
		receipt.ReceiptIssuedAt,
	}
	parts = append(parts, receipt.BatchSegment...)
	for _, task := range receipt.Tasks {
		parts = append(parts,
			task.Task,
			task.Branch,
			task.SourceSHA,
			task.SourceProvenance,
			task.BaseSHA,
			task.MergeCommit,
		)
	}
	parts = append(parts, receipt.Commands...)
	keys := make([]string, 0, len(receipt.Toolchains))
	for key := range receipt.Toolchains {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		parts = append(parts, key+"="+receipt.Toolchains[key])
	}
	parts = append(parts, receipt.GateStartedAt, receipt.GateFinishedAt)
	for _, outcome := range receipt.CommandOutcomes {
		parts = append(parts, outcome.Command, outcome.Result, outcome.StartedAt, outcome.FinishedAt)
	}
	parts = append(parts,
		receipt.ProjectID, receipt.RepoIdentity, receipt.DepartureID, receipt.PolicyID,
		receipt.ScheduledWindow, receipt.DaemonSessionID, receipt.DaemonHost,
		receipt.DaemonProcess, receipt.AuthorityID, fmt.Sprintf("%d", receipt.AuthorityGen),
	)
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return fmt.Sprintf("%x", sum)
}

func loadV7LandingReceipt(vaultPath, fingerprint string) (v7LandingReceipt, bool) {
	if !isV7LandingFingerprint(fingerprint) {
		return v7LandingReceipt{}, false
	}
	raw, err := os.ReadFile(v7LandingGateCachePath(vaultPath, fingerprint))
	if err != nil {
		return v7LandingReceipt{}, false
	}
	var record v7LandingGateCacheRecord
	if json.Unmarshal(raw, &record) != nil ||
		record.Schema != v7LandingGateCacheSchemaV2 ||
		record.Fingerprint != fingerprint ||
		record.Receipt == nil ||
		record.Receipt.Fingerprint != fingerprint {
		return v7LandingReceipt{}, false
	}
	return *record.Receipt, true
}

func isV7LandingFingerprint(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	for _, r := range value {
		if !strings.ContainsRune("0123456789abcdef", r) {
			return false
		}
	}
	return true
}

func recoverV7LandingAuditFromReceipt(vaultPath, repoRoot, integrationBranch string, task v7LandTask, trustedStore *RuntimeStore) (v7LandingAuditEntry, bool) {
	for _, receipt := range indexedV7LandingReceipts(vaultPath, integrationBranch, task.ID, task.SourceSHA) {
		if receipt.Target != integrationBranch {
			continue
		}
		proof, ok := verifiedV7LandingReceiptTaskWithStore(repoRoot, integrationBranch, receipt, task.ID, trustedStore)
		if !ok ||
			!strings.EqualFold(proof.SourceSHA, task.SourceSHA) ||
			proof.Branch != task.Branch {
			continue
		}
		return v7LandingAuditEntry{
			Task: proof.Task, Branch: proof.Branch, SourceSHA: proof.SourceSHA,
			SourceProvenance: proof.SourceProvenance, Target: receipt.Target,
			BaseSHA: proof.BaseSHA, MergeCommit: proof.MergeCommit,
			GateResult: "pass", GateSummary: receipt.GateSummary,
			GateFingerprint: receipt.GateFingerprint, ReceiptFingerprint: receipt.Fingerprint,
			ControlAuthority: receipt.ControlAuthority,
			Commit:           receipt.BatchHeadSHA, Tree: receipt.BatchTreeSHA,
			Actor: receipt.Actor, Timestamp: time.Now().UTC().Format(time.RFC3339),
		}, true
	}
	return v7LandingAuditEntry{}, false
}

func verifiedV7LandingReceiptTask(repoRoot, integrationBranch string, receipt v7LandingReceipt, taskID string) (v7LandingReceiptTask, bool) {
	return verifiedV7LandingReceiptTaskWithStore(repoRoot, integrationBranch, receipt, taskID, nil)
}

func verifiedV7LandingReceiptTaskWithStore(repoRoot, integrationBranch string, receipt v7LandingReceipt, taskID string, trustedStore *RuntimeStore) (v7LandingReceiptTask, bool) {
	if receipt.Schema != v7LandingReceiptSchema ||
		receipt.Outcome != "pass" ||
		receipt.Target != integrationBranch ||
		!trustedV7LandingControlAuthority(receipt.ControlAuthority, receipt.Actor) ||
		receipt.GateFingerprint != v7LandingGateFingerprintFromFacts(receipt.BatchHeadSHA, receipt.LaneIdentity, receipt.Commands, receipt.Toolchains) ||
		receipt.Fingerprint != v7LandingReceiptFingerprint(receipt) ||
		len(receipt.Commands) == 0 ||
		len(receipt.Tasks) == 0 ||
		len(receipt.BatchSegment) < len(receipt.Tasks) ||
		len(receipt.BatchSegment) > len(receipt.Tasks)+1 {
		return v7LandingReceiptTask{}, false
	}
	if !verifyV7LandingReceiptAuthorityWithStore(repoRoot, receipt, trustedStore) {
		return v7LandingReceiptTask{}, false
	}
	if _, err := time.Parse(time.RFC3339Nano, receipt.ReceiptIssuedAt); err != nil {
		return v7LandingReceiptTask{}, false
	}
	if !validV7LandingReceiptOutcomes(receipt) {
		return v7LandingReceiptTask{}, false
	}
	base, baseOK := gitRevParse(repoRoot, receipt.BatchBaseSHA+"^{commit}")
	head, headOK := gitRevParse(repoRoot, receipt.BatchHeadSHA+"^{commit}")
	tree, treeOK := gitRevParse(repoRoot, receipt.BatchHeadSHA+"^{tree}")
	integration, integrationOK := gitRevParse(repoRoot, integrationBranch+"^{commit}")
	if !baseOK || !headOK || !treeOK || !integrationOK ||
		!strings.EqualFold(base, receipt.BatchBaseSHA) ||
		!strings.EqualFold(head, receipt.BatchHeadSHA) ||
		!strings.EqualFold(tree, receipt.BatchTreeSHA) ||
		!gitMergeBaseAncestor(repoRoot, head, integration) {
		return v7LandingReceiptTask{}, false
	}
	receiptTasks := make([]v7LandTask, 0, len(receipt.Tasks))
	proofsByMerge := make(map[string]v7LandingReceiptTask, len(receipt.Tasks))
	var wanted v7LandingReceiptTask
	for _, proof := range receipt.Tasks {
		if proof.Task == "" || proof.Branch != v7TaskBranchName(proof.Task) ||
			!trustedV7LandingSourceProvenance(proof.SourceProvenance) ||
			proof.SourceSHA == "" || proof.BaseSHA == "" || proof.MergeCommit == "" ||
			proofsByMerge[proof.MergeCommit].Task != "" {
			return v7LandingReceiptTask{}, false
		}
		receiptTasks = append(receiptTasks, v7LandTask{ID: proof.Task, Branch: proof.Branch, SourceSHA: proof.SourceSHA})
		proofsByMerge[proof.MergeCommit] = proof
		if proof.Task == taskID {
			wanted = proof
		}
	}
	if wanted.Task == "" || receipt.LaneIdentity != v7LandingBatchIdentity(receiptTasks) {
		return v7LandingReceiptTask{}, false
	}
	current := receipt.BatchBaseSHA
	seenTasks := map[string]bool{}
	nonTaskCommits := 0
	for _, commit := range receipt.BatchSegment {
		resolved, ok := gitRevParse(repoRoot, commit+"^{commit}")
		parents, parentsOK := v7LandingCommitParents(repoRoot, commit)
		if !ok || !parentsOK || !strings.EqualFold(resolved, commit) ||
			len(parents) == 0 || !strings.EqualFold(parents[0], current) {
			return v7LandingReceiptTask{}, false
		}
		if proof, taskMerge := proofsByMerge[commit]; taskMerge {
			if len(parents) != 2 ||
				!strings.EqualFold(proof.BaseSHA, current) ||
				!strings.EqualFold(parents[1], proof.SourceSHA) ||
				seenTasks[proof.Task] {
				return v7LandingReceiptTask{}, false
			}
			source, sourceOK := gitRevParse(repoRoot, proof.SourceSHA+"^{commit}")
			if !sourceOK || !strings.EqualFold(source, proof.SourceSHA) {
				return v7LandingReceiptTask{}, false
			}
			seenTasks[proof.Task] = true
		} else {
			if len(parents) != 1 {
				return v7LandingReceiptTask{}, false
			}
			nonTaskCommits++
		}
		current = commit
	}
	if !strings.EqualFold(current, receipt.BatchHeadSHA) ||
		len(seenTasks) != len(receipt.Tasks) ||
		nonTaskCommits > 1 {
		return v7LandingReceiptTask{}, false
	}
	return wanted, true
}

func validV7LandingReceiptOutcomes(receipt v7LandingReceipt) bool {
	started, startErr := time.Parse(time.RFC3339Nano, receipt.GateStartedAt)
	finished, finishErr := time.Parse(time.RFC3339Nano, receipt.GateFinishedAt)
	if startErr != nil || finishErr != nil || finished.Before(started) || len(receipt.CommandOutcomes) != len(receipt.Commands) {
		return false
	}
	previous := started
	for index, outcome := range receipt.CommandOutcomes {
		commandStarted, commandStartErr := time.Parse(time.RFC3339Nano, outcome.StartedAt)
		commandFinished, commandFinishErr := time.Parse(time.RFC3339Nano, outcome.FinishedAt)
		if commandStartErr != nil || commandFinishErr != nil || outcome.Command != receipt.Commands[index] || outcome.Result != "pass" || commandStarted.Before(previous) || commandFinished.Before(commandStarted) || commandFinished.After(finished) {
			return false
		}
		previous = commandFinished
	}
	return true
}

func v7LandingCommitParents(repoRoot, commit string) ([]string, bool) {
	output, err := gitOutputTrim(repoRoot, "rev-list", "--parents", "-n", "1", commit)
	if err != nil {
		return nil, false
	}
	fields := strings.Fields(output)
	if len(fields) < 2 || !strings.EqualFold(fields[0], commit) {
		return nil, false
	}
	return fields[1:], true
}

func landingFailureSummary(command, output string, err error) string {
	line := firstActionableLine(output, err.Error())
	if line == "" {
		line = "command failed"
	}
	return limitLandingSummary("gate failed: "+command+": "+line, 500)
}

func limitLandingSummary(value string, max int) string {
	value = strings.TrimSpace(value)
	if max <= 0 || len(value) <= max {
		return value
	}
	if max <= 3 {
		return value[:max]
	}
	return strings.TrimSpace(value[:max-3]) + "..."
}

// firstActionableLine returns the most useful line from command output for a
// failure summary. It prefers a line that clearly names a conflict or error and
// otherwise skips progress chatter such as `Auto-merging ...` so the actionable
// conflict/error line (with its path) survives in the summary (A6).
func firstActionableLine(output, fallbackLine string) string {
	lines := strings.Split(output, "\n")
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line != "" && isActionableFailureLine(line) {
			return line
		}
	}
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line != "" && !isMergeProgressChatter(line) {
			return line
		}
	}
	for _, raw := range lines {
		if line := strings.TrimSpace(raw); line != "" {
			return line
		}
	}
	return strings.TrimSpace(fallbackLine)
}

func isActionableFailureLine(line string) bool {
	lower := strings.ToLower(line)
	switch {
	case strings.HasPrefix(line, "CONFLICT"),
		strings.Contains(line, "Merge conflict in "),
		strings.Contains(line, "needs merge"),
		strings.Contains(line, "would be overwritten"),
		strings.HasPrefix(lower, "error:"),
		strings.HasPrefix(lower, "fatal:"),
		strings.HasPrefix(lower, "panic:"),
		strings.HasPrefix(line, "--- FAIL"),
		strings.HasPrefix(line, "FAIL\t"),
		strings.HasPrefix(line, "FAIL "):
		return true
	}
	return false
}

func isMergeProgressChatter(line string) bool {
	for _, prefix := range []string{
		"Auto-merging",
		"Removing ",
		"Adding ",
		"Merge made by",
		"Fast-forward",
		"Already up to date",
		"Performing inexact rename detection",
		"Updating ",
		"# ",
	} {
		if strings.HasPrefix(line, prefix) {
			return true
		}
	}
	return false
}

func kickV7LandingTaskToRework(vaultPath, taskID, summary, actor string) error {
	statusArgs := Args{
		"vault": vaultPath, "quiet": "true", "local": "true",
		"id": taskID, "status": "rework", "by": actor,
		"reason": summary,
	}
	var statusErr error
	if internal, internalErr := newV7InternalActor(actor); internalErr == nil {
		statusErr = statusV7CmdWithInternalActor(statusArgs, &internal)
	} else {
		statusErr = statusV7Cmd(statusArgs)
	}
	if statusErr != nil {
		return statusErr
	}
	note, err := resolveV7Note(vaultPath, taskID, "task")
	if err != nil {
		return err
	}
	data, body, err := parseFrontmatterMustRead(note.AbsolutePath)
	if err != nil {
		return err
	}
	baseRev := stringField(data, "state_rev")
	data["next_owner"] = "agent"
	data["next_source"] = "task"
	data["next_ref"] = taskID
	data["next_action"] = "Fix landing gate failure: " + summary
	data["updated_at"] = time.Now().UTC().Format(time.RFC3339)
	data["updated_by"] = actor
	_, err = saveV7DocumentCAS(note.AbsolutePath, data, body, v7FrontmatterOrder["task"], baseRev)
	return err
}

func landV7WaveToMainIfReady(vaultPath, waveID string, args Args, summary *v7LandSummary) error {
	idx, err := loadV7Index(vaultPath)
	if err != nil {
		return err
	}
	wave, ok := idx.Waves[waveID]
	if !ok {
		return tuskerError(errorNotFound, "V7 wave not found: "+waveID)
	}
	allowed, err := scheduledPromotionAllowsDefaultAdvance(vaultPath)
	if err != nil {
		return err
	}
	if !allowed {
		summary.MainNotes = append(summary.MainNotes, "main: scheduled promotion policy keeps "+waveID+" staged")
		return nil
	}
	open := 0
	for _, member := range normalizeList(wave.Data["members"]) {
		status, found, err := v7WaveIntegrationMemberStatus(vaultPath, wave, member)
		if err != nil {
			return err
		}
		if !found || status != "done" {
			open++
		}
	}
	if open > 0 {
		summary.MainNotes = append(summary.MainNotes, fmt.Sprintf("main: waiting on %d open task%s in %s", open, plural(open), waveID))
		return nil
	}
	return landV7WaveToMain(vaultPath, waveID, args, summary)
}

func landV7WaveToMain(vaultPath, waveID string, args Args, summary *v7LandSummary) error {
	idx, err := loadV7Index(vaultPath)
	if err != nil {
		return err
	}
	wave, ok := idx.Waves[waveID]
	if !ok {
		return tuskerError(errorNotFound, "V7 wave not found: "+waveID)
	}
	if err := refuseV7TerminalWaveLanding(waveID, wave); err != nil {
		return err
	}
	allowed, err := scheduledPromotionAllowsDefaultAdvance(vaultPath)
	if err != nil {
		return err
	}
	if !allowed {
		return tuskerError(errorInvalidTransition, "scheduled promotion policy refuses default-branch advance; configured departures own main promotion")
	}
	for _, member := range normalizeList(wave.Data["members"]) {
		status, found, err := v7WaveIntegrationMemberStatus(vaultPath, wave, member)
		if err != nil {
			return err
		}
		if !found || status != "done" {
			return tuskerError(errorInvalidTransition, waveID+" cannot land to main until every member task is done")
		}
	}
	repoRoot := v7RepoRoot(vaultPath)
	if !v7GitRepo(repoRoot) {
		return tuskerError(errorInvalidTransition, "wave landing requires a Git repository", withPath(repoRoot))
	}
	integrationBranch := v7WaveIntegrationBranch(wave)
	defaultBranch := v7DefaultBranch(vaultPath)
	integrationExists := gitRefExists(repoRoot, "refs/heads/"+integrationBranch)
	if !integrationExists {
		summary.MainNotes = append(summary.MainNotes, "main: "+waveID+" has no integration branch to land")
		return nil
	}
	integrationRev, err := gitOutputTrim(repoRoot, "rev-parse", integrationBranch)
	if err != nil {
		return err
	}
	if gitMergeBaseAncestor(repoRoot, integrationBranch, defaultBranch) {
		mainRev, _ := gitOutputTrim(repoRoot, "rev-parse", defaultBranch)
		actor := landV7Actor(args)
		if err := appendV7WaveLandingAudit(vaultPath, waveID, []v7LandingAuditEntry{{
			Task: "wave", Branch: integrationBranch, Target: defaultBranch,
			GateResult: "pass", GateSummary: "already landed; cleaned up integration branch",
			Commit: mainRev, Actor: actor, Timestamp: time.Now().UTC().Format(time.RFC3339),
		}}, actor); err != nil {
			return err
		}
		if err := deleteGitBranch(repoRoot, integrationBranch); err != nil {
			return err
		}
		summary.MainNotes = append(summary.MainNotes, "main: "+waveID+" already landed at "+shortCommit(mainRev)+"; cleaned up integration branch")
		return nil
	}
	if !gitMergeBaseAncestor(repoRoot, defaultBranch, integrationBranch) {
		return tuskerError(errorInvalidTransition, defaultBranch+" is not an ancestor of "+integrationBranch+"; rebase or merge main before wave landing")
	}
	pass, summaryText := runV7LandingGateOnRef(vaultPath, repoRoot, integrationBranch)
	if !pass {
		return tuskerError(errorInvalidTransition, waveID+" integration branch is red: "+summaryText)
	}
	if err := syncV7WaveControlStateToIntegration(vaultPath, wave, integrationBranch); err != nil {
		return err
	}
	integrationRev, err = gitOutputTrim(repoRoot, "rev-parse", integrationBranch)
	if err != nil {
		return err
	}
	mainRev, err := gitOutputTrim(repoRoot, "rev-parse", defaultBranch)
	if err != nil {
		return err
	}
	title := strings.TrimSpace(stringField(wave.Data, "title"))
	message := fmt.Sprintf("Wave %s: %s", waveID, title)
	mergeCommit, err := gitOutputTrim(repoRoot, "commit-tree", integrationBranch+"^{tree}", "-p", mainRev, "-p", integrationRev, "-m", message)
	if err != nil {
		return err
	}
	preparation, err := prepareV7WaveMembersForDefaultAdvance(repoRoot, vaultPath, defaultBranch, wave)
	if err != nil {
		return err
	}
	if err := advanceV7DefaultBranch(repoRoot, defaultBranch, mergeCommit, mainRev); err != nil {
		return errors.Join(err, preparation.finishAfterRefAttempt(repoRoot, defaultBranch, mainRev, mergeCommit))
	}
	preparation.commit()
	actor := landV7Actor(args)
	if err := appendV7WaveLandingAudit(vaultPath, waveID, []v7LandingAuditEntry{{
		Task: "wave", Branch: integrationBranch, Target: defaultBranch,
		GateResult: "pass", GateSummary: summaryText, Commit: mergeCommit,
		Actor: actor, Timestamp: time.Now().UTC().Format(time.RFC3339),
	}}, actor); err != nil {
		return err
	}
	if err := deleteGitBranch(repoRoot, integrationBranch); err != nil {
		return err
	}
	summary.MainNotes = append(summary.MainNotes, "main: moved to "+shortCommit(mergeCommit)+" ("+message+")")
	return nil
}

type v7PreparedWaveMemberState struct {
	Exists bool
	Mode   os.FileMode
	Bytes  []byte
}

type v7PreparedWaveMemberPath struct {
	Absolute         string
	WorkDir          string
	Relative         string
	Before           v7PreparedWaveMemberState
	PreparedIndex    v7PreparedWaveMemberState
	PreparedWorktree v7PreparedWaveMemberState
}

type v7WaveMemberPreparation struct {
	paths    []v7PreparedWaveMemberPath
	finished bool
}

// prepareV7WaveMembersForDefaultAdvance temporarily restores the checked-out
// index versions of wave control documents so Git can fast-forward the default
// branch. The returned transaction must either be committed after the ref
// moves or restored on a pre-ref refusal. Restoration is compare-and-swap:
// operator edits made after preparation are never overwritten.
func prepareV7WaveMembersForDefaultAdvance(repoRoot, vaultPath, defaultBranch string, wave Note) (*v7WaveMemberPreparation, error) {
	relVault, err := filepath.Rel(repoRoot, vaultPath)
	if err != nil || filepath.IsAbs(relVault) || strings.HasPrefix(filepath.Clean(relVault), "..") {
		return nil, tuskerError(errorInvalidTransition, "cannot prepare wave members for default-branch advance: invalid vault path "+vaultPath)
	}
	paths := make([]string, 0, len(normalizeList(wave.Data["members"])))
	for _, member := range normalizeList(wave.Data["members"]) {
		paths = append(paths, filepath.ToSlash(filepath.Join(relVault, "work", "tasks", member+".md")))
	}
	paths = append(paths, filepath.ToSlash(filepath.Join(relVault, "work", "waves", stringField(wave.Data, "id")+".md")))
	preparation := &v7WaveMemberPreparation{}
	for _, wt := range v7DefaultBranchCheckouts(repoRoot, defaultBranch) {
		firstPreparedPath := len(preparation.paths)
		tracked := make([]string, 0, len(paths))
		for _, path := range paths {
			preparedIndex, ok, stateErr := v7PreparedIndexState(wt.Path, path)
			if stateErr != nil {
				return nil, errors.Join(stateErr, preparation.restore())
			}
			if !ok {
				continue
			}
			before, stateErr := v7PreparedWorktreeState(filepath.Join(wt.Path, filepath.FromSlash(path)))
			if stateErr != nil {
				return nil, errors.Join(stateErr, preparation.restore())
			}
			preparation.paths = append(preparation.paths, v7PreparedWaveMemberPath{
				Absolute: filepath.Join(wt.Path, filepath.FromSlash(path)),
				WorkDir:  wt.Path,
				Relative: path,
				Before:   before,
				// Until checkout succeeds, the worktree still contains Before.
				// Capturing this separately from the raw index blob matters for
				// checkout filters and platform-specific working-tree modes.
				PreparedIndex:    preparedIndex,
				PreparedWorktree: before,
			})
			tracked = append(tracked, path)
		}
		if len(tracked) == 0 {
			continue
		}
		args := append([]string{"checkout", "--"}, tracked...)
		if output, err := gitCombined(wt.Path, args...); err != nil {
			prepareErr := tuskerError(errorInvalidTransition, "failed to reset integrated wave task projections before default-branch advance: "+firstActionableLine(output, err.Error()), withPath(wt.Path))
			captureErr := preparation.capturePreparedWorktree(firstPreparedPath)
			return nil, errors.Join(prepareErr, captureErr, preparation.restore())
		}
		if err := preparation.capturePreparedWorktree(firstPreparedPath); err != nil {
			return nil, errors.Join(err, preparation.restore())
		}
	}
	return preparation, nil
}

func (preparation *v7WaveMemberPreparation) capturePreparedWorktree(first int) error {
	if preparation == nil {
		return nil
	}
	var captureErrors []error
	for index := first; index < len(preparation.paths); index++ {
		state, err := v7PreparedWorktreeState(preparation.paths[index].Absolute)
		if err != nil {
			captureErrors = append(captureErrors, err)
			continue
		}
		preparation.paths[index].PreparedWorktree = state
	}
	return errors.Join(captureErrors...)
}

func v7PreparedIndexState(workDir, relativePath string) (v7PreparedWaveMemberState, bool, error) {
	raw, err := exec.Command("git", "-C", workDir, "ls-files", "--stage", "-z", "--", relativePath).Output()
	if err != nil {
		return v7PreparedWaveMemberState{}, false, err
	}
	if len(raw) == 0 {
		return v7PreparedWaveMemberState{}, false, nil
	}
	entry := strings.SplitN(strings.TrimSuffix(string(raw), "\x00"), "\t", 2)
	fields := strings.Fields(entry[0])
	if len(entry) != 2 || len(fields) != 3 || fields[2] != "0" {
		return v7PreparedWaveMemberState{}, false, tuskerError(errorInvalidTransition, "cannot prepare unmerged wave control document: "+relativePath, withPath(workDir))
	}
	mode := os.FileMode(0o644)
	switch fields[0] {
	case "100644":
	case "100755":
		mode = 0o755
	default:
		return v7PreparedWaveMemberState{}, false, tuskerError(errorInvalidTransition, "cannot prepare non-regular wave control document: "+relativePath, withPath(workDir))
	}
	content, err := exec.Command("git", "-C", workDir, "cat-file", "blob", fields[1]).Output()
	if err != nil {
		return v7PreparedWaveMemberState{}, false, err
	}
	return v7PreparedWaveMemberState{Exists: true, Mode: mode, Bytes: content}, true, nil
}

func v7PreparedWorktreeState(absolutePath string) (v7PreparedWaveMemberState, error) {
	info, err := os.Lstat(absolutePath)
	if errors.Is(err, os.ErrNotExist) {
		return v7PreparedWaveMemberState{}, nil
	}
	if err != nil {
		return v7PreparedWaveMemberState{}, err
	}
	if !info.Mode().IsRegular() {
		return v7PreparedWaveMemberState{}, tuskerError(errorInvalidTransition, "cannot prepare non-regular wave control document", withPath(absolutePath))
	}
	content, err := os.ReadFile(absolutePath)
	if err != nil {
		return v7PreparedWaveMemberState{}, err
	}
	return v7PreparedWaveMemberState{Exists: true, Mode: info.Mode().Perm(), Bytes: content}, nil
}

func sameV7PreparedWaveMemberBytes(left, right v7PreparedWaveMemberState) bool {
	return left.Exists == right.Exists && (!left.Exists || bytes.Equal(left.Bytes, right.Bytes))
}

func sameV7PreparedWaveMemberIndexState(left, right v7PreparedWaveMemberState) bool {
	return sameV7PreparedWaveMemberBytes(left, right) &&
		(!left.Exists || left.Mode.Perm() == right.Mode.Perm())
}

func sameV7PreparedWaveMemberExactState(left, right v7PreparedWaveMemberState) bool {
	return sameV7PreparedWaveMemberIndexState(left, right)
}

func (preparation *v7WaveMemberPreparation) restore() error {
	if preparation == nil || preparation.finished {
		return nil
	}
	preparation.finished = true
	var restoreErrors []error
	for _, path := range preparation.paths {
		indexState, tracked, err := v7PreparedIndexState(path.WorkDir, path.Relative)
		if err != nil {
			restoreErrors = append(restoreErrors, err)
			continue
		}
		if !tracked {
			indexState = v7PreparedWaveMemberState{}
		}
		if !sameV7PreparedWaveMemberIndexState(indexState, path.PreparedIndex) {
			// A checkout or index update owns the path now. Its bytes must win
			// even if the branch ref moved to an unexpected third value.
			restoreErrors = append(restoreErrors, fmt.Errorf("refused to restore %s: index changed after default-advance preparation", path.Relative))
			continue
		}
		current, err := v7PreparedWorktreeState(path.Absolute)
		if err != nil {
			restoreErrors = append(restoreErrors, err)
			continue
		}
		if !sameV7PreparedWaveMemberExactState(current, path.PreparedWorktree) {
			// A concurrent operator edit owns the path now. It is safer to leave
			// that edit intact than to force our preimage over it.
			restoreErrors = append(restoreErrors, fmt.Errorf("refused to restore %s: worktree changed after default-advance preparation", path.Relative))
			continue
		}
		if !path.Before.Exists {
			if err := os.Remove(path.Absolute); err != nil && !errors.Is(err, os.ErrNotExist) {
				restoreErrors = append(restoreErrors, err)
				continue
			}
			invalidateCachedNote(path.Absolute)
			restored, err := v7PreparedWorktreeState(path.Absolute)
			if err != nil {
				restoreErrors = append(restoreErrors, err)
				continue
			}
			if !sameV7PreparedWaveMemberExactState(restored, path.Before) {
				restoreErrors = append(restoreErrors, fmt.Errorf("restored wave control document does not match its absent preimage: %s", path.Relative))
			}
			continue
		}
		if err := atomicReplaceV7Document(path.Absolute, string(path.Before.Bytes)); err != nil {
			restoreErrors = append(restoreErrors, err)
			continue
		}
		if err := os.Chmod(path.Absolute, path.Before.Mode.Perm()); err != nil {
			restoreErrors = append(restoreErrors, err)
			continue
		}
		restored, err := v7PreparedWorktreeState(path.Absolute)
		if err != nil {
			restoreErrors = append(restoreErrors, err)
			continue
		}
		if !sameV7PreparedWaveMemberExactState(restored, path.Before) {
			restoreErrors = append(restoreErrors, fmt.Errorf("restored wave control document does not match its byte-and-mode preimage: %s", path.Relative))
		}
	}
	return errors.Join(restoreErrors...)
}

func (preparation *v7WaveMemberPreparation) finishAfterRefAttempt(repoRoot, defaultBranch, expectedRev, intendedRev string) error {
	if preparation == nil || preparation.finished {
		return nil
	}
	current, err := gitOutputTrim(repoRoot, "rev-parse", "refs/heads/"+defaultBranch+"^{commit}")
	if err != nil {
		restoreErr := preparation.restore()
		return errors.Join(fmt.Errorf("cannot inspect default ref while restoring wave control documents: %w", err), restoreErr)
	}
	switch {
	case strings.EqualFold(strings.TrimSpace(current), strings.TrimSpace(intendedRev)):
		preparation.commit()
		return nil
	case strings.EqualFold(strings.TrimSpace(current), strings.TrimSpace(expectedRev)):
		return preparation.restore()
	default:
		// An external third-value ref race is still an aborted attempt. The
		// index/worktree CAS in restore decides whether our preimage still owns
		// each path or a concurrent checkout/edit must be preserved.
		return preparation.restore()
	}
}

func (preparation *v7WaveMemberPreparation) commit() {
	if preparation == nil {
		return
	}
	preparation.finished = true
	preparation.paths = nil
}

func syncV7WaveControlStateToIntegration(vaultPath string, wave Note, integrationBranch string) error {
	repoRoot := v7RepoRoot(vaultPath)
	rel, err := filepath.Rel(repoRoot, wave.AbsolutePath)
	if err != nil || filepath.IsAbs(rel) || strings.HasPrefix(filepath.Clean(rel), "..") {
		return tuskerError(errorInvalidTransition, "cannot sync wave control state to integration: invalid wave path "+wave.AbsolutePath)
	}
	canonical, err := os.ReadFile(wave.AbsolutePath)
	if err != nil {
		return err
	}
	tmp, err := os.MkdirTemp("", "tusker-wave-control-*")
	if err != nil {
		return err
	}
	defer func() { _ = exec.Command("git", "-C", repoRoot, "worktree", "remove", "--force", tmp).Run() }()
	if output, err := gitCombined(repoRoot, "worktree", "add", "--detach", tmp, integrationBranch); err != nil {
		return tuskerError(errorInvalidTransition, "failed to stage wave control state: "+firstActionableLine(output, err.Error()))
	}
	target := filepath.Join(tmp, rel)
	if err := writeText(target, string(canonical)); err != nil {
		return err
	}
	if output, err := gitCombined(tmp, "add", "--", filepath.ToSlash(rel)); err != nil {
		return tuskerError(errorInvalidTransition, "failed to stage wave control state: "+firstActionableLine(output, err.Error()))
	}
	if exec.Command("git", "-C", tmp, "diff", "--cached", "--quiet").Run() == nil {
		return nil
	}
	if output, err := gitCombined(tmp, "commit", "-m", "Sync wave control state "+stringField(wave.Data, "id")); err != nil {
		return tuskerError(errorInvalidTransition, "failed to commit wave control state: "+firstActionableLine(output, err.Error()))
	}
	commit, err := gitOutputTrim(tmp, "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	old, err := gitOutputTrim(repoRoot, "rev-parse", integrationBranch)
	if err != nil {
		return err
	}
	return updateGitRef(repoRoot, "refs/heads/"+integrationBranch, commit, old)
}

func v7WaveIntegrationMemberStatus(vaultPath string, wave Note, member string) (string, bool, error) {
	member = strings.ToUpper(strings.TrimSpace(member))
	if !v7TaskIDPattern.MatchString(member) {
		return "", false, tuskerError(errorInvalidField, "invalid wave member task id: "+member)
	}
	repoRoot := v7RepoRoot(vaultPath)
	integrationBranch := v7WaveIntegrationBranch(wave)
	rel := filepath.ToSlash(filepath.Join(relativeFromRepo(repoRoot, vaultPath), "work", "tasks", member+".md"))
	data, ok, err := v7GitFrontmatterAtRef(repoRoot, integrationBranch, rel)
	if err != nil {
		return "", false, err
	}
	if !ok {
		return "", false, nil
	}
	if effectiveV7Kind(data) != "task" || stringField(data, "id") != member {
		return "", false, tuskerError(errorInvalidField, "wave integration member identity mismatch: "+integrationBranch+":"+rel)
	}
	return stringField(data, "status"), true, nil
}

func advanceV7DefaultBranch(repoRoot, defaultBranch, newRev, oldRev string) error {
	checkouts, err := advanceV7DefaultBranchRef(repoRoot, defaultBranch, newRev, oldRev)
	if err != nil {
		return err
	}
	return finishV7DefaultBranchAdvance(checkouts)
}

// advanceV7DefaultBranchRef performs only the checked ref transition. Callers
// that hold the material epoch can release it after this returns, before
// derived epic/dashboard reconciliation acquires canonical document locks.
func advanceV7DefaultBranchRef(repoRoot, defaultBranch, newRev, oldRev string) ([]v7Worktree, error) {
	checkouts := v7DefaultBranchCheckouts(repoRoot, defaultBranch)
	if len(checkouts) == 0 {
		return nil, updateGitRef(repoRoot, "refs/heads/"+defaultBranch, newRev, oldRev)
	}
	for _, wt := range checkouts {
		if err := prepareV7GeneratedStateForDefaultAdvance(wt.Path); err != nil {
			return nil, err
		}
		if err := prepareV7IdenticalUntrackedStateForDefaultAdvance(wt.Path, newRev); err != nil {
			return nil, err
		}
		dirty, err := inPlaceDirtyPaths(wt.Path)
		if err != nil {
			return nil, err
		}
		if len(dirty) > 0 {
			return nil, tuskerError(errorInvalidTransition, defaultBranch+" is checked out in "+wt.Path+" with dirty paths: "+strings.Join(limitStrings(dirty, 5), ", ")+". Commit, stash, or clean those paths before running tusker land.", withPath(wt.Path))
		}
	}
	currentRev, err := gitOutputTrim(repoRoot, "rev-parse", "refs/heads/"+defaultBranch)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(currentRev) != strings.TrimSpace(oldRev) {
		return nil, tuskerError(errorInvalidTransition, defaultBranch+" changed while preparing wave land; retry tusker land so the default-branch advance can be checked against the current ref")
	}
	for _, wt := range checkouts {
		// Fast-forward-only merge advances the checked-out branch without
		// discarding local work: if local changes would be overwritten it fails
		// (safe refusal) instead of destroying them, unlike `reset --merge`
		// which silently drops staged/modified tracked files (including
		// tusker-owned .tusker/* files that the dirty-path guard skips).
		if output, err := gitCombined(wt.Path, "merge", "--ff-only", newRev); err != nil {
			status, _ := gitCombined(wt.Path, "status", "--porcelain")
			paths := strings.Join(limitStrings(strings.Fields(status), 12), " ")
			return nil, tuskerError(errorInvalidTransition, "failed to advance checked-out "+defaultBranch+" worktree at "+wt.Path+": "+firstActionableLine(output, err.Error())+"; local status: "+paths, withPath(wt.Path))
		}
		head, err := gitOutputTrim(wt.Path, "rev-parse", "HEAD")
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(head) != strings.TrimSpace(newRev) {
			return nil, tuskerError(errorInvalidTransition, "checked-out "+defaultBranch+" worktree did not advance to "+shortCommit(newRev), withPath(wt.Path))
		}
	}
	return checkouts, nil
}

func finishV7DefaultBranchAdvance(checkouts []v7Worktree) error {
	for _, wt := range checkouts {
		vaultPath := filepath.Join(wt.Path, ".tusker")
		idx, err := loadV7Index(vaultPath)
		if err != nil {
			return err
		}
		if _, err := reconcileV7EpicManagedBlocks(vaultPath, idx); err != nil {
			return err
		}
		if err := buildV7Dashboards(vaultPath, idx); err != nil {
			return err
		}
	}
	return nil
}

// prepareV7IdenticalUntrackedStateForDefaultAdvance resolves the benign case
// where Tusker created a local control-plane file before the candidate began
// tracking that exact file. Git refuses to overwrite any untracked path, even
// byte-identical content. We remove only .tusker files whose complete bytes
// are already recoverable from newRev; divergent or user-owned files remain
// untouched and continue to block the fast-forward.
func prepareV7IdenticalUntrackedStateForDefaultAdvance(workDir, newRev string) error {
	output, err := gitCombined(workDir, "status", "--porcelain=v1", "-z", "--untracked-files=all")
	if err != nil {
		return err
	}
	for _, entry := range strings.Split(output, "\x00") {
		if !strings.HasPrefix(entry, "?? ") {
			continue
		}
		rel := filepath.ToSlash(strings.TrimSpace(strings.TrimPrefix(entry, "?? ")))
		if !strings.HasPrefix(rel, ".tusker/") {
			continue
		}
		local, readErr := os.ReadFile(filepath.Join(workDir, filepath.FromSlash(rel)))
		if readErr != nil {
			return readErr
		}
		candidate, showErr := exec.Command("git", "-C", workDir, "show", newRev+":"+rel).Output()
		if showErr != nil {
			continue
		}
		if !bytes.Equal(local, candidate) {
			return tuskerError(errorInvalidTransition, "cannot advance default branch over divergent untracked Tusker state: "+rel, withPath(filepath.Join(workDir, filepath.FromSlash(rel))))
		}
		if err := os.Remove(filepath.Join(workDir, filepath.FromSlash(rel))); err != nil {
			return err
		}
	}
	return nil
}

func prepareV7GeneratedStateForDefaultAdvance(workDir string) error {
	for _, path := range []string{".tusker/Dashboard.md", ".tusker/_generated", ".tusker/dashboards"} {
		if output, err := gitCombined(workDir, "checkout", "--", path); err != nil && !strings.Contains(output, "did not match any file") {
			return tuskerError(errorInvalidTransition, "failed to reset generated projection before default-branch advance: "+firstActionableLine(output, err.Error()), withPath(workDir))
		}
	}
	output, err := gitCombined(workDir, "diff", "--name-only", "--", ".tusker/work/epics")
	if err != nil {
		return err
	}
	for _, path := range strings.Fields(output) {
		head, ok, err := v7GitNoteAtRef(workDir, "HEAD", path)
		if err != nil || !ok {
			return err
		}
		raw, err := os.ReadFile(filepath.Join(workDir, filepath.FromSlash(path)))
		if err != nil {
			return err
		}
		workingData, workingBody, err := parseFrontmatter(string(raw))
		if err != nil {
			return err
		}
		headRaw, err := serializeDocument(head.Data, head.Body, v7FrontmatterOrder["epic"])
		if err != nil {
			return err
		}
		workingRaw, err := serializeDocument(workingData, workingBody, v7FrontmatterOrder["epic"])
		if err != nil {
			return err
		}
		if stripV7EpicManagedState(headRaw) != stripV7EpicManagedState(workingRaw) {
			continue
		}
		if output, err := gitCombined(workDir, "checkout", "--", path); err != nil {
			return tuskerError(errorInvalidTransition, "failed to reset epic managed projection before default-branch advance: "+firstActionableLine(output, err.Error()), withPath(path))
		}
	}
	return nil
}

func v7DefaultBranchCheckouts(repoRoot, defaultBranch string) []v7Worktree {
	branchRef := "refs/heads/" + strings.TrimSpace(defaultBranch)
	if branchRef == "refs/heads/" {
		return nil
	}
	var out []v7Worktree
	for _, wt := range v7ListWorktrees(repoRoot) {
		if strings.TrimSpace(wt.Branch) == branchRef {
			out = append(out, wt)
		}
	}
	// A ref-only update leaves a checked-out branch's index and files stale.
	// Keep a direct primary-worktree fallback in case platform path aliases or
	// older Git porcelain omit/misreport that entry.
	if branch, err := gitOutputTrim(repoRoot, "symbolic-ref", "--short", "HEAD"); err == nil && strings.TrimSpace(branch) == strings.TrimSpace(defaultBranch) {
		found := false
		for _, wt := range out {
			if workspacePathsCompatible(wt.Path, repoRoot) {
				found = true
				break
			}
		}
		if !found {
			out = append(out, v7Worktree{Path: repoRoot, Branch: branchRef})
		}
	}
	return out
}

// printV7LandingSummary renders the concise landing summary for a successful
// land (A4): task, source branch, target, gate result, commit, and whether main
// moved or is waiting on open wave tasks.
func printV7LandingSummary(summary *v7LandSummary, args Args) {
	if summary == nil || args.Bool("json") || args.Bool("quiet") {
		return
	}
	if len(summary.Landed) == 0 && len(summary.Reworked) == 0 && len(summary.MainNotes) == 0 {
		return
	}
	var b strings.Builder
	vaultPath, _ := resolveVaultPath(args, false)
	if len(summary.Landed) > 0 {
		b.WriteString(fmt.Sprintf("Landed %d task%s:\n", len(summary.Landed), plural(len(summary.Landed))))
		for _, row := range summary.Landed {
			b.WriteString(fmt.Sprintf("  %s  %s -> %s  gate:%s  %s\n", row.Task, row.Branch, v7LandingTargetLabel(vaultPath, row.Target), row.GateResult, shortCommit(row.Commit)))
		}
	}
	if len(summary.Reworked) > 0 {
		b.WriteString(fmt.Sprintf("Returned %d task%s to rework:\n", len(summary.Reworked), plural(len(summary.Reworked))))
		for _, row := range summary.Reworked {
			b.WriteString(fmt.Sprintf("  %s  %s -> %s  gate:%s\n", row.Task, row.Branch, v7LandingTargetLabel(vaultPath, row.Target), row.GateResult))
		}
	}
	for _, note := range summary.MainNotes {
		b.WriteString(note + "\n")
	}
	fmt.Print(b.String())
}

// Basic operator output is product language. The internal wave remains
// available through diagnostic wave commands and audit payloads.
func v7LandingTargetLabel(vaultPath, target string) string {
	if strings.TrimSpace(vaultPath) == "" {
		return target
	}
	idx, err := loadV7Index(vaultPath)
	if err != nil {
		return target
	}
	for _, wave := range idx.Waves {
		if v7ImplicitDeliveryUnit(wave) && v7WaveIntegrationBranch(wave) == target {
			return "scheduled staging"
		}
	}
	return target
}

func shortCommit(commit string) string {
	commit = strings.TrimSpace(commit)
	if len(commit) > 12 {
		return commit[:12]
	}
	return commit
}

func runV7LandingGateOnRef(vaultPath, repoRoot, ref string) (bool, string) {
	tmp, err := os.MkdirTemp("", "tusker-land-gate-*")
	if err != nil {
		return false, err.Error()
	}
	removeWorktree := false
	defer func() {
		if removeWorktree {
			_ = exec.Command("git", "-C", repoRoot, "worktree", "remove", "--force", tmp).Run()
		} else {
			_ = os.RemoveAll(tmp)
		}
	}()
	if output, err := gitCombined(repoRoot, "worktree", "add", "--detach", tmp, ref); err != nil {
		return false, "failed to create gate worktree: " + firstActionableLine(output, err.Error())
	}
	removeWorktree = true
	return runV7LandingGate(vaultPath, tmp, "wave-ref:"+ref)
}

// runV7GateTierOnRef executes the canonical full gate on a detached frozen
// candidate. Unlike the focused landing gate, this path uses the shared gate
// ledger and harvest semantics and is therefore valid promotion proof.
type promotionGateExecution struct {
	Result           GateTierResult
	ArtifactRef      string
	ArtifactRefs     []string
	ProviderReceipts []GateProviderReceipt
	ProviderOutcomes []GateProviderReceipt
	FinalizeOutcomes func() error
	ProviderOutcome  v7FullGateOutcome
	Err              error
}

const (
	v7PromotionGateMaxCommands       = 64
	v7PromotionGateMaxCommandBytes   = 16 << 10
	v7PromotionGateMaxTranscriptSize = 4 << 20
	v7PromotionGateMaxReceipts       = v7PromotionGateMaxCommands
)

// validateV7PromotionGatePolicy bounds repository-controlled gate declarations
// before provider startup. Harvest mode is intentionally exhaustive, but it
// must not let an untrusted candidate turn that property into unbounded daemon
// memory, artifact, or lifecycle work.
func validateV7PromotionGatePolicy(policy GateTierPolicy) error {
	if len(policy.HarvestCommands) == 0 {
		return fmt.Errorf("promotion full-gate refusal: no harvest commands")
	}
	if len(policy.HarvestCommands) > v7PromotionGateMaxCommands {
		return fmt.Errorf("promotion full-gate refusal: harvest command count exceeds %d", v7PromotionGateMaxCommands)
	}
	for _, command := range policy.HarvestCommands {
		if len(command) == 0 || len(command) > v7PromotionGateMaxCommandBytes {
			return fmt.Errorf("promotion full-gate refusal: harvest command exceeds %d bytes", v7PromotionGateMaxCommandBytes)
		}
	}
	return nil
}

// v7CertifiedFullGateLedger rejects artifact-only or legacy green rows for a
// lifecycle-provider toolchain. The receipt is the durable certificate that
// the measured provider scope was cleaned, not merely a text log annotation.
type v7FullGateReceiptVerifier interface {
	MatchesGateProviderReceipt(*GateProviderReceipt) bool
}

type v7CertifiedFullGateLedger struct {
	store    *RuntimeStore
	verifier v7FullGateReceiptVerifier
}

func (l v7CertifiedFullGateLedger) FindGateLedger(projectID, treeHash, command, profile, toolchain string) (*GateLedgerEntry, error) {
	entry, err := l.store.FindGateLedger(projectID, treeHash, command, profile, toolchain)
	if err != nil || entry == nil || l.verifier == nil || !v7CertifiedGateProviderReceipt(entry.ProviderReceipt) || entry.ProviderReceipt.ProjectID != projectID || entry.ProviderReceipt.CandidateDigest != treeHash || entry.ProviderReceipt.CommandDigest != v7FullGateTextDigest(command) || entry.ProviderReceipt.Profile != profile || entry.ProviderReceipt.Toolchain != toolchain || !l.verifier.MatchesGateProviderReceipt(entry.ProviderReceipt) {
		return nil, err
	}
	return entry, nil
}

func v7CertifiedGateProviderReceipt(receipt *GateProviderReceipt) bool {
	return receipt != nil && receipt.Schema == v7FullGateProviderSchema && receipt.Outcome == string(v7FullGateOutcomePassed) && strings.TrimSpace(receipt.ProjectID) != "" && strings.TrimSpace(receipt.DepartureID) != "" && strings.TrimSpace(receipt.CandidateDigest) != "" && strings.TrimSpace(receipt.CommandDigest) != "" && strings.TrimSpace(receipt.Profile) != "" && strings.TrimSpace(receipt.ProviderProfile) != "" && strings.TrimSpace(receipt.Toolchain) != "" && strings.TrimSpace(receipt.LifecycleID) != "" && receipt.CleanupCertified && v7FullGateDigest(receipt.RequestDigest) && v7FullGateDigest(receipt.ProviderDigest) && v7FullGateDigest(receipt.ProviderClosureDigest) && v7FullGateDigest(receipt.ClientDigest) && v7FullGateDigest(receipt.ReceiptDigest) && v7FullGateDigest(receipt.RuntimeDigest) && v7FullGateDigest(receipt.PolicyDigest) && v7FullGateDigest(receipt.AttestationDigest) && v7FullGateDigest(receipt.ImageOrVMID) && v7FullGateDigest(receipt.CapabilitiesDigest) && v7FullGateDigest(receipt.ContainmentDigest) && v7FullGateDigest(receipt.CleanupDigest) && v7FullGateDigest(receipt.ResultDigest) && v7FullGateDigest(receipt.OutputDigest)
}

func runV7GateTierOnRef(vaultPath, repoRoot, ref, projectID string, policy GateTierPolicy, store *RuntimeStore) promotionGateExecution {
	return runV7GateTierOnRefContext(context.Background(), vaultPath, repoRoot, ref, projectID, policy, store)
}

func runV7GateTierOnRefContext(ctx context.Context, vaultPath, repoRoot, ref, projectID string, policy GateTierPolicy, store *RuntimeStore) promotionGateExecution {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return promotionGateExecution{Err: err}
	}
	stateRoot := DefaultStateRoot()
	if store != nil && strings.TrimSpace(store.stateRoot) != "" {
		stateRoot = store.stateRoot
	}
	artifactState, stateErr := openV7FullGateStateRoot(stateRoot)
	if stateErr != nil {
		return promotionGateExecution{Err: stateErr, ProviderOutcome: v7FullGateOutcomeProvider}
	}
	defer artifactState.Close()
	path := filepath.Join(artifactState.path, "artifacts", "promotion-gates", strings.ToLower(newRecordID())+".log")
	writeFailure := func(detail string) promotionGateExecution {
		artifactErr := writeV7DurablePromotionArtifactAtRoot(artifactState, path, []byte(safePacketText(detail, 4096)+"\n"))
		failureErr := fmt.Errorf("%s", detail)
		if artifactErr != nil {
			failureErr = errors.Join(failureErr, fmt.Errorf("durable artifact: %w", artifactErr))
			return promotionGateExecution{Err: failureErr}
		}
		return promotionGateExecution{ArtifactRef: path, ArtifactRefs: []string{path}, Err: failureErr}
	}
	if err := validateV7PromotionGatePolicy(policy); err != nil {
		return writeFailure(err.Error())
	}
	// An omitted gate profile is still a profile choice. Persist a stable name
	// rather than allowing an empty value to weaken the receipt/ledger key.
	if strings.TrimSpace(policy.Profile) == "" {
		policy.Profile = "default"
	}
	provider, err := newV7FullGateProvider(policy.IsolationProvider, repoRoot, stateRoot)
	if err != nil {
		failure := writeFailure("promotion full-gate refusal: " + err.Error())
		failure.ProviderOutcome = v7FullGateOutcomeProvider
		if errorToIssue(err).Code == v7FullGateProviderUnsupportedPlatformCode {
			failure.Err = errors.Join(err, failure.Err)
		}
		return failure
	}
	if external, ok := provider.(*v7ExternalFullGateProvider); ok && !artifactState.sameIdentity(external.state) {
		_ = provider.Close()
		failure := writeFailure("promotion full-gate refusal: daemon state root identity changed while resolving the provider")
		failure.ProviderOutcome = v7FullGateOutcomeProvider
		return failure
	}
	defer provider.Close()
	tmp, err := os.MkdirTemp("", "tusker-promotion-gate-*")
	if err != nil {
		return writeFailure("failed to create full-gate temporary workspace: " + err.Error())
	}
	removeWorktree := false
	defer func() {
		if removeWorktree {
			_ = exec.Command("git", "-C", repoRoot, "worktree", "remove", "--force", tmp).Run()
		} else {
			_ = os.RemoveAll(tmp)
		}
	}()
	if output, err := gitCombined(repoRoot, "worktree", "add", "--detach", tmp, ref); err != nil {
		return writeFailure("failed to create full-gate worktree: " + firstActionableLine(output, err.Error()))
	}
	removeWorktree = true
	if err := ctx.Err(); err != nil {
		return promotionGateExecution{Err: err}
	}
	runtime := defaultGateTierRuntimeWithContext(ctx, store, projectID, tmp)
	frozenTreeHash, err := runtime.TreeHash(tmp)
	if err != nil {
		return writeFailure("promotion full-gate refusal: cannot freeze candidate tree identity: " + err.Error())
	}
	frozenTreeStatus, err := runtime.TreeStatus(tmp)
	if err != nil {
		return writeFailure("promotion full-gate refusal: cannot freeze candidate worktree status: " + err.Error())
	}
	// Git facts are computed by the trusted daemon before command execution.
	// Removing only this disposable linked-worktree pointer prevents a
	// repository-controlled gate from reaching the shared common directory,
	// refs, hooks, config, or signing material. It also keeps Go/Rust VCS
	// stamping from turning read access to the control repository into an
	// accidental prerequisite for a full gate.
	gitPointer := filepath.Join(tmp, ".git")
	info, statErr := os.Lstat(gitPointer)
	if statErr != nil || !info.Mode().IsRegular() {
		return writeFailure("promotion full-gate refusal: detached candidate Git pointer is missing or invalid")
	}
	gitPointerRaw, err := os.ReadFile(gitPointer)
	if err != nil {
		return writeFailure("promotion full-gate refusal: cannot read detached candidate Git pointer: " + err.Error())
	}
	if err := os.Remove(gitPointer); err != nil {
		return writeFailure("promotion full-gate refusal: cannot detach candidate from shared Git metadata: " + err.Error())
	}
	defer func() {
		if fileExists(tmp) {
			_ = os.WriteFile(gitPointer, gitPointerRaw, info.Mode().Perm())
		}
	}()
	runtime.TreeHash = func(string) (string, error) { return frozenTreeHash, nil }
	runtime.TreeStatus = func(string) (string, error) { return frozenTreeStatus, nil }
	runtime.Toolchain = func(repoRoot string, commands []string) string {
		return scheduledPromotionFullGateToolchainFingerprint(repoRoot, commands, policy.IsolationProvider, stateRoot)
	}
	providerBinder, providerCanBind := provider.(v7FullGateProviderBinder)
	providerArtifactBinder, providerCanBindArtifact := provider.(v7FullGateProviderArtifactBinder)
	providerBinding := v7FullGateProviderBinding{
		ProjectID: projectID, DepartureID: v7FullGateDepartureID(ctx), CandidateDigest: frozenTreeHash, GateProfile: policy.Profile, ProviderProfile: policy.IsolationProvider,
		Toolchain: runtime.Toolchain(tmp, policy.HarvestCommands), ArtifactRef: path,
	}
	if providerCanBind {
		if err := providerBinder.BindFullGateProvider(providerBinding); err != nil {
			failure := writeFailure("promotion full-gate refusal: " + err.Error())
			failure.ProviderOutcome = v7FullGateOutcomeProvider
			return failure
		}
	}
	runtime.DiffPaths = nil
	verifier, trustedProvider := provider.(v7FullGateReceiptVerifier)
	if store != nil {
		runtime.Ledger = v7CertifiedFullGateLedger{store: store, verifier: verifier}
	}
	var raw v7GateBoundedOutput
	raw.max = v7PromotionGateMaxTranscriptSize
	raw.truncationNotice = "\n[promotion gate transcript truncated]\n"
	var commandReceipt *GateProviderReceipt
	var providerReceipts []GateProviderReceipt
	var providerOutcomes []GateProviderReceipt
	var artifactRefs []string
	var ledgerErr error
	var typedProviderErr error
	var typedProviderOutcome v7FullGateOutcome
	runtime.RecordPass = func(command, treeHash, profile, toolchain string, elapsed time.Duration) {
		if ledgerErr != nil {
			return
		}
		if store == nil || !trustedProvider || !verifier.MatchesGateProviderReceipt(commandReceipt) {
			ledgerErr = fmt.Errorf("%w: cannot persist certified lifecycle receipt for %q", errV7FullGateProvider, command)
			return
		}
		receipt := *commandReceipt
		if len(providerReceipts) >= v7PromotionGateMaxReceipts {
			ledgerErr = fmt.Errorf("%w: provider receipt count exceeds %d", errV7FullGateProvider, v7PromotionGateMaxReceipts)
			return
		}
		if err := store.RecordGateLedger(GateLedgerEntry{ProjectID: projectID, TreeHash: treeHash, Command: command, Profile: profile, Toolchain: toolchain, Host: runtimeLeaseHost(), DurationMS: elapsed.Milliseconds(), ProviderReceipt: &receipt}); err != nil {
			ledgerErr = fmt.Errorf("%w: persist certified lifecycle receipt for %q: %v", errV7FullGateProvider, command, err)
			return
		}
		// Ledger persistence is not the retirement boundary. The shared gate
		// artifact is written and rebound into every pending journal first;
		// the caller acknowledges all outcomes only after its departure CAS.
		providerReceipts = append(providerReceipts, receipt)
		commandReceipt = nil
	}
	runtime.Exec = func(workspace, command string) (string, error) {
		commandReceipt = nil
		if typedProviderOutcome != "" {
			return "", typedProviderErr
		}
		commandArtifact := filepath.Join(artifactState.path, "artifacts", "promotion-gates", strings.ToLower(newRecordID())+".log")
		artifactRefs = append(artifactRefs, commandArtifact)
		if providerCanBind {
			commandBinding := providerBinding
			commandBinding.ArtifactRef = commandArtifact
			if bindErr := providerBinder.BindFullGateProvider(commandBinding); bindErr != nil {
				typedProviderOutcome = v7FullGateOutcomeProvider
				typedProviderErr = fmt.Errorf("%w: bind command evidence: %v", errV7FullGateProvider, bindErr)
				detail := []byte("# lifecycle_gate_error=" + safePacketText(typedProviderErr.Error(), 4096) + "\n")
				_, _ = raw.Write(detail)
				if artifactErr := writeV7DurablePromotionArtifactAtRoot(artifactState, commandArtifact, detail); artifactErr != nil {
					typedProviderErr = errors.Join(typedProviderErr, artifactErr)
				}
				return "", typedProviderErr
			}
		}
		scheduledPromotionBeforeFullGateCommand(command)
		invocation, runErr := provider.Run(ctx, workspace, command)
		output := string(invocation.Output)
		var commandEvidence v7GateBoundedOutput
		commandEvidence.max = v7FullGateArtifactMaxBytes
		commandEvidence.truncationNotice = "\n[provider command evidence truncated]\n"
		fmt.Fprintf(&commandEvidence, "$ %s\n%s\n", command, output)
		receipt := invocation.Receipt
		receiptMatches := receipt.ReceiptDigest != "" && receipt.RequestDigest != "" && receipt.Outcome == string(invocation.Outcome) && receipt.CommandDigest == v7FullGateTextDigest(command) && receipt.ProjectID == projectID && receipt.CandidateDigest == frozenTreeHash
		if receiptMatches {
			fmt.Fprintf(&commandEvidence, "# lifecycle_id=%s request_digest=%s receipt_digest=%s outcome=%s\n", receipt.LifecycleID, receipt.RequestDigest, receipt.ReceiptDigest, receipt.Outcome)
			providerOutcomes = append(providerOutcomes, receipt)
			if runErr == nil && v7CertifiedGateProviderReceipt(&receipt) {
				commandReceipt = &receipt
			}
		}
		if runErr != nil {
			fmt.Fprintf(&commandEvidence, "# lifecycle_gate_error=%s\n", safePacketText(runErr.Error(), 4096))
		}
		_, _ = raw.Write(commandEvidence.Bytes())
		artifactErr := writeV7DurablePromotionArtifactAtRoot(artifactState, commandArtifact, commandEvidence.Bytes())
		if artifactErr == nil && providerCanBindArtifact && receipt.RequestDigest != "" && receipt.ReceiptDigest != "" {
			artifactErr = providerArtifactBinder.BindFullGateProviderArtifact(commandArtifact, []GateProviderReceipt{receipt})
		}
		if artifactErr != nil {
			commandReceipt = nil
			typedProviderOutcome = v7FullGateOutcomeProvider
			typedProviderErr = fmt.Errorf("%w: persist command evidence: %v", errV7FullGateProvider, artifactErr)
			runErr = errors.Join(runErr, typedProviderErr)
		}
		if invocation.Outcome == v7FullGateOutcomeProvider || invocation.Outcome == v7FullGateOutcomeCanceled || invocation.Outcome == v7FullGateOutcomeTimedOut {
			typedProviderOutcome = invocation.Outcome
			typedProviderErr = runErr
		} else if isV7FullGateProviderError(runErr) {
			typedProviderOutcome = v7FullGateOutcomeProvider
			typedProviderErr = runErr
		}
		if typedProviderOutcome != "" && typedProviderErr == nil {
			typedProviderErr = &v7FullGateOutcomeError{Outcome: typedProviderOutcome}
		}
		return output, runErr
	}
	result, err := runGateTier(policy, policy.Profile, runtime)
	if err == nil && ledgerErr != nil {
		err = ledgerErr
	}
	if typedProviderOutcome == "" && isV7FullGateProviderError(err) {
		typedProviderOutcome = v7FullGateOutcomeProvider
		typedProviderErr = err
	}
	if err == nil && typedProviderOutcome != "" {
		if typedProviderErr == nil {
			typedProviderErr = &v7FullGateOutcomeError{Outcome: typedProviderOutcome}
		}
		err = typedProviderErr
	}
	if err == nil && (result.Outcome == gateOutcomePassed || result.Outcome == gateOutcomeLedgerHit) {
		verified := make([]GateProviderReceipt, 0, len(policy.HarvestCommands))
		ledger := v7CertifiedFullGateLedger{store: store, verifier: verifier}
		for _, command := range policy.HarvestCommands {
			entry, lookupErr := ledger.FindGateLedger(projectID, frozenTreeHash, command, result.Profile, result.Toolchain)
			if lookupErr != nil || entry == nil || entry.ProviderReceipt == nil {
				err = fmt.Errorf("%w: complete full-gate receipt set unavailable for %q: %v", errV7FullGateProvider, command, lookupErr)
				break
			}
			verified = append(verified, *entry.ProviderReceipt)
		}
		if err == nil {
			providerReceipts = verified
		}
	}
	if err != nil {
		// The raw provider transcript can be long enough to hide this final
		// ledger/certification error in callers' bounded excerpts. Keep the
		// fail-closed cause in the durable artifact itself.
		fmt.Fprintf(&raw, "# lifecycle_gate_error=%s\n", safePacketText(err.Error(), 4096))
	}
	writeErr := writeV7DurablePromotionArtifactAtRoot(artifactState, path, raw.Bytes())
	if writeErr != nil && err == nil {
		err = writeErr
	}
	artifactRefs = append(artifactRefs, path)
	finalizeOutcomes := func() error {
		if writeErr != nil {
			return fmt.Errorf("%w: cannot acknowledge provider outcomes without durable artifact: %v", errV7FullGateProvider, writeErr)
		}
		finalizer, ok := provider.(v7FullGateProviderFinalizer)
		if !ok {
			return nil
		}
		var errs []error
		for _, receipt := range providerOutcomes {
			if finalizeErr := finalizer.FinalizeFullGateProviderOutcome(receipt); finalizeErr != nil {
				errs = append(errs, finalizeErr)
			}
		}
		return errors.Join(errs...)
	}
	return promotionGateExecution{Result: result, ArtifactRef: path, ArtifactRefs: artifactRefs, ProviderReceipts: providerReceipts, ProviderOutcomes: providerOutcomes, FinalizeOutcomes: finalizeOutcomes, ProviderOutcome: typedProviderOutcome, Err: err}
}

func appendV7WaveLandingAudit(vaultPath, waveID string, entries []v7LandingAuditEntry, actor string) error {
	if len(entries) == 0 {
		return nil
	}
	note, err := resolveV7Note(vaultPath, waveID, "wave")
	if err != nil {
		return err
	}
	nextRev, changed, err := mutateV7DocumentLocked(note.AbsolutePath, v7FrontmatterOrder["wave"], func(data map[string]any, body string) (map[string]any, string, bool, error) {
		landings := normalizeLandingAudit(data["landings"])
		seen := map[string]bool{}
		for _, row := range landings {
			key := fmt.Sprintf("%s|%s|%s|%s|%s|%s|%s|%s|%s|%s",
				stringField(row, "task"), stringField(row, "branch"), stringField(row, "source_sha"),
				stringField(row, "target"), stringField(row, "base_sha"), stringField(row, "merge_commit"),
				stringField(row, "commit"), stringField(row, "tree"), stringField(row, "receipt_fingerprint"), stringField(row, "defect_id"))
			seen[key] = true
		}
		before := len(landings)
		for _, entry := range entries {
			provenance := ""
			if entry.Task != "wave" &&
				entry.GateResult == "pass" &&
				entry.SourceSHA != "" &&
				entry.SourceProvenance != "" &&
				entry.BaseSHA != "" &&
				entry.MergeCommit != "" &&
				entry.GateFingerprint != "" &&
				entry.ReceiptFingerprint != "" &&
				entry.ControlAuthority != "" &&
				entry.Commit != "" &&
				entry.Tree != "" {
				provenance = v7LandingAuditProvenance
			}
			key := fmt.Sprintf("%s|%s|%s|%s|%s|%s|%s|%s|%s|%s",
				entry.Task, entry.Branch, entry.SourceSHA, entry.Target, entry.BaseSHA,
				entry.MergeCommit, entry.Commit, entry.Tree, entry.ReceiptFingerprint, entry.DefectID)
			if seen[key] {
				continue
			}
			row := map[string]any{
				"task":        entry.Task,
				"branch":      entry.Branch,
				"target":      entry.Target,
				"gate_result": entry.GateResult,
				"timestamp":   entry.Timestamp,
				"actor":       entry.Actor,
			}
			if entry.GateSummary != "" {
				row["gate_summary"] = entry.GateSummary
			}
			if entry.SourceSHA != "" {
				row["source_sha"] = entry.SourceSHA
			}
			if entry.SourceProvenance != "" {
				row["source_provenance"] = entry.SourceProvenance
			}
			if entry.BaseSHA != "" {
				row["base_sha"] = entry.BaseSHA
			}
			if entry.MergeCommit != "" {
				row["merge_commit"] = entry.MergeCommit
			}
			if entry.GateFingerprint != "" {
				row["gate_fingerprint"] = entry.GateFingerprint
			}
			if entry.ReceiptFingerprint != "" {
				row["receipt_fingerprint"] = entry.ReceiptFingerprint
			}
			if entry.ControlAuthority != "" {
				row["control_authority"] = entry.ControlAuthority
			}
			if entry.Commit != "" {
				row["commit"] = entry.Commit
			}
			if entry.Tree != "" {
				row["tree"] = entry.Tree
			}
			if entry.DefectID != "" {
				row["defect_id"] = entry.DefectID
			}
			if provenance != "" {
				row["provenance"] = provenance
			}
			landings = append(landings, row)
			seen[key] = true
		}
		changed := len(landings) != before
		if receiptAt, ok := v7WaveLandingReceiptAt(landings); ok {
			if receiptAt == "" {
				receiptAt = time.Now().UTC().Format(time.RFC3339)
			}
			if stringField(data, "status") != "landed" {
				data["status"] = "landed"
				changed = true
			}
			if stringField(data, "landed_at") != receiptAt {
				data["landed_at"] = receiptAt
				changed = true
			}
		}
		if !changed {
			return data, body, false, nil
		}
		data["landings"] = landings
		data["updated_at"] = time.Now().UTC().Format(time.RFC3339)
		data["updated_by"] = actor
		return data, body, true, nil
	})
	if err != nil {
		return err
	}
	if !changed {
		return nil
	}
	return emitV7Event(vaultPath, waveID, "wave", "updated", actor, map[string]any{"landing_audit": len(entries), "state_rev": nextRev})
}

func normalizeLandingAudit(value any) []map[string]any {
	var out []map[string]any
	switch typed := value.(type) {
	case []map[string]any:
		out = append(out, typed...)
	case []any:
		for _, item := range typed {
			if row, ok := item.(map[string]any); ok {
				out = append(out, row)
			}
		}
	}
	return out
}

func v7TaskBranchName(taskID string) string {
	return "task/" + strings.ToUpper(strings.TrimSpace(taskID))
}

func v7IntegrationBranchName(waveID string) string {
	return "integration/" + strings.ToUpper(strings.TrimSpace(waveID))
}

func v7GitObjectID(value string) bool {
	value = strings.TrimSpace(value)
	if len(value) != 40 && len(value) != 64 {
		return false
	}
	for _, r := range value {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

func v7WaveIntegrationBranch(wave Note) string {
	return firstNonEmpty(stringField(wave.Data, "integration_branch"), v7IntegrationBranchName(stringField(wave.Data, "id")))
}

func v7WorkspaceBranchForTask(vaultPath string, note Note) (string, string, error) {
	taskID := trackerRecordID(note)
	waveID := stringField(note.Data, "wave")
	if strings.TrimSpace(taskID) == "" || strings.TrimSpace(waveID) == "" {
		return "", "", nil
	}
	idx, err := loadV7Index(vaultPath)
	if err != nil {
		return "", "", err
	}
	wave, ok := idx.Waves[waveID]
	if !ok {
		return "", "", tuskerError(errorNotFound, "V7 wave not found: "+waveID)
	}
	base := v7WaveIntegrationBranch(wave)
	if v7GitRepo(v7RepoRoot(vaultPath)) && !gitRefExists(v7RepoRoot(vaultPath), "refs/heads/"+base) {
		if frozen := strings.TrimSpace(stringField(wave.Data, "integration_base_sha")); frozen != "" {
			// Fresh Start waves intentionally defer integration-ref creation until
			// the serialized landing path. Task worktrees still branch from the
			// exact frozen commit, not whatever default happens to be later.
			base = frozen
		}
	}
	return v7TaskBranchName(taskID), base, nil
}

// v7WorkspaceBranchForLane resolves an isolated workspace branch for a
// dispatched lane. Execute work stays on the task branch. Review must instead
// inspect a separate branch pinned to the exact implementation commit: a
// second worktree cannot check out the task branch, and falling back to HEAD
// makes the reviewer inspect a different delivery than the one it is judging.
func v7WorkspaceBranchForLane(vaultPath string, note Note, lane string) (string, string, error) {
	branchName, branchBase, err := v7WorkspaceBranchForTask(vaultPath, note)
	if err != nil || strings.TrimSpace(lane) != runLaneReview {
		return branchName, branchBase, err
	}
	taskID := trackerRecordID(note)
	repoRoot := v7RepoRoot(vaultPath)
	if strings.TrimSpace(taskID) == "" || !v7GitRepo(repoRoot) {
		return branchName, branchBase, nil
	}
	sourceSHA := firstNonEmpty(
		stringField(note.Data, "source_sha"),
		stringField(note.Data, "source_commit"),
		stringField(note.Data, "source_branch_sha"),
	)
	if sourceSHA != "" {
		recordedSource := sourceSHA
		resolvedSource, ok := gitRevParse(repoRoot, sourceSHA+"^{commit}")
		if !ok {
			return "", "", tuskerError(errorInvalidTransition, "review workspace requires recorded implementation source "+recordedSource+"; repair the task source identity before review")
		}
		sourceSHA = resolvedSource
		return v7ReviewBranchName(taskID, sourceSHA), sourceSHA, nil
	}
	implementationBranch := v7TaskBranchName(taskID)
	var ok bool
	sourceSHA, ok = gitRevParse(repoRoot, "refs/heads/"+implementationBranch+"^{commit}")
	if !ok {
		return "", "", tuskerError(
			errorInvalidTransition,
			"review workspace requires implementation branch "+implementationBranch+"; rerun the execute lane or repair the missing task branch before review",
		)
	}
	return v7ReviewBranchName(taskID, sourceSHA), sourceSHA, nil
}

func v7ReviewBranchName(taskID, sourceSHA string) string {
	return "review/" + strings.ToUpper(strings.TrimSpace(taskID)) + "/" + strings.ToLower(strings.TrimSpace(sourceSHA))
}

func ensureV7WaveIntegrationBranch(vaultPath string, wave Note) error {
	branch := v7WaveIntegrationBranch(wave)
	repoRoot := v7RepoRoot(vaultPath)
	if !v7GitRepo(repoRoot) || gitRefExists(repoRoot, "refs/heads/"+branch) {
		return nil
	}
	frozen := strings.TrimSpace(stringField(wave.Data, "integration_base_sha"))
	if frozen == "" {
		return ensureV7IntegrationBranch(vaultPath, branch)
	}
	current, err := gitOutputTrim(repoRoot, "rev-parse", "refs/heads/"+v7DefaultBranch(vaultPath))
	if err != nil || current != frozen {
		return tuskerError(errorInvalidTransition, "integration base drifted before first completion; regenerate delivery review and Start")
	}
	// Supplying the all-zero old value makes this a create-only CAS. A racing
	// completion cannot silently overwrite another integration lane.
	zero := strings.Repeat("0", len(frozen))
	if _, err := gitCombined(repoRoot, "update-ref", "refs/heads/"+branch, frozen, zero); err != nil {
		return tuskerError(errorInvalidTransition, "failed to create integration branch from frozen base; retry the serialized landing lane: "+err.Error())
	}
	return nil
}

func ensureV7IntegrationBranch(vaultPath, branch string) error {
	repoRoot := v7RepoRoot(vaultPath)
	if !v7GitRepo(repoRoot) {
		return nil
	}
	if gitRefExists(repoRoot, "refs/heads/"+branch) {
		return nil
	}
	base := v7DefaultBranch(vaultPath)
	if _, err := gitCombined(repoRoot, "branch", branch, base); err != nil {
		return tuskerError(errorInvalidTransition, "failed to create integration branch "+branch+" from "+base+": "+err.Error())
	}
	return nil
}

func v7DefaultBranch(vaultPath string) string {
	if cfg, _, err := readV7TuskerConfig(vaultPath); err == nil {
		if branch := strings.TrimSpace(cfg.Branches.DefaultBranch); branch != "" {
			return branch
		}
	}
	repoRoot := v7RepoRoot(vaultPath)
	for _, branch := range []string{"main", "master"} {
		if gitRefExists(repoRoot, "refs/heads/"+branch) {
			return branch
		}
	}
	if branch, err := currentGitBranchIn(repoRoot); err == nil && branch != "" && branch != "HEAD" {
		return branch
	}
	return "main"
}

func v7GitRepo(repoRoot string) bool {
	if strings.TrimSpace(repoRoot) == "" {
		return false
	}
	return exec.Command("git", "-C", repoRoot, "rev-parse", "--git-dir").Run() == nil
}

func gitBranchExists(repoRoot, branch string) bool {
	return gitRefExists(repoRoot, "refs/heads/"+branch)
}

func gitMergeBaseAncestor(repoRoot, ancestor, descendant string) bool {
	return exec.Command("git", "-C", repoRoot, "merge-base", "--is-ancestor", ancestor, descendant).Run() == nil
}

func deleteGitBranch(repoRoot, branch string) error {
	if !gitBranchExists(repoRoot, branch) {
		return nil
	}
	if _, err := gitCombined(repoRoot, "branch", "-D", branch); err != nil {
		return err
	}
	return nil
}

func updateGitRef(repoRoot, ref, next, old string) error {
	args := []string{"update-ref", ref, next}
	if strings.TrimSpace(old) != "" {
		args = append(args, old)
	}
	if _, err := gitCombined(repoRoot, args...); err != nil {
		return err
	}
	return nil
}

func gitOutputTrim(repoRoot string, args ...string) (string, error) {
	output, err := gitCombined(repoRoot, args...)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(output), nil
}

func gitCombined(repoRoot string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", repoRoot}, args...)...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return string(output), fmt.Errorf("git -C %s %s: %w: %s", repoRoot, strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return string(output), nil
}
