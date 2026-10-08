package main

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

func coordinatorBodyRefErrors(vault, body string) []string {
	var out []string
	for _, m := range v7MarkdownLinkPattern.FindAllStringSubmatch(body, -1) {
		ref := strings.TrimSpace(m[2])
		if strings.HasPrefix(ref, "<") && strings.HasSuffix(ref, ">") {
			ref = strings.Trim(ref, "<>")
		}
		if u, e := url.Parse(ref); e == nil && (u.Scheme != "" || strings.HasPrefix(ref, "//")) {
			continue
		}
		if strings.HasPrefix(ref, "#") {
			if !coordinatorAnchorExists(body, strings.TrimPrefix(ref, "#")) {
				out = append(out, "missing body section: "+ref)
			}
			continue
		}
		ref = strings.SplitN(ref, ` "`, 2)[0]
		if decoded, e := url.PathUnescape(ref); e == nil {
			ref = decoded
		}
		if err := coordinatorFileReference(vault, ref); err != nil {
			out = append(out, err.Error())
		}
	}
	// Backticked documentation references are common in hand-written briefs.
	re := regexp.MustCompile("`((?:docs/|.tusker/specs/)[^`]+\\.md(?:#[^`]+)?)`")
	for _, m := range re.FindAllStringSubmatch(body, -1) {
		if e := coordinatorFileReference(vault, m[1]); e != nil {
			out = append(out, e.Error())
		}
	}
	return uniqueStrings(out)
}
func coordinatorAnchorExists(body, anchor string) bool {
	want := strings.ToLower(strings.TrimSpace(anchor))
	want = strings.ReplaceAll(want, " ", "-")
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "#") {
			continue
		}
		title := strings.TrimSpace(strings.TrimLeft(line, "#"))
		slug := strings.ToLower(title)
		slug = regexp.MustCompile(`[^\pL\pN_ -]`).ReplaceAllString(slug, "")
		slug = strings.ReplaceAll(slug, " ", "-")
		if slug == want || strings.EqualFold(title, anchor) {
			return true
		}
	}
	return false
}
func coordinatorFileReference(vault, ref string) error {
	path, anchor, _ := strings.Cut(ref, "#")
	if path == "" {
		return nil
	}
	if filepath.IsAbs(path) || strings.HasPrefix(filepath.Clean(path), "..") {
		return fmt.Errorf("reference escapes repository: %s", ref)
	}
	root := v7RepoRoot(vault)
	abs := filepath.Join(root, filepath.FromSlash(path))
	actual, e := filepath.EvalSymlinks(abs)
	if e != nil {
		return fmt.Errorf("dead reference: %s", ref)
	}
	if !pathWithin(root, actual) {
		return fmt.Errorf("reference escapes repository: %s", ref)
	}
	raw, e := os.ReadFile(actual)
	if e != nil {
		return fmt.Errorf("dead reference: %s", ref)
	}
	if anchor != "" && !coordinatorAnchorExists(string(raw), anchor) {
		return fmt.Errorf("dead section reference: %s", ref)
	}
	return nil
}
func coordinatorForks(vault, epic string) ([]coordinatorFork, error) {
	if epic == "" {
		return nil, nil
	}
	note, e := resolveV7Note(vault, epic, "epic")
	if e != nil {
		return nil, e
	}
	raw, e := json.Marshal(note.Data["forks"])
	if e != nil {
		return nil, e
	}
	var forks []coordinatorFork
	e = json.Unmarshal(raw, &forks)
	return forks, e
}
func coordinatorFlakyActive(vault string, f coordinatorFlaky, now time.Time) bool {
	if until, e := time.Parse("2006-01-02", f.Until); e == nil {
		return now.Before(until.Add(24 * time.Hour))
	}
	if until, e := time.Parse(time.RFC3339, f.Until); e == nil {
		return now.Before(until)
	}
	note, e := resolveV7Note(vault, f.Until, "task")
	if e != nil {
		return false
	}
	switch stringField(note.Data, "status") {
	case "done", "cancelled", "superseded":
		return false
	}
	return true
}
func coordinatorPacket(vault string, task Note, routing bool) (string, error) {
	if errors := coordinatorBodyRefErrors(vault, task.Body); len(errors) > 0 {
		return "", fmt.Errorf("invalid packet references: %s", strings.Join(errors, "; "))
	}
	idx, e := loadV7Index(vault)
	if e != nil {
		return "", e
	}
	for _, ref := range normalizeList(task.Data["spec_refs"]) {
		if msg, _ := v7SpecRefError(vault, ref, idx.Decisions); msg != "" {
			return "", fmt.Errorf("%s", msg)
		}
	}
	forks, e := coordinatorForks(vault, stringField(task.Data, "epic"))
	if e != nil {
		return "", e
	}
	registry, e := coordinatorLoadRegistry(vault)
	if e != nil {
		return "", e
	}
	var b strings.Builder
	b.WriteString(strings.TrimSpace(task.Body))
	b.WriteString("\n\n## Execution contract\n\n")
	fmt.Fprintf(&b, "Task: %s\nOwned paths: %s\nOff-limits: every other path unless REPORT.json names a reasoned exception.\n", stringField(task.Data, "id"), strings.Join(normalizeList(task.Data["owned_paths"]), ", "))
	if refs := normalizeList(task.Data["spec_refs"]); len(refs) > 0 {
		fmt.Fprintf(&b, "Read: %s\n", strings.Join(refs, ", "))
	}
	b.WriteString("\n## Already decided — do not stop on these\n\n")
	for _, f := range forks {
		if f.SupersededBy == "" {
			fmt.Fprintf(&b, "- %s: %s → %s (%s; %s)\n", f.ID, f.Question, f.Ruling, f.By, f.Doc)
		}
	}
	b.WriteString("\n## Flaky gates\n\nOnly listed, unexpired tests qualify. Record failures by exact test ID with an isolated_command; the batch requires two isolated passes before accepting that gate. Never mark it passed yourself.\n")
	for _, f := range registry.Flaky {
		if coordinatorFlakyActive(vault, f, time.Now()) {
			fmt.Fprintf(&b, "- %s (until %s): %s\n", f.Test, f.Until, f.Reason)
		}
	}
	sample := coordinatorReport{Schema: coordinatorReportSchema, Task: stringField(task.Data, "id"), Verdict: "pass", Commits: []string{}, Gates: []coordinatorGate{{Acceptance: "A1", Command: "go test ./affected", Result: "pass", Host: "local"}}, Questions: []coordinatorQuestion{}, Remaining: []coordinatorRemaining{}, Boxes: []coordinatorBox{}}
	raw, _ := json.Marshal(sample)
	fmt.Fprintf(&b, "\n## Report\n\nWrite REPORT.json and REPORT.md under $TUSKER_BATCH_RUN_DIR, never commit either. JSON schema example:\n```json\n%s\n```\nVerdict: pass, checkpoint, fail, or blocked. Every acceptance ID needs a gate with a command and result. Questions may cite an exact fork ID. Remaining items require title, why and evidence. Boxes use RFC3339 timestamps and eur_per_hour or final eur.\nDo not push. Commit as the configured Git user, without AI attribution. Read the repository AGENTS.md.\n", raw)
	if routing {
		b.WriteString("\n## Routing context\n\n")
		b.WriteString(v7Packet(vault, task, idx, "agent"))
	}
	return b.String(), nil
}
