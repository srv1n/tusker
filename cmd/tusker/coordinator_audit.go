package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

func coordinatorReadReport(dir, id string) (coordinatorReport, error) {
	var r coordinatorReport
	e := coordinatorReadJSON(filepath.Join(dir, "REPORT.json"), &r)
	if e != nil {
		return r, e
	}
	if r.Schema != coordinatorReportSchema || r.Task != id {
		return r, fmt.Errorf("report schema/task mismatch")
	}
	switch r.Verdict {
	case "pass", "checkpoint", "fail", "blocked":
	default:
		return r, fmt.Errorf("invalid report verdict %q", r.Verdict)
	}
	for _, f := range r.Remaining {
		if strings.TrimSpace(f.Title) == "" || strings.TrimSpace(f.Why) == "" || strings.TrimSpace(f.Evidence) == "" {
			return r, fmt.Errorf("remaining items require title, why, evidence")
		}
	}
	return r, nil
}
func coordinatorGit(root string, args ...string) (string, error) {
	out, e := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput()
	if e != nil {
		return "", fmt.Errorf("git %s: %s", args[0], oneLine(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}
func coordinatorAuditReport(vault string, t *coordinatorTask, r coordinatorReport, dir string, lockFiles ...*os.File) coordinatorAudit {
	a := coordinatorAudit{Passed: true, Failures: []string{}, Warnings: []string{"No-push check detects local evidence only; absence of evidence does not prove no remote write."}}
	fail := func(s string) { a.Failures = append(a.Failures, s); a.Passed = false }
	note, e := resolveV7Note(vault, t.ID, "task")
	if e != nil {
		fail(e.Error())
		return a
	}
	if t.Contract != "" && coordinatorContract(note) != t.Contract {
		fail("task contract changed during run")
	}
	if r.Verdict != "pass" {
		fail("report verdict is " + r.Verdict)
	}
	if _, e := os.Stat(filepath.Join(dir, "REPORT.md")); e != nil {
		fail("REPORT.md missing")
	}
	acceptance := v7AcceptanceIDs(note.Body)
	if len(acceptance) == 0 {
		fail("no acceptance IDs in task contract")
	}
	for _, id := range acceptance {
		passed := false
		for _, g := range r.Gates {
			if containsString(splitCSV(g.Acceptance), id) && strings.TrimSpace(g.Command) != "" && (g.Result == "pass" || strings.HasPrefix(g.Result, "pass (flaky:")) {
				passed = true
			}
		}
		if !passed {
			fail("acceptance " + id + " has no passing command gate")
		}
	}
	for _, g := range r.Gates {
		if strings.HasPrefix(g.Result, "pass") && strings.TrimSpace(g.Command) == "" {
			fail("pass gate without command")
		}
	}
	if dirt, err := coordinatorGit(t.Workspace, "status", "--porcelain"); err != nil {
		fail(err.Error())
	} else if dirt != "" {
		fail("uncommitted changes remain in worker workspace")
	}
	if refs, err := coordinatorGit(t.Workspace, "for-each-ref", "--format=%(refname) %(objectname)", "refs/remotes"); err != nil {
		fail(err.Error())
	} else if refs != t.RemoteRefs {
		fail("remote-tracking refs changed during run; investigate fetch/push activity")
	}
	name, e1 := coordinatorGit(t.Workspace, "config", "user.name")
	email, e2 := coordinatorGit(t.Workspace, "config", "user.email")
	if e1 != nil || e2 != nil || name == "" || email == "" {
		fail("configured Git author missing")
	}
	expectedAuthor := name + " <" + email + ">"
	if t.Author != "" {
		if expectedAuthor != t.Author {
			fail("configured Git author changed during run")
		}
		expectedAuthor = t.Author
	}
	branch, e := coordinatorGit(t.Workspace, "symbolic-ref", "--short", "HEAD")
	if e != nil || t.Branch != "" && branch != t.Branch {
		fail("target branch changed during run")
	}
	attribution := regexp.MustCompile(`(?im)(co-authored-by:.*(?:claude|codex|devin|muse|copilot|chatgpt|gemini|cursor|openai|anthropic|gpt|agent)|claude-session:|generated (?:with|by).*(?:claude|codex|devin|muse|copilot|chatgpt|gemini|cursor|openai|anthropic|gpt|agent))`)
	seen := map[string]bool{}
	for _, commit := range r.Commits {
		if !regexp.MustCompile(`^[0-9a-fA-F]{7,64}$`).MatchString(commit) {
			fail("invalid commit ID " + commit)
			continue
		}
		sha, e := coordinatorGit(t.Workspace, "rev-parse", "--verify", commit+"^{commit}")
		if e != nil {
			fail(e.Error())
			continue
		}
		if seen[sha] {
			continue
		}
		seen[sha] = true
		if _, e = coordinatorGit(t.Workspace, "merge-base", "--is-ancestor", sha, "HEAD"); e != nil {
			fail("commit not on target branch: " + commit)
		}
		if t.Base != "" {
			if _, e = coordinatorGit(t.Workspace, "merge-base", "--is-ancestor", sha, t.Base); e == nil {
				fail("report lists pre-existing commit: " + commit)
			}
		}
		author, e := coordinatorGit(t.Workspace, "show", "-s", "--format=%an <%ae>", sha)
		if e != nil || author != expectedAuthor {
			fail("unexpected author on " + commit + ": " + author)
		}
		message, e := coordinatorGit(t.Workspace, "show", "-s", "--format=%B", sha)
		if e != nil {
			fail(e.Error())
		} else if attribution.MatchString(message) {
			fail("AI attribution on " + commit)
		}
		// -m catches each parent of merge commits; -z preserves spaces/newlines in paths.
		out, e := exec.Command("git", "-C", t.Workspace, "diff-tree", "--root", "-m", "--no-commit-id", "--name-only", "-r", "-z", sha).Output()
		if e != nil {
			fail("cannot inspect " + commit)
			continue
		}
		for _, path := range strings.Split(string(out), "\x00") {
			if path == "" {
				continue
			}
			if _, e := coordinatorGit(t.Workspace, "check-ignore", "--no-index", "--", path); e == nil {
				fail("committed ignored path: " + path)
			}
			if strings.Contains(path, ".tusker/scratch/") || strings.HasPrefix(path, ".tusker/scratch/") {
				fail("committed scratch path: " + path)
			}
			allowed := false
			for _, p := range t.Owned {
				p = strings.TrimSuffix(filepath.ToSlash(filepath.Clean(p)), "/")
				if p == "." || path == p || strings.HasPrefix(path, p+"/") {
					allowed = true
				}
			}
			for _, exception := range r.Exceptions {
				if exception.Path == path && strings.TrimSpace(exception.Reason) != "" {
					allowed = true
				}
			}
			if !allowed {
				fail("changed path outside ownership: " + path)
			}
		}
	}
	// A single isolated workspace must account for every new commit, even one omitted from REPORT.json.
	if t.Base != "" {
		all, e := coordinatorGit(t.Workspace, "rev-list", t.Base+"..HEAD")
		if e != nil {
			fail(e.Error())
		} else {
			for _, sha := range strings.Fields(all) {
				if !seen[sha] {
					fail("unreported commit: " + sha)
				}
			}
		}
	}
	log, e := os.ReadFile(filepath.Join(dir, "run.log"))
	if e != nil {
		fail("run.log missing")
	} else if regexp.MustCompile(`(?m)(?:^\s*\$?\s*|"command"\s*:\s*")git\s+(?:-C\s+\S+\s+)?push\b`).Match(log) {
		fail("git push observed in run log")
	}
	hooks, _ := filepath.Glob(filepath.Join(vault, "audit.d", "*"))
	for _, hook := range hooks {
		info, e := os.Stat(hook)
		if e != nil || info.IsDir() || info.Mode()&0111 == 0 {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		cmd := exec.CommandContext(ctx, hook, append([]string{t.ID}, r.Commits...)...)
		cmd.Dir = t.Workspace
		out, e := coordinatorRunCheck(cmd, dir, lockFiles, time.Minute)
		cancel()
		if e != nil {
			fail("audit hook " + filepath.Base(hook) + ": " + oneLine(string(out)))
		}
	}
	a.Failures = uniqueStrings(a.Failures)
	return a
}
func coordinatorAuditCmd(args Args) error {
	vault, e := resolveVaultPath(args, false)
	if e != nil {
		return e
	}
	id := firstNonEmpty(args.String("id"), args.String("_pos0"))
	batches, e := coordinatorBatches(vault)
	if e != nil {
		return e
	}
	bound := ""
	if note, err := resolveV7Note(vault, id, "task"); err == nil {
		bound = stringField(note.Data, "batch_id")
	}
	found := false
	allPass := true
	results := map[string]coordinatorAudit{}
	for _, b := range batches {
		for _, t := range b.Tasks {
			if b.ID != id && (t.ID != id || bound != "" && b.ID != bound) {
				continue
			}
			found = true
			dir := coordinatorAttemptDir(b, t)
			r, e := coordinatorReadReport(dir, t.ID)
			if e != nil {
				results[t.ID] = coordinatorAudit{Failures: []string{e.Error()}}
				allPass = false
				continue
			}
			locks, err := coordinatorAcquireResources(b, t)
			if err != nil {
				return fmt.Errorf("cannot audit active task %s: %w", t.ID, err)
			}
			a := coordinatorAuditReport(vault, t, r, dir, locks...)
			coordinatorLocksClose(locks)
			results[t.ID] = a
			allPass = allPass && a.Passed
		}
	}
	if !found {
		return fmt.Errorf("no batch run found for %s", id)
	}
	if args.Bool("json") {
		emitJSON(results)
	} else {
		for task, a := range results {
			fmt.Printf("%s pass=%t\n", task, a.Passed)
			for _, f := range a.Failures {
				fmt.Println("  " + f)
			}
		}
	}
	if !allPass {
		return afterResultEmitted(fmt.Errorf("mechanical audit failed"))
	}
	return nil
}
