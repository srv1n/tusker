package main

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	skillbundle "tusker/skill"
)

type noteLoadOptions struct {
	FrontmatterOnly bool
}

type noteFileVersion struct {
	modTime int64
	size    int64
}

type cachedNote struct {
	version noteFileVersion
	note    Note
	hasBody bool
}

type vaultNoteCache struct {
	mu      sync.Mutex
	entries map[string]cachedNote
}

var sharedVaultNoteCaches = struct {
	mu     sync.Mutex
	vaults map[string]*vaultNoteCache
}{vaults: map[string]*vaultNoteCache{}}

// Test-only observers are deliberately functions so production does no counter
// bookkeeping. Callers must use atomics if they install an observer: changed
// files are loaded concurrently.
var (
	noteCacheReadObserver  func()
	noteCacheParseObserver func()
)

func listAllNotes(vaultPath string) ([]Note, error) {
	return listAllNotesWithOptions(vaultPath, noteLoadOptions{})
}

func listAllNotesFrontmatter(vaultPath string) ([]Note, error) {
	return listAllNotesWithOptions(vaultPath, noteLoadOptions{FrontmatterOnly: true})
}

func listAllNotesWithOptions(vaultPath string, opts noteLoadOptions) ([]Note, error) {
	absVaultPath, err := filepath.Abs(vaultPath)
	if err != nil {
		return nil, err
	}
	cache, err := noteCacheForVault(absVaultPath)
	if err != nil {
		return nil, err
	}
	cache.mu.Lock()
	defer cache.mu.Unlock()
	return cache.list(absVaultPath, opts)
}

func noteCacheForVault(vaultPath string) (*vaultNoteCache, error) {
	abs, err := filepath.Abs(vaultPath)
	if err != nil {
		return nil, err
	}
	key := filepath.Clean(abs)
	sharedVaultNoteCaches.mu.Lock()
	defer sharedVaultNoteCaches.mu.Unlock()
	cache := sharedVaultNoteCaches.vaults[key]
	if cache == nil {
		cache = &vaultNoteCache{entries: map[string]cachedNote{}}
		sharedVaultNoteCaches.vaults[key] = cache
	}
	return cache, nil
}

func invalidateCachedNote(filePath string) {
	abs, err := filepath.Abs(filePath)
	if err != nil {
		return
	}
	sharedVaultNoteCaches.mu.Lock()
	caches := make([]*vaultNoteCache, 0, len(sharedVaultNoteCaches.vaults))
	for _, cache := range sharedVaultNoteCaches.vaults {
		caches = append(caches, cache)
	}
	sharedVaultNoteCaches.mu.Unlock()
	for _, cache := range caches {
		cache.mu.Lock()
		delete(cache.entries, filepath.Clean(abs))
		cache.mu.Unlock()
	}
}

func (cache *vaultNoteCache) list(vaultPath string, opts noteLoadOptions) ([]Note, error) {
	paths, err := walkNoteFiles(vaultPath)
	if err != nil {
		return nil, err
	}

	type result struct {
		index int
		note  Note
		err   error
	}

	type job struct {
		index   int
		path    string
		version noteFileVersion
	}
	jobs := make([]job, 0)
	versions := make(map[int]noteFileVersion)
	notes := make([]Note, len(paths))
	seen := make(map[string]struct{}, len(paths))
	for index, filePath := range paths {
		info, err := os.Stat(filePath)
		if err != nil {
			return nil, err
		}
		version := noteFileVersion{modTime: info.ModTime().UnixNano(), size: info.Size()}
		seen[filePath] = struct{}{}
		if entry, ok := cache.entries[filePath]; ok && entry.version == version && (opts.FrontmatterOnly || entry.hasBody) {
			notes[index] = cloneNoteForLoad(entry.note, opts.FrontmatterOnly)
			continue
		}
		jobs = append(jobs, job{index: index, path: filePath, version: version})
		versions[index] = version
	}

	sem := make(chan struct{}, 8)
	done := make(chan result, len(jobs))
	for _, job := range jobs {
		sem <- struct{}{}
		go func(i int, current string) {
			defer func() { <-sem }()
			if noteCacheReadObserver != nil {
				noteCacheReadObserver()
			}
			text, err := readText(current)
			if err != nil {
				done <- result{index: i, err: err}
				return
			}
			if noteCacheParseObserver != nil {
				noteCacheParseObserver()
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
		}(job.index, job.path)
	}

	for range jobs {
		current := <-done
		if current.err != nil {
			return nil, current.err
		}
		entry := cachedNote{version: versions[current.index], note: current.note, hasBody: true}
		if opts.FrontmatterOnly {
			entry.note.Body = ""
			entry.hasBody = false
		}
		cache.entries[current.note.AbsolutePath] = entry
		notes[current.index] = cloneNoteForLoad(entry.note, opts.FrontmatterOnly)
	}
	for path := range cache.entries {
		if _, ok := seen[path]; !ok {
			delete(cache.entries, path)
		}
	}
	return notes, nil
}

func cloneNoteForLoad(note Note, frontmatterOnly bool) Note {
	copy := Note{AbsolutePath: note.AbsolutePath, RelativePath: note.RelativePath, Data: cloneNoteData(note.Data)}
	if !frontmatterOnly {
		copy.Body = note.Body
	}
	return copy
}

func cloneNoteData(data map[string]any) map[string]any {
	if data == nil {
		return nil
	}
	copy := make(map[string]any, len(data))
	for key, value := range data {
		copy[key] = cloneNoteValue(value)
	}
	return copy
}

func cloneNoteValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return cloneNoteData(typed)
	case []any:
		copy := make([]any, len(typed))
		for i, item := range typed {
			copy[i] = cloneNoteValue(item)
		}
		return copy
	case []string:
		return append([]string(nil), typed...)
	default:
		return value
	}
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
			if rel == "_system" || strings.HasPrefix(rel, "_system/") || rel == "_config" || strings.HasPrefix(rel, "_config/") || rel == "Attachments" || strings.HasPrefix(rel, "Attachments/") || rel == ".tusker" || strings.HasPrefix(rel, ".tusker/") {
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
	letter := map[string]string{"task": "T", "doc": "D"}[kind]
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
	for _, field := range []string{"epic"} {
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
