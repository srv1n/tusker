package main

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"errors"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	maxProjectIconBytes     = 512 << 10
	maxProjectIconScanFiles = 20_000
)

var errProjectIconScanLimit = errors.New("project icon scan limit reached")

type projectIconCandidate struct {
	path   string
	mime   string
	score  int
	square bool
	area   int
	depth  int
}

type projectIconScanResult struct {
	candidate projectIconCandidate
	found     bool
}

func serveProjectIconID(path string) (string, bool) {
	const prefix = "/api/projects/"
	const suffix = "/icon"
	if !strings.HasPrefix(path, prefix) || !strings.HasSuffix(path, suffix) {
		return "", false
	}
	id := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(path, prefix), suffix))
	return id, id != "" && !strings.Contains(id, "/")
}

func (s *serveServer) handleProjectIcon(w http.ResponseWriter, r *http.Request, projectID string) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "read-only", http.StatusMethodNotAllowed)
		return
	}
	project, err := s.projectForSnapshot(projectID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	source := "uploaded"
	candidate, ok := s.uploadedProjectIcon(projectID)
	if !ok {
		source = "discovered"
		candidate, ok = s.projectIcon(project.RepoRoot)
	}
	if !ok {
		http.NotFound(w, r)
		return
	}
	file, err := os.Open(candidate.path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || info.IsDir() || info.Size() > maxProjectIconBytes {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", candidate.mime)
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Tusker-Icon-Source", source)
	http.ServeContent(w, r, filepath.Base(candidate.path), info.ModTime(), file)
}

// projectIconGroupKey maps a group or checkout route id to the logical project
// key the uploaded icon is stored under, so every checkout shares one icon.
func (s *serveServer) projectIconGroupKey(projectID string) string {
	if s.store != nil {
		if projects, err := s.store.ListProjects(); err == nil {
			for _, group := range groupRegisteredProjects(projects) {
				if group.ID == projectID || registeredProjectGroupContains(group, projectID) {
					return group.ID
				}
			}
		}
	}
	return projectID
}

func projectIconStoreKey(projectID string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(projectID)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "project"
	}
	return b.String()
}

func projectIconStorePath(projectID string) string {
	return filepath.Join(DefaultStateRoot(), "project-icons", projectIconStoreKey(projectID))
}

// uploadedProjectIcon returns the operator-uploaded icon for a project, if any.
func (s *serveServer) uploadedProjectIcon(projectID string) (projectIconCandidate, bool) {
	dir := filepath.Dir(projectIconStorePath("x"))
	matches, err := filepath.Glob(projectIconStorePath(s.projectIconGroupKey(projectID)) + ".*")
	if err != nil {
		return projectIconCandidate{}, false
	}
	for _, path := range matches {
		if candidate, ok := inspectProjectIcon(dir, path, 200); ok {
			return candidate, true
		}
	}
	return projectIconCandidate{}, false
}

// projectIconUploadType validates an uploaded icon payload and picks the stored
// extension/mime from sniffed content; declared type is trusted only for SVG.
func projectIconUploadType(data []byte, declared string) (ext, mimeType string, ok bool) {
	if declared == "image/svg+xml" && bytes.Contains(bytes.ToLower(data), []byte("<svg")) {
		return ".svg", "image/svg+xml", true
	}
	switch strings.TrimSpace(strings.Split(http.DetectContentType(data), ";")[0]) {
	case "image/png":
		return ".png", "image/png", true
	case "image/jpeg":
		return ".jpg", "image/jpeg", true
	case "image/gif":
		return ".gif", "image/gif", true
	case "image/webp":
		return ".webp", "image/webp", true
	case "image/vnd.microsoft.icon", "image/x-icon":
		return ".ico", "image/x-icon", true
	default:
		return "", "", false
	}
}

func clearStoredProjectIcon(projectID string) error {
	matches, err := filepath.Glob(projectIconStorePath(projectID) + ".*")
	if err != nil {
		return err
	}
	for _, path := range matches {
		if err := os.Remove(path); err != nil {
			return err
		}
	}
	return nil
}

func (s *serveServer) projectIcon(repoRoot string) (projectIconCandidate, bool) {
	root, err := filepath.EvalSymlinks(repoRoot)
	if err != nil {
		return projectIconCandidate{}, false
	}
	// ponytail: one global lock keeps cached scans simple; split per-root only if contention is measurable.
	s.iconMu.Lock()
	defer s.iconMu.Unlock()
	if s.iconCache == nil {
		s.iconCache = map[string]projectIconScanResult{}
	}
	if result, ok := s.iconCache[root]; ok {
		return result.candidate, result.found
	}
	candidate, found := discoverProjectIcon(root)
	s.iconCache[root] = projectIconScanResult{candidate: candidate, found: found}
	return candidate, found
}

func (s *serveServer) invalidateProjectIcon(repoRoot string) {
	root, err := filepath.EvalSymlinks(repoRoot)
	if err != nil {
		return
	}
	s.iconMu.Lock()
	defer s.iconMu.Unlock()
	delete(s.iconCache, root)
}

func discoverProjectIcon(repoRoot string) (projectIconCandidate, bool) {
	root, err := filepath.EvalSymlinks(repoRoot)
	if err != nil {
		return projectIconCandidate{}, false
	}
	var candidates []projectIconCandidate
	files := 0
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if entry.IsDir() {
			if path != root && skipProjectIconDirectory(entry.Name()) {
				return fs.SkipDir
			}
			return nil
		}
		files++
		if files > maxProjectIconScanFiles {
			return errProjectIconScanLimit
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return nil
		}
		base := strings.ToLower(entry.Name())
		switch {
		case base == "package.json" || isProjectIconManifest(base):
			for _, raw := range projectIconJSONPaths(path) {
				if iconPath, ok := resolveProjectIconPath(root, filepath.Dir(path), raw); ok {
					candidates = appendProjectIconCandidate(candidates, root, iconPath, 110)
				}
			}
		case base == "info.plist":
			for _, raw := range projectIconPlistPaths(path) {
				if iconPath, ok := resolveProjectIconPath(root, filepath.Dir(path), raw); ok {
					candidates = appendProjectIconCandidate(candidates, root, iconPath, 105)
				}
			}
		case base == "contents.json" && strings.HasSuffix(filepath.ToSlash(rel), ".appiconset/Contents.json"):
			for _, raw := range projectIconAssetPaths(path) {
				if iconPath, ok := resolveProjectIconPath(root, filepath.Dir(path), raw); ok {
					candidates = appendProjectIconCandidate(candidates, root, iconPath, 120)
				}
			}
		}
		if score, ok := conventionalProjectIconScore(rel); ok {
			candidates = appendProjectIconCandidate(candidates, root, path, score)
		}
		return nil
	})
	if err != nil && !errors.Is(err, errProjectIconScanLimit) {
		return projectIconCandidate{}, false
	}
	if len(candidates) == 0 {
		return projectIconCandidate{}, false
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		left, right := candidates[i], candidates[j]
		if left.score != right.score {
			return left.score > right.score
		}
		if left.square != right.square {
			return left.square
		}
		if left.area != right.area {
			return left.area > right.area
		}
		if left.depth != right.depth {
			return left.depth < right.depth
		}
		return left.path < right.path
	})
	return candidates[0], true
}

func skipProjectIconDirectory(name string) bool {
	switch strings.ToLower(name) {
	case ".git", ".tusker", ".build", "build", "dist", "target", "node_modules", "vendor", ".venv", "deriveddata", "pods", "coverage", ".next":
		return true
	default:
		return false
	}
}

func isProjectIconManifest(name string) bool {
	return (strings.HasPrefix(name, "manifest") && (strings.HasSuffix(name, ".json") || strings.HasSuffix(name, ".webmanifest"))) || name == "site.webmanifest"
}

func projectIconJSONPaths(path string) []string {
	file, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer file.Close()
	var value any
	if err := json.NewDecoder(io.LimitReader(file, 256<<10)).Decode(&value); err != nil {
		return nil
	}
	var paths []string
	collectProjectIconJSONPaths(value, false, &paths)
	return paths
}

func collectProjectIconJSONPaths(value any, iconContext bool, paths *[]string) {
	switch value := value.(type) {
	case string:
		if iconContext {
			*paths = append(*paths, value)
		}
	case []any:
		for _, item := range value {
			collectProjectIconJSONPaths(item, iconContext, paths)
		}
	case map[string]any:
		for key, item := range value {
			collectProjectIconJSONPaths(item, iconContext || isProjectIconJSONKey(key), paths)
		}
	}
}

func isProjectIconJSONKey(key string) bool {
	key = strings.ToLower(strings.ReplaceAll(key, "-", "_"))
	return strings.Contains(key, "icon") || strings.Contains(key, "favicon") || strings.Contains(key, "logo")
}

func projectIconPlistPaths(path string) []string {
	file, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer file.Close()
	decoder := xml.NewDecoder(io.LimitReader(file, 128<<10))
	var paths []string
	wanted := false
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return paths
		}
		start, ok := token.(xml.StartElement)
		if !ok {
			continue
		}
		switch start.Name.Local {
		case "key":
			var key string
			if decoder.DecodeElement(&key, &start) == nil {
				wanted = strings.Contains(strings.ToLower(key), "cfbundleicon")
			}
		case "string":
			var value string
			if decoder.DecodeElement(&value, &start) == nil && wanted {
				paths = append(paths, value)
			}
		}
	}
	return paths
}

func projectIconAssetPaths(path string) []string {
	file, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer file.Close()
	var value struct {
		Images []struct {
			Filename string `json:"filename"`
		} `json:"images"`
	}
	if err := json.NewDecoder(io.LimitReader(file, 128<<10)).Decode(&value); err != nil {
		return nil
	}
	paths := make([]string, 0, len(value.Images))
	for _, image := range value.Images {
		if strings.TrimSpace(image.Filename) != "" {
			paths = append(paths, image.Filename)
		}
	}
	return paths
}

func resolveProjectIconPath(root, base, raw string) (string, bool) {
	raw = strings.TrimSpace(strings.SplitN(raw, "?", 2)[0])
	raw = strings.TrimSpace(strings.SplitN(raw, "#", 2)[0])
	if raw == "" || strings.Contains(raw, "://") || strings.HasPrefix(raw, "data:") {
		return "", false
	}
	raw = strings.TrimPrefix(raw, "/")
	candidates := []string{filepath.Join(base, filepath.FromSlash(raw))}
	if filepath.Ext(raw) == "" {
		for _, ext := range []string{".png", ".svg", ".jpg", ".jpeg", ".gif", ".webp", ".ico"} {
			candidates = append(candidates, candidates[0]+ext)
		}
	}
	for _, candidate := range candidates {
		if resolved, ok := projectIconPathInside(root, candidate); ok {
			return resolved, true
		}
	}
	return "", false
}

func projectIconPathInside(root, path string) (string, bool) {
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", false
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil || !accessPathContains(resolvedRoot, resolved) {
		return "", false
	}
	return resolved, true
}

func appendProjectIconCandidate(candidates []projectIconCandidate, root, path string, score int) []projectIconCandidate {
	info, ok := inspectProjectIcon(root, path, score)
	if !ok {
		return candidates
	}
	for _, candidate := range candidates {
		if candidate.path == info.path {
			return candidates
		}
	}
	return append(candidates, info)
}

func inspectProjectIcon(root, path string, score int) (projectIconCandidate, bool) {
	path, ok := projectIconPathInside(root, path)
	if !ok {
		return projectIconCandidate{}, false
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() == 0 || info.Size() > maxProjectIconBytes {
		return projectIconCandidate{}, false
	}
	ext := strings.ToLower(filepath.Ext(path))
	if !projectIconExtension(ext) {
		return projectIconCandidate{}, false
	}
	candidate := projectIconCandidate{path: path, mime: projectIconMime(ext), score: score, depth: strings.Count(filepath.Clean(path), string(os.PathSeparator))}
	if ext == ".svg" {
		raw, readErr := os.ReadFile(path)
		if readErr != nil || !bytes.Contains(bytes.ToLower(raw), []byte("<svg")) {
			return projectIconCandidate{}, false
		}
		candidate.square = true
		return candidate, true
	}
	if ext == ".webp" || ext == ".ico" {
		return candidate, true
	}
	file, err := os.Open(path)
	if err != nil {
		return projectIconCandidate{}, false
	}
	defer file.Close()
	config, _, err := image.DecodeConfig(file)
	if err != nil || config.Width <= 0 || config.Height <= 0 {
		return projectIconCandidate{}, false
	}
	candidate.square = config.Width == config.Height
	candidate.area = config.Width * config.Height
	return candidate, true
}

func projectIconExtension(ext string) bool {
	switch ext {
	case ".png", ".svg", ".jpg", ".jpeg", ".gif", ".webp", ".ico":
		return true
	default:
		return false
	}
}

func projectIconMime(ext string) string {
	if value := mime.TypeByExtension(ext); value != "" {
		return value
	}
	switch ext {
	case ".svg":
		return "image/svg+xml"
	case ".webp":
		return "image/webp"
	case ".ico":
		return "image/x-icon"
	default:
		return "application/octet-stream"
	}
}

func conventionalProjectIconScore(rel string) (int, bool) {
	base := strings.ToLower(filepath.Base(rel))
	ext := strings.ToLower(filepath.Ext(base))
	if !projectIconExtension(ext) {
		return 0, false
	}
	stem := strings.TrimSuffix(base, ext)
	switch stem {
	case "favicon":
		return 75, true
	case "icon":
		return 70, true
	case "logo":
		return 65, true
	default:
		parent := strings.ToLower(filepath.Base(filepath.Dir(rel)))
		if (parent == "icons" || parent == "assets" || parent == "images" || parent == "public" || parent == "static") && (strings.Contains(stem, "icon") || strings.Contains(stem, "logo")) {
			return 55, true
		}
		return 0, false
	}
}
