package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMaterializedSkillProvenanceClassifiesFreshnessAndLocalEdits(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "tusker")
	if err := installSkillPayloadCopy(destination); err != nil {
		t.Fatal(err)
	}
	if got := inspectSkillMaterialization(destination); got.Status != "current" || got.Manifest == nil {
		t.Fatalf("current copy provenance = %#v", got)
	}
	if err := os.Remove(filepath.Join(destination, skillProvenanceFilename)); err != nil {
		t.Fatal(err)
	}
	if got := inspectSkillMaterialization(destination); got.Status != "missing_provenance" {
		t.Fatalf("missing manifest status = %#v", got)
	}
	if err := writeSkillMaterializationProvenance(destination, "embedded", portableSkillSourceIdentity("embedded")); err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(destination, skillProvenanceFilename)
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeText(manifestPath, strings.Replace(string(raw), "factory_intake_contract_version: 1.1.0", "factory_intake_contract_version: 0.0.0", 1)); err != nil {
		t.Fatal(err)
	}
	if got := inspectSkillMaterialization(destination); got.Status != "incompatible" {
		t.Fatalf("manifest/package contradiction status = %#v", got)
	}
	if err := writeText(manifestPath, strings.Replace(string(raw), "schema: tusker.skill-materialization/v1", "schema: tusker.skill-materialization/v0", 1)); err != nil {
		t.Fatal(err)
	}
	if got := inspectSkillMaterialization(destination); got.Status != "incompatible" {
		t.Fatalf("incompatible copy status = %#v", got)
	}
	if err := writeText(manifestPath, string(raw)); err != nil {
		t.Fatal(err)
	}
	if err := writeText(filepath.Join(destination, "SKILL.md"), "local edit\n"); err != nil {
		t.Fatal(err)
	}
	if got := inspectSkillMaterialization(destination); got.Status != "locally_modified" {
		t.Fatalf("edited copy status = %#v", got)
	}
}

func TestSymlinkProvenanceReadsLiveTarget(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "first", "tusker")
	second := filepath.Join(root, "second", "tusker")
	if err := installSkillPayloadCopy(first); err != nil {
		t.Fatal(err)
	}
	if err := installSkillPayloadCopy(second); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link", "tusker")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(first, link); err != nil {
		t.Fatal(err)
	}
	if got := inspectSkillMaterialization(link); got.Status != "current" || got.SourceKind != skillInstallModeLink {
		t.Fatalf("live first symlink = %#v", got)
	}
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(second, link); err != nil {
		t.Fatal(err)
	}
	if got := inspectSkillMaterialization(link); got.Status != "current" {
		t.Fatalf("retargeted symlink used stale cache: %#v", got)
	}
	assetPath := filepath.Join(second, "assets", "factory-intake-contract.yaml")
	asset, err := os.ReadFile(assetPath)
	if err != nil {
		t.Fatal(err)
	}
	currentContract, err := embeddedFactoryIntakeContractProvenance()
	if err != nil {
		t.Fatal(err)
	}
	staleAsset := strings.Replace(string(asset), "contract_version: "+currentContract.Version, "contract_version: 1.0.0", 1)
	staleContract, err := factoryIntakeContractProvenanceFromRaw([]byte(staleAsset))
	if err != nil {
		t.Fatal(err)
	}
	if err := writeText(assetPath, staleAsset); err != nil {
		t.Fatal(err)
	}
	compatibilityPath := filepath.Join(second, "assets", skillCompatibilityFilename)
	compatibility, err := os.ReadFile(compatibilityPath)
	if err != nil {
		t.Fatal(err)
	}
	staleCompatibility := strings.Replace(string(compatibility), "version: "+currentContract.Version, "version: "+staleContract.Version, 1)
	staleCompatibility = strings.Replace(staleCompatibility, "fingerprint: "+currentContract.Fingerprint, "fingerprint: "+staleContract.Fingerprint, 1)
	if err := writeText(compatibilityPath, staleCompatibility); err != nil {
		t.Fatal(err)
	}
	if got := inspectSkillMaterialization(link); got.Status != "stale" {
		t.Fatalf("compatible symlink with older factory contract = %#v", got)
	}
}

func TestSkillBundleProvenanceIsPortable(t *testing.T) {
	repo, tempRoot := t.TempDir(), t.TempDir()
	out := filepath.Join(tempRoot, "bundle")
	if err := skillBundleCmd(Args{"repo": repo, "out": out, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{filepath.Join(".agents", "skills", "tusker", skillProvenanceFilename), filepath.Join(".claude", "skills", "tusker", skillProvenanceFilename)} {
		raw, err := os.ReadFile(filepath.Join(out, rel))
		if err != nil {
			t.Fatal(err)
		}
		for _, forbidden := range []string{repo, out, tempRoot} {
			if strings.Contains(string(raw), forbidden) {
				t.Fatalf("bundle provenance leaked local path %q: %s", forbidden, raw)
			}
		}
	}
}

func TestWaveFactoryContractPreflightRejectsClaimedDriftAndKeepsLegacyCompatible(t *testing.T) {
	repo := t.TempDir()
	vault := filepath.Join(repo, ".tusker")
	for _, destination := range []string{filepath.Join(repo, ".agents", "skills", "tusker"), filepath.Join(repo, ".claude", "skills", "tusker")} {
		if err := installSkillPayloadCopy(destination); err != nil {
			t.Fatal(err)
		}
	}
	contract, err := embeddedFactoryIntakeContractProvenance()
	if err != nil {
		t.Fatal(err)
	}
	claimed := Note{Data: map[string]any{
		"factory_intake_contract_schema":      contract.Schema,
		"factory_intake_contract_version":     contract.Version,
		"factory_intake_contract_fingerprint": contract.Fingerprint,
	}}
	if got := waveFactoryIntakeContractBlockers(vault, claimed); len(got) != 0 {
		t.Fatalf("current factory wave blockers = %#v", got)
	}
	stale := claimed
	stale.Data = cloneMap(claimed.Data)
	stale.Data["factory_intake_contract_version"] = "0.0.0"
	if got := strings.Join(waveFactoryIntakeContractBlockers(vault, stale), "\n"); !strings.Contains(got, "regenerate the V2 plan") {
		t.Fatalf("stale planned contract blocker = %q", got)
	}
	if err := os.Remove(filepath.Join(repo, ".agents", "skills", "tusker", skillProvenanceFilename)); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(waveFactoryIntakeContractBlockers(vault, claimed), "\n"); !strings.Contains(got, "missing_provenance") || !strings.Contains(got, "tusker skill sync --repo .") {
		t.Fatalf("missing installed provenance blocker = %q", got)
	}
	if err := installSkillPayloadCopy(filepath.Join(repo, ".agents", "skills", "tusker")); err != nil {
		t.Fatal(err)
	}
	claudeManifest := filepath.Join(repo, ".claude", "skills", "tusker", skillProvenanceFilename)
	raw, err := os.ReadFile(claudeManifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeText(claudeManifest, strings.Replace(string(raw), "factory_intake_contract_version: 1.1.0", "factory_intake_contract_version: 0.0.0", 1)); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(waveFactoryIntakeContractBlockers(vault, claimed), "\n"); !strings.Contains(got, ".claude skill is incompatible") || !strings.Contains(got, "tusker skill sync --repo .") {
		t.Fatalf("mixed managed install blocker = %q", got)
	}
	if err := os.RemoveAll(filepath.Join(repo, ".agents", "skills", "tusker")); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(waveFactoryIntakeContractBlockers(vault, claimed), "\n"); !strings.Contains(got, ".agents skill is missing") {
		t.Fatalf("missing managed surface blocker = %q", got)
	}
	legacy := Note{Data: map[string]any{}}
	if got := waveFactoryIntakeContractBlockers(vault, legacy); len(got) != 0 {
		t.Fatalf("legacy wave lost compatibility: %#v", got)
	}
	legacyV2 := Note{Data: map[string]any{"delivery_plan_schema": deliveryPlanV2Schema}}
	if got := strings.Join(waveFactoryIntakeContractBlockers(vault, legacyV2), "\n"); !strings.Contains(got, "missing from a V2-derived wave") || !strings.Contains(got, "re-import") {
		t.Fatalf("unclaimed legacy V2 wave remained executable: %q", got)
	}
}

func TestSkillSyncCopyUsesValidatedCanonicalSource(t *testing.T) {
	source := t.TempDir()
	writeCanonicalTuskerSkillFixture(t, source)
	canonical := filepath.Join(source, "skills", "tusker")
	if err := writeText(filepath.Join(canonical, "references", "canonical-only.md"), "not embedded\n"); err != nil {
		t.Fatal(err)
	}
	repo := t.TempDir()
	if err := skillSyncCmd(Args{"repo": repo, "source": source, "mode": skillInstallModeCopy, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	for _, destination := range []string{filepath.Join(repo, ".agents", "skills", "tusker"), filepath.Join(repo, ".claude", "skills", "tusker")} {
		if !fileExists(filepath.Join(destination, "references", "canonical-only.md")) {
			t.Fatalf("copy sync did not materialize canonical payload at %s", destination)
		}
		raw, err := os.ReadFile(filepath.Join(destination, skillProvenanceFilename))
		if err != nil || !strings.Contains(string(raw), "source_kind: canonical") || !strings.Contains(string(raw), "source_identity: canonical://skills/tusker") {
			t.Fatalf("canonical copy provenance = %q (%v)", raw, err)
		}
	}
}

func TestSkillSyncContainsWritesToManagedTuskerPackages(t *testing.T) {
	source := t.TempDir()
	writeCanonicalTuskerSkillFixture(t, source)
	repo := t.TempDir()
	preserved := map[string]string{
		"AGENTS.md":                      "user instructions\n",
		"CLAUDE.md":                      "other user instructions\n",
		".tusker/SKILL.md":               "project knowledge\n",
		".agents/skills/custom/SKILL.md": "custom skill\n",
		".claude/plugins/user-plugin/settings.json": "{\"secret\":\"keep\"}\n",
	}
	for rel, content := range preserved {
		if err := writeText(filepath.Join(repo, rel), content); err != nil {
			t.Fatal(err)
		}
	}
	if err := skillSyncCmd(Args{"repo": repo, "source": source, "mode": skillInstallModeLink, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	for rel, want := range preserved {
		got, err := os.ReadFile(filepath.Join(repo, rel))
		if err != nil || string(got) != want {
			t.Fatalf("skill sync changed preserved %s: %q (%v)", rel, got, err)
		}
	}
	for _, destination := range []string{filepath.Join(repo, ".agents", "skills", "tusker"), filepath.Join(repo, ".claude", "skills", "tusker")} {
		if info, err := os.Lstat(destination); err != nil || info.Mode()&os.ModeSymlink == 0 {
			t.Fatalf("managed package not refreshed as symlink: %s (%v)", destination, err)
		}
	}
}

func TestSymlinkMatchingMetadataWithoutCanonicalPackageIsIncompatible(t *testing.T) {
	repo := t.TempDir()
	fake := filepath.Join(t.TempDir(), "tusker")
	raw, err := os.ReadFile(filepath.Join("..", "..", "skills", "tusker", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if err := writeText(filepath.Join(fake, "SKILL.md"), string(raw)); err != nil {
		t.Fatal(err)
	}
	for _, destination := range []string{filepath.Join(repo, ".agents", "skills", "tusker"), filepath.Join(repo, ".claude", "skills", "tusker")} {
		if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(fake, destination); err != nil {
			t.Fatal(err)
		}
		if got := inspectSkillMaterialization(destination); got.Status != "incompatible" {
			t.Fatalf("fake matching-metadata symlink = %#v", got)
		}
	}
	contract, err := embeddedFactoryIntakeContractProvenance()
	if err != nil {
		t.Fatal(err)
	}
	wave := Note{Data: map[string]any{"factory_intake_contract_schema": contract.Schema, "factory_intake_contract_version": contract.Version, "factory_intake_contract_fingerprint": contract.Fingerprint}}
	if got := strings.Join(waveFactoryIntakeContractBlockers(filepath.Join(repo, ".tusker"), wave), "\n"); strings.Count(got, "is incompatible") != 2 {
		t.Fatalf("fake targets did not block both managed surfaces: %q", got)
	}
}

func TestSkillSyncRejectsRepoParentSymlinkWithoutTouchingExternalContent(t *testing.T) {
	source := t.TempDir()
	writeCanonicalTuskerSkillFixture(t, source)
	repo, external := t.TempDir(), t.TempDir()
	sentinel := filepath.Join(external, "sentinel.txt")
	if err := writeText(sentinel, "keep\n"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, filepath.Join(repo, ".agents")); err != nil {
		t.Fatal(err)
	}
	if err := skillSyncCmd(Args{"repo": repo, "source": source, "mode": skillInstallModeLink, "quiet": "true"}); err == nil || !strings.Contains(err.Error(), "ancestor must be a real directory") {
		t.Fatalf("expected parent-symlink containment refusal, got %v", err)
	}
	if got, err := os.ReadFile(sentinel); err != nil || string(got) != "keep\n" {
		t.Fatalf("sync touched external sentinel: %q (%v)", got, err)
	}
}

func TestSkillBundleRefusesBroadOrArbitraryExistingOutput(t *testing.T) {
	repo := t.TempDir()
	sentinel := filepath.Join(repo, "sentinel.txt")
	if err := writeText(sentinel, "keep\n"); err != nil {
		t.Fatal(err)
	}
	if err := skillBundleCmd(Args{"repo": repo, "out": repo, "quiet": "true"}); err == nil {
		t.Fatal("bundle accepted repository root as output")
	}
	arbitrary := filepath.Join(t.TempDir(), "existing")
	if err := writeText(filepath.Join(arbitrary, "sentinel.txt"), "keep\n"); err != nil {
		t.Fatal(err)
	}
	if err := skillBundleCmd(Args{"repo": repo, "out": arbitrary, "quiet": "true"}); err == nil || !strings.Contains(err.Error(), "arbitrary existing") {
		t.Fatalf("bundle accepted arbitrary existing directory: %v", err)
	}
	if got, err := os.ReadFile(filepath.Join(arbitrary, "sentinel.txt")); err != nil || string(got) != "keep\n" {
		t.Fatalf("bundle touched arbitrary sentinel: %q (%v)", got, err)
	}
	external := t.TempDir()
	link := filepath.Join(t.TempDir(), "bundle-link")
	if err := os.Symlink(external, link); err != nil {
		t.Fatal(err)
	}
	if err := skillBundleCmd(Args{"repo": repo, "out": link, "quiet": "true"}); err == nil || !strings.Contains(err.Error(), "not a symlink") {
		t.Fatalf("bundle accepted symlink output: %v", err)
	}
}
