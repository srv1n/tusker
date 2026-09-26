package main

// This is deliberately a local-artifact installer, not an ACP package
// manager. It performs no discovery, download, authentication, signature
// verification, or adapter launch. The caller supplies a sealed native binary
// and the release metadata whose authenticity it has established elsewhere.

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	acpAdapterInstallReceiptSchema  = "tusker.acp-adapter-install-receipt/v1"
	acpAdapterInstallIdentitySchema = "tusker.acp-adapter-install-identity/v1"
	acpAdapterInstallManifestPath   = "manifest.json"
	acpAdapterInstallAdapter        = "codex-acp"
	acpAdapterInstallProvider       = "codex"
	acpAdapterInstallPublisher      = "agentclientprotocol"
	acpAdapterCallerMetadataStatus  = "unverified_caller_metadata"
)

// ACPAdapterInstallReceipt is a non-secret, immutable receipt kept outside
// the sealed bundle. It makes a partial stage or a bare directory invisible to
// doctor and to future install idempotence checks.
type ACPAdapterInstallReceipt struct {
	Schema                string                              `json:"schema"`
	BundleDigest          string                              `json:"bundle_digest"`
	ArtifactSHA256        string                              `json:"artifact_sha256"`
	Publisher             string                              `json:"publisher"`
	PublisherVerification string                              `json:"publisher_verification"`
	SourceURL             string                              `json:"source_url"`
	SourceVerification    string                              `json:"source_verification"`
	FinalRootDigest       string                              `json:"final_root_digest"`
	Bundle                ACPAdapterBundleVerificationReceipt `json:"bundle"`
}

type acpAdapterInstallIdentity struct {
	Schema                string `json:"schema"`
	Provider              string `json:"provider"`
	Adapter               string `json:"adapter"`
	Version               string `json:"version"`
	Protocol              string `json:"protocol"`
	GOOS                  string `json:"goos"`
	GOARCH                string `json:"goarch"`
	ArtifactSHA256        string `json:"artifact_sha256"`
	Publisher             string `json:"publisher"`
	PublisherVerification string `json:"publisher_verification"`
	SourceURL             string `json:"source_url"`
	SourceVerification    string `json:"source_verification"`
}

// ACPAdapterDoctorRequest is read-only. AuthSource is optional; when absent,
// doctor intentionally does not inspect any authentication source.
type ACPAdapterDoctorRequest struct {
	StateRoot    string
	BundleDigest string
	AuthSource   string
}

// ACPAdapterDoctorReport distinguishes a valid local installation from
// configuration/authentication. This command never authenticates, so
// Authenticated is always false even when a selected source is present.
type ACPAdapterDoctorReport struct {
	Schema            string `json:"schema"`
	BundleDigest      string `json:"bundle_digest"`
	Installed         bool   `json:"installed"`
	Configured        bool   `json:"configured"`
	AuthSource        string `json:"auth_source"`
	AuthSourcePresent bool   `json:"auth_source_present"`
	Authenticated     bool   `json:"authenticated"`
	Integrity         string `json:"integrity"`
	ValidationError   string `json:"validation_error,omitempty"`
}

func doctorACPAdapter(request ACPAdapterDoctorRequest) (ACPAdapterDoctorReport, error) {
	report := ACPAdapterDoctorReport{Schema: "tusker.acp-adapter-doctor/v1", BundleDigest: request.BundleDigest, AuthSource: "none", Authenticated: false, Integrity: "not_installed"}
	if !validACPAdapterBundleDigest(request.BundleDigest) {
		return report, fmt.Errorf("bundle-digest must be a canonical sha256 digest")
	}
	if strings.TrimSpace(request.AuthSource) != "" {
		source := CodexACPAuthSource(request.AuthSource)
		key, err := (CodexACPAuthContract{Source: source}).environmentKey()
		if err != nil {
			return report, fmt.Errorf("unsupported Codex ACP auth source")
		}
		report.AuthSource = string(source)
		// Do not retain or emit the selected value. Presence is diagnostic only;
		// authentication remains a provider-owned operation we do not perform.
		_, report.AuthSourcePresent = os.LookupEnv(key)
	}
	root, exists, err := locateACPAdapterInstallRoot(request.StateRoot)
	if err != nil {
		return report, err
	}
	if !exists {
		return report, nil
	}
	receiptPath := filepath.Join(root, "receipts", acpAdapterInstallDigestName(request.BundleDigest)+".json")
	receipt, exists, err := readACPAdapterInstallReceipt(receiptPath)
	if err != nil {
		return report, err
	}
	if exists {
		report.Installed = true
		if err := validateACPAdapterInstallReceipt(root, request.BundleDigest, receipt); err != nil {
			report.Integrity, report.ValidationError = "invalid", err.Error()
			return report, nil
		}
		// Bundle integrity does not establish a workflow/profile configuration.
		// This doctor intentionally does not inspect or mutate either.
		report.Integrity = "valid"
		return report, nil
	}

	packaged, err := validatePackagedACPAdapterInstallation(root, request.BundleDigest)
	if err != nil {
		report.Installed = packaged
		if packaged {
			report.Integrity, report.ValidationError = "invalid", err.Error()
			return report, nil
		}
		return report, err
	}
	if !packaged {
		return report, nil
	}
	report.Installed = true
	// Bundle integrity does not establish a workflow/profile configuration.
	// This doctor intentionally does not inspect or mutate either.
	report.Integrity = "valid"
	return report, nil
}

func digestACPAdapterInstallIdentity(identity acpAdapterInstallIdentity) (string, error) {
	raw, err := json.Marshal(identity)
	if err != nil {
		return "", err
	}
	return acpAdapterBundleDigest(raw), nil
}

func acpAdapterInstallDigestName(digest string) string {
	return strings.TrimPrefix(digest, acpAdapterBundleDigestPrefix)
}

func acpAdapterBundleDigest(raw []byte) string {
	sum := sha256.Sum256(raw)
	return acpAdapterBundleDigestPrefix + hex.EncodeToString(sum[:])
}

// locateACPAdapterInstallRoot never creates or chmods anything. Doctor uses it
// so an absent installation remains an observation, not an implicit install.
func locateACPAdapterInstallRoot(stateRoot string) (string, bool, error) {
	if stateRoot == "" {
		stateRoot = DefaultStateRoot()
	}
	if !filepath.IsAbs(stateRoot) {
		return "", false, fmt.Errorf("ACP adapter state root must be absolute")
	}
	root := filepath.Join(filepath.Clean(stateRoot), "acp-adapters")
	if err := validateACPAdapterInstallStateDirectory(root); os.IsNotExist(err) {
		return root, false, nil
	} else if err != nil {
		return "", false, fmt.Errorf("ACP adapter state directory is unsafe: %q", root)
	}
	for _, child := range []string{"bundles", "receipts"} {
		path := filepath.Join(root, child)
		if err := validateACPAdapterInstallStateDirectory(path); err != nil {
			return "", false, fmt.Errorf("ACP adapter state directory is unsafe: %q", path)
		}
	}
	physical, err := filepath.EvalSymlinks(root)
	if err != nil || !filepath.IsAbs(physical) {
		return "", false, fmt.Errorf("resolve ACP adapter state root")
	}
	return filepath.Clean(physical), true, nil
}

func validateACPAdapterInstallStateDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() || info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("unsafe directory")
	}
	uid, _, ok := acpAdapterBundlePOSIXIdentity(info)
	if !ok || uid != uint64(os.Getuid()) {
		return fmt.Errorf("directory is not owned by current user")
	}
	return nil
}

func readACPAdapterInstallReceipt(path string) (ACPAdapterInstallReceipt, bool, error) {
	raw, exists, err := readACPAdapterInstallReceiptBytes(path)
	if err != nil {
		return ACPAdapterInstallReceipt{}, false, err
	}
	if !exists {
		return ACPAdapterInstallReceipt{}, false, nil
	}
	var receipt ACPAdapterInstallReceipt
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&receipt); err != nil {
		return ACPAdapterInstallReceipt{}, false, fmt.Errorf("decode ACP adapter receipt: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return ACPAdapterInstallReceipt{}, false, fmt.Errorf("ACP adapter receipt has trailing JSON")
	}
	canonical, err := json.Marshal(receipt)
	if err != nil || !bytes.Equal(raw, canonical) {
		return ACPAdapterInstallReceipt{}, false, fmt.Errorf("ACP adapter receipt is not exact canonical JSON")
	}
	return receipt, true, nil
}

func validateACPAdapterInstallReceipt(root, expectedDigest string, receipt ACPAdapterInstallReceipt) error {
	if receipt.Schema != acpAdapterInstallReceiptSchema || receipt.BundleDigest != expectedDigest || !validACPAdapterBundleDigest(receipt.ArtifactSHA256) || receipt.Publisher != acpAdapterInstallPublisher || receipt.PublisherVerification != acpAdapterCallerMetadataStatus || receipt.SourceVerification != acpAdapterCallerMetadataStatus {
		return fmt.Errorf("ACP adapter receipt identity is invalid")
	}
	identity := acpAdapterInstallIdentity{Schema: acpAdapterInstallIdentitySchema, Provider: acpAdapterInstallProvider, Adapter: acpAdapterInstallAdapter, Version: receipt.Bundle.Version, Protocol: ACPAdapterBundleProtocolV1, GOOS: runtime.GOOS, GOARCH: runtime.GOARCH, ArtifactSHA256: receipt.ArtifactSHA256, Publisher: receipt.Publisher, PublisherVerification: receipt.PublisherVerification, SourceURL: receipt.SourceURL, SourceVerification: receipt.SourceVerification}
	digest, err := digestACPAdapterInstallIdentity(identity)
	if err != nil || digest != receipt.BundleDigest {
		return fmt.Errorf("ACP adapter receipt content identity drift")
	}
	finalRoot := filepath.Join(root, "bundles", acpAdapterInstallDigestName(receipt.BundleDigest))
	manifestDigest := receipt.Bundle.ManifestFileSHA256
	expectedFinalRootDigest, err := ACPAdapterBundleFinalRootDigest(finalRoot, manifestDigest)
	if err != nil || receipt.FinalRootDigest != expectedFinalRootDigest {
		return fmt.Errorf("ACP adapter receipt final root binding drift")
	}
	request := ACPAdapterBundleValidationRequest{BundleRoot: finalRoot, ManifestPath: acpAdapterInstallManifestPath, ExpectedManifestSHA256: manifestDigest, ExpectedDescriptor: ACPAdapterBundleDescriptorPolicy{Provider: acpAdapterInstallProvider, Adapter: acpAdapterInstallAdapter, Version: receipt.Bundle.Version, LaunchKind: ACPAdapterBundleLaunchNative}, ExpectedFinalRoot: finalRoot, ExpectedFinalRootDigest: receipt.FinalRootDigest, TrustCurrentUserBoundary: true, ProviderAllowed: func(provider string) bool { return provider == acpAdapterInstallProvider }}
	if err := RevalidateACPAdapterBundleReceipt(request, receipt.Bundle); err != nil {
		return err
	}
	return validateACPAdapterInstallBundleBinding(receipt.Bundle, receipt.ArtifactSHA256, finalRoot)
}

func validateACPAdapterInstallBundleBinding(bundle ACPAdapterBundleVerificationReceipt, artifactDigest, finalRoot string) error {
	if bundle.Provider != acpAdapterInstallProvider || bundle.Adapter != acpAdapterInstallAdapter || bundle.BundleRoot != finalRoot || len(bundle.Argv) != 1 || bundle.Argv[0] != filepath.Join(finalRoot, "codex-acp") || len(bundle.Assets) != 1 {
		return fmt.Errorf("ACP adapter receipt does not bind the exact native bundle shape")
	}
	asset := bundle.Assets[0]
	if asset.Path != "codex-acp" || asset.Role != "executable" || asset.SHA256 != artifactDigest {
		return fmt.Errorf("ACP adapter outer artifact sha256 does not bind codex-acp executable")
	}
	return nil
}

func printACPAdapterHelp() {
	fmt.Println(`Usage:
  tusker acp setup --npm-prefix /absolute/npm-prefix [--node /absolute/node] [--auth-source chatgpt_session|codex_api_key|openai_api_key] [--auth-principal NON_SECRET_LABEL] [--vault /absolute/.tusker] --json
  tusker acp install --provider codex --artifact /absolute/local/codex-acp --version VERSION --artifact-sha256 sha256:... --source-url https://... --publisher agentclientprotocol --json
  tusker acp doctor --bundle-digest sha256:... [--auth-source chatgpt_session|codex_api_key|openai_api_key] --json

Purpose:
  Setup packages an exact already-installed npm prefix into Tusker's sealed
  runtime and writes machine-local configuration making codex_acp primary.
  It never runs npm, logs in, starts the daemon, or sends a provider prompt.

  Install one already-downloaded, sealed native Codex ACP binary. This command
  never downloads, executes, authenticates, or configures a runner. Doctor
  validates the immutable local bundle and reports source presence separately
  from workflow/profile configuration and authentication; it never
  authenticates or prints credential values.
  Publisher and source URL are unverified caller metadata.`)
}

func acpDoctorCommand(args Args) error {
	if err := validateACPAdapterCommandArgs(args, "json", "bundle-digest", "auth-source"); err != nil {
		return tuskerError(errorInvalidArg, err.Error())
	}
	if !args.Bool("json") {
		return tuskerError(errorInvalidArg, "acp doctor requires --json")
	}
	report, err := doctorACPAdapter(ACPAdapterDoctorRequest{StateRoot: DefaultStateRoot(), BundleDigest: args.String("bundle-digest"), AuthSource: args.String("auth-source")})
	if err != nil {
		return tuskerError(errorInvalidArg, err.Error())
	}
	emitJSON(map[string]any{"ok": true, "doctor": report})
	return nil
}

func validateACPAdapterCommandArgs(args Args, allowed ...string) error {
	accepted := make(map[string]struct{}, len(allowed))
	for _, name := range allowed {
		accepted[name] = struct{}{}
	}
	for name := range args {
		if name == "_pos" || strings.HasPrefix(name, "_pos") {
			return fmt.Errorf("ACP commands do not accept positional arguments")
		}
		if _, ok := accepted[name]; !ok {
			return fmt.Errorf("unsupported ACP command flag --%s", name)
		}
	}
	return nil
}
