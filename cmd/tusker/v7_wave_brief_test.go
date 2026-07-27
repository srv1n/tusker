package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestWaveRuntimeRunsDoesNotCreateOrMutateRuntimeStore(t *testing.T) {
	stateRoot := filepath.Join(t.TempDir(), "runtime")
	t.Setenv("TUSKER_STATE_ROOT", stateRoot)

	if runs := v7WaveRuntimeRuns(t.TempDir(), ""); len(runs) != 0 {
		t.Fatalf("missing runtime store unexpectedly returned runs: %#v", runs)
	}
	if fileExists(stateRoot) {
		t.Fatalf("wave read path created runtime state at %s", stateRoot)
	}
}

func TestWaveBriefArtifactContract(t *testing.T) {
	kinds := []string{"screenshot", "video", "benchmark_delta", "trace", "replay", "behavior_matrix", "reliability_summary", "security_note", "diff_summary", "knowledge_link"}
	for _, kind := range kinds {
		t.Run(kind, func(t *testing.T) {
			note := briefTask("APP-T-0001", "ready", "pending")
			note.Data["artifact_contract"] = map[string]any{"kind": kind, "path": "internal/product/result", "summary": "Compact operator proof.", "acceptance_ids": []string{"A1"}}
			var issues []Issue
			validateV7ArtifactContract(note, "task.md", &issues)
			if len(issues) != 0 {
				t.Fatalf("valid %s contract rejected: %#v", kind, issues)
			}
		})
	}
	bad := briefTask("APP-T-0001", "ready", "pending")
	bad.Data["artifact_contract"] = map[string]any{"kind": "terminal_dump", "path": ".tusker/scratch/raw.log", "summary": "", "acceptance_ids": []string{"A9"}}
	var issues []Issue
	validateV7ArtifactContract(bad, "task.md", &issues)
	if len(issues) != 4 {
		t.Fatalf("expected kind/summary/path/acceptance findings, got %#v", issues)
	}
	missing := briefTask("APP-T-0001", "ready", "pending")
	missing.Data["artifact_contract"] = map[string]any{"kind": "diff_summary", "path": "cmd/tusker", "summary": "Focused change summary."}
	issues = nil
	validateV7ArtifactContract(missing, "task.md", &issues)
	if len(issues) != 1 || issues[0].Code != "TASK_ARTIFACT_ACCEPTANCE_MISSING" {
		t.Fatalf("missing acceptance mapping was not rejected: %#v", issues)
	}
}

func TestWaveBriefEmptySectionsEncodeAsArrays(t *testing.T) {
	idx, wave := briefFixture()
	delete(idx.Evidence, "APP-T-0001")
	delete(wave.Data, "landings")
	brief := buildWaveBrief(idx, wave)
	raw, err := json.Marshal(brief)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"seeIt":[]`, `"landed":[]`, `"reworkParked":[]`, `"humanAction":[]`, `"documentation":[]`} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("empty section is not a JSON array (%s): %s", want, raw)
		}
	}
}

func TestWaveBriefGolden(t *testing.T) {
	idx, wave := briefFixture()
	brief := buildWaveBrief(idx, wave)
	wantOrder := []string{"outcome", "seeIt", "landed", "reworkParked", "humanAction", "documentation"}
	assertEqual(t, wantOrder, brief.SectionOrder, "stable JSON section order")
	text := renderWaveBrief(brief)
	last := -1
	for _, heading := range []string{"## Outcome", "## See it", "## Landed", "## Rework/parked", "## Human action", "## Documentation"} {
		at := strings.Index(text, heading)
		if at <= last {
			t.Fatalf("brief section %q missing or out of order:\n%s", heading, text)
		}
		last = at
	}
	for _, forbidden := range []string{"token count", "transcript", "raw log"} {
		if strings.Contains(strings.ToLower(text), forbidden) {
			t.Fatalf("brief leaked %q", forbidden)
		}
	}
	if len(brief.SeeIt) != 1 || brief.SeeIt[0].Kind != "screenshot" || brief.SeeIt[0].AcceptanceIDs[0] != "A1" || brief.SeeIt[0].EvidenceRef != "APP-T-0001-E-0001" {
		t.Fatalf("artifact normalization mismatch: %#v", brief.SeeIt)
	}
}

func TestWaveBriefHonesty(t *testing.T) {
	idx, wave := briefFixture()
	untouched := briefTask("APP-T-0000", "ready", "pending")
	untouched.Body += "| A1 | command: go test ./... | pending | Contract only. |\n"
	if state := waveBriefState(idx, untouched, false); state.Implementation != "absent" {
		t.Fatalf("pending contract row was presented as implementation: %#v", state)
	}
	partial := briefTask("APP-T-0002", "review", "satisfied")
	partial.Data["knowledge_nodes"] = []string{"knowledge/domains/project/CANON.md"}
	idx.Tasks["APP-T-0002"] = partial
	wave.Data["members"] = []string{"APP-T-0001", "APP-T-0002"}
	brief := buildWaveBrief(idx, wave)
	state := brief.Outcome.Tasks[1]
	if state.Implementation != "present" || state.Proof != "satisfied" || state.Review != "pending" || state.Landing != "not_landed" || state.Documentation != "pending" {
		t.Fatalf("partial state collapsed into completion: %#v", state)
	}
	if brief.Outcome.FullyDrained {
		t.Fatal("code/proof without review, landing, and docs must not fully drain")
	}

	rework := briefTask("APP-T-0003", "rework", "partial")
	rework.Data["next_action"] = "Fix the failing API matrix assertion."
	idx.Tasks["APP-T-0003"] = rework
	wave.Data["members"] = append(normalizeList(wave.Data["members"]), "APP-T-0003")
	brief = buildWaveBrief(idx, wave)
	if len(brief.Rework) != 1 || brief.Rework[0].Failure != "Fix the failing API matrix assertion." {
		t.Fatalf("missing first actionable failure: %#v", brief.Rework)
	}
}

func TestWaveBriefDoesNotDrainBeforeContractedArtifactIsVisible(t *testing.T) {
	idx, wave := briefFixture()
	delete(idx.Evidence, "APP-T-0001")
	brief := buildWaveBrief(idx, wave)
	if brief.Outcome.FullyDrained {
		t.Fatal("a wave with a missing contracted artifact must not report fully drained")
	}
	if len(brief.SeeIt) != 0 {
		t.Fatalf("missing evidence unexpectedly produced artifacts: %#v", brief.SeeIt)
	}
}

func TestWaveBriefSurfacesRuntimeParkedFailure(t *testing.T) {
	idx, wave := briefFixture()
	runs := map[string]RunStatus{"APP-T-0001": {RecordID: "APP-T-0001", LeaseState: string(LeaseStateParkedNoProgress), LastError: "runner exited with code 19"}}
	brief := buildWaveBriefWithRuns(idx, wave, runs)
	if len(brief.Rework) != 1 || brief.Rework[0].State != string(LeaseStateParkedNoProgress) || !strings.Contains(brief.Rework[0].Failure, "code 19") {
		t.Fatalf("runtime parked failure missing from brief: %#v", brief.Rework)
	}
	if brief.Outcome.FullyDrained {
		t.Fatal("runtime parked work must prevent a fully-drained outcome")
	}
}

func TestWaveBriefHumanAction(t *testing.T) {
	idx, wave := briefFixture()
	valid := briefGate("APP-G-0001", "auth", "Provision the staging OAuth secret.", "A successful authenticated probe resumes the task.", "The credential belongs to the human account owner.")
	invalid := briefGate("APP-G-0002", "verification", "Run tests and inspect the logs.", "Tests pass.", "Risk is high.")
	nonblocking := briefGate("APP-G-0003", "auth", "Provision an optional OAuth secret.", "The optional probe succeeds.", "The credential belongs to the human account owner.")
	nonblocking.Data["blocking"] = false
	idx.Gates["APP-G-0001"] = valid
	idx.Gates["APP-G-0002"] = invalid
	idx.Gates["APP-G-0003"] = nonblocking
	brief := buildWaveBrief(idx, wave)
	if len(brief.HumanAction) != 1 {
		t.Fatalf("ordinary objective work leaked into human action: %#v", brief.HumanAction)
	}
	if len(brief.Rework) != 1 || !strings.Contains(brief.Rework[0].Failure, "APP-G-0002") {
		t.Fatalf("invalid human gate did not return to rework: %#v", brief.Rework)
	}
	action := brief.HumanAction[0]
	if action.GateID != "APP-G-0001" || action.ResumeID != "APP-G-0001" || action.Action != "Provision the staging OAuth secret." || action.GateHref != "/p/app/docs?path=APP-T-0001&gate=APP-G-0001" {
		t.Fatalf("human action is not exact/resumable: %#v", action)
	}
}

func TestWaveBriefHumanActionUsesCanonicalBodyAwareGateValidation(t *testing.T) {
	idx, wave := briefFixture()
	bodyBacked := briefGate("APP-G-0001", "auth", "Provision the staging OAuth secret.", "A successful authenticated probe resumes the task.", "")
	bodyBacked.Data["schema"] = "tusker.gate/v1"
	delete(bodyBacked.Data, "why_agent_cannot")
	bodyBacked.Body = "## Why agent cannot do this\n\nThe credential belongs to the human account owner.\n\n## Secret policy\n\nDo not store the secret in the repository.\n"
	bodyBacked.RelativePath = "work/gates/APP-G-0001.md"
	var validationErrors, validationWarnings []Issue
	validateV7Gate(bodyBacked, bodyBacked.RelativePath, &validationErrors, &validationWarnings)
	if len(validationErrors) != 0 {
		t.Fatalf("body-backed human gate is not schema-valid: %#v", validationErrors)
	}

	unknown := briefGate("APP-G-0002", "unknown_authority", "Provide an operator response.", "The response is recorded.", "Only the account owner can respond.")
	idx.Gates["APP-G-0001"] = bodyBacked
	idx.Gates["APP-G-0002"] = unknown
	brief := buildWaveBrief(idx, wave)
	if len(brief.HumanAction) != 1 || brief.HumanAction[0].GateID != "APP-G-0001" {
		t.Fatalf("schema-valid body-backed gate was not isolated to Human action: %#v", brief.HumanAction)
	}
	if len(brief.Rework) != 1 || !strings.Contains(brief.Rework[0].Failure, "APP-G-0002") {
		t.Fatalf("unknown gate kind did not route only to rework: %#v", brief.Rework)
	}
}

func TestWaveBriefArtifactsRequireAcceptedDurableMappedEvidence(t *testing.T) {
	idx, wave := briefFixture()
	task := idx.Tasks["APP-T-0001"]
	pending := idx.Evidence["APP-T-0001"][0]
	pending.Data = cloneMap(pending.Data)
	pending.Data["id"] = "APP-T-0001-E-0002"
	pending.Data["status"] = "pending_review"
	unknown := idx.Evidence["APP-T-0001"][0]
	unknown.Data = cloneMap(unknown.Data)
	unknown.Data["id"] = "APP-T-0001-E-0003"
	unknown.Data["covers"] = []string{"A9"}
	idx.Evidence["APP-T-0001"] = append(idx.Evidence["APP-T-0001"], pending, unknown)
	brief := buildWaveBrief(idx, wave)
	if len(brief.SeeIt) != 1 || brief.SeeIt[0].EvidenceRef != "APP-T-0001-E-0001" {
		t.Fatalf("unaccepted or unmapped evidence leaked into See it: %#v (task %#v)", brief.SeeIt, task.Data)
	}
}

func briefFixture() (v7Index, Note) {
	task := briefTask("APP-T-0001", "done", "satisfied")
	task.Data["artifact_contract"] = map[string]any{"kind": "screenshot_set", "path": "internal/serve/ui", "summary": "Final interaction state.", "acceptance_ids": []string{"A1"}}
	evidence := Note{Data: map[string]any{"schema": "tusker.evidence/v1", "kind": "evidence", "id": "APP-T-0001-E-0001", "task": "APP-T-0001", "status": "accepted", "evidence_kind": "screenshot", "covers": []string{"A1"}, "artifact_paths": []string{"evidence/APP-T-0001/artifacts/result.png"}, "screenshot_checked_by": "reviewer:test", "screenshot_checked_at": "2026-07-14T00:00:00Z"}, Body: "## Summary\n\nFinal interaction state."}
	wave := Note{Data: map[string]any{"schema": "tusker.wave/v7", "kind": "wave", "id": "W-0001", "project": "app", "title": "Morning delivery", "members": []string{"APP-T-0001"}, "landings": []any{map[string]any{"task": "APP-T-0001", "gate_result": "pass", "commit": "abc123", "target": "integration/W-0001"}}}}
	idx := v7Index{Tasks: map[string]Note{"APP-T-0001": task}, Gates: map[string]Note{}, Waves: map[string]Note{"W-0001": wave}, Evidence: map[string][]Note{"APP-T-0001": {evidence}}, Attempts: map[string][]Note{}}
	return idx, wave
}

func briefTask(id, status, proof string) Note {
	return Note{Data: map[string]any{"schema": "tusker.task/v7", "kind": "task", "id": id, "project": "app", "title": "Task " + id, "status": status, "readiness": "ready", "proof_status": proof, "next_action": ""}, Body: "## Acceptance\n\n| ID | Outcome | Proof |\n|---|---|---|\n| A1 | Observable result. | Focused proof. |\n\n## Verification\n\n| Covers | Check | Result | Notes |\n|---|---|---|---|\n"}
}

func briefGate(id, kind, action, verification, why string) Note {
	return Note{Data: map[string]any{"schema": "tusker.gate/v7", "kind": "gate", "id": id, "project": "app", "title": "Gate " + id, "gate_kind": kind, "status": "open", "owner": "human:sarav", "blocking": true, "blocks": []string{"APP-T-0001"}, "action": action, "verification": verification, "why_agent_cannot": why}}
}
