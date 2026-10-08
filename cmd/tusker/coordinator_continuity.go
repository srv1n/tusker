package main

import (
	"fmt"
	"math"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

func coordinatorRecordBoxes(b *coordinatorBatch, boxes []coordinatorBox) error {
	if len(boxes) == 0 {
		return nil
	}
	return coordinatorMutateRegistry(b.Vault, func(r *coordinatorRegistry) error {
		for _, box := range boxes {
			if box.ID == "" || box.Created.IsZero() || box.Rate < 0 || box.EUR < 0 || math.IsNaN(box.Rate) || math.IsInf(box.Rate, 0) {
				return fmt.Errorf("invalid box entry")
			}
			if box.Deleted != nil && box.Deleted.Before(box.Created) {
				return fmt.Errorf("box deleted before creation")
			}
			box.Batch, box.Epic = b.ID, b.Epic
			found := false
			for i, prior := range r.Boxes {
				if prior.ID == box.ID {
					if !prior.Created.Equal(box.Created) {
						return fmt.Errorf("box ID reused with different creation time")
					}
					if box.Deleted == nil {
						box.Deleted = prior.Deleted
					}
					if box.Expires == nil {
						box.Expires = prior.Expires
					}
					if box.Rate == 0 {
						box.Rate = prior.Rate
					}
					if box.EUR == 0 {
						box.EUR = prior.EUR
					}
					r.Boxes[i] = box
					found = true
					break
				}
			}
			if !found {
				r.Boxes = append(r.Boxes, box)
			}
		}
		return nil
	})
}
func coordinatorBoxCost(box coordinatorBox, now time.Time) float64 {
	if box.EUR > 0 {
		return box.EUR
	}
	end := now
	if box.Deleted != nil {
		end = *box.Deleted
	}
	hours := end.Sub(box.Created).Hours()
	if hours < 0 {
		hours = 0
	}
	return hours * box.Rate
}

// Allocate a box's bill across the UTC days it was alive. Final billed euros
// are apportioned by elapsed time; an open box accrues at its hourly rate.
func coordinatorBoxDailyCosts(box coordinatorBox, now time.Time) map[string]float64 {
	costs := map[string]float64{}
	end := now.UTC()
	if box.Deleted != nil {
		end = box.Deleted.UTC()
	}
	at := box.Created.UTC()
	total := end.Sub(at).Seconds()
	if total <= 0 {
		costs[at.Format("2006-01-02")] = coordinatorBoxCost(box, now)
		return costs
	}
	rate := box.Rate / 3600
	if box.EUR > 0 {
		rate = box.EUR / total
	}
	for at.Before(end) {
		next := time.Date(at.Year(), at.Month(), at.Day()+1, 0, 0, 0, 0, time.UTC)
		if next.After(end) {
			next = end
		}
		costs[at.Format("2006-01-02")] += next.Sub(at).Seconds() * rate
		at = next
	}
	return costs
}

func coordinatorLedgerCmd(args Args) error {
	vault, e := resolveVaultPath(args, false)
	if e != nil {
		return e
	}
	action := args.String("_pos1")
	if args.String("_pos0") == "box" {
		id := args.String("id")
		if id == "" {
			return fmt.Errorf("ledger box requires --id")
		}
		if action == "add" {
			rate, e := strconv.ParseFloat(args.String("eur-per-hour"), 64)
			if e != nil || rate < 0 || math.IsNaN(rate) || math.IsInf(rate, 0) {
				return fmt.Errorf("invalid eur-per-hour")
			}
			created := time.Now().UTC()
			if raw := args.String("created"); raw != "" {
				created, e = time.Parse(time.RFC3339, raw)
				if e != nil {
					return e
				}
			}
			box := coordinatorBox{ID: id, Type: args.String("type"), Created: created, Rate: rate}
			if raw := args.String("expires"); raw != "" {
				at, e := time.Parse(time.RFC3339, raw)
				if e != nil {
					return e
				}
				box.Expires = &at
			}
			if box.Type == "" || box.Expires == nil {
				return fmt.Errorf("box add requires --type and --expires")
			}
			return coordinatorRecordBoxes(&coordinatorBatch{Vault: vault, ID: args.String("batch"), Epic: args.String("epic")}, []coordinatorBox{box})
		}
		if action == "delete" {
			return coordinatorMutateRegistry(vault, func(r *coordinatorRegistry) error {
				for i := range r.Boxes {
					if r.Boxes[i].ID == id {
						if r.Boxes[i].Deleted == nil {
							now := time.Now().UTC()
							r.Boxes[i].Deleted = &now
						}
						return nil
					}
				}
				return fmt.Errorf("box not found: %s", id)
			})
		}
		return fmt.Errorf("ledger box expects add or delete")
	}
	if args.String("_pos0") != "" {
		return fmt.Errorf("unknown ledger subcommand")
	}
	reg, e := coordinatorLoadRegistry(vault)
	if e != nil {
		return e
	}
	totals := map[string]float64{}
	live := 0
	now := time.Now()
	for _, box := range reg.Boxes {
		cost := coordinatorBoxCost(box, now)
		totals["batch:"+box.Batch] += cost
		totals["epic:"+box.Epic] += cost
		for day, cost := range coordinatorBoxDailyCosts(box, now) {
			totals["day:"+day] += cost
		}
		if box.Deleted == nil {
			live++
		}
	}
	if args.Bool("json") {
		emitJSON(map[string]any{"boxes": reg.Boxes, "totals_eur": totals, "live": live})
		return nil
	}
	fmt.Printf("Boxes: %d recorded, %d alive\n", len(reg.Boxes), live)
	keys := []string{}
	for k := range totals {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Printf("%s €%.2f\n", k, totals[k])
	}
	return nil
}
func coordinatorBatchDigest(b *coordinatorBatch) string {
	lines := []string{fmt.Sprintf("Batch %s (%d tasks) — %s", b.Name, len(b.Tasks), map[bool]string{true: "finished", false: "running"}[!b.Finished.IsZero()])}
	policies := map[string]int{}
	inputTokens, outputTokens := 0, 0
	wallSeconds := 0.0
	followups := []string{}
	auditFailures := 0
	taskLines := []string{}
	for _, t := range b.Tasks {
		inputTokens += t.InputTokens
		outputTokens += t.OutputTokens
		wallSeconds += t.WallSeconds
		label := strings.ToUpper(t.State)
		if t.State == "review" && t.Audit.Passed {
			label = "PASS"
		}
		commits := strings.Join(t.Commits, " ")
		if commits == "" {
			commits = "—"
		}
		detail := oneLine(t.Blocker)
		if len(detail) > 160 {
			detail = detail[:160] + "…"
		}
		gates := ""
		if t.Attempt > 0 {
			if report, err := coordinatorReadReport(coordinatorAttemptDir(b, t), t.ID); err == nil {
				passed := 0
				for _, gate := range report.Gates {
					if strings.HasPrefix(gate.Result, "pass") {
						passed++
					}
				}
				gates = fmt.Sprintf(" gates %d/%d", passed, len(report.Gates))
			}
		}
		taskLines = append(taskLines, fmt.Sprintf("%-7s %s %s | %s%s %s", label, t.ID, oneLine(t.Title), commits, gates, detail))
		for rule, n := range t.Policy {
			policies[rule] += n
		}
		followups = append(followups, t.Followups...)
		auditFailures += len(t.Audit.Failures)
	}
	if len(taskLines) > 28 {
		lines = append(lines, taskLines[:28]...)
		lines = append(lines, fmt.Sprintf("… %d more tasks; use --json", len(taskLines)-28))
	} else {
		lines = append(lines, taskLines...)
	}
	keys := []string{}
	for key := range policies {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var actions []string
	for _, key := range keys {
		actions = append(actions, fmt.Sprintf("%s=%d", key, policies[key]))
	}
	lines = append(lines, "Policy: "+strings.Join(actions, ", "), fmt.Sprintf("Audit failures: %d", auditFailures), "Follow-up tasks: "+strings.Join(uniqueStrings(followups), ", "))
	lines = append(lines, fmt.Sprintf("Usage: input=%d output=%d tokens; worker wall %.1fs", inputTokens, outputTokens, wallSeconds))
	registry, e := coordinatorLoadRegistry(b.Vault)
	if e != nil {
		lines = append(lines, "Ledger unavailable: "+e.Error())
	} else {
		live, count, deleted, expired := 0, 0, 0, []string{}
		cost := 0.0
		for _, box := range registry.Boxes {
			if box.Batch == b.ID {
				count++
				cost += coordinatorBoxCost(box, time.Now())
				if box.Deleted == nil {
					live++
					if box.Expires != nil && time.Now().After(*box.Expires) {
						expired = append(expired, box.ID)
					}
				} else {
					deleted++
				}
			}
		}
		lines = append(lines, fmt.Sprintf("Boxes: %d recorded, %d deleted, %d alive, €%.2f", count, deleted, live, cost))
		if len(expired) > 0 {
			lines = append(lines, "EXPIRED BOXES: "+strings.Join(expired, ", "))
		}
	}
	return coordinatorScreen(lines)
}
func coordinatorScreen(lines []string) string {
	if len(lines) > 40 {
		lines = append(lines[:39], fmt.Sprintf("… %d more lines; use --json for full details", len(lines)-39))
	}
	return strings.Join(lines, "\n") + "\n"
}
func coordinatorDigestCmd(args Args) error {
	vault, e := resolveVaultPath(args, false)
	if e != nil {
		return e
	}
	registry, e := coordinatorLoadRegistry(vault)
	if e != nil {
		return e
	}
	since := time.Now().Add(-24 * time.Hour)
	raw := args.String("since")
	if raw == "last" {
		since = registry.LastDigest
	} else if raw != "" {
		var ok bool
		since, ok = parseTuskerTime(raw)
		if !ok {
			return fmt.Errorf("digest since must be last, RFC3339 or YYYY-MM-DD")
		}
	}
	if args.Bool("all") {
		since = time.Time{}
	}
	started := time.Now().UTC()
	batches, e := coordinatorBatches(vault)
	if e != nil {
		return e
	}
	idx, e := loadV7Index(vault)
	if e != nil {
		return e
	}
	epic := args.String("epic")
	sections := [][]string{{"Running"}, {"Finished"}, {"Waiting on human"}, {"Waiting on coordinator"}, {"Open follow-ups"}, {"Live boxes"}}
	for _, b := range batches {
		if epic != "" && b.Epic != epic {
			continue
		}
		for _, t := range b.Tasks {
			switch t.State {
			case "running", "retry_wait":
				sections[0] = append(sections[0], fmt.Sprintf("%s/%s %s %s %s", b.ID, t.ID, t.State, time.Since(t.Started).Round(time.Second), oneLine(t.Blocker)))
			case "review", "done", "discarded", "failed", "stalled":
				if !t.Finished.Before(since) {
					sections[1] = append(sections[1], fmt.Sprintf("%s %s %s %s", t.ID, t.State, strings.Join(t.Commits, " "), oneLine(t.Blocker)))
				}
			case "parked":
				sections[3] = append(sections[3], t.ID+" "+oneLine(t.Blocker))
			case "ready":
				if t.Blocker != "" {
					sections[3] = append(sections[3], t.ID+" "+oneLine(t.Blocker))
				}
			}
		}
	}
	for id, t := range idx.Tasks {
		if epic != "" && stringField(t.Data, "epic") != epic {
			continue
		}
		if blocker := coordinatorHumanGateBlocker(id, idx); blocker != "" && stringField(t.Data, "status") != "done" {
			sections[2] = append(sections[2], id+" "+blocker)
		}
	}
	var follow []Note
	for _, t := range idx.Tasks {
		if len(normalizeList(t.Data["followup_origins"])) > 0 && stringField(t.Data, "status") != "done" && stringField(t.Data, "status") != "cancelled" && (epic == "" || stringField(t.Data, "epic") == epic) {
			follow = append(follow, t)
		}
	}
	sort.Slice(follow, func(i, j int) bool {
		return stringField(follow[i].Data, "created_at") > stringField(follow[j].Data, "created_at")
	})
	for i, t := range follow {
		if i == 10 {
			break
		}
		sections[4] = append(sections[4], stringField(t.Data, "id")+" "+stringField(t.Data, "title"))
	}
	for _, box := range registry.Boxes {
		if box.Deleted == nil && (epic == "" || box.Epic == epic) {
			warning := ""
			if box.Expires != nil && time.Now().After(*box.Expires) {
				warning = " EXPIRED"
			}
			expiry := "undeclared"
			if box.Expires != nil {
				expiry = box.Expires.Format(time.RFC3339)
			}
			sections[5] = append(sections[5], fmt.Sprintf("%s %s €%.2f expires %s%s", box.ID, box.Type, coordinatorBoxCost(box, time.Now()), expiry, warning))
		}
	}
	lines := []string{"Coordinator digest"}
	for i, section := range sections {
		limit := 4
		if i == 4 {
			limit = 11
		}
		if len(section) > limit {
			lines = append(lines, section[:limit]...)
			lines = append(lines, fmt.Sprintf("… %d more; use --json", len(section)-limit))
		} else {
			lines = append(lines, section...)
		}
	}
	if args.Bool("json") {
		emitJSON(map[string]any{"schema": "tusker.coordinator-digest/v1", "since": since, "sections": sections, "batches": batches, "boxes": registry.Boxes})
	} else {
		fmt.Print(coordinatorScreen(lines))
	}
	// A per-vault watermark is updated only after successful rendering.
	return coordinatorMutateRegistry(vault, func(r *coordinatorRegistry) error { r.LastDigest = started; return nil })
}
func coordinatorCloseCmd(args Args) error {
	vault, e := resolveVaultPath(args, false)
	if e != nil {
		return e
	}
	id := firstNonEmpty(args.String("id"), args.String("_pos0"))
	verdict := args.String("verdict")
	if verdict != "accept" && verdict != "accept-with-followups" && verdict != "reject" {
		return fmt.Errorf("close verdict must be accept, accept-with-followups or reject")
	}
	batches, e := coordinatorBatches(vault)
	if e != nil {
		return e
	}
	currentTask, e := resolveV7Note(vault, id, "task")
	if e != nil {
		return e
	}
	bound := stringField(currentTask.Data, "batch_id")
	for _, b := range batches {
		if b.ID != bound {
			continue
		}
		for _, t := range b.Tasks {
			if t.ID != id {
				continue
			}
			l, e := coordinatorLock(filepath.Join(coordinatorRunRoot(vault, b.ID), "controller.lock"))
			if e != nil {
				return fmt.Errorf("cannot close while batch is running")
			}
			defer l.Close()
			current, e := coordinatorLoadBatch(vault, b.ID)
			if e != nil {
				return e
			}
			for _, candidate := range current.Tasks {
				if candidate.ID == id {
					t = candidate
					break
				}
			}
			b = current
			idx, e := loadV7Index(vault)
			if e != nil {
				return e
			}
			if blocker := coordinatorHumanGateBlocker(id, idx); blocker != "" {
				return fmt.Errorf("close blocked: %s", blocker)
			}
			if t.State == "running" || t.State == "retry_wait" || t.Attempt == 0 && verdict != "reject" {
				return fmt.Errorf("task has no completed batch attempt")
			}
			if verdict != "reject" {
				r, e := coordinatorReadReport(coordinatorAttemptDir(b, t), id)
				if e != nil {
					return e
				}
				locks, err := coordinatorAcquireResources(b, t)
				if err != nil {
					return fmt.Errorf("cannot close active task: %w", err)
				}
				defer coordinatorLocksClose(locks)
				t.Audit = coordinatorAuditReport(vault, t, r, coordinatorAttemptDir(b, t), locks...)
				if !t.Audit.Passed && strings.TrimSpace(args.String("override-audit")) == "" {
					return fmt.Errorf("audit failed: %s", strings.Join(t.Audit.Failures, "; "))
				}
				if verdict == "accept-with-followups" && len(t.Followups) == 0 {
					return fmt.Errorf("accept-with-followups requires harvested follow-ups")
				}
				t.State = "done"
			} else {
				t.State = "discarded"
			}
			note := idx.Tasks[id]
			_, _, e = mutateV7DocumentLocked(note.AbsolutePath, v7FrontmatterOrder["task"], func(d map[string]any, body string) (map[string]any, string, bool, error) {
				now := time.Now().UTC().Format(time.RFC3339)
				d["batch_state"] = t.State
				d["closed_at"] = now
				d["updated_at"] = now
				if t.State == "done" {
					d["status"] = "done"
					d["accepted_by"] = fallback(args.String("by"), "operator:coordinator")
					d["accepted_at"] = now
				} else {
					d["status"] = "cancelled"
					d["discarded_at"] = now
					d["discard_reason"] = args.String("note")
				}
				d["batch_close_verdict"] = verdict
				d["batch_close_note"] = args.String("note")
				d["batch_audit_override"] = args.String("override-audit")
				return d, body, true, nil
			})
			if e != nil {
				return e
			}
			if e = coordinatorSaveBatch(b); e != nil {
				return e
			}
			if e = coordinatorEvent(b, t, "coordinator_close", map[string]any{"verdict": verdict, "note": args.String("note"), "audit_override": args.String("override-audit")}); e != nil {
				return e
			}
			if args.Bool("json") {
				emitJSON(t)
			} else {
				fmt.Printf("%s %s\n", id, t.State)
			}
			return nil
		}
	}
	return fmt.Errorf("task has no batch run: %s", id)
}
func coordinatorRunsEvents(args Args) (bool, error) {
	vault, e := resolveVaultPath(args, false)
	if e != nil {
		return false, nil
	}
	batches, e := coordinatorBatches(vault)
	if e != nil {
		return false, e
	}
	id := firstNonEmpty(args.String("id"), args.String("_pos0"))
	for _, b := range batches {
		for _, t := range b.Tasks {
			if id != b.ID+":"+t.ID && id != t.ID {
				continue
			}
			path := filepath.Join(coordinatorRunRoot(vault, b.ID), t.ID, "events.jsonl")
			if args.Bool("follow") {
				return true, followLog(path)
			}
			text, e := readText(path)
			if e != nil {
				return true, e
			}
			n := 40
			if raw := args.String("lines"); raw != "" {
				n = atoiSafe(raw)
				if n < 1 {
					return true, fmt.Errorf("lines must be positive")
				}
			}
			tail := tailText(text, n)
			if args.Bool("json") {
				emitJSON(map[string]any{"batch": b.ID, "task": t.ID, "events": parseEventTail(tail)})
			} else {
				fmt.Print(tail)
			}
			return true, nil
		}
	}
	return false, nil
}
