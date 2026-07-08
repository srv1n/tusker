package main

import (
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

	if len(targets) == 1 && v7WaveIDPattern.MatchString(targets[0]) {
		return landV7WaveToMain(vaultPath, targets[0], args)
	}
	return landV7TaskTargets(vaultPath, targets, args)
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

func landV7TaskTargets(vaultPath string, targets []string, args Args) error {
	idx, err := loadV7Index(vaultPath)
	if err != nil {
		return err
	}
	byWave := map[string][]v7LandTask{}
	for _, taskID := range targets {
		task, ok := idx.Tasks[taskID]
		if !ok {
			return tuskerError(errorNotFound, "V7 task not found: "+taskID)
		}
		waveID := stringField(task.Data, "wave")
		if waveID == "" {
			return tuskerError(errorInvalidTransition, taskID+" is not in a wave; merge-lane land requires wave membership")
		}
		if _, ok := idx.Waves[waveID]; !ok {
			return tuskerError(errorNotFound, "V7 wave not found: "+waveID)
		}
		branch := v7TaskBranchName(taskID)
		if len(targets) == 1 {
			branch = firstNonEmpty(strings.TrimSpace(args.String("branch")), branch)
		}
		byWave[waveID] = append(byWave[waveID], v7LandTask{ID: taskID, Branch: branch})
	}
	waveIDs := make([]string, 0, len(byWave))
	for waveID := range byWave {
		waveIDs = append(waveIDs, waveID)
	}
	sort.Strings(waveIDs)
	for _, waveID := range waveIDs {
		if err := landV7WaveTaskBatch(vaultPath, waveID, byWave[waveID], args); err != nil {
			return err
		}
		if err := landV7WaveToMainIfReady(vaultPath, waveID, args); err != nil {
			return err
		}
	}
	return nil
}

func landV7WaveTaskBatch(vaultPath, waveID string, tasks []v7LandTask, args Args) error {
	idx, err := loadV7Index(vaultPath)
	if err != nil {
		return err
	}
	wave, ok := idx.Waves[waveID]
	if !ok {
		return tuskerError(errorNotFound, "V7 wave not found: "+waveID)
	}
	repoRoot := v7RepoRoot(vaultPath)
	if !v7GitRepo(repoRoot) {
		return tuskerError(errorInvalidTransition, "tusker land requires a Git repository", withPath(repoRoot))
	}
	integrationBranch := v7WaveIntegrationBranch(wave)
	if err := ensureV7IntegrationBranch(vaultPath, integrationBranch); err != nil {
		return err
	}
	return landV7BatchRecursive(vaultPath, repoRoot, waveID, integrationBranch, tasks, args)
}

func landV7BatchRecursive(vaultPath, repoRoot, waveID, integrationBranch string, tasks []v7LandTask, args Args) error {
	if len(tasks) == 0 {
		return nil
	}
	pass, commit, summary, err := stageV7LandingBatch(vaultPath, repoRoot, integrationBranch, tasks)
	if err != nil {
		return err
	}
	actor := fallback(fallback(args.String("actor"), args.String("by")), "agent:"+defaultActorName())
	if pass {
		if err := updateGitRef(repoRoot, "refs/heads/"+integrationBranch, commit, ""); err != nil {
			return err
		}
		entries := make([]v7LandingAuditEntry, 0, len(tasks))
		for _, task := range tasks {
			entries = append(entries, v7LandingAuditEntry{
				Task: task.ID, Branch: task.Branch, Target: integrationBranch,
				GateResult: "pass", GateSummary: summary, Commit: commit,
				Actor: actor, Timestamp: time.Now().UTC().Format(time.RFC3339),
			})
		}
		return appendV7WaveLandingAudit(vaultPath, waveID, entries, actor)
	}
	if len(tasks) == 1 {
		task := tasks[0]
		if err := kickV7LandingTaskToRework(vaultPath, task.ID, summary, actor); err != nil {
			return err
		}
		return appendV7WaveLandingAudit(vaultPath, waveID, []v7LandingAuditEntry{{
			Task: task.ID, Branch: task.Branch, Target: integrationBranch,
			GateResult: "fail", GateSummary: summary, Actor: actor,
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		}}, actor)
	}
	mid := len(tasks) / 2
	if err := landV7BatchRecursive(vaultPath, repoRoot, waveID, integrationBranch, tasks[:mid], args); err != nil {
		return err
	}
	return landV7BatchRecursive(vaultPath, repoRoot, waveID, integrationBranch, tasks[mid:], args)
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

func firstActionableLine(output, fallbackLine string) string {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}
	return strings.TrimSpace(fallbackLine)
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

func landV7WaveToMainIfReady(vaultPath, waveID string, args Args) error {
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
			return nil
		}
	}
	return landV7WaveToMain(vaultPath, waveID, args)
}

func landV7WaveToMain(vaultPath, waveID string, args Args) error {
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
		actor := fallback(fallback(args.String("actor"), args.String("by")), "agent:"+defaultActorName())
		return appendV7WaveLandingAudit(vaultPath, waveID, []v7LandingAuditEntry{{
			Task: "wave", Branch: integrationBranch, Target: defaultBranch,
			GateResult: "pass", GateSummary: "already landed; cleaned up integration branch",
			Commit: mainRev, Actor: actor, Timestamp: time.Now().UTC().Format(time.RFC3339),
		}}, actor)
	}
	if !gitMergeBaseAncestor(repoRoot, defaultBranch, integrationBranch) {
		return tuskerError(errorInvalidTransition, defaultBranch+" is not an ancestor of "+integrationBranch+"; rebase or merge main before wave landing")
	}
	pass, summary := runV7LandingGateOnRef(vaultPath, repoRoot, integrationBranch)
	if !pass {
		return tuskerError(errorInvalidTransition, waveID+" integration branch is red: "+summary)
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
	actor := fallback(fallback(args.String("actor"), args.String("by")), "agent:"+defaultActorName())
	return appendV7WaveLandingAudit(vaultPath, waveID, []v7LandingAuditEntry{{
		Task: "wave", Branch: integrationBranch, Target: defaultBranch,
		GateResult: "pass", GateSummary: summary, Commit: mergeCommit,
		Actor: actor, Timestamp: time.Now().UTC().Format(time.RFC3339),
	}}, actor)
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
