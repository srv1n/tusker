package main

import (
	"crypto/sha256"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	skillbundle "tusker/skills/tusker"
)

type noteLoadOptions struct {
	FrontmatterOnly bool
	OperationalOnly bool
}

type noteFileVersion struct {
	modTime int64
	size    int64
}

type cachedNote struct {
	version     noteFileVersion
	contentHash [sha256.Size]byte
	note        Note
	hasBody     bool
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

func listOperationalNotes(vaultPath string) ([]Note, error) {
	return listAllNotesWithOptions(vaultPath, noteLoadOptions{OperationalOnly: true})
}

func listOperationalNotesFrontmatter(vaultPath string) ([]Note, error) {
	return listAllNotesWithOptions(vaultPath, noteLoadOptions{FrontmatterOnly: true, OperationalOnly: true})
}

// loadOperationalNotesByPath reloads only records named by an event. It shares
// the cache used by fallback scans, so a stale or missing path is harmless: the
// caller can fall back to a stat-only full operational scan.
func loadOperationalNotesByPath(vaultPath string, paths []string) ([]Note, error) {
	cache, err := noteCacheForVault(vaultPath)
	if err != nil {
		return nil, err
	}
	cache.mu.Lock()
	defer cache.mu.Unlock()
	result := make([]Note, 0, len(paths))
	for _, path := range paths {
		if !filepath.IsAbs(path) {
			path = filepath.Join(vaultPath, path)
		}
		path = filepath.Clean(path)
		info, err := os.Stat(path)
		if errors.Is(err, os.ErrNotExist) {
			delete(cache.entries, path)
			continue
		}
		if err != nil {
			return nil, err
		}
		version := noteFileVersion{modTime: info.ModTime().UnixNano(), size: info.Size()}
		if entry, ok := cache.entries[path]; ok && entry.version == version {
			result = append(result, cloneNoteForLoad(entry.note, true))
			continue
		}
		text, err := readNoteTextForCache(path)
		if err != nil {
			return nil, err
		}
		if noteCacheParseObserver != nil {
			noteCacheParseObserver()
		}
		data, body, err := parseFrontmatter(text)
		if err != nil {
			return nil, err
		}
		rel, err := filepath.Rel(vaultPath, path)
		if err != nil {
			return nil, err
		}
		note := Note{AbsolutePath: path, RelativePath: filepath.ToSlash(rel), Data: data, Body: body}
		cache.entries[path] = cachedNote{version: version, contentHash: sha256.Sum256([]byte(text)), note: note, hasBody: true}
		result = append(result, cloneNoteForLoad(note, false))
	}
	return result, nil
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
	paths, err := walkNoteFilesForOptions(vaultPath, opts)
	if err != nil {
		return nil, err
	}

	type result struct {
		index       int
		note        Note
		contentHash [sha256.Size]byte
		err         error
	}

	type job struct {
		index   int
		path    string
		version noteFileVersion
		text    string
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
			if noteCacheNeedsCoarseMtimeHashCheck(info.ModTime()) {
				text, err := readNoteTextForCache(filePath)
				if err != nil {
					return nil, err
				}
				if sha256.Sum256([]byte(text)) != entry.contentHash {
					jobs = append(jobs, job{index: index, path: filePath, version: version, text: text})
					versions[index] = version
					continue
				}
			}
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
		go func(i int, current, preloaded string) {
			defer func() { <-sem }()
			text := preloaded
			if text == "" {
				var err error
				text, err = readNoteTextForCache(current)
				if err != nil {
					done <- result{index: i, err: err}
					return
				}
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
				index:       i,
				contentHash: sha256.Sum256([]byte(text)),
				note: Note{
					AbsolutePath: current,
					RelativePath: filepath.ToSlash(rel),
					Data:         data,
					Body:         body,
				},
			}
		}(job.index, job.path, job.text)
	}

	for range jobs {
		current := <-done
		if current.err != nil {
			return nil, current.err
		}
		entry := cachedNote{version: versions[current.index], contentHash: current.contentHash, note: current.note, hasBody: true}
		if opts.FrontmatterOnly {
			entry.note.Body = ""
			entry.hasBody = false
		}
		cache.entries[current.note.AbsolutePath] = entry
		notes[current.index] = cloneNoteForLoad(entry.note, opts.FrontmatterOnly)
	}
	for path := range cache.entries {
		if _, ok := seen[path]; !ok && notePathInLoadScope(vaultPath, path, opts) {
			delete(cache.entries, path)
		}
	}
	return notes, nil
}

func walkNoteFilesForOptions(vaultPath string, opts noteLoadOptions) ([]string, error) {
	if !opts.OperationalOnly {
		return walkNoteFiles(vaultPath)
	}
	workRoot := filepath.Join(vaultPath, "work")
	if !dirExists(workRoot) {
		return []string{}, nil
	}
	return walkNoteFilesFromRoot(vaultPath, workRoot)
}

func notePathInLoadScope(vaultPath, path string, opts noteLoadOptions) bool {
	if !opts.OperationalOnly {
		return true
	}
	return isWithinPath(path, filepath.Join(vaultPath, "work"))
}

func readNoteTextForCache(filePath string) (string, error) {
	if noteCacheReadObserver != nil {
		noteCacheReadObserver()
	}
	return readText(filePath)
}

func noteCacheNeedsCoarseMtimeHashCheck(modTime time.Time) bool {
	if modTime.Nanosecond() != 0 {
		return false
	}
	age := time.Since(modTime)
	return age >= -2*time.Second && age <= 2*time.Second
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
	return walkNoteFilesFromRoot(vaultPath, vaultPath)
}

func walkNoteFilesFromRoot(vaultPath, scanRoot string) ([]string, error) {
	var files []string
	if err := walkDirUnsorted(scanRoot, func(current string, entry fs.DirEntry) error {
		rel, err := filepath.Rel(vaultPath, current)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if entry.IsDir() {
			if rel == "_system" || strings.HasPrefix(rel, "_system/") || rel == "_config" || strings.HasPrefix(rel, "_config/") || rel == "Attachments" || strings.HasPrefix(rel, "Attachments/") || rel == "scratch" || strings.HasPrefix(rel, "scratch/") || rel == ".tusker" || strings.HasPrefix(rel, ".tusker/") {
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
