package main

// This file packages an already-installed npm prefix into the sealed ACP
// bundle format. It deliberately has no npm, shell, network, PATH, lifecycle,
// or module-resolution hook: every input is reached below one exact prefix and
// every byte copied into the bundle is listed in the canonical manifest.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	ACPAdapterNPMAdapterVersion = "1.1.14"
	ACPAdapterNPMCodexVersion   = "0.147.0"

	acpAdapterNPMReceiptSchema  = "tusker.acp-adapter-npm-package-receipt/v1"
	acpAdapterNPMIdentitySchema = "tusker.acp-adapter-npm-package-identity/v1"
	acpAdapterNPMManifestPath   = "manifest.json"
)

// ACPAdapterNPMPackageReceipt is deterministic for an exact input tree and
// destination state root. Bundle is the existing verifier's complete content
// receipt and is suitable for immediate pre-spawn revalidation.
type ACPAdapterNPMPackageReceipt struct {
	Schema          string                              `json:"schema"`
	BundleDigest    string                              `json:"bundle_digest"`
	NodeSHA256      string                              `json:"node_sha256"`
	AdapterVersion  string                              `json:"adapter_version"`
	CodexVersion    string                              `json:"codex_version"`
	PlatformPackage string                              `json:"platform_package"`
	PlatformVersion string                              `json:"platform_version"`
	ManifestSHA256  string                              `json:"manifest_sha256"`
	FinalRootDigest string                              `json:"final_root_digest"`
	Bundle          ACPAdapterBundleVerificationReceipt `json:"bundle"`
}

type acpAdapterNPMIdentity struct {
	Schema          string                  `json:"schema"`
	GOOS            string                  `json:"goos"`
	GOARCH          string                  `json:"goarch"`
	AdapterVersion  string                  `json:"adapter_version"`
	CodexVersion    string                  `json:"codex_version"`
	PlatformPackage string                  `json:"platform_package"`
	PlatformVersion string                  `json:"platform_version"`
	Files           []ACPAdapterBundleAsset `json:"files"`
}

func acpAdapterNPMPlatformCandidates() ([]string, error) {
	suffix := ""
	switch runtime.GOOS + "/" + runtime.GOARCH {
	case "darwin/arm64":
		suffix = "darwin-arm64"
	case "darwin/amd64":
		suffix = "darwin-x64"
	case "linux/arm64":
		suffix = "linux-arm64"
	case "linux/amd64":
		suffix = "linux-x64"
	default:
		return nil, fmt.Errorf("unsupported npm ACP adapter host platform %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	return []string{"@openai/codex-" + suffix}, nil
}

func acpAdapterNPMPlatformVersion(packageName string) string {
	return ACPAdapterNPMCodexVersion + strings.TrimPrefix(packageName, "@openai/codex")
}

func acpAdapterNPMValidationRequest(root, manifestDigest, finalRootDigest string) ACPAdapterBundleValidationRequest {
	return ACPAdapterBundleValidationRequest{
		BundleRoot: root, ManifestPath: acpAdapterNPMManifestPath, ExpectedManifestSHA256: manifestDigest,
		ExpectedDescriptor: ACPAdapterBundleDescriptorPolicy{Provider: acpAdapterInstallProvider, Adapter: acpAdapterInstallAdapter, Version: ACPAdapterNPMAdapterVersion, LaunchKind: ACPAdapterBundleLaunchInterpreter},
		ExpectedFinalRoot:  root, ExpectedFinalRootDigest: finalRootDigest, TrustCurrentUserBoundary: true,
		ProviderAllowed: func(provider string) bool { return provider == acpAdapterInstallProvider },
	}
}

func readACPAdapterNPMPackageReceipt(path string) (ACPAdapterNPMPackageReceipt, bool, error) {
	raw, exists, err := readACPAdapterInstallReceiptBytes(path)
	if err != nil || !exists {
		return ACPAdapterNPMPackageReceipt{}, exists, err
	}
	var receipt ACPAdapterNPMPackageReceipt
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&receipt); err != nil {
		return receipt, true, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return receipt, true, fmt.Errorf("npm ACP adapter receipt has trailing JSON")
	}
	canonical, err := json.Marshal(receipt)
	if err != nil || !bytes.Equal(raw, canonical) {
		return receipt, true, fmt.Errorf("npm ACP adapter receipt is not exact canonical JSON")
	}
	return receipt, true, nil
}

// validatePackagedACPAdapterInstallation lets the common read-only doctor
// recognize the exact sealed runtime created by `acp setup`. The native local
// installer remains independent from package-manager execution or discovery.
func validatePackagedACPAdapterInstallation(root, bundleDigest string) (bool, error) {
	name := "npm-" + acpAdapterInstallDigestName(bundleDigest)
	receipt, exists, err := readACPAdapterNPMPackageReceipt(filepath.Join(root, "receipts", name+".json"))
	if err != nil || !exists {
		return exists, err
	}
	if err := validateACPAdapterNPMPackageReceipt(receipt, bundleDigest, filepath.Join(root, "bundles", name)); err != nil {
		return true, err
	}
	return true, nil
}

func validateACPAdapterNPMPackageReceipt(receipt ACPAdapterNPMPackageReceipt, bundleDigest, finalRoot string) error {
	if receipt.Schema != acpAdapterNPMReceiptSchema || receipt.BundleDigest != bundleDigest || receipt.AdapterVersion != ACPAdapterNPMAdapterVersion || receipt.CodexVersion != ACPAdapterNPMCodexVersion || !validACPAdapterBundleDigest(receipt.NodeSHA256) || !validACPAdapterBundleDigest(receipt.ManifestSHA256) || !validACPAdapterBundleDigest(receipt.FinalRootDigest) {
		return fmt.Errorf("npm ACP adapter receipt identity is invalid")
	}
	candidates, err := acpAdapterNPMPlatformCandidates()
	if err != nil || !containsString(candidates, receipt.PlatformPackage) || receipt.PlatformVersion != acpAdapterNPMPlatformVersion(receipt.PlatformPackage) {
		return fmt.Errorf("npm ACP adapter receipt platform identity is invalid")
	}
	identity := acpAdapterNPMIdentity{
		Schema: acpAdapterNPMIdentitySchema, GOOS: runtime.GOOS, GOARCH: runtime.GOARCH,
		AdapterVersion: receipt.AdapterVersion, CodexVersion: receipt.CodexVersion,
		PlatformPackage: receipt.PlatformPackage, PlatformVersion: receipt.PlatformVersion,
		Files: receipt.Bundle.Assets,
	}
	identityRaw, err := json.Marshal(identity)
	if err != nil || acpAdapterBundleDigest(identityRaw) != receipt.BundleDigest {
		return fmt.Errorf("npm ACP adapter receipt source identity drift")
	}
	nodeBound := false
	for _, asset := range receipt.Bundle.Assets {
		if asset.Path == "bin/node" && asset.Role == "executable" && asset.SHA256 == receipt.NodeSHA256 {
			nodeBound = true
		}
	}
	if !nodeBound {
		return fmt.Errorf("npm ACP adapter receipt does not bind bundled Node")
	}
	expectedRootDigest, err := ACPAdapterBundleFinalRootDigest(finalRoot, receipt.ManifestSHA256)
	if err != nil || expectedRootDigest != receipt.FinalRootDigest {
		return fmt.Errorf("npm ACP adapter receipt final root binding drift")
	}
	if err := RevalidateACPAdapterBundleReceipt(acpAdapterNPMValidationRequest(finalRoot, receipt.ManifestSHA256, receipt.FinalRootDigest), receipt.Bundle); err != nil {
		return err
	}
	return nil
}
