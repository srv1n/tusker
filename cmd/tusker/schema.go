package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
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
	if err == nil {
		return Issue{}
	}
	var typed *TuskerError
	if errors.As(err, &typed) && typed != nil {
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

// annotatePrimaryTuskerError preserves the first typed classification while
// replacing that leaf with an annotated clone. Every sibling leaf remains in
// the returned tree, so later operator and Serve formatters cannot silently
// discard a concurrent cleanup failure.
func annotatePrimaryTuskerError(err error, annotation string) error {
	if err == nil {
		return nil
	}
	var primary *TuskerError
	if !errors.As(err, &primary) {
		return fmt.Errorf("%w; %s", err, annotation)
	}
	cloned := *primary
	cloned.Message = strings.TrimSuffix(cloned.Message, ".") + "; " + annotation
	parts := []error{&cloned}
	for _, leaf := range flattenErrorLeaves(err) {
		if typed, ok := leaf.(*TuskerError); ok && typed == primary {
			continue
		}
		parts = append(parts, leaf)
	}
	if len(parts) == 1 {
		return parts[0]
	}
	return errors.Join(parts...)
}

// serveErrorIssue retains the primary typed code/hint/context and adds a
// deterministic, bounded, redacted rendering of every unique leaf cause.
// Absolute paths discovered only through error prose are suppressed; existing
// structured actionable path context remains untouched.
func serveErrorIssue(err error) Issue {
	if err == nil {
		return Issue{}
	}
	leaves := flattenErrorLeaves(err)
	var primary *TuskerError
	if errors.As(err, &primary) {
		issue := errorToIssue(primary)
		issue.Message = safeOperatorErrorText(issue.Message, 640)
		issue.Hint = safeOperatorErrorText(issue.Hint, 640)
		issue.Path = safeOperatorErrorText(issue.Path, 320)
		issue.Context = safeServeIssueContext(issue.Context, 0)
		return appendServeErrorDetails(issue, leaves, primary)
	}
	issue := Issue{Code: "UNKNOWN"}
	if len(leaves) == 0 {
		issue.Message = safeOperatorErrorText(err.Error(), 640)
		return issue
	}
	issue.Message = safeOperatorErrorLeaf(leaves[0])
	return appendServeErrorDetails(issue, leaves, nil)
}

func flattenErrorLeaves(err error) []error {
	const maxErrorTreeNodes = 256
	var (
		out     []error
		visited int
	)
	var walk func(error)
	walk = func(current error) {
		if current == nil || visited >= maxErrorTreeNodes {
			return
		}
		visited++
		switch wrapped := current.(type) {
		case interface{ Unwrap() []error }:
			children := wrapped.Unwrap()
			if len(children) == 0 {
				out = append(out, current)
				return
			}
			for _, child := range children {
				walk(child)
			}
		case interface{ Unwrap() error }:
			child := wrapped.Unwrap()
			if child == nil {
				out = append(out, current)
				return
			}
			walk(child)
		default:
			out = append(out, current)
		}
	}
	walk(err)
	return out
}

func appendServeErrorDetails(issue Issue, leaves []error, primary *TuskerError) Issue {
	const maxServeErrorLeaves = 16
	seen := map[string]struct{}{}
	var (
		allDetails     []string
		siblingDetails []string
	)
	for _, leaf := range leaves {
		detail := safeOperatorErrorLeaf(leaf)
		if detail == "" {
			continue
		}
		if _, exists := seen[detail]; exists {
			continue
		}
		seen[detail] = struct{}{}
		if len(allDetails) >= maxServeErrorLeaves {
			continue
		}
		allDetails = append(allDetails, detail)
		isPrimary := false
		if typed, ok := leaf.(*TuskerError); ok && primary != nil && typed == primary {
			isPrimary = true
		} else if primary == nil && len(allDetails) == 1 {
			isPrimary = true
		}
		if !isPrimary {
			siblingDetails = append(siblingDetails, detail)
		}
	}
	if len(siblingDetails) == 0 {
		return issue
	}
	issue.Message = safePacketText(issue.Message+"; additional causes: "+strings.Join(siblingDetails, "; "), 2048)
	context := serveIssueContextMap(issue.Context)
	if existing, present := context["error_chain"]; present {
		delete(context, "error_chain")
		context[uniqueServeIssueContextKey(context, "primary_error_chain")] = existing
	}
	context["error_chain"] = allDetails
	issue.Context = context
	return issue
}

func serveIssueContextMap(context any) map[string]any {
	if context == nil {
		return map[string]any{}
	}
	safe := safeServeIssueContext(context, 0)
	if typed, ok := safe.(map[string]any); ok {
		return typed
	}
	return map[string]any{"primary_context": safe}
}

func safeServeIssueContext(value any, depth int) any {
	const (
		maxDepth   = 6
		maxEntries = 32
	)
	if depth >= maxDepth {
		return "[truncated]"
	}
	switch typed := value.(type) {
	case nil, bool, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64, json.Number:
		return typed
	case string:
		return safeOperatorErrorText(typed, 320)
	case []string:
		limit := len(typed)
		if limit > maxEntries {
			limit = maxEntries
		}
		out := make([]any, 0, limit+1)
		for _, item := range typed[:limit] {
			out = append(out, safeServeIssueContext(item, depth+1))
		}
		if len(typed) > limit {
			out = append(out, "[truncated]")
		}
		return out
	case []any:
		limit := len(typed)
		if limit > maxEntries {
			limit = maxEntries
		}
		out := make([]any, 0, limit+1)
		for _, item := range typed[:limit] {
			out = append(out, safeServeIssueContext(item, depth+1))
		}
		if len(typed) > limit {
			out = append(out, "[truncated]")
		}
		return out
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		if len(keys) > maxEntries {
			keys = keys[:maxEntries]
		}
		out := make(map[string]any, len(keys)+1)
		for _, key := range keys {
			safeKey := safeOperatorErrorText(key, 80)
			if safeKey == "" {
				safeKey = "[redacted-key]"
			}
			out[uniqueServeIssueContextKey(out, safeKey)] = safeServeIssueContext(typed[key], depth+1)
		}
		if len(typed) > len(keys) {
			out[uniqueServeIssueContextKey(out, "_truncated")] = true
		}
		return out
	default:
		raw, err := json.Marshal(typed)
		if err != nil {
			return safeOperatorErrorText(fmt.Sprint(typed), 320)
		}
		var decoded any
		if json.Unmarshal(raw, &decoded) != nil {
			return "[unavailable]"
		}
		return safeServeIssueContext(decoded, depth+1)
	}
}

func uniqueServeIssueContextKey(values map[string]any, base string) string {
	if _, exists := values[base]; !exists {
		return base
	}
	for suffix := 2; ; suffix++ {
		candidate := fmt.Sprintf("%s#%d", base, suffix)
		if _, exists := values[candidate]; !exists {
			return candidate
		}
	}
}

func safeOperatorErrorLeaf(err error) string {
	if err == nil {
		return ""
	}
	if typed, ok := err.(*TuskerError); ok {
		return safeOperatorErrorText("["+typed.Code+"] "+typed.Message, 320)
	}
	return safeOperatorErrorText(err.Error(), 320)
}

func safeOperatorErrorText(value string, limit int) string {
	value = safePacketText(value, 0)
	if value == "" {
		return ""
	}
	value = serveErrorJSONSecretPattern.ReplaceAllString(value, "${1}[redacted]")
	value = serveErrorJSONAuthorizationPattern.ReplaceAllString(value, "${1}[redacted]")
	value = serveErrorUnixAbsolutePathPattern.ReplaceAllString(value, "${1}[path]")
	value = serveErrorWindowsAbsolutePathPattern.ReplaceAllString(value, "${1}[path]")
	value = serveErrorWindowsUNCPathPattern.ReplaceAllString(value, "${1}[path]")
	return safePacketText(value, limit)
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
	errorScreenshotCheckMissing     = "SCREENSHOT_CHECK_MISSING"
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
	noteTypes                            = makeSet("epic", "task", "doc", "note")
	taskStatuses                         = makeSet("draft", "backlog", "ready", "active", "blocked", "review", "rework", "done", "cancelled")
	docStatuses                          = makeSet("draft", "review", "approved", "published", "archived")
	publishableDocStatuses               = makeSet("approved", "published")
	docIntents                           = makeSet("canon", "companion")
	canonicalStatuses                    = makeSet("draft", "approved", "deprecated", "historical")
	changeTypes                          = makeSet("feature", "bug", "refactor", "migration", "security", "docs", "chore", "research", "incident")
	sizes                                = makeSet("s", "m", "l", "xl")
	risks                                = makeSet("low", "medium", "high", "critical")
	priorities                           = makeSet("p0", "p1", "p2", "p3")
	delegations                          = makeSet("execute", "explore", "escalate")
	aiAssistanceLevels                   = makeSet("none", "light", "moderate", "heavy")
	docAudiences                         = makeSet("developer", "user", "operator", "support", "release", "agent", "internal")
	docModes                             = makeSet("tutorial", "how-to", "reference", "explanation")
	docAgentLayers                       = makeSet("none", "capsule", "standalone")
	publicationLanes                     = makeSet("developer", "user", "release-notes", "support", "internal")
	uiSurfaces                           = makeSet("frontend", "desktop", "mobile")
	epicAcronymPattern                   = regexp.MustCompile(`^[A-Z]{3}$`)
	taskIDPattern                        = regexp.MustCompile(`^([A-Z]{3})-T-(\d{4})$`)
	docIDPattern                         = regexp.MustCompile(`^([A-Z]{3})-D-(\d{4})$`)
	recordIDPattern                      = regexp.MustCompile(`^[0-9A-HJKMNP-TV-Z]{26}$`)
	assetLinkRegex                       = regexp.MustCompile(`(!\[.*?\]\(.+?\))|(!\[\[.+?\]\])|(\[.+?\]\((https?:\/\/\S+?)\))`)
	integerStringPattern                 = regexp.MustCompile(`^-?\d+$`)
	serveErrorUnixAbsolutePathPattern    = regexp.MustCompile(`(^|[\s=:,"'(\[{])(/[^\s/"'()\[\]{}<>,;][^\s"'()\[\]{}<>,;]*)`)
	serveErrorWindowsAbsolutePathPattern = regexp.MustCompile(`(^|[\s=,"'(\[{])([A-Za-z]:[\\/][^\s"'()\[\]{}<>,;]+)`)
	serveErrorWindowsUNCPathPattern      = regexp.MustCompile(`(^|[\s=,"'(\[{])(\\\\[^\\/\s"'()\[\]{}<>,;]+(?:\\[^\\/\s"'()\[\]{}<>,;]+)+)`)
	serveErrorJSONSecretPattern          = regexp.MustCompile(`(?i)(["']?(?:api[_-]?key|access[_-]?token|refresh[_-]?token|token|password|secret)["']?\s*[:=]\s*["']?)[^"',;}\]]+`)
	serveErrorJSONAuthorizationPattern   = regexp.MustCompile(`(?i)(["']?(?:authorization|proxy-authorization)["']?\s*[:=]\s*["']?(?:(?:bearer|basic)\s+)?)[^"',;}\]]+`)
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
		"schema", "id", "title", "type", "capsule", "status", "owner", "summary", "doc_nodes",
		"created", "updated", "started", "blocked_since", "completed", "cancelled_at", "transitions", "tags",
	},
	"task": {
		"schema", "id", "title", "type", "kind", "capsule", "epic", "status", "priority", "risk", "size",
		"delegation", "ai_assistance", "ai_tools", "assignee", "domains", "doc_nodes", "knowledge_nodes", "blocked_by", "block_reason", "blocks", "created", "updated", "started",
		"review_requested_at", "completed", "cancelled_at", "blocked_since", "verified_by", "verified_at",
		"verification_summary", "closed_by", "closed_at", "close_summary", "docs_resolution", "knowledge_resolution", "transitions", "tags",
	},
	"doc": {
		"schema", "id", "title", "type", "node", "capsule", "status", "epic", "doc_intent", "canon_for",
		"audience", "mode", "agent_layer", "kind", "domains", "source_of_truth", "stale_when_paths", "canonical_status", "last_verified_at", "owner_epic", "verified_at", "deprecated", "superseded_by",
		"publish", "publish_lane", "publish_path", "publish_description", "publish_order", "publish_section_title", "redirect_from", "publish_url", "published_at", "created", "updated", "tags",
	},
	"note": {"title", "type", "capsule", "created", "updated", "tags"},
	"proposal": {
		"schema", "kind", "id", "project", "title", "status", "action", "target_kind", "target", "proposed_fields", "proposed_by", "source_branch", "created_at", "updated_at", "state_rev",
	},
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
	CompletionStore  *RuntimeStore
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

	if schema == "" && noteType == "" && stringField(data, "kind") == "" {
		return errors, warnings
	}
	if strings.HasPrefix(schema, "tusker.") && (strings.HasSuffix(schema, "/v7") || schema == "tusker.gate/v1" || schema == "tusker.evidence/v1" || schema == "tusker.attempt/v1" || schema == "tusker.decision/v1" || schema == "tusker.proposal/v1" || schema == "tusker.closeout/v1") {
		return validateV7Note(note, ctx, where)
	}
	if strings.HasSuffix(schema, "/v5") || strings.HasSuffix(schema, "/v6") {
		errors = append(errors, issue(errorInvalidField, fmt.Sprintf(`%s uses removed legacy schema %q`, where, schema), where, "convert the record into a V7 .tusker/work/** or .tusker/knowledge/domains/** record before validating this repo", map[string]any{"field": "schema", "value": schema}))
		return errors, warnings
	}
	if noteType == "note" && schema == "" {
		for _, field := range []string{"title", "created", "updated"} {
			if stringField(data, field) == "" {
				errors = append(errors, issue(errorMissingField, fmt.Sprintf(`missing required frontmatter "%s"`, field), where, "", map[string]any{"field": field}))
			}
		}
		return errors, warnings
	}
	if strings.HasPrefix(schema, "tusker.") {
		errors = append(errors, issue(errorInvalidField, fmt.Sprintf(`%s is not a supported V7 Tusker note`, where), where, "use V7 schemas: tusker.task/v7, tusker.epic/v7, tusker.domain/v7, tusker.knowledge/v7, tusker.gate/v1, tusker.evidence/v1, tusker.attempt/v1, tusker.decision/v1, tusker.proposal/v1, or tusker.closeout/v1", map[string]any{"field": "schema", "value": schema}))
		return errors, warnings
	}
	if _, ok := noteTypes[noteType]; !ok {
		errors = append(errors, issue(errorUnknownType, fmt.Sprintf(`unknown type "%s"`, noteType), where, "", nil))
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
	return noteType == "epic" || noteType == "task" || noteType == "doc"
}

func managedNoteTypeForData(data map[string]any) string {
	if noteType := stringField(data, "type"); managedNoteType(noteType) {
		return noteType
	}
	if kind := effectiveV7Kind(data); managedNoteType(kind) {
		return kind
	}
	return ""
}

func sanitizeCanonicalNoteData(data map[string]any) {
	noteType := managedNoteTypeForData(data)
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
