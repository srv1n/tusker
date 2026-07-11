package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

type traceGitHeadCacheEntry struct {
	signature string
	sha       string
}

type traceGitHeadCache struct {
	mu       sync.Mutex
	entries  map[string]traceGitHeadCacheEntry
	resolver func(string) string
}

var sharedTraceGitHeadCache = newTraceGitHeadCache(resolveGitHeadWithCommand)

func newTraceGitHeadCache(resolver func(string) string) *traceGitHeadCache {
	return &traceGitHeadCache{entries: map[string]traceGitHeadCacheEntry{}, resolver: resolver}
}

func (cache *traceGitHeadCache) resolve(repoRoot string) string {
	repoRoot = strings.TrimSpace(repoRoot)
	if repoRoot == "" {
		return "unknown"
	}
	absRoot, err := filepath.Abs(repoRoot)
	if err != nil {
		return "unknown"
	}
	signature, err := traceGitHeadSignature(absRoot)
	if err != nil {
		return "unknown"
	}
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if cached, ok := cache.entries[absRoot]; ok && cached.signature == signature {
		return cached.sha
	}
	sha := cache.resolver(absRoot)
	cache.entries[absRoot] = traceGitHeadCacheEntry{signature: signature, sha: sha}
	return sha
}

func resolveGitHeadWithCommand(repoRoot string) string {
	out, err := exec.Command("git", "-C", repoRoot, "rev-parse", "HEAD").Output()
	if err != nil {
		return "unknown"
	}
	if sha := strings.TrimSpace(string(out)); sha != "" {
		return sha
	}
	return "unknown"
}

func traceGitHeadSignature(repoRoot string) (string, error) {
	gitDir, err := traceGitDirectory(repoRoot)
	if err != nil {
		return "", err
	}
	commonDir := gitDir
	if raw, err := os.ReadFile(filepath.Join(gitDir, "commondir")); err == nil {
		commonDir = strings.TrimSpace(string(raw))
		if !filepath.IsAbs(commonDir) {
			commonDir = filepath.Join(gitDir, commonDir)
		}
		commonDir = filepath.Clean(commonDir)
	}
	headPath := filepath.Join(gitDir, "HEAD")
	headRaw, err := os.ReadFile(headPath)
	if err != nil {
		return "", err
	}
	paths := []string{headPath}
	if ref := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(string(headRaw)), "ref:")); strings.HasPrefix(strings.TrimSpace(string(headRaw)), "ref:") && ref != "" {
		paths = append(paths, filepath.Join(commonDir, filepath.FromSlash(ref)))
		if !sameCleanPath(commonDir, gitDir) {
			paths = append(paths, filepath.Join(gitDir, filepath.FromSlash(ref)))
		}
	}
	paths = append(paths, filepath.Join(commonDir, "packed-refs"))
	parts := []string{strings.TrimSpace(string(headRaw))}
	for _, path := range paths {
		version, err := traceGitMetadataVersion(path)
		if err != nil {
			return "", err
		}
		parts = append(parts, version)
	}
	return strings.Join(parts, "\x00"), nil
}

func traceGitDirectory(repoRoot string) (string, error) {
	dotGit := filepath.Join(repoRoot, ".git")
	info, err := os.Stat(dotGit)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return dotGit, nil
	}
	raw, err := os.ReadFile(dotGit)
	if err != nil {
		return "", err
	}
	value := strings.TrimSpace(string(raw))
	if !strings.HasPrefix(value, "gitdir:") {
		return "", fmt.Errorf("invalid .git file in %s", repoRoot)
	}
	gitDir := strings.TrimSpace(strings.TrimPrefix(value, "gitdir:"))
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(repoRoot, gitDir)
	}
	return filepath.Clean(gitDir), nil
}

func traceGitMetadataVersion(path string) (string, error) {
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return path + "=missing", nil
	}
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s=%d:%d", path, info.ModTime().UnixNano(), info.Size()), nil
}
