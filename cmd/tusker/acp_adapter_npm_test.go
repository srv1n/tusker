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

func TestPackageACPAdapterNPMPublishesCompleteVerifiedBundle(t *testing.T) {
	prefix, platform := newACPAdapterNPMFixture(t)
	stateRoot := filepath.Join(t.TempDir(), "state")
	first, err := PackageACPAdapterNPM(ACPAdapterNPMPackageRequest{StateRoot: stateRoot, Prefix: prefix})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = makeACPAdapterBundleWritable(first.Bundle.BundleRoot) })
	second, err := PackageACPAdapterNPM(ACPAdapterNPMPackageRequest{StateRoot: stateRoot, Prefix: prefix})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("idempotent npm package receipt drift:\nfirst=%#v\nsecond=%#v", first, second)
	}
	if first.Schema != acpAdapterNPMReceiptSchema || first.AdapterVersion != ACPAdapterNPMAdapterVersion || first.CodexVersion != ACPAdapterNPMCodexVersion || first.PlatformPackage != platform {
		t.Fatalf("receipt identity is incomplete: %#v", first)
	}
	wantArgv := []string{
		filepath.Join(first.Bundle.BundleRoot, "bin", "node"),
		filepath.Join(first.Bundle.BundleRoot, "node_modules", "@agentclientprotocol", "codex-acp", "dist", "index.js"),
	}
	if !reflect.DeepEqual(first.Bundle.Argv, wantArgv) || first.Bundle.LaunchKind != ACPAdapterBundleLaunchInterpreter {
		t.Fatalf("interpreter argv drift: %#v", first.Bundle)
	}
	wantAssets := []string{
		"bin/node",
		"node_modules/@agentclientprotocol/codex-acp/dist/index.js",
		"node_modules/@agentclientprotocol/codex-acp/package.json",
		"node_modules/@agentclientprotocol/sdk/package.json",
		"node_modules/@agentclientprotocol/sdk/runtime.js",
		"node_modules/" + platform + "/package.json",
		"node_modules/" + platform + "/vendor/test-target/bin/codex",
		"node_modules/@openai/codex/bin/codex.js",
		"node_modules/@openai/codex/package.json",
		"node_modules/zod/index.js",
		"node_modules/zod/package.json",
	}
	gotAssets := make([]string, len(first.Bundle.Assets))
	runtimeExecutable := "node_modules/" + platform + "/vendor/test-target/bin/codex"
	runtimeExecutableBound := false
	for index, asset := range first.Bundle.Assets {
		gotAssets[index] = asset.Path
		if asset.Path == runtimeExecutable && asset.Role == "runtime_executable" {
			runtimeExecutableBound = true
		}
	}
	if !reflect.DeepEqual(gotAssets, wantAssets) {
		t.Fatalf("complete manifest assets = %#v, want %#v", gotAssets, wantAssets)
	}
	if !runtimeExecutableBound {
		t.Fatal("platform Codex binary was not bound as a runtime executable")
	}
	if info, statErr := os.Stat(filepath.Join(first.Bundle.BundleRoot, filepath.FromSlash(runtimeExecutable))); statErr != nil || info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("platform Codex binary is not launchable after sealing: mode=%v err=%v", infoMode(info), statErr)
	}
	for _, path := range append([]string{first.Bundle.BundleRoot}, wantArgv...) {
		info, statErr := os.Lstat(path)
		if statErr != nil || info.Mode().Perm()&0o222 != 0 {
			t.Fatalf("published path is not sealed: %s mode=%v err=%v", path, infoMode(info), statErr)
		}
	}
	if err := RevalidateACPAdapterBundleReceipt(
		acpAdapterNPMValidationRequest(first.Bundle.BundleRoot, first.ManifestSHA256, first.FinalRootDigest),
		first.Bundle,
	); err != nil {
		t.Fatalf("published receipt is incompatible with verifier: %v", err)
	}
}

func TestACPAdapterDoctorRecognizesPackagedNPMBundle(t *testing.T) {
	prefix, _ := newACPAdapterNPMFixture(t)
	stateRoot := filepath.Join(t.TempDir(), "state")
	receipt, err := PackageACPAdapterNPM(ACPAdapterNPMPackageRequest{StateRoot: stateRoot, Prefix: prefix})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = makeACPAdapterBundleWritable(receipt.Bundle.BundleRoot) })
	codexHome := t.TempDir()
	t.Setenv("CODEX_HOME", codexHome)
	report, err := doctorACPAdapter(ACPAdapterDoctorRequest{
		StateRoot: stateRoot, BundleDigest: receipt.BundleDigest, AuthSource: string(CodexACPAuthChatGPTSession),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !report.Installed || report.Integrity != "valid" || !report.AuthSourcePresent || report.Configured || report.Authenticated || report.ValidationError != "" {
		t.Fatalf("npm package doctor report = %#v", report)
	}
}

func TestPackageACPAdapterNPMRecoveryRecreatesReceiptForExactFinalRoot(t *testing.T) {
	prefix, _ := newACPAdapterNPMFixture(t)
	stateRoot := filepath.Join(t.TempDir(), "state")
	first, err := PackageACPAdapterNPM(ACPAdapterNPMPackageRequest{StateRoot: stateRoot, Prefix: prefix})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = makeACPAdapterBundleWritable(first.Bundle.BundleRoot) })
	receiptPath := filepath.Join(stateRoot, "acp-adapters", "receipts", "npm-"+acpAdapterInstallDigestName(first.BundleDigest)+".json")
	if err := os.Remove(receiptPath); err != nil {
		t.Fatal(err)
	}
	recovered, err := PackageACPAdapterNPM(ACPAdapterNPMPackageRequest{StateRoot: stateRoot, Prefix: prefix})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(recovered, first) {
		t.Fatalf("recovered npm receipt drift:\nfirst=%#v\nrecovered=%#v", first, recovered)
	}
	if _, err := os.Lstat(receiptPath); err != nil {
		t.Fatalf("npm receipt was not recreated: %v", err)
	}
}

func TestPackageACPAdapterNPMRecoveryRejectsDivergentFinalRoot(t *testing.T) {
	prefix, _ := newACPAdapterNPMFixture(t)
	stateRoot := filepath.Join(t.TempDir(), "state")
	first, err := PackageACPAdapterNPM(ACPAdapterNPMPackageRequest{StateRoot: stateRoot, Prefix: prefix})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = makeACPAdapterBundleWritable(first.Bundle.BundleRoot) })
	receiptPath := filepath.Join(stateRoot, "acp-adapters", "receipts", "npm-"+acpAdapterInstallDigestName(first.BundleDigest)+".json")
	if err := os.Remove(receiptPath); err != nil {
		t.Fatal(err)
	}
	if err := makeACPAdapterBundleWritable(first.Bundle.BundleRoot); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(first.Bundle.Argv[1], []byte("divergent entrypoint\n"), 0o400); err != nil {
		t.Fatal(err)
	}
	if err := sealACPAdapterNPMTree(first.Bundle.BundleRoot); err != nil {
		t.Fatal(err)
	}
	if _, err := PackageACPAdapterNPM(ACPAdapterNPMPackageRequest{StateRoot: stateRoot, Prefix: prefix}); err == nil || !strings.Contains(err.Error(), "receipt-less npm ACP adapter final root diverges") {
		t.Fatalf("divergent receipt-less npm root error = %v", err)
	}
	if _, err := os.Lstat(receiptPath); !os.IsNotExist(err) {
		t.Fatalf("divergent npm root created a receipt: %v", err)
	}
}

func TestPackageACPAdapterNPMRejectsUntrustedPackageShapes(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(t *testing.T, prefix, platform string)
		want   string
	}{
		{
			name: "adapter version drift",
			mutate: func(t *testing.T, prefix, _ string) {
				writeACPAdapterNPMFixturePackage(t, acpAdapterNPMPackageRoot(prefix, acpAdapterNPMPackageName), map[string]any{
					"name": acpAdapterNPMPackageName, "version": "1.1.13", "bin": map[string]string{"codex-acp": "dist/index.js"},
				})
			},
			want: "exact version 1.1.14",
		},
		{
			name: "codex version drift",
			mutate: func(t *testing.T, prefix, platform string) {
				writeACPAdapterNPMFixturePackage(t, acpAdapterNPMPackageRoot(prefix, acpAdapterNPMCodexName), map[string]any{
					"name": acpAdapterNPMCodexName, "version": "0.146.0", "optionalDependencies": map[string]string{platform: "npm:@openai/codex@" + acpAdapterNPMPlatformVersion(platform)},
				})
			},
			want: "exact version 0.147.0",
		},
		{
			name: "platform version drift",
			mutate: func(t *testing.T, prefix, platform string) {
				writeACPAdapterNPMFixturePackage(t, acpAdapterNPMPackageRoot(prefix, platform), map[string]any{"name": acpAdapterNPMCodexName, "version": "0.146.0"})
			},
			want: "exact version 0.147.0",
		},
		{
			name: "entrypoint traversal",
			mutate: func(t *testing.T, prefix, _ string) {
				writeACPAdapterNPMFixturePackage(t, acpAdapterNPMPackageRoot(prefix, acpAdapterNPMPackageName), map[string]any{
					"name": acpAdapterNPMPackageName, "version": ACPAdapterNPMAdapterVersion, "bin": map[string]string{"codex-acp": "../escape.js"},
				})
			},
			want: "package-relative",
		},
		{
			name: "package symlink",
			mutate: func(t *testing.T, prefix, _ string) {
				path := filepath.Join(acpAdapterNPMPackageRoot(prefix, acpAdapterNPMPackageName), "dist", "index.js")
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Join(prefix, "outside.js"), path); err != nil {
					t.Fatal(err)
				}
			},
			want: "contains symlink",
		},
		{
			name: "lifecycle script",
			mutate: func(t *testing.T, prefix, _ string) {
				writeACPAdapterNPMFixturePackage(t, acpAdapterNPMPackageRoot(prefix, acpAdapterNPMPackageName), map[string]any{
					"name": acpAdapterNPMPackageName, "version": ACPAdapterNPMAdapterVersion, "bin": map[string]string{"codex-acp": "dist/index.js"}, "scripts": map[string]string{"postinstall": "node setup.js"},
				})
			},
			want: "forbidden npm lifecycle script",
		},
		{
			name: "ambient platform resolution",
			mutate: func(t *testing.T, prefix, platform string) {
				if err := os.RemoveAll(acpAdapterNPMPackageRoot(prefix, platform)); err != nil {
					t.Fatal(err)
				}
			},
			want: "found 0",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			prefix, platform := newACPAdapterNPMFixture(t)
			test.mutate(t, prefix, platform)
			stateRoot := filepath.Join(t.TempDir(), "state")
			_, err := PackageACPAdapterNPM(ACPAdapterNPMPackageRequest{StateRoot: stateRoot, Prefix: prefix})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want substring %q", err, test.want)
			}
			if _, statErr := os.Lstat(filepath.Join(stateRoot, "acp-adapters")); !os.IsNotExist(statErr) {
				t.Fatalf("rejected source mutated installation state: %v", statErr)
			}
		})
	}
}

func newACPAdapterNPMFixture(t *testing.T) (string, string) {
	t.Helper()
	prefix := filepath.Join(t.TempDir(), "npm-prefix")
	if err := os.MkdirAll(filepath.Join(prefix, "bin"), 0o700); err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	writeACPAdapterNPMFixtureFile(t, executable, filepath.Join(prefix, "bin", "node"), 0o500)

	platforms, err := acpAdapterNPMPlatformCandidates()
	if err != nil {
		t.Fatal(err)
	}
	platform := platforms[0]
	adapterRoot := acpAdapterNPMPackageRoot(prefix, acpAdapterNPMPackageName)
	writeACPAdapterNPMFixturePackage(t, adapterRoot, map[string]any{
		"name": acpAdapterNPMPackageName, "version": ACPAdapterNPMAdapterVersion,
		"bin": map[string]string{"codex-acp": "dist/index.js"}, "scripts": map[string]string{"test": "never-run"},
		"dependencies": map[string]string{"@openai/codex": "^0.147.0", "@agentclientprotocol/sdk": "^1.3.0"},
	})
	writeACPAdapterNPMFixtureFile(t, "", filepath.Join(adapterRoot, "dist", "index.js"), 0o600)
	codexRoot := acpAdapterNPMPackageRoot(prefix, acpAdapterNPMCodexName)
	writeACPAdapterNPMFixturePackage(t, codexRoot, map[string]any{
		"name": acpAdapterNPMCodexName, "version": ACPAdapterNPMCodexVersion,
		"optionalDependencies": map[string]string{platform: "npm:@openai/codex@" + acpAdapterNPMPlatformVersion(platform)},
	})
	writeACPAdapterNPMFixtureFile(t, "", filepath.Join(codexRoot, "bin", "codex.js"), 0o600)
	platformRoot := acpAdapterNPMPackageRoot(prefix, platform)
	writeACPAdapterNPMFixturePackage(t, platformRoot, map[string]any{"name": acpAdapterNPMCodexName, "version": acpAdapterNPMPlatformVersion(platform)})
	writeACPAdapterNPMFixtureFile(t, executable, filepath.Join(platformRoot, "vendor", "test-target", "bin", "codex"), 0o500)
	sdkRoot := acpAdapterNPMPackageRoot(prefix, "@agentclientprotocol/sdk")
	writeACPAdapterNPMFixturePackage(t, sdkRoot, map[string]any{"name": "@agentclientprotocol/sdk", "version": "1.3.1", "dependencies": map[string]string{"zod": "^4.0.0"}})
	writeACPAdapterNPMFixtureFile(t, "", filepath.Join(sdkRoot, "runtime.js"), 0o600)
	zodRoot := acpAdapterNPMPackageRoot(prefix, "zod")
	writeACPAdapterNPMFixturePackage(t, zodRoot, map[string]any{"name": "zod", "version": "4.1.0"})
	writeACPAdapterNPMFixtureFile(t, "", filepath.Join(zodRoot, "index.js"), 0o600)
	return prefix, platform
}

func writeACPAdapterNPMFixturePackage(t *testing.T, root string, value map[string]any) {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "package.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeACPAdapterNPMFixtureFile(t *testing.T, source, destination string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		t.Fatal(err)
	}
	if source == "" {
		if err := os.WriteFile(destination, []byte("fixture\n"), mode); err != nil {
			t.Fatal(err)
		}
		return
	}
	input, err := os.Open(source)
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := output.ReadFrom(input); err != nil {
		_ = output.Close()
		t.Fatal(err)
	}
	if err := output.Close(); err != nil {
		t.Fatal(err)
	}
}

func infoMode(info os.FileInfo) os.FileMode {
	if info == nil {
		return 0
	}
	return info.Mode().Perm()
}

func TestACPAdapterNPMPlatformMappingMatchesHost(t *testing.T) {
	candidates, err := acpAdapterNPMPlatformCandidates()
	if err != nil {
		t.Fatal(err)
	}
	for _, candidate := range candidates {
		if !strings.HasPrefix(candidate, "@openai/codex-") || !strings.Contains(candidate, runtime.GOOS) {
			t.Fatalf("platform candidate %q does not match host %s/%s", candidate, runtime.GOOS, runtime.GOARCH)
		}
	}
}
