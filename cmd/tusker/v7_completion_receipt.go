package main

// Completion receipts are deliberately one-way. The task records only the
// deterministic receipt identity; the receipt then binds the already-hashed
// task blob, reviewed result, frozen transaction and close projection. Nothing
// in the task needs to know the staging commit/tree/blob, avoiding the usual
// self-authentication loop.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
)

const completionReceiptSchema = "tusker.completion-receipt/v1"

type completionReceiptTransaction struct {
	ID, ProjectID, TaskID, ResultRevision, ReviewedTaskStateRev                      string
	WorkRevision                                                                     int
	ImplementationSHA, ReviewAttempt, IntegrationBase, IntegrationRef, StagingRef    string
	WaveID, WaveAuthorityKind, WaveAuthorizationFP, WaveMaterialFP, CloseAuthorityFP string
}

type completionReceipt struct {
	Schema      string                             `json:"schema"`
	ReceiptID   string                             `json:"receipt_id"`
	TaskPath    string                             `json:"task_path"`
	TaskBlob    string                             `json:"task_blob"`
	TaskMode    string                             `json:"task_mode"`
	Review      ReviewResult                       `json:"review"`
	Transaction completionReceiptTransaction       `json:"transaction"`
	Close       completionCloseAuthorityProjection `json:"close_projection"`
	Authority   completionReceiptAuthority         `json:"completion_authority"`
}

type completionReceiptAuthority struct {
	ID        string `json:"id"`
	Signature []byte `json:"signature"`
}

func completionReceiptID(transactionID string) string {
	sum := sha256.Sum256([]byte("tusker.completion-receipt/v1\x00" + transactionID))
	return "receipt:" + hex.EncodeToString(sum[:])
}

func completionReceiptRepoPath(receiptID string) string {
	return ".tusker/completion-receipts/" + strings.TrimPrefix(receiptID, "receipt:") + ".json"
}

func newCompletionReceipt(vaultPath, taskPath string, task completionGitTreeEntry, result ReviewResult, tx *completionTransaction) (completionReceipt, []byte, error) {
	if tx == nil || task.Mode != "100644" || task.Type != "blob" || task.OID == "" {
		return completionReceipt{}, nil, fmt.Errorf("completion receipt requires exact regular task blob")
	}
	closeProjection, err := completionCloseAuthorityProjectionSnapshot(vaultPath, tx.IntegrationBase, result)
	if err != nil {
		return completionReceipt{}, nil, err
	}
	rawProjection, _ := json.Marshal(closeProjection)
	sum := sha256.Sum256(rawProjection)
	if tx.CloseAuthorityFP != "sha256:"+hex.EncodeToString(sum[:]) {
		return completionReceipt{}, nil, fmt.Errorf("completion receipt close projection does not match frozen authority")
	}
	r := completionReceipt{Schema: completionReceiptSchema, ReceiptID: completionReceiptID(tx.ID), TaskPath: taskPath, TaskBlob: task.OID, TaskMode: task.Mode, Review: result,
		Transaction: completionReceiptTransaction{ID: tx.ID, ProjectID: tx.ProjectID, TaskID: tx.TaskID, ResultRevision: tx.ResultRevision, ReviewedTaskStateRev: tx.ReviewedTaskStateRev, WorkRevision: tx.WorkRevision, ImplementationSHA: tx.ImplementationSHA, ReviewAttempt: tx.ReviewAttempt, IntegrationBase: tx.IntegrationBase, IntegrationRef: tx.IntegrationRef, StagingRef: tx.StagingRef, WaveID: tx.WaveID, WaveAuthorityKind: tx.WaveAuthorityKind, WaveAuthorizationFP: tx.WaveAuthorizationFP, WaveMaterialFP: tx.WaveMaterialFP, CloseAuthorityFP: tx.CloseAuthorityFP}, Close: closeProjection, Authority: completionReceiptAuthority{ID: tx.CompletionAuthorityID, Signature: append([]byte(nil), tx.CompletionAuthoritySig...)}}
	raw, err := json.Marshal(r)
	if err != nil {
		return completionReceipt{}, nil, err
	}
	return r, raw, nil
}

func validateCompletionReceipt(raw []byte, taskPath string, task completionGitTreeEntry, result ReviewResult, tx *completionTransaction, taskData map[string]any, taskBody string) error {
	var receipt completionReceipt
	if err := json.Unmarshal(raw, &receipt); err != nil {
		return fmt.Errorf("completion receipt is malformed: %w", err)
	}
	canonical, err := json.Marshal(receipt)
	if err != nil || string(canonical) != string(raw) {
		return fmt.Errorf("completion receipt is not canonical")
	}
	if tx == nil || receipt.Schema != completionReceiptSchema || receipt.ReceiptID != completionReceiptID(tx.ID) || receipt.TaskPath != taskPath || receipt.TaskBlob != task.OID || receipt.TaskMode != "100644" {
		return fmt.Errorf("completion receipt identity or task entry mismatch")
	}
	if receipt.Authority.ID != tx.CompletionAuthorityID || len(receipt.Authority.Signature) != 64 {
		return fmt.Errorf("completion receipt authority is missing or mismatched")
	}
	if err := validatePersistedReviewResult(receipt.Review); err != nil {
		return fmt.Errorf("completion receipt review invalid: %w", err)
	}
	if receipt.Review.Schema != reviewResultSchema {
		return fmt.Errorf("completion receipt cannot use legacy review-result authority")
	}
	if receipt.Review.ResultRevision != result.ResultRevision || !reflect.DeepEqual(receipt.Review, result) {
		return fmt.Errorf("completion receipt review does not match consumed review result")
	}
	tr := receipt.Transaction
	if receipt.Review.ProjectID != tr.ProjectID || result.ProjectID != tr.ProjectID {
		return fmt.Errorf("completion receipt review and transaction project identities differ")
	}
	if tr.ID != tx.ID ||
		tr.ProjectID != tx.ProjectID ||
		tr.TaskID != tx.TaskID ||
		tr.TaskID != result.TaskID ||
		tr.ResultRevision != tx.ResultRevision ||
		tr.ResultRevision != result.ResultRevision ||
		tr.ReviewedTaskStateRev != tx.ReviewedTaskStateRev ||
		tr.ReviewedTaskStateRev != result.TaskStateRev ||
		tr.WorkRevision != tx.WorkRevision ||
		tr.WorkRevision != result.WorkRevision ||
		tr.ImplementationSHA != tx.ImplementationSHA ||
		tr.ImplementationSHA != result.ImplementationSHA ||
		tr.ReviewAttempt != tx.ReviewAttempt ||
		tr.ReviewAttempt != result.AttemptID ||
		tr.IntegrationBase != tx.IntegrationBase ||
		tr.IntegrationRef != tx.IntegrationRef ||
		tr.StagingRef != tx.StagingRef ||
		tr.CloseAuthorityFP != tx.CloseAuthorityFP ||
		tr.WaveID != tx.WaveID ||
		tr.WaveMaterialFP != tx.WaveMaterialFP ||
		tr.WaveAuthorizationFP != tx.WaveAuthorizationFP ||
		tr.WaveAuthorityKind != tx.WaveAuthorityKind {
		return fmt.Errorf("completion receipt frozen transaction mismatch")
	}
	if tr.StagingRef != completionStagingRef(tr.ID) || !completionFrozenAuthorityComplete(tx, true) {
		return fmt.Errorf("completion receipt frozen authority is incomplete or invalid")
	}
	if receipt.Close.Schema != "tusker.completion-close-authority/v2" ||
		receipt.Close.TaskID != result.TaskID ||
		receipt.Close.TaskStateRev != result.TaskStateRev ||
		receipt.Close.Actor != result.Actor ||
		receipt.Close.ProofFingerprint != result.ProofFingerprint ||
		receipt.Close.GateFingerprint != result.GateFingerprint {
		return fmt.Errorf("completion receipt close projection does not bind the reviewed task")
	}
	projectionRaw, _ := json.Marshal(receipt.Close)
	projectionHash := sha256.Sum256(projectionRaw)
	if "sha256:"+hex.EncodeToString(projectionHash[:]) != tx.CloseAuthorityFP {
		return fmt.Errorf("completion receipt close projection fingerprint mismatch")
	}
	expectedID := completionTransactionID(tx.ProjectID, result, tx.IntegrationBase, completionFrozenAuthorityParts(tx)...)
	if tx.ID != expectedID {
		return fmt.Errorf("completion receipt transaction ID does not bind its result")
	}
	fact, ok := v7TaskCloseAuthorityFromAny(taskData["close_authority"])
	// RegisteredProject.ProjectID is runtime-local; the task fact is anchored
	// to the portable vault project identity recorded in the task tree.
	if !ok ||
		validateV7TaskCloseAuthorityFact(fact, stringField(taskData, "project"), result.TaskID, result.Actor, taskBody) != nil ||
		fact.TransactionID != tx.ID ||
		fact.ReceiptID != receipt.ReceiptID ||
		fact.ReviewResultRevision != result.ResultRevision ||
		fact.ReviewedTaskStateRev != tx.ReviewedTaskStateRev ||
		fact.CloseAuthorityFingerprint != tx.CloseAuthorityFP ||
		fact.ClosedAt != completionResultTimestamp(result) ||
		!v7StateRevMatches(taskData, taskBody, stringField(taskData, "state_rev")) ||
		!completionCanonicalTaskMatches(Note{Data: taskData, Body: taskBody}, result, tx) {
		return fmt.Errorf("completion receipt task fact does not match historical task")
	}
	return nil
}

func completionTransactionFromReceipt(receipt completionReceipt) *completionTransaction {
	tr := receipt.Transaction
	return &completionTransaction{
		Schema:                 completionTransactionSchema,
		ID:                     tr.ID,
		ProjectID:              tr.ProjectID,
		TaskID:                 tr.TaskID,
		WorkRevision:           tr.WorkRevision,
		ImplementationSHA:      tr.ImplementationSHA,
		ReviewAttempt:          tr.ReviewAttempt,
		ResultRevision:         tr.ResultRevision,
		ReviewedTaskStateRev:   tr.ReviewedTaskStateRev,
		WaveID:                 tr.WaveID,
		WaveAuthorityKind:      tr.WaveAuthorityKind,
		WaveAuthorizationFP:    tr.WaveAuthorizationFP,
		WaveMaterialFP:         tr.WaveMaterialFP,
		CloseAuthorityFP:       tr.CloseAuthorityFP,
		IntegrationBase:        tr.IntegrationBase,
		IntegrationRef:         tr.IntegrationRef,
		StagingRef:             tr.StagingRef,
		CompletionAuthorityID:  receipt.Authority.ID,
		CompletionAuthoritySig: append([]byte(nil), receipt.Authority.Signature...),
	}
}
