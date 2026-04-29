package main

import (
	"fmt"
	"strings"
)

func handoffCmd(args Args) error {
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	id, err := requireArg(args, "id")
	if err != nil {
		return err
	}
	role, err := requireArg(args, "for")
	if err != nil {
		return err
	}
	role = strings.TrimSpace(strings.ToLower(role))
	if role != "worker" && role != "verifier" && role != "reviewer" {
		return tuskerError(errorInvalidArg, `--for must be worker, verifier, or reviewer`, withContext(map[string]any{"arg": "--for", "value": role}))
	}
	note, err := resolveNote(vaultPath, id)
	if err != nil {
		return err
	}
	if stringField(note.Data, "type") != "story" && stringField(note.Data, "type") != "bug" {
		return tuskerError(errorInvalidArg, fmt.Sprintf("%s: handoff only supports story and bug notes", id), withContext(map[string]any{"id": id, "type": stringField(note.Data, "type")}))
	}
	packet := renderHandoffPacket(note, role)
	if args.Bool("json") {
		emitJSON(map[string]any{
			"ok":      true,
			"id":      stringField(note.Data, "id"),
			"role":    role,
			"packet":  packet,
			"status":  stringField(note.Data, "status"),
			"review":  stringField(note.Data, "review_state"),
			"note":    note.AbsolutePath,
			"epic":    wikiTarget(note.Data["epic"]),
			"version": intField(note.Data, "work_revision"),
		})
		return nil
	}
	fmt.Println(packet)
	return nil
}

func renderHandoffPacket(note Note, role string) string {
	var out []string
	out = append(out, fmt.Sprintf("# Tusker %s handoff", strings.Title(role)))
	out = append(out, "")
	out = append(out, fmt.Sprintf("- Item: %s — %s", stringField(note.Data, "id"), stringField(note.Data, "title")))
	out = append(out, fmt.Sprintf("- Type: %s", stringField(note.Data, "type")))
	out = append(out, fmt.Sprintf("- Epic: %s", wikiTarget(note.Data["epic"])))
	out = append(out, fmt.Sprintf("- Status: %s", stringField(note.Data, "status")))
	out = append(out, fmt.Sprintf("- Review state: %s", stringField(note.Data, "review_state")))
	out = append(out, fmt.Sprintf("- Work revision: %d", intField(note.Data, "work_revision")))
	out = append(out, fmt.Sprintf("- Risk: %s", stringField(note.Data, "risk")))
	out = append(out, fmt.Sprintf("- Delegation: %s", stringField(note.Data, "delegation")))
	out = append(out, fmt.Sprintf("- Note: %s", note.AbsolutePath))

	addPacketSection(&out, "Problem", firstNonEmpty(
		sectionContent(note.Body, "## Problem"),
		sectionContent(note.Body, "## Summary"),
	))
	addPacketSection(&out, "Acceptance criteria", sectionContent(note.Body, "## Acceptance criteria"))
	addPacketSection(&out, "Canon", sectionContent(note.Body, "## Canon"))
	addPacketSection(&out, "Code anchors", sectionContent(note.Body, "## Code anchors"))
	addPacketSection(&out, "Plan", firstNonEmpty(
		sectionContent(note.Body, "## Plan"),
		sectionContent(note.Body, "## Fix"),
	))
	addPacketSection(&out, "Verification plan", sectionContent(note.Body, "## Verification plan"))
	addPacketSection(&out, "Evidence so far", sectionContent(note.Body, "## Evidence"))
	addPacketSection(&out, "Agent handoff notes", sectionContent(note.Body, "## Agent handoff"))

	switch role {
	case "worker":
		out = append(out, "", "## Worker instructions", "", strings.Join([]string{
			"- Implement in the current project working tree. If the code is not in the current tree, it does not count.",
			"- Update the note below the `## Agent handoff` line only. Do not quietly rewrite the human-authored spec above it.",
			"- Append concrete evidence and work-log entries that match reality.",
			"- Do not self-certify review readiness. Your job is implementation, not truth verification.",
			"- If you finish an implementation pass, say exactly what changed, what was verified, and what still needs verification.",
		}, "\n"))
	case "verifier":
		out = append(out, "", "## Verifier instructions", "", strings.Join([]string{
			"- Verify the worker's claims against the current working tree, not against vibes, side branches, or stale notes.",
			"- Check that named files, symbols, commands, and artifacts in the evidence actually exist.",
			"- Run the verification plan or explain exactly why it could not be completed.",
			"- If claims do not match reality, use `tusker review request-changes --id " + stringField(note.Data, "id") + " --by <name> --summary \"<why>\"`.",
			"- If the implementation pass is truthful and materially complete, use `tusker review verify --id " + stringField(note.Data, "id") + " --by <name> --summary \"verified against current tree\"`.",
		}, "\n"))
	case "reviewer":
		out = append(out, "", "## Reviewer instructions", "", strings.Join([]string{
			"- Assume verification should already have happened. Focus on acceptance, product behavior, risk, and trade-offs.",
			"- If the implementation is not acceptable, use `tusker review request-changes --id " + stringField(note.Data, "id") + " --by <name> --summary \"<why>\"`.",
			"- If it is acceptable, use `tusker review approve --id " + stringField(note.Data, "id") + " --by <name> --summary \"looks good\"`.",
			"- Approval is not done. Attestation and `set-status done` still happen after review.",
		}, "\n"))
	}

	return strings.TrimSpace(strings.Join(out, "\n")) + "\n"
}

func addPacketSection(out *[]string, heading, content string) {
	content = strings.TrimSpace(content)
	if content == "" {
		return
	}
	*out = append(*out, "", "## "+heading, "", content)
}
