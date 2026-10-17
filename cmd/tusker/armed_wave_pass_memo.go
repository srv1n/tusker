package main

import "maps"

// armedWavePassMemo shares armed-wave reads across one project's candidate
// collection inside a daemon poll pass. Without it every candidate task
// reloaded the v7 index, re-hashed its wave material and re-ran the wave's
// integration git probes, so N members of one wave did the same work N times.
//
// Scope: the daemon creates one memo per project collection closure in
// pollOnce and drops it before fair dispatch, whose refresh deliberately
// rereads canonical state. Inside that stretch the poll only writes runtime
// rows; any canonical v7 document write in this process bumps
// v7DocumentWriteGeneration, which resets the memo on its next use. Edits from
// other processes during the stretch are seen by the next pass and by the
// dispatch refresh, never by a claim made from this memo.
//
// Not safe for concurrent use; the poll loop owns it. A nil memo is valid and
// means "read fresh", which keeps every existing caller unchanged.
type armedWavePassMemo struct {
	vaultPath  string
	generation uint64
	loaded     bool
	idx        v7Index
	err        error
	material   *waveMaterialReadContext
	auth       map[string]map[string]any
	projected  map[string]armedWaveProjection
	gitRepos   map[string]bool
	branches   map[string]bool
	refNotes   map[string]armedWaveRefNote
}

type armedWaveProjection struct {
	idx    v7Index
	errors map[string]string
}

type armedWaveRefNote struct {
	note Note
	ok   bool
	err  error
}

// armedWaveAuthorizationObserver lets tests count wave authorization
// computations made through armed-wave lookups.
var armedWaveAuthorizationObserver func()

func newArmedWavePassMemo(vaultPath string) *armedWavePassMemo {
	m := &armedWavePassMemo{vaultPath: vaultPath}
	m.reset()
	return m
}

func (m *armedWavePassMemo) reset() {
	m.generation = v7DocumentWriteGeneration.Load()
	m.loaded, m.idx, m.err = false, v7Index{}, nil
	m.material = newWaveMaterialReadContext()
	m.auth = map[string]map[string]any{}
	m.projected = map[string]armedWaveProjection{}
	m.gitRepos = map[string]bool{}
	m.branches = map[string]bool{}
	m.refNotes = map[string]armedWaveRefNote{}
}

// usable reports whether the memo may answer for vaultPath, first dropping
// everything if a canonical document was written since it was filled.
func (m *armedWavePassMemo) usable(vaultPath string) bool {
	if m == nil || m.vaultPath != vaultPath {
		return false
	}
	if m.generation != v7DocumentWriteGeneration.Load() {
		m.reset()
	}
	return true
}

func (m *armedWavePassMemo) index(vaultPath string) (v7Index, error) {
	if !m.usable(vaultPath) {
		return loadV7Index(vaultPath)
	}
	if !m.loaded {
		m.idx, m.err = loadV7Index(vaultPath)
		m.loaded = true
	}
	idx := m.idx
	// Callers may overlay in-memory task views; keep the memo's map intact.
	idx.Tasks = maps.Clone(m.idx.Tasks)
	return idx, m.err
}

func (m *armedWavePassMemo) authorization(vaultPath string, idx v7Index, wave Note) map[string]any {
	if !m.usable(vaultPath) {
		if armedWaveAuthorizationObserver != nil {
			armedWaveAuthorizationObserver()
		}
		return waveAuthorizationProjection(vaultPath, idx, wave)
	}
	waveID := stringField(wave.Data, "id")
	if auth, ok := m.auth[waveID]; ok {
		return auth
	}
	if armedWaveAuthorizationObserver != nil {
		armedWaveAuthorizationObserver()
	}
	auth := waveAuthorizationProjectionWithContext(vaultPath, idx, wave, m.material)
	m.auth[waveID] = auth
	return auth
}

// projectedIndex memoizes armedWaveProjectedIndex over the memo's own index,
// so a wave's integration git probes run once per pass.
func (m *armedWavePassMemo) projectedIndex(vaultPath string, idx v7Index, wave Note) (v7Index, map[string]string) {
	if !m.usable(vaultPath) {
		return armedWaveProjectedIndexWithMemo(m, vaultPath, idx, wave)
	}
	waveID := stringField(wave.Data, "id")
	cached, ok := m.projected[waveID]
	if !ok {
		generation := m.generation
		cached.idx, cached.errors = armedWaveProjectedIndexWithMemo(m, vaultPath, idx, wave)
		if m.generation == generation && v7DocumentWriteGeneration.Load() == generation {
			m.projected[waveID] = cached
		}
	}
	projected := cached.idx
	projected.Tasks = maps.Clone(cached.idx.Tasks)
	return projected, maps.Clone(cached.errors)
}

func (m *armedWavePassMemo) gitRepo(vaultPath, repoRoot string) bool {
	if !m.usable(vaultPath) {
		return v7GitRepo(repoRoot)
	}
	ok, cached := m.gitRepos[repoRoot]
	if !cached {
		ok = v7GitRepo(repoRoot)
		m.gitRepos[repoRoot] = ok
	}
	return ok
}

func (m *armedWavePassMemo) branchExists(vaultPath, repoRoot, branch string) bool {
	if !m.usable(vaultPath) {
		return gitBranchExists(repoRoot, branch)
	}
	key := repoRoot + "\x00" + branch
	ok, cached := m.branches[key]
	if !cached {
		ok = gitBranchExists(repoRoot, branch)
		m.branches[key] = ok
	}
	return ok
}

func (m *armedWavePassMemo) noteAtRef(vaultPath, repoRoot, ref, rel string) (Note, bool, error) {
	if !m.usable(vaultPath) {
		return v7GitNoteAtRef(repoRoot, ref, rel)
	}
	key := repoRoot + "\x00" + ref + "\x00" + rel
	cached, ok := m.refNotes[key]
	if !ok {
		cached.note, cached.ok, cached.err = v7GitNoteAtRef(repoRoot, ref, rel)
		m.refNotes[key] = cached
	}
	note := cached.note
	note.Data = cloneNoteData(cached.note.Data)
	return note, cached.ok, cached.err
}
