#!/bin/sh
# Author the live-harness qualification project. Inert: it never enables
# automation, starts a wave, starts a daemon, or launches a provider.
#
#   sh docs/qualification/live-harness/seed.sh --repo /tmp/tusker-live-qual \
#     [--bin "$(command -v tusker)"] [--proof-dir /tmp/tusker-live-qual.proof] \
#     [--harnesses codex,claude,devin,muse] [--muse-profile <global-profile>] \
#     [--review-profile <global-profile>] [--with-failure]
#
# On top of the standard `tusker demo seed` fixture it creates epic QLH and one
# task + one-task wave per harness. Each task pins its execute lane to one
# global runner profile with --execute-profile and its review lane to a
# profile on a DIFFERENT harness with --review-profile (override all review
# pins with --review-profile). --with-failure adds a task + wave whose execute
# lane is pinned to global profile qual-bad-<harness>, which must already
# exist (runbook step 0.3). IDs go to <proof-dir>/ids.env.
set -eu

REPO="" BIN="$(command -v tusker || true)" PROOF="" HARNESSES="codex,claude,devin,muse"
MUSE_PROFILE="muse-spark-1.3-high" REVIEW_PROFILE="" FAILURE=""
usage() { sed -n '2,16p' "$0" >&2; exit 2; }
while [ $# -gt 0 ]; do
  case "$1" in
    --repo) REPO=$2; shift 2 ;;
    --bin) BIN=$2; shift 2 ;;
    --proof-dir) PROOF=$2; shift 2 ;;
    --harnesses) HARNESSES=$2; shift 2 ;;
    --muse-profile) MUSE_PROFILE=$2; shift 2 ;;
    --review-profile) REVIEW_PROFILE=$2; shift 2 ;;
    --with-failure) FAILURE=1; shift ;;
    *) usage ;;
  esac
done
[ -n "$REPO" ] && [ -n "$BIN" ] || usage
PROOF=${PROOF:-$REPO.proof}
mkdir -p "$REPO" "$PROOF"
REPO=$(cd "$REPO" && pwd -P)
VAULT=$REPO/.tusker
KEYS=$(echo "$HARNESSES" | tr ',' ' ')

profile_for() {
  case "$1" in
    codex) echo codex_exec-gpt-6-luna ;;
    claude) echo claude-opus-high ;;
    devin) echo devin-swe-2-high ;;
    muse) echo "$MUSE_PROFILE" ;;
    *) echo "unknown harness key $1" >&2; exit 2 ;;
  esac
}

# The reviewer runs on a different harness than the worker.
review_for() {
  if [ -n "$REVIEW_PROFILE" ]; then echo "$REVIEW_PROFILE"; return; fi
  case "$1" in
    codex) echo devin-swe-2-high ;;
    *) echo codex_exec-gpt-6-sol-low ;;
  esac
}

"$BIN" demo seed --repo "$REPO" --scenario parallel-waves --json > "$PROOF/seed.json"
PROJECT=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["runtime_project_id"])' "$PROOF/seed.json")

"$BIN" new epic --vault "$VAULT" --acronym QLH --title "Live harness qualification" >/dev/null
printf 'REPO=%s\nVAULT=%s\nPROJECT=%s\n' "$REPO" "$VAULT" "$PROJECT" > "$PROOF/ids.env"

body() { # $1 tag, $2 ask instruction
  cat <<EOF
# Qualify $1 live session

## Outcome

Write \`sample/qual/$1/result.txt\` whose first line is exactly \`$1 ok\`, after two visible pauses that give the operator time to Say, Stop and Continue.

## Context

Do these steps in order, in this one session. Keep every reply short; this is a cost-bounded qualification task.

1. Run \`python3 sample/tools/wait_progress.py --duration 90 --interval 5 --label $1-pause-1\` and let it finish.
2. $2
3. Run \`python3 sample/tools/wait_progress.py --duration 60 --interval 5 --label $1-pause-2\` and let it finish. If it is interrupted, rerun it once when the session continues.
4. Write \`sample/qual/$1/result.txt\`: first line \`$1 ok\`; second line the operator's answer to step 2, verbatim.
5. If at any point you receive an operator message containing a token that starts with \`SAY-\`, write that exact token on its own line to \`sample/qual/$1/say.txt\`.
6. Commit the owned files and submit through the ordinary CLI.

Owned paths: \`sample/qual/$1/\` only. Non-goals: no other edits, no dependencies, no credential handling.

Review lane: do not repeat steps 1-6. Run the Verification command, check the diff touches only the owned paths, and give the verdict.

## Acceptance

| ID | Outcome | Proof |
| --- | --- | --- |
| A1 | The result file exists with the exact first line. | command: test "\$(head -n 1 sample/qual/$1/result.txt)" = "$1 ok" |

## Verification

| Covers | Check | Result |
| --- | --- | --- |
| A1 | command: test "\$(head -n 1 sample/qual/$1/result.txt)" = "$1 ok" | pending |
EOF
}

MCP_ASK='Call the Tusker MCP tool `ask` with `to` = `operator`, `question` = `%s: which word goes on line 2 of result.txt?` and `wait_seconds` = %s. If it returns without an answer, end your turn and wait; when the session continues, call the Tusker MCP tool `check_messages` and use the answer.'
CLI_ASK='Ask the operator with the Tusker CLI (this harness has no MCP overlay): `tusker message ask --project "$TUSKER_PROJECT_ID" --sender "task:$TUSKER_ITEM_ID" --recipient operator:operator --key %s-ask-1 --body "%s: which word goes on line 2 of result.txt?" --yield --json`. Then end your turn and wait. When the session continues, read the answer with `tusker message list --project "$TUSKER_PROJECT_ID" --recipient-kind task --recipient "$TUSKER_ITEM_ID"`.'

author() { # $1 harness key, $2 tag, $3 env prefix, $4 execute profile, $5 review profile
  case "$1" in
    muse) ask=$(printf "$CLI_ASK" "$2" "$2") ;;
    devin) ask=$(printf "$MCP_ASK" "$2" 50) ;;   # Devin's MCP wait is capped at 50s
    *) ask=$(printf "$MCP_ASK" "$2" 300) ;;
  esac
  body "$2" "$ask" > "$PROOF/task-$2.md"
  task=$("$BIN" new task --vault "$VAULT" --epic QLH --title "Qualify $1 live session ($2)" \
    --execute-profile "$4" --review-profile "$5" --work-level light --status ready --owned-paths "sample/qual/$2/" --body-file "$PROOF/task-$2.md" |
    sed -n 's/^Created V7 task \([A-Z]*-T-[0-9]*\) .*/\1/p')
  [ -n "$task" ] || { echo "task create failed for $2" >&2; exit 1; }
  wave=$("$BIN" wave create "Qualify $2" "$task" --vault "$VAULT" --json |
    python3 -c 'import json,sys; print(json.load(sys.stdin)["wave"]["id"])')
  printf '%s_TASK=%s\n%s_WAVE=%s\n%s_PROFILE=%s\n%s_REVIEW_PROFILE=%s\n' "$3" "$task" "$3" "$wave" "$3" "$4" "$3" "$5" >> "$PROOF/ids.env"
  echo "$2: task $task wave $wave execute $4 review $5"
}

for key in $KEYS; do
  up=$(echo "$key" | tr '[:lower:]' '[:upper:]')
  author "$key" "qual-$key" "$up" "$(profile_for "$key")" "$(review_for "$key")"
  if [ -n "$FAILURE" ]; then author "$key" "qual-$key-bad" "${up}_BAD" "qual-bad-$key" "$(review_for "$key")"; fi
done
echo "ids: $PROOF/ids.env"
