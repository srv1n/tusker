package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func TestV7TaskGateEvidenceAttemptReconcileFlow(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	must := func(args Args, fn func(Args) error) {
		t.Helper()
		if err := fn(args); err != nil {
			t.Fatal(err)
		}
	}

	must(Args{"vault": vault, "quiet": "true"}, bootstrap)
	must(Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App V7", "summary": "V7 tracker smoke.", "v7": "true"}, newV7Epic)
	must(Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Add provider harness", "risk": "low", "priority": "p1", "evidence-required": "automated_test", "v7": "true"}, newV7Task)
	must(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "owner": "agent:codex"}, claimCmd)
	assertExists(t, filepath.Join(filepath.Dir(vault), ".tusker-local", "leases", "APP-T-0001.json"))
	must(Args{"vault": vault, "quiet": "true", "blocks": "APP-T-0001", "kind": "auth", "owner": "human:sarav", "action": "Complete OAuth.", "verification": "Provider endpoint returns ready.", "why-agent-cannot": "Human credentials or account access are required."}, newV7Gate)
	must(Args{"vault": vault, "quiet": "true"}, reconcileV7Cmd)

	taskPath := filepath.Join(vault, "work", "tasks", "APP-T-0001.md")
	assertExists(t, taskPath)
	gatePath := filepath.Join(vault, "work", "gates", "APP-G-0001.md")
	assertExists(t, gatePath)
	data, _, err := parseFrontmatterMustRead(taskPath)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "waiting_on_human", stringField(data, "readiness"), "readiness")
	blockedRev := stringField(data, "state_rev")
	assertEqual(t, "human:sarav", stringField(data, "next_owner"), "next owner")
	assertEqual(t, "APP-G-0001", stringField(data, "next_ref"), "next ref")

	must(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "kind": "automated_test", "covers": "A1", "summary": "Focused V7 smoke passed.", "command": "go test ./cmd/tusker -run TestV7 -count=1"}, evidenceV7AddCmd)
	assertExists(t, filepath.Join(vault, "evidence", "APP-T-0001", "APP-T-0001-E-0001.md"))
	must(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "runner": "codex"}, attemptV7StartCmd)
	must(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "summary": "Implemented V7 smoke slice."}, attemptV7HandoffCmd)
	assertExists(t, filepath.Join(vault, "attempts", "APP-T-0001", "APP-T-0001-A-0001.md"))
	must(Args{"vault": vault, "quiet": "true", "id": "APP-G-0001", "by": "human:sarav", "evidence": "Provider endpoint returned ready."}, func(args Args) error {
		return gateV7Transition(args, "satisfied")
	})
	must(Args{"vault": vault, "quiet": "true"}, reconcileV7Cmd)

	data, _, err = parseFrontmatterMustRead(taskPath)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "ready", stringField(data, "readiness"), "readiness after gate")
	assertEqual(t, "agent", stringField(data, "next_owner"), "next owner after gate")
	if stringField(data, "state_rev") == blockedRev {
		t.Fatal("expected reconcile projection update to write a new state_rev")
	}
	must(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "status": "review", "by": "agent:codex", "reason": "Ready for independent review."}, statusV7Cmd)
	must(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "by": "reviewer:agent", "reason": "smoke accepted"}, closeV7Cmd)
	data, _, err = parseFrontmatterMustRead(taskPath)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "done", stringField(data, "status"), "closed status")
	assertEqual(t, "reviewer:agent", stringField(data, "accepted_by"), "accepted by")
	if code, err := validateCmd(Args{"vault": vault, "json": "true"}); err != nil || code != 0 {
		t.Fatalf("validate failed: code=%d err=%v", code, err)
	}

	packet := v7Packet(vault, Note{Data: data, Body: mustBody(t, taskPath)}, mustIndex(t, vault), "reviewer")
	if !strings.Contains(packet, "reviewer packet") || !strings.Contains(packet, "APP-T-0001-E-0001") {
		t.Fatalf("reviewer packet missing expected content:\n%s", packet)
	}
	assertExists(t, filepath.Join(vault, "dashboards", "human-actions.md"))
	assertExists(t, filepath.Join(vault, "dashboards", "active-runs.md"))
	assertExists(t, filepath.Join(vault, "_generated", "indexes", "tasks.json"))
	assertExists(t, filepath.Join(vault, "_generated", "indexes", "gates.json"))
	assertExists(t, filepath.Join(vault, "_generated", "indexes", "leases.json"))
	assertExists(t, filepath.Join(vault, "_generated", "indexes", "dashboard.json"))
}

func TestV7GateCreationRejectsVagueHumanGates(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	must := func(args Args, fn func(Args) error) {
		t.Helper()
		if err := fn(args); err != nil {
			t.Fatal(err)
		}
	}

	must(Args{"vault": vault, "quiet": "true"}, bootstrap)
	must(Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App V7", "summary": "Gate policy smoke.", "v7": "true"}, newV7Epic)
	must(Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Gate policy target", "risk": "medium", "priority": "p1", "v7": "true"}, newV7Task)

	err := newV7Gate(Args{"vault": vault, "quiet": "true", "blocks": "APP-T-0001", "kind": "manual_hold", "owner": "human:sarav", "action": "Resolve this gate so blocked work can proceed.", "verification": "Owner confirms the gate is satisfied.", "why-agent-cannot": "Human decision required."})
	if err == nil || !strings.Contains(err.Error(), "placeholder") {
		t.Fatalf("expected placeholder human gate rejection, got %v", err)
	}
	err = newV7Gate(Args{"vault": vault, "quiet": "true", "blocks": "APP-T-0001", "kind": "manual_hold", "owner": "human", "action": "Choose next product direction.", "verification": "Decision is recorded."})
	if err == nil || !strings.Contains(err.Error(), "why-agent-cannot") {
		t.Fatalf("expected bare human owner to require agent boundary, got %v", err)
	}
	err = newV7Gate(Args{"vault": vault, "quiet": "true", "blocks": "APP-T-0001", "kind": "verification", "owner": "human:sarav", "action": "Review code diff.", "verification": "Human approves the diff.", "why-agent-cannot": "Human should review the code."})
	if err == nil || !strings.Contains(err.Error(), "agent-capable") {
		t.Fatalf("expected human-owned code review gate rejection, got %v", err)
	}
	err = newV7Gate(Args{"vault": vault, "quiet": "true", "blocks": "APP-T-0001", "kind": "decision", "owner": "human:sarav", "action": "Human reviews code changes.", "verification": "Decision is recorded on the task.", "why-agent-cannot": "Human should review the code.", "suggestion": "Approve the diff.", "covers": "A1"})
	if err == nil || !strings.Contains(err.Error(), "agent-capable") {
		t.Fatalf("expected decision-gate code review loophole rejection, got %v", err)
	}
	must(Args{"vault": vault, "quiet": "true", "blocks": "APP-T-0001", "kind": "decision", "owner": "human:sarav", "action": "Choose frontend/backend API contract.", "verification": "Decision is recorded on the task.", "why-agent-cannot": "The spec conflicts with the current backend API and the agent cannot choose product intent.", "suggestion": "Align the frontend to the backend response field names unless the API change is intentional.", "covers": "A1"}, newV7Gate)
	if code, err := validateCmd(Args{"vault": vault, "json": "true"}); err != nil || code != 0 {
		t.Fatalf("expected concrete decision gate to validate, code=%d err=%v", code, err)
	}
}

func TestV7CreateGateProposalApplyRequiresHumanContext(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	must := func(args Args, fn func(Args) error) {
		t.Helper()
		if err := fn(args); err != nil {
			t.Fatal(err)
		}
	}

	must(Args{"vault": vault, "quiet": "true"}, bootstrap)
	must(Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App V7", "summary": "Gate proposal policy.", "v7": "true"}, newV7Epic)
	must(Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Gate proposal target", "risk": "medium", "priority": "p1", "v7": "true"}, newV7Task)
	must(Args{"vault": vault, "quiet": "true", "_pos0": "create_gate", "_pos1": "APP-T-0001", "kind": "auth", "owner": "human:sarav", "action": "Provision staging OAuth credentials.", "verification": "Provider ready check passes."}, proposalV7Cmd)
	must(Args{"vault": vault, "quiet": "true", "_pos0": "accept", "_pos1": "APP-P-0001", "by": "human:sarav"}, proposalV7Cmd)
	err := proposalV7Cmd(Args{"vault": vault, "quiet": "true", "_pos0": "apply", "_pos1": "APP-P-0001", "by": "human:sarav"})
	if err == nil || !strings.Contains(err.Error(), "why-agent-cannot") {
		t.Fatalf("expected create_gate proposal apply to require human context, got %v", err)
	}
}

func TestV7AgentReadyDashboardExcludesHumanOwnedReadyTasks(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	must := func(args Args, fn func(Args) error) {
		t.Helper()
		if err := fn(args); err != nil {
			t.Fatal(err)
		}
	}
	must(Args{"vault": vault, "quiet": "true"}, bootstrap)
	must(Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App V7", "summary": "Dashboard ownership.", "v7": "true"}, newV7Epic)
	must(Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Human ready", "risk": "low", "priority": "p2", "next-owner": "human:sarav", "next-source": "human_gate", "next-action": "Accept the manual gate.", "v7": "true"}, newV7Task)
	must(Args{"vault": vault, "quiet": "true"}, dashboardV7Cmd)

	dashboard := mustReadIndexTest(t, filepath.Join(vault, "dashboards", "agent-ready.md"))
	if strings.Contains(dashboard, "APP-T-0001") {
		t.Fatalf("human-owned task should not appear agent-ready:\n%s", dashboard)
	}
	index := mustReadIndexTest(t, filepath.Join(vault, "_generated", "indexes", "dashboard.json"))
	assertContainsIndexTest(t, index, `"agent_ready": 0`)
}

func TestV7ReconcileRepairsStaleObjectStateRevAndEmitsEvent(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	must := func(args Args, fn func(Args) error) {
		t.Helper()
		if err := fn(args); err != nil {
			t.Fatal(err)
		}
	}

	must(Args{"vault": vault, "quiet": "true"}, bootstrap)
	must(Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App V7", "summary": "V7 tracker smoke.", "v7": "true"}, newV7Epic)
	must(Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Manual body edit", "risk": "low", "priority": "p1", "v7": "true"}, newV7Task)
	must(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "runner": "codex"}, attemptV7StartCmd)

	taskPath := filepath.Join(vault, "work", "tasks", "APP-T-0001.md")
	data, body, err := parseFrontmatterMustRead(taskPath)
	if err != nil {
		t.Fatal(err)
	}
	staleRev := stringField(data, "state_rev")
	body += "\n## Manual contract detail\n\nThis simulates a body edit that bypassed Tusker CAS.\n"
	content, err := serializeDocument(data, body, v7FrontmatterOrder["task"])
	if err != nil {
		t.Fatal(err)
	}
	if err := writeText(taskPath, content); err != nil {
		t.Fatal(err)
	}
	attemptPath := filepath.Join(vault, "attempts", "APP-T-0001", "APP-T-0001-A-0001.md")
	attemptData, attemptBody, err := parseFrontmatterMustRead(attemptPath)
	if err != nil {
		t.Fatal(err)
	}
	staleAttemptRev := stringField(attemptData, "state_rev")
	attemptBody += "\n## Manual attempt detail\n\nThis simulates a non-task body edit that bypassed Tusker CAS.\n"
	attemptContent, err := serializeDocument(attemptData, attemptBody, v7FrontmatterOrder["attempt"])
	if err != nil {
		t.Fatal(err)
	}
	if err := writeText(attemptPath, attemptContent); err != nil {
		t.Fatal(err)
	}

	must(Args{"vault": vault, "quiet": "true"}, reconcileV7Cmd)
	afterData, afterBody, err := parseFrontmatterMustRead(taskPath)
	if err != nil {
		t.Fatal(err)
	}
	if stringField(afterData, "state_rev") == staleRev {
		t.Fatal("expected reconcile to refresh stale state_rev")
	}
	if stringField(afterData, "state_rev") != v7StateRev(afterData, afterBody) {
		t.Fatalf("state_rev does not match repaired task content")
	}
	assertEqual(t, "tusker:reconcile", stringField(afterData, "updated_by"), "updated_by")
	afterAttemptData, afterAttemptBody, err := parseFrontmatterMustRead(attemptPath)
	if err != nil {
		t.Fatal(err)
	}
	if stringField(afterAttemptData, "state_rev") == staleAttemptRev {
		t.Fatal("expected reconcile to refresh stale attempt state_rev")
	}
	if stringField(afterAttemptData, "state_rev") != v7StateRev(afterAttemptData, afterAttemptBody) {
		t.Fatalf("state_rev does not match repaired attempt content")
	}

	store := v7MarkdownStore{VaultPath: vault}
	events, err := store.GetEvents(context.Background(), v7EventScope{ObjectID: "APP-T-0001", EventKind: "updated"})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, event := range events {
		if stringField(event.Payload, "source") == "state_rev_repair" && stringField(event.Payload, "previous_state_rev") == staleRev {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected state_rev_repair event, got %#v", events)
	}
	attemptEvents, err := store.GetEvents(context.Background(), v7EventScope{ObjectID: "APP-T-0001-A-0001", EventKind: "updated"})
	if err != nil {
		t.Fatal(err)
	}
	found = false
	for _, event := range attemptEvents {
		if stringField(event.Payload, "source") == "state_rev_repair" && stringField(event.Payload, "previous_state_rev") == staleAttemptRev {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected attempt state_rev_repair event, got %#v", attemptEvents)
	}
}

func TestV7EvidenceAddUpdatesProofStatusAndTaskEvidenceSection(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	must := func(args Args, fn func(Args) error) {
		t.Helper()
		if err := fn(args); err != nil {
			t.Fatal(err)
		}
	}

	must(Args{"vault": vault, "quiet": "true"}, bootstrap)
	must(Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App V7", "summary": "V7 tracker smoke.", "v7": "true"}, newV7Epic)
	must(Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Evidence on branch", "risk": "low", "priority": "p2", "v7": "true"}, newV7Task)

	taskPath := filepath.Join(vault, "work", "tasks", "APP-T-0001.md")
	beforeData, beforeBody, err := parseFrontmatterMustRead(taskPath)
	if err != nil {
		t.Fatal(err)
	}
	beforeRev := stringField(beforeData, "state_rev")

	must(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "kind": "automated_test", "covers": "A1", "summary": "Branch-side evidence only."}, evidenceV7AddCmd)
	assertExists(t, filepath.Join(vault, "evidence", "APP-T-0001", "APP-T-0001-E-0001.md"))

	afterData, afterBody, err := parseFrontmatterMustRead(taskPath)
	if err != nil {
		t.Fatal(err)
	}
	if stringField(afterData, "state_rev") == beforeRev {
		t.Fatal("expected evidence add to refresh task proof_status/state_rev")
	}
	assertEqual(t, "satisfied", stringField(afterData, "proof_status"), "task proof_status after evidence add")
	if afterBody == beforeBody {
		t.Fatal("expected evidence add to refresh task Evidence section")
	}
	assertContainsIndexTest(t, afterBody, "Accepted:")
	assertContainsIndexTest(t, afterBody, "[[APP-T-0001-E-0001]] automated_test (A1) - Branch-side evidence only.")
	assertContainsIndexTest(t, afterBody, "Pending:")
	assertContainsIndexTest(t, afterBody, "- None.")
}

func TestV7SpecCLIExamplesRunThroughRouter(t *testing.T) {
	previousWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(previousWD); err != nil {
			t.Fatalf("restore working directory: %v", err)
		}
	})
	repo := t.TempDir()
	vault := filepath.Join(repo, "tusker")
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}
	if err := bootstrap(Args{"vault": vault, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	if err := writeText(filepath.Join(repo, "summary.md"), "Router handoff summary."); err != nil {
		t.Fatal(err)
	}
	if err := writeText(filepath.Join(repo, "provider-ready.png"), "fake screenshot fixture\n"); err != nil {
		t.Fatal(err)
	}
	runCLI := func(argv ...string) string {
		t.Helper()
		full := append([]string{"tusker"}, argv...)
		var code int
		var runErr error
		output := captureStdout(t, func() {
			command, args := parseCLI(full)
			args["vault"] = vault
			args["quiet"] = "true"
			code, runErr = run(command, args)
		})
		if runErr != nil || code != 0 {
			t.Fatalf("%s failed: code=%d err=%v output=%s", strings.Join(full, " "), code, runErr, output)
		}
		return output
	}

	runCLI("new", "epic", "APP", "--title", "First-class harness provider setup")
	runCLI("new", "task", "--epic", "APP", "--title", "Add direct OpenAI provider smoke harness", "--kind", "feature", "--risk", "low", "--priority", "p2")
	runCLI("new", "task", "--epic", "APP", "--title", "Human next action", "--next-owner", "human:sarav")
	runCLI("new", "task", "--epic", "APP", "--title", "Reviewer next action", "--next-owner", "reviewer")
	runCLI("new", "task", "--epic", "APP", "--title", "Agent next action", "--next-owner", "agent")
	makeV7TaskDispatchableForTest(t, vault, "APP-T-0001")
	makeV7TaskDispatchableForTest(t, vault, "APP-T-0004")
	runCLI("new", "gate", "--blocks", "APP-T-0001", "--kind", "auth", "--owner", "human:sarav", "--action", "Complete OAuth.", "--verification", "Provider endpoint returns ready.", "--why-agent-cannot", "Human credentials or account access are required.")
	runCLI("new", "gate", "--blocks", "APP-T-0002", "--kind", "setup", "--owner", "human:sarav", "--action", "Prepare setup.", "--verification", "Setup is available.", "--why-agent-cannot", "Human environment setup is required.")
	runCLI("new", "gate", "--blocks", "APP-T-0003", "--kind", "verification", "--owner", "reviewer", "--action", "Review proof.", "--verification", "Reviewer accepts proof.")
	runCLI("new", "decision", "--epic", "APP", "--title", "Use repo-local branch-safe work tracker")

	runCLI("gate", "list", "--open")
	runCLI("gate", "list", "--owner", "human:sarav")
	runCLI("list", "--runnable")
	runCLI("next", "--owner", "agent")
	runCLI("gate", "satisfy", "APP-G-0001", "--evidence", "Provider ready endpoint returned OpenAI model.")
	runCLI("gate", "waive", "APP-G-0002", "--reason", "Live smoke deferred to release candidate.")
	runCLI("gate", "obsolete", "APP-G-0003", "--reason", "Task superseded.")
	runCLI("claim", "APP-T-0001", "--owner", "agent:codex")
	runCLI("heartbeat", "APP-T-0001")
	runCLI("release", "APP-T-0001")
	runCLI("evidence", "add", "APP-T-0001", "--kind", "automated_test", "--covers", "A1,A2", "--summary", "Focused provider smoke tests passed.")
	runCLI("evidence", "add", "APP-T-0001", "--kind", "screenshot", "--path", "./provider-ready.png", "--covers", "A3", "--status", "accepted", "--checked-by", "reviewer:agent")
	runCLI("attempt", "start", "APP-T-0001")
	runCLI("attempt", "handoff", "APP-T-0001", "--summary", "./summary.md")
	runCLI("propose", "close", "APP-T-0001", "--reason", "Implementation branch is ready.")
	runCLI("proposal", "list", "--target", "APP-T-0001")
	runCLI("proposal", "accept", "APP-P-0001", "--by", "human:sarav")
	runCLI("packet", "APP-T-0001", "--for", "agent", "--force")
	runCLI("packet", "APP-T-0001", "--for", "reviewer")
	runCLI("brief", "APP-T-0001")
	runCLI("brief", "--owner", "human:sarav")
	runCLI("dashboard", "build")
	openOutput := runCLI("dashboard", "open", "human-actions")
	assertContainsIndexTest(t, openOutput, filepath.Join(vault, "dashboards", "human-actions.md"))
	runCLI("next", "--owner", "agent")
	runCLI("reconcile")
	runCLI("status", "APP-T-0001", "review", "--reason", "Ready for independent review.")
	runCLI("close", "APP-T-0001", "--by", "reviewer:agent")

	taskPath := filepath.Join(vault, "work", "tasks", "APP-T-0001.md")
	data, _, err := parseFrontmatterMustRead(taskPath)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "done", stringField(data, "status"), "router close status")
	assertExists(t, filepath.Join(vault, "evidence", "APP-T-0001", "APP-T-0001-E-0001.md"))
	assertExists(t, filepath.Join(vault, "attempts", "APP-T-0001", "APP-T-0001-A-0001.md"))
}

func TestV7ProjectIdentityAndDiscovery(t *testing.T) {
	repo := t.TempDir()
	vault := filepath.Join(repo, ".tusker")
	if err := ensureDir(filepath.Join(vault, "work", "tasks")); err != nil {
		t.Fatal(err)
	}
	if err := ensureDir(filepath.Join(vault, "knowledge", "domains", "project")); err != nil {
		t.Fatal(err)
	}
	if err := writeText(filepath.Join(repo, "tusker.yaml"), "schema: tusker.config/v1\nproject_id: root-project\nstorage:\n  root: .tusker\n"); err != nil {
		t.Fatal(err)
	}
	discovered, err := discoverVault(filepath.Join(repo, "src", "pkg"))
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, vault, discovered, "discovered V7 vault from repo root config")
	projectID, err := resolveV7ProjectID(vault)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "root-project", projectID, "root tusker.yaml project_id")

	legacyVault := filepath.Join(repo, "tusker")
	if err := ensureDir(filepath.Join(legacyVault, "work", "tasks")); err != nil {
		t.Fatal(err)
	}
	discovered, err = discoverVault(filepath.Join(repo, "src", "pkg"))
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, vault, discovered, "dot vault is preferred over legacy tusker when both exist")

	if err := os.Remove(filepath.Join(repo, "tusker.yaml")); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(vault); err != nil {
		t.Fatal(err)
	}
	discovered, err = discoverVault(filepath.Join(repo, "src", "pkg"))
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, legacyVault, discovered, "legacy visible tusker remains discoverable")
	if err := ensureDir(filepath.Join(vault, "_system")); err != nil {
		t.Fatal(err)
	}
	if err := writeText(filepath.Join(vault, "_system", "project.yaml"), "project_id: migrated-project\nid: legacy-project\n"); err != nil {
		t.Fatal(err)
	}
	projectID, err = resolveV7ProjectID(vault)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "migrated-project", projectID, "migration project_id")
	if err := writeText(filepath.Join(vault, "_system", "project.yaml"), "id: legacy-project\n"); err != nil {
		t.Fatal(err)
	}
	projectID, err = resolveV7ProjectID(vault)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "legacy-project", projectID, "legacy id fallback")
	if err := writeText(filepath.Join(vault, "_system", "project.yaml"), "{}\n"); err != nil {
		t.Fatal(err)
	}
	if err := ensureDir(filepath.Join(repo, ".git")); err != nil {
		t.Fatal(err)
	}
	projectID, err = resolveV7ProjectID(vault)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, sanitizeProjectID(filepath.Base(repo)), projectID, "repo directory fallback")

	orphanRepo := t.TempDir()
	orphanVault := filepath.Join(orphanRepo, "tusker")
	if err := ensureDir(filepath.Join(orphanVault, "work")); err != nil {
		t.Fatal(err)
	}
	if _, err := resolveV7ProjectID(orphanVault); err == nil {
		t.Fatal("expected orphan V7 vault without tusker.yaml, metadata, or git repo to fail project identity")
	}
}

func TestV7ProfileInitCreatesSkillShapedKnowledgeVault(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	if err := initCmd(Args{"vault": vault, "yes": "true", "vault-only": "true", "no-mount": "true", "profile": "v7"}); err != nil {
		t.Fatal(err)
	}

	skillPath := filepath.Join(vault, "SKILL.md")
	projectIndexPath := filepath.Join(vault, "knowledge", "domains", "project", "INDEX.md")
	projectCanonPath := filepath.Join(vault, "knowledge", "domains", "project", "CANON.md")
	assertExists(t, skillPath)
	assertExists(t, projectIndexPath)
	assertExists(t, projectCanonPath)
	if _, err := os.Stat(filepath.Join(vault, "domains")); !os.IsNotExist(err) {
		t.Fatalf("V7 profile should not create V6 tusker/domains/**: %v", err)
	}
	data, body, err := parseFrontmatterMustRead(skillPath)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "tusker.project-skill/v7", stringField(data, "schema"), "project skill schema")
	assertContainsIndexTest(t, body, "Tusker operator skill")
	assertContainsIndexTest(t, body, "## Repo Command Policy")
	assertContainsIndexTest(t, body, "build-lock/status commands")
	assertContainsIndexTest(t, body, "managed Tusker bootstrap pointers")
	assertContainsIndexTest(t, body, "knowledge/domains/project/INDEX.md")
	assertContainsIndexTest(t, body, "knowledge/domains/project/CANON.md")
	if code, err := validateCmd(Args{"vault": vault, "json": "true"}); err != nil || code != 0 {
		t.Fatalf("validate failed: code=%d err=%v", code, err)
	}
}

func TestV7PublishSkillExportsKnowledgeBundleAndFiltersProofState(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	if err := bootstrapV7Profile(vault, "v7"); err != nil {
		t.Fatal(err)
	}
	if err := domainNewCmd(Args{"vault": vault, "quiet": "true", "v7": "true", "id": "providers", "title": "Providers", "summary": "Provider integrations."}); err != nil {
		t.Fatal(err)
	}
	_, sourceBody, err := parseFrontmatterMustRead(filepath.Join(vault, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	assertContainsIndexTest(t, sourceBody, "knowledge/domains/providers/INDEX.md")
	assertContainsIndexTest(t, sourceBody, "knowledge/domains/providers/CANON.md")
	for _, rel := range []string{
		"work/tasks/APP-T-0001.md",
		"evidence/APP-T-0001/APP-T-0001-E-0001.md",
		"attempts/APP-T-0001/APP-T-0001-A-0001.md",
		"events/2026/05/APP-T-0001--20260514T000000Z--01.json",
		"_generated/packets/APP-T-0001.agent.md",
		"Attachments/APP-T-0001/raw.log",
	} {
		if err := writeText(filepath.Join(vault, filepath.FromSlash(rel)), "forbidden export fixture\n"); err != nil {
			t.Fatal(err)
		}
	}

	out := filepath.Join(t.TempDir(), "project-skill")
	if err := publishSkillCmd(Args{"vault": vault, "out": out, "v7": "true", "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	staleExportFile := filepath.Join(out, "stale.txt")
	if err := writeText(staleExportFile, "stale export content\n"); err != nil {
		t.Fatal(err)
	}
	if err := publishSkillCmd(Args{"vault": vault, "out": out, "v7": "true", "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(staleExportFile); !os.IsNotExist(err) {
		t.Fatalf("expected stale export file to be removed on marked output cleanup: %v", err)
	}

	assertExists(t, filepath.Join(out, "SKILL.md"))
	assertExists(t, filepath.Join(out, "knowledge", "domains", "project", "INDEX.md"))
	assertExists(t, filepath.Join(out, "knowledge", "domains", "project", "CANON.md"))
	assertExists(t, filepath.Join(out, "knowledge", "domains", "providers", "INDEX.md"))
	assertExists(t, filepath.Join(out, "knowledge", "domains", "providers", "CANON.md"))
	for _, forbidden := range []string{"work", "evidence", "attempts", "events", "_generated", "Attachments", "domains"} {
		if _, err := os.Stat(filepath.Join(out, forbidden)); !os.IsNotExist(err) {
			t.Fatalf("V7 project skill export included forbidden path %s: %v", forbidden, err)
		}
	}
	data, _, err := parseFrontmatterMustRead(filepath.Join(out, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "tusker.project-skill/v7", stringField(data, "schema"), "exported project skill schema")
	_, exportedBody, err := parseFrontmatterMustRead(filepath.Join(out, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	assertContainsIndexTest(t, exportedBody, "knowledge/domains/providers/INDEX.md")
	assertContainsIndexTest(t, exportedBody, "knowledge/domains/providers/CANON.md")
	assertContainsIndexTest(t, exportedBody, "## Repo Command Policy")
	assertContainsIndexTest(t, exportedBody, "token/noise wrappers")
}

func TestV7PublishSkillRejectsUnsafeOutputPaths(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	if err := ensureDir(repo); err != nil {
		t.Fatal(err)
	}
	vault := filepath.Join(repo, "tusker")
	if err := bootstrapV7Profile(vault, "v7"); err != nil {
		t.Fatal(err)
	}
	previousWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(previousWD); err != nil {
			t.Fatalf("restore working directory: %v", err)
		}
	})
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name string
		out  string
		dir  string
	}{
		{name: "current directory", out: ".", dir: repo},
		{name: "repo root", out: repo, dir: repo},
		{name: "vault root", out: vault, dir: vault},
		{name: "home", out: home, dir: home},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sentinel := filepath.Join(tc.dir, "sentinel.txt")
			if err := writeText(sentinel, "do not delete\n"); err != nil {
				t.Fatal(err)
			}
			err := publishSkillCmd(Args{"vault": vault, "out": tc.out, "v7": "true", "quiet": "true"})
			if err == nil {
				t.Fatalf("expected unsafe output path %q to be rejected", tc.out)
			}
			assertExists(t, sentinel)
		})
	}
}

func TestV7ValidationRejectsProjectSkillForbiddenSources(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	if err := bootstrapV7Profile(vault, "v7"); err != nil {
		t.Fatal(err)
	}
	skillPath := filepath.Join(vault, "SKILL.md")
	data, body, err := parseFrontmatterMustRead(skillPath)
	if err != nil {
		t.Fatal(err)
	}
	data["source_of_truth"] = []string{"knowledge/domains", "work/tasks/APP-T-0001.md"}
	content, err := serializeDocument(data, body, v7FrontmatterOrder["project_skill"])
	if err != nil {
		t.Fatal(err)
	}
	if err := writeText(skillPath, content); err != nil {
		t.Fatal(err)
	}

	code, err := validateCmd(Args{"vault": vault, "json": "true"})
	if err != nil {
		t.Fatal(err)
	}
	if code == 0 {
		t.Fatal("expected validate to reject forbidden project skill source truth")
	}
}

func TestV7ValidationRejectsStaleProjectSkillDomainRoutes(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	if err := bootstrapV7Profile(vault, "v7"); err != nil {
		t.Fatal(err)
	}
	if err := domainNewCmd(Args{"vault": vault, "quiet": "true", "v7": "true", "id": "providers", "title": "Providers", "summary": "Provider integrations."}); err != nil {
		t.Fatal(err)
	}
	skillPath := filepath.Join(vault, "SKILL.md")
	data, body, err := parseFrontmatterMustRead(skillPath)
	if err != nil {
		t.Fatal(err)
	}
	var staleRows []string
	for _, line := range strings.Split(body, "\n") {
		if strings.Contains(line, "knowledge/domains/providers/") {
			continue
		}
		staleRows = append(staleRows, line)
	}
	content, err := serializeDocument(data, strings.Join(staleRows, "\n"), v7FrontmatterOrder["project_skill"])
	if err != nil {
		t.Fatal(err)
	}
	if err := writeText(skillPath, content); err != nil {
		t.Fatal(err)
	}

	errs, _ := validateV7SkillKnowledge(vault)
	if !issuesContainCode(errs, "PROJECT_SKILL_DOMAIN_ROUTE_MISSING") {
		t.Fatalf("expected stale project skill route failure, got %#v", errs)
	}
	code, err := validateCmd(Args{"vault": vault, "json": "true"})
	if err != nil {
		t.Fatal(err)
	}
	if code == 0 {
		t.Fatal("expected validate to reject stale project skill routes")
	}
}

func TestV7ValidationRejectsDoneTaskWithOpenGateAndMissingEvidence(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	must := func(args Args, fn func(Args) error) {
		t.Helper()
		if err := fn(args); err != nil {
			t.Fatal(err)
		}
	}

	must(Args{"vault": vault, "quiet": "true"}, bootstrap)
	must(Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App V7", "summary": "V7 tracker smoke.", "v7": "true"}, newV7Epic)
	must(Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Close proof", "risk": "high", "priority": "p0", "evidence-required": "automated_test", "status": "done", "readiness": "done", "v7": "true"}, newV7Task)
	must(Args{"vault": vault, "quiet": "true", "blocks": "APP-T-0001", "kind": "verification", "owner": "reviewer", "action": "Review proof.", "verification": "Evidence accepted."}, newV7Gate)

	code, err := validateCmd(Args{"vault": vault, "json": "true"})
	if err != nil {
		t.Fatal(err)
	}
	if code == 0 {
		t.Fatal("expected validate to reject incomplete done task")
	}
}

func TestV7ReconcileIndexesDoneTasksWithOpenGates(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	must := func(args Args, fn func(Args) error) {
		t.Helper()
		if err := fn(args); err != nil {
			t.Fatal(err)
		}
	}

	must(Args{"vault": vault, "quiet": "true"}, bootstrap)
	must(Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App V7", "summary": "V7 tracker smoke.", "v7": "true"}, newV7Epic)
	must(Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Done with gate", "risk": "low", "priority": "p2", "status": "done", "readiness": "done", "v7": "true"}, newV7Task)
	must(Args{"vault": vault, "quiet": "true", "blocks": "APP-T-0001", "kind": "verification", "owner": "reviewer", "action": "Review proof.", "verification": "Evidence accepted."}, newV7Gate)

	output := captureStdout(t, func() {
		if err := reconcileV7Cmd(Args{"vault": vault}); err != nil {
			t.Fatal(err)
		}
	})
	assertContainsIndexTest(t, output, "1 done/open-gate violation")

	raw, err := readText(filepath.Join(vault, "_generated", "indexes", "dashboard.json"))
	if err != nil {
		t.Fatal(err)
	}
	var dashboard map[string]any
	if err := json.Unmarshal([]byte(raw), &dashboard); err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "1", fmt.Sprintf("%.0f", dashboard["done_open_gates"].(float64)), "done open gate count")
}

func TestV7ReconcileUpdatesEpicManagedBlocks(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	must := func(args Args, fn func(Args) error) {
		t.Helper()
		if err := fn(args); err != nil {
			t.Fatal(err)
		}
	}

	must(Args{"vault": vault, "quiet": "true"}, bootstrap)
	must(Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App V7", "summary": "V7 tracker smoke.", "v7": "true"}, newV7Epic)
	must(Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Managed block task", "risk": "low", "priority": "p2", "status": "ready", "readiness": "ready", "force-ready": "true", "v7": "true"}, newV7Task)
	must(Args{"vault": vault, "quiet": "true", "blocks": "APP-T-0001", "kind": "verification", "owner": "reviewer", "action": "Review proof.", "verification": "Evidence accepted."}, newV7Gate)
	must(Args{"vault": vault, "quiet": "true"}, reconcileV7Cmd)

	epicPath := filepath.Join(vault, "work", "epics", "APP.md")
	epic := mustReadIndexTest(t, epicPath)
	assertContainsIndexTest(t, epic, "<!-- tusker:generated open-gates -->")
	assertContainsIndexTest(t, epic, "| [[APP-G-0001]] | reviewer | [[APP-T-0001]] | Review proof. |")
	assertContainsIndexTest(t, epic, "<!-- tusker:generated active-work -->")
	assertContainsIndexTest(t, epic, "| [[APP-T-0001]] | ready | reviewer | Review proof. |")

	must(Args{"vault": vault, "quiet": "true", "id": "APP-G-0001", "by": "reviewer:agent", "evidence": "Reviewer accepted proof."}, func(args Args) error {
		return gateV7Transition(args, "satisfied")
	})
	must(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "kind": "automated_test", "covers": "A1", "summary": "Focused tests passed."}, evidenceV7AddCmd)
	must(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "status": "review", "by": "agent:codex"}, statusV7Cmd)
	must(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "by": "reviewer:agent"}, closeV7Cmd)
	must(Args{"vault": vault, "quiet": "true"}, reconcileV7Cmd)

	epic = mustReadIndexTest(t, epicPath)
	assertContainsIndexTest(t, epic, "<!-- tusker:generated recently-completed -->")
	assertContainsIndexTest(t, epic, "| [[APP-T-0001]] | reviewer:agent |")
}

func TestV7CreationRejectsInvalidDurableStatus(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	if err := bootstrap(Args{"vault": vault, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	err := newV7Task(Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Invalid active task", "status": "active", "v7": "true"})
	if err == nil {
		t.Fatal("expected V7 task creation to reject active as a durable status")
	}
	if !strings.Contains(err.Error(), "invalid V7 task status") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestV7ValidationRejectsStaleReadyProjectionWithOpenGate(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	must := func(args Args, fn func(Args) error) {
		t.Helper()
		if err := fn(args); err != nil {
			t.Fatal(err)
		}
	}

	must(Args{"vault": vault, "quiet": "true"}, bootstrap)
	must(Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App V7", "summary": "V7 tracker smoke.", "v7": "true"}, newV7Epic)
	must(Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Stale projection", "risk": "low", "priority": "p2", "v7": "true"}, newV7Task)
	must(Args{"vault": vault, "quiet": "true", "blocks": "APP-T-0001", "kind": "manual_hold", "owner": "human:sarav", "action": "Decide next step.", "verification": "Decision recorded.", "why-agent-cannot": "Human product direction is required before the agent can continue."}, newV7Gate)
	forceV7TaskProjection(t, vault, "APP-T-0001", "ready", "ready", "agent", "Execute the task contract.")

	code, err := validateCmd(Args{"vault": vault, "json": "true"})
	if err != nil {
		t.Fatal(err)
	}
	if code == 0 {
		t.Fatal("expected validate to reject ready task with open blocking gate")
	}
}

func TestV7ValidationHardensTaskSchemaAndKind(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	if err := bootstrap(Args{"vault": vault, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	data := map[string]any{
		"schema":                "tusker.task/v7",
		"kind":                  "task",
		"id":                    "APP-T-0001",
		"project":               "tusker",
		"title":                 "Task schema",
		"status":                "ready",
		"readiness":             "ready",
		"priority":              "p2",
		"risk":                  "low",
		"proof_mode":            "inline",
		"proof_status":          "pending",
		"proof_required":        []string{"focused_test"},
		"evidence_budget":       0,
		"raw_artifacts_allowed": false,
		"next_owner":            "agent",
		"next_action":           "Execute the task contract.",
	}
	body := v7TaskBody("APP-T-0001", "Task schema")
	note := Note{Data: data, Body: body, RelativePath: "work/tasks/APP-T-0001.md"}
	errs, _ := validateV7Note(note, validationContext{VaultPath: vault, RelativePath: note.RelativePath}, note.RelativePath)
	if len(errs) != 0 {
		t.Fatalf("expected valid task schema, got %#v", errs)
	}

	data["schema"] = "tusker.gate/v1"
	data["kind"] = "task"
	errs, _ = validateV7Note(note, validationContext{VaultPath: vault, RelativePath: note.RelativePath}, note.RelativePath)
	if !issuesContainCode(errs, errorInvalidField) {
		t.Fatalf("expected task schema failure, got %#v", errs)
	}
	data["schema"] = "tusker.task/v7"
	data["kind"] = "gate"
	errs, _ = validateV7Note(note, validationContext{VaultPath: vault, RelativePath: note.RelativePath}, note.RelativePath)
	if !issuesContainCode(errs, errorInvalidField) {
		t.Fatalf("expected task kind failure, got %#v", errs)
	}
}

func TestV7ReconcileEmitsProjectionUpdateEvent(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	must := func(args Args, fn func(Args) error) {
		t.Helper()
		if err := fn(args); err != nil {
			t.Fatal(err)
		}
	}

	must(Args{"vault": vault, "quiet": "true"}, bootstrap)
	must(Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App V7", "summary": "V7 tracker smoke.", "v7": "true"}, newV7Epic)
	must(Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Projection event", "risk": "low", "priority": "p2", "v7": "true"}, newV7Task)
	must(Args{"vault": vault, "quiet": "true", "blocks": "APP-T-0001", "kind": "auth", "owner": "human:sarav", "action": "Complete OAuth.", "verification": "Provider endpoint returns ready.", "why-agent-cannot": "Human credentials or account access are required."}, newV7Gate)
	forceV7TaskProjection(t, vault, "APP-T-0001", "ready", "ready", "agent", "Execute the task contract.")
	must(Args{"vault": vault, "quiet": "true"}, reconcileV7Cmd)

	eventErrs, _, eventCount := validateV7Events(vault)
	if len(eventErrs) != 0 {
		t.Fatalf("expected reconcile event to validate, got %#v", eventErrs)
	}
	if eventCount == 0 {
		t.Fatal("expected reconcile to emit an event")
	}
	eventFiles, err := filepath.Glob(filepath.Join(vault, "events", "*", "*", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, eventFile := range eventFiles {
		raw, err := readText(eventFile)
		if err != nil {
			t.Fatal(err)
		}
		var data map[string]any
		if err := json.Unmarshal([]byte(raw), &data); err != nil {
			t.Fatal(err)
		}
		if stringField(data, "event_kind") != "updated" || stringField(data, "actor") != "tusker:reconcile" {
			continue
		}
		payload := mapField(data, "payload")
		if stringField(payload, "source") == "projection" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected projection update event, files=%v", eventFiles)
	}
}

func TestV7ValidationWarnsOnLargeKnowledgeDeltaAndManyDomains(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	if err := bootstrap(Args{"vault": vault, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	data := map[string]any{
		"schema":                "tusker.task/v7",
		"kind":                  "task",
		"id":                    "APP-T-0001",
		"project":               "tusker",
		"title":                 "Warn on broad task",
		"epic":                  "APP",
		"status":                "ready",
		"readiness":             "ready",
		"priority":              "p2",
		"risk":                  "low",
		"proof_mode":            "inline",
		"proof_status":          "pending",
		"proof_required":        []string{"focused_test"},
		"evidence_budget":       0,
		"raw_artifacts_allowed": false,
		"next_owner":            "agent",
		"next_action":           "Execute the task contract.",
		"domains":               []string{"a", "b", "c", "d", "e", "f"},
	}
	body := v7TaskBody("APP-T-0001", "Warn on broad task")
	var longDelta []string
	for i := 0; i < 21; i++ {
		longDelta = append(longDelta, fmt.Sprintf("- Durable fact %02d that belongs in domain canon.", i+1))
	}
	body = replaceSection(body, "## Knowledge delta", strings.Join(longDelta, "\n"))
	data["state_rev"] = v7StateRev(data, body)
	note := Note{Data: data, Body: body, RelativePath: "work/tasks/APP-T-0001.md"}

	errs, warns := validateV7Note(note, validationContext{VaultPath: vault, RelativePath: note.RelativePath}, note.RelativePath)
	if len(errs) != 0 {
		t.Fatalf("expected warnings only, got errors %#v", errs)
	}
	codes := map[string]bool{}
	for _, warning := range warns {
		codes[warning.Code] = true
	}
	if !codes["TASK_TOO_MANY_DOMAINS"] || !codes["KNOWLEDGE_DELTA_TOO_LONG"] {
		t.Fatalf("expected domain and knowledge delta warnings, got %#v", warns)
	}
}

func TestV7ValidationReadsConfiguredTaskBodyLineLimits(t *testing.T) {
	repo := t.TempDir()
	vault := filepath.Join(repo, "tusker")
	if err := bootstrap(Args{"vault": vault, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	data := map[string]any{
		"schema":                "tusker.task/v7",
		"kind":                  "task",
		"id":                    "APP-T-0001",
		"project":               "tusker",
		"title":                 "Configured limits",
		"epic":                  "APP",
		"status":                "ready",
		"readiness":             "ready",
		"priority":              "p2",
		"risk":                  "low",
		"proof_mode":            "inline",
		"proof_status":          "pending",
		"proof_required":        []string{"focused_test"},
		"evidence_budget":       0,
		"raw_artifacts_allowed": false,
		"next_owner":            "agent",
		"next_action":           "Execute the task contract.",
	}
	body := v7TaskBody("APP-T-0001", "Configured limits")
	data["state_rev"] = v7StateRev(data, body)
	note := Note{Data: data, Body: body, RelativePath: "work/tasks/APP-T-0001.md"}

	if err := writeText(filepath.Join(repo, "tusker.yaml"), "validation:\n  task_body_warn_lines: 5\n  task_body_fail_lines: 10\n"); err != nil {
		t.Fatal(err)
	}
	errs, _ := validateV7Note(note, validationContext{VaultPath: vault, RelativePath: note.RelativePath}, note.RelativePath)
	if !issuesContainCode(errs, "TASK_BODY_TOO_LONG") {
		t.Fatalf("expected configured fail limit to reject task body, got %#v", errs)
	}

	if err := writeText(filepath.Join(repo, "tusker.yaml"), "validation:\n  task_body_warn_lines: 5\n  task_body_fail_lines: 100\n"); err != nil {
		t.Fatal(err)
	}
	errs, warns := validateV7Note(note, validationContext{VaultPath: vault, RelativePath: note.RelativePath}, note.RelativePath)
	if issuesContainCode(errs, "TASK_BODY_TOO_LONG") {
		t.Fatalf("did not expect body length error with higher fail limit, got %#v", errs)
	}
	if !issuesContainCode(warns, "TASK_BODY_LONG") {
		t.Fatalf("expected configured warning limit to warn on task body length, got %#v", warns)
	}
}

func TestV7ValidationReadsConfiguredFrontmatterWarnLines(t *testing.T) {
	repo := t.TempDir()
	vault := filepath.Join(repo, "tusker")
	if err := bootstrap(Args{"vault": vault, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	if err := writeText(filepath.Join(repo, "tusker.yaml"), "validation:\n  frontmatter_warn_lines: 5\n"); err != nil {
		t.Fatal(err)
	}
	data := map[string]any{
		"schema":                "tusker.task/v7",
		"kind":                  "task",
		"id":                    "APP-T-0001",
		"project":               "tusker",
		"title":                 "Frontmatter warning",
		"epic":                  "APP",
		"status":                "ready",
		"readiness":             "ready",
		"priority":              "p2",
		"risk":                  "low",
		"proof_mode":            "inline",
		"proof_status":          "pending",
		"proof_required":        []string{"focused_test"},
		"evidence_budget":       0,
		"raw_artifacts_allowed": false,
		"next_owner":            "agent",
		"next_action":           "Execute.",
	}
	note := Note{Data: data, Body: v7TaskBody("APP-T-0001", "Frontmatter warning"), RelativePath: "work/tasks/APP-T-0001.md"}
	errs, warns := validateV7Note(note, validationContext{VaultPath: vault, RelativePath: note.RelativePath}, note.RelativePath)
	if len(errs) != 0 || !issuesContainCode(warns, "FRONTMATTER_LONG") {
		t.Fatalf("expected configured frontmatter warning without errors, errs=%#v warns=%#v", errs, warns)
	}
}

func TestV7ValidationWarnsOnVagueAcceptanceCriteria(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "tusker")
	if err := bootstrap(Args{"vault": vault, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	data := map[string]any{
		"schema":                "tusker.task/v7",
		"kind":                  "task",
		"id":                    "APP-T-0001",
		"project":               "tusker",
		"title":                 "Acceptance quality",
		"epic":                  "APP",
		"status":                "ready",
		"readiness":             "ready",
		"priority":              "p2",
		"risk":                  "low",
		"proof_mode":            "inline",
		"proof_status":          "pending",
		"proof_required":        []string{"focused_test"},
		"evidence_budget":       0,
		"raw_artifacts_allowed": false,
		"next_owner":            "agent",
		"next_action":           "Execute the task contract.",
	}
	body := strings.Join([]string{
		"## Acceptance",
		"",
		"- [ ] Works",
		"- [ ] Tests pass",
		"",
		"## Verification",
		"",
		"- Focused validator test.",
	}, "\n")
	note := Note{Data: data, Body: body, RelativePath: "work/tasks/APP-T-0001.md"}

	errs, warns := validateV7Note(note, validationContext{VaultPath: vault, RelativePath: note.RelativePath}, note.RelativePath)
	if len(errs) != 0 {
		t.Fatalf("expected warnings only, got errors %#v", errs)
	}
	if !issuesContainCode(warns, "ACCEPTANCE_TOO_VAGUE") {
		t.Fatalf("expected vague acceptance warning, got %#v", warns)
	}
	if !issuesContainCode(warns, "VERIFICATION_PROOF_MISSING") {
		t.Fatalf("expected verification proof warning, got %#v", warns)
	}

	tableBody := strings.Join([]string{
		"## Acceptance",
		"",
		"| ID | Outcome | Proof |",
		"|---|---|---|",
		"| A1 | Tests pass | Automated test |",
	}, "\n")
	if vague := v7VagueAcceptanceItems(tableBody); len(vague) != 1 || vague[0] != "Tests pass" {
		t.Fatalf("expected table acceptance parser to flag Tests pass, got %#v", vague)
	}
}

func TestV7ValidationRejectsSecretsInAnyRecordKind(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "tusker")
	if err := bootstrap(Args{"vault": vault, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	evidence := Note{
		Data: map[string]any{
			"schema":        "tusker.evidence/v1",
			"kind":          "evidence",
			"id":            "APP-T-0001-E-0001",
			"project":       "tusker",
			"task":          "APP-T-0001",
			"evidence_kind": "manual_smoke",
			"status":        "accepted",
			"created_by":    "agent:test",
			"created_at":    "2026-05-13T05:00:00Z",
		},
		Body:         "## Summary\n\npassword = definitelysecret\n",
		RelativePath: "evidence/APP-T-0001/APP-T-0001-E-0001.md",
	}
	errs, _ := validateV7Note(evidence, validationContext{VaultPath: vault, RelativePath: evidence.RelativePath}, evidence.RelativePath)
	if !issuesContainCode(errs, "SECRET_IN_RECORD") {
		t.Fatalf("expected evidence secret scan failure, got %#v", errs)
	}

	gate := Note{
		Data: map[string]any{
			"schema":       "tusker.gate/v1",
			"kind":         "gate",
			"id":           "APP-G-0001",
			"project":      "tusker",
			"title":        "Credential gate",
			"gate_kind":    "auth",
			"status":       "open",
			"owner":        "human:sarav",
			"action":       "Use api_key: ABCDEFGHIJKLMNOPQRSTUVWX",
			"verification": "Provider endpoint returns ready.",
			"blocking":     true,
			"blocks":       []string{"APP-T-0001"},
		},
		Body:         "## Secret policy\n\nDo not store credentials.\n",
		RelativePath: "work/gates/APP-G-0001.md",
	}
	errs, _ = validateV7Note(gate, validationContext{VaultPath: vault, RelativePath: gate.RelativePath}, gate.RelativePath)
	if !issuesContainCode(errs, "SECRET_IN_RECORD") {
		t.Fatalf("expected gate frontmatter secret scan failure, got %#v", errs)
	}
}

func TestV7ValidationHardensEpicAndDecisionSchema(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "tusker")
	if err := bootstrap(Args{"vault": vault, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}

	epic := Note{
		Data: map[string]any{
			"schema":     "tusker.epic/v7",
			"kind":       "epic",
			"id":         "APP",
			"project":    "tusker",
			"title":      "App V7",
			"status":     "active",
			"owner":      "human:sarav",
			"priority":   "p1",
			"created_at": "2026-05-13T05:00:00Z",
			"updated_at": "2026-05-13T05:00:00Z",
			"state_rev":  "sha256:test",
		},
		Body:         "# APP\n",
		RelativePath: "work/epics/APP.md",
	}
	errs, _ := validateV7Note(epic, validationContext{VaultPath: vault, RelativePath: epic.RelativePath}, epic.RelativePath)
	if len(errs) != 0 {
		t.Fatalf("expected valid epic schema, got %#v", errs)
	}
	epic.Data["owner"] = ""
	errs, _ = validateV7Note(epic, validationContext{VaultPath: vault, RelativePath: epic.RelativePath}, "epics/APP.md")
	if !issuesContainCode(errs, errorMissingField) || !issuesContainCode(errs, errorPathMismatch) {
		t.Fatalf("expected missing owner and path mismatch for epic, got %#v", errs)
	}

	decision := Note{
		Data: map[string]any{
			"schema":  "tusker.decision/v1",
			"kind":    "decision",
			"id":      "APP-D-0001",
			"project": "tusker",
			"epic":    "APP",
			"title":   "Use V7",
			"status":  "accepted",
		},
		Body:         "# APP-D-0001\n",
		RelativePath: "work/decisions/APP-D-0001.md",
	}
	errs, _ = validateV7Note(decision, validationContext{VaultPath: vault, RelativePath: decision.RelativePath}, decision.RelativePath)
	if !issuesContainCode(errs, "DECISION_ACCEPTED_METADATA_MISSING") {
		t.Fatalf("expected accepted decision metadata failure, got %#v", errs)
	}
	decision.Data["decided_by"] = "human:sarav"
	decision.Data["decided_at"] = "2026-05-13T05:00:00Z"
	errs, _ = validateV7Note(decision, validationContext{VaultPath: vault, RelativePath: decision.RelativePath}, "work/tasks/APP-D-0001.md")
	if !issuesContainCode(errs, errorPathMismatch) {
		t.Fatalf("expected decision path mismatch, got %#v", errs)
	}
}

func TestV7ValidationHardensGateEvidenceAttemptAndProposalSchema(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "tusker")
	if err := bootstrap(Args{"vault": vault, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}

	gate := Note{
		Data: map[string]any{
			"schema":       "tusker.gate/v1",
			"kind":         "gate",
			"id":           "APP-G-0001",
			"project":      "tusker",
			"title":        "Review proof",
			"gate_kind":    "verification",
			"status":       "open",
			"owner":        "reviewer",
			"action":       "Review proof.",
			"verification": "Evidence accepted.",
			"blocking":     true,
			"blocks":       []string{"APP-T-0001"},
		},
		Body:         "# APP-G-0001\n",
		RelativePath: "work/gates/APP-G-0001.md",
	}
	errs, warns := validateV7Note(gate, validationContext{VaultPath: vault, RelativePath: gate.RelativePath}, gate.RelativePath)
	if len(errs) != 0 {
		t.Fatalf("expected valid gate schema, got %#v", errs)
	}
	if !issuesContainCode(warns, "GATE_VERIFICATION_PROOF_VAGUE") {
		t.Fatalf("expected verification proof warning, got %#v", warns)
	}
	gate.Data["schema"] = "tusker.task/v7"
	errs, _ = validateV7Note(gate, validationContext{VaultPath: vault, RelativePath: gate.RelativePath}, gate.RelativePath)
	if !issuesContainCode(errs, errorInvalidField) {
		t.Fatalf("expected gate schema failure, got %#v", errs)
	}
	gate.Data["schema"] = "tusker.gate/v1"
	gate.Data["gate_kind"] = "external_service"
	gate.Body = "# APP-G-0001\n\n## Steps\n\n1. Configure service.\n"
	errs, warns = validateV7Note(gate, validationContext{VaultPath: vault, RelativePath: gate.RelativePath}, gate.RelativePath)
	if len(errs) != 0 || !issuesContainCode(warns, "GATE_EXTERNAL_SERVICE_SETUP_MISSING") {
		t.Fatalf("expected external service setup warning without errors, errs=%#v warns=%#v", errs, warns)
	}

	evidence := Note{
		Data: map[string]any{
			"schema":        "tusker.evidence/v1",
			"kind":          "evidence",
			"id":            "APP-T-0001-E-0001",
			"project":       "tusker",
			"task":          "APP-T-0001",
			"evidence_kind": "automated_test",
			"status":        "accepted",
			"created_by":    "agent:test",
			"created_at":    "2026-05-13T05:00:00Z",
		},
		Body:         "# APP-T-0001-E-0001\n",
		RelativePath: "evidence/APP-T-0001/APP-T-0001-E-0001.md",
	}
	evidence.Data["kind"] = "task"
	errs, _ = validateV7Note(evidence, validationContext{VaultPath: vault, RelativePath: evidence.RelativePath}, evidence.RelativePath)
	if !issuesContainCode(errs, errorInvalidField) {
		t.Fatalf("expected evidence kind failure, got %#v", errs)
	}

	attempt := Note{
		Data: map[string]any{
			"schema":         "tusker.attempt/v1",
			"kind":           "attempt",
			"id":             "APP-T-0001-A-0001",
			"project":        "tusker",
			"task":           "APP-T-0001",
			"runner":         "codex",
			"workspace_kind": "same_checkout",
			"status":         "started",
			"started_at":     "2026-05-13T05:00:00Z",
		},
		Body:         "# APP-T-0001-A-0001\n",
		RelativePath: "work/tasks/APP-T-0001-A-0001.md",
	}
	errs, _ = validateV7Note(attempt, validationContext{VaultPath: vault, RelativePath: attempt.RelativePath}, attempt.RelativePath)
	if !issuesContainCode(errs, errorPathMismatch) {
		t.Fatalf("expected attempt path failure, got %#v", errs)
	}

	proposal := Note{
		Data: map[string]any{
			"schema":          "tusker.proposal/v1",
			"kind":            "proposal",
			"id":              "APP-P-0001",
			"project":         "tusker",
			"title":           "Propose close",
			"status":          "proposed",
			"action":          "close",
			"target_kind":     "task",
			"target":          "APP-T-0001",
			"proposed_by":     "agent:test",
			"created_at":      "2026-05-13T05:00:00Z",
			"proposed_fields": map[string]any{"status": "done"},
		},
		Body:         "## Rationale\n\nReady for review.\n",
		RelativePath: "work/inbox/APP-P-0001.md",
	}
	proposal.Data["status"] = "accepted"
	errs, _ = validateV7Note(proposal, validationContext{VaultPath: vault, RelativePath: proposal.RelativePath}, proposal.RelativePath)
	if !issuesContainCode(errs, "PROPOSAL_REVIEW_METADATA_MISSING") {
		t.Fatalf("expected proposal review metadata failure, got %#v", errs)
	}
	proposal.Data["status"] = "proposed"
	proposal.Data["schema"] = "tusker.decision/v1"
	errs, _ = validateV7Note(proposal, validationContext{VaultPath: vault, RelativePath: proposal.RelativePath}, proposal.RelativePath)
	if !issuesContainCode(errs, errorInvalidField) {
		t.Fatalf("expected proposal schema failure, got %#v", errs)
	}
}

func TestV7ValidationFlagsVagueAndMisroutedHumanGates(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "tusker")
	if err := bootstrap(Args{"vault": vault, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	cloneData := func(in map[string]any) map[string]any {
		out := map[string]any{}
		for key, value := range in {
			out[key] = value
		}
		return out
	}

	placeholderGate := Note{
		Data: map[string]any{
			"schema":       "tusker.gate/v1",
			"kind":         "gate",
			"id":           "APP-G-0001",
			"project":      "tusker",
			"title":        "Resolve gate for APP-T-0001",
			"gate_kind":    "manual_hold",
			"status":       "open",
			"owner":        "human:sarav",
			"action":       "Resolve this gate so blocked work can proceed.",
			"verification": "Owner confirms the gate is satisfied.",
			"blocking":     true,
			"blocks":       []string{"APP-T-0001"},
		},
		Body:         "# APP-G-0001\n",
		RelativePath: "work/gates/APP-G-0001.md",
	}
	errs, _ := validateV7Note(placeholderGate, validationContext{VaultPath: vault, RelativePath: placeholderGate.RelativePath}, placeholderGate.RelativePath)
	for _, code := range []string{"GATE_PLACEHOLDER_ACTION", "GATE_PLACEHOLDER_VERIFICATION", "GATE_MISSING_AGENT_BOUNDARY"} {
		if !issuesContainCode(errs, code) {
			t.Fatalf("expected %s for placeholder gate, got %#v", code, errs)
		}
	}

	reviewGate := placeholderGate
	reviewGate.Data = cloneData(placeholderGate.Data)
	reviewGate.Data["id"] = "APP-G-0002"
	reviewGate.Data["title"] = "Review code diff"
	reviewGate.Data["gate_kind"] = "verification"
	reviewGate.Data["owner"] = "human"
	reviewGate.Data["action"] = "Review code diff."
	reviewGate.Data["verification"] = "Human approves the diff."
	reviewGate.Data["why_agent_cannot"] = "Human should review the code."
	reviewGate.RelativePath = "work/gates/APP-G-0002.md"
	errs, _ = validateV7Note(reviewGate, validationContext{VaultPath: vault, RelativePath: reviewGate.RelativePath}, reviewGate.RelativePath)
	if !issuesContainCode(errs, "GATE_HUMAN_OWNS_AGENT_CAPABLE_WORK") {
		t.Fatalf("expected human-owned agent-capable gate failure, got %#v", errs)
	}

	decisionReviewGate := reviewGate
	decisionReviewGate.Data = cloneData(reviewGate.Data)
	decisionReviewGate.Data["id"] = "APP-G-0004"
	decisionReviewGate.Data["title"] = "Human reviews code changes"
	decisionReviewGate.Data["gate_kind"] = "decision"
	decisionReviewGate.Data["action"] = "Human reviews code changes."
	decisionReviewGate.Data["verification"] = "Decision is recorded on the task."
	decisionReviewGate.Data["suggestion"] = "Approve the diff."
	decisionReviewGate.RelativePath = "work/gates/APP-G-0004.md"
	errs, _ = validateV7Note(decisionReviewGate, validationContext{VaultPath: vault, RelativePath: decisionReviewGate.RelativePath}, decisionReviewGate.RelativePath)
	if !issuesContainCode(errs, "GATE_HUMAN_OWNS_AGENT_CAPABLE_WORK") {
		t.Fatalf("expected decision-gate code review loophole failure, got %#v", errs)
	}

	decisionGate := reviewGate
	decisionGate.Data = cloneData(reviewGate.Data)
	decisionGate.Data["id"] = "APP-G-0003"
	decisionGate.Data["title"] = "Choose API contract"
	decisionGate.Data["gate_kind"] = "decision"
	decisionGate.Data["action"] = "Choose frontend/backend API contract."
	decisionGate.Data["verification"] = "Decision is recorded on the task."
	decisionGate.Data["why_agent_cannot"] = "The spec conflicts with the current backend API and the agent cannot choose product intent."
	delete(decisionGate.Data, "suggestion")
	decisionGate.RelativePath = "work/gates/APP-G-0003.md"
	errs, _ = validateV7Note(decisionGate, validationContext{VaultPath: vault, RelativePath: decisionGate.RelativePath}, decisionGate.RelativePath)
	if !issuesContainCode(errs, "GATE_DECISION_SUGGESTION_MISSING") {
		t.Fatalf("expected decision suggestion failure, got %#v", errs)
	}
	decisionGate.Data["suggestion"] = "Align the frontend to the backend response field names unless the API change is intentional."
	decisionGate.Data["covers"] = []string{"A1"}
	errs, _ = validateV7Note(decisionGate, validationContext{VaultPath: vault, RelativePath: decisionGate.RelativePath}, decisionGate.RelativePath)
	if len(errs) != 0 {
		t.Fatalf("expected concrete decision gate to validate, got %#v", errs)
	}
}

func TestV7ScreenshotEvidenceRequiresCheckMetadata(t *testing.T) {
	previousWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	artifactRoot := t.TempDir()
	if err := os.Chdir(artifactRoot); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(previousWD); err != nil {
			t.Fatalf("restore working directory: %v", err)
		}
	})
	vault := filepath.Join(t.TempDir(), "vault")
	must := func(args Args, fn func(Args) error) {
		t.Helper()
		if err := fn(args); err != nil {
			t.Fatal(err)
		}
	}

	must(Args{"vault": vault, "quiet": "true"}, bootstrap)
	must(Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App V7", "v7": "true"}, newV7Epic)
	must(Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Capture UI proof", "risk": "low", "priority": "p2", "v7": "true"}, newV7Task)
	if err := writeText("provider-ready.png", "fake screenshot fixture\n"); err != nil {
		t.Fatal(err)
	}
	must(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "kind": "screenshot", "path": "./provider-ready.png", "covers": "A1", "redacted": "true", "redaction-note": "API key field cropped."}, evidenceV7AddCmd)

	path := filepath.Join(vault, "evidence", "APP-T-0001", "APP-T-0001-E-0001.md")
	data, body, err := parseFrontmatterMustRead(path)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "pending_review", stringField(data, "status"), "screenshot default status")
	if stringField(data, "screenshot_checked_by") != "" || stringField(data, "screenshot_checked_at") != "" {
		t.Fatal("pending screenshot should not be auto-checked")
	}
	if !boolField(data, "redacted") {
		t.Fatal("expected redacted flag")
	}

	err = evidenceV7AddCmd(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "kind": "screenshot", "path": "./provider-ready.png", "covers": "A1", "status": "accepted"})
	if err == nil {
		t.Fatal("expected accepted screenshot without checked-by to fail")
	}

	must(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "kind": "screenshot", "path": "./provider-ready.png", "covers": "A1", "status": "accepted", "checked-by": "human:sarav", "redacted": "true"}, evidenceV7AddCmd)
	acceptedPath := filepath.Join(vault, "evidence", "APP-T-0001", "APP-T-0001-E-0002.md")
	acceptedData, acceptedBody, err := parseFrontmatterMustRead(acceptedPath)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "human:sarav", stringField(acceptedData, "screenshot_checked_by"), "screenshot checked by")
	assertContainsIndexTest(t, normalizeList(acceptedData["artifact_paths"])[0], "evidence/APP-T-0001/artifacts/")

	delete(acceptedData, "screenshot_checked_by")
	delete(acceptedData, "screenshot_checked_at")
	note := Note{Data: data, Body: body, RelativePath: "evidence/APP-T-0001/APP-T-0001-E-0001.md"}
	note.Data = acceptedData
	note.Body = acceptedBody
	note.RelativePath = "evidence/APP-T-0001/APP-T-0001-E-0002.md"
	errs, _ := validateV7Note(note, validationContext{VaultPath: vault, RelativePath: note.RelativePath}, note.RelativePath)
	if !issuesContainCode(errs, errorScreenshotCheckMissing) {
		t.Fatalf("expected missing screenshot check metadata failure, got %#v", errs)
	}
}

func TestV7ValidationWarnsOnLargeEvidenceArtifacts(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	if err := bootstrap(Args{"vault": vault, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	artifactDir := filepath.Join(vault, "evidence", "APP-T-0001")
	if err := os.MkdirAll(artifactDir, 0o755); err != nil {
		t.Fatal(err)
	}
	artifactPath := filepath.Join(artifactDir, "large.bin")
	file, err := os.Create(artifactPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(v7LargeEvidenceWarnBytes + 1); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	note := Note{
		Data: map[string]any{
			"schema":         "tusker.evidence/v1",
			"kind":           "evidence",
			"id":             "APP-T-0001-E-0001",
			"project":        "tusker",
			"task":           "APP-T-0001",
			"evidence_kind":  "manual_smoke",
			"status":         "accepted",
			"covers":         []string{"TASK:A1"},
			"artifact_paths": []string{"large.bin", "external:s3://bucket/video.mov"},
			"accepted_by":    "human:sarav",
			"accepted_at":    "2026-05-13T05:01:00Z",
			"created_by":     "agent:test",
			"created_at":     "2026-05-13T05:00:00Z",
		},
		Body:         "# APP-T-0001-E-0001\n",
		RelativePath: "evidence/APP-T-0001/APP-T-0001-E-0001.md",
		AbsolutePath: filepath.Join(artifactDir, "APP-T-0001-E-0001.md"),
	}
	errs, warns := validateV7Note(note, validationContext{VaultPath: vault, RelativePath: note.RelativePath}, note.RelativePath)
	if len(errs) != 0 || !issuesContainCode(warns, "EVIDENCE_ARTIFACT_LARGE") {
		t.Fatalf("expected large artifact warning without errors, errs=%#v warns=%#v", errs, warns)
	}
}

func TestV7EvidenceAddCopiesArtifactsAndMarksExceptions(t *testing.T) {
	previousWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	repo := t.TempDir()
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(previousWD); err != nil {
			t.Fatalf("restore working directory: %v", err)
		}
	})
	vault := filepath.Join(repo, "tusker")
	must := func(args Args, fn func(Args) error) {
		t.Helper()
		if err := fn(args); err != nil {
			t.Fatal(err)
		}
	}

	must(Args{"vault": vault, "quiet": "true"}, bootstrap)
	must(Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App V7", "v7": "true"}, newV7Epic)
	must(Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Durable artifact", "risk": "low", "priority": "p2", "v7": "true"}, newV7Task)
	if err := writeText(filepath.Join(repo, "proof.txt"), "durable proof\n"); err != nil {
		t.Fatal(err)
	}

	must(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "kind": "manual_smoke", "status": "accepted", "accepted-by": "human:sarav", "covers": "A1", "path": "./proof.txt", "summary": "Artifact copied."}, evidenceV7AddCmd)
	data, _, err := parseFrontmatterMustRead(filepath.Join(vault, "evidence", "APP-T-0001", "APP-T-0001-E-0001.md"))
	if err != nil {
		t.Fatal(err)
	}
	paths := normalizeList(data["artifact_paths"])
	assertEqual(t, 1, len(paths), "copied artifact count")
	assertEqual(t, "evidence/APP-T-0001/artifacts/proof.txt", paths[0], "copied artifact path")
	assertExists(t, filepath.Join(vault, "evidence", "APP-T-0001", "artifacts", "proof.txt"))
	assertEqual(t, "copied", stringField(data, "artifact_durability"), "artifact durability")

	err = evidenceV7AddCmd(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "kind": "manual_smoke", "status": "accepted", "accepted-by": "human:sarav", "covers": "A1", "path": "/tmp/proof.txt", "summary": "Tmp artifact."})
	if err == nil {
		t.Fatal("expected /tmp artifact to be rejected by default")
	}
	must(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "kind": "manual_smoke", "status": "accepted", "accepted-by": "human:sarav", "covers": "A1", "path": "/tmp/proof.txt", "link-only": "true", "summary": "Intentional non-durable link."}, evidenceV7AddCmd)
	linkData, _, err := parseFrontmatterMustRead(filepath.Join(vault, "evidence", "APP-T-0001", "APP-T-0001-E-0002.md"))
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "link_only", stringField(linkData, "artifact_durability"), "link-only durability")
	assertContainsIndexTest(t, normalizeList(linkData["artifact_paths"])[0], "link-only:/tmp/proof.txt")

	must(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "kind": "manual_smoke", "status": "accepted", "accepted-by": "human:sarav", "covers": "A1", "external-url": "https://ci.example.test/run/1", "summary": "External proof."}, evidenceV7AddCmd)
	externalData, _, err := parseFrontmatterMustRead(filepath.Join(vault, "evidence", "APP-T-0001", "APP-T-0001-E-0003.md"))
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "external", stringField(externalData, "artifact_durability"), "external durability")
	assertEqual(t, "external:https://ci.example.test/run/1", normalizeList(externalData["artifact_paths"])[0], "external artifact")
}

func TestV7ReviewEvidenceKindsDefaultPendingAndRequireAcceptor(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	must := func(args Args, fn func(Args) error) {
		t.Helper()
		if err := fn(args); err != nil {
			t.Fatal(err)
		}
	}

	must(Args{"vault": vault, "quiet": "true"}, bootstrap)
	must(Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App V7", "v7": "true"}, newV7Epic)
	must(Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Review evidence", "risk": "medium", "priority": "p2", "v7": "true"}, newV7Task)
	must(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "kind": "manual_smoke", "covers": "A1", "summary": "Manual proof awaiting review."}, evidenceV7AddCmd)
	data, _, err := parseFrontmatterMustRead(filepath.Join(vault, "evidence", "APP-T-0001", "APP-T-0001-E-0001.md"))
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "pending_review", stringField(data, "status"), "manual evidence default status")

	err = evidenceV7AddCmd(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "kind": "video", "status": "accepted", "covers": "A1", "summary": "Video reviewed."})
	if err == nil {
		t.Fatal("expected accepted review evidence without acceptor to fail")
	}
	if !strings.Contains(err.Error(), "requires --accepted-by") {
		t.Fatalf("expected accepted-by error, got %v", err)
	}
	must(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "kind": "video", "status": "accepted", "accepted-by": "reviewer:agent", "covers": "A1", "summary": "Video reviewed."}, evidenceV7AddCmd)
}

func TestV7ValidationReadsConfiguredStrictSwitches(t *testing.T) {
	repo := t.TempDir()
	vault := filepath.Join(repo, "tusker")
	if err := bootstrap(Args{"vault": vault, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	data := map[string]any{
		"schema":      "tusker.task/v7",
		"kind":        "task",
		"id":          "APP-T-0001",
		"project":     "tusker",
		"title":       "Configured strict switches",
		"epic":        "APP",
		"status":      "ready",
		"readiness":   "ready",
		"priority":    "p2",
		"risk":        "low",
		"next_owner":  "agent",
		"next_action": "Execute the task contract.",
	}
	body := strings.Join([]string{
		"## Acceptance",
		"",
		"- [ ] Works",
		"",
		"## Work Log",
		"",
		"FAIL test one",
		"FAIL test two",
		"FAIL test three",
		"FAIL test four",
		"FAIL test five",
	}, "\n")
	note := Note{Data: data, Body: body, RelativePath: "work/tasks/APP-T-0001.md"}

	errs, warns := validateV7Note(note, validationContext{VaultPath: vault, RelativePath: note.RelativePath}, note.RelativePath)
	if !issuesContainCode(errs, "TASK_WORK_LOG_SECTION") || !issuesContainCode(errs, "TASK_RAW_LOG_IN_BODY") {
		t.Fatalf("expected default strict body errors, got %#v", errs)
	}
	if !issuesContainCode(warns, "ACCEPTANCE_PROOF_MISSING") {
		t.Fatalf("expected default acceptance proof warning, got %#v", warns)
	}

	if err := writeText(filepath.Join(repo, "tusker.yaml"), strings.Join([]string{
		"validation:",
		"  require_acceptance_proof: false",
		"  forbid_work_log_section: false",
		"  forbid_raw_logs_in_task: false",
	}, "\n")); err != nil {
		t.Fatal(err)
	}
	errs, warns = validateV7Note(note, validationContext{VaultPath: vault, RelativePath: note.RelativePath}, note.RelativePath)
	if issuesContainCode(errs, "TASK_WORK_LOG_SECTION") || issuesContainCode(errs, "TASK_RAW_LOG_IN_BODY") {
		t.Fatalf("did not expect disabled strict body errors, got %#v", errs)
	}
	if issuesContainCode(warns, "ACCEPTANCE_PROOF_MISSING") {
		t.Fatalf("did not expect disabled acceptance proof warning, got %#v", warns)
	}
}

func TestV7SaveCASRejectsStaleBaseRevision(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	must := func(args Args, fn func(Args) error) {
		t.Helper()
		if err := fn(args); err != nil {
			t.Fatal(err)
		}
	}

	must(Args{"vault": vault, "quiet": "true"}, bootstrap)
	must(Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App V7", "summary": "V7 tracker smoke.", "v7": "true"}, newV7Epic)
	must(Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "CAS conflict", "risk": "low", "priority": "p2", "v7": "true"}, newV7Task)
	taskPath := filepath.Join(vault, "work", "tasks", "APP-T-0001.md")
	data, body, err := parseFrontmatterMustRead(taskPath)
	if err != nil {
		t.Fatal(err)
	}
	baseRev := stringField(data, "state_rev")

	currentData, currentBody, err := parseFrontmatterMustRead(taskPath)
	if err != nil {
		t.Fatal(err)
	}
	currentData["next_action"] = "Concurrent control update."
	if _, err := saveV7DocumentCAS(taskPath, currentData, currentBody, v7FrontmatterOrder["task"], stringField(currentData, "state_rev")); err != nil {
		t.Fatal(err)
	}

	data["next_action"] = "Stale write should fail."
	if _, err := saveV7DocumentCAS(taskPath, data, body, v7FrontmatterOrder["task"], baseRev); err == nil {
		t.Fatal("expected stale CAS save to fail")
	} else if !strings.Contains(err.Error(), "changed since it was loaded") {
		t.Fatalf("expected stale CAS conflict, got %v", err)
	}

	must(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "runner": "codex"}, attemptV7StartCmd)
	attemptPath := filepath.Join(vault, "attempts", "APP-T-0001", "APP-T-0001-A-0001.md")
	attemptData, attemptBody, err := parseFrontmatterMustRead(attemptPath)
	if err != nil {
		t.Fatal(err)
	}
	attemptBaseRev := stringField(attemptData, "state_rev")
	currentAttemptData, currentAttemptBody, err := parseFrontmatterMustRead(attemptPath)
	if err != nil {
		t.Fatal(err)
	}
	currentAttemptData["status"] = "handoff"
	if _, err := saveV7DocumentCAS(attemptPath, currentAttemptData, currentAttemptBody, v7FrontmatterOrder["attempt"], stringField(currentAttemptData, "state_rev")); err != nil {
		t.Fatal(err)
	}
	attemptData["status"] = "failed"
	if _, err := saveV7DocumentCAS(attemptPath, attemptData, attemptBody, v7FrontmatterOrder["attempt"], attemptBaseRev); err == nil {
		t.Fatal("expected stale attempt CAS save to fail")
	} else if !strings.Contains(err.Error(), "changed since it was loaded") {
		t.Fatalf("expected stale attempt CAS conflict, got %v", err)
	}
}

func TestV7SaveCASRejectsOnDiskBodyEditWithStaleStateRev(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	must := func(args Args, fn func(Args) error) {
		t.Helper()
		if err := fn(args); err != nil {
			t.Fatal(err)
		}
	}

	must(Args{"vault": vault, "quiet": "true"}, bootstrap)
	must(Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App V7", "summary": "V7 tracker smoke.", "v7": "true"}, newV7Epic)
	must(Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "CAS stale body", "risk": "low", "priority": "p2", "v7": "true"}, newV7Task)
	taskPath := filepath.Join(vault, "work", "tasks", "APP-T-0001.md")
	data, body, err := parseFrontmatterMustRead(taskPath)
	if err != nil {
		t.Fatal(err)
	}
	baseRev := stringField(data, "state_rev")
	staleBody := body + "\n## Manual contract detail\n\nThis edit bypasses Tusker CAS.\n"
	content, err := serializeDocument(data, staleBody, v7FrontmatterOrder["task"])
	if err != nil {
		t.Fatal(err)
	}
	if err := writeText(taskPath, content); err != nil {
		t.Fatal(err)
	}

	data["next_action"] = "This stale writer should fail."
	if _, err := saveV7DocumentCAS(taskPath, data, body, v7FrontmatterOrder["task"], baseRev); err == nil {
		t.Fatal("expected stale on-disk state_rev to fail CAS")
	} else if !strings.Contains(err.Error(), "content changed without a refreshed state_rev") {
		t.Fatalf("expected stale state_rev CAS conflict, got %v", err)
	}
}

func TestV7MarkdownStoreLoadListSaveCASAndAppendEvent(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	must := func(args Args, fn func(Args) error) {
		t.Helper()
		if err := fn(args); err != nil {
			t.Fatal(err)
		}
	}

	must(Args{"vault": vault, "quiet": "true"}, bootstrap)
	must(Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App V7", "summary": "V7 tracker smoke.", "v7": "true"}, newV7Epic)
	must(Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Store object", "risk": "low", "priority": "p2", "v7": "true"}, newV7Task)

	ctx := context.Background()
	var store v7Store = v7MarkdownStore{VaultPath: vault}
	refs, err := store.List(ctx, v7Query{Kind: "task"})
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 1 {
		t.Fatalf("expected one task ref, got %#v", refs)
	}
	assertEqual(t, v7ObjectID("APP-T-0001"), refs[0].ID, "store task ref id")

	obj, err := store.Load(ctx, "APP-T-0001")
	if err != nil {
		t.Fatal(err)
	}
	taskObj, ok := obj.(v7MarkdownObject)
	if !ok {
		t.Fatalf("expected markdown object, got %T", obj)
	}
	baseRev := taskObj.Rev()
	taskObj.Data["next_action"] = "Updated through the V7 markdown store."
	nextRev, err := store.SaveCAS(ctx, taskObj, baseRev)
	if err != nil {
		t.Fatal(err)
	}
	if nextRev == "" || nextRev == baseRev {
		t.Fatalf("expected new revision, base=%s next=%s", baseRev, nextRev)
	}
	if _, err := store.SaveCAS(ctx, taskObj, baseRev); err == nil {
		t.Fatal("expected stale store SaveCAS to fail")
	}
	if err := store.AppendEvent(ctx, v7Event{
		ObjectID:   "APP-T-0001",
		ObjectKind: "task",
		EventKind:  "updated",
		Actor:      "agent:test",
		Payload:    map[string]any{"source": "store-test"},
	}); err != nil {
		t.Fatal(err)
	}
	eventErrs, _, eventCount := validateV7Events(vault)
	if len(eventErrs) != 0 {
		t.Fatalf("expected store event to validate, got %#v", eventErrs)
	}
	if eventCount == 0 {
		t.Fatal("expected store event to be written")
	}
	events, err := store.GetEvents(ctx, v7EventScope{ObjectID: "APP-T-0001", EventKind: "updated"})
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, 1, len(events), "store event count")
	assertEqual(t, "APP-T-0001", events[0].ObjectID, "store event object")
	assertEqual(t, "updated", events[0].EventKind, "store event kind")
	assertEqual(t, "agent:test", events[0].Actor, "store event actor")
	assertContainsIndexTest(t, events[0].Path, "events/")
	assertEqual(t, "store-test", stringField(events[0].Payload, "source"), "store event payload")
}

func TestV7DomainNewCreatesKnowledgeDomainLayoutAndValidates(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	if err := bootstrap(Args{"vault": vault, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	if err := domainNewCmd(Args{
		"vault":   vault,
		"quiet":   "true",
		"v7":      "true",
		"id":      "providers",
		"title":   "Providers",
		"summary": "Provider integrations.",
	}); err != nil {
		t.Fatal(err)
	}

	indexPath := filepath.Join(vault, "knowledge", "domains", "providers", "INDEX.md")
	canonPath := filepath.Join(vault, "knowledge", "domains", "providers", "CANON.md")
	assertExists(t, indexPath)
	assertExists(t, canonPath)
	assertExists(t, filepath.Join(vault, "knowledge", "domains", "providers", "runbooks"))
	assertExists(t, filepath.Join(vault, "knowledge", "domains", "providers", "decisions"))
	assertExists(t, filepath.Join(vault, "knowledge", "domains", "providers", "interfaces"))
	assertExists(t, filepath.Join(vault, "knowledge", "domains", "providers", "invariants"))
	assertExists(t, filepath.Join(vault, "knowledge", "domains", "providers", "sources"))
	assertExists(t, filepath.Join(vault, "knowledge", "domains", "providers", "glossary.md"))
	if _, err := os.Stat(filepath.Join(vault, "knowledge", "domains", "providers", "references")); !os.IsNotExist(err) {
		t.Fatalf("V7 domain layout must not create references/: %v", err)
	}

	indexData, indexBody, err := parseFrontmatterMustRead(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "tusker.domain/v7", stringField(indexData, "schema"), "domain schema")
	assertEqual(t, "domain", stringField(indexData, "kind"), "domain kind")
	assertEqual(t, "providers", stringField(indexData, "id"), "domain id")
	if !strings.Contains(indexBody, "## Read This When") || !strings.Contains(indexBody, "## Invariants") {
		t.Fatalf("domain INDEX missing expected routing sections:\n%s", indexBody)
	}
	canonData, canonBody, err := parseFrontmatterMustRead(canonPath)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "tusker.domain-canon/v7", stringField(canonData, "schema"), "domain canon schema")
	assertEqual(t, "domain_canon", stringField(canonData, "kind"), "domain canon kind")
	assertEqual(t, "providers", stringField(canonData, "domain"), "domain canon domain")
	assertEqual(t, "providers/canon", stringField(canonData, "id"), "domain canon id")
	if !strings.Contains(canonBody, "## Current Truth") || !strings.Contains(canonBody, "## Deprecated Or Stale") {
		t.Fatalf("domain CANON missing expected canon sections:\n%s", canonBody)
	}

	listOutput := captureStdout(t, func() {
		if err := domainListCmd(Args{"vault": vault, "v7": "true"}); err != nil {
			t.Fatal(err)
		}
	})
	assertContainsIndexTest(t, listOutput, "providers")
	showOutput := captureStdout(t, func() {
		if err := domainShowCmd(Args{"vault": vault, "v7": "true", "id": "providers"}); err != nil {
			t.Fatal(err)
		}
	})
	assertContainsIndexTest(t, showOutput, "Read this when:")
	canonOutput := captureStdout(t, func() {
		if err := domainCanonCmd(Args{"vault": vault, "v7": "true", "id": "providers"}); err != nil {
			t.Fatal(err)
		}
	})
	assertContainsIndexTest(t, canonOutput, "domain canon")

	store := v7MarkdownStore{VaultPath: vault}
	events, err := store.GetEvents(context.Background(), v7EventScope{ObjectID: "providers", ObjectKind: "domain", EventKind: "created"})
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, 1, len(events), "domain created event count")
	if code, err := validateCmd(Args{"vault": vault, "json": "true"}); err != nil || code != 0 {
		t.Fatalf("validate failed: code=%d err=%v", code, err)
	}
}

func TestV7ValidationRejectsDiataxisKnowledgeAndReferences(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	if err := bootstrapV7Profile(vault, "v7"); err != nil {
		t.Fatal(err)
	}
	if err := writeText(filepath.Join(vault, "knowledge", "domains", "project", "sources", "tutorial.md"), strings.Join([]string{
		"---",
		"schema: tusker.knowledge/v7",
		"kind: tutorial",
		"id: project/tutorial",
		"mode: how-to",
		"---",
		"# Tutorial",
		"",
		"Legacy docs taxonomy.",
		"",
	}, "\n")); err != nil {
		t.Fatal(err)
	}
	if err := writeText(filepath.Join(vault, "knowledge", "domains", "project", "references", "legacy.md"), strings.Join([]string{
		"---",
		"schema: tusker.knowledge/v7",
		"kind: source",
		"id: project/legacy-reference",
		"---",
		"# Legacy reference",
		"",
		"Raw material in the old directory.",
		"",
	}, "\n")); err != nil {
		t.Fatal(err)
	}

	code, err := validateCmd(Args{"vault": vault, "json": "true"})
	if err != nil {
		t.Fatal(err)
	}
	if code == 0 {
		t.Fatal("expected validate to reject Diataxis knowledge kind/mode and references/")
	}
}

func TestV7DashboardBuildIsDeterministicAndValidateRejectsStaleProjection(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	if err := bootstrap(Args{"vault": vault, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	if err := newV7Epic(Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App V7", "summary": "V7 tracker smoke.", "v7": "true"}); err != nil {
		t.Fatal(err)
	}
	if err := newV7Task(Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Dashboard task", "risk": "low", "priority": "p2", "v7": "true"}); err != nil {
		t.Fatal(err)
	}
	if err := dashboardV7Cmd(Args{"vault": vault, "quiet": "true", "_pos0": "build"}); err != nil {
		t.Fatal(err)
	}
	first := map[string]string{}
	for rel := range v7CommittedDashboardProjections(mustIndex(t, vault), nil) {
		raw, err := readText(filepath.Join(vault, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		first[rel] = raw
	}
	if err := dashboardV7Cmd(Args{"vault": vault, "quiet": "true", "_pos0": "build"}); err != nil {
		t.Fatal(err)
	}
	for rel, before := range first {
		after, err := readText(filepath.Join(vault, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		assertEqual(t, before, after, rel+" deterministic output")
	}
	if err := writeText(filepath.Join(vault, "dashboards", "agent-ready.md"), "# stale\n"); err != nil {
		t.Fatal(err)
	}
	code, err := validateCmd(Args{"vault": vault, "json": "true"})
	if err != nil {
		t.Fatal(err)
	}
	if code == 0 {
		t.Fatal("expected validate to reject stale committed dashboard projection")
	}
}

func TestV7ReindexRefreshesDashboardBasesAndSummary(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	if err := bootstrap(Args{"vault": vault, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	if err := newV7Epic(Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App V7", "summary": "V7 tracker smoke.", "v7": "true"}); err != nil {
		t.Fatal(err)
	}
	if err := newV7Task(Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Imported held task", "status": "backlog", "readiness": "held", "risk": "low", "priority": "p2", "v7": "true"}); err != nil {
		t.Fatal(err)
	}
	if err := writeText(filepath.Join(vault, "Dashboard.md"), "# Dashboard\n\nLegacy dashboard generation was removed from the V7-only build.\n"); err != nil {
		t.Fatal(err)
	}
	if err := reindex(Args{"vault": vault, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}

	dashboard := mustReadIndexTest(t, filepath.Join(vault, "Dashboard.md"))
	assertContainsIndexTest(t, dashboard, "<!-- tusker:v7-dashboard:landing;")
	assertContainsIndexTest(t, dashboard, "![[_generated/bases/tasks.base#Backlog]]")
	assertNotContainsIndexTest(t, dashboard, "Legacy dashboard generation was removed")
	assertNotContainsIndexTest(t, dashboard, "## Docs catalog")
	assertNotContainsIndexTest(t, dashboard, dashboardRunsBegin)
	assertExists(t, filepath.Join(vault, "dashboards", "human-actions.md"))
	assertExists(t, filepath.Join(vault, "_generated", "bases", "tasks.base"))
	assertExists(t, filepath.Join(vault, "_generated", "bases", "epics.base"))
	assertExists(t, filepath.Join(vault, "_generated", "bases", "backlog.base"))
	assertExists(t, filepath.Join(vault, "_generated", "indexes", "tasks.json"))

	var summary map[string]any
	rawSummary := mustReadIndexTest(t, filepath.Join(vault, "_generated", "indexes", "summary.json"))
	if err := json.Unmarshal([]byte(rawSummary), &summary); err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "tusker.summary/v7", stringValue(summary["schema"]), "summary schema")
	counts, ok := summary["counts"].(map[string]any)
	if !ok {
		t.Fatalf("summary counts missing or malformed: %#v", summary["counts"])
	}
	assertEqual(t, "1", fmt.Sprintf("%.0f", counts["tasks"].(float64)), "summary V7 task count")
	assertEqual(t, "1", fmt.Sprintf("%.0f", counts["epics"].(float64)), "summary V7 epic count")

	for _, rel := range []string{
		"_generated/bases/tasks.base",
		"_generated/bases/epics.base",
		"_generated/bases/agent-ready.base",
		"_generated/bases/human-actions.base",
	} {
		var base map[string]any
		raw := mustReadIndexTest(t, filepath.Join(vault, filepath.FromSlash(rel)))
		if err := yaml.Unmarshal([]byte(raw), &base); err != nil {
			t.Fatalf("%s is not valid YAML: %v\n%s", rel, err, raw)
		}
		if _, ok := base["views"]; !ok {
			t.Fatalf("%s missing views section:\n%s", rel, raw)
		}
	}
}

func TestV7SpecObjectCreationCLIForms(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	if err := bootstrap(Args{"vault": vault, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	command, args := parseCLI([]string{"tusker", "new", "epic", "APP", "--vault", vault, "--quiet", "--title", "App V7"})
	assertEqual(t, "new epic", command, "new epic command parse")
	if code, err := run(command, args); err != nil || code != 0 {
		t.Fatalf("new epic spec form failed: code=%d err=%v", code, err)
	}
	command, args = parseCLI([]string{"tusker", "new", "task", "--vault", vault, "--quiet", "--epic", "APP", "--title", "Spec task"})
	assertEqual(t, "new task", command, "new task command parse")
	if code, err := run(command, args); err != nil || code != 0 {
		t.Fatalf("new task spec form failed: code=%d err=%v", code, err)
	}

	epicPath := filepath.Join(vault, "work", "epics", "APP.md")
	taskPath := filepath.Join(vault, "work", "tasks", "APP-T-0001.md")
	assertExists(t, epicPath)
	assertExists(t, taskPath)
	epicData, _, err := parseFrontmatterMustRead(epicPath)
	if err != nil {
		t.Fatal(err)
	}
	taskData, _, err := parseFrontmatterMustRead(taskPath)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "tusker.epic/v7", stringField(epicData, "schema"), "spec form epic schema")
	assertEqual(t, "tusker.task/v7", stringField(taskData, "schema"), "spec form task schema")
	if code, err := validateCmd(Args{"vault": vault, "json": "true"}); err != nil || code != 0 {
		t.Fatalf("validate failed: code=%d err=%v", code, err)
	}
}

func TestV7HandoffAliasReadsSummaryFile(t *testing.T) {
	dir := t.TempDir()
	vault := filepath.Join(dir, "vault")
	if err := bootstrap(Args{"vault": vault, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	if err := newV7Epic(Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App V7", "summary": "V7 tracker smoke.", "v7": "true"}); err != nil {
		t.Fatal(err)
	}
	if err := newV7Task(Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Alias handoff", "risk": "low", "priority": "p2", "v7": "true"}); err != nil {
		t.Fatal(err)
	}
	if err := attemptV7StartCmd(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "runner": "codex"}); err != nil {
		t.Fatal(err)
	}
	summaryPath := filepath.Join(dir, "summary.md")
	if err := writeText(summaryPath, "Implemented through top-level handoff.\n\nEvidence is linked separately.\n"); err != nil {
		t.Fatal(err)
	}

	command, args := parseCLI([]string{"tusker", "handoff", "APP-T-0001", "--vault", vault, "--quiet", "--summary", summaryPath, "--no-review-proposal"})
	assertEqual(t, "handoff", command, "handoff command parse")
	if code, err := run(command, args); err != nil || code != 0 {
		t.Fatalf("handoff failed: code=%d err=%v", code, err)
	}
	attemptPath := filepath.Join(vault, "attempts", "APP-T-0001", "APP-T-0001-A-0001.md")
	data, body, err := parseFrontmatterMustRead(attemptPath)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "handoff", stringField(data, "status"), "attempt status")
	assertContainsIndexTest(t, body, "Implemented through top-level handoff.")
	if code, err := validateCmd(Args{"vault": vault, "json": "true"}); err != nil || code != 0 {
		t.Fatalf("validate failed: code=%d err=%v", code, err)
	}
}

func TestV7AttemptHandoffRequestsReviewWhenUnblocked(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	must := func(args Args, fn func(Args) error) {
		t.Helper()
		if err := fn(args); err != nil {
			t.Fatal(err)
		}
	}

	must(Args{"vault": vault, "quiet": "true"}, bootstrap)
	must(Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App V7", "summary": "V7 tracker smoke.", "v7": "true"}, newV7Epic)
	must(Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Handoff review", "risk": "low", "priority": "p2", "v7": "true"}, newV7Task)
	must(Args{"vault": vault, "quiet": "true", "_pos1": "APP-T-0001", "covers": "A1", "check": "go test ./cmd/tusker -run TestV7AttemptHandoff -count=1", "result": "pass", "note": "Handoff proof passed."}, verifyV7AddCmd)
	must(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "runner": "codex"}, attemptV7StartCmd)
	must(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "summary": "Implemented and ready."}, attemptV7HandoffCmd)

	data, _, err := parseFrontmatterMustRead(filepath.Join(vault, "work", "tasks", "APP-T-0001.md"))
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "review", stringField(data, "status"), "task status after handoff")
	assertEqual(t, "waiting_on_review", stringField(data, "readiness"), "task readiness after handoff")
}

func TestV7FinishRequiresProofAndRequestsReview(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	must := func(args Args, fn func(Args) error) {
		t.Helper()
		if err := fn(args); err != nil {
			t.Fatal(err)
		}
	}

	must(Args{"vault": vault, "quiet": "true"}, bootstrap)
	must(Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App V7", "summary": "V7 tracker smoke.", "v7": "true"}, newV7Epic)
	must(Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Finish review", "risk": "low", "priority": "p2", "evidence-required": "automated_test", "status": "ready", "readiness": "ready", "force-ready": "true", "v7": "true"}, newV7Task)
	must(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "runner": "codex"}, attemptV7StartCmd)

	err := finishV7Cmd(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "summary": "Implemented but proof is missing."})
	if err == nil {
		t.Fatal("expected finish without required evidence to fail")
	}
	if !strings.Contains(err.Error(), "finish proof incomplete") {
		t.Fatalf("expected proof-incomplete finish error, got %v", err)
	}
	data, _, err := parseFrontmatterMustRead(filepath.Join(vault, "work", "tasks", "APP-T-0001.md"))
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "ready", stringField(data, "status"), "task status after failed finish")
	attemptData, _, err := parseFrontmatterMustRead(filepath.Join(vault, "attempts", "APP-T-0001", "APP-T-0001-A-0001.md"))
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "started", stringField(attemptData, "status"), "attempt status after failed finish")

	must(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "kind": "automated_test", "covers": "A1", "summary": "Focused finish tests passed."}, evidenceV7AddCmd)
	must(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "summary": "Implemented with accepted proof."}, finishV7Cmd)
	data, _, err = parseFrontmatterMustRead(filepath.Join(vault, "work", "tasks", "APP-T-0001.md"))
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "review", stringField(data, "status"), "task status after finish")
	assertEqual(t, "waiting_on_review", stringField(data, "readiness"), "task readiness after finish")
}

func TestV7PacketIncludesDomainContextAndClosePolicy(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	if err := bootstrapV7Profile(vault, "v7"); err != nil {
		t.Fatal(err)
	}
	if err := domainNewCmd(Args{"vault": vault, "quiet": "true", "v7": "true", "id": "providers", "title": "Providers", "summary": "Provider integrations."}); err != nil {
		t.Fatal(err)
	}
	if err := newV7Epic(Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App V7", "summary": "V7 tracker smoke.", "v7": "true"}); err != nil {
		t.Fatal(err)
	}
	if err := newV7Task(Args{
		"vault":             vault,
		"quiet":             "true",
		"epic":              "APP",
		"title":             "Domain packet",
		"risk":              "critical",
		"priority":          "p1",
		"domains":           "providers",
		"evidence-required": "automated_test",
		"v7":                "true",
	}); err != nil {
		t.Fatal(err)
	}
	taskPath := filepath.Join(vault, "work", "tasks", "APP-T-0001.md")
	taskData, taskBody, err := parseFrontmatterMustRead(taskPath)
	if err != nil {
		t.Fatal(err)
	}
	packet := v7Packet(vault, Note{AbsolutePath: taskPath, Data: taskData, Body: taskBody}, mustIndex(t, vault), "agent")
	for _, expected := range []string{"## Project skill routing", "vault/SKILL.md", "knowledge/domains/providers/INDEX.md", "## Domain context", "Providers", "Current truth:", "## Verification", "## Close policy", "Required acceptor: human", "Required gates: release, security"} {
		assertContainsIndexTest(t, packet, expected)
	}
	reviewerPacket := v7Packet(vault, Note{AbsolutePath: taskPath, Data: taskData, Body: taskBody}, mustIndex(t, vault), "reviewer")
	for _, expected := range []string{"reviewer packet", "## Project skill routing", "## Domain context", "knowledge/domains/providers/CANON.md", "## Risk policy", "Required acceptor: human"} {
		assertContainsIndexTest(t, reviewerPacket, expected)
	}
	for _, forbidden := range []string{"## Stable Interfaces", "## Deprecated Or Stale"} {
		if strings.Contains(packet, forbidden) || strings.Contains(reviewerPacket, forbidden) {
			t.Fatalf("packet included unbounded domain canon section %q\nagent:\n%s\nreviewer:\n%s", forbidden, packet, reviewerPacket)
		}
	}
}

func TestV7SkillKnowledgeEndToEnd(t *testing.T) {
	previousWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(previousWD); err != nil {
			t.Fatalf("restore working directory: %v", err)
		}
	})
	repo := t.TempDir()
	vault := filepath.Join(repo, "tusker")
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}
	runCLI := func(argv ...string) string {
		t.Helper()
		full := append([]string{"tusker"}, argv...)
		var code int
		var runErr error
		output := captureStdout(t, func() {
			command, args := parseCLI(full)
			args["quiet"] = "true"
			code, runErr = run(command, args)
		})
		if runErr != nil || code != 0 {
			t.Fatalf("%s failed: code=%d err=%v output=%s", strings.Join(full, " "), code, runErr, output)
		}
		return output
	}

	runCLI("init", "--profile", "v7", "--vault", vault, "--yes", "--vault-only", "--no-mount")
	runCLI("domain", "new", "providers", "--v7", "--vault", vault, "--title", "Providers", "--summary", "Provider integrations.")
	runCLI("new", "epic", "APP", "--vault", vault, "--title", "App V7")
	runCLI("new", "task", "--vault", vault, "--epic", "APP", "--title", "Route provider work", "--domains", "providers", "--risk", "low", "--priority", "p2")
	makeV7TaskDispatchableForTest(t, vault, "APP-T-0001")
	agentPacket := runCLI("packet", "APP-T-0001", "--vault", vault, "--for", "agent")
	reviewerPacket := runCLI("packet", "APP-T-0001", "--vault", vault, "--for", "reviewer")
	out := filepath.Join(repo, "dist", "project-skill")
	runCLI("publish", "skill", "--vault", vault, "--v7", "--out", out)
	runCLI("validate", "--vault", vault)

	assertContainsIndexTest(t, agentPacket, "knowledge/domains/providers/INDEX.md")
	assertContainsIndexTest(t, reviewerPacket, "knowledge/domains/providers/CANON.md")
	assertExists(t, filepath.Join(out, "SKILL.md"))
	assertExists(t, filepath.Join(out, "knowledge", "domains", "providers", "CANON.md"))
	_, exportedBody, err := parseFrontmatterMustRead(filepath.Join(out, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	assertContainsIndexTest(t, exportedBody, "knowledge/domains/providers/INDEX.md")
	assertContainsIndexTest(t, exportedBody, "knowledge/domains/providers/CANON.md")
	for _, forbidden := range []string{"work", "evidence", "attempts", "events", "_generated", "Attachments"} {
		if _, err := os.Stat(filepath.Join(out, forbidden)); !os.IsNotExist(err) {
			t.Fatalf("end-to-end export included forbidden path %s: %v", forbidden, err)
		}
	}
}

func TestV7KnowledgeNewCreatesLeafNodesAndRejectsBadPaths(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	if err := bootstrapV7Profile(vault, "v7"); err != nil {
		t.Fatal(err)
	}
	if err := domainNewCmd(Args{"vault": vault, "quiet": "true", "v7": "true", "id": "providers", "title": "Providers", "summary": "Provider integrations."}); err != nil {
		t.Fatal(err)
	}
	kinds := map[string]string{
		"runbook":   "runbooks/oauth-refresh",
		"decision":  "decisions/auth-provider",
		"invariant": "invariants/token-storage",
		"interface": "interfaces/oauth-client",
		"glossary":  "glossary/access-token",
		"source":    "sources/provider-docs",
	}
	for kind, suffix := range kinds {
		if err := knowledgeNewCmd(Args{"vault": vault, "quiet": "true", "v7": "true", "node": "providers/" + suffix, "kind": kind, "title": "Provider " + kind}); err != nil {
			t.Fatalf("%s leaf create failed: %v", kind, err)
		}
		path := filepath.Join(vault, "knowledge", "domains", "providers", filepath.FromSlash(suffix+".md"))
		assertExists(t, path)
		data, body, err := parseFrontmatterMustRead(path)
		if err != nil {
			t.Fatal(err)
		}
		assertEqual(t, "tusker.knowledge/v7", stringField(data, "schema"), "leaf schema")
		assertEqual(t, kind, stringField(data, "kind"), "leaf kind")
		for _, expected := range []string{"## Read This When", "## Do Not Read This When", "## Source Truth", "## Related"} {
			assertContainsIndexTest(t, body, expected)
		}
	}
	for _, bad := range []Args{
		{"vault": vault, "quiet": "true", "v7": "true", "node": "missing/runbooks/x", "kind": "runbook", "title": "Missing domain"},
		{"vault": vault, "quiet": "true", "v7": "true", "node": "providers/references/raw", "kind": "source", "title": "Legacy path"},
		{"vault": vault, "quiet": "true", "v7": "true", "node": "providers/runbooks/not-interface", "kind": "interface", "title": "Wrong folder"},
	} {
		if err := knowledgeNewCmd(bad); err == nil {
			t.Fatalf("expected bad V7 knowledge create to fail: %#v", bad)
		}
	}
	if code, err := validateCmd(Args{"vault": vault, "json": "true"}); err != nil || code != 0 {
		t.Fatalf("validate failed: code=%d err=%v", code, err)
	}
}

func TestV7SkillDoctorRouteAndPack(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	if err := bootstrapV7Profile(vault, "v7"); err != nil {
		t.Fatal(err)
	}
	if err := domainNewCmd(Args{"vault": vault, "quiet": "true", "v7": "true", "id": "providers", "title": "Providers", "summary": "Provider integrations and auth refresh."}); err != nil {
		t.Fatal(err)
	}
	if err := newV7Epic(Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App V7", "summary": "V7 tracker smoke.", "v7": "true"}); err != nil {
		t.Fatal(err)
	}
	if err := newV7Task(Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Refresh provider auth", "domains": "providers", "risk": "low", "priority": "p2", "v7": "true"}); err != nil {
		t.Fatal(err)
	}
	makeV7TaskDispatchableForTest(t, vault, "APP-T-0001")
	if code, err := skillV7DoctorCmd(Args{"vault": vault, "strict": "true", "json": "true"}); err != nil || code != 0 {
		t.Fatalf("skill doctor failed: code=%d err=%v", code, err)
	}
	routeOutput := captureStdout(t, func() {
		if err := skillV7RouteCmd(Args{"vault": vault, "intent": "change provider auth refresh logic", "json": "true"}); err != nil {
			t.Fatal(err)
		}
	})
	assertContainsIndexTest(t, routeOutput, "knowledge/domains/providers/INDEX.md")
	assertContainsIndexTest(t, routeOutput, "knowledge/domains/providers/CANON.md")
	packOutput := captureStdout(t, func() {
		if err := skillV7PackCmd(Args{"vault": vault, "id": "APP-T-0001", "for": "agent", "budget": "6000"}); err != nil {
			t.Fatal(err)
		}
	})
	assertContainsIndexTest(t, packOutput, "# APP-T-0001 agent packet")
	assertContainsIndexTest(t, packOutput, "knowledge/domains/providers/INDEX.md")

	out := filepath.Join(t.TempDir(), "project-skill")
	if err := publishSkillCmd(Args{"vault": vault, "out": out, "v7": "true", "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	if code, err := skillV7DoctorCmd(Args{"package": out, "strict": "true", "json": "true"}); err != nil || code != 0 {
		t.Fatalf("package skill doctor failed: code=%d err=%v", code, err)
	}
}

func TestV7ProtectedFieldDiffDetectsStateMutation(t *testing.T) {
	before := `---
schema: tusker.task/v7
kind: task
id: APP-T-0001
project: tusker
status: ready
---
`
	after := strings.Replace(before, "status: ready", "status: done", 1)
	issues := protectedFieldIssuesForDiff(".tusker/work/tasks/APP-T-0001.md", "agent/APP-T-0001", before, after)
	if len(issues) != 1 {
		t.Fatalf("expected one protected field issue, got %d", len(issues))
	}
	assertEqual(t, "PROTECTED_FIELD_CHANGED", issues[0].Code, "issue code")
}

func TestV7ProtectedFieldDiffCoversAllStateObjects(t *testing.T) {
	cases := []struct {
		name   string
		path   string
		field  string
		before string
		after  string
	}{
		{
			name:   "epic status",
			path:   ".tusker/work/epics/APP.md",
			field:  "status",
			before: "schema: tusker.epic/v7\nkind: epic\nid: APP\nproject: tusker\nstatus: active\nstate_rev: old\n",
			after:  "schema: tusker.epic/v7\nkind: epic\nid: APP\nproject: tusker\nstatus: done\nstate_rev: old\n",
		},
		{
			name:   "decision acceptance",
			path:   ".tusker/work/decisions/APP-D-0001.md",
			field:  "decided_by",
			before: "schema: tusker.decision/v1\nkind: decision\nid: APP-D-0001\nproject: tusker\nstatus: proposed\nstate_rev: old\n",
			after:  "schema: tusker.decision/v1\nkind: decision\nid: APP-D-0001\nproject: tusker\nstatus: accepted\ndecided_by: human:sarav\nstate_rev: old\n",
		},
		{
			name:   "evidence acceptance",
			path:   "tusker/evidence/APP-T-0001/APP-T-0001-E-0001.md",
			field:  "accepted_at",
			before: "schema: tusker.evidence/v1\nkind: evidence\nid: APP-T-0001-E-0001\nproject: tusker\ntask: APP-T-0001\nstatus: proposed\nstate_rev: old\n",
			after:  "schema: tusker.evidence/v1\nkind: evidence\nid: APP-T-0001-E-0001\nproject: tusker\ntask: APP-T-0001\nstatus: accepted\naccepted_at: 2026-05-15T00:00:00Z\nstate_rev: old\n",
		},
		{
			name:   "attempt close marker",
			path:   "tusker/attempts/APP-T-0001/APP-T-0001-A-0001.md",
			field:  "ended_at",
			before: "schema: tusker.attempt/v1\nkind: attempt\nid: APP-T-0001-A-0001\nproject: tusker\ntask: APP-T-0001\nstatus: started\nstate_rev: old\n",
			after:  "schema: tusker.attempt/v1\nkind: attempt\nid: APP-T-0001-A-0001\nproject: tusker\ntask: APP-T-0001\nstatus: handoff\nended_at: 2026-05-15T00:00:00Z\nstate_rev: old\n",
		},
		{
			name:   "proposal application",
			path:   ".tusker/work/inbox/APP-P-0001.md",
			field:  "applied_at",
			before: "schema: tusker.proposal/v1\nkind: proposal\nid: APP-P-0001\nproject: tusker\nstatus: accepted\nstate_rev: old\n",
			after:  "schema: tusker.proposal/v1\nkind: proposal\nid: APP-P-0001\nproject: tusker\nstatus: accepted\napplied_at: 2026-05-15T00:00:00Z\nstate_rev: old\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			issues := protectedFieldIssuesForDiff(tc.path, "agent/APP-T-0001", "---\n"+tc.before+"---\n", "---\n"+tc.after+"---\n")
			if len(issues) == 0 {
				t.Fatalf("expected protected diff issue for %s", tc.field)
			}
			if !strings.Contains(issues[0].Message, tc.field) {
				t.Fatalf("expected issue for %s, got %#v", tc.field, issues)
			}
		})
	}
}

func TestV7BundledSkillOmitsObsidianStatusHooks(t *testing.T) {
	for _, path := range []string{
		filepath.Join("..", "..", "skill", "assets", "snippets", "status-hooks.js"),
		filepath.Join("..", "..", ".agents", "skills", "tusker", "assets", "snippets", "status-hooks.js"),
		filepath.Join("..", "..", ".claude", "skills", "tusker", "assets", "snippets", "status-hooks.js"),
	} {
		t.Run(path, func(t *testing.T) {
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Fatalf("expected bundled Obsidian status hook to be absent at %s, got err=%v", path, err)
			}
		})
	}
}

func TestV7GateControlEagerlyReconcilesTaskProjectionAndDashboards(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	must := func(args Args, fn func(Args) error) {
		t.Helper()
		if err := fn(args); err != nil {
			t.Fatal(err)
		}
	}

	must(Args{"vault": vault, "quiet": "true"}, bootstrap)
	must(Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App V7", "summary": "V7 tracker smoke.", "v7": "true"}, newV7Epic)
	must(Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Gate projection target", "risk": "low", "priority": "p2", "v7": "true"}, newV7Task)
	must(Args{"vault": vault, "quiet": "true", "blocks": "APP-T-0001", "kind": "auth", "owner": "human:sarav", "action": "Complete setup.", "verification": "Manual proof: setup complete.", "why-agent-cannot": "Human credentials or account access are required."}, newV7Gate)

	taskPath := filepath.Join(vault, "work", "tasks", "APP-T-0001.md")
	data, _, err := parseFrontmatterMustRead(taskPath)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "waiting_on_human", stringField(data, "readiness"), "readiness after gate create")
	assertEqual(t, "APP-G-0001", stringField(data, "next_ref"), "next ref after gate create")
	blockedRev := stringField(data, "state_rev")

	must(Args{"vault": vault, "quiet": "true", "local": "true", "id": "APP-G-0001", "by": "human:sarav", "evidence": "Setup complete."}, func(args Args) error {
		return gateV7Transition(args, "satisfied")
	})
	data, _, err = parseFrontmatterMustRead(taskPath)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "ready", stringField(data, "readiness"), "readiness after gate satisfy")
	assertEqual(t, "agent", stringField(data, "next_owner"), "next owner after gate satisfy")
	if stringField(data, "state_rev") == blockedRev {
		t.Fatal("expected gate control projection to update state_rev")
	}
	assertContainsIndexTest(t, mustReadIndexTest(t, filepath.Join(vault, "_generated", "indexes", "tasks.json")), `"readiness": "ready"`)
	dashboardRaw := mustReadIndexTest(t, filepath.Join(vault, "_generated", "indexes", "dashboard.json"))
	assertContainsIndexTest(t, dashboardRaw, `"human_actions": 0`)

	store := v7MarkdownStore{VaultPath: vault}
	events, err := store.GetEvents(context.Background(), v7EventScope{ObjectID: "APP-T-0001", EventKind: "updated"})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) == 0 {
		t.Fatal("expected eager projection update event")
	}
	assertEqual(t, "gate:APP-G-0001", stringField(events[len(events)-1].Payload, "source"), "projection event source")
}

func TestV7TaskStatusControlEagerlyReconcilesProjection(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	must := func(args Args, fn func(Args) error) {
		t.Helper()
		if err := fn(args); err != nil {
			t.Fatal(err)
		}
	}

	must(Args{"vault": vault, "quiet": "true"}, bootstrap)
	must(Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App V7", "summary": "V7 tracker smoke.", "v7": "true"}, newV7Epic)
	must(Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Status projection target", "risk": "low", "priority": "p2", "v7": "true"}, newV7Task)
	must(Args{"vault": vault, "quiet": "true", "local": "true", "id": "APP-T-0001", "status": "review", "by": "agent:codex"}, statusV7Cmd)

	data, _, err := parseFrontmatterMustRead(filepath.Join(vault, "work", "tasks", "APP-T-0001.md"))
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "review", stringField(data, "status"), "task status")
	assertEqual(t, "waiting_on_review", stringField(data, "readiness"), "task readiness")
	assertEqual(t, "reviewer", stringField(data, "next_owner"), "next owner")
	assertContainsIndexTest(t, mustReadIndexTest(t, filepath.Join(vault, "_generated", "indexes", "tasks.json")), `"readiness": "waiting_on_review"`)
}

func TestV7GateSatisfyRequiresEvidenceForBlockingGate(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	must := func(args Args, fn func(Args) error) {
		t.Helper()
		if err := fn(args); err != nil {
			t.Fatal(err)
		}
	}

	must(Args{"vault": vault, "quiet": "true"}, bootstrap)
	must(Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App V7", "summary": "V7 tracker smoke.", "v7": "true"}, newV7Epic)
	must(Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Gate evidence target", "risk": "low", "priority": "p2", "v7": "true"}, newV7Task)
	must(Args{"vault": vault, "quiet": "true", "blocks": "APP-T-0001", "kind": "verification", "owner": "reviewer", "action": "Review proof.", "verification": "Manual proof: evidence accepted."}, newV7Gate)

	err := gateV7Transition(Args{"vault": vault, "quiet": "true", "id": "APP-G-0001", "by": "reviewer:agent"}, "satisfied")
	if err == nil {
		t.Fatal("expected blocking gate satisfy without evidence to fail")
	}
	if !strings.Contains(err.Error(), "satisfy requires --evidence") {
		t.Fatalf("expected evidence requirement error, got %v", err)
	}

	must(Args{"vault": vault, "quiet": "true", "id": "APP-G-0001", "by": "reviewer:agent", "evidence": "Manual proof: evidence accepted.", "evidence-refs": "APP-T-0001-E-0001"}, func(args Args) error {
		return gateV7Transition(args, "satisfied")
	})
	data, _, err := parseFrontmatterMustRead(filepath.Join(vault, "work", "gates", "APP-G-0001.md"))
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "Manual proof: evidence accepted.", stringField(data, "satisfaction_evidence"), "satisfaction evidence")
	if !containsString(normalizeList(data["satisfaction_evidence_refs"]), "APP-T-0001-E-0001") {
		t.Fatalf("expected satisfaction evidence ref, got %#v", data["satisfaction_evidence_refs"])
	}
}

func TestV7ProposalCreatesInboxRecordAndValidates(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	must := func(args Args, fn func(Args) error) {
		t.Helper()
		if err := fn(args); err != nil {
			t.Fatal(err)
		}
	}

	must(Args{"vault": vault, "quiet": "true"}, bootstrap)
	must(Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App V7", "summary": "V7 tracker smoke.", "v7": "true"}, newV7Epic)
	must(Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Inbox proposal target", "risk": "low", "priority": "p2", "v7": "true"}, newV7Task)
	must(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "kind": "automated_test", "covers": "A1", "summary": "Focused proposal apply smoke passed."}, evidenceV7AddCmd)
	must(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "status": "review", "by": "agent:codex"}, statusV7Cmd)
	must(Args{"vault": vault, "quiet": "true", "_pos0": "close", "_pos1": "APP-T-0001", "reason": "Implementation branch is ready.", "validation": "Focused tests passed."}, proposalV7Cmd)

	proposalPath := filepath.Join(vault, "work", "inbox", "APP-P-0001.md")
	assertExists(t, proposalPath)
	data, body, err := parseFrontmatterMustRead(proposalPath)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "tusker.proposal/v1", stringField(data, "schema"), "proposal schema")
	assertEqual(t, "proposal", stringField(data, "kind"), "proposal kind")
	assertEqual(t, "close", stringField(data, "action"), "proposal action")
	assertEqual(t, "task", stringField(data, "target_kind"), "proposal target kind")
	assertEqual(t, "APP-T-0001", stringField(data, "target"), "proposal target")
	fields, ok := data["proposed_fields"].(map[string]any)
	if !ok {
		t.Fatalf("expected proposed_fields map, got %T", data["proposed_fields"])
	}
	assertEqual(t, "done", toString(fields["status"]), "proposal status field")
	assertContainsIndexTest(t, body, "Implementation branch is ready.")

	idx := mustIndex(t, vault)
	if _, ok := idx.Proposals["APP-P-0001"]; !ok {
		t.Fatal("expected proposal to be indexed")
	}
	code, err := validateCmd(Args{"vault": vault, "json": "true"})
	if err != nil {
		t.Fatal(err)
	}
	if code != 0 {
		t.Fatalf("expected proposal validation to pass, got code %d", code)
	}

	must(Args{"vault": vault, "quiet": "true", "_pos0": "accept", "_pos1": "APP-P-0001", "by": "human:sarav", "reason": "Maintainer accepted the branch-side proposal."}, proposalV7Cmd)
	data, _, err = parseFrontmatterMustRead(proposalPath)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "accepted", stringField(data, "status"), "proposal status after accept")
	assertEqual(t, "human:sarav", stringField(data, "reviewed_by"), "proposal reviewed by")
	if stringField(data, "reviewed_at") == "" {
		t.Fatal("expected reviewed_at")
	}
	code, err = validateCmd(Args{"vault": vault, "json": "true"})
	if err != nil {
		t.Fatal(err)
	}
	if code != 0 {
		t.Fatalf("expected accepted proposal validation to pass, got code %d", code)
	}

	must(Args{"vault": vault, "quiet": "true", "_pos0": "apply", "_pos1": "APP-P-0001", "by": "human:sarav"}, proposalV7Cmd)
	taskData, _, err := parseFrontmatterMustRead(filepath.Join(vault, "work", "tasks", "APP-T-0001.md"))
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "done", stringField(taskData, "status"), "proposal-applied task status")
	data, _, err = parseFrontmatterMustRead(proposalPath)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "human:sarav", stringField(data, "applied_by"), "proposal applied by")
	assertEqual(t, "APP-T-0001", stringField(data, "applied_target"), "proposal applied target")
	if stringField(data, "applying_at") == "" || stringField(data, "apply_transaction") == "" {
		t.Fatal("expected proposal applying metadata")
	}
	if stringField(data, "applied_at") == "" || stringField(data, "applied_target_rev") == "" {
		t.Fatal("expected proposal application metadata")
	}
}

func TestV7ProposalAcceptRequiresIndependentReviewer(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	must := func(args Args, fn func(Args) error) {
		t.Helper()
		if err := fn(args); err != nil {
			t.Fatal(err)
		}
	}

	must(Args{"vault": vault, "quiet": "true"}, bootstrap)
	must(Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App V7", "summary": "V7 tracker smoke.", "v7": "true"}, newV7Epic)
	must(Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Self review proposal", "risk": "low", "priority": "p2", "v7": "true"}, newV7Task)
	must(Args{"vault": vault, "quiet": "true", "_pos0": "status", "_pos1": "APP-T-0001", "status": "review", "by": "agent:codex"}, proposalV7Cmd)

	err := proposalV7Cmd(Args{"vault": vault, "quiet": "true", "_pos0": "accept", "_pos1": "APP-P-0001", "by": "agent:codex"})
	if err == nil {
		t.Fatal("expected self-accept to be rejected")
	}
	if !strings.Contains(err.Error(), "independent reviewer") {
		t.Fatalf("expected independent reviewer error, got %v", err)
	}
	must(Args{"vault": vault, "quiet": "true", "_pos0": "accept", "_pos1": "APP-P-0001", "by": "human:sarav"}, proposalV7Cmd)
}

func TestV7StatusRejectsDoneDirectly(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	must := func(args Args, fn func(Args) error) {
		t.Helper()
		if err := fn(args); err != nil {
			t.Fatal(err)
		}
	}

	must(Args{"vault": vault, "quiet": "true"}, bootstrap)
	must(Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App V7", "summary": "V7 tracker smoke.", "v7": "true"}, newV7Epic)
	must(Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Direct done", "risk": "low", "priority": "p2", "v7": "true"}, newV7Task)

	err := statusV7Cmd(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "status": "done"})
	if err == nil {
		t.Fatal("expected status done to be rejected")
	}
	if !strings.Contains(err.Error(), "use tusker close") {
		t.Fatalf("expected close-command hint, got %v", err)
	}
	data, _, err := parseFrontmatterMustRead(filepath.Join(vault, "work", "tasks", "APP-T-0001.md"))
	if err != nil {
		t.Fatal(err)
	}
	if stringField(data, "status") == "done" {
		t.Fatal("status command must not close the task")
	}
}

func TestV7CloseRequiresReviewStatus(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	must := func(args Args, fn func(Args) error) {
		t.Helper()
		if err := fn(args); err != nil {
			t.Fatal(err)
		}
	}

	must(Args{"vault": vault, "quiet": "true"}, bootstrap)
	must(Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App V7", "summary": "V7 tracker smoke.", "v7": "true"}, newV7Epic)
	must(Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Close from ready", "risk": "low", "priority": "p2", "v7": "true"}, newV7Task)
	must(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "kind": "automated_test", "covers": "A1", "summary": "Focused tests passed."}, evidenceV7AddCmd)

	err := closeV7Cmd(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "by": "reviewer:agent"})
	if err == nil {
		t.Fatal("expected close from ready to be rejected")
	}
	if !strings.Contains(err.Error(), "close requires status review") {
		t.Fatalf("expected review-status close error, got %v", err)
	}

	must(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "status": "review", "by": "agent:codex"}, statusV7Cmd)
	must(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "by": "reviewer:agent"}, closeV7Cmd)
}

func TestV7StatusProposalRejectsDone(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	must := func(args Args, fn func(Args) error) {
		t.Helper()
		if err := fn(args); err != nil {
			t.Fatal(err)
		}
	}

	must(Args{"vault": vault, "quiet": "true"}, bootstrap)
	must(Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App V7", "summary": "V7 tracker smoke.", "v7": "true"}, newV7Epic)
	must(Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Proposal done", "risk": "low", "priority": "p2", "v7": "true"}, newV7Task)

	err := proposalV7Cmd(Args{"vault": vault, "quiet": "true", "_pos0": "status", "_pos1": "APP-T-0001", "status": "done"})
	if err == nil {
		t.Fatal("expected status proposal for done to be rejected")
	}
	if !strings.Contains(err.Error(), "use propose close") {
		t.Fatalf("expected propose-close hint, got %v", err)
	}
	if fileExists(filepath.Join(vault, "work", "inbox", "APP-P-0001.md")) {
		t.Fatal("rejected status proposal should not create an inbox record")
	}
	data, _, err := parseFrontmatterMustRead(filepath.Join(vault, "work", "tasks", "APP-T-0001.md"))
	if err != nil {
		t.Fatal(err)
	}
	if stringField(data, "status") == "done" {
		t.Fatal("rejected status proposal must not close the task")
	}
}

func TestV7ProposalApplyCreatesGate(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	must := func(args Args, fn func(Args) error) {
		t.Helper()
		if err := fn(args); err != nil {
			t.Fatal(err)
		}
	}

	must(Args{"vault": vault, "quiet": "true"}, bootstrap)
	must(Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App V7", "summary": "V7 tracker smoke.", "v7": "true"}, newV7Epic)
	must(Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Gate proposal target", "risk": "low", "priority": "p2", "v7": "true"}, newV7Task)
	must(Args{"vault": vault, "quiet": "true", "_pos0": "create_gate", "_pos1": "APP-T-0001", "kind": "verification", "owner": "reviewer", "action": "Review branch proof.", "verification": "Manual proof: reviewer accepts linked evidence."}, proposalV7Cmd)
	must(Args{"vault": vault, "quiet": "true", "_pos0": "accept", "_pos1": "APP-P-0001", "by": "human:sarav"}, proposalV7Cmd)
	must(Args{"vault": vault, "quiet": "true", "_pos0": "apply", "_pos1": "APP-P-0001", "by": "human:sarav"}, proposalV7Cmd)
	must(Args{"vault": vault, "quiet": "true"}, reconcileV7Cmd)

	gatePath := filepath.Join(vault, "work", "gates", "APP-G-0001.md")
	assertExists(t, gatePath)
	gateData, _, err := parseFrontmatterMustRead(gatePath)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "verification", stringField(gateData, "gate_kind"), "created gate kind")
	assertEqual(t, "APP-T-0001", normalizeList(gateData["blocks"])[0], "created gate blocks")
	proposalData, _, err := parseFrontmatterMustRead(filepath.Join(vault, "work", "inbox", "APP-P-0001.md"))
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "APP-G-0001", stringField(proposalData, "applied_target"), "proposal applied target")
	code, err := validateCmd(Args{"vault": vault, "json": "true"})
	if err != nil {
		t.Fatal(err)
	}
	if code != 0 {
		t.Fatalf("expected create_gate proposal application to validate, got code %d", code)
	}
}

func TestV7ProposalApplyCreatesTaskAndDecision(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	must := func(args Args, fn func(Args) error) {
		t.Helper()
		if err := fn(args); err != nil {
			t.Fatal(err)
		}
	}

	must(Args{"vault": vault, "quiet": "true"}, bootstrap)
	must(Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App V7", "summary": "V7 tracker smoke.", "v7": "true"}, newV7Epic)
	must(Args{"vault": vault, "quiet": "true", "_pos0": "create_task", "epic": "APP", "title": "Proposed implementation task", "risk": "low", "priority": "p1", "evidence-required": "automated_test"}, proposalV7Cmd)
	must(Args{"vault": vault, "quiet": "true", "_pos0": "accept", "_pos1": "APP-P-0001", "by": "human:sarav"}, proposalV7Cmd)
	must(Args{"vault": vault, "quiet": "true", "_pos0": "apply", "_pos1": "APP-P-0001", "by": "human:sarav"}, proposalV7Cmd)
	assertExists(t, filepath.Join(vault, "work", "tasks", "APP-T-0001.md"))
	taskData, _, err := parseFrontmatterMustRead(filepath.Join(vault, "work", "tasks", "APP-T-0001.md"))
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "Proposed implementation task", stringField(taskData, "title"), "created task title")
	assertEqual(t, "automated_test", normalizeList(taskData["evidence_required"])[0], "created task evidence")

	must(Args{"vault": vault, "quiet": "true", "_pos0": "create_decision", "epic": "APP", "title": "Proposed architecture decision", "decision": "Use repo-local proposal application."}, proposalV7Cmd)
	must(Args{"vault": vault, "quiet": "true", "_pos0": "accept", "_pos1": "APP-P-0002", "by": "human:sarav"}, proposalV7Cmd)
	must(Args{"vault": vault, "quiet": "true", "_pos0": "apply", "_pos1": "APP-P-0002", "by": "human:sarav"}, proposalV7Cmd)
	assertExists(t, filepath.Join(vault, "work", "decisions", "APP-D-0001.md"))
	decisionData, decisionBody, err := parseFrontmatterMustRead(filepath.Join(vault, "work", "decisions", "APP-D-0001.md"))
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "Proposed architecture decision", stringField(decisionData, "title"), "created decision title")
	assertContainsIndexTest(t, decisionBody, "Use repo-local proposal application.")

	code, err := validateCmd(Args{"vault": vault, "json": "true"})
	if err != nil {
		t.Fatal(err)
	}
	if code != 0 {
		t.Fatalf("expected create_task/create_decision proposal application to validate, got code %d", code)
	}
}

func TestV7ValidationRejectsInvalidEventJSON(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	if err := bootstrap(Args{"vault": vault, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	eventPath := filepath.Join(vault, "events", "2026", "05", "APP-T-0001--20260513T050001Z--01J0H3EKW5Z7N4JG6CN4M9EV64.json")
	if err := writeJSON(eventPath, map[string]any{
		"schema":      "tusker.event/v1",
		"id":          "01J0H3EKW5Z7N4JG6CN4M9EV64",
		"project":     "tusker",
		"object":      "APP-T-0001",
		"object_kind": "task",
		"event_kind":  "made_up_event",
		"actor":       "agent:codex",
		"at":          "2026-05-13T05:00:01Z",
	}); err != nil {
		t.Fatal(err)
	}

	eventErrs, _, eventCount := validateV7Events(vault)
	assertEqual(t, 2, eventCount, "event count")
	if len(eventErrs) == 0 {
		t.Fatal("expected invalid event kind to fail event validation")
	}
	assertContainsIndexTest(t, eventErrs[0].Message, "invalid V7 event kind")
	code, err := validateCmd(Args{"vault": vault, "json": "true"})
	if err != nil {
		t.Fatal(err)
	}
	if code == 0 {
		t.Fatal("expected validate to reject invalid V7 event")
	}
}

func TestV7ValidationAcceptsExplicitRedactionEvents(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	if err := bootstrap(Args{"vault": vault, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	events := []struct {
		path string
		kind string
		id   string
		at   string
	}{
		{
			path: "APP-T-0001--20260513T050001Z--01J0H3EKW5Z7N4JG6CN4M9EV64.json",
			kind: "redaction",
			id:   "01J0H3EKW5Z7N4JG6CN4M9EV64",
			at:   "2026-05-13T05:00:01Z",
		},
		{
			path: "APP-T-0001--20260513T050002Z--01J0H3EKW5Z7N4JG6CN4M9EV65.json",
			kind: "redacted_replacement",
			id:   "01J0H3EKW5Z7N4JG6CN4M9EV65",
			at:   "2026-05-13T05:00:02Z",
		},
	}
	for _, event := range events {
		if err := writeJSON(filepath.Join(vault, "events", "2026", "05", event.path), map[string]any{
			"schema":      "tusker.event/v1",
			"id":          event.id,
			"project":     "tusker",
			"object":      "APP-T-0001",
			"object_kind": "task",
			"event_kind":  event.kind,
			"actor":       "human:sarav",
			"at":          event.at,
			"payload": map[string]any{
				"reason": "explicit redaction flow",
			},
		}); err != nil {
			t.Fatal(err)
		}
	}

	eventErrs, _, eventCount := validateV7Events(vault)
	assertEqual(t, 3, eventCount, "event count")
	if len(eventErrs) != 0 {
		t.Fatalf("expected redaction events to validate, got %#v", eventErrs)
	}
	code, err := validateCmd(Args{"vault": vault, "json": "true"})
	if err != nil {
		t.Fatal(err)
	}
	if code != 0 {
		t.Fatalf("expected validate to accept redaction events, got code %d", code)
	}
}

func TestV7RedactCommandEmitsRedactionEvents(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	if err := bootstrap(Args{"vault": vault, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	if err := redactV7Cmd(Args{"vault": vault, "quiet": "true", "_pos0": "APP-T-0001", "reason": "Removed leaked token from evidence.", "replacement": "Redacted summary retained.", "by": "human:sarav"}); err != nil {
		t.Fatal(err)
	}
	eventErrs, _, eventCount := validateV7Events(vault)
	assertEqual(t, 3, eventCount, "redaction event count")
	if len(eventErrs) != 0 {
		t.Fatalf("expected redaction command events to validate, got %#v", eventErrs)
	}
	var kinds []string
	if err := filepath.WalkDir(filepath.Join(vault, "events"), func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".json") {
			return err
		}
		raw, err := readText(path)
		if err != nil {
			return err
		}
		var data map[string]any
		if err := json.Unmarshal([]byte(raw), &data); err != nil {
			return err
		}
		kinds = append(kinds, stringField(data, "event_kind"))
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var redactionKinds []string
	for _, kind := range kinds {
		if kind == "redaction" || kind == "redacted_replacement" {
			redactionKinds = append(redactionKinds, kind)
		}
	}
	sort.Strings(redactionKinds)
	assertEqual(t, "redacted_replacement,redaction", strings.Join(redactionKinds, ","), "redaction kinds")
}

func TestV7HeartbeatAndReleasePreserveLeaseClaimAndEmitEvent(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	must := func(args Args, fn func(Args) error) {
		t.Helper()
		if err := fn(args); err != nil {
			t.Fatal(err)
		}
	}

	must(Args{"vault": vault, "quiet": "true"}, bootstrap)
	must(Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App V7", "summary": "V7 tracker smoke.", "v7": "true"}, newV7Epic)
	must(Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Lease lifecycle", "risk": "low", "priority": "p2", "v7": "true"}, newV7Task)
	must(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "owner": "agent:codex", "workspace": "../worktrees/APP-T-0001", "branch": "agent/APP-T-0001"}, claimCmd)

	leasePath := filepath.Join(filepath.Dir(vault), ".tusker-local", "leases", "APP-T-0001.json")
	raw, err := readText(leasePath)
	if err != nil {
		t.Fatal(err)
	}
	var lease v7LeaseRecord
	if err := json.Unmarshal([]byte(raw), &lease); err != nil {
		t.Fatal(err)
	}
	lease.ClaimedAt = "2026-05-13T05:00:00Z"
	if err := writeJSON(leasePath, lease); err != nil {
		t.Fatal(err)
	}

	must(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001"}, heartbeatV7Cmd)
	raw, err = readText(leasePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(raw), &lease); err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "active", lease.Status, "heartbeat status")
	assertEqual(t, "2026-05-13T05:00:00Z", lease.ClaimedAt, "heartbeat preserves claimed_at")
	assertEqual(t, "agent:codex", lease.Owner, "heartbeat preserves owner")
	assertEqual(t, "../worktrees/APP-T-0001", lease.Workspace, "heartbeat preserves workspace")

	must(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001"}, releaseV7Cmd)
	raw, err = readText(leasePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(raw), &lease); err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "released", lease.Status, "release status")
	assertEqual(t, "2026-05-13T05:00:00Z", lease.ClaimedAt, "release preserves claimed_at")

	eventErrs, _, eventCount := validateV7Events(vault)
	if len(eventErrs) != 0 {
		t.Fatalf("expected emitted lease events to validate, got %#v", eventErrs)
	}
	if eventCount == 0 {
		t.Fatal("expected release to emit an event")
	}
	eventFiles, err := filepath.Glob(filepath.Join(vault, "events", "*", "*", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	foundRelease := false
	for _, eventFile := range eventFiles {
		text, err := readText(eventFile)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(text, `"event_kind": "claim_released"`) {
			foundRelease = true
			break
		}
	}
	if !foundRelease {
		t.Fatal("expected claim_released event")
	}
}

func TestV7HeartbeatAndReleaseRequireActiveLease(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	must := func(args Args, fn func(Args) error) {
		t.Helper()
		if err := fn(args); err != nil {
			t.Fatal(err)
		}
	}

	must(Args{"vault": vault, "quiet": "true"}, bootstrap)
	must(Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App V7", "summary": "V7 tracker smoke.", "v7": "true"}, newV7Epic)
	must(Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Lease missing", "risk": "low", "priority": "p2", "v7": "true"}, newV7Task)

	err := heartbeatV7Cmd(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001"})
	if err == nil {
		t.Fatal("expected heartbeat without a lease to fail")
	}
	if !strings.Contains(err.Error(), "V7 lease not found") {
		t.Fatalf("expected missing lease error, got %v", err)
	}
	leasePath := filepath.Join(filepath.Dir(vault), ".tusker-local", "leases", "APP-T-0001.json")
	if fileExists(leasePath) {
		t.Fatal("heartbeat must not create a lease")
	}

	err = releaseV7Cmd(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001"})
	if err == nil {
		t.Fatal("expected release without a lease to fail")
	}
	if !strings.Contains(err.Error(), "V7 lease not found") {
		t.Fatalf("expected missing lease error, got %v", err)
	}
	if fileExists(leasePath) {
		t.Fatal("release must not create a lease")
	}

	must(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "owner": "agent:codex"}, claimCmd)
	must(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001"}, releaseV7Cmd)
	err = heartbeatV7Cmd(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001"})
	if err == nil {
		t.Fatal("expected heartbeat on released lease to fail")
	}
	if !strings.Contains(err.Error(), "not active") {
		t.Fatalf("expected inactive lease error, got %v", err)
	}
	err = releaseV7Cmd(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001"})
	if err == nil {
		t.Fatal("expected second release to fail")
	}
	if !strings.Contains(err.Error(), "not active") {
		t.Fatalf("expected inactive lease error, got %v", err)
	}
}

func TestV7LeaseRejectsDuplicateActiveClaimByDifferentOwner(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	must := func(args Args, fn func(Args) error) {
		t.Helper()
		if err := fn(args); err != nil {
			t.Fatal(err)
		}
	}

	must(Args{"vault": vault, "quiet": "true"}, bootstrap)
	must(Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App V7", "summary": "V7 tracker smoke.", "v7": "true"}, newV7Epic)
	must(Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Lease duplicate", "risk": "low", "priority": "p2", "v7": "true"}, newV7Task)
	must(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "owner": "agent:codex"}, claimCmd)

	err := claimCmd(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "owner": "agent:claude"})
	if err == nil {
		t.Fatal("expected duplicate active claim to fail")
	}
	if !strings.Contains(err.Error(), "already has an active lease") {
		t.Fatalf("expected active lease conflict, got %v", err)
	}

	must(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001"}, heartbeatV7Cmd)
	must(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001"}, releaseV7Cmd)
	must(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "owner": "agent:claude"}, claimCmd)
	leasePath := filepath.Join(filepath.Dir(vault), ".tusker-local", "leases", "APP-T-0001.json")
	raw, err := readText(leasePath)
	if err != nil {
		t.Fatal(err)
	}
	var lease v7LeaseRecord
	if err := json.Unmarshal([]byte(raw), &lease); err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "agent:claude", lease.Owner, "claim after release owner")
	assertEqual(t, "active", lease.Status, "claim after release status")
}

func TestV7FileRuntimeStoreClaimHeartbeatReleaseList(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	must := func(args Args, fn func(Args) error) {
		t.Helper()
		if err := fn(args); err != nil {
			t.Fatal(err)
		}
	}

	must(Args{"vault": vault, "quiet": "true"}, bootstrap)
	must(Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App V7", "summary": "V7 tracker smoke.", "v7": "true"}, newV7Epic)
	must(Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Runtime store lease", "risk": "low", "priority": "p2", "v7": "true"}, newV7Task)

	ctx := context.Background()
	var runtime v7RuntimeStore = v7FileRuntimeStore{VaultPath: vault}
	lease, err := runtime.Claim(ctx, v7ObjectID("APP-T-0001"), "agent:codex", 15*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "APP-T-0001", lease.ID, "claim lease id")
	assertEqual(t, "active", lease.Status, "claim status")
	assertEqual(t, "agent:codex", lease.Owner, "claim owner")

	leasePath := filepath.Join(filepath.Dir(vault), ".tusker-local", "leases", "APP-T-0001.json")
	lease.ClaimedAt = "2026-05-13T05:00:00Z"
	lease.Workspace = "../worktrees/APP-T-0001"
	if err := writeJSON(leasePath, lease); err != nil {
		t.Fatal(err)
	}

	if err := runtime.Heartbeat(ctx, "APP-T-0001"); err != nil {
		t.Fatal(err)
	}
	active, err := runtime.ListLeases(ctx, v7LeaseQuery{Owner: "agent:codex", Status: "active"})
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, 1, len(active), "active lease count")
	assertEqual(t, "2026-05-13T05:00:00Z", active[0].ClaimedAt, "heartbeat preserves claimed_at")
	assertEqual(t, "../worktrees/APP-T-0001", active[0].Workspace, "heartbeat preserves workspace")

	if err := runtime.Release(ctx, "APP-T-0001"); err != nil {
		t.Fatal(err)
	}
	released, err := runtime.ListLeases(ctx, v7LeaseQuery{Task: "APP-T-0001", Status: "released"})
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, 1, len(released), "released lease count")
	assertEqual(t, "2026-05-13T05:00:00Z", released[0].ClaimedAt, "release preserves claimed_at")

	eventErrs, _, eventCount := validateV7Events(vault)
	if len(eventErrs) != 0 {
		t.Fatalf("expected runtime store release event to validate, got %#v", eventErrs)
	}
	if eventCount == 0 {
		t.Fatal("expected runtime store release to emit an event")
	}
}

func TestV7ClosePolicyRequiresRiskEvidenceAndAcceptor(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	must := func(args Args, fn func(Args) error) {
		t.Helper()
		if err := fn(args); err != nil {
			t.Fatal(err)
		}
	}

	must(Args{"vault": vault, "quiet": "true"}, bootstrap)
	must(Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App V7", "summary": "V7 tracker smoke.", "v7": "true"}, newV7Epic)
	must(Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Medium close policy", "risk": "medium", "priority": "p2", "v7": "true"}, newV7Task)
	must(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "kind": "automated_test", "covers": "A1", "summary": "Focused tests passed."}, evidenceV7AddCmd)
	must(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "status": "review", "by": "agent:codex"}, statusV7Cmd)
	must(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "by": "reviewer:agent"}, closeV7Cmd)

	err := evidenceV7AddCmd(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "kind": "evidence_packet", "covers": "A1", "summary": "Reviewer packet accepted."})
	if err == nil {
		t.Fatal("expected evidence_packet to be rejected as an evidence kind")
	}
	if !strings.Contains(err.Error(), "invalid evidence kind") {
		t.Fatalf("expected invalid evidence kind error, got %v", err)
	}

	must(Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "High close policy", "risk": "high", "priority": "p1", "proof-mode": "card", "v7": "true"}, newV7Task)
	must(Args{"vault": vault, "quiet": "true", "id": "APP-T-0002", "kind": "automated_test", "covers": "A1", "summary": "Focused tests passed."}, evidenceV7AddCmd)
	must(Args{"vault": vault, "quiet": "true", "id": "APP-T-0002", "kind": "human_review", "status": "accepted", "accepted-by": "human:sarav", "covers": "A1", "summary": "Human reviewed and accepted."}, evidenceV7AddCmd)
	must(Args{"vault": vault, "quiet": "true", "id": "APP-T-0002", "status": "review", "by": "agent:codex"}, statusV7Cmd)
	err = closeV7Cmd(Args{"vault": vault, "quiet": "true", "id": "APP-T-0002", "by": "reviewer:agent"})
	if err == nil {
		t.Fatal("expected high close to require human acceptor")
	}
	if !strings.Contains(err.Error(), "requires human acceptor") {
		t.Fatalf("expected human acceptor error, got %v", err)
	}
	must(Args{"vault": vault, "quiet": "true", "id": "APP-T-0002", "by": "human:sarav"}, closeV7Cmd)
}

func TestV7CloseRequiresAcceptanceCoverageOrWaiver(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	must := func(args Args, fn func(Args) error) {
		t.Helper()
		if err := fn(args); err != nil {
			t.Fatal(err)
		}
	}

	must(Args{"vault": vault, "quiet": "true"}, bootstrap)
	must(Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App V7", "summary": "V7 tracker smoke.", "v7": "true"}, newV7Epic)
	must(Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Acceptance coverage", "risk": "low", "priority": "p2", "v7": "true"}, newV7Task)
	must(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "kind": "automated_test", "covers": "A2", "summary": "Wrong acceptance covered."}, evidenceV7AddCmd)
	must(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "status": "review", "by": "agent:codex"}, statusV7Cmd)

	err := closeV7Cmd(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "by": "reviewer:agent"})
	if err == nil {
		t.Fatal("expected close to require A1 coverage")
	}
	if !strings.Contains(err.Error(), "A1") {
		t.Fatalf("expected missing A1 close error, got %v", err)
	}

	taskPath := filepath.Join(vault, "work", "tasks", "APP-T-0001.md")
	data, body, err := parseFrontmatterMustRead(taskPath)
	if err != nil {
		t.Fatal(err)
	}
	data["acceptance_waivers"] = []map[string]any{{
		"covers": []string{"TASK:A1"},
		"by":     "reviewer:agent",
		"at":     "2026-05-13T05:00:00Z",
		"reason": "Fixture waiver for acceptance-close policy.",
	}}
	data["state_rev"] = v7StateRev(data, body)
	content, err := serializeDocument(data, body, v7FrontmatterOrder["task"])
	if err != nil {
		t.Fatal(err)
	}
	if err := writeText(taskPath, content); err != nil {
		t.Fatal(err)
	}
	must(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "by": "reviewer:agent"}, closeV7Cmd)
}

func TestV7ValidationRejectsDoneTaskViolatingClosePolicy(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	must := func(args Args, fn func(Args) error) {
		t.Helper()
		if err := fn(args); err != nil {
			t.Fatal(err)
		}
	}

	must(Args{"vault": vault, "quiet": "true"}, bootstrap)
	must(Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App V7", "summary": "V7 tracker smoke.", "v7": "true"}, newV7Epic)
	must(Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Manual bad close", "risk": "high", "priority": "p1", "v7": "true"}, newV7Task)
	must(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "kind": "automated_test", "covers": "A1", "summary": "Focused tests passed."}, evidenceV7AddCmd)
	must(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "kind": "human_review", "status": "accepted", "accepted-by": "human:sarav", "covers": "A1", "summary": "Human review evidence exists."}, evidenceV7AddCmd)

	taskPath := filepath.Join(vault, "work", "tasks", "APP-T-0001.md")
	data, body, err := parseFrontmatterMustRead(taskPath)
	if err != nil {
		t.Fatal(err)
	}
	data["status"] = "done"
	data["readiness"] = "done"
	data["next_owner"] = "none"
	data["next_source"] = "status"
	data["next_ref"] = ""
	data["next_action"] = ""
	data["accepted_by"] = "reviewer:agent"
	data["accepted_at"] = "2026-05-13T05:00:00Z"
	data["closed_at"] = "2026-05-13T05:00:00Z"
	data["state_rev"] = v7StateRev(data, body)
	content, err := serializeDocument(data, body, v7FrontmatterOrder["task"])
	if err != nil {
		t.Fatal(err)
	}
	if err := writeText(taskPath, content); err != nil {
		t.Fatal(err)
	}

	code, err := validateCmd(Args{"vault": vault, "json": "true"})
	if err != nil {
		t.Fatal(err)
	}
	if code == 0 {
		t.Fatal("expected validate to reject done task closed by invalid acceptor")
	}
	issues, _ := validateV7Note(Note{Data: data, Body: body, AbsolutePath: taskPath}, validationContext{RelativePath: filepath.ToSlash(filepath.Join("work", "tasks", "APP-T-0001.md")), VaultPath: vault}, "work/tasks/APP-T-0001.md")
	found := false
	for _, issue := range issues {
		if issue.Code == "DONE_TASK_ACCEPTOR_INVALID" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected DONE_TASK_ACCEPTOR_INVALID, got %#v", issues)
	}
}

func TestV7ClosePolicyReadsTuskerYAMLOverride(t *testing.T) {
	root := t.TempDir()
	vault := filepath.Join(root, "vault")
	if err := writeText(filepath.Join(root, "tusker.yaml"), `close_policy:
  low:
    required_acceptor: reviewer_agent
    required_evidence:
      - manual_smoke
branches:
  mutation_mode: single_user_local
`); err != nil {
		t.Fatal(err)
	}
	must := func(args Args, fn func(Args) error) {
		t.Helper()
		if err := fn(args); err != nil {
			t.Fatal(err)
		}
	}

	must(Args{"vault": vault, "quiet": "true"}, bootstrap)
	must(Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App V7", "summary": "V7 tracker smoke.", "v7": "true"}, newV7Epic)
	must(Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Config close policy", "risk": "low", "priority": "p2", "proof-required": "manual_smoke", "v7": "true"}, newV7Task)
	must(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "kind": "manual_smoke", "status": "accepted", "accepted-by": "human:sarav", "covers": "A1", "summary": "Configured smoke evidence accepted."}, evidenceV7AddCmd)
	must(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "status": "review", "by": "agent:codex"}, statusV7Cmd)
	must(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "by": "reviewer:agent"}, closeV7Cmd)

	code, err := validateCmd(Args{"vault": vault, "json": "true"})
	if err != nil {
		t.Fatal(err)
	}
	if code != 0 {
		t.Fatalf("expected configured close policy to validate, got code %d", code)
	}
}

func TestV7ConfigReadsNestedBranchesStateAndRuntime(t *testing.T) {
	root := t.TempDir()
	vault := filepath.Join(root, "vault")
	if err := writeText(filepath.Join(root, "tusker.yaml"), `schema: tusker.config/v1
branches:
  default_branch: release
  state_branch: team/state
runtime:
  lease_ttl_minutes: 7
`); err != nil {
		t.Fatal(err)
	}
	if err := bootstrap(Args{"vault": vault, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}

	assertEqual(t, []string{"release"}, configuredV7ControlBranches(vault), "default control branch")
	if !isV7ControlBranch(vault, "release") {
		t.Fatal("expected configured default_branch to be a control branch")
	}
	if isV7ControlBranch(vault, "main") {
		t.Fatal("did not expect main to be a control branch when default_branch is release")
	}
	assertEqual(t, "team/state", v7StateBranch(vault), "state branch")
	assertEqual(t, 7*time.Minute, v7LeaseTTL(vault), "lease ttl")

	if err := writeText(filepath.Join(root, "tusker.yaml"), `schema: tusker.config/v1
branches:
  default_branch: release
  control:
    - main
    - trunk
  state_branch: team/state
runtime:
  lease_ttl_minutes: 7
`); err != nil {
		t.Fatal(err)
	}
	assertEqual(t, []string{"main", "trunk"}, configuredV7ControlBranches(vault), "explicit control branches")
}

func TestV7ReconcileMarksExpiredLeaseStale(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	must := func(args Args, fn func(Args) error) {
		t.Helper()
		if err := fn(args); err != nil {
			t.Fatal(err)
		}
	}

	must(Args{"vault": vault, "quiet": "true"}, bootstrap)
	must(Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App V7", "summary": "V7 tracker smoke.", "v7": "true"}, newV7Epic)
	must(Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Lease expiry", "risk": "low", "priority": "p2", "v7": "true"}, newV7Task)
	expired := time.Now().UTC().Add(-10 * time.Minute).Format(time.RFC3339)
	leasePath := filepath.Join(filepath.Dir(vault), ".tusker-local", "leases", "APP-T-0001.json")
	if err := writeJSON(leasePath, v7LeaseRecord{
		Schema:      "tusker.lease/v1",
		ID:          "APP-T-0001",
		Project:     "tusker",
		Task:        "APP-T-0001",
		Owner:       "agent:codex",
		Branch:      "agent/APP-T-0001",
		Status:      "active",
		ClaimedAt:   expired,
		ExpiresAt:   expired,
		HeartbeatAt: expired,
	}); err != nil {
		t.Fatal(err)
	}

	must(Args{"vault": vault, "quiet": "true"}, reconcileV7Cmd)

	raw, err := readText(leasePath)
	if err != nil {
		t.Fatal(err)
	}
	var lease v7LeaseRecord
	if err := json.Unmarshal([]byte(raw), &lease); err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "stale", lease.Status, "lease status")
	dashboard := mustReadIndexTest(t, filepath.Join(vault, "dashboards", "active-runs.md"))
	assertContainsIndexTest(t, dashboard, "`stale`")
}

func TestV7StateBranchSyncAndImportLeases(t *testing.T) {
	previousWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(previousWD); err != nil {
			t.Fatalf("restore working directory: %v", err)
		}
	})
	repo := filepath.Join(t.TempDir(), "repo")
	vault := filepath.Join(repo, "tusker")
	if err := ensureDir(repo); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}
	runGit(t, "init", "-b", "main")
	runGit(t, "config", "user.email", "test@example.com")
	runGit(t, "config", "user.name", "Tusker Test")
	if err := bootstrap(Args{"vault": vault, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	if err := newV7Epic(Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App V7", "summary": "V7 tracker smoke.", "v7": "true"}); err != nil {
		t.Fatal(err)
	}
	if err := newV7Task(Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "State branch lease", "risk": "low", "priority": "p2", "v7": "true"}); err != nil {
		t.Fatal(err)
	}
	if err := claimCmd(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "owner": "agent:codex"}); err != nil {
		t.Fatal(err)
	}

	var backend v7StateBackend = v7GitStateBackend{VaultPath: vault}
	if commit, err := backend.Sync(context.Background(), v7StateSyncOptions{Branch: "tusker/state"}); err != nil {
		t.Fatal(err)
	} else if commit == "" {
		t.Fatal("expected state backend sync to return a commit")
	}
	branchLease := gitOutput(t, "show", "tusker/state:leases/APP-T-0001.json")
	assertContainsIndexTest(t, branchLease, `"task": "APP-T-0001"`)
	assertContainsIndexTest(t, branchLease, `"status": "active"`)
	index := gitOutput(t, "show", "tusker/state:scheduler/index.json")
	assertContainsIndexTest(t, index, `"lease_count": 1`)
	exportDir := filepath.Join(repo, ".tusker-runtime", "state-export")
	exported, err := backend.Export(context.Background(), v7StateSyncOptions{Dir: exportDir})
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, 2, exported, "exported state file count")
	assertExists(t, filepath.Join(exportDir, "leases", "APP-T-0001.json"))
	assertExists(t, filepath.Join(exportDir, "scheduler", "index.json"))

	localLeasePath := filepath.Join(repo, ".tusker-local", "leases", "APP-T-0001.json")
	if err := os.Remove(localLeasePath); err != nil {
		t.Fatal(err)
	}
	importedCount, err := backend.Import(context.Background(), v7StateSyncOptions{Branch: "tusker/state"})
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, 1, importedCount, "imported lease count")
	assertExists(t, localLeasePath)
	imported := mustReadIndexTest(t, localLeasePath)
	assertContainsIndexTest(t, imported, `"task": "APP-T-0001"`)
}

func TestV7StateBranchPushAndFetchRemoteLeases(t *testing.T) {
	previousWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(previousWD); err != nil {
			t.Fatalf("restore working directory: %v", err)
		}
	})
	root := t.TempDir()
	remote := filepath.Join(root, "remote.git")
	repo := filepath.Join(root, "repo")
	vault := filepath.Join(repo, "tusker")
	runGitDir(t, root, "init", "--bare", remote)
	if err := ensureDir(repo); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}
	runGit(t, "init", "-b", "main")
	runGit(t, "config", "user.email", "test@example.com")
	runGit(t, "config", "user.name", "Tusker Test")
	runGit(t, "remote", "add", "origin", remote)
	if err := bootstrap(Args{"vault": vault, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	if err := newV7Epic(Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App V7", "summary": "V7 tracker smoke.", "v7": "true"}); err != nil {
		t.Fatal(err)
	}
	if err := newV7Task(Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Remote state branch lease", "risk": "low", "priority": "p2", "v7": "true"}); err != nil {
		t.Fatal(err)
	}
	if err := claimCmd(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "owner": "agent:codex"}); err != nil {
		t.Fatal(err)
	}

	if err := stateV7Cmd(Args{"vault": vault, "quiet": "true", "_pos0": "sync", "push": "true"}); err != nil {
		t.Fatal(err)
	}
	remoteLease := gitDirOutput(t, remote, "show", "tusker/state:leases/APP-T-0001.json")
	assertContainsIndexTest(t, remoteLease, `"owner": "agent:codex"`)

	localLeasePath := filepath.Join(repo, ".tusker-local", "leases", "APP-T-0001.json")
	if err := os.Remove(localLeasePath); err != nil {
		t.Fatal(err)
	}
	if err := stateV7Cmd(Args{"vault": vault, "quiet": "true", "_pos0": "import", "fetch": "true"}); err != nil {
		t.Fatal(err)
	}
	imported := mustReadIndexTest(t, localLeasePath)
	assertContainsIndexTest(t, imported, `"owner": "agent:codex"`)
}

func TestV7HookInstallPreCommitBranchPolicy(t *testing.T) {
	previousWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(previousWD); err != nil {
			t.Fatalf("restore working directory: %v", err)
		}
	})
	repo := filepath.Join(t.TempDir(), "repo")
	vault := filepath.Join(repo, "tusker")
	if err := ensureDir(repo); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}
	runGit(t, "init", "-b", "main")
	if err := bootstrap(Args{"vault": vault, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}

	command, args := parseCLI([]string{"tusker", "hook", "install", "pre-commit", "--vault", vault, "--quiet"})
	assertEqual(t, "hook install", command, "hook command parse")
	if code, err := run(command, args); err != nil || code != 0 {
		t.Fatalf("hook install failed: code=%d err=%v", code, err)
	}
	hookPath := filepath.Join(repo, ".git", "hooks", "pre-commit")
	info, err := os.Stat(hookPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&0o111 == 0 {
		t.Fatalf("expected pre-commit hook to be executable, mode=%s", info.Mode())
	}
	hook := mustReadIndexTest(t, hookPath)
	assertContainsIndexTest(t, hook, "tusker:pre-commit-branch-policy")
	assertContainsIndexTest(t, hook, "validate --staged --branch-policy")

	if code, err := run(command, args); err != nil || code != 0 {
		t.Fatalf("managed hook reinstall failed: code=%d err=%v", code, err)
	}
}

func TestV7StagedBranchPolicyRejectsProtectedStateMutation(t *testing.T) {
	previousWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(previousWD); err != nil {
			t.Fatalf("restore working directory: %v", err)
		}
	})
	repo := filepath.Join(t.TempDir(), "repo")
	vault := filepath.Join(repo, "tusker")
	if err := ensureDir(repo); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}
	runGit(t, "init", "-b", "main")
	runGit(t, "config", "user.email", "test@example.com")
	runGit(t, "config", "user.name", "Tusker Test")
	if err := writeText(filepath.Join(repo, "tusker.yaml"), "branches:\n  control:\n    - main\n"); err != nil {
		t.Fatal(err)
	}
	if err := bootstrap(Args{"vault": vault, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	if err := newV7Epic(Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App V7", "summary": "V7 tracker smoke.", "v7": "true"}); err != nil {
		t.Fatal(err)
	}
	if err := newV7Task(Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Protected mutation", "risk": "low", "priority": "p2", "v7": "true"}); err != nil {
		t.Fatal(err)
	}
	runGit(t, "add", ".")
	runGit(t, "commit", "-m", "seed")
	runGit(t, "checkout", "-b", "agent/APP-T-0001")

	taskPath := filepath.Join(vault, "work", "tasks", "APP-T-0001.md")
	data, body, err := parseFrontmatterMustRead(taskPath)
	if err != nil {
		t.Fatal(err)
	}
	data["status"] = "done"
	content, err := serializeDocument(data, body, v7FrontmatterOrder["task"])
	if err != nil {
		t.Fatal(err)
	}
	if err := writeText(taskPath, content); err != nil {
		t.Fatal(err)
	}
	taskRelPath := filepath.ToSlash(filepath.Join(vaultDisplayRoot(vault), "work", "tasks", "APP-T-0001.md"))
	gateRelDir := filepath.ToSlash(filepath.Join(vaultDisplayRoot(vault), "work", "gates"))
	taskRelDir := filepath.ToSlash(filepath.Join(vaultDisplayRoot(vault), "work", "tasks"))
	runGit(t, "add", taskRelPath)

	changed, err := exec.Command("git", "diff", "--name-only", "--cached", "--", taskRelDir, gateRelDir).Output()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(changed), taskRelPath) {
		t.Fatalf("expected staged task path, got %q", string(changed))
	}
	errors, _ := validateV7BranchPolicy(vault, Args{"staged": "true"})
	if len(errors) == 0 {
		t.Fatal("expected staged branch-policy validation to reject protected status change")
	}
	assertEqual(t, "PROTECTED_FIELD_CHANGED", errors[0].Code, "branch policy issue code")
	code, err := validateCmd(Args{"vault": vault, "staged": "true", "branch-policy-only": "true", "json": "true"})
	if err != nil {
		t.Fatal(err)
	}
	if code == 0 {
		t.Fatal("expected branch-policy-only validate to reject staged protected mutation")
	}
}

func TestV7BranchPolicyRejectsTaskAndGateDeletion(t *testing.T) {
	previousWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(previousWD); err != nil {
			t.Fatalf("restore working directory: %v", err)
		}
	})
	repo := filepath.Join(t.TempDir(), "repo")
	vault := filepath.Join(repo, "tusker")
	if err := ensureDir(repo); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}
	runGit(t, "init", "-b", "main")
	runGit(t, "config", "user.email", "test@example.com")
	runGit(t, "config", "user.name", "Tusker Test")
	if err := writeText(filepath.Join(repo, "tusker.yaml"), "branches:\n  control:\n    - main\n"); err != nil {
		t.Fatal(err)
	}
	if err := bootstrap(Args{"vault": vault, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	if err := newV7Epic(Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App V7", "summary": "V7 tracker smoke.", "v7": "true"}); err != nil {
		t.Fatal(err)
	}
	if err := newV7Task(Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Deleted task", "risk": "low", "priority": "p2", "v7": "true"}); err != nil {
		t.Fatal(err)
	}
	if err := newV7Gate(Args{"vault": vault, "quiet": "true", "blocks": "APP-T-0001", "kind": "verification", "owner": "reviewer", "action": "Review deletion.", "verification": "Gate exists."}); err != nil {
		t.Fatal(err)
	}
	runGit(t, "add", ".")
	runGit(t, "commit", "-m", "seed")
	runGit(t, "checkout", "-b", "agent/APP-T-0001")

	taskPath := filepath.Join(vault, "work", "tasks", "APP-T-0001.md")
	if err := os.Remove(taskPath); err != nil {
		t.Fatal(err)
	}
	gatePath := filepath.Join(vault, "work", "gates", "APP-G-0001.md")
	if err := os.Remove(gatePath); err != nil {
		t.Fatal(err)
	}
	taskRelPath := filepath.ToSlash(filepath.Join(vaultDisplayRoot(vault), "work", "tasks", "APP-T-0001.md"))
	gateRelPath := filepath.ToSlash(filepath.Join(vaultDisplayRoot(vault), "work", "gates", "APP-G-0001.md"))
	runGit(t, "add", "-u", taskRelPath, gateRelPath)
	errors, _ := validateV7BranchPolicy(vault, Args{"staged": "true"})
	if len(errors) == 0 {
		t.Fatal("expected staged branch-policy validation to reject task/gate deletion")
	}
	assertEqual(t, "PROTECTED_FIELD_CHANGED", errors[0].Code, "staged deletion issue code")

	runGit(t, "commit", "-m", "delete task")
	errors, _ = validateV7BranchPolicy(vault, Args{})
	if len(errors) == 0 {
		t.Fatal("expected branch-policy validation to reject committed task/gate deletion")
	}
	assertEqual(t, "PROTECTED_FIELD_CHANGED", errors[0].Code, "committed deletion issue code")
}

func TestV7BranchPolicyHonorsConfiguredProtectStateFieldsFalse(t *testing.T) {
	previousWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(previousWD); err != nil {
			t.Fatalf("restore working directory: %v", err)
		}
	})
	repo := filepath.Join(t.TempDir(), "repo")
	vault := filepath.Join(repo, "tusker")
	if err := ensureDir(repo); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}
	runGit(t, "init", "-b", "main")
	runGit(t, "config", "user.email", "test@example.com")
	runGit(t, "config", "user.name", "Tusker Test")
	if err := writeText(filepath.Join(repo, "tusker.yaml"), strings.Join([]string{
		"branches:",
		"  control:",
		"    - main",
		"validation:",
		"  protect_state_fields: false",
	}, "\n")); err != nil {
		t.Fatal(err)
	}
	if err := bootstrap(Args{"vault": vault, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	if err := newV7Epic(Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App V7", "summary": "V7 tracker smoke.", "v7": "true"}); err != nil {
		t.Fatal(err)
	}
	if err := newV7Task(Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Configured branch policy", "risk": "low", "priority": "p2", "v7": "true"}); err != nil {
		t.Fatal(err)
	}
	runGit(t, "add", ".")
	runGit(t, "commit", "-m", "seed")
	runGit(t, "checkout", "-b", "agent/APP-T-0001")

	taskPath := filepath.Join(vault, "work", "tasks", "APP-T-0001.md")
	data, body, err := parseFrontmatterMustRead(taskPath)
	if err != nil {
		t.Fatal(err)
	}
	data["status"] = "done"
	content, err := serializeDocument(data, body, v7FrontmatterOrder["task"])
	if err != nil {
		t.Fatal(err)
	}
	if err := writeText(taskPath, content); err != nil {
		t.Fatal(err)
	}
	taskRelPath := filepath.ToSlash(filepath.Join(vaultDisplayRoot(vault), "work", "tasks", "APP-T-0001.md"))
	runGit(t, "add", taskRelPath)

	errors, _ := validateV7BranchPolicy(vault, Args{"staged": "true"})
	if len(errors) != 0 {
		t.Fatalf("expected protect_state_fields=false to disable branch-policy errors, got %#v", errors)
	}
}

func TestV7ControlMutationAllowsExplicitSingleUserLocalMode(t *testing.T) {
	previousWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(previousWD); err != nil {
			t.Fatalf("restore working directory: %v", err)
		}
	})
	repo := filepath.Join(t.TempDir(), "repo")
	vault := filepath.Join(repo, "tusker")
	if err := ensureDir(repo); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}
	runGit(t, "init", "-b", "main")
	runGit(t, "config", "user.email", "test@example.com")
	runGit(t, "config", "user.name", "Tusker Test")
	if err := writeText(filepath.Join(repo, "tusker.yaml"), "branches:\n  control:\n    - main\n"); err != nil {
		t.Fatal(err)
	}
	if err := bootstrap(Args{"vault": vault, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	if err := newV7Epic(Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App V7", "summary": "V7 tracker smoke.", "v7": "true"}); err != nil {
		t.Fatal(err)
	}
	if err := newV7Task(Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Local mutation", "risk": "low", "priority": "p2", "v7": "true"}); err != nil {
		t.Fatal(err)
	}
	runGit(t, "add", ".")
	runGit(t, "commit", "-m", "seed")
	runGit(t, "checkout", "-b", "agent/APP-T-0001")

	err = statusV7Cmd(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "status": "review"})
	if err == nil {
		t.Fatal("expected feature-branch status mutation to fail without explicit local mutation mode")
	}
	if !strings.Contains(err.Error(), "protected Tusker state cannot be mutated") {
		t.Fatalf("expected protected mutation error, got %v", err)
	}

	if err := writeText(filepath.Join(repo, "tusker.yaml"), strings.Join([]string{
		"branches:",
		"  control:",
		"    - main",
		"runtime:",
		"  mutation_mode: single_user_local",
	}, "\n")); err != nil {
		t.Fatal(err)
	}
	if err := statusV7Cmd(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "status": "review"}); err != nil {
		t.Fatalf("expected explicit single_user_local mutation mode to allow status change, got %v", err)
	}
}

func TestV7ControlMutationUsesVaultRepoRootBranch(t *testing.T) {
	previousWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(previousWD); err != nil {
			t.Fatalf("restore working directory: %v", err)
		}
	})
	repo := filepath.Join(t.TempDir(), "repo")
	vault := filepath.Join(repo, "tusker")
	other := filepath.Join(t.TempDir(), "other")
	if err := ensureDir(repo); err != nil {
		t.Fatal(err)
	}
	if err := ensureDir(other); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}
	runGit(t, "init", "-b", "main")
	runGit(t, "config", "user.email", "test@example.com")
	runGit(t, "config", "user.name", "Tusker Test")
	if err := writeText(filepath.Join(repo, "tusker.yaml"), "branches:\n  control:\n    - main\n"); err != nil {
		t.Fatal(err)
	}
	if err := bootstrap(Args{"vault": vault, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	if err := newV7Epic(Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App V7", "summary": "V7 tracker smoke.", "v7": "true"}); err != nil {
		t.Fatal(err)
	}
	if err := newV7Task(Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Repo-root branch", "risk": "low", "priority": "p2", "v7": "true"}); err != nil {
		t.Fatal(err)
	}
	runGit(t, "add", ".")
	runGit(t, "commit", "-m", "seed")
	if err := os.Chdir(other); err != nil {
		t.Fatal(err)
	}
	if err := statusV7Cmd(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "status": "review"}); err != nil {
		t.Fatalf("expected control mutation to use vault repo root branch, got %v", err)
	}
}

func TestV7BranchPolicyRejectsDetachedAndNonGitState(t *testing.T) {
	previousWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(previousWD); err != nil {
			t.Fatalf("restore working directory: %v", err)
		}
	})
	repo := filepath.Join(t.TempDir(), "repo")
	vault := filepath.Join(repo, "tusker")
	if err := ensureDir(repo); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}
	runGit(t, "init", "-b", "main")
	runGit(t, "config", "user.email", "test@example.com")
	runGit(t, "config", "user.name", "Tusker Test")
	if err := writeText(filepath.Join(repo, "tusker.yaml"), "branches:\n  control:\n    - main\n"); err != nil {
		t.Fatal(err)
	}
	if err := bootstrap(Args{"vault": vault, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	if err := newV7Epic(Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App V7", "summary": "V7 tracker smoke.", "v7": "true"}); err != nil {
		t.Fatal(err)
	}
	if err := newV7Task(Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Detached branch", "risk": "low", "priority": "p2", "v7": "true"}); err != nil {
		t.Fatal(err)
	}
	runGit(t, "add", ".")
	runGit(t, "commit", "-m", "seed")
	rev := strings.TrimSpace(gitOutput(t, "rev-parse", "HEAD"))
	runGit(t, "checkout", "--detach", rev)
	err = statusV7Cmd(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "status": "review"})
	if err == nil || !strings.Contains(err.Error(), "checked-out Git branch") {
		t.Fatalf("expected detached HEAD control mutation to fail, got %v", err)
	}
	errors, _ := validateV7BranchPolicy(vault, Args{"staged": "true"})
	if len(errors) == 0 || errors[0].Code != "BRANCH_POLICY_BRANCH_UNAVAILABLE" {
		t.Fatalf("expected detached branch-policy error, got %#v", errors)
	}

	nonGitVault := filepath.Join(t.TempDir(), "vault")
	if err := bootstrap(Args{"vault": nonGitVault, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	errors, _ = validateV7BranchPolicy(nonGitVault, Args{"staged": "true"})
	if len(errors) == 0 || errors[0].Code != "BRANCH_POLICY_BRANCH_UNAVAILABLE" {
		t.Fatalf("expected non-git branch-policy error, got %#v", errors)
	}
}

func runGit(t *testing.T, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, string(output))
	}
}

func gitOutput(t *testing.T, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, string(output))
	}
	return string(output)
}

func runGitDir(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmdArgs := append([]string{"-C", dir}, args...)
	cmd := exec.Command("git", cmdArgs...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git -C %s %s failed: %v\n%s", dir, strings.Join(args, " "), err, string(output))
	}
}

func gitDirOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmdArgs := append([]string{"-C", dir}, args...)
	cmd := exec.Command("git", cmdArgs...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git -C %s %s failed: %v\n%s", dir, strings.Join(args, " "), err, string(output))
	}
	return string(output)
}

func assertGlobCount(t *testing.T, pattern string, want int, label string) {
	t.Helper()
	matches, err := filepath.Glob(pattern)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, want, len(matches), label)
}

func mustIndex(t *testing.T, vault string) v7Index {
	t.Helper()
	idx, err := loadV7Index(vault)
	if err != nil {
		t.Fatal(err)
	}
	return idx
}

func mustBody(t *testing.T, path string) string {
	t.Helper()
	_, body, err := parseFrontmatterMustRead(path)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func forceV7TaskProjection(t *testing.T, vault, taskID, status, readiness, nextOwner, nextAction string) {
	t.Helper()
	path := filepath.Join(vault, "work", "tasks", taskID+".md")
	data, body, err := parseFrontmatterMustRead(path)
	if err != nil {
		t.Fatal(err)
	}
	data["status"] = status
	data["readiness"] = readiness
	data["next_owner"] = nextOwner
	data["next_action"] = nextAction
	data["state_rev"] = v7StateRev(data, body)
	content, err := serializeDocument(data, body, v7FrontmatterOrder["task"])
	if err != nil {
		t.Fatal(err)
	}
	if err := writeText(path, content); err != nil {
		t.Fatal(err)
	}
}

func issuesContainCode(issues []Issue, code string) bool {
	for _, issue := range issues {
		if issue.Code == code {
			return true
		}
	}
	return false
}
