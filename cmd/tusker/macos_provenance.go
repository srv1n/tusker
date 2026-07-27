package main

import (
	"os/exec"
	"runtime"
)

// stripMacOSBuildProvenance prevents a locally built binary under a
// Finder-managed directory (for example ~/Downloads) from inheriting the
// com.apple.provenance attribute. That attribute can make dyld stall before
// Tusker has a chance to start. It is intentionally a Darwin-only best-effort
// cleanup: the attribute may simply be absent, and other platforms do nothing.
func stripMacOSBuildProvenance(path string) {
	stripMacOSBuildProvenanceFor(runtime.GOOS, path, func(path string) error {
		return exec.Command("xattr", "-d", "com.apple.provenance", path).Run()
	})
}

func stripMacOSBuildProvenanceFor(goos, path string, remove func(string) error) {
	if goos != "darwin" || path == "" {
		return
	}
	_ = remove(path) // Missing attributes are normal; never block installation.
}
