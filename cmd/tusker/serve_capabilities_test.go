package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestServeCapabilitiesRegistryIsMachineReadable(t *testing.T) {
	server := newServeServer("/tmp/vault", "/tmp/repo", "127.0.0.1:7420", nil, nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/capabilities", nil)
	server.handleCapabilities(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Schema       string            `json:"schema"`
		Capabilities []serveCapability `json:"capabilities"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Schema != "tusker.serve-capabilities/v1" || len(payload.Capabilities) == 0 {
		t.Fatalf("unexpected registry: %#v", payload)
	}
	for _, item := range payload.Capabilities {
		if item.ID == "profiles" && item.Class != "unavailable" {
			t.Fatalf("profiles must remain unavailable: %#v", item)
		}
	}
}

func TestServeCapabilitiesRejectsMutation(t *testing.T) {
	server := newServeServer("/tmp/vault", "/tmp/repo", "127.0.0.1:7420", nil, nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/capabilities", nil)
	server.handleCapabilities(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}
