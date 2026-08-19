package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestV7InlineProofClosesWithoutEvidenceFile(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	mustV7Proof(t, Args{"vault": vault, "quiet": "true"}, bootstrap)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App", "summary": "Proof policy.", "v7": "true"}, newV7Epic)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Inline proof close", "risk": "low", "priority": "p2", "proof-mode": "inline", "v7": "true"}, newV7Task)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "runner": "codex"}, attemptV7StartCmd)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "_pos1": "APP-T-0001", "covers": "A1", "check": "go test ./cmd/tusker -run TestProof -count=1", "result": "pass", "note": "Focused proof passed."}, v7TestVerificationMutation)

	if err := finishV7Cmd(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "attempt": "APP-T-0001-A-0001", "summary": "Implementation complete.", "local": "true"}); err != nil {
		t.Fatal(err)
	}
	taskPath := filepath.Join(vault, "work", "tasks", "APP-T-0001.md")
	data, body, err := parseFrontmatterMustRead(taskPath)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "review", stringField(data, "status"), "finish moved task to review")
	assertEqual(t, "satisfied", stringField(data, "proof_status"), "proof status")
	if !strings.Contains(body, "| A1 | go test ./cmd/tusker -run TestProof -count=1 | pass | Focused proof passed. |") {
		t.Fatalf("verification row missing:\n%s", body)
	}
	if dirExists(filepath.Join(vault, "evidence", "APP-T-0001")) {
		t.Fatal("inline proof should not create an evidence directory")
	}
	if err := closeV7Cmd(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "by": "reviewer:agent", "reason": "inline proof accepted", "local": "true"}); err != nil {
		t.Fatal(err)
	}
	data, _, err = parseFrontmatterMustRead(taskPath)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "done", stringField(data, "status"), "closed status")
}

func TestV7VerifyAddParsesEscapedPipeCheck(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	mustV7Proof(t, Args{"vault": vault, "quiet": "true"}, bootstrap)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App", "summary": "Proof policy.", "v7": "true"}, newV7Epic)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Escaped pipe proof", "risk": "low", "priority": "p2", "proof-mode": "inline", "v7": "true"}, newV7Task)

	check := "command: go test ./... | tee /tmp/proof.log"
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "_pos1": "APP-T-0001", "covers": "A1", "check": check, "result": "pending", "note": "Gate execution required."}, v7TestVerificationMutation)

	taskPath := filepath.Join(vault, "work", "tasks", "APP-T-0001.md")
	data, body, err := parseFrontmatterMustRead(taskPath)
	if err != nil {
		t.Fatal(err)
	}
	rows := parseV7VerificationRows(body)
	if len(rows) != 1 {
		t.Fatalf("expected one verification row, got %#v", rows)
	}
	assertEqual(t, check, rows[0].Check, "verification check round-trip")
	assertEqual(t, "pending", rows[0].Result, "verification result")
	assertEqual(t, "pending", stringField(data, "proof_status"), "proof status")
}

func TestV7VerifyRemoveByIndexUsesCASAndEmitsCleanTable(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	mustV7Proof(t, Args{"vault": vault, "quiet": "true"}, bootstrap)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App", "summary": "Proof policy.", "v7": "true"}, newV7Epic)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Remove proof row", "risk": "low", "priority": "p2", "proof-mode": "inline", "v7": "true"}, newV7Task)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "_pos1": "APP-T-0001", "covers": "A1", "check": "command: go test ./cmd/tusker -run TestKeep -count=1", "result": "pending", "note": "drop note"}, v7TestVerificationMutation)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "_pos1": "APP-T-0001", "covers": "A1", "check": "command: go test ./cmd/tusker -run TestDrop -count=1", "result": "pending"}, v7TestVerificationMutation)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "_pos1": "APP-T-0001", "index": "1", "by": "agent:luna"}, verifyV7RemoveCmd)

	_, body, err := parseFrontmatterMustRead(filepath.Join(vault, "work", "tasks", "APP-T-0001.md"))
	if err != nil {
		t.Fatal(err)
	}
	rows := parseV7VerificationRows(body)
	if len(rows) != 1 || rows[0].Check != "command: go test ./cmd/tusker -run TestDrop -count=1" {
		t.Fatalf("remove left wrong verification rows: %#v", rows)
	}
	if eventErrs, _, _ := validateV7Events(vault); len(eventErrs) != 0 {
		t.Fatalf("remove emitted invalid event: %#v", eventErrs)
	}
	var removedEvent map[string]any
	if err := filepath.WalkDir(filepath.Join(vault, "events"), func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".json") {
			return err
		}
		raw, err := readText(path)
		if err != nil {
			return err
		}
		var event map[string]any
		if err := json.Unmarshal([]byte(raw), &event); err != nil {
			return err
		}
		if stringField(event, "event_kind") == "verification_removed" {
			removedEvent = event
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	payload, _ := removedEvent["payload"].(map[string]any)
	removedIndex, indexOK := payload["index"].(float64)
	if stringField(removedEvent, "event_kind") != "verification_removed" ||
		!indexOK || int(removedIndex) != 1 ||
		stringField(payload, "covers") != "A1" ||
		stringField(payload, "check") != "command: go test ./cmd/tusker -run TestKeep -count=1" ||
		stringField(payload, "result") != "pending" ||
		stringField(payload, "notes") != "drop note" ||
		stringField(payload, "blocked_by") != "" {
		t.Fatalf("remove event omitted complete row payload: %#v", removedEvent)
	}
}

func TestV7VerifyRemoveRejectsTraversalBeforeLock(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	mustV7Proof(t, Args{"vault": vault, "quiet": "true"}, bootstrap)
	err := verifyV7RemoveCmd(Args{"vault": vault, "quiet": "true", "_pos1": "../../outside", "index": "1"})
	if err == nil || errorToIssue(err).Code != errorIDScheme {
		t.Fatalf("expected task-id scheme refusal before lock path derivation, got %v", err)
	}
	if matches, _ := filepath.Glob(filepath.Join(vault, "locks", "proof-*")); len(matches) != 0 {
		t.Fatalf("traversal refusal created proof lock(s): %v", matches)
	}
}

func TestV7VerifyRemoveRejectsForgedActors(t *testing.T) {
	clearAgentSessionEnvForTest(t)
	vault := filepath.Join(t.TempDir(), "vault")
	mustV7Proof(t, Args{"vault": vault, "quiet": "true"}, bootstrap)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App", "summary": "Proof policy.", "v7": "true"}, newV7Epic)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Actor proof", "risk": "low", "priority": "p2", "proof-mode": "inline", "v7": "true"}, newV7Task)
	if _, err := upsertV7Verification(vault, "APP-T-0001", v7VerificationRow{CoverText: "A1", Check: "command: go test ./cmd/tusker -run TestKeep -count=1", Result: "pass", Notes: "Existing gate receipt."}, "reviewer:gate"); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{"human:operator", "daemon:spoof", "operator"} {
		t.Run(strings.ReplaceAll(raw, ":", "-"), func(t *testing.T) {
			t.Setenv("CODEX_THREAD_ID", "proof-session")
			err := verifyV7RemoveCmd(Args{"vault": vault, "quiet": "true", "_pos1": "APP-T-0001", "index": "1", "by": raw})
			if err == nil {
				t.Fatalf("verify remove accepted forged actor %q", raw)
			}
		})
	}
}

func TestV7ScreenshotCheckRejectsForgedActors(t *testing.T) {
	clearAgentSessionEnvForTest(t)
	vault := filepath.Join(t.TempDir(), "vault")
	mustV7Proof(t, Args{"vault": vault, "quiet": "true"}, bootstrap)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App", "summary": "Proof policy.", "v7": "true"}, newV7Epic)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Screenshot proof", "risk": "low", "priority": "p2", "proof-mode": "artifact", "v7": "true"}, newV7Task)
	for _, raw := range []string{"human:operator", "daemon:spoof", "operator"} {
		t.Run(strings.ReplaceAll(raw, ":", "-"), func(t *testing.T) {
			t.Setenv("CODEX_THREAD_ID", "screenshot-session")
			err := evidenceV7AddCmd(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "kind": "screenshot", "status": "pending_review", "checked-by": raw, "covers": "A1", "external-url": "https://example.test/proof.png"})
			if err == nil {
				t.Fatalf("screenshot evidence accepted forged checker %q", raw)
			}
		})
	}
}

func TestV7ProofMutationsRejectForgedActors(t *testing.T) {
	clearAgentSessionEnvForTest(t)
	vault := filepath.Join(t.TempDir(), "vault")
	mustV7Proof(t, Args{"vault": vault, "quiet": "true"}, bootstrap)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App", "summary": "Proof policy.", "v7": "true"}, newV7Epic)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Proof actor", "risk": "low", "priority": "p2", "proof-mode": "inline", "v7": "true"}, newV7Task)
	for _, raw := range []string{"human:operator", "daemon:spoof", "operator"} {
		t.Run("set-mode/"+strings.ReplaceAll(raw, ":", "-"), func(t *testing.T) {
			t.Setenv("CODEX_THREAD_ID", "proof-session")
			err := proofV7SetModeCmd(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "mode": "card", "by": raw})
			if err == nil {
				t.Fatalf("proof set-mode accepted forged actor %q", raw)
			}
		})
		t.Run("verify-add/"+strings.ReplaceAll(raw, ":", "-"), func(t *testing.T) {
			t.Setenv("CODEX_THREAD_ID", "proof-session")
			err := verifyV7AddCmd(Args{"vault": vault, "quiet": "true", "_pos1": "APP-T-0001", "covers": "A1", "check": "command: go test ./cmd/tusker -run TestProof -count=1", "result": "pending", "by": raw})
			if err == nil {
				t.Fatalf("verify add accepted forged actor %q", raw)
			}
		})
	}
}

func TestV7VerifyAddBlockedRecordsBlockerWithoutSatisfyingProof(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	mustV7Proof(t, Args{"vault": vault, "quiet": "true"}, bootstrap)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App", "summary": "Proof policy.", "v7": "true"}, newV7Epic)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Blocked proof", "risk": "low", "priority": "p2", "proof-mode": "inline", "proof-required": "typecheck", "v7": "true"}, newV7Task)

	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "_pos1": "APP-T-0001", "covers": "A1", "check": "go test ./cmd/tusker -run TestShared -count=1", "result": "blocked", "note": "Owned files typecheck, broad run stops elsewhere.", "blocked-by": "cmd/tusker/other_lane.go"}, v7TestVerificationMutation)

	data, body, err := parseFrontmatterMustRead(filepath.Join(vault, "work", "tasks", "APP-T-0001.md"))
	if err != nil {
		t.Fatal(err)
	}
	rows := parseV7VerificationRows(body)
	if len(rows) != 1 {
		t.Fatalf("expected one blocked row, got %#v", rows)
	}
	assertEqual(t, "blocked", rows[0].Result, "blocked result")
	assertEqual(t, "cmd/tusker/other_lane.go", rows[0].BlockedBy, "blocked attribution")
	assertEqual(t, "partial", stringField(data, "proof_status"), "blocked proof status")

	report := computeV7ProofReport(vault, mustV7Task(t, vault, "APP-T-0001"), mustIndex(t, vault))
	if report.Status == "satisfied" {
		t.Fatalf("blocked row must not satisfy proof: %#v", report)
	}
	assertEqual(t, []string{"acceptance:A1", "proof_required:typecheck"}, report.ExternalMissing, "external blocked gaps")
	if len(report.MachineMissing) != 0 {
		t.Fatalf("blocked shared-workspace proof should not be reported as owned machine gaps: %#v", report.MachineMissing)
	}
	if len(report.ExternalBlockers) != 1 || !strings.Contains(report.ExternalBlockers[0], "cmd/tusker/other_lane.go") {
		t.Fatalf("expected external blocker summary, got %#v", report.ExternalBlockers)
	}
}

func TestV7VerifyAddBlockedRequiresBlockedBy(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	mustV7Proof(t, Args{"vault": vault, "quiet": "true"}, bootstrap)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App", "summary": "Proof policy.", "v7": "true"}, newV7Epic)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Blocked proof missing attribution", "risk": "low", "priority": "p2", "v7": "true"}, newV7Task)

	err := verifyV7AddCmd(Args{"vault": vault, "quiet": "true", "_pos1": "APP-T-0001", "covers": "A1", "check": "go test ./...", "result": "blocked", "note": "Blocked elsewhere."})
	if err == nil || !strings.Contains(err.Error(), "--blocked-by") {
		t.Fatalf("expected blocked attribution error, got %v", err)
	}
}

func TestV7VerifyAddPrintsRemainingProofGaps(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	mustV7Proof(t, Args{"vault": vault, "quiet": "true"}, bootstrap)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App", "summary": "Proof policy.", "v7": "true"}, newV7Epic)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Partial proof", "risk": "medium", "priority": "p2", "proof-required": "focused_test,broad_test,lint", "v7": "true"}, newV7Task)

	output := captureStdout(t, func() {
		if err := verifyV7AddCmd(Args{"vault": vault, "_pos1": "APP-T-0001", "covers": "A1", "check": "command: go test ./cmd/tusker -run TestFocused -count=1", "result": "pending", "note": "Gate execution required."}); err != nil {
			t.Fatal(err)
		}
	})
	assertContainsIndexTest(t, output, "Added 1 verification row for APP-T-0001; proof_status=pending")
	assertContainsIndexTest(t, output, "Remaining proof gaps:")
}

func TestV7FinishWithoutAttemptPrintsRecoveryCommand(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	mustV7Proof(t, Args{"vault": vault, "quiet": "true"}, bootstrap)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App", "summary": "Proof policy.", "v7": "true"}, newV7Epic)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Missing attempt finish", "risk": "low", "priority": "p2", "v7": "true"}, newV7Task)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "_pos1": "APP-T-0001", "covers": "A1", "check": "go test ./cmd/tusker -run TestFinishWithoutAttempt -count=1", "result": "pass", "note": "Proof passed."}, v7TestVerificationMutation)

	err := finishV7Cmd(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "summary": "Implementation complete.", "request-review": "true"})
	issue := errorToIssue(err)
	assertEqual(t, errorNotFound, issue.Code, "missing attempt code")
	assertContainsIndexTest(t, issue.Message, "No attempt exists for APP-T-0001")
	assertContainsIndexTest(t, issue.Hint, "attempts are runtime/session state")
	assertContainsIndexTest(t, issue.Hint, "tusker attempt start APP-T-0001")
	assertContainsIndexTest(t, issue.Hint, "tusker finish APP-T-0001 --request-review")
}

func TestV7VerifyAddBatchRows(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	mustV7Proof(t, Args{"vault": vault, "quiet": "true"}, bootstrap)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App", "summary": "Proof policy.", "v7": "true"}, newV7Epic)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Batch proof", "risk": "low", "priority": "p2", "v7": "true"}, newV7Task)

	batch := strings.Join([]string{
		"A1|command: go test ./cmd/tusker -run TestFocused -count=1|pending|Gate execution required.",
		"A1|command: go test ./cmd/tusker -count=1|pending|Gate execution required.",
	}, "\n")
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "_pos1": "APP-T-0001", "rows": batch}, v7TestVerificationMutation)

	data, body, err := parseFrontmatterMustRead(filepath.Join(vault, "work", "tasks", "APP-T-0001.md"))
	if err != nil {
		t.Fatal(err)
	}
	rows := parseV7VerificationRows(body)
	if len(rows) != 2 {
		t.Fatalf("expected two batch rows, got %#v", rows)
	}
	assertEqual(t, "pending", stringField(data, "proof_status"), "batch proof status")
}

func TestV7VerifyAddBatchRowsAllowPipesInCheck(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	mustV7Proof(t, Args{"vault": vault, "quiet": "true"}, bootstrap)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App", "summary": "Proof policy.", "v7": "true"}, newV7Epic)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Batch proof with pipe", "risk": "low", "priority": "p2", "v7": "true"}, newV7Task)

	check := "command: go test ./... | tee /tmp/proof.log"
	batch := "A1|" + check + "|pending|Gate execution required."
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "_pos1": "APP-T-0001", "rows": batch}, v7TestVerificationMutation)

	_, body, err := parseFrontmatterMustRead(filepath.Join(vault, "work", "tasks", "APP-T-0001.md"))
	if err != nil {
		t.Fatal(err)
	}
	rows := parseV7VerificationRows(body)
	if len(rows) != 1 {
		t.Fatalf("expected one batch row, got %#v", rows)
	}
	assertEqual(t, check, rows[0].Check, "batch check with shell pipe")
	assertEqual(t, "pending", rows[0].Result, "batch result")
}

func TestV7VerifyAddBusyLockReturnsDeterministicRetry(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	mustV7Proof(t, Args{"vault": vault, "quiet": "true"}, bootstrap)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App", "summary": "Proof policy.", "v7": "true"}, newV7Epic)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Locked proof", "risk": "low", "priority": "p2", "v7": "true"}, newV7Task)
	release, err := acquireV7ProofWriteLock(vault, "APP-T-0001", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	err = verifyV7AddCmd(Args{"vault": vault, "quiet": "true", "_pos1": "APP-T-0001", "covers": "A1", "check": "command: go test ./cmd/tusker -run TestLocked -count=1", "result": "pending", "note": "Gate execution required.", "lock-timeout-ms": "1"})
	issue := errorToIssue(err)
	assertEqual(t, "PROOF_WRITE_BUSY", issue.Code, "busy proof lock code")
	for _, want := range []string{"retry exactly:", "tusker verify add APP-T-0001", "--covers A1", "--check 'command: go test ./cmd/tusker -run TestLocked -count=1'", "--result pending", "--note 'Gate execution required.'"} {
		if !strings.Contains(issue.Hint, want) {
			t.Fatalf("retry hint missing %q:\n%s", want, issue.Hint)
		}
	}
}

func TestV7VerifyAddCASErrorIncludesExactRetryCommand(t *testing.T) {
	row := v7VerificationRow{
		CoverText: "A1",
		Check:     "go test ./...",
		Result:    "blocked",
		Notes:     "Blocked by other lane.",
		BlockedBy: "cmd/tusker/other_lane.go",
	}
	err := v7VerificationCASError(tuskerError("CAS_CONFLICT", "stale"), "/tmp/APP-T-0001.md", "APP-T-0001", []v7VerificationRow{row}, nil)
	issue := errorToIssue(err)
	assertEqual(t, "CAS_CONFLICT", issue.Code, "CAS retry code")
	want := `retry exactly: tusker verify add APP-T-0001 --covers A1 --check 'go test ./...' --result blocked --note 'Blocked by other lane.' --blocked-by cmd/tusker/other_lane.go`
	if issue.Hint != want {
		t.Fatalf("unexpected retry hint:\nwant: %s\ngot:  %s", want, issue.Hint)
	}
}

func TestV7ShellQuoteUsesSingleQuotesForExpansions(t *testing.T) {
	got := v7ShellCommand([]string{"tusker", "verify", "add", "APP-T-0001", "--check", "echo $TOKEN && `whoami`"})
	want := "tusker verify add APP-T-0001 --check 'echo $TOKEN && `whoami`'"
	if got != want {
		t.Fatalf("retry command is not shell-safe:\nwant: %s\ngot:  %s", want, got)
	}
}

func TestV7ProofModeNoneSkipsGeneratedAcceptance(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	mustV7Proof(t, Args{"vault": vault, "quiet": "true"}, bootstrap)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App", "summary": "Proof policy.", "v7": "true"}, newV7Epic)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Planning cleanup", "risk": "low", "priority": "p3", "proof-mode": "none", "v7": "true"}, newV7Task)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "status": "review", "by": "agent:codex", "local": "true"}, statusV7Cmd)

	if err := closeV7Cmd(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "by": "reviewer:agent", "reason": "non-executable work accepted", "local": "true"}); err != nil {
		t.Fatal(err)
	}
	data, _, err := parseFrontmatterMustRead(filepath.Join(vault, "work", "tasks", "APP-T-0001.md"))
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "done", stringField(data, "status"), "closed status")
	assertEqual(t, "satisfied", stringField(data, "proof_status"), "proof status")
}

func TestV7ProofStatusForNoneMarksAcceptanceNotRequired(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	mustV7Proof(t, Args{"vault": vault, "quiet": "true"}, bootstrap)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App", "summary": "Proof policy.", "v7": "true"}, newV7Epic)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Planning cleanup", "risk": "low", "priority": "p3", "proof-mode": "none", "v7": "true"}, newV7Task)

	output := captureStdout(t, func() {
		if err := proofV7StatusCmd(Args{"vault": vault, "id": "APP-T-0001", "verbose": "true"}); err != nil {
			t.Fatal(err)
		}
	})
	if strings.Contains(output, "pending, no proof") {
		t.Fatalf("proof status should not show pending proof for proof_mode=none:\n%s", output)
	}
	if !strings.Contains(output, "Proof status: satisfied") || !strings.Contains(output, "A1: not required") {
		t.Fatalf("expected proof_mode=none status output, got:\n%s", output)
	}
}

func TestV7ProofStatusDefaultIsConcise(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	mustV7Proof(t, Args{"vault": vault, "quiet": "true"}, bootstrap)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App", "summary": "Proof policy.", "v7": "true"}, newV7Epic)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Concise proof output", "risk": "low", "priority": "p2", "v7": "true"}, newV7Task)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "blocks": "APP-T-0001", "kind": "signoff", "owner": "human:sarav", "action": "Sign off on acceptance.", "verification": "Human signoff recorded for A1.", "covers": "A1", "why-agent-cannot": "Final human signoff is required by this proof policy."}, newV7Gate)

	output := captureStdout(t, func() {
		if err := proofV7StatusCmd(Args{"vault": vault, "id": "APP-T-0001"}); err != nil {
			t.Fatal(err)
		}
	})
	for _, want := range []string{
		"Task: APP-T-0001",
		"Proof mode: inline",
		"Proof status: partial",
		"Missing gaps:",
		"  machine:",
		"    - proof_required:focused_test",
		"    - proof_required:broad_test",
		"  human:",
		"    - acceptance:A1",
		"Agent action: continue",
		"Evidence summary:",
		"  inline rows: 1",
		"  evidence records: 0",
		"Open gates:",
		"    - APP-G-0001",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("concise output missing %q:\n%s", want, output)
		}
	}
	for _, forbidden := range []string{"Acceptance coverage:", "A1: pending, no proof", "Inline verification:\n", "Evidence:\n"} {
		if strings.Contains(output, forbidden) {
			t.Fatalf("concise output should not contain %q:\n%s", forbidden, output)
		}
	}

	verbose := captureStdout(t, func() {
		if err := proofV7StatusCmd(Args{"vault": vault, "id": "APP-T-0001", "verbose": "true"}); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(verbose, "Acceptance coverage:") || !strings.Contains(verbose, "A1: pending, no proof") {
		t.Fatalf("verbose output should include acceptance coverage matrix:\n%s", verbose)
	}
}

func TestV7HighRiskDefaultsToInlineCodeProof(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	mustV7Proof(t, Args{"vault": vault, "quiet": "true"}, bootstrap)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App", "summary": "Proof policy.", "v7": "true"}, newV7Epic)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Backend high-risk task", "risk": "high", "priority": "p1", "v7": "true"}, newV7Task)

	data, _, err := parseFrontmatterMustRead(filepath.Join(vault, "work", "tasks", "APP-T-0001.md"))
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "inline", stringField(data, "proof_mode"), "proof mode")
	assertEqual(t, []string{"focused_test", "broad_test"}, normalizeList(data["proof_required"]), "proof required")
	assertEqual(t, 0, intField(data, "evidence_budget"), "evidence budget")
	if containsString(normalizeList(data["proof_required"]), "manual_smoke") || containsString(normalizeList(data["proof_required"]), "screenshot") {
		t.Fatalf("high-risk default should not require manual/screenshot proof: %#v", data["proof_required"])
	}
}

func TestV7ArtifactFinishRequiresEvidenceOrGate(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	mustV7Proof(t, Args{"vault": vault, "quiet": "true"}, bootstrap)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App", "summary": "Proof policy.", "v7": "true"}, newV7Epic)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Artifact proof", "risk": "high", "priority": "p1", "proof-mode": "artifact", "v7": "true"}, newV7Task)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "runner": "codex"}, attemptV7StartCmd)

	err := finishV7Cmd(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "attempt": "APP-T-0001-A-0001", "summary": "Implementation complete.", "local": "true"})
	if err == nil || !strings.Contains(err.Error(), "finish proof incomplete") {
		t.Fatalf("expected finish proof error, got %v", err)
	}

	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "blocks": "APP-T-0001", "kind": "env", "owner": "human:sarav", "action": "Use the unavailable physical device to capture artifact proof.", "verification": "The device capture is attached for A1.", "covers": "A1", "why-agent-cannot": "The required physical device is unavailable to the agent."}, newV7Gate)
	if err := finishV7Cmd(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "attempt": "APP-T-0001-A-0001", "summary": "Implementation complete; manual proof gated.", "local": "true"}); err != nil {
		t.Fatal(err)
	}
	data, _, err := parseFrontmatterMustRead(filepath.Join(vault, "work", "tasks", "APP-T-0001.md"))
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "waiting_on_human", stringField(data, "readiness"), "artifact task remains gated")
	assertEqual(t, "partial", stringField(data, "proof_status"), "proof status with open gate")
}

func TestV7AttemptHandoffRequiresProofBeforeReview(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	mustV7Proof(t, Args{"vault": vault, "quiet": "true"}, bootstrap)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App", "summary": "Proof policy.", "v7": "true"}, newV7Epic)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Incomplete handoff", "risk": "low", "priority": "p2", "status": "ready", "readiness": "ready", "force-ready": "true", "v7": "true"}, newV7Task)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "runner": "codex"}, attemptV7StartCmd)

	err := attemptV7HandoffCmd(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "summary": "Implementation claims ready."})
	if err == nil || !strings.Contains(err.Error(), "finish proof incomplete") {
		t.Fatalf("expected handoff proof error, got %v", err)
	}
	taskData, _, err := parseFrontmatterMustRead(filepath.Join(vault, "work", "tasks", "APP-T-0001.md"))
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "ready", stringField(taskData, "status"), "task status after rejected handoff")
	attemptData, _, err := parseFrontmatterMustRead(filepath.Join(vault, "attempts", "APP-T-0001", "APP-T-0001-A-0001.md"))
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "started", stringField(attemptData, "status"), "attempt status after rejected handoff")
}

func TestV7ValidateRejectsReviewAfterHandoffWithIncompleteProof(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	mustV7Proof(t, Args{"vault": vault, "quiet": "true"}, bootstrap)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App", "summary": "Proof policy.", "v7": "true"}, newV7Epic)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Corrupted review", "risk": "low", "priority": "p2", "v7": "true"}, newV7Task)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "runner": "codex"}, attemptV7StartCmd)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "status": "review", "by": "agent:codex", "local": "true"}, statusV7Cmd)

	attemptPath := filepath.Join(vault, "attempts", "APP-T-0001", "APP-T-0001-A-0001.md")
	data, body, err := parseFrontmatterMustRead(attemptPath)
	if err != nil {
		t.Fatal(err)
	}
	baseRev := stringField(data, "state_rev")
	data["status"] = "handoff"
	if _, err := saveV7DocumentCAS(attemptPath, data, body, v7FrontmatterOrder["attempt"], baseRev); err != nil {
		t.Fatal(err)
	}

	code, err := validateCmd(Args{"vault": vault, "json": "true"})
	if err != nil {
		t.Fatal(err)
	}
	if code == 0 {
		t.Fatal("expected validate to reject handoff-backed review with incomplete proof")
	}
}

func TestV7ValidateAllowsHandoffWaitingOnUnresolvedDependency(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	mustV7Proof(t, Args{"vault": vault, "quiet": "true"}, bootstrap)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App", "summary": "Proof policy.", "v7": "true"}, newV7Epic)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Dependency", "risk": "low", "priority": "p2", "proof-mode": "none", "v7": "true"}, newV7Task)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Blocked handoff", "risk": "low", "priority": "p2", "dependencies": "APP-T-0001", "status": "ready", "readiness": "ready", "force-ready": "true", "v7": "true"}, newV7Task)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true"}, reconcileV7Cmd)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "_pos1": "APP-T-0002", "covers": "A1", "check": "go test ./cmd/tusker -run TestV7ValidateAllowsHandoff -count=1", "result": "pass", "note": "Dependent work proof passed."}, v7TestVerificationMutation)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "id": "APP-T-0002", "runner": "codex"}, attemptV7StartCmd)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "id": "APP-T-0002", "summary": "Implementation done; dependency still open."}, attemptV7HandoffCmd)

	data, _, err := parseFrontmatterMustRead(filepath.Join(vault, "work", "tasks", "APP-T-0002.md"))
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "ready", stringField(data, "status"), "blocked task status")
	assertEqual(t, "blocked_by_dependency", stringField(data, "readiness"), "blocked task readiness")
	code, err := validateCmd(Args{"vault": vault, "json": "true"})
	if err != nil {
		t.Fatal(err)
	}
	if code != 0 {
		t.Fatal("expected dependency-blocked handoff to validate")
	}
}

func TestV7ProofRequiredClassesAreEnforced(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	mustV7Proof(t, Args{"vault": vault, "quiet": "true"}, bootstrap)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App", "summary": "Proof policy.", "v7": "true"}, newV7Epic)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Normative proof classes", "risk": "high", "priority": "p1", "proof-mode": "artifact", "proof-required": "screenshot,human_signoff", "v7": "true"}, newV7Task)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "runner": "codex"}, attemptV7StartCmd)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "kind": "video", "status": "accepted", "accepted-by": "reviewer:agent", "covers": "A1", "external-url": "https://example.test/proof.mov", "summary": "Video artifact accepted."}, evidenceV7AddCmd)

	err := finishV7Cmd(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "attempt": "APP-T-0001-A-0001", "summary": "Implementation complete.", "local": "true"})
	if err == nil {
		t.Fatal("expected finish to reject missing required proof classes")
	}
	if !strings.Contains(err.Error(), "proof_required:screenshot") || !strings.Contains(err.Error(), "proof_required:human_signoff") {
		t.Fatalf("expected missing proof_required classes, got %v", err)
	}

	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "kind": "screenshot", "status": "accepted", "checked-by": "human:sarav", "covers": "A1", "external-url": "https://example.test/proof.png", "summary": "Screenshot checked."}, evidenceV7AddCmd)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "kind": "human_review", "status": "accepted", "accepted-by": "human:sarav", "covers": "A1", "summary": "Human signoff complete."}, evidenceV7AddCmd)

	if err := finishV7Cmd(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "attempt": "APP-T-0001-A-0001", "summary": "Implementation complete.", "local": "true"}); err != nil {
		t.Fatal(err)
	}
	data, _, err := parseFrontmatterMustRead(filepath.Join(vault, "work", "tasks", "APP-T-0001.md"))
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "review", stringField(data, "status"), "finish moved task to review")
	assertEqual(t, "satisfied", stringField(data, "proof_status"), "proof status")
}

func TestV7ProofMatchingRejectsKeywordTheater(t *testing.T) {
	if v7EvidenceSatisfiesProofRequired("focused_test", Note{Data: map[string]any{"evidence_kind": "verification_summary"}, Body: "automated test passed"}) {
		t.Fatal("summary text must not masquerade as typed focused_test evidence")
	}
	if !v7EvidenceSatisfiesProofRequired("focused_test", Note{Data: map[string]any{"evidence_kind": "automated_test"}}) {
		t.Fatal("typed automated_test evidence should satisfy focused_test")
	}
	if v7GateTextSatisfiesProofRequirement("manual_smoke", Note{Data: map[string]any{"verification": "This text mentions manual smoke but does not record its result."}}) {
		t.Fatal("unanchored gate prose must not satisfy manual_smoke")
	}
	if !v7GateTextSatisfiesProofRequirement("manual_smoke", Note{Data: map[string]any{"verification": "Manual smoke passed."}}) {
		t.Fatal("anchored gate verification should satisfy manual_smoke")
	}
	vault := filepath.Join(t.TempDir(), "vault")
	mustV7Proof(t, Args{"vault": vault, "quiet": "true"}, bootstrap)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App", "summary": "Proof policy.", "v7": "true"}, newV7Epic)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Structured proof", "risk": "low", "priority": "p2", "proof-mode": "inline", "proof-required": "focused_test", "v7": "true"}, newV7Task)

	// The note and command text mention a test, but the command only prints it.
	if _, err := upsertV7Verification(vault, "APP-T-0001", v7VerificationRow{CoverText: "A1", Check: "command: echo 'go test ./cmd/tusker -run TestWrong -count=1'", Result: "pass", Notes: "Existing gate receipt."}, "reviewer:gate"); err != nil {
		t.Fatal(err)
	}
	report := computeV7ProofReport(vault, mustV7Task(t, vault, "APP-T-0001"), mustIndex(t, vault))
	if !containsString(report.MachineMissing, "proof_required:focused_test") {
		t.Fatalf("keyword-only evidence must not satisfy focused_test: %#v", report)
	}

	if _, err := upsertV7Verification(vault, "APP-T-0001", v7VerificationRow{CoverText: "A1", Check: "command: go test ./cmd/tusker -run TestV7ProofMatchingRejectsKeywordTheater -count=1", Result: "pass", Notes: "Existing gate receipt."}, "reviewer:gate"); err != nil {
		t.Fatal(err)
	}
	report = computeV7ProofReport(vault, mustV7Task(t, vault, "APP-T-0001"), mustIndex(t, vault))
	if containsString(report.MachineMissing, "proof_required:focused_test") {
		t.Fatalf("an actual test command should satisfy focused_test: %#v", report)
	}
}

func TestV7ProofCommandMatcherHandlesWrappedAndPositionedCommands(t *testing.T) {
	tests := []struct {
		required string
		command  string
		want     bool
	}{
		{"focused_test", `rtk proxy go test ./cmd/tusker -run TestX`, true},
		{"focused_test", `make -j4 test`, true},
		{"focused_test", `npm run test:unit`, true},
		{"focused_test", `timeout 600 go test ./...`, true},
		{"focused_test", `tusker note --body "did X; go test passed"`, false},
		{"focused_test", `echo go test ./...`, false},
		{"focused_test", `go test-helper ./...`, false},
		{"focused_test", `gotest ./...`, false},
		{"build", `cargo test`, true},
		{"build", `tsc --noEmit`, true},
		{"lint", `npm --prefix web run lint:strict`, true},
		{"lint", `npx eslint web`, true},
		{"lint", `cargo clippy`, true},
		{"typecheck", `npx tsc --noEmit`, true},
		{"benchmark", `go test -run TestX -bench=BenchmarkX ./...`, true},
		{"benchmark", `cargo bench`, true},
	}
	for _, test := range tests {
		row := v7VerificationRow{Check: "command: " + test.command}
		if got := v7InlineVerificationSatisfies(test.required, row); got != test.want {
			t.Errorf("%s %q: got %v, want %v", test.required, test.command, got, test.want)
		}
	}
}

func TestV7AuditProofIsSatisfiableWithTypedReviewEvidence(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	mustV7Proof(t, Args{"vault": vault, "quiet": "true"}, bootstrap)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App", "summary": "Proof policy.", "v7": "true"}, newV7Epic)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Audited proof", "risk": "critical", "priority": "p1", "v7": "true"}, newV7Task)
	for _, row := range []v7VerificationRow{
		{CoverText: "A1", Check: "command: go test ./cmd/tusker -run TestV7AuditProofIsSatisfiableWithTypedReviewEvidence -count=1", Result: "pass", Notes: "Existing focused gate receipt."},
		{CoverText: "A1", Check: "command: go test ./cmd/tusker -count=1", Result: "pass", Notes: "Existing broad gate receipt."},
	} {
		if _, err := upsertV7Verification(vault, "APP-T-0001", row, "reviewer:gate"); err != nil {
			t.Fatal(err)
		}
	}
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "kind": "human_review", "status": "accepted", "accepted-by": "reviewer:independent", "covers": "A1", "external-url": "https://example.test/review.txt", "summary": "Independent review completed."}, evidenceV7AddCmd)

	data, _, err := parseFrontmatterMustRead(filepath.Join(vault, "work", "tasks", "APP-T-0001.md"))
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "audit", stringField(data, "proof_mode"), "audit proof mode")
	assertEqual(t, []string{"focused_test", "broad_test", "independent_review"}, normalizeList(data["proof_required"]), "audit proof requirements")
	assertEqual(t, "satisfied", stringField(data, "proof_status"), "audit proof status")
	report := computeV7ProofReport(vault, mustV7Task(t, vault, "APP-T-0001"), mustIndex(t, vault))
	if len(report.ModeMissing) != 0 || report.Status != "satisfied" {
		t.Fatalf("typed audit proof should be satisfiable: %#v", report)
	}
}

func TestV7ProofReportClassifiesHumanOnlyGapsAsTerminalWait(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	mustV7Proof(t, Args{"vault": vault, "quiet": "true"}, bootstrap)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App", "summary": "Proof policy.", "v7": "true"}, newV7Epic)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Human wait", "risk": "low", "priority": "p2", "proof-mode": "inline", "proof-required": "human_signoff", "v7": "true"}, newV7Task)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "_pos1": "APP-T-0001", "covers": "A1", "check": "go test ./cmd/tusker -run TestV7ProofReportClassifiesHumanOnlyGapsAsTerminalWait -count=1", "result": "pass", "note": "Machine proof passed."}, v7TestVerificationMutation)

	report := computeV7ProofReport(vault, mustV7Task(t, vault, "APP-T-0001"), mustIndex(t, vault))
	assertEqual(t, true, report.TerminalWait, "terminal wait")
	assertEqual(t, "stop_until_human_response", report.AgentAction, "agent action")
	assertEqual(t, 0, len(report.MachineMissing), "machine gaps")
	assertEqual(t, []string{"proof_required:human_signoff"}, report.HumanMissing, "human gaps")
}

func TestV7ProofReportClassifiesManualSmokeOwner(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	mustV7Proof(t, Args{"vault": vault, "quiet": "true"}, bootstrap)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App", "summary": "Proof policy.", "v7": "true"}, newV7Epic)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Manual smoke", "risk": "low", "priority": "p2", "proof-mode": "inline", "proof-required": "manual_smoke", "proof-required-owner": "manual_smoke=human:sarav", "v7": "true"}, newV7Task)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "_pos1": "APP-T-0001", "covers": "A1", "check": "go test ./cmd/tusker -run TestV7HumanWaitOwner -count=1", "result": "pass", "note": "Machine proof passed."}, v7TestVerificationMutation)

	report := computeV7ProofReport(vault, mustV7Task(t, vault, "APP-T-0001"), mustIndex(t, vault))
	assertEqual(t, true, report.TerminalWait, "terminal wait")
	assertEqual(t, []string{"proof_required:manual_smoke"}, report.HumanMissing, "human gaps")
}

func TestV7HumanOwnedProofRequiresHumanAcceptedArtifact(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	mustV7Proof(t, Args{"vault": vault, "quiet": "true"}, bootstrap)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App", "summary": "Proof policy.", "v7": "true"}, newV7Epic)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Human signoff", "risk": "low", "priority": "p2", "proof-mode": "inline", "proof-required": "human_signoff", "v7": "true"}, newV7Task)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "_pos1": "APP-T-0001", "covers": "A1", "check": "human signoff recorded by agent", "result": "pass", "note": "Agent summary should not satisfy human signoff."}, v7TestVerificationMutation)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "kind": "human_review", "status": "accepted", "accepted-by": "reviewer:agent", "covers": "A1", "summary": "Reviewer accepted human signoff."}, evidenceV7AddCmd)

	report := computeV7ProofReport(vault, mustV7Task(t, vault, "APP-T-0001"), mustIndex(t, vault))
	assertEqual(t, []string{"proof_required:human_signoff"}, report.HumanMissing, "human proof gap")
	if report.Status == "satisfied" {
		t.Fatalf("reviewer/agent proof must not satisfy human-owned signoff: %#v", report)
	}

	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "kind": "human_review", "status": "accepted", "accepted-by": "human:sarav", "covers": "A1", "summary": "Human accepted signoff."}, evidenceV7AddCmd)
	report = computeV7ProofReport(vault, mustV7Task(t, vault, "APP-T-0001"), mustIndex(t, vault))
	assertEqual(t, "satisfied", report.Status, "human-accepted proof status")
}

func TestV7ProofReportKeepsMachineProofMissingActionable(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	mustV7Proof(t, Args{"vault": vault, "quiet": "true"}, bootstrap)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App", "summary": "Proof policy.", "v7": "true"}, newV7Epic)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Machine gap", "risk": "low", "priority": "p2", "proof-mode": "inline", "proof-required": "focused_test", "v7": "true"}, newV7Task)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "_pos1": "APP-T-0001", "covers": "A1", "check": "manual review", "result": "pass", "note": "Acceptance covered only."}, v7TestVerificationMutation)

	report := computeV7ProofReport(vault, mustV7Task(t, vault, "APP-T-0001"), mustIndex(t, vault))
	assertEqual(t, false, report.TerminalWait, "terminal wait")
	assertEqual(t, []string{"proof_required:focused_test"}, report.MachineMissing, "machine gaps")
}

func TestV7CloseoutWritesHumanWaitCheckpoint(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	mustV7Proof(t, Args{"vault": vault, "quiet": "true"}, bootstrap)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App", "summary": "Proof policy.", "v7": "true"}, newV7Epic)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Closeout wait", "risk": "low", "priority": "p2", "proof-mode": "inline", "proof-required": "human_signoff", "v7": "true"}, newV7Task)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "_pos1": "APP-T-0001", "covers": "A1", "check": "go test ./cmd/tusker -run TestV7CloseoutWritesHumanWaitCheckpoint -count=1", "result": "pass", "note": "Machine proof passed."}, v7TestVerificationMutation)

	if err := closeoutV7Cmd(Args{"vault": vault, "quiet": "true", "_pos0": "APP-T-0001", "emit-packet": "true", "validate": "printf validation-ok"}); err != nil {
		t.Fatal(err)
	}
	assertExists(t, filepath.Join(vault, "work", "closeouts", "APP-T-0001-C-0001.md"))
	assertExists(t, filepath.Join(vault, "_generated", "packets", "APP-T-0001.reviewer.md"))
	data, _, err := parseFrontmatterMustRead(filepath.Join(vault, "work", "tasks", "APP-T-0001.md"))
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "review", stringField(data, "status"), "status")
	assertEqual(t, "waiting_on_human", stringField(data, "readiness"), "readiness")
	assertEqual(t, "stop_until_human_response", stringField(data, "agent_action"), "agent action")
	assertEqual(t, "machine_complete_waiting_for_human", stringField(data, "closeout_status"), "closeout status")
	_, latest := latestV7Closeout(mustIndex(t, vault), "APP-T-0001")
	if !latest {
		t.Fatal("expected latest closeout")
	}
}

func TestV7CloseoutStatusStopsForTerminalHumanWaitWithoutCheckpoint(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	mustV7Proof(t, Args{"vault": vault, "quiet": "true"}, bootstrap)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App", "summary": "Proof policy.", "v7": "true"}, newV7Epic)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Pending human signoff", "risk": "low", "priority": "p2", "proof-mode": "inline", "proof-required": "human_signoff", "v7": "true"}, newV7Task)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "_pos1": "APP-T-0001", "covers": "A1", "check": "go test ./cmd/tusker -run TestV7CloseoutStatusStopsForTerminalHumanWaitWithoutCheckpoint -count=1", "result": "pass", "note": "Machine proof passed."}, v7TestVerificationMutation)

	output := captureStdout(t, func() {
		if err := closeoutV7StatusCmd(Args{"vault": vault, "id": "APP-T-0001", "json": "true"}); err != nil {
			t.Fatal(err)
		}
	})
	var payload map[string]any
	if err := json.Unmarshal([]byte(output), &payload); err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "stop_until_human_response", payload["agent_action"], "agent action")
	assertEqual(t, true, payload["terminal_wait"], "terminal wait")
	assertEqual(t, true, payload["machine_complete"], "machine complete")
	assertEqual(t, true, payload["human_action_pending"], "human action pending")
	assertEqual(t, false, payload["checkpoint_exists"], "checkpoint exists")
	assertEqual(t, false, payload["checkpoint_valid"], "checkpoint valid")
	assertEqual(t, true, payload["checkpoint_needed"], "checkpoint needed")
	assertEqual(t, false, payload["packet_exists"], "packet exists")
	assertEqual(t, true, payload["packet_needed"], "packet needed")
	if got := normalizeList(payload["machine_missing"]); len(got) != 0 {
		t.Fatalf("machine gaps: expected none, got %#v", got)
	}
	if got := normalizeList(payload["reviewer_missing"]); len(got) != 0 {
		t.Fatalf("reviewer gaps: expected none, got %#v", got)
	}
	if got := normalizeList(payload["external_missing"]); len(got) != 0 {
		t.Fatalf("external gaps: expected none, got %#v", got)
	}
	assertEqual(t, []string{"proof_required:human_signoff"}, normalizeList(payload["human_missing"]), "human gaps")

	text := captureStdout(t, func() {
		if err := closeoutV7StatusCmd(Args{"vault": vault, "id": "APP-T-0001"}); err != nil {
			t.Fatal(err)
		}
	})
	for _, expected := range []string{
		"Machine work is complete for APP-T-0001.",
		"Human action pending: proof_required:human_signoff",
		"Agent action: stop_until_human_response",
		"Closeout checkpoint: needed",
		"Review packet: needed",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("expected closeout status text to contain %q, got:\n%s", expected, text)
		}
	}
}

func TestV7ValidateIgnoresSupersededCloseoutFingerprint(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	mustV7Proof(t, Args{"vault": vault, "quiet": "true"}, bootstrap)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App", "summary": "Proof policy.", "v7": "true"}, newV7Epic)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Superseded closeout", "risk": "low", "priority": "p2", "proof-mode": "inline", "proof-required": "human_signoff", "v7": "true"}, newV7Task)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "_pos1": "APP-T-0001", "covers": "A1", "check": "go test ./cmd/tusker -run TestV7ValidateIgnoresSupersededCloseoutFingerprint -count=1", "result": "pass", "note": "Machine proof passed."}, v7TestVerificationMutation)

	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "_pos0": "APP-T-0001", "emit-packet": "true", "validate": "printf validation-ok"}, closeoutV7Cmd)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "_pos0": "APP-T-0001", "emit-packet": "true", "validate": "printf validation-ok"}, closeoutV7Cmd)

	code, err := validateCmd(Args{"vault": vault, "json": "true"})
	if err != nil {
		t.Fatal(err)
	}
	if code != 0 {
		t.Fatal("expected superseded closeout fingerprints not to fail validation")
	}
}

func TestV7CloseoutRejectsRiskOnlyHumanCheckpoint(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	mustV7Proof(t, Args{"vault": vault, "quiet": "true"}, bootstrap)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App", "summary": "Proof policy.", "v7": "true"}, newV7Epic)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Human close policy", "risk": "high", "priority": "p1", "status": "review", "proof-mode": "inline", "proof-required": "focused_test", "v7": "true"}, newV7Task)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "_pos1": "APP-T-0001", "covers": "A1", "check": "go test ./cmd/tusker -run TestV7CloseoutAllowsHumanClosePolicyCheckpoint -count=1", "result": "pass", "note": "Machine proof passed."}, v7TestVerificationMutation)

	err := closeoutV7Cmd(Args{"vault": vault, "quiet": "true", "_pos0": "APP-T-0001", "emit-packet": "true", "validate": "printf validation-ok"})
	if err == nil || !strings.Contains(err.Error(), "requires human-owned pending proof, gates, or human close policy") {
		t.Fatalf("expected risk-only human checkpoint rejection, got %v", err)
	}
}

func TestV7CloseoutRequiresValidationAndPacket(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	mustV7Proof(t, Args{"vault": vault, "quiet": "true"}, bootstrap)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App", "summary": "Proof policy.", "v7": "true"}, newV7Epic)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Closeout requirements", "risk": "low", "priority": "p2", "proof-mode": "inline", "proof-required": "human_signoff", "v7": "true"}, newV7Task)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "_pos1": "APP-T-0001", "covers": "A1", "check": "go test ./cmd/tusker -run TestV7CloseoutRequiresValidationAndPacket -count=1", "result": "pass", "note": "Machine proof passed."}, v7TestVerificationMutation)

	err := closeoutV7Cmd(Args{"vault": vault, "quiet": "true", "_pos0": "APP-T-0001", "emit-packet": "true"})
	if err == nil || !strings.Contains(err.Error(), "requires --validate") {
		t.Fatalf("expected missing validation error, got %v", err)
	}
	err = closeoutV7Cmd(Args{"vault": vault, "quiet": "true", "_pos0": "APP-T-0001", "validate": "printf validation-ok"})
	if err == nil || !strings.Contains(err.Error(), "requires --emit-packet") {
		t.Fatalf("expected missing packet error, got %v", err)
	}
}

func TestV7CloseoutDoesNotAdvertiseHumanWaitWhenCheckpointWriteFails(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	mustV7Proof(t, Args{"vault": vault, "quiet": "true"}, bootstrap)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App", "summary": "Proof policy.", "v7": "true"}, newV7Epic)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Failed closeout write", "risk": "low", "priority": "p2", "proof-mode": "inline", "proof-required": "human_signoff", "v7": "true"}, newV7Task)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "_pos1": "APP-T-0001", "covers": "A1", "check": "go test ./cmd/tusker -run TestV7CloseoutDoesNotAdvertiseHumanWaitWhenCheckpointWriteFails -count=1", "result": "pass", "note": "Machine proof passed."}, v7TestVerificationMutation)
	closeoutDir := filepath.Join(vault, "work", "closeouts")
	if err := os.RemoveAll(closeoutDir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(closeoutDir, []byte("not a directory\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := closeoutV7Cmd(Args{"vault": vault, "quiet": "true", "_pos0": "APP-T-0001", "emit-packet": "true", "validate": "printf validation-ok"})
	if err == nil {
		t.Fatal("expected closeout checkpoint write to fail")
	}
	data, _, readErr := parseFrontmatterMustRead(filepath.Join(vault, "work", "tasks", "APP-T-0001.md"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	if stringField(data, "closeout_status") != "" || stringField(data, "agent_action") == "stop_until_human_response" || strings.HasPrefix(stringField(data, "next_source"), "closeout:") {
		t.Fatalf("task advertised human wait despite failed checkpoint write: %#v", data)
	}
}

func TestV7CloseoutRechecksTerminalStateAfterValidation(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	mustV7Proof(t, Args{"vault": vault, "quiet": "true"}, bootstrap)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App", "summary": "Proof policy.", "v7": "true"}, newV7Epic)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Validation side effect", "risk": "low", "priority": "p2", "proof-mode": "inline", "proof-required": "human_signoff", "v7": "true"}, newV7Task)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "_pos1": "APP-T-0001", "covers": "A1", "check": "go test ./cmd/tusker -run TestV7CloseoutRechecksTerminalStateAfterValidation -count=1", "result": "pass", "note": "Machine proof passed."}, v7TestVerificationMutation)
	mutate := `rm ` + filepath.Base(vault) + `/work/tasks/APP-T-0001.md`

	err := closeoutV7Cmd(Args{"vault": vault, "quiet": "true", "_pos0": "APP-T-0001", "emit-packet": "true", "validate": mutate})
	if err == nil || !strings.Contains(err.Error(), "not found after validation") {
		t.Fatalf("expected terminal recheck error, got %v", err)
	}
	if fileExists(filepath.Join(vault, "work", "closeouts", "APP-T-0001-C-0001.md")) {
		t.Fatal("closeout should not be written after validation changes terminal state")
	}
}

func TestV7CloseoutStatusIgnoresStaleCheckpointAgentAction(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	mustV7Proof(t, Args{"vault": vault, "quiet": "true"}, bootstrap)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App", "summary": "Proof policy.", "v7": "true"}, newV7Epic)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Stale closeout", "risk": "low", "priority": "p2", "proof-mode": "inline", "proof-required": "human_signoff", "v7": "true"}, newV7Task)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "_pos1": "APP-T-0001", "covers": "A1", "check": "go test ./cmd/tusker -run TestV7CloseoutStatusIgnoresStaleCheckpointAgentAction -count=1", "result": "pass", "note": "Machine proof passed."}, v7TestVerificationMutation)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "_pos0": "APP-T-0001", "emit-packet": "true", "validate": "printf validation-ok"}, closeoutV7Cmd)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "status": "rework", "by": "human:sarav", "reason": "Needs rework.", "local": "true"}, statusV7Cmd)

	output := captureStdout(t, func() {
		if err := closeoutV7StatusCmd(Args{"vault": vault, "id": "APP-T-0001", "json": "true"}); err != nil {
			t.Fatal(err)
		}
	})
	var payload map[string]any
	if err := json.Unmarshal([]byte(output), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["agent_action"] == "stop_until_human_response" {
		t.Fatalf("stale closeout status must not report terminal agent_action: %#v", payload)
	}
	if payload["fingerprint_matches"] == true || payload["checkpoint_valid"] == true {
		t.Fatalf("expected stale closeout fingerprint to be invalid: %#v", payload)
	}
	projected := v7ProjectedTaskState(vault, mustV7Task(t, vault, "APP-T-0001"), mustIndex(t, vault))
	if stringField(projected, "agent_action") == "stop_until_human_response" {
		t.Fatalf("stale closeout projection must not stop agent: %#v", projected)
	}
	code, err := validateCmd(Args{"vault": vault, "json": "true"})
	if err != nil {
		t.Fatal(err)
	}
	if code == 0 {
		t.Fatal("expected validate to reject stale closeout")
	}
}

func TestV7CloseoutFingerprintInvalidatesGateChange(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	mustV7Proof(t, Args{"vault": vault, "quiet": "true"}, bootstrap)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App", "summary": "Proof policy.", "v7": "true"}, newV7Epic)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Gate stale closeout", "risk": "low", "priority": "p2", "proof-mode": "inline", "proof-required": "human_signoff", "v7": "true"}, newV7Task)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "_pos1": "APP-T-0001", "covers": "A1", "check": "go test ./cmd/tusker -run TestV7CloseoutFingerprintInvalidatesGateChange -count=1", "result": "pass", "note": "Machine proof passed."}, v7TestVerificationMutation)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "blocks": "APP-T-0001", "kind": "signoff", "owner": "human:sarav", "action": "Sign off.", "verification": "Human signoff recorded.", "covers": "A1", "why-agent-cannot": "Final human signoff is required by this proof policy."}, newV7Gate)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "_pos0": "APP-T-0001", "emit-packet": "true", "validate": "printf validation-ok"}, closeoutV7Cmd)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "id": "APP-G-0001", "by": "human:sarav", "evidence": "Human signed off."}, func(args Args) error {
		return gateV7Transition(args, "satisfied")
	})

	idx := mustIndex(t, vault)
	task := mustV7Task(t, vault, "APP-T-0001")
	closeout, ok := latestV7Closeout(idx, "APP-T-0001")
	if !ok {
		t.Fatal("expected closeout")
	}
	if v7CloseoutCheckpointValid(vault, task, idx, closeout) {
		t.Fatal("gate state change should invalidate the closeout checkpoint")
	}
}

func TestV7CloseoutFingerprintHashesDirtyRepoContent(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	runGitV7ProofTest(t, repo, "init")
	runGitV7ProofTest(t, repo, "config", "user.email", "test@example.test")
	runGitV7ProofTest(t, repo, "config", "user.name", "Test User")
	if err := os.WriteFile(filepath.Join(repo, "app.txt"), []byte("clean\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitV7ProofTest(t, repo, "add", "app.txt")
	runGitV7ProofTest(t, repo, "commit", "-m", "initial")

	vault := filepath.Join(repo, "tusker")
	mustV7Proof(t, Args{"vault": vault, "quiet": "true"}, bootstrap)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App", "summary": "Proof policy.", "v7": "true"}, newV7Epic)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Dirty repo", "risk": "low", "priority": "p2", "proof-mode": "inline", "proof-required": "human_signoff", "v7": "true"}, newV7Task)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "_pos1": "APP-T-0001", "covers": "A1", "check": "go test ./cmd/tusker -run TestV7CloseoutFingerprintHashesDirtyRepoContent -count=1", "result": "pass", "note": "Machine proof passed."}, v7TestVerificationMutation)
	if err := os.WriteFile(filepath.Join(repo, "app.txt"), []byte("dirty one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := closeoutV7Cmd(Args{"vault": vault, "quiet": "true", "_pos0": "APP-T-0001", "emit-packet": "true", "validate": "printf validation-ok"}); err != nil {
		t.Fatal(err)
	}
	idx := mustIndex(t, vault)
	closeout, ok := latestV7Closeout(idx, "APP-T-0001")
	if !ok {
		t.Fatal("expected closeout")
	}
	if !v7CloseoutCheckpointValid(vault, mustV7Task(t, vault, "APP-T-0001"), idx, closeout) {
		t.Fatal("expected closeout valid before dirty content changes again")
	}
	if err := os.WriteFile(filepath.Join(repo, "app.txt"), []byte("dirty two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if v7CloseoutCheckpointValid(vault, mustV7Task(t, vault, "APP-T-0001"), mustIndex(t, vault), closeout) {
		t.Fatal("dirty content change with same git status should invalidate closeout")
	}
}

func TestV7CloseoutRepoStateHashesUntrackedContent(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	runGitV7ProofTest(t, repo, "init")
	vault := filepath.Join(repo, "tusker")
	mustV7Proof(t, Args{"vault": vault, "quiet": "true"}, bootstrap)
	untracked := filepath.Join(repo, "scratch.txt")
	if err := os.WriteFile(untracked, []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	first := v7StateRev(v7CloseoutRepoState(vault), "")
	if err := os.WriteFile(untracked, []byte("two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	second := v7StateRev(v7CloseoutRepoState(vault), "")
	if first == second {
		t.Fatal("untracked content change should alter repo fingerprint state")
	}
}

func TestV7HighRiskReviewWithMachineGapsStaysReviewerOwned(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	mustV7Proof(t, Args{"vault": vault, "quiet": "true"}, bootstrap)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App", "summary": "Proof policy.", "v7": "true"}, newV7Epic)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "High review gaps", "risk": "high", "priority": "p1", "status": "review", "proof-mode": "artifact", "v7": "true"}, newV7Task)

	projected := v7ProjectedTaskState(vault, mustV7Task(t, vault, "APP-T-0001"), mustIndex(t, vault))
	assertEqual(t, "waiting_on_review", stringField(projected, "readiness"), "readiness")
	assertEqual(t, "reviewer", stringField(projected, "next_owner"), "next owner")
	if stringField(projected, "agent_action") == "stop_until_human_response" {
		t.Fatalf("machine gaps must not be hidden by human close policy: %#v", projected)
	}
}

func TestV7VerificationGateCanSatisfyManualProofRequirement(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	mustV7Proof(t, Args{"vault": vault, "quiet": "true"}, bootstrap)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App", "summary": "Proof policy.", "v7": "true"}, newV7Epic)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Verification gate smoke", "risk": "low", "priority": "p2", "proof-mode": "inline", "proof-required": "manual_smoke", "proof-required-owner": "manual_smoke=human:sarav", "v7": "true"}, newV7Task)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "blocks": "APP-T-0001", "kind": "verification", "owner": "human:sarav", "action": "Run manual smoke.", "verification": "Manual smoke passed.", "covers": "A1", "why-agent-cannot": "Manual smoke requires human device or environment access."}, newV7Gate)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "id": "APP-G-0001", "by": "human:sarav", "evidence": "Manual smoke passed."}, func(args Args) error {
		return gateV7Transition(args, "satisfied")
	})

	report := computeV7ProofReport(vault, mustV7Task(t, vault, "APP-T-0001"), mustIndex(t, vault))
	assertEqual(t, "satisfied", report.Status, "proof status")
	assertEqual(t, 0, len(report.HumanMissing), "human proof gaps")
}

func runGitV7ProofTest(t *testing.T, repo string, args ...string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func TestV7VerificationSummaryDoesNotAutoSatisfyDefaultCardProof(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	mustV7Proof(t, Args{"vault": vault, "quiet": "true"}, bootstrap)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App", "summary": "Proof policy.", "v7": "true"}, newV7Epic)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Docs-only summary", "risk": "low", "priority": "p2", "proof-mode": "card", "v7": "true"}, newV7Task)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "runner": "codex"}, attemptV7StartCmd)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "kind": "verification_summary", "status": "accepted", "accepted-by": "reviewer:agent", "covers": "A1", "summary": "Reviewed docs only."}, evidenceV7AddCmd)

	err := finishV7Cmd(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "attempt": "APP-T-0001-A-0001", "summary": "Implementation complete.", "local": "true"})
	if err == nil {
		t.Fatal("expected finish to reject docs-only verification summary")
	}
	if !strings.Contains(err.Error(), "proof_required:focused_test") || !strings.Contains(err.Error(), "proof_required:broad_test") {
		t.Fatalf("expected focused/broad proof gaps, got %v", err)
	}
}

func TestV7ValidatorRejectsSourceFileEvidenceArtifacts(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	mustV7Proof(t, Args{"vault": vault, "quiet": "true"}, bootstrap)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App", "summary": "Proof policy.", "v7": "true"}, newV7Epic)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Forbidden evidence", "risk": "medium", "priority": "p2", "proof-mode": "card", "v7": "true"}, newV7Task)
	sourcePath := filepath.Join(filepath.Dir(vault), "copied.go")
	if err := os.WriteFile(sourcePath, []byte("package copied\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "kind": "verification_summary", "covers": "A1", "summary": "Copied source should be rejected.", "path": sourcePath, "link-only": "true"}, evidenceV7AddCmd)

	code, err := validateCmd(Args{"vault": vault, "json": "true"})
	if err != nil {
		t.Fatal(err)
	}
	if code == 0 {
		t.Fatal("expected validate to reject source file evidence artifact")
	}
}

func TestV7EvidencePolicyMigrationHonorsEvidenceRequired(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	mustV7Proof(t, Args{"vault": vault, "quiet": "true"}, bootstrap)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App", "summary": "Proof policy.", "v7": "true"}, newV7Epic)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Evidence policy migration", "risk": "medium", "priority": "p2", "evidence-required": "automated_test", "v7": "true"}, newV7Task)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "kind": "automated_test", "covers": "A1", "summary": "Focused tests passed."}, evidenceV7AddCmd)

	taskPath := filepath.Join(vault, "work", "tasks", "APP-T-0001.md")
	data, body, err := parseFrontmatterMustRead(taskPath)
	if err != nil {
		t.Fatal(err)
	}
	baseRev := stringField(data, "state_rev")
	delete(data, "proof_mode")
	delete(data, "proof_status")
	delete(data, "proof_required")
	delete(data, "evidence_budget")
	delete(data, "raw_artifacts_allowed")
	if _, err := saveV7DocumentCAS(taskPath, data, body, v7FrontmatterOrder["task"], baseRev); err != nil {
		t.Fatal(err)
	}

	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "write": "true"}, migrateV7EvidencePolicyCmd)
	after, afterBody, err := parseFrontmatterMustRead(taskPath)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "card", stringField(after, "proof_mode"), "proof mode")
	assertEqual(t, 1, intField(after, "evidence_budget"), "evidence budget")
	assertEqual(t, "satisfied", stringField(after, "proof_status"), "proof status")
	assertContainsIndexTest(t, afterBody, "[[APP-T-0001-E-0001]] automated_test (A1) - Focused tests passed.")
}

func TestV7AttachmentsMigrateMovesLegacyFilesToScratch(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	mustV7Proof(t, Args{"vault": vault, "quiet": "true"}, bootstrap)
	legacyDir := filepath.Join(vault, "Attachments", "APP-T-0001")
	if err := ensureDir(legacyDir); err != nil {
		t.Fatal(err)
	}
	legacyLog := filepath.Join(legacyDir, "raw.log")
	if err := os.WriteFile(legacyLog, []byte("PASS\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := attachmentsV7MigrateCmd(Args{"vault": vault, "_pos0": "migrate", "write": "true", "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	if fileExists(legacyLog) {
		t.Fatal("expected legacy attachment to be moved")
	}
	if dirExists(filepath.Join(vault, "Attachments")) {
		t.Fatal("expected empty Attachments directory to be removed")
	}
	assertExists(t, filepath.Join(vault, "scratch", "APP-T-0001", "legacy-attachments", "raw.log"))
}

func TestV7NoteWalkerSkipsScratchMarkdown(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	mustV7Proof(t, Args{"vault": vault, "quiet": "true"}, bootstrap)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App", "summary": "Proof policy.", "v7": "true"}, newV7Epic)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Scratch duplicate", "risk": "low", "priority": "p2", "proof-mode": "none", "v7": "true"}, newV7Task)
	scratch := filepath.Join(vault, "scratch", "APP-T-0001", "legacy-attachments", "APP-T-0001.md")
	if err := writeText(scratch, "---\nid: APP-T-0001\nkind: task\n---\n\nscratch copy\n"); err != nil {
		t.Fatal(err)
	}

	code, err := validateCmd(Args{"vault": vault, "json": "true"})
	if err != nil {
		t.Fatal(err)
	}
	if code != 0 {
		t.Fatal("expected scratch markdown to be ignored by validation")
	}
}

func mustV7Proof(t *testing.T, args Args, fn func(Args) error) {
	t.Helper()
	if err := runV7TestMutation(args, fn); err != nil {
		t.Fatal(err)
	}
}

func mustV7Task(t *testing.T, vault, taskID string) Note {
	t.Helper()
	note, err := resolveV7Note(vault, taskID, "task")
	if err != nil {
		t.Fatal(err)
	}
	return note
}
