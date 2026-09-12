package runner

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"tusker/internal/acp"
)

const (
	defaultProbeDeadline = 15 * time.Second
	defaultRunDeadline   = 30 * time.Minute
	defaultOutputLimit   = 10 << 20
	maxProtocolFrame     = 1 << 20
)

func Prepare(ctx context.Context, definition HarnessDefinition, input RunInput) (PreparedLaunch, error) {
	if err := validateDefinition(definition, input); err != nil {
		return PreparedLaunch{}, err
	}
	workspace, err := filepath.Abs(input.Workspace)
	if err != nil {
		return PreparedLaunch{}, admission(definition, "invalid_workspace", "workspace", err.Error())
	}
	if info, statErr := os.Stat(workspace); statErr != nil || !info.IsDir() {
		return PreparedLaunch{}, admission(definition, "invalid_workspace", "workspace", "workspace must be an existing directory")
	}
	input.Workspace = workspace
	searchPath := input.SearchPath
	if strings.TrimSpace(searchPath) == "" {
		searchPath = os.Getenv("PATH")
	}
	candidates := executableCandidates(definition.Executable, searchPath)
	checks := make([]CandidateCheck, 0, len(candidates))
	var physical, version, identity string
	for _, candidate := range candidates {
		info, statErr := os.Stat(candidate)
		if statErr != nil || info.IsDir() || info.Mode()&0o111 == 0 {
			checks = append(checks, CandidateCheck{Path: candidate, Check: "executable", Reason: "not an executable file"})
			continue
		}
		resolved, resolveErr := filepath.EvalSymlinks(candidate)
		if resolveErr != nil {
			resolved = candidate
		}
		resolved, _ = filepath.Abs(resolved)
		probeCtx, cancel := context.WithTimeout(ctx, defaultProbeDeadline)
		out, probeErr := exec.CommandContext(probeCtx, resolved, "--version").CombinedOutput()
		probeTimedOut := probeCtx.Err() == context.DeadlineExceeded
		cancel()
		if probeTimedOut || probeErr != nil {
			reason := strings.TrimSpace(string(out))
			if probeTimedOut {
				reason = "version probe timed out"
			} else if reason == "" {
				reason = probeErr.Error()
			}
			checks = append(checks, CandidateCheck{Path: resolved, Check: "version", Reason: bounded(reason, 300)})
			continue
		}
		identity, err = executableIdentity(resolved, strings.TrimSpace(string(out)))
		if err != nil {
			checks = append(checks, CandidateCheck{Path: resolved, Check: "identity", Reason: err.Error()})
			continue
		}
		physical, version = resolved, strings.TrimSpace(string(out))
		checks = append(checks, CandidateCheck{Path: resolved, Check: "healthy"})
		break
	}
	if physical == "" {
		if len(checks) == 0 {
			checks = append(checks, CandidateCheck{Path: definition.Executable, Check: "executable", Reason: "not found in PATH"})
		}
		return PreparedLaunch{}, &AdmissionError{Code: "runtime_missing", HarnessID: definition.ID, Check: checks[0].Check, Reason: checks[0].Reason, Remedy: "Install or repair the selected system harness, then rerun conformance.", Candidates: checks}
	}
	policy, argv, err := compilePolicy(definition, input)
	if err != nil {
		return PreparedLaunch{}, err
	}
	argv = append([]string{physical}, argv...)
	env, envNames, err := prepareEnvironment(definition.Environment, searchPath)
	if err != nil {
		return PreparedLaunch{}, err
	}
	authState, capabilities, err := probeCapabilities(ctx, definition, input, physical, argv, workspace, env)
	if err != nil {
		return PreparedLaunch{}, err
	}
	if definition.NativeContainment {
		capabilities["native_containment"] = true
	}
	deadline := input.Deadline
	if deadline <= 0 {
		deadline = defaultRunDeadline
	}
	limit := input.OutputLimit
	if limit <= 0 {
		limit = defaultOutputLimit
	}
	prepared := PreparedLaunch{
		HarnessID: definition.ID, Provider: definition.Provider, Transport: definition.Transport, Dialect: definition.Dialect,
		Executable: physical, ExecutableIdentity: identity, Version: version, Argv: argv, CWD: workspace,
		Environment: env, EnvironmentNames: envNames, RequestedPreset: input.Preset, EffectivePolicy: policy,
		Capabilities: capabilities, AuthState: authState, PreparedAt: time.Now().UTC(), Deadline: deadline,
		OutputLimit: limit, prompt: input.Prompt,
	}
	if input.ResolvedAccess != nil {
		prepared.Access = input.ResolvedAccess
	}
	prepared.ConfigurationHash = hashJSON(struct {
		Definition HarnessDefinition
		Workspace  string
	}{definition, workspace})
	prepared.LaunchHash = hashJSON(struct {
		ExecutableIdentity string
		Argv               []string
		CWD                string
		Policy             EffectivePolicy
		Config             string
	}{identity, argv, workspace, policy, prepared.ConfigurationHash})
	return prepared, nil
}

func validateDefinition(d HarnessDefinition, input RunInput) error {
	if strings.TrimSpace(d.ID) == "" || strings.TrimSpace(d.Provider) == "" || strings.TrimSpace(d.Executable) == "" {
		return admission(d, "invalid_configuration", "definition", "harness id, provider, and executable are required")
	}
	if d.SchemaVersion != 0 && d.SchemaVersion != 1 {
		return admission(d, "invalid_configuration", "schema", "unsupported harness schema version")
	}
	if d.Transport != TransportCLI && d.Transport != TransportACP {
		return admission(d, "invalid_configuration", "transport", "transport must be cli or acp_stdio")
	}
	if d.Transport == TransportCLI && d.Dialect != "codex" && d.Dialect != "claude" && d.Dialect != "muse" {
		return admission(d, "unsupported_dialect", "dialect", "CLI dialect must be codex, claude, or muse")
	}
	if d.Transport == TransportACP && d.Dialect != "" && d.Dialect != "acp" {
		return admission(d, "unsupported_dialect", "dialect", "ACP transport does not accept a CLI dialect")
	}
	if strings.TrimSpace(input.Workspace) == "" {
		return admission(d, "invalid_workspace", "workspace", "workspace is required")
	}
	switch input.Preset {
	case PresetReadOnly, PresetWorkspaceOffline, PresetWorkspaceNetwork, PresetDangerFullAccess:
	default:
		return admission(d, "policy_unenforceable", "policy", "unknown permission preset")
	}
	for _, arg := range d.Args {
		if strings.ContainsRune(arg, 0) {
			return admission(d, "invalid_configuration", "argv", "arguments cannot contain NUL")
		}
	}
	return nil
}

func executableCandidates(name, searchPath string) []string {
	name = strings.TrimSpace(name)
	if strings.ContainsRune(name, os.PathSeparator) {
		return []string{name}
	}
	seen, out := map[string]bool{}, []string{}
	for _, dir := range filepath.SplitList(searchPath) {
		if dir == "" {
			dir = "."
		}
		candidate := filepath.Join(dir, name)
		if _, err := os.Stat(candidate); err != nil {
			continue
		}
		abs, _ := filepath.Abs(candidate)
		if !seen[abs] {
			seen[abs] = true
			out = append(out, abs)
		}
	}
	return out
}

func executableIdentity(path, version string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := h.Write([]byte(version)); err != nil {
		return "", err
	}
	buf := make([]byte, 128<<10)
	for {
		n, readErr := f.Read(buf)
		if n > 0 {
			_, _ = h.Write(buf[:n])
		}
		if readErr != nil {
			break
		}
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}

func prepareEnvironment(overrides map[string]string, searchPath string) ([]string, []string, error) {
	envMap := map[string]string{}
	for _, key := range []string{"HOME", "USER", "LOGNAME", "LANG", "LC_ALL", "TMPDIR", "RUST_LOG", "CODEX_HOME", "ANTHROPIC_API_KEY", "CLAUDE_CODE_OAUTH_TOKEN", "AWS_PROFILE", "AWS_REGION", "GOOGLE_CLOUD_PROJECT"} {
		if value, ok := os.LookupEnv(key); ok {
			envMap[key] = value
		}
	}
	envMap["PATH"] = searchPath
	for key, value := range overrides {
		upper := strings.ToUpper(strings.TrimSpace(key))
		if upper == "" || strings.ContainsAny(upper, "=\x00") || strings.HasPrefix(upper, "TUSKER_") || upper == "PATH" || upper == "HOME" {
			return nil, nil, &AdmissionError{Code: "invalid_configuration", Check: "environment", Reason: "reserved or invalid environment override: " + key, Remedy: "Remove the override and use the installed harness configuration."}
		}
		envMap[upper] = value
	}
	names := make([]string, 0, len(envMap))
	for key := range envMap {
		names = append(names, key)
	}
	sort.Strings(names)
	env := make([]string, 0, len(names))
	for _, key := range names {
		env = append(env, key+"="+envMap[key])
	}
	return env, names, nil
}

func probeCapabilities(ctx context.Context, d HarnessDefinition, input RunInput, physical string, argv []string, cwd string, env []string) (string, map[string]bool, error) {
	if d.Transport == TransportACP {
		var stderr bytes.Buffer
		probeCtx, cancel := context.WithTimeout(ctx, defaultProbeDeadline)
		defer cancel()
		client, err := acp.Start(probeCtx, acp.Config{Argv: argv, CWD: cwd, Env: env, Stderr: &stderr, Limits: acp.Limits{MaxFrameBytes: maxProtocolFrame}})
		if err != nil {
			return "unknown", nil, admission(d, "probe_failed", "handshake", bounded(err.Error(), 300))
		}
		defer client.Close()
		initialized, err := client.Initialize(probeCtx)
		if err != nil {
			return "unknown", nil, admission(d, "probe_failed", "handshake", bounded(err.Error()+" "+stderr.String(), 300))
		}
		return "negotiated", map[string]bool{"structured_events": true, "permission_denial": true, "resume": initialized.AgentCapabilities.ResumeSession, "load_session": initialized.AgentCapabilities.LoadSession}, nil
	}
	if d.Provider == "muse" && d.Dialect != "muse" {
		if input.LiveCanary {
			return "pending_live_canary", map[string]bool{"structured_events": true, "permission_denial": true, "resume": true}, nil
		}
		if input.VerifiedAuth {
			return "verified_by_live_canary", map[string]bool{"structured_events": true, "permission_denial": true, "resume": true}, nil
		}
		return "unknown", nil, admission(d, "auth_missing", "auth", "Muse profile authentication has no noninteractive probe; run live conformance")
	}
	if d.Dialect == "muse" {
		// Direct Muse has a provider-free echo mode, but no safe, universal
		// noninteractive credential probe. Keep setup truthful while allowing
		// local argv/protocol qualification and explicit live conformance to
		// establish authentication.
		caps := map[string]bool{"structured_events": true, "permission_denial": true, "resume": true}
		if input.VerifiedAuth {
			return "verified_by_live_canary", caps, nil
		}
		return "unknown", caps, nil
	}
	probeCtx, cancel := context.WithTimeout(ctx, defaultProbeDeadline)
	defer cancel()
	var command *exec.Cmd
	switch d.Dialect {
	case "codex":
		command = exec.CommandContext(probeCtx, physical, "login", "status")
	case "claude":
		command = exec.CommandContext(probeCtx, physical, "auth", "status", "--json")
	}
	command.Env, command.Dir = env, cwd
	out, err := command.CombinedOutput()
	if probeCtx.Err() == context.DeadlineExceeded {
		return "unknown", nil, admission(d, "auth_missing", "auth", "authentication probe timed out")
	}
	if err != nil {
		return "unavailable", nil, admission(d, "auth_missing", "auth", bounded(strings.TrimSpace(string(out)), 300))
	}
	return "authenticated", map[string]bool{"structured_events": true, "permission_denial": true, "resume": true}, nil
}

func admission(d HarnessDefinition, code, check, reason string) *AdmissionError {
	return &AdmissionError{Code: code, HarnessID: d.ID, Check: check, Reason: reason, Remedy: "Fix the harness configuration and rerun conformance."}
}

func bounded(value string, n int) string {
	value = strings.TrimSpace(value)
	if len(value) <= n {
		return value
	}
	return value[:n] + "…"
}

func hashJSON(value any) string {
	raw, _ := json.Marshal(value)
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func verifyPreparedIdentity(p PreparedLaunch) error {
	current, err := executableIdentity(p.Executable, p.Version)
	if err != nil || current != p.ExecutableIdentity {
		return fmt.Errorf("launch_changed: prepared executable identity changed before spawn")
	}
	return nil
}
