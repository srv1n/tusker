package main

import (
	"fmt"
	"os"
	"strings"
)

func showCmd(args Args) error {
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	id := firstNonEmpty(args.String("id"), args.String("_pos0"))
	if strings.TrimSpace(id) == "" {
		return tuskerError(errorMissingArg, "show requires an id", withHint("use `tusker show <ID> --capsule`"))
	}
	note, err := resolveNote(vaultPath, id)
	if err != nil {
		return err
	}
	mode := "capsule"
	for _, candidate := range []string{"full", "acceptance", "evidence", "verification", "capsule"} {
		if args.Bool(candidate) {
			mode = candidate
			break
		}
	}
	if args.String("section") != "" {
		mode = "section"
	}
	switch mode {
	case "full":
		content, err := readText(note.AbsolutePath)
		if err != nil {
			return err
		}
		fmt.Print(content)
		if !strings.HasSuffix(content, "\n") {
			fmt.Println()
		}
	case "acceptance":
		printSectionOrFallback(note, "## Acceptance contract")
	case "evidence":
		printSectionOrFallback(note, "## Evidence")
	case "verification":
		printVerification(note, args)
	case "section":
		heading := strings.TrimSpace(args.String("section"))
		if !strings.HasPrefix(heading, "#") {
			heading = "## " + heading
		}
		printSectionOrFallback(note, heading)
	default:
		fmt.Print(renderCapsule(note))
	}
	return nil
}

func printVerification(note Note, args Args) {
	var lines []string
	if summary := strings.TrimSpace(stringField(note.Data, "verification_summary")); summary != "" {
		lines = append(lines, "Verification: "+summary)
	}
	if by := strings.TrimSpace(stringField(note.Data, "verified_by")); by != "" {
		lines = append(lines, "Verified by: "+by)
	}
	if at := strings.TrimSpace(stringField(note.Data, "verified_at")); at != "" {
		lines = append(lines, "Verified at: "+at)
	}
	if summary := strings.TrimSpace(stringField(note.Data, "close_summary")); summary != "" {
		lines = append(lines, "Close: "+summary)
	}
	if by := strings.TrimSpace(stringField(note.Data, "closed_by")); by != "" {
		lines = append(lines, "Closed by: "+by)
	}
	if content := strings.TrimSpace(sectionContent(note.Body, "## Verification log")); content != "" {
		limit := atoiSafe(args.String("lines"))
		if limit <= 0 {
			limit = 5
		}
		logLines, total := boundedNonEmptyTail(content, limit)
		if len(lines) > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, fmt.Sprintf("Verification log: last %d of %d entries", len(logLines), total))
		lines = append(lines, logLines...)
		if total > len(logLines) {
			lines = append(lines, "")
			lines = append(lines, `Full log: tusker show `+stringField(note.Data, "id")+` --section "Verification log"`)
		}
	}
	if len(lines) == 0 {
		fmt.Fprintf(os.Stdout, "%s\n(no verification summary or Verification log section)\n", noteHeaderLine(note))
		return
	}
	fmt.Fprintf(os.Stdout, "%s\n\n%s\n", noteHeaderLine(note), strings.Join(lines, "\n"))
}

func boundedNonEmptyTail(content string, limit int) ([]string, int) {
	var nonEmpty []string
	for _, line := range strings.Split(content, "\n") {
		if strings.TrimSpace(line) != "" {
			nonEmpty = append(nonEmpty, line)
		}
	}
	if limit >= len(nonEmpty) {
		return nonEmpty, len(nonEmpty)
	}
	return nonEmpty[len(nonEmpty)-limit:], len(nonEmpty)
}

func printSectionOrFallback(note Note, heading string) {
	content := strings.TrimSpace(sectionContent(note.Body, heading))
	if content == "" {
		fmt.Fprintf(os.Stdout, "%s\n(no %s section)\n", noteHeaderLine(note), strings.TrimPrefix(heading, "## "))
		return
	}
	fmt.Fprintf(os.Stdout, "%s\n\n%s\n", noteHeaderLine(note), content)
}

func renderCapsule(note Note) string {
	content := strings.TrimSpace(sectionContent(note.Body, "## Agent capsule"))
	if content == "" {
		content = synthesizeCapsule(note)
	} else if extra := capsuleFrontmatterFacts(note); extra != "" {
		content = content + "\n" + extra
	}
	return noteHeaderLine(note) + "\n\n" + content + "\n"
}

func capsuleFrontmatterFacts(note Note) string {
	if stringField(note.Data, "type") != "task" {
		return ""
	}
	var lines []string
	if status := strings.TrimSpace(stringField(note.Data, "status")); status != "" {
		lines = append(lines, "- Status: "+status)
	}
	if summary := strings.TrimSpace(stringField(note.Data, "verification_summary")); summary != "" {
		lines = append(lines, "- Verification: "+summary)
	}
	if summary := strings.TrimSpace(stringField(note.Data, "close_summary")); summary != "" {
		lines = append(lines, "- Close: "+summary)
	}
	return strings.Join(lines, "\n")
}

func synthesizeCapsule(note Note) string {
	var lines []string
	noteType := stringField(note.Data, "type")
	switch noteType {
	case "task":
		lines = append(lines,
			"- Essence: "+firstNonEmpty(strings.TrimSpace(stringField(note.Data, "title")), stringField(note.Data, "id"))+".",
			"- Status: "+stringField(note.Data, "status"),
			"- Epic: "+wikiTarget(note.Data["epic"]),
			"- Risk/priority: "+firstNonEmpty(stringField(note.Data, "risk"), "?")+"/"+firstNonEmpty(stringField(note.Data, "priority"), "?"),
		)
		if intent := firstParagraph(sectionContent(note.Body, "## Intent")); intent != "" {
			lines = append(lines, "- Intent: "+intent)
		}
		if summary := strings.TrimSpace(stringField(note.Data, "verification_summary")); summary != "" {
			lines = append(lines, "- Verification: "+summary)
		}
		if summary := strings.TrimSpace(stringField(note.Data, "close_summary")); summary != "" {
			lines = append(lines, "- Close: "+summary)
		}
		lines = append(lines, "- Read next: `tusker show "+stringField(note.Data, "id")+" --acceptance`, then exact anchors only.")
	case "epic":
		lines = append(lines,
			"- Summary: "+firstNonEmpty(strings.TrimSpace(stringField(note.Data, "summary")), strings.TrimSpace(stringField(note.Data, "title"))),
			"- Status: "+stringField(note.Data, "status"),
			"- Drill down: `tusker list --epic "+stringField(note.Data, "id")+" --type task --open --limit 10`.",
		)
	case "doc":
		lines = append(lines,
			"- Summary: "+firstNonEmpty(strings.TrimSpace(sectionContent(note.Body, "## Summary")), stringField(note.Data, "title")),
			"- Node: "+stringField(note.Data, "node"),
			"- Audience/mode: "+firstNonEmpty(stringField(note.Data, "audience"), "?")+"/"+firstNonEmpty(stringField(note.Data, "mode"), "?"),
		)
	default:
		lines = append(lines, "- Summary: "+firstNonEmpty(strings.TrimSpace(stringField(note.Data, "title")), stringField(note.Data, "id")))
	}
	lines = append(lines, "- Avoid: raw logs, attachments, generated indexes, and full note reads unless explicitly needed.")
	return strings.Join(lines, "\n")
}

func noteHeaderLine(note Note) string {
	return fmt.Sprintf("%s  %s  %s  %s", stringField(note.Data, "id"), stringField(note.Data, "type"), stringField(note.Data, "status"), stringField(note.Data, "title"))
}

func firstParagraph(text string) string {
	for _, block := range strings.Split(strings.TrimSpace(text), "\n\n") {
		block = strings.TrimSpace(block)
		if block == "" || strings.HasPrefix(block, "|") || strings.HasPrefix(block, "-") {
			continue
		}
		return strings.Join(strings.Fields(block), " ")
	}
	return ""
}
