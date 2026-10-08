package runner

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestBatchSupervisorRetainsEvidenceAndCleansDescendants(t *testing.T) {
	workspace, bin := fakeCodex(t, `/bin/sleep 30 </dev/null >/dev/null 2>&1 &
echo "$!" > child-pid
printf '%s\n' '{"type":"turn.completed"}'`)
	definition := HarnessDefinition{ID: "batch", Provider: "codex", Transport: TransportCLI, Dialect: "codex", Executable: "codex", SchemaVersion: 1}
	prepared, err := Prepare(context.Background(), definition, RunInput{Workspace: workspace, Preset: PresetDangerFullAccess, SearchPath: bin + string(os.PathListSeparator) + "/bin", Deadline: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.CreateTemp(t.TempDir(), "inherited")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	var output bytes.Buffer
	pid := 0
	prepared.ExtraFiles = []*os.File{file}
	prepared.Output = &output
	prepared.ProcessStarted = func(value int) error { pid = value; return nil }
	receipt, err := Execute(context.Background(), prepared, nil)
	if err != nil {
		t.Fatal(err)
	}
	if pid <= 0 || receipt.Outcome != EventCompleted || !strings.Contains(output.String(), "turn.completed") {
		t.Fatalf("missing batch observation: pid=%d receipt=%+v log=%q", pid, receipt, output.String())
	}
	raw, err := os.ReadFile(filepath.Join(workspace, "child-pid"))
	if err != nil {
		t.Fatal(err)
	}
	child, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	defer syscall.Kill(child, syscall.SIGKILL)
	deadline := time.Now().Add(time.Second)
	for {
		stat, err := exec.Command("ps", "-p", strconv.Itoa(child), "-o", "stat=").Output()
		if err != nil || strings.Contains(string(stat), "Z") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("orphan descendant %d survived supervisor completion: %s", child, stat)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
