package main

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const feedbackNoteImportSchema = "tusker.feedback_note_import/v1"

type feedbackTargetWarning struct {
	Code       string `json:"code"`
	Message    string `json:"message"`
	Selector   string `json:"selector,omitempty"`
	ProjectKey string `json:"project_key,omitempty"`
	RepoRoot   string `json:"repo_root,omitempty"`
	VaultRoot  string `json:"vault_root,omitempty"`
	Health     string `json:"health,omitempty"`
}

type feedbackTargetResolution struct {
	Targets  []feedbackTarget
	Warnings []feedbackTargetWarning
}

type feedbackNoteImportRecord struct {
	Schema           string            `json:"schema"`
	ImportRunID      string            `json:"import_run_id"`
	ImportedAt       string            `json:"imported_at"`
	SourceRef        string            `json:"source_ref"`
	SourceProjectKey string            `json:"source_project_key"`
	SourceRepoRoot   string            `json:"source_repo_root"`
	SourceVaultRoot  string            `json:"source_vault_root"`
	SourceNotePath   string            `json:"source_note_path"`
	SourceRelative   string            `json:"source_relative_path"`
	DedupeKey        string            `json:"dedupe_key"`
	SignalID         string            `json:"signal_id"`
	SignalPath       string            `json:"signal_path"`
	Fields           map[string]string `json:"fields"`
}

type feedbackIngestItem struct {
	SourceRef        string `json:"source_ref"`
	SourceProjectKey string `json:"source_project_key"`
	SourceRepoRoot   string `json:"source_repo_root"`
	SourceVaultRoot  string `json:"source_vault_root"`
	SourcePath       string `json:"source_path"`
	ImportPath       string `json:"import_path,omitempty"`
	SignalPath       string `json:"signal_path,omitempty"`
	DedupeKey        string `json:"dedupe_key"`
	SignalID         string `json:"signal_id"`
}

type feedbackIngestResult struct {
	Date           string
	Since          string
	ImportRunID    string
	OutputVault    string
	Targets        []feedbackTarget
	Warnings       []feedbackTargetWarning
	Items          []feedbackIngestItem
	WrittenImports []string
	WrittenSignals []string
}

func feedbackIngestCmd(args Args) error {
	result, err := buildFeedbackIngest(args)
	if err != nil {
		return err
	}
	if args.Bool("write") || args.Bool("apply") {
		if err := writeFeedbackIngestResult(&result); err != nil {
			return err
		}
	}
	if args.Bool("json") {
		emitJSON(map[string]any{
			"ok":            true,
			"date":          result.Date,
			"since":         result.Since,
			"import_run_id": result.ImportRunID,
			"output_vault":  result.OutputVault,
			"targets":       feedbackTargetsJSON(result.Targets),
			"warnings":      feedbackTargetWarningsJSON(result.Warnings),
			"counts":        map[string]any{"targets": len(result.Targets), "notes": len(result.Items), "imports_written": len(result.WrittenImports), "signals_written": len(result.WrittenSignals)},
			"items":         result.Items,
			"import_paths":  result.WrittenImports,
			"signal_paths":  result.WrittenSignals,
		})
		return nil
	}
	fmt.Print(renderFeedbackIngestMarkdown(result))
	return nil
}

func buildFeedbackIngest(args Args) (feedbackIngestResult, error) {
	sinceDate, since, err := feedbackCommandSince(args, "feedback ingest")
	if err != nil {
		return feedbackIngestResult{}, err
	}
	date := feedbackCommandDate(args, "feedback ingest")
	if date == "" {
		return feedbackIngestResult{}, tuskerError(errorInvalidArg, "feedback ingest --date must be YYYY-MM-DD: "+args.String("date"))
	}
	resolution, err := feedbackResolveTargets(args, true)
	if err != nil {
		return feedbackIngestResult{}, err
	}
	outputVault, err := feedbackOutputVaultPath(args, resolution.Targets)
	if err != nil {
		return feedbackIngestResult{}, err
	}
	runID := feedbackImportRunID(args, date, since, resolution.Targets)
	result := feedbackIngestResult{
		Date:        date,
		Since:       since,
		ImportRunID: runID,
		OutputVault: outputVault,
		Targets:     resolution.Targets,
		Warnings:    resolution.Warnings,
	}
	seenSourceRefs := map[string]bool{}
	for _, target := range resolution.Targets {
		records, err := feedbackRecordsForVault(target.vaultPath, target.repoRoot, sinceDate)
		if err != nil {
			return feedbackIngestResult{}, err
		}
		for _, record := range records {
			if len(record.Issues) > 0 {
				continue
			}
			sourceRef := feedbackNoteSourceRef(target, record)
			if seenSourceRefs[sourceRef] {
				continue
			}
			seenSourceRefs[sourceRef] = true
			signal := feedbackSignalFromFeedbackRecord(target, record, date, runID)
			importPath := feedbackNoteImportPath(outputVault, runID, target, record)
			signalPath := feedbackSignalPath(outputVault, signal)
			result.Items = append(result.Items, feedbackIngestItem{
				SourceRef:        sourceRef,
				SourceProjectKey: target.projectKey,
				SourceRepoRoot:   target.repoRoot,
				SourceVaultRoot:  target.vaultPath,
				SourcePath:       record.Path,
				ImportPath:       feedbackPathRelativeToVault(outputVault, importPath),
				SignalPath:       feedbackPathRelativeToVault(outputVault, signalPath),
				DedupeKey:        signal.DedupeKey,
				SignalID:         signal.ID,
			})
		}
	}
	sort.SliceStable(result.Items, func(i, j int) bool {
		return result.Items[i].SourceRef < result.Items[j].SourceRef
	})
	return result, nil
}

func writeFeedbackIngestResult(result *feedbackIngestResult) error {
	for _, item := range result.Items {
		target, record, ok := feedbackIngestSourceRecord(result.Targets, item.SourceRef)
		if !ok {
			continue
		}
		signal := feedbackSignalFromFeedbackRecord(target, record, result.Date, result.ImportRunID)
		signalPath, err := writeFeedbackSignal(result.OutputVault, signal)
		if err != nil {
			return err
		}
		item.SignalPath = feedbackPathRelativeToVault(result.OutputVault, signalPath)
		importRecord := feedbackNoteImportRecordFromSignal(target, record, result.ImportRunID, signal, item.SignalPath)
		importPath, err := writeFeedbackNoteImportRecord(result.OutputVault, importRecord)
		if err != nil {
			return err
		}
		item.ImportPath = feedbackPathRelativeToVault(result.OutputVault, importPath)
		result.WrittenSignals = append(result.WrittenSignals, signalPath)
		result.WrittenImports = append(result.WrittenImports, importPath)
		for i := range result.Items {
			if result.Items[i].SourceRef == item.SourceRef {
				result.Items[i] = item
				break
			}
		}
	}
	return nil
}

func feedbackIngestSourceRecord(targets []feedbackTarget, sourceRef string) (feedbackTarget, feedbackRecord, bool) {
	for _, target := range targets {
		records, err := feedbackRecordsForVault(target.vaultPath, target.repoRoot, time.Time{})
		if err != nil {
			continue
		}
		for _, record := range records {
			if feedbackNoteSourceRef(target, record) == sourceRef {
				return target, record, true
			}
		}
	}
	return feedbackTarget{}, feedbackRecord{}, false
}

func feedbackResolveTargets(args Args, includeDefault bool) (feedbackTargetResolution, error) {
	var result feedbackTargetResolution
	seen := map[string]bool{}
	add := func(target feedbackTarget) {
		target = normalizeFeedbackTarget(target)
		key := filepath.Clean(target.vaultPath)
		if key == "" || seen[key] {
			return
		}
		seen[key] = true
		if warning, ok := feedbackTargetBlockingWarning(target); ok {
			result.Warnings = append(result.Warnings, warning)
			return
		}
		result.Targets = append(result.Targets, target)
	}

	for _, repo := range splitFeedbackList(args.String("repo")) {
		target, err := feedbackTargetForRepo(repo)
		if err != nil {
			return result, err
		}
		target.selector = repo
		add(target)
	}
	if vaultArg := strings.TrimSpace(args.String("vault")); vaultArg != "" {
		vaultPath, err := filepath.Abs(vaultArg)
		if err != nil {
			return result, err
		}
		add(feedbackTarget{vaultPath: vaultPath, repoRoot: filepath.Dir(vaultPath), selector: vaultArg, enabled: true})
	}
	projectSelectors := splitFeedbackList(firstNonEmpty(args.String("project"), args.String("projects")))
	if args.Bool("registered") && len(projectSelectors) == 0 {
		projectSelectors = []string{"*"}
	}
	if len(projectSelectors) > 0 {
		registered, err := feedbackRegisteredProjectTargets(projectSelectors)
		if err != nil {
			return result, err
		}
		for _, target := range registered {
			add(target)
		}
	}
	if len(result.Targets) == 0 && len(seen) == 0 && includeDefault {
		vaultPath, err := resolveVaultPath(args, false)
		if err != nil {
			return result, err
		}
		add(feedbackTarget{vaultPath: vaultPath, repoRoot: filepath.Dir(vaultPath), selector: "current", enabled: true})
	}
	sort.SliceStable(result.Targets, func(i, j int) bool {
		if result.Targets[i].projectKey != result.Targets[j].projectKey {
			return result.Targets[i].projectKey < result.Targets[j].projectKey
		}
		return result.Targets[i].vaultPath < result.Targets[j].vaultPath
	})
	if len(result.Targets) == 0 {
		if len(result.Warnings) > 0 {
			return result, tuskerError(errorNotFound, "no healthy feedback targets", withContext(map[string]any{"warnings": feedbackTargetWarningsJSON(result.Warnings)}))
		}
		return result, tuskerError(errorNotFound, "no feedback targets")
	}
	return result, nil
}

func feedbackTargetForRepo(repo string) (feedbackTarget, error) {
	vaultPath, repoRoot, err := feedbackVaultForRepoPath(repo)
	if err != nil {
		return feedbackTarget{}, err
	}
	return feedbackTarget{vaultPath: vaultPath, repoRoot: repoRoot, enabled: true}, nil
}

func feedbackRegisteredProjectTargets(selectors []string) ([]feedbackTarget, error) {
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		return nil, err
	}
	defer store.Close()
	loaded, err := loadRegisteredProjects(store, registeredProjectLoadOptions{MetadataOnly: true, LoadDisabled: true})
	if err != nil {
		return nil, err
	}
	projects := loadedRegisteredProjects(loaded)
	var targets []feedbackTarget
	for _, selector := range selectors {
		selector = strings.TrimSpace(selector)
		for _, project := range projects {
			if selector == "" || selector == "*" || strings.EqualFold(selector, "all") || registeredProjectMatches(project, selector) {
				targets = append(targets, feedbackTargetFromRegisteredProject(project, selector))
			}
		}
	}
	if len(targets) == 0 {
		return nil, tuskerError(errorNotFound, "registered project not found: "+strings.Join(selectors, ", "))
	}
	return targets, nil
}

func feedbackTargetFromRegisteredProject(project RegisteredProject, selector string) feedbackTarget {
	return feedbackTarget{
		vaultPath:  project.VaultRoot,
		repoRoot:   project.RepoRoot,
		projectKey: firstNonEmpty(project.ProjectKey, project.Name, project.ProjectID),
		projectID:  project.ProjectID,
		name:       project.Name,
		selector:   selector,
		registered: true,
		enabled:    project.Enabled,
		health:     project.Health,
	}
}

func normalizeFeedbackTarget(target feedbackTarget) feedbackTarget {
	if abs, err := filepath.Abs(target.vaultPath); err == nil {
		target.vaultPath = abs
	}
	if target.repoRoot == "" && target.vaultPath != "" {
		target.repoRoot = filepath.Dir(target.vaultPath)
	}
	if abs, err := filepath.Abs(target.repoRoot); err == nil {
		target.repoRoot = abs
	}
	if target.projectKey == "" {
		target.projectKey = firstNonEmpty(v7ProjectID(target.vaultPath), projectKeyFromPath(target.repoRoot))
	}
	if target.name == "" {
		target.name = filepath.Base(target.repoRoot)
	}
	if target.health == "" {
		target.health = projectHealthHealthy
	}
	return target
}

func feedbackTargetBlockingWarning(target feedbackTarget) (feedbackTargetWarning, bool) {
	base := feedbackTargetWarning{
		Selector:   target.selector,
		ProjectKey: target.projectKey,
		RepoRoot:   target.repoRoot,
		VaultRoot:  target.vaultPath,
		Health:     string(target.health),
	}
	if target.registered && (!target.enabled || target.health == projectHealthDisabled) {
		base.Code = "FEEDBACK_TARGET_DISABLED"
		base.Message = "registered feedback project is disabled: " + target.projectKey
		return base, true
	}
	if target.registered && target.health != "" && target.health != projectHealthHealthy {
		base.Code = "FEEDBACK_TARGET_UNHEALTHY"
		base.Message = "registered feedback project is unhealthy: " + target.projectKey
		return base, true
	}
	if target.vaultPath == "" || !dirExists(target.vaultPath) {
		base.Code = "FEEDBACK_TARGET_STALE_VAULT_ROOT"
		base.Message = "feedback target vault root is stale or missing: " + target.vaultPath
		return base, true
	}
	return feedbackTargetWarning{}, false
}

func feedbackOutputVaultPath(args Args, targets []feedbackTarget) (string, error) {
	if outputVault := strings.TrimSpace(args.String("output-vault")); outputVault != "" {
		return filepath.Abs(outputVault)
	}
	if vaultArg := strings.TrimSpace(args.String("vault")); vaultArg != "" {
		return filepath.Abs(vaultArg)
	}
	if len(targets) > 0 {
		return targets[0].vaultPath, nil
	}
	return resolveVaultPath(args, false)
}

func feedbackImportRunID(args Args, date, since string, targets []feedbackTarget) string {
	if value := feedbackSlug(firstNonEmpty(args.String("import-run-id"), args.String("run-id")), ""); value != "" {
		return value
	}
	var parts []string
	for _, target := range targets {
		parts = append(parts, target.projectKey, target.repoRoot, target.vaultPath)
	}
	sort.Strings(parts)
	base := strings.Join(append([]string{date, since}, parts...), "|")
	return "import-" + date + "-" + feedbackSignalHash(base)[:10]
}

func feedbackSignalFromFeedbackRecord(target feedbackTarget, record feedbackRecord, importDate, importRunID string) feedbackSignal {
	sourceRef := feedbackNoteSourceRef(target, record)
	dedupeKey := normalizedFeedbackDedupeKey(record.Fields["dedupe-key"])
	if dedupeKey == "" {
		dedupeKey = feedbackSignalDedupeKey(target.projectKey, record.RelativePath)
	}
	return completeFeedbackSignal(feedbackSignal{
		Date:       firstNonEmpty(record.Date, importDate),
		Project:    target.projectKey,
		TaskID:     firstTaskID(record.Fields["related"]),
		Source:     "feedback_note",
		Category:   feedbackSignalCategoryFromFeedbackRecord(record),
		Severity:   record.PriorityHint,
		Confidence: "medium",
		DedupeKey:  dedupeKey,
		Summary:    feedbackShort(firstNonEmpty(record.Fields["friction"], record.Fields["product-idea"], record.Theme), feedbackSignalMaxSummaryChars),
		ObservedFacts: map[string]any{
			"source_ref":         sourceRef,
			"source_project_key": target.projectKey,
			"source_repo_root":   feedbackSignalBoundedFact(target.repoRoot),
			"source_vault_root":  feedbackSignalBoundedFact(target.vaultPath),
			"source_note":        record.RelativePath,
			"import_run_id":      importRunID,
			"dedupe_key":         dedupeKey,
			"affected_command":   record.AffectedCommand,
			"impact":             feedbackShort(record.Fields["impact"], feedbackSignalMaxFactStringChars),
		},
		Recommendation: feedbackShort(record.Fields["product-idea"], feedbackSignalMaxSummaryChars),
	})
}

func feedbackSignalCategoryFromFeedbackRecord(record feedbackRecord) string {
	text := strings.ToLower(strings.Join([]string{record.Theme, record.AffectedCommand, record.Fields["context"], record.Fields["friction"], record.Fields["product-idea"], record.Fields["related"]}, " "))
	switch {
	case record.AffectedCommand != "" && record.AffectedCommand != "n/a" || strings.Contains(text, " cli ") || strings.Contains(text, " command ") || strings.Contains(text, " flag "):
		return "cli_friction"
	case strings.Contains(text, "acceptance") || strings.Contains(text, "proof") || strings.Contains(text, "verification"):
		return "acceptance_quality"
	case strings.Contains(text, "closeout") || strings.Contains(text, "gate") || strings.Contains(text, "human"):
		return "closeout_churn"
	case strings.Contains(text, "install") || strings.Contains(text, "bootstrap") || strings.Contains(text, "xcode") || strings.Contains(text, "environment"):
		return "environment_setup"
	case strings.Contains(text, "review") || strings.Contains(text, "rework"):
		return "review_loop"
	default:
		return "workflow_repeat"
	}
}

func feedbackNoteImportRecordFromSignal(target feedbackTarget, record feedbackRecord, importRunID string, signal feedbackSignal, signalPath string) feedbackNoteImportRecord {
	return feedbackNoteImportRecord{
		Schema:           feedbackNoteImportSchema,
		ImportRunID:      importRunID,
		ImportedAt:       todayISO(),
		SourceRef:        feedbackNoteSourceRef(target, record),
		SourceProjectKey: target.projectKey,
		SourceRepoRoot:   target.repoRoot,
		SourceVaultRoot:  target.vaultPath,
		SourceNotePath:   record.Path,
		SourceRelative:   record.RelativePath,
		DedupeKey:        signal.DedupeKey,
		SignalID:         signal.ID,
		SignalPath:       signalPath,
		Fields:           record.Fields,
	}
}

func writeFeedbackNoteImportRecord(outputVault string, record feedbackNoteImportRecord) (string, error) {
	path := feedbackNoteImportPathForRef(outputVault, record.ImportRunID, record.SourceProjectKey, record.SourceRelative, record.SourceRef)
	raw, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return "", err
	}
	if err := writeText(path, string(raw)+"\n"); err != nil {
		return "", err
	}
	return path, nil
}

func feedbackNoteImportPath(outputVault, importRunID string, target feedbackTarget, record feedbackRecord) string {
	return feedbackNoteImportPathForRef(outputVault, importRunID, target.projectKey, record.RelativePath, feedbackNoteSourceRef(target, record))
}

func feedbackNoteImportPathForRef(outputVault, importRunID, projectKey, relativePath, sourceRef string) string {
	base := feedbackSlug(projectKey+"-"+filepath.Base(relativePath), "feedback-note")
	return filepath.Join(outputVault, "feedback", "imports", importRunID, base+"-"+feedbackSignalHash(sourceRef)[:10]+".json")
}

func feedbackNoteSourceRef(target feedbackTarget, record feedbackRecord) string {
	return "feedback-note:" + firstNonEmpty(target.projectKey, projectKeyFromPath(target.repoRoot)) + ":" + record.RelativePath
}

func feedbackSignalBoundedFact(value string) string {
	return feedbackShort(value, feedbackSignalMaxFactStringChars)
}

func feedbackPathRelativeToVault(vaultPath, path string) string {
	rel, err := filepath.Rel(vaultPath, path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(rel)
}

func feedbackTargetsJSON(targets []feedbackTarget) []map[string]any {
	out := make([]map[string]any, 0, len(targets))
	for _, target := range targets {
		out = append(out, map[string]any{
			"project_key": target.projectKey,
			"project_id":  nullIfEmptyString(target.projectID),
			"name":        nullIfEmptyString(target.name),
			"repo_root":   target.repoRoot,
			"vault_root":  target.vaultPath,
			"registered":  target.registered,
			"health":      string(target.health),
		})
	}
	return out
}

func feedbackTargetWarningsJSON(warnings []feedbackTargetWarning) []feedbackTargetWarning {
	if warnings == nil {
		return []feedbackTargetWarning{}
	}
	return warnings
}

func renderFeedbackTargetWarnings(b *strings.Builder, warnings []feedbackTargetWarning) {
	if len(warnings) == 0 {
		return
	}
	b.WriteString("## Target Warnings\n\n")
	for _, warning := range warnings {
		b.WriteString("- " + warning.Code + ": " + warning.Message)
		if warning.ProjectKey != "" {
			b.WriteString(" (`" + warning.ProjectKey + "`)")
		}
		b.WriteString("\n")
	}
	b.WriteString("\n")
}

func renderFeedbackIngestMarkdown(result feedbackIngestResult) string {
	var b strings.Builder
	b.WriteString("# Feedback Ingest - " + result.Date + "\n\n")
	b.WriteString("- Since: " + result.Since + "\n")
	b.WriteString("- Import run: `" + result.ImportRunID + "`\n")
	b.WriteString("- Output vault: " + result.OutputVault + "\n")
	b.WriteString(fmt.Sprintf("- Targets: %d\n", len(result.Targets)))
	b.WriteString(fmt.Sprintf("- Notes imported: %d\n\n", len(result.Items)))
	renderFeedbackTargetWarnings(&b, result.Warnings)
	b.WriteString("| Source project | Source note | Dedupe key | Signal |\n")
	b.WriteString("|---|---|---|---|\n")
	if len(result.Items) == 0 {
		b.WriteString("| - | - | - | No feedback notes found for this window. |\n")
		return b.String()
	}
	for _, item := range result.Items {
		b.WriteString("| " + markdownCell(item.SourceProjectKey) + " | " + markdownCell(item.SourceRef) + " | `" + markdownCell(item.DedupeKey) + "` | `" + markdownCell(item.SignalID) + "` |\n")
	}
	return b.String()
}
