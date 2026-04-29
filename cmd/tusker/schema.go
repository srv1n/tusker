package main

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

type Note struct {
	AbsolutePath string
	RelativePath string
	Data         map[string]any
	Body         string
}

type Issue struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Path    string `json:"path"`
	Hint    string `json:"hint"`
	Line    *int   `json:"line"`
	Context any    `json:"context"`
}

func (i Issue) MarshalJSON() ([]byte, error) {
	type wireIssue struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		Path    any    `json:"path"`
		Hint    any    `json:"hint"`
		Line    *int   `json:"line"`
		Context any    `json:"context"`
	}

	wire := wireIssue{
		Code:    i.Code,
		Message: i.Message,
		Path:    nullIfEmptyString(i.Path),
		Hint:    nullIfEmptyString(i.Hint),
		Line:    i.Line,
		Context: i.Context,
	}
	return json.Marshal(wire)
}

type TuskerError struct {
	Code    string
	Message string
	Hint    string
	Path    string
	Line    *int
	Context any
}

func (e *TuskerError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

func tuskerError(code, message string, extras ...func(*TuskerError)) error {
	err := &TuskerError{Code: code, Message: message}
	for _, extra := range extras {
		extra(err)
	}
	return err
}

func withHint(hint string) func(*TuskerError) {
	return func(err *TuskerError) { err.Hint = hint }
}

func withPath(path string) func(*TuskerError) {
	return func(err *TuskerError) { err.Path = path }
}

func withContext(ctx any) func(*TuskerError) {
	return func(err *TuskerError) { err.Context = ctx }
}

func errorToIssue(err error) Issue {
	if typed, ok := err.(*TuskerError); ok {
		return Issue{
			Code:    typed.Code,
			Message: typed.Message,
			Path:    typed.Path,
			Hint:    typed.Hint,
			Line:    typed.Line,
			Context: typed.Context,
		}
	}
	return Issue{Code: "UNKNOWN", Message: err.Error()}
}

func formatIssue(issue Issue) string {
	loc := ""
	if issue.Path != "" {
		loc = issue.Path + ": "
	}
	hint := ""
	if issue.Hint != "" {
		hint = "  (hint: " + issue.Hint + ")"
	}
	return fmt.Sprintf("[%s] %s%s%s", issue.Code, loc, issue.Message, hint)
}

func issue(code, message string, path string, hint string, context any) Issue {
	return Issue{Code: code, Message: message, Path: path, Hint: hint, Context: context}
}

func nullIfEmptyString(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

const (
	errorMissingArg                 = "MISSING_ARG"
	errorInvalidArg                 = "INVALID_ARG"
	errorNotFound                   = "NOT_FOUND"
	errorAlreadyExists              = "ALREADY_EXISTS"
	errorUnknownType                = "UNKNOWN_TYPE"
	errorMissingField               = "MISSING_FIELD"
	errorInvalidField               = "INVALID_FIELD"
	errorIDScheme                   = "ID_SCHEME"
	errorIDKindMismatch             = "ID_KIND_MISMATCH"
	errorIDCollision                = "ID_COLLISION"
	errorPathMismatch               = "PATH_MISMATCH"
	errorPathEscape                 = "PATH_ESCAPE"
	errorMissingSection             = "MISSING_SECTION"
	errorLacksSubstance             = "LACKS_SUBSTANCE"
	errorMissingHandoffMarker       = "MISSING_HANDOFF_MARKER"
	errorUIDemoMissing              = "UI_DEMO_MISSING"
	errorEvidenceGate               = "EVIDENCE_GATE"
	errorOrphanWork                 = "ORPHAN_WORK"
	errorUnknownEpic                = "UNKNOWN_EPIC"
	errorEpicAcronymMismatch        = "EPIC_ACRONYM_MISMATCH"
	errorEpicStoryStack             = "EPIC_STORY_STACK_MISSING"
	errorChildrenUnfinished         = "CHILDREN_UNFINISHED"
	errorAttestationMissing         = "ATTESTATION_MISSING"
	errorAttestationRole            = "ATTESTATION_ROLE"
	errorSignoffMissing             = "SIGNOFF_MISSING"
	errorAlreadyClaimed             = "ALREADY_CLAIMED"
	errorNotClaimed                 = "NOT_CLAIMED"
	errorInvalidDispatchState       = "INVALID_DISPATCH_STATE"
	errorInvalidTransition          = "INVALID_TRANSITION"
	errorInvalidFailureClass        = "INVALID_FAILURE_CLASS"
	errorHookFailed                 = "HOOK_FAILED"
	errorHookTimeout                = "HOOK_TIMEOUT"
	errorConfigInvalid              = "CONFIG_INVALID"
	errorPublishPathMissing         = "PUBLISH_PATH_MISSING"
	errorPublishDescriptionMissing  = "PUBLISH_DESCRIPTION_MISSING"
	errorPublishPathInvalid         = "PUBLISH_PATH_INVALID"
	errorPublishPathCollision       = "PUBLISH_PATH_COLLISION"
	errorPublishStatusInvalid       = "PUBLISH_STATUS_INVALID"
	errorPublishOrderInvalid        = "PUBLISH_ORDER_INVALID"
	errorPublishSectionTitleInvalid = "PUBLISH_SECTION_TITLE_INVALID"
	errorDocsManifestStale          = "DOCS_MANIFEST_STALE"
	errorDocsManifestInvalid        = "DOCS_MANIFEST_INVALID"
	errorDocsManifestMismatch       = "DOCS_MANIFEST_MISMATCH"
	errorDocsCanonMissing           = "DOCS_CANON_MISSING"
	errorDocsRouteRemoved           = "DOCS_ROUTE_REMOVED"
	errorDocsSourceMissing          = "DOCS_SOURCE_MISSING"
)

var (
	noteTypes              = makeSet("epic", "story", "bug", "doc", "note")
	epicStatuses           = makeSet("intake", "active", "blocked", "done", "cancelled")
	storyStatuses          = makeSet("intake", "active", "blocked", "in_review", "rework", "merging", "done", "cancelled")
	reviewStates           = makeSet("none", "verification_requested", "requested", "changes_requested", "approved")
	docStatuses            = makeSet("draft", "review", "approved", "published", "archived")
	publishableDocStatuses = makeSet("approved", "published")
	docIntents             = makeSet("canon", "companion")
	canonicalStatuses      = makeSet("draft", "approved", "deprecated", "historical")
	changeTypes            = makeSet("feature", "bug", "refactor", "migration", "security", "docs", "chore", "research", "incident")
	sizes                  = makeSet("s", "m", "l", "xl")
	risks                  = makeSet("low", "medium", "high", "critical")
	priorities             = makeSet("p0", "p1", "p2", "p3", "icebox")
	delegations            = makeSet("execute", "explore", "escalate")
	aiAssistanceLevels     = makeSet("none", "light", "moderate", "heavy")
	docAudiences           = makeSet("developer", "user", "release", "support", "internal")
	publicationLanes       = makeSet("developer", "user", "release-notes", "support", "internal")
	uiSurfaces             = makeSet("frontend", "desktop", "mobile")
	epicAcronymPattern     = regexp.MustCompile(`^[A-Z]{3}$`)
	storyIDPattern         = regexp.MustCompile(`^([A-Z]{3})-S-(\d{4})$`)
	bugIDPattern           = regexp.MustCompile(`^([A-Z]{3})-B-(\d{4})$`)
	docIDPattern           = regexp.MustCompile(`^([A-Z]{3})-D-(\d{4})$`)
	recordIDPattern        = regexp.MustCompile(`^[0-9A-HJKMNP-TV-Z]{26}$`)
	assetLinkRegex         = regexp.MustCompile(`(!\[.*?\]\(.+?\))|(!\[\[.+?\]\])|(\[.+?\]\((https?:\/\/\S+?)\))`)
	integerStringPattern   = regexp.MustCompile(`^-?\d+$`)
)

var statusTransitionDateFields = map[string]string{
	"active":    "started",
	"in_review": "review_requested_at",
	"done":      "completed",
	"cancelled": "cancelled_at",
	"blocked":   "blocked_since",
}

var frontmatterOrder = map[string][]string{
	"epic": {
		"schema_version", "record_id", "id", "title", "type", "status", "owner", "summary", "target_release", "spec_source", "docs",
		"docs_record_ids", "created", "updated", "started", "blocked_since", "completed", "cancelled_at", "success_metrics", "transitions", "tags",
	},
	"story": {
		"schema_version", "record_id", "id", "title", "type", "status", "review_state", "work_revision", "change_type", "epic", "epic_record_id", "size", "risk", "priority", "delegation",
		"surfaces", "assignee", "requester", "ai_assistance", "ai_tools", "ai_session_log", "attested_by", "attested_at",
		"attested_role", "signoff_by", "signoff_at", "review_requested_at", "verified_by", "verified_at", "reviewed_by", "reviewed_at",
		"dod_code_complete", "dod_user_verified", "created", "updated", "due", "started", "completed", "cancelled_at",
		"blocked_since", "prs", "related", "related_record_ids", "blocks", "blocks_record_ids", "blocked_by",
		"blocked_by_record_ids", "transitions", "tags",
	},
	"bug": {
		"schema_version", "record_id", "id", "title", "type", "status", "review_state", "work_revision", "change_type", "epic", "epic_record_id", "size", "risk", "priority", "delegation",
		"surfaces", "assignee", "requester", "ai_assistance", "ai_tools", "ai_session_log", "attested_by", "attested_at",
		"attested_role", "signoff_by", "signoff_at", "review_requested_at", "verified_by", "verified_at", "reviewed_by", "reviewed_at",
		"dod_code_complete", "dod_user_verified", "created", "updated", "due", "started", "completed", "cancelled_at",
		"blocked_since", "prs", "related", "related_record_ids", "blocks", "blocks_record_ids", "blocked_by",
		"blocked_by_record_ids", "transitions", "tags",
	},
	"doc": {
		"schema_version", "record_id", "id", "title", "type", "status", "epic", "epic_record_id", "doc_intent", "canon_for", "story", "story_record_id",
		"audience", "canonical", "canonical_status", "owner_epic", "verified_at", "deprecated", "superseded_by",
		"publish", "publish_path", "publish_description", "publish_order", "publish_section_title", "redirect_from", "publish_url", "published_at", "created", "updated", "tags",
	},
	"note": {"title", "type", "created", "updated", "tags"},
}

var storySectionMatrix = map[string][]string{
	"always":   {"## Problem", "## Acceptance criteria", "## Plan", "## Verification plan", "## Work log", "## Agent handoff"},
	"low":      {},
	"medium":   {"## Canon", "## Code anchors", "## Evidence"},
	"high":     {"## Canon", "## Code anchors", "## Considered and rejected", "## Decision", "## Evidence", "## Rollout"},
	"critical": {"## Canon", "## Code anchors", "## Considered and rejected", "## Decision", "## Evidence", "## Kill list", "## Rollout"},
}

var (
	bugSections  = []string{"## Summary", "## Repro", "## Environment", "## Root cause", "## Fix", "## Verification plan", "## Evidence", "## Work log", "## Agent handoff"}
	epicSections = []string{"## Problem", "## Scope and non-goals", "## Success criteria", "## Design", "## Stories", "## Open questions"}
)

type parsedID struct {
	Kind     string
	Acronym  string
	Sequence int
}

type validationContext struct {
	RelativePath string
	Basename     string
	EpicAcronyms map[string]struct{}
	StoryCounts  map[string]int
	NoteIDs      map[string]struct{}
	IDToRecordID map[string]string
	RecordIDs    map[string]struct{}
}

func parseID(id string) *parsedID {
	if id == "" {
		return nil
	}
	if epicAcronymPattern.MatchString(id) {
		return &parsedID{Kind: "epic", Acronym: id}
	}
	if match := storyIDPattern.FindStringSubmatch(id); match != nil {
		return &parsedID{Kind: "story", Acronym: match[1], Sequence: atoiSafe(match[2])}
	}
	if match := bugIDPattern.FindStringSubmatch(id); match != nil {
		return &parsedID{Kind: "bug", Acronym: match[1], Sequence: atoiSafe(match[2])}
	}
	if match := docIDPattern.FindStringSubmatch(id); match != nil {
		return &parsedID{Kind: "doc", Acronym: match[1], Sequence: atoiSafe(match[2])}
	}
	return nil
}

func requiredStorySections(risk string) []string {
	out := append([]string{}, storySectionMatrix["always"]...)
	out = append(out, storySectionMatrix[strings.ToLower(risk)]...)
	return out
}

type attestationRule struct {
	NeedsHuman      bool
	NeedsSignoff    bool
	MinAttestations int
}

func attestationRequirement(risk string) attestationRule {
	switch strings.ToLower(risk) {
	case "low", "medium":
		return attestationRule{MinAttestations: 1}
	case "high":
		return attestationRule{NeedsHuman: true, MinAttestations: 1}
	case "critical":
		return attestationRule{NeedsHuman: true, NeedsSignoff: true, MinAttestations: 1}
	default:
		return attestationRule{NeedsHuman: true, MinAttestations: 1}
	}
}

func normalizeList(value any) []string {
	switch v := value.(type) {
	case nil:
		return nil
	case []string:
		return filterStrings(v)
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			s := strings.TrimSpace(toString(item))
			if s != "" {
				out = append(out, s)
			}
		}
		return out
	case string:
		if strings.TrimSpace(v) == "" {
			return nil
		}
		return []string{strings.TrimSpace(v)}
	default:
		s := strings.TrimSpace(toString(value))
		if s == "" {
			return nil
		}
		return []string{s}
	}
}

func wikiTarget(value any) string {
	str := strings.TrimSpace(toString(value))
	if str == "" {
		return ""
	}
	match := regexp.MustCompile(`^\[\[([^\]]+)\]\]$`).FindStringSubmatch(str)
	if match == nil {
		return str
	}
	base := strings.Split(match[1], "|")[0]
	base = strings.Split(base, "#")[0]
	return strings.TrimSpace(base)
}

type headingPos struct {
	Index     int
	NextIndex int
}

func findHeading(body, heading string) *headingPos {
	target := strings.TrimSpace(heading)
	lines := strings.Split(body, "\n")
	for i, line := range lines {
		if strings.TrimSpace(line) != target {
			continue
		}
		next := len(lines)
		for j := i + 1; j < len(lines); j++ {
			if matched, _ := regexp.MatchString(`^#{2,6}\s+`, lines[j]); matched {
				next = j
				break
			}
		}
		return &headingPos{Index: i, NextIndex: next}
	}
	return nil
}

func sectionHasSubstance(body, heading string) bool {
	pos := findHeading(body, heading)
	if pos == nil {
		return false
	}
	lines := strings.Split(body, "\n")[pos.Index+1 : pos.NextIndex]
	text := strings.Join(lines, "\n")
	cleaned := regexp.MustCompile(`<!--[\s\S]*?-->`).ReplaceAllString(text, "")
	cleaned = regexp.MustCompile(`\b(TODO|TBD|FIXME)\b`).ReplaceAllString(cleaned, "")
	cleaned = regexp.MustCompile(`<[^>]+>`).ReplaceAllString(cleaned, "")
	compact := strings.TrimSpace(regexp.MustCompile(`\s+`).ReplaceAllString(cleaned, " "))
	if len(compact) >= 40 {
		return true
	}
	bullets := 0
	for _, line := range lines {
		if matched, _ := regexp.MatchString(`^\s*[-*+]\s+`, line); matched {
			bullets++
		}
	}
	if bullets >= 3 {
		return true
	}
	if strings.Contains(text, "```") {
		return true
	}
	if regexp.MustCompile(`\[\[.+?\]\]`).MatchString(text) {
		return true
	}
	if regexp.MustCompile(`!\[.*?\]\(.+?\)`).MatchString(text) {
		return true
	}
	if regexp.MustCompile(`\[.+?\]\(.+?\)`).MatchString(text) {
		return true
	}
	return false
}

func hasAgentHandoffMarker(body string) bool {
	lines := strings.Split(body, "\n")
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) != "## Agent handoff" {
			continue
		}
		for j := i - 1; j >= 0; j-- {
			prev := strings.TrimSpace(lines[j])
			if prev == "" {
				continue
			}
			if matched, _ := regexp.MatchString(`^---+$|^\*\*\*+$|^___+$`, prev); matched {
				return true
			}
			return false
		}
	}
	return false
}

func isUISurface(surfaces any) bool {
	for _, surface := range normalizeList(surfaces) {
		if _, ok := uiSurfaces[strings.ToLower(surface)]; ok {
			return true
		}
	}
	return false
}

func evidenceHasAsset(body string) bool {
	pos := findHeading(body, "## Evidence")
	if pos == nil {
		return false
	}
	lines := strings.Split(body, "\n")[pos.Index+1 : pos.NextIndex]
	return assetLinkRegex.MatchString(strings.Join(lines, "\n"))
}

func validateNote(note Note, ctx validationContext) ([]Issue, []Issue) {
	var errors []Issue
	var warnings []Issue
	data := note.Data
	body := note.Body
	id := stringField(data, "id")
	noteType := stringField(data, "type")
	where := ctx.RelativePath
	if where == "" {
		where = ctx.Basename
	}
	if where == "" {
		where = id
	}
	if where == "" {
		where = "<unknown>"
	}

	if _, ok := noteTypes[noteType]; !ok {
		errors = append(errors, issue(errorUnknownType, fmt.Sprintf(`unknown type "%s"`, noteType), where, "", nil))
		return errors, warnings
	}

	requiredFields := []string{"title", "created", "updated"}
	if noteType != "note" {
		requiredFields = []string{"schema_version", "record_id", "title", "id", "status", "created", "updated"}
	}
	for _, field := range requiredFields {
		if field == "schema_version" {
			if intField(data, "schema_version") != 2 {
				errors = append(errors, issue(errorMissingField, `missing or invalid frontmatter "schema_version" (expected 2)`, where, "", map[string]any{"field": "schema_version", "value": data["schema_version"]}))
			}
			continue
		}
		if stringField(data, field) == "" {
			errors = append(errors, issue(errorMissingField, fmt.Sprintf(`missing required frontmatter "%s"`, field), where, "", map[string]any{"field": field}))
		}
	}
	if noteType != "note" {
		if !recordIDPattern.MatchString(stringField(data, "record_id")) {
			errors = append(errors, issue(errorInvalidField, fmt.Sprintf(`record_id "%s" is not a valid ULID`, stringField(data, "record_id")), where, "", map[string]any{"field": "record_id", "value": stringField(data, "record_id")}))
		}
	}

	parsed := parseID(id)
	if parsed == nil {
		if noteType != "note" {
			errors = append(errors, issue(errorIDScheme, fmt.Sprintf(`id "%s" does not match the v1 scheme`, id), where, `story/bug/doc ids look like ABC-S-0001 / ABC-B-0001 / ABC-D-0001; epic ids are 3 uppercase letters`, map[string]any{"id": id}))
		}
	} else if parsed.Kind != noteType {
		errors = append(errors, issue(errorIDKindMismatch, fmt.Sprintf(`id kind "%s" does not match type "%s"`, parsed.Kind, noteType), where, "", map[string]any{"id": id, "type": noteType}))
	}

	if parsed != nil && ctx.RelativePath != "" {
		expected := fmt.Sprintf("epics/%s/index.md", parsed.Acronym)
		if parsed.Kind != "epic" {
			expected = fmt.Sprintf("epics/%s/%s.md", parsed.Acronym, id)
		}
		if ctx.RelativePath != expected {
			errors = append(errors, issue(errorPathMismatch, fmt.Sprintf(`file at "%s" declares id "%s" but path does not match expected "%s"`, ctx.RelativePath, id, expected), where, "move the file to the expected path or correct the id", map[string]any{"expected": expected, "actual": ctx.RelativePath, "id": id}))
		}
	}

	switch noteType {
	case "epic":
		if _, ok := epicStatuses[stringField(data, "status")]; !ok {
			errors = append(errors, issue(errorInvalidField, fmt.Sprintf(`invalid epic status "%s"`, stringField(data, "status")), where, "", map[string]any{"field": "status", "value": stringField(data, "status")}))
		}
		if stringField(data, "owner") == "" {
			errors = append(errors, issue(errorMissingField, "epic missing owner", where, "", map[string]any{"field": "owner"}))
		}
		summary := strings.TrimSpace(stringField(data, "summary"))
		if summary == "" {
			entry := issue(errorMissingField, "epic missing summary (one-line, <=120 chars, surfaces in the routing index)", where, "", map[string]any{"field": "summary"})
			if stringField(data, "status") == "active" {
				errors = append(errors, entry)
			} else {
				warnings = append(warnings, entry)
			}
		} else if len(summary) > 120 {
			errors = append(errors, issue(errorInvalidField, fmt.Sprintf("epic summary is %d chars; keep it <=120", len(summary)), where, "", map[string]any{"field": "summary", "length": len(summary)}))
		}
		hasSpec := strings.TrimSpace(stringField(data, "spec_source")) != ""
		hasDesign := sectionHasSubstance(body, "## Design")
		if !hasSpec && !hasDesign {
			errors = append(errors, issue(errorLacksSubstance, `epic must have non-empty spec_source OR a "## Design" section with substance`, where, "", nil))
		}
		if stringField(data, "status") == "active" && (hasSpec || hasDesign) && ctx.StoryCounts[stringField(data, "id")] == 0 {
			errors = append(errors, issue(errorEpicStoryStack, `active epic declares canon but has zero stories`, where, "create at least one story before treating the epic as active", map[string]any{"epic": stringField(data, "id")}))
		}
		for _, section := range epicSections {
			if findHeading(body, section) == nil {
				errors = append(errors, issue(errorMissingSection, fmt.Sprintf(`epic missing required section "%s"`, section), where, "", map[string]any{"section": section}))
				continue
			}
			if section == "## Design" || section == "## Stories" {
				continue
			}
			if !sectionHasSubstance(body, section) {
				errors = append(errors, issue(errorLacksSubstance, fmt.Sprintf(`epic section "%s" lacks substance`, section), where, "", map[string]any{"section": section}))
			}
		}
	case "story", "bug":
		if _, ok := storyStatuses[stringField(data, "status")]; !ok {
			errors = append(errors, issue(errorInvalidField, fmt.Sprintf(`invalid %s status "%s"`, noteType, stringField(data, "status")), where, "", map[string]any{"field": "status", "value": stringField(data, "status")}))
		}
		if _, ok := reviewStates[stringField(data, "review_state")]; !ok {
			errors = append(errors, issue(errorInvalidField, fmt.Sprintf(`invalid or missing review_state "%s"`, stringField(data, "review_state")), where, "", map[string]any{"field": "review_state", "value": stringField(data, "review_state")}))
		}
		if intField(data, "work_revision") < 0 {
			errors = append(errors, issue(errorInvalidField, fmt.Sprintf(`invalid work_revision "%v"`, data["work_revision"]), where, "", map[string]any{"field": "work_revision", "value": data["work_revision"]}))
		}
		if _, ok := changeTypes[stringField(data, "change_type")]; !ok {
			errors = append(errors, issue(errorInvalidField, fmt.Sprintf(`invalid or missing change_type "%s"`, stringField(data, "change_type")), where, "", map[string]any{"field": "change_type", "value": stringField(data, "change_type")}))
		}
		if _, ok := sizes[stringField(data, "size")]; !ok {
			errors = append(errors, issue(errorInvalidField, fmt.Sprintf(`invalid or missing size "%s"`, stringField(data, "size")), where, "", map[string]any{"field": "size", "value": stringField(data, "size")}))
		}
		if _, ok := risks[stringField(data, "risk")]; !ok {
			errors = append(errors, issue(errorInvalidField, fmt.Sprintf(`invalid or missing risk "%s"`, stringField(data, "risk")), where, "", map[string]any{"field": "risk", "value": stringField(data, "risk")}))
		}
		if _, ok := priorities[stringField(data, "priority")]; !ok {
			errors = append(errors, issue(errorInvalidField, fmt.Sprintf(`invalid or missing priority "%s"`, stringField(data, "priority")), where, "", map[string]any{"field": "priority", "value": stringField(data, "priority")}))
		}
		if _, ok := delegations[stringField(data, "delegation")]; !ok {
			errors = append(errors, issue(errorInvalidField, fmt.Sprintf(`invalid or missing delegation "%s"`, stringField(data, "delegation")), where, "", map[string]any{"field": "delegation", "value": stringField(data, "delegation")}))
		}
		if _, ok := aiAssistanceLevels[stringField(data, "ai_assistance")]; !ok {
			errors = append(errors, issue(errorInvalidField, fmt.Sprintf(`invalid or missing ai_assistance "%s"`, stringField(data, "ai_assistance")), where, "", map[string]any{"field": "ai_assistance", "value": stringField(data, "ai_assistance")}))
		}
		if stringField(data, "requester") == "" {
			errors = append(errors, issue(errorMissingField, "missing requester", where, "", map[string]any{"field": "requester"}))
		}
		for _, legacyField := range []string{"dispatch_state", "claimed_by", "claimed_at", "run_attempts", "last_attempt_at", "failure_class"} {
			if _, ok := data[legacyField]; ok {
				errors = append(errors, issue(errorInvalidField, fmt.Sprintf(`legacy runtime field "%s" is not allowed in canonical v2 notes`, legacyField), where, "remove the field or run the schema migration", map[string]any{"field": legacyField}))
			}
		}
		epicRef := wikiTarget(data["epic"])
		if epicRef == "" {
			errors = append(errors, issue(errorOrphanWork, fmt.Sprintf("%s has no epic — orphans are not allowed", noteType), where, "", map[string]any{"field": "epic"}))
		} else {
			if _, ok := ctx.EpicAcronyms[epicRef]; !ok {
				errors = append(errors, issue(errorUnknownEpic, fmt.Sprintf(`epic "%s" does not exist`, epicRef), where, "", map[string]any{"epic": epicRef}))
			} else if parsed != nil && parsed.Acronym != epicRef {
				errors = append(errors, issue(errorEpicAcronymMismatch, fmt.Sprintf(`id acronym "%s" does not match epic "%s"`, parsed.Acronym, epicRef), where, "", map[string]any{"idAcronym": parsed.Acronym, "epic": epicRef}))
			}
			if want := ctx.IDToRecordID[epicRef]; want != "" && stringField(data, "epic_record_id") != want {
				warnings = append(warnings, issue(errorInvalidField, fmt.Sprintf(`epic_record_id does not match epic "%s"`, epicRef), where, "run `tusker reindex --fix-links` to refresh record-id mirrors", map[string]any{"field": "epic_record_id", "value": stringField(data, "epic_record_id"), "expected": want}))
			}
		}
		validateListRecordIDMirror(data, "related", "related_record_ids", ctx, where, &errors, &warnings)
		validateListRecordIDMirror(data, "blocks", "blocks_record_ids", ctx, where, &errors, &warnings)
		validateListRecordIDMirror(data, "blocked_by", "blocked_by_record_ids", ctx, where, &errors, &warnings)
		if status := stringField(data, "status"); status == "rework" && stringField(data, "review_state") != "changes_requested" {
			errors = append(errors, issue(errorInvalidTransition, `status "rework" requires review_state "changes_requested"`, where, "", nil))
		}
		if status := stringField(data, "status"); status == "in_review" && stringField(data, "review_state") != "verification_requested" && stringField(data, "review_state") != "requested" && stringField(data, "review_state") != "approved" {
			errors = append(errors, issue(errorInvalidTransition, `status "in_review" requires review_state "verification_requested", "requested", or "approved"`, where, "", nil))
		}
		if status := stringField(data, "status"); status == "done" && stringField(data, "review_state") != "approved" {
			errors = append(errors, issue(errorInvalidTransition, `status "done" requires review_state "approved"`, where, "", nil))
		}

		required := bugSections
		if noteType == "story" {
			required = requiredStorySections(stringField(data, "risk"))
		}
		for _, section := range required {
			if findHeading(body, section) == nil {
				errors = append(errors, issue(errorMissingSection, fmt.Sprintf(`%s missing required section "%s"`, noteType, section), where, "", map[string]any{"section": section}))
				continue
			}
			if section == "## Agent handoff" || section == "## Work log" || section == "## Evidence" {
				continue
			}
			if !sectionHasSubstance(body, section) {
				warnings = append(warnings, issue(errorLacksSubstance, fmt.Sprintf(`section "%s" lacks substance`, section), where, "", map[string]any{"section": section}))
			}
		}
		if !hasAgentHandoffMarker(body) {
			errors = append(errors, issue(errorMissingHandoffMarker, `missing "---" horizontal rule before "## Agent handoff"`, where, "", nil))
		}
		if status := stringField(data, "status"); status == "in_review" || status == "done" {
			risk := strings.ToLower(stringField(data, "risk"))
			if risk == "medium" || risk == "high" || risk == "critical" {
				if !sectionHasSubstance(body, "## Evidence") {
					errors = append(errors, issue(errorEvidenceGate, fmt.Sprintf(`status "%s" requires substantive "## Evidence" for risk "%s"`, status, risk), where, "", map[string]any{"status": status, "risk": risk}))
				}
			}
			if noteType == "story" && stringField(data, "change_type") == "feature" && isUISurface(data["surfaces"]) && (risk == "medium" || risk == "high" || risk == "critical") {
				if !evidenceHasAsset(body) {
					errors = append(errors, issue(errorUIDemoMissing, fmt.Sprintf(`UI feature at risk "%s" requires a demo asset (video/gif/screenshot) in "## Evidence"`, risk), where, "", map[string]any{"risk": risk}))
				}
			}
		}
		if stringField(data, "status") == "done" {
			rule := attestationRequirement(stringField(data, "risk"))
			if stringField(data, "attested_by") == "" || stringField(data, "attested_at") == "" || stringField(data, "attested_role") == "" {
				errors = append(errors, issue(errorAttestationMissing, `status "done" requires attested_by, attested_at, attested_role`, where, "", nil))
			} else if rule.NeedsHuman && stringField(data, "attested_role") != "human" {
				errors = append(errors, issue(errorAttestationRole, fmt.Sprintf(`risk "%s" requires a human attestation`, stringField(data, "risk")), where, "", map[string]any{"risk": stringField(data, "risk"), "attested_role": stringField(data, "attested_role")}))
			}
			if rule.NeedsSignoff && (stringField(data, "signoff_by") == "" || stringField(data, "signoff_at") == "") {
				errors = append(errors, issue(errorSignoffMissing, `risk "critical" requires signoff_by and signoff_at`, where, "", nil))
			}
			if boolField(data, "dod_code_complete") == false {
				warnings = append(warnings, issue(errorLacksSubstance, `status "done" set while dod_code_complete is false`, where, "flip dod_code_complete to true or revisit status", nil))
			}
		}
	case "doc":
		if _, ok := docStatuses[stringField(data, "status")]; !ok {
			errors = append(errors, issue(errorInvalidField, fmt.Sprintf(`invalid doc status "%s"`, stringField(data, "status")), where, "", map[string]any{"field": "status", "value": stringField(data, "status")}))
		}
		if _, ok := docAudiences[stringField(data, "audience")]; !ok {
			errors = append(errors, issue(errorInvalidField, fmt.Sprintf(`invalid or missing audience "%s"`, stringField(data, "audience")), where, "", map[string]any{"field": "audience", "value": stringField(data, "audience")}))
		}
		epicRef := wikiTarget(data["epic"])
		if epicRef == "" {
			errors = append(errors, issue(errorOrphanWork, "doc has no epic parent", where, "", map[string]any{"field": "epic"}))
		} else if _, ok := ctx.EpicAcronyms[epicRef]; !ok {
			errors = append(errors, issue(errorUnknownEpic, fmt.Sprintf(`epic "%s" does not exist`, epicRef), where, "", map[string]any{"epic": epicRef}))
		} else if want := ctx.IDToRecordID[epicRef]; want != "" && stringField(data, "epic_record_id") != want {
			warnings = append(warnings, issue(errorInvalidField, fmt.Sprintf(`epic_record_id does not match epic "%s"`, epicRef), where, "run `tusker reindex --fix-links` to refresh record-id mirrors", map[string]any{"field": "epic_record_id", "value": stringField(data, "epic_record_id"), "expected": want}))
		}
		storyRef := wikiTarget(data["story"])
		docIntent := strings.TrimSpace(stringField(data, "doc_intent"))
		canonFor := wikiTarget(data["canon_for"])
		ownerEpic := wikiTarget(data["owner_epic"])
		canonical := boolField(data, "canonical")
		canonicalStatus := strings.ToLower(strings.TrimSpace(stringField(data, "canonical_status")))
		if docIntent != "" {
			if _, ok := docIntents[docIntent]; !ok {
				errors = append(errors, issue(errorInvalidField, fmt.Sprintf(`invalid doc_intent "%s"`, docIntent), where, "", map[string]any{"field": "doc_intent", "value": docIntent}))
			}
		}
		if canonicalStatus != "" {
			if _, ok := canonicalStatuses[canonicalStatus]; !ok {
				errors = append(errors, issue(errorInvalidField, fmt.Sprintf(`invalid canonical_status "%s"`, canonicalStatus), where, `use draft, approved, deprecated, or historical`, map[string]any{"field": "canonical_status", "value": canonicalStatus}))
			}
		}
		if canonical && canonicalStatus == "" {
			errors = append(errors, issue(errorMissingField, `canonical docs must set canonical_status`, where, `use draft, approved, deprecated, or historical`, map[string]any{"field": "canonical_status"}))
		}
		if canonicalStatus == "approved" && strings.TrimSpace(stringField(data, "verified_at")) == "" {
			warnings = append(warnings, issue(errorMissingField, `approved canonical doc should set verified_at`, where, "stamp when the doc was last checked against implementation", map[string]any{"field": "verified_at"}))
		}
		if (boolField(data, "deprecated") || canonicalStatus == "deprecated") && strings.TrimSpace(stringField(data, "superseded_by")) == "" {
			warnings = append(warnings, issue(errorMissingField, `deprecated doc should set superseded_by`, where, "point agents at the replacement route or source path", map[string]any{"field": "superseded_by"}))
		}
		if docIntent == "canon" && storyRef != "" {
			errors = append(errors, issue(errorInvalidField, `doc_intent "canon" cannot also set story`, where, "use --canon-for for canonical docs and leave story empty", map[string]any{"field": "story", "value": storyRef}))
		}
		if docIntent == "companion" && canonFor != "" {
			errors = append(errors, issue(errorInvalidField, `doc_intent "companion" cannot also set canon_for`, where, "use --companion-to for companion docs and leave canon_for empty", map[string]any{"field": "canon_for", "value": canonFor}))
		}
		if stringField(data, "audience") == "developer" {
			if docIntent == "" {
				errors = append(errors, issue(errorMissingField, `developer doc must declare doc_intent`, where, "create with --canon-for <EPIC> or --companion-to <STORY-ID>", map[string]any{"field": "doc_intent"}))
			}
			if docIntent == "canon" && canonFor == "" {
				errors = append(errors, issue(errorMissingField, `developer canon doc must set canon_for`, where, "create with --canon-for <EPIC>", map[string]any{"field": "canon_for"}))
			}
			if docIntent == "companion" && storyRef == "" {
				errors = append(errors, issue(errorMissingField, `developer companion doc must point at a story`, where, "create with --companion-to <STORY-ID>", map[string]any{"field": "story"}))
			}
		}
		if canonFor != "" {
			if _, ok := ctx.EpicAcronyms[canonFor]; !ok {
				errors = append(errors, issue(errorUnknownEpic, fmt.Sprintf(`canon_for epic "%s" does not exist`, canonFor), where, "", map[string]any{"canon_for": canonFor}))
			}
			if epicRef != "" && canonFor != epicRef {
				errors = append(errors, issue(errorInvalidField, fmt.Sprintf(`canon_for "%s" must match parent epic "%s"`, canonFor, epicRef), where, "keep canonical docs under the epic they define", map[string]any{"field": "canon_for", "value": canonFor, "epic": epicRef}))
			}
		}
		if ownerEpic != "" {
			if _, ok := ctx.EpicAcronyms[ownerEpic]; !ok {
				errors = append(errors, issue(errorUnknownEpic, fmt.Sprintf(`owner_epic "%s" does not exist`, ownerEpic), where, "", map[string]any{"owner_epic": ownerEpic}))
			}
			if epicRef != "" && ownerEpic != epicRef {
				warnings = append(warnings, issue(errorInvalidField, fmt.Sprintf(`owner_epic "%s" differs from parent epic "%s"`, ownerEpic, epicRef), where, "only diverge when the published doc intentionally belongs to another epic's canon", map[string]any{"field": "owner_epic", "value": ownerEpic, "epic": epicRef}))
			}
		}
		if storyRef != "" {
			if parsed := parseID(storyRef); parsed == nil || parsed.Kind != "story" {
				errors = append(errors, issue(errorInvalidField, fmt.Sprintf(`story "%s" must be a story id`, storyRef), where, "", map[string]any{"story": storyRef}))
			}
			if want := ctx.IDToRecordID[storyRef]; want == "" {
				errors = append(errors, issue(errorNotFound, fmt.Sprintf(`story "%s" does not exist`, storyRef), where, "", map[string]any{"story": storyRef}))
			} else if stringField(data, "story_record_id") != want {
				warnings = append(warnings, issue(errorInvalidField, fmt.Sprintf(`story_record_id does not match story "%s"`, storyRef), where, "run `tusker reindex --fix-links` to refresh record-id mirrors", map[string]any{"field": "story_record_id", "value": stringField(data, "story_record_id"), "expected": want}))
			}
			if parsed := parseID(storyRef); parsed != nil && epicRef != "" && parsed.Acronym != epicRef {
				errors = append(errors, issue(errorEpicAcronymMismatch, fmt.Sprintf(`story "%s" does not belong to epic "%s"`, storyRef, epicRef), where, "", map[string]any{"story": storyRef, "epic": epicRef}))
			}
		}
		publish := boolField(data, "publish")
		publishPath := strings.TrimSpace(stringField(data, "publish_path"))
		if publish {
			if _, ok := publishableDocStatuses[stringField(data, "status")]; !ok {
				errors = append(errors, issue(errorPublishStatusInvalid, `publish: true requires doc status "approved" or "published"`, where, "move the doc to approved/published before exporting it", map[string]any{"field": "status", "value": stringField(data, "status")}))
			}
			if publishPath == "" {
				errors = append(errors, issue(errorPublishPathMissing, `publish: true requires publish_path`, where, "", map[string]any{"field": "publish_path"}))
			}
			if strings.TrimSpace(stringField(data, "publish_description")) == "" {
				errors = append(errors, issue(errorPublishDescriptionMissing, `publish: true requires publish_description`, where, "", map[string]any{"field": "publish_description"}))
			}
		}
		if publishPath != "" {
			if reason := validatePublishPath(publishPath); reason != "" {
				errors = append(errors, issue(errorPublishPathInvalid, fmt.Sprintf(`invalid publish_path "%s": %s`, publishPath, reason), where, "", map[string]any{"field": "publish_path", "value": publishPath}))
			}
		}
		for _, redirect := range normalizeList(data["redirect_from"]) {
			redirect = strings.TrimSpace(redirect)
			if redirect == "" {
				continue
			}
			if reason := validatePublishPath(redirect); reason != "" {
				errors = append(errors, issue(errorPublishPathInvalid, fmt.Sprintf(`invalid redirect_from "%s": %s`, redirect, reason), where, "", map[string]any{"field": "redirect_from", "value": redirect}))
			}
			if publishPath != "" && docsNormalizeRouteValue(redirect) == docsNormalizeRouteValue(publishPath) {
				errors = append(errors, issue(errorInvalidField, fmt.Sprintf(`redirect_from "%s" must not equal publish_path`, redirect), where, "", map[string]any{"field": "redirect_from", "value": redirect}))
			}
		}
		if value, ok := data["publish_order"]; ok && !isIntegerValue(value) {
			errors = append(errors, issue(errorPublishOrderInvalid, `publish_order must be an integer when set`, where, "", map[string]any{"field": "publish_order", "value": value}))
		}
		if _, ok := data["publish_section_title"]; ok && strings.TrimSpace(stringField(data, "publish_section_title")) == "" {
			errors = append(errors, issue(errorPublishSectionTitleInvalid, `publish_section_title must be non-empty when set`, where, "", map[string]any{"field": "publish_section_title"}))
		}
		if stringField(data, "status") == "published" && stringField(data, "published_at") == "" {
			warnings = append(warnings, issue(errorLacksSubstance, `doc status "published" should set published_at`, where, "use `tusker set-status --status published` or set published_at explicitly", nil))
		}
	}
	return errors, warnings
}

func validateListRecordIDMirror(data map[string]any, linkField, mirrorField string, ctx validationContext, where string, errors, warnings *[]Issue) {
	links := normalizeList(data[linkField])
	recordIDs := normalizeList(data[mirrorField])
	expected := make([]string, 0, len(links))
	for _, link := range links {
		target := wikiTarget(link)
		if target == "" {
			expected = append(expected, "")
			continue
		}
		recordID, ok := ctx.IDToRecordID[target]
		if !ok {
			current := issue(errorInvalidField, fmt.Sprintf(`%s link "%s" does not resolve to a known note`, linkField, target), where, "", map[string]any{"field": linkField, "target": target})
			*errors = append(*errors, current)
			return
		}
		expected = append(expected, recordID)
	}
	if stringSlicesEqual(recordIDs, expected) {
		return
	}
	current := issue(errorInvalidField, fmt.Sprintf(`%s is out of sync with %s`, mirrorField, linkField), where, "run `tusker reindex --fix-links` to refresh record-id mirrors", map[string]any{"field": mirrorField, "expected": expected, "value": recordIDs})
	*warnings = append(*warnings, current)
}

func frontmatterOrderForType(noteType string) []string {
	if order, ok := frontmatterOrder[noteType]; ok {
		return order
	}
	return frontmatterOrder["note"]
}

func managedNoteType(noteType string) bool {
	return noteType == "epic" || noteType == "story" || noteType == "bug" || noteType == "doc"
}

func sanitizeCanonicalNoteData(data map[string]any) {
	noteType := stringField(data, "type")
	if !managedNoteType(noteType) {
		return
	}
	for _, legacyField := range []string{"dispatch_state", "claimed_by", "claimed_at", "run_attempts", "last_attempt_at", "failure_class"} {
		delete(data, legacyField)
	}
}

func makeSet(values ...string) map[string]struct{} {
	out := make(map[string]struct{}, len(values))
	for _, value := range values {
		out[value] = struct{}{}
	}
	return out
}

func filterStrings(values []string) []string {
	var out []string
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			out = append(out, strings.TrimSpace(value))
		}
	}
	return out
}

func validatePublishPath(value string) string {
	if strings.HasPrefix(value, "/") {
		return "must not start with /"
	}
	if strings.HasSuffix(value, "/") {
		return "must not end with /"
	}
	segments := strings.Split(value, "/")
	if len(segments) == 0 || segments[0] == "" {
		return "must include a top-level segment"
	}
	if _, ok := publicationLanes[segments[0]]; !ok {
		return "top-level segment must be one of developer, user, release-notes, support, internal"
	}
	for _, segment := range segments {
		if segment == "" {
			return "must not contain empty segments"
		}
		if segment == "." || segment == ".." {
			return `must not contain "." or ".." segments`
		}
	}
	if segments[len(segments)-1] == "index" {
		return `final segment must not be "index"`
	}
	return ""
}

func isIntegerValue(value any) bool {
	switch current := value.(type) {
	case int, int8, int16, int32, int64:
		return true
	case uint, uint8, uint16, uint32, uint64:
		return true
	case float64:
		return current == float64(int(current))
	case string:
		return integerStringPattern.MatchString(strings.TrimSpace(current))
	default:
		return false
	}
}

func posixRelative(root, full string) string {
	return filepath.ToSlash(strings.TrimPrefix(strings.TrimPrefix(full, root), string(filepath.Separator)))
}
