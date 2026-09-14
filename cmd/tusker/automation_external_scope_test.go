package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestExternalCollectDoesNotReusePriorJobReviewArtifacts(t *testing.T) {
	repo := t.TempDir()
	oldSource := writeExternalFetchFiles(t, map[string]string{
		"fix.patch": "old patch\n",
		"review.md": externalScopeReviewPacket(t, "pass"),
	})
	newSource := writeExternalFetchFiles(t, map[string]string{
		"fix.patch": "corrected patch\n",
		"review.md": externalScopeReviewPacket(t, "blocked"),
	})

	oldDir := filepath.Join(repo, "architect", "APP-T-0001", "jobs", externalArtifactScope("job-old"))
	newDir := filepath.Join(repo, "architect", "APP-T-0001", "jobs", externalArtifactScope("job-new"))
	if err := ensureDir(oldDir); err != nil {
		t.Fatal(err)
	}
	if err := ensureDir(newDir); err != nil {
		t.Fatal(err)
	}
	oldPaths, err := normalizeExternalArtifacts(repo, oldDir, externalFetchResult{ArtifactDir: oldSource, Files: []string{"fix.patch", "review.md"}})
	if err != nil {
		t.Fatal(err)
	}
	newPaths, err := normalizeExternalArtifacts(repo, newDir, externalFetchResult{ArtifactDir: newSource, Files: []string{"fix.patch", "review.md"}})
	if err != nil {
		t.Fatal(err)
	}
	oldArtifacts, err := classifyExternalArtifacts(repo, oldPaths)
	if err != nil {
		t.Fatal(err)
	}
	newArtifacts, err := classifyExternalArtifacts(repo, newPaths)
	if err != nil {
		t.Fatal(err)
	}
	oldReview, err := externalReviewResultFromArtifacts(oldArtifacts)
	if err != nil || oldReview == nil || externalReviewVerdictClass(oldReview.Verdict) != "accepted" {
		t.Fatalf("old accepted review missing: result=%#v err=%v", oldReview, err)
	}
	newReview, err := externalReviewResultFromArtifacts(newArtifacts)
	if err != nil || newReview == nil || externalReviewVerdictClass(newReview.Verdict) != "blocked" {
		t.Fatalf("current review was contaminated by old verdict: result=%#v err=%v", newReview, err)
	}
	for _, artifact := range newArtifacts {
		if strings.Contains(artifact.Path, externalArtifactScope("job-old")) {
			t.Fatalf("current job imported prior artifact: %#v", artifact)
		}
	}
}

func TestExternalApplyInputsSelectLatestAdmittedEvent(t *testing.T) {
	vault := automationTestVault(t)
	project := registerAutomationTestProject(t, vault)
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	oldPath := filepath.Join(project.RepoRoot, "architect", "APP-T-0001", "jobs", externalArtifactScope("job-old"), "fix.patch")
	newPath := filepath.Join(project.RepoRoot, "architect", "APP-T-0001", "jobs", externalArtifactScope("job-new"), "fix.patch")
	if err := ensureDir(filepath.Dir(oldPath)); err != nil {
		t.Fatal(err)
	}
	if err := ensureDir(filepath.Dir(newPath)); err != nil {
		t.Fatal(err)
	}
	if err := writeText(oldPath, "old patch\n"); err != nil {
		t.Fatal(err)
	}
	if err := writeText(newPath, "corrected patch\n"); err != nil {
		t.Fatal(err)
	}
	oldHash, err := sha256Path(oldPath)
	if err != nil {
		t.Fatal(err)
	}
	newHash, err := sha256Path(newPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpsertApplyInput(RuntimeApplyInput{
		ProjectID: project.ProjectID, RecordID: "APP-T-0001", JobID: "job-old", EventID: "event-old", WorkRevision: 2,
		Path: oldPath, RelPath: filepath.ToSlash(filepath.Join("architect", "APP-T-0001", "jobs", externalArtifactScope("job-old"), "fix.patch")), Sha256: oldHash,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpsertApplyInput(RuntimeApplyInput{
		ProjectID: project.ProjectID, RecordID: "APP-T-0001", JobID: "job-new", EventID: "event-new", WorkRevision: 2,
		Path: newPath, RelPath: filepath.ToSlash(filepath.Join("architect", "APP-T-0001", "jobs", externalArtifactScope("job-new"), "fix.patch")), Sha256: newHash,
	}); err != nil {
		t.Fatal(err)
	}
	for _, event := range []ExternalLoopEvent{
		{EventID: "event-old", ProjectID: project.ProjectID, RecordID: "APP-T-0001", JobID: "job-old", Stage: externalLoopStageCollected, Action: externalLoopActionApplyPatch, Status: "ok", PayloadJSON: `{"work_revision":2}`, CreatedAt: "2026-09-14T00:00:00Z"},
		{EventID: "event-new", ProjectID: project.ProjectID, RecordID: "APP-T-0001", JobID: "job-new", Stage: externalLoopStageCollected, Action: externalLoopActionApplyPatch, Status: "ok", PayloadJSON: `{"work_revision":2}`, CreatedAt: "2026-09-14T00:01:00Z"},
	} {
		event.IdempotencyKey = externalLoopIdempotencyKey(event)
		if _, _, err := store.SaveExternalLoopEvent(event); err != nil {
			t.Fatal(err)
		}
	}
	inputs, err := listCurrentExternalApplyInputs(store, project.ProjectID, "APP-T-0001", RunStatus{WorkRevision: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(inputs) != 1 || inputs[0].JobID != "job-new" {
		t.Fatalf("latest admitted input was not selected: %#v", inputs)
	}
	wf := Workflow{Runners: map[string]RunnerDefinition{"chatgpt-browser": {ExternalCollect: true}}}
	if blockers := externalLoopApplyResultBlockers(wf, "chatgpt-browser", inputs); len(blockers) != 0 {
		t.Fatalf("corrected input remained ambiguous: %#v", blockers)
	}
	workspace := filepath.Join(t.TempDir(), "workspace")
	if err := mirrorApplyInputsIntoWorkspace(store, project, RunStatus{RecordID: "APP-T-0001", WorkRevision: 2}, workspace); err != nil {
		t.Fatal(err)
	}
	if !fileExists(filepath.Join(workspace, filepath.FromSlash(inputs[0].RelPath))) {
		t.Fatalf("current input was not mirrored: %#v", inputs[0])
	}
	if fileExists(filepath.Join(workspace, filepath.FromSlash("architect/APP-T-0001/jobs/"+externalArtifactScope("job-old")+"/fix.patch"))) {
		t.Fatal("old accepted/rejected cycle patch was mirrored into the current workspace")
	}
}

func TestExternalApplyInputsPreferLiveUnboundJobOverPriorAdmittedEvent(t *testing.T) {
	vault := automationTestVault(t)
	project := registerAutomationTestProject(t, vault)
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.UpsertApplyInput(RuntimeApplyInput{
		ProjectID: project.ProjectID, RecordID: "APP-T-0001", JobID: "job-old", EventID: "event-old", WorkRevision: 2,
		Path: "old.patch", RelPath: "old.patch", Sha256: "old-hash", Kind: "patch",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpsertApplyInput(RuntimeApplyInput{
		ProjectID: project.ProjectID, RecordID: "APP-T-0001", JobID: "job-new", WorkRevision: 2,
		Path: "new.patch", RelPath: "new.patch", Sha256: "new-hash", Kind: "patch",
	}); err != nil {
		t.Fatal(err)
	}
	event := ExternalLoopEvent{
		EventID: "event-old", ProjectID: project.ProjectID, RecordID: "APP-T-0001", JobID: "job-old",
		Stage: externalLoopStageCollected, Action: externalLoopActionApplyPatch, Status: "ok",
		PayloadJSON: `{"work_revision":2}`, CreatedAt: "2026-09-14T00:00:00Z",
	}
	event.IdempotencyKey = externalLoopIdempotencyKey(event)
	if _, _, err := store.SaveExternalLoopEvent(event); err != nil {
		t.Fatal(err)
	}
	inputs, err := listCurrentExternalApplyInputs(store, project.ProjectID, "APP-T-0001", RunStatus{WorkRevision: 2, CloudTaskID: "job-new"})
	if err != nil {
		t.Fatal(err)
	}
	if len(inputs) != 1 || inputs[0].JobID != "job-new" || inputs[0].EventID != "" {
		t.Fatalf("live unbound provider job did not outrank prior admitted event: %#v", inputs)
	}
}

func TestExternalApplyInputsDoNotReviveUnboundRevisionZeroWithoutCurrentJob(t *testing.T) {
	vault := automationTestVault(t)
	project := registerAutomationTestProject(t, vault)
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	path := filepath.Join(project.RepoRoot, "architect", "APP-T-0001", "jobs", externalArtifactScope("stale"), "fix.patch")
	if err := ensureDir(filepath.Dir(path)); err != nil {
		t.Fatal(err)
	}
	if err := writeText(path, "stale patch\n"); err != nil {
		t.Fatal(err)
	}
	hash, err := sha256Path(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpsertApplyInput(RuntimeApplyInput{
		ProjectID: project.ProjectID, RecordID: "APP-T-0001", JobID: "stale-job", WorkRevision: 0,
		Path: path, RelPath: filepath.ToSlash(filepath.Join("architect", "APP-T-0001", "jobs", externalArtifactScope("stale"), "fix.patch")), Sha256: hash,
	}); err != nil {
		t.Fatal(err)
	}
	inputs, err := listCurrentExternalApplyInputs(store, project.ProjectID, "APP-T-0001", RunStatus{WorkRevision: 0})
	if err != nil {
		t.Fatal(err)
	}
	if len(inputs) != 0 {
		t.Fatalf("unbound revision-zero input was revived without a current job: %#v", inputs)
	}
	inputs, err = store.ListApplyInputsForRunScope(project.ProjectID, "APP-T-0001", 0, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(inputs) != 0 {
		t.Fatalf("unbound revision-zero input was exposed by an unscoped store query: %#v", inputs)
	}
	inputs, err = store.ListApplyInputsForRun(project.ProjectID, "APP-T-0001")
	if err != nil {
		t.Fatal(err)
	}
	if len(inputs) != 0 {
		t.Fatalf("legacy unbound input was exposed without an admitted event: %#v", inputs)
	}
	inputs, err = listCurrentExternalApplyInputs(store, project.ProjectID, "APP-T-0001", RunStatus{WorkRevision: 0, CloudTaskID: "stale-job"})
	if err != nil {
		t.Fatal(err)
	}
	if len(inputs) != 1 || inputs[0].JobID != "stale-job" {
		t.Fatalf("current provider job should recover the crash-window input: %#v", inputs)
	}
}

func externalScopeReviewPacket(t *testing.T, verdict string) string {
	t.Helper()
	result := ReviewResult{
		Schema: reviewResultSchema, ProjectID: "project", TaskID: "APP-T-0001", TaskStateRev: "state",
		WorkRevision: 1, ImplementationSHA: "implementation", AttemptID: "review", Actor: "reviewer:agent",
		Runner: "codex_exec", RunnerProfile: "review", WorkerPolicyFP: "sha256:" + strings.Repeat("a", 64),
		ProofFingerprint: "sha256:" + strings.Repeat("c", 64), GateFingerprint: "sha256:" + strings.Repeat("d", 64),
		MaterialFingerprint: "sha256:" + strings.Repeat("b", 64), Covers: []string{}, Verdict: verdict, Summary: "current verdict", CreatedAt: "2026-09-14T00:00:00Z",
	}
	if verdict == "pass" {
		result.Covers = []string{"A1"}
	} else {
		result.Blocker = "machine"
	}
	result.ResultRevision = reviewResultFingerprint(result)
	raw, err := json.Marshal(struct {
		Kind string `json:"kind"`
		ReviewResult
	}{Kind: "review", ReviewResult: result})
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}
