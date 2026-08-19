package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLiveACPNegotiationReceiptUsesBoundedEventFacts(t *testing.T) {
	raw := `{"attempt_id":"attempt-1","runner":"codex_acp","kind":"acp_protocol_negotiated","payload":{"agent_name":"@agentclientprotocol/codex-acp","agent_version":"1.1.14","load_session":true,"resume_session":false}}
{"attempt_id":"attempt-1","runner":"codex_acp","kind":"acp_codex_config_applied","payload":{"steps":1,"config_receipt":"sha256:test"}}
{"attempt_id":"attempt-1","runner":"codex_acp","kind":"acp_turn_terminal","payload":{"transport_outcome":"completed","delivery_phase":"terminal_received"}}
`
	got, err := liveACPNegotiationReceipt(raw, "attempt-1", RunnerCodexACP, "1.1.14", "sha256:test")
	if err != nil {
		t.Fatal(err)
	}
	if got["protocol"] != "acp/v1" || got["agent_name"] != "@agentclientprotocol/codex-acp" || got["agent_version"] != "1.1.14" || got["load_session"] != true || got["resume_session"] != false {
		t.Fatalf("negotiation receipt = %#v", got)
	}
}

func TestRedactedLiveACPPathPreservesVaultRelativeOnly(t *testing.T) {
	root := filepath.Join("/private", "tmp", "vault")
	if got := redactedLiveACPPath(root, filepath.Join(root, "scratch", "event.log")); got != filepath.Join("scratch", "event.log") {
		t.Fatalf("vault-relative path = %q", got)
	}
	if got := redactedLiveACPPath(root, "/Users/sarav/secret.log"); got != "<redacted>/secret.log" {
		t.Fatalf("external path = %q", got)
	}
}

func TestLiveACPNegotiationReceiptRejectsMisbindingAndUntypedFacts(t *testing.T) {
	raw := `{"attempt_id":"other-attempt","runner":"codex_acp","kind":"acp_protocol_negotiated","payload":{"agent_name":"codex","agent_version":"1.1.14","load_session":"true","resume_session":false}}
{"attempt_id":"other-attempt","runner":"codex_acp","kind":"acp_codex_config_applied","payload":{}}
{"attempt_id":"other-attempt","runner":"codex_acp","kind":"acp_turn_terminal","payload":{}}
`
	if _, err := liveACPNegotiationReceipt(raw, "attempt-1", RunnerCodexACP, "1.1.14", "sha256:test"); err == nil {
		t.Fatal("event facts from another attempt were accepted")
	}
	raw = `{"attempt_id":"attempt-1","runner":"codex_acp","kind":"acp_protocol_negotiated","payload":{"agent_name":"codex","agent_version":"1.1.14","load_session":"true","resume_session":false}}
{"attempt_id":"attempt-1","runner":"codex_acp","kind":"acp_codex_config_applied","payload":{"steps":1,"config_receipt":"sha256:test"}}
{"attempt_id":"attempt-1","runner":"codex_acp","kind":"acp_turn_terminal","payload":{"transport_outcome":"completed","delivery_phase":"terminal_received"}}
`
	if _, err := liveACPNegotiationReceipt(raw, "attempt-1", RunnerCodexACP, "1.1.14", "sha256:test"); err == nil {
		t.Fatal("untyped negotiation capability was accepted")
	}
}

func TestLiveACPWrapperFingerprintRejectsSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "wrapper")
	if err := os.WriteFile(target, []byte("binary"), 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "wrapper-link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, err := liveACPWrapperFingerprint(link); err == nil {
		t.Fatal("symlinked wrapper was accepted")
	}
}

func TestWriteLiveACPReceiptIsExclusiveAndRejectsSymlink(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "receipt.json")
	if err := writeLiveACPReceipt(path, []byte("first\n")); err != nil {
		t.Fatal(err)
	}
	if err := writeLiveACPReceipt(path, []byte("second\n")); err == nil {
		t.Fatal("existing receipt was overwritten")
	}
	if raw, err := os.ReadFile(path); err != nil || string(raw) != "first\n" {
		t.Fatalf("receipt changed after rejected overwrite: %q err=%v", raw, err)
	}
	target := filepath.Join(dir, "target.json")
	if err := os.WriteFile(target, []byte("target\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.json")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := writeLiveACPReceipt(link, []byte("attacker\n")); err == nil {
		t.Fatal("symlink receipt path was accepted")
	}
	if raw, err := os.ReadFile(target); err != nil || string(raw) != "target\n" {
		t.Fatalf("symlink target changed: %q err=%v", raw, err)
	}
}
