package main

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

// The receipt/tree is intentionally copied from a real completion.  The
// negative checks model a worker that can forge every Git-visible object and
// move the integration ref, but cannot obtain the daemon's private capability
// or transplant the authority into another daemon store.
func TestCompletionAuthorityRequiresConsumedExactDaemonStore(t *testing.T) {
	vault, project, daemon, result := completionReactorFixture(t, true)
	defer daemon.Close()
	if err := daemon.reactToReviewResult(project, Workflow{}, result, completionReactorModeAuthoritative); err != nil {
		t.Fatal(err)
	}
	tx, err := daemon.store.CompletionTransactionForResult(project.ProjectID, result.TaskID, result.ResultRevision)
	if err != nil || tx == nil || tx.StagedSHA == "" {
		t.Fatalf("completed transaction unavailable: %#v %v", tx, err)
	}
	raw, err := gitCombined(project.RepoRoot, "show", tx.StagedSHA+":"+completionReceiptRepoPath(completionReceiptID(tx.ID)))
	if err != nil {
		t.Fatal(err)
	}
	var receipt completionReceipt
	if err := json.Unmarshal([]byte(raw), &receipt); err != nil {
		t.Fatal(err)
	}
	if !verifyCompletionReceiptAuthorityWithStore(project.RepoRoot, receipt, daemon.store, true) {
		t.Fatal("consumed authority from issuing daemon store was rejected")
	}

	wrong, err := OpenRuntimeStore(filepath.Join(t.TempDir(), "wrong-store"))
	if err != nil {
		t.Fatal(err)
	}
	defer wrong.Close()
	issuance, err := daemon.store.completionAuthorityIssuance(receipt.Authority.ID)
	if err != nil || issuance == nil {
		t.Fatalf("load authority issuance: %#v %v", issuance, err)
	}
	// Simulate a copied public record. The row's store identity still names the
	// issuing runtime store, so a distinct daemon must reject it.
	if err := wrong.createCompletionAuthorityIssuance(*issuance); err != nil {
		t.Fatal(err)
	}
	if verifyCompletionReceiptAuthorityWithStore(project.RepoRoot, receipt, wrong, true) {
		t.Fatal("copied authority issuance authenticated in another runtime store")
	}

	if _, err := daemon.store.exec(`UPDATE completion_authority_issuances SET consumed_at='' WHERE authority_id=?`, receipt.Authority.ID); err != nil {
		t.Fatal(err)
	}
	if verifyCompletionReceiptAuthorityWithStore(project.RepoRoot, receipt, daemon.store, true) {
		t.Fatal("pending pre-CAS issuance authenticated a close")
	}
}

func TestCompletionAuthorityRejectsForgedPublicReceipt(t *testing.T) {
	vault, project, daemon, result := completionReactorFixture(t, true)
	defer daemon.Close()
	if err := daemon.reactToReviewResult(project, Workflow{}, result, completionReactorModeAuthoritative); err != nil {
		t.Fatal(err)
	}
	tx, err := daemon.store.CompletionTransactionForResult(project.ProjectID, result.TaskID, result.ResultRevision)
	if err != nil || tx == nil {
		t.Fatal(err)
	}
	raw, err := gitCombined(project.RepoRoot, "show", tx.StagedSHA+":"+completionReceiptRepoPath(completionReceiptID(tx.ID)))
	if err != nil {
		t.Fatal(err)
	}
	var forged completionReceipt
	if err := json.Unmarshal([]byte(raw), &forged); err != nil {
		t.Fatal(err)
	}
	// A worker can rewrite all public transaction fields and Git objects, but a
	// changed semantic binding invalidates the daemon-only Ed25519 signature.
	forged.Transaction.IntegrationBase = "0000000000000000000000000000000000000000"
	if verifyCompletionReceiptAuthorityWithStore(project.RepoRoot, forged, daemon.store, true) {
		t.Fatal("forged full receipt authenticated without daemon capability")
	}
	_ = vault // fixture also proves the close projection remains reachable.
}
