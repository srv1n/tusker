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
	errorUIDemoMissing              = "UI_DEMO_MISSING"
	errorEvidenceGate               = "EVIDENCE_GATE"
	errorOrphanWork                 = "ORPHAN_WORK"
	errorUnknownEpic                = "UNKNOWN_EPIC"
	errorEpicAcronymMismatch        = "EPIC_ACRONYM_MISMATCH"
	errorChildrenUnfinished         = "CHILDREN_UNFINISHED"
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
	errorUnknownDocNode             = "UNKNOWN_DOC_NODE"
	errorUnknownDomain              = "UNKNOWN_DOMAIN"
	errorDocsImpactUnresolved       = "DOCS_IMPACT_UNRESOLVED"
	errorMissingKnowledgeDelta      = "MISSING_KNOWLEDGE_DELTA"
)

var (
	noteTypes              = makeSet("epic", "task", "doc", "note")
	taskStatuses           = makeSet("draft", "backlog", "ready", "active", "blocked", "review", "rework", "done", "cancelled")
	docStatuses            = makeSet("draft", "review", "approved", "published", "archived")
	publishableDocStatuses = makeSet("approved", "published")
	docIntents             = makeSet("canon", "companion")
	canonicalStatuses      = makeSet("draft", "approved", "deprecated", "historical")
	changeTypes            = makeSet("feature", "bug", "refactor", "migration", "security", "docs", "chore", "research", "incident")
	sizes                  = makeSet("s", "m", "l", "xl")
	risks                  = makeSet("low", "medium", "high", "critical")
	priorities             = makeSet("p0", "p1", "p2", "p3")
	delegations            = makeSet("execute", "explore", "escalate")
	aiAssistanceLevels     = makeSet("none", "light", "moderate", "heavy")
	docAudiences           = makeSet("developer", "user", "operator", "support", "release", "agent", "internal")
	docModes               = makeSet("tutorial", "how-to", "reference", "explanation")
	docAgentLayers         = makeSet("none", "capsule", "standalone")
	publicationLanes       = makeSet("developer", "user", "release-notes", "support", "internal")
	uiSurfaces             = makeSet("frontend", "desktop", "mobile")
	epicAcronymPattern     = regexp.MustCompile(`^[A-Z]{3}$`)
	taskIDPattern          = regexp.MustCompile(`^([A-Z]{3})-T-(\d{4})$`)
	docIDPattern           = regexp.MustCompile(`^([A-Z]{3})-D-(\d{4})$`)
	recordIDPattern        = regexp.MustCompile(`^[0-9A-HJKMNP-TV-Z]{26}$`)
	assetLinkRegex         = regexp.MustCompile(`(!\[.*?\]\(.+?\))|(!\[\[.+?\]\])|(\[.+?\]\((https?:\/\/\S+?)\))`)
	integerStringPattern   = regexp.MustCompile(`^-?\d+$`)
)

var statusTransitionDateFields = map[string]string{
	"active":    "started",
	"review":    "review_requested_at",
	"done":      "completed",
	"cancelled": "cancelled_at",
	"blocked":   "blocked_since",
}

var frontmatterOrder = map[string][]string{
	"epic": {
		"schema", "id", "title", "type", "status", "owner", "summary", "doc_nodes",
		"created", "updated", "started", "blocked_since", "completed", "cancelled_at", "transitions", "tags",
	},
	"task": {
		"schema", "id", "title", "type", "kind", "epic", "status", "priority", "risk", "size",
		"delegation", "ai_assistance", "ai_tools", "assignee", "domains", "doc_nodes", "knowledge_nodes", "blocked_by", "block_reason", "blocks", "created", "updated", "started",
		"review_requested_at", "completed", "cancelled_at", "blocked_since", "verified_by", "verified_at",
		"verification_summary", "closed_by", "closed_at", "close_summary", "docs_resolution", "knowledge_resolution", "transitions", "tags",
	},
	"doc": {
		"schema", "id", "title", "type", "node", "status", "epic", "doc_intent", "canon_for",
		"audience", "mode", "agent_layer", "kind", "domains", "source_of_truth", "stale_when_paths", "canonical_status", "last_verified_at", "owner_epic", "verified_at", "deprecated", "superseded_by",
		"publish", "publish_lane", "publish_path", "publish_description", "publish_order", "publish_section_title", "redirect_from", "publish_url", "published_at", "created", "updated", "tags",
	},
	"note": {"title", "type", "created", "updated", "tags"},
}

type parsedID struct {
	Kind     string
	Acronym  string
	Sequence int
}

type validationContext struct {
	RelativePath     string
	Basename         string
	VaultPath        string
	EpicAcronyms     map[string]struct{}
	NoteIDs          map[string]struct{}
	IDToRecordID     map[string]string
	RecordIDs        map[string]struct{}
	DocsMap          *DocsMap
	V6Domains        map[string]bool
	V6KnowledgeNodes map[string]bool
	V6LinkTargets    map[string]bool
	V6Freshness      map[string]v6FreshnessRecord
}

func parseID(id string) *parsedID {
	if id == "" {
		return nil
	}
	if epicAcronymPattern.MatchString(id) {
		return &parsedID{Kind: "epic", Acronym: id}
	}
	if match := taskIDPattern.FindStringSubmatch(id); match != nil {
		return &parsedID{Kind: "task", Acronym: match[1], Sequence: atoiSafe(match[2])}
	}
	if match := docIDPattern.FindStringSubmatch(id); match != nil {
		return &parsedID{Kind: "doc", Acronym: match[1], Sequence: atoiSafe(match[2])}
	}
	return nil
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
	id := stringField(data, "id")
	noteType := stringField(data, "type")
	schema := stringField(data, "schema")
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

	if strings.HasSuffix(schema, "/v6") {
		return validateV6Note(note, ctx, where)
	}
	if _, ok := noteTypes[noteType]; !ok {
		errors = append(errors, issue(errorUnknownType, fmt.Sprintf(`unknown type "%s"`, noteType), where, "", nil))
		return errors, warnings
	}
	if noteType == "note" {
		for _, field := range []string{"title", "created", "updated"} {
			if stringField(data, field) == "" {
				errors = append(errors, issue(errorMissingField, fmt.Sprintf(`missing required frontmatter "%s"`, field), where, "", map[string]any{"field": field}))
			}
		}
		return errors, warnings
	}
	if !strings.HasSuffix(schema, "/v5") {
		errors = append(errors, issue(errorInvalidField, fmt.Sprintf(`%s is not a V5 or V6 note; current Tusker notes require schema "tusker.<type>/v5" or "tusker.<kind>/v6"`, where), where, "recreate the note with `tusker new` or `tusker knowledge new`", map[string]any{"field": "schema", "value": schema}))
		return errors, warnings
	}
	return validateV5Note(note, ctx, where)
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
	return noteType == "epic" || noteType == "task" || noteType == "doc"
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
