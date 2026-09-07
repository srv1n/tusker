package main

import (
	"fmt"
	"strings"

	"tusker/internal/docgraph"
)

type docsCheckIssue struct {
	Code    string `json:"code"`
	Path    string `json:"path"`
	Message string `json:"message"`
	Repair  string `json:"repair"`
}

type docsCheckResult struct {
	Schema        string           `json:"schema"`
	Valid         bool             `json:"valid"`
	Subject       string           `json:"subject,omitempty"`
	Path          string           `json:"path,omitempty"`
	DocumentCount int              `json:"document_count"`
	Issues        []docsCheckIssue `json:"issues"`
}

// docsCheckFailure carries a normal validation exit without asking main to
// print a second error after the already-rendered human or JSON report.
type docsCheckFailure struct {
	Count int
}

func (e *docsCheckFailure) Error() string {
	return fmt.Sprintf("documentation check found %d issue%s", e.Count, plural(e.Count))
}

func docsCheckCmd(args Args) error {
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	repoRoot := v7RepoRoot(vaultPath)
	corpus, loadIssues, err := docgraph.LoadRepository(repoRoot)
	if err != nil {
		return err
	}
	result := docsCheckResult{Schema: "tusker.docs-check/v1", DocumentCount: len(corpus.Documents), Issues: []docsCheckIssue{}}
	ref := strings.TrimSpace(positionalPhrase(args))
	if ref != "" {
		pathRef := strings.Contains(ref, "/") || strings.HasSuffix(strings.ToLower(ref), ".md")
		if pathRef {
			clean, pathErr := docgraph.NormalizeManagedPath(ref)
			if pathErr != nil {
				return docsDiscoveryError(pathErr)
			}
			for _, issue := range loadIssues {
				if issue.Path == clean {
					result.Path = clean
					break
				}
			}
			if result.Path == "" {
				_, resolver, doc, _, _, resolveErr := docsResolveDocument(repoRoot, ref, false)
				if resolveErr != nil {
					return docsDiscoveryError(resolveErr)
				}
				_ = resolver
				result.Subject, result.Path = doc.Subject, doc.Path
			}
		} else {
			_, resolver, doc, _, _, resolveErr := docsResolveDocument(repoRoot, ref, false)
			if resolveErr != nil {
				return docsDiscoveryError(resolveErr)
			}
			_ = resolver
			result.Subject, result.Path = doc.Subject, doc.Path
		}
	}
	issues, err := docgraph.ValidateRepository(repoRoot)
	if err != nil {
		return err
	}
	for _, issue := range issues {
		if result.Path != "" && issue.Path != result.Path {
			continue
		}
		result.Issues = append(result.Issues, docsCheckIssue{
			Code: issue.Code, Path: issue.Path, Message: issue.Message, Repair: docsRepairHint(issue.Code),
		})
	}
	result.Valid = len(result.Issues) == 0
	if args.Bool("json") {
		emitJSON(result)
		if !result.Valid {
			return &docsCheckFailure{Count: len(result.Issues)}
		}
		return nil
	}
	if result.Valid {
		if result.Path == "" {
			fmt.Printf("Documentation check passed (%d documents).\n", result.DocumentCount)
		} else {
			fmt.Printf("Documentation check passed for %s.\n", result.Path)
		}
		return nil
	}
	for _, issue := range result.Issues {
		fmt.Printf("[%s] %s: %s\n  repair: %s\n", issue.Code, issue.Path, issue.Message, issue.Repair)
	}
	return &docsCheckFailure{Count: len(result.Issues)}
}

func docsRepairHint(code string) string {
	switch code {
	case "DOC_REQUIRED_FIELD_MISSING":
		return "add the required front-matter field, then run `tusker docs check <path>`"
	case "DOC_DUPLICATE_SUBJECT":
		return "keep one subject as the current document and supersede or rename the duplicate"
	case "DOC_TOMBSTONE_SUCCESSOR_MISSING", "DOC_TOMBSTONE_SUCCESSOR_NOT_FOUND":
		return "set superseded_by to an existing current document subject"
	case "DOC_LINK_DANGLING":
		return "fix the managed link or remove the reference; use a source/report path for non-document material"
	case "DOC_VERSIONED_FILENAME":
		return "update the existing subject in place or create an explicit superseded signpost"
	case "DOC_HEADER_MISSING", "DOC_HEADER_PARSE_ERROR":
		return "repair the YAML front matter, then rerun this check"
	case "DOC_HEADER_TYPE_INVALID":
		return "use strings or lists of strings for the declared header field, then rerun this check"
	default:
		return "inspect the document and rerun `tusker docs check` after the repair"
	}
}
