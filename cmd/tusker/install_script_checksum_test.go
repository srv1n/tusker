package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const (
	installScriptTestVersion  = "v9.9.9"
	installScriptTestChecksum = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	installScriptOtherSum     = "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"
)

func TestInstallScriptChecksumFailuresPreserveInstalledBinary(t *testing.T) {
	tests := []struct {
		name       string
		unameS     string
		unameM     string
		tool       string
		actual     string
		manifest   func(string) string
		wantOutput []string
	}{
		{
			name:     "missing checksum tools",
			unameS:   "Darwin",
			unameM:   "arm64",
			manifest: validInstallScriptManifest,
			wantOutput: []string{
				"Missing required checksum command",
				"shasum (macOS)",
				"sha256sum (Linux)",
			},
		},
		{
			name:   "missing asset entry",
			unameS: "Darwin",
			unameM: "arm64",
			tool:   "shasum",
			actual: installScriptTestChecksum,
			manifest: func(string) string {
				return installScriptTestChecksum + "  another_asset.tar.gz\n"
			},
			wantOutput: []string{"Checksum entry missing"},
		},
		{
			name:   "duplicate asset entries",
			unameS: "Linux",
			unameM: "x86_64",
			tool:   "sha256sum",
			actual: installScriptTestChecksum,
			manifest: func(asset string) string {
				return fmt.Sprintf("%s  %s\n%s  %s\n", installScriptTestChecksum, asset, installScriptOtherSum, asset)
			},
			wantOutput: []string{"Duplicate checksum entries"},
		},
		{
			name:   "empty asset entry",
			unameS: "Darwin",
			unameM: "arm64",
			tool:   "shasum",
			actual: installScriptTestChecksum,
			manifest: func(asset string) string {
				return "  " + asset + "\n"
			},
			wantOutput: []string{"empty SHA-256 value"},
		},
		{
			name:   "checksum mismatch",
			unameS: "Linux",
			unameM: "x86_64",
			tool:   "sha256sum",
			actual: installScriptOtherSum,
			manifest: func(asset string) string {
				return fmt.Sprintf("%s  %s\n", installScriptTestChecksum, asset)
			},
			wantOutput: []string{
				"Checksum mismatch",
				"expected: " + installScriptTestChecksum,
				"actual:   " + installScriptOtherSum,
			},
		},
		{
			name:   "malformed asset checksum",
			unameS: "Darwin",
			unameM: "arm64",
			tool:   "shasum",
			actual: installScriptTestChecksum,
			manifest: func(asset string) string {
				return "not-a-sha256  " + asset + "\n"
			},
			wantOutput: []string{"malformed SHA-256 value"},
		},
		{
			name:   "malformed computed checksum",
			unameS: "Linux",
			unameM: "x86_64",
			tool:   "sha256sum",
			actual: "not-a-sha256",
			manifest: func(asset string) string {
				return fmt.Sprintf("%s  %s\n", installScriptTestChecksum, asset)
			},
			wantOutput: []string{"command returned a malformed checksum"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture := newInstallScriptChecksumFixture(t, tt.unameS, tt.unameM, tt.tool, tt.actual)
			fixture.writeManifest(t, tt.manifest(fixture.asset))

			output, err := fixture.run(t)
			if err == nil {
				t.Fatalf("installer unexpectedly succeeded:\n%s", output)
			}
			for _, want := range tt.wantOutput {
				if !strings.Contains(output, want) {
					t.Errorf("installer output missing %q:\n%s", want, output)
				}
			}
			fixture.assertInstalledBinary(t, "existing binary")
			fixture.assertMutationCommandsNotRun(t)
		})
	}
}

func TestInstallScriptChecksumToolVariantsInstallValidRelease(t *testing.T) {
	tests := []struct {
		name   string
		unameS string
		unameM string
		tool   string
	}{
		{name: "macOS shasum", unameS: "Darwin", unameM: "arm64", tool: "shasum"},
		{name: "Linux sha256sum", unameS: "Linux", unameM: "x86_64", tool: "sha256sum"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture := newInstallScriptChecksumFixture(t, tt.unameS, tt.unameM, tt.tool, installScriptTestChecksum)
			fixture.writeManifest(t, validInstallScriptManifest(fixture.asset))

			output, err := fixture.run(t)
			if err != nil {
				t.Fatalf("installer failed: %v\n%s", err, output)
			}
			fixture.assertInstalledBinary(t, "verified binary")
			fixture.assertMutationCommandsRun(t)
			calledTool, err := os.ReadFile(fixture.checksumToolLog)
			if err != nil {
				t.Fatalf("read checksum tool log: %v", err)
			}
			fields := strings.Fields(string(calledTool))
			if len(fields) == 0 || fields[0] != tt.tool {
				t.Fatalf("checksum invocation = %q, want tool %q", calledTool, tt.tool)
			}
			if tt.tool == "shasum" && (len(fields) != 4 || fields[1] != "-a" || fields[2] != "256") {
				t.Fatalf("shasum invocation = %q, want 'shasum -a 256 <archive>'", calledTool)
			}
			if tt.tool == "sha256sum" && len(fields) != 2 {
				t.Fatalf("sha256sum invocation = %q, want 'sha256sum <archive>'", calledTool)
			}
			if got := filepath.Base(fields[len(fields)-1]); got != fixture.asset {
				t.Fatalf("checksum target = %q, want release archive %q", got, fixture.asset)
			}
		})
	}
}

func validInstallScriptManifest(asset string) string {
	return fmt.Sprintf("%s  unrelated_asset.tar.gz\n%s  %s\n", installScriptOtherSum, installScriptTestChecksum, asset)
}

type installScriptChecksumFixture struct {
	root            string
	binDir          string
	fakeBin         string
	releaseDir      string
	asset           string
	extractSubdir   string
	actualChecksum  string
	checksumToolLog string
	tarLog          string
	installLog      string
	newBinaryPath   string
	unameS          string
	unameM          string
}

func newInstallScriptChecksumFixture(t *testing.T, unameS, unameM, checksumTool, actualChecksum string) *installScriptChecksumFixture {
	t.Helper()

	osName := map[string]string{"Darwin": "darwin", "Linux": "linux"}[unameS]
	arch := map[string]string{"arm64": "arm64", "x86_64": "amd64"}[unameM]
	if osName == "" || arch == "" {
		t.Fatalf("unsupported fixture platform %s/%s", unameS, unameM)
	}

	root := t.TempDir()
	fixture := &installScriptChecksumFixture{
		root:            root,
		binDir:          filepath.Join(root, "bin"),
		fakeBin:         filepath.Join(root, "fake-bin"),
		releaseDir:      filepath.Join(root, "release"),
		asset:           fmt.Sprintf("tusker_%s_%s_%s.tar.gz", installScriptTestVersion, osName, arch),
		extractSubdir:   fmt.Sprintf("tusker_%s_%s_%s", installScriptTestVersion, osName, arch),
		actualChecksum:  actualChecksum,
		checksumToolLog: filepath.Join(root, "checksum-tool.log"),
		tarLog:          filepath.Join(root, "tar.log"),
		installLog:      filepath.Join(root, "install.log"),
		newBinaryPath:   filepath.Join(root, "verified-tusker"),
		unameS:          unameS,
		unameM:          unameM,
	}

	for _, dir := range []string{fixture.binDir, fixture.fakeBin, fixture.releaseDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("create fixture directory %s: %v", dir, err)
		}
	}
	if err := os.WriteFile(filepath.Join(fixture.binDir, "tusker"), []byte("existing binary"), 0o755); err != nil {
		t.Fatalf("write existing binary: %v", err)
	}
	if err := os.WriteFile(fixture.newBinaryPath, []byte("verified binary"), 0o755); err != nil {
		t.Fatalf("write verified binary: %v", err)
	}
	if err := os.WriteFile(filepath.Join(fixture.releaseDir, fixture.asset), []byte("archive fixture"), 0o644); err != nil {
		t.Fatalf("write archive fixture: %v", err)
	}

	fixture.writeCommands(t, checksumTool)
	return fixture
}

func (f *installScriptChecksumFixture) writeManifest(t *testing.T, contents string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(f.releaseDir, "checksums.txt"), []byte(contents), 0o644); err != nil {
		t.Fatalf("write checksum manifest: %v", err)
	}
}

func (f *installScriptChecksumFixture) writeCommands(t *testing.T, checksumTool string) {
	t.Helper()
	for _, name := range []string{"awk", "cp", "mkdir", "mktemp", "rm"} {
		path, err := exec.LookPath(name)
		if err != nil {
			t.Fatalf("find required fixture command %s: %v", name, err)
		}
		if err := os.Symlink(path, filepath.Join(f.fakeBin, name)); err != nil {
			t.Fatalf("link fixture command %s: %v", name, err)
		}
	}

	writeInstallScriptFixtureCommand(t, f.fakeBin, "uname", `#!/bin/sh
case "${1:-}" in
	-s) printf '%s\n' "$FIXTURE_UNAME_S" ;;
	-m) printf '%s\n' "$FIXTURE_UNAME_M" ;;
	*) exit 2 ;;
esac
`)
	writeInstallScriptFixtureCommand(t, f.fakeBin, "curl", `#!/bin/sh
output=
url=
while [ "$#" -gt 0 ]; do
	case "$1" in
		-o) output="$2"; shift 2 ;;
		-*) shift ;;
		*) url="$1"; shift ;;
	esac
done
[ -n "$output" ] && [ -n "$url" ] || exit 2
cp "$FIXTURE_RELEASE_DIR/${url##*/}" "$output"
`)
	writeInstallScriptFixtureCommand(t, f.fakeBin, "tar", `#!/bin/sh
printf 'tar\n' > "$FIXTURE_TAR_LOG"
extract_dir=
while [ "$#" -gt 0 ]; do
	case "$1" in
		-C) extract_dir="$2"; shift 2 ;;
		*) shift ;;
	esac
done
[ -n "$extract_dir" ] || exit 2
mkdir -p "$extract_dir/$FIXTURE_EXTRACT_SUBDIR"
cp "$FIXTURE_NEW_BINARY_PATH" "$extract_dir/$FIXTURE_EXTRACT_SUBDIR/tusker"
`)
	writeInstallScriptFixtureCommand(t, f.fakeBin, "install", `#!/bin/sh
printf 'install\n' > "$FIXTURE_INSTALL_LOG"
if [ "$1" = "-m" ]; then
	shift 2
fi
cp "$1" "$2"
`)

	if checksumTool != "" {
		writeInstallScriptFixtureCommand(t, f.fakeBin, checksumTool, `#!/bin/sh
last_arg=
for arg in "$@"; do
	last_arg="$arg"
done
printf '%s' "`+checksumTool+`" > "$FIXTURE_CHECKSUM_TOOL_LOG"
for arg in "$@"; do
	printf ' %s' "$arg" >> "$FIXTURE_CHECKSUM_TOOL_LOG"
done
printf '\n' >> "$FIXTURE_CHECKSUM_TOOL_LOG"
printf '%s  %s\n' "$FIXTURE_ACTUAL_CHECKSUM" "$last_arg"
`)
	}
}

func writeInstallScriptFixtureCommand(t *testing.T, dir, name, body string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("write fixture command %s: %v", name, err)
	}
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatalf("make fixture command %s executable: %v", name, err)
	}
}

func (f *installScriptChecksumFixture) run(t *testing.T) (string, error) {
	t.Helper()
	scriptPath, err := filepath.Abs(filepath.Join("..", "..", "scripts", "install.sh"))
	if err != nil {
		t.Fatalf("resolve installer path: %v", err)
	}

	cmd := exec.Command("/bin/sh", scriptPath, "--version", installScriptTestVersion, "--bin-dir", f.binDir)
	cmd.Env = []string{
		"HOME=" + filepath.Join(f.root, "home"),
		"PATH=" + f.fakeBin,
		"TUSKER_DOWNLOAD_ROOT=https://fixtures.invalid/releases/download",
		"FIXTURE_RELEASE_DIR=" + f.releaseDir,
		"FIXTURE_UNAME_S=" + f.unameS,
		"FIXTURE_UNAME_M=" + f.unameM,
		"FIXTURE_ACTUAL_CHECKSUM=" + f.actualChecksum,
		"FIXTURE_CHECKSUM_TOOL_LOG=" + f.checksumToolLog,
		"FIXTURE_TAR_LOG=" + f.tarLog,
		"FIXTURE_INSTALL_LOG=" + f.installLog,
		"FIXTURE_EXTRACT_SUBDIR=" + f.extractSubdir,
		"FIXTURE_NEW_BINARY_PATH=" + f.newBinaryPath,
	}
	output, runErr := cmd.CombinedOutput()
	return string(output), runErr
}

func (f *installScriptChecksumFixture) assertInstalledBinary(t *testing.T, want string) {
	t.Helper()
	contents, err := os.ReadFile(filepath.Join(f.binDir, "tusker"))
	if err != nil {
		t.Fatalf("read installed binary: %v", err)
	}
	if got := string(contents); got != want {
		t.Fatalf("installed binary = %q, want %q", got, want)
	}
}

func (f *installScriptChecksumFixture) assertMutationCommandsNotRun(t *testing.T) {
	t.Helper()
	for _, path := range []string{f.tarLog, f.installLog} {
		if _, err := os.Stat(path); err == nil {
			t.Errorf("mutation command ran; found %s", path)
		} else if !os.IsNotExist(err) {
			t.Fatalf("stat mutation log %s: %v", path, err)
		}
	}
}

func (f *installScriptChecksumFixture) assertMutationCommandsRun(t *testing.T) {
	t.Helper()
	for _, path := range []string{f.tarLog, f.installLog} {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("mutation command did not run; stat %s: %v", path, err)
		}
	}
}
