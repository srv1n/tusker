package main

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"tusker/internal/docgraph"
)

// docsCmd dispatches the docs subcommands through a table so new subcommands
// (map is wired at integration) slot in without touching the caller, and an
// unknown subcommand fails cleanly rather than falling through.
func docsCmd(subcommand string, args Args) error {
	handlers := map[string]func(Args) error{
		"find": docsFindCmd,
		"new":  docsNewCmd,
		"map":  docsMapCmd,
	}
	handler, ok := handlers[subcommand]
	if !ok {
		return tuskerError(errorInvalidArg, "unknown docs subcommand: "+subcommand)
	}
	return handler(args)
}

func docsFindCmd(args Args) error {
	query := positionalPhrase(args)
	if query == "" {
		return tuskerError(errorMissingArg, "docs find needs a query, for example: tusker docs find worktree")
	}
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	corpus, _, err := docgraph.LoadRepository(v7RepoRoot(vaultPath))
	if err != nil {
		return err
	}
	result := docgraph.Find(corpus, query)
	if args.Bool("json") {
		emitJSON(result)
		return nil
	}
	fmt.Print(renderDocsFind(result))
	return nil
}

func renderDocsFind(result docgraph.FindResult) string {
	var builder strings.Builder
	if len(result.Matches) == 0 {
		fmt.Fprintf(&builder, "No document matches %q.\n", result.Query)
		if len(result.Suggestions) > 0 {
			builder.WriteString("Closest subjects:\n")
			for _, suggestion := range result.Suggestions {
				fmt.Fprintf(&builder, "  %s\n", suggestion)
			}
		}
		return builder.String()
	}
	for _, match := range result.Matches {
		line := match.Path
		if match.Description != "" {
			line += " — " + match.Description
		}
		if match.ResolvedFrom != "" {
			line += fmt.Sprintf(" (resolved forward from superseded %q)", match.ResolvedFrom)
		}
		builder.WriteString(line + "\n")
	}
	return builder.String()
}

func docsMapCmd(args Args) error {
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	repoRoot := v7RepoRoot(vaultPath)
	if err := docgraph.WriteDocsMap(repoRoot); err != nil {
		return err
	}
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": true})
		return nil
	}
	fmt.Println("Docs map, index, and graph regenerated.")
	return nil
}

func docsNewCmd(args Args) error {
	subject := positionalPhrase(args)
	if subject == "" {
		return tuskerError(errorMissingArg, "docs new needs a subject, for example: tusker docs new worktree-lifecycle")
	}
	kind := strings.TrimSpace(strings.ToLower(args.String("kind")))
	if kind == "" {
		kind = "doc"
	}
	var targetDir string
	switch kind {
	case "doc":
		targetDir = "docs/system"
	case "spec":
		targetDir = ".tusker/specs"
	default:
		return tuskerError(errorInvalidArg, `docs new --kind must be "doc" or "spec", got: `+kind)
	}

	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	repoRoot := v7RepoRoot(vaultPath)
	corpus, _, err := docgraph.LoadRepository(repoRoot)
	if err != nil {
		return err
	}
	for _, doc := range corpus.Documents {
		if strings.EqualFold(strings.TrimSpace(doc.Subject), subject) {
			return tuskerError(errorAlreadyExists, fmt.Sprintf("a document about %q already exists at %s; update it in place instead of starting a second copy", subject, doc.Path))
		}
	}

	relative := filepath.ToSlash(filepath.Join(targetDir, docsSubjectSlug(subject)+".md"))
	absolute := filepath.Join(repoRoot, filepath.FromSlash(relative))
	if fileExists(absolute) {
		return tuskerError(errorAlreadyExists, "a file already exists at "+relative+"; pick a different subject or update that file")
	}
	if err := writeText(absolute, docsScaffold(subject)); err != nil {
		return err
	}
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": true, "path": relative, "subject": subject, "kind": kind})
		return nil
	}
	fmt.Printf("Created %s\n", relative)
	return nil
}

func positionalPhrase(args Args) string {
	return strings.TrimSpace(strings.ReplaceAll(args.String("_pos"), "\n", " "))
}

var docsSlugPattern = regexp.MustCompile(`[^a-z0-9]+`)

func docsSubjectSlug(subject string) string {
	slug := docsSlugPattern.ReplaceAllString(strings.ToLower(strings.TrimSpace(subject)), "-")
	slug = strings.Trim(slug, "-")
	if slug == "" {
		return "untitled"
	}
	return slug
}

func docsScaffold(subject string) string {
	created := time.Now().Local().Format("2006-01-02")
	var builder strings.Builder
	builder.WriteString("---\n")
	fmt.Fprintf(&builder, "subject: %s            # unique key; the one right name for this document\n", subject)
	builder.WriteString("keywords: []            # search aliases a reader might type instead of the subject\n")
	builder.WriteString("part_of:                # subject of the parent document this sits under\n")
	builder.WriteString("status: canonical       # canonical, or superseded (then set superseded_by)\n")
	fmt.Fprintf(&builder, "created: %s      # date this document was first written\n", created)
	builder.WriteString("read_when: \"\"           # one line: when a reader should open this\n")
	builder.WriteString("skip_when: \"\"           # one line: when a reader should look elsewhere\n")
	builder.WriteString("---\n\n")
	fmt.Fprintf(&builder, "# %s\n", subject)
	return builder.String()
}
