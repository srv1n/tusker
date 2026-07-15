package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestAgentSkillPackageDoctorAcceptsCanonicalTuskerPackage(t *testing.T) {
	root := filepath.Join("..", "..", "skills", "tusker")
	errs, warns := skillDoctorIssues(root, true, true)
	if len(errs) != 0 || len(warns) != 0 {
		t.Fatalf("canonical Agent Skills package failed doctor: errors=%#v warnings=%#v", errs, warns)
	}
}

func TestAgentSkillPackageDoctorEnforcesMetadataAndPackageIdentity(t *testing.T) {
	root := filepath.Join(t.TempDir(), "wrong-directory")
	if err := writeText(filepath.Join(root, "SKILL.md"), `---
name: tusker
description: Operate Tusker. Use when a repository contains .tusker.
metadata:
  tracker_schema_version: 7
---

# Tusker
`); err != nil {
		t.Fatal(err)
	}
	errs, _ := skillDoctorIssues(root, true, true)
	codes := map[string]bool{}
	for _, current := range errs {
		codes[current.Code] = true
	}
	for _, want := range []string{"AGENT_SKILL_NAME_PATH_MISMATCH", "AGENT_SKILL_METADATA_VALUE"} {
		if !codes[want] {
			t.Fatalf("missing %s in %#v", want, errs)
		}
	}
}

func TestAgentSkillPackageDoctorChecksReferenceAndBodyBudgets(t *testing.T) {
	root := filepath.Join(t.TempDir(), "tusker")
	body := strings.Repeat("instruction\n", 501) + "See `references/MISSING.md`.\n"
	if err := writeText(filepath.Join(root, "SKILL.md"), `---
name: tusker
description: Operate Tusker. Use when a repository contains .tusker.
---

`+body); err != nil {
		t.Fatal(err)
	}
	errs, _ := skillDoctorIssues(root, true, true)
	codes := map[string]bool{}
	for _, current := range errs {
		codes[current.Code] = true
	}
	if !codes["AGENT_SKILL_BODY_BUDGET"] || !codes[errorNotFound] {
		t.Fatalf("expected body and missing-reference errors, got %#v", errs)
	}
}
