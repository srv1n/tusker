#!/bin/sh
# CLI-driven acceptance journey for the resettable real-work test repository.
# Machine-only, bounded, nonzero on failure; proof JSON lands in --proof-dir
# (outside the demo repo, so reset never deletes it), stdout stays compact.
#
# Modes:
#   offline  deterministic timer lane (default; needs no provider or daemon)
#   real     configured-harness lane (needs profiles + resident runtime)
#   mixed    real lane with per-wave profiles (needs codex + Muse profiles)
#
# Variants (--variant fail-once|reject-once|cancel) run the explicit offline
# failure modes so the normal journey stays short.
set -eu

REPO=""
CANDIDATE="${TUSKER_CANDIDATE:-tusker}"
MODE="offline"
FAST="--fast"
REQUIRE_HARNESS=""
PROFILE=""
CODEX_PROFILE=""
MUSE_PROFILE=""
CODEX_HARNESS="codex_exec"
MUSE_HARNESS="muse"
TIMEOUT="15m"
PROOF_DIR=""
VARIANT=""
STAGE="full"

usage() {
	printf 'usage: %s --repo <dir> [--candidate <bin>] [--mode offline|real|mixed] [--full-delays]\n' "$0" >&2
	printf '       [--require-harness NAME] [--profile NAME] [--codex-profile NAME] [--muse-profile NAME]\n' >&2
	printf '       [--stage full|standalone|wave|seed-only] [--timeout 15m] [--proof-dir DIR]\n' >&2
	printf '       [--variant fail-once|reject-once|cancel|session:<scenario>|session:all]\n' >&2
	exit 64
}

while [ $# -gt 0 ]; do
	case "$1" in
		--repo) REPO="$2"; shift 2 ;;
		--candidate) CANDIDATE="$2"; shift 2 ;;
		--mode) MODE="$2"; shift 2 ;;
		--full-delays) FAST=""; shift ;;
		--require-harness) REQUIRE_HARNESS="$2"; shift 2 ;;
		--profile) PROFILE="$2"; shift 2 ;;
		--codex-profile) CODEX_PROFILE="$2"; shift 2 ;;
		--muse-profile|--claude-profile) MUSE_PROFILE="$2"; shift 2 ;;
		--codex-harness) CODEX_HARNESS="$2"; shift 2 ;;
		--muse-harness|--claude-harness) MUSE_HARNESS="$2"; shift 2 ;;
		--stage) STAGE="$2"; shift 2 ;;
		--timeout) TIMEOUT="$2"; shift 2 ;;
		--proof-dir) PROOF_DIR="$2"; shift 2 ;;
		--variant) VARIANT="$2"; shift 2 ;;
		-h|--help) usage ;;
		*) printf 'unknown flag: %s\n' "$1" >&2; usage ;;
	esac
done

[ -n "$REPO" ] || usage
case "$MODE" in offline|real|mixed) ;; *) printf 'bad --mode: %s\n' "$MODE" >&2; usage ;; esac
case "$VARIANT" in ""|fail-once|reject-once|cancel|session:restart|session:kill-worker|session:say-hard|session:say-soft|session:ask-wait|session:ask-nowait|session:stop-continue|session:start-fresh|session:permission-deny|session:all) ;; *) printf 'bad --variant: %s\n' "$VARIANT" >&2; usage ;; esac
case "$STAGE" in full|standalone|wave|seed-only) ;; *) printf 'bad --stage: %s\n' "$STAGE" >&2; usage ;; esac
if [ "$MODE" = "real" ] && [ -z "$REQUIRE_HARNESS$PROFILE" ]; then
	printf 'real mode needs --require-harness and/or --profile (no silent substitution)\n' >&2
	exit 64
fi
if [ "$MODE" = "mixed" ] && { [ -z "$CODEX_PROFILE" ] || [ -z "$MUSE_PROFILE" ]; }; then
	printf 'mixed mode needs --codex-profile and --muse-profile\n' >&2
	exit 64
fi
case "$VARIANT:$MODE" in
	session:*:real) [ -n "$REQUIRE_HARNESS" ] || { printf 'session variants need one --require-harness\n' >&2; exit 64; } ;;
	session:*:*) printf 'session variants require --mode real\n' >&2; exit 64 ;;
	:*) ;;
	*:offline) ;;
	*) printf 'failure variants require --mode offline\n' >&2; exit 64 ;;
esac
if ! command -v "$CANDIDATE" >/dev/null 2>&1; then
	printf 'candidate not found: %s (pass --candidate <tusker-bin>)\n' "$CANDIDATE" >&2
	exit 64
fi
if ! command -v python3 >/dev/null 2>&1; then
	printf 'python3 is required for proof assertions\n' >&2
	exit 64
fi

if [ -z "$PROOF_DIR" ]; then
	PROOF_DIR="${TMPDIR:-/tmp}/real-work-proof-$(date +%Y%m%d-%H%M%S)"
fi
mkdir -p "$PROOF_DIR"
VAULT="$REPO/.tusker"
STEP=0

log() { printf '[%s] %s\n' "$MODE" "$1"; }
fail() { printf 'FAIL: %s (see %s)\n' "$1" "$PROOF_DIR" >&2; exit 1; }

# run_step <name> <proof-file> -- <args...>: run the candidate, save stdout JSON.
run_step() {
	name="$1"; file="$2"; shift 2
	[ "$1" = "--" ] && shift
	STEP=$((STEP + 1))
	set +e
	"$CANDIDATE" "$@" --json >"$PROOF_DIR/$file" 2>"$PROOF_DIR/$file.stderr"
	code=$?
	set -e
	if [ "$code" != "0" ]; then
		log "step $STEP $name ... exit $code"
		cat "$PROOF_DIR/$file.stderr" >&2 || true
		fail "$name exited $code"
	fi
	log "step $STEP $name ... ok"
}

# expect_exit <want> <name> <proof-file> -- <args...>: assert an exit code.
expect_exit() {
	want="$1"; name="$2"; file="$3"; shift 3
	[ "$1" = "--" ] && shift
	STEP=$((STEP + 1))
	set +e
	"$CANDIDATE" "$@" --json >"$PROOF_DIR/$file" 2>"$PROOF_DIR/$file.stderr"
	code=$?
	set -e
	if [ "$code" != "$want" ]; then
		log "step $STEP $name ... exit $code, want $want"
		cat "$PROOF_DIR/$file.stderr" >&2 || true
		fail "$name exited $code, want $want"
	fi
	log "step $STEP $name ... ok (exit $want)"
}

pyassert() {
	file="$1"; script="$2"
	if ! python3 - "$PROOF_DIR/$file" "$REPO" <<PYEOF
import json, sys
proof_path, repo = sys.argv[1], sys.argv[2]
doc = json.load(open(proof_path))
$script
PYEOF
	then
		fail "assertion on $file"
	fi
}

need_ids() {
	pyassert "$1" '
assert len(doc.get("tasks", {})) == 13, "want 13 tasks, got %d" % len(doc.get("tasks", {}))
assert "s1" in doc["tasks"], "standalone task s1 missing"
'
}

# run_waves <proof-file> <waves-csv>: one driver invocation per lane.
# In mixed mode each call covers waves for one harness only (standalone and
# follow-up run on the codex profile; beta runs on the Muse profile).
run_waves() {
	file="$1"; waves="$2"
	if [ "$MODE" = "offline" ]; then
		# shellcheck disable=SC2086
		run_step "run $waves" "$file" -- demo run --repo "$REPO" --waves "$waves" $FAST
		return
	fi
	prof="$PROFILE"; harness="$REQUIRE_HARNESS"
	if [ "$MODE" = "mixed" ]; then
		case "$waves" in
			*beta*) prof="$MUSE_PROFILE"; harness="$MUSE_HARNESS" ;;
			*) prof="$CODEX_PROFILE"; harness="$CODEX_HARNESS" ;;
		esac
	fi
	run_step "run $waves (profile $prof)" "$file" -- demo run --repo "$REPO" --waves "$waves" --mode real --require-harness "$harness" --profile "$prof" --timeout "$TIMEOUT"
}

run_mixed_wave() {
	run_waves "$1" "$2"
}

# assert_mixed_attribution checks every real run's recorded harness against
# the wave assignment: beta on Muse, everything else on codex.
assert_mixed_attribution() {
	python3 - "$PROOF_DIR" <<'PYEOF' || fail "mixed harness attribution"
import glob, json, sys
proof = sys.argv[1]
seen = {}
for path in sorted(glob.glob(proof + "/run-*.json")):
    doc = json.load(open(path))
    if doc.get("executor") != "real-harness":
        continue
    for key, prof in doc.get("task_profiles", {}).items():
        seen[key] = prof.get("harness")
assert seen, "no real-harness task profiles recorded"
for key, harness in sorted(seen.items()):
    want = "muse" if key.startswith("b") else "codex"
    assert want in harness.lower(), "%s ran on %s, want %s" % (key, harness, want)
    print("attribution: %s ran on %s" % (key, harness))
PYEOF
}

assert_terminal() {
	run_step "wait terminal" "$1" -- demo wait --repo "$REPO" --until terminal --timeout "$TIMEOUT"
}

assert_check_ok() {
	run_step "check" "$1" -- demo check --repo "$REPO"
	pyassert "$1" '
assert doc.get("failed", -1) == 0, "check reported %s failures" % doc.get("failed")
'
}

# assert_overlap reads the manifest ledger (runtime evidence in both lanes:
# offline intervals come from driver claims, real intervals from attempts).
assert_overlap() {
	python3 - "$REPO/.tusker/demo/manifest.json" <<'PYEOF' || fail "alpha/beta overlap assertion"
import json, sys
from datetime import datetime
manifest = json.load(open(sys.argv[1]))
# Scope to the current reset generation: intervals from earlier passes would
# otherwise mix predecessors from one pass with joins from another.
runs = manifest["runs"][manifest.get("runs_at_reset", 0):]

def parse(raw):
    raw = raw.strip()
    if raw.endswith("Z"):
        raw = raw[:-1] + "+00:00"
    return datetime.fromisoformat(raw)

by_wave = {}
for run in runs:
    for iv in run.get("intervals", []):
        if iv.get("outcome") != "done" or not iv.get("attempt"):
            continue
        by_wave.setdefault(iv["wave"], []).append(iv)
assert "alpha" in by_wave and "beta" in by_wave, "both waves need done intervals with attempts"
def span(ivs):
    lo = min(parse(i["started_at"]) for i in ivs)
    hi = max(parse(i["finished_at"]) for i in ivs)
    return lo, hi
alo, ahi = span(by_wave["alpha"])
blo, bhi = span(by_wave["beta"])
assert alo < bhi and blo < ahi, "alpha/beta spans are disjoint: %s..%s vs %s..%s" % (alo, ahi, blo, bhi)
print("overlap: alpha %s..%s beta %s..%s" % (alo, ahi, blo, bhi))
# joins start after every branch finishes
by_key = {}
key_of = {t["task_id"]: k for k, t in manifest["tasks"].items()}
for run in runs:
    for iv in run.get("intervals", []):
        if iv.get("outcome") == "done":
            by_key[key_of[iv["task_id"]]] = iv
edges = 0
for key, task in manifest["tasks"].items():
    if len(task.get("deps", [])) < 2 or key not in by_key:
        continue
    join_start = parse(by_key[key]["started_at"])
    for dep in task["deps"]:
        dep_key = dep.split("/")[-1]
        for k, t in manifest["tasks"].items():
            if t["wave"] == task["wave"] and t["source_key"] == dep_key:
                dep_key = k
        if dep_key not in by_key:
            continue
        edges += 1
        assert join_start >= parse(by_key[dep_key]["finished_at"]), "%s started before %s finished" % (key, dep_key)
assert edges > 0, "no join edges observed"
print("joins: %d branch edges ordered, attempts bound" % edges)
PYEOF
}

route_of() {
	# route_of <task-id> <lane> <proof-file> [enforce]: report-only in the
	# offline lane, blocking in real lanes.
	enforce="${4:-enforce}"
	"$CANDIDATE" runner route "$1" --lane "$2" --vault "$VAULT" --json >"$PROOF_DIR/$3" 2>"$PROOF_DIR/$3.stderr" || fail "runner route $1 $2"
	if ! python3 - "$PROOF_DIR/$3" <<'PYEOF'
import json, sys
doc = json.load(open(sys.argv[1]))
assert not doc.get("blockers"), "route blocked: %s" % doc.get("blockers")
print("route %s/%s: profile=%s harness=%s model=%s" % (doc.get("task"), doc.get("lane"), doc.get("profile"), doc.get("harness"), doc.get("model")))
PYEOF
	then
		if [ "$enforce" = "enforce" ]; then
			fail "route $1 $2 has blockers"
		fi
		log "route $1 $2 blocked (offline lane reports only)"
	fi
}

task_id_of() {
	python3 - "$PROOF_DIR/seed.json" <<PYEOF
import json, sys
print(json.load(open(sys.argv[1]))["tasks"]["$1"])
PYEOF
}

status_all_done() {
	pyassert "$1" '
for t in doc.get("tasks", []):
    assert t.get("status") == "done", "%s is %s" % (t.get("key"), t.get("status")
        + ("; blockers=" + ",".join(t.get("blockers", [])) if t.get("blockers") else ""))
    assert t.get("lease", "none") == "none", "%s has lease %s" % (t.get("key"), t.get("lease"))
'
}

journey_seed() {
	# Step 1-2: seed (or confirm idempotent seed), exact graphs, valid corpus.
	if [ -f "$REPO/.tusker/demo/manifest.json" ]; then
		run_step "seed (idempotent)" "seed.json" -- demo seed --repo "$REPO" --scenario parallel-waves
		pyassert "seed.json" 'assert doc.get("repeated_seed") is True, "expected idempotent reseed"'
	else
		run_step "seed" "seed.json" -- demo seed --repo "$REPO" --scenario parallel-waves
	fi
	need_ids "seed.json"
	pyassert "seed.json" '
assert doc.get("project_id"), "seed must return the project ID"
assert doc.get("runtime_project_id") or doc.get("runtime_problem"), "seed must report runtime registration or its reason"
assert doc.get("serve_hint"), "seed must report how to open the project"
'
	log "project $(python3 -c 'import json;print(json.load(open("'$PROOF_DIR'/seed.json"))["project_id"])') runtime-scope $(python3 -c 'import json;print(json.load(open("'$PROOF_DIR'/seed.json"))["runtime_scope"])')"
	run_step "docs check" "docs.json" -- docs check --vault "$VAULT"
	pyassert "docs.json" 'assert doc.get("valid") is True, "corpus invalid: %s" % doc.get("issues")'
	run_step "status" "status-seeded.json" -- demo status --repo "$REPO"
	pyassert "status-seeded.json" '
names = sorted(w["name"] for w in doc.get("waves", []))
assert names == ["alpha", "beta", "follow-up", "standalone"], names
tasks = {t["key"]: t for t in doc.get("tasks", [])}
assert len(tasks) == 13 and "s1" in tasks
assert tasks["s1"]["status"] == "ready", "s1 must be ready, got %s" % tasks["s1"]["status"]
assert tasks["a1"]["status"] == "ready" and tasks["b1"]["status"] == "ready"
assert tasks["c1"]["status"] == "backlog", "c1 must wait for a4/b4, got %s" % tasks["c1"]["status"]
'
}

journey_reset_cycle() {
	# Step 1: preview is non-mutating; apply restores the equivalent scenario.
	run_step "reset preview" "reset-preview.json" -- demo reset --repo "$REPO"
	pyassert "reset-preview.json" '
assert doc.get("dry_run") is True, "preview must be dry"
assert doc.get("remove"), "preview must list removals"
'
	run_step "reset apply" "reset-apply.json" -- demo reset --repo "$REPO" --yes
	pyassert "reset-apply.json" 'assert doc.get("dry_run") is False, "apply must mutate"'
	run_step "seed (post-reset idempotent)" "seed-reseed.json" -- demo seed --repo "$REPO" --scenario parallel-waves
	pyassert "seed-reseed.json" 'assert doc.get("repeated_seed") is True, "post-reset seed must be idempotent"'
	# Equivalent scenario: contract identities survive reset by design.
	python3 - "$PROOF_DIR/seed.json" "$PROOF_DIR/reset-apply.json" <<'PYEOF' || fail "reset changed contract identities"
import json, sys
before = json.load(open(sys.argv[1]))["tasks"]
after = json.load(open(sys.argv[2]))["tasks"]
assert before == after, "contract identities changed across reset"
print("reset cycle ok (13 stable contract identities, attempts refresh per run)")
PYEOF
}

journey_standalone() {
	# Steps 3-4: effective profiles, then the standalone task end to end.
	s1=$(task_id_of s1)
	if [ "$MODE" = "offline" ]; then
		route_of "$s1" execute "route-s1-execute.json" report
		route_of "$s1" review "route-s1-review.json" report
		log "offline lane: routes reported, timer profiles execute"
	else
		route_of "$s1" execute "route-s1-execute.json"
		route_of "$s1" review "route-s1-review.json"
	fi
	run_waves "run-standalone.json" "standalone"
	run_step "status standalone" "status-standalone.json" -- demo status --repo "$REPO"
	pyassert "status-standalone.json" '
tasks = {t["key"]: t for t in doc.get("tasks", [])}
assert tasks["s1"]["status"] == "done", "s1 is %s" % tasks["s1"]["status"]
assert tasks["s1"]["lease"] == "none", "s1 lease leaked: %s" % tasks["s1"]["lease"]
'
	log "standalone accepted and closed through native review"
}

journey_waves() {
	# Step 5: alpha+beta overlap, joins wait, attempts bound to real attempts.
	# Mixed mode runs the waves sequentially with per-wave profiles (overlap
	# is proven by the prerequisite codex-only run); otherwise one explicit
	# start covers both waves so overlap is observable.
	if [ "$MODE" = "mixed" ]; then
		run_mixed_wave "run-alpha.json" "alpha" "$CODEX_PROFILE" "$CODEX_HARNESS"
		run_mixed_wave "run-beta.json" "beta" "$MUSE_PROFILE" "$MUSE_HARNESS"
		assert_mixed_attribution
	else
		run_waves "run-parallel.json" "alpha,beta"
		assert_overlap
	fi
	run_step "status parallel" "status-parallel.json" -- demo status --repo "$REPO"
	pyassert "status-parallel.json" '
tasks = {t["key"]: t for t in doc.get("tasks", [])}
for k in ["a1", "a2", "a3", "a4", "b1", "b2", "b3", "b4"]:
    assert tasks[k]["status"] == "done", "%s is %s" % (k, tasks[k]["status"])
'
}

journey_followup() {
	# Step 6: follow-up needs an explicit start, then completes.
	run_step "status pre-followup" "status-prefollowup.json" -- demo status --repo "$REPO"
	pyassert "status-prefollowup.json" '
tasks = {t["key"]: t for t in doc.get("tasks", [])}
for k in ["c1", "c2", "c3", "c4"]:
    assert tasks[k]["status"] != "done", "%s auto-started" % k
'
	run_waves "run-followup.json" "follow-up"
	assert_terminal "wait-followup.json"
	run_step "status final" "status-final.json" -- demo status --repo "$REPO"
	status_all_done "status-final.json"
	assert_check_ok "check.json"
}

journey_full() {
	journey_seed
	journey_reset_cycle
	journey_standalone
	journey_waves
	journey_followup
	# Keep the first pass proof before the repeat overwrites run filenames.
	mkdir -p "$PROOF_DIR/pass-1"
	cp "$PROOF_DIR"/run-*.json "$PROOF_DIR"/status-*.json "$PROOF_DIR"/check.json "$PROOF_DIR/pass-1/" 2>/dev/null || true
	# Step 8: reset and repeat the essential journey with fresh attempts.
	run_step "reset apply (second)" "reset-apply-2.json" -- demo reset --repo "$REPO" --yes
	journey_standalone
	journey_waves
	journey_followup
	log "PASS: $MODE journey complete; proof in $PROOF_DIR"
}

journey_one_wave() {
	journey_seed
	run_waves "run-alpha.json" "alpha"
	assert_terminal "wait-alpha.json"
	run_step "status alpha" "status-alpha.json" -- demo status --repo "$REPO"
	pyassert "status-alpha.json" '
tasks = {t["key"]: t for t in doc.get("tasks", [])}
for k in ["a1", "a2", "a3", "a4"]:
    assert tasks[k]["status"] == "done", "%s is %s" % (k, tasks[k]["status"])
'
	log "PASS: $MODE Alpha wave complete; proof in $PROOF_DIR"
}

journey_seed_only() {
	if [ -f "$REPO/.tusker/demo/manifest.json" ]; then
		run_step "reset preview" "reset-preview.json" -- demo reset --repo "$REPO"
		run_step "reset apply" "reset-apply.json" -- demo reset --repo "$REPO" --yes
	fi
	journey_seed
	log "PASS: fresh idle fixture ready; proof in $PROOF_DIR"
}

variant_fail_once() {
	journey_seed
	expect_exit 4 "run beta fail-once b3" "variant-fail.json" -- demo run --repo "$REPO" --waves beta --fast --fail-once b3
	run_waves "variant-retry.json" "beta"
	assert_check_ok "variant-check.json"
	log "PASS: fail-once parked the join, retry completed without duplicates"
}

variant_reject_once() {
	journey_seed
	# Complete the predecessors first so follow-up is genuinely runnable,
	# then inject one reviewer rejection and watch correction resubmit.
	run_waves "variant-reject-pre.json" "alpha,beta"
	expect_exit 0 "run follow-up reject-once c2" "variant-reject.json" -- demo run --repo "$REPO" --waves follow-up --fast --reject-once c2
	python3 - "$PROOF_DIR/variant-reject.json" <<'PYEOF' || fail "rejection not recorded"
import json, sys
doc = json.load(open(sys.argv[1]))
notes = "\n".join(doc.get("notes", []))
assert "reviewer rejected" in notes, "no reviewer rejection recorded"
assert doc["results"]["follow-up"]["completed"], "follow-up did not complete after correction"
print("reviewer rejected c2 once; correction resubmitted and completed")
PYEOF
	assert_check_ok "variant-check.json"
	log "PASS: review-rejection path recorded (see variant-reject.json notes)"
}

variant_cancel() {
	journey_seed
	STEP=$((STEP + 1))
	"$CANDIDATE" demo run --repo "$REPO" --waves alpha,beta --json >"$PROOF_DIR/variant-cancel.json" 2>&1 &
	run_pid=$!
	sleep 4
	kill -INT "$run_pid" 2>/dev/null || true
	set +e
	wait "$run_pid"
	code=$?
	set -e
	log "step $STEP cancel run ... exit $code (interrupted)"
	run_step "status after cancel" "variant-cancel-status.json" -- demo status --repo "$REPO"
	python3 - "$PROOF_DIR/variant-cancel-status.json" <<'PYEOF' || fail "cancel left active leases"
import json, sys
doc = json.load(open(sys.argv[1]))
active = [t["key"] for t in doc.get("tasks", []) if t.get("lease", "none") != "none"]
assert not active, "leases leaked: %s" % active
done = [t["key"] for t in doc.get("tasks", []) if t.get("status") == "done"]
assert len(done) < 8, "cancelled run completed everything anyway"
print("cancelled: no leases, %d/8 wave tasks done (no late success yet)" % len(done))
PYEOF
	sleep 3
	run_step "status late-check" "variant-cancel-late.json" -- demo status --repo "$REPO"
	python3 - "$PROOF_DIR/variant-cancel-status.json" "$PROOF_DIR/variant-cancel-late.json" <<'PYEOF' || fail "late success after cancel"
import json, sys
before = {t["key"]: t["status"] for t in json.load(open(sys.argv[1]))["tasks"]}
after = {t["key"]: t["status"] for t in json.load(open(sys.argv[2]))["tasks"]}
late = [k for k in after if after[k] == "done" and before.get(k) != "done"]
assert not late, "late success after cancel: %s" % late
print("no late success after cancel")
PYEOF
	run_step "reset apply (post-cancel)" "variant-cancel-reset.json" -- demo reset --repo "$REPO" --yes
	log "PASS: cancel released ownership, no late success, reset clean"
}

variant_session() {
	case "$VARIANT" in
		session:all)
			printf 'session:all needs a reset and a new standalone run between scenarios; run each scenario separately:\n' >&2
			for session_name in restart kill-worker say-hard say-soft ask-wait ask-nowait stop-continue start-fresh permission-deny; do
				printf '  tusker demo reset --repo %s --yes && %s --repo %s --mode real --require-harness %s --variant session:%s\n' "$REPO" "$0" "$REPO" "$REQUIRE_HARNESS" "$session_name" >&2
			done
			exit 3 ;;
		*) session_names=${VARIANT#session:} ;;
	esac
	journey_seed
	for session_name in $session_names; do
		STEP=$((STEP + 1))
		set -- demo session --repo "$REPO" --harness "$REQUIRE_HARNESS" --scenario "$session_name" --require-harness "$REQUIRE_HARNESS" --json
		if [ -n "$PROFILE" ]; then set -- "$@" --profile "$PROFILE"; fi
		set +e
		"$CANDIDATE" "$@" >"$PROOF_DIR/session-$session_name.json" 2>"$PROOF_DIR/session-$session_name.stderr"
		code=$?
		set -e
		python3 - "$PROOF_DIR/session-$session_name.json" <<'PYEOF' || fail "session $session_name proof invalid"
import json, sys
doc = json.load(open(sys.argv[1]))
assert doc['status'] in ('unsupported', 'unavailable', 'manual-required', 'refused', 'passed', 'failed')
assert doc['proof_label'] in ('fixture', 'live-provider')
print('%s: %s — %s' % (doc['scenario'], doc['status'], doc.get('reason', '')))
PYEOF
		[ "$code" -eq 0 ] || fail "session $session_name exited $code"
		python3 - "$PROOF_DIR/session-$session_name.json" <<'PYEOF' || fail "session $session_name did not pass"
import json, sys
doc = json.load(open(sys.argv[1]))
assert doc['status'] == 'passed', '%s: %s' % (doc['status'], doc.get('reason', ''))
proof = json.load(open(doc['proof']))
assert proof['proof_label'] == 'live-provider', 'session variant requires live-provider proof'
assert proof['scenario'] == doc['scenario'] and proof['harness'] == doc['harness']
assert proof['checks'] and all(check['passed'] for check in proof['checks']), 'missing or failed scenario checks'
print('PASS: %s/%s (%d observed checks)' % (proof['harness'], proof['scenario'], len(proof['checks'])))
PYEOF
	done
	log "session proof recorded; follow docs/system/session-testing.md for the operator checklist"
}

if [ -n "$VARIANT" ]; then
	case "$VARIANT" in
		fail-once) variant_fail_once ;;
		reject-once) variant_reject_once ;;
		cancel) variant_cancel ;;
		session:*) variant_session ;;
	esac
	case "$VARIANT" in session:*) log "PASS: live session variant $VARIANT; proof in $PROOF_DIR" ;; *) log "PASS: variant $VARIANT complete; proof in $PROOF_DIR" ;; esac
	exit 0
fi

if [ "$MODE" = "mixed" ]; then
	log "mixed mode: codex=$CODEX_PROFILE/$CODEX_HARNESS muse=$MUSE_PROFILE/$MUSE_HARNESS (run only after a green codex-only real run)"
fi
case "$STAGE" in
	full) journey_full ;;
	standalone) journey_seed; journey_standalone ;;
	wave) journey_one_wave ;;
	seed-only) journey_seed_only ;;
esac
