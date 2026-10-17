package docgraph

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCachedResolverReusesUntilTreeChanges(t *testing.T) {
	root := t.TempDir()
	spec := filepath.Join(root, ".tusker", "specs", "a.md")
	if err := os.MkdirAll(filepath.Dir(spec), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(spec, []byte("---\nsubject: alpha\n---\n\n# A\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	first, err := CachedResolver(root)
	if err != nil {
		t.Fatal(err)
	}
	if again, _ := CachedResolver(root); again != first {
		t.Fatal("unchanged tree rebuilt the resolver")
	}
	if _, ok := first.ResolveStrict("alpha"); !ok {
		t.Fatal("alpha should resolve")
	}

	// Rewrite in place with a new subject and a distinct mtime.
	if err := os.WriteFile(spec, []byte("---\nsubject: bravo\n---\n\n# B\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	later := time.Now().Add(time.Second)
	if err := os.Chtimes(spec, later, later); err != nil {
		t.Fatal(err)
	}
	changed, err := CachedResolver(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := changed.ResolveStrict("bravo"); !ok || changed == first {
		t.Fatal("edited document did not invalidate the cache")
	}

	// A new file invalidates too.
	if err := os.WriteFile(filepath.Join(filepath.Dir(spec), "c.md"), []byte("---\nsubject: charlie\n---\n\n# C\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	added, _ := CachedResolver(root)
	if _, ok := added.ResolveStrict("charlie"); !ok {
		t.Fatal("added document did not invalidate the cache")
	}
}
