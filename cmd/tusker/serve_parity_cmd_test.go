package main

import (
	"encoding/base64"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestServeParitySharedWrites(t *testing.T) {
	repo := t.TempDir()
	seedDocgraphCorpus(t, repo)
	path := filepath.Join(repo, "docs/system/00-overview.md")
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	body := "# System Overview\n\nEdited through shared save."
	req := serveDocgraphSaveRequest{BaseRev: serveDocgraphRev(original), Body: &body}
	if status, result := saveDocgraphDoc(repo, "overview", req); status != http.StatusOK {
		t.Fatalf("save status=%d result=%v", status, result)
	}
	saved, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(saved), "Edited through shared save.") {
		t.Fatalf("saved=%q err=%v", saved, err)
	}
	if status, _ := saveDocgraphDoc(repo, "overview", req); status != http.StatusConflict {
		t.Fatalf("stale save status=%d", status)
	}

	t.Setenv("TUSKER_STATE_ROOT", t.TempDir())
	svg := []byte("<svg xmlns=\"http://www.w3.org/2000/svg\"/>")
	icon := serveActionBody{"data": base64.StdEncoding.EncodeToString(svg), "mime": "image/svg+xml"}
	if result := saveProjectIcon("test-parity", "test-parity", icon); !result.OK {
		t.Fatalf("icon set: %+v", result)
	}
	if result := saveProjectIcon("test-parity", "test-parity", serveActionBody{"clear": true}); !result.OK {
		t.Fatalf("icon remove: %+v", result)
	}
}
