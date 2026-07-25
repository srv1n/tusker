package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"time"
)

const (
	tuskerLandMainGuardEnv     = "TUSKER_LAND_MAIN_OK"
	v7LandingLockSchema        = "tusker.landing-lock/v1"
	v7LandingLockRecoveryGrace = 30 * time.Second
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
	ID        string
	Branch    string
	SourceSHA string
}

type v7LandingAuditEntry struct {
	Task        string
	Branch      string
	SourceSHA   string
	Target      string
	GateResult  string
	GateSummary string
	Commit      string
	Actor       string
	Timestamp   string
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
	return landV7CmdWithFrozenSources(args, nil)
}

func landV7CmdWithFrozenSources(args Args, frozenSources map[string]string) error {
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
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
	} else if err := landV7TaskTargets(vaultPath, targets, args, summary, frozenSources); err != nil {
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

func landV7TaskTargets(vaultPath string, targets []string, args Args, summary *v7LandSummary, frozenSources map[string]string) error {
	idx, err := loadV7Index(vaultPath)
	if err != nil {
		return err
	}
	repoRoot := v7RepoRoot(vaultPath)
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
		if _, ok := idx.Waves[waveID]; !ok {
			return tuskerError(errorNotFound, "V7 wave not found: "+waveID)
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
		byWave[waveID] = append(byWave[waveID], v7LandTask{ID: taskID, Branch: branch, SourceSHA: sourceSHA})
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
		if err := stageV7WaveBatch(vaultPath, repoRoot, waveID, byWave[waveID], acc); err != nil {
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
	actor := landV7Actor(args)
	for _, waveID := range waveIDs {
		var entries []v7LandingAuditEntry
		for _, landed := range acc.Landed {
			if landed.WaveID != waveID {
				continue
			}
			entry := landed.Entry
			entry.Actor = actor
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
	if _, err := gitCombined(repoRoot, "branch", branch, commit); err != nil {
		return tuskerError(errorInvalidTransition, "failed to create "+branch+" from "+source+": "+firstActionableLine(err.Error(), err.Error()))
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

func stageV7WaveBatch(vaultPath, repoRoot, waveID string, tasks []v7LandTask, acc *v7BatchAccumulator) error {
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
	if err := ensureV7IntegrationBranch(vaultPath, integrationBranch); err != nil {
		return err
	}
	return landV7BatchRecursive(vaultPath, repoRoot, waveID, integrationBranch, tasks, acc)
}

func landV7BatchRecursive(vaultPath, repoRoot, waveID, integrationBranch string, tasks []v7LandTask, acc *v7BatchAccumulator) error {
	if len(tasks) == 0 {
		return nil
	}
	pass, commit, summary, err := stageV7LandingBatch(vaultPath, repoRoot, integrationBranch, tasks)
	if err != nil {
		return err
	}
	if pass {
		if err := updateGitRef(repoRoot, "refs/heads/"+integrationBranch, commit, ""); err != nil {
			return err
		}
		now := time.Now().UTC().Format(time.RFC3339)
		for _, task := range tasks {
			acc.Landed = append(acc.Landed, v7LandedEntry{WaveID: waveID, Entry: v7LandingAuditEntry{
				Task: task.ID, Branch: task.Branch, SourceSHA: task.SourceSHA, Target: integrationBranch,
				GateResult: "pass", GateSummary: summary, Commit: commit, Timestamp: now,
			}})
		}
		return nil
	}
	if len(tasks) == 1 {
		acc.Failed = append(acc.Failed, v7LandFailure{WaveID: waveID, Task: tasks[0], Summary: summary})
		return nil
	}
	mid := len(tasks) / 2
	if err := landV7BatchRecursive(vaultPath, repoRoot, waveID, integrationBranch, tasks[:mid], acc); err != nil {
		return err
	}
	return landV7BatchRecursive(vaultPath, repoRoot, waveID, integrationBranch, tasks[mid:], acc)
}

func stageV7LandingBatch(vaultPath, repoRoot, baseBranch string, tasks []v7LandTask) (bool, string, string, error) {
	tmp, err := os.MkdirTemp("", "tusker-land-stage-*")
	if err != nil {
		return false, "", "", err
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
		return false, "", "", tuskerError(errorInvalidTransition, "failed to create landing staging worktree: "+firstActionableLine(output, err.Error()))
	}
	removeWorktree = true
	for _, task := range tasks {
		source := firstNonEmpty(strings.TrimSpace(task.SourceSHA), task.Branch)
		if output, err := gitCombined(tmp, "merge", "--no-ff", "--no-edit", source); err != nil {
			resolved, unresolved, resolveErr := resolveV7GeneratedProjectionMerge(tmp)
			if resolveErr != nil {
				return false, "", "", resolveErr
			}
			if !resolved {
				summary := landingFailureSummary("merge "+source, output, err)
				if unresolved != "" {
					summary = limitLandingSummary(summary+"; all unmerged paths: "+unresolved, 500)
				}
				return false, "", summary, nil
			}
		}
		if err := guardV7LandingTerminalTaskRewinds(tmp, baseBranch); err != nil {
			return false, "", "", err
		}
	}
	if err := removeV7WorkspaceMetadataFromLanding(tmp); err != nil {
		return false, "", "", err
	}
	if err := commitV7LandingCleanup(tmp); err != nil {
		return false, "", "", err
	}
	pass, summary := runV7LandingGate(vaultPath, tmp, v7LandingBatchIdentity(tasks))
	if !pass {
		return false, "", summary, nil
	}
	commit, err := gitOutputTrim(tmp, "rev-parse", "HEAD")
	if err != nil {
		return false, "", "", err
	}
	return true, commit, summary, nil
}

// resolveV7GeneratedProjectionMerge prevents derived Tusker dashboards from
// serializing otherwise-independent task landings. Only an all-generated
// conflict is eligible; any source or task-contract conflict remains a hard
// landing failure.
func resolveV7GeneratedProjectionMerge(workDir string) (bool, string, error) {
	output, err := gitCombined(workDir, "diff", "--name-only", "--diff-filter=U")
	if err != nil {
		return false, "", err
	}
	paths := strings.Fields(output)
	if len(paths) == 0 {
		return false, "", nil
	}
	for _, path := range paths {
		generated := path == ".tusker/Dashboard.md" || path == ".tusker/workspace.json" || strings.HasPrefix(path, ".tusker/_generated/") || strings.HasPrefix(path, ".tusker/dashboards/")
		if strings.HasPrefix(path, ".tusker/work/epics/") && strings.HasSuffix(path, ".md") {
			generated = v7EpicConflictOnlyTouchesManagedState(workDir, path)
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
	if output, err := gitCombined(workDir, "add", ".tusker/Dashboard.md", ".tusker/_generated", ".tusker/dashboards", ".tusker/work/epics"); err != nil {
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
	output, err := gitCombined(workDir, "diff", "--name-only", baseRef+"..HEAD", "--", ".tusker/work/tasks")
	if err != nil {
		return err
	}
	for _, rel := range strings.Fields(output) {
		if !strings.HasSuffix(rel, ".md") {
			continue
		}
		baseData, ok, err := v7GitFrontmatterAtRef(workDir, baseRef, rel)
		if err != nil || !ok {
			return err
		}
		headData, ok, err := v7GitFrontmatterAtRef(workDir, "HEAD", rel)
		if err != nil || !ok {
			return err
		}
		if err := guardV7TerminalTaskRewind(filepath.Join(workDir, filepath.FromSlash(rel)), "land:"+baseRef, baseData, headData); err != nil {
			return err
		}
	}
	return nil
}

func runV7LandingGate(vaultPath, workDir, laneIdentity string) (bool, string) {
	commands := backpressureCommands(vaultPath)
	if len(commands) == 0 {
		commands = []string{"go build ./...", "go vet ./...", "go test ./... -count=1"}
	}
	fingerprint := v7LandingGateFingerprint(workDir, laneIdentity, commands)
	if fingerprint != "" && v7LandingGateCacheHit(vaultPath, fingerprint) {
		return true, "gate cached: " + fingerprint
	}
	for _, command := range commands {
		cmd := exec.Command("sh", "-c", command)
		cmd.Dir = workDir
		output, err := cmd.CombinedOutput()
		if err != nil {
			return false, landingFailureSummary(command, string(output), err)
		}
	}
	if fingerprint != "" {
		_ = writeV7LandingGateCache(vaultPath, fingerprint, commands)
	}
	return true, "gate passed: " + strings.Join(commands, " && ")
}

type v7LandingGateCacheRecord struct {
	Schema      string   `json:"schema"`
	Fingerprint string   `json:"fingerprint"`
	Commands    []string `json:"commands"`
	PassedAt    string   `json:"passed_at"`
}

var landingToolchainProbe = v7LandingToolchainFingerprints

func v7LandingGateFingerprint(workDir, laneIdentity string, commands []string) string {
	head, err := gitOutputTrim(workDir, "rev-parse", "HEAD")
	if err != nil || head == "" {
		return ""
	}
	parts := []string{"tusker.landing-gate/v2", head, strings.TrimSpace(laneIdentity)}
	toolchains := landingToolchainProbe(workDir, commands)
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
	return json.Unmarshal(raw, &record) == nil && record.Schema == "tusker.landing-gate-cache/v1" && record.Fingerprint == fingerprint
}

func writeV7LandingGateCache(vaultPath, fingerprint string, commands []string) error {
	path := v7LandingGateCachePath(vaultPath, fingerprint)
	if err := ensureDir(filepath.Dir(path)); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(v7LandingGateCacheRecord{Schema: "tusker.landing-gate-cache/v1", Fingerprint: fingerprint, Commands: commands, PassedAt: time.Now().UTC().Format(time.RFC3339)}, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp-" + newRecordID()
	if err := os.WriteFile(tmp, append(raw, '\n'), 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
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
	if err := statusV7Cmd(Args{
		"vault": vaultPath, "quiet": "true", "local": "true",
		"id": taskID, "status": "rework", "by": actor,
		"reason": summary,
	}); err != nil {
		return err
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
	if err := prepareV7WaveMembersForDefaultAdvance(repoRoot, vaultPath, defaultBranch, wave); err != nil {
		return err
	}
	if err := advanceV7DefaultBranch(repoRoot, defaultBranch, mergeCommit, mainRev); err != nil {
		return err
	}
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

func prepareV7WaveMembersForDefaultAdvance(repoRoot, vaultPath, defaultBranch string, wave Note) error {
	relVault, err := filepath.Rel(repoRoot, vaultPath)
	if err != nil || filepath.IsAbs(relVault) || strings.HasPrefix(filepath.Clean(relVault), "..") {
		return tuskerError(errorInvalidTransition, "cannot prepare wave members for default-branch advance: invalid vault path "+vaultPath)
	}
	paths := make([]string, 0, len(normalizeList(wave.Data["members"])))
	for _, member := range normalizeList(wave.Data["members"]) {
		paths = append(paths, filepath.ToSlash(filepath.Join(relVault, "work", "tasks", member+".md")))
	}
	paths = append(paths, filepath.ToSlash(filepath.Join(relVault, "work", "waves", stringField(wave.Data, "id")+".md")))
	for _, wt := range v7DefaultBranchCheckouts(repoRoot, defaultBranch) {
		tracked := make([]string, 0, len(paths))
		for _, path := range paths {
			if exec.Command("git", "-C", wt.Path, "ls-files", "--error-unmatch", "--", path).Run() == nil {
				tracked = append(tracked, path)
			}
		}
		if len(tracked) == 0 {
			continue
		}
		args := append([]string{"checkout", "--"}, tracked...)
		if output, err := gitCombined(wt.Path, args...); err != nil {
			return tuskerError(errorInvalidTransition, "failed to reset integrated wave task projections before default-branch advance: "+firstActionableLine(output, err.Error()), withPath(wt.Path))
		}
	}
	return nil
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
	checkouts := v7DefaultBranchCheckouts(repoRoot, defaultBranch)
	if len(checkouts) == 0 {
		return updateGitRef(repoRoot, "refs/heads/"+defaultBranch, newRev, oldRev)
	}
	for _, wt := range checkouts {
		if err := prepareV7GeneratedStateForDefaultAdvance(wt.Path); err != nil {
			return err
		}
		if err := prepareV7IdenticalUntrackedStateForDefaultAdvance(wt.Path, newRev); err != nil {
			return err
		}
		dirty, err := inPlaceDirtyPaths(wt.Path)
		if err != nil {
			return err
		}
		if len(dirty) > 0 {
			return tuskerError(errorInvalidTransition, defaultBranch+" is checked out in "+wt.Path+" with dirty paths: "+strings.Join(limitStrings(dirty, 5), ", ")+". Commit, stash, or clean those paths before running tusker land.", withPath(wt.Path))
		}
	}
	currentRev, err := gitOutputTrim(repoRoot, "rev-parse", "refs/heads/"+defaultBranch)
	if err != nil {
		return err
	}
	if strings.TrimSpace(currentRev) != strings.TrimSpace(oldRev) {
		return tuskerError(errorInvalidTransition, defaultBranch+" changed while preparing wave land; retry tusker land so the default-branch advance can be checked against the current ref")
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
			return tuskerError(errorInvalidTransition, "failed to advance checked-out "+defaultBranch+" worktree at "+wt.Path+": "+firstActionableLine(output, err.Error())+"; local status: "+paths, withPath(wt.Path))
		}
		head, err := gitOutputTrim(wt.Path, "rev-parse", "HEAD")
		if err != nil {
			return err
		}
		if strings.TrimSpace(head) != strings.TrimSpace(newRev) {
			return tuskerError(errorInvalidTransition, "checked-out "+defaultBranch+" worktree did not advance to "+shortCommit(newRev), withPath(wt.Path))
		}
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
	Result       GateTierResult
	ArtifactRef  string
	ArtifactRefs []string
	Err          error
}

func runV7GateTierOnRef(vaultPath, repoRoot, ref, projectID string, policy GateTierPolicy, store *RuntimeStore) promotionGateExecution {
	return runV7GateTierOnRefContext(context.Background(), vaultPath, repoRoot, ref, projectID, policy, store)
}

func runV7GateTierOnRefContext(ctx context.Context, vaultPath, repoRoot, ref, projectID string, policy GateTierPolicy, store *RuntimeStore) promotionGateExecution {
	if ctx == nil {
		ctx = context.Background()
	}
	writeFailure := func(detail string) promotionGateExecution {
		root := DefaultStateRoot()
		if store != nil && store.stateRoot != "" {
			root = store.stateRoot
		}
		path := filepath.Join(root, "artifacts", "promotion-gates", strings.ToLower(newRecordID())+".log")
		_ = os.MkdirAll(filepath.Dir(path), 0o755)
		_ = os.WriteFile(path, []byte(safePacketText(detail, 4096)+"\n"), 0o600)
		return promotionGateExecution{ArtifactRef: path, ArtifactRefs: []string{path}, Err: fmt.Errorf("%s", detail)}
	}
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
	runtime := defaultGateTierRuntimeWithContext(ctx, store, projectID, tmp)
	var raw bytes.Buffer
	execGate := runtime.Exec
	runtime.Exec = func(workspace, command string) (string, error) {
		output, runErr := execGate(workspace, command)
		fmt.Fprintf(&raw, "$ %s\n%s\n", command, output)
		return output, runErr
	}
	result, err := runGateTier(policy, policy.Profile, runtime)
	root := DefaultStateRoot()
	if store != nil && store.stateRoot != "" {
		root = store.stateRoot
	}
	path := filepath.Join(root, "artifacts", "promotion-gates", strings.ToLower(newRecordID())+".log")
	if writeErr := os.MkdirAll(filepath.Dir(path), 0o755); writeErr != nil {
		if err == nil {
			err = writeErr
		}
	} else if writeErr = os.WriteFile(path, raw.Bytes(), 0o600); writeErr != nil && err == nil {
		err = writeErr
	}
	return promotionGateExecution{Result: result, ArtifactRef: path, ArtifactRefs: []string{path}, Err: err}
}

func appendV7WaveLandingAudit(vaultPath, waveID string, entries []v7LandingAuditEntry, actor string) error {
	if len(entries) == 0 {
		return nil
	}
	note, err := resolveV7Note(vaultPath, waveID, "wave")
	if err != nil {
		return err
	}
	data, body, err := parseFrontmatterMustRead(note.AbsolutePath)
	if err != nil {
		return err
	}
	baseRev := stringField(data, "state_rev")
	landings := normalizeLandingAudit(data["landings"])
	seen := map[string]bool{}
	for _, row := range landings {
		key := fmt.Sprintf("%s|%s|%s|%s|%s", stringField(row, "task"), stringField(row, "branch"), stringField(row, "source_sha"), stringField(row, "target"), stringField(row, "commit"))
		seen[key] = true
	}
	for _, entry := range entries {
		key := fmt.Sprintf("%s|%s|%s|%s|%s", entry.Task, entry.Branch, entry.SourceSHA, entry.Target, entry.Commit)
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
		if entry.Commit != "" {
			row["commit"] = entry.Commit
		}
		landings = append(landings, row)
		seen[key] = true
	}
	if len(landings) == len(normalizeLandingAudit(data["landings"])) {
		return nil
	}
	data["landings"] = landings
	data["updated_at"] = time.Now().UTC().Format(time.RFC3339)
	data["updated_by"] = actor
	nextRev, err := saveV7DocumentCAS(note.AbsolutePath, data, body, v7FrontmatterOrder["wave"], baseRev)
	if err != nil {
		return err
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
	return v7TaskBranchName(taskID), v7WaveIntegrationBranch(wave), nil
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
