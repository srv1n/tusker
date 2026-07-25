package main

// This file is the trust boundary for scheduled landing receipts.  Receipt
// JSON is intentionally ordinary, inspectable data; authority is the daemon's
// in-memory Ed25519 capability plus a durable *public* issuance record.

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const v7LandingAuthoritySchema = "tusker.landing-authority-issuance/v1"

type v7LandingAuthorityContext struct {
	Schema          string             `json:"schema"`
	ProjectID       string             `json:"project_id"`
	RepoIdentity    string             `json:"repo_identity"`
	DepartureID     string             `json:"departure_id"`
	PolicyID        string             `json:"policy_id"`
	ScheduledWindow string             `json:"scheduled_window"`
	Candidate       DepartureCandidate `json:"candidate"`
	Target          string             `json:"target"`
}

type v7LandingAuthorityIssuance struct {
	AuthorityID     string                    `json:"authority_id"`
	ProjectID       string                    `json:"project_id"`
	RepoIdentity    string                    `json:"repo_identity"`
	DepartureID     string                    `json:"departure_id"`
	PolicyID        string                    `json:"policy_id"`
	ScheduledWindow string                    `json:"scheduled_window"`
	SessionID       string                    `json:"session_id"`
	HostIdentity    string                    `json:"host_identity"`
	ProcessIdentity string                    `json:"process_identity"`
	Generation      int                       `json:"generation"`
	Context         v7LandingAuthorityContext `json:"context"`
	PublicKey       []byte                    `json:"public_key"`
	IssuedAt        string                    `json:"issued_at"`
	ExpiresAt       string                    `json:"expires_at"`
	RevokedAt       string                    `json:"revoked_at"`
	ConsumedAt      string                    `json:"consumed_at"`
}

type v7LandingAuthority struct {
	Issuance v7LandingAuthorityIssuance
	private  ed25519.PrivateKey
	store    *RuntimeStore
}

func v7LandingAuthorityCandidate(candidate DepartureCandidate, taskIDs []string) DepartureCandidate {
	next := candidate
	next.CargoTaskIDs = append([]string(nil), taskIDs...)
	next.TaskStateRevisions = map[string]string{}
	next.TaskSourceSHAs = map[string]string{}
	for _, id := range taskIDs {
		next.TaskStateRevisions[id] = candidate.TaskStateRevisions[id]
		next.TaskSourceSHAs[id] = candidate.TaskSourceSHAs[id]
	}
	return next
}

func v7LandingRepoIdentity(repoRoot string) (string, error) {
	root, err := filepath.EvalSymlinks(repoRoot)
	if err != nil {
		return "", err
	}
	gitDir, err := gitOutputTrim(root, "rev-parse", "--git-dir")
	if err != nil {
		return "", err
	}
	remote, _ := gitOutputTrim(root, "config", "--get", "remote.origin.url")
	sum := sha256.Sum256([]byte(strings.Join([]string{root, gitDir, remote}, "\x00")))
	return fmt.Sprintf("sha256:%x", sum), nil
}

func (d *Daemon) issueV7LandingAuthority(project RegisteredProject, wf Workflow, run DepartureRun, candidate DepartureCandidate, target string) (*v7LandingAuthority, error) {
	if d == nil || d.store == nil || strings.TrimSpace(run.ID) == "" {
		return nil, fmt.Errorf("landing authority requires a resident daemon departure")
	}
	durable, err := d.store.FindDepartureRun(run.ID)
	if err != nil || durable == nil || durable.ProjectID != run.ProjectID || durable.PolicyID != run.PolicyID || durable.ScheduledWindow != run.ScheduledWindow {
		return nil, tuskerError(errorInvalidTransition, "landing authority refusal: durable departure run is missing or mismatched")
	}
	repoIdentity, err := v7LandingRepoIdentity(project.RepoRoot)
	if err != nil {
		return nil, err
	}
	host, hostOK := v7LandingLockHostIdentity()
	if !hostOK {
		return nil, tuskerError(errorInvalidTransition, "landing authority refusal: host identity unavailable")
	}
	started, startedOK := processStartTime(os.Getpid())
	if !startedOK {
		return nil, tuskerError(errorInvalidTransition, "landing authority refusal: daemon process identity unavailable")
	}
	pub, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	issuedAt := time.Now().UTC()
	generation, err := d.store.NextV7LandingAuthorityGeneration(project.ProjectID, run.ID)
	if err != nil {
		return nil, err
	}
	candidateSHA, err := gitOutputTrim(project.RepoRoot, "rev-parse", target+"^{commit}")
	if err != nil {
		return nil, tuskerError(errorInvalidTransition, "landing authority refusal: candidate ref is unavailable: "+target)
	}
	candidateTree, err := gitOutputTrim(project.RepoRoot, "rev-parse", candidateSHA+"^{tree}")
	if err != nil {
		return nil, err
	}
	candidate.CandidateSHA, candidate.CandidateTreeHash, candidate.IntegrationBaseSHA = candidateSHA, candidateTree, candidateSHA
	context := v7LandingAuthorityContext{Schema: v7LandingAuthoritySchema, ProjectID: project.ProjectID, RepoIdentity: repoIdentity, DepartureID: run.ID, PolicyID: run.PolicyID, ScheduledWindow: run.ScheduledWindow, Candidate: candidate, Target: target}
	issuance := v7LandingAuthorityIssuance{AuthorityID: "landing-authority-" + strings.ToLower(newRecordID()), ProjectID: project.ProjectID, RepoIdentity: repoIdentity, DepartureID: run.ID, PolicyID: run.PolicyID, ScheduledWindow: run.ScheduledWindow, SessionID: "daemon-" + strings.ToLower(newRecordID()), HostIdentity: host, ProcessIdentity: fmt.Sprintf("pid=%d;started=%s", os.Getpid(), started), Generation: generation, Context: context, PublicKey: append([]byte(nil), pub...), IssuedAt: issuedAt.Format(time.RFC3339Nano), ExpiresAt: issuedAt.Add(30 * time.Minute).Format(time.RFC3339Nano)}
	if err := d.store.CreateV7LandingAuthorityIssuance(issuance); err != nil {
		return nil, err
	}
	d.landingAuthorityMu.Lock()
	if d.landingAuthorityPrivate == nil {
		d.landingAuthorityPrivate = map[string]ed25519.PrivateKey{}
	}
	d.landingAuthorityPrivate[issuance.AuthorityID] = private
	d.landingAuthorityMu.Unlock()
	return &v7LandingAuthority{Issuance: issuance, private: private, store: d.store}, nil
}

func (s *RuntimeStore) NextV7LandingAuthorityGeneration(projectID, departureID string) (int, error) {
	var generation int
	err := s.queryRowScan(`SELECT COALESCE(MAX(generation), 0) + 1 FROM landing_authority_issuances WHERE project_id = ? AND departure_id = ?`, []any{projectID, departureID}, &generation)
	if err != nil {
		return 0, err
	}
	return generation, nil
}

func (s *RuntimeStore) CreateV7LandingAuthorityIssuance(issuance v7LandingAuthorityIssuance) error {
	if issuance.AuthorityID == "" || issuance.Context.Schema != v7LandingAuthoritySchema || len(issuance.PublicKey) != ed25519.PublicKeySize || issuance.ProjectID == "" || issuance.RepoIdentity == "" || issuance.DepartureID == "" || issuance.PolicyID == "" || issuance.ScheduledWindow == "" || issuance.SessionID == "" || issuance.HostIdentity == "" || issuance.ProcessIdentity == "" || issuance.Generation <= 0 {
		return fmt.Errorf("invalid landing authority issuance")
	}
	if issuance.Context.ProjectID != issuance.ProjectID || issuance.Context.RepoIdentity != issuance.RepoIdentity || issuance.Context.DepartureID != issuance.DepartureID || issuance.Context.PolicyID != issuance.PolicyID || issuance.Context.ScheduledWindow != issuance.ScheduledWindow {
		return fmt.Errorf("landing authority context does not bind issuance")
	}
	encoded, err := json.Marshal(issuance.Context)
	if err != nil {
		return err
	}
	_, err = s.exec(`INSERT INTO landing_authority_issuances (authority_id, project_id, repo_identity, departure_id, policy_id, scheduled_window, session_id, host_identity, process_identity, generation, context_json, public_key, issued_at, expires_at, revoked_at, consumed_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, '', '')`, issuance.AuthorityID, issuance.ProjectID, issuance.RepoIdentity, issuance.DepartureID, issuance.PolicyID, issuance.ScheduledWindow, issuance.SessionID, issuance.HostIdentity, issuance.ProcessIdentity, issuance.Generation, string(encoded), issuance.PublicKey, issuance.IssuedAt, issuance.ExpiresAt)
	return err
}

func (s *RuntimeStore) FindV7LandingAuthorityIssuance(id string) (*v7LandingAuthorityIssuance, error) {
	var issuance v7LandingAuthorityIssuance
	var contextJSON string
	err := s.queryRowScan(`SELECT authority_id, project_id, repo_identity, departure_id, policy_id, scheduled_window, session_id, host_identity, process_identity, generation, context_json, public_key, issued_at, expires_at, revoked_at, consumed_at FROM landing_authority_issuances WHERE authority_id = ?`, []any{id}, &issuance.AuthorityID, &issuance.ProjectID, &issuance.RepoIdentity, &issuance.DepartureID, &issuance.PolicyID, &issuance.ScheduledWindow, &issuance.SessionID, &issuance.HostIdentity, &issuance.ProcessIdentity, &issuance.Generation, &contextJSON, &issuance.PublicKey, &issuance.IssuedAt, &issuance.ExpiresAt, &issuance.RevokedAt, &issuance.ConsumedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if json.Unmarshal([]byte(contextJSON), &issuance.Context) != nil {
		return nil, fmt.Errorf("decode landing authority context")
	}
	return &issuance, nil
}

// verifyV7LandingReceiptAuthority is the command-path wrapper for the
// configured canonical daemon store.
func verifyV7LandingReceiptAuthority(repoRoot string, receipt v7LandingReceipt) bool {
	return verifyV7LandingReceiptAuthorityWithStore(repoRoot, receipt, nil)
}

// verifyV7LandingReceiptAuthorityWithStore deliberately treats
// receipt/cache/index data as hostile discovery material. Only an issuance in
// the caller's daemon-held store (or the configured canonical store for a
// historical daemon), created before the isolated gate, plus its
// daemon-private signature confers authority.
func verifyV7LandingReceiptAuthorityWithStore(repoRoot string, receipt v7LandingReceipt, trustedStore *RuntimeStore) bool {
	if receipt.Schema != v7LandingReceiptSchema ||
		receipt.Fingerprint != v7LandingReceiptFingerprint(receipt) ||
		receipt.ControlAuthority != v7LandingAuthorityDeparture || receipt.AuthorityID == "" ||
		receipt.ProjectID == "" || receipt.RepoIdentity == "" || receipt.DepartureID == "" ||
		receipt.PolicyID == "" || receipt.ScheduledWindow == "" || receipt.DaemonSessionID == "" ||
		receipt.DaemonHost == "" || receipt.DaemonProcess == "" || receipt.AuthorityGen <= 0 ||
		len(receipt.AuthoritySignature) != ed25519.SignatureSize {
		return false
	}
	repoIdentity, err := v7LandingRepoIdentity(repoRoot)
	if err != nil || repoIdentity != receipt.RepoIdentity {
		return false
	}
	if trustedStore != nil && verifyV7LandingReceiptAuthorityInStore(receipt, trustedStore) {
		return true
	}
	defaultRoot := DefaultStateRoot()
	if trustedStore != nil && filepath.Clean(trustedStore.stateRoot) == filepath.Clean(defaultRoot) {
		return false
	}
	store, err := OpenRuntimeStoreReadOnly(defaultRoot)
	if err != nil {
		return false
	}
	defer store.Close()
	return verifyV7LandingReceiptAuthorityInStore(receipt, store)
}

func verifyV7LandingReceiptAuthorityInStore(receipt v7LandingReceipt, store *RuntimeStore) bool {
	if store == nil {
		return false
	}
	issuance, err := store.FindV7LandingAuthorityIssuance(receipt.AuthorityID)
	if err != nil || issuance == nil || issuance.RevokedAt != "" || len(issuance.PublicKey) != ed25519.PublicKeySize {
		return false
	}
	run, err := store.FindDepartureRun(receipt.DepartureID)
	if err != nil || run == nil || run.ProjectID != receipt.ProjectID || run.PolicyID != receipt.PolicyID || run.ScheduledWindow != receipt.ScheduledWindow {
		return false
	}
	now := time.Now().UTC()
	issuedAt, issuedErr := time.Parse(time.RFC3339Nano, issuance.IssuedAt)
	expiresAt, expiresErr := time.Parse(time.RFC3339Nano, issuance.ExpiresAt)
	receiptAt, receiptErr := time.Parse(time.RFC3339Nano, receipt.ReceiptIssuedAt)
	if issuedErr != nil || expiresErr != nil || receiptErr != nil || !expiresAt.After(issuedAt) || receiptAt.Before(issuedAt) || receiptAt.After(expiresAt) || now.Before(issuedAt.Add(-5*time.Minute)) {
		return false
	}
	if issuance.ProjectID != receipt.ProjectID || issuance.RepoIdentity != receipt.RepoIdentity || issuance.DepartureID != receipt.DepartureID ||
		issuance.PolicyID != receipt.PolicyID || issuance.ScheduledWindow != receipt.ScheduledWindow || issuance.SessionID != receipt.DaemonSessionID ||
		issuance.HostIdentity != receipt.DaemonHost || issuance.ProcessIdentity != receipt.DaemonProcess || issuance.Generation != receipt.AuthorityGen ||
		issuance.Context.Schema != v7LandingAuthoritySchema || issuance.Context.Target != receipt.Target ||
		issuance.Context.Candidate.CandidateSHA == "" || issuance.Context.Candidate.CandidateTreeHash == "" {
		return false
	}
	seen := map[string]bool{}
	for _, proof := range receipt.Tasks {
		if issuance.Context.Candidate.TaskSourceSHAs[proof.Task] != proof.SourceSHA || seen[proof.Task] {
			return false
		}
		seen[proof.Task] = true
	}
	// One departure can stage several waves. Each signed receipt may therefore
	// cover a strict, exact subset of the departure's frozen cargo; it must
	// never introduce a task or source outside that candidate.
	if len(seen) == 0 {
		return false
	}
	for id := range seen {
		if !containsString(issuance.Context.Candidate.CargoTaskIDs, id) || run.Candidate.TaskSourceSHAs[id] != issuance.Context.Candidate.TaskSourceSHAs[id] {
			return false
		}
	}
	return ed25519.Verify(ed25519.PublicKey(issuance.PublicKey), []byte(receipt.Fingerprint), receipt.AuthoritySignature)
}
