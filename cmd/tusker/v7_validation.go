package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

var (
	v7RawLogLinePattern     = regexp.MustCompile(`(?i)\b(PASS|FAIL|panic:|goroutine \d+|exit status \d+|npm ERR!|Traceback \(most recent call last\))\b`)
	v7AcceptanceIDLine      = regexp.MustCompile(`(?i)^A\d+\s*:\s*(.+)$`)
	v7ProtectedCommonFields = makeSet("schema", "kind", "id", "project", "state_rev")
	v7ProtectedFieldsByKind = map[string]map[string]struct{}{
		"task":          makeSet("status", "readiness", "wave", "next_owner", "next_source", "next_ref", "next_action", "accepted_by", "accepted_at", "closed_at", "superseded_by"),
		"gate":          makeSet("status", "owner", "blocking", "blocks", "satisfaction_evidence", "satisfaction_evidence_refs", "satisfied_by", "satisfied_at", "waived_by", "waived_at", "waive_reason", "obsolete_reason"),
		"wave":          makeSet("status", "landed_at"),
		"epic":          makeSet("status", "owner", "priority", "next_task_number", "next_gate_number", "next_decision_number"),
		"decision":      makeSet("status", "decided_by", "decided_at", "supersedes"),
		"evidence":      makeSet("task", "status", "accepted_by", "accepted_at", "screenshot_checked_by", "screenshot_checked_at", "redacted", "redaction_note"),
		"attempt":       makeSet("task", "status", "ended_at", "pr_url", "evidence"),
		"proposal":      makeSet("status", "reviewed_by", "reviewed_at", "review_reason", "applying_by", "applying_at", "apply_transaction", "applied_by", "applied_at", "applied_target", "applied_target_rev"),
		"closeout":      makeSet("task", "state", "agent_action", "state_fingerprint"),
		"domain":        makeSet("status"),
		"project_skill": makeSet("status"),
	}
	v7EventKinds       = makeSet("created", "updated", "status_changed", "gate_added", "gate_satisfied", "gate_waived", "gate_obsoleted", "claimed", "claim_released", "attempt_started", "attempt_handoff", "attempt_failed", "verification_added", "evidence_added", "review_requested", "review_passed", "review_failed", "closed", "reopened", "superseded", "cancelled", "decision_accepted", "lease_stale", "redaction", "redacted_replacement")
	v7EventObjectKinds = makeSet("task", "gate", "wave", "epic", "decision", "evidence", "attempt", "proposal", "domain", "closeout")
	v7KnowledgeKinds   = makeSet("runbook", "decision", "invariant", "interface", "glossary", "source")
)

func validateV7Note(note Note, ctx validationContext, where string) ([]Issue, []Issue) {
	var errors, warnings []Issue
	if stringField(note.Data, "schema") == "tusker.knowledge/v7" {
		validateV7KnowledgeNode(note, ctx, where, &errors, &warnings)
		validateV7BodyBudget(note, ctx.VaultPath, where, &errors, &warnings)
		validateV7FrontmatterSize(note, ctx, where, &warnings)
		validateV7RecordSecrets(note, where, &errors)
		return errors, warnings
	}
	kind := effectiveV7Kind(note.Data)
	switch kind {
	case "task":
		validateV7Task(note, ctx, where, &errors, &warnings)
	case "gate":
		validateV7Gate(note, where, &errors, &warnings)
	case "wave":
		validateV7Wave(note, ctx, where, &errors, &warnings)
	case "evidence":
		validateV7Evidence(note, ctx, where, &errors, &warnings)
	case "attempt":
		validateV7Attempt(note, where, &errors, &warnings)
	case "decision":
		validateV7Decision(note, where, &errors, &warnings)
	case "epic":
		validateV7Epic(note, where, &errors, &warnings)
	case "proposal":
		validateV7Proposal(note, ctx, where, &errors, &warnings)
	case "closeout":
		validateV7Closeout(note, ctx, where, &errors)
	case "domain":
		validateV7Domain(note, ctx, where, &errors, &warnings)
	case "domain_canon":
		validateV7DomainCanon(note, ctx, where, &errors, &warnings)
	case "project_skill":
		validateV7ProjectSkill(note, ctx, where, &errors, &warnings)
	default:
		errors = append(errors, issue(errorUnknownType, fmt.Sprintf(`unknown V7 object kind "%s"`, kind), where, "", map[string]any{"kind": kind}))
	}
	validateV7BodyBudget(note, ctx.VaultPath, where, &errors, &warnings)
	validateV7FrontmatterSize(note, ctx, where, &warnings)
	validateV7RecordSecrets(note, where, &errors)
	return errors, warnings
}

func validateV7FrontmatterSize(note Note, ctx validationContext, where string, warnings *[]Issue) {
	limit := v7FrontmatterWarnLimit(ctx.VaultPath)
	if limit <= 0 || len(note.Data) <= limit {
		return
	}
	*warnings = append(*warnings, issue("FRONTMATTER_LONG", fmt.Sprintf("V7 frontmatter has %d fields; warning limit is %d", len(note.Data), limit), where, "", map[string]any{"fields": len(note.Data), "limit": limit}))
}

func validateV7Task(note Note, ctx validationContext, where string, errors, warnings *[]Issue) {
	data := note.Data
	id := stringField(data, "id")
	policy := v7ValidationPolicyFor(ctx.VaultPath)
	for _, field := range []string{"schema", "kind", "id", "project", "title", "status", "risk", "priority"} {
		if stringField(data, field) == "" {
			*errors = append(*errors, issue(errorMissingField, fmt.Sprintf(`missing required frontmatter "%s"`, field), where, "", map[string]any{"field": field}))
		}
	}
	if stringField(data, "schema") != "tusker.task/v7" {
		*errors = append(*errors, issue(errorInvalidField, "V7 task schema must be tusker.task/v7", where, "", map[string]any{"field": "schema"}))
	}
	if stringField(data, "kind") != "task" {
		*errors = append(*errors, issue(errorInvalidField, "V7 task kind must be task", where, "", map[string]any{"field": "kind"}))
	}
	if !v7TaskIDPattern.MatchString(id) {
		*errors = append(*errors, issue(errorIDScheme, "V7 task id must match ABC-T-0001", where, "", map[string]any{"id": id}))
	}
	if wave := stringField(data, "wave"); wave != "" && !v7WaveIDPattern.MatchString(wave) {
		*errors = append(*errors, issue(errorInvalidField, "invalid V7 task wave: "+wave, where, "", map[string]any{"field": "wave"}))
	}
	if !strings.HasSuffix(filepath.ToSlash(where), "work/tasks/"+id+".md") {
		*errors = append(*errors, issue(errorPathMismatch, "V7 task path must be .tusker/work/tasks/"+id+".md", where, "", nil))
	}
	if _, ok := v7TaskStatuses[stringField(data, "status")]; !ok {
		*errors = append(*errors, issue(errorInvalidField, "invalid V7 task status: "+stringField(data, "status"), where, "", map[string]any{"field": "status"}))
	}
	if readiness := stringField(data, "readiness"); readiness != "" {
		if _, ok := v7Readiness[readiness]; !ok {
			*errors = append(*errors, issue(errorInvalidField, "invalid V7 readiness: "+readiness, where, "", map[string]any{"field": "readiness"}))
		}
	}
	if status := stringField(data, "status"); status != "done" && status != "cancelled" && status != "superseded" {
		if stringField(data, "next_owner") == "" || stringField(data, "next_action") == "" {
			*errors = append(*errors, issue("TASK_MISSING_NEXT_ACTION", "open V7 task requires next_owner and next_action", where, "", nil))
		}
	}
	validateV7TaskProofPolicy(note, ctx, where, errors, warnings)
	validateV7TaskReadiness(note, ctx, where, errors)
	if stringField(data, "status") == "done" {
		if policy.RequireAcceptanceProof && (stringField(data, "accepted_by") == "" || stringField(data, "accepted_at") == "") {
			*errors = append(*errors, issue("DONE_TASK_ACCEPTANCE_MISSING", "done V7 task requires accepted_by and accepted_at", where, "", nil))
		}
		for _, gate := range findV7BlockingGates(ctx.VaultPath, id, normalizeList(data["gates"])) {
			if stringField(gate.Data, "status") == "open" && boolField(gate.Data, "blocking") {
				*errors = append(*errors, issue("DONE_TASK_OPEN_GATE", "done V7 task has open blocking gate "+stringField(gate.Data, "id"), where, "", nil))
			}
		}
		if missing := missingRequiredEvidence(ctx.VaultPath, id, normalizeList(data["evidence_required"])); len(missing) > 0 {
			*errors = append(*errors, issue("EVIDENCE_REQUIRED_MISSING", "done V7 task missing required evidence: "+strings.Join(missing, ", "), where, "", map[string]any{"missing": missing}))
		}
		idx, err := loadV7Index(ctx.VaultPath)
		if err == nil {
			report := computeV7ProofReport(ctx.VaultPath, note, idx)
			for _, acceptance := range report.Missing {
				*errors = append(*errors, issue("DONE_TASK_ACCEPTANCE_PROOF_MISSING", "done V7 task missing proof for "+acceptance, where, "cover acceptance with inline verification, accepted evidence, satisfied gate, or explicit waiver", map[string]any{"acceptance": acceptance}))
			}
			for _, missing := range report.ModeMissing {
				*errors = append(*errors, issue("DONE_TASK_PROOF_MODE_MISSING", "done V7 task proof_mode requirement missing: "+missing, where, "satisfy proof_mode or record an explicit waiver", map[string]any{"missing": missing}))
			}
		}
		validateV7DoneTaskClosePolicy(note, ctx, where, errors)
	}
	validateV7TaskBodyPolicy(note.Body, ctx.VaultPath, where, errors, warnings)
	if policy.RequireAcceptanceProof && !v7AcceptanceHasProof(note.Body) {
		*warnings = append(*warnings, issue("ACCEPTANCE_PROOF_MISSING", "V7 task acceptance should include a proof column or proof text", where, "", nil))
	}
	if vague := v7VagueAcceptanceItems(note.Body); len(vague) > 0 {
		*warnings = append(*warnings, issue("ACCEPTANCE_TOO_VAGUE", "V7 task acceptance contains vague criteria: "+strings.Join(vague, ", "), where, "replace vague checks with observable outcomes and proof", map[string]any{"items": vague}))
	}
	verification := sectionContent(note.Body, "## Verification")
	if strings.TrimSpace(verification) == "" {
		*warnings = append(*warnings, issue("VERIFICATION_MISSING", "V7 task verification should name exact commands or manual proof", where, "", nil))
	} else if !v7TextHasExactVerificationProof(verification) {
		*warnings = append(*warnings, issue("VERIFICATION_PROOF_MISSING", "V7 task verification should name exact commands or manual proof", where, "", nil))
	}
	if len(normalizeList(data["domains"])) > 5 {
		*warnings = append(*warnings, issue("TASK_TOO_MANY_DOMAINS", "V7 task should stay focused on a small number of domains", where, "", map[string]any{"count": len(normalizeList(data["domains"]))}))
	}
	validateV7KnowledgeDelta(note.Body, where, warnings)
}

func validateV7DoneTaskClosePolicy(note Note, ctx validationContext, where string, errors *[]Issue) {
	data := note.Data
	id := stringField(data, "id")
	risk := strings.ToLower(fallback(stringField(data, "risk"), "medium"))
	actor := stringField(data, "accepted_by")
	policy, err := v7ClosePolicyFor(ctx.VaultPath, risk)
	if err != nil {
		*errors = append(*errors, issue(errorConfigInvalid, err.Error(), "../tusker.yaml", "", map[string]any{"risk": risk}))
		return
	}
	requiredAcceptor := policy.RequiredAcceptor
	if actor != "" && !v7CloseAcceptorAllowed(actor, requiredAcceptor) {
		*errors = append(*errors, issue("DONE_TASK_ACCEPTOR_INVALID", "done V7 task requires "+requiredAcceptor+" acceptor for "+risk+" risk", where, "", map[string]any{"risk": risk, "actor": actor, "required_acceptor": requiredAcceptor}))
	}
	requiredEvidence := mergeUniqueStrings(normalizeList(data["evidence_required"]), policy.RequiredEvidence)
	if missing := missingRequiredEvidence(ctx.VaultPath, id, requiredEvidence); len(missing) > 0 {
		*errors = append(*errors, issue("CLOSE_POLICY_EVIDENCE_MISSING", "done V7 task missing close-policy evidence: "+strings.Join(missing, ", "), where, "", map[string]any{"risk": risk, "missing": missing}))
	}
	idx, err := loadV7Index(ctx.VaultPath)
	if err != nil {
		return
	}
	for _, kind := range policy.RequiredGates {
		if !v7CloseGateKindSatisfied(idx, id, kind) {
			*errors = append(*errors, issue("CLOSE_POLICY_GATE_MISSING", "done V7 task requires satisfied or waived "+kind+" gate for "+risk+" risk", where, "", map[string]any{"risk": risk, "gate_kind": kind}))
		}
	}
}

func validateV7KnowledgeDelta(body, where string, warnings *[]Issue) {
	content := strings.TrimSpace(sectionContent(body, "## Knowledge delta"))
	if content == "" {
		return
	}
	lines := 0
	for _, line := range strings.Split(content, "\n") {
		if strings.TrimSpace(line) != "" {
			lines++
		}
	}
	if lines > 20 || len(content) > 1500 {
		*warnings = append(*warnings, issue("KNOWLEDGE_DELTA_TOO_LONG", "V7 task knowledge delta should stay concise; move durable truth to domain canon/docs", where, "", map[string]any{"lines": lines, "bytes": len(content)}))
	}
}

func validateV7TaskProofPolicy(note Note, ctx validationContext, where string, errors, warnings *[]Issue) {
	data := note.Data
	status := stringField(data, "status")
	terminalWithoutProof := status == "cancelled" || status == "superseded"
	mode := strings.ToLower(strings.TrimSpace(stringField(data, "proof_mode")))
	proofStatus := strings.ToLower(strings.TrimSpace(stringField(data, "proof_status")))
	if !terminalWithoutProof {
		if mode == "" {
			*errors = append(*errors, issue("TASK_PROOF_MODE_MISSING", "executable V7 task requires proof_mode", where, "set proof_mode to none, inline, card, artifact, or audit", nil))
		} else if _, ok := v7ProofModes[mode]; !ok {
			*errors = append(*errors, issue(errorInvalidField, "invalid V7 proof_mode: "+mode, where, "", map[string]any{"field": "proof_mode"}))
		}
		if proofStatus == "" {
			*errors = append(*errors, issue("TASK_PROOF_STATUS_MISSING", "V7 task requires proof_status", where, "set proof_status to pending, partial, satisfied, or waived", nil))
		} else if _, ok := v7ProofStatuses[proofStatus]; !ok {
			*errors = append(*errors, issue(errorInvalidField, "invalid V7 proof_status: "+proofStatus, where, "", map[string]any{"field": "proof_status"}))
		}
		if mode != "" && mode != "none" && len(normalizeList(data["proof_required"])) == 0 {
			*errors = append(*errors, issue("TASK_PROOF_REQUIRED_MISSING", "V7 task proof_mode="+mode+" requires proof_required", where, "record the proof classes expected for close", nil))
		}
	}
	if boolField(data, "raw_artifacts_allowed") && strings.TrimSpace(stringField(data, "raw_artifacts_reason")) == "" {
		*warnings = append(*warnings, issue("RAW_ARTIFACTS_REASON_MISSING", "raw_artifacts_allowed should include raw_artifacts_reason", where, "", nil))
	}
	if ctx.VaultPath == "" || stringField(data, "id") == "" {
		return
	}
	idx, err := loadV7Index(ctx.VaultPath)
	if err != nil {
		return
	}
	task := note
	report := computeV7ProofReport(ctx.VaultPath, task, idx)
	if proofStatus == "satisfied" && len(v7PacketStubAcceptanceItems(task.Body)) > 0 && len(v7AcceptanceWaivers(data)) == 0 {
		*errors = append(*errors, issue("TASK_PROOF_STATUS_PLACEHOLDER_ACCEPTANCE", "proof_status=satisfied but acceptance is placeholder or vague", where, "replace stub acceptance with observable outcomes and proof mapping, or record an explicit waiver", nil))
	}
	if proofStatus == "satisfied" && (len(report.Missing) > 0 || len(report.ModeMissing) > 0) {
		missing := append([]string{}, report.Missing...)
		missing = append(missing, report.ModeMissing...)
		*errors = append(*errors, issue("TASK_PROOF_STATUS_STALE", "proof_status=satisfied but proof is incomplete: "+strings.Join(missing, ", "), where, "run `tusker proof status "+stringField(data, "id")+"` and satisfy missing proof", map[string]any{"missing": missing}))
	}
	if status == "done" && proofStatus != "satisfied" && proofStatus != "waived" {
		*errors = append(*errors, issue("DONE_TASK_PROOF_STATUS_INCOMPLETE", "done V7 task requires proof_status satisfied or waived", where, "", map[string]any{"proof_status": proofStatus}))
	}
	if status == "review" && proofStatus != "satisfied" && proofStatus != "waived" {
		finding := issue("REVIEW_TASK_PROOF_INCOMPLETE", "review V7 task has incomplete proof_status: "+fallback(proofStatus, "(missing)"), where, "satisfy proof before moving a handed-off task to review, or create a blocking gate", nil)
		if v7LatestAttemptIsHandoff(idx, stringField(data, "id")) || v7ValidationPolicyFor(ctx.VaultPath).StrictProofPolicy {
			*errors = append(*errors, finding)
		} else {
			*warnings = append(*warnings, finding)
		}
	}
	budget := intField(data, "evidence_budget")
	evidenceCount := len(idx.Evidence[stringField(data, "id")])
	if budget >= 0 && evidenceCount > budget {
		finding := issue("EVIDENCE_BUDGET_EXCEEDED", fmt.Sprintf("task has %d evidence card%s but evidence_budget is %d", evidenceCount, plural(evidenceCount), budget), where, "use inline verification, one summary card, or move low-signal detail to attempt/scratch", map[string]any{"count": evidenceCount, "budget": budget})
		if v7ValidationPolicyFor(ctx.VaultPath).StrictProofPolicy {
			*errors = append(*errors, finding)
		} else {
			*warnings = append(*warnings, finding)
		}
	}
	if status != "review" && status != "done" && report.Status == "satisfied" && v7LatestAttemptIsHandoff(idx, stringField(data, "id")) && !v7TaskHasReviewProposal(idx, stringField(data, "id")) && len(report.OpenGates) == 0 {
		projected := v7ProjectedTaskState(ctx.VaultPath, task, idx)
		switch stringField(projected, "readiness") {
		case "blocked_by_gate", "blocked_by_dependency", "waiting_on_human", "waiting_on_ci", "held":
			return
		}
		*errors = append(*errors, issue("COMPLETED_ATTEMPT_WITHOUT_REVIEW_OR_GATE", "completed attempt has satisfied proof but task is not in review and has no blocking gate", where, "run `tusker finish <task-id> --request-review` or create a blocking gate", nil))
	}
}

func validateV7TaskReadiness(note Note, ctx validationContext, where string, errors *[]Issue) {
	data := note.Data
	id := stringField(data, "id")
	status := stringField(data, "status")
	readiness := stringField(data, "readiness")
	if status == "done" || status == "cancelled" || status == "superseded" {
		if readiness != "" && readiness != status {
			*errors = append(*errors, issue("READINESS_STALE", fmt.Sprintf("terminal task status %s requires readiness %s", status, status), where, "", nil))
		}
		return
	}
	hasOpenGate := false
	for _, gate := range findV7BlockingGates(ctx.VaultPath, id, normalizeList(data["gates"])) {
		if stringField(gate.Data, "status") == "open" && boolField(gate.Data, "blocking") {
			hasOpenGate = true
			break
		}
	}
	hasUnresolvedDep := false
	for _, depID := range normalizeList(data["dependencies"]) {
		dep, err := resolveV7Note(ctx.VaultPath, depID, "task")
		if err == nil && stringField(dep.Data, "status") != "done" {
			hasUnresolvedDep = true
			break
		}
	}
	switch readiness {
	case "blocked_by_gate":
		if !hasOpenGate {
			*errors = append(*errors, issue("READINESS_STALE", "readiness blocked_by_gate requires an open blocking gate", where, "", nil))
		}
	case "waiting_on_human":
		owner := stringField(data, "next_owner")
		if v7ProofOwnerClass(owner) != "human" {
			*errors = append(*errors, issue("READINESS_STALE", "readiness waiting_on_human requires next_owner human:<name>", where, "", map[string]any{"next_owner": owner}))
		}
	case "blocked_by_dependency":
		if !hasUnresolvedDep {
			*errors = append(*errors, issue("READINESS_STALE", "readiness blocked_by_dependency requires an unresolved dependency", where, "", nil))
		}
	case "ready":
		if hasOpenGate {
			*errors = append(*errors, issue("READINESS_STALE", "readiness ready conflicts with an open blocking gate", where, "run `tusker reconcile`", nil))
		}
		if hasUnresolvedDep {
			*errors = append(*errors, issue("READINESS_STALE", "readiness ready conflicts with an unresolved dependency", where, "run `tusker reconcile`", nil))
		}
	}
}

func v7LatestAttemptIsHandoff(idx v7Index, taskID string) bool {
	attempts := append([]Note{}, idx.Attempts[taskID]...)
	if len(attempts) == 0 {
		return false
	}
	sort.Slice(attempts, func(i, j int) bool { return stringField(attempts[i].Data, "id") < stringField(attempts[j].Data, "id") })
	return stringField(attempts[len(attempts)-1].Data, "status") == "handoff"
}

func v7TaskHasReviewProposal(idx v7Index, taskID string) bool {
	for _, proposal := range idx.Proposals {
		if stringField(proposal.Data, "status") != "proposed" || stringField(proposal.Data, "action") != "status" || stringField(proposal.Data, "target") != taskID {
			continue
		}
		fields := v7ProposalFieldMap(proposal.Data["proposed_fields"])
		if strings.ToLower(toString(fields["status"])) == "review" {
			return true
		}
	}
	return false
}

func findV7BlockingGates(vaultPath, taskID string, gateIDs []string) []Note {
	idx, err := loadV7Index(vaultPath)
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var gates []Note
	for _, gateID := range gateIDs {
		if gate, ok := idx.Gates[gateID]; ok {
			gates = append(gates, gate)
			seen[gateID] = true
		}
	}
	for _, gate := range idx.Gates {
		gateID := stringField(gate.Data, "id")
		if seen[gateID] {
			continue
		}
		if containsString(normalizeList(gate.Data["blocks"]), taskID) {
			gates = append(gates, gate)
		}
	}
	return gates
}

func validateV7Wave(note Note, ctx validationContext, where string, errors, warnings *[]Issue) {
	data := note.Data
	id := stringField(data, "id")
	for _, field := range []string{"schema", "kind", "id", "project", "title", "status", "members", "created_at", "created_by", "updated_at", "updated_by", "state_rev"} {
		if field == "members" {
			if len(normalizeList(data[field])) == 0 {
				*errors = append(*errors, issue(errorMissingField, `missing required frontmatter "members"`, where, "", map[string]any{"field": field}))
			}
			continue
		}
		if stringField(data, field) == "" {
			*errors = append(*errors, issue(errorMissingField, fmt.Sprintf(`missing required frontmatter "%s"`, field), where, "", map[string]any{"field": field}))
		}
	}
	if stringField(data, "schema") != "tusker.wave/v7" {
		*errors = append(*errors, issue(errorInvalidField, "V7 wave schema must be tusker.wave/v7", where, "", map[string]any{"field": "schema"}))
	}
	if stringField(data, "kind") != "wave" {
		*errors = append(*errors, issue(errorInvalidField, "V7 wave kind must be wave", where, "", map[string]any{"field": "kind"}))
	}
	if !v7WaveIDPattern.MatchString(id) {
		*errors = append(*errors, issue(errorIDScheme, "V7 wave id must match W-0001", where, "", map[string]any{"id": id}))
	}
	if !strings.HasSuffix(filepath.ToSlash(where), "work/waves/"+id+".md") {
		*errors = append(*errors, issue(errorPathMismatch, "V7 wave path must be .tusker/work/waves/"+id+".md", where, "", nil))
	}
	status := stringField(data, "status")
	if _, ok := v7WaveStatuses[status]; !ok {
		*errors = append(*errors, issue(errorInvalidField, "invalid V7 wave status: "+status, where, "", map[string]any{"field": "status"}))
	}
	if status == "landed" && stringField(data, "landed_at") == "" {
		*errors = append(*errors, issue(errorInvalidField, "landed V7 wave requires landed_at", where, "run `tusker reconcile`", map[string]any{"field": "landed_at"}))
	}
	if status == "open" && stringField(data, "landed_at") != "" {
		*warnings = append(*warnings, issue("WAVE_OPEN_LANDED_AT_STALE", "open V7 wave should not have landed_at", where, "run `tusker reconcile`", map[string]any{"field": "landed_at"}))
	}
	members := normalizeList(data["members"])
	seen := map[string]bool{}
	idx, _ := loadV7Index(ctx.VaultPath)
	for _, member := range members {
		if !v7TaskIDPattern.MatchString(member) {
			*errors = append(*errors, issue(errorInvalidField, "invalid V7 wave member task id: "+member, where, "", map[string]any{"member": member}))
			continue
		}
		if seen[member] {
			*errors = append(*errors, issue("WAVE_DUPLICATE_MEMBER", "V7 wave lists duplicate member "+member, where, "", map[string]any{"member": member}))
		}
		seen[member] = true
		if ctx.VaultPath != "" {
			if _, ok := idx.Tasks[member]; !ok {
				*errors = append(*errors, issue("WAVE_UNKNOWN_MEMBER", "V7 wave references unknown task "+member, where, "", map[string]any{"member": member}))
			}
		}
	}
	if status != "open" || ctx.VaultPath == "" {
		return
	}
	for _, other := range idx.Waves {
		otherID := stringField(other.Data, "id")
		if otherID == id || stringField(other.Data, "status") != "open" {
			continue
		}
		for _, member := range members {
			if containsString(normalizeList(other.Data["members"]), member) {
				*errors = append(*errors, issue("WAVE_OPEN_MEMBER_CONFLICT", fmt.Sprintf("%s belongs to multiple open waves: %s and %s", member, id, otherID), where, "", map[string]any{"member": member, "other_wave": otherID}))
			}
		}
	}
}

func validateV7Gate(note Note, where string, errors, warnings *[]Issue) {
	data := note.Data
	id := stringField(data, "id")
	for _, field := range []string{"schema", "kind", "id", "project", "title", "gate_kind", "status", "owner", "action"} {
		if stringField(data, field) == "" {
			*errors = append(*errors, issue(errorMissingField, fmt.Sprintf(`missing required frontmatter "%s"`, field), where, "", map[string]any{"field": field}))
		}
	}
	if stringField(data, "schema") != "tusker.gate/v1" {
		*errors = append(*errors, issue(errorInvalidField, "V7 gate schema must be tusker.gate/v1", where, "", map[string]any{"field": "schema"}))
	}
	if stringField(data, "kind") != "gate" {
		*errors = append(*errors, issue(errorInvalidField, "V7 gate kind must be gate", where, "", map[string]any{"field": "kind"}))
	}
	if !v7GateIDPattern.MatchString(id) {
		*errors = append(*errors, issue(errorIDScheme, "V7 gate id must match ABC-G-0001", where, "", map[string]any{"id": id}))
	}
	if !strings.HasSuffix(filepath.ToSlash(where), "work/gates/"+id+".md") {
		*errors = append(*errors, issue(errorPathMismatch, "V7 gate path must be .tusker/work/gates/"+id+".md", where, "", nil))
	}
	if _, ok := v7GateKinds[stringField(data, "gate_kind")]; !ok {
		*errors = append(*errors, issue(errorInvalidField, "invalid V7 gate kind: "+stringField(data, "gate_kind"), where, "", nil))
	}
	if _, ok := v7GateStatuses[stringField(data, "status")]; !ok {
		*errors = append(*errors, issue(errorInvalidField, "invalid V7 gate status: "+stringField(data, "status"), where, "", nil))
	}
	if boolField(data, "blocking") && len(normalizeList(data["blocks"])) == 0 {
		*errors = append(*errors, issue("GATE_BLOCKS_EMPTY", "blocking gate requires blocks", where, "", nil))
	}
	if boolField(data, "blocking") && stringField(data, "verification") == "" {
		*errors = append(*errors, issue("GATE_MISSING_VERIFICATION", "blocking gate requires verification", where, "", nil))
	}
	if v7GateTextIsPlaceholder(stringField(data, "action")) {
		*errors = append(*errors, issue("GATE_PLACEHOLDER_ACTION", "gate action is placeholder text", where, "state the exact owner action that unblocks the task", nil))
	}
	if v7GateTextIsPlaceholder(stringField(data, "verification")) {
		*errors = append(*errors, issue("GATE_PLACEHOLDER_VERIFICATION", "gate verification is placeholder text", where, "state the concrete command, artifact, or owner decision that proves the gate is satisfied", nil))
	}
	if boolField(data, "blocking") && v7GateOwnerNeedsAgentBoundary(stringField(data, "owner")) && !v7GateHasAgentBoundary(note) {
		*errors = append(*errors, issue("GATE_MISSING_AGENT_BOUNDARY", "human/external blocking gate requires why_agent_cannot or a Why agent cannot do this section", where, "", nil))
	}
	if boolField(data, "blocking") && v7HumanGateOwnsAgentCapableWork(stringField(data, "gate_kind"), stringField(data, "owner"), stringField(data, "action"), stringField(data, "verification"), v7GateBoundaryText(note), v7GateSuggestionText(note)) {
		*errors = append(*errors, issue("GATE_HUMAN_OWNS_AGENT_CAPABLE_WORK", "human gate appears to own agent-capable review work", where, "use an independent reviewer/subagent for code review, diffs, test inspection, or implementation judgment", nil))
	}
	if boolField(data, "blocking") && v7GateOwnerNeedsAgentBoundary(stringField(data, "owner")) && stringField(data, "gate_kind") == "decision" && !v7GateHasSuggestion(note) {
		*errors = append(*errors, issue("GATE_DECISION_SUGGESTION_MISSING", "human/external decision gate requires a suggestion or recommendation", where, "include the agent's recommended choice or repair path", nil))
	}
	if boolField(data, "blocking") && len(normalizeList(data["covers"])) == 0 {
		*warnings = append(*warnings, issue("GATE_MISSING_ACCEPTANCE_COVERAGE", "blocking gate should name the acceptance/proof gap it covers", where, "", nil))
	}
	if stringField(data, "status") == "satisfied" && (stringField(data, "satisfied_by") == "" || stringField(data, "satisfied_at") == "") {
		*errors = append(*errors, issue("GATE_SATISFIED_METADATA_MISSING", "satisfied gate requires satisfied_by and satisfied_at", where, "", nil))
	}
	if stringField(data, "status") == "satisfied" && boolField(data, "blocking") && stringField(data, "satisfaction_evidence") == "" && len(normalizeList(data["satisfaction_evidence_refs"])) == 0 {
		*errors = append(*errors, issue("GATE_SATISFACTION_EVIDENCE_MISSING", "satisfied blocking gate requires satisfaction_evidence or satisfaction_evidence_refs", where, "", nil))
	}
	if stringField(data, "status") == "waived" && (stringField(data, "waived_by") == "" || stringField(data, "waived_at") == "" || stringField(data, "waive_reason") == "") {
		*errors = append(*errors, issue("GATE_WAIVE_METADATA_MISSING", "waived gate requires waived_by, waived_at, and waive_reason", where, "", nil))
	}
	if (stringField(data, "gate_kind") == "auth" || stringField(data, "gate_kind") == "env") && !strings.Contains(strings.ToLower(note.Body), "secret policy") {
		*warnings = append(*warnings, issue("GATE_SECRET_POLICY_MISSING", "auth/env gate should include secret policy", where, "", nil))
	}
	if stringField(data, "gate_kind") == "external_service" && !v7GateBodyHasExternalServiceSetup(note.Body) {
		*warnings = append(*warnings, issue("GATE_EXTERNAL_SERVICE_SETUP_MISSING", "external_service gate should include an official documentation link or setup notes", where, "", nil))
	}
	if stringField(data, "gate_kind") == "verification" && !v7GateBodyHasExactVerificationProof(note.Body) {
		*warnings = append(*warnings, issue("GATE_VERIFICATION_PROOF_VAGUE", "verification gate should include an exact command or manual proof", where, "", nil))
	}
}

func v7GateOwnerNeedsAgentBoundary(owner string) bool {
	switch v7ProofOwnerClass(owner) {
	case "human", "external":
		return true
	default:
		return false
	}
}

func v7GateHasAgentBoundary(note Note) bool {
	return v7GateBoundaryText(note) != ""
}

func v7GateBoundaryText(note Note) string {
	if value := strings.TrimSpace(stringField(note.Data, "why_agent_cannot")); value != "" {
		return value
	}
	return strings.TrimSpace(sectionContent(note.Body, "## Why agent cannot do this"))
}

func v7GateHasSuggestion(note Note) bool {
	return v7GateSuggestionText(note) != ""
}

func v7GateSuggestionText(note Note) string {
	for _, field := range []string{"suggestion", "recommendation"} {
		if value := strings.TrimSpace(stringField(note.Data, field)); value != "" {
			return value
		}
	}
	for _, heading := range []string{"## Suggested resolution", "## Suggestion", "## Recommendation"} {
		if value := strings.TrimSpace(sectionContent(note.Body, heading)); value != "" {
			return value
		}
	}
	return ""
}

func v7GateTextIsPlaceholder(text string) bool {
	normalized := strings.ToLower(strings.TrimSpace(text))
	normalized = strings.Trim(normalized, ".:;! ")
	normalized = strings.Join(strings.Fields(normalized), " ")
	switch normalized {
	case "",
		"tbd",
		"todo",
		"resolve gate",
		"resolve this gate",
		"resolve this gate so blocked work can proceed",
		"complete the gate action",
		"capture the required verification",
		"owner confirms",
		"owner confirms the gate is satisfied":
		return true
	default:
		return false
	}
}

func v7HumanGateOwnsAgentCapableWork(gateKind, owner, action, verification, whyAgentCannot, suggestion string) bool {
	if !v7GateOwnerNeedsAgentBoundary(owner) {
		return false
	}
	text := strings.ToLower(strings.Join([]string{action, verification, whyAgentCannot, suggestion}, " "))
	if gateKind == "decision" && strings.TrimSpace(suggestion) != "" && v7GateHasDecisionConflictContext(text) {
		return false
	}
	if gateKind == "signoff" || gateKind == "security" || gateKind == "release" {
		return false
	}
	humanOnly := []string{
		"credential", "secret", "oauth", "api key", "payment", "billing", "account",
		"device", "physical", "manual smoke", "browser ui", "production access",
		"security approval", "release approval", "product decision", "legal",
	}
	for _, marker := range humanOnly {
		if strings.Contains(text, marker) {
			return false
		}
	}
	agentCapable := []string{
		"code review", "review code", "reviews code", "review code changes", "review diff", "diff review", "approve diff", "compare code",
		"code comparison", "review branch proof", "review proof", "review acceptance", "review changes",
		"inspect logs", "log analysis", "test inspection", "test failure", "debug test",
		"documentation review", "implementation judgment", "audit implementation",
	}
	for _, marker := range agentCapable {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func v7GateHasDecisionConflictContext(text string) bool {
	markers := []string{
		"spec", "requirement", "acceptance", "product intent", "product decision",
		"api", "contract", "frontend", "backend", "schema", "ux", "usability",
		"conflict", "contradict", "mismatch", "incompatible", "unclear", "choose",
	}
	for _, marker := range markers {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func v7GateBodyHasExternalServiceSetup(body string) bool {
	lower := strings.ToLower(body)
	return strings.Contains(lower, "https://") ||
		strings.Contains(lower, "http://") ||
		sectionHasSubstance(body, "## Setup notes") ||
		sectionHasSubstance(body, "## Official documentation")
}

func v7GateBodyHasExactVerificationProof(body string) bool {
	return v7TextHasExactVerificationProof(body)
}

func v7TextHasExactVerificationProof(text string) bool {
	lower := strings.ToLower(text)
	if strings.Contains(text, "```") ||
		strings.Contains(lower, "command:") ||
		strings.Contains(lower, "manual proof") ||
		strings.Contains(lower, "proof:") {
		return true
	}
	for _, row := range parseV7VerificationRows("## Verification\n\n" + text) {
		if v7VerificationCheckLooksExact(row.Check) {
			return true
		}
	}
	return false
}

func v7VerificationCheckLooksExact(check string) bool {
	lower := strings.ToLower(strings.TrimSpace(check))
	if lower == "" || lower == "-" {
		return false
	}
	for _, marker := range []string{"command:", "manual proof", "proof:"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	for _, prefix := range []string{
		"rtk ",
		"go test",
		"go run",
		"make ",
		"npm ",
		"pnpm ",
		"yarn ",
		"npx ",
		"node ",
		"python ",
		"uv ",
		"pytest",
		"cargo ",
		"docker ",
		"kubectl ",
		"curl ",
		"git ",
		"tusker ",
	} {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}

const v7LargeEvidenceWarnBytes = 10 * 1024 * 1024

func validateV7Evidence(note Note, ctx validationContext, where string, errors, warnings *[]Issue) {
	data := note.Data
	id := stringField(data, "id")
	for _, field := range []string{"schema", "kind", "id", "project", "task", "evidence_kind", "status", "created_by", "created_at"} {
		if stringField(data, field) == "" {
			*errors = append(*errors, issue(errorMissingField, fmt.Sprintf(`missing required frontmatter "%s"`, field), where, "", map[string]any{"field": field}))
		}
	}
	if stringField(data, "schema") != "tusker.evidence/v1" {
		*errors = append(*errors, issue(errorInvalidField, "V7 evidence schema must be tusker.evidence/v1", where, "", map[string]any{"field": "schema"}))
	}
	if stringField(data, "kind") != "evidence" {
		*errors = append(*errors, issue(errorInvalidField, "V7 evidence kind must be evidence", where, "", map[string]any{"field": "kind"}))
	}
	if !v7EvidenceIDPattern.MatchString(id) {
		*errors = append(*errors, issue(errorIDScheme, "V7 evidence id must match ABC-T-0001-E-0001", where, "", nil))
	}
	task := stringField(data, "task")
	if task != "" && !strings.HasSuffix(filepath.ToSlash(where), "evidence/"+task+"/"+id+".md") {
		*errors = append(*errors, issue(errorPathMismatch, "V7 evidence path must be .tusker/evidence/<task>/<evidence>.md", where, "", nil))
	}
	if _, ok := v7EvidenceKinds[stringField(data, "evidence_kind")]; !ok {
		*errors = append(*errors, issue(errorInvalidField, "invalid V7 evidence kind: "+stringField(data, "evidence_kind"), where, "", nil))
	}
	if _, ok := v7EvidenceStatus[stringField(data, "status")]; !ok {
		*errors = append(*errors, issue(errorInvalidField, "invalid V7 evidence status: "+stringField(data, "status"), where, "", map[string]any{"status": stringField(data, "status")}))
	}
	if len(normalizeV7Covers(normalizeList(data["covers"]))) == 0 {
		*errors = append(*errors, issue("EVIDENCE_COVERS_MISSING", "V7 evidence requires covers entries such as TASK:A1", where, "pass --covers A1 or --covers TASK:A1 when adding evidence", nil))
	}
	for _, cover := range normalizeList(data["covers"]) {
		if v7NormalizeCover(cover) == "" {
			*errors = append(*errors, issue("EVIDENCE_COVER_INVALID", "V7 evidence cover must look like A1 or TASK:A1: "+cover, where, "use the acceptance ID from the task acceptance table", map[string]any{"cover": cover}))
		}
	}
	if stringField(data, "evidence_kind") == "screenshot" && stringField(data, "status") == "accepted" {
		if stringField(data, "screenshot_checked_by") == "" || stringField(data, "screenshot_checked_at") == "" {
			*errors = append(*errors, issue(errorScreenshotCheckMissing, "accepted screenshot evidence requires screenshot_checked_by and screenshot_checked_at", where, "check/redact screenshots before accepting them as evidence", map[string]any{"fields": []string{"screenshot_checked_by", "screenshot_checked_at"}}))
		}
	}
	if stringField(data, "status") == "accepted" && v7EvidenceRequiresReviewerAcceptance(stringField(data, "evidence_kind")) {
		if stringField(data, "accepted_by") == "" || stringField(data, "accepted_at") == "" {
			*errors = append(*errors, issue("EVIDENCE_ACCEPTANCE_METADATA_MISSING", "accepted "+stringField(data, "evidence_kind")+" evidence requires accepted_by and accepted_at", where, "", nil))
		} else if !v7EvidenceAcceptorAllowed(stringField(data, "accepted_by")) {
			*errors = append(*errors, issue("EVIDENCE_ACCEPTOR_INVALID", "accepted "+stringField(data, "evidence_kind")+" evidence requires human or reviewer acceptor", where, "", map[string]any{"accepted_by": stringField(data, "accepted_by")}))
		}
	}
	rawArtifactsAllowed := false
	rawArtifactsReason := ""
	if task != "" {
		if taskNote, err := resolveV7Note(ctx.VaultPath, task, "task"); err == nil {
			rawArtifactsAllowed = boolField(taskNote.Data, "raw_artifacts_allowed")
			rawArtifactsReason = stringField(taskNote.Data, "raw_artifacts_reason")
		}
	}
	for _, artifact := range normalizeList(data["artifact_paths"]) {
		if v7ArtifactPathExternal(artifact) {
			continue
		}
		if v7ForbiddenEvidenceArtifactPath(artifact) && (!rawArtifactsAllowed || strings.TrimSpace(rawArtifactsReason) == "") {
			*errors = append(*errors, issue("EVIDENCE_ARTIFACT_FORBIDDEN", "source/project/archive files are not valid canonical evidence: "+artifact, where, "point reviewers at the Git diff; use .tusker/scratch/<task>/ for raw debug files or set raw_artifacts_allowed with a reason for explicit exceptions", map[string]any{"artifact": artifact}))
			continue
		}
		if stringField(data, "status") == "accepted" && !v7ArtifactPathDurable(task, artifact) {
			*errors = append(*errors, issue("EVIDENCE_ARTIFACT_NON_DURABLE", "accepted evidence artifact is not durable: "+artifact, where, "copy artifacts into .tusker/evidence/<task>/artifacts/ or mark intentional external links with --external-url", map[string]any{"artifact": artifact}))
			continue
		}
		path, ok := resolveV7ArtifactPath(ctx.VaultPath, where, note.AbsolutePath, artifact)
		if !ok {
			continue
		}
		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			continue
		}
		if info.Size() > v7LargeEvidenceWarnBytes {
			*warnings = append(*warnings, issue("EVIDENCE_ARTIFACT_LARGE", "large evidence artifact should be Git LFS-backed or external", where, "", map[string]any{"artifact": artifact, "bytes": info.Size(), "threshold_bytes": v7LargeEvidenceWarnBytes}))
		}
	}
}

func v7ArtifactPathExternal(path string) bool {
	path = strings.TrimSpace(strings.ToLower(path))
	return path == "" ||
		strings.Contains(path, "://") ||
		strings.HasPrefix(path, "external:") ||
		strings.HasPrefix(path, "lfs:")
}

func v7ArtifactPathDurable(taskID, path string) bool {
	path = strings.TrimSpace(path)
	if path == "" || v7ArtifactPathExternal(path) {
		return true
	}
	if strings.HasPrefix(strings.ToLower(path), "link-only:") {
		return false
	}
	if filepath.IsAbs(path) {
		return false
	}
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(path)))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, "/tmp/") {
		return false
	}
	if taskID == "" {
		return true
	}
	prefix := "evidence/" + taskID + "/"
	return clean == filepath.Base(clean) || strings.HasPrefix(clean, prefix)
}

func resolveV7ArtifactPath(vaultPath, where, absoluteNotePath, artifact string) (string, bool) {
	artifact = strings.TrimSpace(artifact)
	if artifact == "" {
		return "", false
	}
	if filepath.IsAbs(artifact) && fileExists(artifact) {
		return artifact, true
	}
	var candidates []string
	if vaultPath != "" {
		candidates = append(candidates, filepath.Join(vaultPath, filepath.FromSlash(artifact)))
		if where != "" && where != "<unknown>" {
			candidates = append(candidates, filepath.Join(vaultPath, filepath.Dir(filepath.FromSlash(where)), filepath.FromSlash(artifact)))
		}
	}
	if absoluteNotePath != "" {
		candidates = append(candidates, filepath.Join(filepath.Dir(absoluteNotePath), filepath.FromSlash(artifact)))
	}
	for _, candidate := range candidates {
		if fileExists(candidate) {
			return candidate, true
		}
	}
	return "", false
}

func validateV7Attempt(note Note, where string, errors, warnings *[]Issue) {
	data := note.Data
	id := stringField(data, "id")
	for _, field := range []string{"schema", "kind", "id", "project", "task", "runner", "workspace_kind", "status", "started_at"} {
		if stringField(data, field) == "" {
			*errors = append(*errors, issue(errorMissingField, fmt.Sprintf(`missing required frontmatter "%s"`, field), where, "", nil))
		}
	}
	if stringField(data, "schema") != "tusker.attempt/v1" {
		*errors = append(*errors, issue(errorInvalidField, "V7 attempt schema must be tusker.attempt/v1", where, "", map[string]any{"field": "schema"}))
	}
	if stringField(data, "kind") != "attempt" {
		*errors = append(*errors, issue(errorInvalidField, "V7 attempt kind must be attempt", where, "", map[string]any{"field": "kind"}))
	}
	if !v7AttemptIDPattern.MatchString(id) {
		*errors = append(*errors, issue(errorIDScheme, "V7 attempt id must match ABC-T-0001-A-0001", where, "", nil))
	}
	task := stringField(data, "task")
	if task != "" && id != "" && !strings.HasSuffix(filepath.ToSlash(where), "attempts/"+task+"/"+id+".md") {
		*errors = append(*errors, issue(errorPathMismatch, "V7 attempt path must be .tusker/attempts/<task>/<attempt>.md", where, "", nil))
	}
	if _, ok := v7AttemptStatus[stringField(data, "status")]; !ok {
		*errors = append(*errors, issue(errorInvalidField, "invalid V7 attempt status: "+stringField(data, "status"), where, "", nil))
	}
}

func validateV7Decision(note Note, where string, errors, warnings *[]Issue) {
	data := note.Data
	id := stringField(data, "id")
	for _, field := range []string{"schema", "kind", "id", "project", "epic", "title", "status"} {
		if stringField(data, field) == "" {
			*errors = append(*errors, issue(errorMissingField, fmt.Sprintf(`missing required frontmatter "%s"`, field), where, "", nil))
		}
	}
	if stringField(data, "schema") != "tusker.decision/v1" {
		*errors = append(*errors, issue(errorInvalidField, "V7 decision schema must be tusker.decision/v1", where, "", map[string]any{"field": "schema"}))
	}
	if stringField(data, "kind") != "decision" {
		*errors = append(*errors, issue(errorInvalidField, "V7 decision kind must be decision", where, "", map[string]any{"field": "kind"}))
	}
	if !v7DecisionIDPattern.MatchString(id) {
		*errors = append(*errors, issue(errorIDScheme, "V7 decision id must match ABC-D-0001", where, "", nil))
	}
	if id != "" && !strings.HasSuffix(filepath.ToSlash(where), "work/decisions/"+id+".md") {
		*errors = append(*errors, issue(errorPathMismatch, "V7 decision path must be .tusker/work/decisions/"+id+".md", where, "", nil))
	}
	if _, ok := v7DecisionStatus[stringField(data, "status")]; !ok {
		*errors = append(*errors, issue(errorInvalidField, "invalid V7 decision status: "+stringField(data, "status"), where, "", nil))
	}
	if stringField(data, "status") == "accepted" && (stringField(data, "decided_by") == "" || stringField(data, "decided_at") == "") {
		*errors = append(*errors, issue("DECISION_ACCEPTED_METADATA_MISSING", "accepted decision requires decided_by and decided_at", where, "", nil))
	}
}

func validateV7Epic(note Note, where string, errors, warnings *[]Issue) {
	data := note.Data
	id := stringField(data, "id")
	for _, field := range []string{"schema", "kind", "id", "project", "title", "status", "owner", "priority", "created_at", "updated_at", "state_rev"} {
		if stringField(data, field) == "" {
			*errors = append(*errors, issue(errorMissingField, fmt.Sprintf(`missing required frontmatter "%s"`, field), where, "", nil))
		}
	}
	if stringField(data, "schema") != "tusker.epic/v7" {
		*errors = append(*errors, issue(errorInvalidField, "V7 epic schema must be tusker.epic/v7", where, "", map[string]any{"field": "schema"}))
	}
	if stringField(data, "kind") != "epic" {
		*errors = append(*errors, issue(errorInvalidField, "V7 epic kind must be epic", where, "", map[string]any{"field": "kind"}))
	}
	if id != "" && !epicAcronymPattern.MatchString(id) {
		*errors = append(*errors, issue(errorIDScheme, "V7 epic id must match ABC", where, "", map[string]any{"id": id}))
	}
	if id != "" && !strings.HasSuffix(filepath.ToSlash(where), "work/epics/"+id+".md") {
		*errors = append(*errors, issue(errorPathMismatch, "V7 epic path must be .tusker/work/epics/"+id+".md", where, "", nil))
	}
}

func validateV7Proposal(note Note, ctx validationContext, where string, errors, warnings *[]Issue) {
	data := note.Data
	id := stringField(data, "id")
	for _, field := range []string{"schema", "kind", "id", "project", "title", "status", "action", "target_kind", "target", "proposed_by", "created_at"} {
		if stringField(data, field) == "" {
			*errors = append(*errors, issue(errorMissingField, fmt.Sprintf(`missing required frontmatter "%s"`, field), where, "", map[string]any{"field": field}))
		}
	}
	if stringField(data, "schema") != "tusker.proposal/v1" {
		*errors = append(*errors, issue(errorInvalidField, "V7 proposal schema must be tusker.proposal/v1", where, "", map[string]any{"field": "schema"}))
	}
	if stringField(data, "kind") != "proposal" {
		*errors = append(*errors, issue(errorInvalidField, "V7 proposal kind must be proposal", where, "", map[string]any{"field": "kind"}))
	}
	if !v7ProposalIDPattern.MatchString(id) {
		*errors = append(*errors, issue(errorIDScheme, "V7 proposal id must match ABC-P-0001", where, "", map[string]any{"id": id}))
	}
	if !strings.HasSuffix(filepath.ToSlash(where), "work/inbox/"+id+".md") {
		*errors = append(*errors, issue(errorPathMismatch, "V7 proposal path must be .tusker/work/inbox/"+id+".md", where, "", nil))
	}
	if _, ok := v7ProposalStatus[stringField(data, "status")]; !ok {
		*errors = append(*errors, issue(errorInvalidField, "invalid V7 proposal status: "+stringField(data, "status"), where, "", map[string]any{"field": "status"}))
	}
	action := stringField(data, "action")
	if _, ok := v7ProposalAction[action]; !ok {
		*errors = append(*errors, issue(errorInvalidField, "invalid V7 proposal action: "+action, where, "", map[string]any{"field": "action"}))
	}
	target := stringField(data, "target")
	switch stringField(data, "target_kind") {
	case "task":
		if !v7TaskIDPattern.MatchString(target) {
			*errors = append(*errors, issue(errorIDScheme, "task proposal target must match ABC-T-0001", where, "", map[string]any{"target": target}))
		}
	case "gate":
		if !v7GateIDPattern.MatchString(target) {
			*errors = append(*errors, issue(errorIDScheme, "gate proposal target must match ABC-G-0001", where, "", map[string]any{"target": target}))
		}
	case "decision":
		if !v7DecisionIDPattern.MatchString(target) {
			*errors = append(*errors, issue(errorIDScheme, "decision proposal target must match ABC-D-0001", where, "", map[string]any{"target": target}))
		}
	case "epic":
		if !epicAcronymPattern.MatchString(target) {
			*errors = append(*errors, issue(errorIDScheme, "epic proposal target must match ABC", where, "", map[string]any{"target": target}))
		}
	case "object":
	default:
		*errors = append(*errors, issue(errorInvalidField, "invalid V7 proposal target_kind: "+stringField(data, "target_kind"), where, "", map[string]any{"field": "target_kind"}))
	}
	if v7ProposalFieldsEmpty(data["proposed_fields"]) {
		*errors = append(*errors, issue(errorMissingField, "proposal requires proposed_fields", where, "", map[string]any{"field": "proposed_fields"}))
	}
	if stringField(data, "status") != "" && stringField(data, "status") != "proposed" {
		if stringField(data, "reviewed_by") == "" || stringField(data, "reviewed_at") == "" {
			*errors = append(*errors, issue("PROPOSAL_REVIEW_METADATA_MISSING", "reviewed proposal requires reviewed_by and reviewed_at", where, "", map[string]any{"fields": []string{"reviewed_by", "reviewed_at"}}))
		}
		if stringField(data, "status") == "rejected" && stringField(data, "review_reason") == "" {
			*errors = append(*errors, issue("PROPOSAL_REJECT_REASON_MISSING", "rejected proposal requires review_reason", where, "", map[string]any{"field": "review_reason"}))
		}
	}
	if !sectionHasSubstance(note.Body, "## Rationale") {
		*warnings = append(*warnings, issue("PROPOSAL_RATIONALE_MISSING", "proposal should include rationale for maintainer review", where, "", nil))
	}
}

func validateV7Closeout(note Note, ctx validationContext, where string, errors *[]Issue) {
	data := note.Data
	for _, field := range []string{"schema", "kind", "id", "project", "task", "state", "agent_action", "state_fingerprint", "created_by", "created_at"} {
		if stringField(data, field) == "" {
			*errors = append(*errors, issue(errorMissingField, fmt.Sprintf(`missing required frontmatter "%s"`, field), where, "", map[string]any{"field": field}))
		}
	}
	if stringField(data, "schema") != "tusker.closeout/v1" {
		*errors = append(*errors, issue(errorInvalidField, "V7 closeout schema must be tusker.closeout/v1", where, "", map[string]any{"field": "schema"}))
	}
	if stringField(data, "kind") != "closeout" {
		*errors = append(*errors, issue(errorInvalidField, "V7 closeout kind must be closeout", where, "", map[string]any{"field": "kind"}))
	}
	id := stringField(data, "id")
	taskID := stringField(data, "task")
	if taskID == "" || !v7TaskIDPattern.MatchString(taskID) {
		*errors = append(*errors, issue(errorIDScheme, "V7 closeout task must match ABC-T-0001", where, "", map[string]any{"task": taskID}))
	}
	expectedPrefix := taskID + "-C-"
	if id == "" || !strings.HasPrefix(id, expectedPrefix) {
		*errors = append(*errors, issue(errorIDScheme, "V7 closeout id must look like "+expectedPrefix+"0001", where, "", map[string]any{"id": id}))
	}
	if !strings.HasSuffix(filepath.ToSlash(where), "work/closeouts/"+id+".md") {
		*errors = append(*errors, issue(errorPathMismatch, "V7 closeout path must be .tusker/work/closeouts/"+id+".md", where, "", nil))
	}
	if stringField(data, "state") != "machine_complete_waiting_for_human" {
		*errors = append(*errors, issue(errorInvalidField, "V7 closeout state must be machine_complete_waiting_for_human", where, "", map[string]any{"field": "state"}))
	}
	if stringField(data, "agent_action") != "stop_until_human_response" {
		*errors = append(*errors, issue(errorInvalidField, "V7 closeout agent_action must be stop_until_human_response", where, "", map[string]any{"field": "agent_action"}))
	}
	if !v7CloseoutValidationClean(note) {
		*errors = append(*errors, issue("CLOSEOUT_VALIDATION_NOT_CLEAN", "V7 closeout requires a passing validation command snapshot", where, "rerun closeout with --validate \"<command>\"", nil))
	}
	if !v7CloseoutPacketExists(ctx.VaultPath, note) {
		*errors = append(*errors, issue("CLOSEOUT_PACKET_MISSING", "V7 closeout requires an existing review packet", where, "rerun closeout with --emit-packet", map[string]any{"review_packet": v7CloseoutPacketPaths(note)}))
	}
	if ctx.VaultPath == "" || taskID == "" {
		return
	}
	idx, err := loadV7Index(ctx.VaultPath)
	if err != nil {
		*errors = append(*errors, issue("CLOSEOUT_INDEX_LOAD_FAILED", "could not load V7 index to validate closeout: "+err.Error(), where, "", nil))
		return
	}
	task, ok := idx.Tasks[taskID]
	if !ok {
		*errors = append(*errors, issue(errorNotFound, "V7 closeout references missing task "+taskID, where, "", map[string]any{"task": taskID}))
		return
	}
	if latest, ok := latestV7Closeout(idx, taskID); ok && stringField(latest.Data, "id") != stringField(data, "id") {
		return
	}
	report := computeV7ProofReport(ctx.VaultPath, task, idx)
	report, terminalWait := v7CloseoutTerminalReport(ctx.VaultPath, task, report)
	if !terminalWait || !v7ProofReportMachineComplete(report) {
		*errors = append(*errors, issue("CLOSEOUT_NOT_TERMINAL_HUMAN_WAIT", "V7 closeout no longer matches a machine-complete human-wait state", where, "remove stale closeout or rerun closeout after current machine work is complete", map[string]any{
			"machine_missing":       report.MachineMissing,
			"open_machine_gates":    report.OpenMachineGates,
			"reviewer_missing":      report.ReviewerMissing,
			"open_reviewer_gates":   report.OpenReviewerGates,
			"external_missing":      report.ExternalMissing,
			"open_external_gates":   report.OpenExternalGates,
			"human_missing":         report.HumanMissing,
			"open_human_gates":      report.OpenHumanGates,
			"close_policy_human":    v7ClosePolicyHumanWait(ctx.VaultPath, task, report),
			"closeout_agent_action": stringField(data, "agent_action"),
		}))
	}
	expected := v7CloseoutFingerprint(ctx.VaultPath, task, idx, note)
	if stringField(data, "state_fingerprint") != expected {
		*errors = append(*errors, issue("CLOSEOUT_FINGERPRINT_STALE", "V7 closeout state_fingerprint does not match current task/proof/gate/repo state", where, "remove stale closeout or rerun closeout for the current state", map[string]any{"expected": expected, "actual": stringField(data, "state_fingerprint")}))
	}
}

func validateV7Domain(note Note, ctx validationContext, where string, errors, warnings *[]Issue) {
	data := note.Data
	id := stringField(data, "id")
	for _, field := range []string{"schema", "kind", "id", "project", "title", "status", "summary", "source_of_truth", "canonical_files", "created_at", "updated_at", "state_rev"} {
		if stringField(data, field) == "" {
			*errors = append(*errors, issue(errorMissingField, fmt.Sprintf(`missing required frontmatter "%s"`, field), where, "", map[string]any{"field": field}))
		}
	}
	if stringField(data, "schema") != "tusker.domain/v7" {
		*errors = append(*errors, issue(errorInvalidField, "V7 domain schema must be tusker.domain/v7", where, "", map[string]any{"field": "schema"}))
	}
	if stringField(data, "kind") != "domain" {
		*errors = append(*errors, issue(errorInvalidField, "V7 domain kind must be domain", where, "", map[string]any{"field": "kind"}))
	}
	validateV7DomainID(id, where, errors)
	if id != "" && !strings.HasSuffix(filepath.ToSlash(where), "knowledge/domains/"+id+"/INDEX.md") {
		*errors = append(*errors, issue(errorPathMismatch, "V7 domain path must be .tusker/knowledge/domains/"+id+"/INDEX.md", where, "", nil))
	}
	if _, ok := v7DomainStatus[stringField(data, "status")]; !ok {
		*errors = append(*errors, issue(errorInvalidField, "invalid V7 domain status: "+stringField(data, "status"), where, "", map[string]any{"field": "status"}))
	}
	if len(normalizeList(data["source_of_truth"])) == 0 {
		*errors = append(*errors, issue(errorMissingField, "V7 domain requires source_of_truth entries", where, "", map[string]any{"field": "source_of_truth"}))
	}
	canonicalFiles := normalizeList(data["canonical_files"])
	if !containsString(canonicalFiles, "INDEX.md") || !containsString(canonicalFiles, "CANON.md") {
		*errors = append(*errors, issue(errorInvalidField, "V7 domain canonical_files must include INDEX.md and CANON.md", where, "", map[string]any{"field": "canonical_files"}))
	}
	if ctx.VaultPath != "" && id != "" && !fileExists(filepath.Join(ctx.VaultPath, "knowledge", "domains", filepath.FromSlash(id), "CANON.md")) {
		*errors = append(*errors, issue(errorPathMismatch, "V7 domain requires sibling CANON.md", where, "", nil))
	}
	validateV7DomainLayout(ctx.VaultPath, id, where, errors)
}

func validateV7DomainCanon(note Note, ctx validationContext, where string, errors, warnings *[]Issue) {
	data := note.Data
	domain := stringField(data, "domain")
	id := stringField(data, "id")
	for _, field := range []string{"schema", "kind", "id", "project", "domain", "title", "status", "summary", "source_of_truth", "created_at", "updated_at", "state_rev"} {
		if stringField(data, field) == "" {
			*errors = append(*errors, issue(errorMissingField, fmt.Sprintf(`missing required frontmatter "%s"`, field), where, "", map[string]any{"field": field}))
		}
	}
	if stringField(data, "schema") != "tusker.domain-canon/v7" {
		*errors = append(*errors, issue(errorInvalidField, "V7 domain canon schema must be tusker.domain-canon/v7", where, "", map[string]any{"field": "schema"}))
	}
	if stringField(data, "kind") != "domain_canon" {
		*errors = append(*errors, issue(errorInvalidField, "V7 domain canon kind must be domain_canon", where, "", map[string]any{"field": "kind"}))
	}
	validateV7DomainID(domain, where, errors)
	if id != "" && domain != "" && id != domain+"/canon" {
		*errors = append(*errors, issue(errorIDScheme, "V7 domain canon id must be <domain>/canon", where, "", map[string]any{"id": id, "domain": domain}))
	}
	if domain != "" && !strings.HasSuffix(filepath.ToSlash(where), "knowledge/domains/"+domain+"/CANON.md") {
		*errors = append(*errors, issue(errorPathMismatch, "V7 domain canon path must be .tusker/knowledge/domains/"+domain+"/CANON.md", where, "", nil))
	}
	if _, ok := v7DomainStatus[stringField(data, "status")]; !ok {
		*errors = append(*errors, issue(errorInvalidField, "invalid V7 domain canon status: "+stringField(data, "status"), where, "", map[string]any{"field": "status"}))
	}
	if len(normalizeList(data["source_of_truth"])) == 0 {
		*errors = append(*errors, issue(errorMissingField, "V7 domain canon requires source_of_truth entries", where, "", map[string]any{"field": "source_of_truth"}))
	}
	if ctx.VaultPath != "" && domain != "" && !fileExists(filepath.Join(ctx.VaultPath, "knowledge", "domains", filepath.FromSlash(domain), "INDEX.md")) {
		*errors = append(*errors, issue(errorPathMismatch, "V7 domain canon requires sibling INDEX.md", where, "", nil))
	}
}

func validateV7DomainLayout(vaultPath, id, where string, errors *[]Issue) {
	if vaultPath == "" || id == "" {
		return
	}
	domainDir := filepath.Join(vaultPath, "knowledge", "domains", filepath.FromSlash(id))
	for _, rel := range []string{"runbooks", "decisions", "interfaces", "invariants", "sources", "glossary"} {
		if !fileExists(filepath.Join(domainDir, rel)) {
			*errors = append(*errors, issue(errorPathMismatch, "V7 domain layout requires "+rel+"/", where, "run `tusker domain new "+id+" --v7` or refresh the V7 profile", map[string]any{"path": filepath.ToSlash(filepath.Join("knowledge", "domains", id, rel))}))
		}
	}
	if !fileExists(filepath.Join(domainDir, "glossary.md")) {
		*errors = append(*errors, issue(errorPathMismatch, "V7 domain layout requires glossary.md", where, "regenerate the V7 domain layout", map[string]any{"path": filepath.ToSlash(filepath.Join("knowledge", "domains", id, "glossary.md"))}))
	}
	if fileExists(filepath.Join(domainDir, "references")) {
		*errors = append(*errors, issue("V7_REFERENCES_DIR_FORBIDDEN", "V7 canonical knowledge uses sources/ for raw external input, not references/", where, "move raw external material to knowledge/domains/"+id+"/sources/", map[string]any{"path": filepath.ToSlash(filepath.Join("knowledge", "domains", id, "references"))}))
	}
}

func validateV7ProjectSkill(note Note, ctx validationContext, where string, errors, warnings *[]Issue) {
	data := note.Data
	for _, field := range []string{"schema", "kind", "name", "project", "status", "description", "operator_skill", "source_of_truth", "canonical_files", "created_at", "updated_at", "state_rev"} {
		if stringField(data, field) == "" {
			*errors = append(*errors, issue(errorMissingField, fmt.Sprintf(`missing required frontmatter "%s"`, field), where, "", map[string]any{"field": field}))
		}
	}
	if stringField(data, "schema") != "tusker.project-skill/v7" {
		*errors = append(*errors, issue(errorInvalidField, "V7 project skill schema must be tusker.project-skill/v7", where, "", map[string]any{"field": "schema"}))
	}
	if stringField(data, "kind") != "project_skill" {
		*errors = append(*errors, issue(errorInvalidField, "V7 project skill kind must be project_skill", where, "", map[string]any{"field": "kind"}))
	}
	if note.RelativePath != "" && note.RelativePath != "SKILL.md" {
		*errors = append(*errors, issue(errorPathMismatch, "V7 project skill must live at "+vaultDisplayPath(ctx.VaultPath, "SKILL.md"), where, "", map[string]any{"expected": "SKILL.md", "actual": note.RelativePath}))
	}
	if stringField(data, "operator_skill") != "tusker" {
		*errors = append(*errors, issue(errorInvalidField, "V7 project skill must name the Tusker operator skill as operator_skill: tusker", where, "", map[string]any{"field": "operator_skill"}))
	}
	for _, field := range []string{"source_of_truth", "canonical_files"} {
		for _, rel := range normalizeList(data[field]) {
			if v7ProjectSkillForbiddenSourcePath(rel) {
				*errors = append(*errors, issue("PROJECT_SKILL_FORBIDDEN_SOURCE", "V7 project skill must not use task/evidence/generated/runtime material as source truth: "+rel, where, "keep project skill source truth under knowledge/domains/**", map[string]any{"field": field, "path": rel}))
			}
		}
	}
	body := strings.ToLower(note.Body)
	for _, required := range []string{"project knowledge skill", "tusker operator skill", "knowledge/domains", "index.md", "canon.md"} {
		if !strings.Contains(body, required) {
			*errors = append(*errors, issue(errorMissingSection, "V7 project skill is missing routing text for "+required, where, "", map[string]any{"required_text": required}))
		}
	}
	if !strings.Contains(body, "do not publish") || !strings.Contains(body, "task records") || !strings.Contains(body, "evidence") || !strings.Contains(body, "generated") {
		*errors = append(*errors, issue("PROJECT_SKILL_BOUNDARY_MISSING", "V7 project skill must state the publication boundary for tasks, evidence, generated output, runtime state, and raw logs", where, "", nil))
	}
}

func validateV7KnowledgeNode(note Note, ctx validationContext, where string, errors, warnings *[]Issue) {
	data := note.Data
	for _, field := range []string{"schema", "kind", "id", "project", "title", "status", "summary", "source_of_truth"} {
		if stringField(data, field) == "" {
			*errors = append(*errors, issue(errorMissingField, fmt.Sprintf(`missing required frontmatter "%s"`, field), where, "", map[string]any{"field": field}))
		}
	}
	if stringField(data, "schema") != "tusker.knowledge/v7" {
		*errors = append(*errors, issue(errorInvalidField, "V7 knowledge schema must be tusker.knowledge/v7", where, "", map[string]any{"field": "schema"}))
	}
	kind := stringField(data, "kind")
	if _, ok := v7KnowledgeKinds[kind]; !ok {
		*errors = append(*errors, issue(errorInvalidField, "invalid V7 knowledge kind: "+kind, where, "", map[string]any{"field": "kind"}))
	}
	if mode := strings.ToLower(strings.TrimSpace(stringField(data, "mode"))); mode != "" {
		if _, docsMode := docModes[mode]; docsMode || mode == "dia"+"taxis" {
			*errors = append(*errors, issue("V7_DOCS_MODE_FORBIDDEN", "V7 canonical knowledge must not use docs-map publication modes: "+mode, where, "keep public docs taxonomy in site/docs commands, not canonical V7 knowledge", map[string]any{"mode": mode}))
		}
	}
	id := strings.Trim(strings.ToLower(stringField(data, "id")), "/")
	if id == "" || strings.Contains(id, "..") {
		*errors = append(*errors, issue(errorIDScheme, "V7 knowledge id must be a stable slash path", where, "", map[string]any{"id": stringField(data, "id")}))
	}
	normalized := filepath.ToSlash(where)
	if strings.Contains(normalized, "/references/") || strings.HasSuffix(normalized, "/references.md") {
		*errors = append(*errors, issue("V7_REFERENCES_PATH_FORBIDDEN", "V7 canonical knowledge uses sources/ for raw external input, not references/", where, "move raw external material under sources/", nil))
	}
	if ctx.VaultPath != "" && id != "" {
		if !strings.HasPrefix(normalized, "knowledge/domains/") {
			*warnings = append(*warnings, issue(errorPathMismatch, "V7 knowledge nodes should live under .tusker/knowledge/domains/**", where, "move durable project knowledge under the owning domain folder", nil))
		}
	}
	validateV7KnowledgeNodePath(kind, id, normalized, where, errors)
	validateV7Body(note.Body, ctx.VaultPath, where, errors, warnings)
}

func validateV7KnowledgeNodePath(kind, id, normalized, where string, errors *[]Issue) {
	if kind == "" || normalized == "" || !strings.HasPrefix(normalized, "knowledge/domains/") {
		return
	}
	parts := strings.Split(strings.TrimSuffix(normalized, ".md"), "/")
	if len(parts) < 5 {
		*errors = append(*errors, issue(errorPathMismatch, "V7 knowledge leaf path must be knowledge/domains/<domain>/<folder>/<slug>.md", where, "", nil))
		return
	}
	folder := parts[3]
	expected := v7KnowledgeFolderForKind(kind)
	if expected == "" {
		return
	}
	if folder != expected {
		*errors = append(*errors, issue(errorPathMismatch, fmt.Sprintf("V7 %s nodes must live under %s/", kind, expected), where, "", map[string]any{"kind": kind, "folder": folder, "expected_folder": expected}))
	}
	if id != "" && strings.TrimSuffix(normalized, ".md") != filepath.ToSlash(filepath.Join("knowledge", "domains", filepath.FromSlash(id))) {
		*errors = append(*errors, issue(errorIDScheme, "V7 knowledge id must mirror path under knowledge/domains", where, "", map[string]any{"id": id, "path": normalized}))
	}
}

func v7ProjectSkillForbiddenSourcePath(rel string) bool {
	normalized := strings.Trim(strings.ToLower(filepath.ToSlash(rel)), "/")
	if normalized == "" {
		return false
	}
	for _, prefix := range []string{
		"work/", "epics/", "evidence/", "attempts/", "events/", "attachments/",
		"_generated/", "_system/", ".tusker-local/", ".tusker-runtime/",
		"dashboards/", "raw-", "logs/",
	} {
		if normalized == strings.TrimSuffix(prefix, "/") || strings.HasPrefix(normalized, prefix) {
			return true
		}
	}
	return strings.HasSuffix(normalized, ".log") || strings.Contains(normalized, "/raw-")
}

func validateV7SkillKnowledge(vaultPath string) ([]Issue, []Issue) {
	if !hasV7ProjectSkill(vaultPath) && !hasV7KnowledgeDomains(vaultPath) {
		return nil, nil
	}
	var errors []Issue
	skillPath := filepath.Join(vaultPath, "SKILL.md")
	if !fileExists(skillPath) {
		errors = append(errors, issue(errorMissingField, "V7 knowledge domains require "+vaultDisplayPath(vaultPath, "SKILL.md")+" project knowledge skill", "SKILL.md", "run `tusker init --profile v7` or `tusker publish skill --v7` after adding V7 domains", nil))
	} else if hasV7KnowledgeDomains(vaultPath) && !hasV7ProjectSkill(vaultPath) {
		errors = append(errors, issue(errorInvalidField, "V7 knowledge domains require SKILL.md to use schema tusker.project-skill/v7", "SKILL.md", "keep V6 domains under .tusker/knowledge/domains/** or migrate the project skill to V7", nil))
	} else if hasV7KnowledgeDomains(vaultPath) {
		_, body, err := parseFrontmatterMustRead(skillPath)
		if err != nil {
			errors = append(errors, issue(errorInvalidField, "could not read V7 project skill: "+err.Error(), "SKILL.md", "", nil))
			return errors, nil
		}
		domains, err := listV7ProjectSkillDomains(vaultPath)
		if err != nil {
			errors = append(errors, issue(errorInvalidField, "could not list V7 project skill domains: "+err.Error(), "knowledge/domains", "", nil))
			return errors, nil
		}
		for _, domain := range domains {
			id := stringField(domain.Data, "id")
			if id == "" {
				continue
			}
			for _, rel := range []string{
				filepath.ToSlash(filepath.Join("knowledge", "domains", id, "INDEX.md")),
				filepath.ToSlash(filepath.Join("knowledge", "domains", id, "CANON.md")),
			} {
				if !strings.Contains(body, rel) {
					errors = append(errors, issue("PROJECT_SKILL_DOMAIN_ROUTE_MISSING", "V7 project skill routing omits domain route "+rel, "SKILL.md", "regenerate `"+vaultDisplayPath(vaultPath, "SKILL.md")+"` by running `tusker domain new <id> --v7` or `tusker init --profile v7`", map[string]any{"domain": id, "path": rel}))
				}
			}
		}
		domainIDs := map[string]struct{}{}
		for _, domain := range domains {
			domainIDs[stringField(domain.Data, "id")] = struct{}{}
		}
		notes, err := listAllNotes(vaultPath)
		if err == nil {
			for _, note := range notes {
				if effectiveV7Kind(note.Data) != "task" || !strings.HasSuffix(stringField(note.Data, "schema"), "/v7") {
					continue
				}
				for _, domain := range normalizeList(note.Data["domains"]) {
					if _, ok := domainIDs[domain]; !ok {
						errors = append(errors, issue("TASK_DOMAIN_ROUTE_MISSING", "V7 task declares a domain with no V7 project skill route: "+domain, note.RelativePath, "create the domain with `tusker domain new "+domain+" --v7` or remove the stale task domain", map[string]any{"task": stringField(note.Data, "id"), "domain": domain}))
					}
				}
			}
		} else {
			errors = append(errors, issue(errorInvalidField, "could not inspect V7 task domain routes: "+err.Error(), "work/tasks", "", nil))
		}
	}
	return errors, nil
}

func validateV7DomainID(id, where string, errors *[]Issue) {
	if id == "" {
		return
	}
	if err := validateKnowledgeNodePath(id); err != "" {
		*errors = append(*errors, issue(errorIDScheme, "invalid V7 domain id: "+err, where, "", map[string]any{"id": id}))
		return
	}
	if strings.Contains(id, "/") {
		*errors = append(*errors, issue(errorIDScheme, "V7 domain id must be one portable path segment", where, "", map[string]any{"id": id}))
	}
}

func v7ProposalFieldsEmpty(value any) bool {
	switch current := value.(type) {
	case nil:
		return true
	case map[string]any:
		return len(current) == 0
	case map[any]any:
		return len(current) == 0
	default:
		return strings.TrimSpace(toString(current)) == ""
	}
}

func validateV7Events(vaultPath string) ([]Issue, []Issue, int) {
	eventsRoot := filepath.Join(vaultPath, "events")
	if _, err := os.Stat(eventsRoot); os.IsNotExist(err) {
		return nil, nil, 0
	}
	var errors, warnings []Issue
	count := 0
	err := filepath.WalkDir(eventsRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			rel := v7RelativeToVault(vaultPath, path)
			errors = append(errors, issue(errorPathEscape, "could not read V7 event path: "+walkErr.Error(), rel, "", nil))
			return nil
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			return nil
		}
		count++
		rel := v7RelativeToVault(vaultPath, path)
		raw, err := readText(path)
		if err != nil {
			errors = append(errors, issue(errorInvalidField, "could not read V7 event JSON: "+err.Error(), rel, "", nil))
			return nil
		}
		var data map[string]any
		if err := json.Unmarshal([]byte(raw), &data); err != nil {
			errors = append(errors, issue(errorInvalidField, "invalid V7 event JSON: "+err.Error(), rel, "", nil))
			return nil
		}
		validateV7Event(data, raw, rel, &errors, &warnings)
		return nil
	})
	if err != nil {
		errors = append(errors, issue(errorInvalidField, "could not scan V7 events: "+err.Error(), "events", "", nil))
	}
	return errors, warnings, count
}

func v7RelativeToVault(vaultPath, path string) string {
	rel, err := filepath.Rel(vaultPath, path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(rel)
}

func validateV7Event(data map[string]any, raw, where string, errors, warnings *[]Issue) {
	for _, field := range []string{"schema", "id", "project", "object", "object_kind", "event_kind", "actor", "at"} {
		if stringField(data, field) == "" {
			*errors = append(*errors, issue(errorMissingField, fmt.Sprintf(`missing required event field "%s"`, field), where, "", map[string]any{"field": field}))
		}
	}
	if stringField(data, "schema") != "tusker.event/v1" {
		*errors = append(*errors, issue(errorInvalidField, "V7 event schema must be tusker.event/v1", where, "", map[string]any{"field": "schema"}))
	}
	if _, ok := v7EventObjectKinds[stringField(data, "object_kind")]; stringField(data, "object_kind") != "" && !ok {
		*errors = append(*errors, issue(errorInvalidField, "invalid V7 event object_kind: "+stringField(data, "object_kind"), where, "", map[string]any{"field": "object_kind"}))
	}
	if _, ok := v7EventKinds[stringField(data, "event_kind")]; stringField(data, "event_kind") != "" && !ok {
		*errors = append(*errors, issue(errorInvalidField, "invalid V7 event kind: "+stringField(data, "event_kind"), where, "", map[string]any{"field": "event_kind"}))
	}
	if at := stringField(data, "at"); at != "" {
		if _, err := time.Parse(time.RFC3339, at); err != nil {
			*errors = append(*errors, issue(errorInvalidField, "V7 event at must be RFC3339", where, "", map[string]any{"field": "at"}))
		}
	}
	validateV7EventPath(data, where, errors)
	if containsV7Secret(raw) {
		*errors = append(*errors, issue("SECRET_IN_EVENT", "V7 events must not contain obvious secrets", where, "", nil))
	}
}

func validateV7EventPath(data map[string]any, where string, errors *[]Issue) {
	parts := strings.Split(filepath.ToSlash(where), "/")
	if len(parts) != 4 || parts[0] != "events" || len(parts[1]) != 4 || len(parts[2]) != 2 || !strings.HasSuffix(parts[3], ".json") {
		*errors = append(*errors, issue(errorPathMismatch, "V7 event path must be .tusker/events/YYYY/MM/<object>--<timestamp>--<id>.json", where, "", nil))
		return
	}
	id := stringField(data, "id")
	objectID := stringField(data, "object")
	base := strings.TrimSuffix(parts[3], ".json")
	chunks := strings.Split(base, "--")
	if len(chunks) < 3 {
		*errors = append(*errors, issue(errorPathMismatch, "V7 event filename must include object, timestamp, and id", where, "", nil))
		return
	}
	fileID := chunks[len(chunks)-1]
	fileTimestamp := chunks[len(chunks)-2]
	fileObject := strings.Join(chunks[:len(chunks)-2], "--")
	if id != "" && fileID != id {
		*errors = append(*errors, issue(errorPathMismatch, "V7 event filename id must match event id", where, "", map[string]any{"id": id, "filename_id": fileID}))
	}
	if objectID != "" && fileObject != objectID {
		*errors = append(*errors, issue(errorPathMismatch, "V7 event filename object must match event object", where, "", map[string]any{"object": objectID, "filename_object": fileObject}))
	}
	if ts, err := time.Parse("20060102T150405Z", fileTimestamp); err != nil {
		*errors = append(*errors, issue(errorPathMismatch, "V7 event filename timestamp must use YYYYMMDDTHHMMSSZ", where, "", nil))
	} else if parts[1] != ts.Format("2006") || parts[2] != ts.Format("01") {
		*errors = append(*errors, issue(errorPathMismatch, "V7 event directory must match filename timestamp year/month", where, "", nil))
	}
}

func validateV7BodyBudget(note Note, vaultPath, where string, errors, warnings *[]Issue) {
	budget := v7BodyBudgetObject(note, where)
	if budget == "" {
		return
	}
	body := note.Body
	lines := strings.Split(strings.TrimRight(body, "\n"), "\n")
	warnLimit, failLimit := v7BodyLineLimitsFor(vaultPath, budget)
	codePrefix := strings.ToUpper(strings.ReplaceAll(budget, "-", "_"))
	if failLimit > 0 && len(lines) > failLimit {
		*errors = append(*errors, issue(codePrefix+"_BODY_TOO_LONG", fmt.Sprintf("V7 %s body has %d lines; hard limit is %d", budget, len(lines), failLimit), where, "move durable detail into linked canon/evidence artifacts and keep this record as a concise summary", map[string]any{"lines": len(lines), "limit": failLimit, "object_type": budget}))
	} else if warnLimit > 0 && len(lines) > warnLimit {
		*warnings = append(*warnings, issue(codePrefix+"_BODY_LONG", fmt.Sprintf("V7 %s body has %d lines; warning limit is %d", budget, len(lines), warnLimit), where, "summarize the body and link durable detail instead of pasting long prose", map[string]any{"lines": len(lines), "limit": warnLimit, "object_type": budget}))
	}
}

func validateV7TaskBodyPolicy(body, vaultPath, where string, errors, warnings *[]Issue) {
	lines := strings.Split(strings.TrimRight(body, "\n"), "\n")
	policy := v7ValidationPolicyFor(vaultPath)
	lower := strings.ToLower(body)
	if policy.ForbidWorkLogSection && (strings.Contains(lower, "\n## work log") || strings.Contains(lower, "\n## execution diary")) {
		*errors = append(*errors, issue("TASK_WORK_LOG_SECTION", "V7 task body must not contain Work Log or Execution Diary sections", where, "record attempts in .tusker/attempts/<task>/ and durable proof in .tusker/evidence/<task>/", nil))
	}
	if strings.Contains(lower, "\n## verification log") {
		*errors = append(*errors, issue("TASK_VERIFICATION_LOG_SECTION", "V7 task body must not contain Verification log sections", where, "use the concise ## Verification table; move chronology to attempts and raw output to .tusker/scratch/<task>/", nil))
	}
	evidenceLines := 0
	for _, line := range strings.Split(sectionContent(body, "## Evidence"), "\n") {
		if strings.TrimSpace(line) != "" {
			evidenceLines++
		}
	}
	if evidenceLines > 10 {
		*errors = append(*errors, issue("TASK_EVIDENCE_SECTION_TOO_LONG", fmt.Sprintf("V7 task Evidence section has %d non-empty lines; hard limit is 10", evidenceLines), where, "keep only accepted/pending first-class proof links; move chronology to attempt summaries", map[string]any{"lines": evidenceLines}))
	} else if evidenceLines > 8 {
		*warnings = append(*warnings, issue("TASK_EVIDENCE_SECTION_LONG", fmt.Sprintf("V7 task Evidence section has %d non-empty lines; target is 8", evidenceLines), where, "keep task evidence concise", map[string]any{"lines": evidenceLines}))
	}
	rawHits := 0
	section := ""
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "## ") {
			section = strings.ToLower(trimmed)
		}
		if section == "## verification" && strings.HasPrefix(trimmed, "|") {
			continue
		}
		if v7RawLogLinePattern.MatchString(line) {
			rawHits++
		}
	}
	if policy.ForbidRawLogsInTask && rawHits >= 5 {
		*errors = append(*errors, issue("TASK_RAW_LOG_IN_BODY", "V7 task body appears to contain raw command output", where, "store raw debug exhaust under .tusker/scratch/<task>/ and summarize the proof signal in verification or an attempt summary", nil))
	}
}

func validateV7Body(body, vaultPath, where string, errors, warnings *[]Issue) {
	validateV7BodyBudget(Note{Data: map[string]any{"kind": v7KindFromPath(where)}, Body: body}, vaultPath, where, errors, warnings)
}

func validateV7AttachmentsPolicy(vaultPath string) ([]Issue, []Issue) {
	var errors, warnings []Issue
	attachments := filepath.Join(vaultPath, "Attachments")
	if !dirExists(attachments) {
		return errors, warnings
	}
	finding := issue("V7_ATTACHMENTS_FORBIDDEN", "Attachments/ is deprecated for V7 proof; migrate raw files to .tusker/scratch and promote only acceptance-linked artifacts", "Attachments", "run `tusker attachments migrate --dry-run`", nil)
	if v7ValidationPolicyFor(vaultPath).StrictProofPolicy {
		errors = append(errors, finding)
	} else {
		warnings = append(warnings, finding)
	}
	return errors, warnings
}

func v7BodyBudgetObject(note Note, where string) string {
	kind := effectiveV7Kind(note.Data)
	switch kind {
	case "task":
		return "task"
	case "gate":
		return "gate"
	case "domain":
		return "index"
	case "domain_canon":
		return "canon"
	case "evidence":
		return "evidence_summary"
	}
	lower := strings.ToLower(filepath.ToSlash(where))
	if strings.Contains(lower, "runbook") {
		return "runbook"
	}
	return ""
}

func validateV7RecordSecrets(note Note, where string, errors *[]Issue) {
	frontmatter, _ := json.Marshal(note.Data)
	if containsV7Secret(string(frontmatter) + "\n" + note.Body) {
		*errors = append(*errors, issue("SECRET_IN_RECORD", "Tusker records must not contain obvious secrets", where, "", nil))
	}
}

type v7ValidationPolicy struct {
	RequireAcceptanceProof bool
	ForbidWorkLogSection   bool
	ForbidRawLogsInTask    bool
	ProtectStateFields     bool
	StrictProofPolicy      bool
}

func v7ValidationPolicyFor(vaultPath string) v7ValidationPolicy {
	policy := v7ValidationPolicy{
		RequireAcceptanceProof: true,
		ForbidWorkLogSection:   true,
		ForbidRawLogsInTask:    true,
		ProtectStateFields:     true,
		StrictProofPolicy:      false,
	}
	if strings.TrimSpace(vaultPath) == "" {
		return policy
	}
	cfg, _, err := readV7TuskerConfig(vaultPath)
	if err != nil {
		return policy
	}
	if cfg.Validation.RequireAcceptanceProof != nil {
		policy.RequireAcceptanceProof = *cfg.Validation.RequireAcceptanceProof
	}
	if cfg.Validation.ForbidWorkLogSection != nil {
		policy.ForbidWorkLogSection = *cfg.Validation.ForbidWorkLogSection
	}
	if cfg.Validation.ForbidRawLogsInTask != nil {
		policy.ForbidRawLogsInTask = *cfg.Validation.ForbidRawLogsInTask
	}
	if cfg.Validation.ProtectStateFields != nil {
		policy.ProtectStateFields = *cfg.Validation.ProtectStateFields
	}
	if cfg.Validation.StrictProofPolicy != nil {
		policy.StrictProofPolicy = *cfg.Validation.StrictProofPolicy
	}
	return policy
}

func v7BodyLineLimits(vaultPath string) (int, int) {
	return v7BodyLineLimitsFor(vaultPath, "task")
}

func v7BodyLineLimitsFor(vaultPath, objectType string) (int, int) {
	warnLimit, failLimit := defaultV7BodyLineLimitsFor(objectType)
	if strings.TrimSpace(vaultPath) == "" {
		return warnLimit, failLimit
	}
	cfg, _, err := readV7TuskerConfig(vaultPath)
	if err != nil {
		return warnLimit, failLimit
	}
	if objectType == "task" {
		if cfg.Validation.TaskBodyWarnLines > 0 {
			warnLimit = cfg.Validation.TaskBodyWarnLines
		}
		if cfg.Validation.TaskBodyFailLines > 0 {
			failLimit = cfg.Validation.TaskBodyFailLines
		}
	}
	if failLimit > 0 && warnLimit > failLimit {
		warnLimit = failLimit
	}
	return warnLimit, failLimit
}

func defaultV7BodyLineLimitsFor(objectType string) (int, int) {
	switch objectType {
	case "gate":
		return 80, 160
	case "index":
		return 180, 300
	case "canon":
		return 220, 400
	case "runbook":
		return 240, 500
	case "evidence_summary":
		return 80, 160
	default:
		return 120, 220
	}
}

func containsV7Secret(text string) bool {
	for _, secretPattern := range []string{"sk-[a-zA-Z0-9]{20,}", `(?i)api[_-]?key\s*[:=]\s*["']?[A-Za-z0-9_\-]{20,}`, `(?i)password\s*[:=]\s*["']?[^"'\s]{8,}`} {
		if regexp.MustCompile(secretPattern).MatchString(text) {
			return true
		}
	}
	return false
}

func v7FrontmatterWarnLimit(vaultPath string) int {
	limit := 60
	if strings.TrimSpace(vaultPath) == "" {
		return limit
	}
	cfg, _, err := readV7TuskerConfig(vaultPath)
	if err != nil {
		return limit
	}
	if cfg.Validation.FrontmatterWarnLines > 0 {
		limit = cfg.Validation.FrontmatterWarnLines
	}
	return limit
}

func v7AcceptanceHasProof(body string) bool {
	content := strings.ToLower(sectionContent(body, "## Acceptance"))
	return strings.Contains(content, "| proof |") || strings.Contains(content, "proof:")
}

func v7VagueAcceptanceItems(body string) []string {
	content := sectionContent(body, "## Acceptance")
	seen := map[string]bool{}
	var vague []string
	for _, line := range strings.Split(content, "\n") {
		item := v7AcceptanceOutcomeFromLine(line)
		if item == "" || !v7AcceptanceOutcomeVague(item) {
			continue
		}
		normalized := strings.ToLower(item)
		if seen[normalized] {
			continue
		}
		seen[normalized] = true
		vague = append(vague, item)
	}
	return vague
}

func v7AcceptanceOutcomeFromLine(line string) string {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return ""
	}
	if strings.HasPrefix(trimmed, "|") {
		parts := strings.Split(trimmed, "|")
		var cells []string
		for _, part := range parts {
			cell := strings.TrimSpace(part)
			if cell != "" {
				cells = append(cells, cell)
			}
		}
		if len(cells) < 2 {
			return ""
		}
		first := strings.ToLower(cells[0])
		second := strings.ToLower(cells[1])
		if first == "id" || second == "outcome" || strings.Trim(cells[0], "-: ") == "" {
			return ""
		}
		return cells[1]
	}
	lower := strings.ToLower(trimmed)
	if !strings.HasPrefix(lower, "- [ ]") && !strings.HasPrefix(lower, "- [x]") {
		return ""
	}
	item := strings.TrimSpace(trimmed[5:])
	if i := strings.Index(item, "Proof:"); i >= 0 {
		item = strings.TrimSpace(item[:i])
	}
	if match := v7AcceptanceIDLine.FindStringSubmatch(item); match != nil {
		item = strings.TrimSpace(match[1])
	}
	return strings.Trim(item, " .")
}

func v7AcceptanceOutcomeVague(item string) bool {
	normalized := strings.ToLower(strings.TrimSpace(item))
	normalized = strings.Trim(normalized, ".!")
	normalized = strings.Join(strings.Fields(normalized), " ")
	switch normalized {
	case "works", "it works", "tests pass", "tests passing", "tests are passing", "passes tests", "define the accepted outcome", "tbd", "todo", "placeholder":
		return true
	default:
		return false
	}
}

func findV7Gate(vaultPath, gateID string) (Note, bool) {
	note, err := resolveV7Note(vaultPath, gateID, "gate")
	return note, err == nil
}

func missingRequiredEvidence(vaultPath, taskID string, required []string) []string {
	if len(required) == 0 {
		return nil
	}
	idx, err := loadV7Index(vaultPath)
	if err != nil {
		return required
	}
	found := map[string]bool{}
	for _, ev := range idx.Evidence[taskID] {
		if v7EvidenceUsableForClose(ev) {
			found[stringField(ev.Data, "evidence_kind")] = true
		}
	}
	var missing []string
	for _, kind := range required {
		if !found[kind] {
			missing = append(missing, kind)
		}
	}
	return missing
}

func missingV7AcceptanceProof(vaultPath string, task Note, idx v7Index) []string {
	return computeV7ProofReport(vaultPath, task, idx).Missing
}

func v7EvidenceUsableForClose(ev Note) bool {
	if stringField(ev.Data, "status") != "accepted" {
		return false
	}
	if stringField(ev.Data, "evidence_kind") == "evidence_packet" {
		return false
	}
	if len(normalizeV7Covers(normalizeList(ev.Data["covers"]))) == 0 {
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

func v7AcceptanceWaivers(data map[string]any) []string {
	var out []string
	for _, waiver := range listOfMaps(data["acceptance_waivers"]) {
		if stringField(waiver, "by") == "" || stringField(waiver, "at") == "" || stringField(waiver, "reason") == "" {
			continue
		}
		for _, cover := range normalizeV7Covers(normalizeList(waiver["covers"])) {
			if acceptance := v7AcceptanceIDFromCover(cover); acceptance != "" {
				out = append(out, acceptance)
			}
		}
	}
	return out
}

func listOfMaps(value any) []map[string]any {
	switch v := value.(type) {
	case []map[string]any:
		return v
	case []any:
		out := make([]map[string]any, 0, len(v))
		for _, item := range v {
			switch typed := item.(type) {
			case map[string]any:
				out = append(out, typed)
			case map[any]any:
				next := map[string]any{}
				for key, value := range typed {
					next[toString(key)] = value
				}
				out = append(out, next)
			}
		}
		return out
	default:
		return nil
	}
}

func v7AcceptanceIDs(body string) []string {
	content := sectionContent(body, "## Acceptance")
	seen := map[string]bool{}
	var ids []string
	for _, line := range strings.Split(content, "\n") {
		id := v7AcceptanceIDFromLine(line)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	return ids
}

func v7AcceptanceIDFromLine(line string) string {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return ""
	}
	if strings.HasPrefix(trimmed, "|") {
		parts := strings.Split(trimmed, "|")
		var cells []string
		for _, part := range parts {
			cell := strings.TrimSpace(part)
			if cell != "" {
				cells = append(cells, cell)
			}
		}
		if len(cells) == 0 || strings.EqualFold(cells[0], "id") || strings.Trim(cells[0], "-: ") == "" {
			return ""
		}
		return normalizeV7AcceptanceID(cells[0])
	}
	lower := strings.ToLower(trimmed)
	if !strings.HasPrefix(lower, "- [ ]") && !strings.HasPrefix(lower, "- [x]") {
		return ""
	}
	item := strings.TrimSpace(trimmed[5:])
	if match := v7AcceptanceIDLine.FindStringSubmatch(item); match != nil {
		return normalizeV7AcceptanceID(strings.Split(match[0], ":")[0])
	}
	fields := strings.Fields(item)
	if len(fields) == 0 {
		return ""
	}
	return normalizeV7AcceptanceID(strings.TrimSuffix(fields[0], ":"))
}

func normalizeV7AcceptanceID(value string) string {
	value = strings.ToUpper(strings.TrimSpace(value))
	value = strings.Trim(value, "`:| ")
	if regexp.MustCompile(`^A\d+$`).MatchString(value) {
		return value
	}
	return ""
}

func normalizeV7Covers(covers []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, cover := range covers {
		normalized := v7NormalizeCover(cover)
		if normalized == "" || seen[normalized] {
			continue
		}
		seen[normalized] = true
		out = append(out, normalized)
	}
	return out
}

func v7NormalizeCover(cover string) string {
	cover = strings.ToUpper(strings.TrimSpace(cover))
	cover = strings.Trim(cover, "` ")
	if cover == "" {
		return ""
	}
	if cover == "ALL" || v7AcceptanceRangeCover(cover) {
		return "TASK:" + cover
	}
	if normalizeV7AcceptanceID(cover) != "" {
		return "TASK:" + cover
	}
	parts := strings.SplitN(cover, ":", 2)
	if len(parts) != 2 {
		return ""
	}
	scope := strings.TrimSpace(parts[0])
	id := normalizeV7AcceptanceID(parts[1])
	if id == "" {
		raw := strings.ToUpper(strings.TrimSpace(parts[1]))
		if raw == "ALL" || v7AcceptanceRangeCover(raw) {
			id = raw
		}
	}
	if scope == "" || id == "" {
		return ""
	}
	return scope + ":" + id
}

func v7AcceptanceRangeCover(value string) bool {
	parts := strings.SplitN(strings.ToUpper(strings.TrimSpace(value)), "-", 2)
	if len(parts) != 2 {
		return false
	}
	start := strings.TrimPrefix(normalizeV7AcceptanceID(parts[0]), "A")
	end := strings.TrimPrefix(normalizeV7AcceptanceID(parts[1]), "A")
	return atoiSafe(start) > 0 && atoiSafe(end) >= atoiSafe(start)
}

func v7AcceptanceIDFromCover(cover string) string {
	normalized := v7NormalizeCover(cover)
	if normalized == "" {
		return ""
	}
	parts := strings.SplitN(normalized, ":", 2)
	if len(parts) != 2 {
		return ""
	}
	return normalizeV7AcceptanceID(parts[1])
}

func validateV7BranchPolicy(vaultPath string, args Args) ([]Issue, []Issue) {
	var errors, warnings []Issue
	if !v7ValidationPolicyFor(vaultPath).ProtectStateFields {
		return errors, warnings
	}
	repoRoot := v7RepoRoot(vaultPath)
	branch, err := currentGitBranchIn(repoRoot)
	if err != nil || branch == "" || branch == "HEAD" {
		errors = append(errors, issue("BRANCH_POLICY_BRANCH_UNAVAILABLE", "branch-policy validation requires a checked-out Git branch in team mode", "", "run from a Git worktree branch or use explicit local-only mode for local work", map[string]any{"repo_root": repoRoot, "branch": branch}))
		return errors, warnings
	}
	if isV7ControlBranch(vaultPath, branch) {
		return errors, warnings
	}
	if args.Bool("staged") {
		changes, err := gitV7NameStatusChanges(repoRoot, append([]string{"diff", "--name-status", "--cached", "--"}, v7ProtectedGitPaths(vaultPath)...)...)
		if err != nil {
			return errors, warnings
		}
		for _, change := range changes {
			errors = append(errors, protectedFieldIssuesForGitChange(repoRoot, branch, change, "HEAD", ":")...)
		}
		return errors, warnings
	}

	baseRef := firstNonEmpty(args.String("base"), "origin/main")
	if !gitRefExists(repoRoot, baseRef) {
		baseRef = "HEAD~1"
	}
	mergeBaseOut, err := exec.Command("git", "-C", repoRoot, "merge-base", "HEAD", baseRef).Output()
	if err != nil {
		warnings = append(warnings, issue("BRANCH_POLICY_BASE_UNAVAILABLE", "could not resolve branch-policy base ref "+baseRef, "", "", nil))
		return errors, warnings
	}
	mergeBase := strings.TrimSpace(string(mergeBaseOut))
	changes, err := gitV7NameStatusChanges(repoRoot, append([]string{"diff", "--name-status", mergeBase + "...HEAD", "--"}, v7ProtectedGitPaths(vaultPath)...)...)
	if err != nil {
		return errors, warnings
	}
	for _, change := range changes {
		errors = append(errors, protectedFieldIssuesForGitChange(repoRoot, branch, change, mergeBase, "HEAD")...)
	}
	return errors, warnings
}

func v7ProtectedGitPaths(vaultPath string) []string {
	root := vaultDisplayRoot(vaultPath)
	paths := []string{
		"work/tasks",
		"work/gates",
		"work/epics",
		"work/decisions",
		"work/inbox",
		"evidence",
		"attempts",
	}
	for i, path := range paths {
		paths[i] = filepath.ToSlash(filepath.Join(root, path))
	}
	return paths
}

type v7GitNameStatusChange struct {
	Status  string
	Path    string
	OldPath string
}

func gitV7NameStatusChanges(repoRoot string, args ...string) ([]v7GitNameStatusChange, error) {
	fullArgs := append([]string{"-C", repoRoot}, args...)
	changedOut, err := exec.Command("git", fullArgs...).Output()
	if err != nil {
		return nil, err
	}
	var changes []v7GitNameStatusChange
	for _, line := range strings.Split(strings.TrimSpace(string(changedOut)), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		change := v7GitNameStatusChange{Status: fields[0], Path: fields[1]}
		if (strings.HasPrefix(change.Status, "R") || strings.HasPrefix(change.Status, "C")) && len(fields) >= 3 {
			change.OldPath = fields[1]
			change.Path = fields[2]
		}
		changes = append(changes, change)
	}
	return changes, nil
}

func protectedFieldIssuesForGitChange(repoRoot, branch string, change v7GitNameStatusChange, beforeRef, afterRef string) []Issue {
	if strings.HasPrefix(change.Status, "R") || strings.HasPrefix(change.Status, "C") {
		var issues []Issue
		if change.OldPath != "" {
			issues = append(issues, protectedFieldIssuesForGitChange(repoRoot, branch, v7GitNameStatusChange{Status: "D", Path: change.OldPath}, beforeRef, afterRef)...)
		}
		issues = append(issues, protectedFieldIssuesForGitChange(repoRoot, branch, v7GitNameStatusChange{Status: "A", Path: change.Path}, beforeRef, afterRef)...)
		return issues
	}

	var beforeRaw, afterRaw string
	if !strings.HasPrefix(change.Status, "A") {
		if raw, err := exec.Command("git", "-C", repoRoot, "show", beforeRef+":"+change.Path).Output(); err == nil {
			beforeRaw = string(raw)
		}
	}
	if !strings.HasPrefix(change.Status, "D") {
		if afterRef == ":" {
			raw, err := exec.Command("git", "-C", repoRoot, "show", ":"+change.Path).Output()
			if err != nil {
				return nil
			}
			afterRaw = string(raw)
		} else {
			raw, err := exec.Command("git", "-C", repoRoot, "show", afterRef+":"+change.Path).Output()
			if err != nil {
				return nil
			}
			afterRaw = string(raw)
		}
	}
	return protectedFieldIssuesForDiff(change.Path, branch, beforeRaw, afterRaw)
}

func protectedFieldIssuesForDiff(file, branch, beforeRaw, afterRaw string) []Issue {
	beforeData, _, _ := parseFrontmatter(beforeRaw)
	afterData, _, _ := parseFrontmatter(afterRaw)
	return protectedFieldIssues(file, branch, beforeData, afterData)
}

func protectedFieldIssues(file, branch string, beforeData, afterData map[string]any) []Issue {
	var errors []Issue
	protectedFields := v7ProtectedFieldsForDiff(file, beforeData, afterData)
	for _, field := range changedFields(beforeData, afterData) {
		if _, protected := protectedFields[field]; protected {
			errors = append(errors, issue("PROTECTED_FIELD_CHANGED", fmt.Sprintf("protected Tusker state field %q changed on non-control branch %s", field, branch), file, "use Tusker control operations on a configured control branch, or propose evidence/attempt/inbox changes instead", map[string]any{"field": field, "branch": branch}))
		}
	}
	return errors
}

func v7ProtectedFieldsForDiff(file string, beforeData, afterData map[string]any) map[string]bool {
	kind := firstNonEmpty(effectiveV7KindFromData(afterData), effectiveV7KindFromData(beforeData), v7KindFromPath(file))
	fields := map[string]bool{}
	for field := range v7ProtectedCommonFields {
		fields[field] = true
	}
	for field := range v7ProtectedFieldsByKind[kind] {
		fields[field] = true
	}
	return fields
}

func effectiveV7KindFromData(data map[string]any) string {
	if len(data) == 0 {
		return ""
	}
	return effectiveV7Kind(data)
}

func v7KindFromPath(path string) string {
	normalized := filepath.ToSlash(path)
	switch {
	case strings.Contains(normalized, "/work/tasks/"):
		return "task"
	case strings.Contains(normalized, "/work/gates/"):
		return "gate"
	case strings.Contains(normalized, "/work/epics/"):
		return "epic"
	case strings.Contains(normalized, "/work/decisions/"):
		return "decision"
	case strings.Contains(normalized, "/work/inbox/"):
		return "proposal"
	case strings.Contains(normalized, "/work/closeouts/"):
		return "closeout"
	case strings.Contains(normalized, "/evidence/"):
		return "evidence"
	case strings.Contains(normalized, "/attempts/"):
		return "attempt"
	default:
		return ""
	}
}

func changedFields(before, after map[string]any) []string {
	keys := map[string]bool{}
	for key := range before {
		keys[key] = true
	}
	for key := range after {
		keys[key] = true
	}
	var changed []string
	for key := range keys {
		if toString(before[key]) != toString(after[key]) {
			changed = append(changed, key)
		}
	}
	sortStrings(changed)
	return changed
}

func isV7ControlBranch(vaultPath, branch string) bool {
	for _, control := range configuredV7ControlBranches(vaultPath) {
		if branch == control {
			return true
		}
	}
	return false
}

func configuredV7ControlBranches(vaultPath string) []string {
	cfg, _, err := readV7TuskerConfig(vaultPath)
	if err == nil {
		if branches := filterStrings(cfg.Branches.Control); len(branches) > 0 {
			return branches
		}
		if branch := strings.TrimSpace(cfg.Branches.DefaultBranch); branch != "" {
			return []string{branch}
		}
	}
	return []string{"main", "trunk", "master"}
}

func gitRefExists(repoRoot, ref string) bool {
	return exec.Command("git", "-C", repoRoot, "rev-parse", "--verify", "--quiet", ref).Run() == nil
}

func sortStrings(values []string) {
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
}
