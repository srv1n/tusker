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
// task under the proof lock, executes every command row from the repository
// root, records the observed result and bounded receipt in the same table, and
// returns failures for the caller to refuse before any lifecycle transition.
// Manual-proof rows are deliberately left byte-for-byte untouched.
func executeV7CommandVerificationRows(vaultPath string, task Note, args Args, actor string, trustedWorker bool) (Note, v7ProofReport, []v7VerificationExecutionFailure, error) {
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
		manifest, pending := v7VerificationManifest(data, rows)
		if len(pending) > v7VerificationCommandMaxRows {
			return tuskerError(errorEvidenceGate, fmt.Sprintf("%s: verification manifest has %d pending command rows; maximum is %d", taskID, len(pending), v7VerificationCommandMaxRows))
		}
		if len(pending) == 0 {
			idx, err := loadV7Index(vaultPath)
			if err != nil {
				return err
			}
			fresh = current
			report = computeV7ProofReport(vaultPath, current, idx)
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
		repoRoot, err := canonicalV7VerificationRepoRoot(vaultPath)
		if err != nil {
			return err
		}
		deadline := time.Now().Add(v7VerificationCommandTimeoutFor(args))
		changed := false
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
			rows[i].Notes = appendV7VerificationExecutionNote(row.Notes, observed)
			rows[i].BlockedBy = ""
			changed = true
		}
		if !changed {
			idx, err := loadV7Index(vaultPath)
			if err != nil {
				return err
			}
			fresh = current
			report = computeV7ProofReport(vaultPath, current, idx)
			return nil
		}
		body = replaceSection(body, "## Verification", renderV7VerificationTable(rows))
		idx, err := loadV7Index(vaultPath)
		if err != nil {
			return err
		}
		current.Body = body
		report = computeV7ProofReport(vaultPath, current, idx)
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
}

func runV7VerificationCommand(repoRoot, command string, timeout time.Duration) (v7VerificationCommandObservation, error) {
	started := time.Now().UTC()
	cmd := exec.Command("/bin/sh", "-c", command)
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

func appendV7VerificationExecutionNote(existing string, observation v7VerificationCommandObservation) string {
	parts := []string{fmt.Sprintf("tusker gate executed at %s", observation.FinishedAt.Format(time.RFC3339Nano)), fmt.Sprintf("exit=%d", observation.ExitCode), "output_sha256=" + observation.Digest}
	if observation.TimedOut {
		parts = append(parts, "timeout")
	}
	if observation.Truncated {
		parts = append(parts, "output_truncated")
	}
	receipt := strings.Join(parts, "; ")
	if strings.TrimSpace(existing) == "" || existing == "-" {
		return receipt
	}
	return strings.TrimSpace(existing) + "; " + receipt
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

func canonicalV7VerificationRepoRoot(vaultPath string) (string, error) {
	repoRoot, err := filepath.Abs(filepath.Clean(v7RepoRoot(vaultPath)))
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
