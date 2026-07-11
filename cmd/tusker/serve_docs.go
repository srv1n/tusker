package main

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func (s *serveServer) handleDocs(w http.ResponseWriter, r *http.Request) {
	snap, err := s.loadSnapshotForRequest(r)
	if err != nil {
		serveJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	serveJSON(w, http.StatusOK, snap.docs)
}

func (s *serveServer) handleDoc(w http.ResponseWriter, r *http.Request, rawPath string) {
	project, err := s.projectForSnapshot(strings.TrimSpace(r.URL.Query().Get("project")))
	if err != nil {
		serveJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
		return
	}
	rel := filepath.ToSlash(strings.TrimPrefix(rawPath, "/"))
	full, ok := safeRepoPath(project.RepoRoot, rel)
	if !ok {
		serveJSON(w, http.StatusBadRequest, map[string]any{"error": "path escapes repository"})
		return
	}
	text, err := readText(full)
	if err != nil {
		serveJSON(w, http.StatusNotFound, map[string]any{"error": "doc not found"})
		return
	}
	data, body, err := parseFrontmatter(text)
	if err != nil {
		serveJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	meta := serveDocMeta{
		Path:        rel,
		Title:       serveDocTitle(rel, data, body),
		Kind:        serveDocKind(rel, data),
		UpdatedAt:   serveDocUpdatedAt(full, data),
		Frontmatter: serveDocFrontmatter(data),
	}
	sum := sha256.Sum256([]byte(text))
	serveJSON(w, http.StatusOK, serveDocContent{
		serveDocMeta: meta,
		Markdown:     text,
		Outline:      serveDocOutline(body),
		Rev:          "sha256:" + hex.EncodeToString(sum[:]),
	})
}

func serveDocList(repoRoot, vaultPath string) ([]serveDocListEntry, error) {
	out := []serveDocListEntry{}
	err := filepath.WalkDir(repoRoot, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(repoRoot, path)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)
		if entry.IsDir() {
			if serveSkipDocDir(rel, vaultPath, repoRoot) {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(entry.Name(), ".md") {
			return nil
		}
		text, readErr := readText(path)
		if readErr != nil {
			return nil
		}
		data, body, parseErr := parseFrontmatter(text)
		if parseErr != nil {
			return nil
		}
		out = append(out, serveDocListEntry{
			Path:      rel,
			Title:     serveDocTitle(rel, data, body),
			Kind:      serveDocKind(rel, data),
			UpdatedAt: serveDocUpdatedAt(path, data),
		})
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, err
}

func serveSkipDocDir(rel, vaultPath, repoRoot string) bool {
	if rel == "." {
		return false
	}
	base := filepath.Base(rel)
	if strings.HasPrefix(base, ".") && rel != ".tusker" {
		return true
	}
	if base == "node_modules" || base == "dist" || base == "artifacts" || base == "site" || base == "tmp" {
		return true
	}
	if strings.HasPrefix(rel, ".tusker/attempts") || strings.HasPrefix(rel, ".tusker/events") || strings.HasPrefix(rel, ".tusker/evidence") || strings.HasPrefix(rel, ".tusker/_generated") || strings.HasPrefix(rel, ".tusker/scratch") {
		return true
	}
	_ = vaultPath
	_ = repoRoot
	return false
}

func serveDocTitle(rel string, data map[string]any, body string) string {
	if title := stringField(data, "title"); title != "" {
		return title
	}
	if heading := firstMarkdownHeading(body); heading != "" {
		return heading
	}
	return strings.TrimSuffix(filepath.Base(rel), filepath.Ext(rel))
}

func serveDocKind(rel string, data map[string]any) string {
	switch {
	case strings.Contains(rel, "/tasks/"):
		return "task"
	case strings.Contains(rel, "/epics/"):
		return "epic"
	case strings.Contains(rel, "/decisions/"):
		return "decision"
	case strings.Contains(rel, "/dashboards/"):
		return "dashboard"
	case strings.Contains(rel, "spec"):
		return "spec"
	default:
		if kind := strings.ToLower(firstNonEmpty(stringField(data, "doc_kind"), stringField(data, "kind"))); kind == "decision" {
			return "decision"
		}
		return "knowledge"
	}
}

func serveDocUpdatedAt(path string, data map[string]any) string {
	if value := firstNonEmpty(stringField(data, "updated_at"), stringField(data, "updated"), stringField(data, "created_at"), stringField(data, "created")); value != "" {
		return value
	}
	if info, err := os.Stat(path); err == nil {
		return info.ModTime().UTC().Format(time.RFC3339)
	}
	return ""
}

func serveDocFrontmatter(data map[string]any) []serveDocFrontitem {
	keys := make([]string, 0, len(data))
	for key := range data {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := []serveDocFrontitem{}
	for _, key := range keys {
		out = append(out, serveDocFrontitem{Key: key, Value: toString(data[key]), Locked: true})
	}
	return out
}

func serveDocOutline(body string) []serveDocOutlineEntry {
	out := []serveDocOutlineEntry{}
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "## ") && !strings.HasPrefix(trimmed, "####") {
			text := strings.TrimSpace(strings.TrimPrefix(trimmed, "## "))
			out = append(out, serveDocOutlineEntry{Level: 2, Text: text, Slug: serveSlug(text)})
		} else if strings.HasPrefix(trimmed, "### ") {
			text := strings.TrimSpace(strings.TrimPrefix(trimmed, "### "))
			out = append(out, serveDocOutlineEntry{Level: 3, Text: text, Slug: serveSlug(text)})
		}
	}
	return out
}

func serveSlug(text string) string {
	var b strings.Builder
	prevDash := false
	for _, r := range strings.ToLower(text) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			prevDash = false
			continue
		}
		if !prevDash {
			b.WriteByte('-')
			prevDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

func safeRepoPath(repoRoot, rel string) (string, bool) {
	if rel == "" || strings.Contains(rel, "\x00") {
		return "", false
	}
	cleaned := filepath.Clean(filepath.FromSlash(rel))
	if filepath.IsAbs(cleaned) || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) || cleaned == ".." {
		return "", false
	}
	full := filepath.Join(repoRoot, cleaned)
	absRoot, _ := filepath.Abs(repoRoot)
	absFull, _ := filepath.Abs(full)
	if absFull != absRoot && !strings.HasPrefix(absFull, absRoot+string(filepath.Separator)) {
		return "", false
	}
	return absFull, true
}
