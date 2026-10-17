package docgraph

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

type cachedRepository struct {
	signature [sha256.Size]byte
	resolver  *Resolver
}

var repositoryCache struct {
	sync.Mutex
	byRoot map[string]cachedRepository
}

// CachedResolver returns a resolver over LoadRepository's corpus for
// repoRoot, reusing the previous build while a stat-only signature (path,
// size, mtime of every file the scan reads) is unchanged. Added, removed, or
// rewritten documents change the signature and force a fresh parse. The
// returned Resolver is immutable and safe to share.
//
// ponytail: one entry per repo root, never evicted; fine for a handful of
// projects per process. mtime+size can miss a same-size rewrite within one
// mtime tick; hash contents if that ever matters.
func CachedResolver(repoRoot string) (*Resolver, error) {
	root, err := filepath.Abs(repoRoot)
	if err != nil {
		return nil, err
	}
	paths, err := corpusPaths(root)
	if err != nil {
		return nil, err
	}
	// Stat before reading: a write racing the parse leaves the old
	// signature cached, so the next call sees a new mtime and reparses.
	h := sha256.New()
	for _, path := range paths {
		info, err := os.Lstat(path)
		if err != nil {
			return nil, err
		}
		fmt.Fprintf(h, "%s\x00%d\x00%d\x00", path, info.Size(), info.ModTime().UnixNano())
	}
	var signature [sha256.Size]byte
	h.Sum(signature[:0])
	repositoryCache.Lock()
	cached, ok := repositoryCache.byRoot[root]
	repositoryCache.Unlock()
	if ok && cached.signature == signature {
		return cached.resolver, nil
	}
	documents, _, err := parseCorpusPaths(root, paths)
	if err != nil {
		return nil, err
	}
	resolver := NewResolver(Corpus{Documents: documents})
	repositoryCache.Lock()
	if repositoryCache.byRoot == nil {
		repositoryCache.byRoot = map[string]cachedRepository{}
	}
	repositoryCache.byRoot[root] = cachedRepository{signature: signature, resolver: resolver}
	repositoryCache.Unlock()
	return resolver, nil
}
