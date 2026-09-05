package main

import (
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	humanControlReceiptV1           = "tusker.human-receipt/v1"
	humanControlReceiptRequiredCode = "HUMAN_CONTROL_RECEIPT_REQUIRED"
	humanControlReceiptInvalidCode  = "HUMAN_CONTROL_RECEIPT_INVALID"
	humanControlReceiptExpiredCode  = "HUMAN_CONTROL_RECEIPT_EXPIRED"
	humanControlReceiptReplayCode   = "HUMAN_CONTROL_RECEIPT_REPLAYED"
	humanControlReceiptLifetime     = 5 * time.Minute
)

type humanControlReceipt struct {
	Schema           string `json:"schema"`
	ChallengeID      string `json:"challenge_id"`
	ProjectID        string `json:"project_id"`
	GateID           string `json:"gate_id"`
	Actor            string `json:"actor"`
	KeyID            string `json:"key_id"`
	MaterialRevision string `json:"material_revision"`
	ActionDigest     string `json:"action_digest"`
	Answer           string `json:"answer"`
	Nonce            string `json:"nonce"`
	IssuedAt         string `json:"issued_at"`
	ExpiresAt        string `json:"expires_at"`
}

type humanControlChallenge struct {
	ID               string `json:"id"`
	ProjectID        string `json:"project_id"`
	GateID           string `json:"gate_id"`
	Actor            string `json:"actor"`
	KeyID            string `json:"key_id"`
	MaterialRevision string `json:"material_revision"`
	ActionDigest     string `json:"action_digest"`
	Action           string `json:"action"`
	Nonce            string `json:"nonce"`
	IssuedAt         string `json:"issued_at"`
	ExpiresAt        string `json:"expires_at"`
	GateTitle        string `json:"gate_title"`
	ActionText       string `json:"action_text"`
	VerificationText string `json:"verification_text"`
	ConsumedAt       string `json:"-"`
	RevokedAt        string `json:"-"`
}

type verifiedHumanControlReceipt struct {
	GateID           string
	Actor            string
	MaterialRevision string
	ActionDigest     string
	Action           string
	Receipt          humanControlReceipt
	Signature        string
}

func humanControlReceiptPayload(receipt humanControlReceipt) []byte {
	return []byte(strings.Join([]string{
		receipt.Schema,
		receipt.ChallengeID,
		receipt.ProjectID,
		receipt.GateID,
		receipt.Actor,
		receipt.MaterialRevision,
		receipt.ActionDigest,
		receipt.Answer,
		receipt.Nonce,
		receipt.IssuedAt,
		receipt.ExpiresAt,
	}, "\n"))
}

func humanControlKeyID(der []byte) string {
	digest := sha256.Sum256(der)
	return "sha256:" + hex.EncodeToString(digest[:])
}

func parseHumanControlPublicKey(der []byte) (*ecdsa.PublicKey, error) {
	key, err := x509.ParsePKIXPublicKey(der)
	if err != nil {
		return nil, err
	}
	public, ok := key.(*ecdsa.PublicKey)
	if !ok || public.Curve.Params().Name != "P-256" {
		return nil, fmt.Errorf("human receipt key must be P-256")
	}
	return public, nil
}

func configuredHumanControlPublicKey() []byte {
	raw := strings.TrimSpace(os.Getenv("TUSKER_HUMAN_RECEIPT_PUBLIC_KEY"))
	if raw == "" {
		return nil
	}
	key, err := base64.RawStdEncoding.DecodeString(raw)
	if err != nil {
		return nil
	}
	if _, err := parseHumanControlPublicKey(key); err != nil {
		return nil
	}
	return key
}

func humanControlNonce() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func humanControlDisplayedMaterial(data map[string]any, body string) (title, actionText, verificationText string) {
	return stringField(data, "title"),
		firstNonEmpty(stringField(data, "action"), sectionContent(body, "## Action")),
		firstNonEmpty(stringField(data, "verification"), sectionContent(body, "## Verification"))
}

func humanControlActionDigest(data map[string]any, body, action string) string {
	title, actionText, verificationText := humanControlDisplayedMaterial(data, body)
	canonical := strings.Join([]string{
		strings.ToLower(strings.TrimSpace(action)),
		stringField(data, "id"),
		title,
		actionText,
		verificationText,
	}, "\n")
	digest := sha256.Sum256([]byte(canonical))
	return "sha256:" + hex.EncodeToString(digest[:])
}

func v7HumanControlAction(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "satisfied":
		return "satisfy"
	case "waived":
		return "waive"
	case "obsolete":
		return "obsolete"
	default:
		return ""
	}
}

func v7HumanControlRequired(data map[string]any, status string) bool {
	if v7HumanControlAction(status) == "" {
		return false
	}
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(stringField(data, "owner"))), "human:")
}

func (s *serveServer) humanControlKey() (*ecdsa.PublicKey, string, error) {
	if len(s.humanControlPublicKey) == 0 {
		return nil, "", tuskerError(humanControlReceiptUnavailableCode, "human receipt control is unavailable: TuskerBar has not supplied a native signing key")
	}
	public, err := parseHumanControlPublicKey(s.humanControlPublicKey)
	if err != nil {
		return nil, "", tuskerError(humanControlReceiptUnavailableCode, "human receipt control is unavailable: configured native signing key is invalid")
	}
	return public, humanControlKeyID(s.humanControlPublicKey), nil
}

func (s *serveServer) issueHumanControlChallenge(project RegisteredProject, gateID, action string) (humanControlChallenge, error) {
	_, keyID, err := s.humanControlKey()
	if err != nil {
		return humanControlChallenge{}, err
	}
	actor, err := s.serveOperatorActor(serveActionBody{}, "human receipt challenge")
	if err != nil {
		return humanControlChallenge{}, err
	}
	gate, err := resolveV7Note(project.VaultRoot, strings.ToUpper(strings.TrimSpace(gateID)), "gate")
	if err != nil {
		return humanControlChallenge{}, err
	}
	data, body, err := parseFrontmatterMustRead(gate.AbsolutePath)
	if err != nil {
		return humanControlChallenge{}, err
	}
	if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(stringField(data, "owner"))), "human:") ||
		strings.TrimSpace(stringField(data, "status")) != "open" ||
		!containsString([]string{"satisfy", "waive", "obsolete"}, strings.ToLower(strings.TrimSpace(action))) {
		return humanControlChallenge{}, tuskerError(errorInvalidTransition, "human receipt challenge requires an open human-owned gate")
	}
	if rev := strings.TrimSpace(stringField(data, "state_rev")); rev == "" || !v7StateRevMatches(data, body, rev) {
		return humanControlChallenge{}, tuskerError(humanControlReceiptInvalidCode, "human receipt challenge requires a current gate material revision")
	}
	title, actionText, verificationText := humanControlDisplayedMaterial(data, body)
	nonce, err := humanControlNonce()
	if err != nil {
		return humanControlChallenge{}, err
	}
	now := s.now().UTC()
	challenge := humanControlChallenge{
		ID:               "human-receipt-" + strings.ToLower(newRecordID()),
		ProjectID:        project.ProjectID,
		GateID:           stringField(data, "id"),
		Actor:            actor,
		KeyID:            keyID,
		MaterialRevision: stringField(data, "state_rev"),
		ActionDigest:     humanControlActionDigest(data, body, action),
		Action:           strings.ToLower(strings.TrimSpace(action)),
		Nonce:            nonce,
		IssuedAt:         now.Format(time.RFC3339Nano),
		ExpiresAt:        now.Add(humanControlReceiptLifetime).Format(time.RFC3339Nano),
		GateTitle:        title,
		ActionText:       actionText,
		VerificationText: verificationText,
	}
	if err := s.store.createHumanControlChallenge(challenge); err != nil {
		return humanControlChallenge{}, err
	}
	return challenge, nil
}

func (s *serveServer) verifyHumanControlReceipt(project RegisteredProject, receipt humanControlReceipt, signature string, action string) (verifiedHumanControlReceipt, error) {
	public, keyID, err := s.humanControlKey()
	if err != nil {
		return verifiedHumanControlReceipt{}, err
	}
	challenge, err := s.store.humanControlChallenge(receipt.ChallengeID)
	if err != nil {
		return verifiedHumanControlReceipt{}, err
	}
	if challenge == nil || challenge.ConsumedAt != "" || challenge.RevokedAt != "" {
		return verifiedHumanControlReceipt{}, tuskerError(humanControlReceiptReplayCode, "human receipt challenge is unavailable or already consumed")
	}
	now := s.now().UTC()
	expiresAt, err := time.Parse(time.RFC3339Nano, challenge.ExpiresAt)
	if err != nil || !now.Before(expiresAt) {
		return verifiedHumanControlReceipt{}, tuskerError(humanControlReceiptExpiredCode, "human receipt challenge expired; request a new native confirmation")
	}
	if receipt.Schema != humanControlReceiptV1 || receipt.Answer != "accept" || receipt.KeyID != keyID || receipt.ProjectID != project.ProjectID ||
		challenge.ProjectID != project.ProjectID || receipt.GateID != challenge.GateID || receipt.Actor != challenge.Actor || receipt.MaterialRevision != challenge.MaterialRevision ||
		receipt.ActionDigest != challenge.ActionDigest || receipt.Nonce != challenge.Nonce || receipt.ExpiresAt != challenge.ExpiresAt || action != challenge.Action {
		return verifiedHumanControlReceipt{}, tuskerError(humanControlReceiptInvalidCode, "human receipt does not match its issued gate challenge")
	}
	issuedAt, err := time.Parse(time.RFC3339Nano, receipt.IssuedAt)
	if err != nil || issuedAt.After(now.Add(time.Minute)) || issuedAt.Before(now.Add(-humanControlReceiptLifetime)) {
		return verifiedHumanControlReceipt{}, tuskerError(humanControlReceiptInvalidCode, "human receipt issued_at is outside the accepted confirmation window")
	}
	signatureBytes, err := base64.RawStdEncoding.DecodeString(strings.TrimSpace(signature))
	if err != nil || !ecdsa.VerifyASN1(public, humanControlReceiptPayload(receipt), signatureBytes) {
		return verifiedHumanControlReceipt{}, tuskerError(humanControlReceiptInvalidCode, "human receipt signature did not verify against the configured native key")
	}
	gate, err := resolveV7Note(project.VaultRoot, challenge.GateID, "gate")
	if err != nil {
		return verifiedHumanControlReceipt{}, err
	}
	data, body, err := parseFrontmatterMustRead(gate.AbsolutePath)
	if err != nil {
		return verifiedHumanControlReceipt{}, err
	}
	if !v7HumanControlRequired(data, "satisfied") || stringField(data, "status") != "open" ||
		stringField(data, "state_rev") == "" || !v7StateRevMatches(data, body, stringField(data, "state_rev")) ||
		stringField(data, "state_rev") != challenge.MaterialRevision ||
		humanControlActionDigest(data, body, challenge.Action) != challenge.ActionDigest {
		_ = s.store.revokeHumanControlChallenge(challenge.ID, now)
		return verifiedHumanControlReceipt{}, tuskerError(humanControlReceiptInvalidCode, "human receipt challenge is stale because its gate material changed")
	}
	if err := s.store.consumeHumanControlChallenge(challenge.ID, now); err != nil {
		return verifiedHumanControlReceipt{}, err
	}
	return verifiedHumanControlReceipt{GateID: receipt.GateID, Actor: receipt.Actor, MaterialRevision: receipt.MaterialRevision, ActionDigest: receipt.ActionDigest, Action: action, Receipt: receipt, Signature: signature}, nil
}

func (s *serveServer) handleHumanControlChallenge(w http.ResponseWriter, body serveActionBody) {
	_, project, err := serveBaseArgsForBody(s, body)
	if err != nil {
		serveJSON(w, http.StatusOK, serveCommandResult("human receipt challenge", "", err))
		return
	}
	challenge, err := s.issueHumanControlChallenge(project, body.string("gateId", "gate_id", "gate"), body.string("action"))
	if err != nil {
		serveJSON(w, http.StatusOK, serveCommandResult("human receipt challenge", "", err))
		return
	}
	serveJSON(w, http.StatusOK, challenge)
}

func (s *serveServer) handleHumanControlReceiptSubmit(w http.ResponseWriter, body serveActionBody) {
	rawReceipt, ok := body["receipt"]
	if !ok {
		serveJSON(w, http.StatusOK, serveCommandResult("human receipt submit", "", tuskerError(errorMissingArg, "human receipt submit requires receipt")))
		return
	}
	raw, err := json.Marshal(rawReceipt)
	if err != nil {
		serveJSON(w, http.StatusOK, serveCommandResult("human receipt submit", "", tuskerError(humanControlReceiptInvalidCode, "human receipt is not a JSON object")))
		return
	}
	var receipt humanControlReceipt
	if err := json.Unmarshal(raw, &receipt); err != nil {
		serveJSON(w, http.StatusOK, serveCommandResult("human receipt submit", "", tuskerError(humanControlReceiptInvalidCode, "human receipt is malformed")))
		return
	}
	if body.string("projectId", "project_id", "project") == "" {
		body["projectId"] = receipt.ProjectID
	}
	_, project, err := serveBaseArgsForBody(s, body)
	if err != nil {
		serveJSON(w, http.StatusOK, serveCommandResult("human receipt submit", "", err))
		return
	}
	challenge, err := s.store.humanControlChallenge(receipt.ChallengeID)
	if err != nil {
		serveJSON(w, http.StatusOK, serveCommandResult("human receipt submit", "", err))
		return
	}
	if challenge == nil || !containsString([]string{"satisfy", "waive", "obsolete"}, challenge.Action) {
		serveJSON(w, http.StatusOK, serveCommandResult("human receipt submit", "", tuskerError(humanControlReceiptInvalidCode, "human receipt does not name a supported gate action")))
		return
	}
	action := challenge.Action
	verified, err := s.verifyHumanControlReceipt(project, receipt, body.string("signature"), action)
	if err != nil {
		serveJSON(w, http.StatusOK, serveCommandResult("human receipt submit", "", err))
		return
	}
	args := Args{"vault": project.VaultRoot, "repo": project.RepoRoot, "id": verified.GateID, "by": verified.Actor, "quiet": "true"}
	if err := gateV7TransitionWithHumanReceipt(args, map[string]string{"satisfy": "satisfied", "waive": "waived", "obsolete": "obsolete"}[action], &verified); err != nil {
		serveJSON(w, http.StatusOK, serveCommandResult("human receipt submit", "", err))
		return
	}
	s.invalidateProjectSnapshot(project.ProjectID)
	result := serveActionResult{OK: true, Reason: "native human receipt accepted", GateID: verified.GateID, ProjectID: project.ProjectID}
	if gate := s.findGateDetailForProject(verified.GateID, project.ProjectID); gate != nil {
		result.Gate = gate
		result.TaskID = firstNonEmpty(gate.Blocks...)
		s.decorateTaskActionResultForProject(&result, result.TaskID, project.ProjectID)
	}
	serveJSON(w, http.StatusOK, result)
}

func humanControlReceiptEvidence(receipt verifiedHumanControlReceipt) string {
	payload := map[string]any{"receipt": receipt.Receipt, "signature": receipt.Signature}
	raw, _ := json.Marshal(payload)
	return string(raw)
}

func (s *RuntimeStore) createHumanControlChallenge(challenge humanControlChallenge) error {
	_, err := s.exec(`INSERT INTO human_control_challenges(challenge_id, project_id, gate_id, actor, key_id, material_revision, action_digest, action, nonce, issued_at, expires_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		challenge.ID, challenge.ProjectID, challenge.GateID, challenge.Actor, challenge.KeyID, challenge.MaterialRevision, challenge.ActionDigest, challenge.Action, challenge.Nonce, challenge.IssuedAt, challenge.ExpiresAt)
	return err
}

func (s *RuntimeStore) humanControlChallenge(id string) (*humanControlChallenge, error) {
	var challenge humanControlChallenge
	err := s.queryRowScan(`SELECT challenge_id, project_id, gate_id, actor, key_id, material_revision, action_digest, action, nonce, issued_at, expires_at, consumed_at, revoked_at FROM human_control_challenges WHERE challenge_id=?`, []any{id},
		&challenge.ID, &challenge.ProjectID, &challenge.GateID, &challenge.Actor, &challenge.KeyID, &challenge.MaterialRevision, &challenge.ActionDigest, &challenge.Action, &challenge.Nonce, &challenge.IssuedAt, &challenge.ExpiresAt, &challenge.ConsumedAt, &challenge.RevokedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &challenge, nil
}

func (s *RuntimeStore) consumeHumanControlChallenge(id string, now time.Time) error {
	result, err := s.exec(`UPDATE human_control_challenges SET consumed_at=? WHERE challenge_id=? AND consumed_at='' AND revoked_at='' AND expires_at>?`, now.UTC().Format(time.RFC3339Nano), id, now.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return tuskerError(humanControlReceiptReplayCode, "human receipt challenge was already consumed, revoked, or expired")
	}
	return nil
}

func (s *RuntimeStore) revokeHumanControlChallenge(id string, now time.Time) error {
	_, err := s.exec(`UPDATE human_control_challenges SET revoked_at=? WHERE challenge_id=? AND consumed_at='' AND revoked_at=''`, now.UTC().Format(time.RFC3339Nano), id)
	return err
}
