package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"
)

const (
	feedbackSignalSchema             = "tusker.feedback_signal/v1"
	feedbackSignalMaxSummaryChars    = 240
	feedbackSignalMaxFactStringChars = 180
	feedbackSignalMaxFactKeys        = 16
	feedbackSignalMaxFactListItems   = 12
	feedbackSignalTokenBurnThreshold = 80000
)

var (
	feedbackSignalCategories = map[string]bool{
		"review_loop":        true,
		"acceptance_quality": true,
		"token_burn":         true,
		"cli_friction":       true,
		"closeout_churn":     true,
		"workflow_repeat":    true,
		"environment_setup":  true,
	}
	feedbackSignalSeverities  = map[string]bool{"p0": true, "p1": true, "p2": true, "p3": true}
	feedbackSignalConfidences = map[string]bool{"low": true, "medium": true, "high": true}
)

type feedbackSignal struct {
	Schema         string                     `json:"schema"`
	ID             string                     `json:"id"`
	Date           string                     `json:"date"`
	Project        string                     `json:"project"`
	TaskID         string                     `json:"task,omitempty"`
	AttemptID      string                     `json:"attempt,omitempty"`
	Source         string                     `json:"source"`
	Category       string                     `json:"category"`
	Severity       string                     `json:"severity"`
	Confidence     string                     `json:"confidence"`
	DedupeKey      string                     `json:"dedupe_key"`
	Summary        string                     `json:"summary"`
	ObservedFacts  map[string]any             `json:"observed_facts"`
	Occurrences    []feedbackSignalOccurrence `json:"occurrences,omitempty"`
	Recommendation string                     `json:"recommendation,omitempty"`
	RawPayload     map[string]any             `json:"raw_payload,omitempty"`
}

type feedbackSignalOccurrence struct {
	Vault      string `json:"vault,omitempty"`
	Project    string `json:"project,omitempty"`
	Source     string `json:"source,omitempty"`
	TaskID     string `json:"task,omitempty"`
	AttemptID  string `json:"attempt,omitempty"`
	SignalID   string `json:"signal,omitempty"`
	SourcePath string `json:"source_path,omitempty"`
}

func completeFeedbackSignal(signal feedbackSignal) feedbackSignal {
	signal.Schema = firstNonEmpty(signal.Schema, feedbackSignalSchema)
	signal.Date = firstNonEmpty(strings.TrimSpace(signal.Date), todayISO())
	signal.Project = strings.ToUpper(strings.TrimSpace(signal.Project))
	signal.TaskID = strings.ToUpper(strings.TrimSpace(signal.TaskID))
	signal.AttemptID = strings.TrimSpace(signal.AttemptID)
	signal.Source = feedbackCleanValue(signal.Source)
	signal.Category = strings.ToLower(feedbackCleanValue(signal.Category))
	signal.Severity = normalizeFeedbackSignalSeverity(firstNonEmpty(signal.Severity, "P2"))
	signal.Confidence = strings.ToLower(feedbackCleanValue(firstNonEmpty(signal.Confidence, "medium")))
	signal.DedupeKey = normalizedFeedbackSignalDedupeKey(signal.DedupeKey)
	signal.Summary = feedbackCleanValue(signal.Summary)
	signal.Recommendation = feedbackCleanValue(signal.Recommendation)
	if signal.ObservedFacts == nil {
		signal.ObservedFacts = map[string]any{}
	}
	if signal.ID == "" {
		signal.ID = feedbackSignalID(signal)
	}
	return signal
}

func validateFeedbackSignal(signal feedbackSignal) []Issue {
	signal = completeFeedbackSignal(signal)
	path := feedbackSignalRelativePath(signal)
	var issues []Issue
	if signal.Schema != feedbackSignalSchema {
		issues = append(issues, issue("FEEDBACK_SIGNAL_SCHEMA_INVALID", "feedback signal schema must be "+feedbackSignalSchema, path, "", map[string]any{"schema": signal.Schema}))
	}
	if strings.TrimSpace(signal.ID) == "" {
		issues = append(issues, issue("FEEDBACK_SIGNAL_ID_MISSING", "feedback signal id is required", path, "", nil))
	}
	if _, err := time.Parse("2006-01-02", signal.Date); err != nil {
		issues = append(issues, issue("FEEDBACK_SIGNAL_DATE_INVALID", "feedback signal date must be YYYY-MM-DD: "+signal.Date, path, "", nil))
	}
	if signal.Project == "" {
		issues = append(issues, issue("FEEDBACK_SIGNAL_PROJECT_MISSING", "feedback signal project is required", path, "", nil))
	}
	if signal.Source == "" {
		issues = append(issues, issue("FEEDBACK_SIGNAL_SOURCE_MISSING", "feedback signal source is required", path, "", nil))
	}
	if !feedbackSignalCategories[signal.Category] {
		issues = append(issues, issue("FEEDBACK_SIGNAL_CATEGORY_INVALID", "feedback signal category is invalid: "+signal.Category, path, "use one of: "+strings.Join(feedbackSignalCategoryNames(), ", "), map[string]any{"category": signal.Category}))
	}
	if !feedbackSignalSeverities[strings.ToLower(signal.Severity)] {
		issues = append(issues, issue("FEEDBACK_SIGNAL_SEVERITY_INVALID", "feedback signal severity is invalid: "+signal.Severity, path, "use P0, P1, P2, or P3", map[string]any{"severity": signal.Severity}))
	}
	if !feedbackSignalConfidences[signal.Confidence] {
		issues = append(issues, issue("FEEDBACK_SIGNAL_CONFIDENCE_INVALID", "feedback signal confidence is invalid: "+signal.Confidence, path, "use low, medium, or high", map[string]any{"confidence": signal.Confidence}))
	}
	if signal.DedupeKey == "" {
		issues = append(issues, issue("FEEDBACK_SIGNAL_DEDUPE_KEY_MISSING", "feedback signal dedupe_key is required", path, "", nil))
	}
	if signal.Summary == "" {
		issues = append(issues, issue("FEEDBACK_SIGNAL_SUMMARY_MISSING", "feedback signal summary is required", path, "", nil))
	} else if len(signal.Summary) > feedbackSignalMaxSummaryChars || strings.Contains(signal.Summary, "\n") {
		issues = append(issues, issue("FEEDBACK_SIGNAL_SUMMARY_INVALID", fmt.Sprintf("feedback signal summary must be one short line under %d chars", feedbackSignalMaxSummaryChars), path, "", map[string]any{"chars": len(signal.Summary)}))
	}
	if signal.Recommendation != "" && (len(signal.Recommendation) > feedbackSignalMaxSummaryChars || strings.Contains(signal.Recommendation, "\n")) {
		issues = append(issues, issue("FEEDBACK_SIGNAL_RECOMMENDATION_INVALID", fmt.Sprintf("feedback signal recommendation must be one short line under %d chars", feedbackSignalMaxSummaryChars), path, "", map[string]any{"chars": len(signal.Recommendation)}))
	}
	if len(signal.RawPayload) > 0 {
		issues = append(issues, issue("FEEDBACK_SIGNAL_RAW_PAYLOAD_FORBIDDEN", "feedback signals must not store raw event payloads, transcripts, command logs, attachments, or copied source", path, "summarize raw input into observed_facts", map[string]any{"keys": sortedAnyMapKeys(signal.RawPayload)}))
	}
	issues = append(issues, validateFeedbackSignalFacts(path, signal.ObservedFacts)...)
	return issues
}

func validateFeedbackSignalFacts(path string, facts map[string]any) []Issue {
	if len(facts) == 0 {
		return []Issue{issue("FEEDBACK_SIGNAL_FACTS_MISSING", "feedback signal observed_facts are required", path, "", nil)}
	}
	var issues []Issue
	if len(facts) > feedbackSignalMaxFactKeys {
		issues = append(issues, issue("FEEDBACK_SIGNAL_FACTS_TOO_MANY", fmt.Sprintf("feedback signal observed_facts has %d keys; max is %d", len(facts), feedbackSignalMaxFactKeys), path, "", nil))
	}
	for _, key := range sortedAnyMapKeys(facts) {
		value := facts[key]
		cleanKey := strings.TrimSpace(key)
		if cleanKey == "" || feedbackSignalForbiddenFactKey(cleanKey) {
			issues = append(issues, issue("FEEDBACK_SIGNAL_FACT_KEY_INVALID", "feedback signal observed_facts key is not summary-safe: "+key, path, "use bounded counts, labels, paths, task IDs, and short reason excerpts", map[string]any{"key": key}))
			continue
		}
		if detail := feedbackSignalUnsafeFactValue(value); detail != "" {
			issues = append(issues, issue("FEEDBACK_SIGNAL_FACT_VALUE_INVALID", "feedback signal observed_facts."+key+" is not summary-safe: "+detail, path, "do not store raw transcripts, full logs, attachments, diffs, or copied source", map[string]any{"key": key}))
		}
	}
	return issues
}

// feedbackSignalRelativePath must stay the vault-relative form of the real write
// path, otherwise validation issues and source refs point at files that never exist.
func feedbackSignalRelativePath(signal feedbackSignal) string {
	signal = completeFeedbackSignal(signal)
	hash := feedbackSignalHash(signal.DedupeKey)[:10]
	base := feedbackSlug(signal.Category+"-"+signal.DedupeKey, "signal")
	return filepath.ToSlash(filepath.Join("feedback", "signals", signal.Date, base+"-"+hash+".json"))
}

func normalizedFeedbackSignalDedupeKey(value string) string {
	value = strings.ToLower(feedbackCleanValue(value))
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

func normalizeFeedbackSignalSeverity(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "P2"
	}
	upper := strings.ToUpper(value)
	if strings.HasPrefix(upper, "P") && len(upper) >= 2 {
		switch upper[:2] {
		case "P0", "P1", "P2", "P3":
			return upper[:2]
		}
	}
	switch strings.ToLower(value) {
	case "critical", "blocker":
		return "P0"
	case "high", "major":
		return "P1"
	case "medium", "moderate":
		return "P2"
	case "low", "minor", "info", "informational":
		return "P3"
	default:
		return strings.ToLower(feedbackCleanValue(value))
	}
}

func feedbackSignalID(signal feedbackSignal) string {
	signal.Schema = firstNonEmpty(signal.Schema, feedbackSignalSchema)
	base := strings.Join([]string{signal.Schema, signal.Date, signal.Project, signal.Category, signal.DedupeKey}, "|")
	return "FS-" + signal.Date + "-" + feedbackSignalHash(base)[:12]
}

func feedbackSignalHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func feedbackSignalUnsafeFactValue(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return feedbackSignalUnsafeFactString(v)
	case bool, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64, json.Number:
		return ""
	case []string:
		if len(v) > feedbackSignalMaxFactListItems {
			return "list is too long"
		}
		for _, item := range v {
			if detail := feedbackSignalUnsafeFactString(item); detail != "" {
				return detail
			}
		}
		return ""
	case []any:
		if len(v) > feedbackSignalMaxFactListItems {
			return "list is too long"
		}
		for _, item := range v {
			switch item.(type) {
			case string, bool, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64, json.Number:
				if detail := feedbackSignalUnsafeFactValue(item); detail != "" {
					return detail
				}
			default:
				return "list contains nested or raw values"
			}
		}
		return ""
	default:
		return "value must be a bounded primitive or primitive list"
	}
}

func feedbackSignalUnsafeFactString(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > feedbackSignalMaxFactStringChars {
		return fmt.Sprintf("string is %d chars; max is %d", len(value), feedbackSignalMaxFactStringChars)
	}
	if strings.Count(value, "\n") > 1 {
		return "string has multiple lines"
	}
	lower := strings.ToLower(value)
	for _, marker := range []string{"function_call_output", "original token count", "diff --git", "```", "stack trace:", "transcript:", "raw log", "begin patch"} {
		if strings.Contains(lower, marker) {
			return "string looks like raw output"
		}
	}
	return ""
}

func feedbackSignalForbiddenFactKey(key string) bool {
	normalized := normalizedFeedbackSignalDedupeKey(key)
	for _, marker := range []string{"raw", "payload", "transcript", "attachment", "diff", "patch", "full-log", "command-log", "source-code", "file-content"} {
		if normalized == marker || strings.Contains(normalized, marker) {
			return true
		}
	}
	return false
}

func feedbackSignalCategoryNames() []string {
	keys := make([]string, 0, len(feedbackSignalCategories))
	for key := range feedbackSignalCategories {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sortedAnyMapKeys(values map[string]any) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
