package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"tusker/internal/v7policy"
)

func defaultV7ClosePolicy(risk string) v7ClosePolicy {
	return v7policy.DefaultClosePolicy(risk)
}

func v7CloseRequiredEvidence(risk string) []string {
	return v7policy.RequiredEvidence(risk)
}

func v7CloseRequiredGateKinds(risk string) []string {
	return v7policy.RequiredGateKinds(risk)
}

func v7CloseGateKindSatisfied(idx v7Index, taskID, gateKind string) bool {
	for _, gate := range idx.Gates {
		if stringField(gate.Data, "gate_kind") != gateKind {
			continue
		}
		if !containsString(normalizeList(gate.Data["blocks"]), taskID) {
			continue
		}
		status := stringField(gate.Data, "status")
		if (status == "satisfied" || status == "waived") && v7GateAuthorityReceiptCurrent(gate, idx) {
			return true
		}
	}
	return false
}

func mergeUniqueStrings(groups ...[]string) []string {
	seen := map[string]bool{}
	var out []string
	for _, group := range groups {
		for _, item := range group {
			item = strings.TrimSpace(item)
			if item == "" || seen[item] {
				continue
			}
			seen[item] = true
			out = append(out, item)
		}
	}
	return out
}

func evidenceV7AddCmd(args Args) error {
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	taskID, err := requireArg(args, "id")
	if err != nil {
		return err
	}
	task, err := resolveV7Note(vaultPath, taskID, "task")
	if err != nil {
		return err
	}
	kind := strings.ToLower(fallback(args.String("kind"), "manual_smoke"))
	if _, ok := v7EvidenceKinds[kind]; !ok {
		return tuskerError(errorInvalidField, "invalid evidence kind: "+kind)
	}
	id := strings.ToUpper(args.String("evidence-id"))
	if id == "" {
		id = fmt.Sprintf("%s-E-%s", taskID, padNumber(nextV7EvidenceSequence(vaultPath, taskID)))
	}
	if !v7EvidenceIDPattern.MatchString(id) {
		return tuskerError(errorInvalidArg, "invalid evidence id: "+id)
	}
	dir := filepath.Join(vaultPath, "evidence", taskID)
	path := filepath.Join(dir, id+".md")
	if fileExists(path) {
		return tuskerError(errorAlreadyExists, "Evidence already exists: "+id, withPath(path))
	}
	now := time.Now().UTC().Format(time.RFC3339)
	covers := normalizeV7Covers(splitCSV(args.String("covers")))
	if len(covers) == 0 {
		return tuskerError(errorMissingArg, "evidence requires --covers A1 or --covers TASK:A1", withHint("tie every evidence record to the acceptance item it proves"))
	}
	artifactPaths, durability, err := prepareV7EvidenceArtifacts(vaultPath, taskID, id, args)
	if err != nil {
		return err
	}
	createdBy := fallback(args.String("by"), "agent:"+defaultActorName())
	status := fallback(args.String("status"), "accepted")
	if v7EvidenceRequiresReviewerAcceptance(kind) && args.String("status") == "" {
		status = "pending_review"
	}
	if _, ok := v7EvidenceStatus[status]; !ok {
		return tuskerError(errorInvalidField, "invalid evidence status: "+status)
	}
	data := map[string]any{
		"schema":         "tusker.evidence/v1",
		"kind":           "evidence",
		"id":             id,
		"project":        v7ProjectID(vaultPath),
		"task":           taskID,
		"epic":           stringField(task.Data, "epic"),
		"evidence_kind":  kind,
		"status":         status,
		"covers":         covers,
		"artifact_paths": artifactPaths,
		"created_by":     createdBy,
		"created_at":     now,
	}
	if durability != "" {
		data["artifact_durability"] = durability
	}
	if kind == "screenshot" {
		checkedBy := firstNonEmpty(args.String("screenshot-checked-by"), args.String("checked-by"))
		if status == "accepted" {
			if checkedBy == "" {
				return tuskerError(errorMissingArg, "accepted screenshot evidence requires --checked-by", withHint("screenshots start pending_review until a reviewer records the visual check"))
			}
			if checkedBy == createdBy {
				risk := strings.ToLower(fallback(stringField(task.Data, "risk"), "medium"))
				if risk != "low" || !args.Bool("allow-self-check") {
					return tuskerError(errorInvalidTransition, "screenshot creator cannot self-check accepted evidence without explicit low-risk policy", withHint("use an independent checker, or pass --allow-self-check only for low-risk tasks"))
				}
			}
			data["screenshot_checked_by"] = checkedBy
			data["screenshot_checked_at"] = firstNonEmpty(args.String("screenshot-checked-at"), args.String("checked-at"), now)
		} else if checkedBy != "" {
			data["screenshot_checked_by"] = checkedBy
			data["screenshot_checked_at"] = firstNonEmpty(args.String("screenshot-checked-at"), args.String("checked-at"), now)
		}
		data["redacted"] = args.Bool("redacted")
		if note := args.String("redaction-note"); note != "" {
			data["redaction_note"] = note
		}
	}
	if status == "accepted" {
		acceptedBy := fallback(args.String("accepted-by"), stringField(data, "created_by"))
		if v7EvidenceRequiresReviewerAcceptance(kind) {
			acceptedBy = firstNonEmpty(args.String("accepted-by"), args.String("checked-by"), args.String("screenshot-checked-by"))
			if acceptedBy == "" {
				return tuskerError(errorMissingArg, "accepted "+kind+" evidence requires --accepted-by human:<name> or reviewer:<name>")
			}
			if !v7EvidenceAcceptorAllowed(acceptedBy) {
				return tuskerError(errorInvalidTransition, "accepted "+kind+" evidence requires a human or reviewer acceptor", withContext(map[string]any{"accepted_by": acceptedBy, "evidence_kind": kind}))
			}
			if acceptedBy == createdBy {
				risk := strings.ToLower(fallback(stringField(task.Data, "risk"), "medium"))
				if risk != "low" || !args.Bool("allow-self-check") {
					return tuskerError(errorInvalidTransition, "evidence creator cannot self-accept "+kind+" evidence without explicit low-risk policy", withHint("use an independent human/reviewer, or pass --allow-self-check only for low-risk tasks"))
				}
			}
		}
		data["accepted_by"] = acceptedBy
		data["accepted_at"] = now
	}
	summary := fallback(args.String("summary"), "Evidence captured.")
	body := fmt.Sprintf("# %s · %s evidence\n\n## Summary\n\n%s\n\n## Commands\n\n%s\n\n## Result\n\n%s\n\n## Covers\n\n%s\n\n## Artifact links\n\n%s\n", id, kind, summary, v7MaybeCodeBlock(args.String("command")), fallback(args.String("result"), "Recorded."), v7BulletList(normalizeList(data["covers"])), v7BulletList(artifactPaths))
	data["state_rev"] = v7StateRev(data, body)
	content, err := serializeDocument(data, body, v7FrontmatterOrder["evidence"])
	if err != nil {
		return err
	}
	if err := writeText(path, content); err != nil {
		return err
	}
	if !args.Bool("quiet") {
		fmt.Printf("Added evidence %s at %s\n", id, path)
	}
	actor := stringField(data, "created_by")
	if err := emitV7Event(vaultPath, taskID, "task", "evidence_added", actor, map[string]any{"evidence": id, "kind": kind}); err != nil {
		return err
	}
	return updateV7TaskProofStatus(vaultPath, taskID, actor)
}

func v7EvidenceRequiresReviewerAcceptance(kind string) bool {
	switch kind {
	case "screenshot", "video", "manual_smoke", "physical_smoke", "release_smoke", "security_review", "privacy_review", "human_review", "performance_profile":
		return true
	default:
		return false
	}
}

func v7EvidenceAcceptorAllowed(actor string) bool {
	return strings.HasPrefix(actor, "human:") || strings.HasPrefix(actor, "reviewer:")
}

func prepareV7EvidenceArtifacts(vaultPath, taskID, evidenceID string, args Args) ([]string, string, error) {
	var paths []string
	externalURL := strings.TrimSpace(args.String("external-url"))
	if externalURL != "" {
		if !strings.Contains(externalURL, "://") {
			return nil, "", tuskerError(errorInvalidArg, "--external-url requires a URL with a scheme", withHint("use --external-url https://... for intentional external evidence"))
		}
		paths = append(paths, "external:"+externalURL)
	}
	inputs := splitCSV(firstNonEmpty(args.String("path"), args.String("artifact-paths")))
	if len(inputs) == 0 {
		if externalURL != "" {
			return paths, "external", nil
		}
		return nil, "", nil
	}
	if args.Bool("link-only") {
		for _, input := range inputs {
			trimmed := strings.TrimSpace(input)
			if trimmed != "" {
				paths = append(paths, "link-only:"+trimmed)
			}
		}
		return paths, "link_only", nil
	}
	artifactDir := filepath.Join(vaultPath, "evidence", taskID, "artifacts")
	seenNames := map[string]int{}
	for _, input := range inputs {
		source, err := resolveDurableEvidenceSource(input)
		if err != nil {
			return nil, "", err
		}
		base := filepath.Base(source)
		seenNames[base]++
		if seenNames[base] > 1 {
			ext := filepath.Ext(base)
			stem := strings.TrimSuffix(base, ext)
			base = fmt.Sprintf("%s-%d%s", stem, seenNames[base], ext)
		}
		target := filepath.Join(artifactDir, base)
		if err := copyFile(source, target); err != nil {
			return nil, "", err
		}
		rel, err := filepath.Rel(vaultPath, target)
		if err != nil {
			return nil, "", err
		}
		paths = append(paths, filepath.ToSlash(rel))
	}
	if len(paths) == 0 {
		return paths, "", nil
	}
	if externalURL != "" {
		return paths, "mixed", nil
	}
	return paths, "copied", nil
}

func resolveDurableEvidenceSource(input string) (string, error) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return "", tuskerError(errorMissingArg, "empty evidence artifact path")
	}
	if strings.Contains(trimmed, "://") {
		return "", tuskerError(errorInvalidArg, "URL evidence requires --external-url", withHint("use --external-url for intentional external evidence links"))
	}
	if strings.HasPrefix(filepath.ToSlash(trimmed), "/tmp/") {
		return "", tuskerError(errorInvalidArg, "temporary evidence artifact paths are not durable: "+trimmed, withHint("copy the file from a durable workspace path or use --link-only to mark it non-durable"))
	}
	if filepath.IsAbs(trimmed) {
		return "", tuskerError(errorInvalidArg, "absolute local evidence artifact paths are not durable: "+trimmed, withHint("pass a repo-relative path so Tusker can copy it into .tusker/evidence/<task>/artifacts/"))
	}
	clean := filepath.Clean(trimmed)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || filepath.IsAbs(clean) {
		return "", tuskerError(errorPathEscape, "evidence artifact path escapes the workspace: "+trimmed)
	}
	abs, err := filepath.Abs(clean)
	if err != nil {
		return "", err
	}
	if !fileExists(abs) {
		return "", tuskerError(errorNotFound, "evidence artifact not found: "+trimmed)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return "", tuskerError(errorInvalidArg, "evidence artifact path is a directory: "+trimmed)
	}
	return abs, nil
}
