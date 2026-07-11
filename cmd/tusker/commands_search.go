package main

import (
	"fmt"
	"sort"
	"strings"
)

type searchResult struct {
	ID      string            `json:"id"`
	Type    string            `json:"type"`
	Kind    string            `json:"kind,omitempty"`
	Status  string            `json:"status,omitempty"`
	Epic    string            `json:"epic,omitempty"`
	Title   string            `json:"title"`
	Path    string            `json:"path"`
	Capsule map[string]string `json:"capsule,omitempty"`
	Snippet string            `json:"snippet,omitempty"`
}

func searchCmd(args Args) error {
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	query := strings.TrimSpace(firstNonEmpty(args.String("query"), strings.ReplaceAll(args.String("_pos"), "\n", " "), args.String("_pos0")))
	if query == "" {
		return tuskerError(errorMissingArg, "search requires a query", withHint("use `tusker search <text>` or `tusker search --query <text>`"))
	}
	limit := atoiSafe(args.String("limit"))
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	filterType := strings.ToLower(strings.TrimSpace(args.String("type")))
	filterStatus := strings.ToLower(strings.TrimSpace(args.String("status")))
	filterEpic := strings.ToUpper(strings.TrimSpace(args.String("epic")))

	notes, err := listAllNotes(vaultPath)
	if err != nil {
		return err
	}
	matches := searchNotes(notes, query, searchFilters{
		Type:   filterType,
		Status: filterStatus,
		Epic:   filterEpic,
		Limit:  limit,
	})
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": true, "query": query, "items": matches})
		return nil
	}
	if len(matches) == 0 {
		fmt.Printf("No tracker notes matched %q\n", query)
		return nil
	}
	for _, item := range matches {
		fmt.Printf("%-14s  %-5s  %-8s  %-6s  %s\n", item.ID, item.Type, item.Status, item.Epic, item.Title)
		fmt.Printf("  %s\n", item.Path)
		if capsule := capsuleOneLine(Note{Data: map[string]any{"capsule": item.Capsule}}); capsule != "" {
			fmt.Printf("  Capsule: %s\n", capsule)
		}
		if item.Snippet != "" {
			fmt.Printf("  %s\n", item.Snippet)
		}
		if what := item.Capsule["what"]; what != "" {
			fmt.Printf("  capsule.what: %s\n", what)
		}
	}
	return nil
}

type searchFilters struct {
	Type   string
	Status string
	Epic   string
	Limit  int
}

func searchNotes(notes []Note, query string, filters searchFilters) []searchResult {
	needle := strings.ToLower(query)
	var out []searchResult
	for _, note := range notes {
		noteType := strings.ToLower(noteDisplayKind(note.Data))
		legacyType := strings.ToLower(stringField(note.Data, "type"))
		kind := strings.ToLower(stringField(note.Data, "kind"))
		status := strings.ToLower(stringField(note.Data, "status"))
		epic := strings.ToUpper(wikiTarget(note.Data["epic"]))
		if filters.Type != "" && noteType != filters.Type && legacyType != filters.Type && kind != filters.Type {
			continue
		}
		if filters.Status != "" && status != filters.Status {
			continue
		}
		if filters.Epic != "" && epic != filters.Epic && strings.ToUpper(stringField(note.Data, "id")) != filters.Epic {
			continue
		}
		searchText := strings.Join([]string{
			stringField(note.Data, "id"),
			stringField(note.Data, "title"),
			stringField(note.Data, "kind"),
			stringField(note.Data, "status"),
			epic,
			note.RelativePath,
			v7CapsuleSearchText(note),
			note.Body,
		}, "\n")
		if !strings.Contains(strings.ToLower(searchText), needle) {
			continue
		}
		out = append(out, searchResult{
			ID:      stringField(note.Data, "id"),
			Type:    noteDisplayKind(note.Data),
			Kind:    stringField(note.Data, "kind"),
			Status:  stringField(note.Data, "status"),
			Epic:    epic,
			Title:   stringField(note.Data, "title"),
			Path:    note.RelativePath,
			Capsule: v7CapsuleMap(note),
			Snippet: compactSnippet(searchText, needle, 180),
			Capsule: capsulePayload(note),
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Status != out[j].Status {
			return out[i].Status < out[j].Status
		}
		if out[i].Epic != out[j].Epic {
			return out[i].Epic < out[j].Epic
		}
		return out[i].ID < out[j].ID
	})
	if filters.Limit > 0 && len(out) > filters.Limit {
		return out[:filters.Limit]
	}
	return out
}

func compactSnippet(text, lowerNeedle string, maxLen int) string {
	flat := strings.Join(strings.Fields(text), " ")
	if flat == "" {
		return ""
	}
	lowerFlat := strings.ToLower(flat)
	index := strings.Index(lowerFlat, lowerNeedle)
	if index < 0 {
		if len(flat) <= maxLen {
			return flat
		}
		return flat[:maxLen] + "..."
	}
	start := index - maxLen/3
	if start < 0 {
		start = 0
	}
	end := start + maxLen
	if end > len(flat) {
		end = len(flat)
		start = maxInt(0, end-maxLen)
	}
	prefix := ""
	if start > 0 {
		prefix = "..."
	}
	suffix := ""
	if end < len(flat) {
		suffix = "..."
	}
	return prefix + flat[start:end] + suffix
}
