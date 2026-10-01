package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestACPAdapterDoctorReportsMissingStateWithoutCreatingIt(t *testing.T) {
	stateRoot := filepath.Join(t.TempDir(), "state")
	missingDigest := "sha256:" + strings.Repeat("a", 64)
	before, err := doctorACPAdapter(ACPAdapterDoctorRequest{StateRoot: stateRoot, BundleDigest: missingDigest})
	if err != nil || before.Installed || before.Configured || before.Authenticated {
		t.Fatalf("missing-state doctor = %#v err=%v", before, err)
	}
	if _, err := os.Lstat(filepath.Join(stateRoot, "acp-adapters")); !os.IsNotExist(err) {
		t.Fatalf("doctor created state: %v", err)
	}

}

func TestACPAdapterInstallIsLocalOnlyAndCLIIsRetired(t *testing.T) {
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
	for _, name := range []string{"acp", "acp install", "acp doctor"} {
		if capability, ok := capabilityCommandNamed(manifest.Commands, name); ok {
			t.Fatalf("retiring ACP command %q is advertised: %#v", name, capability)
		}
	}
	if code, err := runInner("acp install", Args{}); err != nil || code == 0 {
		t.Fatalf("retired acp install result: code=%d err=%v", code, err)
	}
	if code, _ := runInner("acp", Args{"_pos0": "unexpected"}); code == 0 {
		t.Fatal("root acp accepted an arbitrary subcommand")
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
