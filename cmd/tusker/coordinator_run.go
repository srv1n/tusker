package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	runnercore "tusker/internal/runner"
)

var coordinatorHelperCommand = func(path string) (*exec.Cmd, error) {
	exe, e := os.Executable()
	if e != nil {
		return nil, e
	}
	return exec.Command(exe, "batch execute", "--file", path), nil
}
var coordinatorPoll = 100 * time.Millisecond

func coordinatorBatchCreateCmd(args Args) error {
	vault, e := resolveVaultPath(args, false)
	if e != nil {
		return e
	}
	id := firstNonEmpty(args.String("id"), args.String("name"), "B-"+time.Now().UTC().Format("20060102-150405"))
	path, e := coordinatorBatchPath(vault, id)
	if e != nil {
		return e
	}
	lock, e := coordinatorDocumentLock(path, v7DocumentLockTimeout)
	if e != nil {
		return e
	}
	defer lock.Close()
	if _, e = os.Lstat(path); !os.IsNotExist(e) {
		return fmt.Errorf("batch already exists: %s", id)
	}
	material, e := acquireV7MaterialEpochLock(vault)
	if e != nil {
		return e
	}
	defer material.Close()
	idx, e := loadV7Index(vault)
	if e != nil {
		return e
	}
	ids := splitCSV(args.String("tasks"))
	if len(ids) == 0 {
		return fmt.Errorf("batch create requires --tasks <ids>")
	}
	caps, e := coordinatorResources(splitCSV(args.String("resource-capacity")))
	if e != nil {
		return e
	}
	max := 1
	if s := args.String("max-parallel"); s != "" {
		max, e = strconv.Atoi(s)
		if e != nil || max < 1 {
			return fmt.Errorf("max-parallel must be positive")
		}
	}
	b := &coordinatorBatch{Schema: coordinatorSchema, ID: id, Name: fallback(args.String("name"), id), Epic: args.String("epic"), Vault: vault, Workspace: v7RepoRoot(vault), Created: time.Now().UTC(), MaxParallel: max, Capacities: caps}
	b.SourceCommit, e = coordinatorGit(b.Workspace, "rev-parse", "HEAD")
	if e != nil {
		return e
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if seen[id] {
			return fmt.Errorf("duplicate task %s", id)
		}
		seen[id] = true
		note, ok := idx.Tasks[id]
		if !ok {
			return fmt.Errorf("task not found: %s", id)
		}
		if b.Epic != "" && stringField(note.Data, "epic") != b.Epic {
			return fmt.Errorf("task %s is outside epic %s", id, b.Epic)
		}
		resources, e := coordinatorResources(normalizeList(note.Data["resources"]))
		if e != nil {
			return e
		}
		budget, e := coordinatorDuration(stringField(note.Data, "budget"), 2*time.Hour)
		if e != nil {
			return e
		}
		workspace := stringField(note.Data, "workspace_path")
		if workspace == "" {
			workspace = filepath.Join(coordinatorRunRoot(vault, b.ID), "workspaces", id)
		}
		t := &coordinatorTask{ID: id, Title: stringField(note.Data, "title"), State: "ready", Contract: coordinatorContract(note), Profile: firstNonEmpty(stringField(note.Data, "runner_profile"), stringField(note.Data, "execute_profile")), Resources: resources, Owned: normalizeList(note.Data["owned_paths"]), Workspace: workspace, Budget: budget, Policy: map[string]int{}}
		for _, edge := range v7TaskDependencyEdges(note, idx) {
			t.Dependencies = append(t.Dependencies, edge.ID)
		}
		for key, n := range resources {
			cap := caps[key]
			if cap == 0 {
				cap = 1
			}
			if n > cap {
				return fmt.Errorf("%s needs %d units of %s, capacity %d", id, n, key, cap)
			}
		}
		b.Tasks = append(b.Tasks, t)
	}
	if e = coordinatorDAG(b, idx); e != nil {
		return e
	}
	if e = coordinatorReserveBatch(b, idx); e != nil {
		return e
	}
	if args.Bool("json") {
		emitJSON(b)
	} else {
		fmt.Println(id)
	}
	return nil
}
func coordinatorDAG(b *coordinatorBatch, idx v7Index) error {
	by := map[string]*coordinatorTask{}
	for _, t := range b.Tasks {
		by[t.ID] = t
	}
	visiting := map[string]int{}
	var visit func(string) error
	visit = func(id string) error {
		if visiting[id] == 1 {
			return fmt.Errorf("dependency cycle at %s", id)
		}
		if visiting[id] == 2 {
			return nil
		}
		visiting[id] = 1
		for _, dep := range by[id].Dependencies {
			if by[dep] != nil {
				if e := visit(dep); e != nil {
					return e
				}
			} else if _, ok := idx.Tasks[dep]; !ok {
				return fmt.Errorf("missing dependency %s", dep)
			}
		}
		visiting[id] = 2
		return nil
	}
	for id := range by {
		if e := visit(id); e != nil {
			return e
		}
	}
	return nil
}
func coordinatorBatchCmd(args Args, action string) error {
	vault, e := resolveVaultPath(args, false)
	if e != nil {
		return e
	}
	id := firstNonEmpty(args.String("id"), args.String("_pos0"))
	b, e := coordinatorLoadBatch(vault, id)
	if e != nil {
		return e
	}
	if action == "status" {
		if args.Bool("json") {
			emitJSON(b)
		} else {
			fmt.Print(coordinatorBatchDigest(b))
		}
		return nil
	}
	if action == "stop" {
		request := map[string]any{"drain": args.Bool("drain"), "at": time.Now().UTC()}
		if e = coordinatorWriteJSON(filepath.Join(coordinatorRunRoot(vault, id), "stop.json"), request); e != nil {
			return e
		}
		if args.Bool("json") {
			emitJSON(request)
		} else {
			fmt.Println("Stop requested; running attempts " + map[bool]string{true: "will drain", false: "will stop"}[args.Bool("drain")])
		}
		return nil
	}
	return coordinatorRunBatch(args, b)
}
func coordinatorRunBatch(args Args, b *coordinatorBatch) error {
	root := coordinatorRunRoot(b.Vault, b.ID)
	lock, e := coordinatorLock(filepath.Join(root, "controller.lock"))
	if e != nil {
		return fmt.Errorf("batch already running or unavailable: %w", e)
	}
	defer lock.Close()
	b, e = coordinatorLoadBatch(b.Vault, b.ID)
	if e != nil {
		return e
	}
	if s := args.String("max-parallel"); s != "" {
		n, e := strconv.Atoi(s)
		if e != nil || n < 1 {
			return fmt.Errorf("max-parallel must be positive")
		}
		b.MaxParallel = n
	}
	if b.MaxParallel < 1 {
		return fmt.Errorf("invalid batch parallelism")
	}
	wake := args.String("wake-on")
	if wake != "" && wake != "parked" {
		return fmt.Errorf("wake-on must be parked")
	}
	delay, e := coordinatorDuration(args.String("usage-wait"), time.Hour)
	if e != nil {
		return e
	}
	idx, e := loadV7Index(b.Vault)
	if e != nil {
		return e
	}
	wf, e := loadWorkflow(b.Vault)
	if e != nil {
		return e
	}
	if e = coordinatorDAG(b, idx); e != nil {
		return e
	}
	// Validate every unresolved task/profile before admitting any work.
	for _, t := range b.Tasks {
		if t.State == "review" || t.State == "done" || t.State == "discarded" || t.State == "running" {
			continue
		}
		note, ok := idx.Tasks[t.ID]
		if !ok {
			return fmt.Errorf("task missing: %s", t.ID)
		}
		if t.Profile == "" {
			return fmt.Errorf("%s: explicit runner_profile or execute_profile required", t.ID)
		}
		profile, e := resolveRunnerProfileForNote(Note{Data: map[string]any{"runner_profile": t.Profile}}, wf.Data, runLaneExecute)
		if e != nil {
			return e
		}
		if strings.TrimSpace(profile.Definition.Model) == "" || strings.TrimSpace(profile.Definition.Effort) == "" {
			return fmt.Errorf("%s: profile must pin model and effort", t.ID)
		}
		if profile.Definition.Harness != string(RunnerCodexExec) && profile.Definition.Harness != string(RunnerClaude) && profile.Definition.Harness != string(RunnerMuse) {
			return fmt.Errorf("batch profile must use a CLI harness")
		}
		profileRaw, _ := json.Marshal(profile.Definition)
		fingerprint := coordinatorHash(string(profileRaw))
		if t.ProfileFingerprint != "" && t.ProfileFingerprint != fingerprint {
			return fmt.Errorf("%s: runner profile changed; create a new batch", t.ID)
		}
		t.ProfileFingerprint = fingerprint
		t.Model, t.Effort = profile.Definition.Model, profile.Definition.Effort
		if coordinatorContract(note) != t.Contract {
			return fmt.Errorf("%s: task contract changed; create a new batch", t.ID)
		}
		if _, e := coordinatorPacket(b.Vault, note, false); e != nil {
			return e
		}
		if state := stringField(note.Data, "status"); state == "done" {
			t.State = "done"
		} else if state == "cancelled" {
			t.State = "discarded"
		} else if state == "review" && t.Attempt == 0 {
			return fmt.Errorf("%s is already in review", t.ID)
		}
		if t.State == "parked" {
			t.State = "ready"
			t.Resume = "Continue from the checkpoint with the active rulings; report any still-unanswered question."
		}
	}
	active := false
	for _, t := range b.Tasks {
		if t.State == "ready" || t.State == "running" || t.State == "retry_wait" {
			active = true
		}
	}
	if active {
		b.Generation++
		b.Finished = time.Time{}
		if e = coordinatorSaveBatch(b); e != nil {
			return e
		}
	}
	if e = os.Remove(filepath.Join(root, "stop.json")); e != nil && !os.IsNotExist(e) {
		return e
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	for {
		idx, e = loadV7Index(b.Vault)
		if e != nil {
			return e
		}
		var stop struct {
			Drain bool `json:"drain"`
		}
		stopping := coordinatorReadJSON(filepath.Join(root, "stop.json"), &stop) == nil
		if ctx.Err() != nil {
			stopping = true
			stop.Drain = false
		}
		running := 0
		pending := false
		parked := false
		for _, t := range b.Tasks {
			if t.State != "running" {
				continue
			}
			done, e := coordinatorCollectAttempt(b, t, delay, stopping && !stop.Drain)
			if e != nil {
				return e
			}
			if !done {
				running++
			}
		}
		for _, t := range b.Tasks {
			if t.State == "parked" {
				parked = true
			}
			if t.State == "ready" || t.State == "retry_wait" {
				note, exists := idx.Tasks[t.ID]
				if !exists {
					return fmt.Errorf("batch task disappeared: %s", t.ID)
				}
				status := stringField(note.Data, "status")
				if stringField(note.Data, "batch_id") != b.ID || status == "cancelled" || status == "superseded" {
					t.State = "discarded"
					t.Blocker = "canonical task cancelled or reservation replaced"
					continue
				}
				if status == "done" {
					t.State = "done"
					continue
				}
			}
			if (t.State == "ready" || t.State == "retry_wait") && !t.Started.IsZero() && time.Since(t.Started) >= t.Budget {
				t.State = "stalled"
				t.Blocker = "wall-clock budget exhausted"
				t.Finished = time.Now().UTC()
				if e = coordinatorTaskState(b.Vault, t.ID, t.State, t.Blocker); e != nil {
					return e
				}
				continue
			}
			if t.State == "retry_wait" && time.Now().Before(t.RetryAt) {
				pending = true
				continue
			}
			if t.State == "retry_wait" {
				t.State = "ready"
			}
			if t.State != "ready" {
				continue
			}
			if stopping {
				t.Blocker = "batch stopped before admission"
				continue
			}
			blocker := coordinatorDependencyBlocker(b, t, idx)
			if blocker != "" {
				t.Blocker = blocker
				continue
			}
			if coordinatorHumanGateBlocker(t.ID, idx) != "" {
				t.State = "parked"
				t.Blocker = coordinatorHumanGateBlocker(t.ID, idx)
				if e = coordinatorTaskState(b.Vault, t.ID, "parked", t.Blocker); e != nil {
					return e
				}
				continue
			}
			if running >= b.MaxParallel {
				pending = true
				continue
			}
			started, e := coordinatorStartAttempt(ctx, b, t, wf.Data)
			if e != nil {
				return e
			}
			if started {
				running++
			} else {
				pending = true
			}
		}
		if e = coordinatorSaveBatch(b); e != nil {
			return e
		}
		if stopping && running == 0 || wake == "parked" && parked || running == 0 && !pending {
			break
		}
		select {
		case <-time.After(coordinatorPoll):
		case <-ctx.Done():
		}
	}
	b.Finished = time.Now().UTC()
	if e = coordinatorSaveBatch(b); e != nil {
		return e
	}
	digest := coordinatorBatchDigest(b)
	if e = coordinatorWriteText(filepath.Join(root, "digest.md"), digest); e != nil {
		return e
	}
	if b.NotifiedGeneration != b.Generation {
		// Record before notifying: callbacks are at-most-once, never repeated on a resume of finished work.
		b.NotifiedGeneration = b.Generation
		if e = coordinatorSaveBatch(b); e != nil {
			return e
		}
		if notify := args.String("notify"); notify != "" {
			nctx, ncancel := context.WithTimeout(context.Background(), time.Minute)
			cmd := exec.CommandContext(nctx, "/bin/sh", "-c", notify)
			cmd.Dir = b.Workspace
			cmd.Stdin = strings.NewReader(digest)
			cmd.Env = append(os.Environ(), "TUSKER_BATCH_ID="+b.ID)
			out, ne := cmd.CombinedOutput()
			ncancel()
			if ne != nil {
				fmt.Fprintln(os.Stderr, "Notification failed:", ne, oneLine(string(out)))
			}
		}
	}
	if args.Bool("json") {
		emitJSON(b)
	} else {
		fmt.Print(digest)
	}
	for _, t := range b.Tasks {
		if t.State != "review" && t.State != "done" && t.State != "discarded" {
			return afterResultEmitted(fmt.Errorf("batch has unresolved tasks"))
		}
	}
	return nil
}
func coordinatorWriteText(path, body string) error {
	if e := coordinatorSafePath(path); e != nil {
		return e
	}
	if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return e
	}
	return coordinatorAtomicWrite(path, body)
}
func coordinatorDependencyBlocker(b *coordinatorBatch, t *coordinatorTask, idx v7Index) string {
	for _, id := range t.Dependencies {
		satisfied := false
		for _, dep := range b.Tasks {
			if dep.ID == id {
				satisfied = dep.State == "done" || dep.State == "review" && dep.Audit.Passed
				break
			}
		}
		if !satisfied {
			if dep, ok := idx.Tasks[id]; ok && stringField(dep.Data, "status") == "done" {
				satisfied = true
			}
		}
		if !satisfied {
			return "waiting on " + id
		}
	}
	return ""
}
func coordinatorHumanGateBlocker(id string, idx v7Index) string {
	if task, ok := idx.Tasks[id]; ok && (stringField(task.Data, "agent_action") == "stop_until_human_response" || stringField(task.Data, "readiness") == "waiting_on_human") {
		return "explicit human gate"
	}
	for gid, g := range idx.Gates {
		if (containsString(normalizeList(g.Data["blocks"]), id) || containsString(normalizeList(idx.Tasks[id].Data["gates"]), gid)) && stringField(g.Data, "status") != "satisfied" && stringField(g.Data, "status") != "waived" && stringField(g.Data, "status") != "obsolete" {
			return "human gate " + gid
		}
	}
	return ""
}
func coordinatorAcquireResources(b *coordinatorBatch, t *coordinatorTask) ([]*os.File, error) {
	keys := []string{}
	for key := range t.Resources {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	taskKey := coordinatorHash(b.Vault + ":" + t.ID)
	first, e := coordinatorLock(filepath.Join(DefaultStateRoot(), "coordinator-locks", "task-"+taskKey))
	if e != nil {
		return nil, e
	}
	files := []*os.File{first}
	for _, key := range keys {
		cap := b.Capacities[key]
		if cap == 0 {
			cap = 1
		}
		need := t.Resources[key]
		// Counting resource capacity is fixed for the machine; conflicting batch declarations refuse admission.
		config := filepath.Join(DefaultStateRoot(), "coordinator-locks", coordinatorHash(key)+".capacity")
		l, e := coordinatorDocumentLock(config, v7DocumentLockTimeout)
		if e != nil {
			coordinatorLocksClose(files)
			return nil, e
		}
		raw, readErr := os.ReadFile(config)
		if readErr == nil && strings.TrimSpace(string(raw)) != strconv.Itoa(cap) {
			l.Close()
			coordinatorLocksClose(files)
			return nil, fmt.Errorf("resource %s capacity conflicts with machine registry", key)
		}
		if readErr != nil && !os.IsNotExist(readErr) {
			l.Close()
			coordinatorLocksClose(files)
			return nil, readErr
		}
		if os.IsNotExist(readErr) {
			e = coordinatorWriteText(config, strconv.Itoa(cap))
		}
		l.Close()
		if e != nil {
			coordinatorLocksClose(files)
			return nil, e
		}
		got := 0
		for slot := 0; slot < cap && got < need; slot++ {
			f, e := coordinatorLock(filepath.Join(DefaultStateRoot(), "coordinator-locks", coordinatorHash(key)+"-"+strconv.Itoa(slot)))
			if e == nil {
				files = append(files, f)
				got++
			} else if !coordinatorLockBusy(e) {
				coordinatorLocksClose(files)
				return nil, e
			}
		}
		if got < need {
			coordinatorLocksClose(files)
			return nil, syscall.EWOULDBLOCK
		}
	}
	return files, nil
}
func coordinatorSetupWorkspace(b *coordinatorBatch, t *coordinatorTask) error {
	if _, e := os.Stat(t.Workspace); os.IsNotExist(e) {
		if e = coordinatorSafePath(t.Workspace); e != nil {
			return e
		}
		if e = os.MkdirAll(filepath.Dir(t.Workspace), 0700); e != nil {
			return e
		}
		branch := "srv1nc/batch-" + b.ID + "-" + t.ID
		if _, e = coordinatorGit(b.Workspace, "worktree", "add", "-b", branch, t.Workspace, b.SourceCommit); e != nil {
			return e
		}
	}
	if t.Base != "" {
		return nil
	}
	if dirt, err := coordinatorGit(t.Workspace, "status", "--porcelain"); err != nil {
		return err
	} else if dirt != "" {
		return fmt.Errorf("batch workspace must start clean: %s", t.Workspace)
	}
	// Dependencies are integrated in the task workspace before the audit baseline is captured.
	for _, depID := range t.Dependencies {
		for _, dep := range b.Tasks {
			if dep.ID == depID && len(dep.Commits) > 0 {
				tip, err := coordinatorGit(dep.Workspace, "rev-parse", "HEAD")
				if err != nil {
					return err
				}
				if _, e := coordinatorGit(t.Workspace, "merge", "--no-edit", tip); e != nil {
					return e
				}
			}
		}
	}
	authorName, err := coordinatorGit(t.Workspace, "config", "user.name")
	if err != nil {
		return err
	}
	authorEmail, err := coordinatorGit(t.Workspace, "config", "user.email")
	if err != nil {
		return err
	}
	t.Author = authorName + " <" + authorEmail + ">"
	var e error
	t.RemoteRefs, e = coordinatorGit(t.Workspace, "for-each-ref", "--format=%(refname) %(objectname)", "refs/remotes")
	if e != nil {
		return e
	}
	t.Base, e = coordinatorGit(t.Workspace, "rev-parse", "HEAD")
	if e != nil {
		return e
	}
	t.Branch, e = coordinatorGit(t.Workspace, "symbolic-ref", "--short", "HEAD")
	return e
}
func coordinatorStartAttempt(ctx context.Context, b *coordinatorBatch, t *coordinatorTask, wf Workflow) (bool, error) {
	files, e := coordinatorAcquireResources(b, t)
	if coordinatorLockBusy(e) {
		if t.Blocker != "waiting on resources" {
			t.Blocker = "waiting on resources"
			_ = coordinatorEvent(b, t, "resource_wait", t.Resources)
		}
		return false, nil
	}
	if e != nil {
		return false, e
	}
	defer coordinatorLocksClose(files)
	if e = coordinatorSetupWorkspace(b, t); e != nil {
		t.State = "parked"
		t.Blocker = e.Error()
		return false, coordinatorTaskState(b.Vault, t.ID, "parked", t.Blocker)
	}
	note, e := resolveV7Note(b.Vault, t.ID, "task")
	if e != nil {
		return false, e
	}
	if coordinatorContract(note) != t.Contract {
		t.State = "parked"
		t.Blocker = "task contract changed; create a new batch"
		return false, coordinatorTaskState(b.Vault, t.ID, "parked", t.Blocker)
	}
	prompt, e := coordinatorPacket(b.Vault, note, false)
	if e != nil {
		return false, e
	}
	fresh, e := loadWorkflow(b.Vault)
	if e != nil {
		return false, e
	}
	wf = fresh.Data
	profile, e := resolveRunnerProfileForNote(Note{Data: map[string]any{"runner_profile": t.Profile}}, wf, runLaneExecute)
	if e != nil {
		return false, e
	}
	profileRaw, _ := json.Marshal(profile.Definition)
	if coordinatorHash(string(profileRaw)) != t.ProfileFingerprint {
		t.State = "parked"
		t.Blocker = "runner profile changed; create a new batch"
		return false, coordinatorTaskState(b.Vault, t.ID, "parked", t.Blocker)
	}
	definition, model, effort, e := conformanceHarnessDefinition(b.Vault, t.Profile)
	if e != nil {
		return false, e
	}
	if definition.Transport != runnercore.TransportCLI {
		return false, fmt.Errorf("coordinator batches require a CLI runner profile")
	}
	if model != profile.Definition.Model || effort != profile.Definition.Effort {
		return false, fmt.Errorf("profile changed during launch compilation")
	}
	if t.Started.IsZero() {
		t.Started = time.Now().UTC()
	}
	t.Blocker = ""
	t.State = "running"
	remaining := t.Budget - time.Since(t.Started)
	if remaining <= 0 {
		t.State = "stalled"
		t.Blocker = "wall-clock budget exhausted"
		return false, coordinatorTaskState(b.Vault, t.ID, t.State, t.Blocker)
	}
	t.Attempt++
	dir := coordinatorAttemptDir(b, t)
	launch := coordinatorLaunch{Schema: "tusker.batch-launch/v1", Vault: b.Vault, Batch: b.ID, Task: t.ID, Dir: dir, Workspace: t.Workspace, Prompt: prompt + "\n\n" + t.Resume, Profile: profile, Definition: definition, Budget: remaining, FDCount: len(files)}
	path := filepath.Join(dir, "launch.json")
	if e = coordinatorWriteJSON(path, launch); e != nil {
		return false, e
	}
	if e = coordinatorSaveBatch(b); e != nil {
		return false, e
	}
	if e = coordinatorTaskState(b.Vault, t.ID, "running", ""); e != nil {
		return false, e
	}
	cmd, e := coordinatorHelperCommand(path)
	if e != nil {
		return false, e
	}
	helperLog, err := os.OpenFile(filepath.Join(dir, "supervisor.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return false, err
	}
	defer helperLog.Close()
	cmd.Stdout, cmd.Stderr = helperLog, helperLog
	cmd.ExtraFiles = files
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if e = cmd.Start(); e != nil {
		return false, e
	}
	// Child owns durable receipt/log writes. Reap only; controller death does not stop it.
	go func() { _ = cmd.Wait() }()
	return true, coordinatorEvent(b, t, "attempt_started", map[string]any{"attempt": t.Attempt, "profile": t.Profile, "model": t.Model, "effort": t.Effort})
}
func coordinatorEvent(b *coordinatorBatch, t *coordinatorTask, kind string, payload any) error {
	return NewEventLog(filepath.Join(coordinatorRunRoot(b.Vault, b.ID), t.ID, "events.jsonl")).Append(kind, b.ID+":"+t.ID, RunnerName("batch"), map[string]any{"payload": payload})
}
func coordinatorBatchExecuteCmd(args Args) error {
	var launch coordinatorLaunch
	if e := coordinatorReadJSON(args.String("file"), &launch); e != nil {
		return e
	}
	if launch.Schema != "tusker.batch-launch/v1" || !coordinatorID.MatchString(launch.Batch) || !coordinatorID.MatchString(launch.Task) || launch.FDCount < 1 || launch.FDCount > 128 {
		return fmt.Errorf("invalid batch launch")
	}
	if !pathWithin(coordinatorRunRoot(launch.Vault, launch.Batch), launch.Dir) {
		return fmt.Errorf("launch directory escapes runtime")
	}
	files := []*os.File{}
	for i := 0; i < launch.FDCount; i++ {
		f := os.NewFile(uintptr(3+i), "resource-lock")
		if _, e := f.Stat(); e != nil {
			return fmt.Errorf("missing inherited resource lock")
		}
		files = append(files, f)
	}
	defer coordinatorLocksClose(files)
	log, e := os.OpenFile(filepath.Join(launch.Dir, "run.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if e != nil {
		return e
	}
	defer log.Close()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	ctx, deadlineCancel := context.WithTimeout(ctx, launch.Budget)
	defer deadlineCancel()
	if len(launch.CheckArgs) > 0 {
		return coordinatorExecuteCheck(ctx, launch, files)
	}
	preset := runnercore.PermissionPreset(launch.Profile.Definition.PermissionPreset)
	if preset == "" {
		preset = runnercore.PresetWorkspaceOffline
	}
	input := runnercore.RunInput{Prompt: launch.Prompt, Workspace: launch.Workspace, Preset: preset, Model: launch.Profile.Definition.Model, Effort: launch.Profile.Definition.Effort, Deadline: launch.Budget, PromptPath: filepath.Join(launch.Dir, "prompt.md")}
	if launch.Profile.Definition.Access != nil {
		private := []string{}
		if report, err := modelLevelsRead(launch.Vault); err == nil {
			private = append(private, report.PrivateFolders...)
		}
		controls := nativeAccessControls(launch.Definition, launch.Profile.Definition.Access)
		for i := range controls {
			if controls[i].Mechanism == runnercore.AccessNativeHook {
				controls[i].Mechanism = runnercore.AccessUnsupported
				controls[i].Coverage = "batch direct CLI has no callback hook"
			}
		}
		resolved, err := resolveAccess(launch.Profile.Definition, private, AgentAccessResolutionContext{Workspace: launch.Workspace, References: accessReferencesForProfile(launch.Profile.Definition.Access, launch.Workspace), Controls: controls})
		if err != nil {
			return err
		}
		if resolved.State != "ready" {
			return fmt.Errorf("batch access policy is not enforceable")
		}
		resolved.Folders = append(resolved.Folders, runnercore.AccessFolder{Path: launch.Dir, Access: "write"})
		input.Access = launch.Profile.Definition.Access
		input.ResolvedAccess = &resolved
		input.Preset = resolved.Effective.Preset
	} else {
		profile := launch.Profile.Definition
		profile.PermissionPreset = string(preset)
		resolved, err := resolveAccess(profile, nil, AgentAccessResolutionContext{Workspace: launch.Workspace})
		if err != nil {
			return err
		}
		resolved.Effective.Workspace = launch.Workspace
		resolved.Folders = append(resolved.Folders, runnercore.AccessFolder{Path: launch.Dir, Access: "write"})
		input.ResolvedAccess = &resolved
	}

	prepared, e := runnercore.Prepare(ctx, launch.Definition, input)
	var receipt runnercore.ExecutionReceipt
	if e == nil {
		if e = coordinatorWriteText(filepath.Join(launch.Dir, "prompt.md"), launch.Prompt); e == nil {
			prepared.Environment = append(prepared.Environment, "TUSKER_BATCH_RUN_DIR="+launch.Dir, "TUSKER_BATCH_ID="+launch.Batch, "TUSKER_TASK_ID="+launch.Task, "TUSKER_STATE_ROOT="+DefaultStateRoot(), "TUSKER_VAULT="+launch.Vault)
			prepared.ExtraFiles = files
			prepared.Output = log
			prepared.ProcessStarted = func(pid int) error {
				started, ok := processStartTime(pid)
				if !ok {
					return fmt.Errorf("cannot record worker process identity")
				}
				p := coordinatorProcess{PID: pid, PGID: processGroupID(pid), Started: started}
				if e := coordinatorWriteText(filepath.Join(launch.Dir, "pid"), strconv.Itoa(pid)); e != nil {
					return e
				}
				return coordinatorWriteJSON(filepath.Join(launch.Dir, "process.json"), p)
			}
			receipt, e = runnercore.Execute(ctx, prepared, nil)
		}
	}
	if e != nil && receipt.Reason == "" {
		receipt.Reason = e.Error()
		receipt.Outcome = runnercore.EventFailed
		receipt.FinishedAt = time.Now().UTC()
	}
	if we := coordinatorWriteJSON(filepath.Join(launch.Dir, "receipt.json"), receipt); we != nil {
		return we
	}
	if we := coordinatorWriteText(filepath.Join(launch.Dir, "last.md"), coordinatorFinalMessage(receipt.Stdout)); we != nil {
		return we
	}
	return e
}
func coordinatorCollectAttempt(b *coordinatorBatch, t *coordinatorTask, delay time.Duration, stop bool) (bool, error) {
	dir := coordinatorAttemptDir(b, t)
	var receipt runnercore.ExecutionReceipt
	receiptErr := coordinatorReadJSON(filepath.Join(dir, "receipt.json"), &receipt)
	taskLock := filepath.Join(DefaultStateRoot(), "coordinator-locks", "task-"+coordinatorHash(b.Vault+":"+t.ID))
	lock, e := coordinatorLock(taskLock)
	if coordinatorLockBusy(e) {
		if stop || time.Since(t.Started) > t.Budget {
			var p coordinatorProcess
			if e := coordinatorReadJSON(filepath.Join(dir, "process.json"), &p); e == nil && coordinatorProcessAlive(p) {
				_ = syscall.Kill(-p.PGID, syscall.SIGTERM)
				if time.Since(t.Started) > t.Budget+3*time.Second || stop {
					_ = syscall.Kill(-p.PGID, syscall.SIGKILL)
				}
			}
		}
		return false, nil
	}
	if e != nil {
		return false, e
	}
	lock.Close()
	resources, e := coordinatorAcquireResources(b, t)
	if coordinatorLockBusy(e) {
		return false, nil
	}
	if e != nil {
		return false, e
	}
	defer coordinatorLocksClose(resources)
	if receiptErr == nil && t.AccountedAttempt < t.Attempt {
		t.AccountedAttempt = t.Attempt
		t.WallSeconds += receipt.FinishedAt.Sub(receipt.StartedAt).Seconds()
		for _, line := range strings.Split(receipt.Stdout, "\n") {
			var event map[string]any
			if json.Unmarshal([]byte(line), &event) != nil {
				continue
			}
			kind, _ := event["type"].(string)
			if kind != "turn.completed" && kind != "result" {
				continue
			}
			usage := extractUsageCounters(json.RawMessage(line))
			t.InputTokens += usage.inputTokens
			t.OutputTokens += usage.outputTokens
		}
	}
	// The helper inherited the task lock before cmd.Start returned. An unlocked task has no live owner.
	if stop || time.Since(t.Started) >= t.Budget || receipt.Reason == "timeout" {
		if r, err := coordinatorReadReport(dir, t.ID); err == nil {
			if e := coordinatorHarvest(b, t, r.Remaining); e != nil {
				return false, e
			}
			if e := coordinatorRecordBoxes(b, r.Boxes); e != nil {
				return false, e
			}
			t.Commits = r.Commits
		}
	}
	if stop {
		t.State = "stalled"
		t.Blocker = "stopped by coordinator"
	} else if time.Since(t.Started) >= t.Budget || receipt.Reason == "timeout" {
		t.State = "stalled"
		t.Blocker = "wall-clock budget exhausted"
	} else {
		if receiptErr != nil && !os.IsNotExist(receiptErr) {
			return false, receiptErr
		}
		r, re := coordinatorReadReport(dir, t.ID)
		if receiptErr != nil {
			receipt.Reason = "worker supervisor lost; no durable receipt"
			if diagnostic, err := os.ReadFile(filepath.Join(dir, "supervisor.log")); err == nil {
				receipt.Reason += " " + oneLine(string(diagnostic))
			}
			receipt.Outcome = runnercore.EventFailed
		}
		if e = coordinatorApplyPolicy(b, t, receipt, r, re, dir, delay, resources...); e != nil {
			return false, e
		}
	}
	if e = coordinatorEvent(b, t, "attempt_exited", map[string]any{"state": t.State, "reason": t.Blocker, "exit": receipt.ExitCode}); e != nil {
		return false, e
	}
	if t.State != "ready" && t.State != "retry_wait" {
		t.Finished = time.Now().UTC()
		if e = coordinatorTaskState(b.Vault, t.ID, t.State, t.Blocker); e != nil {
			return false, e
		}
	}
	return true, nil
}

func coordinatorBatches(vault string) ([]*coordinatorBatch, error) {
	paths, e := filepath.Glob(filepath.Join(vault, "work", "batches", "*.json"))
	if e != nil {
		return nil, e
	}
	var out []*coordinatorBatch
	for _, p := range paths {
		id := strings.TrimSuffix(filepath.Base(p), ".json")
		b, e := coordinatorLoadBatch(vault, id)
		if e != nil {
			return nil, e
		}
		out = append(out, b)
	}
	return out, nil
}

func coordinatorProcessGroup() *syscall.SysProcAttr { return &syscall.SysProcAttr{Setpgid: true} }
func coordinatorCancelCommand(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}

func coordinatorFinalMessage(stdout string) string {
	last := ""
	for _, line := range strings.Split(stdout, "\n") {
		var event map[string]any
		if json.Unmarshal([]byte(line), &event) != nil {
			continue
		}
		if event["type"] == "result" {
			if text, ok := event["result"].(string); ok {
				last = text
			}
		}
		if event["type"] == "item.completed" {
			if item, ok := event["item"].(map[string]any); ok {
				if item["type"] == "agent_message" {
					if text, ok := item["text"].(string); ok {
						last = text
					}
				}
			}
		}
	}
	if last == "" {
		last = stdout
	}
	return last
}

// Reserve the batch and every member together, retaining prior inactive batch history.
func coordinatorReserveBatch(b *coordinatorBatch, idx v7Index) error {
	writes := map[string]string{}
	expected := map[string][]byte{}
	oldBatches := map[string]*coordinatorBatch{}
	var locks []*os.File
	defer func() { coordinatorLocksClose(locks) }()
	oldIDs := []string{}
	for _, task := range b.Tasks {
		note := idx.Tasks[task.ID]
		state := stringField(note.Data, "status")
		if state == "done" || state == "cancelled" || state == "superseded" {
			return fmt.Errorf("cannot reserve closed task %s", task.ID)
		}
		oldID := stringField(note.Data, "batch_id")
		if state == "in_progress" && oldID == "" {
			return fmt.Errorf("task already has ordinary work in progress: %s", task.ID)
		}
		if oldID != "" && oldID != b.ID && !containsString(oldIDs, oldID) {
			oldIDs = append(oldIDs, oldID)
		}
	}
	sort.Strings(oldIDs)
	for _, id := range oldIDs {
		lock, e := coordinatorLock(filepath.Join(coordinatorRunRoot(b.Vault, id), "controller.lock"))
		if e != nil {
			return fmt.Errorf("prior batch %s is running", id)
		}
		locks = append(locks, lock)
		prior, e := coordinatorLoadBatch(b.Vault, id)
		if e != nil {
			return e
		}
		oldBatches[id] = prior
	}
	for _, task := range b.Tasks {
		lock, e := coordinatorLock(filepath.Join(DefaultStateRoot(), "coordinator-locks", "task-"+coordinatorHash(b.Vault+":"+task.ID)))
		if e != nil {
			return fmt.Errorf("task worker is still active: %s", task.ID)
		}
		locks = append(locks, lock)
		note := idx.Tasks[task.ID]
		data := cloneMap(note.Data)
		if oldID := stringField(data, "batch_id"); oldID != "" {
			if prior := oldBatches[oldID]; prior != nil {
				for _, old := range prior.Tasks {
					if old.ID == task.ID {
						old.State = "discarded"
						old.Blocker = "replaced by batch " + b.ID
					}
				}
			}
		}
		data["execution_mode"] = "batch"
		data["batch_id"] = b.ID
		data["batch_state"] = "ready"
		data["state_rev"] = v7StateRev(data, note.Body)
		content, e := serializeDocument(data, note.Body, v7FrontmatterOrder["task"])
		if e != nil {
			return e
		}
		writes[note.AbsolutePath] = content
		raw, e := os.ReadFile(note.AbsolutePath)
		if e != nil {
			return e
		}
		expected[note.AbsolutePath] = raw
	}
	path, e := coordinatorBatchPath(b.Vault, b.ID)
	if e != nil {
		return e
	}
	if e = os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return e
	}
	raw, e := json.MarshalIndent(b, "", "  ")
	if e != nil {
		return e
	}
	writes[path] = string(raw) + "\n"
	for id, prior := range oldBatches {
		oldPath, e := coordinatorBatchPath(b.Vault, id)
		if e != nil {
			return e
		}
		raw, e := json.MarshalIndent(prior, "", "  ")
		if e != nil {
			return e
		}
		writes[oldPath] = string(raw) + "\n"
	}
	for target := range writes {
		if e := coordinatorSafePath(target); e != nil {
			return e
		}
	}
	return commitV7DocumentWritesExpecting(writes, expected)
}

// Checks inherit the same locks as workers. A controller crash cannot admit a
// second Cargo stream while an isolated rerun or audit hook is still alive.
func coordinatorRunCheck(cmd *exec.Cmd, dir string, files []*os.File, budget time.Duration, checkID ...string) ([]byte, error) {
	if len(files) > 0 {
		return coordinatorSuperviseCheck(cmd, dir, files, budget, checkID...)
	}
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	cmd.SysProcAttr = coordinatorProcessGroup()
	cmd.WaitDelay = 2 * time.Second
	cmd.Cancel = func() error { return coordinatorCancelCommand(cmd) }
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	err := cmd.Wait()
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	return output.Bytes(), err
}

// The same detached helper supervises checks, so their deadline survives a
// controller crash just as the worker deadline does.
func coordinatorSuperviseCheck(cmd *exec.Cmd, dir string, files []*os.File, budget time.Duration, checkID ...string) ([]byte, error) {
	var launch coordinatorLaunch
	if err := coordinatorReadJSON(filepath.Join(dir, "launch.json"), &launch); err != nil {
		return nil, err
	}
	launch.CheckArgs = cmd.Args
	launch.CheckID = strconv.FormatInt(time.Now().UnixNano(), 10)
	if len(checkID) > 0 {
		launch.CheckID = checkID[0]
		if output, err, found := coordinatorCheckResult(dir, launch.CheckID); found {
			return output, err
		}
	}
	launch.Workspace = cmd.Dir
	launch.Budget = budget
	launch.FDCount = len(files)
	path := filepath.Join(dir, "check-launch-"+launch.CheckID+".json")
	if err := coordinatorWriteJSON(path, launch); err != nil {
		return nil, err
	}
	helper, err := coordinatorHelperCommand(path)
	if err != nil {
		return nil, err
	}
	helper.ExtraFiles = files
	helper.SysProcAttr = coordinatorProcessGroup()
	diagnostic, err := helper.CombinedOutput()
	if err != nil {
		return diagnostic, err
	}
	output, err, found := coordinatorCheckResult(dir, launch.CheckID)
	if !found {
		return nil, fmt.Errorf("check supervisor produced no durable result")
	}
	return output, err
}
func coordinatorCheckResult(dir, id string) ([]byte, error, bool) {
	var result struct {
		Error string `json:"error"`
	}
	if err := coordinatorReadJSON(filepath.Join(dir, "check-result-"+id+".json"), &result); err != nil {
		if os.IsNotExist(err) {
			return nil, nil, false
		}
		return nil, err, true
	}
	output, err := os.ReadFile(filepath.Join(dir, "check-output-"+id+".log"))
	if err != nil {
		return nil, err, true
	}
	if result.Error != "" {
		return output, fmt.Errorf("check failed: %s", result.Error), true
	}
	return output, nil, true
}

func coordinatorExecuteCheck(ctx context.Context, launch coordinatorLaunch, files []*os.File) error {
	if !coordinatorID.MatchString(launch.CheckID) {
		return fmt.Errorf("invalid check identity")
	}
	output, err := os.OpenFile(filepath.Join(launch.Dir, "check-output-"+launch.CheckID+".log"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer output.Close()
	cmd := exec.CommandContext(ctx, launch.CheckArgs[0], launch.CheckArgs[1:]...)
	cmd.Dir = launch.Workspace
	cmd.Stdout = output
	cmd.Stderr = output
	cmd.ExtraFiles = files
	cmd.SysProcAttr = coordinatorProcessGroup()
	cmd.Cancel = func() error { return coordinatorCancelCommand(cmd) }
	cmd.WaitDelay = 2 * time.Second
	err = cmd.Start()
	if err == nil {
		started, ok := processStartTime(cmd.Process.Pid)
		if !ok {
			err = fmt.Errorf("cannot record check process identity")
		} else {
			err = coordinatorWriteJSON(filepath.Join(launch.Dir, "process.json"), coordinatorProcess{PID: cmd.Process.Pid, PGID: processGroupID(cmd.Process.Pid), Started: started})
		}
		if err != nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			_ = cmd.Wait()
		} else {
			err = cmd.Wait()
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
	}
	message := ""
	if err != nil {
		message = err.Error()
	}
	if err := output.Sync(); err != nil {
		return err
	}
	return coordinatorWriteJSON(filepath.Join(launch.Dir, "check-result-"+launch.CheckID+".json"), map[string]string{"error": message})
}
