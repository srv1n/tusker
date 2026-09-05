//go:build !windows

package contractconvergence_test

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

// TestContractConvergence is the release gate for the phase/readiness contract.
// The focused tests own their fixture detail; this test proves they converge in
// one hermetic authority boundary instead of silently relying on the operator's
// home, resident services, provider credentials, or repository refs.
func TestContractConvergence(t *testing.T) {
	repo := repositoryRoot(t)
	goBinary, err := exec.LookPath("go")
	if err != nil {
		t.Fatal("locate go:", err)
	}
	moduleCache := strings.TrimSpace(commandOutput(t, "", goBinary, "env", "GOMODCACHE"))

	sandbox := t.TempDir()
	home := filepath.Join(sandbox, "home")
	stateRoot := filepath.Join(sandbox, "state")
	tmpRoot := filepath.Join(sandbox, "tmp")
	for _, path := range []string{home, stateRoot, tmpRoot} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatalf("create sandbox path %s: %v", path, err)
		}
	}
	trapBin, trapLog := installAuthorityTraps(t, sandbox)
	stopSecretSentinel := watchSecretReadSentinel(t, filepath.Join(home, ".env"))
	before := snapshotRepositoryAuthority(t, repo)

	tests := []string{
		// Typed phase/readiness and inert held import.
		"TestReadinessContract",
		"TestDeliveryPhaseReadinessSeparation",
		"TestDeliveryReviewEnvironmentStatesHaveOneTruthfulAction",

		// Interactive admission, exact typed refusals, CAS lifecycle, reclaim,
		// and proof that work notifications cannot become dispatch.
		"TestInteractiveWorkReadinessSeparation",
		"TestWorkSessionInteractiveStartWithAutomationDisabled",
		"TestWorkSessionStartRefusalMatrix",
		"TestWorkSessionDeadExpiredHolderReclaims",
		"TestWorkSessionLifecycleCASAndExactOnce",
		"TestWorkSessionHeartbeatAndSubmitCAS",
		"TestWorkSessionTaskRevisionDriftRefusesMutation",
		"TestWorkSessionNotificationIsExactRunHintAndDoesNotSpawn",

		// Independent fleet dimensions and authority-scoped repair.
		"TestDeliveryRolloutPreservation",
		"TestDeliveryRolloutQuarantine",
		"TestScopedFleetRepair",
		"TestFleetHealthDimensions",
		"TestMixedFleetCoreRepairPreservesOtherScopes",

		// Binary/package compatibility, every install shape, deterministic
		// repair, and bounded progressive disclosure.
		"TestInstalledCapabilityManifest",
		"TestCanonicalSkillCompatibilityMatchesFactoryIntakeContract",
		"TestMaterializedSkillProvenanceClassifiesFreshnessAndLocalEdits",
		"TestSymlinkProvenanceReadsLiveTarget",
		"TestSkillBundleProvenanceIsPortable",
		"TestSkillSyncCopyUsesValidatedCanonicalSource",
		"TestSetupDoctorRepairsGeneratedSkillInstallsFromCanonicalSource",
		"TestSetupDoctorRepairsLocallyModifiedSkillInstall",
		"TestAsymmetricManagedSkillMetadataBlocksClaimedWaveAndSetupRepairs",
		"TestSkillContractCompatibility",
		"TestTuskerSkillProgressiveDisclosure",
	}
	sort.Strings(tests)
	pattern := "^(" + strings.Join(tests, "|") + ")$"

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, goBinary, "test", "./cmd/tusker", "-run", pattern, "-count=1", "-parallel", "4", "-v")
	cmd.Dir = repo
	cmd.Env = contractTestEnvironment(home, stateRoot, tmpRoot, trapBin, trapLog, moduleCache)
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	err = cmd.Run()
	secretRead := stopSecretSentinel()
	if ctx.Err() != nil {
		t.Fatalf("focused convergence suite timed out: %v\n%s", ctx.Err(), output.String())
	}
	if err != nil {
		t.Fatalf("focused convergence suite failed: %v\n%s", err, output.String())
	}
	if secretRead {
		t.Fatal("convergence fixture opened the isolated HOME/.env secret sentinel")
	}
	for _, name := range tests {
		if !strings.Contains(output.String(), "=== RUN   "+name) {
			t.Fatalf("convergence gate silently skipped %s\n%s", name, output.String())
		}
	}

	if raw, err := os.ReadFile(trapLog); err != nil && !os.IsNotExist(err) {
		t.Fatal("read authority trap log:", err)
	} else if strings.TrimSpace(string(raw)) != "" {
		t.Fatalf("convergence fixture attempted forbidden external authority:\n%s", raw)
	}
	after := snapshotRepositoryAuthority(t, repo)
	if before != after {
		t.Fatalf("convergence fixture changed repository authority\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

func watchSecretReadSentinel(t *testing.T, path string) func() bool {
	t.Helper()
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Fatalf("create secret-read sentinel: %v", err)
	}
	var observed atomic.Bool
	stop := make(chan struct{})
	done := make(chan struct{})
	var once sync.Once
	go func() {
		defer close(done)
		ticker := time.NewTicker(2 * time.Millisecond)
		defer ticker.Stop()
		for {
			fd, err := syscall.Open(path, syscall.O_WRONLY|syscall.O_NONBLOCK, 0)
			if err == nil {
				observed.Store(true)
				_ = syscall.Close(fd)
				return
			}
			select {
			case <-stop:
				return
			case <-ticker.C:
			}
		}
	}()
	return func() bool {
		once.Do(func() {
			close(stop)
			<-done
		})
		return observed.Load()
	}
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test source")
	}
	root, err := filepath.Abs(filepath.Join(filepath.Dir(source), "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("resolved repository root %s: %v", root, err)
	}
	return root
}

func installAuthorityTraps(t *testing.T, root string) (string, string) {
	t.Helper()
	bin := filepath.Join(root, "authority-traps")
	log := filepath.Join(root, "authority-traps.log")
	if err := os.MkdirAll(bin, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{
		"claude", "codex", "curl", "gh", "launchctl", "op", "open",
		"osascript", "security", "wget",
	} {
		probe := ""
		switch name {
		case "codex":
			probe = "if [ \"$*\" = \"debug models\" ]; then exit 127; fi\n"
		case "claude":
			probe = "if [ \"$*\" = \"--version\" ]; then exit 127; fi\n"
		}
		script := "#!/bin/sh\n" + probe +
			"printf '%s\\n' \"$(basename \"$0\") $*\" >> \"$TUSKER_CONTRACT_TRAP_LOG\"\nexit 97\n"
		if err := os.WriteFile(filepath.Join(bin, name), []byte(script), 0o700); err != nil {
			t.Fatalf("install %s authority trap: %v", name, err)
		}
	}
	return bin, log
}

func contractTestEnvironment(home, stateRoot, tmpRoot, trapBin, trapLog, moduleCache string) []string {
	blocked := []string{
		"ANTHROPIC_", "CHATGPT_", "CLAUDE", "CODEX_", "GH_", "GITHUB_",
		"GOAUTH", "OPENAI_", "TUSKER_ATTEMPT_ID=", "TUSKER_STATE_ROOT=",
	}
	env := make([]string, 0, len(os.Environ())+12)
	for _, entry := range os.Environ() {
		reject := false
		for _, prefix := range blocked {
			if strings.HasPrefix(entry, prefix) {
				reject = true
				break
			}
		}
		if !reject && !strings.HasPrefix(entry, "HOME=") && !strings.HasPrefix(entry, "TMPDIR=") &&
			!strings.HasPrefix(entry, "GIT_CONFIG_GLOBAL=") && !strings.HasPrefix(entry, "GIT_CONFIG_SYSTEM=") {
			env = append(env, entry)
		}
	}
	return append(env,
		"HOME="+home,
		"TMPDIR="+tmpRoot,
		"TUSKER_STATE_ROOT="+stateRoot,
		"TUSKER_CONTRACT_TRAP_LOG="+trapLog,
		"PATH="+trapBin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"GIT_CONFIG_GLOBAL="+os.DevNull,
		"GIT_CONFIG_SYSTEM="+os.DevNull,
		"GIT_TERMINAL_PROMPT=0",
		"GCM_INTERACTIVE=Never",
		"GOAUTH=off",
		"GOTOOLCHAIN=local",
		"GOMODCACHE="+moduleCache,
		"GOPROXY=off",
		"GOSUMDB=off",
		"GOVCS=*:off",
	)
}

func snapshotRepositoryAuthority(t *testing.T, repo string) string {
	t.Helper()
	commands := [][]string{
		{"rev-parse", "HEAD"},
		{"show-ref", "--head"},
		{"config", "--local", "--list", "--show-origin"},
		{"status", "--porcelain=v1", "--untracked-files=all"},
	}
	var snapshot strings.Builder
	for _, args := range commands {
		raw := commandOutput(t, "", "git", append([]string{"-C", repo}, args...)...)
		fmt.Fprintf(&snapshot, "$ git %s\n%s", strings.Join(args, " "), raw)
	}
	return snapshot.String()
}

func commandOutput(t *testing.T, dir, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	raw, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s: %v\n%s", name, strings.Join(args, " "), err, raw)
	}
	return string(raw)
}
