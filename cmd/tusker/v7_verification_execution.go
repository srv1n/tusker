package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"gopkg.in/yaml.v3"
)

// Verification commands are gate work, not worker attestations. Each check
// gets a finite timeout so a broken command cannot wedge accept/close
// forever; WORKFLOW.md verification.timeout replaces the default.
const v7VerificationCommandTimeout = 10 * time.Minute
const v7VerificationCommandMaxRows = 16
const v7VerificationCommandMaxOutput = 64 << 10

type v7VerificationExecutionFailure struct {
	Row     v7VerificationRow
	Message string
}

const v7VerificationReceiptSchema = "tusker.verification-receipt/v1"

type v7VerificationReceiptIdentity struct {
	ContractFingerprint string
	WorkRevision        int
	SourceRevision      string
	MaterialFingerprint string
}

// v7VerificationCommandTimeoutFor returns the per-row timeout: the repo's
// limit, lowered by the internal/test-facing verification-timeout-ms knob.
func v7VerificationCommandTimeoutFor(args Args, limit time.Duration) time.Duration {
	if args != nil {
		if ms := atoiSafe(strings.TrimSpace(args.String("verification-timeout-ms"))); ms > 0 && time.Duration(ms)*time.Millisecond < limit {
			return time.Duration(ms) * time.Millisecond
		}
	}
	return limit
}

type v7VerificationSettings struct {
	timeout      time.Duration
	env          []string
	testCommands []*regexp.Regexp
}

var v7VerificationSettingsCache = struct {
	sync.Mutex
	m map[string]v7CachedVerificationSettings
}{m: map[string]v7CachedVerificationSettings{}}

type v7CachedVerificationSettings struct {
	size     int64
	modified time.Time
	settings v7VerificationSettings
}

// v7VerificationSettingsFor reads WORKFLOW.md's verification section, cached
// on the file's size and mtime because proof reports consult it on every
// read. A missing or unreadable section keeps the defaults; tusker check
// reports an invalid one.
func v7VerificationSettingsFor(vaultPath string) v7VerificationSettings {
	settings := v7VerificationSettings{timeout: v7VerificationCommandTimeout}
	if strings.TrimSpace(vaultPath) == "" {
		return settings
	}
	path := workflowPath(vaultPath)
	info, err := os.Stat(path)
	if err != nil {
		return settings
	}
	v7VerificationSettingsCache.Lock()
	cached, ok := v7VerificationSettingsCache.m[path]
	v7VerificationSettingsCache.Unlock()
	if ok && cached.size == info.Size() && cached.modified.Equal(info.ModTime()) {
		return cached.settings
	}
	if text, err := readText(path); err == nil {
		if data, _, err := parseFrontmatter(text); err == nil {
			var policy VerificationPolicy
			if raw, err := yaml.Marshal(data["verification"]); err == nil && yaml.Unmarshal(raw, &policy) == nil {
				if d, err := time.ParseDuration(strings.TrimSpace(policy.Timeout)); err == nil && d > 0 {
					settings.timeout = d
				}
				settings.env = policy.Env
				for _, pattern := range policy.TestCommands {
					if re, err := regexp.Compile(pattern); err == nil {
						settings.testCommands = append(settings.testCommands, re)
					}
				}
			}
		}
	}
	v7VerificationSettingsCache.Lock()
	v7VerificationSettingsCache.m[path] = v7CachedVerificationSettings{size: info.Size(), modified: info.ModTime(), settings: settings}
	v7VerificationSettingsCache.Unlock()
	return settings
}

// v7TaskVault is the vault a loaded task note came from, or "" for a note
// built in memory.
func v7TaskVault(task Note) string {
	abs, rel := filepath.ToSlash(task.AbsolutePath), task.RelativePath
	if abs == "" || rel == "" || !strings.HasSuffix(abs, "/"+rel) {
		return ""
	}
	return filepath.FromSlash(strings.TrimSuffix(abs, "/"+rel))
}

// v7RepoTestCommand reports whether command matches one of the repo's
// declared test wrappers (WORKFLOW.md verification.test_commands).
func v7RepoTestCommand(task Note, command string) bool {
	for _, re := range v7VerificationSettingsFor(v7TaskVault(task)).testCommands {
		if re.MatchString(command) {
			return true
		}
	}
	return false
}

// executeV7CommandVerificationRows is the shared gate seam used by accept,
// direct close, and the authoritative review/completion path. It reloads the
// task under the proof lock, executes pending commands from the canonical
// repository root, records bounded receipts, and returns failures before any
// lifecycle transition. Reviews supply a separately bound workspace below.
// Manual-proof rows are deliberately left byte-for-byte untouched.
func executeV7CommandVerificationRows(vaultPath string, task Note, args Args, actor string, trustedWorker bool) (Note, v7ProofReport, []v7VerificationExecutionFailure, error) {
	return executeV7CommandVerificationRowsInWorkspace(vaultPath, task, args, actor, trustedWorker, nil)
}

// A workspace target is supplied only by the trusted review coordinator after
// resolving the durable execute parent. It is never selected by a CLI flag.
type v7VerificationWorkspace struct {
	Path                string
	Verify              func() error
	MaterialFingerprint string
	// Submission, when set, is the execute submission whose own workspace and
	// scope a passing Verify proves still hold MaterialFingerprint.
	Submission *RunAttempt
}

func executeV7CommandVerificationRowsInWorkspace(vaultPath string, task Note, args Args, actor string, trustedWorker bool, workspace *v7VerificationWorkspace) (Note, v7ProofReport, []v7VerificationExecutionFailure, error) {
	taskID := stringField(task.Data, "id")
	if taskID == "" {
		return task, v7ProofReport{}, nil, tuskerError(errorInvalidArg, "verification execution requires a task id")
	}
	var fresh Note
	var report v7ProofReport
	var failures []v7VerificationExecutionFailure
	retry := "tusker done " + taskID
	err := withV7ProofWriteLock(vaultPath, taskID, args, retry, func() error {
		current, err := resolveV7Note(vaultPath, taskID, "task")
		if err != nil {
			return err
		}
		data, body, err := parseFrontmatterMustRead(current.AbsolutePath)
		if err != nil {
			return err
		}
		current.Data, current.Body = data, body
		expectedRev := stringField(task.Data, "state_rev")
		if expectedRev == "" || stringField(data, "state_rev") != expectedRev || !v7StateRevMatches(data, body, expectedRev) {
			return tuskerError("CAS_CONFLICT", taskID+": verification manifest task snapshot drifted before execution")
		}
		rows := parseV7VerificationRows(body)
		// Rows reset only because the work moved under an unchanged, already
		// accepted command rerun without a fresh manifest confirmation.
		staleOnly := 0
		if args.Bool("rerun-invalid") {
			material, materialErr := v7VerificationCurrentScopedMaterial(vaultPath, current)
			if workspace != nil {
				material, materialErr = workspace.MaterialFingerprint, workspace.Verify()
			}
			for i := range rows {
				if cause := v7VerificationReceiptInvalidationForMaterial(current, rows[i], material, materialErr); cause != nil {
					rows[i].Result = "pending"
					if v7VerificationRerunsWithoutConfirmation(cause) {
						staleOnly++
					}
				}
			}
		}
		manifest, pending := v7VerificationManifest(data, rows)
		if len(pending) > v7VerificationCommandMaxRows {
			return tuskerError(errorEvidenceGate, fmt.Sprintf("%s: verification manifest has %d pending command rows; maximum is %d", taskID, len(pending), v7VerificationCommandMaxRows))
		}
		if len(pending) == 0 {
			idx, err := loadV7Index(vaultPath)
			if err != nil {
				return err
			}
			material, err := v7VerificationMaterialForReport(vaultPath, current, workspace)
			if err != nil {
				return err
			}
			fresh = current
			report = computeV7ProofReportForMaterial(vaultPath, current, idx, material, nil)
			return nil
		}
		if len(pending) > staleOnly && !trustedWorker {
			wf := defaultWorkflow()
			if fileExists(workflowPath(vaultPath)) {
				loaded, workflowErr := loadWorkflow(vaultPath)
				if workflowErr != nil {
					return workflowErr
				}
				wf = loaded.Data
			}
			configured := reviewerActorForNote(wf.Reviewer.Actor, current)
			if actor != configured {
				return tuskerError(errorInvalidTransition, taskID+": verification executor is not the configured reviewer", withContext(map[string]any{"actor": actor, "configured_reviewer": configured}))
			}
			confirmed := strings.TrimSpace(firstNonEmpty(args.String("confirm-verification"), args.String("confirm")))
			if confirmed == "" {
				return tuskerError(errorMissingArg, taskID+": command verification requires explicit manifest confirmation", withHint("rerun with --confirm "+manifest), withContext(map[string]any{"verification_manifest": manifest}))
			}
			if confirmed != manifest {
				return tuskerError(errorInvalidTransition, taskID+": verification manifest changed after confirmation", withHint("review the exact pending command rows and confirm "+manifest), withContext(map[string]any{"confirmed": confirmed, "verification_manifest": manifest}))
			}
		}
		root := v7RepoRoot(vaultPath)
		if workspace != nil {
			if workspace.Verify == nil {
				return tuskerError(errorInvalidTransition, "verification workspace lacks an implementation binding")
			}
			if err := workspace.Verify(); err != nil {
				return err
			}
			root = workspace.Path
		}
		repoRoot, err := canonicalV7VerificationWorkspaceRoot(root)
		if err != nil {
			return err
		}
		identity, verifyMaterial, err := v7VerificationReceiptIdentityFor(vaultPath, current, repoRoot, workspace)
		if err != nil {
			return err
		}
		settings := v7VerificationSettingsFor(vaultPath)
		timeout := v7VerificationCommandTimeoutFor(args, settings.timeout)
		progress := !args.Bool("quiet")
		env := v7VerificationCommandEnv(settings.env, progress)
		changed := false
		observations := map[int]v7VerificationCommandObservation{}
		// Rows naming the same command share one run of it.
		// ponytail: assumes a repeated check sees the same tree, which a
		// command editing its own inputs would break.
		ran := map[string]v7VerificationCommandObservation{}
		treeChanges := map[string][]string{}
		for i, row := range rows {
			if !strings.EqualFold(strings.TrimSpace(row.Result), "pending") {
				continue
			}
			command, ok := v7VerificationCommand(row.Check)
			if !ok {
				continue
			}
			// Shell checks may read tracker state, ignored files, or external
			// inputs. A source-tree hash cannot safely share their observations
			// across runs, only between rows of this one.
			observed, seen := ran[command]
			if !seen {
				if progress {
					fmt.Fprintf(os.Stderr, "%s %s: running %s\n", taskID, fallback(strings.TrimSpace(row.CoverText), "check"), command)
				}
				before, beforeOK := v7CheckoutSnapshot(repoRoot)
				observed, _ = runV7VerificationCommand(repoRoot, command, timeout, env)
				after, afterOK := v7CheckoutSnapshot(repoRoot)
				if beforeOK && afterOK {
					treeChanges[command] = v7CheckoutChangedPaths(before, after)
				}
				ran[command] = observed
				if progress {
					fmt.Fprintf(os.Stderr, "%s %s: %s (%s)\n", taskID, fallback(strings.TrimSpace(row.CoverText), "check"), observed.Message, observed.FinishedAt.Sub(observed.StartedAt).Round(time.Second))
				}
			}
			rows[i].Result = "pass"
			if observed.ExitCode != 0 {
				rows[i].Result = "fail"
				message := observed.Message
				if paths := treeChanges[command]; len(paths) > 0 {
					// ponytail: a failing check that edits tracked files itself also counts as a tree change.
					rows[i].Result = "pending"
					if len(paths) > 5 {
						paths = paths[:5]
					}
					message += "; the checkout changed while it ran (" + strings.Join(paths, ", ") + "), so the result was not recorded; rerun when the tree is quiet"
				}
				if saved := v7SaveFailedCheckOutput(vaultPath, taskID, i+1, observed.Tail); saved != "" {
					message += "; output saved to " + saved
				}
				failures = append(failures, v7VerificationExecutionFailure{Row: rows[i], Message: message})
			}
			if rows[i].Result == "pending" {
				continue
			}
			observations[i] = observed
			rows[i].BlockedBy = ""
			changed = true
		}
		if !changed {
			idx, err := loadV7Index(vaultPath)
			if err != nil {
				return err
			}
			material, err := v7VerificationMaterialForReport(vaultPath, current, workspace)
			if err != nil {
				return err
			}
			fresh = current
			report = computeV7ProofReportForMaterial(vaultPath, current, idx, material, nil)
			return nil
		}
		// Review commands may not change the implementation under review.
		// Direct commands bind their receipt to the resulting scoped material.
		if err := verifyMaterial(); err != nil {
			return err
		}
		identity, _, err = v7VerificationReceiptIdentityFor(vaultPath, current, repoRoot, workspace)
		if err != nil {
			return err
		}
		for i, observed := range observations {
			rows[i].Notes = appendV7VerificationExecutionNote(rows[i], observed, identity)
		}
		body = updateV7VerificationLedger(body, rows)
		idx, err := loadV7Index(vaultPath)
		if err != nil {
			return err
		}
		current.Body = body
		report = computeV7ProofReportForMaterial(vaultPath, current, idx, identity.MaterialFingerprint, nil)
		if report.Status == "satisfied" && len(v7PacketStubAcceptanceItems(body)) > 0 && len(v7AcceptanceWaivers(data)) == 0 {
			report.Status = "partial"
		}
		data["proof_status"] = report.Status
		data["updated_at"] = time.Now().UTC().Format(time.RFC3339Nano)
		data["updated_by"] = fallback(actor, "reviewer:gate")
		if _, err := saveV7DocumentCAS(current.AbsolutePath, data, body, v7FrontmatterOrder["task"], stringField(data, "state_rev")); err != nil {
			return err
		}
		fresh, err = resolveV7Note(vaultPath, taskID, "task")
		if err != nil {
			return err
		}
		fresh.Data, fresh.Body = data, body
		fresh.Data["state_rev"] = stringField(data, "state_rev")
		return nil
	})
	if err != nil {
		return task, v7ProofReport{}, nil, err
	}
	return fresh, report, failures, nil
}

type v7VerificationCommandObservation struct {
	StartedAt  time.Time
	FinishedAt time.Time
	ExitCode   int
	Digest     string
	Message    string
	TimedOut   bool
	Truncated  bool
	MatchCount int
	// Tail is the end of the raw output. It is never written to the task.
	Tail []byte
}

// runV7VerificationCommand runs one check. A nil env is the minimal default.
func runV7VerificationCommand(repoRoot, command string, timeout time.Duration, env []string) (v7VerificationCommandObservation, error) {
	if env == nil {
		env = v7VerificationCommandEnv(nil, false)
	}
	started := time.Now().UTC()
	filtered := v7FilteredTestCommand(command)
	var cmd *exec.Cmd
	if filtered {
		args, ok := v7SupportedFilteredTestArgs(command)
		if !ok {
			message := "filtered test command uses unsupported selector runner or shell syntax; use a single go test -run or cargo test <filter> invocation"
			return v7RejectedVerificationCommandObservation(started, message)
		}
		cmd = exec.Command(args[0], args[1:]...)
	} else {
		cmd = exec.Command("/bin/sh", "-c", command)
	}
	cmd.Dir = filepath.Clean(repoRoot)
	cmd.Env = sharedReviewCargoTargetEnv(env, cmd.Dir, "")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	output := &v7BoundedOutput{limit: v7VerificationCommandMaxOutput}
	cmd.Stdout, cmd.Stderr = output, output
	if err := cmd.Start(); err != nil {
		return v7VerificationCommandObservation{StartedAt: started, FinishedAt: time.Now().UTC(), ExitCode: 1, Message: "verification command failed to start"}, err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	var err error
	timedOut := false
	select {
	case err = <-done:
	case <-time.After(timeout):
		timedOut = true
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
		select {
		case err = <-done:
		case <-time.After(500 * time.Millisecond):
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			err = <-done
		}
	}
	finished := time.Now().UTC()
	outputBytes := output.Bytes()
	digest := sha256.Sum256(outputBytes)
	obs := v7VerificationCommandObservation{StartedAt: started, FinishedAt: finished, Digest: "sha256:" + hex.EncodeToString(digest[:]), Truncated: output.truncated, Tail: output.Tail()}
	if timedOut {
		obs.TimedOut = true
		obs.ExitCode = 124
		obs.Message = fmt.Sprintf("timed out after %s", timeout)
		return obs, errors.New(obs.Message)
	}
	if err == nil {
		if filtered {
			if output.truncated {
				obs.ExitCode = 1
				obs.Message = "filtered test command produced truncated JSON test evidence"
				return obs, errors.New(obs.Message)
			}
			count, parsed := v7FilteredTestMatchCount(command, string(outputBytes))
			obs.MatchCount = count
			if !parsed {
				obs.ExitCode = 1
				obs.Message = "filtered test command produced untrusted test evidence; use go test -json or cargo test"
				return obs, errors.New(obs.Message)
			}
			if count == 0 {
				obs.ExitCode = 1
				obs.Message = "filtered test command produced no matched-test evidence; use verbose or JSON test output"
				return obs, errors.New(obs.Message)
			}
		}
		obs.ExitCode = 0
		obs.Message = "pass"
		return obs, nil
	}
	obs.ExitCode = 1
	if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() >= 0 {
		obs.ExitCode = exitErr.ExitCode()
	}
	obs.Message = fmt.Sprintf("exit status %d", obs.ExitCode)
	return obs, err
}

func v7RejectedVerificationCommandObservation(started time.Time, message string) (v7VerificationCommandObservation, error) {
	finished := time.Now().UTC()
	digest := sha256.Sum256(nil)
	obs := v7VerificationCommandObservation{
		StartedAt:  started,
		FinishedAt: finished,
		ExitCode:   1,
		Digest:     "sha256:" + hex.EncodeToString(digest[:]),
		Message:    message,
	}
	return obs, errors.New(message)
}

func appendV7VerificationExecutionNote(row v7VerificationRow, observation v7VerificationCommandObservation, identity v7VerificationReceiptIdentity) string {
	parts := []string{fmt.Sprintf("tusker gate executed at %s", observation.FinishedAt.Format(time.RFC3339Nano)), fmt.Sprintf("exit=%d", observation.ExitCode), "output_sha256=" + observation.Digest}
	if observation.TimedOut {
		parts = append(parts, "timeout")
	}
	if observation.Truncated {
		parts = append(parts, "output_truncated")
	}
	receiptFields := []string{
		v7VerificationReceiptSchema,
		"contract=" + identity.ContractFingerprint,
		"work_revision=" + strconv.Itoa(identity.WorkRevision),
		"source=" + fallback(identity.SourceRevision, "-"),
		"material=" + identity.MaterialFingerprint,
		"row=" + v7VerificationRowFingerprint(row),
	}
	if v7FilteredTestCommand(row.Check) {
		receiptFields = append(receiptFields, "match_count="+strconv.Itoa(observation.MatchCount))
	}
	parts = append(parts, strings.Join(receiptFields, " "))
	receipt := strings.Join(parts, "; ")
	if strings.TrimSpace(row.Notes) == "" || row.Notes == "-" {
		return receipt
	}
	return strings.TrimSpace(row.Notes) + "; " + receipt
}

func v7FilteredTestCommand(check string) bool {
	command, ok := v7VerificationCommand(check)
	if !ok {
		command = check
	}
	lower := strings.ToLower(command)
	if strings.Contains(lower, "go test") && (strings.Contains(lower, "-run ") || strings.Contains(lower, "-run=")) {
		return true
	}
	return v7UnsupportedTestSelectorCommand(command)
}

func v7FilteredTestMatchCount(command, output string) (int, bool) {
	if !v7FilteredTestCommand(command) {
		return 0, false
	}
	if args, ok := v7SupportedCargoTestArgs(command); ok && args[0] == "cargo" {
		return v7CargoTestMatchCount(output)
	}
	count, parsed := v7GoTestJSONMatchCount(output)
	return count, parsed
}

// v7CargoTestMatchCount sums the passed counts on libtest's per-binary
// "test result:" summary lines. Test output is captured unless --nocapture or
// --show-output is set, and both are refused, so a test cannot print a fake
// summary.
func v7CargoTestMatchCount(output string) (int, bool) {
	count, saw := 0, false
	for _, line := range strings.Split(output, "\n") {
		rest, ok := strings.CutPrefix(strings.TrimSpace(line), "test result: ")
		if !ok {
			continue
		}
		fields := strings.Fields(rest)
		if len(fields) < 3 || fields[2] != "passed;" {
			return 0, false
		}
		n, err := strconv.Atoi(fields[1])
		if err != nil {
			return 0, false
		}
		count, saw = count+n, true
	}
	return count, saw
}

// v7SupportedFilteredTestArgs is the argv for a filtered test the executor can
// count matches for: go test -run, or cargo test with a filter.
func v7SupportedFilteredTestArgs(command string) ([]string, bool) {
	if args, ok := v7SupportedGoTestArgs(command); ok {
		return args, true
	}
	return v7SupportedCargoTestArgs(command)
}

// v7SupportedCargoTestArgs accepts one direct cargo test invocation, run
// without a shell. Flags that uncapture test output are refused because they
// would let a test print its own summary line.
func v7SupportedCargoTestArgs(command string) ([]string, bool) {
	if len(v7ShellCommandSegments(command)) != 1 {
		return nil, false
	}
	args, ok := v7VerificationCommandFields(command)
	if !ok || len(args) < 3 || args[0] != "cargo" || args[1] != "test" {
		return nil, false
	}
	for _, arg := range args[2:] {
		if arg == "--nocapture" || arg == "--show-output" {
			return nil, false
		}
	}
	return args, true
}

func v7GoTestJSONMatchCount(output string) (int, bool) {
	count := 0
	sawEvent := false
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var event struct {
			Action string `json:"Action"`
			Test   string `json:"Test"`
		}
		if err := json.Unmarshal([]byte(line), &event); err != nil || strings.TrimSpace(event.Action) == "" {
			return 0, false
		}
		sawEvent = true
		if event.Action == "run" && strings.TrimSpace(event.Test) != "" {
			count++
		}
	}
	return count, sawEvent
}

// v7SupportedGoTestArgs is the only selector form for which the executor can
// establish non-zero matches. It intentionally rejects shell composition and
// wrappers so output from another process cannot impersonate go test events.
func v7SupportedGoTestArgs(command string) ([]string, bool) {
	segments := v7ShellCommandSegments(command)
	if len(segments) != 1 {
		return nil, false
	}
	args, ok := v7VerificationCommandFields(command)
	if !ok || len(args) < 3 || args[0] != "go" || args[1] != "test" {
		return nil, false
	}
	hasRun, hasJSON := false, false
	for i := 2; i < len(args); i++ {
		switch {
		case args[i] == "-run":
			if hasRun || i+1 >= len(args) || args[i+1] == "" {
				return nil, false
			}
			hasRun = true
			i++
		case strings.HasPrefix(args[i], "-run="):
			if hasRun || strings.TrimPrefix(args[i], "-run=") == "" {
				return nil, false
			}
			hasRun = true
		case args[i] == "-json" || strings.HasPrefix(args[i], "-json="):
			hasJSON = true
		}
	}
	if !hasRun {
		return nil, false
	}
	if !hasJSON {
		args = append(args[:2], append([]string{"-json"}, args[2:]...)...)
	}
	return args, true
}

// v7VerificationCommandFields is a small shell-word parser for the controlled
// go test path. It preserves case in selectors while refusing shell operators,
// unquoted expansions, and unterminated quoting.
func v7VerificationCommandFields(command string) ([]string, bool) {
	var fields []string
	var current strings.Builder
	var quote rune
	escaped := false
	hasToken := false
	flush := func() {
		if hasToken {
			fields = append(fields, current.String())
			current.Reset()
			hasToken = false
		}
	}
	for _, r := range command {
		if escaped {
			current.WriteRune(r)
			hasToken = true
			escaped = false
			continue
		}
		if quote != 0 {
			if r == quote {
				quote = 0
				continue
			}
			current.WriteRune(r)
			hasToken = true
			continue
		}
		switch r {
		case '\'', '"':
			quote = r
			hasToken = true
		case '\\':
			escaped = true
			hasToken = true
		case ' ', '\t':
			flush()
		default:
			if strings.ContainsRune("&;|><`$()\n\r", r) {
				return nil, false
			}
			current.WriteRune(r)
			hasToken = true
		}
	}
	if quote != 0 || escaped {
		return nil, false
	}
	flush()
	return fields, len(fields) > 0
}

func v7UnsupportedTestSelectorCommand(command string) bool {
	for _, invocation := range v7ShellCommandInvocations(command) {
		if !v7UnsupportedTestRunner(invocation) || !v7UnsupportedTestSelector(invocation) {
			continue
		}
		return true
	}
	return false
}

func v7UnsupportedTestRunner(invocation v7ShellCommandInvocation) bool {
	switch invocation.name {
	case "cargo", "swift", "dotnet", "pytest", "jest", "vitest":
		return v7ArgsContain(invocation.args, "test") || invocation.name == "pytest" || invocation.name == "jest" || invocation.name == "vitest"
	case "python", "python3":
		for i := 0; i+1 < len(invocation.args); i++ {
			if invocation.args[i] == "-m" && (invocation.args[i+1] == "pytest" || invocation.args[i+1] == "unittest") {
				return true
			}
		}
	case "npm", "pnpm", "yarn", "bun":
		return v7ArgsContain(invocation.args, "test") || v7ArgsContain(invocation.args, "run:test")
	case "npx":
		return v7ArgsContain(invocation.args, "jest") || v7ArgsContain(invocation.args, "vitest")
	}
	return false
}

func v7UnsupportedTestSelector(invocation v7ShellCommandInvocation) bool {
	for _, arg := range invocation.args {
		lower := strings.ToLower(arg)
		for _, flag := range []string{"-k", "--filter", "--grep", "--test-filter", "--testnamepattern", "--test-name-pattern"} {
			if lower == flag || strings.HasPrefix(lower, flag+"=") {
				return true
			}
		}
	}
	if invocation.name == "cargo" {
		seenTest := false
		for _, arg := range invocation.args {
			if !seenTest {
				seenTest = arg == "test"
				continue
			}
			if arg == "--" || !strings.HasPrefix(arg, "-") {
				return true
			}
		}
	}
	return false
}

func v7VerificationRowFingerprint(row v7VerificationRow) string {
	raw, _ := json.Marshal(v7VerificationManifestRow{Covers: strings.TrimSpace(row.CoverText), Command: strings.TrimSpace(row.Check)})
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func v7VerificationMaterialForReport(vaultPath string, task Note, workspace *v7VerificationWorkspace) (string, error) {
	if workspace == nil {
		return v7VerificationCurrentScopedMaterial(vaultPath, task)
	}
	if workspace.Verify == nil {
		return "", tuskerError(errorInvalidTransition, "verification workspace lacks an implementation binding")
	}
	if err := workspace.Verify(); err != nil {
		return "", err
	}
	repoRoot, err := canonicalV7VerificationWorkspaceRoot(workspace.Path)
	if err != nil {
		return "", err
	}
	identity, _, err := v7VerificationReceiptIdentityFor(vaultPath, task, repoRoot, workspace)
	if err != nil {
		return "", err
	}
	return identity.MaterialFingerprint, nil
}

func v7VerificationReceiptIdentityFor(vaultPath string, task Note, repoRoot string, workspace *v7VerificationWorkspace) (v7VerificationReceiptIdentity, func() error, error) {
	contract := directWaveTaskContractFingerprint(task.Data, task.Body)
	if stored := strings.TrimSpace(stringField(task.Data, "contract_fingerprint")); stored == "" || stored != contract {
		return v7VerificationReceiptIdentity{}, nil, tuskerError(errorEvidenceGate, stringField(task.Data, "id")+": verification task contract fingerprint is stale")
	}
	scope, err := canonicalTaskMaterialScope(vaultPath, task)
	if err != nil {
		return v7VerificationReceiptIdentity{}, nil, err
	}
	generatedOutputScope, err := taskGeneratedOutputScope(task)
	if err != nil {
		return v7VerificationReceiptIdentity{}, nil, err
	}
	if len(scope) == 0 {
		scope = nil
	}
	// When the commands run in the submission's own workspace on its own scope,
	// a passing Verify already proves the material; hashing it again adds nothing.
	proven := v7VerificationWorkspaceProvesMaterial(workspace, repoRoot, scope, generatedOutputScope)
	material := ""
	if proven {
		material = workspace.MaterialFingerprint
	} else if material, err = workspaceTreeStateHashForPaths(repoRoot, scope, generatedOutputScope); err != nil {
		return v7VerificationReceiptIdentity{}, nil, err
	}
	if workspace != nil && strings.TrimSpace(workspace.MaterialFingerprint) != "" && material != workspace.MaterialFingerprint {
		return v7VerificationReceiptIdentity{}, nil, tuskerError(errorEvidenceGate, "verification workspace material does not match the reviewed implementation")
	}
	verify := func() error {
		if workspace == nil {
			return nil
		}
		if err := workspace.Verify(); err != nil {
			return err
		}
		if proven {
			return nil
		}
		current, hashErr := workspaceTreeStateHashForPaths(repoRoot, scope, generatedOutputScope)
		if hashErr != nil {
			return hashErr
		}
		if current != material {
			return tuskerError(errorEvidenceGate, "verification command changed the scoped implementation material")
		}
		return nil
	}
	return v7VerificationReceiptIdentity{
		ContractFingerprint: directWaveTaskCheckContractFingerprint(task.Data, task.Body),
		WorkRevision:        intField(task.Data, "work_revision"),
		SourceRevision:      firstNonEmpty(stringField(task.Data, "source_sha"), stringField(task.Data, "source_commit")),
		MaterialFingerprint: material,
	}, verify, nil
}

func v7VerificationWorkspaceProvesMaterial(workspace *v7VerificationWorkspace, repoRoot string, scope, generatedOutputScope []string) bool {
	if workspace == nil || workspace.Submission == nil || strings.TrimSpace(workspace.MaterialFingerprint) == "" ||
		workspace.MaterialFingerprint != workspace.Submission.EndState.MaterialFingerprint ||
		strings.Join(scope, "\x00") != strings.Join(workspace.Submission.EndState.MaterialScope, "\x00") ||
		strings.Join(generatedOutputScope, "\x00") != strings.Join(workspace.Submission.EndState.GeneratedOutputScope, "\x00") {
		return false
	}
	root, err := canonicalV7VerificationWorkspaceRoot(workspace.Submission.WorkspacePath)
	return err == nil && root == repoRoot
}

func v7VerificationReceiptCurrent(task Note, row v7VerificationRow, currentMaterial string, materialErr error) bool {
	if _, command := v7VerificationCommand(row.Check); !command {
		return true
	}
	if !strings.EqualFold(strings.TrimSpace(row.Result), "pass") || materialErr != nil || strings.TrimSpace(currentMaterial) == "" {
		return false
	}
	marker := v7VerificationReceiptSchema + " "
	pos := strings.LastIndex(row.Notes, marker)
	if pos < 0 {
		return false
	}
	fields := map[string]string{}
	for _, token := range strings.Fields(row.Notes[pos+len(marker):]) {
		key, value, ok := strings.Cut(strings.TrimRight(token, ";"), "=")
		if ok {
			fields[key] = value
		}
	}
	contract := directWaveTaskContractFingerprint(task.Data, task.Body)
	stored := strings.TrimSpace(stringField(task.Data, "contract_fingerprint"))
	if stored == "" || stored != contract || fields["contract"] != v7VerificationReceiptContract(task, fields["contract"]) || fields["row"] != v7VerificationRowFingerprint(row) {
		return false
	}
	if fields["work_revision"] != strconv.Itoa(intField(task.Data, "work_revision")) || fields["source"] != fallback(firstNonEmpty(stringField(task.Data, "source_sha"), stringField(task.Data, "source_commit")), "-") || fields["material"] != currentMaterial {
		return false
	}
	if v7FilteredTestCommand(row.Check) {
		command, _ := v7VerificationCommand(row.Check)
		if _, ok := v7SupportedFilteredTestArgs(command); !ok {
			return false
		}
		count, err := strconv.Atoi(fields["match_count"])
		return err == nil && count > 0
	}
	return true
}

type v7VerificationInvalidation struct {
	Kind        string `json:"kind" yaml:"kind"`
	Dimension   string `json:"dimension" yaml:"dimension"`
	Previous    string `json:"previous,omitempty" yaml:"previous,omitempty"`
	Current     string `json:"current,omitempty" yaml:"current,omitempty"`
	NextActor   string `json:"nextActor" yaml:"nextActor"`
	Recovery    string `json:"recovery" yaml:"recovery"`
	Explanation string `json:"explanation" yaml:"explanation"`
}

func v7VerificationReceiptInvalidationForWorkspace(vaultPath string, task Note, workspace *v7VerificationWorkspace) *v7VerificationInvalidation {
	return v7VerificationReceiptInvalidationLazy(task, func() (string, error) {
		return v7VerificationMaterialForReport(vaultPath, task, workspace)
	})
}

// v7VerificationReceiptInvalidationLazy reads scoped material (git hashing)
// only when a row reaches the material comparison: a command row with a
// passing, receipted result. Other rows decide without it.
func v7VerificationReceiptInvalidationLazy(task Note, readMaterial func() (string, error)) *v7VerificationInvalidation {
	material, err, read := "", error(nil), false
	for _, row := range parseV7VerificationRows(task.Body) {
		if !read && v7VerificationRowNeedsMaterial(row) {
			material, err = readMaterial()
			read = true
		}
		if cause := v7VerificationReceiptInvalidationForMaterial(task, row, material, err); cause != nil {
			return cause
		}
	}
	return nil
}

func v7VerificationRowNeedsMaterial(row v7VerificationRow) bool {
	if _, command := v7VerificationCommand(row.Check); !command {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(row.Result), "pass") && v7VerificationReceiptFields(row) != nil
}

func v7VerificationReceiptFields(row v7VerificationRow) map[string]string {
	history := v7VerificationReceiptHistory(row)
	if len(history) == 0 {
		return nil
	}
	return history[len(history)-1]
}

func v7VerificationReceiptHistory(row v7VerificationRow) []map[string]string {
	marker := v7VerificationReceiptSchema + " "
	history := []map[string]string{}
	for notes := row.Notes; ; {
		pos := strings.Index(notes, marker)
		if pos < 0 {
			return history
		}
		notes = notes[pos+len(marker):]
		end := strings.Index(notes, "; "+v7VerificationReceiptSchema+" ")
		segment := notes
		if end >= 0 {
			segment = notes[:end]
		}
		fields := map[string]string{}
		for _, token := range strings.Fields(segment) {
			key, value, ok := strings.Cut(strings.TrimRight(token, ";"), "=")
			if ok {
				fields[key] = value
			}
		}
		if len(fields) > 0 {
			history = append(history, fields)
		}
		if end < 0 {
			return history
		}
		notes = notes[end+2:]
	}
}

func v7VerificationReceiptInvalidationForMaterial(task Note, row v7VerificationRow, currentMaterial string, materialErr error) *v7VerificationInvalidation {
	if _, command := v7VerificationCommand(row.Check); !command {
		return nil
	}
	base := v7VerificationInvalidation{NextActor: "command_executor", Recovery: "rerun_checks"}
	if strings.EqualFold(strings.TrimSpace(row.Result), "fail") || strings.EqualFold(strings.TrimSpace(row.Result), "failed") {
		base.Kind, base.Dimension, base.Explanation = "failed", "check_result", "the recorded verification command failed"
		history := v7VerificationReceiptHistory(row)
		if len(history) > 0 {
			base.Current = history[len(history)-1]["material"]
		}
		if len(history) > 1 {
			base.Previous = history[len(history)-2]["material"]
			base.Explanation = "the latest verification command failed; the previous passing receipt remains recorded"
		}
		return &base
	}
	result := strings.ToLower(strings.TrimSpace(row.Result))
	if result == "blocked" || result == "skipped" {
		return nil
	}
	if result != "pass" {
		base.Kind, base.Dimension, base.Explanation = "missing", "receipt", "current command proof has not been recorded"
		return &base
	}
	fields := v7VerificationReceiptFields(row)
	if fields == nil {
		base.Kind, base.Dimension, base.Explanation = "missing", "receipt", "the pass row has no authenticated verification receipt"
		return &base
	}
	if materialErr != nil || strings.TrimSpace(currentMaterial) == "" {
		base.Kind, base.Dimension, base.Previous, base.Explanation = "unavailable", "material", fields["material"], "current scoped material could not be read; this does not prove that work changed"
		return &base
	}
	checks := []struct{ dimension, previous, current string }{
		{"contract", fields["contract"], v7VerificationReceiptContract(task, fields["contract"])},
		{"work_revision", fields["work_revision"], strconv.Itoa(intField(task.Data, "work_revision"))},
		{"source", fields["source"], fallback(firstNonEmpty(stringField(task.Data, "source_sha"), stringField(task.Data, "source_commit")), "-")},
		{"material", fields["material"], currentMaterial},
		{"check", fields["row"], v7VerificationRowFingerprint(row)},
	}
	for _, check := range checks {
		if check.previous != check.current {
			base.Kind, base.Dimension, base.Previous, base.Current = "changed", check.dimension, check.previous, check.current
			base.Explanation = "accepted verification no longer matches the current " + check.dimension
			return &base
		}
	}
	return nil
}

// v7VerificationReceiptContract is the task's current contract fingerprint in
// the form a receipt recorded: receipts bind the check contract, and older
// receipts bound the whole contract, policy included.
func v7VerificationReceiptContract(task Note, recorded string) string {
	if full := directWaveTaskContractFingerprint(task.Data, task.Body); recorded == full {
		return full
	}
	return directWaveTaskCheckContractFingerprint(task.Data, task.Body)
}

func v7VerificationCurrentScopedMaterial(vaultPath string, task Note) (string, error) {
	return v7VerificationCurrentScopedMaterialWithStore(nil, vaultPath, task)
}

// v7VerificationCurrentScopedMaterialWithStore uses the caller's runtime store
// when it has one instead of opening another.
func v7VerificationCurrentScopedMaterialWithStore(store *RuntimeStore, vaultPath string, task Note) (string, error) {
	hasCommand := false
	for _, row := range parseV7VerificationRows(task.Body) {
		if _, ok := v7VerificationCommand(row.Check); ok {
			hasCommand = true
			break
		}
	}
	if !hasCommand {
		return "", nil
	}
	if material, found, err := v7SubmittedVerificationMaterial(store, vaultPath, task); found || err != nil {
		return material, err
	}
	repoRoot, err := canonicalV7VerificationWorkspaceRoot(v7RepoRoot(vaultPath))
	if err != nil {
		return "", err
	}
	scope, err := canonicalTaskMaterialScope(vaultPath, task)
	if err != nil {
		return "", err
	}
	generatedOutputScope, err := taskGeneratedOutputScope(task)
	if err != nil {
		return "", err
	}
	if len(scope) == 0 {
		scope = nil
	}
	return workspaceTreeStateHashForPaths(repoRoot, scope, generatedOutputScope)
}

func v7SubmittedVerificationMaterial(store *RuntimeStore, vaultPath string, task Note) (string, bool, error) {
	if store == nil {
		opened, err := OpenRuntimeStoreReadOnly(DefaultStateRoot())
		if err != nil {
			return "", false, nil
		}
		defer opened.Close()
		store = opened
	}
	projectID, registered, err := registeredProjectIDForVault(store, vaultPath)
	if err != nil || !registered {
		return "", false, err
	}
	run, err := store.FindRunScoped(projectID, trackerRecordID(task))
	if err != nil || run == nil || run.WorkRevision == 0 {
		return "", false, err
	}
	// Building the workspace binds and hashes the submission once; verifying it
	// again at once would only repeat that.
	workspace, err := recoveryCommandVerificationWorkspace(store, vaultPath, task, *run)
	if err != nil {
		return "", true, err
	}
	return workspace.MaterialFingerprint, true, nil
}

func v7VerificationReceiptRequirementMissingForMaterial(task Note, currentMaterial string, materialErr error) string {
	for _, row := range parseV7VerificationRows(task.Body) {
		if cause := v7VerificationReceiptInvalidationForMaterial(task, row, currentMaterial, materialErr); cause != nil {
			return "command proof for " + fallback(strings.TrimSpace(row.CoverText), "unknown acceptance") + ": " + cause.Explanation
		}
	}
	return ""
}

func v7VerificationReceiptInvalidation(vaultPath string, task Note) *v7VerificationInvalidation {
	return v7VerificationReceiptInvalidationLazy(task, func() (string, error) {
		return v7VerificationCurrentScopedMaterial(vaultPath, task)
	})
}

func v7VerificationReceiptRequirementMissing(vaultPath string, task Note) string {
	material, err := v7VerificationCurrentScopedMaterial(vaultPath, task)
	return v7VerificationReceiptRequirementMissingForMaterial(task, material, err)
}

type v7VerificationManifestRow struct {
	Covers  string `json:"covers"`
	Command string `json:"command"`
}

func v7VerificationManifest(data map[string]any, rows []v7VerificationRow) (string, []v7VerificationManifestRow) {
	pending := []v7VerificationManifestRow{}
	for _, row := range rows {
		if !strings.EqualFold(strings.TrimSpace(row.Result), "pending") {
			continue
		}
		if command, ok := v7VerificationCommand(row.Check); ok {
			pending = append(pending, v7VerificationManifestRow{Covers: strings.TrimSpace(row.CoverText), Command: command})
		}
	}
	manifest := struct {
		Schema    string                      `json:"schema"`
		TaskID    string                      `json:"task_id"`
		StateRev  string                      `json:"state_rev"`
		SourceSHA string                      `json:"source_sha,omitempty"`
		Commands  []v7VerificationManifestRow `json:"commands"`
	}{"tusker.verification-manifest/v1", stringField(data, "id"), stringField(data, "state_rev"), firstNonEmpty(stringField(data, "source_sha"), stringField(data, "source_commit")), pending}
	raw, _ := json.Marshal(manifest)
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:]), pending
}

func canonicalV7VerificationWorkspaceRoot(root string) (string, error) {
	repoRoot, err := filepath.Abs(filepath.Clean(root))
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(repoRoot)
	if err != nil {
		return "", tuskerError(errorInvalidTransition, "verification repository root must be a canonical real path")
	}
	repoRoot = filepath.Clean(resolved)
	top, err := gitOutputTrim(repoRoot, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", tuskerError(errorInvalidTransition, "verification commands require a canonical Git repository root")
	}
	top, err = filepath.Abs(filepath.Clean(top))
	if err != nil {
		return "", tuskerError(errorInvalidTransition, "verification Git repository root is invalid")
	}
	top, err = filepath.EvalSymlinks(top)
	if err != nil || filepath.Clean(top) != repoRoot {
		return "", tuskerError(errorInvalidTransition, "verification vault does not resolve to the canonical Git repository root")
	}
	return repoRoot, nil
}

var v7VerificationDroppedEnvReported sync.Once

// v7VerificationCommandEnv is the minimal environment checks run with, plus
// the repo's verification.env names (NAME_* matches a prefix). With report
// set it names, once per process, the variables checks will not see.
func v7VerificationCommandEnv(extra []string, report bool) []string {
	allowed := map[string]bool{"HOME": true, "PATH": true, "TMPDIR": true, "TMP": true, "TEMP": true, "LANG": true, "LC_ALL": true, "LC_CTYPE": true, "TZ": true, "GOCACHE": true, "GOMODCACHE": true, "CARGO_TARGET_DIR": true, "TUSKER_VALIDATION_LOCK_DIR": true}
	var prefixes []string
	for _, name := range extra {
		name = strings.TrimSpace(name)
		// A bare * would hand every check the shell's secrets; validation
		// refuses it and it is ignored here.
		if prefix, ok := strings.CutSuffix(name, "*"); ok && prefix != "" {
			prefixes = append(prefixes, prefix)
		} else if !ok && name != "" {
			allowed[name] = true
		}
	}
	env := []string{"TUSKER_VERIFICATION_GATE=1"}
	var dropped []string
	for _, pair := range os.Environ() {
		key, _, _ := strings.Cut(pair, "=")
		keep := allowed[key]
		for _, prefix := range prefixes {
			keep = keep || strings.HasPrefix(key, prefix)
		}
		if keep {
			env = append(env, pair)
		} else if key != "" {
			dropped = append(dropped, key)
		}
	}
	if report && len(dropped) > 0 {
		v7VerificationDroppedEnvReported.Do(func() {
			sort.Strings(dropped)
			fmt.Fprintf(os.Stderr, "checks run without these variables from your shell (pass more with verification.env in WORKFLOW.md): %s\n", strings.Join(dropped, ", "))
		})
	}
	return env
}

// v7VerificationOutputTail is how much of a check's last output is kept for
// the failure log; build tools print the cause last.
const v7VerificationOutputTail = 16 << 10

type v7BoundedOutput struct {
	mu        sync.Mutex
	buf       []byte
	tail      []byte
	limit     int
	truncated bool
}

func (b *v7BoundedOutput) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if remaining := b.limit - len(b.buf); remaining > 0 {
		if len(p) < remaining {
			remaining = len(p)
		}
		b.buf = append(b.buf, p[:remaining]...)
	}
	if len(b.buf) >= b.limit && len(p) > 0 {
		b.truncated = true
	}
	b.tail = append(b.tail, p...)
	if over := len(b.tail) - v7VerificationOutputTail; over > 0 {
		b.tail = append(b.tail[:0], b.tail[over:]...)
	}
	return len(p), nil
}

func (b *v7BoundedOutput) Bytes() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]byte(nil), b.buf...)
}

func (b *v7BoundedOutput) Tail() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]byte(nil), b.tail...)
}

// v7SaveFailedCheckOutput keeps the end of a failed check's output in the
// vault's git-ignored generated area, since the task records only a digest.
// It returns the file's path relative to the repo, or "" when nothing was
// saved.
func v7SaveFailedCheckOutput(vaultPath, taskID string, row int, tail []byte) string {
	if len(tail) == 0 || strings.TrimSpace(vaultPath) == "" {
		return ""
	}
	path := filepath.Join(vaultPath, "_system", "generated", "check-output", taskID, fmt.Sprintf("row-%d.log", row))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil || os.WriteFile(path, tail, 0o644) != nil {
		return ""
	}
	if rel, err := filepath.Rel(v7RepoRoot(vaultPath), path); err == nil && !strings.HasPrefix(rel, "..") {
		return filepath.ToSlash(rel)
	}
	return path
}

// v7VerificationRerunsWithoutConfirmation reports a receipt that went stale
// only because the work moved under its unchanged, already accepted command.
func v7VerificationRerunsWithoutConfirmation(cause *v7VerificationInvalidation) bool {
	return cause != nil && cause.Kind == "changed" && cause.Dimension != "check"
}

// v7TaskWithStaleCommandRowsPending returns the task as a stale-rerunning
// close will execute it: rows whose receipts went stale read as pending.
func v7TaskWithStaleCommandRowsPending(vaultPath string, task Note) Note {
	rows := parseV7VerificationRows(task.Body)
	material, materialErr := v7VerificationCurrentScopedMaterial(vaultPath, task)
	changed := false
	for i := range rows {
		if v7VerificationRerunsWithoutConfirmation(v7VerificationReceiptInvalidationForMaterial(task, rows[i], material, materialErr)) {
			rows[i].Result, changed = "pending", true
		}
	}
	if !changed {
		return task
	}
	task.Body = replaceSection(task.Body, "## Verification", renderV7VerificationTable(rows))
	return task
}

func v7PendingCommandProofGaps(task Note, report v7ProofReport) []string {
	rows := parseV7VerificationRows(task.Body)
	var pending []v7VerificationRow
	for _, row := range rows {
		if strings.EqualFold(strings.TrimSpace(row.Result), "pending") {
			if _, ok := v7VerificationCommand(row.Check); ok {
				pending = append(pending, row)
			}
		}
	}
	if len(pending) == 0 {
		return append(append([]string{}, report.Missing...), report.ModeMissing...)
	}
	acceptance := v7AcceptanceIDs(task.Body)
	covered := map[string]bool{}
	for _, row := range pending {
		for _, id := range v7CoverTextToAcceptanceIDs(row.CoverText, acceptance) {
			covered[id] = true
		}
	}
	var missing []string
	for _, gap := range report.Missing {
		if !covered[strings.TrimPrefix(gap, "acceptance:")] {
			missing = append(missing, gap)
		}
	}
	for _, gap := range report.ModeMissing {
		if strings.HasPrefix(gap, "verification_receipt:") {
			// Pending command rows are allowed through the read-only preflight so
			// the shared executor can create their receipts. The post-execution
			// proof gate rechecks every command row and still rejects stale or
			// non-PASS receipts.
			continue
		}
		if strings.HasPrefix(gap, "proof_required:") {
			required := strings.TrimPrefix(gap, "proof_required:")
			matched := false
			for _, row := range pending {
				if v7InlineVerificationSatisfies(task, required, row) {
					matched = true
					break
				}
			}
			if matched {
				continue
			}
		}
		missing = append(missing, gap)
	}
	return uniqueStrings(missing)
}

func v7CheckoutSnapshot(repoRoot string) (map[string]string, bool) {
	head, err := exec.Command("git", "-C", repoRoot, "rev-parse", "HEAD").Output()
	if err != nil {
		return nil, false
	}
	status, err := exec.Command("git", "-C", repoRoot, "status", "--porcelain=v1", "-z", "--untracked-files=all").Output()
	if err != nil {
		return nil, false
	}
	snapshot := map[string]string{"HEAD": strings.TrimSpace(string(head))}
	entries := strings.Split(string(status), "\x00")
	for i := 0; i < len(entries); i++ {
		entry := entries[i]
		if len(entry) < 4 {
			continue
		}
		code, path := entry[:2], entry[3:]
		value := code
		if info, err := os.Lstat(filepath.Join(repoRoot, path)); err == nil {
			value += fmt.Sprintf(" %d %d", info.Size(), info.ModTime().UnixNano())
		}
		snapshot[path] = value
		if strings.ContainsAny(code, "RC") && i+1 < len(entries) {
			i++
			snapshot[path] += " " + entries[i]
		}
	}
	return snapshot, true
}

func v7CheckoutChangedPaths(before, after map[string]string) []string {
	var paths []string
	for path, value := range before {
		if other, ok := after[path]; !ok || other != value {
			paths = append(paths, path)
		}
	}
	for path := range after {
		if _, ok := before[path]; !ok {
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)
	for i, path := range paths {
		if path == "HEAD" {
			paths[i] = "HEAD moved"
		}
	}
	return paths
}
