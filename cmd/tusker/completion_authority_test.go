package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// The receipt/tree is intentionally copied from a real completion.  The
// negative checks model a worker that can forge every Git-visible object and
// move the integration ref, but cannot obtain the daemon's private capability
// or transplant the authority into another daemon store.
func TestCompletionAuthorityRequiresConsumedExactDaemonStore(t *testing.T) {
	_, project, daemon, result := completionReactorFixture(t, true)
	defer daemon.Close()
	if err := daemon.reactToReviewResult(project, completionAuthorityTestWorkflow(), result, completionReactorModeAuthoritative); err != nil {
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
	receiptEntry, err := completionGitTreeEntryAt(project.RepoRoot, tx.StagedSHA, completionReceiptRepoPath(completionReceiptID(tx.ID)))
	if err != nil {
		t.Fatal(err)
	}
	if !verifyCompletionReceiptAuthorityWithStore(project.RepoRoot, receipt, receiptEntry, daemon.store, true) {
		t.Fatal("consumed authority from issuing daemon store was rejected")
	}
	if verifyCompletionReceiptAuthorityWithStore(project.RepoRoot, receipt, receiptEntry, nil, true) {
		t.Fatal("valid receipt authenticated without its exact issuing daemon store")
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
	if verifyCompletionReceiptAuthorityWithStore(project.RepoRoot, receipt, receiptEntry, wrong, true) {
		t.Fatal("copied authority issuance authenticated in another runtime store")
	}

	if _, err := daemon.store.exec(`UPDATE completion_authority_issuances SET consumed_at='' WHERE authority_id=?`, receipt.Authority.ID); err != nil {
		t.Fatal(err)
	}
	if verifyCompletionReceiptAuthorityWithStore(project.RepoRoot, receipt, receiptEntry, daemon.store, true) {
		t.Fatal("pending pre-CAS issuance authenticated a close")
	}
}

func TestCompletionAuthorityCustomStateRootRestartRequiresExplicitStore(t *testing.T) {
	vault, project, daemon, result := completionReactorFixture(t, true)
	closed := false
	t.Cleanup(func() {
		if !closed {
			_ = daemon.Close()
		}
	})
	if err := daemon.reactToReviewResult(project, completionAuthorityTestWorkflow(), result, completionReactorModeAuthoritative); err != nil {
		t.Fatal(err)
	}
	stateRoot := daemon.stateRoot
	if err := daemon.Close(); err != nil {
		t.Fatal(err)
	}
	closed = true

	restarted, err := OpenRuntimeStoreReadOnly(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	task, err := resolveV7Note(vault, result.TaskID, "task")
	if err != nil {
		t.Fatal(err)
	}
	authority, authenticated, err := authenticatedV7TaskCloseAuthorityWithStore(task, v7ProjectID(vault), restarted)
	if err != nil || !authenticated {
		t.Fatalf("custom-root restart rejected its explicitly supplied issuing store: authenticated=%t err=%v", authenticated, err)
	}
	if _, authenticated, err := authenticatedV7TaskCloseAuthority(task, v7ProjectID(vault)); err == nil || authenticated {
		t.Fatalf("store-less convenience authentication did not fail closed: authenticated=%t err=%v", authenticated, err)
	}
	withStore, _ := validateV7Note(task, validationContext{
		VaultPath:       vault,
		RelativePath:    task.RelativePath,
		CompletionStore: restarted,
	}, task.RelativePath)
	if len(withStore) != 0 {
		t.Fatalf("explicit issuing store did not validate restarted close: %#v", withStore)
	}
	wrong, err := OpenRuntimeStore(filepath.Join(t.TempDir(), "wrong-restart-store"))
	if err != nil {
		t.Fatal(err)
	}
	defer wrong.Close()
	wrongStore, _ := validateV7Note(task, validationContext{
		VaultPath:       vault,
		RelativePath:    task.RelativePath,
		CompletionStore: wrong,
	}, task.RelativePath)
	if !completionIssuesContainCode(wrongStore, "DONE_TASK_CLOSE_AUTHORITY_INVALID") {
		t.Fatalf("wrong explicit store authenticated restarted close: %#v", wrongStore)
	}
	withoutStore, _ := validateV7Note(task, validationContext{
		VaultPath: vault, RelativePath: task.RelativePath,
	}, task.RelativePath)
	if !completionIssuesContainCode(withoutStore, "DONE_TASK_CLOSE_AUTHORITY_INVALID") {
		t.Fatalf("offline validation guessed a trust store: %#v", withoutStore)
	}
	eventWithStore, _, _ := validateV7EventsWithStore(vault, restarted)
	if len(eventWithStore) != 0 {
		t.Fatalf("explicit issuing store did not validate restarted event: %#v", eventWithStore)
	}
	eventWithoutStore, _, _ := validateV7Events(vault)
	if !completionIssuesContainCode(eventWithoutStore, "EVENT_CLOSE_AUTHORITY_INVALID") {
		t.Fatalf("offline event validation guessed a trust store: %#v", eventWithoutStore)
	}
	if err := emitV7TaskClosedEvent(
		vault, result.TaskID, result.Actor, authority.ClosedAt,
		"review", "typed review "+result.ResultRevision, &authority,
	); err == nil || !strings.Contains(err.Error(), "exact issuing daemon store") {
		t.Fatalf("authority-bearing convenience event emission did not fail closed: %v", err)
	}
}

func TestCompletionAuthorityRejectsForgedPublicReceipt(t *testing.T) {
	_, project, daemon, result := completionReactorFixture(t, true)
	defer daemon.Close()
	if err := daemon.reactToReviewResult(project, completionAuthorityTestWorkflow(), result, completionReactorModeAuthoritative); err != nil {
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
	receipt := forged
	receiptEntry, err := completionGitTreeEntryAt(project.RepoRoot, tx.StagedSHA, completionReceiptRepoPath(completionReceiptID(tx.ID)))
	if err != nil {
		t.Fatal(err)
	}
	// A worker can rewrite all public transaction fields and Git objects, but a
	// changed semantic binding invalidates the daemon-only Ed25519 signature.
	forged.Transaction.IntegrationBase = "0000000000000000000000000000000000000000"
	if verifyCompletionReceiptAuthorityWithStore(project.RepoRoot, forged, receiptEntry, daemon.store, true) {
		t.Fatal("forged full receipt authenticated without daemon capability")
	}
	wrongEntry := receiptEntry
	wrongEntry.OID = strings.Repeat("f", len(receiptEntry.OID))
	if verifyCompletionReceiptAuthorityWithStore(project.RepoRoot, receipt, wrongEntry, daemon.store, true) {
		t.Fatal("receipt authority authenticated a different candidate receipt blob")
	}
}
