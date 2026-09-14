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
	"strconv"
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
	Authority           ReviewResult `json:"-"`
	Kind                string       `json:"kind,omitempty"`
	Verdict             string       `json:"verdict,omitempty"`
	Risk                string       `json:"risk,omitempty"`
	Summary             string       `json:"summary,omitempty"`
	MaterialFingerprint string       `json:"material_fingerprint,omitempty"`
	Findings            []string     `json:"findings,omitempty"`
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
	artifactScope := externalArtifactScope(jobID)
	artifactDirAbs := filepath.Join(ctx.Project.RepoRoot, "architect", taskID, "jobs", artifactScope)
	artifactDirRel := filepath.ToSlash(filepath.Join("architect", taskID, "jobs", artifactScope))
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
	reviewResult, reviewParseErr := externalReviewResultFromArtifacts(classified)
	if reviewParseErr == nil && reviewResult != nil {
		if err := validateExternalReviewAuthority(ctx, note, run, reviewResult); err != nil {
			reviewParseErr = err
			// Do not let an untrusted/stale DTO become event material identity;
			// the blocker remains durable, but only an authority-validated review
			// can bind a workspace fingerprint to a transition.
			reviewResult = nil
		}
	}
	report := externalCollectReport{
		Schema:       externalCollectSchema,
		TaskID:       taskID,
		RecordID:     recordID,
		Runner:       runner,
		JobID:        firstNonEmpty(jobID, fetch.JobID),
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
			ProjectID:    ctx.Project.ProjectID,
			RecordID:     recordID,
			ItemID:       taskID,
			Runner:       runner,
			JobID:        report.JobID,
			AttemptID:    run.ActiveAttemptID,
			WorkRevision: intField(note.Data, "work_revision"),
			Path:         artifact.Path,
			RelPath:      artifact.RelPath,
			Sha256:       artifact.Sha256,
			Kind:         "patch",
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
	if reviewParseErr != nil {
		report.NextAction = externalLoopActionEscalateHuman
		report.Dispatchable = false
		report.Blockers = append(report.Blockers, "external review result parse failed: "+reviewParseErr.Error())
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

func externalReviewResultFromArtifacts(artifacts []externalArtifact) (*externalReviewResult, error) {
	for _, artifact := range artifacts {
		if artifact.Kind != "review_packet" {
			continue
		}
		text, err := readText(artifact.Path)
		if err != nil {
			continue
		}
		if result, ok, parseErr := parseExternalReviewResult(text); parseErr != nil {
			return nil, parseErr
		} else if ok {
			return &result, nil
		}
	}
	return nil, nil
}

func parseExternalReviewResult(text string) (externalReviewResult, bool, error) {
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
	if len(candidates) == 0 {
		return externalReviewResult{}, false, nil
	}
	for _, candidate := range candidates {
		var values map[string]any
		if err := json.Unmarshal([]byte(candidate), &values); err != nil {
			return externalReviewResult{}, false, fmt.Errorf("review result JSON is invalid: %w", err)
		}
		kind := strings.ToLower(strings.TrimSpace(stringValue(values["kind"])))
		if kind != "" && kind != "review" && kind != "architect" {
			continue
		}
		if _, ok := values["schema"]; !ok {
			return externalReviewResult{}, false, fmt.Errorf("external review result must carry authoritative %s DTO", reviewResultSchema)
		}
		findings, err := normalizeExternalReviewFindings(values["findings"])
		if err != nil {
			return externalReviewResult{}, false, err
		}
		values["findings"] = findings
		authorityRaw, err := json.Marshal(values)
		if err != nil {
			return externalReviewResult{}, false, fmt.Errorf("external review result could not be canonicalized: %w", err)
		}
		var authority ReviewResult
		if err := json.Unmarshal(authorityRaw, &authority); err != nil {
			return externalReviewResult{}, false, fmt.Errorf("external review result is not a valid %s DTO: %w", reviewResultSchema, err)
		}
		if err := normalizeReviewResult(&authority); err != nil {
			return externalReviewResult{}, false, fmt.Errorf("external review result is not a valid %s DTO: %w", reviewResultSchema, err)
		}
		if authority.Schema != reviewResultSchema {
			return externalReviewResult{}, false, fmt.Errorf("external review result schema must be %s", reviewResultSchema)
		}
		if err := validatePersistedReviewResult(authority); err != nil {
			return externalReviewResult{}, false, fmt.Errorf("external review result is not authoritative: %w", err)
		}
		if authority.Verdict == "" {
			continue
		}
		result := externalReviewResult{
			Authority: authority,
			Kind:      firstNonEmpty(kind, "review"),
			Verdict:   authority.Verdict,
			// Risk is task policy, not a field in the authoritative v3 DTO. Do
			// not let provider metadata downgrade a high-risk task into an
			// auto-close-eligible result; callers fall back to the canonical task
			// risk when this transport field is empty.
			Risk:                "",
			Summary:             authority.Summary,
			MaterialFingerprint: authority.MaterialFingerprint,
			Findings:            authority.Findings,
		}
		return result, true, nil
	}
	return externalReviewResult{}, false, nil
}

// validateExternalReviewAuthority applies the same durable implementation
// binding used by the v3 review-result/completion path. The provider artifact
// is transport only until its task, attempt, source, and exact workspace
// material all match the current canonical review run.
func validateExternalReviewAuthority(ctx *automationCommandContext, note Note, run RunStatus, result *externalReviewResult) error {
	if ctx == nil || ctx.Store == nil || result == nil {
		return fmt.Errorf("external review authority context is unavailable")
	}
	authority := result.Authority
	recordID := trackerRecordID(note)
	if run.Lane != runLaneReview || strings.TrimSpace(run.ActiveAttemptID) == "" {
		return fmt.Errorf("external review authority requires the current review attempt")
	}
	if authority.ProjectID != ctx.Project.ProjectID || authority.TaskID != recordID || authority.AttemptID != run.ActiveAttemptID || authority.WorkRevision != run.WorkRevision || authority.WorkRevision != intField(note.Data, "work_revision") {
		return fmt.Errorf("external review authority task, project, attempt, or work revision is stale")
	}
	if authority.TaskStateRev != stringField(note.Data, "state_rev") {
		return fmt.Errorf("external review authority task revision is stale")
	}
	expectedSource := firstNonEmpty(stringField(note.Data, "source_sha"), stringField(note.Data, "source_commit"))
	if expectedSource == "" || authority.ImplementationSHA != expectedSource {
		return fmt.Errorf("external review authority implementation source is stale")
	}
	if authority.Actor != reviewerActorForNote(ctx.Workflow.Data.Reviewer.Actor, note) {
		return fmt.Errorf("external review authority reviewer actor is not authorized")
	}
	if strings.TrimSpace(run.Runner) != "" && authority.Runner != run.Runner {
		return fmt.Errorf("external review authority runner drifted")
	}
	if strings.TrimSpace(run.RunnerProfile) != "" && authority.RunnerProfile != run.RunnerProfile {
		return fmt.Errorf("external review authority runner profile drifted")
	}
	if strings.TrimSpace(run.WorkerPolicyFP) != "" && authority.WorkerPolicyFP != run.WorkerPolicyFP {
		return fmt.Errorf("external review authority worker policy drifted")
	}
	_, expectedMaterial, bindingErr := reviewImplementationParent(ctx.Store, ctx.Project.VaultRoot, ctx.Project.ProjectID, recordID, run.WorkRevision, authority.ImplementationSHA, note)
	if bindingErr != nil {
		return fmt.Errorf("external review authority implementation binding is unavailable: %w", bindingErr)
	}
	material, materialErr := reviewAttemptMaterialFingerprint(ctx.Store, ctx.Project.ProjectID, recordID, run.ActiveAttemptID, run.WorkRevision, authority.ImplementationSHA)
	if materialErr != nil {
		return fmt.Errorf("external review authority workspace material is unavailable: %w", materialErr)
	}
	if !reviewMaterialFingerprintsEqual(material, expectedMaterial) || !reviewMaterialFingerprintsEqual(material, authority.MaterialFingerprint) {
		return fmt.Errorf("external review authority material fingerprint is stale or arbitrary")
	}
	return nil
}

func reviewMaterialFingerprintsEqual(left, right string) bool {
	left = strings.TrimPrefix(strings.TrimSpace(left), "sha256:")
	right = strings.TrimPrefix(strings.TrimSpace(right), "sha256:")
	return left != "" && left == right
}

// normalizeExternalReviewFindings keeps structured findings as canonical JSON
// instead of relying on fmt.Sprint's map formatting. A malformed structured
// finding is a progression blocker: silently dropping it could turn a
// changes_requested review into an unqualified apply or close.
func normalizeExternalReviewFindings(value any) ([]string, error) {
	if value == nil {
		return nil, nil
	}
	var items []any
	switch typed := value.(type) {
	case []any:
		items = typed
	case []string:
		for _, item := range typed {
			items = append(items, item)
		}
	default:
		items = []any{value}
	}
	findings := make([]string, 0, len(items))
	for _, item := range items {
		switch typed := item.(type) {
		case string:
			text := strings.TrimSpace(typed)
			if text == "" {
				continue
			}
			if strings.HasPrefix(text, "{") {
				var object map[string]any
				if err := json.Unmarshal([]byte(text), &object); err != nil {
					return nil, fmt.Errorf("external review finding JSON is invalid: %w", err)
				}
				canonical, err := canonicalExternalReviewFinding(object)
				if err != nil {
					return nil, err
				}
				findings = append(findings, canonical)
				continue
			}
			findings = append(findings, text)
		case map[string]any:
			canonical, err := canonicalExternalReviewFinding(typed)
			if err != nil {
				return nil, err
			}
			findings = append(findings, canonical)
		default:
			return nil, fmt.Errorf("external review finding must be a string or object, got %T", item)
		}
	}
	return findings, nil
}

func canonicalExternalReviewFinding(value map[string]any) (string, error) {
	if value == nil {
		return "", fmt.Errorf("external review finding object is null")
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("external review finding could not be canonicalized: %w", err)
	}
	if _, err := parseReviewerFinding(string(encoded)); err != nil {
		return "", fmt.Errorf("external review finding is invalid: %w", err)
	}
	return string(encoded), nil
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
	// A provider may return a directory as a convenience, but walking it when
	// explicit files are present re-imports artifacts from older provider
	// cycles. The destination is already job-scoped, so directory discovery is
	// safe only when the fetch returned no file list at all and the source is
	// not a task-wide parent of the destination.
	sourceIsDest := filepath.Clean(sourceDir) == filepath.Clean(destDir)
	sourceContainsDest := samePathOrChild(destDir, sourceDir)
	if len(fetch.Files) == 0 && sourceDir != "" && dirExists(sourceDir) && (sourceIsDest || !sourceContainsDest) {
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

func externalArtifactScope(jobID string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(jobID)))
	return "job-" + hex.EncodeToString(sum[:])[:16]
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
	inputs, err := listCurrentExternalApplyInputs(store, project.ProjectID, run.RecordID, run)
	if err != nil {
		return err
	}
	if len(inputs) == 0 {
		return nil
	}
	for _, input := range inputs {
		rel := filepath.ToSlash(firstNonEmpty(input.RelPath, input.Path))
		if !strings.HasPrefix(rel, "architect/") {
			continue
		}
		clean := filepath.Clean(filepath.FromSlash(rel))
		if clean == "." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || filepath.IsAbs(clean) {
			return tuskerError(errorPathEscape, "apply input path escapes repository: "+rel)
		}
		source := strings.TrimSpace(input.Path)
		if source == "" {
			source = filepath.Join(project.RepoRoot, clean)
		}
		if !samePathOrChild(source, project.RepoRoot) || !fileExists(source) {
			continue
		}
		target := filepath.Join(workspacePath, clean)
		if samePathOrChild(source, target) && samePathOrChild(target, source) {
			continue
		}
		if err := copyFile(source, target); err != nil {
			return err
		}
	}
	return nil
}

func currentExternalApplyEvent(store *RuntimeStore, projectID, recordID string, run RunStatus) (*ExternalLoopEvent, error) {
	if store == nil || strings.TrimSpace(projectID) == "" || strings.TrimSpace(recordID) == "" {
		return nil, nil
	}
	events, err := store.ListExternalLoopEvents(projectID, recordID)
	if err != nil {
		return nil, err
	}
	wantRevision := strconv.Itoa(run.WorkRevision)
	liveJobID := firstNonEmpty(strings.TrimSpace(run.CloudTaskID), strings.TrimSpace(run.ApplyRef))
	for i := len(events) - 1; i >= 0; i-- {
		event := events[i]
		if strings.TrimSpace(event.Status) == "blocked" ||
			normalizeExternalLoopStage(event.Stage) != externalLoopStageCollected ||
			normalizeExternalLoopAction(event.Action) != externalLoopActionApplyPatch {
			continue
		}
		if revision, _ := externalLoopEventRevisionMaterial(event); revision != wantRevision {
			continue
		}
		if liveJobID != "" && strings.TrimSpace(event.JobID) != liveJobID {
			continue
		}
		return &event, nil
	}
	return nil, nil
}

func listCurrentExternalApplyInputs(store *RuntimeStore, projectID, recordID string, run RunStatus) ([]RuntimeApplyInput, error) {
	event, err := currentExternalApplyEvent(store, projectID, recordID, run)
	if err != nil {
		return nil, err
	}
	if event == nil {
		// Collection writes inputs before event admission. Recover that narrow
		// crash window only when the live run still names the provider job; an
		// unbound row without that identity is legacy/stale and must not drive an
		// apply.
		jobID := firstNonEmpty(strings.TrimSpace(run.CloudTaskID), strings.TrimSpace(run.ApplyRef))
		if jobID == "" {
			return nil, nil
		}
		return store.ListApplyInputsForRunScope(projectID, recordID, run.WorkRevision, "", jobID)
	}
	return store.ListApplyInputsForRunScope(projectID, recordID, run.WorkRevision, event.EventID, event.JobID)
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
