package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// safetyVaultTest builds the minimum layout that authorizes deletion: a V7 work
// tree plus one vault marker. The vault deliberately sits inside a parent temp
// dir so containment tests can plant a sentinel outside it.
func safetyVaultTest(t *testing.T) string {
	t.Helper()
	vault := filepath.Join(t.TempDir(), "vault")
	if err := ensureDir(filepath.Join(vault, "work", "tasks")); err != nil {
		t.Fatal(err)
	}
	if err := writeText(filepath.Join(vault, "WORKFLOW.md"), "# workflow\n"); err != nil {
		t.Fatal(err)
	}
	return vault
}

func seedScratchTest(t *testing.T, vault, taskID string) string {
	t.Helper()
	dir := filepath.Join(vault, "scratch", taskID)
	if err := ensureDir(dir); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"PLAN.md", "render.wav"} {
		if err := writeText(filepath.Join(dir, name), "scratch\n"); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestScratchCoordinationSweepBlocksWriter(t *testing.T) {
	vault := safetyVaultTest(t)
	entered := make(chan struct{})
	release := make(chan struct{})
	firstDone := make(chan error, 1)
	go func() {
		firstDone <- withScratchRetentionLock(vault, func() error {
			close(entered)
			<-release
			return nil
		})
	}()
	<-entered
	secondStarted := make(chan struct{})
	secondDone := make(chan error, 1)
	go func() {
		close(secondStarted)
		secondDone <- withScratchRetentionLock(vault, func() error { return nil })
	}()
	<-secondStarted
	select {
	case err := <-secondDone:
		t.Fatalf("writer entered while sweep held lock: %v", err)
	default:
	}
	close(release)
	if err := <-firstDone; err != nil {
		t.Fatal(err)
	}
	if err := <-secondDone; err != nil {
		t.Fatal(err)
	}
}

// agedScratchTest seeds one scratch entry and backdates the file and its
// directory so the entry's newest mtime is `age` old.
func agedScratchTest(t *testing.T, vault, name string, age time.Duration) string {
	t.Helper()
	dir := filepath.Join(vault, "scratch", name)
	if err := ensureDir(dir); err != nil {
		t.Fatal(err)
	}
	blob := filepath.Join(dir, "blob.bin")
	if err := writeText(blob, "scratch exhaust\n"); err != nil {
		t.Fatal(err)
	}
	stamp := time.Now().Add(-age)
	for _, path := range []string{blob, dir} {
		if err := os.Chtimes(path, stamp, stamp); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func mustReadTest(t *testing.T, path string) string {
	t.Helper()
	text, err := readText(path)
	if err != nil {
		t.Fatal(err)
	}
	return text
}

func externalDirTest(t *testing.T, name string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), name)
	if err := ensureDir(dir); err != nil {
		t.Fatal(err)
	}
	if err := writeText(filepath.Join(dir, "keepme.txt"), "outside the vault\n"); err != nil {
		t.Fatal(err)
	}
	return dir
}

func mustSymlinkTest(t *testing.T, target, link string) {
	t.Helper()
	if err := ensureDir(filepath.Dir(link)); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable on this platform: %v", err)
	}
}

// TestScratchSafetyReapRefusesTraversalIDs is the blocker regression: no task ID
// that escapes scratch/<ID> may reach a recursive delete.
func TestScratchSafetyReapRefusesTraversalIDs(t *testing.T) {
	vault := safetyVaultTest(t)
	parent := filepath.Dir(vault)
	scratch := seedScratchTest(t, vault, "APP-T-0001")

	insideSentinel := filepath.Join(vault, "work", "sentinel.md")
	outsideSentinel := filepath.Join(parent, "sentinel.md")
	const sentinelBody = "do not delete me\n"
	for _, path := range []string{insideSentinel, outsideSentinel} {
		if err := writeText(path, sentinelBody); err != nil {
			t.Fatal(err)
		}
	}

	for _, id := range []string{"..", ".", "../work", "a/../../..", "APP-T-0001/../..", "", "APP-T-0001/render.wav"} {
		err := reapTaskScratch(vault, id)
		if !errors.Is(err, errNotScratchChild) {
			t.Fatalf("reapTaskScratch(%q) must refuse with errNotScratchChild, got %v", id, err)
		}
	}

	for _, path := range []string{insideSentinel, outsideSentinel} {
		if got := mustReadTest(t, path); got != sentinelBody {
			t.Fatalf("sentinel %s changed: %q", path, got)
		}
	}
	if !dirExists(filepath.Join(vault, "work", "tasks")) {
		t.Fatal("refused reap removed the vault work tree")
	}
	if !dirExists(parent) || !dirExists(vault) {
		t.Fatal("refused reap removed a directory it must never touch")
	}
	if !fileExists(filepath.Join(scratch, "render.wav")) {
		t.Fatalf("refused reap removed the task's own scratch: %s", scratch)
	}

	// The same ID without separators is the one that is allowed to delete.
	if err := reapTaskScratch(vault, "APP-T-0001"); err != nil {
		t.Fatal(err)
	}
	if dirExists(scratch) {
		t.Fatalf("canonical reap left scratch behind: %s", scratch)
	}
}

func TestScratchSafetyNonVaultRefusesDeletion(t *testing.T) {
	root := t.TempDir()
	if err := ensureDir(filepath.Join(root, "work", "tasks")); err != nil {
		t.Fatal(err)
	}
	victim := seedScratchTest(t, root, "APP-T-0001")

	entries, err := scanScratchEntries(root)
	if !errors.Is(err, errNotTuskerVault) {
		t.Fatalf("scan of a non-vault must fail closed, got entries=%v err=%v", entries, err)
	}
	if _, err := resolveScratchRoot(root); !errors.Is(err, errNotTuskerVault) {
		t.Fatalf("resolveScratchRoot must refuse a non-vault, got %v", err)
	}

	plan := []scratchEntry{{Name: "APP-T-0001", Path: victim}}
	outcome, err := applyScratchGC(root, plan, time.Now())
	if !errors.Is(err, errNotTuskerVault) {
		t.Fatalf("applyScratchGC must refuse a non-vault, got %v", err)
	}
	if len(outcome.Deleted) != 0 || outcome.Reclaimed != 0 {
		t.Fatalf("refused apply reported work: %+v", outcome)
	}
	if err := reapTaskScratch(root, "APP-T-0001"); err != nil {
		t.Fatalf("reap of a non-vault must be an inert no-op, got %v", err)
	}
	if !fileExists(filepath.Join(victim, "render.wav")) {
		t.Fatalf("non-vault scratch was deleted: %s", victim)
	}
}

func TestScratchSafetySymlinkedScratchRootRefused(t *testing.T) {
	vault := safetyVaultTest(t)
	external := externalDirTest(t, "elsewhere")
	if err := ensureDir(filepath.Join(external, "APP-T-0001")); err != nil {
		t.Fatal(err)
	}
	keep := filepath.Join(external, "APP-T-0001", "keepme.txt")
	if err := writeText(keep, "outside the vault\n"); err != nil {
		t.Fatal(err)
	}
	mustSymlinkTest(t, external, filepath.Join(vault, "scratch"))

	if _, err := resolveScratchRoot(vault); !errors.Is(err, errScratchRootUnsafe) {
		t.Fatalf("a symlinked scratch root must be refused, got %v", err)
	}
	if err := reapTaskScratch(vault, "APP-T-0001"); !errors.Is(err, errScratchRootUnsafe) {
		t.Fatalf("reap through a symlinked scratch root must be refused, got %v", err)
	}
	if !fileExists(keep) {
		t.Fatalf("reap followed the scratch symlink and deleted %s", keep)
	}
}

func TestScratchSafetyChildSymlinkNotFollowed(t *testing.T) {
	vault := safetyVaultTest(t)
	external := externalDirTest(t, "linked-target")
	keep := filepath.Join(external, "keepme.txt")

	link := filepath.Join(vault, "scratch", "APP-T-0001")
	mustSymlinkTest(t, external, link)
	gcLink := filepath.Join(vault, "scratch", "orig-piano")
	mustSymlinkTest(t, external, gcLink)

	if err := reapTaskScratch(vault, "APP-T-0001"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(link); !os.IsNotExist(err) {
		t.Fatalf("reap must remove the scratch symlink itself, got err=%v", err)
	}

	entries, err := scanScratchEntries(vault)
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := applyScratchGC(vault, entries, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(outcome.Deleted) != 1 || outcome.Deleted[0].Name != "orig-piano" {
		t.Fatalf("expected the symlink entry to be collected, got %+v", outcome)
	}
	if _, err := os.Lstat(gcLink); !os.IsNotExist(err) {
		t.Fatalf("GC must remove the scratch symlink itself, got err=%v", err)
	}
	if !fileExists(keep) || !dirExists(external) {
		t.Fatalf("deletion followed a child symlink and destroyed %s", external)
	}
}

func TestScratchSafetyLinkOnlyPathMatching(t *testing.T) {
	matches := []string{
		"scratch/APP-T-0001/render.wav",
		"link-only:.tusker/scratch/./APP-T-0001/render.wav",
		".tusker/scratch/tmp/../APP-T-0001/render.wav",
		`C:\repo\.tusker\scratch\APP-T-0001\render.wav`,
		"SCRATCH/app-t-0001/render.wav",
	}
	for _, recorded := range matches {
		if !scratchPathRefersToTask(recorded, "APP-T-0001") {
			t.Fatalf("expected %q to refer to APP-T-0001", recorded)
		}
	}
	misses := []string{
		"notscratch/APP-T-0001/file",
		"scratch/APP-T-0002/file",
		"scratch/file",
		"",
	}
	for _, recorded := range misses {
		if scratchPathRefersToTask(recorded, "APP-T-0001") {
			t.Fatalf("expected %q not to refer to APP-T-0001", recorded)
		}
	}
}

func writeEvidenceCardTest(t *testing.T, vault, taskID, name, body string) string {
	t.Helper()
	path := filepath.Join(vault, "evidence", taskID, name)
	if err := ensureDir(filepath.Dir(path)); err != nil {
		t.Fatal(err)
	}
	if err := writeText(path, body); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestScratchReapSparesLinkOnlyEvidence(t *testing.T) {
	vault := safetyVaultTest(t)
	dir := seedScratchTest(t, vault, "APP-T-0001")
	card := writeEvidenceCardTest(t, vault, "APP-T-0001", "APP-T-0001-E-0001.md", strings.Join([]string{
		"---",
		`schema: "tusker.evidence/v1"`,
		`kind: "evidence"`,
		`id: "APP-T-0001-E-0001"`,
		`task: "APP-T-0001"`,
		`evidence_kind: "log_excerpt"`,
		`artifact_durability: "link_only"`,
		"artifact_paths:",
		`  - "link-only:.tusker/scratch/APP-T-0001/render.wav"`,
		"---",
		"",
		"# APP-T-0001-E-0001 - evidence",
		"",
	}, "\n"))

	if err := reapTaskScratch(vault, "APP-T-0001"); err != nil {
		t.Fatal(err)
	}
	if !fileExists(filepath.Join(dir, "render.wav")) {
		t.Fatalf("link-only evidence did not spare scratch: %s", dir)
	}

	// A durability other than link_only means the artifact was copied out, so the
	// same scratch is reapable.
	copied := strings.Replace(mustReadTest(t, card), `artifact_durability: "link_only"`, `artifact_durability: "copied"`, 1)
	if err := writeText(card, copied); err != nil {
		t.Fatal(err)
	}
	if err := reapTaskScratch(vault, "APP-T-0001"); err != nil {
		t.Fatal(err)
	}
	if dirExists(dir) {
		t.Fatalf("reap left scratch behind without link-only evidence: %s", dir)
	}
}

func TestScratchReapFailsOpenOnUnreadableEvidence(t *testing.T) {
	vault := safetyVaultTest(t)
	dir := seedScratchTest(t, vault, "APP-T-0001")
	writeEvidenceCardTest(t, vault, "APP-T-0001", "APP-T-0001-E-0001.md", "---\nartifact_paths: [unterminated\n---\n\nbroken card\n")

	if err := reapTaskScratch(vault, "APP-T-0001"); err != nil {
		t.Fatal(err)
	}
	if !fileExists(filepath.Join(dir, "render.wav")) {
		t.Fatalf("an unparseable evidence card must keep scratch, deleted: %s", dir)
	}
}

func TestScratchReapGCRevalidatesRefreshedEntries(t *testing.T) {
	vault := safetyVaultTest(t)
	stale := agedScratchTest(t, vault, "old-render", 30*24*time.Hour)
	refreshed := agedScratchTest(t, vault, "APP-T-0001", 30*24*time.Hour)

	now := time.Now()
	ttl := time.Duration(defaultScratchTTLDays) * 24 * time.Hour
	plan, err := planScratchGC(vault, ttl, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan) != 2 {
		t.Fatalf("expected both entries in the plan, got %+v", plan)
	}

	// The entry becomes active between planning and applying.
	blob := filepath.Join(refreshed, "blob.bin")
	if err := writeText(blob, "fresh work in progress\n"); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{blob, refreshed} {
		if err := os.Chtimes(path, now, now); err != nil {
			t.Fatal(err)
		}
	}

	outcome, err := applyScratchGC(vault, plan, now.Add(-ttl))
	if err != nil {
		t.Fatal(err)
	}
	if len(outcome.Skipped) != 1 || outcome.Skipped[0].Name != "APP-T-0001" {
		t.Fatalf("expected the refreshed entry to be skipped, got %+v", outcome)
	}
	if len(outcome.Deleted) != 1 || outcome.Deleted[0].Name != "old-render" {
		t.Fatalf("expected only the stale entry to be deleted, got %+v", outcome)
	}
	if !fileExists(blob) {
		t.Fatalf("apply deleted an entry that was refreshed after planning: %s", refreshed)
	}
	if dirExists(stale) {
		t.Fatalf("apply left the stale entry behind: %s", stale)
	}
}

func TestScratchSafetyPartialFailureReportsProgress(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	vault := safetyVaultTest(t)
	first := agedScratchTest(t, vault, "aaa-render", 30*24*time.Hour)

	second := filepath.Join(vault, "scratch", "bbb-render")
	locked := filepath.Join(second, "locked")
	pinned := filepath.Join(locked, "pinned.bin")
	if err := ensureDir(locked); err != nil {
		t.Fatal(err)
	}
	if err := writeText(pinned, "cannot unlink\n"); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(locked, 0o500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(locked, 0o700)
	// Backdate after the tree exists: creating a child refreshes its parent.
	stamp := time.Now().Add(-30 * 24 * time.Hour)
	for _, path := range []string{pinned, locked, second} {
		if err := os.Chtimes(path, stamp, stamp); err != nil {
			t.Fatal(err)
		}
	}

	now := time.Now()
	ttl := time.Duration(defaultScratchTTLDays) * 24 * time.Hour
	plan, err := planScratchGC(vault, ttl, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan) != 2 || plan[0].Name != "aaa-render" {
		t.Fatalf("expected aaa-render planned first, got %+v", plan)
	}

	outcome, err := applyScratchGC(vault, plan, now.Add(-ttl))
	if err == nil {
		t.Fatal("expected the locked entry to fail removal")
	}
	if len(outcome.Deleted) != 1 || outcome.Deleted[0].Name != "aaa-render" {
		t.Fatalf("apply must report completed deletions before the failure, got %+v", outcome)
	}
	if outcome.Reclaimed == 0 {
		t.Fatalf("apply must report bytes reclaimed before the failure, got %+v", outcome)
	}
	if outcome.Failed != filepath.Join(vault, "scratch", "bbb-render") {
		t.Fatalf("apply must name the failing entry, got %q", outcome.Failed)
	}
	if dirExists(first) {
		t.Fatalf("expected %s to be deleted", first)
	}
	if !fileExists(filepath.Join(locked, "pinned.bin")) {
		t.Fatalf("the undeletable file vanished: %s", locked)
	}
}

func inlineProofTaskTest(t *testing.T) string {
	t.Helper()
	vault := filepath.Join(t.TempDir(), "vault")
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true"}, bootstrap)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App", "summary": "Scratch retention.", "v7": "true"}, newV7Epic)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Reap scratch", "risk": "low", "priority": "p2", "proof-mode": "inline", "v7": "true"}, newV7Task)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "runner": "codex"}, attemptV7StartCmd)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "_pos1": "APP-T-0001", "covers": "A1", "check": "go test ./cmd/tusker -run ScratchReap -count=1", "result": "pass", "note": "Focused proof passed."}, verifyV7AddCmd)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "attempt": "APP-T-0001-A-0001", "summary": "Implementation complete.", "local": "true"}, finishV7Cmd)
	return vault
}

func TestScratchReapOnManualClose(t *testing.T) {
	vault := inlineProofTaskTest(t)
	dir := seedScratchTest(t, vault, "APP-T-0001")

	if err := closeV7Cmd(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "by": "reviewer:agent", "reason": "accepted", "local": "true"}); err != nil {
		t.Fatal(err)
	}
	if dirExists(dir) {
		t.Fatalf("manual close left scratch behind: %s", dir)
	}
}

func TestScratchReapOnDiscard(t *testing.T) {
	vault := pickupV7TestVault(t)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Discard me", "risk": "low", "priority": "p1", "v7": "true"}, newV7Task)
	dir := seedScratchTest(t, vault, "APP-T-0001")

	if err := discardV7Cmd(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "reason": "No longer desired."}); err != nil {
		t.Fatal(err)
	}
	if dirExists(dir) {
		t.Fatalf("discard left scratch behind: %s", dir)
	}
}

// The daemon's automated close only lands through the canonical projection, and
// that path needs a full git/review-transaction fixture. Assert the wiring at the
// source instead of standing up one; reapTaskScratch itself is covered above.
func TestScratchReapOnReactorClose(t *testing.T) {
	source, err := readText("completion_reactor.go")
	if err != nil {
		t.Fatal(err)
	}
	body := source[strings.Index(source, "func projectCompletionTaskToCanonical("):]
	body = body[:strings.Index(body, "\n}\n")]
	if strings.Count(body, "reapTaskScratch(vaultPath, result.TaskID)") != 2 {
		t.Fatal("canonical completion projection must reap task scratch on both return paths")
	}
}
