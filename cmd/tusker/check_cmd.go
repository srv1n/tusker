package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"tusker/internal/docgraph"
)

// checkPartNames is the run order. acp is opt-in: it needs an installed
// adapter bundle, so it only runs when named in --only.
var checkPartNames = []string{"setup", "validate", "skills", "docs", "acp"}

// checkStep is one existing handler run as part of `tusker check`.
type checkStep struct {
	Part, Name string
	Run        func(Args) (int, error)
}

type checkStepResult struct {
	Name     string          `json:"name"`
	ExitCode int             `json:"exit_code"`
	OK       bool            `json:"ok"`
	Report   json.RawMessage `json:"report,omitempty"`
	Output   string          `json:"output,omitempty"`
	Error    *Issue          `json:"error,omitempty"`
}

type checkPartResult struct {
	OK    bool              `json:"ok"`
	Steps []checkStepResult `json:"steps"`
}

func checkCmd(args Args) (int, error) {
	if args.Bool("help") {
		printCheckHelp()
		return 0, nil
	}
	steps, err := checkPlan(args)
	if err != nil {
		return 0, err
	}
	return runCheckSteps(args, steps), nil
}

// checkPlan routes the selected parts to their existing handlers. Under --fix
// the deterministic repairs run first, so the checks that follow report the
// repaired state.
func checkPlan(args Args) ([]checkStep, error) {
	selected := map[string]bool{}
	if only := strings.TrimSpace(args.String("only")); only != "" && only != "true" {
		for _, name := range strings.Split(only, ",") {
			name = strings.TrimSpace(name)
			if !containsString(checkPartNames, name) {
				return nil, tuskerError(errorInvalidArg, "unknown check part: "+name, withHint("use --only "+strings.Join(checkPartNames, ",")))
			}
			selected[name] = true
		}
	} else {
		for _, name := range checkPartNames[:4] {
			selected[name] = true
		}
	}
	fix := args.Bool("fix")
	var steps []checkStep
	add := func(part, name string, run func(Args) (int, error)) {
		if selected[part] {
			steps = append(steps, checkStep{Part: part, Name: name, Run: run})
		}
	}
	if fix {
		add("setup", "setup repair", func(a Args) (int, error) { return setupDoctorCheck(a, true) })
		add("setup", "vault repair", func(a Args) (int, error) { return vaultMountsCheck(a, true) })
		add("setup", "workspace mode", workspaceModeCheck)
		add("validate", "reconcile", run0(reconcileV7Cmd))
		add("validate", "reindex", run0(reindex))
		add("docs", "docs map", run0(checkDocsMapIfStale))
	} else {
		add("setup", "setup doctor", func(a Args) (int, error) { return setupDoctorCheck(a, false) })
		add("setup", "vault status", func(a Args) (int, error) { return vaultMountsCheck(a, false) })
		add("setup", "workspace mode", workspaceModeCheck)
	}
	add("validate", "validate", validateCmd)
	add("skills", "skill doctor", skillV7DoctorCmd)
	add("skills", "skill audit-agent-guidance", skillV7AuditAgentGuidanceCmd)
	add("docs", "docs check", func(a Args) (int, error) { return 0, docsCmd("check", a) })
	add("docs", "docs status --stale", func(a Args) (int, error) {
		a["stale"] = "true"
		return 0, docsCmd("status", a)
	})
	if since := strings.TrimSpace(args.String("since")); since != "" && since != "true" {
		add("docs", "docs touch", func(a Args) (int, error) {
			a["since"] = since
			return 0, docsCmd("touch", a)
		})
	}
	add("acp", "acp doctor", func(a Args) (int, error) {
		acpArgs := Args{"json": "true"}
		for _, key := range []string{"bundle-digest", "auth-source"} {
			if value, ok := a[key]; ok {
				acpArgs[key] = value
			}
		}
		return 0, acpDoctorCommand(acpArgs)
	})
	return steps, nil
}

func checkDocsMapIfStale(args Args) error {
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	issues, err := docgraph.CheckDocsMapFresh(v7RepoRoot(vaultPath))
	if err != nil {
		return err
	}
	if len(issues) == 0 {
		if !args.Bool("json") {
			fmt.Println("Docs map is current.")
		}
		return nil
	}
	return docsMapCmd(args)
}

// runCheckSteps runs every step even after a failure and returns the worst
// exit code. --json captures each step's own JSON into one object.
func runCheckSteps(args Args, steps []checkStep) int {
	jsonMode := args.Bool("json")
	worst := 0
	parts := map[string]*checkPartResult{}
	for _, step := range steps {
		stepArgs := checkStepArgs(args)
		var code int
		var err error
		result := checkStepResult{Name: step.Name}
		if jsonMode {
			out := captureCheckStdout(func() { code, err = step.Run(stepArgs) })
			if trimmed := bytes.TrimSpace(out); json.Valid(trimmed) && len(trimmed) > 0 {
				result.Report = json.RawMessage(trimmed)
			} else {
				result.Output = string(out)
			}
		} else {
			fmt.Printf("== %s: %s ==\n", step.Part, step.Name)
			code, err = step.Run(stepArgs)
		}
		if err != nil {
			var docsFailure *docsCheckFailure
			if !errors.As(err, &docsFailure) {
				issue := errorToIssue(err)
				result.Error = &issue
				if !jsonMode {
					reportCLIError(err, false)
				}
			}
			if code == 0 {
				code = 1
			}
		}
		result.ExitCode, result.OK = code, code == 0
		worst = max(worst, code)
		part := parts[step.Part]
		if part == nil {
			part = &checkPartResult{OK: true}
			parts[step.Part] = part
		}
		part.OK = part.OK && result.OK
		part.Steps = append(part.Steps, result)
	}
	if jsonMode {
		emitJSON(map[string]any{"schema": "tusker.check/v1", "ok": worst == 0, "exit_code": worst, "fix": args.Bool("fix"), "parts": parts})
		return worst
	}
	fmt.Println("== summary ==")
	for _, name := range checkPartNames {
		if part := parts[name]; part != nil {
			state := "ok"
			if !part.OK {
				state = "FAIL"
			}
			fmt.Printf("  %-9s %s\n", name, state)
		}
	}
	return worst
}

// checkStepArgs hands each handler a fresh copy without check's own flags or
// positionals, so a handler cannot see or mutate another part's arguments.
func checkStepArgs(args Args) Args {
	out := Args{}
	for key, value := range args {
		if key == "only" || key == "fix" || key == "since" || strings.HasPrefix(key, "_pos") {
			continue
		}
		out[key] = value
	}
	return out
}

// captureCheckStdout swaps os.Stdout for the duration of fn. Handlers print
// with fmt, which resolves os.Stdout per call, so the swap catches them.
func captureCheckStdout(fn func()) []byte {
	previous := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		fn()
		return nil
	}
	done := make(chan []byte, 1)
	go func() {
		out, _ := io.ReadAll(reader)
		done <- out
	}()
	os.Stdout = writer
	defer func() { os.Stdout = previous }()
	func() {
		defer writer.Close()
		fn()
	}()
	return <-done
}

func printCheckHelp() {
	fmt.Println(`Usage:
  tusker check [--only setup,validate,skills,docs,acp] [--fix] [--since <ref>] [--strict] [--json]

Purpose:
  One health check for the repo and machine. Bare, it runs:
    setup     setup drift (old setup doctor) and Obsidian mounts (old vault status)
    validate  vault schema and workflow rules (same as tusker check --only validate)
    skills    project skill package (old skill doctor) and stray AGENTS.md/CLAUDE.md
              guidance (old skill audit-agent-guidance)
    docs      docs front matter and links (old docs check) and stale docs (old docs status --stale)
    acp       ACP adapter bundle (old acp doctor); only when named in --only

Behavior:
  - every part runs even after a failure; exit code is the worst part's
  - --fix first applies deterministic repairs: setup repair, vault mount repair,
    reconcile, reindex, and docs map when stale; then runs the checks
  - --since <ref> adds the docs touch-rule check for code changed since <ref>
  - --strict makes skill warnings and stale docs fail
  - --json prints one object with a section per part

Examples:
  tusker check
  tusker check --only docs --since origin/main
  tusker check --fix
  tusker check --only acp --json`)
}

// workspaceModeCheck warns when .tusker config overrides the workspace mode
// that WORKFLOW.md names, which older `tusker init` templates did silently.
func workspaceModeCheck(args Args) (int, error) {
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return 0, err
	}
	text, err := readText(workflowPath(vaultPath))
	if err != nil {
		return 0, nil
	}
	data, _, err := parseFrontmatter(text)
	if err != nil {
		return 0, nil
	}
	resolved, err := resolveTuskerConfig(vaultPath)
	if err != nil {
		return 0, err
	}
	workflowValue, _ := lookupConfigValue(data, "workspace.strategy")
	workflowText, _ := workflowValue.(string)
	workflowMode := workspaceStrategyFromWorkflow(workflowText)
	configured := strings.TrimSpace(resolved.Config.Automation.Workspace.Strategy)
	var message string
	switch {
	case !resolvedConfigKeyPresent(resolved, "automation.workspace.strategy"):
		message = fmt.Sprintf("Workspace mode: %s (WORKFLOW.md).", workflowMode)
	case workspaceStrategyFromWorkflow(configured) == workflowMode:
		message = fmt.Sprintf("Workspace mode: %s (.tusker config, matches WORKFLOW.md).", workflowMode)
	default:
		message = fmt.Sprintf("Warning: .tusker config sets automation.workspace.strategy %q, which overrides WORKFLOW.md (%s). Remove one so a single setting decides.", configured, workflowMode)
	}
	if args.Bool("json") {
		return 0, json.NewEncoder(os.Stdout).Encode(map[string]string{"message": message})
	}
	fmt.Println(message)
	return 0, nil
}
