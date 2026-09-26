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
	"os/exec"
	"path/filepath"
	"regexp"
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
	// Kind/Lifecycle/Conformance/Evidence select the portable identity of a
	// migrated document explicitly. Empty values take the per-kind S46
	// defaults (doc/current, proposal/proposed, decision/proposed, unverified)
	// at apply time; implemented/matches/drift require Evidence.
	Kind        string `json:"kind,omitempty"`
	Lifecycle   string `json:"lifecycle,omitempty"`
	Conformance string `json:"conformance,omitempty"`
	Evidence    string `json:"evidence,omitempty"`
	// Links inventories the relative Markdown links/assets found in the
	// source body. Links pointing at co-migrated sources are repaired on
	// apply; every other link is preserved byte-identical for review.
	Links []string `json:"links,omitempty"`
	// StubSource marks a legacy vault source (.tusker/specs, knowledge
	// domains) that becomes a subject-less forwarding stub once its content
	// has a canonical owner, so the old path forwards without duplicating
	// the current subject. The original bytes are kept in the recovery
	// journal. Outside-tree sources always keep their originals.
	StubSource bool `json:"stub_source,omitempty"`
	// ActiveRefs inventories the active task files whose spec_refs name this
	// source path. It is workspace state, not reviewed material, so it stays
	// out of the approval fingerprint; preflight re-derives it live and
	// refuses overlapping active work before any damaging write.
	ActiveRefs []string `json:"active_refs,omitempty"`
	Applied    bool     `json:"applied"`
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
	// alreadyApplied marks a row whose post-apply state is already on disk
	// (forwarding stub present, merge marker present). Apply skips it so a
	// resumed or repeated apply is idempotent.
	alreadyApplied bool
	migKind        docgraph.Kind
	migLifecycle   string
	migConformance string
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
		Path              string   `json:"path"`
		Subject           string   `json:"subject"`
		Disposition       string   `json:"disposition"`
		Target            string   `json:"target,omitempty"`
		Reason            string   `json:"reason"`
		SourceFingerprint string   `json:"source_fingerprint,omitempty"`
		Kind              string   `json:"kind,omitempty"`
		Lifecycle         string   `json:"lifecycle,omitempty"`
		Conformance       string   `json:"conformance,omitempty"`
		Evidence          string   `json:"evidence,omitempty"`
		Links             []string `json:"links,omitempty"`
		StubSource        bool     `json:"stub_source,omitempty"`
	}
	rows := make([]fingerprintRow, 0, len(proposals))
	for _, proposal := range proposals {
		rows = append(rows, fingerprintRow{
			Path: proposal.Path, Subject: proposal.Subject,
			Disposition: proposal.Disposition, Target: proposal.Target,
			Reason: proposal.Reason, SourceFingerprint: proposal.SourceFingerprint,
			Kind: proposal.Kind, Lifecycle: proposal.Lifecycle,
			Conformance: proposal.Conformance, Evidence: proposal.Evidence,
			Links: proposal.Links, StubSource: proposal.StubSource,
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
	migKind, migLifecycle, migConformance, err := docsAdoptRowIdentity(proposal)
	if err != nil {
		return docsAdoptPrepared{}, err
	}
	// Legacy vault sources (.tusker/specs, knowledge domains) are adopted
	// only through explicit reviewed migration rows, so the protected-tree
	// guard below does not apply to them. Every other guard (fingerprints,
	// collisions, symlinks, dirty inputs, active references) still holds.
	if !docsAdoptIsLegacyVaultSource(proposal.Path) &&
		(docsAdoptLeave(relative, strings.ToLower(filepath.Base(relative))) || docsAdoptSkipDir(filepath.ToSlash(filepath.Dir(relative)))) {
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
	// A resumed or repeated apply meets the forwarding stub left by the
	// first apply instead of the reviewed bytes. The stub carries the exact
	// successor, so recognizing it here keeps the repeat idempotent instead
	// of reporting source drift.
	if stubbed, stubErr := docsAdoptSourceIsExpectedStub(repoRoot, proposal, source); stubErr != nil {
		return docsAdoptPrepared{}, stubErr
	} else if stubbed {
		return docsAdoptPrepared{proposal: proposal, source: source, alreadyApplied: true,
			migKind: migKind, migLifecycle: migLifecycle, migConformance: migConformance}, nil
	}
	if migKind == docgraph.KindDecision && docsAdoptDecidesFor(source) == "" {
		return docsAdoptPrepared{}, fmt.Errorf("documentation adoption row %s migrates a decision without decides_for: record the settled subject or leave the row for manual review", proposal.Path)
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
	prepared := docsAdoptPrepared{proposal: proposal, source: source,
		migKind: migKind, migLifecycle: migLifecycle, migConformance: migConformance}
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

func applyPreparedDocsAdoptProposalMoves(repoRoot string, prepared docsAdoptPrepared, moves map[string]string) error {
	proposal := prepared.proposal
	disposition := strings.ToLower(strings.TrimSpace(proposal.Disposition))
	switch disposition {
	case "promote":
		if !prepared.targetExists {
			content := docsAdoptPromotedDoc(proposal, prepared.source, prepared.migKind, prepared.migLifecycle, prepared.migConformance, moves)
			if err := docsAdoptWriteText(repoRoot, proposal.Target, content); err != nil {
				return err
			}
			return docsAdoptMaybeStubLegacySource(repoRoot, proposal, prepared)
		}
		if err := mergeDocsAdoptSource(repoRoot, proposal.Target, proposal.Path, prepared.source); err != nil {
			return err
		}
		return docsAdoptMaybeStubLegacySource(repoRoot, proposal, prepared)
	case "merge":
		if err := mergeDocsAdoptSource(repoRoot, proposal.Target, proposal.Path, prepared.source); err != nil {
			return err
		}
		return docsAdoptMaybeStubLegacySource(repoRoot, proposal, prepared)
	case "tombstone":
		content := docsAdoptTombstone(prepared.source, proposal, prepared.successorSubject)
		return docsAdoptWriteText(repoRoot, proposal.Path, content)
	default:
		return fmt.Errorf("unknown documentation adoption disposition %q", proposal.Disposition)
	}
}

func docsAdoptTombstone(source []byte, proposal docsAdoptProposal, successorSubject string) string {
	return docsAdoptForwardingStub(source, proposal.Path, proposal.Target, successorSubject)
}

// docsAdoptForwardingStub renders the model-conformant legacy placeholder:
// a subject-less superseded stub that owns no subject (so it cannot create
// duplicate-subject ownership), carries only its successor link and minimum
// identity metadata, and forwards through one standard relative Markdown
// link. The output is deterministic so rewriting an identical stub is a
// no-op and a repeated apply stays idempotent.
func docsAdoptForwardingStub(source []byte, sourceRel, targetRel, successorSubject string) string {
	decidesFor := ""
	if data, _, err := parseFrontmatter(string(source)); err == nil && data != nil {
		decidesFor = strings.TrimSpace(fmt.Sprint(data["decides_for"]))
		if decidesFor == "<nil>" {
			decidesFor = ""
		}
	}
	var b strings.Builder
	b.WriteString("---\nstatus: superseded\nsuperseded_by: ")
	b.WriteString(strconv.Quote(successorSubject))
	if decidesFor != "" {
		b.WriteString("\ndecides_for: ")
		b.WriteString(strconv.Quote(decidesFor))
	}
	b.WriteString("\n---\n\nThis document has moved to [")
	b.WriteString(successorSubject)
	b.WriteString("](")
	b.WriteString(docsAdoptRelativeLink(sourceRel, targetRel))
	b.WriteString(").\n")
	return b.String()
}

// docsAdoptRelativeLink expresses targetRel as a relative Markdown link from
// the directory containing sourceRel. Both are repo-relative slash paths.
func docsAdoptRelativeLink(sourceRel, targetRel string) string {
	sourceDir := filepath.FromSlash(filepath.ToSlash(filepath.Dir(filepath.FromSlash(sourceRel))))
	target := filepath.FromSlash(filepath.ToSlash(filepath.Clean(filepath.FromSlash(targetRel))))
	if rel, err := filepath.Rel(sourceDir, target); err == nil {
		return filepath.ToSlash(rel)
	}
	return filepath.ToSlash(target)
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
		var proposals []docsAdoptProposal
		var inventoryErr error
		// --migration extends the reviewed table beyond stray Markdown to
		// the legacy vault roots (specs, decisions, knowledge domains) with
		// explicit destinations, kinds and lifecycle dispositions, plus the
		// relative links/assets and active task references each move needs
		// reviewed. The default inventory is unchanged.
		if args.Bool("migration") {
			proposals, inventoryErr = inventoryDocsMigration(repoRoot, corpus)
		} else {
			proposals, inventoryErr = inventoryDocsAdopt(repoRoot, corpus)
		}
		if inventoryErr != nil {
			return inventoryErr
		}
		table = docsAdoptTable{Schema: docsAdoptTableSchema, Proposals: proposals}
		table.Fingerprint = docsAdoptTableFingerprint(table.Proposals)
	}
	migrationMode := args.Bool("migration")
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
		// Dirty owned inputs and overlapping active work are refused before
		// the approval audit and before any damaging write, with the exact
		// conflicting paths in the error.
		if err := preflightDocsAdoptGuards(repoRoot, proposals); err != nil {
			return err
		}
		prepared, preflightErr := preflightDocsAdoptTable(repoRoot, proposals)
		if preflightErr != nil {
			return preflightErr
		}
		if err := emitDocsAdoptAudit(vaultPath, "docs_adopt_approved", actor, approvalMethod, table.Fingerprint, tablePath, proposals, args.String("approval-token"), false, "reviewed table passed preflight"); err != nil {
			return fmt.Errorf("documentation adoption approval audit failed: %w", err)
		}
		// The recovery journal preserves every mutated path's original bytes
		// through completion: an interrupted apply resumes safely and a
		// repeated identical apply is idempotent.
		if err := applyPreparedDocsAdoptTableJournaled(repoRoot, table.Fingerprint, prepared); err != nil {
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
		scope := "markdown outside docs/system and .tusker/specs; generated/runtime trees omitted"
		if migrationMode || tablePath != "" && docsAdoptTableIsMigration(table) {
			scope = "migration: legacy .tusker/specs, decisions and knowledge domains plus markdown outside docs/system; generated/runtime trees omitted"
		}
		emitJSON(map[string]any{
			"schema":      table.Schema,
			"fingerprint": table.Fingerprint,
			"approved_by": table.ApprovedBy,
			"ok":          true,
			"approved":    approved,
			"applied":     applied,
			"migration":   migrationMode,
			"map":         "not regenerated; run `tusker docs map` after review",
			"proposals":   proposals,
			"scope":       scope,
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

// docsAdoptNormalizeKind maps a reviewed kind spelling to its portable kind.
// An empty value takes the caller default (doc); spec stays accepted as the
// compatibility spelling for proposal. Canonical is a lifecycle spelling,
// not a kind, and is rejected with a repair hint.
func docsAdoptNormalizeKind(raw string) (docgraph.Kind, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "":
		return "", true
	case "doc":
		return docgraph.KindDoc, true
	case "proposal", "spec":
		return docgraph.KindProposal, true
	case "decision":
		return docgraph.KindDecision, true
	default:
		return "", false
	}
}

// docsAdoptRowIdentity resolves the explicit portable identity of a reviewed
// migration row. Empty kind/lifecycle/conformance take the S46 defaults;
// lifecycle and conformance values are validated against the kind, and any
// mapping that claims implementation proof or code conformance requires
// recorded evidence. A legacy canonical source never translates into
// implemented/matches on its own: those need an explicit reviewed row with
// evidence, and matches additionally needs a post-migration docs verify
// stamp that migration cannot mint.
func docsAdoptRowIdentity(proposal docsAdoptProposal) (docgraph.Kind, string, string, error) {
	kind, ok := docsAdoptNormalizeKind(proposal.Kind)
	if !ok {
		return "", "", "", fmt.Errorf("documentation adoption row %s declares unknown kind %q: declare doc, proposal, or decision", proposal.Path, proposal.Kind)
	}
	if kind == "" {
		kind = docgraph.KindDoc
	}
	lifecycle := strings.ToLower(strings.TrimSpace(proposal.Lifecycle))
	if lifecycle == "" {
		switch kind {
		case docgraph.KindProposal, docgraph.KindDecision:
			lifecycle = "proposed"
		default:
			lifecycle = "current"
		}
	}
	if !docgraph.ValidLifecycle(kind, docgraph.KindSourceExplicit, lifecycle) {
		return "", "", "", fmt.Errorf("documentation adoption row %s declares lifecycle %q, which is not valid for %s documents: use %s", proposal.Path, proposal.Lifecycle, kind, strings.Join(docgraph.LifecycleValues(kind), "|"))
	}
	conformance := strings.ToLower(strings.TrimSpace(proposal.Conformance))
	if conformance == "" {
		conformance = "unverified"
	}
	if !docgraph.ValidCodeConformance(conformance) {
		return "", "", "", fmt.Errorf("documentation adoption row %s declares unknown code_conformance %q: declare unverified, matches, drift, or not_applicable", proposal.Path, proposal.Conformance)
	}
	evidence := strings.TrimSpace(proposal.Evidence)
	switch {
	case lifecycle == "implemented" && evidence == "":
		return "", "", "", fmt.Errorf("documentation adoption row %s claims implemented without recorded completion evidence: record evidence or select proposed|accepted", proposal.Path)
	case conformance == "matches":
		return "", "", "", fmt.Errorf("documentation adoption row %s claims code_conformance matches during migration: migrate as unverified, then verify with docs verify to mint the checked stamp", proposal.Path)
	case conformance == "drift" && evidence == "":
		return "", "", "", fmt.Errorf("documentation adoption row %s claims drift without naming the unmatched scope as evidence", proposal.Path)
	}
	return kind, lifecycle, conformance, nil
}

// docsAdoptIsLegacyVaultSource reports whether a source lives under a legacy
// vault root whose content moves to docs/system. Only these sources become
// forwarding stubs after apply; outside-tree sources always keep originals.
func docsAdoptIsLegacyVaultSource(relative string) bool {
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(relative)))
	for _, root := range []string{".tusker/specs/", ".tusker/decisions/", ".tusker/knowledge/"} {
		if strings.HasPrefix(clean, root) {
			return true
		}
	}
	return false
}

// docsAdoptSuccessorSubject returns the canonical subject a row forwards to:
// the reviewed subject for promote/merge (both require target subject
// agreement) and the existing target subject for tombstone.
func docsAdoptSuccessorSubject(repoRoot string, proposal docsAdoptProposal) (string, error) {
	if strings.ToLower(strings.TrimSpace(proposal.Disposition)) != "tombstone" {
		return strings.TrimSpace(proposal.Subject), nil
	}
	target, err := docgraph.ReadDocumentFile(repoRoot, proposal.Target)
	if err != nil {
		return "", err
	}
	doc, err := docgraph.ParseDocHeaders(proposal.Target, target)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(doc.Subject), nil
}

// docsAdoptSourceIsExpectedStub recognizes the post-apply state of a
// tombstone row or a stub-source promote/merge row: the source already holds
// the exact deterministic forwarding stub for this row's successor. Returns
// false (and no error) when the row still needs work or when the successor
// cannot be determined yet, so the normal reviewed checks run instead.
func docsAdoptSourceIsExpectedStub(repoRoot string, proposal docsAdoptProposal, source []byte) (bool, error) {
	disposition := strings.ToLower(strings.TrimSpace(proposal.Disposition))
	if disposition != "tombstone" && !proposal.StubSource {
		return false, nil
	}
	successor, err := docsAdoptSuccessorSubject(repoRoot, proposal)
	if err != nil || successor == "" {
		return false, nil
	}
	expected := docsAdoptForwardingStub(source, proposal.Path, proposal.Target, successor)
	return string(source) == expected, nil
}

// docsAdoptMaybeStubLegacySource replaces a migrated legacy vault source with
// its forwarding stub once the canonical target owns the content, so the old
// path forwards without duplicating the current subject. The stub is
// deterministic; when it is already in place this is a no-op. Outside-tree
// sources and rows without stub_source keep their originals untouched.
func docsAdoptMaybeStubLegacySource(repoRoot string, proposal docsAdoptProposal, prepared docsAdoptPrepared) error {
	if !proposal.StubSource || !docsAdoptIsLegacyVaultSource(proposal.Path) {
		return nil
	}
	successor, err := docsAdoptSuccessorSubject(repoRoot, proposal)
	if err != nil {
		return err
	}
	expected := docsAdoptForwardingStub(prepared.source, proposal.Path, proposal.Target, successor)
	current, err := docgraph.ReadDocumentFile(repoRoot, proposal.Path)
	if err != nil {
		return err
	}
	if string(current) == expected {
		return nil
	}
	return docsAdoptWriteText(repoRoot, proposal.Path, expected)
}

// docsAdoptPromotedDoc renders a created portable document with its explicit
// kind, kind-specific lifecycle and independent conformance. Migrated legacy
// material starts unverified and unapproved: proposals start proposed and
// docs start current regardless of the legacy status spelling. Creation
// preserves the source body (with co-migrated links repaired), parent,
// decision target and keywords, and records the legacy path in sources.
func docsAdoptPromotedDoc(proposal docsAdoptProposal, source []byte, kind docgraph.Kind, lifecycle, conformance string, moves map[string]string) string {
	partOf, decidesFor, keywords := "", "", []string{}
	if data, _, err := parseFrontmatter(string(source)); err == nil && data != nil {
		partOf = strings.TrimSpace(fmt.Sprint(data["part_of"]))
		decidesFor = strings.TrimSpace(fmt.Sprint(data["decides_for"]))
		if partOf == "<nil>" {
			partOf = ""
		}
		if decidesFor == "<nil>" {
			decidesFor = ""
		}
		if list, ok := data["keywords"]; ok && list != nil {
			for _, item := range docsAdoptFrontmatterList(list) {
				if item != "" {
					keywords = append(keywords, item)
				}
			}
		}
	}
	if partOf == "" {
		partOf = "overview"
	}
	body := docsAdoptBody(source)
	if len(moves) > 0 {
		body = docsAdoptRepairBodyLinks(body, proposal.Path, proposal.Target, moves)
	}
	created := time.Now().Local().Format("2006-01-02")
	var b strings.Builder
	b.WriteString("---\nkind: ")
	b.WriteString(string(kind))
	b.WriteString("\nsubject: ")
	b.WriteString(strconv.Quote(strings.TrimSpace(proposal.Subject)))
	b.WriteString("\nkeywords: [")
	for i, keyword := range keywords {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(strconv.Quote(keyword))
	}
	b.WriteString("]\npart_of: ")
	b.WriteString(strconv.Quote(partOf))
	b.WriteString("\ndescribes: []\nstatus: ")
	b.WriteString(lifecycle)
	if kind == docgraph.KindDecision {
		b.WriteString("\ndecides_for: ")
		b.WriteString(strconv.Quote(decidesFor))
	}
	b.WriteString("\ncode_conformance: ")
	b.WriteString(conformance)
	b.WriteString("\ncreated: ")
	b.WriteString(created)
	b.WriteString("\nlast_verified:\nread_when: \"\"\nskip_when: \"\"\n")
	if kind == docgraph.KindProposal {
		b.WriteString("updates: []\ndecisions_locked: false\n")
	}
	b.WriteString("sources: [")
	b.WriteString(strconv.Quote(filepath.ToSlash(proposal.Path)))
	b.WriteString("]\n---\n\n")
	b.WriteString(body)
	b.WriteString("\n")
	return b.String()
}

func docsAdoptFrontmatterList(value any) []string {
	switch list := value.(type) {
	case []any:
		var out []string
		for _, item := range list {
			if text := strings.TrimSpace(fmt.Sprint(item)); text != "" && text != "<nil>" {
				out = append(out, text)
			}
		}
		return out
	case []string:
		return list
	case string:
		if strings.TrimSpace(list) == "" {
			return nil
		}
		return []string{strings.TrimSpace(list)}
	default:
		return nil
	}
}

func docsAdoptDecidesFor(source []byte) string {
	if data, _, err := parseFrontmatter(string(source)); err == nil && data != nil {
		if decidesFor := strings.TrimSpace(fmt.Sprint(data["decides_for"])); decidesFor != "" && decidesFor != "<nil>" {
			return decidesFor
		}
	}
	return ""
}

var docsAdoptInlineLinkPattern = regexp.MustCompile(`!?\[[^\]]*\]\(([^)\s]+)[^)]*\)`)

// docsAdoptRelativeRefs inventories the relative Markdown links and assets in
// a source body: inline link/image destinations without a scheme or anchor.
// Absolute URLs, mailto links and pure anchors are not portable-tree moves
// and are excluded; everything reported stays byte-identical unless its
// target is itself migrated by the same reviewed table.
func docsAdoptRelativeRefs(body string) []string {
	seen := map[string]bool{}
	var refs []string
	for _, match := range docsAdoptInlineLinkPattern.FindAllStringSubmatch(body, -1) {
		if len(match) < 2 {
			continue
		}
		dest := strings.TrimSpace(match[1])
		if dest == "" || strings.HasPrefix(dest, "#") || strings.Contains(dest, "://") || strings.HasPrefix(dest, "mailto:") {
			continue
		}
		if anchor := strings.IndexByte(dest, '#'); anchor >= 0 {
			dest = strings.TrimSpace(dest[:anchor])
		}
		if dest == "" || seen[dest] {
			continue
		}
		seen[dest] = true
		refs = append(refs, dest)
	}
	sort.Strings(refs)
	return refs
}

// docsAdoptRepairBodyLinks rewrites relative links in a promoted body whose
// targets move under the same reviewed table, so standard Markdown links
// stay valid from the document's new location. Links to unmigrated paths are
// preserved byte-identical for the reviewer.
func docsAdoptRepairBodyLinks(body, sourceRel, targetRel string, moves map[string]string) string {
	if len(moves) == 0 {
		return body
	}
	cleanMoves := map[string]string{}
	for old, successor := range moves {
		oldClean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(old)))
		newClean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(successor)))
		if oldClean != "" && newClean != "" {
			cleanMoves[oldClean] = newClean
		}
	}
	sourceDir := filepath.ToSlash(filepath.Dir(filepath.FromSlash(sourceRel)))
	targetDir := filepath.ToSlash(filepath.Dir(filepath.FromSlash(targetRel)))
	resolve := func(base, dest string) string {
		if strings.HasPrefix(dest, "/") {
			return filepath.ToSlash(filepath.Clean(filepath.FromSlash(strings.TrimPrefix(dest, "/"))))
		}
		return filepath.ToSlash(filepath.Clean(filepath.FromSlash(base) + "/" + dest))
	}
	return docsAdoptInlineLinkPattern.ReplaceAllStringFunc(body, func(link string) string {
		open := strings.IndexByte(link, '(')
		close := strings.LastIndexByte(link, ')')
		if open < 0 || close < 0 || close <= open+1 {
			return link
		}
		inner := strings.TrimSpace(link[open+1 : close])
		dest := inner
		suffix := ""
		if fields := strings.Fields(inner); len(fields) > 1 {
			dest = fields[0]
			suffix = strings.TrimPrefix(inner, dest)
		}
		if dest == "" || strings.HasPrefix(dest, "#") || strings.Contains(dest, "://") || strings.HasPrefix(dest, "mailto:") {
			return link
		}
		anchor := ""
		if hash := strings.IndexByte(dest, '#'); hash >= 0 {
			anchor, dest = dest[hash:], strings.TrimSpace(dest[:hash])
		}
		if dest == "" {
			return link
		}
		successor, moved := cleanMoves[resolve(sourceDir, dest)]
		if !moved {
			return link
		}
		rel, err := filepath.Rel(filepath.FromSlash(targetDir), filepath.FromSlash(successor))
		if err != nil {
			return link
		}
		return link[:open+1] + filepath.ToSlash(rel) + anchor + suffix + ")"
	})
}

// docsAdoptTableIsMigration reports whether a reviewed table carries explicit
// migration selections (kinds, lifecycles, stubbed legacy sources).
func docsAdoptTableIsMigration(table docsAdoptTable) bool {
	for _, proposal := range table.Proposals {
		if strings.EqualFold(strings.TrimSpace(proposal.Disposition), "leave") {
			continue
		}
		if strings.TrimSpace(proposal.Kind) != "" || strings.TrimSpace(proposal.Lifecycle) != "" ||
			strings.TrimSpace(proposal.Conformance) != "" || proposal.StubSource ||
			docsAdoptIsLegacyVaultSource(proposal.Path) {
			return true
		}
	}
	return false
}

// inventoryDocsMigration extends the reviewed adoption table to the S46
// migration: legacy vault roots (specs, decisions, knowledge domains) are
// inventoried with explicit destinations, kinds and lifecycle dispositions,
// and every row carries the relative links/assets found in its source plus
// the active task references that still name the old path. Rows that need a
// human decision (ambiguous lifecycle, duplicate subjects, target
// collisions, missing decision targets, overlapping active work surfaced
// here for review) stay leave with a reason. A preview never writes.
func inventoryDocsMigration(repoRoot string, corpus docgraph.Corpus) ([]docsAdoptProposal, error) {
	generic, err := inventoryDocsAdopt(repoRoot, corpus)
	if err != nil {
		return nil, err
	}
	for i := range generic {
		if strings.EqualFold(strings.TrimSpace(generic[i].Disposition), "leave") {
			continue
		}
		if raw, readErr := docgraph.ReadDocumentFile(repoRoot, generic[i].Path); readErr == nil {
			generic[i].Links = docsAdoptRelativeRefs(docsAdoptFrontmatterBody(string(raw)))
		}
	}
	known := map[string][]docgraph.Document{}
	for _, doc := range corpus.Documents {
		if subject := strings.ToLower(strings.TrimSpace(doc.Subject)); subject != "" {
			known[subject] = append(known[subject], doc)
		}
	}
	occupiedSubjects := map[string]string{}
	occupiedTargets := map[string]string{}
	for subject, docs := range known {
		// Legacy vault sources are claimed by this inventory run, not by
		// the corpus scan that still sees them at their old paths.
		for _, doc := range docs {
			if !docsAdoptIsLegacyVaultSource(doc.Path) {
				occupiedSubjects[subject] = doc.Path
				break
			}
		}
	}
	for _, proposal := range generic {
		if strings.EqualFold(strings.TrimSpace(proposal.Disposition), "leave") {
			continue
		}
		if subject := strings.ToLower(strings.TrimSpace(proposal.Subject)); subject != "" {
			occupiedSubjects[subject] = proposal.Path
		}
		if target := filepath.ToSlash(filepath.Clean(filepath.FromSlash(proposal.Target))); proposal.Target != "" {
			occupiedTargets[target] = proposal.Path
		}
	}
	legacy, err := inventoryDocsMigrationLegacy(repoRoot, known, occupiedSubjects, occupiedTargets)
	if err != nil {
		return nil, err
	}
	return append(generic, legacy...), nil
}

func docsAdoptFrontmatterBody(content string) string {
	if _, body, err := parseFrontmatter(content); err == nil {
		return body
	}
	return content
}

type docsMigrationLegacyRoot struct {
	dir     string
	kind    docgraph.Kind
	destDir string
	topOnly bool
}

func inventoryDocsMigrationLegacy(repoRoot string, known map[string][]docgraph.Document, occupiedSubjects, occupiedTargets map[string]string) ([]docsAdoptProposal, error) {
	roots := []docsMigrationLegacyRoot{
		{dir: ".tusker/specs/decisions", kind: docgraph.KindDecision, destDir: "docs/system/decisions", topOnly: true},
		{dir: ".tusker/specs", kind: docgraph.KindProposal, destDir: "docs/system/proposals", topOnly: true},
		{dir: ".tusker/knowledge/domains", kind: docgraph.KindDoc, destDir: "docs/system/domains", topOnly: false},
	}
	var candidates []string
	for _, root := range roots {
		abs := filepath.Join(repoRoot, filepath.FromSlash(root.dir))
		info, err := os.Lstat(abs)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			continue
		}
		walkErr := filepath.WalkDir(abs, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			rel, err := filepath.Rel(repoRoot, path)
			if err != nil {
				return err
			}
			rel = filepath.ToSlash(rel)
			if entry.IsDir() {
				if rel != filepath.ToSlash(root.dir) && root.topOnly {
					return fs.SkipDir
				}
				// The decisions subtree is inventoried by its own root.
				if root.dir == ".tusker/specs" && rel == ".tusker/specs/decisions" {
					return fs.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(strings.ToLower(entry.Name()), ".md") {
				return nil
			}
			candidates = append(candidates, rel)
			return nil
		})
		if walkErr != nil {
			return nil, walkErr
		}
	}
	sort.Strings(candidates)
	proposals := make([]docsAdoptProposal, 0, len(candidates))
	subjectPaths := map[string][]string{}
	for _, rel := range candidates {
		subject := migrationLegacySubject(repoRoot, rel)
		if normalized := strings.ToLower(strings.TrimSpace(subject)); normalized != "" {
			subjectPaths[normalized] = append(subjectPaths[normalized], rel)
		}
	}
	for _, rel := range candidates {
		proposals = append(proposals, inventoryDocsMigrationRow(repoRoot, known, occupiedSubjects, occupiedTargets, subjectPaths, rel))
		proposal := &proposals[len(proposals)-1]
		if !strings.EqualFold(strings.TrimSpace(proposal.Disposition), "leave") {
			if subject := strings.ToLower(strings.TrimSpace(proposal.Subject)); subject != "" {
				occupiedSubjects[subject] = proposal.Path
			}
			if proposal.Target != "" {
				occupiedTargets[filepath.ToSlash(filepath.Clean(filepath.FromSlash(proposal.Target)))] = proposal.Path
			}
		}
	}
	return proposals, nil
}

func migrationLegacySubject(repoRoot, rel string) string {
	info, err := os.Lstat(filepath.Join(repoRoot, filepath.FromSlash(rel)))
	if err != nil || info.Mode()&os.ModeSymlink != 0 {
		return ""
	}
	raw, err := docgraph.ReadDocumentFile(repoRoot, rel)
	if err != nil {
		return ""
	}
	return docsAdoptSubject(rel, raw)
}

func inventoryDocsMigrationRow(repoRoot string, known map[string][]docgraph.Document, occupiedSubjects, occupiedTargets map[string]string, subjectPaths map[string][]string, rel string) docsAdoptProposal {
	proposal := docsAdoptProposal{Path: rel, StubSource: true}
	info, err := os.Lstat(filepath.Join(repoRoot, filepath.FromSlash(rel)))
	if err != nil || info.Mode()&os.ModeSymlink != 0 {
		proposal.Disposition, proposal.Reason = "leave", "legacy source is a symlink or unreadable; manual review required"
		return proposal
	}
	raw, err := docgraph.ReadDocumentFile(repoRoot, rel)
	if err != nil {
		proposal.Disposition, proposal.Reason = "leave", "legacy source cannot be read; manual review required"
		return proposal
	}
	kind, destDir := migrationLegacyKindTarget(rel)
	subject := docsAdoptSubject(rel, raw)
	proposal.Subject = subject
	proposal.SourceFingerprint = docsAdoptBytesFingerprint(raw)
	body := docsAdoptFrontmatterBody(string(raw))
	proposal.Links = docsAdoptRelativeRefs(body)
	proposal.ActiveRefs = docsAdoptActiveRefsForSource(repoRoot, rel)
	switch kind {
	case docgraph.KindProposal, docgraph.KindDecision:
		proposal.Kind = string(kind)
		proposal.Lifecycle = "proposed"
		proposal.Conformance = "unverified"
	default:
		proposal.Kind = string(docgraph.KindDoc)
		proposal.Lifecycle = "current"
		proposal.Conformance = "unverified"
	}
	if docsAdoptUntitledSubject(subject) {
		proposal.Disposition, proposal.Reason = "leave", "untitled or missing subject; manual review required"
		return proposal
	}
	if kind == docgraph.KindDecision && docsAdoptDecidesFor(raw) == "" {
		proposal.Disposition, proposal.Reason = "leave", "decision migration requires decides_for; manual review required"
		return proposal
	}
	normalizedSubject := strings.ToLower(strings.TrimSpace(subject))
	if paths := subjectPaths[normalizedSubject]; len(paths) > 1 {
		proposal.Disposition, proposal.Reason = "leave", "multiple legacy files share this subject; manual merge required"
		return proposal
	}
	if owner, taken := occupiedSubjects[normalizedSubject]; taken && owner != rel {
		others := make([]docgraph.Document, 0, len(known[normalizedSubject]))
		for _, doc := range known[normalizedSubject] {
			if filepath.ToSlash(doc.Path) != rel {
				others = append(others, doc)
			}
		}
		if len(others) == 1 && filepath.ToSlash(others[0].Path) == owner {
			if symlinkPath, symlinkErr := docsAdoptSymlinkPath(repoRoot, owner); symlinkErr == nil && symlinkPath == "" {
				proposal.Target = owner
				proposal.Disposition, proposal.Reason = "merge", "subject already has a canonical owner"
				return proposal
			}
		}
		proposal.Disposition, proposal.Reason = "leave", "canonical subject is ambiguous or already selected by "+owner+"; manual merge required"
		return proposal
	}
	proposal.Target = migrationLegacyTarget(rel, kind, destDir, subject)
	if previous, taken := occupiedTargets[filepath.ToSlash(filepath.Clean(filepath.FromSlash(proposal.Target)))]; taken {
		proposal.Disposition, proposal.Reason = "leave", "multiple legacy files map to the same canonical target as "+previous+"; manual merge required"
		return proposal
	}
	if symlinkPath, symlinkErr := docsAdoptSymlinkPath(repoRoot, proposal.Target); symlinkErr != nil {
		proposal.Disposition, proposal.Reason = "leave", "canonical target path crosses a symlink; manual review required"
		return proposal
	} else if symlinkPath != "" {
		proposal.Disposition, proposal.Reason = "leave", "canonical target path crosses a symlink; manual review required"
		return proposal
	}
	targetPath := filepath.Join(repoRoot, filepath.FromSlash(proposal.Target))
	if targetInfo, statErr := os.Lstat(targetPath); statErr == nil {
		if targetInfo.Mode()&os.ModeSymlink != 0 {
			proposal.Disposition, proposal.Reason = "leave", "canonical target is a symlink; manual review required"
			return proposal
		}
		targetRaw, readErr := docgraph.ReadDocumentFile(repoRoot, proposal.Target)
		if readErr != nil {
			proposal.Disposition, proposal.Reason = "leave", "canonical target cannot be read; manual review required"
			return proposal
		}
		targetDoc, parseErr := docgraph.ParseDocHeaders(proposal.Target, targetRaw)
		if parseErr != nil || !strings.EqualFold(strings.TrimSpace(targetDoc.Subject), strings.TrimSpace(subject)) {
			proposal.Disposition, proposal.Reason = "leave", "canonical target path collision; manual review required"
			return proposal
		}
		proposal.Disposition, proposal.Reason = "merge", "canonical target path already exists"
		return proposal
	} else if !os.IsNotExist(statErr) {
		proposal.Disposition, proposal.Reason = "leave", "canonical target cannot be inspected; manual review required"
		return proposal
	}
	legacyStatus := migrationLegacyStatus(raw)
	if legacyStatus != "" {
		proposal.Reason = "legacy " + legacyStatus + " migration carries no prior approval; selected " + proposal.Lifecycle
	} else {
		proposal.Reason = "legacy migration; selected " + proposal.Lifecycle
	}
	proposal.Disposition = "promote"
	return proposal
}

func migrationLegacyKindTarget(rel string) (docgraph.Kind, string) {
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(rel)))
	switch {
	case clean == ".tusker/specs/decisions" || strings.HasPrefix(clean, ".tusker/specs/decisions/"):
		return docgraph.KindDecision, "docs/system/decisions"
	case clean == ".tusker/specs" || strings.HasPrefix(clean, ".tusker/specs/"):
		return docgraph.KindProposal, "docs/system/proposals"
	default:
		return docgraph.KindDoc, "docs/system/domains"
	}
}

func migrationLegacyTarget(rel string, kind docgraph.Kind, destDir, subject string) string {
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(rel)))
	if kind == docgraph.KindDoc && strings.HasPrefix(clean, ".tusker/knowledge/domains/") {
		sub := strings.TrimPrefix(clean, ".tusker/knowledge/domains/")
		if strings.EqualFold(filepath.Base(sub), "INDEX.md") {
			sub = filepath.ToSlash(filepath.Join(filepath.Dir(sub), "00-index.md"))
		}
		return filepath.ToSlash(filepath.Join("docs/system/domains", sub))
	}
	return filepath.ToSlash(filepath.Join(destDir, docsSubjectSlug(subject)+".md"))
}

func migrationLegacyStatus(raw []byte) string {
	if data, _, err := parseFrontmatter(string(raw)); err == nil && data != nil {
		if status := strings.TrimSpace(fmt.Sprint(data["status"])); status != "" && status != "<nil>" {
			return status
		}
	}
	return ""
}

// preflightDocsAdoptGuards refuses the reviewed table before the approval
// audit and before any damaging write when an owned input is dirty in git or
// when an active task still binds the old path through spec_refs. Every
// refusal names the exact conflicting paths. Task reference changes keep
// their proof authority: migration never rewrites task files, so overlapping
// active work must rebind through the normal revision-aware task commands
// first; historical references keep resolving through forwarding stubs.
func preflightDocsAdoptGuards(repoRoot string, proposals []docsAdoptProposal) error {
	seen := map[string]bool{}
	var relatives []string
	for _, proposal := range proposals {
		if strings.EqualFold(strings.TrimSpace(proposal.Disposition), "leave") {
			continue
		}
		for _, relative := range []string{proposal.Path, proposal.Target} {
			clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(relative)))
			if clean == "" || seen[clean] {
				continue
			}
			seen[clean] = true
			relatives = append(relatives, clean)
		}
	}
	if err := docsAdoptRefuseDirtyInputs(repoRoot, relatives); err != nil {
		return err
	}
	if err := docsAdoptRefuseDuplicateSubjects(repoRoot, proposals); err != nil {
		return err
	}
	for _, proposal := range proposals {
		if strings.EqualFold(strings.TrimSpace(proposal.Disposition), "leave") {
			continue
		}
		clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(proposal.Path)))
		if refs := docsAdoptActiveRefsForSource(repoRoot, clean); len(refs) > 0 {
			return fmt.Errorf("documentation adoption refuses overlapping active work: %s is referenced by active tasks %s; rebind those tasks through the normal revision-aware task commands first", proposal.Path, strings.Join(refs, ", "))
		}
	}
	return nil
}

// docsAdoptRefuseDirtyInputs refuses owned migration paths with uncommitted
// tracked changes. Untracked files are not dirty; a missing git binary, a
// non-git directory, or any git failure skips the check (best effort) so
// disposable fixtures and fresh projects still migrate.
func docsAdoptRefuseDirtyInputs(repoRoot string, relatives []string) error {
	if len(relatives) == 0 {
		return nil
	}
	if info, err := os.Lstat(filepath.Join(repoRoot, ".git")); err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return nil
	}
	// A deleted tracked file is still a valid pathspec (it is in the
	// index); a never-tracked missing path (a promote target that does not
	// exist yet) can make git report a pathspec error, so fall back to the
	// existing paths instead of skipping the check.
	args := append([]string{"-C", repoRoot, "status", "--porcelain=v1", "--"}, relatives...)
	out, err := exec.Command("git", args...).Output()
	if err != nil {
		var existing []string
		for _, relative := range relatives {
			if _, statErr := os.Lstat(filepath.Join(repoRoot, filepath.FromSlash(relative))); statErr == nil {
				existing = append(existing, relative)
			}
		}
		if len(existing) == 0 {
			return nil
		}
		retry := append([]string{"-C", repoRoot, "status", "--porcelain=v1", "--"}, existing...)
		if out, err = exec.Command("git", retry...).Output(); err != nil {
			return nil
		}
	}
	owned := map[string]bool{}
	for _, relative := range relatives {
		owned[filepath.ToSlash(filepath.Clean(filepath.FromSlash(relative)))] = true
	}
	dirty := map[string]bool{}
	for _, line := range strings.Split(string(out), "\n") {
		if len(line) < 4 {
			continue
		}
		status, rest := strings.TrimSpace(line[:2]), strings.TrimSpace(line[3:])
		if status == "" || status == "??" {
			continue
		}
		for _, path := range strings.Split(rest, " -> ") {
			path = strings.Trim(strings.TrimSpace(path), `"`)
			if owned[filepath.ToSlash(filepath.Clean(path))] {
				dirty[filepath.ToSlash(filepath.Clean(path))] = true
			}
		}
	}
	if len(dirty) == 0 {
		return nil
	}
	var paths []string
	for path := range dirty {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return fmt.Errorf("documentation adoption refuses dirty owned inputs with uncommitted changes: %s; commit, stash, or restore them first", strings.Join(paths, ", "))
}

// docsAdoptActiveRefsForSource inventories the active task files whose
// spec_refs name a legacy source path. Only path references overlap with a
// move: subject references keep resolving to the same subject at its new
// home. Inactive (done/closed/superseded/cancelled) tasks keep their
// recorded bytes untouched and resolve through forwarding stubs.
func docsAdoptActiveRefsForSource(repoRoot, sourceRel string) []string {
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(sourceRel)))
	tasksDir := filepath.Join(repoRoot, ".tusker", "work", "tasks")
	var refs []string
	_ = filepath.WalkDir(tasksDir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".md") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		data, _, err := parseFrontmatter(string(raw))
		if err != nil || data == nil {
			return nil
		}
		matched := false
		for _, ref := range docsAdoptFrontmatterList(data["spec_refs"]) {
			target := ref
			if anchor := strings.IndexByte(target, '#'); anchor >= 0 {
				target = target[:anchor]
			}
			target = strings.TrimSpace(target)
			target = strings.TrimPrefix(target, "./")
			if filepath.ToSlash(filepath.Clean(filepath.FromSlash(target))) == clean {
				matched = true
				break
			}
		}
		if !matched || !docsAdoptTaskIsActive(data) {
			return nil
		}
		rel, err := filepath.Rel(repoRoot, path)
		if err != nil {
			return nil
		}
		refs = append(refs, filepath.ToSlash(rel))
		return nil
	})
	sort.Strings(refs)
	return refs
}

// docsAdoptRefuseDuplicateSubjects refuses reviewed rows that would leave
// two current documents owning one subject. Tombstone rows always end
// subject-less, merge rows join their existing owner, and a row never
// conflicts with its own legacy source still visible at the old path; every
// other clash names the exact subject and paths before any damaging write.
func docsAdoptRefuseDuplicateSubjects(repoRoot string, proposals []docsAdoptProposal) error {
	var actionable []docsAdoptProposal
	for _, proposal := range proposals {
		disposition := strings.ToLower(strings.TrimSpace(proposal.Disposition))
		if disposition == "leave" || disposition == "tombstone" {
			continue
		}
		actionable = append(actionable, proposal)
	}
	if len(actionable) == 0 {
		return nil
	}
	type subjectClaim struct{ path, target string }
	seen := map[string]subjectClaim{}
	for _, proposal := range actionable {
		subject := strings.ToLower(strings.TrimSpace(proposal.Subject))
		if subject == "" {
			continue
		}
		target := filepath.ToSlash(filepath.Clean(filepath.FromSlash(proposal.Target)))
		if previous, ok := seen[subject]; ok && previous.target != target {
			return fmt.Errorf("documentation adoption refuses duplicate subject %q claimed by %s and %s; merge the rows or leave one for manual review", proposal.Subject, previous.path, proposal.Path)
		}
		seen[subject] = subjectClaim{path: proposal.Path, target: target}
	}
	corpus, _, err := docgraph.LoadRepository(repoRoot)
	if err != nil {
		return err
	}
	for _, proposal := range actionable {
		subject := strings.TrimSpace(proposal.Subject)
		if subject == "" {
			continue
		}
		target := filepath.ToSlash(filepath.Clean(filepath.FromSlash(proposal.Target)))
		for _, doc := range corpus.Documents {
			if docgraph.IsForwardingStub(doc) {
				continue
			}
			if !strings.EqualFold(strings.TrimSpace(doc.Subject), subject) {
				continue
			}
			if filepath.ToSlash(doc.Path) == target || filepath.ToSlash(doc.Path) == filepath.ToSlash(filepath.Clean(filepath.FromSlash(proposal.Path))) {
				continue
			}
			return fmt.Errorf("documentation adoption refuses duplicate subject %q: %s would shadow current owner %s; merge into that target or leave the row for manual review", subject, proposal.Path, doc.Path)
		}
	}
	return nil
}

func docsAdoptTaskIsActive(data map[string]any) bool {
	status := strings.ToLower(strings.TrimSpace(fmt.Sprint(data["status"])))
	switch status {
	case "done", "completed", "complete", "closed", "superseded", "cancelled", "dropped", "archived":
		return false
	default:
		return true
	}
}

const docsAdoptJournalSchema = "tusker.docs-adopt-journal/v1"

type docsAdoptJournalRow struct {
	Path           string `json:"path"`
	Target         string `json:"target,omitempty"`
	Disposition    string `json:"disposition"`
	Done           bool   `json:"done"`
	PreSourceFP    string `json:"pre_source_fp,omitempty"`
	PreTargetFP    string `json:"pre_target_fp,omitempty"`
	PostSourceFP   string `json:"post_source_fp,omitempty"`
	PostTargetFP   string `json:"post_target_fp,omitempty"`
	TargetExisted  bool   `json:"target_existed"`
	OriginalSource []byte `json:"original_source_b64,omitempty"`
	OriginalTarget []byte `json:"original_target_b64,omitempty"`
}

type docsAdoptJournal struct {
	Schema      string                `json:"schema"`
	Fingerprint string                `json:"fingerprint"`
	Complete    bool                  `json:"complete"`
	Rows        []docsAdoptJournalRow `json:"rows"`
}

// docsAdoptJournalPath locates the recovery journal for one reviewed table.
// Generated state lives under .tusker/_generated/docs; the journal preserves
// every mutated path's original bytes through completion.
func docsAdoptJournalPath(repoRoot, fingerprint string) string {
	digest := strings.TrimPrefix(fingerprint, "sha256:")
	if len(digest) > 16 {
		digest = digest[:16]
	}
	if strings.TrimSpace(digest) == "" {
		digest = "untabled"
	}
	return filepath.Join(repoRoot, ".tusker", "_generated", "docs", "adopt-"+digest+".json")
}

func docsAdoptFileState(repoRoot, relative string) (fingerprint string, existed bool, content []byte) {
	raw, err := docgraph.ReadDocumentFile(repoRoot, relative)
	if err != nil {
		return "", false, nil
	}
	return docsAdoptBytesFingerprint(raw), true, raw
}

func loadDocsAdoptJournal(path string) (*docsAdoptJournal, error) {
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var journal docsAdoptJournal
	if err := json.Unmarshal(raw, &journal); err != nil {
		return nil, fmt.Errorf("read documentation adoption recovery journal %s: %w", path, err)
	}
	if journal.Schema != docsAdoptJournalSchema {
		return nil, fmt.Errorf("documentation adoption recovery journal schema %q is unsupported", journal.Schema)
	}
	return &journal, nil
}

func saveDocsAdoptJournal(path string, journal *docsAdoptJournal) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(journal, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(raw, '\n'), 0o644)
}

func docsAdoptFailAfter() (int, bool) {
	raw := strings.TrimSpace(os.Getenv("TUSKER_DOCS_ADOPT_FAIL_AFTER"))
	if raw == "" {
		return 0, false
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}

// applyPreparedDocsAdoptTableJournaled applies a reviewed table through the
// recovery journal. An interrupted apply resumes the pending rows; a
// repeated identical apply is a no-op. Rows already in their post-apply
// state (forwarding stub present, merge marker present) are skipped, and a
// row whose files changed outside the migration during recovery is refused
// with its exact path instead of being silently reblessed.
func applyPreparedDocsAdoptTableJournaled(repoRoot, fingerprint string, prepared []docsAdoptPrepared) error {
	docsAdoptApplyMu.Lock()
	defer docsAdoptApplyMu.Unlock()
	var actionable []docsAdoptPrepared
	for _, item := range prepared {
		if strings.EqualFold(strings.TrimSpace(item.proposal.Disposition), "leave") {
			continue
		}
		actionable = append(actionable, item)
	}
	journalPath := docsAdoptJournalPath(repoRoot, fingerprint)
	journal, err := loadDocsAdoptJournal(journalPath)
	if err != nil {
		return err
	}
	if journal != nil && journal.Fingerprint == fingerprint && journal.Complete {
		return nil
	}
	if journal == nil || journal.Fingerprint != fingerprint {
		journal = &docsAdoptJournal{Schema: docsAdoptJournalSchema, Fingerprint: fingerprint}
		for _, item := range actionable {
			row := docsAdoptJournalRow{Path: item.proposal.Path, Target: item.proposal.Target, Disposition: item.proposal.Disposition}
			preSourceFP, _, sourceRaw := docsAdoptFileState(repoRoot, item.proposal.Path)
			preTargetFP, targetExisted, targetRaw := docsAdoptFileState(repoRoot, item.proposal.Target)
			row.PreSourceFP, row.PreTargetFP = preSourceFP, preTargetFP
			row.TargetExisted = targetExisted
			if item.alreadyApplied {
				row.Done = true
				row.PostSourceFP, row.PostTargetFP = preSourceFP, preTargetFP
			} else {
				row.OriginalSource = sourceRaw
				if targetExisted {
					row.OriginalTarget = targetRaw
				}
			}
			journal.Rows = append(journal.Rows, row)
		}
		if err := saveDocsAdoptJournal(journalPath, journal); err != nil {
			return err
		}
	}
	byPath := map[string]int{}
	for i := range journal.Rows {
		byPath[journal.Rows[i].Path] = i
	}
	rollback, err := snapshotDocsAdoptBatch(repoRoot, prepared)
	if err != nil {
		return err
	}
	for _, item := range actionable {
		if item.alreadyApplied {
			continue
		}
		if err := verifyPreparedDocsAdoptCAS(repoRoot, item); err != nil {
			return err
		}
	}
	moves := map[string]string{}
	for _, item := range actionable {
		switch strings.ToLower(strings.TrimSpace(item.proposal.Disposition)) {
		case "promote", "merge":
			moves[item.proposal.Path] = item.proposal.Target
		}
	}
	failAfter, failArmed := docsAdoptFailAfter()
	applied := 0
	for _, item := range actionable {
		rowIdx, ok := byPath[item.proposal.Path]
		if !ok {
			return fmt.Errorf("documentation adoption recovery journal is missing row %s", item.proposal.Path)
		}
		row := &journal.Rows[rowIdx]
		if row.Done {
			currentSourceFP, _, _ := docsAdoptFileState(repoRoot, row.Path)
			currentTargetFP, _, _ := docsAdoptFileState(repoRoot, row.Target)
			if currentSourceFP == row.PostSourceFP && currentTargetFP == row.PostTargetFP {
				continue
			}
			if currentSourceFP == row.PreSourceFP && currentTargetFP == row.PreTargetFP {
				row.Done = false
			} else {
				return fmt.Errorf("documentation adoption cannot resume: %s changed during recovery; restore it or regenerate the reviewed table", row.Path)
			}
		}
		if item.alreadyApplied {
			row.Done = true
			if err := saveDocsAdoptJournal(journalPath, journal); err != nil {
				return err
			}
			continue
		}
		if failArmed && applied >= failAfter {
			return fmt.Errorf("injected docs adopt failure after %d applied rows (TUSKER_DOCS_ADOPT_FAIL_AFTER=%d)", applied, failAfter)
		}
		if err := applyPreparedDocsAdoptProposalMoves(repoRoot, item, moves); err != nil {
			if rollbackErr := restoreDocsAdoptBatch(repoRoot, rollback); rollbackErr != nil {
				return fmt.Errorf("%w (documentation adoption rollback failed: %v)", err, rollbackErr)
			}
			return err
		}
		postSourceFP, _, _ := docsAdoptFileState(repoRoot, row.Path)
		postTargetFP, _, _ := docsAdoptFileState(repoRoot, row.Target)
		row.PostSourceFP, row.PostTargetFP = postSourceFP, postTargetFP
		row.Done = true
		if err := saveDocsAdoptJournal(journalPath, journal); err != nil {
			return err
		}
		applied++
	}
	journal.Complete = true
	return saveDocsAdoptJournal(journalPath, journal)
}
