#!/bin/sh

set -eu

repo_root=$(cd "$(dirname "$0")/.." && pwd -P)
gate="$repo_root/scripts/with-validation-lock.sh"
tmp_dir=$(mktemp -d "${TMPDIR:-/tmp}/tusker-validation-lock-test.XXXXXX")
first_pid=''
second_pid=''
signal_pid=''

cleanup() {
	for pid in "$first_pid" "$second_pid" "$signal_pid"; do
		if [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null; then
			kill -TERM "$pid" 2>/dev/null || true
		fi
	done
	rm -rf "$tmp_dir"
}
trap cleanup EXIT HUP INT TERM

wait_for_path() {
	path=$1
	remaining=1000
	while [ ! -e "$path" ] && [ "$remaining" -gt 0 ]; do
		sleep 0.02
		remaining=$((remaining - 1))
	done
	[ -e "$path" ] || {
		printf 'timed out waiting for %s\n' "$path" >&2
		exit 1
	}
}

events="$tmp_dir/events"
lock="$tmp_dir/shared.lock"
wait_log="$tmp_dir/wait.log"

EVENTS="$events" TUSKER_VALIDATION_LOCK_DIR="$lock" \
	sh "$gate" -- sh -c 'printf "first-start\n" >>"$EVENTS"; sleep 1; printf "first-end\n" >>"$EVENTS"' &
first_pid=$!
wait_for_path "$lock/pid"
wait_for_path "$events"

EVENTS="$events" TUSKER_VALIDATION_LOCK_DIR="$lock" TUSKER_VALIDATION_LOCK_POLL_SECONDS=0.05 \
	sh "$gate" -- sh -c 'printf "second-start\n" >>"$EVENTS"; printf "second-end\n" >>"$EVENTS"' 2>"$wait_log" &
second_pid=$!
sleep 0.15
if grep -q '^second-start$' "$events"; then
	printf 'second validation entered before the first released the lock\n' >&2
	exit 1
fi
grep -q 'validation gate: busy' "$wait_log"

wait "$first_pid"
first_pid=''
wait "$second_pid"
second_pid=''
expected=$(printf 'first-start\nfirst-end\nsecond-start\nsecond-end')
actual=$(cat "$events")
[ "$actual" = "$expected" ] || {
	printf 'unexpected validation order:\n%s\n' "$actual" >&2
	exit 1
}
[ ! -d "$lock" ] || {
	printf 'validation lock survived normal exit\n' >&2
	exit 1
}

stale_lock="$tmp_dir/stale.lock"
mkdir "$stale_lock"
printf '99999999\n' >"$stale_lock/pid"
printf 'stale\n' >"$stale_lock/token"
TUSKER_VALIDATION_LOCK_DIR="$stale_lock" sh "$gate" -- true 2>"$tmp_dir/stale.log"
grep -q 'recovered stale owner pid 99999999' "$tmp_dir/stale.log"
[ ! -d "$stale_lock" ] || {
	printf 'recovered stale lock survived command exit\n' >&2
	exit 1
}

signal_lock="$tmp_dir/signal.lock"
TUSKER_VALIDATION_LOCK_DIR="$signal_lock" sh "$gate" -- sleep 30 &
signal_pid=$!
wait_for_path "$signal_lock/pid"
kill -TERM "$signal_pid"
wait "$signal_pid" 2>/dev/null || true
signal_pid=''
[ ! -d "$signal_lock" ] || {
	printf 'validation lock survived TERM\n' >&2
	exit 1
}

git_repo="$tmp_dir/repo"
git_peer="$tmp_dir/peer"
git init -q "$git_repo"
git -C "$git_repo" config user.name 'Tusker Test'
git -C "$git_repo" config user.email 'tusker-test@example.invalid'
printf 'fixture\n' >"$git_repo/fixture"
git -C "$git_repo" add fixture
git -C "$git_repo" commit -qm fixture
git -C "$git_repo" worktree add -qb peer "$git_peer"
main_lock=$(cd "$git_repo" && sh "$gate" --print-lock-path)
peer_lock=$(cd "$git_peer" && sh "$gate" --print-lock-path)
[ "$main_lock" = "$peer_lock" ] || {
	printf 'linked worktrees resolved different locks:\n%s\n%s\n' "$main_lock" "$peer_lock" >&2
	exit 1
}

suite_lock="$tmp_dir/suite.lock"
suite_events="$tmp_dir/suite-events"
TUSKER_VALIDATION_LOCK_DIR="$suite_lock" TUSKER_VALIDATION_LOCK_POLL_SECONDS=0.05 \
	TUSKER_TEST_SUITE_PROBE_EVENTS="$suite_events" TUSKER_TEST_SUITE_PROBE_HOLD_MS=1500 \
	GOMAXPROCS=1 go test -p=1 ./cmd/tusker -run '^TestValidationSuiteLockProbe$' -count=1 >"$tmp_dir/suite-first.log" 2>&1 &
first_pid=$!
wait_for_path "$suite_events"
TUSKER_VALIDATION_LOCK_DIR="$suite_lock" TUSKER_VALIDATION_LOCK_POLL_SECONDS=0.05 \
	TUSKER_TEST_SUITE_PROBE_EVENTS="$suite_events" TUSKER_TEST_SUITE_PROBE_HOLD_MS=0 \
	GOMAXPROCS=1 go test -p=1 ./cmd/tusker -run '^TestValidationSuiteLockProbe$' -count=1 >"$tmp_dir/suite-second.log" 2>&1 &
second_pid=$!
sleep 0.15
[ "$(grep -c '^start ' "$suite_events")" -eq 1 ] || {
	printf 'second raw go test bypassed the suite lock\n' >&2
	exit 1
}
wait "$first_pid"
first_pid=''
wait "$second_pid"
second_pid=''
suite_order=$(sed 's/ [0-9][0-9]*$//' "$suite_events")
[ "$suite_order" = "$(printf 'start\nend\nstart\nend')" ] || {
	printf 'unexpected raw go test order:\n%s\n' "$suite_order" >&2
	exit 1
}

test_plan=$(make -s -n -C "$repo_root" test)
test_unlocked_plan=$(make -s -n -C "$repo_root" test-unlocked)
check_plan=$(make -s -n -C "$repo_root" check)
printf '%s\n' "$test_plan" | grep -q 'with-validation-lock.sh --.*test-unlocked'
printf '%s\n' "$test_unlocked_plan" | grep -q 'GOMAXPROCS=2 go test -timeout=20m -p=1 -parallel=1 ./...'
printf '%s\n' "$check_plan" | grep -q 'with-validation-lock.sh --.*check-unlocked'
printf '%s\n' "$check_plan" | grep -q 'make fmt-check test-unlocked vet-unlocked validate-unlocked build-go-unlocked'

printf 'PASS validation lock serialization, raw go-test admission, stale recovery, signal cleanup, worktree sharing, and canonical routing\n'
