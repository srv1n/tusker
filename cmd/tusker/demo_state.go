package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Demo exit categories. Success is 0 and invalid requests stay on the
// existing convention (2); the remaining categories are demo-specific so
// scripts never have to scrape text.
const (
	demoExitOK           = 0
	demoExitInternal     = 1
	demoExitInvalid      = 2
	demoExitPrecondition = 3
	demoExitAssertion    = 4
	demoExitTimeout      = 5
)

const (
	demoCodeNotSeeded    = "DEMO_NOT_SEEDED"
	demoCodeSeedMismatch = "DEMO_SEED_MISMATCH"
	demoCodePrecondition = "DEMO_PRECONDITION_UNMET"
	demoCodeAssertion    = "DEMO_ASSERTION_FAILED"
	demoCodeTimeout      = "DEMO_TIMEOUT"
	demoCodeActiveRuns   = "DEMO_ACTIVE_RUNS"
)

func demoExitForError(err error) int {
	var typed *TuskerError
	if e, ok := err.(*TuskerError); ok {
		typed = e
	}
	if typed == nil {
		return demoExitInternal
	}
	switch typed.Code {
	case errorMissingArg, errorInvalidArg, errorInvalidField, errorAlreadyExists:
		return demoExitInvalid
	case errorNotFound, demoCodeNotSeeded, demoCodeSeedMismatch:
		return demoExitInvalid
	case demoCodePrecondition, demoCodeActiveRuns:
		return demoExitPrecondition
	case demoCodeAssertion:
		return demoExitAssertion
	case demoCodeTimeout:
		return demoExitTimeout
	default:
		return demoExitInternal
	}
}

// demoManifest is the ownership marker and run ledger. It lives inside the
// demo repo so the project stays disposable; every mutating demo command
// requires it before touching anything.
type demoManifest struct {
	Schema          string                    `json:"schema"`
	Scenario        string                    `json:"scenario"`
	ScenarioVersion int                       `json:"scenario_version"`
	RepoRoot        string                    `json:"repo_root"`
	Vault           string                    `json:"vault"`
	SeededAt        string                    `json:"seeded_at"`
	SeededBy        string                    `json:"seeded_by"`
	HumanGate       bool                      `json:"human_gate"`
	SecondProject   string                    `json:"second_project,omitempty"`
	Waves           map[string]demoWaveRecord `json:"waves"`
	Tasks           map[string]demoTaskRecord `json:"tasks"`
	Profiles        map[string]string         `json:"profiles"`
	Runs            []demoRunRecord           `json:"runs"`
	RunsAtReset     int                       `json:"runs_at_reset"`
	// CreatedPaths lists every static seed-owned repo-relative file (sample
	// project, knowledge corpus) so reset removes exactly what seed made.
	CreatedPaths []string `json:"created_paths,omitempty"`
	// LocalProjectID is the repo-scoped project registration; Runtime* is
	// the default-scope registration the normal UI reads (best-effort).
	LocalProjectID    string `json:"local_project_id,omitempty"`
	RuntimeProjectID  string `json:"runtime_project_id,omitempty"`
	RuntimeScope      string `json:"runtime_scope,omitempty"`
	RuntimeStripScope bool   `json:"runtime_strip_scope,omitempty"`
	RuntimeRegistered bool   `json:"runtime_registered,omitempty"`
	RuntimeProblem    string `json:"runtime_problem,omitempty"`
}

type demoWaveRecord struct {
	Name    string   `json:"name"`
	WaveID  string   `json:"wave_id"`
	Title   string   `json:"title"`
	Scope   string   `json:"scope"`
	Epic    string   `json:"epic"`
	Members []string `json:"members"`
}

type demoTaskRecord struct {
	SourceKey string   `json:"source_key"`
	TaskID    string   `json:"task_id"`
	Wave      string   `json:"wave"`
	Title     string   `json:"title"`
	Artifact  string   `json:"artifact"`
	Content   string   `json:"content"`
	DelaySecs float64  `json:"delay_secs"`
	FastSecs  float64  `json:"fast_secs"`
	Deps      []string `json:"deps"`
	// Complexity selects the model work level through the ordinary mapping
	// (routine=light, standard, complex/frontier=demanding).
	Complexity string `json:"complexity,omitempty"`
}

// demoWorkLevel maps fixture complexity to the supported work-level label.
func demoWorkLevel(complexity string) string {
	switch strings.ToLower(strings.TrimSpace(complexity)) {
	case "routine":
		return "light"
	case "complex", "frontier":
		return "demanding"
	default:
		return "standard"
	}
}

type demoRunRecord struct {
	RunID      string                    `json:"run_id"`
	StartedAt  string                    `json:"started_at"`
	FinishedAt string                    `json:"finished_at"`
	Waves      []string                  `json:"waves"`
	Executor   string                    `json:"executor"`
	Fast       bool                      `json:"fast"`
	Results    map[string]demoWaveResult `json:"results"`
	Intervals  []demoInterval            `json:"intervals"`
	Notes      []string                  `json:"notes"`
	// TaskProfiles is only populated by real-harness runs: the effective
	// profile/harness/model/effort/transport/attempt per task, resolved
	// from the ordinary route preview and runtime inspection.
	TaskProfiles map[string]demoRealTaskProfile `json:"task_profiles,omitempty"`
}

type demoWaveResult struct {
	Wave        string   `json:"wave"`
	Authorized  bool     `json:"authorized"`
	Completed   []string `json:"completed"`
	Failed      []string `json:"failed"`
	Blocked     []string `json:"blocked"`
	Interrupted []string `json:"interrupted,omitempty"`
	Error       string   `json:"error,omitempty"`
}

type demoInterval struct {
	TaskID   string `json:"task_id"`
	Wave     string `json:"wave"`
	Attempt  string `json:"attempt"`
	Started  string `json:"started_at"`
	Finished string `json:"finished_at"`
	Outcome  string `json:"outcome"`
}

func demoDir(repoRoot string) string {
	return filepath.Join(repoRoot, ".tusker", "demo")
}

func demoManifestPath(repoRoot string) string {
	return filepath.Join(demoDir(repoRoot), "manifest.json")
}

func demoLoadManifest(repoRoot string) (*demoManifest, error) {
	raw, err := os.ReadFile(demoManifestPath(repoRoot))
	if err != nil {
		return nil, tuskerError(demoCodeNotSeeded, "not a seeded demo repo: "+repoRoot, withHint("run `tusker demo seed --repo "+repoRoot+"` first"))
	}
	var manifest demoManifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return nil, tuskerError(demoCodeNotSeeded, "demo manifest is unreadable: "+err.Error())
	}
	if manifest.Schema != demoManifestSchema || manifest.Scenario != demoScenario {
		return nil, tuskerError(demoCodeSeedMismatch, "demo manifest is for another scenario; reset and reseed")
	}
	return &manifest, nil
}

func demoSaveManifest(repoRoot string, manifest *demoManifest) error {
	if err := ensureDir(demoDir(repoRoot)); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(demoManifestPath(repoRoot), append(raw, '\n'), 0o644)
}

// demoResolveRepo canonicalizes --repo (default cwd) and enforces the demo
// ownership boundary: an existing manifest must name the same canonical root,
// and symlinked or outside paths are refused.
func demoResolveRepo(args Args, needManifest bool) (string, *demoManifest, error) {
	raw := strings.TrimSpace(args.String("repo"))
	if raw == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return "", nil, err
		}
		raw = cwd
	}
	abs, err := filepath.Abs(raw)
	if err != nil {
		return "", nil, err
	}
	canonical, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", nil, tuskerError(errorInvalidArg, "demo repo does not exist: "+raw)
	}
	if !dirExists(filepath.Join(canonical, ".tusker")) {
		return "", nil, tuskerError(demoCodeNotSeeded, "no .tusker vault in "+canonical, withHint("seed creates the demo repo; it never adopts an unrelated project"))
	}
	manifest, err := demoLoadManifest(canonical)
	if needManifest {
		if err != nil {
			return "", nil, err
		}
		if manifest.RepoRoot != canonical {
			return "", nil, tuskerError(demoCodeSeedMismatch, "demo manifest names a different repo root", withContext(map[string]any{"manifest_repo": manifest.RepoRoot, "requested_repo": canonical}))
		}
		return canonical, manifest, nil
	}
	if err == nil && manifest.RepoRoot != canonical {
		return "", nil, tuskerError(demoCodeSeedMismatch, "demo manifest names a different repo root")
	}
	return canonical, manifest, nil
}

func demoVaultPath(repoRoot string) string {
	return filepath.Join(repoRoot, ".tusker")
}

// demoEnsureStateRoot keeps demo runtime state inside the disposable repo
// unless the operator explicitly overrode TUSKER_STATE_ROOT.
func demoEnsureStateRoot(repoRoot string) string {
	if strings.TrimSpace(os.Getenv("TUSKER_STATE_ROOT")) != "" {
		return os.Getenv("TUSKER_STATE_ROOT")
	}
	root := filepath.Join(repoRoot, ".tusker", "runtime-state")
	os.Setenv("TUSKER_STATE_ROOT", root)
	return root
}

func demoSortedTaskKeys(manifest *demoManifest) []string {
	keys := make([]string, 0, len(manifest.Tasks))
	for key := range manifest.Tasks {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func demoEmit(args Args, payload map[string]any) error {
	if args.Bool("json") {
		emitJSON(payload)
		return nil
	}
	if text, ok := payload["text"].(string); ok && text != "" {
		fmt.Println(text)
		return nil
	}
	raw, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(raw))
	return nil
}

func demoFail(args Args, code int, err error) (int, error) {
	issue := errorToIssue(err)
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": false, "error": issue})
		return code, nil
	}
	loc := ""
	if issue.Path != "" {
		loc = issue.Path + ": "
	}
	fmt.Fprintf(os.Stderr, "[%s] %s%s\n", issue.Code, loc, issue.Message)
	if issue.Hint != "" {
		fmt.Fprintf(os.Stderr, "  hint: %s\n", issue.Hint)
	}
	return code, nil
}
