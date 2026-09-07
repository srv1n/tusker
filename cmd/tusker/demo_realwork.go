package main

// Real-work test repository support: seeded sample project, fictional
// knowledge corpus, default-scope runtime registration, and the real-harness
// run lane.
//
// The offline timer lane (demo-timer executor) stays the cheap deterministic
// default. The real-harness lane (--mode real) never executes tasks itself:
// it resolves effective profiles through `runner route`, requires the named
// harness in `runner catalog` with no silent substitution, moves tasks ready
// through the ordinary CLI, authorizes waves, then waits for the configured
// runtime (resident daemon or operator-driven agent) to do the real file,
// test, submit, review and close work. Attempt IDs, profiles, harness, model
// and transport are recorded from runtime inspection, never invented.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/oklog/ulid/v2"
)

func demoNewRunID() string {
	return "R-" + strings.ToLower(ulid.Make().String())
}

const (
	demoRunModeOffline = "offline"
	demoRunModeReal    = "real"

	demoRealDefaultTimeout = "15m"
	demoRealPollEvery      = 2 * time.Second
)

var demoServeBaseURL = "http://" + defaultServeAddr

// ---------------------------------------------------------------------------
// seeded static files
// ---------------------------------------------------------------------------

// demoRealWorkFiles returns every static file the real-work seed owns, as
// repo-relative path to exact bytes. Task/wave/epic/decision documents are
// tracked through the manifest instead; everything here is rewritten by both
// seed and reseed and removed by reset, so the lists must stay complete.
func demoRealWorkFiles() map[string]string {
	files := map[string]string{
		"sample/README.md":              demoSampleReadme(),
		"sample/tools/wait_progress.py": demoProgressHelper(),
	}
	for path, body := range demoKnowledgeDocs() {
		files[path] = body
	}
	return files
}

// demoSeedRealWorkFiles writes the static real-work files into the repo and
// returns the sorted repo-relative paths created.
func demoSeedRealWorkFiles(repoRoot string) ([]string, error) {
	files := demoRealWorkFiles()
	paths := make([]string, 0, len(files))
	for rel := range files {
		paths = append(paths, rel)
	}
	sort.Strings(paths)
	for _, rel := range paths {
		full := filepath.Join(repoRoot, filepath.FromSlash(rel))
		if err := ensureDir(filepath.Dir(full)); err != nil {
			return nil, err
		}
		if err := os.WriteFile(full, []byte(files[rel]), 0o644); err != nil {
			return nil, err
		}
	}
	if err := os.Chmod(filepath.Join(repoRoot, "sample", "tools", "wait_progress.py"), 0o755); err != nil {
		return nil, err
	}
	return paths, nil
}

func demoSampleReadme() string {
	bt := "`"
	return "# Real-work test project (disposable)\n" +
		"\n" +
		"This directory is seeded by " + bt + "tusker demo seed" + bt + " for CLI-driven testing of real coding-agent work. All content is fictional.\n" +
		"\n" +
		"## Layout\n" +
		"\n" +
		"- " + bt + "sample/standalone/" + bt + " — smoke task: tiny module plus test, owned by task " + bt + "s1" + bt + ".\n" +
		"- " + bt + "sample/alpha/" + bt + ", " + bt + "sample/beta/" + bt + " — parallel waves; roots generate fixtures, branches add a transformation and a validator with tests, joins combine both outputs.\n" +
		"- " + bt + "sample/followup/" + bt + " — follow-up wave; assembles the combined report after both waves finish.\n" +
		"- " + bt + "sample/tools/wait_progress.py" + bt + " — bounded progress helper jobs invoke as a tool.\n" +
		"\n" +
		"## Run commands\n" +
		"\n" +
		"Offline (deterministic timer, no provider needed):\n" +
		"\n" +
		"    tusker demo seed --repo <empty-dir> --scenario parallel-waves --json\n" +
		"    tusker demo run --repo <dir> --waves standalone --fast --json\n" +
		"    tusker demo run --repo <dir> --waves alpha,beta --fast --json\n" +
		"    tusker demo run --repo <dir> --waves follow-up --fast --json\n" +
		"    tusker demo check --repo <dir> --json\n" +
		"    tusker demo reset --repo <dir> --yes --json\n" +
		"\n" +
		"Real harness (configured profiles, resident runtime does the work):\n" +
		"\n" +
		"    scripts/test-real-work-project.sh --mode real --repo <dir> --require-harness codex_exec --profile <name>\n" +
		"\n" +
		"Fictional product knowledge lives in " + bt + ".tusker/specs/fixture-cafe/" + bt + " and validates with:\n" +
		"\n" +
		"    tusker docs check --vault <dir>/.tusker --json\n"
}

// ---------------------------------------------------------------------------
// fictional knowledge corpus
// ---------------------------------------------------------------------------

// demoKnowledgeDocs returns the fictional product knowledge seeded into the
// disposable repo. Every document parents into one closed graph rooted at
// the demo-parallel-waves governing spec, uses only [[subject]] backlinks
// that resolve inside the corpus, and keeps invalid metadata and broken
// links out of the default seed (those live only in explicit negative-test
// variants). The result must pass `tusker docs check` in the seeded repo.
func demoKnowledgeDocs() map[string]string {
	return map[string]string{
		".tusker/specs/fixture-cafe/overview.md":             demoDocOverview(),
		".tusker/specs/fixture-cafe/catalog/index.md":        demoDocCatalogIndex(),
		".tusker/specs/fixture-cafe/catalog/blend-intent.md": demoDocBlendIntent(),
		".tusker/specs/fixture-cafe/catalog/legacy-blend.md": demoDocLegacyBlend(),
		".tusker/specs/fixture-cafe/ops/index.md":            demoDocOpsIndex(),
		".tusker/specs/fixture-cafe/ops/brewing.md":          demoDocBrewing(),
		".tusker/specs/fixture-cafe/ops/grind-decision.md":   demoDocGrindDecision(),
	}
}

func demoDocOverview() string {
	return `---
subject: fixture-cafe-overview
title: Fixture Cafe sample product overview
parent: demo-parallel-waves
part_of: demo-parallel-waves
status: canonical
read_when: "Starting any real-work test task; this explains what the sample product is."
skip_when: "Checking exact task bytes; see the task contract instead."
---

# Fixture Cafe sample product overview

Fixture Cafe is a fictional two-person coffee roastery used only to give
test tasks something concrete to reference. It has no customers, no revenue,
and no production systems; every name, recipe, and number here is invented
for CLI-driven testing.

The sample product has two halves. The [[fixture-cafe-catalog]] describes
what the roastery sells: a small rotating menu of blends with tasting notes
and brewing guidance. The [[fixture-cafe-ops]] describes how the roastery
works: batch scheduling, grind policy, and the long brewing reference in
[[fixture-cafe-brewing]].

Test tasks mirror this split on purpose. Alpha-wave work tends to touch
catalog-shaped fixtures while beta-wave work tends to touch ops-shaped
fixtures, and the follow-up wave assembles a combined report the way this
overview assembles the two halves. Product intent lives in
[[fixture-cafe-blend-intent]]; the grind policy decision lives in
[[fixture-cafe-grind-decision]].
`
}

func demoDocCatalogIndex() string {
	return `---
subject: fixture-cafe-catalog
title: Catalog index
parent: fixture-cafe-overview
part_of: fixture-cafe-overview
status: canonical
read_when: "Working on catalog-shaped test fixtures."
skip_when: "Working on roastery operations; see the ops index instead."
---

# Catalog index

Concise introduction to the fictional sales catalog. The current product
intent is [[fixture-cafe-blend-intent]]: three small blends, seasonal
rotation, tasting notes written before the beans arrive. The superseded
holiday blend note [[fixture-cafe-legacy-blend]] remains readable but must
not be treated as current; its successor is the blend-intent note.
`
}

func demoDocBlendIntent() string {
	return `---
subject: fixture-cafe-blend-intent
title: Blend product intent
parent: fixture-cafe-catalog
part_of: fixture-cafe-catalog
status: canonical
read_when: "Deciding what a catalog fixture should contain."
skip_when: "Tuning brew parameters; see the brewing reference instead."
---

# Blend product intent

We sell three fictional blends — Harbor Morning, Night Tram, and Orchard
Steps — in 250g bags, rotating one slot per season. Each blend page states
origin, process, and two tasting notes, plus a brew starting point drawn
from [[fixture-cafe-brewing]]. Grind choice follows
[[fixture-cafe-grind-decision]]: conical burr as the house default with a
documented flat-burr alternative for espresso-forward seasons.
`
}

func demoDocLegacyBlend() string {
	return `---
subject: fixture-cafe-legacy-blend
title: Winter 2024 holiday blend (retired)
parent: fixture-cafe-catalog
part_of: fixture-cafe-catalog
status: superseded
superseded_by: fixture-cafe-blend-intent
read_when: "Tracing why the holiday blend disappeared from the catalog."
skip_when: "Building anything current; use the blend intent instead."
---

# Winter 2024 holiday blend (retired)

This note is superseded by [[fixture-cafe-blend-intent]]. The 2024 holiday
blend (fictional: cocoa, clove, orange peel) sold through in January 2025
and was not renewed; the seasonal slot now follows the standing blend
intent. Kept so the catalog history stays traceable.
`
}

func demoDocOpsIndex() string {
	return `---
subject: fixture-cafe-ops
title: Operations index
parent: fixture-cafe-overview
part_of: fixture-cafe-overview
status: canonical
read_when: "Working on ops-shaped test fixtures."
skip_when: "Working on the sales catalog; see the catalog index instead."
---

# Operations index

Concise introduction to the fictional roastery operations. Batch scheduling
runs Tuesday and Friday mornings; roast curves are logged per batch. Grind
policy is decided in [[fixture-cafe-grind-decision]]. The long brewing
reference [[fixture-cafe-brewing]] holds temperatures, ratios, and
troubleshooting; product intent stays in [[fixture-cafe-blend-intent]].
`
}

func demoDocBrewing() string {
	return `---
subject: fixture-cafe-brewing
title: Brewing reference covering every fictional method we serve at the counter including pour-over immersion espresso and cold brew with temperatures ratios grind starting points and troubleshooting for each
parent: fixture-cafe-ops
part_of: fixture-cafe-ops
status: canonical
read_when: "Needing any brew parameter for a fixture or test."
skip_when: "Deciding what to sell; see the blend intent instead."
---

# Brewing reference

Long technical explanation of the fictional brew program. All parameters
are invented starting points for test fixtures, not café advice.

## Pour-over (fictional house method)

Ratio 1:16, water 94C, medium grind per [[fixture-cafe-grind-decision]].
Bloom 45s with twice the coffee weight in water, then two slow pours. Total
drawdown target 2:45-3:15. If the bed stalls, coarsen one step; if it races
past 2:30, tighten one step. Log every change; fixtures in the alpha wave
mirror this log shape.

## Immersion

Ratio 1:14, water 96C, coarse grind. Steep 4 minutes, break the crust at
2:00, plunge slowly. Forgiving method, good default when the grinder drifts.

## Espresso (fictional single-origin program)

Ratio 1:2 in 27-30s at 93C. Flat burr per the grind decision's alternative
path. Dial in with dose fixed, grind variable; fixtures record dose, yield,
and time as three separate fields so validators can check each.

## Cold brew

Ratio 1:10, room temperature, 16 hours, extra-coarse grind. Dilute 1:1 to
serve. The most stable recipe we pretend to have; use it as the happy-path
example in validator tests.

## Water (fictional house profile)

120ppm total dissolved solids, 40ppm alkalinity. Re-mineralized from
distilled. Beta-wave fixtures encode this profile as constants; the join
tasks assert both branches agree on it before assembling the report.

## Troubleshooting matrix

Sour and fast: tighten grind or raise temperature. Bitter and dry: coarsen
or shorten contact. Hollow middle: check water freshness, then distribution.
Astringent immersion: plunge earlier. Every symptom maps to exactly one
first action so test validators stay deterministic.

## Cross-links

Product intent: [[fixture-cafe-blend-intent]]. Grind policy:
[[fixture-cafe-grind-decision]]. Catalog home: [[fixture-cafe-catalog]].
Ops home: [[fixture-cafe-ops]]. Product overview:
[[fixture-cafe-overview]].
`
}

func demoDocGrindDecision() string {
	return `---
subject: fixture-cafe-grind-decision
title: House grind policy decision
parent: fixture-cafe-ops
part_of: fixture-cafe-ops
status: canonical
read_when: "Choosing grinder settings for a fixture or reviewing the policy."
skip_when: "Looking for brew ratios; see the brewing reference instead."
---

# House grind policy decision

Decision record for the fictional grinder setup. Alternatives considered,
with rationale.

## Alternatives

1. Conical burr as the single house grinder. Forgiving across methods,
   cheaper fictional burrs, slower for espresso rushes.
2. Flat burr as the single house grinder. Better fictional espresso
   clarity, harsher filter cups at our invented roast level.
3. Both, split by bar: conical for filter, flat for espresso. Best cups,
   highest fictional cost and counter space.

## Rationale

We chose alternative 1 with a documented exception: espresso-forward
seasons may borrow the flat-burr path described in
[[fixture-cafe-brewing]]. Single-grinder simplicity won because this shop
is fictional and its staff count is two. Revisit if a third fictional
hire appears.

## Consequences

Catalog pages state grind starting points per the brewing reference;
fixtures encode them as constants. Blend intent
[[fixture-cafe-blend-intent]] stays valid under either path.
`
}

func demoProgressHelper() string {
	return `#!/usr/bin/env python3
"""Bounded timestamped progress waiter for real-work test jobs.

Jobs invoke this as a tool so progress is visible while real file/test work
happens; the helper never does the task work itself. Emits one timestamped
line roughly every --interval seconds for --duration seconds (defaults 60/5
so a human can watch running and review states), handles SIGINT/SIGTERM by
printing an interruption line, and exits deterministically:

  0   full wait completed
  130 interrupted by SIGINT
  143 interrupted by SIGTERM
  2   invalid arguments (argparse)

Output is bounded: at most 720 progress lines no matter the flags.
Standard library only.
"""

import argparse
import datetime
import signal
import sys
import time

MAX_LINES = 720

_interrupted_by = None


def _handle(signum, _frame):
    global _interrupted_by
    if _interrupted_by is None:
        _interrupted_by = signum


def _stamp():
    return datetime.datetime.now().astimezone().strftime("%H:%M:%S")


def parse_args(argv):
    parser = argparse.ArgumentParser(description="Bounded progress waiter.")
    parser.add_argument("--duration", type=float, default=60.0,
                        help="total wait in seconds (default 60)")
    parser.add_argument("--interval", type=float, default=5.0,
                        help="seconds between progress lines (default 5)")
    parser.add_argument("--short", action="store_true",
                        help="cheap-test mode: ~5s total, ~1s interval")
    parser.add_argument("--label", default="real-work",
                        help="label prefix for each line")
    args = parser.parse_args(argv)
    if args.short:
        args.duration, args.interval = 5.0, 1.0
    if not (args.duration > 0):
        parser.error("--duration must be positive")
    if not (args.interval > 0):
        parser.error("--interval must be positive")
    return args


def main(argv=None):
    args = parse_args(argv)
    signal.signal(signal.SIGINT, _handle)
    signal.signal(signal.SIGTERM, _handle)
    start = time.monotonic()
    step = 0
    total = max(1, min(MAX_LINES, int(args.duration / args.interval) + 1))
    while step < total:
        elapsed = time.monotonic() - start
        if _interrupted_by is not None:
            print("%s %s interrupted after %.1fs (step %d/%d)" % (
                _stamp(), args.label, elapsed, step, total), flush=True)
            return 128 + _interrupted_by
        if elapsed >= args.duration:
            break
        print("%s %s step %d/%d elapsed=%.1fs" % (
            _stamp(), args.label, step + 1, total, elapsed), flush=True)
        step += 1
        deadline = start + min(args.duration, float(step) * args.interval)
        while True:
            if _interrupted_by is not None:
                break
            remaining = deadline - time.monotonic()
            if remaining <= 0:
                break
            time.sleep(min(0.1, remaining))
    elapsed = time.monotonic() - start
    if _interrupted_by is not None:
        print("%s %s interrupted after %.1fs (step %d/%d)" % (
            _stamp(), args.label, elapsed, step, total), flush=True)
        return 128 + _interrupted_by
    print("%s %s done in %.1fs (%d steps)" % (
        _stamp(), args.label, elapsed, step), flush=True)
    return 0


if __name__ == "__main__":
    sys.exit(main())
`
}

// ---------------------------------------------------------------------------
// runtime registration (default scope, for normal UI discoverability)
// ---------------------------------------------------------------------------

// demoRuntimeScope resolves the state-root scope the normal UI/runtime
// instance reads: the caller's own TUSKER_STATE_ROOT override when set,
// otherwise the true default store with the in-process demo override
// removed. Seed captures the pre-ensure value so tests (which override the
// variable) register inside their sandbox instead of the operator's home.
func demoRuntimeScope(preEnsureOverride, repoRoot string) (scope string, stripForDefault bool) {
	if strings.TrimSpace(preEnsureOverride) != "" {
		return preEnsureOverride, false
	}
	return "default", true
}

// demoExecForScope returns an executor whose environment resolves
// DefaultStateRoot to the requested scope.
func demoExecForScope(base *demoExec, scope string, strip bool) *demoExec {
	out := &demoExec{Bin: base.Bin}
	if !strip {
		out.Env = base.Env
		return out
	}
	for _, entry := range base.Env {
		if strings.HasPrefix(entry, "TUSKER_STATE_ROOT=") {
			continue
		}
		out.Env = append(out.Env, entry)
	}
	return out
}

// demoRegisterRuntimeProject registers the disposable repo in the runtime
// scope the normal UI reads and returns the project ID. Registration is
// best-effort by design: when it is unavailable, seed still prepares data
// and reports the missing step instead of failing.
func demoRegisterRuntimeProject(repoRoot string, exec *demoExec, vaultPath, scope string, strip bool) (string, error) {
	scoped := demoExecForScope(exec, scope, strip)
	if _, err := scoped.run(repoRoot, "projects", "add", "--repo", repoRoot, "--vault", vaultPath); err != nil {
		return "", err
	}
	listed, err := scoped.run(repoRoot, "projects", "list", "--vault", vaultPath)
	if err != nil {
		return "", err
	}
	id := demoFindProjectID(listed, repoRoot)
	if id == "" {
		return "", fmt.Errorf("projects list did not return the registered repo")
	}
	return id, nil
}

// demoRemoveRuntimeProject removes a manifest-owned default-scope project
// registration. An already-absent ID is success (never an error): reset must
// stay retryable and must not fail when scopes differ between seed and reset.
func demoRemoveRuntimeProject(repoRoot string, exec *demoExec, vaultPath, scope string, strip bool, projectID string) error {
	scoped := demoExecForScope(exec, scope, strip)
	listed, err := scoped.run(repoRoot, "projects", "list", "--vault", vaultPath)
	if err != nil {
		return err
	}
	if demoFindProjectID(listed, repoRoot) == "" {
		present := false
		if projects, ok := listed["projects"].([]any); ok {
			for _, entry := range projects {
				if record, ok := entry.(map[string]any); ok {
					if demoStringField(record, "project_id", "id") == projectID {
						present = true
					}
				}
			}
		}
		if !present {
			return nil
		}
	}
	_, err = scoped.run(repoRoot, "projects", "remove", projectID, "--vault", vaultPath)
	return err
}

func demoFindProjectID(listed map[string]any, repoRoot string) string {
	projects, _ := listed["projects"].([]any)
	for _, entry := range projects {
		record, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		if root, _ := record["repo_root"].(string); demoCanonicalEqual(root, repoRoot) {
			if id := demoStringField(record, "project_id", "id"); id != "" {
				return id
			}
		}
	}
	return ""
}

func demoStringField(record map[string]any, keys ...string) string {
	for _, key := range keys {
		if s, _ := record[key].(string); strings.TrimSpace(s) != "" {
			return strings.TrimSpace(s)
		}
	}
	return ""
}

func demoServeHint(repoRoot string) string {
	return "Open the same project in the normal UI with: tusker serve --vault " + filepath.Join(repoRoot, ".tusker") + " --by human:<name> (view URL appears in the serve output; the driver never starts a server on an occupied port itself)"
}

// ---------------------------------------------------------------------------
// real-harness run lane
// ---------------------------------------------------------------------------

// demoRealTaskProfile records what actually resolved and ran for one task:
// configured profile, harness, model, effort, transport when known, and the
// runtime attempt ID. Nothing here is defaulted or substituted.
type demoRealTaskProfile struct {
	TaskKey   string `json:"task_key"`
	TaskID    string `json:"task_id"`
	Profile   string `json:"profile"`
	Harness   string `json:"harness"`
	Model     string `json:"model"`
	Effort    string `json:"effort"`
	Transport string `json:"transport"`
	Attempt   string `json:"attempt,omitempty"`
}

type demoCatalogHarness struct {
	Name      string
	Available bool
	State     string
	Version   string
	Problem   string
}

func demoLoadCatalog(exec *demoExec, repoRoot, vaultPath string) ([]demoCatalogHarness, error) {
	envelope, err := exec.run(repoRoot, "runner", "catalog", "--vault", vaultPath)
	if err != nil {
		return nil, err
	}
	var out []demoCatalogHarness
	harnesses, _ := envelope["harnesses"].([]any)
	for _, entry := range harnesses {
		record, _ := entry.(map[string]any)
		if record == nil {
			continue
		}
		available, _ := record["available"].(bool)
		out = append(out, demoCatalogHarness{
			Name:      demoStringField(record, "harness", "id", "name"),
			Available: available,
			State:     demoStringField(record, "state"),
			Version:   demoStringField(record, "version"),
			Problem:   demoStringField(record, "error"),
		})
	}
	return out, nil
}

// demoResolveTaskRoute returns the effective execute-lane routing for one
// task through the same route preview the UI explains with.
func demoResolveTaskRoute(exec *demoExec, repoRoot, vaultPath, taskID string) (demoRealTaskProfile, []string, error) {
	envelope, err := exec.run(repoRoot, "runner", "route", taskID, "--lane", "execute", "--vault", vaultPath)
	if err != nil {
		return demoRealTaskProfile{}, nil, err
	}
	profile := demoRealTaskProfile{
		TaskID:    taskID,
		Profile:   demoStringField(envelope, "profile"),
		Harness:   demoStringField(envelope, "harness"),
		Model:     demoStringField(envelope, "model"),
		Effort:    demoStringField(envelope, "effort"),
		Transport: "unknown",
	}
	var blockers []string
	if raw, ok := envelope["blockers"].([]any); ok {
		for _, item := range raw {
			if s, _ := item.(string); strings.TrimSpace(s) != "" {
				blockers = append(blockers, strings.TrimSpace(s))
			}
		}
	}
	return profile, blockers, nil
}

// demoCheckRealPreconditions resolves every selected task through the
// ordinary route preview and requires the named harness to be available in
// the runner catalog. Any mismatch is a precondition refusal: the driver
// never substitutes another harness, profile, or the timer lane.
func demoCheckRealPreconditions(exec *demoExec, repoRoot, vaultPath string, manifest *demoManifest, keys []string, requireHarness, requireProfile string) (map[string]demoRealTaskProfile, error) {
	profiles := map[string]demoRealTaskProfile{}
	for _, key := range keys {
		rec := manifest.Tasks[key]
		profile, blockers, err := demoResolveTaskRoute(exec, repoRoot, vaultPath, rec.TaskID)
		if err != nil {
			return nil, tuskerError(demoCodePrecondition, fmt.Sprintf("task %s (%s): route resolution failed: %s", key, rec.TaskID, err.Error()))
		}
		if len(blockers) > 0 {
			return nil, tuskerError(demoCodePrecondition, fmt.Sprintf("task %s (%s): no effective execute profile: %s", key, rec.TaskID, strings.Join(blockers, "; ")),
				withHint("configure automation profiles/routing for this project; the demo never substitutes another harness"))
		}
		profile.TaskKey = key
		if requireProfile != "" && profile.Profile != requireProfile {
			return nil, tuskerError(demoCodePrecondition, fmt.Sprintf("task %s (%s): effective profile %q does not match required %q", key, rec.TaskID, profile.Profile, requireProfile),
				withHint("adjust the project routing or pass the effective profile; refusing to substitute"))
		}
		if requireHarness != "" && profile.Harness != requireHarness {
			return nil, tuskerError(demoCodePrecondition, fmt.Sprintf("task %s (%s): effective harness %q does not match required %q", key, rec.TaskID, profile.Harness, requireHarness),
				withHint("configure routing to the required harness; refusing to substitute"))
		}
		profiles[key] = profile
	}
	catalog, err := demoLoadCatalog(exec, repoRoot, vaultPath)
	if err != nil {
		return nil, tuskerError(demoCodePrecondition, "runner catalog unreadable: "+err.Error())
	}
	byName := map[string]demoCatalogHarness{}
	for _, entry := range catalog {
		byName[entry.Name] = entry
	}
	needed := map[string]bool{}
	for _, profile := range profiles {
		if profile.Harness != "" {
			needed[profile.Harness] = true
		}
	}
	for name := range needed {
		entry, ok := byName[name]
		if !ok {
			return nil, tuskerError(demoCodePrecondition, "required runner harness is unavailable: "+name,
				withHint("install the harness or rerun without --require-harness; the demo never substitutes another harness silently"))
		}
		if !entry.Available {
			reason := firstNonEmpty(entry.Problem, "harness reported state "+firstNonEmpty(entry.State, "unknown"))
			return nil, tuskerError(demoCodePrecondition, fmt.Sprintf("required runner harness %q is not available: %s", name, reason),
				withHint("authenticate/install the harness (see `tusker runner catalog --json`), then rerun; unavailable capacity is a prerequisite, not a pass"))
		}
	}
	return profiles, nil
}

// demoRunReal drives selected waves through the ordinary lifecycle without
// touching task execution itself: readiness transitions use the supported
// status commands, authorization is recorded at the demo level, and the
// configured runtime performs claim, progress, submit, review and close.
// The driver polls selected tasks to terminal state within a bounded timeout
// and records runtime attempt evidence for every task.
func demoRunReal(ctx context.Context, args Args, repoRoot string, manifest *demoManifest, exec *demoExec, vaultPath string, waves []string, profiles map[string]demoRealTaskProfile) (map[string]any, int, error) {
	timeout, err := demoParseDuration(firstNonEmpty(strings.TrimSpace(args.String("timeout")), demoRealDefaultTimeout))
	if err != nil {
		return nil, 0, err
	}
	selected := demoSelectedKeys(manifest, waves)
	record := demoRunRecord{
		RunID:        demoNewRunID(),
		StartedAt:    time.Now().UTC().Format(time.RFC3339),
		Waves:        waves,
		Executor:     "real-harness",
		Results:      map[string]demoWaveResult{},
		TaskProfiles: map[string]demoRealTaskProfile{},
	}
	for key, profile := range profiles {
		record.TaskProfiles[key] = profile
	}
	for _, name := range waves {
		record.Results[name] = demoWaveResult{Wave: name}
	}
	// Ordinary readiness transitions first: without a resident daemon nothing
	// recomputes next_owner until reconcile runs.
	if _, err := exec.run(repoRoot, "reconcile", "--vault", vaultPath); err != nil {
		return nil, 0, err
	}
	for _, key := range selected {
		rec := manifest.Tasks[key]
		status := demoLiveTaskStatus(vaultPath, rec.TaskID)
		if status == "backlog" || status == "rework" {
			if _, err := exec.run(repoRoot, "status", rec.TaskID, "ready", "--reason", "real-harness run: ready for configured execution", "--vault", vaultPath); err != nil {
				if gateID := demoOpenGateID(vaultPath, rec.TaskID); gateID != "" {
					recordOutcomeInto(&record, manifest, key, "blocked", "", "", "")
					record.Notes = append(record.Notes, fmt.Sprintf("task %s (%s) blocked on open gate %s: only the owning human can release it", key, rec.TaskID, gateID))
					continue
				}
				return nil, 0, tuskerError(demoCodePrecondition, fmt.Sprintf("task %s (%s) could not move to ready: %s", key, rec.TaskID, err.Error()))
			}
		}
	}
	for _, name := range waves {
		wave, ok := manifest.Waves[name]
		if !ok || strings.TrimSpace(wave.WaveID) == "" {
			return nil, 0, tuskerError(demoCodePrecondition, "real-harness run has no registered wave for "+name)
		}
		receipt, err := demoExecuteWave(ctx, manifest.RuntimeProjectID, wave.WaveID)
		if err != nil {
			return nil, 0, err
		}
		result := record.Results[name]
		result.Authorized = true
		record.Results[name] = result
		record.Notes = append(record.Notes, fmt.Sprintf("wave %s authorized and queued by supported Execute Wave action (%d new, %d already queued); execution uses configured profiles through the resident runtime, never the timer lane", name, len(receipt.QueuedTaskIDs), len(receipt.AlreadyQueuedTaskIDs)))
	}
	manifest.Runs = append(manifest.Runs, record)
	runIndex := len(manifest.Runs) - 1
	if err := demoSaveManifest(repoRoot, manifest); err != nil {
		return nil, 0, err
	}
	instructions := demoRealInstructions(manifest, selected, profiles)
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(demoRealPollEvery)
	defer ticker.Stop()
	for {
		done, failed, terminal := demoRealSelectedState(vaultPath, manifest, selected)
		if terminal {
			demoCollectRealEvidence(exec, repoRoot, vaultPath, manifest, &manifest.Runs[runIndex], selected)
			manifest.Runs[runIndex].FinishedAt = time.Now().UTC().Format(time.RFC3339)
			if err := demoSaveManifest(repoRoot, manifest); err != nil {
				return nil, 0, err
			}
			final := manifest.Runs[runIndex]
			payload := map[string]any{
				"scenario": manifest.Scenario, "repo": repoRoot, "run": final.RunID,
				"executor": "real-harness", "mode": demoRunModeReal, "waves": waves,
				"task_profiles": final.TaskProfiles, "intervals": final.Intervals,
				"results": final.Results, "notes": final.Notes,
				"instructions": instructions,
				"text":         demoRealRunText(final),
			}
			code := demoExitOK
			if len(done) != len(selected) {
				code = demoExitAssertion
			}
			if failed > 0 {
				code = demoExitAssertion
			}
			_ = done
			return payload, code, nil
		}
		select {
		case <-ctx.Done():
			demoCollectRealEvidence(exec, repoRoot, vaultPath, manifest, &manifest.Runs[runIndex], selected)
			manifest.Runs[runIndex].FinishedAt = time.Now().UTC().Format(time.RFC3339)
			manifest.Runs[runIndex].Notes = append(manifest.Runs[runIndex].Notes, "run interrupted by operator before terminal state")
			_ = demoSaveManifest(repoRoot, manifest)
			return map[string]any{
				"repo": repoRoot, "run": manifest.Runs[runIndex].RunID, "state": "interrupted",
				"text": "real-harness run interrupted; tasks keep their runtime state, rerun to continue observing",
			}, demoExitOK, nil
		case <-ticker.C:
		}
		if !time.Now().Before(deadline) {
			demoCollectRealEvidence(exec, repoRoot, vaultPath, manifest, &manifest.Runs[runIndex], selected)
			manifest.Runs[runIndex].FinishedAt = time.Now().UTC().Format(time.RFC3339)
			manifest.Runs[runIndex].Notes = append(manifest.Runs[runIndex].Notes, fmt.Sprintf("timed out after %s waiting for terminal state; start the resident runtime (`tusker daemon run` from an independent shell, never from an agent session) or have the configured agent drive the tasks, then rerun", timeout.String()))
			_ = demoSaveManifest(repoRoot, manifest)
			return map[string]any{
					"repo": repoRoot, "run": manifest.Runs[runIndex].RunID, "state": "waiting",
					"executor": "real-harness", "mode": demoRunModeReal, "waves": waves,
					"task_profiles": manifest.Runs[runIndex].TaskProfiles,
					"instructions":  instructions,
					"text":          "timeout waiting for terminal state (not a task failure): tasks keep their runtime state",
				}, demoExitTimeout, tuskerError(demoCodeTimeout, "real-harness run timed out after "+timeout.String(),
					withHint("start the resident runtime from an independent shell (never from an agent session) or have the configured agent drive the tasks through work start/submit/review/close, then rerun"))
		}
	}
}

func demoExecuteWave(ctx context.Context, projectID, waveID string) (*serveWaveExecuteReceipt, error) {
	if strings.TrimSpace(projectID) == "" {
		return nil, tuskerError(demoCodePrecondition, "real-harness run is not registered in the resident runtime", withHint("reset and reseed the fixture after installing the current Tusker candidate"))
	}
	base := demoServeBaseURL
	var capability struct {
		Capability string `json:"capability"`
	}
	if err := demoHTTPJSON(ctx, http.MethodGet, base+"/api/capability", "", &capability); err != nil {
		return nil, tuskerError(demoCodePrecondition, "resident runtime is unavailable: "+err.Error(), withHint("install and open the current TuskerBar candidate, then rerun"))
	}
	var result serveWaveExecuteResult
	endpoint := base + "/api/waves/" + url.PathEscape(waveID) + "/execute?project=" + url.QueryEscape(projectID)
	if err := demoHTTPJSON(ctx, http.MethodPost, endpoint, capability.Capability, &result); err != nil {
		return nil, tuskerError(demoCodePrecondition, "Execute Wave request failed: "+err.Error())
	}
	if !result.OK || result.Refused || result.Execution == nil {
		return nil, tuskerError(demoCodePrecondition, "Execute Wave refused: "+firstNonEmpty(result.Reason, "no execution receipt"))
	}
	return result.Execution, nil
}

func demoHTTPJSON(ctx context.Context, method, endpoint, capability string, out any) error {
	var body io.Reader = http.NoBody
	if method == http.MethodPost {
		body = bytes.NewBufferString(`{}`)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return err
	}
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(serveCapabilityHeader, capability)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s returned %s", endpoint, resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func demoSelectedKeys(manifest *demoManifest, waves []string) []string {
	in := map[string]bool{}
	for _, name := range waves {
		in[name] = true
	}
	var keys []string
	for key, task := range manifest.Tasks {
		if in[task.Wave] {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys
}

func demoLiveTaskStatus(vaultPath, taskID string) string {
	idx, err := loadV7Index(vaultPath)
	if err != nil {
		return "unknown"
	}
	note, ok := idx.Tasks[taskID]
	if !ok {
		return "missing"
	}
	return strings.ToLower(strings.TrimSpace(stringField(note.Data, "status")))
}

func demoOpenGateID(vaultPath, taskID string) string {
	idx, err := loadV7Index(vaultPath)
	if err != nil {
		return ""
	}
	for _, gate := range idx.Gates {
		if !v7GateTouchesTask(gate, taskID) {
			continue
		}
		if status := strings.ToLower(strings.TrimSpace(stringField(gate.Data, "status"))); status != "satisfied" && status != "waived" && status != "obsolete" {
			return stringField(gate.Data, "id")
		}
	}
	return ""
}

// demoRealSelectedState reports per-selected-task terminal progress without
// mutating anything: done counts completions, failed counts failures, and
// terminal is true only when every selected task is done, failed, cancelled
// or superseded.
func demoRealSelectedState(vaultPath string, manifest *demoManifest, selected []string) (done []string, failed int, terminal bool) {
	idx, err := loadV7Index(vaultPath)
	if err != nil {
		return nil, 0, false
	}
	terminal = true
	for _, key := range selected {
		rec := manifest.Tasks[key]
		note, ok := idx.Tasks[rec.TaskID]
		if !ok {
			terminal = false
			continue
		}
		switch strings.ToLower(strings.TrimSpace(stringField(note.Data, "status"))) {
		case "done":
			done = append(done, key)
		case "failed":
			failed++
		case "cancelled", "superseded":
		default:
			terminal = false
		}
	}
	return done, failed, terminal
}

func recordOutcomeInto(record *demoRunRecord, manifest *demoManifest, key, outcome, attempt, started, finished string) {
	rec := manifest.Tasks[key]
	result := record.Results[rec.Wave]
	switch outcome {
	case "done":
		result.Completed = append(result.Completed, key)
	case "failed":
		result.Failed = append(result.Failed, key)
	case "blocked":
		result.Blocked = append(result.Blocked, key)
	case "interrupted":
		result.Interrupted = append(result.Interrupted, key)
	}
	record.Results[rec.Wave] = result
	if started != "" {
		record.Intervals = append(record.Intervals, demoInterval{
			TaskID: rec.TaskID, Wave: rec.Wave, Attempt: attempt,
			Started: started, Finished: finished, Outcome: outcome,
		})
	}
}

// demoCollectRealEvidence binds runtime attempts to the run record: one
// interval per recorded attempt plus the profile's attempt ID, so overlap,
// joins and attempt binding are asserted from runtime evidence rather than
// driver sleeps.
func demoCollectRealEvidence(exec *demoExec, repoRoot, vaultPath string, manifest *demoManifest, record *demoRunRecord, selected []string) {
	for _, key := range selected {
		rec := manifest.Tasks[key]
		status := demoLiveTaskStatus(vaultPath, rec.TaskID)
		outcome := status
		switch status {
		case "done":
		case "failed", "cancelled", "superseded", "blocked":
		default:
			outcome = "waiting"
		}
		attempts := demoRuntimeAttempts(exec, repoRoot, rec.TaskID)
		profile := record.TaskProfiles[key]
		if len(attempts) == 0 {
			if outcome == "done" || outcome == "failed" {
				recordOutcomeInto(record, manifest, key, outcome, profile.Attempt, "", "")
			} else {
				recordOutcomeInto(record, manifest, key, "interrupted", profile.Attempt, "", "")
			}
			continue
		}
		for _, attempt := range attempts {
			finished := attempt.Finished
			if finished == "" {
				finished = time.Now().UTC().Format(time.RFC3339Nano)
			}
			intervalOutcome := outcome
			if attempt.Outcome != "" && outcome != "done" && outcome != "failed" {
				intervalOutcome = attempt.Outcome
			}
			recordOutcomeInto(record, manifest, key, intervalOutcome, attempt.ID, attempt.Started, finished)
			if attempt.ID != "" {
				profile.Attempt = attempt.ID
				if attempt.Transport != "" {
					profile.Transport = attempt.Transport
				}
			}
		}
		record.TaskProfiles[key] = profile
	}
}

type demoRuntimeAttempt struct {
	ID        string
	Started   string
	Finished  string
	Outcome   string
	Transport string
}

func demoRuntimeAttempts(exec *demoExec, repoRoot, taskID string) []demoRuntimeAttempt {
	var out []demoRuntimeAttempt
	inspected, err := exec.run(repoRoot, "runs", "inspect", taskID)
	if err != nil {
		return out
	}
	attempts, _ := demoDig(inspected, "attempts").([]any)
	for _, entry := range attempts {
		record, _ := entry.(map[string]any)
		if record == nil {
			continue
		}
		attempt := demoRuntimeAttempt{
			Started:   demoStringField(record, "started_at"),
			Finished:  demoStringField(record, "finished_at"),
			Outcome:   strings.ToLower(strings.TrimSpace(demoStringField(record, "outcome"))),
			Transport: "unknown",
		}
		attempt.ID = demoStringField(record, "attempt_id", "id")
		if demoStringField(record, "cloud_task_id") != "" {
			attempt.Transport = "cloud"
		}
		if attempt.Started == "" {
			continue
		}
		out = append(out, attempt)
	}
	return out
}

func demoRealInstructions(manifest *demoManifest, selected []string, profiles map[string]demoRealTaskProfile) []string {
	var lines []string
	lines = append(lines, "Real-harness run: the driver authorized the waves; the configured runtime performs the work.")
	for _, key := range selected {
		rec := manifest.Tasks[key]
		profile := profiles[key]
		lines = append(lines, fmt.Sprintf("task %s (%s): profile %q harness %q model %q; work in %s, run `python3 sample/tools/wait_progress.py` for visible progress, then `tusker work start/submit` and review/close through the ordinary CLI",
			key, rec.TaskID, profile.Profile, profile.Harness, profile.Model, demoTaskDir(rec.Artifact)))
	}
	return lines
}

func demoRealRunText(record demoRunRecord) string {
	var lines []string
	lines = append(lines, fmt.Sprintf("real-harness run %s:", record.RunID))
	keys := make([]string, 0, len(record.TaskProfiles))
	for key := range record.TaskProfiles {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		profile := record.TaskProfiles[key]
		lines = append(lines, fmt.Sprintf("  task %s (%s): profile %q harness %q model %q effort %q transport %q attempt %q",
			key, profile.TaskID, profile.Profile, profile.Harness, profile.Model, profile.Effort, profile.Transport, profile.Attempt))
	}
	names := make([]string, 0, len(record.Results))
	for name := range record.Results {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		result := record.Results[name]
		lines = append(lines, fmt.Sprintf("  wave %s: %d completed, %d failed, %d blocked, %d interrupted",
			name, len(result.Completed), len(result.Failed), len(result.Blocked), len(result.Interrupted)))
	}
	return strings.Join(lines, "\n")
}

func demoRealEntryContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
}
