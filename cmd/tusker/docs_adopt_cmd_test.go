package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tusker/internal/docgraph"
)

func TestInventoryDocsAdoptProposesLeaveAndPromote(t *testing.T) {
	repo := t.TempDir()
	writeTestDoc(t, repo, "docs/system/00-overview.md", "---\nsubject: overview\n---\n# Overview\n")
	writeTestDoc(t, repo, "README.md", "# Project\n")
	writeTestDoc(t, repo, "docs/legacy.md", "# Legacy system\n\nCurrent behavior.\n")
	corpus, _, err := docgraph.LoadRepository(repo)
	if err != nil {
		t.Fatal(err)
	}
	proposals, err := inventoryDocsAdopt(repo, corpus)
	if err != nil {
		t.Fatal(err)
	}
	var foundLeave, foundPromote bool
	for _, proposal := range proposals {
		if proposal.Path == "README.md" && proposal.Disposition == "leave" {
			foundLeave = true
		}
		if proposal.Path == "docs/legacy.md" && proposal.Disposition == "promote" && proposal.Target == "docs/system/legacy-system.md" {
			foundPromote = true
		}
	}
	if !foundLeave || !foundPromote {
		t.Fatalf("unexpected adoption proposals: %#v", proposals)
	}
}

func TestApplyDocsAdoptPreservesLegacySource(t *testing.T) {
	repo := t.TempDir()
	writeTestDoc(t, repo, "docs/system/00-overview.md", "---\nsubject: overview\n---\n# Overview\n")
	writeTestDoc(t, repo, "docs/legacy.md", "# Legacy system\n\nCurrent behavior.\n")
	proposal := docsAdoptProposal{Path: "docs/legacy.md", Subject: "Legacy system", Disposition: "promote", Target: "docs/system/legacy-system.md"}
	if err := applyDocsAdoptProposal(repo, proposal); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(repo, filepath.FromSlash(proposal.Target))); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(repo, "docs/legacy.md")); err != nil {
		t.Fatalf("legacy source must remain for review: %v", err)
	}
	if _, err := docgraph.ParseDocHeaders(proposal.Target, mustReadFile(t, filepath.Join(repo, filepath.FromSlash(proposal.Target)))); err != nil {
		t.Fatalf("promoted document header invalid: %v", err)
	}
	legacy := string(mustReadFile(t, filepath.Join(repo, "docs/legacy.md")))
	if legacy != "# Legacy system\n\nCurrent behavior.\n" {
		t.Fatalf("promoted legacy source was changed: %s", legacy)
	}
}

func TestApplyDocsAdoptMergePreservesLegacySource(t *testing.T) {
	repo := t.TempDir()
	writeTestDoc(t, repo, "docs/system/00-overview.md", "---\nsubject: overview\n---\n# Overview\n")
	writeTestDoc(t, repo, "docs/system/cli.md", "---\nsubject: cli\npart_of: overview\nstatus: canonical\n---\n# CLI\n\nCurrent answer.\n")
	writeTestDoc(t, repo, "docs/old-cli.md", "# Old CLI\n\nLegacy answer.\n")
	proposal := docsAdoptProposal{Path: "docs/old-cli.md", Subject: "CLI", Disposition: "merge", Target: "docs/system/cli.md"}
	if err := applyDocsAdoptProposal(repo, proposal); err != nil {
		t.Fatal(err)
	}
	canonical := string(mustReadFile(t, filepath.Join(repo, "docs/system/cli.md")))
	if !strings.Contains(canonical, "adopted-source:docs/old-cli.md") || !strings.Contains(canonical, "Legacy answer.") {
		t.Fatalf("merge did not preserve legacy material: %s", canonical)
	}
	legacy := string(mustReadFile(t, filepath.Join(repo, "docs/old-cli.md")))
	if legacy != "# Old CLI\n\nLegacy answer.\n" {
		t.Fatalf("merge changed legacy source: %s", legacy)
	}
	if err := applyDocsAdoptProposal(repo, proposal); err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(mustReadFile(t, filepath.Join(repo, "docs/system/cli.md"))), "adopted-source:docs/old-cli.md"); got != 1 {
		t.Fatalf("merge was not idempotent; marker count=%d", got)
	}
}

func TestApplyDocsAdoptTombstoneRequiresSuccessor(t *testing.T) {
	repo := t.TempDir()
	writeTestDoc(t, repo, "docs/system/00-overview.md", "---\nsubject: overview\n---\n# Overview\n")
	writeTestDoc(t, repo, "docs/old.md", "# Old\n")
	err := applyDocsAdoptProposal(repo, docsAdoptProposal{Path: "docs/old.md", Subject: "Old", Disposition: "tombstone", Target: "docs/system/missing.md"})
	if err == nil || !strings.Contains(err.Error(), "successor target does not exist") {
		t.Fatalf("tombstone disposition = %v", err)
	}
}

func TestApplyDocsAdoptRefusesCanonicalTargetCollision(t *testing.T) {
	repo := t.TempDir()
	writeTestDoc(t, repo, "docs/system/existing.md", "---\nsubject: Existing\nstatus: canonical\n---\n# Existing\n")
	writeTestDoc(t, repo, "docs/legacy.md", "# Legacy\n\nKeep source.\n")
	err := applyDocsAdoptProposal(repo, docsAdoptProposal{Path: "docs/legacy.md", Subject: "Legacy", Disposition: "promote", Target: "docs/system/existing.md"})
	if err == nil || !strings.Contains(err.Error(), "canonical target collision") {
		t.Fatalf("target collision was accepted: %v", err)
	}
	got := string(mustReadFile(t, filepath.Join(repo, "docs/system/existing.md")))
	if strings.Contains(got, "Keep source.") {
		t.Fatalf("target collision changed canonical bytes: %s", got)
	}
}

func TestInventoryDocsAdoptSkipsVaultAndRuntimeTrees(t *testing.T) {
	repo := t.TempDir()
	writeTestDoc(t, repo, "docs/system/00-overview.md", "---\nsubject: overview\n---\n# Overview\n")
	for _, rel := range []string{".tusker/SKILL.md", ".tusker/knowledge/domains/a/INDEX.md", ".tusker/dashboards/review.md", ".tusker-worktrees/wt/notes.md", ".chatgpt-handoff/context/reviewer.md", "dist/generated.md", "artifacts/review.md", "tmp/debug.md"} {
		writeTestDoc(t, repo, rel, "# Should not be adopted\n")
	}
	corpus, _, err := docgraph.LoadRepository(repo)
	if err != nil {
		t.Fatal(err)
	}
	proposals, err := inventoryDocsAdopt(repo, corpus)
	if err != nil {
		t.Fatal(err)
	}
	for _, proposal := range proposals {
		if strings.HasPrefix(proposal.Path, ".tusker/") || strings.HasPrefix(proposal.Path, ".tusker-worktrees/") || strings.HasPrefix(proposal.Path, ".chatgpt-handoff/") || strings.HasPrefix(proposal.Path, "dist/") || strings.HasPrefix(proposal.Path, "artifacts/") || strings.HasPrefix(proposal.Path, "tmp/") {
			t.Fatalf("runtime/control path was proposed: %#v", proposal)
		}
	}
}

func TestInventoryDocsAdoptLeavesControlGuidanceAndSkills(t *testing.T) {
	repo := t.TempDir()
	writeTestDoc(t, repo, "docs/system/00-overview.md", "---\nsubject: overview\n---\n# Overview\n")
	for _, rel := range []string{"WORKFLOW.md", "SKILL.md", "DOSSIER.md", "NARRATIVE-NOTES.md"} {
		writeTestDoc(t, repo, rel, "# Control guidance\n")
	}
	for _, rel := range []string{"skills/tusker/SKILL.md", "skills/tusker/references/TRACK.md", "skills/spec/SKILL.md"} {
		writeTestDoc(t, repo, rel, "# Packaged guidance\n")
	}
	writeTestDoc(t, repo, "docs/legacy.md", "# Legacy system\n\nCurrent behavior.\n")

	corpus, _, err := docgraph.LoadRepository(repo)
	if err != nil {
		t.Fatal(err)
	}
	proposals, err := inventoryDocsAdopt(repo, corpus)
	if err != nil {
		t.Fatal(err)
	}
	protected := map[string]bool{
		"WORKFLOW.md":        false,
		"SKILL.md":           false,
		"DOSSIER.md":         false,
		"NARRATIVE-NOTES.md": false,
	}
	for _, proposal := range proposals {
		if _, ok := protected[proposal.Path]; ok {
			if proposal.Disposition != "leave" {
				t.Fatalf("control guidance was offered for adoption: %#v", proposal)
			}
			protected[proposal.Path] = true
		}
		if strings.HasPrefix(proposal.Path, "skills/tusker/") || strings.HasPrefix(proposal.Path, "skills/spec/") {
			t.Fatalf("packaged skill guidance was proposed: %#v", proposal)
		}
	}
	for rel, found := range protected {
		if !found {
			t.Fatalf("control guidance was not inventoried as leave: %s", rel)
		}
	}
}

func TestInventoryDocsAdoptRefusesUntitledAndCollisions(t *testing.T) {
	repo := t.TempDir()
	writeTestDoc(t, repo, "docs/system/00-overview.md", "---\nsubject: overview\n---\n# Overview\n")
	writeTestDoc(t, repo, "docs/system/cli.md", "---\nsubject: cli\nstatus: canonical\n---\n# CLI\n")
	writeTestDoc(t, repo, "docs/old-cli-a.md", "# CLI\n\nA\n")
	writeTestDoc(t, repo, "docs/old-cli-b.md", "# CLI\n\nB\n")
	writeTestDoc(t, repo, "docs/untitled.md", "# Untitled\n\nNo stable subject.\n")
	corpus, _, err := docgraph.LoadRepository(repo)
	if err != nil {
		t.Fatal(err)
	}
	proposals, err := inventoryDocsAdopt(repo, corpus)
	if err != nil {
		t.Fatal(err)
	}
	for _, proposal := range proposals {
		if proposal.Path == "docs/old-cli-a.md" || proposal.Path == "docs/old-cli-b.md" || proposal.Path == "docs/untitled.md" {
			if proposal.Disposition != "leave" {
				t.Fatalf("unsafe collision/untitled proposal: %#v", proposal)
			}
		}
	}
}

func TestInventoryDocsAdoptLeavesSymlinkCanonicalTargets(t *testing.T) {
	repo := t.TempDir()
	writeTestDoc(t, repo, "docs/system/00-overview.md", "---\nsubject: overview\n---\n# Overview\n")
	external := filepath.Join(repo, "outside.txt")
	if err := os.WriteFile(external, []byte("---\nsubject: legacy\nstatus: canonical\n---\n# Legacy\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(repo, "docs/system/legacy.md")
	if err := os.Symlink(external, target); err != nil {
		t.Fatal(err)
	}
	writeTestDoc(t, repo, "docs/legacy.md", "# Legacy\n\nKeep source.\n")
	corpus, _, err := docgraph.LoadRepository(repo)
	if err != nil {
		t.Fatal(err)
	}
	proposals, err := inventoryDocsAdopt(repo, corpus)
	if err != nil {
		t.Fatal(err)
	}
	for _, proposal := range proposals {
		if proposal.Path == "docs/legacy.md" && proposal.Disposition != "leave" {
			t.Fatalf("symlink canonical target was offered for adoption: %#v", proposal)
		}
	}
}

func TestInventoryDocsAdoptLeavesSymlinkCanonicalParents(t *testing.T) {
	repo := t.TempDir()
	external := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, filepath.Join(repo, "docs/system")); err != nil {
		t.Fatal(err)
	}
	writeTestDoc(t, repo, "docs/legacy.md", "# Legacy\n\nKeep source.\n")
	corpus, _, err := docgraph.LoadRepository(repo)
	if err != nil {
		t.Fatal(err)
	}
	proposals, err := inventoryDocsAdopt(repo, corpus)
	if err != nil {
		t.Fatal(err)
	}
	for _, proposal := range proposals {
		if proposal.Path == "docs/legacy.md" && proposal.Disposition != "leave" {
			t.Fatalf("symlink canonical parent was offered for adoption: %#v", proposal)
		}
	}
}

func TestInventoryDocsAdoptRejectsSymlinkLegacySourcesBeforeRead(t *testing.T) {
	repo := t.TempDir()
	writeTestDoc(t, repo, "docs/system/00-overview.md", "---\nsubject: overview\n---\n# Overview\n")
	external := filepath.Join(repo, "outside.txt")
	if err := os.WriteFile(external, []byte("not markdown and should not be read"), 0o644); err != nil {
		t.Fatal(err)
	}
	legacy := filepath.Join(repo, "docs/legacy.md")
	if err := os.MkdirAll(filepath.Dir(legacy), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, legacy); err != nil {
		t.Fatal(err)
	}
	corpus, _, err := docgraph.LoadRepository(repo)
	if err != nil {
		t.Fatal(err)
	}
	proposals, err := inventoryDocsAdopt(repo, corpus)
	if err != nil {
		t.Fatal(err)
	}
	for _, proposal := range proposals {
		if proposal.Path == "docs/legacy.md" {
			if proposal.Disposition != "leave" || !strings.Contains(proposal.Reason, "source is a symlink") {
				t.Fatalf("symlink source was not rejected: %#v", proposal)
			}
			return
		}
	}
	t.Fatal("symlink source was not inventoried as a rejected proposal")
}

func TestDocsAdoptRejectsSymlinkRoots(t *testing.T) {
	realRepo := t.TempDir()
	symlinkRepo := filepath.Join(t.TempDir(), "repo-link")
	if err := os.Symlink(realRepo, symlinkRepo); err != nil {
		t.Fatal(err)
	}
	if err := docsAdoptValidateRoots(symlinkRepo, ""); err == nil || !strings.Contains(err.Error(), "symlinked repository root") {
		t.Fatalf("symlinked repository root accepted: %v", err)
	}
	if _, err := docsAdoptCanonicalRoot(symlinkRepo, "repository"); err == nil || !strings.Contains(err.Error(), "symlinked repository root") {
		t.Fatalf("canonical root accepted symlink: %v", err)
	}
	realVault := filepath.Join(realRepo, ".tusker")
	if err := os.MkdirAll(realVault, 0o755); err != nil {
		t.Fatal(err)
	}
	symlinkVault := filepath.Join(t.TempDir(), "vault-link")
	if err := os.Symlink(realVault, symlinkVault); err != nil {
		t.Fatal(err)
	}
	if err := docsAdoptCmd(Args{"vault": symlinkVault, "dry-run": "true"}); err == nil || !strings.Contains(err.Error(), "symlinked vault root") {
		t.Fatalf("symlinked vault root accepted: %v", err)
	}
}

func TestDocsAdoptApprovedNoOpDoesNotRegenerateMap(t *testing.T) {
	repo := t.TempDir()
	vault := filepath.Join(repo, ".tusker")
	if err := os.MkdirAll(vault, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestDoc(t, repo, "docs/system/00-overview.md", "---\nsubject: overview\n---\n# Overview\n")
	writeTestDoc(t, repo, "docs/untitled.md", "# Untitled\n\nManual review only.\n")
	if err := docsAdoptCmd(Args{"vault": vault, "approve": "true"}); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"docs/system/INDEX.md", "docs/system/graph.json"} {
		if _, err := os.Stat(filepath.Join(repo, filepath.FromSlash(rel))); !os.IsNotExist(err) {
			t.Fatalf("no-op approval regenerated %s: %v", rel, err)
		}
	}
}

func TestDocsAdoptLeavesMapArtifactsForExplicitReview(t *testing.T) {
	repo := t.TempDir()
	vault := filepath.Join(repo, ".tusker")
	if err := os.MkdirAll(vault, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestDoc(t, repo, "docs/system/00-overview.md", "---\nsubject: overview\n---\n# Overview\n")
	external := filepath.Join(repo, "outside.json")
	if err := os.WriteFile(external, []byte("outside"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, filepath.Join(repo, "docs/system/graph.json")); err != nil {
		t.Fatal(err)
	}
	writeTestDoc(t, repo, "docs/legacy.md", "# Legacy\n\nKeep source.\n")
	corpus, _, err := docgraph.LoadRepository(repo)
	if err != nil {
		t.Fatal(err)
	}
	proposals, err := inventoryDocsAdopt(repo, corpus)
	if err != nil {
		t.Fatal(err)
	}
	table := docsAdoptTable{Schema: docsAdoptTableSchema, ApprovedBy: "human:test", Proposals: proposals}
	table.Fingerprint = docsAdoptTableFingerprint(table.Proposals)
	tablePath := filepath.Join(repo, "adoption.json")
	raw, err := json.Marshal(table)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tablePath, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := docsAdoptCmd(Args{"vault": vault, "table": tablePath, "approve": "true", "by": "human:test"}); err != nil {
		t.Fatalf("approved adoption should not write generated map artifacts: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repo, "docs/system/legacy.md")); err != nil {
		t.Fatalf("canonical adoption did not create target: %v", err)
	}
	if target, err := os.Readlink(filepath.Join(repo, "docs/system/graph.json")); err != nil || target != external {
		t.Fatalf("map symlink changed: target=%q err=%v", target, err)
	}
}

func TestDocsAdoptUserSessionApprovalIsFingerprintBoundAndAudited(t *testing.T) {
	clearAgentSessionEnvForTest(t)
	t.Setenv("CODEX_THREAD_ID", "user-thread")
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	vault := filepath.Join(repo, ".tusker")
	if err := os.MkdirAll(vault, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestDoc(t, repo, "docs/system/00-overview.md", "---\nsubject: overview\n---\n# Overview\n")
	writeTestDoc(t, repo, "docs/legacy.md", "# Legacy\n\nKeep this source.\n")
	source, err := os.ReadFile(filepath.Join(repo, "docs/legacy.md"))
	if err != nil {
		t.Fatal(err)
	}
	proposals := []docsAdoptProposal{{
		Path: "docs/legacy.md", Subject: "Legacy", Disposition: "promote",
		Target: "docs/system/legacy.md", Reason: "user reviewed",
		SourceFingerprint: docsAdoptBytesFingerprint(source),
	}}
	fingerprint := docsAdoptTableFingerprint(proposals)
	actor := "user-session:sarav-thread"
	table := docsAdoptTable{Schema: docsAdoptTableSchema, Fingerprint: fingerprint, ApprovedBy: actor, Proposals: proposals}
	tablePath := filepath.Join(repo, "adoption.json")
	raw, err := json.Marshal(table)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tablePath, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	token := actor + "@" + fingerprint
	if err := docsAdoptCmd(Args{
		"vault": vault, "table": tablePath, "approve": "true", "by": actor,
		"approval-token": token,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(repo, "docs/system/legacy.md")); err != nil {
		t.Fatalf("user-session approval did not apply: %v", err)
	}
	if eventErrors, _, _ := validateV7Events(vault); len(eventErrors) != 0 {
		t.Fatalf("audit events failed V7 validation: %#v", eventErrors)
	}
	eventsRoot := filepath.Join(vault, "events")
	var kinds = map[string]bool{}
	err = filepath.WalkDir(eventsRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() {
			return walkErr
		}
		data := map[string]any{}
		raw, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if err := json.Unmarshal(raw, &data); err != nil {
			return err
		}
		kinds[stringField(data, "event_kind")] = true
		payload := mapField(data, "payload")
		if stringField(data, "event_kind") == "docs_adopt_approved" {
			if stringField(payload, "proposal_fingerprint") != fingerprint || stringField(data, "actor") != actor {
				t.Fatalf("approval audit lost binding: %#v", data)
			}
			if strings.Contains(string(raw), token) {
				t.Fatalf("approval audit leaked the raw token: %s", raw)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !kinds["docs_adopt_approved"] || !kinds["docs_adopt_applied"] {
		t.Fatalf("audit events = %#v, want approval and applied", kinds)
	}
}

func TestDocsAdoptUserSessionApprovalKeepsUnattendedHumanGate(t *testing.T) {
	clearAgentSessionEnvForTest(t)
	if _, _, err := docsAdoptApprovalActor(Args{"by": "user-session:operator"}, "sha256:"+strings.Repeat("a", 64)); err == nil || !strings.Contains(err.Error(), "interactive agent session") {
		t.Fatalf("user-session approval escaped human-terminal gate: %v", err)
	}
	t.Setenv("CODEX_THREAD_ID", "interactive")
	fingerprint := "sha256:" + strings.Repeat("a", 64)
	if _, _, err := docsAdoptApprovalActor(Args{"by": "user-session:operator", "approval-token": "user-session:operator@sha256:" + strings.Repeat("b", 64)}, fingerprint); err == nil || !strings.Contains(err.Error(), "different proposal fingerprint") {
		t.Fatalf("mismatched approval token was accepted: %v", err)
	}
	clearAgentSessionEnvForTest(t)
	t.Setenv("TUSKER_ATTEMPT_ID", "unattended-worker")
	if _, _, err := docsAdoptApprovalActor(Args{"by": "user-session:operator"}, fingerprint); err == nil || !strings.Contains(err.Error(), "interactive agent session") {
		t.Fatalf("dispatched worker received user-session approval: %v", err)
	}
	if _, _, err := docsAdoptApprovalActor(Args{"by": "agent:worker"}, fingerprint); err == nil || !strings.Contains(err.Error(), "human:<name> or --by user-session") {
		t.Fatalf("agent actor became a docs adoption break-glass path: %v", err)
	}
}

func TestDocsAdoptMixedTablePreflightAndApply(t *testing.T) {
	repo := t.TempDir()
	writeTestDoc(t, repo, "docs/system/00-overview.md", "---\nsubject: overview\n---\n# Overview\n")
	writeTestDoc(t, repo, "docs/system/cli.md", "---\nsubject: cli\nstatus: canonical\npart_of: overview\n---\n# CLI\n\nCurrent answer.\n")
	writeTestDoc(t, repo, "docs/system/new.md", "---\nsubject: new\nstatus: canonical\npart_of: overview\n---\n# New\n\nSuccessor.\n")
	writeTestDoc(t, repo, "docs/system/new-managed.md", "---\nsubject: new-managed\nstatus: canonical\npart_of: overview\n---\n# New managed\n\nSuccessor.\n")
	writeTestDoc(t, repo, "docs/system/old-managed.md", "---\nsubject: old-managed\nstatus: canonical\npart_of: overview\n---\n# Old managed\n\nStale managed body.\n")
	writeTestDoc(t, repo, "docs/legacy.md", "# Legacy system\n\nLegacy body.\n")
	writeTestDoc(t, repo, "docs/old-cli.md", "# CLI\n\nOld CLI body.\n")
	writeTestDoc(t, repo, "docs/old-copy.md", "# Old copy\n\nStale body.\n")
	writeTestDoc(t, repo, "README.md", "# Keep me\n")

	readSource := func(rel string) []byte {
		raw, err := os.ReadFile(filepath.Join(repo, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	promoteSource := readSource("docs/legacy.md")
	mergeSource := readSource("docs/old-cli.md")
	tombstoneSource := readSource("docs/old-copy.md")
	managedTombstoneSource := readSource("docs/system/old-managed.md")
	proposals := []docsAdoptProposal{
		{Path: "docs/legacy.md", Subject: "Legacy system", Disposition: "promote", Target: "docs/system/legacy-system.md", Reason: "reviewed", SourceFingerprint: docsAdoptBytesFingerprint(promoteSource)},
		{Path: "docs/old-cli.md", Subject: "CLI", Disposition: "merge", Target: "docs/system/cli.md", Reason: "reviewed", SourceFingerprint: docsAdoptBytesFingerprint(mergeSource)},
		{Path: "docs/old-copy.md", Subject: "Old copy", Disposition: "tombstone", Target: "docs/system/new.md", Reason: "reviewed", SourceFingerprint: docsAdoptBytesFingerprint(tombstoneSource)},
		{Path: "docs/system/old-managed.md", Subject: "old-managed", Disposition: "tombstone", Target: "docs/system/new-managed.md", Reason: "reviewed", SourceFingerprint: docsAdoptBytesFingerprint(managedTombstoneSource)},
		{Path: "README.md", Disposition: "leave", Reason: "user guidance"},
	}

	// A bad later row must fail before the first promote can write.
	bad := append([]docsAdoptProposal(nil), proposals...)
	bad[1].Target = "docs/system/missing.md"
	if _, err := preflightDocsAdoptTable(repo, bad); err == nil {
		t.Fatal("invalid mixed table unexpectedly preflighted")
	}
	if _, err := os.Stat(filepath.Join(repo, "docs/system/legacy-system.md")); !os.IsNotExist(err) {
		t.Fatalf("preflight partially applied promote: %v", err)
	}

	prepared, err := preflightDocsAdoptTable(repo, proposals)
	if err != nil {
		t.Fatal(err)
	}
	if len(prepared) != len(proposals) {
		t.Fatalf("prepared rows = %d, want %d", len(prepared), len(proposals))
	}
	if err := applyPreparedDocsAdoptTable(repo, prepared); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(repo, "docs/system/legacy-system.md")); err != nil {
		t.Fatalf("promote target missing: %v", err)
	}
	if got := string(readSource("docs/legacy.md")); got != string(promoteSource) {
		t.Fatalf("promote rewrote legacy source: %q", got)
	}
	merged := string(readSource("docs/system/cli.md"))
	if !strings.Contains(merged, "adopted-source:docs/old-cli.md") || !strings.Contains(merged, "Old CLI body.") {
		t.Fatalf("merge did not preserve legacy material: %s", merged)
	}
	tombstone := string(readSource("docs/old-copy.md"))
	if !strings.Contains(tombstone, "status: superseded") || !strings.Contains(tombstone, "superseded_by: \"new\"") || !strings.Contains(tombstone, "[[new]]") {
		t.Fatalf("tombstone is not a validated signpost: %s", tombstone)
	}
	if _, err := os.Stat(filepath.Join(repo, "docs/old-copy.md")); err != nil {
		t.Fatalf("tombstone deleted source: %v", err)
	}
	managedTombstone := string(readSource("docs/system/old-managed.md"))
	if !strings.Contains(managedTombstone, "status: superseded") || !strings.Contains(managedTombstone, "superseded_by: \"new-managed\"") {
		t.Fatalf("managed tombstone is not a signpost: %s", managedTombstone)
	}
	corpus, _, err := docgraph.LoadRepository(repo)
	if err != nil {
		t.Fatal(err)
	}
	find := docgraph.Find(corpus, "old-managed")
	if len(find.Matches) != 1 || find.Matches[0].Subject != "new-managed" || find.Matches[0].ResolvedFrom != "old-managed" {
		t.Fatalf("tombstone search did not resolve uniquely: %#v", find)
	}
	if got := string(readSource("README.md")); got != "# Keep me\n" {
		t.Fatalf("leave changed user guidance: %q", got)
	}
}

func TestDocsAdoptTableFingerprintRejectsDrift(t *testing.T) {
	repo := t.TempDir()
	writeTestDoc(t, repo, "docs/system/00-overview.md", "---\nsubject: overview\n---\n# Overview\n")
	writeTestDoc(t, repo, "docs/system/new.md", "---\nsubject: new\nstatus: canonical\npart_of: overview\n---\n# New\n")
	writeTestDoc(t, repo, "docs/old.md", "# Old\n\nOriginal.\n")
	proposal := docsAdoptProposal{Path: "docs/old.md", Subject: "Old", Disposition: "tombstone", Target: "docs/system/new.md", SourceFingerprint: docsAdoptBytesFingerprint([]byte("different"))}
	if _, err := preflightDocsAdoptTable(repo, []docsAdoptProposal{proposal}); err == nil || !strings.Contains(err.Error(), "source changed") {
		t.Fatalf("source drift was accepted: %v", err)
	}
}

func TestDocsAdoptApplyRejectsPostPreflightSourceDrift(t *testing.T) {
	repo := t.TempDir()
	writeTestDoc(t, repo, "docs/system/00-overview.md", "---\nsubject: overview\n---\n# Overview\n")
	writeTestDoc(t, repo, "docs/legacy.md", "# Legacy\n\nOriginal.\n")
	source, err := os.ReadFile(filepath.Join(repo, "docs/legacy.md"))
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := preflightDocsAdoptTable(repo, []docsAdoptProposal{{
		Path: "docs/legacy.md", Subject: "Legacy", Disposition: "promote", Target: "docs/system/legacy.md",
		Reason: "reviewed", SourceFingerprint: docsAdoptBytesFingerprint(source),
	}})
	if err != nil {
		t.Fatal(err)
	}
	writeTestDoc(t, repo, "docs/legacy.md", "# Legacy\n\nChanged after review.\n")
	if err := applyPreparedDocsAdoptTable(repo, prepared); err == nil || !strings.Contains(err.Error(), "source changed during approval") {
		t.Fatalf("post-preflight source drift accepted: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repo, "docs/system/legacy.md")); !os.IsNotExist(err) {
		t.Fatalf("drifted source created canonical target: %v", err)
	}
}

func TestDocsAdoptApplyRollsBackEarlierRowsOnFailure(t *testing.T) {
	repo := t.TempDir()
	writeTestDoc(t, repo, "docs/system/00-overview.md", "---\nsubject: overview\n---\n# Overview\n")
	writeTestDoc(t, repo, "docs/first.md", "# First\n")
	writeTestDoc(t, repo, "docs/second.md", "# Second\n")
	firstSource, err := os.ReadFile(filepath.Join(repo, "docs/first.md"))
	if err != nil {
		t.Fatal(err)
	}
	first, err := prepareDocsAdoptProposal(repo, docsAdoptProposal{
		Path: "docs/first.md", Subject: "First", Disposition: "promote", Target: "docs/system/first.md",
		SourceFingerprint: docsAdoptBytesFingerprint(firstSource),
	}, true)
	if err != nil {
		t.Fatal(err)
	}
	secondSource, err := os.ReadFile(filepath.Join(repo, "docs/second.md"))
	if err != nil {
		t.Fatal(err)
	}
	second := docsAdoptPrepared{proposal: docsAdoptProposal{
		Path: "docs/second.md", Subject: "Second", Disposition: "invalid", Target: "docs/system/second.md",
	}, source: secondSource}
	if err := applyPreparedDocsAdoptTable(repo, []docsAdoptPrepared{first, second}); err == nil {
		t.Fatal("invalid later row unexpectedly applied")
	}
	if _, err := os.Stat(filepath.Join(repo, "docs/system/first.md")); !os.IsNotExist(err) {
		t.Fatalf("earlier row was not rolled back: %v", err)
	}
}

func TestDocsAdoptCmdRequiresReviewedTableForMutation(t *testing.T) {
	repo := t.TempDir()
	vault := filepath.Join(repo, ".tusker")
	if err := os.MkdirAll(vault, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestDoc(t, repo, "docs/system/00-overview.md", "---\nsubject: overview\n---\n# Overview\n")
	writeTestDoc(t, repo, "docs/legacy.md", "# Legacy\n")
	if err := docsAdoptCmd(Args{"vault": vault, "approve": "true", "by": "human:test"}); err == nil || !strings.Contains(err.Error(), "explicit reviewed proposal table") {
		t.Fatalf("approve without table was accepted: %v", err)
	}
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
