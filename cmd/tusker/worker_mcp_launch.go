package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// workerMCPProjection is rebuilt from durable attempt identity on every launch.
// Only Claude needs files; all paths stay inside the private attempt state dir.
type workerMCPProjection struct {
	command        string
	args           []string
	env            map[string]string
	claudeConfig   string
	claudeSettings string
}

func projectWorkerMCP(project, record, item, attempt string, leaseGeneration, workRevision int, eventSink, statusPath string, maxWait int, claude bool) (workerMCPProjection, error) {
	if strings.TrimSpace(project) == "" || strings.TrimSpace(attempt) == "" || strings.TrimSpace(statusPath) == "" || strings.TrimSpace(eventSink) == "" {
		return workerMCPProjection{}, tuskerError(errorConfigInvalid, "worker MCP projection requires attempt identity, event sink, and status path")
	}
	exe, err := os.Executable()
	if err != nil || !filepath.IsAbs(exe) {
		return workerMCPProjection{}, tuskerError(errorConfigInvalid, "worker MCP executable is unavailable")
	}
	p := workerMCPProjection{command: exe, args: []string{"mcp", "serve", "--max-wait", fmt.Sprint(maxWait)}, env: map[string]string{
		"TUSKER_PROJECT_ID": project, "TUSKER_RECORD_ID": record, "TUSKER_ITEM_ID": item,
		"TUSKER_ATTEMPT_ID": attempt, "TUSKER_EVENT_SINK": eventSink,
		"TUSKER_LEASE_GENERATION": fmt.Sprint(leaseGeneration), "TUSKER_WORK_REVISION": fmt.Sprint(workRevision),
	}}
	stateRoot := strings.TrimSpace(os.Getenv("TUSKER_STATE_ROOT"))
	defaultRoot := filepath.Join(userHomeDir(), "Library", "Application Support", "tusker")
	if userHomeDir() == "" {
		defaultRoot = filepath.Join(os.TempDir(), "tusker")
	}
	if stateRoot != "" && stateRoot != defaultRoot {
		p.env["TUSKER_STATE_ROOT"] = stateRoot
	}
	if !claude {
		return p, nil
	}
	dir := filepath.Dir(statusPath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return workerMCPProjection{}, tuskerError(errorConfigInvalid, "worker MCP state directory is unavailable: "+err.Error())
	}
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() {
		return workerMCPProjection{}, tuskerError(errorConfigInvalid, "worker MCP state directory is not a direct directory")
	}
	if err := os.Chmod(dir, 0700); err != nil {
		return workerMCPProjection{}, tuskerError(errorConfigInvalid, "worker MCP state directory permissions could not be enforced: "+err.Error())
	}
	p.claudeConfig = filepath.Join(dir, "mcp.json")
	p.claudeSettings = filepath.Join(dir, "settings.json")
	config := map[string]any{"mcpServers": map[string]any{"tusker": map[string]any{"command": p.command, "args": p.args, "env": p.env}}}
	hookCommand := strings.Join([]string{shellSingleQuote(exe), "message", "inbox", "--project", shellSingleQuote(project), "--run", shellSingleQuote(attempt), "--format", "hook"}, " ")
	if stateRoot != "" && stateRoot != defaultRoot {
		hookCommand = "TUSKER_STATE_ROOT=" + shellSingleQuote(stateRoot) + " " + hookCommand
	}
	settings := map[string]any{
		"permissions": map[string]any{"allow": []string{"mcp__tusker"}},
		"hooks":       map[string]any{"PostToolUse": []any{map[string]any{"hooks": []any{map[string]any{"type": "command", "command": hookCommand}}}}},
	}
	for path, value := range map[string]any{p.claudeConfig: config, p.claudeSettings: settings} {
		b, err := json.Marshal(value)
		if err != nil {
			return workerMCPProjection{}, err
		}
		if err := os.WriteFile(path, b, 0600); err != nil {
			return workerMCPProjection{}, tuskerError(errorConfigInvalid, "worker MCP projection is unwritable: "+err.Error())
		}
		if err := os.Chmod(path, 0600); err != nil {
			return workerMCPProjection{}, tuskerError(errorConfigInvalid, "worker MCP projection permissions could not be enforced: "+err.Error())
		}
	}
	return p, nil
}

func appendClaudeMCP(argv []string, p workerMCPProjection) []string {
	return append(argv, "--mcp-config", p.claudeConfig, "--settings", p.claudeSettings)
}

func appendCodexMCP(argv []string, p workerMCPProjection) []string {
	// These flags must precede `resume`; the Codex CLI treats later tokens as
	// positional resume arguments.
	clean := make([]string, 0, len(argv))
	for i := 0; i < len(argv); i++ {
		if argv[i] == "-c" && i+1 < len(argv) && strings.HasPrefix(argv[i+1], "mcp_servers.tusker.") {
			i++
			continue
		}
		clean = append(clean, argv[i])
	}
	argv = clean
	flags := []string{"-c", "mcp_servers.tusker.command=" + jsonString(p.command), "-c", "mcp_servers.tusker.args=" + jsonString(p.args), "-c", "mcp_servers.tusker.tool_timeout_sec=960", "-c", `mcp_servers.tusker.default_tools_approval_mode="approve"`}
	keys := make([]string, 0, len(p.env))
	for key := range p.env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		flags = append(flags, "-c", "mcp_servers.tusker.env."+key+"="+jsonString(p.env[key]))
	}
	if len(argv) >= 2 && argv[1] == "exec" {
		return append(append(append([]string{}, argv[:2]...), flags...), argv[2:]...)
	}
	return append(argv, flags...)
}

func jsonString(v any) string { b, _ := json.Marshal(v); return string(b) }
