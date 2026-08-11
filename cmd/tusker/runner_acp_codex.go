package main

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"tusker/internal/acp"
)

// codexACPRunnerName is intentionally local to this provider slice. Factory
// wiring must promote this to a persisted RunnerName only after it has bound
// the descriptor fingerprint to StartRequest and the wrapper dispatch path.
// In particular, it must never reinterpret legacy codex, codex_exec, or
// codex_cloud records as this protocol runner.
const codexACPRunnerName RunnerName = "codex_acp"

const (
	codexACPProvider           = "codex"
	codexACPProtocol           = "acp/v1"
	codexACPSessionRefVersion  = "tusker.codex-acp-session/v1"
	codexACPDefaultFixedPath   = "/usr/local/bin:/opt/homebrew/bin:/usr/bin:/bin:/usr/sbin:/sbin"
	codexACPMaxAdapterVersion  = 128
	codexACPMaxProviderSession = 512
)

// CodexACPImmutableAsset is an install-time declaration. Path and Fingerprint
// are both part of the provider-local descriptor fingerprint, then the current
// bytes are compared to Fingerprint just before launch. It proves only listed
// assets; shared integration must also require a verified bundle receipt that
// proves the declaration is complete and has no unlisted runtime dependencies.
type CodexACPImmutableAsset struct {
	Path        string
	Fingerprint string
}

// CodexACPDescriptor describes an already-installed codex-acp adapter. Launch
// is either one exact native executable or an exact bundled Node interpreter
// plus exact bundled JavaScript entrypoint. It is never npx, a shell, PATH
// lookup, or a package manager. External CODEX_PATH overrides remain refused.
//
// ManifestFingerprint is sha256 over the versioned, canonical local manifest
// that includes the native adapter with bundled Codex and every declared
// asset. It is not an arbitrary version label and is the adapter binding used
// in persisted session references.
type CodexACPDescriptor struct {
	AdapterVersion      string
	LaunchKind          ACPAdapterBundleLaunchKind
	Adapter             CodexACPImmutableAsset
	Entrypoint          CodexACPImmutableAsset
	Assets              []CodexACPImmutableAsset
	ManifestFingerprint string
	Model               string
	Effort              string
	Mode                CodexACPMode
}

// CodexACPMode is intentionally provider-facing, not a direct Tusker
// permission grant. A later factory must still route every tool callback into
// ACPPermissionPolicy; INITIAL_AGENT_MODE merely selects the adapter's initial
// behavior before a callback exists.
type CodexACPMode string

const (
	CodexACPModeReadOnly       CodexACPMode = "read_only"
	CodexACPModeWorkspaceWrite CodexACPMode = "workspace_write"
	CodexACPModeFullAccess     CodexACPMode = "full_access"
)

// CodexACPReadinessRequirement is a human-facing, non-authorizing readiness
// contract. It intentionally separates installed/conformant/authenticated
// from a live task admission, which remains owned by the daemon integration.
type CodexACPReadinessRequirement struct {
	ID          string
	Description string
	Required    bool
}

func CodexACPReadinessRequirements() []CodexACPReadinessRequirement {
	return []CodexACPReadinessRequirement{
		{ID: "adapter_manifest", Required: true, Description: "the pinned adapter runtime and declared assets are present and match the local manifest; external CODEX_PATH is unsupported"},
		{ID: "acp_conformance", Required: true, Description: "the exact manifest fingerprint passed ACP conformance and an explicitly authorized authenticated smoke without a shell or runtime download"},
		{ID: "codex_auth", Required: true, Description: "the selected Codex authentication path is available to the adapter process; this may be ChatGPT login or an explicit API credential"},
		{ID: "task_authorization", Required: true, Description: "a separate Tusker lease, permission policy, and human launch authority exist for the target attempt"},
		{ID: "permission_parity", Required: true, Description: "workspace edits and commands are one-shot-authorized only inside the bound workspace; network remains controlled by the resolved profile"},
	}
}

// CodexACPModeForPermissionPreset is a one-way mapping of existing profile
// vocabulary into codex-acp's INITIAL_AGENT_MODE. It does not grant tool
// permission: the ACP broker remains the final fail-closed decision point.
func CodexACPModeForPermissionPreset(preset string) (CodexACPMode, error) {
	switch strings.TrimSpace(preset) {
	case "read-only":
		return CodexACPModeReadOnly, nil
	case "workspace-write-network", "workspace-write-offline":
		return CodexACPModeWorkspaceWrite, nil
	case "danger-full-access":
		return "", errors.New("codex ACP full-access mode is blocked until execute and edit permission parity is safe")
	default:
		return "", fmt.Errorf("codex ACP does not recognize permission preset %q", preset)
	}
}

func (m CodexACPMode) initialAgentMode() (string, error) {
	switch m {
	case CodexACPModeReadOnly:
		return "read-only", nil
	case CodexACPModeWorkspaceWrite:
		return "agent", nil
	case CodexACPModeFullAccess:
		return "agent-full-access", nil
	default:
		return "", fmt.Errorf("invalid codex ACP mode %q", m)
	}
}

// LaunchArgv revalidates an externally anchored bundle receipt immediately
// before returning the exact native or interpreter-plus-entrypoint invocation
// accepted by the generic ACP process runtime. The provider descriptor alone is self-description and must
// never authorize launch: request carries the separately trusted manifest/root
// policy, while receipt proves the complete bytes previously admitted by that
// policy.
func (d CodexACPDescriptor) LaunchArgv(request ACPAdapterBundleValidationRequest, receipt ACPAdapterBundleVerificationReceipt) ([]string, error) {
	if err := d.Validate(); err != nil {
		return nil, err
	}
	if err := RevalidateACPAdapterBundleReceipt(request, receipt); err != nil {
		return nil, err
	}
	adapter, err := codexACPManifestPath(d.Adapter.Path)
	if err != nil {
		return nil, err
	}
	launchKind := d.LaunchKind
	if launchKind == "" {
		launchKind = ACPAdapterBundleLaunchNative
	}
	if receipt.Schema != ACPAdapterBundleVerificationSchema || receipt.Provider != codexACPProvider || receipt.Adapter != "codex-acp" ||
		receipt.Version != strings.TrimSpace(d.AdapterVersion) || receipt.Protocol != codexACPProtocol ||
		receipt.LaunchKind != launchKind ||
		(len(receipt.Argv) != 1 && len(receipt.Argv) != 2) || receipt.Argv[0] != adapter {
		return nil, errors.New("codex ACP launch is not bound to the verified adapter bundle receipt")
	}
	if launchKind == ACPAdapterBundleLaunchInterpreter {
		entrypoint, resolveErr := codexACPManifestPath(d.Entrypoint.Path)
		if resolveErr != nil || len(receipt.Argv) != 2 || receipt.Argv[1] != entrypoint {
			return nil, errors.New("codex ACP interpreter entrypoint is not bound to the verified bundle receipt")
		}
	} else if len(receipt.Argv) != 1 {
		return nil, errors.New("codex ACP native launch receipt has unexpected arguments")
	}
	relativeAdapter, err := filepath.Rel(receipt.BundleRoot, adapter)
	if err != nil || relativeAdapter == "." || relativeAdapter == ".." || strings.HasPrefix(relativeAdapter, ".."+string(filepath.Separator)) {
		return nil, errors.New("codex ACP adapter is outside the verified bundle root")
	}
	adapterAsset := filepath.ToSlash(relativeAdapter)
	matched := false
	for _, asset := range receipt.Assets {
		if asset.Path == adapterAsset && asset.Role == "executable" && asset.SHA256 == strings.TrimSpace(d.Adapter.Fingerprint) {
			matched = true
			break
		}
	}
	if !matched {
		return nil, errors.New("codex ACP adapter executable is not the verified bundle executable")
	}
	return append([]string(nil), receipt.Argv...), nil
}

// Validate verifies both the descriptor's canonical manifest and its present
// immutable files. It is deliberately an install/readiness operation; a
// factory integration must call it again immediately before handoff to the
// fenced generic ACP runtime.
func (d CodexACPDescriptor) Validate() error {
	if !validCodexACPAdapterVersion(d.AdapterVersion) {
		return fmt.Errorf("invalid codex ACP adapter version")
	}
	launchKind := d.LaunchKind
	if launchKind == "" {
		launchKind = ACPAdapterBundleLaunchNative
	}
	switch launchKind {
	case ACPAdapterBundleLaunchNative:
		if !isCodexACPStandaloneAdapter(d.Adapter.Path) {
			return fmt.Errorf("native codex ACP adapter must be the standalone codex-acp executable")
		}
	case ACPAdapterBundleLaunchInterpreter:
		if strings.ToLower(filepath.Base(d.Adapter.Path)) != "node" || strings.TrimSpace(d.Entrypoint.Path) == "" {
			return fmt.Errorf("interpreter codex ACP adapter requires exact Node and JavaScript entrypoint assets")
		}
	default:
		return fmt.Errorf("invalid codex ACP launch kind")
	}
	if strings.TrimSpace(d.Model) == "" || len(d.Model) > 256 || containsControl(d.Model) {
		return fmt.Errorf("invalid codex ACP model")
	}
	if strings.TrimSpace(d.Effort) != "" && (len(d.Effort) > 64 || containsControl(d.Effort)) {
		return fmt.Errorf("invalid codex ACP effort")
	}
	if _, err := d.Mode.initialAgentMode(); err != nil {
		return err
	}
	if d.Mode != CodexACPModeReadOnly && d.Mode != CodexACPModeWorkspaceWrite {
		return errors.New("codex ACP provider slice admits only read-only or workspace-write mode")
	}
	if err := validateCodexACPAsset(d.Adapter, "adapter", true); err != nil {
		return err
	}
	seen := map[string]struct{}{filepath.Clean(d.Adapter.Path): {}}
	if launchKind == ACPAdapterBundleLaunchInterpreter {
		if err := validateCodexACPAsset(d.Entrypoint, "entrypoint", false); err != nil {
			return err
		}
		seen[filepath.Clean(d.Entrypoint.Path)] = struct{}{}
	}
	for index, asset := range d.Assets {
		path := filepath.Clean(asset.Path)
		if _, exists := seen[path]; exists {
			return fmt.Errorf("codex ACP manifest repeats asset %q", path)
		}
		seen[path] = struct{}{}
		if err := validateCodexACPAsset(asset, fmt.Sprintf("asset[%d]", index), false); err != nil {
			return err
		}
	}
	if !v7CloseAuthorityDigest(strings.TrimSpace(d.ManifestFingerprint), "sha256:") {
		return fmt.Errorf("invalid codex ACP manifest fingerprint")
	}
	calculated, err := d.ManifestFingerprintForDescriptor()
	if err != nil {
		return err
	}
	if calculated != d.ManifestFingerprint {
		return fmt.Errorf("codex ACP manifest fingerprint does not bind its descriptor")
	}
	return nil
}

// ManifestFingerprintForDescriptor calculates a canonical fingerprint over
// declared paths and expected content fingerprints, rather than current file
// bytes. Validate separately compares the current bytes to the declaration.
func (d CodexACPDescriptor) ManifestFingerprintForDescriptor() (string, error) {
	if !validCodexACPAdapterVersion(d.AdapterVersion) {
		return "", fmt.Errorf("invalid codex ACP adapter version")
	}
	adapterPath, err := codexACPManifestPath(d.Adapter.Path)
	if err != nil {
		return "", err
	}
	launchKind := d.LaunchKind
	if launchKind == "" {
		launchKind = ACPAdapterBundleLaunchNative
	}
	items := []codexACPManifestItem{
		{Role: "adapter", Path: adapterPath, Fingerprint: strings.TrimSpace(d.Adapter.Fingerprint)},
	}
	if launchKind == ACPAdapterBundleLaunchInterpreter {
		entrypointPath, err := codexACPManifestPath(d.Entrypoint.Path)
		if err != nil {
			return "", err
		}
		items = append(items, codexACPManifestItem{Role: "entrypoint", Path: entrypointPath, Fingerprint: strings.TrimSpace(d.Entrypoint.Fingerprint)})
	}
	for _, asset := range d.Assets {
		path, err := codexACPManifestPath(asset.Path)
		if err != nil {
			return "", err
		}
		items = append(items, codexACPManifestItem{Role: "asset", Path: path, Fingerprint: strings.TrimSpace(asset.Fingerprint)})
	}
	for _, item := range items {
		if !filepath.IsAbs(item.Path) || !v7CloseAuthorityDigest(item.Fingerprint, "sha256:") {
			return "", fmt.Errorf("invalid codex ACP manifest item")
		}
	}
	firstAsset := len(items)
	for index, item := range items {
		if item.Role == "asset" {
			firstAsset = index
			break
		}
	}
	sort.Slice(items[firstAsset:], func(i, j int) bool {
		left, right := items[i+firstAsset], items[j+firstAsset]
		if left.Path == right.Path {
			return left.Fingerprint < right.Fingerprint
		}
		return left.Path < right.Path
	})
	encoded, err := json.Marshal(struct {
		Schema         string                 `json:"schema"`
		Provider       string                 `json:"provider"`
		Protocol       string                 `json:"protocol"`
		AdapterVersion string                 `json:"adapter_version"`
		LaunchKind     string                 `json:"launch_kind"`
		Items          []codexACPManifestItem `json:"items"`
	}{
		Schema: codexACPSessionRefVersion + ":manifest", Provider: codexACPProvider, Protocol: codexACPProtocol,
		AdapterVersion: strings.TrimSpace(d.AdapterVersion), LaunchKind: string(launchKind), Items: items,
	})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

type codexACPManifestItem struct {
	Role        string `json:"role"`
	Path        string `json:"path"`
	Fingerprint string `json:"fingerprint"`
}

func validateCodexACPAsset(asset CodexACPImmutableAsset, role string, executable bool) error {
	path := strings.TrimSpace(asset.Path)
	if !filepath.IsAbs(path) || !v7CloseAuthorityDigest(strings.TrimSpace(asset.Fingerprint), "sha256:") {
		return fmt.Errorf("invalid codex ACP %s manifest entry", role)
	}
	clean := filepath.Clean(path)
	leaf, err := os.Lstat(clean)
	if err != nil || leaf.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("codex ACP %s must be an existing non-symlink absolute file", role)
	}
	physical, err := codexACPManifestPath(clean)
	if err != nil {
		return fmt.Errorf("codex ACP %s must be an existing non-symlink absolute file", role)
	}
	info, err := os.Stat(clean)
	if err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("codex ACP %s is not a regular file", role)
	}
	if executable && info.Mode()&0o111 == 0 {
		return fmt.Errorf("codex ACP executable is not executable")
	}
	if shebang, err := acpExecutableHasShebang(physical); err != nil || (executable && shebang) {
		return fmt.Errorf("codex ACP %s must not be a shebang launcher", role)
	}
	actual, err := acpExecutableFingerprint(physical)
	if err != nil || actual != strings.TrimSpace(asset.Fingerprint) {
		return fmt.Errorf("codex ACP %s fingerprint drift", role)
	}
	if executable && isCodexACPShellOrPackageLauncher(physical) {
		return fmt.Errorf("codex ACP executable cannot be a shell or package launcher")
	}
	return nil
}

// codexACPManifestPath permits a symlinked parent such as macOS's /var ->
// /private/var, but callers separately reject a symlink at the declared asset
// leaf. The resolved absolute spelling is what enters the manifest and argv.
func codexACPManifestPath(path string) (string, error) {
	path = filepath.Clean(strings.TrimSpace(path))
	if !filepath.IsAbs(path) {
		return "", errors.New("Codex ACP asset path is not absolute")
	}
	physical, err := filepath.EvalSymlinks(path)
	if err != nil || !filepath.IsAbs(physical) {
		return "", errors.New("Codex ACP asset path could not be resolved")
	}
	return filepath.Clean(physical), nil
}

func isCodexACPStandaloneAdapter(path string) bool {
	base := strings.ToLower(filepath.Base(path))
	return base == "codex-acp" || strings.HasPrefix(base, "codex-acp-") || strings.HasPrefix(base, "codex-acp_")
}

func isCodexACPShellOrPackageLauncher(path string) bool {
	switch strings.ToLower(filepath.Base(path)) {
	case "sh", "bash", "zsh", "fish", "dash", "npx", "npm", "yarn", "pnpm":
		return true
	default:
		return false
	}
}

func validCodexACPAdapterVersion(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > codexACPMaxAdapterVersion {
		return false
	}
	for _, r := range value {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '-' || r == '_') {
			return false
		}
	}
	return true
}

func containsControl(value string) bool {
	return strings.IndexFunc(value, unicode.IsControl) >= 0
}

type CodexACPAuthSource string

const (
	CodexACPAuthChatGPTSession CodexACPAuthSource = "chatgpt_session"
	CodexACPAuthCodexAPIKey    CodexACPAuthSource = "codex_api_key"
	CodexACPAuthOpenAIAPIKey   CodexACPAuthSource = "openai_api_key"
)

// CodexACPAuthContract selects exactly one credential source. PrincipalDigest
// is a non-secret sha256 identity/correlation value supplied by the control
// plane; it must never be derived from or replaced with the credential itself.
type CodexACPAuthContract struct {
	Source          CodexACPAuthSource
	PrincipalDigest string
}

type CodexACPAuthIdentity struct {
	Method          string
	PrincipalDigest string
}

type CodexACPEnvironmentResult struct {
	Variables []string
	Auth      CodexACPAuthIdentity
}

func (a CodexACPAuthContract) environmentKey() (string, error) {
	switch a.Source {
	case CodexACPAuthChatGPTSession:
		return "CODEX_HOME", nil
	case CodexACPAuthCodexAPIKey:
		return "CODEX_API_KEY", nil
	case CodexACPAuthOpenAIAPIKey:
		return "OPENAI_API_KEY", nil
	default:
		return "", fmt.Errorf("invalid codex ACP auth source %q", a.Source)
	}
}

// CodexACPEnvironment builds a positive environment plus a non-secret auth
// receipt. Exactly one selected auth variable crosses the process boundary;
// HOME, XDG auth homes, unselected API keys, CODEX_PATH, and TUSKER_* never do.
func (d CodexACPDescriptor) CodexACPEnvironment(inherited []string, auth CodexACPAuthContract) (CodexACPEnvironmentResult, error) {
	if err := d.Validate(); err != nil {
		return CodexACPEnvironmentResult{}, err
	}
	authKey, err := auth.environmentKey()
	if err != nil || !v7CloseAuthorityDigest(strings.TrimSpace(auth.PrincipalDigest), "sha256:") {
		return CodexACPEnvironmentResult{}, errors.New("codex ACP auth contract requires one valid source and non-secret principal digest")
	}
	authValue, err := exactCodexACPEnvironmentValue(inherited, authKey)
	if err != nil && auth.Source == CodexACPAuthChatGPTSession {
		// The normal Codex installation stores its authenticated session under
		// ~/.codex even when CODEX_HOME is not exported by the interactive app.
		// Resolve that conventional local path once and still pass it explicitly;
		// the adapter never receives ambient HOME or an unbounded environment.
		candidate := filepath.Join(userHomeDir(), ".codex")
		if info, statErr := os.Stat(candidate); statErr == nil && info.IsDir() {
			authValue = candidate
			err = nil
		}
	}
	if err != nil {
		return CodexACPEnvironmentResult{}, err
	}
	if auth.Source == CodexACPAuthChatGPTSession {
		if !filepath.IsAbs(authValue) {
			return CodexACPEnvironmentResult{}, errors.New("codex ACP ChatGPT auth requires an absolute CODEX_HOME")
		}
		resolved, resolveErr := filepath.EvalSymlinks(authValue)
		info, statErr := os.Stat(resolved)
		if resolveErr != nil || statErr != nil || !info.IsDir() {
			return CodexACPEnvironmentResult{}, errors.New("codex ACP ChatGPT CODEX_HOME is not an existing directory")
		}
		authValue = filepath.Clean(resolved)
	}
	config, err := json.Marshal(struct {
		Model  string `json:"model"`
		Effort string `json:"model_reasoning_effort,omitempty"`
	}{Model: strings.TrimSpace(d.Model), Effort: strings.TrimSpace(d.Effort)})
	if err != nil {
		return CodexACPEnvironmentResult{}, err
	}
	mode, err := d.Mode.initialAgentMode()
	if err != nil {
		return CodexACPEnvironmentResult{}, err
	}
	allowed := map[string]bool{
		"TMPDIR": true, "LANG": true, "TERM": true, "LC_ALL": true, "LC_CTYPE": true,
		"SSL_CERT_FILE": true, "SSL_CERT_DIR": true,
	}
	values := make(map[string]string, len(allowed)+4)
	for _, entry := range inherited {
		key, value, ok := strings.Cut(entry, "=")
		if !ok || strings.HasPrefix(key, "TUSKER_") || !allowed[key] || containsControl(key) || containsControl(value) {
			continue
		}
		// First occurrence wins. This makes an accidentally duplicated
		// credential environment deterministic instead of last-writer wins.
		if _, exists := values[key]; !exists {
			values[key] = value
		}
	}
	values["PATH"] = codexACPDefaultFixedPath
	values["CODEX_CONFIG"] = string(config)
	values["INITIAL_AGENT_MODE"] = mode
	values[authKey] = authValue
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		out = append(out, key+"="+values[key])
	}
	return CodexACPEnvironmentResult{
		Variables: out,
		Auth: CodexACPAuthIdentity{
			Method: strings.TrimSpace(string(auth.Source)), PrincipalDigest: strings.TrimSpace(auth.PrincipalDigest),
		},
	}, nil
}

func exactCodexACPEnvironmentValue(inherited []string, selected string) (string, error) {
	value := ""
	seen := false
	for _, entry := range inherited {
		key, candidate, ok := strings.Cut(entry, "=")
		if !ok || key != selected {
			continue
		}
		if seen {
			return "", fmt.Errorf("codex ACP selected auth source %s is duplicated", selected)
		}
		seen = true
		value = candidate
	}
	if !seen || strings.TrimSpace(value) == "" || len(value) > 16<<10 || containsControl(value) {
		return "", fmt.Errorf("codex ACP selected auth source %s is unavailable or invalid", selected)
	}
	return value, nil
}

// CodexACPAdvertisedConfigOption is the minimal provider-facing projection of
// an ACP session/new configOptions entry. The generic ACP client does not yet
// expose this field or session/set_config_option, so this remains a deliberately
// explicit integration seam instead of pretending CODEX_CONFIG alone is a
// verified configuration.
type CodexACPAdvertisedConfigOption struct {
	ID            string
	Value         string
	AllowedValues []string
}

// CodexACPConfigStep is one required, value-exact configuration operation.
// The adapter must return the same ID/value after session/set_config_option.
// Accepting an acknowledgement without that verification would let an adapter
// silently fall back to a less-safe default mode or an unexpected model.
type CodexACPConfigStep struct {
	Semantic string
	OptionID string
	Value    string
}

// CodexACPConfigPlan binds the desired model, reasoning effort, and mode to
// option IDs advertised by the exact ACP session. Integration sequence:
//
//  1. initialize the pinned adapter;
//  2. create/load/resume the session and capture its configOptions;
//  3. build this plan and refuse if any required option is absent/ambiguous;
//  4. call session/set_config_option for every step before prompt; and
//  5. pass returned values to VerifyApplied and refuse on any mismatch.
//
// Environment defaults are only bootstrap hints. They never replace steps 3-5.
type CodexACPConfigPlan struct{ Steps []CodexACPConfigStep }

func (d CodexACPDescriptor) ConfigPlan(advertised []CodexACPAdvertisedConfigOption) (CodexACPConfigPlan, error) {
	if err := d.Validate(); err != nil {
		return CodexACPConfigPlan{}, err
	}
	mode, err := d.Mode.initialAgentMode()
	if err != nil {
		return CodexACPConfigPlan{}, err
	}
	requirements := []struct {
		semantic string
		optionID string
		value    string
	}{
		{semantic: "model", optionID: "model", value: strings.TrimSpace(d.Model)},
		{semantic: "reasoning_effort", optionID: "reasoning_effort", value: strings.TrimSpace(d.Effort)},
		{semantic: "mode", optionID: "mode", value: mode},
	}
	steps := make([]CodexACPConfigStep, 0, len(requirements))
	for _, requirement := range requirements {
		// A blank effort is intentional only when the adapter has no configured
		// effort knob. When a profile selected effort, absence is fail-closed.
		if requirement.semantic == "reasoning_effort" && requirement.value == "" {
			continue
		}
		option, err := uniqueCodexACPConfigOption(advertised, requirement.optionID)
		if err != nil {
			return CodexACPConfigPlan{}, fmt.Errorf("codex ACP %s config option: %w", requirement.semantic, err)
		}
		if !codexACPConfigOptionAllows(option, requirement.value) {
			return CodexACPConfigPlan{}, fmt.Errorf("codex ACP %s config option does not advertise desired value", requirement.semantic)
		}
		steps = append(steps, CodexACPConfigStep{Semantic: requirement.semantic, OptionID: option.ID, Value: requirement.value})
	}
	return CodexACPConfigPlan{Steps: steps}, nil
}

func uniqueCodexACPConfigOption(advertised []CodexACPAdvertisedConfigOption, expectedID string) (CodexACPAdvertisedConfigOption, error) {
	var matched CodexACPAdvertisedConfigOption
	for _, option := range advertised {
		id := strings.TrimSpace(option.ID)
		if id != expectedID {
			continue
		}
		if matched.ID != "" {
			return CodexACPAdvertisedConfigOption{}, errors.New("ambiguous advertised option")
		}
		option.ID = id
		matched = option
	}
	if matched.ID == "" {
		return CodexACPAdvertisedConfigOption{}, errors.New("required option was not advertised")
	}
	return matched, nil
}

func codexACPConfigOptionAllows(option CodexACPAdvertisedConfigOption, desired string) bool {
	if len(option.AllowedValues) == 0 {
		return false
	}
	for _, value := range option.AllowedValues {
		if value == desired {
			return true
		}
	}
	return false
}

// VerifyApplied validates the post-set values returned by the adapter. It is
// intentionally strict: a missing response, duplicate response, or coercion
// from full access to some provider-defined default is an admission failure.
func (p CodexACPConfigPlan) VerifyApplied(applied []CodexACPAdvertisedConfigOption) error {
	if len(p.Steps) == 0 {
		return errors.New("empty codex ACP configuration plan")
	}
	byID := make(map[string]string, len(applied))
	for _, option := range applied {
		id := strings.TrimSpace(option.ID)
		if id == "" {
			return errors.New("invalid applied Codex ACP config option")
		}
		if _, exists := byID[id]; exists {
			return errors.New("duplicate applied Codex ACP config option")
		}
		byID[id] = option.Value
	}
	for _, step := range p.Steps {
		if actual, exists := byID[step.OptionID]; !exists || actual != step.Value {
			return fmt.Errorf("codex ACP %s configuration was not applied exactly", step.Semantic)
		}
	}
	return nil
}

// CodexACPPermissionOperation is the version-pinned provider vocabulary
// accepted by the Codex ACP parity layer. Execute is authorized against its
// working directory; edit is authorized against a workspace/grant root; and
// explicit network requests remain subject to the resolved profile ceiling.
type CodexACPPermissionOperation string

const (
	CodexACPPermissionExecute CodexACPPermissionOperation = "execute"
	CodexACPPermissionEdit    CodexACPPermissionOperation = "edit"
	CodexACPPermissionRead    CodexACPPermissionOperation = "read"
	CodexACPPermissionNetwork CodexACPPermissionOperation = "network"
	CodexACPPermissionOther   CodexACPPermissionOperation = "other"
)

type CodexACPPermission struct {
	SessionID  string
	ToolCallID string
	Operation  CodexACPPermissionOperation
	Target     string
	Options    []ACPPermissionOption
}

// BrokerRequest produces the canonical, bounded broker request. It has no
// defaults: an unrecognized Codex request deliberately becomes an unknown tool
// kind and will fail closed in EvaluateACPPermission.
func (p CodexACPPermission) BrokerRequest(attemptID, boundSessionID, workspace string, cancelled bool) ACPPermissionRequest {
	toolKind := "other"
	switch p.Operation {
	case CodexACPPermissionEdit:
		toolKind = "write"
	case CodexACPPermissionExecute:
		toolKind = "execute"
	case CodexACPPermissionRead:
		toolKind = "read"
	case CodexACPPermissionNetwork:
		toolKind = "network"
	case CodexACPPermissionOther:
		toolKind = "other"
	}
	target := strings.TrimSpace(p.Target)
	if p.Operation == CodexACPPermissionEdit && target == "" {
		// codex-acp 1.1.14 does not expose the individual edit path in the
		// public toolCall. Its configured `agent` mode mechanically confines
		// writes to the session workspace, so authorize only that bound root.
		target = workspace
	}
	return ACPPermissionRequest{
		AttemptID: attemptID, BoundAttemptID: attemptID,
		SessionID: strings.TrimSpace(p.SessionID), BoundSessionID: strings.TrimSpace(boundSessionID),
		Workspace: workspace, Target: target, ToolKind: toolKind,
		Options: append([]ACPPermissionOption(nil), p.Options...), Cancelled: cancelled,
	}
}

// DecodeCodexACPPermission decodes the official CodexApprovalHandler shape:
// {sessionId, toolCall:{toolCallId,kind,rawInput:{command,cwd}},options,_meta}.
// It verifies raw session/tool-call IDs against the ACP client's already-bound
// values before recognizing a kind. Execute carries only a conservative cwd
// correlation target. Public edit callbacks do not expose an individual file;
// for this pinned adapter version their optional grantRoot is accepted only as
// a narrower hint, otherwise BrokerRequest binds the edit to the workspace.
// Raw commands/prompts never enter the canonical broker request or audit.
func DecodeCodexACPPermission(request acp.PermissionRequest) CodexACPPermission {
	decoded := struct {
		SessionID string `json:"sessionId"`
		ToolCall  struct {
			ToolCallID string `json:"toolCallId"`
			Kind       string `json:"kind"`
			RawInput   struct {
				Command string `json:"command"`
				CWD     string `json:"cwd"`
			} `json:"rawInput"`
		} `json:"toolCall"`
		Meta struct {
			Codex struct {
				Params struct {
					GrantRoot   string `json:"grantRoot"`
					Permissions struct {
						Network *struct {
							Enabled *bool `json:"enabled"`
						} `json:"network"`
						FileSystem *struct {
							Read  []string `json:"read"`
							Write []string `json:"write"`
						} `json:"fileSystem"`
					} `json:"permissions"`
				} `json:"params"`
			} `json:"codex"`
		} `json:"_meta"`
	}{}
	validShape := len(request.Raw) > 0 && len(request.Raw) <= 16<<10 && json.Unmarshal(request.Raw, &decoded) == nil
	if validShape && (strings.TrimSpace(decoded.SessionID) != strings.TrimSpace(request.SessionID) ||
		strings.TrimSpace(decoded.ToolCall.ToolCallID) != strings.TrimSpace(request.ToolCallID)) {
		validShape = false
	}
	providerOperation := CodexACPPermissionOther
	target := ""
	if validShape {
		switch strings.ToLower(strings.TrimSpace(decoded.ToolCall.Kind)) {
		case "execute":
			providerOperation = CodexACPPermissionExecute
			if cwd := strings.TrimSpace(decoded.ToolCall.RawInput.CWD); filepath.IsAbs(cwd) && !containsControl(cwd) {
				target = cwd
			}
		case "edit":
			providerOperation = CodexACPPermissionEdit
			if root := strings.TrimSpace(decoded.Meta.Codex.Params.GrantRoot); filepath.IsAbs(root) && !containsControl(root) {
				target = root
			}
		case "other":
			permissions := decoded.Meta.Codex.Params.Permissions
			filesystem := permissions.FileSystem
			networkOnly := permissions.Network != nil && permissions.Network.Enabled != nil && *permissions.Network.Enabled && filesystem == nil
			if networkOnly {
				providerOperation, target = CodexACPPermissionNetwork, "network"
			} else if permissions.Network == nil && filesystem != nil && len(filesystem.Write) == 1 && len(filesystem.Read) == 0 {
				providerOperation, target = CodexACPPermissionEdit, strings.TrimSpace(filesystem.Write[0])
			} else if permissions.Network == nil && filesystem != nil && len(filesystem.Read) == 1 && len(filesystem.Write) == 0 {
				providerOperation, target = CodexACPPermissionRead, strings.TrimSpace(filesystem.Read[0])
			}
		}
	}
	options := make([]ACPPermissionOption, 0, len(request.Options))
	for _, option := range request.Options {
		options = append(options, ACPPermissionOption{OptionID: option.ID, Kind: option.Kind})
	}
	return CodexACPPermission{
		SessionID: strings.TrimSpace(request.SessionID), ToolCallID: strings.TrimSpace(request.ToolCallID),
		Operation: providerOperation, Target: target, Options: options,
	}
}

// CodexACPAuthorityBinding is correlation context, not resume authorization.
// The shared integration must still load the durable originating attempt,
// verify its current lease/authorization state, and then compare this binding
// before invoking session/load or session/resume.
type CodexACPAuthorityBinding struct {
	ProjectID           string `json:"project_id"`
	WorkspacePath       string `json:"workspace_path"`
	RunnerProfile       string `json:"runner_profile"`
	AuthPrincipalDigest string `json:"auth_principal_digest"`
	OriginAttemptID     string `json:"origin_attempt_id"`
	WorkRevision        int    `json:"work_revision"`
}

func (b CodexACPAuthorityBinding) normalized() (CodexACPAuthorityBinding, error) {
	b.ProjectID = strings.TrimSpace(b.ProjectID)
	b.WorkspacePath = strings.TrimSpace(b.WorkspacePath)
	b.RunnerProfile = strings.TrimSpace(b.RunnerProfile)
	b.AuthPrincipalDigest = strings.TrimSpace(b.AuthPrincipalDigest)
	b.OriginAttemptID = strings.TrimSpace(b.OriginAttemptID)
	if b.ProjectID == "" || len(b.ProjectID) > 256 || containsControl(b.ProjectID) ||
		b.RunnerProfile == "" || len(b.RunnerProfile) > 256 || containsControl(b.RunnerProfile) ||
		b.OriginAttemptID == "" || len(b.OriginAttemptID) > 256 || containsControl(b.OriginAttemptID) ||
		b.WorkRevision <= 0 || !v7CloseAuthorityDigest(b.AuthPrincipalDigest, "sha256:") || !filepath.IsAbs(b.WorkspacePath) {
		return CodexACPAuthorityBinding{}, errors.New("invalid Codex ACP authority binding")
	}
	workspace, err := filepath.EvalSymlinks(b.WorkspacePath)
	info, statErr := os.Stat(workspace)
	if err != nil || statErr != nil || !info.IsDir() || !filepath.IsAbs(workspace) {
		return CodexACPAuthorityBinding{}, errors.New("Codex ACP authority binding workspace is unavailable")
	}
	b.WorkspacePath = filepath.Clean(workspace)
	return b, nil
}

// CodexACPSessionRef binds a provider session observation to the exact adapter
// and originating authority context. It is deliberately not a bearer token.
type CodexACPSessionRef struct {
	Provider           string                   `json:"provider"`
	Version            string                   `json:"version"`
	AdapterFingerprint string                   `json:"adapter_fingerprint"`
	ProviderSessionID  string                   `json:"provider_session_id"`
	AuthorityBinding   CodexACPAuthorityBinding `json:"authority_binding"`
}

func (d CodexACPDescriptor) EncodeSessionRef(providerSessionID string, binding CodexACPAuthorityBinding) (string, error) {
	if err := d.Validate(); err != nil {
		return "", err
	}
	binding, err := binding.normalized()
	if err != nil {
		return "", err
	}
	providerSessionID = strings.TrimSpace(providerSessionID)
	if providerSessionID == "" || len(providerSessionID) > codexACPMaxProviderSession || containsControl(providerSessionID) {
		return "", fmt.Errorf("invalid Codex ACP provider session id")
	}
	payload, err := json.Marshal(CodexACPSessionRef{
		Provider: codexACPProvider, Version: strings.TrimSpace(d.AdapterVersion),
		AdapterFingerprint: d.ManifestFingerprint, ProviderSessionID: providerSessionID, AuthorityBinding: binding,
	})
	if err != nil {
		return "", err
	}
	return codexACPSessionRefVersion + ":" + base64.RawURLEncoding.EncodeToString(payload), nil
}

func (d CodexACPDescriptor) DecodeSessionRef(value string, expected CodexACPAuthorityBinding) (CodexACPSessionRef, error) {
	if err := d.Validate(); err != nil {
		return CodexACPSessionRef{}, err
	}
	expected, err := expected.normalized()
	if err != nil {
		return CodexACPSessionRef{}, err
	}
	prefix := codexACPSessionRefVersion + ":"
	if !strings.HasPrefix(value, prefix) {
		return CodexACPSessionRef{}, errors.New("invalid Codex ACP session reference schema")
	}
	encoded := strings.TrimPrefix(value, prefix)
	if encoded == "" || len(encoded) > 16<<10 {
		return CodexACPSessionRef{}, errors.New("invalid Codex ACP session reference length")
	}
	payload, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil || len(payload) == 0 || len(payload) > 12<<10 {
		return CodexACPSessionRef{}, errors.New("invalid Codex ACP session reference encoding")
	}
	decoder := json.NewDecoder(strings.NewReader(string(payload)))
	decoder.DisallowUnknownFields()
	var ref CodexACPSessionRef
	if err := decoder.Decode(&ref); err != nil || decoder.More() {
		return CodexACPSessionRef{}, errors.New("invalid Codex ACP session reference payload")
	}
	actualBinding, bindingErr := ref.AuthorityBinding.normalized()
	if ref.Provider != codexACPProvider || ref.Version != strings.TrimSpace(d.AdapterVersion) || ref.AdapterFingerprint != d.ManifestFingerprint || bindingErr != nil || actualBinding != expected ||
		strings.TrimSpace(ref.ProviderSessionID) == "" || len(ref.ProviderSessionID) > codexACPMaxProviderSession || containsControl(ref.ProviderSessionID) {
		return CodexACPSessionRef{}, errors.New("Codex ACP session reference is not bound to this adapter and authority context")
	}
	ref.AuthorityBinding = actualBinding
	return ref, nil
}

// CodexACPStop is a provider-local terminal mapping. DeliveryUnknown is never
// retryable or resumable automatically: we cannot honestly know whether the
// provider received the turn, and issuing it again may duplicate edits.
type CodexACPStop struct {
	Outcome       AttemptOutcome
	ExitCode      int
	Reason        string
	AutoRetry     bool
	AutoResume    bool
	DeliveryKnown bool
}

func CodexACPMapStop(result acp.PromptResult, err error) CodexACPStop {
	reason := ""
	if err != nil {
		reason = "codex ACP transport error: " + boundedACPObservation(err.Error())
	}
	if errors.Is(err, acp.ErrDeliveryUnknown) || result.Outcome == acp.OutcomeDeliveryUnknown ||
		(result.Outcome == acp.OutcomeCompleted && result.Delivery != acp.DeliveryTerminalReceived) {
		if reason == "" {
			reason = "codex ACP delivery_unknown"
		}
		return CodexACPStop{Outcome: AttemptOutcomeFailed, ExitCode: 1, Reason: reason + "; no automatic retry or resume", AutoRetry: false, AutoResume: false, DeliveryKnown: false}
	}
	known := result.Delivery == acp.DeliveryTerminalReceived
	switch result.Outcome {
	case acp.OutcomeCompleted:
		if err != nil {
			return CodexACPStop{Outcome: AttemptOutcomeFailed, ExitCode: 1, Reason: reason, DeliveryKnown: known}
		}
		return CodexACPStop{Outcome: AttemptOutcomeNone, DeliveryKnown: true}
	case acp.OutcomeBudgetExceeded:
		return CodexACPStop{Outcome: AttemptOutcomeBudgetExceeded, ExitCode: exitCodeForOutcome(AttemptOutcomeBudgetExceeded), Reason: firstNonEmpty(reason, "codex ACP reported max_tokens"), DeliveryKnown: known}
	case acp.OutcomeTurnCapExhausted:
		return CodexACPStop{Outcome: AttemptOutcomeTurnCapExhausted, Reason: firstNonEmpty(reason, "codex ACP reported max_turn_requests"), DeliveryKnown: known}
	case acp.OutcomeCancelled:
		return CodexACPStop{Outcome: AttemptOutcomeCancelled, ExitCode: exitCodeForOutcome(AttemptOutcomeCancelled), Reason: firstNonEmpty(reason, "codex ACP prompt cancelled"), DeliveryKnown: known}
	case acp.OutcomeRefused:
		return CodexACPStop{Outcome: AttemptOutcomeBlocked, ExitCode: 1, Reason: firstNonEmpty(reason, "codex ACP refused the prompt or required permission"), DeliveryKnown: known}
	case acp.OutcomeTimedOut:
		return CodexACPStop{Outcome: AttemptOutcomeFailed, ExitCode: 1, Reason: firstNonEmpty(reason, "codex ACP prompt timed out"), DeliveryKnown: known}
	case acp.OutcomePoisoned, acp.OutcomeProtocolFailed:
		return CodexACPStop{Outcome: AttemptOutcomeFailed, ExitCode: 1, Reason: firstNonEmpty(reason, "codex ACP transport failed"), DeliveryKnown: known}
	default:
		return CodexACPStop{Outcome: AttemptOutcomeFailed, ExitCode: 1, Reason: firstNonEmpty(reason, "codex ACP terminated without a trustworthy result"), DeliveryKnown: known}
	}
}

// ObserveCodexACPUpdate bridges an ACP session/update into the existing Codex
// observation adapter. It first decodes the local session token and supplies
// the raw provider session ID only to the observation layer. That layer can
// attach/correlate observations, but it cannot create a task, alter a lease,
// accept evidence, or set an attempt outcome.
func (d CodexACPDescriptor) ObserveCodexACPUpdate(store *RuntimeStore, run RunStatus, binding CodexACPAuthorityBinding, update acp.Update, sequence int64) (bool, error) {
	if store == nil {
		return false, providerObservationError("codex ACP observation adapter store is nil")
	}
	if sequence <= 0 || strings.TrimSpace(update.Method) == "" || len(update.Method) > 128 || containsControl(update.Method) {
		return false, providerObservationError("invalid codex ACP update identity")
	}
	bound, err := binding.normalized()
	if err != nil || bound.ProjectID != strings.TrimSpace(run.ProjectID) || bound.RunnerProfile != strings.TrimSpace(run.RunnerProfile) {
		return false, providerObservationError("codex ACP update authority binding does not match the run")
	}
	runWorkspace, workspaceErr := filepath.EvalSymlinks(strings.TrimSpace(run.WorkspacePath))
	if workspaceErr != nil || filepath.Clean(runWorkspace) != bound.WorkspacePath {
		return false, providerObservationError("codex ACP update workspace binding does not match the run")
	}
	ref, err := d.DecodeSessionRef(run.SessionRef, bound)
	if err != nil {
		return false, providerObservationError("codex ACP update session is not bound to this adapter")
	}
	if len(update.Params) == 0 || len(update.Params) > 64<<10 {
		return false, providerObservationError("invalid codex ACP update payload")
	}
	var payload map[string]any
	decoder := json.NewDecoder(strings.NewReader(string(update.Params)))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil || payload == nil || decoder.More() {
		return false, providerObservationError("invalid codex ACP update JSON object")
	}
	// Treat session ID as a correlation key, never as an authority token.
	payload["session_id"] = ref.ProviderSessionID
	observedRun := run
	observedRun.Runner = string(codexACPRunnerName)
	observedRun.SessionRef = ref.ProviderSessionID
	return (CodexExecutionAdapter{Store: store}).ObserveRunPayload(observedRun, payload, sequence, "codex_acp:"+strings.ToLower(strings.TrimSpace(update.Method)))
}
