package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// demoExec runs one CLI invocation and decodes its JSON envelope. Mutations
// go through the real command surface (the current binary re-executed, or an
// explicit TUSKER_DEMO_EXE override in tests), so demo runs exercise the same
// validation, permissions and effects as a human typing the commands.
type demoExec struct {
	Bin string
	Env []string
}

func demoNewExec() *demoExec {
	bin := strings.TrimSpace(os.Getenv("TUSKER_DEMO_EXE"))
	if bin == "" {
		self, err := os.Executable()
		if err == nil && self != "" {
			bin = self
		}
	}
	return &demoExec{Bin: bin, Env: os.Environ()}
}

// run invokes: bin subcommand... --json (appended unless present) in cwd and
// returns the decoded envelope. Non-JSON output is an internal error; a
// well-formed {"ok":false,...} envelope resolves to a typed demo error.
func (d *demoExec) run(cwd string, argv ...string) (map[string]any, error) {
	if d.Bin == "" {
		return nil, fmt.Errorf("demo executor has no binary")
	}
	hasJSON := false
	for _, arg := range argv {
		if arg == "--json" {
			hasJSON = true
		}
	}
	if !hasJSON {
		argv = append(append([]string{}, argv...), "--json")
	}
	cmd := exec.Command(d.Bin, argv...)
	cmd.Dir = cwd
	cmd.Env = d.Env
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	trimmed := bytes.TrimSpace(stdout.Bytes())
	if len(trimmed) == 0 {
		if runErr != nil {
			return nil, fmt.Errorf("tusker %s failed with no output: %s: %s", strings.Join(argv, " "), runErr.Error(), strings.TrimSpace(stderr.String()))
		}
		return map[string]any{"ok": true}, nil
	}
	var envelope map[string]any
	if err := json.Unmarshal(trimmed, &envelope); err != nil {
		// Some commands are human-output only and ignore --json. A zero
		// exit keeps their success; anything else is a real failure.
		if runErr == nil {
			return map[string]any{"ok": true, "output": string(trimmed)}, nil
		}
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = string(trimmed)
		}
		return nil, fmt.Errorf("tusker %s failed: %s: %s", strings.Join(argv, " "), runErr.Error(), detail)
	}
	// Success is explicit ok:true, a domain payload without an error, or a
	// human-output-only command that already exited zero. Only an explicit
	// ok:false or a present error object is a failure: commands like
	// `work start` emit a packet with neither field on success.
	if okValue, present := envelope["ok"]; present {
		if ok, _ := okValue.(bool); !ok {
			return nil, demoErrorFromEnvelope(argv, envelope)
		}
		return envelope, nil
	}
	if _, hasError := envelope["error"]; hasError {
		return nil, demoErrorFromEnvelope(argv, envelope)
	}
	return envelope, nil
}

func demoErrorFromEnvelope(argv []string, envelope map[string]any) error {
	code, message, hint := "UNKNOWN", "command failed", ""
	var context any
	if raw, ok := envelope["error"].(map[string]any); ok {
		if s, ok := raw["code"].(string); ok && s != "" {
			code = s
		}
		if s, ok := raw["message"].(string); ok && s != "" {
			message = s
		}
		if s, ok := raw["hint"].(string); ok {
			hint = s
		}
		context = raw["context"]
	}
	err := tuskerError(code, fmt.Sprintf("tusker %s: %s", strings.Join(argv, " "), message))
	if typed, ok := err.(*TuskerError); ok {
		typed.Hint = hint
		typed.Context = context
	}
	return err
}

func demoEnvelopeString(envelope map[string]any, keys ...string) string {
	current := envelope
	for i, key := range keys {
		value, ok := current[key]
		if !ok {
			return ""
		}
		if i == len(keys)-1 {
			if s, ok := value.(string); ok {
				return s
			}
			return ""
		}
		next, ok := value.(map[string]any)
		if !ok {
			return ""
		}
		current = next
	}
	return ""
}

// demoGit runs git inside the demo repo for seed/reset bookkeeping (init,
// baseline commit, worktree prune, branch cleanup).
func demoGit(repoRoot string, argv ...string) (string, error) {
	cmd := exec.Command("git", argv...)
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=tusker-demo", "GIT_AUTHOR_EMAIL=demo@tusker.local",
		"GIT_COMMITTER_NAME=tusker-demo", "GIT_COMMITTER_EMAIL=demo@tusker.local",
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %s: %s", strings.Join(argv, " "), err.Error(), strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
}
