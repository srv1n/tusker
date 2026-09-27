package main

// Completion authority is intentionally separate from the Git integration
// lane.  Git objects are public and writable by a linked-worktree worker; the
// resident daemon's short-lived private key is the capability that turns an
// otherwise well-formed receipt into an authenticated close.

import (
	"crypto/ed25519"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

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

const completionAuthoritySchema = "tusker.completion-authority-issuance/v2"

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
	WorkerPolicyFP  string `json:"worker_policy_fingerprint"`
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

func completionAuthorityPayload(c completionAuthorityContext) []byte {
	// The signature intentionally excludes receipt_blob: including it would
	// make the receipt claim to sign its own Git object.  ReceiptBlob is *not*
	// signature-covered.  It is instead bound by the daemon-owned issuance and
	// must be proved against the exact candidate tree entry at every use site.
	c.TaskBlob, c.ReceiptBlob = "", ""
	raw, _ := json.Marshal(c)
	return raw
}

func verifyCompletionReceiptAuthority(repoRoot string, receipt completionReceipt, receiptEntry completionGitTreeEntry, store *RuntimeStore, requireConsumed bool) bool {
	if store == nil || receipt.Authority.ID == "" || len(receipt.Authority.Signature) != ed25519.SignatureSize ||
		receiptEntry.Type != "blob" || receiptEntry.Mode != "100644" || receiptEntry.OID == "" {
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
		i.Context.WaveID != tr.WaveID || i.Context.WorkerPolicyFP != tr.WorkerPolicyFP || i.Context.IntegrationRef != tr.IntegrationRef || i.Context.IntegrationBase != tr.IntegrationBase ||
		i.Context.TaskBlob != receipt.TaskBlob || i.Context.ReceiptBlob != receiptEntry.OID {
		return false
	}
	return ed25519.Verify(ed25519.PublicKey(i.PublicKey), completionAuthorityPayload(i.Context), receipt.Authority.Signature)
}

func verifyCompletionReceiptAuthorityWithStore(repoRoot string, receipt completionReceipt, receiptEntry completionGitTreeEntry, store *RuntimeStore, requireConsumed bool) bool {
	return verifyCompletionReceiptAuthority(repoRoot, receipt, receiptEntry, store, requireConsumed)
}
