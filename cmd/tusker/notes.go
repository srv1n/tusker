package main

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	skillbundle "tusker/skill"
)

func listAllNotes(vaultPath string) ([]Note, error) {
	paths, err := walkNoteFiles(vaultPath)
	if err != nil {
		return nil, err
	}

	type result struct {
		index int
		note  Note
		err   error
	}

	sem := make(chan struct{}, 8)
	done := make(chan result, len(paths))

	for index, filePath := range paths {
		sem <- struct{}{}
		go func(i int, current string) {
			defer func() { <-sem }()
			text, err := readText(current)
			if err != nil {
				done <- result{index: i, err: err}
				return
			}
			data, body, err := parseFrontmatter(text)
			if err != nil {
				done <- result{index: i, err: err}
				return
			}
			rel, err := filepath.Rel(vaultPath, current)
			if err != nil {
				done <- result{index: i, err: err}
				return
			}
			done <- result{
				index: i,
				note: Note{
					AbsolutePath: current,
					RelativePath: filepath.ToSlash(rel),
					Data:         data,
					Body:         body,
				},
			}
		}(index, filePath)
	}

	notes := make([]Note, len(paths))
	for range paths {
		current := <-done
		if current.err != nil {
			return nil, current.err
		}
		notes[current.index] = current.note
	}
	return notes, nil
}

func walkNoteFiles(vaultPath string) ([]string, error) {
	var files []string
	if err := walkDirUnsorted(vaultPath, func(current string, entry fs.DirEntry) error {
		rel, err := filepath.Rel(vaultPath, current)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if entry.IsDir() {
			if rel == "_system" || strings.HasPrefix(rel, "_system/") || rel == "Attachments" || strings.HasPrefix(rel, "Attachments/") {
				return fs.SkipDir
			}
			return nil
		}
		if rel == "WORKFLOW.md" {
			return nil
		}
		if strings.HasSuffix(current, ".md") {
			files = append(files, current)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return files, nil
}

func walkDirUnsorted(root string, visit func(string, fs.DirEntry) error) error {
	info, err := os.Stat(root)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return nil
	}
	if err := walkDirEntries(root, visit); err != nil {
		return err
	}
	return nil
}

func walkDirEntries(current string, visit func(string, fs.DirEntry) error) error {
	file, err := os.Open(current)
	if err != nil {
		return err
	}
	defer file.Close()

	entries, err := file.ReadDir(-1)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		fullPath := filepath.Join(current, entry.Name())
		if err := visit(fullPath, entry); err != nil {
			if errors.Is(err, fs.SkipDir) && entry.IsDir() {
				continue
			}
			return err
		}
		if entry.IsDir() {
			if err := walkDirEntries(fullPath, visit); err != nil {
				return err
			}
		}
	}
	return nil
}

func resolveNote(vaultPath, idOrLink string) (Note, error) {
	target := wikiTarget(idOrLink)
	notes, err := listAllNotes(vaultPath)
	if err != nil {
		return Note{}, err
	}
	for _, note := range notes {
		if stringField(note.Data, "id") == target || stringField(note.Data, "record_id") == target {
			return note, nil
		}
	}
	return Note{}, tuskerError(errorNotFound, "Note not found: "+target, withContext(map[string]any{"id": target}))
}

func resolveRecordIDsByLink(notes []Note, value any) []string {
	var out []string
	for _, link := range normalizeList(value) {
		target := wikiTarget(link)
		recordID := ""
		for _, note := range notes {
			if stringField(note.Data, "id") == target {
				recordID = stringField(note.Data, "record_id")
				break
			}
		}
		out = append(out, recordID)
	}
	return out
}

func nextSequence(notes []Note, acronym, kind string) int {
	letter := map[string]string{"story": "S", "bug": "B", "doc": "D"}[kind]
	pattern := regexp.MustCompile("^" + acronym + "-" + letter + "-(\\d{4})$")
	max := 0
	for _, note := range notes {
		match := pattern.FindStringSubmatch(stringField(note.Data, "id"))
		if match == nil {
			continue
		}
		max = maxInt(max, atoiSafe(match[1]))
	}
	return max + 1
}

func baseIndexShape(note Note) map[string]any {
	return map[string]any{
		"id":             stringField(note.Data, "id"),
		"record_id":      stringField(note.Data, "record_id"),
		"schema_version": intField(note.Data, "schema_version"),
		"title":          stringField(note.Data, "title"),
		"type":           stringField(note.Data, "type"),
		"status":         stringField(note.Data, "status"),
		"review_state":   stringField(note.Data, "review_state"),
		"work_revision":  intField(note.Data, "work_revision"),
		"path":           note.RelativePath,
		"created":        stringField(note.Data, "created"),
		"updated":        stringField(note.Data, "updated"),
		"tags":           normalizeList(note.Data["tags"]),
	}
}

func collectLinks(note Note) []map[string]any {
	var edges []map[string]any
	source := stringField(note.Data, "id")
	sourceRecordID := stringField(note.Data, "record_id")
	for _, field := range []string{"epic", "story"} {
		value := note.Data[field]
		if value == nil || toString(value) == "" {
			continue
		}
		recordKey := field + "_record_id"
		edges = append(edges, map[string]any{"from": source, "from_record_id": sourceRecordID, "relation": field, "to": wikiTarget(value), "to_record_id": stringField(note.Data, recordKey)})
	}
	for _, field := range []string{"blocked_by", "blocks", "related"} {
		targets := normalizeList(note.Data[field])
		targetIDs := normalizeList(note.Data[field+"_record_ids"])
		for i, value := range targets {
			if value != "" {
				targetRecordID := ""
				if i < len(targetIDs) {
					targetRecordID = targetIDs[i]
				}
				edges = append(edges, map[string]any{"from": source, "from_record_id": sourceRecordID, "relation": field, "to": wikiTarget(value), "to_record_id": targetRecordID})
			}
		}
	}
	return edges
}

func sortByUpdatedDesc(items []map[string]any) {
	sort.Slice(items, func(i, j int) bool {
		return stringValue(items[i]["updated"]) > stringValue(items[j]["updated"])
	})
}

func writeEmbeddedTree(prefix, targetDir string, overwrite bool, report *[]string) error {
	entries, err := skillbundle.AssetEntries(prefix)
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		return nil
	}
	if err := ensureDir(targetDir); err != nil {
		return err
	}
	for _, entry := range entries {
		destination := filepath.Join(targetDir, filepath.FromSlash(entry.Relative))
		exists := fileExists(destination)
		if exists && !overwrite {
			if report != nil {
				*report = append(*report, "Skipped existing "+destination)
			}
			continue
		}
		if err := ensureDir(filepath.Dir(destination)); err != nil {
			return err
		}
		if err := writeText(destination, entry.Content); err != nil {
			return err
		}
		if report != nil {
			action := "Copied"
			if exists {
				action = "Updated"
			}
			*report = append(*report, action+" "+destination)
		}
	}
	return nil
}
