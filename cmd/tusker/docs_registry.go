package main

import (
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type docsPublicationRegistry struct {
	RepoDocs []docsRegistryEntry `yaml:"repo_docs"`
}

type docsRegistryEntry struct {
	Source          string   `yaml:"source"`
	Include         string   `yaml:"include"`
	RoutePrefix     string   `yaml:"route_prefix"`
	Route           string   `yaml:"route"`
	Audience        string   `yaml:"audience"`
	SectionTitle    string   `yaml:"section_title"`
	Tags            []string `yaml:"tags"`
	Internal        bool     `yaml:"internal"`
	Title           string   `yaml:"title"`
	Description     string   `yaml:"description"`
	Canonical       bool     `yaml:"canonical"`
	CanonicalStatus string   `yaml:"canonical_status"`
	OwnerEpic       string   `yaml:"owner_epic"`
	VerifiedAt      string   `yaml:"verified_at"`
	Deprecated      bool     `yaml:"deprecated"`
	SupersededBy    string   `yaml:"superseded_by"`
	RedirectFrom    []string `yaml:"redirect_from"`
}

type docsRegistrySource struct {
	Entry         docsRegistryEntry
	SourceAbsPath string
	SourcePath    string
	RoutePath     string
	FromDirectory bool
}

func loadDocsRegistry(repoRoot string) ([]docsRegistrySource, error) {
	registryPath := filepath.Join(repoRoot, docsRegistryRelative)
	if !fileExists(registryPath) {
		return nil, nil
	}
	raw, err := os.ReadFile(registryPath)
	if err != nil {
		return nil, err
	}
	var registry docsPublicationRegistry
	if err := yaml.Unmarshal(raw, &registry); err != nil {
		return nil, err
	}
	return expandDocsRegistry(repoRoot, registry.RepoDocs)
}

func expandDocsRegistry(repoRoot string, entries []docsRegistryEntry) ([]docsRegistrySource, error) {
	var out []docsRegistrySource
	for _, entry := range entries {
		source := docsNormalizePath(entry.Source)
		if source == "" {
			return nil, tuskerError(errorInvalidField, "docs/publication.yaml entry is missing source", withPath(docsRegistryRelative))
		}
		include := strings.TrimSpace(entry.Include)
		if include == "" {
			include = "**/*.md"
		}
		sourceAbs := filepath.Join(repoRoot, filepath.FromSlash(source))
		info, err := os.Stat(sourceAbs)
		if err != nil {
			return nil, tuskerError(errorNotFound, "Registry source not found: "+source, withPath(docsRegistryRelative))
		}
		if info.IsDir() {
			if strings.TrimSpace(entry.RoutePrefix) == "" {
				return nil, tuskerError(errorInvalidField, "Directory registry source requires route_prefix: "+source, withPath(docsRegistryRelative))
			}
			paths, err := docsWalkRegistryDirectory(sourceAbs)
			if err != nil {
				return nil, err
			}
			for _, current := range paths {
				rel := docsNormalizePath(strings.TrimPrefix(current, sourceAbs))
				rel = strings.TrimPrefix(rel, "/")
				if !docsGlobMatch(include, rel) {
					continue
				}
				routeTail := docsRegistryRouteTail(rel)
				route := docsJoinRoute(entry.RoutePrefix, routeTail)
				out = append(out, docsRegistrySource{
					Entry:         entry,
					SourceAbsPath: current,
					SourcePath:    docsRelativeTo(repoRoot, current),
					RoutePath:     route,
					FromDirectory: true,
				})
			}
			continue
		}
		if strings.TrimSpace(entry.Route) == "" && strings.TrimSpace(entry.RoutePrefix) == "" {
			return nil, tuskerError(errorInvalidField, "File registry source requires route or route_prefix: "+source, withPath(docsRegistryRelative))
		}
		route := strings.TrimSpace(entry.Route)
		if route == "" {
			route = docsJoinRoute(entry.RoutePrefix, docsRegistryRouteTail(filepath.Base(sourceAbs)))
		}
		out = append(out, docsRegistrySource{
			Entry:         entry,
			SourceAbsPath: sourceAbs,
			SourcePath:    source,
			RoutePath:     route,
		})
	}
	docsSortRegistrySources(out)
	return out, nil
}

func docsSortRegistrySources(items []docsRegistrySource) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].RoutePath != items[j].RoutePath {
			return items[i].RoutePath < items[j].RoutePath
		}
		return items[i].SourcePath < items[j].SourcePath
	})
}

func docsWalkRegistryDirectory(root string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(current string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		name := strings.ToLower(entry.Name())
		if strings.HasSuffix(name, ".md") || strings.HasSuffix(name, ".mdx") {
			files = append(files, current)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	return files, nil
}

func docsRegistryRouteTail(rel string) string {
	rel = docsNormalizePath(rel)
	ext := path.Ext(rel)
	trimmed := strings.TrimSuffix(rel, ext)
	segments := strings.Split(trimmed, "/")
	out := make([]string, 0, len(segments))
	for i, segment := range segments {
		segment = strings.TrimSpace(segment)
		if segment == "" {
			continue
		}
		if i == len(segments)-1 && (strings.EqualFold(segment, "readme") || strings.EqualFold(segment, "index")) {
			continue
		}
		out = append(out, docsSlugifySegment(segment))
	}
	return strings.Join(out, "/")
}

func docsGlobMatch(pattern, value string) bool {
	pattern = docsNormalizePath(pattern)
	value = docsNormalizePath(value)
	if pattern == "" {
		return false
	}
	regex := docsGlobToRegex(pattern)
	matched, _ := regexp.MatchString("^"+regex+"$", value)
	return matched
}

func docsGlobToRegex(pattern string) string {
	var b strings.Builder
	for i := 0; i < len(pattern); i++ {
		switch pattern[i] {
		case '*':
			if i+1 < len(pattern) && pattern[i+1] == '*' {
				if i+2 < len(pattern) && pattern[i+2] == '/' {
					b.WriteString(`(?:.*/)?`)
					i += 2
					continue
				}
				b.WriteString(`.*`)
				i++
				continue
			}
			b.WriteString(`[^/]*`)
		case '?':
			b.WriteString(`[^/]`)
		case '[':
			end := strings.IndexByte(pattern[i+1:], ']')
			if end < 0 {
				b.WriteString(`\[`)
				continue
			}
			end += i + 1
			b.WriteString(pattern[i : end+1])
			i = end
		default:
			if strings.ContainsRune(`.+()|^$@%{}\`, rune(pattern[i])) {
				b.WriteByte('\\')
			}
			b.WriteByte(pattern[i])
		}
	}
	return b.String()
}
