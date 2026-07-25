package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const skillMaterializationSchema = "tusker.skill-materialization/v1"
const skillProvenanceFilename = ".tusker-skill-provenance.yaml"

// skillMaterializationProvenance is written only into materialized packages.
// Its payload hash excludes this manifest and timestamps, so it is stable and
// can detect a local edit without recursively hashing its own output.
type skillMaterializationProvenance struct {
	Schema                     string `yaml:"schema" json:"schema"`
	SourceKind                 string `yaml:"source_kind" json:"source_kind"`
	SourceIdentity             string `yaml:"source_identity" json:"source_identity"`
	FactoryContractSchema      string `yaml:"factory_intake_contract_schema" json:"factory_intake_contract_schema"`
	FactoryContractVersion     string `yaml:"factory_intake_contract_version" json:"factory_intake_contract_version"`
	FactoryContractFingerprint string `yaml:"factory_intake_contract_fingerprint" json:"factory_intake_contract_fingerprint"`
	PayloadFingerprint         string `yaml:"payload_fingerprint" json:"payload_fingerprint"`
}

type skillProvenanceReport struct {
	Status     string                          `json:"status"`
	SourceKind string                          `json:"source_kind,omitempty"`
	Manifest   *skillMaterializationProvenance `json:"manifest,omitempty"`
	Message    string                          `json:"message,omitempty"`
}

func skillPayloadFingerprint(root string) (string, error) {
	entries := []string{}
	contents := map[string][]byte{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || entry.Name() == skillProvenanceFilename || entry.Name() == ".DS_Store" {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		entries = append(entries, rel)
		contents[rel] = raw
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(entries)
	h := sha256.New()
	for _, rel := range entries {
		_, _ = h.Write([]byte(rel))
		_, _ = h.Write([]byte{0})
		_, _ = h.Write(contents[rel])
		_, _ = h.Write([]byte{0})
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}

func writeSkillMaterializationProvenance(destination, sourceKind, sourceIdentity string) error {
	contract, err := embeddedFactoryIntakeContractProvenance()
	if err != nil {
		return err
	}
	return writeSkillMaterializationProvenanceWithContract(destination, sourceKind, sourceIdentity, contract)
}

func writeSkillMaterializationProvenanceWithContract(destination, sourceKind, sourceIdentity string, contract factoryIntakeContractProvenance) error {
	payload, err := skillPayloadFingerprint(destination)
	if err != nil {
		return err
	}
	manifest := skillMaterializationProvenance{
		Schema: skillMaterializationSchema, SourceKind: sourceKind, SourceIdentity: sourceIdentity,
		FactoryContractSchema: contract.Schema, FactoryContractVersion: contract.Version,
		FactoryContractFingerprint: contract.Fingerprint, PayloadFingerprint: payload,
	}
	raw, err := yaml.Marshal(manifest)
	if err != nil {
		return err
	}
	return writeText(filepath.Join(destination, skillProvenanceFilename), string(raw))
}

func factoryIntakeContractProvenanceFromPackage(root string) (factoryIntakeContractProvenance, error) {
	if err := validateTuskerSkillPackageShape(root); err != nil {
		return factoryIntakeContractProvenance{}, err
	}
	raw, err := os.ReadFile(filepath.Join(root, "assets", "factory-intake-contract.yaml"))
	if err != nil {
		return factoryIntakeContractProvenance{}, err
	}
	contract, err := factoryIntakeContractProvenanceFromRaw(raw)
	if err != nil {
		return factoryIntakeContractProvenance{}, err
	}
	metadata, err := readSkillMetadata(root)
	if err != nil {
		return factoryIntakeContractProvenance{}, err
	}
	if status, detail := factoryContractStatus(metadata, contract); status != "current" {
		return factoryIntakeContractProvenance{}, fmt.Errorf("canonical skill metadata does not match its contract: %s", detail)
	}
	return contract, nil
}

func validateCurrentCanonicalTuskerSkillPackage(root string) error {
	if err := validateTuskerSkillCompatibilityMetadata(root); err != nil {
		return err
	}
	have, err := factoryIntakeContractProvenanceFromPackage(root)
	if err != nil {
		return fmt.Errorf("canonical Tusker skill package is invalid: %w", err)
	}
	want, err := embeddedFactoryIntakeContractProvenance()
	if err != nil {
		return fmt.Errorf("load embedded factory-intake contract: %w", err)
	}
	if status, detail := factoryContractStatus(have, want); status != "current" {
		return fmt.Errorf("canonical Tusker skill factory-intake contract is %s: %s", status, detail)
	}
	return nil
}

func validateTuskerSkillCompatibilityMetadata(root string) error {
	data, _, err := parseFrontmatterMustRead(filepath.Join(root, "SKILL.md"))
	if err != nil {
		return err
	}
	metadata, ok := data["metadata"].(map[string]any)
	if !ok {
		return fmt.Errorf("canonical Tusker skill metadata is incompatible")
	}
	if stringField(metadata, "wave_authorization_schema") != waveAuthorizationSchema {
		return fmt.Errorf("canonical Tusker skill wave_authorization_schema is incompatible")
	}
	if intField(metadata, "workflow_version") != 1 {
		return fmt.Errorf("canonical Tusker skill workflow_version is incompatible")
	}
	if intField(metadata, "tracker_schema_version") != 7 {
		return fmt.Errorf("canonical Tusker skill tracker_schema_version is incompatible")
	}
	return nil
}

func validateTuskerSkillPackageShape(root string) error {
	data, body, err := parseFrontmatterMustRead(filepath.Join(root, "SKILL.md"))
	if err != nil {
		return err
	}
	if stringField(data, "name") != "tusker" || !strings.Contains(body, "# Tusker Operator Skill") {
		return fmt.Errorf("Tusker skill package identity is invalid")
	}
	for _, rel := range []string{".", "references", "assets"} {
		info, err := os.Lstat(filepath.Join(root, rel))
		if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("Tusker skill package requires real directory %s", filepath.ToSlash(rel))
		}
	}
	for _, rel := range []string{"SKILL.md", filepath.Join("references", "COMMANDS.md"), filepath.Join("references", "REPO_CONTRACT.md"), filepath.Join("references", "WORKFLOW.md"), filepath.Join("assets", "factory-intake-contract.yaml")} {
		info, err := os.Lstat(filepath.Join(root, rel))
		if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return fmt.Errorf("Tusker skill package requires regular file %s", filepath.ToSlash(rel))
		}
	}
	return nil
}

func readSkillMetadata(root string) (factoryIntakeContractProvenance, error) {
	data, _, err := parseFrontmatterMustRead(filepath.Join(root, "SKILL.md"))
	if err != nil {
		return factoryIntakeContractProvenance{}, err
	}
	metadata := mapField(data, "metadata")
	return factoryIntakeContractProvenance{
		Schema:      stringField(metadata, "factory_intake_contract_schema"),
		Version:     stringField(metadata, "factory_intake_contract_version"),
		Fingerprint: stringField(metadata, "factory_intake_contract_fingerprint"),
	}, nil
}

func factoryContractStatus(have, want factoryIntakeContractProvenance) (string, string) {
	if have.Schema == "" || have.Version == "" || have.Fingerprint == "" {
		return "incompatible", "factory-intake contract metadata is incomplete"
	}
	if have.Schema != want.Schema {
		return "incompatible", fmt.Sprintf("factory-intake contract schema %q is incompatible with %q", have.Schema, want.Schema)
	}
	if have.Version != want.Version || have.Fingerprint != want.Fingerprint {
		return "stale", "factory-intake contract version or fingerprint predates the current canonical contract"
	}
	return "current", ""
}

// inspectSkillMaterialization never consults a cached manifest for symlinks:
// resolving the target and reading its metadata is the only truthful answer.
func inspectSkillMaterialization(destination string) skillProvenanceReport {
	info, err := os.Lstat(destination)
	if os.IsNotExist(err) {
		return skillProvenanceReport{Status: "missing", Message: "managed Tusker skill package is missing"}
	}
	if err != nil {
		return skillProvenanceReport{Status: "incompatible", Message: err.Error()}
	}
	want, err := embeddedFactoryIntakeContractProvenance()
	if err != nil {
		return skillProvenanceReport{Status: "incompatible", Message: err.Error()}
	}
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := filepath.EvalSymlinks(destination)
		if err != nil {
			return skillProvenanceReport{Status: "missing", SourceKind: skillInstallModeLink, Message: "managed Tusker skill symlink is broken"}
		}
		if err := validateTuskerSkillCompatibilityMetadata(target); err != nil {
			return skillProvenanceReport{Status: "incompatible", SourceKind: skillInstallModeLink, Message: err.Error()}
		}
		have, err := factoryIntakeContractProvenanceFromPackage(target)
		if err != nil {
			return skillProvenanceReport{Status: "incompatible", SourceKind: skillInstallModeLink, Message: err.Error()}
		}
		status, message := factoryContractStatus(have, want)
		return skillProvenanceReport{Status: status, SourceKind: skillInstallModeLink, Message: message}
	}
	manifestPath := filepath.Join(destination, skillProvenanceFilename)
	raw, err := os.ReadFile(manifestPath)
	if os.IsNotExist(err) {
		return skillProvenanceReport{Status: "missing_provenance", SourceKind: skillInstallModeCopy, Message: "materialized Tusker skill has no provenance manifest"}
	}
	if err != nil {
		return skillProvenanceReport{Status: "incompatible", SourceKind: skillInstallModeCopy, Message: err.Error()}
	}
	var manifest skillMaterializationProvenance
	if err := yaml.Unmarshal(raw, &manifest); err != nil || manifest.Schema != skillMaterializationSchema || !validSkillProvenanceSource(manifest) || manifest.PayloadFingerprint == "" || manifest.FactoryContractSchema == "" || manifest.FactoryContractVersion == "" || manifest.FactoryContractFingerprint == "" {
		return skillProvenanceReport{Status: "incompatible", SourceKind: skillInstallModeCopy, Message: "materialized Tusker skill provenance schema is incompatible"}
	}
	result := skillProvenanceReport{SourceKind: skillInstallModeCopy, Manifest: &manifest}
	payload, err := skillPayloadFingerprint(destination)
	if err != nil || payload != manifest.PayloadFingerprint {
		result.Status, result.Message = "locally_modified", "materialized Tusker skill payload differs from its recorded provenance"
		return result
	}
	if err := validateTuskerSkillCompatibilityMetadata(destination); err != nil {
		result.Status, result.Message = "incompatible", err.Error()
		return result
	}
	packaged, err := factoryIntakeContractProvenanceFromPackage(destination)
	if err != nil {
		result.Status, result.Message = "incompatible", "materialized Tusker package contract is invalid: "+err.Error()
		return result
	}
	have := factoryIntakeContractProvenance{Schema: manifest.FactoryContractSchema, Version: manifest.FactoryContractVersion, Fingerprint: manifest.FactoryContractFingerprint}
	if have != packaged {
		result.Status, result.Message = "incompatible", "materialized Tusker manifest contradicts its packaged skill contract"
		return result
	}
	result.Status, result.Message = factoryContractStatus(have, want)
	return result
}

func validSkillProvenanceSource(manifest skillMaterializationProvenance) bool {
	switch manifest.SourceKind {
	case "embedded":
		return manifest.SourceIdentity == "embedded://tusker/skills/tusker"
	case "canonical":
		return manifest.SourceIdentity == "canonical://skills/tusker"
	default:
		return false
	}
}

func inspectTuskerSkillPackage(root string) skillProvenanceReport {
	if filepath.Base(filepath.Clean(root)) != currentSkillInstallDir {
		return skillProvenanceReport{Status: "not_tusker"}
	}
	if info, err := os.Lstat(root); err == nil && info.Mode()&os.ModeSymlink == 0 {
		if repo, rootErr := findRepoRoot(root); rootErr == nil && sameCleanPath(root, filepath.Join(repo, "skills", currentSkillInstallDir)) {
			want, wantErr := embeddedFactoryIntakeContractProvenance()
			have, haveErr := factoryIntakeContractProvenanceFromPackage(root)
			compatErr := validateTuskerSkillCompatibilityMetadata(root)
			if wantErr != nil || haveErr != nil || compatErr != nil {
				return skillProvenanceReport{Status: "incompatible", Message: firstNonEmpty(errorString(wantErr), errorString(haveErr), errorString(compatErr))}
			}
			status, message := factoryContractStatus(have, want)
			return skillProvenanceReport{Status: status, SourceKind: "canonical", Message: message}
		}
	}
	return inspectSkillMaterialization(root)
}

func skillSyncRepairAction() string {
	return "run tusker skill sync --repo . --source <canonical-tusker-checkout>"
}

func portableSkillSourceIdentity(kind string) string {
	if strings.TrimSpace(kind) == "canonical" {
		return "canonical://skills/tusker"
	}
	return "embedded://tusker/skills/tusker"
}
