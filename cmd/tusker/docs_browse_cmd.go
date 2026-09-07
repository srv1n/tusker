package main

import (
	"fmt"
	"strings"

	"tusker/internal/docgraph"
)

const defaultDocsBrowsePath = docgraph.DocsSystemRoot

func docsBrowseCmd(args Args) error {
	relative := strings.TrimSpace(positionalPhrase(args))
	if relative == "" {
		relative = defaultDocsBrowsePath
	}
	limit, err := docsDiscoveryLimit(args)
	if err != nil {
		return err
	}
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	result, err := docgraph.Browse(v7RepoRoot(vaultPath), relative, limit)
	if err != nil {
		return docsDiscoveryError(err)
	}
	if args.Bool("json") {
		emitJSON(map[string]any{
			"schema":    "tusker.docs-browse/v1",
			"path":      result.Path,
			"entries":   result.Entries,
			"truncated": result.Truncated,
			"limit":     result.Limit,
			"omitted":   result.Omitted,
		})
		return nil
	}
	fmt.Printf("%s\n", result.Path)
	for _, entry := range result.Entries {
		if entry.Kind == "folder" {
			if entry.Summary != "" {
				fmt.Printf("  %s/ — %s\n", entry.Name, entry.Summary)
			} else {
				fmt.Printf("  %s/\n", entry.Name)
			}
			continue
		}
		label := entry.Subject
		if entry.Title != "" && entry.Title != entry.Subject {
			label += " — " + entry.Title
		}
		if entry.Status != "" {
			label += " [" + entry.Status + "]"
		}
		fmt.Printf("  %s — %s\n", entry.Name, label)
	}
	if result.Truncated {
		fmt.Printf("Showing %d entries; %d omitted. Use --limit up to %d.\n", len(result.Entries), result.Omitted, docgraph.MaxBrowseLimit)
	}
	return nil
}
