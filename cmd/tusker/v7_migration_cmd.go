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

func packetV7Cmd(args Args) error {
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	idx, err := loadV7Index(vaultPath)
	if err != nil {
		return err
	}
	id := firstNonEmpty(args.String("id"), args.String("_pos0"))
	if id == "" {
		return tuskerError(errorMissingArg, "Missing task id")
	}
	task, ok := idx.Tasks[id]
	if !ok {
		return tuskerError(errorNotFound, "V7 task not found: "+id)
	}
	audience := fallback(args.String("for"), "agent")
	if audience == "integrator" {
		if stringField(task.Data, "work_kind") != "integrator" {
			return tuskerError(errorInvalidArg, id+": integrator packet requires work_kind: integrator")
		}
		content := integratorPacket(vaultPath, task, idx)
		if args.Bool("write") {
			path := filepath.Join(vaultPath, "_generated", "packets", id+".integrator.md")
			if err := writeText(path, content); err != nil {
				return err
			}
			if !args.Bool("quiet") {
				fmt.Println(path)
			}
			return nil
		}
		fmt.Print(content)
		return nil
	}
	if audience == "agent" && !args.Bool("force") {
		if reasons := v7TaskDispatchBlockers(vaultPath, task); len(reasons) > 0 {
			return tuskerError(
				errorInvalidTransition,
				id+": task is not dispatchable",
				withHint("fix dispatch blockers or pass --force to inspect the packet anyway: "+strings.Join(reasons, "; ")),
				withContext(map[string]any{"id": id, "dispatch_blockers": reasons}),
			)
		}
	}
	content := v7Packet(vaultPath, task, idx, audience)
	if args.Bool("write") {
		path := filepath.Join(vaultPath, "_generated", "packets", id+"."+audience+".md")
		if err := writeText(path, content); err != nil {
			return err
		}
		if !args.Bool("quiet") {
			fmt.Println(path)
		}
		return nil
	}
	fmt.Print(content)
	return nil
}

func dashboardV7Cmd(args Args) error {
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	switch strings.ToLower(args.String("_pos0")) {
	case "build", "":
		idx, err := loadV7Index(vaultPath)
		if err != nil {
			return err
		}
		if err := buildV7Dashboards(vaultPath, idx); err != nil {
			return err
		}
		if !args.Bool("quiet") {
			fmt.Println("Built V7 dashboards.")
		}
		return nil
	case "open":
		name := fallback(args.String("_pos1"), "human-actions")
		fmt.Println(filepath.Join(vaultPath, "dashboards", name+".md"))
		return nil
	default:
		return tuskerError(errorInvalidArg, "Usage: tusker dashboard build|open <name>")
	}
}

func stateV7Cmd(args Args) error {
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	backend := v7GitStateBackend{VaultPath: vaultPath}
	switch strings.ToLower(args.String("_pos0")) {
	case "sync", "push", "":
		branch := fallback(args.String("branch"), v7StateBranch(vaultPath))
		remote := args.String("remote")
		if remote == "" && (args.Bool("push") || strings.ToLower(args.String("_pos0")) == "push") {
			remote = "origin"
		}
		commit, err := backend.Sync(context.Background(), v7StateSyncOptions{Branch: branch, Remote: remote, Message: args.String("message")})
		if err != nil {
			return err
		}
		if !args.Bool("quiet") {
			target := branch
			if remote != "" {
				target = remote + "/" + branch
			}
			fmt.Printf("Synced V7 runtime state to %s at %s\n", target, commit)
		}
		return nil
	case "import":
		branch := fallback(args.String("branch"), v7StateBranch(vaultPath))
		remote := args.String("remote")
		if remote == "" && args.Bool("fetch") {
			remote = "origin"
		}
		count, err := backend.Import(context.Background(), v7StateSyncOptions{Branch: branch, Remote: remote})
		if err != nil {
			return err
		}
		if !args.Bool("quiet") {
			fmt.Printf("Imported %d V7 lease%s from %s\n", count, plural(count), branch)
		}
		return nil
	case "export":
		dir := args.String("dir")
		if dir == "" {
			dir = filepath.Join(filepath.Dir(vaultPath), ".tusker-runtime", "state")
		}
		count, err := backend.Export(context.Background(), v7StateSyncOptions{Dir: dir})
		if err != nil {
			return err
		}
		if !args.Bool("quiet") {
			fmt.Printf("Exported %d V7 state file%s to %s\n", count, plural(count), dir)
		}
		return nil
	default:
		return tuskerError(errorInvalidArg, "Usage: tusker state sync|import|export")
	}
}

func hookInstallCmd(args Args) error {
	hookName := firstNonEmpty(args.String("hook"), args.String("_pos0"))
	if hookName == "" || hookName == "pre-commit" {
		return installV7PreCommitHook(args)
	}
	if hookName == "pre-push" {
		return installV7PrePushHook(args)
	}
	return tuskerError(errorInvalidArg, "unsupported Tusker hook: "+hookName, withHint("supported hooks: pre-commit, pre-push"))
}

func installV7PreCommitHook(args Args) error {
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	repoRoot := filepath.Dir(vaultPath)
	if err := exec.Command("git", "-C", repoRoot, "rev-parse", "--git-dir").Run(); err != nil {
		return tuskerError(errorInvalidArg, "Tusker hook install requires a Git repository", withPath(repoRoot))
	}
	out, err := exec.Command("git", "-C", repoRoot, "rev-parse", "--git-path", "hooks/pre-commit").Output()
	if err != nil {
		return err
	}
	hookPath := strings.TrimSpace(string(out))
	if !filepath.IsAbs(hookPath) {
		hookPath = filepath.Join(repoRoot, hookPath)
	}
	const marker = "tusker:pre-commit-branch-policy"
	if fileExists(hookPath) && !args.Bool("force") {
		existing, err := readText(hookPath)
		if err != nil {
			return err
		}
		if !strings.Contains(existing, marker) {
			return tuskerError(errorAlreadyExists, "refusing to overwrite existing pre-commit hook", withPath(hookPath), withHint("rerun with --force to replace it"))
		}
	}
	content := `#!/bin/sh
set -eu

# tusker:pre-commit-branch-policy
REPO_ROOT=$(git rev-parse --show-toplevel)
cd "$REPO_ROOT"

TUSKER_BIN="${TUSKER_BIN:-tusker}"
exec "$TUSKER_BIN" validate --staged --branch-policy
`
	if err := writeText(hookPath, content); err != nil {
		return err
	}
	if err := os.Chmod(hookPath, 0o755); err != nil {
		return err
	}
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": true, "hook": "pre-commit", "path": hookPath})
		return nil
	}
	if !args.Bool("quiet") {
		fmt.Printf("Installed Tusker pre-commit hook at %s\n", hookPath)
	}
	return nil
}

func installV7PrePushHook(args Args) error {
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	repoRoot := filepath.Dir(vaultPath)
	if err := exec.Command("git", "-C", repoRoot, "rev-parse", "--git-dir").Run(); err != nil {
		return tuskerError(errorInvalidArg, "Tusker hook install requires a Git repository", withPath(repoRoot))
	}
	out, err := exec.Command("git", "-C", repoRoot, "rev-parse", "--git-path", "hooks/pre-push").Output()
	if err != nil {
		return err
	}
	hookPath := strings.TrimSpace(string(out))
	if !filepath.IsAbs(hookPath) {
		hookPath = filepath.Join(repoRoot, hookPath)
	}
	const marker = "tusker:pre-push-main-guard"
	if fileExists(hookPath) && !args.Bool("force") {
		existing, err := readText(hookPath)
		if err != nil {
			return err
		}
		if !strings.Contains(existing, marker) {
			return tuskerError(errorAlreadyExists, "refusing to overwrite existing pre-push hook", withPath(hookPath), withHint("rerun with --force to replace it"))
		}
	}
	defaultBranch := v7DefaultBranch(vaultPath)
	content := fmt.Sprintf(`#!/bin/sh
set -eu

# %s
DEFAULT_BRANCH=%q
if [ "${%s:-}" = "1" ]; then
  exit 0
fi

while read local_ref local_sha remote_ref remote_sha
do
  case "$remote_ref" in
    refs/heads/$DEFAULT_BRANCH)
      echo "Tusker blocks direct pushes to $DEFAULT_BRANCH; use tusker land." >&2
      exit 1
      ;;
  esac
done
exit 0
`, marker, defaultBranch, tuskerLandMainGuardEnv)
	if err := writeText(hookPath, content); err != nil {
		return err
	}
	if err := os.Chmod(hookPath, 0o755); err != nil {
		return err
	}
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": true, "hook": "pre-push", "path": hookPath})
		return nil
	}
	if !args.Bool("quiet") {
		fmt.Printf("Installed Tusker pre-push hook at %s\n", hookPath)
	}
	return nil
}

func migrateV7Cmd(args Args) error {
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	notes, err := listAllNotes(vaultPath)
	if err != nil {
		return err
	}
	counts := map[string]int{"v5_tasks": 0, "blocked_tasks": 0, "tasks_with_work_log": 0, "tasks_with_evidence": 0}
	for _, note := range notes {
		if stringField(note.Data, "type") != "task" || !strings.HasSuffix(stringField(note.Data, "schema"), "/v5") {
			continue
		}
		counts["v5_tasks"]++
		if stringField(note.Data, "status") == "blocked" || stringField(note.Data, "block_reason") != "" || len(normalizeList(note.Data["blocked_by"])) > 0 {
			counts["blocked_tasks"]++
		}
		if findHeading(note.Body, "## Work log") != nil {
			counts["tasks_with_work_log"]++
		}
		if sectionHasSubstance(note.Body, "## Evidence") {
			counts["tasks_with_evidence"]++
		}
	}
	report := map[string]any{
		"ok":     true,
		"mode":   "dry-run",
		"counts": counts,
		"next": []string{
			"Create V7 gates from blocked V5 tasks.",
			"Extract raw logs into evidence or attempt records.",
			"Run `tusker reconcile --write` after conversion.",
		},
	}
	if args.Bool("write") {
		if err := bootstrapV7Dirs(vaultPath); err != nil {
			return err
		}
		created, err := migrateV7ExtractRecords(vaultPath, notes, args)
		if err != nil {
			return err
		}
		report["created"] = created
		report["mode"] = "write"
		report["created_layout"] = true
	}
	if args.Bool("json") {
		emitJSON(report)
		return nil
	}
	fmt.Printf("V7 migration %s: %d V5 task%s, %d blocked, %d with Work log, %d with evidence.\n",
		report["mode"], counts["v5_tasks"], plural(counts["v5_tasks"]), counts["blocked_tasks"], counts["tasks_with_work_log"], counts["tasks_with_evidence"])
	if !args.Bool("write") {
		fmt.Println("Dry run only. Use --write to create missing V7 layout directories.")
	}
	return nil
}

func migrateV7ExtractRecords(vaultPath string, notes []Note, args Args) (map[string]int, error) {
	created := map[string]int{"tasks": 0, "gates": 0, "evidence": 0, "attempts": 0, "source_rewrites": 0}
	gateProposals := v7GateProposalsFromV5(vaultPath, notes)
	gatesByTask := map[string][]v7GateMigrationProposal{}
	for _, proposal := range gateProposals {
		gatesByTask[proposal.TaskID] = append(gatesByTask[proposal.TaskID], proposal)
	}
	for _, proposal := range gateProposals {
		if fileExists(filepath.Join(vaultPath, "work", "gates", proposal.GateID+".md")) {
			continue
		}
		if err := newV7Gate(Args{
			"vault":            vaultPath,
			"quiet":            "true",
			"id":               proposal.GateID,
			"blocks":           proposal.TaskID,
			"kind":             "manual_hold",
			"owner":            fallback(args.String("owner"), "human:"+defaultActorName()),
			"title":            proposal.Title,
			"action":           proposal.Action,
			"verification":     proposal.Verification,
			"why-agent-cannot": "Migrated from V5 blocked-task metadata; the agent cannot safely infer or complete the human-owned blocker without owner input.",
		}); err != nil {
			return created, err
		}
		created["gates"]++
	}
	attemptOffset := map[string]int{}
	for _, note := range notes {
		if stringField(note.Data, "type") != "task" || !strings.HasSuffix(stringField(note.Data, "schema"), "/v5") {
			continue
		}
		taskID := stringField(note.Data, "id")
		if !v7TaskIDPattern.MatchString(taskID) {
			continue
		}
		if evidenceText := sectionContent(note.Body, "## Evidence"); strings.TrimSpace(evidenceText) != "" && !strings.Contains(evidenceText, "_No evidence yet") && !migratedV7EvidenceExists(vaultPath, taskID, "V5 task Evidence section") {
			id := fmt.Sprintf("%s-E-%s", taskID, padNumber(nextV7EvidenceSequence(vaultPath, taskID)))
			if !fileExists(filepath.Join(vaultPath, "evidence", taskID, id+".md")) {
				if err := writeMigratedV7Evidence(vaultPath, note, id, evidenceText); err != nil {
					return created, err
				}
				created["evidence"]++
			}
		}
		if verificationLog := sectionContent(note.Body, "## Verification log"); strings.TrimSpace(verificationLog) != "" && !strings.Contains(verificationLog, "_No verification yet") && !migratedV7EvidenceExists(vaultPath, taskID, "V5 task Verification log") {
			id := fmt.Sprintf("%s-E-%s", taskID, padNumber(nextV7EvidenceSequence(vaultPath, taskID)))
			if !fileExists(filepath.Join(vaultPath, "evidence", taskID, id+".md")) {
				if err := writeMigratedV7EvidenceRecord(vaultPath, note, id, "log_excerpt", "V5 task Verification log", verificationLog); err != nil {
					return created, err
				}
				created["evidence"]++
			}
		}
		if workLog := sectionContent(note.Body, "## Work log"); strings.TrimSpace(workLog) != "" && !migratedV7AttemptExists(vaultPath, taskID) {
			attemptOffset[taskID]++
			id := fmt.Sprintf("%s-A-%s", taskID, padNumber(nextV7AttemptSequence(vaultPath, taskID)+attemptOffset[taskID]-1))
			if !fileExists(filepath.Join(vaultPath, "attempts", taskID, id+".md")) {
				if err := writeMigratedV7Attempt(vaultPath, note, id, workLog); err != nil {
					return created, err
				}
				created["attempts"]++
			}
		}
	}
	for _, note := range notes {
		if stringField(note.Data, "type") != "task" || !strings.HasSuffix(stringField(note.Data, "schema"), "/v5") {
			continue
		}
		taskID := stringField(note.Data, "id")
		if !v7TaskIDPattern.MatchString(taskID) || fileExists(filepath.Join(vaultPath, "work", "tasks", taskID+".md")) {
			continue
		}
		if err := writeMigratedV7Task(vaultPath, note, gatesByTask[taskID]); err != nil {
			return created, err
		}
		created["tasks"]++
	}
	for _, note := range notes {
		if stringField(note.Data, "type") != "task" || !strings.HasSuffix(stringField(note.Data, "schema"), "/v5") {
			continue
		}
		result, err := compactNote(note, true, true, vaultPath)
		if err != nil {
			return created, err
		}
		if result.Written {
			created["source_rewrites"]++
		}
	}
	return created, nil
}

type v7GateMigrationProposal struct {
	TaskID       string `json:"task_id"`
	GateID       string `json:"gate_id"`
	Title        string `json:"title"`
	Action       string `json:"action"`
	Verification string `json:"verification"`
}

func v7GateProposalsFromV5(vaultPath string, notes []Note) []v7GateMigrationProposal {
	var proposals []v7GateMigrationProposal
	epicOffsets := map[string]int{}
	idx, _ := loadV7Index(vaultPath)
	for _, note := range notes {
		if stringField(note.Data, "type") != "task" || !strings.HasSuffix(stringField(note.Data, "schema"), "/v5") {
			continue
		}
		taskID := stringField(note.Data, "id")
		reason := strings.TrimSpace(stringField(note.Data, "block_reason"))
		if reason == "" && len(normalizeList(note.Data["blocked_by"])) > 0 {
			reason = "Wait for dependencies: " + strings.Join(normalizeList(note.Data["blocked_by"]), ", ")
		}
		if reason == "" || !v7TaskIDPattern.MatchString(taskID) {
			continue
		}
		action := v7GateMigrationAction(taskID, reason)
		if existing := existingV7GateForMigration(idx, taskID, reason); existing != "" {
			proposals = append(proposals, v7GateMigrationProposal{
				TaskID:       taskID,
				GateID:       existing,
				Title:        "Resolve blocker for " + taskID,
				Action:       action,
				Verification: "Blocker is resolved and the task can proceed.",
			})
			continue
		}
		epic := v7EpicFromTaskID(taskID)
		epicOffsets[epic]++
		gateID := fmt.Sprintf("%s-G-%s", epic, padNumber(nextV7Sequence(vaultPath, epic, "gate")+epicOffsets[epic]-1))
		proposals = append(proposals, v7GateMigrationProposal{
			TaskID:       taskID,
			GateID:       gateID,
			Title:        "Resolve blocker for " + taskID,
			Action:       action,
			Verification: "Blocker is resolved and the task can proceed.",
		})
	}
	return proposals
}

func v7GateMigrationAction(taskID, reason string) string {
	if v7GateTextIsPlaceholder(reason) {
		return "Clarify migrated V5 blocker for " + taskID + "."
	}
	return reason
}

func existingV7GateForMigration(idx v7Index, taskID, action string) string {
	for _, gate := range idx.Gates {
		if !containsString(normalizeList(gate.Data["blocks"]), taskID) {
			continue
		}
		if strings.TrimSpace(stringField(gate.Data, "action")) == strings.TrimSpace(action) {
			return stringField(gate.Data, "id")
		}
	}
	return ""
}

func writeMigratedV7Task(vaultPath string, task Note, gateProposals []v7GateMigrationProposal) error {
	taskID := stringField(task.Data, "id")
	now := time.Now().UTC().Format(time.RFC3339)
	gateIDs := make([]string, 0, len(gateProposals))
	for _, proposal := range gateProposals {
		gateIDs = append(gateIDs, proposal.GateID)
	}
	dependencies := normalizeV7IDs(normalizeList(task.Data["blocked_by"]), v7TaskIDPattern)
	status := v7StatusFromV5Status(stringField(task.Data, "status"))
	readiness, nextOwner, nextSource, nextRef, nextAction := migratedV7Projection(task, status, gateProposals, dependencies)
	risk := strings.ToLower(fallback(stringField(task.Data, "risk"), "medium"))
	proofMode := defaultV7ProofMode(risk)
	data := map[string]any{
		"schema":                "tusker.task/v7",
		"kind":                  "task",
		"id":                    taskID,
		"project":               v7ProjectID(vaultPath),
		"title":                 stringField(task.Data, "title"),
		"epic":                  wikiTarget(task.Data["epic"]),
		"status":                status,
		"readiness":             readiness,
		"priority":              strings.ToLower(fallback(stringField(task.Data, "priority"), "p2")),
		"risk":                  risk,
		"size":                  strings.ToLower(fallback(stringField(task.Data, "size"), "m")),
		"proof_mode":            proofMode,
		"proof_status":          "pending",
		"proof_required":        defaultV7ProofRequired(proofMode),
		"evidence_budget":       defaultV7EvidenceBudget(proofMode),
		"raw_artifacts_allowed": false,
		"next_owner":            nextOwner,
		"next_source":           nextSource,
		"next_ref":              nextRef,
		"next_action":           nextAction,
		"domains":               normalizeList(task.Data["domains"]),
		"gates":                 gateIDs,
		"dependencies":          dependencies,
		"evidence_required":     []string{},
		"created_at":            v7MigratedTimestamp(task.Data, "created", now),
		"created_by":            "tusker:migrate-v7",
		"updated_at":            now,
		"updated_by":            "tusker:migrate-v7",
	}
	if status == "done" {
		closedAt := v7MigratedTimestamp(task.Data, "completed", now)
		data["proof_status"] = "satisfied"
		data["accepted_by"] = "tusker:migrate-v7"
		data["accepted_at"] = closedAt
		data["closed_at"] = closedAt
	}
	body := migratedV7TaskBody(vaultPath, task, gateIDs)
	data["state_rev"] = v7StateRev(data, body)
	content, err := serializeDocument(data, body, v7FrontmatterOrder["task"])
	if err != nil {
		return err
	}
	if err := writeText(filepath.Join(vaultPath, "work", "tasks", taskID+".md"), content); err != nil {
		return err
	}
	return emitV7Event(vaultPath, taskID, "task", "created", "tusker:migrate-v7", map[string]any{"source": task.RelativePath})
}

func migratedV7Projection(task Note, status string, gateProposals []v7GateMigrationProposal, dependencies []string) (string, string, string, string, string) {
	switch status {
	case "done", "cancelled", "superseded":
		return status, "none", "status", "", ""
	case "review":
		return "waiting_on_review", "reviewer", "review_policy", "", "Review migrated evidence and close or return to rework."
	}
	if len(gateProposals) > 0 {
		gate := gateProposals[0]
		return "blocked_by_gate", "human:" + defaultActorName(), "gate", gate.GateID, gate.Action
	}
	if len(dependencies) > 0 {
		dep := dependencies[0]
		return "blocked_by_dependency", "blocked_dependency", "dependency", dep, "Wait for dependency " + dep + " to reach done."
	}
	return "ready", migratedNextOwner(task), "task", stringField(task.Data, "id"), "Execute the migrated task contract and satisfy proof mode."
}

func migratedNextOwner(task Note) string {
	assignee := strings.TrimSpace(stringField(task.Data, "assignee"))
	if assignee == "" {
		return "agent"
	}
	if strings.Contains(assignee, ":") {
		return assignee
	}
	return "agent:" + assignee
}

func v7StatusFromV5Status(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "draft", "backlog":
		return "backlog"
	case "review":
		return "review"
	case "rework":
		return "rework"
	case "done":
		return "done"
	case "cancelled":
		return "cancelled"
	default:
		return "ready"
	}
}

func normalizeV7IDs(values []string, pattern *regexp.Regexp) []string {
	var out []string
	for _, value := range values {
		id := wikiTarget(value)
		if pattern.MatchString(id) && !containsString(out, id) {
			out = append(out, id)
		}
	}
	return out
}

func v7MigratedTimestamp(data map[string]any, key, fallbackValue string) string {
	value := strings.TrimSpace(stringField(data, key))
	if value == "" {
		return fallbackValue
	}
	if _, err := time.Parse(time.RFC3339, value); err == nil {
		return value
	}
	if parsed, err := time.Parse("2006-01-02", value); err == nil {
		return parsed.UTC().Format(time.RFC3339)
	}
	return fallbackValue
}

func migratedV7TaskBody(vaultPath string, task Note, gateIDs []string) string {
	taskID := stringField(task.Data, "id")
	title := stringField(task.Data, "title")
	intent := compactMigratedSection(sectionContent(task.Body, "## Intent"), "Migrated V5 task. Confirm intent before execution.", 16)
	acceptance := compactMigratedSection(sectionContent(task.Body, "## Acceptance contract"), "| ID | Outcome | Proof |\n|---|---|---|\n| A1 | Complete the migrated task contract. | Inline verification, evidence, gate, or waiver |", 28)
	verification := compactMigratedSection(sectionContent(task.Body, "## Verification plan"), "| Covers | Check | Result | Notes |\n|---|---|---|---|\n| A1 | Migrated verification plan review | pending | Satisfy proof_mode before close. |", 16)
	knowledge := compactMigratedSection(sectionContent(task.Body, "## Knowledge delta"), "None recorded during migration.", 16)
	evidenceLinks := migratedV7EvidenceLinks(vaultPath, taskID)
	if len(evidenceLinks) == 0 {
		evidenceLinks = []string{"Pending."}
	}
	gateLinks := "None."
	if len(gateIDs) > 0 {
		gateLinks = v7BulletList(gateIDs)
	}
	return fmt.Sprintf(`# %s · %s

## Intent

%s

## Acceptance

%s

## Non-goals

- Preserve migrated task semantics; do not treat migration as fresh product scope.

## Verification

%s

## Gates

%s

## Evidence

%s

## Knowledge delta

%s

## Migration source

- %s
`, taskID, title, intent, acceptance, verification, gateLinks, strings.Join(evidenceLinks, "\n"), knowledge, task.RelativePath)
}
