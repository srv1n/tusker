package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"

	"tusker/internal/docgraph"

	"gopkg.in/yaml.v3"
)

const waveAuthorizationSchema = "tusker.wave-authorization/v1"

// closeV7AuthorizationLock is a narrow seam for exercising the transaction's
// release failure handling. v7DocumentLock.Close itself always attempts both
// unlock and file close; this seam must therefore only be used while releasing
// authorization transaction locks.
var closeV7AuthorizationLock = func(lock *v7DocumentLock) error {
	return lock.Close()
}

func waveV7PauseCmd(args Args) error {
	return directWaveControlCmd(args, "wave pause", directWavePause)
}

func waveV7ResumeCmd(args Args) error {
	return directWaveControlCmd(args, "wave resume", directWaveResume)
}

type waveMaterialResult struct {
	fingerprint string
	issues      []string
}

type waveMaterialSpecResult struct {
	hash  string
	issue string
}

// Request-local only: mutation authorization always gets a new context.
// This shares corpus, spec bytes and identical material fingerprints across
// summary/recovery projections without retaining authority across requests.
type waveMaterialReadContext struct {
	resolver        *docgraph.Resolver
	corpusAttempted bool
	corpusLoads     int
	specs           map[string]waveMaterialSpecResult
	files           map[string]waveMaterialSpecResult
	fingerprints    map[string]waveMaterialResult
}

func newWaveMaterialReadContext() *waveMaterialReadContext {
	return &waveMaterialReadContext{specs: map[string]waveMaterialSpecResult{}, files: map[string]waveMaterialSpecResult{}, fingerprints: map[string]waveMaterialResult{}}
}

func sharedWaveMaterialReadContext(contexts []*waveMaterialReadContext) *waveMaterialReadContext {
	if len(contexts) > 0 && contexts[0] != nil {
		return contexts[0]
	}
	return newWaveMaterialReadContext()
}

func waveMaterialFingerprint(vaultPath string, idx v7Index, wave Note) (string, []string) {
	return waveMaterialFingerprintWithContext(vaultPath, idx, wave, newWaveMaterialReadContext())
}

// waveMaterialFingerprintObserver lets tests count wave material fingerprints.
var waveMaterialFingerprintObserver func()

func waveMaterialFingerprintWithContext(vaultPath string, idx v7Index, wave Note, context *waveMaterialReadContext) (string, []string) {
	if waveMaterialFingerprintObserver != nil {
		waveMaterialFingerprintObserver()
	}
	if context == nil {
		context = newWaveMaterialReadContext()
	}
	material := map[string]any{"schema": waveAuthorizationSchema, "wave": stringField(wave.Data, "id"), "outcome": firstNonEmpty(stringField(wave.Data, "outcome"), stringField(wave.Data, "summary")), "integration_base_sha": stringField(wave.Data, "integration_base_sha"), "members": []any{}, "specs": map[string]any{}}
	var issues []string
	members := uniqueStrings(normalizeList(wave.Data["members"]))
	sort.Strings(members)
	rows := make([]any, 0, len(members))
	for _, id := range members {
		task, ok := idx.Tasks[id]
		if !ok {
			issues = append(issues, "member task does not resolve: "+id)
			continue
		}
		row := map[string]any{
			"id": id, "title": stringField(task.Data, "title"), "epic": stringField(task.Data, "epic"), "risk": stringField(task.Data, "risk"),
			"dependencies": sortedStrings(normalizeList(task.Data["dependencies"])), "spec_refs": sortedStrings(normalizeList(task.Data["spec_refs"])),
			"contract_fingerprint": directWaveTaskContract(task), "artifact_contract": task.Data["artifact_contract"],
			"owned_paths": sortedStrings(normalizeList(task.Data["owned_paths"])), "generated_outputs": sortedStrings(normalizeList(task.Data["generated_outputs"])), "runner_profile": stringField(task.Data, "runner_profile"), "complexity": stringField(task.Data, "complexity"),
			"work_level": stringField(task.Data, "work_level"), "review_level": stringField(task.Data, "review_level"), "review_reason": stringField(task.Data, "review_reason"),
			"execute_profile": stringField(task.Data, "execute_profile"), "review_profile": stringField(task.Data, "review_profile"),
			"proof_contract":        map[string]any{"mode": stringField(task.Data, "proof_mode"), "required": sortedStrings(normalizeList(task.Data["proof_required"])), "required_owner": task.Data["proof_required_owner"], "evidence_budget": intField(task.Data, "evidence_budget"), "evidence_required": sortedStrings(normalizeList(task.Data["evidence_required"]))},
			"gates":                 waveMaterialGates(idx, id),
			"dependency_contracts":  task.Data["dependency_contracts"],
			"close_policy_snapshot": task.Data["close_policy_snapshot"],
		}
		// Authored tasks carry an immutable contract fingerprint. Their live
		// Verification table is also the proof ledger, so reviewers may append
		// rows without invalidating authorization.
		if stringField(task.Data, "contract_fingerprint") == "" {
			row["acceptance"] = waveMaterialTable(sectionContent(task.Body, "## Acceptance"), []int{0, 1})
			row["verification"] = waveMaterialTable(sectionContent(task.Body, "## Verification"), []int{0, 1})
		}
		rows = append(rows, row)
	}
	material["members"] = rows
	allSpecRefs := append([]string{}, normalizeList(wave.Data["spec_refs"])...)
	for _, id := range members {
		if task, ok := idx.Tasks[id]; ok {
			allSpecRefs = append(allSpecRefs, normalizeList(task.Data["spec_refs"])...)
		}
	}
	refs := sortedStrings(allSpecRefs)
	// The key only needs to separate distinct material; %#v (typed, sorted
	// map keys) is finer than the yaml fingerprint encoding and far cheaper.
	keyHash := sha256.New()
	fmt.Fprintf(keyHash, "%s\x00%#v\x00%#v\x00%#v", vaultPath, material, refs, issues)
	key := string(keyHash.Sum(nil))
	if cached, ok := context.fingerprints[key]; ok {
		return cached.fingerprint, cached.issues
	}
	if len(refs) > 0 && !context.corpusAttempted {
		context.corpusAttempted = true
		context.corpusLoads++
		// Shared across contexts only while the doc tree's stat signature
		// holds, so mutation contexts still resolve current documents.
		if resolver, err := docgraph.CachedResolver(v7RepoRoot(vaultPath)); err == nil {
			context.resolver = resolver
		}
	}
	for _, ref := range refs {
		cached, ok := context.specs[ref]
		if !ok {
			path, issue := waveMaterialSpecFile(vaultPath, ref, context.resolver)
			cached.issue = issue
			if issue == "" {
				file, read := context.files[path]
				if !read {
					raw, err := os.ReadFile(path)
					if err != nil {
						file.issue = "unreadable"
					} else {
						sum := sha256.Sum256(raw)
						file.hash = hex.EncodeToString(sum[:])
					}
					context.files[path] = file
				}
				cached.hash = file.hash
				if file.issue != "" {
					cached.issue = "spec_ref cannot be read: " + ref
				}
			}
			context.specs[ref] = cached
		}
		if cached.issue != "" {
			issues = append(issues, cached.issue)
		} else {
			// Only which specs a wave cites is material. Their text changes
			// through commits to main, and a commit to main never needs
			// re-approval; a task's promise (acceptance, scope, checks) does.
			material["specs"].(map[string]any)[ref] = true
		}
	}
	raw, _ := yaml.Marshal(material)
	sum := sha256.Sum256(raw)
	result := waveMaterialResult{"sha256:" + hex.EncodeToString(sum[:]), uniqueStrings(issues)}
	context.fingerprints[key] = result
	return result.fingerprint, result.issues
}

// waveMaterialSpecFile resolves one material spec_ref to the file whose bytes
// are hashed, using the same subject/path/section and legacy-forwarded
// document convention as authoring (v7SpecRefExists) and packet readers.
// Decision IDs keep their vault-relative route; every other ref resolves
// through the shared docgraph resolver so a bare subject such as
// portable-project-documentation hashes the same current source bytes the
// authoring check accepted. Ambiguous duplicate subjects fail instead of
// silently picking one route, and only spec/proposal/decision kinds are
// admitted. The returned path is empty when the ref does not resolve; the
// issue string then carries the MATERIAL_INVALID reason.
func waveMaterialSpecFile(vaultPath, ref string, resolver *docgraph.Resolver) (string, string) {
	unresolvable := "spec_ref does not resolve: " + ref
	if v7SpecRefPathEscapes(v7CleanSpecRef(ref)) {
		return "", unresolvable
	}
	if id := v7SpecRefDecisionID(v7NormalizeSpecRef(ref)); id != "" {
		path := filepath.Join(vaultPath, "work", "decisions", id+".md")
		if !fileExists(path) {
			return "", unresolvable
		}
		if anchor := v7SpecRefAnchor(ref); anchor != "" {
			raw, err := os.ReadFile(path)
			if err != nil || !v7SpecRefSectionExists(string(raw), anchor) {
				return "", unresolvable
			}
		}
		return path, ""
	}
	if resolver == nil {
		return "", unresolvable
	}
	base := v7CleanSpecRef(ref)
	anchor := v7SpecRefAnchor(ref)
	if base == "" {
		return "", unresolvable
	}
	if _, strictOK := resolver.ResolveStrict(base); !strictOK {
		if _, lenientOK := resolver.Resolve(base); lenientOK {
			return "", "spec_ref is ambiguous: " + ref
		}
		// Not a managed subject, path, or alias. Keep the historical
		// plain-file route so repo-relative files outside the managed
		// corpus (for example docs/specs/delivery.md) keep hashing their
		// on-disk bytes instead of newly failing authorization.
		return waveMaterialPlainFile(vaultPath, ref)
	}
	current, ok := resolver.ResolveCurrent(base)
	if !ok {
		return "", unresolvable
	}
	switch current.Document.Kind {
	case docgraph.KindSpec, docgraph.KindProposal, docgraph.KindDecision:
	default:
		return "", unresolvable
	}
	if anchor != "" && !v7SpecRefSectionExists(current.Document.Body, anchor) {
		return "", unresolvable
	}
	return filepath.Join(v7RepoRoot(vaultPath), filepath.FromSlash(current.Document.Path)), ""
}

// waveMaterialPlainFile resolves a spec_ref that names no managed document
// to a repo-relative file, preserving the pre-S46 material behavior for
// tracker-adjacent files outside the documentation corpus. Path escapes,
// absolute paths, decision IDs, and missing files stay unresolvable; a
// section anchor must exist in the file when one is declared.
func waveMaterialPlainFile(vaultPath, ref string) (string, string) {
	unresolvable := "spec_ref does not resolve: " + ref
	clean := v7CleanSpecRef(ref)
	if clean == "" || v7SpecRefPathEscapes(clean) || filepath.IsAbs(clean) {
		return "", unresolvable
	}
	if v7SpecRefDecisionID(clean) != "" {
		return "", unresolvable
	}
	var path string
	if strings.HasPrefix(clean, "work/") {
		path = filepath.Join(vaultPath, filepath.FromSlash(clean))
	} else {
		path = filepath.Join(v7RepoRoot(vaultPath), filepath.FromSlash(clean))
	}
	if !fileExists(path) {
		return "", unresolvable
	}
	if anchor := v7SpecRefAnchor(ref); anchor != "" {
		raw, err := os.ReadFile(path)
		if err != nil || !v7SpecRefSectionExists(string(raw), anchor) {
			return "", unresolvable
		}
	}
	return path, ""
}

func waveMaterialGates(idx v7Index, taskID string) []any {
	var out []any
	for _, gate := range sortedV7Gates(idx) {
		if !containsString(normalizeList(gate.Data["blocks"]), taskID) && !containsString(normalizeList(idx.Tasks[taskID].Data["gates"]), stringField(gate.Data, "id")) {
			continue
		}
		// Gate disposition is lifecycle state, not new execution scope. Keeping
		// it out lets a signed approval release already-authorized work without
		// silently authorizing changed gate text, limits, or affected tasks.
		row := map[string]any{"id": stringField(gate.Data, "id"), "gate_kind": stringField(gate.Data, "gate_kind"), "owner": stringField(gate.Data, "owner"), "blocking": boolField(gate.Data, "blocking"), "blocks": sortedStrings(normalizeList(gate.Data["blocks"])), "covers": sortedStrings(normalizeList(gate.Data["covers"])), "action": stringField(gate.Data, "action"), "verification": stringField(gate.Data, "verification"), "why_agent_cannot": v7GateBoundaryText(gate), "suggestion": v7GateSuggestionText(gate)}
		out = append(out, row)
	}
	return out
}

// waveMaterialLedgerRow reports whether a Verification/Acceptance table row is
// a lifecycle receipt rather than authored contract. Completion appends the
// typed-review receipt after independent review; hashing it would stale a wave
// after its first successful landing. The same predicate keeps the receipt out
// of the pinned contract canon in directWaveCanonicalContractBody.
func waveMaterialLedgerRow(cells []string) bool {
	return len(cells) > 3 && strings.HasPrefix(strings.TrimSpace(cells[1]), "typed review ") && strings.Contains(cells[3], "[tusker-review-result:")
}

func waveMaterialTable(section string, columns []int) []string {
	var out []string
	for _, line := range strings.Split(section, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "|") || strings.Contains(line, "---") {
			continue
		}
		cells := v7MarkdownTableCells(line)
		if len(cells) == 0 || strings.EqualFold(strings.TrimSpace(cells[0]), "ID") || strings.EqualFold(strings.TrimSpace(cells[0]), "Covers") {
			continue
		}
		if waveMaterialLedgerRow(cells) {
			continue
		}
		var selected []string
		for _, column := range columns {
			if column < len(cells) {
				selected = append(selected, strings.TrimSpace(cells[column]))
			}
		}
		out = append(out, strings.Join(selected, "|"))
	}
	return out
}

func closeV7DocumentLocks(locks []*v7DocumentLock) error {
	var errs []error
	for index := len(locks) - 1; index >= 0; index-- {
		if err := closeV7AuthorizationLock(locks[index]); err != nil {
			errs = append(errs, fmt.Errorf("member authorization lock %d: %w", index, err))
		}
	}
	return errors.Join(errs...)
}

func closeV7AuthorizationLocks(materialLock, waveLock *v7DocumentLock, taskLocks []*v7DocumentLock) error {
	var errs []error
	if err := closeV7DocumentLocks(taskLocks); err != nil {
		errs = append(errs, err)
	}
	if waveLock != nil {
		if err := closeV7AuthorizationLock(waveLock); err != nil {
			errs = append(errs, fmt.Errorf("wave authorization lock: %w", err))
		}
	}
	if materialLock != nil {
		if err := closeV7AuthorizationLock(materialLock); err != nil {
			errs = append(errs, fmt.Errorf("material authorization lock: %w", err))
		}
	}
	return errors.Join(errs...)
}

func waveAuthorizationProjection(vaultPath string, idx v7Index, wave Note) map[string]any {
	return waveAuthorizationProjectionWithContext(vaultPath, idx, wave, newWaveMaterialReadContext())
}

func waveAuthorizationProjectionWithContext(vaultPath string, idx v7Index, wave Note, context *waveMaterialReadContext) map[string]any {
	fingerprint, _ := waveMaterialFingerprintWithContext(vaultPath, idx, wave, context)
	return waveAuthorizationProjectionForFingerprint(wave, fingerprint)
}

func waveAuthorizationProjectionForFingerprint(wave Note, fingerprint string) map[string]any {
	stored, state := stringField(wave.Data, "authorization_fingerprint"), fallback(stringField(wave.Data, "authorization"), "disarmed")
	stale := waveAuthorizationFingerprintActive(state) && stored != "" && stored != fingerprint
	if stale {
		state = "stale"
	}
	return map[string]any{"state": state, "stale": stale, "fingerprint": fingerprint, "authorizedFingerprint": nullIfBlank(stored), "actor": nullIfBlank(stringField(wave.Data, "authorized_by")), "at": nullIfBlank(stringField(wave.Data, "authorized_at")), "action": waveAuthorizationProjectionAction(state)}
}

func waveAuthorizationFingerprintActive(state string) bool {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "armed", "paused":
		return true
	default:
		return false
	}
}

func waveAuthorizationProjectionAction(state string) string {
	switch state {
	case "armed":
		return "none"
	case "paused":
		return "tusker start <wave-id>"
	case "stale":
		return "tusker start <wave-id> --by <actor>"
	default:
		return "tusker start <wave-id> --by <actor>"
	}
}

func waveIntegrationBaseClean(vaultPath string, wave Note) bool {
	repoRoot := v7RepoRoot(vaultPath)
	branch := firstNonEmpty(stringField(wave.Data, "integration_branch"), v7IntegrationBranchName(stringField(wave.Data, "id")))
	if !v7GitRepo(repoRoot) {
		return false
	}
	defaultBranch := v7DefaultBranch(vaultPath)
	baseRev, baseErr := gitCombined(repoRoot, "rev-parse", "refs/heads/"+defaultBranch)
	if baseErr != nil {
		return false
	}
	baseRev = strings.TrimSpace(baseRev)
	frozenBase := strings.TrimSpace(stringField(wave.Data, "integration_base_sha"))
	branchRef := "refs/heads/" + branch
	if !gitRefExists(repoRoot, branchRef) {
		// A newly authored wave intentionally has no integration ref. It is
		// clean only while its frozen base still is the configured default.
		if frozenBase == "" || frozenBase != baseRev {
			return false
		}
	} else {
		integrationRev, integrationErr := gitCombined(repoRoot, "rev-parse", branchRef)
		if integrationErr != nil {
			return false
		}
		integrationRev = strings.TrimSpace(integrationRev)
		expected := firstNonEmpty(frozenBase, baseRev)
		if stringField(wave.Data, "authorized_at") == "" {
			if integrationRev != expected {
				return false
			}
		} else {
			if _, err := gitCombined(repoRoot, "merge-base", "--is-ancestor", expected, integrationRev); err != nil {
				return false
			}
		}
	}
	for _, worktree := range v7ListWorktrees(repoRoot) {
		if worktree.Branch != branchRef {
			continue
		}
		dirty, err := inPlaceDirtyPaths(worktree.Path)
		if err != nil || len(dirty) > 0 {
			return false
		}
	}
	return true
}

func sortedStrings(values []string) []string {
	values = uniqueStrings(values)
	sort.Strings(values)
	return values
}

func cloneMap(src map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range src {
		out[k] = v
	}
	return out
}

func cloneNoteMap(src map[string]Note) map[string]Note {
	out := map[string]Note{}
	for k, v := range src {
		out[k] = v
	}
	return out
}

// waveCheckEntry carries one vault's index and per-wave material fingerprints
// across wave checks (the daemon's frontier advance), so a check with nothing
// changed neither reloads the vault nor re-fingerprints the wave. The index is
// reused only while a stat-only signature of every note file and the
// in-process write generation hold; a fingerprint only while its wave
// document, the doc corpus and every cited spec file are unchanged. An edit
// to the wave, a member, a gate or a cited spec is therefore recomputed on
// the next check. Mutations (arming, pause, resume) never read this cache.
//
// ponytail: one entry per vault, never evicted; fine for a handful of
// projects per process.
type waveCheckEntry struct {
	signature [sha256.Size]byte
	idx       v7Index
	waves     map[string]waveCheckMaterial
}

type waveCheckMaterial struct {
	wave         Note
	result       waveMaterialResult
	usesResolver bool
	resolver     *docgraph.Resolver
	specs        map[string]noteFileVersion
}

var waveCheckCache = struct {
	sync.Mutex
	byVault map[string]*waveCheckEntry
}{byVault: map[string]*waveCheckEntry{}}

// waveCheckIndex returns the vault index for a wave check. The returned
// index's Tasks and Waves maps are copies, so callers may overlay notes.
func waveCheckIndex(vaultPath string) (v7Index, *waveCheckEntry, error) {
	signature, cacheable := waveCheckSignature(vaultPath)
	waveCheckCache.Lock()
	entry := waveCheckCache.byVault[vaultPath]
	waveCheckCache.Unlock()
	if !cacheable || entry == nil || entry.signature != signature {
		// The signature is taken before the load: a write racing the load
		// changes the next signature, so the next check reloads.
		idx, err := loadV7Index(vaultPath)
		if err != nil {
			return v7Index{}, nil, err
		}
		entry = &waveCheckEntry{signature: signature, idx: idx, waves: map[string]waveCheckMaterial{}}
		if cacheable {
			waveCheckCache.Lock()
			waveCheckCache.byVault[vaultPath] = entry
			waveCheckCache.Unlock()
		}
	}
	idx := entry.idx
	idx.Tasks = cloneNoteMap(entry.idx.Tasks)
	idx.Waves = cloneNoteMap(entry.idx.Waves)
	return idx, entry, nil
}

// waveCheckSignature is a stat-only signature of every note loadV7Index
// reads. It is not cacheable when a file's mtime is too coarse to order a
// same-size rewrite.
func waveCheckSignature(vaultPath string) ([sha256.Size]byte, bool) {
	var signature [sha256.Size]byte
	h := sha256.New()
	fmt.Fprintf(h, "%d\x00", v7DocumentWriteGeneration.Load())
	paths, err := walkNoteFiles(vaultPath)
	if err != nil {
		return signature, false
	}
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil || noteCacheNeedsCoarseMtimeHashCheck(info.ModTime()) {
			return signature, false
		}
		fmt.Fprintf(h, "%s\x00%+v\x00", path, noteFileVersionOf(info))
	}
	h.Sum(signature[:0])
	return signature, true
}

// fingerprint returns the wave's material fingerprint over idx, reusing the
// previous result while it is provably current.
func (entry *waveCheckEntry) fingerprint(vaultPath string, idx v7Index, wave Note) (string, []string) {
	id := stringField(wave.Data, "id")
	waveCheckCache.Lock()
	cached, ok := entry.waves[id]
	waveCheckCache.Unlock()
	if ok && cached.current(vaultPath, wave) {
		return cached.result.fingerprint, cached.result.issues
	}
	context := newWaveMaterialReadContext()
	fingerprint, issues := waveMaterialFingerprintWithContext(vaultPath, idx, wave, context)
	if len(issues) > 0 || (context.corpusAttempted && context.resolver == nil) {
		return fingerprint, issues
	}
	specs := make(map[string]noteFileVersion, len(context.files))
	for path, file := range context.files {
		// Stat, then confirm the bytes still hash as fingerprinted: an edit
		// racing the fingerprint is either seen here or moves the mtime.
		info, err := os.Stat(path)
		if err != nil || noteCacheNeedsCoarseMtimeHashCheck(info.ModTime()) {
			return fingerprint, issues
		}
		raw, err := os.ReadFile(path)
		if sum := sha256.Sum256(raw); err != nil || hex.EncodeToString(sum[:]) != file.hash {
			return fingerprint, issues
		}
		specs[path] = noteFileVersionOf(info)
	}
	waveCheckCache.Lock()
	wave.Data = cloneNoteData(wave.Data)
	entry.waves[id] = waveCheckMaterial{wave: wave, result: waveMaterialResult{fingerprint, issues}, usesResolver: context.corpusAttempted, resolver: context.resolver, specs: specs}
	waveCheckCache.Unlock()
	return fingerprint, issues
}

func (cached waveCheckMaterial) current(vaultPath string, wave Note) bool {
	if wave.Body != cached.wave.Body || !reflect.DeepEqual(wave.Data, cached.wave.Data) {
		return false
	}
	// CachedResolver returns the same resolver while the doc corpus's stat
	// signature holds, so spec resolution (route, ambiguity) is unchanged.
	if cached.usesResolver {
		if resolver, err := docgraph.CachedResolver(v7RepoRoot(vaultPath)); err != nil || resolver != cached.resolver {
			return false
		}
	}
	for path, version := range cached.specs {
		info, err := os.Stat(path)
		if err != nil || (noteFileVersionOf(info)) != version {
			return false
		}
	}
	return true
}
