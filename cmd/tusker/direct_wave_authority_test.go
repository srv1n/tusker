package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func authorityFixture(t *testing.T) (vault string, store *RuntimeStore, project RegisteredProject) {
	t.Helper()
	vault = v7DirectTestVault(t)
	if _, err := setProjectLocalConfigWithReadback(vault, "automation.profiles.test-codex-exec", directEmergencyRunnerProfileForTest()); err != nil {
		t.Fatal(err)
	}
	for _, lane := range []string{"execute", "review"} {
		if _, err := setProjectLocalConfigWithReadback(vault, "automation.model_levels.standard."+lane, []string{"test-codex-exec"}); err != nil {
			t.Fatal(err)
		}
	}
	stateRoot := filepath.Join(t.TempDir(), "state")
	t.Setenv("TUSKER_STATE_ROOT", stateRoot)
	var err error
	store, err = OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	project = newRegisteredProject(v7RepoRoot(vault), vault)
	if _, _, err := store.RegisterProject(project); err != nil {
		t.Fatal(err)
	}
	return vault, store, project
}

func directTaskBody(id, intent string) string {
	return "# " + id + "\n\n## Intent\n\n" + intent + "\n\n## Acceptance\n\n| ID | Outcome | Proof |\n| --- | --- | --- |\n| A1 | The named behavior is observable in the changed files. | Verification A1 |\n\n## Verification\n\n| Covers | Check | Result | Notes |\n| --- | --- | --- | --- |\n| A1 | command: go test ./x | pending | |\n"
}

// directDispatchableTaskBody carries observable acceptance with proof mapping
// so content-level dispatch blockers stay silent: tests exercising admission
// or live-attempt continuity can then isolate authorization behavior.
func directDispatchableTaskBody(id string) string {
	return "# " + id + "\n\n## Intent\n\nDo " + id + ".\n\n## Acceptance\n\n| ID | Outcome | Proof |\n| --- | --- | --- |\n| A1 | The named behavior is observable in the changed files. | focused test output |\n\n## Verification\n\n| Covers | Check | Result | Notes |\n| --- | --- | --- | --- |\n| A1 | command: go test ./cmd/tusker -run TestDirect -count=1 | pending | |\n"
}

func writeDirectTask(t *testing.T, vault, id, waveID string, extra map[string]any) string {
	t.Helper()
	data := map[string]any{
		"schema": "tusker.task/v7", "kind": "task", "id": id, "project": v7ProjectID(vault),
		"title": "Direct " + id, "status": "backlog", "readiness": "held",
		"proof_mode": "inline", "proof_status": "pending", "proof_required": []any{"focused_test"},
		"work_level": "standard", "next_owner": "agent",
		"created_at": "2026-01-01T00:00:00Z", "created_by": "agent:test", "updated_at": "2026-01-01T00:00:00Z", "updated_by": "agent:test",
	}
	for k, v := range extra {
		data[k] = v
	}
	if waveID != "" {
		data["wave"] = waveID
	}
	body := directTaskBody(id, "Do "+id+".")
	data["contract_fingerprint"] = directWaveTaskContractFingerprint(data, body)
	data["state_rev"] = v7StateRev(data, body)
	content, err := serializeDocument(data, body, v7FrontmatterOrder["task"])
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(vault, "work", "tasks", id+".md")
	if err := writeText(path, content); err != nil {
		t.Fatal(err)
	}
	if err := v7TestVerificationMutation(Args{
		"vault": vault, "quiet": "true", "id": id, "by": "reviewer:test",
		"covers": "A1", "check": "command: go test ./x", "result": "pass", "note": "Current fixture gate receipt.",
	}); err != nil {
		t.Fatal(err)
	}
	return path
}

func writePendingDirectTask(t *testing.T, vault, id, waveID string, extra map[string]any) {
	t.Helper()
	path := writeDirectTask(t, vault, id, waveID, extra)
	data, body, err := parseFrontmatterMustRead(path)
	if err != nil {
		t.Fatal(err)
	}
	body = directDispatchableTaskBody(id)
	rows := parseV7VerificationRows(body)
	rows[0].Check, rows[0].Result, rows[0].Notes = "command: go test ./... -run '^TestProofExists$' -count=1", "pending", ""
	body = replaceSection(body, "## Verification", renderV7VerificationTable(rows))
	data["proof_status"] = "pending"
	data["contract_fingerprint"] = directWaveTaskContractFingerprint(data, body)
	data["state_rev"] = v7StateRev(data, body)
	content, err := serializeDocument(data, body, v7FrontmatterOrder["task"])
	if err != nil {
		t.Fatal(err)
	}
	if err := writeText(path, content); err != nil {
		t.Fatal(err)
	}
}

func writeDirectWave(t *testing.T, vault, id string, members []string, extra map[string]any) {
	t.Helper()
	data := map[string]any{
		"schema": "tusker.wave/v7", "kind": "wave", "id": id, "project": v7ProjectID(vault),
		"title": "Direct wave " + id, "summary": "Ship " + id, "status": "open", "authorization": "disarmed",
		"members": members, "integration_branch": "wave/" + id,
		"created_at": "2026-01-01T00:00:00Z", "created_by": "agent:test", "updated_at": "2026-01-01T00:00:00Z", "updated_by": "agent:test",
	}
	for k, v := range extra {
		data[k] = v
	}
	body := "# " + id + "\n\n## Intended result\n\nShip it.\n"
	data["state_rev"] = v7StateRev(data, body)
	content, err := serializeDocument(data, body, v7FrontmatterOrder["wave"])
	if err != nil {
		t.Fatal(err)
	}
	if err := writeText(filepath.Join(vault, "work", "waves", id+".md"), content); err != nil {
		t.Fatal(err)
	}
}

func writeHumanGate(t *testing.T, vault, id, taskID string) {
	t.Helper()
	data := map[string]any{
		"schema": "tusker.gate/v1", "kind": "gate", "id": id, "project": v7ProjectID(vault),
		"title": "Gate " + id, "gate_kind": "manual_hold", "status": "open", "owner": "human:sarav",
		"blocking": true, "blocks": []any{taskID},
		"action": "Approve the release.", "verification": "Human confirmed approval.", "why_agent_cannot": "Human authority is required for release signoff.",
		"created_at": "2026-01-01T00:00:00Z", "created_by": "agent:test", "updated_at": "2026-01-01T00:00:00Z", "updated_by": "agent:test",
	}
	body := "# " + id + "\n\nGate body.\n"
	data["state_rev"] = v7StateRev(data, body)
	content, err := serializeDocument(data, body, v7FrontmatterOrder["gate"])
	if err != nil {
		t.Fatal(err)
	}
	if err := writeText(filepath.Join(vault, "work", "gates", id+".md"), content); err != nil {
		t.Fatal(err)
	}
}

func rewriteTaskFile(t *testing.T, vault, id string, mutate func(data map[string]any, body string) (map[string]any, string)) {
	t.Helper()
	path := filepath.Join(vault, "work", "tasks", id+".md")
	data, body, err := parseFrontmatterMustRead(path)
	if err != nil {
		t.Fatal(err)
	}
	data, body = mutate(data, body)
	data["contract_fingerprint"] = directWaveTaskContractFingerprint(data, body)
	data["state_rev"] = v7StateRev(data, body)
	content, err := serializeDocument(data, body, v7FrontmatterOrder["task"])
	if err != nil {
		t.Fatal(err)
	}
	if err := writeText(path, content); err != nil {
		t.Fatal(err)
	}
}

func waveAuthorizationState(t *testing.T, vault, waveID string) map[string]any {
	t.Helper()
	idx, err := loadV7Index(vault)
	if err != nil {
		t.Fatal(err)
	}
	return waveAuthorizationProjection(vault, idx, idx.Waves[waveID])
}

func TestDirectRunQueuesTask(t *testing.T) {
	vault, store, project := authorityFixture(t)
	writePendingDirectTask(t, vault, "APP-T-0001", "", map[string]any{"readiness": "ready"})

	if err := directRunCmd(Args{"vault": vault, "_pos0": "APP-T-0001", "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	directive, err := store.RunDirective(project.ProjectID, "APP-T-0001")
	if err != nil {
		t.Fatal(err)
	}
	if directive == nil || directive.State != "queued" || !strings.HasPrefix(directive.Actor, "operator:") {
		t.Fatalf("directive=%#v", directive)
	}
}

func TestDirectRunReopensTerminalRuntimeRow(t *testing.T) {
	vault, store, project := authorityFixture(t)
	writePendingDirectTask(t, vault, "APP-T-0001", "", map[string]any{"readiness": "ready"})
	if err := directRunCmd(Args{"vault": vault, "_pos0": "APP-T-0001", "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	note, err := resolveNote(vault, "APP-T-0001")
	if err != nil {
		t.Fatal(err)
	}
	run := RunStatus{ProjectID: project.ProjectID, RecordID: "APP-T-0001", Runner: string(RunnerCodexExec), Lane: runLaneExecute, LeaseState: string(LeaseStateReleased), AttemptOutcome: string(AttemptOutcomeAbandoned), AttemptCount: 3, Terminal: true, LastError: "old failure"}
	reopened, changed, err := reopenTerminalRunForDirective(vault, store, note, run, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if !changed || reopened.Terminal || reopened.LeaseState != string(LeaseStateUnclaimed) || reopened.AttemptOutcome != string(AttemptOutcomeNone) || reopened.AttemptCount != 0 || reopened.LastError != "" {
		t.Fatalf("reopened=%#v changed=%t", reopened, changed)
	}
}

func TestDirectWaveRetryRedrivesFailedMembers(t *testing.T) {
	vault, store, project := authorityFixture(t)
	writeDirectTask(t, vault, "APP-T-0001", "W-0001", nil)
	writeDirectWave(t, vault, "W-0001", []string{"APP-T-0001"}, nil)
	if _, err := directWaveStart(vault, store, "W-0001", "human:test"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.exec(`UPDATE run_directives SET state='consumed' WHERE project_id=? AND record_id=?`, project.ProjectID, "APP-T-0001"); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertRun(RunStatus{ProjectID: project.ProjectID, RecordID: "APP-T-0001", ItemID: "APP-T-0001", Lane: runLaneExecute, LeaseState: string(LeaseStateParkedNoProgress), AttemptOutcome: string(AttemptOutcomeBlocked), AttemptCount: 3, Terminal: true, LastError: "old workspace refusal"}); err != nil {
		t.Fatal(err)
	}

	result, err := directWaveStart(vault, store, "W-0001", "human:test")
	if err != nil {
		t.Fatal(err)
	}
	run, err := store.FindRunScoped(project.ProjectID, "APP-T-0001")
	if err != nil || run == nil {
		t.Fatalf("run=%#v err=%v", run, err)
	}
	if !result.Replayed || !containsString(result.QueuedTaskIDs, "APP-T-0001") || run.Terminal || run.AttemptCount != 0 || run.LeaseState != string(LeaseStateRetryQueued) {
		t.Fatalf("result=%#v run=%#v", result, run)
	}
}

func TestDirectWaveAuthorityReviewUsesOnlyDurableMaterial(t *testing.T) {
	vault, store, project := authorityFixture(t)
	writeDirectTask(t, vault, "APP-T-0001", "W-0001", nil)
	writeDirectTask(t, vault, "APP-T-0002", "W-0001", map[string]any{"dependencies": []any{"APP-T-0001:hard"}})
	writeDirectWave(t, vault, "W-0001", []string{"APP-T-0001", "APP-T-0002"}, nil)
	review, err := buildDirectWaveReview(vault, store, project.ProjectID, "W-0001", nil)
	if err != nil {
		t.Fatal(err)
	}
	if review.State != "Planned" || review.Authorization != "inert" || review.MaterialFingerprint == "" {
		t.Fatalf("review=%#v", review)
	}
	if len(review.Members) != 2 || review.Members[0].State != "ready" || review.Members[1].State != "waiting" {
		t.Fatalf("members=%#v", review.Members)
	}
	if len(review.Frontiers) != 2 || review.Frontiers[0][0] != "APP-T-0001" || review.Frontiers[1][0] != "APP-T-0002" {
		t.Fatalf("frontiers=%#v", review.Frontiers)
	}
	if review.Members[0].ExecuteRoute == "" || review.Members[0].ReviewRoute == "" {
		t.Fatalf("routes not resolved: %#v", review.Members[0])
	}
	if len(review.Members[0].Acceptance) == 0 || len(review.Members[0].Verification) == 0 {
		t.Fatalf("acceptance/verification missing: %#v", review.Members[0])
	}
	raw, _ := json.Marshal(review)
	for _, forbidden := range []string{"delivery_", "source_key", "plan_fingerprint", "factory", "context_fingerprint"} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatalf("review DTO leaks %s: %s", forbidden, raw)
		}
	}
}

func TestDirectWaveStartRefusesInvalidMemberContractBeforeArm(t *testing.T) {
	vault, store, _ := authorityFixture(t)
	writeDirectTask(t, vault, "APP-T-0001", "W-0001", nil)
	writeDirectWave(t, vault, "W-0001", []string{"APP-T-0001"}, nil)
	rewriteTaskFile(t, vault, "APP-T-0001", func(data map[string]any, body string) (map[string]any, string) {
		return data, strings.Replace(body, "command: go test ./x", "check later", 1)
	})
	_, err := directWaveStart(vault, store, "W-0001", "human:test")
	if err == nil || !strings.Contains(err.Error(), "APP-T-0001: verification missing exact command or manual proof") {
		t.Fatalf("err=%v", err)
	}
	if auth := waveAuthorizationState(t, vault, "W-0001"); stringField(auth, "state") == "armed" {
		t.Fatalf("invalid member contract was armed: %#v", auth)
	}
}

func TestDirectWaveReviewPreflightsAllMemberContracts(t *testing.T) {
	vault, store, project := authorityFixture(t)
	writeDirectTask(t, vault, "APP-T-0001", "W-0001", nil)
	writeDirectTask(t, vault, "APP-T-0002", "W-0001", map[string]any{"dependencies": []any{"APP-T-0001:hard"}})
	writeDirectWave(t, vault, "W-0001", []string{"APP-T-0001", "APP-T-0002"}, nil)
	for _, id := range []string{"APP-T-0001", "APP-T-0002"} {
		rewriteTaskFile(t, vault, id, func(data map[string]any, body string) (map[string]any, string) {
			return data, strings.Replace(body, "| ID | Outcome | Proof |", "| ID | Outcome | Check |", 1)
		})
	}
	review, err := buildDirectWaveReview(vault, store, project.ProjectID, "W-0001", nil)
	if err != nil {
		t.Fatal(err)
	}
	blocked := map[string]bool{}
	for _, blocker := range review.Blockers {
		if blocker.Code == "MEMBER_CONTRACT_INVALID" && blocker.Reason == "acceptance missing proof mapping" {
			blocked[blocker.TaskID] = true
		}
	}
	if !blocked["APP-T-0001"] || !blocked["APP-T-0002"] {
		t.Fatalf("review omitted root or dependent contract blocker: %#v", review.Blockers)
	}
	for _, control := range review.Controls {
		if (control.Action == "wave start" || control.Action == "task start") && control.Enabled {
			t.Fatalf("invalid member has enabled start control: %#v", control)
		}
	}
	if _, err := directWaveStart(vault, store, "W-0001", "human:test"); err == nil || !strings.Contains(err.Error(), "acceptance missing proof mapping") {
		t.Fatalf("start disagrees with review: %v", err)
	}
	if auth := waveAuthorizationState(t, vault, "W-0001"); stringField(auth, "state") == "armed" {
		t.Fatalf("invalid wave was authorized: %#v", auth)
	}
	if err := waveReviewCmd(Args{"vault": vault, "id": "W-0001", "check": "true", "quiet": "true"}); err == nil || !strings.Contains(err.Error(), "acceptance missing proof mapping") {
		t.Fatalf("author preflight did not fail clearly: %v", err)
	}
	for _, id := range []string{"APP-T-0001", "APP-T-0002"} {
		rewriteTaskFile(t, vault, id, func(data map[string]any, body string) (map[string]any, string) {
			return data, strings.Replace(body, "| ID | Outcome | Check |", "| ID | Outcome | Proof |", 1)
		})
	}
	if err := waveReviewCmd(Args{"vault": vault, "id": "W-0001", "check": "true", "quiet": "true"}); err != nil {
		t.Fatalf("repaired author preflight failed: %v", err)
	}
}

func TestDirectWaveReviewSurfacesPersistentDispatchBlocker(t *testing.T) {
	vault, store, project := authorityFixture(t)
	writeDirectTask(t, vault, "APP-T-0001", "W-0001", nil)
	writeDirectWave(t, vault, "W-0001", []string{"APP-T-0001"}, map[string]any{"authorization": "armed"})
	if err := store.UpsertRun(RunStatus{ProjectID: project.ProjectID, RecordID: "APP-T-0001", ItemID: "APP-T-0001", Lane: runLaneExecute, LeaseState: string(LeaseStateUnclaimed), LastError: "dispatch blocked: acceptance missing proof mapping"}); err != nil {
		t.Fatal(err)
	}
	review, err := buildDirectWaveReview(vault, store, project.ProjectID, "W-0001", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(review.Members) != 1 || review.Members[0].State != "blocked" || review.Members[0].Phase != "blocked" || !strings.Contains(review.Members[0].WaitingReason, "acceptance missing proof mapping") {
		t.Fatalf("member=%#v", review.Members)
	}
	if len(review.Blockers) == 0 || review.Blockers[0].Code != "DISPATCH_BLOCKED" {
		t.Fatalf("blockers=%#v", review.Blockers)
	}
}

func TestDirectWaveReviewProjectsLegacyDeliveryUnknown(t *testing.T) {
	vault, store, project := authorityFixture(t)
	writeDirectTask(t, vault, "APP-T-0001", "W-0001", nil)
	writeDirectWave(t, vault, "W-0001", []string{"APP-T-0001"}, map[string]any{"authorization": "armed"})
	if err := store.UpsertRun(RunStatus{ProjectID: project.ProjectID, RecordID: "APP-T-0001", ItemID: "APP-T-0001", Lane: runLaneExecute, LeaseState: string(LeaseStateReleased), AttemptOutcome: string(AttemptOutcomeFailed), Terminal: true, LastError: `acp outcome delivery_unknown (write_complete): connection lost`}); err != nil {
		t.Fatal(err)
	}
	review, err := buildDirectWaveReview(vault, store, project.ProjectID, "W-0001", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(review.Members) != 1 || review.Members[0].Phase != "outcome_unknown" {
		t.Fatalf("member=%#v", review.Members)
	}
	if len(review.Blockers) == 0 || review.Blockers[0].Code != "OUTCOME_UNKNOWN" {
		t.Fatalf("blockers=%#v", review.Blockers)
	}
}

func TestDirectWaveReviewExternalDependencies(t *testing.T) {
	newFixture := func(t *testing.T) (string, *RuntimeStore, RegisteredProject) {
		vault, store, project := authorityFixture(t)
		writeDirectTask(t, vault, "ALP-T-0001", "W-0002", map[string]any{"title": "Alpha report", "status": "done", "readiness": "done"})
		writeDirectWave(t, vault, "W-0002", []string{"ALP-T-0001"}, map[string]any{"title": "Alpha wave"})
		writeDirectTask(t, vault, "BET-T-0001", "W-0003", map[string]any{"title": "Beta report", "status": "backlog", "readiness": "held"})
		writeDirectWave(t, vault, "W-0003", []string{"BET-T-0001"}, map[string]any{"title": "Beta wave"})
		writeDirectTask(t, vault, "BAD-T-0001", "", map[string]any{"title": "Malformed producer", "status": "backlog", "readiness": ""})
		writeDirectTask(t, vault, "ORF-T-0001", "W-0099", map[string]any{"title": "Orphaned producer", "status": "ready", "readiness": "ready"})
		writeDirectTask(t, vault, "NTL-T-0001", "", map[string]any{"title": "", "status": "ready", "readiness": "ready"})
		writeDirectTask(t, vault, "XPW-T-0001", "W-0004", map[string]any{"title": "Cross-project wave member", "status": "ready", "readiness": "ready"})
		writeDirectWave(t, vault, "W-0004", []string{"XPW-T-0001"}, map[string]any{"title": "Foreign wave", "project": "other-project"})
		writeDirectTask(t, vault, "COL-T-0001", "W-0002", map[string]any{"title": "Foreign task", "status": "done", "readiness": "done", "project": "other-project"})
		writeDirectWave(t, vault, "W-0001", []string{"FOL-T-0001", "FOL-T-0002"}, map[string]any{"title": "Follow-up wave"})
		return vault, store, project
	}
	consumerDeps := []any{"ALP-T-0001:hard", "BET-T-0001:hard", "EXT-T-0009:hard", "BAD-T-0001:hard", "ORF-T-0001:hard", "COL-T-0001:hard", "NTL-T-0001:hard", "XPW-T-0001:hard"}

	findFact := func(review directWaveReview, id string) *directWaveExternalDependency {
		for i := range review.ExternalDependencies {
			if review.ExternalDependencies[i].TaskID == id {
				return &review.ExternalDependencies[i]
			}
		}
		return nil
	}
	findMember := func(review directWaveReview, id string) *directWaveReviewMember {
		for i := range review.Members {
			if review.Members[i].TaskID == id {
				return &review.Members[i]
			}
		}
		return nil
	}

	t.Run("mixed completed and unfinished upstreams with titles and waves", func(t *testing.T) {
		vault, store, project := newFixture(t)
		writeDirectTask(t, vault, "FOL-T-0001", "W-0001", map[string]any{"dependencies": consumerDeps})
		writeDirectTask(t, vault, "FOL-T-0002", "W-0001", nil)
		review, err := buildDirectWaveReview(vault, store, project.ProjectID, "W-0001", nil)
		if err != nil {
			t.Fatal(err)
		}
		alpha := findFact(review, "ALP-T-0001")
		if alpha == nil || alpha.Classification != "external" || alpha.Title != "Alpha report" || alpha.Status != "done" || alpha.Readiness != "done" || alpha.WaveID != "W-0002" || alpha.WaveTitle != "Alpha wave" {
			t.Fatalf("alpha fact=%#v", alpha)
		}
		beta := findFact(review, "BET-T-0001")
		if beta == nil || beta.Classification != "external" || beta.Title != "Beta report" || beta.Status != "backlog" || beta.Readiness != "held" || beta.WaveID != "W-0003" || beta.WaveTitle != "Beta wave" {
			t.Fatalf("beta fact=%#v", beta)
		}
	})

	t.Run("waiting reason names the unfinished dependency not the completed one", func(t *testing.T) {
		vault, store, project := newFixture(t)
		writeDirectTask(t, vault, "FOL-T-0001", "W-0001", map[string]any{"dependencies": consumerDeps, "next_ref": "ALP-T-0001"})
		writeDirectTask(t, vault, "FOL-T-0002", "W-0001", nil)
		review, err := buildDirectWaveReview(vault, store, project.ProjectID, "W-0001", nil)
		if err != nil {
			t.Fatal(err)
		}
		consumer := findMember(review, "FOL-T-0001")
		if consumer == nil || consumer.State != "waiting" || consumer.WaitingReason != "waiting for dependency BET-T-0001" {
			t.Fatalf("consumer member=%#v", consumer)
		}
		if strings.Contains(consumer.WaitingReason, "ALP-T-0001") {
			t.Fatalf("completed dependency named as the wait: %q", consumer.WaitingReason)
		}
	})

	t.Run("unique stable sorted order across duplicate member references", func(t *testing.T) {
		vault, store, project := newFixture(t)
		writeDirectTask(t, vault, "FOL-T-0001", "W-0001", map[string]any{"dependencies": consumerDeps})
		writeDirectTask(t, vault, "FOL-T-0002", "W-0001", map[string]any{"dependencies": []any{"ALP-T-0001:hard", "COL-T-0001:hard"}})
		review, err := buildDirectWaveReview(vault, store, project.ProjectID, "W-0001", nil)
		if err != nil {
			t.Fatal(err)
		}
		var ids []string
		for _, fact := range review.ExternalDependencies {
			ids = append(ids, fact.TaskID)
		}
		want := []string{"ALP-T-0001", "BAD-T-0001", "BET-T-0001", "COL-T-0001", "EXT-T-0009", "NTL-T-0001", "ORF-T-0001", "XPW-T-0001"}
		if len(ids) != len(want) {
			t.Fatalf("external dependency ids=%v", ids)
		}
		for i, id := range want {
			if ids[i] != id {
				t.Fatalf("external dependency ids=%v, want %v", ids, want)
			}
		}
	})

	t.Run("missing and cross-project collision leak nothing", func(t *testing.T) {
		vault, store, project := newFixture(t)
		writeDirectTask(t, vault, "FOL-T-0001", "W-0001", map[string]any{"dependencies": consumerDeps})
		writeDirectTask(t, vault, "FOL-T-0002", "W-0001", nil)
		review, err := buildDirectWaveReview(vault, store, project.ProjectID, "W-0001", nil)
		if err != nil {
			t.Fatal(err)
		}
		missing := findFact(review, "EXT-T-0009")
		if missing == nil || *missing != (directWaveExternalDependency{TaskID: "EXT-T-0009", Classification: "missing"}) {
			t.Fatalf("missing fact=%#v", missing)
		}
		collision := findFact(review, "COL-T-0001")
		if collision == nil || *collision != (directWaveExternalDependency{TaskID: "COL-T-0001", Classification: "missing"}) {
			t.Fatalf("cross-project collision fact=%#v", collision)
		}
	})

	t.Run("unavailable producers invent no display facts", func(t *testing.T) {
		vault, store, project := newFixture(t)
		writeDirectTask(t, vault, "FOL-T-0001", "W-0001", map[string]any{"dependencies": consumerDeps})
		writeDirectTask(t, vault, "FOL-T-0002", "W-0001", nil)
		review, err := buildDirectWaveReview(vault, store, project.ProjectID, "W-0001", nil)
		if err != nil {
			t.Fatal(err)
		}
		malformed := findFact(review, "BAD-T-0001")
		if malformed == nil || *malformed != (directWaveExternalDependency{TaskID: "BAD-T-0001", Classification: "unavailable"}) {
			t.Fatalf("malformed fact=%#v", malformed)
		}
		orphaned := findFact(review, "ORF-T-0001")
		if orphaned == nil || *orphaned != (directWaveExternalDependency{TaskID: "ORF-T-0001", Classification: "unavailable"}) {
			t.Fatalf("orphaned fact=%#v", orphaned)
		}
		titleless := findFact(review, "NTL-T-0001")
		if titleless == nil || *titleless != (directWaveExternalDependency{TaskID: "NTL-T-0001", Classification: "unavailable"}) {
			t.Fatalf("titleless fact=%#v", titleless)
		}
		crossWave := findFact(review, "XPW-T-0001")
		if crossWave == nil || *crossWave != (directWaveExternalDependency{TaskID: "XPW-T-0001", Classification: "unavailable"}) {
			t.Fatalf("cross-project-wave fact=%#v", crossWave)
		}
	})

	t.Run("facts stay bounded on the wire", func(t *testing.T) {
		vault, store, project := newFixture(t)
		writeDirectTask(t, vault, "FOL-T-0001", "W-0001", map[string]any{"dependencies": consumerDeps})
		writeDirectTask(t, vault, "FOL-T-0002", "W-0001", nil)
		review, err := buildDirectWaveReview(vault, store, project.ProjectID, "W-0001", nil)
		if err != nil {
			t.Fatal(err)
		}
		if review.ExternalDependencies == nil {
			t.Fatal("externalDependencies is nil")
		}
		raw, err := json.Marshal(review.ExternalDependencies)
		if err != nil {
			t.Fatal(err)
		}
		var decoded []map[string]any
		if err := json.Unmarshal(raw, &decoded); err != nil {
			t.Fatal(err)
		}
		if len(decoded) != len(review.ExternalDependencies) {
			t.Fatalf("decoded facts: %s", raw)
		}
		allowed := map[string]bool{"taskId": true, "title": true, "status": true, "readiness": true, "waveId": true, "waveTitle": true, "classification": true}
		for _, fact := range decoded {
			for key := range fact {
				if !allowed[key] {
					t.Fatalf("external dependency fact carries unexpected key %q: %s", key, raw)
				}
				lower := strings.ToLower(key)
				for _, forbidden := range []string{"contract", "fingerprint", "body", "hash", "receipt", "runtime", "proof", "acceptance"} {
					if strings.Contains(lower, forbidden) {
						t.Fatalf("external dependency fact leaks %q key: %s", key, raw)
					}
				}
			}
		}
	})
}

func TestDirectWaveAuthorityWaveStartQueuesEligibleRoots(t *testing.T) {
	vault, store, project := authorityFixture(t)
	writeDirectTask(t, vault, "APP-T-0001", "W-0001", nil)
	writeDirectTask(t, vault, "APP-T-0002", "W-0001", map[string]any{"dependencies": []any{"APP-T-0001:hard"}})
	writeDirectTask(t, vault, "APP-T-0003", "W-0002", nil)
	writeDirectWave(t, vault, "W-0001", []string{"APP-T-0001", "APP-T-0002"}, nil)
	writeDirectWave(t, vault, "W-0002", []string{"APP-T-0003"}, nil)
	result, err := directWaveStart(vault, store, "W-0001", "human:sarav")
	if err != nil {
		t.Fatal(err)
	}
	if result.State != "Waiting" || result.Authorization != "authorized" || len(result.QueuedTaskIDs) != 1 || result.QueuedTaskIDs[0] != "APP-T-0001" {
		t.Fatalf("result=%#v", result)
	}
	directive, err := store.RunDirective(project.ProjectID, "APP-T-0001")
	if err != nil || directive == nil || directive.State != "queued" || directive.WaveID != "W-0001" || directive.AuthorizationFingerprint != result.MaterialFingerprint {
		t.Fatalf("directive=%#v err=%v", directive, err)
	}
	if other, _ := store.RunDirective(project.ProjectID, "APP-T-0002"); other != nil && other.State == "queued" {
		t.Fatal("downstream dependency was queued")
	}
	if other, _ := store.RunDirective(project.ProjectID, "APP-T-0003"); other != nil && other.State == "queued" {
		t.Fatal("sibling wave task was queued")
	}
	replay, err := directWaveStart(vault, store, "W-0001", "human:sarav")
	if err != nil {
		t.Fatal(err)
	}
	if !replay.Replayed || len(replay.QueuedTaskIDs) != 0 {
		t.Fatalf("replay=%#v", replay)
	}
	auth := waveAuthorizationState(t, vault, "W-0001")
	if stringField(auth, "state") != "armed" || boolFromAny(auth["stale"]) {
		t.Fatalf("authorization=%#v", auth)
	}
	if other := waveAuthorizationState(t, vault, "W-0002"); stringField(other, "state") != "disarmed" {
		t.Fatalf("sibling wave mutated: %#v", other)
	}
}

func TestDirectStartAuthorizesPendingProofButCompletionStillRequiresIt(t *testing.T) {
	vault, store, project := authorityFixture(t)
	if err := writeText(filepath.Join(v7RepoRoot(vault), "go.mod"), "module fixture\n\ngo 1.22\n"); err != nil {
		t.Fatal(err)
	}
	if err := writeText(filepath.Join(v7RepoRoot(vault), "proof_test.go"), "package fixture\nimport \"testing\"\nfunc TestProofExists(t *testing.T) {}\n"); err != nil {
		t.Fatal(err)
	}
	writePendingDirectTask(t, vault, "APP-T-0001", "W-0001", nil)
	writePendingDirectTask(t, vault, "APP-T-0002", "W-0001", map[string]any{"dependencies": []any{"APP-T-0001:hard"}})
	writePendingDirectTask(t, vault, "APP-T-0003", "", nil)
	writePendingDirectTask(t, vault, "APP-T-0004", "", nil)
	writeDirectWave(t, vault, "W-0001", []string{"APP-T-0001", "APP-T-0002"}, nil)

	standalone, err := directTaskBackgroundStart(vault, store, "APP-T-0003", "human:sarav")
	if err != nil || standalone.Authorization != "authorized" {
		t.Fatalf("pending-proof task start: result=%#v err=%v", standalone, err)
	}
	replay, err := directTaskBackgroundStart(vault, store, "APP-T-0003", "human:sarav")
	if err != nil || !replay.Replayed {
		t.Fatalf("duplicate task start: result=%#v err=%v", replay, err)
	}
	waveStart, err := directWaveStart(vault, store, "W-0001", "human:sarav")
	if err != nil || waveStart.Authorization != "authorized" || len(waveStart.QueuedTaskIDs) != 1 || waveStart.QueuedTaskIDs[0] != "APP-T-0001" {
		t.Fatalf("pending-proof wave start: result=%#v err=%v", waveStart, err)
	}
	if waveStart.Reason != "Queued" {
		t.Fatalf("wave start reason=%q", waveStart.Reason)
	}
	if directive, _ := store.RunDirective(project.ProjectID, "APP-T-0002"); directive != nil && directive.State == "queued" {
		t.Fatal("dependency-blocked member was dispatched")
	}

	rewriteTaskFile(t, vault, "APP-T-0001", func(data map[string]any, body string) (map[string]any, string) {
		data["status"] = "done"
		return data, body
	})
	review, err := buildDirectWaveReview(vault, store, project.ProjectID, "W-0001", nil)
	if err != nil {
		t.Fatal(err)
	}
	if review.State == "Completed" || review.Members[0].State == "completed" {
		t.Fatalf("pending proof falsely completed wave: %#v", review)
	}

	task, err := resolveV7Note(vault, "APP-T-0001", "task")
	if err != nil {
		t.Fatal(err)
	}
	fresh, report, failures, err := executeV7CommandVerificationRows(vault, task, nil, "reviewer:test", true)
	if err != nil || len(failures) != 0 || report.Status != "satisfied" || v7VerificationReceiptRequirementMissing(vault, fresh) != "" {
		t.Fatalf("normal proof executor: status=%q failures=%#v err=%v", report.Status, failures, err)
	}
	review, err = buildDirectWaveReview(vault, store, project.ProjectID, "W-0001", nil)
	if err != nil || review.Members[0].State != "completed" {
		t.Fatalf("fresh proof was not accepted: review=%#v err=%v", review, err)
	}

	rework, err := resolveV7Note(vault, "APP-T-0004", "task")
	if err != nil {
		t.Fatal(err)
	}
	rework, report, failures, err = executeV7CommandVerificationRows(vault, rework, nil, "reviewer:test", true)
	if err != nil || len(failures) != 0 || report.Status != "satisfied" {
		t.Fatalf("rework baseline proof: status=%q failures=%#v err=%v", report.Status, failures, err)
	}
	rewriteTaskFile(t, vault, "APP-T-0004", func(data map[string]any, body string) (map[string]any, string) {
		data["status"] = "rework"
		return data, strings.Replace(body, "Do APP-T-0004.", "Redo APP-T-0004 materially.", 1)
	})
	rework, err = resolveV7Note(vault, "APP-T-0004", "task")
	if err != nil || v7VerificationReceiptRequirementMissing(vault, rework) == "" {
		t.Fatalf("material rework did not stale old proof: task=%#v err=%v", rework.Data, err)
	}
	reworkStart, err := directTaskBackgroundStart(vault, store, "APP-T-0004", "human:sarav")
	if err != nil || reworkStart.Authorization != "authorized" {
		t.Fatalf("stale-proof rework start: result=%#v err=%v", reworkStart, err)
	}
}

func TestDirectWaveAuthorityRouteRemovedIsCaught(t *testing.T) {
	vault, store, project := authorityFixture(t)
	writeDirectTask(t, vault, "APP-T-0001", "W-0001", nil)
	writeDirectWave(t, vault, "W-0001", []string{"APP-T-0001"}, nil)
	review, err := buildDirectWaveReview(vault, store, project.ProjectID, "W-0001", nil)
	if err != nil || len(review.Blockers) != 0 {
		t.Fatalf("review=%#v err=%v", review, err)
	}
	configPath := managedTuskerLocalConfigPath(vault)
	configText, err := readText(configPath)
	if err != nil {
		t.Fatal(err)
	}
	configText = strings.Replace(configText, "test-codex-exec:", "removed-profile:", -1)
	if err := writeText(configPath, configText); err != nil {
		t.Fatal(err)
	}
	if _, err := directWaveStart(vault, store, "W-0001", "human:sarav"); err == nil {
		t.Fatal("wave start succeeded after routes were removed")
	}
	auth := waveAuthorizationState(t, vault, "W-0001")
	if stringField(auth, "state") == "armed" {
		t.Fatal("authorization was written despite route refusal")
	}
	if directive, _ := store.RunDirective(project.ProjectID, "APP-T-0001"); directive != nil && directive.State == "queued" {
		t.Fatal("directive queued despite route refusal")
	}
}

func TestDirectWaveAuthorityMaterialEditStalesAuthorization(t *testing.T) {
	vault, store, project := authorityFixture(t)
	writeDirectTask(t, vault, "APP-T-0001", "W-0001", nil)
	writeDirectWave(t, vault, "W-0001", []string{"APP-T-0001"}, nil)
	if _, err := directWaveStart(vault, store, "W-0001", "human:sarav"); err != nil {
		t.Fatal(err)
	}
	rewriteTaskFile(t, vault, "APP-T-0001", func(data map[string]any, body string) (map[string]any, string) {
		data["status"] = "ready"
		data["readiness"] = "ready"
		return data, body
	})
	if auth := waveAuthorizationState(t, vault, "W-0001"); boolFromAny(auth["stale"]) {
		t.Fatal("status/readiness-only edit staled authorization")
	}
	rewriteTaskFile(t, vault, "APP-T-0001", func(data map[string]any, body string) (map[string]any, string) {
		return data, strings.Replace(body, "Do APP-T-0001.", "Do something else entirely.", 1)
	})
	if auth := waveAuthorizationState(t, vault, "W-0001"); !boolFromAny(auth["stale"]) {
		t.Fatal("contract body edit did not stale authorization")
	}
	review, err := buildDirectWaveReview(vault, store, project.ProjectID, "W-0001", nil)
	if err != nil {
		t.Fatal(err)
	}
	if review.Authorization != "stale" {
		t.Fatalf("review authorization=%s", review.Authorization)
	}
	idx, err := loadV7Index(vault)
	if err != nil {
		t.Fatal(err)
	}
	task := idx.Tasks["APP-T-0001"]
	oldFP := stringField(task.Data, "contract_fingerprint")
	rewriteTaskFile(t, vault, "APP-T-0001", func(data map[string]any, body string) (map[string]any, string) {
		return data, strings.Replace(body, "command: go test ./x", "command: go test ./y", 1)
	})
	idx, _ = loadV7Index(vault)
	task = idx.Tasks["APP-T-0001"]
	if stringField(task.Data, "contract_fingerprint") == oldFP {
		t.Fatal("check-text edit did not change contract_fingerprint")
	}
}

func TestDirectWaveAuthorityGateWaitingSemantics(t *testing.T) {
	vault, store, project := authorityFixture(t)
	writeDirectTask(t, vault, "APP-T-0001", "W-0001", nil)
	writeDirectTask(t, vault, "APP-T-0002", "W-0001", map[string]any{"dependencies": []any{"APP-T-0001:hard"}})
	writeHumanGate(t, vault, "APP-G-0001", "APP-T-0002")
	writeDirectWave(t, vault, "W-0001", []string{"APP-T-0001", "APP-T-0002"}, nil)
	result, err := directWaveStart(vault, store, "W-0001", "human:sarav")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.QueuedTaskIDs) != 1 || result.QueuedTaskIDs[0] != "APP-T-0001" {
		t.Fatalf("root was not queued: %#v", result)
	}
	review, err := buildDirectWaveReview(vault, store, project.ProjectID, "W-0001", nil)
	if err != nil {
		t.Fatal(err)
	}
	var downstream *directWaveReviewMember
	for i := range review.Members {
		if review.Members[i].TaskID == "APP-T-0002" {
			downstream = &review.Members[i]
		}
	}
	if downstream == nil || downstream.State != "waiting" {
		t.Fatalf("downstream member=%#v", downstream)
	}
	vault2, store2, project2 := authorityFixture(t)
	writeDirectTask(t, vault2, "APP-T-0001", "W-0001", nil)
	writeHumanGate(t, vault2, "APP-G-0001", "APP-T-0001")
	writeDirectWave(t, vault2, "W-0001", []string{"APP-T-0001"}, nil)
	result2, err := directWaveStart(vault2, store2, "W-0001", "human:sarav")
	if err != nil {
		t.Fatal(err)
	}
	if result2.State != "Waiting" || result2.Authorization != "authorized" || len(result2.QueuedTaskIDs) != 0 {
		t.Fatalf("root-gated result=%#v", result2)
	}
	if directive, _ := store2.RunDirective(project2.ProjectID, "APP-T-0001"); directive != nil && directive.State == "queued" {
		t.Fatal("gated root was queued")
	}
	_ = project
}

func TestHumanApprovalContinuationReusesAuthorizedWave(t *testing.T) {
	vault, store, project := authorityFixture(t)
	writeDirectTask(t, vault, "APP-T-0001", "W-0001", nil)
	writeHumanGate(t, vault, "APP-G-0001", "APP-T-0001")
	writeDirectWave(t, vault, "W-0001", []string{"APP-T-0001"}, nil)

	start, err := directWaveStart(vault, store, "W-0001", "human:sarav")
	if err != nil || start.Authorization != "authorized" || start.State != "Waiting" || len(start.QueuedTaskIDs) != 0 {
		t.Fatalf("authorized gated wave=%#v err=%v", start, err)
	}
	review, err := buildDirectWaveReview(vault, store, project.ProjectID, "W-0001", nil)
	if err != nil || len(review.HumanActions) != 1 || review.HumanActions[0].Action.GateID != "APP-G-0001" {
		t.Fatalf("human action projection=%#v err=%v", review.HumanActions, err)
	}
	if err := gateV7Transition(Args{"vault": vault, "id": "APP-G-0001", "by": "human:sarav", "evidence": "Approved in Tusker.", "quiet": "true"}, "satisfied"); err != nil {
		t.Fatal(err)
	}
	queued, err := queueAuthorizedWaveFrontier(vault, store, project.ProjectID, "W-0001", time.Now().UTC())
	if err != nil || len(queued) != 1 || queued[0] != "APP-T-0001" {
		t.Fatalf("approval continuation queued=%#v err=%v", queued, err)
	}
	again, err := queueAuthorizedWaveFrontier(vault, store, project.ProjectID, "W-0001", time.Now().UTC())
	if err != nil || len(again) != 0 {
		t.Fatalf("duplicate continuation queued=%#v err=%v", again, err)
	}
}

func TestHumanApprovalContinuationPreservesPauseAndInertScope(t *testing.T) {
	for _, tc := range []struct {
		name, wantAuthorization string
		start, pause            bool
	}{
		{name: "inert", wantAuthorization: "disarmed"},
		{name: "paused", start: true, pause: true, wantAuthorization: "paused"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			vault, store, project := authorityFixture(t)
			writeDirectTask(t, vault, "APP-T-0001", "W-0001", nil)
			writeHumanGate(t, vault, "APP-G-0001", "APP-T-0001")
			writeDirectWave(t, vault, "W-0001", []string{"APP-T-0001"}, nil)
			if tc.start {
				if _, err := directWaveStart(vault, store, "W-0001", "human:sarav"); err != nil {
					t.Fatal(err)
				}
			}
			if tc.pause {
				if _, err := directWavePause(vault, store, "W-0001", "human:sarav"); err != nil {
					t.Fatal(err)
				}
			}
			if err := gateV7Transition(Args{"vault": vault, "id": "APP-G-0001", "by": "human:sarav", "evidence": "Approved in Tusker.", "quiet": "true"}, "satisfied"); err != nil {
				t.Fatal(err)
			}
			queued, err := queueAuthorizedWaveFrontier(vault, store, project.ProjectID, "W-0001", time.Now().UTC())
			if err != nil || len(queued) != 0 {
				t.Fatalf("queued=%#v err=%v", queued, err)
			}
			wave, _ := resolveV7Note(vault, "W-0001", "wave")
			if got := stringField(wave.Data, "authorization"); got != tc.wantAuthorization {
				t.Fatalf("authorization=%s want=%s", got, tc.wantAuthorization)
			}
		})
	}
}

func TestDirectWaveAuthorityCrashBeforeQueueStaysWaiting(t *testing.T) {
	vault, store, project := authorityFixture(t)
	writeDirectTask(t, vault, "APP-T-0001", "W-0001", nil)
	writeDirectWave(t, vault, "W-0001", []string{"APP-T-0001"}, nil)
	directWaveStartInjectCrashBeforeQueue = func() bool { return true }
	defer func() { directWaveStartInjectCrashBeforeQueue = nil }()
	result, err := directWaveStart(vault, store, "W-0001", "human:sarav")
	if err != nil {
		t.Fatal(err)
	}
	if result.State != "Waiting" || result.Authorization != "authorized" || len(result.QueuedTaskIDs) != 0 {
		t.Fatalf("crash result=%#v", result)
	}
	auth := waveAuthorizationState(t, vault, "W-0001")
	if stringField(auth, "state") != "armed" {
		t.Fatalf("authorization was not durable: %#v", auth)
	}
	if directive, _ := store.RunDirective(project.ProjectID, "APP-T-0001"); directive != nil && directive.State == "queued" {
		t.Fatal("directive was queued after injected crash")
	}
	directWaveStartInjectCrashBeforeQueue = nil
	result, err = directWaveStart(vault, store, "W-0001", "human:sarav")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.QueuedTaskIDs) != 1 {
		t.Fatalf("recovery did not queue root: %#v", result)
	}
}

func TestDirectStartAuthorityBackgroundScopeAndBlockers(t *testing.T) {
	vault, store, project := authorityFixture(t)
	writeDirectTask(t, vault, "APP-T-0001", "", nil)
	writeDirectTask(t, vault, "APP-T-0002", "", map[string]any{"dependencies": []any{"APP-T-0003:hard"}})
	writeDirectTask(t, vault, "APP-T-0003", "", map[string]any{"status": "backlog"})
	writeDirectTask(t, vault, "APP-T-0004", "", nil)
	writeHumanGate(t, vault, "APP-G-0001", "APP-T-0004")
	writeDirectTask(t, vault, "APP-T-0005", "", nil)
	if err := store.UpsertRun(RunStatus{ProjectID: project.ProjectID, RecordID: "APP-T-0005", ItemID: "APP-T-0005", LeaseState: string(LeaseStateRunning), LeaseOwner: "agent:other", LeaseExpiresAt: time.Now().Add(time.Hour).UTC().Format(time.RFC3339)}); err != nil {
		t.Fatal(err)
	}
	result, err := directTaskBackgroundStart(vault, store, "APP-T-0001", "human:sarav")
	if err != nil {
		t.Fatal(err)
	}
	if result.Authorization != "authorized" || result.State != "Waiting" || len(result.QueuedTaskIDs) != 1 {
		t.Fatalf("result=%#v", result)
	}
	directive, err := store.RunDirective(project.ProjectID, "APP-T-0001")
	if err != nil || directive == nil || directive.WaveID != "" || directive.AuthorizationFingerprint == "" || directive.Actor != "human:sarav" {
		t.Fatalf("directive=%#v err=%v", directive, err)
	}
	taskData, _, err := parseFrontmatterMustRead(filepath.Join(vault, "work", "tasks", "APP-T-0001.md"))
	if err != nil {
		t.Fatal(err)
	}
	if directive.AuthorizationFingerprint != stringField(taskData, "contract_fingerprint") {
		t.Fatal("directive fingerprint does not match task contract_fingerprint")
	}
	if stringField(taskData, "status") != "backlog" || stringField(taskData, "readiness") != "held" {
		t.Fatal("background start mutated task lifecycle")
	}
	replay, err := directTaskBackgroundStart(vault, store, "APP-T-0001", "human:sarav")
	if err != nil || !replay.Replayed {
		t.Fatalf("replay=%#v err=%v", replay, err)
	}
	for _, tc := range []struct{ id, code string }{
		{"APP-T-0002", "DEPENDENCY_WAITING"},
		{"APP-T-0004", "HUMAN_GATE_OPEN"},
		{"APP-T-0005", "ACTIVE_OWNER"},
	} {
		if _, err := directTaskBackgroundStart(vault, store, tc.id, "human:sarav"); err == nil || !strings.Contains(err.Error(), tc.code) {
			t.Fatalf("%s start: err=%v want %s", tc.id, err, tc.code)
		}
		if directive, _ := store.RunDirective(project.ProjectID, tc.id); directive != nil && directive.State == "queued" {
			t.Fatalf("%s was queued despite blocker", tc.id)
		}
	}
}

func TestDirectStartAuthorityBackgroundRequiresRegistration(t *testing.T) {
	vault := v7DirectTestVault(t)
	stateRoot := filepath.Join(t.TempDir(), "state")
	t.Setenv("TUSKER_STATE_ROOT", stateRoot)
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	writeDirectTask(t, vault, "APP-T-0001", "", nil)
	if _, err := directTaskBackgroundStart(vault, store, "APP-T-0001", "human:sarav"); err == nil {
		t.Fatal("background start succeeded without a registered project")
	}
}

func TestDirectStartAuthorityInteractiveClaimsPlannedTask(t *testing.T) {
	vault := v7DirectTestVault(t)
	repoRoot := v7RepoRoot(vault)
	if out, err := gitCombined(repoRoot, "init", "-b", "main"); err != nil {
		t.Skipf("git init unavailable: %v %s", err, out)
	}
	if _, err := gitCombined(repoRoot, "-c", "user.email=test@example.com", "-c", "user.name=test", "commit", "--allow-empty", "-m", "init"); err != nil {
		t.Skipf("git commit unavailable: %v", err)
	}
	stateRoot := filepath.Join(t.TempDir(), "state")
	t.Setenv("TUSKER_STATE_ROOT", stateRoot)
	t.Setenv("TUSKER_ATTEMPT_ID", "")
	t.Setenv("CODEX_SESSION_ID", "native-session")
	writeDirectTask(t, vault, "APP-T-0001", "", nil)
	writeDirectTask(t, vault, "APP-T-0002", "", map[string]any{"dependencies": []any{"APP-T-0003:hard"}})
	writeDirectTask(t, vault, "APP-T-0003", "", nil)
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(repoRoot); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(cwd) }()
	if err := taskStartCmd(Args{"vault": vault, "_pos0": "APP-T-0001", "mode": "interactive", "current-workspace": "true", "by": "agent:codex", "quiet": "true", "embedded": "true"}); err != nil {
		t.Fatal(err)
	}
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	projectID, err := resolveV7ProjectID(vault)
	if err != nil {
		t.Fatal(err)
	}
	run, err := store.FindRunScoped(projectID, "APP-T-0001")
	if err != nil || run == nil || run.LeaseState != string(LeaseStateClaimed) && run.LeaseState != string(LeaseStateRunning) {
		t.Fatalf("run=%#v err=%v", run, err)
	}
	data, _, err := parseFrontmatterMustRead(filepath.Join(vault, "work", "tasks", "APP-T-0001.md"))
	if err != nil {
		t.Fatal(err)
	}
	if stringField(data, "status") != "backlog" || stringField(data, "readiness") != "held" {
		t.Fatal("interactive claim persisted a lifecycle edit")
	}
	if err := taskStartCmd(Args{"vault": vault, "_pos0": "APP-T-0002", "mode": "interactive", "current-workspace": "true", "by": "agent:codex", "quiet": "true", "embedded": "true"}); err == nil {
		t.Fatal("dependency-blocked task claimed interactively")
	}
}

func TestDirectStartAuthorityInteractiveBlockedByGateAndOwner(t *testing.T) {
	vault, store, project := authorityFixture(t)
	repoRoot := v7RepoRoot(vault)
	if out, err := gitCombined(repoRoot, "init", "-b", "main"); err != nil {
		t.Skipf("git init unavailable: %v %s", err, out)
	}
	if _, err := gitCombined(repoRoot, "-c", "user.email=test@example.com", "-c", "user.name=test", "commit", "--allow-empty", "-m", "init"); err != nil {
		t.Skipf("git commit unavailable: %v", err)
	}
	t.Setenv("TUSKER_ATTEMPT_ID", "")
	t.Setenv("CODEX_SESSION_ID", "native-session")
	writeDirectTask(t, vault, "APP-T-0001", "", nil)
	writeHumanGate(t, vault, "APP-G-0001", "APP-T-0001")
	writeDirectTask(t, vault, "APP-T-0002", "", nil)
	if err := store.UpsertRun(RunStatus{ProjectID: project.ProjectID, RecordID: "APP-T-0002", ItemID: "APP-T-0002", LeaseState: string(LeaseStateRunning), LeaseOwner: "agent:other", LeaseExpiresAt: time.Now().Add(time.Hour).UTC().Format(time.RFC3339)}); err != nil {
		t.Fatal(err)
	}
	cwd, _ := os.Getwd()
	if err := os.Chdir(repoRoot); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(cwd) }()
	for _, id := range []string{"APP-T-0001", "APP-T-0002"} {
		if err := taskStartCmd(Args{"vault": vault, "_pos0": id, "mode": "interactive", "current-workspace": "true", "by": "agent:codex", "quiet": "true", "embedded": "true"}); err == nil {
			t.Fatalf("%s claimed despite gate/owner blocker", id)
		}
	}
}

func TestDirectWaveAuthoritySoftEdgeFrontierOrdering(t *testing.T) {
	vault, store, project := authorityFixture(t)
	writeDirectTask(t, vault, "APP-T-0001", "W-0001", nil)
	writeDirectTask(t, vault, "APP-T-0002", "W-0001", map[string]any{"dependencies": []any{"APP-T-0001:soft"}})
	writeDirectWave(t, vault, "W-0001", []string{"APP-T-0001", "APP-T-0002"}, nil)
	review, err := buildDirectWaveReview(vault, store, project.ProjectID, "W-0001", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(review.Frontiers) != 2 || review.Frontiers[0][0] != "APP-T-0001" || review.Frontiers[1][0] != "APP-T-0002" {
		t.Fatalf("soft dependency broke frontier ordering: %#v", review.Frontiers)
	}
}

func TestDirectWaveAuthorityRuntimeFactsScopedByProject(t *testing.T) {
	vault, store, project := authorityFixture(t)
	writeDirectTask(t, vault, "APP-T-0001", "W-0001", nil)
	writeDirectWave(t, vault, "W-0001", []string{"APP-T-0001"}, nil)
	if err := store.UpsertRun(RunStatus{ProjectID: "other-project", RecordID: "APP-T-0001", LeaseState: string(LeaseStateRunning), LeaseOwner: "agent:other", LeaseExpiresAt: time.Now().Add(time.Hour).UTC().Format(time.RFC3339)}); err != nil {
		t.Fatal(err)
	}
	review, err := buildDirectWaveReview(vault, store, project.ProjectID, "W-0001", nil)
	if err != nil {
		t.Fatal(err)
	}
	if review.Members[0].State != "ready" {
		t.Fatalf("foreign project run created a false owner: %#v", review.Members[0])
	}
	review, err = buildDirectWaveReview(vault, store, project.ProjectID, "W-0001", fmt.Errorf("database is locked"))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, blocker := range review.Blockers {
		if blocker.Code == "RUNTIME_UNAVAILABLE" {
			found = true
		}
	}
	if !found {
		t.Fatalf("runtime error did not surface: %#v", review.Blockers)
	}
}

func TestDirectWaveAuthorityInstructionsCarryFullBody(t *testing.T) {
	vault, store, project := authorityFixture(t)
	writeDirectTask(t, vault, "APP-T-0001", "W-0001", nil)
	writeDirectWave(t, vault, "W-0001", []string{"APP-T-0001"}, nil)
	path := filepath.Join(vault, "work", "tasks", "APP-T-0001.md")
	data, body, err := parseFrontmatterMustRead(path)
	if err != nil {
		t.Fatal(err)
	}
	body += "\n## Implementation notes\n\nEdge case: empty input must not panic.\n"
	data["contract_fingerprint"] = directWaveTaskContractFingerprint(data, body)
	data["state_rev"] = v7StateRev(data, body)
	content, _ := serializeDocument(data, body, v7FrontmatterOrder["task"])
	if err := writeText(path, content); err != nil {
		t.Fatal(err)
	}
	review, err := buildDirectWaveReview(vault, store, project.ProjectID, "W-0001", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(review.Members[0].Instructions, "empty input must not panic") {
		t.Fatalf("instructions dropped body outside Intent: %q", review.Members[0].Instructions)
	}
}

func TestDirectWaveAuthorityCrossWaveDependencyContracts(t *testing.T) {
	vault, store, project := authorityFixture(t)
	writeDirectTask(t, vault, "APP-T-0001", "W-0001", map[string]any{"dependencies": []any{"EXT-T-0001:hard"}})
	writeDirectTask(t, vault, "EXT-T-0001", "", nil)
	writeDirectWave(t, vault, "W-0001", []string{"APP-T-0001"}, nil)
	review, err := buildDirectWaveReview(vault, store, project.ProjectID, "W-0001", nil)
	if err != nil {
		t.Fatal(err)
	}
	assertBlockerCode := func(want string) {
		t.Helper()
		found := false
		for _, blocker := range review.Blockers {
			if blocker.Code == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("missing %s: %#v", want, review.Blockers)
		}
	}
	assertBlockerCode("DEPENDENCY_CONTRACT_INVALID")
	extData, extBody, _ := parseFrontmatterMustRead(filepath.Join(vault, "work", "tasks", "EXT-T-0001.md"))
	extFP := directWaveTaskContractFingerprint(extData, extBody)
	rewriteTaskFile(t, vault, "APP-T-0001", func(data map[string]any, body string) (map[string]any, string) {
		data["dependency_contracts"] = []any{
			map[string]any{"task_id": "EXT-T-0001", "kind": "hard", "target_contract_fingerprint": extFP},
			map[string]any{"task_id": "EXT-T-0001", "kind": "hard", "target_contract_fingerprint": extFP},
		}
		return data, body
	})
	review, err = buildDirectWaveReview(vault, store, project.ProjectID, "W-0001", nil)
	if err != nil {
		t.Fatal(err)
	}
	assertBlockerCode("DEPENDENCY_CONTRACT_INVALID")
	rewriteTaskFile(t, vault, "APP-T-0001", func(data map[string]any, body string) (map[string]any, string) {
		data["dependency_contracts"] = []any{map[string]any{"task_id": "EXT-T-0001", "kind": "hard", "target_contract_fingerprint": extFP}}
		return data, body
	})
	review, err = buildDirectWaveReview(vault, store, project.ProjectID, "W-0001", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, blocker := range review.Blockers {
		if blocker.Code == "DEPENDENCY_CONTRACT_INVALID" {
			t.Fatalf("valid cross-wave contract refused: %#v", blocker)
		}
	}
}

func TestDirectWaveAuthorityStoredFingerprintStale(t *testing.T) {
	vault, store, project := authorityFixture(t)
	writeDirectTask(t, vault, "APP-T-0001", "W-0001", nil)
	writeDirectWave(t, vault, "W-0001", []string{"APP-T-0001"}, nil)
	path := filepath.Join(vault, "work", "tasks", "APP-T-0001.md")
	data, body, err := parseFrontmatterMustRead(path)
	if err != nil {
		t.Fatal(err)
	}
	body = strings.Replace(body, "Do APP-T-0001.", "Do changed material.", 1)
	data["state_rev"] = v7StateRev(data, body)
	content, _ := serializeDocument(data, body, v7FrontmatterOrder["task"])
	if err := writeText(path, content); err != nil {
		t.Fatal(err)
	}
	review, err := buildDirectWaveReview(vault, store, project.ProjectID, "W-0001", nil)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, blocker := range review.Blockers {
		if blocker.Code == "CONTRACT_FINGERPRINT_STALE" {
			found = true
		}
	}
	if !found {
		t.Fatalf("stale stored fingerprint not reported: %#v", review.Blockers)
	}
	if _, err := directWaveStart(vault, store, "W-0001", "human:sarav"); err == nil {
		t.Fatal("wave start authorized stale stored fingerprint")
	}
	if _, err := directTaskBackgroundStart(vault, store, "APP-T-0001", "human:sarav"); err == nil || !strings.Contains(err.Error(), "CONTRACT_FINGERPRINT_STALE") {
		t.Fatalf("task start err=%v", err)
	}
}

func TestDirectWaveAuthorityRevalidatesUnderLock(t *testing.T) {
	vault, store, project := authorityFixture(t)
	writeDirectTask(t, vault, "APP-T-0001", "W-0001", nil)
	writeDirectWave(t, vault, "W-0001", []string{"APP-T-0001"}, nil)
	directWaveStartInjectAfterReview = func() {
		writeHumanGate(t, vault, "APP-G-0001", "APP-T-0001")
	}
	defer func() { directWaveStartInjectAfterReview = nil }()
	if _, err := directWaveStart(vault, store, "W-0001", "human:sarav"); err == nil {
		t.Fatal("wave start authorized material mutated after the initial review")
	}
	auth := waveAuthorizationState(t, vault, "W-0001")
	if stringField(auth, "state") == "armed" {
		t.Fatal("authorization written for mutated material")
	}
	if directive, _ := store.RunDirective(project.ProjectID, "APP-T-0001"); directive != nil && directive.State == "queued" {
		t.Fatal("directive queued for mutated material")
	}
}

func TestDirectWaveAuthorityOwnershipSemantics(t *testing.T) {
	vault, store, project := authorityFixture(t)
	writeDirectTask(t, vault, "APP-T-0001", "W-0001", nil)
	writeDirectTask(t, vault, "APP-T-0002", "W-0001", nil)
	writeDirectWave(t, vault, "W-0001", []string{"APP-T-0001", "APP-T-0002"}, nil)
	if err := store.UpsertRun(RunStatus{ProjectID: project.ProjectID, RecordID: "APP-T-0001", ItemID: "APP-T-0001", LeaseState: string(LeaseStateRunning), LeaseOwner: "agent:other", LeaseExpiresAt: time.Now().Add(time.Hour).UTC().Format(time.RFC3339)}); err != nil {
		t.Fatal(err)
	}
	if _, err := directWaveStart(vault, store, "W-0001", "human:sarav"); err == nil || !strings.Contains(err.Error(), "ACTIVE_OWNER") {
		t.Fatalf("inert wave with foreign owner: err=%v", err)
	}
	if auth := waveAuthorizationState(t, vault, "W-0001"); stringField(auth, "state") == "armed" {
		t.Fatal("authorization written despite foreign owner")
	}
	if err := store.UpsertRun(RunStatus{ProjectID: project.ProjectID, RecordID: "APP-T-0001", ItemID: "APP-T-0001", LeaseState: string(LeaseStateReleased), LeaseOwner: "agent:other", Terminal: true}); err != nil {
		t.Fatal(err)
	}
	startResult, err := directWaveStart(vault, store, "W-0001", "human:sarav")
	if err != nil {
		t.Fatal(err)
	}
	directive, _ := store.RunDirective(project.ProjectID, "APP-T-0001")
	if directive == nil {
		directives, _ := store.ListActiveRunDirectives(project.ProjectID, time.Now().UTC())
		t.Fatalf("root directive missing after start: result=%#v directives=%#v", startResult, directives)
	}
	if err := store.UpsertRun(RunStatus{ProjectID: project.ProjectID, RecordID: "APP-T-0001", ItemID: "APP-T-0001", LeaseState: string(LeaseStateRunning), LeaseOwner: "attempt-1", LeaseGeneration: 1, ActiveAttemptID: "attempt-1", LeaseExpiresAt: time.Now().Add(time.Hour).UTC().Format(time.RFC3339)}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveRunAuthorization(RunAuthorization{ProjectID: project.ProjectID, RecordID: "APP-T-0001", Source: "human_run_directive", Actor: "human:sarav", LeaseGeneration: 1, AttemptID: "attempt-1", DirectiveWaveID: "W-0001", DirectiveAuthorizationFingerprint: startResult.MaterialFingerprint, DirectiveWaveAuthorizedAt: directive.WaveAuthorizedAt}); err != nil {
		t.Fatal(err)
	}
	replay, err := directWaveStart(vault, store, "W-0001", "human:sarav")
	if err != nil {
		t.Fatalf("admitted owner treated as conflict: %v", err)
	}
	if !replay.Replayed {
		t.Fatalf("replay=%#v", replay)
	}
	review, err := buildDirectWaveReview(vault, store, project.ProjectID, "W-0001", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, blocker := range review.Blockers {
		if blocker.Code == "ACTIVE_OWNER" {
			t.Fatalf("admitted wave owner reported as conflict: %#v", blocker)
		}
	}
}

func TestDirectWaveAuthorityReviewReadOnlyRuntime(t *testing.T) {
	vault, store, project := authorityFixture(t)
	store.Close()
	stateRoot := filepath.Dir(runtimeStoreDBPath(DefaultStateRoot()))
	if err := os.Remove(runtimeStoreDBPath(DefaultStateRoot())); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	writeDirectTask(t, vault, "APP-T-0001", "W-0001", nil)
	writeDirectWave(t, vault, "W-0001", []string{"APP-T-0001"}, nil)
	if err := waveReviewCmd(Args{"vault": vault, "_pos0": "W-0001", "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	if fileExists(runtimeStoreDBPath(DefaultStateRoot())) {
		t.Fatal("wave review created a runtime database")
	}
	dbPath := runtimeStoreDBPath(DefaultStateRoot())
	if err := os.MkdirAll(dbPath, 0o755); err != nil {
		t.Fatal(err)
	}
	reviewStore, runtimeErr := directWaveReviewRuntimeStore()
	if reviewStore != nil {
		reviewStore.Close()
	}
	review, err := buildDirectWaveReview(vault, nil, project.ProjectID, "W-0001", runtimeErr)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, blocker := range review.Blockers {
		if blocker.Code == "RUNTIME_UNAVAILABLE" {
			found = true
		}
	}
	if !found || runtimeErr == nil {
		t.Fatalf("unreadable DB did not surface RUNTIME_UNAVAILABLE: err=%v blockers=%#v", runtimeErr, review.Blockers)
	}
	_ = os.RemoveAll(stateRoot)
}

func TestDirectStartAuthorityTaskDirectiveConsumedOnce(t *testing.T) {
	vault, store, project := authorityFixture(t)
	writeDirectTask(t, vault, "APP-T-0001", "", nil)
	if _, err := directTaskBackgroundStart(vault, store, "APP-T-0001", "human:sarav"); err != nil {
		t.Fatal(err)
	}
	directive, err := store.RunDirective(project.ProjectID, "APP-T-0001")
	if err != nil || directive == nil {
		t.Fatal("directive missing")
	}
	idx, err := loadV7Index(vault)
	if err != nil {
		t.Fatal(err)
	}
	task := idx.Tasks["APP-T-0001"]
	now := time.Now().UTC()
	if !runDirectiveMatchesTaskAuthority(vault, task, directive, now) {
		t.Fatal("queued task directive did not match task authority")
	}
	auth := &RunAuthorization{Source: "human_run_directive", Actor: "human:sarav", LeaseGeneration: 1, DirectiveAuthorizationFingerprint: directive.AuthorizationFingerprint}
	run := RunStatus{ProjectID: project.ProjectID, RecordID: "APP-T-0001", LeaseGeneration: 1}
	if !runDirectiveAuthorizationMatchesTaskAuthority(vault, task, run, auth) {
		t.Fatal("task-scope authorization did not match task authority")
	}
	consumed := *directive
	consumed.State = "consumed"
	if !consumedRunDirectiveMatchesTaskAuthority(vault, task, run, &consumed, auth, now) {
		t.Fatal("consumed task directive did not match task authority")
	}
	rewriteTaskFile(t, vault, "APP-T-0001", func(data map[string]any, body string) (map[string]any, string) {
		return data, strings.Replace(body, "Do APP-T-0001.", "Do different material.", 1)
	})
	idx, _ = loadV7Index(vault)
	task = idx.Tasks["APP-T-0001"]
	if runDirectiveMatchesTaskAuthority(vault, task, directive, now) {
		t.Fatal("changed material still matched the old directive")
	}
	if runDirectiveAuthorizationMatchesTaskAuthority(vault, task, run, auth) {
		t.Fatal("changed material still matched the old authorization")
	}
	if consumedRunDirectiveMatchesTaskAuthority(vault, task, run, &consumed, auth, now) {
		t.Fatal("changed material still matched the consumed directive")
	}
	if queued, err := store.QueueRunDirective(*directive); err != nil || queued {
		t.Fatalf("duplicate directive overwrote the queued authorization: queued=%t err=%v", queued, err)
	}
}

func TestDirectStartAuthorityTaskStartLocksMaterial(t *testing.T) {
	vault, store, project := authorityFixture(t)
	writeDirectTask(t, vault, "APP-T-0001", "", nil)
	var newFP string
	directTaskStartInjectBeforeLock = func() {
		rewriteTaskFile(t, vault, "APP-T-0001", func(data map[string]any, body string) (map[string]any, string) {
			return data, strings.Replace(body, "Do APP-T-0001.", "Do the updated contract.", 1)
		})
		idx, _ := loadV7Index(vault)
		newFP = stringField(idx.Tasks["APP-T-0001"].Data, "contract_fingerprint")
	}
	defer func() { directTaskStartInjectBeforeLock = nil }()
	result, err := directTaskBackgroundStart(vault, store, "APP-T-0001", "human:sarav")
	if err != nil {
		t.Fatal(err)
	}
	directive, _ := store.RunDirective(project.ProjectID, "APP-T-0001")
	if directive == nil || directive.AuthorizationFingerprint != newFP || result.MaterialFingerprint != newFP {
		t.Fatalf("queued fingerprint is not the locked current material: %#v result=%#v", directive, result)
	}
}

func TestDirectWaveAuthorityAmendmentInvalidatesPassedVerification(t *testing.T) {
	vault := v7DirectTestVault(t)
	writeDirectTask(t, vault, "APP-T-0001", "W-0001", nil)
	rewriteTaskFile(t, vault, "APP-T-0001", func(data map[string]any, body string) (map[string]any, string) {
		data["status"] = "done"
		data["readiness"] = "done"
		data["proof_status"] = "satisfied"
		data["accepted_by"] = "human:sarav"
		data["accepted_at"] = "2026-02-01T00:00:00Z"
		data["closed_at"] = "2026-02-01T00:00:00Z"
		return data, body
	})
	before, beforeBody, err := parseFrontmatterMustRead(filepath.Join(vault, "work", "tasks", "APP-T-0001.md"))
	if err != nil {
		t.Fatal(err)
	}
	reworkBody := directAuthoringBodyPath(t, vault, "rework-body.md", strings.Replace(beforeBody, "Do APP-T-0001.", "Do the amended contract.", 1))
	if err := updateV7TaskCmd(Args{"vault": vault, "quiet": "true", "_pos0": "APP-T-0001", "if-revision": stringField(before, "state_rev"), "body-file": reworkBody, "by": "agent:builder"}); err != nil {
		t.Fatal(err)
	}
	data, body, err := parseFrontmatterMustRead(filepath.Join(vault, "work", "tasks", "APP-T-0001.md"))
	if err != nil {
		t.Fatal(err)
	}
	if stringField(data, "status") != "rework" || stringField(data, "proof_status") != "pending" {
		t.Fatalf("rework transition=%v/%v", data["status"], data["proof_status"])
	}
	rows := parseV7VerificationRows(body)
	if len(rows) != 1 || rows[0].Result != "pending" || !strings.Contains(rows[0].Notes, "invalidated by task contract amendment") {
		t.Fatalf("amendment retained passed proof: %#v", rows)
	}
}

func TestDirectStartAuthorityInteractiveRequiresBy(t *testing.T) {
	vault := v7DirectTestVault(t)
	writeDirectTask(t, vault, "APP-T-0001", "", nil)
	err := taskStartCmd(Args{"vault": vault, "_pos0": "APP-T-0001", "mode": "interactive", "current-workspace": "true", "quiet": "true"})
	if err == nil || !strings.Contains(err.Error(), "--by") {
		t.Fatalf("missing --by accepted: %v", err)
	}
}

// armWaveForTest writes armed authorization fields for the exact current
// material — the same identity directWaveStart would store — without a
// runtime store or registered project.
func armWaveForTest(t *testing.T, vault string) {
	t.Helper()
	idx, err := loadV7Index(vault)
	if err != nil {
		t.Fatal(err)
	}
	wave, ok := idx.Waves["W-0001"]
	if !ok {
		t.Fatal("W-0001 missing")
	}
	fingerprint, issues := waveMaterialFingerprint(vault, idx, wave)
	if len(issues) > 0 {
		t.Fatalf("wave material not armable: %v", issues)
	}
	path := filepath.Join(vault, "work", "waves", "W-0001.md")
	data, body, err := parseFrontmatterMustRead(path)
	if err != nil {
		t.Fatal(err)
	}
	data["authorization"] = "armed"
	data["authorization_fingerprint"] = fingerprint
	data["authorized_by"] = "human:test"
	data["authorized_at"] = "2026-01-01T00:00:00Z"
	data["state_rev"] = v7StateRev(data, body)
	content, err := serializeDocument(data, body, v7FrontmatterOrder["wave"])
	if err != nil {
		t.Fatal(err)
	}
	if err := writeText(path, content); err != nil {
		t.Fatal(err)
	}
}

func TestDirectWaveAuthorityAuthoredColumnEditsStaleContract(t *testing.T) {
	vault, store, project := authorityFixture(t)
	// A three-column Acceptance table keeps its authored Proof column in the
	// contract canon; only ledger-valued columns are excluded by header name.
	body := "# APP-T-0001\n\n## Intent\n\nDo it.\n\n## Acceptance\n\n| ID | Outcome | Proof |\n| --- | --- | --- |\n| A1 | Works. | show the log |\n\n## Verification\n\n| Covers | Check | Result | Notes |\n| --- | --- | --- | --- |\n| A1 | command: go test ./x | pending | |\n"
	data := map[string]any{
		"schema": "tusker.task/v7", "kind": "task", "id": "APP-T-0001", "project": v7ProjectID(vault),
		"title": "Direct APP-T-0001", "status": "backlog", "readiness": "held",
		"proof_mode": "inline", "proof_status": "pending", "proof_required": []any{"focused_test"},
		"work_level": "standard", "next_owner": "agent",
		"created_at": "2026-01-01T00:00:00Z", "created_by": "agent:test", "updated_at": "2026-01-01T00:00:00Z", "updated_by": "agent:test",
	}
	data["contract_fingerprint"] = directWaveTaskContractFingerprint(data, body)
	data["state_rev"] = v7StateRev(data, body)
	content, err := serializeDocument(data, body, v7FrontmatterOrder["task"])
	if err != nil {
		t.Fatal(err)
	}
	if err := writeText(filepath.Join(vault, "work", "tasks", "APP-T-0001.md"), content); err != nil {
		t.Fatal(err)
	}
	if _, err := directTaskBackgroundStart(vault, store, "APP-T-0001", "human:sarav"); err != nil {
		t.Fatal(err)
	}
	directive, err := store.RunDirective(project.ProjectID, "APP-T-0001")
	if err != nil || directive == nil || directive.State != "queued" {
		t.Fatalf("missing queued directive: %#v err=%v", directive, err)
	}
	// Editing the authored Proof column must stale the contract: the stored
	// pin bound it, so the queued authorization no longer covers these bytes.
	writeTaskFileOutOfBand(t, vault, "APP-T-0001", func(d map[string]any, b string) (map[string]any, string) {
		b = strings.Replace(b, "| A1 | Works. | show the log |", "| A1 | Works. | narrate a demo |", 1)
		d["state_rev"] = v7StateRev(d, b)
		return d, b
	})
	idx, err := loadV7Index(vault)
	if err != nil {
		t.Fatal(err)
	}
	task := idx.Tasks["APP-T-0001"]
	if reason := directWaveTaskContractStaleReason(task); !strings.Contains(reason, "contract drifted") {
		t.Fatalf("authored Proof-column edit did not stale the contract: %q", reason)
	}
	if runDirectiveMatchesTaskAuthority(vault, task, directive, time.Now().UTC()) {
		t.Fatal("queued directive still admits after an authored contract edit")
	}
	// Ledger-valued Result/Notes edits under the same table stay neutral.
	writeTaskFileOutOfBand(t, vault, "APP-T-0001", func(d map[string]any, b string) (map[string]any, string) {
		b = strings.Replace(b, "| A1 | Works. | narrate a demo |", "| A1 | Works. | show the log |", 1)
		b = strings.Replace(b, "| A1 | command: go test ./x | pending | |", "| A1 | command: go test ./x | pass | recorded |", 1)
		d["contract_fingerprint"] = directWaveTaskContractFingerprint(d, b)
		d["state_rev"] = v7StateRev(d, b)
		return d, b
	})
	idx, err = loadV7Index(vault)
	if err != nil {
		t.Fatal(err)
	}
	if reason := directWaveTaskContractStaleReason(idx.Tasks["APP-T-0001"]); reason != "" {
		t.Fatalf("Result/Notes ledger edit staled the contract: %q", reason)
	}
}

func TestDirectWaveReconcileRebasesEraContractPins(t *testing.T) {
	vault, _, _ := authorityFixture(t)
	bodyWithProof := func(id string) string {
		return "# " + id + "\n\n## Intent\n\nDo it.\n\n## Acceptance\n\n| ID | Outcome | Proof |\n| --- | --- | --- |\n| A1 | Works. | show the log |\n\n## Verification\n\n| Covers | Check | Result | Notes |\n| --- | --- | --- | --- |\n| A1 | command: go test ./x | pending | |\n"
	}
	taskPath := func(id string) string { return filepath.Join(vault, "work", "tasks", id+".md") }
	writeDirectTaskBody(t, vault, "APP-T-0001", "", nil, bodyWithProof("APP-T-0001"))
	writeDirectTask(t, vault, "APP-T-0002", "", nil)
	writeDirectTaskBody(t, vault, "APP-T-0003", "", nil, bodyWithProof("APP-T-0003"))
	writeDirectTaskBody(t, vault, "APP-T-0004", "", nil, bodyWithProof("APP-T-0004"))

	// The committed revision is the trusted historical body for a rebase:
	// snapshot each record's authored bytes, then rewind pins out of band.
	committed := map[string][]byte{}
	snapshot := func(id string) {
		raw, err := os.ReadFile(taskPath(id))
		if err != nil {
			t.Fatal(err)
		}
		committed[taskPath(id)] = raw
	}
	snapshot("APP-T-0001")
	snapshot("APP-T-0003")
	// APP-T-0004 gets no committed receipt at all.
	oldLoader := v7CommittedObjectBytes
	v7CommittedObjectBytes = func(_ string, absPath string) ([]byte, bool) {
		raw, ok := committed[absPath]
		return raw, ok
	}
	defer func() { v7CommittedObjectBytes = oldLoader }()

	// APP-T-0001, -0003, -0004 rewind to pins written under the superseded
	// truncated canon; APP-T-0002 gets a pin matching no known algorithm.
	for _, id := range []string{"APP-T-0001", "APP-T-0003", "APP-T-0004"} {
		writeTaskFileOutOfBand(t, vault, id, func(d map[string]any, b string) (map[string]any, string) {
			d["contract_fingerprint"] = legacyDirectWaveTaskContractFingerprint(d, b)
			d["state_rev"] = v7StateRev(d, b)
			return d, b
		})
	}
	writeTaskFileOutOfBand(t, vault, "APP-T-0002", func(d map[string]any, b string) (map[string]any, string) {
		d["contract_fingerprint"] = "sha256:deadbeef"
		d["state_rev"] = v7StateRev(d, b)
		return d, b
	})
	// Tamper with APP-T-0003's authored Proof cell after pinning. The legacy
	// canon cannot see this column, so the stored pin still matches the legacy
	// canon of the tampered bytes; only the committed receipt exposes it.
	writeTaskFileOutOfBand(t, vault, "APP-T-0003", func(d map[string]any, b string) (map[string]any, string) {
		d["state_rev"] = v7StateRev(d, b)
		return d, strings.Replace(b, "| A1 | Works. | show the log |", "| A1 | Works. | forged proof |", 1)
	})

	rebased, foreign, err := reconcileV7ContractFingerprints(vault)
	if err != nil {
		t.Fatal(err)
	}
	if rebased != 1 || foreign != 3 {
		t.Fatalf("rebased=%d foreign=%d", rebased, foreign)
	}
	idx, err := loadV7Index(vault)
	if err != nil {
		t.Fatal(err)
	}
	task1 := idx.Tasks["APP-T-0001"]
	if stored := stringField(task1.Data, "contract_fingerprint"); stored != directWaveTaskContractFingerprint(task1.Data, task1.Body) {
		t.Fatalf("era pin was not rebased to the current canon: %q", stored)
	}
	if reason := directWaveTaskContractStaleReason(task1); reason != "" {
		t.Fatalf("rebased task still reads stale: %q", reason)
	}
	task2 := idx.Tasks["APP-T-0002"]
	if stored := stringField(task2.Data, "contract_fingerprint"); stored != "sha256:deadbeef" {
		t.Fatalf("foreign pin was rewritten: %q", stored)
	}
	if reason := directWaveTaskContractStaleReason(task2); !strings.Contains(reason, "contract drifted") {
		t.Fatalf("foreign pin did not surface the stale reason: %q", reason)
	}
	// The tampered Proof cell was invisible to the legacy pin but visible to
	// the committed-material receipt: APP-T-0003 must stay flagged.
	task3 := idx.Tasks["APP-T-0003"]
	if stored := stringField(task3.Data, "contract_fingerprint"); stored == directWaveTaskContractFingerprint(task3.Data, task3.Body) {
		t.Fatalf("tampered record was certified by rebase")
	}
	if reason := directWaveTaskContractStaleReason(task3); !strings.Contains(reason, "contract drifted") {
		t.Fatalf("tampered record did not stay stale: %q", reason)
	}
	// APP-T-0004 has a verifiable-era pin but no trusted historical body.
	task4 := idx.Tasks["APP-T-0004"]
	if stored := stringField(task4.Data, "contract_fingerprint"); stored == directWaveTaskContractFingerprint(task4.Data, task4.Body) {
		t.Fatalf("unreceipted record was certified by rebase")
	}
	if reason := directWaveTaskContractStaleReason(task4); !strings.Contains(reason, "contract drifted") {
		t.Fatalf("unreceipted record did not stay stale: %q", reason)
	}
	// The surviving rebase was committed with its audit event atomically.
	matches, err := filepath.Glob(filepath.Join(vault, "events", "*", "*", "APP-T-0001--*.json"))
	if err != nil {
		t.Fatal(err)
	}
	foundRebaseEvent := false
	for _, path := range matches {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var ev map[string]any
		if err := json.Unmarshal(raw, &ev); err != nil {
			continue
		}
		payload, _ := ev["payload"].(map[string]any)
		if payload["source"] == "contract_fingerprint_rebase" && payload["contract_fingerprint"] == stringField(task1.Data, "contract_fingerprint") {
			foundRebaseEvent = true
		}
	}
	if !foundRebaseEvent {
		t.Fatalf("rebase committed without its audit event")
	}
}

func TestDirectWaveReconcileRebaseCommitsTaskAndEventAtomically(t *testing.T) {
	vault, _, _ := authorityFixture(t)
	writeDirectTaskBody(t, vault, "APP-T-0001", "", nil, "# APP-T-0001\n\n## Intent\n\nDo it.\n\n## Acceptance\n\n| ID | Outcome | Proof |\n| --- | --- | --- |\n| A1 | Works. | show the log |\n\n## Verification\n\n| Covers | Check | Result | Notes |\n| --- | --- | --- | --- |\n| A1 | command: go test ./x | pending | |\n")
	taskPath := filepath.Join(vault, "work", "tasks", "APP-T-0001.md")
	committedRaw, err := os.ReadFile(taskPath)
	if err != nil {
		t.Fatal(err)
	}
	oldLoader := v7CommittedObjectBytes
	v7CommittedObjectBytes = func(_ string, absPath string) ([]byte, bool) {
		if absPath == taskPath {
			return committedRaw, true
		}
		return nil, false
	}
	defer func() { v7CommittedObjectBytes = oldLoader }()
	var legacyPin string
	writeTaskFileOutOfBand(t, vault, "APP-T-0001", func(d map[string]any, b string) (map[string]any, string) {
		legacyPin = legacyDirectWaveTaskContractFingerprint(d, b)
		d["contract_fingerprint"] = legacyPin
		d["state_rev"] = v7StateRev(d, b)
		return d, b
	})
	// Abort the transaction after its first write: the surviving document must
	// be restored and no audit event may remain.
	oldFailAfter := v7ContractRebaseInjectCommitFailAfter
	v7ContractRebaseInjectCommitFailAfter = 1
	defer func() { v7ContractRebaseInjectCommitFailAfter = oldFailAfter }()
	rebased, foreign, err := reconcileV7ContractFingerprints(vault)
	if err == nil {
		t.Fatalf("injected commit failure did not surface")
	}
	data, _, err := parseFrontmatterMustRead(taskPath)
	if err != nil {
		t.Fatal(err)
	}
	if stored := stringField(data, "contract_fingerprint"); stored != legacyPin {
		t.Fatalf("aborted rebase left the new pin behind: %q", stored)
	}
	matches, err := filepath.Glob(filepath.Join(vault, "events", "*", "*", "APP-T-0001--*.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range matches {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), "contract_fingerprint_rebase") {
			t.Fatalf("aborted rebase left its audit event behind: %s", path)
		}
	}
	_ = rebased
	_ = foreign
}

func TestDirectWaveReconcileStateRevRepairCommitsTaskAndEventAtomically(t *testing.T) {
	vault, _, _ := authorityFixture(t)
	taskPath := writeDirectTask(t, vault, "APP-T-0001", "", nil)
	writeTaskFileOutOfBand(t, vault, "APP-T-0001", func(data map[string]any, body string) (map[string]any, string) {
		return data, body + "\n## Out-of-band detail\n\nThe stored revision must remain stale until an audited repair commits.\n"
	})
	before, err := os.ReadFile(taskPath)
	if err != nil {
		t.Fatal(err)
	}
	idx, err := loadV7Index(vault)
	if err != nil {
		t.Fatal(err)
	}
	oldFailAfter := v7StateRevRepairInjectCommitFailAfter
	v7StateRevRepairInjectCommitFailAfter = 2 // event and task writes both roll back.
	defer func() { v7StateRevRepairInjectCommitFailAfter = oldFailAfter }()
	if _, err := repairV7ObjectStateRev(vault, idx.Tasks["APP-T-0001"], v7FrontmatterOrder["task"]); err == nil {
		t.Fatal("injected state-revision repair failure did not surface")
	}
	after, err := os.ReadFile(taskPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("aborted state-revision repair changed the task")
	}
	matches, err := filepath.Glob(filepath.Join(vault, "events", "*", "*", "APP-T-0001--*.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range matches {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), "state_rev_repair") {
			t.Fatalf("aborted state-revision repair left its audit event behind: %s", path)
		}
	}
}

func TestDirectWaveStaleDoneMemberDoesNotCompleteWave(t *testing.T) {
	vault, store, project := authorityFixture(t)
	writeDirectTask(t, vault, "APP-T-0001", "W-0001", map[string]any{"status": "done"})
	writeDirectTask(t, vault, "APP-T-0002", "W-0001", map[string]any{"status": "done"})
	writeDirectWave(t, vault, "W-0001", []string{"APP-T-0001", "APP-T-0002"}, nil)
	review, err := buildDirectWaveReview(vault, store, project.ProjectID, "W-0001", nil)
	if err != nil {
		t.Fatal(err)
	}
	if review.State != "Completed" {
		t.Fatalf("baseline wave with all done members should be Completed, got %q", review.State)
	}
	// An out-of-band authored-contract edit on a done member must dominate its
	// lifecycle status: the wave cannot certify completion over stale bytes.
	writeTaskFileOutOfBand(t, vault, "APP-T-0001", func(d map[string]any, b string) (map[string]any, string) {
		return d, strings.Replace(b, "The named behavior is observable in the changed files.", "The named behavior changed out of band.", 1)
	})
	review, err = buildDirectWaveReview(vault, store, project.ProjectID, "W-0001", nil)
	if err != nil {
		t.Fatal(err)
	}
	if review.State == "Completed" {
		t.Fatalf("wave reported Completed with a stale member contract")
	}
	staleBlocker := false
	for _, blocker := range review.Blockers {
		if blocker.Code == "CONTRACT_FINGERPRINT_STALE" && blocker.TaskID == "APP-T-0001" {
			staleBlocker = true
		}
	}
	if !staleBlocker {
		t.Fatalf("stale done member did not record CONTRACT_FINGERPRINT_STALE")
	}
	for _, member := range review.Members {
		if member.TaskID == "APP-T-0001" && member.State == "completed" {
			t.Fatalf("stale member still reported completed")
		}
	}
}
