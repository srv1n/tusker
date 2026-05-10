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
		printSectionOrFallback(note, "## Verification log")
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
