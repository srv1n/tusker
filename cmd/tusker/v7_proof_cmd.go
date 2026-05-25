package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

var v7VerificationResults = makeSet("pass", "fail", "blocked", "skipped", "waived", "pending")

type v7VerificationRow struct {
	CoverText string
	Check     string
	Result    string
	Notes     string
	BlockedBy string
}

type v7ProofReport struct {
	TaskID            string              `json:"task_id"`
	Mode              string              `json:"mode"`
	Status            string              `json:"status"`
	Acceptance        []string            `json:"acceptance"`
	Covered           map[string][]string `json:"covered"`
	Missing           []string            `json:"missing"`
	ModeMissing       []string            `json:"mode_missing"`
	MachineMissing    []string            `json:"machine_missing"`
	HumanMissing      []string            `json:"human_missing"`
	ReviewerMissing   []string            `json:"reviewer_missing,omitempty"`
	ExternalMissing   []string            `json:"external_missing"`
	OpenGates         []string            `json:"open_gates"`
	OpenMachineGates  []string            `json:"open_machine_gates"`
	OpenHumanGates    []string            `json:"open_human_gates"`
	OpenReviewerGates []string            `json:"open_reviewer_gates,omitempty"`
	OpenExternalGates []string            `json:"open_external_gates"`
	SatisfiedGates    []string            `json:"satisfied_gates"`
	ExternalBlockers  []string            `json:"external_blockers,omitempty"`
	TerminalWait      bool                `json:"terminal_wait"`
	AgentAction       string              `json:"agent_action"`
	GapOwners         map[string]string   `json:"gap_owners,omitempty"`
	GateOwners        map[string]string   `json:"gate_owners,omitempty"`
	InlineRows        []v7VerificationRow `json:"inline_rows"`
	Evidence          []string            `json:"evidence"`
	ProofOwner        map[string]string   `json:"proof_owner,omitempty"`
	OwnerHints        map[string]string   `json:"owner_hints,omitempty"`
}

func defaultV7ProofMode(risk string) string {
	switch strings.ToLower(strings.TrimSpace(risk)) {
	case "critical":
		return "audit"
	default:
		return "inline"
	}
}

func defaultV7ProofRequired(mode string) []string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "none":
		return nil
	case "card":
		return []string{"focused_test", "broad_test"}
	case "artifact":
		return []string{"build", "manual_smoke", "screenshot"}
	case "audit":
		return []string{"focused_test", "broad_test", "human_signoff"}
	default:
		return []string{"focused_test", "broad_test"}
	}
}

func defaultV7EvidenceBudget(mode string) int {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "card":
		return 1
	case "artifact":
		return 3
	case "audit":
		return 5
	default:
		return 0
	}
}

func proofV7Cmd(args Args) error {
	switch strings.ToLower(args.String("_pos0")) {
	case "status":
		args["id"] = firstNonEmpty(args.String("id"), args.String("_pos1"))
		return proofV7StatusCmd(args)
	case "set-mode":
		args["id"] = firstNonEmpty(args.String("id"), args.String("_pos1"))
		args["mode"] = firstNonEmpty(args.String("mode"), args.String("_pos2"))
		return proofV7SetModeCmd(args)
	case "recipe":
		args["id"] = firstNonEmpty(args.String("id"), args.String("_pos1"))
		return proofV7RecipeCmd(args)
	default:
		return tuskerError(errorMissingArg, "Usage: tusker proof status <task-id> | tusker proof set-mode <task-id> none|inline|card|artifact|audit | tusker proof recipe <task-id>")
	}
}

func proofV7StatusCmd(args Args) error {
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	taskID, err := requireArg(args, "id")
	if err != nil {
		return err
	}
	idx, err := loadV7Index(vaultPath)
	if err != nil {
		return err
	}
	task, ok := idx.Tasks[taskID]
	if !ok {
		return tuskerError(errorNotFound, "V7 task not found: "+taskID)
	}
	report := computeV7ProofReport(vaultPath, task, idx)
	if args.Bool("json") {
		emitJSON(report)
		return nil
	}
	if !args.Bool("verbose") {
		printV7ProofStatusConcise(report)
		return nil
	}
	printV7ProofStatusVerbose(report)
	return nil
}

func printV7ProofStatusConcise(report v7ProofReport) {
	fmt.Printf("Task: %s\n", report.TaskID)
	fmt.Printf("Proof mode: %s\n", report.Mode)
	fmt.Printf("Proof status: %s\n\n", report.Status)
	fmt.Println("Missing gaps:")
	printV7OwnedProofGapGroup("machine", report.MachineMissing)
	printV7OwnedProofGapGroup("human", report.HumanMissing)
	printV7OwnedProofGapGroup("reviewer", report.ReviewerMissing)
	printV7OwnedProofGapGroup("external", report.ExternalMissing)
	fmt.Printf("\nAgent action: %s\n", fallback(report.AgentAction, "continue"))
	fmt.Println("\nEvidence summary:")
	fmt.Printf("  inline rows: %d\n", len(report.InlineRows))
	fmt.Printf("  evidence records: %d\n", len(report.Evidence))
	if len(report.Evidence) > 0 {
		for _, line := range v7EvidenceStatusSummary(report.Evidence) {
			fmt.Printf("  %s\n", line)
		}
	}
	fmt.Println("\nOpen gates:")
	printV7OwnedProofGateGroup("machine", report.OpenMachineGates)
	printV7OwnedProofGateGroup("human", report.OpenHumanGates)
	printV7OwnedProofGateGroup("reviewer", report.OpenReviewerGates)
	printV7OwnedProofGateGroup("external", report.OpenExternalGates)
	fmt.Println("\nExternal/shared blockers:")
	printV7ProofBlockerGroup(report.ExternalBlockers)
}

func printV7ProofStatusVerbose(report v7ProofReport) {
	fmt.Printf("Task: %s\n", report.TaskID)
	fmt.Printf("Proof mode: %s\n", report.Mode)
	fmt.Printf("Proof status: %s\n\n", report.Status)
	fmt.Println("Acceptance coverage:")
	if len(report.Acceptance) == 0 {
		fmt.Println("  none recorded")
	} else {
		for _, acceptance := range report.Acceptance {
			if report.Mode == "none" {
				fmt.Printf("  %s: not required\n", acceptance)
				continue
			}
			status := "covered"
			detail := strings.Join(report.Covered[acceptance], ", ")
			if detail == "" {
				status = "pending"
				detail = "no proof"
			}
			fmt.Printf("  %s: %s, %s\n", acceptance, status, detail)
		}
	}
	if len(report.ModeMissing) > 0 {
		fmt.Printf("\nProof mode gaps:\n")
		for _, missing := range report.ModeMissing {
			fmt.Printf("  - %s\n", missing)
			if owner := report.GapOwners[missing]; owner != "" {
				fmt.Printf("    owner: %s\n", owner)
			}
		}
	}
	if report.AgentAction != "" {
		fmt.Printf("\nAgent action: %s\n", report.AgentAction)
	}
	fmt.Println("\nInline verification:")
	if len(report.InlineRows) == 0 {
		fmt.Println("  none")
	} else {
		for _, row := range report.InlineRows {
			suffix := ""
			if row.BlockedBy != "" {
				suffix = " (blocked_by: " + row.BlockedBy + ")"
			}
			fmt.Printf("  %s: %s — %s%s\n", row.CoverText, row.Result, row.Check, suffix)
		}
	}
	fmt.Println("\nEvidence:")
	if len(report.Evidence) == 0 {
		fmt.Println("  none")
	} else {
		for _, evidence := range report.Evidence {
			fmt.Printf("  %s\n", evidence)
		}
	}
	fmt.Println("\nOpen gates:")
	if len(report.OpenGates) == 0 {
		fmt.Println("  none")
	} else {
		for _, gate := range report.OpenGates {
			fmt.Printf("  %s\n", gate)
		}
	}
	fmt.Println("\nExternal/shared blockers:")
	printV7ProofBlockerGroup(report.ExternalBlockers)
}

func printV7OwnedProofGapGroup(owner string, gaps []string) {
	fmt.Printf("  %s:", owner)
	if len(gaps) == 0 {
		fmt.Println(" none")
		return
	}
	fmt.Println()
	for _, gap := range gaps {
		fmt.Printf("    - %s\n", gap)
	}
}

func printV7OwnedProofGateGroup(owner string, gates []string) {
	fmt.Printf("  %s:", owner)
	if len(gates) == 0 {
		fmt.Println(" none")
		return
	}
	fmt.Println()
	for _, gate := range gates {
		fmt.Printf("    - %s\n", gate)
	}
}

func printV7ProofBlockerGroup(blockers []string) {
	if len(blockers) == 0 {
		fmt.Println("  none")
		return
	}
	for _, blocker := range blockers {
		fmt.Printf("  - %s\n", blocker)
	}
}

func v7EvidenceStatusSummary(evidence []string) []string {
	counts := map[string]int{}
	for _, item := range evidence {
		fields := strings.Fields(item)
		status := "unknown"
		if len(fields) >= 3 {
			status = strings.ToLower(strings.TrimSpace(fields[len(fields)-1]))
		}
		counts[status]++
	}
	var statuses []string
	for status := range counts {
		statuses = append(statuses, status)
	}
	sort.Strings(statuses)
	var out []string
	for _, status := range statuses {
		count := counts[status]
		out = append(out, fmt.Sprintf("%s: %d", status, count))
	}
	return out
}

func proofV7SetModeCmd(args Args) error {
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	taskID, err := requireArg(args, "id")
	if err != nil {
		return err
	}
	mode := strings.ToLower(strings.TrimSpace(args.String("mode")))
	if _, ok := v7ProofModes[mode]; !ok {
		return tuskerError(errorInvalidField, "invalid proof_mode: "+mode)
	}
	note, err := resolveV7Note(vaultPath, taskID, "task")
	if err != nil {
		return err
	}
	data, body, err := parseFrontmatterMustRead(note.AbsolutePath)
	if err != nil {
		return err
	}
	baseRev := stringField(data, "state_rev")
	data["proof_mode"] = mode
	required := splitCSV(firstNonEmpty(args.String("required"), args.String("proof-required")))
	if len(required) == 0 {
		required = defaultV7ProofRequired(mode)
	}
	data["proof_required"] = required
	if budget := strings.TrimSpace(args.String("evidence-budget")); budget != "" {
		data["evidence_budget"] = atoiSafe(budget)
	} else {
		data["evidence_budget"] = defaultV7EvidenceBudget(mode)
	}
	if args.String("raw-artifacts-allowed") != "" {
		data["raw_artifacts_allowed"] = args.Bool("raw-artifacts-allowed")
	}
	if reason := strings.TrimSpace(args.String("raw-artifacts-reason")); reason != "" {
		data["raw_artifacts_reason"] = reason
	}
	idx, err := loadV7Index(vaultPath)
	if err != nil {
		return err
	}
	task := note
	task.Data = data
	task.Body = body
	report := computeV7ProofReport(vaultPath, task, idx)
	if report.Status == "satisfied" && len(v7PacketStubAcceptanceItems(body)) > 0 && len(v7AcceptanceWaivers(data)) == 0 {
		report.Status = "partial"
	}
	data["proof_status"] = report.Status
	data["updated_at"] = time.Now().UTC().Format(time.RFC3339)
	data["updated_by"] = fallback(args.String("by"), "agent:"+defaultActorName())
	if _, err := saveV7DocumentCAS(note.AbsolutePath, data, body, v7FrontmatterOrder["task"], baseRev); err != nil {
		return err
	}
	if !args.Bool("quiet") {
		fmt.Printf("%s proof_mode=%s proof_status=%s\n", taskID, mode, report.Status)
	}
	return emitV7Event(vaultPath, taskID, "task", "updated", stringField(data, "updated_by"), map[string]any{"proof_mode": mode, "proof_status": report.Status})
}

func verifyV7AddCmd(args Args) error {
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	taskID := firstNonEmpty(args.String("id"), args.String("_pos1"))
	if strings.TrimSpace(taskID) == "" {
		return tuskerError(errorMissingArg, "verify add requires <task-id>")
	}
	rows, err := parseV7VerifyAddRows(args)
	if err != nil {
		return err
	}
	status, err := upsertV7Verifications(vaultPath, taskID, rows, fallback(args.String("by"), "agent:"+defaultActorName()), args)
	if err != nil {
		return err
	}
	if !args.Bool("quiet") {
		fmt.Printf("Added %d verification row%s for %s; proof_status=%s\n", len(rows), plural(len(rows)), taskID, status)
		if report, err := loadV7ProofReport(vaultPath, taskID); err == nil {
			fmt.Printf("Remaining proof gaps: %s\n", v7ProofRemainingGapSummary(report))
		}
	}
	return nil
}

func loadV7ProofReport(vaultPath, taskID string) (v7ProofReport, error) {
	idx, err := loadV7Index(vaultPath)
	if err != nil {
		return v7ProofReport{}, err
	}
	task, ok := idx.Tasks[taskID]
	if !ok {
		return v7ProofReport{}, tuskerError(errorNotFound, "V7 task not found: "+taskID)
	}
	return computeV7ProofReport(vaultPath, task, idx), nil
}

func v7ProofRemainingGapSummary(report v7ProofReport) string {
	var parts []string
	add := func(owner string, gaps, gates []string) {
		items := append([]string{}, gaps...)
		for _, gate := range gates {
			items = append(items, "open_gate:"+gate)
		}
		if len(items) > 0 {
			parts = append(parts, owner+": "+strings.Join(items, ", "))
		}
	}
	add("machine", report.MachineMissing, report.OpenMachineGates)
	add("reviewer", report.ReviewerMissing, report.OpenReviewerGates)
	add("human", report.HumanMissing, report.OpenHumanGates)
	add("external", report.ExternalMissing, report.OpenExternalGates)
	if len(parts) == 0 {
		return "none"
	}
	return strings.Join(parts, "; ")
}

func parseV7VerifyAddRows(args Args) ([]v7VerificationRow, error) {
	if batchFile := strings.TrimSpace(args.String("batch-file")); batchFile != "" {
		text, err := readText(batchFile)
		if err != nil {
			return nil, err
		}
		return parseV7VerificationRowsArg(text)
	}
	if rowsText := strings.TrimSpace(args.String("rows")); rowsText != "" {
		return parseV7VerificationRowsArg(rowsText)
	}
	row := v7VerificationRow{
		CoverText: firstNonEmpty(args.String("covers"), args.String("cover")),
		Check:     args.String("check"),
		Result:    strings.ToLower(fallback(args.String("result"), "pass")),
		Notes:     firstNonEmpty(args.String("note"), args.String("notes")),
		BlockedBy: firstNonEmpty(args.String("blocked-by"), args.String("blocked_by")),
	}
	if err := validateV7VerificationRow(row); err != nil {
		return nil, err
	}
	return []v7VerificationRow{row}, nil
}

func parseV7VerificationRowsArg(raw string) ([]v7VerificationRow, error) {
	rows, err := parseV7FinishVerificationRows(raw)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, tuskerError(errorMissingArg, "verify add batch requires at least one row")
	}
	for _, row := range rows {
		if err := validateV7VerificationRow(row); err != nil {
			return nil, err
		}
	}
	return rows, nil
}

func validateV7VerificationRow(row v7VerificationRow) error {
	if row.CoverText == "" {
		return tuskerError(errorMissingArg, "verify add requires --covers A1 or --covers A1,A2")
	}
	if strings.TrimSpace(row.Check) == "" {
		return tuskerError(errorMissingArg, "verify add requires --check")
	}
	if _, ok := v7VerificationResults[row.Result]; !ok || row.Result == "pending" {
		return tuskerError(errorInvalidField, "invalid verification result: "+row.Result)
	}
	if row.Result == "blocked" && strings.TrimSpace(row.BlockedBy) == "" {
		return tuskerError(errorMissingArg, "verify add --result blocked requires --blocked-by <path-or-task>")
	}
	return nil
}

func upsertV7Verification(vaultPath, taskID string, row v7VerificationRow, actor string) (string, error) {
	return upsertV7Verifications(vaultPath, taskID, []v7VerificationRow{row}, actor, nil)
}

func upsertV7Verifications(vaultPath, taskID string, rows []v7VerificationRow, actor string, args Args) (string, error) {
	if len(rows) == 0 {
		return "", tuskerError(errorMissingArg, "verify add requires at least one verification row")
	}
	for _, row := range rows {
		if err := validateV7VerificationRow(row); err != nil {
			return "", err
		}
	}
	retryCommand := v7VerificationRetryCommand(taskID, rows, args)
	var status string
	err := withV7ProofWriteLock(vaultPath, taskID, args, retryCommand, func() error {
		nextStatus, err := upsertV7VerificationsLocked(vaultPath, taskID, rows, actor, args)
		status = nextStatus
		return err
	})
	if err != nil {
		return "", err
	}
	return status, nil
}

func upsertV7VerificationsLocked(vaultPath, taskID string, rows []v7VerificationRow, actor string, args Args) (string, error) {
	note, err := resolveV7Note(vaultPath, taskID, "task")
	if err != nil {
		return "", err
	}
	data, body, err := parseFrontmatterMustRead(note.AbsolutePath)
	if err != nil {
		return "", err
	}
	baseRev := stringField(data, "state_rev")
	for _, row := range rows {
		body = upsertV7VerificationRow(body, row)
	}
	idx, err := loadV7Index(vaultPath)
	if err != nil {
		return "", err
	}
	task := note
	task.Data = data
	task.Body = body
	report := computeV7ProofReport(vaultPath, task, idx)
	if report.Status == "satisfied" && len(v7PacketStubAcceptanceItems(body)) > 0 && len(v7AcceptanceWaivers(data)) == 0 {
		report.Status = "partial"
	}
	data["proof_status"] = report.Status
	data["updated_at"] = time.Now().UTC().Format(time.RFC3339)
	data["updated_by"] = actor
	if _, err := saveV7DocumentCAS(note.AbsolutePath, data, body, v7FrontmatterOrder["task"], baseRev); err != nil {
		return "", v7VerificationCASError(err, note.AbsolutePath, taskID, rows, args)
	}
	for _, row := range rows {
		if err := emitV7Event(vaultPath, taskID, "task", "verification_added", actor, map[string]any{"covers": row.CoverText, "check": row.Check, "result": row.Result, "blocked_by": row.BlockedBy}); err != nil {
			return "", err
		}
	}
	return report.Status, nil
}

func withV7ProofWriteLock(vaultPath, taskID string, args Args, retryCommand string, fn func() error) error {
	release, err := acquireV7ProofWriteLock(vaultPath, taskID, v7ProofLockTimeout(args))
	if err != nil {
		if typed, ok := err.(*TuskerError); ok {
			if typed.Code == "PROOF_WRITE_BUSY" {
				return tuskerError(typed.Code, typed.Message, withPath(typed.Path), withHint("retry exactly: "+retryCommand), withContext(map[string]any{"retry_command": retryCommand}))
			}
			return typed
		}
		return err
	}
	defer release()
	return fn()
}

func acquireV7ProofWriteLock(vaultPath, taskID string, timeout time.Duration) (func(), error) {
	lockPath := filepath.Join(vaultPath, ".tusker", "locks", "proof-"+taskID+".lock")
	if err := ensureDir(filepath.Dir(lockPath)); err != nil {
		return nil, err
	}
	deadline := time.Now().Add(timeout)
	for {
		file, err := os.OpenFile(lockPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err == nil {
			_, _ = fmt.Fprintf(file, "pid=%d\nat=%s\n", os.Getpid(), time.Now().UTC().Format(time.RFC3339Nano))
			_ = file.Close()
			return func() { _ = os.Remove(lockPath) }, nil
		}
		if !os.IsExist(err) {
			return nil, err
		}
		if timeout <= 0 || time.Now().After(deadline) {
			return nil, tuskerError("PROOF_WRITE_BUSY", taskID+": proof write lock is held", withPath(lockPath))
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func v7ProofLockTimeout(args Args) time.Duration {
	if args != nil {
		if raw := strings.TrimSpace(args.String("lock-timeout-ms")); raw != "" {
			if ms := atoiSafe(raw); ms >= 0 {
				return time.Duration(ms) * time.Millisecond
			}
		}
	}
	return 2 * time.Second
}

func v7VerificationCASError(err error, path, taskID string, rows []v7VerificationRow, args Args) error {
	typed, ok := err.(*TuskerError)
	if !ok || typed.Code != "CAS_CONFLICT" {
		return err
	}
	retry := v7VerificationRetryCommand(taskID, rows, args)
	return tuskerError("CAS_CONFLICT", taskID+": verification write conflicted with a newer proof edit", withPath(path), withHint("retry exactly: "+retry), withContext(map[string]any{"retry_command": retry, "cause": errorToIssue(err)}))
}

func v7VerificationRetryCommand(taskID string, rows []v7VerificationRow, args Args) string {
	parts := []string{"tusker", "verify", "add", taskID}
	if args != nil {
		if vault := strings.TrimSpace(args.String("vault")); vault != "" {
			parts = append(parts, "--vault", vault)
		}
		if batchFile := strings.TrimSpace(args.String("batch-file")); batchFile != "" {
			parts = append(parts, "--batch-file", batchFile)
			return v7ShellCommand(parts)
		}
		if rowsText := strings.TrimSpace(args.String("rows")); rowsText != "" && len(rows) != 1 {
			parts = append(parts, "--rows", rowsText)
			return v7ShellCommand(parts)
		}
	}
	if len(rows) == 1 {
		row := rows[0]
		parts = append(parts, "--covers", row.CoverText, "--check", row.Check, "--result", row.Result)
		if strings.TrimSpace(row.Notes) != "" {
			parts = append(parts, "--note", row.Notes)
		}
		if strings.TrimSpace(row.BlockedBy) != "" {
			parts = append(parts, "--blocked-by", row.BlockedBy)
		}
		return v7ShellCommand(parts)
	}
	parts = append(parts, "--rows", renderV7VerificationRowsArg(rows))
	return v7ShellCommand(parts)
}

func renderV7VerificationRowsArg(rows []v7VerificationRow) string {
	var lines []string
	for _, row := range rows {
		parts := []string{row.CoverText, row.Check, row.Result, row.Notes}
		if strings.TrimSpace(row.BlockedBy) != "" {
			parts = append(parts, row.BlockedBy)
		}
		lines = append(lines, strings.Join(parts, "|"))
	}
	return strings.Join(lines, "\n")
}

func v7ShellCommand(parts []string) string {
	quoted := make([]string, 0, len(parts))
	for _, part := range parts {
		quoted = append(quoted, v7ShellQuote(part))
	}
	return strings.Join(quoted, " ")
}

func v7ShellQuote(value string) string {
	if value == "" {
		return "''"
	}
	if strings.IndexFunc(value, func(r rune) bool {
		return !(r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || strings.ContainsRune("@%_+=:,./-", r))
	}) == -1 {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

func upsertV7VerificationRow(body string, row v7VerificationRow) string {
	row.Result = strings.ToLower(strings.TrimSpace(row.Result))
	rows := parseV7VerificationRows(body)
	var kept []v7VerificationRow
	replaced := false
	for _, existing := range rows {
		if strings.EqualFold(strings.TrimSpace(existing.Check), "TBD") || existing.Result == "pending" {
			continue
		}
		if strings.EqualFold(existing.CoverText, row.CoverText) && strings.EqualFold(existing.Check, row.Check) {
			kept = append(kept, row)
			replaced = true
			continue
		}
		kept = append(kept, existing)
	}
	if !replaced {
		kept = append(kept, row)
	}
	table := renderV7VerificationTable(kept)
	if findHeading(body, "## Verification") == nil {
		return strings.TrimRight(body, "\n") + "\n\n## Verification\n\n" + table + "\n"
	}
	return replaceSection(body, "## Verification", table)
}

func renderV7VerificationTable(rows []v7VerificationRow) string {
	hasBlocker := false
	for _, row := range rows {
		if strings.TrimSpace(row.BlockedBy) != "" {
			hasBlocker = true
			break
		}
	}
	lines := []string{
		"| Covers | Check | Result | Notes |",
		"|---|---|---|---|",
	}
	if hasBlocker {
		lines = []string{
			"| Covers | Check | Result | Notes | Blocked By |",
			"|---|---|---|---|---|",
		}
	}
	for _, row := range rows {
		cells := []string{
			escapeV7TableCell(row.CoverText),
			escapeV7TableCell(row.Check),
			escapeV7TableCell(row.Result),
			escapeV7TableCell(row.Notes),
		}
		if hasBlocker {
			cells = append(cells, escapeV7TableCell(row.BlockedBy))
		}
		lines = append(lines, "| "+strings.Join(cells, " | ")+" |")
	}
	return strings.Join(lines, "\n")
}

func escapeV7TableCell(value string) string {
	value = strings.ReplaceAll(strings.TrimSpace(value), "\n", " ")
	value = strings.ReplaceAll(value, "|", `\|`)
	if value == "" {
		return "-"
	}
	return value
}

func parseV7VerificationRows(body string) []v7VerificationRow {
	content := sectionContent(body, "## Verification")
	var rows []v7VerificationRow
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "|") {
			continue
		}
		cells := v7MarkdownTableCells(trimmed)
		if len(cells) < 3 {
			continue
		}
		if strings.EqualFold(cells[0], "covers") || strings.Trim(cells[0], "-: ") == "" {
			continue
		}
		notes := ""
		if len(cells) > 3 {
			notes = cells[3]
		}
		blockedBy := ""
		if len(cells) > 4 {
			blockedBy = cells[4]
			if blockedBy == "-" {
				blockedBy = ""
			}
		}
		result := strings.ToLower(strings.TrimSpace(cells[2]))
		if result == "" {
			result = "pending"
		}
		rows = append(rows, v7VerificationRow{
			CoverText: cells[0],
			Check:     cells[1],
			Result:    result,
			Notes:     notes,
			BlockedBy: blockedBy,
		})
	}
	return rows
}

func v7MarkdownTableCells(line string) []string {
	trimmed := strings.TrimSpace(line)
	if strings.HasPrefix(trimmed, "|") {
		trimmed = trimmed[1:]
	}
	if strings.HasSuffix(trimmed, "|") && !v7EscapedAt(trimmed, len(trimmed)-1) {
		trimmed = trimmed[:len(trimmed)-1]
	}
	var cells []string
	var cell strings.Builder
	escaped := false
	for i := 0; i < len(trimmed); i++ {
		ch := trimmed[i]
		if escaped {
			if ch == '|' {
				cell.WriteByte('|')
			} else {
				cell.WriteByte('\\')
				cell.WriteByte(ch)
			}
			escaped = false
			continue
		}
		if ch == '\\' {
			escaped = true
			continue
		}
		if ch == '|' {
			cells = append(cells, strings.TrimSpace(cell.String()))
			cell.Reset()
			continue
		}
		cell.WriteByte(ch)
	}
	if escaped {
		cell.WriteByte('\\')
	}
	cells = append(cells, strings.TrimSpace(cell.String()))
	return cells
}

func v7EscapedAt(value string, pos int) bool {
	if pos <= 0 || pos >= len(value) {
		return false
	}
	backslashes := 0
	for i := pos - 1; i >= 0 && value[i] == '\\'; i-- {
		backslashes++
	}
	return backslashes%2 == 1
}

func computeV7ProofReport(vaultPath string, task Note, idx v7Index) v7ProofReport {
	taskID := stringField(task.Data, "id")
	mode := strings.ToLower(fallback(stringField(task.Data, "proof_mode"), defaultV7ProofMode(stringField(task.Data, "risk"))))
	acceptanceIDs := v7AcceptanceIDs(task.Body)
	report := v7ProofReport{
		TaskID:     taskID,
		Mode:       mode,
		Acceptance: acceptanceIDs,
		Covered:    map[string][]string{},
	}
	report.InlineRows = parseV7VerificationRows(task.Body)
	for _, row := range report.InlineRows {
		if strings.EqualFold(row.Result, "blocked") && strings.TrimSpace(row.BlockedBy) != "" {
			report.ExternalBlockers = append(report.ExternalBlockers, v7VerificationBlockerSummary(row))
		}
		if !v7VerificationResultCovers(row.Result) {
			continue
		}
		for _, acceptance := range v7CoverTextToAcceptanceIDs(row.CoverText, acceptanceIDs) {
			report.Covered[acceptance] = append(report.Covered[acceptance], "inline:"+row.Check)
		}
	}
	for _, evidence := range idx.Evidence[taskID] {
		id := stringField(evidence.Data, "id")
		status := stringField(evidence.Data, "status")
		kind := stringField(evidence.Data, "evidence_kind")
		report.Evidence = append(report.Evidence, fmt.Sprintf("%s %s %s", id, kind, status))
		if !v7EvidenceUsableForProof(evidence) {
			continue
		}
		for _, acceptance := range v7CoversToAcceptanceIDs(normalizeList(evidence.Data["covers"]), acceptanceIDs) {
			report.Covered[acceptance] = append(report.Covered[acceptance], "evidence:"+id)
		}
	}
	for _, gate := range idx.Gates {
		if !v7GateTouchesTask(gate, taskID) {
			continue
		}
		gateID := stringField(gate.Data, "id")
		if stringField(gate.Data, "status") == "open" && boolField(gate.Data, "blocking") {
			report.OpenGates = append(report.OpenGates, gateID)
			continue
		}
		if stringField(gate.Data, "status") != "satisfied" && stringField(gate.Data, "status") != "waived" {
			continue
		}
		report.SatisfiedGates = append(report.SatisfiedGates, gateID)
		covers := v7CoversToAcceptanceIDs(normalizeList(gate.Data["covers"]), acceptanceIDs)
		if len(covers) == 0 && containsString(normalizeList(gate.Data["blocks"]), taskID) {
			covers = append(covers, acceptanceIDs...)
		}
		for _, acceptance := range covers {
			report.Covered[acceptance] = append(report.Covered[acceptance], "gate:"+gateID)
		}
	}
	for _, acceptance := range v7AcceptanceWaivers(task.Data) {
		report.Covered[acceptance] = append(report.Covered[acceptance], "waiver")
	}
	if mode == "none" {
		report.ModeMissing = v7ProofModeRequirementMissing(task, idx)
		report.Status = v7ComputedProofStatus(task, report)
		classifyV7ProofReport(&report, task, idx)
		return report
	}
	for _, acceptance := range acceptanceIDs {
		if len(report.Covered[acceptance]) == 0 {
			report.Missing = append(report.Missing, acceptance)
		}
	}
	report.ModeMissing = v7ProofModeRequirementMissing(task, idx)
	report.Status = v7ComputedProofStatus(task, report)
	classifyV7ProofReport(&report, task, idx)
	return report
}

func classifyV7ProofReport(report *v7ProofReport, task Note, idx v7Index) {
	if report == nil {
		return
	}
	report.GapOwners = map[string]string{}
	report.GateOwners = map[string]string{}
	report.ProofOwner = v7TaskProofOwnerHints(task)
	taskID := stringField(task.Data, "id")
	acceptanceIDs := v7AcceptanceIDs(task.Body)
	blockedAcceptanceOwners := v7BlockedAcceptanceOwners(taskID, report.InlineRows, acceptanceIDs)
	blockedRequirementOwners := v7BlockedRequirementOwners(taskID, task, report.InlineRows)
	for _, missing := range report.Missing {
		gap := "acceptance:" + missing
		owner := blockedAcceptanceOwners[missing]
		if owner == "" {
			owner = classifyV7AcceptanceGapOwner(taskID, missing, acceptanceIDs, idx)
		}
		report.GapOwners[gap] = owner
		appendV7OwnedProofGap(report, owner, gap)
	}
	for _, missing := range report.ModeMissing {
		owner := "machine"
		if strings.HasPrefix(missing, "proof_required:") {
			required := strings.TrimPrefix(missing, "proof_required:")
			owner = blockedRequirementOwners[required]
			if owner == "" {
				owner = classifyProofRequirement(required, task, idx)
			}
		}
		report.GapOwners[missing] = owner
		appendV7OwnedProofGap(report, owner, missing)
	}
	for _, gateID := range report.OpenGates {
		gate, ok := idx.Gates[gateID]
		if !ok {
			report.OpenMachineGates = append(report.OpenMachineGates, gateID)
			continue
		}
		owner := classifyV7GateOwner(gate)
		report.GateOwners[gateID] = owner
		switch owner {
		case "human":
			report.OpenHumanGates = append(report.OpenHumanGates, gateID)
		case "reviewer":
			report.OpenReviewerGates = append(report.OpenReviewerGates, gateID)
		case "external":
			report.OpenExternalGates = append(report.OpenExternalGates, gateID)
		default:
			report.OpenMachineGates = append(report.OpenMachineGates, gateID)
		}
	}
	report.MachineMissing = uniqueStrings(report.MachineMissing)
	report.HumanMissing = uniqueStrings(report.HumanMissing)
	report.ReviewerMissing = uniqueStrings(report.ReviewerMissing)
	report.ExternalMissing = uniqueStrings(report.ExternalMissing)
	report.OpenMachineGates = uniqueStrings(report.OpenMachineGates)
	report.OpenHumanGates = uniqueStrings(report.OpenHumanGates)
	report.OpenReviewerGates = uniqueStrings(report.OpenReviewerGates)
	report.OpenExternalGates = uniqueStrings(report.OpenExternalGates)
	report.ExternalBlockers = uniqueStrings(report.ExternalBlockers)
	if len(report.MachineMissing) == 0 &&
		len(report.ReviewerMissing) == 0 &&
		len(report.ExternalMissing) == 0 &&
		len(report.OpenMachineGates) == 0 &&
		len(report.OpenReviewerGates) == 0 &&
		len(report.OpenExternalGates) == 0 &&
		(len(report.HumanMissing) > 0 || len(report.OpenHumanGates) > 0) {
		report.TerminalWait = true
		report.AgentAction = "stop_until_human_response"
	}
}

func v7ProofReportMachineComplete(report v7ProofReport) bool {
	return len(report.MachineMissing) == 0 &&
		len(report.ReviewerMissing) == 0 &&
		len(report.ExternalMissing) == 0 &&
		len(report.OpenMachineGates) == 0 &&
		len(report.OpenReviewerGates) == 0 &&
		len(report.OpenExternalGates) == 0
}

func appendV7OwnedProofGap(report *v7ProofReport, owner, gap string) {
	switch owner {
	case "human":
		report.HumanMissing = append(report.HumanMissing, gap)
	case "reviewer":
		report.ReviewerMissing = append(report.ReviewerMissing, gap)
	case "external":
		report.ExternalMissing = append(report.ExternalMissing, gap)
	default:
		report.MachineMissing = append(report.MachineMissing, gap)
	}
}

func classifyV7AcceptanceGapOwner(taskID, acceptance string, acceptanceIDs []string, idx v7Index) string {
	for _, gate := range sortedV7Gates(idx) {
		if stringField(gate.Data, "status") != "open" || !boolField(gate.Data, "blocking") || !v7GateTouchesTask(gate, taskID) {
			continue
		}
		covers := v7CoversToAcceptanceIDs(normalizeList(gate.Data["covers"]), acceptanceIDs)
		if len(covers) == 0 && containsString(normalizeList(gate.Data["blocks"]), taskID) {
			covers = append(covers, acceptanceIDs...)
		}
		if containsString(covers, acceptance) {
			return classifyV7GateOwner(gate)
		}
	}
	return "machine"
}

func v7BlockedAcceptanceOwners(taskID string, rows []v7VerificationRow, acceptanceIDs []string) map[string]string {
	owners := map[string]string{}
	for _, row := range rows {
		if !strings.EqualFold(row.Result, "blocked") || strings.TrimSpace(row.BlockedBy) == "" {
			continue
		}
		owner := classifyV7VerificationBlocker(taskID, row.BlockedBy)
		for _, acceptance := range v7CoverTextToAcceptanceIDs(row.CoverText, acceptanceIDs) {
			if owners[acceptance] == "" || owner == "external" {
				owners[acceptance] = owner
			}
		}
	}
	return owners
}

func v7BlockedRequirementOwners(taskID string, task Note, rows []v7VerificationRow) map[string]string {
	owners := map[string]string{}
	for _, row := range rows {
		if !strings.EqualFold(row.Result, "blocked") || strings.TrimSpace(row.BlockedBy) == "" {
			continue
		}
		owner := classifyV7VerificationBlocker(taskID, row.BlockedBy)
		for _, required := range v7TaskProofRequired(task) {
			if v7InlineVerificationSatisfies(required, row) && (owners[required] == "" || owner == "external") {
				owners[required] = owner
			}
		}
	}
	return owners
}

func classifyV7VerificationBlocker(taskID, blocker string) string {
	blocker = strings.TrimSpace(blocker)
	if blocker == "" {
		return "machine"
	}
	if owner := v7ProofOwnerClass(blocker); owner != "" && owner != "machine" {
		return owner
	}
	upper := strings.ToUpper(blocker)
	if v7TaskIDPattern.MatchString(upper) && upper != taskID {
		return "external"
	}
	if v7GateIDPattern.MatchString(upper) {
		return "external"
	}
	if strings.HasPrefix(strings.ToLower(blocker), "agent:") {
		return "external"
	}
	if strings.Contains(blocker, "/") || strings.Contains(blocker, "\\") || strings.Contains(blocker, ".") {
		return "external"
	}
	return "external"
}

func v7VerificationBlockerSummary(row v7VerificationRow) string {
	parts := []string{row.BlockedBy}
	if row.CoverText != "" {
		parts = append(parts, "covers "+row.CoverText)
	}
	if row.Check != "" {
		parts = append(parts, row.Check)
	}
	if row.Notes != "" && row.Notes != "-" {
		parts = append(parts, row.Notes)
	}
	return strings.Join(parts, " - ")
}

func classifyProofRequirement(required string, task Note, idx v7Index) string {
	required = strings.ToLower(strings.TrimSpace(required))
	required = strings.ReplaceAll(required, "-", "_")
	if owner := v7ProofOwnerClass(v7TaskProofOwnerHints(task)[required]); owner != "" {
		return owner
	}
	if owner := proofOwnerFromTaskOrGate(required, task, idx); owner != "" {
		return owner
	}
	switch required {
	case "human_signoff", "manual_smoke", "physical_smoke", "release_smoke", "security_review", "privacy_review", "accessibility_review":
		return "human"
	case "ci", "provider_probe":
		return "external"
	default:
		return "machine"
	}
}

func proofOwnerFromTaskOrGate(required string, task Note, idx v7Index) string {
	taskID := stringField(task.Data, "id")
	for _, gate := range sortedV7Gates(idx) {
		if !v7GateTouchesTask(gate, taskID) {
			continue
		}
		if stringField(gate.Data, "status") != "open" || !boolField(gate.Data, "blocking") {
			continue
		}
		if !v7GateCouldOwnProofRequirement(required, gate) {
			continue
		}
		if owner := classifyV7GateOwner(gate); owner != "" {
			return owner
		}
	}
	return ""
}

func v7GateCouldOwnProofRequirement(required string, gate Note) bool {
	if v7GateKindSatisfiesProofRequired(required, gate) {
		return true
	}
	return v7GateTextSatisfiesProofRequirement(required, gate)
}

func v7GateTextSatisfiesProofRequirement(required string, gate Note) bool {
	text := strings.ToLower(strings.Join([]string{
		stringField(gate.Data, "title"),
		stringField(gate.Data, "gate_kind"),
		stringField(gate.Data, "action"),
		stringField(gate.Data, "verification"),
		stringField(gate.Data, "satisfaction_evidence"),
	}, " "))
	requiredText := strings.ReplaceAll(required, "_", " ")
	if strings.Contains(text, required) || strings.Contains(text, requiredText) {
		return true
	}
	switch required {
	case "manual_smoke", "physical_smoke":
		return strings.Contains(text, "smoke") && (strings.Contains(text, "manual") || strings.Contains(text, "device") || strings.Contains(text, "physical"))
	case "human_signoff":
		return strings.Contains(text, "signoff") || strings.Contains(text, "sign off") || strings.Contains(text, "human")
	default:
		return false
	}
}

func classifyV7GateOwner(gate Note) string {
	if owner := v7ProofOwnerClass(stringField(gate.Data, "owner")); owner != "" {
		return owner
	}
	switch strings.ToLower(stringField(gate.Data, "gate_kind")) {
	case "ci", "external_service", "quota":
		return "external"
	case "auth", "env", "setup", "dev_host", "verification", "signoff", "manual_hold", "security", "release":
		return "human"
	default:
		return "machine"
	}
}

func v7ProofOwnerClass(owner string) string {
	owner = strings.ToLower(strings.TrimSpace(owner))
	switch {
	case owner == "":
		return ""
	case owner == "human" || strings.HasPrefix(owner, "human:"):
		return "human"
	case owner == "reviewer" || strings.HasPrefix(owner, "reviewer:"):
		return "reviewer"
	case owner == "external" || strings.HasPrefix(owner, "external:") || owner == "ci" || strings.HasPrefix(owner, "ci:") || owner == "provider" || strings.HasPrefix(owner, "provider:"):
		return "external"
	case owner == "agent" || strings.HasPrefix(owner, "agent:") || owner == "automation" || strings.HasPrefix(owner, "tusker:"):
		return "machine"
	default:
		return ""
	}
}

func v7TaskProofOwnerHints(task Note) map[string]string {
	hints := map[string]string{}
	for key, value := range stringMapValue(task.Data["proof_required_owner"]) {
		normalized := strings.ToLower(strings.TrimSpace(key))
		normalized = strings.ReplaceAll(normalized, "-", "_")
		if normalized == "" {
			continue
		}
		hints[normalized] = strings.TrimSpace(value)
	}
	return hints
}

func stringMapValue(value any) map[string]string {
	out := map[string]string{}
	switch v := value.(type) {
	case nil:
		return out
	case map[string]string:
		for key, item := range v {
			out[key] = item
		}
	case map[string]any:
		for key, item := range v {
			out[key] = toString(item)
		}
	case map[any]any:
		for key, item := range v {
			out[toString(key)] = toString(item)
		}
	}
	return out
}

func v7VerificationResultCovers(result string) bool {
	switch strings.ToLower(strings.TrimSpace(result)) {
	case "pass", "waived":
		return true
	default:
		return false
	}
}

func v7EvidenceUsableForProof(ev Note) bool {
	if stringField(ev.Data, "status") != "accepted" {
		return false
	}
	taskID := stringField(ev.Data, "task")
	for _, artifact := range normalizeList(ev.Data["artifact_paths"]) {
		if !v7ArtifactPathExternal(artifact) && !v7ArtifactPathDurable(taskID, artifact) {
			return false
		}
	}
	if stringField(ev.Data, "evidence_kind") == "screenshot" {
		return stringField(ev.Data, "screenshot_checked_by") != "" && stringField(ev.Data, "screenshot_checked_at") != ""
	}
	return true
}

func v7GateTouchesTask(gate Note, taskID string) bool {
	if containsString(normalizeList(gate.Data["blocks"]), taskID) {
		return true
	}
	prefix := taskID + ":"
	for _, cover := range normalizeList(gate.Data["covers"]) {
		if strings.HasPrefix(strings.ToUpper(strings.TrimSpace(cover)), prefix) {
			return true
		}
	}
	return false
}

func v7CoverTextToAcceptanceIDs(text string, acceptanceIDs []string) []string {
	return v7CoversToAcceptanceIDs(v7CoverTokens(text), acceptanceIDs)
}

func v7CoversToAcceptanceIDs(covers []string, acceptanceIDs []string) []string {
	acceptanceSet := map[string]bool{}
	for _, id := range acceptanceIDs {
		acceptanceSet[id] = true
	}
	seen := map[string]bool{}
	var out []string
	add := func(id string) {
		id = normalizeV7AcceptanceID(id)
		if id == "" || !acceptanceSet[id] || seen[id] {
			return
		}
		seen[id] = true
		out = append(out, id)
	}
	for _, cover := range covers {
		token := strings.ToUpper(strings.TrimSpace(cover))
		token = strings.Trim(token, "` ")
		if token == "" {
			continue
		}
		if strings.Contains(token, ":") {
			parts := strings.SplitN(token, ":", 2)
			token = parts[1]
		}
		if token == "ALL" {
			for _, id := range acceptanceIDs {
				add(id)
			}
			continue
		}
		if strings.Contains(token, "-") {
			parts := strings.SplitN(token, "-", 2)
			start := strings.TrimPrefix(normalizeV7AcceptanceID(parts[0]), "A")
			end := strings.TrimPrefix(normalizeV7AcceptanceID(parts[1]), "A")
			startN := atoiSafe(start)
			endN := atoiSafe(end)
			if startN > 0 && endN >= startN {
				for i := startN; i <= endN; i++ {
					add(fmt.Sprintf("A%d", i))
				}
				continue
			}
		}
		add(token)
	}
	return out
}

func v7CoverTokens(text string) []string {
	fields := strings.FieldsFunc(text, func(r rune) bool {
		return r == ',' || r == ';'
	})
	var out []string
	for _, field := range fields {
		trimmed := strings.TrimSpace(field)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func v7ProofModeRequirementMissing(task Note, idx v7Index) []string {
	if strings.EqualFold(stringField(task.Data, "proof_status"), "waived") {
		return nil
	}
	taskID := stringField(task.Data, "id")
	mode := strings.ToLower(fallback(stringField(task.Data, "proof_mode"), defaultV7ProofMode(stringField(task.Data, "risk"))))
	if mode == "none" {
		return nil
	}
	var missing []string
	for _, required := range v7TaskProofRequired(task) {
		if !v7ProofRequiredClassSatisfied(taskID, required, task, idx) {
			missing = append(missing, "proof_required:"+required)
		}
	}
	switch mode {
	case "inline":
		return uniqueStrings(missing)
	case "card":
		if !v7TaskHasAcceptedEvidence(taskID, idx, false) {
			missing = append(missing, "accepted evidence card")
		}
		return uniqueStrings(missing)
	case "artifact", "audit":
		if !v7TaskHasAcceptedEvidence(taskID, idx, true) {
			missing = append(missing, "accepted artifact evidence")
		}
		return uniqueStrings(missing)
	default:
		return []string{"valid proof_mode"}
	}
}

func v7TaskProofRequired(task Note) []string {
	var out []string
	for _, required := range normalizeList(task.Data["proof_required"]) {
		required = strings.ToLower(strings.TrimSpace(required))
		required = strings.ReplaceAll(required, "-", "_")
		if required != "" {
			out = append(out, required)
		}
	}
	return uniqueStrings(out)
}

func v7ProofRequiredClassSatisfied(taskID, required string, task Note, idx v7Index) bool {
	required = strings.ToLower(strings.TrimSpace(required))
	switch required {
	case "", "none":
		return true
	}
	owner := classifyProofRequirement(required, task, idx)
	for _, row := range parseV7VerificationRows(task.Body) {
		if !v7VerificationResultCovers(row.Result) {
			continue
		}
		if owner == "machine" && v7InlineVerificationSatisfies(required, row) {
			return true
		}
	}
	for _, ev := range idx.Evidence[taskID] {
		if !v7EvidenceUsableForProof(ev) {
			continue
		}
		if v7EvidenceSatisfiesProofRequired(required, ev) && v7EvidenceSatisfiesRequiredOwner(owner, ev) {
			return true
		}
	}
	for _, gate := range idx.Gates {
		if !v7GateTouchesTask(gate, taskID) {
			continue
		}
		status := stringField(gate.Data, "status")
		if status != "satisfied" && status != "waived" {
			continue
		}
		if v7GateSatisfiesProofRequired(required, gate) && v7GateSatisfiesRequiredOwner(owner, gate) {
			return true
		}
	}
	return false
}

func v7InlineVerificationSatisfies(required string, row v7VerificationRow) bool {
	text := strings.ToLower(row.Check + " " + row.Notes)
	switch required {
	case "focused_test", "broad_test":
		return strings.Contains(text, "test")
	case "typecheck":
		return strings.Contains(text, "typecheck") || strings.Contains(text, "tsc") || strings.Contains(text, "cargo check") || strings.Contains(text, "go test") || strings.Contains(text, "swift build")
	case "lint":
		return strings.Contains(text, "lint") || strings.Contains(text, "eslint") || strings.Contains(text, "golangci") || strings.Contains(text, "ruff") || strings.Contains(text, "staticcheck")
	case "build":
		return strings.Contains(text, "build") || strings.Contains(text, "xcodebuild") || strings.Contains(text, "go test") || strings.Contains(text, "go build") || strings.Contains(text, "npm run build")
	case "ci":
		return strings.Contains(text, "ci")
	case "manual_smoke":
		return strings.Contains(text, "manual smoke") || strings.Contains(text, "smoke")
	case "screenshot":
		return strings.Contains(text, "screenshot")
	case "video":
		return strings.Contains(text, "video") || strings.Contains(text, ".mov") || strings.Contains(text, ".mp4") || strings.Contains(text, ".webm")
	case "trace":
		return strings.Contains(text, "trace")
	case "provider_probe":
		return strings.Contains(text, "provider") || strings.Contains(text, "probe")
	case "benchmark":
		return strings.Contains(text, "bench") || strings.Contains(text, "benchmark")
	case "security_review":
		return strings.Contains(text, "security review")
	case "privacy_review":
		return strings.Contains(text, "privacy review")
	case "release_smoke":
		return strings.Contains(text, "release smoke")
	case "human_signoff":
		return strings.Contains(text, "human signoff") || strings.Contains(text, "human sign-off")
	default:
		return strings.Contains(text, required)
	}
}

func v7EvidenceSatisfiesProofRequired(required string, ev Note) bool {
	kind := strings.ToLower(stringField(ev.Data, "evidence_kind"))
	artifactPaths := normalizeList(ev.Data["artifact_paths"])
	switch required {
	case "focused_test", "broad_test":
		return kind == "automated_test" || kind == "unit_test" || kind == "integration_test" || kind == "e2e_test" || kind == "ci_run" || v7EvidenceTextContains(ev, "test", "go test", "pytest", "cargo test", "jest", "vitest")
	case "typecheck":
		return kind == "ci_run" || v7EvidenceTextContains(ev, "typecheck", "tsc", "cargo check", "go test", "swift build")
	case "lint":
		return kind == "ci_run" || v7EvidenceTextContains(ev, "lint", "eslint", "golangci", "ruff", "staticcheck")
	case "build":
		return kind == "ci_run" || v7EvidenceTextContains(ev, "build", "xcodebuild", "go test", "go build", "npm run build")
	case "ci":
		return kind == "ci_run"
	case "manual_smoke":
		return kind == "manual_smoke" || kind == "physical_smoke"
	case "screenshot":
		return kind == "screenshot" || v7EvidenceHasArtifactExt(artifactPaths, ".png", ".jpg", ".jpeg")
	case "video":
		return kind == "video" || v7EvidenceHasArtifactExt(artifactPaths, ".mov", ".mp4", ".webm")
	case "trace":
		return kind == "trace" || v7EvidenceHasArtifactExt(artifactPaths, ".trace")
	case "provider_probe":
		return kind == "provider_probe"
	case "benchmark":
		return kind == "benchmark" || kind == "performance_profile"
	case "security_review":
		return kind == "security_review"
	case "privacy_review":
		return kind == "privacy_review"
	case "release_smoke":
		return kind == "release_smoke"
	case "human_signoff":
		return kind == "human_review"
	default:
		return kind == required
	}
}

func v7EvidenceSatisfiesRequiredOwner(owner string, ev Note) bool {
	switch owner {
	case "", "machine":
		return true
	case "human":
		return v7ProofOwnerClass(firstNonEmpty(stringField(ev.Data, "accepted_by"), stringField(ev.Data, "created_by"), stringField(ev.Data, "screenshot_checked_by"))) == "human"
	case "reviewer":
		return v7ProofOwnerClass(firstNonEmpty(stringField(ev.Data, "accepted_by"), stringField(ev.Data, "screenshot_checked_by"))) == "reviewer"
	case "external":
		kind := strings.ToLower(stringField(ev.Data, "evidence_kind"))
		return kind == "ci_run" || kind == "provider_probe"
	default:
		return false
	}
}

func v7EvidenceTextContains(ev Note, needles ...string) bool {
	text := strings.ToLower(stringField(ev.Data, "summary") + " " + ev.Body)
	for _, needle := range needles {
		if strings.Contains(text, needle) {
			return true
		}
	}
	return false
}

func v7EvidenceHasArtifactExt(paths []string, exts ...string) bool {
	for _, path := range paths {
		lower := strings.ToLower(strings.TrimSpace(path))
		for _, ext := range exts {
			if strings.HasSuffix(lower, ext) || strings.Contains(lower, ext+"?") {
				return true
			}
		}
	}
	return false
}

func v7GateSatisfiesProofRequired(required string, gate Note) bool {
	return v7GateKindSatisfiesProofRequired(required, gate) || v7GateTextSatisfiesProofRequirement(required, gate)
}

func v7GateKindSatisfiesProofRequired(required string, gate Note) bool {
	kind := strings.ToLower(stringField(gate.Data, "gate_kind"))
	switch required {
	case "ci":
		return kind == "ci"
	case "provider_probe":
		return kind == "external_service"
	case "security_review":
		return kind == "security"
	case "release_smoke":
		return kind == "release"
	case "human_signoff":
		return kind == "signoff"
	default:
		return kind == required
	}
}

func v7GateSatisfiesRequiredOwner(owner string, gate Note) bool {
	switch owner {
	case "", "machine":
		return true
	case "human":
		return v7ProofOwnerClass(firstNonEmpty(stringField(gate.Data, "satisfied_by"), stringField(gate.Data, "waived_by"))) == "human"
	case "reviewer":
		return v7ProofOwnerClass(firstNonEmpty(stringField(gate.Data, "satisfied_by"), stringField(gate.Data, "waived_by"))) == "reviewer"
	case "external":
		return classifyV7GateOwner(gate) == "external"
	default:
		return false
	}
}

func v7TaskHasAcceptedEvidence(taskID string, idx v7Index, requireArtifact bool) bool {
	for _, ev := range idx.Evidence[taskID] {
		if !v7EvidenceUsableForProof(ev) {
			continue
		}
		if requireArtifact && len(normalizeList(ev.Data["artifact_paths"])) == 0 {
			continue
		}
		return true
	}
	return false
}

func v7ComputedProofStatus(task Note, report v7ProofReport) string {
	if strings.EqualFold(stringField(task.Data, "proof_status"), "waived") {
		return "waived"
	}
	if len(report.Missing) == 0 && len(report.ModeMissing) == 0 {
		if report.Mode == "none" || len(report.Acceptance) > 0 || len(normalizeList(task.Data["proof_required"])) == 0 {
			return "satisfied"
		}
	}
	if len(report.Covered) > 0 || len(report.Evidence) > 0 || len(report.OpenGates) > 0 || len(report.SatisfiedGates) > 0 || v7HasSubstantiveVerificationRows(report.InlineRows) {
		return "partial"
	}
	return "pending"
}

func v7HasSubstantiveVerificationRows(rows []v7VerificationRow) bool {
	for _, row := range rows {
		if strings.EqualFold(strings.TrimSpace(row.Check), "TBD") || strings.EqualFold(strings.TrimSpace(row.Result), "pending") {
			continue
		}
		return true
	}
	return false
}

func updateV7TaskProofStatus(vaultPath, taskID, actor string) error {
	idx, err := loadV7Index(vaultPath)
	if err != nil {
		return err
	}
	task, ok := idx.Tasks[taskID]
	if !ok {
		return nil
	}
	data, body, err := parseFrontmatterMustRead(task.AbsolutePath)
	if err != nil {
		return err
	}
	baseRev := stringField(data, "state_rev")
	task.Data = data
	task.Body = body
	report := computeV7ProofReport(vaultPath, task, idx)
	if report.Status == "satisfied" && len(v7PacketStubAcceptanceItems(body)) > 0 && len(v7AcceptanceWaivers(data)) == 0 {
		report.Status = "partial"
	}
	nextBody := syncV7TaskEvidenceSection(body, idx.Evidence[taskID])
	if stringField(data, "proof_status") == report.Status && nextBody == body {
		return nil
	}
	data["proof_status"] = report.Status
	data["updated_at"] = time.Now().UTC().Format(time.RFC3339)
	data["updated_by"] = fallback(actor, "agent:"+defaultActorName())
	_, err = saveV7DocumentCAS(task.AbsolutePath, data, nextBody, v7FrontmatterOrder["task"], baseRev)
	return err
}

func syncV7TaskEvidenceSection(body string, evidence []Note) string {
	if !strings.Contains(body, "## Evidence") {
		return body
	}
	accepted, pending := v7EvidenceSectionLines(evidence)
	content := "Accepted:\n"
	if len(accepted) == 0 {
		content += "- None.\n"
	} else {
		content += strings.Join(accepted, "\n") + "\n"
	}
	content += "\nPending:\n"
	if len(pending) == 0 {
		content += "- None."
	} else {
		content += strings.Join(pending, "\n")
	}
	return replaceSection(body, "## Evidence", content)
}

func v7EvidenceSectionLines(evidence []Note) ([]string, []string) {
	sorted := append([]Note{}, evidence...)
	sort.Slice(sorted, func(i, j int) bool {
		return stringField(sorted[i].Data, "id") < stringField(sorted[j].Data, "id")
	})
	var accepted []string
	var pending []string
	for _, ev := range sorted {
		line := v7EvidenceSectionLine(ev)
		if line == "" {
			continue
		}
		if stringField(ev.Data, "status") == "accepted" {
			accepted = append(accepted, line)
			continue
		}
		pending = append(pending, line)
	}
	return limitV7EvidenceSectionLines(accepted), limitV7EvidenceSectionLines(pending)
}

func v7EvidenceSectionLine(ev Note) string {
	id := stringField(ev.Data, "id")
	if id == "" {
		return ""
	}
	kind := stringField(ev.Data, "evidence_kind")
	status := stringField(ev.Data, "status")
	covers := v7DisplayCovers(normalizeList(ev.Data["covers"]))
	summary := v7EvidenceSummary(ev)
	detail := kind
	if status != "" && status != "accepted" {
		detail += " " + status
	}
	if covers != "" {
		detail += " (" + covers + ")"
	}
	if summary != "" {
		detail += " - " + summary
	}
	return "- [[" + id + "]] " + strings.TrimSpace(detail)
}

func v7DisplayCovers(covers []string) string {
	normalized := normalizeV7Covers(covers)
	for i, cover := range normalized {
		normalized[i] = strings.TrimPrefix(cover, "TASK:")
	}
	return strings.Join(normalized, ", ")
}

func v7EvidenceSummary(ev Note) string {
	summary := strings.TrimSpace(stringField(ev.Data, "summary"))
	if summary == "" {
		summary = strings.TrimSpace(sectionContent(ev.Body, "## Summary"))
	}
	if summary == "" {
		return ""
	}
	summary = strings.Join(strings.Fields(summary), " ")
	if len(summary) > 120 {
		summary = strings.TrimSpace(summary[:117]) + "..."
	}
	return summary
}

func limitV7EvidenceSectionLines(lines []string) []string {
	const maxLines = 4
	if len(lines) <= maxLines {
		return lines
	}
	kept := append([]string{}, lines[:maxLines-1]...)
	kept = append(kept, fmt.Sprintf("- %d more evidence records; see tusker/evidence.", len(lines)-len(kept)))
	return kept
}

func parseV7FinishVerificationRows(raw string) ([]v7VerificationRow, error) {
	var rows []v7VerificationRow
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		row, err := parseV7VerificationRowArg(line)
		if err != nil {
			return nil, err
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func parseV7VerificationRowArg(line string) (v7VerificationRow, error) {
	parts := strings.Split(line, "|")
	if len(parts) < 3 {
		return v7VerificationRow{}, tuskerError(errorInvalidArg, `--verify must look like "A1-A2|command|pass|note|blocked_by"`)
	}
	resultIndex := -1
	result := ""
	for i := len(parts) - 1; i >= 2; i-- {
		candidate := strings.ToLower(strings.TrimSpace(parts[i]))
		if _, ok := v7VerificationResults[candidate]; ok && candidate != "pending" {
			resultIndex = i
			result = candidate
			break
		}
	}
	if resultIndex == -1 {
		bad := ""
		if len(parts) > 2 {
			bad = strings.TrimSpace(parts[2])
		}
		return v7VerificationRow{}, tuskerError(errorInvalidField, "invalid verification result: "+bad)
	}
	note := ""
	blockedBy := ""
	if len(parts) > resultIndex+1 {
		note = strings.TrimSpace(parts[resultIndex+1])
	}
	if len(parts) > resultIndex+2 {
		blockedBy = strings.TrimSpace(strings.Join(parts[resultIndex+2:], "|"))
	}
	return v7VerificationRow{
		CoverText: strings.TrimSpace(parts[0]),
		Check:     strings.TrimSpace(strings.Join(parts[1:resultIndex], "|")),
		Result:    result,
		Notes:     note,
		BlockedBy: blockedBy,
	}, nil
}

func proofV7MissingForFinish(vaultPath, taskID string) ([]string, []string, error) {
	idx, err := loadV7Index(vaultPath)
	if err != nil {
		return nil, nil, err
	}
	task, ok := idx.Tasks[taskID]
	if !ok {
		return nil, nil, tuskerError(errorNotFound, "V7 task not found: "+taskID)
	}
	report := computeV7ProofReport(vaultPath, task, idx)
	missing := append([]string{}, report.Missing...)
	missing = append(missing, report.ModeMissing...)
	return missing, report.OpenGates, nil
}

func evidenceV7PromoteCmd(args Args) error {
	taskID := firstNonEmpty(args.String("id"), args.String("_pos1"))
	if taskID == "" {
		return tuskerError(errorMissingArg, "evidence promote requires <task-id>")
	}
	source := firstNonEmpty(args.String("from"), args.String("path"), args.String("_pos2"))
	if source == "" {
		return tuskerError(errorMissingArg, "evidence promote requires --from .tusker/scratch/<task-id>/<artifact>")
	}
	args["id"] = taskID
	args["path"] = source
	args["kind"] = fallback(args.String("kind"), "log_excerpt")
	args["status"] = fallback(args.String("status"), "accepted")
	if !strings.Contains(filepath.ToSlash(source), ".tusker/scratch/") && !args.Bool("force") {
		return tuskerError(errorInvalidArg, "evidence promote expects a scratch source under .tusker/scratch/<task-id>/", withHint("pass --force only when intentionally promoting a durable non-scratch artifact"))
	}
	return evidenceV7AddCmd(args)
}

func evidenceV7PruneCmd(args Args) error {
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	taskID := firstNonEmpty(args.String("id"), args.String("_pos1"))
	if taskID == "" {
		return tuskerError(errorMissingArg, "evidence prune requires <task-id>")
	}
	report, err := classifyV7EvidenceBloat(vaultPath, taskID)
	if err != nil {
		return err
	}
	if args.Bool("json") {
		emitJSON(report)
		return nil
	}
	fmt.Printf("Evidence prune %s (dry-run)\n", taskID)
	for _, category := range []string{"keep", "move_to_attempt", "move_to_scratch", "delete_candidate", "promote_to_gate", "forbidden"} {
		items := normalizeList(report[category])
		if len(items) == 0 {
			continue
		}
		fmt.Printf("\n%s:\n", category)
		for _, item := range items {
			fmt.Printf("  - %s\n", item)
		}
	}
	return nil
}

func classifyV7EvidenceBloat(vaultPath, taskID string) (map[string]any, error) {
	idx, err := loadV7Index(vaultPath)
	if err != nil {
		return nil, err
	}
	report := map[string]any{
		"task":             taskID,
		"mode":             "dry-run",
		"keep":             []string{},
		"move_to_attempt":  []string{},
		"move_to_scratch":  []string{},
		"delete_candidate": []string{},
		"promote_to_gate":  []string{},
		"forbidden":        []string{},
	}
	for _, ev := range idx.Evidence[taskID] {
		id := stringField(ev.Data, "id")
		if len(v7CoversToAcceptanceIDs(normalizeList(ev.Data["covers"]), v7AcceptanceIDs(idx.Tasks[taskID].Body))) == 0 {
			report["move_to_attempt"] = appendStringAny(report["move_to_attempt"], id+" has no acceptance coverage")
			continue
		}
		for _, artifact := range normalizeList(ev.Data["artifact_paths"]) {
			if v7ForbiddenEvidenceArtifactPath(artifact) {
				report["forbidden"] = appendStringAny(report["forbidden"], id+" artifact "+artifact)
			}
		}
		report["keep"] = appendStringAny(report["keep"], id+" "+stringField(ev.Data, "evidence_kind"))
	}
	attachments := filepath.Join(vaultPath, "Attachments", taskID)
	if dirExists(attachments) {
		_ = filepath.WalkDir(attachments, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			rel, _ := filepath.Rel(vaultPath, path)
			class := classifyV7AttachmentPath(path)
			report[class] = appendStringAny(report[class], filepath.ToSlash(rel))
			return nil
		})
	}
	return report, nil
}

func appendStringAny(value any, item string) []string {
	items := normalizeList(value)
	items = append(items, item)
	return items
}

func classifyV7AttachmentPath(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch {
	case v7ForbiddenEvidenceArtifactPath(path):
		return "forbidden"
	case ext == ".log" || ext == ".txt" || ext == ".json" || ext == ".jsonl":
		return "move_to_scratch"
	case ext == ".png" || ext == ".jpg" || ext == ".jpeg" || ext == ".gif" || ext == ".mp4" || ext == ".mov" || ext == ".webm":
		return "move_to_scratch"
	case ext == ".md":
		return "move_to_attempt"
	default:
		return "delete_candidate"
	}
}

func attachmentsV7Cmd(args Args) error {
	if strings.ToLower(args.String("_pos0")) != "migrate" {
		return tuskerError(errorMissingArg, "Usage: tusker attachments migrate --dry-run|--write")
	}
	return attachmentsV7MigrateCmd(args)
}

func attachmentsV7MigrateCmd(args Args) error {
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	attachments := filepath.Join(vaultPath, "Attachments")
	report := map[string]any{
		"mode":             "dry-run",
		"move_to_scratch":  []string{},
		"move_to_attempt":  []string{},
		"forbidden":        []string{},
		"delete_candidate": []string{},
		"dirs_removed":     []string{},
	}
	if !dirExists(attachments) {
		if args.Bool("json") {
			emitJSON(report)
		} else {
			fmt.Println("No Attachments/ directory found.")
		}
		return nil
	}
	write := args.Bool("write")
	if write {
		report["mode"] = "write"
	}
	err = filepath.WalkDir(attachments, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(vaultPath, path)
		taskID := v7TaskIDFromAttachmentPath(vaultPath, path)
		class := classifyV7AttachmentPath(path)
		report[class] = appendStringAny(report[class], filepath.ToSlash(rel))
		if !write {
			return nil
		}
		targetTask := fallback(taskID, "_unmapped")
		target := filepath.Join(vaultPath, ".tusker", "scratch", targetTask, "legacy-attachments", filepath.Base(path))
		if err := ensureDir(filepath.Dir(target)); err != nil {
			return err
		}
		if err := os.Rename(path, target); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return err
	}
	if write {
		removed, err := removeEmptyDirsUnder(vaultPath, attachments)
		if err != nil {
			return err
		}
		for _, rel := range removed {
			report["dirs_removed"] = appendStringAny(report["dirs_removed"], rel)
		}
	}
	if args.Bool("json") {
		emitJSON(report)
		return nil
	}
	fmt.Printf("Attachments migration %s\n", report["mode"])
	for _, category := range []string{"move_to_scratch", "move_to_attempt", "forbidden", "delete_candidate", "dirs_removed"} {
		items := normalizeList(report[category])
		if len(items) == 0 {
			continue
		}
		fmt.Printf("\n%s:\n", category)
		for _, item := range items {
			fmt.Printf("  - %s\n", item)
		}
	}
	if !write {
		fmt.Println("\nDry run only. Use --write to move files under .tusker/scratch/<task>/legacy-attachments/.")
	}
	return nil
}

func removeEmptyDirsUnder(vaultPath, root string) ([]string, error) {
	var dirs []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if d.IsDir() {
			dirs = append(dirs, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(dirs, func(i, j int) bool {
		return len(dirs[i]) > len(dirs[j])
	})
	var removed []string
	for _, dir := range dirs {
		if err := os.Remove(dir); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			if entries, readErr := os.ReadDir(dir); readErr == nil && len(entries) > 0 {
				continue
			}
			return nil, err
		}
		rel, _ := filepath.Rel(vaultPath, dir)
		removed = append(removed, filepath.ToSlash(rel))
	}
	sort.Strings(removed)
	return removed, nil
}

func migrateV7EvidencePolicyCmd(args Args) error {
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	idx, err := loadV7Index(vaultPath)
	if err != nil {
		return err
	}
	write := args.Bool("write")
	report := map[string]any{
		"mode":                 "dry-run",
		"tasks_seen":           len(idx.Tasks),
		"tasks_updated":        0,
		"proof_mode_added":     0,
		"proof_status_fixed":   0,
		"evidence_over_budget": []string{},
	}
	if write {
		report["mode"] = "write"
	}
	for _, task := range sortedV7Tasks(idx) {
		data, body, err := parseFrontmatterMustRead(task.AbsolutePath)
		if err != nil {
			return err
		}
		changed := false
		baseRev := stringField(data, "state_rev")
		mode := strings.ToLower(strings.TrimSpace(stringField(data, "proof_mode")))
		if mode == "" {
			mode = defaultV7ProofModeForTask(data)
			data["proof_mode"] = mode
			data["proof_required"] = defaultV7ProofRequired(mode)
			data["evidence_budget"] = defaultV7EvidenceBudget(mode)
			data["raw_artifacts_allowed"] = false
			report["proof_mode_added"] = intValue(report["proof_mode_added"]) + 1
			changed = true
		}
		if mode != "none" && len(normalizeList(data["proof_required"])) == 0 {
			data["proof_required"] = defaultV7ProofRequired(mode)
			changed = true
		}
		if _, ok := data["evidence_budget"]; !ok {
			data["evidence_budget"] = defaultV7EvidenceBudget(mode)
			changed = true
		}
		if _, ok := data["raw_artifacts_allowed"]; !ok {
			data["raw_artifacts_allowed"] = false
			changed = true
		}
		nextBody := syncV7TaskEvidenceSection(body, idx.Evidence[stringField(data, "id")])
		if nextBody != body {
			body = nextBody
			changed = true
		}
		current := task
		current.Data = data
		current.Body = body
		proofStatus := computeV7ProofReport(vaultPath, current, idx).Status
		if stringField(data, "proof_status") != proofStatus {
			data["proof_status"] = proofStatus
			report["proof_status_fixed"] = intValue(report["proof_status_fixed"]) + 1
			changed = true
		}
		budget := intField(data, "evidence_budget")
		if budget >= 0 && len(idx.Evidence[stringField(data, "id")]) > budget {
			report["evidence_over_budget"] = appendStringAny(report["evidence_over_budget"], stringField(data, "id"))
		}
		if !write || !changed {
			continue
		}
		data["updated_at"] = time.Now().UTC().Format(time.RFC3339)
		data["updated_by"] = "tusker:migrate-evidence-policy"
		if _, err := saveV7DocumentCAS(task.AbsolutePath, data, body, v7FrontmatterOrder["task"], baseRev); err != nil {
			return err
		}
		report["tasks_updated"] = intValue(report["tasks_updated"]) + 1
	}
	if args.Bool("json") {
		emitJSON(report)
		return nil
	}
	tasksSeen := intValue(report["tasks_seen"])
	tasksUpdated := intValue(report["tasks_updated"])
	fmt.Printf("Evidence policy migration %s: %d task%s seen, %d updated.\n", report["mode"], tasksSeen, plural(tasksSeen), tasksUpdated)
	if !write {
		fmt.Println("Dry run only. Use --write to add proof_mode/proof_status defaults.")
	}
	if over := normalizeList(report["evidence_over_budget"]); len(over) > 0 {
		fmt.Printf("Evidence over budget: %s\n", strings.Join(over, ", "))
	}
	return nil
}

func defaultV7ProofModeForTask(data map[string]any) string {
	mode := defaultV7ProofMode(stringField(data, "risk"))
	if mode == "inline" && len(normalizeList(data["evidence_required"])) > 0 {
		return "card"
	}
	return mode
}

func v7TaskIDFromAttachmentPath(vaultPath, path string) string {
	rel, err := filepath.Rel(filepath.Join(vaultPath, "Attachments"), path)
	if err != nil {
		return ""
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	if len(parts) == 0 {
		return ""
	}
	candidate := strings.ToUpper(parts[0])
	if v7TaskIDPattern.MatchString(candidate) {
		return candidate
	}
	return ""
}

func v7ForbiddenEvidenceArtifactPath(path string) bool {
	lower := strings.ToLower(strings.TrimSpace(path))
	lower = strings.TrimPrefix(lower, "link-only:")
	lower = strings.TrimPrefix(lower, "external:")
	if strings.Contains(lower, "://") || lower == "" {
		return false
	}
	if strings.HasSuffix(lower, ".xcodeproj") || strings.HasSuffix(lower, ".xcworkspace") || strings.HasSuffix(lower, "project.pbxproj") {
		return true
	}
	for _, ext := range []string{".swift", ".pbxproj", ".ts", ".tsx", ".js", ".jsx", ".go", ".rs", ".py", ".java", ".kt", ".cs", ".zip", ".tar", ".gz"} {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}

func sortedV7ProofStrings(values []string) []string {
	out := append([]string{}, values...)
	sort.Strings(out)
	return out
}
