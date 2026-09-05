package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"tusker/internal/v7policy"
)

var v7EvidenceBeforeTaskCommitHook func(taskID, evidenceID string) error

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
	proofCategory, proofFacts, err := parseV7EvidenceProofCategory(args)
	if err != nil {
		return err
	}
	id := strings.ToUpper(args.String("evidence-id"))
	if id == "" {
		id = fmt.Sprintf("%s-E-%s", taskID, padNumber(nextV7EvidenceSequence(vaultPath, taskID)))
	}
	if !v7EvidenceIDPattern.MatchString(id) {
		return tuskerError(errorInvalidArg, "invalid evidence id: "+id)
	}
	covers := normalizeV7Covers(splitCSV(args.String("covers")))
	if len(covers) == 0 && tuskerTier(vaultPath) >= 2 {
		return tuskerError(errorMissingArg, "evidence requires --covers A1 or --covers TASK:A1", withHint("tie every evidence record to the acceptance item it proves"))
	}
	dir := filepath.Join(vaultPath, "evidence", taskID)
	path := filepath.Join(dir, id+".md")
	if fileExists(path) {
		if resumed, err := resumeV7EvidenceAdd(vaultPath, taskID, id, path, kind, covers, args); resumed {
			return err
		}
		return tuskerError(errorAlreadyExists, "Evidence already exists: "+id, withPath(path))
	}
	now := time.Now().UTC().Format(time.RFC3339)
	artifactPaths, durability, err := prepareV7EvidenceArtifacts(vaultPath, taskID, id, args)
	if err != nil {
		return err
	}
	if proofCategory != "" && !v7ProofCategoryFactsValid(proofCategory, proofFacts, artifactPaths) {
		return tuskerError(errorInvalidArg, "invalid proof facts for "+proofCategory, withHint(v7EvidenceProofCategoryHint(proofCategory)))
	}
	// Serve supplies an explicitly configured operator actor for durable
	// evidence writes. Direct/automation callers retain the agent default.
	createdBy, err := v7AgentDefaultActor(args, "evidence creation")
	if err != nil {
		return err
	}
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
	if proofCategory != "" {
		data["proof_category"] = proofCategory
		data["proof_facts"] = proofFacts
	}
	if sourceRevision := firstNonEmpty(stringField(task.Data, "source_sha"), stringField(task.Data, "source_commit")); sourceRevision != "" {
		data["source_revision"] = sourceRevision
	}
	if durability == "copied" {
		fingerprint, ok := v7EvidenceArtifactFingerprint(vaultPath, Note{Data: data})
		if !ok {
			return tuskerError(errorInvalidTransition, "copied evidence artifact identity could not be recorded")
		}
		data["artifact_fingerprint"] = fingerprint
	}
	if kind == "screenshot" {
		rawCheckedBy := firstNonEmpty(args.String("screenshot-checked-by"), args.String("checked-by"))
		checkedBy := ""
		if rawCheckedBy != "" {
			checkedBy, err = v7ReviewerOrHumanActor(Args{"by": rawCheckedBy}, "screenshot check")
			if err != nil {
				return err
			}
		}
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
			rawAcceptedBy := firstNonEmpty(args.String("accepted-by"), args.String("checked-by"), args.String("screenshot-checked-by"))
			if rawAcceptedBy == "" {
				return tuskerError(errorMissingArg, "accepted "+kind+" evidence requires --accepted-by human:<name> or reviewer:<name>")
			}
			var acceptErr error
			acceptedBy, acceptErr = v7ReviewerOrHumanActor(Args{"by": rawAcceptedBy}, "accepted "+kind+" evidence")
			if acceptErr != nil {
				return acceptErr
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
	if err := writeNewV7EvidenceDocument(path, content); err != nil {
		return err
	}
	if v7EvidenceBeforeTaskCommitHook != nil {
		if err := v7EvidenceBeforeTaskCommitHook(taskID, id); err != nil {
			return err
		}
	}
	actor := stringField(data, "created_by")
	if err := updateV7TaskProofStatus(vaultPath, taskID, actor); err != nil {
		return err
	}
	if err := emitV7Event(vaultPath, taskID, "task", "evidence_added", actor, map[string]any{"evidence": id, "kind": kind}); err != nil {
		return err
	}
	if !args.Bool("quiet") {
		fmt.Printf("Added evidence %s at %s\n", id, path)
		if len(covers) == 0 {
			fmt.Println("Warning: evidence is not linked to an acceptance item.")
		}
	}
	return nil
}

func parseV7EvidenceProofCategory(args Args) (string, map[string]string, error) {
	category := strings.ToLower(strings.TrimSpace(args.String("proof-category")))
	factsRaw := strings.TrimSpace(args.String("proof-facts"))
	if category == "" && factsRaw == "" {
		return "", nil, nil
	}
	if category != "visual" && category != "performance" && category != "backend" && category != "migration" {
		return "", nil, tuskerError(errorInvalidArg, "proof category must be visual, performance, backend, or migration")
	}
	facts := map[string]string{}
	for _, item := range strings.Split(factsRaw, ",") {
		key, value, ok := strings.Cut(item, "=")
		key = strings.ToLower(strings.TrimSpace(strings.ReplaceAll(key, "-", "_")))
		value = strings.TrimSpace(value)
		if !ok || key == "" || value == "" || facts[key] != "" {
			return "", nil, tuskerError(errorInvalidArg, "proof facts must use unique key=value entries")
		}
		facts[key] = value
	}
	return category, facts, nil
}

func v7EvidenceProofCategoryHint(category string) string {
	switch category {
	case "visual":
		return "use baseline=before_after with two artifacts, or baseline=new_ui with an after artifact"
	case "performance":
		return "include before, after, before_workload, after_workload, units, method, environment, and revision; workloads must match"
	case "backend":
		return "include observable and negative"
	default:
		return "include preservation, interruption, and recovery"
	}
}

func resumeV7EvidenceAdd(vaultPath, taskID, evidenceID, path, requestedKind string, requestedCovers []string, args Args) (bool, error) {
	data, body, err := parseFrontmatterMustRead(path)
	if err != nil || effectiveV7Kind(data) != "evidence" || stringField(data, "id") != evidenceID || stringField(data, "task") != taskID {
		return true, v7EvidenceIDTakenError(evidenceID, path)
	}
	if rev := stringField(data, "state_rev"); rev == "" || !v7StateRevMatches(data, body, rev) {
		return true, v7EvidenceIDTakenError(evidenceID, path)
	}
	if strings.ToLower(stringField(data, "evidence_kind")) != requestedKind {
		return true, v7EvidenceIDTakenError(evidenceID, path)
	}
	existingCovers := normalizeV7Covers(normalizeList(data["covers"]))
	if len(existingCovers) != len(requestedCovers) {
		return true, v7EvidenceIDTakenError(evidenceID, path)
	}
	for i := range requestedCovers {
		if existingCovers[i] != requestedCovers[i] {
			return true, v7EvidenceIDTakenError(evidenceID, path)
		}
	}
	requestedStatus := fallback(args.String("status"), "accepted")
	if v7EvidenceRequiresReviewerAcceptance(requestedKind) && args.String("status") == "" {
		requestedStatus = "pending_review"
	}
	if stringField(data, "status") != requestedStatus || strings.TrimSpace(sectionContent(body, "## Summary")) != strings.TrimSpace(fallback(args.String("summary"), "Evidence captured.")) || !v7EvidenceArtifactRequestMatches(taskID, evidenceID, args, normalizeList(data["artifact_paths"])) {
		return true, v7EvidenceIDTakenError(evidenceID, path)
	}
	task, err := resolveV7Note(vaultPath, taskID, "task")
	if err != nil {
		return false, nil
	}
	if strings.Contains(task.Body, "[["+evidenceID+"]] ") {
		return false, nil
	}
	if v7EvidenceBeforeTaskCommitHook != nil {
		if err := v7EvidenceBeforeTaskCommitHook(taskID, evidenceID); err != nil {
			return true, err
		}
	}
	actor := stringField(data, "created_by")
	if err := updateV7TaskProofStatus(vaultPath, taskID, actor); err != nil {
		return true, err
	}
	if err := emitV7Event(vaultPath, taskID, "task", "evidence_added", actor, map[string]any{"evidence": evidenceID, "kind": stringField(data, "evidence_kind")}); err != nil {
		return true, err
	}
	return true, nil
}

func writeNewV7EvidenceDocument(path, content string) error {
	if err := ensureDir(filepath.Dir(path)); err != nil {
		return err
	}
	sweepStaleV7EvidenceTemps(filepath.Dir(path), filepath.Base(path))
	temp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer func() {
		_ = temp.Close()
		_ = os.Remove(tempPath)
	}()
	if err := temp.Chmod(0o644); err != nil {
		return err
	}
	if written, err := temp.WriteString(content); err != nil {
		return err
	} else if written != len(content) {
		return fmt.Errorf("write evidence document temporary file: wrote %d of %d bytes", written, len(content))
	}
	if err := temp.Sync(); err != nil {
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Link(tempPath, path); err != nil {
		if os.IsExist(err) {
			return v7EvidenceAlreadyExistsError(path)
		}
		if !errors.Is(err, syscall.ENOTSUP) && !errors.Is(err, syscall.EPERM) {
			return err
		}
		reserved, reserveErr := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if reserveErr != nil {
			if os.IsExist(reserveErr) {
				return v7EvidenceAlreadyExistsError(path)
			}
			return reserveErr
		}
		if reserveErr := reserved.Close(); reserveErr != nil {
			return reserveErr
		}
		if renameErr := os.Rename(tempPath, path); renameErr != nil {
			if os.IsExist(renameErr) {
				return v7EvidenceAlreadyExistsError(path)
			}
			return renameErr
		}
	}
	if err := syncV7DocumentDirectory(filepath.Dir(path)); err != nil {
		return err
	}
	invalidateCachedNote(path)
	recordCLIVaultMutation(path)
	return nil
}

func v7EvidenceAlreadyExistsError(path string) error {
	return tuskerError(errorAlreadyExists, "Evidence already exists at "+path, withPath(path))
}

func v7EvidenceIDTakenError(evidenceID, path string) error {
	return tuskerError(errorAlreadyExists, "Evidence ID is taken with different content: "+evidenceID, withPath(path))
}

func sweepStaleV7EvidenceTemps(dir, base string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	prefix := "." + base + ".tmp-"
	cutoff := time.Now().Add(-time.Hour)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), prefix) {
			continue
		}
		info, err := entry.Info()
		if err != nil || info.ModTime().After(cutoff) {
			continue
		}
		_ = os.Remove(filepath.Join(dir, entry.Name()))
	}
}

func v7EvidenceArtifactRequestMatches(taskID, evidenceID string, args Args, existing []string) bool {
	var requested []string
	if externalURL := strings.TrimSpace(args.String("external-url")); externalURL != "" {
		requested = append(requested, "external:"+externalURL)
	}
	inputs := splitCSV(firstNonEmpty(args.String("path"), args.String("artifact-paths")))
	if args.Bool("link-only") {
		for _, input := range inputs {
			requested = append(requested, "link-only:"+strings.TrimSpace(input))
		}
	} else {
		seenNames := map[string]int{}
		for _, input := range inputs {
			base := filepath.Base(input)
			seenNames[base]++
			if seenNames[base] > 1 {
				ext := filepath.Ext(base)
				stem := strings.TrimSuffix(base, ext)
				base = fmt.Sprintf("%s-%d%s", stem, seenNames[base], ext)
			}
			requested = append(requested, filepath.ToSlash(filepath.Join("evidence", taskID, "artifacts", evidenceID, base)))
		}
	}
	if len(requested) != len(existing) {
		return false
	}
	for i := range requested {
		if requested[i] != existing[i] {
			return false
		}
	}
	return true
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
	artifactDir := filepath.Join(vaultPath, "evidence", taskID, "artifacts", evidenceID)
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
		if err := copyV7EvidenceArtifact(source, target); err != nil {
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

func copyV7EvidenceArtifact(source, target string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := ensureDir(filepath.Dir(target)); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(target), "."+filepath.Base(target)+".tmp-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer func() {
		_ = temp.Close()
		_ = os.Remove(tempPath)
	}()
	if err := temp.Chmod(0o644); err != nil {
		return err
	}
	if _, err := io.Copy(temp, in); err != nil {
		return err
	}
	if err := temp.Sync(); err != nil {
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	// Publish the fully synced copy atomically.
	if err := os.Rename(tempPath, target); err != nil {
		return err
	}
	return syncV7DocumentDirectory(filepath.Dir(target))
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
