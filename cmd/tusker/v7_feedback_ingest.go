package main

import (
	"path/filepath"
	"sort"
	"strings"
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

func feedbackNoteSourceRef(target feedbackTarget, record feedbackRecord) string {
	return "feedback-note:" + firstNonEmpty(target.projectKey, projectKeyFromPath(target.repoRoot)) + ":" + record.RelativePath
}

func feedbackSignalBoundedFact(value string) string {
	return feedbackShort(value, feedbackSignalMaxFactStringChars)
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
