package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestACPAdapterInstallHappyIdempotentAndDoctor(t *testing.T) {
	stateRoot := filepath.Join(t.TempDir(), "state")
	artifact := writeACPAdapterInstallArtifact(t, nil)
	request := newACPAdapterInstallTestRequest(t, stateRoot, artifact)

	first, err := installACPAdapter(request)
	if err != nil {
		t.Fatal(err)
	}
	if first.BundleDigest == "" || first.Bundle.BundleRoot == "" || first.Bundle.Argv[0] != filepath.Join(first.Bundle.BundleRoot, "codex-acp") {
		t.Fatalf("install receipt is incomplete: %#v", first)
	}
	t.Cleanup(func() { _ = os.Chmod(first.Bundle.BundleRoot, 0o700) })
	if !strings.HasPrefix(first.BundleDigest, "sha256:") || first.BundleDigest == first.FinalRootDigest {
		t.Fatalf("content and path-bound identities were not distinct: %#v", first)
	}
	if first.SourceVerification != acpAdapterCallerMetadataStatus || first.PublisherVerification != acpAdapterCallerMetadataStatus {
		t.Fatalf("caller metadata labels = %#v", first)
	}
	second, err := installACPAdapter(request)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(second, first) {
		t.Fatalf("idempotent receipt drift:\nfirst=%#v\nsecond=%#v", first, second)
	}
	for _, path := range []string{first.Bundle.BundleRoot, filepath.Join(first.Bundle.BundleRoot, "codex-acp")} {
		info, err := os.Lstat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o500 {
			t.Fatalf("sealed mode %s = %#o, want 0500", path, info.Mode().Perm())
		}
	}

	t.Setenv("OPENAI_API_KEY", "this-value-must-never-appear")
	report, err := doctorACPAdapter(ACPAdapterDoctorRequest{StateRoot: stateRoot, BundleDigest: first.BundleDigest, AuthSource: string(CodexACPAuthOpenAIAPIKey)})
	if err != nil {
		t.Fatal(err)
	}
	if !report.Installed || report.Configured || !report.AuthSourcePresent || report.Authenticated || report.Integrity != "valid" {
		t.Fatalf("doctor report = %#v", report)
	}
	rawReceipt, err := os.ReadFile(filepath.Join(stateRoot, "acp-adapters", "receipts", acpAdapterInstallDigestName(first.BundleDigest)+".json"))
	if err != nil {
		t.Fatal(err)
	}
	rawReport, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{string(rawReceipt), string(rawReport)} {
		if strings.Contains(raw, "this-value-must-never-appear") || strings.Contains(raw, artifact) {
			t.Fatalf("secret or local artifact path leaked: %s", raw)
		}
	}
}

func TestACPAdapterInstallRecoveryRecreatesReceiptForExactFinalRoot(t *testing.T) {
	stateRoot := filepath.Join(t.TempDir(), "state")
	artifact := writeACPAdapterInstallArtifact(t, nil)
	request := newACPAdapterInstallTestRequest(t, stateRoot, artifact)
	first, err := installACPAdapter(request)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = makeACPAdapterBundleWritable(first.Bundle.BundleRoot) })
	receiptPath := filepath.Join(stateRoot, "acp-adapters", "receipts", acpAdapterInstallDigestName(first.BundleDigest)+".json")
	if err := os.Remove(receiptPath); err != nil {
		t.Fatal(err)
	}
	recovered, err := installACPAdapter(request)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(recovered, first) {
		t.Fatalf("recovered receipt drift:\nfirst=%#v\nrecovered=%#v", first, recovered)
	}
	if _, err := os.Lstat(receiptPath); err != nil {
		t.Fatalf("receipt was not recreated: %v", err)
	}
}

func TestACPAdapterInstallRecoveryRejectsDivergentFinalRoot(t *testing.T) {
	stateRoot := filepath.Join(t.TempDir(), "state")
	artifact := writeACPAdapterInstallArtifact(t, nil)
	request := newACPAdapterInstallTestRequest(t, stateRoot, artifact)
	first, err := installACPAdapter(request)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = makeACPAdapterBundleWritable(first.Bundle.BundleRoot) })
	receiptPath := filepath.Join(stateRoot, "acp-adapters", "receipts", acpAdapterInstallDigestName(first.BundleDigest)+".json")
	if err := os.Remove(receiptPath); err != nil {
		t.Fatal(err)
	}
	if err := makeACPAdapterBundleWritable(first.Bundle.BundleRoot); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(first.Bundle.Argv[0], []byte("divergent adapter\n"), 0o500); err != nil {
		t.Fatal(err)
	}
	if err := sealACPAdapterInstallDirectory(first.Bundle.BundleRoot); err != nil {
		t.Fatal(err)
	}
	if _, err := installACPAdapter(request); err == nil || !strings.Contains(err.Error(), "receipt-less ACP adapter final root diverges") {
		t.Fatalf("divergent receipt-less root error = %v", err)
	}
	if _, err := os.Lstat(receiptPath); !os.IsNotExist(err) {
		t.Fatalf("divergent root created a receipt: %v", err)
	}
}

func TestACPAdapterInstallRefusesWrongDigestSymlinkAndHardlink(t *testing.T) {
	stateRoot := filepath.Join(t.TempDir(), "state")
	artifact := writeACPAdapterInstallArtifact(t, nil)
	request := newACPAdapterInstallTestRequest(t, stateRoot, artifact)
	request.ArtifactSHA256 = "sha256:" + strings.Repeat("0", 64)
	if _, err := installACPAdapter(request); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("wrong digest error = %v", err)
	}
	if report, err := doctorACPAdapter(ACPAdapterDoctorRequest{StateRoot: stateRoot, BundleDigest: request.ArtifactSHA256}); err != nil || report.Installed || report.Configured {
		t.Fatalf("failed stage was advertised: report=%#v err=%v", report, err)
	}

	symlink := filepath.Join(t.TempDir(), "adapter-link")
	if err := os.Symlink(artifact, symlink); err != nil {
		t.Fatal(err)
	}
	request = newACPAdapterInstallTestRequest(t, filepath.Join(t.TempDir(), "state"), artifact)
	request.ArtifactPath = symlink
	if _, err := installACPAdapter(request); err == nil || !strings.Contains(err.Error(), "non-symlink") {
		t.Fatalf("symlink error = %v", err)
	}

	request = newACPAdapterInstallTestRequest(t, filepath.Join(t.TempDir(), "state"), artifact)
	hardlink := filepath.Join(t.TempDir(), "adapter-hardlink")
	if err := os.Link(artifact, hardlink); err != nil {
		t.Fatal(err)
	}
	request.ArtifactPath = hardlink
	if _, err := installACPAdapter(request); err == nil || !strings.Contains(err.Error(), "hard links") {
		t.Fatalf("hardlink error = %v", err)
	}
}

func TestACPAdapterInstallRefusesMalformedWrongArchitectureAndSensitiveSourceURL(t *testing.T) {
	stateRoot := filepath.Join(t.TempDir(), "state")
	malformed := filepath.Join(t.TempDir(), "malformed")
	if err := os.WriteFile(malformed, []byte{0x7f, 'E', 'L', 'F', 1}, 0o500); err != nil {
		t.Fatal(err)
	}
	request := ACPAdapterInstallRequest{StateRoot: stateRoot, Provider: acpAdapterInstallProvider, ArtifactPath: malformed, Version: "1.2.3", ArtifactSHA256: "sha256:" + strings.Repeat("a", 64), SourceURL: "https://example.test/release", Publisher: acpAdapterInstallPublisher}
	if _, err := installACPAdapter(request); err == nil || !strings.Contains(err.Error(), "native") {
		t.Fatalf("malformed native error = %v", err)
	}
	artifact := writeACPAdapterInstallArtifact(t, nil)
	request = newACPAdapterInstallTestRequest(t, stateRoot, artifact)
	request.SourceURL = "https://token@example.test/release?secret=value#fragment"
	if _, err := installACPAdapter(request); err == nil || !strings.Contains(err.Error(), "source-url") {
		t.Fatalf("sensitive source URL error = %v", err)
	}
	// The test binary is host-native. Flipping a CPU-header byte makes its
	// Mach-O/ELF architecture unacceptable without executing it.
	raw, err := os.ReadFile(artifact)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) < 8 {
		t.Fatal("host executable header is unexpectedly short")
	}
	if runtime.GOOS == "linux" {
		raw[18] ^= 1 // ELF e_machine
	} else {
		raw[4] ^= 1 // Mach-O cputype
	}
	wrong := filepath.Join(t.TempDir(), "wrong-arch")
	if err := os.WriteFile(wrong, raw, 0o500); err != nil {
		t.Fatal(err)
	}
	request = newACPAdapterInstallTestRequest(t, stateRoot, artifact)
	request.ArtifactPath = wrong
	if _, err := installACPAdapter(request); err == nil || !strings.Contains(err.Error(), "architecture") {
		t.Fatalf("wrong architecture error = %v", err)
	}
}

func TestACPAdapterDoctorDetectsTamperAndDoesNotCreateMissingState(t *testing.T) {
	stateRoot := filepath.Join(t.TempDir(), "state")
	missingDigest := "sha256:" + strings.Repeat("a", 64)
	before, err := doctorACPAdapter(ACPAdapterDoctorRequest{StateRoot: stateRoot, BundleDigest: missingDigest})
	if err != nil || before.Installed || before.Configured || before.Authenticated {
		t.Fatalf("missing-state doctor = %#v err=%v", before, err)
	}
	if _, err := os.Lstat(filepath.Join(stateRoot, "acp-adapters")); !os.IsNotExist(err) {
		t.Fatalf("doctor created state: %v", err)
	}

	artifact := writeACPAdapterInstallArtifact(t, nil)
	receipt, err := installACPAdapter(newACPAdapterInstallTestRequest(t, stateRoot, artifact))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(receipt.Bundle.BundleRoot, 0o700) })
	if err := os.Chmod(receipt.Bundle.BundleRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(receipt.Bundle.Argv[0], 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(receipt.Bundle.Argv[0], []byte{0x7f, 'E', 'L', 'F', 8}, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(receipt.Bundle.Argv[0], 0o500); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(receipt.Bundle.BundleRoot, 0o500); err != nil {
		t.Fatal(err)
	}
	report, err := doctorACPAdapter(ACPAdapterDoctorRequest{StateRoot: stateRoot, BundleDigest: receipt.BundleDigest})
	if err != nil {
		t.Fatal(err)
	}
	if !report.Installed || report.Configured || report.Integrity != "invalid" || report.Authenticated || report.ValidationError == "" {
		t.Fatalf("tamper report = %#v", report)
	}
}

func TestACPAdapterInstallIsLocalOnlyAndCLIIsAdvertised(t *testing.T) {
	raw, err := os.ReadFile("acp_adapter_install.go")
	if err != nil {
		t.Fatal(err)
	}
	runtimeSource := strings.Split(string(raw), "func printACPAdapterHelp()")[0]
	for _, forbidden := range []string{"exec.Command", "http.Get", "npx", "npm", "Minisign", "OpenPGP"} {
		if strings.Contains(runtimeSource, forbidden) {
			t.Fatalf("installer unexpectedly contains %q", forbidden)
		}
	}
	command, args := parseCLI([]string{"tusker", "acp", "doctor", "--bundle-digest", "sha256:" + strings.Repeat("b", 64), "--json"})
	if command != "acp doctor" || !args.Bool("json") {
		t.Fatalf("parseCLI = %q %#v", command, args)
	}
	manifest, err := buildCapabilitiesManifest(nil, "")
	if err != nil {
		t.Fatal(err)
	}
	capability, ok := capabilityCommandNamed(manifest.Commands, "acp")
	if !ok || len(capability.Flags) != 0 || !containsString(capability.Subcommands, "install") || !containsString(capability.Subcommands, "doctor") {
		t.Fatalf("acp capability = %#v", capability)
	}
	installCapability, installOK := capabilityCommandNamed(manifest.Commands, "acp install")
	doctorCapability, doctorOK := capabilityCommandNamed(manifest.Commands, "acp doctor")
	if !installOK || !doctorOK || !containsString(installCapability.Flags, "--artifact") || containsString(installCapability.Flags, "--auth-source") || !containsString(doctorCapability.Flags, "--bundle-digest") || containsString(doctorCapability.Flags, "--artifact") {
		t.Fatalf("per-subcommand capability flags install=%#v doctor=%#v", installCapability, doctorCapability)
	}
	if _, err := runInner("acp", Args{"unexpected": "true"}); err == nil {
		t.Fatal("root acp accepted arbitrary args")
	}
	if err := validateACPAdapterCommandArgs(Args{"json": "true", "artifact": "/x"}, "json", "bundle-digest"); err == nil {
		t.Fatal("cross-subcommand flag was accepted")
	}
	if err := validateACPAdapterCommandArgs(Args{"json": "true", "_pos0": "extra"}, "json"); err == nil {
		t.Fatal("positional argument was accepted")
	}
	oversized := filepath.Join(t.TempDir(), "receipt")
	if err := os.WriteFile(oversized, []byte(strings.Repeat("x", int(acpAdapterInstallMaxReceiptBytes)+1)), 0o400); err != nil {
		t.Fatal(err)
	}
	if _, _, err := readACPAdapterInstallReceipt(oversized); err == nil || !strings.Contains(err.Error(), "size") {
		t.Fatalf("oversized receipt error = %v", err)
	}
}

func newACPAdapterInstallTestRequest(t *testing.T, stateRoot, artifact string) ACPAdapterInstallRequest {
	t.Helper()
	digest, err := hashACPAdapterInstallSource(artifact)
	if err != nil {
		t.Fatal(err)
	}
	return ACPAdapterInstallRequest{StateRoot: stateRoot, Provider: acpAdapterInstallProvider, ArtifactPath: artifact, Version: "1.2.3", ArtifactSHA256: digest, SourceURL: "https://example.test/releases/codex-acp", Publisher: acpAdapterInstallPublisher}
}

func writeACPAdapterInstallArtifact(t *testing.T, contents []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "codex-acp")
	if contents == nil {
		source, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		contents, err = os.ReadFile(source)
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(path, contents, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o500); err != nil {
		t.Fatal(err)
	}
	return path
}
