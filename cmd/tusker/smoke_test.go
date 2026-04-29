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

func TestSmokeCLI(t *testing.T) {
	repoRoot, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	tempRoot := t.TempDir()
	binary := filepath.Join(tempRoot, "tusker")
	build := exec.Command("go", "build", "-o", binary, ".")
	build.Dir = repoRoot
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build failed: %v\n%s", err, output)
	}

	vault := filepath.Join(tempRoot, "vault")
	repo := filepath.Join(tempRoot, "repo")
	binDir := filepath.Join(tempRoot, "bin")
	homeDir := filepath.Join(tempRoot, "home")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}

	var runAt func(dir string, args ...string) string
	runAt = func(dir string, args ...string) string {
		t.Helper()
		cmd := exec.Command(binary, args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "TUSKER_STATE_ROOT="+filepath.Join(tempRoot, "state"))
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("command failed: %s\n%s", strings.Join(args, " "), output)
		}
		return string(output)
	}

	run := func(args ...string) string {
		t.Helper()
		return runAt(repoRoot, args...)
	}

	runWithHome := func(args ...string) string {
		t.Helper()
		cmd := exec.Command(binary, args...)
		cmd.Dir = repoRoot
		cmd.Env = append(os.Environ(),
			"HOME="+homeDir,
			"GOMODCACHE="+filepath.Join(os.TempDir(), "tusker-test-gomodcache"),
			"TUSKER_STATE_ROOT="+filepath.Join(tempRoot, "state"),
		)
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("command failed: %s\n%s", strings.Join(args, " "), output)
		}
		return string(output)
	}

	var runJSONAt func(dir string, args ...string) map[string]any
	runJSONAt = func(dir string, args ...string) map[string]any {
		t.Helper()
		output := runAt(dir, args...)
		lines := strings.Split(strings.TrimSpace(output), "\n")
		last := lines[len(lines)-1]
		var decoded map[string]any
		if err := json.Unmarshal([]byte(last), &decoded); err != nil {
			t.Fatalf("invalid json output for %s: %v\n%s", strings.Join(args, " "), err, output)
		}
		return decoded
	}

	runJSON := func(args ...string) map[string]any {
		t.Helper()
		return runJSONAt(repoRoot, args...)
	}

	runJSONExpectFail := func(args ...string) map[string]any {
		t.Helper()
		cmd := exec.Command(binary, args...)
		cmd.Dir = repoRoot
		cmd.Env = append(os.Environ(), "TUSKER_STATE_ROOT="+filepath.Join(tempRoot, "state"))
		output, err := cmd.CombinedOutput()
		if err == nil {
			t.Fatalf("expected command failure: %s\n%s", strings.Join(args, " "), output)
		}
		lines := strings.Split(strings.TrimSpace(string(output)), "\n")
		last := lines[len(lines)-1]
		var decoded map[string]any
		if err := json.Unmarshal([]byte(last), &decoded); err != nil {
			t.Fatalf("invalid json error output for %s: %v\n%s", strings.Join(args, " "), err, output)
		}
		return decoded
	}

	run("bootstrap", "--vault", vault)
	assertExists(t, filepath.Join(vault, "WORKFLOW.md"))
	assertExists(t, filepath.Join(vault, "_system", "workspaces"))
	rewriteWorkflowRunner(t, filepath.Join(vault, "WORKFLOW.md"), markReviewCommand())

	run("new-epic", "--vault", vault, "--acronym", "MEM", "--title", "Memory system", "--summary", "Unify durable memory tracking and review workflows.", "--owner", "sarav", "--spec-source", "docs/specs/MEMORY_RFC.md")
	run("new-story", "--vault", vault, "--epic", "MEM", "--title", "Add memory backend", "--change-type", "feature", "--size", "m", "--risk", "medium", "--priority", "p1", "--delegation", "execute", "--surfaces", "api", "--assignee", "codex", "--requester", "sarav", "--ai-assistance", "heavy", "--ai-tools", "claude-code")
	run("new-bug", "--vault", vault, "--epic", "MEM", "--title", "Crash on empty input", "--size", "s", "--risk", "medium", "--priority", "p2", "--delegation", "execute", "--requester", "sarav", "--ai-assistance", "moderate")
	run("new-doc", "--vault", vault, "--epic", "MEM", "--title", "Memory user guide", "--audience", "user")

	epicIndex := filepath.Join(vault, "epics", "MEM", "index.md")
	story := filepath.Join(vault, "epics", "MEM", "MEM-S-0001.md")
	bug := filepath.Join(vault, "epics", "MEM", "MEM-B-0001.md")
	doc := filepath.Join(vault, "epics", "MEM", "MEM-D-0001.md")

	populateFile(t, epicIndex, map[string]string{
		"## Problem": `## Problem

Durable memory across agent sessions is currently fragmented; this epic unifies the backing store.`,
		"## Success criteria": `## Success criteria

- One canonical store used by every agent
- No silent data loss across host restarts
- Public API is stable for 6 months`,
		"## Open questions": `## Open questions

- Do we snapshot or stream?
- What is the retention default?
- Who owns expiry?`,
	})

	populateFile(t, story, map[string]string{
		"## Problem": `## Problem

The current backend does not persist memory beyond a single process lifetime.`,
		"## Acceptance criteria": `## Acceptance criteria

- [ ] Memory survives a process restart
- [ ] API returns 200 on happy path
- [ ] Errors carry typed codes`,
		"## Canon": `## Canon

- Epic: [[MEM]]
- Spec: docs/specs/MEMORY_RFC.md
- Design review: 2026-04-15`,
		"## Code anchors": `## Code anchors

- src/memory/backend.ts
- src/memory/types.ts
- tests/memory.spec.ts`,
		"## Plan": `## Plan

1. Pick storage engine
2. Wire API surface
3. Add integration tests`,
		"## Verification plan": `## Verification plan

- Unit tests for the backend
- Integration test for restart persistence
- Benchmark baseline before and after`,
		"## Evidence": `## Evidence

- Integration test output attached
- Manual restart check completed
- API happy-path verified`,
	})
	populateFile(t, bug, map[string]string{
		"## Summary": `## Summary

Empty input crashes the request handler instead of returning a typed validation error.`,
		"## Repro": `## Repro

1. Send an empty payload.
2. Observe the server panic.
3. Compare against the expected validation path.

Expected:

The API returns a typed 4xx validation error.

Observed:

The process crashes before the handler returns.`,
		"## Root cause": `## Root cause

The empty-input branch dereferences a nil request body before validation.`,
		"## Fix": `## Fix

Guard the empty-input path before dereferencing and return a typed validation failure instead.`,
		"## Verification plan": `## Verification plan

- [x] Added regression coverage for empty input
- [x] Replayed the original repro after the fix`,
		"## Evidence": `## Evidence

- Repro captured
- Failure trace attached
- Fix verification completed

---`,
	})

	run("reindex", "--vault", vault)
	assertExists(t, epicIndex)
	assertExists(t, story)
	assertExists(t, bug)
	assertExists(t, doc)
	assertExists(t, filepath.Join(vault, "_system", "generated", "epics.index.json"))
	assertExists(t, filepath.Join(vault, "_system", "generated", "stories.index.json"))
	assertExists(t, filepath.Join(vault, "_system", "generated", "bugs.index.json"))
	assertExists(t, filepath.Join(vault, "_system", "generated", "dashboard.json"))

	validateJSON := runJSON("validate", "--vault", vault, "--json")
	assertEqual(t, true, validateJSON["ok"], "validate --json ok")

	storyData, storyBody, err := parseFrontmatterMustRead(story)
	if err != nil {
		t.Fatal(err)
	}
	storyData["related"] = []string{"[[MEM-B-0001]]"}
	storyData["related_record_ids"] = []string{}
	storyData["blocks"] = []string{"[[MEM-B-0001]]"}
	storyData["blocks_record_ids"] = []string{}
	storyData["blocked_by"] = []string{"[[MEM-B-0001]]"}
	storyData["blocked_by_record_ids"] = []string{}
	storyContent, err := serializeDocument(storyData, storyBody, frontmatterOrderForType("story"))
	if err != nil {
		t.Fatal(err)
	}
	if err := writeText(story, storyContent); err != nil {
		t.Fatal(err)
	}
	validateJSON = runJSON("validate", "--vault", vault, "--json")
	assertEqual(t, true, validateJSON["ok"], "validate tolerates resolvable stale record-id mirrors")
	run("reindex", "--vault", vault)
	storyData, _, err = parseFrontmatterMustRead(story)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, 0, len(normalizeList(storyData["related_record_ids"])), "plain reindex does not repair note frontmatter")
	fixJSON := runJSON("reindex", "--vault", vault, "--fix-links", "--json")
	assertEqual(t, true, fixJSON["ok"], "reindex --fix-links ok")
	assertEqual(t, 1.0, fixJSON["fixed_links"], "reindex --fix-links changed one note")
	bugData, _, err := parseFrontmatterMustRead(bug)
	if err != nil {
		t.Fatal(err)
	}
	bugRecordID := stringField(bugData, "record_id")
	storyData, _, err = parseFrontmatterMustRead(story)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, bugRecordID, normalizeList(storyData["related_record_ids"])[0], "related_record_ids repaired from wikilink")
	assertEqual(t, bugRecordID, normalizeList(storyData["blocks_record_ids"])[0], "blocks_record_ids repaired from wikilink")
	assertEqual(t, bugRecordID, normalizeList(storyData["blocked_by_record_ids"])[0], "blocked_by_record_ids repaired from wikilink")
	validateJSON = runJSON("validate", "--vault", vault, "--json")
	assertEqual(t, true, validateJSON["ok"], "validate ok after link repair")

	listJSON := runJSON("list", "--vault", vault, "--json", "--type", "story")
	assertEqual(t, true, listJSON["ok"], "list --json ok")
	assertEqual(t, 1.0, listJSON["count"], "list --json count")
	items := listJSON["items"].([]any)
	first := items[0].(map[string]any)
	assertEqual(t, "MEM-S-0001", first["id"], "list item id")
	assertEqual(t, "none", first["review_state"], "fresh story review state")

	storyData, storyBody, err = parseFrontmatterMustRead(story)
	if err != nil {
		t.Fatal(err)
	}
	storyData["blocked_by"] = []any{}
	storyData["blocked_by_record_ids"] = []any{}
	storyContent, err = serializeDocument(storyData, storyBody, frontmatterOrderForType("story"))
	if err != nil {
		t.Fatal(err)
	}
	if err := writeText(story, storyContent); err != nil {
		t.Fatal(err)
	}

	run("set-status", "--vault", vault, "--id", "MEM-S-0001", "--status", "active", "--actor", "sarav")
	run("projects", "add", "--repo", repo, "--vault", vault)
	disableJSON := runJSONAt(repo, "projects", "disable", "--json")
	projectRow := disableJSON["project"].(map[string]any)
	assertEqual(t, false, projectRow["enabled"], "projects disable flips enabled bit")
	assertEqual(t, "disabled", projectRow["health"], "projects disable marks project disabled")
	run("daemon", "run", "--once")
	storyListJSON := runJSON("list", "--vault", vault, "--json", "--type", "story")
	storyItems := storyListJSON["items"].([]any)
	storyRow := storyItems[0].(map[string]any)
	assertEqual(t, "active", storyRow["status"], "disabled project stays active")
	runsJSON := runJSON("runs", "--json")
	assertEqual(t, 0.0, runsJSON["count"], "disabled project spawns no runs")
	enableJSON := runJSONAt(repo, "projects", "enable", "--json")
	projectRow = enableJSON["project"].(map[string]any)
	assertEqual(t, true, projectRow["enabled"], "projects enable flips enabled bit")

	runDaemonUntil(t, run, func() bool {
		storyListJSON := runJSON("list", "--vault", vault, "--json", "--type", "story")
		storyItems := storyListJSON["items"].([]any)
		storyRow := storyItems[0].(map[string]any)
		return storyRow["status"] == "in_review" && storyRow["review_state"] == "verification_requested"
	})

	storyListJSON = runJSON("list", "--vault", vault, "--json", "--type", "story")
	storyItems = storyListJSON["items"].([]any)
	storyRow = storyItems[0].(map[string]any)
	assertEqual(t, "in_review", storyRow["status"], "daemon moved story to review")
	assertEqual(t, "verification_requested", storyRow["review_state"], "daemon requested verification")

	run("review", "verify", "--vault", vault, "--id", "MEM-S-0001", "--by", "peer-agent", "--summary", "claims match current tree")

	run("review", "approve", "--vault", vault, "--id", "MEM-S-0001", "--by", "sarav", "--summary", "Looks good")

	reviewBlocked := runJSONExpectFail("pickup", "--vault", vault, "--id", "MEM-S-0001", "--by", "codex", "--json")
	reviewBlockedErr := reviewBlocked["error"].(map[string]any)
	assertEqual(t, "INVALID_TRANSITION", reviewBlockedErr["code"], "pickup deprecation code")

	run("attest", "--vault", vault, "--id", "MEM-S-0001", "--by", "sarav", "--role", "human")
	run("set-status", "--vault", vault, "--id", "MEM-S-0001", "--status", "done", "--actor", "sarav")
	run("reindex", "--vault", vault)

	dashboardRaw, err := os.ReadFile(filepath.Join(vault, "_system", "generated", "dashboard.json"))
	if err != nil {
		t.Fatal(err)
	}
	var dashboard map[string]any
	if err := json.Unmarshal(dashboardRaw, &dashboard); err != nil {
		t.Fatal(err)
	}
	counts := dashboard["counts"].(map[string]any)
	assertEqual(t, 0.0, counts["active"], "dashboard active")

	projectsJSON := runJSON("projects", "list", "--json")
	assertEqual(t, true, projectsJSON["ok"], "projects list ok")
	assertEqual(t, 1.0, projectsJSON["count"], "projects count")

	statusJSON := runJSON("daemon", "status", "--json")
	assertEqual(t, true, statusJSON["ok"], "daemon status ok")

	run("set-status", "--vault", vault, "--id", "MEM-B-0001", "--status", "active", "--actor", "sarav")
	rewriteWorkflowRunner(t, filepath.Join(vault, "WORKFLOW.md"), `python3 -c '
import json, os, pathlib, re, sys
print(json.dumps({"session_id":"bug-session-001"}))
resume = os.getenv("TUSKER_SESSION_REF", "")
path = pathlib.Path(os.environ["TUSKER_WORKSPACE"]) / "resume.txt"
if resume:
    path.write_text(resume)
    note = pathlib.Path(os.environ["TUSKER_NOTE_PATH"])
    text = note.read_text()
    text = re.sub("status: \"[^\"]+\"", "status: \"in_review\"", text, count=1)
    text = re.sub("review_state: \"[^\"]+\"", "review_state: \"verification_requested\"", text, count=1)
    note.write_text(text)
sys.exit(0 if resume else 7)
'`)
	runDaemonUntil(t, run, func() bool {
		runsJSON := runJSON("runs", "--json")
		for _, item := range runsJSON["runs"].([]any) {
			row := item.(map[string]any)
			if row["item_id"] == "MEM-B-0001" && row["lease_state"] == "retry_queued" {
				return true
			}
		}
		return false
	})
	runsJSON = runJSON("runs", "--json")
	assertEqual(t, true, runsJSON["ok"], "runs ok")
	foundRetry := false
	bugWorkspace := ""
	for _, item := range runsJSON["runs"].([]any) {
		row := item.(map[string]any)
		if row["item_id"] == "MEM-B-0001" && row["lease_state"] == "retry_queued" {
			foundRetry = true
			bugWorkspace = row["workspace_path"].(string)
		}
	}
	if !foundRetry {
		t.Fatalf("expected MEM-B-0001 to be retry_queued after a failed daemon attempt")
	}
	run("retry", "now", "--id", "MEM-B-0001")
	runDaemonUntil(t, run, func() bool {
		bugListJSON := runJSON("list", "--vault", vault, "--json", "--type", "bug")
		bugItems := bugListJSON["items"].([]any)
		bugRow := bugItems[0].(map[string]any)
		return bugRow["status"] == "in_review"
	})
	bugListJSON := runJSON("list", "--vault", vault, "--json", "--type", "bug")
	bugItems := bugListJSON["items"].([]any)
	bugRow := bugItems[0].(map[string]any)
	assertEqual(t, "in_review", bugRow["status"], "retry promoted bug to review")
	assertEqual(t, "verification_requested", bugRow["review_state"], "retry requested verification")
	resumeMarker, err := os.ReadFile(filepath.Join(bugWorkspace, "resume.txt"))
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "bug-session-001", strings.TrimSpace(string(resumeMarker)), "resume reused prior session")
	run("review", "request-changes", "--vault", vault, "--id", "MEM-B-0001", "--by", "sarav", "--summary", "Handle the empty payload edge case more cleanly")
	rewriteWorkflowRunner(t, filepath.Join(vault, "WORKFLOW.md"), markReviewCommand())
	runDaemonUntil(t, run, func() bool {
		bugListJSON := runJSON("list", "--vault", vault, "--json", "--type", "bug")
		bugItems := bugListJSON["items"].([]any)
		bugRow := bugItems[0].(map[string]any)
		return bugRow["status"] == "in_review" && bugRow["work_revision"] == 1.0 && bugRow["review_state"] == "verification_requested"
	})
	bugListJSON = runJSON("list", "--vault", vault, "--json", "--type", "bug")
	bugItems = bugListJSON["items"].([]any)
	bugRow = bugItems[0].(map[string]any)
	assertEqual(t, "in_review", bugRow["status"], "rework returned bug to review")
	assertEqual(t, 1.0, bugRow["work_revision"], "review request-changes incremented work revision")
	assertEqual(t, "verification_requested", bugRow["review_state"], "rework returns to verification requested")

	missingArg := runJSONExpectFail("new-story", "--vault", vault, "--json")
	missingArgErr := missingArg["error"].(map[string]any)
	assertEqual(t, "MISSING_ARG", missingArgErr["code"], "missing arg code")

	run("sync-repo-contract", "--repo", repo)
	run("install", "--repo", repo, "--no-bin")
	assertExists(t, filepath.Join(repo, ".agents", "skills", "tusker", "SKILL.md"))
	assertExists(t, filepath.Join(repo, ".claude", "skills", "tusker", "references", "COMMANDS.md"))

	userSkill := filepath.Join(homeDir, ".agents", "skills", "tusker")
	if err := os.MkdirAll(userSkill, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(userSkill, "SKILL.md"), []byte("stale skill\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	updateOutput := runWithHome("update", "--bin-dir", binDir)
	linkPath := filepath.Join(binDir, "tusker")
	assertExists(t, linkPath)
	assertExists(t, filepath.Join(userSkill, "references", "COMMANDS.md"))
	if !strings.Contains(updateOutput, "Updated Tusker skill at") {
		t.Fatalf("expected update output to mention refreshed skill, got:\n%s", updateOutput)
	}
	target, err := os.Readlink(linkPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(target, filepath.Join("dist", "tusker")) {
		t.Fatalf("expected install symlink to point at dist/tusker, got %s", target)
	}
}

func TestRunInspectInterruptAndResume(t *testing.T) {
	repoRoot, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	tempRoot := t.TempDir()
	binary := filepath.Join(tempRoot, "tusker")
	build := exec.Command("go", "build", "-o", binary, ".")
	build.Dir = repoRoot
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build failed: %v\n%s", err, output)
	}

	vault := filepath.Join(tempRoot, "vault")
	repo := filepath.Join(tempRoot, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}

	run := func(args ...string) string {
		t.Helper()
		cmd := exec.Command(binary, args...)
		cmd.Dir = repoRoot
		cmd.Env = append(os.Environ(), "TUSKER_STATE_ROOT="+filepath.Join(tempRoot, "state"))
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("command failed: %s\n%s", strings.Join(args, " "), output)
		}
		return string(output)
	}

	runJSON := func(args ...string) map[string]any {
		t.Helper()
		output := run(args...)
		lines := strings.Split(strings.TrimSpace(output), "\n")
		last := lines[len(lines)-1]
		var decoded map[string]any
		if err := json.Unmarshal([]byte(last), &decoded); err != nil {
			t.Fatalf("invalid json output for %s: %v\n%s", strings.Join(args, " "), err, output)
		}
		return decoded
	}

	run("bootstrap", "--vault", vault)
	rewriteWorkflowRunner(t, filepath.Join(vault, "WORKFLOW.md"), `python3 -c 'import json,time; print(json.dumps({"session_id":"long-run-001","message":{"id":"msg-0001"}}), flush=True); time.sleep(5); print(json.dumps({"done":True}), flush=True)'`)
	run("new-epic", "--vault", vault, "--acronym", "MEM", "--title", "Memory system", "--summary", "Unify durable memory tracking and review workflows.", "--owner", "sarav", "--spec-source", "docs/specs/MEMORY_RFC.md")
	run("new-story", "--vault", vault, "--epic", "MEM", "--title", "Long running agent flow", "--change-type", "feature", "--size", "m", "--risk", "medium", "--priority", "p1", "--delegation", "execute", "--surfaces", "api", "--assignee", "codex", "--requester", "sarav", "--ai-assistance", "heavy", "--ai-tools", "codex")

	story := filepath.Join(vault, "epics", "MEM", "MEM-S-0001.md")
	populateFile(t, story, map[string]string{
		"## Problem": `## Problem

The agent needs to hold a long-lived coding session across daemon polls.`,
		"## Acceptance criteria": `## Acceptance criteria

- [ ] Mid-flight run survives daemon restarts
- [ ] Operator can inspect and interrupt safely
- [ ] Retry resumes the same agent session`,
		"## Canon": `## Canon

- Epic: [[MEM]]
- Spec: docs/specs/MEMORY_RFC.md`,
		"## Code anchors": `## Code anchors

- src/agent/runtime.ts`,
		"## Plan": `## Plan

1. Start a long-running process
2. Reattach across polls
3. Interrupt and resume`,
		"## Verification plan": `## Verification plan

- Repeat daemon polls while the process is live
- Interrupt via CLI
- Resume from the same session`,
		"## Evidence": `## Evidence

- Long-running session observed
- Interrupt path verified
- Resume verification completed`,
	})
	run("reindex", "--vault", vault)
	run("projects", "add", "--repo", repo, "--vault", vault)
	run("set-status", "--vault", vault, "--id", "MEM-S-0001", "--status", "active", "--actor", "sarav")

	run("daemon", "run", "--once")
	time.Sleep(300 * time.Millisecond)
	run("daemon", "run", "--once")

	inspectJSON := runJSON("runs", "inspect", "--id", "MEM-S-0001", "--json")
	runRow := inspectJSON["run"].(map[string]any)
	assertEqual(t, "running", runRow["lease_state"], "long run lease state")
	assertEqual(t, 1.0, runRow["attempt_count"], "no duplicate attempt while running")
	assertEqual(t, "long-run-001", runRow["session_ref"], "session ref captured during running reconcile")
	attempts := inspectJSON["attempts"].([]any)
	assertEqual(t, 1, len(attempts), "single active attempt")
	sessionRow := inspectJSON["session"].(map[string]any)
	assertEqual(t, "long-run-001", sessionRow["session_ref"], "session persisted")
	assertEqual(t, "msg-0001", sessionRow["last_message_ref"], "message ref persisted")

	logsJSON := runJSON("runs", "logs", "--id", "MEM-S-0001", "--json", "--lines", "10")
	if !strings.Contains(logsJSON["tail"].(string), "long-run-001") {
		t.Fatalf("expected logs tail to contain session id, got: %s", logsJSON["tail"].(string))
	}

	run("daemon", "run", "--once")
	inspectJSON = runJSON("runs", "inspect", "--id", "MEM-S-0001", "--json")
	runRow = inspectJSON["run"].(map[string]any)
	assertEqual(t, 1.0, runRow["attempt_count"], "re-poll still does not spawn duplicate attempt")

	run("runs", "interrupt", "--id", "MEM-S-0001")
	runDaemonUntil(t, run, func() bool {
		inspect := runJSON("runs", "inspect", "--id", "MEM-S-0001", "--json")
		row := inspect["run"].(map[string]any)
		return row["lease_state"] == "interrupted"
	})

	rewriteWorkflowRunner(t, filepath.Join(vault, "WORKFLOW.md"), `python3 -c '
import json, os, pathlib, re, sys
print(json.dumps({"session_id":"long-run-001"}))
path = pathlib.Path(os.environ["TUSKER_WORKSPACE"]) / "resume-after-interrupt.txt"
path.write_text(os.getenv("TUSKER_SESSION_REF", ""))
note = pathlib.Path(os.environ["TUSKER_NOTE_PATH"])
text = note.read_text()
text = re.sub("status: \"[^\"]+\"", "status: \"in_review\"", text, count=1)
text = re.sub("review_state: \"[^\"]+\"", "review_state: \"verification_requested\"", text, count=1)
note.write_text(text)
sys.exit(0)
'`)
	run("retry", "now", "--id", "MEM-S-0001")
	runDaemonUntil(t, run, func() bool {
		storyListJSON := runJSON("list", "--vault", vault, "--json", "--type", "story")
		storyItems := storyListJSON["items"].([]any)
		storyRow := storyItems[0].(map[string]any)
		return storyRow["status"] == "in_review"
	})

	resumeMarker, err := os.ReadFile(filepath.Join(runRow["workspace_path"].(string), "resume-after-interrupt.txt"))
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "long-run-001", strings.TrimSpace(string(resumeMarker)), "resume after interrupt reused prior session")
	storyListJSON := runJSON("list", "--vault", vault, "--json", "--type", "story")
	storyItems := storyListJSON["items"].([]any)
	storyRow := storyItems[0].(map[string]any)
	assertEqual(t, "verification_requested", storyRow["review_state"], "long run completion requests verification")
}

func TestDeveloperDocRequiresExplicitIntent(t *testing.T) {
	repoRoot, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	tempRoot := t.TempDir()
	binary := filepath.Join(tempRoot, "tusker")
	build := exec.Command("go", "build", "-o", binary, ".")
	build.Dir = repoRoot
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build failed: %v\n%s", err, output)
	}

	vault := filepath.Join(tempRoot, "vault")
	run := func(args ...string) string {
		t.Helper()
		cmd := exec.Command(binary, args...)
		cmd.Dir = repoRoot
		cmd.Env = append(os.Environ(), "TUSKER_STATE_ROOT="+filepath.Join(tempRoot, "state"))
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("command failed: %s\n%s", strings.Join(args, " "), output)
		}
		return string(output)
	}
	runJSONExpectFail := func(args ...string) map[string]any {
		t.Helper()
		cmd := exec.Command(binary, args...)
		cmd.Dir = repoRoot
		cmd.Env = append(os.Environ(), "TUSKER_STATE_ROOT="+filepath.Join(tempRoot, "state"))
		output, err := cmd.CombinedOutput()
		if err == nil {
			t.Fatalf("expected command failure: %s\n%s", strings.Join(args, " "), output)
		}
		lines := strings.Split(strings.TrimSpace(string(output)), "\n")
		last := lines[len(lines)-1]
		var decoded map[string]any
		if err := json.Unmarshal([]byte(last), &decoded); err != nil {
			t.Fatalf("invalid json error output for %s: %v\n%s", strings.Join(args, " "), err, output)
		}
		return decoded
	}

	run("bootstrap", "--vault", vault)
	run("new-epic", "--vault", vault, "--acronym", "MEM", "--title", "Memory system", "--summary", "Memory canon and execution flow.", "--owner", "sarav")

	failure := runJSONExpectFail("new-doc", "--vault", vault, "--epic", "MEM", "--title", "Spec note", "--audience", "developer", "--json")
	failureErr := failure["error"].(map[string]any)
	assertEqual(t, "INVALID_ARG", failureErr["code"], "developer doc requires explicit intent")

	run("new-doc", "--vault", vault, "--epic", "MEM", "--title", "Canonical spec", "--audience", "developer", "--canon-for", "MEM")
	docPath := filepath.Join(vault, "epics", "MEM", "MEM-D-0001.md")
	docText, err := os.ReadFile(docPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(docText), `doc_intent: "canon"`) {
		t.Fatalf("expected canonical doc intent in %s", docPath)
	}
	if !strings.Contains(string(docText), `canon_for: "[[MEM]]"`) {
		t.Fatalf("expected canon_for in %s", docPath)
	}

	run("new-doc", "--vault", vault, "--epic", "MEM", "--title", "Memory user guide", "--audience", "user", "--status", "approved", "--publish", "true", "--publish-path", "user/guides/memory", "--publish-description", "User guide for the memory system.", "--publish-order", "20", "--publish-section-title", "Guides", "--tags", "memory,user-guide")
	publishedPath := filepath.Join(vault, "epics", "MEM", "MEM-D-0002.md")
	publishedText, err := os.ReadFile(publishedPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		`publish: true`,
		`publish_path: "user/guides/memory"`,
		`publish_description: "User guide for the memory system."`,
		`publish_order: "20"`,
		`publish_section_title: "Guides"`,
		`- "memory"`,
		`- "user-guide"`,
	} {
		if !strings.Contains(string(publishedText), expected) {
			t.Fatalf("published doc missing %q in %s:\n%s", expected, publishedPath, publishedText)
		}
	}
}

func TestHandoffCommandRendersRoleSpecificPacket(t *testing.T) {
	repoRoot, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	tempRoot := t.TempDir()
	binary := filepath.Join(tempRoot, "tusker")
	build := exec.Command("go", "build", "-o", binary, ".")
	build.Dir = repoRoot
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build failed: %v\n%s", err, output)
	}

	vault := filepath.Join(tempRoot, "vault")
	run := func(args ...string) string {
		t.Helper()
		cmd := exec.Command(binary, args...)
		cmd.Dir = repoRoot
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("command failed: %s\n%s", strings.Join(args, " "), output)
		}
		return string(output)
	}

	run("bootstrap", "--vault", vault)
	run("new-epic", "--vault", vault, "--acronym", "MEM", "--title", "Memory system", "--summary", "Memory work.", "--owner", "sarav")
	run("new-story", "--vault", vault, "--epic", "MEM", "--title", "Ship memory verifier", "--change-type", "feature", "--size", "m", "--risk", "medium", "--priority", "p1", "--delegation", "execute", "--ai-assistance", "heavy", "--ai-tools", "codex")

	story := filepath.Join(vault, "epics", "MEM", "MEM-S-0001.md")
	populateFile(t, story, map[string]string{
		"## Problem": `## Problem

Worker claims and reality can drift apart.`,
		"## Acceptance criteria": `## Acceptance criteria

- [ ] Worker output is truth-checked before human approval
- [ ] Copy-pasteable handoff exists for verifier`,
		"## Canon": `## Canon

- Epic: [[MEM]]`,
		"## Code anchors": `## Code anchors

- cmd/tusker/daemon.go
- cmd/tusker/review_commands.go`,
		"## Verification plan": `## Verification plan

- Run tusker handoff
- Run review verify`,
	})
	run("reindex", "--vault", vault)

	workerPacket := run("handoff", "--vault", vault, "--id", "MEM-S-0001", "--for", "worker")
	if !strings.Contains(workerPacket, "# Tusker Worker handoff") {
		t.Fatalf("expected worker packet heading, got:\n%s", workerPacket)
	}
	if !strings.Contains(workerPacket, "Do not self-certify review readiness") {
		t.Fatalf("expected worker-specific instructions, got:\n%s", workerPacket)
	}

	verifierPacket := run("handoff", "--vault", vault, "--id", "MEM-S-0001", "--for", "verifier")
	if !strings.Contains(verifierPacket, "tusker review verify --id MEM-S-0001") {
		t.Fatalf("expected verifier command in packet, got:\n%s", verifierPacket)
	}
	if !strings.Contains(verifierPacket, "Verify the worker's claims against the current working tree") {
		t.Fatalf("expected verifier instructions, got:\n%s", verifierPacket)
	}
}

func TestActiveEpicWithCanonAndNoStoriesFailsValidation(t *testing.T) {
	repoRoot, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	tempRoot := t.TempDir()
	binary := filepath.Join(tempRoot, "tusker")
	build := exec.Command("go", "build", "-o", binary, ".")
	build.Dir = repoRoot
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build failed: %v\n%s", err, output)
	}

	vault := filepath.Join(tempRoot, "vault")
	run := func(args ...string) string {
		t.Helper()
		cmd := exec.Command(binary, args...)
		cmd.Dir = repoRoot
		cmd.Env = append(os.Environ(), "TUSKER_STATE_ROOT="+filepath.Join(tempRoot, "state"))
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("command failed: %s\n%s", strings.Join(args, " "), output)
		}
		return string(output)
	}
	runJSONExpectFail := func(args ...string) map[string]any {
		t.Helper()
		cmd := exec.Command(binary, args...)
		cmd.Dir = repoRoot
		cmd.Env = append(os.Environ(), "TUSKER_STATE_ROOT="+filepath.Join(tempRoot, "state"))
		output, err := cmd.CombinedOutput()
		if err == nil {
			t.Fatalf("expected command failure: %s\n%s", strings.Join(args, " "), output)
		}
		lines := strings.Split(strings.TrimSpace(string(output)), "\n")
		last := lines[len(lines)-1]
		var decoded map[string]any
		if err := json.Unmarshal([]byte(last), &decoded); err != nil {
			t.Fatalf("invalid json error output for %s: %v\n%s", strings.Join(args, " "), err, output)
		}
		return decoded
	}

	run("bootstrap", "--vault", vault)
	run("new-epic", "--vault", vault, "--acronym", "MEM", "--title", "Memory system", "--summary", "Memory canon and execution flow.", "--owner", "sarav", "--spec-source", "docs/specs/memory.md", "--status", "active")
	populateFile(t, filepath.Join(vault, "epics", "MEM", "index.md"), map[string]string{
		"## Problem": `## Problem

Memory needs an explicit canon and execution model.`,
		"## Scope and non-goals": `## Scope and non-goals

In scope:

- define canon location

Out of scope:

- implementation

Non-goals:

- shipping code in this note`,
		"## Success criteria": `## Success criteria

- Canon is explicit
- Stories exist before execution starts`,
		"## Open questions": `## Open questions

- Should the first implementation story own runtime validation?`,
	})

	failure := runJSONExpectFail("validate", "--vault", vault, "--json")
	errors := failure["errors"].([]any)
	found := false
	for _, item := range errors {
		row := item.(map[string]any)
		if row["code"] == "EPIC_STORY_STACK_MISSING" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected EPIC_STORY_STACK_MISSING, got %#v", errors)
	}
}

func TestDaemonHonorsProjectAndGlobalRunLimits(t *testing.T) {
	repoRoot, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	tempRoot := t.TempDir()
	binary := filepath.Join(tempRoot, "tusker")
	build := exec.Command("go", "build", "-o", binary, ".")
	build.Dir = repoRoot
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build failed: %v\n%s", err, output)
	}

	stateRoot := filepath.Join(tempRoot, "state")
	baseEnv := append(os.Environ(), "TUSKER_STATE_ROOT="+stateRoot)

	run := func(dir string, args ...string) string {
		t.Helper()
		cmd := exec.Command(binary, args...)
		cmd.Dir = dir
		cmd.Env = baseEnv
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("command failed: %s\n%s", strings.Join(args, " "), output)
		}
		return string(output)
	}
	runJSON := func(dir string, args ...string) map[string]any {
		t.Helper()
		output := run(dir, args...)
		lines := strings.Split(strings.TrimSpace(output), "\n")
		last := lines[len(lines)-1]
		var decoded map[string]any
		if err := json.Unmarshal([]byte(last), &decoded); err != nil {
			t.Fatalf("invalid json output for %s: %v\n%s", strings.Join(args, " "), err, output)
		}
		return decoded
	}

	setupProject := func(repoName, acronym string, storyTitles ...string) (string, string) {
		t.Helper()
		repo := filepath.Join(tempRoot, repoName)
		vault := filepath.Join(tempRoot, repoName+"-vault")
		if err := os.MkdirAll(repo, 0o755); err != nil {
			t.Fatal(err)
		}
		run(repoRoot, "bootstrap", "--vault", vault)
		rewriteWorkflowRunner(t, filepath.Join(vault, "WORKFLOW.md"), `python3 -c 'import json,time; print(json.dumps({"session_id":"limit-test"}), flush=True); time.sleep(5)'`)
		run(repoRoot, "new-epic", "--vault", vault, "--acronym", acronym, "--title", repoName, "--summary", "Dispatch limit test.", "--owner", "sarav")
		for _, title := range storyTitles {
			run(repoRoot, "new-story", "--vault", vault, "--epic", acronym, "--title", title, "--change-type", "feature", "--size", "m", "--risk", "medium", "--priority", "p1", "--delegation", "execute", "--assignee", "codex", "--requester", "sarav", "--ai-assistance", "heavy", "--ai-tools", "codex")
		}
		run(repoRoot, "projects", "add", "--repo", repo, "--vault", vault)
		run(repoRoot, "projects", "limits", "--repo", repo, "--max-active-runs", "1")
		return repo, vault
	}

	repoA, vaultA := setupProject("repo-a", "AAA", "Alpha one", "Alpha two")
	_, vaultB := setupProject("repo-b", "BBB", "Beta one")
	run(repoRoot, "daemon", "limits", "--max-active-runs", "2")

	run(repoRoot, "set-status", "--vault", vaultA, "--id", "AAA-S-0001", "--status", "active", "--actor", "sarav")
	run(repoRoot, "set-status", "--vault", vaultA, "--id", "AAA-S-0002", "--status", "active", "--actor", "sarav")
	run(repoRoot, "set-status", "--vault", vaultB, "--id", "BBB-S-0001", "--status", "active", "--actor", "sarav")

	run(repoRoot, "daemon", "run", "--once")

	runsJSON := runJSON(repoRoot, "runs", "--json")
	activeItems := map[string]bool{}
	for _, item := range runsJSON["runs"].([]any) {
		row := item.(map[string]any)
		if row["lease_state"] == "claimed" || row["lease_state"] == "running" {
			activeItems[row["item_id"].(string)] = true
		}
	}
	assertEqual(t, 2, len(activeItems), "global active run cap")
	assertEqual(t, true, activeItems["AAA-S-0001"] || activeItems["AAA-S-0002"], "first project got one slot")
	assertEqual(t, false, activeItems["AAA-S-0001"] && activeItems["AAA-S-0002"], "per-project cap blocked second slot")
	assertEqual(t, true, activeItems["BBB-S-0001"], "second project used remaining global slot")

	run(repoRoot, "projects", "limits", "--repo", repoA, "--max-active-runs", "2")
	run(repoRoot, "daemon", "limits", "--max-active-runs", "3")
	run(repoRoot, "daemon", "run", "--once")

	runsJSON = runJSON(repoRoot, "runs", "--json")
	activeItems = map[string]bool{}
	for _, item := range runsJSON["runs"].([]any) {
		row := item.(map[string]any)
		if row["lease_state"] == "claimed" || row["lease_state"] == "running" {
			activeItems[row["item_id"].(string)] = true
		}
	}
	assertEqual(t, 3, len(activeItems), "raised limits allow third dispatch")
	assertEqual(t, true, activeItems["AAA-S-0001"] || activeItems["AAA-S-0002"], "project A still has active work")
	assertEqual(t, true, activeItems["AAA-S-0001"] && activeItems["AAA-S-0002"], "project limit hot update allowed second project A slot")
	assertEqual(t, true, activeItems["BBB-S-0001"], "project B kept its slot")

	storiesA := runJSON(repoRoot, "list", "--vault", vaultA, "--json", "--type", "story")["items"].([]any)
	statusesA := map[string]string{}
	for _, item := range storiesA {
		row := item.(map[string]any)
		statusesA[row["id"].(string)] = row["status"].(string)
	}
	assertEqual(t, "active", statusesA["AAA-S-0001"], "running story remains active")
	assertEqual(t, "active", statusesA["AAA-S-0002"], "undispatched story remains active")

	_ = repoA
}

func assertExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected file to exist: %s", path)
	}
}

func assertEqual(t *testing.T, expected, actual any, label string) {
	t.Helper()
	if expected != actual {
		t.Fatalf("%s: expected %v, got %v", label, expected, actual)
	}
}

func populateFile(t *testing.T, filePath string, replacements map[string]string) {
	t.Helper()
	raw, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for heading, replacement := range replacements {
		text = replaceTestSection(text, heading, replacement)
	}
	if err := os.WriteFile(filePath, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func replaceTestSection(text, heading, replacement string) string {
	lines := strings.Split(text, "\n")
	target := strings.TrimSpace(heading)
	headingIdx := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == target {
			headingIdx = i
			break
		}
	}
	if headingIdx == -1 {
		return text
	}
	endIdx := len(lines)
	for i := headingIdx + 1; i < len(lines); i++ {
		if strings.HasPrefix(lines[i], "## ") {
			endIdx = i
			break
		}
	}
	return strings.Join(lines[:headingIdx], "\n") + "\n" + replacement + "\n\n" + strings.Join(lines[endIdx:], "\n")
}

func rewriteWorkflowRunner(t *testing.T, workflowPath, command string) {
	t.Helper()
	text, err := readText(workflowPath)
	if err != nil {
		t.Fatal(err)
	}
	data, body, err := parseFrontmatter(text)
	if err != nil {
		t.Fatal(err)
	}
	codexBlock, ok := data["codex"].(map[string]any)
	if !ok || codexBlock == nil {
		codexBlock = map[string]any{}
	}
	codexBlock["command"] = command
	data["codex"] = codexBlock
	fm, err := stringifyFrontmatter(data, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeText(workflowPath, fm+"\n"+strings.TrimLeft(body, "\n")); err != nil {
		t.Fatal(err)
	}
}

func markReviewCommand() string {
	return `python3 -c '
import os, pathlib, re
note = pathlib.Path(os.environ["TUSKER_NOTE_PATH"])
text = note.read_text()
text = re.sub("status: \"[^\"]+\"", "status: \"in_review\"", text, count=1)
text = re.sub("review_state: \"[^\"]+\"", "review_state: \"verification_requested\"", text, count=1)
note.write_text(text)
'`
}

func runDaemonUntil(t *testing.T, run func(args ...string) string, predicate func() bool) {
	t.Helper()
	for attempt := 0; attempt < 12; attempt++ {
		run("daemon", "run", "--once")
		if predicate() {
			return
		}
		time.Sleep(150 * time.Millisecond)
	}
	t.Fatal("daemon did not converge before timeout")
}
