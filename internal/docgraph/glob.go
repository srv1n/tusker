package docgraph

import (
	"path"
	"strings"
)

// hasGlobMeta reports whether a slash path contains glob metacharacters.
func hasGlobMeta(p string) bool { return strings.ContainsAny(p, "*?[") }

// matchGlob matches a slash-separated pattern against a slash-separated path.
// Within a segment `*`, `?` and `[...]` follow path.Match; a whole `**`
// segment matches zero or more path segments.
func matchGlob(pattern, name string) bool {
	return matchGlobSegments(strings.Split(pattern, "/"), strings.Split(name, "/"))
}

// globCovers reports whether a pattern matches name itself or a directory
// containing it, so `src*/` and `internal/*` cover the files below them the
// way a literal directory entry does.
func globCovers(pattern, name string) bool {
	pattern = strings.TrimSuffix(pattern, "/")
	return matchGlob(pattern, name) || matchGlob(pattern+"/**", name)
}

func matchGlobSegments(pattern, name []string) bool {
	for len(pattern) > 0 {
		if pattern[0] == "**" {
			for i := 0; i <= len(name); i++ {
				if matchGlobSegments(pattern[1:], name[i:]) {
					return true
				}
			}
			return false
		}
		if len(name) == 0 {
			return false
		}
		if ok, err := path.Match(pattern[0], name[0]); err != nil || !ok {
			return false
		}
		pattern, name = pattern[1:], name[1:]
	}
	return len(name) == 0
}
