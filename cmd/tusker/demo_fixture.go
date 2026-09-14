package main

// Deterministic demo fixture: one standalone smoke task plus three waves,
// thirteen tasks total.
//
// The fixture is data, not behavior. Seed renders it into wave-authoring
// input and creates it through the normal wave create path; the demo
// executor only drives supported CLI operations against the created tasks.
// Domain values and report contents are explicitly fictional.
//
// The standalone smoke task exercises the individual-task journey (ordinary
// claim/submit/review/close with no wave siblings) before the parallel
// waves run. Real-harness runs execute the same contracts through configured
// profiles; the offline timer lane only changes how each task is executed,
// never the graph.

const (
	demoScenario         = "parallel-waves"
	demoScenarioVersion  = 2
	demoManifestSchema   = "tusker.demo/v1"
	demoEpicStandalone   = "STN"
	demoEpicAlpha        = "ALP"
	demoEpicBeta         = "BET"
	demoEpicFollowup     = "FOL"
	demoScopeStandalone  = "demo.standalone"
	demoScopeAlpha       = "demo.alpha"
	demoScopeBeta        = "demo.beta"
	demoScopeFollowup    = "demo.followup"
	demoSpecSubject      = "demo-parallel-waves"
	demoImplementActor   = "agent:demo-timer"
	demoReviewerActor    = "reviewer:agent"
	demoProfileImplement = "demo-timer-implement"
	demoProfileReview    = "demo-timer-review"
	demoProfilePlan      = "demo-timer-plan"
)

// demoDelayClass assigns the visible per-task delays from the spec: roots are
// quick, branches take longer so overlap is observable, joins are quick.
// Wall-clock exactness is not a correctness assumption; check asserts order,
// not durations.
type demoTaskDef struct {
	Key        string
	Title      string
	Outcome    string
	Acceptance string
	Artifact   string
	Content    string
	DelaySecs  float64
	FastSecs   float64
	Deps       []string
	// Complexity selects the model work level through the ordinary route
	// preview (routine=light, standard, complex/frontier=demanding) so the
	// fixture exercises all three supported levels. Empty means standard.
	Complexity string
}

// demoWaveDef now supports single-task waves: the standalone smoke journey is
// one independently startable task with no siblings and no dependencies.
type demoWaveDef struct {
	Name        string
	Title       string
	Scope       string
	Epic        string
	EpicTitle   string
	Requirement string
	Tasks       []demoTaskDef
}

func demoFixtureWaves() []demoWaveDef {
	standalone := []demoTaskDef{
		{Key: "s1", Title: "Smoke: build and verify a tiny module", Outcome: "Create a tiny module and test in sample/standalone/, run the supplied progress helper, verify and submit.", Acceptance: "The smoke module, its test, and the smoke artifact exist and the test passes.", Artifact: "sample/standalone/smoke.txt", Content: "standalone-smoke ok", DelaySecs: 3, FastSecs: 0.2, Complexity: "standard"},
	}
	alpha := []demoTaskDef{
		{Key: "a1", Title: "Draft alpha part one", Outcome: "Generate the alpha fixture dataset.", Acceptance: "Part one exists with the exact fixture content.", Artifact: "sample/alpha/part-1.txt", Content: "alpha-1 ok", DelaySecs: 3, FastSecs: 0.2, Complexity: "routine"},
		{Key: "a2", Title: "Draft alpha part two", Outcome: "Add the alpha transformation and validator with tests in non-overlapping files.", Acceptance: "Part two exists with the exact fixture content.", Artifact: "sample/alpha/part-2.txt", Content: "alpha-2 ok", DelaySecs: 6, FastSecs: 0.3, Deps: []string{"a1"}, Complexity: "standard"},
		{Key: "a3", Title: "Draft alpha part three", Outcome: "Add the alpha validator with tests in files owned only by this task.", Acceptance: "Part three exists with the exact fixture content.", Artifact: "sample/alpha/part-3.txt", Content: "alpha-3 ok", DelaySecs: 9, FastSecs: 0.4, Deps: []string{"a1"}, Complexity: "standard"},
		{Key: "a4", Title: "Assemble alpha report", Outcome: "Combine both branch outputs into the alpha report and check them.", Acceptance: "The alpha report exists with the exact fixture content.", Artifact: "sample/alpha/report.txt", Content: "alpha-report ok", DelaySecs: 3, FastSecs: 0.2, Deps: []string{"a2", "a3"}, Complexity: "complex"},
	}
	beta := []demoTaskDef{
		{Key: "b1", Title: "Draft beta part one", Outcome: "Generate the beta fixture dataset.", Acceptance: "Part one exists with the exact fixture content.", Artifact: "sample/beta/part-1.txt", Content: "beta-1 ok", DelaySecs: 3, FastSecs: 0.2, Complexity: "routine"},
		{Key: "b2", Title: "Draft beta part two", Outcome: "Add the beta transformation and validator with tests in non-overlapping files.", Acceptance: "Part two exists with the exact fixture content.", Artifact: "sample/beta/part-2.txt", Content: "beta-2 ok", DelaySecs: 6, FastSecs: 0.3, Deps: []string{"b1"}, Complexity: "standard"},
		{Key: "b3", Title: "Draft beta part three", Outcome: "Add the beta validator with tests in files owned only by this task.", Acceptance: "Part three exists with the exact fixture content.", Artifact: "sample/beta/part-3.txt", Content: "beta-3 ok", DelaySecs: 9, FastSecs: 0.4, Deps: []string{"b1"}, Complexity: "standard"},
		{Key: "b4", Title: "Assemble beta report", Outcome: "Combine both branch outputs into the beta report and check them.", Acceptance: "The beta report exists with the exact fixture content.", Artifact: "sample/beta/report.txt", Content: "beta-report ok", DelaySecs: 3, FastSecs: 0.2, Deps: []string{"b2", "b3"}, Complexity: "complex"},
	}
	followup := []demoTaskDef{
		{Key: "c1", Title: "Open combined report", Outcome: "Start the combined report from both wave reports.", Acceptance: "The combined part one exists with the exact fixture content.", Artifact: "sample/followup/part-1.txt", Content: "combined-1 ok", DelaySecs: 3, FastSecs: 0.2, Complexity: "standard"},
		{Key: "c2", Title: "Draft combined part two", Outcome: "Write the second combined part file.", Acceptance: "Part two exists with the exact fixture content.", Artifact: "sample/followup/part-2.txt", Content: "combined-2 ok", DelaySecs: 6, FastSecs: 0.3, Deps: []string{"c1"}, Complexity: "standard"},
		{Key: "c3", Title: "Draft combined part three", Outcome: "Write the third combined part file.", Acceptance: "Part three exists with the exact fixture content.", Artifact: "sample/followup/part-3.txt", Content: "combined-3 ok", DelaySecs: 9, FastSecs: 0.4, Deps: []string{"c1"}, Complexity: "standard"},
		{Key: "c4", Title: "Assemble combined report", Outcome: "Assemble the final combined report from the combined parts.", Acceptance: "The combined report exists with the exact fixture content.", Artifact: "sample/followup/report.txt", Content: "combined-report ok", DelaySecs: 3, FastSecs: 0.2, Deps: []string{"c2", "c3"}, Complexity: "complex"},
	}
	return []demoWaveDef{
		{Name: "standalone", Title: "Standalone: smoke a single task", Scope: demoScopeStandalone, Epic: demoEpicStandalone, EpicTitle: "Standalone smoke", Requirement: "One individual task completes through the ordinary lifecycle with no wave siblings.", Tasks: standalone},
		{Name: "alpha", Title: "Alpha: assemble a small report", Scope: demoScopeAlpha, Epic: demoEpicAlpha, EpicTitle: "Alpha report", Requirement: "The alpha report is assembled from three deterministic parts.", Tasks: alpha},
		{Name: "beta", Title: "Beta: assemble an independent report", Scope: demoScopeBeta, Epic: demoEpicBeta, EpicTitle: "Beta report", Requirement: "The beta report is assembled from three deterministic parts.", Tasks: beta},
		{Name: "follow-up", Title: "Follow-up: combine the reports", Scope: demoScopeFollowup, Epic: demoEpicFollowup, EpicTitle: "Combined report", Requirement: "The combined report joins the alpha and beta reports.", Tasks: followup},
	}
}

// demoCrossScopeDeps wires the follow-up entry to both wave reports. The
// scope-qualified form is the native cross-scope dependency contract.
func demoCrossScopeDeps() map[string][][2]string {
	return map[string][][2]string{
		"c1": {{demoScopeAlpha, "a4"}, {demoScopeBeta, "b4"}},
	}
}

// demoTaskContext renders the bounded per-task context promised by the
// real-work packet: files to inspect, owned paths, non-goals, exact
// verification and review requirements. It links to the shared fictional
// spec instead of copying it into every plan.
func demoTaskContext(wave demoWaveDef, task demoTaskDef) string {
	check := `test "$(cat ` + task.Artifact + `)" = "` + task.Content + `"`
	return "Context: sample product Fixture Cafe; read .tusker/specs/fixture-cafe/overview.md before implementing.\n" +
		"Files to inspect: " + task.Artifact + " and its wave siblings under " + demoTaskDir(task.Artifact) + ".\n" +
		"Owned paths: " + task.Artifact + " (never edit task status files or sibling outputs).\n" +
		"Non-goals: no work outside " + demoTaskDir(task.Artifact) + ", no new dependencies, no credential handling.\n" +
		"Exact verification (offline): " + check + ".\n" +
		"Real-harness work: implement the outcome above in " + demoTaskDir(task.Artifact) + "/, run `python3 sample/tools/wait_progress.py` so progress is visible (~60s default, --short for cheap runs), execute the real tests, commit the owned artifact, then submit through the ordinary CLI.\n" +
		"Review: independent review lane must accept before close; reviewer re-runs the exact verification."
}

// demoTaskDir returns the sample directory that owns a task artifact.
func demoTaskDir(artifact string) string {
	if idx := indexSlash(artifact); idx > 0 {
		return artifact[:idx]
	}
	return "sample"
}

func indexSlash(s string) int {
	for i := 0; i < len(s); i++ {
		if s[i] == '/' {
			return i
		}
	}
	return -1
}

func demoSpecDoc() string {
	return `---
subject: demo-parallel-waves
title: Demo parallel waves fixture
# Corpus root of this disposable repo: every fictional note below parents to
# this governing spec, which parents to itself so ` + "`tusker docs check`" + ` validates.
part_of: demo-parallel-waves
status: canonical
---

# Demo parallel waves fixture

Governing note for the disposable repeatable-work demo. One standalone smoke
task plus Alpha and Beta, independent reports assembled in parallel waves;
the follow-up wave combines them. All values are fictional fixture content
for CLI-driven testing.
`
}

const demoDecisionTitle = "Demo uses deterministic timer execution"
