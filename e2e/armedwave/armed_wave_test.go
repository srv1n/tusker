package armedwave_test

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// Keep one process-boundary proof for the restart reducer. The detailed
// timeline/failure matrices live beside the daemon package; this verifies they
// remain executable through the repository's public Go test boundary.
func TestArmedWaveRestartConvergence(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve e2e source path")
	}
	repo := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	cmd := exec.Command("go", "test", "./cmd/tusker", "-run", "TestArmedWaveRestart$", "-count=1")
	cmd.Dir = repo
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("armed-wave restart boundary failed: %v\n%s", err, output)
	}
}
