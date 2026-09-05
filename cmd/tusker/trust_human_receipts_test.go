package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTrustHumanReceipts(t *testing.T) {
	t.Run("canonical payload matches native producer", func(t *testing.T) {
		receipt := humanControlReceipt{Schema: humanControlReceiptV1, ChallengeID: "challenge-1", ProjectID: "project-1", GateID: "FLW-G-0010", Actor: "human:operator", KeyID: "sha256:ignored", MaterialRevision: "sha256:material", ActionDigest: "sha256:action", Answer: "accept", Nonce: "nonce-1", IssuedAt: "2026-09-05T00:00:00.000Z", ExpiresAt: "2030-01-02T03:04:05.123456789Z"}
		want := "tusker.human-receipt/v1\nchallenge-1\nproject-1\nFLW-G-0010\nhuman:operator\nsha256:material\nsha256:action\naccept\nnonce-1\n2026-09-05T00:00:00.000Z\n2030-01-02T03:04:05.123456789Z"
		if got := string(humanControlReceiptPayload(receipt)); got != want {
			t.Fatalf("native canonical payload mismatch: got %q want %q", got, want)
		}
	})

	newReceiptServer := func(t *testing.T) (*serveServer, *ecdsa.PrivateKey) {
		t.Helper()
		server := newServeEmptyNeedsFixture(t)
		writeServeTask(t, server.vaultPath, serveTaskSeed{ID: "APP-T-0042", Epic: "APP", Title: "Native approval", Status: "backlog", Readiness: "waiting_on_human", Risk: "medium", Priority: "p1"})
		writeServeHumanVerificationGate(t, server.vaultPath, "APP-G-0042", "APP-T-0042", "A1")
		gatePath := filepath.Join(server.vaultPath, "work", "gates", "APP-G-0042.md")
		data, body, err := parseFrontmatterMustRead(gatePath)
		if err != nil {
			t.Fatal(err)
		}
		data["state_rev"] = v7StateRev(data, body)
		content, err := serializeDocument(data, body, v7FrontmatterOrder["gate"])
		if err != nil {
			t.Fatal(err)
		}
		if err := writeText(gatePath, content); err != nil {
			t.Fatal(err)
		}
		private, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		der, err := x509.MarshalPKIXPublicKey(&private.PublicKey)
		if err != nil {
			t.Fatal(err)
		}
		server.humanControlPublicKey = der
		server.now = func() time.Time { return time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC) }
		return server, private
	}
	issue := func(t *testing.T, server *serveServer, private *ecdsa.PrivateKey) (RegisteredProject, humanControlReceipt, string) {
		t.Helper()
		project, err := server.projectForSnapshot("app")
		if err != nil {
			t.Fatal(err)
		}
		challenge, err := server.issueHumanControlChallenge(project, "APP-G-0042", "satisfy")
		if err != nil {
			t.Fatal(err)
		}
		receipt := humanControlReceipt{Schema: humanControlReceiptV1, ChallengeID: challenge.ID, ProjectID: challenge.ProjectID, GateID: challenge.GateID, Actor: challenge.Actor, KeyID: challenge.KeyID, MaterialRevision: challenge.MaterialRevision, ActionDigest: challenge.ActionDigest, Answer: "accept", Nonce: challenge.Nonce, IssuedAt: server.now().Format(time.RFC3339Nano), ExpiresAt: challenge.ExpiresAt}
		signature, err := ecdsa.SignASN1(rand.Reader, private, humanControlReceiptPayload(receipt))
		if err != nil {
			t.Fatal(err)
		}
		return project, receipt, base64.RawStdEncoding.EncodeToString(signature)
	}

	t.Run("native receipt binds and consumes one current human action", func(t *testing.T) {
		server, private := newReceiptServer(t)
		project, receipt, signature := issue(t, server, private)
		verified, err := server.verifyHumanControlReceipt(project, receipt, signature, "satisfy")
		if err != nil {
			t.Fatal(err)
		}
		if err := gateV7TransitionWithHumanReceipt(Args{"vault": project.VaultRoot, "repo": project.RepoRoot, "id": receipt.GateID, "by": receipt.Actor, "quiet": "true"}, "satisfied", &verified); err != nil {
			t.Fatal(err)
		}
		note, err := resolveV7Note(project.VaultRoot, receipt.GateID, "gate")
		if err != nil || stringField(note.Data, "status") != "satisfied" || stringField(note.Data, "satisfied_by") != "human:test-operator" {
			t.Fatalf("native receipt did not satisfy gate: %#v %v", note.Data, err)
		}
	})

	t.Run("native HTTP flow presents authoritative material and derives submit action", func(t *testing.T) {
		server, private := newReceiptServer(t)
		var challenge humanControlChallenge
		servePost(t, server, "/api/human-receipts/challenge", `{"projectId":"app","gateId":"APP-G-0042","action":"satisfy"}`, &challenge)
		if challenge.GateTitle == "" || challenge.ActionText == "" || challenge.VerificationText == "" {
			t.Fatalf("native challenge omitted authoritative display material: %#v", challenge)
		}
		receipt := humanControlReceipt{Schema: humanControlReceiptV1, ChallengeID: challenge.ID, ProjectID: challenge.ProjectID, GateID: challenge.GateID, Actor: challenge.Actor, KeyID: challenge.KeyID, MaterialRevision: challenge.MaterialRevision, ActionDigest: challenge.ActionDigest, Answer: "accept", Nonce: challenge.Nonce, IssuedAt: server.now().Format(time.RFC3339Nano), ExpiresAt: challenge.ExpiresAt}
		signature, err := ecdsa.SignASN1(rand.Reader, private, humanControlReceiptPayload(receipt))
		if err != nil {
			t.Fatal(err)
		}
		body, err := json.Marshal(map[string]any{"projectId": "app", "receipt": receipt, "signature": base64.RawStdEncoding.EncodeToString(signature)})
		if err != nil {
			t.Fatal(err)
		}
		var result serveActionResult
		servePost(t, server, "/api/human-receipts/submit", string(body), &result)
		if !result.OK || result.GateID != "APP-G-0042" {
			t.Fatalf("native submit did not settle the issued action: %#v", result)
		}
	})

	t.Run("forged stale cross-gate and replay receipts fail", func(t *testing.T) {
		server, private := newReceiptServer(t)
		project, receipt, signature := issue(t, server, private)
		forgedSuffix := "A"
		if signature[len(signature)-1:] == forgedSuffix {
			forgedSuffix = "B"
		}
		forged := signature[:len(signature)-1] + forgedSuffix
		if _, err := server.verifyHumanControlReceipt(project, receipt, forged, "satisfy"); err == nil {
			t.Fatal("forged signature accepted")
		}
		crossGate := receipt
		crossGate.GateID = "APP-G-9999"
		if _, err := server.verifyHumanControlReceipt(project, crossGate, signature, "satisfy"); err == nil {
			t.Fatal("cross-gate receipt accepted")
		}
		verified, err := server.verifyHumanControlReceipt(project, receipt, signature, "satisfy")
		if err != nil || verified.GateID == "" {
			t.Fatalf("valid receipt refused: %#v %v", verified, err)
		}
		if _, err := server.verifyHumanControlReceipt(project, receipt, signature, "satisfy"); err == nil {
			t.Fatal("replayed receipt accepted")
		}
		server.now = func() time.Time { return time.Date(2026, 9, 5, 12, 6, 0, 0, time.UTC) }
		_, staleReceipt, staleSignature := issue(t, server, private)
		server.now = func() time.Time { return time.Date(2026, 9, 5, 12, 12, 0, 0, time.UTC) }
		if _, err := server.verifyHumanControlReceipt(project, staleReceipt, staleSignature, "satisfy"); err == nil {
			t.Fatal("expired receipt accepted")
		}
	})

	t.Run("changed material revokes an unused challenge", func(t *testing.T) {
		server, private := newReceiptServer(t)
		project, receipt, signature := issue(t, server, private)
		path := filepath.Join(project.VaultRoot, "work", "gates", "APP-G-0042.md")
		data, body, err := parseFrontmatterMustRead(path)
		if err != nil {
			t.Fatal(err)
		}
		data["action"] = "Exercise the changed panel."
		data["state_rev"] = v7StateRev(data, body)
		content, err := serializeDocument(data, body, v7FrontmatterOrder["gate"])
		if err != nil {
			t.Fatal(err)
		}
		if err := writeText(path, content); err != nil {
			t.Fatal(err)
		}
		if _, err := server.verifyHumanControlReceipt(project, receipt, signature, "satisfy"); err == nil || errorToIssue(err).Code != humanControlReceiptInvalidCode {
			t.Fatalf("changed material receipt accepted: %v", err)
		}
		if _, err := server.verifyHumanControlReceipt(project, receipt, signature, "satisfy"); err == nil || errorToIssue(err).Code != humanControlReceiptReplayCode {
			t.Fatalf("revoked challenge remained usable: %v", err)
		}
	})

	t.Run("body-only material edit invalidates its stale revision", func(t *testing.T) {
		server, private := newReceiptServer(t)
		project, err := server.projectForSnapshot("app")
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(project.VaultRoot, "work", "gates", "APP-G-0042.md")
		data, body, err := parseFrontmatterMustRead(path)
		if err != nil {
			t.Fatal(err)
		}
		delete(data, "action")
		delete(data, "verification")
		data["state_rev"] = v7StateRev(data, body)
		content, err := serializeDocument(data, body, v7FrontmatterOrder["gate"])
		if err != nil {
			t.Fatal(err)
		}
		if err := writeText(path, content); err != nil {
			t.Fatal(err)
		}
		project, receipt, signature := issue(t, server, private)
		raw, err := readText(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := writeText(path, strings.Replace(raw, "Exercise the panel.", "Exercise the changed panel.", 1)); err != nil {
			t.Fatal(err)
		}
		if _, err := server.verifyHumanControlReceipt(project, receipt, signature, "satisfy"); err == nil || errorToIssue(err).Code != humanControlReceiptInvalidCode {
			t.Fatalf("body-only material edit accepted stale receipt: %v", err)
		}
	})

	t.Run("arbitrary human actor cannot satisfy a human gate", func(t *testing.T) {
		server, _ := newReceiptServer(t)
		err := gateV7Transition(Args{"vault": server.vaultPath, "id": "APP-G-0042", "by": "human:forged", "evidence": "forged", "quiet": "true"}, "satisfied")
		if err == nil || errorToIssue(err).Code != humanControlReceiptRequiredCode {
			t.Fatalf("untrusted human actor satisfied gate: %v", err)
		}
		path := filepath.Join(server.vaultPath, "work", "gates", "APP-G-0042.md")
		note, err := resolveV7Note(server.vaultPath, "APP-G-0042", "gate")
		if err != nil || stringField(note.Data, "status") != "open" || !fileExists(path) {
			t.Fatalf("forged actor mutated gate: %#v %v", note.Data, err)
		}
	})
}
