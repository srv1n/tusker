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
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
	"unicode"
)

const (
	ACPAdapterNPMAdapterVersion = "1.1.14"
	ACPAdapterNPMCodexVersion   = "0.147.0"

	acpAdapterNPMReceiptSchema  = "tusker.acp-adapter-npm-package-receipt/v1"
	acpAdapterNPMIdentitySchema = "tusker.acp-adapter-npm-package-identity/v1"
	acpAdapterNPMPackageName    = "@agentclientprotocol/codex-acp"
	acpAdapterNPMCodexName      = "@openai/codex"
	acpAdapterNPMManifestPath   = "manifest.json"
)

// ACPAdapterNPMPackageRequest names one local npm prefix. Prefix must contain
// a conventional node_modules or lib/node_modules tree; no ambient npm roots
// or PATH entries are consulted.
type ACPAdapterNPMPackageRequest struct {
	StateRoot string
	Prefix    string
	// NodePath may name the exact local Node executable used only as packaging
	// input. When empty, Prefix/bin/node is required for compatibility with
	// already self-contained prefixes. The published bundle always owns its
	// copied Node binary and never consults PATH at attempt runtime.
	NodePath string
}

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

type acpAdapterNPMSourceFile struct {
	Source string
	Asset  ACPAdapterBundleAsset
}

type acpAdapterNPMPackageJSON struct {
	Name                 string            `json:"name"`
	Version              string            `json:"version"`
	Bin                  json.RawMessage   `json:"bin"`
	Scripts              map[string]any    `json:"scripts"`
	Dependencies         map[string]string `json:"dependencies"`
	OptionalDependencies map[string]string `json:"optionalDependencies"`
	PeerDependencies     map[string]string `json:"peerDependencies"`
}

// PackageACPAdapterNPM is the local-first packaging API. It performs no
// process execution. A successful return means the private bundle was sealed,
// atomically published, and accepted by ValidateACPAdapterBundle.
func PackageACPAdapterNPM(request ACPAdapterNPMPackageRequest) (ACPAdapterNPMPackageReceipt, error) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		return ACPAdapterNPMPackageReceipt{}, fmt.Errorf("npm ACP adapter packaging is supported only on darwin and linux")
	}
	prefix, err := canonicalACPAdapterNPMPrefix(request.Prefix)
	if err != nil {
		return ACPAdapterNPMPackageReceipt{}, err
	}
	nodePath := strings.TrimSpace(request.NodePath)
	if nodePath == "" {
		nodePath = filepath.Join(prefix, "bin", "node")
	} else if !filepath.IsAbs(nodePath) || filepath.Clean(nodePath) != nodePath || nodePath != request.NodePath {
		return ACPAdapterNPMPackageReceipt{}, fmt.Errorf("Node path must be an exact absolute local executable")
	}
	physicalNode, err := filepath.EvalSymlinks(nodePath)
	if err != nil || !filepath.IsAbs(physicalNode) {
		return ACPAdapterNPMPackageReceipt{}, fmt.Errorf("resolve bundled Node input")
	}
	nodePath = filepath.Clean(physicalNode)
	if err := validateACPAdapterNPMNativeSource(nodePath); err != nil {
		return ACPAdapterNPMPackageReceipt{}, fmt.Errorf("validate bundled Node: %w", err)
	}

	modulesRoot, err := acpAdapterNPMModulesRoot(prefix)
	if err != nil {
		return ACPAdapterNPMPackageReceipt{}, err
	}
	adapterRoot := filepath.Join(modulesRoot, filepath.FromSlash(acpAdapterNPMPackageName))
	adapterMeta, err := readACPAdapterNPMPackageJSON(adapterRoot, acpAdapterNPMPackageName, ACPAdapterNPMAdapterVersion)
	if err != nil {
		return ACPAdapterNPMPackageReceipt{}, err
	}
	entrypoint, err := acpAdapterNPMEntrypoint(adapterMeta)
	if err != nil {
		return ACPAdapterNPMPackageReceipt{}, err
	}
	codexRoot := filepath.Join(modulesRoot, filepath.FromSlash(acpAdapterNPMCodexName))
	codexMeta, err := readACPAdapterNPMPackageJSON(codexRoot, acpAdapterNPMCodexName, ACPAdapterNPMCodexVersion)
	if err != nil {
		return ACPAdapterNPMPackageReceipt{}, err
	}
	platformName, platformVersion, err := selectACPAdapterNPMPlatformPackage(modulesRoot, codexMeta)
	if err != nil {
		return ACPAdapterNPMPackageReceipt{}, err
	}
	platformRoot := filepath.Join(modulesRoot, filepath.FromSlash(platformName))
	if _, err := readACPAdapterNPMPackageJSON(platformRoot, acpAdapterNPMCodexName, platformVersion); err != nil {
		return ACPAdapterNPMPackageReceipt{}, err
	}

	sources := []acpAdapterNPMSourceFile{}
	nodeDigest, err := hashACPAdapterInstallFile(nodePath, false)
	if err != nil {
		return ACPAdapterNPMPackageReceipt{}, err
	}
	sources = append(sources, acpAdapterNPMSourceFile{Source: nodePath, Asset: ACPAdapterBundleAsset{Path: "bin/node", SHA256: nodeDigest, Role: "executable"}})
	packageRoots, err := resolveACPAdapterNPMRuntimeClosure(modulesRoot, adapterRoot)
	if err != nil {
		return ACPAdapterNPMPackageReceipt{}, err
	}
	if !containsCleanPath(packageRoots, codexRoot) || !containsCleanPath(packageRoots, platformRoot) {
		return ACPAdapterNPMPackageReceipt{}, fmt.Errorf("npm runtime closure omits exact Codex or host platform package")
	}
	entrypointAsset := "node_modules/@agentclientprotocol/codex-acp/" + entrypoint
	topNodeModules := modulesRoot
	for _, packageRoot := range minimalACPAdapterNPMPackageRoots(packageRoots) {
		relative, relErr := filepath.Rel(topNodeModules, packageRoot)
		if relErr != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return ACPAdapterNPMPackageReceipt{}, fmt.Errorf("resolved npm package escaped exact prefix")
		}
		files, collectErr := collectACPAdapterNPMTree(packageRoot, filepath.ToSlash(filepath.Join("node_modules", relative)), entrypointAsset)
		if collectErr != nil {
			return ACPAdapterNPMPackageReceipt{}, collectErr
		}
		sources = append(sources, files...)
	}
	// Re-parse the security-relevant metadata after hashing the closure. A
	// package.json replacement between initial validation and tree collection
	// must fail rather than smuggling different metadata into the bundle.
	adapterMetaAfter, err := readACPAdapterNPMPackageJSON(adapterRoot, acpAdapterNPMPackageName, ACPAdapterNPMAdapterVersion)
	if err != nil {
		return ACPAdapterNPMPackageReceipt{}, err
	}
	if afterEntrypoint, entryErr := acpAdapterNPMEntrypoint(adapterMetaAfter); entryErr != nil || afterEntrypoint != entrypoint {
		return ACPAdapterNPMPackageReceipt{}, fmt.Errorf("codex-acp entrypoint changed while collecting npm runtime closure")
	}
	if _, err := readACPAdapterNPMPackageJSON(codexRoot, acpAdapterNPMCodexName, ACPAdapterNPMCodexVersion); err != nil {
		return ACPAdapterNPMPackageReceipt{}, err
	}
	if _, err := readACPAdapterNPMPackageJSON(platformRoot, acpAdapterNPMCodexName, platformVersion); err != nil {
		return ACPAdapterNPMPackageReceipt{}, err
	}
	entrypoints := 0
	runtimeExecutables := 0
	codexRuntimeExecutables := 0
	platformExecutablePrefix := "node_modules/" + platformName + "/vendor/"
	for _, source := range sources {
		if source.Asset.Role == "entrypoint" {
			entrypoints++
		}
	}
	for index := range sources {
		assetPath := sources[index].Asset.Path
		if !strings.HasPrefix(assetPath, platformExecutablePrefix) {
			continue
		}
		info, statErr := os.Lstat(sources[index].Source)
		if statErr != nil {
			return ACPAdapterNPMPackageReceipt{}, statErr
		}
		if info.Mode()&0o111 == 0 {
			continue
		}
		if err := validateACPAdapterNativeBinary(sources[index].Source); err != nil {
			return ACPAdapterNPMPackageReceipt{}, fmt.Errorf("validate bundled Codex runtime executable: %w", err)
		}
		sources[index].Asset.Role = "runtime_executable"
		runtimeExecutables++
		if strings.HasSuffix(assetPath, "/bin/codex") {
			codexRuntimeExecutables++
		}
	}
	if entrypoints != 1 {
		return ACPAdapterNPMPackageReceipt{}, fmt.Errorf("codex-acp bin entrypoint is missing from its package tree")
	}
	if runtimeExecutables == 0 || codexRuntimeExecutables != 1 {
		return ACPAdapterNPMPackageReceipt{}, fmt.Errorf("Codex host package must contain one native Codex runtime and its executable support files")
	}
	sort.Slice(sources, func(i, j int) bool { return sources[i].Asset.Path < sources[j].Asset.Path })
	if len(sources) == 0 || len(sources) > acpAdapterBundleMaxAssets {
		return ACPAdapterNPMPackageReceipt{}, fmt.Errorf("npm ACP adapter asset count is outside the allowed range")
	}
	var totalBytes int64
	for _, source := range sources {
		info, statErr := os.Lstat(source.Source)
		if statErr != nil || info.Size() < 0 || info.Size() > acpAdapterBundleMaxTotalBytes-totalBytes {
			return ACPAdapterNPMPackageReceipt{}, fmt.Errorf("npm ACP adapter runtime closure exceeds total byte limit")
		}
		totalBytes += info.Size()
	}
	assets := make([]ACPAdapterBundleAsset, len(sources))
	for index := range sources {
		assets[index] = sources[index].Asset
	}
	identity := acpAdapterNPMIdentity{
		Schema: acpAdapterNPMIdentitySchema, GOOS: runtime.GOOS, GOARCH: runtime.GOARCH,
		AdapterVersion: ACPAdapterNPMAdapterVersion, CodexVersion: ACPAdapterNPMCodexVersion,
		PlatformPackage: platformName, PlatformVersion: platformVersion, Files: assets,
	}
	identityRaw, err := json.Marshal(identity)
	if err != nil {
		return ACPAdapterNPMPackageReceipt{}, err
	}
	bundleDigest := acpAdapterBundleDigest(identityRaw)
	installRoot, err := prepareACPAdapterInstallRoot(request.StateRoot)
	if err != nil {
		return ACPAdapterNPMPackageReceipt{}, err
	}
	name := "npm-" + acpAdapterInstallDigestName(bundleDigest)
	finalRoot := filepath.Join(installRoot, "bundles", name)
	receiptPath := filepath.Join(installRoot, "receipts", name+".json")
	if existing, exists, readErr := readACPAdapterNPMPackageReceipt(receiptPath); readErr != nil {
		return ACPAdapterNPMPackageReceipt{}, readErr
	} else if exists {
		if err := validateACPAdapterNPMPackageReceipt(existing, bundleDigest, finalRoot); err != nil {
			return ACPAdapterNPMPackageReceipt{}, fmt.Errorf("existing npm ACP adapter receipt drift: %w", err)
		}
		return existing, nil
	}
	manifest := ACPAdapterBundleManifest{
		Schema: ACPAdapterBundleSchema, Provider: acpAdapterInstallProvider, Adapter: acpAdapterInstallAdapter,
		Version: ACPAdapterNPMAdapterVersion, Protocol: ACPAdapterBundleProtocolV1, GOOS: runtime.GOOS, GOARCH: runtime.GOARCH,
		Argv: []string{filepath.Join(finalRoot, "bin", "node"), filepath.Join(finalRoot, filepath.FromSlash(entrypointAsset))}, Assets: assets,
	}
	manifestRaw, err := json.Marshal(manifest)
	if err != nil {
		return ACPAdapterNPMPackageReceipt{}, err
	}
	manifestDigest := acpAdapterBundleDigest(manifestRaw)
	if _, err := os.Lstat(finalRoot); err == nil {
		recovered, recoverErr := verifyACPAdapterNPMFinalRoot(bundleDigest, nodeDigest, platformName, platformVersion, finalRoot, manifestDigest)
		if recoverErr != nil {
			return ACPAdapterNPMPackageReceipt{}, fmt.Errorf("receipt-less npm ACP adapter final root diverges from requested bundle: %w", recoverErr)
		}
		if err := writeACPAdapterNPMPackageReceipt(receiptPath, recovered); err != nil {
			return ACPAdapterNPMPackageReceipt{}, err
		}
		return recovered, nil
	} else if !os.IsNotExist(err) {
		return ACPAdapterNPMPackageReceipt{}, err
	}

	stage, err := os.MkdirTemp(filepath.Join(installRoot, ".staging"), "npm-package-")
	if err != nil {
		return ACPAdapterNPMPackageReceipt{}, err
	}
	defer os.RemoveAll(stage)
	if err := os.Chmod(stage, 0o700); err != nil {
		return ACPAdapterNPMPackageReceipt{}, err
	}
	stageRoot := filepath.Join(stage, "bundle")
	if err := os.Mkdir(stageRoot, 0o700); err != nil {
		return ACPAdapterNPMPackageReceipt{}, err
	}
	for _, source := range sources {
		destination := filepath.Join(stageRoot, filepath.FromSlash(source.Asset.Path))
		mode := os.FileMode(0o600)
		if source.Asset.Role == "executable" || source.Asset.Role == "runtime_executable" {
			mode = 0o700
		}
		if err := copyACPAdapterNPMFile(source.Source, destination, source.Asset.SHA256, mode); err != nil {
			return ACPAdapterNPMPackageReceipt{}, err
		}
	}
	if err := validateACPAdapterNativeBinary(filepath.Join(stageRoot, "bin", "node")); err != nil {
		return ACPAdapterNPMPackageReceipt{}, fmt.Errorf("staged Node executable is invalid: %w", err)
	}
	if err := writeACPAdapterInstallFile(filepath.Join(stageRoot, acpAdapterNPMManifestPath), manifestRaw, 0o600); err != nil {
		return ACPAdapterNPMPackageReceipt{}, err
	}
	if err := syncACPAdapterNPMTree(stageRoot); err != nil {
		return ACPAdapterNPMPackageReceipt{}, err
	}
	if err := publishACPAdapterBundleExclusive(stageRoot, finalRoot); err != nil {
		return ACPAdapterNPMPackageReceipt{}, fmt.Errorf("publish npm ACP adapter bundle without overwrite: %w", err)
	}
	// Darwin requires the renamed source directory to remain writable. As with
	// the native installer, publish privately first, then seal before emitting
	// any manifest receipt that could make the root discoverable.
	if err := sealACPAdapterNPMTree(finalRoot); err != nil {
		return ACPAdapterNPMPackageReceipt{}, fmt.Errorf("seal published npm ACP adapter bundle: %w", err)
	}
	if err := syncACPAdapterInstallDirectory(filepath.Join(installRoot, "bundles")); err != nil {
		return ACPAdapterNPMPackageReceipt{}, err
	}
	receipt, err := verifyACPAdapterNPMFinalRoot(bundleDigest, nodeDigest, platformName, platformVersion, finalRoot, manifestDigest)
	if err != nil {
		return ACPAdapterNPMPackageReceipt{}, fmt.Errorf("validate packaged npm ACP adapter: %w", err)
	}
	if err := writeACPAdapterNPMPackageReceipt(receiptPath, receipt); err != nil {
		return ACPAdapterNPMPackageReceipt{}, err
	}
	return receipt, nil
}

func canonicalACPAdapterNPMPrefix(prefix string) (string, error) {
	if prefix == "" || prefix != strings.TrimSpace(prefix) || !filepath.IsAbs(prefix) || filepath.Clean(prefix) != prefix || strings.IndexFunc(prefix, unicode.IsControl) >= 0 {
		return "", fmt.Errorf("npm prefix must be an exact absolute local directory")
	}
	info, err := os.Lstat(prefix)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return "", fmt.Errorf("npm prefix must be a non-symlink directory")
	}
	physical, err := filepath.EvalSymlinks(prefix)
	if err != nil || !filepath.IsAbs(physical) {
		return "", fmt.Errorf("resolve npm prefix")
	}
	return filepath.Clean(physical), nil
}

func acpAdapterNPMPackageRoot(prefix, name string) string {
	return filepath.Join(prefix, "lib", "node_modules", filepath.FromSlash(name))
}

func acpAdapterNPMModulesRoot(prefix string) (string, error) {
	candidates := []string{filepath.Join(prefix, "node_modules"), filepath.Join(prefix, "lib", "node_modules")}
	selected := ""
	for _, candidate := range candidates {
		info, err := os.Lstat(candidate)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return "", fmt.Errorf("npm modules root must be a non-symlink directory inside the exact prefix")
		}
		if selected != "" {
			return "", fmt.Errorf("npm prefix ambiguously contains both local and global node_modules roots")
		}
		selected = candidate
	}
	if selected == "" {
		return "", fmt.Errorf("npm prefix has no local or global node_modules root")
	}
	return selected, nil
}

func readACPAdapterNPMPackageJSON(root, expectedName, expectedVersion string) (acpAdapterNPMPackageJSON, error) {
	metadata, err := readACPAdapterNPMUnpinnedPackageJSON(root)
	if err != nil {
		return metadata, err
	}
	if metadata.Name != expectedName || metadata.Version != expectedVersion {
		return metadata, fmt.Errorf("%s must be installed at exact version %s", expectedName, expectedVersion)
	}
	return metadata, nil
}

func readACPAdapterNPMUnpinnedPackageJSON(root string) (acpAdapterNPMPackageJSON, error) {
	path := filepath.Join(root, "package.json")
	raw, err := readACPAdapterNPMSourceFile(path, acpAdapterBundleMaxManifest)
	if err != nil {
		return acpAdapterNPMPackageJSON{}, fmt.Errorf("read npm package metadata at %s: %w", root, err)
	}
	var metadata acpAdapterNPMPackageJSON
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if err := decoder.Decode(&metadata); err != nil {
		return metadata, fmt.Errorf("decode npm package metadata at %s: %w", root, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return metadata, fmt.Errorf("npm package metadata at %s has trailing JSON", root)
	}
	if !validACPAdapterNPMPackageName(metadata.Name) || !validACPAdapterBundleIdentity(metadata.Version) {
		return metadata, fmt.Errorf("npm package metadata at %s has invalid exact identity", root)
	}
	// Published metadata may retain build/publish scripts (the official adapter
	// has prepublishOnly), but install-time hooks are not admissible. This API
	// never executes any script; rejecting install hooks also prevents callers
	// from mistaking this copier for an npm lifecycle runner.
	for _, lifecycle := range []string{"preinstall", "install", "postinstall"} {
		if _, present := metadata.Scripts[lifecycle]; present {
			return metadata, fmt.Errorf("%s declares forbidden npm lifecycle script %s", metadata.Name, lifecycle)
		}
	}
	return metadata, nil
}

func validACPAdapterNPMPackageName(name string) bool {
	if name == "" || name != strings.TrimSpace(name) || strings.IndexFunc(name, unicode.IsControl) >= 0 || strings.Contains(name, "\\") || strings.Contains(name, ":") {
		return false
	}
	parts := strings.Split(name, "/")
	if len(parts) == 1 {
		return parts[0] != "." && parts[0] != ".." && !strings.HasPrefix(parts[0], ".")
	}
	return len(parts) == 2 && strings.HasPrefix(parts[0], "@") && len(parts[0]) > 1 && parts[1] != "" && parts[1] != "." && parts[1] != ".." && !strings.HasPrefix(parts[1], ".")
}

func acpAdapterNPMEntrypoint(metadata acpAdapterNPMPackageJSON) (string, error) {
	var value string
	if err := json.Unmarshal(metadata.Bin, &value); err != nil {
		var bins map[string]string
		if mapErr := json.Unmarshal(metadata.Bin, &bins); mapErr != nil || len(bins) != 1 || bins["codex-acp"] == "" {
			return "", fmt.Errorf("codex-acp package must declare one exact codex-acp bin entrypoint")
		}
		value = bins["codex-acp"]
	}
	value = strings.TrimPrefix(value, "./")
	normalized, err := normalizeACPAdapterBundleRelative(value)
	if err != nil || normalized != value {
		return "", fmt.Errorf("codex-acp bin entrypoint must be canonical and package-relative")
	}
	return normalized, nil
}

func selectACPAdapterNPMPlatformPackage(modulesRoot string, codex acpAdapterNPMPackageJSON) (string, string, error) {
	candidates, err := acpAdapterNPMPlatformCandidates()
	if err != nil {
		return "", "", err
	}
	type installedPlatform struct{ name, version string }
	installed := []installedPlatform{}
	for _, candidate := range candidates {
		declared := codex.OptionalDependencies[candidate]
		if declared == "" {
			declared = codex.Dependencies[candidate]
		}
		if declared == "" {
			continue
		}
		actualName, actualVersion, aliasErr := parseACPAdapterNPMAlias(declared)
		if aliasErr != nil || actualName != acpAdapterNPMCodexName || actualVersion != acpAdapterNPMPlatformVersion(candidate) {
			return "", "", fmt.Errorf("%s dependency on %s must be the exact host package alias", acpAdapterNPMCodexName, candidate)
		}
		if info, statErr := os.Lstat(filepath.Join(modulesRoot, filepath.FromSlash(candidate))); statErr == nil {
			if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
				return "", "", fmt.Errorf("host platform package %s is not a non-symlink directory", candidate)
			}
			installed = append(installed, installedPlatform{name: candidate, version: actualVersion})
		} else if !os.IsNotExist(statErr) {
			return "", "", statErr
		}
	}
	if len(installed) != 1 {
		return "", "", fmt.Errorf("exactly one declared host platform package must exist under the npm prefix; found %d", len(installed))
	}
	return installed[0].name, installed[0].version, nil
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

func parseACPAdapterNPMAlias(value string) (string, string, error) {
	const prefix = "npm:"
	if !strings.HasPrefix(value, prefix) {
		return "", "", fmt.Errorf("dependency is not an exact npm alias")
	}
	value = strings.TrimPrefix(value, prefix)
	separator := strings.LastIndex(value, "@")
	if separator <= 0 || separator == len(value)-1 {
		return "", "", fmt.Errorf("dependency alias is malformed")
	}
	name, version := value[:separator], value[separator+1:]
	if !validACPAdapterNPMPackageName(name) || !validACPAdapterBundleIdentity(version) {
		return "", "", fmt.Errorf("dependency alias is not canonical")
	}
	return name, version, nil
}

func resolveACPAdapterNPMRuntimeClosure(top, adapterRoot string) ([]string, error) {
	top = filepath.Clean(top)
	queue := []string{adapterRoot}
	seen := map[string]bool{}
	roots := []string{}
	for len(queue) > 0 {
		root := filepath.Clean(queue[0])
		queue = queue[1:]
		if seen[root] {
			continue
		}
		if !pathWithin(top, root) {
			return nil, fmt.Errorf("resolved npm dependency escaped exact prefix: %s", root)
		}
		metadata, err := readACPAdapterNPMUnpinnedPackageJSON(root)
		if err != nil {
			return nil, err
		}
		seen[root] = true
		roots = append(roots, root)
		type dependency struct {
			name, requirement string
			optional          bool
		}
		dependencies := make([]dependency, 0, len(metadata.Dependencies)+len(metadata.OptionalDependencies)+len(metadata.PeerDependencies))
		for name, requirement := range metadata.Dependencies {
			dependencies = append(dependencies, dependency{name: name, requirement: requirement})
		}
		for name, requirement := range metadata.OptionalDependencies {
			dependencies = append(dependencies, dependency{name: name, requirement: requirement, optional: true})
		}
		for name, requirement := range metadata.PeerDependencies {
			dependencies = append(dependencies, dependency{name: name, requirement: requirement, optional: true})
		}
		sort.Slice(dependencies, func(i, j int) bool { return dependencies[i].name < dependencies[j].name })
		for _, dependency := range dependencies {
			if !validACPAdapterNPMPackageName(dependency.name) {
				return nil, fmt.Errorf("%s declares invalid dependency name %q", metadata.Name, dependency.name)
			}
			resolved, exists, resolveErr := resolveACPAdapterNPMDependency(top, root, dependency.name)
			if resolveErr != nil {
				return nil, resolveErr
			}
			if !exists {
				if dependency.optional {
					continue
				}
				return nil, fmt.Errorf("required npm dependency %s for %s is missing from exact prefix", dependency.name, metadata.Name)
			}
			resolvedMetadata, metadataErr := readACPAdapterNPMUnpinnedPackageJSON(resolved)
			if metadataErr != nil {
				return nil, metadataErr
			}
			expectedName := dependency.name
			expectedVersion := ""
			if strings.HasPrefix(dependency.requirement, "npm:") {
				expectedName, expectedVersion, metadataErr = parseACPAdapterNPMAlias(dependency.requirement)
				if metadataErr != nil {
					return nil, fmt.Errorf("%s has invalid npm alias for %s", metadata.Name, dependency.name)
				}
			}
			if resolvedMetadata.Name != expectedName || (expectedVersion != "" && resolvedMetadata.Version != expectedVersion) {
				return nil, fmt.Errorf("resolved npm dependency identity drift for %s", dependency.name)
			}
			queue = append(queue, resolved)
		}
	}
	sort.Strings(roots)
	return roots, nil
}

func resolveACPAdapterNPMDependency(top, packageRoot, name string) (string, bool, error) {
	if !validACPAdapterNPMPackageName(name) {
		return "", false, fmt.Errorf("invalid npm dependency name")
	}
	candidates := []string{}
	for current := filepath.Clean(packageRoot); pathWithin(top, current) && current != filepath.Clean(top); current = filepath.Dir(current) {
		candidates = append(candidates, filepath.Join(current, "node_modules", filepath.FromSlash(name)))
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
	}
	candidates = append(candidates, filepath.Join(top, filepath.FromSlash(name)))
	for _, candidate := range candidates {
		clean := filepath.Clean(candidate)
		if !pathWithin(top, clean) {
			return "", false, fmt.Errorf("npm dependency candidate escaped exact prefix")
		}
		info, err := os.Lstat(clean)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return "", false, err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return "", false, fmt.Errorf("npm dependency %s resolved to non-directory or symlink", name)
		}
		return clean, true, nil
	}
	return "", false, nil
}

func minimalACPAdapterNPMPackageRoots(roots []string) []string {
	sorted := append([]string(nil), roots...)
	sort.Slice(sorted, func(i, j int) bool {
		if len(sorted[i]) == len(sorted[j]) {
			return sorted[i] < sorted[j]
		}
		return len(sorted[i]) < len(sorted[j])
	})
	minimal := []string{}
	for _, root := range sorted {
		covered := false
		for _, parent := range minimal {
			if pathWithin(parent, root) {
				covered = true
				break
			}
		}
		if !covered {
			minimal = append(minimal, root)
		}
	}
	sort.Strings(minimal)
	return minimal
}

func containsCleanPath(paths []string, wanted string) bool {
	wanted = filepath.Clean(wanted)
	for _, path := range paths {
		if filepath.Clean(path) == wanted {
			return true
		}
	}
	return false
}

func collectACPAdapterNPMTree(root, destination, entrypoint string) ([]acpAdapterNPMSourceFile, error) {
	rootInfo, err := os.Lstat(root)
	if err != nil || rootInfo.Mode()&os.ModeSymlink != 0 || !rootInfo.IsDir() {
		return nil, fmt.Errorf("npm package root is missing or unsafe: %s", root)
	}
	files := []acpAdapterNPMSourceFile{}
	err = filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("npm package tree contains symlink: %s", path)
		}
		relative, err := filepath.Rel(root, path)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return fmt.Errorf("npm package path escapes package root")
		}
		if relative == "." {
			return nil
		}
		bundlePath := filepath.ToSlash(filepath.Join(destination, relative))
		if _, err := normalizeACPAdapterBundleRelative(bundlePath); err != nil {
			return fmt.Errorf("npm package path is not canonical: %s", bundlePath)
		}
		if info.IsDir() {
			return nil
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("npm package tree contains non-regular file: %s", path)
		}
		if info.Size() < 0 || info.Size() > acpAdapterBundleMaxFile {
			return fmt.Errorf("npm package file exceeds byte limit: %s", path)
		}
		if err := requireACPAdapterBundleFileOwnership(info, path); err != nil {
			return err
		}
		digest, err := hashACPAdapterInstallFile(path, false)
		if err != nil {
			return err
		}
		role := "asset"
		if bundlePath == entrypoint {
			role = "entrypoint"
		}
		files = append(files, acpAdapterNPMSourceFile{Source: path, Asset: ACPAdapterBundleAsset{Path: bundlePath, SHA256: digest, Role: role}})
		if len(files) > acpAdapterBundleMaxAssets {
			return fmt.Errorf("npm package tree exceeds asset count limit")
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}

func validateACPAdapterNPMNativeSource(path string) error {
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Mode()&0o111 == 0 {
		return fmt.Errorf("Node must be a direct executable regular file at prefix/bin/node")
	}
	if err := requireACPAdapterBundleFileOwnership(info, path); err != nil {
		return err
	}
	return validateACPAdapterNativeBinary(path)
}

func readACPAdapterNPMSourceFile(path string, limit int64) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Size() < 0 || info.Size() > limit {
		return nil, fmt.Errorf("source is not a bounded regular non-symlink file")
	}
	if err := requireACPAdapterBundleFileOwnership(info, path); err != nil {
		return nil, err
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil || int64(len(raw)) > limit {
		return nil, fmt.Errorf("source exceeds byte limit")
	}
	after, err := file.Stat()
	if err != nil || !os.SameFile(info, after) || info.Size() != after.Size() || !info.ModTime().Equal(after.ModTime()) {
		return nil, fmt.Errorf("source changed while reading")
	}
	return raw, nil
}

func copyACPAdapterNPMFile(source, destination, expectedDigest string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return err
	}
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	before, err := input.Stat()
	if err != nil {
		return err
	}
	output, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	written, copyErr := io.Copy(output, io.LimitReader(input, acpAdapterBundleMaxFile+1))
	if copyErr == nil && written > acpAdapterBundleMaxFile {
		copyErr = fmt.Errorf("npm package file exceeds byte limit while copying")
	}
	if copyErr == nil {
		copyErr = output.Sync()
	}
	if closeErr := output.Close(); copyErr == nil {
		copyErr = closeErr
	}
	if copyErr != nil {
		return copyErr
	}
	after, err := input.Stat()
	if err != nil || !os.SameFile(before, after) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		return fmt.Errorf("npm package source changed while copying: %s", source)
	}
	digest, err := hashACPAdapterInstallFile(destination, false)
	if err != nil || digest != expectedDigest {
		return fmt.Errorf("staged npm package file fingerprint drift: %s", source)
	}
	return nil
}

func sealACPAdapterNPMTree(root string) error {
	directories := []string{}
	err := filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("npm ACP adapter stage contains symlink")
		}
		if info.IsDir() {
			directories = append(directories, path)
			return nil
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("npm ACP adapter stage contains non-regular file")
		}
		mode := os.FileMode(0o400)
		if info.Mode()&0o111 != 0 {
			mode = 0o500
		}
		return os.Chmod(path, mode)
	})
	if err != nil {
		return err
	}
	for index := len(directories) - 1; index >= 0; index-- {
		if err := os.Chmod(directories[index], 0o500); err != nil {
			return err
		}
		if err := syncACPAdapterInstallDirectory(directories[index]); err != nil {
			return err
		}
	}
	return nil
}

func syncACPAdapterNPMTree(root string) error {
	directories := []string{}
	err := filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("npm ACP adapter stage contains symlink")
		}
		if info.IsDir() {
			directories = append(directories, path)
		}
		return nil
	})
	if err != nil {
		return err
	}
	for index := len(directories) - 1; index >= 0; index-- {
		if err := syncACPAdapterInstallDirectory(directories[index]); err != nil {
			return err
		}
	}
	return nil
}

func acpAdapterNPMValidationRequest(root, manifestDigest, finalRootDigest string) ACPAdapterBundleValidationRequest {
	return ACPAdapterBundleValidationRequest{
		BundleRoot: root, ManifestPath: acpAdapterNPMManifestPath, ExpectedManifestSHA256: manifestDigest,
		ExpectedDescriptor: ACPAdapterBundleDescriptorPolicy{Provider: acpAdapterInstallProvider, Adapter: acpAdapterInstallAdapter, Version: ACPAdapterNPMAdapterVersion, LaunchKind: ACPAdapterBundleLaunchInterpreter},
		ExpectedFinalRoot:  root, ExpectedFinalRootDigest: finalRootDigest, TrustCurrentUserBoundary: true,
		ProviderAllowed: func(provider string) bool { return provider == acpAdapterInstallProvider },
	}
}

func verifyACPAdapterNPMFinalRoot(bundleDigest, nodeDigest, platformName, platformVersion, finalRoot, manifestDigest string) (ACPAdapterNPMPackageReceipt, error) {
	finalRootDigest, err := ACPAdapterBundleFinalRootDigest(finalRoot, manifestDigest)
	if err != nil {
		return ACPAdapterNPMPackageReceipt{}, err
	}
	verified, err := ValidateACPAdapterBundle(acpAdapterNPMValidationRequest(finalRoot, manifestDigest, finalRootDigest))
	if err != nil {
		return ACPAdapterNPMPackageReceipt{}, err
	}
	receipt := ACPAdapterNPMPackageReceipt{
		Schema: acpAdapterNPMReceiptSchema, BundleDigest: bundleDigest, NodeSHA256: nodeDigest,
		AdapterVersion: ACPAdapterNPMAdapterVersion, CodexVersion: ACPAdapterNPMCodexVersion,
		PlatformPackage: platformName, PlatformVersion: platformVersion,
		ManifestSHA256: manifestDigest, FinalRootDigest: finalRootDigest, Bundle: verified,
	}
	if err := validateACPAdapterNPMPackageReceipt(receipt, bundleDigest, finalRoot); err != nil {
		return ACPAdapterNPMPackageReceipt{}, err
	}
	return receipt, nil
}

func writeACPAdapterNPMPackageReceipt(path string, receipt ACPAdapterNPMPackageReceipt) error {
	raw, err := json.Marshal(receipt)
	if err != nil {
		return err
	}
	stage := path + ".stage-" + fmt.Sprintf("%d", time.Now().UnixNano())
	if err := writeACPAdapterInstallFile(stage, raw, 0o400); err != nil {
		return err
	}
	defer os.Remove(stage)
	if err := os.Link(stage, path); err != nil {
		return fmt.Errorf("publish npm ACP adapter receipt without overwrite: %w", err)
	}
	if err := os.Remove(stage); err != nil {
		return err
	}
	return syncACPAdapterInstallDirectory(filepath.Dir(path))
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
