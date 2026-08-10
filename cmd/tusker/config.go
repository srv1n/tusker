package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

const hookOutputLimit = 64 << 10

var (
	// Values may be quoted or contain spaces. Stop unquoted values only at a
	// structural delimiter/newline so `token: secret value` cannot leak its
	// suffix after redacting the first word.
	hookSecretPattern      = regexp.MustCompile(`(?i)(["']?)(token|secret|password|api[_-]?key|authorization|capability)\b(["']?)(\s*[:=]\s*)(?:"[^"\r\n]*"|'[^'\r\n]*'|[^\r\n,;}]+)`)
	hookQuerySecretPattern = regexp.MustCompile(`(?i)([?&](?:access_token|token|api[_-]?key|secret|password|capability)=)[^&#\s]+`)
	hookBearerPattern      = regexp.MustCompile(`(?i)\bBearer\s+[A-Za-z0-9._~+/=-]+`)
	hookKnownTokenPattern  = regexp.MustCompile(`(?i)\b(?:ghp_[A-Za-z0-9]{20,}|github_pat_[A-Za-z0-9_]{20,}|xox[baprs]-[A-Za-z0-9-]{10,}|sk-[A-Za-z0-9_-]{16,})\b`)
)

func redactHookOutput(text string) string {
	text = hookSecretPattern.ReplaceAllString(text, `$1$2$3$4[REDACTED]`)
	text = hookQuerySecretPattern.ReplaceAllString(text, `$1[REDACTED]`)
	text = hookBearerPattern.ReplaceAllString(text, `Bearer [REDACTED]`)
	return hookKnownTokenPattern.ReplaceAllString(text, `[REDACTED]`)
}

type boundedHookOutput struct {
	mu        sync.Mutex
	buf       bytes.Buffer
	truncated bool
}

func (w *boundedHookOutput) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	remaining := hookOutputLimit - w.buf.Len()
	if remaining <= 0 {
		w.truncated = true
		return len(p), nil
	}
	if len(p) > remaining {
		_, _ = w.buf.Write(p[:remaining])
		w.truncated = true
		return len(p), nil
	}
	_, _ = w.buf.Write(p)
	return len(p), nil
}

func (w *boundedHookOutput) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	text := redactHookOutput(w.buf.String())
	if w.truncated {
		text += "\n[hook output truncated]"
	}
	return text
}

func writeDefaultConfig(vaultPath string) error {
	// V7 stores workflow policy in WORKFLOW.md and .tusker/config.yaml. Do not create
	// legacy _system/config.yaml during normal init; that file is only read for
	// explicit legacy/migration flows.
	return writeDefaultWorkflow(vaultPath)
}

func loadConfig(vaultPath string) (Config, string, bool, error) {
	if fileExists(workflowPath(vaultPath)) {
		wfFile, err := loadWorkflow(vaultPath)
		if err != nil {
			return Config{}, workflowPath(vaultPath), false, err
		}
		cfg := workflowToConfig(wfFile.Data)
		return cfg, wfFile.Path, false, nil
	}
	filePath := legacyConfigPath(vaultPath)
	cfg := defaultConfig()
	if !fileExists(filePath) {
		return cfg, filePath, true, nil
	}
	raw, err := readText(filePath)
	if err != nil {
		return Config{}, filePath, false, err
	}
	var user Config
	if err := yaml.Unmarshal([]byte(raw), &user); err != nil {
		return Config{}, filePath, false, tuskerError(errorConfigInvalid, fmt.Sprintf("failed to parse _system/config.yaml: %s", err.Error()), withPath(filePath))
	}
	mergeConfig(&cfg, user)
	if err := validateConfig(cfg, filePath); err != nil {
		return Config{}, filePath, false, err
	}
	return cfg, filePath, false, nil
}

func defaultConfig() Config {
	var cfg Config
	cfg.Version = 1
	cfg.Agents.Enabled = []string{"sarav", "claude-code", "codex", "gemini"}
	cfg.Agents.Concurrency = map[string]int{"claude-code": 2, "codex": 1, "gemini": 1, "sarav": 0}
	cfg.Poll.IntervalSeconds = 60
	cfg.Hooks.PreClaim = []string{}
	cfg.Hooks.PostClaim = []string{}
	cfg.Hooks.PreRelease = []string{}
	cfg.Hooks.OnFail = []string{}
	cfg.HookTimeoutSeconds = 120
	cfg.Retry.MaxAttempts = 3
	cfg.Retry.BackoffSeconds = []int{30, 120, 600}
	cfg.Workspace.Root = "."
	cfg.Workspace.Isolation = string(WorkspaceStrategyShared)
	cfg.DefinitionOfDone.RequireCodeComplete = true
	cfg.DefinitionOfDone.RequireUserVerifiedForUI = true
	return cfg
}

func mergeConfig(dst *Config, src Config) {
	if src.Version != 0 {
		dst.Version = src.Version
	}
	if src.Agents.Enabled != nil {
		dst.Agents.Enabled = src.Agents.Enabled
	}
	if src.Agents.Concurrency != nil {
		dst.Agents.Concurrency = src.Agents.Concurrency
	}
	if src.Poll.IntervalSeconds != 0 {
		dst.Poll.IntervalSeconds = src.Poll.IntervalSeconds
	}
	if src.Hooks.PreClaim != nil {
		dst.Hooks.PreClaim = src.Hooks.PreClaim
	}
	if src.Hooks.PostClaim != nil {
		dst.Hooks.PostClaim = src.Hooks.PostClaim
	}
	if src.Hooks.PreRelease != nil {
		dst.Hooks.PreRelease = src.Hooks.PreRelease
	}
	if src.Hooks.OnFail != nil {
		dst.Hooks.OnFail = src.Hooks.OnFail
	}
	if src.HookTimeoutSeconds != 0 {
		dst.HookTimeoutSeconds = src.HookTimeoutSeconds
	}
	if src.Retry.MaxAttempts != 0 {
		dst.Retry.MaxAttempts = src.Retry.MaxAttempts
	}
	if src.Retry.BackoffSeconds != nil {
		dst.Retry.BackoffSeconds = src.Retry.BackoffSeconds
	}
	if src.Budget.MonthlyUSDCeiling != nil {
		dst.Budget.MonthlyUSDCeiling = src.Budget.MonthlyUSDCeiling
	}
	if src.Budget.DailyUSDCeiling != nil {
		dst.Budget.DailyUSDCeiling = src.Budget.DailyUSDCeiling
	}
	if src.Workspace.Root != "" {
		dst.Workspace.Root = src.Workspace.Root
	}
	if src.Workspace.Isolation != "" {
		dst.Workspace.Isolation = src.Workspace.Isolation
	}
	if src.DefinitionOfDone.RequireCodeComplete != dst.DefinitionOfDone.RequireCodeComplete {
		dst.DefinitionOfDone.RequireCodeComplete = src.DefinitionOfDone.RequireCodeComplete
	}
	if src.DefinitionOfDone.RequireUserVerifiedForUI != dst.DefinitionOfDone.RequireUserVerifiedForUI {
		dst.DefinitionOfDone.RequireUserVerifiedForUI = src.DefinitionOfDone.RequireUserVerifiedForUI
	}
}

func validateConfig(cfg Config, filePath string) error {
	if cfg.Agents.Enabled == nil {
		return tuskerError(errorConfigInvalid, "agents.enabled must be a list", withPath(filePath))
	}
	for agent, capValue := range cfg.Agents.Concurrency {
		if capValue < 0 {
			return tuskerError(errorConfigInvalid, fmt.Sprintf("agents.concurrency.%s must be a non-negative integer, got %d", agent, capValue), withPath(filePath))
		}
	}
	if cfg.Retry.BackoffSeconds == nil {
		return tuskerError(errorConfigInvalid, "retry.backoff_seconds must be a list", withPath(filePath))
	}
	return nil
}

func runHooks(event string, config Config, vaultPath, id, actor, dispatchState string) error {
	var commands []string
	switch event {
	case "pre_claim":
		commands = config.Hooks.PreClaim
	case "post_claim":
		commands = config.Hooks.PostClaim
	case "pre_release":
		commands = config.Hooks.PreRelease
	case "on_fail":
		commands = config.Hooks.OnFail
	}
	if len(commands) == 0 {
		return nil
	}
	timeout := time.Duration(config.HookTimeoutSeconds) * time.Second
	for _, command := range commands {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		cmd := exec.CommandContext(ctx, "sh", "-c", command)
		output := &boundedHookOutput{}
		cmd.Stdout = output
		cmd.Stderr = output
		cmd.Env = append(os.Environ(),
			"TUSKER_VAULT="+vaultPath,
			"TUSKER_EVENT="+event,
			"TUSKER_ID="+id,
			"TUSKER_ACTOR="+actor,
			"TUSKER_DISPATCH_STATE="+dispatchState,
		)
		err := cmd.Run()
		cancel()
		if ctx.Err() == context.DeadlineExceeded {
			return tuskerError(errorHookTimeout, fmt.Sprintf("hook timed out after %ds", config.HookTimeoutSeconds), withContext(map[string]any{"event": event, "output": output.String()}))
		}
		if err != nil {
			return tuskerError(errorHookFailed, "hook failed", withContext(map[string]any{"event": event, "output": output.String()}))
		}
	}
	return nil
}
