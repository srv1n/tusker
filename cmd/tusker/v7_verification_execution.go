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
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Verification commands are gate work, not worker attestations. Keep the
// default finite so a broken command cannot wedge accept/close forever.
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

// v7VerificationCommandTimeoutFor returns the bounded per-row timeout. The
// override is intentionally an internal/test-facing duration knob; zero or
// malformed values retain the safe default.
func v7VerificationCommandTimeoutFor(args Args) time.Duration {
	if args != nil {
		if raw := strings.TrimSpace(args.String("verification-timeout-ms")); raw != "" {
			if ms := atoiSafe(raw); ms > 0 {
				limit := time.Duration(ms) * time.Millisecond
				if limit < v7VerificationCommandTimeout {
					return limit
				}
			}
		}
	}
	return v7VerificationCommandTimeout
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
}

func executeV7CommandVerificationRowsInWorkspace(vaultPath string, task Note, args Args, actor string, trustedWorker bool, workspace *v7VerificationWorkspace) (Note, v7ProofReport, []v7VerificationExecutionFailure, error) {
	taskID := stringField(task.Data, "id")
	if taskID == "" {
		return task, v7ProofReport{}, nil, tuskerError(errorInvalidArg, "verification execution requires a task id")
	}
	var fresh Note
	var report v7ProofReport
	var failures []v7VerificationExecutionFailure
	retry := "tusker accept " + taskID
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
		if args.Bool("rerun-invalid") {
			material, materialErr := v7VerificationCurrentScopedMaterial(vaultPath, current)
			if workspace != nil {
				material, materialErr = workspace.MaterialFingerprint, workspace.Verify()
			}
			for i := range rows {
				if v7VerificationReceiptInvalidationForMaterial(current, rows[i], material, materialErr) != nil {
					rows[i].Result = "pending"
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
		if len(pending) > 0 && !trustedWorker {
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
		deadline := time.Now().Add(v7VerificationCommandTimeoutFor(args))
		changed := false
		observations := map[int]v7VerificationCommandObservation{}
		for i, row := range rows {
			if !strings.EqualFold(strings.TrimSpace(row.Result), "pending") {
				continue
			}
			command, ok := v7VerificationCommand(row.Check)
			if !ok {
				continue
			}
			remaining := time.Until(deadline)
			if remaining <= 0 {
				return tuskerError(errorEvidenceGate, taskID+": verification command total wall budget exhausted")
			}
			observed, execErr := runV7VerificationCommand(repoRoot, command, remaining)
			rows[i].Result = "pass"
			if execErr != nil {
				rows[i].Result = "fail"
				failure := v7VerificationExecutionFailure{Row: rows[i], Message: observed.Message}
				failures = append(failures, failure)
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
}

func runV7VerificationCommand(repoRoot, command string, timeout time.Duration) (v7VerificationCommandObservation, error) {
	started := time.Now().UTC()
	filtered := v7FilteredTestCommand(command)
	var cmd *exec.Cmd
	if filtered {
		args, ok := v7SupportedGoTestArgs(command)
		if !ok {
			message := "filtered test command uses unsupported selector runner or shell syntax; use a single go test -run invocation"
			return v7RejectedVerificationCommandObservation(started, message)
		}
		cmd = exec.Command(args[0], args[1:]...)
	} else {
		cmd = exec.Command("/bin/sh", "-c", command)
	}
	cmd.Dir = filepath.Clean(repoRoot)
	cmd.Env = v7VerificationCommandEnv()
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
	obs := v7VerificationCommandObservation{StartedAt: started, FinishedAt: finished, Digest: "sha256:" + hex.EncodeToString(digest[:]), Truncated: output.truncated}
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
				obs.Message = "filtered test command produced untrusted test evidence; use go test -json"
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

func recordProviderVerificationReceipts(vaultPath string, run RunStatus, material string) (Note, error) {
	task, err := resolveV7Note(vaultPath, run.ItemID, "task")
	if err != nil {
		return task, err
	}
	identity, _, err := v7VerificationReceiptIdentityFor(vaultPath, task, run.WorkspacePath, nil)
	if err != nil || identity.MaterialFingerprint != material {
		return task, err
	}
	rows := parseV7VerificationRows(task.Body)
	events := readReviewPacketEvents(run.EventSinkPath)
	changed := false
	for i, row := range rows {
		if !strings.EqualFold(strings.TrimSpace(row.Result), "pending") {
			continue
		}
		wanted, ok := v7VerificationCommand(row.Check)
		if !ok {
			continue
		}
		for _, event := range events {
			payload := reviewPacketEventPayload(event)
			if reviewPacketEventKind(event) != "command_completed" || intValue(payload["exit_code"]) != 0 || stringValue(payload["material"]) != material || !providerCommandContainsExactCheck(stringValue(payload["command"]), wanted) {
				continue
			}
			observation, execErr := runV7VerificationCommand(run.WorkspacePath, wanted, v7VerificationCommandTimeout)
			rows[i].Result = "pass"
			if execErr != nil {
				rows[i].Result = "fail"
			}
			currentIdentity, _, identityErr := v7VerificationReceiptIdentityFor(vaultPath, task, run.WorkspacePath, nil)
			if identityErr != nil || currentIdentity.MaterialFingerprint != identity.MaterialFingerprint {
				return task, tuskerError(errorEvidenceGate, "provider-observed verification changed the scoped implementation material")
			}
			rows[i].Notes = appendV7VerificationExecutionNote(rows[i], observation, currentIdentity)
			rows[i].Notes = appendV7ProofNote(rows[i].Notes, "provider_event="+firstNonEmpty(stringValue(payload["command_id"]), "anonymous")+" provider_output="+stringValue(payload["output_sha256"]))
			changed = true
			break
		}
	}
	if !changed {
		return task, nil
	}
	data, body, err := parseFrontmatterMustRead(task.AbsolutePath)
	if err != nil {
		return task, err
	}
	body = updateV7VerificationLedger(body, rows)
	idx, err := loadV7Index(vaultPath)
	if err != nil {
		return task, err
	}
	current := task
	current.Data, current.Body = data, body
	data["proof_status"] = computeV7ProofReportForMaterial(vaultPath, current, idx, material, nil).Status
	data["updated_at"], data["updated_by"] = time.Now().UTC().Format(time.RFC3339Nano), "daemon:provider-proof"
	if _, err := saveV7DocumentCAS(task.AbsolutePath, data, body, v7FrontmatterOrder["task"], stringField(data, "state_rev")); err != nil {
		return task, err
	}
	return resolveV7Note(vaultPath, run.ItemID, "task")
}

func providerCommandContainsExactCheck(raw, wanted string) bool {
	raw = strings.TrimSpace(raw)
	if raw == wanted {
		return true
	}
	for _, prefix := range []string{"/bin/zsh -lc ", "/bin/bash -lc ", "/bin/sh -lc ", "zsh -lc ", "bash -lc ", "sh -lc "} {
		if !strings.HasPrefix(raw, prefix) {
			continue
		}
		body := strings.TrimSpace(strings.TrimPrefix(raw, prefix))
		if len(body) < 2 || body[0] != body[len(body)-1] || (body[0] != '\'' && body[0] != '"') {
			return false
		}
		body = body[1 : len(body)-1]
		for _, segment := range v7ShellCommandSegments(body) {
			if strings.TrimSpace(segment) == wanted {
				return true
			}
		}
	}
	return false
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
	count, parsed := v7GoTestJSONMatchCount(output)
	return count, parsed
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
	material, err := workspaceTreeStateHashForPaths(repoRoot, scope, generatedOutputScope)
	if err != nil {
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
		ContractFingerprint: contract,
		WorkRevision:        intField(task.Data, "work_revision"),
		SourceRevision:      firstNonEmpty(stringField(task.Data, "source_sha"), stringField(task.Data, "source_commit")),
		MaterialFingerprint: material,
	}, verify, nil
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
	if stored == "" || stored != contract || fields["contract"] != contract || fields["row"] != v7VerificationRowFingerprint(row) {
		return false
	}
	if fields["work_revision"] != strconv.Itoa(intField(task.Data, "work_revision")) || fields["source"] != fallback(firstNonEmpty(stringField(task.Data, "source_sha"), stringField(task.Data, "source_commit")), "-") || fields["material"] != currentMaterial {
		return false
	}
	if v7FilteredTestCommand(row.Check) {
		command, _ := v7VerificationCommand(row.Check)
		if _, ok := v7SupportedGoTestArgs(command); !ok {
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
	material, err := v7VerificationMaterialForReport(vaultPath, task, workspace)
	for _, row := range parseV7VerificationRows(task.Body) {
		if cause := v7VerificationReceiptInvalidationForMaterial(task, row, material, err); cause != nil {
			return cause
		}
	}
	return nil
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
	if !strings.EqualFold(strings.TrimSpace(row.Result), "pass") {
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
	contract := directWaveTaskContractFingerprint(task.Data, task.Body)
	checks := []struct{ dimension, previous, current string }{
		{"contract", fields["contract"], contract},
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

func v7VerificationCurrentScopedMaterial(vaultPath string, task Note) (string, error) {
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
	if material, found, err := v7SubmittedVerificationMaterial(vaultPath, task); found || err != nil {
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

func v7SubmittedVerificationMaterial(vaultPath string, task Note) (string, bool, error) {
	store, err := OpenRuntimeStoreReadOnly(DefaultStateRoot())
	if err != nil {
		return "", false, nil
	}
	defer store.Close()
	projectID, registered, err := registeredProjectIDForVault(store, vaultPath)
	if err != nil || !registered {
		return "", false, err
	}
	run, err := store.FindRunScoped(projectID, trackerRecordID(task))
	if err != nil || run == nil || run.WorkRevision == 0 {
		return "", false, err
	}
	workspace, err := recoveryCommandVerificationWorkspace(store, vaultPath, task, *run)
	if err != nil {
		return "", true, err
	}
	if err := workspace.Verify(); err != nil {
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
	material, err := v7VerificationCurrentScopedMaterial(vaultPath, task)
	for _, row := range parseV7VerificationRows(task.Body) {
		if cause := v7VerificationReceiptInvalidationForMaterial(task, row, material, err); cause != nil {
			return cause
		}
	}
	return nil
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

func v7VerificationCommandEnv() []string {
	allowed := map[string]bool{"HOME": true, "PATH": true, "TMPDIR": true, "TMP": true, "TEMP": true, "LANG": true, "LC_ALL": true, "LC_CTYPE": true, "TZ": true, "GOCACHE": true, "GOMODCACHE": true, "TUSKER_VALIDATION_LOCK_DIR": true}
	env := []string{"TUSKER_VERIFICATION_GATE=1"}
	for _, pair := range os.Environ() {
		key, _, _ := strings.Cut(pair, "=")
		if allowed[key] {
			env = append(env, pair)
		}
	}
	return env
}

type v7BoundedOutput struct {
	mu        sync.Mutex
	buf       []byte
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
	return len(p), nil
}

func (b *v7BoundedOutput) Bytes() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]byte(nil), b.buf...)
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
				if v7InlineVerificationSatisfies(required, row) {
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
