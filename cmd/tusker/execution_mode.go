package main

import (
	"os"
	"strings"
)

func agentSessionKind() string {
	if strings.TrimSpace(os.Getenv("TUSKER_ATTEMPT_ID")) != "" {
		return "dispatched Tusker worker"
	}
	if strings.TrimSpace(os.Getenv("CODEX_SHELL")) != "" || strings.TrimSpace(os.Getenv("CODEX_THREAD_ID")) != "" {
		return "interactive Codex session"
	}
	if strings.TrimSpace(os.Getenv("CLAUDECODE")) != "" || strings.TrimSpace(os.Getenv("CLAUDE_CODE_ENTRYPOINT")) != "" {
		return "interactive Claude session"
	}
	return ""
}

func rejectAgentSpawn(command string) error {
	kind := agentSessionKind()
	if kind == "" {
		return nil
	}
	return tuskerError(
		errorInvalidTransition,
		command+" cannot run from "+kind,
		withHint("interactive agents execute work directly; background model runners may be launched only by an independently running resident daemon"),
		withContext(map[string]any{"execution_role": kind, "command": command}),
	)
}
