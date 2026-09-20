package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestVerificationReceiptPreservesAuthoredContract(t *testing.T) {
	for _, notesColumn := range []bool{true, false} {
		name := "with notes"
		if !notesColumn {
			name = "without notes"
		}
		t.Run(name, func(t *testing.T) {
			vault, store, project := authorityFixture(t)
			repo := v7RepoRoot(vault)
			runGit(t, "-C", repo, "init", "-q")
			if err := writeText(filepath.Join(repo, "proof.sh"), "test 2 -eq $((1 + 1)) || exit 1\nprintf '1 pass\\n'\n"); err != nil {
				t.Fatal(err)
			}
			const id = "APP-T-0001"
			writePendingDirectTask(t, vault, id, "W-0001", map[string]any{"owned_paths": []string{"proof.sh"}, "proof_required": []string{}})
			writeDirectWave(t, vault, "W-0001", []string{id}, nil)
			rewriteTaskFile(t, vault, id, func(data map[string]any, body string) (map[string]any, string) {
				header, separator, row := "| Covers | Check | Result", "|:-------|:-----|:------", "| A1 | command: sh proof.sh | pending"
				if notesColumn {
					header += " | Notes"
					separator += "|:-----"
					row += " | Run the actual assertion."
				}
				body = replaceSection(body, "## Verification", header+" |\n"+separator+"|\n"+row+" |")
				// Direct authoring accepts tables without padding below the heading.
				body = strings.Replace(body, "## Verification\n\n", "## Verification\n", 1)
				return data, body + "## Handoff\nKeep this authored section intact.\n"
			})
			task, err := resolveV7Note(vault, id, "task")
			if err != nil {
				t.Fatal(err)
			}
			beforeCanon := directWaveCanonicalContractBody(task.Body)
			beforeFingerprint := stringField(task.Data, "contract_fingerprint")
			fresh, report, failures, err := executeV7CommandVerificationRows(vault, task, nil, "reviewer:test", true)
			if err != nil || len(failures) != 0 || report.Status != "satisfied" {
				t.Fatalf("successful non-Go proof rejected: report=%#v failures=%#v err=%v", report, failures, err)
			}
			if got := directWaveCanonicalContractBody(fresh.Body); got != beforeCanon {
				t.Fatalf("verification write changed authored contract:\nbefore:\n%s\nafter:\n%s", beforeCanon, got)
			}
			if got := directWaveTaskContractFingerprint(fresh.Data, fresh.Body); got != beforeFingerprint {
				t.Fatalf("contract drifted: before=%s after=%s", beforeFingerprint, got)
			}
			persisted, err := resolveV7Note(vault, id, "task")
			if err != nil {
				t.Fatal(err)
			}
			if missing := v7VerificationReceiptRequirementMissing(vault, persisted); missing != "" {
				t.Fatal(missing)
			}
			review, err := buildDirectWaveReview(vault, store, project.ProjectID, "W-0001", nil)
			if err != nil {
				t.Fatal(err)
			}
			for _, blocker := range review.Blockers {
				if blocker.Code == "CONTRACT_FINGERPRINT_STALE" || blocker.Code == "STRICT_PROOF_STALE" {
					t.Fatalf("fresh receipt is stale to wave review: %#v", blocker)
				}
			}
			// Receipt identity must still reject a real check amendment.
			persisted.Body = strings.Replace(persisted.Body, "command: sh proof.sh", "command: false", 1)
			if missing := v7VerificationReceiptRequirementMissing(vault, persisted); missing == "" {
				t.Fatal("authored check amendment retained a current receipt")
			}
		})
	}
}
