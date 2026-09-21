package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

func demoCmd(args Args) (int, error) {
	printDemoHelp()
	return 0, nil
}

func printDemoHelp() {
	fmt.Println(`Usage:
  tusker demo seed --repo <dedicated-demo-path> --scenario parallel-waves [--with-human-gate] [--with-second-project] [--visible] [--json]
  tusker demo status --repo <dedicated-demo-path> [--json]
  tusker demo run --repo <dedicated-demo-path> --waves alpha,beta [--fast] [--fail-once B3] [--reject-once C2] [--require-harness NAME] [--json]
  tusker demo run --repo <dedicated-demo-path> --waves standalone --mode real --require-harness NAME [--profile NAME] [--timeout 15m] [--json]
  tusker demo wait --repo <dedicated-demo-path> --until terminal --timeout 120s [--json]
  tusker demo check --repo <dedicated-demo-path> [--json]
  tusker demo reset --repo <dedicated-demo-path> [--dry-run] [--yes] [--json]

Purpose:
  Build and drive a disposable repeatable-work demo: one standalone smoke
  task plus three waves (thirteen tasks) exercised through the native CLI.
  Seed is inert: it imports task contracts and leaves every wave unstarted.
  The default offline lane authorizes only the named waves and executes them
  with the deterministic demo-timer executor (fixed delays, exact fixture
  bytes), recording real attempts, evidence, and reviewer closes. No daemon,
  GUI, or provider is required. The real-harness lane (--mode real) instead
  resolves effective profiles, requires the named harness with no silent
  substitution, and waits for the configured runtime to do the real work.

Safety:
  Every mutating command requires the .tusker/demo/manifest.json ownership
  marker and an exact canonical path match. Seed refuses non-empty foreign
  directories; reset previews by default, refuses active runs, and only
  deletes demo-owned paths inside the marker repo.`)
}

// ---------------------------------------------------------------------------
// seed
// ---------------------------------------------------------------------------

func demoSeedCmd(args Args) (int, error) {
	payload, err := demoSeed(args)
	if err != nil {
		return demoFail(args, demoExitForError(err), err)
	}
	payload["ok"] = true
	if err := demoEmit(args, payload); err != nil {
		return demoExitInternal, err
	}
	return demoExitOK, nil
}

func demoSeed(args Args) (map[string]any, error) {
	scenario := firstNonEmpty(strings.TrimSpace(args.String("scenario")), demoScenario)
	if scenario != demoScenario {
		return nil, tuskerError(errorInvalidArg, "unknown demo scenario: "+scenario, withHint("only "+demoScenario+" is supported"))
	}
	raw := strings.TrimSpace(args.String("repo"))
	if raw == "" {
		return nil, tuskerError(errorMissingArg, "Usage: tusker demo seed --repo <dedicated-demo-path> --scenario parallel-waves")
	}
	abs, err := filepath.Abs(raw)
	if err != nil {
		return nil, err
	}
	canonical, err := filepath.EvalSymlinks(abs)
	if err != nil {
		if mkErr := os.MkdirAll(abs, 0o755); mkErr != nil {
			return nil, tuskerError(errorInvalidArg, "demo repo does not exist and cannot be created: "+raw)
		}
		canonical, err = filepath.EvalSymlinks(abs)
		if err != nil {
			return nil, err
		}
	}
	repoRoot := canonical
	withGate := args.Bool("with-human-gate")
	withSecond := args.Bool("with-second-project")
	visible := args.Bool("visible")

	if existing, loadErr := demoLoadManifest(repoRoot); loadErr == nil {
		if existing.ScenarioVersion != demoScenarioVersion || existing.HumanGate != withGate || existing.Visible != visible {
			return nil, tuskerError(demoCodeSeedMismatch, "existing seed differs (version or options); reset before reseeding", withHint("tusker demo reset --repo "+repoRoot+" --yes"))
		}
		return demoSeedReport(repoRoot, existing, true), nil
	}

	if err := demoEnsureEmptyRepo(repoRoot); err != nil {
		return nil, err
	}
	// Capture the caller's state-root override before ensure pins the demo
	// scope: runtime registration targets the caller's scope (or the true
	// default store) so the normal UI can discover the project.
	preEnsureOverride := strings.TrimSpace(os.Getenv("TUSKER_STATE_ROOT"))
	stateRoot := demoEnsureStateRoot(repoRoot)
	exec := demoNewExec()
	exec.Env = append(exec.Env, "TUSKER_STATE_ROOT="+stateRoot)
	vaultPath := demoVaultPath(repoRoot)
	actor := "agent:demo-seed"

	if _, err := demoGit(repoRoot, "init", "-b", "main"); err != nil {
		// An existing repo keeps its history; make sure main exists.
		if _, err2 := demoGit(repoRoot, "rev-parse", "--verify", "main"); err2 != nil {
			return nil, tuskerError(errorInvalidArg, "demo repo is not a git repository and git init failed: "+err.Error())
		}
	}
	if _, err := exec.run(repoRoot, "init", "--vault", vaultPath, "--yes", "--vault-only", "--no-mount"); err != nil {
		return nil, err
	}
	if err := demoConfigureUnattendedWorkflow(vaultPath); err != nil {
		return nil, err
	}
	if err := writeText(filepath.Join(vaultPath, "specs", "demo-parallel-waves.md"), demoSpecDoc()); err != nil {
		return nil, err
	}
	if err := demoSeedGitignore(repoRoot); err != nil {
		return nil, err
	}
	profiles, err := demoSeedProfiles(repoRoot)
	if err != nil {
		return nil, err
	}
	// Declare the fixture's required capacity as project policy: two waves
	// run concurrently with two-task frontiers, so the demo project allows
	// four live runs. This touches only the demo repo's local overlay, never
	// unrelated global settings.
	if err := writeText(filepath.Join(vaultPath, "config.local.yaml"), "automation:\n  completion_reactor:\n    mode: authoritative\n  concurrency:\n    max_active_runs: 4\n    max_active_runs_per_project: 4\n  profiles:\n    execute-fast:\n      harness: codex_exec\n      model: gpt-5.6-luna\n      effort: medium\n      permission_preset: workspace-write-offline\n      sandbox:\n        mode: workspace-write\n        network: false\n      subagents:\n        allowed: false\n        max_concurrent: 0\n    review-independent:\n      harness: codex_exec\n      model: gpt-5.6-luna\n      effort: medium\n      permission_preset: read-only\n      sandbox:\n        mode: read-only\n        network: false\n      subagents:\n        allowed: false\n        max_concurrent: 0\n  model_levels:\n    light:\n      execute: [execute-fast]\n      review: [review-independent]\n    standard:\n      execute: [execute-fast]\n      review: [review-independent]\n    demanding:\n      execute: [execute-fast]\n      review: [review-independent]\n  validation:\n    commands:\n      - git diff --check\n"); err != nil {
		return nil, err
	}
	createdPaths, err := demoSeedRealWorkFiles(repoRoot)
	if err != nil {
		return nil, err
	}
	if _, err := demoGit(repoRoot, "add", "-A"); err != nil {
		return nil, err
	}
	if _, err := demoGit(repoRoot, "commit", "-qm", "demo baseline"); err != nil {
		return nil, err
	}

	manifest := &demoManifest{
		Schema: demoManifestSchema, Scenario: demoScenario, ScenarioVersion: demoScenarioVersion,
		RepoRoot: repoRoot, Vault: ".tusker", SeededAt: time.Now().UTC().Format(time.RFC3339),
		SeededBy: actor, HumanGate: withGate, Visible: visible,
		Waves: map[string]demoWaveRecord{}, Tasks: map[string]demoTaskRecord{}, Profiles: profiles,
		CreatedPaths: createdPaths,
	}
	mappings := map[string]map[string]string{}
	for _, wave := range demoFixtureWaves() {
		mapping, waveID, err := demoAuthorWave(exec, repoRoot, vaultPath, wave, withGate && wave.Name == "follow-up", actor)
		if err != nil {
			return nil, err
		}
		mappings[wave.Name] = mapping
		if _, err := demoGit(repoRoot, "branch", "-f", "integration/"+waveID, "HEAD"); err != nil {
			return nil, err
		}
		record := demoWaveRecord{Name: wave.Name, WaveID: waveID, Title: wave.Title, Scope: wave.Scope, Epic: wave.Epic}
		for _, task := range wave.Tasks {
			taskID, ok := mapping[task.Key]
			if !ok || taskID == "" {
				return nil, tuskerError(demoCodePrecondition, "wave create did not map task "+task.Key)
			}
			record.Members = append(record.Members, taskID)
			deps := append([]string{}, task.Deps...)
			for _, cross := range demoCrossScopeDeps()[task.Key] {
				deps = append(deps, cross[0]+"/"+cross[1])
			}
			manifest.Tasks[task.Key] = demoTaskRecord{
				SourceKey: task.Key, TaskID: taskID, Wave: wave.Name, Title: task.Title,
				Artifact: task.Artifact, Content: task.Content,
				DelaySecs: task.DelaySecs, FastSecs: task.FastSecs, Deps: deps,
				Complexity: task.Complexity,
			}
		}
		manifest.Waves[wave.Name] = record
	}
	if err := demoApplyCrossScopeDeps(exec, repoRoot, vaultPath, mappings, actor); err != nil {
		return nil, err
	}

	if _, err := exec.run(repoRoot, "new", "decision", "--epic", demoEpicAlpha, "--title", demoDecisionTitle, "--vault", vaultPath); err != nil {
		return nil, err
	}
	// Roots are marked ready so the demo scheduler can claim them directly;
	// branches, joins and the follow-up stay backlog until their
	// dependencies are really satisfied.
	for key, task := range manifest.Tasks {
		if len(fixtureDeps(key)) > 0 {
			continue
		}
		if _, err := exec.run(repoRoot, "status", task.TaskID, "ready", "--reason", "demo seed: wave ready to start", "--vault", vaultPath); err != nil {
			return nil, err
		}
	}
	localProjectID, err := demoRegisterProject(repoRoot, exec, vaultPath)
	if err != nil {
		return nil, err
	}
	manifest.LocalProjectID = localProjectID
	// Default-scope registration makes the disposable project discoverable
	// by the normal UI/runtime instance. Best-effort: when unavailable, seed
	// still prepares data and reports the missing step.
	runtimeScope, runtimeStrip := demoRuntimeScope(preEnsureOverride, repoRoot)
	manifest.RuntimeScope = runtimeScope
	manifest.RuntimeStripScope = runtimeStrip
	if !runtimeStrip {
		// Shared store: the runtime registration is the local row.
		manifest.RuntimeProjectID = localProjectID
		manifest.RuntimeRegistered = localProjectID != ""
	} else if runtimeID, regErr := demoRegisterRuntimeProject(repoRoot, exec, vaultPath, runtimeScope, runtimeStrip); regErr != nil {
		manifest.RuntimeProblem = regErr.Error()
	} else {
		manifest.RuntimeProjectID = runtimeID
		manifest.RuntimeRegistered = true
	}
	if withSecond {
		side, err := demoSeedSecondProject(repoRoot, exec)
		if err != nil {
			return nil, err
		}
		manifest.SecondProject = side
	}
	if err := demoSaveManifest(repoRoot, manifest); err != nil {
		return nil, err
	}
	return demoSeedReport(repoRoot, manifest, false), nil
}

func demoConfigureUnattendedWorkflow(vaultPath string) error {
	data, body, err := parseFrontmatterMustRead(workflowPath(vaultPath))
	if err != nil {
		return err
	}
	codex, _ := data["codex"].(map[string]any)
	if codex == nil {
		codex = map[string]any{}
		data["codex"] = codex
	}
	codex["approval_policy"] = "never"
	reviewer, _ := data["reviewer"].(map[string]any)
	if reviewer == nil {
		reviewer = map[string]any{}
		data["reviewer"] = reviewer
	}
	reviewer["enabled"] = true
	content, err := serializeDocument(data, body, nil)
	if err != nil {
		return err
	}
	return writeText(workflowPath(vaultPath), content)
}

func fixtureDeps(key string) []string {
	if cross, ok := demoCrossScopeDeps()[key]; ok {
		out := make([]string, 0, len(cross))
		for _, dep := range cross {
			out = append(out, dep[0]+"/"+dep[1])
		}
		return out
	}
	for _, wave := range demoFixtureWaves() {
		for _, task := range wave.Tasks {
			if task.Key == key {
				return append([]string{}, task.Deps...)
			}
		}
	}
	return nil
}

func demoEnsureEmptyRepo(repoRoot string) error {
	if dirExists(filepath.Join(repoRoot, ".tusker")) {
		return tuskerError(errorInvalidArg, "refusing to seed over an existing vault: "+repoRoot, withHint("seed only creates dedicated demo repos; point --repo at an empty directory"))
	}
	entries, err := os.ReadDir(repoRoot)
	if err != nil {
		return err
	}
	// A fresh git repo with no commits has at most a .git directory.
	kept := []string{}
	for _, entry := range entries {
		if entry.Name() == ".git" {
			continue
		}
		kept = append(kept, entry.Name())
	}
	if len(kept) > 0 {
		return tuskerError(errorInvalidArg, "refusing to seed into a non-empty directory: "+repoRoot, withHint("use an empty directory so the demo stays disposable; if a previous seed was interrupted, remove the directory and reseed"))
	}
	return nil
}

func demoSeedGitignore(repoRoot string) error {
	path := filepath.Join(repoRoot, ".gitignore")
	existing := ""
	if fileExists(path) {
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		existing = string(raw)
	}
	// Task work files must not dirty the work tree: work claims require a
	// clean tree outside .tusker, and parallel tasks write concurrently.
	// The sample project itself (README, tools) stays tracked; only the
	// per-task work directories are ignored.
	for _, line := range []string{"sample/standalone/", "sample/alpha/", "sample/beta/", "sample/followup/"} {
		if !strings.Contains(existing, line) {
			existing += line + "\n"
		}
	}
	return os.WriteFile(path, []byte(existing), 0o644)
}

func demoSeedProfiles(repoRoot string) (map[string]string, error) {
	// Demo-timer profiles are declared in demo-scoped config, never in
	// automation.profiles: the platform only admits operator-installed
	// harnesses there, and a fake harness would either break workflow
	// loading or misrepresent execution. The demo executor reads this
	// file; nothing else routes through it, and no silent substitution
	// to another harness ever happens.
	sandbox := map[string]any{"mode": "workspace-write", "network": false}
	subagents := map[string]any{"allowed": false, "max_concurrent": 0}
	doc := map[string]any{
		"schema": "tusker.demo-profiles/v1", "scope": "repeatable-work-testing",
		"executor": "demo-timer",
		"levels": map[string]any{
			"implement": map[string]any{"profile": demoProfileImplement, "harness": "demo-timer", "model": "demo-timer", "effort": "medium", "permission_preset": "workspace-write-offline", "sandbox": sandbox, "subagents": subagents},
			"review":    map[string]any{"profile": demoProfileReview, "harness": "demo-timer", "model": "demo-timer", "effort": "low", "permission_preset": "workspace-write-offline", "sandbox": sandbox, "subagents": subagents},
			"plan":      map[string]any{"profile": demoProfilePlan, "harness": "demo-timer", "model": "demo-timer", "effort": "high", "permission_preset": "workspace-write-offline", "sandbox": sandbox, "subagents": subagents},
		},
	}
	out, err := yaml.Marshal(doc)
	if err != nil {
		return nil, err
	}
	if err := ensureDir(demoDir(repoRoot)); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(demoDir(repoRoot), "profiles.yaml"), out, 0o644); err != nil {
		return nil, err
	}
	return map[string]string{"implement": demoProfileImplement, "review": demoProfileReview, "plan": demoProfilePlan}, nil
}

// demoEnsureEpic creates the wave's epic when absent; wave authoring requires
// the epic to already exist.
func demoEnsureEpic(exec *demoExec, repoRoot, vaultPath string, wave demoWaveDef) error {
	if fileExists(filepath.Join(vaultPath, "work", "epics", wave.Epic+".md")) {
		return nil
	}
	_, err := exec.run(repoRoot, "new", "epic", wave.Epic, "--title", wave.EpicTitle, "--vault", vaultPath)
	return err
}

// demoAuthorWave renders a tusker.wave-authoring/v1 request and creates the
// wave through `wave create --file`. Creation is inert and idempotent on the
// stable request key, so reseed replays cleanly.
func demoAuthorWave(exec *demoExec, repoRoot, vaultPath string, wave demoWaveDef, withGate bool, actor string) (map[string]string, string, error) {
	if err := demoEnsureEpic(exec, repoRoot, vaultPath, wave); err != nil {
		return nil, "", err
	}
	path, err := demoRenderWaveAuthoring(repoRoot, wave, withGate)
	if err != nil {
		return nil, "", err
	}
	report, err := exec.run(repoRoot, "wave", "create", "--file", path, "--request-key", "demo-"+wave.Name, "--by", actor, "--vault", vaultPath)
	if err != nil {
		return nil, "", err
	}
	mapping := demoStringMap(demoDig(report, "wave", "taskMapping"))
	waveID := demoEnvelopeString(report, "wave", "waveId")
	if waveID == "" {
		return nil, "", tuskerError(demoCodePrecondition, "wave create did not report a wave for "+wave.Scope)
	}
	return mapping, waveID, nil
}

func demoTaskBody(wave demoWaveDef, task demoTaskDef) string {
	check := fmt.Sprintf("command: test \"$(cat %s)\" = \"%s\"", task.Artifact, task.Content)
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n## Outcome\n\n%s\n\n## Requirement\n\n%s\n", task.Title, task.Outcome, wave.Requirement)
	b.WriteString("\n## Context\n\n" + demoTaskContext(wave, task) + "\n")
	b.WriteString("\n## Acceptance\n\n| ID | Outcome | Proof |\n| --- | --- | --- |\n| A1 | " + task.Acceptance + " | " + check + " |\n")
	b.WriteString("\n## Verification\n\n| Covers | Check | Result |\n| --- | --- | --- |\n| A1 | " + check + " | pending |\n")
	b.WriteString("\n## Artifact\n\n- kind: diff_summary\n- path: " + task.Artifact + "\n- summary: " + task.Title + ".\n- acceptance_ids: A1\n")
	return b.String()
}

func demoRenderWaveAuthoring(repoRoot string, wave demoWaveDef, withGate bool) (string, error) {
	var tasks []map[string]any
	for _, task := range wave.Tasks {
		entry := map[string]any{
			"key": task.Key, "title": task.Title,
			"work_level":        demoWorkLevel(task.Complexity),
			"body":              demoTaskBody(wave, task),
			"epic":              wave.Epic,
			"owned_paths":       []string{task.Artifact},
			"generated_outputs": []string{task.Artifact},
		}
		var deps []map[string]any
		for _, dep := range task.Deps {
			deps = append(deps, map[string]any{"task": dep})
		}
		if len(deps) > 0 {
			entry["dependencies"] = deps
		}
		tasks = append(tasks, entry)
	}
	request := map[string]any{
		"schema": "tusker.wave-authoring/v1", "request_key": "demo-" + wave.Name,
		"title": wave.Title, "outcome": wave.Requirement,
		"spec_refs":   []string{".tusker/specs/demo-parallel-waves.md"},
		"concurrency": 2,
		"tasks":       tasks,
	}
	if withGate {
		request["human_actions"] = []map[string]any{{
			"key": "signoff", "task": "c4", "owner": "human:demo-approver",
			"action":           "Read sample/followup/report.txt and confirm the fixture bytes.",
			"verification":     "The approver confirms the combined report matches the fixture.",
			"why_agent_cannot": "Only the named human approver can sign off; the timer executor never acts as a human.",
		}}
	}
	raw, err := yaml.Marshal(request)
	if err != nil {
		return "", err
	}
	path := filepath.Join(demoDir(repoRoot), "wave-"+wave.Name+".yaml")
	if err := ensureDir(demoDir(repoRoot)); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// demoApplyCrossScopeDeps pins the follow-up entry task to the alpha and beta
// report tasks through canonical durable dependencies after their waves exist.
func demoApplyCrossScopeDeps(exec *demoExec, repoRoot, vaultPath string, mappings map[string]map[string]string, actor string) error {
	cross := demoCrossScopeDeps()
	if len(cross) == 0 {
		return nil
	}
	waveByKey := map[string]string{}
	for _, wave := range demoFixtureWaves() {
		for _, task := range wave.Tasks {
			waveByKey[task.Key] = wave.Name
		}
	}
	for key, deps := range cross {
		taskID := mappings[waveByKey[key]][key]
		if taskID == "" {
			return tuskerError(demoCodePrecondition, "wave create did not map task "+key)
		}
		var edges []string
		for _, dep := range deps {
			target := mappings[waveByKey[dep[1]]][dep[1]]
			if target == "" {
				return tuskerError(demoCodePrecondition, "wave create did not map cross-scope target "+dep[1])
			}
			edges = append(edges, target+":hard")
		}
		taskData, _, err := parseFrontmatterMustRead(filepath.Join(vaultPath, "work", "tasks", taskID+".md"))
		if err != nil {
			return err
		}
		if _, err := exec.run(repoRoot, "task", "update", taskID, "--if-revision", stringField(taskData, "state_rev"), "--dependencies", strings.Join(edges, ","), "--rebind-dependency-contracts", "--by", actor, "--vault", vaultPath); err != nil {
			return err
		}
	}
	return nil
}

func demoRegisterProject(repoRoot string, exec *demoExec, vaultPath string) (string, error) {
	listed, err := exec.run(repoRoot, "projects", "list", "--vault", vaultPath)
	if err != nil {
		return "", err
	}
	if id := demoFindProjectID(listed, repoRoot); id != "" {
		return id, nil
	}
	if _, err = exec.run(repoRoot, "projects", "add", "--repo", repoRoot, "--vault", vaultPath); err != nil {
		return "", err
	}
	listed, err = exec.run(repoRoot, "projects", "list", "--vault", vaultPath)
	if err != nil {
		return "", err
	}
	return demoFindProjectID(listed, repoRoot), nil
}

func demoCanonicalEqual(a, b string) bool {
	ca, errA := filepath.EvalSymlinks(a)
	cb, errB := filepath.EvalSymlinks(b)
	if errA != nil || errB != nil {
		return false
	}
	return ca == cb
}

func demoSeedSecondProject(repoRoot string, exec *demoExec) (string, error) {
	side := repoRoot + "-side"
	if err := os.MkdirAll(side, 0o755); err != nil {
		return "", err
	}
	if entries, err := os.ReadDir(side); err != nil || len(entries) > 1 {
		return "", tuskerError(errorInvalidArg, "second-project path is not empty: "+side)
	} else if err == nil && len(entries) == 1 && entries[0].Name() == ".git" {
		// Fresh git init; acceptable.
	}
	if _, err := demoGit(side, "init", "-b", "main"); err != nil {
		if _, err2 := demoGit(side, "rev-parse", "--verify", "main"); err2 != nil {
			return "", err
		}
	}
	sideVault := demoVaultPath(side)
	if _, err := exec.run(side, "init", "--vault", sideVault, "--yes", "--vault-only", "--no-mount"); err != nil {
		return "", err
	}
	if _, err := exec.run(side, "new", "epic", "--acronym", "SDP", "--title", "Side project probe", "--vault", sideVault); err != nil {
		return "", err
	}
	if _, err := demoGit(side, "add", "-A"); err != nil {
		return "", err
	}
	if _, err := demoGit(side, "commit", "-qm", "side baseline"); err != nil {
		return "", err
	}
	if _, err := exec.run(side, "projects", "add", "--repo", side, "--vault", sideVault); err != nil {
		return "", err
	}
	return side, nil
}

func demoSeedReport(repoRoot string, manifest *demoManifest, repeated bool) map[string]any {
	mapping := map[string]string{}
	for key, task := range manifest.Tasks {
		mapping[key] = task.TaskID
	}
	waves := map[string]string{}
	for name, wave := range manifest.Waves {
		waves[name] = wave.WaveID
	}
	return map[string]any{
		"scenario": manifest.Scenario, "scenario_version": manifest.ScenarioVersion,
		"repo": repoRoot, "vault": manifest.Vault, "repeated_seed": repeated,
		"waves": waves, "tasks": mapping, "profiles": manifest.Profiles,
		"human_gate": manifest.HumanGate, "second_project": manifest.SecondProject,
		"project_id":         manifest.LocalProjectID,
		"runtime_project_id": manifest.RuntimeProjectID, "runtime_scope": manifest.RuntimeScope,
		"runtime_registered": manifest.RuntimeRegistered, "runtime_problem": manifest.RuntimeProblem,
		"serve_hint":         demoServeHint(repoRoot),
		"created_paths":      manifest.CreatedPaths,
		"unmet_capabilities": demoUnmetCapabilities(),
		"next": []string{
			"tusker demo status --repo " + repoRoot + " --json",
			"tusker demo run --repo " + repoRoot + " --waves alpha,beta --json",
		},
		"text": demoSeedText(repoRoot, manifest, repeated),
	}
}

func demoUnmetCapabilities() []map[string]string {
	return []map[string]string{
		{"capability": "wave start", "requires": "resident daemon reconciling an automation-enabled project", "demo_behavior": "demo run records demo-level authorization per named wave; native waves stay disarmed"},
		{"capability": "review submit lane", "requires": "daemon completion reactor stamping work revision and source identity", "demo_behavior": "deterministic reviewer checks run inside demo run; close executes verification under reviewer authority"},
		{"capability": "runner route for demo-timer profiles", "requires": "operator-installed demo-timer harness adapter", "demo_behavior": "profiles are declared and labeled; no silent substitution to another harness"},
	}
}

func demoSeedText(repoRoot string, manifest *demoManifest, repeated bool) string {
	verb := "Seeded"
	if repeated {
		verb = "Seed already present (idempotent, nothing added)"
	}
	var names []string
	for name := range manifest.Waves {
		names = append(names, name)
	}
	sort.Strings(names)
	return fmt.Sprintf("%s demo %s v%d in %s: %d waves (%s), %d tasks. Roots are ready; run alpha,beta to start.",
		verb, manifest.Scenario, manifest.ScenarioVersion, repoRoot, len(manifest.Waves), strings.Join(names, ","), len(manifest.Tasks))
}

func demoStringMap(value any) map[string]string {
	out := map[string]string{}
	if table, ok := value.(map[string]any); ok {
		for key, item := range table {
			if s, ok := item.(string); ok {
				out[key] = s
			}
		}
	}
	return out
}

func demoDig(envelope map[string]any, keys ...string) any {
	var current any = envelope
	for _, key := range keys {
		table, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		current = table[key]
	}
	return current
}
