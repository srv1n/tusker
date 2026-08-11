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
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"unicode"
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

// ACPAdapterInstallRequest contains only release metadata and a local path.
// ArtifactPath is intentionally omitted from the persisted receipt. Publisher
// and SourceURL are caller assertions retained as metadata, not verified facts.
type ACPAdapterInstallRequest struct {
	StateRoot      string
	Provider       string
	ArtifactPath   string
	Version        string
	ArtifactSHA256 string
	SourceURL      string
	Publisher      string
}

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

func installACPAdapter(request ACPAdapterInstallRequest) (ACPAdapterInstallReceipt, error) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		return ACPAdapterInstallReceipt{}, fmt.Errorf("ACP adapter installation is supported only on darwin and linux")
	}
	identity, err := normalizeACPAdapterInstallRequest(request)
	if err != nil {
		return ACPAdapterInstallReceipt{}, err
	}
	bundleDigest, err := digestACPAdapterInstallIdentity(identity)
	if err != nil {
		return ACPAdapterInstallReceipt{}, err
	}
	root, err := prepareACPAdapterInstallRoot(request.StateRoot)
	if err != nil {
		return ACPAdapterInstallReceipt{}, err
	}
	finalRoot := filepath.Join(root, "bundles", acpAdapterInstallDigestName(bundleDigest))
	receiptPath := filepath.Join(root, "receipts", acpAdapterInstallDigestName(bundleDigest)+".json")

	if existing, exists, err := readACPAdapterInstallReceipt(receiptPath); err != nil {
		return ACPAdapterInstallReceipt{}, err
	} else if exists {
		if err := validateACPAdapterInstallReceipt(root, bundleDigest, existing); err != nil {
			return ACPAdapterInstallReceipt{}, fmt.Errorf("existing ACP adapter receipt drift: %w", err)
		}
		if existing.ArtifactSHA256 != identity.ArtifactSHA256 || existing.Publisher != identity.Publisher || existing.SourceURL != identity.SourceURL || existing.Bundle.Version != identity.Version {
			return ACPAdapterInstallReceipt{}, fmt.Errorf("existing ACP adapter receipt does not match requested identity")
		}
		return existing, nil
	}
	if _, err := os.Lstat(finalRoot); err == nil {
		return ACPAdapterInstallReceipt{}, fmt.Errorf("ACP adapter final root exists without a matching receipt")
	} else if !os.IsNotExist(err) {
		return ACPAdapterInstallReceipt{}, fmt.Errorf("inspect ACP adapter final root: %w", err)
	}

	artifactDigest, err := hashACPAdapterInstallSource(request.ArtifactPath)
	if err != nil {
		return ACPAdapterInstallReceipt{}, err
	}
	if artifactDigest != identity.ArtifactSHA256 {
		return ACPAdapterInstallReceipt{}, fmt.Errorf("local ACP adapter artifact sha256 does not match caller-supplied digest")
	}
	stage, err := os.MkdirTemp(filepath.Join(root, ".staging"), "install-")
	if err != nil {
		return ACPAdapterInstallReceipt{}, fmt.Errorf("create ACP adapter stage: %w", err)
	}
	defer os.RemoveAll(stage)
	if err := os.Chmod(stage, 0o700); err != nil {
		return ACPAdapterInstallReceipt{}, err
	}
	stageRoot := filepath.Join(stage, "bundle")
	if err := os.Mkdir(stageRoot, 0o700); err != nil {
		return ACPAdapterInstallReceipt{}, err
	}
	stageBinary := filepath.Join(stageRoot, "codex-acp")
	if err := copyACPAdapterInstallSource(request.ArtifactPath, stageBinary); err != nil {
		return ACPAdapterInstallReceipt{}, err
	}
	// Hash the source again after copying. This does not elevate a mutable
	// source into trust; it catches ordinary source replacement races before
	// the staged copy is sealed and separately verifies copied bytes below.
	if after, err := hashACPAdapterInstallSource(request.ArtifactPath); err != nil || after != artifactDigest {
		if err != nil {
			return ACPAdapterInstallReceipt{}, err
		}
		return ACPAdapterInstallReceipt{}, fmt.Errorf("local ACP adapter artifact changed while staging")
	}
	stagedDigest, err := hashACPAdapterInstallSealedFile(stageBinary, false)
	if err != nil {
		return ACPAdapterInstallReceipt{}, err
	}
	if stagedDigest != artifactDigest {
		return ACPAdapterInstallReceipt{}, fmt.Errorf("staged ACP adapter artifact fingerprint drift")
	}
	if err := os.Chmod(stageBinary, 0o500); err != nil {
		return ACPAdapterInstallReceipt{}, err
	}

	manifest := ACPAdapterBundleManifest{
		Schema: ACPAdapterBundleSchema, Provider: acpAdapterInstallProvider, Adapter: acpAdapterInstallAdapter,
		Version: identity.Version, Protocol: ACPAdapterBundleProtocolV1, GOOS: runtime.GOOS, GOARCH: runtime.GOARCH,
		Argv:   []string{filepath.Join(finalRoot, "codex-acp")},
		Assets: []ACPAdapterBundleAsset{{Path: "codex-acp", SHA256: artifactDigest, Role: "executable"}},
	}
	manifestRaw, err := json.Marshal(manifest)
	if err != nil {
		return ACPAdapterInstallReceipt{}, err
	}
	manifestDigest := acpAdapterBundleDigest(manifestRaw)
	if err := writeACPAdapterInstallFile(filepath.Join(stageRoot, acpAdapterInstallManifestPath), manifestRaw, 0o400); err != nil {
		return ACPAdapterInstallReceipt{}, err
	}
	if err := syncACPAdapterInstallDirectory(stage); err != nil {
		return ACPAdapterInstallReceipt{}, err
	}
	if err := publishACPAdapterBundleExclusive(stageRoot, finalRoot); err != nil {
		return ACPAdapterInstallReceipt{}, fmt.Errorf("publish ACP adapter bundle without overwrite: %w", err)
	}
	// Darwin requires write access to the source directory for rename(2), so
	// seal the final directory immediately after the no-overwrite rename. The
	// root is never advertised: the immutable receipt is published only after
	// this seal and whole-tree validation succeed.
	if err := sealACPAdapterInstallDirectory(finalRoot); err != nil {
		return ACPAdapterInstallReceipt{}, fmt.Errorf("seal published ACP adapter bundle: %w", err)
	}
	if err := syncACPAdapterInstallDirectory(filepath.Join(root, "bundles")); err != nil {
		return ACPAdapterInstallReceipt{}, err
	}
	finalRootDigest, err := ACPAdapterBundleFinalRootDigest(finalRoot, manifestDigest)
	if err != nil {
		return ACPAdapterInstallReceipt{}, err
	}
	validated, err := ValidateACPAdapterBundle(ACPAdapterBundleValidationRequest{
		BundleRoot: finalRoot, ManifestPath: acpAdapterInstallManifestPath, ExpectedManifestSHA256: manifestDigest,
		ExpectedDescriptor: ACPAdapterBundleDescriptorPolicy{Provider: acpAdapterInstallProvider, Adapter: acpAdapterInstallAdapter, Version: identity.Version, LaunchKind: ACPAdapterBundleLaunchNative},
		ExpectedFinalRoot:  finalRoot, ExpectedFinalRootDigest: finalRootDigest, TrustCurrentUserBoundary: true,
		ProviderAllowed: func(provider string) bool { return provider == acpAdapterInstallProvider },
	})
	if err != nil {
		return ACPAdapterInstallReceipt{}, fmt.Errorf("validate published ACP adapter bundle: %w", err)
	}
	if err := validateACPAdapterInstallBundleBinding(validated, artifactDigest, finalRoot); err != nil {
		return ACPAdapterInstallReceipt{}, err
	}
	receipt := ACPAdapterInstallReceipt{
		Schema: acpAdapterInstallReceiptSchema, BundleDigest: bundleDigest, ArtifactSHA256: artifactDigest,
		Publisher: identity.Publisher, PublisherVerification: acpAdapterCallerMetadataStatus,
		SourceURL: identity.SourceURL, SourceVerification: acpAdapterCallerMetadataStatus, FinalRootDigest: finalRootDigest, Bundle: validated,
	}
	if err := writeACPAdapterInstallReceipt(receiptPath, receipt); err != nil {
		return ACPAdapterInstallReceipt{}, err
	}
	return receipt, nil
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
	if !exists {
		return report, nil
	}
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

func normalizeACPAdapterInstallRequest(request ACPAdapterInstallRequest) (acpAdapterInstallIdentity, error) {
	if request.Provider != acpAdapterInstallProvider || request.Publisher != acpAdapterInstallPublisher {
		return acpAdapterInstallIdentity{}, fmt.Errorf("this local installer currently accepts only codex with caller-supplied publisher metadata agentclientprotocol")
	}
	if _, err := normalizeACPAdapterInstallArtifactPath(request.ArtifactPath); err != nil {
		return acpAdapterInstallIdentity{}, err
	}
	if !validACPAdapterBundleIdentity(request.Version) || !validACPAdapterBundleDigest(request.ArtifactSHA256) {
		return acpAdapterInstallIdentity{}, fmt.Errorf("an exact adapter version and canonical artifact-sha256 are required")
	}
	if request.SourceURL != strings.TrimSpace(request.SourceURL) || len(request.SourceURL) > 2048 || strings.IndexFunc(request.SourceURL, unicode.IsControl) >= 0 {
		return acpAdapterInstallIdentity{}, fmt.Errorf("source-url must be exact non-control metadata")
	}
	parsed, err := url.Parse(request.SourceURL)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return acpAdapterInstallIdentity{}, fmt.Errorf("source-url must be an absolute https URL")
	}
	return acpAdapterInstallIdentity{Schema: acpAdapterInstallIdentitySchema, Provider: acpAdapterInstallProvider, Adapter: acpAdapterInstallAdapter, Version: request.Version, Protocol: ACPAdapterBundleProtocolV1, GOOS: runtime.GOOS, GOARCH: runtime.GOARCH, ArtifactSHA256: request.ArtifactSHA256, Publisher: request.Publisher, PublisherVerification: acpAdapterCallerMetadataStatus, SourceURL: request.SourceURL, SourceVerification: acpAdapterCallerMetadataStatus}, nil
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

func prepareACPAdapterInstallRoot(stateRoot string) (string, error) {
	if stateRoot == "" {
		stateRoot = DefaultStateRoot()
	}
	if !filepath.IsAbs(stateRoot) {
		return "", fmt.Errorf("ACP adapter state root must be absolute")
	}
	root := filepath.Join(filepath.Clean(stateRoot), "acp-adapters")
	for _, path := range []string{root, filepath.Join(root, ".staging"), filepath.Join(root, "bundles"), filepath.Join(root, "receipts")} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			return "", fmt.Errorf("create ACP adapter state directory: %w", err)
		}
		if err := validateACPAdapterInstallStateDirectory(path); err != nil {
			return "", fmt.Errorf("ACP adapter state directory is unsafe: %q", path)
		}
		if err := os.Chmod(path, 0o700); err != nil {
			return "", err
		}
	}
	physical, err := filepath.EvalSymlinks(root)
	if err != nil || !filepath.IsAbs(physical) {
		return "", fmt.Errorf("resolve ACP adapter state root")
	}
	return filepath.Clean(physical), nil
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

func normalizeACPAdapterInstallArtifactPath(path string) (string, error) {
	if path == "" || path != strings.TrimSpace(path) || !filepath.IsAbs(path) || filepath.Clean(path) != path || strings.IndexFunc(path, unicode.IsControl) >= 0 {
		return "", fmt.Errorf("artifact must be an exact absolute local file path")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return "", fmt.Errorf("inspect local ACP adapter artifact: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Mode().Perm()&0o222 != 0 || info.Mode().Perm()&0o111 == 0 || info.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 {
		return "", fmt.Errorf("local ACP adapter artifact must be a sealed non-symlink regular file")
	}
	if err := requireACPAdapterBundleFileOwnership(info, path); err != nil {
		return "", err
	}
	if err := validateACPAdapterNativeBinary(path); err != nil {
		return "", err
	}
	return path, nil
}

func hashACPAdapterInstallSource(path string) (string, error) {
	if _, err := normalizeACPAdapterInstallArtifactPath(path); err != nil {
		return "", err
	}
	return hashACPAdapterInstallFile(path, false)
}

func hashACPAdapterInstallSealedFile(path string, executable bool) (string, error) {
	if executable {
		if _, _, _, _, err := readAndHashACPAdapterBundleFile(path, acpAdapterBundleMaxFile, false, "executable"); err != nil {
			return "", err
		}
	}
	return hashACPAdapterInstallFile(path, false)
}

func hashACPAdapterInstallFile(path string, requireSealed bool) (string, error) {
	before, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if before.Mode()&os.ModeSymlink != 0 || !before.Mode().IsRegular() || before.Size() < 0 || before.Size() > acpAdapterBundleMaxFile {
		return "", fmt.Errorf("ACP adapter file is not a bounded regular file")
	}
	if requireSealed && before.Mode().Perm()&0o222 != 0 {
		return "", fmt.Errorf("ACP adapter file is writable")
	}
	if err := requireACPAdapterBundleFileOwnership(before, path); err != nil {
		return "", err
	}
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(before, opened) {
		return "", fmt.Errorf("ACP adapter source identity changed before hashing")
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, io.LimitReader(file, acpAdapterBundleMaxFile+1)); err != nil {
		return "", err
	}
	after, err := file.Stat()
	if err != nil || !os.SameFile(opened, after) || opened.Size() != after.Size() || !opened.ModTime().Equal(after.ModTime()) {
		return "", fmt.Errorf("ACP adapter source changed while hashing")
	}
	pathAfter, err := os.Lstat(path)
	if err != nil || !os.SameFile(after, pathAfter) || after.Size() != pathAfter.Size() || !after.ModTime().Equal(pathAfter.ModTime()) {
		return "", fmt.Errorf("ACP adapter source path changed while hashing")
	}
	return acpAdapterBundleDigestPrefix + hex.EncodeToString(hash.Sum(nil)), nil
}

func copyACPAdapterInstallSource(source, destination string) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	before, err := input.Stat()
	if err != nil {
		return err
	}
	output, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o700)
	if err != nil {
		return err
	}
	if _, err := io.Copy(output, io.LimitReader(input, acpAdapterBundleMaxFile+1)); err != nil {
		_ = output.Close()
		return err
	}
	if err := output.Sync(); err != nil {
		_ = output.Close()
		return err
	}
	if err := output.Close(); err != nil {
		return err
	}
	after, err := input.Stat()
	if err != nil || !os.SameFile(before, after) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		return fmt.Errorf("ACP adapter source changed while copying")
	}
	return nil
}

func writeACPAdapterInstallFile(path string, raw []byte, mode os.FileMode) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	if _, err := file.Write(raw); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func sealACPAdapterInstallDirectory(path string) error {
	entries, err := os.ReadDir(path)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		info, err := os.Lstat(filepath.Join(path, entry.Name()))
		if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("stage contains a non-regular ACP adapter file")
		}
	}
	if err := os.Chmod(path, 0o500); err != nil {
		return err
	}
	return syncACPAdapterInstallDirectory(path)
}

func syncACPAdapterInstallDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

func writeACPAdapterInstallReceipt(path string, receipt ACPAdapterInstallReceipt) error {
	raw, err := json.Marshal(receipt)
	if err != nil {
		return err
	}
	stage := path + ".stage-" + fmt.Sprintf("%d", time.Now().UnixNano())
	if err := writeACPAdapterInstallFile(stage, raw, 0o400); err != nil {
		return err
	}
	defer os.Remove(stage)
	// link(2) is an atomic no-replace publication primitive for this regular
	// receipt. Remove the staging name afterward so the final receipt retains
	// the single-link invariant enforced by the reader.
	if err := os.Link(stage, path); err != nil {
		return fmt.Errorf("publish ACP adapter receipt without overwrite: %w", err)
	}
	if err := os.Remove(stage); err != nil {
		return err
	}
	return syncACPAdapterInstallDirectory(filepath.Dir(path))
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

func acpInstallCommand(args Args) error {
	if err := validateACPAdapterCommandArgs(args, "json", "provider", "artifact", "version", "artifact-sha256", "source-url", "publisher"); err != nil {
		return tuskerError(errorInvalidArg, err.Error())
	}
	if !args.Bool("json") {
		return tuskerError(errorInvalidArg, "acp install requires --json")
	}
	receipt, err := installACPAdapter(ACPAdapterInstallRequest{StateRoot: DefaultStateRoot(), Provider: args.String("provider"), ArtifactPath: args.String("artifact"), Version: args.String("version"), ArtifactSHA256: args.String("artifact-sha256"), SourceURL: args.String("source-url"), Publisher: args.String("publisher")})
	if err != nil {
		return tuskerError(errorInvalidArg, err.Error())
	}
	emitJSON(map[string]any{"ok": true, "receipt": receipt})
	return nil
}

func printACPAdapterHelp() {
	fmt.Println(`Usage:
  tusker acp install --provider codex --artifact /absolute/local/codex-acp --version VERSION --artifact-sha256 sha256:... --source-url https://... --publisher agentclientprotocol --json
  tusker acp doctor --bundle-digest sha256:... [--auth-source chatgpt_session|codex_api_key|openai_api_key] --json

Purpose:
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
