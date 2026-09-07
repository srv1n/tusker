package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/oklog/ulid/v2"
)

// ---------------------------------------------------------------------------
// status
// ---------------------------------------------------------------------------

func demoStatusCmd(args Args) (int, error) {
	payload, err := demoStatus(args)
	if err != nil {
		return demoFail(args, demoExitForError(err), err)
	}
	payload["ok"] = true
	if err := demoEmit(args, payload); err != nil {
		return demoExitInternal, err
	}
	return demoExitOK, nil
}

type demoTaskView struct {
	Key        string   `json:"key"`
	TaskID     string   `json:"task_id"`
	Wave       string   `json:"wave"`
	Status     string   `json:"status"`
	Readiness  string   `json:"readiness"`
	Profile    string   `json:"profile"`
	Deps       []string `json:"deps"`
	DepsMet    bool     `json:"deps_met"`
	Artifact   string   `json:"artifact"`
	ArtifactOK bool     `json:"artifact_ok"`
	Lease      string   `json:"lease"`
	Blockers   []string `json:"blockers"`
}

type demoWaveView struct {
	Name          string   `json:"name"`
	WaveID        string   `json:"wave_id"`
	Authorization string   `json:"authorization"`
	Members       []string `json:"members"`
	Done          int      `json:"done"`
}

func demoStatus(args Args) (map[string]any, error) {
	repoRoot, manifest, err := demoResolveRepo(args, true)
	if err != nil {
		return nil, err
	}
	demoEnsureStateRoot(repoRoot)
	vaultPath := demoVaultPath(repoRoot)
	idx, err := loadV7Index(vaultPath)
	if err != nil {
		return nil, err
	}
	exec := demoNewExec()
	taskViews := []demoTaskView{}
	doneByID := map[string]bool{}
	for _, note := range idx.Tasks {
		if strings.ToLower(strings.TrimSpace(stringField(note.Data, "status"))) == "done" {
			doneByID[stringField(note.Data, "id")] = true
		}
	}
	for _, key := range demoSortedTaskKeys(manifest) {
		rec := manifest.Tasks[key]
		view := demoTaskView{Key: key, TaskID: rec.TaskID, Wave: rec.Wave, Deps: rec.Deps, Artifact: rec.Artifact}
		if note, ok := idx.Tasks[rec.TaskID]; ok {
			view.Status = strings.ToLower(strings.TrimSpace(stringField(note.Data, "status")))
			view.Readiness = strings.ToLower(strings.TrimSpace(stringField(note.Data, "readiness")))
			view.Profile = stringField(note.Data, "runner_profile")
			if edge, blocked := v7BlockingDependencyForReadiness(note, idx); blocked {
				view.Blockers = append(view.Blockers, "dependency: "+edge.ID)
			}
			for _, gate := range idx.Gates {
				if !v7GateTouchesTask(gate, rec.TaskID) {
					continue
				}
				if status := strings.ToLower(strings.TrimSpace(stringField(gate.Data, "status"))); status != "satisfied" && status != "waived" && status != "obsolete" {
					view.Blockers = append(view.Blockers, "gate: "+stringField(gate.Data, "id"))
				}
			}
		} else {
			view.Status = "missing"
			view.Blockers = append(view.Blockers, "task document not found")
		}
		view.DepsMet = demoDepsMet(rec, doneByID, manifest)
		if raw, err := os.ReadFile(filepath.Join(repoRoot, rec.Artifact)); err == nil {
			view.ArtifactOK = string(raw) == rec.Content
		}
		iflease := demoActiveLease(exec, repoRoot, rec.TaskID)
		view.Lease = iflease
		taskViews = append(taskViews, view)
	}
	waveViews := []demoWaveView{}
	for _, name := range demoSortedWaveNames(manifest) {
		rec := manifest.Waves[name]
		view := demoWaveView{Name: name, WaveID: rec.WaveID, Members: rec.Members, Authorization: "disarmed"}
		if note, ok := idx.Waves[rec.WaveID]; ok {
			view.Authorization = stringField(note.Data, "authorization")
		}
		for _, member := range rec.Members {
			if doneByID[member] {
				view.Done++
			}
		}
		waveViews = append(waveViews, view)
	}
	runs := len(manifest.Runs)
	lastRun := ""
	if runs > 0 {
		lastRun = manifest.Runs[runs-1].RunID
	}
	return map[string]any{
		"scenario": manifest.Scenario, "repo": repoRoot, "waves": waveViews, "tasks": taskViews,
		"runs": runs, "last_run": lastRun, "profiles": manifest.Profiles,
		"unmet_capabilities": demoUnmetCapabilities(),
		"text":               demoStatusText(manifest, taskViews, waveViews),
	}, nil
}

func demoSortedWaveNames(manifest *demoManifest) []string {
	names := make([]string, 0, len(manifest.Waves))
	for name := range manifest.Waves {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func demoDepsMet(rec demoTaskRecord, doneByID map[string]bool, manifest *demoManifest) bool {
	for _, dep := range rec.Deps {
		id := dep
		if strings.Contains(dep, "/") {
			parts := strings.SplitN(dep, "/", 2)
			wantWave := waveNameForScope(manifest, parts[0])
			for _, task := range manifest.Tasks {
				if task.Wave == wantWave && task.SourceKey == parts[1] {
					id = task.TaskID
				}
			}
		} else {
			for _, task := range manifest.Tasks {
				if task.Wave == rec.Wave && task.SourceKey == dep {
					id = task.TaskID
				}
			}
		}
		if id == "" || !doneByID[id] {
			return false
		}
	}
	return true
}

func waveNameForScope(manifest *demoManifest, scope string) string {
	for name, wave := range manifest.Waves {
		if wave.Scope == scope {
			return name
		}
	}
	return ""
}

func demoActiveLease(exec *demoExec, repoRoot, taskID string) string {
	inspected, err := exec.run(repoRoot, "runs", "inspect", taskID)
	if err != nil {
		return "none"
	}
	run, _ := demoDig(inspected, "run").(map[string]any)
	if run == nil {
		return "none"
	}
	state, _ := run["lease_state"].(string)
	owner, _ := run["lease_owner"].(string)
	if state == "claimed" || state == "running" {
		if owner != "" {
			return state + " by " + owner
		}
		return state
	}
	return "none"
}

// ---------------------------------------------------------------------------
// run
// ---------------------------------------------------------------------------

const demoRunCapacity = 3

type demoRunOptions struct {
	Waves          []string
	Fast           bool
	FailOnce       string
	RejectOnce     string
	RequireHarness string
}

type demoScheduler struct {
	repoRoot string
	vault    string
	exec     *demoExec
	manifest *demoManifest
	options  demoRunOptions
	record   demoRunRecord
	failed   map[string]bool
	injected map[string]bool
	mu       sync.Mutex
}

func demoRunCmd(args Args) (int, error) {
	payload, code, err := demoRun(args)
	if err != nil {
		if code == 0 {
			code = demoExitForError(err)
		}
		// Real-lane timeouts and interruptions still carry evidence (run
		// ID, effective profiles, agent instructions): keep it in the
		// failure envelope instead of reducing it to a bare error.
		if payload != nil {
			payload["ok"] = false
			payload["error"] = errorToIssue(err)
			if emitErr := demoEmit(args, payload); emitErr != nil {
				return demoExitInternal, emitErr
			}
			return code, nil
		}
		return demoFail(args, code, err)
	}
	payload["ok"] = true
	if err := demoEmit(args, payload); err != nil {
		return demoExitInternal, err
	}
	return code, nil
}

func demoRun(args Args) (map[string]any, int, error) {
	repoRoot, manifest, err := demoResolveRepo(args, true)
	if err != nil {
		return nil, 0, err
	}
	stateRoot := demoEnsureStateRoot(repoRoot)
	exec := demoNewExec()
	exec.Env = append(exec.Env, "TUSKER_STATE_ROOT="+stateRoot)
	vaultPath := demoVaultPath(repoRoot)

	waves, err := demoParseWaves(args, manifest)
	if err != nil {
		return nil, 0, err
	}
	mode := strings.ToLower(strings.TrimSpace(args.String("mode")))
	if mode == "" {
		mode = demoRunModeOffline
	}
	if mode != demoRunModeOffline && mode != demoRunModeReal {
		return nil, 0, tuskerError(errorInvalidArg, "unknown --mode: "+args.String("mode"), withHint("use --mode offline (deterministic timer) or --mode real (configured harness)"))
	}
	requireProfile := strings.TrimSpace(args.String("profile"))
	options := demoRunOptions{
		Waves: waves, Fast: args.Bool("fast"),
		FailOnce:       strings.ToLower(strings.TrimSpace(args.String("fail-once"))),
		RejectOnce:     strings.ToLower(strings.TrimSpace(args.String("reject-once"))),
		RequireHarness: strings.TrimSpace(args.String("require-harness")),
	}
	if mode == demoRunModeReal {
		// The real lane never executes, injects faults, or hurries: the
		// configured runtime owns execution timing and failure semantics.
		if options.Fast {
			return nil, 0, tuskerError(errorInvalidArg, "--fast is an offline timer option; real-harness timing belongs to the configured runtime")
		}
		if options.FailOnce != "" || options.RejectOnce != "" {
			return nil, 0, tuskerError(errorInvalidArg, "--fail-once/--reject-once are offline timer variants; real-harness failure and retry use supported lifecycle semantics")
		}
		if requireProfile == "" && options.RequireHarness == "" {
			return nil, 0, tuskerError(errorMissingArg, "--mode real requires --require-harness and/or --profile so the driver can refuse unavailable prerequisites without substitution")
		}
		profiles, err := demoCheckRealPreconditions(exec, repoRoot, vaultPath, manifest, demoSelectedKeys(manifest, waves), options.RequireHarness, requireProfile)
		if err != nil {
			return nil, 0, err
		}
		ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		defer stop()
		return demoRunReal(ctx, args, repoRoot, manifest, exec, vaultPath, waves, profiles)
	}
	if requireProfile != "" {
		return nil, 0, tuskerError(errorInvalidArg, "--profile is a real-harness option; the offline lane always uses the demo-timer profiles")
	}
	if strings.TrimSpace(args.String("timeout")) != "" {
		return nil, 0, tuskerError(errorInvalidArg, "--timeout is a real-harness option; the offline lane runs to completion")
	}
	if options.FailOnce != "" {
		if _, ok := manifest.Tasks[options.FailOnce]; !ok {
			return nil, 0, tuskerError(errorInvalidArg, "unknown --fail-once task: "+options.FailOnce)
		}
	}
	if options.RejectOnce != "" {
		if _, ok := manifest.Tasks[options.RejectOnce]; !ok {
			return nil, 0, tuskerError(errorInvalidArg, "unknown --reject-once task: "+options.RejectOnce)
		}
	}
	if options.RequireHarness != "" {
		if err := demoRequireHarness(exec, repoRoot, options.RequireHarness); err != nil {
			return nil, 0, err
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	scheduler := &demoScheduler{
		repoRoot: repoRoot, vault: vaultPath, exec: exec, manifest: manifest, options: options,
		failed: map[string]bool{}, injected: map[string]bool{},
		record: demoRunRecord{
			RunID:     "R-" + strings.ToLower(ulid.Make().String()),
			StartedAt: time.Now().UTC().Format(time.RFC3339),
			Waves:     waves, Executor: "demo-timer", Fast: options.Fast,
			Results: map[string]demoWaveResult{},
		},
	}
	for _, name := range waves {
		// Explicit per-wave authorization at the demo level. Native wave arm
		// stays untouched: it requires a resident daemon, which the demo
		// never starts. Each wave result is recorded separately.
		scheduler.record.Results[name] = demoWaveResult{Wave: name, Authorized: true}
		scheduler.note(fmt.Sprintf("wave %s authorized by demo run %s (native arm requires a resident daemon; not started)", name, scheduler.record.RunID))
	}

	selected := scheduler.selectedKeys()
	sem := make(chan struct{}, demoRunCapacity)
	var wg sync.WaitGroup
	for _, key := range selected {
		wg.Add(1)
		go func(key string) {
			defer wg.Done()
			// Dependency parking never holds an execution slot.
			if !scheduler.waitForDeps(ctx, key) {
				if ctx.Err() != nil {
					scheduler.recordOutcome(key, "interrupted", "", "", "")
				} else {
					scheduler.recordOutcome(key, "blocked", "", "", "")
					rec := scheduler.manifest.Tasks[key]
					scheduler.note(fmt.Sprintf("task %s (%s) blocked: dependencies not satisfied", key, rec.TaskID))
				}
				return
			}
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				scheduler.recordOutcome(key, "interrupted", "", "", "")
				return
			}
			scheduler.runTask(ctx, key)
		}(key)
	}
	wg.Wait()

	scheduler.record.FinishedAt = time.Now().UTC().Format(time.RFC3339)
	manifest.Runs = append(manifest.Runs, scheduler.record)
	if err := demoSaveManifest(repoRoot, manifest); err != nil {
		return nil, 0, err
	}
	payload := map[string]any{
		"scenario": manifest.Scenario, "repo": repoRoot, "run": scheduler.record.RunID,
		"executor": "demo-timer", "fast": options.Fast, "waves": waves,
		"results": scheduler.record.Results, "notes": scheduler.record.Notes,
		"text": demoRunText(scheduler.record),
	}
	code := demoExitOK
	for _, name := range waves {
		result := scheduler.record.Results[name]
		if len(result.Failed) > 0 || len(result.Blocked) > 0 || len(result.Interrupted) > 0 || result.Error != "" {
			code = demoExitAssertion
		}
	}
	return payload, code, nil
}

func demoParseWaves(args Args, manifest *demoManifest) ([]string, error) {
	raw := strings.TrimSpace(args.String("waves"))
	if raw == "" {
		return nil, tuskerError(errorMissingArg, "Usage: tusker demo run --repo <path> --waves alpha,beta")
	}
	byID := map[string]string{}
	for name, wave := range manifest.Waves {
		byID[strings.ToUpper(wave.WaveID)] = name
	}
	var out []string
	for _, part := range strings.Split(raw, ",") {
		name := strings.ToLower(strings.TrimSpace(part))
		if name == "" {
			continue
		}
		if _, ok := manifest.Waves[name]; ok {
			out = append(out, name)
			continue
		}
		if resolved, ok := byID[strings.ToUpper(part)]; ok {
			out = append(out, resolved)
			continue
		}
		return nil, tuskerError(errorInvalidArg, "unknown demo wave: "+part)
	}
	if len(out) == 0 {
		return nil, tuskerError(errorMissingArg, "no demo waves selected")
	}
	return out, nil
}

func demoRequireHarness(exec *demoExec, repoRoot, name string) error {
	catalog, err := exec.run(repoRoot, "runner", "catalog")
	if err != nil {
		return err
	}
	harnesses, _ := catalog["harnesses"].([]any)
	for _, entry := range harnesses {
		if record, ok := entry.(map[string]any); ok {
			if id, _ := record["harness"].(string); id == name {
				return nil
			}
		}
	}
	return tuskerError(demoCodePrecondition, "required runner harness is unavailable: "+name,
		withHint("install the harness or rerun without --require-harness; the demo never substitutes another harness silently"))
}

func (s *demoScheduler) note(text string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.record.Notes = append(s.record.Notes, text)
}

func (s *demoScheduler) selectedKeys() []string {
	in := map[string]bool{}
	for _, name := range s.options.Waves {
		in[name] = true
	}
	var keys []string
	for key, task := range s.manifest.Tasks {
		if in[task.Wave] {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys
}

func (s *demoScheduler) depTaskID(wave, key string) string {
	for _, task := range s.manifest.Tasks {
		if task.Wave == wave && task.SourceKey == key {
			return task.TaskID
		}
	}
	return ""
}

func (s *demoScheduler) depID(rec demoTaskRecord, dep string) string {
	if strings.Contains(dep, "/") {
		parts := strings.SplitN(dep, "/", 2)
		for _, task := range s.manifest.Tasks {
			if wave, ok := s.manifest.Waves[task.Wave]; ok && wave.Scope == parts[0] && task.SourceKey == parts[1] {
				return task.TaskID
			}
		}
		return ""
	}
	return s.depTaskID(rec.Wave, dep)
}

// waitDeps blocks until every dependency is done. A failed or blocked
// dependency parks this task as blocked: joins never run on partial input.
func (s *demoScheduler) waitDeps(ctx context.Context, rec demoTaskRecord) bool {
	for {
		ready := true
		for _, dep := range rec.Deps {
			id := s.depID(rec, dep)
			status := s.taskStatus(id)
			switch status {
			case "done":
			case "failed", "blocked":
				return false
			default:
				ready = false
			}
		}
		if ready {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(200 * time.Millisecond):
		}
	}
}

func (s *demoScheduler) taskStatus(taskID string) string {
	if taskID == "" {
		return "missing"
	}
	idx, err := loadV7Index(s.vault)
	if err != nil {
		return "unknown"
	}
	note, ok := idx.Tasks[taskID]
	if !ok {
		return "missing"
	}
	return strings.ToLower(strings.TrimSpace(stringField(note.Data, "status")))
}

func (s *demoScheduler) finishKey(key, outcome string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if outcome == "failed" || outcome == "blocked" {
		s.failed[key] = true
	}
}

func (s *demoScheduler) keyFailed(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.failed[key]
}

func (s *demoScheduler) depKey(rec demoTaskRecord, dep string) string {
	if strings.Contains(dep, "/") {
		parts := strings.SplitN(dep, "/", 2)
		for key, task := range s.manifest.Tasks {
			if wave, ok := s.manifest.Waves[task.Wave]; ok && wave.Scope == parts[0] && task.SourceKey == parts[1] {
				return key
			}
		}
		return ""
	}
	for key, task := range s.manifest.Tasks {
		if task.Wave == rec.Wave && task.SourceKey == dep {
			return key
		}
	}
	return ""
}

func (s *demoScheduler) recordOutcome(key, outcome, attempt, started, finished string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec := s.manifest.Tasks[key]
	result := s.record.Results[rec.Wave]
	switch outcome {
	case "done":
		result.Completed = append(result.Completed, key)
	case "failed":
		result.Failed = append(result.Failed, key)
		s.failed[key] = true
	case "blocked":
		result.Blocked = append(result.Blocked, key)
		s.failed[key] = true
	case "interrupted":
		result.Interrupted = append(result.Interrupted, key)
		s.failed[key] = true
	}
	s.record.Results[rec.Wave] = result
	if started != "" {
		s.record.Intervals = append(s.record.Intervals, demoInterval{
			TaskID: rec.TaskID, Wave: rec.Wave, Attempt: attempt,
			Started: started, Finished: finished, Outcome: outcome,
		})
	}
}

// waitForDeps parks tasks whose dependencies cannot be satisfied: unselected
// incomplete dependencies block immediately, selected ones resolve through
// live outcomes. Callers must invoke it outside the execution semaphore.
func (s *demoScheduler) waitForDeps(ctx context.Context, key string) bool {
	rec := s.manifest.Tasks[key]
	if s.taskStatus(rec.TaskID) == "done" {
		return true
	}
	selected := map[string]bool{}
	for _, name := range s.options.Waves {
		for k, task := range s.manifest.Tasks {
			if task.Wave == name {
				selected[k] = true
			}
		}
	}
	for {
		parked := false
		waiting := false
		for _, dep := range rec.Deps {
			depKey := s.depKey(rec, dep)
			if depKey == "" {
				parked = true
				break
			}
			depTask := s.manifest.Tasks[depKey]
			if s.taskStatus(depTask.TaskID) == "done" {
				continue
			}
			if !selected[depKey] {
				parked = true
				break
			}
			s.mu.Lock()
			failed := s.failed[depKey]
			s.mu.Unlock()
			if failed {
				parked = true
				break
			}
			waiting = true
		}
		if parked {
			return false
		}
		if !waiting {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(200 * time.Millisecond):
		}
	}
}

func (s *demoScheduler) runTask(ctx context.Context, key string) {
	rec := s.manifest.Tasks[key]
	switch s.taskStatus(rec.TaskID) {
	case "done":
		s.recordOutcome(key, "done", "", "", "")
		return
	case "review":
		// Resuming after an interruption past submit: finish the review.
		s.finishTask(ctx, key, rec, "", time.Now().UTC().Format(time.RFC3339Nano))
		return
	}
	if ctx.Err() != nil {
		s.recordOutcome(key, "interrupted", "", "", "")
		return
	}
	s.executeTask(ctx, key, rec)
}

// runRetrying reloads and retries control operations across CAS conflicts:
// concurrent completions legitimately touch shared dependents, and the hint
// on the refusal says exactly that. Bounded so a real conflict still fails.
func (s *demoScheduler) runRetrying(ctx context.Context, what string, attempts int, op func() error) error {
	var err error
	for i := 0; i < attempts; i++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err = op(); err == nil {
			return nil
		}
		if !demoIsCASConflict(err) {
			return err
		}
		s.note(fmt.Sprintf("%s: CAS conflict, retrying (%d/%d)", what, i+1, attempts))
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(150*(i+1)) * time.Millisecond):
		}
	}
	return err
}

func demoIsCASConflict(err error) bool {
	var typed *TuskerError
	if errors.As(err, &typed) && typed != nil {
		if typed.Code == "CAS_CONFLICT" {
			return true
		}
	}
	return strings.Contains(err.Error(), "CAS_CONFLICT")
}

func (s *demoScheduler) reconcile() {
	_, _ = s.exec.run(s.repoRoot, "reconcile", "--vault", s.vault)
}

func (s *demoScheduler) taskDelay(rec demoTaskRecord) time.Duration {
	secs := rec.DelaySecs
	if s.options.Fast {
		secs = rec.FastSecs
	}
	return time.Duration(secs * float64(time.Second))
}

func (s *demoScheduler) sleepCtx(ctx context.Context, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

// executeTask drives one task through the native lifecycle: ready, claim,
// deterministic delay, artifact write, evidence promote while the run is
// active, submit, deterministic reviewer check, reviewer close.
func (s *demoScheduler) executeTask(ctx context.Context, key string, rec demoTaskRecord) {
	status := s.taskStatus(rec.TaskID)
	if status == "backlog" || status == "rework" {
		// Fresh projections first: without a resident daemon nothing
		// recomputes next_owner until reconcile runs.
		s.reconcile()
		err := s.runRetrying(ctx, "task "+key+" move to ready", 8, func() error {
			_, err := s.exec.run(s.repoRoot, "status", rec.TaskID, "ready", "--reason", "demo run: dependencies satisfied", "--vault", s.vault)
			return err
		})
		if err != nil {
			if gateID := s.openGate(rec.TaskID); gateID != "" {
				s.recordOutcome(key, "blocked", "", "", "")
				s.note(fmt.Sprintf("task %s (%s) blocked on open gate %s: only the owning human can release it with a native confirmation in the UI (`tusker serve`); the CLI never bypasses human authority, then rerun", key, rec.TaskID, gateID))
				return
			}
			s.recordOutcome(key, "failed", "", "", "")
			s.note(fmt.Sprintf("task %s (%s) could not move to ready: %s", key, rec.TaskID, err.Error()))
			return
		}
	}
	if gateID := s.openGate(rec.TaskID); gateID != "" {
		s.recordOutcome(key, "blocked", "", "", "")
		s.note(fmt.Sprintf("task %s (%s) blocked on open gate %s: only the owning human can release it with a native confirmation in the UI (`tusker serve`); the CLI never bypasses human authority, then rerun", key, rec.TaskID, gateID))
		return
	}
	claimed, err := s.exec.run(s.repoRoot, "work", "start", rec.TaskID, "--by", demoImplementActor, "--vault", s.vault)
	if err != nil {
		s.recordOutcome(key, "failed", "", "", "")
		s.note(fmt.Sprintf("task %s (%s) claim failed: %s", key, rec.TaskID, err.Error()))
		return
	}
	workspace, _ := demoDig(claimed, "workspace").(string)
	started := time.Now().UTC().Format(time.RFC3339Nano)
	attempt := s.activeAttempt(rec.TaskID)
	s.note(fmt.Sprintf("task %s (%s) claimed at %s (attempt %s)", key, rec.TaskID, started, attempt))

	delay := s.taskDelay(rec)
	s.note(fmt.Sprintf("task %s (%s) executing: %.1fs deterministic delay", key, rec.TaskID, delay.Seconds()))
	if !s.sleepCtx(ctx, delay) {
		s.releaseLease(rec.TaskID, "interrupted by operator")
		s.recordOutcome(key, "interrupted", attempt, started, time.Now().UTC().Format(time.RFC3339Nano))
		return
	}

	corrupt := strings.EqualFold(s.options.FailOnce, key) && !s.alreadyInjected(key)
	if corrupt {
		s.markInjected(key)
		if _, err := s.exec.run(s.repoRoot, "work", "fail", rec.TaskID, "--by", demoImplementActor, "--reason", "demo fault injection: deterministic branch failure"); err != nil {
			s.note(fmt.Sprintf("task %s (%s) fault injection failed to record: %s", key, rec.TaskID, err.Error()))
		}
		s.recordOutcome(key, "failed", attempt, started, time.Now().UTC().Format(time.RFC3339Nano))
		s.note(fmt.Sprintf("task %s (%s) failed once by injection; retry reruns it cleanly", key, rec.TaskID))
		return
	}

	content := rec.Content
	if strings.EqualFold(s.options.RejectOnce, key) && !s.alreadyInjected(key) {
		s.markInjected(key)
		content = "tampered by demo rejection injection"
	}
	if err := s.writeArtifact(rec, workspace, content); err != nil {
		s.releaseLease(rec.TaskID, "artifact write failed")
		s.recordOutcome(key, "failed", attempt, started, time.Now().UTC().Format(time.RFC3339Nano))
		s.note(fmt.Sprintf("task %s (%s) artifact write failed: %s", key, rec.TaskID, err.Error()))
		return
	}
	promoteErr := s.runRetrying(ctx, "task "+key+" evidence promote", 5, func() error {
		_, err := s.exec.run(s.repoRoot, "evidence", "promote", rec.TaskID, "--from", filepath.Join(".tusker", "scratch", rec.TaskID, filepath.Base(rec.Artifact)), "--kind", "automated_test", "--covers", "A1", "--vault", s.vault)
		return err
	})
	if promoteErr != nil {
		s.releaseLease(rec.TaskID, "evidence promote failed")
		s.recordOutcome(key, "failed", attempt, started, time.Now().UTC().Format(time.RFC3339Nano))
		s.note(fmt.Sprintf("task %s (%s) evidence promote failed: %s", key, rec.TaskID, promoteErr.Error()))
		return
	}
	submitErr := s.runRetrying(ctx, "task "+key+" submit", 5, func() error {
		_, err := s.exec.run(s.repoRoot, "work", "submit", rec.TaskID, "--by", demoImplementActor, "--gate-verdicts", "A1=pass", "--deliverable", "demo-timer: wrote "+rec.Artifact, "--verification", "command check passes on fixture bytes", "--vault", s.vault)
		return err
	})
	if submitErr != nil {
		// A partial submit can land review while reporting a conflict.
		// Re-reading keeps the driver idempotent instead of failing done work.
		if s.taskStatus(rec.TaskID) == "review" {
			s.note(fmt.Sprintf("task %s (%s) submit reported conflict after reaching review; continuing", key, rec.TaskID))
			s.finishTask(ctx, key, rec, attempt, started)
			return
		}
		s.recordOutcome(key, "failed", attempt, started, time.Now().UTC().Format(time.RFC3339Nano))
		s.note(fmt.Sprintf("task %s (%s) submit failed: %s", key, rec.TaskID, submitErr.Error()))
		return
	}
	s.finishTask(ctx, key, rec, attempt, started)
}

// finishTask runs the deterministic reviewer stage and the reviewer close.
// The reviewer checks the declared file and result; a mismatch moves the
// task to rework, corrects the bytes, and resubmits as a new attempt with no
// duplicate acceptance. It never approves as a human.
func (s *demoScheduler) finishTask(ctx context.Context, key string, rec demoTaskRecord, attempt, started string) {
	if started == "" {
		started = time.Now().UTC().Format(time.RFC3339Nano)
	}
	if !s.reviewerAccepts(key, rec) {
		s.note(fmt.Sprintf("task %s (%s) reviewer rejected the artifact: content mismatch, requesting rework", key, rec.TaskID))
		if _, err := s.exec.run(s.repoRoot, "status", rec.TaskID, "rework", "--reason", "demo reviewer: artifact does not match the declared fixture bytes", "--vault", s.vault); err != nil {
			s.recordOutcome(key, "failed", attempt, started, time.Now().UTC().Format(time.RFC3339Nano))
			return
		}
		if err := s.writeArtifact(rec, "", rec.Content); err != nil {
			s.recordOutcome(key, "failed", attempt, started, time.Now().UTC().Format(time.RFC3339Nano))
			return
		}
		if _, err := s.exec.run(s.repoRoot, "evidence", "promote", rec.TaskID, "--from", filepath.Join(".tusker", "scratch", rec.TaskID, filepath.Base(rec.Artifact)), "--kind", "automated_test", "--covers", "A1", "--vault", s.vault); err != nil {
			s.recordOutcome(key, "failed", attempt, started, time.Now().UTC().Format(time.RFC3339Nano))
			return
		}
		s.note(fmt.Sprintf("task %s (%s) corrected after rejection; resubmitting as a new attempt", key, rec.TaskID))
		if _, err := s.exec.run(s.repoRoot, "status", rec.TaskID, "ready", "--reason", "demo run: corrected after reviewer rejection", "--vault", s.vault); err != nil {
			s.recordOutcome(key, "failed", attempt, started, time.Now().UTC().Format(time.RFC3339Nano))
			return
		}
		if ctx.Err() != nil {
			s.recordOutcome(key, "interrupted", attempt, started, time.Now().UTC().Format(time.RFC3339Nano))
			return
		}
		s.executeTask(ctx, key, rec)
		return
	}
	if err := s.closeTask(ctx, rec); err != nil {
		if gateID := s.openGate(rec.TaskID); gateID != "" {
			s.recordOutcome(key, "blocked", attempt, started, time.Now().UTC().Format(time.RFC3339Nano))
			s.note(fmt.Sprintf("task %s (%s) blocked on open gate %s", key, rec.TaskID, gateID))
			return
		}
		// A partial close can land done while reporting a conflict.
		if s.taskStatus(rec.TaskID) == "done" {
			s.note(fmt.Sprintf("task %s (%s) close reported conflict after reaching done; accepting", key, rec.TaskID))
			s.recordOutcome(key, "done", attempt, started, time.Now().UTC().Format(time.RFC3339Nano))
			return
		}
		s.recordOutcome(key, "failed", attempt, started, time.Now().UTC().Format(time.RFC3339Nano))
		s.note(fmt.Sprintf("task %s (%s) close failed: %s", key, rec.TaskID, err.Error()))
		return
	}
	s.recordOutcome(key, "done", attempt, started, time.Now().UTC().Format(time.RFC3339Nano))
}

func (s *demoScheduler) alreadyInjected(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.injected[key]
}

func (s *demoScheduler) markInjected(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.injected[key] = true
}

func (s *demoScheduler) activeAttempt(taskID string) string {
	inspected, err := s.exec.run(s.repoRoot, "runs", "inspect", taskID, "--vault", s.vault)
	if err != nil {
		return ""
	}
	run, _ := demoDig(inspected, "run").(map[string]any)
	if run == nil {
		return ""
	}
	active, _ := run["active_attempt_id"].(string)
	if active != "" {
		return active
	}
	if attempts, ok := demoDig(inspected, "attempts").([]any); ok && len(attempts) > 0 {
		if last, ok := attempts[len(attempts)-1].(map[string]any); ok {
			id, _ := last["attempt_id"].(string)
			return id
		}
	}
	return ""
}

func (s *demoScheduler) releaseLease(taskID, reason string) {
	_, _ = s.exec.run(s.repoRoot, "work", "release", taskID, "--by", demoImplementActor, "--reason", reason, "--vault", s.vault)
}

func (s *demoScheduler) writeArtifact(rec demoTaskRecord, workspace, content string) error {
	targets := []string{filepath.Join(s.repoRoot, rec.Artifact)}
	if workspace != "" {
		targets = append(targets, filepath.Join(workspace, rec.Artifact))
	}
	scratch := filepath.Join(s.vault, "scratch", rec.TaskID, filepath.Base(rec.Artifact))
	targets = append(targets, scratch)
	for _, path := range targets {
		if err := ensureDir(filepath.Dir(path)); err != nil {
			return err
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// reviewerAccepts performs the deterministic reviewer check: the declared
// file must exist in the repo with the exact fixture bytes. It records no
// state; acceptance is the later reviewer-authorized close.
func (s *demoScheduler) reviewerAccepts(key string, rec demoTaskRecord) bool {
	raw, err := os.ReadFile(filepath.Join(s.repoRoot, rec.Artifact))
	if err != nil {
		return false
	}
	return string(raw) == rec.Content
}

var demoConfirmPattern = regexp.MustCompile(`sha256:[0-9a-f]{64}`)

func (s *demoScheduler) closeTask(ctx context.Context, rec demoTaskRecord) error {
	err := s.runRetrying(ctx, "task "+rec.TaskID+" close", 5, func() error {
		_, err := s.exec.run(s.repoRoot, "close", rec.TaskID, "--by", demoReviewerActor, "--reason", "demo reviewer: fixture bytes verified, proof satisfied", "--vault", s.vault)
		return err
	})
	if err == nil {
		return nil
	}
	// The verification manifest arrives in the refusal hint, the same way
	// an operator reads it from the CLI and confirms explicitly.
	haystack := err.Error()
	var typed *TuskerError
	if errors.As(err, &typed) && typed != nil {
		haystack += " " + typed.Hint
	}
	manifest := demoConfirmPattern.FindString(haystack)
	if manifest == "" {
		return err
	}
	return s.runRetrying(ctx, "task "+rec.TaskID+" confirmed close", 5, func() error {
		_, err := s.exec.run(s.repoRoot, "close", rec.TaskID, "--by", demoReviewerActor, "--reason", "demo reviewer: fixture bytes verified, proof satisfied", "--confirm", manifest, "--vault", s.vault)
		return err
	})
}

func (s *demoScheduler) openGate(taskID string) string {
	idx, err := loadV7Index(s.vault)
	if err != nil {
		return ""
	}
	for _, gate := range idx.Gates {
		if !v7GateTouchesTask(gate, taskID) {
			continue
		}
		if status := strings.ToLower(strings.TrimSpace(stringField(gate.Data, "status"))); status != "satisfied" && status != "waived" && status != "obsolete" {
			return stringField(gate.Data, "id")
		}
	}
	return ""
}

func demoRunText(record demoRunRecord) string {
	var lines []string
	lines = append(lines, fmt.Sprintf("run %s (%s):", record.RunID, record.Executor))
	names := make([]string, 0, len(record.Results))
	for name := range record.Results {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		result := record.Results[name]
		lines = append(lines, fmt.Sprintf("  wave %s: %d completed, %d failed, %d blocked, %d interrupted%s",
			name, len(result.Completed), len(result.Failed), len(result.Blocked), len(result.Interrupted),
			func() string {
				if result.Error != "" {
					return " error: " + result.Error
				}
				return ""
			}()))
	}
	return strings.Join(lines, "\n")
}

func demoStatusText(manifest *demoManifest, tasks []demoTaskView, waves []demoWaveView) string {
	var lines []string
	for _, wave := range waves {
		lines = append(lines, fmt.Sprintf("wave %s (%s, %s): %d/%d done", wave.Name, wave.WaveID, wave.Authorization, wave.Done, len(wave.Members)))
	}
	for _, task := range tasks {
		extra := ""
		if len(task.Blockers) > 0 {
			extra = " blocked by " + strings.Join(task.Blockers, ", ")
		}
		lines = append(lines, fmt.Sprintf("  %s %s: %s/%s%s", task.Key, task.TaskID, task.Status, task.Readiness, extra))
	}
	return strings.Join(lines, "\n")
}
