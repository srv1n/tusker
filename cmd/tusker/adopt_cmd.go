package main

import (
	"fmt"
	"path"
	"sort"
	"strings"
	"time"
)

// adoptCmd records work that is already committed, whoever produced it. It
// selects tasks, then runs adopt_completed on each: Tusker reruns the task's
// command proof in the checkout and moves the task to done (a person or an
// authorized agent) or review (any other agent). No claim or work session is
// needed, so parallel agents, other harnesses and hand edits all land the
// same way.
func adoptCmd(args Args) error {
	vault, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	actor, err := requireArg(args, "by")
	if err != nil {
		return err
	}
	ids, err := adoptCandidates(vault, args)
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		return tuskerError(errorMissingArg, "tusker adopt needs task IDs, --wave <W-ID>, or --since <git-ref>")
	}
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		return err
	}
	defer store.Close()
	projectID, registered, err := registeredProjectIDForVault(store, vault)
	if err != nil {
		return err
	}
	if !registered {
		return tuskerError(errorNotFound, "tusker adopt requires a registered project; run `tusker projects add --repo .`")
	}
	wf, err := loadWorkflow(vault)
	if err != nil {
		return err
	}
	// Adopt in rounds so a dependency lands before its dependents, whatever
	// order the IDs arrive in. A round that adopts nothing ends the loop.
	results := map[string]serveRecoveryResult{}
	pending := ids
	for len(pending) > 0 {
		var retry []string
		for _, id := range pending {
			result, err := adoptCompletedWork(vault, store, projectID, id, actor, wf.Data.Retry.MaxAttempts, time.Now().UTC())
			if err != nil {
				result = serveRecoveryResult{Action: "adopt_completed", TaskID: id, Refused: true, Reason: err.Error()}
			}
			results[id] = result
			if !result.OK && strings.Contains(result.Reason, "unfinished dependency") {
				retry = append(retry, id)
			}
		}
		if len(retry) == len(pending) {
			break
		}
		pending = retry
	}
	ok := true
	out := make([]serveRecoveryResult, 0, len(ids))
	for _, id := range ids {
		out = append(out, results[id])
		ok = ok && results[id].OK
	}
	_ = sendDaemonControlOneWay(DefaultStateRoot(), daemonControlRequest{Command: "reconcile_project", ProjectID: projectID, Cause: "adopt"}, 250*time.Millisecond)
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": ok, "results": out})
	} else {
		for _, r := range out {
			outcome := "refused"
			if r.OK && r.Lane == runLaneReview {
				outcome = "review"
			} else if r.OK {
				outcome = "done"
			}
			fmt.Printf("%-12s %-8s %s\n", r.TaskID, outcome, r.Reason)
		}
	}
	if !ok {
		return afterResultEmitted(tuskerError(errorEvidenceGate, "some tasks were not adopted; fix the named reason and rerun tusker adopt"))
	}
	return nil
}

// adoptCandidates unions explicit IDs, wave members, and open tasks whose
// owned paths changed since a git ref. The ref form needs nothing from the
// agent that did the work: no trailer, claim or harness integration.
func adoptCandidates(vault string, args Args) ([]string, error) {
	var ids []string
	for _, id := range strings.Fields(args.String("_pos")) {
		ids = append(ids, strings.TrimSpace(id))
	}
	idx, err := loadV7Index(vault)
	if err != nil {
		return nil, err
	}
	if waveID := strings.TrimSpace(args.String("wave")); waveID != "" {
		wave, ok := idx.Waves[waveID]
		if !ok {
			return nil, tuskerError(errorNotFound, "wave not found: "+waveID)
		}
		ids = append(ids, normalizeList(wave.Data["members"])...)
	}
	if since := strings.TrimSpace(args.String("since")); since != "" {
		changed, err := gitOutputTrim(v7RepoRoot(vault), "diff", "--name-only", since+"..HEAD")
		if err != nil {
			return nil, tuskerError(errorInvalidArg, "cannot list files changed since "+since+": "+err.Error())
		}
		files := strings.Fields(changed)
		var touched []string
		for id, task := range idx.Tasks {
			switch strings.ToLower(stringField(task.Data, "status")) {
			case "done", "cancelled", "superseded":
				continue
			}
			if adoptOwnedPathsTouched(normalizeList(task.Data["owned_paths"]), files) {
				touched = append(touched, id)
			}
		}
		sort.Strings(touched)
		ids = append(ids, touched...)
	}
	return uniqueStringsPreserveOrder(ids), nil
}

func adoptOwnedPathsTouched(owned, files []string) bool {
	for _, pattern := range owned {
		pattern = strings.TrimSuffix(strings.TrimPrefix(pattern, "./"), "/")
		for _, file := range files {
			if matched, _ := path.Match(pattern, file); matched || file == pattern || strings.HasPrefix(file, pattern+"/") {
				return true
			}
		}
	}
	return false
}
