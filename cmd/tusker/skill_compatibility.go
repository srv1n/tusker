package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	skillbundle "tusker/skills/tusker"

	"gopkg.in/yaml.v3"
)

const (
	skillCompatibilitySchema   = "tusker.skill-compatibility/v1"
	skillCompatibilityFilename = "compatibility.yaml"
)

type skillCompatibilityContract struct {
	Schema                   string                          `yaml:"schema" json:"schema"`
	Version                  int                             `yaml:"version" json:"version"`
	WorkflowMin              int                             `yaml:"workflow_min" json:"workflow_min"`
	WorkflowMax              int                             `yaml:"workflow_max" json:"workflow_max"`
	TrackerSchemaVersions    []int                           `yaml:"tracker_schema_versions" json:"tracker_schema_versions"`
	WaveAuthorizationSchemas []string                        `yaml:"wave_authorization_schemas" json:"wave_authorization_schemas"`
	FactoryIntakeContract    factoryIntakeContractProvenance `yaml:"factory_intake_contract" json:"factory_intake_contract"`
	CanonicalSource          string                          `yaml:"canonical_source" json:"canonical_source"`
	MaterializationSchema    string                          `yaml:"materialization_schema" json:"materialization_schema"`
	PrimaryGuides            []string                        `yaml:"primary_guides" json:"primary_guides"`
}

func loadSkillCompatibilityContract(raw []byte) (skillCompatibilityContract, error) {
	var contract skillCompatibilityContract
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	decoder.KnownFields(true)
	if err := decoder.Decode(&contract); err != nil {
		return skillCompatibilityContract{}, fmt.Errorf("parse Tusker skill compatibility contract: %w", err)
	}
	if err := validateSkillCompatibilityContract(contract); err != nil {
		return skillCompatibilityContract{}, err
	}
	return contract, nil
}

func embeddedSkillCompatibilityContract() (skillCompatibilityContract, error) {
	raw, err := skillbundle.GetAsset(skillCompatibilityFilename)
	if err != nil {
		return skillCompatibilityContract{}, err
	}
	return loadSkillCompatibilityContract([]byte(raw))
}

func readSkillCompatibilityContract(root string) (skillCompatibilityContract, error) {
	raw, err := os.ReadFile(filepath.Join(root, "assets", skillCompatibilityFilename))
	if err != nil {
		return skillCompatibilityContract{}, err
	}
	return loadSkillCompatibilityContract(raw)
}

func validateSkillCompatibilityContract(contract skillCompatibilityContract) error {
	if contract.Schema != skillCompatibilitySchema || contract.Version <= 0 {
		return fmt.Errorf("Tusker skill compatibility schema or version is incompatible")
	}
	if contract.WorkflowMin <= 0 || contract.WorkflowMax < contract.WorkflowMin || len(contract.TrackerSchemaVersions) == 0 || len(contract.WaveAuthorizationSchemas) == 0 {
		return fmt.Errorf("Tusker skill workflow, tracker, or wave-authorization range is incomplete")
	}
	if strings.TrimSpace(contract.CanonicalSource) == "" || strings.TrimSpace(contract.MaterializationSchema) == "" {
		return fmt.Errorf("Tusker skill source or materialization schema is incomplete")
	}
	if len(contract.PrimaryGuides) == 0 {
		return fmt.Errorf("Tusker skill primary guide contract is incomplete")
	}
	if contract.FactoryIntakeContract.Schema == "" || contract.FactoryIntakeContract.Version == "" || contract.FactoryIntakeContract.Fingerprint == "" {
		return fmt.Errorf("Tusker skill factory-intake compatibility is incomplete")
	}
	return nil
}

func skillCompatibilityStatusForPackage(root string) (string, string) {
	have, err := readSkillCompatibilityContract(root)
	if err != nil {
		if os.IsNotExist(err) {
			if legacy, legacyErr := legacySkillMetadata(root); legacyErr == nil && legacy.Schema != "" {
				return "stale", "legacy SKILL.md compatibility metadata predates assets/compatibility.yaml"
			}
			return "incompatible", "Tusker skill compatibility manifest is missing"
		}
		return "incompatible", err.Error()
	}
	want, err := embeddedSkillCompatibilityContract()
	if err != nil {
		return "incompatible", err.Error()
	}
	if have.Schema != want.Schema || have.WorkflowMin > want.WorkflowMin || have.WorkflowMax < want.WorkflowMax ||
		!containsInt(have.TrackerSchemaVersions, 7) || !containsString(have.WaveAuthorizationSchemas, waveAuthorizationSchema) {
		return "incompatible", "Tusker skill compatibility range does not support this binary"
	}
	if have.Version != want.Version || have.FactoryIntakeContract != want.FactoryIntakeContract ||
		have.CanonicalSource != want.CanonicalSource || have.MaterializationSchema != want.MaterializationSchema ||
		strings.Join(have.PrimaryGuides, "\n") != strings.Join(want.PrimaryGuides, "\n") {
		return "stale", "Tusker skill compatibility contract predates the installed binary"
	}
	return "current", ""
}

func legacySkillMetadata(root string) (factoryIntakeContractProvenance, error) {
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

func embeddedSkillPayloadFingerprint() (string, error) {
	entries, err := skillbundle.PayloadEntries()
	if err != nil {
		return "", err
	}
	hash := sha256.New()
	for _, entry := range entries {
		if filepath.Base(entry.Relative) == skillProvenanceFilename {
			continue
		}
		_, _ = hash.Write([]byte(filepath.ToSlash(entry.Relative)))
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write([]byte(entry.Content))
		_, _ = hash.Write([]byte{0})
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil)), nil
}

func containsInt(values []int, wanted int) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
