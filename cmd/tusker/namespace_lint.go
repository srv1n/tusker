package main

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

func validateCollisionProneNamespaces(vaultPath string) []Issue {
	wf, err := loadWorkflow(vaultPath)
	if err != nil || len(wf.Data.Orchestration.NamespaceLints) == 0 {
		return nil
	}
	repoRoot := v7RepoRoot(vaultPath)
	var issues []Issue
	for _, pattern := range wf.Data.Orchestration.NamespaceLints {
		if strings.TrimSpace(pattern.Name) == "" || strings.TrimSpace(pattern.Glob) == "" || strings.TrimSpace(pattern.CaptureRegex) == "" {
			issues = append(issues, issue("NAMESPACE_LINT_CONFIG", "namespace lint requires name, glob, and capture_regex", "WORKFLOW.md", "configure a named collision key capture", map[string]any{"pattern": pattern.Name}))
			continue
		}
		capture, compileErr := regexp.Compile(pattern.CaptureRegex)
		globRE, globErr := compileNamespaceGlob(pattern.Glob)
		if compileErr != nil || globErr != nil || capture.NumSubexp() < 1 {
			issues = append(issues, issue("NAMESPACE_LINT_CONFIG", "invalid namespace lint "+pattern.Name, "WORKFLOW.md", "capture_regex must compile and contain a capture group", map[string]any{"pattern": pattern.Name}))
			continue
		}
		claims := map[string][]string{}
		_ = filepath.WalkDir(repoRoot, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return nil
			}
			if entry.IsDir() {
				if entry.Name() == ".git" || (path != vaultPath && strings.HasPrefix(path, vaultPath+string(filepath.Separator))) {
					return filepath.SkipDir
				}
				return nil
			}
			rel, relErr := filepath.Rel(repoRoot, path)
			if relErr != nil {
				return nil
			}
			rel = filepath.ToSlash(rel)
			if !globRE.MatchString(rel) {
				return nil
			}
			match := capture.FindStringSubmatch(rel)
			if len(match) < 2 || strings.TrimSpace(match[1]) == "" {
				issues = append(issues, issue("NAMESPACE_SEQUENCE_POLICY", fmt.Sprintf("%s matched %s but capture_regex produced no sequence key", pattern.Name, rel), rel, pattern.NamingRecommendation, map[string]any{"pattern": pattern.Name, "glob": pattern.Glob}))
				return nil
			}
			claims[match[1]] = append(claims[match[1]], rel)
			return nil
		})
		keys := make([]string, 0, len(claims))
		for key := range claims {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			paths := claims[key]
			if len(paths) < 2 {
				continue
			}
			sort.Strings(paths)
			hint := firstNonEmpty(pattern.NamingRecommendation, "use a collision-resistant name such as a timestamp")
			issues = append(issues, issue("NAMESPACE_COLLISION", fmt.Sprintf("namespace lint %s: key %s is claimed by %s", pattern.Name, key, strings.Join(paths, ", ")), paths[0], hint, map[string]any{"pattern": pattern.Name, "glob": pattern.Glob, "key": key, "paths": paths}))
		}
	}
	return issues
}

func compileNamespaceGlob(glob string) (*regexp.Regexp, error) {
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(glob); i++ {
		switch glob[i] {
		case '*':
			if i+1 < len(glob) && glob[i+1] == '*' {
				b.WriteString(".*")
				i++
			} else {
				b.WriteString("[^/]*")
			}
		case '?':
			b.WriteString("[^/]")
		case '.', '+', '(', ')', '|', '^', '$', '{', '}', '[', ']', '\\':
			b.WriteByte('\\')
			b.WriteByte(glob[i])
		default:
			b.WriteByte(glob[i])
		}
	}
	b.WriteString("$")
	return regexp.Compile(b.String())
}
