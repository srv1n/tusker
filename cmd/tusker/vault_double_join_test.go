package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// vaultPath already IS the .tusker directory, so lock paths must not join
// ".tusker" a second time.
func TestProofWriteLockUsesSingleTuskerPath(t *testing.T) {
	vault := filepath.Join(t.TempDir(), ".tusker")
	release, err := acquireV7ProofWriteLock(vault, "APP-T-0001", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	if _, err := os.Stat(filepath.Join(vault, "locks", "proof-APP-T-0001.lock")); err != nil {
		t.Fatalf("expected lock at <vault>/locks: %v", err)
	}
	if _, err := os.Stat(filepath.Join(vault, ".tusker")); !os.IsNotExist(err) {
		t.Fatalf("double .tusker directory created under %s (err=%v)", vault, err)
	}
}

// An existing lock written by an older build under .tusker/.tusker must be
// relocated, not stranded: the caller has to still see the lock as held.
func TestProofWriteLockReclaimsLegacyDoublePath(t *testing.T) {
	vault := filepath.Join(t.TempDir(), ".tusker")
	legacy := filepath.Join(vault, ".tusker", "locks", "proof-APP-T-0002.lock")
	if err := ensureDir(filepath.Dir(legacy)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacy, []byte("pid=1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := acquireV7ProofWriteLock(vault, "APP-T-0002", 0); err == nil {
		t.Fatal("expected relocated legacy lock to read as held")
	} else if typed, ok := err.(*TuskerError); !ok || typed.Code != "PROOF_WRITE_BUSY" {
		t.Fatalf("expected PROOF_WRITE_BUSY, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(vault, "locks", "proof-APP-T-0002.lock")); err != nil {
		t.Fatalf("legacy lock not relocated: %v", err)
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Fatalf("legacy lock still present (err=%v)", err)
	}
}

func TestReclaimLegacyDoubleTuskerPathKeepsCanonicalFiles(t *testing.T) {
	vault := filepath.Join(t.TempDir(), ".tusker")
	legacy := filepath.Join(vault, ".tusker", "scratch", "APP-T-0003", "a.txt")
	canonical := filepath.Join(vault, "scratch", "APP-T-0003", "a.txt")
	stranded := filepath.Join(vault, ".tusker", "scratch", "APP-T-0003", "b.txt")
	for path, body := range map[string]string{legacy: "legacy", canonical: "canonical", stranded: "stranded"} {
		if err := ensureDir(filepath.Dir(path)); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	reclaimLegacyDoubleTuskerPath(vault, "scratch")

	body, err := os.ReadFile(canonical)
	if err != nil || string(body) != "canonical" {
		t.Fatalf("canonical file must win: body=%q err=%v", body, err)
	}
	if body, err := os.ReadFile(filepath.Join(vault, "scratch", "APP-T-0003", "b.txt")); err != nil || string(body) != "stranded" {
		t.Fatalf("stranded file not reclaimed: body=%q err=%v", body, err)
	}
}
