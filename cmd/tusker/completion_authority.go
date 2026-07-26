package main

// Completion authority is intentionally separate from the Git integration
// lane.  Git objects are public and writable by a linked-worktree worker; the
// resident daemon's short-lived private key is the capability that turns an
// otherwise well-formed receipt into an authenticated close.

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var trustedCompletionStores = struct {
	sync.Mutex
	stores map[*RuntimeStore]struct{}
}{stores: map[*RuntimeStore]struct{}{}}

func registerTrustedCompletionStore(store *RuntimeStore) {
	if store == nil {
		return
	}
	trustedCompletionStores.Lock()
	trustedCompletionStores.stores[store] = struct{}{}
	trustedCompletionStores.Unlock()
}

func unregisterTrustedCompletionStore(store *RuntimeStore) {
	trustedCompletionStores.Lock()
	delete(trustedCompletionStores.stores, store)
	trustedCompletionStores.Unlock()
}

func completionCanonicalPhysicalDirectory(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("directory path is empty")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	physical, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(physical)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%s is not a directory", physical)
	}
	return filepath.Clean(physical), nil
}

const completionAuthoritySchema = "tusker.completion-authority-issuance/v1"

type completionAuthorityContext struct {
	Schema          string `json:"schema"`
	ProjectID       string `json:"project_id"`
	RepoIdentity    string `json:"repo_identity"`
	TransactionID   string `json:"transaction_id"`
	TaskID          string `json:"task_id"`
	ResultRevision  string `json:"result_revision"`
	TaskStateRev    string `json:"task_state_rev"`
	WorkRevision    int    `json:"work_revision"`
	Implementation  string `json:"implementation_sha"`
	ReviewAttempt   string `json:"review_attempt"`
	WaveID          string `json:"wave_id"`
	IntegrationRef  string `json:"integration_ref"`
	IntegrationBase string `json:"integration_base"`
	TaskBlob        string `json:"task_blob"`
	ReceiptBlob     string `json:"receipt_blob"`
}

type completionAuthorityIssuance struct {
	AuthorityID   string
	ProjectID     string
	StoreIdentity string
	RepoIdentity  string
	TransactionID string
	Context       completionAuthorityContext
	PublicKey     []byte
	IssuedAt      string
	BoundAt       string
	ConsumedAt    string
	RevokedAt     string
}

func completionRuntimeStoreIdentity(store *RuntimeStore) string {
	if store == nil {
		return ""
	}
	root, err := completionCanonicalPhysicalDirectory(store.stateRoot)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256([]byte("tusker.completion-runtime-store/v1\x00" + root))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func completionRepoIdentity(repoRoot string) (string, error) {
	root, err := completionCanonicalPhysicalDirectory(repoRoot)
	if err != nil {
		return "", err
	}
	gitDir, err := gitOutputTrim(root, "rev-parse", "--git-common-dir")
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(root, gitDir)
	}
	gitDir, err = completionCanonicalPhysicalDirectory(gitDir)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte("tusker.completion-repo-identity/v1\x00" + root + "\x00" + gitDir))
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func completionAuthorityContextFor(project RegisteredProject, result ReviewResult, tx *completionTransaction) (completionAuthorityContext, error) {
	if tx == nil || tx.ID == "" {
		return completionAuthorityContext{}, fmt.Errorf("completion authority requires a transaction")
	}
	repo, err := completionRepoIdentity(project.RepoRoot)
	if err != nil {
		return completionAuthorityContext{}, err
	}
	return completionAuthorityContext{Schema: completionAuthoritySchema, ProjectID: project.ProjectID, RepoIdentity: repo,
		TransactionID: tx.ID, TaskID: tx.TaskID, ResultRevision: tx.ResultRevision, TaskStateRev: tx.ReviewedTaskStateRev,
		WorkRevision: tx.WorkRevision, Implementation: tx.ImplementationSHA, ReviewAttempt: tx.ReviewAttempt,
		WaveID: tx.WaveID, IntegrationRef: tx.IntegrationRef, IntegrationBase: tx.IntegrationBase}, nil
}

func (s *RuntimeStore) completionAuthorityIssuance(id string) (*completionAuthorityIssuance, error) {
	var out completionAuthorityIssuance
	var raw string
	err := s.queryRowScan(`SELECT authority_id,project_id,store_identity,repo_identity,transaction_id,context_json,public_key,issued_at,bound_at,consumed_at,revoked_at FROM completion_authority_issuances WHERE authority_id=?`, []any{id}, &out.AuthorityID, &out.ProjectID, &out.StoreIdentity, &out.RepoIdentity, &out.TransactionID, &raw, &out.PublicKey, &out.IssuedAt, &out.BoundAt, &out.ConsumedAt, &out.RevokedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(raw), &out.Context); err != nil {
		return nil, err
	}
	return &out, nil
}

func (s *RuntimeStore) completionAuthorityForTransaction(projectID, transactionID string) (*completionAuthorityIssuance, error) {
	var id string
	err := s.queryRowScan(`SELECT authority_id FROM completion_authority_issuances WHERE project_id=? AND transaction_id=?`, []any{projectID, transactionID}, &id)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return s.completionAuthorityIssuance(id)
}

func (s *RuntimeStore) createCompletionAuthorityIssuance(i completionAuthorityIssuance) error {
	raw, err := json.Marshal(i.Context)
	if err != nil {
		return err
	}
	_, err = s.exec(`INSERT INTO completion_authority_issuances(authority_id,project_id,store_identity,repo_identity,transaction_id,context_json,public_key,issued_at,bound_at,consumed_at,revoked_at) VALUES(?,?,?,?,?,?,?,?,'','','')`, i.AuthorityID, i.ProjectID, i.StoreIdentity, i.RepoIdentity, i.TransactionID, string(raw), i.PublicKey, i.IssuedAt)
	return err
}

func (s *RuntimeStore) bindCompletionAuthority(authorityID string, context completionAuthorityContext) error {
	raw, err := json.Marshal(context)
	if err != nil {
		return err
	}
	result, err := s.exec(`UPDATE completion_authority_issuances SET context_json=?,bound_at=? WHERE authority_id=? AND consumed_at='' AND revoked_at=''`, string(raw), time.Now().UTC().Format(time.RFC3339Nano), authorityID)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return fmt.Errorf("completion authority binding is no longer pending")
	}
	return nil
}

func (s *RuntimeStore) consumeCompletionAuthority(authorityID string) error {
	result, err := s.exec(`UPDATE completion_authority_issuances SET consumed_at=? WHERE authority_id=? AND consumed_at='' AND revoked_at='' AND bound_at<>''`, time.Now().UTC().Format(time.RFC3339Nano), authorityID)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		i, loadErr := s.completionAuthorityIssuance(authorityID)
		if loadErr == nil && i != nil && i.ConsumedAt != "" && i.RevokedAt == "" {
			return nil
		}
		return fmt.Errorf("completion authority was not bound for consumption")
	}
	return nil
}

func completionAuthorityPayload(c completionAuthorityContext) []byte {
	// The signature intentionally excludes receipt_blob: including it would
	// make the receipt claim to sign its own Git object.  ReceiptBlob is *not*
	// signature-covered.  It is instead bound by the daemon-owned issuance and
	// must be proved against the exact candidate tree entry at every use site.
	c.TaskBlob, c.ReceiptBlob = "", ""
	raw, _ := json.Marshal(c)
	return raw
}

func (d *Daemon) issueCompletionAuthority(project RegisteredProject, result ReviewResult, tx *completionTransaction) error {
	if d == nil || d.store == nil {
		return fmt.Errorf("completion authority requires resident daemon store")
	}
	if existing, err := d.store.completionAuthorityForTransaction(project.ProjectID, tx.ID); err != nil {
		return err
	} else if existing != nil {
		tx.CompletionAuthorityID = existing.AuthorityID
		d.completionAuthorityMu.Lock()
		private := d.completionAuthorityKey[existing.AuthorityID]
		d.completionAuthorityMu.Unlock()
		if len(private) != ed25519.PrivateKeySize {
			return completionFrozenAuthorityRepairError(tx, "pending completion authority lost its resident-daemon capability")
		}
		tx.CompletionAuthoritySig = ed25519.Sign(private, completionAuthorityPayload(existing.Context))
		return nil
	}
	context, err := completionAuthorityContextFor(project, result, tx)
	if err != nil {
		return err
	}
	pub, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	id := "completion-authority-" + strings.ToLower(newRecordID())
	issuance := completionAuthorityIssuance{AuthorityID: id, ProjectID: project.ProjectID, StoreIdentity: completionRuntimeStoreIdentity(d.store), RepoIdentity: context.RepoIdentity, TransactionID: tx.ID, Context: context, PublicKey: pub, IssuedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	if err := d.store.createCompletionAuthorityIssuance(issuance); err != nil {
		return err
	}
	d.completionAuthorityMu.Lock()
	if d.completionAuthorityKey == nil {
		d.completionAuthorityKey = map[string]ed25519.PrivateKey{}
	}
	d.completionAuthorityKey[id] = private
	d.completionAuthorityMu.Unlock()
	tx.CompletionAuthorityID = id
	tx.CompletionAuthoritySig = ed25519.Sign(private, completionAuthorityPayload(context))
	return nil
}

func (d *Daemon) bindCompletionAuthority(project RegisteredProject, result ReviewResult, tx *completionTransaction, taskBlob, receiptBlob string) error {
	if tx == nil || tx.CompletionAuthorityID == "" {
		return completionFrozenAuthorityRepairError(tx, "completion authority issuance is missing")
	}
	i, err := d.store.completionAuthorityIssuance(tx.CompletionAuthorityID)
	if err != nil || i == nil {
		return completionFrozenAuthorityRepairError(tx, "completion authority issuance is unavailable")
	}
	expected, err := completionAuthorityContextFor(project, result, tx)
	if err != nil {
		return err
	}
	if i.Context.TaskBlob != "" && i.Context.TaskBlob != taskBlob || i.Context.ReceiptBlob != "" && i.Context.ReceiptBlob != receiptBlob {
		return completionFrozenAuthorityRepairError(tx, "completion authority object binding drifted")
	}
	if i.Context.Schema != expected.Schema || i.Context.ProjectID != expected.ProjectID || i.Context.RepoIdentity != expected.RepoIdentity || i.Context.TransactionID != expected.TransactionID || i.Context.TaskID != expected.TaskID || i.Context.ResultRevision != expected.ResultRevision || i.Context.TaskStateRev != expected.TaskStateRev || i.Context.WorkRevision != expected.WorkRevision || i.Context.Implementation != expected.Implementation || i.Context.ReviewAttempt != expected.ReviewAttempt || i.Context.WaveID != expected.WaveID || i.Context.IntegrationRef != expected.IntegrationRef || i.Context.IntegrationBase != expected.IntegrationBase {
		return completionFrozenAuthorityRepairError(tx, "completion authority context drifted")
	}
	expected.TaskBlob, expected.ReceiptBlob = taskBlob, receiptBlob
	if receiptBlob != "" && i.BoundAt == "" {
		if err := d.store.bindCompletionAuthority(i.AuthorityID, expected); err != nil {
			return err
		}
	}
	return nil
}

func verifyCompletionReceiptAuthority(repoRoot string, receipt completionReceipt, store *RuntimeStore, requireConsumed bool) bool {
	if store == nil || receipt.Authority.ID == "" || len(receipt.Authority.Signature) != ed25519.SignatureSize {
		return false
	}
	i, err := store.completionAuthorityIssuance(receipt.Authority.ID)
	if err != nil || i == nil || i.RevokedAt != "" || i.StoreIdentity == "" || i.StoreIdentity != completionRuntimeStoreIdentity(store) || len(i.PublicKey) != ed25519.PublicKeySize {
		return false
	}
	if requireConsumed && i.ConsumedAt == "" {
		return false
	}
	repo, err := completionRepoIdentity(repoRoot)
	if err != nil || repo != i.RepoIdentity {
		return false
	}
	tr := receipt.Transaction
	if i.ProjectID != tr.ProjectID || i.TransactionID != tr.ID || i.Context.Schema != completionAuthoritySchema ||
		i.Context.ProjectID != tr.ProjectID || i.Context.RepoIdentity != repo || i.Context.TransactionID != tr.ID ||
		i.Context.TaskID != tr.TaskID || i.Context.ResultRevision != tr.ResultRevision || i.Context.TaskStateRev != tr.ReviewedTaskStateRev ||
		i.Context.WorkRevision != tr.WorkRevision || i.Context.Implementation != tr.ImplementationSHA || i.Context.ReviewAttempt != tr.ReviewAttempt ||
		i.Context.WaveID != tr.WaveID || i.Context.IntegrationRef != tr.IntegrationRef || i.Context.IntegrationBase != tr.IntegrationBase ||
		i.Context.TaskBlob != receipt.TaskBlob || i.Context.ReceiptBlob == "" {
		return false
	}
	return ed25519.Verify(ed25519.PublicKey(i.PublicKey), completionAuthorityPayload(i.Context), receipt.Authority.Signature)
}

// A caller that lacks the daemon's exact store may use only a daemon-registered
// store in this process or the canonical service root derived without ambient
// TUSKER_STATE_ROOT. It must never select a worker-provided look-alike store.
func verifyCompletionReceiptAuthorityWithStore(repoRoot string, receipt completionReceipt, store *RuntimeStore, requireConsumed bool) bool {
	if store != nil {
		return verifyCompletionReceiptAuthority(repoRoot, receipt, store, requireConsumed)
	}
	trustedCompletionStores.Lock()
	stores := make([]*RuntimeStore, 0, len(trustedCompletionStores.stores))
	for candidate := range trustedCompletionStores.stores {
		stores = append(stores, candidate)
	}
	trustedCompletionStores.Unlock()
	for _, candidate := range stores {
		if verifyCompletionReceiptAuthority(repoRoot, receipt, candidate, requireConsumed) {
			return true
		}
	}
	root := canonicalOfflineCompletionStateRoot()
	if root == "" {
		return false
	}
	canonical, err := OpenRuntimeStoreReadOnly(root)
	if err != nil {
		return false
	}
	defer canonical.Close()
	return verifyCompletionReceiptAuthority(repoRoot, receipt, canonical, requireConsumed)
}

func canonicalOfflineCompletionStateRoot() string {
	home := userHomeDir()
	if home == "" {
		return ""
	}
	return filepath.Join(home, "Library", "Application Support", "tusker")
}
