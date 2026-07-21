package main

import (
	"strings"
	"time"
)

// reviewerFindingSection is the task-body heading under which a reviewer's
// finding is pasted so the person who did the work sees it when the task
// returns to them.
const reviewerFindingSection = "## Reviewer findings"

// reviewerFindingReturnReason is the run-level reason recorded when a reviewer
// finding bounces the work back to its author.
const reviewerFindingReturnReason = "reviewer finding returned to implementer"

// reviewerFindingFromTask reports the reviewer's finding on a submitted task, if
// any. A finding is a verification row the reviewer marked failing (fail /
// failed / blocked) in the task's `## Verification` table. The returned text is
// a compact bullet list of the failing rows, suitable for pasting straight into
// the task body so the implementer can act on it.
func reviewerFindingFromTask(note Note) (string, bool) {
	section := sectionContent(note.Body, "## Verification")
	if section == "" {
		return "", false
	}
	var findings []string
	for _, line := range strings.Split(section, "\n") {
		cells := markdownRowCells(line)
		if len(cells) < 3 {
			continue
		}
		result := strings.ToLower(strings.TrimSpace(cells[2]))
		if result != "fail" && result != "failed" && result != "blocked" {
			continue
		}
		covers := strings.TrimSpace(cells[0])
		check := strings.TrimSpace(cells[1])
		entry := "- "
		if covers != "" {
			entry += covers + ": "
		}
		entry += check + " -> " + result
		if len(cells) >= 4 {
			if noteText := strings.TrimSpace(cells[3]); noteText != "" {
				entry += " (" + noteText + ")"
			}
		}
		findings = append(findings, entry)
	}
	if len(findings) == 0 {
		return "", false
	}
	return strings.Join(findings, "\n"), true
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
func returnReviewerFindingToImplementer(vaultPath, taskID, findingText, actor string) error {
	finding := strings.TrimSpace(findingText)
	if finding == "" {
		finding = "The reviewer recorded a finding. Re-check the work against the acceptance rows."
	}
	if strings.TrimSpace(actor) == "" {
		actor = "daemon:reviewer-finding"
	}
	if err := statusV7Cmd(Args{
		"vault": vaultPath, "quiet": "true", "local": "true",
		"id": taskID, "status": "rework", "by": actor,
		"reason": reviewerFindingReturnReason,
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
	now := time.Now().UTC().Format(time.RFC3339)
	section := strings.Join([]string{
		finding,
		"",
		"_Recorded by the reviewer on " + now + ". Address the finding above, then re-request review._",
	}, "\n")
	body = upsertMarkdownSection(body, reviewerFindingSection, section)
	data["next_owner"] = "agent"
	data["next_source"] = "task"
	data["next_ref"] = taskID
	data["next_action"] = "Address the reviewer finding, then re-request review."
	data["updated_at"] = now
	data["updated_by"] = actor
	_, err = saveV7DocumentCAS(note.AbsolutePath, data, body, v7FrontmatterOrder["task"], baseRev)
	return err
}

// upsertMarkdownSection replaces the named section's content when the heading is
// present, or appends the section to the end of the body when it is absent.
func upsertMarkdownSection(body, heading, content string) string {
	for _, line := range strings.Split(body, "\n") {
		if strings.TrimSpace(line) == strings.TrimSpace(heading) {
			return replaceSection(body, heading, content)
		}
	}
	trimmed := strings.TrimRight(body, "\n")
	if trimmed == "" {
		return heading + "\n\n" + content + "\n"
	}
	return trimmed + "\n\n" + heading + "\n\n" + content + "\n"
}
