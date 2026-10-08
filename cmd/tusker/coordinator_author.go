package main

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

func coordinatorForkCmd(args Args, action string) error {
	vault, e := resolveVaultPath(args, false)
	if e != nil {
		return e
	}
	epic := args.String("epic")
	if epic == "" {
		return fmt.Errorf("fork requires --epic")
	}
	note, e := resolveV7Note(vault, epic, "epic")
	if e != nil {
		return e
	}
	if action == "list" {
		forks, e := coordinatorForks(vault, epic)
		if e != nil {
			return e
		}
		if args.Bool("json") {
			emitJSON(forks)
		} else {
			for _, f := range forks {
				fmt.Printf("%s: %s → %s (superseded_by=%s)\n", f.ID, f.Question, f.Ruling, f.SupersededBy)
			}
		}
		return nil
	}
	id := firstNonEmpty(args.String("id"), args.String("_pos0"))
	if !coordinatorID.MatchString(id) {
		return fmt.Errorf("invalid fork ID")
	}
	doc := args.String("doc")
	if doc != "" {
		idx, e := loadV7Index(vault)
		if e != nil {
			return e
		}
		if msg, _ := v7SpecRefError(vault, doc, idx.Decisions); msg != "" {
			return fmt.Errorf("%s", msg)
		}
	}
	_, _, e = mutateV7DocumentLocked(note.AbsolutePath, v7FrontmatterOrder["epic"], func(d map[string]any, body string) (map[string]any, string, bool, error) {
		raw, e := json.Marshal(d["forks"])
		if e != nil {
			return d, body, false, e
		}
		var forks []coordinatorFork
		if e = json.Unmarshal(raw, &forks); e != nil {
			return d, body, false, e
		}
		switch action {
		case "add":
			if strings.TrimSpace(args.String("question")) == "" || strings.TrimSpace(args.String("ruling")) == "" {
				return d, body, false, fmt.Errorf("fork add requires question and ruling")
			}
			by := args.String("by")
			if by != "sarav" && by != "coordinator" {
				return d, body, false, fmt.Errorf("fork by must be sarav or coordinator")
			}
			for _, f := range forks {
				if f.ID == id {
					return d, body, false, fmt.Errorf("fork already exists: %s", id)
				}
			}
			forks = append(forks, coordinatorFork{ID: id, Question: args.String("question"), Ruling: args.String("ruling"), By: by, Date: time.Now().UTC(), Doc: doc})
		case "supersede":
			next := args.String("by")
			foundNext := false
			for _, f := range forks {
				if f.ID == next && f.SupersededBy == "" && next != id {
					foundNext = true
				}
			}
			if !foundNext {
				return d, body, false, fmt.Errorf("replacement must be another active fork")
			}
			found := false
			for i := range forks {
				if forks[i].ID == id {
					if forks[i].SupersededBy != "" {
						return d, body, false, fmt.Errorf("fork already superseded")
					}
					forks[i].SupersededBy = next
					found = true
				}
			}
			if !found {
				return d, body, false, fmt.Errorf("fork not found: %s", id)
			}
		}
		// Round-trip JSON into generic values so YAML uses the same field names.
		encoded, _ := json.Marshal(forks)
		var generic any
		_ = json.Unmarshal(encoded, &generic)
		d["forks"] = generic
		d["updated_at"] = time.Now().UTC().Format(time.RFC3339)
		return d, body, true, nil
	})
	if e != nil {
		return e
	}
	if args.Bool("json") {
		forks, e := coordinatorForks(vault, epic)
		if e != nil {
			return e
		}
		emitJSON(forks)
	}
	return nil
}
func coordinatorFlakyCmd(args Args, action string) error {
	vault, e := resolveVaultPath(args, false)
	if e != nil {
		return e
	}
	if action == "list" {
		r, e := coordinatorLoadRegistry(vault)
		if e != nil {
			return e
		}
		if args.Bool("json") {
			emitJSON(r.Flaky)
		} else {
			for _, f := range r.Flaky {
				fmt.Printf("%s active=%t until=%s %s\n", f.Test, coordinatorFlakyActive(vault, f, time.Now()), f.Until, f.Reason)
			}
		}
		return nil
	}
	test := args.String("_pos0")
	reason := args.String("reason")
	until := args.String("until")
	if test == "" || strings.TrimSpace(reason) == "" || until == "" {
		return fmt.Errorf("flaky add requires test, --reason, --until")
	}
	f := coordinatorFlaky{Test: test, Reason: reason, Until: until}
	if !coordinatorFlakyActive(vault, f, time.Now()) {
		return fmt.Errorf("flaky expiry must be future date/time or an open fix task")
	}
	return coordinatorMutateRegistry(vault, func(r *coordinatorRegistry) error {
		for i, old := range r.Flaky {
			if old.Test == test {
				r.Flaky[i] = f
				return nil
			}
		}
		r.Flaky = append(r.Flaky, f)
		return nil
	})
}

type coordinatorTriageRow struct {
	Task      string   `json:"task"`
	Revision  string   `json:"revision"`
	Title     string   `json:"title"`
	Signals   []string `json:"signals"`
	Action    string   `json:"action"`
	Reason    string   `json:"reason"`
	MergeInto string   `json:"merge_into,omitempty"`
}

func coordinatorTriageCmd(args Args) error {
	vault, e := resolveVaultPath(args, false)
	if e != nil {
		return e
	}
	idx, e := loadV7Index(vault)
	if e != nil {
		return e
	}
	age, e := coordinatorAge(args.String("older-than"))
	if e != nil {
		return e
	}
	rows := []coordinatorTriageRow{}
	titles := map[string][]string{}
	for id, t := range idx.Tasks {
		title := stringField(t.Data, "epic") + ":" + strings.ToLower(oneLine(stringField(t.Data, "title")))
		titles[title] = append(titles[title], id)
	}
	for id, t := range idx.Tasks {
		switch stringField(t.Data, "status") {
		case "done", "cancelled", "superseded":
			continue
		}
		if epic := args.String("epic"); epic != "" && stringField(t.Data, "epic") != epic {
			continue
		}
		row := coordinatorTriageRow{Task: id, Revision: stringField(t.Data, "state_rev"), Title: stringField(t.Data, "title"), Signals: []string{}, Action: "keep"}
		if at, ok := parseTuskerTime(firstNonEmpty(stringField(t.Data, "updated_at"), stringField(t.Data, "created_at"))); ok && time.Since(at) > age {
			row.Signals = append(row.Signals, "no activity since "+at.Format("2006-01-02"))
		}
		row.Signals = append(row.Signals, coordinatorBodyRefErrors(vault, t.Body)...)
		for _, ref := range normalizeList(t.Data["spec_refs"]) {
			if msg, _ := v7SpecRefError(vault, ref, idx.Decisions); msg != "" {
				row.Signals = append(row.Signals, msg)
			}
		}
		title := stringField(t.Data, "epic") + ":" + strings.ToLower(oneLine(row.Title))
		if len(titles[title]) > 1 {
			row.Signals = append(row.Signals, "duplicate title: "+strings.Join(titles[title], ", "))
		}
		created := stringField(t.Data, "created_at")
		owned := normalizeList(t.Data["owned_paths"])
		if created != "" && len(owned) > 0 {
			gitArgs := []string{"log", "--format=%h", "--since=" + created, "--"}
			gitArgs = append(gitArgs, owned...)
			if out, e := coordinatorGit(v7RepoRoot(vault), gitArgs...); e == nil && out != "" {
				row.Signals = append(row.Signals, "owned paths changed since creation")
			}
		}
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Task < rows[j].Task })
	if output := args.String("output"); output != "" {
		if _, e := os.Lstat(output); !os.IsNotExist(e) {
			return fmt.Errorf("triage output already exists")
		}
		if e := coordinatorWriteJSON(output, rows); e != nil {
			return e
		}
	}
	if args.Bool("json") {
		emitJSON(rows)
	} else {
		fmt.Printf("%d open tasks; review actions in --output JSON before triage apply.\n", len(rows))
		for _, row := range rows {
			fmt.Printf("%s | %s | %s\n", row.Task, row.Title, strings.Join(row.Signals, "; "))
		}
	}
	return nil
}
func coordinatorAge(raw string) (time.Duration, error) {
	if raw == "" {
		return 14 * 24 * time.Hour, nil
	}
	if strings.HasSuffix(raw, "d") {
		d, e := strconv.ParseInt(strings.TrimSuffix(raw, "d"), 10, 64)
		if e != nil || d <= 0 || d > math.MaxInt64/int64(24*time.Hour) {
			return 0, fmt.Errorf("older-than requires a positive duration")
		}
		return time.Duration(d) * 24 * time.Hour, nil
	}
	return coordinatorDuration(raw, 14*24*time.Hour)
}
func coordinatorTriageApplyCmd(args Args) error {
	vault, e := resolveVaultPath(args, false)
	if e != nil {
		return e
	}
	var rows []coordinatorTriageRow
	if e = coordinatorReadJSON(firstNonEmpty(args.String("file"), args.String("_pos0")), &rows); e != nil {
		return e
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
	actions := map[string]string{}
	for _, row := range rows {
		actions[row.Task] = row.Action
	}
	seen := map[string]bool{}
	for _, row := range rows {
		t, ok := idx.Tasks[row.Task]
		if !ok || seen[row.Task] {
			return fmt.Errorf("missing or duplicate triage task %s", row.Task)
		}
		seen[row.Task] = true
		if row.Revision == "" || row.Revision != stringField(t.Data, "state_rev") {
			return fmt.Errorf("triage task changed since review: %s", row.Task)
		}
		if row.Action != "keep" && row.Action != "discard" && row.Action != "merge" {
			return fmt.Errorf("invalid triage action")
		}
		if row.Action != "keep" && strings.TrimSpace(row.Reason) == "" {
			return fmt.Errorf("triage mutation requires a reason")
		}
		if row.Action == "merge" {
			target, ok := idx.Tasks[row.MergeInto]
			if !ok || row.MergeInto == row.Task || stringField(target.Data, "epic") != stringField(t.Data, "epic") || (actions[row.MergeInto] != "" && actions[row.MergeInto] != "keep") || (stringField(target.Data, "status") == "done" || stringField(target.Data, "status") == "cancelled" || stringField(target.Data, "status") == "superseded") {
				return fmt.Errorf("invalid merge target")
			}
		}
		if stringField(t.Data, "batch_state") == "running" {
			return fmt.Errorf("cannot triage running task %s", row.Task)
		}
	}
	for _, row := range rows {
		if row.Action == "keep" {
			continue
		}
		t := idx.Tasks[row.Task]
		_, _, e = mutateV7DocumentUnderMaterialLock(t.AbsolutePath, v7FrontmatterOrder["task"], func(d map[string]any, body string) (map[string]any, string, bool, error) {
			if row.Revision != stringField(d, "state_rev") {
				return d, body, false, fmt.Errorf("triage revision changed")
			}
			d["status"] = "cancelled"
			d["discarded_at"] = time.Now().UTC().Format(time.RFC3339)
			d["discard_reason"] = row.Reason
			d["discarded_by"] = fallback(args.String("by"), "operator:coordinator")
			if row.Action == "merge" {
				d["superseded_by"] = row.MergeInto
			}
			return d, body, true, nil
		})
		if e != nil {
			return e
		}
	}
	if args.Bool("json") {
		emitJSON(map[string]any{"applied": len(rows)})
	} else {
		fmt.Printf("Reviewed %d tasks; history preserved.\n", len(rows))
	}
	return nil
}
