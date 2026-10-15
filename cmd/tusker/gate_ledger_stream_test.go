package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"testing"
)

func TestWorkspaceTreeStateHashStreamsLargeFiles(t *testing.T) {
	repo := orchestrationGitRepo(t)
	contents := map[string][]byte{
		"empty.bin": nil,
		"large.bin": bytes.Repeat([]byte{0, 1, 127, 128, 255}, (16<<20)/5+1),
		"script.sh": []byte("#!/bin/sh\necho fixture\n"),
	}
	scope := []string{"empty.bin", "large.bin", "script.sh", "tracked.txt"}
	for rel, content := range contents {
		mode := os.FileMode(0o644)
		if rel == "script.sh" {
			mode = 0o755
		}
		if err := os.WriteFile(filepath.Join(repo, rel), content, mode); err != nil {
			t.Fatal(err)
		}
	}
	if runtime.GOOS != "windows" {
		if err := os.Symlink("missing-target", filepath.Join(repo, "link")); err != nil {
			t.Fatal(err)
		}
		scope = append(scope, "link")
	}
	if err := os.Remove(filepath.Join(repo, "tracked.txt")); err != nil {
		t.Fatal(err)
	}
	sort.Strings(scope)
	// Independent framing oracle: the streaming change must retain every
	// existing file digest and type marker, including empty and deleted files.
	wantHash := sha256.New()
	for _, rel := range scope {
		_, _ = wantHash.Write([]byte("scope\x00" + rel + "\x00"))
	}
	for _, rel := range scope {
		_, _ = wantHash.Write([]byte(rel + "\x00"))
		switch rel {
		case "tracked.txt":
			_, _ = wantHash.Write([]byte("<deleted>"))
		case "link":
			_, _ = wantHash.Write([]byte("<symlink>\x00missing-target\x00"))
		default:
			info, err := os.Stat(filepath.Join(repo, rel))
			if err != nil {
				t.Fatal(err)
			}
			marker := "<regular>"
			if info.Mode().Perm()&0o111 != 0 {
				marker = "<regular:executable>"
			}
			_, _ = wantHash.Write([]byte(marker))
			digest := sha256.Sum256(contents[rel])
			_, _ = wantHash.Write(digest[:])
		}
	}
	want := hex.EncodeToString(wantHash.Sum(nil))
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	got, err := workspaceTreeStateHashForPaths(repo, scope)
	runtime.ReadMemStats(&after)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("streamed digest = %s, want %s", got, want)
	}
	// Includes Git enumeration and path bookkeeping, but remains well below
	// one copy of the 16MiB source file. No elapsed-time assertion is needed.
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated > 4<<20 {
		t.Fatalf("hash allocated %d bytes for a 16MiB file; want <= 4MiB", allocated)
	}
}
