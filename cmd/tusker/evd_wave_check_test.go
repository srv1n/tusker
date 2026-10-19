package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const evdWaveSpec = ".tusker/specs/delivery.md"

func evdArmedWave(t *testing.T) (string, *RuntimeStore, RegisteredProject) {
	t.Helper()
	vault, store, project := authorityFixture(t)
	writeDirectTask(t, vault, "APP-T-0001", "W-0001", map[string]any{"spec_refs": []any{evdWaveSpec}})
	writeDirectTask(t, vault, "APP-T-0002", "W-0001", nil)
	writeDirectWave(t, vault, "W-0001", []string{"APP-T-0001", "APP-T-0002"}, map[string]any{"spec_refs": []any{evdWaveSpec}, "concurrency": 1})
	if _, err := directWaveStart(vault, store, "W-0001", "human:sarav"); err != nil {
		t.Fatal(err)
	}
	return vault, store, project
}

func evdCountWaveCheckWork(t *testing.T) (loads, fingerprints *int) {
	t.Helper()
	loads, fingerprints = new(int), new(int)
	previousLoad, previousFingerprint := loadV7IndexObserver, waveMaterialFingerprintObserver
	loadV7IndexObserver = func() { *loads++ }
	waveMaterialFingerprintObserver = func() { *fingerprints++ }
	t.Cleanup(func() { loadV7IndexObserver, waveMaterialFingerprintObserver = previousLoad, previousFingerprint })
	return loads, fingerprints
}

func TestWaveCheckReusesUnchangedMaterial(t *testing.T) {
	vault, store, project := evdArmedWave(t)
	if auth := waveAuthorizationState(t, vault, "W-0001"); stringField(auth, "state") != "armed" {
		t.Fatalf("fixture wave is not armed: %#v", auth)
	}
	// Arming already warmed the cache; start cold as a fresh daemon would.
	v7DocumentWriteGeneration.Add(1)
	loads, fingerprints := evdCountWaveCheckWork(t)
	for range 2 {
		if _, err := queueAuthorizedWaveFrontier(vault, store, project.ProjectID, "W-0001", time.Now().UTC()); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("two unchanged wave checks: vault loads=%d fingerprints=%d", *loads, *fingerprints)
	if *loads > 1 || *fingerprints > 1 {
		t.Fatalf("two unchanged wave checks: vault loads=%d fingerprints=%d, want at most 1 each", *loads, *fingerprints)
	}
}

func TestWaveCheckSeesMaterialChange(t *testing.T) {
	rewrite := func(t *testing.T, path, old, replacement string) {
		t.Helper()
		raw, err := os.ReadFile(path)
		if err != nil || !strings.Contains(string(raw), old) {
			t.Fatalf("read %s: %v (missing %q)", path, err, old)
		}
		if err := os.WriteFile(path, []byte(strings.Replace(string(raw), old, replacement, 1)), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		name   string
		mutate func(t *testing.T, vault string)
	}{
		{"member task", func(t *testing.T, vault string) {
			rewrite(t, filepath.Join(vault, "work", "tasks", "APP-T-0002.md"), "Direct APP-T-0002", "Edited APP-T-0002")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			vault, store, project := evdArmedWave(t)
			// Warm the cross-check cache, then confirm it is serving.
			for range 2 {
				if _, err := queueAuthorizedWaveFrontier(vault, store, project.ProjectID, "W-0001", time.Now().UTC()); err != nil {
					t.Fatal(err)
				}
			}
			armed := stringField(waveAuthorizationState(t, vault, "W-0001"), "authorizedFingerprint")
			_, fingerprints := evdCountWaveCheckWork(t)
			idx, check, err := waveCheckIndex(vault)
			if err != nil {
				t.Fatal(err)
			}
			if got, _ := check.fingerprint(vault, idx, idx.Waves["W-0001"]); got != armed || *fingerprints != 0 {
				t.Fatalf("warm check fingerprint=%s armed=%s recomputed=%d", got, armed, *fingerprints)
			}

			tc.mutate(t, vault)
			idx, check, err = waveCheckIndex(vault)
			if err != nil {
				t.Fatal(err)
			}
			got, _ := check.fingerprint(vault, idx, idx.Waves["W-0001"])
			freshIdx, err := loadV7Index(vault)
			if err != nil {
				t.Fatal(err)
			}
			want, _ := waveMaterialFingerprint(vault, freshIdx, freshIdx.Waves["W-0001"])
			if got != want || got == armed {
				t.Fatalf("after %s change: check=%s fresh=%s armed=%s", tc.name, got, want, armed)
			}
			queued, err := queueAuthorizedWaveFrontier(vault, store, project.ProjectID, "W-0001", time.Now().UTC())
			if err != nil || len(queued) != 0 {
				t.Fatalf("changed material still queued=%v err=%v", queued, err)
			}
		})
	}

	// Cited spec text changes through commits to main, which never need
	// re-approval (M5): the check must keep the armed fingerprint.
	t.Run("cited spec text", func(t *testing.T) {
		vault, _, _ := evdArmedWave(t)
		armed := stringField(waveAuthorizationState(t, vault, "W-0001"), "authorizedFingerprint")
		rewrite(t, filepath.Join(v7RepoRoot(vault), filepath.FromSlash(evdWaveSpec)), "Existing context.", "Edited context.")
		idx, check, err := waveCheckIndex(vault)
		if err != nil {
			t.Fatal(err)
		}
		if got, _ := check.fingerprint(vault, idx, idx.Waves["W-0001"]); got != armed {
			t.Fatalf("spec text edit changed the wave fingerprint: %s != %s", got, armed)
		}
	})

	t.Run("branch tip", func(t *testing.T) {
		repo := t.TempDir()
		gitDirOutput(t, repo, "init")
		gitDirOutput(t, repo, "config", "user.email", "test@example.com")
		gitDirOutput(t, repo, "config", "user.name", "Test")
		rel := "work/tasks/APP-T-0001.md"
		write := func(status string) {
			if err := writeText(filepath.Join(repo, filepath.FromSlash(rel)), "---\nkind: task\nid: APP-T-0001\nstatus: "+status+"\n---\n# APP-T-0001\n"); err != nil {
				t.Fatal(err)
			}
			gitDirOutput(t, repo, "add", rel)
			gitDirOutput(t, repo, "commit", "-m", status)
			gitDirOutput(t, repo, "branch", "-f", "wave/W-0001", "HEAD")
		}
		read := func() string {
			// A fresh memo per read is a fresh daemon poll.
			vault := filepath.Join(repo, ".tusker")
			note, ok, err := newArmedWavePassMemo(vault).noteAtRef(vault, repo, "wave/W-0001", rel)
			if err != nil || !ok {
				t.Fatalf("noteAtRef ok=%v err=%v", ok, err)
			}
			return stringField(note.Data, "status")
		}
		write("review")
		if got := read(); got != "review" {
			t.Fatalf("first poll status=%s", got)
		}
		if got := read(); got != "review" {
			t.Fatalf("cached poll status=%s", got)
		}
		write("done")
		if got := read(); got != "done" {
			t.Fatalf("new branch tip was served stale: status=%s", got)
		}
	})
}
