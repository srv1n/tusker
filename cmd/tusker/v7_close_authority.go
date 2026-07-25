package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const v7TaskCloseAuthoritySchema = "tusker.task-close-authority/v2"

// v7TaskCloseAuthority is the canonical, protected audit fact for a close
// whose policy authority was frozen before an external commit boundary. It is
// deliberately small: the transaction authenticates the full frozen
// projection, while this fact binds the closed tracker record and event back
// to that transaction without making later validation depend on mutable
// close-policy configuration.
type v7TaskCloseAuthority struct {
	Schema                    string `json:"schema"`
	Project                   string `json:"project"`
	TransactionID             string `json:"transaction_id"`
	ReceiptID                 string `json:"receipt_id"`
	TaskID                    string `json:"task_id"`
	ReviewResultRevision      string `json:"review_result_revision"`
	ReviewedTaskStateRev      string `json:"reviewed_task_state_rev"`
	CloseAuthorityFingerprint string `json:"close_authority_fingerprint"`
	Actor                     string `json:"actor"`
	ClosedAt                  string `json:"closed_at"`
	BindingFingerprint        string `json:"binding_fingerprint"`
}

type v7TaskCloseAuthorityBinding struct {
	Schema                    string `json:"schema"`
	Project                   string `json:"project"`
	TransactionID             string `json:"transaction_id"`
	ReceiptID                 string `json:"receipt_id"`
	TaskID                    string `json:"task_id"`
	ReviewResultRevision      string `json:"review_result_revision"`
	ReviewedTaskStateRev      string `json:"reviewed_task_state_rev"`
	CloseAuthorityFingerprint string `json:"close_authority_fingerprint"`
	Actor                     string `json:"actor"`
	ClosedAt                  string `json:"closed_at"`
}

func newCompletionTaskCloseAuthority(vaultPath string, result ReviewResult, transaction *completionTransaction) (v7TaskCloseAuthority, error) {
	if transaction == nil {
		return v7TaskCloseAuthority{}, completionFrozenAuthorityRepairError(nil, "close audit transaction is missing")
	}
	fact := v7TaskCloseAuthority{
		Schema: v7TaskCloseAuthoritySchema, Project: v7ProjectID(vaultPath),
		TransactionID: transaction.ID, ReceiptID: completionReceiptID(transaction.ID), TaskID: result.TaskID,
		ReviewResultRevision: result.ResultRevision, ReviewedTaskStateRev: transaction.ReviewedTaskStateRev,
		CloseAuthorityFingerprint: transaction.CloseAuthorityFP, Actor: result.Actor,
		ClosedAt: completionResultTimestamp(result),
	}
	binding, err := v7TaskCloseAuthorityBindingFingerprint(fact)
	if err != nil {
		return v7TaskCloseAuthority{}, err
	}
	fact.BindingFingerprint = binding
	if err := validateV7TaskCloseAuthorityFact(fact, fact.Project, result.TaskID, result.Actor, "[tusker-review-result:"+result.ResultRevision+"]"); err != nil {
		return v7TaskCloseAuthority{}, err
	}
	return fact, nil
}

func v7TaskCloseAuthorityBindingFingerprint(fact v7TaskCloseAuthority) (string, error) {
	raw, err := json.Marshal(v7TaskCloseAuthorityBinding{
		Schema: fact.Schema, Project: fact.Project, TransactionID: fact.TransactionID, ReceiptID: fact.ReceiptID,
		TaskID: fact.TaskID, ReviewResultRevision: fact.ReviewResultRevision,
		ReviewedTaskStateRev:      fact.ReviewedTaskStateRev,
		CloseAuthorityFingerprint: fact.CloseAuthorityFingerprint,
		Actor:                     fact.Actor, ClosedAt: fact.ClosedAt,
	})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func (fact v7TaskCloseAuthority) mapValue() map[string]any {
	return map[string]any{
		"schema": fact.Schema, "project": fact.Project,
		"transaction_id": fact.TransactionID, "receipt_id": fact.ReceiptID, "task_id": fact.TaskID,
		"review_result_revision":      fact.ReviewResultRevision,
		"reviewed_task_state_rev":     fact.ReviewedTaskStateRev,
		"close_authority_fingerprint": fact.CloseAuthorityFingerprint,
		"actor":                       fact.Actor, "closed_at": fact.ClosedAt,
		"binding_fingerprint": fact.BindingFingerprint,
	}
}

func v7TaskCloseAuthorityFromAny(value any) (v7TaskCloseAuthority, bool) {
	data := map[string]any{}
	switch typed := value.(type) {
	case map[string]any:
		data = typed
	case map[any]any:
		for key, current := range typed {
			data[toString(key)] = current
		}
	default:
		return v7TaskCloseAuthority{}, false
	}
	return v7TaskCloseAuthority{
		Schema: stringField(data, "schema"), Project: stringField(data, "project"),
		TransactionID: stringField(data, "transaction_id"), ReceiptID: stringField(data, "receipt_id"), TaskID: stringField(data, "task_id"),
		ReviewResultRevision:      stringField(data, "review_result_revision"),
		ReviewedTaskStateRev:      stringField(data, "reviewed_task_state_rev"),
		CloseAuthorityFingerprint: stringField(data, "close_authority_fingerprint"),
		Actor:                     stringField(data, "actor"), ClosedAt: stringField(data, "closed_at"),
		BindingFingerprint: stringField(data, "binding_fingerprint"),
	}, true
}

func validateV7TaskCloseAuthorityFact(fact v7TaskCloseAuthority, project, taskID, actor, markerText string) error {
	if fact.Schema != v7TaskCloseAuthoritySchema {
		return fmt.Errorf("close authority schema must be %s", v7TaskCloseAuthoritySchema)
	}
	for field, value := range map[string]string{
		"project": fact.Project, "transaction_id": fact.TransactionID, "receipt_id": fact.ReceiptID, "task_id": fact.TaskID,
		"review_result_revision":      fact.ReviewResultRevision,
		"reviewed_task_state_rev":     fact.ReviewedTaskStateRev,
		"close_authority_fingerprint": fact.CloseAuthorityFingerprint,
		"actor":                       fact.Actor, "closed_at": fact.ClosedAt, "binding_fingerprint": fact.BindingFingerprint,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("close authority %s is required", field)
		}
	}
	if !v7CloseAuthorityDigest(fact.TransactionID, "completion:") {
		return fmt.Errorf("close authority transaction_id is not a completion transaction")
	}
	if !v7CloseAuthorityDigest(fact.ReceiptID, "receipt:") {
		return fmt.Errorf("close authority receipt_id is invalid")
	}
	for field, value := range map[string]string{
		"review_result_revision":      fact.ReviewResultRevision,
		"reviewed_task_state_rev":     fact.ReviewedTaskStateRev,
		"close_authority_fingerprint": fact.CloseAuthorityFingerprint,
		"binding_fingerprint":         fact.BindingFingerprint,
	} {
		if !v7CloseAuthorityDigest(value, "sha256:") {
			return fmt.Errorf("close authority %s is invalid", field)
		}
	}
	if project != "" && fact.Project != project {
		return fmt.Errorf("close authority project does not match closed record")
	}
	if taskID != "" && fact.TaskID != taskID {
		return fmt.Errorf("close authority task does not match closed record")
	}
	if actor != "" && fact.Actor != actor {
		return fmt.Errorf("close authority actor does not match closed record")
	}
	if markerText != "" && !strings.Contains(markerText, "[tusker-review-result:"+fact.ReviewResultRevision+"]") {
		return fmt.Errorf("close authority review result is not present in the closed record")
	}
	if _, err := time.Parse(time.RFC3339, fact.ClosedAt); err != nil {
		return fmt.Errorf("close authority closed_at must be RFC3339")
	}
	expected, err := v7TaskCloseAuthorityBindingFingerprint(fact)
	if err != nil {
		return err
	}
	if fact.BindingFingerprint != expected {
		return fmt.Errorf("close authority binding fingerprint is invalid")
	}
	return nil
}

func v7CloseAuthorityDigest(value, prefix string) bool {
	if !strings.HasPrefix(value, prefix) || len(value) != len(prefix)+64 || strings.ToLower(value) != value {
		return false
	}
	decoded, err := hex.DecodeString(strings.TrimPrefix(value, prefix))
	return err == nil && len(decoded) == sha256.Size
}

func authenticatedV7TaskCloseAuthority(note Note, project string) (v7TaskCloseAuthority, bool, error) {
	value, present := note.Data["close_authority"]
	if !present {
		return v7TaskCloseAuthority{}, false, nil
	}
	fact, ok := v7TaskCloseAuthorityFromAny(value)
	if !ok {
		return v7TaskCloseAuthority{}, false, fmt.Errorf("close authority must be a mapping")
	}
	if err := validateV7TaskCloseAuthorityFact(
		fact, firstNonEmpty(project, stringField(note.Data, "project")),
		stringField(note.Data, "id"), stringField(note.Data, "accepted_by"), note.Body,
	); err != nil {
		return v7TaskCloseAuthority{}, false, err
	}
	if stringField(note.Data, "closed_at") != fact.ClosedAt || stringField(note.Data, "accepted_at") != fact.ClosedAt {
		return v7TaskCloseAuthority{}, false, fmt.Errorf("close authority timestamp does not match closed record")
	}
	return fact, true, nil
}

// emitV7TaskClosedEvent preserves the existing close event shape for normal
// closes. Frozen completion closes use a deterministic event identity and
// timestamp, making replay after the tracker write idempotent and ensuring the
// event carries the same authenticated fact as the task.
func emitV7TaskClosedEvent(vaultPath, taskID, actor, at, from, reason string, authority *v7TaskCloseAuthority) error {
	if authority == nil {
		return emitV7Event(vaultPath, taskID, "task", "closed", actor, map[string]any{"from": from, "reason": reason})
	}
	if err := validateV7TaskCloseAuthorityFact(*authority, v7ProjectID(vaultPath), taskID, actor, "[tusker-review-result:"+authority.ReviewResultRevision+"]"); err != nil {
		return err
	}
	parsedAt, err := time.Parse(time.RFC3339, at)
	if err != nil {
		return err
	}
	digest := strings.TrimPrefix(authority.BindingFingerprint, "sha256:")
	eventID := "close-" + digest[:32]
	event := map[string]any{
		"schema": "tusker.event/v1", "id": eventID,
		"project": v7ProjectID(vaultPath), "object": taskID, "object_kind": "task",
		"event_kind": "closed", "actor": actor, "at": parsedAt.UTC().Format(time.RFC3339),
		"payload": map[string]any{
			"from": from, "reason": reason, "close_authority": authority.mapValue(),
		},
	}
	name := fmt.Sprintf("%s--%s--%s.json", taskID, parsedAt.UTC().Format("20060102T150405Z"), eventID)
	path := filepath.Join(vaultPath, "events", parsedAt.UTC().Format("2006"), parsedAt.UTC().Format("01"), name)
	return writeDeterministicV7CloseEvent(path, event)
}

// writeDeterministicV7CloseEvent is intentionally not writeJSON.  Close-event
// identity is part of replay safety: a partially written final name must never
// become an immutable false conflict. We fsync a private file then link it into
// place (the link is no-clobber); an existing final path is accepted only when
// its canonical bytes are exactly ours.  A malformed short legacy/crash file is
// replaced atomically, which is safe because it was never a valid event.
func writeDeterministicV7CloseEvent(path string, event map[string]any) error {
	raw, err := json.MarshalIndent(event, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	if existing, readErr := os.ReadFile(path); readErr == nil {
		if bytes.Equal(existing, raw) {
			return nil
		}
		var parsed map[string]any
		if json.Unmarshal(existing, &parsed) == nil {
			return tuskerError("CAS_CONFLICT", "closed-event audit identity already exists with different content", withPath(path))
		}
		// A short/corrupt final name can only be recovered to canonical bytes;
		// atomic replacement prevents readers from observing another short file.
		if err := atomicReplaceV7Document(path, string(raw)); err != nil {
			return err
		}
		return nil
	} else if !os.IsNotExist(readErr) {
		return readErr
	}
	if err := ensureDir(filepath.Dir(path)); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer func() { _ = temp.Close(); _ = os.Remove(tempPath) }()
	if err := temp.Chmod(0o644); err != nil {
		return err
	}
	if n, err := temp.Write(raw); err != nil || n != len(raw) {
		if err != nil {
			return err
		}
		return fmt.Errorf("write deterministic close event: short write %d of %d", n, len(raw))
	}
	if err := temp.Sync(); err != nil {
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Link(tempPath, path); err != nil {
		if os.IsExist(err) {
			existing, readErr := os.ReadFile(path)
			if readErr == nil && bytes.Equal(existing, raw) {
				return nil
			}
			return tuskerError("CAS_CONFLICT", "closed-event audit identity already exists with different content", withPath(path))
		}
		return err
	}
	return syncV7DocumentDirectory(filepath.Dir(path))
}
