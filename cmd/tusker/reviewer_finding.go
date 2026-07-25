package main

import (
	"strings"
	"time"
)

// reviewerFindingSection is the task-body heading under which a reviewer's
// finding is pasted so the person who did the work sees it when the task
// returns to them.
const reviewerFindingSection = "## Reviewer findings"

// reviewerFindingGeneratedMarker namespaces the generated reviewer-findings
// section. It is written on the line directly beneath the heading so the daemon
// can find and replace its own section without clobbering a user-authored
// "## Reviewer findings" section that carries no marker.
const reviewerFindingGeneratedMarker = "<!-- tusker:reviewer-findings -->"

// reviewerFindingReturnReason is the run-level reason recorded when a reviewer
// finding bounces the work back to its author.
const reviewerFindingReturnReason = "reviewer finding returned to implementer"

// reviewerFindingRowMarkerPrefix tags a verification row's Notes cell with the
// review attempt that recorded it. Only rows carrying the marker for the
// current review attempt count as findings, so a stale fail row left behind by
// an earlier round does not bounce fresh work.
const reviewerFindingRowMarkerPrefix = "tusker-review:"

// reviewerFindingRowMarker is the exact marker token a review attempt embeds in
// a finding row's Notes cell.
func reviewerFindingRowMarker(attemptID string) string {
	return "[" + reviewerFindingRowMarkerPrefix + strings.TrimSpace(attemptID) + "]"
}

// reviewerFindingFromTask reports the reviewer's finding on a submitted task, if
// any, for the review attempt identified by attemptID. A finding is a
// verification row the reviewer marked failing (fail / failed) in the task's
// `## Verification` table whose Notes cell carries this attempt's marker. Rows
// inside fenced code blocks are ignored, and "blocked" rows are NOT findings:
// blocked-on-external is a legitimate state, not a reviewer rejection. The
// returned text is a compact bullet list of the failing rows, suitable for
// pasting straight into the task body so the implementer can act on it.
func reviewerFindingFromTask(note Note, attemptID string) (string, bool) {
	attemptID = strings.TrimSpace(attemptID)
	if attemptID == "" {
		return "", false
	}
	marker := reviewerFindingRowMarker(attemptID)
	section := fenceAwareSectionContent(note.Body, "## Verification")
	if section == "" {
		return "", false
	}
	var findings []string
	for _, line := range fenceAwareLines(section) {
		cells := markdownRowCells(line)
		if len(cells) < 3 {
			continue
		}
		result := strings.ToLower(strings.TrimSpace(cells[2]))
		if result != "fail" && result != "failed" {
			continue
		}
		noteText := ""
		if len(cells) >= 4 {
			noteText = strings.TrimSpace(cells[3])
		}
		// Round/author gating: only rows this review attempt recorded bounce
		// the work. A row without the current attempt's marker is either
		// pre-existing or from another round and must be ignored.
		if !strings.Contains(noteText, marker) {
			continue
		}
		covers := strings.TrimSpace(cells[0])
		check := strings.TrimSpace(cells[1])
		entry := "- "
		if covers != "" {
			entry += covers + ": "
		}
		entry += check + " -> " + result
		if display := strings.TrimSpace(stripReviewerFindingMarker(noteText, marker)); display != "" {
			entry += " (" + display + ")"
		}
		findings = append(findings, entry)
	}
	if len(findings) == 0 {
		return "", false
	}
	return strings.Join(findings, "\n"), true
}

// legacyReviewerFindingResult makes pre-result marker rows readable through
// the typed review surface. It intentionally does not infer a pass from old
// prose: only a marked failing row becomes changes_requested, preserving the
// original task and review-attempt attribution for migration/replay.
func legacyReviewerFindingResult(note Note, attemptID string) (ReviewResult, bool) {
	finding, ok := reviewerFindingFromTask(note, attemptID)
	if !ok {
		return ReviewResult{}, false
	}
	result := ReviewResult{Schema: reviewResultSchema, TaskID: stringField(note.Data, "id"), TaskStateRev: stringField(note.Data, "state_rev"), WorkRevision: intField(note.Data, "work_revision"), AttemptID: strings.TrimSpace(attemptID), Verdict: "changes_requested", Summary: "Migrated legacy reviewer finding.", Findings: []string{finding}}
	result.ResultRevision = reviewResultFingerprint(result)
	return result, true
}

// stripReviewerFindingMarker removes the attempt marker token from a Notes cell
// so the pasted finding reads cleanly without the machine tag.
func stripReviewerFindingMarker(noteText, marker string) string {
	return strings.TrimSpace(strings.ReplaceAll(noteText, marker, ""))
}

// fenceAwareLines returns the lines of text that are NOT inside a fenced code
// block (``` or ~~~), so heading and table scans do not treat fenced content as
// live markdown structure.
func fenceAwareLines(text string) []string {
	var out []string
	inFence := false
	for _, line := range strings.Split(text, "\n") {
		if isFenceDelimiter(line) {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		out = append(out, line)
	}
	return out
}

// isFenceDelimiter reports whether a line opens or closes a fenced code block.
func isFenceDelimiter(line string) bool {
	trimmed := strings.TrimSpace(line)
	return strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~")
}

// fenceAwareSectionContent returns the body content beneath a heading, stopping
// at the next heading, while ignoring headings that appear inside fenced code
// blocks.
func fenceAwareSectionContent(body, heading string) string {
	lines := strings.Split(body, "\n")
	target := strings.TrimSpace(heading)
	inFence := false
	headingIdx := -1
	for i, line := range lines {
		if isFenceDelimiter(line) {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		if strings.TrimSpace(line) == target {
			headingIdx = i
			break
		}
	}
	if headingIdx == -1 {
		return ""
	}
	endIdx := len(lines)
	inFence = false
	for i := headingIdx + 1; i < len(lines); i++ {
		if isFenceDelimiter(lines[i]) {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		if isMarkdownHeadingLine(lines[i]) {
			endIdx = i
			break
		}
	}
	return strings.TrimSpace(strings.Join(lines[headingIdx+1:endIdx], "\n"))
}

// isMarkdownHeadingLine reports whether a line is an H2-H6 ATX heading.
func isMarkdownHeadingLine(line string) bool {
	trimmed := strings.TrimLeft(line, " ")
	if !strings.HasPrefix(trimmed, "##") {
		return false
	}
	rest := strings.TrimLeft(trimmed, "#")
	return strings.HasPrefix(rest, " ") || rest == ""
}

// markdownRowCells splits a markdown table row into its trimmed cells, dropping
// the empty leading and trailing cells produced by the outer pipes. A line that
// is not a table row returns nil.
func markdownRowCells(line string) []string {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "|") {
		return nil
	}
	parts := strings.Split(line, "|")
	// Drop the empty cell before the leading pipe and after the trailing pipe.
	if len(parts) > 0 && strings.TrimSpace(parts[0]) == "" {
		parts = parts[1:]
	}
	if len(parts) > 0 && strings.TrimSpace(parts[len(parts)-1]) == "" {
		parts = parts[:len(parts)-1]
	}
	return parts
}

// returnReviewerFindingToImplementer sends a task a reviewer flagged back to
// whoever did the work: it pastes the finding into the task body and moves the
// task out of review into rework. Because close requires review status, the
// rework move holds the work open until the implementer settles the finding and
// re-requests review.
//
// The write order is crash-safe: the finding explanation and hand-back fields
// are committed first (while the task is still in review), and only then is the
// status flipped to rework. A crash between the two steps therefore never
// leaves rework with no explanation. The operation is idempotent: a task
// already in rework carrying the same generated finding is left untouched, so a
// daemon restart re-detecting the row neither double-appends nor re-bounces.
func returnReviewerFindingToImplementer(vaultPath, taskID, findingText, actor string) error {
	finding := strings.TrimSpace(findingText)
	if finding == "" {
		finding = "The reviewer recorded a finding. Re-check the work against the acceptance rows."
	}
	if strings.TrimSpace(actor) == "" {
		actor = "daemon:reviewer-finding"
	}
	note, err := resolveV7Note(vaultPath, taskID, "task")
	if err != nil {
		return err
	}
	data, body, err := parseFrontmatterMustRead(note.AbsolutePath)
	if err != nil {
		return err
	}
	// Idempotency: if the task is already in rework and the generated section
	// already carries this finding, re-detection is a no-op.
	if strings.EqualFold(stringField(data, "status"), "rework") {
		if existing := generatedReviewerFindingContent(body); existing != "" && strings.Contains(existing, finding) {
			return nil
		}
	}
	baseRev := stringField(data, "state_rev")
	now := time.Now().UTC().Format(time.RFC3339)
	section := strings.Join([]string{
		reviewerFindingGeneratedMarker,
		"",
		finding,
		"",
		"_Recorded by the reviewer on " + now + ". Address the finding above, then re-request review._",
	}, "\n")
	body = upsertGeneratedReviewerFindingSection(body, section)
	data["next_owner"] = "agent"
	data["next_source"] = "task"
	data["next_ref"] = taskID
	data["next_action"] = "Address the reviewer finding, then re-request review."
	data["updated_at"] = now
	data["updated_by"] = actor
	// Step 1: commit the explanation and hand-back fields while still in review.
	if _, err := saveV7DocumentCAS(note.AbsolutePath, data, body, v7FrontmatterOrder["task"], baseRev); err != nil {
		return err
	}
	// Step 2: flip to rework. If this crashes, the explanation already stands
	// and a later re-detection completes the flip idempotently.
	return statusV7Cmd(Args{
		"vault": vaultPath, "quiet": "true", "local": "true",
		"id": taskID, "status": "rework", "by": actor,
		"reason": reviewerFindingReturnReason,
	})
}

// generatedReviewerFindingContent returns the content of the daemon-generated
// reviewer-findings section (the one carrying the generated marker), or "" if
// no generated section is present. A user-authored section without the marker
// is not returned.
func generatedReviewerFindingContent(body string) string {
	lines := strings.Split(body, "\n")
	start, end := generatedReviewerFindingBounds(lines)
	if start == -1 {
		return ""
	}
	return strings.TrimSpace(strings.Join(lines[start+1:end], "\n"))
}

// generatedReviewerFindingBounds locates the daemon-generated reviewer-findings
// section: a "## Reviewer findings" heading whose first non-blank content line
// is the generated marker. It returns the heading index and the exclusive end
// index (the next heading or end of body), or (-1, -1) when absent. Headings
// inside fenced code blocks are ignored.
func generatedReviewerFindingBounds(lines []string) (int, int) {
	target := strings.TrimSpace(reviewerFindingSection)
	inFence := false
	for i := 0; i < len(lines); i++ {
		if isFenceDelimiter(lines[i]) {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		if strings.TrimSpace(lines[i]) != target {
			continue
		}
		// Find the section end and whether the marker appears before any
		// nested content that is not blank.
		end := len(lines)
		markerFound := false
		nested := false
		for j := i + 1; j < len(lines); j++ {
			if isFenceDelimiter(lines[j]) {
				nested = !nested
				continue
			}
			if nested {
				continue
			}
			if isMarkdownHeadingLine(lines[j]) {
				end = j
				break
			}
			trimmed := strings.TrimSpace(lines[j])
			if trimmed == "" {
				continue
			}
			if trimmed == reviewerFindingGeneratedMarker {
				markerFound = true
			}
			// The marker must be the first non-blank content line to count as
			// the generated section.
			break
		}
		if markerFound {
			return i, end
		}
	}
	return -1, -1
}

// upsertGeneratedReviewerFindingSection replaces the daemon-generated
// reviewer-findings section in place when present, or appends a fresh generated
// section otherwise. A user-authored "## Reviewer findings" section without the
// generated marker is never clobbered; the generated section is namespaced by
// its marker and lives independently.
func upsertGeneratedReviewerFindingSection(body, content string) string {
	lines := strings.Split(body, "\n")
	start, end := generatedReviewerFindingBounds(lines)
	if start != -1 {
		prefix := strings.Join(lines[:start+1], "\n")
		suffix := strings.Join(lines[end:], "\n")
		out := prefix + "\n\n" + content
		if strings.TrimSpace(suffix) != "" {
			out += "\n\n" + suffix
		} else {
			out += "\n"
		}
		return out
	}
	trimmed := strings.TrimRight(body, "\n")
	if trimmed == "" {
		return reviewerFindingSection + "\n\n" + content + "\n"
	}
	return trimmed + "\n\n" + reviewerFindingSection + "\n\n" + content + "\n"
}
