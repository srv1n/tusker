package main

import (
	"os"
	"sync"

	"tusker/internal/docgraph"
)

// serveSnapshotDerived memoizes display-only wave projections for one full
// snapshot build. buildSnapshotForProject creates a fresh one; the runtime
// refresh path copies the snapshot and keeps it, because wave authorization
// and the integration-projected index read notes, spec files and git refs,
// never runs or leases.
//
// Spec/doc files outside the vault can change without a snapshot rebuild, and
// the next request must see that (the "stale" badge). Each request therefore
// revalidates once: the docgraph.CachedResolver pointer (changes with the doc
// corpus stat signature) plus size/mtime of every spec file the cached
// fingerprints hashed. Any change drops every memoized entry. Integration refs read by the projected
// brief index are not revalidated: they lag until the next full rebuild,
// acceptable because Serve only displays them.
// Mutations (start/arm/stop/...) never read this cache: they authorize
// through the direct wave authority with a fresh waveMaterialReadContext.
//
// Callers must pass the canonical index for snap (serveSnapshotIndex or the
// identical serveSnapshotV7Index); results are keyed by wave ID only.
type serveSnapshotDerived struct {
	// ponytail: one lock per snapshot held across computation, so concurrent
	// first requests compute once; per-wave locks if that ever contends.
	mu         sync.Mutex
	corpus     *docgraph.Resolver        // corpus identity the entries were computed under
	checkedFor *waveMaterialReadContext  // last request context that revalidated corpus
	files      map[string]serveFileStamp // spec files hashed by cached entries
	auth       map[string]map[string]any
	projected  map[string]serveProjectedWave
	computes   int               // authorization projections computed; read by tests
	keyHashes  map[string]string // per-stream-topic content; serveSnapshotMetadataKeyHashes
}

type serveFileStamp struct {
	size    int64
	modNano int64
	missing bool
}

func serveStatStamp(path string) serveFileStamp {
	info, err := os.Stat(path)
	if err != nil {
		return serveFileStamp{missing: true}
	}
	return serveFileStamp{size: info.Size(), modNano: info.ModTime().UnixNano()}
}

type serveProjectedWave struct {
	idx  v7Index
	auth map[string]any
}

func newServeSnapshotDerived() *serveSnapshotDerived {
	return &serveSnapshotDerived{files: map[string]serveFileStamp{}, auth: map[string]map[string]any{}, projected: map[string]serveProjectedWave{}}
}

// serveCachedWaveAuthorization returns the wave's authorization projection
// over the canonical index. The returned map is shared; treat it read-only.
func serveCachedWaveAuthorization(snap serveSnapshot, idx v7Index, wave Note, context *waveMaterialReadContext) map[string]any {
	d := snap.derived
	if d == nil {
		return waveAuthorizationProjectionWithContext(snap.project.VaultRoot, idx, wave, context)
	}
	id := stringField(wave.Data, "id")
	d.lockCurrent(snap, context)
	defer d.mu.Unlock()
	if auth, ok := d.auth[id]; ok {
		return auth
	}
	auth := waveAuthorizationProjectionWithContext(snap.project.VaultRoot, idx, wave, context)
	d.recordFiles(context)
	d.auth[id] = auth
	return auth
}

// serveCachedProjectedWave returns the integration-projected index used by
// the wave brief and the authorization projection over that index.
func serveCachedProjectedWave(snap serveSnapshot, idx v7Index, wave Note, context *waveMaterialReadContext) (v7Index, map[string]any) {
	compute := func() serveProjectedWave {
		// armedWaveBriefProjectedIndex writes Evidence; keep the caller's map intact.
		evidence := make(map[string][]Note, len(idx.Evidence))
		for id, notes := range idx.Evidence {
			evidence[id] = notes
		}
		idx.Evidence = evidence
		if projected, err := armedWaveBriefProjectedIndex(snap.project.VaultRoot, idx, wave); err == nil {
			idx = projected
		}
		return serveProjectedWave{idx: idx, auth: waveAuthorizationProjectionWithContext(snap.project.VaultRoot, idx, wave, context)}
	}
	d := snap.derived
	if d == nil {
		out := compute()
		return out.idx, out.auth
	}
	id := stringField(wave.Data, "id")
	d.lockCurrent(snap, context)
	defer d.mu.Unlock()
	out, ok := d.projected[id]
	if !ok {
		out = compute()
		d.recordFiles(context)
		d.projected[id] = out
	}
	return out.idx, out.auth
}

// lockCurrent takes d.mu and, once per request context, drops all entries if
// the doc corpus changed since they were computed. Callers unlock.
func (d *serveSnapshotDerived) lockCurrent(snap serveSnapshot, context *waveMaterialReadContext) {
	d.mu.Lock()
	if context != nil && context == d.checkedFor {
		return
	}
	// ponytail: holds the request context until the next request replaces it.
	d.checkedFor = context
	corpus, _ := docgraph.CachedResolver(v7RepoRoot(snap.project.VaultRoot))
	changed := corpus != d.corpus
	for path, stamp := range d.files {
		if changed || serveStatStamp(path) != stamp {
			changed = true
			break
		}
	}
	if changed {
		d.corpus = corpus
		clear(d.auth)
		clear(d.projected)
		clear(d.files)
	}
}

// recordFiles counts one computation and stamps the spec files the request
// context hashed. ponytail: stamped after the read, so a write landing between
// read and stat is missed until the next full rebuild; stat first if it bites.
func (d *serveSnapshotDerived) recordFiles(context *waveMaterialReadContext) {
	d.computes++
	if context == nil {
		return
	}
	for path := range context.files {
		if _, ok := d.files[path]; !ok {
			d.files[path] = serveStatStamp(path)
		}
	}
}
