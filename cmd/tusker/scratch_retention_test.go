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
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "_pos1": "APP-T-0001", "covers": "A1", "check": "go test ./cmd/tusker -run ScratchReap -count=1", "result": "pass", "note": "Focused proof passed."}, v7TestVerificationMutation)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "attempt": "APP-T-0001-A-0001", "summary": "Implementation complete.", "local": "true"}, finishV7Cmd)
	return vault
}

func TestScratchRetainedOnManualClose(t *testing.T) {
	vault := inlineProofTaskTest(t)
	dir := seedScratchTest(t, vault, "APP-T-0001")

	if err := closeV7Cmd(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "by": "reviewer:agent", "reason": "accepted", "local": "true"}); err != nil {
		t.Fatal(err)
	}
	if !dirExists(dir) {
		t.Fatalf("manual close deleted scratch before retention: %s", dir)
	}
}

func TestScratchRetainedOnTierOneDirectDone(t *testing.T) {
	vault := v7DispatchTestVault(t)
	if _, err := setProjectLocalConfigWithReadback(vault, "tier", 1); err != nil {
		t.Fatal(err)
	}
	if err := newV7Task(Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Direct close", "risk": "low", "priority": "p1", "v7": "true"}); err != nil {
		t.Fatal(err)
	}
	dir := seedScratchTest(t, vault, "APP-T-0001")

	if err := statusV7Cmd(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "status": "done"}); err != nil {
		t.Fatal(err)
	}
	if !dirExists(dir) {
		t.Fatalf("tier-one direct done deleted scratch before retention: %s", dir)
	}
}

func TestScratchRetainedOnDiscard(t *testing.T) {
	vault := pickupV7TestVault(t)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Discard me", "risk": "low", "priority": "p1", "v7": "true"}, newV7Task)
	dir := seedScratchTest(t, vault, "APP-T-0001")

	if err := discardV7Cmd(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "reason": "No longer desired."}); err != nil {
		t.Fatal(err)
	}
	if !dirExists(dir) {
		t.Fatalf("discard deleted scratch before retention: %s", dir)
	}
}

// The daemon's automated close only lands through the canonical projection, and
// that path needs a full git/review-transaction fixture. Assert the wiring at the
// source instead of standing up one; reapTaskScratch itself is covered above.
func TestScratchNotReapedOnReactorClose(t *testing.T) {
	source, err := readText("completion_reactor.go")
	if err != nil {
		t.Fatal(err)
	}
	body := source[strings.Index(source, "func projectCompletionTaskToCanonical("):]
	body = body[:strings.Index(body, "\n}\n")]
	if strings.Contains(body, "reapTaskScratch(vaultPath, result.TaskID)") {
		t.Fatal("canonical completion must leave scratch to the retention policy")
	}
}

func TestRemainingArtifactRetention(t *testing.T) {
	vault := inlineProofTaskTest(t)
	dir := seedScratchTest(t, vault, "APP-T-0001")
	if err := closeV7Cmd(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "by": "reviewer:agent", "reason": "accepted", "local": "true"}); err != nil {
		t.Fatal(err)
	}
	taskPath := filepath.Join(vault, "work", "tasks", "APP-T-0001.md")
	data, body, err := parseFrontmatterMustRead(taskPath)
	if err != nil {
		t.Fatal(err)
	}
	closed := time.Now().UTC().Add(-7 * 24 * time.Hour)
	data["closed_at"], data["accepted_at"] = closed.Format(time.RFC3339), closed.Format(time.RFC3339)
	data["state_rev"] = v7StateRev(data, body)
	content, err := serializeDocument(data, body, v7FrontmatterOrder["task"])
	if err != nil {
		t.Fatal(err)
	}
	if err := writeText(taskPath, content); err != nil {
		t.Fatal(err)
	}
	if plan, err := planScratchGC(vault, 7*24*time.Hour, closed.Add(7*24*time.Hour-time.Second)); err != nil || len(plan) != 0 {
		t.Fatalf("before boundary plan=%v err=%v", plan, err)
	}
	if err := scratchKeepCmd(vault, Args{"keep": "APP-T-0001", "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	if plan, err := planScratchGC(vault, 7*24*time.Hour, closed.Add(7*24*time.Hour)); err != nil || len(plan) != 0 {
		t.Fatalf("kept plan=%v err=%v", plan, err)
	}
	if err := scratchKeepCmd(vault, Args{"unkeep": "APP-T-0001", "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	plan, err := planScratchGC(vault, 7*24*time.Hour, closed.Add(7*24*time.Hour))
	if err != nil || len(plan) != 1 {
		t.Fatalf("expiry plan=%v err=%v", plan, err)
	}
	if _, err := applyScratchGC(vault, plan, closed); err != nil {
		t.Fatal(err)
	}
	if dirExists(dir) {
		t.Fatal("eligible bytes survived expiry")
	}
	expired, _, err := parseFrontmatterMustRead(taskPath)
	if err != nil {
		t.Fatal(err)
	}
	if stringField(expired, "artifacts_availability") != "expired" || stringField(expired, "artifacts_expired_at") == "" {
		t.Fatalf("expiry receipt=%#v", expired)
	}
}

// The daemon's reviewed completion path is a separate close route from the
// interactive ceremony. Keep the shared documentation-touch check wired into
// that terminal projection so automation cannot bypass the policy.
func TestDocTouchCheckOnReactorClose(t *testing.T) {
	source, err := readText("completion_reactor.go")
	if err != nil {
		t.Fatal(err)
	}
	body := source[strings.Index(source, "func projectCompletionTaskToCanonical("):]
	body = body[:strings.Index(body, "\n}\n")]
	if strings.Count(body, "v7DocTouchCheck(vaultPath, staged)") != 1 {
		t.Fatal("canonical completion projection must check documentation drift before terminal replacement")
	}
}

func TestScratchSafetyChildSymlinkNotFollowed(t *testing.T) {
	vault := safetyVaultTest(t)
	external := externalDirTest(t, "linked-target")
	keep := filepath.Join(external, "keepme.txt")

	gcLink := filepath.Join(vault, "scratch", "orig-piano")
	mustSymlinkTest(t, external, gcLink)

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
	if !fileExists(keep) {
		t.Fatalf("scratch root resolution followed the symlink and deleted %s", keep)
	}
}
