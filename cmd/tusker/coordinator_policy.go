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
	"time"

	runnercore "tusker/internal/runner"
)

func coordinatorApplyPolicy(b *coordinatorBatch, t *coordinatorTask, receipt runnercore.ExecutionReceipt, r coordinatorReport, reportErr error, dir string, delay time.Duration, lockFiles ...*os.File) error {
	if t.Policy == nil {
		t.Policy = map[string]int{}
	}
	retry := func(rule, prompt string, cap int, wait time.Duration) bool {
		if t.Policy[rule] >= cap {
			return false
		}
		t.Policy[rule]++
		t.Resume = prompt
		t.State = "ready"
		t.Blocker = ""
		t.RetryAt = time.Now().Add(wait)
		if wait > 0 {
			t.State = "retry_wait"
		}
		if e := coordinatorEvent(b, t, "policy_action", map[string]any{"rule": rule, "count": t.Policy[rule], "wait": wait.String()}); e != nil {
			t.State = "failed"
			t.Blocker = e.Error()
		}
		return true
	}
	if reportErr == nil {
		// Harvest even checkpoints, blocked work and failures, before returning a policy decision.
		if e := coordinatorHarvest(b, t, r.Remaining); e != nil {
			return e
		}
		if e := coordinatorRecordBoxes(b, r.Boxes); e != nil {
			return e
		}
		t.Commits = append([]string(nil), r.Commits...)
	}
	log, _ := os.ReadFile(filepath.Join(dir, "run.log"))
	tail := string(log)
	if len(tail) > 65536 {
		tail = tail[len(tail)-65536:]
	}
	lower := strings.ToLower(receipt.Reason + " " + tail)
	limited := strings.Contains(lower, "usage limit") || strings.Contains(lower, "rate_limit_exceeded") || strings.Contains(lower, "rate limit exceeded") || strings.Contains(lower, "rate limit reached") || strings.Contains(lower, "quota exceeded") || strings.Contains(lower, "you've hit your limit")
	limited = limited && (reportErr != nil || receipt.Outcome != runnercore.EventCompleted || r.Verdict != "pass")
	if limited {
		if retry("usage_limit", "Continue the existing work after the usage limit. Read prior evidence and preserve completed changes.", 12, delay) {
			return nil
		}
		t.State = "failed"
		t.Blocker = "usage-limit retry cap exhausted"
		return nil
	}
	crashed := reportErr != nil && (receipt.Outcome != runnercore.EventCompleted || receipt.ExitCode != nil && *receipt.ExitCode != 0) || receipt.ExitCode != nil && *receipt.ExitCode == 101
	if crashed {
		if retry("crash", "The harness crashed. Resume from existing code and evidence; do not redo completed work. Write a fresh report.", 1, 0) {
			return nil
		}
		t.State = "failed"
		t.Blocker = "crash retry cap exhausted: " + receipt.Reason
		return nil
	}
	if reportErr != nil {
		t.State = "failed"
		t.Blocker = "invalid or missing REPORT.json: " + reportErr.Error()
		return nil
	}
	if len(r.Questions) > 0 {
		note, e := resolveV7Note(b.Vault, t.ID, "task")
		if e != nil {
			return e
		}
		forks, e := coordinatorForks(b.Vault, stringField(note.Data, "epic"))
		if e != nil {
			return e
		}
		var answers []string
		for _, q := range r.Questions {
			answered := false
			for _, f := range forks {
				if f.SupersededBy == "" && (q.Fork != "" && q.Fork == f.ID || q.Fork == "" && strings.TrimSpace(q.Text) == strings.TrimSpace(f.Question)) {
					answers = append(answers, f.ID+": "+f.Ruling)
					answered = true
					break
				}
			}
			if !answered {
				t.State = "parked"
				t.Blocker = q.Text
				return nil
			}
		}
		if retry("fork", "Already decided — continue: "+strings.Join(answers, "\n"), 2, 0) {
			return nil
		}
		t.State = "parked"
		t.Blocker = "fork resume cap exhausted"
		return nil
	}
	var failed []int
	for i, g := range r.Gates {
		if g.Result == "fail" || g.Result == "failed" {
			failed = append(failed, i)
		}
	}
	if len(failed) > 0 {
		reg, e := coordinatorLoadRegistry(b.Vault)
		if e != nil {
			return e
		}
		known := true
		for _, i := range failed {
			g := r.Gates[i]
			matched := false
			for _, f := range reg.Flaky {
				if g.Test == f.Test && coordinatorFlakyActive(b.Vault, f, time.Now()) {
					matched = true
				}
			}
			if !matched {
				known = false
				break
			}
		}
		if known {
			for _, i := range failed {
				g := &r.Gates[i]
				key := "flaky:" + g.Test
				cached := true
				for n := 0; n < 2; n++ {
					_, err, found := coordinatorCheckResult(dir, fmt.Sprintf("flaky-%s-%d", coordinatorHash(g.Test), n+1))
					cached = cached && found && err == nil
				}
				if t.Policy[key] >= 1 && !cached || strings.TrimSpace(g.IsolatedCommand) == "" {
					t.State = "failed"
					t.Blocker = "flaky gate lacks isolated command or retry cap exhausted: " + g.Test
					return nil
				}

				t.Policy[key] = 1
				for n := 0; n < 2; n++ {
					remaining := t.Budget - time.Since(t.Started)
					if remaining <= 0 {
						t.State = "stalled"
						t.Blocker = "budget exhausted during isolated reruns"
						return nil
					}
					if remaining > 5*time.Minute {
						remaining = 5 * time.Minute
					}
					ctx, cancel := context.WithTimeout(context.Background(), remaining)
					cmd := exec.CommandContext(ctx, "/bin/sh", "-c", g.IsolatedCommand)
					cmd.Dir = t.Workspace
					cmd.SysProcAttr = coordinatorProcessGroup()
					cmd.Cancel = func() error { return coordinatorCancelCommand(cmd) }
					out, e := coordinatorRunCheck(cmd, dir, lockFiles, remaining, fmt.Sprintf("flaky-%s-%d", coordinatorHash(g.Test), n+1))
					cancel()
					path := filepath.Join(dir, fmt.Sprintf("flaky-%s-%d.log", coordinatorHash(g.Test), n+1))
					if we := coordinatorWriteText(path, string(out)); we != nil {
						return we
					}
					if e != nil {
						t.State = "failed"
						t.Blocker = "isolated flaky rerun failed: " + g.Test
						return nil
					}
				}
				t.Policy[key] = 1
				if len(lockFiles) > 0 {
					if err := coordinatorSaveBatch(b); err != nil {
						return err
					}
				}
				g.Result = "pass (flaky: " + g.Test + ")"
				if e := coordinatorEvent(b, t, "flaky_rerun", map[string]any{"test": g.Test, "passes": 2}); e != nil {
					return e
				}
			}
			if e := coordinatorWriteJSON(filepath.Join(dir, "REPORT.json"), r); e != nil {
				return e
			}
			// If the worker stopped on that gate, continue; only an actual pass report can finish.
			if r.Verdict != "pass" {
				t.State = "ready"
				t.Resume = "Registered flaky gates passed twice in isolation; continue with the remaining work. Record the rerun evidence in the final report."
				return nil
			}
		} else {
			if retry("unknown_gate", "Fix the failed gate or report its first actionable error. Preserve existing changes and evidence.", 1, 0) {
				return nil
			}
			t.State = "failed"
			t.Blocker = firstNonEmpty(r.FirstBlocker, "unknown gate failed after repair retry")
			return nil
		}
	}
	if r.Verdict == "checkpoint" || r.Verdict == "blocked" {
		t.State = "parked"
		t.Blocker = firstNonEmpty(r.FirstBlocker, "checkpoint needs coordinator judgment")
		return nil
	}
	if r.Verdict == "fail" {
		t.State = "failed"
		t.Blocker = firstNonEmpty(r.FirstBlocker, "worker reported failure")
		return nil
	}
	if receipt.Outcome != runnercore.EventCompleted {
		t.State = "failed"
		t.Blocker = "passing report conflicts with unsuccessful harness receipt: " + receipt.Reason
		return nil
	}
	t.Audit = coordinatorAuditReport(b.Vault, t, r, dir, lockFiles...)
	if !t.Audit.Passed {
		t.State = "parked"
		t.Blocker = "audit: " + strings.Join(t.Audit.Failures, "; ")
		return coordinatorEvent(b, t, "audit_failed", t.Audit)
	}
	t.State = "review"
	t.Blocker = ""
	return coordinatorEvent(b, t, "audit_passed", t.Audit)
}
func coordinatorHarvest(b *coordinatorBatch, parent *coordinatorTask, items []coordinatorRemaining) error {
	if len(items) == 0 {
		return nil
	}
	l, e := coordinatorDocumentLock(filepath.Join(b.Vault, "work", "batches", "harvest"), v7DocumentLockTimeout)
	if e != nil {
		return e
	}
	defer l.Close()
	parentNote, e := resolveV7Note(b.Vault, parent.ID, "task")
	if e != nil {
		return e
	}
	epic := stringField(parentNote.Data, "epic")
	for _, item := range items {
		title := strings.ToLower(oneLine(item.Title))
		idx, e := loadV7Index(b.Vault)
		if e != nil {
			return e
		}
		existing := coordinatorExistingFollowup(idx, epic, title, parent.ID)
		if existing == "" {
			body := fmt.Sprintf("# %s\n\n## Problem\n\n%s\n\n## Evidence\n\n%s\n\nOrigin: %s. Scope and acceptance need coordinator review before promotion.\n", item.Title, item.Why, item.Evidence, parent.ID)
			args := Args{"vault": b.Vault, "title": item.Title, "epic": epic, "owned-paths": strings.Join(parent.Owned, ","), "work-level": "standard", "origin": "task:" + parent.ID, "quiet": "true", "status": "backlog"}
			cleanup, e := serveBodyFileArg(args, body)
			if e != nil {
				return e
			}
			internal, ie := newV7InternalActor("tusker:batch")
			if ie != nil {
				cleanup()
				return ie
			}
			e = newV7TaskWithActor(args, &internal)
			cleanup()
			if e != nil {
				return e
			}
			idx, e = loadV7Index(b.Vault)
			if e != nil {
				return e
			}
			existing = coordinatorExistingFollowup(idx, epic, title, parent.ID)
			if existing == "" {
				return fmt.Errorf("harvested task not found")
			}
		}
		note := idx.Tasks[existing]
		if _, _, e = mutateV7DocumentLocked(note.AbsolutePath, v7FrontmatterOrder["task"], func(d map[string]any, body string) (map[string]any, string, bool, error) {
			origins := normalizeList(d["followup_origins"])
			if !containsString(origins, parent.ID) {
				d["followup_origins"] = append(origins, parent.ID)
			}
			var reports []map[string]string
			raw, err := json.Marshal(d["followup_reports"])
			if err != nil {
				return d, body, false, err
			}
			if err := json.Unmarshal(raw, &reports); err != nil {
				return d, body, false, err
			}
			for _, report := range reports {
				if report["task"] == parent.ID && report["why"] == item.Why && report["evidence"] == item.Evidence {
					return d, body, false, nil
				}
			}
			reports = append(reports, map[string]string{"task": parent.ID, "why": item.Why, "evidence": item.Evidence})
			d["followup_reports"] = reports

			return d, body, true, nil
		}); e != nil {
			return e
		}
		if !containsString(parent.Followups, existing) {
			parent.Followups = append(parent.Followups, existing)
		}
	}
	return nil
}

func coordinatorExistingFollowup(idx v7Index, epic, title, parent string) string {
	ids := []string{}
	for id, n := range idx.Tasks {
		status := stringField(n.Data, "status")
		if id != parent && status != "done" && status != "cancelled" && status != "superseded" && stringField(n.Data, "epic") == epic && strings.ToLower(oneLine(stringField(n.Data, "title"))) == title {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	if len(ids) > 0 {
		return ids[0]
	}
	return ""
}
