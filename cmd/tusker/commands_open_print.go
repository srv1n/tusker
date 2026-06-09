package main

import (
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"charm.land/glamour/v2"
	"charm.land/lipgloss/v2"
)

type resolvedRecord struct {
	Note         Note
	VaultPath    string
	ProjectID    string
	ProjectLabel string
}

func printCmd(args Args) error {
	id := firstNonEmpty(args.String("id"), args.String("_pos0"))
	record, err := resolveRecordForCLI(args, id)
	if err != nil {
		return err
	}
	markdown, err := printableMarkdown(record, args)
	if err != nil {
		return err
	}
	if args.Bool("json") {
		emitJSON(map[string]any{
			"ok":       true,
			"id":       stringField(record.Note.Data, "id"),
			"path":     record.Note.AbsolutePath,
			"vault":    record.VaultPath,
			"project":  record.ProjectLabel,
			"markdown": markdown,
		})
		return nil
	}
	if args.Bool("plain") || strings.EqualFold(args.String("style"), "plain") {
		fmt.Print(ensureTrailingNewline(markdown))
		return nil
	}
	rendered, err := renderTerminalMarkdown(markdown, args)
	if err != nil {
		return err
	}
	fmt.Print(ensureTrailingNewline(rendered))
	return nil
}

func openCmd(args Args) error {
	id := firstNonEmpty(args.String("id"), args.String("_pos0"))
	record, err := resolveRecordForCLI(args, id)
	if err != nil {
		return err
	}
	target := record.Note.AbsolutePath
	targetKind := "file"
	if args.Bool("obsidian") {
		target = obsidianOpenURI(record.Note.AbsolutePath)
		targetKind = "obsidian"
	}
	if args.Bool("json") {
		emitJSON(map[string]any{
			"ok":          true,
			"id":          stringField(record.Note.Data, "id"),
			"path":        record.Note.AbsolutePath,
			"target":      target,
			"target_kind": targetKind,
			"vault":       record.VaultPath,
			"project":     record.ProjectLabel,
		})
		return nil
	}
	if args.Bool("path") || args.Bool("print") {
		fmt.Println(target)
		return nil
	}
	if args.Bool("editor") {
		if args.Bool("obsidian") {
			return tuskerError(errorInvalidArg, "--editor cannot be combined with --obsidian")
		}
		return openWithEditor(target)
	}
	if app := strings.TrimSpace(args.String("app")); app != "" {
		if err := openWithApp(target, app); err != nil {
			return err
		}
	} else if err := openNative(target); err != nil {
		return err
	}
	if !args.Bool("quiet") {
		fmt.Println(cliMuted("Opened " + target))
	}
	return nil
}

func printableMarkdown(record resolvedRecord, args Args) (string, error) {
	note := record.Note
	switch printMode(args) {
	case "capsule":
		capsule := strings.TrimSpace(renderCapsuleWithVault(note, record.VaultPath))
		header := noteHeaderLine(note)
		capsule = strings.TrimSpace(strings.TrimPrefix(capsule, header))
		return "# " + header + "\n\n" + capsule + "\n", nil
	case "acceptance":
		return noteSectionMarkdown(note, "## Acceptance"), nil
	case "evidence":
		return noteSectionMarkdown(note, "## Evidence"), nil
	case "verification":
		return verificationMarkdown(note, args), nil
	case "section":
		heading := strings.TrimSpace(args.String("section"))
		if !strings.HasPrefix(heading, "#") {
			heading = "## " + heading
		}
		return noteSectionMarkdown(note, heading), nil
	default:
		return strings.TrimSpace(note.Body) + "\n", nil
	}
}

func printMode(args Args) string {
	for _, candidate := range []string{"capsule", "acceptance", "evidence", "verification", "full"} {
		if args.Bool(candidate) {
			return candidate
		}
	}
	if strings.TrimSpace(args.String("section")) != "" {
		return "section"
	}
	return "full"
}

func noteSectionMarkdown(note Note, heading string) string {
	content := strings.TrimSpace(sectionContent(note.Body, heading))
	title := strings.TrimSpace(strings.TrimPrefix(heading, "## "))
	if content == "" {
		content = fmt.Sprintf("_No %s section._", title)
	}
	return "## " + title + "\n\n" + content + "\n"
}

func verificationMarkdown(note Note, args Args) string {
	var lines []string
	if summary := strings.TrimSpace(stringField(note.Data, "verification_summary")); summary != "" {
		lines = append(lines, "- Verification: "+summary)
	}
	if by := strings.TrimSpace(stringField(note.Data, "verified_by")); by != "" {
		lines = append(lines, "- Verified by: "+by)
	}
	if at := strings.TrimSpace(stringField(note.Data, "verified_at")); at != "" {
		lines = append(lines, "- Verified at: "+at)
	}
	if summary := strings.TrimSpace(stringField(note.Data, "close_summary")); summary != "" {
		lines = append(lines, "- Close: "+summary)
	}
	if by := strings.TrimSpace(stringField(note.Data, "closed_by")); by != "" {
		lines = append(lines, "- Closed by: "+by)
	}
	if content := strings.TrimSpace(sectionContent(note.Body, "## Verification log")); content != "" {
		limit := atoiSafe(args.String("lines"))
		if limit <= 0 {
			limit = 5
		}
		logLines, total := boundedNonEmptyTail(content, limit)
		lines = append(lines, "", fmt.Sprintf("Verification log: last %d of %d entries", len(logLines), total))
		lines = append(lines, logLines...)
	}
	if len(lines) == 0 {
		lines = append(lines, "_No verification summary or log._")
	}
	return "## Verification\n\n" + strings.Join(lines, "\n") + "\n"
}

func renderTerminalMarkdown(markdown string, args Args) (string, error) {
	width := terminalOutputWidth(args)
	style := strings.TrimSpace(args.String("style"))
	if style == "" {
		style = os.Getenv("GLAMOUR_STYLE")
	}
	if style == "" {
		style = "dark"
	}
	renderer, err := glamour.NewTermRenderer(
		glamour.WithStylePath(style),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		return "", err
	}
	defer renderer.Close()
	return renderer.Render(markdown)
}

func resolveRecordForCLI(args Args, id string) (resolvedRecord, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return resolvedRecord{}, tuskerError(errorMissingArg, "record id is required", withHint("use `tusker print <ID>` or `tusker open <ID>`"))
	}
	var currentVaultErr error
	if args.String("project") == "" {
		vaultPath, err := resolveVaultPath(args, false)
		if err == nil {
			note, noteErr := resolveNote(vaultPath, id)
			if noteErr == nil {
				return resolvedRecord{Note: note, VaultPath: vaultPath, ProjectLabel: filepath.Base(filepath.Dir(vaultPath))}, nil
			}
			currentVaultErr = noteErr
			if args.String("vault") != "" {
				return resolvedRecord{}, noteErr
			}
		} else if args.String("vault") != "" {
			return resolvedRecord{}, err
		}
	}
	projects, err := registeredProjects(args.String("project"))
	if err != nil {
		if currentVaultErr != nil && args.String("project") == "" {
			return resolvedRecord{}, currentVaultErr
		}
		return resolvedRecord{}, err
	}
	var matches []resolvedRecord
	for _, project := range projects {
		note, err := resolveNote(project.VaultRoot, id)
		if err != nil {
			if typed, ok := err.(*TuskerError); ok && typed.Code == errorNotFound {
				continue
			}
			return resolvedRecord{}, err
		}
		matches = append(matches, resolvedRecord{
			Note:         note,
			VaultPath:    project.VaultRoot,
			ProjectID:    project.ProjectID,
			ProjectLabel: registeredProjectLabel(project),
		})
	}
	if len(matches) == 0 {
		return resolvedRecord{}, tuskerError(errorNotFound, "record not found in current vault or registered projects: "+id)
	}
	if len(matches) > 1 {
		var labels []string
		for _, match := range matches {
			labels = append(labels, match.ProjectLabel)
		}
		return resolvedRecord{}, tuskerError(errorInvalidArg, "multiple registered projects contain "+id, withHint("pass --project <id|key|name> or --vault <path>"), withContext(map[string]any{"id": id, "projects": labels}))
	}
	return matches[0], nil
}

func registeredProjects(selector string) ([]RegisteredProject, error) {
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		return nil, err
	}
	defer store.Close()
	projects, err := store.ListProjects()
	if err != nil {
		return nil, err
	}
	selector = strings.TrimSpace(selector)
	if selector == "" {
		if len(projects) == 0 {
			return nil, tuskerError(errorNotFound, "no registered projects", withHint("run `tusker projects add --repo <repo> --vault <repo>/.tusker`"))
		}
		return projects, nil
	}
	var matches []RegisteredProject
	for _, project := range projects {
		if registeredProjectMatches(project, selector) {
			matches = append(matches, project)
		}
	}
	if len(matches) == 0 {
		return nil, tuskerError(errorNotFound, "registered project not found: "+selector)
	}
	return matches, nil
}

func registeredProjectMatches(project RegisteredProject, selector string) bool {
	selector = strings.TrimSpace(selector)
	for _, candidate := range []string{project.ProjectID, project.ProjectKey, project.Name, registeredProjectLabel(project)} {
		if strings.EqualFold(strings.TrimSpace(candidate), selector) {
			return true
		}
	}
	return false
}

func registeredProjectLabel(project RegisteredProject) string {
	return firstNonEmpty(project.ProjectKey, project.Name, project.ProjectID)
}

func obsidianOpenURI(path string) string {
	values := url.Values{}
	values.Set("path", path)
	return "obsidian://open?" + values.Encode()
}

func openNative(target string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", target)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", "", target)
	default:
		cmd = exec.Command("xdg-open", target)
	}
	return runOpenCommand(cmd)
}

func openWithApp(target, app string) error {
	var cmd *exec.Cmd
	if runtime.GOOS == "darwin" {
		cmd = exec.Command("open", "-a", app, target)
	} else {
		cmd = exec.Command(app, target)
	}
	return runOpenCommand(cmd)
}

func openWithEditor(target string) error {
	editor := strings.TrimSpace(firstNonEmpty(os.Getenv("VISUAL"), os.Getenv("EDITOR")))
	if editor == "" {
		return tuskerError(errorMissingArg, "no editor configured", withHint("set VISUAL or EDITOR, or use `tusker open <ID> --path`"))
	}
	if runtime.GOOS == "windows" {
		return runEditorCommand(exec.Command("cmd", "/c", editor, target))
	}
	return runEditorCommand(exec.Command("sh", "-c", editor+" \"$1\"", "tusker-open", target))
}

func runOpenCommand(cmd *exec.Cmd) error {
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

func runEditorCommand(cmd *exec.Cmd) error {
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func cliMuted(text string) string {
	if os.Getenv("TUSKER_COLOR") != "1" || os.Getenv("NO_COLOR") != "" {
		return text
	}
	return lipgloss.NewStyle().Faint(true).Render(text)
}

func ensureTrailingNewline(text string) string {
	if strings.HasSuffix(text, "\n") {
		return text
	}
	return text + "\n"
}
