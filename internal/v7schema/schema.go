package v7schema

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"regexp"
	"strings"
)

var (
	TaskIDPattern     = regexp.MustCompile(`^([A-Z]{3})-T-(\d{4})$`)
	GateIDPattern     = regexp.MustCompile(`^([A-Z]{3})-G-(\d{4})$`)
	DecisionIDPattern = regexp.MustCompile(`^([A-Z]{3})-D-(\d{4})$`)
	ProposalIDPattern = regexp.MustCompile(`^([A-Z]{3})-P-(\d{4})$`)
	EvidenceIDPattern = regexp.MustCompile(`^([A-Z]{3})-T-(\d{4})-E-(\d{4})$`)
	AttemptIDPattern  = regexp.MustCompile(`^([A-Z]{3})-T-(\d{4})-A-(\d{4})$`)

	TaskStatuses   = makeSet("idea", "backlog", "ready", "review", "rework", "done", "cancelled", "superseded")
	Readiness      = makeSet("ready", "blocked_by_gate", "blocked_by_dependency", "waiting_on_review", "waiting_on_human", "waiting_on_ci", "held", "done", "cancelled", "superseded")
	GateKinds      = makeSet("auth", "env", "setup", "dev_host", "ci", "verification", "signoff", "decision", "quota", "external_service", "manual_hold", "security", "release")
	GateStatuses   = makeSet("open", "satisfied", "waived", "obsolete")
	ProofModes     = makeSet("none", "inline", "card", "artifact", "audit")
	ProofStatuses  = makeSet("pending", "partial", "satisfied", "waived")
	EvidenceKinds  = makeSet("automated_test", "unit_test", "integration_test", "e2e_test", "verification_summary", "screenshot", "video", "trace", "log_excerpt", "manual_smoke", "physical_smoke", "ci_run", "provider_probe", "benchmark", "review_packet", "security_review", "privacy_review", "accessibility_review", "performance_profile", "release_smoke", "human_review")
	EvidenceStatus = makeSet("pending_review", "accepted", "rejected", "superseded", "historical")
	AttemptStatus  = makeSet("started", "handoff", "failed")
	DecisionStatus = makeSet("proposed", "accepted", "superseded")
	ProposalStatus = makeSet("proposed", "accepted", "rejected", "superseded")
	ProposalAction = makeSet("close", "status", "change", "create_task", "create_gate", "create_decision")
	DomainStatus   = makeSet("draft", "current", "deprecated")
)

var FrontmatterOrder = map[string][]string{
	"task": {
		"schema", "kind", "id", "project", "title", "epic", "status", "readiness", "priority", "risk", "size",
		"proof_mode", "proof_status", "proof_required", "proof_required_owner", "evidence_budget", "raw_artifacts_allowed", "raw_artifacts_reason",
		"machine_status", "human_status", "closeout_status", "agent_action",
		"next_owner", "next_source", "next_ref", "next_action", "domains", "gates", "dependencies", "evidence_required",
		"accepted_by", "accepted_at", "closed_at", "superseded_by", "created_at", "created_by", "updated_at", "updated_by", "state_rev",
	},
	"closeout": {
		"schema", "kind", "id", "project", "task", "state", "agent_action", "state_fingerprint",
		"machine_missing", "human_missing", "reviewer_missing", "external_missing", "open_human_gates", "review_packet", "validation",
		"created_by", "created_at", "state_rev",
	},
	"gate": {
		"schema", "kind", "id", "project", "title", "gate_kind", "status", "owner", "priority", "blocking", "blocks",
		"covers", "action", "verification", "satisfaction_evidence", "satisfaction_evidence_refs", "satisfied_by", "satisfied_at", "waived_by", "waived_at", "waive_reason", "obsolete_reason",
		"created_at", "created_by", "updated_at", "updated_by", "state_rev",
	},
	"epic": {
		"schema", "kind", "id", "project", "title", "status", "owner", "priority", "domains", "next_task_number", "next_gate_number", "next_decision_number", "created_at", "updated_at", "state_rev",
	},
	"decision": {
		"schema", "kind", "id", "project", "epic", "title", "status", "decided_by", "decided_at", "supersedes", "created_at", "created_by", "updated_at", "updated_by", "state_rev",
	},
	"evidence": {
		"schema", "kind", "id", "project", "task", "epic", "evidence_kind", "status", "covers", "artifact_paths", "artifact_durability", "screenshot_checked_by", "screenshot_checked_at", "redacted", "redaction_note", "created_by", "created_at", "accepted_by", "accepted_at", "state_rev",
	},
	"attempt": {
		"schema", "kind", "id", "project", "task", "runner", "agent_model", "workspace_kind", "workspace_path", "branch", "status", "started_at", "ended_at", "pr_url", "evidence", "state_rev",
	},
	"proposal": {
		"schema", "kind", "id", "project", "title", "status", "action", "target_kind", "target", "proposed_fields", "proposed_by", "source_branch", "reviewed_by", "reviewed_at", "review_reason", "applying_by", "applying_at", "apply_transaction", "applied_by", "applied_at", "applied_target", "applied_target_rev", "created_at", "updated_at", "updated_by", "state_rev",
	},
	"domain": {
		"schema", "kind", "id", "project", "title", "status", "summary", "source_of_truth", "canonical_files", "created_at", "updated_at", "state_rev",
	},
	"domain_canon": {
		"schema", "kind", "id", "project", "domain", "title", "status", "summary", "source_of_truth", "created_at", "updated_at", "state_rev",
	},
	"knowledge": {
		"schema", "kind", "id", "project", "domain", "title", "status", "summary", "source_of_truth", "related", "created_at", "updated_at", "state_rev",
	},
	"project_skill": {
		"schema", "kind", "name", "project", "status", "description", "operator_skill", "source_of_truth", "canonical_files", "created_at", "updated_at", "state_rev",
	},
}

type TuskerConfigFile struct {
	ProjectID    string `yaml:"project_id"`
	MutationMode string `yaml:"mutation_mode"`
	Branches     struct {
		DefaultBranch          string   `yaml:"default_branch"`
		Control                []string `yaml:"control"`
		StateBranch            string   `yaml:"state_branch"`
		ImplementationPatterns []string `yaml:"implementation_patterns"`
		MutationMode           string   `yaml:"mutation_mode"`
	} `yaml:"branches"`
	Runtime struct {
		LeaseBackend    string `yaml:"lease_backend"`
		LeaseTTLMinutes int    `yaml:"lease_ttl_minutes"`
		MutationMode    string `yaml:"mutation_mode"`
	} `yaml:"runtime"`
	Validation struct {
		TaskBodyWarnLines      int   `yaml:"task_body_warn_lines"`
		TaskBodyFailLines      int   `yaml:"task_body_fail_lines"`
		FrontmatterWarnLines   int   `yaml:"frontmatter_warn_lines"`
		RequireAcceptanceProof *bool `yaml:"require_acceptance_proof"`
		ForbidWorkLogSection   *bool `yaml:"forbid_work_log_section"`
		ForbidRawLogsInTask    *bool `yaml:"forbid_raw_logs_in_task"`
		ProtectStateFields     *bool `yaml:"protect_state_fields"`
		StrictProofPolicy      *bool `yaml:"strict_proof_policy"`
	} `yaml:"validation"`
}

func ProjectID(vaultPath string, readText func(string) (string, error)) string {
	projectPath := vaultPath + "/_system/project.yaml"
	raw, err := readText(projectPath)
	if err != nil {
		return "tusker"
	}
	for _, line := range strings.Split(raw, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "id:") {
			return strings.Trim(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "id:")), `"`)
		}
	}
	return "tusker"
}

func StateRev(data map[string]any, body string) string {
	copyData := map[string]any{}
	for k, v := range data {
		if k == "state_rev" {
			continue
		}
		copyData[k] = stateRevValue(v)
	}
	raw, _ := json.Marshal(copyData)
	normalizedBody := strings.TrimRight(strings.TrimLeft(body, "\n"), "\n\t ")
	sum := sha256.Sum256([]byte(string(raw) + "\n" + normalizedBody))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func stateRevValue(value any) any {
	if value == nil {
		return ""
	}
	rv := reflect.ValueOf(value)
	if !rv.IsValid() {
		return ""
	}
	switch rv.Kind() {
	case reflect.Slice, reflect.Array:
		if rv.Kind() == reflect.Slice && rv.IsNil() {
			return []any{}
		}
		out := make([]any, 0, rv.Len())
		for i := 0; i < rv.Len(); i++ {
			out = append(out, stateRevValue(rv.Index(i).Interface()))
		}
		return out
	case reflect.Map:
		if rv.IsNil() {
			return map[string]any{}
		}
	}
	switch current := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(current))
		for k, v := range current {
			out[k] = stateRevValue(v)
		}
		return out
	case map[any]any:
		out := make(map[string]any, len(current))
		for k, v := range current {
			out[strings.TrimSpace(stringField(map[string]any{"key": k}, "key"))] = stateRevValue(v)
		}
		return out
	default:
		return value
	}
}

func EffectiveKind(data map[string]any) string {
	if kind := strings.TrimSpace(stringField(data, "kind")); kind != "" {
		return kind
	}
	return strings.TrimSpace(stringField(data, "type"))
}

func EpicFromTaskID(id string) string {
	match := TaskIDPattern.FindStringSubmatch(id)
	if match == nil {
		return ""
	}
	return match[1]
}

func AcceptanceCount(body string) int {
	content := sectionContent(body, "## Acceptance")
	count := 0
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "| A") || strings.HasPrefix(trimmed, "- [") {
			count++
		}
	}
	return count
}

func MaybeCodeBlock(value string) string {
	if strings.TrimSpace(value) == "" {
		return "Not recorded."
	}
	return "```sh\n" + strings.TrimSpace(value) + "\n```"
}

func BulletList(items []string) string {
	if len(items) == 0 {
		return "- None."
	}
	var lines []string
	for _, item := range items {
		lines = append(lines, "- "+item)
	}
	return strings.Join(lines, "\n")
}

func WikiLinks(items []string) string {
	if len(items) == 0 {
		return ""
	}
	var links []string
	for _, item := range items {
		links = append(links, "[["+item+"]]")
	}
	return strings.Join(links, ", ")
}

func NormalizeMutationMode(mode string) string {
	mode = strings.ToLower(strings.TrimSpace(mode))
	mode = strings.ReplaceAll(mode, "-", "_")
	mode = strings.ReplaceAll(mode, " ", "_")
	return mode
}

func makeSet(values ...string) map[string]struct{} {
	out := make(map[string]struct{}, len(values))
	for _, value := range values {
		out[value] = struct{}{}
	}
	return out
}

func stringField(data map[string]any, key string) string {
	value, ok := data[key]
	if !ok || value == nil {
		return ""
	}
	if s, ok := value.(string); ok {
		return strings.TrimSpace(s)
	}
	return strings.TrimSpace(fmtAny(value))
}

func fmtAny(value any) string {
	switch v := value.(type) {
	case string:
		return v
	default:
		return strings.TrimSpace(strings.ReplaceAll(strings.TrimPrefix(strings.TrimSuffix(jsonMarshal(v), "\""), "\""), `\"`, `"`))
	}
}

func jsonMarshal(value any) string {
	raw, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return string(raw)
}

func sectionContent(body, heading string) string {
	start := strings.Index(body, heading)
	if start == -1 {
		return ""
	}
	rest := body[start+len(heading):]
	next := strings.Index(rest, "\n## ")
	if next == -1 {
		return strings.TrimSpace(rest)
	}
	return strings.TrimSpace(rest[:next])
}
