package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	runnercore "tusker/internal/runner"
)

func coordinatorTestRoot(t *testing.T) string {
	t.Helper()
	root, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	return root
}
func coordinatorTestVault(t *testing.T) string {
	t.Helper()
	root := coordinatorTestRoot(t)
	t.Setenv("TUSKER_STATE_ROOT", filepath.Join(root, "state"))
	vault := filepath.Join(root, "repo", ".tusker")
	mustWave(t, Args{"vault": vault, "quiet": "true"}, bootstrap)
	if e := writeDefaultWorkflow(vault); e != nil {
		t.Fatal(e)
	}
	mustWave(t, Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "Batch tests", "summary": "Synthetic local workers."}, newV7Epic)
	repo := v7RepoRoot(vault)
	for _, args := range [][]string{{"init", "-b", "main"}, {"config", "user.name", "Batch Tester"}, {"config", "user.email", "batch@example.test"}} {
		if _, e := coordinatorGit(repo, args...); e != nil {
			t.Fatal(e)
		}
	}
	if e := os.WriteFile(filepath.Join(repo, ".gitignore"), []byte(".tusker/scratch/\n"), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e := coordinatorGit(repo, "add", "."); e != nil {
		t.Fatal(e)
	}
	if _, e := coordinatorGit(repo, "commit", "-m", "Initial fixture"); e != nil {
		t.Fatal(e)
	}
	return vault
}
func coordinatorTestTask(t *testing.T, vault, id, title, resources, deps string) {
	t.Helper()
	body := fmt.Sprintf("# %s\n\n## Problem\n\nExercise a batch worker.\n\n## Acceptance\n\n- [ ] A1: Worker writes its owned file.\n\n## Verification\n\n| Covers | Check | Result | Notes |\n| --- | --- | --- | --- |\n| A1 | command: test -f %s.txt | pending | |\n", title, id)
	path := filepath.Join(v7RepoRoot(vault), ".tusker", "scratch", id+"-body.md")
	if e := writeText(path, body); e != nil {
		t.Fatal(e)
	}
	mustWave(t, Args{"vault": vault, "quiet": "true", "id": id, "epic": "APP", "title": title, "owned-paths": id + ".txt", "body-file": path, "runner-profile": "batch-test", "resources": resources, "dependencies": deps, "budget": "30s"}, newV7Task)
}
func coordinatorTestProfile(t *testing.T, command string) {
	t.Helper()
	setGlobalProfileForTest(t, "batch-test", map[string]any{"harness": "codex_exec", "model": "synthetic-test-model", "effort": "low", "command": command, "permission_preset": "danger-full-access", "sandbox": map[string]any{"mode": "danger-full-access", "network": true}})
}

// This subprocess is a local deterministic supervisor, never a model invocation.
func TestCoordinatorHelperProcess(t *testing.T) {
	if os.Getenv("TUSKER_COORDINATOR_TEST_HELPER") != "1" {
		return
	}
	var path string
	for i, arg := range os.Args {
		if arg == "--" && i+1 < len(os.Args) {
			path = os.Args[i+1]
			break
		}
	}
	e := coordinatorBatchExecuteCmd(Args{"file": path})
	if e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
	os.Exit(0)
}
func coordinatorUseTestHelper(t *testing.T) {
	t.Helper()
	old := coordinatorHelperCommand
	coordinatorHelperCommand = func(path string) (*exec.Cmd, error) {
		cmd := exec.Command(os.Args[0], "-test.run=^TestCoordinatorHelperProcess$", "--", path)
		cmd.Env = append(os.Environ(), "TUSKER_COORDINATOR_TEST_HELPER=1")
		return cmd, nil
	}
	t.Cleanup(func() { coordinatorHelperCommand = old })
}
func TestCoordinatorBatchEndToEnd(t *testing.T) {
	vault := coordinatorTestVault(t)
	coordinatorUseTestHelper(t)
	root := filepath.Dir(v7RepoRoot(vault))
	markers := filepath.Join(root, "markers")
	if e := os.MkdirAll(markers, 0700); e != nil {
		t.Fatal(e)
	}
	script := filepath.Join(root, "fake-codex")
	text := `#!/bin/sh
case "$1" in
 --version) echo 'codex synthetic 1'; exit 0;;
 login) exit 0;;
esac
cat > "$TUSKER_BATCH_RUN_DIR/input.txt"
id="$TUSKER_TASK_ID"
dir="$TUSKER_BATCH_RUN_DIR"
markers=` + shellSingleQuote(markers) + `
if [ "$id" = APP-T-0001 ] && [ ! -f "$markers/crashed" ]; then touch "$markers/crashed"; exit 101; fi
if [ "$id" = APP-T-0002 ] && [ ! -f "$markers/questioned" ]; then
 touch "$markers/questioned"
 cat > "$dir/REPORT.json" <<EOF
{"schema":"tusker.batch-report/v1","task":"$id","verdict":"blocked","commits":[],"gates":[],"questions":[{"text":"What format?","fork":"format"}],"remaining":[],"boxes":[]}
EOF
 echo checkpoint > "$dir/REPORT.md"
 echo '{"type":"turn.completed"}'
 exit 0
fi
if [ "$id" != APP-T-0002 ]; then mkdir "$markers/cargo-active" || exit 77; fi
printf 'start %s\n' "$id" >> "$markers/events"
sleep 0.4
printf '%s\n' "$id" > "$id.txt"
git add "$id.txt" || exit 1
git commit -m "Implement $id" >/dev/null || exit 1
sha=$(git rev-parse HEAD)
remaining='[]'
if [ "$id" = APP-T-0001 ]; then remaining='[{"title":"Harden follow-up","why":"Exercise harvesting","evidence":"APP-T-0001.txt:1"}]'; fi
cat > "$dir/REPORT.json" <<EOF
{"schema":"tusker.batch-report/v1","task":"$id","verdict":"pass","commits":["$sha"],"gates":[{"acceptance":"A1","command":"test -f $id.txt","result":"pass","host":"local"}],"questions":[],"remaining":$remaining,"boxes":[]}
EOF
echo 'Synthetic verification passed.' > "$dir/REPORT.md"
printf 'end %s\n' "$id" >> "$markers/events"
if [ "$id" != APP-T-0002 ]; then rmdir "$markers/cargo-active"; fi
echo '{"type":"turn.completed","usage":{"input_tokens":12,"output_tokens":7}}'
`
	if e := os.WriteFile(script, []byte(text), 0700); e != nil {
		t.Fatal(e)
	}
	coordinatorTestProfile(t, script)
	coordinatorTestTask(t, vault, "APP-T-0001", "One", "cargo:synthetic=1", "")
	coordinatorTestTask(t, vault, "APP-T-0002", "Two", "docs-only", "")
	coordinatorTestTask(t, vault, "APP-T-0003", "Three", "cargo:synthetic=1", "APP-T-0001")
	if e := coordinatorForkCmd(Args{"vault": vault, "epic": "APP", "id": "format", "question": "What format?", "ruling": "Use the current format.", "by": "coordinator"}, "add"); e != nil {
		t.Fatal(e)
	}
	if e := coordinatorBatchCreateCmd(Args{"vault": vault, "name": "demo", "epic": "APP", "tasks": "APP-T-0001,APP-T-0002,APP-T-0003", "max-parallel": "2"}); e != nil {
		t.Fatal(e)
	}
	if e := coordinatorBatchCmd(Args{"vault": vault, "id": "demo", "notify": "echo notified >> " + shellSingleQuote(filepath.Join(markers, "notify"))}, "run"); e != nil {
		b, _ := coordinatorLoadBatch(vault, "demo")
		raw, _ := json.MarshalIndent(b, "", "  ")
		t.Fatalf("run: %v\n%s", e, raw)
	}
	b, e := coordinatorLoadBatch(vault, "demo")
	if e != nil {
		t.Fatal(e)
	}
	for _, task := range b.Tasks {
		if task.State != "review" || !task.Audit.Passed {
			t.Fatalf("task not review-ready: %+v", task)
		}
	}
	if b.Tasks[0].Policy["crash"] != 1 || b.Tasks[1].Policy["fork"] != 1 || len(b.Tasks[0].Followups) != 1 {
		t.Fatalf("missing recovery/harvest: %+v", b.Tasks)
	}
	events, _ := os.ReadFile(filepath.Join(markers, "events"))
	lines := strings.Split(strings.TrimSpace(string(events)), "\n")
	if len(lines) != 6 || !strings.HasPrefix(lines[0], "start ") || !strings.HasPrefix(lines[1], "start ") {
		t.Fatalf("independent workers did not overlap: %s", events)
	}
	if strings.Index(string(events), "start APP-T-0003") < strings.Index(string(events), "end APP-T-0001") {
		t.Fatalf("dependency ordering: %s", events)
	}
	// Finished batches are inert on rerun and never notify twice.
	if e := coordinatorBatchCmd(Args{"vault": vault, "id": "demo", "notify": "echo duplicate >> " + shellSingleQuote(filepath.Join(markers, "notify"))}, "run"); e != nil {
		t.Fatal(e)
	}
	notices, _ := os.ReadFile(filepath.Join(markers, "notify"))
	if string(notices) != "notified\n" {
		t.Fatalf("notifications: %q", notices)
	}
	if e := coordinatorCloseCmd(Args{"vault": vault, "id": "APP-T-0001", "verdict": "accept-with-followups"}); e != nil {
		t.Fatal(e)
	}
	if e := coordinatorDigestCmd(Args{"vault": vault, "since": "last", "json": "true"}); e != nil {
		t.Fatal(e)
	}
	r, e := coordinatorLoadRegistry(vault)
	if e != nil || r.LastDigest.IsZero() {
		t.Fatalf("digest watermark: %+v %v", r, e)
	}
	if len(strings.Split(strings.TrimSpace(coordinatorBatchDigest(b)), "\n")) > 40 {
		t.Fatal("digest exceeds one screen")
	}
}
func TestCoordinatorRejectsMissingProfileCycleAndDeadLinks(t *testing.T) {
	vault := coordinatorTestVault(t)
	coordinatorTestProfile(t, "codex")
	coordinatorTestTask(t, vault, "APP-T-0001", "One", "", "")
	note, _ := resolveV7Note(vault, "APP-T-0001", "task")
	_, _, e := mutateV7DocumentLocked(note.AbsolutePath, v7FrontmatterOrder["task"], func(d map[string]any, b string) (map[string]any, string, bool, error) {
		delete(d, "runner_profile")
		return d, b, true, nil
	})
	if e != nil {
		t.Fatal(e)
	}
	if e = coordinatorBatchCreateCmd(Args{"vault": vault, "name": "missing", "tasks": "APP-T-0001"}); e != nil {
		t.Fatal(e)
	}
	if e = coordinatorBatchCmd(Args{"vault": vault, "id": "missing"}, "run"); e == nil || !strings.Contains(e.Error(), "explicit") {
		t.Fatalf("missing profile: %v", e)
	}
	for _, body := range []string{"[gone](docs/absent.md)", "`docs/absent.md#Missing`", "[section](#absent)"} {
		if len(coordinatorBodyRefErrors(vault, body)) == 0 {
			t.Fatalf("accepted dead reference: %s", body)
		}
	}
	b := &coordinatorBatch{Tasks: []*coordinatorTask{{ID: "A", Dependencies: []string{"B"}}, {ID: "B", Dependencies: []string{"A"}}}}
	if e := coordinatorDAG(b, v7Index{}); e == nil {
		t.Fatal("accepted dependency cycle")
	}
}
func TestCoordinatorPolicyFlakyExpiryLimitsAndQuestions(t *testing.T) {
	vault := coordinatorTestVault(t)
	coordinatorTestProfile(t, "codex")
	coordinatorTestTask(t, vault, "APP-T-0001", "One", "", "")
	b := &coordinatorBatch{Schema: coordinatorSchema, ID: "policy", Vault: vault, Workspace: v7RepoRoot(vault), Epic: "APP"}
	task := &coordinatorTask{ID: "APP-T-0001", Workspace: b.Workspace, Owned: []string{"APP-T-0001.txt"}, Budget: time.Minute, Started: time.Now(), Policy: map[string]int{}}
	b.Tasks = []*coordinatorTask{task}
	dir := filepath.Join(coordinatorRunRoot(vault, b.ID), task.ID, "1")
	if e := os.MkdirAll(dir, 0700); e != nil {
		t.Fatal(e)
	}
	if e := coordinatorFlakyCmd(Args{"vault": vault, "_pos0": "exact-test", "reason": "intermittent fixture", "until": time.Now().AddDate(0, 0, 1).Format("2006-01-02")}, "add"); e != nil {
		t.Fatal(e)
	}
	if coordinatorFlakyActive(vault, coordinatorFlaky{Until: "2000-01-01"}, time.Now()) {
		t.Fatal("expired flaky entry active")
	}
	r := coordinatorReport{Schema: coordinatorReportSchema, Task: task.ID, Verdict: "blocked", Gates: []coordinatorGate{{Acceptance: "A1", Command: "full-suite", Result: "fail", Test: "exact-test", IsolatedCommand: "true"}}}
	receipt := runnercore.ExecutionReceipt{Outcome: runnercore.EventCompleted}
	if e := coordinatorApplyPolicy(b, task, receipt, r, nil, dir, time.Millisecond); e != nil {
		t.Fatal(e)
	}
	if task.State != "ready" || task.Policy["flaky:exact-test"] != 1 {
		t.Fatalf("flaky policy: %+v", task)
	}
	for i := 1; i <= 2; i++ {
		if _, e := os.Stat(filepath.Join(dir, fmt.Sprintf("flaky-%s-%d.log", coordinatorHash("exact-test"), i))); e != nil {
			t.Fatal(e)
		}
	}
	task.Policy = map[string]int{}
	r.Gates = nil
	r.Questions = []coordinatorQuestion{{Text: "new question"}}
	if e := coordinatorApplyPolicy(b, task, receipt, r, nil, dir, time.Millisecond); e != nil {
		t.Fatal(e)
	}
	if task.State != "parked" {
		t.Fatalf("novel question not parked: %+v", task)
	}
	r.Questions = nil
	r.Gates = []coordinatorGate{{Result: "fail", Test: "unknown"}}
	for i := 0; i < 2; i++ {
		if e := coordinatorApplyPolicy(b, task, receipt, r, nil, dir, time.Millisecond); e != nil {
			t.Fatal(e)
		}
	}
	if task.State != "failed" || task.Policy["unknown_gate"] != 1 {
		t.Fatal("unknown gate retry not capped")
	}
	task.Policy = map[string]int{}
	if e := os.WriteFile(filepath.Join(dir, "run.log"), []byte("usage limit"), 0600); e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 13; i++ {
		if e := coordinatorApplyPolicy(b, task, receipt, r, os.ErrNotExist, dir, time.Millisecond); e != nil {
			t.Fatal(e)
		}
	}
	if task.State != "failed" || task.Policy["usage_limit"] != 12 {
		t.Fatal("usage retries not capped")
	}
}
func TestCoordinatorAuditAndReviewedTriage(t *testing.T) {
	vault := coordinatorTestVault(t)
	coordinatorTestProfile(t, "codex")
	coordinatorTestTask(t, vault, "APP-T-0001", "One", "", "")
	root := v7RepoRoot(vault)
	base, e := coordinatorGit(root, "rev-parse", "HEAD")
	if e != nil {
		t.Fatal(e)
	}
	for _, file := range []string{"APP-T-0001.txt", "outside.txt", ".tusker/scratch/leak.txt"} {
		if e := writeText(filepath.Join(root, file), "sample"); e != nil {
			t.Fatal(e)
		}
	}
	if _, e := coordinatorGit(root, "add", "-f", "APP-T-0001.txt", "outside.txt", ".tusker/scratch/leak.txt"); e != nil {
		t.Fatal(e)
	}
	if _, e := coordinatorGit(root, "commit", "-m", "Bad fixture\n\nCo-Authored-By: Devin <test@example.test>"); e != nil {
		t.Fatal(e)
	}
	sha, _ := coordinatorGit(root, "rev-parse", "HEAD")
	dir := filepath.Join(DefaultStateRoot(), "audit")
	if e := os.MkdirAll(dir, 0700); e != nil {
		t.Fatal(e)
	}
	for _, file := range []string{"REPORT.md", "run.log"} {
		if e := writeText(filepath.Join(dir, file), "report"); e != nil {
			t.Fatal(e)
		}
	}
	task := &coordinatorTask{ID: "APP-T-0001", Base: base, Branch: "main", Workspace: root, Owned: []string{"APP-T-0001.txt"}}
	report := coordinatorReport{Verdict: "pass", Commits: []string{sha}, Gates: []coordinatorGate{{Acceptance: "A1", Command: "test", Result: "pass"}}}
	audit := coordinatorAuditReport(vault, task, report, dir)
	joined := strings.Join(audit.Failures, " ")
	for _, want := range []string{"AI attribution", "ignored path", "outside ownership"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %s: %+v", want, audit)
		}
	}
	report.Commits = nil
	audit = coordinatorAuditReport(vault, task, report, dir)
	if !strings.Contains(strings.Join(audit.Failures, " "), "unreported commit") {
		t.Fatal("omitted commit not audited")
	}
	note, _ := resolveV7Note(vault, task.ID, "task")
	file := filepath.Join(DefaultStateRoot(), "reviewed.json")
	rows := []coordinatorTriageRow{{Task: task.ID, Revision: stringField(note.Data, "state_rev"), Action: "discard", Reason: "Reviewed stale fixture"}}
	if e := coordinatorWriteJSON(file, rows); e != nil {
		t.Fatal(e)
	}
	if e := coordinatorTriageApplyCmd(Args{"vault": vault, "file": file}); e != nil {
		t.Fatal(e)
	}
	note, _ = resolveV7Note(vault, task.ID, "task")
	if stringField(note.Data, "status") != "cancelled" || !strings.Contains(note.Body, "Worker writes") {
		t.Fatal("triage lost history or did not discard")
	}
	if e := coordinatorTriageApplyCmd(Args{"vault": vault, "file": file}); e == nil {
		t.Fatal("accepted stale triage revision")
	}
}
func TestCoordinatorResourceLockAcrossControllers(t *testing.T) {
	root := coordinatorTestRoot(t)
	t.Setenv("TUSKER_STATE_ROOT", root)
	b := &coordinatorBatch{Vault: root, Capacities: map[string]int{}}
	one := &coordinatorTask{ID: "ONE", Resources: map[string]int{"cargo:host": 1}}
	two := &coordinatorTask{ID: "TWO", Resources: map[string]int{"cargo:host": 1}}
	files, e := coordinatorAcquireResources(b, one)
	if e != nil {
		t.Fatal(e)
	}
	if other, e := coordinatorAcquireResources(b, two); !coordinatorLockBusy(e) {
		coordinatorLocksClose(other)
		t.Fatalf("resource not exclusive: %v", e)
	}
	coordinatorLocksClose(files)
	other, e := coordinatorAcquireResources(b, two)
	if e != nil {
		t.Fatal(e)
	}
	coordinatorLocksClose(other)
	// Locks inherited by a process survive closing the controller's copies.
	f, e := coordinatorLock(filepath.Join(root, "inherited"))
	if e != nil {
		t.Fatal(e)
	}
	cmd := exec.Command("/bin/sh", "-c", "sleep 0.2")
	cmd.ExtraFiles = []*os.File{f}
	if e := cmd.Start(); e != nil {
		t.Fatal(e)
	}
	f.Close()
	if probe, e := coordinatorLock(filepath.Join(root, "inherited")); !coordinatorLockBusy(e) {
		if probe != nil {
			probe.Close()
		}
		t.Fatalf("inherited lock released early: %v", e)
	}
	if e := cmd.Wait(); e != nil {
		t.Fatal(e)
	}
	probe, e := coordinatorLock(filepath.Join(root, "inherited"))
	if e != nil {
		t.Fatal(e)
	}
	probe.Close()
}
func TestCoordinatorRejectsSymlinkManagedState(t *testing.T) {
	root := coordinatorTestRoot(t)
	outside := coordinatorTestRoot(t)
	if e := os.Symlink(outside, filepath.Join(root, "link")); e != nil {
		t.Fatal(e)
	}
	if e := coordinatorWriteJSON(filepath.Join(root, "link", "escaped.json"), map[string]int{"x": 1}); e == nil {
		t.Fatal("symlink escape accepted")
	}
}

func TestCoordinatorControllerProcess(t *testing.T) {
	if os.Getenv("TUSKER_COORDINATOR_TEST_HELPER") != "1" {
		return
	}
	coordinatorUseTestHelper(t)
	var args Args
	if err := coordinatorReadJSON(os.Getenv("TUSKER_COORDINATOR_CONTROLLER_ARGS"), &args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := coordinatorBatchCmd(args, "run"); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(0)
}
func TestCoordinatorControllerCrashResumesSurvivingWorker(t *testing.T) {
	vault := coordinatorTestVault(t)
	coordinatorUseTestHelper(t)
	root := filepath.Dir(v7RepoRoot(vault))
	marker := filepath.Join(root, "starts")
	script := filepath.Join(root, "fake-codex")
	text := `#!/bin/sh
case "$1" in --version) echo 'synthetic 1'; exit 0;; login) exit 0;; esac
cat >/dev/null
echo start >> ` + shellSingleQuote(marker) + `
sleep 1.5
id="$TUSKER_TASK_ID"; dir="$TUSKER_BATCH_RUN_DIR"
echo complete > "$id.txt"
git add "$id.txt" && git commit -m "Implement restart fixture" >/dev/null || exit 1
sha=$(git rev-parse HEAD)
cat > "$dir/REPORT.json" <<EOF
{"schema":"tusker.batch-report/v1","task":"$id","verdict":"pass","commits":["$sha"],"gates":[{"acceptance":"A1","command":"test -f $id.txt","result":"pass","host":"local"}],"questions":[],"remaining":[],"boxes":[]}
EOF
echo passed > "$dir/REPORT.md"
echo '{"type":"turn.completed"}'
`
	if e := os.WriteFile(script, []byte(text), 0700); e != nil {
		t.Fatal(e)
	}
	coordinatorTestProfile(t, script)
	coordinatorTestTask(t, vault, "APP-T-0001", "Restart worker", "cargo:restart=1", "")
	if e := coordinatorBatchCreateCmd(Args{"vault": vault, "name": "restart", "tasks": "APP-T-0001"}); e != nil {
		t.Fatal(e)
	}
	file := filepath.Join(root, "controller-args.json")
	if e := coordinatorWriteJSON(file, Args{"vault": vault, "id": "restart"}); e != nil {
		t.Fatal(e)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestCoordinatorControllerProcess$")
	cmd.Env = append(os.Environ(), "TUSKER_COORDINATOR_TEST_HELPER=1", "TUSKER_COORDINATOR_CONTROLLER_ARGS="+file)
	if e := cmd.Start(); e != nil {
		t.Fatal(e)
	}
	killed := false
	t.Cleanup(func() {
		if !killed {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	})
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, e := os.Stat(marker); e == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("worker did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if e := cmd.Process.Kill(); e != nil {
		t.Fatal(e)
	}
	_ = cmd.Wait()
	killed = true
	b, e := coordinatorLoadBatch(vault, "restart")
	if e != nil {
		t.Fatal(e)
	}
	if files, e := coordinatorAcquireResources(b, &coordinatorTask{ID: "competitor", Resources: map[string]int{"cargo:restart": 1}}); !coordinatorLockBusy(e) {
		coordinatorLocksClose(files)
		t.Fatalf("controller crash released worker resources: %v", e)
	}
	if e := coordinatorBatchCmd(Args{"vault": vault, "id": "restart"}, "run"); e != nil {
		t.Fatal(e)
	}
	b, e = coordinatorLoadBatch(vault, "restart")
	if e != nil {
		t.Fatal(e)
	}
	if b.Tasks[0].Attempt != 1 || b.Tasks[0].State != "review" || b.Tasks[0].Policy["crash"] != 0 {
		t.Fatalf("surviving worker relaunched: %+v", b.Tasks[0])
	}
	starts, _ := os.ReadFile(marker)
	if string(starts) != "start\n" {
		t.Fatalf("worker ran more than once: %q", starts)
	}
}
func TestCoordinatorBoxLedgerAndFollowupDedupe(t *testing.T) {
	vault := coordinatorTestVault(t)
	coordinatorTestProfile(t, "codex")
	coordinatorTestTask(t, vault, "APP-T-0001", "One", "", "")
	b := &coordinatorBatch{Vault: vault, ID: "ledger", Epic: "APP"}
	task := &coordinatorTask{ID: "APP-T-0001", Owned: []string{"APP-T-0001.txt"}}
	items := []coordinatorRemaining{{Title: " Follow-up ", Why: "Remaining concern", Evidence: "file.go:1"}}
	if e := coordinatorHarvest(b, task, items); e != nil {
		t.Fatal(e)
	}
	if e := coordinatorHarvest(b, task, items); e != nil {
		t.Fatal(e)
	}
	if len(task.Followups) != 1 {
		t.Fatalf("duplicate follow-ups: %v", task.Followups)
	}
	before, err := resolveV7Note(vault, task.Followups[0], "task")
	if err != nil {
		t.Fatal(err)
	}
	contract := coordinatorContract(before)
	items[0].Evidence = "file.go:2"
	if err := coordinatorHarvest(b, task, items); err != nil {
		t.Fatal(err)
	}
	after, err := resolveV7Note(vault, task.Followups[0], "task")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(after.Data["followup_reports"])
	var reports []map[string]string
	_ = json.Unmarshal(raw, &reports)
	if len(reports) != 2 || coordinatorContract(after) != contract {
		t.Fatalf("dedupe lost evidence or changed task contract: %s", raw)
	}

	now := time.Now().UTC()
	created := now.Add(-time.Hour)
	expiry := now.Add(-time.Minute)
	box := coordinatorBox{ID: "test-box", Type: "synthetic", Created: created, Expires: &expiry, Rate: 0.5}
	if e := coordinatorRecordBoxes(b, []coordinatorBox{box}); e != nil {
		t.Fatal(e)
	}
	reg, e := coordinatorLoadRegistry(vault)
	if e != nil || len(reg.Boxes) != 1 {
		t.Fatalf("ledger: %+v %v", reg, e)
	}
	if cost := coordinatorBoxCost(reg.Boxes[0], now); cost != 0.5 {
		t.Fatalf("cost=%v", cost)
	}
	digest := coordinatorBatchDigest(&coordinatorBatch{Vault: vault, ID: "ledger"})
	if !strings.Contains(digest, "EXPIRED BOXES: test-box") {
		t.Fatalf("missing expiry warning: %s", digest)
	}
	if e := coordinatorLedgerCmd(Args{"vault": vault, "_pos0": "box", "_pos1": "delete", "id": "test-box"}); e != nil {
		t.Fatal(e)
	}
	if e := coordinatorRecordBoxes(b, []coordinatorBox{box}); e != nil {
		t.Fatal(e)
	}
	reg, _ = coordinatorLoadRegistry(vault)
	if reg.Boxes[0].Deleted == nil {
		t.Fatal("stale report reopened deleted box")
	}
}

func TestCoordinatorReservationsAreAtomicAndReplaceOnlyInactiveBatches(t *testing.T) {
	vault := coordinatorTestVault(t)
	coordinatorTestTask(t, vault, "APP-T-0001", "First", "cargo:mac=1", "")
	coordinatorTestTask(t, vault, "APP-T-0002", "Second", "", "")
	mustWave(t, Args{"vault": vault, "name": "original", "tasks": "APP-T-0001", "quiet": "true"}, coordinatorBatchCreateCmd)
	original, err := coordinatorLoadBatch(vault, "original")
	if err != nil {
		t.Fatal(err)
	}
	lock, err := coordinatorAcquireResources(original, original.Tasks[0])
	if err != nil {
		t.Fatal(err)
	}
	err = coordinatorBatchCreateCmd(Args{"vault": vault, "name": "replacement", "tasks": "APP-T-0002,APP-T-0001"})
	coordinatorLocksClose(lock)
	if err == nil {
		t.Fatal("replaced a task with a surviving worker lock")
	}
	note, err := resolveV7Note(vault, "APP-T-0002", "task")
	if err != nil {
		t.Fatal(err)
	}
	if stringField(note.Data, "batch_id") != "" {
		t.Fatal("failed reservation partially reserved a member")
	}
	if _, err := coordinatorLoadBatch(vault, "replacement"); !os.IsNotExist(err) {
		t.Fatalf("failed reservation created batch: %v", err)
	}
	mustWave(t, Args{"vault": vault, "name": "replacement", "tasks": "APP-T-0001,APP-T-0002", "quiet": "true"}, coordinatorBatchCreateCmd)
	original, err = coordinatorLoadBatch(vault, "original")
	if err != nil {
		t.Fatal(err)
	}
	if original.Tasks[0].State != "discarded" || !strings.Contains(original.Tasks[0].Blocker, "replacement") {
		t.Fatal("prior history did not record replacement")
	}
	replacement, err := coordinatorLoadBatch(vault, "replacement")
	if err != nil {
		t.Fatal(err)
	}
	if replacement.SourceCommit == "" {
		t.Fatal("source commit was not frozen")
	}
}

func TestCoordinatorWorkerResolvesCanonicalVault(t *testing.T) {
	vault := coordinatorTestVault(t)
	coordinatorTestTask(t, vault, "APP-T-0001", "First", "", "")
	mustWave(t, Args{"vault": vault, "name": "binding", "tasks": "APP-T-0001", "quiet": "true"}, coordinatorBatchCreateCmd)
	batch, err := coordinatorLoadBatch(vault, "binding")
	if err != nil {
		t.Fatal(err)
	}
	workspace := filepath.Join(filepath.Dir(v7RepoRoot(vault)), "worker")
	if err := os.Mkdir(workspace, 0700); err != nil {
		t.Fatal(err)
	}
	batch.Tasks[0].Workspace = workspace
	if err := coordinatorSaveBatch(batch); err != nil {
		t.Fatal(err)
	}
	old := mustGetwd()
	if err := os.Chdir(workspace); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })
	t.Setenv("TUSKER_BATCH_ID", "binding")
	t.Setenv("TUSKER_TASK_ID", "APP-T-0001")
	t.Setenv("TUSKER_VAULT", vault)
	actual, err := resolveVaultPath(Args{}, false)
	if err != nil || actual != vault {
		t.Fatalf("canonical vault: %q %v", actual, err)
	}
	t.Setenv("TUSKER_TASK_ID", "APP-T-9999")
	if _, err := resolveVaultPath(Args{}, false); err == nil {
		t.Fatal("accepted another worker identity")
	}
}

func TestCoordinatorBudgetStopsWorkerAndKeepsEvidence(t *testing.T) {
	vault := coordinatorTestVault(t)
	coordinatorUseTestHelper(t)
	root := filepath.Dir(v7RepoRoot(vault))
	script := filepath.Join(root, "fake-codex")
	if err := os.WriteFile(script, []byte("#!/bin/sh\ncase \"$1\" in --version) echo codex; exit 0;; login) exit 0;; esac\ncat >/dev/null\necho budget-evidence\nexec /bin/sleep 30\n"), 0700); err != nil {
		t.Fatal(err)
	}
	coordinatorTestProfile(t, script)
	coordinatorTestTask(t, vault, "APP-T-0001", "Budget", "cargo:mac=1", "")
	note, err := resolveV7Note(vault, "APP-T-0001", "task")
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = mutateV7DocumentLocked(note.AbsolutePath, v7FrontmatterOrder["task"], func(d map[string]any, body string) (map[string]any, string, bool, error) {
		d["budget"] = "1s"
		return d, body, true, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	mustWave(t, Args{"vault": vault, "name": "budget", "tasks": "APP-T-0001", "quiet": "true"}, coordinatorBatchCreateCmd)
	if err := coordinatorBatchCmd(Args{"vault": vault, "_pos0": "budget"}, "run"); err == nil {
		t.Fatal("stalled batch reported success")
	}
	batch, err := coordinatorLoadBatch(vault, "budget")
	if err != nil {
		t.Fatal(err)
	}
	task := batch.Tasks[0]
	if task.State != "stalled" || task.Attempt != 1 {
		t.Fatalf("budget result: %+v", task)
	}
	raw, err := os.ReadFile(filepath.Join(coordinatorAttemptDir(batch, task), "run.log"))
	if err != nil || !strings.Contains(string(raw), "budget-evidence") {
		t.Fatalf("lost evidence: %s %v", raw, err)
	}
	locks, err := coordinatorAcquireResources(batch, task)
	if err != nil {
		t.Fatalf("locks leaked: %v", err)
	}
	coordinatorLocksClose(locks)
}

func TestCoordinatorDailyCostsAndTriageAge(t *testing.T) {
	start := time.Date(2026, 10, 7, 23, 0, 0, 0, time.UTC)
	end := start.Add(3 * time.Hour)
	box := coordinatorBox{Created: start, Deleted: &end, Rate: 2}
	costs := coordinatorBoxDailyCosts(box, end)
	if costs["2026-10-07"] != 2 || costs["2026-10-08"] != 4 {
		t.Fatalf("daily estimate: %+v", costs)
	}
	box.EUR = 3
	costs = coordinatorBoxDailyCosts(box, end)
	if costs["2026-10-07"] != 1 || costs["2026-10-08"] != 2 {
		t.Fatalf("daily bill: %+v", costs)
	}
	for _, raw := range []string{"-1d", "0d", "999999999999d"} {
		if _, err := coordinatorAge(raw); err == nil {
			t.Fatalf("accepted age %q", raw)
		}
	}
}

func TestCoordinatorChecksHaveIndependentSupervisedDeadlines(t *testing.T) {
	vault := coordinatorTestVault(t)
	coordinatorUseTestHelper(t)
	task := &coordinatorTask{ID: "APP-T-0001", Workspace: v7RepoRoot(vault), Resources: map[string]int{"cargo:mac": 1}, Attempt: 1}
	batch := &coordinatorBatch{ID: "checks", Vault: vault, Tasks: []*coordinatorTask{task}}
	dir := coordinatorAttemptDir(batch, task)
	locks, err := coordinatorAcquireResources(batch, task)
	if err != nil {
		t.Fatal(err)
	}
	defer coordinatorLocksClose(locks)
	launch := coordinatorLaunch{Schema: "tusker.batch-launch/v1", Vault: vault, Batch: batch.ID, Task: task.ID, Dir: dir, Workspace: task.Workspace, FDCount: len(locks)}
	if err := coordinatorWriteJSON(filepath.Join(dir, "launch.json"), launch); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("/bin/sh", "-c", "echo check-evidence; exec /bin/sleep 30")
	cmd.Dir = task.Workspace
	started := time.Now()
	output, err := coordinatorRunCheck(cmd, dir, locks, 300*time.Millisecond)
	if err == nil || time.Since(started) > 5*time.Second || !strings.Contains(string(output), "check-evidence") {
		t.Fatalf("check supervision: %s %v after %s", output, err, time.Since(started))
	}
	var process coordinatorProcess
	if err := coordinatorReadJSON(filepath.Join(dir, "process.json"), &process); err != nil {
		t.Fatal(err)
	}
	if coordinatorProcessAlive(process) {
		t.Fatal("timed-out check still alive")
	}

	marker := filepath.Join(dir, "check-count")
	success := exec.Command("/bin/sh", "-c", "printf x >> "+shellSingleQuote(marker)+"; echo cached-proof")
	success.Dir = task.Workspace
	for n := 0; n < 2; n++ {
		output, err := coordinatorRunCheck(success, dir, locks, 2*time.Second, "cached")
		if err != nil || !strings.Contains(string(output), "cached-proof") {
			t.Fatalf("durable check result: %s %v", output, err)
		}
	}
	count, err := os.ReadFile(marker)
	if err != nil || string(count) != "x" {
		t.Fatalf("completed check repeated: %q %v", count, err)
	}
}
