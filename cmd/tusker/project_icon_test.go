package main

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiscoverProjectIconUsesManifestIcon(t *testing.T) {
	root := t.TempDir()
	iconDir := filepath.Join(root, "extension", "src", "icons")
	if err := os.MkdirAll(iconDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `{"icons":[{"src":"icons/brain.png"}]}` + "\n"
	if err := os.WriteFile(filepath.Join(root, "extension", "src", "manifest.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	icon, err := os.Create(filepath.Join(iconDir, "brain.png"))
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(icon, solidTestIcon()); err != nil {
		icon.Close()
		t.Fatal(err)
	}
	if err := icon.Close(); err != nil {
		t.Fatal(err)
	}

	candidate, ok := discoverProjectIcon(root)
	if !ok {
		t.Fatal("expected manifest icon to be discovered")
	}
	wantPath, err := filepath.EvalSymlinks(filepath.Join(iconDir, "brain.png"))
	if err != nil {
		t.Fatal(err)
	}
	if candidate.path != wantPath {
		t.Fatalf("discovered %q, want manifest icon", candidate.path)
	}
	if candidate.mime != "image/png" {
		t.Fatalf("mime=%q, want image/png", candidate.mime)
	}
}

func TestServeProjectIconEndpointServesDetectedAsset(t *testing.T) {
	root := t.TempDir()
	vault := filepath.Join(root, ".tusker")
	if err := os.MkdirAll(vault, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "logo.svg"), []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1 1"><circle cx=".5" cy=".5" r=".5"/></svg>`), 0o644); err != nil {
		t.Fatal(err)
	}
	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	project := newRegisteredProject(root, vault)
	if err := store.UpsertProject(project); err != nil {
		t.Fatal(err)
	}
	server := newServeServer(vault, root, defaultServeAddr, store, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/projects/"+project.ProjectID+"/icon", nil)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/svg+xml" || !strings.Contains(rec.Body.String(), "<svg") {
		t.Fatalf("status=%d content-type=%q body=%q", rec.Code, rec.Header().Get("Content-Type"), rec.Body.String())
	}
}

func TestServeProjectIconUploadOverridesDiscoveryAndClears(t *testing.T) {
	t.Setenv("TUSKER_STATE_ROOT", t.TempDir())
	root := t.TempDir()
	vault := filepath.Join(root, ".tusker")
	if err := os.MkdirAll(vault, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "logo.svg"), []byte(`<svg xmlns="http://www.w3.org/2000/svg"><rect width="1" height="1"/></svg>`), 0o644); err != nil {
		t.Fatal(err)
	}
	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	project := newRegisteredProject(root, vault)
	if err := store.UpsertProject(project); err != nil {
		t.Fatal(err)
	}
	server := newServeServer(vault, root, defaultServeAddr, store, nil)

	var buf bytes.Buffer
	if err := png.Encode(&buf, solidTestIcon()); err != nil {
		t.Fatal(err)
	}
	body := `{"data":"` + base64.StdEncoding.EncodeToString(buf.Bytes()) + `","mime":"image/png"}`
	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1/api/projects/"+project.ProjectID+"/icon", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"ok":true`) {
		t.Fatalf("upload status=%d body=%q", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/projects/"+project.ProjectID+"/icon", nil)
	rec = httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("icon status=%d content-type=%q", rec.Code, rec.Header().Get("Content-Type"))
	}
	if got := rec.Header().Get("X-Tusker-Icon-Source"); got != "uploaded" {
		t.Fatalf("icon source=%q, want uploaded", got)
	}
	if !bytes.Contains(rec.Body.Bytes(), buf.Bytes()[:8]) {
		t.Fatal("served body is not the uploaded PNG")
	}

	req = httptest.NewRequest(http.MethodPost, "http://127.0.0.1/api/projects/"+project.ProjectID+"/icon", strings.NewReader(`{"clear":true}`))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"ok":true`) {
		t.Fatalf("clear status=%d body=%q", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/projects/"+project.ProjectID+"/icon", nil)
	rec = httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Header().Get("X-Tusker-Icon-Source") != "discovered" || !strings.Contains(rec.Body.String(), "<svg") {
		t.Fatalf("after clear status=%d source=%q body=%q", rec.Code, rec.Header().Get("X-Tusker-Icon-Source"), rec.Body.String())
	}
}

func solidTestIcon() image.Image {
	icon := image.NewRGBA(image.Rect(0, 0, 32, 32))
	for y := 0; y < 32; y++ {
		for x := 0; x < 32; x++ {
			icon.Set(x, y, color.RGBA{R: 0x46, G: 0x90, B: 0xff, A: 0xff})
		}
	}
	return icon
}
