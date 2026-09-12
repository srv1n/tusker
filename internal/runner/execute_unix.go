//go:build darwin || linux

package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
)

type cappedBuffer struct {
	mu        sync.Mutex
	b         bytes.Buffer
	limit     int
	truncated bool
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	writable := b.limit - b.b.Len()
	if writable > 0 {
		if writable > len(p) {
			writable = len(p)
		}
		_, _ = b.b.Write(p[:writable])
	}
	if writable < len(p) {
		b.truncated = true
	}
	return len(p), nil
}

func (b *cappedBuffer) snapshot() (string, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.String(), b.truncated
}

func Execute(ctx context.Context, prepared PreparedLaunch, sink EventSink) (ExecutionReceipt, error) {
	if err := verifyPreparedIdentity(prepared); err != nil {
		return ExecutionReceipt{}, err
	}
	if prepared.Transport == TransportACP {
		return executeACP(ctx, prepared, sink)
	}
	if prepared.Transport != TransportCLI {
		return ExecutionReceipt{}, fmt.Errorf("unsupported transport %s", prepared.Transport)
	}
	if len(prepared.Argv) == 0 || prepared.Argv[0] != prepared.Executable {
		return ExecutionReceipt{}, errors.New("invalid prepared launch argv")
	}
	deadline := prepared.Deadline
	if deadline <= 0 {
		deadline = defaultRunDeadline
	}
	runCtx, cancel := context.WithTimeout(ctx, deadline)
	defer cancel()
	stdout, stderr := &cappedBuffer{limit: prepared.OutputLimit}, &cappedBuffer{limit: prepared.OutputLimit}
	cmd := exec.CommandContext(runCtx, prepared.Executable, prepared.Argv[1:]...)
	cmd.Dir, cmd.Env = prepared.CWD, append([]string(nil), prepared.Environment...)
	stdin := prepared.prompt
	if prepared.Dialect == "claude" && contains(prepared.Argv, "--input-format") {
		encoded, _ := json.Marshal(map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": prepared.prompt}})
		stdin = string(encoded) + "\n"
	}
	cmd.Stdin = strings.NewReader(stdin)
	cmd.Stdout, cmd.Stderr = stdout, stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
			_ = cmd.Process.Kill()
		}
		time.AfterFunc(2*time.Second, func() { _ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) })
		return nil
	}
	started := time.Now().UTC()
	receipt := ExecutionReceipt{Schema: "tusker.runner-execution/v1", AttemptID: eventAttempt(prepared), LaunchHash: prepared.LaunchHash, HarnessID: prepared.HarnessID, Provider: prepared.Provider, Transport: prepared.Transport, Version: prepared.Version, StartedAt: started, RequestedPreset: prepared.RequestedPreset, EffectivePolicy: prepared.EffectivePolicy}
	if err := runCtx.Err(); err != nil {
		receipt.FinishedAt, receipt.Outcome, receipt.Reason = time.Now().UTC(), EventCancelled, "cancelled"
		return receipt, err
	}
	if err := cmd.Start(); err != nil {
		receipt.FinishedAt, receipt.Outcome, receipt.Reason = time.Now().UTC(), EventFailed, err.Error()
		return receipt, err
	}
	emit := func(kind EventType, reason string, data json.RawMessage) {
		event := Event{AttemptID: receipt.AttemptID, Sequence: uint64(len(receipt.Events) + 1), ObservedAt: time.Now().UTC(), Type: kind, Reason: reason, Data: data}
		receipt.Events = append(receipt.Events, event)
		if sink != nil {
			_ = sink(runCtx, event)
		}
	}
	emit(EventStarted, "", nil)
	waitErr := cmd.Wait()
	receipt.FinishedAt = time.Now().UTC()
	receipt.Stdout, receipt.StdoutTruncated = stdout.snapshot()
	receipt.Stderr, receipt.StderrTruncated = stderr.snapshot()
	if exitErr, ok := waitErr.(*exec.ExitError); ok {
		code := exitErr.ExitCode()
		receipt.ExitCode = &code
	} else if waitErr == nil {
		code := 0
		receipt.ExitCode = &code
	}
	if runCtx.Err() != nil {
		if errors.Is(ctx.Err(), context.Canceled) {
			receipt.Outcome, receipt.Reason = EventCancelled, "cancelled"
		} else {
			receipt.Outcome, receipt.Reason = EventFailed, "timeout"
		}
	} else if waitErr != nil {
		kind, reason, session := classifyCLIResult(prepared.Dialect, receipt.Stdout)
		receipt.SessionID = session
		if kind == EventFailed && reason != "required final result missing" {
			receipt.Outcome, receipt.Reason = kind, reason
		} else {
			receipt.Outcome, receipt.Reason = EventFailed, bounded(waitErr.Error()+" "+receipt.Stderr, 500)
		}
	} else {
		receipt.Outcome, receipt.Reason, receipt.SessionID = classifyCLIResult(prepared.Dialect, receipt.Stdout)
	}
	emit(receipt.Outcome, receipt.Reason, nil)
	if receipt.Outcome == EventFailed {
		return receipt, errors.New(receipt.Reason)
	}
	return receipt, nil
}

func eventAttempt(p PreparedLaunch) string {
	if p.LaunchHash == "" {
		return ""
	}
	digest := strings.TrimPrefix(p.LaunchHash, "sha256:")
	if len(digest) > 12 {
		digest = digest[:12]
	}
	return p.HarnessID + ":" + digest
}

func classifyCLIResult(dialect, output string) (EventType, string, string) {
	seenFinal, failed, session := false, false, ""
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if len(line) > maxProtocolFrame {
			return EventFailed, "protocol_frame_too_large", session
		}
		var value map[string]any
		if json.Unmarshal([]byte(line), &value) != nil {
			continue
		}
		typeName, _ := value["type"].(string)
		if typeName == "thread.started" {
			session, _ = value["thread_id"].(string)
		}
		switch dialect {
		case "codex":
			if typeName == "turn.completed" {
				seenFinal = true
			}
			if typeName == "turn.failed" || typeName == "error" {
				seenFinal, failed = true, true
			}
		case "claude":
			if typeName == "system" {
				session, _ = value["session_id"].(string)
			}
			if typeName == "result" {
				seenFinal = true
				failed, _ = value["is_error"].(bool)
				if failed {
					if result, ok := value["result"].(string); ok && strings.TrimSpace(result) != "" {
						return EventFailed, bounded(result, 500), session
					}
				}
			}
		case "muse":
			if stream, ok := value["stream"].(map[string]any); ok {
				if id, ok := stream["id"].(string); ok && strings.TrimSpace(id) != "" {
					session = strings.TrimSpace(id)
				}
			}
			payloadType, _ := value["payload_type"].(string)
			if payloadType == "" {
				payloadType, _ = value["record_type"].(string)
			}
			switch strings.ToLower(strings.TrimSpace(payloadType)) {
			case "run.terminal.completed", "terminal.completed":
				seenFinal = true
			case "run.terminal.failed", "terminal.failed", "run.terminal.error", "terminal.error":
				seenFinal, failed = true, true
			case "run.terminal.cancelled", "terminal.cancelled":
				return EventCancelled, "Muse run cancelled", session
			}
		}
	}
	if !seenFinal {
		return EventFailed, "required final result missing", session
	}
	if failed {
		return EventFailed, "provider reported failure", session
	}
	return EventCompleted, "", session
}
