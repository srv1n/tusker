package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// wait
// ---------------------------------------------------------------------------

func demoWaitCmd(args Args) (int, error) {
	payload, code, err := demoWait(args)
	if err != nil {
		if code == 0 {
			code = demoExitForError(err)
		}
		return demoFail(args, code, err)
	}
	payload["ok"] = true
	if err := demoEmit(args, payload); err != nil {
		return demoExitInternal, err
	}
	return code, nil
}

func demoWait(args Args) (map[string]any, int, error) {
	repoRoot, manifest, err := demoResolveRepo(args, true)
	if err != nil {
		return nil, 0, err
	}
	demoEnsureStateRoot(repoRoot)
	if until := strings.TrimSpace(args.String("until")); until != "" && until != "terminal" {
		return nil, 0, tuskerError(errorInvalidArg, "unsupported --until: "+until, withHint("only --until terminal is supported"))
	}
	timeout, err := demoParseDuration(firstNonEmpty(strings.TrimSpace(args.String("timeout")), "120s"))
	if err != nil {
		return nil, 0, err
	}
	deadline := time.Now().Add(timeout)
	for {
		terminal, summary := demoTerminalState(repoRoot, manifest)
		if terminal {
			return map[string]any{
				"repo": repoRoot, "state": "terminal", "summary": summary,
				"text": "terminal: " + summary,
			}, demoExitOK, nil
		}
		if !time.Now().Before(deadline) {
			return map[string]any{
				"repo": repoRoot, "state": "waiting", "summary": summary,
				"text": "timeout waiting for terminal state (not a task failure): " + summary,
			}, demoExitTimeout, tuskerError(demoCodeTimeout, "demo wait timed out after "+timeout.String())
		}
		select {
		case <-time.After(time.Second):
		}
	}
}

func demoParseDuration(raw string) (time.Duration, error) {
	parsed, err := time.ParseDuration(raw)
	if err != nil || parsed <= 0 {
		return 0, tuskerError(errorInvalidArg, "invalid --timeout: "+raw, withHint("use Go duration form, e.g. 120s, 2m"))
	}
	return parsed, nil
}

func demoTerminalState(repoRoot string, manifest *demoManifest) (bool, string) {
	vaultPath := demoVaultPath(repoRoot)
	idx, err := loadV7Index(vaultPath)
	if err != nil {
		return false, "index unreadable: " + err.Error()
	}
	exec := demoNewExec()
	pending := []string{}
	active := []string{}
	for _, key := range demoSortedTaskKeys(manifest) {
		rec := manifest.Tasks[key]
		note, ok := idx.Tasks[rec.TaskID]
		if !ok {
			pending = append(pending, key+"(missing)")
			continue
		}
		status := strings.ToLower(strings.TrimSpace(stringField(note.Data, "status")))
		switch status {
		case "done", "cancelled", "superseded":
		default:
			pending = append(pending, key+"("+status+")")
		}
		if lease := demoActiveLease(exec, repoRoot, rec.TaskID); lease != "none" {
			active = append(active, key+"("+lease+")")
		}
	}
	if len(pending) == 0 && len(active) == 0 {
		return true, fmt.Sprintf("all %d demo tasks terminal, no active runs", len(manifest.Tasks))
	}
	summary := ""
	if len(pending) > 0 {
		summary += "non-terminal: " + strings.Join(pending, ", ")
	}
	if len(active) > 0 {
		if summary != "" {
			summary += "; "
		}
		summary += "active runs: " + strings.Join(active, ", ")
	}
	return false, summary
}

// ---------------------------------------------------------------------------
// check
// ---------------------------------------------------------------------------

type demoAssertion struct {
	Name     string `json:"name"`
	Result   string `json:"result"`
	Expected string `json:"expected"`
	Observed string `json:"observed"`
	Ref      string `json:"ref"`
}

func demoCheckCmd(args Args) (int, error) {
	payload, code, err := demoCheck(args)
	if err != nil {
		if code == 0 {
			code = demoExitForError(err)
		}
		return demoFail(args, code, err)
	}
	payload["ok"] = true
	if err := demoEmit(args, payload); err != nil {
		return demoExitInternal, err
	}
	return code, nil
}

func demoCheck(args Args) (map[string]any, int, error) {
	repoRoot, manifest, err := demoResolveRepo(args, true)
	if err != nil {
		return nil, 0, err
	}
	demoEnsureStateRoot(repoRoot)
	vaultPath := demoVaultPath(repoRoot)
	idx, err := loadV7Index(vaultPath)
	if err != nil {
		return nil, 0, err
	}
	exec := demoNewExec()
	assertions := []demoAssertion{}
	assertions = append(assertions, demoCheckGraph(manifest, idx))
	assertions = append(assertions, demoCheckInitialState(manifest, idx))
	assertions = append(assertions, demoCheckOutputs(manifest, idx, repoRoot))
	assertions = append(assertions, demoCheckRealAttempts(manifest, exec, repoRoot))
	assertions = append(assertions, demoCheckReviewEvidence(manifest, idx, vaultPath))
	assertions = append(assertions, demoCheckFollowupGating(manifest, idx))
	assertions = append(assertions, demoCheckNoActiveRuns(manifest, exec, repoRoot))
	assertions = append(assertions, demoCheckOverlap(manifest, exec, repoRoot))
	assertions = append(assertions, demoCheckJoins(manifest))
	assertions = append(assertions, demoCheckVariants(manifest, idx))
	assertions = append(assertions, demoCheckGate(manifest, idx))

	failed := 0
	var lines []string
	for _, assertion := range assertions {
		lines = append(lines, fmt.Sprintf("[%s] %s: expected %s; observed %s", assertion.Result, assertion.Name, assertion.Expected, assertion.Observed))
		if assertion.Result == "FAIL" {
			failed++
		}
	}
	code := demoExitOK
	if failed > 0 {
		code = demoExitAssertion
	}
	return map[string]any{
		"repo": repoRoot, "assertions": assertions, "failed": failed,
		"text": strings.Join(lines, "\n"),
	}, code, nil
}

func demoPass(name, expected, observed, ref string) demoAssertion {
	return demoAssertion{Name: name, Result: "PASS", Expected: expected, Observed: observed, Ref: ref}
}

func demoFailAs(name, expected, observed, ref string) demoAssertion {
	return demoAssertion{Name: name, Result: "FAIL", Expected: expected, Observed: observed, Ref: ref}
}

func demoSkip(name, reason string) demoAssertion {
	return demoAssertion{Name: name, Result: "SKIP", Expected: reason, Observed: "not applicable", Ref: ""}
}

func demoCheckGraph(manifest *demoManifest, idx v7Index) demoAssertion {
	const name = "seed-graph-exact"
	if len(manifest.Waves) != 4 || len(manifest.Tasks) != 13 {
		return demoFailAs(name, "4 waves, 13 tasks", fmt.Sprintf("%d waves, %d tasks", len(manifest.Waves), len(manifest.Tasks)), "")
	}
	if _, ok := manifest.Waves["standalone"]; !ok {
		return demoFailAs(name, "standalone wave present", "missing standalone wave", "")
	}
	if _, ok := manifest.Tasks["s1"]; !ok {
		return demoFailAs(name, "standalone smoke task present", "missing task s1", "")
	}
	for key, rec := range manifest.Tasks {
		note, ok := idx.Tasks[rec.TaskID]
		if !ok {
			return demoFailAs(name, "every mapped task exists", "missing "+rec.TaskID+" ("+key+")", "")
		}
		if wave := stringField(note.Data, "wave"); wave == "" {
			return demoFailAs(name, "every task in a wave", key+" has no wave", "")
		}
	}
	for _, name := range demoSortedWaveNames(manifest) {
		rec := manifest.Waves[name]
		note, ok := idx.Waves[rec.WaveID]
		if !ok {
			return demoFailAs(name, "every mapped wave exists", "missing "+rec.WaveID, "")
		}
		members := normalizeList(note.Data["members"])
		want := 4
		if name == "standalone" {
			want = 1
		}
		if len(members) != want {
			return demoFailAs(name, fmt.Sprintf("%d members in wave %s", want, name), fmt.Sprintf("%s has %d", rec.WaveID, len(members)), "")
		}
	}
	for _, level := range []string{"implement", "review", "plan"} {
		if manifest.Profiles[level] == "" {
			return demoFailAs(name, "test profiles mapped for implement/review/plan", "missing "+level, "")
		}
	}
	// The fixture must exercise every supported work level through the
	// ordinary complexity mapping (routine=light, standard, complex=demanding).
	levels := map[string]bool{}
	for _, rec := range manifest.Tasks {
		levels[demoWorkLevel(rec.Complexity)] = true
	}
	for _, level := range []string{"light", "standard", "demanding"} {
		if !levels[level] {
			return demoFailAs(name, "fixture covers light, standard and demanding work", "missing "+level, "")
		}
	}
	return demoPass(name, "4 waves, 13 tasks, expected members each", "4 waves, 13 tasks, expected members each", "")
}

func demoCheckInitialState(manifest *demoManifest, idx v7Index) demoAssertion {
	const name = "seed-initial-state"
	if len(manifest.Runs) > manifest.RunsAtReset {
		return demoSkip(name, "a run already happened; initial state is superseded")
	}
	for _, key := range demoSortedTaskKeys(manifest) {
		rec := manifest.Tasks[key]
		note, ok := idx.Tasks[rec.TaskID]
		if !ok {
			return demoFailAs(name, "task present", "missing "+key, "")
		}
		status := strings.ToLower(strings.TrimSpace(stringField(note.Data, "status")))
		roots := len(rec.Deps) == 0 && rec.Wave != "follow-up"
		_ = roots
		if len(rec.Deps) == 0 && rec.Wave != "follow-up" {
			if status != "ready" {
				return demoFailAs(name, "roots ready", key+" is "+status, "")
			}
			continue
		}
		if status != "backlog" {
			return demoFailAs(name, "non-roots backlog", key+" is "+status, "")
		}
	}
	return demoPass(name, "roots ready, branches/joins/follow-up backlog", "roots ready, branches/joins/follow-up backlog", "")
}

func demoLastRunExecutor(manifest *demoManifest) string {
	if len(manifest.Runs) == 0 {
		return ""
	}
	return manifest.Runs[len(manifest.Runs)-1].Executor
}

func demoCheckOutputs(manifest *demoManifest, idx v7Index, repoRoot string) demoAssertion {
	const name = "outputs-match-files"
	if demoLastRunExecutor(manifest) == "real-harness" {
		return demoSkip(name, "last run used the real harness: artifact bytes are owned by agent work, see real-attempts-bound")
	}
	done := 0
	for _, key := range demoSortedTaskKeys(manifest) {
		rec := manifest.Tasks[key]
		note, ok := idx.Tasks[rec.TaskID]
		if !ok {
			continue
		}
		if strings.ToLower(strings.TrimSpace(stringField(note.Data, "status"))) != "done" {
			continue
		}
		done++
		raw, err := os.ReadFile(filepath.Join(repoRoot, rec.Artifact))
		if err != nil {
			return demoFailAs(name, "artifact readable for "+key, err.Error(), "")
		}
		if string(raw) != rec.Content {
			return demoFailAs(name, "exact fixture bytes for "+key, "content differs", "")
		}
	}
	if done == 0 {
		return demoSkip(name, "no completed tasks yet")
	}
	return demoPass(name, "every done task matches its fixture file", fmt.Sprintf("%d done tasks match", done), "")
}

// demoCheckRealAttempts verifies real-harness runs from runtime evidence:
// every task the run claims must resolve to an actual configured
// profile/harness/model (nothing substituted) and bind a real runtime
// attempt ID. Timer-lane-only histories skip.
func demoCheckRealAttempts(manifest *demoManifest, exec *demoExec, repoRoot string) demoAssertion {
	const name = "real-attempts-bound"
	runtimeExec := demoExecForScope(exec, manifest.RuntimeScope, manifest.RuntimeStripScope)
	var last *demoRunRecord
	for i := len(manifest.Runs) - 1; i >= 0; i-- {
		if manifest.Runs[i].Executor == "real-harness" {
			last = &manifest.Runs[i]
			break
		}
	}
	if last == nil {
		return demoSkip(name, "no real-harness run recorded")
	}
	if len(last.TaskProfiles) == 0 {
		return demoFailAs(name, "real run records effective profiles", "no task profiles recorded", "")
	}
	checked := 0
	for key, profile := range last.TaskProfiles {
		if profile.Profile == "" || profile.Harness == "" {
			return demoFailAs(name, "every real task has an effective profile and harness", key+" resolved empty", "")
		}
		if profile.Attempt == "" {
			attempts := demoRuntimeAttempts(runtimeExec, repoRoot, manifest.RuntimeProjectID, manifest.Tasks[key].TaskID)
			executeOK, reviewOK := false, false
			for _, attempt := range attempts {
				if attempt.Outcome != "succeeded" || attempt.Started < last.StartedAt {
					continue
				}
				executeOK = executeOK || attempt.Lane == runLaneExecute
				reviewOK = reviewOK || attempt.Lane == runLaneReview
			}
			if !executeOK || !reviewOK {
				return demoFailAs(name, "every real task has successful worker and reviewer attempts in the recorded run window", key+" is missing a successful worker or reviewer attempt", "")
			}
			checked++
			continue
		}
		if !demoAttemptKnown(runtimeExec, repoRoot, manifest.RuntimeProjectID, manifest.Tasks[key].TaskID, profile.Attempt) {
			return demoFailAs(name, "recorded attempts exist in runtime inspection", key+" attempt "+profile.Attempt+" not found", "")
		}
		checked++
	}
	return demoPass(name, "real tasks bound to runtime attempts", fmt.Sprintf("%d tasks verified against runtime inspection", checked), "")
}

func demoAttemptKnown(exec *demoExec, repoRoot, projectID, taskID, attemptID string) bool {
	for _, attempt := range demoRuntimeAttempts(exec, repoRoot, projectID, taskID) {
		if attempt.ID == attemptID {
			return true
		}
	}
	return false
}

func demoCheckReviewEvidence(manifest *demoManifest, idx v7Index, vaultPath string) demoAssertion {
	const name = "review-evidence-bound"
	done := 0
	for _, key := range demoSortedTaskKeys(manifest) {
		rec := manifest.Tasks[key]
		note, ok := idx.Tasks[rec.TaskID]
		if !ok {
			continue
		}
		if strings.ToLower(strings.TrimSpace(stringField(note.Data, "status"))) != "done" {
			continue
		}
		done++
		if proof := strings.ToLower(strings.TrimSpace(stringField(note.Data, "proof_status"))); proof != "satisfied" {
			return demoFailAs(name, "proof satisfied for "+key, "proof_status="+proof, "")
		}
		if len(idx.Evidence[rec.TaskID]) == 0 {
			if missing := v7VerificationReceiptRequirementMissing(vaultPath, note); missing != "" {
				if !demoClosedVerificationReceiptsBound(note) {
					return demoFailAs(name, "current evidence or verification receipt bound for "+key, missing, "")
				}
			}
		}
	}
	if done == 0 {
		return demoSkip(name, "no completed tasks yet")
	}
	return demoPass(name, "proof satisfied with bound evidence or verification receipts", fmt.Sprintf("%d done tasks verified", done), "")
}

func demoClosedVerificationReceiptsBound(task Note) bool {
	authority, ok := v7TaskCloseAuthorityFromAny(task.Data["close_authority"])
	if !ok || validateV7TaskCloseAuthorityFact(authority, stringField(task.Data, "project"), stringField(task.Data, "id"), stringField(task.Data, "accepted_by"), task.Body) != nil {
		return false
	}
	seen := false
	for _, row := range parseV7VerificationRows(task.Body) {
		if _, command := v7VerificationCommand(row.Check); !command {
			continue
		}
		seen = true
		marker := v7VerificationReceiptSchema + " "
		pos := strings.LastIndex(row.Notes, marker)
		if pos < 0 {
			return false
		}
		material := ""
		for _, token := range strings.Fields(row.Notes[pos+len(marker):]) {
			if key, value, found := strings.Cut(strings.TrimRight(token, ";"), "="); found && key == "material" {
				material = value
			}
		}
		if !v7VerificationReceiptCurrent(task, row, material, nil) {
			return false
		}
	}
	return seen
}

func demoCheckFollowupGating(manifest *demoManifest, idx v7Index) demoAssertion {
	const name = "followup-gating"
	c1 := manifest.Tasks["c1"]
	note, ok := idx.Tasks[c1.TaskID]
	if !ok {
		return demoFailAs(name, "c1 present", "missing", "")
	}
	status := strings.ToLower(strings.TrimSpace(stringField(note.Data, "status")))
	if status == "done" {
		a4, b4 := manifest.Tasks["a4"], manifest.Tasks["b4"]
		a4note, aok := idx.Tasks[a4.TaskID]
		b4note, bok := idx.Tasks[b4.TaskID]
		if !aok || !bok {
			return demoFailAs(name, "predecessors present", "missing", "")
		}
		if strings.ToLower(strings.TrimSpace(stringField(a4note.Data, "status"))) != "done" ||
			strings.ToLower(strings.TrimSpace(stringField(b4note.Data, "status"))) != "done" {
			return demoFailAs(name, "a4 and b4 done before c1", "predecessor incomplete", "")
		}
		return demoPass(name, "c1 done only after a4 and b4", "a4, b4, c1 done in order", "")
	}
	if edge, blocked := v7BlockingDependencyForReadiness(note, idx); !blocked {
		return demoFailAs(name, "c1 blocked on unmet predecessors", "no blocker reported", "")
	} else {
		return demoPass(name, "c1 blocked on unmet predecessors", "blocked by "+edge.ID, "")
	}
}

func demoCheckNoActiveRuns(manifest *demoManifest, exec *demoExec, repoRoot string) demoAssertion {
	const name = "no-active-runs"
	active := []string{}
	for _, key := range demoSortedTaskKeys(manifest) {
		if lease := demoActiveLease(exec, repoRoot, manifest.Tasks[key].TaskID); lease != "none" {
			active = append(active, key+"("+lease+")")
		}
	}
	if len(active) > 0 {
		return demoFailAs(name, "no active runs", strings.Join(active, ", "), "")
	}
	return demoPass(name, "no active runs", "none", "")
}

func demoParseStamp(raw string) (time.Time, error) {
	if parsed, err := time.Parse(time.RFC3339Nano, raw); err == nil {
		return parsed, nil
	}
	return time.Parse(time.RFC3339, raw)
}

func demoAttemptIntervals(exec *demoExec, repoRoot, taskID string) [][2]time.Time {
	var out [][2]time.Time
	inspected, err := exec.run(repoRoot, "runs", "inspect", taskID)
	if err != nil {
		return out
	}
	attempts, _ := demoDig(inspected, "attempts").([]any)
	for _, entry := range attempts {
		record, _ := entry.(map[string]any)
		if record == nil {
			continue
		}
		startRaw, _ := record["started_at"].(string)
		finishRaw, _ := record["finished_at"].(string)
		start, err1 := time.Parse(time.RFC3339Nano, startRaw)
		if err1 != nil {
			start, err1 = time.Parse(time.RFC3339, startRaw)
		}
		finish, err2 := time.Parse(time.RFC3339Nano, finishRaw)
		if err2 != nil {
			finish, err2 = time.Parse(time.RFC3339, finishRaw)
		}
		if err1 != nil || err2 != nil {
			continue
		}
		out = append(out, [2]time.Time{start, finish})
	}
	return out
}

func demoCheckOverlap(manifest *demoManifest, exec *demoExec, repoRoot string) demoAssertion {
	const name = "parallel-overlap"
	if len(manifest.Runs) == 0 {
		return demoSkip(name, "no run recorded yet")
	}
	alpha, beta := []demoInterval{}, []demoInterval{}
	for i := len(manifest.Runs) - 1; i >= 0; i-- {
		alpha, beta = alpha[:0], beta[:0]
		for _, interval := range manifest.Runs[i].Intervals {
			switch interval.Wave {
			case "alpha":
				alpha = append(alpha, interval)
			case "beta":
				beta = append(beta, interval)
			}
		}
		if len(alpha) > 0 && len(beta) > 0 {
			break
		}
	}
	if len(alpha) == 0 || len(beta) == 0 {
		return demoSkip(name, "last run did not cover both alpha and beta")
	}
	// Prefer runtime attempt timestamps (the authoritative overlap evidence);
	// fall back to the driver-observed manifest intervals.
	runtimeSpan := func(keys []string) (time.Time, time.Time, bool) {
		var lo, hi time.Time
		ok := false
		for _, key := range keys {
			for _, span := range demoAttemptIntervals(exec, repoRoot, manifest.Tasks[key].TaskID) {
				if !ok || span[0].Before(lo) {
					lo = span[0]
				}
				if !ok || span[1].After(hi) {
					hi = span[1]
				}
				ok = true
			}
		}
		return lo, hi, ok
	}
	alphaKeys, betaKeys := []string{}, []string{}
	for _, interval := range append(append([]demoInterval{}, alpha...), beta...) {
		for key, task := range manifest.Tasks {
			if task.TaskID == interval.TaskID {
				if task.Wave == "alpha" {
					alphaKeys = append(alphaKeys, key)
				} else {
					betaKeys = append(betaKeys, key)
				}
			}
		}
	}
	if aLo, aHi, aOK := runtimeSpan(alphaKeys); aOK {
		if bLo, bHi, bOK := runtimeSpan(betaKeys); bOK {
			if aLo.Before(bHi) && bLo.Before(aHi) {
				return demoPass(name, "alpha and beta execution overlaps", fmt.Sprintf("runtime attempts overlap: alpha %s..%s, beta %s..%s",
					aLo.Format("15:04:05"), aHi.Format("15:04:05"), bLo.Format("15:04:05"), bHi.Format("15:04:05")), "")
			}
			return demoFailAs(name, "alpha and beta execution overlaps", "runtime attempt spans are disjoint", "")
		}
	}
	span := func(intervals []demoInterval) (time.Time, time.Time, bool) {
		var lo, hi time.Time
		ok := false
		for _, interval := range intervals {
			start, err1 := demoParseStamp(interval.Started)
			finish, err2 := demoParseStamp(interval.Finished)
			if err1 != nil || err2 != nil {
				continue
			}
			if !ok || start.Before(lo) {
				lo = start
			}
			if !ok || finish.After(hi) {
				hi = finish
			}
			ok = true
		}
		return lo, hi, ok
	}
	aLo, aHi, aOK := span(alpha)
	bLo, bHi, bOK := span(beta)
	if !aOK || !bOK {
		return demoSkip(name, "run intervals incomplete")
	}
	if aLo.Before(bHi) && bLo.Before(aHi) {
		return demoPass(name, "alpha and beta execution overlaps", fmt.Sprintf("alpha %s..%s overlaps beta %s..%s",
			aLo.Format("15:04:05"), aHi.Format("15:04:05"), bLo.Format("15:04:05"), bHi.Format("15:04:05")), "")
	}
	return demoFailAs(name, "alpha and beta execution overlaps", "spans are disjoint", "")
}

func demoCheckJoins(manifest *demoManifest) demoAssertion {
	const name = "joins-wait"
	if len(manifest.Runs) == 0 {
		return demoSkip(name, "no run recorded yet")
	}
	// Scope to the current reset generation so predecessors from an earlier
	// pass never mix with joins from a later one.
	generation := manifest.Runs
	if manifest.RunsAtReset >= 0 && manifest.RunsAtReset < len(manifest.Runs) {
		generation = manifest.Runs[manifest.RunsAtReset:]
	}
	byKey := map[string]demoInterval{}
	for _, run := range generation {
		for _, interval := range run.Intervals {
			for key, task := range manifest.Tasks {
				if task.TaskID == interval.TaskID && interval.Outcome == "done" {
					byKey[key] = interval
				}
			}
		}
	}
	checked := 0
	for key, rec := range manifest.Tasks {
		if len(rec.Deps) < 2 {
			continue
		}
		join, ok := byKey[key]
		if !ok {
			continue
		}
		joinStart, err := demoParseStamp(join.Started)
		if err != nil {
			continue
		}
		for _, dep := range rec.Deps {
			depKey := ""
			if strings.Contains(dep, "/") {
				parts := strings.SplitN(dep, "/", 2)
				for k, task := range manifest.Tasks {
					if wave, ok := manifest.Waves[task.Wave]; ok && wave.Scope == parts[0] && task.SourceKey == parts[1] {
						depKey = k
					}
				}
			} else {
				for k, task := range manifest.Tasks {
					if task.Wave == rec.Wave && task.SourceKey == dep {
						depKey = k
					}
				}
			}
			branch, ok := byKey[depKey]
			if !ok {
				continue
			}
			branchFinish, err := demoParseStamp(branch.Finished)
			if err != nil {
				continue
			}
			checked++
			if joinStart.Before(branchFinish) {
				return demoFailAs(name, "join starts after every branch finishes", key+" started before "+depKey+" finished", "")
			}
		}
	}
	if checked == 0 {
		return demoSkip(name, "no completed multi-dependency joins yet")
	}
	return demoPass(name, "joins start after every branch finishes", fmt.Sprintf("%d branch edges ordered", checked), "")
}

var demoInjectedTaskPattern = regexp.MustCompile(`task ([a-z][0-9]) \(`)

func demoCheckVariants(manifest *demoManifest, idx v7Index) demoAssertion {
	const name = "failure-retry-clean"
	injected := map[string]bool{}
	for _, run := range manifest.Runs {
		for _, note := range run.Notes {
			if !strings.Contains(note, "failed once by injection") && !strings.Contains(note, "reviewer rejected") {
				continue
			}
			if match := demoInjectedTaskPattern.FindStringSubmatch(note); match != nil {
				injected[match[1]] = true
			}
		}
	}
	if len(injected) == 0 {
		return demoSkip(name, "no fault injection recorded")
	}
	var keys []string
	for key := range injected {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		rec, ok := manifest.Tasks[key]
		if !ok {
			return demoFailAs(name, "injected task known", "unknown "+key, "")
		}
		note, ok := idx.Tasks[rec.TaskID]
		if !ok {
			return demoFailAs(name, "injected task present", key+" missing", "")
		}
		if strings.ToLower(strings.TrimSpace(stringField(note.Data, "status"))) != "done" {
			return demoFailAs(name, "injected task eventually done", key+" is not done", "")
		}
	}
	return demoPass(name, "injected tasks eventually done exactly once", strings.Join(keys, ",")+" done", "")
}

func demoCheckGate(manifest *demoManifest, idx v7Index) demoAssertion {
	const name = "gate-honored"
	if !manifest.HumanGate {
		return demoSkip(name, "seeded without a human gate")
	}
	c4 := manifest.Tasks["c4"]
	gateID, open := "", false
	for _, gate := range idx.Gates {
		if !v7GateTouchesTask(gate, c4.TaskID) {
			continue
		}
		if status := strings.ToLower(strings.TrimSpace(stringField(gate.Data, "status"))); status != "satisfied" && status != "waived" && status != "obsolete" {
			open = true
			gateID = stringField(gate.Data, "id")
		}
	}
	note, ok := idx.Tasks[c4.TaskID]
	status := ""
	if ok {
		status = strings.ToLower(strings.TrimSpace(stringField(note.Data, "status")))
	}
	if open {
		if status == "done" {
			return demoFailAs(name, "open gate blocks completion", "c4 done despite open "+gateID, "")
		}
		return demoPass(name, "open gate blocks completion", "c4 "+status+", gate "+gateID+" open", "")
	}
	if status != "done" {
		return demoFailAs(name, "satisfied gate releases the task", "gate satisfied but c4 "+status, "")
	}
	return demoPass(name, "satisfied gate releases the task", "c4 done", "")
}

// ---------------------------------------------------------------------------
// reset
// ---------------------------------------------------------------------------

func demoResetCmd(args Args) (int, error) {
	payload, err := demoReset(args)
	if err != nil {
		return demoFail(args, demoExitForError(err), err)
	}
	payload["ok"] = true
	if err := demoEmit(args, payload); err != nil {
		return demoExitInternal, err
	}
	return demoExitOK, nil
}

func demoReset(args Args) (map[string]any, error) {
	repoRoot, manifest, err := demoResolveRepo(args, true)
	if err != nil {
		return nil, err
	}
	stateRoot := demoEnsureStateRoot(repoRoot)
	exec := demoNewExec()
	exec.Env = append(exec.Env, "TUSKER_STATE_ROOT="+stateRoot)

	plan, err := demoResetPlan(repoRoot, manifest, exec)
	if err != nil {
		return nil, err
	}
	if !args.Bool("yes") {
		return map[string]any{
			"repo": repoRoot, "dry_run": true, "remove": plan.remove, "retain": plan.retain,
			"text": "reset preview (no changes):\nremove:\n  " + strings.Join(plan.remove, "\n  ") + "\nretain:\n  " + strings.Join(plan.retain, "\n  "),
		}, nil
	}
	leftovers, err := demoApplyReset(repoRoot, manifest, exec, plan)
	if err != nil {
		return map[string]any{
			"repo": repoRoot, "dry_run": false, "remaining": leftovers,
			"text": "reset failed partway; remaining state:\n  " + strings.Join(leftovers, "\n  "),
		}, err
	}
	// Re-seed the fixture contracts onto the cleaned vault: same scenario,
	// same options, stable contract identities (wave create reissues the
	// same task/wave IDs for the same authoring input). Freshness lives in
	// attempts:
	// every later run claims new runtime attempt IDs.
	fresh, err := demoReseed(repoRoot, manifest, exec)
	if err != nil {
		return map[string]any{
			"repo": repoRoot, "dry_run": false, "remaining": []string{"reseed: " + err.Error()},
			"text": "reset cleaned the vault but reseeding failed: " + err.Error(),
		}, err
	}
	return map[string]any{
		"repo": repoRoot, "dry_run": false, "removed": plan.remove, "waves": fresh.waves, "tasks": fresh.tasks,
		"runtime_project_id": fresh.runtimeProjectID, "runtime_problem": fresh.runtimeProblem,
		"text": fmt.Sprintf("reset applied: removed %d demo-owned paths, reseeded an equivalent scenario (%d waves, %d tasks, stable contract identities); later runs record fresh attempt IDs",
			len(plan.remove), len(fresh.waves), len(fresh.tasks)),
	}, nil
}

type demoResetPlanData struct {
	remove []string
	retain []string
	tasks  []demoTaskRecord
	waves  []demoWaveRecord
}

func demoResetPlan(repoRoot string, manifest *demoManifest, exec *demoExec) (*demoResetPlanData, error) {
	plan := &demoResetPlanData{}
	active := []string{}
	for _, key := range demoSortedTaskKeys(manifest) {
		rec := manifest.Tasks[key]
		plan.tasks = append(plan.tasks, rec)
		if lease := demoActiveLease(exec, repoRoot, rec.TaskID); lease != "none" {
			active = append(active, key+"("+lease+")")
		}
	}
	if len(active) > 0 {
		return nil, tuskerError(demoCodeActiveRuns, "refusing reset with active runs: "+strings.Join(active, ", "),
			withHint("stop them first with `tusker runs interrupt <task>` or `tusker work release <task> --by <owner>`, then rerun reset"))
	}
	for _, name := range demoSortedWaveNames(manifest) {
		plan.waves = append(plan.waves, manifest.Waves[name])
	}
	seen := map[string]bool{}
	add := func(path string) {
		if path == "" || seen[path] {
			return
		}
		seen[path] = true
		plan.remove = append(plan.remove, path)
	}
	vaultPath := demoVaultPath(repoRoot)
	for _, rec := range plan.tasks {
		add(filepath.Join(vaultPath, "work", "tasks", rec.TaskID+".md"))
		add(filepath.Join(repoRoot, rec.Artifact))
		add(filepath.Join(vaultPath, "scratch", rec.TaskID))
		add(filepath.Join(vaultPath, "evidence", rec.TaskID))
	}
	// Static seed-owned files (sample project, knowledge corpus) are removed
	// and rewritten by reseed, so reset restores an equivalent scenario.
	for _, rel := range manifest.CreatedPaths {
		add(filepath.Join(repoRoot, filepath.FromSlash(rel)))
	}
	for _, wave := range plan.waves {
		add(filepath.Join(vaultPath, "work", "waves", wave.WaveID+".md"))
		add(filepath.Join(vaultPath, "work", "epics", wave.Epic+".md"))
	}
	plan.retain = []string{
		filepath.Join(vaultPath, "specs", "demo-parallel-waves.md"),
		demoDir(repoRoot),
		"run ledger entries in manifest.json (" + fmt.Sprintf("%d", len(manifest.Runs)) + " runs kept)",
		"runtime history in the state root (attempts stay inspectable)",
	}
	if manifest.RuntimeRegistered && !manifest.RuntimeStripScope {
		plan.retain = append(plan.retain, "runtime project registration shared with the local scope (kept: "+manifest.RuntimeProjectID+")")
	}
	sort.Strings(plan.remove)
	return plan, nil
}

func demoApplyReset(repoRoot string, manifest *demoManifest, exec *demoExec, plan *demoResetPlanData) ([]string, error) {
	var leftovers []string
	vaultPath := demoVaultPath(repoRoot)
	for _, path := range plan.remove {
		if !demoOwnedPath(repoRoot, path) {
			leftovers = append(leftovers, path+": refused (outside demo repo)")
			continue
		}
		if err := os.RemoveAll(path); err != nil {
			leftovers = append(leftovers, path+": "+err.Error())
		}
	}
	// Drop demo worktree registrations and task branches, then re-prune.
	if out, err := demoGit(repoRoot, "worktree", "list", "--porcelain"); err == nil {
		for _, line := range strings.Split(out, "\n") {
			dir := strings.TrimSpace(strings.TrimPrefix(line, "worktree "))
			if dir == "" || dir == repoRoot {
				continue
			}
			if strings.HasPrefix(dir, filepath.Join(stateRootOf(repoRoot), "workspaces")+string(filepath.Separator)) {
				_, _ = demoGit(repoRoot, "worktree", "remove", "--force", dir)
			}
		}
	}
	_, _ = demoGit(repoRoot, "worktree", "prune")
	for _, rec := range plan.tasks {
		_, _ = demoGit(repoRoot, "branch", "-D", "task/"+rec.TaskID)
	}
	// Remove the manifest-owned default-scope project registration so the
	// normal UI no longer lists the disposable repo. An already-absent
	// registration is success; anything else is a reportable leftover. When
	// the runtime scope shares the local store (caller override), the two
	// registrations are one row that retained workspace metadata references,
	// so it is kept: removing it would orphan those workspaces.
	if manifest.RuntimeRegistered && manifest.RuntimeProjectID != "" && manifest.RuntimeStripScope {
		scope := firstNonEmpty(manifest.RuntimeScope, "default")
		if err := demoRemoveRuntimeProject(repoRoot, exec, vaultPath, scope, manifest.RuntimeStripScope, manifest.RuntimeProjectID); err != nil {
			leftovers = append(leftovers, "runtime registration "+manifest.RuntimeProjectID+": "+err.Error())
		}
	}
	// Remove demo decisions by title match. They are seed-created records with
	// no dependents; the match is exact on the fixture title so foreign
	// decisions are never touched.
	if entries, err := os.ReadDir(filepath.Join(vaultPath, "work", "decisions")); err == nil {
		for _, entry := range entries {
			path := filepath.Join(vaultPath, "work", "decisions", entry.Name())
			raw, err := os.ReadFile(path)
			if err != nil {
				leftovers = append(leftovers, path+": "+err.Error())
				continue
			}
			if strings.Contains(string(raw), demoDecisionTitle) {
				if err := os.Remove(path); err != nil {
					leftovers = append(leftovers, path+": "+err.Error())
				}
			}
		}
	}
	if len(leftovers) > 0 {
		return leftovers, tuskerError(demoCodePrecondition, "reset cleanup incomplete")
	}
	return nil, nil
}

func stateRootOf(repoRoot string) string {
	if explicit := strings.TrimSpace(os.Getenv("TUSKER_STATE_ROOT")); explicit != "" {
		return explicit
	}
	return filepath.Join(repoRoot, ".tusker", "runtime-state")
}

// demoOwnedPath confines deletes to the marker repo: absolute, symlink-free,
// and strictly inside the canonical root.
func demoOwnedPath(repoRoot, path string) bool {
	candidate := demoResolveDeep(path)
	if candidate == "" {
		return false
	}
	root := demoResolveDeep(repoRoot)
	if root == "" {
		return false
	}
	return candidate == root || strings.HasPrefix(candidate, root+string(filepath.Separator))
}

// demoResolveDeep canonicalizes the deepest existing ancestor and rejoins
// the remainder, so ownership holds for not-yet-created demo paths too.
func demoResolveDeep(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return ""
	}
	probe := abs
	var tail []string
	for {
		if resolved, err := filepath.EvalSymlinks(probe); err == nil {
			out := resolved
			for i := len(tail) - 1; i >= 0; i-- {
				out = filepath.Join(out, tail[i])
			}
			return out
		}
		parent := filepath.Dir(probe)
		if parent == probe {
			return ""
		}
		tail = append(tail, filepath.Base(probe))
		probe = parent
	}
}

type demoReseedResult struct {
	waves            map[string]string
	tasks            map[string]string
	runtimeProjectID string
	runtimeProblem   string
}

func demoReseed(repoRoot string, manifest *demoManifest, exec *demoExec) (*demoReseedResult, error) {
	vaultPath := demoVaultPath(repoRoot)
	actor := "agent:demo-seed"
	// Rewrite the static seed-owned files removed by reset so the reseeded
	// scenario is equivalent (sample project, knowledge corpus).
	created, err := demoSeedRealWorkFiles(repoRoot)
	if err != nil {
		return nil, err
	}
	manifest.CreatedPaths = created
	result := &demoReseedResult{waves: map[string]string{}, tasks: map[string]string{}}
	mappings := map[string]map[string]string{}
	for _, wave := range demoFixtureWaves() {
		mapping, waveID, err := demoAuthorWave(exec, repoRoot, vaultPath, wave, manifest.HumanGate && wave.Name == "follow-up", actor)
		if err != nil {
			return nil, err
		}
		mappings[wave.Name] = mapping
		record := manifest.Waves[wave.Name]
		record.WaveID = waveID
		record.Members = nil
		for _, task := range wave.Tasks {
			taskID, ok := mapping[task.Key]
			if !ok || taskID == "" {
				return nil, tuskerError(demoCodePrecondition, "wave create did not map task "+task.Key)
			}
			record.Members = append(record.Members, taskID)
			rec := manifest.Tasks[task.Key]
			rec.TaskID = taskID
			rec.Complexity = task.Complexity
			manifest.Tasks[task.Key] = rec
			result.tasks[task.Key] = taskID
		}
		manifest.Waves[wave.Name] = record
		result.waves[wave.Name] = waveID
	}
	if err := demoApplyCrossScopeDeps(exec, repoRoot, vaultPath, mappings, actor); err != nil {
		return nil, err
	}
	for _, task := range manifest.Tasks {
		if len(task.Deps) > 0 {
			continue
		}
		if _, err := exec.run(repoRoot, "status", task.TaskID, "ready", "--reason", "demo reset: wave ready to start", "--vault", vaultPath); err != nil {
			return nil, err
		}
	}
	// Re-register the default-scope project removed by reset so the normal
	// UI keeps discovering the disposable repo. Best-effort like seed: when
	// the scope is unavailable, reseed still restores data and reports the
	// missing step instead of failing the reset. Shared-scope registrations
	// were never removed; just re-ensure the local row they point at.
	if manifest.RuntimeStripScope {
		scope := firstNonEmpty(manifest.RuntimeScope, "default")
		runtimeID, err := demoRegisterRuntimeProject(repoRoot, exec, vaultPath, scope, manifest.RuntimeStripScope)
		if err != nil {
			manifest.RuntimeRegistered = false
			manifest.RuntimeProblem = "reseed could not restore the runtime project registration: " + err.Error()
			result.runtimeProblem = manifest.RuntimeProblem
		} else {
			manifest.RuntimeProjectID = runtimeID
			manifest.RuntimeRegistered = true
			manifest.RuntimeProblem = ""
			result.runtimeProjectID = runtimeID
		}
	} else {
		localID, err := demoRegisterProject(repoRoot, exec, vaultPath)
		if err != nil {
			return nil, err
		}
		manifest.LocalProjectID = localID
		manifest.RuntimeProjectID = localID
		manifest.RuntimeRegistered = true
		manifest.RuntimeProblem = ""
		result.runtimeProjectID = localID
	}
	manifest.RunsAtReset = len(manifest.Runs)
	if err := demoSaveManifest(repoRoot, manifest); err != nil {
		return nil, err
	}
	return result, nil
}
