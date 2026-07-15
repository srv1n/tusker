package main

import (
	"path/filepath"
	"testing"
)

func TestCLIVaultMutationTrackingUsesCommonWriteBoundary(t *testing.T) {
	vault := pickupV7TestVault(t)
	beginCLIVaultMutationTracking()
	if err := writeText(filepath.Join(vault, "work", "tasks", "APP-T-0001.md"), "---\nid: APP-T-0001\n---\n"); err != nil {
		t.Fatal(err)
	}
	vaults := finishCLIVaultMutationTracking()
	if len(vaults) != 1 || !sameCanonicalProjectPath(vaults[0], vault) {
		t.Fatalf("tracked vaults = %#v, want %s", vaults, vault)
	}
}

func TestCLIVaultMutationTrackingIgnoresProductFiles(t *testing.T) {
	repo := t.TempDir()
	beginCLIVaultMutationTracking()
	if err := writeText(filepath.Join(repo, "product.txt"), "product\n"); err != nil {
		t.Fatal(err)
	}
	if vaults := finishCLIVaultMutationTracking(); len(vaults) != 0 {
		t.Fatalf("product write must not notify Tusker vaults: %#v", vaults)
	}
}
