package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
	"tusker/internal/acp"
	runnercore "tusker/internal/runner"
)

// RunnerCatalog is deliberately a machine-local observation, never project policy.
type RunnerCatalog struct {
	Schema     string                 `json:"schema"`
	Version    int                    `json:"version"`
	ObservedAt string                 `json:"observed_at"`
	Harnesses  []RunnerCatalogHarness `json:"harnesses"`
}

type RunnerCatalogHarness struct {
	Harness            string                `json:"harness"`
	DisplayName        string                `json:"display_name"`
	Group              string                `json:"group"`
	Transports         []string              `json:"transports"`
	Setup              string                `json:"setup"`
	Source             string                `json:"source"`
	Confidence         string                `json:"confidence"`
	Version            string                `json:"version,omitempty"`
	Available          bool                  `json:"available"`
	ExecutableDetected bool                  `json:"executable_detected"`
	Authentication     string                `json:"authentication"`
	DiscoveryState     string                `json:"discovery_state"`
	DiscoverySource    string                `json:"discovery_source"`
	LastChecked        string                `json:"last_checked"`
	Conformance        string                `json:"conformance"`
	ManualEntry        bool                  `json:"manual_entry"`
	State              string                `json:"state"`
	Models             []RunnerCatalogModel  `json:"models,omitempty"`
	Options            []RunnerCatalogOption `json:"options,omitempty"`
	AccessControls     []ControlSupport      `json:"access_controls,omitempty"`
	Extension          map[string]any        `json:"extension,omitempty"`
	Error              string                `json:"error,omitempty"`
	ErrorKind          string                `json:"error_kind,omitempty"`
}

// RunnerCatalogOption is intentionally not a generic form schema. Adapters
// expose only bounded controls that Tusker already knows how to compile.
type RunnerCatalogOption struct {
	ID      string   `json:"id"`
	Label   string   `json:"label"`
	Kind    string   `json:"kind"` // boolean, enum, or scalar
	Values  []string `json:"values,omitempty"`
	Default any      `json:"default,omitempty"`
}

type RunnerCatalogModel struct {
	Model         string   `json:"model"`
	DisplayName   string   `json:"display_name,omitempty"`
	Description   string   `json:"description,omitempty"`
	Efforts       []string `json:"efforts"`
	ServiceTiers  []string `json:"service_tiers,omitempty"`
	Default       bool     `json:"default"`
	DefaultKnown  bool     `json:"default_known"`
	DefaultEffort string   `json:"default_effort,omitempty"`
	Visibility    string   `json:"visibility"`
	Hidden        bool     `json:"hidden"`
}

var runnerCatalogNow = func() time.Time { return time.Now().UTC() }
var runnerCatalogStateRoot = DefaultStateRoot
var runnerCatalogAppServerModels = codexAppServerModels
var runnerCatalogMuseServerModels = museServerModels
var runnerCatalogDevinModels = devinModels
var runnerCatalogCodexExecutable = resolveCodexExecutable
var runnerCatalogCommand = func(name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	// A wrapper may leave a child holding the output pipe after its parent is
	// cancelled. Bound that cleanup too; catalog discovery is observation only.
	cmd.WaitDelay = time.Second
	return cmd.Output()
}

func runnerCatalogCmd(args Args) error {
	catalog := discoverRunnerCatalogWithRefresh(args.Bool("bundled"), args.Bool("refresh"))
	if args.Bool("json") {
		emitJSON(catalog)
		return nil
	}
	for _, harness := range catalog.Harnesses {
		fmt.Printf("%s: %s (%s)\n", harness.Harness, harness.Source, harness.Confidence)
		if harness.Error != "" {
			fmt.Printf("  error: %s\n", harness.Error)
		}
		for _, model := range harness.Models {
			fmt.Printf("  %s [%s]\n", model.Model, strings.Join(model.Efforts, ", "))
		}
	}
	return nil
}

func discoverRunnerCatalog(bundled bool) RunnerCatalog {
	return discoverRunnerCatalogWithRefresh(bundled, false)
}

func discoverRunnerCatalogWithRefresh(bundled, refresh bool) RunnerCatalog {
	result := RunnerCatalog{Schema: "tusker.runner-catalog/v1", Version: 1, ObservedAt: runnerCatalogNow().Format(time.RFC3339), Harnesses: []RunnerCatalogHarness{}}
	result.Harnesses = append(result.Harnesses, discoverCodexCatalogCached(bundled, refresh))
	result.Harnesses = append(result.Harnesses, discoverClaudeCatalog())
	result.Harnesses = append(result.Harnesses, discoverMuseCatalogCached(refresh))
	result.Harnesses = append(result.Harnesses,
		futureCatalogHarness("opencode", "OpenCode"),
		futureCatalogHarness("cursor", "Cursor"),
		discoverDevinCatalogCached(refresh),
	)
	for i := range result.Harnesses {
		h := &result.Harnesses[i]
		if h.LastChecked == "" {
			h.LastChecked = result.ObservedAt
		}
		if h.Conformance == "" {
			h.Conformance = "not_checked"
		}
		switch {
		case h.State != "":
		case h.Available:
			h.State = "available"
		case h.ExecutableDetected:
			h.State = "unsupported"
		case h.Error != "":
			h.State = "error"
		default:
			h.State = "unsupported"
		}
	}
	return result
}

func discoverMuseCatalog() RunnerCatalogHarness {
	harness := catalogHarness(string(RunnerMuse), "Muse", "supported", "Install and authenticate Muse, then use Check setup or Run test.")
	harness.Source = "live"
	harness.DiscoverySource = "muse_server:model/list"
	harness.Version = museVersion()
	harness.ExecutableDetected = museExecutableDetected()
	harness.AccessControls = nativeAccessControls(runnercore.HarnessDefinition{Provider: "muse", Dialect: "muse"}, nil)
	if !harness.ExecutableDetected {
		harness.State = "unsupported"
		harness.DiscoveryState = "unsupported"
		harness.Error = "direct Muse executable was not found"
		harness.ErrorKind = "unsupported"
		return harness
	}
	models, err := runnerCatalogMuseServerModels(context.Background())
	if err != nil {
		harness.ErrorKind, harness.Error = museDiscoveryErrorKind(err), museDiscoveryErrorMessage(err)
		return harness
	}
	for i := range models {
		models[i].Efforts = append([]string(nil), museReasoningEfforts...)
		models[i].DefaultEffort = "high"
		models[i].Visibility = "visible"
	}
	if len(models) == 0 {
		harness.ErrorKind, harness.Error = "implementation", "Muse CLI model discovery returned no choices"
		return harness
	}
	harness.Available, harness.Confidence, harness.Authentication, harness.DiscoveryState, harness.Models = true, "high", "unknown", "available", models
	return harness
}

func discoverMuseCatalogCached(refresh bool) RunnerCatalogHarness {
	return discoverCatalogCached(string(RunnerMuse), museVersion(), "msp_stdio", "installed-account", refresh, discoverMuseCatalog)
}

type museModelDiscoveryError struct {
	Kind    string
	Message string
}

func (e *museModelDiscoveryError) Error() string { return e.Message }

var museDiscoveryTimeout = 10 * time.Second

var museReasoningEfforts = []string{"none", "minimal", "low", "medium", "high", "xhigh", "max", "ultra"}

type museServerModelList struct {
	Models []struct {
		ModelID      string `json:"modelId"`
		DisplayLabel string `json:"displayLabel"`
		Description  string `json:"description"`
	} `json:"models"`
}

// museServerModels uses Muse's own versioned MSP model/list interface. It is
// the same catalog that powers the native /model picker, rather than a broad
// provider inventory containing non-coding or retired models.
func museServerModels(parent context.Context) ([]RunnerCatalogModel, error) {
	cmd := exec.CommandContext(parent, "muse", "serve", "--no-session-log")
	cmd.WaitDelay = time.Second
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, &museModelDiscoveryError{Kind: "implementation", Message: "Muse discovery could not open stdin"}
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, &museModelDiscoveryError{Kind: "implementation", Message: "Muse discovery could not open stdout"}
	}
	var stderr limitedCatalogBuffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return nil, classifyMuseServerDiscoveryError(parent, err, stderr.String())
	}
	waited := make(chan error, 1)
	go func() { waited <- cmd.Wait() }()
	defer func() {
		_ = stdin.Close()
		select {
		case <-waited:
		case <-time.After(time.Second):
			_ = cmd.Process.Kill()
			<-waited
		}
	}()

	messages := make(chan codexAppServerMessage, 16)
	readErr := make(chan error, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 1024), 1<<20)
		for scanner.Scan() {
			var message codexAppServerMessage
			if err := json.Unmarshal(scanner.Bytes(), &message); err == nil && len(message.ID) > 0 {
				messages <- message
			}
		}
		readErr <- scanner.Err()
	}()
	write := func(id int, method string, params any) error {
		return json.NewEncoder(stdin).Encode(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	}
	await := func(id int, target any) error {
		for {
			select {
			case <-parent.Done():
				return classifyMuseServerDiscoveryError(parent, parent.Err(), stderr.String())
			case scanErr := <-readErr:
				if scanErr != nil {
					return classifyMuseServerDiscoveryError(parent, scanErr, stderr.String())
				}
				return classifyMuseServerDiscoveryError(parent, io.EOF, stderr.String())
			case message := <-messages:
				var responseID int
				_ = json.Unmarshal(message.ID, &responseID)
				if responseID != id {
					continue
				}
				if message.Error != nil {
					return classifyMuseServerDiscoveryError(parent, errors.New(message.Error.Message), stderr.String())
				}
				if err := json.Unmarshal(message.Result, target); err != nil {
					return &museModelDiscoveryError{Kind: "implementation", Message: "Muse server returned an invalid model/list response"}
				}
				return nil
			}
		}
	}
	if err := write(1, "initialize", map[string]any{"clientInfo": map[string]string{"name": "tusker", "version": "1"}}); err != nil {
		return nil, classifyMuseServerDiscoveryError(parent, err, stderr.String())
	}
	var initialized map[string]any
	if err := await(1, &initialized); err != nil {
		return nil, err
	}
	if err := json.NewEncoder(stdin).Encode(map[string]any{"jsonrpc": "2.0", "method": "initialized", "params": map[string]any{}}); err != nil {
		return nil, classifyMuseServerDiscoveryError(parent, err, stderr.String())
	}
	var result museServerModelList
	if err := write(2, "model/list", map[string]any{}); err != nil {
		return nil, classifyMuseServerDiscoveryError(parent, err, stderr.String())
	}
	if err := await(2, &result); err != nil {
		return nil, err
	}
	models := make([]RunnerCatalogModel, 0, len(result.Models))
	for _, row := range result.Models {
		if model := strings.TrimSpace(row.ModelID); model != "" {
			models = append(models, RunnerCatalogModel{Model: model, DisplayName: strings.TrimSpace(row.DisplayLabel), Description: strings.TrimSpace(row.Description)})
		}
	}
	return models, nil
}

func museDiscoveryErrorKind(err error) string {
	var discovery *museModelDiscoveryError
	if errors.As(err, &discovery) {
		return discovery.Kind
	}
	return "implementation"
}

func museDiscoveryErrorMessage(err error) string {
	var discovery *museModelDiscoveryError
	if errors.As(err, &discovery) {
		return discovery.Message
	}
	return "Muse profile discovery failed: " + safeOperatorErrorLeaf(err)
}

func classifyMuseServerDiscoveryError(ctx context.Context, err error, stderr string) error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
		return &museModelDiscoveryError{Kind: "timeout", Message: "Muse model discovery timed out after 10 seconds"}
	}
	message := strings.ToLower(strings.TrimSpace(firstNonEmpty(stderr, safeOperatorErrorLeaf(err))))
	safe := safeOperatorErrorText(firstNonEmpty(stderr, errString(err)), 220)
	switch {
	case errors.Is(err, exec.ErrNotFound), strings.Contains(message, "unknown command"), strings.Contains(message, "unrecognized subcommand"), strings.Contains(message, "method not found"), strings.Contains(message, "not supported"):
		return &museModelDiscoveryError{Kind: "unsupported", Message: "This installed Muse version does not support model discovery"}
	case strings.Contains(message, "auth"), strings.Contains(message, "login"), strings.Contains(message, "unauthorized"), strings.Contains(message, "forbidden"), strings.Contains(message, "credential"):
		return &museModelDiscoveryError{Kind: "authentication", Message: "Muse server authentication failed: " + safe}
	default:
		return &museModelDiscoveryError{Kind: "implementation", Message: "Muse server discovery failed: " + safe}
	}
}

func discoverClaudeCatalog() RunnerCatalogHarness {
	return futureCatalogHarness("claude-code", "Claude Code")
}

func discoverDevinCatalog() RunnerCatalogHarness {
	harness := catalogHarness(string(RunnerDevin), "Devin", "supported", "Install and authenticate Devin, then configure its installed ACP endpoint and run Check setup.")
	harness.Transports = []string{"acp_stdio"}
	harness.Options[0].Values = []string{"workspace-write-network"}
	harness.Options[0].Default = "workspace-write-network"
	harness.DiscoverySource = "devin models list --format json + ACP session/new"
	harness.Version = devinVersion()
	harness.ExecutableDetected = harness.Version != ""
	if !harness.ExecutableDetected {
		harness.State = "unsupported"
		harness.DiscoveryState = "unsupported"
		harness.Error = "Devin executable was not found"
		harness.ErrorKind = "unsupported"
		return harness
	}
	models, err := runnerCatalogDevinModels(context.Background())
	if err != nil {
		harness.Source = "live"
		harness.Error = "Devin model discovery failed: " + boundedACPObservation(err.Error())
		harness.ErrorKind = "implementation"
		return harness
	}
	if len(models) == 0 {
		harness.Source = "live"
		harness.Error = "Devin ACP returned no executable models"
		harness.ErrorKind = "unsupported"
		return harness
	}
	harness.Source, harness.Confidence, harness.Available, harness.Authentication, harness.DiscoveryState, harness.Models = "live", "high", true, "authenticated", "available", models
	harness.AccessControls = devinAccessControls(newAgentAccessDefaults())
	return harness
}

func discoverDevinCatalogCached(refresh bool) RunnerCatalogHarness {
	return discoverCatalogCached(string(RunnerDevin), devinVersion(), "native_acp_stdio", "installed-account", refresh, discoverDevinCatalog)
}

type devinModelCatalog struct {
	Families []struct {
		Variants []struct {
			ModelUID        string `json:"model_uid"`
			Label           string `json:"label"`
			MaxOutputTokens int    `json:"max_output_tokens"`
			CostSummary     string `json:"cost_summary"`
		} `json:"variants"`
	} `json:"families"`
}

func devinModels(parent context.Context) ([]RunnerCatalogModel, error) {
	executable, err := exec.LookPath("devin")
	if err != nil {
		return nil, err
	}
	executable, err = filepath.Abs(executable)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(parent, 15*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, executable, "models", "list", "--format", "json")
	command.WaitDelay = time.Second
	raw, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("models list: %w", err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	var stderr limitedCatalogBuffer
	client, err := acp.Start(ctx, acp.Config{Argv: []string{executable, "acp"}, CWD: cwd, Env: acpRunnerEnvironment(StartRequest{}, cwd, CodexPolicy{}), Stderr: &stderr})
	if err != nil {
		return nil, fmt.Errorf("ACP start: %w", err)
	}
	defer client.Close()
	if _, err := client.Initialize(ctx); err != nil {
		return nil, fmt.Errorf("ACP initialize: %w", err)
	}
	session, err := client.NewSession(ctx)
	if err != nil {
		return nil, fmt.Errorf("ACP session/new: %w", err)
	}
	var modelOption acp.ConfigOption
	for _, option := range session.ConfigOptions {
		if option.ID == "model" {
			modelOption = option
			break
		}
	}
	return parseDevinModels(raw, modelOption)
}

func parseDevinModels(raw []byte, option acp.ConfigOption) ([]RunnerCatalogModel, error) {
	var catalog devinModelCatalog
	if err := json.Unmarshal(raw, &catalog); err != nil {
		return nil, errors.New("models list returned invalid JSON")
	}
	metadata := map[string]RunnerCatalogModel{}
	for _, family := range catalog.Families {
		for _, variant := range family.Variants {
			id := strings.TrimSpace(variant.ModelUID)
			if id == "" {
				continue
			}
			description := strings.TrimSpace(variant.CostSummary)
			if variant.MaxOutputTokens > 0 {
				description = strings.TrimSpace(fmt.Sprintf("%s · max output %d tokens", description, variant.MaxOutputTokens))
			}
			metadata[id] = RunnerCatalogModel{Model: id, DisplayName: strings.TrimSpace(variant.Label), Description: description, Visibility: "visible"}
		}
	}
	models := make([]RunnerCatalogModel, 0, len(metadata)+len(option.Options))
	for _, model := range metadata {
		effort := devinModelEffort(model.Model + " " + model.DisplayName)
		model.Efforts, model.DefaultEffort = []string{effort}, effort
		model.Default, model.DefaultKnown = model.Model == option.CurrentValue, option.CurrentValue != ""
		models = append(models, model)
	}
	for _, value := range option.Options {
		if _, exists := metadata[value.Value]; exists {
			continue
		}
		model := metadata[value.Value]
		if model.Model == "" {
			model = RunnerCatalogModel{Model: value.Value, DisplayName: value.Name, Visibility: "visible"}
		}
		effort := devinModelEffort(model.Model)
		model.Efforts, model.DefaultEffort = []string{effort}, effort
		model.Default, model.DefaultKnown = value.Value == option.CurrentValue, true
		models = append(models, model)
	}
	sort.Slice(models, func(i, j int) bool { return models[i].Model < models[j].Model })
	return models, nil
}

func devinModelEffort(model string) string {
	model = strings.ToLower(strings.TrimSpace(model))
	for _, effort := range []string{"minimal", "none", "low", "medium", "high", "xhigh", "max", "ultra"} {
		if strings.HasSuffix(model, "-"+effort) || strings.HasSuffix(model, " "+effort) {
			return effort
		}
	}
	return "medium"
}

func catalogHarness(id, displayName, group, setup string) RunnerCatalogHarness {
	return RunnerCatalogHarness{Harness: id, DisplayName: displayName, Group: group, Transports: []string{"cli"}, Setup: setup, Source: "installed", Confidence: "none", Authentication: "unknown", ManualEntry: true, DiscoveryState: "unsupported", Options: []RunnerCatalogOption{{ID: "permission_preset", Label: "Access", Kind: "enum", Values: []string{"read-only", "workspace-write-offline", "workspace-write-network", "danger-full-access"}, Default: "workspace-write-offline"}}}
}

func futureCatalogHarness(id, displayName string) RunnerCatalogHarness {
	h := catalogHarness(id, displayName, "future", "No implemented adapter is available. Do not create a runnable profile.")
	h.ManualEntry, h.Options, h.State, h.Error = false, nil, "unsupported", "future integration; no adapter or conformance route"
	return h
}

func cachedRunnerReady(harness, preset string) (bool, string) {
	var cached struct {
		Ready      bool       `json:"ready"`
		ValidUntil *time.Time `json:"valid_until"`
		Version    string     `json:"version"`
	}
	raw, err := os.ReadFile(filepath.Join(DefaultStateRoot(), "runner-conformance", harness+"-"+preset+".json"))
	if err != nil || json.Unmarshal(raw, &cached) != nil || !cached.Ready || cached.ValidUntil == nil || !cached.ValidUntil.After(runnerCatalogNow()) {
		return false, ""
	}
	return true, cached.Version
}

func discoverCodexCatalog(bundled bool) RunnerCatalogHarness {
	if bundled {
		return bundledCodexCatalog()
	}
	models, err := runnerCatalogAppServerModels(context.Background())
	if err != nil {
		harness := catalogHarness(string(RunnerCodexExec), "Codex", "supported", "Install and authenticate Codex, then use Check setup or Run test for a selected profile.")
		harness.Source, harness.ExecutableDetected, harness.Version = "live", codexExecutableDetected(), codexVersion()
		harness.DiscoverySource, harness.ErrorKind, harness.Error = "app_server:model/list", codexDiscoveryErrorKind(err), codexDiscoveryErrorMessage(err)
		return harness
	}
	if len(models) == 0 {
		harness := catalogHarness(string(RunnerCodexExec), "Codex", "supported", "Install and authenticate Codex, then use Check setup or Run test for a selected profile.")
		harness.Source, harness.ExecutableDetected, harness.Version = "live", true, codexVersion()
		harness.DiscoverySource, harness.ErrorKind, harness.Error = "app_server:model/list", "implementation", "Codex App Server returned no selectable models"
		return harness
	}
	harness := catalogHarness(string(RunnerCodexExec), "Codex", "supported", "Install and authenticate Codex, then use Check setup or Run test for a selected profile.")
	harness.Source, harness.DiscoverySource, harness.Confidence, harness.Available, harness.ExecutableDetected, harness.DiscoveryState, harness.Version, harness.Models = "live", "app_server:model/list", "high", true, true, "available", codexVersion(), models
	if executable := runnerCatalogCodexExecutable(); executable != "" {
		if _, err := runnerCatalogCommand(executable, "login", "status"); err == nil {
			harness.Authentication = "authenticated"
		}
	}
	return harness
}

func discoverCodexCatalogCached(bundled, refresh bool) RunnerCatalogHarness {
	version := codexVersion()
	context := "installed"
	if bundled {
		context = "bundled"
	}
	return discoverCatalogCached(string(RunnerCodexExec), version, "app_server_stdio", context, refresh, func() RunnerCatalogHarness { return discoverCodexCatalog(bundled) })
}

type runnerCatalogCacheEntry struct {
	CachedAt time.Time            `json:"cached_at"`
	Harness  RunnerCatalogHarness `json:"harness"`
}

const runnerCatalogCacheTTL = 24 * time.Hour

func discoverCatalogCached(id, version, transport, configContext string, refresh bool, discover func() RunnerCatalogHarness) RunnerCatalogHarness {
	key := fmt.Sprintf("%x", sha256.Sum256([]byte("v2\x00"+id+"\x00"+version+"\x00"+transport+"\x00"+configContext)))
	path := filepath.Join(runnerCatalogStateRoot(), "runner-catalog", key+".json")
	var cached runnerCatalogCacheEntry
	raw, readErr := os.ReadFile(path)
	cacheOK := readErr == nil && json.Unmarshal(raw, &cached) == nil && cached.Harness.Harness == id
	if cacheOK && !refresh && runnerCatalogNow().Sub(cached.CachedAt) < runnerCatalogCacheTTL {
		return cached.Harness
	}
	fresh := discover()
	if fresh.DiscoveryState == "available" {
		fresh.LastChecked = runnerCatalogNow().Format(time.RFC3339)
		entry := runnerCatalogCacheEntry{CachedAt: runnerCatalogNow(), Harness: fresh}
		if raw, err := json.MarshalIndent(entry, "", "  "); err == nil && ensureRuntimeStateRoot(runnerCatalogStateRoot()) == nil && ensureDir(filepath.Dir(path)) == nil {
			_ = os.WriteFile(path, append(raw, '\n'), 0o600)
		}
		return fresh
	}
	if cacheOK {
		cached.Harness.DiscoveryState, cached.Harness.State = "stale", "stale"
		cached.Harness.Error = "refresh failed; retained the last successful discovery"
		cached.Harness.LastChecked = runnerCatalogNow().Format(time.RFC3339)
		return cached.Harness
	}
	return fresh
}

func codexVersion() string {
	executable := runnerCatalogCodexExecutable()
	if executable == "" {
		return ""
	}
	out, err := runnerCatalogCommand(executable, "--version")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func museVersion() string {
	out, err := runnerCatalogCommand("muse", "--version")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func devinVersion() string {
	out, err := runnerCatalogCommand("devin", "--version")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func codexExecutableDetected() bool {
	return runnerCatalogCodexExecutable() != ""
}

// resolveCodexExecutable rejects stale package-manager shims before discovery.
// GUI processes inherit a different PATH from the shell, so LookPath alone can
// find an executable wrapper whose bundled native binary has disappeared.
func resolveCodexExecutable() string {
	candidates := healthyCodexExecutables(context.Background())
	if len(candidates) == 0 {
		return ""
	}
	return candidates[0]
}

func codexExecutablePaths() []string {
	candidates := []string{}
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if dir = strings.TrimSpace(dir); dir != "" {
			candidates = append(candidates, filepath.Join(dir, "codex"))
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates,
			filepath.Join(home, ".bun", "bin", "codex"),
			filepath.Join(home, ".local", "bin", "codex"),
		)
	}
	candidates = append(candidates,
		"/opt/homebrew/bin/codex",
		"/usr/local/bin/codex",
		"/Applications/ChatGPT.app/Contents/Resources/codex",
	)
	return candidates
}

func healthyCodexExecutables(parent context.Context) []string {
	seen := map[string]bool{}
	healthy := []string{}
	for _, candidate := range codexExecutablePaths() {
		candidate = filepath.Clean(candidate)
		if seen[candidate] {
			continue
		}
		seen[candidate] = true
		info, err := os.Stat(candidate)
		if err != nil || !info.Mode().IsRegular() || info.Mode()&0o111 == 0 {
			continue
		}
		ctx, cancel := context.WithTimeout(parent, 3*time.Second)
		cmd := exec.CommandContext(ctx, candidate, "--version")
		cmd.WaitDelay = time.Second
		out, err := cmd.Output()
		cancel()
		if err == nil && strings.Contains(strings.ToLower(string(out)), "codex") {
			healthy = append(healthy, candidate)
		}
	}
	return healthy
}

func museExecutableDetected() bool {
	_, err := exec.LookPath("muse")
	return err == nil
}

func bundledCodexCatalog() RunnerCatalogHarness {
	harness := catalogHarness(string(RunnerCodexExec), "Codex", "supported", "Install and authenticate Codex, then use Check setup or Run test for a selected profile.")
	harness.Source, harness.DiscoverySource, harness.Error = "bundled", "bundled", "bundled model discovery failed; no model IDs were invented"
	return harness
}

func parseCodexModels(raw []byte) []RunnerCatalogModel {
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return nil
	}
	var rows []any
	switch v := value.(type) {
	case []any:
		rows = v
	case map[string]any:
		if m, ok := v["models"].([]any); ok {
			rows = m
		}
	}
	models := make([]RunnerCatalogModel, 0, len(rows))
	for _, row := range rows {
		m, ok := row.(map[string]any)
		if !ok {
			continue
		}
		name := firstNonEmpty(stringAny(m["id"]), stringAny(m["model"]), stringAny(m["slug"]))
		if name == "" {
			continue
		}
		efforts := stringSliceAny(m["efforts"])
		if len(efforts) == 0 {
			efforts = stringSliceAny(m["reasoning_efforts"])
		}
		if len(efforts) == 0 {
			efforts = stringSliceAny(m["supported_reasoning_levels"])
		}
		if len(efforts) == 0 {
			efforts = stringSliceAny(m["supportedReasoningEfforts"])
		}
		if len(efforts) == 0 {
			continue
		}
		defaultEffort := firstNonEmpty(stringAny(m["default_reasoning_level"]), stringAny(m["defaultReasoningEffort"]))
		if defaultEffort != "" {
			efforts = uniqueCatalogStrings(append([]string{defaultEffort}, efforts...))
		}
		visibility := strings.ToLower(firstNonEmpty(stringAny(m["visibility"]), "visible"))
		defaultValue, defaultKnown := boolFieldAny(m, "is_default", "isDefault", "default")
		serviceTiers := stringSliceAny(m["service_tiers"])
		if len(serviceTiers) == 0 {
			serviceTiers = stringSliceAny(m["serviceTiers"])
		}
		models = append(models, RunnerCatalogModel{Model: name, DisplayName: firstNonEmpty(stringAny(m["display_name"]), stringAny(m["displayName"])), Description: stringAny(m["description"]), Efforts: efforts, ServiceTiers: serviceTiers, Default: defaultValue, DefaultKnown: defaultKnown, DefaultEffort: defaultEffort, Visibility: visibility, Hidden: visibility == "hide" || visibility == "hidden" || boolAny(m["hidden"])})
	}
	sort.Slice(models, func(i, j int) bool { return models[i].Model < models[j].Model })
	return models
}

var codexAppServerDiscoveryTimeout = 10 * time.Second

type codexAppServerMessage struct {
	ID     json.RawMessage `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

type codexAppServerModelPage struct {
	Data       []map[string]any `json:"data"`
	NextCursor *string          `json:"nextCursor"`
}

type codexModelDiscoveryError struct {
	Kind    string
	Message string
}

func (e *codexModelDiscoveryError) Error() string { return e.Message }

func codexDiscoveryErrorKind(err error) string {
	var discovery *codexModelDiscoveryError
	if errors.As(err, &discovery) {
		return discovery.Kind
	}
	return "implementation"
}

func codexDiscoveryErrorMessage(err error) string {
	var discovery *codexModelDiscoveryError
	if errors.As(err, &discovery) {
		return discovery.Message
	}
	return "Codex App Server discovery failed: " + safeOperatorErrorLeaf(err)
}

// codexAppServerModels reads the installed Codex App Server in the same default
// Codex configuration and credential context that codex_exec uses. Discovery
// must not select a model itself: model/list is the account-scoped source of truth.
func codexAppServerModels(parent context.Context) ([]RunnerCatalogModel, error) {
	ctx, cancel := context.WithTimeout(parent, codexAppServerDiscoveryTimeout)
	defer cancel()
	candidates := healthyCodexExecutables(ctx)
	if len(candidates) == 0 {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, classifyCodexDiscoveryError(ctx, ctx.Err(), "")
		}
		return nil, &codexModelDiscoveryError{Kind: "unsupported", Message: "No working Codex executable was found in the installed locations"}
	}
	failures := []string{}
	for _, executable := range candidates {
		models, err := codexAppServerModelsAt(ctx, executable)
		if err == nil && len(models) > 0 {
			return models, nil
		}
		if err == nil {
			failures = append(failures, filepath.Base(executable)+": App Server returned no selectable models")
			continue
		}
		if codexDiscoveryErrorKind(err) == "authentication" || codexDiscoveryErrorKind(err) == "timeout" {
			return nil, err
		}
		failures = append(failures, filepath.Base(executable)+": "+safeOperatorErrorLeaf(err))
	}
	return nil, &codexModelDiscoveryError{Kind: "unsupported", Message: "No installed Codex executable supports App Server model discovery: " + strings.Join(failures, "; ")}
}

func codexAppServerModelsAt(ctx context.Context, executable string) ([]RunnerCatalogModel, error) {
	cmd := exec.CommandContext(ctx, executable, "app-server", "--stdio")
	cmd.WaitDelay = time.Second
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, &codexModelDiscoveryError{Kind: "implementation", Message: "Codex App Server discovery could not open stdin"}
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, &codexModelDiscoveryError{Kind: "implementation", Message: "Codex App Server discovery could not open stdout"}
	}
	var stderr limitedCatalogBuffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return nil, classifyCodexDiscoveryError(ctx, err, stderr.String())
	}
	waited := make(chan error, 1)
	go func() { waited <- cmd.Wait() }()
	defer func() {
		_ = stdin.Close()
		select {
		case <-waited:
		case <-time.After(time.Second):
			_ = cmd.Process.Kill()
			<-waited
		}
	}()

	messages := make(chan codexAppServerMessage, 16)
	readErr := make(chan error, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 1024), 1<<20)
		for scanner.Scan() {
			var message codexAppServerMessage
			if err := json.Unmarshal(scanner.Bytes(), &message); err == nil && len(message.ID) > 0 {
				messages <- message
			}
		}
		readErr <- scanner.Err()
	}()

	write := func(id int, method string, params any) error {
		return json.NewEncoder(stdin).Encode(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	}
	await := func(id int, target any) error {
		for {
			select {
			case <-ctx.Done():
				return classifyCodexDiscoveryError(ctx, ctx.Err(), stderr.String())
			case scanErr := <-readErr:
				if scanErr != nil {
					return classifyCodexDiscoveryError(ctx, scanErr, stderr.String())
				}
				return classifyCodexDiscoveryError(ctx, io.EOF, stderr.String())
			case message := <-messages:
				var responseID int
				_ = json.Unmarshal(message.ID, &responseID)
				if responseID != id {
					continue
				}
				if message.Error != nil {
					return classifyCodexDiscoveryError(ctx, errors.New(message.Error.Message), stderr.String())
				}
				if err := json.Unmarshal(message.Result, target); err != nil {
					return &codexModelDiscoveryError{Kind: "implementation", Message: "Codex App Server returned an invalid model/list response"}
				}
				return nil
			}
		}
	}
	if err := write(1, "initialize", map[string]any{"clientInfo": map[string]string{"name": "tusker", "version": "1"}, "capabilities": map[string]any{}}); err != nil {
		return nil, classifyCodexDiscoveryError(ctx, err, stderr.String())
	}
	var initialized map[string]any
	if err := await(1, &initialized); err != nil {
		return nil, err
	}
	if err := json.NewEncoder(stdin).Encode(map[string]any{"jsonrpc": "2.0", "method": "initialized", "params": map[string]any{}}); err != nil {
		return nil, classifyCodexDiscoveryError(ctx, err, stderr.String())
	}
	all := []map[string]any{}
	cursor := ""
	for requestID := 2; ; requestID++ {
		params := map[string]any{"limit": 100}
		if cursor != "" {
			params["cursor"] = cursor
		}
		if err := write(requestID, "model/list", params); err != nil {
			return nil, classifyCodexDiscoveryError(ctx, err, stderr.String())
		}
		var page codexAppServerModelPage
		if err := await(requestID, &page); err != nil {
			return nil, err
		}
		all = append(all, page.Data...)
		if page.NextCursor == nil || strings.TrimSpace(*page.NextCursor) == "" {
			break
		}
		if *page.NextCursor == cursor {
			return nil, &codexModelDiscoveryError{Kind: "implementation", Message: "Codex App Server repeated a model-list pagination cursor"}
		}
		cursor = *page.NextCursor
	}
	raw, err := json.Marshal(map[string]any{"models": all})
	if err != nil {
		return nil, &codexModelDiscoveryError{Kind: "implementation", Message: "Codex App Server models could not be decoded"}
	}
	return parseCodexModels(raw), nil
}

type limitedCatalogBuffer struct{ bytes.Buffer }

func (b *limitedCatalogBuffer) Write(p []byte) (int, error) {
	const limit = 4096
	if b.Len() < limit {
		_, _ = b.Buffer.Write(p[:min(len(p), limit-b.Len())])
	}
	return len(p), nil
}

func classifyCodexDiscoveryError(ctx context.Context, err error, stderr string) error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
		return &codexModelDiscoveryError{Kind: "timeout", Message: "Codex App Server model discovery timed out after 10 seconds"}
	}
	message := strings.ToLower(strings.TrimSpace(firstNonEmpty(stderr, safeOperatorErrorLeaf(err))))
	safe := safeOperatorErrorText(firstNonEmpty(stderr, errString(err)), 220)
	switch {
	case errors.Is(err, exec.ErrNotFound), strings.Contains(message, "unknown subcommand"), strings.Contains(message, "unrecognized subcommand"), strings.Contains(message, "method not found"), strings.Contains(message, "model/list") && strings.Contains(message, "unsupported"):
		return &codexModelDiscoveryError{Kind: "unsupported", Message: "This installed Codex version does not support App Server model discovery"}
	case strings.Contains(message, "auth"), strings.Contains(message, "login"), strings.Contains(message, "unauthorized"), strings.Contains(message, "forbidden"), strings.Contains(message, "credential"):
		return &codexModelDiscoveryError{Kind: "authentication", Message: "Codex App Server authentication failed: " + safe}
	default:
		return &codexModelDiscoveryError{Kind: "implementation", Message: "Codex App Server discovery failed: " + safe}
	}
}

func errString(err error) string {
	if err == nil {
		return "no diagnostic was returned"
	}
	return err.Error()
}

func stringAny(value any) string { out, _ := value.(string); return strings.TrimSpace(out) }
func boolAny(value any) bool     { out, _ := value.(bool); return out }
func boolFieldAny(values map[string]any, keys ...string) (bool, bool) {
	for _, key := range keys {
		if value, ok := values[key]; ok {
			out, ok := value.(bool)
			return out, ok
		}
	}
	return false, false
}
func stringSliceAny(value any) []string {
	raw, _ := value.([]any)
	out := []string{}
	for _, v := range raw {
		if s := firstNonEmpty(stringAny(v), stringAny(catalogMapStringAny(v)["effort"]), stringAny(catalogMapStringAny(v)["reasoningEffort"]), stringAny(catalogMapStringAny(v)["id"])); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func catalogMapStringAny(value any) map[string]any { out, _ := value.(map[string]any); return out }
func uniqueCatalogStrings(values []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, value := range values {
		if value != "" && !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	return out
}

func runnerProfilesBootstrapCmd(args Args) error {
	vault, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	resolved, err := resolveTuskerConfig(vault)
	if err != nil {
		return err
	}
	// Bootstrap is a fresh managed write. Seed it with the effective managed
	// policy so profile generation preserves existing project settings.
	path := managedTuskerConfigPath(vault)
	raw := cloneConfigRaw(resolved.Raw)
	profiles := semanticBootstrapProfiles(discoverRunnerCatalog(args.Bool("bundled")))
	automation := mapAny(raw["automation"])
	if automation == nil {
		automation = map[string]any{}
		raw["automation"] = automation
	}
	existing := mapAny(automation["profiles"])
	profilesExplicitlyEmpty := false
	if rawProfiles, present := automation["profiles"]; present {
		profilesExplicitlyEmpty = mapAny(rawProfiles) != nil && len(mapAny(rawProfiles)) == 0
	}
	if existing == nil {
		existing = map[string]any{}
		automation["profiles"] = existing
	}
	added := []string{}
	for name, profile := range profiles {
		if _, found := existing[name]; !found {
			existing[name] = profile
			added = append(added, name)
		}
	}
	sort.Strings(added)
	if _, present := automation["enabled"]; !present {
		automation["enabled"] = false
	}
	// Do not create a dangling policy reference on a machine with no usable
	// harness.  Existing policy remains entirely user-owned.
	if _, present := automation["default_profile"]; !present && hasBootstrapProfile(profiles, "execute-standard") {
		automation["default_profile"] = "execute-standard"
	}
	// An explicit empty profile map is a deliberate project override that
	// clears the built-in profiles.  Do not leave the inherited built-in
	// default_profile pointing at a profile that no longer exists when the
	// machine catalog cannot provide replacements.
	if profilesExplicitlyEmpty && len(profiles) == 0 {
		delete(automation, "default_profile")
	}
	automationEnabled := boolAny(automation["enabled"])
	report := map[string]any{
		"write":               args.Bool("write"),
		"path":                path,
		"added_profiles":      added,
		"preserved_profiles":  sortedBootstrapMapKeys(existing, added),
		"semantic_profiles":   profiles,
		"default_profile":     stringAny(automation["default_profile"]),
		"automation_enabled":  automationEnabled,
		"selection_policy":    "live or explicitly bundled Codex prefers Luna for fast work, Terra for standard/complex/review/repair, and Sol for planning/frontier; an available Claude-only machine prefers Sonnet, Opus, and Fable by the corresponding role; every role uses a visible capable model and nearest supported effort",
		"configuration_scope": "project policy; the observed harness catalog remains machine-local",
	}
	if args.Bool("write") {
		out, err := yaml.Marshal(raw)
		if err != nil {
			return err
		}
		if err := writeConfigTextAtomically(path, string(out)); err != nil {
			return err
		}
	}
	if args.Bool("json") {
		emitJSON(report)
	} else {
		fmt.Printf("profiles %s: %s\n", map[bool]string{true: "written", false: "previewed"}[args.Bool("write")], strings.Join(added, ", "))
	}
	return nil
}

func mapAny(value any) map[string]any { out, _ := value.(map[string]any); return out }
func sortedBootstrapMapKeys(values map[string]any, exclude []string) []string {
	skip := map[string]bool{}
	for _, k := range exclude {
		skip[k] = true
	}
	out := []string{}
	for k := range values {
		if !skip[k] {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

func semanticBootstrapProfiles(catalog RunnerCatalog) map[string]any {
	efforts := map[string]string{"planner": "high", "execute-fast": "low", "execute-standard": "medium", "execute-complex": "high", "execute-frontier": "xhigh", "review-independent": "high", "repair-complex": "high"}
	harness, models, ok := bootstrapCatalogHarness(catalog)
	if !ok {
		return map[string]any{}
	}
	out := map[string]any{}
	for name, effort := range efforts {
		model, ok := semanticModelFor(harness, name, models)
		if !ok {
			continue
		}
		resolvedEffort := semanticEffortFor(harness, effort, model.Efforts)
		if resolvedEffort == "" {
			continue
		}
		mode := "workspace-write"
		preset := "workspace-write-offline"
		network := false
		if name == "review-independent" {
			mode = "read-only"
			preset = "read-only"
		}
		out[name] = map[string]any{"harness": harness, "model": model.Model, "effort": resolvedEffort, "permission_preset": preset, "sandbox": map[string]any{"mode": mode, "network": network}, "subagents": map[string]any{"allowed": false, "max_concurrent": 0}}
	}
	return out
}

func hasBootstrapProfile(profiles map[string]any, name string) bool {
	_, ok := profiles[name]
	return ok
}

// bootstrapCatalogHarness trusts Codex inventory only when the installed CLI
// returned it, whether from the live endpoint or an explicit --bundled query.
// This command is an explicit compatibility/profile-generation action; it is
// not the runtime default or an automatic fallback from an ACP attempt.
// Claude's aliases remain a truthful fallback when locally available.
func bootstrapCatalogHarness(catalog RunnerCatalog) (string, []RunnerCatalogModel, bool) {
	for _, entry := range catalog.Harnesses {
		if entry.Harness == string(RunnerCodexExec) && (entry.Source == "live" || entry.Source == "bundled") && entry.Available {
			if models := usableCatalogModels(entry.Harness, entry.Models); len(models) > 0 {
				return entry.Harness, models, true
			}
		}
	}
	for _, entry := range catalog.Harnesses {
		if entry.Harness == string(RunnerClaude) && entry.Source == "declared" && entry.Available {
			if models := usableCatalogModels(entry.Harness, entry.Models); len(models) > 0 {
				return entry.Harness, models, true
			}
		}
	}
	return "", nil, false
}

func usableCatalogModels(harness string, models []RunnerCatalogModel) []RunnerCatalogModel {
	out := make([]RunnerCatalogModel, 0, len(models))
	for _, model := range models {
		if model.Hidden || !validRunnerModelName(model.Model) {
			continue
		}
		for _, effort := range model.Efforts {
			if validCatalogEffort(harness, effort) {
				out = append(out, model)
				break
			}
		}
	}
	return out
}

func validCatalogEffort(harness, effort string) bool {
	return validRunnerEffort(effort) && !(harness == string(RunnerClaude) && strings.EqualFold(strings.TrimSpace(effort), "ultra"))
}

func semanticModelFor(harness, role string, models []RunnerCatalogModel) (RunnerCatalogModel, bool) {
	preferences := []string{""}
	if harness == string(RunnerClaude) {
		switch role {
		case "execute-fast":
			preferences = []string{"sonnet", ""}
		case "planner", "execute-frontier":
			preferences = []string{"fable", "opus", ""}
		case "execute-standard":
			preferences = []string{"sonnet", "opus", ""}
		default: // complex execution, independent review, and repair
			preferences = []string{"opus", "fable", "sonnet", ""}
		}
	} else {
		switch role {
		case "execute-fast":
			preferences = []string{"luna", "mini", "spark", ""}
		case "planner", "execute-frontier":
			preferences = []string{"sol", ""}
		default:
			preferences = []string{"terra", ""}
		}
	}
	for _, preference := range preferences {
		for _, model := range models {
			if preference == "" || strings.Contains(strings.ToLower(model.Model), preference) {
				return model, true
			}
		}
	}
	return RunnerCatalogModel{}, false
}

func semanticEffortFor(harness, want string, supported []string) string {
	levels := []string{"low", "medium", "high", "xhigh", "max", "ultra"}
	wantIndex := -1
	for i, level := range levels {
		if strings.EqualFold(level, strings.TrimSpace(want)) {
			wantIndex = i
			break
		}
	}
	if wantIndex < 0 {
		return ""
	}
	best, bestDistance := "", len(levels)+1
	for i, level := range levels {
		if !catalogContainsString(supported, level) || !validCatalogEffort(harness, level) {
			continue
		}
		distance := i - wantIndex
		if distance < 0 {
			distance = -distance
		}
		// Equal-distance ties select the lower effort (smaller i), avoiding an
		// accidental spend escalation when the exact level is unavailable.
		if distance < bestDistance || (distance == bestDistance && (best == "" || i < effortLevelIndex(best, levels))) {
			best, bestDistance = level, distance
		}
	}
	return best
}

func effortLevelIndex(effort string, levels []string) int {
	for i, level := range levels {
		if level == effort {
			return i
		}
	}
	return len(levels)
}
func catalogContainsString(values []string, want string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), strings.TrimSpace(want)) {
			return true
		}
	}
	return false
}

func printRunnerHelp() {
	fmt.Println(`Usage:
  tusker runner catalog [--bundled] [--refresh] [--json]
  tusker runner profiles [--bundled] [--write] [--json]
  tusker runner route <TASK-ID> --lane execute|review --json
  tusker runner test <id-or-profile> [--preset <preset>] [--live] [--exercise print|timer] [--script <executable>] [--json|--quiet]
  tusker runner conformance --harness <id-or-profile> [same flags]

Catalog observes installed harnesses without authentication or model launch. --bundled
selects an explicit bundled/offline Codex catalog source; --refresh bypasses a fresh
version/transport/context-keyed cache. Neither is a runtime fallback.
Profiles previews an additive semantic profile bootstrap; --write updates the project
config without enabling automation. Codex profiles use codex_exec by default. ACP
profiles name the exact operator-installed endpoint; Tusker does not install one.
Conformance never launches a model unless --live is explicitly supplied.`)
}
