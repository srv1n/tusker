package main

import (
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

// runV7TestMutation preserves historical/executed verification receipts as
// fixtures without reopening the public verify-add authority boundary.
func runV7TestMutation(args Args, fn func(Args) error) error {
	name := runtime.FuncForPC(reflect.ValueOf(fn).Pointer()).Name()
	if !strings.HasSuffix(name, ".verifyV7AddCmd") {
		return fn(args)
	}
	rows, err := parseV7VerifyAddRows(args)
	if err != nil {
		return err
	}
	seed := false
	for _, row := range rows {
		if _, command := v7VerificationCommand(row.Check); command && !strings.EqualFold(strings.TrimSpace(row.Result), "pending") {
			seed = true
		}
	}
	if !seed {
		return fn(args)
	}
	vault := args.String("vault")
	taskID := firstNonEmpty(args.String("id"), args.String("_pos1"))
	if err := ensureV7TestReceiptScope(vault, taskID); err != nil {
		return err
	}
	if err := removeSupersededV7TestPendingRows(vault, taskID, rows); err != nil {
		return err
	}
	actor := fallback(args.String("by"), "reviewer:gate")
	if _, err = upsertV7Verifications(vault, taskID, rows, actor, args); err != nil {
		return err
	}
	note, err := resolveV7Note(vault, taskID, "task")
	if err != nil {
		return err
	}
	repo := v7RepoRoot(vault)
	if !v7GitRepo(repo) {
		if err := exec.Command("git", "-C", repo, "init", "-q").Run(); err != nil {
			return err
		}
	}
	identity, _, err := v7VerificationReceiptIdentityFor(vault, note, repo, nil)
	if err != nil {
		return err
	}
	for i := range rows {
		if _, command := v7VerificationCommand(rows[i].Check); command && strings.EqualFold(rows[i].Result, "pass") {
			rows[i].Notes = appendV7VerificationExecutionNote(rows[i], v7VerificationCommandObservation{ExitCode: 0, Digest: "sha256:test-fixture", MatchCount: 1}, identity)
		}
	}
	_, err = upsertV7Verifications(vault, taskID, rows, actor, args)
	return err
}

// Legacy unit fixtures sometimes name the control directory "vault" instead
// of ".tusker". Give their synthetic receipts one stable source file so later
// task-ledger writes cannot masquerade as implementation drift.
func ensureV7TestReceiptScope(vault, taskID string) error {
	if filepath.Base(filepath.Clean(vault)) == ".tusker" {
		return nil
	}
	note, err := resolveV7Note(vault, taskID, "task")
	if err != nil {
		return err
	}
	scope, err := canonicalTaskMaterialScope(vault, note)
	if err != nil || len(scope) > 0 {
		return err
	}
	rel := filepath.ToSlash(filepath.Join(".tusker-test-fixtures", strings.ToLower(taskID)+".txt"))
	path := filepath.Join(v7RepoRoot(vault), filepath.FromSlash(rel))
	if err := ensureDir(filepath.Dir(path)); err != nil {
		return err
	}
	if !fileExists(path) {
		if err := writeText(path, "stable verification fixture\n"); err != nil {
			return err
		}
	}
	data, body, err := parseFrontmatterMustRead(note.AbsolutePath)
	if err != nil {
		return err
	}
	data["owned_paths"] = []string{rel}
	data["contract_fingerprint"] = directWaveTaskContractFingerprint(data, body)
	_, err = saveV7DocumentCAS(note.AbsolutePath, data, body, v7FrontmatterOrder["task"], stringField(data, "state_rev"))
	return err
}

func v7TestVerificationMutation(args Args) error {
	return runV7TestMutation(args, verifyV7AddCmd)
}

func removeSupersededV7TestPendingRows(vault, taskID string, receipts []v7VerificationRow) error {
	note, err := resolveV7Note(vault, taskID, "task")
	if err != nil {
		return err
	}
	data, body, err := parseFrontmatterMustRead(note.AbsolutePath)
	if err != nil {
		return err
	}
	rows := parseV7VerificationRows(body)
	kept := rows[:0]
	for _, existing := range rows {
		superseded := false
		if strings.EqualFold(strings.TrimSpace(existing.Result), "pending") {
			if _, command := v7VerificationCommand(existing.Check); command {
				for _, receipt := range receipts {
					if strings.EqualFold(strings.TrimSpace(existing.CoverText), strings.TrimSpace(receipt.CoverText)) {
						superseded = true
						break
					}
				}
			}
		}
		if !superseded {
			kept = append(kept, existing)
		}
	}
	if len(kept) == len(rows) {
		return nil
	}
	body = replaceSection(body, "## Verification", renderV7VerificationTable(kept))
	_, err = saveV7DocumentCAS(note.AbsolutePath, data, body, v7FrontmatterOrder["task"], stringField(data, "state_rev"))
	return err
}

func mustRunIndexTest(t *testing.T, args Args, fn func(Args) error) {
	t.Helper()
	if err := fn(args); err != nil {
		t.Fatal(err)
	}
}

func mustReadIndexTest(t *testing.T, path string) string {
	t.Helper()
	content, err := readText(path)
	if err != nil {
		t.Fatal(err)
	}
	return content
}

func assertContainsIndexTest(t *testing.T, haystack, needle string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		t.Fatalf("expected output to contain %q:\n%s", needle, haystack)
	}
}

func assertNotContainsIndexTest(t *testing.T, haystack, needle string) {
	t.Helper()
	if strings.Contains(haystack, needle) {
		t.Fatalf("expected output not to contain %q:\n%s", needle, haystack)
	}
}

func assertMaxLineWidthIndexTest(t *testing.T, output string, maxWidth int) {
	t.Helper()
	for _, line := range strings.Split(strings.TrimRight(output, "\n"), "\n") {
		if displayCellWidth(line) > maxWidth {
			t.Fatalf("line width %d exceeds %d:\n%s", displayCellWidth(line), maxWidth, output)
		}
	}
}

func pickupV7TestVault(t *testing.T) string {
	t.Helper()
	vault := filepath.Join(t.TempDir(), defaultRepoVaultDir)
	if err := bootstrap(Args{"vault": vault, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	if err := newV7Epic(Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App", "summary": "App work.", "v7": "true"}); err != nil {
		t.Fatal(err)
	}
	for _, domain := range []string{"backend", "frontend"} {
		dir := filepath.Join(vault, "knowledge", "domains", domain)
		if err := ensureDir(dir); err != nil {
			t.Fatal(err)
		}
		if err := writeText(filepath.Join(dir, "INDEX.md"), "# "+domain+"\n"); err != nil {
			t.Fatal(err)
		}
		if err := writeText(filepath.Join(dir, "CANON.md"), "# "+domain+" canon\n"); err != nil {
			t.Fatal(err)
		}
	}
	return vault
}

func mustRunPickupTest(t *testing.T, args Args, fn func(Args) error) {
	t.Helper()
	if err := runV7TestMutation(args, fn); err != nil {
		t.Fatal(err)
	}
}
