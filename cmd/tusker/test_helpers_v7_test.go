package main

import (
	"path/filepath"
	"strings"
	"testing"
)

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
	vault := filepath.Join(t.TempDir(), "vault")
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
	if err := fn(args); err != nil {
		t.Fatal(err)
	}
}
