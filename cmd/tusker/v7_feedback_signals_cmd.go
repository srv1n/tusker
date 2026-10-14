package main

import (
	"encoding/json"
	"io/fs"
	"path/filepath"
	"strings"
)

func feedbackSignalFromFile(path string) (feedbackSignal, error) {
	raw, err := readText(path)
	if err != nil {
		return feedbackSignal{}, err
	}
	var signal feedbackSignal
	if err := json.Unmarshal([]byte(raw), &signal); err != nil {
		return feedbackSignal{}, err
	}
	return completeFeedbackSignal(signal), nil
}

func validateV7FeedbackSignals(vaultPath string) []Issue {
	signalsDir := filepath.Join(vaultPath, "feedback", "signals")
	if !dirExists(signalsDir) {
		return nil
	}
	var issues []Issue
	if err := walkDirUnsorted(signalsDir, func(current string, entry fs.DirEntry) error {
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".json") {
			return nil
		}
		signal, err := feedbackSignalFromFile(current)
		rel, _ := filepath.Rel(vaultPath, current)
		rel = filepath.ToSlash(rel)
		if err != nil {
			issues = append(issues, issue("FEEDBACK_SIGNAL_READ_FAILED", err.Error(), rel, "", nil))
			return nil
		}
		for _, currentIssue := range validateFeedbackSignal(signal) {
			currentIssue.Path = rel
			issues = append(issues, currentIssue)
		}
		return nil
	}); err != nil {
		return []Issue{issue("FEEDBACK_SIGNAL_VALIDATE_FAILED", err.Error(), "feedback/signals", "", nil)}
	}
	return issues
}
