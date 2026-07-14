package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const externalCollectSchema = "tusker.external_collect/v1"

type externalCollectReport struct {
	Schema           string                `json:"schema"`
	TaskID           string                `json:"task_id"`
	RecordID         string                `json:"record_id"`
	Runner           string                `json:"runner"`
	JobID            string                `json:"job_id"`
	ArtifactDir      string                `json:"artifact_dir"`
	Patches          []string              `json:"patches"`
	ReviewPackets    []string              `json:"review_packets"`
	ReviewResult     *externalReviewResult `json:"review_result,omitempty"`
	Bundles          []string              `json:"bundles"`
	RuntimeArtifacts []string              `json:"runtime_artifacts"`
	ApplyInputs      []RuntimeApplyInput   `json:"apply_inputs"`
	EvidenceAdded    []string              `json:"evidence_added"`
	EvidenceExisting []string              `json:"evidence_existing,omitempty"`
	NextAction       string                `json:"next_action"`
	Dispatchable     bool                  `json:"dispatchable"`
	Blockers         []string              `json:"blockers,omitempty"`
}

type externalReviewResult struct {
	Kind     string   `json:"kind,omitempty"`
	Verdict  string   `json:"verdict,omitempty"`
	Risk     string   `json:"risk,omitempty"`
	Summary  string   `json:"summary,omitempty"`
	Findings []string `json:"findings,omitempty"`
}

type externalFetchResult struct {
	JobID       string
	ChatID      string
	ArtifactDir string
	Files       []string
	Raw         map[string]any
}

type externalArtifact struct {
	Path    string
	RelPath string
	Kind    string
	Sha256  string
}

type externalCollectFetcher func(ctx context.Context, req externalFetchRequest) (externalFetchResult, error)

type externalFetchRequest struct {
	RepoRoot string
	JobID    string
	Runner   string
	OutDir   string
	Command  string
}

var runExternalCollectFetch externalCollectFetcher = defaultExternalCollectFetch

func automationCollectExternalCmd(args Args) error {
	taskID, err := requireArg(args, "id")
	if err != nil {
		return err
	}
	ctx, err := loadAutomationCommandContext(args)
	if err != nil {
		return err
	}
	defer ctx.Close()
	note, err := ctx.findTask(taskID)
	if err != nil {
		return err
	}
	runner := firstNonEmpty(strings.TrimSpace(args.String("runner")), "chatgpt-browser")
	run := ctx.effectiveRunForTask(note, runner)
	jobID := firstNonEmpty(strings.TrimSpace(args.String("job")), strings.TrimSpace(args.String("cloud-task-id")), run.CloudTaskID)
	if jobID == "" {
		return tuskerError(errorMissingArg, "collect-external requires --job when the task runtime has no cloud_task_id", withContext(map[string]any{"task_id": stringField(note.Data, "id")}))
	}
	report, err := ctx.collectExternal(note, run, runner, jobID, args)
	if err != nil {
		return err
	}
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": len(report.Blockers) == 0 || report.NextAction != "escalate_human", "collection": report})
		return nil
	}
	printExternalCollectReport(report)
	if report.NextAction == "escalate_human" {
		return tuskerError(errorInvalidTransition, stringField(note.Data, "id")+": external collection needs human escalation: "+strings.Join(report.Blockers, "; "), withContext(report))
	}
	return nil
}

func (ctx *automationCommandContext) collectExternal(note Note, run RunStatus, runner, jobID string, args Args) (externalCollectReport, error) {
	taskID := stringField(note.Data, "id")
	recordID := trackerRecordID(note)
	artifactDirAbs := filepath.Join(ctx.Project.RepoRoot, "architect", taskID)
	artifactDirRel := filepath.ToSlash(filepath.Join("architect", taskID))
	fetch, err := runExternalCollectFetch(context.Background(), externalFetchRequest{
		RepoRoot: ctx.Project.RepoRoot,
		JobID:    jobID,
		Runner:   runner,
		OutDir:   artifactDirAbs,
		Command:  firstNonEmpty(strings.TrimSpace(args.String("transport")), strings.TrimSpace(args.String("fetch-command"))),
	})
	if err != nil {
		return externalCollectReport{}, err
	}
	if err := ensureDir(artifactDirAbs); err != nil {
		return externalCollectReport{}, err
	}
	files, err := normalizeExternalArtifacts(ctx.Project.RepoRoot, artifactDirAbs, fetch)
	if err != nil {
		return externalCollectReport{}, err
	}
	classified, err := classifyExternalArtifacts(ctx.Project.RepoRoot, files)
	if err != nil {
		return externalCollectReport{}, err
	}
	reviewResult := externalReviewResultFromArtifacts(classified)
	report := externalCollectReport{
		Schema:       externalCollectSchema,
		TaskID:       taskID,
		RecordID:     recordID,
		Runner:       runner,
		JobID:        firstNonEmpty(fetch.JobID, jobID),
		ArtifactDir:  artifactDirRel,
		ReviewResult: reviewResult,
		NextAction:   "escalate_human",
	}
	for _, artifact := range classified {
		switch artifact.Kind {
		case "patch":
			report.Patches = append(report.Patches, artifact.RelPath)
		case "review_packet":
			report.ReviewPackets = append(report.ReviewPackets, artifact.RelPath)
		case "bundle":
			report.Bundles = append(report.Bundles, artifact.RelPath)
		default:
			report.RuntimeArtifacts = append(report.RuntimeArtifacts, artifact.RelPath)
		}
	}
	if len(classified) == 0 {
		report.Blockers = append(report.Blockers, "no artifacts fetched for job "+jobID)
	}
	for _, artifact := range classified {
		if artifact.Kind != "review_packet" {
			continue
		}
		evidenceID, added, err := ctx.ensureExternalReviewEvidence(note, artifact, report.JobID, args)
		if err != nil {
			return externalCollectReport{}, err
		}
		if added {
			report.EvidenceAdded = append(report.EvidenceAdded, evidenceID)
		} else if evidenceID != "" {
			report.EvidenceExisting = append(report.EvidenceExisting, evidenceID)
		}
	}
	for _, artifact := range classified {
		if artifact.Kind != "patch" {
			continue
		}
		input := RuntimeApplyInput{
			ProjectID: ctx.Project.ProjectID,
			RecordID:  recordID,
			ItemID:    taskID,
			Runner:    runner,
			JobID:     report.JobID,
			AttemptID: run.ActiveAttemptID,
			Path:      artifact.Path,
			RelPath:   artifact.RelPath,
			Sha256:    artifact.Sha256,
			Kind:      "patch",
		}
		stored, err := ctx.Store.UpsertApplyInput(input)
		if err != nil {
			return externalCollectReport{}, err
		}
		report.ApplyInputs = append(report.ApplyInputs, stored)
	}
	if len(report.Patches) == 1 {
		applyRunner := firstNonEmpty(strings.TrimSpace(args.String("apply-runner")), externalLoopDefaultApplyRunner(ctx.Workflow.Data, runner))
		applyRun := ctx.effectiveRunForTask(note, applyRunner)
		if current, ok := ctx.ProjectRuns[recordID]; ok && externalLoopRunnerRequiresCollect(ctx.Workflow.Data, current.Runner) && LeaseState(strings.TrimSpace(current.LeaseState)) == LeaseStateReleased {
			applyRun = externalLoopApplyDispatchRun(ctx.Project, note, current, applyRunner)
		}
		explanation := ctx.explainTaskForRunner(note, applyRunner, &applyRun)
		report.NextAction = externalLoopActionApplyPatch
		report.Dispatchable = explanation.Dispatchable
		report.Blockers = append(report.Blockers, explanation.Blockers...)
	} else if len(report.Patches) > 1 {
		report.NextAction = externalLoopActionEscalateHuman
		report.Dispatchable = false
		report.Blockers = append(report.Blockers, "multiple patch artifacts require human selection")
	} else if action, blockers := externalReviewResultAction(ctx.Workflow.Data, note, run, report.ReviewResult); action != "" {
		report.NextAction = action
		report.Dispatchable = action == externalLoopActionContinueThreadOnFailure || action == externalLoopActionRequestReviewNext
		report.Blockers = append(report.Blockers, blockers...)
	} else if len(report.ReviewPackets) > 0 || len(report.Bundles) > 0 || len(report.RuntimeArtifacts) > 0 {
		report.NextAction = externalLoopActionRecordResearch
		report.Dispatchable = false
	}
	report.Blockers = uniqueStrings(report.Blockers)
	sort.Strings(report.Patches)
	sort.Strings(report.ReviewPackets)
	sort.Strings(report.Bundles)
	sort.Strings(report.RuntimeArtifacts)
	sort.Strings(report.EvidenceAdded)
	sort.Strings(report.EvidenceExisting)
	return report, nil
}

func defaultExternalCollectFetch(ctx context.Context, req externalFetchRequest) (externalFetchResult, error) {
	jobID := strings.TrimSpace(req.JobID)
	if jobID == "" {
		return externalFetchResult{}, tuskerError(errorMissingArg, "external fetch requires a job id")
	}
	command := strings.TrimSpace(req.Command)
	var cmd *exec.Cmd
	if command == "" {
		cmdArgs := []string{"fetch", jobID, "--json"}
		if strings.TrimSpace(req.OutDir) != "" {
			cmdArgs = append(cmdArgs, "--out-dir", req.OutDir)
		}
		cmd = exec.CommandContext(ctx, "chatgpt-handoff", cmdArgs...)
	} else {
		command = replaceTemplateTokens(command, map[string]string{
			"{{job_id}}":        jobID,
			"{{cloud_task_id}}": jobID,
			"{{out_dir}}":       req.OutDir,
			"{{runner}}":        req.Runner,
		})
		cmd = exec.CommandContext(ctx, "sh", "-lc", command)
	}
	cmd.Dir = firstNonEmpty(req.RepoRoot, ".")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return externalFetchResult{}, fmt.Errorf("external fetch failed: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return parseExternalFetchResult(output, jobID)
}

func parseExternalFetchResult(output []byte, fallbackJobID string) (externalFetchResult, error) {
	var value any
	if err := json.Unmarshal(output, &value); err != nil {
		return externalFetchResult{}, tuskerError(errorConfigInvalid, "external fetch did not return JSON: "+err.Error())
	}
	values, ok := value.(map[string]any)
	if !ok {
		return externalFetchResult{}, tuskerError(errorConfigInvalid, "external fetch JSON must be an object")
	}
	if nested, ok := values["collection"].(map[string]any); ok {
		values = nested
	}
	files := normalizeExternalFetchFiles(values)
	return externalFetchResult{
		JobID:       firstNonEmpty(stringValue(firstPresent(values, "job_id", "job", "id", "cloud_task_id")), fallbackJobID),
		ChatID:      stringValue(firstPresent(values, "chat_id", "chatId")),
		ArtifactDir: stringValue(firstPresent(values, "artifact_dir", "architect_dir", "artifacts_dir", "out_dir")),
		Files:       files,
		Raw:         values,
	}, nil
}

func normalizeExternalFetchFiles(values map[string]any) []string {
	files := normalizeList(firstPresent(values, "files", "artifacts", "result_paths"))
	for _, key := range []string{"patch_path", "review_path", "notes_path", "apply_ref"} {
		if value := strings.TrimSpace(stringValue(values[key])); value != "" {
			files = append(files, value)
		}
	}
	for _, parent := range []string{"job", "result", "data"} {
		nested, ok := values[parent].(map[string]any)
		if !ok {
			continue
		}
		files = append(files, normalizeExternalFetchFiles(nested)...)
	}
	return uniqueStrings(files)
}

var externalJSONFenceRE = regexp.MustCompile("(?is)```(?:json)?\\s*\\n?([\\s\\S]*?)\\n?```")

func externalReviewResultFromArtifacts(artifacts []externalArtifact) *externalReviewResult {
	for _, artifact := range artifacts {
		if artifact.Kind != "review_packet" {
			continue
		}
		text, err := readText(artifact.Path)
		if err != nil {
			continue
		}
		if result, ok := parseExternalReviewResult(text); ok {
			return &result
		}
	}
	return nil
}

func parseExternalReviewResult(text string) (externalReviewResult, bool) {
	var candidates []string
	trimmed := strings.TrimSpace(text)
	if strings.HasPrefix(trimmed, "{") && strings.HasSuffix(trimmed, "}") {
		candidates = append(candidates, trimmed)
	}
	for _, match := range externalJSONFenceRE.FindAllStringSubmatch(text, -1) {
		if len(match) > 1 {
			candidates = append(candidates, strings.TrimSpace(match[1]))
		}
	}
	for _, candidate := range candidates {
		var values map[string]any
		if err := json.Unmarshal([]byte(candidate), &values); err != nil {
			continue
		}
		kind := strings.ToLower(strings.TrimSpace(stringValue(values["kind"])))
		verdict := strings.TrimSpace(firstNonEmpty(stringValue(values["verdict"]), stringValue(values["status"]), stringValue(values["result"])))
		if verdict == "" {
			continue
		}
		if kind != "" && kind != "review" && kind != "architect" && !externalReviewVerdictKnown(verdict) {
			continue
		}
		result := externalReviewResult{
			Kind:    firstNonEmpty(kind, "review"),
			Verdict: verdict,
			Risk:    strings.TrimSpace(stringValue(values["risk"])),
			Summary: strings.TrimSpace(stringValue(values["summary"])),
		}
		for _, item := range normalizeList(values["findings"]) {
			result.Findings = append(result.Findings, item)
		}
		return result, true
	}
	return externalReviewResult{}, false
}

func externalReviewVerdictKnown(verdict string) bool {
	switch externalReviewVerdictClass(verdict) {
	case "accepted", "rework", "blocked":
		return true
	default:
		return false
	}
}

func externalReviewVerdictClass(verdict string) string {
	value := strings.ToLower(strings.TrimSpace(verdict))
	value = strings.ReplaceAll(value, "-", "_")
	value = strings.ReplaceAll(value, " ", "_")
	switch value {
	case "approve", "approved", "approve_with_nits", "accepted", "accept", "pass", "passed", "ok", "success", "succeeded":
		return "accepted"
	case "request_changes", "changes_requested", "request_change", "rework", "rejected", "reject", "fail", "failed", "needs_work":
		return "rework"
	case "blocked", "needs_input", "needs_info", "insufficient_context":
		return "blocked"
	default:
		return ""
	}
}

func externalReviewResultAction(wf Workflow, note Note, run RunStatus, result *externalReviewResult) (string, []string) {
	if result == nil || firstNonEmpty(strings.TrimSpace(run.Lane), runLaneExecute) != runLaneReview {
		return "", nil
	}
	switch externalReviewVerdictClass(result.Verdict) {
	case "accepted":
		risk := strings.ToLower(strings.TrimSpace(firstNonEmpty(result.Risk, stringField(note.Data, "risk"))))
		if reviewerMayAutoCloseRisk(wf.Reviewer, risk) {
			return externalLoopActionCloseTask, nil
		}
		return externalLoopActionRecordResearch, []string{"external review accepted, but reviewer auto-close is not configured for risk " + firstNonEmpty(risk, "unknown")}
	case "rework":
		return externalLoopActionContinueThreadOnFailure, nil
	case "blocked":
		return externalLoopActionEscalateHuman, []string{"external review returned blocked verdict"}
	default:
		return externalLoopActionRecordResearch, nil
	}
}

func firstPresent(values map[string]any, keys ...string) any {
	for _, key := range keys {
		if value, ok := values[key]; ok {
			return value
		}
	}
	return nil
}

func normalizeExternalArtifacts(repoRoot, destDir string, fetch externalFetchResult) ([]string, error) {
	sourceDir := strings.TrimSpace(fetch.ArtifactDir)
	if sourceDir != "" && !filepath.IsAbs(sourceDir) {
		sourceDir = filepath.Join(repoRoot, filepath.FromSlash(sourceDir))
	}
	var candidates []string
	if sourceDir != "" && dirExists(sourceDir) {
		if err := filepath.WalkDir(sourceDir, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				return nil
			}
			candidates = append(candidates, path)
			return nil
		}); err != nil {
			return nil, err
		}
	}
	for _, file := range fetch.Files {
		file = strings.TrimSpace(file)
		if file == "" {
			continue
		}
		path := file
		if !filepath.IsAbs(path) {
			if sourceDir != "" {
				path = filepath.Join(sourceDir, filepath.FromSlash(file))
			} else {
				path = filepath.Join(repoRoot, filepath.FromSlash(file))
			}
		}
		if fileExists(path) {
			candidates = append(candidates, path)
		}
	}
	candidates = uniqueStrings(candidates)
	var normalized []string
	for _, source := range candidates {
		if source == "" || !fileExists(source) {
			continue
		}
		if samePathOrChild(source, destDir) {
			normalized = append(normalized, source)
			continue
		}
		target, err := copyExternalArtifact(source, destDir)
		if err != nil {
			return nil, err
		}
		normalized = append(normalized, target)
	}
	sort.Strings(normalized)
	return uniqueStrings(normalized), nil
}

func samePathOrChild(path, dir string) bool {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(absDir, absPath)
	if err != nil {
		return false
	}
	return rel == "." || (!strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != ".." && !filepath.IsAbs(rel))
}

func copyExternalArtifact(source, destDir string) (string, error) {
	base := filepath.Base(source)
	target := filepath.Join(destDir, base)
	sourceHash, err := sha256Path(source)
	if err != nil {
		return "", err
	}
	if fileExists(target) {
		targetHash, err := sha256Path(target)
		if err == nil && targetHash == sourceHash {
			return target, nil
		}
		ext := filepath.Ext(base)
		stem := strings.TrimSuffix(base, ext)
		for i := 0; i < 100; i++ {
			candidate := filepath.Join(destDir, fmt.Sprintf("%s-%s%s", stem, sourceHash[:12], ext))
			if i > 0 {
				candidate = filepath.Join(destDir, fmt.Sprintf("%s-%s-%d%s", stem, sourceHash[:12], i+1, ext))
			}
			if !fileExists(candidate) {
				target = candidate
				break
			}
			candidateHash, err := sha256Path(candidate)
			if err == nil && candidateHash == sourceHash {
				return candidate, nil
			}
		}
	}
	return target, copyFile(source, target)
}

func classifyExternalArtifacts(repoRoot string, paths []string) ([]externalArtifact, error) {
	var out []externalArtifact
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			return nil, tuskerError(errorNotFound, "external artifact unreadable: "+path)
		}
		if info.IsDir() {
			continue
		}
		hash, err := sha256Path(path)
		if err != nil {
			return nil, err
		}
		rel := path
		if repoRoot != "" {
			if computed, err := filepath.Rel(repoRoot, path); err == nil && !strings.HasPrefix(computed, "..") && !filepath.IsAbs(computed) {
				rel = computed
			}
		}
		kind := classifyExternalArtifactPath(path)
		if kind == "patch" && info.Size() == 0 {
			return nil, tuskerError(errorInvalidArg, "patch artifact is empty: "+rel)
		}
		out = append(out, externalArtifact{Path: path, RelPath: filepath.ToSlash(rel), Kind: kind, Sha256: hash})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].RelPath < out[j].RelPath })
	return out, nil
}

func classifyExternalArtifactPath(path string) string {
	base := strings.ToLower(filepath.Base(path))
	ext := strings.ToLower(filepath.Ext(base))
	if ext == ".patch" || ext == ".diff" {
		return "patch"
	}
	if strings.Contains(base, "transcript") || ext == ".json" || ext == ".jsonl" {
		return "runtime"
	}
	if ext == ".md" || ext == ".txt" {
		return "review_packet"
	}
	switch ext {
	case ".zip", ".tgz", ".gz", ".tar", ".bz2", ".xz":
		return "bundle"
	default:
		return "runtime"
	}
}

func (ctx *automationCommandContext) ensureExternalReviewEvidence(note Note, artifact externalArtifact, jobID string, args Args) (string, bool, error) {
	taskID := stringField(note.Data, "id")
	if taskID == "" {
		return "", false, tuskerError(errorInvalidArg, "task id is missing")
	}
	if existing := findExternalReviewEvidence(ctx.Project.VaultRoot, taskID, jobID, artifact.Sha256); existing != "" {
		return existing, false, nil
	}
	evidenceID := fmt.Sprintf("%s-E-%s", taskID, padNumber(nextV7EvidenceSequence(ctx.Project.VaultRoot, taskID)))
	covers := externalCollectCovers(note, args)
	relPath, err := filepath.Rel(ctx.Project.RepoRoot, artifact.Path)
	if err != nil || strings.HasPrefix(relPath, "..") || filepath.IsAbs(relPath) {
		return "", false, tuskerError(errorPathEscape, "review packet artifact is outside repo root: "+artifact.Path)
	}
	summary := fmt.Sprintf("ChatGPT Pro handoff notes collected for %s. external_job=%s artifact_sha256=%s", jobID, jobID, artifact.Sha256)
	cwd, _ := os.Getwd()
	if ctx.Project.RepoRoot != "" {
		if err := os.Chdir(ctx.Project.RepoRoot); err != nil {
			return "", false, err
		}
		defer func() { _ = os.Chdir(cwd) }()
	}
	err = evidenceV7AddCmd(Args{
		"vault":       ctx.Project.VaultRoot,
		"quiet":       "true",
		"id":          taskID,
		"evidence-id": evidenceID,
		"kind":        "review_packet",
		"covers":      strings.Join(covers, ","),
		"summary":     summary,
		"path":        filepath.ToSlash(relPath),
		"by":          "agent:" + firstNonEmpty(strings.TrimSpace(args.String("runner")), "chatgpt-browser"),
	})
	if err != nil {
		return "", false, err
	}
	return evidenceID, true, nil
}

func externalCollectCovers(note Note, args Args) []string {
	covers := normalizeV7Covers(splitCSV(args.String("covers")))
	if len(covers) > 0 {
		return covers
	}
	ids := v7AcceptanceIDs(note.Body)
	if len(ids) == 0 {
		return []string{"TASK:ALL"}
	}
	return normalizeV7Covers(ids)
}

func findExternalReviewEvidence(vaultPath, taskID, jobID, sha string) string {
	dir := filepath.Join(vaultPath, "evidence", taskID)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		data, body, err := parseFrontmatterMustRead(path)
		if err != nil {
			continue
		}
		if stringField(data, "evidence_kind") != "review_packet" {
			continue
		}
		if strings.Contains(body, "external_job="+jobID) && strings.Contains(body, "artifact_sha256="+sha) {
			return stringField(data, "id")
		}
	}
	return ""
}

func sha256Path(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func printExternalCollectReport(report externalCollectReport) {
	fmt.Printf("%s external collection: %s\n", report.TaskID, report.NextAction)
	fmt.Printf("  runner=%s job=%s artifacts=%s\n", report.Runner, report.JobID, report.ArtifactDir)
	if len(report.Patches) > 0 {
		fmt.Printf("  patches=%s\n", strings.Join(report.Patches, ", "))
	}
	if len(report.ReviewPackets) > 0 {
		fmt.Printf("  review_packets=%s\n", strings.Join(report.ReviewPackets, ", "))
	}
	if len(report.Bundles) > 0 {
		fmt.Printf("  bundles=%s\n", strings.Join(report.Bundles, ", "))
	}
	if len(report.EvidenceAdded) > 0 {
		fmt.Printf("  evidence_added=%s\n", strings.Join(report.EvidenceAdded, ", "))
	}
	if len(report.EvidenceExisting) > 0 {
		fmt.Printf("  evidence_existing=%s\n", strings.Join(report.EvidenceExisting, ", "))
	}
	fmt.Printf("  dispatchable=%t\n", report.Dispatchable)
	if len(report.Blockers) == 0 {
		fmt.Println("  blockers=none")
		return
	}
	fmt.Println("  blockers:")
	for _, blocker := range report.Blockers {
		fmt.Println("    - " + blocker)
	}
}

func mirrorApplyInputsIntoWorkspace(store *RuntimeStore, project RegisteredProject, run RunStatus, workspacePath string) error {
	if store == nil || strings.TrimSpace(workspacePath) == "" {
		return nil
	}
	inputs, err := store.ListApplyInputsForRun(project.ProjectID, run.RecordID)
	if err != nil {
		return err
	}
	if len(inputs) == 0 {
		return nil
	}
	seenDirs := map[string]bool{}
	for _, input := range inputs {
		rel := filepath.ToSlash(firstNonEmpty(input.RelPath, input.Path))
		if !strings.HasPrefix(rel, "architect/") {
			continue
		}
		parts := strings.Split(rel, "/")
		if len(parts) < 2 {
			continue
		}
		sourceDir := filepath.Join(project.RepoRoot, "architect", parts[1])
		if seenDirs[sourceDir] || !dirExists(sourceDir) {
			continue
		}
		seenDirs[sourceDir] = true
		targetDir := filepath.Join(workspacePath, "architect", parts[1])
		if err := copyDirContents(sourceDir, targetDir); err != nil {
			return err
		}
	}
	return nil
}

func copyDirContents(sourceDir, targetDir string) error {
	return filepath.WalkDir(sourceDir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(sourceDir, path)
		if err != nil {
			return err
		}
		clean := filepath.Clean(rel)
		if clean == "." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || filepath.IsAbs(clean) {
			return tuskerError(errorPathEscape, "artifact path escapes source directory: "+rel)
		}
		return copyFile(path, filepath.Join(targetDir, clean))
	})
}
