package main

import (
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
	if err := removeSupersededV7TestPendingRows(vault, taskID, rows); err != nil {
		return err
	}
	_, err = upsertV7Verifications(vault, taskID, rows, fallback(args.String("by"), "reviewer:gate"), args)
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
