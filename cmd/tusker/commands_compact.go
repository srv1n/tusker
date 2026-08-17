package main

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

type compactResult struct {
	ID               string   `json:"id"`
	Path             string   `json:"path"`
	Written          bool     `json:"written"`
	BytesBefore      int      `json:"bytes_before"`
	BytesAfter       int      `json:"bytes_after"`
	BytesSaved       int      `json:"bytes_saved"`
	RemovedFields    []string `json:"removed_fields,omitempty"`
	RemovedSections  []string `json:"removed_sections,omitempty"`
	ArchivedLogs     []string `json:"archived_logs,omitempty"`
	ArchivedEvidence []string `json:"archived_evidence,omitempty"`
}

func compactCmd(args Args) error {
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	write := args.Bool("write")
	archiveLogs := args.Bool("archive-logs")
	if archiveLogs {
		if err := bootstrapV7Dirs(vaultPath); err != nil {
			return err
		}
	}
	var notes []Note
	if args.Bool("all") {
		all, err := listAllNotes(vaultPath)
		if err != nil {
			return err
		}
		for _, note := range all {
			if managedNoteType(stringField(note.Data, "type")) {
				notes = append(notes, note)
			}
		}
	} else {
		id := firstNonEmpty(args.String("id"), args.String("_pos0"))
		if strings.TrimSpace(id) == "" {
			return tuskerError(errorMissingArg, "compact requires an id or --all", withHint("use `tusker compact <ID>` for a dry run or `tusker compact <ID> --write` to update the note"))
		}
		note, err := resolveNote(vaultPath, id)
		if err != nil {
			return err
		}
		notes = append(notes, note)
	}

	results := make([]compactResult, 0, len(notes))
	for _, note := range notes {
		result, err := compactNote(note, write, archiveLogs, vaultPath)
		if err != nil {
			return err
		}
		results = append(results, result)
	}
	if write {
		autoReindex(vaultPath)
	}
	if args.Bool("json") {
		items := filteredCompactResults(results, args)
		emitJSON(map[string]any{
			"ok":      true,
			"write":   write,
			"count":   len(items),
			"total":   len(results),
			"changed": countChangedCompactResults(results),
			"items":   items,
		})
		return nil
	}
	printed := 0
	for _, result := range filteredCompactResults(results, args) {
		changed := compactResultChanged(result)
		action := "unchanged"
		if result.Written {
			action = "compacted"
		} else if changed {
			action = "would compact"
		}
		fmt.Printf("%s %s: %d -> %d bytes", result.ID, action, result.BytesBefore, result.BytesAfter)
		if result.BytesSaved > 0 {
			fmt.Printf(" (saved %d)", result.BytesSaved)
		}
		fmt.Println()
		if len(result.RemovedFields) > 0 {
			fmt.Printf("  fields: %s\n", strings.Join(result.RemovedFields, ", "))
		}
		if len(result.RemovedSections) > 0 {
			fmt.Printf("  sections: %s\n", strings.Join(result.RemovedSections, ", "))
		}
		if len(result.ArchivedLogs) > 0 {
			fmt.Printf("  archived logs: %s\n", strings.Join(result.ArchivedLogs, ", "))
		}
		if len(result.ArchivedEvidence) > 0 {
			fmt.Printf("  archived evidence: %s\n", strings.Join(result.ArchivedEvidence, ", "))
		}
		printed++
	}
	if args.Bool("all") && !args.Bool("verbose") {
		changed := countChangedCompactResults(results)
		unchanged := len(results) - changed
		if printed == 0 {
			fmt.Printf("No compactable notes among %d checked.\n", len(results))
		} else if unchanged > 0 {
			fmt.Printf("(%d unchanged notes hidden; add --verbose to show all)\n", unchanged)
		}
	}
	if !write {
		fmt.Println("dry run; add --write to update files")
	}
	return nil
}

func filteredCompactResults(results []compactResult, args Args) []compactResult {
	if !args.Bool("all") || args.Bool("verbose") {
		return results
	}
	var out []compactResult
	for _, result := range results {
		if compactResultChanged(result) {
			out = append(out, result)
		}
	}
	return out
}

func countChangedCompactResults(results []compactResult) int {
	count := 0
	for _, result := range results {
		if compactResultChanged(result) {
			count++
		}
	}
	return count
}

func compactResultChanged(result compactResult) bool {
	return result.BytesBefore != result.BytesAfter ||
		len(result.RemovedFields) > 0 ||
		len(result.RemovedSections) > 0 ||
		len(result.ArchivedLogs) > 0 ||
		len(result.ArchivedEvidence) > 0
}

func compactNote(note Note, write, archiveLogs bool, vaultPath string) (compactResult, error) {
	original, err := readText(note.AbsolutePath)
	if err != nil {
		return compactResult{}, err
	}
	data, body, err := parseFrontmatter(original)
	if err != nil {
		return compactResult{}, err
	}
	removedFields := pruneEmptyOptionalFrontmatter(data)
	var removedSections []string
	var archivedLogs []string
	var archivedEvidence []string
	if stringField(data, "type") == "task" {
		if archiveLogs && strings.HasSuffix(stringField(data, "schema"), "/v5") {
			body, archivedLogs, err = archiveV5WorkLogSection(vaultPath, note, data, body, write)
			if err != nil {
				return compactResult{}, err
			}
			if len(archivedLogs) > 0 {
				removedSections = append(removedSections, "Work log")
			}
			body, archivedEvidence, err = archiveV5VerificationLogSection(vaultPath, note, data, body, write)
			if err != nil {
				return compactResult{}, err
			}
			if len(archivedEvidence) > 0 {
				removedSections = append(removedSections, "Verification log")
			}
		}
		var compactRemoved []string
		body, compactRemoved = compactTaskBody(body, stringField(data, "risk"))
		removedSections = append(removedSections, compactRemoved...)
	}
	content, err := serializeDocument(data, body, frontmatterOrderForType(stringField(data, "type")))
	if err != nil {
		return compactResult{}, err
	}
	result := compactResult{
		ID:               firstNonEmpty(stringField(data, "id"), filepath.Base(note.AbsolutePath)),
		Path:             note.RelativePath,
		Written:          false,
		BytesBefore:      len([]byte(original)),
		BytesAfter:       len([]byte(content)),
		BytesSaved:       len([]byte(original)) - len([]byte(content)),
		RemovedFields:    removedFields,
		RemovedSections:  removedSections,
		ArchivedLogs:     archivedLogs,
		ArchivedEvidence: archivedEvidence,
	}
	if write && content != original {
		if err := writeText(note.AbsolutePath, content); err != nil {
			return compactResult{}, err
		}
		result.Written = true
	}
	return result, nil
}

func archiveV5VerificationLogSection(vaultPath string, note Note, data map[string]any, body string, write bool) (string, []string, error) {
	taskID := stringField(data, "id")
	verificationLog := sectionContent(body, "## Verification log")
	if taskID == "" || strings.TrimSpace(verificationLog) == "" || strings.Contains(verificationLog, "_No verification yet") || !v7TaskIDPattern.MatchString(taskID) {
		return body, nil, nil
	}
	if migratedV7EvidenceExists(vaultPath, taskID, "V5 task Verification log") {
		return removeMarkdownSection(body, "## Verification log"), []string{"existing migrated verification evidence"}, nil
	}
	evidenceID := fmt.Sprintf("%s-E-%s", taskID, padNumber(nextV7EvidenceSequence(vaultPath, taskID)))
	if write {
		path := filepath.Join(vaultPath, "evidence", taskID, evidenceID+".md")
		if !fileExists(path) {
			if err := writeMigratedV7EvidenceRecord(vaultPath, note, evidenceID, "log_excerpt", "V5 task Verification log", verificationLog); err != nil {
				return body, nil, err
			}
		}
	}
	return removeMarkdownSection(body, "## Verification log"), []string{evidenceID}, nil
}

func archiveV5WorkLogSection(vaultPath string, note Note, data map[string]any, body string, write bool) (string, []string, error) {
	taskID := stringField(data, "id")
	workLog := sectionContent(body, "## Work log")
	if taskID == "" || strings.TrimSpace(workLog) == "" || !v7TaskIDPattern.MatchString(taskID) {
		return body, nil, nil
	}
	if migratedV7AttemptExists(vaultPath, taskID) {
		return removeMarkdownSection(body, "## Work log"), []string{"existing migrated attempt"}, nil
	}
	attemptID := fmt.Sprintf("%s-A-%s", taskID, padNumber(nextV7AttemptSequence(vaultPath, taskID)))
	if write {
		if err := writeMigratedV7Attempt(vaultPath, note, attemptID, workLog); err != nil {
			return body, nil, err
		}
	}
	return removeMarkdownSection(body, "## Work log"), []string{attemptID}, nil
}

func migratedV7AttemptExists(vaultPath, taskID string) bool {
	idx, err := loadV7Index(vaultPath)
	if err != nil {
		return false
	}
	for _, attempt := range idx.Attempts[taskID] {
		if stringField(attempt.Data, "runner") == "migration" && strings.Contains(attempt.Body, "Migrated from the V5 task Work log") {
			return true
		}
	}
	return false
}

func removeMarkdownSection(body, heading string) string {
	lines := strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n")
	target := strings.TrimSpace(heading)
	for i, line := range lines {
		if strings.TrimSpace(line) != target {
			continue
		}
		next := nextHeadingIndex(lines, i+1)
		return collapseBlankRuns(strings.TrimSpace(strings.Join(append(lines[:i], lines[next:]...), "\n")))
	}
	return body
}

func compactTaskBody(body, risk string) (string, []string) {
	required := requiredTaskSectionSet(risk)
	candidates := makeSet(
		"## Execution plan",
		"## Work log",
		"## Canon",
		"## Code/system anchors",
		"## Constraints",
		"## Escalate if",
		"## Knowledge delta",
		"## Verification log",
	)
	lines := strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n")
	var out []string
	var removed []string
	for i := 0; i < len(lines); {
		heading := strings.TrimSpace(lines[i])
		if _, candidate := candidates[heading]; candidate {
			if _, isRequired := required[heading]; !isRequired {
				next := nextHeadingIndex(lines, i+1)
				if sectionIsDisposable(heading, lines[i+1:next]) {
					removed = append(removed, strings.TrimPrefix(heading, "## "))
					i = next
					continue
				}
			}
		}
		out = append(out, lines[i])
		i++
	}
	return collapseBlankRuns(strings.TrimSpace(strings.Join(out, "\n"))), removed
}

func requiredTaskSectionSet(risk string) map[string]struct{} {
	required := makeSet("## Intent", "## Acceptance contract", "## Evidence")
	switch strings.ToLower(strings.TrimSpace(risk)) {
	case "medium":
		required = makeSet("## Intent", "## Scope", "## Acceptance contract", "## Deliverables", "## Verification plan", "## Evidence")
	case "high":
		required = makeSet("## Intent", "## Scope", "## Acceptance contract", "## Canon", "## Code/system anchors", "## Constraints", "## Deliverables", "## Verification plan", "## Knowledge delta", "## Evidence", "## Verification log")
	case "critical":
		required = makeSet("## Intent", "## Scope", "## Acceptance contract", "## Canon", "## Code/system anchors", "## Constraints", "## Deliverables", "## Verification plan", "## Knowledge delta", "## Rollback", "## Evidence", "## Verification log")
	}
	return required
}

func nextHeadingIndex(lines []string, start int) int {
	for i := start; i < len(lines); i++ {
		if matched, _ := regexp.MatchString(`^#{2,6}\s+`, strings.TrimSpace(lines[i])); matched {
			return i
		}
	}
	return len(lines)
}

func sectionIsDisposable(heading string, lines []string) bool {
	for _, line := range lines {
		if !disposableLine(heading, strings.TrimSpace(line)) {
			return false
		}
	}
	return true
}

func disposableLine(heading, line string) bool {
	if line == "" || line == "-" || line == "---" {
		return true
	}
	if regexp.MustCompile(`^\d+\.$`).MatchString(line) {
		return true
	}
	if strings.HasPrefix(line, "- _No ") && strings.HasSuffix(line, "yet._") {
		return true
	}
	if heading == "## Work log" && regexp.MustCompile(`^- \d{4}-\d{2}-\d{2} .* (task created|bug task created)$`).MatchString(line) {
		return true
	}
	if heading == "## Knowledge delta" && disposableTableLine(line) {
		return true
	}
	return false
}

func disposableTableLine(line string) bool {
	if !strings.HasPrefix(line, "|") {
		return false
	}
	normalized := strings.ToLower(strings.ReplaceAll(line, " ", ""))
	if strings.Contains(normalized, "topic|before|after") || strings.Contains(normalized, "---") {
		return true
	}
	cells := splitMarkdownTableRow(line)
	for _, cell := range cells {
		if strings.TrimSpace(cell) != "" {
			return false
		}
	}
	return true
}

func collapseBlankRuns(body string) string {
	var out []string
	blankRun := 0
	for _, line := range strings.Split(body, "\n") {
		if strings.TrimSpace(line) == "" {
			blankRun++
			if blankRun > 1 {
				continue
			}
			out = append(out, "")
			continue
		}
		blankRun = 0
		out = append(out, line)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}
