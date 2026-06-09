package main

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func v7PacketSnippet(text string, maxLines int) string {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	var out []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			out = append(out, trimmed)
		}
		if len(out) >= maxLines {
			break
		}
	}
	if len(out) == 0 {
		return "- None."
	}
	return strings.Join(out, "\n")
}

func buildV7Dashboards(vaultPath string, idx v7Index) error {
	leases, err := loadV7Leases(vaultPath)
	if err != nil {
		return err
	}
	if err := writeV7DashboardLandingNote(vaultPath); err != nil {
		return err
	}
	for rel, content := range v7CommittedDashboardProjections(idx, leases) {
		if err := writeText(filepath.Join(vaultPath, filepath.FromSlash(rel)), content); err != nil {
			return err
		}
	}
	if err := writeJSON(filepath.Join(vaultPath, "_generated", "indexes", "tasks.json"), v7IndexRecords(sortedV7Tasks(idx))); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(vaultPath, "_generated", "indexes", "gates.json"), v7IndexRecords(sortedV7Gates(idx))); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(vaultPath, "_generated", "indexes", "leases.json"), v7LeaseIndexRecords(leases)); err != nil {
		return err
	}
	dashboard := v7DashboardIndexData(idx, leases)
	if err := writeJSON(filepath.Join(vaultPath, "_generated", "indexes", "dashboard.json"), dashboard); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(vaultPath, "_generated", "indexes", "summary.json"), v7SummaryIndexData(idx, dashboard)); err != nil {
		return err
	}
	return nil
}

func v7CommittedDashboardProjections(idx v7Index, leases []v7LeaseRecord) map[string]string {
	return map[string]string{
		"dashboards/human-actions.md":         v7HumanActionsDashboard(idx),
		"dashboards/agent-ready.md":           v7AgentReadyDashboard(idx),
		"dashboards/review-queue.md":          v7ReviewQueueDashboard(idx),
		"dashboards/ci-waiting.md":            v7CIWaitingDashboard(idx),
		"dashboards/active-runs.md":           v7ActiveRunsDashboard(leases),
		"_generated/bases/tasks.base":         v7TasksBase(),
		"_generated/bases/epics.base":         v7EpicsBase(),
		"_generated/bases/agent-ready.base":   v7AgentReadyBase(),
		"_generated/bases/human-actions.base": v7HumanActionsBase(),
		"_generated/bases/backlog.base":       v7BacklogBase(),
		"_generated/bases/review-queue.base":  v7ReviewQueueBase(),
		"_generated/bases/blocked.base":       v7BlockedBase(),
		"_generated/bases/recently-done.base": v7RecentlyDoneBase(),
		"_generated/bases/ci-waiting.base":    v7CIWaitingBase(),
	}
}

func v7DashboardIndexData(idx v7Index, leases []v7LeaseRecord) map[string]any {
	return map[string]any{
		"schema":          "tusker.dashboard-index/v1",
		"source":          "v7",
		"generatedAt":     time.Now().UTC().Format(time.RFC3339),
		"human_actions":   len(v7OpenHumanGates(idx)) + len(v7HumanWaitTasks(idx)),
		"agent_ready":     len(v7ReadyAgentTasks(idx)),
		"review_queue":    len(v7ReviewTasks(idx)),
		"human_wait":      len(v7HumanWaitTasks(idx)),
		"active_runs":     len(v7ActiveLeases(leases)),
		"stale_leases":    len(v7StaleLeases(leases)),
		"done_open_gates": len(v7DoneTaskOpenGateViolations(idx)),
	}
}

func v7SummaryIndexData(idx v7Index, dashboard map[string]any) map[string]any {
	openTasks := 0
	doneTasks := 0
	cancelledTasks := 0
	supersededTasks := 0
	for _, task := range idx.Tasks {
		switch stringField(task.Data, "status") {
		case "done":
			doneTasks++
		case "cancelled":
			cancelledTasks++
		case "superseded":
			supersededTasks++
		default:
			openTasks++
		}
	}
	openGates := 0
	for _, gate := range idx.Gates {
		if stringField(gate.Data, "status") == "open" {
			openGates++
		}
	}
	return map[string]any{
		"schema":      "tusker.summary/v7",
		"source":      "v7",
		"generatedAt": dashboard["generatedAt"],
		"counts": map[string]any{
			"epics":         len(idx.Epics),
			"tasks":         len(idx.Tasks),
			"openWork":      openTasks,
			"done":          doneTasks,
			"cancelled":     cancelledTasks,
			"superseded":    supersededTasks,
			"gates":         len(idx.Gates),
			"openGates":     openGates,
			"humanActions":  dashboard["human_actions"],
			"ready":         dashboard["agent_ready"],
			"inReview":      dashboard["review_queue"],
			"humanWait":     dashboard["human_wait"],
			"running":       dashboard["active_runs"],
			"staleLeases":   dashboard["stale_leases"],
			"doneOpenGates": dashboard["done_open_gates"],
		},
	}
}

func isV7VaultLayout(vaultPath string) bool {
	return dirExists(filepath.Join(vaultPath, "work", "tasks")) ||
		dirExists(filepath.Join(vaultPath, "work", "epics")) ||
		dirExists(filepath.Join(vaultPath, "work", "gates"))
}

func writeV7DashboardLandingNote(vaultPath string) error {
	path := filepath.Join(vaultPath, "Dashboard.md")
	next := v7DashboardLandingNote()
	if !fileExists(path) {
		return writeText(path, next)
	}
	current, err := readText(path)
	if err != nil {
		return err
	}
	if migrated, ok := replaceLegacyDashboardPlaceholder(current, next); ok {
		return writeText(path, migrated)
	}
	if shouldReplaceGeneratedV7DashboardLanding(current) {
		return writeText(path, next)
	}
	return nil
}

func replaceLegacyDashboardPlaceholder(current, next string) (string, bool) {
	const legacy = "Legacy dashboard generation was removed from the V7-only build."
	if !strings.Contains(current, legacy) {
		return "", false
	}
	idx := strings.Index(current, legacy)
	rest := current[idx+len(legacy):]
	rest = strings.TrimLeft(rest, " \t\r\n")
	if rest == "" {
		return next, true
	}
	return strings.TrimRight(next, "\n") + "\n\n" + rest, true
}

func v7DashboardLandingNote() string {
	return strings.Join([]string{
		"<!-- tusker:v7-dashboard:landing; generated by tusker dashboard build -->",
		"",
		"# Tusker Dashboard",
		"",
		"## Action queues",
		"",
		"![[dashboards/human-actions]]",
		"",
		"![[dashboards/agent-ready]]",
		"",
		"![[dashboards/review-queue]]",
		"",
		"![[dashboards/ci-waiting]]",
		"",
		"![[dashboards/active-runs]]",
		"",
		"## Bases",
		"",
		"![[_generated/bases/tasks.base#Agent Ready]]",
		"",
		"![[_generated/bases/tasks.base#Backlog]]",
		"",
		"![[_generated/bases/tasks.base#Review]]",
		"",
		"![[_generated/bases/epics.base#Open Epics]]",
		"",
		"_Regenerate with `tusker dashboard build --quiet`._",
		"",
	}, "\n")
}

func shouldReplaceGeneratedV7DashboardLanding(current string) bool {
	if strings.Contains(current, "<!-- tusker:v7-dashboard:landing;") {
		return true
	}
	return strings.Contains(current, "# Tusker Dashboard") &&
		strings.Contains(current, "![[_generated/bases/tasks.base#Agent Ready]]") &&
		(strings.Contains(current, docsFreshnessBegin) ||
			strings.Contains(current, dashboardRunsBegin) ||
			strings.Contains(current, "## Docs catalog"))
}

func v7TasksBase() string {
	return strings.Join([]string{
		`filters:`,
		`  and:`,
		`    - file.ext == "md"`,
		`    - 'schema == "tusker.task/v7"'`,
		`properties:`,
		`  id:`,
		`    displayName: ID`,
		`  title:`,
		`    displayName: Title`,
		`  epic:`,
		`    displayName: Epic`,
		`  status:`,
		`    displayName: Status`,
		`  readiness:`,
		`    displayName: Readiness`,
		`  priority:`,
		`    displayName: Priority`,
		`  risk:`,
		`    displayName: Risk`,
		`  next_owner:`,
		`    displayName: Next owner`,
		`  next_action:`,
		`    displayName: Next action`,
		`views:`,
		`  - type: table`,
		`    name: "Agent Ready"`,
		`    filters:`,
		`      and:`,
		`        - 'readiness == "ready"'`,
		`        - 'status == "ready" || status == "rework"'`,
		`        - 'next_owner == "agent" || next_owner.startsWith("agent:")'`,
		`    order: [id, title, epic, priority, risk, next_action]`,
		`  - type: table`,
		`    name: Backlog`,
		`    filters:`,
		`      or:`,
		`        - 'status == "idea"'`,
		`        - 'status == "backlog"'`,
		`        - 'readiness == "held"'`,
		`    order: [id, title, epic, priority, risk, next_owner, next_action]`,
		`  - type: table`,
		`    name: Review`,
		`    filters:`,
		`      and:`,
		`        - 'status == "review"'`,
		`        - 'readiness != "waiting_on_human"'`,
		`    order: [id, title, epic, risk, next_action]`,
		`  - type: table`,
		`    name: Human Wait`,
		`    filters:`,
		`      or:`,
		`        - 'readiness == "waiting_on_human"'`,
		`        - 'agent_action == "stop_until_human_response"'`,
		`    order: [id, title, next_owner, next_ref, next_action]`,
		`  - type: table`,
		`    name: Blocked`,
		`    filters:`,
		`      or:`,
		`        - 'readiness.startsWith("blocked")'`,
		`        - 'readiness == "waiting_on_ci"'`,
		`    order: [id, title, epic, readiness, next_owner, next_action]`,
		`  - type: table`,
		`    name: Done`,
		`    filters:`,
		`      and:`,
		`        - 'status == "done"'`,
		`    order: [id, title, epic, accepted_by, closed_at]`,
		``,
	}, "\n")
}

func v7EpicsBase() string {
	return strings.Join([]string{
		`filters:`,
		`  and:`,
		`    - file.ext == "md"`,
		`    - 'schema == "tusker.epic/v7"'`,
		`properties:`,
		`  id:`,
		`    displayName: ID`,
		`  title:`,
		`    displayName: Title`,
		`  status:`,
		`    displayName: Status`,
		`  priority:`,
		`    displayName: Priority`,
		`  owner:`,
		`    displayName: Owner`,
		`views:`,
		`  - type: table`,
		`    name: "Open Epics"`,
		`    filters:`,
		`      and:`,
		`        - 'status != "done"'`,
		`        - 'status != "cancelled"'`,
		`    order: [id, title, status, priority, owner]`,
		`  - type: table`,
		`    name: Archive`,
		`    filters:`,
		`      or:`,
		`        - 'status == "done"'`,
		`        - 'status == "cancelled"'`,
		`    order: [id, title, status, owner]`,
		``,
	}, "\n")
}

func v7AgentReadyBase() string {
	return v7SingleTaskBase("Agent Ready", []string{
		`        - 'readiness == "ready"'`,
		`        - 'status == "ready" || status == "rework"'`,
		`        - 'next_owner == "agent" || next_owner.startsWith("agent:")'`,
	}, `[id, title, epic, priority, risk, next_action]`)
}

func v7BacklogBase() string {
	return v7SingleTaskBase("Backlog", []string{
		`        - 'status == "idea" || status == "backlog" || readiness == "held"'`,
	}, `[id, title, epic, priority, risk, next_owner, next_action]`)
}

func v7ReviewQueueBase() string {
	return v7SingleTaskBase("Review Queue", []string{
		`        - 'status == "review"'`,
		`        - 'readiness != "waiting_on_human"'`,
	}, `[id, title, epic, risk, next_action]`)
}

func v7BlockedBase() string {
	return v7SingleTaskBase("Blocked", []string{
		`        - 'readiness.startsWith("blocked") || readiness == "waiting_on_ci"'`,
	}, `[id, title, epic, readiness, next_owner, next_action]`)
}

func v7RecentlyDoneBase() string {
	return v7SingleTaskBase("Recently Done", []string{
		`        - 'status == "done"'`,
	}, `[id, title, epic, accepted_by, closed_at]`)
}

func v7CIWaitingBase() string {
	return v7SingleTaskBase("CI Waiting", []string{
		`        - 'readiness == "waiting_on_ci"'`,
	}, `[id, title, epic, next_action]`)
}

func v7SingleTaskBase(name string, viewFilters []string, order string) string {
	lines := []string{
		`filters:`,
		`  and:`,
		`    - file.ext == "md"`,
		`    - 'schema == "tusker.task/v7"'`,
		`properties:`,
		`  id:`,
		`    displayName: ID`,
		`  title:`,
		`    displayName: Title`,
		`  epic:`,
		`    displayName: Epic`,
		`  status:`,
		`    displayName: Status`,
		`  readiness:`,
		`    displayName: Readiness`,
		`  priority:`,
		`    displayName: Priority`,
		`  next_owner:`,
		`    displayName: Next owner`,
		`  next_action:`,
		`    displayName: Next action`,
		`views:`,
		`  - type: table`,
		fmt.Sprintf(`    name: "%s"`, name),
		`    filters:`,
		`      and:`,
	}
	lines = append(lines, viewFilters...)
	lines = append(lines, `    order: `+order, ``)
	return strings.Join(lines, "\n")
}

func v7HumanActionsBase() string {
	return strings.Join([]string{
		`filters:`,
		`  or:`,
		`    - and:`,
		`      - file.ext == "md"`,
		`      - 'schema == "tusker.gate/v1"'`,
		`      - 'status == "open"'`,
		`      - 'owner == "human" || owner.startsWith("human:")'`,
		`    - and:`,
		`      - file.ext == "md"`,
		`      - 'schema == "tusker.task/v7"'`,
		`      - 'readiness == "waiting_on_human" || agent_action == "stop_until_human_response"'`,
		`properties:`,
		`  id:`,
		`    displayName: ID`,
		`  title:`,
		`    displayName: Title`,
		`  owner:`,
		`    displayName: Owner`,
		`  next_owner:`,
		`    displayName: Next owner`,
		`  blocks:`,
		`    displayName: Blocks`,
		`  next_action:`,
		`    displayName: Next action`,
		`  action:`,
		`    displayName: Gate action`,
		`views:`,
		`  - type: table`,
		`    name: "Human Actions"`,
		`    order: [id, title, owner, next_owner, blocks, action, next_action]`,
		``,
	}, "\n")
}

func validateV7GeneratedDashboards(vaultPath string) ([]Issue, []Issue) {
	if !hasV7DashboardProjection(vaultPath) {
		return nil, nil
	}
	idx, err := loadV7Index(vaultPath)
	if err != nil {
		return []Issue{issue(errorInvalidField, "failed to load V7 index for generated dashboard validation: "+err.Error(), "", "", nil)}, nil
	}
	leases, err := loadV7Leases(vaultPath)
	if err != nil {
		return []Issue{issue(errorInvalidField, "failed to load V7 leases for generated dashboard validation: "+err.Error(), "", "", nil)}, nil
	}
	var errs []Issue
	for rel, expected := range v7CommittedDashboardProjections(idx, leases) {
		path := filepath.Join(vaultPath, filepath.FromSlash(rel))
		actual, err := readText(path)
		if err != nil {
			errs = append(errs, issue("V7_GENERATED_DASHBOARD_MISSING", "V7 generated dashboard is missing: "+rel, rel, "run `tusker dashboard build --quiet`", nil))
			continue
		}
		if actual != expected {
			errs = append(errs, issue("V7_GENERATED_DASHBOARD_STALE", "V7 generated dashboard is stale: "+rel, rel, "run `tusker dashboard build --quiet`", nil))
		}
	}
	return errs, nil
}

func hasV7DashboardProjection(vaultPath string) bool {
	for rel := range v7CommittedDashboardProjections(v7Index{}, nil) {
		if fileExists(filepath.Join(vaultPath, filepath.FromSlash(rel))) {
			return true
		}
	}
	return false
}

func sortedV7Epics(idx v7Index) []Note {
	epics := make([]Note, 0, len(idx.Epics))
	for _, epic := range idx.Epics {
		epics = append(epics, epic)
	}
	sort.Slice(epics, func(i, j int) bool {
		return stringField(epics[i].Data, "id") < stringField(epics[j].Data, "id")
	})
	return epics
}

func v7EpicOpenGatesBlock(idx v7Index, epicID string) string {
	var rows []string
	for _, gate := range sortedV7Gates(idx) {
		if stringField(gate.Data, "status") != "open" || !v7GateTouchesEpic(idx, gate, epicID) {
			continue
		}
		rows = append(rows, fmt.Sprintf("| [[%s]] | %s | %s | %s |", stringField(gate.Data, "id"), stringField(gate.Data, "owner"), v7WikiLinks(normalizeList(gate.Data["blocks"])), stringField(gate.Data, "action")))
	}
	if len(rows) == 0 {
		rows = append(rows, "| _None._ |  |  |  |")
	}
	return "<!-- tusker:generated open-gates -->\n\n| Gate | Owner | Blocks | Action |\n|---|---|---|---|\n" + strings.Join(rows, "\n")
}

func v7EpicActiveWorkBlock(idx v7Index, epicID string) string {
	var rows []string
	for _, task := range sortedV7Tasks(idx) {
		if stringField(task.Data, "epic") != epicID {
			continue
		}
		status := stringField(task.Data, "status")
		if status == "done" || status == "cancelled" || status == "superseded" {
			continue
		}
		rows = append(rows, fmt.Sprintf("| [[%s]] | %s | %s | %s |", stringField(task.Data, "id"), status, stringField(task.Data, "next_owner"), stringField(task.Data, "next_action")))
	}
	if len(rows) == 0 {
		rows = append(rows, "| _None._ |  |  |  |")
	}
	return "<!-- tusker:generated active-work -->\n\n| Task | Status | Next owner | Next action |\n|---|---|---|---|\n" + strings.Join(rows, "\n")
}

func v7EpicRecentlyCompletedBlock(idx v7Index, epicID string) string {
	var rows []string
	for _, task := range sortedV7Tasks(idx) {
		if stringField(task.Data, "epic") != epicID || stringField(task.Data, "status") != "done" {
			continue
		}
		rows = append(rows, fmt.Sprintf("| [[%s]] | %s | %s |", stringField(task.Data, "id"), stringField(task.Data, "accepted_by"), stringField(task.Data, "closed_at")))
	}
	if len(rows) == 0 {
		rows = append(rows, "| _None._ |  | |")
	}
	return "<!-- tusker:generated recently-completed -->\n\n| Task | Accepted by | Closed at |\n|---|---|---|\n" + strings.Join(rows, "\n")
}

func v7GateTouchesEpic(idx v7Index, gate Note, epicID string) bool {
	if strings.HasPrefix(stringField(gate.Data, "id"), epicID+"-") {
		return true
	}
	for _, taskID := range normalizeList(gate.Data["blocks"]) {
		if task, ok := idx.Tasks[taskID]; ok && stringField(task.Data, "epic") == epicID {
			return true
		}
	}
	return false
}

func v7HumanActionsDashboard(idx v7Index) string {
	var rows []string
	for _, gate := range v7OpenHumanGates(idx) {
		rows = append(rows, fmt.Sprintf("| [[%s]] | %s | %s | %s |", stringField(gate.Data, "id"), stringField(gate.Data, "owner"), v7WikiLinks(normalizeList(gate.Data["blocks"])), stringField(gate.Data, "action")))
	}
	for _, task := range v7HumanWaitTasks(idx) {
		rows = append(rows, fmt.Sprintf("| [[%s]] | %s | %s | %s |", stringField(task.Data, "id"), stringField(task.Data, "next_owner"), stringField(task.Data, "next_ref"), stringField(task.Data, "next_action")))
	}
	sort.Strings(rows)
	return v7GeneratedDashboardHeader() + "# Human Actions\n\n<!-- tusker:generated:start human-actions -->\n\n| Item | Owner | Blocks / refs | Action |\n|---|---|---|---|\n" + strings.Join(rows, "\n") + "\n\n<!-- tusker:generated:end -->\n"
}

func v7AgentReadyDashboard(idx v7Index) string {
	var rows []string
	for _, task := range v7ReadyAgentTasks(idx) {
		rows = append(rows, fmt.Sprintf("| [[%s]] | %s | %s |", stringField(task.Data, "id"), stringField(task.Data, "priority"), stringField(task.Data, "next_action")))
	}
	sort.Strings(rows)
	return v7GeneratedDashboardHeader() + "# Agent Ready\n\n<!-- tusker:generated:start agent-ready -->\n\n| Task | Priority | Next action |\n|---|---|---|\n" + strings.Join(rows, "\n") + "\n\n<!-- tusker:generated:end -->\n"
}

func v7ReviewQueueDashboard(idx v7Index) string {
	var rows []string
	for _, task := range v7ReviewTasks(idx) {
		rows = append(rows, fmt.Sprintf("| [[%s]] | %s | %s |", stringField(task.Data, "id"), stringField(task.Data, "risk"), stringField(task.Data, "next_action")))
	}
	sort.Strings(rows)
	return v7GeneratedDashboardHeader() + "# Review Queue\n\n<!-- tusker:generated:start review-queue -->\n\n| Task | Risk | Next action |\n|---|---|---|\n" + strings.Join(rows, "\n") + "\n\n<!-- tusker:generated:end -->\n"
}

func v7CIWaitingDashboard(idx v7Index) string {
	var rows []string
	for _, task := range idx.Tasks {
		if stringField(task.Data, "readiness") == "waiting_on_ci" {
			rows = append(rows, fmt.Sprintf("| [[%s]] | %s |", stringField(task.Data, "id"), stringField(task.Data, "next_action")))
		}
	}
	sort.Strings(rows)
	return v7GeneratedDashboardHeader() + "# CI Waiting\n\n<!-- tusker:generated:start ci-waiting -->\n\n| Task | Next action |\n|---|---|\n" + strings.Join(rows, "\n") + "\n\n<!-- tusker:generated:end -->\n"
}

func v7DoneTaskOpenGateViolations(idx v7Index) []string {
	var violations []string
	for _, gate := range idx.Gates {
		if stringField(gate.Data, "status") != "open" || !boolField(gate.Data, "blocking") {
			continue
		}
		gateID := stringField(gate.Data, "id")
		for _, taskID := range normalizeList(gate.Data["blocks"]) {
			task, ok := idx.Tasks[taskID]
			if !ok || stringField(task.Data, "status") != "done" {
				continue
			}
			violations = append(violations, taskID+"<-"+gateID)
		}
	}
	sort.Strings(violations)
	return violations
}

func v7ActiveRunsDashboard(leases []v7LeaseRecord) string {
	var rows []string
	now := time.Now().UTC()
	for _, lease := range leases {
		if lease.Status != "active" && lease.Status != "stale" {
			continue
		}
		state := lease.Status
		if state == "active" && v7LeaseExpired(lease, now) {
			state = "stale"
		}
		rows = append(rows, fmt.Sprintf("| [[%s]] | `%s` | %s | `%s` | `%s` |", lease.Task, state, lease.Owner, lease.Branch, lease.ExpiresAt))
	}
	sort.Strings(rows)
	return v7GeneratedDashboardHeader() + "# Active Runs\n\n<!-- tusker:generated:start active-runs -->\n\n| Task | Runtime state | Owner | Branch | Expires |\n|---|---|---|---|---|\n" + strings.Join(rows, "\n") + "\n\n<!-- tusker:generated:end -->\n"
}

func v7GeneratedDashboardHeader() string {
	return "<!-- tusker:generated:file; do not edit manually; run `tusker dashboard build` -->\n\n"
}

func v7LeaseTTL(vaultPath string) time.Duration {
	cfg, _, err := readV7TuskerConfig(vaultPath)
	if err == nil && cfg.Runtime.LeaseTTLMinutes > 0 {
		return time.Duration(cfg.Runtime.LeaseTTLMinutes) * time.Minute
	}
	return 120 * time.Minute
}

func reconcileV7Leases(vaultPath string) (int, error) {
	leases, err := loadV7Leases(vaultPath)
	if err != nil {
		return 0, err
	}
	now := time.Now().UTC()
	stale := 0
	for _, lease := range leases {
		if lease.Status != "active" || !v7LeaseExpired(lease, now) {
			continue
		}
		lease.Status = "stale"
		lease.StaleAt = now.Format(time.RFC3339)
		lease.StaleReason = "lease expired without heartbeat"
		path := lease.Path
		lease.Path = ""
		if err := writeJSON(path, lease); err != nil {
			return stale, err
		}
		stale++
		_ = emitV7Event(vaultPath, lease.Task, "task", "lease_stale", "tusker:reconcile", map[string]any{"lease": lease.ID, "owner": lease.Owner, "expires_at": lease.ExpiresAt})
	}
	return stale, nil
}

func loadV7Leases(vaultPath string) ([]v7LeaseRecord, error) {
	paths, err := filepath.Glob(filepath.Join(v7LeaseDir(vaultPath), "*.json"))
	if err != nil {
		return nil, err
	}
	var leases []v7LeaseRecord
	for _, path := range paths {
		raw, err := readText(path)
		if err != nil {
			return nil, err
		}
		var lease v7LeaseRecord
		if err := json.Unmarshal([]byte(raw), &lease); err != nil {
			return nil, err
		}
		if lease.Task == "" {
			lease.Task = strings.TrimSuffix(filepath.Base(path), ".json")
		}
		lease.Path = path
		leases = append(leases, lease)
	}
	sort.Slice(leases, func(i, j int) bool {
		if leases[i].Task == leases[j].Task {
			return leases[i].ID < leases[j].ID
		}
		return leases[i].Task < leases[j].Task
	})
	return leases, nil
}

func v7LeaseDir(vaultPath string) string {
	return filepath.Join(vaultPath, "..", ".tusker-local", "leases")
}

func v7LeaseExpired(lease v7LeaseRecord, now time.Time) bool {
	expiresAt, err := time.Parse(time.RFC3339, lease.ExpiresAt)
	if err != nil {
		return false
	}
	return !expiresAt.After(now)
}

func v7LeaseIndexRecords(leases []v7LeaseRecord) []map[string]any {
	now := time.Now().UTC()
	records := make([]map[string]any, 0, len(leases))
	for _, lease := range leases {
		state := lease.Status
		if state == "active" && v7LeaseExpired(lease, now) {
			state = "stale"
		}
		records = append(records, map[string]any{
			"id":            lease.ID,
			"task":          lease.Task,
			"owner":         lease.Owner,
			"workspace":     lease.Workspace,
			"branch":        lease.Branch,
			"status":        lease.Status,
			"runtime_state": state,
			"claimed_at":    lease.ClaimedAt,
			"heartbeat_at":  lease.HeartbeatAt,
			"expires_at":    lease.ExpiresAt,
			"stale_at":      lease.StaleAt,
		})
	}
	return records
}

func v7ActiveLeases(leases []v7LeaseRecord) []v7LeaseRecord {
	now := time.Now().UTC()
	var active []v7LeaseRecord
	for _, lease := range leases {
		if lease.Status == "active" && !v7LeaseExpired(lease, now) {
			active = append(active, lease)
		}
	}
	return active
}

func v7StaleLeases(leases []v7LeaseRecord) []v7LeaseRecord {
	now := time.Now().UTC()
	var stale []v7LeaseRecord
	for _, lease := range leases {
		if lease.Status == "stale" || (lease.Status == "active" && v7LeaseExpired(lease, now)) {
			stale = append(stale, lease)
		}
	}
	return stale
}

func v7StateBranch(vaultPath string) string {
	cfg, _, err := readV7TuskerConfig(vaultPath)
	if err == nil && strings.TrimSpace(cfg.Branches.StateBranch) != "" {
		return strings.TrimSpace(cfg.Branches.StateBranch)
	}
	return "tusker/state"
}

func v7StateFiles(vaultPath string) ([]v7StateFile, error) {
	leases, err := loadV7Leases(vaultPath)
	if err != nil {
		return nil, err
	}
	var files []v7StateFile
	for _, lease := range leases {
		lease.Path = ""
		raw, err := json.MarshalIndent(lease, "", "  ")
		if err != nil {
			return nil, err
		}
		files = append(files, v7StateFile{
			Path:    filepath.ToSlash(filepath.Join("leases", lease.Task+".json")),
			Content: string(raw) + "\n",
		})
	}
	index := map[string]any{
		"schema":       "tusker.scheduler-index/v1",
		"generated_at": time.Now().UTC().Format(time.RFC3339),
		"lease_count":  len(leases),
	}
	raw, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return nil, err
	}
	files = append(files, v7StateFile{Path: "scheduler/index.json", Content: string(raw) + "\n"})
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, nil
}

func exportV7StateDir(vaultPath, dir string) (int, error) {
	files, err := v7StateFiles(vaultPath)
	if err != nil {
		return 0, err
	}
	for _, file := range files {
		if err := writeText(filepath.Join(dir, filepath.FromSlash(file.Path)), file.Content); err != nil {
			return 0, err
		}
	}
	return len(files), nil
}
