package main

import (
	"archive/tar"
	"compress/gzip"
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
			fixture.assertInstalledVersion(t, installScriptTestVersion)
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

func TestInstallScriptRejectsMaliciousArchiveMembers(t *testing.T) {
	tests := []struct {
		name, member, link string
		kind               byte
	}{
		{name: "traversal", member: "../escape", kind: tar.TypeReg},
		{name: "absolute", member: "/tmp/escape", kind: tar.TypeReg},
		{name: "symlink", member: "tusker_v9.9.9_linux_amd64/tusker", link: "../../escape", kind: tar.TypeSymlink},
		{name: "hardlink", member: "tusker_v9.9.9_linux_amd64/tusker", link: "../../escape", kind: tar.TypeLink},
		{name: "device", member: "tusker_v9.9.9_linux_amd64/tusker", kind: tar.TypeChar},
		{name: "duplicate", member: "tusker_v9.9.9_linux_amd64/tusker", kind: 'D'},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture := newInstallScriptChecksumFixture(t, "Linux", "x86_64", "sha256sum", installScriptTestChecksum)
			fixture.writeMaliciousArchive(t, tt.member, tt.link, tt.kind)
			fixture.writeManifest(t, validInstallScriptManifest(fixture.asset))
			output, err := fixture.run(t)
			if err == nil {
				t.Fatalf("installer accepted malicious archive:\n%s", output)
			}
			fixture.assertInstalledBinary(t, "existing binary")
		})
	}
}

func TestInstallScriptSignatureAndPostSwapFailuresRollback(t *testing.T) {
	t.Run("signature", func(t *testing.T) {
		fixture := newInstallScriptChecksumFixture(t, "Linux", "x86_64", "sha256sum", installScriptTestChecksum)
		fixture.signatureFail = true
		fixture.writeManifest(t, validInstallScriptManifest(fixture.asset))
		if output, err := fixture.run(t); err == nil {
			t.Fatalf("installer accepted bad signature:\n%s", output)
		}
		fixture.assertInstalledBinary(t, "existing binary")
	})
	t.Run("post swap health", func(t *testing.T) {
		fixture := newInstallScriptChecksumFixture(t, "Linux", "x86_64", "sha256sum", installScriptTestChecksum)
		fixture.postSwapFail = true
		fixture.writeManifest(t, validInstallScriptManifest(fixture.asset))
		if output, err := fixture.run(t); err == nil {
			t.Fatalf("installer accepted failed post-swap health:\n%s", output)
		}
		fixture.assertInstalledBinary(t, "existing binary")
	})
	t.Run("fresh install post swap health", func(t *testing.T) {
		fixture := newInstallScriptChecksumFixture(t, "Linux", "x86_64", "sha256sum", installScriptTestChecksum)
		fixture.postSwapFail = true
		if err := os.Remove(filepath.Join(fixture.binDir, "tusker")); err != nil {
			t.Fatal(err)
		}
		fixture.writeManifest(t, validInstallScriptManifest(fixture.asset))
		if output, err := fixture.run(t); err == nil {
			t.Fatalf("installer accepted failed fresh-install health:\n%s", output)
		}
		if _, err := os.Stat(filepath.Join(fixture.binDir, "tusker")); !os.IsNotExist(err) {
			t.Fatalf("failed fresh install left a final binary: %v", err)
		}
	})
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
	signatureFail   bool
	postSwapFail    bool
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
	if err := os.WriteFile(fixture.newBinaryPath, []byte("#!/bin/sh\nif [ \"${1:-}\" = version ]; then n=0; if [ -f \"$FIXTURE_VERSION_CALL_LOG\" ]; then read -r n <\"$FIXTURE_VERSION_CALL_LOG\"; fi; n=$((n+1)); printf '%s\\n' \"$n\" >\"$FIXTURE_VERSION_CALL_LOG\"; version=v9.9.9; if [ \"${FIXTURE_POST_SWAP_FAIL:-0}\" = 1 ] && [ \"$n\" -gt 1 ]; then version=v0.0.0; fi; printf '{\"version\":\"%s\"}\\n' \"$version\"; exit 0; fi\nif [ \"${1:-}\" = install ] && [ \"${2:-}\" = --help ]; then printf '%s\\n' -- '--refresh-existing-user-skills'; exit 0; fi\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write verified binary: %v", err)
	}
	fixture.writeArchive(t)
	if err := os.WriteFile(filepath.Join(fixture.releaseDir, "MANIFEST.sha256.minisig"), []byte("fixture signature\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	fixture.writeCommands(t, checksumTool)
	return fixture
}

func (f *installScriptChecksumFixture) writeManifest(t *testing.T, contents string) {
	t.Helper()
	contents += fmt.Sprintf("%s  checksums.txt\n%s  provenance.json\n%s  sbom.cdx.json\n", installScriptOtherSum, installScriptOtherSum, installScriptOtherSum)
	if err := os.WriteFile(filepath.Join(f.releaseDir, "MANIFEST.sha256"), []byte(contents), 0o644); err != nil {
		t.Fatalf("write checksum manifest: %v", err)
	}
}

func (f *installScriptChecksumFixture) writeArchive(t *testing.T) {
	t.Helper()
	out, err := os.Create(filepath.Join(f.releaseDir, f.asset))
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(out)
	tw := tar.NewWriter(gz)
	write := func(name string, mode int64, body []byte, typeflag byte) {
		t.Helper()
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: mode, Size: int64(len(body)), Typeflag: typeflag}); err != nil {
			t.Fatal(err)
		}
		if len(body) > 0 {
			if _, err := tw.Write(body); err != nil {
				t.Fatal(err)
			}
		}
	}
	write(f.extractSubdir+"/", 0o755, nil, tar.TypeDir)
	binary, err := os.ReadFile(f.newBinaryPath)
	if err != nil {
		t.Fatal(err)
	}
	write(f.extractSubdir+"/tusker", 0o755, binary, tar.TypeReg)
	write(f.extractSubdir+"/README.md", 0o644, []byte("fixture\n"), tar.TypeReg)
	write(f.extractSubdir+"/LICENSE", 0o644, []byte("fixture\n"), tar.TypeReg)
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
}

func (f *installScriptChecksumFixture) writeMaliciousArchive(t *testing.T, name, link string, kind byte) {
	t.Helper()
	out, err := os.Create(filepath.Join(f.releaseDir, f.asset))
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(out)
	tw := tar.NewWriter(gz)
	write := func(h *tar.Header) {
		t.Helper()
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if h.Size > 0 {
			if _, err := tw.Write([]byte("x")); err != nil {
				t.Fatal(err)
			}
		}
	}
	headerKind := kind
	if kind == 'D' {
		headerKind = tar.TypeReg
	}
	size := int64(0)
	if headerKind == tar.TypeReg {
		size = 1
	}
	write(&tar.Header{Name: name, Linkname: link, Typeflag: headerKind, Mode: 0o755, Size: size})
	if kind == 'D' {
		write(&tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: 0o755, Size: 1})
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
}

func (f *installScriptChecksumFixture) writeCommands(t *testing.T, checksumTool string) {
	t.Helper()
	for _, name := range []string{"awk", "bash", "cp", "grep", "mkdir", "mktemp", "mv", "python3", "rm", "sync"} {
		path, err := exec.LookPath(name)
		if name == "python3" {
			if _, statErr := os.Stat("/usr/bin/python3"); statErr == nil {
				path, err = "/usr/bin/python3", nil
			}
		}
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
	realTar, err := exec.LookPath("tar")
	if err != nil {
		t.Fatal(err)
	}
	realInstall, err := exec.LookPath("install")
	if err != nil {
		t.Fatal(err)
	}
	writeInstallScriptFixtureCommand(t, f.fakeBin, "tar", fmt.Sprintf("#!/bin/sh\nprintf 'tar\\n' > \"$FIXTURE_TAR_LOG\"\nexec %q \"$@\"\n", realTar))
	writeInstallScriptFixtureCommand(t, f.fakeBin, "install", fmt.Sprintf("#!/bin/sh\nprintf 'install\\n' > \"$FIXTURE_INSTALL_LOG\"\nexec %q \"$@\"\n", realInstall))
	writeInstallScriptFixtureCommand(t, f.fakeBin, "minisign", "#!/bin/sh\n[ \"${FIXTURE_SIGNATURE_FAIL:-0}\" = 1 ] && exit 1\nexit 0\n")

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
		"TUSKER_INSTALL_TEST_MODE=1",
		"TUSKER_TEST_MINISIGN_PUBLIC_KEY=RWQfixture-public-key",
		"FIXTURE_RELEASE_DIR=" + f.releaseDir,
		"FIXTURE_UNAME_S=" + f.unameS,
		"FIXTURE_UNAME_M=" + f.unameM,
		"FIXTURE_ACTUAL_CHECKSUM=" + f.actualChecksum,
		"FIXTURE_CHECKSUM_TOOL_LOG=" + f.checksumToolLog,
		"FIXTURE_TAR_LOG=" + f.tarLog,
		"FIXTURE_INSTALL_LOG=" + f.installLog,
		"FIXTURE_EXTRACT_SUBDIR=" + f.extractSubdir,
		"FIXTURE_NEW_BINARY_PATH=" + f.newBinaryPath,
		fmt.Sprintf("FIXTURE_SIGNATURE_FAIL=%d", boolInt(f.signatureFail)),
		fmt.Sprintf("FIXTURE_POST_SWAP_FAIL=%d", boolInt(f.postSwapFail)),
		"FIXTURE_VERSION_CALL_LOG=" + filepath.Join(f.root, "version-calls"),
	}
	output, runErr := cmd.CombinedOutput()
	return string(output), runErr
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
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

func (f *installScriptChecksumFixture) assertInstalledVersion(t *testing.T, want string) {
	t.Helper()
	output, err := exec.Command(filepath.Join(f.binDir, "tusker"), "version", "--json").Output()
	if err != nil {
		t.Fatalf("run installed binary: %v", err)
	}
	if !strings.Contains(string(output), `"version":"`+want+`"`) {
		t.Fatalf("installed version output = %q, want %s", output, want)
	}
}

func (f *installScriptChecksumFixture) assertMutationCommandsNotRun(t *testing.T) {
	t.Helper()
	for _, path := range []string{f.installLog} {
		if _, err := os.Stat(path); err == nil {
			t.Errorf("mutation command ran; found %s", path)
		} else if !os.IsNotExist(err) {
			t.Fatalf("stat mutation log %s: %v", path, err)
		}
	}
}

func (f *installScriptChecksumFixture) assertMutationCommandsRun(t *testing.T) {
	t.Helper()
	for _, path := range []string{f.installLog} {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("mutation command did not run; stat %s: %v", path, err)
		}
	}
}
