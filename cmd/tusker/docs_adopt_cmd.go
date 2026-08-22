package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"tusker/internal/docgraph"
)

type docsAdoptProposal struct {
	Path              string `json:"path"`
	Subject           string `json:"subject"`
	Disposition       string `json:"disposition"`
	Target            string `json:"target,omitempty"`
	Reason            string `json:"reason"`
	SourceFingerprint string `json:"source_fingerprint,omitempty"`
	Applied           bool   `json:"applied"`
}

const docsAdoptTableSchema = "tusker.docs-adopt/v1"

// docsAdoptTable is the reviewed adoption boundary. The fingerprint binds
// every proposed row and its source bytes; ApprovedBy is deliberately outside
// that digest and is checked against the explicit --by human actor at apply.
type docsAdoptTable struct {
	Schema      string              `json:"schema"`
	Fingerprint string              `json:"fingerprint"`
	ApprovedBy  string              `json:"approved_by,omitempty"`
	Proposals   []docsAdoptProposal `json:"proposals"`
}

type docsAdoptPrepared struct {
	proposal         docsAdoptProposal
	source           []byte
	target           []byte
	targetExists     bool
	successorSubject string
}

var docsAdoptApplyMu sync.Mutex

// docs adopt is the one mutation that may be explicitly authorized by the
// user while an agent is driving the CLI. The session namespace is local to
// this command; it must never become a general actor kind or break-glass
// escape hatch for other mutations.
func normalizeDocsAdoptActor(raw string) (string, string, bool) {
	parts := strings.SplitN(strings.TrimSpace(raw), ":", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	kind := strings.ToLower(strings.TrimSpace(parts[0]))
	name := strings.TrimSpace(parts[1])
	if name == "" || strings.ContainsAny(name, " \t\r\n") {
		return "", "", false
	}
	switch kind {
	case "human", "user-session":
		return kind + ":" + name, kind, true
	default:
		return "", "", false
	}
}

func parseDocsAdoptApprovalToken(raw string) (string, string, error) {
	raw = strings.TrimSpace(raw)
	separator := strings.LastIndexByte(raw, '@')
	if separator <= 0 || separator == len(raw)-1 {
		return "", "", tuskerError(errorInvalidField, "docs adopt --approval-token must be <actor>@<proposal-fingerprint>")
	}
	actor, kind, ok := normalizeDocsAdoptActor(raw[:separator])
	if !ok || kind != "user-session" {
		return "", "", tuskerError(errorInvalidField, "docs adopt --approval-token must identify a user-session actor")
	}
	fingerprint := strings.TrimSpace(raw[separator+1:])
	if !strings.HasPrefix(fingerprint, "sha256:") || len(strings.TrimPrefix(fingerprint, "sha256:")) != sha256.Size*2 {
		return "", "", tuskerError(errorInvalidField, "docs adopt --approval-token must contain a sha256 proposal fingerprint")
	}
	if _, err := hex.DecodeString(strings.TrimPrefix(fingerprint, "sha256:")); err != nil {
		return "", "", tuskerError(errorInvalidField, "docs adopt --approval-token contains an invalid proposal fingerprint")
	}
	return actor, fingerprint, nil
}

func docsAdoptApprovalActor(args Args, fingerprint string) (string, string, error) {
	rawBy := strings.TrimSpace(firstNonEmpty(args.String("by"), args.String("actor")))
	rawToken := strings.TrimSpace(args.String("approval-token"))
	tokenActor := ""
	if rawToken != "" {
		var tokenFingerprint string
		var err error
		tokenActor, tokenFingerprint, err = parseDocsAdoptApprovalToken(rawToken)
		if err != nil {
			return "", "", err
		}
		if tokenFingerprint != fingerprint {
			return "", "", tuskerError(errorInvalidTransition, "docs adopt approval token is bound to a different proposal fingerprint")
		}
		if rawBy == "" {
			rawBy = tokenActor
		}
	}
	actor, kind, ok := normalizeDocsAdoptActor(rawBy)
	if !ok {
		return "", "", tuskerError(errorInvalidField, "docs adopt approval requires --by human:<name> or --by user-session:<id>")
	}
	if tokenActor != "" && actor != tokenActor {
		return "", "", tuskerError(errorInvalidField, "docs adopt --approval-token actor must match explicit --by "+actor)
	}
	if kind == "human" {
		resolved, err := v7HumanActor(Args{"by": actor}, "docs adopt approval")
		if err != nil {
			return "", "", err
		}
		return resolved, "human", nil
	}
	if !strings.HasPrefix(agentSessionKind(), "interactive ") {
		return "", "", tuskerError(errorInvalidTransition,
			"docs adopt user-session approval requires an interactive agent session",
			withHint("run unattended adoption from a human terminal with --by human:<name>; user-session approval is not an agent break-glass flag"))
	}
	if rawToken != "" {
		return actor, "user-session-receipt", nil
	}
	return actor, "user-session", nil
}

func emitDocsAdoptAudit(vaultPath, eventKind, actor, approvalMethod, fingerprint, tablePath string, proposals []docsAdoptProposal, token string, applied bool, detail string) error {
	digest := strings.TrimPrefix(fingerprint, "sha256:")
	if len(digest) < 16 {
		return fmt.Errorf("documentation adoption audit requires a complete proposal fingerprint")
	}
	payload := map[string]any{
		"schema":               "tusker.docs-adopt-audit/v1",
		"proposal_fingerprint": fingerprint,
		"proposal_table":       filepath.Base(tablePath),
		"proposal_count":       len(proposals),
		"action_count":         len(docsAdoptActionRows(proposals)),
		"approval_method":      approvalMethod,
		"execution_role":       agentSessionKind(),
		"applied":              applied,
	}
	if token != "" {
		payload["approval_token_digest"] = docsAdoptBytesFingerprint([]byte(token))
	}
	if detail != "" {
		payload["detail"] = detail
	}
	objectID := "docs-adopt-" + digest[:16]
	return emitV7Event(vaultPath, objectID, "documentation", eventKind, actor, payload)
}

func docsAdoptBytesFingerprint(raw []byte) string {
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func docsAdoptTableFingerprint(proposals []docsAdoptProposal) string {
	// Applied is runtime output, not reviewed material. Keep it out of the
	// digest so a successful apply can report the same table identity.
	type fingerprintRow struct {
		Path              string `json:"path"`
		Subject           string `json:"subject"`
		Disposition       string `json:"disposition"`
		Target            string `json:"target,omitempty"`
		Reason            string `json:"reason"`
		SourceFingerprint string `json:"source_fingerprint,omitempty"`
	}
	rows := make([]fingerprintRow, 0, len(proposals))
	for _, proposal := range proposals {
		rows = append(rows, fingerprintRow{
			Path: proposal.Path, Subject: proposal.Subject,
			Disposition: proposal.Disposition, Target: proposal.Target,
			Reason: proposal.Reason, SourceFingerprint: proposal.SourceFingerprint,
		})
	}
	raw, _ := json.Marshal(rows)
	return docsAdoptBytesFingerprint(raw)
}

func docsAdoptActionRows(proposals []docsAdoptProposal) []docsAdoptProposal {
	actions := make([]docsAdoptProposal, 0, len(proposals))
	for _, proposal := range proposals {
		if !strings.EqualFold(strings.TrimSpace(proposal.Disposition), "leave") {
			actions = append(actions, proposal)
		}
	}
	return actions
}

func loadDocsAdoptTable(path, repoRoot string) (docsAdoptTable, error) {
	if strings.TrimSpace(path) == "" {
		return docsAdoptTable{}, fmt.Errorf("documentation adoption table path is empty")
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(repoRoot, filepath.FromSlash(path))
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return docsAdoptTable{}, fmt.Errorf("read documentation adoption table %s: %w", path, err)
	}
	var table docsAdoptTable
	if err := json.Unmarshal(raw, &table); err != nil {
		return docsAdoptTable{}, fmt.Errorf("parse documentation adoption table %s: %w", path, err)
	}
	if table.Schema != docsAdoptTableSchema {
		return docsAdoptTable{}, fmt.Errorf("documentation adoption table schema %q is unsupported", table.Schema)
	}
	if strings.TrimSpace(table.Fingerprint) == "" {
		return docsAdoptTable{}, fmt.Errorf("documentation adoption table is missing fingerprint")
	}
	return table, nil
}

func preflightDocsAdoptTable(repoRoot string, proposals []docsAdoptProposal) ([]docsAdoptPrepared, error) {
	prepared := make([]docsAdoptPrepared, 0, len(proposals))
	seenPaths := map[string]string{}
	seenTargets := map[string]string{}
	for _, proposal := range proposals {
		key := filepath.ToSlash(filepath.Clean(filepath.FromSlash(proposal.Path)))
		if previous, exists := seenPaths[key]; exists {
			return nil, fmt.Errorf("documentation adoption table repeats source %s (rows %s and %s)", key, previous, proposal.Path)
		}
		seenPaths[key] = proposal.Path
		if !strings.EqualFold(strings.TrimSpace(proposal.Disposition), "leave") {
			target := filepath.ToSlash(filepath.Clean(filepath.FromSlash(proposal.Target)))
			if previous, exists := seenTargets[target]; exists {
				return nil, fmt.Errorf("documentation adoption table repeats target %s (rows %s and %s)", target, previous, proposal.Path)
			}
			seenTargets[target] = proposal.Path
		}
		item, err := prepareDocsAdoptProposal(repoRoot, proposal, true)
		if err != nil {
			return nil, err
		}
		prepared = append(prepared, item)
	}
	return prepared, nil
}

func prepareDocsAdoptProposal(repoRoot string, proposal docsAdoptProposal, requireFingerprint bool) (docsAdoptPrepared, error) {
	disposition := strings.ToLower(strings.TrimSpace(proposal.Disposition))
	relative := filepath.ToSlash(filepath.Clean(filepath.FromSlash(proposal.Path)))
	if disposition == "leave" {
		return docsAdoptPrepared{proposal: proposal}, nil
	}
	if docsAdoptLeave(relative, strings.ToLower(filepath.Base(relative))) || docsAdoptSkipDir(filepath.ToSlash(filepath.Dir(relative))) {
		return docsAdoptPrepared{}, fmt.Errorf("documentation adoption protected source must remain leave: %s", proposal.Path)
	}
	canonicalRepoRoot, err := docsAdoptCanonicalRoot(repoRoot, "repository")
	if err != nil {
		return docsAdoptPrepared{}, err
	}
	repoRoot = canonicalRepoRoot
	if symlinkPath, symlinkErr := docsAdoptSymlinkPath(repoRoot, proposal.Path); symlinkErr != nil {
		return docsAdoptPrepared{}, symlinkErr
	} else if symlinkPath != "" {
		return docsAdoptPrepared{}, fmt.Errorf("documentation adoption refuses symlinked legacy source: %s", proposal.Path)
	}
	source, err := docgraph.ReadDocumentFile(repoRoot, proposal.Path)
	if err != nil {
		return docsAdoptPrepared{}, err
	}
	if strings.TrimSpace(proposal.Subject) == "" {
		return docsAdoptPrepared{}, fmt.Errorf("documentation adoption requires a subject for %s", proposal.Path)
	}
	if strings.TrimSpace(proposal.Target) == "" {
		return docsAdoptPrepared{}, fmt.Errorf("documentation adoption requires a successor target for %s", proposal.Path)
	}
	if requireFingerprint && strings.TrimSpace(proposal.SourceFingerprint) == "" {
		return docsAdoptPrepared{}, fmt.Errorf("documentation adoption table is missing source fingerprint for %s", proposal.Path)
	}
	if expected := strings.TrimSpace(proposal.SourceFingerprint); expected != "" && expected != docsAdoptBytesFingerprint(source) {
		return docsAdoptPrepared{}, fmt.Errorf("documentation adoption source changed after review: %s", proposal.Path)
	}
	if actualSubject := docsAdoptSubject(proposal.Path, source); disposition != "merge" && !strings.EqualFold(strings.TrimSpace(actualSubject), strings.TrimSpace(proposal.Subject)) {
		return docsAdoptPrepared{}, fmt.Errorf("documentation adoption source subject changed after review: %s", proposal.Path)
	}
	targetPath := filepath.Join(repoRoot, filepath.FromSlash(proposal.Target))
	if symlinkPath, symlinkErr := docsAdoptSymlinkPath(repoRoot, proposal.Target); symlinkErr != nil {
		return docsAdoptPrepared{}, symlinkErr
	} else if symlinkPath != "" {
		return docsAdoptPrepared{}, fmt.Errorf("documentation adoption refuses symlink target or parent: %s", proposal.Target)
	}
	prepared := docsAdoptPrepared{proposal: proposal, source: source}
	cleanTarget := filepath.ToSlash(filepath.Clean(filepath.FromSlash(proposal.Target)))
	if !strings.HasPrefix(cleanTarget, "docs/system/") {
		return docsAdoptPrepared{}, fmt.Errorf("documentation adoption successor must be under docs/system: %s", proposal.Target)
	}
	if relative == cleanTarget {
		return docsAdoptPrepared{}, fmt.Errorf("documentation adoption source and successor must differ: %s", proposal.Path)
	}
	if fileExists(targetPath) {
		prepared.targetExists = true
		prepared.target, err = os.ReadFile(targetPath)
		if err != nil {
			return docsAdoptPrepared{}, err
		}
		targetDoc, parseErr := docgraph.ParseDocHeaders(proposal.Target, prepared.target)
		if parseErr != nil {
			return docsAdoptPrepared{}, fmt.Errorf("documentation adoption target is not a canonical document: %s: %w", proposal.Target, parseErr)
		}
		subjectMatches := strings.EqualFold(strings.TrimSpace(targetDoc.Subject), strings.TrimSpace(proposal.Subject))
		switch disposition {
		case "promote", "merge":
			if !subjectMatches {
				return docsAdoptPrepared{}, fmt.Errorf("documentation adoption refuses canonical target collision: %s", proposal.Target)
			}
		case "tombstone":
			if subjectMatches || strings.TrimSpace(targetDoc.Subject) == "" {
				return docsAdoptPrepared{}, fmt.Errorf("documentation adoption tombstone target must be a different canonical subject: %s", proposal.Target)
			}
			prepared.successorSubject = strings.TrimSpace(targetDoc.Subject)
		default:
			return docsAdoptPrepared{}, fmt.Errorf("unknown documentation adoption disposition %q", proposal.Disposition)
		}
	} else {
		switch disposition {
		case "promote":
			// A promote may create its canonical target.
		case "merge":
			return docsAdoptPrepared{}, fmt.Errorf("cannot merge %s: canonical target does not exist: %s", proposal.Path, proposal.Target)
		case "tombstone":
			return docsAdoptPrepared{}, fmt.Errorf("cannot tombstone %s: successor target does not exist: %s", proposal.Path, proposal.Target)
		default:
			return docsAdoptPrepared{}, fmt.Errorf("unknown documentation adoption disposition %q", proposal.Disposition)
		}
	}
	if disposition == "tombstone" && !strings.HasPrefix(filepath.ToSlash(filepath.Clean(filepath.FromSlash(proposal.Target))), "docs/system/") {
		return docsAdoptPrepared{}, fmt.Errorf("documentation adoption tombstone successor must be under docs/system: %s", proposal.Target)
	}
	return prepared, nil
}

func applyPreparedDocsAdoptTable(repoRoot string, prepared []docsAdoptPrepared) error {
	docsAdoptApplyMu.Lock()
	defer docsAdoptApplyMu.Unlock()
	rollback, err := snapshotDocsAdoptBatch(repoRoot, prepared)
	if err != nil {
		return err
	}
	for _, item := range prepared {
		if strings.EqualFold(strings.TrimSpace(item.proposal.Disposition), "leave") {
			continue
		}
		if err := verifyPreparedDocsAdoptCAS(repoRoot, item); err != nil {
			return err
		}
	}
	for _, item := range prepared {
		if strings.EqualFold(strings.TrimSpace(item.proposal.Disposition), "leave") {
			continue
		}
		if err := applyPreparedDocsAdoptProposal(repoRoot, item); err != nil {
			if rollbackErr := restoreDocsAdoptBatch(repoRoot, rollback); rollbackErr != nil {
				return fmt.Errorf("%w (documentation adoption rollback failed: %v)", err, rollbackErr)
			}
			return err
		}
	}
	return nil
}

type docsAdoptRollbackEntry struct {
	relative string
	exists   bool
	content  []byte
}

func snapshotDocsAdoptBatch(repoRoot string, prepared []docsAdoptPrepared) ([]docsAdoptRollbackEntry, error) {
	seen := map[string]bool{}
	var rollback []docsAdoptRollbackEntry
	for _, item := range prepared {
		if strings.EqualFold(strings.TrimSpace(item.proposal.Disposition), "leave") {
			continue
		}
		for _, relative := range []string{item.proposal.Path, item.proposal.Target} {
			clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(relative)))
			if seen[clean] {
				continue
			}
			seen[clean] = true
			if symlinkPath, err := docsAdoptSymlinkPath(repoRoot, clean); err != nil {
				return nil, err
			} else if symlinkPath != "" {
				return nil, fmt.Errorf("documentation adoption refuses symlinked batch path: %s", relative)
			}
			content, err := docgraph.ReadDocumentFile(repoRoot, clean)
			if err == nil {
				rollback = append(rollback, docsAdoptRollbackEntry{relative: clean, exists: true, content: content})
				continue
			}
			if !os.IsNotExist(err) {
				return nil, err
			}
			rollback = append(rollback, docsAdoptRollbackEntry{relative: clean})
		}
	}
	return rollback, nil
}

func verifyPreparedDocsAdoptCAS(repoRoot string, prepared docsAdoptPrepared) error {
	proposal := prepared.proposal
	currentSource, err := docgraph.ReadDocumentFile(repoRoot, proposal.Path)
	if err != nil || !bytes.Equal(currentSource, prepared.source) {
		return fmt.Errorf("documentation adoption source changed during approval: %s", proposal.Path)
	}
	if prepared.targetExists {
		currentTarget, err := docgraph.ReadDocumentFile(repoRoot, proposal.Target)
		if err != nil || !bytes.Equal(currentTarget, prepared.target) {
			return fmt.Errorf("documentation adoption target changed during approval: %s", proposal.Target)
		}
		return nil
	}
	if _, err := os.Lstat(filepath.Join(repoRoot, filepath.FromSlash(proposal.Target))); err == nil {
		return fmt.Errorf("documentation adoption target appeared during approval: %s", proposal.Target)
	} else if !os.IsNotExist(err) {
		return err
	}
	return nil
}

func restoreDocsAdoptBatch(repoRoot string, rollback []docsAdoptRollbackEntry) error {
	var errs []string
	for i := len(rollback) - 1; i >= 0; i-- {
		entry := rollback[i]
		if entry.exists {
			if err := docgraph.WriteDocumentFile(repoRoot, entry.relative, entry.content); err != nil {
				errs = append(errs, fmt.Sprintf("%s: %v", entry.relative, err))
			}
			continue
		}
		if err := docgraph.RemoveDocumentFile(repoRoot, entry.relative); err != nil && !os.IsNotExist(err) {
			errs = append(errs, fmt.Sprintf("%s: %v", entry.relative, err))
		}
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

func applyPreparedDocsAdoptProposal(repoRoot string, prepared docsAdoptPrepared) error {
	proposal := prepared.proposal
	disposition := strings.ToLower(strings.TrimSpace(proposal.Disposition))
	switch disposition {
	case "promote":
		if !prepared.targetExists {
			created := time.Now().Local().Format("2006-01-02")
			content := fmt.Sprintf("---\nsubject: %s\nkeywords: []\npart_of: overview\ndescribes: []\nstatus: canonical\ncreated: %s\nlast_verified:\nread_when: %s\nskip_when: \"\"\nsources: [%s]\n---\n\n%s", strconv.Quote(proposal.Subject), created, strconv.Quote("You need the current truth for "+proposal.Subject+"."), strconv.Quote(proposal.Path), docsAdoptBody(prepared.source))
			return docsAdoptWriteText(repoRoot, proposal.Target, content)
		}
		return mergeDocsAdoptSource(repoRoot, proposal.Target, proposal.Path, prepared.source)
	case "merge":
		return mergeDocsAdoptSource(repoRoot, proposal.Target, proposal.Path, prepared.source)
	case "tombstone":
		content := docsAdoptTombstone(prepared.source, proposal, prepared.successorSubject)
		return docsAdoptWriteText(repoRoot, proposal.Path, content)
	default:
		return fmt.Errorf("unknown documentation adoption disposition %q", proposal.Disposition)
	}
}

func docsAdoptTombstone(source []byte, proposal docsAdoptProposal, successorSubject string) string {
	partOf, decidesFor := "", ""
	if data, _, err := parseFrontmatter(string(source)); err == nil && data != nil {
		partOf = strings.TrimSpace(fmt.Sprint(data["part_of"]))
		decidesFor = strings.TrimSpace(fmt.Sprint(data["decides_for"]))
		if partOf == "<nil>" {
			partOf = ""
		}
		if decidesFor == "<nil>" {
			decidesFor = ""
		}
	}
	var b strings.Builder
	b.WriteString("---\nsubject: ")
	b.WriteString(strconv.Quote(proposal.Subject))
	b.WriteString("\nstatus: superseded\nsuperseded_by: ")
	b.WriteString(strconv.Quote(successorSubject))
	if partOf != "" {
		b.WriteString("\npart_of: ")
		b.WriteString(strconv.Quote(partOf))
	}
	if decidesFor != "" {
		b.WriteString("\ndecides_for: ")
		b.WriteString(strconv.Quote(decidesFor))
	}
	b.WriteString("\n---\n\nThis document is superseded; read [[")
	b.WriteString(successorSubject)
	b.WriteString("]].\n")
	return b.String()
}

// docsAdoptCmd is deliberately a batch operation: inventory and propose by
// default; mutate only after the operator approves a reviewed, fingerprinted
// table. Adoption never deletes a source; tombstones rewrite one to a durable
// signpost only when that exact row was approved.
func docsAdoptCmd(args Args) error {
	if _, present := args["apply"]; present {
		return tuskerError(errorInvalidArg, "docs adopt accepts --approve only; --apply and --yes are unsupported")
	}
	if _, present := args["yes"]; present {
		return tuskerError(errorInvalidArg, "docs adopt accepts --approve only; --apply and --yes are unsupported")
	}
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	if err := docsAdoptValidateRoots(v7RepoRoot(vaultPath), vaultPath); err != nil {
		return err
	}
	canonicalVault, err := docsAdoptCanonicalRoot(vaultPath, "vault")
	if err != nil {
		return err
	}
	repoRoot, err := docsAdoptCanonicalRoot(filepath.Dir(canonicalVault), "repository")
	if err != nil {
		return err
	}
	var table docsAdoptTable
	tablePath := strings.TrimSpace(firstNonEmpty(args.String("table"), args.String("proposal-table")))
	if tablePath != "" {
		table, err = loadDocsAdoptTable(tablePath, repoRoot)
		if err != nil {
			return err
		}
	} else {
		corpus, _, err := docgraph.LoadRepository(repoRoot)
		if err != nil {
			return err
		}
		proposals, inventoryErr := inventoryDocsAdopt(repoRoot, corpus)
		if inventoryErr != nil {
			return inventoryErr
		}
		table = docsAdoptTable{Schema: docsAdoptTableSchema, Proposals: proposals}
		table.Fingerprint = docsAdoptTableFingerprint(table.Proposals)
	}
	proposals := table.Proposals
	// --dry-run is an explicit read-only fence, even if an operator accidentally
	// combines it with --approve.
	approved := args.Bool("approve") && !args.Bool("dry-run")
	applied := false
	if approved && tablePath == "" && len(docsAdoptActionRows(proposals)) > 0 {
		return tuskerError(errorInvalidTransition, "docs adopt --approve requires an explicit reviewed proposal table; save the dry-run JSON, review every row, set approved_by, then pass --table <file>")
	}
	if approved && len(docsAdoptActionRows(proposals)) > 0 {
		actor, approvalMethod, actorErr := docsAdoptApprovalActor(args, table.Fingerprint)
		if actorErr != nil {
			return actorErr
		}
		if strings.TrimSpace(table.ApprovedBy) == "" {
			return tuskerError(errorInvalidTransition, "docs adopt approval table requires approved_by: "+actor)
		}
		approvedBy, _, ok := normalizeDocsAdoptActor(table.ApprovedBy)
		if !ok || approvedBy != actor {
			return tuskerError(errorInvalidField, "docs adopt approval table approved_by must match explicit --by "+actor)
		}
		if table.Fingerprint != docsAdoptTableFingerprint(proposals) {
			return tuskerError(errorInvalidTransition, "docs adopt approval table fingerprint does not match reviewed rows; regenerate the fingerprint after editing the table")
		}
		prepared, preflightErr := preflightDocsAdoptTable(repoRoot, proposals)
		if preflightErr != nil {
			return preflightErr
		}
		if err := emitDocsAdoptAudit(vaultPath, "docs_adopt_approved", actor, approvalMethod, table.Fingerprint, tablePath, proposals, args.String("approval-token"), false, "reviewed table passed preflight"); err != nil {
			return fmt.Errorf("documentation adoption approval audit failed: %w", err)
		}
		if err := applyPreparedDocsAdoptTable(repoRoot, prepared); err != nil {
			if auditErr := emitDocsAdoptAudit(vaultPath, "docs_adopt_failed", actor, approvalMethod, table.Fingerprint, tablePath, proposals, args.String("approval-token"), false, err.Error()); auditErr != nil {
				return fmt.Errorf("%w (failure audit failed: %v)", err, auditErr)
			}
			return err
		}
		if err := emitDocsAdoptAudit(vaultPath, "docs_adopt_applied", actor, approvalMethod, table.Fingerprint, tablePath, proposals, args.String("approval-token"), true, "reviewed table applied"); err != nil {
			return fmt.Errorf("documentation adoption applied but completion audit failed: %w", err)
		}
		for i := range proposals {
			proposals[i].Applied = true
		}
		applied = true
	}
	if args.Bool("json") {
		emitJSON(map[string]any{
			"schema":      table.Schema,
			"fingerprint": table.Fingerprint,
			"approved_by": table.ApprovedBy,
			"ok":          true,
			"approved":    approved,
			"applied":     applied,
			"map":         "not regenerated; run `tusker docs map` after review",
			"proposals":   proposals,
			"scope":       "markdown outside docs/system and .tusker/specs; generated/runtime trees omitted",
		})
		return nil
	}
	if len(proposals) == 0 {
		fmt.Println("No legacy Markdown files found outside the managed documentation tree.")
		return nil
	}
	if !approved {
		fmt.Println("Documentation adoption proposal (dry run; nothing changed):")
	} else {
		fmt.Println("Documentation adoption proposal applied:")
	}
	for _, proposal := range proposals {
		line := fmt.Sprintf("- %-9s %s", proposal.Disposition, proposal.Path)
		if proposal.Target != "" {
			line += " -> " + proposal.Target
		}
		if proposal.Reason != "" {
			line += " (" + proposal.Reason + ")"
		}
		fmt.Println(line)
	}
	if !approved {
		fmt.Printf("Review this table, set approved_by, then run `tusker docs adopt --table <file> --approve --by human:<name>` (or an explicit user-session approval in an interactive agent session; fingerprint %s); no file is changed by this run.\n", table.Fingerprint)
	} else if applied {
		fmt.Println("Generated map artifacts were not changed; run `tusker docs map` after reviewing the adopted canonical docs.")
	}
	return nil
}

func inventoryDocsAdopt(repoRoot string, corpus docgraph.Corpus) ([]docsAdoptProposal, error) {
	if err := docsAdoptValidateRoots(repoRoot, ""); err != nil {
		return nil, err
	}
	known := map[string][]docgraph.Document{}
	for _, doc := range corpus.Documents {
		subject := strings.ToLower(strings.TrimSpace(doc.Subject))
		if subject != "" {
			known[subject] = append(known[subject], doc)
		}
	}
	var paths []string
	symlinkSources := map[string]bool{}
	err := filepath.WalkDir(repoRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			rel, _ := filepath.Rel(repoRoot, path)
			if rel != "." && docsAdoptSkipDir(filepath.ToSlash(rel)) {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(entry.Name()), ".md") {
			return nil
		}
		rel, err := filepath.Rel(repoRoot, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if docsAdoptManagedPath(rel) || docsAdoptSkipDir(filepath.ToSlash(filepath.Dir(rel))) {
			return nil
		}
		if info, statErr := os.Lstat(path); statErr != nil {
			return statErr
		} else if info.Mode()&os.ModeSymlink != 0 {
			symlinkSources[rel] = true
		}
		paths = append(paths, rel)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	type candidate struct {
		rel               string
		subject           string
		sourceFingerprint string
		symlink           bool
	}
	candidates := make([]candidate, 0, len(paths))
	subjectPaths := map[string][]string{}
	targetPaths := map[string][]string{}
	for _, rel := range paths {
		if symlinkSources[rel] {
			candidates = append(candidates, candidate{rel: rel, symlink: true})
			continue
		}
		raw, err := docgraph.ReadDocumentFile(repoRoot, rel)
		if err != nil {
			return nil, err
		}
		subject := docsAdoptSubject(rel, raw)
		candidates = append(candidates, candidate{rel: rel, subject: subject, sourceFingerprint: docsAdoptBytesFingerprint(raw)})
		normalizedSubject := strings.ToLower(strings.TrimSpace(subject))
		if normalizedSubject != "" {
			subjectPaths[normalizedSubject] = append(subjectPaths[normalizedSubject], rel)
		}
		if !docsAdoptUntitledSubject(subject) {
			target := filepath.ToSlash(filepath.Join("docs/system", docsSubjectSlug(subject)+".md"))
			targetPaths[target] = append(targetPaths[target], rel)
		}
	}
	var proposals []docsAdoptProposal
	for _, item := range candidates {
		rel := item.rel
		proposal := docsAdoptProposal{Path: rel, Disposition: "promote", Reason: "legacy Markdown", SourceFingerprint: item.sourceFingerprint}
		base := strings.ToLower(filepath.Base(rel))
		if item.symlink {
			proposal.Disposition, proposal.Reason = "leave", "legacy source is a symlink; manual review required"
			proposals = append(proposals, proposal)
			continue
		}
		if docsAdoptLeave(rel, base) {
			proposal.Disposition, proposal.Reason = "leave", "README, policy, or repository instruction"
			proposals = append(proposals, proposal)
			continue
		}
		subject := item.subject
		proposal.Subject = subject
		normalizedSubject := strings.ToLower(strings.TrimSpace(subject))
		if docsAdoptUntitledSubject(subject) {
			proposal.Disposition, proposal.Reason = "leave", "untitled or missing subject; manual review required"
			proposals = append(proposals, proposal)
			continue
		}
		if paths := subjectPaths[normalizedSubject]; len(paths) > 1 {
			proposal.Disposition, proposal.Reason = "leave", "multiple legacy files share this subject; manual merge required"
			proposals = append(proposals, proposal)
			continue
		}
		if current := known[normalizedSubject]; len(current) > 1 {
			proposal.Disposition, proposal.Reason = "leave", "canonical subject is ambiguous; manual merge required"
			proposals = append(proposals, proposal)
			continue
		} else if len(current) == 1 {
			proposal.Target = current[0].Path
			if symlinkPath, symlinkErr := docsAdoptSymlinkPath(repoRoot, proposal.Target); symlinkErr != nil {
				return nil, symlinkErr
			} else if symlinkPath != "" {
				proposal.Disposition, proposal.Reason = "leave", "canonical target is a symlink; manual review required"
			} else {
				proposal.Disposition, proposal.Reason = "merge", "subject already has a canonical owner"
			}
			proposals = append(proposals, proposal)
			continue
		}
		proposal.Target = filepath.ToSlash(filepath.Join("docs/system", docsSubjectSlug(subject)+".md"))
		if paths := targetPaths[proposal.Target]; len(paths) > 1 {
			proposal.Disposition, proposal.Reason = "leave", "multiple legacy files map to the same canonical target; manual merge required"
			proposals = append(proposals, proposal)
			continue
		}
		if symlinkPath, symlinkErr := docsAdoptSymlinkPath(repoRoot, proposal.Target); symlinkErr != nil {
			return nil, symlinkErr
		} else if symlinkPath != "" {
			proposal.Disposition, proposal.Reason = "leave", "canonical target path crosses a symlink; manual review required"
			proposals = append(proposals, proposal)
			continue
		}
		targetPath := filepath.Join(repoRoot, filepath.FromSlash(proposal.Target))
		if targetInfo, statErr := os.Lstat(targetPath); statErr == nil {
			if targetInfo.Mode()&os.ModeSymlink != 0 {
				proposal.Disposition, proposal.Reason = "leave", "canonical target is a symlink; manual review required"
				proposals = append(proposals, proposal)
				continue
			}
			targetRaw, readErr := docgraph.ReadDocumentFile(repoRoot, proposal.Target)
			if readErr != nil {
				return nil, readErr
			}
			targetDoc, parseErr := docgraph.ParseDocHeaders(proposal.Target, targetRaw)
			if parseErr != nil || !strings.EqualFold(strings.TrimSpace(targetDoc.Subject), strings.TrimSpace(subject)) {
				proposal.Disposition, proposal.Reason = "leave", "canonical target path collision; manual review required"
			} else {
				proposal.Disposition, proposal.Reason = "merge", "canonical target path already exists"
			}
		} else if !os.IsNotExist(statErr) {
			return nil, statErr
		}
		proposals = append(proposals, proposal)
	}
	return proposals, nil
}

func docsAdoptManagedPath(relative string) bool {
	return relative == "docs/system" || strings.HasPrefix(relative, "docs/system/") || relative == ".tusker/specs" || strings.HasPrefix(relative, ".tusker/specs/")
}

func docsAdoptSkipDir(relative string) bool {
	if relative == "." || relative == "" {
		return false
	}
	for _, prefix := range []string{
		".git", ".tusker", ".tusker-worktrees", ".tusker-runtime", ".tusker-state",
		".chatgpt-handoff", ".agents", ".claude", ".github", ".tools", "vendor", "node_modules", "dist", "build",
		"artifacts", "site", "tmp", "coverage", "out", "target", "skills/tusker", "skills/spec",
	} {
		if relative == prefix || strings.HasPrefix(relative, prefix+"/") {
			return true
		}
	}
	return false
}

func docsAdoptUntitledSubject(subject string) bool {
	normalized := strings.ToLower(strings.TrimSpace(subject))
	switch normalized {
	case "", "untitled", "untitled document", "new document", "document":
		return true
	default:
		return strings.HasPrefix(normalized, "untitled ") || strings.HasPrefix(normalized, "untitled-") || strings.HasPrefix(normalized, "new document ")
	}
}

func docsAdoptValidateRoots(repoRoot, vaultPath string) error {
	for _, root := range []struct {
		label string
		path  string
	}{
		{label: "repository", path: repoRoot},
		{label: "vault", path: vaultPath},
	} {
		label, path := root.label, root.path
		if strings.TrimSpace(path) == "" {
			continue
		}
		if _, err := docsAdoptCanonicalRoot(path, label); err != nil {
			return err
		}
	}
	return nil
}

func docsAdoptCanonicalRoot(path, label string) (string, error) {
	abs, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return "", fmt.Errorf("documentation adoption %s root is unavailable: %w", label, err)
	}
	info, err := os.Lstat(abs)
	if err != nil {
		return "", fmt.Errorf("documentation adoption %s root is unavailable: %w", label, err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("documentation adoption refuses symlinked %s root: %s", label, abs)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("documentation adoption %s root cannot be canonicalized: %w", label, err)
	}
	resolved, err = filepath.Abs(filepath.Clean(resolved))
	if err != nil {
		return "", fmt.Errorf("documentation adoption %s root cannot be canonicalized: %w", label, err)
	}
	resolvedInfo, err := os.Lstat(resolved)
	if err != nil || !resolvedInfo.IsDir() || resolvedInfo.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("documentation adoption refuses non-directory %s root: %s", label, resolved)
	}
	return resolved, nil
}

// docsAdoptSymlinkPath rejects both a symlinked target and a symlinked parent.
// os.WriteFile follows parent symlinks, so checking only the final target would
// still allow an adoption to write outside the repository.
func docsAdoptSymlinkPath(repoRoot, relative string) (string, error) {
	current := repoRoot
	clean := filepath.Clean(filepath.FromSlash(relative))
	if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("documentation adoption target escapes repository: %s", relative)
	}
	for _, part := range strings.Split(clean, string(os.PathSeparator)) {
		if part == "." || part == "" {
			continue
		}
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			return "", nil
		}
		if err != nil {
			return "", err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return current, nil
		}
	}
	return "", nil
}

func docsAdoptLeave(relative, base string) bool {
	if strings.HasPrefix(relative, ".github/") || strings.HasPrefix(relative, ".agents/") || strings.HasPrefix(relative, ".claude/") {
		return true
	}
	switch strings.ToLower(filepath.ToSlash(relative)) {
	case "workflow.md", "skill.md", "dossier.md", "narrative-notes.md":
		return true
	}
	switch base {
	case "readme.md", "license.md", "copying.md", "changelog.md", "contributing.md", "agents.md", "claude.md":
		return true
	default:
		return false
	}
}

func docsAdoptSubject(relative string, raw []byte) string {
	if data, _, err := parseFrontmatter(string(raw)); err == nil && data != nil {
		if value, ok := data["subject"]; ok && value != nil {
			subject := strings.TrimSpace(fmt.Sprint(value))
			if subject != "" && subject != "<nil>" {
				return subject
			}
		}
	}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(line, "#"))
		if line != "" {
			return line
		}
	}
	return strings.TrimSuffix(filepath.Base(relative), filepath.Ext(relative))
}

func applyDocsAdoptProposal(repoRoot string, proposal docsAdoptProposal) error {
	prepared, err := prepareDocsAdoptProposal(repoRoot, proposal, false)
	if err != nil {
		return err
	}
	return applyPreparedDocsAdoptTable(repoRoot, []docsAdoptPrepared{prepared})
}

func docsAdoptBody(raw []byte) string {
	if _, body, err := parseFrontmatter(string(raw)); err == nil {
		return strings.TrimSpace(body)
	}
	return strings.TrimSpace(string(raw))
}

func docsAdoptWriteText(repoRoot, relative, content string) error {
	if err := docgraph.WriteDocumentFile(repoRoot, relative, []byte(content)); err != nil {
		return err
	}
	path := filepath.Join(repoRoot, filepath.FromSlash(relative))
	invalidateCachedNote(path)
	recordCLIVaultMutation(path)
	return nil
}

func mergeDocsAdoptSource(repoRoot, targetPath, sourcePath string, source []byte) error {
	content, err := docgraph.ReadDocumentFile(repoRoot, targetPath)
	if err != nil {
		return err
	}
	marker := "<!-- tusker:adopted-source:" + filepath.ToSlash(sourcePath) + " -->"
	if strings.Contains(string(content), marker) {
		return nil
	}
	addition := "\n\n## Adopted legacy material\n\n" + marker + "\n\n" + docsAdoptBody(source) + "\n"
	return docsAdoptWriteText(repoRoot, targetPath, string(content)+addition)
}
