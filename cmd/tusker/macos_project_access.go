package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

type macOSProtectedProject struct {
	ProjectID     string `json:"project_id"`
	Name          string `json:"name"`
	RepoRoot      string `json:"repo_root"`
	VaultRoot     string `json:"vault_root"`
	MatchedPath   string `json:"matched_path"`
	ProtectedRoot string `json:"protected_root"`
	Location      string `json:"location"`
}

type macOSProtectedRoot struct {
	Name string
	Path string
}

func macOSProtectedProjects(projects []RegisteredProject, home string) []macOSProtectedProject {
	roots := macOSProtectedRoots(home)
	issues := make([]macOSProtectedProject, 0)
	for _, project := range projects {
		if !project.Enabled {
			continue
		}
		matchedPath, root, ok := macOSProtectedProjectPath(project, roots)
		if !ok {
			continue
		}
		issues = append(issues, macOSProtectedProject{
			ProjectID:     project.ProjectID,
			Name:          project.Name,
			RepoRoot:      project.RepoRoot,
			VaultRoot:     project.VaultRoot,
			MatchedPath:   matchedPath,
			ProtectedRoot: root.Path,
			Location:      root.Name,
		})
	}
	sort.Slice(issues, func(i, j int) bool {
		left := firstNonEmpty(issues[i].ProjectID, issues[i].Name, issues[i].RepoRoot)
		right := firstNonEmpty(issues[j].ProjectID, issues[j].Name, issues[j].RepoRoot)
		return left < right
	})
	return issues
}

func macOSProtectedRoots(home string) []macOSProtectedRoot {
	home = strings.TrimSpace(home)
	if home == "" {
		return nil
	}
	return []macOSProtectedRoot{
		{Name: "Desktop", Path: filepath.Join(home, "Desktop")},
		{Name: "Documents", Path: filepath.Join(home, "Documents")},
		{Name: "Downloads", Path: filepath.Join(home, "Downloads")},
		{Name: "iCloud Drive", Path: filepath.Join(home, "Library", "Mobile Documents")},
	}
}

func macOSProtectedProjectPath(project RegisteredProject, roots []macOSProtectedRoot) (string, macOSProtectedRoot, bool) {
	for _, candidate := range []string{project.RepoRoot, project.VaultRoot} {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		for _, root := range roots {
			if macOSPathWithinRoot(candidate, root.Path) {
				return candidate, root, true
			}
		}
	}
	return "", macOSProtectedRoot{}, false
}

func macOSPathWithinRoot(candidate, root string) bool {
	for _, candidatePath := range macOSPathRepresentations(candidate) {
		for _, rootPath := range macOSPathRepresentations(root) {
			relative, err := filepath.Rel(rootPath, candidatePath)
			if err != nil || filepath.IsAbs(relative) {
				continue
			}
			if relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(os.PathSeparator))) {
				return true
			}
		}
	}
	return false
}

func macOSPathRepresentations(path string) []string {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	if absolute, err := filepath.Abs(path); err == nil {
		path = absolute
	}
	lexical := filepath.Clean(path)
	paths := []string{lexical}
	if resolved := resolvePathWithMissingTail(lexical); resolved != "" && resolved != lexical {
		paths = append(paths, resolved)
	}
	return paths
}

func resolvePathWithMissingTail(path string) string {
	current := filepath.Clean(path)
	missing := []string{}
	for {
		if resolved, err := filepath.EvalSymlinks(current); err == nil {
			for i := len(missing) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, missing[i])
			}
			return filepath.Clean(resolved)
		}
		parent := filepath.Dir(current)
		if parent == current {
			return ""
		}
		missing = append(missing, filepath.Base(current))
		current = parent
	}
}

func macOSProtectedProjectWarning(project RegisteredProject) string {
	if runtime.GOOS != "darwin" {
		return ""
	}
	issues := macOSProtectedProjects([]RegisteredProject{project}, userHomeDir())
	if len(issues) == 0 {
		return ""
	}
	issue := issues[0]
	return fmt.Sprintf("project %s is under macOS-protected %s (%s); manual Tusker commands may work while the launchd daemon is denied access. Move the repository to ~/Developer or ~/Projects before installing the daemon service, or grant the service executable Full Disk Access", firstNonEmpty(project.Name, project.ProjectID), issue.Location, issue.MatchedPath)
}
