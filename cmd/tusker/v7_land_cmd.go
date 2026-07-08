package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const tuskerLandMainGuardEnv = "TUSKER_LAND_MAIN_OK"

type v7LandTask struct {
	ID     string
	Branch string
}

type v7LandingAuditEntry struct {
	Task        string
	Branch      string
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
	} else if err := landV7TaskTargets(vaultPath, targets, args, summary); err != nil {
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
	file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		if os.IsExist(err) {
			return nil, tuskerError(errorInvalidTransition, "landing lane is already running", withPath(lockPath))
		}
		return nil, err
	}
	_, _ = fmt.Fprintf(file, "%s\n", time.Now().UTC().Format(time.RFC3339))
	_ = file.Close()
	return func() { _ = os.Remove(lockPath) }, nil
}

func landV7TaskTargets(vaultPath string, targets []string, args Args, summary *v7LandSummary) error {
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
			return tuskerError(errorInvalidTransition, v7NoWaveRefusal(taskID))
		}
		if _, ok := idx.Waves[waveID]; !ok {
			return tuskerError(errorNotFound, "V7 wave not found: "+waveID)
		}
		branch := v7TaskBranchName(taskID)
		if len(targets) == 1 {
			branch = firstNonEmpty(strings.TrimSpace(args.String("branch")), branch)
		}
		if v7GitRepo(repoRoot) && !gitBranchExists(repoRoot, branch) {
			if err := ensureV7TaskLandingBranch(repoRoot, taskID, branch, args); err != nil {
				return err
			}
		}
		byWave[waveID] = append(byWave[waveID], v7LandTask{ID: taskID, Branch: branch})
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
				Target: v7IntegrationBranchName(waveID), GateResult: "fail",
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
		commit, err := resolveV7LandingRef(repoRoot, from)
		if err != nil {
			return "", "", tuskerError(errorInvalidArg, "cannot resolve --from "+from+" for "+taskID+": "+firstActionableLine(err.Error(), err.Error()))
		}
		return commit, from, nil
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

func resolveV7LandingRef(repoRoot, ref string) (string, error) {
	if info, err := os.Stat(ref); err == nil && info.IsDir() {
		if commit, err := gitOutputTrim(ref, "rev-parse", "HEAD"); err == nil {
			return commit, nil
		}
	}
	return gitOutputTrim(repoRoot, "rev-parse", ref+"^{commit}")
}

type v7Worktree struct {
	Path     string
	HEAD     string
	Detached bool
}

// v7DetachedWorktreesForTask returns detached git worktrees that look like the
// runner's completed workspace for taskID, matched either by the workspace
// metadata record id or by the task id appearing in the worktree path.
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
		if v7WorkspaceRecordID(wt.Path) == upperID || strings.Contains(strings.ToUpper(abs), upperID) {
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
				Task: task.ID, Branch: task.Branch, Target: integrationBranch,
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
		if output, err := gitCombined(tmp, "merge", "--no-ff", "--no-edit", task.Branch); err != nil {
			return false, "", landingFailureSummary("merge "+task.Branch, output, err), nil
		}
		if err := guardV7LandingTerminalTaskRewinds(tmp, baseBranch); err != nil {
			return false, "", "", err
		}
	}
	pass, summary := runV7LandingGate(vaultPath, tmp)
	if !pass {
		return false, "", summary, nil
	}
	commit, err := gitOutputTrim(tmp, "rev-parse", "HEAD")
	if err != nil {
		return false, "", "", err
	}
	return true, commit, summary, nil
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

func runV7LandingGate(vaultPath, workDir string) (bool, string) {
	commands := backpressureCommands(vaultPath)
	if len(commands) == 0 {
		commands = []string{"go build ./...", "go vet ./...", "go test ./... -count=1"}
	}
	for _, command := range commands {
		cmd := exec.Command("sh", "-c", command)
		cmd.Dir = workDir
		output, err := cmd.CombinedOutput()
		if err != nil {
			return false, landingFailureSummary(command, string(output), err)
		}
	}
	return true, "gate passed: " + strings.Join(commands, " && ")
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
	open := 0
	for _, member := range normalizeList(wave.Data["members"]) {
		task, ok := idx.Tasks[member]
		if !ok || stringField(task.Data, "status") != "done" {
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
	for _, member := range normalizeList(wave.Data["members"]) {
		task, ok := idx.Tasks[member]
		if !ok || stringField(task.Data, "status") != "done" {
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
		if err := deleteGitBranch(repoRoot, integrationBranch); err != nil {
			return err
		}
		actor := landV7Actor(args)
		summary.MainNotes = append(summary.MainNotes, "main: "+waveID+" already landed at "+shortCommit(mainRev)+"; cleaned up integration branch")
		return appendV7WaveLandingAudit(vaultPath, waveID, []v7LandingAuditEntry{{
			Task: "wave", Branch: integrationBranch, Target: defaultBranch,
			GateResult: "pass", GateSummary: "already landed; cleaned up integration branch",
			Commit: mainRev, Actor: actor, Timestamp: time.Now().UTC().Format(time.RFC3339),
		}}, actor)
	}
	if !gitMergeBaseAncestor(repoRoot, defaultBranch, integrationBranch) {
		return tuskerError(errorInvalidTransition, defaultBranch+" is not an ancestor of "+integrationBranch+"; rebase or merge main before wave landing")
	}
	pass, summaryText := runV7LandingGateOnRef(vaultPath, repoRoot, integrationBranch)
	if !pass {
		return tuskerError(errorInvalidTransition, waveID+" integration branch is red: "+summaryText)
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
	if err := updateGitRef(repoRoot, "refs/heads/"+defaultBranch, mergeCommit, mainRev); err != nil {
		return err
	}
	if err := deleteGitBranch(repoRoot, integrationBranch); err != nil {
		return err
	}
	actor := landV7Actor(args)
	summary.MainNotes = append(summary.MainNotes, "main: moved to "+shortCommit(mergeCommit)+" ("+message+")")
	return appendV7WaveLandingAudit(vaultPath, waveID, []v7LandingAuditEntry{{
		Task: "wave", Branch: integrationBranch, Target: defaultBranch,
		GateResult: "pass", GateSummary: summaryText, Commit: mergeCommit,
		Actor: actor, Timestamp: time.Now().UTC().Format(time.RFC3339),
	}}, actor)
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
	if len(summary.Landed) > 0 {
		b.WriteString(fmt.Sprintf("Landed %d task%s:\n", len(summary.Landed), plural(len(summary.Landed))))
		for _, row := range summary.Landed {
			b.WriteString(fmt.Sprintf("  %s  %s -> %s  gate:%s  %s\n", row.Task, row.Branch, row.Target, row.GateResult, shortCommit(row.Commit)))
		}
	}
	if len(summary.Reworked) > 0 {
		b.WriteString(fmt.Sprintf("Returned %d task%s to rework:\n", len(summary.Reworked), plural(len(summary.Reworked))))
		for _, row := range summary.Reworked {
			b.WriteString(fmt.Sprintf("  %s  %s -> %s  gate:%s\n", row.Task, row.Branch, row.Target, row.GateResult))
		}
	}
	for _, note := range summary.MainNotes {
		b.WriteString(note + "\n")
	}
	fmt.Print(b.String())
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
	return runV7LandingGate(vaultPath, tmp)
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
		key := fmt.Sprintf("%s|%s|%s|%s", stringField(row, "task"), stringField(row, "branch"), stringField(row, "target"), stringField(row, "commit"))
		seen[key] = true
	}
	for _, entry := range entries {
		key := fmt.Sprintf("%s|%s|%s|%s", entry.Task, entry.Branch, entry.Target, entry.Commit)
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
