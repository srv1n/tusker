package main

import "testing"

func TestStripMacOSBuildProvenanceFor(t *testing.T) {
	t.Run("does nothing outside darwin", func(t *testing.T) {
		called := false
		stripMacOSBuildProvenanceFor("linux", "/tmp/tusker", func(string) error {
			called = true
			return nil
		})
		if called {
			t.Fatal("provenance cleanup ran outside darwin")
		}
	})

	t.Run("removes only the produced darwin binary", func(t *testing.T) {
		var got string
		stripMacOSBuildProvenanceFor("darwin", "/tmp/tusker", func(path string) error {
			got = path
			return nil
		})
		if got != "/tmp/tusker" {
			t.Fatalf("cleanup target = %q, want produced binary", got)
		}
	})

	t.Run("does nothing without a path", func(t *testing.T) {
		called := false
		stripMacOSBuildProvenanceFor("darwin", "", func(string) error {
			called = true
			return nil
		})
		if called {
			t.Fatal("provenance cleanup ran without a binary path")
		}
	})
}
