package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func acceptSetVerificationBody(t *testing.T, vault, id, row string) {
	t.Helper()
	path := filepath.Join(vault, "work", "tasks", id+".md")
	data, body, err := parseFrontmatterMustRead(path)
	if err != nil {
		t.Fatal(err)
	}
	body = replaceSection(body, "## Verification", "| Covers | Check | Result | Notes |\n|---|---|---|---|\n"+row)
	data["proof_status"] = "pending"
	if _, err := saveV7DocumentCAS(path, data, body, v7FrontmatterOrder["task"], stringField(data, "state_rev")); err != nil {
		t.Fatal(err)
	}
}

func acceptPendingCommand(t *testing.T, vault, id, command string) {
	t.Helper()
	repo := v7RepoRoot(vault)
	if !v7GitRepo(repo) {
		runGit(t, "-C", repo, "init", "-q")
	}
	if err := verifyV7AddCmd(Args{"vault": vault, "quiet": "true", "_pos1": id, "by": "agent:worker", "covers": "A1", "check": "command: " + command, "result": "pending", "note": "Gate will observe this."}); err != nil {
		t.Fatal(err)
	}
}

func acceptVerificationManifest(t *testing.T, vault, id string) string {
	t.Helper()
	note, err := resolveV7Note(vault, id, "task")
	if err != nil {
		t.Fatal(err)
	}
	digest, pending := v7VerificationManifest(note.Data, parseV7VerificationRows(note.Body))
	if len(pending) == 0 {
		t.Fatal("expected pending command manifest")
	}
	return digest
}

func TestAcceptExecutesCommandRows(t *testing.T) {
	vault, id := acceptTestVaultWithTask(t)
	acceptPendingCommand(t, vault, id, "python3 -m unittest discover -s .")
	if err := acceptV7Cmd(Args{"vault": vault, "quiet": "true", "_pos0": id, "by": "reviewer:agent", "confirm": acceptVerificationManifest(t, vault, id)}); err != nil {
		t.Fatalf("accept pending command row: %v", err)
	}
	data, body, err := parseFrontmatterMustRead(filepath.Join(vault, "work", "tasks", id+".md"))
	if err != nil {
		t.Fatal(err)
	}
	rows := parseV7VerificationRows(body)
	if len(rows) != 1 || rows[0].Result != "pass" || !strings.Contains(rows[0].Notes, "output_sha256=sha256:") || !strings.Contains(rows[0].Notes, "tusker gate executed at") {
		t.Fatalf("accept did not persist an observed command receipt: %#v", rows)
	}
	if stringField(data, "status") != "done" {
		t.Fatalf("accept did not close after command passed: %s", stringField(data, "status"))
	}
}

func TestAcceptManualRowsStillGate(t *testing.T) {
	vault, id := acceptTestVaultWithTask(t)
	acceptSetVerificationBody(t, vault, id, "| A1 | manual proof: owner confirms the result | pending | Human confirmation required. |")
	err := acceptV7Cmd(Args{"vault": vault, "quiet": "true", "_pos0": id, "by": "reviewer:independent"})
	if err == nil || !strings.Contains(err.Error(), "proof is not green") {
		t.Fatalf("pending manual proof did not remain human-gated: %v", err)
	}
	if data := acceptTestTaskData(t, vault, id); stringField(data, "status") == "done" {
		t.Fatal("pending manual proof was accepted")
	}
	rows := parseV7VerificationRows(mustBody(t, filepath.Join(vault, "work", "tasks", id+".md")))
	if len(rows) != 1 || rows[0].Result != "pending" || !strings.HasPrefix(rows[0].Check, "manual proof:") {
		t.Fatalf("manual row was modified by accept: %#v", rows)
	}
}

func TestAcceptCommandRowTimeout(t *testing.T) {
	vault, id := acceptTestVaultWithTask(t)
	acceptPendingCommand(t, vault, id, "sleep 2; python3 -m unittest discover -s .")
	started := time.Now()
	err := acceptV7Cmd(Args{"vault": vault, "quiet": "true", "_pos0": id, "by": "reviewer:agent", "confirm": acceptVerificationManifest(t, vault, id), "verification-timeout-ms": "20"})
	if err == nil || !strings.Contains(err.Error(), "timed out") || !strings.Contains(err.Error(), "row A1") {
		t.Fatalf("timeout did not fail the named row: %v", err)
	}
	if time.Since(started) > time.Second {
		t.Fatalf("bounded command timeout took too long: %s", time.Since(started))
	}
	rows := parseV7VerificationRows(mustBody(t, filepath.Join(vault, "work", "tasks", id+".md")))
	if len(rows) != 1 || rows[0].Result != "fail" || !strings.Contains(rows[0].Notes, "timeout") {
		t.Fatalf("timeout receipt was not persisted: %#v", rows)
	}
}

func TestReviewRequestAllowsPendingCommandRows(t *testing.T) {
	vault, id := acceptTestVaultWithTask(t)
	acceptPendingCommand(t, vault, id, "python3 -m unittest discover -s .")
	if err := requestV7ReviewAfterHandoff(vault, id, Args{"vault": vault, "quiet": "true", "local": "true", "by": "agent:worker"}); err != nil {
		t.Fatalf("pending command row blocked review request: %v", err)
	}
	if got := stringField(acceptTestTaskData(t, vault, id), "status"); got != "review" {
		t.Fatalf("review request did not move task to review: %s", got)
	}
}

func TestCloseExecutesCommandRows(t *testing.T) {
	vault, id := acceptTestVaultWithTask(t)
	acceptPendingCommand(t, vault, id, "python3 -m unittest discover -s .")
	if err := statusV7Cmd(Args{"vault": vault, "quiet": "true", "id": id, "status": "review", "by": "agent:worker", "local": "true"}); err != nil {
		t.Fatalf("move task to review: %v", err)
	}
	if err := closeV7Cmd(Args{"vault": vault, "quiet": "true", "id": id, "by": "reviewer:agent", "local": "true", "confirm": acceptVerificationManifest(t, vault, id)}); err != nil {
		t.Fatalf("close pending command row: %v", err)
	}
	if got := stringField(acceptTestTaskData(t, vault, id), "status"); got != "done" {
		t.Fatalf("close did not execute and accept command row: %s", got)
	}
}

func TestVerificationCommandRequiresExactManifestBeforeShell(t *testing.T) {
	vault, id := acceptTestVaultWithTask(t)
	sentinel := filepath.Join(v7RepoRoot(vault), "must-not-run")
	acceptPendingCommand(t, vault, id, "touch must-not-run; python3 -m unittest discover -s .")
	for _, confirm := range []string{"", "sha256:" + strings.Repeat("0", 64)} {
		err := acceptV7Cmd(Args{"vault": vault, "quiet": "true", "_pos0": id, "by": "reviewer:agent", "confirm": confirm})
		if err == nil || (!strings.Contains(err.Error(), "manifest confirmation") && !strings.Contains(err.Error(), "manifest changed")) {
			t.Fatalf("confirmation %q was accepted: %v", confirm, err)
		}
		if fileExists(sentinel) {
			t.Fatal("unconfirmed manifest executed shell")
		}
	}
	err := acceptV7Cmd(Args{"vault": vault, "quiet": "true", "_pos0": id, "by": "reviewer:forged", "confirm": acceptVerificationManifest(t, vault, id)})
	if err == nil || !strings.Contains(err.Error(), "not the configured reviewer") {
		t.Fatalf("forged reviewer reached command execution: %v", err)
	}
	if fileExists(sentinel) {
		t.Fatal("forged reviewer executed shell")
	}
}

func TestVerificationCommandDoesNotRunForIneligibleClose(t *testing.T) {
	vault, id := acceptTestVaultWithTask(t)
	sentinel := filepath.Join(v7RepoRoot(vault), "must-not-run")
	acceptPendingCommand(t, vault, id, "touch must-not-run; python3 -m unittest discover -s .")
	if err := newV7Gate(Args{"vault": vault, "quiet": "true", "blocks": id, "kind": "release", "owner": "human:release", "action": "Authorize the production release.", "verification": "Release authority records approval.", "why-agent-cannot": "Only the production release authority can deploy."}); err != nil {
		t.Fatal(err)
	}
	err := acceptV7Cmd(Args{"vault": vault, "quiet": "true", "_pos0": id, "by": "reviewer:agent", "confirm": acceptVerificationManifest(t, vault, id)})
	if err == nil || !strings.Contains(err.Error(), "open gate") {
		t.Fatalf("ineligible accept was not refused before execution: %v", err)
	}
	if fileExists(sentinel) {
		t.Fatal("ineligible close executed shell")
	}
}

func TestVerificationCommandExecutesOnceAndDoesNotPersistRawOutput(t *testing.T) {
	vault, id := acceptTestVaultWithTask(t)
	t.Setenv("OPENAI_API_KEY", "TOP-SECRET-VERIFICATION-VALUE")
	acceptPendingCommand(t, vault, id, `test -z "$OPENAI_API_KEY" && printf x >> execution-count && printf '\122\101\127\055\117\125\124\120\125\124'; python3 -m unittest discover -s .`)
	if err := acceptV7Cmd(Args{"vault": vault, "quiet": "true", "_pos0": id, "by": "reviewer:agent", "confirm": acceptVerificationManifest(t, vault, id)}); err != nil {
		t.Fatal(err)
	}
	count, err := os.ReadFile(filepath.Join(v7RepoRoot(vault), "execution-count"))
	if err != nil || string(count) != "x" {
		t.Fatalf("command execution count=%q err=%v", count, err)
	}
	body := mustBody(t, filepath.Join(vault, "work", "tasks", id+".md"))
	if strings.Contains(body, "RAW-OUTPUT") {
		t.Fatal("raw command output was persisted in task receipt")
	}
}

func TestVerificationTimeoutKillsProcessGroup(t *testing.T) {
	vault, id := acceptTestVaultWithTask(t)
	acceptPendingCommand(t, vault, id, `sleep 30 & echo $! > verification-child.pid; wait; python3 -m unittest discover -s .`)
	err := acceptV7Cmd(Args{"vault": vault, "quiet": "true", "_pos0": id, "by": "reviewer:agent", "confirm": acceptVerificationManifest(t, vault, id), "verification-timeout-ms": "40"})
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("timeout was not surfaced: %v", err)
	}
	raw, readErr := os.ReadFile(filepath.Join(v7RepoRoot(vault), "verification-child.pid"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(raw)))
	deadline := time.Now().Add(time.Second)
	for pid > 0 && syscall.Kill(pid, 0) == nil && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if pid > 0 && syscall.Kill(pid, 0) == nil {
		_ = syscall.Kill(pid, syscall.SIGKILL)
		t.Fatalf("verification child %d survived process-group timeout", pid)
	}
}

func TestVerifyAddCannotForgeCommandExecutionResult(t *testing.T) {
	vault, id := acceptTestVaultWithTask(t)
	path := filepath.Join(vault, "work", "tasks", id+".md")
	before := mustBody(t, path)
	for _, result := range []string{"pass", "fail"} {
		err := verifyV7AddCmd(Args{"vault": vault, "quiet": "true", "_pos1": id, "by": "agent:worker", "covers": "A1", "check": "command: python3 -m unittest discover -s .", "result": result, "note": "claimed"})
		if err == nil || !strings.Contains(err.Error(), "must be pending") {
			t.Fatalf("public verify add accepted forged %s result: %v", result, err)
		}
	}
	if after := mustBody(t, path); after != before {
		t.Fatal("rejected forged rows mutated task")
	}
}

func TestVerificationDoesNotExecuteNonPendingCommandRows(t *testing.T) {
	vault, id := acceptTestVaultWithTask(t)
	acceptSetVerificationBody(t, vault, id, "| A1 | command: touch must-not-run | fail | Prior failure. |")
	err := acceptV7Cmd(Args{"vault": vault, "quiet": "true", "_pos0": id, "by": "reviewer:agent"})
	if err == nil || !strings.Contains(err.Error(), "proof is not green") {
		t.Fatalf("failed command row was not a proof refusal: %v", err)
	}
	if fileExists(filepath.Join(v7RepoRoot(vault), "must-not-run")) {
		t.Fatal("non-pending command row executed")
	}
}
