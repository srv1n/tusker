package main

// This file is the read-only content-verification boundary for local ACP
// adapters. It neither downloads nor starts an adapter. Passing validation
// proves that a separately trusted policy and manifest digest describe the
// complete sealed bundle bytes currently present at one path. It does not
// prove immutable execution: a same-UID process can chmod or replace these
// files. Production installation must atomically publish a content-addressed
// final root, and the execution sandbox must prevent the adapter from writing
// that root or its parent before immediate pre-spawn revalidation.

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"unicode"
)

const (
	ACPAdapterBundleSchema             = "tusker.acp-adapter-bundle/v1"
	ACPAdapterBundleProtocolV1         = "acp/v1"
	ACPAdapterBundleVerificationSchema = "tusker.acp-adapter-bundle-content-verification/v1"

	acpAdapterBundleDigestPrefix   = "sha256:"
	acpAdapterBundleMaxAssets      = 4096
	acpAdapterBundleMaxPathBytes   = 1024
	acpAdapterBundleMaxManifest    = int64(1 << 20)
	acpAdapterBundleMaxFile        = int64(512 << 20)
	acpAdapterBundleMaxTotalBytes  = int64(2 << 30)
	acpAdapterBundleMaxTreeEntries = 8192
	acpAdapterBundleMaxTreeDepth   = 64
)

// ACPAdapterBundleManifest is the canonical on-disk declaration. Asset paths
// are sorted, slash-separated, bundle-relative paths. Argv is exactly one
// bundled native executable or one bundled interpreter plus one bundled
// entrypoint. Flags, package names, PATH commands, and free arguments are not
// part of this launch contract.
type ACPAdapterBundleManifest struct {
	Schema   string                  `json:"schema"`
	Provider string                  `json:"provider"`
	Adapter  string                  `json:"adapter"`
	Version  string                  `json:"version"`
	Protocol string                  `json:"protocol"`
	GOOS     string                  `json:"goos"`
	GOARCH   string                  `json:"goarch"`
	Argv     []string                `json:"argv"`
	Assets   []ACPAdapterBundleAsset `json:"assets"`
}

// ACPAdapterBundleAsset is content-addressed. Role is executable, entrypoint,
// or asset. A native manifest has no entrypoint role; an interpreter manifest
// has exactly one.
type ACPAdapterBundleAsset struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Role   string `json:"role"`
}

type ACPAdapterBundleLaunchKind string

const (
	ACPAdapterBundleLaunchNative      ACPAdapterBundleLaunchKind = "native"
	ACPAdapterBundleLaunchInterpreter ACPAdapterBundleLaunchKind = "interpreter_entrypoint"
)

// ACPAdapterBundleDescriptorPolicy is trusted caller input, not manifest
// self-description. It fixes the provider, adapter, exact version, and launch
// shape accepted at this admission point.
type ACPAdapterBundleDescriptorPolicy struct {
	Provider   string
	Adapter    string
	Version    string
	LaunchKind ACPAdapterBundleLaunchKind
}

// ACPAdapterBundleValidationRequest keeps the expected digest outside the
// manifest it authenticates. ProviderAllowed is caller-owned onboarding
// policy; nil accepts any syntactically valid non-empty provider.
type ACPAdapterBundleValidationRequest struct {
	BundleRoot               string
	ManifestPath             string
	ExpectedManifestSHA256   string
	ExpectedDescriptor       ACPAdapterBundleDescriptorPolicy
	ExpectedFinalRoot        string
	ExpectedFinalRootDigest  string
	TrustCurrentUserBoundary bool
	ProviderAllowed          ACPAdapterProviderAllowlist
}

// ACPAdapterProviderAllowlist is supplied by the onboarding caller.
type ACPAdapterProviderAllowlist func(provider string) bool

// ACPAdapterBundleVerificationReceipt records verified content and policy. It
// is not an execution-immutability attestation. It contains no environment,
// credential, prompt, or provider-response values.
type ACPAdapterBundleVerificationReceipt struct {
	Schema                string                     `json:"schema"`
	Provider              string                     `json:"provider"`
	Adapter               string                     `json:"adapter"`
	Version               string                     `json:"version"`
	Protocol              string                     `json:"protocol"`
	GOOS                  string                     `json:"goos"`
	GOARCH                string                     `json:"goarch"`
	BundleRoot            string                     `json:"bundle_root"`
	ManifestPath          string                     `json:"manifest_path"`
	ManifestFileSHA256    string                     `json:"manifest_file_sha256"`
	LaunchKind            ACPAdapterBundleLaunchKind `json:"launch_kind"`
	Argv                  []string                   `json:"argv"`
	Assets                []ACPAdapterBundleAsset    `json:"assets"`
	VerifiedContentDigest string                     `json:"verified_content_digest"`
}

// ValidateACPAdapterBundle binds a separately trusted manifest hash to the
// exact canonical manifest file, the complete bundle tree, and this runtime's
// supported OS/architecture.
func ValidateACPAdapterBundle(request ACPAdapterBundleValidationRequest) (ACPAdapterBundleVerificationReceipt, error) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		return ACPAdapterBundleVerificationReceipt{}, fmt.Errorf("ACP adapter bundles are supported only on darwin and linux")
	}
	if !request.TrustCurrentUserBoundary {
		return ACPAdapterBundleVerificationReceipt{}, fmt.Errorf("ACP adapter verification requires an explicit trusted same-UID installation boundary")
	}
	root, err := canonicalACPAdapterBundleRoot(request.BundleRoot)
	if err != nil {
		return ACPAdapterBundleVerificationReceipt{}, err
	}
	if err := validateACPAdapterBundleExpectedRoot(root, request.ExpectedFinalRoot); err != nil {
		return ACPAdapterBundleVerificationReceipt{}, err
	}
	manifestRelative, err := normalizeACPAdapterBundleRelative(request.ManifestPath)
	if err != nil || manifestRelative == "" {
		return ACPAdapterBundleVerificationReceipt{}, fmt.Errorf("a canonical bundle-relative manifest path is required")
	}
	expectedManifestDigest := request.ExpectedManifestSHA256
	if !validACPAdapterBundleDigest(expectedManifestDigest) {
		return ACPAdapterBundleVerificationReceipt{}, fmt.Errorf("a separately trusted lowercase manifest sha256 is required")
	}
	if request.ExpectedFinalRoot == "" && request.ExpectedFinalRootDigest == "" {
		return ACPAdapterBundleVerificationReceipt{}, fmt.Errorf("ACP adapter verification requires an exact trusted final root or expected root digest")
	}

	manifestAbsolute := filepath.Join(root, filepath.FromSlash(manifestRelative))
	rawManifest, manifestDigest, manifestSize, manifestInfo, err := readAndHashACPAdapterBundleFile(manifestAbsolute, acpAdapterBundleMaxManifest, true, "manifest")
	if err != nil {
		return ACPAdapterBundleVerificationReceipt{}, err
	}
	if manifestDigest != expectedManifestDigest {
		return ACPAdapterBundleVerificationReceipt{}, fmt.Errorf("manifest fingerprint drift")
	}
	if err := validateACPAdapterBundleExpectedRootDigest(root, manifestDigest, request.ExpectedFinalRootDigest); err != nil {
		return ACPAdapterBundleVerificationReceipt{}, err
	}
	manifest, err := decodeCanonicalACPAdapterBundleManifest(rawManifest)
	if err != nil {
		return ACPAdapterBundleVerificationReceipt{}, err
	}
	if err := validateACPAdapterBundleIdentity(manifest, request.ProviderAllowed); err != nil {
		return ACPAdapterBundleVerificationReceipt{}, err
	}
	if err := validateACPAdapterBundleDescriptorPolicy(manifest, request.ExpectedDescriptor); err != nil {
		return ACPAdapterBundleVerificationReceipt{}, err
	}
	assets, byPath, roles, err := normalizeACPAdapterBundleAssets(manifest.Assets)
	if err != nil {
		return ACPAdapterBundleVerificationReceipt{}, err
	}
	if _, overlaps := byPath[manifestRelative]; overlaps {
		return ACPAdapterBundleVerificationReceipt{}, fmt.Errorf("manifest path overlaps an adapter asset")
	}
	if err := validateACPAdapterBundleArgv(root, manifest.Argv, byPath, roles); err != nil {
		return ACPAdapterBundleVerificationReceipt{}, err
	}
	if len(manifest.Argv) == 1 {
		if _, exists := roles["entrypoint"]; exists {
			return ACPAdapterBundleVerificationReceipt{}, fmt.Errorf("native ACP adapter manifest has an unused entrypoint role")
		}
	} else if _, exists := roles["entrypoint"]; !exists {
		return ACPAdapterBundleVerificationReceipt{}, fmt.Errorf("interpreter ACP adapter manifest omits its entrypoint role")
	}

	physical := []acpAdapterBundlePhysicalFile{{path: manifestAbsolute, info: manifestInfo}}
	verifiedFiles := map[string]os.FileInfo{manifestRelative: manifestInfo}
	totalBytes := manifestSize
	for _, asset := range assets {
		path := filepath.Join(root, filepath.FromSlash(asset.Path))
		info, err := os.Lstat(path)
		if err != nil {
			return ACPAdapterBundleVerificationReceipt{}, fmt.Errorf("asset %q is missing: %w", asset.Path, err)
		}
		for _, prior := range physical {
			if os.SameFile(prior.info, info) {
				return ACPAdapterBundleVerificationReceipt{}, fmt.Errorf("asset %q is a physical duplicate of %q", asset.Path, prior.path)
			}
		}
		physical = append(physical, acpAdapterBundlePhysicalFile{path: path, info: info})
	}
	for _, asset := range assets {
		path := filepath.Join(root, filepath.FromSlash(asset.Path))
		_, digest, size, info, err := readAndHashACPAdapterBundleFile(path, acpAdapterBundleMaxFile, false, asset.Role)
		if err != nil {
			return ACPAdapterBundleVerificationReceipt{}, err
		}
		if digest != asset.SHA256 {
			return ACPAdapterBundleVerificationReceipt{}, fmt.Errorf("asset %q fingerprint drift", asset.Path)
		}
		if size > acpAdapterBundleMaxTotalBytes-totalBytes {
			return ACPAdapterBundleVerificationReceipt{}, fmt.Errorf("ACP adapter bundle exceeds total byte limit")
		}
		totalBytes += size
		verifiedFiles[asset.Path] = info
	}
	if err := validateACPAdapterBundleTree(root, assets, manifestRelative, verifiedFiles); err != nil {
		return ACPAdapterBundleVerificationReceipt{}, err
	}
	// The tree walk above must not be the last word: it validates identity,
	// ownership, mode, bounds, and completeness, then this second digest pass
	// proves that no declared bytes drifted between their first hash and the
	// completed tree scan. A hostile same-UID process is outside this function's
	// stated trust boundary; callers still re-run the whole verifier immediately
	// before spawn.
	if _, digest, _, _, err := readAndHashACPAdapterBundleFile(manifestAbsolute, acpAdapterBundleMaxManifest, false, "manifest"); err != nil || digest != manifestDigest {
		return ACPAdapterBundleVerificationReceipt{}, fmt.Errorf("manifest changed during bundle verification")
	}
	for _, asset := range assets {
		path := filepath.Join(root, filepath.FromSlash(asset.Path))
		if _, digest, _, _, err := readAndHashACPAdapterBundleFile(path, acpAdapterBundleMaxFile, false, asset.Role); err != nil || digest != asset.SHA256 {
			return ACPAdapterBundleVerificationReceipt{}, fmt.Errorf("asset %q changed during bundle verification", asset.Path)
		}
	}

	receipt := ACPAdapterBundleVerificationReceipt{
		Schema: ACPAdapterBundleVerificationSchema, Provider: manifest.Provider, Adapter: manifest.Adapter,
		Version: manifest.Version, Protocol: manifest.Protocol, GOOS: manifest.GOOS, GOARCH: manifest.GOARCH,
		BundleRoot: root, ManifestPath: manifestRelative, ManifestFileSHA256: manifestDigest,
		LaunchKind: request.ExpectedDescriptor.LaunchKind, Argv: append([]string(nil), manifest.Argv...), Assets: assets,
	}
	receipt.VerifiedContentDigest, err = digestACPAdapterBundleReceipt(receipt)
	if err != nil {
		return ACPAdapterBundleVerificationReceipt{}, err
	}
	return receipt, nil
}

func decodeCanonicalACPAdapterBundleManifest(raw []byte) (ACPAdapterBundleManifest, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var manifest ACPAdapterBundleManifest
	if err := decoder.Decode(&manifest); err != nil {
		return ACPAdapterBundleManifest{}, fmt.Errorf("decode ACP adapter manifest: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return ACPAdapterBundleManifest{}, fmt.Errorf("ACP adapter manifest has trailing JSON")
	}
	canonical, err := json.Marshal(manifest)
	if err != nil {
		return ACPAdapterBundleManifest{}, err
	}
	if !bytes.Equal(raw, canonical) {
		return ACPAdapterBundleManifest{}, fmt.Errorf("ACP adapter manifest is not exact canonical JSON")
	}
	return manifest, nil
}

func validateACPAdapterBundleIdentity(manifest ACPAdapterBundleManifest, allow ACPAdapterProviderAllowlist) error {
	if manifest.Schema != ACPAdapterBundleSchema {
		return fmt.Errorf("unsupported ACP adapter bundle schema")
	}
	for label, value := range map[string]string{"provider": manifest.Provider, "adapter": manifest.Adapter, "version": manifest.Version} {
		if !validACPAdapterBundleIdentity(value) {
			return fmt.Errorf("invalid ACP adapter bundle %s", label)
		}
	}
	if allow != nil && !allow(manifest.Provider) {
		return fmt.Errorf("ACP adapter provider is not allowlisted")
	}
	if manifest.Protocol != ACPAdapterBundleProtocolV1 {
		return fmt.Errorf("unsupported ACP adapter protocol")
	}
	if manifest.GOOS != runtime.GOOS || manifest.GOARCH != runtime.GOARCH {
		return fmt.Errorf("ACP adapter platform %s/%s does not match runtime %s/%s", manifest.GOOS, manifest.GOARCH, runtime.GOOS, runtime.GOARCH)
	}
	return nil
}

func validateACPAdapterBundleDescriptorPolicy(manifest ACPAdapterBundleManifest, policy ACPAdapterBundleDescriptorPolicy) error {
	if !validACPAdapterBundleIdentity(policy.Provider) || !validACPAdapterBundleIdentity(policy.Adapter) || !validACPAdapterBundleIdentity(policy.Version) {
		return fmt.Errorf("ACP adapter verification requires an exact trusted descriptor policy")
	}
	if manifest.Provider != policy.Provider || manifest.Adapter != policy.Adapter || manifest.Version != policy.Version {
		return fmt.Errorf("ACP adapter manifest does not match expected descriptor policy")
	}
	switch policy.LaunchKind {
	case ACPAdapterBundleLaunchNative:
		if len(manifest.Argv) != 1 {
			return fmt.Errorf("native ACP adapter policy requires one-part argv")
		}
	case ACPAdapterBundleLaunchInterpreter:
		if len(manifest.Argv) != 2 {
			return fmt.Errorf("interpreter ACP adapter policy requires executable plus entrypoint argv")
		}
	default:
		return fmt.Errorf("ACP adapter verification requires an exact launch kind")
	}
	switch policy.Provider + "/" + policy.Adapter {
	case "codex/codex-acp":
		if policy.LaunchKind != ACPAdapterBundleLaunchNative {
			return fmt.Errorf("codex/codex-acp requires the native launch kind")
		}
	case "claude/claude-agent-acp":
		if policy.LaunchKind != ACPAdapterBundleLaunchInterpreter {
			return fmt.Errorf("claude/claude-agent-acp requires the interpreter entrypoint launch kind")
		}
	}
	return nil
}

func validateACPAdapterBundleExpectedRoot(root, expected string) error {
	if expected == "" {
		return nil
	}
	if expected != strings.TrimSpace(expected) || !filepath.IsAbs(expected) || filepath.Clean(expected) != expected || expected != root {
		return fmt.Errorf("ACP adapter bundle root does not match exact trusted final root")
	}
	return nil
}

func validateACPAdapterBundleExpectedRootDigest(root, manifestDigest, expected string) error {
	if expected == "" {
		return nil
	}
	actual, err := ACPAdapterBundleFinalRootDigest(root, manifestDigest)
	if err != nil || actual != expected {
		return fmt.Errorf("ACP adapter bundle root does not match expected content-addressed final root digest")
	}
	return nil
}

// ACPAdapterBundleFinalRootDigest is the installer-facing identity for one
// canonical final root and one separately authenticated manifest.
func ACPAdapterBundleFinalRootDigest(root, manifestDigest string) (string, error) {
	if !filepath.IsAbs(root) || filepath.Clean(root) != root || !validACPAdapterBundleDigest(manifestDigest) {
		return "", fmt.Errorf("invalid ACP adapter final root identity")
	}
	payload := strings.Join([]string{"tusker.acp-adapter-final-root/v1", root, manifestDigest}, "\x00")
	digest := sha256.Sum256([]byte(payload))
	return acpAdapterBundleDigestPrefix + hex.EncodeToString(digest[:]), nil
}

// RevalidateACPAdapterBundleReceipt reruns whole-tree verification immediately
// before spawn and requires exact equality with the prior content receipt.
func RevalidateACPAdapterBundleReceipt(request ACPAdapterBundleValidationRequest, expected ACPAdapterBundleVerificationReceipt) error {
	actual, err := ValidateACPAdapterBundle(request)
	if err != nil {
		return fmt.Errorf("revalidate ACP adapter bundle: %w", err)
	}
	if !reflect.DeepEqual(actual, expected) {
		return fmt.Errorf("ACP adapter bundle content receipt changed before spawn")
	}
	return nil
}

func validACPAdapterBundleIdentity(value string) bool {
	if value == "" || value != strings.TrimSpace(value) || len(value) > 256 || strings.IndexFunc(value, unicode.IsControl) >= 0 {
		return false
	}
	for _, r := range value {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '-' || r == '_' || r == '+') {
			return false
		}
	}
	return true
}

func canonicalACPAdapterBundleRoot(root string) (string, error) {
	if root == "" || root != strings.TrimSpace(root) || strings.IndexFunc(root, unicode.IsControl) >= 0 {
		return "", fmt.Errorf("ACP adapter bundle root is required as exact bytes")
	}
	if !filepath.IsAbs(root) {
		return "", fmt.Errorf("ACP adapter bundle root must be absolute")
	}
	abs, err := filepath.Abs(root)
	if err != nil || !filepath.IsAbs(abs) {
		return "", fmt.Errorf("ACP adapter bundle root must be absolute")
	}
	abs = filepath.Clean(abs)
	info, err := os.Lstat(abs)
	if err != nil {
		return "", fmt.Errorf("stat ACP adapter bundle root: %w", err)
	}
	if err := validateACPAdapterBundleDirectoryInfo(info, abs); err != nil {
		return "", err
	}
	physical, err := filepath.EvalSymlinks(abs)
	if err != nil || !filepath.IsAbs(physical) {
		return "", fmt.Errorf("resolve ACP adapter bundle root")
	}
	return filepath.Clean(physical), nil
}

func normalizeACPAdapterBundleRelative(value string) (string, error) {
	if value == "" || value != strings.TrimSpace(value) || len(value) > acpAdapterBundleMaxPathBytes || strings.IndexFunc(value, unicode.IsControl) >= 0 {
		return "", fmt.Errorf("path is empty, oversized, or non-canonical")
	}
	if strings.Contains(value, "\\") || strings.Contains(value, ":") || filepath.IsAbs(filepath.FromSlash(value)) || filepath.VolumeName(value) != "" {
		return "", fmt.Errorf("path must be volume-free and relative to bundle")
	}
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(value)))
	if clean != value || clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("path escapes bundle or is not normalized")
	}
	return clean, nil
}

func normalizeACPAdapterBundleAssets(input []ACPAdapterBundleAsset) ([]ACPAdapterBundleAsset, map[string]ACPAdapterBundleAsset, map[string]ACPAdapterBundleAsset, error) {
	if len(input) == 0 || len(input) > acpAdapterBundleMaxAssets {
		return nil, nil, nil, fmt.Errorf("ACP adapter bundle asset count is outside the allowed range")
	}
	assets := append([]ACPAdapterBundleAsset(nil), input...)
	seen := make(map[string]struct{}, len(assets))
	byPath := make(map[string]ACPAdapterBundleAsset, len(assets))
	roles := make(map[string]ACPAdapterBundleAsset, 2)
	previous := ""
	for index := range assets {
		path, err := normalizeACPAdapterBundleRelative(assets[index].Path)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("asset[%d] path: %w", index, err)
		}
		if assets[index].Path != path || !validACPAdapterBundleDigest(assets[index].SHA256) {
			return nil, nil, nil, fmt.Errorf("asset %q is not canonical or has invalid sha256", path)
		}
		switch assets[index].Role {
		case "executable", "entrypoint", "asset":
		default:
			return nil, nil, nil, fmt.Errorf("asset %q has invalid role", path)
		}
		if _, exists := seen[path]; exists {
			return nil, nil, nil, fmt.Errorf("duplicate ACP adapter asset %q", path)
		}
		if previous != "" && previous >= path {
			return nil, nil, nil, fmt.Errorf("ACP adapter assets must be strictly sorted by path")
		}
		seen[path], previous, byPath[path] = struct{}{}, path, assets[index]
		if assets[index].Role == "executable" || assets[index].Role == "entrypoint" {
			if _, exists := roles[assets[index].Role]; exists {
				return nil, nil, nil, fmt.Errorf("duplicate ACP adapter %s role", assets[index].Role)
			}
			roles[assets[index].Role] = assets[index]
		}
	}
	if _, ok := roles["executable"]; !ok {
		return nil, nil, nil, fmt.Errorf("ACP adapter bundle is missing executable asset")
	}
	return assets, byPath, roles, nil
}

func validateACPAdapterBundleArgv(root string, argv []string, assets map[string]ACPAdapterBundleAsset, roles map[string]ACPAdapterBundleAsset) error {
	if len(argv) != 1 && len(argv) != 2 {
		return fmt.Errorf("ACP adapter argv must be exactly native executable or interpreter plus entrypoint")
	}
	for index, arg := range argv {
		if arg == "" || arg != strings.TrimSpace(arg) || len(arg) > acpAdapterBundleMaxPathBytes || strings.IndexFunc(arg, unicode.IsControl) >= 0 || !filepath.IsAbs(arg) || filepath.Clean(arg) != arg {
			return fmt.Errorf("ACP adapter argv[%d] must be an exact canonical absolute path", index)
		}
		base := strings.ToLower(filepath.Base(arg))
		for _, forbidden := range []string{"sh", "bash", "zsh", "fish", "cmd", "powershell", "pwsh", "npx", "npm", "pnpm", "yarn"} {
			if base == forbidden || strings.HasPrefix(base, forbidden+".") {
				return fmt.Errorf("ACP adapter argv[%d] rejects shell or package launcher %q", index, base)
			}
		}
		rel, err := filepath.Rel(root, arg)
		if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return fmt.Errorf("ACP adapter argv[%d] must point inside bundle", index)
		}
		declared, ok := assets[filepath.ToSlash(rel)]
		if !ok {
			return fmt.Errorf("ACP adapter argv[%d] is not declared in asset manifest", index)
		}
		role := "executable"
		if index == 1 {
			role = "entrypoint"
		}
		if declared.Role != role || declared.Path != roles[role].Path {
			return fmt.Errorf("ACP adapter argv[%d] does not match %s asset", index, role)
		}
	}
	return nil
}

func readAndHashACPAdapterBundleFile(path string, limit int64, capture bool, role string) ([]byte, string, int64, os.FileInfo, error) {
	before, err := os.Lstat(path)
	if err != nil {
		return nil, "", 0, nil, fmt.Errorf("%s file %q is missing: %w", role, path, err)
	}
	if err := validateACPAdapterBundleFileInfo(before, path, role == "executable"); err != nil {
		return nil, "", 0, nil, err
	}
	if before.Size() < 0 || before.Size() > limit {
		return nil, "", 0, nil, fmt.Errorf("%s file %q exceeds byte limit", role, path)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, "", 0, nil, fmt.Errorf("open %s file %q: %w", role, path, err)
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(before, opened) {
		return nil, "", 0, nil, fmt.Errorf("%s file %q identity changed before hashing", role, path)
	}
	hash := sha256.New()
	var raw bytes.Buffer
	writer := io.Writer(hash)
	if capture {
		writer = io.MultiWriter(hash, &raw)
	}
	written, err := io.Copy(writer, io.LimitReader(file, limit+1))
	if err != nil {
		return nil, "", 0, nil, err
	}
	if written > limit {
		return nil, "", 0, nil, fmt.Errorf("%s file %q exceeds byte limit", role, path)
	}
	afterFD, err := file.Stat()
	if err != nil || !os.SameFile(opened, afterFD) || opened.Size() != afterFD.Size() || !opened.ModTime().Equal(afterFD.ModTime()) {
		return nil, "", 0, nil, fmt.Errorf("%s file %q changed while hashing", role, path)
	}
	afterPath, err := os.Lstat(path)
	if err != nil || !os.SameFile(afterFD, afterPath) {
		return nil, "", 0, nil, fmt.Errorf("%s file %q path identity changed after hashing", role, path)
	}
	if err := validateACPAdapterBundleFileInfo(afterPath, path, role == "executable"); err != nil {
		return nil, "", 0, nil, err
	}
	digest := acpAdapterBundleDigestPrefix + hex.EncodeToString(hash.Sum(nil))
	return raw.Bytes(), digest, written, afterPath, nil
}

func validateACPAdapterBundleFileInfo(info os.FileInfo, path string, executable bool) error {
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("ACP adapter file %q must not be a symlink", path)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("ACP adapter file %q is not regular", path)
	}
	if info.Mode().Perm()&0o222 != 0 {
		return fmt.Errorf("ACP adapter final file %q must not be owner/group/world writable", path)
	}
	if info.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 {
		return fmt.Errorf("ACP adapter final file %q has forbidden setuid/setgid/sticky bits", path)
	}
	if executable && info.Mode()&0o111 == 0 {
		return fmt.Errorf("ACP adapter executable %q is not executable", path)
	}
	if executable {
		if shebang, err := acpExecutableHasShebang(path); err != nil || shebang {
			return fmt.Errorf("ACP adapter executable %q must be a direct non-shebang binary", path)
		}
	}
	return requireACPAdapterBundleFileOwnership(info, path)
}

func validateACPAdapterBundleDirectoryInfo(info os.FileInfo, path string) error {
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("ACP adapter directory %q must be a non-symlink directory", path)
	}
	if info.Mode().Perm()&0o222 != 0 {
		return fmt.Errorf("ACP adapter final directory %q must not be owner/group/world writable", path)
	}
	if info.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 {
		return fmt.Errorf("ACP adapter final directory %q has forbidden setuid/setgid/sticky bits", path)
	}
	uid, _, ok := acpAdapterBundlePOSIXIdentity(info)
	if !ok || uid != uint64(os.Getuid()) {
		return fmt.Errorf("ACP adapter directory %q is not verifiably owned by the current user", path)
	}
	return nil
}

func requireACPAdapterBundleFileOwnership(info os.FileInfo, path string) error {
	uid, nlink, ok := acpAdapterBundlePOSIXIdentity(info)
	if !ok || uid != uint64(os.Getuid()) {
		return fmt.Errorf("ACP adapter file %q is not verifiably owned by the current user", path)
	}
	if nlink != 1 {
		return fmt.Errorf("ACP adapter file %q has unexpected hard links", path)
	}
	return nil
}

func acpAdapterBundlePOSIXIdentity(info os.FileInfo) (uint64, uint64, bool) {
	value := reflect.ValueOf(info.Sys())
	if value.IsValid() && value.Kind() == reflect.Ptr && !value.IsNil() {
		value = value.Elem()
	}
	if !value.IsValid() || value.Kind() != reflect.Struct {
		return 0, 0, false
	}
	uid, nlink := value.FieldByName("Uid"), value.FieldByName("Nlink")
	if !uid.IsValid() || !nlink.IsValid() || !uid.CanUint() || !nlink.CanUint() {
		return 0, 0, false
	}
	return uid.Uint(), nlink.Uint(), true
}

func validateACPAdapterBundleTree(root string, assets []ACPAdapterBundleAsset, manifestPath string, verifiedFiles map[string]os.FileInfo) error {
	declared := make(map[string]struct{}, len(assets)+1)
	roles := make(map[string]string, len(assets)+1)
	declared[manifestPath] = struct{}{}
	roles[manifestPath] = "manifest"
	for _, asset := range assets {
		declared[asset.Path] = struct{}{}
		roles[asset.Path] = asset.Role
	}
	directories := []string{root}
	directoryHasFile := map[string]bool{root: true}
	entries := 1
	var walkDirectory func(string, string, int) error
	walkDirectory = func(directory, relative string, depth int) error {
		handle, err := os.Open(directory)
		if err != nil {
			return err
		}
		defer handle.Close()
		for {
			batch, readErr := handle.ReadDir(128)
			for _, entry := range batch {
				entries++
				if entries > acpAdapterBundleMaxTreeEntries {
					return fmt.Errorf("ACP adapter bundle exceeds tree entry limit")
				}
				childRelative := entry.Name()
				if relative != "" {
					childRelative = filepath.Join(relative, entry.Name())
				}
				childRelative = filepath.ToSlash(childRelative)
				childDepth := depth + 1
				if len(childRelative) > acpAdapterBundleMaxPathBytes || childDepth > acpAdapterBundleMaxTreeDepth {
					return fmt.Errorf("ACP adapter bundle path exceeds tree depth or length limit")
				}
				path := filepath.Join(directory, entry.Name())
				info, err := os.Lstat(path)
				if err != nil {
					return err
				}
				if entry.IsDir() {
					if err := validateACPAdapterBundleDirectoryInfo(info, path); err != nil {
						return err
					}
					directories = append(directories, path)
					if err := walkDirectory(path, childRelative, childDepth); err != nil {
						return err
					}
					continue
				}
				if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
					return fmt.Errorf("bundle contains symlink or non-regular file %q", path)
				}
				if _, ok := declared[childRelative]; !ok {
					return fmt.Errorf("bundle contains undeclared extra file %q", childRelative)
				}
				expected, ok := verifiedFiles[childRelative]
				if !ok || !os.SameFile(expected, info) || expected.Size() != info.Size() || !expected.ModTime().Equal(info.ModTime()) {
					return fmt.Errorf("bundle file %q changed during tree verification", childRelative)
				}
				if err := validateACPAdapterBundleFileInfo(info, path, roles[childRelative] == "executable"); err != nil {
					return err
				}
				for dir := filepath.Dir(path); ; dir = filepath.Dir(dir) {
					directoryHasFile[dir] = true
					if dir == root {
						break
					}
				}
			}
			if readErr == io.EOF {
				return nil
			}
			if readErr != nil {
				return readErr
			}
		}
	}
	if err := walkDirectory(root, "", 0); err != nil {
		return err
	}
	for _, dir := range directories {
		if dir != root && !directoryHasFile[dir] {
			return fmt.Errorf("bundle contains undeclared empty directory %q", dir)
		}
	}
	if len(declared) != len(assets)+1 {
		return fmt.Errorf("ACP adapter manifest/asset declarations overlap")
	}
	return nil
}

type acpAdapterBundlePhysicalFile struct {
	path string
	info os.FileInfo
}

type acpAdapterBundleReceiptPayload struct {
	Schema             string                     `json:"schema"`
	Provider           string                     `json:"provider"`
	Adapter            string                     `json:"adapter"`
	Version            string                     `json:"version"`
	Protocol           string                     `json:"protocol"`
	GOOS               string                     `json:"goos"`
	GOARCH             string                     `json:"goarch"`
	BundleRoot         string                     `json:"bundle_root"`
	ManifestPath       string                     `json:"manifest_path"`
	ManifestFileSHA256 string                     `json:"manifest_file_sha256"`
	LaunchKind         ACPAdapterBundleLaunchKind `json:"launch_kind"`
	Argv               []string                   `json:"argv"`
	Assets             []ACPAdapterBundleAsset    `json:"assets"`
}

func digestACPAdapterBundleReceipt(receipt ACPAdapterBundleVerificationReceipt) (string, error) {
	payload := acpAdapterBundleReceiptPayload{
		Schema: receipt.Schema, Provider: receipt.Provider, Adapter: receipt.Adapter, Version: receipt.Version,
		Protocol: receipt.Protocol, GOOS: receipt.GOOS, GOARCH: receipt.GOARCH,
		BundleRoot: receipt.BundleRoot, ManifestPath: receipt.ManifestPath, ManifestFileSHA256: receipt.ManifestFileSHA256,
		LaunchKind: receipt.LaunchKind, Argv: receipt.Argv, Assets: receipt.Assets,
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return acpAdapterBundleDigestPrefix + hex.EncodeToString(digest[:]), nil
}

func validACPAdapterBundleDigest(value string) bool {
	if value != strings.ToLower(value) || len(value) != len(acpAdapterBundleDigestPrefix)+64 || !strings.HasPrefix(value, acpAdapterBundleDigestPrefix) {
		return false
	}
	_, err := hex.DecodeString(strings.TrimPrefix(value, acpAdapterBundleDigestPrefix))
	return err == nil
}

func SortACPAdapterBundleAssets(assets []ACPAdapterBundleAsset) []ACPAdapterBundleAsset {
	out := append([]ACPAdapterBundleAsset(nil), assets...)
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}
