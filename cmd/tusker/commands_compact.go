package main

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

type compactResult struct {
	ID              string   `json:"id"`
	Path            string   `json:"path"`
	Written         bool     `json:"written"`
	BytesBefore     int      `json:"bytes_before"`
	BytesAfter      int      `json:"bytes_after"`
	BytesSaved      int      `json:"bytes_saved"`
	RemovedFields   []string `json:"removed_fields,omitempty"`
	RemovedSections []string `json:"removed_sections,omitempty"`
}

func compactCmd(args Args) error {
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	write := args.Bool("write")
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
		result, err := compactNote(note, write)
		if err != nil {
			return err
		}
		results = append(results, result)
	}
	if write {
		autoReindex(vaultPath)
	}
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": true, "write": write, "count": len(results), "items": results})
		return nil
	}
	for _, result := range results {
		changed := result.BytesBefore != result.BytesAfter || len(result.RemovedFields) > 0 || len(result.RemovedSections) > 0
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
	}
	if !write {
		fmt.Println("dry run; add --write to update files")
	}
	return nil
}

func compactNote(note Note, write bool) (compactResult, error) {
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
	if stringField(data, "type") == "task" {
		body, removedSections = compactTaskBody(body, stringField(data, "risk"))
	}
	content, err := serializeDocument(data, body, frontmatterOrderForType(stringField(data, "type")))
	if err != nil {
		return compactResult{}, err
	}
	result := compactResult{
		ID:              firstNonEmpty(stringField(data, "id"), filepath.Base(note.AbsolutePath)),
		Path:            note.RelativePath,
		Written:         false,
		BytesBefore:     len([]byte(original)),
		BytesAfter:      len([]byte(content)),
		BytesSaved:      len([]byte(original)) - len([]byte(content)),
		RemovedFields:   removedFields,
		RemovedSections: removedSections,
	}
	if write && content != original {
		if err := writeText(note.AbsolutePath, content); err != nil {
			return compactResult{}, err
		}
		result.Written = true
	}
	return result, nil
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
