package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestServeWaveAuthorizationCachedPerSnapshot(t *testing.T) {
	server := newServeEmptyNeedsFixture(t)
	writeDepartureTestWave(t, server.vaultPath, "W-CACHE", "armed", "")
	server.invalidateProjectSnapshot("app")

	get := func(path string) {
		t.Helper()
		rec := httptest.NewRecorder()
		server.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status=%d body=%s", path, rec.Code, rec.Body.String())
		}
	}
	derived := func() *serveSnapshotDerived {
		t.Helper()
		snap, err := server.loadSnapshotForProject("")
		if err != nil {
			t.Fatal(err)
		}
		if len(snap.waves) != 1 || snap.derived == nil {
			t.Fatalf("waves=%d derived=%v", len(snap.waves), snap.derived)
		}
		return snap.derived
	}
	computes := func(d *serveSnapshotDerived) int {
		d.mu.Lock()
		defer d.mu.Unlock()
		return d.computes
	}
	readAll := func() {
		for _, path := range []string{"/api/waves", "/api/waves?view=list", "/api/tasks", "/api/waves/W-CACHE"} {
			get(path)
		}
	}

	readAll()
	first := derived()
	want := computes(first)
	if want == 0 {
		t.Fatal("wave authorization was never computed through the cache")
	}
	readAll()
	if got := computes(first); got != want {
		t.Fatalf("repeat requests recomputed authorization: %d -> %d", want, got)
	}

	server.invalidateProjectRuntimeSnapshot("app")
	readAll()
	if d := derived(); d != first || computes(d) != want {
		t.Fatalf("runtime refresh dropped metadata cache: same=%v computes=%d", d == first, computes(d))
	}

	server.invalidateProjectSnapshot("app")
	readAll()
	rebuilt := derived()
	if rebuilt == first {
		t.Fatal("full rebuild reused the previous snapshot cache")
	}
	if got := computes(rebuilt); got != want {
		t.Fatalf("rebuilt snapshot computes=%d want %d", got, want)
	}
}
