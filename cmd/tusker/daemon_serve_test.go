package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func TestDaemonServeInProcessReportsPidAndPollData(t *testing.T) {
	stateRoot := setupDaemonServeProject(t, true, "127.0.0.1:0")
	errCh := startDaemonRunForTest(t, stateRoot)
	addr := waitForDaemonServeAddr(t, stateRoot, errCh)
	defer stopDaemonRunForTest(t, stateRoot, errCh)

	var status serveDaemonStatus
	waitForDaemonServeJSON(t, "http://"+addr+"/api/daemon", &status, func() bool {
		return status.DaemonAlive && status.DaemonPID == os.Getpid() && status.DaemonLastPollAt != nil
	})
	if status.Addr != addr {
		t.Fatalf("expected served addr %q, got %#v", addr, status)
	}

	var projects []serveProjectSummary
	waitForDaemonServeJSON(t, "http://"+addr+"/api/projects", &projects, func() bool {
		return len(projects) == 1 && projects[0].DaemonConnected
	})

	resp, err := http.Get("http://" + addr + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected embedded SPA status 200, got %d", resp.StatusCode)
	}
}

func TestDaemonServeStreamIdlePollEmitsNoInvalidation(t *testing.T) {
	stateRoot := setupDaemonServeProject(t, false, "127.0.0.1:0")
	daemon, err := NewDaemon(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer daemon.Close()
	daemon.stream = newServeStreamBroker()
	ch, unsubscribe, ok := daemon.stream.Subscribe()
	if !ok {
		t.Fatal("expected stream subscription")
	}
	defer unsubscribe()
	if err := daemon.PollOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case event := <-ch:
		t.Fatalf("idle poll emitted client invalidation: %#v", event)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestServePanicRecoveryKeepsDaemonPollUsable(t *testing.T) {
	stateRoot := filepath.Join(t.TempDir(), "state")
	t.Setenv("TUSKER_STATE_ROOT", stateRoot)
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	server := newServeServer(t.TempDir(), t.TempDir(), "127.0.0.1:0", store, panicFS{})
	var logs bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(previous) })

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected recovered panic to return 500, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(logs.String(), "recovered handler panic") {
		t.Fatalf("expected recovered panic to be logged, got %q", logs.String())
	}

	daemon := &Daemon{stateRoot: stateRoot, store: store}
	if err := daemon.PollOnce(context.Background()); err != nil {
		t.Fatalf("poll loop must remain usable after handler panic: %v", err)
	}
	status, err := store.DaemonStatus()
	if err != nil {
		t.Fatal(err)
	}
	if stringValue(status["daemon_last_poll_at"]) == "" {
		t.Fatalf("expected poll data after recovery, got %#v", status)
	}
}

func TestServeDefersToDaemonAndStandaloneStillServes(t *testing.T) {
	stateRoot := filepath.Join(t.TempDir(), "state")
	t.Setenv("TUSKER_STATE_ROOT", stateRoot)
	guard, err := acquireDaemonGuard(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	if err := guard.updateServePIDFile(true, "127.0.0.1:7777"); err != nil {
		t.Fatal(err)
	}
	output := captureStdout(t, func() {
		if err := serveCmd(Args{}); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(output, "http://127.0.0.1:7777") {
		t.Fatalf("expected serve to print incumbent URL, got %q", output)
	}
	if _, err := os.Stat(runtimeStoreDBPath(stateRoot)); !os.IsNotExist(err) {
		t.Fatalf("deferring serve should not open the runtime store, stat err=%v", err)
	}
	if err := guard.Close(); err != nil {
		t.Fatal(err)
	}
	deferred, err := serveDeferToIncumbentDaemon(Args{}, stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	if deferred {
		t.Fatal("expected no incumbent after daemon guard closes")
	}

	standalone := newServeEmptyNeedsFixture(t)
	req := httptest.NewRequest(http.MethodGet, "/api/needs", nil)
	rec := httptest.NewRecorder()
	standalone.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Fatalf("standalone serve handler regressed: status=%d body=%q", rec.Code, rec.Body.String())
	}
}

func TestServeConfigDisableAndLocalhostDefaults(t *testing.T) {
	wf := defaultWorkflow()
	if !wf.Runtime.Serve.Enabled {
		t.Fatal("default workflow must enable embedded serve")
	}
	if wf.Runtime.Serve.Addr != defaultServeAddr {
		t.Fatalf("expected default serve addr %q, got %q", defaultServeAddr, wf.Runtime.Serve.Addr)
	}
	wf.Runtime.Serve.Addr = "0.0.0.0:7420"
	err := validateWorkflowFile(WorkflowFile{Path: "WORKFLOW.md", Body: defaultWorkflowMarkdown(), Data: wf})
	if err == nil {
		t.Fatal("expected non-localhost runtime.serve.addr to be rejected")
	}

	stateRoot := setupDaemonServeProject(t, false, "127.0.0.1:0")
	errCh := startDaemonRunForTest(t, stateRoot)
	defer stopDaemonRunForTest(t, stateRoot, errCh)
	waitForDaemonServeDisabled(t, stateRoot, errCh)

	output := captureStdout(t, func() {
		if err := serveCmd(Args{}); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(output, "embedded serve disabled") {
		t.Fatalf("expected disabled embedded serve message, got %q", output)
	}
}

type panicFS struct{}

func (panicFS) Open(string) (fs.File, error) {
	panic("fixture handler panic")
}

func setupDaemonServeProject(t *testing.T, serveEnabled bool, serveAddr string) string {
	t.Helper()
	root := shortDaemonServeTempDir(t)
	stateRoot := filepath.Join(root, "state")
	vault := filepath.Join(root, ".tusker")
	t.Setenv("TUSKER_STATE_ROOT", stateRoot)
	for _, dir := range []string{
		filepath.Join(vault, "work", "tasks"),
		filepath.Join(vault, "work", "epics"),
		filepath.Join(vault, "work", "gates"),
	} {
		if err := ensureDir(dir); err != nil {
			t.Fatal(err)
		}
	}
	if err := writeText(filepath.Join(root, "tusker.yaml"), "schema: tusker.config/v1\nproject_id: app\nstorage:\n  root: .tusker\n"); err != nil {
		t.Fatal(err)
	}
	writeDaemonServeWorkflow(t, vault, serveEnabled, serveAddr)
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	project := RegisteredProject{
		ProjectID: "app", ProjectKey: "app", Name: "app",
		RepoRoot: root, VaultRoot: vault, WorkflowPath: workflowPath(vault),
		Enabled: true, Health: projectHealthHealthy,
	}
	if err := store.UpsertProject(project); err != nil {
		t.Fatal(err)
	}
	return stateRoot
}

func writeDaemonServeWorkflow(t *testing.T, vault string, serveEnabled bool, serveAddr string) {
	t.Helper()
	wf := defaultWorkflow()
	wf.Runtime.PollIntervalMS = 25
	wf.Runtime.Serve.Enabled = serveEnabled
	wf.Runtime.Serve.Addr = serveAddr
	raw, err := yaml.Marshal(wf)
	if err != nil {
		t.Fatal(err)
	}
	body := "---\n" + strings.TrimSpace(string(raw)) + "\n---\n\n## Routing\n\nTest routing.\n\n## Prompt\n\nTest prompt.\n\n## Retry policy\n\nRetry transient failures.\n\n## Human override policy\n\nHumans may override.\n"
	if err := writeText(workflowPath(vault), body); err != nil {
		t.Fatal(err)
	}
}

func startDaemonRunForTest(t *testing.T, stateRoot string) <-chan error {
	t.Helper()
	t.Setenv("TUSKER_STATE_ROOT", stateRoot)
	clearAgentSessionEnvForTest(t)
	errCh := make(chan error, 1)
	go func() {
		errCh <- daemonRunCmd(Args{})
	}()
	return errCh
}

func stopDaemonRunForTest(t *testing.T, stateRoot string, errCh <-chan error) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	stopRequested := false
	for time.Now().Before(deadline) {
		select {
		case err := <-errCh:
			if err != nil {
				t.Fatalf("daemon exited with error: %v", err)
			}
			return
		default:
		}
		if liveness := readDaemonLiveness(stateRoot, time.Now().UTC()); !liveness.Alive {
			return
		}
		resp, err := sendDaemonControlWithTimeout(stateRoot, daemonControlRequest{Command: "stop"}, 250*time.Millisecond)
		if err == nil && resp.OK {
			stopRequested = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !stopRequested {
		t.Fatal("daemon stop request was not accepted")
	}
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("daemon exited with error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("daemon did not stop")
	}
}

func shortDaemonServeTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "tusker-daemon-serve-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func waitForDaemonServeAddr(t *testing.T, stateRoot string, errCh <-chan error) string {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case err := <-errCh:
			t.Fatalf("daemon exited before serve started: %v", err)
		default:
		}
		pidFile, ok, err := readDaemonPIDFile(filepath.Join(stateRoot, daemonPIDFileName))
		if err != nil {
			t.Fatal(err)
		}
		if ok && pidFile.ServeEnabled && strings.TrimSpace(pidFile.ServeAddr) != "" && pidFile.ServeAddr != defaultServeAddr {
			return pidFile.ServeAddr
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("daemon serve address was not published")
	return ""
}

func waitForDaemonServeDisabled(t *testing.T, stateRoot string, errCh <-chan error) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case err := <-errCh:
			t.Fatalf("daemon exited before disabled serve was published: %v", err)
		default:
		}
		pidFile, ok, err := readDaemonPIDFile(filepath.Join(stateRoot, daemonPIDFileName))
		if err != nil {
			t.Fatal(err)
		}
		if ok && !pidFile.ServeEnabled && strings.TrimSpace(pidFile.ServeAddr) == "" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("daemon disabled serve state was not published")
}

func waitForDaemonServeJSON[T any](t *testing.T, url string, out *T, ready func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		resp, err := http.Get(url)
		if err != nil {
			lastErr = err
			time.Sleep(10 * time.Millisecond)
			continue
		}
		func() {
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				lastErr = errUnexpectedHTTPStatus(resp.StatusCode)
				return
			}
			if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
				lastErr = err
				return
			}
			if ready() {
				lastErr = nil
				return
			}
			lastErr = errConditionNotReady{}
		}()
		if lastErr == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s: %v", url, lastErr)
}

type errConditionNotReady struct{}

func (errConditionNotReady) Error() string { return "condition not ready" }

type errUnexpectedHTTPStatus int

func (e errUnexpectedHTTPStatus) Error() string { return fmt.Sprintf("unexpected HTTP status %d", e) }
