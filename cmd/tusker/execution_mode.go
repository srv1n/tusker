package main

import (
	"os"
	"strings"
)

func agentSessionKind() string {
	if strings.TrimSpace(os.Getenv("TUSKER_ATTEMPT_ID")) != "" {
		return "dispatched Tusker worker"
	}
	if strings.TrimSpace(os.Getenv("CODEX_SHELL")) != "" || strings.TrimSpace(os.Getenv("CODEX_THREAD_ID")) != "" || strings.TrimSpace(os.Getenv("CODEX_SESSION_ID")) != "" {
		return "interactive Codex session"
	}
	if strings.TrimSpace(os.Getenv("CLAUDECODE")) != "" || strings.TrimSpace(os.Getenv("CLAUDE_CODE_ENTRYPOINT")) != "" {
		return "interactive Claude session"
	}
	if strings.TrimSpace(os.Getenv("CHISEL_SESSION_DB")) != "" {
		return "interactive Devin session"
	}
	return ""
}

func requireOwnerSession(operation string) error {
	if agentSessionKind() != "" {
		return tuskerError(errorInvalidTransition, operation+" is owner-only and cannot run from an agent session")
	}
	return nil
}

func eventPayloadWithExecutionRole(payload map[string]any) map[string]any {
	if role := agentSessionKind(); role != "" {
		payload["execution_role"] = role
	}
	return payload
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
